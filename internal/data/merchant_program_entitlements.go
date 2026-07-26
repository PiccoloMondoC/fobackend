// Package data provides models and database access methods for merchant program entitlements.
//
// sdworkspace/sdbackend/internal/data/merchant_program_entitlements.go
//
// GTM:
//
//	Layer: 2.4 Merchant / Future Offering Domain
//	Release Class: SPINE
//	Reason:
//	  merchant_program_entitlements is release-critical merchant monetization
//	  and capability-gating infrastructure for Future Offering v1. It connects
//	  merchant program plans to the capabilities they unlock, including Launch
//	  Campaign access and Future Offering / Launch Intelligence access.
//
//	  Future Offering is the Platform's core business object. A merchant program
//	  plan must not be treated as capable of activating Future Offering
//	  workflows unless the plan carries the appropriate entitlement.
//
//	  The database enforces one compound invariant through
//	  ensure_future_offering_includes_campaign_trigger: inserting
//	  future_offering_access automatically ensures launch_campaign_access for
//	  the same plan. Application code must respect and document that invariant
//	  without shadowing it with duplicate policy.
//
// SPINE Rule:
//
//	Keep compiling.
//	Keep production-ready.
//	Preserve entitlement-code integrity.
//	Preserve plan-entitlement uniqueness.
//	Preserve plan foreign-key readiness.
//	Preserve trigger-owned Future Offering includes Launch Campaign invariant.
//	Preserve idempotent startup seed behavior.
//	Block deployment if this file breaks build, merchant capability gating,
//	Future Offering entitlement checks, Launch Campaign entitlement checks,
//	or startup merchant-plan entitlement seeding.
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
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// MerchantProgramEntitlementCode is the controlled vocabulary for merchant
// program entitlement codes.
type MerchantProgramEntitlementCode string

const (
	// EntitlementCodeLaunchCampaignAccess grants access to Launch Campaign
	// capability.
	EntitlementCodeLaunchCampaignAccess MerchantProgramEntitlementCode = "launch_campaign_access"

	// EntitlementCodeFutureOfferingAccess grants access to Future Offering and
	// Launch Intelligence capability. The database trigger ensures this
	// entitlement also implies EntitlementCodeLaunchCampaignAccess for the same
	// plan.
	EntitlementCodeFutureOfferingAccess MerchantProgramEntitlementCode = "future_offering_access"
)

const merchantProgramEntitlementSelectColumns = `
	id,
	plan_id,
	entitlement_code,
	created_at
`

// MerchantProgramEntitlement represents a row in merchant_program_entitlements.
type MerchantProgramEntitlement struct {
	ID              uuid.UUID                      `json:"id" db:"id"`
	PlanID          uuid.UUID                      `json:"plan_id" db:"plan_id"`
	EntitlementCode MerchantProgramEntitlementCode `json:"entitlement_code" db:"entitlement_code"`
	CreatedAt       time.Time                      `json:"created_at" db:"created_at"`
}

// MerchantProgramEntitlementModel owns persistence for merchant program
// entitlements.
type MerchantProgramEntitlementModel struct {
	DB     *pgxpool.Pool
	Logger *logging.Logger
}

func scanMerchantProgramEntitlement(row pgx.Row, entitlement *MerchantProgramEntitlement) error {
	return row.Scan(
		&entitlement.ID,
		&entitlement.PlanID,
		&entitlement.EntitlementCode,
		&entitlement.CreatedAt,
	)
}

func scanMerchantProgramEntitlementFromRows(rows pgx.Rows, entitlement *MerchantProgramEntitlement) error {
	return rows.Scan(
		&entitlement.ID,
		&entitlement.PlanID,
		&entitlement.EntitlementCode,
		&entitlement.CreatedAt,
	)
}

// NormalizeMerchantProgramEntitlementCode trims and canonicalizes a merchant
// program entitlement code.
func NormalizeMerchantProgramEntitlementCode(code MerchantProgramEntitlementCode) MerchantProgramEntitlementCode {
	return MerchantProgramEntitlementCode(strings.ToLower(strings.TrimSpace(string(code))))
}

// IsValidMerchantProgramEntitlementCode reports whether code is an allowed
// merchant program entitlement code.
func IsValidMerchantProgramEntitlementCode(code MerchantProgramEntitlementCode) bool {
	switch NormalizeMerchantProgramEntitlementCode(code) {
	case EntitlementCodeLaunchCampaignAccess, EntitlementCodeFutureOfferingAccess:
		return true
	default:
		return false
	}
}

