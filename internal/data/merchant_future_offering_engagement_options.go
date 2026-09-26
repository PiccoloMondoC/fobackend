// Package data provides models and database access methods for merchant
// Future Offering Engagement Options.
//
// focodebase/fobackend/internal/data/merchant_future_offering_engagement_options.go
//
// GTM:
//
//	Layer: 2.4 Merchant / Future Offering Domain
//	Release Class: SPINE
//	Reason:
//	  Engagement Options are the merchant-controlled Engagement Actions made
//	  available for one Future Offering. PCDF-M01 establishes them during
//	  creation; readiness and later consumer engagement depend on them.
//
// Domain Boundary:
//
//	An option references one canonical engagement_actions row, belongs to
//	exactly one group of the same Future Offering (composite FK), and owns
//	quantity configuration. Quantity bounds express anticipated consumer
//	demand only; they are not inventory, allocation, or fulfillment
//	commitments. Watch is never an option.
//
// Draft Replacement / Transaction Boundary:
//
//	See merchant_future_offering_engagement_action_groups.go. Options are
//	deleted before groups and inserted after them, inside one service-owned
//	transaction holding the Future Offering row lock.
//
// SPINE Rule:
//
//	Keep compiling.
//	Keep production-ready.
//	Preserve one option per (future_offering_id, engagement_action_id).
//	Preserve the quantity CHECK semantics in service-side validation.
//	Preserve composite group FK integrity.
//	Block deployment if options can reference another FO's group.
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

// MaxMerchantFutureOfferingEngagementQuantity is the Engineering safety
// ceiling for any quantity bound. It protects integer arithmetic and
// downstream aggregation; Administration may narrow it, never widen it.
const MaxMerchantFutureOfferingEngagementQuantity = 1_000_000

const (
	merchantFutureOfferingEngagementOptionActionUniqueConstraint = "ux_merchant_future_offering_engagement_options_action"
	merchantFutureOfferingEngagementOptionQuantityConstraint     = "chk_merchant_future_offering_engagement_options_quantity"
	merchantFutureOfferingEngagementOptionGroupFKConstraint      = "fk_merchant_future_offering_engagement_options_group"
	merchantFutureOfferingEngagementOptionActionFKConstraint     = "merchant_future_offering_engagement_options_engagement_action_id_fkey"
	merchantFutureOfferingEngagementOptionFOFKConstraint         = "merchant_future_offering_engagement_options_future_offering_id_fkey"
	merchantFutureOfferingEngagementGroupFOFKConstraint          = "merchant_future_offering_engagement_action_groups_future_offering_id_fkey"
)

const merchantFutureOfferingEngagementOptionSelectColumns = `
	o.id,
	o.future_offering_id,
	o.engagement_action_group_id,
	o.engagement_action_id,
	o.quantity_enabled,
	o.min_quantity,
	o.max_quantity,
	o.display_order,
	o.is_active,
	o.created_at,
	o.updated_at
`

// MerchantFutureOfferingEngagementOption is one persisted option.
type MerchantFutureOfferingEngagementOption struct {
	ID                      uuid.UUID `json:"id" db:"id"`
	FutureOfferingID        uuid.UUID `json:"future_offering_id" db:"future_offering_id"`
	EngagementActionGroupID uuid.UUID `json:"engagement_action_group_id" db:"engagement_action_group_id"`
	EngagementActionID      uuid.UUID `json:"engagement_action_id" db:"engagement_action_id"`
	QuantityEnabled         bool      `json:"quantity_enabled" db:"quantity_enabled"`
	MinQuantity             *int      `json:"min_quantity,omitempty" db:"min_quantity"`
	MaxQuantity             *int      `json:"max_quantity,omitempty" db:"max_quantity"`
	DisplayOrder            int       `json:"display_order" db:"display_order"`
	IsActive                bool      `json:"is_active" db:"is_active"`
	CreatedAt               time.Time `json:"created_at" db:"created_at"`
	UpdatedAt               time.Time `json:"updated_at" db:"updated_at"`
}

// NewMerchantFutureOfferingEngagementOption is the insert input.
type NewMerchantFutureOfferingEngagementOption struct {
	EngagementActionGroupID uuid.UUID
	EngagementActionID      uuid.UUID
	QuantityEnabled         bool
	MinQuantity             *int
	MaxQuantity             *int
	DisplayOrder            int
}

