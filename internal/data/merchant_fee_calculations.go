// Package data provides models and database access methods for merchant fee
// calculations.
//
// focodebase/fobackend/internal/data/merchant_fee_calculations.go
//
// GTM:
//
//	Layer: 2.4.b Commerce Architecture / Monetization Layer
//	Release Class: SPINE
//	Reason:
//	  merchant_fee_calculations is the canonical durable record of the monetary
//	  result produced for a merchant billable occurrence. It sits downstream of
//	  merchant_billable_events and merchant_program_fee_schedules and upstream of
//	  platform-credit applications, invoicing, and payment collection.
//
// Domain Boundary:
//
//	A merchant fee calculation records what was calculated. It is not a fee
//	schedule, invoice, invoice line, payment, payment attempt, platform-credit
//	application, billing account, commercial-policy store, or authorization
//	decision.
//
//	The service layer decides which configured commercial terms apply and when a
//	lifecycle transition is appropriate. This model validates canonical values,
//	preserves immutable calculation facts, enforces safe lifecycle transitions,
//	and translates database integrity failures. It does not decide whether fee 
//  categories, promotions, adjustments, or credits are commercially enabled.
//
// Source Integrity and Idempotency:
//
//	Every calculation currently belongs to exactly one merchant_billable_event.
//	The database enforces existence of that source through the canonical
//	billable_event_id foreign key. Because merchant_id is stored independently,
//	the database does not prove that the referenced billable event belongs to the
//	same merchant. Service orchestration must therefore validate merchant/source
//	ownership inside the caller-owned transaction before InsertTx. This model
//	exposes GetBillableEventFact and GetBillableEventFactTx for that purpose.
//	That ownership check is a cross-table integrity rule, not configurable
//	commercial policy.
//
//	At most one non-reversed calculation may exist for the same
//	(billable_event_id, fee_type_id) pair. The database owns this concurrency boundary
//	through a partial unique index. Recalculation is explicit: reverse the prior
//	calculation, then insert the replacement.
//
// Monetary Snapshot Boundary:
//
//	FeeTypeID, CalculationMethod, FeeRate, FlatFeeAmount, CalculatedFeeAmount,
//	Currency, CalculationBasis, BillableEventID, MerchantID, and FeeScheduleID are
//	immutable after insertion through this model.
//
//	Monetary and percentage values use decimal strings so PostgreSQL NUMERIC values
//	are never converted through binary floating point. SQL NULL remains distinct
//	from numeric zero.
//
//	FeeScheduleID is optional because the platform capability must support
//	properly governed calculations whose pricing provenance is not represented by
//	a standard program fee schedule. Whether a fee schedule is required for a
//	particular commercial path is service/configuration policy, not a data-model
//	invariant.
//
//	calculation_basis is a write-once JSON object containing calculation inputs or
//	provenance needed for auditability. It must not become a generic policy store,
//	a replacement for normalized domain entities, or a container for credits,
//	invoices, payments, promotions, or lifecycle state. PostgreSQL JSONB preserves
//	the semantic JSON value, not the caller's original whitespace or key ordering.
//
// Lifecycle:
//
//	pending  -> approved
//	pending  -> waived
//	pending  -> reversed
//	approved -> settled
//	approved -> waived
//	approved -> reversed
//	waived   -> reversed
//	settled  -> reversed
//
//	reversed is terminal. Reversal invalidates the current calculation outcome
//	without destroying history and releases the idempotency slot for an explicit
//	replacement calculation. Waiver remains distinct from reversal: waiver records
//	a zero-collection commercial outcome; reversal invalidates the calculation
//	record as the current outcome.
//
// Transaction Ownership:
//
//	This model never begins, commits, or rolls back caller-owned business
//	transactions. GetBillableEventFactTx, InsertTx, and lifecycle *Tx methods
//	accept a caller-owned pgx.Tx so service orchestration can validate source
//	ownership and persist the calculation atomically.
//
// SPINE Rule:
//
//	Keep compiling.
//	Keep production-ready.
//	Preserve exact decimal handling.
//	Preserve immutable calculation snapshots.
//	Preserve merchant/source relational integrity.
//	Preserve database-owned idempotency.
//	Preserve guarded lifecycle transitions and audit timestamps.
//	Preserve deterministic bounded reads.
//	Do not expose generic update, upsert, delete, soft-delete, or restore methods.
//	Do not embed commercial enablement, pricing-selection, promotion, credit,
//	invoicing, payment, or authorization policy.
//	Block deployment if this file breaks calculation persistence, monetary
//	precision, source integrity, idempotency, lifecycle integrity, auditability,
//	or downstream Commerce Architecture readiness.
package data

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/PiccoloMondoC/focodebase/fobackend/internal/logging"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const merchantFeeCalculationMaxPageSize = 100

var (
	merchantFeeCalculationAmountPattern = regexp.MustCompile(`^[0-9]{1,15}(?:\.[0-9]{1,4})?$`)
	merchantFeeCalculationRatePattern   = regexp.MustCompile(`^[0-9]{1,3}(?:\.[0-9]{1,6})?$`)
)

const merchantFeeCalculationSelectColumns = `
	id,
	merchant_id,
	billable_event_id,
	fee_schedule_id,
	fee_type_id,
	calculation_method,
	fee_rate,
	flat_fee_amount,
	calculated_fee_amount,
	currency,
	calculation_basis,
	calculated_at,
	status,
	approved_at,
	settled_at,
	waived_at,
	reversed_at,
	created_at,
	updated_at
`

const (
	merchantFeeCalculationMerchantFKConstraint      = "fk_merchant_fee_calculations_merchant"
	merchantFeeCalculationBillableEventFKConstraint = "fk_merchant_fee_calculations_billable_event"
	merchantFeeCalculationFeeScheduleFKConstraint   = "fk_merchant_fee_calculations_fee_schedule"
	merchantFeeCalculationFeeTypeFKConstraint       = "fk_merchant_fee_calculations_fee_type"

	merchantFeeCalculationActiveSourceUniqueIndex = "uq_merchant_fee_calculations_billable_event_fee_type_active"

	merchantFeeCalculationCalculationMethodCheckConstraint   = "chk_merchant_fee_calculations_calculation_method"
	merchantFeeCalculationMethodInputsCheckConstraint        = "chk_merchant_fee_calculations_method_inputs"
	merchantFeeCalculationFeeRateCheckConstraint             = "chk_merchant_fee_calculations_fee_rate"
	merchantFeeCalculationFlatFeeAmountCheckConstraint       = "chk_merchant_fee_calculations_flat_fee_amount"
	merchantFeeCalculationCalculatedFeeAmountCheckConstraint = "chk_merchant_fee_calculations_calculated_fee_amount"
	merchantFeeCalculationCurrencyCheckConstraint            = "chk_merchant_fee_calculations_currency"
	merchantFeeCalculationBasisObjectCheckConstraint         = "chk_merchant_fee_calculations_basis_object"
	merchantFeeCalculationStatusCheckConstraint              = "chk_merchant_fee_calculations_status"
	merchantFeeCalculationStatusTimestampCheckConstraint     = "chk_merchant_fee_calculations_status_timestamps"
	merchantFeeCalculationLifecycleTimeCheckConstraint       = "chk_merchant_fee_calculations_lifecycle_times"
)

