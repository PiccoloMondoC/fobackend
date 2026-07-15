// Package data provides models and database access methods for offer clicks and other entities.
//
// sdworkspace/sdbackend/internal/data/offer_clicks.go
//
// GTM:
//   Layer: 2.5 Catalog / Offer Domain
//   Release Class: DEFERRED
//   Reason:
//     Offer clicks are release-critical affiliate monetization infrastructure.
//     They record immutable outbound offer interactions, support affiliate
//     click tracking, merchant/offer analytics, and the minimum evidence trail
//     needed for v1 monetization behavior.
//
// SPINE Rule:
//   Keep compiling.
//   Keep production-ready.
//   Preserve immutable click-event semantics.
//   Preserve DB-owned id and clicked_at lifecycle behavior.
//   Preserve bounded read paths.
//   Preserve LogOfferClick as the canonical handler-layer entry point.
//   Block deployment if this file breaks build, click persistence,
//   affiliate tracking, offer analytics, or outbound monetization integrity.
package data

import (
	"context"
	"errors"
	"time"

	"github.com/PiccoloMondoC/sdworkspace/sdbackend/internal/logging"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const offerClickSelectColumns = "id, offer_id, user_id, ip_address, user_agent, referrer, clicked_at"

// OfferClick represents a single historical offer-click event.
type OfferClick struct {
	ID        uuid.UUID  `json:"id"                   db:"id"`
	OfferID   uuid.UUID  `json:"offer_id"             db:"offer_id"`
	UserID    *uuid.UUID `json:"user_id,omitempty"    db:"user_id"`    // Nullable authenticated user.
	IPAddress *string    `json:"ip_address,omitempty" db:"ip_address"` // Nullable canonical text IP address.
	UserAgent *string    `json:"user_agent,omitempty" db:"user_agent"` // Nullable user agent.
	Referrer  *string    `json:"referrer,omitempty"   db:"referrer"`   // Nullable referrer URL.
	ClickedAt time.Time  `json:"clicked_at"           db:"clicked_at"`
}

// OfferClickModel is the structure which holds the DB instance.
type OfferClickModel struct {
	DB     *pgxpool.Pool
	Logger *logging.Logger
}

// offerClickScan scans the current row into an OfferClick.
func offerClickScan(scanner interface {
	Scan(dest ...any) error
}) (*OfferClick, error) {
	var offerClick OfferClick

	err := scanner.Scan(
		&offerClick.ID,
		&offerClick.OfferID,
		&offerClick.UserID,
		&offerClick.IPAddress,
		&offerClick.UserAgent,
		&offerClick.Referrer,
		&offerClick.ClickedAt,
	)
	if err != nil {
		return nil, err
	}

	return &offerClick, nil
}

// Insert inserts a new immutable offer-click event into the database.
// The database owns id and clicked_at; both fields are populated on the
// provided struct via RETURNING after a successful insert.
func (m *OfferClickModel) Insert(ctx context.Context, offerClick *OfferClick) error {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("InsertOfferClick")

	if offerClick == nil {
		err := errors.New("offer_click is required")
		logger.Error("Validation failed", err)
		return err
	}

	if offerClick.OfferID == uuid.Nil {
		err := errors.New("offer_id is required")
		logger.Error("Validation failed", err)
		return err
	}

	query := `
		INSERT INTO offer_clicks (
			offer_id,
			user_id,
			ip_address,
			user_agent,
			referrer
		)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id, clicked_at
	`

	err := m.DB.QueryRow(
		ctx,
		query,
		offerClick.OfferID,
		offerClick.UserID,
		offerClick.IPAddress,
		offerClick.UserAgent,
		offerClick.Referrer,
	).Scan(
		&offerClick.ID,
		&offerClick.ClickedAt,
	)
	if err != nil {
		logger.Error("Insert offer click failed", err)
		return err
	}

	logger.Info("Insert offer click successful",
		"id", offerClick.ID,
		"offer_id", offerClick.OfferID,
	)

	return nil
}

// GetByID retrieves an offer click by its ID from the database.
// Returns ErrOfferClickNotFound when no row matches.
func (m *OfferClickModel) GetByID(ctx context.Context, id uuid.UUID) (*OfferClick, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("GetOfferClickByID")

	if id == uuid.Nil {
		err := errors.New("id is required")
		logger.Error("Validation failed", err)
		return nil, err
	}

	query := `
		SELECT ` + offerClickSelectColumns + `
		FROM offer_clicks
		WHERE id = $1
	`

	offerClick, err := offerClickScan(m.DB.QueryRow(ctx, query, id))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			logger.Warn("Offer click not found", "id", id)
			return nil, ErrOfferClickNotFound
		}
		logger.Error("Query failed", err)
		return nil, err
	}

	logger.Info("Retrieved offer click successfully",
		"id", offerClick.ID,
		"offer_id", offerClick.OfferID,
	)

	return offerClick, nil
}

