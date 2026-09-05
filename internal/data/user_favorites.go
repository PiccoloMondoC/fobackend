// Package data provides models and database access methods for user favorites
// and merchant follows.
//
// sdworkspace/sdbackend/internal/data/user_favorites.go
//
// GTM:
//
//	Layer: 2.3 Consumer Domain
//	Release Class: DEFERRED
//	Reason:
//	  User favorites, My Stash behavior, merchant follows, favorite-derived
//	  recommendations, personalized retrieval, and favorite analytics are
//	  valid future consumer-engagement capabilities, but they are not required
//	  for the initial Platform release spine. The initial release
//	  prioritizes canonical offers, publication governance, commerce routing,
//	  attribution, merchant foundations, and the Future Offering Platform
//	  supported by its Monetization Layer.
//
// DEFERRED Rule:
//
//	Keep compiling.
//	Keep safe.
//	Preserve one-active-favorite-per-user-per-offer semantics.
//	Preserve soft-delete and restore behavior.
//	Preserve database-owned lifecycle timestamps.
//	Preserve true hard-delete and retention-based purge as distinct operations.
//	Preserve merchant-follow persistence and public-safe offer filtering.
//	Do not introduce new favorite, My Stash, merchant-follow, recommendation,
//	or favorite-analytics API capabilities for the initial release.
//	Do not expose deferred workflows in the v1 router.
//	Do not block deployment on this domain unless it breaks compilation or
//	compromises a SPINE-dependent package.
package data

import (
	"context"
	"errors"
	"time"

	"github.com/PiccoloMondoC/focodebase/fobackend/internal/logging"
	"github.com/PiccoloMondoC/focodebase/fobackend/internal/utils/timeutil"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// UserFavorite represents a consumer-owned favorite mapping to an offer.
//
// Lifecycle:
// - Active row:   deleted_at IS NULL
// - Deleted row:  deleted_at IS NOT NULL
//
// Persisted lifecycle timestamps are DB-owned.
type UserFavorite struct {
	ID      uuid.UUID `json:"id" db:"id"`
	UserID  uuid.UUID `json:"user_id" db:"user_id"`
	OfferID uuid.UUID `json:"offer_id" db:"offer_id"`
	// FavoritedAt records the most recent time the offer was actively added
	// to the user's stash. If a soft-deleted favorite is restored, FavoritedAt
	// is refreshed to the restore time, because restore is treated as a new
	// active favorite action.
	FavoritedAt time.Time  `json:"favorited_at" db:"favorited_at"`
	UpdatedAt   time.Time  `json:"updated_at" db:"updated_at"`
	DeletedAt   *time.Time `json:"-" db:"deleted_at"`
}

// UserMerchantFollow represents a user following a merchant.
type UserMerchantFollow struct {
	ID         uuid.UUID `json:"id" db:"id"`
	UserID     uuid.UUID `json:"user_id" db:"user_id"`
	MerchantID uuid.UUID `json:"merchant_id" db:"merchant_id"`
	FollowedAt time.Time `json:"followed_at" db:"followed_at"`
}

// UserFavoriteModel is the structure which holds the DB instance.
type UserFavoriteModel struct {
	DB     *pgxpool.Pool
	Logger *logging.Logger
}

// UserMerchantFollowModel is the structure which holds the DB instance.
type UserMerchantFollowModel struct {
	DB     *pgxpool.Pool
	Logger *logging.Logger
}

// Insert creates or restores an active favorite for the given user/offer pair.
//
// Contract:
// - Only one active row may exist per (user_id, offer_id).
// - If an active row already exists, the operation is idempotent.
// - If a soft-deleted row exists, the operation restores that row.
func (m *UserFavoriteModel) Insert(ctx context.Context, favorite *UserFavorite) error {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("InsertUserFavorite")

	if favorite == nil {
		err := errors.New("favorite payload is required")
		logger.Error("Validation failed", err)
		return err
	}

	if favorite.UserID == uuid.Nil {
		err := errors.New("user ID is required")
		logger.Error("Validation failed", err)
		return err
	}

	if favorite.OfferID == uuid.Nil {
		err := errors.New("offer ID is required")
		logger.Error("Validation failed", err)
		return err
	}

	// First, attempt a normal insert or idempotent touch of an already-active row.
	query := `
		INSERT INTO user_favorites (user_id, offer_id)
		VALUES ($1, $2)
		ON CONFLICT (user_id, offer_id) WHERE deleted_at IS NULL
		DO UPDATE
		SET updated_at = NOW()
		RETURNING id, user_id, offer_id, favorited_at, updated_at, deleted_at;
	`

	err := m.DB.QueryRow(ctx, query, favorite.UserID, favorite.OfferID).Scan(
		&favorite.ID,
		&favorite.UserID,
		&favorite.OfferID,
		&favorite.FavoritedAt,
		&favorite.UpdatedAt,
		&favorite.DeletedAt,
	)
	if err == nil {
		logger.Info(
			"Insert user favorite successful",
			"favorite_id", favorite.ID,
			"user_id", favorite.UserID,
			"offer_id", favorite.OfferID,
		)
		return nil
	}

	// Only a unique-violation path should trigger a restore attempt.
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		restoreQuery := `
			UPDATE user_favorites
			SET deleted_at = NULL,
				favorited_at = NOW(),
				updated_at = NOW()
			WHERE user_id = $1
			  AND offer_id = $2
			  AND deleted_at IS NOT NULL
			RETURNING id, user_id, offer_id, favorited_at, updated_at, deleted_at;
		`

		restoreErr := m.DB.QueryRow(ctx, restoreQuery, favorite.UserID, favorite.OfferID).Scan(
			&favorite.ID,
			&favorite.UserID,
			&favorite.OfferID,
			&favorite.FavoritedAt,
			&favorite.UpdatedAt,
			&favorite.DeletedAt,
		)
		if restoreErr == nil {
			logger.Info(
				"Restored user favorite successfully",
				"favorite_id", favorite.ID,
				"user_id", favorite.UserID,
				"offer_id", favorite.OfferID,
			)
			return nil
		}

		logger.Error("Restore user favorite failed", restoreErr,
			"user_id", favorite.UserID,
			"offer_id", favorite.OfferID,
		)
		return restoreErr
	}

	logger.Error("Insert user favorite failed", err,
		"user_id", favorite.UserID,
		"offer_id", favorite.OfferID,
	)
	return err
}

