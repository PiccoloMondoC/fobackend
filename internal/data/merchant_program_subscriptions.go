// Package data provides models and database access methods for merchant
// program subscriptions.
//
// sdworkspace/sdbackend/internal/data/merchant_program_subscriptions.go
//
// GTM:
//
//	Layer: 2.4 Merchant / Future Offering Domain
//	Release Class: SPINE
//	Reason:
//	  merchant_program_subscriptions is release-critical merchant monetization
//	  infrastructure. It connects a merchant to a merchant program plan over
//	  time and determines whether a merchant currently has a pending, active,
//	  paused, suspended, cancelled, or expired subscription relationship to a
//	  plan.
//
//	  Future Offering is Sagrenti's core business object. Merchant program
//	  subscriptions are the durable merchant-plan lifecycle records that later
//	  services will use to resolve merchant program access, Future Offering
//	  readiness, entitlement eligibility, and Merchant Center plan state.
//
//	  This file is not billing ledger logic, payment processing, checkout,
//	  invoice handling, fee calculation, or merchant-of-record logic. Those
//	  concerns belong to later billing/settlement files and to merchant-owned
//	  commerce workflows.
//
// SPINE Rule:
//
//	Keep compiling.
//	Keep production-ready.
//	Preserve subscription-status integrity.
//	Preserve billing-period integrity.
//	Preserve one-current-subscription-per-merchant invariant.
//	Preserve merchant and plan foreign-key readiness.
//	Preserve soft-delete lifecycle semantics.
//	Preserve DB-owned lifecycle timestamps.
//	Block deployment if this file breaks build, merchant program subscription
//	persistence, current subscription resolution, Future Offering access
//	readiness, or merchant billing plan integrity.
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
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// MerchantProgramSubscriptionStatus is the controlled vocabulary for merchant
// program subscription lifecycle status.
type MerchantProgramSubscriptionStatus string

const (
	// SubscriptionStatusPending means the subscription exists but is not yet
	// active.
	SubscriptionStatusPending MerchantProgramSubscriptionStatus = "pending"

	// SubscriptionStatusActive means the subscription is active and may grant
	// merchant program access through service-layer entitlement checks.
	SubscriptionStatusActive MerchantProgramSubscriptionStatus = "active"

	// SubscriptionStatusPaused means the subscription is temporarily paused but
	// still occupies the merchant's current-subscription slot.
	SubscriptionStatusPaused MerchantProgramSubscriptionStatus = "paused"

	// SubscriptionStatusCancelled means the subscription has been cancelled and
	// no longer occupies the merchant's current-subscription slot.
	SubscriptionStatusCancelled MerchantProgramSubscriptionStatus = "cancelled"

	// SubscriptionStatusExpired means the subscription has expired and no longer
	// occupies the merchant's current-subscription slot.
	SubscriptionStatusExpired MerchantProgramSubscriptionStatus = "expired"

	// SubscriptionStatusSuspended means the subscription is suspended but still
	// occupies the merchant's current-subscription slot.
	SubscriptionStatusSuspended MerchantProgramSubscriptionStatus = "suspended"
)

// MerchantProgramSubscriptionBillingPeriod is the controlled vocabulary for
// merchant program subscription billing cadence.
type MerchantProgramSubscriptionBillingPeriod string

const (
	// BillingPeriodMonthly means the subscription uses monthly billing cadence.
	BillingPeriodMonthly MerchantProgramSubscriptionBillingPeriod = "monthly"

	// BillingPeriodAnnual means the subscription uses annual billing cadence.
	BillingPeriodAnnual MerchantProgramSubscriptionBillingPeriod = "annual"

	// BillingPeriodCustom means the subscription uses a non-standard billing
	// cadence. Custom billing terms belong to later billing files, not this
	// lifecycle table.
	BillingPeriodCustom MerchantProgramSubscriptionBillingPeriod = "custom"
)

const merchantProgramSubscriptionSelectColumns = `
	id,
	merchant_id,
	plan_id,
	status,
	billing_period,
	started_at,
	expires_at,
	cancelled_at,
	created_at,
	updated_at,
	deleted_at
`