// MerchantFutureOfferingEngagementOptionModel owns option persistence.
type MerchantFutureOfferingEngagementOptionModel struct {
	DB     *pgxpool.Pool
	Logger *logging.Logger
}

func (m *MerchantFutureOfferingEngagementOptionModel) validateBase() error {
	if m == nil {
		return errors.New("engagement option model is required")
	}
	if m.Logger == nil {
		return errors.New("engagement option model logger is required")
	}
	return nil
}

// ValidateMerchantFutureOfferingEngagementQuantity enforces the canonical
// quantity model (mirrors chk_merchant_future_offering_engagement_options_quantity)
// plus the Engineering safety ceiling.
func ValidateMerchantFutureOfferingEngagementQuantity(enabled bool, minQ, maxQ *int) error {
	if !enabled {
		if minQ != nil || maxQ != nil {
			return engagementInvalidInput("quantity bounds require quantity_enabled")
		}
		return nil
	}
	if minQ == nil || *minQ < 1 {
		return engagementInvalidInput("min_quantity must be at least 1 when quantity is enabled")
	}
	if *minQ > MaxMerchantFutureOfferingEngagementQuantity {
		return engagementInvalidInput("min_quantity exceeds the supported maximum")
	}
	if maxQ != nil {
		if *maxQ < *minQ {
			return engagementInvalidInput("max_quantity must not be less than min_quantity")
		}
		if *maxQ > MaxMerchantFutureOfferingEngagementQuantity {
			return engagementInvalidInput("max_quantity exceeds the supported maximum")
		}
	}
	return nil
}

func classifyMerchantFutureOfferingEngagementWriteError(err error) error {
	switch {
	case IsPgConstraint(err, merchantFutureOfferingEngagementOptionActionUniqueConstraint):
		return fmt.Errorf("%w: an engagement action may be offered only once", ErrMerchantFutureOfferingEngagementInvalidInput)
	case IsPgConstraint(err, merchantFutureOfferingEngagementOptionQuantityConstraint):
		return fmt.Errorf("%w: invalid quantity configuration", ErrMerchantFutureOfferingEngagementInvalidInput)
	case IsPgConstraint(err, merchantFutureOfferingEngagementOptionActionFKConstraint):
		return ErrEngagementActionNotFound
	case IsPgConstraint(err, merchantFutureOfferingEngagementOptionGroupFKConstraint),
		IsPgConstraint(err, merchantFutureOfferingEngagementOptionFOFKConstraint),
		IsPgConstraint(err, merchantFutureOfferingEngagementGroupFOFKConstraint):
		return ErrMerchantFutureOfferingInvalidState
	case IsCheckViolation(err):
		return fmt.Errorf("%w: engagement configuration violates a constraint", ErrMerchantFutureOfferingEngagementInvalidInput)
	case IsForeignKeyViolation(err), IsNotNullViolation(err):
		return ErrMerchantFutureOfferingInvalidState
	default:
		return err
	}
}

// InsertTx inserts one option for a locked draft Future Offering.
func (m *MerchantFutureOfferingEngagementOptionModel) InsertTx(
	ctx context.Context,
	tx pgx.Tx,
	futureOfferingID uuid.UUID,
	in NewMerchantFutureOfferingEngagementOption,
) (*MerchantFutureOfferingEngagementOption, error) {
	if err := m.validateBase(); err != nil {
		return nil, err
	}
	if tx == nil {
		return nil, engagementInvalidInput("transaction is required")
	}
	if futureOfferingID == uuid.Nil || in.EngagementActionGroupID == uuid.Nil || in.EngagementActionID == uuid.Nil {
		return nil, engagementInvalidInput("future_offering_id, group and engagement_action_id are required")
	}
	if in.DisplayOrder < 0 {
		return nil, engagementInvalidInput("display_order must not be negative")
	}
	if err := ValidateMerchantFutureOfferingEngagementQuantity(in.QuantityEnabled, in.MinQuantity, in.MaxQuantity); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	const query = `
		INSERT INTO merchant_future_offering_engagement_options AS o (
			future_offering_id,
			engagement_action_group_id,
			engagement_action_id,
			quantity_enabled,
			min_quantity,
			max_quantity,
			display_order
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING ` + merchantFutureOfferingEngagementOptionSelectColumns
	rows, err := tx.Query(ctx, query,
		futureOfferingID, in.EngagementActionGroupID, in.EngagementActionID,
		in.QuantityEnabled, in.MinQuantity, in.MaxQuantity, in.DisplayOrder,
	)
	if err != nil {
		return nil, classifyMerchantFutureOfferingEngagementWriteError(err)
	}
	option, err := pgx.CollectExactlyOneRow(rows, pgx.RowToAddrOfStructByName[MerchantFutureOfferingEngagementOption])
	if err != nil {
		err = classifyMerchantFutureOfferingEngagementWriteError(err)
		m.Logger.GetLoggerWithContextFromContext(ctx).
			WithFunctionName("InsertMerchantFutureOfferingEngagementOptionTx").
			Error("Insert engagement option failed", err, "future_offering_id", futureOfferingID)
		return nil, err
	}
	return option, nil
}

