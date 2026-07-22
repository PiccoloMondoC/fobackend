// Package data provides models and database access methods for offer ratings and other entities.
//
// File: sdworkspace/sdbackend/internal/data/offer_ratings.go
//
// GTM:
//   Layer: 2.5 Catalog / Offer Domain
//   Release Class: DEFERRED
//   Reason:
//     Offer ratings are valid future engagement and trust-signal
//     infrastructure, but they are not required for the initial Platform
//     release spine. The v1 spine requires canonical offers, publication
//     governance, affiliate links, click tracking, price history, and moderation
//     flags before expanding into user-generated rating and review workflows.
//
// DEFERRED Rule:
//   Keep compiling.
//   Keep safe.
//   Preserve one-active-rating-per-user-per-offer semantics.
//   Preserve soft-delete review moderation behavior.
//   Preserve DB-owned lifecycle timestamp behavior.
//   Preserve bounded analytics/reporting behavior.
//   Do not add new features.
//   Do not route into v1 UI/API expansion.
//   Do not block deployment on this file unless it breaks the build.
package data

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/PiccoloMondoC/sdworkspace/sdbackend/internal/logging"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// offerRatingSelectColumns is the canonical flat column list for offer_ratings SELECT queries.
const offerRatingSelectColumns = `id, user_id, offer_id, rating, review, deleted_at, created_at, updated_at`

// offerRatingSpamReviewLengthThreshold is the character length above which a review
// is considered a spam candidate by AutoFlagSpamReviews.
// Richer spam detection belongs in a dedicated moderation service.
const offerRatingSpamReviewLengthThreshold = 500

// OfferRating represents a user's rating and optional review for an offer.
// This struct reflects the canonical persisted row shape in the database.
type OfferRating struct {
	ID        uuid.UUID  `json:"id"                   db:"id"`
	UserID    uuid.UUID  `json:"user_id"              db:"user_id"`
	OfferID   uuid.UUID  `json:"offer_id"             db:"offer_id"`
	Rating    int        `json:"rating"               db:"rating"`
	Review    *string    `json:"review,omitempty"     db:"review"`
	DeletedAt *time.Time `json:"-"                    db:"deleted_at"`
	CreatedAt time.Time  `json:"created_at"           db:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"           db:"updated_at"`
}

// OfferRatingModel holds the DB pool and logger for offer rating data access.
type OfferRatingModel struct {
	DB     *pgxpool.Pool
	Logger *logging.Logger
}

// normalizeOptionalReview trims review text and collapses blank strings to nil.
func normalizeOptionalReview(review *string) *string {
	if review == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*review)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

// scanOfferRating scans a single pgx row or QueryRow result into an OfferRating.
func scanOfferRating(scanner interface {
	Scan(dest ...any) error
}, rating *OfferRating) error {
	return scanner.Scan(
		&rating.ID,
		&rating.UserID,
		&rating.OfferID,
		&rating.Rating,
		&rating.Review,
		&rating.DeletedAt,
		&rating.CreatedAt,
		&rating.UpdatedAt,
	)
}

// collectOfferRating is the canonical pgx.CollectRows collector for OfferRating.
func collectOfferRating(row pgx.CollectableRow) (*OfferRating, error) {
	var rating OfferRating
	if err := scanOfferRating(row, &rating); err != nil {
		return nil, err
	}
	return &rating, nil
}

// Insert inserts a new active offer rating into the database.
// The database owns id, created_at, and updated_at.
// The partial unique index ux_offer_ratings_active_user_offer enforces one active
// rating per (user_id, offer_id) pair at the database level.
func (m *OfferRatingModel) Insert(ctx context.Context, offerRating *OfferRating) error {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("InsertOfferRating")

	if offerRating == nil {
		err := errors.New("offer rating is required")
		logger.Error("Validation failed", err)
		return err
	}
	if offerRating.UserID == uuid.Nil {
		err := errors.New("user_id is required")
		logger.Error("Validation failed", err)
		return err
	}
	if offerRating.OfferID == uuid.Nil {
		err := errors.New("offer_id is required")
		logger.Error("Validation failed", err)
		return err
	}
	if offerRating.Rating < 1 || offerRating.Rating > 5 {
		err := errors.New("rating must be between 1 and 5")
		logger.Error("Validation failed", err)
		return err
	}

	offerRating.Review = normalizeOptionalReview(offerRating.Review)

	query := fmt.Sprintf(`
		INSERT INTO offer_ratings (
			user_id,
			offer_id,
			rating,
			review
		)
		VALUES ($1, $2, $3, $4)
		RETURNING %s
	`, offerRatingSelectColumns)

	err := scanOfferRating(
		m.DB.QueryRow(ctx, query,
			offerRating.UserID,
			offerRating.OfferID,
			offerRating.Rating,
			offerRating.Review,
		),
		offerRating,
	)
	if err != nil {
		logger.Error("Insert offer rating failed", err,
			"user_id", offerRating.UserID,
			"offer_id", offerRating.OfferID,
		)
		return err
	}

	logger.Info("Insert offer rating successful",
		"id", offerRating.ID,
		"user_id", offerRating.UserID,
		"offer_id", offerRating.OfferID,
		"rating", offerRating.Rating,
	)

	return nil
}

