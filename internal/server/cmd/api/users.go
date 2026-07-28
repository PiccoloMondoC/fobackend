// Package main provides HTTP handlers for user registration, authentication,
// external identity-provider login, OAuth account linking, password management,
// refresh-token rotation, logout, and account lifecycle operations.
//
// sdworkspace/sdbackend/internal/server/cmd/api/users.go
//
// GTM:
//
//	Layer: 2.2 Identity / Auth Domain
//	Release Class: SPINE
//	Reason:
//	  User authentication handlers are release-critical identity and session
//	  lifecycle infrastructure. They provide the HTTP boundary for account
//	  registration, email/password authentication, verified Google and Facebook
//	  login, OAuth account linking, password management, access-token issuance,
//	  refresh-token rotation, logout, and controlled account deletion required
//	  by the initial Platform release spine.
//
// SPINE Rule:
//
//	Keep compiling.
//	Keep production-ready.
//	Preserve controlled registration and role-assignment boundaries.
//	Preserve email/password authentication through the canonical user model.
//	Preserve Google and Facebook token verification through TokenService.
//	Preserve access-token issuance through the canonical EdDSA token service.
//	Preserve protected refresh-token persistence and lookup.
//	Preserve mandatory refresh-token rotation before issuing replacement tokens.
//	Preserve authenticated OAuth account-linking ownership boundaries.
//	Preserve password hashing through the shared security package.
//	Preserve account soft-delete semantics.
//	Do not persist, log, audit, or return protected token hashes or password
//	material.
//	Block deployment if this file breaks registration, login, external identity
//	verification, token issuance, refresh-token rotation, logout, password
//	management, OAuth linking, account deletion, or authentication integrity.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/PiccoloMondoC/sdworkspace/sdbackend/internal/data"
	"github.com/PiccoloMondoC/sdworkspace/sdbackend/internal/security"

	"github.com/google/uuid"
)

type setPasswordInput struct {
	NewPassword string `json:"new_password"`
}

