// Package data provides the production data-layer implementation for
// Merchant Future Offering Service Periods.
//
// focodebase/fobackend/internal/data/merchant_future_offering_service_periods.go
//
// GTM:
//
//	Layer: 2.4 Merchant / Future Offering Domain
//	Release Class: SPINE
//	Reason:
//	  Service Periods are immutable, authoritative performance windows within
//	  an established Future Offering Service Term. They preserve actual
//	  continuing-service chronology without conflating Service Periods with
//	  Service Terms, Billing Periods, Payment Periods, invoices, or settlement.
//
// Future Offering v1 Doctrine:
//
//	A Service Period is one authoritative performance window within an
//	established Service Term.
//
//	Service Period cadence and calendar semantics are Engineering invariants.
//	The service layer derives canonical monthly windows according to STCD from
//	the authoritative Service Term anchor. The final Service Period may be
//	shorter where necessary to terminate exactly at term_ends_on.
//
//	Service Periods are created just in time. Persisted rows represent completed
//	or currently performing service, not a forecast schedule of future service.
//
//	Merchants amend Service Terms, not Service Periods. Once created, a Service
//	Period is immutable.
//
//	A Service Term amendment may be requested while a Service Period is
//	performing, but authority transitions only at an eligible Service Period
//	boundary. The successor Service Term and its first Service Period must be
//	composed atomically by the service layer.
//
// SPINE Rule:
//
//	Keep compiling.
//	Keep production-ready.
//	Preserve immutable Service Period history.
//	Preserve just-in-time Service Period creation.
//	Preserve contiguous period chronology within each Service Term.
//	Preserve authoritative Service Term ownership.
//	Preserve transaction composability for Service Term boundary transitions.
//	Preserve the Engineering invariant that Service Period calendar semantics
//	are governed by STCD.
//	Never permit Administration configuration to redefine Service Period
//	duration, cadence, or calendar-boundary meaning.
//	Never couple this capability to Billing Period, Payment Period, invoice,
//	payment, settlement, transport, or downstream consumer implementation.
//	Block deployment if this file breaks Service Period identity, chronology,
//	concurrency, historical reconstruction, or transaction composability.
package data

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/PiccoloMondoC/focodebase/fobackend/internal/logging"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	merchantFutureOfferingServicePeriodTimelineDefaultLimit = 20
	merchantFutureOfferingServicePeriodTimelineMaxLimit     = 100
)

const merchantFutureOfferingServicePeriodSelectColumns = `
	p.id,
	p.service_term_id,
	p.future_offering_id,
	p.period_number,
	p.period_starts_on,
	p.period_ends_on,
	p.created_at`

// MerchantFutureOfferingServicePeriod is the canonical persisted
// representation of one immutable Future Offering Service Period.
type MerchantFutureOfferingServicePeriod struct {
	ID               uuid.UUID `json:"id" db:"id"`
	ServiceTermID    uuid.UUID `json:"service_term_id" db:"service_term_id"`
	FutureOfferingID uuid.UUID `json:"future_offering_id" db:"future_offering_id"`
	PeriodNumber     int       `json:"period_number" db:"period_number"`
	PeriodStartsOn   time.Time `json:"period_starts_on" db:"period_starts_on"`
	PeriodEndsOn     time.Time `json:"period_ends_on" db:"period_ends_on"`
	CreatedAt        time.Time `json:"created_at" db:"created_at"`
}

// MerchantFutureOfferingServicePeriodWindow is one concrete Service Period
// calendar window supplied for persistence.
//
// Calendar generation does not belong to this data model. The service layer
// derives the authoritative window according to STCD. This data capability
// validates the supplied window against persisted Service Term boundaries and
// Service Period chronology before inserting the immutable fact.
type MerchantFutureOfferingServicePeriodWindow struct {
	StartsOn time.Time
	EndsOn   time.Time
}

// MerchantFutureOfferingServicePeriodTimelineCursor identifies the row after
// which ListTimelineForFutureOffering continues.
type MerchantFutureOfferingServicePeriodTimelineCursor struct {
	PeriodStartsOn time.Time
	ID             uuid.UUID
}

