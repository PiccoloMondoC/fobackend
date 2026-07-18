// Package data provides models and database access methods for merchant
// program monetization fee schedules.
//
// sdworkspace/sdbackend/internal/data/merchant_program_fee_schedules.go
//
// GTM:
//
//	Layer: 2.4 Merchant / Future Offering Domain
//	Release Class: SPINE
//	Reason:
//	  merchant_program_fee_schedules is release-critical monetization
//	  infrastructure for merchant setup fees, subscriptions, Future Offering
//	  fees, Launch Intelligence monetization, Campaign Performance Fees,
//	  adjustments, refunds, reversals, plan-specific pricing, global pricing
//	  policy, and Merchant Center billing configuration.
//
// SPINE Rule:
//
//	Keep compiling.
//	Keep production-ready.
//	Preserve fixed-precision monetary values.
//	Preserve effective-dated commercial-policy integrity.
//	Preserve deterministic plan-specific-over-global fee resolution.
//	Preserve immutable commercial terms after insertion.
//	Preserve non-negative refund and reversal amount semantics.
//	Block deployment if this file breaks merchant billing readiness,
//	Future Offering monetization readiness, effective fee resolution,
//	plan/global precedence, or monetary precision.
package data

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/PiccoloMondoC/sdworkspace/sdbackend/internal/logging"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	feeScheduleMaxPageSize = 100

	pgCodeForeignKeyViolation = "23503"
	pgCodeCheckViolation      = "23514"
	pgCodeExclusionViolation  = "23P01"
)

// MerchantFeeScope identifies whether a fee schedule is global or plan-scoped.
type MerchantFeeScope string

const (
	// MerchantFeeScopeGlobal applies a fee schedule platform-wide.
	MerchantFeeScopeGlobal MerchantFeeScope = "global"

	// MerchantFeeScopePlan applies a fee schedule to one merchant program plan.
	MerchantFeeScopePlan MerchantFeeScope = "plan"
)

// MerchantFeeType identifies the commercial purpose of a merchant fee.
type MerchantFeeType string

const (
	// MerchantFeeTypeSetup identifies the one-time merchant setup fee.
	MerchantFeeTypeSetup MerchantFeeType = "merchant_setup_fee"

	// MerchantFeeTypeSubscription identifies a recurring subscription fee.
	MerchantFeeTypeSubscription MerchantFeeType = "subscription_fee"

	// MerchantFeeTypeCampaignPerformance identifies a Campaign Performance Fee.
	MerchantFeeTypeCampaignPerformance MerchantFeeType = "campaign_performance_fee"

	// MerchantFeeTypeFutureOffering identifies a Future Offering or Launch Intelligence fee.
	MerchantFeeTypeFutureOffering MerchantFeeType = "future_offering_fee"

	// MerchantFeeTypeAdjustment identifies a billing adjustment.
	MerchantFeeTypeAdjustment MerchantFeeType = "adjustment_fee"

	// MerchantFeeTypeRefund identifies a refund economic entry.
	MerchantFeeTypeRefund MerchantFeeType = "refund"

	// MerchantFeeTypeReversal identifies a reversal economic entry.
	MerchantFeeTypeReversal MerchantFeeType = "reversal"
)

// MerchantBillingInterval identifies the billing cadence of a fee.
type MerchantBillingInterval string

const (
	// MerchantBillingIntervalOneTime identifies a one-time charge.
	MerchantBillingIntervalOneTime MerchantBillingInterval = "one_time"

	// MerchantBillingIntervalMonthly identifies a monthly charge.
	MerchantBillingIntervalMonthly MerchantBillingInterval = "monthly"

	// MerchantBillingIntervalAnnual identifies an annual charge.
	MerchantBillingIntervalAnnual MerchantBillingInterval = "annual"

	// MerchantBillingIntervalEvent identifies a charge per qualifying event.
	MerchantBillingIntervalEvent MerchantBillingInterval = "event"
)

// MerchantFeeCalculationMethod identifies how a fee is calculated.
type MerchantFeeCalculationMethod string

const (
	// MerchantFeeCalculationFlat uses a fixed amount.
	MerchantFeeCalculationFlat MerchantFeeCalculationMethod = "flat"

	// MerchantFeeCalculationPercentage uses a percentage rate.
	MerchantFeeCalculationPercentage MerchantFeeCalculationMethod = "percentage"

	// MerchantFeeCalculationHybrid combines a fixed amount and percentage rate.
	MerchantFeeCalculationHybrid MerchantFeeCalculationMethod = "hybrid"

	// MerchantFeeCalculationNegotiated delegates the final price to a
	// separately governed negotiated agreement.
	MerchantFeeCalculationNegotiated MerchantFeeCalculationMethod = "negotiated"
)

const merchantProgramFeeScheduleSelectColumns = `
	id,
	fee_scope,
	plan_id,
	fee_type,
	billing_interval,
	calculation_method,
	flat_amount,
	percentage_rate,
	minimum_fee,
	maximum_fee,
	included_seats,
	extra_seat_fee,
	currency,
	is_active,
	effective_from,
	effective_to,
	created_at,
	updated_at,
	deleted_at
`

// MerchantProgramFeeSchedule represents one effective-dated merchant pricing
// policy row.
//
// Monetary and percentage values use decimal strings so PostgreSQL NUMERIC
// precision is never converted through binary floating point. Pointer fields
// preserve the distinction between SQL NULL and numeric zero.
type MerchantProgramFeeSchedule struct {
	ID                uuid.UUID                   `json:"id" db:"id"`
	FeeScope          MerchantFeeScope             `json:"fee_scope" db:"fee_scope"`
	PlanID            *uuid.UUID                  `json:"plan_id,omitempty" db:"plan_id"`
	FeeType           MerchantFeeType              `json:"fee_type" db:"fee_type"`
	BillingInterval   MerchantBillingInterval      `json:"billing_interval" db:"billing_interval"`
	CalculationMethod MerchantFeeCalculationMethod `json:"calculation_method" db:"calculation_method"`
	FlatAmount        *string                     `json:"flat_amount,omitempty" db:"flat_amount"`
	PercentageRate    *string                     `json:"percentage_rate,omitempty" db:"percentage_rate"`
	MinimumFee        *string                     `json:"minimum_fee,omitempty" db:"minimum_fee"`
	MaximumFee        *string                     `json:"maximum_fee,omitempty" db:"maximum_fee"`
	IncludedSeats     int                         `json:"included_seats" db:"included_seats"`
	ExtraSeatFee      *string                     `json:"extra_seat_fee,omitempty" db:"extra_seat_fee"`
	Currency          string                      `json:"currency" db:"currency"`
	IsActive          bool                        `json:"is_active" db:"is_active"`
	EffectiveFrom     time.Time                   `json:"effective_from" db:"effective_from"`
	EffectiveTo       *time.Time                  `json:"effective_to,omitempty" db:"effective_to"`
	CreatedAt         time.Time                   `json:"created_at" db:"created_at"`
	UpdatedAt         time.Time                   `json:"updated_at" db:"updated_at"`
	DeletedAt         *time.Time                  `json:"deleted_at,omitempty" db:"deleted_at"`
}

