// sdworkspace/sdbackend/internal/server/cmd/api/user_dashboards.go
//   Release Class: DEFERRED
package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/PiccoloMondoC/sdworkspace/sdbackend/internal/data"
	"github.com/PiccoloMondoC/sdworkspace/sdbackend/internal/utils/timeutil"

	"github.com/google/uuid"
)

// Principles:
// Users can only access their own dashboards or if the role from context is "super_admin" or similar.
// Admins must have a specific permission (e.g., view_all_dashboards) to access any dashboard. Only
// "super_admin" role has this specific permission.
// That permission can be checked via a HasPermission() helper tied to  role/permission system.

// CreateUserDashboardHandler handles the creation of a user dashboard.
// It enforces permission checks, extracts trusted identifiers from context,
// performs the creation operation, logs the action, and handles audit logging.
func (app *Application) CreateUserDashboardHandler(w http.ResponseWriter, r *http.Request) {
	logger := app.Logger.GetLoggerWithContext(r).WithFunctionName("CreateUserDashboardHandler")
	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	// Permission enforcement
	if !app.HasPermission(ctx, "create_user_dashboard") {
		app.respondWithError(w, errors.New("forbidden: insufficient permissions"), http.StatusForbidden)
		return
	}

	// Extract caller UUID (trusted, injected by middleware)
	userID := app.getUserIDFromContext(ctx)
	if userID == nil {
		app.respondWithError(w, errors.New("missing user ID in context"), http.StatusUnauthorized)
		return
	}

	// Parse & validate request body
	var input struct {
		Name        string         `json:"name"`                  // mandatory
		Description *string        `json:"description,omitempty"` // optional
		UserType    string         `json:"user_type,omitempty"`   // optional; fallback below
		Layout      map[string]any `json:"layout,omitempty"`
		Widgets     []any          `json:"widgets,omitempty"`
		Filters     map[string]any `json:"filters,omitempty"`
	}
	if err := app.readJSON(w, r, &input); err != nil {
		app.respondWithError(w, fmt.Errorf("invalid JSON input: %w", err), http.StatusBadRequest)
		return
	}
	input.Name = strings.TrimSpace(input.Name)
	if input.Name == "" {
		app.respondWithError(w, errors.New("name is required"), http.StatusBadRequest)
		return
	}

	// default user type (if omitted) keeps Insert() happy
	if strings.TrimSpace(input.UserType) == "" {
		input.UserType = "consumer"
	}

	// Build new UserDashboard
	dashboard := &data.UserDashboard{
		UserID:      *userID,
		UserType:    input.UserType,
		Name:        input.Name,
		Description: input.Description,
		Layout:      input.Layout,
		Widgets:     input.Widgets,
		Filters:     input.Filters,
	}

	// Insert UserDashboard into database
	if err := app.Models.UserDashboard.Insert(ctx, dashboard); err != nil {
		logger.Error("Insert user dashboard failed", "error", err)
		app.respondWithError(w, fmt.Errorf("failed to create user dashboard: %w", err), http.StatusInternalServerError)
		return
	}

	// Resolve Audit Action
	action, err := app.Models.Action.GetByName(ctx, "create_user_dashboard")
	if err != nil || action == nil {
		logger.Warn("Audit action 'create_user_dashboard' not found, attempting to create...", "error", err)
		actionID, createErr := app.Models.Action.CreateIfNotExists(ctx, "create_user_dashboard", "Create a new user dashboard")
		if createErr != nil {
			logger.Error("Failed to create missing audit action", "error", createErr)
			// Allow main operation to succeed
		} else {
			action = &data.Action{ID: actionID}
		}
	}

	// Resolve Audit Entity Type
	entityType, err := app.Models.EntityType.GetByName(ctx, "user_dashboard")
	if err != nil || entityType == nil {
		logger.Warn("Audit entity type 'user_dashboard' not found, attempting to create...", "error", err)
		entityTypeID, createErr := app.Models.EntityType.CreateIfNotExists(ctx, "user_dashboard", "User-specific dashboard")
		if createErr != nil {
			logger.Error("Failed to create missing entity type", "error", createErr)
			// Allow main operation to succeed
		} else {
			entityType = &data.EntityType{ID: entityTypeID}
		}
	}

	// Insert Audit Log (fail gracefully if fails)
	if userID != nil && action != nil && entityType != nil {
		audit := data.AuditLog{
			ID:           uuid.New(),
			UserID:       userID,
			ActionID:     &action.ID,
			EntityTypeID: entityType.ID,
			EntityID:     dashboard.ID.String(),
			Timestamp:    timeutil.Now(),
		}
		if err := app.Models.AuditLog.Insert(ctx, &audit); err != nil {
			// Audit logging failed (partial success)
			logger.Warn("Audit logging failed", "user_dashboard_id", dashboard.ID, "error", err)
			app.respondWithJSON(w, http.StatusPartialContent, jsonResponse{
				Error:   false,
				Message: "User dashboard created, but audit logging failed",
				Data:    dashboard.ID,
			})
			return
		}
	}

	// Respond success
	logger.Info("User dashboard created successfully", "user_dashboard_id", dashboard.ID)
	app.respondWithJSON(w, http.StatusCreated, jsonResponse{
		Error:   false,
		Message: "User dashboard created successfully",
		Data:    dashboard.ID,
	})
}


