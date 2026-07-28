// Package data provides models and database access methods for merchant
// program subscription lifecycle events.
//
// sdworkspace/sdbackend/internal/data/merchant_program_subscription_events.go
//
// GTM:
//
//	Layer: 2.4 Merchant / Future Offering Domain
//	Release Class: SPINE
//	Reason:
//	  merchant_program_subscription_events preserves the durable,
//	  append-only history of commercially meaningful merchant program
//	  subscription lifecycle actions.
//
//	  Merchant program subscriptions are optional commercial packaging
//	  infrastructure beneath the Future Offering Platform and Monetization
//	  Layer. This capability remains compiled and production-ready regardless
//	  of whether subscriptions are administratively enabled or disabled.
//
//	  When subscriptions are enabled, their lifecycle history supports merchant
//	  assistance, entitlement investigation, subscription administration,
//	  commercial traceability, and later billing reconciliation.
//
//	  Canonical current subscription state remains in
//	  merchant_program_subscriptions. This file records historical lifecycle
//	  facts and must not become an independently mutable second source of
//	  subscription state.
//
//	  The controlled event vocabulary is an engineering-owned lifecycle
//	  invariant. Whether subscriptions are enabled and how they are packaged
//	  commercially remain configuration and Admin policy.
//
//	  This file is not billing-ledger logic, payment processing, invoice
//	  handling, fee calculation, audit-log persistence, outbox publication,
//	  authorization policy, or subscription-transition policy.
//
// SPINE Rule:
//
//	Keep compiling.
//	Keep production-ready.
//	Preserve append-only event integrity.
//	Preserve controlled event-type integrity.
//	Preserve subscription foreign-key integrity.
//	Preserve actor provenance where available.
//	Preserve DB-owned event timestamps.
//	Preserve deterministic subscription timeline ordering.
//	Preserve transaction-compatible insertion.
//	Preserve centralized normalization, scanning, and PostgreSQL error handling.
//	Never expose update, upsert, soft-delete, restore, or hard-delete methods.
//	Never log event-note contents.
//	Block deployment if this file breaks build, event persistence,
//	subscription lifecycle traceability, or subscription timeline retrieval.
package data

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/PiccoloMondoC/sdworkspace/sdbackend/internal/logging"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// MerchantProgramSubscriptionEventType is the controlled vocabulary for
// merchant program subscription lifecycle actions.
//
// Event types describe actions recorded in subscription history. They are not
// a duplicate subscription-status vocabulary. For example, resumed records an
// action that ordinarily returns a subscription to active status; resumed is
// not itself a MerchantProgramSubscriptionStatus.
type MerchantProgramSubscriptionEventType string

const (
	// MerchantProgramSubscriptionEventCreated means the subscription record was
	// created.
	MerchantProgramSubscriptionEventCreated MerchantProgramSubscriptionEventType = "created"

	// MerchantProgramSubscriptionEventActivated means the subscription entered
	// active service for the first time or from a non-resumable pre-active state.
	MerchantProgramSubscriptionEventActivated MerchantProgramSubscriptionEventType = "activated"

	// MerchantProgramSubscriptionEventPlanChanged means the subscription's
	// merchant program plan changed.
	MerchantProgramSubscriptionEventPlanChanged MerchantProgramSubscriptionEventType = "plan_changed"

	// MerchantProgramSubscriptionEventPaused means the subscription was paused.
	MerchantProgramSubscriptionEventPaused MerchantProgramSubscriptionEventType = "paused"

	// MerchantProgramSubscriptionEventResumed means a paused or suspended
	// subscription returned to active service. The coordinating service owns
	// the activated-versus-resumed decision from the canonical prior state.
	MerchantProgramSubscriptionEventResumed MerchantProgramSubscriptionEventType = "resumed"

	// MerchantProgramSubscriptionEventCancelled means the subscription was
	// cancelled.
	MerchantProgramSubscriptionEventCancelled MerchantProgramSubscriptionEventType = "cancelled"

	// MerchantProgramSubscriptionEventExpired means the subscription reached
	// expiration.
	MerchantProgramSubscriptionEventExpired MerchantProgramSubscriptionEventType = "expired"

	// MerchantProgramSubscriptionEventSuspended means the subscription was
	// suspended.
	MerchantProgramSubscriptionEventSuspended MerchantProgramSubscriptionEventType = "suspended"
)