// GetByID retrieves an offer rating by its primary key.
// Soft-deleted records are returned; the caller must inspect DeletedAt if
// visibility filtering is required.
func (m *OfferRatingModel) GetByID(ctx context.Context, id uuid.UUID) (*OfferRating, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("GetOfferRatingByID")

	if id == uuid.Nil {
		err := errors.New("id is required")
		logger.Error("Validation failed", err)
		return nil, err
	}

	query := fmt.Sprintf(`
		SELECT %s
		FROM offer_ratings
		WHERE id = $1
	`, offerRatingSelectColumns)

	var offerRating OfferRating
	err := scanOfferRating(m.DB.QueryRow(ctx, query, id), &offerRating)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			logger.Warn("Offer rating not found", "id", id)
			return nil, ErrOfferRatingNotFound
		}
		logger.Error("Get offer rating by ID failed", err, "id", id)
		return nil, err
	}

	logger.Info("Get offer rating by ID successful", "id", offerRating.ID)
	return &offerRating, nil
}

// GetByOfferID retrieves all active offer ratings for a specific offer,
// ordered by most recently created first.
func (m *OfferRatingModel) GetByOfferID(ctx context.Context, offerID uuid.UUID) ([]*OfferRating, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("GetOfferRatingsByOfferID")

	if offerID == uuid.Nil {
		err := errors.New("offer_id is required")
		logger.Error("Validation failed", err)
		return nil, err
	}

	query := fmt.Sprintf(`
		SELECT %s
		FROM offer_ratings
		WHERE offer_id = $1
		  AND deleted_at IS NULL
		ORDER BY created_at DESC
	`, offerRatingSelectColumns)

	rows, err := m.DB.Query(ctx, query, offerID)
	if err != nil {
		logger.Error("Get offer ratings by offer ID failed", err, "offer_id", offerID)
		return nil, err
	}
	defer rows.Close()

	ratings, err := pgx.CollectRows(rows, collectOfferRating)
	if err != nil {
		logger.Error("Collect offer ratings by offer ID failed", err, "offer_id", offerID)
		return nil, err
	}

	logger.Info("Get offer ratings by offer ID successful", "offer_id", offerID, "count", len(ratings))
	return ratings, nil
}

// GetByUserID retrieves all active offer ratings created by a specific user,
// ordered by most recently created first.
func (m *OfferRatingModel) GetByUserID(ctx context.Context, userID uuid.UUID) ([]*OfferRating, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("GetOfferRatingsByUserID")

	if userID == uuid.Nil {
		err := errors.New("user_id is required")
		logger.Error("Validation failed", err)
		return nil, err
	}

	query := fmt.Sprintf(`
		SELECT %s
		FROM offer_ratings
		WHERE user_id = $1
		  AND deleted_at IS NULL
		ORDER BY created_at DESC
	`, offerRatingSelectColumns)

	rows, err := m.DB.Query(ctx, query, userID)
	if err != nil {
		logger.Error("Get offer ratings by user ID failed", err, "user_id", userID)
		return nil, err
	}
	defer rows.Close()

	ratings, err := pgx.CollectRows(rows, collectOfferRating)
	if err != nil {
		logger.Error("Collect offer ratings by user ID failed", err, "user_id", userID)
		return nil, err
	}

	logger.Info("Get offer ratings by user ID successful", "user_id", userID, "count", len(ratings))
	return ratings, nil
}