// MerchantProgramSubscription represents a row in
// merchant_program_subscriptions.
type MerchantProgramSubscription struct {
	ID            uuid.UUID                                `json:"id" db:"id"`
	MerchantID    uuid.UUID                                `json:"merchant_id" db:"merchant_id"`
	PlanID        uuid.UUID                                `json:"plan_id" db:"plan_id"`
	Status        MerchantProgramSubscriptionStatus         `json:"status" db:"status"`
	BillingPeriod MerchantProgramSubscriptionBillingPeriod  `json:"billing_period" db:"billing_period"`
	StartedAt     *time.Time                               `json:"started_at,omitempty" db:"started_at"`
	ExpiresAt     *time.Time                               `json:"expires_at,omitempty" db:"expires_at"`
	CancelledAt   *time.Time                               `json:"cancelled_at,omitempty" db:"cancelled_at"`
	CreatedAt     time.Time                                `json:"created_at" db:"created_at"`
	UpdatedAt     time.Time                                `json:"updated_at" db:"updated_at"`
	DeletedAt     *time.Time                               `json:"deleted_at,omitempty" db:"deleted_at"`
}

// MerchantProgramSubscriptionModel owns persistence for merchant program
// subscriptions.
type MerchantProgramSubscriptionModel struct {
	DB     *pgxpool.Pool
	Logger *logging.Logger
}


func (m *MerchantProgramSubscriptionModel) validate() error {
	if m == nil {
		return errors.New("merchant program subscription model is required")
	}
	if m.DB == nil {
		return errors.New("merchant program subscription model database pool is required")
	}
	if m.Logger == nil {
		return errors.New("merchant program subscription model logger is required")
	}
	return nil
}


func scanMerchantProgramSubscription(row pgx.Row, subscription *MerchantProgramSubscription) error {
	return row.Scan(
		&subscription.ID,
		&subscription.MerchantID,
		&subscription.PlanID,
		&subscription.Status,
		&subscription.BillingPeriod,
		&subscription.StartedAt,
		&subscription.ExpiresAt,
		&subscription.CancelledAt,
		&subscription.CreatedAt,
		&subscription.UpdatedAt,
		&subscription.DeletedAt,
	)
}

func scanMerchantProgramSubscriptionFromRows(rows pgx.Rows, subscription *MerchantProgramSubscription) error {
	return rows.Scan(
		&subscription.ID,
		&subscription.MerchantID,
		&subscription.PlanID,
		&subscription.Status,
		&subscription.BillingPeriod,
		&subscription.StartedAt,
		&subscription.ExpiresAt,
		&subscription.CancelledAt,
		&subscription.CreatedAt,
		&subscription.UpdatedAt,
		&subscription.DeletedAt,
	)
}

// NormalizeMerchantProgramSubscriptionStatus trims and canonicalizes a merchant
// program subscription status.
func NormalizeMerchantProgramSubscriptionStatus(status MerchantProgramSubscriptionStatus) MerchantProgramSubscriptionStatus {
	return MerchantProgramSubscriptionStatus(strings.ToLower(strings.TrimSpace(string(status))))
}

// IsValidMerchantProgramSubscriptionStatus reports whether status is allowed by
// the merchant_program_subscriptions status CHECK constraint.
func IsValidMerchantProgramSubscriptionStatus(status MerchantProgramSubscriptionStatus) bool {
	switch NormalizeMerchantProgramSubscriptionStatus(status) {
	case SubscriptionStatusPending,
		SubscriptionStatusActive,
		SubscriptionStatusPaused,
		SubscriptionStatusCancelled,
		SubscriptionStatusExpired,
		SubscriptionStatusSuspended:
		return true
	default:
		return false
	}
}

// NormalizeMerchantProgramSubscriptionBillingPeriod trims and canonicalizes a
// merchant program subscription billing period.
func NormalizeMerchantProgramSubscriptionBillingPeriod(period MerchantProgramSubscriptionBillingPeriod) MerchantProgramSubscriptionBillingPeriod {
	return MerchantProgramSubscriptionBillingPeriod(strings.ToLower(strings.TrimSpace(string(period))))
}

