// Package main provides HTTP handlers for the Sagrenti Future Offering
// Platform.
//
// focodebase/fobackend/internal/server/cmd/api/merchants.go
//
// GTM:
//
//	Layer: 3.2 Merchant / Future Offering HTTP Boundary
//	Release Class: SPINE
//	Reason:
//	  Provides the authenticated HTTP boundary for a merchant principal to
//	  read and maintain the canonical FO-native Merchant identity that the
//	  principal owns through the canonical Merchant Account relationship.
//
//	  Merchant identity is distinct from User identity, Merchant Account
//	  lifecycle, Future Offering state, consumer disclosure state, Engagement
//	  Actions, billing, catalog, affiliate, platform, and handoff state.
//
// SPINE Rule:
//
//	Keep compiling.
//	Keep production-ready.
//	Preserve MIDD pre-QR Merchant anonymity.
//	Never expose Merchant identity through an unauthenticated or consumer route.
//	Never treat X-Merchant-ID as authority; it is a resource selector only.
//	Require persisted principal ownership before every Merchant read or mutation.
//	Never duplicate Merchant creation owned by merchant_onboarding.go.
//	Never duplicate canonical Merchant validation/canonicalization.
//	Block deployment if this file weakens Merchant/Merchant Account separation,
//	principal authorization, auditability, or the MIDD disclosure boundary.
package main

import (
	"context"
	"errors"
	"net/http"

	"github.com/PiccoloMondoC/focodebase/fobackend/internal/data"

	"github.com/google/uuid"
)

const (
	actionReadMerchant   = "read_merchant"
	actionUpdateMerchant = "update_merchant"

	maxMerchantNameFieldBytes = 256
	maxMerchantURLFieldBytes  = 2048
)

type updateMerchantInput struct {
	Name    string  `json:"name"`
	LogoURL *string `json:"logo_url"`
	Website *string `json:"website"`
}

// requirePrincipalMerchant resolves the authenticated actor and selected
// Merchant and proves that the actor is the canonical principal of the active
// Merchant Account for that Merchant.
//
// X-Merchant-ID is a selector only. Authority comes exclusively from the
// persisted merchant_accounts principal relationship.
func (app *Application) requirePrincipalMerchant(
	ctx context.Context,
) (*uuid.UUID, uuid.UUID, error) {
	userID := app.getUserIDFromContext(ctx)
	if userID == nil || *userID == uuid.Nil {
		return nil, uuid.Nil, errMerchantAuthenticatedUserMissing
	}

	merchantID := app.getMerchantIDFromContext(ctx)
	if merchantID == nil || *merchantID == uuid.Nil {
		return userID, uuid.Nil, errMerchantContextRequired
	}

	authorized, err := app.Models.MerchantAccount.IsPrincipalForMerchant(
		ctx,
		*userID,
		*merchantID,
	)
	if err != nil {
		return userID, uuid.Nil, err
	}
	if !authorized {
		// Do not reveal whether the selected Merchant exists.
		return userID, uuid.Nil, errMerchantAccessDenied
	}

	return userID, *merchantID, nil
}

var (
	errMerchantAuthenticatedUserMissing = errors.New(
		"authenticated user context is required",
	)
	errMerchantContextRequired = errors.New(
		"merchant context is required",
	)
	errMerchantAccessDenied = errors.New(
		"merchant access denied",
	)
)

// respondMerchantAuthorizationError maps trusted-context and ownership failures
// without leaking Merchant existence to an unauthorized actor.
func (app *Application) respondMerchantAuthorizationError(
	w http.ResponseWriter,
	r *http.Request,
	err error,
) {
	switch {
	case errors.Is(err, errMerchantContextRequired):
		app.respondWithError(
			w,
			errors.New("merchant context is required"),
			http.StatusBadRequest,
		)

	case errors.Is(err, errMerchantAccessDenied):
		app.respondWithError(
			w,
			errors.New("forbidden"),
			http.StatusForbidden,
		)

	case errors.Is(err, errMerchantAuthenticatedUserMissing):
		// AuthMiddleware is mandatory upstream. Missing trusted identity here
		// means the protected route stack is incorrectly composed.
		app.serverErrorResponse(
			app.Logger.GetLoggerWithContext(r),
			w,
			r,
			err,
		)

	case errors.Is(err, context.DeadlineExceeded):
		app.respondWithError(
			w,
			errors.New("request timed out"),
			http.StatusGatewayTimeout,
		)

	case errors.Is(err, context.Canceled):
		return

	default:
		app.serverErrorResponse(
			app.Logger.GetLoggerWithContext(r),
			w,
			r,
			err,
		)
	}
}

