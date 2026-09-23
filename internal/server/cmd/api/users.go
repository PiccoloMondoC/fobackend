// Package main provides HTTP handlers for public user signup, authentication,
// password establishment, refresh-token rotation, logout, and controlled
// account lifecycle operations.
//
// focodebase/fobackend/internal/server/cmd/api/users.go
//
// GTM:
//
//	Layer: 2.2 Identity / Auth Domain
//	Release Class: SPINE
//	Reason:
//	  User authentication handlers are release-critical identity and session
//	  lifecycle infrastructure. They provide the HTTP boundary for public signup,
//	  email/password and verified external-provider authentication, password
//	  establishment, access-token issuance, refresh-token rotation, logout,
//	  self-service account closure, and privileged account expulsion required
//	  by the initial Platform release spine.
//
// SPINE Rule:
//
//	Keep compiling.
//	Keep production-ready.
//	Preserve public signup through the canonical Users service workflow.
//	Preserve data-driven signup-role eligibility; do not hard-code public roles.
//	Preserve email/password authentication through the canonical Users service.
//	Preserve Facebook token verification through TokenService.
//	Keep Google authentication fail-closed until stable provider-subject
//	verification and explicit account provisioning/linking are implemented.
//	Preserve provider-neutral session issuance through InternalServices.
//	Preserve account-lifecycle eligibility at the canonical session-issuance
//	service boundary before credentials are issued.
//	Preserve protected refresh-token persistence, validation, rotation, and
//	revocation through the canonical Users service workflow.
//	Preserve password validation and hashing through the shared security package
//	and Users service workflow.
//	Preserve authenticated identity exclusively through AuthMiddleware.
//	Preserve account soft-delete semantics through the Users service boundary.
//	Preserve required audit metadata through canonical seed data.
//	Do not invoke HTTP handlers from HTTP handlers.
//	Do not create required audit metadata during requests.
//	Do not synchronously provision profile or wallet domains from signup.
//	Do not persist, log, audit, or return password or protected token material.
//	Block deployment if this file breaks signup, login, token issuance,
//	refresh-token rotation, logout, password establishment, account deletion,
//	authorization boundaries, or authentication integrity.
package main

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/PiccoloMondoC/focodebase/fobackend/internal/data"
	"github.com/PiccoloMondoC/focodebase/fobackend/internal/security"
	"github.com/PiccoloMondoC/focodebase/fobackend/internal/services"

	"github.com/google/uuid"
)

const (
	userAuditEntityType            = "users"
	userAuditEntityTypeDescription = "Canonical user account"

	actionSignupUser        = "signup_user"
	actionLogin             = "login"
	actionSetPassword       = "set_password"
	actionRefreshAuthTokens = "refresh_auth_tokens"
	actionLogout            = "logout"
	actionDeleteOwnAccount  = "delete_own_account"
	actionExpelUser         = "expel_user"
)

type signupUserInput struct {
	Email         string `json:"email"`
	Password      string `json:"password"`
	RequestedRole string `json:"requested_role"`
}

type loginInput struct {
	Provider       string `json:"provider"`
	Email          string `json:"email,omitempty"`
	Password       string `json:"password,omitempty"`
	IDToken        string `json:"id_token,omitempty"`
	AccessToken    string `json:"access_token,omitempty"`
	FacebookAppTok string `json:"facebook_app_token,omitempty"`
}

type setPasswordInput struct {
	NewPassword string `json:"new_password"`
}

type refreshTokenInput struct {
	RefreshToken string `json:"refresh_token"`
}

type logoutInput struct {
	RefreshToken string `json:"refresh_token"`
}

func (app *Application) recordUserAudit(
	ctx context.Context,
	actorID *uuid.UUID,
	actionName string,
	actionDescription string,
	entityID uuid.UUID,
) error {
	return app.insertGovernanceAudit(
		ctx,
		actorID,
		actionName,
		actionDescription,
		userAuditEntityType,
		userAuditEntityTypeDescription,
		entityID.String(),
	)
}

