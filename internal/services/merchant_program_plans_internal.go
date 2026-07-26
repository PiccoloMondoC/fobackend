// Package services contains business logic for internal operations such as
// system bootstrap, plan integrity enforcement, and entitlement resolution.
//
// sdworkspace/sdbackend/internal/services/merchant_program_plans_internal.go
//
// GTM:
//
//	Layer: 2.4 Merchant / Future Offering Domain
//	Release Class: SPINE
//	Reason:
//	  merchant_program_plans service layer is release-critical bootstrap,
//	  readiness, and active-plan resolution infrastructure for Future Offering
//	  access, merchant subscriptions, entitlement assignment, fee schedules,
//	  billing accounts, and Merchant Center plan selection.
//
// SPINE Rule:
//
//	Keep compiling.
//	Keep production-ready.
//	Preserve idempotent bootstrap correctness.
//	Preserve plan-code integrity and immutability.
//	Preserve active-plan resolution for entitlement and billing consumers.
//	Block deployment if this file breaks plan bootstrap, readiness validation,
//	Future Offering entitlement resolution, or merchant billing plan integrity.
package services

import (
	"context"
	"errors"
	"fmt"

	"github.com/PiccoloMondoC/sdworkspace/sdbackend/internal/data"
)

var canonicalMerchantProgramPlanDefaults = []data.MerchantProgramPlan{
	{
		Code:        data.PlanCodeStandard,
		Name:        "Standard",
		Description: "Standard merchant program plan with core merchant access.",
	},
	{
		Code:        data.PlanCodePremium,
		Name:        "Premium",
		Description: "Premium merchant program plan with enhanced Future Offering and Launch Campaign access.",
	},
	{
		Code:        data.PlanCodeEnterprise,
		Name:        "Enterprise",
		Description: "Enterprise merchant program plan with full Future Offering, Launch Campaign, and priority support access.",
	},
}

// MerchantProgramPlanReadiness reports merchant program plan readiness.
type MerchantProgramPlanReadiness struct {
	AllCodesPresent bool                    `json:"all_codes_present"`
	AllActive       bool                    `json:"all_active"`
	NoSoftDeleted   bool                    `json:"no_soft_deleted"`
	MissingCodes    []data.MerchantPlanCode `json:"missing_codes,omitempty"`
	InactiveCodes   []data.MerchantPlanCode `json:"inactive_codes,omitempty"`
	DeletedCodes    []data.MerchantPlanCode `json:"deleted_codes,omitempty"`
}

// IsReady reports whether merchant program plans are ready for Future Offering workflows.
func (r MerchantProgramPlanReadiness) IsReady() bool {
	return r.AllCodesPresent && r.AllActive && r.NoSoftDeleted
}

func validateMerchantProgramPlanService(s *Service) error {
	if s == nil {
		return errors.New("merchant program plan service is required")
	}
	if s.Logger == nil {
		return errors.New("merchant program plan service logger is required")
	}
	return nil
}

func merchantProgramPlanRequiredCodes() []data.MerchantPlanCode {
	return []data.MerchantPlanCode{
		data.PlanCodeStandard,
		data.PlanCodePremium,
		data.PlanCodeEnterprise,
	}
}

func (s *Service) findMerchantProgramPlanByCodeIncludingDeleted(ctx context.Context, code data.MerchantPlanCode) (*data.MerchantProgramPlan, error) {
	code = data.NormalizeMerchantPlanCode(code)
	if !data.IsValidMerchantPlanCode(code) {
		return nil, fmt.Errorf("invalid merchant program plan code: %s", code)
	}

	dbCtx, cancel := context.WithTimeout(ctx, s.Cfg.DBTimeout)
	defer cancel()

	// limit 100: safe ceiling; table is bounded to exactly 3 rows by schema CHECK constraint.
	plans, err := s.Models.MerchantProgramPlan.GetAll(dbCtx, true, 100, 0)
	if err != nil {
		return nil, err
	}

	for _, plan := range plans {
		if plan != nil && data.NormalizeMerchantPlanCode(plan.Code) == code {
			return plan, nil
		}
	}

	return nil, nil
}

