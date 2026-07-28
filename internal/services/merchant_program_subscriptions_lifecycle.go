// Package services contains coordinated merchant program subscription
// lifecycle workflows.
//
// sdworkspace/sdbackend/internal/services/merchant_program_subscriptions_lifecycle.go
//
// GTM:
//
//	Layer: 2.4 Merchant / Future Offering Domain
//	Release Class: SPINE
//	Reason:
//	  Merchant program subscription lifecycle workflows must preserve canonical
//	  subscription state and append-only lifecycle history atomically.
//
//	  This file owns transaction coordination for subscription creation,
//	  activation, resumption, pause, suspension, cancellation, expiration,
//	  and plan changes. Each successful canonical mutation records its
//	  corresponding merchant_program_subscription_events row in the same
//	  transaction.
//
//	  Merchant program subscriptions remain optional commercial packaging
//	  infrastructure. This capability remains compiled and production-ready
//	  regardless of whether subscriptions are administratively enabled.
//
//	  Engineering owns lifecycle-state validity, row locking, transaction
//	  integrity, event accuracy, and actor-provenance preservation.
//	  Administration governs whether and how subscriptions participate in the
//	  commercial operating model.
//
//	  This file does not determine whether subscriptions are enabled, calculate
//	  fees, create invoices, collect payments, resolve merchant ownership,
//	  authorize HTTP actors, publish outbox messages, or mutate event history.
//
// SPINE Rule:
//
//	Keep compiling.
//	Keep production-ready.
//	Preserve canonical subscription-state ownership.
//	Preserve append-only lifecycle history.
//	Preserve mutation-plus-event atomicity.
//	Preserve row locking before transition decisions.
//	Preserve explicit lifecycle-state validity.
//	Preserve activated-versus-resumed correctness from canonical prior state.
//	Preserve actor provenance where available.
//	Preserve short transaction boundaries.
//	Never reactivate terminal subscriptions.
//	Never emit duplicate or contradictory lifecycle events for invalid repeats.
//	Never perform lifecycle mutation without recording its event.
//	Never record a lifecycle event outside the mutation transaction.
//	Never log lifecycle-event note contents.
//	Never impose commercial enablement policy in this integrity coordinator.
//	Block deployment if this file permits subscription state and event history
//	to diverge or permits an invalid lifecycle transition.
package services