// SignupUserHandler handles public email/password signup.
//
// Canonical Users persistence, signup-event publication, and activation-token
// initialization are service-owned. HTTP owns request decoding, controlled
// activation notification delivery, audit presentation, and response mapping.
func (app *Application) SignupUserHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	logger := app.Logger.
		GetLoggerWithContext(r).
		WithFunctionName("SignupUserHandler")

	var input signupUserInput
	if err := app.readJSON(w, r, &input); err != nil {
		app.respondWithError(
			w,
			errors.New("invalid request payload"),
			http.StatusBadRequest,
		)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	result, err := app.InternalServices.SignupUserInternal(
		ctx,
		services.SignupUserInput{
			Email:         input.Email,
			PlainPassword: input.Password,
			RequestedRole: input.RequestedRole,
		},
	)

	// This is a documented soft failure: the canonical user account was
	// created successfully, but activation-token issuance did not complete.
	// The request must not fail — the account exists and can still be
	// activated later via the resend flow — but this is an operationally
	// significant outcome and must leave a trace for whoever is watching
	// the logs, even though it is invisible to the caller.
	if err != nil && errors.Is(err, services.ErrUsersActivationInitialization) {
		logger.Warn(
			"signup succeeded but activation-token initialization failed; "+
				"account requires resend-triggered or administrative activation-token issuance",
			"user_id", result.UserID,
			"error", err,
		)
	}

	if err != nil &&
		!errors.Is(err, services.ErrUsersActivationInitialization) {
		switch {
		case errors.Is(err, services.ErrUsersSignupInputInvalid),
			errors.Is(err, security.ErrEmptyPassword),
			errors.Is(err, security.ErrPasswordLength):
			app.respondWithError(
				w,
				errors.New("invalid signup input"),
				http.StatusBadRequest,
			)

		case errors.Is(err, data.ErrRoleNotAssignableAtSignup):
			app.respondWithError(
				w,
				errors.New("requested role is not available for signup"),
				http.StatusBadRequest,
			)

		case errors.Is(err, data.ErrDuplicateEmail),
			errors.Is(err, data.ErrDuplicateGoogleID),
			errors.Is(err, data.ErrDuplicateFacebookID),
			errors.Is(err, data.ErrDuplicate):
			app.respondWithError(
				w,
				errors.New("an account with this identity already exists"),
				http.StatusConflict,
			)

		case errors.Is(err, context.DeadlineExceeded):
			app.respondWithError(
				w,
				errors.New("signup request timed out"),
				http.StatusGatewayTimeout,
			)

		case errors.Is(err, context.Canceled):
			return

		default:
			app.serverErrorResponse(logger, w, r, err)
		}
		return
	}

	activationDelivery := "pending"

	if result.ActivationToken != "" {
		activationURL, urlErr := buildActivationURL(
			app.Config.Bootstrap.BaseURL,
			result.ActivationToken,
			app.activationURLPolicy(),
		)
		if urlErr == nil {
			contact := &data.UserContactInfo{
				Email: input.Email,
			}

			_, sendErr := app.sendActivationNotification(
				ctx,
				result.UserID,
				contact,
				activationURL,
			)
			if sendErr == nil {
				activationDelivery = "sent"
			} else {
				logger.Warn(
					"signup completed but activation delivery failed",
					"user_id", result.UserID,
					"error", sendErr,
				)
			}
		} else {
			logger.Error(
				"signup completed but activation URL construction failed",
				"user_id", result.UserID,
				"error", urlErr,
			)
		}
	}

	// Public signup is not authenticated actor identity. Do not fabricate the
	// newly-created user as an authenticated audit actor.
	if auditErr := app.recordUserAudit(
		ctx,
		nil,
		actionSignupUser,
		"Signup a new user account",
		result.UserID,
	); auditErr != nil {
		logger.Warn(
			"user signup audit recording failed",
			"user_id", result.UserID,
			"error", auditErr,
		)
	}

	app.respondWithJSON(
		w,
		http.StatusCreated,
		jsonResponse{
			Error:   false,
			Message: "Account created successfully; activation is required",
			Data: struct {
				UserID             uuid.UUID `json:"user_id"`
				ActivationRequired bool      `json:"activation_required"`
				ActivationDelivery string    `json:"activation_delivery"`
			}{
				UserID:             result.UserID,
				ActivationRequired: true,
				ActivationDelivery: activationDelivery,
			},
		},
	)
}

