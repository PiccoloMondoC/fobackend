// Package data defines shared sentinel errors and PostgreSQL error helpers.
//
// sdworkspace/sdbackend/internal/data/errors.go
//
// GTM:
//
//	Layer: 2.1 Database / Governance Foundation
//	Release Class: SPINE
//	Reason:
//	  Centralized sentinel errors and PostgreSQL error helpers are release-critical
//	  foundation infrastructure. This file preserves consistent domain error
//	  translation, avoids local duplicate sentinel definitions, supports
//	  constraint-aware persistence handling, and keeps model/handler error
//	  contracts stable across SPINE and DEFERRED domains.
//
// SPINE Rule:
//
//	Keep compiling.
//	Keep production-ready.
//	Preserve centralized sentinel ownership.
//	Preserve PostgreSQL SQLSTATE helper behavior.
//	Preserve domain-specific typed error contracts.
//	Block deployment if this file breaks build, error translation,
//	constraint detection, or cross-model error contract integrity.
package data

import (
	"errors"

	"github.com/jackc/pgx/v5/pgconn"
)

const (
	// SQLSTATE 23505: unique_violation.
	sqlStateUniqueViolation = "23505"

	// SQLSTATE 23503: foreign_key_violation.
	sqlStateForeignKeyViolation = "23503"

	// SQLSTATE 23502: not_null_violation.
	sqlStateNotNullViolation = "23502"

	// SQLSTATE 23514: check_violation.
	sqlStateCheckViolation = "23514"

	// SQLSTATE 23P01: exclusion_violation.
	sqlStateExclusionViolation = "23P01"
)