// MerchantProgramFeeScheduleModel owns persistence for merchant program fee
// schedules.
type MerchantProgramFeeScheduleModel struct {
	DB     *pgxpool.Pool
	Logger *logging.Logger
}

// NormalizeMerchantFeeScope returns the canonical fee-scope value.
func NormalizeMerchantFeeScope(value MerchantFeeScope) MerchantFeeScope {
	return MerchantFeeScope(normalizeIdentifier(string(value)))
}

// IsValidMerchantFeeScope reports whether value is an allowed fee scope.
func IsValidMerchantFeeScope(value MerchantFeeScope) bool {
	switch NormalizeMerchantFeeScope(value) {
	case MerchantFeeScopeGlobal, MerchantFeeScopePlan:
		return true
	default:
		return false
	}
}

// NormalizeMerchantFeeType returns the canonical merchant-fee type.
func NormalizeMerchantFeeType(value MerchantFeeType) MerchantFeeType {
	return MerchantFeeType(normalizeIdentifier(string(value)))
}

// IsValidMerchantFeeType reports whether value is an allowed merchant-fee type.
func IsValidMerchantFeeType(value MerchantFeeType) bool {
	switch NormalizeMerchantFeeType(value) {
	case MerchantFeeTypeSetup,
		MerchantFeeTypeSubscription,
		MerchantFeeTypeCampaignPerformance,
		MerchantFeeTypeFutureOffering,
		MerchantFeeTypeAdjustment,
		MerchantFeeTypeRefund,
		MerchantFeeTypeReversal:
		return true
	default:
		return false
	}
}

// NormalizeMerchantBillingInterval returns the canonical billing interval.
func NormalizeMerchantBillingInterval(value MerchantBillingInterval) MerchantBillingInterval {
	return MerchantBillingInterval(normalizeIdentifier(string(value)))
}

// IsValidMerchantBillingInterval reports whether value is an allowed interval.
func IsValidMerchantBillingInterval(value MerchantBillingInterval) bool {
	switch NormalizeMerchantBillingInterval(value) {
	case MerchantBillingIntervalOneTime,
		MerchantBillingIntervalMonthly,
		MerchantBillingIntervalAnnual,
		MerchantBillingIntervalEvent:
		return true
	default:
		return false
	}
}

// NormalizeMerchantFeeCalculationMethod returns the canonical calculation method.
func NormalizeMerchantFeeCalculationMethod(
	value MerchantFeeCalculationMethod,
) MerchantFeeCalculationMethod {
	return MerchantFeeCalculationMethod(normalizeIdentifier(string(value)))
}

// IsValidMerchantFeeCalculationMethod reports whether value is allowed.
func IsValidMerchantFeeCalculationMethod(value MerchantFeeCalculationMethod) bool {
	switch NormalizeMerchantFeeCalculationMethod(value) {
	case MerchantFeeCalculationFlat,
		MerchantFeeCalculationPercentage,
		MerchantFeeCalculationHybrid,
		MerchantFeeCalculationNegotiated:
		return true
	default:
		return false
	}
}

// IsMerchantFeeTypeIntervalCompatible reports whether a fee type may use the
// supplied billing interval.
func IsMerchantFeeTypeIntervalCompatible(
	feeType MerchantFeeType,
	interval MerchantBillingInterval,
) bool {
	feeType = NormalizeMerchantFeeType(feeType)
	interval = NormalizeMerchantBillingInterval(interval)

	switch feeType {
	case MerchantFeeTypeSetup:
		return interval == MerchantBillingIntervalOneTime

	case MerchantFeeTypeSubscription:
		return interval == MerchantBillingIntervalMonthly ||
			interval == MerchantBillingIntervalAnnual

	case MerchantFeeTypeCampaignPerformance,
		MerchantFeeTypeFutureOffering,
		MerchantFeeTypeAdjustment,
		MerchantFeeTypeRefund,
		MerchantFeeTypeReversal:
		return interval == MerchantBillingIntervalEvent

	default:
		return false
	}
}

func scanMerchantProgramFeeSchedule(
	row pgx.Row,
	schedule *MerchantProgramFeeSchedule,
) error {
	return row.Scan(
		&schedule.ID,
		&schedule.FeeScope,
		&schedule.PlanID,
		&schedule.FeeType,
		&schedule.BillingInterval,
		&schedule.CalculationMethod,
		&schedule.FlatAmount,
		&schedule.PercentageRate,
		&schedule.MinimumFee,
		&schedule.MaximumFee,
		&schedule.IncludedSeats,
		&schedule.ExtraSeatFee,
		&schedule.Currency,
		&schedule.IsActive,
		&schedule.EffectiveFrom,
		&schedule.EffectiveTo,
		&schedule.CreatedAt,
		&schedule.UpdatedAt,
		&schedule.DeletedAt,
	)
}

