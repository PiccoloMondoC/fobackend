// Package data provides models and database access methods for merchant
// program subscription periods.
//
// sdworkspace/sdbackend/internal/data/merchant_program_subscription_periods.go
//
// GTM:
//
//	Layer: 2.4 Merchant / Future Offering Domain
//	Release Class: SPINE
//	Reason:
//	  merchant_program_subscription_periods preserves the immutable,
//	  time-bounded plan and billing-cadence facts applicable to a merchant
//	  program subscription during a specific commercial period.
//
//	  Merchant program subscriptions are optional commercial packaging
//	  infrastructure beneath the Future Offering Platform and Monetization
//	  Layer. This capability remains compiled and production-ready regardless
//	  of whether Administration enables or disables subscriptions.
//
//	  Canonical current subscription state remains in
//	  merchant_program_subscriptions. This file records historical period facts
//	  and must not become a second source of current subscription lifecycle
//	  state.
//
// Domain Boundary:
//
//	A merchant program subscription period records:
//
//	  - the parent subscription;
//	  - the plan applicable during the period;
//	  - the billing cadence applicable during the period; and
//	  - the exact half-open interval during which those terms applied.
//
//	It does not determine:
//
//	  - whether subscriptions are administratively enabled;
//	  - whether a merchant may use a plan;
//	  - whether a plan or cadence is commercially offered;
//	  - pricing;
//	  - entitlement eligibility;
//	  - renewal policy;
//	  - invoicing;
//	  - payment collection;
//	  - fee calculation; or
//	  - subscription lifecycle transitions.
//
// Interval Semantics:
//
//	Periods use half-open intervals:
//
//	  [period_start, period_end)
//
//	A period contains an instant when:
//
//	  period_start <= instant AND period_end > instant
//
//	This permits adjacent periods to meet at one boundary without both
//	containing that boundary instant.
//
// Period Continuity:
//
//	Gaps between periods are permitted. A gap means no subscription-period
//	terms are recorded for that interval. Whether a service should create a
//	successor period, renew a subscription, or treat a gap as an operational
//	exception belongs to service orchestration and Administration-governed
//	commercial behavior.
//
//	A subscription period is a bounded commercial-terms record. It is not an
//	invoice and does not independently prove that a charge was calculated,
//	invoiced, collected, or settled.
//
// Immutability:
//
//	Every period is fully bounded when inserted. This model exposes no update,
//	upsert, delete, soft-delete, restore, or hard-delete capability.
//
//	An erroneous persisted period must be repaired through a separately
//	governed corrective transaction or migration that preserves auditability.
//	It must not be silently rewritten or hidden by this model.
//
// Concurrency:
//
//	Non-overlap is a database-owned integrity invariant. Production schema must
//	include:
//
//	  excl_merchant_program_subscription_periods_no_overlap
//
//	as a GiST exclusion constraint over subscription_id and
//	tstzrange(period_start, period_end, '[)'). Application-side existence or
//	overlap checks are not substitutes for that constraint.
//
// Transaction Ownership:
//
//	This model does not begin, commit, or roll back transactions. InsertTx
//	accepts a caller-owned pgx.Tx so coordinating services can atomically
//	combine period creation with subscription lifecycle mutation, subscription
//	event insertion, and downstream monetization work.
//
// SPINE Rule:
//
//	Keep compiling.
//	Keep production-ready.
//	Preserve immutable period facts.
//	Preserve subscription and plan foreign-key integrity.
//	Preserve non-overlapping periods per subscription.
//	Preserve half-open interval semantics.
//	Preserve deterministic subscription timeline ordering.
//	Preserve DB-owned created_at.
//	Preserve transaction-compatible insertion.
//	Do not give this model transaction ownership.
//	Do not embed commercial enablement, eligibility, pricing, renewal,
//	invoicing, or authorization policy.
//	Block deployment if this file breaks build, period persistence,
//	non-overlap integrity, point-in-time term resolution, subscription timeline
//	retrieval, or downstream billing readiness.
package data

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/PiccoloMondoC/sdworkspace/sdbackend/internal/logging"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	merchantProgramSubscriptionPeriodMaxPageSize = 100

	merchantProgramSubscriptionPeriodsSubscriptionFKConstraint = "fk_merchant_program_subscription_periods_subscription"

	merchantProgramSubscriptionPeriodsPlanFKConstraint = "fk_merchant_program_subscription_periods_plan"

	merchantProgramSubscriptionPeriodsBillingPeriodConstraint = "chk_merchant_program_subscription_periods_billing_period"

	merchantProgramSubscriptionPeriodsWindowConstraint = "chk_merchant_program_subscription_periods_window"

	merchantProgramSubscriptionPeriodsSubscriptionStartConstraint = "uq_merchant_program_subscription_periods_subscription_start"

	merchantProgramSubscriptionPeriodsNoOverlapConstraint = "excl_merchant_program_subscription_periods_no_overlap"
)

