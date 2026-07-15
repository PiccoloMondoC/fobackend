// sdworkspace/sdbackend/internal/server/cmd/api/user_profiles.go
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/PiccoloMondoC/sdworkspace/sdbackend/internal/data"
	"github.com/PiccoloMondoC/sdworkspace/sdbackend/internal/utils/timeutil"

	"github.com/google/uuid"
)

// default when preferred_contact not supplied
const profilePrefContactFallback = "email" 

// Write User Profile Handlers

// createUserProfileAfterRegistration inserts a minimal user profile immediately
// after a successful registration. It is *internal‑only* – never exposed as an
// HTTP endpoint.
//
// Design notes
// ------------
// • NO calls to the services layer. Automation (e.g., auto‑flagging) is handled
//   elsewhere by the services package listening for DB changes / events.
// • Strict validation & normalisation occur synchronously via the model layer.
// • Audit logging is best‑effort and must never block the main flow.
//
func (app *Application) createUserProfileAfterRegistration(
	ctx context.Context,
	userID uuid.UUID,
	rawHandle *string,
) error {
	logger := app.Logger.
		GetLoggerWithContextFromContext(ctx).
		WithFunctionName("createUserProfileAfterRegistration")

	// ---------- Sanity checks ----------
	if userID == uuid.Nil {
		err := errors.New("userID must not be nil")
		logger.Error("missing userID", err)
		return err
	}

	// ---------- Optional handle validation ----------
	var normalisedHandle *string
	if rawHandle != nil {
		// Lower‑case & trim – lightweight normalisation before deeper checks.
		h := strings.ToLower(strings.TrimSpace(*rawHandle))

		// Re‑use existing model‑level validation (format, length, uniqueness,
		// reserved words, etc.). The model returns nil on success.
		if err := app.Models.UserProfile.ValidateUserHandle(ctx, h); err != nil {
			logger.Warn("handle validation failed", "raw_handle", *rawHandle, "error", err)
			return fmt.Errorf("invalid user handle: %w", err)
		}
		normalisedHandle = &h
	}

	// ---------- Build & insert profile ----------
	now := timeutil.Now()
	profile := &data.UserProfile{
		UserID:           userID,
		UserHandle:       normalisedHandle,
		PreferredContact: "email", // safe default; user may change later
		CreatedAt:        now,
		UpdatedAt:        now,
	}

	if err := app.Models.UserProfile.Insert(ctx, profile); err != nil {
		logger.Error("insert user profile failed", "user_id", userID, "error", err)
		return err
	}

	// --- Resolve Audit Action ---
	action, err := app.Models.Action.GetByName(ctx, "create_user_profile")
	if err != nil || action == nil {
		logger.Warn("Audit action 'create_user_profile' missing, attempting to create", "error", err)
		actionID, createErr := app.Models.Action.CreateIfNotExists(ctx, "create_user_profile", "Create a new user profile")
		if createErr != nil {
			logger.Error("Failed to create audit action", "error", createErr)
		} else {
			action = &data.Action{ID: actionID}
		}
	}

	// --- Resolve Entity Type ---
	entityType, err := app.Models.EntityType.GetByName(ctx, "user_profile")
	if err != nil || entityType == nil {
		logger.Warn("Entity type 'user_profile' missing, attempting to create", "error", err)
		entityTypeID, createErr := app.Models.EntityType.CreateIfNotExists(ctx, "user_profile", "User profile entity")
		if createErr != nil {
			logger.Error("Failed to create entity type", "error", createErr)
		} else {
			entityType = &data.EntityType{ID: entityTypeID}
		}
	}

	// --- Perform Audit Log Insert (non-blocking) ---
	if action != nil && entityType != nil {
		audit := data.AuditLog{
			ID:           uuid.New(),
			UserID:       &userID,
			ActionID:     &action.ID,
			EntityTypeID: entityType.ID,
			EntityID:     userID.String(),
			Timestamp:    timeutil.Now(),
		}
		if err := app.Models.AuditLog.Insert(ctx, &audit); err != nil {
			// Audit logging failed (partial success)
			logger.Warn("Audit log failed", "user_id", userID, "error", err)
			// Continue silently
		}
	}

	// Success
	logger.Info("User profile auto-created successfully", "user_id", userID)
	return nil
}


