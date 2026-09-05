// Package services contains business logic for internal operations such as moderation,
// automation, audit logging, and data synchronization. It is used by async routines
// and internal system workflows, not exposed via public API routes.
//
// sdworkspace/sdbackend/internal/services/user_favorites_internal.go
//
// GTM:
//
//	Layer: 2.3 Consumer Domain
//	Release Class: DEFERRED
//	Reason:
//	  User favorites are deferred affinity and personalization infrastructure.
//	  They support consumer engagement intelligence, offer-affinity signals,
//	  personalization, and cleanup automation, but they are not part of the
//	  Future Offering v1 release spine.
//
// DEFERRED Rule:
//
//	Keep compiling.
//	Keep production-ready.
//	Preserve active-favorite retrieval.
//	Preserve personalized-offer retrieval.
//	Preserve soft-delete cleanup behavior.
//	Preserve purge behavior for deleted favorites.
//	Preserve DB-owned lifecycle timestamp behavior through the data layer.
//	This file must not drive v1 routes, UI expansion, or release blocking.
package services

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/PiccoloMondoC/focodebase/fobackend/internal/data"

	"github.com/google/uuid"
)

const (
	userFavoritePersonalizedDefaultLimit = 50
	userFavoritePersonalizedMaxLimit     = 100
	userFavoriteAutoExpireWindow         = 90 * 24 * time.Hour
	userFavoriteInactiveWindow           = 120 * 24 * time.Hour
)

// GetPersonalizedOffersInternal retrieves active offer IDs recently favorited by a user.
//
// This method delegates persistence behavior to data.UserFavoriteModel and does not
// invent co-favoriting, category tracking, purchase history, or recommendation logic
// that is not represented by the canonical data model.
func (s *Service) GetPersonalizedOffersInternal(ctx context.Context, userID uuid.UUID, rawLimit int) ([]uuid.UUID, error) {
	logger := s.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("GetPersonalizedOffersInternal")

	if userID == uuid.Nil {
		err := errors.New("user ID is required")
		logger.Warn("Validation failed", "error", err)
		return nil, err
	}

	limit := normalizeUserFavoriteLimit(rawLimit)

	offerIDs, err := s.Models.UserFavorite.GetPersonalizedOffers(ctx, userID, limit)
	if err != nil {
		logger.Error("Get personalized favorite offers failed", "user_id", userID, "limit", limit, "error", err)
		return nil, fmt.Errorf("get personalized favorite offers: %w", err)
	}

	logger.Info("Personalized favorite offers retrieved", "user_id", userID, "count", len(offerIDs))
	return offerIDs, nil
}

// GetUserFavoritesInternal retrieves all active favorites for a user.
func (s *Service) GetUserFavoritesInternal(ctx context.Context, userID uuid.UUID) ([]data.UserFavorite, error) {
	logger := s.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("GetUserFavoritesInternal")

	if userID == uuid.Nil {
		err := errors.New("user ID is required")
		logger.Warn("Validation failed", "error", err)
		return nil, err
	}

	favorites, err := s.Models.UserFavorite.GetByUserID(ctx, userID)
	if err != nil {
		logger.Error("Get user favorites failed", "user_id", userID, "error", err)
		return nil, fmt.Errorf("get user favorites: %w", err)
	}

	logger.Info("User favorites retrieved", "user_id", userID, "count", len(favorites))
	return favorites, nil
}

// FavoriteOfferInternal creates or restores an active favorite for a user and offer.
func (s *Service) FavoriteOfferInternal(ctx context.Context, userID, offerID uuid.UUID) (*data.UserFavorite, error) {
	logger := s.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("FavoriteOfferInternal")

	if userID == uuid.Nil {
		err := errors.New("user ID is required")
		logger.Warn("Validation failed", "error", err)
		return nil, err
	}

	if offerID == uuid.Nil {
		err := errors.New("offer ID is required")
		logger.Warn("Validation failed", "error", err)
		return nil, err
	}

	favorite := &data.UserFavorite{
		UserID:  userID,
		OfferID: offerID,
	}

	if err := s.Models.UserFavorite.Insert(ctx, favorite); err != nil {
		logger.Error("Favorite offer failed", "user_id", userID, "offer_id", offerID, "error", err)
		return nil, fmt.Errorf("favorite offer: %w", err)
	}

	logger.Info("Offer favorited", "favorite_id", favorite.ID, "user_id", userID, "offer_id", offerID)
	return favorite, nil
}