func scanMerchantProgramFeeScheduleFromRows(
	rows pgx.Rows,
	schedule *MerchantProgramFeeSchedule,
) error {
	return rows.Scan(
		&schedule.ID,
		&schedule.FeeScope,
		&schedule.PlanID,
		&schedule.FeeType,
		&schedule.BillingInterval,
		&schedule.CalculationMethod,
		&schedule.FlatAmount,
		&schedule.PercentageRate,
		&schedule.MinimumFee,
		&schedule.MaximumFee,
		&schedule.IncludedSeats,
		&schedule.ExtraSeatFee,
		&schedule.Currency,
		&schedule.IsActive,
		&schedule.EffectiveFrom,
		&schedule.EffectiveTo,
		&schedule.CreatedAt,
		&schedule.UpdatedAt,
		&schedule.DeletedAt,
	)
}

func normalizeMerchantProgramFeeSchedule(schedule *MerchantProgramFeeSchedule) {
	schedule.FeeScope = NormalizeMerchantFeeScope(schedule.FeeScope)
	schedule.FeeType = NormalizeMerchantFeeType(schedule.FeeType)
	schedule.BillingInterval = NormalizeMerchantBillingInterval(schedule.BillingInterval)
	schedule.CalculationMethod = NormalizeMerchantFeeCalculationMethod(schedule.CalculationMethod)
	schedule.Currency = strings.ToUpper(strings.TrimSpace(schedule.Currency))

	normalizeOptionalDecimal(&schedule.FlatAmount)
	normalizeOptionalDecimal(&schedule.PercentageRate)
	normalizeOptionalDecimal(&schedule.MinimumFee)
	normalizeOptionalDecimal(&schedule.MaximumFee)
	normalizeOptionalDecimal(&schedule.ExtraSeatFee)

	schedule.EffectiveFrom = schedule.EffectiveFrom.UTC()
	if schedule.EffectiveTo != nil {
		effectiveTo := schedule.EffectiveTo.UTC()
		schedule.EffectiveTo = &effectiveTo
	}
}

func normalizeOptionalDecimal(value **string) {
	if value == nil || *value == nil {
		return
	}

	normalized := strings.TrimSpace(**value)
	*value = &normalized
}

func validateMerchantProgramFeeSchedule(
	schedule *MerchantProgramFeeSchedule,
) error {
	if schedule == nil {
		return errors.New("merchant program fee schedule is required")
	}

	normalizeMerchantProgramFeeSchedule(schedule)

	if !IsValidMerchantFeeScope(schedule.FeeScope) {
		return fmt.Errorf("invalid merchant fee scope: %s", schedule.FeeScope)
	}

	switch schedule.FeeScope {
	case MerchantFeeScopeGlobal:
		if schedule.PlanID != nil {
			return errors.New("global fee schedules must not specify plan_id")
		}

	case MerchantFeeScopePlan:
		if schedule.PlanID == nil || *schedule.PlanID == uuid.Nil {
			return errors.New("plan-scoped fee schedules require plan_id")
		}
	}

	if !IsValidMerchantFeeType(schedule.FeeType) {
		return fmt.Errorf("invalid merchant fee type: %s", schedule.FeeType)
	}

	if !IsValidMerchantBillingInterval(schedule.BillingInterval) {
		return fmt.Errorf("invalid merchant billing interval: %s", schedule.BillingInterval)
	}

	if !IsMerchantFeeTypeIntervalCompatible(
		schedule.FeeType,
		schedule.BillingInterval,
	) {
		return fmt.Errorf(
			"merchant fee type %s is incompatible with billing interval %s",
			schedule.FeeType,
			schedule.BillingInterval,
		)
	}

	if !IsValidMerchantFeeCalculationMethod(schedule.CalculationMethod) {
		return fmt.Errorf(
			"invalid merchant fee calculation method: %s",
			schedule.CalculationMethod,
		)
	}

	switch schedule.CalculationMethod {
	case MerchantFeeCalculationFlat:
		if schedule.FlatAmount == nil {
			return errors.New("flat calculation requires flat_amount")
		}

	case MerchantFeeCalculationPercentage:
		if schedule.PercentageRate == nil {
			return errors.New("percentage calculation requires percentage_rate")
		}

	case MerchantFeeCalculationHybrid:
		if schedule.FlatAmount == nil || schedule.PercentageRate == nil {
			return errors.New(
				"hybrid calculation requires flat_amount and percentage_rate",
			)
		}
	}

	if err := validateOptionalNonNegativeDecimalString(
		schedule.FlatAmount,
		"flat_amount",
	); err != nil {
		return err
	}

	if err := validateOptionalNonNegativeDecimalString(
		schedule.PercentageRate,
		"percentage_rate",
	); err != nil {
		return err
	}

	if err := validateOptionalNonNegativeDecimalString(
		schedule.MinimumFee,
		"minimum_fee",
	); err != nil {
		return err
	}

	if err := validateOptionalNonNegativeDecimalString(
		schedule.MaximumFee,
		"maximum_fee",
	); err != nil {
		return err
	}

	if err := validateOptionalNonNegativeDecimalString(
		schedule.ExtraSeatFee,
		"extra_seat_fee",
	); err != nil {
		return err
	}

	if schedule.MinimumFee != nil && schedule.MaximumFee != nil {
		less, err := merchantDecimalLess(
			*schedule.MaximumFee,
			*schedule.MinimumFee,
		)
		if err != nil {
			return err
		}
		if less {
			return errors.New("maximum_fee must not be less than minimum_fee")
		}
	}

	if schedule.IncludedSeats < 1 {
		return errors.New("included_seats must be at least 1")
	}

	if schedule.Currency == "" {
		schedule.Currency = "USD"
	}
	if !isCanonicalCurrency(schedule.Currency) {
		return fmt.Errorf("invalid currency: %s", schedule.Currency)
	}

	if schedule.EffectiveFrom.IsZero() {
		return errors.New("effective_from is required")
	}

	if schedule.EffectiveTo != nil &&
		!schedule.EffectiveTo.After(schedule.EffectiveFrom) {
		return errors.New("effective_to must be after effective_from")
	}

	return nil
}

// validateOptionalNonNegativeDecimalString validates an optional decimal
// string that must be greater than or equal to zero.
//
// A nil value is valid because absence is meaningful and distinct from both
// an explicitly supplied blank value and a stored numeric zero. A non-nil
// blank value is rejected as invalid caller input.
//
// Decimal syntax and non-negativity validation are delegated to the
// centralized validateNonNegativeDecimalString helper in commons.go.
func validateOptionalNonNegativeDecimalString(
	value *string,
	fieldName string,
) error {
	if value == nil {
		return nil
	}

	if strings.TrimSpace(*value) == "" {
		return fmt.Errorf("%s must not be empty", fieldName)
	}

	return validateNonNegativeDecimalString(*value, fieldName)
}