// respondMerchantError maps canonical Merchant-domain failures.
//
// Invalid-input and identity-conflict sentinels must be emitted by the owning
// data/service boundary. This handler must never inspect PostgreSQL constraint
// names or parse error strings.
func (app *Application) respondMerchantError(
	w http.ResponseWriter,
	r *http.Request,
	err error,
) {
	logger := app.Logger.
		GetLoggerWithContext(r).
		WithFunctionName("respondMerchantError")

	switch {
	case errors.Is(err, data.ErrMerchantNotFound):
		app.respondWithError(
			w,
			errors.New("merchant not found"),
			http.StatusNotFound,
		)

	case errors.Is(err, data.ErrMerchantIdentityConflict):
		app.respondWithError(
			w,
			errors.New("merchant identity conflicts with an existing merchant"),
			http.StatusConflict,
		)

	case errors.Is(err, context.DeadlineExceeded):
		app.respondWithError(
			w,
			errors.New("request timed out"),
			http.StatusGatewayTimeout,
		)

	case errors.Is(err, context.Canceled):
		return

	default:
		app.serverErrorResponse(logger, w, r, err)
	}
}

// GetMerchantByIDHandler returns the authenticated merchant principal's
// selected canonical Merchant identity.
//
// Route:
//
//	GET /api/v1/merchants/
//
// The route may retain X-Merchant-ID as its resource selector, but the selector
// is never sufficient authorization. Principal ownership is independently
// established from merchant_accounts before Merchant identity is returned.
//
// This is not a consumer/public Merchant endpoint. MIDD-governed consumer
// disclosure remains owned by the controlled handoff boundary.
func (app *Application) GetMerchantByIDHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	logger := app.Logger.
		GetLoggerWithContext(r).
		WithFunctionName("GetMerchantByIDHandler")

	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	userID, merchantID, err := app.requirePrincipalMerchant(ctx)
	if err != nil {
		app.respondMerchantAuthorizationError(w, r, err)
		return
	}

	merchant, err := app.Models.Merchant.GetByID(ctx, merchantID)
	if err != nil {
		app.respondMerchantError(w, r, err)
		return
	}

	logger.Info(
		"merchant identity retrieved",
		"user_id", *userID,
		"merchant_id", merchant.ID,
	)

	app.respondWithJSON(
		w,
		http.StatusOK,
		jsonResponse{
			Error:   false,
			Message: "Merchant retrieved successfully",
			Data:    merchant,
		},
	)
}

// UpdateMerchantHandler replaces the mutable identity facts of the selected
// Merchant owned by the authenticated merchant principal.
//
// Route:
//
//	PATCH /api/v1/merchants/
//
// Although the existing route uses PATCH, the current MerchantModel.Update
// contract replaces the complete mutable identity tuple:
//
//	name, logo_url, website.
//
// Consequently all three fields belong to this request contract. A future true
// partial-update capability should be introduced deliberately rather than
// inferred from the HTTP verb.
//
// Canonical trimming and HTTP(S) URL normalization remain owned by
// data.MerchantModel.Update.
func (app *Application) UpdateMerchantHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	logger := app.Logger.
		GetLoggerWithContext(r).
		WithFunctionName("UpdateMerchantHandler")

	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	userID, merchantID, err := app.requirePrincipalMerchant(ctx)
	if err != nil {
		app.respondMerchantAuthorizationError( w, r, err)
		return
	}

	var input updateMerchantInput
	if err := app.readJSON(w, r, &input); err != nil {
		app.respondWithError(
			w,
			errors.New("invalid request payload"),
			http.StatusBadRequest,
		)
		return
	}

	// These are transport/resource bounds only. They are not Merchant-domain
	// validation and do not replace canonical data-layer validation.
	if len(input.Name) > maxMerchantNameFieldBytes {
		app.respondWithError(
			w,
			errors.New("merchant name exceeds maximum request size"),
			http.StatusUnprocessableEntity,
		)
		return
	}
	if input.LogoURL != nil &&
		len(*input.LogoURL) > maxMerchantURLFieldBytes {
		app.respondWithError(
			w,
			errors.New("merchant logo URL exceeds maximum request size"),
			http.StatusUnprocessableEntity,
		)
		return
	}
	if input.Website != nil &&
		len(*input.Website) > maxMerchantURLFieldBytes {
		app.respondWithError(
			w,
			errors.New("merchant website exceeds maximum request size"),
			http.StatusUnprocessableEntity,
		)
		return
	}

	merchant := &data.Merchant{
		ID:      merchantID,
		Name:    input.Name,
		LogoURL: input.LogoURL,
		Website: input.Website,
	}

	if err := app.Models.Merchant.Update(ctx, merchant); err != nil {
		app.respondMerchantError(w, r, err)
		return
	}

	// The mutation is already committed. Audit failure must therefore never
	// cause the mutation to be repeated or reported as unsuccessful.
	if auditErr := app.insertGovernanceAudit(
		ctx,
		userID,
		actionUpdateMerchant,
		"Update Merchant identity",
		"merchant",
		"Merchant entity",
		merchant.ID.String(),
	); auditErr != nil {
		logger.Warn(
			"merchant identity updated but audit recording failed",
			"merchant_id", merchant.ID,
			"error", auditErr,
		)
	}

	logger.Info(
		"merchant identity updated",
		"user_id", *userID,
		"merchant_id", merchant.ID,
	)

	app.respondWithJSON(
		w,
		http.StatusOK,
		jsonResponse{
			Error:   false,
			Message: "Merchant updated successfully",
			Data:    merchant,
		},
	)
}