// MerchantFeeCalculationStatus is the persisted lifecycle state of a merchant
// fee calculation.
type MerchantFeeCalculationStatus string

const (
	// MerchantFeeCalculationStatusPending is the initial persisted state.
	MerchantFeeCalculationStatusPending MerchantFeeCalculationStatus = "pending"

	// MerchantFeeCalculationStatusApproved indicates that the calculation has
	// been accepted as the current commercial obligation candidate.
	MerchantFeeCalculationStatusApproved MerchantFeeCalculationStatus = "approved"

	// MerchantFeeCalculationStatusSettled indicates that Commerce orchestration
	// has recorded this calculation's commercial obligation as settled.
	//
	// This is a Commerce-layer lifecycle fact, not a payment-provider transaction
	// status. Service orchestration owns the conditions and coordination required
	// before entering this state, including any Merchant Payments interaction
	// required by the applicable workflow.
	MerchantFeeCalculationStatusSettled MerchantFeeCalculationStatus = "settled"

	// MerchantFeeCalculationStatusWaived indicates that downstream commercial
	// orchestration has elected not to collect this calculated amount.
	MerchantFeeCalculationStatusWaived MerchantFeeCalculationStatus = "waived"

	// MerchantFeeCalculationStatusReversed indicates that this calculation is no
	// longer the current outcome for its billable-event/fee-type identity.
	MerchantFeeCalculationStatusReversed MerchantFeeCalculationStatus = "reversed"
)

// NormalizeMerchantFeeCalculationStatus returns the canonical status value.
func NormalizeMerchantFeeCalculationStatus(
	status MerchantFeeCalculationStatus,
) MerchantFeeCalculationStatus {
	return MerchantFeeCalculationStatus(normalizeIdentifier(string(status)))
}

// IsValidMerchantFeeCalculationStatus reports whether status belongs to the
// merchant fee calculation lifecycle vocabulary.
func IsValidMerchantFeeCalculationStatus(status MerchantFeeCalculationStatus) bool {
	switch NormalizeMerchantFeeCalculationStatus(status) {
	case MerchantFeeCalculationStatusPending,
		MerchantFeeCalculationStatusApproved,
		MerchantFeeCalculationStatusSettled,
		MerchantFeeCalculationStatusWaived,
		MerchantFeeCalculationStatusReversed:
		return true
	default:
		return false
	}
}

// MerchantFeeCalculation represents one durable fee calculation snapshot.
type MerchantFeeCalculation struct {
	ID                  uuid.UUID                    `json:"id" db:"id"`
	MerchantID          uuid.UUID                    `json:"merchant_id" db:"merchant_id"`
	BillableEventID     uuid.UUID                    `json:"billable_event_id" db:"billable_event_id"`
	FeeScheduleID       *uuid.UUID                   `json:"fee_schedule_id,omitempty" db:"fee_schedule_id"`
	FeeTypeID           uuid.UUID                    `json:"fee_type_id" db:"fee_type_id"`
	CalculationMethod   MerchantFeeCalculationMethod `json:"calculation_method" db:"calculation_method"`
	FeeRate             *string                      `json:"fee_rate,omitempty" db:"fee_rate"`
	FlatFeeAmount       *string                      `json:"flat_fee_amount,omitempty" db:"flat_fee_amount"`
	CalculatedFeeAmount string                       `json:"calculated_fee_amount" db:"calculated_fee_amount"`
	Currency            string                       `json:"currency" db:"currency"`
	CalculationBasis    json.RawMessage              `json:"calculation_basis" db:"calculation_basis"`
	CalculatedAt        time.Time                    `json:"calculated_at" db:"calculated_at"`
	Status              MerchantFeeCalculationStatus `json:"status" db:"status"`
	ApprovedAt          *time.Time                   `json:"approved_at,omitempty" db:"approved_at"`
	SettledAt           *time.Time                   `json:"settled_at,omitempty" db:"settled_at"`
	WaivedAt            *time.Time                   `json:"waived_at,omitempty" db:"waived_at"`
	ReversedAt          *time.Time                   `json:"reversed_at,omitempty" db:"reversed_at"`
	CreatedAt           time.Time                    `json:"created_at" db:"created_at"`
	UpdatedAt           time.Time                    `json:"updated_at" db:"updated_at"`
}

// MerchantFeeCalculationModel owns merchant fee calculation persistence.
type MerchantFeeCalculationModel struct {
	DB     *pgxpool.Pool
	Logger *logging.Logger
}

// MerchantFeeCalculationBillableEventFact contains the minimum authoritative
// billable-event facts required by service orchestration before calculation
// insertion.
//
// The data layer deliberately exposes facts rather than deciding workflow
// eligibility. Service orchestration owns merchant/source ownership validation
// and any configured rule governing which billable-event states may proceed to
// fee calculation.
type MerchantFeeCalculationBillableEventFact struct {
	MerchantID uuid.UUID                   `json:"merchant_id" db:"merchant_id"`
	Status     MerchantBillableEventStatus `json:"status" db:"status"`
}

type merchantFeeCalculationQueryRower interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

func (m *MerchantFeeCalculationModel) validateBase() error {
	if m == nil {
		return errors.New("merchant fee calculation model is required")
	}
	if m.Logger == nil {
		return errors.New("merchant fee calculation model logger is required")
	}
	return nil
}

func (m *MerchantFeeCalculationModel) validatePool() error {
	if err := m.validateBase(); err != nil {
		return err
	}
	if m.DB == nil {
		return errors.New("merchant fee calculation model database pool is required")
	}
	return nil
}

