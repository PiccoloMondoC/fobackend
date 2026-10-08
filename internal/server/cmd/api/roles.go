// Package main provides HTTP handlers for the Platform API.
//
// focodebase/fobackend/internal/server/cmd/api/roles.go
//
// GTM:
//
//	Layer: 2.2 Identity / Auth Domain
//	Release Class: SPINE
//	Reason:
//	  Role administration and user-role assignment are release-critical
//	  authorization infrastructure. These handlers support role definition
//	  lookup, creation, partial update, and unused-role retirement, plus
//	  assignment and revocation of NON-administrative roles.
//
//	  Administrative roles (super_admin, admin) are governed exclusively by
//	  the administrative governance workflow (admin_governance.go). Every
//	  assignment path here refuses them, and the database consistency
//	  triggers refuse any administrative role assignment without an active
//	  appointment, so this surface cannot create, strip, or bypass an
//	  administrator.
//
// SPINE Rule:
//
//	Keep compiling.
//	Keep production-ready.
//	Preserve route permission names identical to handler checks and seeds.
//	Preserve the distinction between role lifecycle and user-role assignment.
//	Preserve hierarchy: the actor must outrank the target account.
//	Preserve canonical soft-delete for role removal; refuse removing a role
//	that live accounts still hold.
//	Do not return wrapped internal errors to clients.
//	Block deployment if this file breaks build, role administration,
//	assignment integrity, or authorization integrity.
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
	roleAuditEntityType            = "roles"
	roleAuditEntityTypeDescription = "User role assignments"
)

func parseRoleIDParam(r *http.Request) (uuid.UUID, bool) {
	id, err := uuid.Parse(strings.TrimSpace(chi.URLParam(r, "roleID")))
	return id, err == nil && id != uuid.Nil
}

// roleErrorResponse maps role-model errors to a status, code, and safe message.
func roleErrorResponse(err error) (int, string, string) {
	switch {
	case errors.Is(err, data.ErrRoleNotFound):
		return http.StatusNotFound, "role_not_found", "role not found"
	case errors.Is(err, data.ErrUserNotFound):
		return http.StatusNotFound, "account_not_found", "account not found"
	case errors.Is(err, data.ErrRoleInactive):
		return http.StatusConflict, "role_inactive", "role is inactive"
	case errors.Is(err, data.ErrRoleInUse):
		return http.StatusConflict, "role_in_use", "role is still assigned to accounts"
	case errors.Is(err, data.ErrRoleNameTaken):
		return http.StatusConflict, "role_name_taken", "role name already exists"
	case errors.Is(err, data.ErrRoleInvalidInput):
		return http.StatusBadRequest, "invalid_request", "invalid role definition"
	case errors.Is(err, data.ErrLastActiveRole):
		return http.StatusConflict, "last_role", "an account must keep at least one active role"
	case errors.Is(err, data.ErrGovernanceRoleProtected):
		return http.StatusConflict, "protected_role", "administrative roles are governed by platform seeding and appointments"
	case errors.Is(err, data.ErrRootSuperAdminProtected):
		return http.StatusConflict, "root_protected", "the Root Super Admin is protected"
	case errors.Is(err, data.ErrAdministrativeInconsistency):
		return http.StatusConflict, "governance_invariant", "the change would violate an administrative governance rule"
	case errors.Is(err, data.ErrConcurrentGovernanceChange):
		return http.StatusConflict, "concurrent_change", "the account changed while this request was processed; reload and try again"
	case errors.Is(err, data.ErrPermissionNotFound):
		return http.StatusBadRequest, "permission_not_found", "one or more permissions do not exist"
	default:
		return http.StatusInternalServerError, "server_error", "internal server error"
	}
}

func (app *Application) respondWithRoleError(w http.ResponseWriter, r *http.Request, operation string, err error) {
	status, code, message := roleErrorResponse(err)
 if status>=500 && errIsGovernance(err){status,code,message=governanceErrorResponse(err)}
	logger := app.Logger.GetLoggerWithContext(r).WithFunctionName("respondWithRoleError")
	if status >= http.StatusInternalServerError {
		logger.Error("role request failed", "operation", operation, "error", err)
	} else {
		logger.Warn("role request refused", "operation", operation, "code", code)
	}
	app.respondWithErrorCode(w, status, code, message)
}

