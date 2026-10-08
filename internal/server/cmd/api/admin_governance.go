// Package main provides HTTP handlers for administrative governance.
//
// focodebase/fobackend/internal/server/cmd/api/admin_governance.go
//
// GTM:
//
//	Layer: 3.1 API / Administrative Control Plane
//	Release Class: SPINE
//	Reason:
//	  HTTP boundary for the administrative hierarchy (Root Super Admin ->
//	  Super Admin -> Admin): administrator directory, account directory,
//	  governance history, and appointment lifecycle.
//
//	  Authority is decided by services.AuthorizeGovernanceOperation from state
//	  read under lock. Route permissions are the coarse gate; handlers
//	  re-check them as defense in depth. Every error carries a stable code
//	  (data.code) so the Admin Console never parses messages, and no
//	  authorization or targeting failure is ever reported as 401.
//
// SPINE Rule:
//
//	Keep compiling.
//	Keep production-ready.
//	Preserve 401 exclusively for authentication failure.
//	Preserve coded, non-leaking error responses.
//	Do not log passwords, confirmation text, or directory search text.
//	Block deployment if this file breaks build, hierarchy enforcement, or
//	error-contract stability.
package main

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/PiccoloMondoC/focodebase/fobackend/internal/data"
	"github.com/PiccoloMondoC/focodebase/fobackend/internal/services"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

const (
	permissionReadAdministrators    = "read_administrators"
	permissionReadAccounts          = "read_accounts"
	permissionAppointAdmin          = "appoint_admin"
	permissionAppointSuperAdmin     = "appoint_super_admin"
	permissionDemoteAdministrator   = "demote_administrator"
	permissionSuspendAdministrator  = "suspend_administrator"
	permissionRestoreAdministrator  = "restore_administrator"
	permissionRevokeAdministrator   = "revoke_administrator"
	accountDirectoryDefaultPageSize = 25
)

// governanceErrorResponse maps a governance or account-lifecycle error to a
// status, stable code, and safe message. Unknown errors are server failures
// with a generic message; their detail is logged, never returned.
func governanceErrorResponse(err error) (int, string, string) {
	code := services.GovernanceDenialCode(err)
	switch code {
	case "self_governance_refused":
		return http.StatusConflict, code, "administrators cannot change their own administrative standing or close their own account here"
	case "root_protected":
		return http.StatusConflict, code, "the Root Super Admin is protected"
	case "insufficient_authority":
		return http.StatusForbidden, code, "forbidden: insufficient authority over this account"
	case "account_not_found":
		return http.StatusNotFound, code, "account not found"
	case "account_ineligible":
		return http.StatusConflict, code, "account is not eligible: it must be active with a confirmed email"
	case "invalid_transition":
		return http.StatusConflict, code, "this change does not apply to the account's current standing"
	case "administrator_account":
		return http.StatusConflict, code, "revoke administrative standing before closing this account"
	case "administrator_self_closure":
		return http.StatusConflict, code, "an administrator cannot close their own account"
	case "reauthentication_failed":
		return http.StatusForbidden, code, "your password was not accepted"
	case "reauthentication_unavailable":
		return http.StatusConflict, code, "this action requires an account password"
	case "confirmation_mismatch":
		return http.StatusBadRequest, code, "confirmation does not match the account"
	case "invalid_request":
		return http.StatusBadRequest, code, "invalid request"
	case "concurrent_change":
		return http.StatusConflict, code, "the account changed while this request was processed; reload and try again"
	case "governance_invariant":
		return http.StatusConflict, code, "the change would violate an administrative governance rule"
	default:
		return http.StatusInternalServerError, "server_error", "internal server error"
	}
}

func (app *Application) respondWithGovernanceError(w http.ResponseWriter, r *http.Request, operation string, err error) {
	status, code, message := governanceErrorResponse(err)
	logger := app.Logger.GetLoggerWithContext(r).WithFunctionName("respondWithGovernanceError")
	if status >= http.StatusInternalServerError {
		logger.Error("governance request failed", "operation", operation, "error", err)
	} else {
		logger.Warn("governance request refused", "operation", operation, "code", code)
	}
	app.respondWithErrorCode(w, status, code, message)
}

