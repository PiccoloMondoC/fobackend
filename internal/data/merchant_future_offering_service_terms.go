// Package data provides the production data-layer implementation for
// Merchant Future Offering Service Terms.
//
// sdworkspace/sdbackend/internal/data/merchant_future_offering_service_terms.go
//
// GTM:
//
//	Layer: 2.4 Merchant / Future Offering Domain
//	Release Class: SPINE
//	Reason:
//	  Service Terms preserve the overall agreed duration of Sagrenti service
//	  for individual Future Offerings. They form the authoritative duration
//	  boundary beneath later Service Period, Billing Period, and Payment
//	  Period capabilities without collapsing those domains together.
//
// Future Offering v1 Doctrine:
//
//	A Service Term is the overall agreed duration for which Sagrenti provides
//	service to one Future Offering.
//
//	It is not a merchant-account subscription, Service Period, Billing Period,
//	or Payment Period.
//
//	This file owns Service Term persistence and Service Term lifecycle
//	integrity only. It must not acquire knowledge of downstream domain
//	implementation merely because other capabilities may consume Service Term
//	facts.
//
// SPINE Rule:
//
//	Keep compiling.
//	Keep production-ready.
//	Preserve proposed -> established -> superseded lifecycle integrity.
//	Preserve established commercial facts.
//	Preserve explicit, auditable replacement rather than in-place amendment.
//	Preserve the Engineering duration envelope of >0 and <=1188 months.
//	Never hard-code Administration's current operating-duration policy.
//	Block deployment if this file breaks Service Term identity, concurrency,
//	historical integrity, or replacement atomicity.
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

// MerchantFutureOfferingServiceTermStatus identifies the canonical persisted
// lifecycle state of a Future Offering Service Term.
type MerchantFutureOfferingServiceTermStatus string

const (
	// MerchantFutureOfferingServiceTermStatusProposed identifies a Service
	// Term that has not yet become an authoritative commercial agreement.
	MerchantFutureOfferingServiceTermStatusProposed MerchantFutureOfferingServiceTermStatus = "proposed"

	// MerchantFutureOfferingServiceTermStatusEstablished identifies the
	// currently authoritative Service Term for a Future Offering.
	MerchantFutureOfferingServiceTermStatusEstablished MerchantFutureOfferingServiceTermStatus = "established"

	// MerchantFutureOfferingServiceTermStatusSuperseded identifies a
	// historical Service Term replaced by a later authoritative Service Term.
	MerchantFutureOfferingServiceTermStatusSuperseded MerchantFutureOfferingServiceTermStatus = "superseded"
)

// MerchantFutureOfferingServiceTermMaxDurationMonths is the absolute
// Engineering-supported Service Term duration ceiling.
//
// Administration may configure narrower operating limits but may not widen
// this invariant boundary.
const MerchantFutureOfferingServiceTermMaxDurationMonths = 1188

const (
	merchantFutureOfferingServiceTermTimelineDefaultLimit = 20
	merchantFutureOfferingServiceTermTimelineMaxLimit     = 100
)

const merchantFutureOfferingServiceTermSelectColumns = `
	id,
	future_offering_id,
	duration_months,
	term_starts_on,
	term_ends_on,
	term_status,
	supersedes_service_term_id,
	established_at,
	superseded_at,
	created_at,
	updated_at`

// MerchantFutureOfferingServiceTerm is the canonical persisted representation
// of a Future Offering Service Term.
type MerchantFutureOfferingServiceTerm struct {
	ID                      uuid.UUID                               `json:"id" db:"id"`
	FutureOfferingID        uuid.UUID                               `json:"future_offering_id" db:"future_offering_id"`
	DurationMonths          int                                     `json:"duration_months" db:"duration_months"`
	TermStartsOn            *time.Time                              `json:"term_starts_on" db:"term_starts_on"`
	TermEndsOn              *time.Time                              `json:"term_ends_on" db:"term_ends_on"`
	TermStatus              MerchantFutureOfferingServiceTermStatus `json:"term_status" db:"term_status"`
	SupersedesServiceTermID *uuid.UUID                              `json:"supersedes_service_term_id" db:"supersedes_service_term_id"`
	EstablishedAt           *time.Time                              `json:"established_at" db:"established_at"`
	SupersededAt            *time.Time                              `json:"superseded_at" db:"superseded_at"`
	CreatedAt               time.Time                               `json:"created_at" db:"created_at"`
	UpdatedAt               time.Time                               `json:"updated_at" db:"updated_at"`
}

