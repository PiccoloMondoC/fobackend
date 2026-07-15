// Package services contains internal merchant program subscription readiness
// and resolution logic.
//
// sdworkspace/sdbackend/internal/services/merchant_program_subscriptions_internal.go
//
// GTM:
//
//	Layer: 2.4 Merchant / Future Offering Domain
//	Release Class: SPINE
//	Reason:
//	  merchant_program_subscriptions service logic is release-critical merchant
//	  monetization infrastructure. It resolves merchant subscription readiness,
//	  current subscription state, and active subscription state for Future
//	  Offering access, entitlement eligibility, Merchant Center plan state, and
//	  later billing-plan resolution.
//
//	  Subscriptions are merchant-specific lifecycle records. They are not seed
//	  data and must not be globally bootstrapped.
//
//	  This file does not implement checkout, invoice handling, payment
//	  processing, settlement, fee calculation, merchant-of-record behavior, or
//	  consumer Future Commerce engagement.
//
// SPINE Rule:
//
//	Keep compiling.
//	Keep production-ready.
//	Preserve current/active subscription distinction.
//	Preserve Future Offering access correctness.
//	Preserve merchant-scoped readiness semantics.
//	Block deployment if this file breaks subscription readiness, active access
//	resolution, entitlement readiness, or Merchant Center subscription state.
package services

import (
	"context"
	"errors"
	"fmt"

	"github.com/PiccoloMondoC/sdworkspace/sdbackend/internal/data"
	"github.com/google/uuid"
)

type MerchantProgramSubscriptionReadiness struct {
	MerchantID              uuid.UUID                               `json:"merchant_id"`
	HasCurrentSubscription  bool                                    `json:"has_current_subscription"`
	HasActiveSubscription   bool                                    `json:"has_active_subscription"`
	CurrentSubscriptionID   *uuid.UUID                              `json:"current_subscription_id,omitempty"`
	ActiveSubscriptionID    *uuid.UUID                              `json:"active_subscription_id,omitempty"`
	CurrentPlanID           *uuid.UUID                              `json:"current_plan_id,omitempty"`
	ActivePlanID            *uuid.UUID                              `json:"active_plan_id,omitempty"`
	CurrentStatus           *data.MerchantProgramSubscriptionStatus `json:"current_status,omitempty"`
	ReadyForFutureOfferings bool                                    `json:"ready_for_future_offerings"`
}

func (r MerchantProgramSubscriptionReadiness) IsReady() bool {
	return r.ReadyForFutureOfferings
}

func validateMerchantProgramSubscriptionService(s *Service) error {
	if s == nil {
		return errors.New("merchant program subscription service is required")
	}
	if s.Logger == nil {
		return errors.New("merchant program subscription service logger is required")
	}
	if s.Models == nil {
		return errors.New("merchant program subscription service models are required")
	}
	if s.Cfg == nil {
		return errors.New("merchant program subscription service config is required")
	}
	if s.Cfg.DBTimeout <= 0 {
		return errors.New("merchant program subscription service DB timeout must be positive")
	}
	return nil
}

