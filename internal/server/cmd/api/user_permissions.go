// Package main provides HTTP handlers for the Platform API.
//
// sdworkspace/sdbackend/internal/server/cmd/api/user_permissions.go
//
// GTM:
//
//	Layer: 2.2 Identity / Auth Domain
//	Release Class: SPINE
//	Reason:
//	  Permission discovery, permission evaluation, and role-permission
//	  administration are release-critical authorization infrastructure.
//	  These handlers expose the canonical permission catalog, support explicit
//	  user capability checks, and preserve controlled role capability
//	  assignments required by the initial Platform release spine.
//
// SPINE Rule:
//
//	Keep compiling.
//	Keep production-ready.
//	Preserve canonical permission and role-permission semantics.
//	Preserve active-role enforcement for permission checks.
//	Preserve UUID validation through the canonical context and data layers.
//	Preserve explicit authorization around privileged permission workflows.
//	Preserve auditability for permission reads, checks, and mutations.
//	Preserve DB-owned lifecycle timestamps.
//	Do not recreate persistence contracts in the handler layer.
//	Do not silently continue after role-permission mutation failures.
//	Do not expose privileged permission administration without route-level
//	authentication and permission enforcement.
//	Block deployment if this file breaks build, permission lookup,
//	role-permission assignment, permission evaluation, auditability, or
//	authorization integrity.
package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/PiccoloMondoC/sdworkspace/sdbackend/internal/data"

	"github.com/google/uuid"
)

// ListAllPermissionsHandler returns the canonical permission catalog.
func (app *Application) ListAllPermissionsHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	logger := app.Logger.
		GetLoggerWithContext(r).
		WithFunctionName("ListAllPermissionsHandler")

	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	permissions, err := app.Models.Permission.GetAllPermissions(ctx)
	if err != nil {
		logger.Error(
			"Fetch permissions failed",
			"error", err,
		)
		app.respondWithError(
			w,
			errors.New("could not fetch permissions"),
			http.StatusInternalServerError,
		)
		return
	}

	actionID := app.Preloaded.ActionIDs["list_permissions"]
	userID := app.getUserIDFromContext(ctx)

	auditLog := &data.AuditLog{
		ID:           uuid.New(),
		UserID:       userID,
		ActionID:     actionID,
		EntityTypeID: app.Preloaded.EntityTypeIDs["permissions"],
		EntityID:     "",
	}

	auditCtx, auditCancel := context.WithTimeout(
		context.Background(),
		cfgTimeout,
	)
	defer auditCancel()

	if err := app.Models.AuditLog.Insert(auditCtx, auditLog); err != nil {
		logger.Warn(
			"Permission catalog read succeeded but audit insertion failed",
			"error", err,
		)
	}

	logger.Info(
		"Permissions listed",
		"permission_count", len(permissions),
	)

	app.respondWithJSON(w, http.StatusOK, jsonResponse{
		Error:   false,
		Message: "Permissions listed successfully",
		Data:    permissions,
	})
}