func normalizeMerchantProgramEntitlement(entitlement *MerchantProgramEntitlement) {
	entitlement.EntitlementCode = NormalizeMerchantProgramEntitlementCode(entitlement.EntitlementCode)
}

func validateMerchantProgramEntitlementForInsert(entitlement *MerchantProgramEntitlement) error {
	if entitlement == nil {
		return errors.New("merchant program entitlement is required")
	}
	if entitlement.PlanID == uuid.Nil {
		return errors.New("merchant program entitlement plan ID is required")
	}
	normalizeMerchantProgramEntitlement(entitlement)
	if !IsValidMerchantProgramEntitlementCode(entitlement.EntitlementCode) {
		return fmt.Errorf("invalid merchant program entitlement code: %s", entitlement.EntitlementCode)
	}
	return nil
}

func validateMerchantProgramEntitlementCode(code MerchantProgramEntitlementCode) (MerchantProgramEntitlementCode, error) {
	code = NormalizeMerchantProgramEntitlementCode(code)
	if !IsValidMerchantProgramEntitlementCode(code) {
		return "", fmt.Errorf("invalid merchant program entitlement code: %s", code)
	}
	return code, nil
}

func isMerchantProgramEntitlementDuplicate(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

func isMerchantProgramEntitlementForeignKeyViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23503"
}

// Insert inserts a new merchant program entitlement.
//
// If entitlement.ID is uuid.Nil, a new UUID is generated. Duplicate
// (plan_id, entitlement_code) pairs return an error. Callers that need
// idempotent startup or seed behavior should use Ensure.
//
// Inserting EntitlementCodeFutureOfferingAccess may cause the database trigger
// to also insert EntitlementCodeLaunchCampaignAccess for the same plan.
func (m *MerchantProgramEntitlementModel) Insert(ctx context.Context, entitlement *MerchantProgramEntitlement) error {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("InsertMerchantProgramEntitlement")

	if err := validateMerchantProgramEntitlementForInsert(entitlement); err != nil {
		logger.Error("Validation failed", err)
		return err
	}

	if entitlement.ID == uuid.Nil {
		entitlement.ID = uuid.New()
	}

	query := `
		INSERT INTO merchant_program_entitlements (
			id,
			plan_id,
			entitlement_code
		)
		VALUES ($1, $2, $3)
		RETURNING created_at
	`

	err := m.DB.QueryRow(ctx, query,
		entitlement.ID,
		entitlement.PlanID,
		entitlement.EntitlementCode,
	).Scan(&entitlement.CreatedAt)
	if err != nil {
		switch {
		case isMerchantProgramEntitlementDuplicate(err):
			err = fmt.Errorf(
				"merchant program entitlement already exists for plan %s and code %s",
				entitlement.PlanID,
				entitlement.EntitlementCode,
			)
		case isMerchantProgramEntitlementForeignKeyViolation(err):
			err = fmt.Errorf(
				"merchant program entitlement references missing merchant program plan %s",
				entitlement.PlanID,
			)
		}

		logger.Error("Insert merchant program entitlement failed", err,
			"entitlement_id", entitlement.ID,
			"plan_id", entitlement.PlanID,
			"entitlement_code", entitlement.EntitlementCode,
		)
		return err
	}

	logger.Info("Insert merchant program entitlement successful",
		"entitlement_id", entitlement.ID,
		"plan_id", entitlement.PlanID,
		"entitlement_code", entitlement.EntitlementCode,
	)
	return nil
}

// Ensure inserts a merchant program entitlement if missing and returns the
// persisted row. It is idempotent and safe for startup seed paths.
//
// This method intentionally uses a single INSERT ... ON CONFLICT ... RETURNING
// statement so callers receive a canonical persisted row without a race-prone
// exec-then-read sequence.
//
// Inserting EntitlementCodeFutureOfferingAccess may cause the database trigger
// to also insert EntitlementCodeLaunchCampaignAccess for the same plan. This
// method returns only the explicitly requested entitlement row.
func (m *MerchantProgramEntitlementModel) Ensure(
	ctx context.Context,
	planID uuid.UUID,
	code MerchantProgramEntitlementCode,
) (*MerchantProgramEntitlement, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("EnsureMerchantProgramEntitlement")

	if planID == uuid.Nil {
		err := errors.New("merchant program entitlement plan ID is required")
		logger.Error("Validation failed", err)
		return nil, err
	}

	code, err := validateMerchantProgramEntitlementCode(code)
	if err != nil {
		logger.Error("Validation failed", err)
		return nil, err
	}

	query := `
		INSERT INTO merchant_program_entitlements (
			plan_id,
			entitlement_code
		)
		VALUES ($1, $2)
		ON CONFLICT (plan_id, entitlement_code) DO UPDATE
		SET entitlement_code = EXCLUDED.entitlement_code
		RETURNING ` + merchantProgramEntitlementSelectColumns + `
	`

	var entitlement MerchantProgramEntitlement
	err = scanMerchantProgramEntitlement(m.DB.QueryRow(ctx, query, planID, code), &entitlement)
	if err != nil {
		logger.Error("Ensure merchant program entitlement failed", err,
			"plan_id", planID,
			"entitlement_code", code,
		)
		return nil, err
	}

	logger.Info("Ensure merchant program entitlement successful",
		"entitlement_id", entitlement.ID,
		"plan_id", entitlement.PlanID,
		"entitlement_code", entitlement.EntitlementCode,
	)
	return &entitlement, nil
}

