// Package services contains async service wrappers for internal system workflows.
//
// sdworkspace/sdbackend/internal/services/merchant_program_entitlements_async.go
//
// GTM:
//
//	Layer: 2.4 Merchant / Future Offering Domain
//	Release Class: SPINE
//	Reason:
//	  Async support wrappers for merchant program entitlement SPINE workflows.
//	  These wrappers preserve cancellation, return completion results, and do
//	  not pretend async work completed before service execution finishes.
//
//	  Required merchant program entitlement seed data should still be ensured
//	  synchronously before the application is considered ready. Async wrappers
//	  exist for controlled internal orchestration, not for hiding startup
//	  readiness failures.
//
// SPINE Rule:
//
//	Keep compiling.
//	Keep production-ready.
//	Do not introduce direct DB access, handler-layer concerns, route logic,
//	or audit dependencies into this file.
//	Preserve cancellation and result reporting.
//	Block deployment if this file breaks entitlement bootstrap support,
//	capability-gate support, or async error propagation.
package services

import (
	"context"
	"fmt"
	"time"

	"github.com/PiccoloMondoC/sdworkspace/sdbackend/internal/data"

	"github.com/google/uuid"
)

// MerchantProgramEntitlementAsyncResult carries async entitlement operation
// results.
type MerchantProgramEntitlementAsyncResult struct {
	Entitlement *data.MerchantProgramEntitlement
	Err         error
}

// MerchantProgramEntitlementBoolAsyncResult carries async entitlement boolean
// check results.
type MerchantProgramEntitlementBoolAsyncResult struct {
	Value bool
	Err   error
}

// MerchantProgramEntitlementReadinessAsyncResult carries async entitlement
// readiness results.
type MerchantProgramEntitlementReadinessAsyncResult struct {
	Readiness MerchantProgramEntitlementReadiness
	Err       error
}

// MerchantProgramEntitlementsListAsyncResult carries async entitlement list
// results.
type MerchantProgramEntitlementsListAsyncResult struct {
	Entitlements []*data.MerchantProgramEntitlement
	Err          error
}

// EnsureMerchantProgramEntitlementAsync ensures one merchant program
// entitlement asynchronously and returns a completion channel.
func EnsureMerchantProgramEntitlementAsync(
	parentCtx context.Context,
	s *Service,
	planID uuid.UUID,
	code data.MerchantProgramEntitlementCode,
) <-chan MerchantProgramEntitlementAsyncResult {
	ch := make(chan MerchantProgramEntitlementAsyncResult, 1)

	go func() {
		defer close(ch)

		ctx := parentCtx
		if ctx == nil {
			ctx = context.Background()
		}

		entitlement, err := s.EnsureMerchantProgramEntitlementInternal(ctx, planID, code)
		ch <- MerchantProgramEntitlementAsyncResult{
			Entitlement: entitlement,
			Err:         err,
		}
	}()

	return ch
}

// EnsureDefaultMerchantProgramEntitlementsAsync ensures canonical default
// merchant program entitlements asynchronously and returns a completion channel.
func EnsureDefaultMerchantProgramEntitlementsAsync(
	parentCtx context.Context,
	s *Service,
) <-chan error {
	ch := make(chan error, 1)

	go func() {
		defer close(ch)

		ctx := parentCtx
		if ctx == nil {
			ctx = context.Background()
		}

		ch <- s.EnsureDefaultMerchantProgramEntitlementsInternal(ctx)
	}()

	return ch
}

// ValidateMerchantProgramEntitlementReadinessAsync checks entitlement readiness
// asynchronously and returns a completion channel.
func ValidateMerchantProgramEntitlementReadinessAsync(
	parentCtx context.Context,
	s *Service,
) <-chan MerchantProgramEntitlementReadinessAsyncResult {
	ch := make(chan MerchantProgramEntitlementReadinessAsyncResult, 1)

	go func() {
		defer close(ch)

		ctx := parentCtx
		if ctx == nil {
			ctx = context.Background()
		}

		readiness, err := s.ValidateMerchantProgramEntitlementReadinessInternal(ctx)
		ch <- MerchantProgramEntitlementReadinessAsyncResult{
			Readiness: readiness,
			Err:       err,
		}
	}()

	return ch
}

