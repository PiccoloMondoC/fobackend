// Package data provides the production data-layer implementation for
// Merchant Future Offering Billing Periods.
//
// sdworkspace/sdbackend/internal/data/merchant_future_offering_billing_periods.go
//
// GTM:
//
//	Layer: 2.4 Merchant / Future Offering Domain
//	Release Class: SPINE
//	Reason:
//	  Billing Periods are immutable, authoritative monthly accounting and
//	  consumption windows within an established Future Offering Service Term.
//	  They preserve the chronology against which applicable FO consumption is
//	  attributed for later monetization without conflating accounting windows
//	  with service performance, fee calculation, invoicing, funding, payment,
//	  settlement, or legacy subscription semantics.
//
// Future Offering Consumption Billing Model (FOCBM v1.0) Doctrine:
//
//	A Billing Period is one authoritative monthly accounting/consumption window
//	for one Future Offering under one Service Term.
//
//	Billing Period cadence and calendar-boundary meaning are Engineering
//	invariants. Boundaries are derived independently from the authoritative
//	Service Term start anchor by the database-owned
//	merchant_future_offering_billing_period_boundary function.
//
//	Billing Periods are created just in time. Persisted rows represent accounting
//	windows that have begun; they are not a pre-generated future schedule.
//
//	Billing Periods are immutable once inserted. Ordinary domain behavior has no
//	supersession, replacement, retirement, update, delete, soft-delete, restore,
//	or schedule-revision lifecycle. Exceptional correction belongs to separately
//	governed corrective-data tooling or migration.
//
// Domain Boundary:
//
//	Billing Period answers:
//
//	  "Over what authoritative monthly accounting window is applicable
//	   Future Offering consumption accumulated for billing?"
//
//	It does not determine QAE eligibility, measure AHO usage, calculate fees,
//	construct invoices, maintain FO balances, apply prepayments, select payment
//	methods, control financial exposure, or settle obligations.
//
//	Service Periods and Billing Periods are separate domain facts. They may have
//	identical date boundaries because both consume the same underlying monthly
//	calendar semantics from the same Service Term anchor, but Billing Period
//	identity, persistence, and lifecycle do not depend on Service Period rows.
//
// Transaction Ownership:
//
//	Creation methods accept caller-owned pgx.Tx values. They do not own the
//	caller's business transaction and do not publish events. Service orchestration
//	may compose Billing Period persistence atomically with producer-owned outbox
//	facts or other required domain work without this model knowing downstream
//	consumers or transport.
//
// SPINE Rule:
//
//	Keep compiling.
//	Keep production-ready.
//	Preserve immutable Billing Period history.
//	Preserve just-in-time, non-forecast creation.
//	Preserve monthly cadence and authoritative calendar semantics.
//	Preserve deterministic sequential period numbering and gapless chronology.
//	Preserve authoritative Service Term ownership and FO-wide non-overlap.
//	Preserve caller-owned transaction composition.
//	Preserve Billing Period independence from Service Period identity/lifecycle.
//	Never expose supersession, replacement, retirement, update, upsert, delete,
//	soft-delete, restore, or generic mutation capability.
//	Never embed QAE qualification, AHO measurement, fee calculation, invoice,
//	FO-account, funding, payment, settlement, transport, or consumer behavior.
//	Block deployment if this file breaks Billing Period identity, chronology,
//	concurrency, historical reconstruction, or transaction composability.
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
	merchantFutureOfferingBillingPeriodTimelineDefaultLimit = 20
	merchantFutureOfferingBillingPeriodTimelineMaxLimit     = 100
)

const (
	merchantFutureOfferingBillingPeriodNoOverlapConstraint = "excl_merchant_future_offering_billing_periods_no_overlap"

	merchantFutureOfferingBillingPeriodTermNumberConstraint = "ux_merchant_future_offering_billing_periods_term_number"

	merchantFutureOfferingBillingPeriodServiceTermFKConstraint = "fk_merchant_future_offering_billing_periods_service_term"
)

