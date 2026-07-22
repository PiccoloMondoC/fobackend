// Package data provides models and database access methods for offer sponsorships and other entities.
//
// File: sdworkspace/sdbackend/internal/data/offer_sponsorships.go
//
// GTM:
//   Layer: 2.5 Catalog / Offer Domain
//   Release Class: DEFERRED
//   Reason:
//     Offer sponsorships, sponsorship bid types, and sponsorship bid minimums
//     are valid future monetization infrastructure, but they are not required
//     for the initial Platform release spine. The v1 spine requires
//     canonical offers, publication governance, affiliate links, click tracking,
//     price history, and moderation flags before expanding into paid placement
//     and sponsorship workflows.
//
// DEFERRED Rule:
//   Keep compiling.
//   Keep safe.
//   Preserve NUMERIC-safe decimal string behavior.
//   Preserve sponsorship bid-type and bid-minimum validation.
//   Preserve CPD overlap protection.
//   Preserve 30-minute sponsorship edit window.
//   Preserve soft-delete lifecycle behavior.
//   Do not add new features.
//   Do not route into v1 UI/API expansion.
//   Do not block deployment on this file unless it breaks the build.
package data

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"time"

	"github.com/PiccoloMondoC/sdworkspace/sdbackend/internal/logging"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	// offerSponsorshipMoneyPattern is intentionally file-specific to avoid duplicate
	// package-level symbol collisions with other money-bearing model files.
	offerSponsorshipMoneyPattern = regexp.MustCompile(`^\d+(\.\d{1,4})?$`)
)

const offerSponsorshipSelectColumns = `id, offer_id, merchant_id, start_date, end_date, sponsorship_bid_type_id, bid_amount::text, COALESCE(max_budget::text, ''), budget_spent::text, impressions_served, clicks_served, deleted_at, created_at, updated_at`

// OfferSponsorship represents a merchant's sponsorship of a specific offer.
// Money-bearing numeric fields are modeled as canonical decimal strings so the
// Go layer preserves PostgreSQL NUMERIC(19,4) semantics without float drift.
type OfferSponsorship struct {
	ID                   uuid.UUID `json:"id" db:"id"`
	OfferID              uuid.UUID `json:"offer_id" db:"offer_id"`
	MerchantID           uuid.UUID `json:"merchant_id" db:"merchant_id"`
	StartDate            time.Time `json:"start_date" db:"start_date"`
	EndDate              time.Time `json:"end_date" db:"end_date"`
	SponsorshipBidTypeID uuid.UUID `json:"sponsorship_bid_type_id" db:"sponsorship_bid_type_id"`

	BidAmount   string  `json:"bid_amount" db:"bid_amount"`
	MaxBudget   *string `json:"max_budget,omitempty" db:"max_budget"`
	BudgetSpent string  `json:"budget_spent" db:"budget_spent"`

	ImpressionsServed int       `json:"impressions_served" db:"impressions_served"`
	ClicksServed      int       `json:"clicks_served" db:"clicks_served"`
	DeletedAt         *time.Time `json:"-" db:"deleted_at"`
	CreatedAt         time.Time `json:"created_at" db:"created_at"`
	UpdatedAt         time.Time `json:"updated_at" db:"updated_at"`
}

// SponsorshipBidType represents a supported bid strategy for offer sponsorships.
type SponsorshipBidType struct {
	ID          uuid.UUID `json:"id" db:"id"`
	Code        string    `json:"code" db:"code"`
	Name        string    `json:"name" db:"name"`
	Description string    `json:"description" db:"description"`
	CreatedAt   time.Time `json:"created_at" db:"created_at"`
}

// SponsorshipBidMinimum defines the lowest acceptable bid and budget for each sponsorship bid type.
type SponsorshipBidMinimum struct {
	ID           uuid.UUID `json:"id" db:"id"`
	BidTypeID    uuid.UUID `json:"bid_type_id" db:"bid_type_id"`
	MinBidAmount string    `json:"min_bid_amount" db:"min_bid_amount"`
	MinMaxBudget string    `json:"min_max_budget" db:"min_max_budget"`
	CreatedAt    time.Time `json:"created_at" db:"created_at"`
	UpdatedAt    time.Time `json:"updated_at" db:"updated_at"`
}

