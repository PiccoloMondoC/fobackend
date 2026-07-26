// Package data provides models and database access methods for coupons and other entities.
//
// sdworkspace/sdbackend/internal/data/coupons.go
//
// GTM:
//
//	Layer: 2.5 Catalog / Offer Domain
//	Release Class: DEFERRED
//	Reason:
//	  Coupons, coupon statuses, coupon usage, and coupon performance statistics
//	  are valid future commerce features, but they are not required for the
//	  initial Platform release spine. The v1 spine is offer-first and
//	  relies on canonical offers, affiliate links, publication status, click
//	  tracking, and price history before expanding into a dedicated coupon
//	  ecosystem.
//
// DEFERRED Rule:
//
//	Keep compiling.
//	Keep safe.
//	Preserve offer-first coupon ownership.
//	Preserve coupon status lookup semantics.
//	Do not add new features.
//	Do not route into v1 UI/API expansion.
//	Do not block deployment on this file unless it breaks the build.
package data

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"net/url"
	"strings"
	"time"

	"github.com/PiccoloMondoC/sdworkspace/sdbackend/internal/logging"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PostgreSQL error codes used for typed error translation.
const (
	couponPGUniqueViolation     = "23505"
	couponPGForeignKeyViolation = "23503"
)

// Coupon represents the persisted canonical coupon model.
//
// Engineering notes:
//   - Offer-first: coupons belong to offers, not deals.
//   - Numeric database fields are represented as strings to preserve exact
//     decimal fidelity for NUMERIC(19,4) columns. Callers must not parse these
//     into float64.
//   - created_at and updated_at are DB-owned and must not be set by the
//     application. Insert/Update read them back via RETURNING.
//   - database.go should be tightened so coupons.discount_type is NOT NULL.
//     The application already enforces this invariant, and the schema should
//     match it (§2.5 integrity at the lowest safe layer).
type Coupon struct {
	ID                uuid.UUID  `json:"id"                         db:"id"`
	OfferID           *uuid.UUID `json:"offer_id,omitempty"         db:"offer_id"`
	CouponStatusID    *uuid.UUID `json:"coupon_status_id,omitempty" db:"coupon_status_id"`
	Code              string     `json:"code"                       db:"code"`
	DiscountType      string     `json:"discount_type"              db:"discount_type"`
	DiscountValue     string     `json:"discount_value"             db:"discount_value"`
	MinPurchaseAmount string     `json:"min_purchase_amount"        db:"min_purchase_amount"`
	StartDate         time.Time  `json:"start_date"                 db:"start_date"`
	EndDate           *time.Time `json:"end_date,omitempty"         db:"end_date"`
	AffiliateURL      *string    `json:"affiliate_url,omitempty"    db:"affiliate_url"`
	RejectionReason   *string    `json:"rejection_reason,omitempty" db:"rejection_reason"`
	DeletedAt         *time.Time `json:"-"                          db:"deleted_at"`
	CreatedAt         time.Time  `json:"created_at"                 db:"created_at"`
	UpdatedAt         time.Time  `json:"updated_at"                 db:"updated_at"`
}

// CouponFlagSummary holds metadata about an unresolved flag on a coupon.
// Fields map directly to the columns selected by GetFlaggedCoupons, which
// filters to WHERE resolved_at IS NULL. If you need resolution metadata,
// use CouponResolvedFlagSummary via GetResolvedFlaggedCoupons.
type CouponFlagSummary struct {
	ID        uuid.UUID  `json:"id"`
	Reason    string     `json:"reason"`
	FlaggedBy *uuid.UUID `json:"flagged_by,omitempty"`
	FlaggedAt time.Time  `json:"flagged_at"`
}

// CouponResolvedFlagSummary holds the full flag record including resolution
// metadata. Used by GetResolvedFlaggedCoupons where resolution fields are
// guaranteed to be non-NULL.
type CouponResolvedFlagSummary struct {
	ID               uuid.UUID  `json:"id"`
	Reason           string     `json:"reason"`
	FlaggedBy        *uuid.UUID `json:"flagged_by,omitempty"`
	FlaggedAt        time.Time  `json:"flagged_at"`
	ResolvedAt       *time.Time `json:"resolved_at,omitempty"`
	ResolvedBy       *uuid.UUID `json:"resolved_by,omitempty"`
	ResolutionNotes  *string    `json:"resolution_notes,omitempty"`
	ResolutionStatus *string    `json:"resolution_status,omitempty"`
}

// CouponWithResolvedFlag extends Coupon with resolved flag info.
type CouponWithResolvedFlag struct {
	*Coupon
	FlagInfo *CouponResolvedFlagSummary `json:"flag_info,omitempty"`
}

// CouponWithFlag extends Coupon with unresolved flag info.
type CouponWithFlag struct {
	*Coupon
	FlagInfo *CouponFlagSummary `json:"flag_info,omitempty"`
}

// CouponStatus represents the status of a discount coupon in the system.
type CouponStatus struct {
	ID          uuid.UUID `json:"id"          db:"id"`
	Name        string    `json:"name"        db:"name"`
	Description string    `json:"description" db:"description"`
	IsActive    bool      `json:"is_active"   db:"is_active"`
	CreatedAt   time.Time `json:"created_at"  db:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"  db:"updated_at"`
}

// CouponFlag represents a persisted coupon flag record.
type CouponFlag struct {
	ID               uuid.UUID  `json:"id"                          db:"id"`
	CouponID         uuid.UUID  `json:"coupon_id"                   db:"coupon_id"`
	Reason           string     `json:"reason"                      db:"reason"`
	FlaggedBy        *uuid.UUID `json:"flagged_by,omitempty"        db:"flagged_by"`
	CreatedAt        time.Time  `json:"created_at"                  db:"created_at"`
	ResolvedAt       *time.Time `json:"resolved_at,omitempty"       db:"resolved_at"`
	ResolvedBy       *uuid.UUID `json:"resolved_by,omitempty"       db:"resolved_by"`
	ResolutionNotes  *string    `json:"resolution_notes,omitempty"  db:"resolution_notes"`
	ResolutionStatus *string    `json:"resolution_status,omitempty" db:"resolution_status"`
}

// CouponUsage represents a user interaction with a coupon.
type CouponUsage struct {
	ID        uuid.UUID `json:"id"         db:"id"`
	UserID    uuid.UUID `json:"user_id"    db:"user_id"`
	CouponID  uuid.UUID `json:"coupon_id"  db:"coupon_id"`
	Action    string    `json:"action"     db:"action"`
	CreatedAt time.Time `json:"created_at" db:"created_at"`
}

