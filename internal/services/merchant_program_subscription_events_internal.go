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
//	  Canonical current subscription state remains owned by
//	  merchant_program_subscriptions. This service provides the transaction-
//	  compatible event-recording seam required by subscription lifecycle
//	  workflows so the canonical subscription mutation and corresponding event
//	  insertion can eventually occur atomically.
//
//	  This file does not begin, commit, or roll back transactions. Transaction
//	  ownership belongs to the coordinating subscription lifecycle workflow.
//
//	  This file does not independently mutate subscription state, expose event
//	  creation through HTTP, perform billing, publish outbox events, implement
//	  authorization policy, or provide asynchronous event insertion.
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
//	Never provide a non-transactional lifecycle-transition event workflow.
//	Never begin, commit, or roll back a caller-owned transaction.
//	Never log lifecycle-event note contents.
//	Never expose update, delete, restore, purge, or upsert behavior.
//	Block deployment if this file breaks transaction-compatible lifecycle-event
//	recording or permits subscription state and event history to diverge.
package services

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/PiccoloMondoC/sdworkspace/sdbackend/internal/data"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// MerchantProgramSubscriptionEventRecordInput contains the canonical input
// required to record one immutable merchant program subscription lifecycle
// event.
//
// The coordinating subscription workflow supplies SubscriptionID and EventType.
// PerformedBy is nil for platform-originated actions or when no retained user
// actor can properly be attributed.
//
// Note is optional explanatory context. Its contents must never be written to
// application logs, audit metadata, or error messages.
type MerchantProgramSubscriptionEventRecordInput struct {
	SubscriptionID uuid.UUID
	EventType      data.MerchantProgramSubscriptionEventType
	Note           *string
	PerformedBy    *uuid.UUID
}

// validateMerchantProgramSubscriptionEventService validates the dependency
// container required for transaction-compatible lifecycle-event recording.
func validateMerchantProgramSubscriptionEventService(
	s *Service,
) error {
	if s == nil {
		return errors.New(
			"merchant program subscription event service is required",
		)
	}
	if s.Logger == nil {
		return errors.New(
			"merchant program subscription event service logger is required",
		)
	}
	if s.Models == nil {
		return errors.New(
			"merchant program subscription event service models are required",
		)
	}
	if s.Cfg == nil {
		return errors.New(
			"merchant program subscription event service config is required",
		)
	}
	if s.Cfg.DBTimeout <= 0 {
		return errors.New(
			"merchant program subscription event service DB timeout must be positive",
		)
	}
	if s.Models.MerchantProgramSubscriptionEvent.DB == nil {
		return errors.New(
			"merchant program subscription event model database pool is required",
		)
	}
	if s.Models.MerchantProgramSubscriptionEvent.Logger == nil {
		return errors.New(
			"merchant program subscription event model logger is required",
		)
	}

	return nil
}

// normalizeMerchantProgramSubscriptionEventRecordNote trims an optional note.
//
// Blank notes are normalized to nil. The canonical normalized value returned
// here is the value passed to persistence.
func normalizeMerchantProgramSubscriptionEventRecordNote(
	note *string,
) *string {
	if note == nil {
		return nil
	}

	normalized := strings.TrimSpace(*note)
	if normalized == "" {
		return nil
	}

	return &normalized
}

// validateMerchantProgramSubscriptionEventRecordInput validates and
// canonicalizes lifecycle-event input.
//
// The returned value, rather than the caller's original input, must be used for
// persistence so validation and writing operate on the same canonical values.
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

	input.Note =
		normalizeMerchantProgramSubscriptionEventRecordNote(
			input.Note,
		)

	return input, nil
}

// RecordMerchantProgramSubscriptionEventTxInternal records one immutable
// subscription lifecycle event through an existing transaction.
//
// This method deliberately requires pgx.Tx. It must be called by a coordinating
// subscription lifecycle workflow that uses the same transaction for:
//
//  1. the canonical merchant_program_subscriptions mutation; and
//  2. the corresponding merchant_program_subscription_events insertion.
//
// This method does not begin, commit, or roll back tx. The caller owns the
// transaction lifecycle.
//
// Until transaction-compatible subscription mutation methods are added, this
// method is the authoritative event-side integration seam but must not be used
// to represent a lifecycle transition whose subscription mutation occurs
// outside the same transaction.
func (s *Service) RecordMerchantProgramSubscriptionEventTxInternal(
	ctx context.Context,
	tx pgx.Tx,
	input MerchantProgramSubscriptionEventRecordInput,
) (*data.MerchantProgramSubscriptionEvent, error) {
	if err :=
		validateMerchantProgramSubscriptionEventService(s); err != nil {
		return nil, err
	}
	if ctx == nil {
		return nil, errors.New(
			"merchant program subscription event context is required",
		)
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