// OfferSponsorshipModel is the structure which holds the DB instance.
type OfferSponsorshipModel struct {
	DB     *pgxpool.Pool
	Logger *logging.Logger
}

// SponsorshipBidTypeModel is the structure which holds the DB instance.
type SponsorshipBidTypeModel struct {
	DB     *pgxpool.Pool
	Logger *logging.Logger
}

// SponsorshipBidMinimumModel is the structure which holds the DB instance.
type SponsorshipBidMinimumModel struct {
	DB     *pgxpool.Pool
	Logger *logging.Logger
}

// validateOfferSponsorshipMoney validates a canonical decimal money string.
func validateOfferSponsorshipMoney(name, value string, allowZero bool) error {
	if value == "" {
		return fmt.Errorf("%s is required", name)
	}
	if !offerSponsorshipMoneyPattern.MatchString(value) {
		return fmt.Errorf("%s must be a non-negative decimal with up to 4 fractional digits", name)
	}
	if !allowZero && (value == "0" || value == "0.0" || value == "0.00" || value == "0.000" || value == "0.0000") {
		return fmt.Errorf("%s must be greater than zero", name)
	}
	return nil
}

// offerSponsorshipOptionalMoneyParam converts an optional decimal string pointer
// into a nil-or-string SQL parameter.
func offerSponsorshipOptionalMoneyParam(value *string) any {
	if value == nil {
		return nil
	}
	return *value
}

// offerSponsorshipNullableString converts the scanned COALESCE(max_budget::text, '')
// representation back to a nil pointer when the DB value is NULL.
func offerSponsorshipNullableString(s string) *string {
	if s == "" {
		return nil
	}
	v := s
	return &v
}

// offerSponsorshipScanner abstracts both pgx.Row and pgx.Rows for shared scan logic.
type offerSponsorshipScanner interface {
	Scan(dest ...any) error
}

// scanOfferSponsorship centralizes the canonical scan order for offer sponsorship reads.
func scanOfferSponsorship(src offerSponsorshipScanner) (*OfferSponsorship, error) {
	var (
		ds           OfferSponsorship
		maxBudgetRaw string
	)

	err := src.Scan(
		&ds.ID,
		&ds.OfferID,
		&ds.MerchantID,
		&ds.StartDate,
		&ds.EndDate,
		&ds.SponsorshipBidTypeID,
		&ds.BidAmount,
		&maxBudgetRaw,
		&ds.BudgetSpent,
		&ds.ImpressionsServed,
		&ds.ClicksServed,
		&ds.DeletedAt,
		&ds.CreatedAt,
		&ds.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}

	ds.MaxBudget = offerSponsorshipNullableString(maxBudgetRaw)
	return &ds, nil
}

// HasCPDOverlap checks whether a CPD sponsorship overlaps the given date range.
// excludeID is optional and is used by Update so the row does not conflict with itself.
func (m *OfferSponsorshipModel) HasCPDOverlap(
	ctx context.Context,
	offerID uuid.UUID,
	startDate time.Time,
	endDate time.Time,
	excludeID *uuid.UUID,
) (bool, error) {
	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("HasCPDOverlap")

	if offerID == uuid.Nil {
		return false, errors.New("offer ID is required")
	}
	if startDate.IsZero() || endDate.IsZero() || !endDate.After(startDate) {
		return false, errors.New("invalid sponsorship date range")
	}

	query := `
		SELECT EXISTS (
			SELECT 1
			FROM offer_sponsorships os
			JOIN sponsorship_bid_types sbt
			  ON sbt.id = os.sponsorship_bid_type_id
			WHERE os.offer_id = $1
			  AND os.deleted_at IS NULL
			  AND os.start_date < $3
			  AND os.end_date > $2
			  AND sbt.code = 'CPD'
			  AND ($4::uuid IS NULL OR os.id <> $4)
		)
	`

	var exists bool
	err := m.DB.QueryRow(ctx, query, offerID, startDate.UTC(), endDate.UTC(), excludeID).Scan(&exists)
	if err != nil {
		logger.Error("Failed to check CPD overlap", err)
		return false, err
	}

	return exists, nil
}