// LoginHandler authenticates a user through the requested supported provider
// and issues a fresh access/refresh token pair.
//
// Email/password authentication is service-owned. Facebook provider proof
// verification remains owned by TokenService, after which provider-neutral
// session issuance re-reads canonical account lifecycle state before issuing
// credentials.
//
// Google authentication is deliberately unavailable at this boundary until
// provider verification supplies a stable Google subject and the Platform owns
// an explicit provisioning/linking lifecycle. Verified email alone is not used
// as an implicit account-linking credential.
func (app *Application) LoginHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	logger := app.Logger.
		GetLoggerWithContext(r).
		WithFunctionName("LoginHandler")

	var input loginInput
	if err := app.readJSON(w, r, &input); err != nil {
		logger.Warn("invalid login payload", "error", err)
		app.respondWithError(
			w,
			errors.New("invalid request payload"),
			http.StatusBadRequest,
		)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	var session services.UserSession

	switch strings.ToLower(strings.TrimSpace(input.Provider)) {
	case "email":
		var err error

		session, err = app.InternalServices.LoginUserPasswordInternal(
			ctx,
			input.Email,
			input.Password,
		)
		if err != nil {
			if errors.Is(err, data.ErrInvalidCredentials) ||
				errors.Is(err, data.ErrUserNotFound) ||
				errors.Is(
					err,
					services.ErrUsersAuthenticationInputInvalid,
				) {
				logger.Warn("authentication rejected")
				app.respondWithError(
					w,
					errors.New("invalid credentials"),
					http.StatusUnauthorized,
				)
				return
			}

			app.serverErrorResponse(logger, w, r, err)
			return
		}

	case "google":
		// Google authentication is intentionally fail-closed until the
		// provider-verification contract exposes a stable Google subject and
		// the Platform has an explicit provisioning/linking lifecycle. Resolving
		// canonical accounts by verified email alone would make email address
		// equivalence an implicit identity-linking authority, which is not an
		// acceptable authentication boundary.
		app.respondWithError(
			w,
			errors.New("google authentication is not available"),
			http.StatusNotImplemented,
		)
		return

	case "facebook":
		input.AccessToken = strings.TrimSpace(input.AccessToken)
		input.FacebookAppTok = strings.TrimSpace(input.FacebookAppTok)

		if input.AccessToken == "" || input.FacebookAppTok == "" {
			app.respondWithError(
				w,
				errors.New(
					"access_token and facebook_app_token are required",
				),
				http.StatusBadRequest,
			)
			return
		}

		facebookUserID, err :=
			app.TokenService.VerifyFacebookToken(
				ctx,
				input.AccessToken,
				input.FacebookAppTok,
				app.Config.OAuth.FacebookAppID,
			)
		if err != nil {
			logger.Warn(
				"facebook token verification failed",
				"error", err,
			)
			app.respondWithError(
				w,
				errors.New("invalid facebook token"),
				http.StatusUnauthorized,
			)
			return
		}

		user, err := app.Models.User.Authenticate(
			ctx,
			"",
			"",
			"",
			facebookUserID,
		)
		if err != nil {
			if errors.Is(err, data.ErrInvalidCredentials) ||
				errors.Is(err, data.ErrUserNotFound) {
				logger.Warn("authentication rejected")
				app.respondWithError(
					w,
					errors.New("invalid credentials"),
					http.StatusUnauthorized,
				)
				return
			}

			app.serverErrorResponse(logger, w, r, err)
			return
		}

		if user == nil || user.ID == uuid.Nil {
			logger.Warn("authentication rejected")
			app.respondWithError(
				w,
				errors.New("invalid credentials"),
				http.StatusUnauthorized,
			)
			return
		}

		session, err = app.InternalServices.IssueUserSessionInternal(
			ctx,
			user.ID,
		)
		if err != nil {
			if errors.Is(err, data.ErrInvalidCredentials) ||
				errors.Is(err, data.ErrUserNotFound) {
				logger.Warn("authentication rejected")
				app.respondWithError(
					w,
					errors.New("invalid credentials"),
					http.StatusUnauthorized,
				)
				return
			}

			app.serverErrorResponse(logger, w, r, err)
			return
		}

	default:
		app.respondWithError(
			w,
			errors.New("unsupported authentication provider"),
			http.StatusBadRequest,
		)
		return
	}

	userID := session.UserID

	if auditErr := app.recordUserAudit(
		ctx,
		&userID,
		actionLogin,
		"Log in a user and issue authentication tokens",
		userID,
	); auditErr != nil {
		logger.Warn(
			"user login succeeded but audit recording failed",
			"user_id", userID,
			"error", auditErr,
		)
	}

	logger.Info(
		"user login successful",
		"user_id", userID,
	)

	app.respondWithJSON(
		w,
		http.StatusOK,
		jsonResponse{
			Error:   false,
			Message: "Login successful",
			Data: struct {
				AccessToken  string `json:"access_token"`
				RefreshToken string `json:"refresh_token"`
			}{
				AccessToken:  session.AccessToken,
				RefreshToken: session.RefreshToken,
			},
		},
	)
}