// GetByOfferID retrieves a bounded page of offer clicks for a given offer ID,
// ordered by clicked_at DESC. limit must be between 1 and 100 inclusive.
// offset must be zero or greater.
func (m *OfferClickModel) GetByOfferID(ctx context.Context, offerID uuid.UUID, limit int, offset int) ([]*OfferClick, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("GetByOfferID")

	if offerID == uuid.Nil {
		err := errors.New("offer_id is required")
		logger.Error("Validation failed", err)
		return nil, err
	}

	if limit <= 0 {
		err := errors.New("limit must be greater than zero")
		logger.Error("Validation failed", err, "limit", limit)
		return nil, err
	}

	if limit > 100 {
		err := errors.New("limit must be less than or equal to 100")
		logger.Error("Validation failed", err, "limit", limit)
		return nil, err
	}

	if offset < 0 {
		err := errors.New("offset must be zero or greater")
		logger.Error("Validation failed", err, "offset", offset)
		return nil, err
	}

	query := `
		SELECT ` + offerClickSelectColumns + `
		FROM offer_clicks
		WHERE offer_id = $1
		ORDER BY clicked_at DESC
		LIMIT $2
		OFFSET $3
	`

	rows, err := m.DB.Query(ctx, query, offerID, limit, offset)
	if err != nil {
		logger.Error("Query offer clicks failed", err)
		return nil, err
	}
	defer rows.Close()

	var offerClicks []*OfferClick
	for rows.Next() {
		offerClick, err := offerClickScan(rows)
		if err != nil {
			logger.Error("Scanning offer click failed", err)
			return nil, err
		}
		offerClicks = append(offerClicks, offerClick)
	}

	if err = rows.Err(); err != nil {
		logger.Error("Rows iteration error", err)
		return nil, err
	}

	logger.Info(
		"Retrieved offer clicks successfully",
		"offer_id", offerID,
		"count", len(offerClicks),
		"limit", limit,
		"offset", offset,
	)

	return offerClicks, nil
}

// Update is intentionally prohibited because offer-click records are immutable
// historical events.
func (m *OfferClickModel) Update(ctx context.Context, offerClick *OfferClick) error {
	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("UpdateOfferClick")

	if offerClick == nil {
		err := errors.New("offer_click is required")
		logger.Error("Validation failed", err)
		return err
	}

	if offerClick.ID == uuid.Nil {
		err := errors.New("id is required for update")
		logger.Error("Validation failed", err)
		return err
	}

	logger.Warn("Offer click update rejected because click records are immutable", "id", offerClick.ID)
	return ErrOfferClickImmutable
}

// Delete permanently removes an offer click by ID.
//
// This is an explicit administrative cleanup operation, not a normal business
// mutation path. Offer-click records are immutable historical events and do
// not support SoftDelete semantics.
// Returns ErrOfferClickNotFound when no row matches.
func (m *OfferClickModel) Delete(ctx context.Context, id uuid.UUID) error {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("DeleteOfferClick")

	if id == uuid.Nil {
		err := errors.New("id is required")
		logger.Error("Validation failed", err)
		return err
	}

	var deletedID uuid.UUID
	err := m.DB.QueryRow(
		ctx,
		`DELETE FROM offer_clicks
		  WHERE id = $1
		  RETURNING id`,
		id,
	).Scan(&deletedID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			logger.Warn("Delete attempted on non-existent offer click", "id", id)
			return ErrOfferClickNotFound
		}
		logger.Error("Delete offer click failed", err)
		return err
	}

	logger.Info("Delete offer click successful", "id", deletedID)

	return nil
}

// DeleteByOfferID removes all offer clicks for a specific offer. This is an
// explicit administrative cleanup operation, not a normal business mutation path.
func (m *OfferClickModel) DeleteByOfferID(ctx context.Context, offerID uuid.UUID) error {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("DeleteByOfferID")

	if offerID == uuid.Nil {
		err := errors.New("offer_id is required")
		logger.Error("Validation failed", err)
		return err
	}

	query := `DELETE FROM offer_clicks WHERE offer_id = $1`

	result, err := m.DB.Exec(ctx, query, offerID)
	if err != nil {
		logger.Error("Delete offer clicks failed", err)
		return err
	}

	logger.Info("Delete offer clicks successful", "offer_id", offerID, "rows_affected", result.RowsAffected())

	return nil
}