// EnsureDefaultMerchantProgramPlansInternal ensures required merchant program plans exist and are active.
func (s *Service) EnsureDefaultMerchantProgramPlansInternal(ctx context.Context) error {
	if err := validateMerchantProgramPlanService(s); err != nil {
		return err
	}

	logger := s.Logger.GetLoggerWithContextFromContext(ctx).
		WithFunctionName("EnsureDefaultMerchantProgramPlansInternal")

	for _, def := range canonicalMerchantProgramPlanDefaults {
		existing, err := s.findMerchantProgramPlanByCodeIncludingDeleted(ctx, def.Code)
		if err != nil {
			logger.Error("Failed to inspect merchant program plan", "code", def.Code, "error", err)
			return fmt.Errorf("inspect merchant program plan %s: %w", def.Code, err)
		}

		if existing == nil {
			plan := data.MerchantProgramPlan{
				Code:        def.Code,
				Name:        def.Name,
				Description: def.Description,
			}

			dbCtx, cancel := context.WithTimeout(ctx, s.Cfg.DBTimeout)
			err := s.Models.MerchantProgramPlan.Insert(dbCtx, &plan)
			cancel()

			if err != nil {
				logger.Error("Failed to insert default merchant program plan", "code", def.Code, "error", err)
				return fmt.Errorf("insert default merchant program plan %s: %w", def.Code, err)
			}

			logger.Info("Default merchant program plan inserted", "code", plan.Code, "plan_id", plan.ID)
			continue
		}

		// Existing plans are not updated to match canonical defaults.
		// Operators may customize name and description; bootstrap corrects only lifecycle state.
		if existing.DeletedAt != nil {
			dbCtx, cancel := context.WithTimeout(ctx, s.Cfg.DBTimeout)
			err := s.Models.MerchantProgramPlan.Restore(dbCtx, existing.ID)
			cancel()

			if err != nil {
				logger.Error("Failed to restore default merchant program plan", "code", existing.Code, "plan_id", existing.ID, "error", err)
				return fmt.Errorf("restore default merchant program plan %s: %w", existing.Code, err)
			}
		}

		// Restore preserves the pre-deletion is_active state.
		// Required plan codes must always be active, so inactive required plans are explicitly activated.
		if !existing.IsActive {
			dbCtx, cancel := context.WithTimeout(ctx, s.Cfg.DBTimeout)
			err := s.Models.MerchantProgramPlan.Activate(dbCtx, existing.ID)
			cancel()

			if err != nil {
				logger.Error("Failed to activate default merchant program plan", "code", existing.Code, "plan_id", existing.ID, "error", err)
				return fmt.Errorf("activate default merchant program plan %s: %w", existing.Code, err)
			}
		}

		logger.Info("Default merchant program plan ensured", "code", existing.Code, "plan_id", existing.ID)
	}

	return nil
}

// ValidateMerchantProgramPlanReadinessInternal checks required plan presence and active readiness.
func (s *Service) ValidateMerchantProgramPlanReadinessInternal(ctx context.Context) (MerchantProgramPlanReadiness, error) {
	if err := validateMerchantProgramPlanService(s); err != nil {
		return MerchantProgramPlanReadiness{}, err
	}

	logger := s.Logger.GetLoggerWithContextFromContext(ctx).
		WithFunctionName("ValidateMerchantProgramPlanReadinessInternal")

	result := MerchantProgramPlanReadiness{
		AllCodesPresent: true,
		AllActive:       true,
		NoSoftDeleted:   true,
	}

	for _, code := range merchantProgramPlanRequiredCodes() {
		plan, err := s.findMerchantProgramPlanByCodeIncludingDeleted(ctx, code)
		if err != nil {
			logger.Error("Merchant program plan readiness check failed", "code", code, "error", err)
			return MerchantProgramPlanReadiness{}, fmt.Errorf("check merchant program plan readiness for %s: %w", code, err)
		}

		if plan == nil {
			result.AllCodesPresent = false
			result.AllActive = false
			result.NoSoftDeleted = false
			result.MissingCodes = append(result.MissingCodes, code)
			continue
		}

		if plan.DeletedAt != nil {
			result.NoSoftDeleted = false
			result.AllActive = false
			result.DeletedCodes = append(result.DeletedCodes, code)
			continue
		}

		if !plan.IsActive {
			result.AllActive = false
			result.InactiveCodes = append(result.InactiveCodes, code)
		}
	}

	logger.Info("Merchant program plan readiness checked", "ready", result.IsReady())
	return result, nil
}

// ResolveActiveMerchantProgramPlanInternal resolves a stable plan code to an active plan.
func (s *Service) ResolveActiveMerchantProgramPlanInternal(ctx context.Context, code data.MerchantPlanCode) (*data.MerchantProgramPlan, error) {
	if err := validateMerchantProgramPlanService(s); err != nil {
		return nil, err
	}

	logger := s.Logger.GetLoggerWithContextFromContext(ctx).
		WithFunctionName("ResolveActiveMerchantProgramPlanInternal")

	code = data.NormalizeMerchantPlanCode(code)
	if !data.IsValidMerchantPlanCode(code) {
		return nil, fmt.Errorf("invalid merchant program plan code: %s", code)
	}

	dbCtx, cancel := context.WithTimeout(ctx, s.Cfg.DBTimeout)
	defer cancel()

	plan, err := s.Models.MerchantProgramPlan.GetActiveByCode(dbCtx, code)
	if err != nil {
		logger.Error("Failed to resolve active merchant program plan", "code", code, "error", err)
		return nil, fmt.Errorf("resolve active merchant program plan %s: %w", code, err)
	}
	if plan == nil {
		return nil, fmt.Errorf("active merchant program plan not found: %s", code)
	}

	return plan, nil
}
