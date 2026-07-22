// Package data provides models and database access methods for affiliate performance and other entities.
//
// sdworkspace/sdbackend/internal/data/affiliate_performance.go
//
// GTM:
//
//	Layer: 2.4 Merchant / Affiliate Domain
//	Release Class: DEFERRED
//	Reason:
//	  Affiliate performance is valid post-release analytics/reporting infrastructure,
//	  but it is not required for the initial Platform release spine. The v1
//	  spine only requires offer publication, affiliate click tracking, and safe
//	  public offer behavior. Do not expand this file until the release spine is
//	  functionally complete.
//
// DEFERRED Rule:
//
//	Keep compiling.
//	Keep safe.
//	Do not add new features.
//	Do not route into v1 UI/API expansion.
//	Do not block deployment on this file unless it breaks the build.
package data

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/PiccoloMondoC/sdworkspace/sdbackend/internal/logging"

	// Timestamps are intentionally DB-owned in this file.
	// created_at comes from DEFAULT NOW(), and updated_at is maintained by
	// the set_updated_at() trigger. We read both back via RETURNING, so
	// importing timeutil here would be redundant.

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// AffiliatePerformance represents the persisted aggregate performance metrics for a single offer.
//
// Engineering notes:
//   - Offer-first in alignment with Backend Engineer Guidance V1.3 §2.8.
//   - Decimal business values (money, ratios) are modeled as strings to preserve
//     fixed-point precision without floating-point error (BEG §12).
//   - created_at and updated_at are DB-owned. The set_updated_at() trigger
//     maintains updated_at automatically; the application must not override them.
//   - There is at most one AffiliatePerformance row per offer, enforced by a
//     UNIQUE constraint on offer_id in the affiliate_performance table.
type AffiliatePerformance struct {
	ID               uuid.UUID `json:"id"                db:"id"`
	OfferID          uuid.UUID `json:"offer_id"          db:"offer_id"`
	TotalClicks      int       `json:"total_clicks"      db:"total_clicks"`
	EstimatedRevenue string    `json:"estimated_revenue" db:"estimated_revenue"`
	ConversionRate   string    `json:"conversion_rate"   db:"conversion_rate"`
	AvgOrderValue    string    `json:"avg_order_value"   db:"avg_order_value"`
	CreatedAt        time.Time `json:"created_at"        db:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"        db:"updated_at"`
}

// AffiliatePerformanceModel holds the DB pool and logger for affiliate performance operations.
type AffiliatePerformanceModel struct {
	DB     *pgxpool.Pool
	Logger *logging.Logger
}

// validateAffiliatePerformance runs full pre-persistence validation (BEG §21).
//
// Decimal fields are validated via the centralized validateNonNegativeDecimalString
// from commons.go, which enforces syntactic validity and non-negativity without
// introducing floating-point precision error into the validation path (BEG §3.22, §12).
func validateAffiliatePerformance(p *AffiliatePerformance) error {
	if p == nil {
		return errors.New("performance is required")
	}
	if p.OfferID == uuid.Nil {
		return errors.New("offer_id is required")
	}
	if p.TotalClicks < 0 {
		return errors.New("total_clicks cannot be negative")
	}
	if err := validateNonNegativeDecimalString(p.EstimatedRevenue, "estimated_revenue"); err != nil {
		return err
	}
	if err := validateNonNegativeDecimalString(p.ConversionRate, "conversion_rate"); err != nil {
		return err
	}
	if err := validateNonNegativeDecimalString(p.AvgOrderValue, "avg_order_value"); err != nil {
		return err
	}
	return nil
}