// IsValidMerchantProgramSubscriptionBillingPeriod reports whether period is
// allowed by the merchant_program_subscriptions billing_period CHECK constraint.
func IsValidMerchantProgramSubscriptionBillingPeriod(period MerchantProgramSubscriptionBillingPeriod) bool {
	switch NormalizeMerchantProgramSubscriptionBillingPeriod(period) {
	case BillingPeriodMonthly, BillingPeriodAnnual, BillingPeriodCustom:
		return true
	default:
		return false
	}
}

func normalizeMerchantProgramSubscription(subscription *MerchantProgramSubscription) {
	subscription.Status = NormalizeMerchantProgramSubscriptionStatus(subscription.Status)
	subscription.BillingPeriod = NormalizeMerchantProgramSubscriptionBillingPeriod(subscription.BillingPeriod)
}

func validateMerchantProgramSubscriptionDateOrder(startedAt, expiresAt *time.Time) error {
	if startedAt != nil && expiresAt != nil && !expiresAt.After(*startedAt) {
		return errors.New("merchant program subscription expires_at must be after started_at")
	}
	return nil
}

func validateMerchantProgramSubscriptionForInsert(subscription *MerchantProgramSubscription) error {
	if subscription == nil {
		return errors.New("merchant program subscription is required")
	}
	if subscription.MerchantID == uuid.Nil {
		return errors.New("merchant program subscription merchant ID is required")
	}
	if subscription.PlanID == uuid.Nil {
		return errors.New("merchant program subscription plan ID is required")
	}

	normalizeMerchantProgramSubscription(subscription)

	if subscription.Status == "" {
		subscription.Status = SubscriptionStatusPending
	}
	if !IsValidMerchantProgramSubscriptionStatus(subscription.Status) {
		return fmt.Errorf("invalid merchant program subscription status: %s", subscription.Status)
	}

	if subscription.BillingPeriod == "" {
		subscription.BillingPeriod = BillingPeriodMonthly
	}
	if !IsValidMerchantProgramSubscriptionBillingPeriod(subscription.BillingPeriod) {
		return fmt.Errorf("invalid merchant program subscription billing period: %s", subscription.BillingPeriod)
	}

	return validateMerchantProgramSubscriptionDateOrder(subscription.StartedAt, subscription.ExpiresAt)
}

func validateMerchantProgramSubscriptionID(id uuid.UUID) error {
	if id == uuid.Nil {
		return errors.New("merchant program subscription ID is required")
	}
	return nil
}

func validateMerchantProgramSubscriptionPagination(limit, offset int) error {
	if limit <= 0 || limit > 100 {
		return errors.New("limit must be between 1 and 100")
	}
	if offset < 0 {
		return errors.New("offset must be non-negative")
	}
	return nil
}

func validateMerchantProgramSubscriptionStatus(status MerchantProgramSubscriptionStatus) (MerchantProgramSubscriptionStatus, error) {
	status = NormalizeMerchantProgramSubscriptionStatus(status)
	if !IsValidMerchantProgramSubscriptionStatus(status) {
		return "", fmt.Errorf("invalid merchant program subscription status: %s", status)
	}
	return status, nil
}

func translateMerchantProgramSubscriptionWriteError(err error, merchantID, planID uuid.UUID) error {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return err
	}

	switch pgErr.Code {
	case "23505":
		if pgErr.ConstraintName == "ux_merchant_program_subscriptions_one_active" {
			return fmt.Errorf(
				"merchant %s already has a current merchant program subscription",
				merchantID,
			)
		}
		return errors.New("merchant program subscription already exists")
	case "23503":
		switch {
		case strings.Contains(pgErr.ConstraintName, "merchant_id"):
			return fmt.Errorf("merchant program subscription references missing merchant %s", merchantID)
		case strings.Contains(pgErr.ConstraintName, "plan_id"):
			return fmt.Errorf("merchant program subscription references missing merchant program plan %s", planID)
		default:
			return errors.New("merchant program subscription references a missing related record")
		}
	case "23514":
		if pgErr.ConstraintName == "chk_merchant_program_subscriptions_dates" {
			return errors.New("merchant program subscription expires_at must be after started_at")
		}
		return fmt.Errorf("merchant program subscription violates constraint %s", pgErr.ConstraintName)
	default:
		return err
	}
}