const merchantProgramSubscriptionPeriodSelectColumns = `
	id,
	subscription_id,
	plan_id,
	billing_period,
	period_start,
	period_end,
	created_at
`

// MerchantProgramSubscriptionPeriod represents one immutable, bounded period of
// commercial terms for a merchant program subscription.
//
// PeriodStart is inclusive and PeriodEnd is exclusive.
type MerchantProgramSubscriptionPeriod struct {
	ID             uuid.UUID                                `json:"id" db:"id"`
	SubscriptionID uuid.UUID                                `json:"subscription_id" db:"subscription_id"`
	PlanID         uuid.UUID                                `json:"plan_id" db:"plan_id"`
	BillingPeriod  MerchantProgramSubscriptionBillingPeriod `json:"billing_period" db:"billing_period"`
	PeriodStart    time.Time                                `json:"period_start" db:"period_start"`
	PeriodEnd      time.Time                                `json:"period_end" db:"period_end"`
	CreatedAt      time.Time                                `json:"created_at" db:"created_at"`
}

// MerchantProgramSubscriptionPeriodModel owns persistence for immutable merchant
// program subscription periods.
type MerchantProgramSubscriptionPeriodModel struct {
	DB     *pgxpool.Pool
	Logger *logging.Logger
}

// merchantProgramSubscriptionPeriodQuerier is the minimal query contract needed
// by pool-backed and transaction-backed period insertion.
type merchantProgramSubscriptionPeriodQuerier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

func (m *MerchantProgramSubscriptionPeriodModel) validateBase() error {
	if m == nil {
		return errors.New(
			"merchant program subscription period model is required",
		)
	}
	if m.Logger == nil {
		return errors.New(
			"merchant program subscription period model logger is required",
		)
	}
	return nil
}

func (m *MerchantProgramSubscriptionPeriodModel) validatePool() error {
	if err := m.validateBase(); err != nil {
		return err
	}
	if m.DB == nil {
		return errors.New(
			"merchant program subscription period model database pool is required",
		)
	}
	return nil
}

func validateMerchantProgramSubscriptionPeriodTx(tx pgx.Tx) error {
	if tx == nil {
		return errors.New(
			"merchant program subscription period transaction is required",
		)
	}
	return nil
}

func validateMerchantProgramSubscriptionPeriodID(id uuid.UUID) error {
	if id == uuid.Nil {
		return errors.New(
			"merchant program subscription period ID is required",
		)
	}
	return nil
}

func validateMerchantProgramSubscriptionPeriodSubscriptionID(
	subscriptionID uuid.UUID,
) error {
	if subscriptionID == uuid.Nil {
		return errors.New(
			"merchant program subscription period subscription ID is required",
		)
	}
	return nil
}

func validateMerchantProgramSubscriptionPeriodPagination(
	limit int,
	offset int,
) error {
	if limit <= 0 || limit > merchantProgramSubscriptionPeriodMaxPageSize {
		return fmt.Errorf(
			"merchant program subscription period limit must be between 1 and %d",
			merchantProgramSubscriptionPeriodMaxPageSize,
		)
	}
	if offset < 0 {
		return errors.New(
			"merchant program subscription period offset must be non-negative",
		)
	}
	return nil
}