// Insert upserts an affiliate performance record by offer_id.
//
// Contract:
//   - There must be at most one affiliate_performance row per offer.
//   - The database enforces this via a UNIQUE constraint on offer_id.
//   - On conflict the existing row is updated in place; the original created_at
//     is preserved by the database and returned via RETURNING.
//   - created_at and updated_at are database-owned lifecycle fields.
//     Any caller-provided values are not authoritative; the database
//     sets and returns the canonical timestamps.
func (m *AffiliatePerformanceModel) Insert(ctx context.Context, performance *AffiliatePerformance) error {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("InsertAffiliatePerformance")

	if err := validateAffiliatePerformance(performance); err != nil {
		logger.Error("Validation failed", err)
		return err
	}

	if performance.ID == uuid.Nil {
		performance.ID = uuid.New()
	}

	err := m.DB.QueryRow(ctx, `
		INSERT INTO affiliate_performance
			(id, offer_id, total_clicks, estimated_revenue, conversion_rate, avg_order_value)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (offer_id) DO UPDATE SET
			total_clicks      = EXCLUDED.total_clicks,
			estimated_revenue = EXCLUDED.estimated_revenue,
			conversion_rate   = EXCLUDED.conversion_rate,
			avg_order_value   = EXCLUDED.avg_order_value
		RETURNING created_at, updated_at
	`,
		performance.ID,
		performance.OfferID,
		performance.TotalClicks,
		performance.EstimatedRevenue,
		performance.ConversionRate,
		performance.AvgOrderValue,
	).Scan(&performance.CreatedAt, &performance.UpdatedAt)

	if err != nil {
		logger.Error("Upsert affiliate performance failed", err)
		return err
	}

	logger.Info("Upsert affiliate performance successful",
		"id", performance.ID,
		"offer_id", performance.OfferID,
		"total_clicks", performance.TotalClicks,
		"estimated_revenue", performance.EstimatedRevenue,
		"conversion_rate", performance.ConversionRate,
		"avg_order_value", performance.AvgOrderValue,
		"created_at", performance.CreatedAt,
		"updated_at", performance.UpdatedAt,
	)

	return nil
}

// GetByID retrieves an affiliate performance record by its primary key.
// Returns nil, nil when no row is found.
func (m *AffiliatePerformanceModel) GetByID(ctx context.Context, id uuid.UUID) (*AffiliatePerformance, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("GetByIDAffiliatePerformance")

	if id == uuid.Nil {
		err := errors.New("id is required")
		logger.Error("Validation failed", err)
		return nil, err
	}

	var p AffiliatePerformance
	err := m.DB.QueryRow(ctx, `
		SELECT id, offer_id, total_clicks, estimated_revenue, conversion_rate, avg_order_value, created_at, updated_at
		FROM affiliate_performance
		WHERE id = $1
	`, id).Scan(
		&p.ID,
		&p.OfferID,
		&p.TotalClicks,
		&p.EstimatedRevenue,
		&p.ConversionRate,
		&p.AvgOrderValue,
		&p.CreatedAt,
		&p.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			logger.Warn("Affiliate performance record not found", "id", id)
			return nil, nil
		}
		logger.Error("Query affiliate performance record failed", err)
		return nil, err
	}

	logger.Info("GetByID affiliate performance successful",
		"id", p.ID,
		"offer_id", p.OfferID,
		"total_clicks", p.TotalClicks,
		"estimated_revenue", p.EstimatedRevenue,
		"conversion_rate", p.ConversionRate,
		"avg_order_value", p.AvgOrderValue,
		"created_at", p.CreatedAt,
		"updated_at", p.UpdatedAt,
	)

	return &p, nil
}

// GetByOfferID retrieves the affiliate performance record for a specific offer.
// Returns nil, nil when no row is found. Because offer_id is UNIQUE, this returns
// at most one record.
func (m *AffiliatePerformanceModel) GetByOfferID(ctx context.Context, offerID uuid.UUID) (*AffiliatePerformance, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("GetAffiliatePerformanceByOfferID")

	if offerID == uuid.Nil {
		err := errors.New("offer_id is required")
		logger.Error("Validation failed", err)
		return nil, err
	}

	var p AffiliatePerformance
	err := m.DB.QueryRow(ctx, `
		SELECT id, offer_id, total_clicks, estimated_revenue, conversion_rate, avg_order_value, created_at, updated_at
		FROM affiliate_performance
		WHERE offer_id = $1
	`, offerID).Scan(
		&p.ID,
		&p.OfferID,
		&p.TotalClicks,
		&p.EstimatedRevenue,
		&p.ConversionRate,
		&p.AvgOrderValue,
		&p.CreatedAt,
		&p.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			logger.Warn("Affiliate performance record not found", "offer_id", offerID)
			return nil, nil
		}
		logger.Error("Query affiliate performance by offer_id failed", err)
		return nil, err
	}

	logger.Info("GetByOfferID affiliate performance successful",
		"id", p.ID,
		"offer_id", p.OfferID,
		"total_clicks", p.TotalClicks,
		"estimated_revenue", p.EstimatedRevenue,
		"conversion_rate", p.ConversionRate,
		"avg_order_value", p.AvgOrderValue,
		"created_at", p.CreatedAt,
		"updated_at", p.UpdatedAt,
	)

	return &p, nil
}

