// Package main provides HTTP handlers for the SagrentiDeals API.
//
// sdworkspace/sdbackend/internal/server/cmd/api/user_dashboards.go
//
// GTM:
//
//	Layer: 2.3 Consumer Domain
//	Release Class: SPINE
//	Reason:
//	  The authenticated user dashboard is the canonical return surface for
//	  consumer and merchant intent. It gives users a central location to
//	  revisit watched Future Offerings and other dashboard-supported activity
//	  required by the Future Offering Platform.
//
//	  The domain is intentionally bounded to one persisted dashboard per user.
//	  Dashboard reports, reusable templates, and template assignments are
//	  separate deferred persistence objects and do not belong in this file.
//
// SPINE Rule:
//
//	Keep compiling.
//	Keep production-ready.
//	Preserve one canonical dashboard per user.
//	Preserve customer and merchant dashboard ownership semantics.
//	Preserve owner-scoped dashboard reads and mutations.
//	Preserve internal/admin governed access to dashboards owned by other users.
//	Preserve persisted name, description, layout, widgets, and filters.
//	Preserve database-owned created_at and updated_at timestamps.
//	Do not application-write persisted lifecycle timestamps.
//	Do not trust a request body to determine customer or merchant actor class.
//	Do not add dashboard reports, dashboard templates, template assignments,
//	or synthetic dashboard-activity workflows to this file.
//	Do not expose one user's dashboard to another user.
//	Block deployment if this file breaks dashboard creation, retrieval,
//	mutation, ownership enforcement, administrative statistics, or the build.
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

const (
	userDashboardEntityTypeName        = "user_dashboard"
	userDashboardEntityTypeDescription = "User-owned dashboard configuration"

	createUserDashboardAction     = "create_user_dashboard"
	readUserDashboardAction       = "read_user_dashboard"
	updateUserDashboardAction     = "update_user_dashboard"
	deleteUserDashboardAction     = "delete_user_dashboard"
	viewDashboardStatsAction      = "view_admin_dashboard_stats"
	viewAllUserDashboardsPermission   = "view_all_dashboards"
	updateAllUserDashboardsPermission = "update_all_user_dashboards"
	deleteAllUserDashboardsPermission = "delete_all_user_dashboards"
)

type createUserDashboardInput struct {
	Name        string         `json:"name"`
	Description *string        `json:"description,omitempty"`
	Layout      map[string]any `json:"layout,omitempty"`
	Widgets     []any          `json:"widgets,omitempty"`
	Filters     map[string]any `json:"filters,omitempty"`
}

type updateUserDashboardInput struct {
	Name        string         `json:"name,omitempty"`
	Description *string        `json:"description,omitempty"`
	Layout      map[string]any `json:"layout,omitempty"`
	Widgets     []any          `json:"widgets,omitempty"`
	Filters     map[string]any `json:"filters,omitempty"`
}

