// Package data provides models and database access methods for user dashboards and other entities.
//
// sdworkspace/sdbackend/internal/data/user_dashboards.go
//
// GTM:
//   Layer: 2.3 Consumer Domain
//   Release Class: DEFERRED
//   Reason:
//     User dashboards, dashboard reports, and dashboard templates are valid
//     future personalization and reporting infrastructure, but they are not
//     required for the initial Platform release spine. The v1 spine
//     requires account identity, notifications, favorites/stash, merchant
//     follows, and core offer behavior before expanding into rich dashboard
//     configuration and reporting.
//
// DEFERRED Rule:
//   Keep compiling.
//   Keep safe.
//   Preserve dashboard owner-type validation.
//   Preserve persisted dashboard/report/template shape.
//   Preserve bounded report listing behavior.
//   Preserve idempotent template assignment behavior.
//   Do not add new features.
//   Do not route into v1 UI/API expansion.
//   Do not block deployment on this file unless it breaks the build.
package data

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/PiccoloMondoC/sdworkspace/sdbackend/internal/logging"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// UserDashboard represents a persisted dashboard configuration owned by a user.
type UserDashboard struct {
	ID          uuid.UUID      `json:"id" db:"id"`
	UserID      uuid.UUID      `json:"user_id" db:"user_id"`
	UserType    string         `json:"user_type" db:"user_type"`
	Name        string         `json:"name" db:"name"`
	Description *string        `json:"description,omitempty" db:"description"`
	Layout      map[string]any `json:"layout,omitempty" db:"layout"`
	Widgets     []any          `json:"widgets,omitempty" db:"widgets"`
	Filters     map[string]any `json:"filters,omitempty" db:"filters"`
	CreatedAt   time.Time      `json:"created_at" db:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at" db:"updated_at"`
}

// UserDashboardReport represents a persisted dashboard report row.
type UserDashboardReport struct {
	ID                    uuid.UUID `json:"id" db:"id"`
	ReportID              uuid.UUID `json:"report_id" db:"report_id"`
	TotalDashboards       int       `json:"total_dashboards" db:"total_dashboards"`
	DashboardsWithWidgets int       `json:"dashboards_with_widgets" db:"dashboards_with_widgets"`
	UserType              string    `json:"user_type" db:"user_type"`
	GeneratedAt           time.Time `json:"generated_at" db:"generated_at"`
	CreatedAt             time.Time `json:"created_at" db:"created_at"`
	UpdatedAt             time.Time `json:"updated_at" db:"updated_at"`
}

// DashboardTemplate represents a reusable dashboard blueprint.
type DashboardTemplate struct {
	ID          uuid.UUID      `json:"id" db:"id"`
	Name        string         `json:"name" db:"name"`
	Description *string        `json:"description,omitempty" db:"description"`
	Layout      map[string]any `json:"layout,omitempty" db:"layout"`
	Widgets     []any          `json:"widgets,omitempty" db:"widgets"`
	Filters     map[string]any `json:"filters,omitempty" db:"filters"`
	CreatedAt   time.Time      `json:"created_at" db:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at" db:"updated_at"`
}

// UserDashboardModel is the structure which holds the DB instance.
type UserDashboardModel struct {
	DB     *pgxpool.Pool
	Logger *logging.Logger
}

// UserDashboardReportModel is the structure which holds the DB instance.
type UserDashboardReportModel struct {
	DB     *pgxpool.Pool
	Logger *logging.Logger
}

// DashboardTemplateModel wraps DB access.
type DashboardTemplateModel struct {
	DB     *pgxpool.Pool
	Logger *logging.Logger
}

// normalizeDashboardUserType validates and canonicalizes the persisted dashboard owner type.
func normalizeDashboardUserType(userType string) (string, error) {
	normalized := strings.ToLower(strings.TrimSpace(userType))

	switch normalized {
	case "customer", "merchant":
		return normalized, nil
	default:
		return "", fmt.Errorf("invalid user_type %q: must be 'customer' or 'merchant'", userType)
	}
}