// Delete permanently removes an affiliate performance record by primary key.
//
// This is intentionally a true hard delete. affiliate_performance is a derived
// aggregate row — one per offer, machine-maintained — and carries no editorial
// provenance, recovery, moderation, or historical-retention meaning. Soft-delete
// semantics are not appropriate for this model (BEG §13).
//
// Returns ErrAffiliatePerformanceNotFound if no row exists for the given id.
func (m *AffiliatePerformanceModel) Delete(ctx context.Context, id uuid.UUID) error {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("DeleteAffiliatePerformance")

	if id == uuid.Nil {
		err := errors.New("id is required")
		logger.Error("Validation failed", err)
		return err
	}

	var deletedID uuid.UUID
	err := m.DB.QueryRow(ctx, `
		DELETE FROM affiliate_performance
		WHERE id = $1
		RETURNING id
	`, id).Scan(&deletedID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			logger.Warn("Affiliate performance record not found for deletion", "id", id)
			return ErrAffiliatePerformanceNotFound
		}
		logger.Error("Delete affiliate performance failed", err, "id", id)
		return err
	}

	logger.Info("Delete affiliate performance successful", "id", deletedID)
	return nil
}

// List retrieves affiliate performance records ordered by updated_at DESC with
// mandatory limit/offset pagination.
//
// limit must be greater than zero. offset must be non-negative. Both are
// validated before any query is issued.
func (m *AffiliatePerformanceModel) List(ctx context.Context, limit, offset int) ([]AffiliatePerformance, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("ListAffiliatePerformance")

	if limit < 1 {
		err := errors.New("limit must be greater than 0")
		logger.Error("Validation failed", err)
		return nil, err
	}
	if offset < 0 {
		err := errors.New("offset cannot be negative")
		logger.Error("Validation failed", err)
		return nil, err
	}

	rows, err := m.DB.Query(ctx, `
		SELECT id, offer_id, total_clicks, estimated_revenue, conversion_rate, avg_order_value, created_at, updated_at
		FROM affiliate_performance
		ORDER BY updated_at DESC
		LIMIT $1 OFFSET $2
	`, limit, offset)
	if err != nil {
		logger.Error("List affiliate performance query failed", err)
		return nil, err
	}
	defer rows.Close()

	var performances []AffiliatePerformance
	for rows.Next() {
		var p AffiliatePerformance
		if err := rows.Scan(
			&p.ID,
			&p.OfferID,
			&p.TotalClicks,
			&p.EstimatedRevenue,
			&p.ConversionRate,
			&p.AvgOrderValue,
			&p.CreatedAt,
			&p.UpdatedAt,
		); err != nil {
			logger.Error("Scanning affiliate performance record failed", err)
			return nil, err
		}
		performances = append(performances, p)
	}
	if err := rows.Err(); err != nil {
		logger.Error("Error iterating affiliate performance rows", err)
		return nil, err
	}

	logger.Info("List affiliate performance successful",
		"count", len(performances),
		"limit", limit,
		"offset", offset,
	)
	return performances, nil
}

