// Package services contains internal merchant billable-event source-integrity
// validation and lifecycle orchestration.
//
// focodebase/fobackend/internal/services/merchant_billable_events_internal.go
//
// GTM:
//
//	Layer: 2.4.b Commerce Architecture / Monetization Layer
//	Release Class: SPINE
//	Reason:
//	  merchant_billable_events is the canonical source-linked commercial
//	  occurrence record downstream of authoritative Future Offering,
//	  Billing Period, and positive consumer-engagement facts and upstream
//	  of fee calculation, platform-credit application, invoicing, and payment
//	  collection.
//
//	  SQL constraints can enforce source shape and referential integrity but
//	  cannot prove the cross-table semantic relationship between a source fact,
//	  its merchant owner, and the billable occurrence represented by that fact.
//	  This service owns that proof before insertion.
//
// Domain Boundary:
//
//	A merchant billable event is not an invoice, fee calculation, invoice line,
//	payment, payment attempt, payment-provider transaction, billing account,
//	platform-credit application, merchant-held balance, billing-policy decision,
//	authorization to charge a merchant, or proof of a particular amount owed.
//
//	This service does not decide whether a valid source occurrence is currently
//	commercially billable. Administration-governed configuration and the
//	coordinating Commerce workflow decide whether to invoke recording.
//
//	The service does not calculate fees, select fee schedules, apply credits or
//	promotions, generate invoices, execute payments, or decide collection
//	policy.
//
// Source-Integrity Contract:
//
//	Callers provide only:
//	  - the authoritative source identifier; and
//	  - optional gross-event-value/currency metadata.
//
//	The service derives:
//	  - merchant_id;
//	  - occurred_at; and
//	  - source-semantic billable_event_type.
//
//	Activation requires submitted_for_activation.
//
//	Platform Service Fee occurrences derive merchant ownership and occurrence
//	time from the authoritative Billing Period source fact.
//
//	Consumer-engagement occurrences accept only:
//	  watched                       -> watch
//	  waitlisted                    -> waitlist
//	  early_access_requested        -> early_access_request
//	  beta_joined                   -> beta
//	  reservation_interest_recorded -> reservation_interest
//	  preorder_intent_recorded      -> preorder_intent
//
//	Negative, removal, notification, mute/unmute, unwatched, expiration, and
//	unknown engagement events are not positive billable-occurrence sources.
//
// Consumer Identity Sovereignty:
//
//	Engagement source resolution retains only opaque source identity,
//	merchant ownership, source event type, and occurrence timestamp. This
//	service never retrieves, returns, or logs consumer identity or engagement
//	contents.
//
// Transaction Boundary:
//
//	Pool-backed recording exists only for reconciliation against an
//	authoritative source that is already durably committed.
//
//	Tx-suffixed recording performs source resolution and billable-event
//	insertion through the same caller-owned pgx.Tx. These methods never begin,
//	commit, roll back, or silently replace that transaction.
//
//	Lifecycle Tx methods likewise preserve caller transaction ownership.
//
// Concurrency:
//
//	The database partial unique indexes on future_offering_event_id,
//	billing_period_id, and engagement_event_id are the durable idempotency
//	authority. This service never performs SELECT-before-INSERT duplicate
//	detection.
//
// SPINE Rule:
//
//	Keep compiling.
//	Keep production-ready.
//	Preserve authoritative source-derived merchant_id.
//	Preserve authoritative source-derived occurred_at.
//	Preserve service-owned source-semantic validation.
//	Preserve one-source/one-billable-event database idempotency.
//	Preserve transaction-compatible recording.
//	Preserve guarded lifecycle transitions.
//	Preserve exact monetary strings.
//	Preserve consumer identity sovereignty.
//	Preserve errors.Is compatibility through %w wrapping.
//	Never trust caller-supplied merchant ownership or occurrence timestamps.
//	Never manufacture positive engagement events from negative lifecycle facts.
//	Never implement commercial enablement, pricing, invoicing, payment,
//	collection, or authorization policy here.
//	Never expose consumer identity or engagement contents.
//	Never begin, commit, or roll back caller-owned transactions.
//	Never introduce asynchronous insertion without a durable asynchronous owner.
//	Block deployment if this file breaks source integrity, transaction
//	discipline, concurrency safety, privacy, or billing reconciliation readiness.
package services