import (
	"context"
	"errors"
	"fmt"

	"github.com/PiccoloMondoC/sdworkspace/sdbackend/internal/data"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// MerchantProgramSubscriptionCreateInput contains the canonical input for an
// atomic subscription-create workflow.
type MerchantProgramSubscriptionCreateInput struct {
	Subscription *data.MerchantProgramSubscription
	Note         *string
	PerformedBy  *uuid.UUID
}

// MerchantProgramSubscriptionTransition identifies one supported canonical
// subscription lifecycle transition.
type MerchantProgramSubscriptionTransition string

const (
	// MerchantProgramSubscriptionTransitionActivate moves a pending
	// subscription into active service or resumes a paused or suspended
	// subscription. The coordinator selects activated or resumed from the
	// locked canonical prior status.
	MerchantProgramSubscriptionTransitionActivate MerchantProgramSubscriptionTransition = "activate"

	// MerchantProgramSubscriptionTransitionPause moves an active subscription
	// to paused.
	MerchantProgramSubscriptionTransitionPause MerchantProgramSubscriptionTransition = "pause"

	// MerchantProgramSubscriptionTransitionSuspend moves an active or paused
	// subscription to suspended.
	MerchantProgramSubscriptionTransitionSuspend MerchantProgramSubscriptionTransition = "suspend"

	// MerchantProgramSubscriptionTransitionCancel moves a current subscription
	// to the terminal cancelled state.
	MerchantProgramSubscriptionTransitionCancel MerchantProgramSubscriptionTransition = "cancel"

	// MerchantProgramSubscriptionTransitionExpire moves a current subscription
	// to the terminal expired state.
	MerchantProgramSubscriptionTransitionExpire MerchantProgramSubscriptionTransition = "expire"
)

// MerchantProgramSubscriptionTransitionInput contains the canonical input for
// an atomic status-transition workflow.
type MerchantProgramSubscriptionTransitionInput struct {
	SubscriptionID uuid.UUID
	Transition     MerchantProgramSubscriptionTransition
	Note           *string
	PerformedBy    *uuid.UUID
}

// MerchantProgramSubscriptionPlanChangeInput contains the canonical input for
// an atomic subscription-plan-change workflow.
type MerchantProgramSubscriptionPlanChangeInput struct {
	SubscriptionID uuid.UUID
	PlanID         uuid.UUID
	Note           *string
	PerformedBy    *uuid.UUID
}

// validateMerchantProgramSubscriptionLifecycleService validates the complete
// dependency set required by lifecycle transaction coordination.
//
// The coordinator requires the canonical service configuration and positive
// DB timeout, the subscription model's database pool and logger, and the event
// model's transaction-backed dependencies. The event model's own pool is not
// required because event insertion occurs through the caller-owned transaction.
func validateMerchantProgramSubscriptionLifecycleService(
	s *Service,
) error {
	if err := s.validate(); err != nil {
		return err
	}

	if s.Models.MerchantProgramSubscription.DB == nil {
		return fmt.Errorf(
			"%w: merchant program subscription model database pool is nil",
			ErrInvalidServiceConfiguration,
		)
	}
	if s.Models.MerchantProgramSubscription.Logger == nil {
		return fmt.Errorf(
			"%w: merchant program subscription model logger is nil",
			ErrInvalidServiceConfiguration,
		)
	}

	if err := validateMerchantProgramSubscriptionEventTxService(s); err != nil {
		return err
	}

	return nil
}

func validateMerchantProgramSubscriptionLifecycleActor(
	performedBy *uuid.UUID,
) error {
	if performedBy != nil && *performedBy == uuid.Nil {
		return errors.New(
			"merchant program subscription performed-by ID must not be a nil UUID",
		)
	}

	return nil
}

func rollbackMerchantProgramSubscriptionLifecycleTx(
	tx pgx.Tx,
) {
	if tx == nil {
		return
	}

	// Rollback must remain best effort. pgx returns pgx.ErrTxClosed after a
	// successful commit; that condition requires no further handling.
	_ = tx.Rollback(context.Background())
}

func commitMerchantProgramSubscriptionLifecycleTx(
	ctx context.Context,
	tx pgx.Tx,
) error {
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf(
			"commit merchant program subscription lifecycle transaction: %w",
			err,
		)
	}

	return nil
}

func invalidMerchantProgramSubscriptionTransition(
	transition MerchantProgramSubscriptionTransition,
	status data.MerchantProgramSubscriptionStatus,
) error {
	return fmt.Errorf(
		"%w: cannot %s subscription from %s status",
		ErrInvalidMerchantProgramSubscriptionTransition,
		transition,
		status,
	)
}