// CouponPerformanceStats holds engagement and conversion metrics for a coupon.
//
// TotalRevenue is always "0.0000" until a coupon-attributed revenue column is
// added to the schema. It is typed as string to match the NUMERIC(19,4)
// convention used throughout this file.
type CouponPerformanceStats struct {
	TotalClicks      int    `json:"total_clicks"`
	TotalConversions int    `json:"total_conversions"`
	TotalRevenue     string `json:"total_revenue"`
}

// CouponModel holds the DB pool and logger for coupon operations.
type CouponModel struct {
	DB     *pgxpool.Pool
	Logger *logging.Logger
}

// CouponStatusModel holds the DB pool and logger for coupon status operations.
type CouponStatusModel struct {
	DB     *pgxpool.Pool
	Logger *logging.Logger
}

// CouponUsageModel holds the DB pool and logger for coupon usage operations.
type CouponUsageModel struct {
	DB     *pgxpool.Pool
	Logger *logging.Logger
}

// CouponPerformanceStatsModel holds the DB pool and logger for coupon
// performance operations.
type CouponPerformanceStatsModel struct {
	DB     *pgxpool.Pool
	Logger *logging.Logger
}

// ─────────────────────────────────────────────────────────────────────────────
// Internal helpers
// ─────────────────────────────────────────────────────────────────────────────

// normalizeCoupon canonicalizes caller-provided fields so persistence and
// validation operate on stable values. It is called before validateCoupon on
// every write path (Insert, Update).
func normalizeCoupon(coupon *Coupon) {
	if coupon == nil {
		return
	}

	coupon.Code = strings.TrimSpace(coupon.Code)
	coupon.DiscountType = strings.TrimSpace(strings.ToLower(coupon.DiscountType))
	coupon.DiscountValue = strings.TrimSpace(coupon.DiscountValue)
	coupon.MinPurchaseAmount = strings.TrimSpace(coupon.MinPurchaseAmount)

	if coupon.AffiliateURL != nil {
		trimmed := strings.TrimSpace(*coupon.AffiliateURL)
		if trimmed == "" {
			coupon.AffiliateURL = nil
		} else {
			coupon.AffiliateURL = &trimmed
		}
	}

	if coupon.RejectionReason != nil {
		trimmed := strings.TrimSpace(*coupon.RejectionReason)
		if trimmed == "" {
			coupon.RejectionReason = nil
		} else {
			coupon.RejectionReason = &trimmed
		}
	}

	if coupon.StartDate.IsZero() {
		coupon.StartDate = time.Now().UTC()
	}
}

// validateCouponDecimal validates a decimal string for NUMERIC columns while
// preserving exact precision semantics. Using big.Rat avoids float64 rounding
// errors during validation.
//
// allowZero=true permits 0 (useful for min_purchase_amount).
// allowZero=false requires a strictly positive value (useful for discount_value).
func validateCouponDecimal(value, field string, allowZero bool) error {
	if strings.TrimSpace(value) == "" {
		return fmt.Errorf("%s is required", field)
	}

	rat, ok := new(big.Rat).SetString(value)
	if !ok {
		return fmt.Errorf("%s must be a valid decimal value", field)
	}

	zero := new(big.Rat)
	if allowZero {
		if rat.Cmp(zero) < 0 {
			return fmt.Errorf("%s cannot be negative", field)
		}
	} else {
		if rat.Cmp(zero) <= 0 {
			return fmt.Errorf("%s must be greater than zero", field)
		}
	}

	return nil
}

// validateCoupon enforces the application-layer invariants that must agree with
// the coupons table DDL in database.go. It is called on every write path after
// normalizeCoupon.
func validateCoupon(coupon *Coupon) error {
	if coupon == nil {
		return errors.New("coupon is required")
	}

	if coupon.OfferID == nil || *coupon.OfferID == uuid.Nil {
		return ErrCouponOfferRequired
	}

	if coupon.Code == "" {
		return ErrCouponCodeRequired
	}

	switch coupon.DiscountType {
	case "percentage", "fixed", "rebate":
		// valid
	default:
		return errors.New("discount_type must be one of: percentage, fixed, rebate")
	}

	if err := validateCouponDecimal(coupon.DiscountValue, "discount_value", false); err != nil {
		return err
	}
	if err := validateCouponDecimal(coupon.MinPurchaseAmount, "min_purchase_amount", true); err != nil {
		return err
	}

	// Percentage coupons must be between 0.01 and 100.
	if coupon.DiscountType == "percentage" {
		rat, _ := new(big.Rat).SetString(coupon.DiscountValue)
		lo := big.NewRat(1, 100) // 0.01
		hi := big.NewRat(100, 1) // 100
		if rat.Cmp(lo) < 0 || rat.Cmp(hi) > 0 {
			return errors.New("discount_value for percentage coupons must be between 0.01 and 100")
		}
	}

	if coupon.StartDate.IsZero() {
		return errors.New("start_date is required")
	}

	if coupon.EndDate != nil && !coupon.EndDate.After(coupon.StartDate) {
		return errors.New("end_date must be after start_date")
	}

	if coupon.AffiliateURL != nil {
		parsed, err := url.ParseRequestURI(*coupon.AffiliateURL)
		if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
			return errors.New("affiliate_url must be a valid absolute http or https URL")
		}
	}

	return nil
}

// resolveCouponStatusIDByName resolves an active coupon status name to its
// seeded UUID. It is used by ApproveCoupon, RejectCoupon, UpdateCouponStatus,
// and AutoExpireCoupons so that status transitions always reference the
// coupon_statuses table rather than inline text values.
func (m *CouponModel) resolveCouponStatusIDByName(ctx context.Context, statusName string) (uuid.UUID, error) {
	var statusID uuid.UUID

	err := m.DB.QueryRow(
		ctx,
		`SELECT id
		FROM coupon_statuses
		WHERE lower(name) = lower($1)
		  AND is_active = TRUE`,
		strings.TrimSpace(statusName),
	).Scan(&statusID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return uuid.Nil, ErrCouponStatusNotFound
		}
		return uuid.Nil, err
	}

	return statusID, nil
}

// scanCoupon centralizes row scanning for all canonical coupon reads. The dest
// argument accepts anything that implements Scan (pgx.Row, pgx.Rows), so the
// same function is used by single-row and multi-row paths alike.
func scanCoupon(
	row interface {
		Scan(dest ...any) error
	},
	coupon *Coupon,
) error {
	return row.Scan(
		&coupon.ID,
		&coupon.OfferID,
		&coupon.CouponStatusID,
		&coupon.Code,
		&coupon.DiscountType,
		&coupon.DiscountValue,
		&coupon.MinPurchaseAmount,
		&coupon.StartDate,
		&coupon.EndDate,
		&coupon.AffiliateURL,
		&coupon.RejectionReason,
		&coupon.CreatedAt,
		&coupon.UpdatedAt,
	)
}