// CheckUserPermissionHandler checks whether the context-scoped user currently
// holds a named permission through an active role.
func (app *Application) CheckUserPermissionHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	logger := app.Logger.
		GetLoggerWithContext(r).
		WithFunctionName("CheckUserPermissionHandler")

	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	userID := app.getUserIDFromContext(ctx)
	if userID == nil || *userID == uuid.Nil {
		logger.Warn("User ID not found in context")
		app.respondWithError(
			w,
			errors.New("missing user ID"),
			http.StatusBadRequest,
		)
		return
	}

	var input struct {
		Permission string `json:"permission"`
	}

	if err := app.readJSON(w, r, &input); err != nil {
		logger.Warn(
			"Invalid request payload",
			"error", err,
		)
		app.respondWithError(
			w,
			fmt.Errorf("invalid request payload: %w", err),
			http.StatusBadRequest,
		)
		return
	}

	input.Permission = strings.TrimSpace(input.Permission)
	if input.Permission == "" {
		logger.Warn(
			"Permission name is required",
			"user_id", *userID,
		)
		app.respondWithError(
			w,
			errors.New("permission is required"),
			http.StatusBadRequest,
		)
		return
	}

	role, err := app.Models.Role.GetRoleByUserID(ctx, *userID)
	if err != nil {
		logger.Error(
			"Retrieve user role failed",
			"user_id", *userID,
			"error", err,
		)
		app.respondWithError(
			w,
			errors.New("internal server error"),
			http.StatusInternalServerError,
		)
		return
	}

	if role == nil {
		logger.Warn(
			"User role not found",
			"user_id", *userID,
		)
		app.respondWithError(
			w,
			errors.New("user role not found"),
			http.StatusNotFound,
		)
		return
	}

	if !role.IsActive {
		logger.Warn(
			"Inactive role cannot grant permissions",
			"user_id", *userID,
			"role_id", role.ID,
			"role_name", role.Name,
		)
		app.respondWithError(
			w,
			errors.New("user role is inactive"),
			http.StatusForbidden,
		)
		return
	}

	hasPermission, err := app.Models.RolePermission.RoleHasPermission(
		ctx,
		role.ID.String(),
		input.Permission,
	)
	if err != nil {
		logger.Error(
			"Permission check failed",
			"user_id", *userID,
			"role_id", role.ID,
			"permission", input.Permission,
			"error", err,
		)
		app.respondWithError(
			w,
			errors.New("internal server error"),
			http.StatusInternalServerError,
		)
		return
	}

	actionID := app.Preloaded.ActionIDs["check_permission"]

	auditLog := &data.AuditLog{
		ID:           uuid.New(),
		UserID:       userID,
		ActionID:     actionID,
		EntityTypeID: app.Preloaded.EntityTypeIDs["users"],
		EntityID:     userID.String(),
	}

	auditCtx, auditCancel := context.WithTimeout(
		context.Background(),
		cfgTimeout,
	)
	defer auditCancel()

	if err := app.Models.AuditLog.Insert(auditCtx, auditLog); err != nil {
		logger.Warn(
			"Permission check succeeded but audit insertion failed",
			"user_id", *userID,
			"permission", input.Permission,
			"error", err,
		)
	}

	logger.Info(
		"Permission check complete",
		"user_id", *userID,
		"role_id", role.ID,
		"permission", input.Permission,
		"granted", hasPermission,
	)

	app.respondWithJSON(w, http.StatusOK, jsonResponse{
		Error:   false,
		Message: "Permission check complete",
		Data:    hasPermission,
	})
}

// GetRolePermissionsHandler returns all permissions assigned to the
// context-scoped role.
func (app *Application) GetRolePermissionsHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	logger := app.Logger.
		GetLoggerWithContext(r).
		WithFunctionName("GetRolePermissionsHandler")

	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	roleIDPtr := app.getRoleIDFromContext(ctx)
	if roleIDPtr == nil || *roleIDPtr == uuid.Nil {
		logger.Warn("Role ID not found in context")
		app.respondWithError(
			w,
			errors.New("missing role ID"),
			http.StatusBadRequest,
		)
		return
	}

	roleID := roleIDPtr.String()

	permissions, err := app.Models.RolePermission.GetPermissionsByRole(
		ctx,
		roleID,
	)
	if err != nil {
		logger.Error(
			"Retrieve role permissions failed",
			"role_id", roleID,
			"error", err,
		)
		app.respondWithError(
			w,
			errors.New("failed to retrieve permissions for role"),
			http.StatusInternalServerError,
		)
		return
	}

	actionID := app.Preloaded.ActionIDs["view_role_permissions"]
	userID := app.getUserIDFromContext(ctx)

	auditLog := &data.AuditLog{
		ID:           uuid.New(),
		UserID:       userID,
		ActionID:     actionID,
		EntityTypeID: app.Preloaded.EntityTypeIDs["roles"],
		EntityID:     roleID,
	}

	auditCtx, auditCancel := context.WithTimeout(
		context.Background(),
		cfgTimeout,
	)
	defer auditCancel()

	if err := app.Models.AuditLog.Insert(auditCtx, auditLog); err != nil {
		logger.Warn(
			"Role-permission read succeeded but audit insertion failed",
			"role_id", roleID,
			"error", err,
		)
	}

	logger.Info(
		"Role permissions retrieved",
		"role_id", roleID,
		"permission_count", len(permissions),
	)

	app.respondWithJSON(w, http.StatusOK, jsonResponse{
		Error:   false,
		Message: "Role permissions retrieved successfully",
		Data:    permissions,
	})
}

