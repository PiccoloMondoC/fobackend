// Package services contains trusted internal service composition,
// automation support, moderation helpers, and shared internal workflow logic.
//
// focodebase/fobackend/internal/services/merchant_future_offering_billing_periods_internal.go
//
// GTM:
//
//	Layer: 2.6 Internal Services / Future Offering Commercial Domain
//	Release Class: SPINE
//	Reason:
//	  Owns trusted orchestration for just-in-time Merchant Future Offering
//	  Billing Period creation.
//
//	  Billing Period persistence is immutable and transaction-only. This service
//	  composes each authoritative Billing Period creation atomically with its
//	  producer-owned transactional outbox occurrence and provides the synchronous
//	  catch-up capability required to ensure that the presently applicable
//	  accounting window exists after inactivity, restart, or delayed invocation.
//
//	  Calendar arithmetic, period numbering, due-state determination, chronology,
//	  Service Term locking, overlap enforcement, and immutable persistence remain
//	  owned by the data layer and PostgreSQL.
//
//	  Billing Period is independent of Service Period identity and lifecycle.
//	  This service does not know QAE, AHO, fee-calculation, invoice, funding,
//	  payment, settlement, downstream-consumer, broker, or Pub/Sub semantics.
//
// SPINE Rule:
//
//	Keep compiling.
//	Keep production-ready.
//	Preserve just-in-time, non-forecast Billing Period creation.
//	Preserve one atomic commit boundary for each Billing Period and its outbox
//	occurrence.
//	Preserve database-owned calendar, chronology, locking, and due-state rules.
//	Never accept caller-supplied period numbers, dates, cadence, or windows.
//	Never derive Billing Period identity or lifecycle from Service Period rows.
//	Never publish externally or invoke downstream consumers.
//	Never use process-local locking for chronology or uniqueness.
//	Preserve deterministic event identity/versioning and transport neutrality.
//	Block deployment if Billing Period coverage, transactional atomicity,
//	concurrency safety, historical integrity, or domain separation is weakened.
package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/PiccoloMondoC/focodebase/fobackend/internal/data"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

const (
	merchantFutureOfferingBillingPeriodAggregateType = "merchant_future_offering_billing_period"

	merchantFutureOfferingBillingPeriodCreatedEventType = "merchant_future_offering_billing_period.created"

	merchantFutureOfferingBillingPeriodCreatedEventVersion = 1

	merchantFutureOfferingBillingPeriodDateLayout = "2006-01-02"
)

// merchantFutureOfferingBillingPeriodCreatedEventV1 is the version-1 producer
// payload for one authoritative Billing Period creation.
//
// The payload contains producer-owned Billing Period facts only. It contains no
// command for a downstream consumer and no mutable presentation terminology.
type merchantFutureOfferingBillingPeriodCreatedEventV1 struct {
	BillingPeriodID  uuid.UUID `json:"billing_period_id"`
	FutureOfferingID uuid.UUID `json:"future_offering_id"`
	ServiceTermID    uuid.UUID `json:"service_term_id"`
	PeriodNumber     int       `json:"period_number"`
	PeriodStartsOn   string    `json:"period_starts_on"`
	PeriodEndsOn     string    `json:"period_ends_on"`
}

// merchantFutureOfferingBillingPeriodCreation identifies which canonical
// transaction-only data-layer creation primitive the producer workflow invokes.
type merchantFutureOfferingBillingPeriodCreation uint8

const (
	merchantFutureOfferingBillingPeriodCreationFirst merchantFutureOfferingBillingPeriodCreation = iota + 1
	merchantFutureOfferingBillingPeriodCreationNext
)

