// Package main provides HTTP handlers and API-boundary helpers for password
// recovery and authenticated password change.
//
// focodebase/fobackend/internal/server/cmd/api/user_password.go
//
// GTM:
//
//	Layer: 2.2 Identity / Auth Domain
//	Release Class: SPINE
//	Reason:
//	  Password recovery and password change are release-critical credential
//	  lifecycle infrastructure. This file owns the HTTP boundary for public
//	  password-reset requests, public reset-credential redemption, and
//	  authenticated password change, together with controlled reset-link
//	  construction and delivery.
//
//	  Cross-model workflow composition does not belong here. The canonical
//	  internal service layer owns the transactions that compose UserModel,
//	  PasswordResetModel, and TokenModel.
//
// SPINE Rule:
//
//	Keep compiling.
//	Keep production-ready.
//	Preserve non-enumerating password-reset requests: every well-formed
//	request receives the same accepted response regardless of account
//	existence, state, or issuance outcome.
//	Preserve reset-credential plaintext only at controlled input and
//	notification boundaries. Never log, audit, trace, or persist plaintext
//	reset tokens or reset URLs.
//	Preserve delivery to the account's canonical stored email address only.
//	Preserve uniform public treatment of unknown, replaced, expired, and
//	ineligible reset credentials.
//	Preserve authenticated identity exclusively through AuthMiddleware for
//	password change, plus verification of the existing credential.
//	Preserve post-change session invalidation, including the presented access
//	token.
//	Keep password-change credential failure distinct from 401 so client
//	refresh-and-retry logic can never treat a wrong current password as an
//	expired session.
//	Do not fabricate authenticated actor identity for public bearer flows.
//	Do not invoke one HTTP handler from another HTTP handler.
//	Do not dynamically create audit actions or entity types during requests.
//	Block deployment if this file breaks reset requests, reset redemption,
//	password change, non-enumeration, credential protection, or session
//	invalidation.
package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/PiccoloMondoC/focodebase/fobackend/internal/data"
	notificationservices "github.com/PiccoloMondoC/focodebase/fobackend/internal/notification_services"
	"github.com/PiccoloMondoC/focodebase/fobackend/internal/security"
	"github.com/PiccoloMondoC/focodebase/fobackend/internal/services"

	"github.com/google/uuid"
)

const (
	// passwordResetPath is the Angular route that accepts a reset credential.
	passwordResetPath = "/reset-password"

	actionRequestPasswordReset = "request_password_reset"
	actionResetPassword        = "reset_password"
	actionChangePassword       = "change_password"

	passwordResetAcceptedMessage = "If an active account uses this email address, a password reset link will be sent"
	passwordRequirementsMessage  = "password does not meet minimum security requirements"
	resetCredentialInvalidMsg    = "reset token is invalid or expired"
)

type requestPasswordResetInput struct {
	Email string `json:"email"`
}

type resetPasswordInput struct {
	Token       string `json:"token"`
	NewPassword string `json:"new_password"`
}

type changePasswordInput struct {
	CurrentPassword    string `json:"current_password"`
	NewPassword        string `json:"new_password"`
	ConfirmNewPassword string `json:"confirm_new_password"`
}

// passwordErrorResponse is the public HTTP treatment of a password-workflow
// error. serverError marks outcomes that must go through serverErrorResponse
// so infrastructure detail is logged but never returned; silent marks a
// client-cancelled request that must not be answered.
type passwordErrorResponse struct {
	status      int
	message     string
	serverError bool
	silent      bool
}

// classifyResetPasswordError maps ResetPasswordInternal failures to their
// public treatment.
//
// Every credential condition collapses to one response so the endpoint cannot
// be used to learn whether a credential ever existed, was replaced, expired,
// or belongs to an account that is no longer active.
func classifyResetPasswordError(err error) passwordErrorResponse {
	switch {
	case errors.Is(err, data.ErrInvalidResetToken):
		return passwordErrorResponse{
			status:  http.StatusUnauthorized,
			message: resetCredentialInvalidMsg,
		}

	case errors.Is(err, security.ErrEmptyPassword),
		errors.Is(err, security.ErrPasswordLength):
		return passwordErrorResponse{
			status:  http.StatusBadRequest,
			message: passwordRequirementsMessage,
		}

	case errors.Is(err, context.Canceled):
		return passwordErrorResponse{silent: true}

	case errors.Is(err, context.DeadlineExceeded):
		return passwordErrorResponse{
			status:  http.StatusGatewayTimeout,
			message: "password reset request timed out",
		}

	default:
		return passwordErrorResponse{serverError: true}
	}
}

