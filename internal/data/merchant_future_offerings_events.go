// Package data provides append-only Future Offering lifecycle history persistence.
// focodebase/fobackend/internal/data/merchant_future_offerings_events.go
package data

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/PiccoloMondoC/focodebase/fobackend/internal/logging"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type MerchantFutureOfferingEventType string

const (
	MerchantFutureOfferingEventCreated                    MerchantFutureOfferingEventType = "created"
	MerchantFutureOfferingEventSubmittedForActivation     MerchantFutureOfferingEventType = "submitted_for_activation"
	MerchantFutureOfferingEventSentToTrustReview          MerchantFutureOfferingEventType = "sent_to_trust_review"
	MerchantFutureOfferingEventChangesRequested           MerchantFutureOfferingEventType = "changes_requested"
	MerchantFutureOfferingEventApproved                   MerchantFutureOfferingEventType = "approved"
	MerchantFutureOfferingEventActivationPaymentSatisfied MerchantFutureOfferingEventType = "activation_payment_satisfied"
	MerchantFutureOfferingEventActivated                  MerchantFutureOfferingEventType = "activated"
	MerchantFutureOfferingEventPublished                  MerchantFutureOfferingEventType = "published"
	MerchantFutureOfferingEventPaused                     MerchantFutureOfferingEventType = "paused"
	MerchantFutureOfferingEventExpired                    MerchantFutureOfferingEventType = "expired"
	MerchantFutureOfferingEventRejected                   MerchantFutureOfferingEventType = "rejected"
	MerchantFutureOfferingEventUnpublished                MerchantFutureOfferingEventType = "unpublished"
	MerchantFutureOfferingEventArchived                   MerchantFutureOfferingEventType = "archived"
	MerchantFutureOfferingEventRestored                   MerchantFutureOfferingEventType = "restored"
)
const merchantFutureOfferingEventReadLimit = 200
const merchantFutureOfferingEventSelectColumns = `e.id,e.future_offering_id,e.event_type,e.from_status,e.to_status,e.note,e.performed_by,e.created_at`

type MerchantFutureOfferingEvent struct {
	ID               uuid.UUID                       `json:"id" db:"id"`
	FutureOfferingID uuid.UUID                       `json:"future_offering_id" db:"future_offering_id"`
	EventType        MerchantFutureOfferingEventType `json:"event_type" db:"event_type"`
	FromStatus       *string                         `json:"from_status,omitempty" db:"from_status"`
	ToStatus         *string                         `json:"to_status,omitempty" db:"to_status"`
	Note             *string                         `json:"note,omitempty" db:"note"`
	PerformedBy      *uuid.UUID                      `json:"performed_by,omitempty" db:"performed_by"`
	CreatedAt        time.Time                       `json:"created_at" db:"created_at"`
}
type NewMerchantFutureOfferingEvent struct {
	FutureOfferingID uuid.UUID
	EventType        MerchantFutureOfferingEventType
	FromStatus       *MerchantFutureOfferingStatus
	ToStatus         *MerchantFutureOfferingStatus
	Note             *string
	PerformedBy      *uuid.UUID
}
type MerchantFutureOfferingEventModel struct {
	DB     *pgxpool.Pool
	Logger *logging.Logger
}

func (m *MerchantFutureOfferingEventModel) validateBase() error {
	if m == nil {
		return errors.New("merchant future offering event model is required")
	}
	if m.Logger == nil {
		return errors.New("merchant future offering event model logger is required")
	}
	return nil
}
func IsValidMerchantFutureOfferingEventType(t MerchantFutureOfferingEventType) bool {
	switch t {
	case MerchantFutureOfferingEventCreated, MerchantFutureOfferingEventSubmittedForActivation, MerchantFutureOfferingEventSentToTrustReview, MerchantFutureOfferingEventChangesRequested, MerchantFutureOfferingEventApproved, MerchantFutureOfferingEventActivationPaymentSatisfied, MerchantFutureOfferingEventActivated, MerchantFutureOfferingEventPublished, MerchantFutureOfferingEventPaused, MerchantFutureOfferingEventExpired, MerchantFutureOfferingEventRejected, MerchantFutureOfferingEventUnpublished, MerchantFutureOfferingEventArchived, MerchantFutureOfferingEventRestored:
		return true
	}
	return false
}
func optionalStatusString(s *MerchantFutureOfferingStatus) (*string, error) {
	if s == nil {
		return nil, nil
	}
	n := NormalizeMerchantFutureOfferingStatus(*s)
	if !IsValidMerchantFutureOfferingStatus(n) {
		return nil, merchantFutureOfferingInvalidInput("invalid history status: %q", *s)
	}
	v := string(n)
	return &v, nil
}
func (m *MerchantFutureOfferingEventModel) InsertTx(ctx context.Context, tx pgx.Tx, in NewMerchantFutureOfferingEvent) (*MerchantFutureOfferingEvent, error) {
	if err := m.validateBase(); err != nil {
		return nil, err
	}
	if tx == nil || in.FutureOfferingID == uuid.Nil {
		return nil, merchantFutureOfferingInvalidInput("transaction and future_offering_id are required")
	}
	if !IsValidMerchantFutureOfferingEventType(in.EventType) {
		return nil, merchantFutureOfferingInvalidInput("invalid event_type: %q", in.EventType)
	}
	fs, err := optionalStatusString(in.FromStatus)
	if err != nil {
		return nil, err
	}
	ts, err := optionalStatusString(in.ToStatus)
	if err != nil {
		return nil, err
	}
	var note *string
	if in.Note != nil {
		v := strings.TrimSpace(*in.Note)
		if v != "" {
			note = &v
		}
	}
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()
	rows, err := tx.Query(ctx, `INSERT INTO merchant_future_offerings_events AS e(future_offering_id,event_type,from_status,to_status,note,performed_by) VALUES($1,$2,$3,$4,$5,$6) RETURNING `+merchantFutureOfferingEventSelectColumns, in.FutureOfferingID, string(in.EventType), fs, ts, note, in.PerformedBy)
	if err != nil {
		return nil, classifyMerchantFutureOfferingWriteError(err)
	}
	return pgx.CollectExactlyOneRow(rows, pgx.RowToAddrOfStructByName[MerchantFutureOfferingEvent])
}
func (m *MerchantFutureOfferingEventModel) ListForMerchant(ctx context.Context, merchantID, fo uuid.UUID) ([]*MerchantFutureOfferingEvent, error) {
	if err := m.validateBase(); err != nil {
		return nil, err
	}
	if m.DB == nil {
		return nil, errors.New("merchant future offering event model database pool is required")
	}
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()
	rows, err := m.DB.Query(ctx, `SELECT `+merchantFutureOfferingEventSelectColumns+` FROM merchant_future_offerings_events e JOIN merchant_future_offerings fo ON fo.id=e.future_offering_id WHERE e.future_offering_id=$1 AND fo.merchant_id=$2 AND fo.deleted_at IS NULL ORDER BY e.created_at DESC,e.id DESC LIMIT $3`, fo, merchantID, merchantFutureOfferingEventReadLimit)
	if err != nil {
		return nil, fmt.Errorf("list Future Offering history: %w", err)
	}
	return pgx.CollectRows(rows, pgx.RowToAddrOfStructByName[MerchantFutureOfferingEvent])
}