// CreateMerchantProgramSubscriptionInternal creates a subscription and records
// its immutable created event in one transaction.
func (s *Service) CreateMerchantProgramSubscriptionInternal(
	ctx context.Context,
	input MerchantProgramSubscriptionCreateInput,
) (*data.MerchantProgramSubscription, error) {
	if ctx == nil {
		return nil, ErrNilContext
	}
	if err := validateMerchantProgramSubscriptionLifecycleService(s); err != nil {
		return nil, err
	}
	if input.Subscription == nil {
		return nil, errors.New(
			"merchant program subscription is required",
		)
	}
	if err := validateMerchantProgramSubscriptionLifecycleActor(
		input.PerformedBy,
	); err != nil {
		return nil, err
	}

	operationCtx, cancel :=
		context.WithTimeout(ctx, s.Cfg.DBTimeout)
	defer cancel()

	logger := s.Logger.
		GetLoggerWithContextFromContext(operationCtx).
		WithFunctionName(
			"CreateMerchantProgramSubscriptionInternal",
		)

	tx, err :=
		s.Models.MerchantProgramSubscription.DB.Begin(
			operationCtx,
		)
	if err != nil {
		logger.Error(
			"Begin merchant program subscription create transaction failed",
			"error",
			err,
		)

		return nil, fmt.Errorf(
			"begin merchant program subscription create transaction: %w",
			err,
		)
	}
	defer rollbackMerchantProgramSubscriptionLifecycleTx(tx)

	if err :=
		s.Models.
			MerchantProgramSubscription.
			InsertTx(
				operationCtx,
				tx,
				input.Subscription,
			); err != nil {
		return nil, fmt.Errorf(
			"create merchant program subscription: %w",
			err,
		)
	}

	if _, err :=
		s.RecordMerchantProgramSubscriptionEventTxInternal(
			operationCtx,
			tx,
			MerchantProgramSubscriptionEventRecordInput{
				SubscriptionID: input.Subscription.ID,
				EventType: data.
					MerchantProgramSubscriptionEventCreated,
				Note:        input.Note,
				PerformedBy: input.PerformedBy,
			},
		); err != nil {
		return nil, fmt.Errorf(
			"record merchant program subscription created event: %w",
			err,
		)
	}

	if err :=
		commitMerchantProgramSubscriptionLifecycleTx(
			operationCtx,
			tx,
		); err != nil {
		return nil, err
	}

	logger.Info(
		"Merchant program subscription created with lifecycle event",
		"subscription_id",
		input.Subscription.ID,
		"merchant_id",
		input.Subscription.MerchantID,
		"plan_id",
		input.Subscription.PlanID,
	)

	return input.Subscription, nil
}

// TransitionMerchantProgramSubscriptionInternal performs one valid canonical
// status transition and records its corresponding event in one transaction.
//
// The subscription row is locked before the transition decision. The locked
// canonical prior status determines whether the transition is valid and, for
// activation, whether the corresponding event is activated or resumed.
func (s *Service) TransitionMerchantProgramSubscriptionInternal(
	ctx context.Context,
	input MerchantProgramSubscriptionTransitionInput,
) error {
	if ctx == nil {
		return ErrNilContext
	}
	if err := validateMerchantProgramSubscriptionLifecycleService(s); err != nil {
		return err
	}
	if input.SubscriptionID == uuid.Nil {
		return errors.New(
			"merchant program subscription ID is required",
		)
	}
	if err := validateMerchantProgramSubscriptionLifecycleActor(
		input.PerformedBy,
	); err != nil {
		return err
	}

	operationCtx, cancel :=
		context.WithTimeout(ctx, s.Cfg.DBTimeout)
	defer cancel()

	logger := s.Logger.
		GetLoggerWithContextFromContext(operationCtx).
		WithFunctionName(
			"TransitionMerchantProgramSubscriptionInternal",
		)

	tx, err :=
		s.Models.MerchantProgramSubscription.DB.Begin(
			operationCtx,
		)
	if err != nil {
		logger.Error(
			"Begin merchant program subscription transition transaction failed",
			"error",
			err,
			"subscription_id",
			input.SubscriptionID,
		)

		return fmt.Errorf(
			"begin merchant program subscription transition transaction: %w",
			err,
		)
	}
	defer rollbackMerchantProgramSubscriptionLifecycleTx(tx)

	current, err :=
		s.Models.
			MerchantProgramSubscription.
			GetByIDForUpdateTx(
				operationCtx,
				tx,
				input.SubscriptionID,
			)
	if err != nil {
		return fmt.Errorf(
			"lock merchant program subscription for transition: %w",
			err,
		)
	}
	if current == nil {
		return fmt.Errorf(
			"merchant program subscription not found or already deleted: %s",
			input.SubscriptionID,
		)
	}

	eventType, err :=
		transitionMerchantProgramSubscriptionTx(
			operationCtx,
			s,
			tx,
			current,
			input.Transition,
		)
	if err != nil {
		return err
	}

	if _, err :=
		s.RecordMerchantProgramSubscriptionEventTxInternal(
			operationCtx,
			tx,
			MerchantProgramSubscriptionEventRecordInput{
				SubscriptionID: input.SubscriptionID,
				EventType:      eventType,
				Note:           input.Note,
				PerformedBy:    input.PerformedBy,
			},
		); err != nil {
		return fmt.Errorf(
			"record merchant program subscription transition event: %w",
			err,
		)
	}

	if err :=
		commitMerchantProgramSubscriptionLifecycleTx(
			operationCtx,
			tx,
		); err != nil {
		return err
	}

	logger.Info(
		"Merchant program subscription transitioned with lifecycle event",
		"subscription_id",
		input.SubscriptionID,
		"prior_status",
		current.Status,
		"transition",
		input.Transition,
		"event_type",
		eventType,
	)

	return nil
}

