// Package services contains business logic for internal operations such as
// system bootstrap, entitlement readiness, and capability resolution.
//
// sdworkspace/sdbackend/internal/services/merchant_program_entitlements_internal.go
//
// GTM:
//
//	Layer: 2.4 Merchant / Future Offering Domain
//	Release Class: SPINE
//	Reason:
//	  merchant_program_entitlements service layer is release-critical
//	  capability-gate infrastructure for Future Offering v1. It provides
//	  internal orchestration for default entitlement bootstrap, plan capability
//	  checks, and reusable service-layer guards for Launch Campaign and Future
//	  Offering workflows.
//
//	  Future Offering is Platform's core merchant-side future-commerce object.
//	  A merchant program plan must not be treated as capable of activating
//	  Future Offering workflows unless the plan carries the appropriate
//	  entitlement.
//
//	  The database owns the compound invariant enforced by
//	  ensure_future_offering_includes_campaign_trigger: inserting
//	  future_offering_access automatically ensures launch_campaign_access for
//	  the same plan. Service code must not shadow that invariant with duplicate
//	  companion inserts.
//
// SPINE Rule:
//
//	Keep compiling.
//	Keep production-ready.
//	Preserve idempotent default entitlement bootstrap.
//	Preserve entitlement-code integrity.
//	Preserve DB-owned Future Offering includes Launch Campaign invariant.
//	Preserve reusable service-layer capability gates.
//	Block deployment if this file breaks entitlement bootstrap, Future Offering
//	capability checks, Launch Campaign capability checks, or startup readiness.
package services

import (
	"context"
	"errors"
	"fmt"

	"github.com/PiccoloMondoC/focodebase/fobackend/internal/data"

	"github.com/google/uuid"
)

var canonicalMerchantProgramEntitlementDefaults = []struct {
	PlanCode        data.MerchantPlanCode
	EntitlementCode data.MerchantProgramEntitlementCode
}{
	{PlanCode: data.PlanCodeStandard, EntitlementCode: data.EntitlementCodeLaunchCampaignAccess},
	{PlanCode: data.PlanCodePremium, EntitlementCode: data.EntitlementCodeFutureOfferingAccess},
	{PlanCode: data.PlanCodeEnterprise, EntitlementCode: data.EntitlementCodeFutureOfferingAccess},
}

// MerchantProgramEntitlementReadiness reports entitlement readiness for the
// canonical merchant program plans.
type MerchantProgramEntitlementReadiness struct {
	AllDefaultsPresent bool                                       `json:"all_defaults_present"`
	MissingDefaults    []MerchantProgramEntitlementMissingDefault `json:"missing_defaults,omitempty"`
}

// MerchantProgramEntitlementMissingDefault describes a missing default
// entitlement assignment.
type MerchantProgramEntitlementMissingDefault struct {
	PlanCode        data.MerchantPlanCode               `json:"plan_code"`
	EntitlementCode data.MerchantProgramEntitlementCode `json:"entitlement_code"`
}

// IsReady reports whether canonical merchant program entitlements are ready.
func (r MerchantProgramEntitlementReadiness) IsReady() bool {
	return r.AllDefaultsPresent
}

func validateMerchantProgramEntitlementService(s *Service) error {
	if s == nil {
		return errors.New("merchant program entitlement service is required")
	}
	if s.Logger == nil {
		return errors.New("merchant program entitlement service logger is required")
	}
	if s.Models == nil {
		return errors.New("merchant program entitlement service models are required")
	}
	if s.Cfg == nil {
		return errors.New("merchant program entitlement service config is required")
	}
	if s.Cfg.DBTimeout <= 0 {
		return errors.New("merchant program entitlement service DB timeout must be greater than zero")
	}
	return nil
}

func normalizeAndValidateMerchantProgramEntitlementCode(code data.MerchantProgramEntitlementCode) (data.MerchantProgramEntitlementCode, error) {
	code = data.NormalizeMerchantProgramEntitlementCode(code)
	if !data.IsValidMerchantProgramEntitlementCode(code) {
		return "", fmt.Errorf("invalid merchant program entitlement code: %s", code)
	}
	return code, nil
}

// EnsureMerchantProgramEntitlementInternal ensures an entitlement exists for a
// merchant program plan and returns the persisted entitlement.
func (s *Service) EnsureMerchantProgramEntitlementInternal(
	ctx context.Context,
	planID uuid.UUID,
	code data.MerchantProgramEntitlementCode,
) (*data.MerchantProgramEntitlement, error) {
	if err := validateMerchantProgramEntitlementService(s); err != nil {
		return nil, err
	}

	logger := s.Logger.GetLoggerWithContextFromContext(ctx).
		WithFunctionName("EnsureMerchantProgramEntitlementInternal")

	if planID == uuid.Nil {
		return nil, errors.New("merchant program entitlement plan ID is required")
	}

	code, err := normalizeAndValidateMerchantProgramEntitlementCode(code)
	if err != nil {
		return nil, err
	}

	dbCtx, cancel := context.WithTimeout(ctx, s.Cfg.DBTimeout)
	defer cancel()

	entitlement, err := s.Models.MerchantProgramEntitlement.Ensure(dbCtx, planID, code)
	if err != nil {
		logger.Error("Ensure merchant program entitlement failed",
			"plan_id", planID,
			"entitlement_code", code,
			"error", err,
		)
		return nil, fmt.Errorf("ensure merchant program entitlement for plan %s and code %s: %w", planID, code, err)
	}

	logger.Info("Merchant program entitlement ensured",
		"entitlement_id", entitlement.ID,
		"plan_id", entitlement.PlanID,
		"entitlement_code", entitlement.EntitlementCode,
	)

	return entitlement, nil
}