// newMerchantFutureOfferingBillingPeriodCreatedOutboxInput constructs the
// transport-neutral outbox input for one authoritative Billing Period.
//
// The idempotency key identifies the event occurrence, not an external request.
// Ordinary orchestration retry safety continues to rely on authoritative
// Billing Period chronology, serialization, and database constraints.
func newMerchantFutureOfferingBillingPeriodCreatedOutboxInput(
	period *data.MerchantFutureOfferingBillingPeriod,
) (data.NewOutboxEvent, error) {
	if period == nil ||
		period.ID == uuid.Nil ||
		period.FutureOfferingID == uuid.Nil ||
		period.ServiceTermID == uuid.Nil ||
		period.PeriodNumber <= 0 ||
		period.PeriodStartsOn.IsZero() ||
		period.PeriodEndsOn.IsZero() ||
		!period.PeriodEndsOn.After(period.PeriodStartsOn) {
		return data.NewOutboxEvent{}, fmt.Errorf(
			"%w: created billing period is incomplete",
			ErrMerchantFutureOfferingBillingPeriodInvalidState,
		)
	}

	payload, err := json.Marshal(
		merchantFutureOfferingBillingPeriodCreatedEventV1{
			BillingPeriodID:  period.ID,
			FutureOfferingID: period.FutureOfferingID,
			ServiceTermID:    period.ServiceTermID,
			PeriodNumber:     period.PeriodNumber,
			PeriodStartsOn: period.PeriodStartsOn.Format(
				merchantFutureOfferingBillingPeriodDateLayout,
			),
			PeriodEndsOn: period.PeriodEndsOn.Format(
				merchantFutureOfferingBillingPeriodDateLayout,
			),
		},
	)
	if err != nil {
		return data.NewOutboxEvent{}, fmt.Errorf(
			"marshal merchant future offering billing period created event: %w",
			err,
		)
	}

	return data.NewOutboxEvent{
		AggregateType: merchantFutureOfferingBillingPeriodAggregateType,
		AggregateID:   period.ID,
		EventType:     merchantFutureOfferingBillingPeriodCreatedEventType,
		EventVersion:  merchantFutureOfferingBillingPeriodCreatedEventVersion,
		Payload:       payload,
		IdempotencyKey: fmt.Sprintf(
			"%s:v%d:%s",
			merchantFutureOfferingBillingPeriodCreatedEventType,
			merchantFutureOfferingBillingPeriodCreatedEventVersion,
			period.ID,
		),
	}, nil
}