import (
	"context"
	"fmt"

	"github.com/PiccoloMondoC/focodebase/fobackend/internal/data"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

const merchantBillableActivationSourceEventType = "submitted_for_activation"

var merchantBillableEngagementSourceTypes = map[string]data.MerchantBillableEventType{
	"watched":                       data.MerchantBillableEventTypeWatch,
	"waitlisted":                    data.MerchantBillableEventTypeWaitlist,
	"early_access_requested":        data.MerchantBillableEventTypeEarlyAccessRequest,
	"beta_joined":                   data.MerchantBillableEventTypeBeta,
	"reservation_interest_recorded": data.MerchantBillableEventTypeReservationInterest,
	"preorder_intent_recorded":      data.MerchantBillableEventTypePreorderIntent,
}

// MerchantBillableEventOccurrenceInput contains optional occurrence-level
// monetary metadata.
//
// GrossEventValue is not the amount owed. It is optional source-associated
// metadata or a possible basis for later fee calculation.
//
// Structural decimal validation and value/currency pairing remain owned by the
// MerchantBillableEvent data model.
type MerchantBillableEventOccurrenceInput struct {
	GrossEventValue *string
	Currency        *string
}

func validateMerchantBillableEventService(
	s *Service,
) error {
	if err := s.validate(); err != nil {
		return err
	}

	if s.Models.MerchantBillableEvent.DB == nil {
		return fmt.Errorf(
			"%w: merchant billable event model database is nil",
			ErrInvalidServiceConfiguration,
		)
	}

	if s.Models.MerchantBillableEvent.Logger == nil {
		return fmt.Errorf(
			"%w: merchant billable event model logger is nil",
			ErrInvalidServiceConfiguration,
		)
	}

	return nil
}

func validateMerchantBillableEventTxService(
	s *Service,
) error {
	if s == nil {
		return fmt.Errorf(
			"%w: merchant billable event service is nil",
			ErrInvalidServiceConfiguration,
		)
	}

	if s.Logger == nil {
		return fmt.Errorf(
			"%w: merchant billable event service logger is nil",
			ErrInvalidServiceConfiguration,
		)
	}

	if s.Models == nil {
		return fmt.Errorf(
			"%w: merchant billable event service models are nil",
			ErrInvalidServiceConfiguration,
		)
	}

	if s.Models.MerchantBillableEvent.Logger == nil {
		return fmt.Errorf(
			"%w: merchant billable event model logger is nil",
			ErrInvalidServiceConfiguration,
		)
	}

	return nil
}

func (s *Service) merchantBillableEventContext(
	ctx context.Context,
) (context.Context, context.CancelFunc, error) {
	if ctx == nil {
		return nil, nil, ErrNilContext
	}

	if err := validateMerchantBillableEventService(s); err != nil {
		return nil, nil, err
	}

	dbCtx, cancel := context.WithTimeout(
		ctx,
		s.Cfg.DBTimeout,
	)

	return dbCtx, cancel, nil
}

func validateMerchantBillableEventTx(
	tx pgx.Tx,
) error {
	if tx == nil {
		return fmt.Errorf(
			"%w: transaction is required",
			data.ErrMerchantBillableEventInvalidInput,
		)
	}

	return nil
}

func merchantBillableActivationEventFromSourceFact(
	futureOfferingEventID uuid.UUID,
	fact *data.MerchantBillableEventActivationSourceFact,
	input MerchantBillableEventOccurrenceInput,
) (*data.MerchantBillableEvent, error) {
	if fact == nil {
		return nil, fmt.Errorf(
			"%w: future offering event %s",
			ErrMerchantBillableEventSourceNotFound,
			futureOfferingEventID,
		)
	}

	if fact.EventType != merchantBillableActivationSourceEventType {
		return nil, fmt.Errorf(
			"%w: future offering event %s has source event type %q",
			ErrMerchantBillableEventSourceIneligible,
			futureOfferingEventID,
			fact.EventType,
		)
	}

	return &data.MerchantBillableEvent{
		MerchantID:            fact.MerchantID,
		FutureOfferingEventID: &futureOfferingEventID,
		BillableEventType:     data.MerchantBillableEventTypeActivation,
		GrossEventValue:       input.GrossEventValue,
		Currency:              input.Currency,
		OccurredAt:            fact.OccurredAt,
	}, nil
}

func merchantBillablePlatformServiceFeeEventFromSourceFact(
	billingPeriodID uuid.UUID,
	fact *data.MerchantBillableEventBillingPeriodSourceFact,
	input MerchantBillableEventOccurrenceInput,
) (*data.MerchantBillableEvent, error) {
	if fact == nil {
		return nil, fmt.Errorf(
			"%w: billing period %s",
			ErrMerchantBillableEventSourceNotFound,
			billingPeriodID,
		)
	}

	return &data.MerchantBillableEvent{
		MerchantID:           fact.MerchantID,
		BillingPeriodID: &billingPeriodID,
		BillableEventType:    data.MerchantBillableEventTypePlatformServiceFee,
		GrossEventValue:      input.GrossEventValue,
		Currency:             input.Currency,
		OccurredAt:           fact.OccurredAt,
	}, nil
}

func merchantBillableEngagementEventFromSourceFact(
	engagementEventID uuid.UUID,
	fact *data.MerchantBillableEventEngagementSourceFact,
	input MerchantBillableEventOccurrenceInput,
) (*data.MerchantBillableEvent, error) {
	if fact == nil {
		return nil, fmt.Errorf(
			"%w: engagement event %s",
			ErrMerchantBillableEventSourceNotFound,
			engagementEventID,
		)
	}

	billableEventType, ok :=
		merchantBillableEngagementSourceTypes[fact.EventType]
	if !ok {
		return nil, fmt.Errorf(
			"%w: engagement event %s has source event type %q",
			ErrMerchantBillableEventSourceIneligible,
			engagementEventID,
			fact.EventType,
		)
	}

	return &data.MerchantBillableEvent{
		MerchantID:        fact.MerchantID,
		EngagementEventID: &engagementEventID,
		BillableEventType: billableEventType,
		GrossEventValue:   input.GrossEventValue,
		Currency:          input.Currency,
		OccurredAt:        fact.OccurredAt,
	}, nil
}

// RecordMerchantBillableActivationEventInternal records an activation
// occurrence against an already committed authoritative source.
func (s *Service) RecordMerchantBillableActivationEventInternal(
	ctx context.Context,
	futureOfferingEventID uuid.UUID,
	input MerchantBillableEventOccurrenceInput,
) (*data.MerchantBillableEvent, error) {
	dbCtx, cancel, err := s.merchantBillableEventContext(ctx)
	if err != nil {
		return nil, err
	}
	defer cancel()

	fact, err :=
		s.Models.
			MerchantBillableEvent.
			GetActivationSourceFact(
				dbCtx,
				futureOfferingEventID,
			)
	if err != nil {
		return nil, fmt.Errorf(
			"resolve merchant billable activation source: %w",
			err,
		)
	}

	event, err := merchantBillableActivationEventFromSourceFact(
		futureOfferingEventID,
		fact,
		input,
	)
	if err != nil {
		return nil, err
	}

	result, err :=
		s.Models.
			MerchantBillableEvent.
			Insert(
				dbCtx,
				event,
			)
	if err != nil {
		return nil, fmt.Errorf(
			"record merchant billable activation event: %w",
			err,
		)
	}

	return result, nil
}

// RecordMerchantBillableActivationEventTxInternal records an activation
// occurrence through caller-owned tx.
func (s *Service) RecordMerchantBillableActivationEventTxInternal(
	ctx context.Context,
	tx pgx.Tx,
	futureOfferingEventID uuid.UUID,
	input MerchantBillableEventOccurrenceInput,
) (*data.MerchantBillableEvent, error) {
	if ctx == nil {
		return nil, ErrNilContext
	}

	if err := validateMerchantBillableEventTxService(s); err != nil {
		return nil, err
	}

	if err := validateMerchantBillableEventTx(tx); err != nil {
		return nil, err
	}

	fact, err :=
		s.Models.
			MerchantBillableEvent.
			GetActivationSourceFactTx(
				ctx,
				tx,
				futureOfferingEventID,
			)
	if err != nil {
		return nil, fmt.Errorf(
			"resolve merchant billable activation source in transaction: %w",
			err,
		)
	}

	event, err := merchantBillableActivationEventFromSourceFact(
		futureOfferingEventID,
		fact,
		input,
	)
	if err != nil {
		return nil, err
	}

	result, err :=
		s.Models.
			MerchantBillableEvent.
			InsertTx(
				ctx,
				tx,
				event,
			)
	if err != nil {
		return nil, fmt.Errorf(
			"record merchant billable activation event in transaction: %w",
			err,
		)
	}

	return result, nil
}

// RecordMerchantBillablePlatformServiceFeeEventInternal records one Platform
// Service Fee occurrence against an already committed authoritative Billing Period.
func (s *Service) RecordMerchantBillablePlatformServiceFeeEventInternal(
	ctx context.Context,
	billingPeriodID uuid.UUID,
	input MerchantBillableEventOccurrenceInput,
) (*data.MerchantBillableEvent, error) {
	dbCtx, cancel, err := s.merchantBillableEventContext(ctx)
	if err != nil {
		return nil, err
	}
	defer cancel()

	fact, err :=
		s.Models.
			MerchantBillableEvent.
			GetBillingPeriodSourceFact(
				dbCtx,
				billingPeriodID,
			)
	if err != nil {
		return nil, fmt.Errorf(
			"resolve merchant billable Platform Service Fee billing-period source: %w",
			err,
		)
	}

	event, err := merchantBillablePlatformServiceFeeEventFromSourceFact(
		billingPeriodID,
		fact,
		input,
	)
	if err != nil {
		return nil, err
	}

	result, err :=
		s.Models.
			MerchantBillableEvent.
			Insert(
				dbCtx,
				event,
			)
	if err != nil {
		return nil, fmt.Errorf(
			"record merchant billable Platform Service Fee event: %w",
			err,
		)
	}

	return result, nil
}

// RecordMerchantBillablePlatformServiceFeeEventTxInternal is the
// transaction-aware Platform Service Fee recording seam.
func (s *Service) RecordMerchantBillablePlatformServiceFeeEventTxInternal(
	ctx context.Context,
	tx pgx.Tx,
	billingPeriodID uuid.UUID,
	input MerchantBillableEventOccurrenceInput,
) (*data.MerchantBillableEvent, error) {
	if ctx == nil {
		return nil, ErrNilContext
	}

	if err := validateMerchantBillableEventTxService(s); err != nil {
		return nil, err
	}

	if err := validateMerchantBillableEventTx(tx); err != nil {
		return nil, err
	}

	fact, err :=
		s.Models.
			MerchantBillableEvent.
			GetBillingPeriodSourceFactTx(
				ctx,
				tx,
				billingPeriodID,
			)
	if err != nil {
		return nil, fmt.Errorf(
			"resolve merchant billable Platform Service Fee billing-period source in transaction: %w",
			err,
		)
	}

	event, err := merchantBillablePlatformServiceFeeEventFromSourceFact(
		billingPeriodID,
		fact,
		input,
	)
	if err != nil {
		return nil, err
	}

	result, err :=
		s.Models.
			MerchantBillableEvent.
			InsertTx(
				ctx,
				tx,
				event,
			)
	if err != nil {
		return nil, fmt.Errorf(
			"record merchant billable Platform Service Fee event in transaction: %w",
			err,
		)
	}

	return result, nil
}

// RecordMerchantBillableEngagementEventInternal records one positive
// engagement occurrence against an already committed authoritative engagement
// event.
func (s *Service) RecordMerchantBillableEngagementEventInternal(
	ctx context.Context,
	engagementEventID uuid.UUID,
	input MerchantBillableEventOccurrenceInput,
) (*data.MerchantBillableEvent, error) {
	dbCtx, cancel, err := s.merchantBillableEventContext(ctx)
	if err != nil {
		return nil, err
	}
	defer cancel()

	fact, err :=
		s.Models.
			MerchantBillableEvent.
			GetEngagementSourceFact(
				dbCtx,
				engagementEventID,
			)
	if err != nil {
		return nil, fmt.Errorf(
			"resolve merchant billable engagement source: %w",
			err,
		)
	}

	event, err := merchantBillableEngagementEventFromSourceFact(
		engagementEventID,
		fact,
		input,
	)
	if err != nil {
		return nil, err
	}

	result, err :=
		s.Models.
			MerchantBillableEvent.
			Insert(
				dbCtx,
				event,
			)
	if err != nil {
		return nil, fmt.Errorf(
			"record merchant billable engagement event: %w",
			err,
		)
	}

	return result, nil
}

// RecordMerchantBillableEngagementEventTxInternal is the transaction-aware
// positive-engagement recording seam.
func (s *Service) RecordMerchantBillableEngagementEventTxInternal(
	ctx context.Context,
	tx pgx.Tx,
	engagementEventID uuid.UUID,
	input MerchantBillableEventOccurrenceInput,
) (*data.MerchantBillableEvent, error) {
	if ctx == nil {
		return nil, ErrNilContext
	}

	if err := validateMerchantBillableEventTxService(s); err != nil {
		return nil, err
	}

	if err := validateMerchantBillableEventTx(tx); err != nil {
		return nil, err
	}

	fact, err :=
		s.Models.
			MerchantBillableEvent.
			GetEngagementSourceFactTx(
				ctx,
				tx,
				engagementEventID,
			)
	if err != nil {
		return nil, fmt.Errorf(
			"resolve merchant billable engagement source in transaction: %w",
			err,
		)
	}

	event, err := merchantBillableEngagementEventFromSourceFact(
		engagementEventID,
		fact,
		input,
	)
	if err != nil {
		return nil, err
	}

	result, err :=
		s.Models.
			MerchantBillableEvent.
			InsertTx(
				ctx,
				tx,
				event,
			)
	if err != nil {
		return nil, fmt.Errorf(
			"record merchant billable engagement event in transaction: %w",
			err,
		)
	}

	return result, nil
}

// ConfirmMerchantBillableEventInternal transitions a pending billable event to
// confirmed. The authorized coordinating workflow owns the reason for
// confirmation.
func (s *Service) ConfirmMerchantBillableEventInternal(
	ctx context.Context,
	id uuid.UUID,
) (*data.MerchantBillableEvent, error) {
	dbCtx, cancel, err := s.merchantBillableEventContext(ctx)
	if err != nil {
		return nil, err
	}
	defer cancel()

	result, err :=
		s.Models.
			MerchantBillableEvent.
			Confirm(
				dbCtx,
				id,
			)
	if err != nil {
		return nil, fmt.Errorf(
			"confirm merchant billable event: %w",
			err,
		)
	}

	return result, nil
}

// ConfirmMerchantBillableEventTxInternal is the transaction-aware confirmation
// seam.
func (s *Service) ConfirmMerchantBillableEventTxInternal(
	ctx context.Context,
	tx pgx.Tx,
	id uuid.UUID,
) (*data.MerchantBillableEvent, error) {
	if ctx == nil {
		return nil, ErrNilContext
	}

	if err := validateMerchantBillableEventTxService(s); err != nil {
		return nil, err
	}

	if err := validateMerchantBillableEventTx(tx); err != nil {
		return nil, err
	}

	result, err :=
		s.Models.
			MerchantBillableEvent.
			ConfirmTx(
				ctx,
				tx,
				id,
			)
	if err != nil {
		return nil, fmt.Errorf(
			"confirm merchant billable event in transaction: %w",
			err,
		)
	}

	return result, nil
}

// RejectMerchantBillableEventInternal transitions a pending billable event to
// rejected. The authorized coordinating workflow owns the reason for rejection.
func (s *Service) RejectMerchantBillableEventInternal(
	ctx context.Context,
	id uuid.UUID,
) (*data.MerchantBillableEvent, error) {
	dbCtx, cancel, err := s.merchantBillableEventContext(ctx)
	if err != nil {
		return nil, err
	}
	defer cancel()

	result, err :=
		s.Models.
			MerchantBillableEvent.
			Reject(
				dbCtx,
				id,
			)
	if err != nil {
		return nil, fmt.Errorf(
			"reject merchant billable event: %w",
			err,
		)
	}

	return result, nil
}

// RejectMerchantBillableEventTxInternal is the transaction-aware rejection
// seam.
func (s *Service) RejectMerchantBillableEventTxInternal(
	ctx context.Context,
	tx pgx.Tx,
	id uuid.UUID,
) (*data.MerchantBillableEvent, error) {
	if ctx == nil {
		return nil, ErrNilContext
	}

	if err := validateMerchantBillableEventTxService(s); err != nil {
		return nil, err
	}

	if err := validateMerchantBillableEventTx(tx); err != nil {
		return nil, err
	}

	result, err :=
		s.Models.
			MerchantBillableEvent.
			RejectTx(
				ctx,
				tx,
				id,
			)
	if err != nil {
		return nil, fmt.Errorf(
			"reject merchant billable event in transaction: %w",
			err,
		)
	}

	return result, nil
}

// ReverseMerchantBillableEventInternal transitions a confirmed billable event
// to reversed. The authorized coordinating workflow owns the reason for
// reversal.
func (s *Service) ReverseMerchantBillableEventInternal(
	ctx context.Context,
	id uuid.UUID,
) (*data.MerchantBillableEvent, error) {
	dbCtx, cancel, err := s.merchantBillableEventContext(ctx)
	if err != nil {
		return nil, err
	}
	defer cancel()

	result, err :=
		s.Models.
			MerchantBillableEvent.
			Reverse(
				dbCtx,
				id,
			)
	if err != nil {
		return nil, fmt.Errorf(
			"reverse merchant billable event: %w",
			err,
		)
	}

	return result, nil
}

// ReverseMerchantBillableEventTxInternal is the transaction-aware reversal
// seam.
func (s *Service) ReverseMerchantBillableEventTxInternal(
	ctx context.Context,
	tx pgx.Tx,
	id uuid.UUID,
) (*data.MerchantBillableEvent, error) {
	if ctx == nil {
		return nil, ErrNilContext
	}

	if err := validateMerchantBillableEventTxService(s); err != nil {
		return nil, err
	}

	if err := validateMerchantBillableEventTx(tx); err != nil {
		return nil, err
	}

	result, err :=
		s.Models.
			MerchantBillableEvent.
			ReverseTx(
				ctx,
				tx,
				id,
			)
	if err != nil {
		return nil, fmt.Errorf(
			"reverse merchant billable event in transaction: %w",
			err,
		)
	}

	return result, nil
}