// scanUserDashboard scans a user_dashboard row into a UserDashboard struct.
func scanUserDashboard(row pgx.Row, dashboard *UserDashboard) error {
	return row.Scan(
		&dashboard.ID,
		&dashboard.UserID,
		&dashboard.UserType,
		&dashboard.Name,
		&dashboard.Description,
		&dashboard.Layout,
		&dashboard.Widgets,
		&dashboard.Filters,
		&dashboard.CreatedAt,
		&dashboard.UpdatedAt,
	)
}

// Insert inserts a new user dashboard into the database.
func (m *UserDashboardModel) Insert(ctx context.Context, dashboard *UserDashboard) error {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("UserDashboardModel.Insert")

	if dashboard == nil {
		err := errors.New("dashboard is required")
		logger.Error("Validation failed", err)
		return err
	}

	if dashboard.UserID == uuid.Nil {
		err := errors.New("user ID is required")
		logger.Error("Validation failed", err)
		return err
	}

	normalizedUserType, err := normalizeDashboardUserType(dashboard.UserType)
	if err != nil {
		logger.Error("Validation failed", err)
		return err
	}
	dashboard.UserType = normalizedUserType

	if dashboard.ID == uuid.Nil {
		dashboard.ID = uuid.New()
	}

	const query = `
		INSERT INTO user_dashboards (id, user_id, user_type, name, description, layout, widgets, filters)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING created_at, updated_at
	`

	err = m.DB.QueryRow(
		ctx,
		query,
		dashboard.ID,
		dashboard.UserID,
		dashboard.UserType,
		dashboard.Name,
		dashboard.Description,
		dashboard.Layout,
		dashboard.Widgets,
		dashboard.Filters,
	).Scan(&dashboard.CreatedAt, &dashboard.UpdatedAt)
	if err != nil {
		logger.Error("Insert user dashboard failed", err)
		return err
	}

	logger.Info("Insert user dashboard successful", "dashboard_id", dashboard.ID, "user_id", dashboard.UserID, "user_type", dashboard.UserType)
	return nil
}

// GetByID retrieves a user dashboard by its unique ID.
func (m *UserDashboardModel) GetByID(ctx context.Context, id uuid.UUID) (*UserDashboard, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("UserDashboardModel.GetByID")

	if id == uuid.Nil {
		err := errors.New("dashboard ID is required")
		logger.Error("Validation failed", err)
		return nil, err
	}

	const query = `
		SELECT id, user_id, user_type, name, description, layout, widgets, filters, created_at, updated_at
		FROM user_dashboards
		WHERE id = $1
	`

	var dashboard UserDashboard
	err := scanUserDashboard(m.DB.QueryRow(ctx, query, id), &dashboard)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			logger.Warn("User dashboard not found", "dashboard_id", id)
			return nil, nil
		}
		logger.Error("Query user dashboard failed", err)
		return nil, err
	}

	logger.Info("User dashboard retrieved successfully", "dashboard_id", dashboard.ID)
	return &dashboard, nil
}

// GetByUserID retrieves the single dashboard associated with a user.
func (m *UserDashboardModel) GetByUserID(ctx context.Context, userID uuid.UUID) (*UserDashboard, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("UserDashboardModel.GetByUserID")

	if userID == uuid.Nil {
		err := errors.New("user ID is required")
		logger.Error("Validation failed", err)
		return nil, err
	}

	const query = `
		SELECT id, user_id, user_type, name, description, layout, widgets, filters, created_at, updated_at
		FROM user_dashboards
		WHERE user_id = $1
		LIMIT 1
	`

	var dashboard UserDashboard
	err := scanUserDashboard(m.DB.QueryRow(ctx, query, userID), &dashboard)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			logger.Warn("User dashboard not found for user", "user_id", userID)
			return nil, nil
		}
		logger.Error("Query user dashboard by user_id failed", err)
		return nil, err
	}

	logger.Info("GetByUserID successful", "user_id", userID, "dashboard_id", dashboard.ID)
	return &dashboard, nil
}