// MerchantFutureOfferingServicePeriodModel manages immutable Service Period
// persistence and retrieval.
//
// Creation methods intentionally exist only in Tx form. Service Period creation
// is part of producer-owned domain workflow and may need to commit atomically
// with Service Term lifecycle changes and transactional outbox insertion.
type MerchantFutureOfferingServicePeriodModel struct {
	DB     *pgxpool.Pool
	Logger *logging.Logger
}

func scanMerchantFutureOfferingServicePeriod(
	row scannableRow,
) (*MerchantFutureOfferingServicePeriod, error) {
	var period MerchantFutureOfferingServicePeriod

	if err := row.Scan(
		&period.ID,
		&period.ServiceTermID,
		&period.FutureOfferingID,
		&period.PeriodNumber,
		&period.PeriodStartsOn,
		&period.PeriodEndsOn,
		&period.CreatedAt,
	); err != nil {
		return nil, err
	}

	return &period, nil
}

// normalizeMerchantFutureOfferingServicePeriodDate converts a caller-supplied
// domain date into the package's canonical UTC-midnight representation.
//
// The input Year/Month/Day fields are preserved rather than first converting
// the represented instant into UTC. Service Period boundaries are domain
// calendar dates under STCD, not elapsed-time instants.
func normalizeMerchantFutureOfferingServicePeriodDate(
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

func validateMerchantFutureOfferingServicePeriodWindow(
	requiredStartsOn time.Time,
	termEndsOn time.Time,
	window MerchantFutureOfferingServicePeriodWindow,
) (MerchantFutureOfferingServicePeriodWindow, error) {
	if requiredStartsOn.IsZero() ||
		termEndsOn.IsZero() ||
		window.StartsOn.IsZero() ||
		window.EndsOn.IsZero() {
		return MerchantFutureOfferingServicePeriodWindow{},
			ErrMerchantFutureOfferingServicePeriodInvalidInput
	}

	requiredStartsOn =
		normalizeMerchantFutureOfferingServicePeriodDate(requiredStartsOn)
	termEndsOn =
		normalizeMerchantFutureOfferingServicePeriodDate(termEndsOn)

	normalized := MerchantFutureOfferingServicePeriodWindow{
		StartsOn: normalizeMerchantFutureOfferingServicePeriodDate(
			window.StartsOn,
		),
		EndsOn: normalizeMerchantFutureOfferingServicePeriodDate(
			window.EndsOn,
		),
	}

	if !termEndsOn.After(requiredStartsOn) {
		return MerchantFutureOfferingServicePeriodWindow{},
			ErrMerchantFutureOfferingServicePeriodInvalidState
	}

	if !normalized.EndsOn.After(normalized.StartsOn) {
		return MerchantFutureOfferingServicePeriodWindow{},
			ErrMerchantFutureOfferingServicePeriodInvalidInput
	}

	if !normalized.StartsOn.Equal(requiredStartsOn) {
		return MerchantFutureOfferingServicePeriodWindow{},
			ErrMerchantFutureOfferingServicePeriodInvalidTransition
	}

	if normalized.EndsOn.After(termEndsOn) {
		return MerchantFutureOfferingServicePeriodWindow{},
			ErrMerchantFutureOfferingServicePeriodInvalidTransition
	}

	return normalized, nil
}

func (m *MerchantFutureOfferingServicePeriodModel) lockEstablishedServiceTermTx(
	ctx context.Context,
	tx pgx.Tx,
	serviceTermID uuid.UUID,
	futureOfferingID uuid.UUID,
) (time.Time, time.Time, error) {
	const query = `
		SELECT
			term_status,
			term_starts_on,
			term_ends_on
		FROM merchant_future_offering_service_terms
		WHERE id = $1
			AND future_offering_id = $2
		FOR UPDATE
	`

	var (
		status   string
		startsOn *time.Time
		endsOn   *time.Time
	)

	if err := tx.QueryRow(
		ctx,
		query,
		serviceTermID,
		futureOfferingID,
	).Scan(
		&status,
		&startsOn,
		&endsOn,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return time.Time{},
				time.Time{},
				ErrMerchantFutureOfferingServicePeriodServiceTermNotFound
		}

		return time.Time{}, time.Time{}, fmt.Errorf(
			"lock merchant future offering service term for service period: %w",
			err,
		)
	}

	if status != string(MerchantFutureOfferingServiceTermStatusEstablished) ||
		startsOn == nil ||
		endsOn == nil {
		return time.Time{},
			time.Time{},
			ErrMerchantFutureOfferingServicePeriodInvalidState
	}

	normalizedStartsOn :=
		normalizeMerchantFutureOfferingServicePeriodDate(*startsOn)
	normalizedEndsOn :=
		normalizeMerchantFutureOfferingServicePeriodDate(*endsOn)

	if !normalizedEndsOn.After(normalizedStartsOn) {
		return time.Time{},
			time.Time{},
			ErrMerchantFutureOfferingServicePeriodInvalidState
	}

	return normalizedStartsOn, normalizedEndsOn, nil
}