const merchantFutureOfferingBillingPeriodSelectColumns = `
	bp.id,
	bp.service_term_id,
	bp.future_offering_id,
	bp.period_number,
	bp.period_starts_on,
	bp.period_ends_on,
	bp.created_at`

// MerchantFutureOfferingBillingPeriod is the canonical persisted
// representation of one immutable Future Offering Billing Period.
type MerchantFutureOfferingBillingPeriod struct {
	ID               uuid.UUID `json:"id" db:"id"`
	ServiceTermID    uuid.UUID `json:"service_term_id" db:"service_term_id"`
	FutureOfferingID uuid.UUID `json:"future_offering_id" db:"future_offering_id"`
	PeriodNumber     int       `json:"period_number" db:"period_number"`
	PeriodStartsOn   time.Time `json:"period_starts_on" db:"period_starts_on"`
	PeriodEndsOn     time.Time `json:"period_ends_on" db:"period_ends_on"`
	CreatedAt        time.Time `json:"created_at" db:"created_at"`
}

// MerchantFutureOfferingBillingPeriodTimelineCursor identifies the row after
// which ListTimelineForFutureOffering continues.
type MerchantFutureOfferingBillingPeriodTimelineCursor struct {
	PeriodStartsOn time.Time
	ID             uuid.UUID
}

// MerchantFutureOfferingBillingPeriodModel manages immutable Billing Period
// persistence and retrieval.
//
// Billing Period creation is intentionally transaction-only. The service layer
// owns the larger producer workflow and may need to commit the Billing Period
// fact atomically with an outbox event or other producer-owned work.
type MerchantFutureOfferingBillingPeriodModel struct {
	DB     *pgxpool.Pool
	Logger *logging.Logger
}

func scanMerchantFutureOfferingBillingPeriod(
	row scannableRow,
) (*MerchantFutureOfferingBillingPeriod, error) {
	var period MerchantFutureOfferingBillingPeriod

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

// normalizeMerchantFutureOfferingBillingPeriodDate converts a domain date to
// the package's canonical UTC-midnight representation while preserving the
// caller-supplied Year/Month/Day fields.
//
// Billing Period boundaries are calendar dates, not elapsed-time instants.
func normalizeMerchantFutureOfferingBillingPeriodDate(
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

func (m *MerchantFutureOfferingBillingPeriodModel) lockEstablishedServiceTermTx(
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
				ErrMerchantFutureOfferingBillingPeriodServiceTermNotFound
		}

		return time.Time{}, time.Time{}, fmt.Errorf(
			"lock service term for merchant future offering billing period: %w",
			err,
		)
	}

	if status != string(MerchantFutureOfferingServiceTermStatusEstablished) ||
		startsOn == nil ||
		endsOn == nil {
		return time.Time{},
			time.Time{},
			ErrMerchantFutureOfferingBillingPeriodInvalidState
	}

	termStartsOn := normalizeMerchantFutureOfferingBillingPeriodDate(*startsOn)
	termEndsOn := normalizeMerchantFutureOfferingBillingPeriodDate(*endsOn)

	if !termEndsOn.After(termStartsOn) {
		return time.Time{},
			time.Time{},
			ErrMerchantFutureOfferingBillingPeriodInvalidState
	}

	return termStartsOn, termEndsOn, nil
}

// getLatestForServiceTermTx returns the highest-numbered Billing Period for the
// caller-locked Service Term. It returns nil, nil when no period exists.
//
// The owning Service Term row is the serialization lock for Billing Period
// creation, so this read does not acquire a second row lock on the immutable
// Billing Period itself.
func (m *MerchantFutureOfferingBillingPeriodModel) getLatestForServiceTermTx(
	ctx context.Context,
	tx pgx.Tx,
	serviceTermID uuid.UUID,
	futureOfferingID uuid.UUID,
) (*MerchantFutureOfferingBillingPeriod, error) {
	const query = `
		SELECT ` + merchantFutureOfferingBillingPeriodSelectColumns + `
		FROM merchant_future_offering_billing_periods AS bp
		WHERE bp.service_term_id = $1
			AND bp.future_offering_id = $2
		ORDER BY bp.period_number DESC
		LIMIT 1
	`

	period, err := scanMerchantFutureOfferingBillingPeriod(
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
			"get latest merchant future offering billing period: %w",
			err,
		)
	}

	return period, nil
}