// auditRoleAction records a role read or mutation. Mutations report a failed
// audit as 206 (done, not audited), consistent with the rest of the console.
func (app *Application) auditRoleAction(ctx context.Context, action, description, entityID string) error {
	return app.insertGovernanceAudit(
		ctx,
		app.getUserIDFromContext(ctx),
		action,
		description,
		roleAuditEntityType,
		roleAuditEntityTypeDescription,
		entityID,
	)
}

func (app *Application) respondRoleMutation(w http.ResponseWriter,r *http.Request,status int,message string,payload any,auditErr error) {
 // Successful callers have already committed mutation and audit atomically.
 app.respondWithJSON(w,status,jsonResponse{Error:false,Message:message,Data:payload})
}

// ListRolesHandler returns all non-deleted roles.
//
//	GET /api/v1/roles/   list_roles
func (app *Application) ListRolesHandler(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	if !app.HasPermission(ctx, "list_roles") {
		app.respondWithErrorCode(w, http.StatusForbidden, "insufficient_permission", "forbidden: insufficient permissions")
		return
	}

	roles, err := app.Models.Role.ListRoles(ctx)
	if err != nil {
		app.respondWithRoleError(w, r, "list_roles", err)
		return
	}
	if roles == nil {
		roles = []*data.Role{}
	}

	if err := app.auditRoleAction(ctx, "list_roles", "List all roles in the system", "list_all"); err != nil {
		app.Logger.GetLoggerWithContext(r).Warn("roles listed but audit failed", "error", err)
	}

	app.respondWithJSON(w, http.StatusOK, jsonResponse{Error: false, Message: "Roles retrieved successfully", Data: roles})
}

// GetRoleByIDHandler returns one role.
//
//	GET /api/v1/roles/{roleID}   read_role
func (app *Application) GetRoleByIDHandler(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	if !app.HasPermission(ctx, "read_role") {
		app.respondWithErrorCode(w, http.StatusForbidden, "insufficient_permission", "forbidden: insufficient permissions")
		return
	}
	roleID, ok := parseRoleIDParam(r)
	if !ok {
		app.respondWithErrorCode(w, http.StatusBadRequest, "invalid_request", "invalid role ID")
		return
	}

	role, err := app.Models.Role.GetRoleByID(ctx, roleID)
	if err != nil {
		app.respondWithRoleError(w, r, "read_role", err)
		return
	}

	if err := app.auditRoleAction(ctx, "read_role", "Read one role definition", roleID.String()); err != nil {
		app.Logger.GetLoggerWithContext(r).Warn("role read but audit failed", "error", err)
	}

	app.respondWithJSON(w, http.StatusOK, jsonResponse{Error: false, Message: "Role retrieved successfully", Data: role})
}

type createRoleInput struct {
	Name               string `json:"name"`
	Description        string `json:"description"`
	HierarchyLevel     int    `json:"hierarchy_level"`
	IsInternal         bool   `json:"is_internal"`
	AssignableAtSignup bool   `json:"assignable_at_signup"`
	ApprovalRequired   bool   `json:"approval_required"`
}