func translateMerchantFutureOfferingServicePeriodWriteError(
	err error,
) error {
	switch {
	case IsPgConstraint(
		err,
		"excl_merchant_future_offering_service_periods_no_overlap",
	):
		return ErrMerchantFutureOfferingServicePeriodOverlap

	case IsPgConstraint(
		err,
		"ux_merchant_future_offering_service_periods_term_number",
	):
		return ErrMerchantFutureOfferingServicePeriodNumberConflict

	case IsPgConstraint(
		err,
		"fk_merchant_future_offering_service_periods_service_term",
	):
		return ErrMerchantFutureOfferingServicePeriodServiceTermNotFound

	case IsExclusionViolation(err):
		return ErrMerchantFutureOfferingServicePeriodOverlap

	case IsUniqueViolation(err):
		return ErrMerchantFutureOfferingServicePeriodNumberConflict

	case IsForeignKeyViolation(err):
		return ErrMerchantFutureOfferingServicePeriodServiceTermNotFound

	case IsCheckViolation(err):
		return ErrMerchantFutureOfferingServicePeriodInvalidInput

	default:
		return err
	}
}

func (m *MerchantFutureOfferingServicePeriodModel) getLatestForServiceTermTx(
	ctx context.Context,
	tx pgx.Tx,
	serviceTermID uuid.UUID,
	futureOfferingID uuid.UUID,
) (*MerchantFutureOfferingServicePeriod, error) {
	const query = `
		SELECT ` + merchantFutureOfferingServicePeriodSelectColumns + `
		FROM merchant_future_offering_service_periods AS p
		WHERE p.service_term_id = $1
			AND p.future_offering_id = $2
		ORDER BY p.period_number DESC
		LIMIT 1
		FOR UPDATE
	`

	period, err := scanMerchantFutureOfferingServicePeriod(
		tx.QueryRow(
			ctx,
			query,
			serviceTermID,
			futureOfferingID,
		),
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}

		return nil, fmt.Errorf(
			"get latest merchant future offering service period for update: %w",
			err,
		)
	}

	return period, nil
}

func (m *MerchantFutureOfferingServicePeriodModel) insertServicePeriodTx(
	ctx context.Context,
	tx pgx.Tx,
	serviceTermID uuid.UUID,
	futureOfferingID uuid.UUID,
	periodNumber int,
	window MerchantFutureOfferingServicePeriodWindow,
) (*MerchantFutureOfferingServicePeriod, error) {
	const query = `
		INSERT INTO merchant_future_offering_service_periods AS p (
			service_term_id,
			future_offering_id,
			period_number,
			period_starts_on,
			period_ends_on
		)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING ` + merchantFutureOfferingServicePeriodSelectColumns

	period, err := scanMerchantFutureOfferingServicePeriod(
		tx.QueryRow(
			ctx,
			query,
			serviceTermID,
			futureOfferingID,
			periodNumber,
			window.StartsOn,
			window.EndsOn,
		),
	)
	if err != nil {
		translated :=
			translateMerchantFutureOfferingServicePeriodWriteError(err)
		if translated != err {
			return nil, translated
		}

		return nil, fmt.Errorf(
			"insert merchant future offering service period: %w",
			err,
		)
	}

	return period, nil
}