// Persistence and domain sentinel errors.
//
// These exported sentinels define stable model-facing and persistence-facing
// error contracts for the data package. Low-level security primitives must not
// be added here merely for convenience; JWT, password, random-token, and other
// cryptographic validation errors belong in their owning security packages.
//
// Boundary layers may translate lower-level security failures into safe public
// domain errors such as ErrInvalidCredentials, but security packages must not
// depend on this package for primitive sentinel ownership.
var (
	// Generic errors.
	ErrDuplicate      = errors.New("duplicate record")
	ErrRecordNotFound = errors.New("record not found")
	ErrEditConflict   = errors.New("edit conflict")

	// Duplicate errors.
	ErrDuplicateEmail    = errors.New("duplicate email")
	ErrDuplicateClientID = errors.New("duplicate client_id")

	// Platform settings.
	ErrPlatformSettingNotFound      = errors.New("platform setting not found")
	ErrPlatformSettingAlreadyExists = errors.New("platform setting already exists")
	ErrPlatformSettingActorNotFound = errors.New("platform setting actor not found")
	ErrPlatformSettingTypeMismatch  = errors.New("platform setting value type mismatch")

	// Users and identity.
	ErrUserNotFound         = errors.New("user not found")
	ErrUserProfileNotFound  = errors.New("user profile not found")
	ErrUserHandleNotFound   = errors.New("user handle not found")
	ErrUserHandleTaken      = errors.New("user handle already taken")
	ErrGlobalHandleNotFound = errors.New("global handle not found")
	ErrGlobalHandleTaken    = errors.New("global handle already taken")
	ErrUserSettingsNotFound = errors.New("user settings not found")
	ErrUserFavoriteNotFound = errors.New("user favorite not found")
	ErrUserWishlistNotFound = errors.New("user wishlist item not found")

	// Authentication and token persistence.
	//
	// ErrInvalidCredentials is a boundary-safe domain error. It may be returned
	// after translating lower-level password, JWT, or token verification failures,
	// but low-level security packages must own their own primitive sentinels.
	ErrInvalidCredentials = errors.New("invalid credentials")

	ErrJWTSecretNotConfigured = errors.New("jwt secret not configured")

	ErrResetTokenInvalid    = errors.New("reset token invalid")
	ErrInvalidResetToken    = ErrResetTokenInvalid
	ErrResetTokenExpired    = errors.New("reset token expired")
	ErrRefreshTokenNotFound = errors.New("refresh token not found")
	ErrRefreshTokenInvalid  = errors.New("refresh token invalid")
	ErrRefreshTokenExpired  = errors.New("refresh token expired")
	ErrRefreshTokenRevoked  = errors.New("refresh token revoked")

	// ErrAccessTokenRevoked is data-layer-owned only when produced by the
	// persisted access-token blacklist/revocation query path.
	ErrAccessTokenRevoked = errors.New("access token revoked")
	ErrAccessTokenInvalid = errors.New("access token invalid")

	// ErrAlreadyRevoked reports an idempotent lifecycle condition where the
	// persistence row already records revocation. Callers should treat this as a
	// lifecycle outcome, not as proof of token validity.
	ErrAlreadyRevoked = errors.New("record already revoked")

	// User activation.
	ErrActivationTokenInvalid      = errors.New("invalid activation token")
	ErrActivationTokenExpired      = errors.New("activation token expired")
	ErrActivationTokenRequired     = errors.New("activation token is required")
	ErrActivationTokenNotFound     = errors.New("activation token not found")
	ErrActivationTokenDeleteFailed = errors.New("failed to delete activation token")

	// Roles and permissions.
	ErrPermissionNotFound            = errors.New("permission not found")
	ErrRoleNotFound                  = errors.New("role not found")
	ErrRolePermissionNotFound        = errors.New("role permission not found")
	ErrRolePermissionAlreadyAssigned = errors.New("role permission already assigned")

	// Audit.
	ErrAuditLogNotFound         = errors.New("audit log not found")
	ErrArchivedAuditLogNotFound = errors.New("archived audit log not found")
	ErrEntityTypeNotFound       = errors.New("entity type not found")
	ErrActionNotFound           = errors.New("action not found")

	// Departments and categories.
	ErrDepartmentNotFound = errors.New("department not found")
	ErrCategoryNotFound   = errors.New("category not found")

	// Brands, merchants, and products.
	ErrBrandNotFound                            = errors.New("brand not found")
	ErrMerchantNotFound                         = errors.New("merchant not found")
	ErrProductNotFound                          = errors.New("product not found")
	ErrMerchantProductNotFound                  = errors.New("merchant product not found")
	ErrMerchantApplicationNotFound              = errors.New("merchant application not found")
	ErrMerchantApplicationStatusNotFound        = errors.New("merchant application status not found")
	ErrMerchantApplicationStatusAlreadyInactive = errors.New("merchant application status already inactive")
	ErrMerchantPromotionNotFound                = errors.New("merchant promotion not found")
	ErrMerchantFollowNotFound                   = errors.New("merchant follow not found")

	// Merchant future offering service terms.
	ErrMerchantFutureOfferingServiceTermInvalidInput = errors.New(
		"invalid merchant future offering service term input",
	)
	ErrMerchantFutureOfferingServiceTermNotFound = errors.New(
		"merchant future offering service term not found",
	)
	ErrMerchantFutureOfferingServiceTermFutureOfferingNotFound = errors.New(
		"merchant future offering service term references a nonexistent future offering",
	)
	ErrMerchantFutureOfferingServiceTermProposedAlreadyExists = errors.New(
		"a proposed merchant future offering service term already exists for this future offering",
	)
	ErrMerchantFutureOfferingServiceTermEstablishedAlreadyExists = errors.New(
		"an established merchant future offering service term already exists for this future offering",
	)
	ErrMerchantFutureOfferingServiceTermInvalidState = errors.New(
		"invalid merchant future offering service term state",
	)
	ErrMerchantFutureOfferingServiceTermInvalidTransition = errors.New(
		"invalid merchant future offering service term lifecycle transition",
	)
	ErrMerchantFutureOfferingServiceTermReplacementConflict = errors.New(
		"merchant future offering service term replacement conflict",
	)

	// Merchant future offering service periods.
	ErrMerchantFutureOfferingServicePeriodInvalidInput = errors.New(
		"invalid merchant future offering service period input",
	)

	ErrMerchantFutureOfferingServicePeriodNotFound = errors.New(
		"merchant future offering service period not found",
	)

	ErrMerchantFutureOfferingServicePeriodServiceTermNotFound = errors.New(
		"merchant future offering service period references a nonexistent or mismatched service term",
	)

	ErrMerchantFutureOfferingServicePeriodInvalidState = errors.New(
		"invalid merchant future offering service period state",
	)

	ErrMerchantFutureOfferingServicePeriodInvalidSchedule = errors.New(
		"invalid merchant future offering service period schedule",
	)

	ErrMerchantFutureOfferingServicePeriodScheduleAlreadyExists = errors.New(
		"merchant future offering service period schedule already exists",
	)

	ErrMerchantFutureOfferingServicePeriodOverlap = errors.New(
		"merchant future offering service period overlaps an existing current period",
	)

	ErrMerchantFutureOfferingServicePeriodNumberConflict = errors.New(
		"merchant future offering service period number conflicts with an existing current period",
	)

	ErrMerchantFutureOfferingServicePeriodInvalidTransition = errors.New(
		"invalid merchant future offering service period lifecycle transition",
	)

	ErrMerchantFutureOfferingServicePeriodMutationConflict = errors.New(
		"merchant future offering service period mutation conflict",
	)

	// Merchant future offering billing periods.
	ErrMerchantFutureOfferingBillingPeriodInvalidInput = errors.New(
		"invalid merchant future offering billing period input",
	)

	ErrMerchantFutureOfferingBillingPeriodNotFound = errors.New(
		"merchant future offering billing period not found",
	)

	ErrMerchantFutureOfferingBillingPeriodServiceTermNotFound = errors.New(
		"merchant future offering billing period references a nonexistent or mismatched service term",
	)

	ErrMerchantFutureOfferingBillingPeriodInvalidState = errors.New(
		"invalid merchant future offering billing period state",
	)

	ErrMerchantFutureOfferingBillingPeriodInvalidSchedule = errors.New(
		"invalid merchant future offering billing period schedule",
	)

	ErrMerchantFutureOfferingBillingPeriodOverlap = errors.New(
		"merchant future offering billing period overlaps an existing period",
	)

	ErrMerchantFutureOfferingBillingPeriodNumberConflict = errors.New(
		"merchant future offering billing period number conflicts with an existing period",
	)

	ErrMerchantFutureOfferingBillingPeriodTermExhausted = errors.New(
		"merchant future offering billing period service term coverage is complete",
	)

	ErrMerchantFutureOfferingBillingPeriodNotYetDue = errors.New(
		"merchant future offering billing period is not yet due to begin",
	)

	ErrMerchantFutureOfferingBillingPeriodPersistenceConflict = errors.New(
		"merchant future offering billing period persistence conflict",
	)

	// Merchant platform credit accounts.
	ErrMerchantPlatformCreditAccountNotFound = errors.New(
		"merchant platform credit account not found",
	)
	ErrMerchantPlatformCreditAccountInvalidState = errors.New(
		"invalid merchant platform credit account state",
	)
	ErrMerchantPlatformCreditAccountInvalidTransition = errors.New(
		"invalid merchant platform credit account lifecycle transition",
	)
	ErrMerchantPlatformCreditAccountNotUsable = errors.New(
		"merchant platform credit account is not currently usable",
	)
	ErrMerchantPlatformCreditAccountInsufficientBalance = errors.New(
		"merchant platform credit account has insufficient remaining balance",
	)
	ErrMerchantPlatformCreditAccountCurrencyMismatch = errors.New(
		"merchant platform credit account currency mismatch",
	)
	ErrMerchantPlatformCreditAccountMutationConflict = errors.New(
		"merchant platform credit account mutation conflict",
	)
	ErrMerchantPlatformCreditAccountNotConsumable = errors.New(
		"merchant platform credit account is not consumable",
	)

	// Merchant platform credit eligible fee types.
	ErrMerchantPlatformCreditEligibleFeeTypeInvalidInput = errors.New(
		"invalid merchant platform credit eligible fee type input",
	)
	ErrMerchantPlatformCreditEligibleFeeTypeAlreadyExists = errors.New(
		"merchant platform credit eligible fee type already exists",
	)
	ErrMerchantPlatformCreditEligibleFeeTypeNotFound = errors.New(
		"merchant platform credit eligible fee type not found",
	)

	// Merchant billing accounts.
	ErrMerchantBillingAccountAlreadyExists = errors.New(
		"merchant billing account already exists",
	)
	ErrMerchantBillingAccountNotFound = errors.New(
		"merchant billing account not found",
	)
	ErrMerchantBillingAccountInvalidState = errors.New(
		"invalid merchant billing account state",
	)
	ErrMerchantBillingAccountInvalidTransition = errors.New(
		"invalid merchant billing account lifecycle transition",
	)
	ErrMerchantBillingAccountMutationConflict = errors.New(
		"merchant billing account mutation conflict",
	)

	// Merchant billable events.
	ErrMerchantBillableEventInvalidInput = errors.New(
		"invalid merchant billable event input",
	)
	ErrMerchantBillableEventNotFound = errors.New(
		"merchant billable event not found",
	)
	ErrMerchantBillableEventInvalidState = errors.New(
		"invalid merchant billable event state",
	)
	ErrMerchantBillableEventDuplicateSource = errors.New(
		"merchant billable event already exists for this source occurrence",
	)
	ErrMerchantBillableEventMerchantNotFound = errors.New(
		"merchant billable event references a nonexistent merchant",
	)
	ErrMerchantBillableEventFutureOfferingEventNotFound = errors.New(
		"merchant billable event references a nonexistent future offering event",
	)
	ErrMerchantBillableEventEngagementEventNotFound = errors.New(
		"merchant billable event references a nonexistent engagement event",
	)
	ErrMerchantBillableEventBillingPeriodNotFound = errors.New(
		"merchant billable event references a nonexistent billing period",
	)
	ErrMerchantBillableEventInvalidTransition = errors.New(
		"invalid merchant billable event lifecycle transition",
	)

	// Merchant fee calculations.
	ErrMerchantFeeCalculationInvalidInput = errors.New(
		"invalid merchant fee calculation input",
	)
	ErrMerchantFeeCalculationNotFound = errors.New(
		"merchant fee calculation not found",
	)
	ErrMerchantFeeCalculationInvalidState = errors.New(
		"invalid merchant fee calculation state",
	)
	ErrMerchantFeeCalculationDuplicate = errors.New(
		"an active merchant fee calculation already exists for this billable event and fee type",
	)
	ErrMerchantFeeCalculationMerchantNotFound = errors.New(
		"merchant fee calculation references a nonexistent merchant",
	)
	ErrMerchantFeeCalculationBillableEventNotFound = errors.New(
		"merchant fee calculation references a nonexistent billable event",
	)
	ErrMerchantFeeCalculationFeeScheduleNotFound = errors.New(
		"merchant fee calculation references a nonexistent fee schedule",
	)
	ErrMerchantFeeCalculationFeeTypeNotFound = errors.New(
		"merchant fee calculation references a nonexistent fee type",
	)
	ErrMerchantFeeCalculationInvalidTransition = errors.New(
		"invalid merchant fee calculation lifecycle transition",
	)

	// Merchant platform credit applications.
	ErrMerchantPlatformCreditApplicationInvalidInput = errors.New(
		"invalid merchant platform credit application input",
	)
	ErrMerchantPlatformCreditApplicationDuplicate = errors.New(
		"merchant platform credit application already exists for this credit account and fee calculation",
	)
	ErrMerchantPlatformCreditApplicationCreditAccountNotFound = errors.New(
		"merchant platform credit application references a nonexistent credit account",
	)
	ErrMerchantPlatformCreditApplicationFeeCalculationNotFound = errors.New(
		"merchant platform credit application references a nonexistent fee calculation",
	)
	ErrMerchantPlatformCreditApplicationInvalidState = errors.New(
		"merchant platform credit application violates a persisted integrity constraint",
	)

	// Merchant invoices.
	ErrMerchantInvoiceInvalidInput = errors.New(
		"invalid merchant invoice input",
	)
	ErrMerchantInvoiceNotFound = errors.New(
		"merchant invoice not found",
	)
	ErrMerchantInvoiceFutureOfferingNotFound = errors.New(
		"merchant invoice references a nonexistent future offering",
	)
	ErrMerchantInvoiceInvalidState = errors.New(
		"invalid merchant invoice state",
	)
	ErrMerchantInvoiceDuplicateNumber = errors.New(
		"merchant invoice number already exists",
	)
	ErrMerchantInvoiceMerchantNotFound = errors.New(
		"merchant invoice references a nonexistent merchant",
	)
	ErrMerchantInvoiceInvalidTransition = errors.New(
		"invalid merchant invoice lifecycle transition",
	)
	ErrMerchantInvoicePaymentExceedsBalance = errors.New(
		"merchant invoice payment exceeds remaining balance",
	)
	ErrMerchantInvoiceFutureOfferingMerchantMismatch = errors.New(
		"merchant invoice future offering does not belong to merchant",
	)

	// Merchant invoice items.
	ErrMerchantInvoiceItemInvalidInput = errors.New(
		"invalid merchant invoice item input",
	)
	ErrMerchantInvoiceItemNotFound = errors.New(
		"merchant invoice item not found",
	)
	ErrMerchantInvoiceItemInvalidState = errors.New(
		"invalid merchant invoice item state",
	)
	ErrMerchantInvoiceItemInvoiceNotFound = errors.New(
		"merchant invoice item references a nonexistent invoice",
	)
	ErrMerchantInvoiceItemFeeCalculationNotFound = errors.New(
		"merchant invoice item references a nonexistent fee calculation",
	)
	ErrMerchantInvoiceItemDuplicateFeeCalculation = errors.New(
		"merchant invoice item already exists for this fee calculation",
	)
	ErrMerchantInvoiceItemInvoiceNotDraft = errors.New(
		"merchant invoice item requires the parent invoice to be in draft status",
	)
	ErrMerchantInvoiceItemProvenanceMismatch = errors.New(
	"merchant invoice item fee calculation does not match the parent invoice merchant, currency, or future offering",
	)

	// Merchant payment methods.
	ErrMerchantPaymentMethodNotFound        = errors.New("merchant payment method not found")
	ErrMerchantPaymentMethodAlreadyExists   = errors.New("merchant payment method already exists")
	ErrMerchantPaymentMethodInvalidState    = errors.New("invalid merchant payment method state")
	ErrMerchantPaymentMethodDefaultConflict = errors.New("merchant payment method default assignment conflict")

	// Affiliate programs and performance.
	ErrAffiliatePerformanceNotFound   = errors.New("affiliate performance not found")
	ErrAffiliateProgramNotFound       = errors.New("affiliate program not found")
	ErrAffiliateProgramAlreadyExists  = errors.New("affiliate program already exists")
	ErrAffiliateProgramSecretNotFound = errors.New("affiliate program secret material not found")

	// Coupons.
	ErrCouponNotFound         = errors.New("coupon not found")
	ErrCouponAlreadyExists    = errors.New("coupon already exists")
	ErrCouponStatusNotFound   = errors.New("coupon status not found")
	ErrCouponOfferRequired    = errors.New("offer_id is required")
	ErrCouponCodeRequired     = errors.New("code is required")
	ErrCouponClipNotSupported = errors.New("coupon cannot be clipped because it is not linked to an offer")

	// Offers.
	ErrOfferClickNotFound             = errors.New("offer click not found")
	ErrOfferClickImmutable            = errors.New("offer click records are immutable")
	ErrOfferConversionNotFound        = errors.New("offer conversion not found")
	ErrOfferConversionImmutable       = errors.New("offer conversion records are immutable")
	ErrOfferFlagNotFound              = errors.New("offer flag not found")
	ErrOfferFlagAlreadyResolved       = errors.New("offer flag already resolved")
	ErrOfferPriceHistoryNotFound      = errors.New("offer price history not found")
	ErrOfferPriceHistoryImmutable     = errors.New("offer price history records are immutable")
	ErrPriceDropSubscriptionNotFound  = errors.New("price drop subscription not found")
	ErrOfferRatingNotFound            = errors.New("offer rating not found")
	ErrOfferReviewNotFound            = errors.New("offer not found or not eligible for review transition")
	ErrOfferSponsorshipNotFound       = errors.New("offer sponsorship not found")
	ErrOfferStatusNotFound            = errors.New("offer status not found")
	ErrOfferStatusNameRequired        = errors.New("offer status name is required")
	ErrOfferStatusDescriptionRequired = errors.New("offer status description is required")
	ErrOfferStatusIDRequired          = errors.New("offer status id is required")
	ErrOfferIDRequired                = errors.New("offer id is required")
	ErrMerchantIDRequired             = errors.New("merchant id is required")
	ErrPendingReviewStatusMissing     = errors.New("required offer status 'pending_review' is missing")
	ErrOfferNotFoundForMerchant       = errors.New("offer not found for merchant")

	// Notifications.
	ErrUserNotificationNotFound    = errors.New("user notification not found")
	ErrUserNotificationImmutable   = errors.New("user notification records are immutable")
	ErrNotificationTypeNotFound    = errors.New("notification type not found")
	ErrNotificationChannelNotFound = errors.New("notification channel not found")

	// Wallets.
	ErrWalletNotFound      = errors.New("wallet not found")
	ErrLedgerEntryNotFound = errors.New("wallet ledger entry not found")
	ErrInvalidWalletState  = errors.New("invalid wallet state")
	ErrInvalidLedgerState  = errors.New("invalid wallet ledger state")
)

