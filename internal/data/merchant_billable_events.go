// Package data provides models and database access methods for merchant
// billable events.
//
// sdworkspace/sdbackend/internal/data/merchant_billable_events.go
//
// GTM:
//
//	Layer: 2.4.b Commerce Architecture / Monetization Layer
//	Release Class: SPINE
//	Reason:
//	  merchant_billable_events is the canonical billable-occurrence record for
//	  the Commerce Architecture. It preserves durable, source-linked commercial
//	  occurrences that may participate in later fee calculation.
//
//	  Billable occurrences currently originate from:
//
//	    - Future Offering submission-for-activation lifecycle events;
//	    - Future Offering billing periods; and
//	    - positive consumer Future Offering engagement events.
//
//	  This table sits downstream of authoritative source facts and upstream of
//	  fee calculations, platform-credit applications, invoices, and payment
//	  collection.
//
// Domain Boundary:
//
//	A merchant billable event is not:
//
//	  - an invoice;
//	  - a fee calculation;
//	  - an invoice line;
//	  - a payment;
//	  - a payment attempt;
//	  - a payment-provider transaction;
//	  - a billing account;
//	  - a platform-credit application;
//	  - a merchant-held balance;
//	  - a billing-policy decision;
//	  - authorization to charge a merchant;
//	  - proof that a merchant owes a particular amount; or
//	  - a mechanism for charging consumers for their actions.
//
//	The merchant pays for Sagrenti's platform-delivered value, including Market
//	Anticipation Intelligence. Consumer watches, waitlist joins, early-access
//	requests, beta participation, reservation interest, and preorder intent are
//	measurable source facts through which that value may be delivered. This
//	model does not treat consumer actions themselves as products sold to the
//	merchant.
//
//	This file does not decide:
//
//	  - whether a fee category is enabled;
//	  - which source events Administration designates as commercially billable;
//	  - the price of an occurrence;
//	  - which fee schedule applies;
//	  - whether a merchant receives a promotion, adjustment, or credit;
//	  - whether an invoice should be issued;
//	  - which payment method should be used;
//	  - whether collection should be attempted; or
//	  - whether an actor is authorized to initiate a workflow.
//
//	Those responsibilities belong to Administration-governed configuration,
//	service orchestration, authorization, fee schedules, fee calculation,
//	invoicing, Commerce Architecture, and Merchant Payments Architecture.
//
// Source and Idempotency Boundary:
//
//	Every row belongs to exactly one Future Offering and references exactly one
//	authoritative source. future_offering_id is a derived Commerce-owned identity
//	snapshot populated by the database from that source; callers do not choose it.
//	This lets downstream Commerce consumers preserve FO isolation without reaching
//	through producer-owned source schemas.
//
//	Every row references exactly one authoritative source:
//
//	  - future_offering_event_id for activation;
//	  - billing_period_id for a recurring billing period; or
//	  - engagement_event_id for a consumer-engagement occurrence.
//
//	The partial unique indexes on those source columns form the durable
//	idempotency boundary. One authoritative source occurrence may produce at
//	most one merchant billable event. This model never performs a
//	SELECT-before-INSERT uniqueness check because that pattern is race-prone
//	under concurrent processing.
//
//	The database can verify source category, but an ordinary CHECK constraint
//	cannot inspect another table's event vocabulary or ownership. The service
//	transaction must therefore enforce the following source-semantic mappings:
//
//	  - activation references a merchant_future_offerings_events row whose
//	    event_type is submitted_for_activation;
//	  - platform_service_fee references the authoritative billing period,
//	    with occurred_at copied from period_start;
//	  - watch references an engagement event whose event_type is watched;
//	  - waitlist references an engagement event whose event_type is waitlisted;
//	  - early_access_request references an engagement event whose event_type is
//	    early_access_requested;
//	  - beta references an engagement event whose event_type is beta_joined;
//	  - reservation_interest references an engagement event whose event_type is
//	    reservation_interest_recorded;
//	  - preorder_intent references an engagement event whose event_type is
//	    preorder_intent_recorded;
//	  - engagement-derived occurred_at is copied from the source engagement
//	    event's created_at;
//	  - activation occurred_at is copied from the source Future Offering
//	    event's created_at;
//	  - the source belongs to the supplied merchant; and
//	  - the billing period belongs to a Future Offering owned by the supplied merchant.
//
//	activation_payment_satisfied and activated are downstream Future Offering
//	lifecycle facts. Neither is the source of the activation billable
//	occurrence: the commercial occurrence arises when the Future Offering is
//	submitted for activation, before payment satisfaction and activation.
//
//	Muted, unmuted, unwatched, notification-change, engagement-removal, and
//	expiration events are not positive billable-engagement sources. Any later
//	reversal or correction of a previously recognized occurrence must use the
//	explicit billable-event lifecycle rather than manufacture another positive
//	billable occurrence.
//
//	These are engineering-owned source-integrity rules implemented through
//	service orchestration, not configurable commercial policy.
//
// Monetary Boundary:
//
//	gross_event_value is optional occurrence metadata or a possible monetary
//	basis for later fee calculation. It is not an invoice amount and is not
//	proof of an amount owed.
//
//	gross_event_value and currency must either both be present or both be nil.
//	Zero is a valid gross_event_value. Currency is structurally represented as
//	an uppercase three-letter identifier; this model contains no catalog of
//	commercially enabled currencies.
//
// Lifecycle:
//
//	pending   -> confirmed
//	pending   -> rejected
//	confirmed -> reversed
//
//	rejected and reversed are terminal.
//
//	Reversal preserves confirmed_at and adds reversed_at. Historical source
//	identity, occurrence time, event type, merchant ownership, gross value, and
//	currency are immutable after insertion.
//
// Transaction Boundary:
//
//	InsertTx exists for workflows that must atomically persist a source
//	occurrence and its billable-event representation, or otherwise compose
//	billable-event creation with related Commerce mutations.
//
//	Insert exists for legitimate after-the-fact reconciliation against an
//	already committed authoritative source row.
//
//	ConfirmTx, RejectTx, and ReverseTx allow lifecycle changes to participate in
//	larger service-owned transactions. Transaction-aware methods never begin,
//	commit, or roll back the caller's transaction.
//
//	This model does not imply that every source event must produce a billable
//	event. The caller determines whether to invoke insertion after resolving
//	Administration-governed commercial configuration.
//
// SPINE Rule:
//
//	Keep compiling.
//	Keep production-ready.
//	Preserve exactly one authoritative source per billable event.
//	Preserve one billable event per source occurrence.
//	Preserve transaction-compatible insertion.
//	Preserve controlled event-type and status vocabularies.
//	Preserve exact decimal-string handling.
//	Preserve currency/value pairing.
//	Preserve guarded lifecycle transitions.
//	Preserve confirmed_at through reversal.
//	Preserve immutable source and occurrence history.
//	Preserve deterministic bounded merchant history reads.
//	Never expose generic update, upsert, delete, soft-delete, or restore methods.
//	Never implement commercial policy, pricing, invoicing, payment, or
//	authorization in this file.
//	Block deployment if this file breaks build, source idempotency, occurrence
//	integrity, monetary structure, lifecycle integrity, concurrency safety, or
//	billing reconciliation readiness.
package data

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/PiccoloMondoC/sdworkspace/sdbackend/internal/logging"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// -----------------------------------------------------------------------------
// Structural bounds and canonical projections
// -----------------------------------------------------------------------------

const merchantBillableEventMaxListLimit = 100

var (
	merchantBillableEventCurrencyPattern = regexp.MustCompile(`^[A-Z]{3}$`)

	// NUMERIC(19,4) permits at most 15 integer digits and 4 fractional digits.
	// Zero is valid for gross_event_value.
	merchantBillableEventGrossValuePattern = regexp.MustCompile(
		`^[0-9]{1,15}(?:\.[0-9]{1,4})?$`,
	)
)

const merchantBillableEventSelectColumns = `
	id,
	merchant_id,
	future_offering_id,
	future_offering_event_id,
	billing_period_id,
	engagement_event_id,
	billable_event_type,
	gross_event_value,
	currency,
	occurred_at,
	confirmed_at,
	rejected_at,
	reversed_at,
	status,
	created_at,
	updated_at
`

