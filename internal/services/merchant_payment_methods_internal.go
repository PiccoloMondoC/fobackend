// Package services contains internal business workflows and domain-level
// coordination built on the platform data models.
//
// sdworkspace/sdbackend/internal/services/merchant_payment_methods_internal.go
//
// GTM:
//
//	Layer: 2.4 Merchant Payments Architecture
//	Release Class: SPINE
//	Reason:
//	  Merchant payment-method internal services provide factual,
//	  platform-local capability resolution and safe active-default resolution
//	  for trusted internal consumers.
//
//	  These services coordinate canonical data-layer reads without assuming
//	  provider-link, verification, payment-execution, treasury,
//	  authorization, settlement, or successful-collection responsibilities.
//
// Architecture Boundary:
//
//	This file operates only on canonical merchant payment methods.
//
//	It may answer:
//
//	  - whether the merchant has an active canonical payment method;
//	  - whether the merchant has an active canonical default;
//	  - which canonical active default is currently designated.
//
//	It must not claim or infer:
//
//	  - external-provider connectivity;
//	  - provider verification;
//	  - instrument ownership or validity;
//	  - authorization capability;
//	  - available funds;
//	  - payment collectability;
//	  - payment success.
//
//	Provider identity, provider references, connectivity, verification,
//	synchronization, and disconnection belong to
//	merchant_payment_method_provider_links.go.
//
//	Payment authorization, execution, outcome, and transaction recording
//	belong to merchant_payments.go.
//
// Security Boundary:
//
//	This file operates only on canonical payment-method identity, lifecycle,
//	default state, and safe display metadata.
//
//	It must never accept, resolve, expose, log, trace, audit, or metric-label:
//
//	  - full card numbers;
//	  - bank account or routing numbers;
//	  - CVVs or PINs;
//	  - provider payment-method references;
//	  - provider access or bearer tokens;
//	  - provider account identifiers;
//	  - provider credentials;
//	  - raw provider payloads.
//
//	last_four remains unverified display metadata only.
//
// Operational Capability Doctrine:
//
//	Engineering provides complete canonical payment-method resolution
//	capability and stable extension boundaries.
//
//	Administration governs variable operational policy outside this file,
//	including whether payment methods are required, which method types are
//	enabled, which installed providers are available, whether verification is
//	required, and which conditions make a method eligible for payment
//	execution.
//
// SPINE Rule:
//
//	Keep compiling.
//	Keep production-ready.
//	Preserve merchant ownership scoping on every payment-method lookup.
//	Preserve the data layer as the owner of persistence, default assignment,
//	lifecycle transitions, soft deletion, restoration, and concurrency
//	invariants.
//	Fail closed when a resolved payment method does not satisfy the stronger
//	internal result contract.
//	Report factual platform-local state only.
//	Never treat canonical active or default state as proof of provider
//	connectivity, verification, authorization, collectability, or payment
//	success.
//	Never hard-code whether a payment method is commercially required.
//	Never seed, create, restore, activate, default, or otherwise mutate
//	merchant-owned payment methods during application startup.
//	Do not introduce provider synchronization into this canonical
//	payment-method service.
//	Do not introduce an async wrapper without a genuine asynchronous
//	operational responsibility.
//	Block deployment if this file breaks build, merchant ownership
//	confinement, active-default resolution, lifecycle integrity, or
//	capability/policy separation.
package services

import (
	"context"
	"errors"
	"fmt"

	"github.com/PiccoloMondoC/sdworkspace/sdbackend/internal/data"

	"github.com/google/uuid"
)

var (
	// ErrMerchantPaymentMethodDefaultNotSet indicates that an internal
	// workflow requiring a canonical active default cannot proceed because
	// the merchant currently has no active default designated.
	//
	// This differs from data.ErrMerchantPaymentMethodNotFound. No particular
	// payment-method ID was requested and found missing; the merchant's
	// current canonical configuration simply has no active default.
	ErrMerchantPaymentMethodDefaultNotSet = errors.New(
		"merchant payment method: active default is not set",
	)

	// errMerchantPaymentMethodResolvedStateInvalid indicates that the data
	// layer returned a record that does not satisfy the stronger active-default
	// contract required by this internal service.
	//
	// This error is intentionally unexported because it represents an internal
	// consistency failure rather than an ordinary caller-correctable domain
	// condition.
	errMerchantPaymentMethodResolvedStateInvalid = errors.New(
		"merchant payment method: resolved default state is invalid",
	)
)