// classifyChangePasswordError maps ChangePasswordInternal failures to their
// public treatment.
//
// A wrong current password is 403, never 401: 401 on an authenticated route
// means "your session is not valid", which clients answer by refreshing the
// session and retrying. A missing or no-longer-active account is 401 because
// the caller's session genuinely cannot be honoured.
func classifyChangePasswordError(err error) passwordErrorResponse {
	switch {
	case errors.Is(err, services.ErrUsersAuthenticationInputInvalid):
		return passwordErrorResponse{
			status:  http.StatusBadRequest,
			message: "current password, new password, and confirmation are required",
		}

	case errors.Is(err, services.ErrUsersPasswordConfirmationMismatch):
		return passwordErrorResponse{
			status:  http.StatusBadRequest,
			message: "new password and confirmation do not match",
		}

	case errors.Is(err, services.ErrUsersPasswordUnchanged):
		return passwordErrorResponse{
			status:  http.StatusBadRequest,
			message: "new password must be different from the current password",
		}

	case errors.Is(err, security.ErrEmptyPassword),
		errors.Is(err, security.ErrPasswordLength):
		return passwordErrorResponse{
			status:  http.StatusBadRequest,
			message: passwordRequirementsMessage,
		}

	case errors.Is(err, services.ErrUsersCurrentPasswordIncorrect):
		return passwordErrorResponse{
			status:  http.StatusForbidden,
			message: "current password is incorrect",
		}

	case errors.Is(err, services.ErrUsersPasswordNotEstablished):
		return passwordErrorResponse{
			status:  http.StatusConflict,
			message: "account has no password to change",
		}

	case errors.Is(err, data.ErrUserNotFound),
		errors.Is(err, services.ErrUsersActorRequired):
		return passwordErrorResponse{
			status:  http.StatusUnauthorized,
			message: "unauthorized",
		}

	case errors.Is(err, context.Canceled):
		return passwordErrorResponse{silent: true}

	case errors.Is(err, context.DeadlineExceeded):
		return passwordErrorResponse{
			status:  http.StatusGatewayTimeout,
			message: "password change request timed out",
		}

	default:
		return passwordErrorResponse{serverError: true}
	}
}

// RequestPasswordResetHandler accepts a public password-reset request.
//
// The response is deliberately identical for every well-formed request.
// Account absence, inactivity, deletion, issuance failure, and delivery
// failure are visible only in server logs, never to the caller.
//
// Known residual risk, shared with ResendActivationLinkHandler: an eligible
// account causes a database write and a synchronous provider call, so response
// timing differs from the ineligible path even though the body does not.
// Moving delivery onto the durable async/outbox foundation closes this for
// both flows together. Rate limiting is IP-scoped (RateLimitMiddleware); each
// new issuance replaces the previous credential, so repeated requests cannot
// accumulate valid reset links.
func (app *Application) RequestPasswordResetHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	logger := app.Logger.
		GetLoggerWithContext(r).
		WithFunctionName("RequestPasswordResetHandler")

	var input requestPasswordResetInput
	if err := app.readJSON(w, r, &input); err != nil {
		app.respondWithError(
			w,
			errors.New("invalid request body"),
			http.StatusBadRequest,
		)
		return
	}

	email := strings.TrimSpace(input.Email)
	if email == "" {
		app.respondWithError(
			w,
			errors.New("email is required"),
			http.StatusBadRequest,
		)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	if app.InternalServices == nil {
		logger.Error("internal password-reset service is unavailable")
		app.respondWithError(
			w,
			errors.New("password reset service is unavailable"),
			http.StatusServiceUnavailable,
		)
		return
	}

	// Public privacy invariant: do not disclose whether this email identifies
	// an account, whether that account is active, or whether issuance or
	// delivery succeeded. Mirrors ResendActivationLinkHandler.
	user, err := app.Models.User.GetByEmail(ctx, email)
	if err == nil &&
		user != nil &&
		user.ID != uuid.Nil &&
		user.IsActive {

		token, issueErr := app.InternalServices.RequestPasswordResetInternal(
			ctx,
			user.ID,
		)
		switch {
		case issueErr == nil:
			// The credential is committed (replacing any earlier one).
			// Delivery runs off the request path so an existing account is
			// not measurably slower to answer than an unknown address.
			userID, email := user.ID, user.Email
			app.dispatchCredentialNotification(
				ctx,
				logger,
				notificationservices.FlowPasswordReset,
				func(deliveryCtx context.Context) error {
					return app.deliverPasswordReset(deliveryCtx, userID, email, token)
				},
			)

			// Public reset requests are not authenticated actor identity.
			if auditErr := app.recordUserAudit(
				ctx,
				nil,
				actionRequestPasswordReset,
				"Issue a password reset credential for a user account",
				user.ID,
			); auditErr != nil {
				logger.Warn(
					"password reset issued but audit recording failed",
					"user_id", user.ID,
					"error", auditErr,
				)
			}

		case errors.Is(issueErr, data.ErrUserNotFound),
			errors.Is(issueErr, context.Canceled),
			errors.Is(issueErr, context.DeadlineExceeded):
			// Became ineligible concurrently, or the request ended.

		default:
			logger.Warn(
				"password reset issuance failed",
				"user_id", user.ID,
				"error", issueErr,
			)
		}
	} else if err != nil &&
		!errors.Is(err, data.ErrUserNotFound) &&
		!errors.Is(err, context.Canceled) &&
		!errors.Is(err, context.DeadlineExceeded) {
		// Preserve the non-enumerating response while keeping genuine
		// datastore failures visible to operators.
		logger.Warn(
			"password reset account lookup failed",
			"error", err,
		)
	}

	if errors.Is(ctx.Err(), context.Canceled) {
		return
	}

	app.respondWithJSON(
		w,
		http.StatusAccepted,
		jsonResponse{
			Error:   false,
			Message: passwordResetAcceptedMessage,
		},
	)
}