// GetUserDashboardByIDHandler handles retrieving a user dashboard by its ID.
// It enforces permission checks, extracts the dashboard ID from context,
// retrieves the dashboard from the database, performs audit logging,
// and responds with the dashboard details.
func (app *Application) GetUserDashboardByIDHandler(w http.ResponseWriter, r *http.Request) {
	logger := app.Logger.GetLoggerWithContext(r).WithFunctionName("GetUserDashboardByIDHandler")
	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	// Permission enforcement
	if !app.HasPermission(ctx, "read_user_dashboard") {
		app.respondWithError(w, errors.New("forbidden: insufficient permissions"), http.StatusForbidden)
		return
	}

	// Extract Dashboard ID from context
	dashboardID := app.getDashboardIDFromContext(ctx)
	if dashboardID == nil {
		app.respondWithError(w, errors.New("missing dashboard ID in context"), http.StatusBadRequest)
		return
	}

	// Retrieve the user dashboard
	dashboard, err := app.Models.UserDashboard.GetByID(ctx, *dashboardID)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			app.respondWithError(w, fmt.Errorf("user dashboard not found"), http.StatusNotFound)
		} else {
			logger.Error("Failed to retrieve user dashboard", "error", err)
			app.respondWithError(w, fmt.Errorf("failed to retrieve user dashboard: %w", err), http.StatusInternalServerError)
		}
		return
	}

	// Resolve Audit Action
	action, err := app.Models.Action.GetByName(ctx, "read_user_dashboard")
	if err != nil || action == nil {
		logger.Warn("Audit action 'read_user_dashboard' not found, attempting to create...", "error", err)
		actionID, createErr := app.Models.Action.CreateIfNotExists(ctx, "read_user_dashboard", "Retrieve a user dashboard by ID")
		if createErr != nil {
			logger.Error("Failed to create missing audit action", "error", createErr)
			// Allow main operation to proceed
		} else {
			action = &data.Action{ID: actionID}
		}
	}

	// Resolve Audit Entity Type
	entityType, err := app.Models.EntityType.GetByName(ctx, "user_dashboard")
	if err != nil || entityType == nil {
		logger.Warn("Audit entity type 'user_dashboard' not found, attempting to create...", "error", err)
		entityTypeID, createErr := app.Models.EntityType.CreateIfNotExists(ctx, "user_dashboard", "User dashboard entity")
		if createErr != nil {
			logger.Error("Failed to create missing entity type", "error", createErr)
			// Allow main operation to proceed
		} else {
			entityType = &data.EntityType{ID: entityTypeID}
		}
	}

	// Insert Audit Log (fail gracefully if fails)
	userID := app.getUserIDFromContext(ctx)
	if userID != nil && action != nil && entityType != nil {
		audit := data.AuditLog{
			ID:           uuid.New(),
			UserID:       userID,
			ActionID:     &action.ID,
			EntityTypeID: entityType.ID,
			EntityID:     dashboard.ID.String(),
			Timestamp:    timeutil.Now(),
		}
		if err := app.Models.AuditLog.Insert(ctx, &audit); err != nil {
			// Audit logging failed (partial success)
			logger.Warn("Audit logging failed", "dashboard_id", dashboard.ID, "error", err)
			app.respondWithJSON(w, http.StatusPartialContent, jsonResponse{
				Error:   false,
				Message: "User dashboard retrieved, but audit logging failed",
				Data:    dashboard,
			})
			return
		}
	}

	// Success
	logger.Info("Successfully retrieved user dashboard", "dashboard_id", dashboard.ID)
	app.respondWithJSON(w, http.StatusOK, jsonResponse{
		Error:   false,
		Message: "User dashboard retrieved successfully",
		Data:    dashboard,
	})
}


// GetUserDashboardByUserIDHandler retrieves the user dashboard by user ID.
// It ensures permission checks, extracts the user ID from context, queries the database,
// logs structured audit events, and returns the dashboard data.
func (app *Application) GetUserDashboardByUserIDHandler(w http.ResponseWriter, r *http.Request) {
	logger := app.Logger.GetLoggerWithContext(r).WithFunctionName("GetUserDashboardByUserIDHandler")
	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	// Permission enforcement
	if !app.HasPermission(ctx, "read_user_dashboard") {
		app.respondWithError(w, errors.New("forbidden: insufficient permissions"), http.StatusForbidden)
		return
	}

	// Extract User Dashboard ID from context
	dashboardID := app.getDashboardIDFromContext(ctx)
	if dashboardID == nil {
		app.respondWithError(w, errors.New("missing user dashboard ID in context"), http.StatusBadRequest)
		return
	}

	// Retrieve the user dashboard from the database
	dashboard, err := app.Models.UserDashboard.GetByID(ctx, *dashboardID)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			app.respondWithError(w, fmt.Errorf("user dashboard not found"), http.StatusNotFound)
		} else {
			logger.Error("Failed to retrieve user dashboard", "error", err)
			app.respondWithError(w, fmt.Errorf("failed to retrieve user dashboard: %w", err), http.StatusInternalServerError)
		}
		return
	}

	// Resolve Audit Action
	action, err := app.Models.Action.GetByName(ctx, "read_user_dashboard")
	if err != nil || action == nil {
		logger.Warn("Audit action 'read_user_dashboard' not found, attempting to create...", "error", err)
		actionID, createErr := app.Models.Action.CreateIfNotExists(ctx, "read_user_dashboard", "Retrieve user dashboard by ID")
		if createErr != nil {
			logger.Error("Failed to create missing audit action", "error", createErr)
			// Allow main operation to succeed
		} else {
			action = &data.Action{ID: actionID}
		}
	}

	// Resolve Audit Entity Type
	entityType, err := app.Models.EntityType.GetByName(ctx, "user_dashboard")
	if err != nil || entityType == nil {
		logger.Warn("Audit entity type 'user_dashboard' not found, attempting to create...", "error", err)
		entityTypeID, createErr := app.Models.EntityType.CreateIfNotExists(ctx, "user_dashboard", "User dashboard entity")
		if createErr != nil {
			logger.Error("Failed to create missing entity type", "error", createErr)
			// Allow main operation to succeed
		} else {
			entityType = &data.EntityType{ID: entityTypeID}
		}
	}

	// Insert Audit Log (fail gracefully if fails)
	userID := app.getUserIDFromContext(ctx)
	if userID != nil && action != nil && entityType != nil {
		audit := data.AuditLog{
			ID:           uuid.New(),
			UserID:       userID,
			ActionID:     &action.ID,
			EntityTypeID: entityType.ID,
			EntityID:     dashboard.ID.String(),
			Timestamp:    timeutil.Now(),
		}
		if err := app.Models.AuditLog.Insert(ctx, &audit); err != nil {
			// Audit logging failed (partial success)
			logger.Warn("Audit logging failed", "dashboard_id", dashboard.ID, "error", err)
			app.respondWithJSON(w, http.StatusPartialContent, jsonResponse{
				Error:   false,
				Message: "User dashboard retrieved, but audit logging failed",
				Data:    dashboard,
			})
			return
		}
	}

	// Success
	logger.Info("Successfully retrieved user dashboard", "dashboard_id", dashboard.ID)
	app.respondWithJSON(w, http.StatusOK, jsonResponse{
		Error:   false,
		Message: "User dashboard retrieved successfully",
		Data:    dashboard,
	})
}