func transitionMerchantProgramSubscriptionTx(
	ctx context.Context,
	s *Service,
	tx pgx.Tx,
	current *data.MerchantProgramSubscription,
	transition MerchantProgramSubscriptionTransition,
) (data.MerchantProgramSubscriptionEventType, error) {
	if current == nil {
		return "", errors.New(
			"merchant program subscription current state is required",
		)
	}

	current.Status =
		data.NormalizeMerchantProgramSubscriptionStatus(
			current.Status,
		)

	switch transition {
	case MerchantProgramSubscriptionTransitionActivate:
		var eventType data.MerchantProgramSubscriptionEventType

		switch current.Status {
		case data.SubscriptionStatusPending:
			eventType =
				data.MerchantProgramSubscriptionEventActivated

		case data.SubscriptionStatusPaused,
			data.SubscriptionStatusSuspended:
			eventType =
				data.MerchantProgramSubscriptionEventResumed

		default:
			return "",
				invalidMerchantProgramSubscriptionTransition(
					transition,
					current.Status,
				)
		}

		if err :=
			s.Models.
				MerchantProgramSubscription.
				ActivateTx(
					ctx,
					tx,
					current.ID,
				); err != nil {
			return "", fmt.Errorf(
				"activate merchant program subscription: %w",
				err,
			)
		}

		return eventType, nil

	case MerchantProgramSubscriptionTransitionPause:
		if current.Status != data.SubscriptionStatusActive {
			return "",
				invalidMerchantProgramSubscriptionTransition(
					transition,
					current.Status,
				)
		}

		if err :=
			s.Models.
				MerchantProgramSubscription.
				PauseTx(
					ctx,
					tx,
					current.ID,
				); err != nil {
			return "", fmt.Errorf(
				"pause merchant program subscription: %w",
				err,
			)
		}

		return data.MerchantProgramSubscriptionEventPaused, nil

	case MerchantProgramSubscriptionTransitionSuspend:
		switch current.Status {
		case data.SubscriptionStatusActive,
			data.SubscriptionStatusPaused:
		default:
			return "",
				invalidMerchantProgramSubscriptionTransition(
					transition,
					current.Status,
				)
		}

		if err :=
			s.Models.
				MerchantProgramSubscription.
				SuspendTx(
					ctx,
					tx,
					current.ID,
				); err != nil {
			return "", fmt.Errorf(
				"suspend merchant program subscription: %w",
				err,
			)
		}

		return data.MerchantProgramSubscriptionEventSuspended, nil

	case MerchantProgramSubscriptionTransitionCancel:
		switch current.Status {
		case data.SubscriptionStatusPending,
			data.SubscriptionStatusActive,
			data.SubscriptionStatusPaused,
			data.SubscriptionStatusSuspended:
		default:
			return "",
				invalidMerchantProgramSubscriptionTransition(
					transition,
					current.Status,
				)
		}

		if err :=
			s.Models.
				MerchantProgramSubscription.
				CancelTx(
					ctx,
					tx,
					current.ID,
				); err != nil {
			return "", fmt.Errorf(
				"cancel merchant program subscription: %w",
				err,
			)
		}

		return data.MerchantProgramSubscriptionEventCancelled, nil

	case MerchantProgramSubscriptionTransitionExpire:
		switch current.Status {
		case data.SubscriptionStatusPending,
			data.SubscriptionStatusActive,
			data.SubscriptionStatusPaused,
			data.SubscriptionStatusSuspended:
		default:
			return "",
				invalidMerchantProgramSubscriptionTransition(
					transition,
					current.Status,
				)
		}

		if err :=
			s.Models.
				MerchantProgramSubscription.
				ExpireTx(
					ctx,
					tx,
					current.ID,
				); err != nil {
			return "", fmt.Errorf(
				"expire merchant program subscription: %w",
				err,
			)
		}

		return data.MerchantProgramSubscriptionEventExpired, nil

	default:
		return "", fmt.Errorf(
			"%w: %s",
			ErrUnsupportedMerchantProgramSubscriptionTransition,
			transition,
		)
	}
}