// deliverPasswordReset builds the reset link from the authoritative browser
// origin (FRONTEND_URL, never the API BaseURL) and sends the reset message.
//
// The link carries plaintext bearer material: it is passed straight to the
// EmailSender and never logged. Errors describe the stage only.
func (app *Application) deliverPasswordReset(
	ctx context.Context,
	userID uuid.UUID,
	email string,
	resetToken string,
) error {
	resetURL, err := buildCredentialLinkURL(
		app.frontendBaseURL(),
		passwordResetPath,
		resetToken,
		app.credentialLinkURLPolicy(),
	)
	if err != nil {
		return fmt.Errorf("user %s: build password reset link: %w", userID, err)
	}

	if app.EmailService == nil {
		return fmt.Errorf("user %s: password reset email service is unavailable", userID)
	}

	content := notificationservices.PasswordResetContent{ResetURL: resetURL}
	if app.InternalServices != nil && app.InternalServices.Cfg != nil {
		content.ValidFor = app.InternalServices.Cfg.PasswordResetTokenTTL
	}

	if err := app.EmailService.SendPasswordResetContext(
		ctx,
		strings.TrimSpace(email),
		content,
	); err != nil {
		return fmt.Errorf("user %s: send password reset email: %w", userID, err)
	}

	return nil
}

// ResetPasswordHandler redeems a plaintext reset credential and establishes
// the replacement password.
//
// This endpoint is public because the reset credential is itself the bearer
// proof. Credential validation, password mutation, credential consumption,
// and session revocation are service-owned.
func (app *Application) ResetPasswordHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	logger := app.Logger.
		GetLoggerWithContext(r).
		WithFunctionName("ResetPasswordHandler")

	var input resetPasswordInput
	if err := app.readJSON(w, r, &input); err != nil {
		app.respondWithError(
			w,
			errors.New("invalid request body"),
			http.StatusBadRequest,
		)
		return
	}

	if strings.TrimSpace(input.Token) == "" {
		app.respondWithError(
			w,
			errors.New("reset token is required"),
			http.StatusBadRequest,
		)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	if app.InternalServices == nil {
		logger.Error("internal password-reset service is unavailable")
		app.respondWithError(
			w,
			errors.New("password reset service is unavailable"),
			http.StatusServiceUnavailable,
		)
		return
	}

	userID, err := app.InternalServices.ResetPasswordInternal(
		ctx,
		input.Token,
		input.NewPassword,
	)
	if err != nil {
		// Never log the plaintext credential or password.
		logger.Warn("password reset failed", "error", err)

		mapped := classifyResetPasswordError(err)
		switch {
		case mapped.silent:
		case mapped.serverError:
			app.serverErrorResponse(logger, w, r, err)
		default:
			app.respondWithError(
				w,
				errors.New(mapped.message),
				mapped.status,
			)
		}
		return
	}

	// Public bearer-credential flow: record the account as the entity target
	// without fabricating it as an authenticated actor.
	if auditErr := app.recordUserAudit(
		ctx,
		nil,
		actionResetPassword,
		"Reset a user account password using a password reset credential",
		userID,
	); auditErr != nil {
		logger.Warn(
			"password reset completed but audit recording failed",
			"user_id", userID,
			"error", auditErr,
		)
	}

	logger.Info("password reset completed", "user_id", userID)

	app.respondWithJSON(
		w,
		http.StatusOK,
		jsonResponse{
			Error:   false,
			Message: "Password reset successfully",
			Data: struct {
				Status string `json:"status"`
			}{
				Status: "password_reset",
			},
		},
	)
}

