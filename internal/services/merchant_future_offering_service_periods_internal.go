// Package services contains trusted internal service composition,
// automation support, moderation helpers, and shared internal workflow logic.
//
// sdworkspace/sdbackend/internal/services/merchant_future_offering_service_periods_internal.go
//
// GTM:
//
//	Layer: 2.6 Internal Services / Future Offering Commercial Domain
//	Release Class: SPINE
//	Reason:
//	  Orchestrates just-in-time creation of authoritative Future Offering
//	  Service Periods.
//
//	  Service Period duration, cadence, and calendar-boundary semantics are
//	  Engineering invariants. Each ordinary Service Period is one calendar-month
//	  performance window derived from the original authoritative Service Term
//	  anchor according to STCD. Only the final Service Period may be shorter
//	  where necessary to terminate exactly at the Service Term end boundary.
//
//	  Persisted Service Periods represent actual or currently performing service.
//	  They are not forecasts and are never pre-generated as a future schedule.
//
//	  Each Service Period creation commits atomically with its producer-owned
//	  transactional outbox event. The producer knows nothing about downstream
//	  consumers or transport.
//
// SPINE Rule:
//
//	Keep compiling.
//	Keep production-ready.
//	Preserve one-calendar-month Service Period cadence as an Engineering
//	invariant.
//	Preserve STCD anchor-derived calendar arithmetic.
//	Preserve just-in-time creation: create one authoritative period at a time.
//	Never pre-generate future Service Periods.
//	Never rewrite, supersede, shorten, extend, repartition, update, or delete an
//	established Service Period.
//	Never place Service Period cadence or calendar semantics in Administration
//	configuration.
//	Preserve atomic Service Period creation + producer-owned outbox insertion.
//	Keep Billing Period, Payment Period, invoice, payment, settlement, transport,
//	and downstream-consumer behavior outside this capability.
//	Do not publish external messages while holding the domain transaction.
//	Block deployment if transactional atomicity, JiT semantics, calendar
//	correctness, historical integrity, or producer/consumer separation is
//	weakened.
package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/PiccoloMondoC/sdworkspace/sdbackend/internal/data"

	"github.com/google/uuid"
)

const (
	merchantFutureOfferingServicePeriodEventAggregateType = "merchant_future_offering_service_period"

	eventTypeMerchantFutureOfferingServicePeriodEstablished = "merchant_future_offering.service_period.established"

	merchantFutureOfferingServicePeriodEventVersion = 1

	merchantFutureOfferingServicePeriodDateLayout = "2006-01-02"
)

func servicePeriodDaysInMonth(
	year int,
	month time.Month,
) int {
	return time.Date(
		year,
		month+1,
		0,
		0,
		0,
		0,
		0,
		time.UTC,
	).Day()
}

// normalizeServicePeriodPlanningDate converts a domain date into canonical
// UTC-midnight representation without changing its Year/Month/Day fields.
//
// Service Term and Service Period boundaries are domain dates, not elapsed-time
// instants. The input location therefore must not shift the represented
// calendar date.
func normalizeServicePeriodPlanningDate(
	value time.Time,
) time.Time {
	return time.Date(
		value.Year(),
		value.Month(),
		value.Day(),
		0,
		0,
		0,
		0,
		time.UTC,
	)
}

// addServicePeriodCalendarMonths advances from one authoritative Service Term
// anchor according to STCD.
//
// Month-end remains month-end. An ordinary day-of-month remains its original
// anniversary anchor wherever the target month can represent that day. A short
// month substitutes only its own month-end and does not become a new anchor.
//
// Callers must always derive boundaries from the original Service Term anchor,
// never recursively from a previously calculated boundary.
func addServicePeriodCalendarMonths(
	anchor time.Time,
	months int,
) time.Time {
	anchor = normalizeServicePeriodPlanningDate(anchor)

	year, month, day := anchor.Date()

	monthEndAnchor :=
		day == servicePeriodDaysInMonth(year, month)

	totalMonths := int(month) - 1 + months

	targetYear := year + totalMonths/12
	targetMonthIndex := totalMonths % 12

	if targetMonthIndex < 0 {
		targetMonthIndex += 12
		targetYear--
	}

	targetMonth := time.Month(targetMonthIndex + 1)

	targetMonthEnd :=
		servicePeriodDaysInMonth(
			targetYear,
			targetMonth,
		)

	targetDay := day

	if monthEndAnchor || targetDay > targetMonthEnd {
		targetDay = targetMonthEnd
	}

	return time.Date(
		targetYear,
		targetMonth,
		targetDay,
		0,
		0,
		0,
		0,
		time.UTC,
	)
}