// TermStartsOn and TermEndsOn are optional draft values while the Service Term
// remains proposed. When both are supplied, TermEndsOn must be strictly after
// TermStartsOn.
//
// Proposed dates are not authoritative commercial facts and do not implicitly
// become the established service window. Establish/ReplaceEstablished receive
// the authoritative dates explicitly so the service layer can validate the
// final commercial arrangement immediately before establishment.
//
// These PostgreSQL DATE-semantic values are normalized to calendar dates before
// persistence. Their time-of-day and timezone components are not domain facts.
type MerchantFutureOfferingServiceTermProposal struct {
	DurationMonths int
	TermStartsOn   *time.Time
	TermEndsOn     *time.Time
}

// MerchantFutureOfferingServiceTermTimelineCursor identifies the row after
// which ListTimeline should continue when keyset pagination is used.
type MerchantFutureOfferingServiceTermTimelineCursor struct {
	CreatedAt time.Time
	ID        uuid.UUID
}

// MerchantFutureOfferingServiceTermModel manages Future Offering Service Term
// persistence and lifecycle transitions.
type MerchantFutureOfferingServiceTermModel struct {
	DB     *pgxpool.Pool
	Logger *logging.Logger
}

type merchantFutureOfferingServiceTermQueryRower interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// scanMerchantFutureOfferingServiceTerm hydrates a Service Term through the
// shared package scanner contract.
func scanMerchantFutureOfferingServiceTerm(row scannableRow) (*MerchantFutureOfferingServiceTerm, error) {
	var term MerchantFutureOfferingServiceTerm
	var status string

	if err := row.Scan(
		&term.ID,
		&term.FutureOfferingID,
		&term.DurationMonths,
		&term.TermStartsOn,
		&term.TermEndsOn,
		&status,
		&term.SupersedesServiceTermID,
		&term.EstablishedAt,
		&term.SupersededAt,
		&term.CreatedAt,
		&term.UpdatedAt,
	); err != nil {
		return nil, err
	}

	term.TermStatus = MerchantFutureOfferingServiceTermStatus(status)

	return &term, nil
}

func validateMerchantFutureOfferingServiceTermDuration(durationMonths int) error {
	if durationMonths <= 0 {
		return errors.New("duration_months must be greater than zero")
	}

	if durationMonths > MerchantFutureOfferingServiceTermMaxDurationMonths {
		return fmt.Errorf(
			"duration_months must not exceed %d",
			MerchantFutureOfferingServiceTermMaxDurationMonths,
		)
	}

	return nil
}

func normalizeMerchantFutureOfferingServiceTermDate(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}

	normalized := time.Date(
		value.Year(),
		value.Month(),
		value.Day(),
		0,
		0,
		0,
		0,
		time.UTC,
	)

	return &normalized
}

func normalizeMerchantFutureOfferingServiceTermProposal(
	proposal MerchantFutureOfferingServiceTermProposal,
) (MerchantFutureOfferingServiceTermProposal, error) {
	if err := validateMerchantFutureOfferingServiceTermDuration(proposal.DurationMonths); err != nil {
		return MerchantFutureOfferingServiceTermProposal{}, err
	}

	proposal.TermStartsOn = normalizeMerchantFutureOfferingServiceTermDate(proposal.TermStartsOn)
	proposal.TermEndsOn = normalizeMerchantFutureOfferingServiceTermDate(proposal.TermEndsOn)

	if proposal.TermStartsOn != nil &&
		proposal.TermEndsOn != nil &&
		!proposal.TermEndsOn.After(*proposal.TermStartsOn) {
		return MerchantFutureOfferingServiceTermProposal{}, errors.New(
			"term_ends_on must be after term_starts_on",
		)
	}

	return proposal, nil
}

func normalizeMerchantFutureOfferingServiceTermWindow(
	termStartsOn time.Time,
	termEndsOn time.Time,
) (time.Time, time.Time, error) {
	startsOn := time.Date(
		termStartsOn.Year(),
		termStartsOn.Month(),
		termStartsOn.Day(),
		0,
		0,
		0,
		0,
		time.UTC,
	)

	endsOn := time.Date(
		termEndsOn.Year(),
		termEndsOn.Month(),
		termEndsOn.Day(),
		0,
		0,
		0,
		0,
		time.UTC,
	)

	if termStartsOn.IsZero() {
		return time.Time{}, time.Time{}, errors.New("term_starts_on is required")
	}

	if termEndsOn.IsZero() {
		return time.Time{}, time.Time{}, errors.New("term_ends_on is required")
	}

	if !endsOn.After(startsOn) {
		return time.Time{}, time.Time{}, errors.New(
			"term_ends_on must be after term_starts_on",
		)
	}

	return startsOn, endsOn, nil
}