// FilteredList retrieves affiliate performance records with optional filters
// and mandatory limit/offset pagination. Results are ordered by updated_at DESC.
//
// Supported filters:
//   - offerID: restricts results to the single row for that offer.
//   - merchantID: restricts results to offers owned by the given merchant,
//     resolved via a subquery on the offers table.
//   - startDate: lower bound on updated_at (inclusive).
//   - endDate: upper bound on updated_at (inclusive).
//
// Passing nil for any filter parameter omits that predicate from the query.
// limit must be greater than zero; offset must be non-negative.
func (m *AffiliatePerformanceModel) FilteredList(
	ctx context.Context,
	limit, offset int,
	offerID, merchantID *uuid.UUID,
	startDate, endDate *time.Time,
) ([]AffiliatePerformance, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("ListAffiliatePerformanceFiltered")

	if limit < 1 {
		err := errors.New("limit must be greater than 0")
		logger.Error("Validation failed", err)
		return nil, err
	}
	if offset < 0 {
		err := errors.New("offset cannot be negative")
		logger.Error("Validation failed", err)
		return nil, err
	}

	query := `
		SELECT id, offer_id, total_clicks, estimated_revenue, conversion_rate, avg_order_value, created_at, updated_at
		FROM affiliate_performance
		WHERE 1=1
	`
	args := []any{}
	argIdx := 1

	if offerID != nil {
		query += fmt.Sprintf(" AND offer_id = $%d", argIdx)
		args = append(args, *offerID)
		argIdx++
	}
	if merchantID != nil {
		query += fmt.Sprintf(" AND offer_id IN (SELECT id FROM offers WHERE merchant_id = $%d)", argIdx)
		args = append(args, *merchantID)
		argIdx++
	}
	if startDate != nil {
		query += fmt.Sprintf(" AND updated_at >= $%d", argIdx)
		args = append(args, *startDate)
		argIdx++
	}
	if endDate != nil {
		query += fmt.Sprintf(" AND updated_at <= $%d", argIdx)
		args = append(args, *endDate)
		argIdx++
	}

	query += fmt.Sprintf(" ORDER BY updated_at DESC LIMIT $%d OFFSET $%d", argIdx, argIdx+1)
	args = append(args, limit, offset)

	rows, err := m.DB.Query(ctx, query, args...)
	if err != nil {
		logger.Error("Filtered affiliate performance query failed", err)
		return nil, err
	}
	defer rows.Close()

	var performances []AffiliatePerformance
	for rows.Next() {
		var p AffiliatePerformance
		if err := rows.Scan(
			&p.ID,
			&p.OfferID,
			&p.TotalClicks,
			&p.EstimatedRevenue,
			&p.ConversionRate,
			&p.AvgOrderValue,
			&p.CreatedAt,
			&p.UpdatedAt,
		); err != nil {
			logger.Error("Row scan failed", err)
			return nil, err
		}
		performances = append(performances, p)
	}
	if err := rows.Err(); err != nil {
		logger.Error("Row iteration failed", err)
		return nil, err
	}

	logger.Info("Filtered affiliate performance list retrieved",
		"count", len(performances),
		"limit", limit,
		"offset", offset,
	)
	return performances, nil
}

// UpdateMetrics updates the aggregate metrics for an existing record by primary key.
//
// updated_at is maintained automatically by the set_updated_at() trigger on the
// affiliate_performance table. The post-update value is captured via RETURNING
// for structured logging and caller inspection.
//
// Returns a descriptive error when no row exists for the given id.
// All metric fields are validated before any query is issued.
func (m *AffiliatePerformanceModel) UpdateMetrics(
	ctx context.Context,
	id uuid.UUID,
	totalClicks int,
	estimatedRevenue, conversionRate, avgOrderValue string,
) error {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("UpdateAffiliatePerformanceMetrics")

	if id == uuid.Nil {
		err := errors.New("id is required")
		logger.Error("Validation failed", err)
		return err
	}
	if totalClicks < 0 {
		err := errors.New("total_clicks cannot be negative")
		logger.Error("Validation failed", err)
		return err
	}
	if err := validateNonNegativeDecimalString(estimatedRevenue, "estimated_revenue"); err != nil {
		logger.Error("Validation failed", err)
		return err
	}
	if err := validateNonNegativeDecimalString(conversionRate, "conversion_rate"); err != nil {
		logger.Error("Validation failed", err)
		return err
	}
	if err := validateNonNegativeDecimalString(avgOrderValue, "avg_order_value"); err != nil {
		logger.Error("Validation failed", err)
		return err
	}

	var updatedAt time.Time
	err := m.DB.QueryRow(ctx, `
		UPDATE affiliate_performance
		SET total_clicks      = $1,
		    estimated_revenue = $2,
		    conversion_rate   = $3,
		    avg_order_value   = $4
		WHERE id = $5
		RETURNING updated_at
	`, totalClicks, estimatedRevenue, conversionRate, avgOrderValue, id).Scan(&updatedAt)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			err = ErrAffiliatePerformanceNotFound
		}
		logger.Error("Update affiliate performance metrics failed", err)
		return err
	}

	logger.Info("Update affiliate performance metrics successful",
		"id", id,
		"total_clicks", totalClicks,
		"estimated_revenue", estimatedRevenue,
		"conversion_rate", conversionRate,
		"avg_order_value", avgOrderValue,
		"updated_at", updatedAt,
	)
	return nil
}