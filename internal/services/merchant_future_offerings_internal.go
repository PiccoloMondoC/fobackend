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
//	  merchant_future_offerings is the PCDF-M01 orchestration anchor. This
//	  file owns the transaction boundaries for operations that span
//	  authoritative Future Offering domains:
//	    - draft creation + 'created' history;
//	    - authoritative readiness evaluation;
//	    - draft -> submitted transition + 'submitted_for_activation' history
//	      + FutureOfferingSubmitted outbox event.
//
//	Single-domain draft fact replacement and discard remain data-model
//	operations invoked directly by handlers.
//
// Submission Boundary:
//
//	Submission evaluates the canonical composite readiness (see
//	merchant_future_offering_readiness.go) inside the transaction that
//	performs the transition. Callers cannot supply or omit readiness checks.
//	The history record and outbox event commit atomically with the status
//	change. The Activation billable event (FOCA §16) is NOT created here; it
//	is produced by the Commerce consumer of FutureOfferingSubmitted, keyed by
//	future_offering_event_id (SE report §7, CE confirmation requested).
//
// Lock Order (all M01 transactions):
//
//  1. merchant_future_offerings row (FOR UPDATE, merchant-scoped)
//  2. engagement_actions rows (FOR SHARE)
//  3. engagement groups/options rows (DML)
//  4. merchant_future_offerings_events / outbox_events (INSERT)
//
// SPINE Rule:
//
//	Keep compiling.
//	Keep production-ready.
//	Preserve merchant scope and optimistic concurrency.
//	Preserve one service-owned transaction for submission writes, history and
//	outbox.
//	Preserve the documented lock order.
//	Preserve canonical, non-selectable readiness.
//	Do not perform network calls inside a transaction.
//	Block deployment if submission can commit without readiness, history,
//	or outbox persistence.
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
// FutureOfferingSubmitted v1. It carries stable identity only; consumers read
// mutable descriptive state from the authoritative domain.
//
// future_offering_event_id identifies the authoritative submitted_for_activation
// history record, giving Commerce a stable key for the Activation billable
// event (merchant_billable_events already supports lookup by FO event id).
type merchantFutureOfferingSubmittedPayloadV1 struct {
	FutureOfferingID      uuid.UUID `json:"future_offering_id"`
	FutureOfferingEventID uuid.UUID `json:"future_offering_event_id"`
	MerchantID            uuid.UUID `json:"merchant_id"`
	SubmittedAt           time.Time `json:"submitted_at"`
}

func marshalMerchantFutureOfferingSubmittedPayload(
	fo *data.MerchantFutureOffering,
	history *data.MerchantFutureOfferingEvent,
) (json.RawMessage, error) {
	if fo == nil || fo.ID == uuid.Nil || fo.MerchantID == uuid.Nil ||
		fo.SubmittedAt == nil || fo.SubmittedAt.IsZero() ||
		history == nil || history.ID == uuid.Nil {
		return nil, data.ErrMerchantFutureOfferingInvalidState
	}
	payload, err := json.Marshal(merchantFutureOfferingSubmittedPayloadV1{
		FutureOfferingID:      fo.ID,
		FutureOfferingEventID: history.ID,
		MerchantID:            fo.MerchantID,
		SubmittedAt:           fo.SubmittedAt.UTC(),
	})
	if err != nil {
		return nil, fmt.Errorf("marshal FutureOfferingSubmitted v1 payload: %w", err)
	}
	return payload, nil
}

// runMerchantFutureOfferingTx owns begin/rollback/commit for M01 service
// transactions. commit=false always rolls back (used for read-only
// evaluation that must take the same locks as submission).
//
// CE NOTE: if a central transaction helper exists in services, replace this
// with it (BEG 3.21/3.22). None was supplied with the assignment.
func (s *Service) runMerchantFutureOfferingTx(
	ctx context.Context,
	functionName string,
	commit bool,
	fn func(ctx context.Context, tx pgx.Tx) error,
) error {
	if err := s.validate(); err != nil {
		return err
	}
	if ctx == nil {
		return ErrNilContext
	}
	ctx, cancel := context.WithTimeout(ctx, s.Cfg.DBTimeout)
	defer cancel()

	logger := s.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName(functionName)

	tx, err := s.Models.DB.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin %s transaction: %w", functionName, err)
	}
	finished := false
	defer func() {
		if finished {
			return
		}
		if rbErr := tx.Rollback(ctx); rbErr != nil && !errors.Is(rbErr, pgx.ErrTxClosed) {
			logger.Warn("Future Offering transaction rollback failed", "error", rbErr)
		}
	}()

	if err := fn(ctx, tx); err != nil {
		return err
	}
	if !commit {
		return nil
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit %s transaction: %w", functionName, err)
	}
	finished = true
	return nil
}