// CreateRoleHandler creates a non-administrative role definition.
//
//	POST /api/v1/roles/   create_role (Super Admin tier)
func (app *Application) CreateRoleHandler(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	if !app.HasPermission(ctx, "create_role") {
		app.respondWithErrorCode(w, http.StatusForbidden, "insufficient_permission", "forbidden: insufficient permissions")
		return
	}

	var input createRoleInput
	if err := app.readJSON(w, r, &input); err != nil {
		app.respondWithErrorCode(w, http.StatusBadRequest, "invalid_request", "invalid request body")
		return
	}
	input.Name = strings.TrimSpace(input.Name)
	input.Description = strings.TrimSpace(input.Description)
	if input.Name == "" || input.Description == "" || input.HierarchyLevel < 0 ||
		(input.IsInternal && input.AssignableAtSignup) {
		app.respondWithErrorCode(w, http.StatusBadRequest, "invalid_request",
			"name, description, and a non-negative hierarchy_level are required; internal roles cannot be signup-assignable")
		return
	}
	if data.IsAdministrativeRoleName(input.Name) {
		app.respondWithErrorCode(w, http.StatusConflict, "protected_role", "administrative roles are governed by platform seeding")
		return
	}

	role := &data.Role{
		Name:               input.Name,
		Description:        input.Description,
		HierarchyLevel:     input.HierarchyLevel,
		IsInternal:         input.IsInternal,
		AssignableAtSignup: input.AssignableAtSignup,
		ApprovalRequired:   input.ApprovalRequired,
	}
	actorID,ok:=app.governanceActor(w,ctx);if !ok{return}
 if err := app.InternalServices.CreateOrdinaryRoleInternal(ctx,actorID,role); err != nil {
		if data.IsUniqueViolation(err) {
			app.respondWithRoleError(w, r, "create_role", data.ErrRoleNameTaken)
			return
		}
		if data.IsCheckViolation(err) {
			app.respondWithRoleError(w, r, "create_role", data.ErrRoleInvalidInput)
			return
		}
		app.respondWithRoleError(w, r, "create_role", err)
		return
	}

	var auditErr error // mutation and audit have already committed together
	app.respondRoleMutation(w, r, http.StatusCreated, "Role created successfully", role, auditErr)
}

type updateRoleInput struct {
	Name               *string `json:"name,omitempty"`
	Description        *string `json:"description,omitempty"`
	HierarchyLevel     *int    `json:"hierarchy_level,omitempty"`
	IsInternal         *bool   `json:"is_internal,omitempty"`
	AssignableAtSignup *bool   `json:"assignable_at_signup,omitempty"`
	ApprovalRequired   *bool   `json:"approval_required,omitempty"`
	IsActive           *bool   `json:"is_active,omitempty"`
}

// UpdateRoleHandler partially updates a role definition atomically.
// Governance roles are refused by trigger (409 protected_role).
//
//	PATCH /api/v1/roles/{roleID}   update_role (Super Admin tier)
func (app *Application) UpdateRoleHandler(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	if !app.HasPermission(ctx, "update_role") {
		app.respondWithErrorCode(w, http.StatusForbidden, "insufficient_permission", "forbidden: insufficient permissions")
		return
	}
	roleID, ok := parseRoleIDParam(r)
	if !ok {
		app.respondWithErrorCode(w, http.StatusBadRequest, "invalid_request", "invalid role ID")
		return
	}

	var input updateRoleInput
	if err := app.readJSON(w, r, &input); err != nil {
		app.respondWithErrorCode(w, http.StatusBadRequest, "invalid_request", "invalid request body")
		return
	}
	if input.Name != nil && data.IsAdministrativeRoleName(*input.Name) {
		app.respondWithErrorCode(w, http.StatusConflict, "protected_role", "administrative role names are reserved")
		return
	}

 actorID,ok:=app.governanceActor(w,ctx);if !ok{return}
 var role *data.Role
 err:=app.InternalServices.WithRoleAdministrationInternal(ctx,actorID,"update_role","update_role",roleID.String(),func(ctx context.Context,tx pgx.Tx) error {
 var err error
	role, err = app.Models.Role.UpdateRolePartialTx(ctx,tx, roleID, data.RolePatch{
		Name:               input.Name,
		Description:        input.Description,
		HierarchyLevel:     input.HierarchyLevel,
		IsInternal:         input.IsInternal,
		AssignableAtSignup: input.AssignableAtSignup,
		ApprovalRequired:   input.ApprovalRequired,
		IsActive:           input.IsActive,
	})
 return err
 })
	if err != nil {
		app.respondWithRoleError(w, r, "update_role", err)
		return
	}

	var auditErr error
	app.respondRoleMutation(w, r, http.StatusOK, "Role updated successfully", role, auditErr)
}