// GetOwnUserProfileHandler retrieves the current authenticated user's profile.
// It does NOT support accessing others' profiles — even as admin/operator.
// - Requires authentication.
// - Extracts user ID from trusted JWT-injected context.
// - Applies context timeout.
// - Performs structured and audit logging.
// - Returns full user profile on success.
func (app *Application) GetOwnUserProfileHandler(w http.ResponseWriter, r *http.Request) {
	logger := app.Logger.GetLoggerWithContext(r).WithFunctionName("GetOwnUserProfileHandler")
	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	// --- Extract Authenticated User ID from Context ---
	userID := app.getUserIDFromContext(ctx)
	if userID == nil {
		app.respondWithError(w, errors.New("unauthorized: login required"), http.StatusUnauthorized)
		return
	}

	// --- Retrieve User Profile by User ID ---
	profile, err := app.Models.UserProfile.GetByUserID(ctx, *userID)
	if err != nil {
		logger.Error("Failed to load user profile", "user_id", userID, "error", err)
		app.respondWithError(w, fmt.Errorf("failed to load profile: %w", err), http.StatusInternalServerError)
		return
	}
	if profile == nil {
		logger.Warn("User profile not found", "user_id", userID)
		app.respondWithError(w, errors.New("profile not found"), http.StatusNotFound)
		return
	}

	// --- Ensure Audit Metadata Exists (Action + EntityType) ---
	actionName := "read_user_profile"
	entityTypeName := "user_profile"

	// Resolve or create audit action
	action, err := app.Models.Action.GetByName(ctx, actionName)
	if err != nil || action == nil {
		logger.Warn("Audit action missing, attempting to create", "action", actionName, "error", err)
		actionID, createErr := app.Models.Action.CreateIfNotExists(ctx, actionName, "Retrieve current user profile")
		if createErr != nil {
			logger.Error("Failed to create audit action", "action", actionName, "error", createErr)
		} else {
			action = &data.Action{ID: actionID}
		}
	}

	// Resolve or create audit entity type
	entityType, err := app.Models.EntityType.GetByName(ctx, entityTypeName)
	if err != nil || entityType == nil {
		logger.Warn("Audit entity type missing, attempting to create", "entity_type", entityTypeName, "error", err)
		entityTypeID, createErr := app.Models.EntityType.CreateIfNotExists(ctx, entityTypeName, "User profile entity")
		if createErr != nil {
			logger.Error("Failed to create entity type", "entity_type", entityTypeName, "error", createErr)
		} else {
			entityType = &data.EntityType{ID: entityTypeID}
		}
	}

	// --- Insert Audit Log (fail-safe) ---
	if userID != nil && action != nil && entityType != nil {
		audit := data.AuditLog{
			ID:           uuid.New(),
			UserID:       userID,
			ActionID:     &action.ID,
			EntityTypeID: entityType.ID,
			EntityID:     userID.String(),
			Timestamp:    time.Now().UTC(),
		}
		if err := app.Models.AuditLog.Insert(ctx, &audit); err != nil {
			// Audit logging failed (partial success)
			logger.Warn("Audit logging failed", "user_id", userID, "error", err)
			app.respondWithJSON(w, http.StatusPartialContent, jsonResponse{
				Error:   false,
				Message: "User profile retrieved, but audit logging failed",
				Data:    profile,
			})
			return
		}
	}

	// --- Respond with User Profile ---
	logger.Info("User profile retrieved successfully", "user_id", userID)
	app.respondWithJSON(w, http.StatusOK, jsonResponse{
		Error:   false,
		Message: "User profile retrieved successfully",
		Data:    profile,
	})
}


// UpdateOwnUserProfileHandler lets an authenticated user update *only* their profile.
//
// Behaviour
// ----------
//   • Only operates on the caller’s profile (admins must use a separate handler).  
//   • Accepts a *partial* JSON payload – fields omitted remain unchanged.  
//   • Validates / normalises user_handle server‑side.  
//   • Writes an audit record (action: update_user_profile, entity: user_profile).  
//   • 200 on success, 400 on bad input, 500 on internal error.
//
func (app *Application) UpdateOwnUserProfileHandler(w http.ResponseWriter, r *http.Request) {
	logger := app.Logger.GetLoggerWithContext(r).WithFunctionName("UpdateOwnUserProfileHandler")

	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	// -------------------------------------------------------------------------
	// 1. Get authenticated user ID from trusted context (set by middleware).
	// -------------------------------------------------------------------------
	userIDPtr := app.getUserIDFromContext(ctx)
	if userIDPtr == nil || *userIDPtr == uuid.Nil {
		app.respondWithError(w, errors.New("unauthorized"), http.StatusUnauthorized)
		return
	}
	userID := *userIDPtr

	// -------------------------------------------------------------------------
	// 2. Decode request JSON into a lightweight DTO.
	//    (Avoid blindly binding into the full data.UserProfile struct.)
	// -------------------------------------------------------------------------
	var input struct {
		FirstName               *string          `json:"first_name,omitempty"`
		LastName                *string          `json:"last_name,omitempty"`
		UserHandle              *string          `json:"user_handle,omitempty"`
		Phone                   *string          `json:"phone,omitempty"`
		AvatarURL               *string          `json:"avatar_url,omitempty"`
		Bio                     *string          `json:"bio,omitempty"`
		Location                *string          `json:"location,omitempty"`
		Website                 *string          `json:"website,omitempty"`
		Company                 *string          `json:"company,omitempty"`
		PreferredContact        *string          `json:"preferred_contact,omitempty"`
		SocialLinks             json.RawMessage `json:"social_links,omitempty"`
		NotificationPreferences json.RawMessage `json:"notification_preferences,omitempty"`
	}
	if err := app.readJSON(w, r, &input); err != nil {
		app.respondWithError(w, err, http.StatusBadRequest)
		return
	}

	// -------------------------------------------------------------------------
	// 3. Validate & normalise user_handle if present (model‑layer validation).
	// -------------------------------------------------------------------------
	if input.UserHandle != nil {
		handle := strings.ToLower(strings.TrimSpace(*input.UserHandle))
		if err := app.Models.UserProfile.ValidateUserHandle(ctx, handle); err != nil {
			app.respondWithError(w, err, http.StatusBadRequest)
			return
		}
		input.UserHandle = &handle
	}

	// -------------------------------------------------------------------------
	// 4. Build the profile struct for update (only allowed fields).
	// -------------------------------------------------------------------------
	profile := data.UserProfile{
		UserID:                  userID,
		FirstName:               input.FirstName,
		LastName:                input.LastName,
		UserHandle:              input.UserHandle,
		Phone:                   input.Phone,
		AvatarURL:               input.AvatarURL,
		Bio:                     input.Bio,
		Location:                input.Location,
		Website:                 input.Website,
		Company:                 input.Company,
		PreferredContact:        derefOr(profilePrefContactFallback, input.PreferredContact),
		SocialLinks:             input.SocialLinks,
		NotificationPreferences: input.NotificationPreferences,
		UpdatedAt:               timeutil.Now(),
	}

	// -------------------------------------------------------------------------
	// 5. Persist the changes.
	// -------------------------------------------------------------------------
	if err := app.Models.UserProfile.Update(ctx, &profile); err != nil {
		logger.Error("update failed", "err", err)
		app.respondWithError(w, errors.New("profile update failed"), http.StatusInternalServerError)
		return
	}

	//--------------------------------------------------------------------
	// 6. **Inline audit logging** (dynamic resolution, fail‑gracefully)
	//--------------------------------------------------------------------

	// Resolve Audit Action
	action, err := app.Models.Action.GetByName(ctx, "update_user_profile")
	if err != nil || action == nil {
		logger.Warn("Audit action 'update_user_profile' not found, attempting to create...", "error", err)
		actionID, createErr := app.Models.Action.CreateIfNotExists(ctx, "update_user_profile", "Update own user profile")
		if createErr != nil {
			logger.Error("Failed to create missing audit action", "error", createErr)
			// allow main op to succeed
		} else {
			action = &data.Action{ID: actionID}
		}
	}

	// Resolve Audit Entity Type
	entityType, err := app.Models.EntityType.GetByName(ctx, "user_profile")
	if err != nil || entityType == nil {
		logger.Warn("Audit entity type 'user_profile' not found, attempting to create...", "error", err)
		entityTypeID, createErr := app.Models.EntityType.CreateIfNotExists(ctx, "user_profile", "User profile entity")
		if createErr != nil {
			logger.Error("Failed to create missing entity type", "error", createErr)
			// allow main op to succeed
		} else {
			entityType = &data.EntityType{ID: entityTypeID}
		}
	}

	// Insert Audit Log (fail‑soft)
	if action != nil && entityType != nil {
		audit := data.AuditLog{
			ID:           uuid.New(),
			UserID:       &userID,
			ActionID:     &action.ID,
			EntityTypeID: entityType.ID,
			EntityID:     userID.String(),
			Timestamp:    timeutil.Now(),
		}
		if err := app.Models.AuditLog.Insert(ctx, &audit); err != nil {
			// Audit logging failed (partial success)
			logger.Warn("Audit logging failed", "user_id", userID, "error", err)
			app.respondWithJSON(w, http.StatusPartialContent, jsonResponse{
				Error:   false,
				Message: "Profile updated, but audit logging failed",
				Data:    userID,
			})
			return
		}
	}

	//--------------------------------------------------------------------
	// 7. Success response
	//--------------------------------------------------------------------
	logger.Info("User profile updated", "user_id", userID)
	app.respondWithJSON(w, http.StatusOK, jsonResponse{
		Error:   false,
		Message: "Profile updated successfully",
		Data:    userID,
	})
}