// scanCouponWithAlias centralizes row scanning for join-based coupon reads
// where the coupons table is aliased as "c" (e.g. SELECT c.id, c.offer_id, ...).
// It delegates to scanCoupon because the column order and destination fields
// are identical — the alias is purely a SQL-layer concern.
func scanCouponWithAlias(
	row interface {
		Scan(dest ...any) error
	},
	coupon *Coupon,
) error {
	return scanCoupon(row, coupon)
}

// couponSelectColumns is the canonical SELECT column list for queries that read
// directly from the coupons table with no alias (e.g. FROM coupons WHERE id=$1).
// Column order must match scanCoupon exactly.
const couponSelectColumns = `
	id,
	offer_id,
	coupon_status_id,
	code,
	discount_type,
	discount_value,
	min_purchase_amount,
	start_date,
	end_date,
	affiliate_url,
	rejection_reason,
	created_at,
	updated_at`

// couponAliasedColumns is the canonical SELECT column list for queries that
// join coupons under the alias "c". Using a shared constant keeps the column
// order consistent with scanCoupon across all join-based read paths.
const couponAliasedColumns = `
	c.id,
	c.offer_id,
	c.coupon_status_id,
	c.code,
	c.discount_type,
	c.discount_value,
	c.min_purchase_amount,
	c.start_date,
	c.end_date,
	c.affiliate_url,
	c.rejection_reason,
	c.created_at,
	c.updated_at`

// couponGroupByID is the minimal GROUP BY clause for aggregate queries over
// coupons aliased as "c". PostgreSQL allows grouping by primary key alone
// because all other columns are functionally dependent on c.id.
const couponGroupByID = `GROUP BY c.id`

// ─────────────────────────────────────────────────────────────────────────────
// CouponModel — CRUD
// ─────────────────────────────────────────────────────────────────────────────

// Insert inserts a new coupon and populates DB-owned fields (id default,
// created_at, updated_at) via RETURNING. The caller's coupon pointer is
// mutated in place so it reflects the committed state.
func (m *CouponModel) Insert(ctx context.Context, coupon *Coupon) error {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("InsertCoupon")

	normalizeCoupon(coupon)

	if err := validateCoupon(coupon); err != nil {
		logger.Error("Validation failed", err)
		return err
	}

	if coupon.ID == uuid.Nil {
		coupon.ID = uuid.New()
	}

	query := `
		INSERT INTO coupons (
			id,
			offer_id,
			coupon_status_id,
			code,
			discount_type,
			discount_value,
			min_purchase_amount,
			start_date,
			end_date,
			affiliate_url,
			rejection_reason
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		RETURNING` + couponSelectColumns

	err := scanCoupon(
		m.DB.QueryRow(
			ctx,
			query,
			coupon.ID,
			coupon.OfferID,
			coupon.CouponStatusID,
			coupon.Code,
			coupon.DiscountType,
			coupon.DiscountValue,
			coupon.MinPurchaseAmount,
			coupon.StartDate,
			coupon.EndDate,
			coupon.AffiliateURL,
			coupon.RejectionReason,
		),
		coupon,
	)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) {
			switch pgErr.Code {
			case couponPGUniqueViolation:
				logger.Warn("Insert coupon failed: duplicate business key",
					"coupon_id", coupon.ID,
					"offer_id", coupon.OfferID,
					"code", coupon.Code,
				)
				return ErrCouponAlreadyExists
			case couponPGForeignKeyViolation:
				logger.Error("Insert coupon failed: foreign key violation", err)
				return fmt.Errorf("insert coupon: foreign key violation: %w", err)
			}
		}
		logger.Error("Insert coupon failed", err)
		return err
	}

	logger.Info("Insert coupon successful",
		"id", coupon.ID,
		"offer_id", coupon.OfferID,
		"coupon_status_id", coupon.CouponStatusID,
		"code", coupon.Code,
		"discount_type", coupon.DiscountType,
	)

	return nil
}

// GetByID retrieves a single coupon by its primary key. Returns
// ErrCouponNotFound when no row matches.
func (m *CouponModel) GetByID(ctx context.Context, id uuid.UUID) (*Coupon, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("GetCouponByID")

	if id == uuid.Nil {
		err := errors.New("coupon id is required")
		logger.Error("Validation failed", err)
		return nil, err
	}

	query := `
		SELECT` + couponSelectColumns + `
		FROM coupons
		WHERE id = $1`

	var coupon Coupon
	if err := scanCoupon(m.DB.QueryRow(ctx, query, id), &coupon); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			logger.Warn("Coupon not found", "id", id)
			return nil, ErrCouponNotFound
		}
		logger.Error("Get coupon by id failed", err)
		return nil, err
	}

	logger.Info("Get coupon by id successful", "id", coupon.ID)
	return &coupon, nil
}

// GetByOfferID retrieves all coupons attached to a single offer, ordered by
// most recent start_date first, then by created_at descending.
func (m *CouponModel) GetByOfferID(ctx context.Context, offerID uuid.UUID) ([]*Coupon, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("GetCouponsByOfferID")

	if offerID == uuid.Nil {
		err := errors.New("offer_id is required")
		logger.Error("Validation failed", err)
		return nil, err
	}

	query := `
		SELECT` + couponSelectColumns + `
		FROM coupons
		WHERE offer_id = $1
		ORDER BY start_date DESC, created_at DESC`

	rows, err := m.DB.Query(ctx, query, offerID)
	if err != nil {
		logger.Error("Get coupons by offer id failed", err)
		return nil, err
	}
	defer rows.Close()

	var coupons []*Coupon
	for rows.Next() {
		var coupon Coupon
		if err := scanCoupon(rows, &coupon); err != nil {
			logger.Error("Scan coupon row failed", err)
			return nil, err
		}
		coupons = append(coupons, &coupon)
	}

	if err := rows.Err(); err != nil {
		logger.Error("Iterate coupon rows failed", err)
		return nil, err
	}

	logger.Info("Get coupons by offer id successful", "offer_id", offerID, "count", len(coupons))
	return coupons, nil
}

// GetByDealID is a compatibility shim. Callers should migrate to GetByOfferID.
// It delegates directly and will be removed once all call sites are updated.
func (m *CouponModel) GetByDealID(ctx context.Context, dealID uuid.UUID) ([]*Coupon, error) {
	return m.GetByOfferID(ctx, dealID)
}

