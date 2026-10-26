// Package main provides authenticated principal-scoped account-navigation eligibility.
// focodebase/fobackend/internal/server/cmd/api/account_navigation.go
//
// Adaptable Account Menu.
//
// GTM:
//
//	Layer: 2.2 Identity / Auth Domain
//	Release Class: SPINE
//	Reason:
//	  The shared header's account menu offers merchant and administrative
//	  workspaces alongside personal destinations. Which of those workspaces a
//	  person may open is an authorization fact, so the backend computes it
//	  from persisted state instead of the client inferring it from role
//	  names, JWT claims, or local storage.
//
// Contract:
//
//	GET /api/v1/account/navigation  (AuthMiddleware)
//
//	merchant_center.state
//	  none                      no merchant relationship
//	  registration_in_progress  a saved, incomplete registration (limited
//	                            Merchant Center; no merchant authority)
//	  active                    the principal's Merchant Account is active
//	  restricted                the principal's Merchant Account exists but is
//	                            pending, suspended, or closed
//	merchant_center.registration_available
//	  true when state is none and the account may begin registration
//	merchant_center.public_name
//	  the public DBA / brand name, present only with a Merchant Account
//	admin_console.available
//	  true when the caller holds active administrative standing and the
//	  read_admin_console permission
//
// SPINE Rule:
//
//	Keep compiling.
//	Keep production-ready.
//	Return presentation hints only; every destination re-authorizes.
//	Never return private registration facts.
//	Never record an Admin Console audit entry from this read.
//	Respond with Cache-Control: no-store.
package main

import (
	"context"
	"errors"
	"net/http"

	"github.com/PiccoloMondoC/focodebase/fobackend/internal/data"

	"github.com/google/uuid"
)

const (
	accountNavigationMerchantNone       = "none"
	accountNavigationMerchantInProgress = "registration_in_progress"
	accountNavigationMerchantActive     = "active"
	accountNavigationMerchantRestricted = "restricted"
)

type accountNavigationMerchantCenter struct {
	State                 string  `json:"state"`
	RegistrationAvailable bool    `json:"registration_available"`
	PublicName            *string `json:"public_name"`
}

type accountNavigationAdminConsole struct {
	Available bool `json:"available"`
}

type accountNavigationResponse struct {
	MerchantCenter accountNavigationMerchantCenter `json:"merchant_center"`
	AdminConsole   accountNavigationAdminConsole   `json:"admin_console"`
}

// buildAccountNavigationResponse maps persisted merchant relationship state
// and backend-resolved administrative eligibility into the menu contract.
func buildAccountNavigationResponse(
	state *data.MerchantNavigationState,
	registrationEligible bool,
	adminConsoleAvailable bool,
) accountNavigationResponse {
	response := accountNavigationResponse{
		MerchantCenter: accountNavigationMerchantCenter{
			State: accountNavigationMerchantNone,
		},
		AdminConsole: accountNavigationAdminConsole{
			Available: adminConsoleAvailable,
		},
	}

	if state == nil {
		response.MerchantCenter.RegistrationAvailable = registrationEligible
		return response
	}

	switch {
	case state.AccountStatus != nil:
		response.MerchantCenter.State = accountNavigationMerchantRestricted
		if *state.AccountStatus == data.MerchantAccountStatusActive {
			response.MerchantCenter.State = accountNavigationMerchantActive
		}
		response.MerchantCenter.PublicName = state.PublicName

	case state.RegistrationStatus != nil &&
		*state.RegistrationStatus == data.MerchantRegistrationStatusDraft:
		response.MerchantCenter.State = accountNavigationMerchantInProgress

	case state.RegistrationStatus != nil:
		// A completed registration whose Merchant Account is soft-deleted.
		response.MerchantCenter.State = accountNavigationMerchantRestricted

	default:
		response.MerchantCenter.RegistrationAvailable = registrationEligible
	}

	return response
}

// GetOwnAccountNavigationHandler returns the caller's account-menu
// destinations eligibility.
func (app *Application) GetOwnAccountNavigationHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	w.Header().Set("Cache-Control", "no-store")

	logger := app.Logger.
		GetLoggerWithContext(r).
		WithFunctionName("GetOwnAccountNavigationHandler")

	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	userID := app.getUserIDFromContext(ctx)
	if userID == nil || *userID == uuid.Nil {
		app.respondWithErrorCode(w, http.StatusUnauthorized, "authentication_required", "Please sign in.")
		return
	}

	state, registrationEligible, err := app.InternalServices.GetOwnMerchantNavigationInternal(ctx, *userID)
	if err != nil {
		switch {
		case errors.Is(err, context.Canceled):
			return
		case errors.Is(err, context.DeadlineExceeded):
			app.respondWithErrorCode(w, http.StatusGatewayTimeout, "request_timeout",
				"Account navigation took too long to load.")
		default:
			logger.Error("account navigation resolution failed", "error", err)
			app.respondWithErrorCode(w, http.StatusInternalServerError, "account_navigation_failed",
				"Account navigation couldn't be loaded.")
		}
		return
	}

	// AuthMiddleware has already refused internal-role callers whose
	// administrative standing is not active, so an internal role here means
	// active standing. The permission is resolved from persistence.
	adminConsoleAvailable := isInternalRole(getRoleFromContext(ctx)) &&
		app.HasPermission(ctx, permissionReadAdminConsole)

	app.respondWithJSON(w, http.StatusOK, jsonResponse{
		Error:   false,
		Message: "Account navigation retrieved successfully",
		Data: buildAccountNavigationResponse(
			state,
			registrationEligible,
			adminConsoleAvailable,
		),
	})
}