// GetByID retrieves a single user favorite by its ID.
func (m *UserFavoriteModel) GetByID(ctx context.Context, id uuid.UUID) (*UserFavorite, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("GetUserFavoriteByID")

	if id == uuid.Nil {
		err := errors.New("favorite ID is required")
		logger.Error("Validation failed", err)
		return nil, err
	}

	query := `
		SELECT id, user_id, offer_id, favorited_at, updated_at, deleted_at
		FROM user_favorites
		WHERE id = $1;
	`

	var favorite UserFavorite
	err := m.DB.QueryRow(ctx, query, id).Scan(
		&favorite.ID,
		&favorite.UserID,
		&favorite.OfferID,
		&favorite.FavoritedAt,
		&favorite.UpdatedAt,
		&favorite.DeletedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			logger.Warn("User favorite not found", "favorite_id", id)
			return nil, ErrUserFavoriteNotFound
		}

		logger.Error("Query user favorite failed", err, "favorite_id", id)
		return nil, err
	}

	logger.Info("Retrieved user favorite successfully", "favorite_id", id)
	return &favorite, nil
}

// GetByUserID retrieves all active favorites for a given user ID.
func (m *UserFavoriteModel) GetByUserID(ctx context.Context, userID uuid.UUID) ([]UserFavorite, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("GetUserFavorites")

	if userID == uuid.Nil {
		err := errors.New("user ID is required")
		logger.Error("Validation failed", err)
		return nil, err
	}

	query := `
		SELECT id, user_id, offer_id, favorited_at, updated_at, deleted_at
		FROM user_favorites
		WHERE user_id = $1
		  AND deleted_at IS NULL
		ORDER BY favorited_at DESC;
	`

	rows, err := m.DB.Query(ctx, query, userID)
	if err != nil {
		logger.Error("Query user favorites failed", err, "user_id", userID)
		return nil, err
	}
	defer rows.Close()

	favorites, err := pgx.CollectRows(rows, pgx.RowToStructByName[UserFavorite])
	if err != nil {
		logger.Error("Collect user favorites failed", err, "user_id", userID)
		return nil, err
	}

	logger.Info("Retrieved user favorites successfully", "user_id", userID, "count", len(favorites))
	return favorites, nil
}