// GetByUserIDAndOfferID retrieves the active rating for a given user and offer pair.
// Returns ErrOfferRatingNotFound if no active rating exists for the pair.
func (m *OfferRatingModel) GetByUserIDAndOfferID(ctx context.Context, userID, offerID uuid.UUID) (*OfferRating, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("GetOfferRatingByUserIDAndOfferID")

	if userID == uuid.Nil {
		err := errors.New("user_id is required")
		logger.Error("Validation failed", err)
		return nil, err
	}
	if offerID == uuid.Nil {
		err := errors.New("offer_id is required")
		logger.Error("Validation failed", err)
		return nil, err
	}

	query := fmt.Sprintf(`
		SELECT %s
		FROM offer_ratings
		WHERE user_id = $1
		  AND offer_id = $2
		  AND deleted_at IS NULL
		LIMIT 1
	`, offerRatingSelectColumns)

	var rating OfferRating
	err := scanOfferRating(m.DB.QueryRow(ctx, query, userID, offerID), &rating)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			logger.Info("Offer rating not found", "user_id", userID, "offer_id", offerID)
			return nil, ErrOfferRatingNotFound
		}
		logger.Error("Get offer rating by user and offer failed", err,
			"user_id", userID,
			"offer_id", offerID,
		)
		return nil, err
	}

	logger.Info("Get offer rating by user and offer successful", "id", rating.ID)
	return &rating, nil
}

// Update updates the mutable fields (rating, review) of an active offer rating.
// created_at is never mutated. Soft-deleted records are not reactivated by this method.
// The DB trigger owns updated_at; the explicit SET is defensive and idempotent.
func (m *OfferRatingModel) Update(ctx context.Context, offerRating *OfferRating) error {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("UpdateOfferRating")

	if offerRating == nil {
		err := errors.New("offer rating is required")
		logger.Error("Validation failed", err)
		return err
	}
	if offerRating.ID == uuid.Nil {
		err := errors.New("id is required")
		logger.Error("Validation failed", err)
		return err
	}
	if offerRating.Rating < 1 || offerRating.Rating > 5 {
		err := errors.New("rating must be between 1 and 5")
		logger.Error("Validation failed", err)
		return err
	}

	offerRating.Review = normalizeOptionalReview(offerRating.Review)

	query := fmt.Sprintf(`
		UPDATE offer_ratings
		SET rating     = $1,
		    review     = $2,
		    updated_at = NOW()
		WHERE id = $3
		  AND deleted_at IS NULL
		RETURNING %s
	`, offerRatingSelectColumns)

	err := scanOfferRating(
		m.DB.QueryRow(ctx, query,
			offerRating.Rating,
			offerRating.Review,
			offerRating.ID,
		),
		offerRating,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			logger.Warn("Offer rating not found for update", "id", offerRating.ID)
			return ErrOfferRatingNotFound
		}
		logger.Error("Update offer rating failed", err, "id", offerRating.ID)
		return err
	}

	logger.Info("Update offer rating successful",
		"id", offerRating.ID,
		"rating", offerRating.Rating,
	)

	return nil
}

// SoftDelete marks an offer rating record as deleted without removing the row.
// This is the standard business-lifecycle removal path when soft delete is supported.
func (m *OfferRatingModel) SoftDelete(ctx context.Context, id uuid.UUID) error {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("SoftDeleteOfferRating")

	if id == uuid.Nil {
		err := errors.New("id is required")
		logger.Error("Validation failed", err)
		return err
	}

	var deletedAt time.Time
	err := m.DB.QueryRow(ctx, `
		UPDATE offer_ratings
		SET deleted_at = NOW(),
			updated_at = NOW()
		WHERE id = $1
		AND deleted_at IS NULL
		RETURNING deleted_at
	`, id).Scan(&deletedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			logger.Warn("Offer rating not found for soft delete", "id", id)
			return ErrOfferRatingNotFound
		}
		logger.Error("Soft delete offer rating failed", err, "id", id)
		return err
	}

	logger.Info("Soft delete offer rating successful", "id", id, "deleted_at", deletedAt)
	return nil
}