// Insert inserts a new merchant program subscription.
//
// If subscription.ID is uuid.Nil, a new UUID is generated. Status defaults to
// SubscriptionStatusPending and billing period defaults to BillingPeriodMonthly
// when omitted.
//
// Insert does not cancel or mutate existing subscriptions. If the merchant
// already has a non-deleted current subscription, the database partial unique
// index rejects the write and this method returns a clear conflict error.
func (m *MerchantProgramSubscriptionModel) Insert(ctx context.Context, subscription *MerchantProgramSubscription) error {
	if err := m.validate(); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("InsertMerchantProgramSubscription")

	if err := validateMerchantProgramSubscriptionForInsert(subscription); err != nil {
		logger.Error("Validation failed", err)
		return err
	}

	if subscription.ID == uuid.Nil {
		subscription.ID = uuid.New()
	}

	query := `
		INSERT INTO merchant_program_subscriptions (
			id,
			merchant_id,
			plan_id,
			status,
			billing_period,
			started_at,
			expires_at,
			cancelled_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING created_at, updated_at, deleted_at
	`

	err := m.DB.QueryRow(ctx, query,
		subscription.ID,
		subscription.MerchantID,
		subscription.PlanID,
		subscription.Status,
		subscription.BillingPeriod,
		subscription.StartedAt,
		subscription.ExpiresAt,
		subscription.CancelledAt,
	).Scan(
		&subscription.CreatedAt,
		&subscription.UpdatedAt,
		&subscription.DeletedAt,
	)
	if err != nil {
		err = translateMerchantProgramSubscriptionWriteError(err, subscription.MerchantID, subscription.PlanID)
		logger.Error("Insert merchant program subscription failed", err,
			"subscription_id", subscription.ID,
			"merchant_id", subscription.MerchantID,
			"plan_id", subscription.PlanID,
			"status", subscription.Status,
			"billing_period", subscription.BillingPeriod,
		)
		return err
	}

	logger.Info("Insert merchant program subscription successful",
		"subscription_id", subscription.ID,
		"merchant_id", subscription.MerchantID,
		"plan_id", subscription.PlanID,
		"status", subscription.Status,
		"billing_period", subscription.BillingPeriod,
	)
	return nil
}

// GetByID retrieves a non-deleted merchant program subscription by ID.
func (m *MerchantProgramSubscriptionModel) GetByID(ctx context.Context, id uuid.UUID) (*MerchantProgramSubscription, error) {
	if err := m.validate(); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("GetMerchantProgramSubscriptionByID")

	if err := validateMerchantProgramSubscriptionID(id); err != nil {
		logger.Error("Validation failed", err)
		return nil, err
	}

	query := `
		SELECT ` + merchantProgramSubscriptionSelectColumns + `
		FROM merchant_program_subscriptions
		WHERE id = $1
		  AND deleted_at IS NULL
	`

	var subscription MerchantProgramSubscription
	err := scanMerchantProgramSubscription(m.DB.QueryRow(ctx, query, id), &subscription)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			logger.Warn("Merchant program subscription not found", "subscription_id", id)
			return nil, nil
		}
		logger.Error("Get merchant program subscription by ID failed", err, "subscription_id", id)
		return nil, err
	}

	logger.Info("Get merchant program subscription by ID successful",
		"subscription_id", subscription.ID,
		"merchant_id", subscription.MerchantID,
		"plan_id", subscription.PlanID,
		"status", subscription.Status,
	)
	return &subscription, nil
}