const (
	merchantBillableEventFutureOfferingEventColumn = "future_offering_event_id"
	merchantBillableEventBillingPeriodColumn       = "billing_period_id"
	merchantBillableEventEngagementEventColumn     = "engagement_event_id"
)

// -----------------------------------------------------------------------------
// Persisted constraint and index names
// -----------------------------------------------------------------------------

// These values must remain synchronized with the authoritative migration.
// Constraint-aware classification depends on stable persisted names.
const (
	merchantBillableEventMerchantFKConstraint =
		"fk_merchant_billable_events_merchant"

	merchantBillableEventFutureOfferingFKConstraint =
		"fk_merchant_billable_events_future_offering"

	merchantBillableEventFutureOfferingEventFKConstraint =
		"fk_merchant_billable_events_future_offering_event"

	merchantBillableEventBillingPeriodFKConstraint =
		"fk_merchant_billable_events_billing_period"

	merchantBillableEventEngagementEventFKConstraint =
		"fk_merchant_billable_events_engagement_event"

	merchantBillableEventFutureOfferingEventUniqueIndex =
		"uq_merchant_billable_events_future_offering_event"

	merchantBillableEventBillingPeriodUniqueIndex =
		"uq_merchant_billable_events_billing_period"

	merchantBillableEventEngagementEventUniqueIndex =
		"uq_merchant_billable_events_engagement_event"

	merchantBillableEventSourceCountCheckConstraint =
		"chk_merchant_billable_events_source_count"

	merchantBillableEventSourceTypeCheckConstraint =
		"chk_merchant_billable_events_source_type"

	merchantBillableEventValueCurrencyConstraint =
		"chk_merchant_billable_events_value_currency"

	merchantBillableEventStatusTimestampConstraint =
		"chk_merchant_billable_events_status_timestamps"

	merchantBillableEventSourceIdentityCheckConstraint =
		"chk_merchant_billable_events_source_identity"
)

// -----------------------------------------------------------------------------
// Billable-event type vocabulary
// -----------------------------------------------------------------------------

// MerchantBillableEventType is the persisted controlled vocabulary describing
// the kind of billable occurrence represented by a merchant billable event.
type MerchantBillableEventType string

const (
	// MerchantBillableEventTypeActivation represents activation of Market
	// Anticipation Intelligence services for a Future Offering.
	MerchantBillableEventTypeActivation MerchantBillableEventType = "activation"

	// MerchantBillableEventTypePlatformServiceFee represents the recurring
	// Platform Service Fee occurrence for one authoritative billing period.
	MerchantBillableEventTypePlatformServiceFee MerchantBillableEventType = "platform_service_fee"

	// MerchantBillableEventTypeWatch represents an authoritative consumer watch
	// occurrence for a Future Offering.
	MerchantBillableEventTypeWatch MerchantBillableEventType = "watch"

	// MerchantBillableEventTypeWaitlist represents an authoritative consumer
	// waitlist occurrence for a Future Offering.
	MerchantBillableEventTypeWaitlist MerchantBillableEventType = "waitlist"

	// MerchantBillableEventTypeEarlyAccessRequest represents an authoritative
	// consumer early-access request occurrence.
	MerchantBillableEventTypeEarlyAccessRequest MerchantBillableEventType = "early_access_request"

	// MerchantBillableEventTypeBeta represents an authoritative consumer beta
	// participation occurrence.
	MerchantBillableEventTypeBeta MerchantBillableEventType = "beta"

	// MerchantBillableEventTypeReservationInterest represents an authoritative
	// consumer reservation-interest occurrence.
	MerchantBillableEventTypeReservationInterest MerchantBillableEventType = "reservation_interest"

	// MerchantBillableEventTypePreorderIntent represents an authoritative
	// consumer preorder-intent occurrence.
	MerchantBillableEventTypePreorderIntent MerchantBillableEventType = "preorder_intent"
)

// NormalizeMerchantBillableEventType returns the canonical identifier form of
// eventType.
func NormalizeMerchantBillableEventType(
	eventType MerchantBillableEventType,
) MerchantBillableEventType {
	return MerchantBillableEventType(
		normalizeIdentifier(string(eventType)),
	)
}

// IsValidMerchantBillableEventType reports whether eventType belongs to the
// merchant_billable_events billable_event_type vocabulary.
func IsValidMerchantBillableEventType(
	eventType MerchantBillableEventType,
) bool {
	switch NormalizeMerchantBillableEventType(eventType) {
	case MerchantBillableEventTypeActivation,
		MerchantBillableEventTypePlatformServiceFee,
		MerchantBillableEventTypeWatch,
		MerchantBillableEventTypeWaitlist,
		MerchantBillableEventTypeEarlyAccessRequest,
		MerchantBillableEventTypeBeta,
		MerchantBillableEventTypeReservationInterest,
		MerchantBillableEventTypePreorderIntent:
		return true
	default:
		return false
	}
}

// -----------------------------------------------------------------------------
// Billable-event status vocabulary
// -----------------------------------------------------------------------------

// MerchantBillableEventStatus is the persisted lifecycle state of a merchant
// billable event.
type MerchantBillableEventStatus string

const (
	// MerchantBillableEventStatusPending is the initial state of a newly
	// recorded billable occurrence.
	MerchantBillableEventStatusPending MerchantBillableEventStatus = "pending"

	// MerchantBillableEventStatusConfirmed indicates that the occurrence has
	// been confirmed and may participate in downstream fee calculation.
	MerchantBillableEventStatusConfirmed MerchantBillableEventStatus = "confirmed"

	// MerchantBillableEventStatusRejected is a terminal state indicating that
	// the pending occurrence was rejected.
	MerchantBillableEventStatusRejected MerchantBillableEventStatus = "rejected"

	// MerchantBillableEventStatusReversed is a terminal state indicating that
	// a previously confirmed occurrence was reversed.
	MerchantBillableEventStatusReversed MerchantBillableEventStatus = "reversed"
)

// NormalizeMerchantBillableEventStatus returns the canonical identifier form
// of status.
func NormalizeMerchantBillableEventStatus(
	status MerchantBillableEventStatus,
) MerchantBillableEventStatus {
	return MerchantBillableEventStatus(
		normalizeIdentifier(string(status)),
	)
}

// IsValidMerchantBillableEventStatus reports whether status belongs to the
// merchant_billable_events status vocabulary.
func IsValidMerchantBillableEventStatus(
	status MerchantBillableEventStatus,
) bool {
	switch NormalizeMerchantBillableEventStatus(status) {
	case MerchantBillableEventStatusPending,
		MerchantBillableEventStatusConfirmed,
		MerchantBillableEventStatusRejected,
		MerchantBillableEventStatusReversed:
		return true
	default:
		return false
	}
}

// -----------------------------------------------------------------------------
// Source classification
// -----------------------------------------------------------------------------

type merchantBillableEventSourceKind string

const (
	merchantBillableEventSourceFutureOffering merchantBillableEventSourceKind = "future_offering_event"
	merchantBillableEventSourceBillingPeriod  merchantBillableEventSourceKind = "billing_period"
	merchantBillableEventSourceEngagement     merchantBillableEventSourceKind = "engagement_event"
)

func merchantBillableEventSourceKindForType(
	eventType MerchantBillableEventType,
) (merchantBillableEventSourceKind, error) {
	switch NormalizeMerchantBillableEventType(eventType) {
	case MerchantBillableEventTypeActivation:
		return merchantBillableEventSourceFutureOffering, nil

	case MerchantBillableEventTypePlatformServiceFee:
		return merchantBillableEventSourceBillingPeriod, nil

	case MerchantBillableEventTypeWatch,
		MerchantBillableEventTypeWaitlist,
		MerchantBillableEventTypeEarlyAccessRequest,
		MerchantBillableEventTypeBeta,
		MerchantBillableEventTypeReservationInterest,
		MerchantBillableEventTypePreorderIntent:
		return merchantBillableEventSourceEngagement, nil

	default:
		return "", merchantBillableEventInvalidInput(
			"invalid billable_event_type: %q",
			eventType,
		)
	}
}

// -----------------------------------------------------------------------------
// Entity and model
// -----------------------------------------------------------------------------

