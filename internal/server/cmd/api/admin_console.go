// Package main provides HTTP handlers for the privileged Admin Console
// control-plane overview.
//
// sdworkspace/sdbackend/internal/server/cmd/api/admin_console.go
//
// GTM:
//
//	Layer: 3.1 API / Administrative Control Plane
//	Release Class: SPINE
//	Reason:
//	  Admin Console is the privileged administrative entry surface for
//	  release-critical platform governance and completed SPINE domains.
//
//	  This file provides a bounded control-plane overview by composing
//	  existing domain state through canonical models and shared API
//	  infrastructure. It does not own business persistence, lifecycle rules,
//	  commercial calculations, release classification, route registration,
//	  permissions, or domain mutation behavior.
//
//	  Platform Settings, Platform Setting History, Merchant Accounts,
//	  Merchant Billing Accounts, Merchant Billable Events, Merchant Program
//	  Plans, Merchant Program Entitlements, Merchant Program Fee Schedules,
//	  Merchant Program Subscriptions, Merchant Program Subscription Periods,
//	  Merchant Platform Credit Accounts, Merchant Fee Calculations,
//	  Merchant Platform Credit Applications, Merchant Invoices,
//	  Merchant Invoice Items, Merchant Platform Credit Eligible Fee Types,
//	  and Merchant Payment Methods remain owned by their respective handler,
//	  data, and service contracts.
//
//	  platform_settings_admin_enabled governs Platform Settings
//	  administration only. It is reported by Admin Console but does not
//	  enable or disable the entire administrative control plane.
//
// SPINE Rule:
//
//	Keep compiling.
//	Keep production-ready.
//	Preserve privileged authentication and authorization at every boundary.
//	Preserve bounded and deterministic administrative reads.
//	Preserve handler, data, service, and startup separation.
//	Preserve existing domain ownership and lifecycle authority.
//	Preserve audit accountability through centralized governance helpers.
//	Preserve platform-setting value opacity in responses, logs, traces,
//	metrics, audit entity IDs, and errors.
//	Do not create an Admin Console table, data model, or service.
//	Do not make Admin Console a second route, permission, capability, or
//	release-classification registry.
//	Do not use platform_settings_admin_enabled as a global Admin Console
//	enablement flag.
//	Do not expose public Admin Console routes.
//	Do not introduce unfinished or DEFERRED domain operations.
//	Block deployment if this file breaks build, privileged access control,
//	configuration confidentiality, bounded administrative reads, audit
//	accountability, or domain ownership.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
)

const (
	adminConsoleEntityType = "admin_console"

	adminConsoleEntityTypeDescription = "Privileged Admin Console control-plane access surface"

	actionReadAdminConsole = "read_admin_console"

	permissionReadAdminConsole = "read_admin_console"

	adminConsoleFutureOfferingEnabledKey = "future_offering_enabled"

	adminConsolePlatformSettingsAdminEnabledKey = "platform_settings_admin_enabled"
)

// adminConsolePlatformStatus reports selected canonical platform controls
// without exposing their raw platform-setting records or JSON values.
type adminConsolePlatformStatus struct {
	FutureOfferingEnabled bool `json:"future_offering_enabled"`

	PlatformSettingsAdministrationEnabled bool `json:"platform_settings_administration_enabled"`

	PlatformSettingsHardDeleteEnabled bool `json:"platform_settings_hard_delete_enabled"`
}

// adminConsoleDomains reports the completed SPINE domains currently included
// in the administrative control plane.
//
// These fields identify control-plane inclusion. They do not claim that every
// possible operation within the domain is enabled for the requesting actor.
type adminConsoleDomains struct {
	PlatformSettings                       bool `json:"platform_settings"`
	PlatformSettingHistory                 bool `json:"platform_setting_history"`
	MerchantAccounts                       bool `json:"merchant_accounts"`
	MerchantProgramPlans                   bool `json:"merchant_program_plans"`
	MerchantProgramEntitlements            bool `json:"merchant_program_entitlements"`
	MerchantProgramFeeSchedules            bool `json:"merchant_program_fee_schedules"`
	MerchantFutureOfferingServiceTerms     bool `json:"merchant_future_offering_service_terms"`
	MerchantFutureOfferingServicePeriods   bool `json:"merchant_future_offering_service_periods"`
	MerchantFutureOfferingBillingPeriods   bool `json:"merchant_future_offering_billing_periods"`
	MerchantProgramSubscriptions           bool `json:"merchant_program_subscriptions"`
	MerchantProgramSubscriptionPeriods     bool `json:"merchant_program_subscription_periods"`
	MerchantPlatformCreditAccounts         bool `json:"merchant_platform_credit_accounts"`
	MerchantPlatformCreditEligibleFeeTypes bool `json:"merchant_platform_credit_eligible_fee_types"`
	MerchantBillingAccounts                bool `json:"merchant_billing_accounts"`
	MerchantBillableEvents                 bool `json:"merchant_billable_events"`
	MerchantFeeCalculations                bool `json:"merchant_fee_calculations"`
	MerchantPlatformCreditApplications     bool `json:"merchant_platform_credit_applications"`
	MerchantInvoices                       bool `json:"merchant_invoices"`
	MerchantInvoiceItems                   bool `json:"merchant_invoice_items"`
	MerchantPaymentMethods                 bool `json:"merchant_payment_methods"`
}

// adminConsoleOverviewResponse is the stable presentation DTO returned by the
// Admin Console overview handler.
type adminConsoleOverviewResponse struct {
	Platform adminConsolePlatformStatus `json:"platform"`
	Domains  adminConsoleDomains        `json:"domains"`
}

