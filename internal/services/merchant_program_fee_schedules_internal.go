// Package services contains business orchestration for merchant program
// fee-schedule readiness and effective commercial-policy resolution.
//
// sdworkspace/sdbackend/internal/services/merchant_program_fee_schedules_internal.go
//
// GTM:
//
//	Layer: 2.4 Merchant / Future Offering Domain
//	Release Class: SPINE
//	Reason:
//	  merchant_program_fee_schedules service behavior is release-critical
//	  monetization infrastructure for merchant setup fees, subscription fees,
//	  Future Offering and Launch Intelligence fees, Campaign Performance Fees,
//	  adjustments, refunds, reversals, plan-specific pricing, and global
//	  fallback pricing.
//
//	  This file provides synchronous commercial-policy readiness validation
//	  and stable plan-code resolution for downstream merchant billing
//	  consumers. It does not calculate fees, invent pricing, process payments,
//	  or govern HTTP authorization.
//
// SPINE Rule:
//
//	Keep compiling.
//	Keep production-ready.
//	Preserve synchronous monetization-readiness validation.
//	Preserve deterministic plan-specific-over-global resolution.
//	Preserve exact decimal values as returned by the data layer.
//	Never invent, hard-code, or silently default commercial price terms.
//	Never convert a missing schedule into a zero fee.
//	Never suppress ambiguous effective-policy resolution.
//	Do not duplicate fee-schedule SQL, persistence validation, lifecycle
//	mutation, HTTP authorization, handler auditing, or fee calculation.
//	Block deployment if this file breaks merchant monetization readiness,
//	active-plan resolution, effective commercial-policy resolution, or
//	monetary precision.
package services

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/PiccoloMondoC/sdworkspace/sdbackend/internal/data"

	"github.com/google/uuid"
)

// MerchantProgramFeeScheduleRequirement identifies one commercial-policy
// category that must resolve before a dependent monetization workflow is
// considered ready.
//
// A nil PlanCode requires a global schedule.
//
// A non-nil PlanCode requires the named active merchant program plan and
// resolves its applicable schedule using the data layer's canonical
// plan-specific-over-global precedence.
//
// Requirements identify policy coverage only. They never contain monetary
// amounts, percentages, or other price terms.
type MerchantProgramFeeScheduleRequirement struct {
	FeeType         data.MerchantFeeType         `json:"fee_type"`
	BillingInterval data.MerchantBillingInterval `json:"billing_interval"`
	PlanCode        *data.MerchantPlanCode       `json:"plan_code,omitempty"`
}

// MerchantProgramFeeScheduleRequirementResult reports the effective schedule
// selected for one validated readiness requirement.
type MerchantProgramFeeScheduleRequirementResult struct {
	Requirement   MerchantProgramFeeScheduleRequirement `json:"requirement"`
	ResolvedScope data.MerchantFeeScope                 `json:"resolved_scope,omitempty"`
	PlanID        *uuid.UUID                            `json:"plan_id,omitempty"`
	FeeScheduleID uuid.UUID                             `json:"fee_schedule_id"`
}

// MerchantProgramFeeScheduleReadiness reports effective commercial-policy
// coverage for a non-empty caller-supplied requirement set.
type MerchantProgramFeeScheduleReadiness struct {
	AsOf         time.Time                                     `json:"as_of"`
	Requirements []MerchantProgramFeeScheduleRequirementResult `json:"requirements"`
}

// IsReady reports whether a non-empty requirement set was fully resolved.
func (r MerchantProgramFeeScheduleReadiness) IsReady() bool {
	if len(r.Requirements) == 0 {
		return false
	}

	for _, result := range r.Requirements {
		if result.FeeScheduleID == uuid.Nil {
			return false
		}
	}

	return true
}

func validateMerchantProgramFeeScheduleService(s *Service) error {
	if s == nil {
		return errors.New(
			"merchant program fee schedule service is required",
		)
	}
	if s.Logger == nil {
		return errors.New(
			"merchant program fee schedule service logger is required",
		)
	}
	if s.Models == nil {
		return errors.New(
			"merchant program fee schedule service models are required",
		)
	}
	if s.Cfg == nil {
		return errors.New(
			"merchant program fee schedule service configuration is required",
		)
	}
	if s.Cfg.DBTimeout <= 0 {
		return errors.New(
			"merchant program fee schedule service database timeout must be positive",
		)
	}

	return nil
}