/*
// UpdateUserProfileVisibilityHandler allows authenticated users to update the visibility of their own profile.
// Only self-updates are permitted — no other users, including admins, may perform this action.
// Merchant accounts are always public and are not allowed to toggle visibility.
// This handler enforces security, performs structured + audit logging, and updates the `is_public` flag.
func (app *Application) UpdateUserProfileVisibilityHandler(w http.ResponseWriter, r *http.Request) {
	logger := app.Logger.GetLoggerWithContext(r).WithFunctionName("UpdateUserProfileVisibilityHandler")

	// --- Apply a context timeout to guard all DB calls ---
	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	// --- Retrieve authenticated user ID from trusted context ---
	userID := app.getUserIDFromContext(ctx)
	if userID == nil {
		logger.Warn("Missing user ID in context")
		app.respondWithError(w, errors.New("unauthorized: login required"), http.StatusUnauthorized)
		return
	}

	// --- Parse input JSON body: expecting { "is_public": true/false } ---
	var input struct {
		IsPublic *bool `json:"is_public"`
	}
	if err := app.readJSON(w, r, &input); err != nil {
		logger.Warn("Invalid JSON input", "error", err)
		app.respondWithError(w, err, http.StatusBadRequest)
		return
	}
	if input.IsPublic == nil {
		logger.Warn("Missing 'is_public' field in request")
		app.respondWithError(w, errors.New("missing required field: is_public"), http.StatusBadRequest)
		return
	}

	// --- Fetch the user's full record (includes role name for enforcement) ---
	user, err := app.Models.User.GetByID(ctx, *userID)
	if err != nil {
		logger.Error("Failed to retrieve user record", "error", err)
		app.respondWithError(w, errors.New("could not verify user role"), http.StatusInternalServerError)
		return
	}
	if user == nil {
		logger.Warn("Authenticated user not found", "user_id", userID)
		app.respondWithError(w, errors.New("user not found"), http.StatusUnauthorized)
		return
	}

	// --- Block visibility updates for merchant roles (case-insensitive check) ---
	if strings.EqualFold(user.RoleName, "merchant") {
		logger.Warn("Merchant user not allowed to change profile visibility",
			"user_id", userID, "role_name", user.RoleName)
		app.respondWithError(w, errors.New("merchant users cannot change profile visibility"), http.StatusForbidden)
		return
	}

	// --- Perform visibility update in the database ---
	err = app.Models.UserProfile.UpdateVisibilityByUserID(ctx, *userID, *input.IsPublic)
	if err != nil {
		logger.Error("Failed to update visibility", "error", err)
		app.respondWithError(w, errors.New("could not update profile visibility"), http.StatusInternalServerError)
		return
	}

	// --- Trigger auto-flagging on updated profile ---
	updatedProfile, err := app.Models.UserProfile.GetByUserID(ctx, *userID)
	if err == nil && updatedProfile != nil {
		go app.Services.UserProfiles.CheckAndAutoFlagProfile(ctx, updatedProfile)
	}


	// --- Audit Metadata ---
	const actionName = "update_own_profile_visibility"
	const entityTypeName = "user_profile"

	// Resolve or create action
	action, err := app.Models.Action.GetByName(ctx, actionName)
	if err != nil || action == nil {
		logger.Warn("Missing audit action, creating", "action", actionName, "error", err)
		actionID, err := app.Models.AuditActions.CreateIfNotExists(ctx, actionName, "Update own profile visibility")
		if err != nil {
			logger.Error("Audit action resolution failed", "error", err)
		}
	}

	// Resolve or create entity type
	entityType, err := app.Models.EntityType.GetByName(ctx, entityTypeName)
	if err != nil || entityType == nil {
		logger.Warn("Missing entity type, creating", "entity_type", entityTypeName, "error", err)
		entityID, err := app.Models.AuditEntityTypes.CreateIfNotExists(ctx, entityTypeName, "User profile entity")
		if err != nil {
			logger.Error("Audit entity type resolution failed", "error", err)
			}
		}

	// Insert audit log (non-blocking on failure)
	_ = app.Models.AuditLogs.Insert(ctx, data.AuditLog{
		ActionID:     actionID,
		EntityTypeID: entityID,
		EntityID:     userID,
		ActorUserID:  userID,
		ActorIsSelf:  true,
		Additional:   fmt.Sprintf(`{"is_public": %t}`, *input.IsPublic),
	})

	// --- Respond with success and updated value ---
	logger.Info("User profile visibility updated", "user_id", userID, "is_public", *input.IsPublic)
	app.writeJSON(w, http.StatusOK, envelope{
		"message":   "visibility updated",
		"is_public": *input.IsPublic,
	}, nil)
}
*/