// Propose creates a proposed Service Term using the pooled database connection.
func (m *MerchantFutureOfferingServiceTermModel) Propose(
	ctx context.Context,
	futureOfferingID uuid.UUID,
	proposal MerchantFutureOfferingServiceTermProposal,
) (*MerchantFutureOfferingServiceTerm, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	return m.propose(ctx, m.DB, futureOfferingID, proposal)
}

// ProposeTx creates a proposed Service Term within a caller-owned transaction.
func (m *MerchantFutureOfferingServiceTermModel) ProposeTx(
	ctx context.Context,
	tx pgx.Tx,
	futureOfferingID uuid.UUID,
	proposal MerchantFutureOfferingServiceTermProposal,
) (*MerchantFutureOfferingServiceTerm, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	return m.propose(ctx, tx, futureOfferingID, proposal)
}

func (m *MerchantFutureOfferingServiceTermModel) propose(
	ctx context.Context,
	db merchantFutureOfferingServiceTermQueryRower,
	futureOfferingID uuid.UUID,
	proposal MerchantFutureOfferingServiceTermProposal,
) (*MerchantFutureOfferingServiceTerm, error) {
	logger := m.Logger.GetLoggerWithContextFromContext(ctx).
		WithFunctionName("ProposeMerchantFutureOfferingServiceTerm")

	if futureOfferingID == uuid.Nil {
		err := errors.New("future offering id is required")
		logger.Error("propose service term validation failed", "error", err)
		return nil, fmt.Errorf(
			"%w: %v",
			ErrMerchantFutureOfferingServiceTermInvalidInput,
			err,
		)
	}

	normalized, err := normalizeMerchantFutureOfferingServiceTermProposal(proposal)
	if err != nil {
		logger.Error(
			"propose service term validation failed",
			"error", err,
			"future_offering_id", futureOfferingID,
		)
		return nil, fmt.Errorf(
			"%w: %v",
			ErrMerchantFutureOfferingServiceTermInvalidInput,
			err,
		)
	}

	const query = `
		INSERT INTO merchant_future_offering_service_terms (
			future_offering_id,
			duration_months,
			term_starts_on,
			term_ends_on
		)
		VALUES ($1, $2, $3, $4)
		RETURNING ` + merchantFutureOfferingServiceTermSelectColumns

	term, err := scanMerchantFutureOfferingServiceTerm(
		db.QueryRow(
			ctx,
			query,
			futureOfferingID,
			normalized.DurationMonths,
			normalized.TermStartsOn,
			normalized.TermEndsOn,
		),
	)
	if err != nil {
		switch {
		case IsPgConstraint(
			err,
			"ux_merchant_future_offering_service_terms_one_proposed",
		):
			logger.Warn(
				"proposed service term already exists",
				"future_offering_id", futureOfferingID,
			)
			return nil, ErrMerchantFutureOfferingServiceTermProposedAlreadyExists

		case IsForeignKeyViolation(err):
			logger.Warn(
				"future offering does not exist for proposed service term",
				"future_offering_id", futureOfferingID,
			)
			return nil, ErrMerchantFutureOfferingServiceTermFutureOfferingNotFound

		case IsCheckViolation(err):
			logger.Error(
				"proposed service term violated persisted integrity constraint",
				"error", err,
				"future_offering_id", futureOfferingID,
			)
			return nil, ErrMerchantFutureOfferingServiceTermInvalidState

		default:
			logger.Error(
				"propose service term failed",
				"error", err,
				"future_offering_id", futureOfferingID,
			)
			return nil, fmt.Errorf(
				"propose merchant future offering service term: %w",
				err,
			)
		}
	}

	logger.Info(
		"service term proposed",
		"service_term_id", term.ID,
		"future_offering_id", term.FutureOfferingID,
	)

	return term, nil
}

// GetByID retrieves a Service Term by canonical ID.
func (m *MerchantFutureOfferingServiceTermModel) GetByID(
	ctx context.Context,
	id uuid.UUID,
) (*MerchantFutureOfferingServiceTerm, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).
		WithFunctionName("GetMerchantFutureOfferingServiceTermByID")

	if id == uuid.Nil {
		return nil, ErrMerchantFutureOfferingServiceTermNotFound
	}

	const query = `
		SELECT ` + merchantFutureOfferingServiceTermSelectColumns + `
		FROM merchant_future_offering_service_terms
		WHERE id = $1
	`

	term, err := scanMerchantFutureOfferingServiceTerm(
		m.DB.QueryRow(ctx, query, id),
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrMerchantFutureOfferingServiceTermNotFound
		}

		logger.Error(
			"get service term failed",
			"error", err,
			"service_term_id", id,
		)
		return nil, fmt.Errorf(
			"get merchant future offering service term: %w",
			err,
		)
	}

	return term, nil
}