// SoftDeleteInactiveFavorites soft-deletes favorites older than the provided inactivity window.
// This is a bulk operation, so affected-row counting is acceptable here.
func (m *UserFavoriteModel) SoftDeleteInactiveFavorites(ctx context.Context, inactivityWindow time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("SoftDeleteInactiveFavorites")

	if inactivityWindow <= 0 {
		err := errors.New("inactivity window must be positive")
		logger.Error("Invalid inactivity window", err)
		return err
	}

	cutoff := timeutil.Now().Add(-inactivityWindow)

	query := `
		UPDATE user_favorites
		SET deleted_at = NOW(),
			updated_at = NOW()
		WHERE deleted_at IS NULL
		  AND favorited_at < $1;
	`

	cmdTag, err := m.DB.Exec(ctx, query, cutoff)
	if err != nil {
		logger.Error("Failed to soft-delete inactive favorites", err, "cutoff", cutoff)
		return err
	}

	logger.Info(
		"Inactive favorites soft-deleted",
		"rows_affected", cmdTag.RowsAffected(),
		"cutoff", cutoff,
	)

	return nil
}

// SoftDelete marks a user favorite as logically removed.
func (m *UserFavoriteModel) SoftDelete(ctx context.Context, id uuid.UUID) error {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("SoftDeleteUserFavorite")

	if id == uuid.Nil {
		err := errors.New("favorite ID is required")
		logger.Error("Validation failed", err)
		return err
	}

	query := `
		UPDATE user_favorites
		SET deleted_at = NOW(),
			updated_at = NOW()
		WHERE id = $1
		  AND deleted_at IS NULL
		RETURNING id;
	`

	var favoriteID uuid.UUID
	err := m.DB.QueryRow(ctx, query, id).Scan(&favoriteID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			logger.Warn("Active user favorite not found for soft delete", "favorite_id", id)
			return ErrUserFavoriteNotFound
		}

		logger.Error("Soft delete user favorite failed", err, "favorite_id", id)
		return err
	}

	logger.Info("Soft delete user favorite successful", "favorite_id", favoriteID)
	return nil
}

// UnsaveFavorite soft-deletes the active favorite identified by its natural
// user/offer key.
//
// This operation is distinct from SoftDelete, which is keyed by the favorite
// row ID. Consumer-facing request paths possess the authenticated user ID and
// trusted offer ID, so the natural key is the correct persistence contract.
//
// Persisted lifecycle timestamps remain database-owned.
func (m *UserFavoriteModel) UnsaveFavorite(
	ctx context.Context,
	userID uuid.UUID,
	offerID uuid.UUID,
) error {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.
		GetLoggerWithContextFromContext(ctx).
		WithFunctionName("UnsaveFavorite")

	if userID == uuid.Nil {
		err := errors.New("user ID is required")
		logger.Error("Validation failed", err)
		return err
	}

	if offerID == uuid.Nil {
		err := errors.New("offer ID is required")
		logger.Error("Validation failed", err)
		return err
	}

	query := `
		UPDATE user_favorites
		SET deleted_at = NOW(),
			updated_at = NOW()
		WHERE user_id = $1
		  AND offer_id = $2
		  AND deleted_at IS NULL
		RETURNING id;
	`

	var favoriteID uuid.UUID

	err := m.DB.QueryRow(ctx, query, userID, offerID).Scan(&favoriteID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			logger.Warn(
				"Active user favorite not found for unsave",
				"user_id", userID,
				"offer_id", offerID,
			)
			return ErrUserFavoriteNotFound
		}

		logger.Error(
			"Unsave user favorite failed",
			err,
			"user_id", userID,
			"offer_id", offerID,
		)
		return err
	}

	logger.Info(
		"Unsave user favorite successful",
		"favorite_id", favoriteID,
		"user_id", userID,
		"offer_id", offerID,
	)

	return nil
}

// Delete permanently removes a user favorite from the database.
func (m *UserFavoriteModel) Delete(ctx context.Context, id uuid.UUID) error {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("DeleteUserFavorite")

	if id == uuid.Nil {
		err := errors.New("favorite ID is required")
		logger.Error("Validation failed", err)
		return err
	}

	query := `
		DELETE FROM user_favorites
		WHERE id = $1
		RETURNING id;
	`

	var deletedID uuid.UUID
	err := m.DB.QueryRow(ctx, query, id).Scan(&deletedID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			logger.Warn("Delete user favorite failed: not found", "favorite_id", id)
			return ErrUserFavoriteNotFound
		}

		logger.Error("Delete user favorite failed", err, "favorite_id", id)
		return err
	}

	logger.Info("Delete user favorite successful", "favorite_id", deletedID)
	return nil
}