// Insert inserts a new offer sponsorship using the single canonical write path.
// The database owns id, created_at, and updated_at.
func (m *OfferSponsorshipModel) Insert(ctx context.Context, ds *OfferSponsorship) error {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("InsertOfferSponsorship")

	if ds == nil {
		err := errors.New("offer sponsorship is required")
		logger.Error("Validation failed", err)
		return err
	}
	if ds.OfferID == uuid.Nil {
		err := errors.New("offer ID is required")
		logger.Error("Validation failed", err)
		return err
	}
	if ds.MerchantID == uuid.Nil {
		err := errors.New("merchant ID is required")
		logger.Error("Validation failed", err)
		return err
	}
	if ds.SponsorshipBidTypeID == uuid.Nil {
		err := errors.New("sponsorship bid type ID is required")
		logger.Error("Validation failed", err)
		return err
	}
	if ds.StartDate.IsZero() || ds.EndDate.IsZero() || !ds.EndDate.After(ds.StartDate) {
		err := errors.New("end date must be after start date")
		logger.Error("Validation failed", err)
		return err
	}
	if err := validateOfferSponsorshipMoney("bid amount", ds.BidAmount, false); err != nil {
		logger.Error("Validation failed", err)
		return err
	}
	if ds.MaxBudget != nil {
		if err := validateOfferSponsorshipMoney("max budget", *ds.MaxBudget, true); err != nil {
			logger.Error("Validation failed", err)
			return err
		}
	}
	if ds.BudgetSpent == "" {
		ds.BudgetSpent = "0.0000"
	}
	if err := validateOfferSponsorshipMoney("budget spent", ds.BudgetSpent, true); err != nil {
		logger.Error("Validation failed", err)
		return err
	}

	var (
		bidCode      string
		minBid       string
		minMaxBudget string
		bidOK        bool
		budgetOK     bool
	)

	rulesQuery := `
		SELECT
			sbt.code,
			sbm.min_bid_amount::text,
			sbm.min_max_budget::text,
			($2::numeric >= sbm.min_bid_amount) AS bid_ok,
			CASE
				WHEN sbt.code IN ('CPC', 'CPI') THEN ($3::numeric IS NOT NULL AND $3::numeric >= sbm.min_max_budget)
				WHEN sbt.code = 'CPD' THEN ($3::numeric IS NULL)
				ELSE FALSE
			END AS budget_ok
		FROM sponsorship_bid_types sbt
		JOIN sponsorship_bid_minimums sbm
		  ON sbm.bid_type_id = sbt.id
		WHERE sbt.id = $1
	`

	err := m.DB.QueryRow(ctx, rulesQuery, ds.SponsorshipBidTypeID, ds.BidAmount, offerSponsorshipOptionalMoneyParam(ds.MaxBudget)).
		Scan(&bidCode, &minBid, &minMaxBudget, &bidOK, &budgetOK)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			err = errors.New("invalid sponsorship bid type or missing bid minimums")
		}
		logger.Error("Failed to load bid rules", err)
		return err
	}

	if !bidOK {
		err := fmt.Errorf("bid amount must be at least %s for bid type %s", minBid, bidCode)
		logger.Error("Bid validation failed", err)
		return err
	}
	if !budgetOK {
		switch bidCode {
		case "CPC", "CPI":
			err = fmt.Errorf("max budget must be at least %s for bid type %s", minMaxBudget, bidCode)
		case "CPD":
			err = errors.New("max budget must not be set for CPD bid type")
		default:
			err = fmt.Errorf("unsupported bid type code: %s", bidCode)
		}
		logger.Error("Budget validation failed", err)
		return err
	}

	if bidCode == "CPD" {
		conflict, err := m.HasCPDOverlap(ctx, ds.OfferID, ds.StartDate.UTC(), ds.EndDate.UTC(), nil)
		if err != nil {
			logger.Error("Failed CPD overlap check", err)
			return err
		}
		if conflict {
			err := errors.New("a CPD sponsorship already exists during the selected timeframe")
			logger.Error("CPD overlap detected", err)
			return err
		}
	}

	insertQuery := `
		INSERT INTO offer_sponsorships (
			offer_id,
			merchant_id,
			start_date,
			end_date,
			sponsorship_bid_type_id,
			bid_amount,
			max_budget,
			budget_spent,
			impressions_served,
			clicks_served
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10
		)
		RETURNING id, created_at, updated_at
	`

	err = m.DB.QueryRow(
		ctx,
		insertQuery,
		ds.OfferID,
		ds.MerchantID,
		ds.StartDate.UTC(),
		ds.EndDate.UTC(),
		ds.SponsorshipBidTypeID,
		ds.BidAmount,
		offerSponsorshipOptionalMoneyParam(ds.MaxBudget),
		ds.BudgetSpent,
		ds.ImpressionsServed,
		ds.ClicksServed,
	).Scan(&ds.ID, &ds.CreatedAt, &ds.UpdatedAt)
	if err != nil {
		logger.Error("Failed to insert offer sponsorship", err)
		return err
	}

	logger.Info("Offer sponsorship inserted successfully", "sponsorship_id", ds.ID)
	return nil
}

