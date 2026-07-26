// Package services contains business logic for internal operations such as moderation,
// automation, audit logging, and data synchronization. It is used by async routines
// and internal system workflows, not exposed via public API routes.
//
// sdworkspace/sdbackend/internal/services/user_favorites_async.go
//
// GTM:
//
//	Layer: 2.3 Consumer Domain
//	Release Class: DEFERRED
//	Reason:
//	  User favorite async work is deferred cleanup and personalization support.
//	  It wraps the internal service contract for background execution without
//	  expanding the v1 release spine.
//
// DEFERRED Rule:
//
//	Keep compiling.
//	Keep production-ready.
//	Preserve bounded async execution.
//	Preserve panic-safe worker behavior.
//	Preserve low-cardinality metrics labels.
//	Preserve OpenTelemetry error recording.
//	Do not expand routes, UI scope, or v1 release dependency from this file.
package services

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/PiccoloMondoC/sdworkspace/sdbackend/internal/observability/metrics"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
)

const userFavoriteAsyncTimeout = 15 * time.Second

// GetPersonalizedOffersEvent encapsulates an async request to retrieve
// personalized favorite-backed offer IDs for a user.
type GetPersonalizedOffersEvent struct {
	UserID uuid.UUID
	Limit  int
}

// FavoriteOfferEvent encapsulates an async request to create or restore a user favorite.
type FavoriteOfferEvent struct {
	UserID  uuid.UUID
	OfferID uuid.UUID
}

// RestoreFavoriteEvent encapsulates an async request to restore a soft-deleted favorite.
type RestoreFavoriteEvent struct {
	UserID  uuid.UUID
	OfferID uuid.UUID
}

// OfferFavoritesEvent encapsulates an async request scoped to a single offer.
type OfferFavoritesEvent struct {
	OfferID uuid.UUID
}

// GetPersonalizedOffersAsync retrieves personalized favorite-backed offer IDs
// in a bounded background goroutine.
func GetPersonalizedOffersAsync(parentCtx context.Context, svc *Service, ev GetPersonalizedOffersEvent) {
	go runUserFavoriteAsync(parentCtx, svc, "GetPersonalizedOffersAsync", "personalize", func(ctx context.Context) error {
		if ev.UserID == uuid.Nil {
			return errors.New("user ID is required")
		}

		offerIDs, err := svc.GetPersonalizedOffersInternal(ctx, ev.UserID, ev.Limit)
		if err != nil {
			return err
		}

		svc.Logger.WithFunctionName("GetPersonalizedOffersAsync").Info(
			"Personalized favorite offers retrieved",
			"count", len(offerIDs),
		)
		return nil
	})
}

// FavoriteOfferAsync creates or restores a favorite in a bounded background goroutine.
func FavoriteOfferAsync(parentCtx context.Context, svc *Service, ev FavoriteOfferEvent) {
	go runUserFavoriteAsync(parentCtx, svc, "FavoriteOfferAsync", "favorite", func(ctx context.Context) error {
		if ev.UserID == uuid.Nil {
			return errors.New("user ID is required")
		}

		if ev.OfferID == uuid.Nil {
			return errors.New("offer ID is required")
		}

		_, err := svc.FavoriteOfferInternal(ctx, ev.UserID, ev.OfferID)
		return err
	})
}

// RestoreFavoriteAsync restores a favorite by delegating to the canonical
// favorite insert/restore service path.
func RestoreFavoriteAsync(parentCtx context.Context, svc *Service, ev RestoreFavoriteEvent) {
	go runUserFavoriteAsync(parentCtx, svc, "RestoreFavoriteAsync", "restore", func(ctx context.Context) error {
		if ev.UserID == uuid.Nil {
			return errors.New("user ID is required")
		}

		if ev.OfferID == uuid.Nil {
			return errors.New("offer ID is required")
		}

		return svc.RestoreFavoriteInternal(ctx, ev.UserID, ev.OfferID)
	})
}

// AutoExpireOldFavoritesAsync soft-deletes old active favorites in a bounded
// background goroutine.
func AutoExpireOldFavoritesAsync(parentCtx context.Context, svc *Service) {
	go runUserFavoriteAsync(parentCtx, svc, "AutoExpireOldFavoritesAsync", "auto_expire", func(ctx context.Context) error {
		return svc.AutoExpireOldFavoritesInternal(ctx)
	})
}