// ValidateMerchantProgramSubscriptionReadinessInternal checks a merchant's subscription readiness.
//
// This method performs two sequential data-layer lookups: current subscription
// and active subscription. It applies an outer budget of 2 * DBTimeout so the
// combined readiness check is governed as one operation while each resolver
// still applies its own per-query DB timeout.
func (s *Service) ValidateMerchantProgramSubscriptionReadinessInternal(
	ctx context.Context,
	merchantID uuid.UUID,
) (MerchantProgramSubscriptionReadiness, error) {
	if err := validateMerchantProgramSubscriptionService(s); err != nil {
		return MerchantProgramSubscriptionReadiness{}, err
	}
	if merchantID == uuid.Nil {
		return MerchantProgramSubscriptionReadiness{}, errors.New("merchant ID is required")
	}

	ctx, cancel := context.WithTimeout(ctx, 2*s.Cfg.DBTimeout)
	defer cancel()

	logger := s.Logger.GetLoggerWithContextFromContext(ctx).
		WithFunctionName("ValidateMerchantProgramSubscriptionReadinessInternal")

	result := MerchantProgramSubscriptionReadiness{
		MerchantID: merchantID,
	}

	current, err := s.ResolveCurrentMerchantProgramSubscriptionInternal(ctx, merchantID)
	if err != nil {
		logger.Error("Current merchant program subscription readiness resolution failed", "merchant_id", merchantID, "error", err)
		return MerchantProgramSubscriptionReadiness{}, err
	}

	if current != nil {
		result.HasCurrentSubscription = true

		currentID := current.ID
		result.CurrentSubscriptionID = &currentID

		currentPlanID := current.PlanID
		result.CurrentPlanID = &currentPlanID

		currentStatus := current.Status
		result.CurrentStatus = &currentStatus
	}

	active, err := s.ResolveActiveMerchantProgramSubscriptionInternal(ctx, merchantID)
	if err != nil {
		logger.Error("Active merchant program subscription readiness resolution failed", "merchant_id", merchantID, "error", err)
		return MerchantProgramSubscriptionReadiness{}, err
	}

	if active != nil {
		result.HasActiveSubscription = true

		activeID := active.ID
		result.ActiveSubscriptionID = &activeID

		activePlanID := active.PlanID
		result.ActivePlanID = &activePlanID

		// GetActiveByMerchantID already enforces status = active and deleted_at IS NULL.
		result.ReadyForFutureOfferings = active.Status == data.SubscriptionStatusActive &&
			active.PlanID != uuid.Nil
	}

	logger.Info(
		"Merchant program subscription readiness checked",
		"merchant_id", merchantID,
		"has_current_subscription", result.HasCurrentSubscription,
		"has_active_subscription", result.HasActiveSubscription,
		"ready_for_future_offerings", result.ReadyForFutureOfferings,
	)

	return result, nil
}

// ResolveCurrentMerchantProgramSubscriptionInternal resolves the merchant's current subscription.
//
// A nil subscription with nil error means no current subscription was found.
// Callers must nil-check the returned subscription before use.
func (s *Service) ResolveCurrentMerchantProgramSubscriptionInternal(
	ctx context.Context,
	merchantID uuid.UUID,
) (*data.MerchantProgramSubscription, error) {
	if err := validateMerchantProgramSubscriptionService(s); err != nil {
		return nil, err
	}
	if merchantID == uuid.Nil {
		return nil, errors.New("merchant ID is required")
	}

	logger := s.Logger.GetLoggerWithContextFromContext(ctx).
		WithFunctionName("ResolveCurrentMerchantProgramSubscriptionInternal")

	dbCtx, cancel := context.WithTimeout(ctx, s.Cfg.DBTimeout)
	defer cancel()

	subscription, err := s.Models.MerchantProgramSubscription.GetCurrentByMerchantID(dbCtx, merchantID)
	if err != nil {
		logger.Error("Failed to resolve current merchant program subscription", "merchant_id", merchantID, "error", err)
		return nil, fmt.Errorf("resolve current merchant program subscription for merchant %s: %w", merchantID, err)
	}

	return subscription, nil
}

// ResolveActiveMerchantProgramSubscriptionInternal resolves the merchant's active subscription.
//
// A nil subscription with nil error means no active subscription was found.
// Callers must nil-check the returned subscription before use.
func (s *Service) ResolveActiveMerchantProgramSubscriptionInternal(
	ctx context.Context,
	merchantID uuid.UUID,
) (*data.MerchantProgramSubscription, error) {
	if err := validateMerchantProgramSubscriptionService(s); err != nil {
		return nil, err
	}
	if merchantID == uuid.Nil {
		return nil, errors.New("merchant ID is required")
	}

	logger := s.Logger.GetLoggerWithContextFromContext(ctx).
		WithFunctionName("ResolveActiveMerchantProgramSubscriptionInternal")

	dbCtx, cancel := context.WithTimeout(ctx, s.Cfg.DBTimeout)
	defer cancel()

	subscription, err := s.Models.MerchantProgramSubscription.GetActiveByMerchantID(dbCtx, merchantID)
	if err != nil {
		logger.Error("Failed to resolve active merchant program subscription", "merchant_id", merchantID, "error", err)
		return nil, fmt.Errorf("resolve active merchant program subscription for merchant %s: %w", merchantID, err)
	}

	return subscription, nil
}