// SponsorOffer is a convenience wrapper over the single canonical Insert path.
func (m *OfferSponsorshipModel) SponsorOffer(
	ctx context.Context,
	offerID uuid.UUID,
	merchantID uuid.UUID,
	startDate time.Time,
	endDate time.Time,
	bidTypeID uuid.UUID,
	bidAmount string,
	maxBudget *string,
) error {
	ds := &OfferSponsorship{
		OfferID:              offerID,
		MerchantID:           merchantID,
		StartDate:            startDate,
		EndDate:              endDate,
		SponsorshipBidTypeID: bidTypeID,
		BidAmount:            bidAmount,
		MaxBudget:            maxBudget,
		BudgetSpent:          "0.0000",
		ImpressionsServed:    0,
		ClicksServed:         0,
	}

	return m.Insert(ctx, ds)
}

// GetByID retrieves an offer sponsorship by its unique ID.
func (m *OfferSponsorshipModel) GetByID(ctx context.Context, id uuid.UUID) (*OfferSponsorship, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("GetByID")

	if id == uuid.Nil {
		err := errors.New("offer sponsorship ID is required")
		logger.Error("Validation failed", err)
		return nil, err
	}

	query := fmt.Sprintf(`
		SELECT %s
		FROM offer_sponsorships
		WHERE id = $1
		  AND deleted_at IS NULL
		LIMIT 1
	`, offerSponsorshipSelectColumns)

	ds, err := scanOfferSponsorship(m.DB.QueryRow(ctx, query, id))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			logger.Warn("Offer sponsorship not found", "id", id)
			return nil, ErrOfferSponsorshipNotFound
		}
		logger.Error("Failed to retrieve offer sponsorship", err, "id", id)
		return nil, err
	}

	logger.Info("Successfully retrieved offer sponsorship", "id", ds.ID)
	return ds, nil
}

// GetByOfferID retrieves sponsorships for a specific offer.
func (m *OfferSponsorshipModel) GetByOfferID(ctx context.Context, offerID uuid.UUID) ([]*OfferSponsorship, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("GetByOfferID")

	if offerID == uuid.Nil {
		err := errors.New("offer ID is required")
		logger.Error("Validation failed", err)
		return nil, err
	}

	query := fmt.Sprintf(`
		SELECT %s
		FROM offer_sponsorships
		WHERE offer_id = $1
		  AND deleted_at IS NULL
		ORDER BY created_at DESC
	`, offerSponsorshipSelectColumns)

	rows, err := m.DB.Query(ctx, query, offerID)
	if err != nil {
		logger.Error("Query execution failed", err)
		return nil, err
	}

	results, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (*OfferSponsorship, error) {
		return scanOfferSponsorship(row)
	})
	if err != nil {
		logger.Error("Row collection failed", err, "offer_id", offerID)
		return nil, err
	}

	logger.Info("Sponsorships fetched", "offer_id", offerID, "count", len(results))
	return results, nil
}