// GetByIDForFutureOffering retrieves a Service Term only when it belongs to
// the supplied Future Offering.
func (m *MerchantFutureOfferingServiceTermModel) GetByIDForFutureOffering(
	ctx context.Context,
	id uuid.UUID,
	futureOfferingID uuid.UUID,
) (*MerchantFutureOfferingServiceTerm, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).
		WithFunctionName("GetMerchantFutureOfferingServiceTermByIDForFutureOffering")

	if id == uuid.Nil || futureOfferingID == uuid.Nil {
		return nil, ErrMerchantFutureOfferingServiceTermNotFound
	}

	const query = `
		SELECT ` + merchantFutureOfferingServiceTermSelectColumns + `
		FROM merchant_future_offering_service_terms
		WHERE id = $1
			AND future_offering_id = $2
	`

	term, err := scanMerchantFutureOfferingServiceTerm(
		m.DB.QueryRow(ctx, query, id, futureOfferingID),
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrMerchantFutureOfferingServiceTermNotFound
		}

		logger.Error(
			"get scoped service term failed",
			"error", err,
			"service_term_id", id,
			"future_offering_id", futureOfferingID,
		)
		return nil, fmt.Errorf(
			"get merchant future offering service term for future offering: %w",
			err,
		)
	}

	return term, nil
}

// GetCurrentProposed retrieves the currently proposed Service Term for a
// Future Offering.
//
// The lifecycle status is deliberately expressed as a SQL literal so PostgreSQL
// can directly match the query predicate to
// ux_merchant_future_offering_service_terms_one_proposed.
func (m *MerchantFutureOfferingServiceTermModel) GetCurrentProposed(
	ctx context.Context,
	futureOfferingID uuid.UUID,
) (*MerchantFutureOfferingServiceTerm, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).
		WithFunctionName("GetCurrentProposedMerchantFutureOfferingServiceTerm")

	if futureOfferingID == uuid.Nil {
		return nil, ErrMerchantFutureOfferingServiceTermNotFound
	}

	const query = `
		SELECT ` + merchantFutureOfferingServiceTermSelectColumns + `
		FROM merchant_future_offering_service_terms
		WHERE future_offering_id = $1
			AND term_status = 'proposed'
	`

	term, err := scanMerchantFutureOfferingServiceTerm(
		m.DB.QueryRow(ctx, query, futureOfferingID),
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrMerchantFutureOfferingServiceTermNotFound
		}

		logger.Error(
			"get current proposed service term failed",
			"error", err,
			"future_offering_id", futureOfferingID,
		)
		return nil, fmt.Errorf(
			"get current proposed merchant future offering service term: %w",
			err,
		)
	}

	return term, nil
}

// GetCurrentEstablished retrieves the currently established Service Term for a
// Future Offering.
//
// The lifecycle status is deliberately expressed as a SQL literal so PostgreSQL
// can directly match the query predicate to
// ux_merchant_future_offering_service_terms_one_established.
func (m *MerchantFutureOfferingServiceTermModel) GetCurrentEstablished(
	ctx context.Context,
	futureOfferingID uuid.UUID,
) (*MerchantFutureOfferingServiceTerm, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).
		WithFunctionName("GetCurrentEstablishedMerchantFutureOfferingServiceTerm")

	if futureOfferingID == uuid.Nil {
		return nil, ErrMerchantFutureOfferingServiceTermNotFound
	}

	const query = `
		SELECT ` + merchantFutureOfferingServiceTermSelectColumns + `
		FROM merchant_future_offering_service_terms
		WHERE future_offering_id = $1
			AND term_status = 'established'
	`

	term, err := scanMerchantFutureOfferingServiceTerm(
		m.DB.QueryRow(ctx, query, futureOfferingID),
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrMerchantFutureOfferingServiceTermNotFound
		}

		logger.Error(
			"get current established service term failed",
			"error", err,
			"future_offering_id", futureOfferingID,
		)
		return nil, fmt.Errorf(
			"get current established merchant future offering service term: %w",
			err,
		)
	}

	return term, nil
}