// GetActiveCoupons retrieves all coupons active at the current DB time.
// Active means start_date <= NOW() and (end_date IS NULL OR end_date > NOW()).
func (m *CouponModel) GetActiveCoupons(ctx context.Context) ([]*Coupon, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("GetActiveCoupons")

	query := `
		SELECT` + couponSelectColumns + `
		FROM coupons
		WHERE start_date <= NOW()
		  AND (end_date IS NULL OR end_date > NOW())
		ORDER BY start_date DESC, created_at DESC`

	rows, err := m.DB.Query(ctx, query)
	if err != nil {
		logger.Error("Get active coupons failed", err)
		return nil, err
	}
	defer rows.Close()

	var coupons []*Coupon
	for rows.Next() {
		var coupon Coupon
		if err := scanCoupon(rows, &coupon); err != nil {
			logger.Error("Scan coupon row failed", err)
			return nil, err
		}
		coupons = append(coupons, &coupon)
	}

	if err := rows.Err(); err != nil {
		logger.Error("Iterate coupon rows failed", err)
		return nil, err
	}

	logger.Info("Get active coupons successful", "count", len(coupons))
	return coupons, nil
}

// Update replaces the mutable business fields of an existing coupon and
// returns DB-owned timestamps via RETURNING. Returns ErrCouponNotFound when
// no row matches the supplied ID, and ErrCouponAlreadyExists when the update
// would violate the (offer_id, code) unique constraint.
func (m *CouponModel) Update(ctx context.Context, coupon *Coupon) error {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("UpdateCoupon")

	if coupon == nil {
		err := errors.New("coupon is required")
		logger.Error("Validation failed", err)
		return err
	}

	if coupon.ID == uuid.Nil {
		err := errors.New("coupon id is required for update")
		logger.Error("Validation failed", err)
		return err
	}

	normalizeCoupon(coupon)

	if err := validateCoupon(coupon); err != nil {
		logger.Error("Validation failed", err)
		return err
	}

	query := `
		UPDATE coupons
		SET
			offer_id           = $1,
			coupon_status_id   = $2,
			code               = $3,
			discount_type      = $4,
			discount_value     = $5,
			min_purchase_amount = $6,
			start_date         = $7,
			end_date           = $8,
			affiliate_url      = $9,
			rejection_reason   = $10
		WHERE id = $11
		RETURNING` + couponSelectColumns

	err := scanCoupon(
		m.DB.QueryRow(
			ctx,
			query,
			coupon.OfferID,
			coupon.CouponStatusID,
			coupon.Code,
			coupon.DiscountType,
			coupon.DiscountValue,
			coupon.MinPurchaseAmount,
			coupon.StartDate,
			coupon.EndDate,
			coupon.AffiliateURL,
			coupon.RejectionReason,
			coupon.ID,
		),
		coupon,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			logger.Warn("Update coupon failed: coupon not found", "id", coupon.ID)
			return ErrCouponNotFound
		}
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == couponPGUniqueViolation {
			logger.Warn("Update coupon failed: duplicate business key",
				"id", coupon.ID,
				"offer_id", coupon.OfferID,
				"code", coupon.Code,
			)
			return ErrCouponAlreadyExists
		}
		logger.Error("Update coupon failed", err)
		return err
	}

	logger.Info("Update coupon successful",
		"id", coupon.ID,
		"offer_id", coupon.OfferID,
		"coupon_status_id", coupon.CouponStatusID,
		"code", coupon.Code,
		"discount_type", coupon.DiscountType,
		"discount_value", coupon.DiscountValue,
	)

	return nil
}

// SoftDelete logically removes a coupon by setting deleted_at to the current
// database time.
//
// Coupons are a retention-sensitive business record: they may be moderated,
// expired, rejected, analyzed for performance, and referenced historically.
// Standard reads exclude soft-deleted rows by default. Delete() is the
// separate physical purge path and must not be used in place of SoftDelete()
// for ordinary coupon removal.
//
// Returns ErrCouponNotFound when no active row matches the supplied ID.
func (m *CouponModel) SoftDelete(ctx context.Context, id uuid.UUID) error {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("SoftDeleteCoupon")

	if id == uuid.Nil {
		err := errors.New("coupon id is required")
		logger.Error("Validation failed", err)
		return err
	}

	var deletedAt time.Time
	err := m.DB.QueryRow(ctx, `
		UPDATE coupons
		SET deleted_at = NOW()
		WHERE id = $1
		  AND deleted_at IS NULL
		RETURNING deleted_at
	`, id).Scan(&deletedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			logger.Warn("Soft delete coupon failed: coupon not found", "id", id)
			return ErrCouponNotFound
		}
		logger.Error("Soft delete coupon failed", err, "id", id)
		return err
	}

	logger.Info("Soft delete coupon successful",
		"id", id,
		"deleted_at", deletedAt,
	)
	return nil
}

// Delete permanently removes a coupon from the database by ID.
//
// This is the physical purge path. It must remain a true hard delete and must
// not become an alias for SoftDelete().
//
// Returns ErrCouponNotFound when no row exists for the supplied ID.
func (m *CouponModel) Delete(ctx context.Context, id uuid.UUID) error {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("DeleteCoupon")

	if id == uuid.Nil {
		err := errors.New("coupon id is required")
		logger.Error("Validation failed", err)
		return err
	}

	var deletedID uuid.UUID
	err := m.DB.QueryRow(ctx, `
		DELETE FROM coupons
		WHERE id = $1
		RETURNING id
	`, id).Scan(&deletedID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			logger.Warn("Delete coupon failed: coupon not found", "id", id)
			return ErrCouponNotFound
		}
		logger.Error("Delete coupon failed", err, "id", id)
		return err
	}

	logger.Info("Delete coupon successful", "id", deletedID)
	return nil
}

// DeleteExpiredCoupons hard-deletes all coupon rows whose end_date has passed.
// This is a destructive operation. Consider AutoExpireCoupons (status
// transition) as the preferred alternative where audit history matters.
func (m *CouponModel) DeleteExpiredCoupons(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("DeleteExpiredCoupons")

	result, err := m.DB.Exec(ctx,
		`DELETE FROM coupons
		WHERE end_date IS NOT NULL
		  AND end_date < NOW()`,
	)
	if err != nil {
		logger.Error("Delete expired coupons failed", err)
		return err
	}

	logger.Info("Delete expired coupons successful", "rows_deleted", result.RowsAffected())
	return nil
}

// ─────────────────────────────────────────────────────────────────────────────
// CouponModel — User engagement
// ─────────────────────────────────────────────────────────────────────────────

