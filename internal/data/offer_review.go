// Package data provides models and database access methods for offers and other entities.
//
// File: sdworkspace/sdbackend/internal/data/offer_review.go
//
// GTM:
//   Layer: 2.5 Catalog / Offer Domain
//   Release Class: SPINE
//   Reason:
//     Offer review and publication workflow is release-critical catalog
//     governance infrastructure. It controls status transitions, editorial
//     approval, publishing, unpublishing, and expiration behavior over the
//     canonical offers table used by the public catalog.
//
// SPINE Rule:
//   Keep compiling.
//   Keep production-ready.
//   Preserve OfferModel ownership.
//   Preserve canonical offers table dependency.
//   Preserve offer status lookup behavior.
//   Preserve editorial approval and publication semantics.
//   Preserve DB-owned lifecycle timestamp behavior.
//   Block deployment if this file breaks build, offer review,
//   publication workflow, expiration handling, or catalog governance integrity.
package data

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func getOfferStatusIDByName(ctx context.Context, db Querier, name string) (uuid.UUID, error) {
	var id uuid.UUID

	err := db.QueryRow(
		ctx,
		`SELECT id FROM offer_statuses WHERE name = $1 AND is_active = TRUE LIMIT 1`,
		name,
	).Scan(&id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return uuid.Nil, fmt.Errorf("%w: %q", ErrOfferStatusNotFound, name)
		}
		return uuid.Nil, fmt.Errorf("resolve offer status %q: %w", name, err)
	}

	return id, nil
}

// Querier allows getOfferStatusIDByName to work with pool or tx.
type Querier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// SetOfferStatus updates the offer status only.
// Approval/rejection semantics remain explicit methods below.
func (m *OfferModel) SetOfferStatus(ctx context.Context, offerID, statusID uuid.UUID) error {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	log := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("SetOfferStatus")

	if offerID == uuid.Nil {
		err := errors.New("offer ID is required")
		log.Error("Validation failed", "error", err)
		return err
	}
	if statusID == uuid.Nil {
		err := errors.New("status ID is required")
		log.Error("Validation failed", "error", err)
		return err
	}

	const q = `
		UPDATE offers
		SET status_id = $1,
			updated_at = NOW()
		WHERE id = $2
		  AND deleted_at IS NULL
	`

	res, err := m.DB.Exec(ctx, q, statusID, offerID)
	if err != nil {
		log.Error("Set offer status failed", "error", err, "offer_id", offerID, "status_id", statusID)
		return fmt.Errorf("set offer status: %w", err)
	}
	if res.RowsAffected() == 0 {
		log.Warn("Offer not found for status update", "offer_id", offerID, "status_id", statusID)
		return ErrOfferReviewNotFound
	}

	log.Info("Offer status updated", "offer_id", offerID, "status_id", statusID)
	return nil
}

// ApproveOffer performs canonical approval using the existing reviewed schema.
func (m *OfferModel) ApproveOffer(ctx context.Context, offerID uuid.UUID) error {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	log := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("ApproveOffer")

	if offerID == uuid.Nil {
		err := errors.New("offer ID is required")
		log.Error("Validation failed", "error", err)
		return err
	}

	statusID, err := getOfferStatusIDByName(ctx, m.DB, "approved")
	if err != nil {
		log.Error("Resolve approved status failed", "error", err)
		return err
	}

	const q = `
		UPDATE offers
		SET status_id = $1,
			is_editorial_approved = TRUE,
			is_active = TRUE,
			published_at = COALESCE(published_at, NOW()),
			updated_at = NOW()
		WHERE id = $2
		  AND deleted_at IS NULL
	`

	res, err := m.DB.Exec(ctx, q, statusID, offerID)
	if err != nil {
		log.Error("Approve offer failed", "error", err, "offer_id", offerID, "status_id", statusID)
		return fmt.Errorf("approve offer: %w", err)
	}
	if res.RowsAffected() == 0 {
		log.Warn("Offer not found for approval", "offer_id", offerID)
		return ErrOfferReviewNotFound
	}

	log.Info("Offer approved", "offer_id", offerID, "status_id", statusID)
	return nil
}

// RejectOffer performs canonical rejection using status, approval state,
// publication state, and activation state only.
func (m *OfferModel) RejectOffer(ctx context.Context, offerID uuid.UUID, statusID uuid.UUID) error {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	log := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("RejectOffer")

	if offerID == uuid.Nil {
		err := errors.New("offer ID is required")
		log.Error("Validation failed", "error", err)
		return err
	}
	if statusID == uuid.Nil {
		err := errors.New("status ID is required")
		log.Error("Validation failed", "error", err)
		return err
	}

	const q = `
		UPDATE offers
		SET status_id = $1,
			is_editorial_approved = FALSE,
			is_active = FALSE,
			published_at = NULL,
			updated_at = NOW()
		WHERE id = $2
		  AND deleted_at IS NULL
	`

	res, err := m.DB.Exec(ctx, q, statusID, offerID)
	if err != nil {
		log.Error("Reject offer failed", "error", err, "offer_id", offerID, "status_id", statusID)
		return fmt.Errorf("reject offer: %w", err)
	}
	if res.RowsAffected() == 0 {
		log.Warn("Offer not found for rejection", "offer_id", offerID, "status_id", statusID)
		return ErrOfferReviewNotFound
	}

	log.Info("Offer rejected", "offer_id", offerID, "status_id", statusID)
	return nil
}