// ListTimeline returns Service Terms for one Future Offering newest first.
//
// Pagination is keyset-based on (created_at, id), matching the timeline index.
func (m *MerchantFutureOfferingServiceTermModel) ListTimeline(
	ctx context.Context,
	futureOfferingID uuid.UUID,
	limit int,
	before *MerchantFutureOfferingServiceTermTimelineCursor,
) ([]*MerchantFutureOfferingServiceTerm, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).
		WithFunctionName("ListMerchantFutureOfferingServiceTermTimeline")

	if futureOfferingID == uuid.Nil {
		return nil, ErrMerchantFutureOfferingServiceTermInvalidInput
	}

	if before != nil &&
		(before.CreatedAt.IsZero() || before.ID == uuid.Nil) {
		return nil, ErrMerchantFutureOfferingServiceTermInvalidInput
	}

	if limit <= 0 {
		limit = merchantFutureOfferingServiceTermTimelineDefaultLimit
	}
	if limit > merchantFutureOfferingServiceTermTimelineMaxLimit {
		limit = merchantFutureOfferingServiceTermTimelineMaxLimit
	}

	var (
		rows pgx.Rows
		err  error
	)

	if before == nil {
		const query = `
			SELECT ` + merchantFutureOfferingServiceTermSelectColumns + `
			FROM merchant_future_offering_service_terms
			WHERE future_offering_id = $1
			ORDER BY created_at DESC, id DESC
			LIMIT $2
		`

		rows, err = m.DB.Query(
			ctx,
			query,
			futureOfferingID,
			limit,
		)
	} else {
		const query = `
			SELECT ` + merchantFutureOfferingServiceTermSelectColumns + `
			FROM merchant_future_offering_service_terms
			WHERE future_offering_id = $1
				AND (created_at, id) < ($2, $3)
			ORDER BY created_at DESC, id DESC
			LIMIT $4
		`

		rows, err = m.DB.Query(
			ctx,
			query,
			futureOfferingID,
			before.CreatedAt,
			before.ID,
			limit,
		)
	}

	if err != nil {
		logger.Error(
			"list service term timeline failed",
			"error", err,
			"future_offering_id", futureOfferingID,
		)
		return nil, fmt.Errorf(
			"list merchant future offering service term timeline: %w",
			err,
		)
	}

	terms, err := pgx.CollectRows(
		rows,
		func(row pgx.CollectableRow) (*MerchantFutureOfferingServiceTerm, error) {
			return scanMerchantFutureOfferingServiceTerm(row)
		},
	)
	if err != nil {
		logger.Error(
			"collect service term timeline failed",
			"error", err,
			"future_offering_id", futureOfferingID,
		)
		return nil, fmt.Errorf(
			"collect merchant future offering service term timeline: %w",
			err,
		)
	}

	return terms, nil
}

// UpdateProposed replaces the mutable material facts of a proposed Service
// Term using the pooled database connection.
func (m *MerchantFutureOfferingServiceTermModel) UpdateProposed(
	ctx context.Context,
	id uuid.UUID,
	futureOfferingID uuid.UUID,
	proposal MerchantFutureOfferingServiceTermProposal,
) (*MerchantFutureOfferingServiceTerm, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	return m.updateProposed(
		ctx,
		m.DB,
		id,
		futureOfferingID,
		proposal,
	)
}

// UpdateProposedTx replaces the mutable material facts of a proposed Service
// Term within a caller-owned transaction.
func (m *MerchantFutureOfferingServiceTermModel) UpdateProposedTx(
	ctx context.Context,
	tx pgx.Tx,
	id uuid.UUID,
	futureOfferingID uuid.UUID,
	proposal MerchantFutureOfferingServiceTermProposal,
) (*MerchantFutureOfferingServiceTerm, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	return m.updateProposed(
		ctx,
		tx,
		id,
		futureOfferingID,
		proposal,
	)
}