// getBoundaryDueStateTx invokes the authoritative database Billing Period
// boundary function and compares that boundary to PostgreSQL CURRENT_DATE.
//
// The comparison deliberately uses the same database-owned calendar date as the
// authoritative insert trigger. It does not compare against the application
// clock, avoiding an application/database boundary disagreement around UTC day
// changes or clock skew.
func (m *MerchantFutureOfferingBillingPeriodModel) getBoundaryDueStateTx(
	ctx context.Context,
	tx pgx.Tx,
	anchor time.Time,
	monthOffset int,
) (time.Time, bool, error) {
	if monthOffset < 0 {
		return time.Time{},
			false,
			ErrMerchantFutureOfferingBillingPeriodInvalidInput
	}

	const query = `
		SELECT
			merchant_future_offering_billing_period_boundary(
				$1::date,
				$2::integer
			),
			merchant_future_offering_billing_period_boundary(
				$1::date,
				$2::integer
			) <= CURRENT_DATE
	`

	var (
		boundary time.Time
		due      bool
	)

	if err := tx.QueryRow(
		ctx,
		query,
		anchor,
		monthOffset,
	).Scan(
		&boundary,
		&due,
	); err != nil {
		return time.Time{}, false, fmt.Errorf(
			"resolve merchant future offering billing period boundary: %w",
			err,
		)
	}

	return normalizeMerchantFutureOfferingBillingPeriodDate(boundary),
		due,
		nil
}

func translateMerchantFutureOfferingBillingPeriodWriteError(err error) error {
	switch {
	case IsPgConstraint(
		err,
		merchantFutureOfferingBillingPeriodNoOverlapConstraint,
	):
		return ErrMerchantFutureOfferingBillingPeriodOverlap

	case IsPgConstraint(
		err,
		merchantFutureOfferingBillingPeriodTermNumberConstraint,
	):
		return ErrMerchantFutureOfferingBillingPeriodNumberConflict

	case IsPgConstraint(
		err,
		merchantFutureOfferingBillingPeriodServiceTermFKConstraint,
	):
		return ErrMerchantFutureOfferingBillingPeriodServiceTermNotFound

	case IsCheckViolation(err):
		return ErrMerchantFutureOfferingBillingPeriodInvalidInput

	case IsExclusionViolation(err):
		return ErrMerchantFutureOfferingBillingPeriodOverlap

	case IsForeignKeyViolation(err):
		return ErrMerchantFutureOfferingBillingPeriodServiceTermNotFound

	case IsUniqueViolation(err):
		// The known logical uniqueness conflict is handled above by exact
		// constraint name. Any other unique violation is not safe to classify as
		// a period-number conflict merely because it shares SQLSTATE 23505.
		return ErrMerchantFutureOfferingBillingPeriodPersistenceConflict

	default:
		return err
	}
}