// MerchantBillableEvent represents one source-linked occurrence preserved in
// the canonical Commerce billable-occurrence record.
//
// Exactly one of FutureOfferingEventID, BillingPeriodID, and
// EngagementEventID must be non-nil.
//
// Source identity, MerchantID, BillableEventType, GrossEventValue, Currency,
// and OccurredAt are immutable after insertion through this model.
type MerchantBillableEvent struct {
	ID                    uuid.UUID                   `json:"id" db:"id"`
	MerchantID            uuid.UUID                   `json:"merchant_id" db:"merchant_id"`
	FutureOfferingID      uuid.UUID                   `json:"future_offering_id" db:"future_offering_id"`
	FutureOfferingEventID *uuid.UUID                  `json:"future_offering_event_id,omitempty" db:"future_offering_event_id"`
	BillingPeriodID       *uuid.UUID                  `json:"billing_period_id,omitempty" db:"billing_period_id"`
	EngagementEventID     *uuid.UUID                  `json:"engagement_event_id,omitempty" db:"engagement_event_id"`
	BillableEventType     MerchantBillableEventType   `json:"billable_event_type" db:"billable_event_type"`
	GrossEventValue       *string                     `json:"gross_event_value,omitempty" db:"gross_event_value"`
	Currency              *string                     `json:"currency,omitempty" db:"currency"`
	OccurredAt            time.Time                   `json:"occurred_at" db:"occurred_at"`
	ConfirmedAt           *time.Time                  `json:"confirmed_at,omitempty" db:"confirmed_at"`
	RejectedAt            *time.Time                  `json:"rejected_at,omitempty" db:"rejected_at"`
	ReversedAt            *time.Time                  `json:"reversed_at,omitempty" db:"reversed_at"`
	Status                MerchantBillableEventStatus `json:"status" db:"status"`
	CreatedAt             time.Time                   `json:"created_at" db:"created_at"`
	UpdatedAt             time.Time                   `json:"updated_at" db:"updated_at"`
}

// MerchantBillableEventModel owns persistence for merchant billable events.
type MerchantBillableEventModel struct {
	DB     *pgxpool.Pool
	Logger *logging.Logger
}

type merchantBillableEventQueryRower interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// MerchantBillableEventActivationSourceFact contains the minimum authoritative
// source data required by service orchestration to validate an activation
// billable occurrence.
//
// This is a cross-table persistence projection, not a second Future Offering
// domain model.
type MerchantBillableEventActivationSourceFact struct {
	MerchantID uuid.UUID
	EventType  string
	OccurredAt time.Time
}

// MerchantBillableEventBillingPeriodSourceFact contains the minimum
// authoritative source data required by service orchestration to validate a
// billing-period billable occurrence.
type MerchantBillableEventBillingPeriodSourceFact struct {
	MerchantID uuid.UUID
	OccurredAt time.Time
}

// MerchantBillableEventEngagementSourceFact contains the minimum authoritative
// source data required by service orchestration to validate a positive consumer
// engagement billable occurrence.
//
// It deliberately contains no consumer identifier or engagement contents.
type MerchantBillableEventEngagementSourceFact struct {
	MerchantID uuid.UUID
	EventType  string
	OccurredAt time.Time
}

func (m *MerchantBillableEventModel) validateBase() error {
	if m == nil {
		return errors.New("merchant billable event model is required")
	}

	if m.Logger == nil {
		return errors.New("merchant billable event model logger is required")
	}

	return nil
}

func (m *MerchantBillableEventModel) validatePool() error {
	if err := m.validateBase(); err != nil {
		return err
	}

	if m.DB == nil {
		return errors.New(
			"merchant billable event model database pool is required",
		)
	}

	return nil
}

func scanMerchantBillableEvent(
	row scannableRow,
	event *MerchantBillableEvent,
) error {
	return row.Scan(
		&event.ID,
		&event.MerchantID,
		&event.FutureOfferingID,
		&event.FutureOfferingEventID,
		&event.BillingPeriodID,
		&event.EngagementEventID,
		&event.BillableEventType,
		&event.GrossEventValue,
		&event.Currency,
		&event.OccurredAt,
		&event.ConfirmedAt,
		&event.RejectedAt,
		&event.ReversedAt,
		&event.Status,
		&event.CreatedAt,
		&event.UpdatedAt,
	)
}

// -----------------------------------------------------------------------------
// Stable input and persistence errors
// -----------------------------------------------------------------------------

func merchantBillableEventInvalidInput(
	format string,
	args ...interface{},
) error {
	return fmt.Errorf(
		"%w: %s",
		ErrMerchantBillableEventInvalidInput,
		fmt.Sprintf(format, args...),
	)
}

func classifyMerchantBillableEventWriteError(err error) error {
	switch {
	case IsPgConstraint(
		err,
		merchantBillableEventFutureOfferingEventUniqueIndex,
	),
		IsPgConstraint(
			err,
			merchantBillableEventBillingPeriodUniqueIndex,
		),
		IsPgConstraint(
			err,
			merchantBillableEventEngagementEventUniqueIndex,
		):
		return ErrMerchantBillableEventDuplicateSource

	case IsPgConstraint(
		err,
		merchantBillableEventMerchantFKConstraint,
	):
		return ErrMerchantBillableEventMerchantNotFound

	case IsPgConstraint(
		err,
		merchantBillableEventFutureOfferingEventFKConstraint,
	):
		return ErrMerchantBillableEventFutureOfferingEventNotFound

	case IsPgConstraint(
		err,
		merchantBillableEventBillingPeriodFKConstraint,
	):
		return ErrMerchantBillableEventBillingPeriodNotFound

	case IsPgConstraint(
		err,
		merchantBillableEventEngagementEventFKConstraint,
	):
		return ErrMerchantBillableEventEngagementEventNotFound

	case IsPgConstraint(
		err,
		merchantBillableEventSourceCountCheckConstraint,
	),
		IsPgConstraint(
			err,
			merchantBillableEventSourceTypeCheckConstraint,
		),
		IsPgConstraint(
			err,
			merchantBillableEventValueCurrencyConstraint,
		),
		IsPgConstraint(
			err,
			merchantBillableEventStatusTimestampConstraint,
		),
		IsPgConstraint(
			err,
			merchantBillableEventSourceIdentityCheckConstraint,
		):
		return ErrMerchantBillableEventInvalidState

	case IsUniqueViolation(err):
		return ErrMerchantBillableEventDuplicateSource

	case IsForeignKeyViolation(err),
		IsCheckViolation(err),
		IsNotNullViolation(err):
		return ErrMerchantBillableEventInvalidState

	default:
		return err
	}
}

// -----------------------------------------------------------------------------
// Normalization and validation
// -----------------------------------------------------------------------------

func normalizeMerchantBillableEventGrossValue(
	value *string,
) *string {
	if value == nil {
		return nil
	}

	normalized := strings.TrimSpace(*value)
	if normalized == "" {
		return nil
	}

	return &normalized
}

func normalizeMerchantBillableEventCurrency(
	currency *string,
) *string {
	if currency == nil {
		return nil
	}

	normalized := strings.ToUpper(strings.TrimSpace(*currency))
	if normalized == "" {
		return nil
	}

	return &normalized
}

func validateMerchantBillableEventGrossValue(
	value string,
) error {
	value = strings.TrimSpace(value)

	if value == "" {
		return merchantBillableEventInvalidInput(
			"gross_event_value is required when currency is supplied",
		)
	}

	if !merchantBillableEventGrossValuePattern.MatchString(value) {
		return merchantBillableEventInvalidInput(
			"gross_event_value must be a non-negative NUMERIC(19,4)-compatible decimal",
		)
	}

	if err := validateNonNegativeDecimalString(
		value,
		"gross_event_value",
	); err != nil {
		return merchantBillableEventInvalidInput(
			"gross_event_value is invalid: %v",
			err,
		)
	}

	return nil
}

func validateMerchantBillableEventCurrency(
	currency string,
) error {
	if !merchantBillableEventCurrencyPattern.MatchString(currency) {
		return merchantBillableEventInvalidInput(
			"currency must be a three-letter uppercase identifier: %q",
			currency,
		)
	}

	return nil
}