// Read User Profile Handlers


// GetUserProfileByUserIDHandler retrieves a user's profile by their ID.
// - Public profiles are accessible to all (including guests).
// - Private profiles are restricted to the user themselves or internal roles.
// - Sensitive fields are redacted for unauthorized viewers.
// - Logs all access with fail-safe audit logging.
func (app *Application) GetUserProfileByUserIDHandler(w http.ResponseWriter, r *http.Request) {
	logger := app.Logger.GetLoggerWithContext(r).WithFunctionName("GetUserProfileByUserIDHandler")
	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	// --- Extract Target User ID from Trusted Context ---
	targetID := app.getTargetUserIDFromContext(ctx)
	if targetID == nil {
		logger.Warn("Missing target user ID")
		app.respondWithError(w, errors.New("target user ID is required"), http.StatusBadRequest)
		return
	}

	// --- Fetch Profile from DB ---
	profile, err := app.Models.UserProfile.GetByUserID(ctx, *targetID)
	if err != nil {
		logger.Error("DB error retrieving profile", "target_user_id", targetID, "error", err)
		app.respondWithError(w, fmt.Errorf("failed to retrieve profile: %w", err), http.StatusInternalServerError)
		return
	}
	if profile == nil {
		logger.Warn("Profile not found", "target_user_id", targetID)
		app.respondWithError(w, errors.New("profile not found"), http.StatusNotFound)
		return
	}

	// --- Role and Viewer Identity Checks ---
	requesterID := app.getUserIDFromContext(ctx)
	roleName, _ := app.getRoleFromContextOrDB(ctx)

	isSelf := requesterID != nil && *requesterID == *targetID
	isInternal := roleName == "admin" || roleName == "internal_operator"
	isPublic := profile.PreferredContact != "private"

	if !isSelf && !isInternal && !isPublic {
		logger.Warn("Access denied: private profile", "requester_id", requesterID, "target_user_id", targetID, "role", roleName)
		app.respondWithError(w, errors.New("access denied: profile is private"), http.StatusForbidden)
		return
	}

	// --- Redact Sensitive Fields for Non-Privileged Viewers ---
	if !isSelf && !isInternal {
		profile.Phone = nil
		profile.NotificationPreferences = nil
		profile.SocialLinks = nil
	}

	// --- Audit Logging Setup ---
	const actionName = "read_user_profile_by_user_id"
	const entityTypeName = "user_profile"

	// Ensure audit action exists
	action, err := app.Models.Action.GetByName(ctx, actionName)
	if err != nil || action == nil {
		logger.Warn("Missing audit action; attempting dynamic creation", "action", actionName, "error", err)
		if id, createErr := app.Models.Action.CreateIfNotExists(ctx, actionName, "Retrieve user profile by ID"); createErr == nil {
			action = &data.Action{ID: id}
		} else {
			logger.Error("Failed to create audit action", "action", actionName, "error", createErr)
		}
	}

	// Ensure entity type exists
	entityType, err := app.Models.EntityType.GetByName(ctx, entityTypeName)
	if err != nil || entityType == nil {
		logger.Warn("Missing entity type; attempting dynamic creation", "entity_type", entityTypeName)
		if id, createErr := app.Models.EntityType.CreateIfNotExists(ctx, entityTypeName, "User profile entity"); createErr == nil {
			entityType = &data.EntityType{ID: id}
		} else {
			logger.Error("Failed to create entity type", "error", createErr)
		}
	}

	// --- Insert Audit Log (non-blocking failure) ---
	if requesterID != nil && action != nil && entityType != nil {
		audit := data.AuditLog{
			ID:           uuid.New(),
			UserID:       requesterID,
			ActionID:     &action.ID,
			EntityTypeID: entityType.ID,
			EntityID:     targetID.String(),
			Timestamp:    time.Now().UTC(),
		}
		if err := app.Models.AuditLog.Insert(ctx, &audit); err != nil {
			// Audit logging failed (partial success)
			logger.Error("Audit log insert failed", "error", err)
			app.respondWithJSON(w, http.StatusPartialContent, jsonResponse{
				Error:   false,
				Message: "Profile retrieved, but audit logging failed",
				Data:    profile,
			})
			return
		}
	}

	// --- Search Engine Metadata Control (Frontend Hint) ---
	// NOTE: The frontend should use profile.SearchIndexed to render appropriate <meta name="robots"> tags.

	// Success
	logger.Info("Profile retrieved successfully", "target_user_id", targetID, "requester_id", requesterID)
	app.respondWithJSON(w, http.StatusOK, jsonResponse{
		Error:   false,
		Message: "Profile retrieved successfully",
		Data:    profile,
	})
}