func (m *MerchantFutureOfferingBillingPeriodModel) insertBillingPeriodTx(
	ctx context.Context,
	tx pgx.Tx,
	serviceTermID uuid.UUID,
	futureOfferingID uuid.UUID,
	periodNumber int,
) (*MerchantFutureOfferingBillingPeriod, error) {
	const query = `
		INSERT INTO merchant_future_offering_billing_periods AS bp (
			service_term_id,
			future_offering_id,
			period_number,
			period_starts_on,
			period_ends_on
		)
		SELECT
			t.id,
			t.future_offering_id,
			$3::integer,
			merchant_future_offering_billing_period_boundary(
				t.term_starts_on,
				$3::integer - 1
			),
			LEAST(
				merchant_future_offering_billing_period_boundary(
					t.term_starts_on,
					$3::integer
				),
				t.term_ends_on
			)
		FROM merchant_future_offering_service_terms AS t
		WHERE t.id = $1
			AND t.future_offering_id = $2
		RETURNING ` + merchantFutureOfferingBillingPeriodSelectColumns

	period, err := scanMerchantFutureOfferingBillingPeriod(
		tx.QueryRow(
			ctx,
			query,
			serviceTermID,
			futureOfferingID,
			periodNumber,
		),
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil,
				ErrMerchantFutureOfferingBillingPeriodServiceTermNotFound
		}

		translated := translateMerchantFutureOfferingBillingPeriodWriteError(err)
		if translated != err {
			return nil, translated
		}

		// Plain PL/pgSQL RAISE EXCEPTION paths currently share SQLSTATE P0001.
		// Do not parse human-readable trigger messages here. Known ordinary
		// rejection conditions are preclassified before insertion while holding
		// the same Service Term serialization lock; any remaining trigger failure
		// is therefore a stable persistence conflict at this boundary.
		if IsPgErrorCode(err, "P0001") {
			return nil,
				ErrMerchantFutureOfferingBillingPeriodPersistenceConflict
		}

		return nil, fmt.Errorf(
			"insert merchant future offering billing period: %w",
			err,
		)
	}

	return period, nil
}

func (m *MerchantFutureOfferingBillingPeriodModel) createBillingPeriodTx(
	ctx context.Context,
	tx pgx.Tx,
	serviceTermID uuid.UUID,
	futureOfferingID uuid.UUID,
	requireExisting bool,
	functionName string,
) (*MerchantFutureOfferingBillingPeriod, error) {
	logger := m.Logger.GetLoggerWithContextFromContext(ctx).
		WithFunctionName(functionName)

	termStartsOn, termEndsOn, err := m.lockEstablishedServiceTermTx(
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

	var nextPeriodNumber int

	switch {
	case requireExisting && latest == nil:
		return nil, ErrMerchantFutureOfferingBillingPeriodInvalidState

	case !requireExisting && latest != nil:
		return nil, ErrMerchantFutureOfferingBillingPeriodNumberConflict

	case latest == nil:
		nextPeriodNumber = 1

	default:
		if latest.PeriodNumber <= 0 {
			return nil, ErrMerchantFutureOfferingBillingPeriodInvalidState
		}

		if latest.PeriodNumber >= MerchantFutureOfferingServiceTermMaxDurationMonths {
			return nil, ErrMerchantFutureOfferingBillingPeriodTermExhausted
		}

		nextPeriodNumber = latest.PeriodNumber + 1
	}

	expectedStartsOn, due, err := m.getBoundaryDueStateTx(
		ctx,
		tx,
		termStartsOn,
		nextPeriodNumber-1,
	)
	if err != nil {
		return nil, err
	}

	if !expectedStartsOn.Before(termEndsOn) {
		return nil, ErrMerchantFutureOfferingBillingPeriodTermExhausted
	}

	if !due {
		return nil, ErrMerchantFutureOfferingBillingPeriodNotYetDue
	}

	if latest != nil {
		latestEndsOn := normalizeMerchantFutureOfferingBillingPeriodDate(
			latest.PeriodEndsOn,
		)
		if !latestEndsOn.Equal(expectedStartsOn) {
			logger.Error(
				"merchant future offering billing period chronology is not contiguous",
				"service_term_id", serviceTermID,
				"future_offering_id", futureOfferingID,
				"latest_period_number", latest.PeriodNumber,
				"latest_period_ends_on", latestEndsOn,
				"expected_next_starts_on", expectedStartsOn,
			)
			return nil, ErrMerchantFutureOfferingBillingPeriodInvalidSchedule
		}
	}

	period, err := m.insertBillingPeriodTx(
		ctx,
		tx,
		serviceTermID,
		futureOfferingID,
		nextPeriodNumber,
	)
	if err != nil {
		logger.Error(
			"insert merchant future offering billing period failed",
			"error", err,
			"service_term_id", serviceTermID,
			"future_offering_id", futureOfferingID,
			"period_number", nextPeriodNumber,
		)
		return nil, err
	}

	logger.Info(
		"merchant future offering billing period created",
		"billing_period_id", period.ID,
		"service_term_id", period.ServiceTermID,
		"future_offering_id", period.FutureOfferingID,
		"period_number", period.PeriodNumber,
		"period_starts_on", period.PeriodStartsOn,
		"period_ends_on", period.PeriodEndsOn,
	)

	return period, nil
}

// CreateFirstPeriodTx creates period 1 for an established Service Term inside
// a caller-owned transaction.
//
// The caller supplies no dates. PostgreSQL derives the authoritative monthly
// window from the Service Term anchor and rejects future pre-generation. The
// method serializes creation through the owning Service Term row.
func (m *MerchantFutureOfferingBillingPeriodModel) CreateFirstPeriodTx(
	ctx context.Context,
	tx pgx.Tx,
	serviceTermID uuid.UUID,
	futureOfferingID uuid.UUID,
) (*MerchantFutureOfferingBillingPeriod, error) {
	if tx == nil ||
		serviceTermID == uuid.Nil ||
		futureOfferingID == uuid.Nil {
		return nil, ErrMerchantFutureOfferingBillingPeriodInvalidInput
	}

	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	nestedTx, err := tx.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf(
			"begin first billing period creation savepoint: %w",
			err,
		)
	}
	defer nestedTx.Rollback(ctx)

	period, err := m.createBillingPeriodTx(
		ctx,
		nestedTx,
		serviceTermID,
		futureOfferingID,
		false,
		"CreateFirstMerchantFutureOfferingBillingPeriod",
	)
	if err != nil {
		return nil, err
	}

	if err := nestedTx.Commit(ctx); err != nil {
		return nil, fmt.Errorf(
			"release first billing period creation savepoint: %w",
			err,
		)
	}

	return period, nil
}

