// Package main provides the HTTP boundary for merchant self-service onboarding.
//
// focodebase/fobackend/internal/server/cmd/api/merchant_onboarding.go
package main

import (
	"context"
	"errors"
	"net/http"

	"github.com/PiccoloMondoC/focodebase/fobackend/internal/data"
	"github.com/PiccoloMondoC/focodebase/fobackend/internal/services"

	"github.com/google/uuid"
)

// onboardMerchantInput is the v1 self-service onboarding request. It carries
// only identity facts owned by canonical Merchant persistence.
type onboardMerchantInput struct {
	Name    string  `json:"name"`
	LogoURL *string `json:"logo_url,omitempty"`
	Website *string `json:"website,omitempty"`
}

type onboardMerchantResponse struct {
	Merchant        *data.Merchant        `json:"merchant"`
	MerchantAccount *data.MerchantAccount `json:"merchant_account"`
}

// OnboardMerchantHandler establishes the authenticated merchant user as the
// principal of a newly created Merchant and v1 Merchant Account. Actor
// identity comes exclusively from trusted authentication context.
func (app *Application) OnboardMerchantHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	logger := app.Logger.
		GetLoggerWithContext(r).
		WithFunctionName("OnboardMerchantHandler")

	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	userID := app.getUserIDFromContext(ctx)
	if userID == nil || *userID == uuid.Nil {
		app.respondWithError(w, errors.New("unauthorized"), http.StatusUnauthorized)
		return
	}

	var input onboardMerchantInput
	if err := app.readJSON(w, r, &input); err != nil {
		app.respondWithError(w, errors.New("invalid request payload"), http.StatusBadRequest)
		return
	}

	result, err := app.InternalServices.OnboardMerchantInternal(
		ctx,
		*userID,
		services.MerchantOnboardingInput{
			Name:    input.Name,
			LogoURL: input.LogoURL,
			Website: input.Website,
		},
	)
	if err != nil {
		switch {
		case errors.Is(err, services.ErrMerchantOnboardingInputInvalid):
			app.respondWithError(w, errors.New("invalid merchant onboarding input"), http.StatusBadRequest)
		case errors.Is(err, services.ErrMerchantOnboardingActorIneligible):
			app.respondWithError(w, errors.New("forbidden"), http.StatusForbidden)
		case errors.Is(err, services.ErrMerchantOnboardingAlreadyCompleted):
			app.respondWithError(w, errors.New("merchant onboarding already completed"), http.StatusConflict)
		case errors.Is(err, data.ErrMerchantIdentityConflict):
			app.respondWithError(w, errors.New("merchant name already exists"), http.StatusConflict)
		case errors.Is(err, context.DeadlineExceeded):
			app.respondWithError(w, errors.New("merchant onboarding request timed out"), http.StatusGatewayTimeout)
		case errors.Is(err, context.Canceled):
			return
		default:
			app.serverErrorResponse(logger, w, r, err)
		}
		return
	}

	if auditErr := app.insertGovernanceAudit(
		ctx,
		userID,
		"onboard_merchant",
		"Complete merchant-principal onboarding",
		"merchant",
		"Merchant entity",
		result.Merchant.ID.String(),
	); auditErr != nil {
		logger.Warn(
			"merchant onboarding succeeded but audit recording failed",
			"merchant_id", result.Merchant.ID,
			"error", auditErr,
		)
	}

	app.respondWithJSON(
		w,
		http.StatusCreated,
		jsonResponse{
			Error:   false,
			Message: "Merchant onboarding completed successfully",
			Data: onboardMerchantResponse{
				Merchant:        result.Merchant,
				MerchantAccount: result.MerchantAccount,
			},
		},
	)
}
