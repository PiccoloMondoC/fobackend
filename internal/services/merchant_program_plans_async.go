// Package services contains async service wrappers for internal system workflows.
//
// sdworkspace/sdbackend/internal/services/merchant_program_plans_async.go
//
// GTM:
//
//	Layer: 2.4 Merchant / Future Offering Domain
//	Release Class: SPINE
//	Reason:
//	  Async support wrapper for merchant program plan SPINE seed data.
//	  This file does not own readiness. Required merchant program plan seed data
//	  must be ensured synchronously before the application is considered ready.
//
// SPINE Rule:
//
//	Keep compiling.
//	Keep production-ready.
//	Do not introduce direct DB access, plan CRUD, audit dependencies,
//	or handler-layer concerns into this file.
//	Block deployment if this file breaks bootstrap support behavior.
package services

import (
	"context"
	"fmt"
	"time"
)

// EnsureDefaultMerchantProgramPlansAsync ensures required merchant program plans in the background.
func EnsureDefaultMerchantProgramPlansAsync(parentCtx context.Context, s *Service) {
	if s == nil || s.Logger == nil {
		panic("EnsureDefaultMerchantProgramPlansAsync: called with nil Service or nil Logger")
	}

	go func() {
		ctx := parentCtx
		if ctx == nil {
			ctx = context.Background()
		}

		logger := s.Logger.GetLoggerWithContextFromContext(ctx).
			WithFunctionName("EnsureDefaultMerchantProgramPlansAsync")

		if parentCtx == nil {
			logger.Warn("EnsureDefaultMerchantProgramPlansAsync called with nil parentCtx; using Background context")
		}

		defer func() {
			if r := recover(); r != nil {
				logger.Error("Recovered panic in merchant program plan bootstrap", "error", fmt.Errorf("%v", r))
			}
		}()

		ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()

		if err := s.EnsureDefaultMerchantProgramPlansInternal(ctx); err != nil {
			logger.Error("Default merchant program plan bootstrap failed", "error", err)
			return
		}

		logger.Info("Default merchant program plan bootstrap completed")
	}()
}