func validateMerchantBillableEventSourceReferences(
	eventType MerchantBillableEventType,
	futureOfferingEventID *uuid.UUID,
	billingPeriodID *uuid.UUID,
	engagementEventID *uuid.UUID,
) error {
	sourceKind, err := merchantBillableEventSourceKindForType(eventType)
	if err != nil {
		return err
	}

	sourceCount := 0

	if futureOfferingEventID != nil {
		if *futureOfferingEventID == uuid.Nil {
			return merchantBillableEventInvalidInput(
				"future_offering_event_id must not be a nil UUID",
			)
		}
		sourceCount++
	}

	if billingPeriodID != nil {
		if *billingPeriodID == uuid.Nil {
			return merchantBillableEventInvalidInput(
				"billing_period_id must not be a nil UUID",
			)
		}
		sourceCount++
	}

	if engagementEventID != nil {
		if *engagementEventID == uuid.Nil {
			return merchantBillableEventInvalidInput(
				"engagement_event_id must not be a nil UUID",
			)
		}
		sourceCount++
	}

	if sourceCount != 1 {
		return merchantBillableEventInvalidInput(
			"exactly one authoritative source event ID is required",
		)
	}

	switch sourceKind {
	case merchantBillableEventSourceFutureOffering:
		if futureOfferingEventID == nil {
			return merchantBillableEventInvalidInput(
				"billable_event_type %q requires future_offering_event_id",
				eventType,
			)
		}

	case merchantBillableEventSourceBillingPeriod:
		if billingPeriodID == nil {
			return merchantBillableEventInvalidInput(
				"billable_event_type %q requires billing_period_id",
				eventType,
			)
		}

	case merchantBillableEventSourceEngagement:
		if engagementEventID == nil {
			return merchantBillableEventInvalidInput(
				"billable_event_type %q requires engagement_event_id",
				eventType,
			)
		}

	default:
		return merchantBillableEventInvalidInput(
			"unsupported source category for billable_event_type %q",
			eventType,
		)
	}

	return nil
}

func validateMerchantBillableEventForInsert(
	event *MerchantBillableEvent,
) error {
	if event == nil {
		return merchantBillableEventInvalidInput(
			"event is required",
		)
	}

	if event.MerchantID == uuid.Nil {
		return merchantBillableEventInvalidInput(
			"merchant_id is required",
		)
	}

	if event.FutureOfferingID != uuid.Nil {
		return merchantBillableEventInvalidInput(
			"future_offering_id is database-derived from the authoritative source",
		)
	}

	event.BillableEventType = NormalizeMerchantBillableEventType(
		event.BillableEventType,
	)
	if !IsValidMerchantBillableEventType(event.BillableEventType) {
		return merchantBillableEventInvalidInput(
			"invalid billable_event_type: %q",
			event.BillableEventType,
		)
	}

	if err := validateMerchantBillableEventSourceReferences(
		event.BillableEventType,
		event.FutureOfferingEventID,
		event.BillingPeriodID,
		event.EngagementEventID,
	); err != nil {
		return err
	}

	if event.OccurredAt.IsZero() {
		return merchantBillableEventInvalidInput(
			"occurred_at is required",
		)
	}

	event.OccurredAt = event.OccurredAt.UTC()

	event.GrossEventValue = normalizeMerchantBillableEventGrossValue(
		event.GrossEventValue,
	)
	event.Currency = normalizeMerchantBillableEventCurrency(
		event.Currency,
	)

	if (event.GrossEventValue == nil) != (event.Currency == nil) {
		return merchantBillableEventInvalidInput(
			"gross_event_value and currency must be supplied together or both omitted",
		)
	}

	if event.GrossEventValue != nil {
		if err := validateMerchantBillableEventGrossValue(
			*event.GrossEventValue,
		); err != nil {
			return err
		}

		if err := validateMerchantBillableEventCurrency(
			*event.Currency,
		); err != nil {
			return err
		}
	}

	if event.Status != "" &&
		NormalizeMerchantBillableEventStatus(event.Status) !=
			MerchantBillableEventStatusPending {
		return merchantBillableEventInvalidInput(
			"status is database-owned at creation",
		)
	}

	if event.ConfirmedAt != nil ||
		event.RejectedAt != nil ||
		event.ReversedAt != nil {
		return merchantBillableEventInvalidInput(
			"lifecycle timestamps must not be supplied at creation",
		)
	}

	return nil
}

func validateMerchantBillableEventPersistedState(
	event *MerchantBillableEvent,
) error {
	if event == nil ||
		event.ID == uuid.Nil ||
		event.MerchantID == uuid.Nil ||
		event.FutureOfferingID == uuid.Nil ||
		event.OccurredAt.IsZero() ||
		event.CreatedAt.IsZero() ||
		event.UpdatedAt.IsZero() {
		return ErrMerchantBillableEventInvalidState
	}

	eventType := NormalizeMerchantBillableEventType(
		event.BillableEventType,
	)
	if !IsValidMerchantBillableEventType(eventType) {
		return ErrMerchantBillableEventInvalidState
	}

	if err := validateMerchantBillableEventSourceReferences(
		eventType,
		event.FutureOfferingEventID,
		event.BillingPeriodID,
		event.EngagementEventID,
	); err != nil {
		return ErrMerchantBillableEventInvalidState
	}

	if (event.GrossEventValue == nil) != (event.Currency == nil) {
		return ErrMerchantBillableEventInvalidState
	}

	if event.GrossEventValue != nil {
		if err := validateMerchantBillableEventGrossValue(
			*event.GrossEventValue,
		); err != nil {
			return ErrMerchantBillableEventInvalidState
		}

		if event.Currency == nil ||
			!merchantBillableEventCurrencyPattern.MatchString(
				*event.Currency,
			) {
			return ErrMerchantBillableEventInvalidState
		}
	}

	status := NormalizeMerchantBillableEventStatus(event.Status)
	if !IsValidMerchantBillableEventStatus(status) {
		return ErrMerchantBillableEventInvalidState
	}

	switch status {
	case MerchantBillableEventStatusPending:
		if event.ConfirmedAt != nil ||
			event.RejectedAt != nil ||
			event.ReversedAt != nil {
			return ErrMerchantBillableEventInvalidState
		}

	case MerchantBillableEventStatusConfirmed:
		if event.ConfirmedAt == nil ||
			event.RejectedAt != nil ||
			event.ReversedAt != nil {
			return ErrMerchantBillableEventInvalidState
		}

	case MerchantBillableEventStatusRejected:
		if event.ConfirmedAt != nil ||
			event.RejectedAt == nil ||
			event.ReversedAt != nil {
			return ErrMerchantBillableEventInvalidState
		}

	case MerchantBillableEventStatusReversed:
		if event.ConfirmedAt == nil ||
			event.RejectedAt != nil ||
			event.ReversedAt == nil {
			return ErrMerchantBillableEventInvalidState
		}
	}

	return nil
}

func validateMerchantBillableEventID(
	id uuid.UUID,
) error {
	if id == uuid.Nil {
		return merchantBillableEventInvalidInput(
			"id is required",
		)
	}

	return nil
}

func validateMerchantBillableEventMerchantID(
	merchantID uuid.UUID,
) error {
	if merchantID == uuid.Nil {
		return merchantBillableEventInvalidInput(
			"merchant_id is required",
		)
	}

	return nil
}

func validateMerchantBillableEventSourceID(
	sourceID uuid.UUID,
	fieldName string,
) error {
	if sourceID == uuid.Nil {
		return merchantBillableEventInvalidInput(
			"%s is required",
			fieldName,
		)
	}

	return nil
}

func validateMerchantBillableEventListLimit(
	limit int,
) error {
	if limit <= 0 || limit > merchantBillableEventMaxListLimit {
		return merchantBillableEventInvalidInput(
			"limit must be between 1 and %d",
			merchantBillableEventMaxListLimit,
		)
	}

	return nil
}

func validateMerchantBillableEventBeforeCursor(
	beforeOccurredAt *time.Time,
	beforeID *uuid.UUID,
) error {
	if (beforeOccurredAt == nil) != (beforeID == nil) {
		return merchantBillableEventInvalidInput(
			"before_occurred_at and before_id must be supplied together",
		)
	}

	if beforeOccurredAt == nil {
		return nil
	}

	if beforeOccurredAt.IsZero() {
		return merchantBillableEventInvalidInput(
			"before_occurred_at must not be zero",
		)
	}

	if *beforeID == uuid.Nil {
		return merchantBillableEventInvalidInput(
			"before_id must not be a nil UUID",
		)
	}

	return nil
}