// DeleteRoleHandler soft-deletes a role that no live account holds.
// Hard deletion is never exposed.
//
//	DELETE /api/v1/roles/{roleID}   delete_role (Super Admin tier)
func (app *Application) DeleteRoleHandler(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	if !app.HasPermission(ctx, "delete_role") {
		app.respondWithErrorCode(w, http.StatusForbidden, "insufficient_permission", "forbidden: insufficient permissions")
		return
	}
	roleID, ok := parseRoleIDParam(r)
	if !ok {
		app.respondWithErrorCode(w, http.StatusBadRequest, "invalid_request", "invalid role ID")
		return
	}

	actorID,ok:=app.governanceActor(w,ctx);if !ok{return}
 if err := app.InternalServices.WithRoleAdministrationInternal(ctx,actorID,"delete_role","delete_role",roleID.String(),func(ctx context.Context,tx pgx.Tx) error {
 return app.Models.Role.SoftDeleteUnusedTx(ctx,tx,roleID)
 }); err != nil {
		app.respondWithRoleError(w, r, "delete_role", err)
		return
	}

	var auditErr error
	app.respondRoleMutation(w, r, http.StatusOK, "Role deleted successfully",
		struct {
			RoleID uuid.UUID `json:"role_id"`
		}{roleID}, auditErr)
}

type roleAssignmentInput struct {
	UserID    string `json:"user_id"`
	RoleID    string `json:"role_id,omitempty"`
	RoleName  string `json:"role,omitempty"`
	IsPrimary bool   `json:"is_primary,omitempty"`
}

// resolveAssignmentRole resolves the role named by ID or name.
func (app *Application) resolveAssignmentRole(ctx context.Context, in roleAssignmentInput) (*data.Role, error) {
	if strings.TrimSpace(in.RoleID) != "" {
		id, err := uuid.Parse(strings.TrimSpace(in.RoleID))
		if err != nil || id == uuid.Nil {
			return nil, data.ErrRoleInvalidInput
		}
		return app.Models.Role.GetRoleByID(ctx, id)
	}
	if strings.TrimSpace(in.RoleName) == "" {
		return nil, data.ErrRoleInvalidInput
	}
	return app.Models.Role.GetRoleByName(ctx, in.RoleName)
}

// authorizeRoleAssignmentChange applies the shared guards for assign and
// revoke: administrative roles are refused; self-targeting is refused; the
// actor must outrank the target's administrative standing; the Root is
// protected. It writes the refusal and returns false when refused.
func (app *Application) authorizeRoleAssignmentChange(
	w http.ResponseWriter,
	r *http.Request,
	operation string,
	actorID uuid.UUID,
	targetID uuid.UUID,
	role *data.Role,
) bool {
	if data.IsAdministrativeRoleName(role.Name) {
		app.respondWithErrorCode(w, http.StatusConflict, "governance_role_requires_appointment",
			"administrative roles are granted and removed only through administrator governance")
		return false
	}
	if actorID == targetID {
		app.respondWithErrorCode(w, http.StatusConflict, "self_governance_refused",
			"you cannot change your own roles")
		return false
	}
	if err := app.InternalServices.AuthorizeAccountStewardshipInternal(r.Context(), actorID, targetID); err != nil {
		app.respondWithGovernanceError(w, r, operation, err)
		return false
	}
	return true
}

// AssignRoleToUserHandler assigns a non-administrative role to an account.
//
//	POST /api/v1/roles/assign   assign_role
func (app *Application) AssignRoleToUserHandler(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	if !app.HasPermission(ctx, "assign_role") {
		app.respondWithErrorCode(w, http.StatusForbidden, "insufficient_permission", "forbidden: insufficient permissions")
		return
	}
	actorID := app.getUserIDFromContext(ctx)
	if actorID == nil {
		app.respondWithErrorCode(w, http.StatusUnauthorized, "unauthenticated", "unauthorized")
		return
	}

	var in roleAssignmentInput
	if err := app.readJSON(w, r, &in); err != nil {
		app.respondWithErrorCode(w, http.StatusBadRequest, "invalid_request", "invalid request body")
		return
	}
	targetID, err := uuid.Parse(strings.TrimSpace(in.UserID))
	if err != nil || targetID == uuid.Nil {
		app.respondWithErrorCode(w, http.StatusBadRequest, "invalid_request", "invalid user_id")
		return
	}

	role, err := app.resolveAssignmentRole(ctx, in)
	if err != nil {
		app.respondWithRoleError(w, r, "assign_role", err)
		return
	}
	if !app.authorizeRoleAssignmentChange(w, r, "assign_role", *actorID, targetID, role) {
		return
	}

	if err := app.InternalServices.ChangeOrdinaryRoleInternal(ctx,*actorID,targetID,role.ID,in.IsPrimary,false); err != nil {
		app.respondWithRoleError(w, r, "assign_role", err)
		return
	}

	payload := struct {
		UserID    uuid.UUID `json:"user_id"`
		RoleID    uuid.UUID `json:"role_id"`
		Role      string    `json:"role"`
		IsPrimary bool      `json:"is_primary"`
	}{targetID, role.ID, role.Name, in.IsPrimary}

	var auditErr error
	app.respondRoleMutation(w, r, http.StatusCreated, "Role assigned successfully", payload, auditErr)
}