func merchantDecimalLess(left, right string) (bool, error) {
	leftNumber, ok := new(big.Rat).SetString(left)
	if !ok {
		return false, errors.New("left decimal value is invalid")
	}

	rightNumber, ok := new(big.Rat).SetString(right)
	if !ok {
		return false, errors.New("right decimal value is invalid")
	}

	return leftNumber.Cmp(rightNumber) < 0, nil
}

func isCanonicalCurrency(currency string) bool {
	if len(currency) != 3 {
		return false
	}

	for index := 0; index < len(currency); index++ {
		if currency[index] < 'A' || currency[index] > 'Z' {
			return false
		}
	}

	return true
}

func validateFeeSchedulePagination(limit, offset int) error {
	if limit <= 0 || limit > feeScheduleMaxPageSize {
		return fmt.Errorf(
			"limit must be between 1 and %d",
			feeScheduleMaxPageSize,
		)
	}

	if offset < 0 {
		return errors.New("offset must be non-negative")
	}

	return nil
}

func feeSchedulePostgresCode(err error) string {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return ""
	}

	return pgErr.Code
}

func translateFeeScheduleWriteError(err error) error {
	switch feeSchedulePostgresCode(err) {
	case pgCodeExclusionViolation:
		return fmt.Errorf(
			"merchant program fee schedule conflicts with an overlapping active schedule: %w",
			err,
		)

	case pgCodeForeignKeyViolation:
		return fmt.Errorf(
			"merchant program fee schedule references an unknown merchant program plan: %w",
			err,
		)

	case pgCodeCheckViolation:
		return fmt.Errorf(
			"merchant program fee schedule violates a database constraint: %w",
			err,
		)

	default:
		return err
	}
}

// Insert inserts a new merchant program fee schedule.
//
// Commercial identity and price fields are immutable after insertion. New
// pricing must be represented by a new effective-dated schedule.
func (m *MerchantProgramFeeScheduleModel) Insert(
	ctx context.Context,
	schedule *MerchantProgramFeeSchedule,
) error {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.
		GetLoggerWithContextFromContext(ctx).
		WithFunctionName("InsertMerchantProgramFeeSchedule")

	if err := validateMerchantProgramFeeSchedule(schedule); err != nil {
		logger.Error("Validation failed", err)
		return err
	}

	if schedule.ID == uuid.Nil {
		schedule.ID = uuid.New()
	}

	const query = `
		INSERT INTO merchant_program_fee_schedules (
			id,
			fee_scope,
			plan_id,
			fee_type,
			billing_interval,
			calculation_method,
			flat_amount,
			percentage_rate,
			minimum_fee,
			maximum_fee,
			included_seats,
			extra_seat_fee,
			currency,
			is_active,
			effective_from,
			effective_to
		)
		VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8,
			$9, $10, $11, $12, $13, $14, $15, $16
		)
		RETURNING
			is_active,
			created_at,
			updated_at,
			deleted_at
	`

	err := m.DB.QueryRow(
		ctx,
		query,
		schedule.ID,
		schedule.FeeScope,
		schedule.PlanID,
		schedule.FeeType,
		schedule.BillingInterval,
		schedule.CalculationMethod,
		schedule.FlatAmount,
		schedule.PercentageRate,
		schedule.MinimumFee,
		schedule.MaximumFee,
		schedule.IncludedSeats,
		schedule.ExtraSeatFee,
		schedule.Currency,
		schedule.IsActive,
		schedule.EffectiveFrom,
		schedule.EffectiveTo,
	).Scan(
		&schedule.IsActive,
		&schedule.CreatedAt,
		&schedule.UpdatedAt,
		&schedule.DeletedAt,
	)
	if err != nil {
		err = translateFeeScheduleWriteError(err)
		logger.Error(
			"Insert merchant program fee schedule failed",
			err,
			"fee_scope",
			schedule.FeeScope,
			"fee_type",
			schedule.FeeType,
			"billing_interval",
			schedule.BillingInterval,
		)
		return err
	}

	logger.Info(
		"Insert merchant program fee schedule successful",
		"fee_schedule_id",
		schedule.ID,
		"fee_scope",
		schedule.FeeScope,
		"fee_type",
		schedule.FeeType,
	)
	return nil
}

// GetByID retrieves a non-deleted merchant program fee schedule by ID.
func (m *MerchantProgramFeeScheduleModel) GetByID(
	ctx context.Context,
	id uuid.UUID,
) (*MerchantProgramFeeSchedule, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.
		GetLoggerWithContextFromContext(ctx).
		WithFunctionName("GetMerchantProgramFeeScheduleByID")

	if id == uuid.Nil {
		err := errors.New("merchant program fee schedule ID is required")
		logger.Error("Validation failed", err)
		return nil, err
	}

	query := `
		SELECT ` + merchantProgramFeeScheduleSelectColumns + `
		FROM merchant_program_fee_schedules
		WHERE id = $1
		  AND deleted_at IS NULL
	`

	var schedule MerchantProgramFeeSchedule
	err := scanMerchantProgramFeeSchedule(
		m.DB.QueryRow(ctx, query, id),
		&schedule,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			logger.Warn(
				"Merchant program fee schedule not found",
				"fee_schedule_id",
				id,
			)
			return nil, nil
		}

		logger.Error(
			"Get merchant program fee schedule by ID failed",
			err,
			"fee_schedule_id",
			id,
		)
		return nil, err
	}

	logger.Info(
		"Get merchant program fee schedule by ID successful",
		"fee_schedule_id",
		schedule.ID,
	)
	return &schedule, nil
}