// CreateFirstPeriodTx creates the first immutable Service Period for an
// established Service Term inside a caller-owned transaction.
//
// The supplied window must begin exactly at term_starts_on. Calendar generation
// is deliberately outside this data method; the service layer must derive the
// canonical first window according to STCD.
//
// This operation acquires a lock on the owning Service Term, ensuring that
// concurrent attempts for the same term serialize before period existence is
// evaluated.
func (m *MerchantFutureOfferingServicePeriodModel) CreateFirstPeriodTx(
	ctx context.Context,
	tx pgx.Tx,
	serviceTermID uuid.UUID,
	futureOfferingID uuid.UUID,
	window MerchantFutureOfferingServicePeriodWindow,
) (*MerchantFutureOfferingServicePeriod, error) {
	if tx == nil ||
		serviceTermID == uuid.Nil ||
		futureOfferingID == uuid.Nil {
		return nil, ErrMerchantFutureOfferingServicePeriodInvalidInput
	}

	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	nestedTx, err := tx.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf(
			"begin first service period creation savepoint: %w",
			err,
		)
	}
	defer nestedTx.Rollback(ctx)

	period, err := m.createFirstPeriodTx(
		ctx,
		nestedTx,
		serviceTermID,
		futureOfferingID,
		window,
	)
	if err != nil {
		return nil, err
	}

	if err := nestedTx.Commit(ctx); err != nil {
		return nil, fmt.Errorf(
			"release first service period creation savepoint: %w",
			err,
		)
	}

	return period, nil
}

func (m *MerchantFutureOfferingServicePeriodModel) createFirstPeriodTx(
	ctx context.Context,
	tx pgx.Tx,
	serviceTermID uuid.UUID,
	futureOfferingID uuid.UUID,
	window MerchantFutureOfferingServicePeriodWindow,
) (*MerchantFutureOfferingServicePeriod, error) {
	logger := m.Logger.GetLoggerWithContextFromContext(ctx).
		WithFunctionName("CreateFirstMerchantFutureOfferingServicePeriod")

	termStartsOn, termEndsOn, err :=
		m.lockEstablishedServiceTermTx(
			ctx,
			tx,
			serviceTermID,
			futureOfferingID,
		)
	if err != nil {
		return nil, err
	}

	latest, err := m.getLatestForServiceTermTx(
		ctx,
		tx,
		serviceTermID,
		futureOfferingID,
	)
	if err != nil {
		return nil, err
	}

	if latest != nil {
		return nil, ErrMerchantFutureOfferingServicePeriodNumberConflict
	}

	normalized, err :=
		validateMerchantFutureOfferingServicePeriodWindow(
			termStartsOn,
			termEndsOn,
			window,
		)
	if err != nil {
		logger.Error(
			"first service period validation failed",
			"error", err,
			"service_term_id", serviceTermID,
			"future_offering_id", futureOfferingID,
		)
		return nil, err
	}

	period, err := m.insertServicePeriodTx(
		ctx,
		tx,
		serviceTermID,
		futureOfferingID,
		1,
		normalized,
	)
	if err != nil {
		return nil, err
	}

	logger.Info(
		"first service period created",
		"service_period_id", period.ID,
		"service_term_id", serviceTermID,
		"future_offering_id", futureOfferingID,
		"period_number", period.PeriodNumber,
		"period_starts_on", period.PeriodStartsOn,
		"period_ends_on", period.PeriodEndsOn,
	)

	return period, nil
}