func validateMerchantFutureOfferingScope(merchantID, futureOfferingID uuid.UUID) error {
	if merchantID == uuid.Nil {
		return fmt.Errorf("%w: merchant_id is required", data.ErrMerchantFutureOfferingInvalidInput)
	}
	if futureOfferingID == uuid.Nil {
		return fmt.Errorf("%w: future_offering_id is required", data.ErrMerchantFutureOfferingInvalidInput)
	}
	return nil
}

func validatePerformedBy(userID uuid.UUID) error {
	if userID == uuid.Nil {
		return fmt.Errorf("%w: performing user is required", data.ErrMerchantFutureOfferingInvalidInput)
	}
	return nil
}

// lockDraftAtVersion locks the merchant-scoped Future Offering and verifies
// it is a draft at expectedUpdatedAt. Stale requests fail before any
// cross-domain work.
func (s *Service) lockDraftAtVersion(
	ctx context.Context,
	tx pgx.Tx,
	merchantID, futureOfferingID uuid.UUID,
	expectedUpdatedAt time.Time,
) (*data.MerchantFutureOffering, error) {
	locked, err := s.Models.MerchantFutureOffering.GetByIDForUpdateTx(ctx, tx, merchantID, futureOfferingID)
	if err != nil {
		return nil, err
	}
	if data.NormalizeMerchantFutureOfferingStatus(locked.Status) != data.MerchantFutureOfferingStatusDraft {
		return nil, data.ErrMerchantFutureOfferingInvalidTransition
	}
	if !locked.UpdatedAt.Equal(expectedUpdatedAt.UTC()) {
		return nil, data.ErrMerchantFutureOfferingEditConflict
	}
	return locked, nil
}

// -----------------------------------------------------------------------------
// Create
// -----------------------------------------------------------------------------

// CreateMerchantFutureOfferingDraft creates a merchant-owned draft and its
// 'created' history record atomically.
func (s *Service) CreateMerchantFutureOfferingDraft(
	ctx context.Context,
	performedBy uuid.UUID,
	fo *data.MerchantFutureOffering,
) (*data.MerchantFutureOffering, error) {
	if err := validatePerformedBy(performedBy); err != nil {
		return nil, err
	}
	if fo == nil {
		return nil, fmt.Errorf("%w: future offering is required", data.ErrMerchantFutureOfferingInvalidInput)
	}
	var created *data.MerchantFutureOffering
	err := s.runMerchantFutureOfferingTx(ctx, "CreateMerchantFutureOfferingDraft", true,
		func(ctx context.Context, tx pgx.Tx) error {
			inserted, err := s.Models.MerchantFutureOffering.InsertDraftTx(ctx, tx, fo)
			if err != nil {
				return err
			}
			draft := data.MerchantFutureOfferingStatusDraft
			if _, err := s.Models.MerchantFutureOfferingEvent.InsertTx(ctx, tx, data.NewMerchantFutureOfferingEvent{
				FutureOfferingID: inserted.ID,
				EventType:        data.MerchantFutureOfferingEventCreated,
				ToStatus:         &draft,
				PerformedBy:      &performedBy,
			}); err != nil {
				return err
			}
			created = inserted
			return nil
		})
	if err != nil {
		return nil, err
	}
	return created, nil
}

// -----------------------------------------------------------------------------
// Readiness
// -----------------------------------------------------------------------------

// EvaluateMerchantFutureOfferingReadiness returns the authoritative readiness
// of a merchant-owned draft from a coherent read transaction. Submission re-evaluates
// the same canonical composite after taking its write lock and version guard.
func (s *Service) EvaluateMerchantFutureOfferingReadiness(
	ctx context.Context,
	merchantID, futureOfferingID uuid.UUID,
) (MerchantFutureOfferingReadiness, error) {
	var result MerchantFutureOfferingReadiness
	if err := validateMerchantFutureOfferingScope(merchantID, futureOfferingID); err != nil {
		return result, err
	}
	err := s.runMerchantFutureOfferingTx(ctx, "EvaluateMerchantFutureOfferingReadiness", false,
		func(ctx context.Context, tx pgx.Tx) error {
			fo, err := s.Models.MerchantFutureOffering.GetByIDForMerchantTx(ctx, tx, merchantID, futureOfferingID)
			if err != nil {
				return err
			}
			if data.NormalizeMerchantFutureOfferingStatus(fo.Status) != data.MerchantFutureOfferingStatusDraft {
				return data.ErrMerchantFutureOfferingInvalidTransition
			}
			result, err = s.merchantFutureOfferingReadiness().Evaluate(ctx, tx, fo)
			return err
		})
	return result, err
}

