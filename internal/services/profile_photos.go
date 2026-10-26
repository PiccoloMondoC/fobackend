// Package services provides private, bounded profile photo persistence and safe image normalization.
// focodebase/fobackend/internal/services/profile_photos.go
package services

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	_ "image/png"
	"io"
	"time"

	"github.com/PiccoloMondoC/focodebase/fobackend/internal/data"
	"github.com/PiccoloMondoC/focodebase/fobackend/internal/mediastore"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

const ProfilePhotoMaxBytes = 2 * 1024 * 1024

// DecodeConfig bounds pixel allocation before decoding. Re-encoding strips
// uploaded metadata, rejects active content, and never fetches remote URLs.
func normalizeProfilePhoto(raw []byte) ([]byte, error) {
	invalid := data.ProfileFieldErrors{"photo": "invalid_photo"}
	if len(raw) == 0 || len(raw) > ProfilePhotoMaxBytes {
		return nil, invalid
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(raw))
	if err != nil || (format != "jpeg" && format != "png") || cfg.Width < 1 || cfg.Height < 1 || cfg.Width > 4096 || cfg.Height > 4096 || int64(cfg.Width)*int64(cfg.Height) > 4000000 {
		return nil, invalid
	}
	decoded, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		return nil, invalid
	}
	// Composite transparency on white for predictable JPEG rendering.
	canvas := image.NewRGBA(image.Rect(0, 0, cfg.Width, cfg.Height))
	draw.Draw(canvas, canvas.Bounds(), &image.Uniform{C: color.White}, image.Point{}, draw.Src)
	draw.Draw(canvas, canvas.Bounds(), decoded, decoded.Bounds().Min, draw.Over)
	var out bytes.Buffer
	if err = jpeg.Encode(&out, orientProfilePhoto(canvas, profilePhotoOrientation(raw)), &jpeg.Options{Quality: 85}); err != nil {
		return nil, invalid
	}
	if out.Len() > ProfilePhotoMaxBytes {
		return nil, invalid
	}
	return out.Bytes(), nil
}