// Update updates mutable dashboard fields.
func (m *UserDashboardModel) Update(ctx context.Context, dashboard *UserDashboard) error {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("UserDashboardModel.Update")

	if dashboard == nil {
		err := errors.New("dashboard is required")
		logger.Error("Validation failed", err)
		return err
	}

	if dashboard.ID == uuid.Nil {
		err := errors.New("dashboard ID is required")
		logger.Error("Validation failed", err)
		return err
	}

	if dashboard.Name == "" && dashboard.Description == nil && dashboard.Layout == nil && dashboard.Widgets == nil && dashboard.Filters == nil {
		err := errors.New("at least one of name, description, layout, widgets, or filters must be provided")
		logger.Error("Validation failed", err)
		return err
	}

	const query = `
		UPDATE user_dashboards
		SET name = COALESCE(NULLIF($2, ''), name),
			description = COALESCE($3, description),
			layout = COALESCE($4, layout),
			widgets = COALESCE($5, widgets),
			filters = COALESCE($6, filters),
			updated_at = NOW()
		WHERE id = $1
		RETURNING updated_at
	`

	err := m.DB.QueryRow(
		ctx,
		query,
		dashboard.ID,
		dashboard.Name,
		dashboard.Description,
		dashboard.Layout,
		dashboard.Widgets,
		dashboard.Filters,
	).Scan(&dashboard.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			err = fmt.Errorf("no dashboard found with ID %s", dashboard.ID)
			logger.Warn("Update affected no rows", "dashboard_id", dashboard.ID)
			return err
		}
		logger.Error("Update user dashboard failed", err)
		return err
	}

	logger.Info("Update user dashboard successful", "dashboard_id", dashboard.ID)
	return nil
}

// Delete permanently deletes a user dashboard by its unique ID.
func (m *UserDashboardModel) Delete(ctx context.Context, id uuid.UUID) error {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("UserDashboardModel.Delete")

	if id == uuid.Nil {
		err := errors.New("dashboard ID is required")
		logger.Error("Validation failed", err)
		return err
	}

	const query = `
		DELETE FROM user_dashboards
		WHERE id = $1
		RETURNING id
	`

	var deletedID uuid.UUID
	err := m.DB.QueryRow(ctx, query, id).Scan(&deletedID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			err = fmt.Errorf("no user dashboard found with ID %s", id)
			logger.Warn("Delete user dashboard not found", "dashboard_id", id)
			return err
		}
		logger.Error("Delete user dashboard failed", err, "dashboard_id", id)
		return err
	}

	logger.Info("Delete user dashboard successful", "dashboard_id", deletedID)
	return nil
}

// GetAdminStats retrieves internal/admin-facing aggregate statistics about the dashboard domain.
func (m *UserDashboardModel) GetAdminStats(ctx context.Context) (int64, time.Time, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("UserDashboardModel.GetAdminStats")

	var total int64
	var latestUpdatedAt time.Time

	const query = `
		SELECT COUNT(*), COALESCE(MAX(updated_at), 'epoch'::timestamptz)
		FROM user_dashboards
	`

	err := m.DB.QueryRow(ctx, query).Scan(&total, &latestUpdatedAt)
	if err != nil {
		logger.Error("GetAdminStats query failed", err)
		return 0, time.Time{}, err
	}

	logger.Info("GetAdminStats query successful", "total", total, "latest_updated_at", latestUpdatedAt)
	return total, latestUpdatedAt, nil
}