// CreateNextPeriodTx creates exactly one next immutable Billing Period inside
// an established Service Term.
//
// The next period number is deterministic. Its window is derived by PostgreSQL
// from the original Service Term anchor, must begin exactly where the persisted
// chronology requires, is shortened at term_ends_on when necessary, and cannot
// be created before its authoritative start date.
func (m *MerchantFutureOfferingBillingPeriodModel) CreateNextPeriodTx(
	ctx context.Context,
	tx pgx.Tx,
	serviceTermID uuid.UUID,
	futureOfferingID uuid.UUID,
) (*MerchantFutureOfferingBillingPeriod, error) {
	if tx == nil ||
		serviceTermID == uuid.Nil ||
		futureOfferingID == uuid.Nil {
		return nil, ErrMerchantFutureOfferingBillingPeriodInvalidInput
	}

	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	nestedTx, err := tx.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf(
			"begin next billing period creation savepoint: %w",
			err,
		)
	}
	defer nestedTx.Rollback(ctx)

	period, err := m.createBillingPeriodTx(
		ctx,
		nestedTx,
		serviceTermID,
		futureOfferingID,
		true,
		"CreateNextMerchantFutureOfferingBillingPeriod",
	)
	if err != nil {
		return nil, err
	}

	if err := nestedTx.Commit(ctx); err != nil {
		return nil, fmt.Errorf(
			"release next billing period creation savepoint: %w",
			err,
		)
	}

	return period, nil
}

// GetByID retrieves one immutable Billing Period by canonical ID.
func (m *MerchantFutureOfferingBillingPeriodModel) GetByID(
	ctx context.Context,
	id uuid.UUID,
) (*MerchantFutureOfferingBillingPeriod, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	if id == uuid.Nil {
		return nil, ErrMerchantFutureOfferingBillingPeriodNotFound
	}

	const query = `
		SELECT ` + merchantFutureOfferingBillingPeriodSelectColumns + `
		FROM merchant_future_offering_billing_periods AS bp
		WHERE bp.id = $1
	`

	period, err := scanMerchantFutureOfferingBillingPeriod(
		m.DB.QueryRow(ctx, query, id),
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrMerchantFutureOfferingBillingPeriodNotFound
		}

		return nil, fmt.Errorf(
			"get merchant future offering billing period: %w",
			err,
		)
	}

	return period, nil
}