// ClipCoupon stores the coupon's parent offer in user_favorites. This aligns
// coupon clipping with the canonical persistence model, which exposes favorites
// at the offer level. ON CONFLICT DO NOTHING makes the operation idempotent so
// callers may retry safely without duplicate-key errors.
func (m *CouponModel) ClipCoupon(ctx context.Context, userID, couponID uuid.UUID) error {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("ClipCoupon")

	if userID == uuid.Nil {
		err := errors.New("user_id is required")
		logger.Error("Validation failed", err)
		return err
	}
	if couponID == uuid.Nil {
		err := errors.New("coupon_id is required")
		logger.Error("Validation failed", err)
		return err
	}

	// Resolve the offer that owns this coupon. offer_id is nullable on coupons,
	// so scan into a pointer to avoid a pgx error when the column value is NULL.
	var offerID *uuid.UUID
	err := m.DB.QueryRow(
		ctx,
		`SELECT offer_id FROM coupons WHERE id = $1`,
		couponID,
	).Scan(&offerID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			logger.Warn("Clip coupon failed: coupon not found", "coupon_id", couponID)
			return ErrCouponNotFound
		}
		logger.Error("Clip coupon failed while resolving offer", err)
		return err
	}
	if offerID == nil || *offerID == uuid.Nil {
		logger.Warn("Clip coupon failed: coupon has no linked offer", "coupon_id", couponID)
		return ErrCouponClipNotSupported
	}

	// Upsert into user_favorites at the offer level.
	favoriteID := uuid.New()
	_, err = m.DB.Exec(
		ctx,
		`INSERT INTO user_favorites (id, user_id, offer_id)
		VALUES ($1, $2, $3)
		ON CONFLICT DO NOTHING`,
		favoriteID,
		userID,
		*offerID,
	)
	if err != nil {
		logger.Error("Clip coupon failed: insert user_favorites", err,
			"user_id", userID,
			"offer_id", offerID,
		)
		return err
	}

	logger.Info("Clip coupon successful",
		"user_id", userID,
		"coupon_id", couponID,
		"offer_id", *offerID,
		"user_favorite_id", favoriteID,
	)

	return nil
}

// GetClippedCoupons retrieves coupons whose parent offers have been saved by a
// user. In the current schema, saved/clip behavior is represented by
// user_favorites against offers, so coupons are reached through coupon.offer_id.
func (m *CouponModel) GetClippedCoupons(ctx context.Context, userID uuid.UUID) ([]*Coupon, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("GetClippedCoupons")

	if userID == uuid.Nil {
		err := errors.New("user_id is required")
		logger.Error("Validation failed", err)
		return nil, err
	}

	query := `
		SELECT` + couponAliasedColumns + `
		FROM coupons c
		INNER JOIN user_favorites uf
			ON uf.offer_id = c.offer_id
		WHERE uf.user_id = $1
			AND uf.deleted_at IS NULL
			AND c.deleted_at IS NULL
		ORDER BY uf.favorited_at DESC, c.created_at DESC`

	rows, err := m.DB.Query(ctx, query, userID)
	if err != nil {
		logger.Error("Get clipped coupons failed", err)
		return nil, err
	}
	defer rows.Close()

	var coupons []*Coupon
	for rows.Next() {
		var coupon Coupon
		if err := scanCouponWithAlias(rows, &coupon); err != nil {
			logger.Error("Scan clipped coupon row failed", err)
			return nil, err
		}
		coupons = append(coupons, &coupon)
	}

	if err := rows.Err(); err != nil {
		logger.Error("Iterate clipped coupon rows failed", err)
		return nil, err
	}

	logger.Info("Get clipped coupons successful",
		"user_id", userID,
		"coupon_count", len(coupons),
	)
	return coupons, nil
}

// GetPopularCoupons retrieves coupons ordered by total coupon_usages volume.
// This is grounded in the coupon_usages table (the only usage-tracking table
// in the current schema). There is no click_count column on coupons.
func (m *CouponModel) GetPopularCoupons(ctx context.Context, limit int) ([]*Coupon, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("GetPopularCoupons")

	if limit <= 0 {
		err := errors.New("limit must be greater than zero")
		logger.Error("Validation failed", err)
		return nil, err
	}

	// GROUP BY c.id is sufficient because c.id is the primary key: PostgreSQL
	// treats all other columns as functionally dependent on it. The previous
	// verbose GROUP BY repeated all 13 columns, which would silently break if
	// a column were added to the Coupon struct without updating this query.
	query := `
		SELECT` + couponAliasedColumns + `
		FROM coupons c
		LEFT JOIN coupon_usages cu ON cu.coupon_id = c.id
		WHERE c.deleted_at IS NULL
		` + couponGroupByID + `
		ORDER BY COUNT(cu.id) DESC, c.created_at DESC
		LIMIT $1`

	rows, err := m.DB.Query(ctx, query, limit)
	if err != nil {
		logger.Error("Get popular coupons failed", err)
		return nil, err
	}
	defer rows.Close()

	var coupons []*Coupon
	for rows.Next() {
		var coupon Coupon
		if err := scanCouponWithAlias(rows, &coupon); err != nil {
			logger.Error("Scan popular coupon row failed", err)
			return nil, err
		}
		coupons = append(coupons, &coupon)
	}

	if err := rows.Err(); err != nil {
		logger.Error("Iterate popular coupon rows failed", err)
		return nil, err
	}

	logger.Info("Get popular coupons successful", "count", len(coupons))
	return coupons, nil
}

// GetCouponsByCategory retrieves coupons by the category of their parent offer.
// The join path is: coupons -> offers -> categories. category is matched
// case-insensitively against categories.name.
func (m *CouponModel) GetCouponsByCategory(ctx context.Context, category string, limit int) ([]*Coupon, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("GetCouponsByCategory")

	category = strings.TrimSpace(category)
	if category == "" {
		err := errors.New("category is required")
		logger.Error("Validation failed", err)
		return nil, err
	}
	if limit <= 0 {
		err := errors.New("limit must be greater than zero")
		logger.Error("Validation failed", err)
		return nil, err
	}

	// Join path: coupons -> offers -> categories.
	// The old code joined through a non-existent deals table.
	query := `
		SELECT` + couponAliasedColumns + `
		FROM coupons c
		INNER JOIN offers o       ON o.id   = c.offer_id
		INNER JOIN categories cat ON cat.id = o.category_id
		WHERE lower(cat.name::text) = lower($1)
		ORDER BY c.start_date DESC, c.created_at DESC
		LIMIT $2`

	rows, err := m.DB.Query(ctx, query, category, limit)
	if err != nil {
		logger.Error("Get coupons by category failed", err)
		return nil, err
	}
	defer rows.Close()

	var coupons []*Coupon
	for rows.Next() {
		var coupon Coupon
		if err := scanCouponWithAlias(rows, &coupon); err != nil {
			logger.Error("Scan coupon row failed", err)
			return nil, err
		}
		coupons = append(coupons, &coupon)
	}

	if err := rows.Err(); err != nil {
		logger.Error("Iterate coupon rows failed", err)
		return nil, err
	}

	logger.Info("GetCouponsByCategory successful",
		"category", category,
		"limit", limit,
		"results_count", len(coupons),
	)
	return coupons, nil
}

// ─────────────────────────────────────────────────────────────────────────────
// CouponModel — Administrative control
// ─────────────────────────────────────────────────────────────────────────────