// CreateUserDashboardHandler creates the single canonical dashboard owned by
// the authenticated user.
//
// Actor class is derived from trusted authorization context. A request body
// cannot promote a customer dashboard into a merchant dashboard.
func (app *Application) CreateUserDashboardHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	logger := app.Logger.
		GetLoggerWithContext(r).
		WithFunctionName("CreateUserDashboardHandler")

	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	if !app.HasPermission(ctx, createUserDashboardAction) {
		app.respondWithError(
			w,
			errors.New("forbidden: insufficient permissions"),
			http.StatusForbidden,
		)
		return
	}

	userID := app.getUserIDFromContext(ctx)
	if userID == nil || *userID == uuid.Nil {
		app.respondWithError(
			w,
			errors.New("user ID not found in context"),
			http.StatusUnauthorized,
		)
		return
	}

	existing, err := app.Models.UserDashboard.GetByUserID(ctx, *userID)
	if err != nil {
		logger.Error(
			"Check existing user dashboard failed",
			"user_id", *userID,
			"error", err,
		)
		app.respondWithError(
			w,
			fmt.Errorf("failed to check existing user dashboard: %w", err),
			http.StatusInternalServerError,
		)
		return
	}

	if existing != nil {
		app.respondWithError(
			w,
			errors.New("user dashboard already exists"),
			http.StatusConflict,
		)
		return
	}

	var input createUserDashboardInput
	if err := app.readJSON(w, r, &input); err != nil {
		app.respondWithError(
			w,
			fmt.Errorf("invalid JSON input: %w", err),
			http.StatusBadRequest,
		)
		return
	}

	input.Name = strings.TrimSpace(input.Name)
	if input.Name == "" {
		app.respondWithError(
			w,
			errors.New("name is required"),
			http.StatusBadRequest,
		)
		return
	}

	userType, err := app.resolveUserDashboardOwnerType(ctx)
	if err != nil {
		logger.Error(
			"Resolve dashboard owner type failed",
			"user_id", *userID,
			"error", err,
		)
		app.respondWithError(
			w,
			fmt.Errorf("failed to resolve dashboard owner type: %w", err),
			http.StatusInternalServerError,
		)
		return
	}

	dashboard := &data.UserDashboard{
		UserID:      *userID,
		UserType:    userType,
		Name:        input.Name,
		Description: input.Description,
		Layout:      input.Layout,
		Widgets:     input.Widgets,
		Filters:     input.Filters,
	}

	if err := app.Models.UserDashboard.Insert(ctx, dashboard); err != nil {
		logger.Error(
			"Create user dashboard failed",
			"user_id", *userID,
			"user_type", userType,
			"error", err,
		)
		app.respondWithError(
			w,
			fmt.Errorf("failed to create user dashboard: %w", err),
			http.StatusInternalServerError,
		)
		return
	}

	app.insertUserDashboardAudit(
		ctx,
		userID,
		createUserDashboardAction,
		"Create a user dashboard",
		dashboard.ID.String(),
	)

	logger.Info(
		"User dashboard created",
		"dashboard_id", dashboard.ID,
		"user_id", dashboard.UserID,
		"user_type", dashboard.UserType,
	)

	app.respondWithJSON(w, http.StatusCreated, jsonResponse{
		Error:   false,
		Message: "User dashboard created successfully",
		Data:    dashboard,
	})
}

// GetUserDashboardByUserIDHandler retrieves the canonical dashboard owned by
// the authenticated user.
//
// The user identifier comes exclusively from trusted authentication context.
// No arbitrary user identifier is accepted from the request.
func (app *Application) GetUserDashboardByUserIDHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	logger := app.Logger.
		GetLoggerWithContext(r).
		WithFunctionName("GetUserDashboardByUserIDHandler")

	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	if !app.HasPermission(ctx, readUserDashboardAction) {
		app.respondWithError(
			w,
			errors.New("forbidden: insufficient permissions"),
			http.StatusForbidden,
		)
		return
	}

	userID := app.getUserIDFromContext(ctx)
	if userID == nil || *userID == uuid.Nil {
		app.respondWithError(
			w,
			errors.New("user ID not found in context"),
			http.StatusUnauthorized,
		)
		return
	}

	dashboard, err := app.Models.UserDashboard.GetByUserID(ctx, *userID)
	if err != nil {
		logger.Error(
			"Retrieve user dashboard by owner failed",
			"user_id", *userID,
			"error", err,
		)
		app.respondWithError(
			w,
			fmt.Errorf("failed to retrieve user dashboard: %w", err),
			http.StatusInternalServerError,
		)
		return
	}

	if dashboard == nil {
		app.respondWithError(
			w,
			errors.New("user dashboard not found"),
			http.StatusNotFound,
		)
		return
	}

	app.insertUserDashboardAudit(
		ctx,
		userID,
		readUserDashboardAction,
		"Read the authenticated user's dashboard",
		dashboard.ID.String(),
	)

	logger.Info(
		"User dashboard retrieved by owner",
		"dashboard_id", dashboard.ID,
		"user_id", dashboard.UserID,
	)

	app.respondWithJSON(w, http.StatusOK, jsonResponse{
		Error:   false,
		Message: "User dashboard retrieved successfully",
		Data:    dashboard,
	})
}

