// Package data provides persistence for merchant Future Offering Engagement Action Groups.
// focodebase/fobackend/internal/data/merchant_future_offering_engagement_action_groups.go
package data

import (
	"context"
	"errors"
	"fmt"
	"time"
	"unicode/utf8"

	"github.com/PiccoloMondoC/focodebase/fobackend/internal/logging"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	MaxMerchantFutureOfferingEngagementGroups    = 50
	MaxMerchantFutureOfferingEngagementGroupName = 120
)
const merchantFutureOfferingEngagementActionGroupSelectColumns = `g.id,g.future_offering_id,g.name,g.display_order,g.max_selections,g.is_active,g.created_at,g.updated_at`

type MerchantFutureOfferingEngagementActionGroup struct {
	ID               uuid.UUID `json:"id" db:"id"`
	FutureOfferingID uuid.UUID `json:"future_offering_id" db:"future_offering_id"`
	Name             *string   `json:"name,omitempty" db:"name"`
	DisplayOrder     int       `json:"display_order" db:"display_order"`
	MaxSelections    *int      `json:"max_selections,omitempty" db:"max_selections"`
	IsActive         bool      `json:"is_active" db:"is_active"`
	CreatedAt        time.Time `json:"created_at" db:"created_at"`
	UpdatedAt        time.Time `json:"updated_at" db:"updated_at"`
}
type NewMerchantFutureOfferingEngagementActionGroup struct {
	Name          *string
	DisplayOrder  int
	MaxSelections *int
}
type MerchantFutureOfferingEngagementActionGroupModel struct {
	DB     *pgxpool.Pool
	Logger *logging.Logger
}

func (m *MerchantFutureOfferingEngagementActionGroupModel) validateBase() error {
	if m == nil {
		return errors.New("engagement action group model is required")
	}
	if m.Logger == nil {
		return errors.New("engagement action group model logger is required")
	}
	return nil
}
func engagementInvalidInput(f string, a ...any) error {
	return fmt.Errorf("%w: %s", ErrMerchantFutureOfferingEngagementInvalidInput, fmt.Sprintf(f, a...))
}
func NormalizeMerchantFutureOfferingEngagementGroupInput(in NewMerchantFutureOfferingEngagementActionGroup) (NewMerchantFutureOfferingEngagementActionGroup, error) {
	out := NewMerchantFutureOfferingEngagementActionGroup{Name: normalizeOptionalString(in.Name), DisplayOrder: in.DisplayOrder, MaxSelections: in.MaxSelections}
	if out.Name != nil && utf8.RuneCountInString(*out.Name) > MaxMerchantFutureOfferingEngagementGroupName {
		return out, engagementInvalidInput("group name exceeds %d characters", MaxMerchantFutureOfferingEngagementGroupName)
	}
	if out.DisplayOrder < 0 {
		return out, engagementInvalidInput("display_order must not be negative")
	}
	if out.MaxSelections != nil && *out.MaxSelections < 1 {
		return out, engagementInvalidInput("max_selections must be at least 1 when set")
	}
	return out, nil
}
func (m *MerchantFutureOfferingEngagementActionGroupModel) InsertTx(ctx context.Context, tx pgx.Tx, fo uuid.UUID, in NewMerchantFutureOfferingEngagementActionGroup) (*MerchantFutureOfferingEngagementActionGroup, error) {
	if err := m.validateBase(); err != nil {
		return nil, err
	}
	if tx == nil || fo == uuid.Nil {
		return nil, engagementInvalidInput("transaction and future_offering_id are required")
	}
	c, err := NormalizeMerchantFutureOfferingEngagementGroupInput(in)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()
	rows, err := tx.Query(ctx, `INSERT INTO merchant_future_offering_engagement_action_groups AS g(future_offering_id,name,display_order,max_selections) VALUES($1,$2,$3,$4) RETURNING `+merchantFutureOfferingEngagementActionGroupSelectColumns, fo, c.Name, c.DisplayOrder, c.MaxSelections)
	if err != nil {
		return nil, classifyMerchantFutureOfferingEngagementWriteError(err)
	}
	return pgx.CollectExactlyOneRow(rows, pgx.RowToAddrOfStructByName[MerchantFutureOfferingEngagementActionGroup])
}
func (m *MerchantFutureOfferingEngagementActionGroupModel) DeleteAllForDraftReplacementTx(ctx context.Context, tx pgx.Tx, fo uuid.UUID) error {
	if err := m.validateBase(); err != nil {
		return err
	}
	if tx == nil || fo == uuid.Nil {
		return engagementInvalidInput("transaction and future_offering_id are required")
	}
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()
	_, err := tx.Exec(ctx, `DELETE FROM merchant_future_offering_engagement_action_groups WHERE future_offering_id=$1`, fo)
	return classifyMerchantFutureOfferingEngagementWriteError(err)
}
func (m *MerchantFutureOfferingEngagementActionGroupModel) ListActiveTx(ctx context.Context, tx pgx.Tx, fo uuid.UUID) ([]*MerchantFutureOfferingEngagementActionGroup, error) {
	if err := m.validateBase(); err != nil {
		return nil, err
	}
	if tx == nil || fo == uuid.Nil {
		return nil, engagementInvalidInput("transaction and future_offering_id are required")
	}
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()
	rows, err := tx.Query(ctx, `SELECT `+merchantFutureOfferingEngagementActionGroupSelectColumns+` FROM merchant_future_offering_engagement_action_groups g WHERE g.future_offering_id=$1 AND g.is_active=TRUE ORDER BY g.display_order,g.id LIMIT $2`, fo, MaxMerchantFutureOfferingEngagementGroups+1)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToAddrOfStructByName[MerchantFutureOfferingEngagementActionGroup])
}
