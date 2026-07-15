// Package data provides models and database access methods for offers and other entities.
//
// File: sdworkspace/sdbackend/internal/data/offer_flags.go
//
// GTM:
//   Layer: 2.5 Catalog / Offer Domain
//   Release Class: DEFERRED
//   Reason:
//     Offer flags are release-critical moderation and catalog-quality
//     infrastructure. They support user/internal flagging, unresolved-flag
//     review queues, price-anomaly moderation, and public catalog integrity over
//     the canonical offers table.
//
// SPINE Rule:
//   Keep compiling.
//   Keep production-ready.
//   Preserve OfferFlagModel ownership.
//   Preserve canonical offers table dependency.
//   Preserve unresolved flag workflow.
//   Preserve resolution status semantics.
//   Preserve DB-owned lifecycle timestamp behavior.
//   Block deployment if this file breaks build, offer moderation,
//   flag persistence, anomaly flagging, or catalog-quality integrity.
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

// OfferFlag represents a full record in offer_flags.
type OfferFlag struct {
	ID               uuid.UUID  `json:"id" db:"id"`
	OfferID          uuid.UUID  `json:"offer_id" db:"offer_id"`
	Reason           string     `json:"reason" db:"reason"`
	FlaggedBy        *uuid.UUID `json:"flagged_by,omitempty" db:"flagged_by"`
	CreatedAt        time.Time  `json:"created_at" db:"created_at"`
	ResolvedAt       *time.Time `json:"resolved_at,omitempty" db:"resolved_at"`
	ResolvedBy       *uuid.UUID `json:"resolved_by,omitempty" db:"resolved_by"`
	ResolutionNotes  *string    `json:"resolution_notes,omitempty" db:"resolution_notes"`
	ResolutionStatus *string    `json:"resolution_status,omitempty" db:"resolution_status"`
}

// OfferFlagSummary provides unresolved flag context attached to an offer view.
type OfferFlagSummary struct {
	ID        uuid.UUID  `json:"id"`
	Reason    string     `json:"reason"`
	FlaggedBy *uuid.UUID `json:"flagged_by,omitempty"`
	FlaggedAt time.Time  `json:"flagged_at"`
}

// FlaggedOffer is an internal/admin moderation view.
type FlaggedOffer struct {
	Offer       *Offer            `json:"offer"`
	FlagSummary *OfferFlagSummary `json:"flag_summary"`
}

// OfferFlagModel holds the DB instance for offer flag workflows.
type OfferFlagModel struct {
	DB     *pgxpool.Pool
	Logger *logging.Logger
}

// normalizeOfferFlagResolutionStatus canonicalizes resolution_status and
// validates it against the reviewed offer_flags check constraint.
//
// Nil remains nil. Blank becomes nil.
func normalizeOfferFlagResolutionStatus(v *string) (*string, error) {
	if v == nil {
		return nil, nil
	}

	s := strings.TrimSpace(strings.ToLower(*v))
	if s == "" {
		return nil, nil
	}

	switch s {
	case "pending", "resolved", "dismissed":
		return &s, nil
	default:
		return nil, errors.New("resolution_status must be one of pending, resolved, or dismissed")
	}
}

// collectFlaggedOffers consumes a pgx rows result into []*FlaggedOffer using the
// canonical offer scan destination contract plus the unresolved-flag summary
// columns appended after it.
func collectFlaggedOffers(rows pgx.Rows) ([]*FlaggedOffer, error) {
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (*FlaggedOffer, error) {
		var offer Offer
		var summary OfferFlagSummary

		dest := append(
			offerScanDestinations(&offer),
			&summary.ID,
			&summary.Reason,
			&summary.FlaggedBy,
			&summary.FlaggedAt,
		)

		if err := row.Scan(dest...); err != nil {
			return nil, err
		}

		return &FlaggedOffer{
			Offer:       &offer,
			FlagSummary: &summary,
		}, nil
	})
}

// collectUUIDs consumes a pgx rows result into []uuid.UUID.
func collectUUIDs(rows pgx.Rows) ([]uuid.UUID, error) {
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (uuid.UUID, error) {
		var id uuid.UUID
		if err := row.Scan(&id); err != nil {
			return uuid.Nil, err
		}
		return id, nil
	})
}