func (app *Application) governanceActor(w http.ResponseWriter, ctx context.Context) (uuid.UUID, bool) {
	id := app.getUserIDFromContext(ctx)
	if id == nil || *id == uuid.Nil {
		app.respondWithErrorCode(w, http.StatusUnauthorized, "unauthenticated", "unauthorized")
		return uuid.Nil, false
	}
	return *id, true
}

func (app *Application) requireGovernancePermission(w http.ResponseWriter, ctx context.Context, permission string) bool {
	if !app.HasPermission(ctx, permission) {
		app.respondWithErrorCode(w, http.StatusForbidden, "insufficient_permission", "forbidden: insufficient permissions")
		return false
	}
	return true
}

func parseGovernanceTarget(r *http.Request) (uuid.UUID, bool) {
	id, err := uuid.Parse(strings.TrimSpace(chi.URLParam(r, "userID")))
	if err != nil || id == uuid.Nil {
		return uuid.Nil, false
	}
	return id, true
}

// ListAdministratorsHandler returns administrators with the caller's
// standing and per-row available actions.
//
//	GET /api/v1/admin-governance/administrators?include_revoked=true
func (app *Application) ListAdministratorsHandler(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	actorID, ok := app.governanceActor(w, ctx)
	if !ok || !app.requireGovernancePermission(w, ctx, permissionReadAdministrators) {
		return
	}

	includeRevoked := strings.EqualFold(r.URL.Query().Get("include_revoked"), "true")

	directory, err := app.InternalServices.ListAdministratorsInternal(ctx, actorID, includeRevoked)
	if err != nil {
		app.respondWithGovernanceError(w, r, "list_administrators", err)
		return
	}

	if err := app.insertGovernanceAudit(ctx, &actorID, "list_administrators", "List administrators",
		data.GovernanceEntityAdministrator, "Administrative standing of an account", "directory"); err != nil {
		app.Logger.GetLoggerWithContext(r).Warn("administrator directory read but audit failed", "error", err)
	}

	app.respondWithJSON(w, http.StatusOK, jsonResponse{
		Error:   false,
		Message: "Administrators retrieved successfully",
		Data:    directory,
	})
}

// GetAdministratorHistoryHandler returns governance history for one account.
//
//	GET /api/v1/admin-governance/administrators/{userID}/history
func (app *Application) GetAdministratorHistoryHandler(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	actorID, ok := app.governanceActor(w, ctx)
	if !ok || !app.requireGovernancePermission(w, ctx, permissionReadAdministrators) {
		return
	}
	targetID, ok := parseGovernanceTarget(r)
	if !ok {
		app.respondWithErrorCode(w, http.StatusBadRequest, "invalid_request", "invalid account ID")
		return
	}

	history, err := app.InternalServices.ListAdministratorHistoryInternal(ctx, actorID, targetID, data.MaxGovernanceHistory)
	if err != nil {
		app.respondWithGovernanceError(w, r, "read_administrator_history", err)
		return
	}

	if err := app.insertGovernanceAudit(ctx, &actorID, "read_administrator_history", "Read administrative governance history",
		data.GovernanceEntityAdministrator, "Administrative standing of an account", targetID.String()); err != nil {
		app.Logger.GetLoggerWithContext(r).Warn("governance history read but audit failed", "error", err)
	}

	app.respondWithJSON(w, http.StatusOK, jsonResponse{
		Error:   false,
		Message: "Governance history retrieved successfully",
		Data:    history,
	})
}