// UpdateUserDashboardHandler handles the update of a user's dashboard settings.
// It enforces permission checks, extracts trusted identifiers from context,
// performs the update operation, logs the operation, and handles audit logging.
func (app *Application) UpdateUserDashboardHandler(w http.ResponseWriter, r *http.Request) {
	logger := app.Logger.GetLoggerWithContext(r).WithFunctionName("UpdateUserDashboardHandler")
	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	// Explicit internal role enforcement
	isAdmin, err := app.HasRole(ctx, "admin") // returns (bool, error)
	if err != nil {
		app.respondWithError(w, err, http.StatusInternalServerError)
		return
	}
	isOperator, err := app.HasRole(ctx, "internal_operator")
	if err != nil {
		app.respondWithError(w, err, http.StatusInternalServerError)
		return
	}
	if !(isAdmin || isOperator) {
		app.respondWithError(w, errors.New("forbidden: dashboard updates are internal only"), http.StatusForbidden)
		return
	}

	// Permission enforcement
	if !app.HasPermission(ctx, "update_user_dashboard") {
		app.respondWithError(w, errors.New("forbidden: insufficient permissions"), http.StatusForbidden)
		return
	}

	// Extract trusted identifiers from context
	userID := app.getUserIDFromContext(ctx)         // uuid.UUID (non‑nil if middleware behaved)
	dashboardID := app.getDashboardIDFromContext(ctx)
	if userID == nil || dashboardID == nil {
		app.respondWithError(w, errors.New("missing user or dashboard ID in context"), http.StatusBadRequest)
		return
	}

	// Parse request body
	var input struct {
		Layout  map[string]any `json:"layout,omitempty"`
		Widgets []any          `json:"widgets,omitempty"`
		Filters map[string]any `json:"filters,omitempty"`
	}

	if err := app.readJSON(w, r, &input); err != nil {
		app.respondWithError(w, err, http.StatusBadRequest)
		return
	}
	if input.Layout == nil && input.Widgets == nil && input.Filters == nil {
		app.respondWithError(w, errors.New("at least one of layout, widgets or filters must be provided"), http.StatusBadRequest)
		return
	}

	// Build Dashboard update object & persist
	dashboard := &data.UserDashboard{
		ID:       *dashboardID,
		UserID:   *userID,
		Layout:   input.Layout,
		Widgets:  input.Widgets,
		Filters:  input.Filters,
		UpdatedAt: timeutil.Now(), // model still updates this, but set here for audit log consistency
	}

	// Perform update operation
	if err := app.Models.UserDashboard.Update(ctx, dashboard); err != nil {
		logger.Error("failed to update user dashboard", "error", err)
		app.respondWithError(w, err, http.StatusInternalServerError)
		return
	}

	// Resolve Audit Action
	action, err := app.Models.Action.GetByName(ctx, "update_user_dashboard")
	if err != nil || action == nil {
		logger.Warn("Audit action 'update_user_dashboard' not found, attempting to create...", "error", err)
		actionID, createErr := app.Models.Action.CreateIfNotExists(ctx, "update_user_dashboard", "Update user dashboard settings")
		if createErr != nil {
			logger.Error("Failed to create missing audit action", "error", createErr)
			// Allow main operation to succeed
		} else {
			action = &data.Action{ID: actionID}
		}
	}

	// Resolve Audit Entity Type
	entityType, err := app.Models.EntityType.GetByName(ctx, "user_dashboard")
	if err != nil || entityType == nil {
		logger.Warn("Audit entity type 'user_dashboard' not found, attempting to create...", "error", err)
		entityTypeID, createErr := app.Models.EntityType.CreateIfNotExists(ctx, "user_dashboard", "User dashboard settings")
		if createErr != nil {
			logger.Error("Failed to create missing entity type", "error", createErr)
			// Allow main operation to succeed
		} else {
			entityType = &data.EntityType{ID: entityTypeID}
		}
	}

	// Insert Audit Log (fail gracefully if needed)
	if userID != nil && action != nil && entityType != nil {
		audit := data.AuditLog{
			ID:           uuid.New(),
			UserID:       userID,
			ActionID:     &action.ID,
			EntityTypeID: entityType.ID,
			EntityID:     dashboard.ID.String(),
			Timestamp:    timeutil.Now(),
		}
		if err := app.Models.AuditLog.Insert(ctx, &audit); err != nil {
			// Audit logging failed (partial success)
			logger.Warn("Audit logging failed", "dashboard_id", dashboard.ID, "error", err)
			app.respondWithJSON(w, http.StatusPartialContent, jsonResponse{
				Error:   false,
				Message: "Dashboard updated, but audit logging failed",
				Data:    dashboard.ID,
			})
			return
		}
	}

	// Respond success
	logger.Info("User dashboard updated successfully", "dashboard_id", dashboard.ID)
	app.respondWithJSON(w, http.StatusOK, jsonResponse{
		Error:   false,
		Message: "User dashboard updated successfully",
		Data:    dashboard.ID,
	})
}

// Admin Specific User Dashboards


// AdminDashboardStatsHandler returns aggregate statistics for all admin dashboards.
//
// Behaviour
// ---------
//   • Requires the caller to have the "view_admin_dashboard_stats" permission.  
//   • Retrieves the total number of admin dashboards and the timestamp of the most‑recent change.  
//   • Writes an audit‑log entry (best‑effort; failure doesn’t block the main response).  
//   • Responds with HTTP 200 and a JSON body on success.
//
// Security
// --------
//   • Caller identity (uuid.UUID) is injected upstream and fetched type‑safely from context.  
//   • No untrusted inputs are interpolated into SQL.
//
// Route:  GET /api/v1/admin/dashboard/stats
func (app *Application) AdminDashboardStatsHandler(w http.ResponseWriter, r *http.Request) {
	logger := app.Logger.GetLoggerWithContext(r).WithFunctionName("AdminDashboardStatsHandler")
	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	// ─────────────────────────────────────────────────────────────────────────────
	// Permission enforcement
	// ─────────────────────────────────────────────────────────────────────────────
	if !app.HasPermission(ctx, "view_admin_dashboard_stats") {
		app.respondWithError(w, errors.New("forbidden: insufficient permissions"), http.StatusForbidden)
		return
	}

	// ─────────────────────────────────────────────────────────────────────────────
	// Extract caller (admin) ID from trusted context
	// ─────────────────────────────────────────────────────────────────────────────
	adminID := app.getAdminIDFromContext(ctx) // returns *uuid.UUID (may be nil if middleware mis‑configured)
	if adminID == nil || *adminID == uuid.Nil {
		app.respondWithError(w, errors.New("missing admin ID in context"), http.StatusUnauthorized)
		return
	}

	// ─────────────────────────────────────────────────────────────────────────────
	// Business operation: fetch aggregate stats
	// ─────────────────────────────────────────────────────────────────────────────
	total, latestUpdatedAt, err := app.Models.UserDashboard.GetAdminStats(ctx)
	if err != nil {
		logger.Error("Failed to retrieve admin dashboard stats",
			"admin_id", *adminID, "error", err)
		app.respondWithError(w, fmt.Errorf("failed to retrieve dashboard stats: %w", err),
			http.StatusInternalServerError)
		return
	}

	// Prepare response payload
	stats := map[string]any{
		"total_dashboards":   total,
		"latest_updated_at":  latestUpdatedAt.UTC(), // ISO‑8601 in JSON
	}

	// Resolve Audit Action
	action, err := app.Models.Action.GetByName(ctx, "view_admin_dashboard_stats")
	if err != nil || action == nil {
		logger.Warn("Audit action 'view_admin_dashboard_stats' not found, attempting to create...", "error", err)
		actionID, createErr := app.Models.Action.CreateIfNotExists(ctx, "view_admin_dashboard_stats", "View admin dashboard statistics")
		if createErr != nil {
			logger.Error("Failed to create missing audit action", "error", createErr)
			// Allow main operation to succeed
		} else {
			action = &data.Action{ID: actionID}
		}
	}

	// Resolve Audit Entity Type
	entityType, err := app.Models.EntityType.GetByName(ctx, "admin_dashboard")
	if err != nil || entityType == nil {
		logger.Warn("Audit entity type 'admin_dashboard' not found, attempting to create...", "error", err)
		entityTypeID, createErr := app.Models.EntityType.CreateIfNotExists(ctx, "admin_dashboard", "Admin dashboard statistics")
		if createErr != nil {
			logger.Error("Failed to create missing entity type", "error", createErr)
			// Allow main operation to succeed
		} else {
			entityType = &data.EntityType{ID: entityTypeID}
		}
	}

	// Insert Audit Log (fail gracefully if fails)
	if adminID != nil && action != nil && entityType != nil {
		audit := data.AuditLog{
			ID:           uuid.New(),
			UserID:       adminID,
			ActionID:     &action.ID,
			EntityTypeID: entityType.ID,
			EntityID:     adminID.String(),
			Timestamp:    timeutil.Now(),
		}
		if err := app.Models.AuditLog.Insert(ctx, &audit); err != nil {
			// Audit logging failed (partial success)
			logger.Warn("Audit logging failed", "admin_id", adminID, "error", err)
			app.respondWithJSON(w, http.StatusPartialContent, jsonResponse{
				Error:   false,
				Message: "Dashboard stats retrieved, but audit logging failed",
				Data:    stats,
			})
			return
		}
	}

	// Respond success
	logger.Info("Admin dashboard stats retrieved successfully", "admin_id", adminID)
	app.respondWithJSON(w, http.StatusOK, jsonResponse{
		Error:   false,
		Message: "Admin dashboard stats retrieved successfully",
		Data:    stats,
	})
}