// CreateNextPeriodTx creates exactly one next immutable Service Period inside
// an established Service Term.
//
// The service layer supplies the STCD-derived calendar window. This method
// proves that the supplied window starts exactly at the end of the latest
// persisted period, remains within term_ends_on, and receives the next
// deterministic period number.
//
// It does not decide whether today's date is the appropriate workflow boundary.
// Time-based eligibility and scheduling belong to the service/automation layer.
func (m *MerchantFutureOfferingServicePeriodModel) CreateNextPeriodTx(
	ctx context.Context,
	tx pgx.Tx,
	serviceTermID uuid.UUID,
	futureOfferingID uuid.UUID,
	window MerchantFutureOfferingServicePeriodWindow,
) (*MerchantFutureOfferingServicePeriod, error) {
	if tx == nil ||
		serviceTermID == uuid.Nil ||
		futureOfferingID == uuid.Nil {
		return nil, ErrMerchantFutureOfferingServicePeriodInvalidInput
	}

	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	nestedTx, err := tx.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf(
			"begin next service period creation savepoint: %w",
			err,
		)
	}
	defer nestedTx.Rollback(ctx)

	period, err := m.createNextPeriodTx(
		ctx,
		nestedTx,
		serviceTermID,
		futureOfferingID,
		window,
	)
	if err != nil {
		return nil, err
	}

	if err := nestedTx.Commit(ctx); err != nil {
		return nil, fmt.Errorf(
			"release next service period creation savepoint: %w",
			err,
		)
	}

	return period, nil
}

func (m *MerchantFutureOfferingServicePeriodModel) createNextPeriodTx(
	ctx context.Context,
	tx pgx.Tx,
	serviceTermID uuid.UUID,
	futureOfferingID uuid.UUID,
	window MerchantFutureOfferingServicePeriodWindow,
) (*MerchantFutureOfferingServicePeriod, error) {
	logger := m.Logger.GetLoggerWithContextFromContext(ctx).
		WithFunctionName("CreateNextMerchantFutureOfferingServicePeriod")

	_, termEndsOn, err :=
		m.lockEstablishedServiceTermTx(
			ctx,
			tx,
			serviceTermID,
			futureOfferingID,
		)
	if err != nil {
		return nil, err
	}

	latest, err := m.getLatestForServiceTermTx(
		ctx,
		tx,
		serviceTermID,
		futureOfferingID,
	)
	if err != nil {
		return nil, err
	}

	if latest == nil {
		return nil, ErrMerchantFutureOfferingServicePeriodInvalidState
	}

	latestEndsOn :=
		normalizeMerchantFutureOfferingServicePeriodDate(
			latest.PeriodEndsOn,
		)

	if !termEndsOn.After(latestEndsOn) {
		return nil, ErrMerchantFutureOfferingServicePeriodInvalidTransition
	}

	if latest.PeriodNumber <= 0 ||
		latest.PeriodNumber >= 2147483647 {
		return nil, ErrMerchantFutureOfferingServicePeriodInvalidState
	}

	normalized, err :=
		validateMerchantFutureOfferingServicePeriodWindow(
			latestEndsOn,
			termEndsOn,
			window,
		)
	if err != nil {
		logger.Error(
			"next service period validation failed",
			"error", err,
			"service_term_id", serviceTermID,
			"future_offering_id", futureOfferingID,
			"latest_period_id", latest.ID,
			"latest_period_number", latest.PeriodNumber,
		)
		return nil, err
	}

	period, err := m.insertServicePeriodTx(
		ctx,
		tx,
		serviceTermID,
		futureOfferingID,
		latest.PeriodNumber+1,
		normalized,
	)
	if err != nil {
		return nil, err
	}

	logger.Info(
		"next service period created",
		"service_period_id", period.ID,
		"service_term_id", serviceTermID,
		"future_offering_id", futureOfferingID,
		"period_number", period.PeriodNumber,
		"period_starts_on", period.PeriodStartsOn,
		"period_ends_on", period.PeriodEndsOn,
	)

	return period, nil
}

// GetByID retrieves an immutable Service Period by canonical ID.
func (m *MerchantFutureOfferingServicePeriodModel) GetByID(
	ctx context.Context,
	id uuid.UUID,
) (*MerchantFutureOfferingServicePeriod, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	if id == uuid.Nil {
		return nil, ErrMerchantFutureOfferingServicePeriodNotFound
	}

	const query = `
		SELECT ` + merchantFutureOfferingServicePeriodSelectColumns + `
		FROM merchant_future_offering_service_periods AS p
		WHERE p.id = $1
	`

	period, err := scanMerchantFutureOfferingServicePeriod(
		m.DB.QueryRow(ctx, query, id),
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrMerchantFutureOfferingServicePeriodNotFound
		}

		return nil, fmt.Errorf(
			"get merchant future offering service period: %w",
			err,
		)
	}

	return period, nil
}