// ApproveCoupon sets the coupon_status_id to the seeded "active" status.
// The old implementation mutated a non-existent approved boolean column.
func (m *CouponModel) ApproveCoupon(ctx context.Context, couponID uuid.UUID) error {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("ApproveCoupon")

	if couponID == uuid.Nil {
		err := errors.New("coupon_id is required")
		logger.Error("Validation failed", err)
		return err
	}

	statusID, err := m.resolveCouponStatusIDByName(ctx, "active")
	if err != nil {
		logger.Error("Approve coupon failed while resolving active status", err)
		return err
	}

	commandTag, err := m.DB.Exec(ctx,
		`UPDATE coupons
		SET coupon_status_id = $1
		WHERE id = $2`,
		statusID,
		couponID,
	)
	if err != nil {
		logger.Error("Approve coupon failed", err)
		return err
	}

	if commandTag.RowsAffected() == 0 {
		logger.Warn("Approve coupon failed: coupon not found", "coupon_id", couponID)
		return ErrCouponNotFound
	}

	logger.Info("Approve coupon successful",
		"coupon_id", couponID,
		"coupon_status_id", statusID,
	)
	return nil
}

// RejectCoupon sets the coupon status to the seeded "rejected" status and
// records a rejection reason. The old implementation mutated a non-existent
// status text column.
func (m *CouponModel) RejectCoupon(ctx context.Context, couponID uuid.UUID, reason string) error {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("RejectCoupon")

	if couponID == uuid.Nil {
		err := errors.New("coupon_id is required")
		logger.Error("Validation failed", err)
		return err
	}

	reason = strings.TrimSpace(reason)
	if reason == "" {
		err := errors.New("rejection reason is required")
		logger.Error("Validation failed", err)
		return err
	}

	statusID, err := m.resolveCouponStatusIDByName(ctx, "rejected")
	if err != nil {
		logger.Error("Reject coupon failed while resolving rejected status", err)
		return err
	}

	commandTag, err := m.DB.Exec(ctx,
		`UPDATE coupons
		SET coupon_status_id = $1,
			rejection_reason = $2
		WHERE id = $3`,
		statusID,
		reason,
		couponID,
	)
	if err != nil {
		logger.Error("Reject coupon failed", err, "coupon_id", couponID)
		return err
	}

	if commandTag.RowsAffected() == 0 {
		logger.Warn("Reject coupon failed: coupon not found", "coupon_id", couponID)
		return ErrCouponNotFound
	}

	logger.Info("Reject coupon successful",
		"coupon_id", couponID,
		"coupon_status_id", statusID,
		"rejection_reason", reason,
	)
	return nil
}

// FlagCoupon records a moderation flag on a coupon. created_at is DB-owned.
func (m *CouponModel) FlagCoupon(ctx context.Context, couponID uuid.UUID, reason string, userID *uuid.UUID) error {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("FlagCoupon")

	if couponID == uuid.Nil {
		err := errors.New("coupon_id is required")
		logger.Error("Validation failed", err)
		return err
	}

	reason = strings.TrimSpace(reason)
	if reason == "" {
		err := errors.New("reason is required")
		logger.Error("Validation failed", err)
		return err
	}

	id := uuid.New()
	_, err := m.DB.Exec(ctx,
		`INSERT INTO coupon_flags (
			id,
			coupon_id,
			reason,
			flagged_by
		)
		VALUES ($1, $2, $3, $4)`,
		id,
		couponID,
		reason,
		userID,
	)
	if err != nil {
		logger.Error("Flag coupon failed", err, "coupon_id", couponID)
		return err
	}

	logger.Info("Flag coupon successful",
		"flag_id", id,
		"coupon_id", couponID,
		"reason", reason,
		"flagged_by", userID,
	)
	return nil
}

// GetFlaggedCoupons retrieves coupons that have at least one unresolved flag,
// paginated by limit and offset. Only flags where resolved_at IS NULL are
// returned. Because the WHERE clause guarantees unresolved rows, the
// CouponFlagSummary carries only the fields that can be non-NULL at that
// point: ID, Reason, FlaggedBy, and FlaggedAt. Resolution fields are
// intentionally excluded from the SELECT to make it explicit that they carry
// no data in this result set.
func (m *CouponModel) GetFlaggedCoupons(ctx context.Context, limit, offset int) ([]*CouponWithFlag, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("GetFlaggedCoupons")

	if limit <= 0 {
		err := errors.New("limit must be greater than zero")
		logger.Error("Validation failed", err)
		return nil, err
	}
	if offset < 0 {
		err := errors.New("offset cannot be negative")
		logger.Error("Validation failed", err)
		return nil, err
	}

	// Resolution columns are intentionally omitted: WHERE f.resolved_at IS NULL
	// guarantees they are always NULL, so selecting them would populate dead
	// fields and mislead callers (§2.2 explicit over implicit).
	query := `
		SELECT` + couponAliasedColumns + `,
			f.id         AS flag_id,
			f.reason,
			f.flagged_by,
			f.created_at AS flagged_at
		FROM coupons c
		INNER JOIN coupon_flags f ON c.id = f.coupon_id
		WHERE f.resolved_at IS NULL
		ORDER BY f.created_at DESC
		LIMIT $1 OFFSET $2`

	rows, err := m.DB.Query(ctx, query, limit, offset)
	if err != nil {
		logger.Error("Get flagged coupons failed", err)
		return nil, err
	}
	defer rows.Close()

	var results []*CouponWithFlag
	for rows.Next() {
		var coupon Coupon
		var flag CouponFlagSummary

		if err := rows.Scan(
			&coupon.ID,
			&coupon.OfferID,
			&coupon.CouponStatusID,
			&coupon.Code,
			&coupon.DiscountType,
			&coupon.DiscountValue,
			&coupon.MinPurchaseAmount,
			&coupon.StartDate,
			&coupon.EndDate,
			&coupon.AffiliateURL,
			&coupon.RejectionReason,
			&coupon.CreatedAt,
			&coupon.UpdatedAt,
			&flag.ID,
			&flag.Reason,
			&flag.FlaggedBy,
			&flag.FlaggedAt,
		); err != nil {
			logger.Error("Scan flagged coupon row failed", err)
			return nil, err
		}

		results = append(results, &CouponWithFlag{Coupon: &coupon, FlagInfo: &flag})
	}

	if err := rows.Err(); err != nil {
		logger.Error("Iterate flagged coupon rows failed", err)
		return nil, err
	}

	logger.Info("Get flagged coupons successful",
		"count", len(results),
		"limit", limit,
		"offset", offset,
	)
	return results, nil
}