// GetByID retrieves a merchant program entitlement by ID.
func (m *MerchantProgramEntitlementModel) GetByID(ctx context.Context, id uuid.UUID) (*MerchantProgramEntitlement, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("GetMerchantProgramEntitlementByID")

	if id == uuid.Nil {
		err := errors.New("merchant program entitlement ID is required")
		logger.Error("Validation failed", err)
		return nil, err
	}

	query := `
		SELECT ` + merchantProgramEntitlementSelectColumns + `
		FROM merchant_program_entitlements
		WHERE id = $1
	`

	var entitlement MerchantProgramEntitlement
	err := scanMerchantProgramEntitlement(m.DB.QueryRow(ctx, query, id), &entitlement)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			logger.Warn("Merchant program entitlement not found", "entitlement_id", id)
			return nil, nil
		}
		logger.Error("Get merchant program entitlement by ID failed", err, "entitlement_id", id)
		return nil, err
	}

	logger.Info("Get merchant program entitlement by ID successful",
		"entitlement_id", entitlement.ID,
		"plan_id", entitlement.PlanID,
		"entitlement_code", entitlement.EntitlementCode,
	)
	return &entitlement, nil
}

// GetByPlanAndCode retrieves a merchant program entitlement by plan ID and
// entitlement code.
func (m *MerchantProgramEntitlementModel) GetByPlanAndCode(
	ctx context.Context,
	planID uuid.UUID,
	code MerchantProgramEntitlementCode,
) (*MerchantProgramEntitlement, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("GetMerchantProgramEntitlementByPlanAndCode")

	if planID == uuid.Nil {
		err := errors.New("merchant program entitlement plan ID is required")
		logger.Error("Validation failed", err)
		return nil, err
	}

	code, err := validateMerchantProgramEntitlementCode(code)
	if err != nil {
		logger.Error("Validation failed", err)
		return nil, err
	}

	query := `
		SELECT ` + merchantProgramEntitlementSelectColumns + `
		FROM merchant_program_entitlements
		WHERE plan_id = $1
		  AND entitlement_code = $2
		LIMIT 1
	`

	var entitlement MerchantProgramEntitlement
	err = scanMerchantProgramEntitlement(m.DB.QueryRow(ctx, query, planID, code), &entitlement)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			logger.Warn("Merchant program entitlement not found",
				"plan_id", planID,
				"entitlement_code", code,
			)
			return nil, nil
		}
		logger.Error("Get merchant program entitlement by plan and code failed", err,
			"plan_id", planID,
			"entitlement_code", code,
		)
		return nil, err
	}

	logger.Info("Get merchant program entitlement by plan and code successful",
		"entitlement_id", entitlement.ID,
		"plan_id", entitlement.PlanID,
		"entitlement_code", entitlement.EntitlementCode,
	)
	return &entitlement, nil
}

// ListByPlanID retrieves all merchant program entitlements for a plan.
func (m *MerchantProgramEntitlementModel) ListByPlanID(ctx context.Context, planID uuid.UUID) ([]*MerchantProgramEntitlement, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("ListMerchantProgramEntitlementsByPlanID")

	if planID == uuid.Nil {
		err := errors.New("merchant program entitlement plan ID is required")
		logger.Error("Validation failed", err)
		return nil, err
	}

	query := `
		SELECT ` + merchantProgramEntitlementSelectColumns + `
		FROM merchant_program_entitlements
		WHERE plan_id = $1
		ORDER BY entitlement_code ASC
	`

	rows, err := m.DB.Query(ctx, query, planID)
	if err != nil {
		logger.Error("List merchant program entitlements by plan ID query failed", err, "plan_id", planID)
		return nil, err
	}
	defer rows.Close()

	var entitlements []*MerchantProgramEntitlement
	for rows.Next() {
		var entitlement MerchantProgramEntitlement
		if err := scanMerchantProgramEntitlementFromRows(rows, &entitlement); err != nil {
			logger.Error("Merchant program entitlement row scan failed", err, "plan_id", planID)
			return nil, err
		}
		entitlements = append(entitlements, &entitlement)
	}

	if err := rows.Err(); err != nil {
		logger.Error("Merchant program entitlement row iteration failed", err, "plan_id", planID)
		return nil, err
	}

	logger.Info("List merchant program entitlements by plan ID successful",
		"plan_id", planID,
		"count", len(entitlements),
	)
	return entitlements, nil
}