// createMerchantFutureOfferingBillingPeriodInternal creates exactly one
// authoritative Billing Period and its producer-owned outbox occurrence inside
// one service-owned transaction.
//
// creation selects either the canonical first-period or next-period data-layer
// primitive. Calendar arithmetic, due-state checks, numbering, locking, and
// chronology remain below this service boundary.
func (s *Service) createMerchantFutureOfferingBillingPeriodInternal(
	ctx context.Context,
	futureOfferingID uuid.UUID,
	serviceTermID uuid.UUID,
	creation merchantFutureOfferingBillingPeriodCreation,
	functionName string,
) (*data.MerchantFutureOfferingBillingPeriod, error) {
	if err := s.validate(); err != nil {
		return nil, err
	}

	if ctx == nil {
		return nil, ErrNilContext
	}

	if futureOfferingID == uuid.Nil ||
		serviceTermID == uuid.Nil {
		return nil, fmt.Errorf(
			"%w: future offering id and service term id are required",
			ErrMerchantFutureOfferingBillingPeriodInputInvalid,
		)
	}

	if creation != merchantFutureOfferingBillingPeriodCreationFirst &&
		creation != merchantFutureOfferingBillingPeriodCreationNext {
		return nil, fmt.Errorf(
			"%w: unsupported billing period creation operation",
			ErrMerchantFutureOfferingBillingPeriodInputInvalid,
		)
	}

	ctx, cancel := context.WithTimeout(ctx, s.Cfg.DBTimeout)
	defer cancel()

	logger := s.Logger.
		GetLoggerWithContextFromContext(ctx).
		WithFunctionName(functionName)

	tx, err :=
		s.Models.MerchantFutureOfferingBillingPeriod.BeginTx(ctx)
	if err != nil {
		return nil, fmt.Errorf(
			"begin merchant future offering billing period producer transaction: %w",
			err,
		)
	}

	defer func() {
		if rollbackErr := tx.Rollback(ctx); rollbackErr != nil &&
			!errors.Is(rollbackErr, pgx.ErrTxClosed) {
			logger.Warn(
				"merchant future offering billing period producer transaction rollback failed",
				"future_offering_id", futureOfferingID,
				"service_term_id", serviceTermID,
				"error", rollbackErr,
			)
		}
	}()

	var period *data.MerchantFutureOfferingBillingPeriod

	switch creation {
	case merchantFutureOfferingBillingPeriodCreationFirst:
		period, err =
			s.Models.MerchantFutureOfferingBillingPeriod.CreateFirstPeriodTx(
				ctx,
				tx,
				serviceTermID,
				futureOfferingID,
			)

	case merchantFutureOfferingBillingPeriodCreationNext:
		period, err =
			s.Models.MerchantFutureOfferingBillingPeriod.CreateNextPeriodTx(
				ctx,
				tx,
				serviceTermID,
				futureOfferingID,
			)
	}

	if err != nil {
		return nil,
			translateMerchantFutureOfferingBillingPeriodCreationError(err)
	}

	outboxInput, err :=
		newMerchantFutureOfferingBillingPeriodCreatedOutboxInput(period)
	if err != nil {
		return nil, err
	}

	if _, err := s.Models.OutboxEvent.InsertTx(
		ctx,
		tx,
		outboxInput,
	); err != nil {
		if errors.Is(
			err,
			data.ErrOutboxEventDuplicateIdempotencyKey,
		) {
			return nil, fmt.Errorf(
				"%w: billing_period_id=%s",
				ErrMerchantFutureOfferingBillingPeriodEventConflict,
				period.ID,
			)
		}

		return nil, fmt.Errorf(
			"insert merchant future offering billing period created outbox event: %w",
			err,
		)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf(
			"commit merchant future offering billing period producer transaction: %w",
			err,
		)
	}

	logger.Info(
		"merchant future offering billing period created",
		"billing_period_id", period.ID,
		"future_offering_id", period.FutureOfferingID,
		"service_term_id", period.ServiceTermID,
		"period_number", period.PeriodNumber,
		"period_starts_on", period.PeriodStartsOn,
		"period_ends_on", period.PeriodEndsOn,
	)

	return period, nil
}

// CreateFirstMerchantFutureOfferingBillingPeriodInternal creates period 1 for
// an established Future Offering Service Term and atomically persists the
// producer-owned Billing Period creation event.
//
// No caller-supplied period number or calendar window is accepted.
func (s *Service) CreateFirstMerchantFutureOfferingBillingPeriodInternal(
	ctx context.Context,
	futureOfferingID uuid.UUID,
	serviceTermID uuid.UUID,
) (*data.MerchantFutureOfferingBillingPeriod, error) {
	return s.createMerchantFutureOfferingBillingPeriodInternal(
		ctx,
		futureOfferingID,
		serviceTermID,
		merchantFutureOfferingBillingPeriodCreationFirst,
		"CreateFirstMerchantFutureOfferingBillingPeriodInternal",
	)
}

// CreateNextMerchantFutureOfferingBillingPeriodInternal creates exactly one
// next authoritative Billing Period for an established Future Offering Service
// Term and atomically persists its producer-owned creation event.
//
// PostgreSQL determines whether the next boundary is due.
func (s *Service) CreateNextMerchantFutureOfferingBillingPeriodInternal(
	ctx context.Context,
	futureOfferingID uuid.UUID,
	serviceTermID uuid.UUID,
) (*data.MerchantFutureOfferingBillingPeriod, error) {
	return s.createMerchantFutureOfferingBillingPeriodInternal(
		ctx,
		futureOfferingID,
		serviceTermID,
		merchantFutureOfferingBillingPeriodCreationNext,
		"CreateNextMerchantFutureOfferingBillingPeriodInternal",
	)
}