// MerchantPaymentMethodReadiness reports factual canonical payment-method
// state currently persisted for one merchant.
//
// This type does not determine whether a payment method is required by
// onboarding, Future Offering activation, billing, subscription activation,
// payment execution, or any other commercial workflow. Those decisions belong
// to the applicable policy owner.
//
// The fields are resolved through separate bounded reads and therefore form
// an informational result rather than a transactionally isolated snapshot.
//
// A consumer requiring the current canonical active default at the point of
// use must resolve it again through
// ResolveDefaultMerchantPaymentMethodInternal.
type MerchantPaymentMethodReadiness struct {
	MerchantID uuid.UUID `json:"merchant_id"`

	// HasActivePaymentMethod reports whether at least one non-deleted
	// canonical payment method is currently active for the merchant.
	HasActivePaymentMethod bool `json:"has_active_payment_method"`

	// HasActiveDefaultPaymentMethod reports whether the merchant currently
	// has an active, non-deleted canonical payment method designated as
	// default.
	HasActiveDefaultPaymentMethod bool `json:"has_active_default_payment_method"`
}

// validateMerchantPaymentMethodService verifies the shared dependencies used
// by canonical merchant payment-method internal services.
func validateMerchantPaymentMethodService(s *Service) error {
	switch {
	case s == nil:
		return errors.New("merchant payment method service is required")

	case s.Logger == nil:
		return errors.New(
			"merchant payment method service logger is required",
		)

	case s.Models == nil:
		return errors.New(
			"merchant payment method service models are required",
		)

	case s.Cfg == nil:
		return errors.New(
			"merchant payment method service configuration is required",
		)

	case s.Cfg.DBTimeout <= 0:
		return errors.New(
			"merchant payment method service database timeout must be positive",
		)

	default:
		return nil
	}
}

// validateMerchantPaymentMethodServiceRequest validates common inputs required
// by merchant-scoped canonical payment-method service reads.
func validateMerchantPaymentMethodServiceRequest(
	ctx context.Context,
	merchantID uuid.UUID,
) error {
	if ctx == nil {
		return errors.New("context is required")
	}
	if merchantID == uuid.Nil {
		return errors.New("merchant id is required")
	}

	return nil
}

// ResolveMerchantPaymentMethodReadinessInternal resolves factual,
// platform-local canonical payment-method readiness for one merchant.
//
// This method reports only currently persisted canonical state. It does not
// decide whether the merchant must have a payment method for any particular
// workflow.
//
// The two facts are resolved through separate bounded reads. The result is
// suitable for Merchant Center displays, onboarding status, billing
// preparation, and policy evaluation, but it must not be treated as:
//
//   - an atomic authorization;
//   - evidence of provider connectivity or verification;
//   - evidence that an instrument remains externally valid;
//   - evidence that payment can be successfully executed.
func (s *Service) ResolveMerchantPaymentMethodReadinessInternal(
	ctx context.Context,
	merchantID uuid.UUID,
) (*MerchantPaymentMethodReadiness, error) {
	if err := validateMerchantPaymentMethodService(s); err != nil {
		return nil, err
	}
	if err := validateMerchantPaymentMethodServiceRequest(
		ctx,
		merchantID,
	); err != nil {
		return nil, err
	}

	logger := s.Logger.
		GetLoggerWithContextFromContext(ctx).
		WithFunctionName(
			"ResolveMerchantPaymentMethodReadinessInternal",
		)

	defaultCtx, cancelDefault := context.WithTimeout(
		ctx,
		s.Cfg.DBTimeout,
	)
	defaultMethod, err := s.Models.MerchantPaymentMethod.
		GetDefaultForMerchant(
			defaultCtx,
			merchantID,
		)
	cancelDefault()
	if err != nil {
		logger.Error(
			"Failed to resolve canonical merchant payment method default state",
			"merchant_id", merchantID,
			"error", err,
		)
		return nil, fmt.Errorf(
			"resolve merchant payment method readiness default state: %w",
			err,
		)
	}

	activeStatus := data.PaymentMethodStatusActive

	activeCtx, cancelActive := context.WithTimeout(
		ctx,
		s.Cfg.DBTimeout,
	)
	activeMethods, err := s.Models.MerchantPaymentMethod.ListByMerchant(
		activeCtx,
		merchantID,
		&activeStatus,
		1,
		0,
	)
	cancelActive()
	if err != nil {
		logger.Error(
			"Failed to resolve canonical merchant payment method active state",
			"merchant_id", merchantID,
			"error", err,
		)
		return nil, fmt.Errorf(
			"resolve merchant payment method readiness active state: %w",
			err,
		)
	}

	return &MerchantPaymentMethodReadiness{
		MerchantID:             merchantID,
		HasActivePaymentMethod: len(activeMethods) > 0,
		HasActiveDefaultPaymentMethod: IsMerchantPaymentMethodActiveDefault(
			defaultMethod,
			merchantID,
		),
	}, nil
}

