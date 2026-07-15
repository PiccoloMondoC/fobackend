// sdworkspace/sdbackend/internal/server/cmd/api/user_permissions.go
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/PiccoloMondoC/sdworkspace/sdbackend/internal/data"
	"github.com/PiccoloMondoC/sdworkspace/sdbackend/internal/utils/timeutil"

	"github.com/google/uuid"
)

// Principles:
// Users can only access their own dashboards or if the role from context is "super_admin" or similar.
// Admins must have a specific permission (e.g., view_all_dashboards) to access any dashboard. Only
// "super_admin" role has this specific permission.
// That permission can be checked via a HasPermission() helper tied to  role/permission system.

// Permissions Handlers

// ListAllPermissionsHandler handles listing all available permissions in the system.
func (app *Application) ListAllPermissionsHandler(w http.ResponseWriter, r *http.Request) {
	logger := app.Logger.GetLoggerWithContext(r).WithFunctionName("ListAllPermissionsHandler")

	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	permissions, err := app.Models.Permission.GetAllPermissions(ctx)
	if err != nil {
		logger.Error("Failed to fetch permissions", "error", err)
		app.respondWithError(w, errors.New("could not fetch permissions"), http.StatusInternalServerError)
		return
	}

	// Audit log
	actionID := app.Preloaded.ActionIDs["list_permissions"]

	userID := app.getUserIDFromContext(ctx)

	auditLog := data.AuditLog{
		ID:           uuid.New(),
		UserID:       userID,
		ActionID:     &actionID,
		EntityTypeID: app.Preloaded.EntityTypeIDs["permissions"],
		EntityID:     "", // No single entity, it's a list
		Timestamp:    timeutil.Now(),
	}

	ctxAudit, cancelAudit := context.WithTimeout(context.Background(), cfgTimeout)
	defer cancelAudit()

	if err := app.Models.AuditLog.Insert(ctxAudit, &auditLog); err != nil {
		// Audit logging failed (partial success)
		logger.Warn("Failed to log audit trail", "error", err)
		app.respondWithJSON(w, http.StatusPartialContent, jsonResponse{
			Error:   false,
			Message: "Permissions listed, but audit logging failed",
			Data:    permissions,
		})
		return
	}

	// Success
	logger.Info("Permissions listed", "count", len(permissions))
	app.respondWithJSON(w, http.StatusOK, jsonResponse{
		Error:   false,
		Message: "Permissions listed successfully",
		Data:    permissions,
	})
}

// User-Specific Permission Checks

// CheckUserPermissionHandler checks if the specified user has a specific permission.
// It uses structured logging, extracts user ID from the URL, and logs the audit trail.
func (app *Application) CheckUserPermissionHandler(w http.ResponseWriter, r *http.Request) {
	logger := app.Logger.GetLoggerWithContext(r).WithFunctionName("CheckUserPermissionHandler")

	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	userID := app.getUserIDFromContext(ctx)

	var input struct {
		Permission string `json:"permission"`
	}

	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		logger.Warn("Invalid request payload", "error", err)
		app.respondWithError(w, fmt.Errorf("invalid request payload"), http.StatusBadRequest)
		return
	}

	if userID == nil {
		app.respondWithError(w, fmt.Errorf("missing user ID"), http.StatusBadRequest)
		return
	}

	role, err := app.Models.Role.GetRoleByUserID(ctx, *userID)
	if err != nil {
		logger.Error("Failed to retrieve user role", "user_id", userID, "error", err)
		app.respondWithError(w, fmt.Errorf("internal server error"), http.StatusInternalServerError)
		return
	}
	if role == nil {
		logger.Warn("User not found or inactive/deleted", "user_id", userID)
		app.respondWithError(w, fmt.Errorf("user not found"), http.StatusNotFound)
		return
	}

	if !role.IsActive {
		logger.Warn("Inactive role cannot be checked for permissions", "user_id", userID, "role", role.Name)
		app.respondWithError(w, fmt.Errorf("user role is inactive"), http.StatusForbidden)
		return
	}

	hasPerm, err := app.Models.RolePermission.RoleHasPermission(ctx, role.ID.String(), input.Permission)
	if err != nil {
		logger.Error("Permission check failed", "error", err)
		app.respondWithError(w, fmt.Errorf("internal server error"), http.StatusInternalServerError)
		return
	}
	
	// Audit log
	actionID := app.Preloaded.ActionIDs["check_permission"]

	auditLog := data.AuditLog{
		ID:           uuid.New(),
		UserID:       userID,
		ActionID:     &actionID,
		EntityTypeID: app.Preloaded.EntityTypeIDs["users"],
		EntityID:     userID.String(),
		Timestamp:    timeutil.Now(),
	}

	ctxAudit, cancelAudit := context.WithTimeout(context.Background(), cfgTimeout)
	defer cancelAudit()

	if err := app.Models.AuditLog.Insert(ctxAudit, &auditLog); err != nil {
		// Audit logging failed (partial success)
		logger.Warn("Failed to log audit trail", "error", err)
		app.respondWithJSON(w, http.StatusPartialContent, jsonResponse{
			Error:   false,
			Message: "Permission check complete, but audit logging failed",
			Data:    hasPerm,
		})
		return
	}

	// Success
	logger.Info("Permission check complete", "user_id", userID, "permission", input.Permission, "granted", hasPerm)
	app.respondWithJSON(w, http.StatusOK, jsonResponse{
		Error:   false,
		Message: "Permission check complete",
		Data:    hasPerm,
	})
}

