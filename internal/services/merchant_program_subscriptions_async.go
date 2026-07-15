// Package services contains async wrappers for merchant program subscription
// internal workflows.
//
// sdworkspace/sdbackend/internal/services/merchant_program_subscriptions_async.go
//
// GTM:
//
//	Layer: 2.4 Merchant / Future Offering Domain
//	Release Class: SPINE
//	Reason:
//	  Async support for merchant program subscription readiness checks.
//	  This file is a thin wrapper only. It does not own readiness logic,
//	  create subscriptions, perform direct DB access, or handle audit behavior.
//
// SPINE Rule:
//
//	Keep compiling.
//	Keep production-ready.
//	Do not introduce subscription bootstrap behavior.
//	Do not introduce direct DB access.
//	Do not introduce handler-layer permissions or audit dependencies.
//	Block deployment if this file breaks async readiness support.
package services

import (
	"context"
	"fmt"

	"github.com/google/uuid"
)

// ValidateMerchantProgramSubscriptionReadinessAsync validates subscription readiness in the background.
func ValidateMerchantProgramSubscriptionReadinessAsync(
	parentCtx context.Context,
	s *Service,
	merchantID uuid.UUID,
) {
	if parentCtx == nil {
		panic("merchant program subscription readiness async: parent context is required")
	}
	if err := validateMerchantProgramSubscriptionService(s); err != nil {
		panic(fmt.Sprintf("merchant program subscription readiness async: %v", err))
	}

	go func() {
		logger := s.Logger.GetLoggerWithContextFromContext(parentCtx).
			WithFunctionName("ValidateMerchantProgramSubscriptionReadinessAsync")

		defer func() {
			if r := recover(); r != nil {
				logger.Error("Recovered panic in merchant program subscription readiness async", "merchant_id", merchantID, "error", fmt.Errorf("%v", r))
			}
		}()

		ctx, cancel := context.WithTimeout(parentCtx, 2*s.Cfg.DBTimeout)
		defer cancel()

		readiness, err := s.ValidateMerchantProgramSubscriptionReadinessInternal(ctx, merchantID)
		if err != nil {
			logger.Error("Merchant program subscription readiness async validation failed", "merchant_id", merchantID, "error", err)
			return
		}

		logger.Info(
			"Merchant program subscription readiness async validation completed",
			"merchant_id", merchantID,
			"has_current_subscription", readiness.HasCurrentSubscription,
			"has_active_subscription", readiness.HasActiveSubscription,
			"ready_for_future_offerings", readiness.IsReady(),
		)
	}()
}