// GetByMerchantID retrieves sponsorships for a specific merchant.
func (m *OfferSponsorshipModel) GetByMerchantID(ctx context.Context, merchantID uuid.UUID) ([]*OfferSponsorship, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("GetByMerchantID")

	if merchantID == uuid.Nil {
		err := errors.New("merchant ID is required")
		logger.Error("Validation failed", err)
		return nil, err
	}

	query := fmt.Sprintf(`
		SELECT %s
		FROM offer_sponsorships
		WHERE merchant_id = $1
		  AND deleted_at IS NULL
		ORDER BY created_at DESC
	`, offerSponsorshipSelectColumns)

	rows, err := m.DB.Query(ctx, query, merchantID)
	if err != nil {
		logger.Error("Query execution failed", err)
		return nil, err
	}

	results, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (*OfferSponsorship, error) {
		return scanOfferSponsorship(row)
	})
	if err != nil {
		logger.Error("Row collection failed", err, "merchant_id", merchantID)
		return nil, err
	}

	logger.Info("Merchant sponsorships fetched", "merchant_id", merchantID, "count", len(results))
	return results, nil
}

// List retrieves a paginated list of offer sponsorships.
func (m *OfferSponsorshipModel) List(ctx context.Context, limit, offset int) ([]*OfferSponsorship, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("List")

	if limit <= 0 {
		err := errors.New("limit must be greater than zero")
		logger.Error("Validation failed", err)
		return nil, err
	}
	if limit > 100 {
		err := errors.New("limit must not exceed 100")
		logger.Error("Validation failed", err)
		return nil, err
	}
	if offset < 0 {
		err := errors.New("offset must be non-negative")
		logger.Error("Validation failed", err)
		return nil, err
	}

	query := fmt.Sprintf(`
		SELECT %s
		FROM offer_sponsorships
		WHERE deleted_at IS NULL
		ORDER BY created_at DESC
		LIMIT $1 OFFSET $2
	`, offerSponsorshipSelectColumns)

	rows, err := m.DB.Query(ctx, query, limit, offset)
	if err != nil {
		logger.Error("Query execution failed", err)
		return nil, err
	}

	sponsorships, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (*OfferSponsorship, error) {
		return scanOfferSponsorship(row)
	})
	if err != nil {
		logger.Error("Row collection failed", err)
		return nil, err
	}

	logger.Info("Offer sponsorships listed successfully", "count", len(sponsorships))
	return sponsorships, nil
}

