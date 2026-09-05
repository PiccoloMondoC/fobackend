// Package services contains asynchronous wrappers for trusted internal
// workflows.
//
// sdworkspace/sdbackend/internal/services/merchant_accounts_async.go
//
// GTM:
//
//	Layer: 2.4 Merchant / Future Offering Domain
//	Release Class: SPINE
//	Reason:
//	  Provides the asynchronous execution boundary for merchant-account
//	  creation when a supervised onboarding workflow intentionally performs
//	  that operation outside the initiating call stack.
//
//	  The canonical operation remains CreateMerchantAccountInternal. This
//	  file contains no independent persistence or lifecycle logic.
//
// SPINE Rule:
//
//	Keep compiling.
//	Keep production-ready.
//	Delegate all merchant-account creation to the canonical internal service.
//	Never duplicate SQL, identifier generation, pending-status assignment,
//	relationship validation, or lifecycle logic.
//	Never use request-scoped contexts after their request has completed.
//	Never use this wrapper for application-startup readiness or seeding.
//	Always surface completion through the supplied result channel.
package services

import (
	"context"
	"errors"
	"fmt"

	"github.com/PiccoloMondoC/focodebase/fobackend/internal/data"

	"github.com/google/uuid"
)

// MerchantAccountCreateResult contains the outcome of an asynchronous merchant
// account creation operation.
//
// Exactly one result is sent for each invocation. Account is non-nil only when
// Err is nil.
type MerchantAccountCreateResult struct {
	Account *data.MerchantAccount
	Err     error
}

// CreateMerchantAccountAsync schedules creation of a canonical pending merchant
// account and returns a channel that receives the operation result.
//
// initialPlanID is optional historical onboarding context and is forwarded
// unchanged to the canonical internal service.
//
// The caller must supply a context whose lifetime covers the background
// operation. An HTTP request context must not be retained after the originating
// request has completed.
//
// The returned channel is buffered so the worker can publish its result and
// terminate even when the caller does not immediately receive it.
func (s *Service) CreateMerchantAccountAsync(
	ctx context.Context,
	merchantID uuid.UUID,
	initialPlanID *uuid.UUID,
) <-chan MerchantAccountCreateResult {
	resultCh := make(chan MerchantAccountCreateResult, 1)

	if s == nil {
		resultCh <- MerchantAccountCreateResult{
			Err: errors.New(
				"create merchant account asynchronously: service is required",
			),
		}
		close(resultCh)
		return resultCh
	}

	if ctx == nil {
		resultCh <- MerchantAccountCreateResult{
			Err: errors.New(
				"create merchant account asynchronously: context is required",
			),
		}
		close(resultCh)
		return resultCh
	}

	if merchantID == uuid.Nil {
		resultCh <- MerchantAccountCreateResult{
			Err: errors.New(
				"create merchant account asynchronously: merchant ID is required",
			),
		}
		close(resultCh)
		return resultCh
	}

	if initialPlanID != nil && *initialPlanID == uuid.Nil {
		resultCh <- MerchantAccountCreateResult{
			Err: errors.New(
				"create merchant account asynchronously: initial merchant program plan ID cannot be nil UUID",
			),
		}
		close(resultCh)
		return resultCh
	}

	go func() {
		defer close(resultCh)

		defer func() {
			if recovered := recover(); recovered != nil {
				resultCh <- MerchantAccountCreateResult{
					Err: fmt.Errorf(
						"create merchant account asynchronously: recovered panic: %v",
						recovered,
					),
				}
			}
		}()

		account, err := s.CreateMerchantAccountInternal(
			ctx,
			merchantID,
			initialPlanID,
		)

		resultCh <- MerchantAccountCreateResult{
			Account: account,
			Err:     err,
		}
	}()

	return resultCh
}