// SetPasswordHandler lets an authenticated OAuth-only user establish an
// initial password.
//
// Initial-password eligibility and atomic no-overwrite enforcement are
// service-owned. Existing-password changes belong to the distinct
// password-change workflow.
func (app *Application) SetPasswordHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	logger := app.Logger.
		GetLoggerWithContext(r).
		WithFunctionName("SetPasswordHandler")

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

	var input setPasswordInput
	if err := app.readJSON(w, r, &input); err != nil {
		logger.Warn("invalid password payload", "error", err)
		app.respondWithError(
			w,
			errors.New("invalid request payload"),
			http.StatusBadRequest,
		)
		return
	}

	err := app.InternalServices.EstablishInitialPasswordInternal(
		ctx,
		*userID,
		input.NewPassword,
	)
	if err != nil {
		switch {
		case errors.Is(err, security.ErrEmptyPassword),
			errors.Is(err, security.ErrPasswordLength):
			app.respondWithError(
				w,
				errors.New(
					"password does not meet minimum security requirements",
				),
				http.StatusBadRequest,
			)

		case errors.Is(err, data.ErrUserNotFound):
			app.respondWithError(
				w,
				errors.New("user not found"),
				http.StatusNotFound,
			)

		case errors.Is(err, services.ErrUsersAccountNotEligible),
			errors.Is(
				err,
				services.ErrUsersPasswordAlreadyEstablished,
			):
			app.respondWithError(
				w,
				errors.New(
					"account is not eligible for initial password establishment",
				),
				http.StatusConflict,
			)

		default:
			app.serverErrorResponse(logger, w, r, err)
		}
		return
	}

	if auditErr := app.recordUserAudit(
		ctx,
		userID,
		actionSetPassword,
		"Establish an initial password for a user account",
		*userID,
	); auditErr != nil {
		logger.Warn(
			"password established but audit recording failed",
			"user_id", *userID,
			"error", auditErr,
		)
	}

	logger.Info(
		"password established successfully",
		"user_id", *userID,
	)

	app.respondWithJSON(
		w,
		http.StatusOK,
		jsonResponse{
			Error:   false,
			Message: "Password established successfully",
			Data: struct {
				UserID uuid.UUID `json:"user_id"`
				Status string    `json:"status"`
			}{
				UserID: *userID,
				Status: "password_set",
			},
		},
	)
}