// GetAll retrieves merchant program fee schedules with explicit pagination.
func (m *MerchantProgramFeeScheduleModel) GetAll(
	ctx context.Context,
	includeDeleted bool,
	limit,
	offset int,
) ([]*MerchantProgramFeeSchedule, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.
		GetLoggerWithContextFromContext(ctx).
		WithFunctionName("GetAllMerchantProgramFeeSchedules")

	if err := validateFeeSchedulePagination(limit, offset); err != nil {
		logger.Error("Validation failed", err)
		return nil, err
	}

	query := `
		SELECT ` + merchantProgramFeeScheduleSelectColumns + `
		FROM merchant_program_fee_schedules
	`
	if !includeDeleted {
		query += ` WHERE deleted_at IS NULL`
	}
	query += `
		ORDER BY
			fee_type ASC,
			billing_interval ASC,
			fee_scope ASC,
			effective_from DESC,
			id ASC
		LIMIT $1 OFFSET $2
	`

	schedules, err := m.listFeeSchedules(ctx, query, limit, offset)
	if err != nil {
		logger.Error("Get all merchant program fee schedules failed", err)
		return nil, err
	}

	logger.Info(
		"Get all merchant program fee schedules successful",
		"count",
		len(schedules),
		"include_deleted",
		includeDeleted,
	)
	return schedules, nil
}

// ListByPlan retrieves non-deleted schedules for one merchant program plan.
func (m *MerchantProgramFeeScheduleModel) ListByPlan(
	ctx context.Context,
	planID uuid.UUID,
	limit,
	offset int,
) ([]*MerchantProgramFeeSchedule, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.
		GetLoggerWithContextFromContext(ctx).
		WithFunctionName("ListMerchantProgramFeeSchedulesByPlan")

	if planID == uuid.Nil {
		err := errors.New("merchant program plan ID is required")
		logger.Error("Validation failed", err)
		return nil, err
	}

	if err := validateFeeSchedulePagination(limit, offset); err != nil {
		logger.Error("Validation failed", err)
		return nil, err
	}

	query := `
		SELECT ` + merchantProgramFeeScheduleSelectColumns + `
		FROM merchant_program_fee_schedules
		WHERE fee_scope = 'plan'
		  AND plan_id = $1
		  AND deleted_at IS NULL
		ORDER BY
			fee_type ASC,
			billing_interval ASC,
			effective_from DESC,
			id ASC
		LIMIT $2 OFFSET $3
	`

	schedules, err := m.listFeeSchedules(
		ctx,
		query,
		planID,
		limit,
		offset,
	)
	if err != nil {
		logger.Error(
			"List merchant program fee schedules by plan failed",
			err,
			"plan_id",
			planID,
		)
		return nil, err
	}

	logger.Info(
		"List merchant program fee schedules by plan successful",
		"plan_id",
		planID,
		"count",
		len(schedules),
	)
	return schedules, nil
}

// ListGlobal retrieves non-deleted global fee schedules.
func (m *MerchantProgramFeeScheduleModel) ListGlobal(
	ctx context.Context,
	limit,
	offset int,
) ([]*MerchantProgramFeeSchedule, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.
		GetLoggerWithContextFromContext(ctx).
		WithFunctionName("ListGlobalMerchantProgramFeeSchedules")

	if err := validateFeeSchedulePagination(limit, offset); err != nil {
		logger.Error("Validation failed", err)
		return nil, err
	}

	query := `
		SELECT ` + merchantProgramFeeScheduleSelectColumns + `
		FROM merchant_program_fee_schedules
		WHERE fee_scope = 'global'
		  AND plan_id IS NULL
		  AND deleted_at IS NULL
		ORDER BY
			fee_type ASC,
			billing_interval ASC,
			effective_from DESC,
			id ASC
		LIMIT $1 OFFSET $2
	`

	schedules, err := m.listFeeSchedules(ctx, query, limit, offset)
	if err != nil {
		logger.Error("List global merchant program fee schedules failed", err)
		return nil, err
	}

	logger.Info(
		"List global merchant program fee schedules successful",
		"count",
		len(schedules),
	)
	return schedules, nil
}

// ListByFeeType retrieves non-deleted schedules for one fee type.
func (m *MerchantProgramFeeScheduleModel) ListByFeeType(
	ctx context.Context,
	feeType MerchantFeeType,
	limit,
	offset int,
) ([]*MerchantProgramFeeSchedule, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.
		GetLoggerWithContextFromContext(ctx).
		WithFunctionName("ListMerchantProgramFeeSchedulesByFeeType")

	feeType = NormalizeMerchantFeeType(feeType)
	if !IsValidMerchantFeeType(feeType) {
		err := fmt.Errorf("invalid merchant fee type: %s", feeType)
		logger.Error("Validation failed", err)
		return nil, err
	}

	if err := validateFeeSchedulePagination(limit, offset); err != nil {
		logger.Error("Validation failed", err)
		return nil, err
	}

	query := `
		SELECT ` + merchantProgramFeeScheduleSelectColumns + `
		FROM merchant_program_fee_schedules
		WHERE fee_type = $1
		  AND deleted_at IS NULL
		ORDER BY
			fee_scope ASC,
			billing_interval ASC,
			effective_from DESC,
			id ASC
		LIMIT $2 OFFSET $3
	`

	schedules, err := m.listFeeSchedules(
		ctx,
		query,
		feeType,
		limit,
		offset,
	)
	if err != nil {
		logger.Error(
			"List merchant program fee schedules by fee type failed",
			err,
			"fee_type",
			feeType,
		)
		return nil, err
	}

	logger.Info(
		"List merchant program fee schedules by fee type successful",
		"fee_type",
		feeType,
		"count",
		len(schedules),
	)
	return schedules, nil
}