func (m *MerchantFutureOfferingServiceTermModel) updateProposed(
	ctx context.Context,
	db merchantFutureOfferingServiceTermQueryRower,
	id uuid.UUID,
	futureOfferingID uuid.UUID,
	proposal MerchantFutureOfferingServiceTermProposal,
) (*MerchantFutureOfferingServiceTerm, error) {
	logger := m.Logger.GetLoggerWithContextFromContext(ctx).
		WithFunctionName("UpdateProposedMerchantFutureOfferingServiceTerm")

	if id == uuid.Nil || futureOfferingID == uuid.Nil {
		return nil, ErrMerchantFutureOfferingServiceTermInvalidInput
	}

	normalized, err := normalizeMerchantFutureOfferingServiceTermProposal(proposal)
	if err != nil {
		return nil, fmt.Errorf(
			"%w: %v",
			ErrMerchantFutureOfferingServiceTermInvalidInput,
			err,
		)
	}

	const query = `
		UPDATE merchant_future_offering_service_terms
		SET
			duration_months = $1,
			term_starts_on = $2,
			term_ends_on = $3,
			updated_at = NOW()
		WHERE id = $4
			AND future_offering_id = $5
			AND term_status = 'proposed'
		RETURNING ` + merchantFutureOfferingServiceTermSelectColumns

	term, err := scanMerchantFutureOfferingServiceTerm(
		db.QueryRow(
			ctx,
			query,
			normalized.DurationMonths,
			normalized.TermStartsOn,
			normalized.TermEndsOn,
			id,
			futureOfferingID,
		),
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrMerchantFutureOfferingServiceTermInvalidState
		}

		if IsCheckViolation(err) {
			return nil, ErrMerchantFutureOfferingServiceTermInvalidState
		}

		logger.Error(
			"update proposed service term failed",
			"error", err,
			"service_term_id", id,
			"future_offering_id", futureOfferingID,
		)
		return nil, fmt.Errorf(
			"update proposed merchant future offering service term: %w",
			err,
		)
	}

	logger.Info(
		"proposed service term updated",
		"service_term_id", term.ID,
		"future_offering_id", term.FutureOfferingID,
	)

	return term, nil
}

// Establish transitions a proposed Service Term into the initial established
// Service Term for a Future Offering using the pooled database connection.
//
// Establish does not perform amendment/replacement. Use ReplaceEstablished for
// an already-established Future Offering.
func (m *MerchantFutureOfferingServiceTermModel) Establish(
	ctx context.Context,
	id uuid.UUID,
	futureOfferingID uuid.UUID,
	termStartsOn time.Time,
	termEndsOn time.Time,
) (*MerchantFutureOfferingServiceTerm, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	return m.establish(
		ctx,
		m.DB,
		id,
		futureOfferingID,
		termStartsOn,
		termEndsOn,
	)
}

// EstablishTx transitions a proposed Service Term into the initial established
// Service Term within a caller-owned transaction.
func (m *MerchantFutureOfferingServiceTermModel) EstablishTx(
	ctx context.Context,
	tx pgx.Tx,
	id uuid.UUID,
	futureOfferingID uuid.UUID,
	termStartsOn time.Time,
	termEndsOn time.Time,
) (*MerchantFutureOfferingServiceTerm, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	return m.establish(
		ctx,
		tx,
		id,
		futureOfferingID,
		termStartsOn,
		termEndsOn,
	)
}

func (m *MerchantFutureOfferingServiceTermModel) establish(
	ctx context.Context,
	db merchantFutureOfferingServiceTermQueryRower,
	id uuid.UUID,
	futureOfferingID uuid.UUID,
	termStartsOn time.Time,
	termEndsOn time.Time,
) (*MerchantFutureOfferingServiceTerm, error) {
	logger := m.Logger.GetLoggerWithContextFromContext(ctx).
		WithFunctionName("EstablishMerchantFutureOfferingServiceTerm")

	if id == uuid.Nil || futureOfferingID == uuid.Nil {
		return nil, ErrMerchantFutureOfferingServiceTermInvalidInput
	}

	startsOn, endsOn, err := normalizeMerchantFutureOfferingServiceTermWindow(
		termStartsOn,
		termEndsOn,
	)
	if err != nil {
		return nil, fmt.Errorf(
			"%w: %v",
			ErrMerchantFutureOfferingServiceTermInvalidInput,
			err,
		)
	}

	const query = `
		UPDATE merchant_future_offering_service_terms
		SET
			term_status = 'established',
			term_starts_on = $1,
			term_ends_on = $2,
			established_at = NOW(),
			updated_at = NOW()
		WHERE id = $3
			AND future_offering_id = $4
			AND term_status = 'proposed'
		RETURNING ` + merchantFutureOfferingServiceTermSelectColumns

	term, err := scanMerchantFutureOfferingServiceTerm(
		db.QueryRow(
			ctx,
			query,
			startsOn,
			endsOn,
			id,
			futureOfferingID,
		),
	)
	if err != nil {
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			// Deliberately collapse nonexistent, wrong-FO, and non-proposed rows
			// into the same lifecycle-precondition failure. Mutation paths must not
			// require an additional existence probe merely to distinguish conditions
			// that are equivalent to this transition: the requested establishment
			// cannot legally occur.
			return nil, ErrMerchantFutureOfferingServiceTermInvalidTransition

		case IsPgConstraint(
			err,
			"ux_merchant_future_offering_service_terms_one_established",
		):
			return nil, ErrMerchantFutureOfferingServiceTermEstablishedAlreadyExists

		case IsCheckViolation(err):
			return nil, ErrMerchantFutureOfferingServiceTermInvalidState

		default:
			logger.Error(
				"establish service term failed",
				"error", err,
				"service_term_id", id,
				"future_offering_id", futureOfferingID,
			)
			return nil, fmt.Errorf(
				"establish merchant future offering service term: %w",
				err,
			)
		}
	}

	logger.Info(
		"service term established",
		"service_term_id", term.ID,
		"future_offering_id", term.FutureOfferingID,
	)

	return term, nil
}