// RefreshTokenHandler exchanges a valid refresh credential for a fresh
// access/refresh pair.
//
// Validation, account lifecycle checking, mandatory revocation, and replacement
// session issuance are owned by the Users service.
func (app *Application) RefreshTokenHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	logger := app.Logger.
		GetLoggerWithContext(r).
		WithFunctionName("RefreshTokenHandler")

	var input refreshTokenInput
	if err := app.readJSON(w, r, &input); err != nil {
		logger.Warn("invalid refresh-token payload", "error", err)
		app.respondWithError(
			w,
			errors.New("invalid request payload"),
			http.StatusBadRequest,
		)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	session, err :=
		app.InternalServices.RefreshUserSessionInternal(
			ctx,
			input.RefreshToken,
		)
	if err != nil {
		switch {
		case errors.Is(err, services.ErrUsersRefreshTokenRequired):
			app.respondWithError(
				w,
				errors.New("refresh_token is required"),
				http.StatusBadRequest,
			)

		case errors.Is(err, services.ErrUsersSessionInvalid):
			app.respondWithError(
				w,
				errors.New("invalid or expired refresh token"),
				http.StatusUnauthorized,
			)

		case errors.Is(err, context.DeadlineExceeded):
			app.respondWithError(
				w,
				errors.New("refresh request timed out"),
				http.StatusGatewayTimeout,
			)

		case errors.Is(err, context.Canceled):
			return

		default:
			app.serverErrorResponse(logger, w, r, err)
		}
		return
	}

	userID := session.UserID

	if auditErr := app.recordUserAudit(
		ctx,
		&userID,
		actionRefreshAuthTokens,
		"Rotate a refresh token and issue replacement authentication tokens",
		userID,
	); auditErr != nil {
		logger.Warn(
			"authentication tokens rotated but audit recording failed",
			"user_id", userID,
			"error", auditErr,
		)
	}

	logger.Info(
		"authentication tokens rotated",
		"user_id", userID,
	)

	app.respondWithJSON(
		w,
		http.StatusOK,
		jsonResponse{
			Error:   false,
			Message: "Tokens issued successfully",
			Data: struct {
				AccessToken  string `json:"access_token"`
				RefreshToken string `json:"refresh_token"`
			}{
				AccessToken:  session.AccessToken,
				RefreshToken: session.RefreshToken,
			},
		},
	)
}

// LogoutHandler revokes the presented refresh token for the authenticated
// caller's own session.
//
// Refresh-token validation, ownership checking, and revocation are service-owned.
func (app *Application) LogoutHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	logger := app.Logger.
		GetLoggerWithContext(r).
		WithFunctionName("LogoutHandler")

	var input logoutInput
	if err := app.readJSON(w, r, &input); err != nil {
		logger.Warn("invalid logout payload", "error", err)
		app.respondWithError(
			w,
			errors.New("invalid request payload"),
			http.StatusBadRequest,
		)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	actorID := app.getUserIDFromContext(ctx)
	if actorID == nil || *actorID == uuid.Nil {
		app.respondWithError(
			w,
			errors.New("unauthorized"),
			http.StatusUnauthorized,
		)
		return
	}

	err := app.InternalServices.LogoutUserSessionInternal(
		ctx,
		*actorID,
		input.RefreshToken,
	)
	if err != nil {
		switch {
		case errors.Is(err, services.ErrUsersRefreshTokenRequired):
			app.respondWithError(
				w,
				errors.New("refresh_token is required"),
				http.StatusBadRequest,
			)

		case errors.Is(err, services.ErrUsersSessionInvalid):
			app.respondWithError(
				w,
				errors.New("invalid or expired refresh token"),
				http.StatusUnauthorized,
			)

		case errors.Is(
			err,
			services.ErrUsersSessionOwnershipMismatch,
		):
			app.respondWithError(
				w,
				errors.New("forbidden"),
				http.StatusForbidden,
			)

		case errors.Is(err, context.DeadlineExceeded):
			app.respondWithError(
				w,
				errors.New("logout request timed out"),
				http.StatusGatewayTimeout,
			)

		case errors.Is(err, context.Canceled):
			return

		default:
			app.serverErrorResponse(logger, w, r, err)
		}
		return
	}

	if auditErr := app.recordUserAudit(
		ctx,
		actorID,
		actionLogout,
		"Log out a user and revoke the presented refresh token",
		*actorID,
	); auditErr != nil {
		logger.Warn(
			"logout succeeded but audit recording failed",
			"user_id", *actorID,
			"error", auditErr,
		)
	}

	logger.Info(
		"user logout successful",
		"user_id", *actorID,
	)

	app.respondWithJSON(
		w,
		http.StatusOK,
		jsonResponse{
			Error:   false,
			Message: "Logged out successfully",
			Data: struct {
				UserID uuid.UUID `json:"user_id"`
				Status string    `json:"status"`
			}{
				UserID: *actorID,
				Status: "logged_out",
			},
		},
	)
}