// BatchSoftDeleteInactiveFavoritesAsync soft-deletes inactive active favorites
// in a bounded background goroutine.
func BatchSoftDeleteInactiveFavoritesAsync(parentCtx context.Context, svc *Service) {
	go runUserFavoriteAsync(parentCtx, svc, "BatchSoftDeleteInactiveFavoritesAsync", "soft_delete_inactive", func(ctx context.Context) error {
		return svc.BatchSoftDeleteInactiveFavoritesInternal(ctx)
	})
}

// PurgeDeletedFavoritesAsync permanently removes purge-eligible soft-deleted
// favorites in a bounded background goroutine.
func PurgeDeletedFavoritesAsync(parentCtx context.Context, svc *Service) {
	go runUserFavoriteAsync(parentCtx, svc, "PurgeDeletedFavoritesAsync", "purge_deleted", func(ctx context.Context) error {
		return svc.PurgeDeletedFavoritesInternal(ctx)
	})
}

// GetUsersWhoFavoritedOfferAsync retrieves user IDs for active favorites on an
// offer in a bounded background goroutine. The result is intentionally logged
// only as a count to avoid unbounded identifier fan-out in async observability.
func GetUsersWhoFavoritedOfferAsync(parentCtx context.Context, svc *Service, ev OfferFavoritesEvent) {
	go runUserFavoriteAsync(parentCtx, svc, "GetUsersWhoFavoritedOfferAsync", "users_for_offer", func(ctx context.Context) error {
		if ev.OfferID == uuid.Nil {
			return errors.New("offer ID is required")
		}

		userIDs, err := svc.GetUsersWhoFavoritedOfferInternal(ctx, ev.OfferID)
		if err != nil {
			return err
		}

		svc.Logger.WithFunctionName("GetUsersWhoFavoritedOfferAsync").Info(
			"Users who favorited offer retrieved",
			"count", len(userIDs),
		)
		return nil
	})
}

func runUserFavoriteAsync(
	parentCtx context.Context,
	svc *Service,
	functionName string,
	operation string,
	fn func(context.Context) error,
) {
	start := time.Now()

	ctx, cancel := context.WithTimeout(parentCtx, userFavoriteAsyncTimeout)
	defer cancel()

	logger := svc.Logger.WithFunctionName(functionName)
	tracer := otel.Tracer("user_favorites.async")
	ctx, span := tracer.Start(ctx, functionName)
	defer span.End()

	defer func() {
		metrics.UserFavoriteOperationDuration.
			WithLabelValues(operation).
			Observe(time.Since(start).Seconds())

		if r := recover(); r != nil {
			err := fmt.Errorf("panic: %v", r)
			logger.Error("User favorite async panic", err)
			span.RecordError(err)
			span.SetAttributes(
				attribute.String("status", "panic"),
				attribute.String("operation", operation),
			)
			observeUserFavoriteAsyncResult(operation, "failure")
		}
	}()

	if err := fn(ctx); err != nil {
		logger.Error("User favorite async operation failed", "operation", operation, "error", err)
		span.RecordError(err)
		span.SetAttributes(
			attribute.String("status", "failure"),
			attribute.String("operation", operation),
		)
		observeUserFavoriteAsyncResult(operation, "failure")
		return
	}

	logger.Info("User favorite async operation completed", "operation", operation)
	span.SetAttributes(
		attribute.String("status", "success"),
		attribute.String("operation", operation),
	)
	observeUserFavoriteAsyncResult(operation, "success")
}

func observeUserFavoriteAsyncResult(operation, result string) {
	switch operation {
	case "personalize":
		metrics.UserFavoritePersonalizationResult.WithLabelValues(result).Inc()
	case "auto_expire":
		metrics.UserFavoriteAutoExpireResult.WithLabelValues(result).Inc()
	case "purge_deleted":
		metrics.UserFavoritePurgeResult.WithLabelValues(result).Inc()
	case "restore":
		metrics.UserFavoriteRestoreResult.WithLabelValues(result).Inc()
	default:
		// No-op until metrics.go receives its own SPINE observability review.
		// Duration is still recorded by UserFavoriteOperationDuration.
	}
}