func normalizeMerchantProgramFeeScheduleRequirement(
	requirement MerchantProgramFeeScheduleRequirement,
) (MerchantProgramFeeScheduleRequirement, error) {
	requirement.FeeType =
		data.NormalizeMerchantFeeType(requirement.FeeType)

	if !data.IsValidMerchantFeeType(requirement.FeeType) {
		return MerchantProgramFeeScheduleRequirement{}, fmt.Errorf(
			"invalid merchant fee type: %s",
			requirement.FeeType,
		)
	}

	requirement.BillingInterval =
		data.NormalizeMerchantBillingInterval(
			requirement.BillingInterval,
		)

	if !data.IsValidMerchantBillingInterval(
		requirement.BillingInterval,
	) {
		return MerchantProgramFeeScheduleRequirement{}, fmt.Errorf(
			"invalid merchant billing interval: %s",
			requirement.BillingInterval,
		)
	}

	if !data.IsMerchantFeeTypeIntervalCompatible(
		requirement.FeeType,
		requirement.BillingInterval,
	) {
		return MerchantProgramFeeScheduleRequirement{}, fmt.Errorf(
			"merchant fee type %s is incompatible with billing interval %s",
			requirement.FeeType,
			requirement.BillingInterval,
		)
	}

	if requirement.PlanCode != nil {
		code := data.NormalizeMerchantPlanCode(
			*requirement.PlanCode,
		)
		if !data.IsValidMerchantPlanCode(code) {
			return MerchantProgramFeeScheduleRequirement{}, fmt.Errorf(
				"invalid merchant program plan code: %s",
				code,
			)
		}

		requirement.PlanCode = &code
	}

	return requirement, nil
}

func merchantProgramFeeScheduleRequirementKey(
	requirement MerchantProgramFeeScheduleRequirement,
) string {
	planCode := "<global>"
	if requirement.PlanCode != nil {
		planCode = string(*requirement.PlanCode)
	}

	return fmt.Sprintf(
		"%s:%s:%s",
		planCode,
		requirement.FeeType,
		requirement.BillingInterval,
	)
}

func normalizeMerchantProgramFeeScheduleRequirements(
	requirements []MerchantProgramFeeScheduleRequirement,
) ([]MerchantProgramFeeScheduleRequirement, error) {
	if len(requirements) == 0 {
		return nil, errors.New(
			"at least one merchant program fee schedule requirement is required",
		)
	}

	normalized := make(
		[]MerchantProgramFeeScheduleRequirement,
		0,
		len(requirements),
	)

	seen := make(map[string]struct{}, len(requirements))

	for index, raw := range requirements {
		requirement, err :=
			normalizeMerchantProgramFeeScheduleRequirement(raw)
		if err != nil {
			return nil, fmt.Errorf(
				"invalid merchant program fee schedule requirement at index %d: %w",
				index,
				err,
			)
		}

		key := merchantProgramFeeScheduleRequirementKey(
			requirement,
		)
		if _, exists := seen[key]; exists {
			return nil, fmt.Errorf(
				"duplicate merchant program fee schedule requirement: %s",
				key,
			)
		}

		seen[key] = struct{}{}
		normalized = append(normalized, requirement)
	}

	return normalized, nil
}

func (s *Service) resolveMerchantProgramFeeSchedulePlanID(
	ctx context.Context,
	planCode *data.MerchantPlanCode,
) (*uuid.UUID, error) {
	if planCode == nil {
		return nil, nil
	}

	plan, err := s.ResolveActiveMerchantProgramPlanInternal(
		ctx,
		*planCode,
	)
	if err != nil {
		return nil, fmt.Errorf(
			"resolve active merchant program plan %s: %w",
			*planCode,
			err,
		)
	}

	if plan == nil || plan.ID == uuid.Nil {
		return nil, fmt.Errorf(
			"active merchant program plan not found: %s",
			*planCode,
		)
	}

	planID := plan.ID
	return &planID, nil
}

func (s *Service) resolveEffectiveMerchantProgramFeeSchedule(
	ctx context.Context,
	feeType data.MerchantFeeType,
	billingInterval data.MerchantBillingInterval,
	planID *uuid.UUID,
	asOf time.Time,
) (*data.MerchantProgramFeeSchedule, error) {
	dbCtx, cancel := context.WithTimeout(
		ctx,
		s.Cfg.DBTimeout,
	)
	defer cancel()

	schedule, err :=
		s.Models.
			MerchantProgramFeeSchedule.
			ResolveEffective(
				dbCtx,
				feeType,
				billingInterval,
				planID,
				asOf,
			)
	if err != nil {
		return nil, err
	}

	return schedule, nil
}

