// Package services contains internal merchant program subscription lifecycle
// event workflow support.
//
// sdworkspace/sdbackend/internal/services/merchant_program_subscription_events_internal.go
//
// GTM:
//
//	Layer: 2.4 Merchant / Future Offering Domain
//	Release Class: SPINE
//	Reason:
//	  Merchant program subscription lifecycle events preserve the durable,
//	  append-only history of commercially meaningful subscription actions.
//
//	  Merchant program subscriptions are optional commercial packaging
//	  infrastructure beneath the Future Offering Platform and Monetization
//	  Layer. This capability remains compiled and production-ready regardless
//	  of whether subscriptions are administratively enabled or disabled.
//
//	  Canonical current subscription state remains owned by
//	  merchant_program_subscriptions. This service provides the transaction-
//	  compatible event-recording seam used by coordinating subscription
//	  lifecycle workflows so the canonical subscription mutation and its
//	  corresponding immutable event can be committed atomically.
//
//	  The coordinating subscription lifecycle workflow owns:
//	    - transaction creation and completion;
//	    - canonical subscription mutation;
//	    - prior-state evaluation;
//	    - activated-versus-resumed semantics;
//	    - authorization and commercial-policy evaluation.
//
//	  This service owns only validated, transaction-required lifecycle-event
//	  recording.
//
//	  This file does not begin, commit, or roll back transactions. It does not
//	  independently mutate subscription state, expose event creation through
//	  HTTP, perform billing, publish outbox events, implement authorization or
//	  commercial policy, or provide asynchronous event insertion.
//
// SPINE Rule:
//
//	Keep compiling.
//	Keep production-ready.
//	Preserve append-only lifecycle-event integrity.
//	Preserve canonical subscription-state ownership.
//	Preserve transaction-required event recording.
//	Preserve canonical event-type validation.
//	Preserve actor provenance where available.
//	Preserve DB-owned event timestamps.
//	Preserve operation independently of commercial enablement policy.
//	Never provide a non-transactional lifecycle-transition event workflow.
//	Never begin, commit, or roll back a caller-owned transaction.
//	Never require a database pool for caller-supplied transaction execution.
//	Never impose an independent timeout on caller-owned transaction work.
//	Never log lifecycle-event note contents.
//	Never expose update, delete, restore, purge, or upsert behavior.
//	Never move canonical lifecycle-event insertion to asynchronous processing.
//	Block deployment if this file breaks transaction-compatible lifecycle-event
//	recording or permits subscription state and event history to diverge.
package services

