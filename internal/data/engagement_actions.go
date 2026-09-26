// Package data provides models and database access methods for the Platform-governed Engagement Action catalog.
// focodebase/fobackend/internal/data/engagement_actions.go
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

const engagementActionCatalogReadLimit = 500
const engagementActionSelectColumns = `id, code, name, description, is_active, created_at, updated_at`

type EngagementAction struct {
	ID          uuid.UUID `json:"id" db:"id"`
	Code        string    `json:"code" db:"code"`
	Name        string    `json:"name" db:"name"`
	Description *string   `json:"description,omitempty" db:"description"`
	IsActive    bool      `json:"is_active" db:"is_active"`
	CreatedAt   time.Time `json:"created_at" db:"created_at"`
	UpdatedAt   time.Time `json:"updated_at" db:"updated_at"`
}
type EngagementActionModel struct {
	DB     *pgxpool.Pool
	Logger *logging.Logger
}

func (m *EngagementActionModel) validateBase() error {
	if m == nil {
		return errors.New("engagement action model is required")
	}
	if m.Logger == nil {
		return errors.New("engagement action model logger is required")
	}
	return nil
}
func (m *EngagementActionModel) ListActive(ctx context.Context) ([]*EngagementAction, error) {
	if err := m.validateBase(); err != nil {
		return nil, err
	}
	if m.DB == nil {
		return nil, errors.New("engagement action model database pool is required")
	}
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()
	rows, err := m.DB.Query(ctx, `SELECT `+engagementActionSelectColumns+` FROM engagement_actions WHERE is_active=TRUE ORDER BY name,id LIMIT $1`, engagementActionCatalogReadLimit)
	if err != nil {
		return nil, fmt.Errorf("list active engagement actions: %w", err)
	}
	return pgx.CollectRows(rows, pgx.RowToAddrOfStructByName[EngagementAction])
}
func (m *EngagementActionModel) ListActiveByIDsForShareTx(ctx context.Context, tx pgx.Tx, ids []uuid.UUID) (map[uuid.UUID]*EngagementAction, error) {
	if err := m.validateBase(); err != nil {
		return nil, err
	}
	if tx == nil {
		return nil, errors.New("transaction is required")
	}
	out := make(map[uuid.UUID]*EngagementAction, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	if len(ids) > engagementActionCatalogReadLimit {
		return nil, fmt.Errorf("%w: too many engagement action ids", ErrMerchantFutureOfferingEngagementInvalidInput)
	}
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()
	rows, err := tx.Query(ctx, `SELECT `+engagementActionSelectColumns+` FROM engagement_actions WHERE id=ANY($1) AND is_active=TRUE ORDER BY id FOR SHARE`, ids)
	if err != nil {
		return nil, fmt.Errorf("lock active engagement actions: %w", err)
	}
	actions, err := pgx.CollectRows(rows, pgx.RowToAddrOfStructByName[EngagementAction])
	if err != nil {
		return nil, err
	}
	for _, a := range actions {
		out[a.ID] = a
	}
	return out, nil
}