// Insert inserts a new offer flag.
//
// Time-source rule:
// created_at is DB-owned in the reviewed schema and is therefore returned from
// the database rather than written from application time.
func (m *OfferFlagModel) Insert(ctx context.Context, flag *OfferFlag) error {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	log := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("InsertOfferFlag")

	if flag == nil {
		err := errors.New("offer flag payload is required")
		log.Error("Validation failed", err)
		return err
	}
	if flag.OfferID == uuid.Nil {
		err := errors.New("offer_id is required")
		log.Error("Validation failed", err)
		return err
	}

	flag.Reason = strings.TrimSpace(flag.Reason)
	if flag.Reason == "" {
		err := errors.New("flag reason is required")
		log.Error("Validation failed", err)
		return err
	}

	normalizedStatus, err := normalizeOfferFlagResolutionStatus(flag.ResolutionStatus)
	if err != nil {
		log.Error("Validation failed", err)
		return err
	}
	flag.ResolutionStatus = normalizedStatus
	flag.ResolutionNotes = normalizeOptionalString(flag.ResolutionNotes)

	if flag.ID == uuid.Nil {
		flag.ID = uuid.New()
	}

	const q = `
		INSERT INTO offer_flags (
			id,
			offer_id,
			reason,
			flagged_by,
			resolved_at,
			resolved_by,
			resolution_notes,
			resolution_status
		) VALUES (
			$1,$2,$3,$4,$5,$6,$7,$8
		)
		RETURNING created_at
	`

	if err := m.DB.QueryRow(
		ctx,
		q,
		flag.ID,
		flag.OfferID,
		flag.Reason,
		flag.FlaggedBy,
		flag.ResolvedAt,
		flag.ResolvedBy,
		flag.ResolutionNotes,
		flag.ResolutionStatus,
	).Scan(&flag.CreatedAt); err != nil {
		log.Error("Insert offer flag failed", err, "offer_id", flag.OfferID)
		return fmt.Errorf("insert offer flag: %w", err)
	}

	log.Info("Insert offer flag successful", "offer_flag_id", flag.ID, "offer_id", flag.OfferID)
	return nil
}

// FlagOffer records a new flag event for an offer.
func (m *OfferFlagModel) FlagOffer(ctx context.Context, offerID uuid.UUID, reason string, userID *uuid.UUID) error {
	flag := &OfferFlag{
		OfferID:   offerID,
		Reason:    reason,
		FlaggedBy: userID,
	}
	return m.Insert(ctx, flag)
}

// FlagAnomalous inserts a deduplicated unresolved "price anomaly" flag.
// This belongs on OfferFlagModel because it is a moderation record, not canonical offer CRUD.
//
// Time-source rule:
// created_at is DB-owned by the reviewed offer_flags schema.
func (m *OfferFlagModel) FlagAnomalous(ctx context.Context, offerID uuid.UUID, isAnomalous bool) error {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	log := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("FlagAnomalousOffer")

	if offerID == uuid.Nil {
		err := errors.New("offer ID is required")
		log.Error("Validation failed", err)
		return err
	}
	if !isAnomalous {
		log.Info("No anomaly detected; skipping flag insertion", "offer_id", offerID)
		return nil
	}

	const reason = "price anomaly"

	const q = `
		INSERT INTO offer_flags (
			id,
			offer_id,
			reason,
			resolution_status
		)
		SELECT
			$1,
			$2,
			$3,
			'pending'
		WHERE NOT EXISTS (
			SELECT 1
			FROM offer_flags
			WHERE offer_id = $2
			  AND reason = $3
			  AND resolved_at IS NULL
		)
	`

	res, err := m.DB.Exec(ctx, q, uuid.New(), offerID, reason)
	if err != nil {
		log.Error("Flag anomalous offer failed", err, "offer_id", offerID)
		return fmt.Errorf("flag anomalous offer: %w", err)
	}

	if res.RowsAffected() == 0 {
		log.Info("Existing unresolved anomaly flag already present", "offer_id", offerID, "reason", reason)
		return nil
	}

	log.Warn("Offer flagged for price anomaly", "offer_id", offerID, "reason", reason)
	return nil
}

