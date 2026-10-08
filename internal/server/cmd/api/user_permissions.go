// Package main provides HTTP handlers for the Platform API.
//
// focodebase/fobackend/internal/server/cmd/api/user_permissions.go
//
// GTM:
//
//	Layer: 2.2 Identity / Auth Domain
//	Release Class: SPINE
//	Reason:
//	  Permission discovery, permission evaluation, and role-permission
//	  administration. The role is always named by the URL ({roleID}); the
//	  caller's own role is never the implicit target. Replacement of a role's
//	  permission set is atomic. Governance role permission sets (super_admin:
//	  every permission; admin: every permission except the Super-Admin-tier
//	  set) are owned by platform seeding and cannot be edited here.
//
// SPINE Rule:
//
//	Keep compiling.
//	Keep production-ready.
//	Preserve atomic role-permission replacement.
//	Preserve seed ownership of governance role permission sets.
//	Preserve auditability for permission reads, checks, and mutations.
//	Do not return wrapped internal errors to clients.
//	Block deployment if this file breaks build, permission lookup,
//	role-permission assignment, or authorization integrity.
package main

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/PiccoloMondoC/focodebase/fobackend/internal/data"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
 "github.com/jackc/pgx/v5"
)

const (
	permissionAuditEntityType            = "permissions"
	permissionAuditEntityTypeDescription = "Permission catalog and role-permission grants"
)

func (app *Application) auditPermissionAction(ctx context.Context, action, description, entityID string) error {
	return app.insertGovernanceAudit(
		ctx,
		app.getUserIDFromContext(ctx),
		action,
		description,
		permissionAuditEntityType,
		permissionAuditEntityTypeDescription,
		entityID,
	)
}

// ListAllPermissionsHandler returns the canonical permission catalog.
//
//	GET /api/v1/permissions/   list_permissions
func (app *Application) ListAllPermissionsHandler(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	if !app.HasPermission(ctx, "list_permissions") {
		app.respondWithErrorCode(w, http.StatusForbidden, "insufficient_permission", "forbidden: insufficient permissions")
		return
	}

	permissions, err := app.Models.Permission.GetAllPermissions(ctx)
	if err != nil {
		app.respondWithRoleError(w, r, "list_permissions", err)
		return
	}

	if err := app.auditPermissionAction(ctx, "list_permissions", "List the permission catalog", "catalog"); err != nil {
		app.Logger.GetLoggerWithContext(r).Warn("permission catalog read but audit failed", "error", err)
	}

	app.respondWithJSON(w, http.StatusOK, jsonResponse{Error: false, Message: "Permissions listed successfully", Data: permissions})
}

// CheckUserPermissionHandler reports whether the account in the URL holds
// the named permission through its effective (primary) active role.
//
//	GET /api/v1/permissions/check/{userID}/{permission}   check_user_permission
func (app *Application) CheckUserPermissionHandler(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	if !app.HasPermission(ctx, "check_user_permission") {
		app.respondWithErrorCode(w, http.StatusForbidden, "insufficient_permission", "forbidden: insufficient permissions")
		return
	}

	targetID, err := uuid.Parse(strings.TrimSpace(chi.URLParam(r, "userID")))
	if err != nil || targetID == uuid.Nil {
		app.respondWithErrorCode(w, http.StatusBadRequest, "invalid_request", "invalid user ID")
		return
	}
	permission := strings.TrimSpace(chi.URLParam(r, "permission"))
	if permission == "" {
		app.respondWithErrorCode(w, http.StatusBadRequest, "invalid_request", "permission is required")
		return
	}

	granted := false
	role, err := app.Models.Role.GetRoleByUserID(ctx, targetID)
	switch {
	case errors.Is(err, data.ErrRoleNotFound):
		granted = false
	case err != nil:
		app.respondWithRoleError(w, r, "check_user_permission", err)
		return
	default:
		granted, err = app.Models.RolePermission.RoleHasPermission(ctx, role.ID.String(), permission)
		if err != nil {
			app.respondWithRoleError(w, r, "check_user_permission", err)
			return
		}
	}

	if err := app.auditPermissionAction(ctx, "check_user_permission", "Check whether an account holds a permission", targetID.String()); err != nil {
		app.Logger.GetLoggerWithContext(r).Warn("permission check performed but audit failed", "error", err)
	}

	app.respondWithJSON(w, http.StatusOK, jsonResponse{
		Error:   false,
		Message: "Permission check complete",
		Data: struct {
			UserID     uuid.UUID `json:"user_id"`
			Permission string    `json:"permission"`
			Granted    bool      `json:"granted"`
		}{targetID, permission, granted},
	})
}

