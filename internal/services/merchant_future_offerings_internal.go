// Package services contains trusted internal service composition,
// automation support, moderation helpers, and shared internal workflow logic.
//
// focodebase/fobackend/internal/services/merchant_future_offerings_internal.go
//
// GTM:
//
//	Layer: 2.6 Internal Services / Merchant Future Offering Domain
//	Release Class: SPINE
//	Reason:
//	  merchant_future_offerings is the core Future Offering aggregate and the
//	  PCDF-M01 orchestration anchor. This file owns the transaction boundary for
//	  the guarded draft -> submitted transition once canonical M01 readiness can
//	  be established across the contributing Future Offering domains.
//
//	  Ordinary aggregate draft operations remain data-model operations because
//	  they do not currently require cross-domain workflow orchestration.
//
// Submission Readiness Boundary:
//
//	M01 readiness is an aggregate-level contract composed from authoritative
//	facts owned by the contributing Phase 6 domains. Those domains own the
//	meaning and validation of their own facts; this orchestration anchor owns
//	which domain contributions constitute complete submission readiness.
//
//	The readiness contract is deliberately a single canonical evaluator. It is
//	not a variadic or caller-selected set of checks: an HTTP handler or other
//	caller must never be able to weaken submission readiness by omitting a
//	required domain.
//
//	No canonical composite evaluator is implemented in this file because the
//	contributing data/service contracts are not yet all present. The transaction
//	runner therefore remains package-internal and is not exposed through HTTP.
//	As those domains land, their authoritative checks compose behind
//	merchantFutureOfferingSubmissionReadiness; the transaction algorithm below
//	does not change.
//
// Transaction Boundary:
//
//	submitMerchantFutureOffering owns the transaction. It locks the merchant-
//	scoped Future Offering, rejects stale or invalid lifecycle state before
//	readiness work, evaluates canonical M01 readiness, performs the guarded
//	draft -> submitted transition through MerchantFutureOfferingModel.SubmitTx,
//	and persists FutureOfferingSubmitted through the transactional outbox before
//	committing.
//
//	Future Offering event/history persistence is a separate authoritative domain.
//	When its data-layer transaction contract lands, the canonical submission
//	orchestrator must include the corresponding submitted_for_activation history
//	record in this same commit boundary before HTTP submission is exposed.
//
// SPINE Rule:
//
//	Keep compiling.
//	Keep production-ready.
//	Preserve merchant scope and optimistic concurrency.
//	Preserve one service-owned transaction for submission-side authoritative
//	writes and the outbox event.
//	Preserve contributing-domain ownership of readiness facts.
//	Do not let callers select or omit required readiness checks.
//	Do not query another domain's tables directly from this file.
//	Do not invent submission policy from nullable Future Offering columns.
//	Do not perform network calls inside the submission transaction.
//	Do not create Future-Offering-specific worker machinery for outbox delivery.
//	Block deployment if submission can commit without canonical readiness,
//	required lifecycle history, or transactional outbox persistence.
package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/PiccoloMondoC/focodebase/fobackend/internal/data"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

const (
	merchantFutureOfferingAggregateType = "merchant_future_offering"

	merchantFutureOfferingSubmittedEventType    = "FutureOfferingSubmitted"
	merchantFutureOfferingSubmittedEventVersion = 1
)

// merchantFutureOfferingSubmittedPayloadV1 is the producer-owned payload for
// FutureOfferingSubmitted version 1.
//
// The payload contains only stable aggregate identity, merchant ownership, and
// occurrence time. Consumers that need mutable descriptive state must obtain it
// from the authoritative Future Offering domain rather than treating an event
// snapshot as a second source of truth.
type merchantFutureOfferingSubmittedPayloadV1 struct {
	FutureOfferingID uuid.UUID `json:"future_offering_id"`
	MerchantID       uuid.UUID `json:"merchant_id"`
	SubmittedAt      time.Time `json:"submitted_at"`
}

// merchantFutureOfferingSubmissionReadiness is the canonical M01 readiness
// contract consumed by the Future Offering submission orchestrator.
//
// The eventual concrete composite owns the complete required set of M01
// contributing domains. Individual contributing domains may expose narrower
// domain-owned validation/readiness methods, but callers of submission do not
// choose which of those requirements apply.
//
// Evaluate executes inside the caller-owned submission transaction. It must not
// begin, commit, or roll back the transaction, and it must not perform network
// calls while the transaction is held.
//
// Concurrency contract:
//
//	A contributing readiness implementation must ensure that every
//	authoritative fact on which a successful readiness determination depends
//	cannot be invalidated before the enclosing submission transaction commits.
//
//	The contributing domain owns how that guarantee is achieved. Appropriate
//	mechanisms may include row or aggregate locking, guarded lifecycle mutation,
//	optimistic version checks, immutability, monotonic state, or another explicit
//	domain invariant. A readiness implementation must not rely on an ordinary
//	unprotected read when concurrent mutation could make its successful result
//	stale before submission commits.
type merchantFutureOfferingSubmissionReadiness interface {
	Evaluate(
		ctx context.Context,
		tx pgx.Tx,
		fo *data.MerchantFutureOffering,
	) error
}