// PurgeDeleted permanently deletes soft-deleted favorites older than the retention window.
func (m *UserFavoriteModel) PurgeDeleted(ctx context.Context) (int64, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("PurgeDeletedUserFavorites")

	cutoff := timeutil.Now().Add(-180 * 24 * time.Hour)

	query := `
		DELETE FROM user_favorites
		WHERE deleted_at IS NOT NULL
		  AND deleted_at < $1;
	`

	cmdTag, err := m.DB.Exec(ctx, query, cutoff)
	if err != nil {
		logger.Error("Failed to purge soft-deleted favorites", err, "cutoff", cutoff)
		return 0, err
	}

	logger.Info("Soft-deleted favorites purged", "rows_deleted", cmdTag.RowsAffected())
	return cmdTag.RowsAffected(), nil
}

// Exists checks whether an active favorite mapping exists for a user and offer.
func (m *UserFavoriteModel) Exists(ctx context.Context, userID, offerID uuid.UUID) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("ExistsUserFavorite")

	if userID == uuid.Nil {
		err := errors.New("user ID is required")
		logger.Error("Validation failed", err)
		return false, err
	}

	if offerID == uuid.Nil {
		err := errors.New("offer ID is required")
		logger.Error("Validation failed", err)
		return false, err
	}

	query := `
		SELECT EXISTS (
			SELECT 1
			FROM user_favorites
			WHERE user_id = $1
			  AND offer_id = $2
			  AND deleted_at IS NULL
		);
	`

	var exists bool
	err := m.DB.QueryRow(ctx, query, userID, offerID).Scan(&exists)
	if err != nil {
		logger.Error("Exists user favorite query failed", err,
			"user_id", userID,
			"offer_id", offerID,
		)
		return false, err
	}

	logger.Info("Exists user favorite check successful",
		"user_id", userID,
		"offer_id", offerID,
		"exists", exists,
	)
	return exists, nil
}

// GetActiveByUserID retrieves all active favorites for a given user.
func (m *UserFavoriteModel) GetActiveByUserID(ctx context.Context, userID uuid.UUID) ([]UserFavorite, error) {
	return m.GetByUserID(ctx, userID)
}

// GetFavoritesCount returns the count of active favorites for a user.
func (m *UserFavoriteModel) GetFavoritesCount(ctx context.Context, userID uuid.UUID) (int64, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("GetFavoritesCount")

	if userID == uuid.Nil {
		err := errors.New("user ID is required")
		logger.Error("Validation failed", err)
		return 0, err
	}

	query := `
		SELECT COUNT(*)
		FROM user_favorites
		WHERE user_id = $1
		  AND deleted_at IS NULL;
	`

	var count int64
	if err := m.DB.QueryRow(ctx, query, userID).Scan(&count); err != nil {
		logger.Error("Failed to get favorites count", err, "user_id", userID)
		return 0, err
	}

	logger.Info("Successfully retrieved favorites count", "user_id", userID, "count", count)
	return count, nil
}

// BatchSoftDeleteByUser soft-deletes all active favorites for a user.
// This is a bulk operation, so row-count based reporting is acceptable.
func (m *UserFavoriteModel) BatchSoftDeleteByUser(ctx context.Context, userID uuid.UUID) error {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("BatchSoftDeleteByUser")

	if userID == uuid.Nil {
		err := errors.New("user ID is required")
		logger.Error("Validation failed", err)
		return err
	}

	query := `
		UPDATE user_favorites
		SET deleted_at = NOW(),
			updated_at = NOW()
		WHERE user_id = $1
		  AND deleted_at IS NULL;
	`

	cmdTag, err := m.DB.Exec(ctx, query, userID)
	if err != nil {
		logger.Error("Batch soft delete failed", err, "user_id", userID)
		return err
	}

	logger.Info("Batch soft delete successful", "user_id", userID, "rows_affected", cmdTag.RowsAffected())
	return nil
}