// RestoreFavoriteInternal restores a soft-deleted favorite by reusing the canonical
// idempotent Insert behavior. The data layer owns whether this is a fresh insert,
// an active-row touch, or a soft-deleted-row restore.
func (s *Service) RestoreFavoriteInternal(ctx context.Context, userID, offerID uuid.UUID) error {
	_, err := s.FavoriteOfferInternal(ctx, userID, offerID)
	return err
}

// UserFavoriteExistsInternal checks whether a user currently has an active favorite for an offer.
func (s *Service) UserFavoriteExistsInternal(ctx context.Context, userID, offerID uuid.UUID) (bool, error) {
	logger := s.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("UserFavoriteExistsInternal")

	if userID == uuid.Nil {
		err := errors.New("user ID is required")
		logger.Warn("Validation failed", "error", err)
		return false, err
	}

	if offerID == uuid.Nil {
		err := errors.New("offer ID is required")
		logger.Warn("Validation failed", "error", err)
		return false, err
	}

	exists, err := s.Models.UserFavorite.Exists(ctx, userID, offerID)
	if err != nil {
		logger.Error("User favorite exists check failed", "user_id", userID, "offer_id", offerID, "error", err)
		return false, fmt.Errorf("check user favorite exists: %w", err)
	}

	return exists, nil
}

// GetUsersWhoFavoritedOfferInternal retrieves user IDs with an active favorite for the offer.
func (s *Service) GetUsersWhoFavoritedOfferInternal(ctx context.Context, offerID uuid.UUID) ([]uuid.UUID, error) {
	logger := s.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("GetUsersWhoFavoritedOfferInternal")

	if offerID == uuid.Nil {
		err := errors.New("offer ID is required")
		logger.Warn("Validation failed", "error", err)
		return nil, err
	}

	userIDs, err := s.Models.UserFavorite.GetUsersWhoFavoritedOffer(ctx, offerID)
	if err != nil {
		logger.Error("Get users who favorited offer failed", "offer_id", offerID, "error", err)
		return nil, fmt.Errorf("get users who favorited offer: %w", err)
	}

	logger.Info("Users who favorited offer retrieved", "offer_id", offerID, "count", len(userIDs))
	return userIDs, nil
}

// AutoExpireOldFavoritesInternal soft-deletes active favorites older than the
// platform's deferred-domain auto-expiration window.
func (s *Service) AutoExpireOldFavoritesInternal(ctx context.Context) error {
	logger := s.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("AutoExpireOldFavoritesInternal")

	if err := s.Models.UserFavorite.SoftDeleteInactiveFavorites(ctx, userFavoriteAutoExpireWindow); err != nil {
		logger.Error("Auto-expire old favorites failed", "error", err)
		return fmt.Errorf("auto-expire old favorites: %w", err)
	}

	logger.Info("Old favorites auto-expired", "retention_window", userFavoriteAutoExpireWindow.String())
	return nil
}

// BatchSoftDeleteInactiveFavoritesInternal soft-deletes active favorites older
// than the deferred-domain inactivity cleanup window.
func (s *Service) BatchSoftDeleteInactiveFavoritesInternal(ctx context.Context) error {
	logger := s.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("BatchSoftDeleteInactiveFavoritesInternal")

	if err := s.Models.UserFavorite.SoftDeleteInactiveFavorites(ctx, userFavoriteInactiveWindow); err != nil {
		logger.Error("Soft-delete inactive favorites failed", "error", err)
		return fmt.Errorf("soft-delete inactive favorites: %w", err)
	}

	logger.Info("Inactive favorites soft-deleted", "inactivity_window", userFavoriteInactiveWindow.String())
	return nil
}

// PurgeDeletedFavoritesInternal permanently deletes soft-deleted favorites that
// are eligible for purge under the canonical data-layer retention rule.
func (s *Service) PurgeDeletedFavoritesInternal(ctx context.Context) error {
	logger := s.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("PurgeDeletedFavoritesInternal")

	rowsDeleted, err := s.Models.UserFavorite.PurgeDeleted(ctx)
	if err != nil {
		logger.Error("Purge deleted favorites failed", "error", err)
		return fmt.Errorf("purge deleted favorites: %w", err)
	}

	logger.Info("Deleted favorites purged", "rows_deleted", rowsDeleted)
	return nil
}

func normalizeUserFavoriteLimit(limit int) int {
	if limit <= 0 {
		return userFavoritePersonalizedDefaultLimit
	}

	if limit > userFavoritePersonalizedMaxLimit {
		return userFavoritePersonalizedMaxLimit
	}

	return limit
}