// ListUserProfilesHandler returns a list of user profiles viewable by the current user.
// - Enforces read_user_profiles permission.
// - Authenticated users can see public profiles of others.
// - Admins/operators may see private profiles as well.
// - Guest access is denied (auth required).
// - Uses pagination (limit + offset) from context.
// - Structured and audit logging applied throughout.
func (app *Application) ListUserProfilesHandler(w http.ResponseWriter, r *http.Request) {
	logger := app.Logger.GetLoggerWithContext(r).WithFunctionName("ListUserProfilesHandler")
	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	// --- Extract Requester Identity ---
	requesterID := app.getUserIDFromContext(ctx)
	if requesterID == nil {
		logger.Warn("Missing authenticated user ID in context")
		app.respondWithError(w, errors.New("unauthorized"), http.StatusUnauthorized)
		return
	}

	roleName, _ := app.getRoleFromContextOrDB(ctx)
	isInternal := roleName == "admin" || roleName == "internal_operator"

	//  Parse pagination parameters (?limit & ?offset). Defaults: 50/0.
	limit, offset, err := app.parseLimitOffset(r)
	if err != nil {
		app.respondWithError(w, err, http.StatusBadRequest)
		return
	}

	// --- Fetch Profiles ---
	allProfiles, err := app.Models.UserProfile.GetAll(ctx, limit, offset)
	if err != nil {
		logger.Error("Failed to query user profiles", "error", err)
		app.respondWithError(w, errors.New("could not retrieve profiles"), http.StatusInternalServerError)
		return
	}

	// --- Filter Profiles Based on Visibility ---
	var visible []*data.UserProfile
	for _, p := range allProfiles {
		// Self or internal users can view all
		if p.UserID == *requesterID || isInternal {
			visible = append(visible, p)
			continue
		}

		// Skip private profiles
		if strings.ToLower(p.PreferredContact) == "private" {
			continue
		}

		// Redact sensitive fields for external viewers
		p.Phone = nil
		p.NotificationPreferences = nil
		p.SocialLinks = nil
		visible = append(visible, p)
	}

	// --- Audit Logging (non-blocking) ---
	const actionName = "read_user_profiles"
	const entityTypeName = "user_profile"

	action, err := app.Models.Action.GetByName(ctx, actionName)
	if err != nil || action == nil {
		logger.Warn("Missing audit action, creating", "action", actionName)
		if id, createErr := app.Models.Action.CreateIfNotExists(ctx, actionName, "Retrieve multiple user profiles via search or listing"); createErr == nil {
			action = &data.Action{ID: id}
		} else {
			logger.Error("Audit action creation failed", "error", createErr)
		}
	}

	entityType, err := app.Models.EntityType.GetByName(ctx, entityTypeName)
	if err != nil || entityType == nil {
		logger.Warn("Missing entity type, creating", "entity_type", entityTypeName)
		if id, createErr := app.Models.EntityType.CreateIfNotExists(ctx, entityTypeName, "User profile entity"); createErr == nil {
			entityType = &data.EntityType{ID: id}
		} else {
			logger.Error("Entity type creation failed", "error", createErr)
		}
	}

	if action != nil && entityType != nil {
		audit := data.AuditLog{
			ID:           uuid.New(),
			UserID:       requesterID,
			ActionID:     &action.ID,
			EntityTypeID: entityType.ID,
			EntityID:     "batch_list",
			Timestamp:    time.Now().UTC(),
		}
		if err := app.Models.AuditLog.Insert(ctx, &audit); err != nil {
			// Audit logging failed (partial success)
			logger.Error("Audit log insert failed", "error", err)
			app.respondWithJSON(w, http.StatusPartialContent, jsonResponse{
				Error:   false,
				Message: "User profiles listed, but audit logging failed",
				Data:    visible,
			})
			return
		}
	}

	// --- Response ---
	logger.Info("User profiles listed", "visible_count", len(visible), "role", roleName)
	app.respondWithJSON(w, http.StatusOK, jsonResponse{
		Error:   false,
		Message: "User profiles listed successfully",
		Data:    visible,
	})
}


// SearchUserProfilesHandler handles full-text search across public user profiles.
// - Requires authentication to prevent abuse and tie activity to user identity.
// - Supports query parameters: `q` (search term), `limit`, `offset`.
// - Uses FTS + fallback ILIKE across first_name, last_name, user_handle, and bio.
// - Structured logging, audit logging, safe context timeout, dynamic metadata resolution.
func (app *Application) SearchUserProfilesHandler(w http.ResponseWriter, r *http.Request) {
	logger := app.Logger.GetLoggerWithContext(r).WithFunctionName("SearchUserProfilesHandler")
	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	// --- Require authenticated user ---
	userID := app.getUserIDFromContext(ctx)
	if userID == nil {
		logger.Warn("Unauthorized access to profile search")
		app.respondWithError(w, errors.New("unauthorized"), http.StatusUnauthorized)
		return
	}

	// --- Parse query string ---
	queryText := r.URL.Query().Get("q")
	if strings.TrimSpace(queryText) == "" {
		logger.Warn("Missing query parameter 'q'")
		app.respondWithError(w, errors.New("query parameter 'q' is required"), http.StatusBadRequest)
		return
	}

	//  Parse pagination parameters (?limit & ?offset). Defaults: 50/0.
	limit, offset, err := app.parseLimitOffset(r)
	if err != nil {
		app.respondWithError(w, err, http.StatusBadRequest)
		return
	}

	// --- Perform DB Search ---
	results, err := app.Models.UserProfile.SearchUserProfiles(ctx, queryText, limit, offset)
	if err != nil {
		logger.Error("Search failed", "query", queryText, "error", err)
		app.respondWithError(w, fmt.Errorf("search failed: %w", err), http.StatusInternalServerError)
		return
	}

	// --- Audit Logging ---
	const actionName = "read_user_profiles"
	const entityTypeName = "user_profile"

	action, err := app.Models.Action.GetByName(ctx, actionName)
	if err != nil || action == nil {
		logger.Warn("Missing audit action; attempting creation", "action", actionName)
		if id, createErr := app.Models.Action.CreateIfNotExists(ctx, actionName, "Retrieve multiple user profiles via search or listing"); createErr == nil {
			action = &data.Action{ID: id}
		} else {
			logger.Error("Failed to create audit action", "error", createErr)
		}
	}

	entityType, err := app.Models.EntityType.GetByName(ctx, entityTypeName)
	if err != nil || entityType == nil {
		logger.Warn("Missing entity type; attempting creation", "entity_type", entityTypeName)
		if id, createErr := app.Models.EntityType.CreateIfNotExists(ctx, entityTypeName, "User profile entity"); createErr == nil {
			entityType = &data.EntityType{ID: id}
		} else {
			logger.Error("Failed to create entity type", "error", createErr)
		}
	}

	// Audit log entry (non-fatal)
	if action != nil && entityType != nil {
		audit := data.AuditLog{	
			ID:           uuid.New(),
			UserID:       userID,
			ActionID:     &action.ID,
			EntityTypeID: entityType.ID,
			EntityID:     "search",
			Timestamp:    time.Now().UTC(),
		}
		if err := app.Models.AuditLog.Insert(ctx, &audit); err != nil {
			// Audit logging failed (partial success)
			logger.Warn("Audit logging failed", "user_id", userID, "query", queryText, "error", err)
			app.respondWithJSON(w, http.StatusPartialContent, jsonResponse{
				Error:   false,
				Message: "Search completed, but audit logging failed",
				Data:    results,
			})
			return
		}
	}

	// Success
	logger.Info("Search completed", "user_id", userID, "query", queryText, "result_count", len(results))
	app.respondWithJSON(w, http.StatusOK, jsonResponse{
		Error:   false,
		Message: "Search completed",
		Data:    results,
	})
}