// GetActiveByMerchantID retrieves a merchant's active, non-deleted merchant
// program subscription.
func (m *MerchantProgramSubscriptionModel) GetActiveByMerchantID(ctx context.Context, merchantID uuid.UUID) (*MerchantProgramSubscription, error) {
	if err := m.validate(); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("GetActiveMerchantProgramSubscriptionByMerchantID")

	if merchantID == uuid.Nil {
		err := errors.New("merchant program subscription merchant ID is required")
		logger.Error("Validation failed", err)
		return nil, err
	}

	query := `
		SELECT ` + merchantProgramSubscriptionSelectColumns + `
		FROM merchant_program_subscriptions
		WHERE merchant_id = $1
		  AND status = $2
		  AND deleted_at IS NULL
		LIMIT 1
	`

	var subscription MerchantProgramSubscription
	err := scanMerchantProgramSubscription(m.DB.QueryRow(ctx, query, merchantID, SubscriptionStatusActive), &subscription)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			logger.Warn("Active merchant program subscription not found", "merchant_id", merchantID)
			return nil, nil
		}
		logger.Error("Get active merchant program subscription by merchant ID failed", err, "merchant_id", merchantID)
		return nil, err
	}

	logger.Info("Get active merchant program subscription by merchant ID successful",
		"subscription_id", subscription.ID,
		"merchant_id", subscription.MerchantID,
		"plan_id", subscription.PlanID,
	)
	return &subscription, nil
}

// GetCurrentByMerchantID retrieves the merchant's non-deleted current
// subscription.
//
// Current means status is pending, active, paused, or suspended. This matches
// the partial unique index ux_merchant_program_subscriptions_one_active.
// If that index predicate changes, this query must be updated in sync.
func (m *MerchantProgramSubscriptionModel) GetCurrentByMerchantID(ctx context.Context, merchantID uuid.UUID) (*MerchantProgramSubscription, error) {
	if err := m.validate(); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("GetCurrentMerchantProgramSubscriptionByMerchantID")

	if merchantID == uuid.Nil {
		err := errors.New("merchant program subscription merchant ID is required")
		logger.Error("Validation failed", err)
		return nil, err
	}

	query := `
		SELECT ` + merchantProgramSubscriptionSelectColumns + `
		FROM merchant_program_subscriptions
		WHERE merchant_id = $1
		  AND status IN ('pending', 'active', 'paused', 'suspended')
		  AND deleted_at IS NULL
		LIMIT 1
	`

	var subscription MerchantProgramSubscription
	err := scanMerchantProgramSubscription(m.DB.QueryRow(ctx, query, merchantID), &subscription)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			logger.Warn("Current merchant program subscription not found", "merchant_id", merchantID)
			return nil, nil
		}
		logger.Error("Get current merchant program subscription by merchant ID failed", err, "merchant_id", merchantID)
		return nil, err
	}

	logger.Info("Get current merchant program subscription by merchant ID successful",
		"subscription_id", subscription.ID,
		"merchant_id", subscription.MerchantID,
		"plan_id", subscription.PlanID,
		"status", subscription.Status,
	)
	return &subscription, nil
}

// ListByMerchantID retrieves merchant program subscriptions for one merchant.
func (m *MerchantProgramSubscriptionModel) ListByMerchantID(
	ctx context.Context,
	merchantID uuid.UUID,
	includeDeleted bool,
	limit int,
	offset int,
) ([]*MerchantProgramSubscription, error) {
	if err := m.validate(); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("ListMerchantProgramSubscriptionsByMerchantID")

	if merchantID == uuid.Nil {
		err := errors.New("merchant program subscription merchant ID is required")
		logger.Error("Validation failed", err)
		return nil, err
	}
	if err := validateMerchantProgramSubscriptionPagination(limit, offset); err != nil {
		logger.Error("Validation failed", err)
		return nil, err
	}

	query := `
		SELECT ` + merchantProgramSubscriptionSelectColumns + `
		FROM merchant_program_subscriptions
		WHERE merchant_id = $1
	`
	if !includeDeleted {
		query += ` AND deleted_at IS NULL`
	}
	query += `
		ORDER BY created_at DESC, id DESC
		LIMIT $2 OFFSET $3
	`

	rows, err := m.DB.Query(ctx, query, merchantID, limit, offset)
	if err != nil {
		logger.Error("List merchant program subscriptions by merchant ID query failed", err, "merchant_id", merchantID)
		return nil, err
	}
	defer rows.Close()

	var subscriptions []*MerchantProgramSubscription
	for rows.Next() {
		var subscription MerchantProgramSubscription
		if err := scanMerchantProgramSubscriptionFromRows(rows, &subscription); err != nil {
			logger.Error("Merchant program subscription row scan failed", err, "merchant_id", merchantID)
			return nil, err
		}
		subscriptions = append(subscriptions, &subscription)
	}

	if err := rows.Err(); err != nil {
		logger.Error("Merchant program subscription row iteration failed", err, "merchant_id", merchantID)
		return nil, err
	}

	logger.Info("List merchant program subscriptions by merchant ID successful",
		"merchant_id", merchantID,
		"include_deleted", includeDeleted,
		"count", len(subscriptions),
	)
	return subscriptions, nil
}

