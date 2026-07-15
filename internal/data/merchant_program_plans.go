// Package data provides models and database access methods for merchant program plans.
//
// sdworkspace/sdbackend/internal/data/merchant_program_plans.go
//
// GTM:
//
//	Layer: 2.4 Merchant / Future Offering Domain
//	Release Class: SPINE
//	Reason:
//	  merchant_program_plans is release-critical merchant monetization
//	  infrastructure for Future Offering access, Launch Campaign access,
//	  merchant subscriptions, entitlement assignment, fee schedules, billing
//	  accounts, and Merchant Center plan selection. Future Offering is
//	  Sagrenti's core business object, and program-plan integrity is required
//	  before merchant-owned future-commerce workflows can be safely activated.
//
// SPINE Rule:
//
//	Keep compiling.
//	Keep production-ready.
//	Preserve plan-code integrity.
//	Preserve subscription and entitlement foreign-key readiness.
//	Preserve soft-delete lifecycle semantics.
//	Preserve active-plan lookup behavior.
//	Block deployment if this file breaks build, merchant plan persistence,
//	Future Offering entitlement readiness, subscription plan resolution,
//	or merchant billing plan integrity.
package data

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/PiccoloMondoC/sdworkspace/sdbackend/internal/logging"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// MerchantPlanCode is the controlled vocabulary for merchant program plans.
type MerchantPlanCode string

const (
	// PlanCodeStandard is the standard merchant program plan.
	PlanCodeStandard MerchantPlanCode = "standard"

	// PlanCodePremium is the premium merchant program plan.
	PlanCodePremium MerchantPlanCode = "premium"

	// PlanCodeEnterprise is the enterprise merchant program plan.
	PlanCodeEnterprise MerchantPlanCode = "enterprise"
)

const merchantProgramPlanSelectColumns = `
	id,
	code,
	name,
	description,
	is_active,
	created_at,
	updated_at,
	deleted_at
`

// MerchantProgramPlan represents a row in merchant_program_plans.
type MerchantProgramPlan struct {
	ID          uuid.UUID        `json:"id" db:"id"`
	Code        MerchantPlanCode `json:"code" db:"code"`
	Name        string           `json:"name" db:"name"`
	Description string           `json:"description" db:"description"`
	IsActive    bool             `json:"is_active" db:"is_active"`
	CreatedAt   time.Time        `json:"created_at" db:"created_at"`
	UpdatedAt   time.Time        `json:"updated_at" db:"updated_at"`
	DeletedAt   *time.Time       `json:"deleted_at,omitempty" db:"deleted_at"`
}

// MerchantProgramPlanModel owns persistence for merchant program plans.
type MerchantProgramPlanModel struct {
	DB     *pgxpool.Pool
	Logger *logging.Logger
}

func scanMerchantProgramPlan(row pgx.Row, plan *MerchantProgramPlan) error {
	return row.Scan(
		&plan.ID,
		&plan.Code,
		&plan.Name,
		&plan.Description,
		&plan.IsActive,
		&plan.CreatedAt,
		&plan.UpdatedAt,
		&plan.DeletedAt,
	)
}

func scanMerchantProgramPlanFromRows(rows pgx.Rows, plan *MerchantProgramPlan) error {
	return rows.Scan(
		&plan.ID,
		&plan.Code,
		&plan.Name,
		&plan.Description,
		&plan.IsActive,
		&plan.CreatedAt,
		&plan.UpdatedAt,
		&plan.DeletedAt,
	)
}

// NormalizeMerchantPlanCode trims and canonicalizes a merchant plan code.
func NormalizeMerchantPlanCode(code MerchantPlanCode) MerchantPlanCode {
	return MerchantPlanCode(strings.ToLower(strings.TrimSpace(string(code))))
}

// IsValidMerchantPlanCode reports whether code is one of the allowed plan codes.
func IsValidMerchantPlanCode(code MerchantPlanCode) bool {
	switch NormalizeMerchantPlanCode(code) {
	case PlanCodeStandard, PlanCodePremium, PlanCodeEnterprise:
		return true
	default:
		return false
	}
}

func normalizeMerchantProgramPlan(plan *MerchantProgramPlan) {
	plan.Code = NormalizeMerchantPlanCode(plan.Code)
	plan.Name = strings.TrimSpace(plan.Name)
	plan.Description = strings.TrimSpace(plan.Description)
}

func validateMerchantProgramPlanForInsert(plan *MerchantProgramPlan) error {
	if plan == nil {
		return errors.New("merchant program plan is required")
	}
	normalizeMerchantProgramPlan(plan)
	if !IsValidMerchantPlanCode(plan.Code) {
		return fmt.Errorf("invalid merchant program plan code: %s", plan.Code)
	}
	if plan.Name == "" {
		return errors.New("merchant program plan name is required")
	}
	return nil
}