// Role Permissions Handlers
/*
// GetRolePermissionsHandler retrieves all permissions assigned to a given role.
// It uses context timeout, structured logging, and audit logging.
func (app *Application) GetRolePermissionsHandler(w http.ResponseWriter, r *http.Request) {
	logger := app.Logger.GetLoggerWithContext(r).WithFunctionName("GetRolePermissionsHandler")

	roleID := app.getRoleIDFromContext(r.Context())
	if roleID == "" {
		logger.Warn("Missing role ID in context")
		app.respondWithError(w, errors.New("missing role ID"), http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	permissions, err := app.Models.RolePermission.GetPermissionsByRole(ctx, roleID)
	if err != nil {
		logger.Error("Failed to retrieve permissions", err, "role_id", roleID)
		app.respondWithError(w, fmt.Errorf("failed to retrieve permissions for role"), http.StatusInternalServerError)
		return
	}

	actionID := app.Preloaded.ActionIDs["view_role_permissions"]
	userID := app.getUserIDFromContext(ctx)

	auditLog := data.AuditLog{
		ID:           uuid.New(),
		UserID:       userID,
		ActionID:     &actionID,
		EntityTypeID: app.Preloaded.EntityTypeIDs["roles"],
		EntityID:     roleID,
		Timestamp:    timeutil.Now(),
	}

	ctxAudit, cancelAudit := context.WithTimeout(context.Background(), cfgTimeout)
	defer cancelAudit()
	if err := app.Models.AuditLog.Insert(ctxAudit, &auditLog); err != nil {
		logger.Warn("Audit log failed", "error", err)
	}

	logger.Info("Retrieved role permissions successfully", "role_id", roleID, "permission_count", len(permissions))

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(permissions); err != nil {
		logger.Error("Failed to encode response", "error", err)
	}
}


// UpdateRolePermissionsHandler handles partially updating via PATCH the list of permissions assigned to a role.
// It expects a JSON body with a "permissions" array of permission IDs.
// PUT /roles/{role}/permissions
func (app *Application) UpdateRolePermissionsHandler(w http.ResponseWriter, r *http.Request) {
	logger := app.Logger.GetLoggerWithContext(r).WithFunctionName("UpdateRolePermissionsHandler")

	roleID := app.getRoleIDFromContext(r.Context())
	if roleID == "" {
		logger.Warn("Missing role ID in context")
		app.respondWithError(w, errors.New("missing role ID"), http.StatusBadRequest)
		return
	}

	var input struct {
		Permissions []string `json:"permissions"`
	}

	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		logger.Warn("Invalid request payload", "error", err)
		app.respondWithError(w, fmt.Errorf("invalid request payload"), http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	// Remove all existing permissions
	existing, err := app.Models.RolePermission.GetPermissionsByRole(ctx, roleID)
	if err != nil {
		logger.Error("Failed to retrieve existing permissions", "error", err)
		app.respondWithError(w, fmt.Errorf("failed to retrieve existing permissions"), http.StatusInternalServerError)
		return
	}
	for _, p := range existing {
		if err := app.Models.RolePermission.RemovePermissionFromRole(ctx, roleID, p.ID); err != nil {
			logger.Warn("Failed to remove permission", "permission_id", p.ID, "error", err)
			// Continue removing other permissions
		}
	}

	// Assign new permissions
	for _, permID := range input.Permissions {
		if err := app.Models.RolePermission.AssignPermissionToRole(ctx, roleID, permID); err != nil {
			logger.Warn("Failed to assign permission", "permission_id", permID, "error", err)
			// Continue assigning other permissions
		}
	}

	// Audit log
	actionID := app.Preloaded.ActionIDs["update_role_permissions"]

	userID := app.getUserIDFromContext(ctx)
	auditLog := data.AuditLog{
		ID:           uuid.New(),
		UserID:       userID,
		ActionID:     &actionID,
		EntityTypeID: app.Preloaded.EntityTypeIDs["roles"],
		EntityID:     roleID,
		Timestamp:    timeutil.Now(),
	}

	ctxAudit, cancelAudit := context.WithTimeout(context.Background(), cfgTimeout)
	defer cancelAudit()
	if err := app.Models.AuditLog.Insert(ctxAudit, &auditLog); err != nil {
		logger.Warn("Failed to log audit trail", "error", err)
	}

	logger.Info("Role permissions updated successfully", "role_id", roleID)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(map[string]string{
		"message": "Permissions updated successfully",
	}); err != nil {
		logger.Error("Failed to write response", "error", err)
	}
}
*/