// deriveMerchantFutureOfferingServicePeriodWindow derives one canonical
// Service Period window from the original Service Term anchor.
//
// periodNumber is one-based:
//
//	Period 1 => [anchor + 0 months, anchor + 1 month)
//	Period 2 => [anchor + 1 month,  anchor + 2 months)
//	...
//
// The final end boundary is capped at termEndsOn.
func deriveMerchantFutureOfferingServicePeriodWindow(
	termStartsOn time.Time,
	termEndsOn time.Time,
	periodNumber int,
) (
	data.MerchantFutureOfferingServicePeriodWindow,
	error,
) {
	if periodNumber <= 0 {
		return data.MerchantFutureOfferingServicePeriodWindow{},
			ErrMerchantFutureOfferingServicePeriodInvalidState
	}

	termStartsOn =
		normalizeServicePeriodPlanningDate(termStartsOn)

	termEndsOn =
		normalizeServicePeriodPlanningDate(termEndsOn)

	if !termEndsOn.After(termStartsOn) {
		return data.MerchantFutureOfferingServicePeriodWindow{},
			ErrMerchantFutureOfferingServicePeriodInvalidState
	}

	startsOn :=
		addServicePeriodCalendarMonths(
			termStartsOn,
			periodNumber-1,
		)

	if !startsOn.Before(termEndsOn) {
		return data.MerchantFutureOfferingServicePeriodWindow{},
			ErrMerchantFutureOfferingServicePeriodServiceTermComplete
	}

	endsOn :=
		addServicePeriodCalendarMonths(
			termStartsOn,
			periodNumber,
		)

	if endsOn.After(termEndsOn) {
		endsOn = termEndsOn
	}

	if !endsOn.After(startsOn) {
		return data.MerchantFutureOfferingServicePeriodWindow{},
			ErrMerchantFutureOfferingServicePeriodInvalidState
	}

	return data.MerchantFutureOfferingServicePeriodWindow{
		StartsOn: startsOn,
		EndsOn:   endsOn,
	}, nil
}

func currentServicePeriodDomainDate() time.Time {
	return normalizeServicePeriodPlanningDate(
		time.Now().UTC(),
	)
}

func (s *Service) loadEstablishedMerchantFutureOfferingServiceTermForPlanning(
	ctx context.Context,
	serviceTermID uuid.UUID,
	futureOfferingID uuid.UUID,
) (*data.MerchantFutureOfferingServiceTerm, error) {
	term, err :=
		s.Models.
			MerchantFutureOfferingServiceTerm.
			GetByIDForFutureOffering(
				ctx,
				serviceTermID,
				futureOfferingID,
			)
	if err != nil {
		if errors.Is(
			err,
			data.ErrMerchantFutureOfferingServiceTermNotFound,
		) {
			return nil,
				ErrMerchantFutureOfferingServicePeriodServiceTermScopeMismatch
		}

		return nil, fmt.Errorf(
			"load merchant future offering service term for Service Period planning: %w",
			err,
		)
	}

	if term == nil ||
		term.TermStatus !=
			data.MerchantFutureOfferingServiceTermStatusEstablished ||
		term.TermStartsOn == nil ||
		term.TermEndsOn == nil {
		return nil,
			ErrMerchantFutureOfferingServicePeriodServiceTermNotEstablished
	}

	startsOn :=
		normalizeServicePeriodPlanningDate(
			*term.TermStartsOn,
		)

	endsOn :=
		normalizeServicePeriodPlanningDate(
			*term.TermEndsOn,
		)

	if !endsOn.After(startsOn) {
		return nil,
			ErrMerchantFutureOfferingServicePeriodInvalidState
	}

	return term, nil
}