const merchantProgramSubscriptionEventSelectColumns = `
	id,
	subscription_id,
	event_type,
	note,
	performed_by,
	created_at
`

// MerchantProgramSubscriptionEvent represents an immutable row in
// merchant_program_subscription_events.
//
// Note is nil when no explanatory note was recorded. PerformedBy is nil when
// the event was generated by the platform or cannot properly be attributed to
// a retained user actor.
type MerchantProgramSubscriptionEvent struct {
	ID             uuid.UUID                            `json:"id" db:"id"`
	SubscriptionID uuid.UUID                            `json:"subscription_id" db:"subscription_id"`
	EventType      MerchantProgramSubscriptionEventType `json:"event_type" db:"event_type"`
	Note           *string                              `json:"note,omitempty" db:"note"`
	PerformedBy    *uuid.UUID                           `json:"performed_by,omitempty" db:"performed_by"`
	CreatedAt      time.Time                            `json:"created_at" db:"created_at"`
}

// MerchantProgramSubscriptionEventModel owns persistence for immutable merchant
// program subscription lifecycle events.
//
// The model intentionally exposes no update, upsert, soft-delete, restore, or
// hard-delete operations. Historical corrections must be represented through
// later lifecycle or audit records rather than rewriting persisted facts.
type MerchantProgramSubscriptionEventModel struct {
	DB     *pgxpool.Pool
	Logger *logging.Logger
}

// merchantProgramSubscriptionEventQuerier is the minimal execution contract
// required for event insertion.
//
// Both *pgxpool.Pool and pgx.Tx satisfy this contract. It allows a coordinating
// service to insert a lifecycle event in the same transaction as its canonical
// subscription mutation without giving this model transaction ownership.
type merchantProgramSubscriptionEventQuerier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

func (m *MerchantProgramSubscriptionEventModel) validateBase() error {
	if m == nil {
		return errors.New("merchant program subscription event model is required")
	}
	if m.Logger == nil {
		return errors.New(
			"merchant program subscription event model logger is required",
		)
	}
	return nil
}

func (m *MerchantProgramSubscriptionEventModel) validatePool() error {
	if err := m.validateBase(); err != nil {
		return err
	}
	if m.DB == nil {
		return errors.New(
			"merchant program subscription event model database pool is required",
		)
	}
	return nil
}

func scanMerchantProgramSubscriptionEvent(
	row scannableRow,
	event *MerchantProgramSubscriptionEvent,
) error {
	return row.Scan(
		&event.ID,
		&event.SubscriptionID,
		&event.EventType,
		&event.Note,
		&event.PerformedBy,
		&event.CreatedAt,
	)
}

// NormalizeMerchantProgramSubscriptionEventType trims and canonicalizes a
// merchant program subscription event type.
func NormalizeMerchantProgramSubscriptionEventType(
	eventType MerchantProgramSubscriptionEventType,
) MerchantProgramSubscriptionEventType {
	return MerchantProgramSubscriptionEventType(
		normalizeIdentifier(string(eventType)),
	)
}

// IsValidMerchantProgramSubscriptionEventType reports whether eventType is
// allowed by the merchant_program_subscription_events event_type constraint.
func IsValidMerchantProgramSubscriptionEventType(
	eventType MerchantProgramSubscriptionEventType,
) bool {
	switch NormalizeMerchantProgramSubscriptionEventType(eventType) {
	case MerchantProgramSubscriptionEventCreated,
		MerchantProgramSubscriptionEventActivated,
		MerchantProgramSubscriptionEventPlanChanged,
		MerchantProgramSubscriptionEventPaused,
		MerchantProgramSubscriptionEventResumed,
		MerchantProgramSubscriptionEventCancelled,
		MerchantProgramSubscriptionEventExpired,
		MerchantProgramSubscriptionEventSuspended:
		return true
	default:
		return false
	}
}