// DeleteAllForDraftReplacementTx removes every option of a locked draft
// Future Offering. Must run before group deletion.
func (m *MerchantFutureOfferingEngagementOptionModel) DeleteAllForDraftReplacementTx(
	ctx context.Context,
	tx pgx.Tx,
	futureOfferingID uuid.UUID,
) error {
	if err := m.validateBase(); err != nil {
		return err
	}
	if tx == nil || futureOfferingID == uuid.Nil {
		return engagementInvalidInput("transaction and future_offering_id are required")
	}
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()
	const query = `
		DELETE FROM merchant_future_offering_engagement_options
		WHERE future_offering_id = $1
	`
	if _, err := tx.Exec(ctx, query, futureOfferingID); err != nil {
		return classifyMerchantFutureOfferingEngagementWriteError(err)
	}
	return nil
}

// ListActiveTx returns active options of a locked Future Offering ordered by
// group then display order.
func (m *MerchantFutureOfferingEngagementOptionModel) ListActiveTx(
	ctx context.Context,
	tx pgx.Tx,
	futureOfferingID uuid.UUID,
) ([]*MerchantFutureOfferingEngagementOption, error) {
	if err := m.validateBase(); err != nil {
		return nil, err
	}
	if tx == nil || futureOfferingID == uuid.Nil {
		return nil, engagementInvalidInput("transaction and future_offering_id are required")
	}
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()
	const query = `
		SELECT ` + merchantFutureOfferingEngagementOptionSelectColumns + `
		FROM merchant_future_offering_engagement_options o
		WHERE o.future_offering_id = $1
		  AND o.is_active = TRUE
		ORDER BY o.engagement_action_group_id, o.display_order, o.id
		LIMIT $2
	`
	rows, err := tx.Query(ctx, query, futureOfferingID, engagementActionCatalogReadLimit)
	if err != nil {
		return nil, fmt.Errorf("list engagement options: %w", err)
	}
	return pgx.CollectRows(rows, pgx.RowToAddrOfStructByName[MerchantFutureOfferingEngagementOption])
}

// ListActiveForMerchant returns active options of a non-deleted Future
// Offering owned by merchantID.
func (m *MerchantFutureOfferingEngagementOptionModel) ListActiveForMerchant(
	ctx context.Context,
	merchantID uuid.UUID,
	futureOfferingID uuid.UUID,
) ([]*MerchantFutureOfferingEngagementOption, error) {
	if err := m.validateBase(); err != nil {
		return nil, err
	}
	if m.DB == nil {
		return nil, errors.New("engagement option model database pool is required")
	}
	if merchantID == uuid.Nil || futureOfferingID == uuid.Nil {
		return nil, engagementInvalidInput("merchant_id and future_offering_id are required")
	}
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()
	const query = `
		SELECT ` + merchantFutureOfferingEngagementOptionSelectColumns + `
		FROM merchant_future_offering_engagement_options o
		JOIN merchant_future_offerings fo
		  ON fo.id = o.future_offering_id
		WHERE o.future_offering_id = $1
		  AND fo.merchant_id = $2
		  AND fo.deleted_at IS NULL
		  AND o.is_active = TRUE
		ORDER BY o.engagement_action_group_id, o.display_order, o.id
		LIMIT $3
	`
	rows, err := m.DB.Query(ctx, query, futureOfferingID, merchantID, engagementActionCatalogReadLimit)
	if err != nil {
		return nil, fmt.Errorf("list engagement options for merchant: %w", err)
	}
	return pgx.CollectRows(rows, pgx.RowToAddrOfStructByName[MerchantFutureOfferingEngagementOption])
}