// ReplaceEstablished atomically supersedes the currently established Service
// Term and establishes a proposed replacement using a model-owned transaction.
//
// The predecessor and successor remain separate historical records. If either
// half of the replacement fails, neither lifecycle mutation is committed.
func (m *MerchantFutureOfferingServiceTermModel) ReplaceEstablished(
	ctx context.Context,
	predecessorID uuid.UUID,
	replacementID uuid.UUID,
	futureOfferingID uuid.UUID,
	termStartsOn time.Time,
	termEndsOn time.Time,
) (
	*MerchantFutureOfferingServiceTerm,
	*MerchantFutureOfferingServiceTerm,
	error,
) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	tx, err := m.DB.Begin(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf(
			"begin merchant future offering service term replacement: %w",
			err,
		)
	}
	defer tx.Rollback(ctx)

	predecessor, replacement, err := m.replaceEstablishedTx(
		ctx,
		tx,
		predecessorID,
		replacementID,
		futureOfferingID,
		termStartsOn,
		termEndsOn,
	)
	if err != nil {
		return nil, nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, nil, fmt.Errorf(
			"commit merchant future offering service term replacement: %w",
			err,
		)
	}

	return predecessor, replacement, nil
}

// ReplaceEstablishedTx atomically supersedes an established Service Term and
// establishes its proposed replacement inside a caller-owned transaction.
//
// A nested pgx transaction/savepoint protects the replacement boundary itself:
// if the second lifecycle mutation fails, the predecessor is not left
// superseded even if the caller chooses to continue using its outer transaction.
func (m *MerchantFutureOfferingServiceTermModel) ReplaceEstablishedTx(
	ctx context.Context,
	tx pgx.Tx,
	predecessorID uuid.UUID,
	replacementID uuid.UUID,
	futureOfferingID uuid.UUID,
	termStartsOn time.Time,
	termEndsOn time.Time,
) (
	*MerchantFutureOfferingServiceTerm,
	*MerchantFutureOfferingServiceTerm,
	error,
) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	nestedTx, err := tx.Begin(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf(
			"begin merchant future offering service term replacement savepoint: %w",
			err,
		)
	}
	defer nestedTx.Rollback(ctx)

	predecessor, replacement, err := m.replaceEstablishedTx(
		ctx,
		nestedTx,
		predecessorID,
		replacementID,
		futureOfferingID,
		termStartsOn,
		termEndsOn,
	)
	if err != nil {
		return nil, nil, err
	}

	if err := nestedTx.Commit(ctx); err != nil {
		return nil, nil, fmt.Errorf(
			"release merchant future offering service term replacement savepoint: %w",
			err,
		)
	}

	return predecessor, replacement, nil
}