// -----------------------------------------------------------------------------
// Authoritative source-fact resolution
// -----------------------------------------------------------------------------

func (m *MerchantBillableEventModel) getActivationSourceFactViaQuerier(
	ctx context.Context,
	querier merchantBillableEventQueryRower,
	futureOfferingEventID uuid.UUID,
) (*MerchantBillableEventActivationSourceFact, error) {
	const query = `
		SELECT
			mfo.merchant_id,
			mfoe.event_type,
			mfoe.created_at
		FROM merchant_future_offerings_events AS mfoe
		JOIN merchant_future_offerings AS mfo
		  ON mfo.id = mfoe.future_offering_id
		WHERE mfoe.id = $1
	`

	var fact MerchantBillableEventActivationSourceFact

	err := querier.QueryRow(
		ctx,
		query,
		futureOfferingEventID,
	).Scan(
		&fact.MerchantID,
		&fact.EventType,
		&fact.OccurredAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}

	return &fact, nil
}

// GetActivationSourceFact retrieves the authoritative source facts needed to
// validate one activation billable occurrence.
//
// Absence returns nil, nil.
func (m *MerchantBillableEventModel) GetActivationSourceFact(
	ctx context.Context,
	futureOfferingEventID uuid.UUID,
) (*MerchantBillableEventActivationSourceFact, error) {
	if err := m.validatePool(); err != nil {
		return nil, err
	}

	if err := validateMerchantBillableEventSourceID(
		futureOfferingEventID,
		"future_offering_event_id",
	); err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	fact, err := m.getActivationSourceFactViaQuerier(
		ctx,
		m.DB,
		futureOfferingEventID,
	)
	if err != nil {
		return nil, fmt.Errorf(
			"get merchant billable activation source fact: %w",
			err,
		)
	}

	return fact, nil
}

// GetActivationSourceFactTx is the transaction-aware form of
// GetActivationSourceFact.
//
// It performs the authoritative source read exclusively through tx and does
// not begin, commit, or roll back the caller-owned transaction.
//
// The caller owns the transaction and overall workflow context. The data layer
// retains its normal per-statement dbTimeout so an individual database
// operation cannot remain blocked indefinitely.
func (m *MerchantBillableEventModel) GetActivationSourceFactTx(
	ctx context.Context,
	tx pgx.Tx,
	futureOfferingEventID uuid.UUID,
) (*MerchantBillableEventActivationSourceFact, error) {
	if err := m.validateBase(); err != nil {
		return nil, err
	}

	if tx == nil {
		return nil, merchantBillableEventInvalidInput(
			"transaction is required",
		)
	}

	if err := validateMerchantBillableEventSourceID(
		futureOfferingEventID,
		"future_offering_event_id",
	); err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	fact, err := m.getActivationSourceFactViaQuerier(
		ctx,
		tx,
		futureOfferingEventID,
	)
	if err != nil {
		return nil, fmt.Errorf(
			"get merchant billable activation source fact in transaction: %w",
			err,
		)
	}

	return fact, nil
}

func (m *MerchantBillableEventModel) getBillingPeriodSourceFactViaQuerier(
	ctx context.Context,
	querier merchantBillableEventQueryRower,
	billingPeriodID uuid.UUID,
) (*MerchantBillableEventBillingPeriodSourceFact, error) {
	const query = `
		SELECT
			mfo.merchant_id,
			(mfobp.period_starts_on::timestamp AT TIME ZONE 'UTC')
		FROM merchant_future_offering_billing_periods AS mfobp
		JOIN merchant_future_offerings AS mfo
		  ON mfo.id = mfobp.future_offering_id
		WHERE mfobp.id = $1
	`

	var fact MerchantBillableEventBillingPeriodSourceFact

	err := querier.QueryRow(
		ctx,
		query,
		billingPeriodID,
	).Scan(
		&fact.MerchantID,
		&fact.OccurredAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}

	return &fact, nil
}

// GetBillingPeriodSourceFact retrieves the authoritative source facts needed to
// validate one Platform Service Fee billable occurrence.
//
// Ownership is resolved through billing_period -> Future Offering -> merchant.
// Absence returns nil, nil.
func (m *MerchantBillableEventModel) GetBillingPeriodSourceFact(
	ctx context.Context,
	billingPeriodID uuid.UUID,
) (*MerchantBillableEventBillingPeriodSourceFact, error) {
	if err := m.validatePool(); err != nil {
		return nil, err
	}

	if err := validateMerchantBillableEventSourceID(
		billingPeriodID,
		"billing_period_id",
	); err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	fact, err := m.getBillingPeriodSourceFactViaQuerier(
		ctx,
		m.DB,
		billingPeriodID,
	)
	if err != nil {
		return nil, fmt.Errorf(
			"get merchant billable billing-period source fact: %w",
			err,
		)
	}

	return fact, nil
}

// GetBillingPeriodSourceFactTx is the transaction-aware form of
// GetBillingPeriodSourceFact.
//
// The caller owns the transaction and overall workflow context. The data layer
// retains its normal per-statement dbTimeout.
func (m *MerchantBillableEventModel) GetBillingPeriodSourceFactTx(
	ctx context.Context,
	tx pgx.Tx,
	billingPeriodID uuid.UUID,
) (*MerchantBillableEventBillingPeriodSourceFact, error) {
	if err := m.validateBase(); err != nil {
		return nil, err
	}

	if tx == nil {
		return nil, merchantBillableEventInvalidInput(
			"transaction is required",
		)
	}

	if err := validateMerchantBillableEventSourceID(
		billingPeriodID,
		"billing_period_id",
	); err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	fact, err := m.getBillingPeriodSourceFactViaQuerier(
		ctx,
		tx,
		billingPeriodID,
	)
	if err != nil {
		return nil, fmt.Errorf(
			"get merchant billable billing-period source fact in transaction: %w",
			err,
		)
	}

	return fact, nil
}

func (m *MerchantBillableEventModel) getEngagementSourceFactViaQuerier(
	ctx context.Context,
	querier merchantBillableEventQueryRower,
	engagementEventID uuid.UUID,
) (*MerchantBillableEventEngagementSourceFact, error) {
	const query = `
		SELECT
			mfo.merchant_id,
			utee.event_type,
			utee.created_at
		FROM user_trend_engagement_events AS utee
		JOIN user_trend_engagements AS ute
		  ON ute.id = utee.engagement_id
		JOIN merchant_future_offerings AS mfo
		  ON mfo.offer_id = ute.offer_id
		WHERE utee.id = $1
	`

	var fact MerchantBillableEventEngagementSourceFact

	err := querier.QueryRow(
		ctx,
		query,
		engagementEventID,
	).Scan(
		&fact.MerchantID,
		&fact.EventType,
		&fact.OccurredAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}

	return &fact, nil
}

// GetEngagementSourceFact retrieves the authoritative source facts required to
// validate one consumer-engagement billable occurrence.
//
// The projection deliberately traverses engagement_event -> engagement ->
// Future Offering and returns no consumer identifier.
//
// Absence returns nil, nil.
func (m *MerchantBillableEventModel) GetEngagementSourceFact(
	ctx context.Context,
	engagementEventID uuid.UUID,
) (*MerchantBillableEventEngagementSourceFact, error) {
	if err := m.validatePool(); err != nil {
		return nil, err
	}

	if err := validateMerchantBillableEventSourceID(
		engagementEventID,
		"engagement_event_id",
	); err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	fact, err := m.getEngagementSourceFactViaQuerier(
		ctx,
		m.DB,
		engagementEventID,
	)
	if err != nil {
		return nil, fmt.Errorf(
			"get merchant billable engagement source fact: %w",
			err,
		)
	}

	return fact, nil
}

