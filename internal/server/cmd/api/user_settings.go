// sdworkspace/sdbackend/internal/server/cmd/api/user_settings.go
package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/PiccoloMondoC/sdworkspace/sdbackend/internal/data"
	"github.com/PiccoloMondoC/sdworkspace/sdbackend/internal/utils/timeutil"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	//"github.com/jackc/pgx/v5/pgconn"
)

// Principles:
// Users can only access their own dashboards or if the role from context is "super_admin" or similar.
// Admins must have a specific permission (e.g., view_all_dashboards) to access any dashboard. Only
// "super_admin" role has this specific permission.
// That permission can be checked via a HasPermission() helper tied to  role/permission system.


// SaveUserSettingsHandler handles the creation or update of user settings for a single user.
// It enforces permission checks, extracts trusted identifiers from context, performs the database operation,
// logs the action, and handles audit logging.
func (app *Application) SaveUserSettingsHandler(w http.ResponseWriter, r *http.Request) {
	logger := app.Logger.GetLoggerWithContext(r).WithFunctionName("SaveUserSettingsHandler")
	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	// Permission enforcement
	if !app.HasPermission(ctx, "save_user_settings") {
		app.respondWithError(w, errors.New("forbidden: insufficient permissions"), http.StatusForbidden)
		return
	}

	// Extract User ID from context
	userID := app.getUserIDFromContext(ctx)
	if userID == nil {
		app.respondWithError(w, errors.New("missing user ID in context"), http.StatusUnauthorized)
		return
	}

	// Decode and validate request body
	var settings data.UserSettings
	if err := app.readJSON(w, r, &settings); err != nil {
		app.respondWithError(w, fmt.Errorf("invalid input: %w", err), http.StatusBadRequest)
		return
	}

	// Ensure the user ID in the request matches the context
	if settings.UserID != *userID {
		app.respondWithError(w, errors.New("user ID mismatch"), http.StatusBadRequest)
		return
	}

	// Insert or update UserSettings in the database
	if err := app.Models.UserSettings.Insert(ctx, &settings); err != nil {
		logger.Error("Failed to save user settings", "error", err)
		app.respondWithError(w, fmt.Errorf("failed to save user settings: %w", err), http.StatusInternalServerError)
		return
	}

	// Resolve Audit Action
	action, err := app.Models.Action.GetByName(ctx, "save_user_settings")
	if err != nil || action == nil {
		logger.Warn("Audit action 'save_user_settings' not found, attempting to create...", "error", err)
		actionID, createErr := app.Models.Action.CreateIfNotExists(ctx, "save_user_settings", "Save user settings")
		if createErr != nil {
			logger.Error("Failed to create missing audit action", "error", createErr)
		} else {
			action = &data.Action{ID: actionID}
		}
	}

	// Resolve Audit Entity Type
	entityType, err := app.Models.EntityType.GetByName(ctx, "user_settings")
	if err != nil || entityType == nil {
		logger.Warn("Audit entity type 'user_settings' not found, attempting to create...", "error", err)
		entityTypeID, createErr := app.Models.EntityType.CreateIfNotExists(ctx, "user_settings", "User settings")
		if createErr != nil {
			logger.Error("Failed to create missing entity type", "error", createErr)
		} else {
			entityType = &data.EntityType{ID: entityTypeID}
		}
	}

	// Insert Audit Log (fail gracefully if audit logging fails)
	if userID != nil && action != nil && entityType != nil {
		audit := data.AuditLog{
			ID:           uuid.New(),
			UserID:       userID,
			ActionID:     &action.ID,
			EntityTypeID: entityType.ID,
			EntityID:     settings.UserID.String(),
			Timestamp:    timeutil.Now(),
		}
		if err := app.Models.AuditLog.Insert(ctx, &audit); err != nil {
			// Audit logging failed (partial success)
			logger.Warn("Audit logging failed", "userID", settings.UserID, "error", err)
			app.respondWithJSON(w, http.StatusPartialContent, jsonResponse{
				Error:   false,
				Message: "User settings saved, but audit logging failed",
				Data:    settings.UserID,
			})
			return
		}
	}

	// Respond success
	logger.Info("User settings saved successfully", "userID", settings.UserID)
	app.respondWithJSON(w, http.StatusOK, jsonResponse{
		Error:   false,
		Message: "User settings saved successfully",
		Data:    settings.UserID,
	})
}