func scanMerchantFeeCalculation(
	row scannableRow,
	calc *MerchantFeeCalculation,
) error {
	var calculationBasis []byte

	if err := row.Scan(
		&calc.ID,
		&calc.MerchantID,
		&calc.BillableEventID,
		&calc.FeeScheduleID,
		&calc.FeeTypeID,
		&calc.CalculationMethod,
		&calc.FeeRate,
		&calc.FlatFeeAmount,
		&calc.CalculatedFeeAmount,
		&calc.Currency,
		&calculationBasis,
		&calc.CalculatedAt,
		&calc.Status,
		&calc.ApprovedAt,
		&calc.SettledAt,
		&calc.WaivedAt,
		&calc.ReversedAt,
		&calc.CreatedAt,
		&calc.UpdatedAt,
	); err != nil {
		return err
	}

	calc.CalculationBasis = json.RawMessage(calculationBasis)
	return nil
}

func merchantFeeCalculationInvalidInput(format string, args ...interface{}) error {
	return fmt.Errorf(
		"%w: %s",
		ErrMerchantFeeCalculationInvalidInput,
		fmt.Sprintf(format, args...),
	)
}

func classifyMerchantFeeCalculationWriteError(err error) error {
	switch {
	case IsPgConstraint(err, merchantFeeCalculationActiveSourceUniqueIndex):
		return ErrMerchantFeeCalculationDuplicate

	case IsPgConstraint(err, merchantFeeCalculationMerchantFKConstraint):
		return ErrMerchantFeeCalculationMerchantNotFound

	case IsPgConstraint(err, merchantFeeCalculationBillableEventFKConstraint):
		return ErrMerchantFeeCalculationBillableEventNotFound

	case IsPgConstraint(err, merchantFeeCalculationFeeScheduleFKConstraint):
		return ErrMerchantFeeCalculationFeeScheduleNotFound

	case IsPgConstraint(err, merchantFeeCalculationFeeTypeFKConstraint):
		return ErrMerchantFeeCalculationFeeTypeNotFound

	case IsPgConstraint(err, merchantFeeCalculationCalculationMethodCheckConstraint),
		IsPgConstraint(err, merchantFeeCalculationMethodInputsCheckConstraint),
		IsPgConstraint(err, merchantFeeCalculationFeeRateCheckConstraint),
		IsPgConstraint(err, merchantFeeCalculationFlatFeeAmountCheckConstraint),
		IsPgConstraint(err, merchantFeeCalculationCalculatedFeeAmountCheckConstraint),
		IsPgConstraint(err, merchantFeeCalculationCurrencyCheckConstraint),
		IsPgConstraint(err, merchantFeeCalculationBasisObjectCheckConstraint),
		IsPgConstraint(err, merchantFeeCalculationStatusCheckConstraint),
		IsPgConstraint(err, merchantFeeCalculationStatusTimestampCheckConstraint),
		IsPgConstraint(err, merchantFeeCalculationLifecycleTimeCheckConstraint):
		return ErrMerchantFeeCalculationInvalidState

	case IsUniqueViolation(err):
		return ErrMerchantFeeCalculationDuplicate

	case IsForeignKeyViolation(err),
		IsCheckViolation(err),
		IsNotNullViolation(err):
		return ErrMerchantFeeCalculationInvalidState

	default:
		return err
	}
}

func normalizeMerchantFeeCalculation(calc *MerchantFeeCalculation) {
	calc.CalculationMethod = NormalizeMerchantFeeCalculationMethod(
		calc.CalculationMethod,
	)
	normalizeOptionalDecimal(&calc.FeeRate)
	normalizeOptionalDecimal(&calc.FlatFeeAmount)
	calc.CalculatedFeeAmount = strings.TrimSpace(calc.CalculatedFeeAmount)
	calc.Currency = strings.ToUpper(strings.TrimSpace(calc.Currency))

	basis := bytes.TrimSpace(calc.CalculationBasis)
	if len(basis) == 0 {
		basis = []byte(`{}`)
	}
	calc.CalculationBasis = json.RawMessage(basis)
}

func validateMerchantFeeCalculationBasis(basis json.RawMessage) error {
	if !json.Valid(basis) {
		return merchantFeeCalculationInvalidInput(
			"calculation_basis must be valid JSON",
		)
	}

	trimmed := bytes.TrimSpace(basis)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return merchantFeeCalculationInvalidInput(
			"calculation_basis must be a JSON object",
		)
	}

	return nil
}

func validateMerchantFeeCalculationOptionalDecimal(
	value *string,
	fieldName string,
	pattern *regexp.Regexp,
	shapeDescription string,
) error {
	if value == nil {
		return nil
	}

	if err := validateOptionalNonNegativeDecimalString(value, fieldName); err != nil {
		return merchantFeeCalculationInvalidInput("%v", err)
	}

	if !pattern.MatchString(*value) {
		return merchantFeeCalculationInvalidInput(
			"%s must be a non-negative %s-compatible decimal",
			fieldName,
			shapeDescription,
		)
	}

	return nil
}

func validateMerchantFeeCalculationMethodInputs(
	calc *MerchantFeeCalculation,
) error {
	switch calc.CalculationMethod {
	case MerchantFeeCalculationFlat:
		if calc.FlatFeeAmount == nil || calc.FeeRate != nil {
			return merchantFeeCalculationInvalidInput(
				"flat calculation requires flat_fee_amount and no fee_rate",
			)
		}

	case MerchantFeeCalculationPercentage:
		if calc.FeeRate == nil || calc.FlatFeeAmount != nil {
			return merchantFeeCalculationInvalidInput(
				"percentage calculation requires fee_rate and no flat_fee_amount",
			)
		}

	case MerchantFeeCalculationHybrid:
		if calc.FeeRate == nil || calc.FlatFeeAmount == nil {
			return merchantFeeCalculationInvalidInput(
				"hybrid calculation requires fee_rate and flat_fee_amount",
			)
		}

	case MerchantFeeCalculationNegotiated:
		// Negotiated pricing may legitimately snapshot either, both, or neither
		// component. The resulting amount and provenance remain required.
	}

	return nil
}