// BatchHardDeleteByOffer permanently deletes all favorites for a given offer ID.
func (m *UserFavoriteModel) BatchHardDeleteByOffer(ctx context.Context, offerID uuid.UUID) error {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("BatchHardDeleteByOffer")

	if offerID == uuid.Nil {
		err := errors.New("offer ID is required")
		logger.Error("Validation failed", err)
		return err
	}

	query := `
		DELETE FROM user_favorites
		WHERE offer_id = $1;
	`

	cmdTag, err := m.DB.Exec(ctx, query, offerID)
	if err != nil {
		logger.Error("Batch hard delete failed", err, "offer_id", offerID)
		return err
	}

	logger.Info("Batch hard delete successful", "offer_id", offerID, "rows_deleted", cmdTag.RowsAffected())
	return nil
}

// RecommendOffersForUser suggests offers based on collaborative filtering.
// Already-favorited active offers are explicitly excluded from the result set.
func (m *UserFavoriteModel) RecommendOffersForUser(ctx context.Context, userID uuid.UUID, limit int) ([]uuid.UUID, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("RecommendOffersForUser")

	if userID == uuid.Nil {
		err := errors.New("user ID is required")
		logger.Error("Validation failed", err)
		return nil, err
	}

	if limit <= 0 {
		err := errors.New("limit must be greater than zero")
		logger.Error("Validation failed", err)
		return nil, err
	}

	query := `
		WITH user_fav AS (
			SELECT offer_id
			FROM user_favorites
			WHERE user_id = $1
			  AND deleted_at IS NULL
		),
		similar_users AS (
			SELECT DISTINCT uf.user_id
			FROM user_favorites uf
			JOIN user_fav ufav ON uf.offer_id = ufav.offer_id
			WHERE uf.user_id <> $1
			  AND uf.deleted_at IS NULL
		),
		recommended_offers AS (
			SELECT uf.offer_id, COUNT(*) AS popularity
			FROM user_favorites uf
			JOIN similar_users su ON uf.user_id = su.user_id
			WHERE uf.deleted_at IS NULL
			  AND NOT EXISTS (
				SELECT 1
				FROM user_fav
				WHERE user_fav.offer_id = uf.offer_id
			  )
			GROUP BY uf.offer_id
			ORDER BY popularity DESC, uf.offer_id
			LIMIT $2
		)
		SELECT offer_id
		FROM recommended_offers;
	`

	rows, err := m.DB.Query(ctx, query, userID, limit)
	if err != nil {
		logger.Error("Query execution failed", err, "user_id", userID, "limit", limit)
		return nil, err
	}
	defer rows.Close()

	recommendedOffers, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (uuid.UUID, error) {
		var offerID uuid.UUID
		err := row.Scan(&offerID)
		return offerID, err
	})
	if err != nil {
		logger.Error("Collect recommended offers failed", err, "user_id", userID)
		return nil, err
	}

	logger.Info("RecommendOffersForUser successful", "user_id", userID, "recommendations_count", len(recommendedOffers))
	return recommendedOffers, nil
}

// GetPersonalizedOffers retrieves recently favorited offers for the user.
// DISTINCT is intentionally removed because user_favorites already has at most one active row per user/offer.
func (m *UserFavoriteModel) GetPersonalizedOffers(ctx context.Context, userID uuid.UUID, limit int) ([]uuid.UUID, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("GetPersonalizedOffers")

	if userID == uuid.Nil {
		err := errors.New("user ID is required")
		logger.Error("Validation failed", err)
		return nil, err
	}

	if limit <= 0 {
		err := errors.New("limit must be greater than zero")
		logger.Error("Validation failed", err)
		return nil, err
	}

	query := `
		SELECT o.id
		FROM offers o
		JOIN user_favorites uf ON o.id = uf.offer_id
		WHERE uf.user_id = $1
		  AND uf.deleted_at IS NULL
		  AND o.deleted_at IS NULL
		ORDER BY uf.favorited_at DESC
		LIMIT $2;
	`

	rows, err := m.DB.Query(ctx, query, userID, limit)
	if err != nil {
		logger.Error("Get personalized offers failed", err, "user_id", userID, "limit", limit)
		return nil, err
	}
	defer rows.Close()

	offerIDs, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (uuid.UUID, error) {
		var offerID uuid.UUID
		err := row.Scan(&offerID)
		return offerID, err
	})
	if err != nil {
		logger.Error("Collect personalized offers failed", err, "user_id", userID)
		return nil, err
	}

	logger.Info("GetPersonalizedOffers successful", "user_id", userID, "count", len(offerIDs))
	return offerIDs, nil
}