// Update modifies an existing offer sponsorship.
// offer_id and merchant_id remain immutable. Sponsorships may only be edited
// within 30 minutes of creation.
func (m *OfferSponsorshipModel) Update(ctx context.Context, ds *OfferSponsorship) error {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("UpdateOfferSponsorship")

	if ds == nil {
		err := errors.New("offer sponsorship is required")
		logger.Error("Validation failed", err)
		return err
	}
	if ds.ID == uuid.Nil {
		err := errors.New("sponsorship ID is required for update")
		logger.Error("Validation failed", err)
		return err
	}
	if ds.SponsorshipBidTypeID == uuid.Nil {
		err := errors.New("sponsorship bid type ID is required")
		logger.Error("Validation failed", err)
		return err
	}
	if ds.StartDate.IsZero() || ds.EndDate.IsZero() || !ds.EndDate.After(ds.StartDate) {
		err := errors.New("end date must be after start date")
		logger.Error("Validation failed", err)
		return err
	}
	if err := validateOfferSponsorshipMoney("bid amount", ds.BidAmount, false); err != nil {
		logger.Error("Validation failed", err)
		return err
	}
	if ds.MaxBudget != nil {
		if err := validateOfferSponsorshipMoney("max budget", *ds.MaxBudget, true); err != nil {
			logger.Error("Validation failed", err)
			return err
		}
	}

	var existing OfferSponsorship
	getQuery := `
		SELECT offer_id, merchant_id, created_at
		FROM offer_sponsorships
		WHERE id = $1
	`
	err := m.DB.QueryRow(ctx, getQuery, ds.ID).Scan(&existing.OfferID, &existing.MerchantID, &existing.CreatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			logger.Warn("Offer sponsorship not found", "id", ds.ID)
			return ErrOfferSponsorshipNotFound
		}
		logger.Error("Failed to fetch existing sponsorship", err)
		return err
	}

	if time.Since(existing.CreatedAt.UTC()) > 30*time.Minute {
		err := errors.New("sponsorship cannot be edited after the 30-minute creation window")
		logger.Error("Edit window expired", err)
		return err
	}

	if ds.OfferID != existing.OfferID {
		err := errors.New("offer ID cannot be changed")
		logger.Error("Attempted change to immutable offer ID", err)
		return err
	}
	if ds.MerchantID != existing.MerchantID {
		err := errors.New("merchant ID cannot be changed")
		logger.Error("Attempted change to immutable merchant ID", err)
		return err
	}

	var (
		bidCode      string
		minBid       string
		minMaxBudget string
		bidOK        bool
		budgetOK     bool
	)

	rulesQuery := `
		SELECT
			sbt.code,
			sbm.min_bid_amount::text,
			sbm.min_max_budget::text,
			($2::numeric >= sbm.min_bid_amount) AS bid_ok,
			CASE
				WHEN sbt.code IN ('CPC', 'CPI') THEN ($3::numeric IS NOT NULL AND $3::numeric >= sbm.min_max_budget)
				WHEN sbt.code = 'CPD' THEN ($3::numeric IS NULL)
				ELSE FALSE
			END AS budget_ok
		FROM sponsorship_bid_types sbt
		JOIN sponsorship_bid_minimums sbm
		  ON sbm.bid_type_id = sbt.id
		WHERE sbt.id = $1
	`

	err = m.DB.QueryRow(ctx, rulesQuery, ds.SponsorshipBidTypeID, ds.BidAmount, offerSponsorshipOptionalMoneyParam(ds.MaxBudget)).
		Scan(&bidCode, &minBid, &minMaxBudget, &bidOK, &budgetOK)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			err = errors.New("invalid sponsorship bid type or missing bid minimums")
		}
		logger.Error("Failed to load bid rules", err)
		return err
	}

	if !bidOK {
		err := fmt.Errorf("bid amount must be at least %s for bid type %s", minBid, bidCode)
		logger.Error("Bid validation failed", err)
		return err
	}
	if !budgetOK {
		switch bidCode {
		case "CPC", "CPI":
			err = fmt.Errorf("max budget must be at least %s for bid type %s", minMaxBudget, bidCode)
		case "CPD":
			err = errors.New("max budget must not be set for CPD bid type")
		default:
			err = fmt.Errorf("unsupported bid type code: %s", bidCode)
		}
		logger.Error("Budget validation failed", err)
		return err
	}

	if bidCode == "CPD" {
		conflict, err := m.HasCPDOverlap(ctx, ds.OfferID, ds.StartDate.UTC(), ds.EndDate.UTC(), &ds.ID)
		if err != nil {
			logger.Error("Failed CPD overlap check", err)
			return err
		}
		if conflict {
			err := errors.New("a CPD sponsorship already exists during the selected timeframe")
			logger.Error("CPD overlap detected", err)
			return err
		}
	}

	updateQuery := `
		UPDATE offer_sponsorships
		SET start_date = $1,
			end_date = $2,
			sponsorship_bid_type_id = $3,
			bid_amount = $4,
			max_budget = $5,
			updated_at = NOW()
		WHERE id = $6
		  AND deleted_at IS NULL
		RETURNING updated_at
	`

	err = m.DB.QueryRow(
		ctx,
		updateQuery,
		ds.StartDate.UTC(),
		ds.EndDate.UTC(),
		ds.SponsorshipBidTypeID,
		ds.BidAmount,
		offerSponsorshipOptionalMoneyParam(ds.MaxBudget),
		ds.ID,
	).Scan(&ds.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			logger.Warn("Offer sponsorship not found during update", "id", ds.ID)
			return ErrOfferSponsorshipNotFound
		}
		logger.Error("Failed to update offer sponsorship", err)
		return err
	}

	logger.Info("Offer sponsorship updated successfully", "sponsorship_id", ds.ID)
	return nil
}


