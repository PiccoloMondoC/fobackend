// Package services composes private media storage and reconciles durable object lifecycle records.
// focodebase/fobackend/internal/services/profile_media_lifecycle.go
package services

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/PiccoloMondoC/focodebase/fobackend/internal/data"
	"github.com/PiccoloMondoC/focodebase/fobackend/internal/mediastore"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

var (
	ErrMediaLegacyPhotos = errors.New("legacy database photos require an explicit migration")
	ErrMediaLocationMismatch = errors.New("retained media requires its recorded storage location")
)

// ConfigureMediaInternal is startup-only: main calls it before HTTP/background
// work. Store construction stays outside business logic. It refuses silent
// backend switching and refuses to hide unexpected legacy database photos.
func (s *Service) ConfigureMediaInternal(ctx context.Context, store mediastore.Store) error {
	if ctx == nil {
		return ErrNilContext
	}
	if err := s.validate(); err != nil {
		return err
	}
	if store == nil || s.media != nil {
		return ErrInvalidServiceConfiguration
	}
	driver, location := store.Location()
	if (driver != "filesystem" && driver != "gcs") || location == "" {
		return ErrInvalidServiceConfiguration
	}
	ctx, cancel := context.WithTimeout(ctx, s.Cfg.DBTimeout)
	defer cancel()
	var legacy bool
	if err := s.Models.DB.QueryRow(ctx, `SELECT to_regclass('public.user_profile_photos') IS NOT NULL`).Scan(&legacy); err != nil {
		return err
	}
	if legacy {
		var populated bool
		if err := s.Models.DB.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM user_profile_photos)`).Scan(&populated); err != nil {
			return err
		}
		if populated {
			return fmt.Errorf("%w: %w", ErrInvalidServiceConfiguration, ErrMediaLegacyPhotos)
		}
	}
	var mismatch bool
	if err := s.Models.DB.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM media_objects WHERE storage_driver<>$1 OR storage_location<>$2)`, driver, location).Scan(&mismatch); err != nil {
		return err
	}
	if mismatch {
		return fmt.Errorf("%w: %w", ErrInvalidServiceConfiguration, ErrMediaLocationMismatch)
	}
	s.media = store
	s.photoSlots = make(chan struct{}, 2)
	return nil
}

// RunMediaCleanupInternal is bounded, restart-safe and safe across instances.
// Cleanup never locks users/profiles, so it cannot reverse the mutation lock
// order (account -> profile -> object). It locks only eligible media rows.
// Object deletion is idempotent; a failed DELETE or uncertain commit retains
// enough metadata to retry. Active photos of retained accounts are excluded.
func (s *Service) RunMediaCleanupInternal(ctx context.Context, limit int) error {
	return s.runMediaCleanup(ctx, limit, true)
}

func (s *Service) runMediaCleanup(ctx context.Context, limit int, includeOrphans bool) error {
	if ctx == nil {
		return ErrNilContext
	}
	if err := s.validate(); err != nil {
		return err
	}
	if s.media == nil {
		return ErrInvalidServiceConfiguration
	}
	if limit < 1 || limit > 32 {
		return data.ErrUserProfileInvalidInput
	}
	driver, location := s.media.Location()
	eligibility := `m.state IN ('staged','retired')`
	if includeOrphans {
		eligibility += ` OR (m.state='active' AND m.purpose='profile_photo' AND
			(u.id IS NULL OR u.deleted_at IS NOT NULL OR p.user_id IS NULL OR p.deleted_at IS NOT NULL))`
	}
	var deferred error
	for n := 0; n < limit; n++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		found := false
		var deletionFailure error
		err := s.profileTransaction(ctx, func(tx pgx.Tx) error {
			var object uuid.UUID
			err := tx.QueryRow(ctx, `SELECT m.id FROM media_objects m
				LEFT JOIN users u ON u.id=m.owner_user_id
				LEFT JOIN user_profiles p ON p.user_id=m.owner_user_id
				WHERE m.storage_driver=$1 AND m.storage_location=$2 AND m.cleanup_after<=clock_timestamp()
					AND (`+eligibility+`)
				ORDER BY m.cleanup_after,m.id LIMIT 1 FOR UPDATE OF m SKIP LOCKED`, driver, location).Scan(&object)
			if errors.Is(err, pgx.ErrNoRows) {
				return nil
			}
			if err != nil {
				return err
			}
			found = true
			if err = s.media.Delete(ctx, object.String()); err != nil {
				deletionFailure = err
				// Commit a retry delay so one unavailable object does not block the queue.
				_, err = tx.Exec(ctx, `UPDATE media_objects SET cleanup_after=clock_timestamp()+INTERVAL '5 minutes' WHERE id=$1`, object)
				return err
			}
			_, err = tx.Exec(ctx, `DELETE FROM media_objects WHERE id=$1`, object)
			return err
		})
		if err != nil {
			return err
		}
		if deletionFailure != nil {
			deferred = deletionFailure
		}
		if !found {
			return deferred
		}
	}
	return deferred
}

// Background cleanup supplements request/startup cleanup; correctness never
// assumes Cloud Run keeps a process or allocates CPU between requests.
func (s *Service) StartMediaCleanupInternal(ctx context.Context) (<-chan struct{}, error) {
	if ctx == nil {
		return nil, ErrNilContext
	}
	if err := s.validate(); err != nil {
		return nil, err
	}
	if s.media == nil {
		return nil, ErrInvalidServiceConfiguration
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		tick := time.NewTicker(5 * time.Minute)
		defer tick.Stop()
		for {
			run, cancel := context.WithTimeout(ctx, 15*time.Second)
			err := s.RunMediaCleanupInternal(run, 16)
			cancel()
			if err != nil && ctx.Err() == nil {
				s.Logger.Warn("media cleanup deferred")
			}
			select {
			case <-ctx.Done():
				return
			case <-s.ShutdownChan:
				return
			case <-tick.C:
			}
		}
	}()
	return done, nil
}

// A request-driven pass permits progress with request-based Cloud Run CPU.
// Failure affects cleanup scheduling, not access to the current valid object.
func (s *Service) cleanupMediaOnRequest(ctx context.Context) {
	cleanup, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	if err := s.runMediaCleanup(cleanup, 1, false); err != nil && ctx.Err() == nil {
		s.Logger.Warn("media cleanup deferred")
	}
}

func (s *Service) retireStagedMedia(id uuid.UUID) {
	// Never delete by key here: the profile commit might have succeeded even if
	// its acknowledgment was lost. Only a still-staged record can be retired.
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_, err := s.Models.DB.Exec(ctx, `UPDATE media_objects SET state='retired',cleanup_after=clock_timestamp()+INTERVAL '15 minutes' WHERE id=$1 AND state='staged'`, id)
	if err != nil {
		s.Logger.Warn("media staging reconciliation deferred")
	}
}
