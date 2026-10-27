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
//	  Provides the shared FO transaction boundary, merchant-owned draft
//	  creation with atomic history, and authoritative readiness evaluation.
//	  Automated activation, media, active updates and prelaunch closure
//	  are composed in future_offering_publication.go.
//
// Product Boundary:
//
//	Activation makes an idea available for consumer anticipation and
//	validation before the merchant commits to Build. Activation does not
//	record a decision to build. If the merchant proceeds, the same FO
//	continues supporting anticipation and demand cultivation toward Launch.
//
//	Commercial terms, payments, invoicing and billable events are deferred.
//	They are not prerequisites or side effects of this creation workflow.
//
// Transaction and Lock Ownership:
//
//	runMerchantFutureOfferingTx owns begin, commit and bounded rollback.
//	Readiness evaluation takes the aggregate lock and then rolls back.
//	Mutation composition through runOfferingMutationTx first locks live
//	user and merchant-account authority, then the merchant-scoped FO.
//	Supporting-domain locks follow the FO lock; history and outbox writes
//	belong to the same transaction as their authoritative mutation.
//
// SPINE Rule:
//
//	Preserve merchant ownership, authorization and optimistic concurrency.
//	Preserve atomic draft creation and its lifecycle history.
//	Preserve canonical readiness; callers cannot omit required checks.
//	Preserve the documented lock order and cancellation-safe rollback.
//	Perform no external storage or network calls while holding write locks.
//	Activation must re-evaluate readiness and commit its lifecycle history
//	and outbox occurrence atomically with publication.
//	Do not equate activation with merchant commitment to Build or Launch.
//	Do not introduce duration-driven expiry or deferred commercial work.
package services

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/PiccoloMondoC/focodebase/fobackend/internal/data"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

const merchantFutureOfferingAggregateType = "merchant_future_offering"

// runMerchantFutureOfferingTx owns begin/rollback/commit for M01 service
// transactions. commit=false always rolls back (used for read-only
// evaluation that must take the same locks as submission).
//
// This is the shared FO transaction owner. Administrative transactions retain
// their separate governance policy; offering mutations add live principal locks
// through runOfferingMutationTx before taking aggregate locks.
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
		cleanupContext, cleanupCancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
        defer cleanupCancel()
        if rbErr := tx.Rollback(cleanupContext); rbErr != nil && !errors.Is(rbErr, pgx.ErrTxClosed) {
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
	err := s.runOfferingMutationTx(ctx, "CreateMerchantFutureOfferingDraft", performedBy, fo.MerchantID,
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
			fo, err := s.Models.MerchantFutureOffering.GetByIDForUpdateTx(ctx, tx, merchantID, futureOfferingID)
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