// GetForServiceTermAt retrieves the Billing Period whose half-open window
// contains the supplied domain date for an explicitly identified Service Term.
//
// This is the historical reconstruction lookup. The caller identifies the
// Service Term rather than relying on whichever term is currently established.
func (m *MerchantFutureOfferingBillingPeriodModel) GetForServiceTermAt(
	ctx context.Context,
	serviceTermID uuid.UUID,
	futureOfferingID uuid.UUID,
	at time.Time,
) (*MerchantFutureOfferingBillingPeriod, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	if serviceTermID == uuid.Nil ||
		futureOfferingID == uuid.Nil ||
		at.IsZero() {
		return nil, ErrMerchantFutureOfferingBillingPeriodInvalidInput
	}

	atDate := normalizeMerchantFutureOfferingBillingPeriodDate(at)

	const query = `
		SELECT ` + merchantFutureOfferingBillingPeriodSelectColumns + `
		FROM merchant_future_offering_billing_periods AS bp
		WHERE bp.service_term_id = $1
			AND bp.future_offering_id = $2
			AND daterange(
				bp.period_starts_on,
				bp.period_ends_on,
				'[)'
			) @> $3::date
	`

	period, err := scanMerchantFutureOfferingBillingPeriod(
		m.DB.QueryRow(
			ctx,
			query,
			serviceTermID,
			futureOfferingID,
			atDate,
		),
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrMerchantFutureOfferingBillingPeriodNotFound
		}

		return nil, fmt.Errorf(
			"get merchant future offering billing period for service term at date: %w",
			err,
		)
	}

	return period, nil
}

// GetCurrentAt retrieves the Billing Period containing the supplied domain date
// when its owning Service Term is presently established.
//
// This method answers operational Billing Period authority. Historical
// reconstruction should use GetForServiceTermAt with an explicit Service Term.
func (m *MerchantFutureOfferingBillingPeriodModel) GetCurrentAt(
	ctx context.Context,
	futureOfferingID uuid.UUID,
	at time.Time,
) (*MerchantFutureOfferingBillingPeriod, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	if futureOfferingID == uuid.Nil || at.IsZero() {
		return nil, ErrMerchantFutureOfferingBillingPeriodInvalidInput
	}

	atDate := normalizeMerchantFutureOfferingBillingPeriodDate(at)

	const query = `
		SELECT ` + merchantFutureOfferingBillingPeriodSelectColumns + `
		FROM merchant_future_offering_billing_periods AS bp
		JOIN merchant_future_offering_service_terms AS st
			ON st.id = bp.service_term_id
			AND st.future_offering_id = bp.future_offering_id
		WHERE bp.future_offering_id = $1
			AND st.term_status = $2
			AND daterange(
				bp.period_starts_on,
				bp.period_ends_on,
				'[)'
			) @> $3::date
	`

	period, err := scanMerchantFutureOfferingBillingPeriod(
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
			return nil, ErrMerchantFutureOfferingBillingPeriodNotFound
		}

		return nil, fmt.Errorf(
			"get current merchant future offering billing period at date: %w",
			err,
		)
	}

	return period, nil
}