// RegisterUserHandler handles new user registration.
// It supports secure role assignment (only "consumer" or "merchant"),
// creates the core user account, triggers auto-profile creation,
// logs audit activity, and initiates activation workflow.
func (app *Application) RegisterUserHandler(w http.ResponseWriter, r *http.Request) {
	logger := app.Logger.GetLoggerWithContext(r).WithFunctionName("RegisterUserHandler")

	// --- Decode Request Payload ---
	var input struct {
		Email         string  `json:"email"`
		Password      string  `json:"password"`
		RequestedRole string  `json:"requested_role,omitempty"`
		UserHandle    *string `json:"user_handle,omitempty"` // Optional: passed to auto-create profile
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		logger.Warn("Invalid registration payload", "error", err)
		app.respondWithError(w, errors.New("invalid request payload"), http.StatusBadRequest)
		return
	}

	// --- Basic Field Validation ---
	if input.Email == "" || input.Password == "" {
		app.respondWithError(w, errors.New("email and password are required"), http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	// --- Role Resolution ---
	roleName := "consumer" // Default role
	if input.RequestedRole != "" {
		switch input.RequestedRole {
		case "consumer", "merchant":
			roleName = input.RequestedRole
		default:
			logger.Warn("Unauthorized role requested", "requested_role", input.RequestedRole)
			app.respondWithError(w, errors.New("unauthorized role requested"), http.StatusForbidden)
			return
		}
	}

	role, err := app.Models.Role.GetRoleByName(ctx, roleName)
	if err != nil || role == nil {
		logger.Error("Failed to resolve role", "role_name", roleName, "error", err)
		app.respondWithError(w, errors.New("failed to resolve role"), http.StatusInternalServerError)
		return
	}

	// --- Build Minimal User Struct (profile fields excluded) ---
	user := &data.User{
		Email:    input.Email,
		RoleID:   role.ID,
		IsActive: true,
	}

	// --- Register User Account ---
	userID, err := app.Models.User.Register(ctx, user, input.Password)
	if err != nil {
		logger.Error("User registration failed", "error", err)
		app.respondWithError(w, errors.New("user registration failed"), http.StatusInternalServerError)
		return
	}

	// --- Auto-Create User Profile (non-blocking) ---
	if err := app.createUserProfileAfterRegistration(ctx, userID, input.UserHandle); err != nil {
		logger.Warn("Auto profile creation failed", "user_id", userID, "error", err)
		// Continue — do not fail registration due to profile failure
	}

	// --- Auto‑Create User Wallet (non‑blocking) ---
	if err := app.createUserWalletAfterRegistration(ctx, userID); err != nil {
		logger.Warn("Auto wallet creation failed", "user_id", userID, "error", err)
	}

	// --- Audit Log: register_user ---
	action, err := app.Models.Action.GetByName(ctx, "register_user")
	if err != nil || action == nil {
		logger.Warn("Audit action 'register_user' missing", "error", err)
		actionID, createErr := app.Models.Action.CreateIfNotExists(ctx, "register_user", "Register a new user")
		if createErr != nil {
			logger.Error("Failed to create audit action", "error", createErr)
		} else {
			action = &data.Action{ID: actionID}
		}
	}

	entityType, err := app.Models.EntityType.GetByName(ctx, "users")
	if err != nil || entityType == nil {
		logger.Warn("Audit entity type 'users' missing", "error", err)
		entityTypeID, createErr := app.Models.EntityType.CreateIfNotExists(ctx, "users", "User entity")
		if createErr != nil {
			logger.Error("Failed to create entity type", "error", createErr)
		} else {
			entityType = &data.EntityType{ID: entityTypeID}
		}
	}

	auditFailed := false
	if action != nil && entityType != nil {
		audit := data.AuditLog{
			ID:           uuid.New(),
			UserID:       &userID,
			ActionID:     action.ID,
			EntityTypeID: entityType.ID,
			EntityID:     userID.String(),
		}
		ctxAudit, cancelAudit := context.WithTimeout(context.Background(), cfgTimeout)
		defer cancelAudit()

		if err := app.Models.AuditLog.Insert(ctxAudit, &audit); err != nil {
			// Audit logging failed (partial success)
			logger.Warn("Audit log insertion failed", "error", err)
			auditFailed = true // don't respond yet; continue to activation
		}
	}

	// --- Trigger Activation ---
	ctxWithUserID := context.WithValue(ctx, ctxUserID, userID.String())
	rWithUserID := r.WithContext(ctxWithUserID)
	app.SendActivationLinkHandler(w, rWithUserID)

	if w.Header().Get("Content-Type") != "" {
		return // Activation handler already responded
	}

	// --- Final response (206 if audit failed, else 201) ---
	status := http.StatusCreated
	msg := "User registered successfully. Please check your email or phone to activate your account."
	if auditFailed {
		status = http.StatusPartialContent
		msg = "User registered, but audit logging failed. Please check your email or phone to activate your account."
	}

	// --- Success Response ---
	logger.Info("User registered successfully", "user_id", userID, "audit_failed", auditFailed)
	app.respondWithJSON(w, status, jsonResponse{
		Error:   false,
		Message: msg,
		Data:    userID,
	})
}

// LoginHandler authenticates a user via email/password or social login,
// issues access + refresh tokens, and logs the login event in the audit trail.
func (app *Application) LoginHandler(w http.ResponseWriter, r *http.Request) {
	logger := app.Logger.GetLoggerWithContext(r).WithFunctionName("LoginHandler")

	//  Parse request
	var in struct {
		Provider       string `json:"provider"`
		Email          string `json:"email,omitempty"`
		Password       string `json:"password,omitempty"`
		IDToken        string `json:"id_token,omitempty"`
		AccessToken    string `json:"access_token,omitempty"`
		FacebookAppTok string `json:"facebook_app_token,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		logger.Warn("malformed JSON", "error", err)
		app.respondWithError(w, errors.New("invalid request payload"), http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	// Provider-specific authentication.
	var (
		user *data.User
		err  error
	)

	switch strings.ToLower(strings.TrimSpace(in.Provider)) {
	case "email":
		in.Email = strings.TrimSpace(in.Email)

		if in.Email == "" || in.Password == "" {
			app.respondWithError(
				w,
				errors.New("email and password are required"),
				http.StatusBadRequest,
			)
			return
		}

		user, err = app.Models.User.Authenticate(
			ctx,
			in.Email,
			in.Password,
			"",
			"",
		)

	case "google":
		in.IDToken = strings.TrimSpace(in.IDToken)

		if in.IDToken == "" {
			app.respondWithError(
				w,
				errors.New("id_token is required"),
				http.StatusBadRequest,
			)
			return
		}

		email, verifyErr := app.TokenService.VerifyGoogleToken(
			ctx,
			in.IDToken,
			app.Config.OAuth.GoogleClientID,
		)
		if verifyErr != nil {
			logger.Warn(
				"google token verification failed",
				"error",
				verifyErr,
			)
			app.respondWithError(
				w,
				errors.New("invalid google token"),
				http.StatusUnauthorized,
			)
			return
		}

		user, err = app.Models.User.GetByEmail(ctx, email)

	case "facebook":
		in.AccessToken = strings.TrimSpace(in.AccessToken)
		in.FacebookAppTok = strings.TrimSpace(in.FacebookAppTok)

		if in.AccessToken == "" || in.FacebookAppTok == "" {
			app.respondWithError(
				w,
				errors.New(
					"access_token and facebook_app_token are required",
				),
				http.StatusBadRequest,
			)
			return
		}

		facebookUserID, verifyErr := app.TokenService.VerifyFacebookToken(
			ctx,
			in.AccessToken,
			in.FacebookAppTok,
			app.Config.OAuth.FacebookAppID,
		)
		if verifyErr != nil {
			logger.Warn(
				"facebook token verification failed",
				"error",
				verifyErr,
			)
			app.respondWithError(
				w,
				errors.New("invalid facebook token"),
				http.StatusUnauthorized,
			)
			return
		}

		user, err = app.Models.User.Authenticate(
			ctx,
			"",
			"",
			"",
			facebookUserID,
		)

	default:
		app.respondWithError(
			w,
			errors.New("unsupported authentication provider"),
			http.StatusBadRequest,
		)
		return
	}

	if err != nil || user == nil {
		logger.Warn("authentication failed", "err", err)
		app.respondWithError(w, errors.New("invalid credentials"), http.StatusUnauthorized)
		return
	}

	// Generate & persist tokens
	access, refresh, err := app.TokenService.GenerateTokensPair(ctx, user.ID)
	if err != nil {
		logger.Error("token generation failed", "user_id", user.ID, "err", err)
		app.respondWithError(w, errors.New("internal error"), http.StatusInternalServerError)
		return
	}

	// --- Resolve Audit Action ---
	action, err := app.Models.Action.GetByName(ctx, "login")
	if err != nil || action == nil {
		logger.Warn("Audit action 'login' missing, attempting to create", "error", err)
		actionID, createErr := app.Models.Action.CreateIfNotExists(ctx, "login", "User login event")
		if createErr == nil {
			action = &data.Action{ID: actionID}
		}
	}

	// --- Resolve Audit Entity Type ---
	entityType, err := app.Models.EntityType.GetByName(ctx, "users")
	if err != nil || entityType == nil {
		logger.Warn("Audit entity type 'users' missing, attempting to create", "error", err)
		entityTypeID, createErr := app.Models.EntityType.CreateIfNotExists(ctx, "users", "User entity")
		if createErr == nil {
			entityType = &data.EntityType{ID: entityTypeID}
		}
	}

	// --- Insert Audit Log ---
	if action != nil && entityType != nil {
		audit := data.AuditLog{
			ID:           uuid.New(),
			UserID:       &user.ID,
			ActionID:     action.ID,
			EntityTypeID: entityType.ID,
			EntityID:     user.ID.String(),
		}
		ctxAudit, cancelAudit := context.WithTimeout(context.Background(), cfgTimeout)
		defer cancelAudit()
		if err := app.Models.AuditLog.Insert(ctxAudit, &audit); err != nil {
			// Audit logging failed (partial success)
			logger.Warn("Audit log failed", "error", err)
			app.respondWithJSON(w, http.StatusPartialContent, jsonResponse{
				Error:   false,
				Message: "Login succeeded, but audit logging failed",
				Data: struct {
					AccessToken  string `json:"access_token"`
					RefreshToken string `json:"refresh_token"`
				}{
					AccessToken:  access,
					RefreshToken: refresh,
				},
			})
			return
		}
	}

	// --- Send JSON Response ---
	logger.Info("User login successful", "user_id", user.ID)
	app.respondWithJSON(w, http.StatusOK, jsonResponse{
		Error:   false,
		Message: "Login successful",
		Data: struct {
			AccessToken  string `json:"access_token"`
			RefreshToken string `json:"refresh_token"`
		}{
			AccessToken:  access,
			RefreshToken: refresh,
		},
	})
}

// LinkOAuthAccountInput represents the expected input for linking an OAuth account.
type LinkOAuthAccountInput struct {
	Provider string `json:"provider"` // "google" or "facebook"
	OAuthID  string `json:"oauth_id"` // Google "sub" or Facebook user_id
}

// LinkOAuthAccountHandler allows a user to link a Google or Facebook ID to their account.
func (app *Application) LinkOAuthAccountHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	logger := app.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("LinkOAuthAccountHandler")

	userID := app.getUserIDFromContext(ctx)
	if userID == nil {
		app.respondWithError(w, errors.New("unauthenticated"), http.StatusUnauthorized)
		return
	}

	var input LinkOAuthAccountInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		logger.Error("Invalid JSON", "error", err)
		app.respondWithError(w, errors.New("invalid input"), http.StatusBadRequest)
		return
	}

	input.Provider = strings.ToLower(input.Provider)
	if input.Provider != "google" && input.Provider != "facebook" {
		app.respondWithError(w, errors.New("unsupported provider"), http.StatusBadRequest)
		return
	}
	if input.OAuthID == "" {
		app.respondWithError(w, errors.New("oauth_id is required"), http.StatusBadRequest)
		return
	}

	// Check if OAuth ID is already linked to another account
	exists, err := app.Models.User.IsOAuthIDLinkedToOtherUser(ctx, *userID, input.Provider, input.OAuthID)
	if err != nil {
		logger.Error("OAuth ID lookup failed", err)
		app.respondWithError(w, errors.New("internal error"), http.StatusInternalServerError)
		return
	}
	if exists {
		app.respondWithError(w, errors.New("oauth_id already linked to another user"), http.StatusConflict)
		return
	}

	// Perform the linking
	if err := app.Models.User.LinkOAuthID(ctx, *userID, input.Provider, input.OAuthID); err != nil {
		logger.Error("Failed to link OAuth ID", err)
		app.respondWithError(w, errors.New("failed to link account"), http.StatusInternalServerError)
		return
	}

	// Resolve / create action
	action, err := app.Models.Action.GetByName(ctx, "link_oauth_account")
	if err != nil || action == nil {
		logger.Warn("audit action 'link_oauth_account' missing – creating", "error", err)
		actionID, createErr := app.Models.Action.CreateIfNotExists(ctx, "link_oauth_account",
			"Link a social‑login provider to user account")
		if createErr != nil {
			logger.Error("failed to create audit action", "error", createErr)
		} else {
			action = &data.Action{ID: actionID}
		}
	}

	// Resolve / create entity type
	entityType, err := app.Models.EntityType.GetByName(ctx, "user")
	if err != nil || entityType == nil {
		logger.Warn("audit entity type 'user' missing – creating", "error", err)
		entityTypeID, createErr := app.Models.EntityType.CreateIfNotExists(ctx, "user", "User account")
		if createErr != nil {
			logger.Error("failed to create entity type", "error", createErr)
		} else {
			entityType = &data.EntityType{ID: entityTypeID}
		}
	}

	// Insert audit record
	if action != nil && entityType != nil {
		audit := data.AuditLog{
			ID:           uuid.New(),
			UserID:       userID,
			ActionID:     action.ID,
			EntityTypeID: entityType.ID,
			EntityID:     userID.String(), // the user being modified
		}
		if err := app.Models.AuditLog.Insert(ctx, &audit); err != nil {
			// Audit logging failed (partial success)
			logger.Warn("audit logging failed", "user_id", userID, "error", err)
			app.respondWithJSON(w, http.StatusPartialContent, jsonResponse{
				Error:   false,
				Message: "OAuth account linked, but audit logging failed",
				Data: struct {
					UserID uuid.UUID `json:"user_id"`
					Status string    `json:"status"`
				}{
					UserID: *userID,
					Status: "linked (audit failed)",
				},
			})
			return
		}
	}

	// Success
	logger.Info("Successfully linked OAuth ID", "provider", input.Provider, "user_id", userID)
	app.respondWithJSON(w, http.StatusOK, jsonResponse{
		Error:   false,
		Message: "OAuth account linked successfully",
		Data: struct {
			UserID uuid.UUID `json:"user_id"`
			Status string    `json:"status"`
		}{
			UserID: *userID,
			Status: "linked",
		},
	})
}

// SetPasswordHandler – lets an OAuth‑only user add a traditional password.
//
// Behaviour
// ----------
// • Caller must be authenticated (user ID in context).
// • Accepts tiny JSON payload `{ "new_password": "•••" }`.
// • Enforces a minimum 8‑character length (extend later if you want complexity rules).
// • Writes an **inline** audit record (action: set_password, entity: user).
// • 200 on success, 4xx/5xx on errors (fails “loudly” except for audit log insert).
// -----------------------------------------------------------------------------
func (app *Application) SetPasswordHandler(w http.ResponseWriter, r *http.Request) {
	logger := app.Logger.GetLoggerWithContext(r).WithFunctionName("SetPasswordHandler")

	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	userID := app.getUserIDFromContext(ctx)
	if userID == nil {
		app.respondWithError(w, errors.New("unauthenticated"), http.StatusUnauthorized)
		return
	}

	var input setPasswordInput
	if decodeErr := json.NewDecoder(r.Body).Decode(&input); decodeErr != nil {
		logger.Error("invalid JSON", "error", decodeErr)
		app.respondWithError(w, errors.New("invalid input"), http.StatusBadRequest)
		return
	}
	input.NewPassword = strings.TrimSpace(input.NewPassword)
	if len(input.NewPassword) < 8 {
		app.respondWithError(w, errors.New("password must be at least 8 characters"), http.StatusBadRequest)
		return
	}

	hash, hashErr := security.HashPassword(input.NewPassword)
	if hashErr != nil {
		logger.Error("password hashing failed", "error", hashErr)
		app.respondWithError(w, errors.New("internal error"), http.StatusInternalServerError)
		return
	}

	if updateErr := app.Models.User.SetPasswordHash(ctx, *userID, hash); updateErr != nil {
		logger.Error("failed to set password", "error", updateErr)
		app.respondWithError(w, errors.New("failed to update password"), http.StatusInternalServerError)
		return
	}

	action, actErr := app.Models.Action.GetByName(ctx, "set_password")
	if actErr != nil || action == nil {
		logger.Warn("audit action 'set_password' not found, attempting to create...", "error", actErr)
		actID, createErr := app.Models.Action.CreateIfNotExists(ctx, "set_password", "Set / change account password")
		if createErr != nil {
			logger.Error("failed to create missing audit action", "error", createErr)
		} else {
			action = &data.Action{ID: actID}
		}
	}

	entityType, etErr := app.Models.EntityType.GetByName(ctx, "user")
	if etErr != nil || entityType == nil {
		logger.Warn("audit entity type 'user' not found, attempting to create...", "error", etErr)
		etID, createErr := app.Models.EntityType.CreateIfNotExists(ctx, "user", "System user")
		if createErr != nil {
			logger.Error("failed to create missing entity type", "error", createErr)
		} else {
			entityType = &data.EntityType{ID: etID}
		}
	}

	if action != nil && entityType != nil {
		audit := data.AuditLog{
			ID:           uuid.New(),
			UserID:       userID,
			ActionID:     action.ID,
			EntityTypeID: entityType.ID,
			EntityID:     userID.String(),
		}
		if insertErr := app.Models.AuditLog.Insert(ctx, &audit); insertErr != nil {
			// Audit logging failed (partial success)
			logger.Warn("audit logging failed", "user_id", userID, "error", insertErr)
			app.respondWithJSON(w, http.StatusPartialContent, jsonResponse{
				Error:   false,
				Message: "Password updated, but audit logging failed",
				Data: struct {
					UserID uuid.UUID `json:"user_id"`
					Status string    `json:"status"`
				}{
					UserID: *userID,
					Status: "password_set (audit failed)",
				},
			})
			return
		}
	}

	logger.Info("Password set successfully", "user_id", userID)
	app.respondWithJSON(w, http.StatusOK, jsonResponse{
		Error:   false,
		Message: "Password set successfully",
		Data: struct {
			UserID uuid.UUID `json:"user_id"`
			Status string    `json:"status"`
		}{
			UserID: *userID,
			Status: "password_set",
		},
	})
}

// RefreshTokenHandler exchanges a valid refresh‑token for a fresh access/refresh pair.
//
// Behaviour
// ----------
// • Any caller with a *valid* refresh‑token receives a new JWT + rotated refresh token.
// • Old refresh‑token is revoked immediately (rotation).
// • Inline audit log (`jwt_refresh_token`, entity `user_tokens`).
// • 200 on success, 400/401/500 as appropriate.
func (app *Application) RefreshTokenHandler(w http.ResponseWriter, r *http.Request) {
	logger := app.Logger.GetLoggerWithContext(r).WithFunctionName("RefreshTokenHandler")

	// ────────────────────────────── 1. Decode payload ─────────────────────────────
	var input struct {
		RefreshToken string `json:"refresh_token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil || input.RefreshToken == "" {
		logger.Warn("invalid payload", "err", err)
		app.respondWithError(w, errors.New("invalid request payload"), http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	// ────────────────────────────── 2. Validate token ─────────────────────────────
	dbTok, err := app.Models.Token.ValidateRefreshToken(ctx, input.RefreshToken)
	if err != nil {
		logger.Warn("refresh token invalid/expired", "err", err)
		app.respondWithError(w, errors.New("invalid or expired refresh token"), http.StatusUnauthorized)
		return
	}

	// ────────────────────────────── 3. Rotate token ──────────────────────────────
	//
	// Rotation is mandatory. Replacement tokens must not be issued while the
	// presented refresh token remains valid, because that would permit parallel
	// refresh-token reuse and weaken replay containment.
	if err := app.Models.Token.RevokeRefreshToken(
		ctx,
		input.RefreshToken,
	); err != nil {
		logger.Error(
			"failed to revoke presented refresh token",
			"token_id",
			dbTok.ID,
			"user_id",
			dbTok.UserID,
			"error",
			err,
		)
		app.respondWithError(
			w,
			errors.New("could not rotate refresh token"),
			http.StatusInternalServerError,
		)
		return
	}

	accessTok, newRefreshTok, err := app.TokenService.GenerateTokensPair(
		ctx,
		dbTok.UserID,
	)
	if err != nil {
		logger.Error(
			"replacement token generation failed",
			"user_id",
			dbTok.UserID,
			"revoked_token_id",
			dbTok.ID,
			"error",
			err,
		)
		app.respondWithError(
			w,
			errors.New("could not generate replacement tokens"),
			http.StatusInternalServerError,
		)
		return
	}

	// --- Dynamic Audit Action Resolution ---
	action, err := app.Models.Action.GetByName(ctx, "jwt_refresh_token")
	if err != nil || action == nil {
		logger.Warn("Audit action 'jwt_refresh_token' not found, attempting to create...", "error", err)
		actionID, createErr := app.Models.Action.CreateIfNotExists(ctx, "jwt_refresh_token", "Refresh JWT access token")
		if createErr != nil {
			logger.Error("Failed to create missing audit action", "error", createErr)
		} else {
			action = &data.Action{ID: actionID}
		}
	}

	// --- Dynamic Entity Type Resolution ---
	entityType, err := app.Models.EntityType.GetByName(ctx, "users")
	if err != nil || entityType == nil {
		logger.Warn("Audit entity type 'users' not found, attempting to create...", "error", err)
		entityTypeID, createErr := app.Models.EntityType.CreateIfNotExists(ctx, "users", "User entity")
		if createErr != nil {
			logger.Error("Failed to create missing entity type", "error", createErr)
		} else {
			entityType = &data.EntityType{ID: entityTypeID}
		}
	}

	// --- Insert Audit Log (Fail-safe) ---
	if action != nil && entityType != nil {
		audit := data.AuditLog{
			ID:           uuid.New(),
			UserID:       &dbTok.UserID,
			ActionID:     action.ID,
			EntityTypeID: entityType.ID,
			EntityID:     dbTok.ID.String(),
		}
		ctxAudit, cancelAudit := context.WithTimeout(context.Background(), cfgTimeout)
		defer cancelAudit()

		if err := app.Models.AuditLog.Insert(ctxAudit, &audit); err != nil {
			// Audit logging failed (partial success)
			logger.Warn("Audit log insertion failed", "error", err)
			app.respondWithJSON(w, http.StatusPartialContent, jsonResponse{
				Error:   false,
				Message: "Tokens issued, but audit logging failed",
				Data: struct {
					AccessToken  string `json:"access_token"`
					RefreshToken string `json:"refresh_token"`
				}{
					AccessToken:  accessTok,
					RefreshToken: newRefreshTok,
				},
			})
			return
		}
	}

	// --- Final Response ---
	logger.Info("Refresh token validated and new JWT issued", "user_id", dbTok.UserID)
	app.respondWithJSON(w, http.StatusOK, jsonResponse{
		Error:   false,
		Message: "Tokens issued successfully",
		Data: struct {
			AccessToken  string `json:"access_token"`
			RefreshToken string `json:"refresh_token"`
		}{
			AccessToken:  accessTok,
			RefreshToken: newRefreshTok,
		},
	})
}

// LogoutHandler handles user logout by deleting the refresh token and writing an audit log.
func (app *Application) LogoutHandler(w http.ResponseWriter, r *http.Request) {
	logger := app.Logger.GetLoggerWithContext(r).WithFunctionName("LogoutHandler")

	// --- Decode Request Payload ---
	var input struct {
		RefreshToken string `json:"refresh_token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		logger.Warn("Invalid request payload", "error", err)
		app.respondWithError(w, errors.New("invalid request payload"), http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	// --- Validate Refresh Token ---
	tokenDetails, err := app.Models.Token.ValidateRefreshToken(ctx, input.RefreshToken)
	if err != nil {
		logger.Warn("Invalid or expired refresh token", "error", err)
		app.respondWithError(w, errors.New("invalid or expired refresh token"), http.StatusUnauthorized)
		return
	}

	// --- Delete Refresh Token ---
	if err := app.Models.Token.DeleteRefreshToken(ctx, tokenDetails.ID); err != nil {
		logger.Error("Failed to delete refresh token", "token_id", tokenDetails.ID, "error", err)
		app.respondWithError(w, errors.New("logout failed"), http.StatusInternalServerError)
		return
	}

	// --- Resolve Audit Action (logout) ---
	action, err := app.Models.Action.GetByName(ctx, "logout")
	if err != nil || action == nil {
		logger.Warn("Audit action 'logout' not found, attempting to create...", "error", err)
		actionID, createErr := app.Models.Action.CreateIfNotExists(ctx, "logout", "User logout")
		if createErr != nil {
			logger.Error("Failed to create audit action", "error", createErr)
		} else {
			action = &data.Action{ID: actionID}
		}
	}

	// --- Resolve Entity Type (users) ---
	entityType, err := app.Models.EntityType.GetByName(ctx, "users")
	if err != nil || entityType == nil {
		logger.Warn("Audit entity type 'users' not found, attempting to create...", "error", err)
		entityTypeID, createErr := app.Models.EntityType.CreateIfNotExists(ctx, "users", "User entity")
		if createErr != nil {
			logger.Error("Failed to create entity type", "error", createErr)
		} else {
			entityType = &data.EntityType{ID: entityTypeID}
		}
	}

	// --- Insert Audit Log (Fail-safe) ---
	if action != nil && entityType != nil {
		audit := data.AuditLog{
			ID:           uuid.New(),
			UserID:       &tokenDetails.UserID,
			ActionID:     action.ID,
			EntityTypeID: entityType.ID,
			EntityID:     tokenDetails.UserID.String(),
		}

		ctxAudit, cancelAudit := context.WithTimeout(context.Background(), cfgTimeout)
		defer cancelAudit()

		if err := app.Models.AuditLog.Insert(ctxAudit, &audit); err != nil {
			// Audit logging failed (partial success)
			logger.Warn("Audit log insertion failed", "error", err)
			app.respondWithJSON(w, http.StatusPartialContent, jsonResponse{
				Error:   false,
				Message: "Logged out, but audit logging failed",
				Data: struct {
					UserID uuid.UUID `json:"user_id"`
					Status string    `json:"status"`
				}{
					UserID: tokenDetails.UserID,
					Status: "logged_out (audit failed)",
				},
			})
			return
		}
	}

	// Success
	logger.Info("User logged out successfully", "user_id", tokenDetails.UserID)
	app.respondWithJSON(w, http.StatusOK, jsonResponse{
		Error:   false,
		Message: "Logged out successfully",
		Data: struct {
			UserID uuid.UUID `json:"user_id"`
			Status string    `json:"status"`
		}{
			UserID: tokenDetails.UserID,
			Status: "logged_out",
		},
	})
}

// DeleteMeHandler handles a user's request to soft-delete their own account.
// It enforces permission checks, performs a soft delete with cascade, and logs the action in the audit trail.
func (app *Application) DeleteMeHandler(w http.ResponseWriter, r *http.Request) {
	logger := app.Logger.GetLoggerWithContext(r).WithFunctionName("DeleteMeHandler")

	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	// === Extract User ID from context (injected via middleware) ===
	userID := app.getUserIDFromContext(ctx)
	if userID == nil {
		logger.Warn("Missing user ID in context")
		app.respondWithError(w, errors.New("unauthorized: login required"), http.StatusUnauthorized)
		return
	}

	// === Perform Soft Delete With Cascade ===
	if err := app.Models.User.SoftDeleteWithCascade(ctx, *userID); err != nil {
		logger.Error("Soft delete operation failed", "user_id", *userID, "error", err)
		app.respondWithError(w, fmt.Errorf("failed to delete account: %w", err), http.StatusInternalServerError)
		return
	}

	// === Resolve Audit Action (create if missing) ===
	action, err := app.Models.Action.GetByName(ctx, "delete_own_account")
	if err != nil || action == nil {
		logger.Warn("Audit action 'delete_own_account' not found, creating...", "error", err)
		actionID, createErr := app.Models.Action.CreateIfNotExists(ctx, "delete_own_account", "User deletes their own account")
		if createErr != nil {
			logger.Error("Failed to create audit action", "error", createErr)
		} else {
			action = &data.Action{ID: actionID}
		}
	}

	// === Resolve Audit Entity Type (create if missing) ===
	entityType, err := app.Models.EntityType.GetByName(ctx, "users")
	if err != nil || entityType == nil {
		logger.Warn("Audit entity type 'users' not found, creating...", "error", err)
		entityTypeID, createErr := app.Models.EntityType.CreateIfNotExists(ctx, "users", "User entity")
		if createErr != nil {
			logger.Error("Failed to create entity type", "error", createErr)
		} else {
			entityType = &data.EntityType{ID: entityTypeID}
		}
	}

	// === Insert Audit Log (fail-safe) ===
	if action != nil && entityType != nil {
		audit := &data.AuditLog{
			ID:           uuid.New(),
			UserID:       userID,
			ActionID:     action.ID,
			EntityTypeID: entityType.ID,
			EntityID:     userID.String(),
		}
		auditCtx, auditCancel := context.WithTimeout(context.Background(), cfgTimeout)
		defer auditCancel()

		if err := app.Models.AuditLog.Insert(auditCtx, audit); err != nil {
			// Audit logging failed (partial success)
			logger.Warn("Audit log insert failed", "user_id", *userID, "error", err)
			app.respondWithJSON(w, http.StatusPartialContent, jsonResponse{
				Error:   false,
				Message: "Account deleted, but audit logging failed",
				Data: struct {
					UserID uuid.UUID `json:"user_id"`
					Status string    `json:"status"`
				}{
					UserID: *userID,
					Status: "deleted (audit failed)",
				},
			})
			return
		}
	}

	// === Respond to Client ===
	logger.Info("User account soft-deleted successfully", "user_id", *userID)
	app.respondWithJSON(w, http.StatusOK, jsonResponse{
		Error:   false,
		Message: "Your account has been deleted successfully.",
		Data: struct {
			UserID uuid.UUID `json:"user_id"`
			Status string    `json:"status"`
		}{
			UserID: *userID,
			Status: "deleted",
		},
	})
}

// AdminDeleteUserHandler soft-deletes a user by an admin.
// It performs validation, cascades deletion, and records an audit log.
func (app *Application) AdminDeleteUserHandler(w http.ResponseWriter, r *http.Request) {
	logger := app.Logger.GetLoggerWithContext(r).WithFunctionName("AdminDeleteUserHandler")

	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	// === Extract Acting User ID (admin or automation agent) ===
	actorID := app.getUserIDFromContext(ctx)
	if actorID == nil {
		logger.Error("Missing acting user ID from context")
		app.respondWithError(w, errors.New("unauthorized"), http.StatusUnauthorized)
		return
	}

	// === Extract Target User ID (injected by middleware) ===
	targetUserID := app.getTargetUserIDFromContext(ctx)
	if targetUserID == nil {
		logger.Error("Missing target user ID from context")
		app.respondWithError(w, errors.New("bad request: target user ID missing"), http.StatusBadRequest)
		return
	}

	// === Perform Cascading Soft-Delete ===
	if err := app.Models.User.SoftDeleteWithCascade(ctx, *targetUserID); err != nil {
		logger.Error("Cascade soft-delete failed", "error", err, "target_user_id", *targetUserID)
		app.respondWithError(w, fmt.Errorf("failed to delete user: %w", err), http.StatusInternalServerError)
		return
	}

	// === Audit Action Resolution ===
	action, err := app.Models.Action.GetByName(ctx, "expel_user")
	if err != nil || action == nil {
		logger.Warn("Audit action 'expel_user' missing", "error", err)
		if id, createErr := app.Models.Action.CreateIfNotExists(ctx, "expel_user", "Expel user via admin or automation"); createErr == nil {
			action = &data.Action{ID: id}
		} else {
			logger.Error("Failed to create audit action", "error", createErr)
		}
	}

	// === Entity Type Resolution ===
	entityType, err := app.Models.EntityType.GetByName(ctx, "users")
	if err != nil || entityType == nil {
		logger.Warn("Entity type 'users' missing", "error", err)
		if id, createErr := app.Models.EntityType.CreateIfNotExists(ctx, "users", "User entity"); createErr == nil {
			entityType = &data.EntityType{ID: id}
		} else {
			logger.Error("Failed to create entity type", "error", createErr)
		}
	}

	// === Insert Audit Log ===
	if action != nil && entityType != nil {
		audit := data.AuditLog{
			ID:           uuid.New(),
			UserID:       actorID,
			ActionID:     action.ID,
			EntityTypeID: entityType.ID,
			EntityID:     targetUserID.String(),
		}

		ctxAudit, cancelAudit := context.WithTimeout(context.Background(), cfgTimeout)
		defer cancelAudit()

		if err := app.Models.AuditLog.Insert(ctxAudit, &audit); err != nil {
			// Audit logging failed (partial success)
			logger.Warn("Failed to insert audit log", "error", err)
			app.respondWithJSON(w, http.StatusPartialContent, jsonResponse{
				Error:   false,
				Message: "User expelled, but audit logging failed",
				Data: struct {
					ActorID      uuid.UUID `json:"actor_id"`
					TargetUserID uuid.UUID `json:"target_user_id"`
					Status       string    `json:"status"`
				}{
					ActorID:      *actorID,
					TargetUserID: *targetUserID,
					Status:       "expelled (audit failed)",
				},
			})
			return
		}
	}

	// === Final Response ===
	logger.Info("User expelled successfully", "actor_id", *actorID, "target_user_id", *targetUserID)
	app.respondWithJSON(w, http.StatusOK, jsonResponse{
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
	})
}