// SoftDelete marks an offer sponsorship record as deleted without removing the row.
// This is the standard business-lifecycle removal path when soft delete is supported.
func (m *OfferSponsorshipModel) SoftDelete(ctx context.Context, id uuid.UUID) error {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("SoftDeleteOfferSponsorship")

	if id == uuid.Nil {
		err := errors.New("offer sponsorship ID is required")
		logger.Error("Validation failed", err)
		return err
	}

	var deletedAt time.Time
	err := m.DB.QueryRow(ctx, `
		UPDATE offer_sponsorships
		SET deleted_at = NOW(),
			updated_at = NOW()
		WHERE id = $1
		AND deleted_at IS NULL
		RETURNING deleted_at
	`, id).Scan(&deletedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			logger.Warn("Offer sponsorship not found for soft delete", "offer_sponsorship_id", id)
			return ErrOfferSponsorshipNotFound
		}
		logger.Error("Failed to soft delete offer sponsorship", err)
		return err
	}

	logger.Info("Offer sponsorship soft deleted successfully",
		"offer_sponsorship_id", id,
		"deleted_at", deletedAt,
	)
	return nil
}


// Delete hard-deletes an offer sponsorship record from the database.
// This is an explicit destructive operation intended for purge, maintenance,
// or other deliberate administrative cleanup paths.
func (m *OfferSponsorshipModel) Delete(ctx context.Context, id uuid.UUID) error {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("DeleteOfferSponsorship")

	if id == uuid.Nil {
		err := errors.New("offer sponsorship ID is required")
		logger.Error("Validation failed", err)
		return err
	}

	var deletedID uuid.UUID
	err := m.DB.QueryRow(ctx, `
		DELETE FROM offer_sponsorships
		WHERE id = $1
		RETURNING id
	`, id).Scan(&deletedID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			logger.Warn("Offer sponsorship not found for delete", "offer_sponsorship_id", id)
			return ErrOfferSponsorshipNotFound
		}
		logger.Error("Failed to delete offer sponsorship", err)
		return err
	}

	logger.Info("Offer sponsorship deleted successfully", "offer_sponsorship_id", deletedID)
	return nil
}


// GetMinimumsByBidType returns the bid type code, minimum bid amount, and minimum max budget.
func (m *SponsorshipBidMinimumModel) GetMinimumsByBidType(
	ctx context.Context,
	bidTypeID uuid.UUID,
) (string, string, string, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	if bidTypeID == uuid.Nil {
		return "", "", "", errors.New("bid type ID is required")
	}

	query := `
		SELECT sbt.code, sbm.min_bid_amount::text, sbm.min_max_budget::text
		FROM sponsorship_bid_types sbt
		JOIN sponsorship_bid_minimums sbm ON sbm.bid_type_id = sbt.id
		WHERE sbt.id = $1
	`

	var code string
	var minBid string
	var minBudget string

	err := m.DB.QueryRow(ctx, query, bidTypeID).Scan(&code, &minBid, &minBudget)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", "", "", errors.New("invalid sponsorship bid type or missing bid minimums")
		}
		return "", "", "", err
	}

	return code, minBid, minBudget, nil
}