// Restricted or Internal User Profile Handlers


// UpdateUserProfileHandler lets an *internal* operator (admin or moderator)
// update another user’s profile.
//
// Security / Behaviour
// --------------------
// • Requires HasPermission(ctx,"update_user_profile") (route-level middleware).  
// • Self‑updates are blocked – callers must use the “/self” endpoint.  
// • Accepts a *partial* JSON payload; omitted fields remain unchanged.  
// • Validates / normalises user_handle via the model layer.  
// • Writes an audit record (action: update_user_profile, entity: user_profile).  
// • 200 on success, 400 on bad input, 401/403 on auth failures, 500 on DB errors.
func (app *Application) UpdateUserProfileHandler(w http.ResponseWriter, r *http.Request) {
	logger := app.Logger.GetLoggerWithContext(r).WithFunctionName("UpdateUserProfileHandler")

	// Hard limit all downstream work (DB + logging) to cfgTimeout.
	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	/* --------------------------------------------------------------------- *
	 * 1. Extract & validate trusted context IDs                             *
	 * --------------------------------------------------------------------- */
	requesterID := app.getUserIDFromContext(ctx)
	targetID    := app.getTargetUserIDFromContext(ctx)

	if requesterID == nil || targetID == nil {
		logger.Warn("context IDs missing")
		app.respondWithError(w, errors.New("unauthorized"), http.StatusUnauthorized)
		return
	}
	if *requesterID == *targetID {
		logger.Warn("self‑update attempted", "user_id", requesterID)
		app.respondWithError(w, errors.New("forbidden: use /self to update your own profile"), http.StatusForbidden)
		return
	}

	/* --------------------------------------------------------------------- *
	 * 2. Decode JSON payload into a temp struct                             *
	 * --------------------------------------------------------------------- */
	var input data.UserProfile
	if err := app.readJSON(w, r, &input); err != nil {
		logger.Warn("invalid JSON", "error", err)
		app.respondWithError(w, fmt.Errorf("invalid payload: %w", err), http.StatusBadRequest)
		return
	}

	/* --------------------------------------------------------------------- *
	 * 3. Normalise + validate optional handle                               *
	 * --------------------------------------------------------------------- */
	if input.UserHandle != nil {
		handle := strings.ToLower(strings.TrimSpace(*input.UserHandle))
		if err := app.Models.UserProfile.ValidateUserHandle(ctx, handle); err != nil {
			logger.Warn("handle validation failed", "error", err)
			app.respondWithError(w, err, http.StatusBadRequest)
			return
		}
		input.UserHandle = &handle
	}

	/* --------------------------------------------------------------------- *
	 * 4. Persist update                                                     *
	 * --------------------------------------------------------------------- */
	input.UserID    = *targetID
	input.UpdatedAt = time.Now().UTC()

	if err := app.Models.UserProfile.Update(ctx, &input); err != nil {
		logger.Error("update failed", "user_id", targetID, "error", err)
		app.respondWithError(w, errors.New("failed to update profile"), http.StatusInternalServerError)
		return
	}

	// --- Resolve Audit Action ---
	action, err := app.Models.Action.GetByName(ctx, "update_user_profile")
	if err != nil || action == nil {
		logger.Warn("Audit action missing", "action", "update_user_profile", "error", err)
		if id, createErr := app.Models.Action.CreateIfNotExists(ctx, "update_user_profile", "Update a user profile"); createErr == nil {
			action = &data.Action{ID: id}
		} else {
			logger.Error("Audit action creation failed", "error", createErr)
		}
	}

	// --- Ensure Entity Type Exists ---
	entityType, err := app.Models.EntityType.GetByName(ctx, "user_profile")
	if err != nil || entityType == nil {
		logger.Warn("Entity type missing", "entity_type", "user_profile", "error", err)
		if id, createErr := app.Models.EntityType.CreateIfNotExists(ctx, "user_profile", "User profile entity"); createErr == nil {
			entityType = &data.EntityType{ID: id}
		} else {
			logger.Error("Entity type creation failed", "error", createErr)
		}
	}

	// --- Insert Audit Log (Non-Blocking) ---
	if action != nil && entityType != nil {
		audit := data.AuditLog{
			ID:           uuid.New(),
			UserID:       requesterID,
			ActionID:     &action.ID,
			EntityTypeID: entityType.ID,
			EntityID:     targetID.String(),
			Timestamp:    time.Now().UTC(),
		}
		if err := app.Models.AuditLog.Insert(ctx, &audit); err != nil {
			// Audit logging failed (partial success)
			logger.Warn("Audit log insertion failed", "requester_id", requesterID, "target_id", targetID, "error", err)
			app.respondWithJSON(w, http.StatusPartialContent, jsonResponse{
				Error:   false,
				Message: "User profile updated, but audit logging failed",
				Data:    targetID,
			})
			return
		}
	}

	// --- Success Response ---
	logger.Info("User profile updated by internal operator", "updated_user_id", targetID, "by_user_id", requesterID)
	app.respondWithJSON(w, http.StatusOK, jsonResponse{
		Error:   false,
		Message: "User profile updated successfully",
		Data:    targetID,
	})
}


