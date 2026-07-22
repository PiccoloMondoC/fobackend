// Package main provides HTTP handlers for the Platform API.
//
// sdworkspace/sdbackend/internal/server/cmd/api/roles.go
//
// GTM:
//
//	Layer: 2.2 Identity / Auth Domain
//	Release Class: SPINE
//	Reason:
//	  Role administration and user-role assignment are release-critical
//	  authorization infrastructure. These handlers support canonical role
//	  creation, lookup, update, lifecycle management, assignment, revocation,
//	  and authenticated-user role retrieval required by v1 access control.
//
// SPINE Rule:
//
//	Keep compiling.
//	Keep production-ready.
//	Preserve authorization enforcement at the router and handler boundaries.
//	Preserve the distinction between role lifecycle operations and user-role
//	assignment lifecycle operations.
//	Preserve role hierarchy and internal-role assignment protections.
//	Preserve canonical soft-delete behavior for normal role removal.
//	Preserve user-role assignment and revocation integrity.
//	Preserve audit logging for privileged reads and mutations.
//	Preserve database-owned persisted lifecycle timestamps.
//	Do not use role hard deletion as the normal API removal path.
//	Do not treat revoking a user-role assignment as deleting the role itself.
//	Block deployment if this file breaks build, role administration,
//	role assignment, role revocation, role lifecycle behavior, or
//	authorization integrity.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/PiccoloMondoC/sdworkspace/sdbackend/internal/data"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// Principles:
// Extract the authenticated user ID (guaranteed uuid.UUID by middleware).
// Users can only access their own dashboards or if the role from context is "super_admin" or similar.
// Admins must have a specific permission (e.g., view_all_dashboards) to access any dashboard. Only
// "super_admin" role has this specific permission.
// That permission can be checked via a HasPermission() helper tied to  role/permission system.
// Always extract sensitive identifiers from a trusted context


// ListRolesHandler handles the retrieval of all roles in the system.
// It enforces permission checks, extracts trusted identifiers from context,
// retrieves roles from the database, performs audit logging, and returns the result.
func (app *Application) ListRolesHandler(w http.ResponseWriter, r *http.Request) {
	logger := app.Logger.WithFunctionName("ListRolesHandler")

	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	// Permission enforcement
	if !app.HasPermission(ctx, "list_roles") {
		app.respondWithError(w, errors.New("forbidden: insufficient permissions"), http.StatusForbidden)
		return
	}

	// Extract authenticated user ID from context
	userID := app.getUserIDFromContext(ctx)
	if userID == nil {
		app.respondWithError(w, errors.New("missing user ID in context"), http.StatusUnauthorized)
		return
	}

	// Retrieve all roles from the database
	roles, err := app.Models.Role.ListRoles(ctx)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			logger.Warn("No roles found")
			app.respondWithError(w, fmt.Errorf("no roles found"), http.StatusNotFound)
			return
		}
		logger.Error("Failed to retrieve roles", "error", err)
		app.respondWithError(w, fmt.Errorf("failed to retrieve roles: %w", err), http.StatusInternalServerError)
		return
	}

	// Resolve audit action
	action, err := app.Models.Action.GetByName(ctx, "list_roles")
	if err != nil || action == nil {
		logger.Warn("Audit action 'list_roles' not found, attempting to create...", "error", err)
		actionID, createErr := app.Models.Action.CreateIfNotExists(ctx, "list_roles", "List all roles in the system")
		if createErr != nil {
			logger.Error("Failed to create missing audit action", "error", createErr)
		} else {
			action = &data.Action{ID: actionID}
		}
	}

	// Resolve audit entity type
	entityType, err := app.Models.EntityType.GetByName(ctx, "roles")
	if err != nil || entityType == nil {
		logger.Warn("Audit entity type 'roles' not found, attempting to create...", "error", err)
		entityTypeID, createErr := app.Models.EntityType.CreateIfNotExists(ctx, "roles", "Role definitions")
		if createErr != nil {
			logger.Error("Failed to create missing entity type", "error", createErr)
		} else {
			entityType = &data.EntityType{ID: entityTypeID}
		}
	}

	// Insert audit log
	if action != nil && entityType != nil {
		audit := data.AuditLog{
			ID:           uuid.New(),
			UserID:       userID,
			ActionID:     action.ID,
			EntityTypeID: entityType.ID,
			EntityID:     "list_all", // Indicates list operation, not tied to single entity
		}

		auditCtx, cancelAudit := context.WithTimeout(context.Background(), cfgTimeout)
		defer cancelAudit()

		if err := app.Models.AuditLog.Insert(auditCtx, &audit); err != nil {
			// Audit logging failed (partial success)
			logger.Warn("Audit logging failed", "error", err)
			app.respondWithJSON(w, http.StatusPartialContent, jsonResponse{
				Error:   false,
				Message: "Roles retrieved, but audit logging failed",
				Data:    roles,
			})
			return
		}
	}

	// Success
	logger.Info("Roles retrieved successfully", "count", len(roles))
	app.respondWithJSON(w, http.StatusOK, jsonResponse{
		Error:   false,
		Message: "Roles retrieved successfully",
		Data:    roles,
	})
}