// GetEngagementSourceFactTx is the transaction-aware form of
// GetEngagementSourceFact.
//
// The authoritative source read executes exclusively through tx. The caller
// owns the transaction and overall workflow context while the data layer
// retains its normal per-statement dbTimeout.
func (m *MerchantBillableEventModel) GetEngagementSourceFactTx(
	ctx context.Context,
	tx pgx.Tx,
	engagementEventID uuid.UUID,
) (*MerchantBillableEventEngagementSourceFact, error) {
	if err := m.validateBase(); err != nil {
		return nil, err
	}

	if tx == nil {
		return nil, merchantBillableEventInvalidInput(
			"transaction is required",
		)
	}

	if err := validateMerchantBillableEventSourceID(
		engagementEventID,
		"engagement_event_id",
	); err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	fact, err := m.getEngagementSourceFactViaQuerier(
		ctx,
		tx,
		engagementEventID,
	)
	if err != nil {
		return nil, fmt.Errorf(
			"get merchant billable engagement source fact in transaction: %w",
			err,
		)
	}

	return fact, nil
}

// -----------------------------------------------------------------------------
// Creation
// -----------------------------------------------------------------------------

func merchantBillableEventLogFields(
	event *MerchantBillableEvent,
) []any {
	if event == nil {
		return nil
	}

	fields := []any{
		"billable_event_id", event.ID,
		"merchant_id", event.MerchantID,
		"billable_event_type", event.BillableEventType,
		"status", event.Status,
	}

	if event.FutureOfferingID != uuid.Nil {
		fields = append(fields, "future_offering_id", event.FutureOfferingID)
	}

	if event.FutureOfferingEventID != nil {
		fields = append(
			fields,
			"future_offering_event_id",
			*event.FutureOfferingEventID,
		)
	}

	if event.BillingPeriodID != nil {
		fields = append(
			fields,
			"billing_period_id",
			*event.BillingPeriodID,
		)
	}

	if event.EngagementEventID != nil {
		fields = append(
			fields,
			"engagement_event_id",
			*event.EngagementEventID,
		)
	}

	return fields
}

func (m *MerchantBillableEventModel) insert(
	ctx context.Context,
	querier merchantBillableEventQueryRower,
	functionName string,
	event *MerchantBillableEvent,
) (*MerchantBillableEvent, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.
		GetLoggerWithContextFromContext(ctx).
		WithFunctionName(functionName)

	if err := validateMerchantBillableEventForInsert(event); err != nil {
		logger.Error("Validation failed", err)
		return nil, err
	}

	id := event.ID
	if id == uuid.Nil {
		id = uuid.New()
	}

	// future_offering_id is intentionally omitted. The database BEFORE INSERT
	// identity trigger derives it from the one authoritative source and rejects
	// merchant/source mismatches before the row becomes durable.
	const query = `
		INSERT INTO merchant_billable_events (
			id,
			merchant_id,
			future_offering_event_id,
			billing_period_id,
			engagement_event_id,
			billable_event_type,
			gross_event_value,
			currency,
			occurred_at
		)
		VALUES (
			$1,
			$2,
			$3,
			$4,
			$5,
			$6,
			$7::numeric,
			$8,
			$9
		)
		RETURNING ` + merchantBillableEventSelectColumns

	var result MerchantBillableEvent
	err := scanMerchantBillableEvent(
		querier.QueryRow(
			ctx,
			query,
			id,
			event.MerchantID,
			event.FutureOfferingEventID,
			event.BillingPeriodID,
			event.EngagementEventID,
			event.BillableEventType,
			event.GrossEventValue,
			event.Currency,
			event.OccurredAt,
		),
		&result,
	)
	if err != nil {
		err = classifyMerchantBillableEventWriteError(err)

		fields := append(
			[]any{err},
			merchantBillableEventLogFields(event)...,
		)

		if errors.Is(
			err,
			ErrMerchantBillableEventDuplicateSource,
		) {
			logger.Warn(
				"Merchant billable event source already recorded",
				fields...,
			)
			return nil, err
		}

		logger.Error(
			"Insert merchant billable event failed",
			fields...,
		)
		return nil, err
	}

	if err := validateMerchantBillableEventPersistedState(
		&result,
	); err != nil {
		logger.Error(
			"Insert merchant billable event returned invalid state",
			err,
			"billable_event_id", result.ID,
		)
		return nil, err
	}

	return &result, nil
}

// Insert records a merchant billable event through the model's database pool.
//
// Use Insert only when the authoritative source occurrence is already durably
// committed and no related write must commit atomically with the billable-event
// row.
//
// Workflows creating or processing the source occurrence inside a larger
// transaction must use InsertTx.
func (m *MerchantBillableEventModel) Insert(
	ctx context.Context,
	event *MerchantBillableEvent,
) (*MerchantBillableEvent, error) {
	if err := m.validatePool(); err != nil {
		return nil, err
	}

	return m.insert(
		ctx,
		m.DB,
		"InsertMerchantBillableEvent",
		event,
	)
}

// InsertTx records a merchant billable event through tx.
//
// InsertTx does not begin, commit, or roll back tx. The coordinating service
// owns the complete transaction lifecycle.
//
// The service must use InsertTx when billable-event creation must be atomic with
// source-event creation or another Commerce mutation.
func (m *MerchantBillableEventModel) InsertTx(
	ctx context.Context,
	tx pgx.Tx,
	event *MerchantBillableEvent,
) (*MerchantBillableEvent, error) {
	if err := m.validateBase(); err != nil {
		return nil, err
	}

	if tx == nil {
		return nil, merchantBillableEventInvalidInput(
			"transaction is required",
		)
	}

	return m.insert(
		ctx,
		tx,
		"InsertMerchantBillableEventTx",
		event,
	)
}

// -----------------------------------------------------------------------------
// Single-record reads
// -----------------------------------------------------------------------------

func (m *MerchantBillableEventModel) getByIDViaQuerier(
	ctx context.Context,
	querier merchantBillableEventQueryRower,
	id uuid.UUID,
) (*MerchantBillableEvent, error) {
	const query = `
		SELECT ` + merchantBillableEventSelectColumns + `
		FROM merchant_billable_events
		WHERE id = $1
	`

	var event MerchantBillableEvent
	err := scanMerchantBillableEvent(
		querier.QueryRow(ctx, query, id),
		&event,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}

		return nil, err
	}

	if err := validateMerchantBillableEventPersistedState(
		&event,
	); err != nil {
		return nil, err
	}

	return &event, nil
}

// GetByID retrieves a merchant billable event by canonical ID.
//
// Absence returns nil, nil.
func (m *MerchantBillableEventModel) GetByID(
	ctx context.Context,
	id uuid.UUID,
) (*MerchantBillableEvent, error) {
	if err := m.validatePool(); err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.
		GetLoggerWithContextFromContext(ctx).
		WithFunctionName("GetMerchantBillableEventByID")

	if err := validateMerchantBillableEventID(id); err != nil {
		logger.Error("Validation failed", err)
		return nil, err
	}

	event, err := m.getByIDViaQuerier(ctx, m.DB, id)
	if err != nil {
		logger.Error(
			"Get merchant billable event by ID failed",
			err,
			"billable_event_id", id,
		)
		return nil, err
	}

	return event, nil
}

func (m *MerchantBillableEventModel) getBySourceID(
	ctx context.Context,
	column string,
	sourceID uuid.UUID,
	sourceFieldName string,
	functionName string,
) (*MerchantBillableEvent, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.
		GetLoggerWithContextFromContext(ctx).
		WithFunctionName(functionName)

	if err := validateMerchantBillableEventSourceID(
		sourceID,
		sourceFieldName,
	); err != nil {
		logger.Error("Validation failed", err)
		return nil, err
	}

	query := fmt.Sprintf(
		`
			SELECT %s
			FROM merchant_billable_events
			WHERE %s = $1
		`,
		merchantBillableEventSelectColumns,
		column,
	)

	var event MerchantBillableEvent
	err := scanMerchantBillableEvent(
		m.DB.QueryRow(ctx, query, sourceID),
		&event,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}

		logger.Error(
			"Get merchant billable event by source ID failed",
			err,
			sourceFieldName, sourceID,
		)
		return nil, err
	}

	if err := validateMerchantBillableEventPersistedState(
		&event,
	); err != nil {
		logger.Error(
			"Merchant billable event contains invalid persisted state",
			err,
			"billable_event_id", event.ID,
		)
		return nil, err
	}

	return &event, nil
}