import (
	"context"
	"errors"
	"fmt"

	"github.com/PiccoloMondoC/sdworkspace/sdbackend/internal/data"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// MerchantProgramSubscriptionEventRecordInput contains the canonical workflow
// input required to record one immutable merchant program subscription
// lifecycle event.
//
// The coordinating subscription lifecycle workflow supplies SubscriptionID and
// EventType after determining the correct lifecycle action from canonical
// subscription state.
//
// PerformedBy is nil for platform-originated actions or when no retained user
// actor can properly be attributed.
//
// Note is optional explanatory context. The data layer owns its canonical
// persistence normalization. Note contents must never be written to application
// logs, audit metadata, metrics, traces, or error messages.
type MerchantProgramSubscriptionEventRecordInput struct {
	SubscriptionID uuid.UUID
	EventType      data.MerchantProgramSubscriptionEventType
	Note           *string
	PerformedBy    *uuid.UUID
}

// validateMerchantProgramSubscriptionEventTxService validates only the
// dependencies required for transaction-backed lifecycle-event recording.
//
// This path deliberately does not require:
//
//   - Service.Cfg;
//   - Service.Cfg.DBTimeout; or
//   - MerchantProgramSubscriptionEvent.DB.
//
// The caller owns the transaction and its context lifetime. Persistence occurs
// exclusively through the supplied pgx.Tx. The event model's logger remains
// required because its InsertTx method performs model-level observability and
// validation.
func validateMerchantProgramSubscriptionEventTxService(
	s *Service,
) error {
	if s == nil {
		return fmt.Errorf(
			"%w: merchant program subscription event service is nil",
			ErrInvalidServiceConfiguration,
		)
	}
	if s.Logger == nil {
		return fmt.Errorf(
			"%w: merchant program subscription event service logger is nil",
			ErrInvalidServiceConfiguration,
		)
	}
	if s.Models == nil {
		return fmt.Errorf(
			"%w: merchant program subscription event service models are nil",
			ErrInvalidServiceConfiguration,
		)
	}
	if s.Models.MerchantProgramSubscriptionEvent.Logger == nil {
		return fmt.Errorf(
			"%w: merchant program subscription event model logger is nil",
			ErrInvalidServiceConfiguration,
		)
	}

	return nil
}

// validateMerchantProgramSubscriptionEventRecordInput validates and
// canonicalizes workflow-owned lifecycle-event input.
//
// The service owns validation of workflow identifiers and the controlled event
// vocabulary. The data model remains the canonical owner of persistence-level
// normalization, including optional note normalization.
func validateMerchantProgramSubscriptionEventRecordInput(
	input MerchantProgramSubscriptionEventRecordInput,
) (MerchantProgramSubscriptionEventRecordInput, error) {
	if input.SubscriptionID == uuid.Nil {
		return MerchantProgramSubscriptionEventRecordInput{},
			errors.New(
				"merchant program subscription event subscription ID is required",
			)
	}

	input.EventType =
		data.NormalizeMerchantProgramSubscriptionEventType(
			input.EventType,
		)

	if !data.IsValidMerchantProgramSubscriptionEventType(
		input.EventType,
	) {
		return MerchantProgramSubscriptionEventRecordInput{},
			fmt.Errorf(
				"invalid merchant program subscription event type: %s",
				input.EventType,
			)
	}

	if input.PerformedBy != nil &&
		*input.PerformedBy == uuid.Nil {
		return MerchantProgramSubscriptionEventRecordInput{},
			errors.New(
				"merchant program subscription event performed-by ID must not be a nil UUID",
			)
	}

	return input, nil
}

// RecordMerchantProgramSubscriptionEventTxInternal records one immutable
// subscription lifecycle event through an existing transaction.
//
// The method must be called by a coordinating subscription lifecycle workflow
// that uses the same transaction for:
//
//  1. the canonical merchant_program_subscriptions mutation; and
//  2. the corresponding merchant_program_subscription_events insertion.
//
// Transaction-compatible subscription mutation methods already exist in the
// subscription data model. The coordinating lifecycle workflow must select the
// correct event from the canonical prior state. In particular:
//
//   - initial or pre-active activation records activated;
//   - paused-to-active records resumed;
//   - suspended-to-active records resumed.
//
// This method deliberately does not inspect or mutate subscription state. It
// trusts the authorized coordinating workflow to provide the lifecycle event
// that corresponds to the mutation performed in the same transaction.
//
// RecordMerchantProgramSubscriptionEventTxInternal does not begin, commit, or
// roll back tx. The caller owns transaction lifecycle and context lifetime.
//
// This method must not be used as a general-purpose event writer or to represent
// a lifecycle transition whose canonical subscription mutation occurs outside
// the same transaction.
func (s *Service) RecordMerchantProgramSubscriptionEventTxInternal(
	ctx context.Context,
	tx pgx.Tx,
	input MerchantProgramSubscriptionEventRecordInput,
) (*data.MerchantProgramSubscriptionEvent, error) {
	if ctx == nil {
		return nil, ErrNilContext
	}

	if err := validateMerchantProgramSubscriptionEventTxService(s); err != nil {
		return nil, err
	}

	if tx == nil {
		return nil, errors.New(
			"merchant program subscription event transaction is required",
		)
	}

	canonicalInput, err :=
		validateMerchantProgramSubscriptionEventRecordInput(
			input,
		)
	if err != nil {
		return nil, err
	}

	logger := s.Logger.
		GetLoggerWithContextFromContext(ctx).
		WithFunctionName(
			"RecordMerchantProgramSubscriptionEventTxInternal",
		)

	event := &data.MerchantProgramSubscriptionEvent{
		SubscriptionID: canonicalInput.SubscriptionID,
		EventType:      canonicalInput.EventType,
		Note:           canonicalInput.Note,
		PerformedBy:    canonicalInput.PerformedBy,
	}

	if err :=
		s.Models.
			MerchantProgramSubscriptionEvent.
			InsertTx(
				ctx,
				tx,
				event,
			); err != nil {
		logFields := []any{
			"error",
			err,
			"subscription_id",
			canonicalInput.SubscriptionID,
			"event_type",
			canonicalInput.EventType,
		}

		if canonicalInput.PerformedBy != nil {
			logFields = append(
				logFields,
				"performed_by",
				*canonicalInput.PerformedBy,
			)
		}

		logger.Error(
			"Record merchant program subscription event in transaction failed",
			logFields...,
		)

		return nil, fmt.Errorf(
			"record merchant program subscription event for subscription %s: %w",
			canonicalInput.SubscriptionID,
			err,
		)
	}

	logFields := []any{
		"event_id",
		event.ID,
		"subscription_id",
		event.SubscriptionID,
		"event_type",
		event.EventType,
	}

	if event.PerformedBy != nil {
		logFields = append(
			logFields,
			"performed_by",
			*event.PerformedBy,
		)
	}

	logger.Info(
		"Merchant program subscription event recorded in transaction",
		logFields...,
	)

	return event, nil
}