func validateMerchantProgramSubscriptionPeriodForInsert(
	period *MerchantProgramSubscriptionPeriod,
) error {
	if period == nil {
		return errors.New(
			"merchant program subscription period is required",
		)
	}
	if period.SubscriptionID == uuid.Nil {
		return errors.New(
			"merchant program subscription period subscription ID is required",
		)
	}
	if period.PlanID == uuid.Nil {
		return errors.New(
			"merchant program subscription period plan ID is required",
		)
	}

	period.BillingPeriod =
		NormalizeMerchantProgramSubscriptionBillingPeriod(
			period.BillingPeriod,
		)

	if !IsValidMerchantProgramSubscriptionBillingPeriod(
		period.BillingPeriod,
	) {
		return fmt.Errorf(
			"invalid merchant program subscription period billing period: %s",
			period.BillingPeriod,
		)
	}

	if period.PeriodStart.IsZero() {
		return errors.New(
			"merchant program subscription period start is required",
		)
	}
	if period.PeriodEnd.IsZero() {
		return errors.New(
			"merchant program subscription period end is required",
		)
	}

	period.PeriodStart = period.PeriodStart.UTC()
	period.PeriodEnd = period.PeriodEnd.UTC()

	if !period.PeriodEnd.After(period.PeriodStart) {
		return errors.New(
			"merchant program subscription period end must be after start",
		)
	}

	return nil
}

func scanMerchantProgramSubscriptionPeriod(
	row scannableRow,
	period *MerchantProgramSubscriptionPeriod,
) error {
	return row.Scan(
		&period.ID,
		&period.SubscriptionID,
		&period.PlanID,
		&period.BillingPeriod,
		&period.PeriodStart,
		&period.PeriodEnd,
		&period.CreatedAt,
	)
}

func translateMerchantProgramSubscriptionPeriodWriteError(
	err error,
	subscriptionID uuid.UUID,
	planID uuid.UUID,
) error {
	switch {
	case IsPgConstraint(
		err,
		merchantProgramSubscriptionPeriodsSubscriptionStartConstraint,
	):
		return fmt.Errorf(
			"%w: subscription_id=%s",
			ErrMerchantProgramSubscriptionPeriodAlreadyExists,
			subscriptionID,
		)

	case IsPgConstraint(
		err,
		merchantProgramSubscriptionPeriodsNoOverlapConstraint,
	):
		return fmt.Errorf(
			"%w: subscription_id=%s",
			ErrMerchantProgramSubscriptionPeriodOverlap,
			subscriptionID,
		)

	case IsExclusionViolation(err):
		return fmt.Errorf(
			"%w: subscription_id=%s",
			ErrMerchantProgramSubscriptionPeriodOverlap,
			subscriptionID,
		)

	case IsPgConstraint(
		err,
		merchantProgramSubscriptionPeriodsSubscriptionFKConstraint,
	):
		return fmt.Errorf(
			"%w: subscription_id=%s",
			ErrMerchantProgramSubscriptionPeriodSubscriptionNotFound,
			subscriptionID,
		)

	case IsPgConstraint(
		err,
		merchantProgramSubscriptionPeriodsPlanFKConstraint,
	):
		return fmt.Errorf(
			"%w: plan_id=%s",
			ErrMerchantProgramSubscriptionPeriodPlanNotFound,
			planID,
		)

	case IsPgConstraint(
		err,
		merchantProgramSubscriptionPeriodsBillingPeriodConstraint,
	):
		return errors.New(
			"merchant program subscription period has an invalid billing period",
		)

	case IsPgConstraint(
		err,
		merchantProgramSubscriptionPeriodsWindowConstraint,
	):
		return errors.New(
			"merchant program subscription period end must be after start",
		)

	case IsUniqueViolation(err):
		return fmt.Errorf(
			"%w: subscription_id=%s",
			ErrMerchantProgramSubscriptionPeriodAlreadyExists,
			subscriptionID,
		)

	case IsForeignKeyViolation(err):
		return errors.New(
			"merchant program subscription period references a missing related record",
		)

	case IsCheckViolation(err):
		return fmt.Errorf(
			"merchant program subscription period violates constraint %s",
			PgErrorConstraintName(err),
		)

	default:
		return err
	}
}