// GetUserDashboardByIDHandler retrieves a dashboard using the trusted dashboard
// ID installed in request context by route middleware.
//
// The authenticated owner may retrieve the dashboard. A different actor must
// hold explicit permission to view dashboards owned by other users.
func (app *Application) GetUserDashboardByIDHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	logger := app.Logger.
		GetLoggerWithContext(r).
		WithFunctionName("GetUserDashboardByIDHandler")

	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	if !app.HasPermission(ctx, readUserDashboardAction) {
		app.respondWithError(
			w,
			errors.New("forbidden: insufficient permissions"),
			http.StatusForbidden,
		)
		return
	}

	userID := app.getUserIDFromContext(ctx)
	if userID == nil || *userID == uuid.Nil {
		app.respondWithError(
			w,
			errors.New("user ID not found in context"),
			http.StatusUnauthorized,
		)
		return
	}

	dashboardID := app.getDashboardIDFromContext(ctx)
	if dashboardID == nil || *dashboardID == uuid.Nil {
		app.respondWithError(
			w,
			errors.New("dashboard ID not found in context"),
			http.StatusBadRequest,
		)
		return
	}

	dashboard, err := app.Models.UserDashboard.GetByID(ctx, *dashboardID)
	if err != nil {
		logger.Error(
			"Retrieve user dashboard by ID failed",
			"dashboard_id", *dashboardID,
			"error", err,
		)
		app.respondWithError(
			w,
			fmt.Errorf("failed to retrieve user dashboard: %w", err),
			http.StatusInternalServerError,
		)
		return
	}

	if dashboard == nil {
		app.respondWithError(
			w,
			errors.New("user dashboard not found"),
			http.StatusNotFound,
		)
		return
	}

	if dashboard.UserID != *userID &&
		!app.HasPermission(ctx, viewAllUserDashboardsPermission) {
		app.respondWithError(
			w,
			errors.New("forbidden: dashboard ownership required"),
			http.StatusForbidden,
		)
		return
	}

	app.insertUserDashboardAudit(
		ctx,
		userID,
		readUserDashboardAction,
		"Read a user dashboard by ID",
		dashboard.ID.String(),
	)

	logger.Info(
		"User dashboard retrieved by ID",
		"dashboard_id", dashboard.ID,
		"dashboard_owner_id", dashboard.UserID,
		"actor_user_id", *userID,
	)

	app.respondWithJSON(w, http.StatusOK, jsonResponse{
		Error:   false,
		Message: "User dashboard retrieved successfully",
		Data:    dashboard,
	})
}

// UpdateUserDashboardHandler updates mutable fields on a user dashboard.
//
// The authenticated owner may update the dashboard. A different actor must hold
// explicit permission to update dashboards owned by other users. Persisted
// updated_at remains database-owned through UserDashboardModel.Update.
func (app *Application) UpdateUserDashboardHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	logger := app.Logger.
		GetLoggerWithContext(r).
		WithFunctionName("UpdateUserDashboardHandler")

	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	if !app.HasPermission(ctx, updateUserDashboardAction) {
		app.respondWithError(
			w,
			errors.New("forbidden: insufficient permissions"),
			http.StatusForbidden,
		)
		return
	}

	userID := app.getUserIDFromContext(ctx)
	if userID == nil || *userID == uuid.Nil {
		app.respondWithError(
			w,
			errors.New("user ID not found in context"),
			http.StatusUnauthorized,
		)
		return
	}

	dashboardID := app.getDashboardIDFromContext(ctx)
	if dashboardID == nil || *dashboardID == uuid.Nil {
		app.respondWithError(
			w,
			errors.New("dashboard ID not found in context"),
			http.StatusBadRequest,
		)
		return
	}

	existing, err := app.Models.UserDashboard.GetByID(ctx, *dashboardID)
	if err != nil {
		logger.Error(
			"Retrieve dashboard before update failed",
			"dashboard_id", *dashboardID,
			"error", err,
		)
		app.respondWithError(
			w,
			fmt.Errorf("failed to retrieve user dashboard: %w", err),
			http.StatusInternalServerError,
		)
		return
	}

	if existing == nil {
		app.respondWithError(
			w,
			errors.New("user dashboard not found"),
			http.StatusNotFound,
		)
		return
	}

	if existing.UserID != *userID &&
		!app.HasPermission(ctx, updateAllUserDashboardsPermission) {
		app.respondWithError(
			w,
			errors.New("forbidden: dashboard ownership required"),
			http.StatusForbidden,
		)
		return
	}

	var input updateUserDashboardInput
	if err := app.readJSON(w, r, &input); err != nil {
		app.respondWithError(
			w,
			fmt.Errorf("invalid JSON input: %w", err),
			http.StatusBadRequest,
		)
		return
	}

	input.Name = strings.TrimSpace(input.Name)

	if input.Name == "" &&
		input.Description == nil &&
		input.Layout == nil &&
		input.Widgets == nil &&
		input.Filters == nil {
		app.respondWithError(
			w,
			errors.New(
				"at least one of name, description, layout, widgets, or filters must be provided",
			),
			http.StatusBadRequest,
		)
		return
	}

	dashboard := &data.UserDashboard{
		ID:          existing.ID,
		UserID:      existing.UserID,
		UserType:    existing.UserType,
		Name:        input.Name,
		Description: input.Description,
		Layout:      input.Layout,
		Widgets:     input.Widgets,
		Filters:     input.Filters,
	}

	if err := app.Models.UserDashboard.Update(ctx, dashboard); err != nil {
		logger.Error(
			"Update user dashboard failed",
			"dashboard_id", dashboard.ID,
			"actor_user_id", *userID,
			"error", err,
		)
		app.respondWithError(
			w,
			fmt.Errorf("failed to update user dashboard: %w", err),
			http.StatusInternalServerError,
		)
		return
	}

	persisted, err := app.Models.UserDashboard.GetByID(ctx, dashboard.ID)
	if err != nil {
		logger.Error(
			"Reload updated user dashboard failed",
			"dashboard_id", dashboard.ID,
			"error", err,
		)
		app.respondWithError(
			w,
			fmt.Errorf(
				"user dashboard was updated but could not be reloaded: %w",
				err,
			),
			http.StatusInternalServerError,
		)
		return
	}

	if persisted == nil {
		app.respondWithError(
			w,
			errors.New(
				"user dashboard was updated but could not be found",
			),
			http.StatusInternalServerError,
		)
		return
	}

	app.insertUserDashboardAudit(
		ctx,
		userID,
		updateUserDashboardAction,
		"Update a user dashboard",
		persisted.ID.String(),
	)

	logger.Info(
		"User dashboard updated",
		"dashboard_id", persisted.ID,
		"dashboard_owner_id", persisted.UserID,
		"actor_user_id", *userID,
	)

	app.respondWithJSON(w, http.StatusOK, jsonResponse{
		Error:   false,
		Message: "User dashboard updated successfully",
		Data:    persisted,
	})
}

