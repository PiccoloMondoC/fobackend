// Package data provides the production data-layer implementation for the
// Platform transactional domain-event outbox.
//
// sdworkspace/sdbackend/internal/data/outbox_events.go
//
// GTM:
//
//	Layer: 2.1 Database / Governance Foundation
//	Release Class: SPINE
//	Reason:
//	  Provides the transport-neutral transactional outbox used to persist
//	  producer-owned domain-event occurrences atomically with authoritative
//	  domain mutations.
//
//	  The outbox preserves event identity, versioning, idempotency, correlation,
//	  causation, occurrence time, publication state, retry state, claiming,
//	  failure provenance, and dead-letter lifecycle without embedding producer,
//	  consumer, broker, or transport-specific semantics.
//
//	  Domain producers own the meaning and payload of the events they create.
//	  This model owns durable transactional persistence only. External
//	  publication is performed outside the authoritative domain transaction.
//
// SPINE Rule:
//
//	Keep compiling.
//	Keep production-ready.
//	Preserve caller-owned transactional insertion.
//	Preserve producer-owned event identity, versioning, and payload semantics.
//	Preserve deterministic idempotency enforcement.
//	Preserve occurrence time separately from row creation and publication time.
//	Preserve retry, claiming, failure, and dead-letter state required for
//	reliable eventual publication.
//	Keep this capability transport-neutral and consumer-neutral.
//	Do not embed Future Offering, Service Term, Service Period, billing,
//	payment, notification, Pub/Sub, or other domain-specific behavior.
//	Do not publish external messages from InsertTx.
//	Do not commit independently of the caller-owned transaction.
//	Block deployment if transactional publication guarantees, idempotency,
//	auditability, retry safety, or transport neutrality are weakened.
package data

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/PiccoloMondoC/sdworkspace/sdbackend/internal/logging"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	ErrOutboxEventInvalidInput = errors.New(
		"invalid outbox event input",
	)

	ErrOutboxEventDuplicateIdempotencyKey = errors.New(
		"outbox event idempotency key already exists",
	)
)

// OutboxEvent is one durable producer-owned domain-event occurrence awaiting,
// undergoing, or having completed external publication.
//
// The outbox is transport-neutral infrastructure. Aggregate and event semantics
// belong to the producer and must not depend on a particular broker or consumer.
type OutboxEvent struct {
	ID uuid.UUID `json:"id" db:"id"`

	AggregateType string    `json:"aggregate_type" db:"aggregate_type"`
	AggregateID   uuid.UUID `json:"aggregate_id" db:"aggregate_id"`

	EventType    string `json:"event_type" db:"event_type"`
	EventVersion int    `json:"event_version" db:"event_version"`

	Payload json.RawMessage `json:"payload" db:"payload"`

	IdempotencyKey string `json:"idempotency_key" db:"idempotency_key"`

	CorrelationID *uuid.UUID `json:"correlation_id,omitempty" db:"correlation_id"`
	CausationID   *uuid.UUID `json:"causation_id,omitempty" db:"causation_id"`

	OccurredAt time.Time `json:"occurred_at" db:"occurred_at"`
	CreatedAt  time.Time `json:"created_at" db:"created_at"`

	PublishedAt *time.Time `json:"published_at,omitempty" db:"published_at"`

	AttemptCount  int        `json:"attempt_count" db:"attempt_count"`
	NextAttemptAt *time.Time `json:"next_attempt_at,omitempty" db:"next_attempt_at"`

	ClaimedAt *time.Time `json:"claimed_at,omitempty" db:"claimed_at"`
	ClaimedBy *string    `json:"claimed_by,omitempty" db:"claimed_by"`

	LastError      *string    `json:"last_error,omitempty" db:"last_error"`
	DeadLetteredAt *time.Time `json:"dead_lettered_at,omitempty" db:"dead_lettered_at"`
}

// NewOutboxEvent contains the producer-owned facts required to persist one
// transactional outbox event.
//
// OccurredAt is optional. When nil, InsertTx uses the database transaction time
// as the occurrence instant. A producer may supply OccurredAt when the
// authoritative domain occurrence genuinely predates outbox insertion.
type NewOutboxEvent struct {
	AggregateType string
	AggregateID   uuid.UUID

	EventType    string
	EventVersion int

	Payload json.RawMessage

	IdempotencyKey string

	CorrelationID *uuid.UUID
	CausationID   *uuid.UUID

	OccurredAt *time.Time
}