// RevokeRoleFromUserHandler revokes a non-administrative role from an
// account. The account's last active role cannot be revoked.
//
//	POST /api/v1/roles/revoke   revoke_role
func (app *Application) RevokeRoleFromUserHandler(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	if !app.HasPermission(ctx, "revoke_role") {
		app.respondWithErrorCode(w, http.StatusForbidden, "insufficient_permission", "forbidden: insufficient permissions")
		return
	}
	actorID := app.getUserIDFromContext(ctx)
	if actorID == nil {
		app.respondWithErrorCode(w, http.StatusUnauthorized, "unauthenticated", "unauthorized")
		return
	}

	var in roleAssignmentInput
	if err := app.readJSON(w, r, &in); err != nil {
		app.respondWithErrorCode(w, http.StatusBadRequest, "invalid_request", "invalid request body")
		return
	}
	targetID, err := uuid.Parse(strings.TrimSpace(in.UserID))
	if err != nil || targetID == uuid.Nil {
		app.respondWithErrorCode(w, http.StatusBadRequest, "invalid_request", "invalid user_id")
		return
	}

	role, err := app.resolveAssignmentRole(ctx, in)
	if err != nil {
		app.respondWithRoleError(w, r, "revoke_role", err)
		return
	}
	if !app.authorizeRoleAssignmentChange(w, r, "revoke_role", *actorID, targetID, role) {
		return
	}

	if err := app.InternalServices.ChangeOrdinaryRoleInternal(ctx,*actorID,targetID,role.ID,false,true); err != nil {
		if errors.Is(err, data.ErrRoleNotFound) {
			app.respondWithErrorCode(w, http.StatusNotFound, "assignment_not_found", "role assignment not found")
			return
		}
		app.respondWithRoleError(w, r, "revoke_role", err)
		return
	}

	payload := struct {
		UserID uuid.UUID `json:"user_id"`
		RoleID uuid.UUID `json:"role_id"`
	}{targetID, role.ID}

	var auditErr error
	app.respondRoleMutation(w, r, http.StatusOK, "Role revoked successfully", payload, auditErr)
}

// GetRolesForUserHandler returns the active roles of the account in the URL.
//
//	GET /api/v1/roles/user/{userID}   read_user_roles
func (app *Application) GetRolesForUserHandler(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	if !app.HasPermission(ctx, "read_user_roles") {
		app.respondWithErrorCode(w, http.StatusForbidden, "insufficient_permission", "forbidden: insufficient permissions")
		return
	}
	targetID, err := uuid.Parse(strings.TrimSpace(chi.URLParam(r, "userID")))
	if err != nil || targetID == uuid.Nil {
		app.respondWithErrorCode(w, http.StatusBadRequest, "invalid_request", "invalid user ID")
		return
	}

	roles, err := app.Models.Role.GetRolesByUserID(ctx, targetID)
	if err != nil {
		app.respondWithRoleError(w, r, "read_user_roles", err)
		return
	}
	if roles == nil {
		roles = []*data.Role{}
	}

	if err := app.auditRoleAction(ctx, "read_user_roles", "Read the roles held by an account", targetID.String()); err != nil {
		app.Logger.GetLoggerWithContext(r).Warn("user roles read but audit failed", "error", err)
	}

	app.respondWithJSON(w, http.StatusOK, jsonResponse{Error: false, Message: "Roles retrieved successfully", Data: roles})
}