// DeleteUserDashboardHandler permanently removes a user-owned dashboard
// configuration.
//
// The dashboard contains reconstructible user configuration rather than the
// retained canonical user actor. The authenticated owner may remove it. A
// different actor must hold explicit permission to delete dashboards owned by
// other users.
func (app *Application) DeleteUserDashboardHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	logger := app.Logger.
		GetLoggerWithContext(r).
		WithFunctionName("DeleteUserDashboardHandler")

	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	if !app.HasPermission(ctx, deleteUserDashboardAction) {
		app.respondWithError(
			w,
			errors.New("forbidden: insufficient permissions"),
			http.StatusForbidden,
		)
		return
	}

	userID := app.getUserIDFromContext(ctx)
	if userID == nil || *userID == uuid.Nil {
		app.respondWithError(
			w,
			errors.New("user ID not found in context"),
			http.StatusUnauthorized,
		)
		return
	}

	dashboardID := app.getDashboardIDFromContext(ctx)
	if dashboardID == nil || *dashboardID == uuid.Nil {
		app.respondWithError(
			w,
			errors.New("dashboard ID not found in context"),
			http.StatusBadRequest,
		)
		return
	}

	dashboard, err := app.Models.UserDashboard.GetByID(ctx, *dashboardID)
	if err != nil {
		logger.Error(
			"Retrieve dashboard before delete failed",
			"dashboard_id", *dashboardID,
			"error", err,
		)
		app.respondWithError(
			w,
			fmt.Errorf("failed to retrieve user dashboard: %w", err),
			http.StatusInternalServerError,
		)
		return
	}

	if dashboard == nil {
		app.respondWithError(
			w,
			errors.New("user dashboard not found"),
			http.StatusNotFound,
		)
		return
	}

	if dashboard.UserID != *userID &&
		!app.HasPermission(ctx, deleteAllUserDashboardsPermission) {
		app.respondWithError(
			w,
			errors.New("forbidden: dashboard ownership required"),
			http.StatusForbidden,
		)
		return
	}

	if err := app.Models.UserDashboard.Delete(ctx, dashboard.ID); err != nil {
		logger.Error(
			"Delete user dashboard failed",
			"dashboard_id", dashboard.ID,
			"actor_user_id", *userID,
			"error", err,
		)
		app.respondWithError(
			w,
			fmt.Errorf("failed to delete user dashboard: %w", err),
			http.StatusInternalServerError,
		)
		return
	}

	app.insertUserDashboardAudit(
		ctx,
		userID,
		deleteUserDashboardAction,
		"Delete a user dashboard",
		dashboard.ID.String(),
	)

	logger.Info(
		"User dashboard deleted",
		"dashboard_id", dashboard.ID,
		"dashboard_owner_id", dashboard.UserID,
		"actor_user_id", *userID,
	)

	app.respondWithJSON(w, http.StatusOK, jsonResponse{
		Error:   false,
		Message: "User dashboard deleted successfully",
		Data: struct {
			DashboardID uuid.UUID `json:"dashboard_id"`
		}{
			DashboardID: dashboard.ID,
		},
	})
}