// GetUserSettingsByIDHandler retrieves a user's settings by their UUID.
// It enforces permission checks, extracts the settings ID from context,
// performs a database lookup, logs the operation, and handles audit logging.
func (app *Application) GetUserSettingsByIDHandler(w http.ResponseWriter, r *http.Request) {
	logger := app.Logger.GetLoggerWithContext(r).WithFunctionName("GetUserSettingsByIDHandler")
	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	// Permission enforcement
	if !app.HasPermission(ctx, "read_user_settings") {
		app.respondWithError(w, errors.New("forbidden: insufficient permissions"), http.StatusForbidden)
		return
	}

	// Extract User ID from context
	userID := app.getUserIDFromContext(ctx)
	if userID == nil {
		app.respondWithError(w, errors.New("missing user ID in context"), http.StatusUnauthorized)
		return
	}

	// Retrieve UserSettings from the database
	settings, err := app.Models.UserSettings.GetByID(ctx, *userID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			app.respondWithError(w, fmt.Errorf("user settings not found for user_id: %s", userID), http.StatusNotFound)
		} else {
			logger.Error("Failed to retrieve user settings", "error", err)
			app.respondWithError(w, fmt.Errorf("failed to retrieve user settings: %w", err), http.StatusInternalServerError)
		}
		return
	}

	// Resolve Audit Action
	action, err := app.Models.Action.GetByName(ctx, "read_user_settings")
	if err != nil || action == nil {
		logger.Warn("Audit action 'read_user_settings' not found, attempting to create...", "error", err)
		actionID, createErr := app.Models.Action.CreateIfNotExists(ctx, "read_user_settings", "Read user settings")
		if createErr != nil {
			logger.Error("Failed to create missing audit action", "error", createErr)
		} else {
			action = &data.Action{ID: actionID}
		}
	}

	// Resolve Audit Entity Type
	entityType, err := app.Models.EntityType.GetByName(ctx, "user_settings")
	if err != nil || entityType == nil {
		logger.Warn("Audit entity type 'user_settings' not found, attempting to create...", "error", err)
		entityTypeID, createErr := app.Models.EntityType.CreateIfNotExists(ctx, "user_settings", "User settings")
		if createErr != nil {
			logger.Error("Failed to create missing entity type", "error", createErr)
		} else {
			entityType = &data.EntityType{ID: entityTypeID}
		}
	}

	// Insert Audit Log (fail gracefully if audit logging fails)
	if userID != nil && action != nil && entityType != nil {
		audit := data.AuditLog{
			ID:           uuid.New(),
			UserID:       userID,
			ActionID:     &action.ID,
			EntityTypeID: entityType.ID,
			EntityID:     settings.UserID.String(),
			Timestamp:    timeutil.Now(),
		}
		if err := app.Models.AuditLog.Insert(ctx, &audit); err != nil {
			// Audit logging failed (partial success)
			logger.Warn("Audit logging failed", "user_id", settings.UserID, "error", err)
			app.respondWithJSON(w, http.StatusPartialContent, jsonResponse{
				Error:   false,
				Message: "User settings retrieved, but audit logging failed",
				Data:    settings,
			})
			return
		}
	}

	// Respond success
	logger.Info("User settings retrieved successfully", "user_id", settings.UserID)
	app.respondWithJSON(w, http.StatusOK, jsonResponse{
		Error:   false,
		Message: "User settings retrieved successfully",
		Data:    settings,
	})
}


