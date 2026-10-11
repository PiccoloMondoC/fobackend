// Package main provides the HTTP handler that completes installation
// bootstrap.
//
// focodebase/fobackend/internal/server/cmd/api/installation.go
//
// GTM:
//
//	Layer: 3.1 API / Installation
//	Release Class: SPINE
//	Reason:
//	  Public, rate-limited exchange of the one-time Root bootstrap credential
//	  (issued by cmd/install to the address the installer named) for the
//	  claimant's account password and incumbency. The credential is the
//	  proof of control of the address. Once an installation exists the
//	  database refuses every bootstrap path, so this endpoint confers nothing
//	  afterwards; every failure is one indistinguishable response.
//
// SPINE Rule:
//
//	Keep compiling.
//	Keep production-ready.
//	Never log, echo, or audit the credential or the password.
//	Never distinguish unknown, expired, used, superseded credentials, or an
//	initialized installation, in responses.
//	Block deployment if this file breaks build or bootstrap safety.
package main

import (
	"context"
	"errors"
	"net/http"

	"github.com/PiccoloMondoC/focodebase/fobackend/internal/data"
	"github.com/PiccoloMondoC/focodebase/fobackend/internal/security"
	"github.com/PiccoloMondoC/focodebase/fobackend/internal/services"
)

type completeRootBootstrapInput struct {
	Credential string `json:"credential"`
	Password   string `json:"password"`
}

// CompleteRootBootstrapHandler completes installation bootstrap.
//
//	POST /api/v1/installation/root-bootstrap/complete
//	{ "credential": "...", "password": "..." }
func (app *Application) CompleteRootBootstrapHandler(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	logger := app.Logger.GetLoggerWithContext(r).WithFunctionName("CompleteRootBootstrapHandler")

	var input completeRootBootstrapInput
	if err := app.readJSON(w, r, &input); err != nil {
		app.respondWithErrorCode(w, http.StatusBadRequest, "invalid_request", "invalid request body")
		return
	}

	installation, err := app.InternalServices.CompleteRootBootstrapInternal(ctx, input.Credential, input.Password)
	input.Credential = ""
	input.Password = ""
	if err != nil {
		switch {
		case errors.Is(err, services.ErrBootstrapCredentialInvalid):
			logger.Warn("root bootstrap completion refused", "code", "bootstrap_credential_invalid")
			app.respondWithErrorCode(w, http.StatusBadRequest, "bootstrap_credential_invalid",
				"this installation link is invalid, expired, or already used")
		case errors.Is(err, security.ErrEmptyPassword), errors.Is(err, security.ErrPasswordLength):
			app.respondWithErrorCode(w, http.StatusBadRequest, "password_policy",
				"password does not meet minimum security requirements")
		case errors.Is(err, services.ErrBootstrapReauthenticationFailed):
			app.respondWithErrorCode(w, http.StatusBadRequest, "existing_password_incorrect", "enter the current password for your existing account")
		case errors.Is(err, services.ErrBootstrapAccountUnavailable):
			logger.Warn("root bootstrap completion refused", "code", "bootstrap_account_unavailable")
			app.respondWithErrorCode(w, http.StatusConflict, "bootstrap_account_unavailable",
				"this account cannot be enrolled through installation; contact the installer")
		case errors.Is(err, data.ErrConcurrentGovernanceChange):
			app.respondWithErrorCode(w, http.StatusConflict, "concurrent_change",
				"installation state changed while this request was processed; try again")
		default:
			logger.Error("root bootstrap completion failed", "code", "server_error")
			app.respondWithErrorCode(w, http.StatusInternalServerError, "server_error", "internal server error")
		}
		return
	}

	logger.Warn("installation initialized; Root Super Admin established",
		"installation_id", installation.InstallationID,
		"user_id", installation.OriginalBootstrapUserID)

	app.respondWithJSON(w, http.StatusCreated, jsonResponse{
		Error:   false,
		Message: "Installation complete. Sign in with your email and password.",
		Data: struct {
			InstallationID string `json:"installation_id"`
		}{InstallationID: installation.InstallationID.String()},
	})
}