func validateMerchantProgramSubscriptionEventType(
	eventType MerchantProgramSubscriptionEventType,
) (MerchantProgramSubscriptionEventType, error) {
	eventType = NormalizeMerchantProgramSubscriptionEventType(eventType)
	if !IsValidMerchantProgramSubscriptionEventType(eventType) {
		return "", fmt.Errorf(
			"invalid merchant program subscription event type: %s",
			eventType,
		)
	}
	return eventType, nil
}

func validateMerchantProgramSubscriptionEventForInsert(
	event *MerchantProgramSubscriptionEvent,
) error {
	if event == nil {
		return errors.New("merchant program subscription event is required")
	}
	if event.SubscriptionID == uuid.Nil {
		return errors.New(
			"merchant program subscription event subscription ID is required",
		)
	}

	eventType, err := validateMerchantProgramSubscriptionEventType(
		event.EventType,
	)
	if err != nil {
		return err
	}

	event.EventType = eventType
	event.Note = normalizeOptionalString(event.Note)

	if event.PerformedBy != nil && *event.PerformedBy == uuid.Nil {
		return errors.New(
			"merchant program subscription event performed-by ID must not be a nil UUID",
		)
	}

	return nil
}

func validateMerchantProgramSubscriptionEventID(id uuid.UUID) error {
	if id == uuid.Nil {
		return errors.New("merchant program subscription event ID is required")
	}
	return nil
}

func validateMerchantProgramSubscriptionEventSubscriptionID(
	subscriptionID uuid.UUID,
) error {
	if subscriptionID == uuid.Nil {
		return errors.New(
			"merchant program subscription event subscription ID is required",
		)
	}
	return nil
}

func validateMerchantProgramSubscriptionEventPagination(
	limit int,
	offset int,
) error {
	if limit <= 0 || limit > 100 {
		return errors.New("limit must be between 1 and 100")
	}
	if offset < 0 {
		return errors.New("offset must be non-negative")
	}
	return nil
}

func translateMerchantProgramSubscriptionEventWriteError(
	err error,
	subscriptionID uuid.UUID,
	performedBy *uuid.UUID,
) error {
	switch {
	case IsForeignKeyViolation(err):
		constraintName := PgErrorConstraintName(err)

		switch {
		case strings.Contains(constraintName, "subscription_id"):
			return fmt.Errorf(
				"merchant program subscription event references missing subscription %s",
				subscriptionID,
			)

		case strings.Contains(constraintName, "performed_by"):
			if performedBy == nil {
				return errors.New(
					"merchant program subscription event references a missing user",
				)
			}
			return fmt.Errorf(
				"merchant program subscription event references missing user %s",
				*performedBy,
			)

		default:
			return errors.New(
				"merchant program subscription event references a missing related record",
			)
		}

	case IsUniqueViolation(err):
		return errors.New(
			"merchant program subscription event already exists",
		)

	case IsCheckViolation(err):
		return errors.New(
			"merchant program subscription event contains an invalid event type",
		)

	default:
		return err
	}
}

func insertMerchantProgramSubscriptionEvent(
	ctx context.Context,
	querier merchantProgramSubscriptionEventQuerier,
	event *MerchantProgramSubscriptionEvent,
) error {
	const query = `
		INSERT INTO merchant_program_subscription_events (
			id,
			subscription_id,
			event_type,
			note,
			performed_by
		)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING created_at
	`

	return querier.QueryRow(
		ctx,
		query,
		event.ID,
		event.SubscriptionID,
		event.EventType,
		event.Note,
		event.PerformedBy,
	).Scan(&event.CreatedAt)
}

func merchantProgramSubscriptionEventLogFields(
	event *MerchantProgramSubscriptionEvent,
) []any {
	fields := []any{
		"event_id", event.ID,
		"subscription_id", event.SubscriptionID,
		"event_type", event.EventType,
	}

	if event.PerformedBy != nil {
		fields = append(
			fields,
			"performed_by",
			*event.PerformedBy,
		)
	}

	return fields
}