// PlanHasMerchantProgramEntitlementAsync checks a merchant program entitlement
// asynchronously and returns a completion channel.
func PlanHasMerchantProgramEntitlementAsync(
	parentCtx context.Context,
	s *Service,
	planID uuid.UUID,
	code data.MerchantProgramEntitlementCode,
) <-chan MerchantProgramEntitlementBoolAsyncResult {
	ch := make(chan MerchantProgramEntitlementBoolAsyncResult, 1)

	go func() {
		defer close(ch)

		ctx := parentCtx
		if ctx == nil {
			ctx = context.Background()
		}

		value, err := s.PlanHasMerchantProgramEntitlementInternal(ctx, planID, code)
		ch <- MerchantProgramEntitlementBoolAsyncResult{
			Value: value,
			Err:   err,
		}
	}()

	return ch
}

// RequireMerchantProgramEntitlementAsync requires a merchant program entitlement
// asynchronously and returns a completion channel.
func RequireMerchantProgramEntitlementAsync(
	parentCtx context.Context,
	s *Service,
	planID uuid.UUID,
	code data.MerchantProgramEntitlementCode,
) <-chan error {
	ch := make(chan error, 1)

	go func() {
		defer close(ch)

		ctx := parentCtx
		if ctx == nil {
			ctx = context.Background()
		}

		ch <- s.RequireMerchantProgramEntitlementInternal(ctx, planID, code)
	}()

	return ch
}

// RequireLaunchCampaignAccessAsync requires Launch Campaign access
// asynchronously and returns a completion channel.
func RequireLaunchCampaignAccessAsync(
	parentCtx context.Context,
	s *Service,
	planID uuid.UUID,
) <-chan error {
	ch := make(chan error, 1)

	go func() {
		defer close(ch)

		ctx := parentCtx
		if ctx == nil {
			ctx = context.Background()
		}

		ch <- s.RequireLaunchCampaignAccessInternal(ctx, planID)
	}()

	return ch
}

// RequireFutureOfferingAccessAsync requires Future Offering access
// asynchronously and returns a completion channel.
func RequireFutureOfferingAccessAsync(
	parentCtx context.Context,
	s *Service,
	planID uuid.UUID,
) <-chan error {
	ch := make(chan error, 1)

	go func() {
		defer close(ch)

		ctx := parentCtx
		if ctx == nil {
			ctx = context.Background()
		}

		ch <- s.RequireFutureOfferingAccessInternal(ctx, planID)
	}()

	return ch
}

// ListMerchantProgramEntitlementsForPlanAsync lists merchant program
// entitlements for a plan asynchronously and returns a completion channel.
func ListMerchantProgramEntitlementsForPlanAsync(
	parentCtx context.Context,
	s *Service,
	planID uuid.UUID,
) <-chan MerchantProgramEntitlementsListAsyncResult {
	ch := make(chan MerchantProgramEntitlementsListAsyncResult, 1)

	go func() {
		defer close(ch)

		ctx := parentCtx
		if ctx == nil {
			ctx = context.Background()
		}

		entitlements, err := s.ListMerchantProgramEntitlementsForPlanInternal(ctx, planID)
		ch <- MerchantProgramEntitlementsListAsyncResult{
			Entitlements: entitlements,
			Err:          err,
		}
	}()

	return ch
}

// EnsureDefaultMerchantProgramEntitlementsBootstrapAsync starts default
// entitlement bootstrap in the background for non-readiness-critical internal
// workflows.
//
// Startup readiness must not rely on this helper. Production startup should
// call EnsureDefaultMerchantProgramPlansInternal followed by
// EnsureDefaultMerchantProgramEntitlementsInternal synchronously before the HTTP
// listener opens.
func EnsureDefaultMerchantProgramEntitlementsBootstrapAsync(parentCtx context.Context, s *Service) {
	if s == nil || s.Logger == nil {
		panic("EnsureDefaultMerchantProgramEntitlementsBootstrapAsync: called with nil Service or nil Logger")
	}

	go func() {
		ctx := parentCtx
		if ctx == nil {
			ctx = context.Background()
		}

		logger := s.Logger.GetLoggerWithContextFromContext(ctx).
			WithFunctionName("EnsureDefaultMerchantProgramEntitlementsBootstrapAsync")

		defer func() {
			if r := recover(); r != nil {
				logger.Error("Recovered panic in merchant program entitlement bootstrap", "error", fmt.Errorf("%v", r))
			}
		}()

		ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()

		if err := s.EnsureDefaultMerchantProgramEntitlementsInternal(ctx); err != nil {
			logger.Error("Default merchant program entitlement bootstrap failed", "error", err)
			return
		}

		logger.Info("Default merchant program entitlement bootstrap completed")
	}()
}