// GenerateCurationReportsHandler handles the generation of curation reports for affiliate offers.
// It ensures permission enforcement, extracts necessary identifiers from context, executes the report generation logic,
// performs structured logging, and logs an audit record for traceability.
func (app *Application) GenerateCurationReportsHandler(w http.ResponseWriter, r *http.Request) {
	logger := app.Logger.GetLoggerWithContext(r).WithFunctionName("GenerateCurationReportsHandler")
	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	// Permission enforcement
	if !app.HasPermission(ctx, "generate_curation_reports") {
		app.respondWithError(w, errors.New("forbidden: insufficient permissions"), http.StatusForbidden)
		return
	}

	// Extract Offer ID from trusted context
	offerID := app.getOfferIDFromContext(ctx)
	if offerID == nil {
		app.respondWithError(w, errors.New("missing offer ID in context"), http.StatusBadRequest)
		return
	}

	// Execute curation report generation logic
	report, err := app.Models.UserDashboard.GenerateCurationReport(ctx)
	if err != nil {
		logger.Error("Failed to generate curation report", "offer_id", offerID, "error", err)
		app.respondWithError(w, errors.New("failed to generate curation report"), http.StatusInternalServerError)
		return
	}

	// Resolve Audit Action
	action, err := app.Models.Action.GetByName(ctx, "generate_curation_reports")
	if err != nil || action == nil {
		logger.Warn("Missing audit action: generate_curation_reports, creating...")
		actionID, createErr := app.Models.Action.CreateIfNotExists(ctx, "generate_curation_reports", "Generate curation reports for offers")
		if createErr != nil {
			logger.Error("Failed to create missing audit action", "error", createErr)
			// Allow main operation to succeed
		} else {
			action = &data.Action{ID: actionID}
		}
	}

	// Resolve Entity Type
	entityType, err := app.Models.EntityType.GetByName(ctx, "curation_report")
	if err != nil || entityType == nil {
		logger.Warn("Missing entity type: curation_report, creating...")
		entityTypeID, createErr := app.Models.EntityType.CreateIfNotExists(ctx, "curation_report", "Curation report for affiliate offers")
		if createErr != nil {
			logger.Error("Failed to create missing entity type", "error", createErr)
			// Allow main operation to succeed
		} else {
			entityType = &data.EntityType{ID: entityTypeID}
		}
	}

	// Insert Audit Log (log even if partial failure)
	userID := app.getUserIDFromContext(ctx)
	if userID != nil && action != nil && entityType != nil {
		audit := data.AuditLog{
			ID:           uuid.New(),
			UserID:       userID,
			ActionID:     &action.ID,
			EntityTypeID: entityType.ID,
			EntityID:     offerID.String(),
			Timestamp:    timeutil.Now(),
		}
		if err := app.Models.AuditLog.Insert(ctx, &audit); err != nil {
			// Audit logging failed (partial success)
			logger.Warn("Audit logging failed", "offer_id", offerID, "error", err)
			app.respondWithJSON(w, http.StatusPartialContent, jsonResponse{
				Error:   false,
				Message: "Curation report generated, but audit logging failed",
				Data:    report,
			})
			return
		}
	}

	// Success
	logger.Info("Successfully generated curation report", "offer_id", offerID)
	app.respondWithJSON(w, http.StatusOK, jsonResponse{
		Error:   false,
		Message: "Curation report generated successfully",
		Data:    report,
	})
}