// GetAt retrieves the immutable Service Period whose authoritative window
// covers the supplied date for a Future Offering, regardless of whether its
// owning Service Term remains the currently established term.
//
// This method is suitable for historical reconstruction.
func (m *MerchantFutureOfferingServicePeriodModel) GetAt(
	ctx context.Context,
	futureOfferingID uuid.UUID,
	at time.Time,
) (*MerchantFutureOfferingServicePeriod, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	if futureOfferingID == uuid.Nil || at.IsZero() {
		return nil, ErrMerchantFutureOfferingServicePeriodNotFound
	}

	atDate :=
		normalizeMerchantFutureOfferingServicePeriodDate(at)

	const query = `
		SELECT ` + merchantFutureOfferingServicePeriodSelectColumns + `
		FROM merchant_future_offering_service_periods AS p
		WHERE p.future_offering_id = $1
			AND p.period_starts_on <= $2::date
			AND p.period_ends_on > $2::date
	`

	period, err := scanMerchantFutureOfferingServicePeriod(
		m.DB.QueryRow(
			ctx,
			query,
			futureOfferingID,
			atDate,
		),
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrMerchantFutureOfferingServicePeriodNotFound
		}

		return nil, fmt.Errorf(
			"get merchant future offering service period at date: %w",
			err,
		)
	}

	return period, nil
}

// GetCurrentAt retrieves the authoritative Service Period covering the
// supplied date only when its owning Service Term is currently established.
//
// Unlike GetAt, this method answers operational authority rather than
// historical chronology.
func (m *MerchantFutureOfferingServicePeriodModel) GetCurrentAt(
	ctx context.Context,
	futureOfferingID uuid.UUID,
	at time.Time,
) (*MerchantFutureOfferingServicePeriod, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	if futureOfferingID == uuid.Nil || at.IsZero() {
		return nil, ErrMerchantFutureOfferingServicePeriodNotFound
	}

	atDate :=
		normalizeMerchantFutureOfferingServicePeriodDate(at)

	const query = `
		SELECT ` + merchantFutureOfferingServicePeriodSelectColumns + `
		FROM merchant_future_offering_service_periods AS p
		JOIN merchant_future_offering_service_terms AS t
			ON t.id = p.service_term_id
			AND t.future_offering_id = p.future_offering_id
		WHERE p.future_offering_id = $1
			AND t.term_status = $2
			AND p.period_starts_on <= $3::date
			AND p.period_ends_on > $3::date
	`

	period, err := scanMerchantFutureOfferingServicePeriod(
		m.DB.QueryRow(
			ctx,
			query,
			futureOfferingID,
			string(MerchantFutureOfferingServiceTermStatusEstablished),
			atDate,
		),
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrMerchantFutureOfferingServicePeriodNotFound
		}

		return nil, fmt.Errorf(
			"get current merchant future offering service period at date: %w",
			err,
		)
	}

	return period, nil
}