func validateMerchantFeeCalculationForInsert(calc *MerchantFeeCalculation) error {
	if calc == nil {
		return merchantFeeCalculationInvalidInput("fee calculation is required")
	}

	normalizeMerchantFeeCalculation(calc)

	if calc.MerchantID == uuid.Nil {
		return merchantFeeCalculationInvalidInput("merchant_id is required")
	}
	if calc.BillableEventID == uuid.Nil {
		return merchantFeeCalculationInvalidInput("billable_event_id is required")
	}
	if calc.FeeScheduleID != nil && *calc.FeeScheduleID == uuid.Nil {
		return merchantFeeCalculationInvalidInput(
			"fee_schedule_id must not be a nil UUID",
		)
	}
	if calc.FeeTypeID == uuid.Nil {
		return merchantFeeCalculationInvalidInput("fee_type_id is required")
	}
	if !IsValidMerchantFeeCalculationMethod(calc.CalculationMethod) {
		return merchantFeeCalculationInvalidInput(
			"invalid calculation_method: %q",
			calc.CalculationMethod,
		)
	}

	if err := validateMerchantFeeCalculationMethodInputs(calc); err != nil {
		return err
	}

	if err := validateMerchantFeeCalculationOptionalDecimal(
		calc.FeeRate,
		"fee_rate",
		merchantFeeCalculationRatePattern,
		"NUMERIC(9,6)",
	); err != nil {
		return err
	}

	if err := validateMerchantFeeCalculationOptionalDecimal(
		calc.FlatFeeAmount,
		"flat_fee_amount",
		merchantFeeCalculationAmountPattern,
		"NUMERIC(19,4)",
	); err != nil {
		return err
	}

	if err := validateNonNegativeDecimalString(
		calc.CalculatedFeeAmount,
		"calculated_fee_amount",
	); err != nil {
		return merchantFeeCalculationInvalidInput("%v", err)
	}
	if !merchantFeeCalculationAmountPattern.MatchString(calc.CalculatedFeeAmount) {
		return merchantFeeCalculationInvalidInput(
			"calculated_fee_amount must be a non-negative NUMERIC(19,4)-compatible decimal",
		)
	}

	if calc.Currency == "" {
		calc.Currency = "USD"
	}
	if !isCanonicalCurrency(calc.Currency) {
		return merchantFeeCalculationInvalidInput(
			"invalid currency: %q",
			calc.Currency,
		)
	}

	if err := validateMerchantFeeCalculationBasis(calc.CalculationBasis); err != nil {
		return err
	}

	if calc.Status != "" &&
		NormalizeMerchantFeeCalculationStatus(calc.Status) !=
			MerchantFeeCalculationStatusPending {
		return merchantFeeCalculationInvalidInput(
			"status is database-owned at creation",
		)
	}

	if calc.ApprovedAt != nil ||
		calc.SettledAt != nil ||
		calc.WaivedAt != nil ||
		calc.ReversedAt != nil {
		return merchantFeeCalculationInvalidInput(
			"lifecycle timestamps must not be supplied at creation",
		)
	}

	return nil
}

func validateMerchantFeeCalculationPersistedState(
	calc *MerchantFeeCalculation,
) error {
	if calc == nil ||
		calc.ID == uuid.Nil ||
		calc.MerchantID == uuid.Nil ||
		calc.BillableEventID == uuid.Nil ||
		calc.CalculatedAt.IsZero() ||
		calc.CreatedAt.IsZero() ||
		calc.UpdatedAt.IsZero() {
		return ErrMerchantFeeCalculationInvalidState
	}

	if calc.FeeTypeID == uuid.Nil ||
		!IsValidMerchantFeeCalculationMethod(calc.CalculationMethod) ||
		!IsValidMerchantFeeCalculationStatus(calc.Status) ||
		!json.Valid(calc.CalculationBasis) {
		return ErrMerchantFeeCalculationInvalidState
	}

	trimmedBasis := bytes.TrimSpace(calc.CalculationBasis)
	if len(trimmedBasis) == 0 || trimmedBasis[0] != '{' {
		return ErrMerchantFeeCalculationInvalidState
	}

	status := NormalizeMerchantFeeCalculationStatus(calc.Status)
	switch status {
	case MerchantFeeCalculationStatusPending:
		if calc.ApprovedAt != nil || calc.SettledAt != nil ||
			calc.WaivedAt != nil || calc.ReversedAt != nil {
			return ErrMerchantFeeCalculationInvalidState
		}

	case MerchantFeeCalculationStatusApproved:
		if calc.ApprovedAt == nil || calc.SettledAt != nil ||
			calc.WaivedAt != nil || calc.ReversedAt != nil {
			return ErrMerchantFeeCalculationInvalidState
		}

	case MerchantFeeCalculationStatusSettled:
		if calc.ApprovedAt == nil || calc.SettledAt == nil ||
			calc.WaivedAt != nil || calc.ReversedAt != nil {
			return ErrMerchantFeeCalculationInvalidState
		}

	case MerchantFeeCalculationStatusWaived:
		if calc.SettledAt != nil || calc.WaivedAt == nil || calc.ReversedAt != nil {
			return ErrMerchantFeeCalculationInvalidState
		}

	case MerchantFeeCalculationStatusReversed:
		if calc.ReversedAt == nil {
			return ErrMerchantFeeCalculationInvalidState
		}

		validReversedShape :=
			(calc.SettledAt == nil && calc.WaivedAt == nil) ||
				(calc.ApprovedAt != nil &&
					calc.SettledAt != nil &&
					calc.WaivedAt == nil) ||
				(calc.SettledAt == nil && calc.WaivedAt != nil)

		if !validReversedShape {
			return ErrMerchantFeeCalculationInvalidState
		}
	}

	if calc.ApprovedAt != nil && calc.ApprovedAt.Before(calc.CalculatedAt) {
		return ErrMerchantFeeCalculationInvalidState
	}
	if calc.SettledAt != nil {
		if calc.ApprovedAt == nil || calc.SettledAt.Before(*calc.ApprovedAt) {
			return ErrMerchantFeeCalculationInvalidState
		}
	}
	if calc.WaivedAt != nil {
		waiverFloor := calc.CalculatedAt
		if calc.ApprovedAt != nil {
			waiverFloor = *calc.ApprovedAt
		}
		if calc.WaivedAt.Before(waiverFloor) {
			return ErrMerchantFeeCalculationInvalidState
		}
	}
	if calc.ReversedAt != nil {
		reversalFloor := calc.CalculatedAt
		switch {
		case calc.SettledAt != nil:
			reversalFloor = *calc.SettledAt
		case calc.WaivedAt != nil:
			reversalFloor = *calc.WaivedAt
		case calc.ApprovedAt != nil:
			reversalFloor = *calc.ApprovedAt
		}
		if calc.ReversedAt.Before(reversalFloor) {
			return ErrMerchantFeeCalculationInvalidState
		}
	}

	return nil
}

func validateMerchantFeeCalculationID(id uuid.UUID) error {
	if id == uuid.Nil {
		return merchantFeeCalculationInvalidInput("id is required")
	}
	return nil
}

func validateMerchantFeeCalculationMerchantID(merchantID uuid.UUID) error {
	if merchantID == uuid.Nil {
		return merchantFeeCalculationInvalidInput("merchant_id is required")
	}
	return nil
}