// PlanHasEntitlement checks whether a plan has a specific merchant program
// entitlement.
func (m *MerchantProgramEntitlementModel) PlanHasEntitlement(
	ctx context.Context,
	planID uuid.UUID,
	code MerchantProgramEntitlementCode,
) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("PlanHasMerchantProgramEntitlement")

	if planID == uuid.Nil {
		err := errors.New("merchant program entitlement plan ID is required")
		logger.Error("Validation failed", err)
		return false, err
	}

	code, err := validateMerchantProgramEntitlementCode(code)
	if err != nil {
		logger.Error("Validation failed", err)
		return false, err
	}

	query := `
		SELECT EXISTS (
			SELECT 1
			FROM merchant_program_entitlements
			WHERE plan_id = $1
			  AND entitlement_code = $2
		)
	`

	var exists bool
	err = m.DB.QueryRow(ctx, query, planID, code).Scan(&exists)
	if err != nil {
		logger.Error("Merchant program entitlement existence check failed", err,
			"plan_id", planID,
			"entitlement_code", code,
		)
		return false, err
	}

	return exists, nil
}

// Delete hard-deletes a merchant program entitlement by ID.
//
// The schema does not include deleted_at, so deletion is a true hard delete.
// Removing EntitlementCodeFutureOfferingAccess does not automatically remove
// EntitlementCodeLaunchCampaignAccess because the database trigger is insert
// only. Higher-level revocation policy belongs in service orchestration.
func (m *MerchantProgramEntitlementModel) Delete(ctx context.Context, id uuid.UUID) error {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("DeleteMerchantProgramEntitlement")

	if id == uuid.Nil {
		err := errors.New("merchant program entitlement ID is required")
		logger.Error("Validation failed", err)
		return err
	}

	query := `
		DELETE FROM merchant_program_entitlements
		WHERE id = $1
		RETURNING plan_id, entitlement_code
	`

	var planID uuid.UUID
	var code MerchantProgramEntitlementCode
	err := m.DB.QueryRow(ctx, query, id).Scan(&planID, &code)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			err = fmt.Errorf("no merchant program entitlement found with ID %s", id)
		}
		logger.Error("Delete merchant program entitlement failed", err, "entitlement_id", id)
		return err
	}

	logger.Info("Delete merchant program entitlement successful",
		"entitlement_id", id,
		"plan_id", planID,
		"entitlement_code", code,
	)
	return nil
}

// DeleteByPlanAndCode hard-deletes a merchant program entitlement by plan ID
// and entitlement code.
//
// The schema does not include deleted_at, so deletion is a true hard delete.
// Removing EntitlementCodeFutureOfferingAccess does not automatically remove
// EntitlementCodeLaunchCampaignAccess because the database trigger is insert
// only. Higher-level revocation policy belongs in service orchestration.
func (m *MerchantProgramEntitlementModel) DeleteByPlanAndCode(
	ctx context.Context,
	planID uuid.UUID,
	code MerchantProgramEntitlementCode,
) error {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("DeleteMerchantProgramEntitlementByPlanAndCode")

	if planID == uuid.Nil {
		err := errors.New("merchant program entitlement plan ID is required")
		logger.Error("Validation failed", err)
		return err
	}

	code, err := validateMerchantProgramEntitlementCode(code)
	if err != nil {
		logger.Error("Validation failed", err)
		return err
	}

	query := `
		DELETE FROM merchant_program_entitlements
		WHERE plan_id = $1
		  AND entitlement_code = $2
		RETURNING id
	`

	var id uuid.UUID
	err = m.DB.QueryRow(ctx, query, planID, code).Scan(&id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			err = fmt.Errorf(
				"no merchant program entitlement found for plan %s and code %s",
				planID,
				code,
			)
		}
		logger.Error("Delete merchant program entitlement by plan and code failed", err,
			"plan_id", planID,
			"entitlement_code", code,
		)
		return err
	}

	logger.Info("Delete merchant program entitlement by plan and code successful",
		"entitlement_id", id,
		"plan_id", planID,
		"entitlement_code", code,
	)
	return nil
}