// GetRoleByIDHandler retrieves a role by its ID from the database.
// It uses structured logging, context management, and audit logging for traceability and reliability.
func (app *Application) GetRoleByIDHandler(w http.ResponseWriter, r *http.Request) {
	logger := app.Logger.GetLoggerWithContext(r).WithFunctionName("GetRoleByIDHandler")

	// Extract roleID from the URL parameters
	roleIDParam := r.URL.Query().Get("id")
	roleID, err := uuid.Parse(roleIDParam)
	if err != nil {
		logger.Warn("Invalid role ID", "id", roleIDParam, "error", err)
		app.respondWithError(w, fmt.Errorf("invalid role ID"), http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	// Query the role
	role, err := app.Models.Role.GetRoleByID(ctx, roleID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			logger.Info("Role not found", "user_role_id", roleID)
			app.respondWithError(w, fmt.Errorf("role not found"), http.StatusNotFound)
			return
		}
		logger.Error("Failed to retrieve role", "user_role_id", roleID, "error", err)
		app.respondWithError(w, fmt.Errorf("failed to retrieve role"), http.StatusInternalServerError)
		return
	}

	// Audit log
	actionID := app.Preloaded.ActionIDs["view_user_role"]

	auditLog := data.AuditLog{
		ID:           uuid.New(),
		UserID:       app.getUserIDFromContext(ctx),
		ActionID:     actionID,
		EntityTypeID: app.Preloaded.EntityTypeIDs["roles"],
		EntityID:     roleID.String(),
	}

	ctxAudit, cancelAudit := context.WithTimeout(context.Background(), cfgTimeout)
	defer cancelAudit()

	if err := app.Models.AuditLog.Insert(ctxAudit, &auditLog); err != nil {
		// Audit logging failed (partial success)
		logger.Warn("Failed to log audit trail", "error", err)
		app.respondWithJSON(w, http.StatusPartialContent, jsonResponse{
			Error:   false,
			Message: "Role retrieved, but audit logging failed",
			Data:    role,
		})
		return
	}

	// Success
	logger.Info("Role retrieved successfully", "user_role_id", roleID)
	app.respondWithJSON(w, http.StatusOK, jsonResponse{
		Error:   false,
		Message: "Role retrieved successfully",
		Data:    role,
	})
}


// CreateRoleHandler creates a new role record.
//
// Behaviour
// ---------
//   • AuthZ: caller must hold "create_role" permission.  
//   • Validation: name required, hierarchy_level ≥ 0.  
//   • Idempotent name: duplicate name → HTTP 409.  
//   • Audit‑ready: dynamic action "create_role", entity type "role".
//
// Request JSON
// -------------
//   {
//     "name":            "Editor",
//     "description":     "Can edit content",
//     "hierarchy_level": 30
//   }
//
// Response
// --------
//   201 Created  { "role_id": "<uuid>" }  
//   206 Partial  same body + "audit_error" if audit logging failed  
//   4xx/5xx      error JSON via app.respondWithError.
//
func (app *Application) CreateRoleHandler(w http.ResponseWriter, r *http.Request) {
	logger := app.Logger.GetLoggerWithContext(r).WithFunctionName("CreateRoleHandler")

	// Decode & validate request payload
	var input struct {
		Name           string  `json:"name"`
		Description    *string `json:"description,omitempty"`
		HierarchyLevel int     `json:"hierarchy_level"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		logger.Warn("Invalid JSON payload", "error", err)
		app.respondWithError(w, errors.New("invalid request payload"), http.StatusBadRequest)
		return
	}
	if input.Name == "" || input.HierarchyLevel < 0 {
		app.respondWithError(w, errors.New("name and non‑negative hierarchy_level are required"), http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	// Permission enforcement
	if !app.HasPermission(ctx, "create_role") {
		app.respondWithError(w, errors.New("forbidden: insufficient permissions"), http.StatusForbidden)
		return
	}

	// Create role via model
	role := &data.Role{
		Name:           input.Name,
		HierarchyLevel: input.HierarchyLevel,
	}
	if input.Description != nil {
		role.Description = *input.Description
	}
	if err := app.Models.Role.CreateRole(ctx, role); err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" { // unique_violation
			app.respondWithError(w, errors.New("role name already exists"), http.StatusConflict)
			return
		}
		logger.Error("CreateRole failed", "error", err)
		app.respondWithError(w, errors.New("failed to create role"), http.StatusInternalServerError)
		return
	}

	// Resolve Audit Action
	action, err := app.Models.Action.GetByName(ctx, "create_role")
	if err != nil || action == nil {
		logger.Warn("Audit action 'create_role' not found, attempting to create...", "error", err)
		actionID, createErr := app.Models.Action.CreateIfNotExists(ctx, "create_role", "Create a new role")
		if createErr != nil {
			logger.Error("Failed to create missing audit action", "error", createErr)
			// Allow main operation to succeed
		} else {
			action = &data.Action{ID: actionID}
		}
	}

	// Resolve Audit Entity Type
	entityType, err := app.Models.EntityType.GetByName(ctx, "role")
	if err != nil || entityType == nil {
		logger.Warn("Audit entity type 'role' not found, attempting to create...", "error", err)
		entityTypeID, createErr := app.Models.EntityType.CreateIfNotExists(ctx, "role", "Role entity")
		if createErr != nil {
			logger.Error("Failed to create missing entity type", "error", createErr)
			// Allow main operation to succeed
			entityType = &data.EntityType{ID: entityTypeID}
		}
	}

	// Insert audit record only if all pieces resolved
	actorID := app.getUserIDFromContext(ctx) // *uuid.UUID
	if actorID != nil && action != nil && entityType != nil {
		audit := data.AuditLog{
			ID:           uuid.New(),
			UserID:       actorID,
			ActionID:     action.ID,
			EntityTypeID: entityType.ID,
			EntityID:     role.ID.String(),
		}
		if err := app.Models.AuditLog.Insert(ctx, &audit); err != nil {
			// Audit logging failed (partial success)
			logger.Warn("Audit logging failed", "role_id", role.ID, "error", err)
			app.respondWithJSON(w, http.StatusPartialContent, jsonResponse{
				Error:   false,
				Message: "Role created, but audit logging failed",
				Data:    role.ID,
			})
			return
		}
	}

	// Success
	logger.Info("Role created", "role_id", role.ID)
	app.respondWithJSON(w, http.StatusCreated, jsonResponse{
		Error:   false,
		Message: "Role created successfully",
		Data:    role.ID,
	})
}


// UpdateRoleHandler handles partially updating via PATCH a user's role in the system.
// It uses structured logging, context management, and audit logging to ensure traceability and scalability.
func (app *Application) UpdateRoleHandler(w http.ResponseWriter, r *http.Request) {
	logger := app.Logger.GetLoggerWithContext(r).WithFunctionName("UpdateRoleHandler")

	var input struct {
		UserID string `json:"user_id"`
		RoleID string `json:"role_id"`
	}

	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		logger.Warn("Invalid request payload", "error", err)
		app.respondWithError(w, errors.New("invalid request payload"), http.StatusBadRequest)
		return
	}

	userUUID, err := uuid.Parse(input.UserID)
	if err != nil {
		logger.Warn("Invalid user UUID", "user_id", input.UserID, "error", err)
		app.respondWithError(w, errors.New("invalid user ID"), http.StatusBadRequest)
		return
	}

	roleUUID, err := uuid.Parse(input.RoleID)
	if err != nil {
		logger.Warn("Invalid role UUID", "role_id", input.RoleID, "error", err)
		app.respondWithError(w, errors.New("invalid role ID"), http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()


	role := &data.Role{
		ID: roleUUID,
		// Populate required fields like Name, etc.
	}

	err = app.Models.Role.UpdateRole(ctx, role)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			logger.Warn("User not found", "user_id", userUUID)
			app.respondWithError(w, errors.New("user not found"), http.StatusNotFound)
		} else {
			logger.Error("Failed to update role", "user_id", userUUID, "role_id", roleUUID, "error", err)
			app.respondWithError(w, errors.New("could not update role"), http.StatusInternalServerError)
		}
		return
	}

	// Audit log
	actionID := app.Preloaded.ActionIDs["update_user_role"]

	audit := data.AuditLog{
		ID:           uuid.New(),
		UserID:       app.getUserIDFromContext(ctx),
		ActionID:     actionID,
		EntityTypeID: app.Preloaded.EntityTypeIDs["users"],
		EntityID:     userUUID.String(),
	}

	ctxAudit, cancelAudit := context.WithTimeout(context.Background(), cfgTimeout)
	defer cancelAudit()

	if err := app.Models.AuditLog.Insert(ctxAudit, &audit); err != nil {
		// Audit logging failed (partial success)
		logger.Warn("Failed to log audit trail", "error", err)
		app.respondWithJSON(w, http.StatusPartialContent, jsonResponse{
			Error:   false,
			Message: "Role updated, but audit logging failed",
			Data: struct {
				UserID uuid.UUID `json:"user_id"`
				RoleID uuid.UUID `json:"role_id"`
			}{userUUID, roleUUID},
		})
		return
	}

	// Success
	logger.Info("Role updated", "user_id", userUUID, "role_id", roleUUID)
	app.respondWithJSON(w, http.StatusOK, jsonResponse{
		Error:   false,
		Message: "Role updated successfully",
		Data: struct {
			UserID uuid.UUID `json:"user_id"`
			RoleID uuid.UUID `json:"role_id"`
		}{userUUID, roleUUID},
	})
}


// DeleteRoleHandler performs the normal lifecycle removal of a role.
//
// Normal API deletion is a soft delete. Permanent deletion through
// RoleModel.DeleteRole remains an exceptional internal cleanup operation and
// is not exposed through this handler.
func (app *Application) DeleteRoleHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	logger := app.Logger.
		GetLoggerWithContext(r).
		WithFunctionName("DeleteRoleHandler")

	var input struct {
		RoleID string `json:"role_id"`
	}

	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		logger.Warn("Invalid request payload", "error", err)
		app.respondWithError(
			w,
			errors.New("invalid request payload"),
			http.StatusBadRequest,
		)
		return
	}

	roleID, err := uuid.Parse(input.RoleID)
	if err != nil || roleID == uuid.Nil {
		logger.Warn(
			"Invalid role ID",
			"role_id", input.RoleID,
			"error", err,
		)
		app.respondWithError(
			w,
			errors.New("invalid role_id format"),
			http.StatusBadRequest,
		)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	if err := app.Models.Role.SoftDelete(ctx, roleID); err != nil {
		if errors.Is(err, data.ErrRoleNotFound) {
			logger.Warn(
				"Role not found for deletion",
				"role_id", roleID,
			)
			app.respondWithError(
				w,
				errors.New("role not found"),
				http.StatusNotFound,
			)
			return
		}

		logger.Error(
			"Failed to delete role",
			"role_id", roleID,
			"error", err,
		)
		app.respondWithError(
			w,
			errors.New("failed to delete role"),
			http.StatusInternalServerError,
		)
		return
	}

	actorID := app.getUserIDFromContext(ctx)

	auditLog := data.AuditLog{
		ID:           uuid.New(),
		UserID:       actorID,
		ActionID:     app.Preloaded.ActionIDs["delete_user_role"],
		EntityTypeID: app.Preloaded.EntityTypeIDs["roles"],
		EntityID:     roleID.String(),
	}

	auditCtx, cancelAudit := context.WithTimeout(
		context.Background(),
		cfgTimeout,
	)
	defer cancelAudit()

	responseData := struct {
		RoleID uuid.UUID `json:"role_id"`
	}{
		RoleID: roleID,
	}

	if err := app.Models.AuditLog.Insert(auditCtx, &auditLog); err != nil {
		logger.Warn(
			"Role deleted but audit logging failed",
			"role_id", roleID,
			"error", err,
		)

		app.respondWithJSON(
			w,
			http.StatusPartialContent,
			jsonResponse{
				Error:   false,
				Message: "Role deleted, but audit logging failed",
				Data:    responseData,
			},
		)
		return
	}

	logger.Info(
		"Role deleted successfully",
		"role_id", roleID,
	)

	app.respondWithJSON(
		w,
		http.StatusOK,
		jsonResponse{
			Error:   false,
			Message: "Role deleted successfully",
			Data:    responseData,
		},
	)
}


// AssignRoleToUserHandler assigns a role to a user.
//
// • Validates input JSON (user_id, role, is_primary).  
// • Looks up the role; blocks if caller tries to assign an internal role
//   without being an internal user.  
// • Delegates to RoleModel.AssignRoleToUser().  
// • Dynamically resolves (or creates) action/entity‑type rows for audit logging.  
// • Emits structured logs and JSON responses consistent with the project.
func (app *Application) AssignRoleToUserHandler(w http.ResponseWriter, r *http.Request) {
	logger := app.Logger.GetLoggerWithContext(r).WithFunctionName("AssignRoleToUserHandler")

	// 1️⃣  Parse body -----------------------------------------------------------
	var in struct {
		UserID    string `json:"user_id"`
		RoleName  string `json:"role"`
		IsPrimary bool   `json:"is_primary"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		app.respondWithError(w, errors.New("invalid request payload"), http.StatusBadRequest)
		return
	}

	targetUserID, err := uuid.Parse(in.UserID)
	if err != nil || targetUserID == uuid.Nil {
		app.respondWithError(w, errors.New("invalid user_id format"), http.StatusBadRequest)
		return
	}

	// 2️⃣  Resolve role ---------------------------------------------------------
	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	role, err := app.Models.Role.GetRoleByName(ctx, in.RoleName) // (*data.Role, error)
	if err != nil {
		app.respondWithError(w, errors.New("role lookup failed"), http.StatusInternalServerError)
		return
	}
	if role == nil {
		app.respondWithError(w, errors.New("role not found"), http.StatusNotFound)
		return
	}

	// 3️⃣  Guard‑rail: only internal → internal --------------------------------
	if isInternalRole(role.Name) && !app.IsInternalUser(ctx) {
		app.respondWithError(w, errors.New("forbidden: cannot assign internal role"), http.StatusForbidden)
		return
	}

	// 4️⃣  Persist assignment ---------------------------------------------------
	assignerID := app.getUserIDFromContext(ctx)
	if err := app.Models.Role.AssignRoleToUser(
		ctx, targetUserID, role.ID, assignerID, in.IsPrimary,
	); err != nil {
		app.respondWithError(w, errors.New("failed to assign role"), http.StatusInternalServerError)
		return
	}

	// Resolve dynamic action
	action, err := app.Models.Action.GetByName(ctx, "assign_role")
	if err != nil || action == nil {
		logger.Warn("Audit action 'assign_role' not found, attempting to create...", "error", err)
		actionID, createErr := app.Models.Action.CreateIfNotExists(ctx, "assign_role", "Assign a new role to a user")
		if createErr != nil {
			logger.Error("Failed to create missing audit action", "error", createErr)
		} else {
			action = &data.Action{ID: actionID}
		}
	}

	// Resolve dynamic entity type
	entityType, err := app.Models.EntityType.GetByName(ctx, "roles")
	if err != nil || entityType == nil {
		logger.Warn("Audit entity type 'roles' not found, attempting to create...", "error", err)
		entityTypeID, createErr := app.Models.EntityType.CreateIfNotExists(ctx, "roles", "User role assignments")
		if createErr != nil {
			logger.Error("Failed to create missing entity type", "error", createErr)
		} else {
			entityType = &data.EntityType{ID: entityTypeID}
		}
	}

	// Insert Audit Log
	auditLog := data.AuditLog{
		ID:           uuid.New(),
		UserID:       assignerID,
		ActionID:     action.ID,
		EntityTypeID: entityType.ID,
		EntityID:     targetUserID.String(),
	}

	if err := app.Models.AuditLog.Insert(ctx, &auditLog); err != nil {
		// Audit logging failed (partial success)
		logger.Warn("Failed to log audit trail", "error", err)
		app.respondWithJSON(w, http.StatusPartialContent, jsonResponse{
			Error:   false,
			Message: "Role assigned, but audit logging failed",
			Data: struct {
				UserID    uuid.UUID `json:"user_id"`
				RoleID    uuid.UUID `json:"role_id"`
				Role      string    `json:"role"`
				IsPrimary bool      `json:"is_primary"`
			}{targetUserID, role.ID, role.Name, in.IsPrimary},
		})
		return
	}

	// Success
	logger.Info("Role assigned", "target_user_id", targetUserID, "role", role.Name, "is_primary", in.IsPrimary, "assigned_by", assignerID)
	app.respondWithJSON(w, http.StatusCreated, jsonResponse{
		Error:   false,
		Message: "Role assigned successfully",
		Data: struct {
			UserID    uuid.UUID `json:"user_id"`
			RoleID    uuid.UUID `json:"role_id"`
			Role      string    `json:"role"`
			IsPrimary bool      `json:"is_primary"`
		}{targetUserID, role.ID, role.Name, in.IsPrimary},
	})
}


// RevokeRoleFromUserHandler revokes one current role assignment from a user.
//
// Revocation affects only the user-role assignment. It does not delete,
// deactivate, or otherwise modify the underlying role definition.
func (app *Application) RevokeRoleFromUserHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	logger := app.Logger.
		GetLoggerWithContext(r).
		WithFunctionName("RevokeRoleFromUserHandler")

	var input struct {
		UserID string `json:"user_id"`
		RoleID string `json:"role_id"`
	}

	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		logger.Warn("Invalid request payload", "error", err)
		app.respondWithError(
			w,
			errors.New("invalid request payload"),
			http.StatusBadRequest,
		)
		return
	}

	userID, err := uuid.Parse(input.UserID)
	if err != nil || userID == uuid.Nil {
		logger.Warn(
			"Invalid user ID",
			"user_id", input.UserID,
			"error", err,
		)
		app.respondWithError(
			w,
			errors.New("invalid user_id format"),
			http.StatusBadRequest,
		)
		return
	}

	roleID, err := uuid.Parse(input.RoleID)
	if err != nil || roleID == uuid.Nil {
		logger.Warn(
			"Invalid role ID",
			"role_id", input.RoleID,
			"error", err,
		)
		app.respondWithError(
			w,
			errors.New("invalid role_id format"),
			http.StatusBadRequest,
		)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	if err := app.Models.Role.RevokeRole(ctx, userID, roleID); err != nil {
		if errors.Is(err, data.ErrRoleNotFound) {
			logger.Warn(
				"Current role assignment not found",
				"user_id", userID,
				"role_id", roleID,
			)
			app.respondWithError(
				w,
				errors.New("role assignment not found"),
				http.StatusNotFound,
			)
			return
		}

		logger.Error(
			"Failed to revoke role from user",
			"user_id", userID,
			"role_id", roleID,
			"error", err,
		)
		app.respondWithError(
			w,
			errors.New("failed to revoke role"),
			http.StatusInternalServerError,
		)
		return
	}

	actorID := app.getUserIDFromContext(ctx)

	auditLog := data.AuditLog{
		ID:           uuid.New(),
		UserID:       actorID,
		ActionID:     app.Preloaded.ActionIDs["revoke_user_role"],
		EntityTypeID: app.Preloaded.EntityTypeIDs["roles"],
		EntityID:     fmt.Sprintf("%s:%s", userID, roleID),
	}

	auditCtx, cancelAudit := context.WithTimeout(
		context.Background(),
		cfgTimeout,
	)
	defer cancelAudit()

	responseData := struct {
		UserID uuid.UUID `json:"user_id"`
		RoleID uuid.UUID `json:"role_id"`
	}{
		UserID: userID,
		RoleID: roleID,
	}

	if err := app.Models.AuditLog.Insert(auditCtx, &auditLog); err != nil {
		logger.Warn(
			"Role revoked but audit logging failed",
			"user_id", userID,
			"role_id", roleID,
			"error", err,
		)

		app.respondWithJSON(
			w,
			http.StatusPartialContent,
			jsonResponse{
				Error:   false,
				Message: "Role revoked, but audit logging failed",
				Data:    responseData,
			},
		)
		return
	}

	logger.Info(
		"Role revoked from user",
		"user_id", userID,
		"role_id", roleID,
	)

	app.respondWithJSON(
		w,
		http.StatusOK,
		jsonResponse{
			Error:   false,
			Message: "Role revoked successfully",
			Data:    responseData,
		},
	)
}