// ListForServiceTerm returns all Billing Periods created for one Service Term in
// canonical period-number order.
//
// The query is schema-bounded: period_number is restricted to 1..1188 and is
// unique per Service Term, so this scope can never exceed 1188 rows.
func (m *MerchantFutureOfferingBillingPeriodModel) ListForServiceTerm(
	ctx context.Context,
	serviceTermID uuid.UUID,
	futureOfferingID uuid.UUID,
) ([]*MerchantFutureOfferingBillingPeriod, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	if serviceTermID == uuid.Nil ||
		futureOfferingID == uuid.Nil {
		return nil, ErrMerchantFutureOfferingBillingPeriodInvalidInput
	}

	const query = `
		SELECT ` + merchantFutureOfferingBillingPeriodSelectColumns + `
		FROM merchant_future_offering_billing_periods AS bp
		WHERE bp.service_term_id = $1
			AND bp.future_offering_id = $2
		ORDER BY bp.period_number ASC
	`

	rows, err := m.DB.Query(
		ctx,
		query,
		serviceTermID,
		futureOfferingID,
	)
	if err != nil {
		return nil, fmt.Errorf(
			"list merchant future offering billing periods for service term: %w",
			err,
		)
	}

	periods, err := pgx.CollectRows(
		rows,
		func(
			row pgx.CollectableRow,
		) (*MerchantFutureOfferingBillingPeriod, error) {
			return scanMerchantFutureOfferingBillingPeriod(row)
		},
	)
	if err != nil {
		return nil, fmt.Errorf(
			"collect merchant future offering billing periods for service term: %w",
			err,
		)
	}

	return periods, nil
}

// ListTimelineForFutureOffering returns Billing Period history across Service
// Term revisions using bounded keyset pagination.
func (m *MerchantFutureOfferingBillingPeriodModel) ListTimelineForFutureOffering(
	ctx context.Context,
	futureOfferingID uuid.UUID,
	limit int,
	after *MerchantFutureOfferingBillingPeriodTimelineCursor,
) ([]*MerchantFutureOfferingBillingPeriod, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	if futureOfferingID == uuid.Nil {
		return nil, ErrMerchantFutureOfferingBillingPeriodInvalidInput
	}

	if after != nil &&
		(after.PeriodStartsOn.IsZero() || after.ID == uuid.Nil) {
		return nil, ErrMerchantFutureOfferingBillingPeriodInvalidInput
	}

	if limit <= 0 {
		limit = merchantFutureOfferingBillingPeriodTimelineDefaultLimit
	}
	if limit > merchantFutureOfferingBillingPeriodTimelineMaxLimit {
		limit = merchantFutureOfferingBillingPeriodTimelineMaxLimit
	}

	var (
		rows pgx.Rows
		err  error
	)

	if after == nil {
		const query = `
			SELECT ` + merchantFutureOfferingBillingPeriodSelectColumns + `
			FROM merchant_future_offering_billing_periods AS bp
			WHERE bp.future_offering_id = $1
			ORDER BY bp.period_starts_on ASC, bp.id ASC
			LIMIT $2
		`

		rows, err = m.DB.Query(
			ctx,
			query,
			futureOfferingID,
			limit,
		)
	} else {
		afterStartsOn := normalizeMerchantFutureOfferingBillingPeriodDate(
			after.PeriodStartsOn,
		)

		const query = `
			SELECT ` + merchantFutureOfferingBillingPeriodSelectColumns + `
			FROM merchant_future_offering_billing_periods AS bp
			WHERE bp.future_offering_id = $1
				AND (bp.period_starts_on, bp.id) > ($2, $3)
			ORDER BY bp.period_starts_on ASC, bp.id ASC
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
			"list merchant future offering billing period timeline: %w",
			err,
		)
	}

	periods, err := pgx.CollectRows(
		rows,
		func(
			row pgx.CollectableRow,
		) (*MerchantFutureOfferingBillingPeriod, error) {
			return scanMerchantFutureOfferingBillingPeriod(row)
		},
	)
	if err != nil {
		return nil, fmt.Errorf(
			"collect merchant future offering billing period timeline: %w",
			err,
		)
	}

	return periods, nil
}

// BeginTx begins a caller-owned transaction for Billing Period workflow
// composition.
//
// The service layer owns commit/rollback and may compose Billing Period
// persistence atomically with OutboxEventModel.InsertTx or other producer-owned
// work. This method does not imply that the Billing Period model owns the
// business transaction.
func (m *MerchantFutureOfferingBillingPeriodModel) BeginTx(
	ctx context.Context,
) (pgx.Tx, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	tx, err := m.DB.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf(
			"begin merchant future offering billing period transaction: %w",
			err,
		)
	}

	return tx, nil
}
