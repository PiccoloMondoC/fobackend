// Package services contains application-level orchestration for trusted
// internal workflows.
//
// sdworkspace/sdbackend/internal/services/merchant_accounts_internal.go
//
// GTM:
//
//	Layer: 2.4 Merchant / Future Offering Domain
//	Release Class: SPINE
//	Reason:
//	  merchant_accounts is the canonical platform-account lifecycle record
//	  underlying Merchant Center access, merchant onboarding, Future Offering
//	  ownership, merchant subscriptions, entitlements, and billing.
//
//	  This service owns the trusted internal creation boundary used by
//	  merchant-onboarding workflows. It delegates persistence, pending-status
//	  assignment, identifier generation, database timestamps, relationship
//	  enforcement, and canonical validation to the data layer.
//
// SPINE Rule:
//
//	Keep compiling.
//	Keep production-ready.
//	Create merchant accounts only for real merchant onboarding workflows.
//	Never seed, fabricate, or automatically activate merchant accounts.
//	Preserve the data layer as the sole owner of merchant-account persistence,
//	initial pending status, and lifecycle enforcement.
//	Preserve initial_plan_id as optional historical onboarding context only.
//	Never mutate merchant-account lifecycle state during application startup.
//	Block deployment if this file permits duplicate merchant accounts,
//	bypasses canonical validation, or introduces startup mutation.
package services

import (
	"context"
	"errors"
	"fmt"

	"github.com/PiccoloMondoC/sdworkspace/sdbackend/internal/data"

	"github.com/google/uuid"
)

// CreateMerchantAccountInternal creates the canonical pending merchant account
// for an existing merchant.
//
// initialPlanID is optional historical onboarding context. It does not represent
// the merchant's current subscription, entitlement, billing, or access state.
//
// This operation does not activate the account. Activation remains a distinct,
// explicitly authorized lifecycle transition.
//
// The data layer remains authoritative for:
//
//   - merchant and initial-plan relationship enforcement;
//   - one-account-per-merchant enforcement;
//   - merchant-account identifier generation;
//   - canonical pending-status assignment;
//   - database-owned timestamps;
//   - SQL execution and persistence errors.
//
// Wrapped data-layer errors remain inspectable through errors.Is and errors.As.
func (s *Service) CreateMerchantAccountInternal(
	ctx context.Context,
	merchantID uuid.UUID,
	initialPlanID *uuid.UUID,
) (*data.MerchantAccount, error) {
	if s == nil {
		return nil, errors.New("create merchant account: service is required")
	}
	if ctx == nil {
		return nil, errors.New("create merchant account: context is required")
	}
	if s.Models == nil {
		return nil, errors.New("create merchant account: data models are required")
	}
	if s.Cfg == nil {
		return nil, errors.New("create merchant account: configuration is required")
	}
	if merchantID == uuid.Nil {
		return nil, errors.New("create merchant account: merchant ID is required")
	}
	if initialPlanID != nil && *initialPlanID == uuid.Nil {
		return nil, errors.New(
			"create merchant account: initial merchant program plan ID cannot be nil UUID",
		)
	}

	dbCtx, cancel := context.WithTimeout(ctx, s.Cfg.DBTimeout)
	defer cancel()

	account, err := s.Models.MerchantAccount.Insert(
		dbCtx,
		merchantID,
		initialPlanID,
	)
	if err != nil {
		return nil, fmt.Errorf(
			"create pending merchant account for merchant %s: %w",
			merchantID,
			err,
		)
	}

	return account, nil
}