// GetUserSettingsByUserIDHandler retrieves a user's settings based on the user ID extracted from the context.
// It enforces permission checks, performs a database query, logs the action, and handles audit logging.
func (app *Application) GetUserSettingsByUserIDHandler(w http.ResponseWriter, r *http.Request) {
	logger := app.Logger.GetLoggerWithContext(r).WithFunctionName("GetUserSettingsByUserIDHandler")
	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	// Permission enforcement
	if !app.HasPermission(ctx, "read_user_settings") {
		app.respondWithError(w, errors.New("forbidden: insufficient permissions"), http.StatusForbidden)
		return
	}

	// Extract User ID from context
	userID := app.getUserIDFromContext(ctx)
	if userID == nil {
		app.respondWithError(w, errors.New("missing user ID in context"), http.StatusUnauthorized)
		return
	}

	// Retrieve user settings from the database
	settings, err := app.Models.UserSettings.GetByID(ctx, *userID)
	if err != nil {
		if errors.Is(err, data.ErrRecordNotFound) {
			app.respondWithError(w, fmt.Errorf("user settings not found for user_id: %s", *userID), http.StatusNotFound)
		} else {
			logger.Error("Failed to retrieve user settings", "error", err)
			app.respondWithError(w, fmt.Errorf("failed to retrieve user settings: %w", err), http.StatusInternalServerError)
		}
		return
	}

	// Resolve Audit Action
	action, err := app.Models.Action.GetByName(ctx, "read_user_settings")
	if err != nil || action == nil {
		logger.Warn("Audit action 'read_user_settings' not found, attempting to create...", "error", err)
		actionID, createErr := app.Models.Action.CreateIfNotExists(ctx, "read_user_settings", "Read user settings")
		if createErr != nil {
			logger.Error("Failed to create missing audit action", "error", createErr)
		} else {
			action = &data.Action{ID: actionID}
		}
	}

	// Resolve Audit Entity Type
	entityType, err := app.Models.EntityType.GetByName(ctx, "user_settings")
	if err != nil || entityType == nil {
		logger.Warn("Audit entity type 'user_settings' not found, attempting to create...", "error", err)
		entityTypeID, createErr := app.Models.EntityType.CreateIfNotExists(ctx, "user_settings", "User settings")
		if createErr != nil {
			logger.Error("Failed to create missing entity type", "error", createErr)
		} else {
			entityType = &data.EntityType{ID: entityTypeID}
		}
	}

	// Insert Audit Log (fail gracefully if audit logging fails)
	if userID != nil && action != nil && entityType != nil {
		audit := data.AuditLog{
			ID:           uuid.New(),
			UserID:       userID,
			ActionID:     &action.ID,
			EntityTypeID: entityType.ID,
			EntityID:     settings.UserID.String(),
			Timestamp:    timeutil.Now(),
		}
		if err := app.Models.AuditLog.Insert(ctx, &audit); err != nil {
			// Audit logging failed (partial success)
			logger.Warn("Audit logging failed", "user_id", settings.UserID, "error", err)
			app.respondWithJSON(w, http.StatusPartialContent, jsonResponse{
				Error:   false,
				Message: "User settings retrieved, but audit logging failed",
				Data:    settings,
			})
			return
		}
	}

	// Respond success
	logger.Info("Retrieved user settings successfully", "user_id", settings.UserID)
	app.respondWithJSON(w, http.StatusOK, jsonResponse{
		Error:   false,
		Message: "User settings retrieved successfully",
		Data:    settings,
	})
}