func latestMerchantFutureOfferingServicePeriod(
	periods []*data.MerchantFutureOfferingServicePeriod,
) (*data.MerchantFutureOfferingServicePeriod, error) {
	if len(periods) == 0 {
		return nil, nil
	}

	latest := periods[len(periods)-1]

	if latest == nil ||
		latest.ID == uuid.Nil ||
		latest.ServiceTermID == uuid.Nil ||
		latest.FutureOfferingID == uuid.Nil ||
		latest.PeriodNumber <= 0 ||
		!latest.PeriodEndsOn.After(latest.PeriodStartsOn) {
		return nil,
			ErrMerchantFutureOfferingServicePeriodInvalidState
	}

	return latest, nil
}

type merchantFutureOfferingServicePeriodEstablishedPayload struct {
	ServicePeriodID  uuid.UUID `json:"service_period_id"`
	ServiceTermID    uuid.UUID `json:"service_term_id"`
	FutureOfferingID uuid.UUID `json:"future_offering_id"`

	PeriodNumber int    `json:"period_number"`
	StartsOn     string `json:"starts_on"`
	EndsOn       string `json:"ends_on"`
}

func newMerchantFutureOfferingServicePeriodEstablishedEvent(
	period *data.MerchantFutureOfferingServicePeriod,
) (data.NewOutboxEvent, error) {
	if period == nil ||
		period.ID == uuid.Nil ||
		period.ServiceTermID == uuid.Nil ||
		period.FutureOfferingID == uuid.Nil ||
		period.PeriodNumber <= 0 ||
		!period.PeriodEndsOn.After(period.PeriodStartsOn) {
		return data.NewOutboxEvent{},
			ErrMerchantFutureOfferingServicePeriodInvalidState
	}

	payload, err :=
		json.Marshal(
			merchantFutureOfferingServicePeriodEstablishedPayload{
				ServicePeriodID:  period.ID,
				ServiceTermID:    period.ServiceTermID,
				FutureOfferingID: period.FutureOfferingID,
				PeriodNumber:     period.PeriodNumber,
				StartsOn: normalizeServicePeriodPlanningDate(
					period.PeriodStartsOn,
				).Format(
					merchantFutureOfferingServicePeriodDateLayout,
				),
				EndsOn: normalizeServicePeriodPlanningDate(
					period.PeriodEndsOn,
				).Format(
					merchantFutureOfferingServicePeriodDateLayout,
				),
			},
		)
	if err != nil {
		return data.NewOutboxEvent{}, fmt.Errorf(
			"marshal merchant future offering Service Period established event: %w",
			err,
		)
	}

	return data.NewOutboxEvent{
		AggregateType: merchantFutureOfferingServicePeriodEventAggregateType,
		AggregateID:   period.ID,

		EventType:    eventTypeMerchantFutureOfferingServicePeriodEstablished,
		EventVersion: merchantFutureOfferingServicePeriodEventVersion,

		Payload: payload,

		// Service Term + period number is the stable semantic identity of the
		// establishment occurrence. It does not depend on downstream transport
		// or consumer identity.
		IdempotencyKey: fmt.Sprintf(
			"merchant-future-offering-service-period-established:%s:%d",
			period.ServiceTermID,
			period.PeriodNumber,
		),
	}, nil
}

// MerchantFutureOfferingServicePeriodEstablishment is the canonical result of
// one just-in-time Service Period establishment.
type MerchantFutureOfferingServicePeriodEstablishment struct {
	Period *data.MerchantFutureOfferingServicePeriod
}