func (m *MerchantProgramSubscriptionPeriodModel) insert(
	ctx context.Context,
	querier merchantProgramSubscriptionPeriodQuerier,
	functionName string,
	period *MerchantProgramSubscriptionPeriod,
) error {
	if err := m.validateBase(); err != nil {
		return err
	}
	if querier == nil {
		return errors.New(
			"merchant program subscription period query executor is required",
		)
	}

	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.
		GetLoggerWithContextFromContext(ctx).
		WithFunctionName(functionName)

	if err := validateMerchantProgramSubscriptionPeriodForInsert(
		period,
	); err != nil {
		logger.Error("Validation failed", err)
		return err
	}

	if period.ID == uuid.Nil {
		period.ID = uuid.New()
	}

	const query = `
		INSERT INTO merchant_program_subscription_periods (
			id,
			subscription_id,
			plan_id,
			billing_period,
			period_start,
			period_end
		)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING created_at
	`

	err := querier.
		QueryRow(
			ctx,
			query,
			period.ID,
			period.SubscriptionID,
			period.PlanID,
			period.BillingPeriod,
			period.PeriodStart,
			period.PeriodEnd,
		).
		Scan(&period.CreatedAt)
	if err != nil {
		err = translateMerchantProgramSubscriptionPeriodWriteError(
			err,
			period.SubscriptionID,
			period.PlanID,
		)

		logger.Error(
			"Insert merchant program subscription period failed",
			err,
			"period_id",
			period.ID,
			"subscription_id",
			period.SubscriptionID,
			"plan_id",
			period.PlanID,
			"billing_period",
			period.BillingPeriod,
		)
		return err
	}

	logger.Info(
		"Insert merchant program subscription period successful",
		"period_id",
		period.ID,
		"subscription_id",
		period.SubscriptionID,
		"plan_id",
		period.PlanID,
		"billing_period",
		period.BillingPeriod,
	)

	return nil
}

// Insert inserts one immutable merchant program subscription period through the
// model's database pool.
//
// If period.ID is uuid.Nil, Insert generates a UUID. CreatedAt is owned by the
// database. PeriodStart and PeriodEnd are normalized to UTC.
//
// Insert is appropriate only when period creation is an independent write.
// Workflows that must coordinate subscription mutation, lifecycle history, or
// monetization writes must use InsertTx.
func (m *MerchantProgramSubscriptionPeriodModel) Insert(
	ctx context.Context,
	period *MerchantProgramSubscriptionPeriod,
) error {
	if err := m.validatePool(); err != nil {
		return err
	}

	return m.insert(
		ctx,
		m.DB,
		"InsertMerchantProgramSubscriptionPeriod",
		period,
	)
}

// InsertTx inserts one immutable merchant program subscription period through a
// caller-owned transaction.
//
// InsertTx does not begin, commit, or roll back tx.
func (m *MerchantProgramSubscriptionPeriodModel) InsertTx(
	ctx context.Context,
	tx pgx.Tx,
	period *MerchantProgramSubscriptionPeriod,
) error {
	if err := m.validateBase(); err != nil {
		return err
	}
	if err := validateMerchantProgramSubscriptionPeriodTx(tx); err != nil {
		return err
	}

	return m.insert(
		ctx,
		tx,
		"InsertMerchantProgramSubscriptionPeriodTx",
		period,
	)
}