func marshalMerchantFutureOfferingSubmittedPayload(
	fo *data.MerchantFutureOffering,
) (json.RawMessage, error) {
	if fo == nil ||
		fo.ID == uuid.Nil ||
		fo.MerchantID == uuid.Nil ||
		fo.SubmittedAt == nil ||
		fo.SubmittedAt.IsZero() {
		return nil, data.ErrMerchantFutureOfferingInvalidState
	}

	payload, err := json.Marshal(
		merchantFutureOfferingSubmittedPayloadV1{
			FutureOfferingID: fo.ID,
			MerchantID:       fo.MerchantID,
			SubmittedAt:      fo.SubmittedAt.UTC(),
		},
	)
	if err != nil {
		return nil, fmt.Errorf(
			"marshal FutureOfferingSubmitted v1 payload: %w",
			err,
		)
	}

	return payload, nil
}

// submitMerchantFutureOffering executes the authoritative transaction algorithm
// for PCDF-M01 submission.
//
// This method is intentionally package-internal until the complete canonical
// M01 readiness composite and Future Offering event/history transaction method
// exist. HTTP exposure before those contracts exist would turn a partially
// implemented orchestration boundary into product behavior.
func (s *Service) submitMerchantFutureOffering(
	ctx context.Context,
	merchantID uuid.UUID,
	futureOfferingID uuid.UUID,
	expectedUpdatedAt time.Time,
	readiness merchantFutureOfferingSubmissionReadiness,
) (*data.MerchantFutureOffering, error) {
	if err := s.validate(); err != nil {
		return nil, err
	}
	if ctx == nil {
		return nil, ErrNilContext
	}
	if readiness == nil {
		return nil, fmt.Errorf(
			"%w: merchant Future Offering submission readiness is not composed",
			ErrInvalidServiceConfiguration,
		)
	}

	if merchantID == uuid.Nil {
		return nil, fmt.Errorf(
			"%w: merchant_id is required",
			data.ErrMerchantFutureOfferingInvalidInput,
		)
	}
	if futureOfferingID == uuid.Nil {
		return nil, fmt.Errorf(
			"%w: future_offering_id is required",
			data.ErrMerchantFutureOfferingInvalidInput,
		)
	}
	if expectedUpdatedAt.IsZero() {
		return nil, fmt.Errorf(
			"%w: expected_updated_at is required",
			data.ErrMerchantFutureOfferingInvalidInput,
		)
	}

	pool := s.Models.DB
	if pool == nil {
		return nil, fmt.Errorf(
			"%w: database pool is nil",
			ErrInvalidServiceConfiguration,
		)
	}

	ctx, cancel := context.WithTimeout(ctx, s.Cfg.DBTimeout)
	defer cancel()

	logger := s.Logger.GetLoggerWithContextFromContext(ctx).
		WithFunctionName("submitMerchantFutureOffering")

	tx, err := pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf(
			"begin merchant Future Offering submission transaction: %w",
			err,
		)
	}

	committed := false
	defer func() {
		if committed {
			return
		}

		if rbErr := tx.Rollback(ctx); rbErr != nil &&
			!errors.Is(rbErr, pgx.ErrTxClosed) {
			logger.Warn(
				"merchant Future Offering submission rollback failed",
				"future_offering_id", futureOfferingID,
				"merchant_id", merchantID,
				"error", rbErr,
			)
		}
	}()

	locked, err := s.Models.MerchantFutureOffering.GetByIDForUpdateTx(
		ctx,
		tx,
		merchantID,
		futureOfferingID,
	)
	if err != nil {
		return nil, err
	}

	if locked.Status != data.MerchantFutureOfferingStatusDraft {
		return nil, data.ErrMerchantFutureOfferingInvalidTransition
	}

	// Fail stale requests before potentially multi-domain readiness work.
	//
	// SubmitTx remains the authoritative persistence guard and repeats the
	// optimistic-concurrency condition in SQL before changing lifecycle state.
	if !locked.UpdatedAt.Equal(expectedUpdatedAt.UTC()) {
		return nil, data.ErrMerchantFutureOfferingEditConflict
	}

	if err := readiness.Evaluate(ctx, tx, locked); err != nil {
		return nil, err
	}

	submitted, err := s.Models.MerchantFutureOffering.SubmitTx(
		ctx,
		tx,
		merchantID,
		futureOfferingID,
		expectedUpdatedAt,
	)
	if err != nil {
		return nil, err
	}

	payload, err := marshalMerchantFutureOfferingSubmittedPayload(submitted)
	if err != nil {
		return nil, err
	}

	occurredAt := submitted.SubmittedAt.UTC()

	if _, err := s.Models.OutboxEvent.InsertTx(
		ctx,
		tx,
		data.NewOutboxEvent{
			AggregateType: merchantFutureOfferingAggregateType,
			AggregateID:   submitted.ID,
			EventType:     merchantFutureOfferingSubmittedEventType,
			EventVersion:  merchantFutureOfferingSubmittedEventVersion,
			Payload:       payload,
			IdempotencyKey: fmt.Sprintf(
				"merchant_future_offering:%s:submitted",
				submitted.ID,
			),
			OccurredAt: &occurredAt,
		},
	); err != nil {
		return nil, fmt.Errorf(
			"persist FutureOfferingSubmitted outbox event: %w",
			err,
		)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf(
			"commit merchant Future Offering submission transaction: %w",
			err,
		)
	}
	committed = true

	logger.Info(
		"merchant Future Offering submitted",
		"future_offering_id", submitted.ID,
		"merchant_id", submitted.MerchantID,
	)

	return submitted, nil
}