// ModerateUserProfileHandler allows internal operators to flag or unflag a user profile with notes.
// - Requires "moderate_user_profile" permission.
// - Only internal users can act on others' profiles.
// - Performs audit logging and structured error handling.
func (app *Application) ModerateUserProfileHandler(w http.ResponseWriter, r *http.Request) {
	logger := app.Logger.GetLoggerWithContext(r).WithFunctionName("ModerateUserProfileHandler")

	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	// --- Extract Required IDs ---
	targetID := app.getTargetUserIDFromContext(ctx)
	requesterID := app.getUserIDFromContext(ctx)

	if targetID == nil || requesterID == nil {
		logger.Warn("Missing context IDs", "targetID", targetID, "requesterID", requesterID)
		app.respondWithError(w, errors.New("unauthorized or malformed request"), http.StatusUnauthorized)
		return
	}

	// --- Parse Input Payload ---
	var input struct {
		IsFlagged bool   `json:"is_flagged"`
		Notes     string `json:"notes"`
	}
	if err := app.readJSON(w, r, &input); err != nil {
		logger.Warn("Invalid JSON input", "error", err)
		app.respondWithError(w, fmt.Errorf("invalid input: %w", err), http.StatusBadRequest)
		return
	}

	// --- Perform Flag Operation ---
	err := app.Models.UserProfile.FlagUserProfile(ctx, *targetID, input.IsFlagged, input.Notes)
	if err != nil {
		logger.Error("Failed to flag user profile", "user_id", targetID, "error", err)
		app.respondWithError(w, fmt.Errorf("unable to moderate user profile: %w", err), http.StatusInternalServerError)
		return
	}

	// --- Ensure Audit Metadata Exists ---
	const actionName = "moderate_user_profile"
	const entityTypeName = "user_profile"

	action, err := app.Models.Action.GetByName(ctx, actionName)
	if err != nil || action == nil {
		logger.Warn("Missing audit action, creating", "action", actionName)
		if id, createErr := app.Models.Action.CreateIfNotExists(ctx, actionName, "Flag or unflag user profile for violations"); createErr == nil {
			action = &data.Action{ID: id}
		} else {
			logger.Error("Failed to create audit action", "error", createErr)
		}
	}

	entityType, err := app.Models.EntityType.GetByName(ctx, entityTypeName)
	if err != nil || entityType == nil {
		logger.Warn("Missing entity type, creating", "entity_type", entityTypeName)
		if id, createErr := app.Models.EntityType.CreateIfNotExists(ctx, entityTypeName, "User profile entity"); createErr == nil {
			entityType = &data.EntityType{ID: id}
		} else {
			logger.Error("Failed to create entity type", "error", createErr)
		}
	}

	// --- Insert Audit Log (non-blocking failure) ---
	if action != nil && entityType != nil {
		audit := data.AuditLog{
			ID:           uuid.New(),
			UserID:       requesterID,
			ActionID:     &action.ID,
			EntityTypeID: entityType.ID,
			EntityID:     targetID.String(),
			Timestamp:    time.Now().UTC(),
		}
		if err := app.Models.AuditLog.Insert(ctx, &audit); err != nil {
			// Audit logging failed (partial success)
			logger.Error("Failed to write audit log", "error", err)
			app.respondWithJSON(w, http.StatusPartialContent, jsonResponse{
				Error:   false,
				Message: "User profile moderation complete, but audit logging failed",
				Data:    targetID,
			})
			return
		}
	}

	// --- Success Response ---
	logger.Info("Profile moderation complete", "user_id", targetID, "flagged", input.IsFlagged)
	app.respondWithJSON(w, http.StatusOK, jsonResponse{
		Error:   false,
		Message: "User profile moderation complete",
		Data:    targetID,
	})
}