// GetByID retrieves a merchant program subscription period by ID.
//
// GetByID returns nil, nil when no matching period exists.
func (m *MerchantProgramSubscriptionPeriodModel) GetByID(
	ctx context.Context,
	id uuid.UUID,
) (*MerchantProgramSubscriptionPeriod, error) {
	if err := m.validatePool(); err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.
		GetLoggerWithContextFromContext(ctx).
		WithFunctionName("GetMerchantProgramSubscriptionPeriodByID")

	if err := validateMerchantProgramSubscriptionPeriodID(id); err != nil {
		logger.Error("Validation failed", err)
		return nil, err
	}

	query := `
		SELECT ` + merchantProgramSubscriptionPeriodSelectColumns + `
		FROM merchant_program_subscription_periods
		WHERE id = $1
	`

	var period MerchantProgramSubscriptionPeriod
	err := scanMerchantProgramSubscriptionPeriod(
		m.DB.QueryRow(ctx, query, id),
		&period,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			logger.Warn(
				"Merchant program subscription period not found",
				"period_id",
				id,
			)
			return nil, nil
		}

		logger.Error(
			"Get merchant program subscription period by ID failed",
			err,
			"period_id",
			id,
		)
		return nil, err
	}

	logger.Info(
		"Get merchant program subscription period by ID successful",
		"period_id",
		period.ID,
		"subscription_id",
		period.SubscriptionID,
		"plan_id",
		period.PlanID,
	)

	return &period, nil
}

// GetLatestBySubscriptionID retrieves the period with the latest PeriodStart
// for one subscription.
//
// It returns nil, nil when the subscription has no recorded periods.
func (m *MerchantProgramSubscriptionPeriodModel) GetLatestBySubscriptionID(
	ctx context.Context,
	subscriptionID uuid.UUID,
) (*MerchantProgramSubscriptionPeriod, error) {
	if err := m.validatePool(); err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.
		GetLoggerWithContextFromContext(ctx).
		WithFunctionName(
			"GetLatestMerchantProgramSubscriptionPeriodBySubscriptionID",
		)

	if err := validateMerchantProgramSubscriptionPeriodSubscriptionID(
		subscriptionID,
	); err != nil {
		logger.Error("Validation failed", err)
		return nil, err
	}

	query := `
		SELECT ` + merchantProgramSubscriptionPeriodSelectColumns + `
		FROM merchant_program_subscription_periods
		WHERE subscription_id = $1
		ORDER BY period_start DESC, id DESC
		LIMIT 1
	`

	var period MerchantProgramSubscriptionPeriod
	err := scanMerchantProgramSubscriptionPeriod(
		m.DB.QueryRow(ctx, query, subscriptionID),
		&period,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			logger.Warn(
				"Latest merchant program subscription period not found",
				"subscription_id",
				subscriptionID,
			)
			return nil, nil
		}

		logger.Error(
			"Get latest merchant program subscription period failed",
			err,
			"subscription_id",
			subscriptionID,
		)
		return nil, err
	}

	logger.Info(
		"Get latest merchant program subscription period successful",
		"period_id",
		period.ID,
		"subscription_id",
		period.SubscriptionID,
		"plan_id",
		period.PlanID,
	)

	return &period, nil
}