// ListDashboardReportsHandler handles the retrieval of dashboard reports.
// It enforces permission checks, extracts trusted context identifiers,
// queries the database for reports, performs audit logging, and responds with the report details.
func (app *Application) ListDashboardReportsHandler(w http.ResponseWriter, r *http.Request) {
	logger := app.Logger.GetLoggerWithContext(r).
		WithFunctionName("ListDashboardReportsHandler")

	// Prepare context with timeout
	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	// Authorisation – “read_dashboard_reports” permission required
	if !app.HasPermission(ctx, "read_dashboard_reports") {
		app.respondWithError(w, errors.New("forbidden: insufficient permissions"), http.StatusForbidden)
		return
	}

	// Extract DashboardID injected by middleware
	dashboardID := app.getDashboardIDFromContext(ctx)
	if dashboardID == nil || *dashboardID == uuid.Nil {
		app.respondWithError(w, errors.New("missing or invalid dashboard ID"), http.StatusBadRequest)
		return
	}

	// Retrieve reports
	reports, err := app.Models.UserDashboardReport.GetByDashboardID(ctx, *dashboardID)
	if err != nil {
		logger.Error("failed to retrieve reports", "dashboard_id", dashboardID, "error", err)
		app.respondWithError(w, fmt.Errorf("failed to retrieve dashboard reports: %w", err), http.StatusInternalServerError)
		return
	}

	// Resolve Audit Action
	action, err := app.Models.Action.GetByName(ctx, "read_dashboard_reports")
	if err != nil || action == nil {
		logger.Warn("Audit action 'read_dashboard_reports' not found, attempting to create...", "error", err)
		actionID, createErr := app.Models.Action.CreateIfNotExists(ctx, "read_dashboard_reports", "Retrieve dashboard reports")
		if createErr != nil {
			logger.Error("Failed to create missing audit action", "error", createErr)
			// Allow main operation to succeed
		} else {
			action = &data.Action{ID: actionID}
		}
	}

	// Resolve Audit Entity Type
	entityType, err := app.Models.EntityType.GetByName(ctx, "dashboard_report")
	if err != nil || entityType == nil {
		logger.Warn("Audit entity type 'dashboard_report' not found, attempting to create...", "error", err)
		entityTypeID, createErr := app.Models.EntityType.CreateIfNotExists(ctx, "dashboard_report", "Dashboard report entity")
		if createErr != nil {
			logger.Error("Failed to create missing entity type", "error", createErr)
			// Allow main operation to succeed
		} else {
			entityType = &data.EntityType{ID: entityTypeID}
		}
	}

	// Insert Audit Log (fail gracefully if fails)
	userID := app.getUserIDFromContext(ctx)
	if userID != nil && action != nil && entityType != nil {
		audit := data.AuditLog{
			ID:           uuid.New(),
			UserID:       userID,
			ActionID:     &action.ID,
			EntityTypeID: entityType.ID,
			EntityID:     dashboardID.String(),
			Timestamp:    timeutil.Now(),
		}
		if err := app.Models.AuditLog.Insert(ctx, &audit); err != nil {
			// Audit logging failed (partial success)
			logger.Warn("Audit logging failed", "dashboard_id", dashboardID, "error", err)
			app.respondWithJSON(w, http.StatusPartialContent, jsonResponse{
				Error:   false,
				Message: "Dashboard reports retrieved, but audit logging failed",
				Data: struct {
					DashboardID uuid.UUID `json:"dashboard_id"`
					Reports     any       `json:"reports"`
					Count       int       `json:"count"`
				}{
					DashboardID: *dashboardID,
					Reports:     reports,
					Count:       len(reports),
				},
			})
			return
		}
	}

	// Respond success
	logger.Info("Dashboard reports retrieved successfully", "dashboard_id", dashboardID, "count", len(reports))
	app.respondWithJSON(w, http.StatusOK, jsonResponse{
		Error:   false,
		Message: "Dashboard reports retrieved successfully",
		Data: struct {
			DashboardID uuid.UUID `json:"dashboard_id"`
			Reports     any       `json:"reports"`
			Count       int       `json:"count"`
		}{
			DashboardID: *dashboardID,
			Reports:     reports,
			Count:       len(reports),
		},
	})
}


// AuditUserDashboardActivityHandler handles logging user activity on their dashboard.
// It enforces permission checks, extracts trusted identifiers from context,
// performs the main business operation (e.g., logging activity), and logs an audit entry.
func (app *Application) AuditUserDashboardActivityHandler(w http.ResponseWriter, r *http.Request) {
	logger := app.Logger.GetLoggerWithContext(r).WithFunctionName("AuditUserDashboardActivityHandler")
	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	// Permission enforcement
	if !app.HasPermission(ctx, "audit_user_dashboard_activity") {
		app.respondWithError(w, errors.New("forbidden: insufficient permissions"), http.StatusForbidden)
		return
	}

	// Extract User ID from context
	userID := app.getUserIDFromContext(ctx)
	if userID == nil {
		app.respondWithError(w, errors.New("missing user ID in context"), http.StatusUnauthorized)
		return
	}

	// Extract Dashboard ID from context
	dashboardID := app.getDashboardIDFromContext(ctx)
	if dashboardID == nil {
		app.respondWithError(w, errors.New("missing dashboard ID in context"), http.StatusBadRequest)
		return
	}

	// Perform the main business operation (e.g., logging activity)
	activity := struct {
		Action string `json:"action"`
		Detail string `json:"detail"`
	}{}

	if err := app.readJSON(w, r, &activity); err != nil {
		app.respondWithError(w, fmt.Errorf("invalid JSON input: %w", err), http.StatusBadRequest)
		return
	}

	// Log the activity (this is a placeholder for actual business logic)
	logger.Info("User activity logged", "user_id", userID, "dashboard_id", dashboardID, "action", activity.Action, "detail", activity.Detail)

	// Resolve Audit Action
	action, err := app.Models.Action.GetByName(ctx, "audit_user_dashboard_activity")
	if err != nil || action == nil {
		logger.Warn("Audit action 'audit_user_dashboard_activity' not found, attempting to create...", "error", err)
		actionID, createErr := app.Models.Action.CreateIfNotExists(ctx, "audit_user_dashboard_activity", "Audit user dashboard activity")
		if createErr != nil {
			logger.Error("Failed to create missing audit action", "error", createErr)
			// Allow main operation to succeed
		} else {
			action = &data.Action{ID: actionID}
		}
	}

	// Resolve Audit Entity Type
	entityType, err := app.Models.EntityType.GetByName(ctx, "user_dashboard_activity")
	if err != nil || entityType == nil {
		logger.Warn("Audit entity type 'user_dashboard_activity' not found, attempting to create...", "error", err)
		entityTypeID, createErr := app.Models.EntityType.CreateIfNotExists(ctx, "user_dashboard_activity", "User activity on dashboard")
		if createErr != nil {
			logger.Error("Failed to create missing entity type", "error", createErr)
			// Allow main operation to succeed
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
			EntityID:     dashboardID.String(),
			Timestamp:    timeutil.Now(),
		}
		if err := app.Models.AuditLog.Insert(ctx, &audit); err != nil {
			// Audit logging failed (partial success)
			logger.Warn("Audit logging failed", "dashboard_id", dashboardID, "error", err)
			app.respondWithJSON(w, http.StatusPartialContent, jsonResponse{
				Error:   false,
				Message: "Activity logged, but audit logging failed",
				Data: struct {
					DashboardID uuid.UUID `json:"dashboard_id"`
					Status      string    `json:"status"`
				}{
					DashboardID: *dashboardID,
					Status:      "audited",
				},
			})
			return
		}
	}

	// Respond success
	logger.Info("User dashboard activity audited successfully", "dashboard_id", dashboardID)
	app.respondWithJSON(w, http.StatusOK, jsonResponse{
		Error:   false,
		Message: "User dashboard activity audited successfully",
		Data: struct {
			DashboardID uuid.UUID `json:"dashboard_id"`
			Status      string    `json:"status"`
		}{
			DashboardID: *dashboardID,
			Status:      "audited",
		},
	})
}