// SearchAccountsHandler lists or searches the account directory.
//
//	GET /api/v1/admin-governance/accounts?q=&limit=&offset=
//
// The search text is never logged or audited.
func (app *Application) SearchAccountsHandler(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	actorID, ok := app.governanceActor(w, ctx)
	if !ok || !app.requireGovernancePermission(w, ctx, permissionReadAccounts) {
		return
	}

	q := r.URL.Query()
	limit := accountDirectoryDefaultPageSize
	offset := 0
	if raw := q.Get("limit"); raw != "" {
		v, err := strconv.Atoi(raw)
		if err != nil || v <= 0 || v > data.MaxAccountDirectoryPageSize {
			app.respondWithErrorCode(w, http.StatusBadRequest, "invalid_request", "limit must be between 1 and 100")
			return
		}
		limit = v
	}
	if raw := q.Get("offset"); raw != "" {
		v, err := strconv.Atoi(raw)
		if err != nil || v < 0 {
			app.respondWithErrorCode(w, http.StatusBadRequest, "invalid_request", "offset must be a non-negative integer")
			return
		}
		offset = v
	}
	query := q.Get("q")
	if utf8.RuneCountInString(query) > 254 {
		app.respondWithErrorCode(w, http.StatusBadRequest, "invalid_request", "search text is too long")
		return
	}

	accounts, err := app.InternalServices.SearchAccountsInternal(ctx, actorID, query, limit, offset)
	if err != nil {
		app.respondWithGovernanceError(w, r, "search_accounts", err)
		return
	}

	if err := app.insertGovernanceAudit(ctx, &actorID, "search_accounts", "Search the administrative account directory",
		data.GovernanceEntityUsers, "Tracks user-related actions.", "directory"); err != nil {
		app.Logger.GetLoggerWithContext(r).Warn("account directory read but audit failed", "error", err)
	}

	app.respondWithJSON(w, http.StatusOK, jsonResponse{
		Error:   false,
		Message: "Accounts retrieved successfully",
		Data:    accounts,
	})
}

// GetAccountHandler returns one account with available actions.
//
//	GET /api/v1/admin-governance/accounts/{userID}
func (app *Application) GetAccountHandler(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	actorID, ok := app.governanceActor(w, ctx)
	if !ok || !app.requireGovernancePermission(w, ctx, permissionReadAccounts) {
		return
	}
	targetID, ok := parseGovernanceTarget(r)
	if !ok {
		app.respondWithErrorCode(w, http.StatusBadRequest, "invalid_request", "invalid account ID")
		return
	}

	account, err := app.InternalServices.GetAccountInternal(ctx, actorID, targetID)
	if err != nil {
		app.respondWithGovernanceError(w, r, "read_account", err)
		return
	}

	if err := app.insertGovernanceAudit(ctx, &actorID, "read_account", "Read one account in the administrative directory",
		data.GovernanceEntityUsers, "Tracks user-related actions.", targetID.String()); err != nil {
		app.Logger.GetLoggerWithContext(r).Warn("account read but audit failed", "error", err)
	}

	app.respondWithJSON(w, http.StatusOK, jsonResponse{
		Error:   false,
		Message: "Account retrieved successfully",
		Data:    account,
	})
}

type governanceChangeInput struct {
	Level           string `json:"level,omitempty"`
	Reason          string `json:"reason,omitempty"`
	CurrentPassword string `json:"current_password,omitempty"`
	ConfirmEmail    string `json:"confirm_email,omitempty"`
}

// governanceOperationRoute describes one mutation endpoint.
type governanceOperationRoute struct {
	name       string
	permission func(input governanceChangeInput) (string, services.GovernanceOperation, bool)
}

