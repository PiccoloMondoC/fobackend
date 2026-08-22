// Package services contains trusted internal service composition,
// automation support, moderation helpers, and shared internal workflow logic.
//
// sdworkspace/sdbackend/internal/services/errors.go
//
// GTM:
//
//	Layer: 2.6 Internal Services / Automation Foundation
//	Release Class: SPINE
//	Reason:
//	  Defines stable internal-service sentinel errors used by composition,
//	  decimal validation, offer processing, status resolution, and governed
//	  notification-job behavior.
//
// SPINE Rule:
//
//	Keep compiling.
//	Keep production-ready.
//	Preserve errors.Is compatibility for every sentinel defined here.
//	Wrap underlying failures with %w when adding operational context.
//	Never collapse cancellation, timeout, datastore, configuration, validation,
//	or not-found failures into an unrelated error category.
//	Do not expose secrets, credentials, or sensitive payloads in errors.
package services

import "errors"

var (
	ErrInvalidServiceConfiguration                  = errors.New("invalid internal service configuration")
	ErrNilContext                                   = errors.New("context must not be nil")
	ErrNilOffer                                     = errors.New("offer must not be nil")
	ErrInvalidDecimal                               = errors.New("invalid decimal value")
	ErrOfferMetadataIncomplete                      = errors.New("offer metadata incomplete")
	ErrStatusNameRequired                           = errors.New("status name cannot be empty")
	ErrMaintenanceJobDisabled                       = errors.New("system maintenance notification job is disabled")
	ErrInvalidMerchantProgramSubscriptionTransition = errors.New(
		"invalid merchant program subscription transition",
	)
	ErrUnsupportedMerchantProgramSubscriptionTransition = errors.New(
		"unsupported merchant program subscription transition",
	)
	ErrMerchantProgramSubscriptionPlanUnchanged = errors.New(
		"merchant program subscription plan is unchanged",
	)
	ErrMerchantBillableEventSourceNotFound = errors.New(
		"merchant billable event authoritative source not found",
	)

	ErrMerchantBillableEventSourceIneligible = errors.New(
		"merchant billable event authoritative source is ineligible",
	)
	ErrMerchantPlatformCreditApplicationFeeCalculationNotApproved = errors.New(
		"merchant platform credit application requires an approved fee calculation",
	)

	ErrMerchantPlatformCreditApplicationExceedsObligation = errors.New(
		"merchant platform credit application would exceed the fee calculation obligation",
	)

	ErrMerchantPlatformCreditApplicationMerchantMismatch = errors.New(
		"merchant platform credit account and fee calculation belong to different merchants",
	)
	ErrMerchantFutureOfferingServiceTermOperatingRangeInvalid = errors.New(
		"merchant future offering service term operating range configuration is invalid",
	)

	ErrMerchantFutureOfferingServiceTermDurationOutsideOperatingRange = errors.New(
		"merchant future offering service term duration is outside the currently permitted operating range",
	)
)