// GetAllUserSettingsHandler retrieves a paginated list of all user settings records.
// It enforces permission checks, parses pagination parameters, queries the database for matching results,
// returns a list with total count, and logs the action with structured audit tracking.
func (app *Application) GetAllUserSettingsHandler(w http.ResponseWriter, r *http.Request) {
	logger := app.Logger.GetLoggerWithContext(r).WithFunctionName("GetAllUserSettingsHandler")
	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	if !app.HasPermission(ctx, "read_user_settings") {
		app.respondWithError(w, errors.New("forbidden: insufficient permissions"), http.StatusForbidden)
		return
	}

	// --- Pagination ---------------------------------------------------------
	page, pageSize, err := app.parsePaginationParams(r)
	if err != nil {
		app.respondWithError(w, err, http.StatusBadRequest)
		return
	}
	start := (page - 1) * pageSize

	// --- Fetch & slice ------------------------------------------------------
	settings, err := app.Models.UserSettings.GetAll(ctx)   // now returns (slice, error)
	if err != nil {
		logger.Error("DB fetch failed", "error", err)
		app.respondWithError(w, fmt.Errorf("failed to retrieve user settings: %w", err), http.StatusInternalServerError)
		return
	}
	total := len(settings)

	if start >= total {
		// empty page
		app.respondWithJSON(w, http.StatusOK, jsonResponse{
			Error:   false,
			Message: "User settings retrieved successfully",
			Data: struct {
				Data  []data.UserSettings `json:"data"`
				Total int                 `json:"total"`
			}{Data: []data.UserSettings{}, Total: total},
		})
		return
	}

	end := start + pageSize
	if end > total {
		end = total
	}

	// Resolve Audit Action
	action, err := app.Models.Action.GetByName(ctx, "read_user_settings")
	if err != nil || action == nil {
		logger.Warn("Audit action 'read_user_settings' not found, attempting to create...", "error", err)
		actionID, createErr := app.Models.Action.CreateIfNotExists(ctx, "read_user_settings", "Read user settings")
		if createErr != nil {
			logger.Error("Failed to create missing audit action", "error", createErr)
			// Allow main operation to succeed
		} else {
			action = &data.Action{ID: actionID}
		}
	}

	// Resolve Audit Entity Type
	entityType, err := app.Models.EntityType.GetByName(ctx, "user_settings")
	if err != nil || entityType == nil {
		logger.Warn("Audit entity type 'user_settings' not found, attempting to create...", "error", err)
		entityTypeID, createErr := app.Models.EntityType.CreateIfNotExists(ctx, "user_settings", "User settings")
		if createErr != nil {
			logger.Error("Failed to create missing entity type", "error", createErr)
			// Allow main operation to succeed
		} else {
			entityType = &data.EntityType{ID: entityTypeID}
		}
	}

	// Insert Audit Log (fail gracefully if audit logging fails)
	userID := app.getUserIDFromContext(ctx)
	if userID != nil && action != nil && entityType != nil {
		audit := data.AuditLog{
			ID:           uuid.New(),
			UserID:       userID,
			ActionID:     &action.ID,
			EntityTypeID: entityType.ID,
			EntityID:     "all_user_settings",
			Timestamp:    timeutil.Now(),
		}
		if err := app.Models.AuditLog.Insert(ctx, &audit); err != nil {
			// Audit logging failed (partial success)
			logger.Warn("Audit logging failed", "error", err)
			app.respondWithJSON(w, http.StatusPartialContent, jsonResponse{
				Error:   false,
				Message: "User settings retrieved, but audit logging failed",
				Data: struct {
					Data  []data.UserSettings `json:"data"`
					Total int                 `json:"total"`
				}{Data: settings, Total: total},
			})
			return
		}
	}

	// Success
	logger.Info("User settings retrieved successfully", "total", total)
	app.respondWithJSON(w, http.StatusOK, jsonResponse{
		Error:   false,
		Message: "User settings retrieved successfully",
		Data: struct {
			Data  []data.UserSettings `json:"data"`
			Total int                 `json:"total"`
		}{Data: settings, Total: total},
	})
}