func (app *Application) handleGovernanceChange(w http.ResponseWriter, r *http.Request, route governanceOperationRoute) {
	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	actorID, ok := app.governanceActor(w, ctx)
	if !ok {
		return
	}
	targetID, ok := parseGovernanceTarget(r)
	if !ok {
		app.respondWithErrorCode(w, http.StatusBadRequest, "invalid_request", "invalid account ID")
		return
	}

	var input governanceChangeInput
	if r.ContentLength != 0 {
		if err := app.readJSON(w, r, &input); err != nil {
			app.respondWithErrorCode(w, http.StatusBadRequest, "invalid_request", "invalid request body")
			return
		}
	}

	permission, op, valid := route.permission(input)
	if !valid {
		app.respondWithErrorCode(w, http.StatusBadRequest, "invalid_request", "invalid administrative level")
		return
	}
	if !app.requireGovernancePermission(w, ctx, permission) {
		return
	}

	record, err := app.InternalServices.ExecuteGovernanceOperationInternal(ctx, services.GovernanceRequest{
		ActorUserID:        actorID,
		TargetUserID:       targetID,
		Operation:          op,
		Reason:             input.Reason,
		CurrentPassword:    input.CurrentPassword,
		ConfirmTargetEmail: input.ConfirmEmail,
	})
	// The password is not retained beyond the service call.
	input.CurrentPassword = ""
	if err != nil {
		app.respondWithGovernanceError(w, r, route.name, err)
		return
	}

	app.Logger.GetLoggerWithContext(r).Info("governance change applied",
		"operation", route.name, "actor_user_id", actorID, "target_user_id", targetID)

	app.respondWithJSON(w, http.StatusOK, jsonResponse{
		Error:   false,
		Message: "Administrative standing updated",
		Data:    record,
	})
}

func fixedGovernanceRoute(name, permission string, op services.GovernanceOperation) governanceOperationRoute {
	return governanceOperationRoute{
		name: name,
		permission: func(governanceChangeInput) (string, services.GovernanceOperation, bool) {
			return permission, op, true
		},
	}
}

// AppointAdministratorHandler appoints an Admin, or appoints/promotes a
// Super Admin (level "super_admin", which also requires current_password and
// confirm_email).
//
//	POST /api/v1/admin-governance/administrators/{userID}/appoint
func (app *Application) AppointAdministratorHandler(w http.ResponseWriter, r *http.Request) {
	app.handleGovernanceChange(w, r, governanceOperationRoute{
		name: "appoint",
		permission: func(input governanceChangeInput) (string, services.GovernanceOperation, bool) {
			switch data.AdministrativeLevel(strings.TrimSpace(input.Level)) {
			case data.AdministrativeLevelAdmin:
				return permissionAppointAdmin, services.GovernanceAppointAdmin, true
			case data.AdministrativeLevelSuperAdmin:
				return permissionAppointSuperAdmin, services.GovernanceAppointSuperAdmin, true
			default:
				return "", "", false
			}
		},
	})
}

// DemoteAdministratorHandler demotes a Super Admin to Admin (Root only).
//
//	POST /api/v1/admin-governance/administrators/{userID}/demote
func (app *Application) DemoteAdministratorHandler(w http.ResponseWriter, r *http.Request) {
	app.handleGovernanceChange(w, r, fixedGovernanceRoute("demote", permissionDemoteAdministrator, services.GovernanceDemoteToAdmin))
}

// SuspendAdministratorHandler suspends an administrator.
//
//	POST /api/v1/admin-governance/administrators/{userID}/suspend
func (app *Application) SuspendAdministratorHandler(w http.ResponseWriter, r *http.Request) {
	app.handleGovernanceChange(w, r, fixedGovernanceRoute("suspend", permissionSuspendAdministrator, services.GovernanceSuspend))
}

// RestoreAdministratorHandler restores a suspended administrator.
//
//	POST /api/v1/admin-governance/administrators/{userID}/restore
func (app *Application) RestoreAdministratorHandler(w http.ResponseWriter, r *http.Request) {
	app.handleGovernanceChange(w, r, fixedGovernanceRoute("restore", permissionRestoreAdministrator, services.GovernanceRestore))
}

// RevokeAdministratorHandler revokes administrative standing.
//
//	POST /api/v1/admin-governance/administrators/{userID}/revoke
func (app *Application) RevokeAdministratorHandler(w http.ResponseWriter, r *http.Request) {
	app.handleGovernanceChange(w, r, fixedGovernanceRoute("revoke", permissionRevokeAdministrator, services.GovernanceRevoke))
}

// errIsGovernance reports whether err is a governance/account-lifecycle
// outcome that governanceErrorResponse maps to a non-500 status.
func errIsGovernance(err error) bool {
	status, _, _ := governanceErrorResponse(err)
	return status < http.StatusInternalServerError && !errors.Is(err, context.DeadlineExceeded)
}