// CountByOfferID returns the total number of clicks recorded for an offer.
// Returns int64 to match the PostgreSQL COUNT(*) bigint contract.
func (m *OfferClickModel) CountByOfferID(ctx context.Context, offerID uuid.UUID) (int64, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("CountByOfferID")

	if offerID == uuid.Nil {
		err := errors.New("offer_id is required")
		logger.Error("Validation failed", err)
		return 0, err
	}

	query := `SELECT COUNT(*) FROM offer_clicks WHERE offer_id = $1`

	var count int64
	err := m.DB.QueryRow(ctx, query, offerID).Scan(&count)
	if err != nil {
		logger.Error("Count query failed", err)
		return 0, err
	}

	logger.Info("Count query successful", "offer_id", offerID, "count", count)

	return count, nil
}

// GetRecentClicks retrieves the most recent offer clicks up to limit for analytics use.
// limit must be between 1 and 100 inclusive.
func (m *OfferClickModel) GetRecentClicks(ctx context.Context, limit int) ([]*OfferClick, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("GetRecentClicks")

	if limit <= 0 {
		err := errors.New("limit must be greater than zero")
		logger.Error("Validation failed", err, "limit", limit)
		return nil, err
	}

	if limit > 100 {
		err := errors.New("limit must be less than or equal to 100")
		logger.Error("Validation failed", err, "limit", limit)
		return nil, err
	}

	query := `
		SELECT ` + offerClickSelectColumns + `
		FROM offer_clicks
		ORDER BY clicked_at DESC
		LIMIT $1
	`

	rows, err := m.DB.Query(ctx, query, limit)
	if err != nil {
		logger.Error("Query execution failed", err)
		return nil, err
	}
	defer rows.Close()

	var offerClicks []*OfferClick
	for rows.Next() {
		offerClick, err := offerClickScan(rows)
		if err != nil {
			logger.Error("Failed to scan row", err)
			return nil, err
		}
		offerClicks = append(offerClicks, offerClick)
	}

	if err := rows.Err(); err != nil {
		logger.Error("Row iteration error", err)
		return nil, err
	}

	logger.Info("Retrieved recent offer clicks", "count", len(offerClicks))

	return offerClicks, nil
}

// GetByUserID retrieves a bounded page of offer clicks for a given user ID,
// ordered by clicked_at DESC. limit must be between 1 and 100 inclusive.
// offset must be zero or greater.
// Uses idx_offer_clicks_user_id for efficient user-scoped click history queries.
func (m *OfferClickModel) GetByUserID(ctx context.Context, userID uuid.UUID, limit int, offset int) ([]*OfferClick, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("GetByUserID")

	if userID == uuid.Nil {
		err := errors.New("user_id is required")
		logger.Error("Validation failed", err)
		return nil, err
	}

	if limit <= 0 {
		err := errors.New("limit must be greater than zero")
		logger.Error("Validation failed", err, "limit", limit)
		return nil, err
	}

	if limit > 100 {
		err := errors.New("limit must be less than or equal to 100")
		logger.Error("Validation failed", err, "limit", limit)
		return nil, err
	}

	if offset < 0 {
		err := errors.New("offset must be zero or greater")
		logger.Error("Validation failed", err, "offset", offset)
		return nil, err
	}

	query := `
		SELECT ` + offerClickSelectColumns + `
		FROM offer_clicks
		WHERE user_id = $1
		ORDER BY clicked_at DESC
		LIMIT $2
		OFFSET $3
	`

	rows, err := m.DB.Query(ctx, query, userID, limit, offset)
	if err != nil {
		logger.Error("Query user offer clicks failed", err)
		return nil, err
	}
	defer rows.Close()

	var offerClicks []*OfferClick
	for rows.Next() {
		offerClick, err := offerClickScan(rows)
		if err != nil {
			logger.Error("Scanning user offer click failed", err)
			return nil, err
		}
		offerClicks = append(offerClicks, offerClick)
	}

	if err = rows.Err(); err != nil {
		logger.Error("Rows iteration error", err)
		return nil, err
	}

	logger.Info(
		"Retrieved user offer clicks successfully",
		"user_id", userID,
		"count", len(offerClicks),
		"limit", limit,
		"offset", offset,
	)

	return offerClicks, nil
}

// LogOfferClick records a user interaction with an offer link. It is the
// canonical entry point for click-tracking at the handler layer and delegates
// to Insert for all persistence logic.
func (m *OfferClickModel) LogOfferClick(ctx context.Context, offerID uuid.UUID, userID *uuid.UUID, ipAddress *string, userAgent *string, referrer *string) error {
	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("LogOfferClick")

	if offerID == uuid.Nil {
		err := errors.New("offer_id is required")
		logger.Error("Validation failed", err)
		return err
	}

	offerClick := &OfferClick{
		OfferID:   offerID,
		UserID:    userID,
		IPAddress: ipAddress,
		UserAgent: userAgent,
		Referrer:  referrer,
	}

	if err := m.Insert(ctx, offerClick); err != nil {
		logger.Error("Log offer click failed", err)
		return err
	}

	return nil
}