func validateMerchantFeeCalculationPageSize(limit int) error {
	if limit <= 0 || limit > merchantFeeCalculationMaxPageSize {
		return merchantFeeCalculationInvalidInput(
			"limit must be between 1 and %d",
			merchantFeeCalculationMaxPageSize,
		)
	}
	return nil
}

func validateMerchantFeeCalculationBeforeCursor(
	beforeCalculatedAt *time.Time,
	beforeID *uuid.UUID,
) error {
	if (beforeCalculatedAt == nil) != (beforeID == nil) {
		return merchantFeeCalculationInvalidInput(
			"before_calculated_at and before_id must be supplied together",
		)
	}
	if beforeCalculatedAt == nil {
		return nil
	}
	if beforeCalculatedAt.IsZero() {
		return merchantFeeCalculationInvalidInput(
			"before_calculated_at must not be zero",
		)
	}
	if *beforeID == uuid.Nil {
		return merchantFeeCalculationInvalidInput(
			"before_id must not be a nil UUID",
		)
	}
	return nil
}

func (m *MerchantFeeCalculationModel) getBillableEventFactViaQuerier(
	ctx context.Context,
	querier merchantFeeCalculationQueryRower,
	billableEventID uuid.UUID,
) (*MerchantFeeCalculationBillableEventFact, error) {
	const query = `
		SELECT
			merchant_id,
			status
		FROM merchant_billable_events
		WHERE id = $1
	`

	var fact MerchantFeeCalculationBillableEventFact
	if err := querier.QueryRow(ctx, query, billableEventID).Scan(
		&fact.MerchantID,
		&fact.Status,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}

	return &fact, nil
}

// GetBillableEventFact retrieves the authoritative merchant and lifecycle state
// for the billable event referenced by a prospective fee calculation.
//
// Absence returns nil, nil. This method reports source facts only; it does not
// decide whether the event is eligible for calculation.
func (m *MerchantFeeCalculationModel) GetBillableEventFact(
	ctx context.Context,
	billableEventID uuid.UUID,
) (*MerchantFeeCalculationBillableEventFact, error) {
	if err := m.validatePool(); err != nil {
		return nil, err
	}
	if billableEventID == uuid.Nil {
		return nil, merchantFeeCalculationInvalidInput(
			"billable_event_id is required",
		)
	}

	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	fact, err := m.getBillableEventFactViaQuerier(
		ctx,
		m.DB,
		billableEventID,
	)
	if err != nil {
		return nil, fmt.Errorf(
			"get merchant fee calculation billable event fact: %w",
			err,
		)
	}

	return fact, nil
}

// GetBillableEventFactTx is the transaction-aware form of GetBillableEventFact.
//
// The authoritative source read executes exclusively through tx. The caller
// owns transaction lifecycle and may use the returned merchant/status facts to
// enforce cross-table integrity and workflow rules before calling InsertTx.
func (m *MerchantFeeCalculationModel) GetBillableEventFactTx(
	ctx context.Context,
	tx pgx.Tx,
	billableEventID uuid.UUID,
) (*MerchantFeeCalculationBillableEventFact, error) {
	if err := m.validateBase(); err != nil {
		return nil, err
	}
	if tx == nil {
		return nil, merchantFeeCalculationInvalidInput(
			"transaction is required",
		)
	}
	if billableEventID == uuid.Nil {
		return nil, merchantFeeCalculationInvalidInput(
			"billable_event_id is required",
		)
	}

	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	fact, err := m.getBillableEventFactViaQuerier(
		ctx,
		tx,
		billableEventID,
	)
	if err != nil {
		return nil, fmt.Errorf(
			"get merchant fee calculation billable event fact in transaction: %w",
			err,
		)
	}

	return fact, nil
}

func merchantFeeCalculationLogFields(calc *MerchantFeeCalculation) []any {
	if calc == nil {
		return nil
	}

	fields := []any{
		"fee_calculation_id", calc.ID,
		"merchant_id", calc.MerchantID,
		"billable_event_id", calc.BillableEventID,
		"fee_type_id", calc.FeeTypeID,
		"calculation_method", calc.CalculationMethod,
		"status", calc.Status,
	}
	if calc.FeeScheduleID != nil {
		fields = append(fields, "fee_schedule_id", *calc.FeeScheduleID)
	}
	return fields
}

func (m *MerchantFeeCalculationModel) insert(
	ctx context.Context,
	querier merchantFeeCalculationQueryRower,
	functionName string,
	calc *MerchantFeeCalculation,
) (*MerchantFeeCalculation, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.
		GetLoggerWithContextFromContext(ctx).
		WithFunctionName(functionName)

	if err := validateMerchantFeeCalculationForInsert(calc); err != nil {
		logger.Error("Validation failed", err)
		return nil, err
	}

	id := calc.ID
	if id == uuid.Nil {
		id = uuid.New()
	}

	const query = `
		INSERT INTO merchant_fee_calculations (
			id,
			merchant_id,
			billable_event_id,
			fee_schedule_id,
			fee_type_id,
			calculation_method,
			fee_rate,
			flat_fee_amount,
			calculated_fee_amount,
			currency,
			calculation_basis
		)
		VALUES (
			$1, $2, $3, $4, $5, $6,
			$7::numeric, $8::numeric, $9::numeric,
			$10, $11::jsonb
		)
		RETURNING ` + merchantFeeCalculationSelectColumns

	var result MerchantFeeCalculation
	if err := scanMerchantFeeCalculation(
		querier.QueryRow(
			ctx,
			query,
			id,
			calc.MerchantID,
			calc.BillableEventID,
			calc.FeeScheduleID,
			calc.FeeTypeID,
			calc.CalculationMethod,
			calc.FeeRate,
			calc.FlatFeeAmount,
			calc.CalculatedFeeAmount,
			calc.Currency,
			[]byte(calc.CalculationBasis),
		),
		&result,
	); err != nil {
		err = classifyMerchantFeeCalculationWriteError(err)
		fields := append([]any{err}, merchantFeeCalculationLogFields(calc)...)

		if errors.Is(err, ErrMerchantFeeCalculationDuplicate) {
			logger.Warn("Merchant fee calculation already active", fields...)
			return nil, err
		}

		logger.Error("Insert merchant fee calculation failed", fields...)
		return nil, err
	}

	if err := validateMerchantFeeCalculationPersistedState(&result); err != nil {
		logger.Error(
			"Insert merchant fee calculation returned invalid state",
			err,
			"fee_calculation_id", result.ID,
		)
		return nil, err
	}

	logger.Info(
		"Insert merchant fee calculation successful",
		"fee_calculation_id", result.ID,
		"merchant_id", result.MerchantID,
		"billable_event_id", result.BillableEventID,
		"fee_type_id", result.FeeTypeID,
		"status", result.Status,
	)

	return &result, nil
}