// EnsureDefaultMerchantProgramEntitlementsInternal ensures the canonical
// default entitlement set for standard, premium, and enterprise plans.
//
// The canonical mapping is:
//   - standard   -> launch_campaign_access
//   - premium    -> future_offering_access
//   - enterprise -> future_offering_access
//
// For premium and enterprise, the database trigger automatically ensures
// launch_campaign_access. This service intentionally does not insert that
// companion entitlement manually.
func (s *Service) EnsureDefaultMerchantProgramEntitlementsInternal(ctx context.Context) error {
	if err := validateMerchantProgramEntitlementService(s); err != nil {
		return err
	}

	logger := s.Logger.GetLoggerWithContextFromContext(ctx).
		WithFunctionName("EnsureDefaultMerchantProgramEntitlementsInternal")

	for _, def := range canonicalMerchantProgramEntitlementDefaults {
		plan, err := s.ResolveActiveMerchantProgramPlanInternal(ctx, def.PlanCode)
		if err != nil {
			logger.Error("Failed to resolve canonical active merchant program plan",
				"plan_code", def.PlanCode,
				"error", err,
			)
			return fmt.Errorf("resolve canonical active merchant program plan %s: %w", def.PlanCode, err)
		}
		if plan == nil {
			return fmt.Errorf("canonical active merchant program plan not found: %s", def.PlanCode)
		}

		entitlement, err := s.EnsureMerchantProgramEntitlementInternal(ctx, plan.ID, def.EntitlementCode)
		if err != nil {
			logger.Error("Failed to ensure default merchant program entitlement",
				"plan_code", def.PlanCode,
				"plan_id", plan.ID,
				"entitlement_code", def.EntitlementCode,
				"error", err,
			)
			return fmt.Errorf("ensure default merchant program entitlement for %s/%s: %w", def.PlanCode, def.EntitlementCode, err)
		}

		logger.Info("Default merchant program entitlement ensured",
			"plan_code", def.PlanCode,
			"plan_id", plan.ID,
			"entitlement_id", entitlement.ID,
			"entitlement_code", entitlement.EntitlementCode,
		)
	}

	return nil
}

// ValidateMerchantProgramEntitlementReadinessInternal checks whether canonical
// active merchant program plans have their required entitlements.
func (s *Service) ValidateMerchantProgramEntitlementReadinessInternal(ctx context.Context) (MerchantProgramEntitlementReadiness, error) {
	if err := validateMerchantProgramEntitlementService(s); err != nil {
		return MerchantProgramEntitlementReadiness{}, err
	}

	logger := s.Logger.GetLoggerWithContextFromContext(ctx).
		WithFunctionName("ValidateMerchantProgramEntitlementReadinessInternal")

	result := MerchantProgramEntitlementReadiness{
		AllDefaultsPresent: true,
	}

	for _, def := range canonicalMerchantProgramEntitlementDefaults {
		plan, err := s.ResolveActiveMerchantProgramPlanInternal(ctx, def.PlanCode)
		if err != nil {
			logger.Error("Failed to resolve canonical active merchant program plan during entitlement readiness",
				"plan_code", def.PlanCode,
				"error", err,
			)
			return MerchantProgramEntitlementReadiness{}, fmt.Errorf("resolve canonical active merchant program plan %s: %w", def.PlanCode, err)
		}
		if plan == nil {
			result.AllDefaultsPresent = false
			result.MissingDefaults = append(result.MissingDefaults, MerchantProgramEntitlementMissingDefault{
				PlanCode:        def.PlanCode,
				EntitlementCode: def.EntitlementCode,
			})
			continue
		}

		has, err := s.PlanHasMerchantProgramEntitlementInternal(ctx, plan.ID, def.EntitlementCode)
		if err != nil {
			logger.Error("Failed to check canonical merchant program entitlement readiness",
				"plan_code", def.PlanCode,
				"plan_id", plan.ID,
				"entitlement_code", def.EntitlementCode,
				"error", err,
			)
			return MerchantProgramEntitlementReadiness{}, fmt.Errorf("check entitlement readiness for %s/%s: %w", def.PlanCode, def.EntitlementCode, err)
		}

		if !has {
			result.AllDefaultsPresent = false
			result.MissingDefaults = append(result.MissingDefaults, MerchantProgramEntitlementMissingDefault{
				PlanCode:        def.PlanCode,
				EntitlementCode: def.EntitlementCode,
			})
		}
	}

	logger.Info("Merchant program entitlement readiness checked", "ready", result.IsReady())
	return result, nil
}