// UpdateRolePermissionsHandler replaces the complete permission assignment set
// for the context-scoped role.
//
// An empty permissions array removes every current permission from the role.
// A missing permissions field is rejected because omission must not
// accidentally remove all authorization capabilities.
func (app *Application) UpdateRolePermissionsHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	logger := app.Logger.
		GetLoggerWithContext(r).
		WithFunctionName("UpdateRolePermissionsHandler")

	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	roleIDPtr := app.getRoleIDFromContext(ctx)
	if roleIDPtr == nil || *roleIDPtr == uuid.Nil {
		logger.Warn("Role ID not found in context")
		app.respondWithError(
			w,
			errors.New("missing role ID"),
			http.StatusBadRequest,
		)
		return
	}

	roleID := roleIDPtr.String()

	var input struct {
		Permissions []string `json:"permissions"`
	}

	if err := app.readJSON(w, r, &input); err != nil {
		logger.Warn(
			"Invalid request payload",
			"role_id", roleID,
			"error", err,
		)
		app.respondWithError(
			w,
			fmt.Errorf("invalid request payload: %w", err),
			http.StatusBadRequest,
		)
		return
	}

	if input.Permissions == nil {
		logger.Warn(
			"Permissions array is required",
			"role_id", roleID,
		)
		app.respondWithError(
			w,
			errors.New("permissions array is required"),
			http.StatusBadRequest,
		)
		return
	}

	desiredPermissionIDs := make(
		[]string,
		0,
		len(input.Permissions),
	)
	desiredPermissionSet := make(
		map[string]struct{},
		len(input.Permissions),
	)

	for _, rawPermissionID := range input.Permissions {
		permissionID := strings.TrimSpace(rawPermissionID)
		if permissionID == "" {
			logger.Warn(
				"Empty permission ID rejected",
				"role_id", roleID,
			)
			app.respondWithError(
				w,
				errors.New("permission IDs must not be empty"),
				http.StatusBadRequest,
			)
			return
		}

		parsedPermissionID, err := uuid.Parse(permissionID)
		if err != nil {
			logger.Warn(
				"Invalid permission ID rejected",
				"role_id", roleID,
				"permission_id", permissionID,
				"error", err,
			)
			app.respondWithError(
				w,
				fmt.Errorf("invalid permission ID %q", permissionID),
				http.StatusBadRequest,
			)
			return
		}

		canonicalPermissionID := parsedPermissionID.String()

		if _, exists := desiredPermissionSet[canonicalPermissionID]; exists {
			logger.Warn(
				"Duplicate permission ID rejected",
				"role_id", roleID,
				"permission_id", canonicalPermissionID,
			)
			app.respondWithError(
				w,
				fmt.Errorf(
					"duplicate permission ID %q",
					canonicalPermissionID,
				),
				http.StatusBadRequest,
			)
			return
		}

		// Confirm every requested permission exists before changing any current
		// role-permission assignments.
		if _, err := app.Models.Permission.GetPermissionByID(
			ctx,
			canonicalPermissionID,
		); err != nil {
			if errors.Is(err, data.ErrPermissionNotFound) {
				logger.Warn(
					"Requested permission not found",
					"role_id", roleID,
					"permission_id", canonicalPermissionID,
				)
				app.respondWithError(
					w,
					fmt.Errorf(
						"permission %q not found",
						canonicalPermissionID,
					),
					http.StatusBadRequest,
				)
				return
			}

			logger.Error(
				"Validate requested permission failed",
				"role_id", roleID,
				"permission_id", canonicalPermissionID,
				"error", err,
			)
			app.respondWithError(
				w,
				errors.New("failed to validate requested permissions"),
				http.StatusInternalServerError,
			)
			return
		}

		desiredPermissionSet[canonicalPermissionID] = struct{}{}
		desiredPermissionIDs = append(
			desiredPermissionIDs,
			canonicalPermissionID,
		)
	}

	existingPermissions, err := app.Models.RolePermission.GetPermissionsByRole(
		ctx,
		roleID,
	)
	if err != nil {
		logger.Error(
			"Retrieve existing role permissions failed",
			"role_id", roleID,
			"error", err,
		)
		app.respondWithError(
			w,
			errors.New("failed to retrieve existing permissions"),
			http.StatusInternalServerError,
		)
		return
	}

	existingPermissionSet := make(
		map[string]struct{},
		len(existingPermissions),
	)

	for _, permission := range existingPermissions {
		if permission == nil {
			continue
		}

		permissionID, err := uuid.Parse(permission.ID)
		if err != nil {
			logger.Error(
				"Stored permission contains invalid UUID",
				"role_id", roleID,
				"permission_id", permission.ID,
				"error", err,
			)
			app.respondWithError(
				w,
				errors.New("stored permission data is invalid"),
				http.StatusInternalServerError,
			)
			return
		}

		existingPermissionSet[permissionID.String()] = struct{}{}
	}

	for permissionID := range existingPermissionSet {
		if _, remainsAssigned := desiredPermissionSet[permissionID]; remainsAssigned {
			continue
		}

		if err := app.Models.RolePermission.RemovePermissionFromRole(
			ctx,
			roleID,
			permissionID,
		); err != nil {
			logger.Error(
				"Remove permission from role failed",
				"role_id", roleID,
				"permission_id", permissionID,
				"error", err,
			)
			app.respondWithError(
				w,
				errors.New("failed to update role permissions"),
				http.StatusInternalServerError,
			)
			return
		}
	}

	for _, permissionID := range desiredPermissionIDs {
		if _, alreadyAssigned := existingPermissionSet[permissionID]; alreadyAssigned {
			continue
		}

		if err := app.Models.RolePermission.AssignPermissionToRole(
			ctx,
			roleID,
			permissionID,
		); err != nil {
			logger.Error(
				"Assign permission to role failed",
				"role_id", roleID,
				"permission_id", permissionID,
				"error", err,
			)
			app.respondWithError(
				w,
				errors.New("failed to update role permissions"),
				http.StatusInternalServerError,
			)
			return
		}
	}

	actionID := app.Preloaded.ActionIDs["update_role_permissions"]
	userID := app.getUserIDFromContext(ctx)

	auditLog := &data.AuditLog{
		ID:           uuid.New(),
		UserID:       userID,
		ActionID:     actionID,
		EntityTypeID: app.Preloaded.EntityTypeIDs["roles"],
		EntityID:     roleID,
	}

	auditCtx, auditCancel := context.WithTimeout(
		context.Background(),
		cfgTimeout,
	)
	defer auditCancel()

	if err := app.Models.AuditLog.Insert(auditCtx, auditLog); err != nil {
		logger.Warn(
			"Role-permission update succeeded but audit insertion failed",
			"role_id", roleID,
			"error", err,
		)
	}

	logger.Info(
		"Role permissions updated",
		"role_id", roleID,
		"permission_count", len(desiredPermissionIDs),
	)

	app.respondWithJSON(w, http.StatusOK, jsonResponse{
		Error:   false,
		Message: "Role permissions updated successfully",
		Data: map[string]any{
			"role_id":       roleID,
			"permission_ids": desiredPermissionIDs,
		},
	})
}