// readAdminConsoleBooleanSetting reads one active canonical platform setting
// whose JSON value must contain exactly one boolean.
//
// A missing or inactive setting evaluates to false. Database and malformed
// configuration failures are returned to the caller. Raw setting values are
// never returned or included in errors.
func (app *Application) readAdminConsoleBooleanSetting(
	ctx context.Context,
	settingKey string,
) (bool, error) {
	setting, err :=
		app.Models.PlatformSetting.GetActiveByKey(
			ctx,
			settingKey,
		)
	if err != nil {
		return false, fmt.Errorf(
			"read platform control %q: %w",
			settingKey,
			err,
		)
	}

	if setting == nil {
		return false, nil
	}

	decoder := json.NewDecoder(
		bytes.NewReader(setting.SettingValue),
	)

	var enabled bool

	if err := decoder.Decode(&enabled); err != nil {
		return false, fmt.Errorf(
			"decode platform control %q: invalid boolean value",
			settingKey,
		)
	}

	var trailing any

	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return false, fmt.Errorf(
			"decode platform control %q: invalid trailing JSON data",
			settingKey,
		)
	}

	return enabled, nil
}

// GetAdminConsoleOverviewHandler returns the bounded privileged overview of
// the current administrative control plane.
//
// The handler reports selected platform controls and the completed SPINE
// domains included in Admin Console. It does not perform domain-wide scans,
// calculate operational counts, duplicate route permissions, or expose raw
// platform-setting records.
func (app *Application) GetAdminConsoleOverviewHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	logger := app.Logger.
		GetLoggerWithContext(r).
		WithFunctionName(
			"GetAdminConsoleOverviewHandler",
		)

	ctx, cancel := context.WithTimeout(
		r.Context(),
		cfgTimeout,
	)
	defer cancel()

	if !app.HasPermission(
		ctx,
		permissionReadAdminConsole,
	) {
		app.respondWithError(
			w,
			errors.New(
				"forbidden: insufficient permissions",
			),
			http.StatusForbidden,
		)
		return
	}

	userID := app.getUserIDFromContext(ctx)
	if userID == nil {
		app.respondWithError(
			w,
			errors.New(
				"user ID not found in context",
			),
			http.StatusUnauthorized,
		)
		return
	}

	futureOfferingEnabled, err :=
		app.readAdminConsoleBooleanSetting(
			ctx,
			adminConsoleFutureOfferingEnabledKey,
		)
	if err != nil {
		logger.Error(
			"Read Future Offering platform control failed",
			"setting_key",
			adminConsoleFutureOfferingEnabledKey,
			"error",
			err,
		)

		app.respondWithError(
			w,
			errors.New(
				"failed to retrieve admin console overview",
			),
			http.StatusInternalServerError,
		)
		return
	}

	platformSettingsAdminEnabled, err :=
		app.readAdminConsoleBooleanSetting(
			ctx,
			adminConsolePlatformSettingsAdminEnabledKey,
		)
	if err != nil {
		logger.Error(
			"Read Platform Settings administration control failed",
			"setting_key",
			adminConsolePlatformSettingsAdminEnabledKey,
			"error",
			err,
		)

		app.respondWithError(
			w,
			errors.New(
				"failed to retrieve admin console overview",
			),
			http.StatusInternalServerError,
		)
		return
	}

	hardDeleteEnabled, err :=
		app.platformSettingsHardDeleteEnabled(ctx)
	if err != nil {
		logger.Error(
			"Read Platform Settings hard-delete control failed",
			"setting_key",
			platformSettingsHardDeleteEnabledKey,
			"error",
			err,
		)

		app.respondWithError(
			w,
			errors.New(
				"failed to retrieve admin console overview",
			),
			http.StatusInternalServerError,
		)
		return
	}

	overview := adminConsoleOverviewResponse{
		Platform: adminConsolePlatformStatus{
			FutureOfferingEnabled: futureOfferingEnabled,

			PlatformSettingsAdministrationEnabled: platformSettingsAdminEnabled,

			PlatformSettingsHardDeleteEnabled: hardDeleteEnabled,
		},
		Domains: adminConsoleDomains{
			PlatformSettings:                       true,
			PlatformSettingHistory:                 true,
			MerchantAccounts:                       true,
			MerchantBillingAccounts:                true,
			MerchantProgramPlans:                   true,
			MerchantProgramEntitlements:            true,
			MerchantProgramFeeSchedules:            true,
			MerchantFutureOfferingServiceTerms:     true,
			MerchantFutureOfferingServicePeriods:   true,
			MerchantFutureOfferingBillingPeriods:   true,
			MerchantProgramSubscriptions:           true,
			MerchantProgramSubscriptionPeriods:     true,
			MerchantPlatformCreditAccounts:         true,
			MerchantPlatformCreditEligibleFeeTypes: true,
			MerchantBillableEvents:                 true,
			MerchantFeeCalculations:                true,
			MerchantPlatformCreditApplications:     true,
			MerchantInvoices:                       true,
			MerchantInvoiceItems:                   true,
			MerchantPaymentMethods:                 true,
		},
	}

	if err := app.insertGovernanceAudit(
		ctx,
		userID,
		actionReadAdminConsole,
		"Read the Admin Console control-plane overview",
		adminConsoleEntityType,
		adminConsoleEntityTypeDescription,
		"overview",
	); err != nil {
		logger.Warn(
			"Admin Console overview retrieved but audit recording failed",
			"user_id",
			userID,
			"error",
			err,
		)
	}

	logger.Info(
		"Admin Console overview retrieved",
		"user_id",
		userID,
	)

	app.respondWithJSON(
		w,
		http.StatusOK,
		jsonResponse{
			Error: false,
			Message: "Admin Console overview retrieved " +
				"successfully",
			Data: overview,
		},
	)
}
