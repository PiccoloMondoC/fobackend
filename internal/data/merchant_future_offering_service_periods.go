// Package data provides the production data-layer implementation for
// Merchant Future Offering Service Periods.
//
// sdworkspace/sdbackend/internal/data/merchant_future_offering_service_periods.go
//
// GTM:
//
//	Layer: 2.4 Merchant / Future Offering Domain
//	Release Class: SPINE
//	Reason:
//	  Service Periods are the authoritative bounded performance windows within
//	  an established Future Offering Service Term. They preserve continuing-
//	  service chronology without conflating Service Periods with Service Terms,
//	  Billing Periods, Payment Periods, invoices, or settlement.
//
// Future Offering v1 Doctrine:
//
//	A Service Period is an individual performance window within an established
//	Service Term during which Sagrenti renders continuing service, measures
//	applicable activity, and provides utility.
//
//	Service Period cadence and generation policy are not persistence policy.
//	The service layer supplies concrete windows produced under the applicable
//	Administration configuration. This file validates and persists their
//	structural relationship to the authoritative Service Term.
//
//	The current Service Period schedule must partition the authoritative
//	Service Term window completely, contiguously, and without overlap.
//
//	Once a Service Period has begun, its window is historical fact and cannot
//	be rewritten. A prospective schedule amendment therefore preserves every
//	begun period and atomically replaces only the not-yet-begun suffix.
//
// SPINE Rule:
//
//	Keep compiling.
//	Keep production-ready.
//	Preserve complete and contiguous current-schedule coverage.
//	Preserve immutable performed Service Period history.
//	Preserve atomic prospective schedule replacement.
//	Preserve authoritative Service Term ownership.
//	Never hard-code Service Period cadence or generation policy.
//	Never couple this capability to Billing Period, Payment Period, invoice,
//	payment, or other downstream implementation.
//	Block deployment if this file breaks Service Period identity, schedule
//	integrity, concurrency, historical reconstruction, or transaction
//	composability.
package data

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/PiccoloMondoC/sdworkspace/sdbackend/internal/logging"

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
	p.superseded_at,
	p.created_at`

// MerchantFutureOfferingServicePeriod is the canonical persisted
// representation of one Future Offering Service Period.
type MerchantFutureOfferingServicePeriod struct {
	ID               uuid.UUID  `json:"id" db:"id"`
	ServiceTermID    uuid.UUID  `json:"service_term_id" db:"service_term_id"`
	FutureOfferingID uuid.UUID  `json:"future_offering_id" db:"future_offering_id"`
	PeriodNumber     int        `json:"period_number" db:"period_number"`
	PeriodStartsOn   time.Time  `json:"period_starts_on" db:"period_starts_on"`
	PeriodEndsOn     time.Time  `json:"period_ends_on" db:"period_ends_on"`
	SupersededAt     *time.Time `json:"superseded_at" db:"superseded_at"`
	CreatedAt        time.Time  `json:"created_at" db:"created_at"`
}

// MerchantFutureOfferingServicePeriodWindow is a concrete Service Period
// calendar window supplied for persistence.
//
// PeriodNumber is intentionally absent. Canonical ordering belongs to the
// persisted schedule and is assigned by this data capability after the
// supplied windows have been normalized and validated.
type MerchantFutureOfferingServicePeriodWindow struct {
	StartsOn time.Time
	EndsOn   time.Time
}

// MerchantFutureOfferingServicePeriodScheduleReplacement describes the
// historical rows superseded and the new current rows established by one
// atomic prospective schedule amendment.
type MerchantFutureOfferingServicePeriodScheduleReplacement struct {
	Superseded  []*MerchantFutureOfferingServicePeriod
	Established []*MerchantFutureOfferingServicePeriod
}

// MerchantFutureOfferingServicePeriodTimelineCursor identifies the row after
// which ListTimelineForFutureOffering continues.
type MerchantFutureOfferingServicePeriodTimelineCursor struct {
	PeriodStartsOn time.Time
	ID             uuid.UUID
}

// MerchantFutureOfferingServicePeriodModel manages Service Period persistence,
// schedule establishment, prospective schedule amendment, and retrieval.
type MerchantFutureOfferingServicePeriodModel struct {
	DB     *pgxpool.Pool
	Logger *logging.Logger
}

type merchantFutureOfferingNumberedServicePeriodWindow struct {
	PeriodNumber int
	StartsOn     time.Time
	EndsOn       time.Time
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
		&period.SupersededAt,
		&period.CreatedAt,
	); err != nil {
		return nil, err
	}

	return &period, nil
}

// normalizeMerchantFutureOfferingServicePeriodDate converts a caller-supplied
// domain date into the package's canonical UTC-midnight representation.
//
// It intentionally preserves the input value's Year/Month/Day fields rather
// than converting the represented instant into UTC first. Service Period
// boundaries are domain calendar dates under STCD, not elapsed-time instants.
// Callers must therefore supply a time.Time whose calendar fields already
// represent the intended domain date.
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

func validateAndNumberMerchantFutureOfferingServicePeriodWindows(
	requiredStartsOn time.Time,
	requiredEndsOn time.Time,
	firstPeriodNumber int,
	windows []MerchantFutureOfferingServicePeriodWindow,
) ([]merchantFutureOfferingNumberedServicePeriodWindow, error) {
	if requiredStartsOn.IsZero() || requiredEndsOn.IsZero() {
		return nil, errors.New("required service period schedule boundary is missing")
	}

	requiredStartsOn = normalizeMerchantFutureOfferingServicePeriodDate(requiredStartsOn)
	requiredEndsOn = normalizeMerchantFutureOfferingServicePeriodDate(requiredEndsOn)

	if !requiredEndsOn.After(requiredStartsOn) {
		return nil, errors.New("required service period schedule window is invalid")
	}

	if firstPeriodNumber <= 0 {
		return nil, errors.New("first service period number must be greater than zero")
	}

	if len(windows) == 0 {
		return nil, errors.New("at least one service period window is required")
	}

	// Because Service Period boundaries are DATE values and every period must
	// have positive duration, a valid partition cannot contain more periods
	// than calendar days in the required window. This is a derived Engineering
	// safety invariant, not Service Period cadence policy.
	maxPossiblePeriods := int(
		requiredEndsOn.Sub(requiredStartsOn) / (24 * time.Hour),
	)

	if len(windows) > maxPossiblePeriods {
		return nil, fmt.Errorf(
			"service period schedule contains %d periods but the required window can contain at most %d positive-duration date periods",
			len(windows),
			maxPossiblePeriods,
		)
	}

	normalized := make(
		[]MerchantFutureOfferingServicePeriodWindow,
		len(windows),
	)

	for i, window := range windows {
		if window.StartsOn.IsZero() || window.EndsOn.IsZero() {
			return nil, fmt.Errorf(
				"service period window %d requires both starts_on and ends_on",
				i+1,
			)
		}

		normalized[i] = MerchantFutureOfferingServicePeriodWindow{
			StartsOn: normalizeMerchantFutureOfferingServicePeriodDate(
				window.StartsOn,
			),
			EndsOn: normalizeMerchantFutureOfferingServicePeriodDate(
				window.EndsOn,
			),
		}

		if !normalized[i].EndsOn.After(normalized[i].StartsOn) {
			return nil, fmt.Errorf(
				"service period window %d must end after it starts",
				i+1,
			)
		}

		if i > 0 &&
			!normalized[i].StartsOn.After(normalized[i-1].StartsOn) {
			return nil, fmt.Errorf(
				"service period windows must be supplied in strictly ascending start-date order (position %d)",
				i+1,
			)
		}
	}

	if !normalized[0].StartsOn.Equal(requiredStartsOn) {
		return nil, errors.New(
			"service period schedule must start exactly at the required boundary",
		)
	}

	numbered := make(
		[]merchantFutureOfferingNumberedServicePeriodWindow,
		0,
		len(normalized),
	)

	for i, window := range normalized {
		if i > 0 &&
			!window.StartsOn.Equal(normalized[i-1].EndsOn) {
			return nil, fmt.Errorf(
				"service period schedule has a gap or overlap between positions %d and %d",
				i,
				i+1,
			)
		}

		numbered = append(
			numbered,
			merchantFutureOfferingNumberedServicePeriodWindow{
				PeriodNumber: firstPeriodNumber + i,
				StartsOn:     window.StartsOn,
				EndsOn:       window.EndsOn,
			},
		)
	}

	if !normalized[len(normalized)-1].EndsOn.Equal(requiredEndsOn) {
		return nil, errors.New(
			"service period schedule must end exactly at the required boundary",
		)
	}

	return numbered, nil
}

func validateCurrentMerchantFutureOfferingServicePeriodSchedule(
	termStartsOn time.Time,
	termEndsOn time.Time,
	periods []*MerchantFutureOfferingServicePeriod,
) error {
	if len(periods) == 0 {
		return errors.New("current service period schedule is empty")
	}

	termStartsOn = normalizeMerchantFutureOfferingServicePeriodDate(termStartsOn)
	termEndsOn = normalizeMerchantFutureOfferingServicePeriodDate(termEndsOn)

	for i, period := range periods {
		if period == nil {
			return errors.New("current service period schedule contains a nil period")
		}

		if period.PeriodNumber != i+1 {
			return fmt.Errorf(
				"current service period schedule expected period_number %d but found %d",
				i+1,
				period.PeriodNumber,
			)
		}

		if !period.PeriodEndsOn.After(period.PeriodStartsOn) {
			return fmt.Errorf(
				"current service period %s has an invalid window",
				period.ID,
			)
		}

		if i == 0 {
			if !period.PeriodStartsOn.Equal(termStartsOn) {
				return errors.New(
					"current service period schedule does not start at term_starts_on",
				)
			}

			continue
		}

		if !period.PeriodStartsOn.Equal(periods[i-1].PeriodEndsOn) {
			return fmt.Errorf(
				"current service period schedule is not contiguous between period_number %d and %d",
				periods[i-1].PeriodNumber,
				period.PeriodNumber,
			)
		}
	}

	if !periods[len(periods)-1].PeriodEndsOn.Equal(termEndsOn) {
		return errors.New(
			"current service period schedule does not end at term_ends_on",
		)
	}

	return nil
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
			"lock merchant future offering service term for service periods: %w",
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

	return normalizeMerchantFutureOfferingServicePeriodDate(*startsOn),
		normalizeMerchantFutureOfferingServicePeriodDate(*endsOn),
		nil
}

func translateMerchantFutureOfferingServicePeriodWriteError(err error) error {
	switch {
	case IsPgConstraint(
		err,
		"excl_merchant_future_offering_service_periods_no_current_overlap",
	):
		return ErrMerchantFutureOfferingServicePeriodOverlap

	case IsPgConstraint(
		err,
		"ux_merchant_future_offering_service_periods_current_term_number",
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

func (m *MerchantFutureOfferingServicePeriodModel) insertServicePeriodTx(
	ctx context.Context,
	tx pgx.Tx,
	serviceTermID uuid.UUID,
	futureOfferingID uuid.UUID,
	window merchantFutureOfferingNumberedServicePeriodWindow,
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
			window.PeriodNumber,
			window.StartsOn,
			window.EndsOn,
		),
	)
	if err != nil {
		translated := translateMerchantFutureOfferingServicePeriodWriteError(err)
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

// EstablishSchedule atomically establishes the initial complete Service Period
// schedule for an established Service Term.
//
// The caller supplies concrete calendar windows. This method does not determine
// cadence. The windows must completely and contiguously partition the
// authoritative Service Term.
func (m *MerchantFutureOfferingServicePeriodModel) EstablishSchedule(
	ctx context.Context,
	serviceTermID uuid.UUID,
	futureOfferingID uuid.UUID,
	windows []MerchantFutureOfferingServicePeriodWindow,
) ([]*MerchantFutureOfferingServicePeriod, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	tx, err := m.DB.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf(
			"begin merchant future offering service period schedule establishment: %w",
			err,
		)
	}
	defer tx.Rollback(ctx)

	periods, err := m.establishScheduleTx(
		ctx,
		tx,
		serviceTermID,
		futureOfferingID,
		windows,
	)
	if err != nil {
		return nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf(
			"commit merchant future offering service period schedule establishment: %w",
			err,
		)
	}

	return periods, nil
}

// EstablishScheduleTx establishes an initial complete Service Period schedule
// inside a caller-owned transaction.
//
// A savepoint keeps this operation atomic even when the caller continues using
// its outer transaction after a rejected schedule.
func (m *MerchantFutureOfferingServicePeriodModel) EstablishScheduleTx(
	ctx context.Context,
	tx pgx.Tx,
	serviceTermID uuid.UUID,
	futureOfferingID uuid.UUID,
	windows []MerchantFutureOfferingServicePeriodWindow,
) ([]*MerchantFutureOfferingServicePeriod, error) {
	if tx == nil {
		return nil, ErrMerchantFutureOfferingServicePeriodInvalidInput
	}

	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	nestedTx, err := tx.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf(
			"begin service period schedule establishment savepoint: %w",
			err,
		)
	}
	defer nestedTx.Rollback(ctx)

	periods, err := m.establishScheduleTx(
		ctx,
		nestedTx,
		serviceTermID,
		futureOfferingID,
		windows,
	)
	if err != nil {
		return nil, err
	}

	if err := nestedTx.Commit(ctx); err != nil {
		return nil, fmt.Errorf(
			"release service period schedule establishment savepoint: %w",
			err,
		)
	}

	return periods, nil
}

func (m *MerchantFutureOfferingServicePeriodModel) establishScheduleTx(
	ctx context.Context,
	tx pgx.Tx,
	serviceTermID uuid.UUID,
	futureOfferingID uuid.UUID,
	windows []MerchantFutureOfferingServicePeriodWindow,
) ([]*MerchantFutureOfferingServicePeriod, error) {
	logger := m.Logger.GetLoggerWithContextFromContext(ctx).
		WithFunctionName("EstablishMerchantFutureOfferingServicePeriodSchedule")

	if serviceTermID == uuid.Nil || futureOfferingID == uuid.Nil {
		return nil, ErrMerchantFutureOfferingServicePeriodInvalidInput
	}

	termStartsOn, termEndsOn, err := m.lockEstablishedServiceTermTx(
		ctx,
		tx,
		serviceTermID,
		futureOfferingID,
	)
	if err != nil {
		return nil, err
	}

	const existsQuery = `
		SELECT EXISTS (
			SELECT 1
			FROM merchant_future_offering_service_periods
			WHERE service_term_id = $1
				AND future_offering_id = $2
				AND superseded_at IS NULL
		)
	`

	var exists bool
	if err := tx.QueryRow(
		ctx,
		existsQuery,
		serviceTermID,
		futureOfferingID,
	).Scan(&exists); err != nil {
		return nil, fmt.Errorf(
			"check existing merchant future offering service period schedule: %w",
			err,
		)
	}

	if exists {
		return nil, ErrMerchantFutureOfferingServicePeriodScheduleAlreadyExists
	}

	numbered, err := validateAndNumberMerchantFutureOfferingServicePeriodWindows(
		termStartsOn,
		termEndsOn,
		1,
		windows,
	)
	if err != nil {
		logger.Error(
			"service period schedule validation failed",
			"error", err,
			"service_term_id", serviceTermID,
			"future_offering_id", futureOfferingID,
		)

		return nil, fmt.Errorf(
			"%w: %v",
			ErrMerchantFutureOfferingServicePeriodInvalidSchedule,
			err,
		)
	}

	periods := make(
		[]*MerchantFutureOfferingServicePeriod,
		0,
		len(numbered),
	)

	for _, window := range numbered {
		period, err := m.insertServicePeriodTx(
			ctx,
			tx,
			serviceTermID,
			futureOfferingID,
			window,
		)
		if err != nil {
			return nil, err
		}

		periods = append(periods, period)
	}

	logger.Info(
		"service period schedule established",
		"service_term_id", serviceTermID,
		"future_offering_id", futureOfferingID,
		"period_count", len(periods),
	)

	return periods, nil
}

func (m *MerchantFutureOfferingServicePeriodModel) lockCurrentScheduleTx(
	ctx context.Context,
	tx pgx.Tx,
	serviceTermID uuid.UUID,
	futureOfferingID uuid.UUID,
) ([]*MerchantFutureOfferingServicePeriod, error) {
	const query = `
		SELECT ` + merchantFutureOfferingServicePeriodSelectColumns + `
		FROM merchant_future_offering_service_periods AS p
		WHERE p.service_term_id = $1
			AND p.future_offering_id = $2
			AND p.superseded_at IS NULL
		ORDER BY p.period_number ASC
		FOR UPDATE
	`

	rows, err := tx.Query(
		ctx,
		query,
		serviceTermID,
		futureOfferingID,
	)
	if err != nil {
		return nil, fmt.Errorf(
			"lock current merchant future offering service period schedule: %w",
			err,
		)
	}

	periods, err := pgx.CollectRows(
		rows,
		func(row pgx.CollectableRow) (*MerchantFutureOfferingServicePeriod, error) {
			return scanMerchantFutureOfferingServicePeriod(row)
		},
	)
	if err != nil {
		return nil, fmt.Errorf(
			"collect current merchant future offering service period schedule: %w",
			err,
		)
	}

	return periods, nil
}

// ReplaceFutureSchedule atomically replaces the complete not-yet-begun suffix
// of the current Service Period schedule.
//
// Every Service Period whose start date is today or earlier is preserved.
// Replacement windows must begin exactly where that immutable prefix ends and
// must continue contiguously through term_ends_on.
//
// This operation changes Service Period partitioning only. It cannot change the
// authoritative Service Term window.
func (m *MerchantFutureOfferingServicePeriodModel) ReplaceFutureSchedule(
	ctx context.Context,
	serviceTermID uuid.UUID,
	futureOfferingID uuid.UUID,
	windows []MerchantFutureOfferingServicePeriodWindow,
) (*MerchantFutureOfferingServicePeriodScheduleReplacement, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	tx, err := m.DB.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf(
			"begin merchant future offering service period schedule replacement: %w",
			err,
		)
	}
	defer tx.Rollback(ctx)

	result, err := m.replaceFutureScheduleTx(
		ctx,
		tx,
		serviceTermID,
		futureOfferingID,
		windows,
	)
	if err != nil {
		return nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf(
			"commit merchant future offering service period schedule replacement: %w",
			err,
		)
	}

	return result, nil
}

// ReplaceFutureScheduleTx atomically replaces the complete not-yet-begun
// Service Period schedule suffix within a caller-owned transaction.
func (m *MerchantFutureOfferingServicePeriodModel) ReplaceFutureScheduleTx(
	ctx context.Context,
	tx pgx.Tx,
	serviceTermID uuid.UUID,
	futureOfferingID uuid.UUID,
	windows []MerchantFutureOfferingServicePeriodWindow,
) (*MerchantFutureOfferingServicePeriodScheduleReplacement, error) {
	if tx == nil {
		return nil, ErrMerchantFutureOfferingServicePeriodInvalidInput
	}

	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	nestedTx, err := tx.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf(
			"begin service period schedule replacement savepoint: %w",
			err,
		)
	}
	defer nestedTx.Rollback(ctx)

	result, err := m.replaceFutureScheduleTx(
		ctx,
		nestedTx,
		serviceTermID,
		futureOfferingID,
		windows,
	)
	if err != nil {
		return nil, err
	}

	if err := nestedTx.Commit(ctx); err != nil {
		return nil, fmt.Errorf(
			"release service period schedule replacement savepoint: %w",
			err,
		)
	}

	return result, nil
}

func (m *MerchantFutureOfferingServicePeriodModel) replaceFutureScheduleTx(
	ctx context.Context,
	tx pgx.Tx,
	serviceTermID uuid.UUID,
	futureOfferingID uuid.UUID,
	windows []MerchantFutureOfferingServicePeriodWindow,
) (*MerchantFutureOfferingServicePeriodScheduleReplacement, error) {
	logger := m.Logger.GetLoggerWithContextFromContext(ctx).
		WithFunctionName("ReplaceMerchantFutureOfferingServicePeriodSchedule")

	if serviceTermID == uuid.Nil || futureOfferingID == uuid.Nil {
		return nil, ErrMerchantFutureOfferingServicePeriodInvalidInput
	}

	termStartsOn, termEndsOn, err := m.lockEstablishedServiceTermTx(
		ctx,
		tx,
		serviceTermID,
		futureOfferingID,
	)
	if err != nil {
		return nil, err
	}

	current, err := m.lockCurrentScheduleTx(
		ctx,
		tx,
		serviceTermID,
		futureOfferingID,
	)
	if err != nil {
		return nil, err
	}

	if len(current) == 0 {
		return nil, ErrMerchantFutureOfferingServicePeriodInvalidState
	}

	if err := validateCurrentMerchantFutureOfferingServicePeriodSchedule(
		termStartsOn,
		termEndsOn,
		current,
	); err != nil {
		logger.Error(
			"persisted current service period schedule is invalid",
			"error", err,
			"service_term_id", serviceTermID,
			"future_offering_id", futureOfferingID,
		)

		return nil, fmt.Errorf(
			"%w: %v",
			ErrMerchantFutureOfferingServicePeriodInvalidState,
			err,
		)
	}

	var currentDate time.Time
	if err := tx.QueryRow(ctx, `SELECT CURRENT_DATE`).Scan(&currentDate); err != nil {
		return nil, fmt.Errorf(
			"read database current date for service period replacement: %w",
			err,
		)
	}

	currentDate = normalizeMerchantFutureOfferingServicePeriodDate(currentDate)

	frozenCount := 0
	for _, period := range current {
		if period.PeriodStartsOn.After(currentDate) {
			break
		}

		frozenCount++
	}

	if frozenCount == len(current) {
		return nil, ErrMerchantFutureOfferingServicePeriodInvalidTransition
	}

	replacementStartsOn := termStartsOn
	if frozenCount > 0 {
		replacementStartsOn = current[frozenCount-1].PeriodEndsOn
	}

	numbered, err := validateAndNumberMerchantFutureOfferingServicePeriodWindows(
		replacementStartsOn,
		termEndsOn,
		frozenCount+1,
		windows,
	)
	if err != nil {
		return nil, fmt.Errorf(
			"%w: %v",
			ErrMerchantFutureOfferingServicePeriodInvalidSchedule,
			err,
		)
	}

	superseded := make(
		[]*MerchantFutureOfferingServicePeriod,
		0,
		len(current)-frozenCount,
	)

	const supersedeQuery = `
		UPDATE merchant_future_offering_service_periods AS p
		SET superseded_at = NOW()
		WHERE p.id = $1
			AND p.service_term_id = $2
			AND p.future_offering_id = $3
			AND p.superseded_at IS NULL
			AND p.period_starts_on > CURRENT_DATE
		RETURNING ` + merchantFutureOfferingServicePeriodSelectColumns

	for _, period := range current[frozenCount:] {
		supersededPeriod, err := scanMerchantFutureOfferingServicePeriod(
			tx.QueryRow(
				ctx,
				supersedeQuery,
				period.ID,
				serviceTermID,
				futureOfferingID,
			),
		)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return nil, ErrMerchantFutureOfferingServicePeriodMutationConflict
			}

			return nil, fmt.Errorf(
				"supersede merchant future offering service period %s: %w",
				period.ID,
				err,
			)
		}

		superseded = append(superseded, supersededPeriod)
	}

	established := make(
		[]*MerchantFutureOfferingServicePeriod,
		0,
		len(numbered),
	)

	for _, window := range numbered {
		period, err := m.insertServicePeriodTx(
			ctx,
			tx,
			serviceTermID,
			futureOfferingID,
			window,
		)
		if err != nil {
			return nil, err
		}

		established = append(established, period)
	}

	logger.Info(
		"future service period schedule replaced",
		"service_term_id", serviceTermID,
		"future_offering_id", futureOfferingID,
		"preserved_period_count", frozenCount,
		"superseded_period_count", len(superseded),
		"established_period_count", len(established),
	)

	return &MerchantFutureOfferingServicePeriodScheduleReplacement{
		Superseded:  superseded,
		Established: established,
	}, nil
}

// GetByID retrieves a Service Period by canonical ID, whether current or
// historical.
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

// GetCurrentAt retrieves the authoritative current Service Period covering the
// supplied date for a Future Offering.
//
// "Current" requires both a non-superseded Service Period and the Future
// Offering's currently established Service Term.
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

	atDate := normalizeMerchantFutureOfferingServicePeriodDate(at)

	const query = `
		SELECT ` + merchantFutureOfferingServicePeriodSelectColumns + `
		FROM merchant_future_offering_service_periods AS p
		JOIN merchant_future_offering_service_terms AS t
			ON t.id = p.service_term_id
			AND t.future_offering_id = p.future_offering_id
		WHERE p.future_offering_id = $1
			AND p.superseded_at IS NULL
			AND t.term_status = 'established'
			AND p.period_starts_on <= $2::date
			AND p.period_ends_on > $2::date
	`

	period, err := scanMerchantFutureOfferingServicePeriod(
		m.DB.QueryRow(ctx, query, futureOfferingID, atDate),
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

// ListCurrentSchedule returns the authoritative current Service Period schedule
// for the Future Offering's currently established Service Term.
func (m *MerchantFutureOfferingServicePeriodModel) ListCurrentSchedule(
	ctx context.Context,
	futureOfferingID uuid.UUID,
) ([]*MerchantFutureOfferingServicePeriod, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	if futureOfferingID == uuid.Nil {
		return nil, ErrMerchantFutureOfferingServicePeriodInvalidInput
	}

	const query = `
		SELECT ` + merchantFutureOfferingServicePeriodSelectColumns + `
		FROM merchant_future_offering_service_periods AS p
		JOIN merchant_future_offering_service_terms AS t
			ON t.id = p.service_term_id
			AND t.future_offering_id = p.future_offering_id
		WHERE p.future_offering_id = $1
			AND p.superseded_at IS NULL
			AND t.term_status = 'established'
		ORDER BY p.period_number ASC
	`

	rows, err := m.DB.Query(ctx, query, futureOfferingID)
	if err != nil {
		return nil, fmt.Errorf(
			"list current merchant future offering service period schedule: %w",
			err,
		)
	}

	periods, err := pgx.CollectRows(
		rows,
		func(row pgx.CollectableRow) (*MerchantFutureOfferingServicePeriod, error) {
			return scanMerchantFutureOfferingServicePeriod(row)
		},
	)
	if err != nil {
		return nil, fmt.Errorf(
			"collect current merchant future offering service period schedule: %w",
			err,
		)
	}

	return periods, nil
}

// ListTimelineForFutureOffering returns Service Period history across Service
// Term and schedule revisions using keyset pagination.
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
		(after.PeriodStartsOn.IsZero() || after.ID == uuid.Nil) {
		return nil, ErrMerchantFutureOfferingServicePeriodInvalidInput
	}

	if limit <= 0 {
		limit = merchantFutureOfferingServicePeriodTimelineDefaultLimit
	}

	if limit > merchantFutureOfferingServicePeriodTimelineMaxLimit {
		limit = merchantFutureOfferingServicePeriodTimelineMaxLimit
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
		afterStartsOn := normalizeMerchantFutureOfferingServicePeriodDate(
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
		func(row pgx.CollectableRow) (*MerchantFutureOfferingServicePeriod, error) {
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

// BeginTx begins a caller-owned Service Period transaction.
//
// Service-layer orchestration may use this boundary when Service Period
// persistence must commit atomically with other producer-owned domain work,
// including future outbox/event publication.
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