// -----------------------------------------------------------------------------
// Submit
// -----------------------------------------------------------------------------

// submitMerchantFutureOffering is deliberately package-internal until the complete M01 readiness composite exists. It performs the eventual activation boundary:
// draft -> submitted, guarded by merchant scope, optimistic concurrency and
// canonical readiness, with history and outbox committed atomically.
//
// A readiness failure returns *MerchantFutureOfferingNotReadyError carrying
// the authoritative issues.
func (s *Service) submitMerchantFutureOffering(
	ctx context.Context,
	merchantID, futureOfferingID uuid.UUID,
	performedBy uuid.UUID,
	expectedUpdatedAt time.Time,
) (*data.MerchantFutureOffering, error) {
	if err := validateMerchantFutureOfferingScope(merchantID, futureOfferingID); err != nil {
		return nil, err
	}
	if err := validatePerformedBy(performedBy); err != nil {
		return nil, err
	}
	if expectedUpdatedAt.IsZero() {
		return nil, fmt.Errorf("%w: expected_updated_at is required", data.ErrMerchantFutureOfferingInvalidInput)
	}

	var submitted *data.MerchantFutureOffering
	err := s.runMerchantFutureOfferingTx(ctx, "SubmitMerchantFutureOffering", true,
		func(ctx context.Context, tx pgx.Tx) error {
			locked, err := s.lockDraftAtVersion(ctx, tx, merchantID, futureOfferingID, expectedUpdatedAt)
			if err != nil {
				return err
			}

			readiness, err := s.merchantFutureOfferingReadiness().Evaluate(ctx, tx, locked)
			if err != nil {
				return err
			}
			if !readiness.Ready {
				return &MerchantFutureOfferingNotReadyError{Readiness: readiness}
			}

			result, err := s.Models.MerchantFutureOffering.SubmitTx(ctx, tx, merchantID, futureOfferingID, expectedUpdatedAt)
			if err != nil {
				return err
			}

			from := data.MerchantFutureOfferingStatusDraft
			to := data.MerchantFutureOfferingStatusSubmitted
			history, err := s.Models.MerchantFutureOfferingEvent.InsertTx(ctx, tx, data.NewMerchantFutureOfferingEvent{
				FutureOfferingID: result.ID,
				EventType:        data.MerchantFutureOfferingEventSubmittedForActivation,
				FromStatus:       &from,
				ToStatus:         &to,
				PerformedBy:      &performedBy,
			})
			if err != nil {
				return err
			}

			payload, err := marshalMerchantFutureOfferingSubmittedPayload(result, history)
			if err != nil {
				return err
			}
			occurredAt := result.SubmittedAt.UTC()
			if _, err := s.Models.OutboxEvent.InsertTx(ctx, tx, data.NewOutboxEvent{
				AggregateType: merchantFutureOfferingAggregateType,
				AggregateID:   result.ID,
				EventType:     merchantFutureOfferingSubmittedEventType,
				EventVersion:  merchantFutureOfferingSubmittedEventVersion,
				Payload:       payload,
				// Keyed by the history record, not the FO: a future
				// resubmission (after changes_requested) is a distinct
				// occurrence and must not collide.
				IdempotencyKey: fmt.Sprintf("merchant_future_offering_event:%s", history.ID),
				OccurredAt:     &occurredAt,
			}); err != nil {
				return fmt.Errorf("persist FutureOfferingSubmitted outbox event: %w", err)
			}

			submitted = result
			return nil
		})
	if err != nil {
		return nil, err
	}

	s.Logger.GetLoggerWithContextFromContext(ctx).
		WithFunctionName("SubmitMerchantFutureOffering").
		Info("merchant Future Offering submitted",
			"future_offering_id", submitted.ID,
			"merchant_id", submitted.MerchantID,
		)
	return submitted, nil
}