func (s *Service) SaveOwnProfilePhotoInternal(ctx context.Context, id uuid.UUID, raw []byte, expected *time.Time, remove bool) (*ProfileWorkspace, error) {
	ctx, cancel, err := s.profileRequest(ctx, id)
	if err != nil {
		return nil, err
	}
	defer cancel()
	if s.media == nil {
		return nil, ErrInvalidServiceConfiguration
	}
	if expected == nil {
		return nil, data.ErrProfileVersionRequired
	}
	operation, finish := context.WithTimeout(ctx, 12*time.Second)
	defer finish()
	ctx = operation
	if !remove {
		// Bound decoding memory and upload concurrency per process. Reject
		// overload without queuing an unbounded number of decoded images.
		select {
		case s.photoSlots <- struct{}{}:
			defer func() { <-s.photoSlots }()
		default:
			return nil, mediastore.ErrUnavailable
		}
	}
	// Cleanup failure never discards a user's valid current photo.
	s.cleanupMediaOnRequest(ctx)
	var staged uuid.UUID
	if !remove {
		normalized, err := normalizeProfilePhoto(raw)
		if err != nil {
			return nil, err
		}
		staged = uuid.New()
		driver, location := s.media.Location()
		sum := sha256.Sum256(normalized)
		// Register intent before touching storage; serialize per-owner staging
		// under the profile lock. At most four uploads may remain in flight
		// for one owner. Stale drafts do not upload any bytes.
		err = s.profileTransaction(ctx, func(tx pgx.Tx) error {
			m := &s.Models.UserProfile
			if err := m.LockProfileAccountTx(ctx, tx, id); err != nil {
				return err
			}
			p, err := m.ReadProfileTx(ctx, tx, id, true)
			if err != nil {
				return err
			}
			if p == nil {
				return data.ErrUserProfileNotFound
			}
			if !p.UpdatedAt.Equal(*expected) {
				return data.ErrProfileConflict
			}
			var count int
			if err = tx.QueryRow(ctx, `SELECT count(*) FROM media_objects WHERE owner_user_id=$1 AND purpose='profile_photo' AND state='staged'`, id).Scan(&count); err != nil {
				return err
			}
			if count >= 4 {
				return data.ErrProfileConflict
			}
			_, err = tx.Exec(ctx, `INSERT INTO media_objects(id,owner_user_id,purpose,storage_driver,storage_location,content_type,byte_size,sha256,state)
				VALUES($1,$2,'profile_photo',$3,$4,'image/jpeg',$5,$6,'staged')`, staged, id, driver, location, len(normalized), hex.EncodeToString(sum[:]))
			return err
		})
		// If staging's commit outcome is uncertain, its TTL still permits
		// cleanup. No upload occurs until registration is confirmed.
		if err != nil {
			return nil, err
		}
		defer s.retireStagedMedia(staged)
		if err = s.media.Put(ctx, staged.String(), "image/jpeg", bytes.NewReader(normalized)); err != nil {
			return nil, err
		}
	}
	var result *ProfileWorkspace
	err = s.profileTransaction(ctx, func(tx pgx.Tx) error {
		m := &s.Models.UserProfile
		if err := m.LockProfileAccountTx(ctx, tx, id); err != nil {
			return err
		}
		p, err := m.ReadProfileTx(ctx, tx, id, true)
		if err != nil {
			return err
		}
		if p == nil {
			return data.ErrUserProfileNotFound
		}
		if !p.UpdatedAt.Equal(*expected) {
			return data.ErrProfileConflict
		}
		if remove && !p.HasPhoto {
			if err = m.OwnProfileRelationsTx(ctx, tx, p); err != nil {
				return err
			}
			result = profileWorkspace(p)
			return nil
		}
		if _, err = tx.Exec(ctx, `UPDATE media_objects SET state='retired',cleanup_after=clock_timestamp()+INTERVAL '15 minutes'
			WHERE owner_user_id=$1 AND purpose='profile_photo' AND state='active'`, id); err != nil {
			return err
		}
		if !remove {
			tag, err := tx.Exec(ctx, `UPDATE media_objects SET state='active',cleanup_after=clock_timestamp()
				WHERE id=$1 AND owner_user_id=$2 AND state='staged' AND cleanup_after>clock_timestamp()`, staged, id)
			if err != nil {
				return err
			}
			if tag.RowsAffected() != 1 {
				return data.ErrProfileConflict
			}
		}
		if _, err = tx.Exec(ctx, `UPDATE user_profiles SET updated_at=clock_timestamp() WHERE user_id=$1`, id); err != nil {
			return err
		}
		if err = m.AuditProfileTx(ctx, tx, &id, id.String(), "update_user_profile", []string{"profile_photo"}); err != nil {
			return err
		}
		stored, err := m.ReadProfileTx(ctx, tx, id, false)
		if err != nil {
			return err
		}
		if err = m.OwnProfileRelationsTx(ctx, tx, stored); err != nil {
			return err
		}
		result = profileWorkspace(stored)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

func (s *Service) GetOwnProfilePhotoInternal(ctx context.Context, id uuid.UUID) ([]byte, error) {
	ctx, cancel, err := s.profileRequest(ctx, id)
	if err != nil {
		return nil, err
	}
	defer cancel()
	if s.media == nil {
		return nil, ErrInvalidServiceConfiguration
	}
	operation, finish := context.WithTimeout(ctx, 12*time.Second)
	defer finish()
	ctx = operation
	s.cleanupMediaOnRequest(ctx)
	var raw []byte
	err = s.profileTransaction(ctx, func(tx pgx.Tx) error {
		if err := s.Models.UserProfile.LockProfileAccountTx(ctx, tx, id); err != nil {
			return err
		}
		p, err := s.Models.UserProfile.ReadProfileTx(ctx, tx, id, false)
		if err != nil {
			return err
		}
		if p == nil {
			return data.ErrUserProfileNotFound
		}
		var key uuid.UUID
		var size int64
		var digest, driver, location string
		err = tx.QueryRow(ctx, `SELECT id,byte_size,sha256,storage_driver,storage_location FROM media_objects
			WHERE owner_user_id=$1 AND purpose='profile_photo' AND state='active' FOR SHARE`, id).Scan(&key, &size, &digest, &driver, &location)
		if errors.Is(err, pgx.ErrNoRows) {
			return mediastore.ErrNotFound
		}
		if err != nil {
			return err
		}
		actualDriver, actualLocation := s.media.Location()
		if driver != actualDriver || location != actualLocation || size < 1 || size > ProfilePhotoMaxBytes {
			return mediastore.ErrUnavailable
		}
		file, err := s.media.Open(ctx, key.String())
		if err != nil {
			return mediastore.ErrUnavailable
		}
		// Keep account/profile SHARE locks through the bounded read so a
		// concurrent replacement/closure cannot authorize a different snapshot.
		raw, err = io.ReadAll(io.LimitReader(file, ProfilePhotoMaxBytes+1))
		closeErr := file.Close()
		if err != nil || closeErr != nil || int64(len(raw)) != size {
			return mediastore.ErrUnavailable
		}
		sum := sha256.Sum256(raw)
		if hex.EncodeToString(sum[:]) != digest {
			return mediastore.ErrUnavailable
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return raw, nil
}