// GetUserOfferPurchaseHistory is not implemented here because this model does not own purchase history persistence.
func (m *UserFavoriteModel) GetUserOfferPurchaseHistory(ctx context.Context, userID uuid.UUID, limit int) ([]uuid.UUID, error) {
	return nil, errors.New("purchase history is not implemented in user_favorites; no canonical purchase-history field or table exists for this model")
}

// GetMostFavoritedOffers retrieves the most favorited offers within a timeframe.
func (m *UserFavoriteModel) GetMostFavoritedOffers(ctx context.Context, limit int, since time.Time) ([]uuid.UUID, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("GetMostFavoritedOffers")

	if limit <= 0 {
		err := errors.New("limit must be greater than zero")
		logger.Error("Validation failed", err)
		return nil, err
	}

	if since.IsZero() {
		err := errors.New("since timestamp is required")
		logger.Error("Validation failed", err)
		return nil, err
	}

	now := timeutil.Now()
	if since.After(now) {
		err := errors.New("since timestamp cannot be in the future")
		logger.Error("Validation failed", err, "since", since.UTC(), "now", now)
		return nil, err
	}

	query := `
		SELECT offer_id
		FROM user_favorites
		WHERE favorited_at >= $1
		  AND deleted_at IS NULL
		GROUP BY offer_id
		ORDER BY COUNT(*) DESC, offer_id
		LIMIT $2;
	`

	rows, err := m.DB.Query(ctx, query, since.UTC(), limit)
	if err != nil {
		logger.Error("Query execution failed", err, "since", since.UTC(), "limit", limit)
		return nil, err
	}
	defer rows.Close()

	offerIDs, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (uuid.UUID, error) {
		var offerID uuid.UUID
		err := row.Scan(&offerID)
		return offerID, err
	})
	if err != nil {
		logger.Error("Collect most favorited offers failed", err, "since", since.UTC())
		return nil, err
	}

	logger.Info("Retrieved most favorited offers", "count", len(offerIDs))
	return offerIDs, nil
}

// GetRecentlyFavoritedByUser retrieves a user's most recently favorited active offers.
func (m *UserFavoriteModel) GetRecentlyFavoritedByUser(ctx context.Context, userID uuid.UUID, limit int) ([]UserFavorite, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("GetRecentlyFavoritedByUser")

	if userID == uuid.Nil {
		err := errors.New("user ID is required")
		logger.Error("Validation failed", err)
		return nil, err
	}

	if limit <= 0 {
		err := errors.New("limit must be greater than zero")
		logger.Error("Validation failed", err)
		return nil, err
	}

	query := `
		SELECT id, user_id, offer_id, favorited_at, updated_at, deleted_at
		FROM user_favorites
		WHERE user_id = $1
		  AND deleted_at IS NULL
		ORDER BY favorited_at DESC
		LIMIT $2;
	`

	rows, err := m.DB.Query(ctx, query, userID, limit)
	if err != nil {
		logger.Error("Query execution failed", err, "user_id", userID, "limit", limit)
		return nil, err
	}
	defer rows.Close()

	favorites, err := pgx.CollectRows(rows, pgx.RowToStructByName[UserFavorite])
	if err != nil {
		logger.Error("Collect recently favorited offers failed", err, "user_id", userID)
		return nil, err
	}

	logger.Info("Successfully retrieved user favorites", "user_id", userID, "count", len(favorites))
	return favorites, nil
}

// GetUsersWhoFavoritedOffer retrieves user IDs that actively favorited the given offer.
func (m *UserFavoriteModel) GetUsersWhoFavoritedOffer(ctx context.Context, offerID uuid.UUID) ([]uuid.UUID, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("GetUsersWhoFavoritedOffer")

	if offerID == uuid.Nil {
		err := errors.New("offer ID is required")
		logger.Error("Validation failed", err)
		return nil, err
	}

	query := `
		SELECT user_id
		FROM user_favorites
		WHERE offer_id = $1
		  AND deleted_at IS NULL
		ORDER BY favorited_at DESC;
	`

	rows, err := m.DB.Query(ctx, query, offerID)
	if err != nil {
		logger.Error("Query failed", err, "offer_id", offerID)
		return nil, err
	}
	defer rows.Close()

	userIDs, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (uuid.UUID, error) {
		var userID uuid.UUID
		err := row.Scan(&userID)
		return userID, err
	})
	if err != nil {
		logger.Error("Collect users who favorited offer failed", err, "offer_id", offerID)
		return nil, err
	}

	logger.Info("Retrieved users who favorited offer", "offer_id", offerID, "user_count", len(userIDs))
	return userIDs, nil
}