func (m *MerchantProgramFeeScheduleModel) listFeeSchedules(
	ctx context.Context,
	query string,
	args ...any,
) ([]*MerchantProgramFeeSchedule, error) {
	rows, err := m.DB.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	schedules := make([]*MerchantProgramFeeSchedule, 0)
	for rows.Next() {
		var schedule MerchantProgramFeeSchedule
		if err := scanMerchantProgramFeeScheduleFromRows(
			rows,
			&schedule,
		); err != nil {
			return nil, err
		}

		schedules = append(schedules, &schedule)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return schedules, nil
}

// ResolveEffective resolves the applicable active fee schedule at a reference
// time. A plan-specific schedule takes precedence over a global schedule.
//
// The method returns nil, nil when no schedule applies. It fails rather than
// selecting arbitrarily when overlapping rows make a precedence tier
// ambiguous.
func (m *MerchantProgramFeeScheduleModel) ResolveEffective(
	ctx context.Context,
	feeType MerchantFeeType,
	billingInterval MerchantBillingInterval,
	planID *uuid.UUID,
	asOf time.Time,
) (*MerchantProgramFeeSchedule, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.
		GetLoggerWithContextFromContext(ctx).
		WithFunctionName("ResolveEffectiveMerchantProgramFeeSchedule")

	feeType = NormalizeMerchantFeeType(feeType)
	billingInterval = NormalizeMerchantBillingInterval(billingInterval)

	if !IsValidMerchantFeeType(feeType) {
		err := fmt.Errorf("invalid merchant fee type: %s", feeType)
		logger.Error("Validation failed", err)
		return nil, err
	}

	if !IsValidMerchantBillingInterval(billingInterval) {
		err := fmt.Errorf(
			"invalid merchant billing interval: %s",
			billingInterval,
		)
		logger.Error("Validation failed", err)
		return nil, err
	}

	if !IsMerchantFeeTypeIntervalCompatible(feeType, billingInterval) {
		err := fmt.Errorf(
			"merchant fee type %s is incompatible with billing interval %s",
			feeType,
			billingInterval,
		)
		logger.Error("Validation failed", err)
		return nil, err
	}

	if planID != nil && *planID == uuid.Nil {
		err := errors.New("plan ID must not be the nil UUID")
		logger.Error("Validation failed", err)
		return nil, err
	}

	if asOf.IsZero() {
		asOf = time.Now().UTC()
	} else {
		asOf = asOf.UTC()
	}

	if planID != nil {
		schedule, err := m.resolveEffectiveTier(
			ctx,
			MerchantFeeScopePlan,
			planID,
			feeType,
			billingInterval,
			asOf,
		)
		if err != nil {
			logger.Error(
				"Resolve effective plan fee schedule failed",
				err,
				"plan_id",
				*planID,
				"fee_type",
				feeType,
				"billing_interval",
				billingInterval,
			)
			return nil, err
		}
		if schedule != nil {
			logger.Info(
				"Resolve effective merchant program fee schedule successful",
				"fee_schedule_id",
				schedule.ID,
				"fee_scope",
				MerchantFeeScopePlan,
				"plan_id",
				*planID,
			)
			return schedule, nil
		}
	}

	schedule, err := m.resolveEffectiveTier(
		ctx,
		MerchantFeeScopeGlobal,
		nil,
		feeType,
		billingInterval,
		asOf,
	)
	if err != nil {
		logger.Error(
			"Resolve effective global fee schedule failed",
			err,
			"fee_type",
			feeType,
			"billing_interval",
			billingInterval,
		)
		return nil, err
	}

	if schedule == nil {
		logger.Warn(
			"Effective merchant program fee schedule not found",
			"fee_type",
			feeType,
			"billing_interval",
			billingInterval,
		)
		return nil, nil
	}

	logger.Info(
		"Resolve effective merchant program fee schedule successful",
		"fee_schedule_id",
		schedule.ID,
		"fee_scope",
		MerchantFeeScopeGlobal,
	)
	return schedule, nil
}

func (m *MerchantProgramFeeScheduleModel) resolveEffectiveTier(
	ctx context.Context,
	scope MerchantFeeScope,
	planID *uuid.UUID,
	feeType MerchantFeeType,
	billingInterval MerchantBillingInterval,
	asOf time.Time,
) (*MerchantProgramFeeSchedule, error) {
	query := `
		SELECT ` + merchantProgramFeeScheduleSelectColumns + `
		FROM merchant_program_fee_schedules
		WHERE fee_scope = $1
		  AND fee_type = $2
		  AND billing_interval = $3
		  AND (
				($4::uuid IS NULL AND plan_id IS NULL)
				OR plan_id = $4
		  )
		  AND is_active = TRUE
		  AND deleted_at IS NULL
		  AND effective_from <= $5
		  AND (effective_to IS NULL OR effective_to > $5)
		ORDER BY effective_from DESC, id ASC
		LIMIT 2
	`

	rows, err := m.DB.Query(
		ctx,
		query,
		scope,
		feeType,
		billingInterval,
		planID,
		asOf,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var resolved *MerchantProgramFeeSchedule
	matchCount := 0

	for rows.Next() {
		matchCount++

		var schedule MerchantProgramFeeSchedule
		if err := scanMerchantProgramFeeScheduleFromRows(
			rows,
			&schedule,
		); err != nil {
			return nil, err
		}

		if matchCount == 1 {
			resolved = &schedule
		}
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	if matchCount > 1 {
		return nil, fmt.Errorf(
			"merchant program fee schedule resolution is ambiguous for scope %s, fee type %s, and billing interval %s",
			scope,
			feeType,
			billingInterval,
		)
	}

	return resolved, nil
}

// Activate activates a non-deleted fee schedule.
func (m *MerchantProgramFeeScheduleModel) Activate(
	ctx context.Context,
	id uuid.UUID,
) error {
	return m.setActiveState(
		ctx,
		"ActivateMerchantProgramFeeSchedule",
		id,
		true,
	)
}

// Deactivate deactivates a non-deleted fee schedule.
func (m *MerchantProgramFeeScheduleModel) Deactivate(
	ctx context.Context,
	id uuid.UUID,
) error {
	return m.setActiveState(
		ctx,
		"DeactivateMerchantProgramFeeSchedule",
		id,
		false,
	)
}

func (m *MerchantProgramFeeScheduleModel) setActiveState(
	ctx context.Context,
	functionName string,
	id uuid.UUID,
	active bool,
) error {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.
		GetLoggerWithContextFromContext(ctx).
		WithFunctionName(functionName)

	if id == uuid.Nil {
		err := errors.New("merchant program fee schedule ID is required")
		logger.Error("Validation failed", err)
		return err
	}

	const query = `
		UPDATE merchant_program_fee_schedules
		SET is_active = $1
		WHERE id = $2
		  AND deleted_at IS NULL
		  AND is_active IS DISTINCT FROM $1
		RETURNING updated_at
	`

	var updatedAt time.Time
	err := m.DB.QueryRow(ctx, query, active, id).Scan(&updatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			err = fmt.Errorf(
				"no mutable merchant program fee schedule found with ID %s",
				id,
			)
		} else {
			err = translateFeeScheduleWriteError(err)
		}

		logger.Error(
			functionName+" failed",
			err,
			"fee_schedule_id",
			id,
		)
		return err
	}

	logger.Info(
		functionName+" successful",
		"fee_schedule_id",
		id,
		"is_active",
		active,
		"updated_at",
		updatedAt,
	)
	return nil
}

// Retire closes a schedule's effective window and deactivates it.
func (m *MerchantProgramFeeScheduleModel) Retire(
	ctx context.Context,
	id uuid.UUID,
	effectiveTo time.Time,
) error {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.
		GetLoggerWithContextFromContext(ctx).
		WithFunctionName("RetireMerchantProgramFeeSchedule")

	if id == uuid.Nil {
		err := errors.New("merchant program fee schedule ID is required")
		logger.Error("Validation failed", err)
		return err
	}

	if effectiveTo.IsZero() {
		effectiveTo = time.Now().UTC()
	} else {
		effectiveTo = effectiveTo.UTC()
	}

	const query = `
		UPDATE merchant_program_fee_schedules
		SET
			effective_to = $1,
			is_active = FALSE
		WHERE id = $2
		  AND deleted_at IS NULL
		  AND effective_from < $1
		  AND (effective_to IS NULL OR effective_to > $1)
		RETURNING updated_at
	`

	var updatedAt time.Time
	err := m.DB.QueryRow(ctx, query, effectiveTo, id).Scan(&updatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			err = fmt.Errorf(
				"merchant program fee schedule %s cannot be retired at %s",
				id,
				effectiveTo.Format(time.RFC3339),
			)
		} else {
			err = translateFeeScheduleWriteError(err)
		}

		logger.Error(
			"Retire merchant program fee schedule failed",
			err,
			"fee_schedule_id",
			id,
		)
		return err
	}

	logger.Info(
		"Retire merchant program fee schedule successful",
		"fee_schedule_id",
		id,
		"effective_to",
		effectiveTo,
		"updated_at",
		updatedAt,
	)
	return nil
}

// Replace atomically retires an existing schedule and inserts its effective-
// dated successor.
//
// The incoming schedule must preserve the outgoing schedule's scope, plan,
// fee type, billing interval, and currency. A replacement may change the
// calculation method and price terms.
func (m *MerchantProgramFeeScheduleModel) Replace(
	ctx context.Context,
	outgoingID uuid.UUID,
	incoming *MerchantProgramFeeSchedule,
) error {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.
		GetLoggerWithContextFromContext(ctx).
		WithFunctionName("ReplaceMerchantProgramFeeSchedule")

	if outgoingID == uuid.Nil {
		err := errors.New("outgoing merchant program fee schedule ID is required")
		logger.Error("Validation failed", err)
		return err
	}

	if err := validateMerchantProgramFeeSchedule(incoming); err != nil {
		logger.Error("Validation failed", err)
		return err
	}

	if incoming.ID == uuid.Nil {
		incoming.ID = uuid.New()
	}

	tx, err := m.DB.Begin(ctx)
	if err != nil {
		logger.Error(
			"Begin merchant program fee schedule replacement failed",
			err,
		)
		return err
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	lockQuery := `
		SELECT ` + merchantProgramFeeScheduleSelectColumns + `
		FROM merchant_program_fee_schedules
		WHERE id = $1
		  AND deleted_at IS NULL
		FOR UPDATE
	`

	var outgoing MerchantProgramFeeSchedule
	err = scanMerchantProgramFeeSchedule(
		tx.QueryRow(ctx, lockQuery, outgoingID),
		&outgoing,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			err = fmt.Errorf(
				"outgoing merchant program fee schedule not found: %s",
				outgoingID,
			)
		}

		logger.Error(
			"Lock outgoing merchant program fee schedule failed",
			err,
			"outgoing_fee_schedule_id",
			outgoingID,
		)
		return err
	}

	if err := validateFeeScheduleReplacementIdentity(
		&outgoing,
		incoming,
	); err != nil {
		logger.Error(
			"Merchant program fee schedule replacement identity failed",
			err,
			"outgoing_fee_schedule_id",
			outgoingID,
		)
		return err
	}

	if !incoming.EffectiveFrom.After(outgoing.EffectiveFrom) {
		err = errors.New(
			"incoming effective_from must be after outgoing effective_from",
		)
		logger.Error(
			"Merchant program fee schedule replacement timing failed",
			err,
			"outgoing_fee_schedule_id",
			outgoingID,
		)
		return err
	}

	const retireQuery = `
		UPDATE merchant_program_fee_schedules
		SET
			effective_to = $1,
			is_active = FALSE
		WHERE id = $2
		  AND deleted_at IS NULL
		  AND effective_from < $1
		  AND (effective_to IS NULL OR effective_to > $1)
		RETURNING updated_at
	`

	var outgoingUpdatedAt time.Time
	err = tx.QueryRow(
		ctx,
		retireQuery,
		incoming.EffectiveFrom,
		outgoingID,
	).Scan(&outgoingUpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			err = errors.New(
				"outgoing merchant program fee schedule cannot be retired at incoming effective_from",
			)
		} else {
			err = translateFeeScheduleWriteError(err)
		}

		logger.Error(
			"Retire outgoing merchant program fee schedule failed",
			err,
			"outgoing_fee_schedule_id",
			outgoingID,
		)
		return err
	}

	const insertQuery = `
		INSERT INTO merchant_program_fee_schedules (
			id,
			fee_scope,
			plan_id,
			fee_type,
			billing_interval,
			calculation_method,
			flat_amount,
			percentage_rate,
			minimum_fee,
			maximum_fee,
			included_seats,
			extra_seat_fee,
			currency,
			is_active,
			effective_from,
			effective_to
		)
		VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8,
			$9, $10, $11, $12, $13, $14, $15, $16
		)
		RETURNING
			is_active,
			created_at,
			updated_at,
			deleted_at
	`

	err = tx.QueryRow(
		ctx,
		insertQuery,
		incoming.ID,
		incoming.FeeScope,
		incoming.PlanID,
		incoming.FeeType,
		incoming.BillingInterval,
		incoming.CalculationMethod,
		incoming.FlatAmount,
		incoming.PercentageRate,
		incoming.MinimumFee,
		incoming.MaximumFee,
		incoming.IncludedSeats,
		incoming.ExtraSeatFee,
		incoming.Currency,
		incoming.IsActive,
		incoming.EffectiveFrom,
		incoming.EffectiveTo,
	).Scan(
		&incoming.IsActive,
		&incoming.CreatedAt,
		&incoming.UpdatedAt,
		&incoming.DeletedAt,
	)
	if err != nil {
		err = translateFeeScheduleWriteError(err)
		logger.Error(
			"Insert replacement merchant program fee schedule failed",
			err,
			"outgoing_fee_schedule_id",
			outgoingID,
			"incoming_fee_schedule_id",
			incoming.ID,
		)
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		logger.Error(
			"Commit merchant program fee schedule replacement failed",
			err,
			"outgoing_fee_schedule_id",
			outgoingID,
			"incoming_fee_schedule_id",
			incoming.ID,
		)
		return err
	}

	logger.Info(
		"Replace merchant program fee schedule successful",
		"outgoing_fee_schedule_id",
		outgoingID,
		"incoming_fee_schedule_id",
		incoming.ID,
		"outgoing_updated_at",
		outgoingUpdatedAt,
	)
	return nil
}