// AssignDashboardTemplateToUserHandler assigns a dashboard template to a user.
// It enforces permission checks, extracts trusted identifiers from context,
// performs the assignment operation, logs the action, and handles audit logging.
func (app *Application) AssignDashboardTemplateToUserHandler(w http.ResponseWriter, r *http.Request) {
	logger := app.Logger.WithFunctionName("AssignDashboardTemplateToUserHandler")
	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	// Permission enforcement
	if !app.HasPermission(ctx, "assign_dashboard_template") {
		app.respondWithError(w, errors.New("forbidden: insufficient permissions"), http.StatusForbidden)
		return
	}

	// Extract User ID from context
	userID := app.getUserIDFromContext(ctx)
	if userID == nil {
		app.respondWithError(w, errors.New("missing user ID in context"), http.StatusUnauthorized)
		return
	}

	// Extract Dashboard Template ID from context
	dashboardTemplateID := app.getDashboardTemplateIDFromContext(ctx)
	if dashboardTemplateID == nil {
		app.respondWithError(w, errors.New("missing dashboard template ID in context"), http.StatusBadRequest)
		return
	}

	// Assign the dashboard template to the user
	err := app.Models.DashboardTemplate.AssignToUser(ctx, *dashboardTemplateID, *userID)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			app.respondWithError(w, errors.New("dashboard template or user not found"), http.StatusNotFound)
		} else {
			logger.Error("Failed to assign dashboard template", "error", err)
			app.respondWithError(w, errors.New("failed to assign dashboard template"), http.StatusInternalServerError)
		}
		return
	}

	// Resolve Audit Action
	action, err := app.Models.Action.GetByName(ctx, "assign_dashboard_template")
	if err != nil || action == nil {
		logger.Warn("Audit action 'assign_dashboard_template' not found, attempting to create...", "error", err)
		actionID, createErr := app.Models.Action.CreateIfNotExists(ctx, "assign_dashboard_template", "Assign a dashboard template to a user")
		if createErr != nil {
			logger.Error("Failed to create missing audit action", "error", createErr)
			// Allow main operation to succeed
		} else {
			action = &data.Action{ID: actionID}
		}
	}

	// Resolve Audit Entity Type
	entityType, err := app.Models.EntityType.GetByName(ctx, "dashboard_template")
	if err != nil || entityType == nil {
		logger.Warn("Audit entity type 'dashboard_template' not found, attempting to create...", "error", err)
		entityTypeID, createErr := app.Models.EntityType.CreateIfNotExists(ctx, "dashboard_template", "Dashboard template assigned to a user")
		if createErr != nil {
			logger.Error("Failed to create missing entity type", "error", createErr)
			// Allow main operation to succeed
		} else {
			entityType = &data.EntityType{ID: entityTypeID}
		}
	}

	// Insert Audit Log (fail gracefully if fails)
	if userID != nil && action != nil && entityType != nil {
		audit := data.AuditLog{
			ID:           uuid.New(),
			UserID:       userID,
			ActionID:     &action.ID,
			EntityTypeID: entityType.ID,
			EntityID:     dashboardTemplateID.String(),
			Timestamp:    timeutil.Now(),
		}
		if err := app.Models.AuditLog.Insert(ctx, &audit); err != nil {
			// Audit logging failed (partial success)
			logger.Warn("Audit logging failed", "dashboard_template_id", dashboardTemplateID, "error", err)
			app.respondWithJSON(w, http.StatusPartialContent, jsonResponse{
				Error:   false,
				Message: "Dashboard template assigned, but audit logging failed",
				Data:    *dashboardTemplateID,
			})
			return
		}
	}

	// Respond success
	logger.Info("Dashboard template assigned successfully", "dashboard_template_id", dashboardTemplateID)
	app.respondWithJSON(w, http.StatusOK, jsonResponse{
		Error:   false,
		Message: "Dashboard template assigned successfully",
		Data:    *dashboardTemplateID,
	})
}