// ListByPlanAndStatus retrieves non-deleted merchant program subscriptions for
// a plan and status.
func (m *MerchantProgramSubscriptionModel) ListByPlanAndStatus(
	ctx context.Context,
	planID uuid.UUID,
	status MerchantProgramSubscriptionStatus,
	limit int,
	offset int,
) ([]*MerchantProgramSubscription, error) {
	if err := m.validate(); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("ListMerchantProgramSubscriptionsByPlanAndStatus")

	if planID == uuid.Nil {
		err := errors.New("merchant program subscription plan ID is required")
		logger.Error("Validation failed", err)
		return nil, err
	}
	status, err := validateMerchantProgramSubscriptionStatus(status)
	if err != nil {
		logger.Error("Validation failed", err)
		return nil, err
	}
	if err := validateMerchantProgramSubscriptionPagination(limit, offset); err != nil {
		logger.Error("Validation failed", err)
		return nil, err
	}

	query := `
		SELECT ` + merchantProgramSubscriptionSelectColumns + `
		FROM merchant_program_subscriptions
		WHERE plan_id = $1
		  AND status = $2
		  AND deleted_at IS NULL
		ORDER BY created_at DESC, id DESC
		LIMIT $3 OFFSET $4
	`

	rows, err := m.DB.Query(ctx, query, planID, status, limit, offset)
	if err != nil {
		logger.Error("List merchant program subscriptions by plan and status query failed", err,
			"plan_id", planID,
			"status", status,
		)
		return nil, err
	}
	defer rows.Close()

	var subscriptions []*MerchantProgramSubscription
	for rows.Next() {
		var subscription MerchantProgramSubscription
		if err := scanMerchantProgramSubscriptionFromRows(rows, &subscription); err != nil {
			logger.Error("Merchant program subscription row scan failed", err,
				"plan_id", planID,
				"status", status,
			)
			return nil, err
		}
		subscriptions = append(subscriptions, &subscription)
	}

	if err := rows.Err(); err != nil {
		logger.Error("Merchant program subscription row iteration failed", err,
			"plan_id", planID,
			"status", status,
		)
		return nil, err
	}

	logger.Info("List merchant program subscriptions by plan and status successful",
		"plan_id", planID,
		"status", status,
		"count", len(subscriptions),
	)
	return subscriptions, nil
}

// Exists checks whether a non-deleted merchant program subscription exists by
// ID.
func (m *MerchantProgramSubscriptionModel) Exists(ctx context.Context, id uuid.UUID) (bool, error) {
	if err := m.validate(); err != nil {
		return false, err
	}
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("ExistsMerchantProgramSubscription")

	if err := validateMerchantProgramSubscriptionID(id); err != nil {
		logger.Error("Validation failed", err)
		return false, err
	}

	query := `
		SELECT EXISTS (
			SELECT 1
			FROM merchant_program_subscriptions
			WHERE id = $1
			  AND deleted_at IS NULL
		)
	`

	var exists bool
	err := m.DB.QueryRow(ctx, query, id).Scan(&exists)
	if err != nil {
		logger.Error("Merchant program subscription exists query failed", err, "subscription_id", id)
		return false, err
	}

	return exists, nil
}