func (m *MerchantFutureOfferingServiceTermModel) replaceEstablishedTx(
	ctx context.Context,
	tx pgx.Tx,
	predecessorID uuid.UUID,
	replacementID uuid.UUID,
	futureOfferingID uuid.UUID,
	termStartsOn time.Time,
	termEndsOn time.Time,
) (
	*MerchantFutureOfferingServiceTerm,
	*MerchantFutureOfferingServiceTerm,
	error,
) {
	logger := m.Logger.GetLoggerWithContextFromContext(ctx).
		WithFunctionName("ReplaceEstablishedMerchantFutureOfferingServiceTerm")

	if predecessorID == uuid.Nil ||
		replacementID == uuid.Nil ||
		futureOfferingID == uuid.Nil ||
		predecessorID == replacementID {
		return nil, nil, ErrMerchantFutureOfferingServiceTermInvalidInput
	}

	startsOn, endsOn, err := normalizeMerchantFutureOfferingServiceTermWindow(
		termStartsOn,
		termEndsOn,
	)
	if err != nil {
		return nil, nil, fmt.Errorf(
			"%w: %v",
			ErrMerchantFutureOfferingServiceTermInvalidInput,
			err,
		)
	}

	const lockQuery = `
		SELECT term_status
		FROM merchant_future_offering_service_terms
		WHERE id = $1
			AND future_offering_id = $2
		FOR UPDATE
	`

	var predecessorStatus string
	if err := tx.QueryRow(
		ctx,
		lockQuery,
		predecessorID,
		futureOfferingID,
	).Scan(&predecessorStatus); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil, ErrMerchantFutureOfferingServiceTermNotFound
		}
		return nil, nil, fmt.Errorf(
			"lock predecessor merchant future offering service term: %w",
			err,
		)
	}

	if MerchantFutureOfferingServiceTermStatus(predecessorStatus) !=
		MerchantFutureOfferingServiceTermStatusEstablished {
		return nil, nil, ErrMerchantFutureOfferingServiceTermInvalidTransition
	}

	var replacementStatus string
	if err := tx.QueryRow(
		ctx,
		lockQuery,
		replacementID,
		futureOfferingID,
	).Scan(&replacementStatus); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil, ErrMerchantFutureOfferingServiceTermNotFound
		}
		return nil, nil, fmt.Errorf(
			"lock replacement merchant future offering service term: %w",
			err,
		)
	}

	if MerchantFutureOfferingServiceTermStatus(replacementStatus) !=
		MerchantFutureOfferingServiceTermStatusProposed {
		return nil, nil, ErrMerchantFutureOfferingServiceTermInvalidTransition
	}

	const supersedeQuery = `
		UPDATE merchant_future_offering_service_terms
		SET
			term_status = 'superseded',
			superseded_at = NOW(),
			updated_at = NOW()
		WHERE id = $1
			AND future_offering_id = $2
			AND term_status = 'established'
		RETURNING ` + merchantFutureOfferingServiceTermSelectColumns

	predecessor, err := scanMerchantFutureOfferingServiceTerm(
		tx.QueryRow(
			ctx,
			supersedeQuery,
			predecessorID,
			futureOfferingID,
		),
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil, ErrMerchantFutureOfferingServiceTermInvalidTransition
		}

		logger.Error(
			"supersede predecessor service term failed",
			"error", err,
			"service_term_id", predecessorID,
			"future_offering_id", futureOfferingID,
		)
		return nil, nil, fmt.Errorf(
			"supersede predecessor merchant future offering service term: %w",
			err,
		)
	}

	const establishReplacementQuery = `
		UPDATE merchant_future_offering_service_terms
		SET
			term_status = 'established',
			term_starts_on = $1,
			term_ends_on = $2,
			supersedes_service_term_id = $3,
			established_at = NOW(),
			updated_at = NOW()
		WHERE id = $4
			AND future_offering_id = $5
			AND term_status = 'proposed'
		RETURNING ` + merchantFutureOfferingServiceTermSelectColumns

	replacement, err := scanMerchantFutureOfferingServiceTerm(
		tx.QueryRow(
			ctx,
			establishReplacementQuery,
			startsOn,
			endsOn,
			predecessorID,
			replacementID,
			futureOfferingID,
		),
	)
	if err != nil {
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			return nil, nil, ErrMerchantFutureOfferingServiceTermInvalidTransition

		case IsPgConstraint(
			err,
			"ux_merchant_future_offering_service_terms_one_established",
		):
			return nil, nil, ErrMerchantFutureOfferingServiceTermEstablishedAlreadyExists

		case IsPgConstraint(
			err,
			"ux_merchant_future_offering_service_terms_supersedes",
		):
			return nil, nil, ErrMerchantFutureOfferingServiceTermReplacementConflict

		case IsForeignKeyViolation(err),
			IsCheckViolation(err):
			return nil, nil, ErrMerchantFutureOfferingServiceTermInvalidState

		default:
			logger.Error(
				"establish replacement service term failed",
				"error", err,
				"predecessor_service_term_id", predecessorID,
				"replacement_service_term_id", replacementID,
				"future_offering_id", futureOfferingID,
			)
			return nil, nil, fmt.Errorf(
				"establish replacement merchant future offering service term: %w",
				err,
			)
		}
	}

	logger.Info(
		"established service term replaced",
		"predecessor_service_term_id", predecessor.ID,
		"replacement_service_term_id", replacement.ID,
		"future_offering_id", futureOfferingID,
	)

	return predecessor, replacement, nil
}