/*
// CloneDashboardHandler() Clones a dashboard layout (e.g., for templating or reusing a setup).
func (app *Application) CloneDashboardHandler(w http.ResponseWriter, r *http.Request) {
	logger := app.Logger.GetLoggerWithContext(r).WithFunctionName("CloneDashboardHandler")
	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	// Permission enforcement
	if !app.HasPermission(ctx, "clone_dashboard") {
		app.respondWithError(w, errors.New("forbidden: insufficient permissions"), http.StatusForbidden)
		return
	}

	// Extract Dashboard ID from context
	dashboardID := app.getDashboardIDFromContext(ctx)
	if dashboardID == nil {
		app.respondWithError(w, errors.New("missing dashboard ID in context"), http.StatusBadRequest)
		return
	}

	// Retrieve the existing dashboard
	existingDashboard, err := app.Models.UserDashboard.GetByID(ctx, *dashboardID)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			app.respondWithError(w, fmt.Errorf("dashboard not found"), http.StatusNotFound)
		} else {
			logger.Error("Failed to retrieve dashboard", "error", err)
			app.respondWithError(w, fmt.Errorf("failed to retrieve dashboard: %w", err), http.StatusInternalServerError)
		}
		return
	}

	// Clone the dashboard
	clonedDashboard := existingDashboard.Clone()
	if err := app.Models.UserDashboard.Insert(ctx, clonedDashboard); err != nil {
		logger.Error("Failed to clone dashboard", "error", err)
		app.respondWithError(w, fmt.Errorf("failed to clone dashboard: %w", err), http.StatusInternalServerError)
		return
	}

	// Resolve Audit Action
	action, err := app.Models.Action.GetByName(ctx, "clone_dashboard")
	if err != nil || action == nil {
		logger.Warn("Audit action 'clone_dashboard' not found, attempting to create...", "error", err)
		actionID, createErr := app.Models.Action.CreateIfNotExists(ctx, "clone_dashboard", "Clone an existing dashboard")
		if createErr != nil {
			logger.Error("Failed to create missing audit action", "error", createErr)
			// Allow main operation to succeed
		} else {
			action = &data.Action{ID: actionID}
		}
	}

	// Resolve Audit Entity Type
	entityType, err := app.Models.EntityType.GetByName(ctx, "dashboard")
	if err != nil || entityType == nil {
		logger.Warn("Audit entity type 'dashboard' not found, attempting to create...", "error", err)
		entityTypeID, createErr := app.Models.EntityType.CreateIfNotExists(ctx, "dashboard", "User dashboard entity")
		if createErr != nil {
			logger.Error("Failed to create missing entity type", "error", createErr)
			// Allow main operation to succeed
		} else {
			entityType = &data.EntityType{ID: entityTypeID}
		}
	}

	// Insert Audit Log (fail gracefully if fails)
	userID := app.getUserIDFromContext(ctx)
	if userID != nil && action != nil && entityType != nil {
		audit := data.AuditLog{
			ID:           uuid.New(),
			UserID:       userID,
			ActionID:     &action.ID,
			EntityTypeID: entityType.ID,
			EntityID:     clonedDashboard.ID.String(),
			Timestamp:    timeutil.Now(),
		}
		if err := app.Models.AuditLog.Insert(ctx, &audit); err != nil {
			logger.Warn("Audit logging failed", "dashboard_id", clonedDashboard.ID, "error", err)
			app.respondWithJSON(w, http.StatusPartialContent, map[string]any{
				"dashboard_id": clonedDashboard.ID,
				"error":        "Dashboard cloned, but audit logging failed",
			})
			return
		}
	}

	// Respond success
	logger.Info("Dashboard cloned successfully", "dashboard_id", clonedDashboard.ID)
	app.respondWithJSON(w, http.StatusOK, map[string]any{
		"dashboard_id": clonedDashboard.ID,
	})
}


// ExportUserDashboardHandler handles exporting the user's dashboard data.
// It enforces permission checks, extracts user and dashboard IDs from context,
// retrieves the dashboard data from the database, performs audit logging,
// and responds with the exported data.
func (app *Application) ExportUserDashboardHandler(w http.ResponseWriter, r *http.Request) {
	logger := app.Logger.GetLoggerWithContext(r).WithFunctionName("ExportUserDashboardHandler")
	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	// Permission enforcement
	if !app.HasPermission(ctx, "export_user_dashboard") {
		app.respondWithError(w, errors.New("forbidden: insufficient permissions"), http.StatusForbidden)
		return
	}

	// Extract User ID from context
	userID := app.getUserIDFromContext(ctx)
	if userID == nil {
		app.respondWithError(w, errors.New("missing user ID in context"), http.StatusUnauthorized)
		return
	}

	// Extract Dashboard ID from context
	dashboardID := app.getDashboardIDFromContext(ctx)
	if dashboardID == nil {
		app.respondWithError(w, errors.New("missing dashboard ID in context"), http.StatusBadRequest)
		return
	}

	// Retrieve the dashboard data
	dashboardData, err := app.Models.Dashboard.GetByID(ctx, *dashboardID)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			app.respondWithError(w, fmt.Errorf("dashboard not found"), http.StatusNotFound)
		} else {
			logger.Error("Failed to retrieve dashboard data", "error", err)
			app.respondWithError(w, fmt.Errorf("failed to retrieve dashboard data: %w", err), http.StatusInternalServerError)
		}
		return
	}

	// Resolve Audit Action
	action, err := app.Models.Action.GetByName(ctx, "export_user_dashboard")
	if err != nil || action == nil {
		logger.Warn("Audit action 'export_user_dashboard' not found, attempting to create...", "error", err)
		actionID, createErr := app.Models.Action.CreateIfNotExists(ctx, "export_user_dashboard", "Export user dashboard data")
		if createErr != nil {
			logger.Error("Failed to create missing audit action", "error", createErr)
			// Allow main operation to succeed
		} else {
			action = &data.Action{ID: actionID}
		}
	}

	// Resolve Audit Entity Type
	entityType, err := app.Models.EntityType.GetByName(ctx, "user_dashboard")
	if err != nil || entityType == nil {
		logger.Warn("Audit entity type 'user_dashboard' not found, attempting to create...", "error", err)
		entityTypeID, createErr := app.Models.EntityType.CreateIfNotExists(ctx, "user_dashboard", "User dashboard data")
		if createErr != nil {
			logger.Error("Failed to create missing entity type", "error", createErr)
			// Allow main operation to succeed
		} else {
			entityType = &data.EntityType{ID: entityTypeID}
		}
	}

	// Insert Audit Log (fail gracefully if fails)
	if userID != nil && action != nil && entityType != nil {
		audit := data.AuditLog{
			ID:           uuid.New(),
			UserID:       userID,
			ActionID:     &action.ID,
			EntityTypeID: entityType.ID,
			EntityID:     dashboardID.String(),
			Timestamp:    timeutil.Now(),
		}
		if err := app.Models.AuditLog.Insert(ctx, &audit); err != nil {
			logger.Warn("Audit logging failed", "dashboard_id", dashboardID, "error", err)
			app.respondWithJSON(w, http.StatusPartialContent, map[string]any{
				"dashboard_id": dashboardID,
				"error":        "Dashboard exported, but audit logging failed",
			})
			return
		}
	}

	// Respond success
	logger.Info("Dashboard exported successfully", "dashboard_id", dashboardID)
	app.respondWithJSON(w, http.StatusOK, map[string]any{
		"dashboard_id": dashboardID,
		"data":         dashboardData,
	})
}


// GetUserDashboardWidgetsHandler retrieves the widgets for a user's dashboard.
// It enforces permission checks, extracts user ID from context, queries the database for widgets,
// performs structured logging, and responds with the widget details.
func (app *Application) GetUserDashboardWidgetsHandler(w http.ResponseWriter, r *http.Request) {
	logger := app.Logger.GetLoggerWithContext(r).WithFunctionName("GetUserDashboardWidgetsHandler")
	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	// Permission enforcement
	if !app.HasPermission(ctx, "read_user_dashboard_widgets") {
		app.respondWithError(w, errors.New("forbidden: insufficient permissions"), http.StatusForbidden)
		return
	}

	// Extract User ID from context
	userID := app.getUserIDFromContext(ctx)
	if userID == nil {
		app.respondWithError(w, errors.New("missing user ID in context"), http.StatusUnauthorized)
		return
	}

	// Retrieve user dashboard widgets from the database
	widgets, err := app.Models.UserDashboard.GetWidgetsByUserID(ctx, *userID)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			app.respondWithError(w, fmt.Errorf("widgets not found"), http.StatusNotFound)
		} else {
			logger.Error("Failed to retrieve user dashboard widgets", "error", err)
			app.respondWithError(w, fmt.Errorf("failed to retrieve widgets: %w", err), http.StatusInternalServerError)
		}
		return
	}

	// Resolve Audit Action
	action, err := app.Models.Action.GetByName(ctx, "read_user_dashboard_widgets")
	if err != nil || action == nil {
		logger.Warn("Audit action 'read_user_dashboard_widgets' not found, attempting to create...", "error", err)
		actionID, createErr := app.Models.Action.CreateIfNotExists(ctx, "read_user_dashboard_widgets", "Retrieve user dashboard widgets")
		if createErr != nil {
			logger.Error("Failed to create missing audit action", "error", createErr)
			// Allow main operation to succeed
		} else {
			action = &data.Action{ID: actionID}
		}
	}

	// Resolve Audit Entity Type
	entityType, err := app.Models.EntityType.GetByName(ctx, "user_dashboard_widget")
	if err != nil || entityType == nil {
		logger.Warn("Audit entity type 'user_dashboard_widget' not found, attempting to create...", "error", err)
		entityTypeID, createErr := app.Models.EntityType.CreateIfNotExists(ctx, "user_dashboard_widget", "Widgets for user dashboard")
		if createErr != nil {
			logger.Error("Failed to create missing entity type", "error", createErr)
			// Allow main operation to succeed
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
			EntityID:     userID.String(), // EntityID here is the UserID we retrieved widgets for
			Timestamp:    timeutil.Now(),
		}
		if err := app.Models.AuditLog.Insert(ctx, &audit); err != nil {
			logger.Warn("Audit logging failed", "user_id", userID, "error", err)
			// Proceed without failing the main operation
		}
	}

	// Success response
	logger.Info("Retrieved user dashboard widgets successfully", "user_id", userID, "count", len(widgets))
	app.respondWithJSON(w, http.StatusOK, map[string]any{
		"user_id":  userID,
		"widgets":  widgets,
		"count":    len(widgets),
	})
}


// SaveUserDashboardPreferencesHandler handles saving user-specific dashboard preferences.
// It enforces permission checks, extracts user ID from trusted context, parses the request payload,
// updates the preferences in the database, performs structured logging, and logs an audit record.
func (app *Application) SaveUserDashboardPreferencesHandler(w http.ResponseWriter, r *http.Request) {
	logger := app.Logger.GetLoggerWithContext(r).WithFunctionName("SaveUserDashboardPreferencesHandler")
	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	// Permission enforcement
	if !app.HasPermission(ctx, "save_user_dashboard_preferences") {
		app.respondWithError(w, errors.New("forbidden: insufficient permissions"), http.StatusForbidden)
		return
	}

	// Extract User ID from context
	userID := app.getUserIDFromContext(ctx)
	if userID == nil {
		app.respondWithError(w, errors.New("missing user ID in context"), http.StatusUnauthorized)
		return
	}

	// Parse request body for dashboard preferences
	var input struct {
		Preferences map[string]interface{} `json:"preferences"` // Preferences are stored as a map
	}

	if err := app.readJSON(w, r, &input); err != nil {
		app.respondWithError(w, fmt.Errorf("invalid JSON input: %w", err), http.StatusBadRequest)
		return
	}

	// Update user dashboard preferences in the database
	if err := app.Models.UserDashboard.UpdatePreferences(ctx, *userID, input.Preferences); err != nil {
		logger.Error("Failed to update user dashboard preferences", "user_id", userID, "error", err)
		app.respondWithError(w, fmt.Errorf("failed to save dashboard preferences: %w", err), http.StatusInternalServerError)
		return
	}

	// Resolve Audit Action
	action, err := app.Models.Action.GetByName(ctx, "save_user_dashboard_preferences")
	if err != nil || action == nil {
		logger.Warn("Audit action 'save_user_dashboard_preferences' not found, attempting to create...", "error", err)
		actionID, createErr := app.Models.Action.CreateIfNotExists(ctx, "save_user_dashboard_preferences", "Save user dashboard preferences")
		if createErr != nil {
			logger.Error("Failed to create missing audit action", "error", createErr)
			// Allow main operation to succeed
		} else {
			action = &data.Action{ID: actionID}
		}
	}

	// Resolve Audit Entity Type
	entityType, err := app.Models.EntityType.GetByName(ctx, "user_dashboard_preferences")
	if err != nil || entityType == nil {
		logger.Warn("Audit entity type 'user_dashboard_preferences' not found, attempting to create...", "error", err)
		entityTypeID, createErr := app.Models.EntityType.CreateIfNotExists(ctx, "user_dashboard_preferences", "User-specific dashboard preferences")
		if createErr != nil {
			logger.Error("Failed to create missing entity type", "error", createErr)
			// Allow main operation to succeed
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
			EntityID:     userID.String(),
			Timestamp:    timeutil.Now(),
		}
		if err := app.Models.AuditLog.Insert(ctx, &audit); err != nil {
			logger.Warn("Audit logging failed", "user_id", userID, "error", err)
			app.respondWithJSON(w, http.StatusPartialContent, map[string]any{
				"user_id": userID,
				"error":   "Preferences saved, but audit logging failed",
			})
			return
		}
	}

	// Respond success
	logger.Info("User dashboard preferences saved successfully", "user_id", userID)
	app.respondWithJSON(w, http.StatusOK, map[string]any{
		"user_id": userID,
	})
}


// ListAllUserDashboardsHandler handles the retrieval of all user dashboards.
// It enforces permission checks, extracts trusted identifiers from context,
// queries the database for user dashboards, performs structured logging,
// and logs an audit entry for the operation.
func (app *Application) ListAllUserDashboardsHandler(w http.ResponseWriter, r *http.Request) {
    logger := app.Logger.GetLoggerWithContext(r).WithFunctionName("ListAllUserDashboardsHandler")
    ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
    defer cancel()

    // Permission enforcement
    if !app.HasPermission(ctx, "read_user_dashboards") {
        app.respondWithError(w, errors.New("forbidden: insufficient permissions"), http.StatusForbidden)
        return
    }

    // Extract User ID from context
    userID := app.getUserIDFromContext(ctx)
    if userID == nil {
        app.respondWithError(w, errors.New("missing user ID in context"), http.StatusUnauthorized)
        return
    }

    // Retrieve all user dashboards from the database
    dashboards, err := app.Models.UserDashboard.GetAllByUserID(ctx, *userID)
    if err != nil {
        logger.Error("Failed to retrieve user dashboards", "user_id", userID, "error", err)
        app.respondWithError(w, fmt.Errorf("failed to retrieve user dashboards: %w", err), http.StatusInternalServerError)
        return
    }

    // Resolve Audit Action
    action, err := app.Models.Action.GetByName(ctx, "read_user_dashboards")
    if err != nil || action == nil {
        logger.Warn("Audit action 'read_user_dashboards' not found, attempting to create...", "error", err)
        actionID, createErr := app.Models.Action.CreateIfNotExists(ctx, "read_user_dashboards", "Retrieve all user dashboards")
        if createErr != nil {
            logger.Error("Failed to create missing audit action", "error", createErr)
			// Allow main operation to succeed
        } else {
            action = &data.Action{ID: actionID}
        }
    }

    // Resolve Audit Entity Type
    entityType, err := app.Models.EntityType.GetByName(ctx, "user_dashboard")
    if err != nil || entityType == nil {
        logger.Warn("Audit entity type 'user_dashboard' not found, attempting to create...", "error", err)
        entityTypeID, createErr := app.Models.EntityType.CreateIfNotExists(ctx, "user_dashboard", "User dashboard entity")
        if createErr != nil {
            logger.Error("Failed to create missing entity type", "error", createErr)
			// Allow main operation to succeed
        } else {
            entityType = &data.EntityType{ID: entityTypeID}
        }
    }

    // Insert Audit Log (fail gracefully if fails)
    if userID != nil && action != nil && entityType != nil {
        audit := data.AuditLog{
            ID:           uuid.New(),
            UserID:       userID,
            ActionID:     &action.ID,
            EntityTypeID: entityType.ID,
            EntityID:     "all_dashboards",
            Timestamp:    timeutil.Now(),
        }
        if err := app.Models.AuditLog.Insert(ctx, &audit); err != nil {
            logger.Warn("Audit logging failed", "user_id", userID, "error", err)
            app.respondWithJSON(w, http.StatusPartialContent, map[string]any{
                "dashboards": dashboards,
                "warning":    "Dashboards retrieved, but audit logging failed",
            })
            return
        }
    }

    // Respond success
    logger.Info("User dashboards retrieved successfully", "user_id", userID, "count", len(dashboards))
    app.respondWithJSON(w, http.StatusOK, map[string]any{
        "dashboards": dashboards,
    })
}
*/