func validateMerchantProgramPlanForUpdate(plan *MerchantProgramPlan) error {
	if plan == nil {
		return errors.New("merchant program plan is required")
	}
	if plan.ID == uuid.Nil {
		return errors.New("merchant program plan ID is required")
	}
	plan.Name = strings.TrimSpace(plan.Name)
	plan.Description = strings.TrimSpace(plan.Description)
	if plan.Name == "" {
		return errors.New("merchant program plan name is required")
	}
	return nil
}

func isMerchantProgramPlanDuplicate(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

// Insert inserts a new merchant program plan.
//
// If plan.ID is uuid.Nil, a new UUID is generated. A caller-provided ID is
// accepted to support controlled seed paths only; ordinary application callers
// should leave plan.ID as uuid.Nil.
func (m *MerchantProgramPlanModel) Insert(ctx context.Context, plan *MerchantProgramPlan) error {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("InsertMerchantProgramPlan")

	if err := validateMerchantProgramPlanForInsert(plan); err != nil {
		logger.Error("Validation failed", err)
		return err
	}

	if plan.ID == uuid.Nil {
		plan.ID = uuid.New()
	}

	query := `
		INSERT INTO merchant_program_plans (
			id,
			code,
			name,
			description
		)
		VALUES ($1, $2, $3, $4)
		RETURNING is_active, created_at, updated_at, deleted_at
	`

	err := m.DB.QueryRow(ctx, query,
		plan.ID,
		plan.Code,
		plan.Name,
		plan.Description,
	).Scan(
		&plan.IsActive,
		&plan.CreatedAt,
		&plan.UpdatedAt,
		&plan.DeletedAt,
	)
	if err != nil {
		if isMerchantProgramPlanDuplicate(err) {
			err = fmt.Errorf("merchant program plan already exists for code %s", plan.Code)
		}
		logger.Error("Insert merchant program plan failed", err, "code", plan.Code)
		return err
	}

	logger.Info("Insert merchant program plan successful", "plan_id", plan.ID, "code", plan.Code)
	return nil
}

// GetByID retrieves a non-deleted merchant program plan by ID.
func (m *MerchantProgramPlanModel) GetByID(ctx context.Context, id uuid.UUID) (*MerchantProgramPlan, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("GetMerchantProgramPlanByID")

	if id == uuid.Nil {
		err := errors.New("merchant program plan ID is required")
		logger.Error("Validation failed", err)
		return nil, err
	}

	query := `
		SELECT ` + merchantProgramPlanSelectColumns + `
		FROM merchant_program_plans
		WHERE id = $1
		  AND deleted_at IS NULL
	`

	var plan MerchantProgramPlan
	err := scanMerchantProgramPlan(m.DB.QueryRow(ctx, query, id), &plan)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			logger.Warn("Merchant program plan not found", "plan_id", id)
			return nil, nil
		}
		logger.Error("Get merchant program plan by ID failed", err, "plan_id", id)
		return nil, err
	}

	logger.Info("Get merchant program plan by ID successful", "plan_id", plan.ID, "code", plan.Code)
	return &plan, nil
}

// GetByCode retrieves a non-deleted merchant program plan by code.
func (m *MerchantProgramPlanModel) GetByCode(ctx context.Context, code MerchantPlanCode) (*MerchantProgramPlan, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("GetMerchantProgramPlanByCode")

	code = NormalizeMerchantPlanCode(code)
	if !IsValidMerchantPlanCode(code) {
		err := fmt.Errorf("invalid merchant program plan code: %s", code)
		logger.Error("Validation failed", err)
		return nil, err
	}

	query := `
		SELECT ` + merchantProgramPlanSelectColumns + `
		FROM merchant_program_plans
		WHERE code = $1
		  AND deleted_at IS NULL
		LIMIT 1
	`

	var plan MerchantProgramPlan
	err := scanMerchantProgramPlan(m.DB.QueryRow(ctx, query, code), &plan)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			logger.Warn("Merchant program plan not found", "code", code)
			return nil, nil
		}
		logger.Error("Get merchant program plan by code failed", err, "code", code)
		return nil, err
	}

	logger.Info("Get merchant program plan by code successful", "plan_id", plan.ID, "code", plan.Code)
	return &plan, nil
}