// ResolveDefaultMerchantPaymentMethodInternal resolves the merchant's current
// canonical active, non-deleted default payment method.
//
// This method does not certify the returned method for payment collection.
// Successful resolution proves only that the platform-local canonical record:
//
//   - belongs to the requested merchant;
//   - is active;
//   - is not soft-deleted;
//   - is designated as default.
//
// Provider connectivity, verification, external instrument validity,
// authorization eligibility, and payment execution must be resolved by their
// respective provider-link and payment-execution domains.
//
// data.MerchantPaymentMethodModel.GetDefaultForMerchant returns (nil, nil)
// when no default exists because absence is a valid direct-read result. This
// stronger internal resolver converts that outcome into
// ErrMerchantPaymentMethodDefaultNotSet for workflows that structurally
// require the canonical active default.
func (s *Service) ResolveDefaultMerchantPaymentMethodInternal(
	ctx context.Context,
	merchantID uuid.UUID,
) (*data.MerchantPaymentMethod, error) {
	if err := validateMerchantPaymentMethodService(s); err != nil {
		return nil, err
	}
	if err := validateMerchantPaymentMethodServiceRequest(
		ctx,
		merchantID,
	); err != nil {
		return nil, err
	}

	logger := s.Logger.
		GetLoggerWithContextFromContext(ctx).
		WithFunctionName(
			"ResolveDefaultMerchantPaymentMethodInternal",
		)

	dbCtx, cancel := context.WithTimeout(ctx, s.Cfg.DBTimeout)
	defer cancel()

	method, err := s.Models.MerchantPaymentMethod.GetDefaultForMerchant(
		dbCtx,
		merchantID,
	)
	if err != nil {
		logger.Error(
			"Failed to resolve canonical default merchant payment method",
			"merchant_id", merchantID,
			"error", err,
		)
		return nil, fmt.Errorf(
			"resolve default merchant payment method: %w",
			err,
		)
	}

	if method == nil {
		return nil, ErrMerchantPaymentMethodDefaultNotSet
	}

	if !IsMerchantPaymentMethodActiveDefault(method, merchantID) {
		logger.Error(
			"Resolved merchant payment method failed canonical active-default validation",
			"merchant_id", merchantID,
			"payment_method_id", method.ID,
		)
		return nil, errMerchantPaymentMethodResolvedStateInvalid
	}

	return method, nil
}

// IsMerchantPaymentMethodOperational reports whether an already-resolved
// canonical payment-method record belongs to the requested merchant and is
// currently active and non-deleted.
//
// This is a pure platform-local state check. It does not prove provider
// connectivity, provider verification, external instrument validity,
// authorization capability, collectability, available funds, or payment
// success.
func IsMerchantPaymentMethodOperational(
	method *data.MerchantPaymentMethod,
	merchantID uuid.UUID,
) bool {
	if method == nil || merchantID == uuid.Nil {
		return false
	}
	if method.MerchantID != merchantID {
		return false
	}
	if method.DeletedAt != nil {
		return false
	}

	return data.NormalizeMerchantPaymentMethodStatus(method.Status) ==
		data.PaymentMethodStatusActive
}

// IsMerchantPaymentMethodActiveDefault reports whether an already-resolved
// canonical payment-method record is operational for the requested merchant
// and is designated as that merchant's default.
//
// This helper verifies only the service-level result contract. Default
// assignment concurrency and uniqueness remain owned by the data layer and
// database constraints.
func IsMerchantPaymentMethodActiveDefault(
	method *data.MerchantPaymentMethod,
	merchantID uuid.UUID,
) bool {
	return IsMerchantPaymentMethodOperational(method, merchantID) &&
		method.IsDefault
}