// GenerateCurationReport generates and persists an internal/admin-facing report summarizing the current dashboard domain.
func (m *UserDashboardModel) GenerateCurationReport(ctx context.Context) (*UserDashboardReport, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("UserDashboardModel.GenerateCurationReport")

	report := &UserDashboardReport{
		ID:       uuid.New(),
		ReportID: uuid.New(),
		UserType: "admin",
	}

	const query = `
		WITH dashboard_counts AS (
			SELECT COUNT(*)::int AS total_dashboards,
			       COUNT(*) FILTER (WHERE jsonb_array_length(widgets) > 0)::int AS dashboards_with_widgets
			FROM user_dashboards
		)
		INSERT INTO user_dashboard_reports (id, report_id, total_dashboards, dashboards_with_widgets, user_type)
		SELECT $1, $2, total_dashboards, dashboards_with_widgets, $3
		FROM dashboard_counts
		RETURNING total_dashboards, dashboards_with_widgets, generated_at, created_at, updated_at
	`

	err := m.DB.QueryRow(ctx, query, report.ID, report.ReportID, report.UserType).Scan(
		&report.TotalDashboards,
		&report.DashboardsWithWidgets,
		&report.GeneratedAt,
		&report.CreatedAt,
		&report.UpdatedAt,
	)
	if err != nil {
		logger.Error("Failed to generate dashboard report", err)
		return nil, err
	}

	logger.Info("Dashboard report generated and persisted successfully",
		"report_id", report.ReportID,
		"total_dashboards", report.TotalDashboards,
		"dashboards_with_widgets", report.DashboardsWithWidgets,
		"user_type", report.UserType,
	)

	return report, nil
}

// scanUserDashboardReport scans a single row into a UserDashboardReport, for use with pgx.CollectRows.
func scanUserDashboardReport(row pgx.CollectableRow) (*UserDashboardReport, error) {
	var report UserDashboardReport
	err := row.Scan(
		&report.ID,
		&report.ReportID,
		&report.TotalDashboards,
		&report.DashboardsWithWidgets,
		&report.UserType,
		&report.GeneratedAt,
		&report.CreatedAt,
		&report.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &report, nil
}

// scanUserDashboardReportFields scans a single user_dashboard_reports row from a direct QueryRow call.
func scanUserDashboardReportFields(row interface{ Scan(dest ...any) error }) (*UserDashboardReport, error) {
	var report UserDashboardReport
	err := row.Scan(
		&report.ID,
		&report.ReportID,
		&report.TotalDashboards,
		&report.DashboardsWithWidgets,
		&report.UserType,
		&report.GeneratedAt,
		&report.CreatedAt,
		&report.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &report, nil
}

// ListReports retrieves dashboard reports optionally filtered by userType and created_at date range.
func (m *UserDashboardReportModel) ListReports(ctx context.Context, userType string, startDate, endDate time.Time) ([]*UserDashboardReport, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("UserDashboardReportModel.ListReports")

	query := `
		SELECT id, report_id, total_dashboards, dashboards_with_widgets, user_type, generated_at, created_at, updated_at
		FROM user_dashboard_reports
	`
	var filters []string
	var args []any
	argIndex := 1

	if strings.TrimSpace(userType) != "" {
		filters = append(filters, fmt.Sprintf("user_type = $%d", argIndex))
		args = append(args, strings.TrimSpace(userType))
		argIndex++
	}

	if !startDate.IsZero() && !endDate.IsZero() {
		filters = append(filters, fmt.Sprintf("created_at BETWEEN $%d AND $%d", argIndex, argIndex+1))
		args = append(args, startDate, endDate)
		argIndex += 2
	}

	if len(filters) > 0 {
		query += " WHERE " + strings.Join(filters, " AND ")
	}

	query += " ORDER BY created_at DESC"

	rows, err := m.DB.Query(ctx, query, args...)
	if err != nil {
		logger.Error("Failed to query dashboard reports with filters", err)
		return nil, err
	}
	defer rows.Close()

	reports, err := pgx.CollectRows(rows, scanUserDashboardReport)
	if err != nil {
		logger.Error("Failed to collect dashboard reports", err)
		return nil, err
	}

	logger.Info("Successfully retrieved filtered dashboard reports", "count", len(reports))
	return reports, nil
}

// GetReportByID retrieves a UserDashboardReport by its report_id.
func (m *UserDashboardReportModel) GetReportByID(ctx context.Context, reportID uuid.UUID) (*UserDashboardReport, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("UserDashboardReportModel.GetReportByID")

	if reportID == uuid.Nil {
		err := errors.New("reportID is required")
		logger.Warn("Validation failed", "error", err)
		return nil, err
	}

	const query = `
		SELECT id, report_id, total_dashboards, dashboards_with_widgets, user_type, generated_at, created_at, updated_at
		FROM user_dashboard_reports
		WHERE report_id = $1
	`

	report, err := scanUserDashboardReportFields(m.DB.QueryRow(ctx, query, reportID))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			logger.Info("No report found for given report_id", "report_id", reportID)
			return nil, nil
		}
		logger.Error("Failed to retrieve report by ID", err, "report_id", reportID)
		return nil, err
	}

	logger.Info("Retrieved dashboard report successfully", "report_id", reportID)
	return report, nil
}