// ChangeMerchantProgramSubscriptionPlanInternal changes the canonical plan and
// records an immutable plan_changed event in one transaction.
//
// The subscription is locked before its current plan is compared with the
// requested plan. A same-plan request is rejected without mutation or event
// insertion.
func (s *Service) ChangeMerchantProgramSubscriptionPlanInternal(
	ctx context.Context,
	input MerchantProgramSubscriptionPlanChangeInput,
) error {
	if ctx == nil {
		return ErrNilContext
	}
	if err := validateMerchantProgramSubscriptionLifecycleService(s); err != nil {
		return err
	}
	if input.SubscriptionID == uuid.Nil {
		return errors.New(
			"merchant program subscription ID is required",
		)
	}
	if input.PlanID == uuid.Nil {
		return errors.New(
			"merchant program subscription plan ID is required",
		)
	}
	if err := validateMerchantProgramSubscriptionLifecycleActor(
		input.PerformedBy,
	); err != nil {
		return err
	}

	operationCtx, cancel :=
		context.WithTimeout(ctx, s.Cfg.DBTimeout)
	defer cancel()

	logger := s.Logger.
		GetLoggerWithContextFromContext(operationCtx).
		WithFunctionName(
			"ChangeMerchantProgramSubscriptionPlanInternal",
		)

	tx, err :=
		s.Models.MerchantProgramSubscription.DB.Begin(
			operationCtx,
		)
	if err != nil {
		logger.Error(
			"Begin merchant program subscription plan-change transaction failed",
			"error",
			err,
			"subscription_id",
			input.SubscriptionID,
			"plan_id",
			input.PlanID,
		)

		return fmt.Errorf(
			"begin merchant program subscription plan-change transaction: %w",
			err,
		)
	}
	defer rollbackMerchantProgramSubscriptionLifecycleTx(tx)

	current, err :=
		s.Models.
			MerchantProgramSubscription.
			GetByIDForUpdateTx(
				operationCtx,
				tx,
				input.SubscriptionID,
			)
	if err != nil {
		return fmt.Errorf(
			"lock merchant program subscription for plan change: %w",
			err,
		)
	}
	if current == nil {
		return fmt.Errorf(
			"merchant program subscription not found or already deleted: %s",
			input.SubscriptionID,
		)
	}

	if current.PlanID == input.PlanID {
		return fmt.Errorf(
			"%w: subscription %s already uses plan %s",
			ErrMerchantProgramSubscriptionPlanUnchanged,
			input.SubscriptionID,
			input.PlanID,
		)
	}

	previousPlanID := current.PlanID

	if err :=
		s.Models.
			MerchantProgramSubscription.
			UpdatePlanTx(
				operationCtx,
				tx,
				input.SubscriptionID,
				input.PlanID,
			); err != nil {
		return fmt.Errorf(
			"change merchant program subscription plan: %w",
			err,
		)
	}

	if _, err :=
		s.RecordMerchantProgramSubscriptionEventTxInternal(
			operationCtx,
			tx,
			MerchantProgramSubscriptionEventRecordInput{
				SubscriptionID: input.SubscriptionID,
				EventType: data.
					MerchantProgramSubscriptionEventPlanChanged,
				Note:        input.Note,
				PerformedBy: input.PerformedBy,
			},
		); err != nil {
		return fmt.Errorf(
			"record merchant program subscription plan-change event: %w",
			err,
		)
	}

	if err :=
		commitMerchantProgramSubscriptionLifecycleTx(
			operationCtx,
			tx,
		); err != nil {
		return err
	}

	logger.Info(
		"Merchant program subscription plan changed with lifecycle event",
		"subscription_id",
		input.SubscriptionID,
		"previous_plan_id",
		previousPlanID,
		"plan_id",
		input.PlanID,
	)

	return nil
}