// AdminDashboardStatsHandler returns aggregate statistics directly from the
// canonical user_dashboards table.
//
// This is an internal administrative read. It does not generate or persist a
// dashboard report.
func (app *Application) AdminDashboardStatsHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	logger := app.Logger.
		GetLoggerWithContext(r).
		WithFunctionName("AdminDashboardStatsHandler")

	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	if !app.HasPermission(ctx, viewDashboardStatsAction) {
		app.respondWithError(
			w,
			errors.New("forbidden: insufficient permissions"),
			http.StatusForbidden,
		)
		return
	}

	adminID := app.getAdminIDFromContext(ctx)
	if adminID == nil || *adminID == uuid.Nil {
		app.respondWithError(
			w,
			errors.New("admin ID not found in context"),
			http.StatusUnauthorized,
		)
		return
	}

	total, latestUpdatedAt, err := app.Models.UserDashboard.GetAdminStats(ctx)
	if err != nil {
		logger.Error(
			"Retrieve user dashboard statistics failed",
			"admin_id", *adminID,
			"error", err,
		)
		app.respondWithError(
			w,
			fmt.Errorf("failed to retrieve user dashboard statistics: %w", err),
			http.StatusInternalServerError,
		)
		return
	}

	stats := struct {
		TotalDashboards  int64 `json:"total_dashboards"`
		LatestUpdatedAt any   `json:"latest_updated_at"`
	}{
		TotalDashboards:  total,
		LatestUpdatedAt: latestUpdatedAt.UTC(),
	}

	app.insertUserDashboardAudit(
		ctx,
		adminID,
		viewDashboardStatsAction,
		"View aggregate user dashboard statistics",
		userDashboardEntityTypeName,
	)

	logger.Info(
		"User dashboard statistics retrieved",
		"admin_id", *adminID,
		"total_dashboards", total,
		"latest_updated_at", latestUpdatedAt,
	)

	app.respondWithJSON(w, http.StatusOK, jsonResponse{
		Error:   false,
		Message: "User dashboard statistics retrieved successfully",
		Data:    stats,
	})
}

// resolveUserDashboardOwnerType derives the canonical persisted dashboard owner
// type from trusted actor-role context.
//
// Merchant actors receive merchant dashboards. Other authenticated users receive
// customer dashboards. Internal visibility never creates an admin-owned
// dashboard type.
func (app *Application) resolveUserDashboardOwnerType(
	ctx context.Context,
) (string, error) {
	isMerchant, err := app.HasRole(ctx, "merchant")
	if err != nil {
		return "", err
	}

	if isMerchant {
		return "merchant", nil
	}

	return "customer", nil
}

// insertUserDashboardAudit performs best-effort audit insertion.
//
// A completed dashboard operation is not converted into an HTTP partial-content
// response merely because audit metadata or audit persistence is unavailable.
func (app *Application) insertUserDashboardAudit(
	ctx context.Context,
	userID *uuid.UUID,
	actionName string,
	actionDescription string,
	entityID string,
) {
	if userID == nil || *userID == uuid.Nil {
		return
	}

	action, err := app.Models.Action.GetByName(ctx, actionName)
	if err != nil || action == nil {
		actionID, createErr := app.Models.Action.CreateIfNotExists(
			ctx,
			actionName,
			actionDescription,
		)
		if createErr != nil {
			return
		}

		action = &data.Action{
			ID: actionID,
		}
	}

	entityType, err := app.Models.EntityType.GetByName(
		ctx,
		userDashboardEntityTypeName,
	)
	if err != nil || entityType == nil {
		entityTypeID, createErr := app.Models.EntityType.CreateIfNotExists(
			ctx,
			userDashboardEntityTypeName,
			userDashboardEntityTypeDescription,
		)
		if createErr != nil {
			return
		}

		entityType = &data.EntityType{
			ID: entityTypeID,
		}
	}

	if action == nil || action.ID == uuid.Nil {
		return
	}

	if entityType == nil || entityType.ID == uuid.Nil {
		return
	}

	auditLog := &data.AuditLog{
		ID:           uuid.New(),
		UserID:       userID,
		ActionID:     action.ID,
		EntityTypeID: entityType.ID,
		EntityID:     entityID,
	}

	_ = app.Models.AuditLog.Insert(ctx, auditLog)
}