// UpdatePlan changes the merchant program plan for a non-deleted subscription.
func (m *MerchantProgramSubscriptionModel) UpdatePlan(ctx context.Context, id uuid.UUID, planID uuid.UUID) error {
	if err := m.validate(); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("UpdateMerchantProgramSubscriptionPlan")

	if err := validateMerchantProgramSubscriptionID(id); err != nil {
		logger.Error("Validation failed", err)
		return err
	}
	if planID == uuid.Nil {
		err := errors.New("merchant program subscription plan ID is required")
		logger.Error("Validation failed", err)
		return err
	}

	query := `
		UPDATE merchant_program_subscriptions
		SET
			plan_id = $1,
			updated_at = NOW()
		WHERE id = $2
		  AND deleted_at IS NULL
		RETURNING merchant_id
	`

	var merchantID uuid.UUID
	err := m.DB.QueryRow(ctx, query, planID, id).Scan(&merchantID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			err = fmt.Errorf("merchant program subscription not found or already deleted: %s", id)
		} else {
			err = translateMerchantProgramSubscriptionWriteError(err, uuid.Nil, planID)
		}
		logger.Error("Update merchant program subscription plan failed", err,
			"subscription_id", id,
			"plan_id", planID,
		)
		return err
	}

	logger.Info("Update merchant program subscription plan successful",
		"subscription_id", id,
		"merchant_id", merchantID,
		"plan_id", planID,
	)
	return nil
}

func (m *MerchantProgramSubscriptionModel) transitionStatus(
	ctx context.Context,
	id uuid.UUID,
	status MerchantProgramSubscriptionStatus,
) error {
	if err := m.validate(); err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("TransitionMerchantProgramSubscriptionStatus")

	if err := validateMerchantProgramSubscriptionID(id); err != nil {
		logger.Error("Validation failed", err)
		return err
	}

	status, err := validateMerchantProgramSubscriptionStatus(status)
	if err != nil {
		logger.Error("Validation failed", err)
		return err
	}

	var query string
	switch status {
	case SubscriptionStatusActive:
		query = `
			UPDATE merchant_program_subscriptions
			SET
				status = $1,
				started_at = COALESCE(started_at, NOW()),
				cancelled_at = NULL,
				updated_at = NOW()
			WHERE id = $2
			  AND deleted_at IS NULL
			RETURNING merchant_id, plan_id
		`
	case SubscriptionStatusCancelled:
		query = `
			UPDATE merchant_program_subscriptions
			SET
				status = $1,
				cancelled_at = NOW(),
				updated_at = NOW()
			WHERE id = $2
			  AND deleted_at IS NULL
			RETURNING merchant_id, plan_id
		`
	case SubscriptionStatusExpired:
		query = `
			UPDATE merchant_program_subscriptions
			SET
				status = $1,
				expires_at = COALESCE(expires_at, NOW()),
				updated_at = NOW()
			WHERE id = $2
			  AND deleted_at IS NULL
			RETURNING merchant_id, plan_id
		`
	default:
		query = `
			UPDATE merchant_program_subscriptions
			SET
				status = $1,
				updated_at = NOW()
			WHERE id = $2
			  AND deleted_at IS NULL
			RETURNING merchant_id, plan_id
		`
	}

	var merchantID uuid.UUID
	var planID uuid.UUID
	err = m.DB.QueryRow(ctx, query, status, id).Scan(&merchantID, &planID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			err = fmt.Errorf("merchant program subscription not found or already deleted: %s", id)
		} else {
			err = translateMerchantProgramSubscriptionWriteError(err, uuid.Nil, uuid.Nil)
		}
		logger.Error("Transition merchant program subscription status failed", err,
			"subscription_id", id,
			"status", status,
		)
		return err
	}

	logger.Info("Transition merchant program subscription status successful",
		"subscription_id", id,
		"merchant_id", merchantID,
		"plan_id", planID,
		"status", status,
	)
	return nil
}