// IsUniqueViolation reports whether err wraps a PostgreSQL unique-constraint violation.
func IsUniqueViolation(err error) bool {
	return IsPgErrorCode(err, sqlStateUniqueViolation)
}

// IsForeignKeyViolation reports whether err wraps a PostgreSQL foreign-key violation.
func IsForeignKeyViolation(err error) bool {
	return IsPgErrorCode(err, sqlStateForeignKeyViolation)
}

// IsNotNullViolation reports whether err wraps a PostgreSQL NOT NULL violation.
func IsNotNullViolation(err error) bool {
	return IsPgErrorCode(err, sqlStateNotNullViolation)
}

// IsCheckViolation reports whether err wraps a PostgreSQL CHECK-constraint violation.
func IsCheckViolation(err error) bool {
	return IsPgErrorCode(err, sqlStateCheckViolation)
}

// IsExclusionViolation reports whether err wraps a PostgreSQL
// exclusion-constraint violation.
func IsExclusionViolation(err error) bool {
	return IsPgErrorCode(err, sqlStateExclusionViolation)
}

// IsPgErrorCode reports whether err wraps a pgconn.PgError with the supplied SQLSTATE code.
func IsPgErrorCode(err error, code string) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == code
}

// PgErrorConstraintName returns the PostgreSQL constraint name when available.
func PgErrorConstraintName(err error) string {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.ConstraintName
	}
	return ""
}

// IsPgConstraint reports whether err came from the named PostgreSQL constraint.
// It returns false when err does not wrap a pgconn.PgError or when constraintName is blank.
func IsPgConstraint(err error, constraintName string) bool {
	if constraintName == "" {
		return false
	}

	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.ConstraintName == constraintName
}