func validateFeeScheduleReplacementIdentity(
	outgoing,
	incoming *MerchantProgramFeeSchedule,
) error {
	if outgoing.FeeScope != incoming.FeeScope {
		return errors.New("replacement must preserve fee_scope")
	}

	if !equalOptionalUUID(outgoing.PlanID, incoming.PlanID) {
		return errors.New("replacement must preserve plan_id")
	}

	if outgoing.FeeType != incoming.FeeType {
		return errors.New("replacement must preserve fee_type")
	}

	if outgoing.BillingInterval != incoming.BillingInterval {
		return errors.New("replacement must preserve billing_interval")
	}

	if outgoing.Currency != incoming.Currency {
		return errors.New("replacement must preserve currency")
	}

	return nil
}

func equalOptionalUUID(left, right *uuid.UUID) bool {
	switch {
	case left == nil && right == nil:
		return true
	case left == nil || right == nil:
		return false
	default:
		return *left == *right
	}
}

// SoftDelete deactivates and soft-deletes a fee schedule.
func (m *MerchantProgramFeeScheduleModel) SoftDelete(
	ctx context.Context,
	id uuid.UUID,
) error {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.
		GetLoggerWithContextFromContext(ctx).
		WithFunctionName("SoftDeleteMerchantProgramFeeSchedule")

	if id == uuid.Nil {
		err := errors.New("merchant program fee schedule ID is required")
		logger.Error("Validation failed", err)
		return err
	}

	const query = `
		UPDATE merchant_program_fee_schedules
		SET
			is_active = FALSE,
			deleted_at = NOW()
		WHERE id = $1
		  AND deleted_at IS NULL
		RETURNING deleted_at, updated_at
	`

	var deletedAt time.Time
	var updatedAt time.Time
	err := m.DB.QueryRow(ctx, query, id).Scan(&deletedAt, &updatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			err = fmt.Errorf(
				"no non-deleted merchant program fee schedule found with ID %s",
				id,
			)
		}

		logger.Error(
			"Soft delete merchant program fee schedule failed",
			err,
			"fee_schedule_id",
			id,
		)
		return err
	}

	logger.Info(
		"Soft delete merchant program fee schedule successful",
		"fee_schedule_id",
		id,
		"deleted_at",
		deletedAt,
		"updated_at",
		updatedAt,
	)
	return nil
}