func (in NewOutboxEvent) validate() error {
	if strings.TrimSpace(in.AggregateType) == "" ||
		in.AggregateID == uuid.Nil {
		return fmt.Errorf(
			"%w: aggregate identity is required",
			ErrOutboxEventInvalidInput,
		)
	}

	if strings.TrimSpace(in.EventType) == "" {
		return fmt.Errorf(
			"%w: event type is required",
			ErrOutboxEventInvalidInput,
		)
	}

	if in.EventVersion <= 0 {
		return fmt.Errorf(
			"%w: event version must be positive",
			ErrOutboxEventInvalidInput,
		)
	}

	if len(in.Payload) == 0 ||
		!json.Valid(in.Payload) {
		return fmt.Errorf(
			"%w: payload must be valid JSON",
			ErrOutboxEventInvalidInput,
		)
	}

	if strings.TrimSpace(in.IdempotencyKey) == "" {
		return fmt.Errorf(
			"%w: idempotency key is required",
			ErrOutboxEventInvalidInput,
		)
	}

	if in.OccurredAt != nil &&
		in.OccurredAt.IsZero() {
		return fmt.Errorf(
			"%w: occurred_at must not be zero when supplied",
			ErrOutboxEventInvalidInput,
		)
	}

	return nil
}

// OutboxEventModel persists transport-neutral transactional outbox events.
type OutboxEventModel struct {
	DB     *pgxpool.Pool
	Logger *logging.Logger
}

// InsertTx inserts one outbox event inside the caller-owned transaction.
//
// The producer's authoritative domain mutation and this insert therefore share
// one commit boundary. InsertTx never publishes externally and never commits
// independently.
//
// When NewOutboxEvent.OccurredAt is nil, PostgreSQL transaction time is used.
// Otherwise the supplied authoritative occurrence instant is persisted.
func (m *OutboxEventModel) InsertTx(
	ctx context.Context,
	tx pgx.Tx,
	in NewOutboxEvent,
) (*OutboxEvent, error) {
	if tx == nil {
		return nil, fmt.Errorf(
			"%w: transaction is required",
			ErrOutboxEventInvalidInput,
		)
	}

	if err := in.validate(); err != nil {
		return nil, err
	}

	var occurredAt *time.Time

	if in.OccurredAt != nil {
		normalized := in.OccurredAt.UTC()
		occurredAt = &normalized
	}

	const query = `
		INSERT INTO outbox_events (
			aggregate_type,
			aggregate_id,
			event_type,
			event_version,
			payload,
			idempotency_key,
			correlation_id,
			causation_id,
			occurred_at
		)
		VALUES (
			$1,
			$2,
			$3,
			$4,
			$5,
			$6,
			$7,
			$8,
			COALESCE($9, NOW())
		)
		RETURNING
			id,
			aggregate_type,
			aggregate_id,
			event_type,
			event_version,
			payload,
			idempotency_key,
			correlation_id,
			causation_id,
			occurred_at,
			created_at,
			published_at,
			attempt_count,
			next_attempt_at,
			claimed_at,
			claimed_by,
			last_error,
			dead_lettered_at
	`

	var event OutboxEvent

	err := tx.QueryRow(
		ctx,
		query,
		strings.TrimSpace(in.AggregateType),
		in.AggregateID,
		strings.TrimSpace(in.EventType),
		in.EventVersion,
		in.Payload,
		strings.TrimSpace(in.IdempotencyKey),
		in.CorrelationID,
		in.CausationID,
		occurredAt,
	).Scan(
		&event.ID,
		&event.AggregateType,
		&event.AggregateID,
		&event.EventType,
		&event.EventVersion,
		&event.Payload,
		&event.IdempotencyKey,
		&event.CorrelationID,
		&event.CausationID,
		&event.OccurredAt,
		&event.CreatedAt,
		&event.PublishedAt,
		&event.AttemptCount,
		&event.NextAttemptAt,
		&event.ClaimedAt,
		&event.ClaimedBy,
		&event.LastError,
		&event.DeadLetteredAt,
	)
	if err != nil {
		var pgErr *pgconn.PgError

		if errors.As(err, &pgErr) &&
			pgErr.Code == "23505" &&
			pgErr.ConstraintName ==
				"ux_outbox_events_idempotency_key" {
			return nil,
				ErrOutboxEventDuplicateIdempotencyKey
		}

		return nil, fmt.Errorf(
			"insert outbox event: %w",
			err,
		)
	}

	return &event, nil
}