// ListDashboardReports retrieves dashboard reports filtered by userType, date range, and optional minimum widget count.
func (m *UserDashboardReportModel) ListDashboardReports(
	ctx context.Context,
	userType string,
	startDate, endDate time.Time,
	minWidgets *int,
	limit, offset int,
	sortBy, sortOrder string,
) ([]*UserDashboardReport, int64, bool, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("UserDashboardReportModel.ListDashboardReports")

	if strings.TrimSpace(userType) == "" {
		err := errors.New("userType is required")
		logger.Warn("Validation failed", "error", err)
		return nil, 0, false, err
	}

	if endDate.Before(startDate) {
		err := errors.New("endDate cannot be before startDate")
		logger.Warn("Invalid date range", "error", err)
		return nil, 0, false, err
	}

	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	if offset < 0 {
		offset = 0
	}

	validSortFields := map[string]bool{
		"generated_at": true,
		"created_at":   true,
		"updated_at":   true,
	}
	if !validSortFields[sortBy] {
		sortBy = "generated_at"
	}

	sortOrder = strings.ToUpper(strings.TrimSpace(sortOrder))
	if sortOrder != "DESC" {
		sortOrder = "ASC"
	}

	query := `
		SELECT id, report_id, total_dashboards, dashboards_with_widgets, user_type, generated_at, created_at, updated_at
		FROM user_dashboard_reports
		WHERE user_type = $1
		  AND generated_at BETWEEN $2 AND $3
	`
	args := []any{strings.TrimSpace(userType), startDate, endDate}
	argIndex := 4

	if minWidgets != nil {
		query += fmt.Sprintf(" AND dashboards_with_widgets >= $%d", argIndex)
		args = append(args, *minWidgets)
		argIndex++
	}

	query += fmt.Sprintf(" ORDER BY %s %s LIMIT $%d OFFSET $%d", sortBy, sortOrder, argIndex, argIndex+1)
	args = append(args, limit, offset)

	rows, err := m.DB.Query(ctx, query, args...)
	if err != nil {
		logger.Error("Failed to query dashboard reports", err)
		return nil, 0, false, fmt.Errorf("query error: %w", err)
	}
	defer rows.Close()

	reports, err := pgx.CollectRows(rows, scanUserDashboardReport)
	if err != nil {
		logger.Error("Failed to collect dashboard reports", err)
		return nil, 0, false, fmt.Errorf("collect rows error: %w", err)
	}

	countQuery := `
		SELECT COUNT(*)
		FROM user_dashboard_reports
		WHERE user_type = $1
		  AND generated_at BETWEEN $2 AND $3
	`
	countArgs := []any{strings.TrimSpace(userType), startDate, endDate}
	if minWidgets != nil {
		countQuery += " AND dashboards_with_widgets >= $4"
		countArgs = append(countArgs, *minWidgets)
	}

	var totalCount int64
	err = m.DB.QueryRow(ctx, countQuery, countArgs...).Scan(&totalCount)
	if err != nil {
		logger.Error("Failed to count total dashboard reports", err)
		return nil, 0, false, fmt.Errorf("count error: %w", err)
	}

	hasMore := int64(offset+limit) < totalCount

	logger.Info("Successfully listed dashboard reports",
		"user_type", userType,
		"start_date", startDate.Format(time.RFC3339),
		"end_date", endDate.Format(time.RFC3339),
		"min_widgets", minWidgets,
		"limit", limit,
		"offset", offset,
		"sort_by", sortBy,
		"sort_order", sortOrder,
		"total_count", totalCount,
		"has_more", hasMore,
	)

	return reports, totalCount, hasMore, nil
}

