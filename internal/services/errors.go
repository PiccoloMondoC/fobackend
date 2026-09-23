// Package services contains trusted internal service composition,
// automation support, moderation helpers, and shared internal workflow logic.
//
// focodebase/fobackend/internal/services/errors.go
//
// GTM:
//
//	Layer: 2.6 Internal Services / Automation Foundation
//	Release Class: SPINE
//	Reason:
//	  Defines stable internal-service sentinel errors used by composition,
//	  decimal validation, offer processing, status resolution, governed
//	  notification-job behavior, Users identity workflows, and account
//	  lifecycle orchestration.
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
	ErrInvalidServiceConfiguration = errors.New(
		"invalid internal service configuration",
	)

	ErrNilContext = errors.New(
		"context must not be nil",
	)

	ErrNilOffer = errors.New(
		"offer must not be nil",
	)

	ErrInvalidDecimal = errors.New(
		"invalid decimal value",
	)

	ErrOfferMetadataIncomplete = errors.New(
		"offer metadata incomplete",
	)

	ErrStatusNameRequired = errors.New(
		"status name cannot be empty",
	)

	ErrMaintenanceJobDisabled = errors.New(
		"system maintenance notification job is disabled",
	)

	ErrInvalidMerchantProgramSubscriptionTransition = errors.New(
		"invalid merchant program subscription transition",
	)

	ErrUnsupportedMerchantProgramSubscriptionTransition = errors.New(
		"unsupported merchant program subscription transition",
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

	ErrMerchantFutureOfferingServicePeriodInputInvalid = errors.New(
		"invalid merchant future offering service period orchestration request",
	)

	ErrMerchantFutureOfferingServicePeriodServiceTermScopeMismatch = errors.New(
		"merchant future offering service term was not found within the specified future offering",
	)

	ErrMerchantFutureOfferingServicePeriodServiceTermNotEstablished = errors.New(
		"merchant future offering service term is not established",
	)

	ErrMerchantFutureOfferingServicePeriodCreationNotYetEligible = errors.New(
		"merchant future offering service period creation boundary has not yet been reached",
	)

	ErrMerchantFutureOfferingServicePeriodServiceTermComplete = errors.New(
		"merchant future offering service term has no remaining service period",
	)

	ErrMerchantFutureOfferingServicePeriodInvalidState = errors.New(
		"merchant future offering service period orchestration encountered invalid state",
	)

	ErrMerchantFutureOfferingBillingPeriodInputInvalid = errors.New(
		"invalid merchant future offering billing period orchestration request",
	)

	ErrMerchantFutureOfferingBillingPeriodServiceTermScopeMismatch = errors.New(
		"merchant future offering service term was not found within the specified future offering",
	)

	ErrMerchantFutureOfferingBillingPeriodAlreadyExists = errors.New(
		"merchant future offering billing period already exists",
	)

	ErrMerchantFutureOfferingBillingPeriodTermExhausted = errors.New(
		"merchant future offering service term has no remaining billing period",
	)

	ErrMerchantFutureOfferingBillingPeriodCreationNotYetEligible = errors.New(
		"merchant future offering billing period creation boundary has not yet been reached",
	)

	ErrMerchantFutureOfferingBillingPeriodConcurrentCreation = errors.New(
		"merchant future offering billing period creation encountered a concurrent persistence conflict",
	)

	ErrMerchantFutureOfferingBillingPeriodEventConflict = errors.New(
		"merchant future offering billing period outbox event conflict",
	)

	ErrMerchantFutureOfferingBillingPeriodInvalidState = errors.New(
		"merchant future offering billing period orchestration encountered invalid state",
	)

	// Users / Identity workflow sentinels.

	ErrUsersSignupInputInvalid = errors.New(
		"invalid users signup input",
	)

	ErrUsersActivationInitialization = errors.New(
		"user account created but activation initialization failed",
	)

	ErrUsersAuthenticationInputInvalid = errors.New(
		"invalid user authentication input",
	)

	ErrUsersActorRequired = errors.New(
		"acting user ID is required",
	)

	ErrUsersTargetRequired = errors.New(
		"target user ID is required",
	)

	ErrUsersRefreshTokenRequired = errors.New(
		"refresh token is required",
	)

	ErrUsersSessionInvalid = errors.New(
		"user session is invalid or expired",
	)

	ErrUsersSessionOwnershipMismatch = errors.New(
		"refresh token does not belong to the acting user",
	)

	ErrUsersAccountNotEligible = errors.New(
		"user account is not eligible for this operation",
	)

	ErrUsersPasswordAlreadyEstablished = errors.New(
		"user password is already established",
	)

	ErrUsersPasswordEstablishmentConflict = errors.New(
		"initial password establishment encountered a concurrent state conflict",
	)

	ErrUsersCurrentPasswordIncorrect = errors.New(
		"services: current password is incorrect",
	)

	ErrUsersPasswordConfirmationMismatch = errors.New(
		"services: new password and confirmation do not match",
	)

	ErrMerchantActorRequired = errors.New(
		"merchant service: actor user ID is required",
	)

	ErrMerchantResourceRequired = errors.New(
		"merchant service: merchant ID is required",
	)

	ErrMerchantAccessForbidden = errors.New(
		"merchant service: actor is not authorized for this merchant",
	)
)