func (m *MerchantProgramSubscriptionEventModel) insert(
	ctx context.Context,
	querier merchantProgramSubscriptionEventQuerier,
	functionName string,
	event *MerchantProgramSubscriptionEvent,
) error {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.
		GetLoggerWithContextFromContext(ctx).
		WithFunctionName(functionName)

	if err := validateMerchantProgramSubscriptionEventForInsert(event); err != nil {
		logger.Error("Validation failed", err)
		return err
	}

	if event.ID == uuid.Nil {
		event.ID = uuid.New()
	}

	err := insertMerchantProgramSubscriptionEvent(ctx, querier, event)
	if err != nil {
		err = translateMerchantProgramSubscriptionEventWriteError(
			err,
			event.SubscriptionID,
			event.PerformedBy,
		)

		errorFields := append(
			[]any{err},
			merchantProgramSubscriptionEventLogFields(event)...,
		)

		logger.Error(
			"Insert merchant program subscription event failed",
			errorFields...,
		)
		return err
	}

	logger.Info(
		"Insert merchant program subscription event successful",
		merchantProgramSubscriptionEventLogFields(event)...,
	)

	return nil
}

// Insert inserts an immutable merchant program subscription lifecycle event
// through the model's database pool.
//
// If event.ID is uuid.Nil, Insert generates a UUID. The database owns
// created_at and returns it through RETURNING.
//
// Insert alone is not atomic with a corresponding subscription mutation.
// Workflows requiring mutation-plus-history atomicity must use InsertTx inside
// the same transaction as a transaction-compatible subscription mutation.
func (m *MerchantProgramSubscriptionEventModel) Insert(
	ctx context.Context,
	event *MerchantProgramSubscriptionEvent,
) error {
	if err := m.validatePool(); err != nil {
		return err
	}

	return m.insert(
		ctx,
		m.DB,
		"InsertMerchantProgramSubscriptionEvent",
		event,
	)
}

// InsertTx inserts an immutable merchant program subscription lifecycle event
// through tx.
//
// InsertTx validates only the model dependencies needed by the transaction
// path. It does not require the model database pool because all persistence is
// performed through the caller-supplied transaction.
//
// InsertTx does not begin, commit, or roll back tx. The coordinating service
// owns transaction lifecycle and must use the same transaction for the
// canonical subscription mutation and its corresponding lifecycle event.
func (m *MerchantProgramSubscriptionEventModel) InsertTx(
	ctx context.Context,
	tx pgx.Tx,
	event *MerchantProgramSubscriptionEvent,
) error {
	if err := m.validateBase(); err != nil {
		return err
	}
	if tx == nil {
		return errors.New(
			"merchant program subscription event transaction is required",
		)
	}

	return m.insert(
		ctx,
		tx,
		"InsertMerchantProgramSubscriptionEventTx",
		event,
	)
}

// GetByID retrieves a merchant program subscription event by ID.
//
// GetByID returns nil, nil when the event does not exist.
func (m *MerchantProgramSubscriptionEventModel) GetByID(
	ctx context.Context,
	id uuid.UUID,
) (*MerchantProgramSubscriptionEvent, error) {
	if err := m.validatePool(); err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.
		GetLoggerWithContextFromContext(ctx).
		WithFunctionName("GetMerchantProgramSubscriptionEventByID")

	if err := validateMerchantProgramSubscriptionEventID(id); err != nil {
		logger.Error("Validation failed", err)
		return nil, err
	}

	query := `
		SELECT ` + merchantProgramSubscriptionEventSelectColumns + `
		FROM merchant_program_subscription_events
		WHERE id = $1
	`

	var event MerchantProgramSubscriptionEvent
	err := scanMerchantProgramSubscriptionEvent(
		m.DB.QueryRow(ctx, query, id),
		&event,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			logger.Warn(
				"Merchant program subscription event not found",
				"event_id", id,
			)
			return nil, nil
		}

		logger.Error(
			"Get merchant program subscription event by ID failed",
			err,
			"event_id", id,
		)
		return nil, err
	}

	logger.Info(
		"Get merchant program subscription event by ID successful",
		"event_id", event.ID,
		"subscription_id", event.SubscriptionID,
		"event_type", event.EventType,
	)

	return &event, nil
}