// Delete permanently removes an offer rating record from the database.
//
// This is an explicit administrative hard-delete path intended for purge,
// maintenance, or data-retention enforcement. Normal business-lifecycle
// removal must use SoftDelete instead.
//
// Returns ErrOfferRatingNotFound when no row matches the given id.
func (m *OfferRatingModel) Delete(ctx context.Context, id uuid.UUID) error {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("DeleteOfferRating")

	if id == uuid.Nil {
		err := errors.New("id is required")
		logger.Error("Validation failed", err)
		return err
	}

	var deletedID uuid.UUID
	err := m.DB.QueryRow(ctx, `
		DELETE FROM offer_ratings
		WHERE id = $1
		RETURNING id
	`, id).Scan(&deletedID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			logger.Warn("Offer rating not found for hard delete", "id", id)
			return ErrOfferRatingNotFound
		}
		logger.Error("Delete offer rating failed", err, "id", id)
		return err
	}

	logger.Info("Delete offer rating successful", "id", deletedID)
	return nil
}

// GetAverageRatingByOfferID returns the mean rating across all active ratings for
// the given offer. Returns 0 when no active ratings exist.
func (m *OfferRatingModel) GetAverageRatingByOfferID(ctx context.Context, offerID uuid.UUID) (float64, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("GetAverageRatingByOfferID")

	if offerID == uuid.Nil {
		err := errors.New("offer_id is required")
		logger.Error("Validation failed", err)
		return 0, err
	}

	query := `
		SELECT COALESCE(AVG(rating), 0)
		FROM offer_ratings
		WHERE offer_id = $1
		  AND deleted_at IS NULL
	`

	var avgRating float64
	if err := m.DB.QueryRow(ctx, query, offerID).Scan(&avgRating); err != nil {
		logger.Error("Get average rating failed", err, "offer_id", offerID)
		return 0, err
	}

	logger.Info("Get average rating successful", "offer_id", offerID, "average_rating", avgRating)
	return avgRating, nil
}

// GetRatingCountByOfferID returns the total number of active ratings for an offer.
func (m *OfferRatingModel) GetRatingCountByOfferID(ctx context.Context, offerID uuid.UUID) (int, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("GetRatingCountByOfferID")

	if offerID == uuid.Nil {
		err := errors.New("offer_id is required")
		logger.Error("Validation failed", err)
		return 0, err
	}

	query := `
		SELECT COUNT(*)
		FROM offer_ratings
		WHERE offer_id = $1
		  AND deleted_at IS NULL
	`

	var count int
	if err := m.DB.QueryRow(ctx, query, offerID).Scan(&count); err != nil {
		logger.Error("Get rating count failed", err, "offer_id", offerID)
		return 0, err
	}

	logger.Info("Get rating count successful", "offer_id", offerID, "count", count)
	return count, nil
}

// HasUserRatedOffer reports whether the user has an active rating for the offer.
func (m *OfferRatingModel) HasUserRatedOffer(ctx context.Context, offerID uuid.UUID, userID uuid.UUID) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("HasUserRatedOffer")

	if userID == uuid.Nil {
		err := errors.New("user_id is required")
		logger.Error("Validation failed", err)
		return false, err
	}
	if offerID == uuid.Nil {
		err := errors.New("offer_id is required")
		logger.Error("Validation failed", err)
		return false, err
	}

	query := `
		SELECT EXISTS (
			SELECT 1
			FROM offer_ratings
			WHERE offer_id = $1
			  AND user_id  = $2
			  AND deleted_at IS NULL
		)
	`

	var exists bool
	if err := m.DB.QueryRow(ctx, query, offerID, userID).Scan(&exists); err != nil {
		logger.Error("Check user-rated-offer failed", err,
			"user_id", userID,
			"offer_id", offerID,
		)
		return false, err
	}

	logger.Info("Check user-rated-offer successful",
		"user_id", userID,
		"offer_id", offerID,
		"exists", exists,
	)

	return exists, nil
}

// FlagReview hides a review from public display by soft-deleting the rating record.
// Moderation reasons belong to offer_flags and must be recorded separately via
// OfferFlagModel before or after calling this method.
func (m *OfferRatingModel) FlagReview(ctx context.Context, id uuid.UUID) error {
	return m.SoftDelete(ctx, id)
}