// ListForServiceTerm returns all immutable Service Periods created so far for
// one Service Term in canonical period-number order.
//
// Under the just-in-time doctrine this list contains history plus, where
// applicable, the currently performing period. It is not a forecast schedule.
func (m *MerchantFutureOfferingServicePeriodModel) ListForServiceTerm(
	ctx context.Context,
	serviceTermID uuid.UUID,
	futureOfferingID uuid.UUID,
) ([]*MerchantFutureOfferingServicePeriod, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	if serviceTermID == uuid.Nil ||
		futureOfferingID == uuid.Nil {
		return nil, ErrMerchantFutureOfferingServicePeriodInvalidInput
	}

	const query = `
		SELECT ` + merchantFutureOfferingServicePeriodSelectColumns + `
		FROM merchant_future_offering_service_periods AS p
		WHERE p.service_term_id = $1
			AND p.future_offering_id = $2
		ORDER BY p.period_number ASC
	`

	rows, err := m.DB.Query(
		ctx,
		query,
		serviceTermID,
		futureOfferingID,
	)
	if err != nil {
		return nil, fmt.Errorf(
			"list merchant future offering service periods for service term: %w",
			err,
		)
	}

	periods, err := pgx.CollectRows(
		rows,
		func(
			row pgx.CollectableRow,
		) (*MerchantFutureOfferingServicePeriod, error) {
			return scanMerchantFutureOfferingServicePeriod(row)
		},
	)
	if err != nil {
		return nil, fmt.Errorf(
			"collect merchant future offering service periods for service term: %w",
			err,
		)
	}

	return periods, nil
}

// ListTimelineForFutureOffering returns immutable Service Period history across
// Service Term revisions using keyset pagination.
func (m *MerchantFutureOfferingServicePeriodModel) ListTimelineForFutureOffering(
	ctx context.Context,
	futureOfferingID uuid.UUID,
	limit int,
	after *MerchantFutureOfferingServicePeriodTimelineCursor,
) ([]*MerchantFutureOfferingServicePeriod, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	if futureOfferingID == uuid.Nil {
		return nil, ErrMerchantFutureOfferingServicePeriodInvalidInput
	}

	if after != nil &&
		(after.PeriodStartsOn.IsZero() ||
			after.ID == uuid.Nil) {
		return nil, ErrMerchantFutureOfferingServicePeriodInvalidInput
	}

	if limit <= 0 {
		limit =
			merchantFutureOfferingServicePeriodTimelineDefaultLimit
	}

	if limit >
		merchantFutureOfferingServicePeriodTimelineMaxLimit {
		limit =
			merchantFutureOfferingServicePeriodTimelineMaxLimit
	}

	var (
		rows pgx.Rows
		err  error
	)

	if after == nil {
		const query = `
			SELECT ` + merchantFutureOfferingServicePeriodSelectColumns + `
			FROM merchant_future_offering_service_periods AS p
			WHERE p.future_offering_id = $1
			ORDER BY p.period_starts_on ASC, p.id ASC
			LIMIT $2
		`

		rows, err = m.DB.Query(
			ctx,
			query,
			futureOfferingID,
			limit,
		)
	} else {
		afterStartsOn :=
			normalizeMerchantFutureOfferingServicePeriodDate(
				after.PeriodStartsOn,
			)

		const query = `
			SELECT ` + merchantFutureOfferingServicePeriodSelectColumns + `
			FROM merchant_future_offering_service_periods AS p
			WHERE p.future_offering_id = $1
				AND (p.period_starts_on, p.id) > ($2, $3)
			ORDER BY p.period_starts_on ASC, p.id ASC
			LIMIT $4
		`

		rows, err = m.DB.Query(
			ctx,
			query,
			futureOfferingID,
			afterStartsOn,
			after.ID,
			limit,
		)
	}

	if err != nil {
		return nil, fmt.Errorf(
			"list merchant future offering service period timeline: %w",
			err,
		)
	}

	periods, err := pgx.CollectRows(
		rows,
		func(
			row pgx.CollectableRow,
		) (*MerchantFutureOfferingServicePeriod, error) {
			return scanMerchantFutureOfferingServicePeriod(row)
		},
	)
	if err != nil {
		return nil, fmt.Errorf(
			"collect merchant future offering service period timeline: %w",
			err,
		)
	}

	return periods, nil
}

// BeginTx begins a caller-owned transaction for Service Period domain
// composition.
//
// The service layer may use this seam when Service Period persistence must
// commit atomically with producer-owned Service Term lifecycle work and
// transactional outbox insertion.
func (m *MerchantFutureOfferingServicePeriodModel) BeginTx(
	ctx context.Context,
) (pgx.Tx, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	tx, err := m.DB.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf(
			"begin merchant future offering service period transaction: %w",
			err,
		)
	}

	return tx, nil
}