// GetRolesForUserHandler retrieves all roles assigned to the currently authenticated user.
func (app *Application) GetRolesForUserHandler(w http.ResponseWriter, r *http.Request) {
	logger := app.Logger.GetLoggerWithContext(r).WithFunctionName("GetRolesForUserHandler")

	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	userID := app.getUserIDFromContext(ctx)
	if userID == nil {
		logger.Warn("Missing or invalid user ID in context")
		app.respondWithError(w, errors.New("unauthorized"), http.StatusUnauthorized)
		return
	}

	roles, err := app.Models.Role.GetRolesByUserID(ctx, *userID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			logger.Info("No roles found for user", "user_id", userID)
			roles = []*data.Role{} // return empty list
		} else {
			logger.Error("Failed to retrieve roles", "user_id", userID, "error", err)
			app.respondWithError(w, fmt.Errorf("failed to retrieve roles"), http.StatusInternalServerError)
			return
		}
	}

	// Audit log
	actionID := app.Preloaded.ActionIDs["view_roles"]

	auditLog := data.AuditLog{
		ID:           uuid.New(),
		UserID:       userID,
		ActionID:     actionID,
		EntityTypeID: app.Preloaded.EntityTypeIDs["users"],
		EntityID:     userID.String(),
	}

	ctxAudit, cancelAudit := context.WithTimeout(context.Background(), cfgTimeout)
	defer cancelAudit()
	if err := app.Models.AuditLog.Insert(ctxAudit, &auditLog); err != nil {
		// Audit logging failed (partial success)
		logger.Warn("Failed to log audit trail", "error", err)
		app.respondWithJSON(w, http.StatusPartialContent, jsonResponse{
			Error:   false,
			Message: "Roles retrieved, but audit logging failed",
			Data:    roles,
		})
		return
	}

	// Success
	logger.Info("Retrieved roles for user", "user_id", userID, "roles_count", len(roles))
	app.respondWithJSON(w, http.StatusOK, jsonResponse{
		Error:   false,
		Message: "Roles retrieved successfully",
		Data:    roles,
	})
}