// ResolveFlag resolves or dismisses a flag.
//
// Time-source rule:
// resolved_at is canonical stored lifecycle state and must therefore be DB-owned.
func (m *OfferFlagModel) ResolveFlag(ctx context.Context, flagID uuid.UUID, resolvedBy *uuid.UUID, resolutionStatus string, resolutionNotes *string) error {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	log := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("ResolveOfferFlag")

	if flagID == uuid.Nil {
		err := errors.New("flag ID is required")
		log.Error("Validation failed", err)
		return err
	}

	normalizedStatus, err := normalizeOfferFlagResolutionStatus(&resolutionStatus)
	if err != nil {
		log.Error("Validation failed", err)
		return err
	}
	if normalizedStatus == nil || (*normalizedStatus != "resolved" && *normalizedStatus != "dismissed") {
		err := errors.New("resolution_status must be 'resolved' or 'dismissed' when resolving a flag")
		log.Error("Validation failed", err)
		return err
	}

	resolutionNotes = normalizeOptionalString(resolutionNotes)

	var resolvedAt time.Time

	const q = `
		UPDATE offer_flags
		SET resolved_at = NOW(),
			resolved_by = $1,
			resolution_notes = $2,
			resolution_status = $3
		WHERE id = $4
		  AND resolved_at IS NULL
		RETURNING resolved_at
	`

	err = m.DB.QueryRow(ctx, q, resolvedBy, resolutionNotes, *normalizedStatus, flagID).Scan(&resolvedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			log.Warn("Offer flag not found or already resolved", "flag_id", flagID)
			return ErrOfferFlagNotFound
		}
		log.Error("Resolve offer flag failed", err, "flag_id", flagID)
		return fmt.Errorf("resolve offer flag: %w", err)
	}

	log.Info("Resolve offer flag successful", "flag_id", flagID, "resolution_status", *normalizedStatus, "resolved_at", resolvedAt)
	return nil
}

// GetFlaggedOffers returns unresolved flagged offers for internal/admin review.
//
// Standard read behavior excludes deleted offers by default. This is an
// internal/admin moderation surface, not an audit/history recovery surface.
func (m *OfferFlagModel) GetFlaggedOffers(ctx context.Context, limit, offset int) ([]*FlaggedOffer, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	log := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("GetFlaggedOffers")

	limit, offset = defaultPage(limit, offset)

	q := fmt.Sprintf(`
		SELECT
			%s,
			f.id,
			f.reason,
			f.flagged_by,
			f.created_at
		FROM offers o
		INNER JOIN offer_flags f ON f.offer_id = o.id
		WHERE o.deleted_at IS NULL
		  AND f.resolved_at IS NULL
		ORDER BY f.created_at DESC
		LIMIT $1 OFFSET $2
	`, offerSelectColumns)

	rows, err := m.DB.Query(ctx, q, limit, offset)
	if err != nil {
		log.Error("Query execution failed", err, "limit", limit, "offset", offset)
		return nil, fmt.Errorf("get flagged offers: %w", err)
	}
	defer rows.Close()

	out, err := collectFlaggedOffers(rows)
	if err != nil {
		log.Error("Row collection failed", err, "limit", limit, "offset", offset)
		return nil, fmt.Errorf("collect flagged offers: %w", err)
	}

	log.Info("Retrieved flagged offers", "count", len(out), "limit", limit, "offset", offset)
	return out, nil
}

// GetAutoFlagCandidates returns offers missing any unresolved flag.
// This keeps the automation query separate from canonical offer CRUD.
func (m *OfferFlagModel) GetAutoFlagCandidates(ctx context.Context, limit int) ([]uuid.UUID, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	log := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("GetAutoFlagCandidates")

	if limit <= 0 {
		limit = 100
	}
	if limit > 500 {
		limit = 500
	}

	const q = `
		SELECT o.id
		FROM offers o
		WHERE o.deleted_at IS NULL
		  AND EXISTS (
			SELECT 1
			FROM offer_price_history ph
			WHERE ph.offer_id = o.id
		  )
		  AND NOT EXISTS (
			SELECT 1
			FROM offer_flags f
			WHERE f.offer_id = o.id
			  AND f.resolved_at IS NULL
		  )
		ORDER BY o.updated_at DESC
		LIMIT $1
	`

	rows, err := m.DB.Query(ctx, q, limit)
	if err != nil {
		log.Error("Query execution failed", err, "limit", limit)
		return nil, fmt.Errorf("get auto flag candidates: %w", err)
	}
	defer rows.Close()

	ids, err := collectUUIDs(rows)
	if err != nil {
		log.Error("Row collection failed", err, "limit", limit)
		return nil, fmt.Errorf("collect auto flag candidates: %w", err)
	}

	log.Info("Retrieved auto-flag candidates", "count", len(ids), "limit", limit)
	return ids, nil
}