// PlanHasMerchantProgramEntitlementInternal checks whether a plan has a
// specific merchant program entitlement.
func (s *Service) PlanHasMerchantProgramEntitlementInternal(
	ctx context.Context,
	planID uuid.UUID,
	code data.MerchantProgramEntitlementCode,
) (bool, error) {
	if err := validateMerchantProgramEntitlementService(s); err != nil {
		return false, err
	}

	logger := s.Logger.GetLoggerWithContextFromContext(ctx).
		WithFunctionName("PlanHasMerchantProgramEntitlementInternal")

	if planID == uuid.Nil {
		return false, errors.New("merchant program entitlement plan ID is required")
	}

	code, err := normalizeAndValidateMerchantProgramEntitlementCode(code)
	if err != nil {
		return false, err
	}

	dbCtx, cancel := context.WithTimeout(ctx, s.Cfg.DBTimeout)
	defer cancel()

	has, err := s.Models.MerchantProgramEntitlement.PlanHasEntitlement(dbCtx, planID, code)
	if err != nil {
		logger.Error("Merchant program entitlement check failed",
			"plan_id", planID,
			"entitlement_code", code,
			"error", err,
		)
		return false, fmt.Errorf("check merchant program entitlement for plan %s and code %s: %w", planID, code, err)
	}

	return has, nil
}

// PlanHasLaunchCampaignAccessInternal checks whether a plan has Launch Campaign
// access.
func (s *Service) PlanHasLaunchCampaignAccessInternal(ctx context.Context, planID uuid.UUID) (bool, error) {
	return s.PlanHasMerchantProgramEntitlementInternal(ctx, planID, data.EntitlementCodeLaunchCampaignAccess)
}

// PlanHasFutureOfferingAccessInternal checks whether a plan has Future Offering
// access.
func (s *Service) PlanHasFutureOfferingAccessInternal(ctx context.Context, planID uuid.UUID) (bool, error) {
	return s.PlanHasMerchantProgramEntitlementInternal(ctx, planID, data.EntitlementCodeFutureOfferingAccess)
}

// RequireMerchantProgramEntitlementInternal requires a plan to have a specific
// merchant program entitlement.
func (s *Service) RequireMerchantProgramEntitlementInternal(
	ctx context.Context,
	planID uuid.UUID,
	code data.MerchantProgramEntitlementCode,
) error {
	code, err := normalizeAndValidateMerchantProgramEntitlementCode(code)
	if err != nil {
		return err
	}

	has, err := s.PlanHasMerchantProgramEntitlementInternal(ctx, planID, code)
	if err != nil {
		return err
	}
	if !has {
		return fmt.Errorf("merchant program plan %s lacks required entitlement: %s", planID, code)
	}

	return nil
}

// RequireLaunchCampaignAccessInternal requires a plan to have Launch Campaign
// access.
func (s *Service) RequireLaunchCampaignAccessInternal(ctx context.Context, planID uuid.UUID) error {
	return s.RequireMerchantProgramEntitlementInternal(ctx, planID, data.EntitlementCodeLaunchCampaignAccess)
}

// RequireFutureOfferingAccessInternal requires a plan to have Future Offering
// access.
func (s *Service) RequireFutureOfferingAccessInternal(ctx context.Context, planID uuid.UUID) error {
	return s.RequireMerchantProgramEntitlementInternal(ctx, planID, data.EntitlementCodeFutureOfferingAccess)
}

// ListMerchantProgramEntitlementsForPlanInternal lists all entitlements for a
// merchant program plan.
func (s *Service) ListMerchantProgramEntitlementsForPlanInternal(ctx context.Context, planID uuid.UUID) ([]*data.MerchantProgramEntitlement, error) {
	if err := validateMerchantProgramEntitlementService(s); err != nil {
		return nil, err
	}

	logger := s.Logger.GetLoggerWithContextFromContext(ctx).
		WithFunctionName("ListMerchantProgramEntitlementsForPlanInternal")

	if planID == uuid.Nil {
		return nil, errors.New("merchant program entitlement plan ID is required")
	}

	dbCtx, cancel := context.WithTimeout(ctx, s.Cfg.DBTimeout)
	defer cancel()

	entitlements, err := s.Models.MerchantProgramEntitlement.ListByPlanID(dbCtx, planID)
	if err != nil {
		logger.Error("List merchant program entitlements for plan failed",
			"plan_id", planID,
			"error", err,
		)
		return nil, fmt.Errorf("list merchant program entitlements for plan %s: %w", planID, err)
	}

	return entitlements, nil
}