/* Should this even exist as a handler. Seems more suited for automation

// TriggerAutoFlagUserProfilesHandler allows privileged internal users to manually trigger
// batch auto-flagging of user profiles for policy violations.
//
// - Only internal admins and operators may invoke this endpoint.
// - Requires `trigger_auto_flag_profiles` permission.
// - Executes the AutoFlagUserProfiles batch moderation function.
// - Uses structured logging, audit logging, and safe error reporting.
func (app *Application) TriggerAutoFlagUserProfilesHandler(w http.ResponseWriter, r *http.Request) {
	logger := app.Logger.GetLoggerWithContext(r).WithFunctionName("TriggerAutoFlagUserProfilesHandler")
	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	// --- Extract Requester ID from Trusted Context ---
	requesterID := app.getUserIDFromContext(ctx)
	if requesterID == nil {
		logger.Warn("Missing requester ID in context")
		app.respondWithError(w, errors.New("unauthorized: requester ID missing"), http.StatusUnauthorized)
		return
	}

	// --- Execute Batch Auto Moderation ---
	if err := app.Services.UserProfiles.AutoFlagUserProfiles(ctx); err != nil {
		logger.Error("Batch auto moderation failed", "requester_id", requesterID, "error", err)
		app.respondWithError(w, fmt.Errorf("auto-flag operation failed: %w", err), http.StatusInternalServerError)
		return
	}

	// --- Audit Metadata ---
	const actionName = "trigger_auto_flag_profiles"
	const entityTypeName = "user_profile"

	// Resolve or create audit action
	action, err := app.Models.Action.GetByName(ctx, actionName)
	if err != nil || action == nil {
		logger.Warn("Audit action missing, creating", "action", actionName, "error", err)
		id, createErr := app.Models.Action.CreateIfNotExists(ctx, actionName, "Triggered batch auto-flagging of user profiles")
		if createErr == nil {
			action = &data.Action{ID: id}
		} else {
			logger.Error("Failed to create audit action", "error", createErr)
		}
	}

	// Resolve or create entity type
	entityType, err := app.Models.EntityType.GetByName(ctx, entityTypeName)
	if err != nil || entityType == nil {
		logger.Warn("Entity type missing, creating", "entity_type", entityTypeName, "error", err)
		id, createErr := app.Models.EntityType.CreateIfNotExists(ctx, entityTypeName, "User profile entity")
		if createErr == nil {
			entityType = &data.EntityType{ID: id}
		} else {
			logger.Error("Failed to create entity type", "error", createErr)
		}
	}

	// Insert audit log (non-blocking on failure)
	if action != nil && entityType != nil {
		audit := data.AuditLog{
			ID:           uuid.New(),
			UserID:       requesterID,
			ActionID:     &action.ID,
			EntityTypeID: entityType.ID,
			EntityID:     "batch", // Not tied to a specific user, mark as "batch"
			Timestamp:    time.Now().UTC(),
		}
		if err := app.Models.AuditLog.Insert(ctx, &audit); err != nil {
			logger.Warn("Audit log insertion failed", "requester_id", requesterID, "error", err)
		}
	}

	// --- Success Response ---
	logger.Info("Batch auto moderation completed successfully", "triggered_by", requesterID)
	app.respondWithJSON(w, http.StatusOK, map[string]any{
		"message": "Auto-flag user profiles batch completed successfully",
	})
}
*/

// GetReservedHandlesHandler returns all reserved or claimed user handles for validation UI.
//
// Access Control:
// - Only internal admins and operators with the 'read_reserved_handles' permission may access this endpoint.
// - Enforces context-injected identity, permission checks, structured logging, and audit logging.
//
// Returns:
// - 200 OK with list of reserved handles on success.
// - 401 Unauthorized or 403 Forbidden on access issues.
// - 500 Internal Server Error on unexpected failure.
func (app *Application) GetReservedHandlesHandler(w http.ResponseWriter, r *http.Request) {
	logger := app.Logger.GetLoggerWithContext(r).WithFunctionName("GetReservedHandlesHandler")

	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	// --- Extract Requester ID from Trusted Context ---
	requesterID := app.getUserIDFromContext(ctx)
	if requesterID == nil {
		logger.Warn("Missing requester ID in context")
		app.respondWithError(w, errors.New("unauthorized: requester ID missing"), http.StatusUnauthorized)
		return
	}

	// --- Enforce Internal Permission Check ---
	if !app.RequireInternalPermission(ctx, "read_reserved_handles") {
		logger.Warn("Access denied: requires both internal role and permission",
			"user_id", requesterID.String())
		app.respondWithError(w, errors.New("forbidden: requires internal role and permission"), http.StatusForbidden)
		return
	}

	// --- Retrieve Reserved Handles from DB and In-Memory Set ---
	handles, err := app.Models.UserProfile.GetReservedHandles(ctx)
	if err != nil {
		logger.Error("Failed to load reserved handles", "error", err)
		app.respondWithError(w, fmt.Errorf("failed to retrieve reserved handles: %w", err), http.StatusInternalServerError)
		return
	}

	// --- Audit Logging Setup ---
	const actionName = "read_reserved_handles"
	const entityTypeName = "user_profile"

	// Ensure audit action exists
	action, err := app.Models.Action.GetByName(ctx, actionName)
	if err != nil || action == nil {
		logger.Warn("Missing audit action; attempting creation", "action", actionName)
		if id, createErr := app.Models.Action.CreateIfNotExists(ctx, actionName, "Retrieve reserved user handles"); createErr == nil {
			action = &data.Action{ID: id}
		} else {
			logger.Error("Failed to create audit action", "error", createErr)
		}
	}

	// Ensure entity type exists
	entityType, err := app.Models.EntityType.GetByName(ctx, entityTypeName)
	if err != nil || entityType == nil {
		logger.Warn("Missing entity type; attempting creation", "entity_type", entityTypeName)
		if id, createErr := app.Models.EntityType.CreateIfNotExists(ctx, entityTypeName, "User profile entity"); createErr == nil {
			entityType = &data.EntityType{ID: id}
		} else {
			logger.Error("Failed to create entity type", "error", createErr)
		}
	}

	// Insert audit log (non-blocking failure)
	if action != nil && entityType != nil {
		audit := data.AuditLog{
			ID:           uuid.New(),
			UserID:       requesterID,
			ActionID:     &action.ID,
			EntityTypeID: entityType.ID,
			EntityID:     "system", // No specific profile acted upon
			Timestamp:    time.Now().UTC(),
		}
		if err := app.Models.AuditLog.Insert(ctx, &audit); err != nil {
			// Audit logging failed (partial success)
			logger.Error("Audit log insert failed", "error", err)
			app.respondWithJSON(w, http.StatusPartialContent, jsonResponse{
				Error:   false,
				Message: "Reserved handles retrieved, but audit logging failed",
				Data:    handles,
			})
			return
		}
	}

	// --- Success Response ---
	logger.Info("Reserved handles retrieved", "count", len(handles))
	app.respondWithJSON(w, http.StatusOK, jsonResponse{
		Error:   false,
		Message: "Reserved handles retrieved successfully",
		Data:    handles,
	})
}