// GetContainingInstant retrieves the period containing instant for one
// subscription using half-open [PeriodStart, PeriodEnd) semantics.
//
// It returns nil, nil when no period contains instant.
//
// The query uses the same tstzrange expression as the table's GiST exclusion
// constraint. The exclusion constraint guarantees that at most one period for
// the subscription can contain the supplied instant.
func (m *MerchantProgramSubscriptionPeriodModel) GetContainingInstant(
	ctx context.Context,
	subscriptionID uuid.UUID,
	instant time.Time,
) (*MerchantProgramSubscriptionPeriod, error) {
	if err := m.validatePool(); err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.
		GetLoggerWithContextFromContext(ctx).
		WithFunctionName(
			"GetMerchantProgramSubscriptionPeriodContainingInstant",
		)

	if err := validateMerchantProgramSubscriptionPeriodSubscriptionID(
		subscriptionID,
	); err != nil {
		logger.Error("Validation failed", err)
		return nil, err
	}

	if instant.IsZero() {
		err := errors.New(
			"merchant program subscription period lookup instant is required",
		)
		logger.Error("Validation failed", err)
		return nil, err
	}

	instant = instant.UTC()

	query := `
		SELECT ` + merchantProgramSubscriptionPeriodSelectColumns + `
		FROM merchant_program_subscription_periods
		WHERE subscription_id = $1
		  AND tstzrange(period_start, period_end, '[)') @> $2::timestamptz
		ORDER BY period_start DESC, id DESC
		LIMIT 1
	`

	var period MerchantProgramSubscriptionPeriod

	err := scanMerchantProgramSubscriptionPeriod(
		m.DB.QueryRow(
			ctx,
			query,
			subscriptionID,
			instant,
		),
		&period,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			logger.Warn(
				"Merchant program subscription period containing instant not found",
				"subscription_id",
				subscriptionID,
			)
			return nil, nil
		}

		logger.Error(
			"Get merchant program subscription period containing instant failed",
			err,
			"subscription_id",
			subscriptionID,
		)
		return nil, err
	}

	logger.Info(
		"Get merchant program subscription period containing instant successful",
		"period_id",
		period.ID,
		"subscription_id",
		period.SubscriptionID,
		"plan_id",
		period.PlanID,
	)

	return &period, nil
}

// ListBySubscriptionID retrieves a bounded, newest-first period timeline for one
// subscription.
//
// Ordering is deterministic:
//
//	period_start DESC, id DESC
func (m *MerchantProgramSubscriptionPeriodModel) ListBySubscriptionID(
	ctx context.Context,
	subscriptionID uuid.UUID,
	limit int,
	offset int,
) ([]*MerchantProgramSubscriptionPeriod, error) {
	if err := m.validatePool(); err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.
		GetLoggerWithContextFromContext(ctx).
		WithFunctionName(
			"ListMerchantProgramSubscriptionPeriodsBySubscriptionID",
		)

	if err := validateMerchantProgramSubscriptionPeriodSubscriptionID(
		subscriptionID,
	); err != nil {
		logger.Error("Validation failed", err)
		return nil, err
	}
	if err := validateMerchantProgramSubscriptionPeriodPagination(
		limit,
		offset,
	); err != nil {
		logger.Error("Validation failed", err)
		return nil, err
	}

	query := `
		SELECT ` + merchantProgramSubscriptionPeriodSelectColumns + `
		FROM merchant_program_subscription_periods
		WHERE subscription_id = $1
		ORDER BY period_start DESC, id DESC
		LIMIT $2 OFFSET $3
	`

	rows, err := m.DB.Query(
		ctx,
		query,
		subscriptionID,
		limit,
		offset,
	)
	if err != nil {
		logger.Error(
			"List merchant program subscription periods by subscription ID query failed",
			err,
			"subscription_id",
			subscriptionID,
			"limit",
			limit,
			"offset",
			offset,
		)
		return nil, err
	}
	defer rows.Close()

	periods := make([]*MerchantProgramSubscriptionPeriod, 0)

	for rows.Next() {
		var period MerchantProgramSubscriptionPeriod

		if err := scanMerchantProgramSubscriptionPeriod(
			rows,
			&period,
		); err != nil {
			logger.Error(
				"Scan merchant program subscription period failed",
				err,
				"subscription_id",
				subscriptionID,
			)
			return nil, err
		}

		periods = append(periods, &period)
	}

	if err := rows.Err(); err != nil {
		logger.Error(
			"Iterate merchant program subscription periods failed",
			err,
			"subscription_id",
			subscriptionID,
		)
		return nil, err
	}

	logger.Info(
		"List merchant program subscription periods by subscription ID successful",
		"subscription_id",
		subscriptionID,
		"count",
		len(periods),
		"limit",
		limit,
		"offset",
		offset,
	)

	return periods, nil
}