// EstablishFirstMerchantFutureOfferingServicePeriodInternal creates Period 1
// for the currently established Service Term.
//
// Exactly one immutable Service Period is created. No future schedule is
// generated.
//
// Creation is not allowed before the Service Term start date. The Service
// Period row and producer-owned outbox event commit atomically.
func (s *Service) EstablishFirstMerchantFutureOfferingServicePeriodInternal(
	ctx context.Context,
	futureOfferingID uuid.UUID,
	serviceTermID uuid.UUID,
) (*MerchantFutureOfferingServicePeriodEstablishment, error) {
	if ctx == nil {
		return nil, ErrNilContext
	}

	if err := s.validate(); err != nil {
		return nil, err
	}

	if futureOfferingID == uuid.Nil ||
		serviceTermID == uuid.Nil {
		return nil,
			ErrMerchantFutureOfferingServicePeriodInputInvalid
	}

	term, err :=
		s.loadEstablishedMerchantFutureOfferingServiceTermForPlanning(
			ctx,
			serviceTermID,
			futureOfferingID,
		)
	if err != nil {
		return nil, err
	}

	termStartsOn :=
		normalizeServicePeriodPlanningDate(
			*term.TermStartsOn,
		)

	termEndsOn :=
		normalizeServicePeriodPlanningDate(
			*term.TermEndsOn,
		)

	today :=
		currentServicePeriodDomainDate()

	if today.Before(termStartsOn) {
		return nil,
			ErrMerchantFutureOfferingServicePeriodCreationNotYetEligible
	}

	if !today.Before(termEndsOn) {
		return nil,
			ErrMerchantFutureOfferingServicePeriodInvalidState
	}

	window, err :=
		deriveMerchantFutureOfferingServicePeriodWindow(
			termStartsOn,
			termEndsOn,
			1,
		)
	if err != nil {
		return nil, err
	}

	tx, err :=
		s.Models.
			MerchantFutureOfferingServicePeriod.
			BeginTx(ctx)
	if err != nil {
		return nil, fmt.Errorf(
			"begin first Service Period establishment transaction: %w",
			err,
		)
	}

	defer tx.Rollback(ctx)

	period, err :=
		s.Models.
			MerchantFutureOfferingServicePeriod.
			CreateFirstPeriodTx(
				ctx,
				tx,
				serviceTermID,
				futureOfferingID,
				window,
			)
	if err != nil {
		return nil, err
	}

	event, err :=
		newMerchantFutureOfferingServicePeriodEstablishedEvent(
			period,
		)
	if err != nil {
		return nil, err
	}

	if _, err := s.Models.OutboxEvent.InsertTx(
		ctx,
		tx,
		event,
	); err != nil {
		return nil, fmt.Errorf(
			"record Service Period established outbox event: %w",
			err,
		)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf(
			"commit first Service Period establishment: %w",
			err,
		)
	}

	s.Logger.
		GetLoggerWithContextFromContext(ctx).
		WithFunctionName(
			"EstablishFirstMerchantFutureOfferingServicePeriodInternal",
		).
		Info(
			"first service period established",
			"service_period_id", period.ID,
			"future_offering_id", period.FutureOfferingID,
			"service_term_id", period.ServiceTermID,
			"period_number", period.PeriodNumber,
			"period_starts_on", period.PeriodStartsOn,
			"period_ends_on", period.PeriodEndsOn,
		)

	return &MerchantFutureOfferingServicePeriodEstablishment{
		Period: period,
	}, nil
}