// DeleteMeHandler soft-deletes the authenticated user's own account.
//
// Authorization remains enforced at the HTTP boundary. Canonical account
// lifecycle mutation and session invalidation are service-owned.
func (app *Application) DeleteMeHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	logger := app.Logger.
		GetLoggerWithContext(r).
		WithFunctionName("DeleteMeHandler")

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

	// Defense in depth: the route already enforces this permission.
	if !app.HasPermission(ctx, "delete_own_account") {
		app.respondWithError(
			w,
			errors.New("forbidden"),
			http.StatusForbidden,
		)
		return
	}

	if err := app.InternalServices.CloseOwnAccountInternal(
		ctx,
		*userID,
	); err != nil {
		if errors.Is(err, data.ErrUserNotFound) {
			app.respondWithError(
				w,
				errors.New("user not found"),
				http.StatusNotFound,
			)
			return
		}

		app.serverErrorResponse(logger, w, r, err)
		return
	}

	if auditErr := app.recordUserAudit(
		ctx,
		userID,
		actionDeleteOwnAccount,
		"Close the authenticated user's own account",
		*userID,
	); auditErr != nil {
		logger.Warn(
			"account closure succeeded but audit recording failed",
			"user_id", *userID,
			"error", auditErr,
		)
	}

	logger.Info(
		"user account soft-deleted",
		"user_id", *userID,
	)

	app.respondWithJSON(
		w,
		http.StatusOK,
		jsonResponse{
			Error:   false,
			Message: "Your account has been deleted successfully",
			Data: struct {
				UserID uuid.UUID `json:"user_id"`
				Status string    `json:"status"`
			}{
				UserID: *userID,
				Status: "deleted",
			},
		},
	)
}

// AdminDeleteUserHandler performs privileged soft deletion of a target user
// account.
//
// Internal actor classification and the expel_user capability are enforced at
// both route and handler boundaries. Canonical target-account lifecycle
// mutation and session invalidation are service-owned.
func (app *Application) AdminDeleteUserHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	logger := app.Logger.
		GetLoggerWithContext(r).
		WithFunctionName("AdminDeleteUserHandler")

	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	actorID := app.getUserIDFromContext(ctx)
	if actorID == nil || *actorID == uuid.Nil {
		app.respondWithError(
			w,
			errors.New("unauthorized"),
			http.StatusUnauthorized,
		)
		return
	}

	if !isInternalRole(getRoleFromContext(ctx)) ||
		!app.HasPermission(ctx, "expel_user") {
		app.respondWithError(
			w,
			errors.New("forbidden"),
			http.StatusForbidden,
		)
		return
	}

	targetUserID := app.getTargetUserIDFromContext(ctx)
	if targetUserID == nil || *targetUserID == uuid.Nil {
		app.respondWithError(
			w,
			errors.New("target user ID is required"),
			http.StatusBadRequest,
		)
		return
	}

	if err := app.InternalServices.ExpelUserInternal(
		ctx,
		*actorID,
		*targetUserID,
	); err != nil {
		if errors.Is(err, data.ErrUserNotFound) {
			app.respondWithError(
				w,
				errors.New("user not found"),
				http.StatusNotFound,
			)
			return
		}

		app.serverErrorResponse(logger, w, r, err)
		return
	}

	if auditErr := app.recordUserAudit(
		ctx,
		actorID,
		actionExpelUser,
		"Expel a user account through privileged administration",
		*targetUserID,
	); auditErr != nil {
		logger.Warn(
			"user expulsion succeeded but audit recording failed",
			"actor_id", *actorID,
			"target_user_id", *targetUserID,
			"error", auditErr,
		)
	}

	logger.Info(
		"user account expelled",
		"actor_id", *actorID,
		"target_user_id", *targetUserID,
	)

	app.respondWithJSON(
		w,
		http.StatusOK,
		jsonResponse{
			Error:   false,
			Message: "User expelled successfully",
			Data: struct {
				ActorID      uuid.UUID `json:"actor_id"`
				TargetUserID uuid.UUID `json:"target_user_id"`
				Status       string    `json:"status"`
			}{
				ActorID:      *actorID,
				TargetUserID: *targetUserID,
				Status:       "expelled",
			},
		},
	)
}