// GetByReportID returns all rows matching the supplied report_id.
func (m *UserDashboardReportModel) GetByReportID(ctx context.Context, reportID uuid.UUID) ([]*UserDashboardReport, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).
		WithFunctionName("UserDashboardReportModel.GetByReportID").
		WithCustomField("report_id", reportID)

	if reportID == uuid.Nil {
		err := errors.New("reportID is required")
		logger.Warn("Validation failed", "error", err)
		return nil, err
	}

	const query = `
		SELECT id, report_id, total_dashboards, dashboards_with_widgets, user_type, generated_at, created_at, updated_at
		FROM user_dashboard_reports
		WHERE report_id = $1
		ORDER BY created_at DESC
	`

	rows, err := m.DB.Query(ctx, query, reportID)
	if err != nil {
		logger.Error("Query failed", err)
		return nil, err
	}
	defer rows.Close()

	reports, err := pgx.CollectRows(rows, scanUserDashboardReport)
	if err != nil {
		logger.Error("Failed to collect reports", err)
		return nil, err
	}

	logger.Info("Reports retrieved", "count", len(reports))
	return reports, nil
}

// Exists checks whether a dashboard template exists.
func (m *DashboardTemplateModel) Exists(ctx context.Context, id uuid.UUID) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("DashboardTemplateModel.Exists")

	if id == uuid.Nil {
		err := errors.New("template ID is required")
		logger.Error("Validation failed", err)
		return false, err
	}

	const query = `SELECT EXISTS (SELECT 1 FROM dashboard_templates WHERE id = $1)`

	var exists bool
	err := m.DB.QueryRow(ctx, query, id).Scan(&exists)
	if err != nil {
		logger.Error("Template existence check failed", err, "template_id", id)
		return false, err
	}

	return exists, nil
}

// AssignToUser idempotently assigns a dashboard template to a user.
func (m *DashboardTemplateModel) AssignToUser(ctx context.Context, templateID uuid.UUID, userID uuid.UUID) error {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("DashboardTemplateModel.AssignToUser")

	if templateID == uuid.Nil {
		err := errors.New("templateID must be a valid UUID")
		logger.Error("Validation failed", err)
		return err
	}
	if userID == uuid.Nil {
		err := errors.New("userID must be a valid UUID")
		logger.Error("Validation failed", err)
		return err
	}

	const stmt = `
		INSERT INTO user_dashboard_templates (id, user_id, dashboard_template_id)
		VALUES ($1, $2, $3)
		ON CONFLICT (user_id, dashboard_template_id) DO NOTHING
		RETURNING id
	`

	var assignmentID uuid.UUID
	err := m.DB.QueryRow(ctx, stmt, uuid.New(), userID, templateID).Scan(&assignmentID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			logger.Info("Dashboard template already assigned", "user_id", userID, "template_id", templateID)
			return nil
		}
		logger.Error("Assign template failed", err)
		return err
	}

	logger.Info("Dashboard template assigned", "assignment_id", assignmentID, "user_id", userID, "template_id", templateID)
	return nil
}