// GetRolePermissionsHandler returns the permissions granted to {roleID}.
//
//	GET /api/v1/roles/{roleID}/permissions   read_role_permissions
func (app *Application) GetRolePermissionsHandler(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	if !app.HasPermission(ctx, "read_role_permissions") {
		app.respondWithErrorCode(w, http.StatusForbidden, "insufficient_permission", "forbidden: insufficient permissions")
		return
	}
	roleID, ok := parseRoleIDParam(r)
	if !ok {
		app.respondWithErrorCode(w, http.StatusBadRequest, "invalid_request", "invalid role ID")
		return
	}
	if _, err := app.Models.Role.GetRoleByID(ctx, roleID); err != nil {
		app.respondWithRoleError(w, r, "read_role_permissions", err)
		return
	}

	permissions, err := app.Models.RolePermission.GetPermissionsByRole(ctx, roleID.String())
	if err != nil {
		app.respondWithRoleError(w, r, "read_role_permissions", err)
		return
	}

	if err := app.auditPermissionAction(ctx, "read_role_permissions", "Read the permissions granted to a role", roleID.String()); err != nil {
		app.Logger.GetLoggerWithContext(r).Warn("role permissions read but audit failed", "error", err)
	}

	app.respondWithJSON(w, http.StatusOK, jsonResponse{Error: false, Message: "Role permissions retrieved successfully", Data: permissions})
}

// UpdateRolePermissionsHandler atomically replaces the permission set of a
// non-governance role. An empty array removes every permission; a missing
// field is rejected so omission cannot strip a role by accident.
//
//	PUT /api/v1/roles/{roleID}/permissions   update_role_permissions (Super Admin tier)
func (app *Application) UpdateRolePermissionsHandler(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	if !app.HasPermission(ctx, "update_role_permissions") {
		app.respondWithErrorCode(w, http.StatusForbidden, "insufficient_permission", "forbidden: insufficient permissions")
		return
	}
	roleID, ok := parseRoleIDParam(r)
	if !ok {
		app.respondWithErrorCode(w, http.StatusBadRequest, "invalid_request", "invalid role ID")
		return
	}

	var input struct {
		Permissions *[]string `json:"permissions"`
	}
	if err := app.readJSON(w, r, &input); err != nil {
		app.respondWithErrorCode(w, http.StatusBadRequest, "invalid_request", "invalid request body")
		return
	}
	if input.Permissions == nil {
		app.respondWithErrorCode(w, http.StatusBadRequest, "invalid_request", "permissions array is required")
		return
	}

	role, err := app.Models.Role.GetRoleByID(ctx, roleID)
	if err != nil {
		app.respondWithRoleError(w, r, "update_role_permissions", err)
		return
	}
	if data.IsAdministrativeRoleName(role.Name) {
		app.respondWithRoleError(w, r, "update_role_permissions", data.ErrGovernanceRoleProtected)
		return
	}

	seen := make(map[uuid.UUID]struct{}, len(*input.Permissions))
	ids := make([]uuid.UUID, 0, len(*input.Permissions))
	for _, raw := range *input.Permissions {
		id, err := uuid.Parse(strings.TrimSpace(raw))
		if err != nil || id == uuid.Nil {
			app.respondWithErrorCode(w, http.StatusBadRequest, "invalid_request", "permission IDs must be UUIDs")
			return
		}
		if _, dup := seen[id]; dup {
			app.respondWithErrorCode(w, http.StatusBadRequest, "invalid_request", "permission IDs must be unique")
			return
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}

	actorID,ok:=app.governanceActor(w,ctx);if !ok{return}
 if err := app.InternalServices.WithRoleAdministrationInternal(ctx,actorID,"update_role_permissions","update_role_permissions",roleID.String(),func(ctx context.Context,tx pgx.Tx) error {
 return app.Models.RolePermission.ReplacePermissionsForRoleTx(ctx,tx,roleID,ids)
 }); err != nil {
		app.respondWithRoleError(w, r, "update_role_permissions", err)
		return
	}

	var auditErr error
	app.respondRoleMutation(w, r, http.StatusOK, "Role permissions updated successfully",
		map[string]any{"role_id": roleID, "permission_ids": ids}, auditErr)
}