// EnsureCurrentMerchantFutureOfferingBillingPeriodInternal advances persisted
// Billing Period chronology through every period whose authoritative start
// boundary has already been reached and returns the latest authoritative
// persisted Billing Period for the identified Service Term.
//
// This is the production JIT catch-up capability. It is intentionally
// synchronous and does not create a domain-specific worker. A future common
// Automation Foundation worker, billable-activity workflow, or other trusted
// orchestration may invoke it without changing Billing Period semantics.
//
// Exactly one new Billing Period is committed per producer transaction. This
// keeps write transactions short and ensures every newly created Billing Period
// owns exactly one atomic outbox occurrence.
//
// Existing chronology is read before creation so repeated steady-state calls are
// valid no-op ensures rather than errors. Concurrent advancement is reconciled
// from authoritative persistence; retries continue only when chronology has
// actually advanced.
//
// A nil result with
// ErrMerchantFutureOfferingBillingPeriodCreationNotYetEligible means no Billing
// Period exists and Period 1 has not yet begun.
func (s *Service) EnsureCurrentMerchantFutureOfferingBillingPeriodInternal(
	ctx context.Context,
	futureOfferingID uuid.UUID,
	serviceTermID uuid.UUID,
) (*data.MerchantFutureOfferingBillingPeriod, error) {
	if err := s.validate(); err != nil {
		return nil, err
	}

	if ctx == nil {
		return nil, ErrNilContext
	}

	if futureOfferingID == uuid.Nil ||
		serviceTermID == uuid.Nil {
		return nil, fmt.Errorf(
			"%w: future offering id and service term id are required",
			ErrMerchantFutureOfferingBillingPeriodInputInvalid,
		)
	}

	latest, err :=
		s.Models.MerchantFutureOfferingBillingPeriod.GetLatestForServiceTerm(
			ctx,
			serviceTermID,
			futureOfferingID,
		)
	if err != nil {
		if errors.Is(
			err,
			data.ErrMerchantFutureOfferingBillingPeriodInvalidInput,
		) {
			return nil, fmt.Errorf(
				"%w: %w",
				ErrMerchantFutureOfferingBillingPeriodInputInvalid,
				err,
			)
		}

		return nil, fmt.Errorf(
			"get latest merchant future offering billing period for catch-up: %w",
			err,
		)
	}

	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		if latest != nil {
			if latest.PeriodNumber <= 0 ||
				latest.PeriodNumber >
					data.MerchantFutureOfferingServiceTermMaxDurationMonths {
				return nil, fmt.Errorf(
					"%w: persisted billing period number is outside the engineering envelope",
					ErrMerchantFutureOfferingBillingPeriodInvalidState,
				)
			}

			if latest.PeriodNumber ==
				data.MerchantFutureOfferingServiceTermMaxDurationMonths {
				return latest, nil
			}
		}

		previousPeriodNumber := 0
		if latest != nil {
			previousPeriodNumber = latest.PeriodNumber
		}

		var period *data.MerchantFutureOfferingBillingPeriod

		if latest == nil {
			period, err =
				s.CreateFirstMerchantFutureOfferingBillingPeriodInternal(
					ctx,
					futureOfferingID,
					serviceTermID,
				)
		} else {
			period, err =
				s.CreateNextMerchantFutureOfferingBillingPeriodInternal(
					ctx,
					futureOfferingID,
					serviceTermID,
				)
		}

		switch {
		case err == nil:
			if period == nil ||
				period.PeriodNumber <= previousPeriodNumber {
				return nil, fmt.Errorf(
					"%w: billing period catch-up did not advance chronology",
					ErrMerchantFutureOfferingBillingPeriodInvalidState,
				)
			}

			latest = period
			continue

		case errors.Is(
			err,
			ErrMerchantFutureOfferingBillingPeriodCreationNotYetEligible,
		):
			if latest != nil {
				return latest, nil
			}
			return nil, err

		case errors.Is(
			err,
			ErrMerchantFutureOfferingBillingPeriodTermExhausted,
		):
			if latest != nil {
				return latest, nil
			}
			return nil, err

		case errors.Is(
			err,
			ErrMerchantFutureOfferingBillingPeriodAlreadyExists,
		),
			errors.Is(
				err,
				ErrMerchantFutureOfferingBillingPeriodConcurrentCreation,
			):
			refreshed, refreshErr :=
				s.Models.MerchantFutureOfferingBillingPeriod.
					GetLatestForServiceTerm(
						ctx,
						serviceTermID,
						futureOfferingID,
					)
			if refreshErr != nil {
				return nil, fmt.Errorf(
					"refresh latest merchant future offering billing period after concurrent creation: %w",
					refreshErr,
				)
			}

			if refreshed == nil ||
				refreshed.PeriodNumber <= previousPeriodNumber {
				// A reported creation conflict without observable chronology
				// progress is not retried blindly. Immediate retry would only
				// spin against unchanged authoritative state.
				return nil, err
			}

			latest = refreshed
			continue

		default:
			return nil, err
		}
	}
}

