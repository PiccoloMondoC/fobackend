// sdworkspace/sdbackend/internal/server/cmd/api/oauth_user_info.go
//   Release Class: DEFERRED
package main

import (
	"context"
	"errors"
	"net/http"

	"github.com/PiccoloMondoC/sdworkspace/sdbackend/internal/data"

	"github.com/google/uuid"
)

// OauthUserInfoHandler returns profile information for the
// authenticated resource owner, per OAuth2/OpenID Connect “userinfo”.
func (app *Application) OauthUserInfoHandler(w http.ResponseWriter, r *http.Request) {
	logger := app.Logger.GetLoggerWithContext(r).WithFunctionName("OauthUserInfoHandler")

	// ────────────────────────────────────────────────────────────────
	// 1. Extract caller identity (uuid.UUID injected by AuthMiddleware)
	// ────────────────────────────────────────────────────────────────
	userID, ok := r.Context().Value(ctxUserID).(uuid.UUID)
	if !ok || userID == uuid.Nil {
		logger.Warn("missing userID in context", "remote_ip", r.RemoteAddr)
		app.respondWithError(w, errors.New("unauthorized"), http.StatusUnauthorized)
		return
	}

	// ────────────────────────────────────────────────────────────────
	// 2. Fetch public profile from existing UserModel
	// ────────────────────────────────────────────────────────────────
	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	user, err := app.Models.User.GetByID(ctx, userID)
	if err != nil {
		if errors.Is(err, data.ErrRecordNotFound) {
			app.respondWithError(w, errors.New("user not found"), http.StatusNotFound)
			return
		}
		logger.Error("profile lookup failed", err, "user_id", userID)
		app.respondWithError(w, errors.New("server error"), http.StatusInternalServerError)
		return
	}

	// 3️  Public profile (user_profiles table) – not mandatory
	profile, _ := app.Models.UserProfile.GetByUserID(ctx, userID) // ignore ErrRecordNotFound

	// ────────────────────────────────────────────────────────────────
	// 4. Audit logging ‑‑ EXACT pattern you endorsed
	// ────────────────────────────────────────────────────────────────

	// Resolve Audit Action
	action, err := app.Models.Action.GetByName(ctx, "get_user_info")
	if err != nil || action == nil {
		logger.Warn("Audit action 'get_user_info' not found, attempting to create...", "error", err)
		actionID, createErr := app.Models.Action.CreateIfNotExists(
			ctx, "get_user_info", "Retrieve authenticated user profile")
		if createErr != nil {
			logger.Error("Failed to create missing audit action", "error", createErr)
			// Allow main operation to succeed
		} else {
			action = &data.Action{ID: actionID}
		}
	}

	// Resolve Audit Entity Type
	entityType, err := app.Models.EntityType.GetByName(ctx, "user")
	if err != nil || entityType == nil {
		logger.Warn("Audit entity type 'user' not found, attempting to create...", "error", err)
		entityTypeID, createErr := app.Models.EntityType.CreateIfNotExists(
			ctx, "user", "End‑user account")
		if createErr != nil {
			logger.Error("Failed to create missing entity type", "error", createErr)
			// Allow main operation to succeed
		} else {
			entityType = &data.EntityType{ID: entityTypeID}
		}
	}

	// Insert Audit Log (fail‑gracefully)
	if action != nil && entityType != nil {
		al := data.AuditLog{
			ID:           uuid.New(),
			UserID:       &userID,
			ActionID:     action.ID,
			EntityTypeID: entityType.ID,
			EntityID:     userID.String(), // public identifier for the user
		}
		if err := app.Models.AuditLog.Insert(ctx, &al); err != nil {
			logger.Warn("Audit logging failed", "user_id", userID, "error", err)
		}
	}

	// 5️⃣  Build response (safe fields only)
	resp := envelope{
		"user": map[string]any{
			"id":          user.ID,
			"email":       user.Email,
			"first_name":  profile.FirstName,
			"last_name":   profile.LastName,
			"user_handle": profile.UserHandle,
			"avatar_url":  profile.AvatarURL,
			"created_at":  user.CreatedAt,
		},
	}

	app.writeJSON(w, http.StatusOK, resp, nil)
}