// UpdateUserSettingsHandler handles partial updates to a user’s settings.
//
// Behaviour
// ----------
//   • Allows users to update **their own** settings; admins / internal operators may update any.  
//   • Accepts a _partial_ JSON payload (omitted fields remain unchanged).  
//   • Enforces role / permission checks using HasRole / HasPermission helpers.  
//   • Persists the change via UserSettingsModel.Update, with field‑level restrictions for self‑updates.  
//   • Resolves audit‑action & entity‑type dynamically (creating them if missing) and writes an audit log.  
//   • 200 on success, 400 on bad input, 403 on auth failure, 500 on internal error.
func (app *Application) UpdateUserSettingsHandler(w http.ResponseWriter, r *http.Request) {
	logger := app.Logger.GetLoggerWithContext(r).WithFunctionName("UpdateUserSettingsHandler")

	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	// Resolve caller identity
	userID := app.getUserIDFromContext(ctx)
	if userID == nil {
		app.respondWithError(w, errors.New("unauthorized: login required"), http.StatusUnauthorized)
		return
	}

	// Decode request
	var input data.UserSettings
	if err := app.readJSON(w, r, &input); err != nil {
		app.respondWithError(w, fmt.Errorf("invalid JSON: %w", err), http.StatusBadRequest)
		return
	}

	// Authorisation
	isAdmin, err := app.HasRole(ctx, "admin")
	if err != nil {
		app.respondWithError(w, errors.New("could not verify admin role"), http.StatusInternalServerError)
		return
	}
	isOperator, err := app.HasRole(ctx, "internal_operator")
	if err != nil {
		app.respondWithError(w, errors.New("could not verify operator role"), http.StatusInternalServerError)
		return
	}
	isInternal := isAdmin || isOperator
	isSelf := input.UserID == *userID

	if !isInternal && !isSelf {
		app.respondWithError(w, errors.New("forbidden: cannot update other users' settings"), http.StatusForbidden)
		return
	}

	if isInternal && !app.HasPermission(ctx, "update_user_settings") {
		app.respondWithError(w, errors.New("forbidden: insufficient permissions"), http.StatusForbidden)
		return
	}

	if isSelf {
		input.UserID = *userID // integrity guard
	}

	// Persist change
	if err := app.Models.UserSettings.Update(ctx, &input, isAdmin); err != nil {
		app.respondWithError(w, err, http.StatusInternalServerError)
		return
	}

	// Resolve Audit Action
	action, err := app.Models.Action.GetByName(ctx, "update_user_settings")
	if err != nil || action == nil {
		logger.Warn("Audit action 'update_user_settings' not found, attempting to create...", "error", err)
		actionID, createErr := app.Models.Action.CreateIfNotExists(ctx, "update_user_settings", "Update user settings")
		if createErr != nil {
			logger.Error("Failed to create missing audit action", "error", createErr)
		} else {
			action = &data.Action{ID: actionID}
		}
	}

	// Resolve Audit Entity Type
	entityType, err := app.Models.EntityType.GetByName(ctx, "user_settings")
	if err != nil || entityType == nil {
		logger.Warn("Audit entity type 'user_settings' not found, attempting to create...", "error", err)
		entityTypeID, createErr := app.Models.EntityType.CreateIfNotExists(ctx, "user_settings", "User settings")
		if createErr != nil {
			logger.Error("Failed to create missing entity type", "error", createErr)
		} else {
			entityType = &data.EntityType{ID: entityTypeID}
		}
	}

	// Insert Audit Log
	if userID != nil && action != nil && entityType != nil {
		audit := data.AuditLog{
			ID:           uuid.New(),
			UserID:       userID,
			ActionID:     &action.ID,
			EntityTypeID: entityType.ID,
			EntityID:     input.UserID.String(),
			Timestamp:    timeutil.Now(),
		}
		if err := app.Models.AuditLog.Insert(ctx, &audit); err != nil {
			// Audit logging failed (partial success)
			logger.Warn("Audit logging failed", "userID", input.UserID, "error", err)
			app.respondWithJSON(w, http.StatusPartialContent, jsonResponse{
				Error:   false,
				Message: "User settings updated, but audit logging failed",
				Data:    input.UserID,
			})
			return
		}
	}

	// Respond success
	logger.Info("User settings updated successfully", "userID", input.UserID)
	app.respondWithJSON(w, http.StatusOK, jsonResponse{
		Error:   false,
		Message: "User settings updated successfully",
		Data:    input.UserID,
	})
}