// translateMerchantFutureOfferingBillingPeriodCreationError translates
// persistence-layer creation failures into the small stable set of errors
// genuinely owned by Billing Period service orchestration.
//
// The underlying persistence error remains wrapped so errors.Is continues to
// work against both layers where required.
func translateMerchantFutureOfferingBillingPeriodCreationError(
	err error,
) error {
	switch {
	case errors.Is(
		err,
		data.ErrMerchantFutureOfferingBillingPeriodInvalidInput,
	):
		return fmt.Errorf(
			"%w: %w",
			ErrMerchantFutureOfferingBillingPeriodInputInvalid,
			err,
		)

	case errors.Is(
		err,
		data.ErrMerchantFutureOfferingBillingPeriodServiceTermNotFound,
	):
		return fmt.Errorf(
			"%w: %w",
			ErrMerchantFutureOfferingBillingPeriodServiceTermScopeMismatch,
			err,
		)

	case errors.Is(
		err,
		data.ErrMerchantFutureOfferingBillingPeriodNumberConflict,
	):
		return fmt.Errorf(
			"%w: %w",
			ErrMerchantFutureOfferingBillingPeriodAlreadyExists,
			err,
		)

	case errors.Is(
		err,
		data.ErrMerchantFutureOfferingBillingPeriodTermExhausted,
	):
		return fmt.Errorf(
			"%w: %w",
			ErrMerchantFutureOfferingBillingPeriodTermExhausted,
			err,
		)

	case errors.Is(
		err,
		data.ErrMerchantFutureOfferingBillingPeriodNotYetDue,
	):
		return fmt.Errorf(
			"%w: %w",
			ErrMerchantFutureOfferingBillingPeriodCreationNotYetEligible,
			err,
		)

	case errors.Is(
		err,
		data.ErrMerchantFutureOfferingBillingPeriodOverlap,
	),
		errors.Is(
			err,
			data.ErrMerchantFutureOfferingBillingPeriodPersistenceConflict,
		):
		return fmt.Errorf(
			"%w: %w",
			ErrMerchantFutureOfferingBillingPeriodConcurrentCreation,
			err,
		)

	case errors.Is(
		err,
		data.ErrMerchantFutureOfferingBillingPeriodInvalidState,
	),
		errors.Is(
			err,
			data.ErrMerchantFutureOfferingBillingPeriodInvalidSchedule,
		):
		return fmt.Errorf(
			"%w: %w",
			ErrMerchantFutureOfferingBillingPeriodInvalidState,
			err,
		)

	case errors.Is(err, context.Canceled),
		errors.Is(err, context.DeadlineExceeded):
		return err

	default:
		return fmt.Errorf(
			"merchant future offering billing period creation: %w",
			err,
		)
	}
}