// GetExpiredOffers returns non-deleted offers whose expires_at is before cutoff.
func (m *OfferModel) GetExpiredOffers(ctx context.Context, cutoff time.Time, limit, offset int) ([]*Offer, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	log := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("GetExpiredOffers")

	limit, offset = defaultPage(limit, offset)
	cutoff = cutoff.UTC()

	q := fmt.Sprintf(`
		SELECT %s
		FROM offers
		WHERE deleted_at IS NULL
		  AND expires_at IS NOT NULL
		  AND expires_at < $1
		ORDER BY expires_at ASC
		LIMIT $2 OFFSET $3
	`, offerSelectColumns)

	rows, err := m.DB.Query(ctx, q, cutoff, limit, offset)
	if err != nil {
		log.Error("Query execution failed", "error", err, "cutoff", cutoff, "limit", limit, "offset", offset)
		return nil, fmt.Errorf("get expired offers: %w", err)
	}

	offers, err := collectOffers(rows)
	if err != nil {
		log.Error("Row collection failed", "error", err, "cutoff", cutoff, "limit", limit, "offset", offset)
		return nil, fmt.Errorf("collect expired offers: %w", err)
	}

	log.Info("Retrieved expired offers", "count", len(offers), "cutoff", cutoff, "limit", limit, "offset", offset)
	return offers, nil
}

// AutoExpireOffersWithContext deactivates expired offers and moves them into
// the seeded expired status using a caller-supplied context.
//
// This is the canonical context-aware method for scheduler/service callers.
func (m *OfferModel) AutoExpireOffersWithContext(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}

	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	log := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("AutoExpireOffersWithContext")

	expiredStatusID, err := getOfferStatusIDByName(ctx, m.DB, "expired")
	if err != nil {
		log.Error("Resolve expired status failed", "error", err)
		return err
	}

	const q = `
		UPDATE offers
		SET status_id = $1,
			is_active = FALSE,
			updated_at = NOW()
		WHERE deleted_at IS NULL
		  AND is_active = TRUE
		  AND expires_at IS NOT NULL
		  AND expires_at <= NOW()
	`

	res, err := m.DB.Exec(ctx, q, expiredStatusID)
	if err != nil {
		log.Error("Auto expire offers failed", "error", err, "status_id", expiredStatusID)
		return fmt.Errorf("auto expire offers: %w", err)
	}

	log.Info("Auto expire offers completed", "status_id", expiredStatusID, "rows_affected", res.RowsAffected())
	return nil
}

// AutoExpireOffers is a backward-compatible wrapper for existing callers that
// do not yet pass a context explicitly.
//
// New code should prefer AutoExpireOffersWithContext.
func (m *OfferModel) AutoExpireOffers() error {
	return m.AutoExpireOffersWithContext(context.Background())
}

// PublishOffer makes a reviewed offer visible on the public catalog surface.
//
// Time-source rule:
// published_at and updated_at are persisted lifecycle timestamps and must be DB-owned.
func (m *OfferModel) PublishOffer(ctx context.Context, offerID uuid.UUID) error {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	log := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("PublishOffer")

	if offerID == uuid.Nil {
		err := errors.New("offer ID is required")
		log.Error("Validation failed", "error", err)
		return err
	}

	const q = `
		UPDATE offers
		SET published_at = COALESCE(published_at, NOW()),
			is_active = TRUE,
			updated_at = NOW()
		WHERE id = $1
		  AND deleted_at IS NULL
		  AND is_editorial_approved = TRUE
	`

	res, err := m.DB.Exec(ctx, q, offerID)
	if err != nil {
		log.Error("Publish offer failed", "error", err, "offer_id", offerID)
		return fmt.Errorf("publish offer: %w", err)
	}
	if res.RowsAffected() == 0 {
		log.Warn("Offer not found or not eligible for publish", "offer_id", offerID)
		return ErrOfferReviewNotFound
	}

	log.Info("Offer published", "offer_id", offerID)
	return nil
}

// UnpublishOffer removes an offer from the public catalog surface without deleting it.
func (m *OfferModel) UnpublishOffer(ctx context.Context, offerID uuid.UUID) error {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	log := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("UnpublishOffer")

	if offerID == uuid.Nil {
		err := errors.New("offer ID is required")
		log.Error("Validation failed", "error", err)
		return err
	}

	const q = `
		UPDATE offers
		SET published_at = NULL,
			is_active = FALSE,
			updated_at = NOW()
		WHERE id = $1
		  AND deleted_at IS NULL
	`

	res, err := m.DB.Exec(ctx, q, offerID)
	if err != nil {
		log.Error("Unpublish offer failed", "error", err, "offer_id", offerID)
		return fmt.Errorf("unpublish offer: %w", err)
	}
	if res.RowsAffected() == 0 {
		log.Warn("Offer not found for unpublish", "offer_id", offerID)
		return ErrOfferReviewNotFound
	}

	log.Info("Offer unpublished", "offer_id", offerID)
	return nil
}