// GetByFutureOfferingEventID retrieves the billable event referencing
// futureOfferingEventID.
//
// Absence returns nil, nil. The source unique index remains the concurrency-safe
// idempotency guarantee.
func (m *MerchantBillableEventModel) GetByFutureOfferingEventID(
	ctx context.Context,
	futureOfferingEventID uuid.UUID,
) (*MerchantBillableEvent, error) {
	if err := m.validatePool(); err != nil {
		return nil, err
	}

	return m.getBySourceID(
		ctx,
		merchantBillableEventFutureOfferingEventColumn,
		futureOfferingEventID,
		"future_offering_event_id",
		"GetMerchantBillableEventByFutureOfferingEventID",
	)
}

// GetByBillingPeriodID retrieves the billable event referencing
// billingPeriodID.
//
// Absence returns nil, nil.
func (m *MerchantBillableEventModel) GetByBillingPeriodID(
	ctx context.Context,
	billingPeriodID uuid.UUID,
) (*MerchantBillableEvent, error) {
	if err := m.validatePool(); err != nil {
		return nil, err
	}

	return m.getBySourceID(
		ctx,
		merchantBillableEventBillingPeriodColumn,
		billingPeriodID,
		"billing_period_id",
		"GetMerchantBillableEventByBillingPeriodID",
	)
}

// GetByEngagementEventID retrieves the billable event referencing
// engagementEventID.
//
// Absence returns nil, nil.
func (m *MerchantBillableEventModel) GetByEngagementEventID(
	ctx context.Context,
	engagementEventID uuid.UUID,
) (*MerchantBillableEvent, error) {
	if err := m.validatePool(); err != nil {
		return nil, err
	}

	return m.getBySourceID(
		ctx,
		merchantBillableEventEngagementEventColumn,
		engagementEventID,
		"engagement_event_id",
		"GetMerchantBillableEventByEngagementEventID",
	)
}

// -----------------------------------------------------------------------------
// Merchant-scoped history reads
// -----------------------------------------------------------------------------

// ListByMerchant returns a bounded page of billable events for merchantID,
// ordered by occurred_at DESC, id DESC.
//
// Pagination is keyset-based. beforeOccurredAt and beforeID must either both be
// nil for the first page or both be supplied for a subsequent page.
func (m *MerchantBillableEventModel) ListByMerchant(
	ctx context.Context,
	merchantID uuid.UUID,
	limit int,
	beforeOccurredAt *time.Time,
	beforeID *uuid.UUID,
) ([]*MerchantBillableEvent, error) {
	if err := m.validatePool(); err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.
		GetLoggerWithContextFromContext(ctx).
		WithFunctionName("ListMerchantBillableEventsByMerchant")

	if err := validateMerchantBillableEventMerchantID(
		merchantID,
	); err != nil {
		logger.Error("Validation failed", err)
		return nil, err
	}

	if err := validateMerchantBillableEventListLimit(
		limit,
	); err != nil {
		logger.Error("Validation failed", err)
		return nil, err
	}

	if err := validateMerchantBillableEventBeforeCursor(
		beforeOccurredAt,
		beforeID,
	); err != nil {
		logger.Error("Validation failed", err)
		return nil, err
	}

	var (
		rows pgx.Rows
		err  error
	)

	if beforeOccurredAt == nil {
		const query = `
			SELECT ` + merchantBillableEventSelectColumns + `
			FROM merchant_billable_events
			WHERE merchant_id = $1
			ORDER BY occurred_at DESC, id DESC
			LIMIT $2
		`

		rows, err = m.DB.Query(
			ctx,
			query,
			merchantID,
			limit,
		)
	} else {
		const query = `
			SELECT ` + merchantBillableEventSelectColumns + `
			FROM merchant_billable_events
			WHERE merchant_id = $1
			  AND (occurred_at, id) < ($2, $3)
			ORDER BY occurred_at DESC, id DESC
			LIMIT $4
		`

		rows, err = m.DB.Query(
			ctx,
			query,
			merchantID,
			beforeOccurredAt.UTC(),
			*beforeID,
			limit,
		)
	}

	if err != nil {
		logger.Error(
			"List merchant billable events by merchant failed",
			err,
			"merchant_id", merchantID,
		)
		return nil, err
	}
	defer rows.Close()

	events := make([]*MerchantBillableEvent, 0, limit)

	for rows.Next() {
		var event MerchantBillableEvent

		if err := scanMerchantBillableEvent(
			rows,
			&event,
		); err != nil {
			logger.Error(
				"Scan merchant billable event failed",
				err,
				"merchant_id", merchantID,
			)
			return nil, err
		}

		if err := validateMerchantBillableEventPersistedState(
			&event,
		); err != nil {
			logger.Error(
				"Merchant billable event contains invalid persisted state",
				err,
				"billable_event_id", event.ID,
				"merchant_id", merchantID,
			)
			return nil, err
		}

		events = append(events, &event)
	}

	if err := rows.Err(); err != nil {
		logger.Error(
			"Iterate merchant billable events by merchant failed",
			err,
			"merchant_id", merchantID,
		)
		return nil, err
	}

	return events, nil
}

// ListByMerchantAndStatus returns a bounded page of billable events for
// merchantID filtered by status, ordered by occurred_at DESC, id DESC.
//
// Pagination is keyset-based. beforeOccurredAt and beforeID must either both be
// nil for the first page or both be supplied for a subsequent page.
func (m *MerchantBillableEventModel) ListByMerchantAndStatus(
	ctx context.Context,
	merchantID uuid.UUID,
	status MerchantBillableEventStatus,
	limit int,
	beforeOccurredAt *time.Time,
	beforeID *uuid.UUID,
) ([]*MerchantBillableEvent, error) {
	if err := m.validatePool(); err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.
		GetLoggerWithContextFromContext(ctx).
		WithFunctionName(
			"ListMerchantBillableEventsByMerchantAndStatus",
		)

	if err := validateMerchantBillableEventMerchantID(
		merchantID,
	); err != nil {
		logger.Error("Validation failed", err)
		return nil, err
	}

	status = NormalizeMerchantBillableEventStatus(status)
	if !IsValidMerchantBillableEventStatus(status) {
		err := merchantBillableEventInvalidInput(
			"invalid status: %q",
			status,
		)
		logger.Error("Validation failed", err)
		return nil, err
	}

	if err := validateMerchantBillableEventListLimit(
		limit,
	); err != nil {
		logger.Error("Validation failed", err)
		return nil, err
	}

	if err := validateMerchantBillableEventBeforeCursor(
		beforeOccurredAt,
		beforeID,
	); err != nil {
		logger.Error("Validation failed", err)
		return nil, err
	}

	var (
		rows pgx.Rows
		err  error
	)

	if beforeOccurredAt == nil {
		const query = `
			SELECT ` + merchantBillableEventSelectColumns + `
			FROM merchant_billable_events
			WHERE merchant_id = $1
			  AND status = $2
			ORDER BY occurred_at DESC, id DESC
			LIMIT $3
		`

		rows, err = m.DB.Query(
			ctx,
			query,
			merchantID,
			status,
			limit,
		)
	} else {
		const query = `
			SELECT ` + merchantBillableEventSelectColumns + `
			FROM merchant_billable_events
			WHERE merchant_id = $1
			  AND status = $2
			  AND (occurred_at, id) < ($3, $4)
			ORDER BY occurred_at DESC, id DESC
			LIMIT $5
		`

		rows, err = m.DB.Query(
			ctx,
			query,
			merchantID,
			status,
			beforeOccurredAt.UTC(),
			*beforeID,
			limit,
		)
	}

	if err != nil {
		logger.Error(
			"List merchant billable events by merchant and status failed",
			err,
			"merchant_id", merchantID,
			"status", status,
		)
		return nil, err
	}
	defer rows.Close()

	events := make([]*MerchantBillableEvent, 0, limit)

	for rows.Next() {
		var event MerchantBillableEvent

		if err := scanMerchantBillableEvent(
			rows,
			&event,
		); err != nil {
			logger.Error(
				"Scan merchant billable event failed",
				err,
				"merchant_id", merchantID,
				"status", status,
			)
			return nil, err
		}

		if err := validateMerchantBillableEventPersistedState(
			&event,
		); err != nil {
			logger.Error(
				"Merchant billable event contains invalid persisted state",
				err,
				"billable_event_id", event.ID,
				"merchant_id", merchantID,
				"status", status,
			)
			return nil, err
		}

		events = append(events, &event)
	}

	if err := rows.Err(); err != nil {
		logger.Error(
			"Iterate merchant billable events by merchant and status failed",
			err,
			"merchant_id", merchantID,
			"status", status,
		)
		return nil, err
	}

	return events, nil
}