// ListBySubscriptionID retrieves the chronological lifecycle history for one
// merchant program subscription.
//
// Results are ordered by created_at ascending and id ascending so callers
// receive an oldest-to-newest deterministic timeline.
func (m *MerchantProgramSubscriptionEventModel) ListBySubscriptionID(
	ctx context.Context,
	subscriptionID uuid.UUID,
	limit int,
	offset int,
) ([]*MerchantProgramSubscriptionEvent, error) {
	if err := m.validatePool(); err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.
		GetLoggerWithContextFromContext(ctx).
		WithFunctionName(
			"ListMerchantProgramSubscriptionEventsBySubscriptionID",
		)

	if err := validateMerchantProgramSubscriptionEventSubscriptionID(
		subscriptionID,
	); err != nil {
		logger.Error("Validation failed", err)
		return nil, err
	}

	if err := validateMerchantProgramSubscriptionEventPagination(
		limit,
		offset,
	); err != nil {
		logger.Error("Validation failed", err)
		return nil, err
	}

	query := `
		SELECT ` + merchantProgramSubscriptionEventSelectColumns + `
		FROM merchant_program_subscription_events
		WHERE subscription_id = $1
		ORDER BY created_at ASC, id ASC
		LIMIT $2 OFFSET $3
	`

	rows, err := m.DB.Query(
		ctx,
		query,
		subscriptionID,
		limit,
		offset,
	)
	if err != nil {
		logger.Error(
			"List merchant program subscription events by subscription ID query failed",
			err,
			"subscription_id", subscriptionID,
		)
		return nil, err
	}
	defer rows.Close()

	var events []*MerchantProgramSubscriptionEvent
	for rows.Next() {
		var event MerchantProgramSubscriptionEvent
		if err := scanMerchantProgramSubscriptionEvent(
			rows,
			&event,
		); err != nil {
			logger.Error(
				"Merchant program subscription event row scan failed",
				err,
				"subscription_id", subscriptionID,
			)
			return nil, err
		}

		events = append(events, &event)
	}

	if err := rows.Err(); err != nil {
		logger.Error(
			"Merchant program subscription event row iteration failed",
			err,
			"subscription_id", subscriptionID,
		)
		return nil, err
	}

	logger.Info(
		"List merchant program subscription events by subscription ID successful",
		"subscription_id", subscriptionID,
		"limit", limit,
		"offset", offset,
		"count", len(events),
	)

	return events, nil
}

// ListBySubscriptionIDAndType retrieves a chronological lifecycle history for
// one subscription filtered by event type.
//
// The supplied event type is normalized and validated before query execution.
// Results are ordered by created_at ascending and id ascending.
func (m *MerchantProgramSubscriptionEventModel) ListBySubscriptionIDAndType(
	ctx context.Context,
	subscriptionID uuid.UUID,
	eventType MerchantProgramSubscriptionEventType,
	limit int,
	offset int,
) ([]*MerchantProgramSubscriptionEvent, error) {
	if err := m.validatePool(); err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.
		GetLoggerWithContextFromContext(ctx).
		WithFunctionName(
			"ListMerchantProgramSubscriptionEventsBySubscriptionIDAndType",
		)

	if err := validateMerchantProgramSubscriptionEventSubscriptionID(
		subscriptionID,
	); err != nil {
		logger.Error("Validation failed", err)
		return nil, err
	}

	eventType, err := validateMerchantProgramSubscriptionEventType(eventType)
	if err != nil {
		logger.Error("Validation failed", err)
		return nil, err
	}

	if err := validateMerchantProgramSubscriptionEventPagination(
		limit,
		offset,
	); err != nil {
		logger.Error("Validation failed", err)
		return nil, err
	}

	query := `
		SELECT ` + merchantProgramSubscriptionEventSelectColumns + `
		FROM merchant_program_subscription_events
		WHERE subscription_id = $1
		  AND event_type = $2
		ORDER BY created_at ASC, id ASC
		LIMIT $3 OFFSET $4
	`

	rows, err := m.DB.Query(
		ctx,
		query,
		subscriptionID,
		eventType,
		limit,
		offset,
	)
	if err != nil {
		logger.Error(
			"List merchant program subscription events by subscription ID and type query failed",
			err,
			"subscription_id", subscriptionID,
			"event_type", eventType,
		)
		return nil, err
	}
	defer rows.Close()

	var events []*MerchantProgramSubscriptionEvent
	for rows.Next() {
		var event MerchantProgramSubscriptionEvent
		if err := scanMerchantProgramSubscriptionEvent(
			rows,
			&event,
		); err != nil {
			logger.Error(
				"Merchant program subscription event row scan failed",
				err,
				"subscription_id", subscriptionID,
				"event_type", eventType,
			)
			return nil, err
		}

		events = append(events, &event)
	}

	if err := rows.Err(); err != nil {
		logger.Error(
			"Merchant program subscription event row iteration failed",
			err,
			"subscription_id", subscriptionID,
			"event_type", eventType,
		)
		return nil, err
	}

	logger.Info(
		"List merchant program subscription events by subscription ID and type successful",
		"subscription_id", subscriptionID,
		"event_type", eventType,
		"limit", limit,
		"offset", offset,
		"count", len(events),
	)

	return events, nil
}