// GetResolvedFlaggedCoupons retrieves coupons whose flags have been resolved,
// paginated by limit and offset. Resolution fields (ResolvedAt, ResolvedBy,
// ResolutionNotes, ResolutionStatus) are guaranteed non-NULL here because the
// WHERE clause requires resolved_at IS NOT NULL. This is the correct read path
// for resolution metadata — do not add these fields to GetFlaggedCoupons where
// they would always be NULL (§2.2 explicit over implicit).
func (m *CouponModel) GetResolvedFlaggedCoupons(ctx context.Context, limit, offset int) ([]*CouponWithResolvedFlag, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("GetResolvedFlaggedCoupons")

	if limit <= 0 {
		err := errors.New("limit must be greater than zero")
		logger.Error("Validation failed", err)
		return nil, err
	}
	if offset < 0 {
		err := errors.New("offset cannot be negative")
		logger.Error("Validation failed", err)
		return nil, err
	}

	query := `
		SELECT` + couponAliasedColumns + `,
			f.id               AS flag_id,
			f.reason,
			f.flagged_by,
			f.created_at       AS flagged_at,
			f.resolved_at,
			f.resolved_by,
			f.resolution_notes,
			f.resolution_status
		FROM coupons c
		INNER JOIN coupon_flags f ON c.id = f.coupon_id
		WHERE f.resolved_at IS NOT NULL
		ORDER BY f.resolved_at DESC, f.created_at DESC
		LIMIT $1 OFFSET $2`

	rows, err := m.DB.Query(ctx, query, limit, offset)
	if err != nil {
		logger.Error("Get resolved flagged coupons failed", err)
		return nil, err
	}
	defer rows.Close()

	var results []*CouponWithResolvedFlag
	for rows.Next() {
		var coupon Coupon
		var flag CouponResolvedFlagSummary

		if err := rows.Scan(
			&coupon.ID,
			&coupon.OfferID,
			&coupon.CouponStatusID,
			&coupon.Code,
			&coupon.DiscountType,
			&coupon.DiscountValue,
			&coupon.MinPurchaseAmount,
			&coupon.StartDate,
			&coupon.EndDate,
			&coupon.AffiliateURL,
			&coupon.RejectionReason,
			&coupon.CreatedAt,
			&coupon.UpdatedAt,
			&flag.ID,
			&flag.Reason,
			&flag.FlaggedBy,
			&flag.FlaggedAt,
			&flag.ResolvedAt,
			&flag.ResolvedBy,
			&flag.ResolutionNotes,
			&flag.ResolutionStatus,
		); err != nil {
			logger.Error("Scan resolved flagged coupon row failed", err)
			return nil, err
		}

		results = append(results, &CouponWithResolvedFlag{Coupon: &coupon, FlagInfo: &flag})
	}

	if err := rows.Err(); err != nil {
		logger.Error("Iterate resolved flagged coupon rows failed", err)
		return nil, err
	}

	logger.Info("Get resolved flagged coupons successful",
		"count", len(results),
		"limit", limit,
		"offset", offset,
	)
	return results, nil
}

// UpdateCouponStatus resolves a status name from coupon_statuses and applies
// its ID to the coupon row. The old implementation wrote a freeform status
// text string directly to a column that does not exist in the current schema.
func (m *CouponModel) UpdateCouponStatus(ctx context.Context, couponID uuid.UUID, status string) error {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("UpdateCouponStatus")

	if couponID == uuid.Nil {
		err := errors.New("coupon_id is required")
		logger.Error("Validation failed", err)
		return err
	}

	statusID, err := m.resolveCouponStatusIDByName(ctx, status)
	if err != nil {
		logger.Error("Update coupon status failed while resolving status", err)
		return err
	}

	commandTag, err := m.DB.Exec(ctx,
		`UPDATE coupons
		SET coupon_status_id = $1
		WHERE id = $2`,
		statusID,
		couponID,
	)
	if err != nil {
		logger.Error("Update coupon status failed", err)
		return err
	}

	if commandTag.RowsAffected() == 0 {
		logger.Warn("Update coupon status failed: coupon not found", "coupon_id", couponID)
		return ErrCouponNotFound
	}

	logger.Info("Update coupon status successful",
		"coupon_id", couponID,
		"status_name", status,
		"coupon_status_id", statusID,
	)
	return nil
}

// ─────────────────────────────────────────────────────────────────────────────
// CouponModel — Automation
// ─────────────────────────────────────────────────────────────────────────────

// AutoExpireCoupons transitions all expired coupons (end_date < NOW()) to the
// seeded "expired" status. It is idempotent: coupons already in the expired
// status are excluded from the UPDATE via IS DISTINCT FROM.
func (m *CouponModel) AutoExpireCoupons(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("AutoExpireCoupons")

	statusID, err := m.resolveCouponStatusIDByName(ctx, "expired")
	if err != nil {
		logger.Error("Auto-expire coupons failed while resolving expired status", err)
		return err
	}

	commandTag, err := m.DB.Exec(ctx,
		`UPDATE coupons
		SET coupon_status_id = $1
		WHERE end_date IS NOT NULL
		  AND end_date < NOW()
		  AND (coupon_status_id IS DISTINCT FROM $1)`,
		statusID,
	)
	if err != nil {
		logger.Error("Auto-expire coupons failed", err)
		return err
	}

	logger.Info("Auto-expire coupons completed",
		"affected_rows", commandTag.RowsAffected(),
		"expired_status_id", statusID,
	)
	return nil
}

// SuggestCouponsForUser returns active coupons attached to the categories the
// user has already favorited. The join path is:
//
//	user_favorites -> offers (favored) -> categories -> offers (candidates) -> coupons
//
// The old implementation joined through non-existent user_behavior and deal_id
// columns.
func (m *CouponModel) SuggestCouponsForUser(ctx context.Context, userID uuid.UUID, limit int) ([]*Coupon, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("SuggestCouponsForUser")

	if userID == uuid.Nil {
		err := errors.New("user_id is required")
		logger.Error("Validation failed", err)
		return nil, err
	}
	if limit <= 0 {
		err := errors.New("limit must be greater than zero")
		logger.Error("Validation failed", err)
		return nil, err
	}

	query := `
		SELECT DISTINCT` + couponAliasedColumns + `
		FROM coupons c
		INNER JOIN offers candidate_offer
			ON candidate_offer.id = c.offer_id
		INNER JOIN categories candidate_category
			ON candidate_category.id = candidate_offer.category_id
		WHERE candidate_category.id IN (
			SELECT DISTINCT favored_offer.category_id
			FROM user_favorites uf
			INNER JOIN offers favored_offer
				ON favored_offer.id = uf.offer_id
			WHERE uf.user_id   = $1
			  AND uf.deleted_at IS NULL
		)
		  AND c.deleted_at IS NULL
		  AND c.start_date <= NOW()
		  AND (c.end_date IS NULL OR c.end_date > NOW())
		ORDER BY c.created_at DESC
		LIMIT $2`

	rows, err := m.DB.Query(ctx, query, userID, limit)
	if err != nil {
		logger.Error("Suggest coupons failed", err)
		return nil, err
	}
	defer rows.Close()

	var coupons []*Coupon
	for rows.Next() {
		var coupon Coupon
		if err := scanCouponWithAlias(rows, &coupon); err != nil {
			logger.Error("Scan suggested coupon row failed", err)
			return nil, err
		}
		coupons = append(coupons, &coupon)
	}

	if err := rows.Err(); err != nil {
		logger.Error("Iterate suggested coupon rows failed", err)
		return nil, err
	}

	logger.Info("Suggest coupons successful",
		"user_id", userID,
		"coupon_count", len(coupons),
	)
	return coupons, nil
}