// ResolveEffectiveMerchantProgramFeeScheduleInternal resolves the effective
// merchant commercial policy for one fee type, billing interval, and optional
// stable merchant program plan code.
//
// When planCode is supplied, the code must identify an active canonical plan.
// The data layer then applies plan-specific-over-global precedence.
//
// When planCode is nil, resolution is global-only.
//
// A missing schedule is returned as a clear error. It is never converted into
// a zero fee or another synthetic commercial term.
func (s *Service) ResolveEffectiveMerchantProgramFeeScheduleInternal(
	ctx context.Context,
	feeType data.MerchantFeeType,
	billingInterval data.MerchantBillingInterval,
	planCode *data.MerchantPlanCode,
	asOf time.Time,
) (*data.MerchantProgramFeeSchedule, error) {
	if err := validateMerchantProgramFeeScheduleService(s); err != nil {
		return nil, err
	}

	logger := s.Logger.
		GetLoggerWithContextFromContext(ctx).
		WithFunctionName(
			"ResolveEffectiveMerchantProgramFeeScheduleInternal",
		)

	requirement, err :=
		normalizeMerchantProgramFeeScheduleRequirement(
			MerchantProgramFeeScheduleRequirement{
				FeeType:         feeType,
				BillingInterval: billingInterval,
				PlanCode:        planCode,
			},
		)
	if err != nil {
		return nil, err
	}

	planID, err :=
		s.resolveMerchantProgramFeeSchedulePlanID(
			ctx,
			requirement.PlanCode,
		)
	if err != nil {
		logger.Error(
			"Merchant program plan resolution failed",
			"plan_code",
			requirement.PlanCode,
			"error",
			err,
		)
		return nil, err
	}

	schedule, err :=
		s.resolveEffectiveMerchantProgramFeeSchedule(
			ctx,
			requirement.FeeType,
			requirement.BillingInterval,
			planID,
			asOf,
		)
	if err != nil {
		logger.Error(
			"Effective merchant program fee schedule resolution failed",
			"fee_type",
			requirement.FeeType,
			"billing_interval",
			requirement.BillingInterval,
			"plan_id",
			planID,
			"error",
			err,
		)

		return nil, fmt.Errorf(
			"resolve effective merchant program fee schedule: %w",
			err,
		)
	}

	if schedule == nil {
		return nil, fmt.Errorf(
			"no effective merchant program fee schedule found for fee type %s and billing interval %s",
			requirement.FeeType,
			requirement.BillingInterval,
		)
	}

	return schedule, nil
}

// ValidateMerchantProgramFeeScheduleReadinessInternal validates a non-empty,
// explicit set of commercial-policy requirements at one consistent UTC
// reference time.
//
// Missing policy, inactive plans, ambiguous policy, cancellation, timeout,
// and database failures all prevent successful readiness validation. The
// method does not weaken integrity failures into a partially successful
// readiness report.
//
// This method performs no commercial-policy mutation.
func (s *Service) ValidateMerchantProgramFeeScheduleReadinessInternal(
	ctx context.Context,
	requirements []MerchantProgramFeeScheduleRequirement,
) (MerchantProgramFeeScheduleReadiness, error) {
	if err := validateMerchantProgramFeeScheduleService(s); err != nil {
		return MerchantProgramFeeScheduleReadiness{}, err
	}

	logger := s.Logger.
		GetLoggerWithContextFromContext(ctx).
		WithFunctionName(
			"ValidateMerchantProgramFeeScheduleReadinessInternal",
		)

	normalized, err :=
		normalizeMerchantProgramFeeScheduleRequirements(
			requirements,
		)
	if err != nil {
		return MerchantProgramFeeScheduleReadiness{}, err
	}

	asOf := time.Now().UTC()

	readiness := MerchantProgramFeeScheduleReadiness{
		AsOf: asOf,
		Requirements: make(
			[]MerchantProgramFeeScheduleRequirementResult,
			0,
			len(normalized),
		),
	}

	for _, requirement := range normalized {
		if err := ctx.Err(); err != nil {
			return MerchantProgramFeeScheduleReadiness{}, err
		}

		planID, err :=
			s.resolveMerchantProgramFeeSchedulePlanID(
				ctx,
				requirement.PlanCode,
			)
		if err != nil {
			logger.Error(
				"Merchant program fee schedule readiness plan resolution failed",
				"plan_code",
				requirement.PlanCode,
				"error",
				err,
			)

			return MerchantProgramFeeScheduleReadiness{}, err
		}

		schedule, err :=
			s.resolveEffectiveMerchantProgramFeeSchedule(
				ctx,
				requirement.FeeType,
				requirement.BillingInterval,
				planID,
				asOf,
			)
		if err != nil {
			logger.Error(
				"Merchant program fee schedule readiness resolution failed",
				"fee_type",
				requirement.FeeType,
				"billing_interval",
				requirement.BillingInterval,
				"plan_id",
				planID,
				"error",
				err,
			)

			return MerchantProgramFeeScheduleReadiness{}, fmt.Errorf(
				"resolve required merchant program fee schedule for fee type %s and billing interval %s: %w",
				requirement.FeeType,
				requirement.BillingInterval,
				err,
			)
		}

		if schedule == nil {
			return MerchantProgramFeeScheduleReadiness{}, fmt.Errorf(
				"required merchant program fee schedule not found for fee type %s and billing interval %s",
				requirement.FeeType,
				requirement.BillingInterval,
			)
		}

		feeScheduleID := schedule.ID

		result := MerchantProgramFeeScheduleRequirementResult{
			Requirement:   requirement,
			ResolvedScope: schedule.FeeScope,
			PlanID:        planID,
			FeeScheduleID: feeScheduleID,
		}

		readiness.Requirements = append(
			readiness.Requirements,
			result,
		)
	}

	logger.Info(
		"Merchant program fee schedule readiness validated",
		"ready",
		readiness.IsReady(),
		"requirement_count",
		len(readiness.Requirements),
	)

	return readiness, nil
}
