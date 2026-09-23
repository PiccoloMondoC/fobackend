// Package services contains application-level orchestration for trusted
// internal workflows.
//
// focodebase/fobackend/internal/services/merchants_internal.go
//
// GTM:
//
//	Layer: 2.4 Merchant / Future Offering Domain
//	Release Class: SPINE
//	Reason:
//	  Owns the reusable application-level authorization boundary for
//	  authenticated Merchant identity reads and mutations. Merchant authority
//	  is established from the persisted principal relationship owned by the
//	  Merchant Account domain before canonical Merchant persistence is read
//	  or changed.
//
// SPINE Rule:
//
//	Keep compiling.
//	Keep production-ready.
//	Establish Merchant authority exclusively from persisted Merchant Account
//	principal state.
//	Treat caller-supplied Merchant identity, including X-Merchant-ID, only as
//	a resource selector and never as authorization.
//	Do not duplicate Merchant validation, URL canonicalization, uniqueness,
//	or persistence rules owned by data.MerchantModel.
//	Do not introduce a competing Merchant creation path; Merchant creation
//	remains owned by merchant onboarding.
//	Do not leak Merchant existence to an unauthorized actor.
//	Keep Merchant operations transport-neutral and bounded by the service's
//	database timeout.
package services

import (
	"context"
	"fmt"

	"github.com/PiccoloMondoC/focodebase/fobackend/internal/data"

	"github.com/google/uuid"
)

// MerchantUpdateInput carries the complete mutable Merchant identity tuple.
//
// Canonical validation, trimming, HTTP(S) URL validation and URL
// canonicalization remain owned by data.MerchantModel. This type is a
// transport-neutral application input only.
type MerchantUpdateInput struct {
	Name    string
	LogoURL *string
	Website *string
}

// merchantOperationContext validates the common Merchant service inputs and
// establishes the single bounded context used for the complete service
// operation.
//
// The returned context covers both principal authorization and the subsequent
// Merchant persistence operation. Callers must invoke the returned cancel
// function.
func (s *Service) merchantOperationContext(
	ctx context.Context,
	actorUserID uuid.UUID,
	merchantID uuid.UUID,
) (context.Context, context.CancelFunc, error) {
	if ctx == nil {
		return nil, nil, ErrNilContext
	}

	if actorUserID == uuid.Nil {
		return nil, nil, ErrMerchantActorRequired
	}

	if merchantID == uuid.Nil {
		return nil, nil, ErrMerchantResourceRequired
	}

	if err := s.validate(); err != nil {
		return nil, nil, err
	}

	dbCtx, cancel := context.WithTimeout(ctx, s.Cfg.DBTimeout)

	return dbCtx, cancel, nil
}

// requireMerchantPrincipal establishes that actorUserID is the persisted
// principal authorized to operate merchantID.
//
// merchantID is a resource selector only. Authorization is derived exclusively
// from persisted Merchant Account principal state.
//
// The caller must supply the already-bounded Merchant operation context.
func (s *Service) requireMerchantPrincipal(
	ctx context.Context,
	actorUserID uuid.UUID,
	merchantID uuid.UUID,
) error {
	authorized, err := s.Models.MerchantAccount.IsPrincipalForMerchant(
		ctx,
		actorUserID,
		merchantID,
	)
	if err != nil {
		return fmt.Errorf(
			"merchant service: resolve principal authorization: %w",
			err,
		)
	}

	if !authorized {
		// Authorization failure deliberately does not distinguish between an
		// existing Merchant owned by another principal and a Merchant selector
		// for which no authorized principal relationship exists.
		return ErrMerchantAccessForbidden
	}

	return nil
}

// GetMerchantInternal returns the canonical Merchant identity selected by
// merchantID after establishing that actorUserID is its persisted principal.
//
// Authorization is performed before Merchant retrieval so an unauthorized
// actor cannot use this operation to determine whether a Merchant exists.
func (s *Service) GetMerchantInternal(
	ctx context.Context,
	actorUserID uuid.UUID,
	merchantID uuid.UUID,
) (*data.Merchant, error) {
	dbCtx, cancel, err := s.merchantOperationContext(
		ctx,
		actorUserID,
		merchantID,
	)
	if err != nil {
		return nil, err
	}
	defer cancel()

	if err := s.requireMerchantPrincipal(
		dbCtx,
		actorUserID,
		merchantID,
	); err != nil {
		return nil, err
	}

	merchant, err := s.Models.Merchant.GetByID(dbCtx, merchantID)
	if err != nil {
		return nil, fmt.Errorf(
			"merchant service: get merchant: %w",
			err,
		)
	}

	return merchant, nil
}

// UpdateMerchantInternal replaces the complete mutable Merchant identity tuple
// for merchantID after establishing that actorUserID is its persisted
// principal.
//
// Canonical Merchant validation, normalization, uniqueness enforcement and
// persistence remain owned by data.MerchantModel. Data-layer sentinels remain
// discoverable through errors.Is because persistence failures are wrapped with
// %w.
//
// Authorization is performed before mutation so an unauthorized actor cannot
// use this operation to determine whether a Merchant exists.
func (s *Service) UpdateMerchantInternal(
	ctx context.Context,
	actorUserID uuid.UUID,
	merchantID uuid.UUID,
	in MerchantUpdateInput,
) (*data.Merchant, error) {
	dbCtx, cancel, err := s.merchantOperationContext(
		ctx,
		actorUserID,
		merchantID,
	)
	if err != nil {
		return nil, err
	}
	defer cancel()

	if err := s.requireMerchantPrincipal(
		dbCtx,
		actorUserID,
		merchantID,
	); err != nil {
		return nil, err
	}

	merchant := &data.Merchant{
		ID:      merchantID,
		Name:    in.Name,
		LogoURL: in.LogoURL,
		Website: in.Website,
	}

	if err := s.Models.Merchant.Update(dbCtx, merchant); err != nil {
		return nil, fmt.Errorf(
			"merchant service: update merchant: %w",
			err,
		)
	}

	return merchant, nil
}