// GetActiveByCode retrieves an active, non-deleted merchant program plan by code.
func (m *MerchantProgramPlanModel) GetActiveByCode(ctx context.Context, code MerchantPlanCode) (*MerchantProgramPlan, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("GetActiveMerchantProgramPlanByCode")

	code = NormalizeMerchantPlanCode(code)
	if !IsValidMerchantPlanCode(code) {
		err := fmt.Errorf("invalid merchant program plan code: %s", code)
		logger.Error("Validation failed", err)
		return nil, err
	}

	query := `
		SELECT ` + merchantProgramPlanSelectColumns + `
		FROM merchant_program_plans
		WHERE code = $1
		  AND is_active = TRUE
		  AND deleted_at IS NULL
		LIMIT 1
	`

	var plan MerchantProgramPlan
	err := scanMerchantProgramPlan(m.DB.QueryRow(ctx, query, code), &plan)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			logger.Warn("Active merchant program plan not found", "code", code)
			return nil, nil
		}
		logger.Error("Get active merchant program plan by code failed", err, "code", code)
		return nil, err
	}

	logger.Info("Get active merchant program plan by code successful", "plan_id", plan.ID, "code", plan.Code)
	return &plan, nil
}

// GetAll retrieves merchant program plans with explicit pagination.
func (m *MerchantProgramPlanModel) GetAll(ctx context.Context, includeDeleted bool, limit, offset int) ([]*MerchantProgramPlan, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("GetAllMerchantProgramPlans")

	if limit <= 0 || limit > 100 {
		err := errors.New("limit must be between 1 and 100")
		logger.Error("Validation failed", err)
		return nil, err
	}
	if offset < 0 {
		err := errors.New("offset must be non-negative")
		logger.Error("Validation failed", err)
		return nil, err
	}

	query := `
		SELECT ` + merchantProgramPlanSelectColumns + `
		FROM merchant_program_plans
	`
	if !includeDeleted {
		query += ` WHERE deleted_at IS NULL`
	}
	query += `
		ORDER BY code ASC
		LIMIT $1 OFFSET $2
	`

	rows, err := m.DB.Query(ctx, query, limit, offset)
	if err != nil {
		logger.Error("Get all merchant program plans query failed", err)
		return nil, err
	}
	defer rows.Close()

	var plans []*MerchantProgramPlan
	for rows.Next() {
		var plan MerchantProgramPlan
		if err := scanMerchantProgramPlanFromRows(rows, &plan); err != nil {
			logger.Error("Merchant program plan row scan failed", err)
			return nil, err
		}
		plans = append(plans, &plan)
	}

	if err := rows.Err(); err != nil {
		logger.Error("Merchant program plan row iteration failed", err)
		return nil, err
	}

	logger.Info("Get all merchant program plans successful", "count", len(plans), "include_deleted", includeDeleted)
	return plans, nil
}

// ListActive retrieves all active, non-deleted merchant program plans.
func (m *MerchantProgramPlanModel) ListActive(ctx context.Context) ([]*MerchantProgramPlan, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("ListActiveMerchantProgramPlans")

	query := `
		SELECT ` + merchantProgramPlanSelectColumns + `
		FROM merchant_program_plans
		WHERE is_active = TRUE
		  AND deleted_at IS NULL
		ORDER BY code ASC
	`

	rows, err := m.DB.Query(ctx, query)
	if err != nil {
		logger.Error("List active merchant program plans query failed", err)
		return nil, err
	}
	defer rows.Close()

	var plans []*MerchantProgramPlan
	for rows.Next() {
		var plan MerchantProgramPlan
		if err := scanMerchantProgramPlanFromRows(rows, &plan); err != nil {
			logger.Error("Merchant program plan row scan failed", err)
			return nil, err
		}
		plans = append(plans, &plan)
	}

	if err := rows.Err(); err != nil {
		logger.Error("Merchant program plan row iteration failed", err)
		return nil, err
	}

	logger.Info("List active merchant program plans successful", "count", len(plans))
	return plans, nil
}

// Exists checks whether a non-deleted merchant program plan exists by ID.
func (m *MerchantProgramPlanModel) Exists(ctx context.Context, id uuid.UUID) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("ExistsMerchantProgramPlan")

	if id == uuid.Nil {
		err := errors.New("merchant program plan ID is required")
		logger.Error("Validation failed", err)
		return false, err
	}

	query := `
		SELECT EXISTS (
			SELECT 1
			FROM merchant_program_plans
			WHERE id = $1
			  AND deleted_at IS NULL
		)
	`

	var exists bool
	err := m.DB.QueryRow(ctx, query, id).Scan(&exists)
	if err != nil {
		logger.Error("Merchant program plan exists query failed", err, "plan_id", id)
		return false, err
	}

	logger.Info("Merchant program plan exists check successful", "plan_id", id, "exists", exists)
	return exists, nil
}