// GetLatestBySubscriptionID retrieves the most recently recorded lifecycle
// event for a merchant program subscription.
//
// GetLatestBySubscriptionID returns nil, nil when the subscription has no
// recorded lifecycle events.
func (m *MerchantProgramSubscriptionEventModel) GetLatestBySubscriptionID(
	ctx context.Context,
	subscriptionID uuid.UUID,
) (*MerchantProgramSubscriptionEvent, error) {
	if err := m.validatePool(); err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.
		GetLoggerWithContextFromContext(ctx).
		WithFunctionName(
			"GetLatestMerchantProgramSubscriptionEventBySubscriptionID",
		)

	if err := validateMerchantProgramSubscriptionEventSubscriptionID(
		subscriptionID,
	); err != nil {
		logger.Error("Validation failed", err)
		return nil, err
	}

	query := `
		SELECT ` + merchantProgramSubscriptionEventSelectColumns + `
		FROM merchant_program_subscription_events
		WHERE subscription_id = $1
		ORDER BY created_at DESC, id DESC
		LIMIT 1
	`

	var event MerchantProgramSubscriptionEvent
	err := scanMerchantProgramSubscriptionEvent(
		m.DB.QueryRow(ctx, query, subscriptionID),
		&event,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			logger.Warn(
				"Merchant program subscription event not found",
				"subscription_id", subscriptionID,
			)
			return nil, nil
		}

		logger.Error(
			"Get latest merchant program subscription event by subscription ID failed",
			err,
			"subscription_id", subscriptionID,
		)
		return nil, err
	}

	logger.Info(
		"Get latest merchant program subscription event by subscription ID successful",
		"event_id", event.ID,
		"subscription_id", event.SubscriptionID,
		"event_type", event.EventType,
	)

	return &event, nil
}

// Exists checks whether a merchant program subscription event exists by ID.
func (m *MerchantProgramSubscriptionEventModel) Exists(
	ctx context.Context,
	id uuid.UUID,
) (bool, error) {
	if err := m.validatePool(); err != nil {
		return false, err
	}

	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.
		GetLoggerWithContextFromContext(ctx).
		WithFunctionName("ExistsMerchantProgramSubscriptionEvent")

	if err := validateMerchantProgramSubscriptionEventID(id); err != nil {
		logger.Error("Validation failed", err)
		return false, err
	}

	query := `
		SELECT EXISTS (
			SELECT 1
			FROM merchant_program_subscription_events
			WHERE id = $1
		)
	`

	var exists bool
	if err := m.DB.QueryRow(ctx, query, id).Scan(&exists); err != nil {
		logger.Error(
			"Merchant program subscription event exists query failed",
			err,
			"event_id", id,
		)
		return false, err
	}

	return exists, nil
}