// ApproveReview restores a previously hidden or flagged review by clearing deleted_at.
// Returns ErrOfferRatingNotFound if the record does not exist or is already active.
func (m *OfferRatingModel) ApproveReview(ctx context.Context, id uuid.UUID) error {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("ApproveReview")

	if id == uuid.Nil {
		err := errors.New("id is required")
		logger.Error("Validation failed", err)
		return err
	}

	query := `
		UPDATE offer_ratings
		SET deleted_at = NULL,
		    updated_at = NOW()
		WHERE id = $1
		  AND deleted_at IS NOT NULL
	`

	result, err := m.DB.Exec(ctx, query, id)
	if err != nil {
		logger.Error("Approve review failed", err, "id", id)
		return err
	}
	if result.RowsAffected() == 0 {
		logger.Warn("Review not found for approval", "id", id)
		return ErrOfferRatingNotFound
	}

	logger.Info("Approve review successful", "id", id)
	return nil
}

// HideReview hides a review from public display by soft-deleting the rating record.
func (m *OfferRatingModel) HideReview(ctx context.Context, id uuid.UUID) error {
	return m.SoftDelete(ctx, id)
}

// RestoreReview restores a previously hidden review by clearing deleted_at.
func (m *OfferRatingModel) RestoreReview(ctx context.Context, id uuid.UUID) error {
	return m.ApproveReview(ctx, id)
}

// AutoFlagSpamReviews soft-deletes active reviews whose character length exceeds
// offerRatingSpamReviewLengthThreshold. The predicate runs entirely in the database;
// no review content is loaded into application memory.
//
// Richer spam detection (NLP, repeated-phrase analysis, velocity checks) belongs in
// a dedicated moderation service and must not be implemented in this data model.
func (m *OfferRatingModel) AutoFlagSpamReviews(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("AutoFlagSpamReviews")

	query := `
		UPDATE offer_ratings
		SET deleted_at = NOW(),
		    updated_at = NOW()
		WHERE deleted_at IS NULL
		  AND review IS NOT NULL
		  AND char_length(review) > $1
	`

	result, err := m.DB.Exec(ctx, query, offerRatingSpamReviewLengthThreshold)
	if err != nil {
		logger.Error("Auto flag spam reviews failed", err,
			"review_length_threshold", offerRatingSpamReviewLengthThreshold,
		)
		return err
	}

	logger.Info("Auto flag spam reviews successful",
		"review_length_threshold", offerRatingSpamReviewLengthThreshold,
		"flagged_count", result.RowsAffected(),
	)

	return nil
}

// ArchiveOldRatings soft-deletes all active ratings created before cutoffDate.
// cutoffDate is normalized to UTC before use.
func (m *OfferRatingModel) ArchiveOldRatings(ctx context.Context, cutoffDate time.Time) error {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("ArchiveOldRatings")

	if cutoffDate.IsZero() {
		err := errors.New("cutoff date is required")
		logger.Error("Validation failed", err)
		return err
	}

	cutoffDate = cutoffDate.UTC()

	query := `
		UPDATE offer_ratings
		SET deleted_at = NOW(),
		    updated_at = NOW()
		WHERE created_at < $1
		  AND deleted_at IS NULL
	`

	result, err := m.DB.Exec(ctx, query, cutoffDate)
	if err != nil {
		logger.Error("Archive old ratings failed", err, "cutoff_date", cutoffDate)
		return err
	}

	logger.Info("Archive old ratings successful",
		"cutoff_date", cutoffDate,
		"archived_count", result.RowsAffected(),
	)

	return nil
}

// GenerateRatingAnalyticsReport returns a histogram of active ratings keyed by
// rating value (1–5). All five buckets are always present in the returned map,
// defaulting to zero when no active ratings exist for that value.
func (m *OfferRatingModel) GenerateRatingAnalyticsReport(ctx context.Context) (map[int]int, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("GenerateRatingAnalyticsReport")

	query := `
		SELECT rating, COUNT(*)
		FROM offer_ratings
		WHERE deleted_at IS NULL
		GROUP BY rating
		ORDER BY rating
	`

	rows, err := m.DB.Query(ctx, query)
	if err != nil {
		logger.Error("Generate rating analytics report failed", err)
		return nil, err
	}
	defer rows.Close()

	report := map[int]int{1: 0, 2: 0, 3: 0, 4: 0, 5: 0}

	for rows.Next() {
		var rating, count int
		if err := rows.Scan(&rating, &count); err != nil {
			logger.Error("Scan rating analytics row failed", err)
			return nil, err
		}
		report[rating] = count
	}

	if err := rows.Err(); err != nil {
		logger.Error("Iterate rating analytics rows failed", err)
		return nil, err
	}

	logger.Info("Generate rating analytics report successful", "report", report)
	return report, nil
}