// AnalyzeCouponPerformance returns usage-based coupon metrics grounded in
// coupon_usages. TotalRevenue is always "0.0000" because the current schema
// does not persist coupon-attributed revenue. The old implementation queried a
// non-existent coupon_engagements table.
func (m *CouponModel) AnalyzeCouponPerformance(ctx context.Context, couponID uuid.UUID) (*CouponPerformanceStats, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("AnalyzeCouponPerformance")

	if couponID == uuid.Nil {
		err := errors.New("coupon_id is required")
		logger.Error("Validation failed", err)
		return nil, err
	}

	var stats CouponPerformanceStats
	stats.TotalRevenue = "0.0000"

	err := m.DB.QueryRow(ctx,
		`SELECT
			COALESCE(SUM(CASE WHEN action = 'clicked'  THEN 1 ELSE 0 END), 0) AS total_clicks,
			COALESCE(SUM(CASE WHEN action = 'redeemed' THEN 1 ELSE 0 END), 0) AS total_conversions
		FROM coupon_usages
		WHERE coupon_id = $1`,
		couponID,
	).Scan(&stats.TotalClicks, &stats.TotalConversions)
	if err != nil {
		logger.Error("Analyze coupon performance failed", err)
		return nil, err
	}

	logger.Info("Analyze coupon performance successful",
		"coupon_id", couponID,
		"total_clicks", stats.TotalClicks,
		"total_conversions", stats.TotalConversions,
		"total_revenue", stats.TotalRevenue,
	)
	return &stats, nil
}

// GetTrendingCoupons retrieves active coupons ranked by recent coupon_usages
// volume, then by created_at descending as a tiebreaker. The old
// implementation ordered by a non-existent created_at heuristic without any
// usage signal.
func (m *CouponModel) GetTrendingCoupons(ctx context.Context, limit int) ([]*Coupon, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("GetTrendingCoupons")

	if limit <= 0 {
		err := errors.New("limit must be greater than zero")
		logger.Error("Validation failed", err)
		return nil, err
	}

	query := `
		SELECT` + couponAliasedColumns + `
		FROM coupons c
		LEFT JOIN coupon_usages cu ON cu.coupon_id = c.id
		WHERE c.start_date <= NOW()
		  AND (c.end_date IS NULL OR c.end_date > NOW())
		` + couponGroupByID + `
		ORDER BY COUNT(cu.id) DESC, c.created_at DESC
		LIMIT $1`

	rows, err := m.DB.Query(ctx, query, limit)
	if err != nil {
		logger.Error("Get trending coupons failed", err)
		return nil, err
	}
	defer rows.Close()

	var coupons []*Coupon
	for rows.Next() {
		var coupon Coupon
		if err := scanCouponWithAlias(rows, &coupon); err != nil {
			logger.Error("Scan trending coupon row failed", err)
			return nil, err
		}
		coupons = append(coupons, &coupon)
	}

	if err := rows.Err(); err != nil {
		logger.Error("Iterate trending coupon rows failed", err)
		return nil, err
	}

	logger.Info("GetTrendingCoupons successful", "count", len(coupons), "limit", limit)
	return coupons, nil
}

// ─────────────────────────────────────────────────────────────────────────────
// CouponUsageModel
// ─────────────────────────────────────────────────────────────────────────────

// TrackCouponUsage inserts a coupon usage event. created_at is DB-owned.
// action must be one of "clicked" or "redeemed" — any other value is rejected
// before hitting the DB, matching the coupon_usages CHECK constraint.
func (m *CouponUsageModel) TrackCouponUsage(ctx context.Context, userID, couponID uuid.UUID, action string) error {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("TrackCouponUsage")

	if userID == uuid.Nil {
		err := errors.New("user_id is required")
		logger.Error("Validation failed", err)
		return err
	}
	if couponID == uuid.Nil {
		err := errors.New("coupon_id is required")
		logger.Error("Validation failed", err)
		return err
	}

	action = strings.TrimSpace(strings.ToLower(action))
	switch action {
	case "clicked", "redeemed":
		// valid
	default:
		err := errors.New("action must be one of: clicked, redeemed")
		logger.Error("Validation failed", err)
		return err
	}

	usageID := uuid.New()
	_, err := m.DB.Exec(ctx,
		`INSERT INTO coupon_usages (
			id,
			user_id,
			coupon_id,
			action
		)
		VALUES ($1, $2, $3, $4)`,
		usageID,
		userID,
		couponID,
		action,
	)
	if err != nil {
		logger.Error("Track coupon usage failed", err)
		return err
	}

	logger.Info("Track coupon usage successful",
		"usage_id", usageID,
		"user_id", userID,
		"coupon_id", couponID,
		"action", action,
	)
	return nil
}

// ─────────────────────────────────────────────────────────────────────────────
// CouponStatusModel
// ─────────────────────────────────────────────────────────────────────────────

// GetByName retrieves a single coupon status by name (case-insensitive).
// Returns ErrCouponStatusNotFound when no row matches.
func (m *CouponStatusModel) GetByName(ctx context.Context, name string) (*CouponStatus, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("GetCouponStatusByName")

	name = strings.TrimSpace(name)
	if name == "" {
		err := errors.New("coupon status name is required")
		logger.Error("Validation failed", err)
		return nil, err
	}

	var status CouponStatus
	err := m.DB.QueryRow(ctx,
		`SELECT
			id,
			name,
			description,
			is_active,
			created_at,
			updated_at
		FROM coupon_statuses
		WHERE lower(name) = lower($1)`,
		name,
	).Scan(
		&status.ID,
		&status.Name,
		&status.Description,
		&status.IsActive,
		&status.CreatedAt,
		&status.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			logger.Warn("Coupon status not found", "name", name)
			return nil, ErrCouponStatusNotFound
		}
		logger.Error("Get coupon status by name failed", err)
		return nil, err
	}

	logger.Info("Get coupon status by name successful", "name", name, "id", status.ID)
	return &status, nil
}