// FollowMerchant creates a merchant-follow mapping for a user.
func (m *UserMerchantFollowModel) FollowMerchant(ctx context.Context, userID, merchantID uuid.UUID) error {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("FollowMerchant")

	if userID == uuid.Nil {
		err := errors.New("user ID is required")
		logger.Error("Validation failed", err)
		return err
	}

	if merchantID == uuid.Nil {
		err := errors.New("merchant ID is required")
		logger.Error("Validation failed", err)
		return err
	}

	query := `
		INSERT INTO user_merchant_follows (user_id, merchant_id)
		VALUES ($1, $2)
		ON CONFLICT (user_id, merchant_id) DO NOTHING;
	`

	_, err := m.DB.Exec(ctx, query, userID, merchantID)
	if err != nil {
		logger.Error("Insert user follow failed", err, "user_id", userID, "merchant_id", merchantID)
		return err
	}

	logger.Info("User followed merchant successfully", "user_id", userID, "merchant_id", merchantID)
	return nil
}

// UnfollowMerchant removes a merchant-follow mapping.
func (m *UserMerchantFollowModel) UnfollowMerchant(ctx context.Context, userID, merchantID uuid.UUID) error {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("UnfollowMerchant")

	if userID == uuid.Nil {
		err := errors.New("user ID is required")
		logger.Error("Validation failed", err)
		return err
	}

	if merchantID == uuid.Nil {
		err := errors.New("merchant ID is required")
		logger.Error("Validation failed", err)
		return err
	}

	query := `
		DELETE FROM user_merchant_follows
		WHERE user_id = $1
		  AND merchant_id = $2
		RETURNING id;
	`

	var deletedID uuid.UUID
	err := m.DB.QueryRow(ctx, query, userID, merchantID).Scan(&deletedID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			logger.Warn("Merchant follow not found", "user_id", userID, "merchant_id", merchantID)
			return ErrMerchantFollowNotFound
		}

		logger.Error("Delete user follow failed", err, "user_id", userID, "merchant_id", merchantID)
		return err
	}

	logger.Info("User unfollowed merchant successfully", "user_id", userID, "merchant_id", merchantID)
	return nil
}

// GetFollowedMerchantsOffers retrieves active, public-safe offers from merchants followed by the user.
func (m *UserMerchantFollowModel) GetFollowedMerchantsOffers(ctx context.Context, userID uuid.UUID, limit int) ([]uuid.UUID, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("GetFollowedMerchantsOffers")

	if userID == uuid.Nil {
		err := errors.New("user ID is required")
		logger.Error("Validation failed", err)
		return nil, err
	}

	if limit <= 0 {
		err := errors.New("limit must be greater than zero")
		logger.Error("Validation failed", err)
		return nil, err
	}

	query := `
		SELECT o.id
		FROM user_merchant_follows umf
		JOIN offers o ON o.merchant_id = umf.merchant_id
		WHERE umf.user_id = $1
		  AND o.deleted_at IS NULL
		  AND o.is_active = TRUE
		  AND o.is_editorial_approved = TRUE
		  AND o.published_at IS NOT NULL
		  AND (o.expires_at IS NULL OR o.expires_at > NOW())
		ORDER BY o.created_at DESC
		LIMIT $2;
	`

	rows, err := m.DB.Query(ctx, query, userID, limit)
	if err != nil {
		logger.Error("Failed to retrieve followed merchant offers", err, "user_id", userID, "limit", limit)
		return nil, err
	}
	defer rows.Close()

	offerIDs, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (uuid.UUID, error) {
		var offerID uuid.UUID
		err := row.Scan(&offerID)
		return offerID, err
	})
	if err != nil {
		logger.Error("Collect followed merchant offers failed", err, "user_id", userID)
		return nil, err
	}

	logger.Info("Retrieved followed merchant offers successfully", "user_id", userID, "count", len(offerIDs))
	return offerIDs, nil
}