// ChangePasswordHandler replaces the authenticated caller's password after
// verifying the current password.
//
// Every refresh session for the account is revoked by the service, and the
// presented access token is revoked here, so the caller must sign in again
// with the new password. The response says so explicitly.
func (app *Application) ChangePasswordHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	logger := app.Logger.
		GetLoggerWithContext(r).
		WithFunctionName("ChangePasswordHandler")

	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	userID := app.getUserIDFromContext(ctx)
	if userID == nil || *userID == uuid.Nil {
		app.respondWithError(
			w,
			errors.New("unauthorized"),
			http.StatusUnauthorized,
		)
		return
	}

	var input changePasswordInput
	if err := app.readJSON(w, r, &input); err != nil {
		logger.Warn("invalid password-change payload", "error", err)
		app.respondWithError(
			w,
			errors.New("invalid request payload"),
			http.StatusBadRequest,
		)
		return
	}

	err := app.InternalServices.ChangePasswordInternal(
		ctx,
		*userID,
		input.CurrentPassword,
		input.NewPassword,
		input.ConfirmNewPassword,
	)
	if err != nil {
		mapped := classifyChangePasswordError(err)
		switch {
		case mapped.silent:
		case mapped.serverError:
			app.serverErrorResponse(logger, w, r, err)
		default:
			logger.Warn(
				"password change rejected",
				"user_id", *userID,
				"error", err,
			)
			app.respondWithError(
				w,
				errors.New(mapped.message),
				mapped.status,
			)
		}
		return
	}

	app.revokePresentedAccessToken(ctx, r, logger)

	if auditErr := app.recordUserAudit(
		ctx,
		userID,
		actionChangePassword,
		"Change the authenticated user's password",
		*userID,
	); auditErr != nil {
		logger.Warn(
			"password changed but audit recording failed",
			"user_id", *userID,
			"error", auditErr,
		)
	}

	logger.Info("password changed", "user_id", *userID)

	app.respondWithJSON(
		w,
		http.StatusOK,
		jsonResponse{
			Error:   false,
			Message: "Password changed successfully; sign in again with the new password",
			Data: struct {
				UserID                   uuid.UUID `json:"user_id"`
				Status                   string    `json:"status"`
				ReauthenticationRequired bool      `json:"reauthentication_required"`
			}{
				UserID:                   *userID,
				Status:                   "password_changed",
				ReauthenticationRequired: true,
			},
		},
	)
}

// bearerTokenFromRequest returns the raw bearer credential presented in the
// Authorization header, or "" when none is present.
//
// AuthMiddleware remains the authentication boundary. This helper exists only
// so session-ending handlers can revoke the exact access token they were
// called with. If AuthMiddleware already exposes a central extractor, use it
// here instead and delete this helper.
func bearerTokenFromRequest(r *http.Request) string {
	if r == nil {
		return ""
	}

	const prefix = "bearer "

	header := strings.TrimSpace(r.Header.Get("Authorization"))
	if len(header) <= len(prefix) ||
		!strings.EqualFold(header[:len(prefix)], prefix) {
		return ""
	}

	return strings.TrimSpace(header[len(prefix):])
}

// revokePresentedAccessToken records the caller's presented access token as
// revoked through the canonical TokenService boundary.
//
// This is defense in depth for session-ending operations. Refresh-session
// revocation is the primary control; failure here is logged and does not fail
// the already-completed operation. The token value is never logged.
func (app *Application) revokePresentedAccessToken(
	ctx context.Context,
	r *http.Request,
	logger interface {
		Warn(msg string, keysAndValues ...interface{})
	},
) {
	if app.TokenService == nil {
		return
	}

	accessToken := bearerTokenFromRequest(r)
	if accessToken == "" {
		return
	}

	if err := app.TokenService.RevokeAccessToken(ctx, accessToken); err != nil {
		logger.Warn(
			"presented access-token revocation failed",
			"error", err,
		)
	}
}