// EstablishNextMerchantFutureOfferingServicePeriodInternal creates exactly one
// next just-in-time Service Period for the currently established Service Term.
//
// The next boundary is always derived from the original authoritative Service
// Term anchor and the next deterministic period number. The previous period's
// end is used only as a chronology assertion; it never becomes the calendar
// anchor.
//
// Creation before the preceding Service Period has completed is rejected.
func (s *Service) EstablishNextMerchantFutureOfferingServicePeriodInternal(
	ctx context.Context,
	futureOfferingID uuid.UUID,
	serviceTermID uuid.UUID,
) (*MerchantFutureOfferingServicePeriodEstablishment, error) {
	if ctx == nil {
		return nil, ErrNilContext
	}

	if err := s.validate(); err != nil {
		return nil, err
	}

	if futureOfferingID == uuid.Nil ||
		serviceTermID == uuid.Nil {
		return nil,
			ErrMerchantFutureOfferingServicePeriodInputInvalid
	}

	term, err :=
		s.loadEstablishedMerchantFutureOfferingServiceTermForPlanning(
			ctx,
			serviceTermID,
			futureOfferingID,
		)
	if err != nil {
		return nil, err
	}

	periods, err :=
		s.Models.
			MerchantFutureOfferingServicePeriod.
			ListForServiceTerm(
				ctx,
				serviceTermID,
				futureOfferingID,
			)
	if err != nil {
		return nil, fmt.Errorf(
			"load Service Period chronology for JiT continuation: %w",
			err,
		)
	}

	latest, err :=
		latestMerchantFutureOfferingServicePeriod(
			periods,
		)
	if err != nil {
		return nil, err
	}

	if latest == nil {
		return nil,
			ErrMerchantFutureOfferingServicePeriodInvalidState
	}

	termStartsOn :=
		normalizeServicePeriodPlanningDate(
			*term.TermStartsOn,
		)

	termEndsOn :=
		normalizeServicePeriodPlanningDate(
			*term.TermEndsOn,
		)

	latestEndsOn :=
		normalizeServicePeriodPlanningDate(
			latest.PeriodEndsOn,
		)

	if !latestEndsOn.Before(termEndsOn) {
		return nil,
			ErrMerchantFutureOfferingServicePeriodServiceTermComplete
	}

	today :=
		currentServicePeriodDomainDate()

	if today.Before(latestEndsOn) {
		return nil,
			ErrMerchantFutureOfferingServicePeriodCreationNotYetEligible
	}

	if latest.PeriodNumber >=
		data.MerchantFutureOfferingServiceTermMaxDurationMonths {
		return nil,
			ErrMerchantFutureOfferingServicePeriodInvalidState
	}

	nextPeriodNumber :=
		latest.PeriodNumber + 1

	window, err :=
		deriveMerchantFutureOfferingServicePeriodWindow(
			termStartsOn,
			termEndsOn,
			nextPeriodNumber,
		)
	if err != nil {
		return nil, err
	}

	// STCD requires the boundary to be derived from the original Service Term
	// anchor. Existing persisted chronology must agree with that derivation.
	if !normalizeServicePeriodPlanningDate(
		window.StartsOn,
	).Equal(
		latestEndsOn,
	) {
		return nil,
			ErrMerchantFutureOfferingServicePeriodInvalidState
	}

	tx, err :=
		s.Models.
			MerchantFutureOfferingServicePeriod.
			BeginTx(ctx)
	if err != nil {
		return nil, fmt.Errorf(
			"begin next Service Period establishment transaction: %w",
			err,
		)
	}

	defer tx.Rollback(ctx)

	period, err :=
		s.Models.
			MerchantFutureOfferingServicePeriod.
			CreateNextPeriodTx(
				ctx,
				tx,
				serviceTermID,
				futureOfferingID,
				window,
			)
	if err != nil {
		return nil, err
	}

	event, err :=
		newMerchantFutureOfferingServicePeriodEstablishedEvent(
			period,
		)
	if err != nil {
		return nil, err
	}

	if _, err := s.Models.OutboxEvent.InsertTx(
		ctx,
		tx,
		event,
	); err != nil {
		return nil, fmt.Errorf(
			"record Service Period established outbox event: %w",
			err,
		)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf(
			"commit next Service Period establishment: %w",
			err,
		)
	}

	s.Logger.
		GetLoggerWithContextFromContext(ctx).
		WithFunctionName(
			"EstablishNextMerchantFutureOfferingServicePeriodInternal",
		).
		Info(
			"next service period established",
			"service_period_id", period.ID,
			"future_offering_id", period.FutureOfferingID,
			"service_term_id", period.ServiceTermID,
			"period_number", period.PeriodNumber,
			"period_starts_on", period.PeriodStartsOn,
			"period_ends_on", period.PeriodEndsOn,
		)

	return &MerchantFutureOfferingServicePeriodEstablishment{
		Period: period,
	}, nil
}