// Update updates mutable display fields for a non-deleted merchant program plan.
// The plan code is intentionally immutable after insert.
func (m *MerchantProgramPlanModel) Update(ctx context.Context, plan *MerchantProgramPlan) error {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("UpdateMerchantProgramPlan")

	if err := validateMerchantProgramPlanForUpdate(plan); err != nil {
		logger.Error("Validation failed", err)
		return err
	}

	query := `
		UPDATE merchant_program_plans
		SET
			name = $1,
			description = $2
		WHERE id = $3
		  AND deleted_at IS NULL
		RETURNING code, is_active, created_at, updated_at, deleted_at
	`

	err := m.DB.QueryRow(ctx, query,
		plan.Name,
		plan.Description,
		plan.ID,
	).Scan(
		&plan.Code,
		&plan.IsActive,
		&plan.CreatedAt,
		&plan.UpdatedAt,
		&plan.DeletedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			err = fmt.Errorf("no active merchant program plan found with ID %s", plan.ID)
		}
		logger.Error("Update merchant program plan failed", err, "plan_id", plan.ID)
		return err
	}

	logger.Info("Update merchant program plan successful", "plan_id", plan.ID, "code", plan.Code)
	return nil
}

// Activate marks a non-deleted merchant program plan as active.
func (m *MerchantProgramPlanModel) Activate(ctx context.Context, id uuid.UUID) error {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("ActivateMerchantProgramPlan")

	if id == uuid.Nil {
		err := errors.New("merchant program plan ID is required")
		logger.Error("Validation failed", err)
		return err
	}

	query := `
		UPDATE merchant_program_plans
		SET is_active = TRUE
		WHERE id = $1
		  AND deleted_at IS NULL
		RETURNING updated_at
	`

	var updatedAt time.Time
	err := m.DB.QueryRow(ctx, query, id).Scan(&updatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			err = fmt.Errorf("no non-deleted merchant program plan found with ID %s", id)
		}
		logger.Error("Activate merchant program plan failed", err, "plan_id", id)
		return err
	}

	logger.Info("Activate merchant program plan successful", "plan_id", id, "updated_at", updatedAt)
	return nil
}

// Deactivate marks a non-deleted merchant program plan as inactive.
func (m *MerchantProgramPlanModel) Deactivate(ctx context.Context, id uuid.UUID) error {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("DeactivateMerchantProgramPlan")

	if id == uuid.Nil {
		err := errors.New("merchant program plan ID is required")
		logger.Error("Validation failed", err)
		return err
	}

	query := `
		UPDATE merchant_program_plans
		SET is_active = FALSE
		WHERE id = $1
		  AND deleted_at IS NULL
		RETURNING updated_at
	`

	var updatedAt time.Time
	err := m.DB.QueryRow(ctx, query, id).Scan(&updatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			err = fmt.Errorf("no non-deleted merchant program plan found with ID %s", id)
		}
		logger.Error("Deactivate merchant program plan failed", err, "plan_id", id)
		return err
	}

	logger.Info("Deactivate merchant program plan successful", "plan_id", id, "updated_at", updatedAt)
	return nil
}

// SoftDelete marks a merchant program plan as deleted by setting deleted_at = NOW().
func (m *MerchantProgramPlanModel) SoftDelete(ctx context.Context, id uuid.UUID) error {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("SoftDeleteMerchantProgramPlan")

	if id == uuid.Nil {
		err := errors.New("merchant program plan ID is required")
		logger.Error("Validation failed", err)
		return err
	}

	query := `
		UPDATE merchant_program_plans
		SET deleted_at = NOW()
		WHERE id = $1
		  AND deleted_at IS NULL
		RETURNING deleted_at
	`

	var deletedAt time.Time
	err := m.DB.QueryRow(ctx, query, id).Scan(&deletedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			err = fmt.Errorf("no active merchant program plan found with ID %s", id)
		}
		logger.Error("Soft delete merchant program plan failed", err, "plan_id", id)
		return err
	}

	logger.Info("Soft delete merchant program plan successful", "plan_id", id, "deleted_at", deletedAt)
	return nil
}

// Restore clears deleted_at for a soft-deleted merchant program plan.
//
// The plan's is_active state is preserved from before deletion. If the restored
// plan should be active, callers must call Activate separately.
func (m *MerchantProgramPlanModel) Restore(ctx context.Context, id uuid.UUID) error {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("RestoreMerchantProgramPlan")

	if id == uuid.Nil {
		err := errors.New("merchant program plan ID is required")
		logger.Error("Validation failed", err)
		return err
	}

	query := `
		UPDATE merchant_program_plans
		SET deleted_at = NULL
		WHERE id = $1
		  AND deleted_at IS NOT NULL
		RETURNING updated_at
	`

	var updatedAt time.Time
	err := m.DB.QueryRow(ctx, query, id).Scan(&updatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			err = fmt.Errorf("no soft-deleted merchant program plan found with ID %s", id)
		}
		logger.Error("Restore merchant program plan failed", err, "plan_id", id)
		return err
	}

	logger.Info("Restore merchant program plan successful", "plan_id", id, "updated_at", updatedAt)
	return nil
}