// -----------------------------------------------------------------------------
// Lifecycle transitions
// -----------------------------------------------------------------------------

const merchantBillableEventConfirmQuery = `
	UPDATE merchant_billable_events
	SET
		status = 'confirmed',
		confirmed_at = NOW(),
		updated_at = NOW()
	WHERE id = $1
	  AND status = 'pending'
	RETURNING ` + merchantBillableEventSelectColumns

const merchantBillableEventRejectQuery = `
	UPDATE merchant_billable_events
	SET
		status = 'rejected',
		rejected_at = NOW(),
		updated_at = NOW()
	WHERE id = $1
	  AND status = 'pending'
	RETURNING ` + merchantBillableEventSelectColumns

const merchantBillableEventReverseQuery = `
	UPDATE merchant_billable_events
	SET
		status = 'reversed',
		reversed_at = NOW(),
		updated_at = NOW()
	WHERE id = $1
	  AND status = 'confirmed'
	RETURNING ` + merchantBillableEventSelectColumns

// resolveTransitionNoMatch classifies a guarded transition that updated no row.
//
// This domain intentionally has no separate mutation-conflict result. Each
// transition has one permitted source status, statuses move monotonically, and
// rejected and reversed are terminal. After a guarded update misses:
//
//   - absence means not found;
//   - the requested target state means the operation is already complete; and
//   - every other retained state means the requested transition is invalid.
//
// Unlike a lifecycle that permits movement back into an allowed source state,
// this lifecycle has no legitimate state round-trip from which a distinct
// optimistic mutation conflict could be established.
func (m *MerchantBillableEventModel) resolveTransitionNoMatch(
	ctx context.Context,
	querier merchantBillableEventQueryRower,
	id uuid.UUID,
	toStatus MerchantBillableEventStatus,
) (*MerchantBillableEvent, error) {
	event, err := m.getByIDViaQuerier(
		ctx,
		querier,
		id,
	)
	if err != nil {
		return nil, err
	}

	if event == nil {
		return nil, ErrMerchantBillableEventNotFound
	}

	if event.Status == toStatus {
		return event, nil
	}

	return nil, ErrMerchantBillableEventInvalidTransition
}

func (m *MerchantBillableEventModel) transition(
	ctx context.Context,
	querier merchantBillableEventQueryRower,
	functionName string,
	query string,
	id uuid.UUID,
	toStatus MerchantBillableEventStatus,
) (*MerchantBillableEvent, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.
		GetLoggerWithContextFromContext(ctx).
		WithFunctionName(functionName)

	if err := validateMerchantBillableEventID(id); err != nil {
		logger.Error("Validation failed", err)
		return nil, err
	}

	var event MerchantBillableEvent
	err := scanMerchantBillableEvent(
		querier.QueryRow(ctx, query, id),
		&event,
	)
	if err == nil {
		if err := validateMerchantBillableEventPersistedState(
			&event,
		); err != nil {
			logger.Error(
				"Merchant billable event transition returned invalid state",
				err,
				"billable_event_id", id,
				"to_status", toStatus,
			)
			return nil, err
		}

		logger.Info(
			"Merchant billable event lifecycle transition successful",
			"billable_event_id", event.ID,
			"merchant_id", event.MerchantID,
			"billable_event_type", event.BillableEventType,
			"status", event.Status,
		)

		return &event, nil
	}

	if !errors.Is(err, pgx.ErrNoRows) {
		err = classifyMerchantBillableEventWriteError(err)

		logger.Error(
			"Merchant billable event lifecycle transition failed",
			err,
			"billable_event_id", id,
			"to_status", toStatus,
		)
		return nil, err
	}

	eventResult, resolveErr := m.resolveTransitionNoMatch(
		ctx,
		querier,
		id,
		toStatus,
	)
	if resolveErr != nil {
		logger.Error(
			"Merchant billable event lifecycle transition resolution failed",
			resolveErr,
			"billable_event_id", id,
			"to_status", toStatus,
		)
	}

	return eventResult, resolveErr
}

// Confirm transitions a pending merchant billable event to confirmed through
// the model's database pool.
//
// Repeated confirmation of an already confirmed event is idempotent and returns
// the current event. Other source states return
// ErrMerchantBillableEventInvalidTransition.
func (m *MerchantBillableEventModel) Confirm(
	ctx context.Context,
	id uuid.UUID,
) (*MerchantBillableEvent, error) {
	if err := m.validatePool(); err != nil {
		return nil, err
	}

	return m.transition(
		ctx,
		m.DB,
		"ConfirmMerchantBillableEvent",
		merchantBillableEventConfirmQuery,
		id,
		MerchantBillableEventStatusConfirmed,
	)
}

// ConfirmTx is the transaction-aware form of Confirm.
//
// ConfirmTx does not begin, commit, or roll back tx.
func (m *MerchantBillableEventModel) ConfirmTx(
	ctx context.Context,
	tx pgx.Tx,
	id uuid.UUID,
) (*MerchantBillableEvent, error) {
	if err := m.validateBase(); err != nil {
		return nil, err
	}

	if tx == nil {
		return nil, merchantBillableEventInvalidInput(
			"transaction is required",
		)
	}

	return m.transition(
		ctx,
		tx,
		"ConfirmMerchantBillableEventTx",
		merchantBillableEventConfirmQuery,
		id,
		MerchantBillableEventStatusConfirmed,
	)
}

// Reject transitions a pending merchant billable event to rejected through the
// model's database pool.
//
// Repeated rejection of an already rejected event is idempotent and returns the
// current event. Other source states return
// ErrMerchantBillableEventInvalidTransition.
func (m *MerchantBillableEventModel) Reject(
	ctx context.Context,
	id uuid.UUID,
) (*MerchantBillableEvent, error) {
	if err := m.validatePool(); err != nil {
		return nil, err
	}

	return m.transition(
		ctx,
		m.DB,
		"RejectMerchantBillableEvent",
		merchantBillableEventRejectQuery,
		id,
		MerchantBillableEventStatusRejected,
	)
}

// RejectTx is the transaction-aware form of Reject.
//
// RejectTx does not begin, commit, or roll back tx.
func (m *MerchantBillableEventModel) RejectTx(
	ctx context.Context,
	tx pgx.Tx,
	id uuid.UUID,
) (*MerchantBillableEvent, error) {
	if err := m.validateBase(); err != nil {
		return nil, err
	}

	if tx == nil {
		return nil, merchantBillableEventInvalidInput(
			"transaction is required",
		)
	}

	return m.transition(
		ctx,
		tx,
		"RejectMerchantBillableEventTx",
		merchantBillableEventRejectQuery,
		id,
		MerchantBillableEventStatusRejected,
	)
}

// Reverse transitions a confirmed merchant billable event to reversed through
// the model's database pool.
//
// Reversal preserves confirmed_at and sets reversed_at. Repeated reversal of an
// already reversed event is idempotent and returns the current event. Other
// source states return ErrMerchantBillableEventInvalidTransition.
func (m *MerchantBillableEventModel) Reverse(
	ctx context.Context,
	id uuid.UUID,
) (*MerchantBillableEvent, error) {
	if err := m.validatePool(); err != nil {
		return nil, err
	}

	return m.transition(
		ctx,
		m.DB,
		"ReverseMerchantBillableEvent",
		merchantBillableEventReverseQuery,
		id,
		MerchantBillableEventStatusReversed,
	)
}

// ReverseTx is the transaction-aware form of Reverse.
//
// ReverseTx does not begin, commit, or roll back tx.
func (m *MerchantBillableEventModel) ReverseTx(
	ctx context.Context,
	tx pgx.Tx,
	id uuid.UUID,
) (*MerchantBillableEvent, error) {
	if err := m.validateBase(); err != nil {
		return nil, err
	}

	if tx == nil {
		return nil, merchantBillableEventInvalidInput(
			"transaction is required",
		)
	}

	return m.transition(
		ctx,
		tx,
		"ReverseMerchantBillableEventTx",
		merchantBillableEventReverseQuery,
		id,
		MerchantBillableEventStatusReversed,
	)
}