// Activate marks a non-deleted merchant program subscription as active.
//
// Activation sets started_at to NOW() only if started_at is currently NULL.
func (m *MerchantProgramSubscriptionModel) Activate(ctx context.Context, id uuid.UUID) error {
	return m.transitionStatus(ctx, id, SubscriptionStatusActive)
}

// Pause marks a non-deleted merchant program subscription as paused.
func (m *MerchantProgramSubscriptionModel) Pause(ctx context.Context, id uuid.UUID) error {
	return m.transitionStatus(ctx, id, SubscriptionStatusPaused)
}

// Suspend marks a non-deleted merchant program subscription as suspended.
func (m *MerchantProgramSubscriptionModel) Suspend(ctx context.Context, id uuid.UUID) error {
	return m.transitionStatus(ctx, id, SubscriptionStatusSuspended)
}

// Cancel marks a non-deleted merchant program subscription as cancelled.
func (m *MerchantProgramSubscriptionModel) Cancel(ctx context.Context, id uuid.UUID) error {
	return m.transitionStatus(ctx, id, SubscriptionStatusCancelled)
}

// Expire marks a non-deleted merchant program subscription as expired.
func (m *MerchantProgramSubscriptionModel) Expire(ctx context.Context, id uuid.UUID) error {
	return m.transitionStatus(ctx, id, SubscriptionStatusExpired)
} 

// SoftDelete marks a merchant program subscription as deleted.
//
// Subscription records are lifecycle records. This method intentionally uses
// soft delete and does not hard-delete the row.
func (m *MerchantProgramSubscriptionModel) SoftDelete(ctx context.Context, id uuid.UUID) error {
	if err := m.validate(); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("SoftDeleteMerchantProgramSubscription")

	if err := validateMerchantProgramSubscriptionID(id); err != nil {
		logger.Error("Validation failed", err)
		return err
	}

	query := `
		UPDATE merchant_program_subscriptions
		SET
			deleted_at = NOW(),
			updated_at = NOW()
		WHERE id = $1
		  AND deleted_at IS NULL
		RETURNING merchant_id, plan_id, status
	`

	var merchantID uuid.UUID
	var planID uuid.UUID
	var status MerchantProgramSubscriptionStatus
	err := m.DB.QueryRow(ctx, query, id).Scan(&merchantID, &planID, &status)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			err = fmt.Errorf("merchant program subscription not found or already deleted: %s", id)
		}
		logger.Error("Soft delete merchant program subscription failed", err, "subscription_id", id)
		return err
	}

	logger.Info("Soft delete merchant program subscription successful",
		"subscription_id", id,
		"merchant_id", merchantID,
		"plan_id", planID,
		"status", status,
	)
	return nil
}

// Restore restores a soft-deleted merchant program subscription.
//
// Restoring a pending, active, paused, or suspended subscription may fail if
// the merchant already has another current non-deleted subscription.
func (m *MerchantProgramSubscriptionModel) Restore(ctx context.Context, id uuid.UUID) error {
	if err := m.validate(); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("RestoreMerchantProgramSubscription")

	if err := validateMerchantProgramSubscriptionID(id); err != nil {
		logger.Error("Validation failed", err)
		return err
	}

	query := `
		UPDATE merchant_program_subscriptions
		SET
			deleted_at = NULL,
			updated_at = NOW()
		WHERE id = $1
		  AND deleted_at IS NOT NULL
		RETURNING merchant_id, plan_id, status
	`

	var merchantID uuid.UUID
	var planID uuid.UUID
	var status MerchantProgramSubscriptionStatus
	err := m.DB.QueryRow(ctx, query, id).Scan(&merchantID, &planID, &status)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			err = fmt.Errorf("no soft-deleted merchant program subscription found with ID %s", id)
		} else {
			err = translateMerchantProgramSubscriptionWriteError(err, uuid.Nil, uuid.Nil)
		}
		logger.Error("Restore merchant program subscription failed", err, "subscription_id", id)
		return err
	}

	logger.Info("Restore merchant program subscription successful",
		"subscription_id", id,
		"merchant_id", merchantID,
		"plan_id", planID,
		"status", status,
	)
	return nil
}