// Insert records a merchant fee calculation through the model database pool.
func (m *MerchantFeeCalculationModel) Insert(
	ctx context.Context,
	calc *MerchantFeeCalculation,
) (*MerchantFeeCalculation, error) {
	if err := m.validatePool(); err != nil {
		return nil, err
	}
	return m.insert(ctx, m.DB, "InsertMerchantFeeCalculation", calc)
}

// InsertTx records a merchant fee calculation through caller-owned tx.
func (m *MerchantFeeCalculationModel) InsertTx(
	ctx context.Context,
	tx pgx.Tx,
	calc *MerchantFeeCalculation,
) (*MerchantFeeCalculation, error) {
	if err := m.validateBase(); err != nil {
		return nil, err
	}
	if tx == nil {
		return nil, merchantFeeCalculationInvalidInput("transaction is required")
	}
	return m.insert(ctx, tx, "InsertMerchantFeeCalculationTx", calc)
}

func (m *MerchantFeeCalculationModel) getByIDViaQuerier(
	ctx context.Context,
	querier merchantFeeCalculationQueryRower,
	id uuid.UUID,
) (*MerchantFeeCalculation, error) {
	const query = `
		SELECT ` + merchantFeeCalculationSelectColumns + `
		FROM merchant_fee_calculations
		WHERE id = $1
	`

	var calc MerchantFeeCalculation
	if err := scanMerchantFeeCalculation(
		querier.QueryRow(ctx, query, id),
		&calc,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}

	if err := validateMerchantFeeCalculationPersistedState(&calc); err != nil {
		return nil, err
	}
	return &calc, nil
}

// GetByID retrieves a merchant fee calculation by ID. Absence returns nil, nil.
func (m *MerchantFeeCalculationModel) GetByID(
	ctx context.Context,
	id uuid.UUID,
) (*MerchantFeeCalculation, error) {
	if err := m.validatePool(); err != nil {
		return nil, err
	}
	if err := validateMerchantFeeCalculationID(id); err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	calc, err := m.getByIDViaQuerier(ctx, m.DB, id)
	if err != nil {
		return nil, fmt.Errorf("get merchant fee calculation by ID: %w", err)
	}
	return calc, nil
}

// GetByIDForUpdateTx retrieves one merchant fee calculation through the
// caller-owned transaction and locks the row using SELECT ... FOR UPDATE.
//
// This is the canonical serialization primitive for downstream financial
// composition against one fee calculation. Callers must acquire this lock
// before reading transaction-scoped aggregates whose correctness depends on
// every concurrent writer against the same calculation serializing through
// one authoritative row.
//
// The caller owns transaction lifetime and timeout. This method never begins,
// commits, rolls back, or applies an independent transaction timeout.
//
// Absence returns (nil, nil), consistent with GetByID.
func (m *MerchantFeeCalculationModel) GetByIDForUpdateTx(
	ctx context.Context,
	tx pgx.Tx,
	id uuid.UUID,
) (*MerchantFeeCalculation, error) {
	if err := m.validateBase(); err != nil {
		return nil, err
	}

	if tx == nil {
		return nil,
			merchantFeeCalculationInvalidInput(
				"transaction is required",
			)
	}

	if err := validateMerchantFeeCalculationID(id); err != nil {
		return nil, err
	}

	const query = `
		SELECT ` + merchantFeeCalculationSelectColumns + `
		FROM merchant_fee_calculations
		WHERE id = $1
		FOR UPDATE
	`

	var calc MerchantFeeCalculation

	if err := scanMerchantFeeCalculation(
		tx.QueryRow(ctx, query, id),
		&calc,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}

		return nil, fmt.Errorf(
			"get merchant fee calculation by ID for update in transaction: %w",
			err,
		)
	}

	if err := validateMerchantFeeCalculationPersistedState(&calc); err != nil {
		return nil, err
	}

	return &calc, nil
}

// GetActiveByBillableEventAndFeeType retrieves the current non-reversed
// calculation for a billable-event/fee-type identity. Absence returns nil, nil.
func (m *MerchantFeeCalculationModel) GetActiveByBillableEventAndFeeType(
	ctx context.Context,
	billableEventID uuid.UUID,
	feeTypeID uuid.UUID,
) (*MerchantFeeCalculation, error) {
	if err := m.validatePool(); err != nil {
		return nil, err
	}
	if billableEventID == uuid.Nil {
		return nil, merchantFeeCalculationInvalidInput(
			"billable_event_id is required",
		)
	}

	if feeTypeID == uuid.Nil {
		return nil, merchantFeeCalculationInvalidInput("fee_type_id is required")
	}

	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	const query = `
		SELECT ` + merchantFeeCalculationSelectColumns + `
		FROM merchant_fee_calculations
		WHERE billable_event_id = $1
		  AND fee_type_id = $2
		  AND status <> 'reversed'
		ORDER BY calculated_at DESC, id DESC
		LIMIT 1
	`

	var calc MerchantFeeCalculation
	if err := scanMerchantFeeCalculation(
		m.DB.QueryRow(ctx, query, billableEventID, feeTypeID),
		&calc,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf(
			"get active merchant fee calculation by billable event and fee type: %w",
			err,
		)
	}

	if err := validateMerchantFeeCalculationPersistedState(&calc); err != nil {
		return nil, err
	}
	return &calc, nil
}