// Restore restores a soft-deleted fee schedule in an inactive state.
func (m *MerchantProgramFeeScheduleModel) Restore(
	ctx context.Context,
	id uuid.UUID,
) error {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.
		GetLoggerWithContextFromContext(ctx).
		WithFunctionName("RestoreMerchantProgramFeeSchedule")

	if id == uuid.Nil {
		err := errors.New("merchant program fee schedule ID is required")
		logger.Error("Validation failed", err)
		return err
	}

	const query = `
		UPDATE merchant_program_fee_schedules
		SET
			is_active = FALSE,
			deleted_at = NULL
		WHERE id = $1
		  AND deleted_at IS NOT NULL
		RETURNING updated_at
	`

	var updatedAt time.Time
	err := m.DB.QueryRow(ctx, query, id).Scan(&updatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			err = fmt.Errorf(
				"no soft-deleted merchant program fee schedule found with ID %s",
				id,
			)
		} else {
			err = translateFeeScheduleWriteError(err)
		}

		logger.Error(
			"Restore merchant program fee schedule failed",
			err,
			"fee_schedule_id",
			id,
		)
		return err
	}

	logger.Info(
		"Restore merchant program fee schedule successful",
		"fee_schedule_id",
		id,
		"updated_at",
		updatedAt,
	)
	return nil
}

// HardDelete permanently removes a merchant program fee schedule.
func (m *MerchantProgramFeeScheduleModel) HardDelete(
	ctx context.Context,
	id uuid.UUID,
) error {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.
		GetLoggerWithContextFromContext(ctx).
		WithFunctionName("HardDeleteMerchantProgramFeeSchedule")

	if id == uuid.Nil {
		err := errors.New("merchant program fee schedule ID is required")
		logger.Error("Validation failed", err)
		return err
	}

	const query = `
		DELETE FROM merchant_program_fee_schedules
		WHERE id = $1
		RETURNING id
	`

	var deletedID uuid.UUID
	err := m.DB.QueryRow(ctx, query, id).Scan(&deletedID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			err = fmt.Errorf(
				"merchant program fee schedule not found: %s",
				id,
			)
		}

		logger.Error(
			"Hard delete merchant program fee schedule failed",
			err,
			"fee_schedule_id",
			id,
		)
		return err
	}

	logger.Info(
		"Hard delete merchant program fee schedule successful",
		"fee_schedule_id",
		deletedID,
	)
	return nil
}