// ListByBillableEventID returns newest-first fee-calculation history for a
// billable event, including reversed records.
func (m *MerchantFeeCalculationModel) ListByBillableEventID(
	ctx context.Context,
	billableEventID uuid.UUID,
	limit int,
) ([]*MerchantFeeCalculation, error) {
	if err := m.validatePool(); err != nil {
		return nil, err
	}
	if billableEventID == uuid.Nil {
		return nil, merchantFeeCalculationInvalidInput(
			"billable_event_id is required",
		)
	}
	if err := validateMerchantFeeCalculationPageSize(limit); err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	const query = `
		SELECT ` + merchantFeeCalculationSelectColumns + `
		FROM merchant_fee_calculations
		WHERE billable_event_id = $1
		ORDER BY calculated_at DESC, id DESC
		LIMIT $2
	`

	rows, err := m.DB.Query(ctx, query, billableEventID, limit)
	if err != nil {
		return nil, fmt.Errorf("list merchant fee calculations by billable event: %w", err)
	}
	defer rows.Close()

	calculations := make([]*MerchantFeeCalculation, 0, limit)
	for rows.Next() {
		var calc MerchantFeeCalculation
		if err := scanMerchantFeeCalculation(rows, &calc); err != nil {
			return nil, err
		}
		if err := validateMerchantFeeCalculationPersistedState(&calc); err != nil {
			return nil, err
		}
		calculations = append(calculations, &calc)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return calculations, nil
}

// ListByMerchant returns a bounded keyset-paginated merchant timeline ordered
// by calculated_at DESC, id DESC.
func (m *MerchantFeeCalculationModel) ListByMerchant(
	ctx context.Context,
	merchantID uuid.UUID,
	limit int,
	beforeCalculatedAt *time.Time,
	beforeID *uuid.UUID,
) ([]*MerchantFeeCalculation, error) {
	return m.listByMerchant(
		ctx,
		merchantID,
		nil,
		limit,
		beforeCalculatedAt,
		beforeID,
	)
}

// ListByMerchantAndStatus returns a bounded keyset-paginated merchant timeline
// filtered by lifecycle status.
func (m *MerchantFeeCalculationModel) ListByMerchantAndStatus(
	ctx context.Context,
	merchantID uuid.UUID,
	status MerchantFeeCalculationStatus,
	limit int,
	beforeCalculatedAt *time.Time,
	beforeID *uuid.UUID,
) ([]*MerchantFeeCalculation, error) {
	status = NormalizeMerchantFeeCalculationStatus(status)
	if !IsValidMerchantFeeCalculationStatus(status) {
		return nil, merchantFeeCalculationInvalidInput(
			"invalid status: %q",
			status,
		)
	}

	return m.listByMerchant(
		ctx,
		merchantID,
		&status,
		limit,
		beforeCalculatedAt,
		beforeID,
	)
}

func (m *MerchantFeeCalculationModel) listByMerchant(
	ctx context.Context,
	merchantID uuid.UUID,
	status *MerchantFeeCalculationStatus,
	limit int,
	beforeCalculatedAt *time.Time,
	beforeID *uuid.UUID,
) ([]*MerchantFeeCalculation, error) {
	if err := m.validatePool(); err != nil {
		return nil, err
	}
	if err := validateMerchantFeeCalculationMerchantID(merchantID); err != nil {
		return nil, err
	}
	if err := validateMerchantFeeCalculationPageSize(limit); err != nil {
		return nil, err
	}
	if err := validateMerchantFeeCalculationBeforeCursor(
		beforeCalculatedAt,
		beforeID,
	); err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	var (
		rows pgx.Rows
		err  error
	)

	switch {
	case status == nil && beforeCalculatedAt == nil:
		const query = `
			SELECT ` + merchantFeeCalculationSelectColumns + `
			FROM merchant_fee_calculations
			WHERE merchant_id = $1
			ORDER BY calculated_at DESC, id DESC
			LIMIT $2
		`
		rows, err = m.DB.Query(ctx, query, merchantID, limit)

	case status == nil:
		const query = `
			SELECT ` + merchantFeeCalculationSelectColumns + `
			FROM merchant_fee_calculations
			WHERE merchant_id = $1
			  AND (calculated_at, id) < ($2, $3)
			ORDER BY calculated_at DESC, id DESC
			LIMIT $4
		`
		rows, err = m.DB.Query(
			ctx,
			query,
			merchantID,
			beforeCalculatedAt.UTC(),
			*beforeID,
			limit,
		)

	case beforeCalculatedAt == nil:
		const query = `
			SELECT ` + merchantFeeCalculationSelectColumns + `
			FROM merchant_fee_calculations
			WHERE merchant_id = $1
			  AND status = $2
			ORDER BY calculated_at DESC, id DESC
			LIMIT $3
		`
		rows, err = m.DB.Query(ctx, query, merchantID, *status, limit)

	default:
		const query = `
			SELECT ` + merchantFeeCalculationSelectColumns + `
			FROM merchant_fee_calculations
			WHERE merchant_id = $1
			  AND status = $2
			  AND (calculated_at, id) < ($3, $4)
			ORDER BY calculated_at DESC, id DESC
			LIMIT $5
		`
		rows, err = m.DB.Query(
			ctx,
			query,
			merchantID,
			*status,
			beforeCalculatedAt.UTC(),
			*beforeID,
			limit,
		)
	}

	if err != nil {
		return nil, fmt.Errorf("list merchant fee calculations by merchant: %w", err)
	}
	defer rows.Close()

	calculations := make([]*MerchantFeeCalculation, 0, limit)
	for rows.Next() {
		var calc MerchantFeeCalculation
		if err := scanMerchantFeeCalculation(rows, &calc); err != nil {
			return nil, err
		}
		if err := validateMerchantFeeCalculationPersistedState(&calc); err != nil {
			return nil, err
		}
		calculations = append(calculations, &calc)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return calculations, nil
}

const merchantFeeCalculationApproveQuery = `
	UPDATE merchant_fee_calculations
	SET
		status = 'approved',
		approved_at = NOW(),
		updated_at = NOW()
	WHERE id = $1
	  AND status = 'pending'
	RETURNING ` + merchantFeeCalculationSelectColumns

const merchantFeeCalculationWaiveQuery = `
	UPDATE merchant_fee_calculations
	SET
		status = 'waived',
		waived_at = NOW(),
		updated_at = NOW()
	WHERE id = $1
	  AND status IN ('pending', 'approved')
	RETURNING ` + merchantFeeCalculationSelectColumns

const merchantFeeCalculationSettleQuery = `
	UPDATE merchant_fee_calculations
	SET
		status = 'settled',
		settled_at = NOW(),
		updated_at = NOW()
	WHERE id = $1
	  AND status = 'approved'
	RETURNING ` + merchantFeeCalculationSelectColumns

const merchantFeeCalculationReverseQuery = `
	UPDATE merchant_fee_calculations
	SET
		status = 'reversed',
		reversed_at = NOW(),
		updated_at = NOW()
	WHERE id = $1
	  AND status IN ('pending', 'approved', 'waived', 'settled')
	RETURNING ` + merchantFeeCalculationSelectColumns

func (m *MerchantFeeCalculationModel) resolveTransitionNoMatch(
	ctx context.Context,
	querier merchantFeeCalculationQueryRower,
	id uuid.UUID,
	toStatus MerchantFeeCalculationStatus,
) (*MerchantFeeCalculation, error) {
	calc, err := m.getByIDViaQuerier(ctx, querier, id)
	if err != nil {
		return nil, err
	}
	if calc == nil {
		return nil, ErrMerchantFeeCalculationNotFound
	}
	if NormalizeMerchantFeeCalculationStatus(calc.Status) == toStatus {
		return calc, nil
	}
	return nil, ErrMerchantFeeCalculationInvalidTransition
}

func (m *MerchantFeeCalculationModel) transition(
	ctx context.Context,
	querier merchantFeeCalculationQueryRower,
	functionName string,
	query string,
	id uuid.UUID,
	toStatus MerchantFeeCalculationStatus,
) (*MerchantFeeCalculation, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.
		GetLoggerWithContextFromContext(ctx).
		WithFunctionName(functionName)

	if err := validateMerchantFeeCalculationID(id); err != nil {
		logger.Error("Validation failed", err)
		return nil, err
	}

	var calc MerchantFeeCalculation
	if err := scanMerchantFeeCalculation(
		querier.QueryRow(ctx, query, id),
		&calc,
	); err == nil {
		if err := validateMerchantFeeCalculationPersistedState(&calc); err != nil {
			logger.Error(
				"Merchant fee calculation transition returned invalid state",
				err,
				"fee_calculation_id", calc.ID,
				"to_status", toStatus,
			)
			return nil, err
		}

		logger.Info(
			"Merchant fee calculation lifecycle transition successful",
			"fee_calculation_id", calc.ID,
			"merchant_id", calc.MerchantID,
			"status", calc.Status,
		)

		return &calc, nil
	} else if !errors.Is(err, pgx.ErrNoRows) {
		err = classifyMerchantFeeCalculationWriteError(err)
		logger.Error(
			"Merchant fee calculation lifecycle transition failed",
			err,
			"fee_calculation_id", id,
			"to_status", toStatus,
		)
		return nil, err
	}

	return m.resolveTransitionNoMatch(ctx, querier, id, toStatus)
}

// Approve transitions a pending calculation to approved.
func (m *MerchantFeeCalculationModel) Approve(
	ctx context.Context,
	id uuid.UUID,
) (*MerchantFeeCalculation, error) {
	if err := m.validatePool(); err != nil {
		return nil, err
	}
	return m.transition(
		ctx,
		m.DB,
		"ApproveMerchantFeeCalculation",
		merchantFeeCalculationApproveQuery,
		id,
		MerchantFeeCalculationStatusApproved,
	)
}

// ApproveTx is the transaction-aware form of Approve.
func (m *MerchantFeeCalculationModel) ApproveTx(
	ctx context.Context,
	tx pgx.Tx,
	id uuid.UUID,
) (*MerchantFeeCalculation, error) {
	if err := m.validateBase(); err != nil {
		return nil, err
	}
	if tx == nil {
		return nil, merchantFeeCalculationInvalidInput("transaction is required")
	}
	return m.transition(
		ctx,
		tx,
		"ApproveMerchantFeeCalculationTx",
		merchantFeeCalculationApproveQuery,
		id,
		MerchantFeeCalculationStatusApproved,
	)
}

// Waive transitions a pending or approved calculation to waived.
func (m *MerchantFeeCalculationModel) Waive(
	ctx context.Context,
	id uuid.UUID,
) (*MerchantFeeCalculation, error) {
	if err := m.validatePool(); err != nil {
		return nil, err
	}
	return m.transition(
		ctx,
		m.DB,
		"WaiveMerchantFeeCalculation",
		merchantFeeCalculationWaiveQuery,
		id,
		MerchantFeeCalculationStatusWaived,
	)
}

// WaiveTx is the transaction-aware form of Waive.
func (m *MerchantFeeCalculationModel) WaiveTx(
	ctx context.Context,
	tx pgx.Tx,
	id uuid.UUID,
) (*MerchantFeeCalculation, error) {
	if err := m.validateBase(); err != nil {
		return nil, err
	}
	if tx == nil {
		return nil, merchantFeeCalculationInvalidInput("transaction is required")
	}
	return m.transition(
		ctx,
		tx,
		"WaiveMerchantFeeCalculationTx",
		merchantFeeCalculationWaiveQuery,
		id,
		MerchantFeeCalculationStatusWaived,
	)
}

// Settle transitions an approved calculation to settled.
func (m *MerchantFeeCalculationModel) Settle(
	ctx context.Context,
	id uuid.UUID,
) (*MerchantFeeCalculation, error) {
	if err := m.validatePool(); err != nil {
		return nil, err
	}
	return m.transition(
		ctx,
		m.DB,
		"SettleMerchantFeeCalculation",
		merchantFeeCalculationSettleQuery,
		id,
		MerchantFeeCalculationStatusSettled,
	)
}

// SettleTx is the transaction-aware form of Settle.
func (m *MerchantFeeCalculationModel) SettleTx(
	ctx context.Context,
	tx pgx.Tx,
	id uuid.UUID,
) (*MerchantFeeCalculation, error) {
	if err := m.validateBase(); err != nil {
		return nil, err
	}
	if tx == nil {
		return nil, merchantFeeCalculationInvalidInput("transaction is required")
	}
	return m.transition(
		ctx,
		tx,
		"SettleMerchantFeeCalculationTx",
		merchantFeeCalculationSettleQuery,
		id,
		MerchantFeeCalculationStatusSettled,
	)
}

// Reverse invalidates a non-reversed calculation while preserving its history.
func (m *MerchantFeeCalculationModel) Reverse(
	ctx context.Context,
	id uuid.UUID,
) (*MerchantFeeCalculation, error) {
	if err := m.validatePool(); err != nil {
		return nil, err
	}
	return m.transition(
		ctx,
		m.DB,
		"ReverseMerchantFeeCalculation",
		merchantFeeCalculationReverseQuery,
		id,
		MerchantFeeCalculationStatusReversed,
	)
}

// ReverseTx is the transaction-aware form of Reverse.
func (m *MerchantFeeCalculationModel) ReverseTx(
	ctx context.Context,
	tx pgx.Tx,
	id uuid.UUID,
) (*MerchantFeeCalculation, error) {
	if err := m.validateBase(); err != nil {
		return nil, err
	}
	if tx == nil {
		return nil, merchantFeeCalculationInvalidInput("transaction is required")
	}
	return m.transition(
		ctx,
		tx,
		"ReverseMerchantFeeCalculationTx",
		merchantFeeCalculationReverseQuery,
		id,
		MerchantFeeCalculationStatusReversed,
	)
}
