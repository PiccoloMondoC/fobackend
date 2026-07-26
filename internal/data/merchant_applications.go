// Package data provides models and database access methods for merchant applications and other entities.
//
// sdworkspace/sdbackend/internal/data/merchant_applications.go
//
// GTM:
//
//	Layer: 2.4 Merchant / Affiliate Domain
//	Release Class: DEFERRED
//	Reason:
//	  Merchant applications and merchant application statuses are valid
//	  self-service merchant onboarding infrastructure, but they are not required
//	  for the initial Platform release spine. The v1 spine requires
//	  merchant identity, merchant type classification, affiliate program catalog,
//	  and merchant-affiliate relationships before expanding into application
//	  workflow management.
//
// DEFERRED Rule:
//
//	Keep compiling.
//	Keep safe.
//	Preserve DB-owned lifecycle timestamps.
//	Preserve status workflow semantics.
//	Preserve soft-delete/deactivation distinctions.
//	Do not add new features.
//	Do not route into v1 UI/API expansion.
//	Do not block deployment on this file unless it breaks the build.
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

// ============================================================
// Domain types
// ============================================================

// MerchantApplication mirrors the merchant_applications schema row exactly.
// status_id is NOT NULL in the schema; the DB default resolves to the seeded
// "pending" status when the caller omits it on insert.
// applied_at and updated_at are DB-owned lifecycle timestamps; their values
// are always hydrated via RETURNING rather than written by the application.
type MerchantApplication struct {
	ID                 uuid.UUID  `json:"id"                   db:"id"`
	MerchantID         uuid.UUID  `json:"merchant_id"          db:"merchant_id"`
	AffiliateProgramID uuid.UUID  `json:"affiliate_program_id" db:"affiliate_program_id"`
	StatusID           uuid.UUID  `json:"status_id"            db:"status_id"`
	DeletedAt          *time.Time `json:"-"                    db:"deleted_at"`
	AppliedAt          time.Time  `json:"applied_at"           db:"applied_at"`
	UpdatedAt          time.Time  `json:"updated_at"           db:"updated_at"`
}

// MerchantApplicationStatus mirrors the merchant_application_status schema row
// exactly. The table is singular (merchant_application_status), not plural.
// is_active is the canonical lifecycle field; there is no deleted_at column.
type MerchantApplicationStatus struct {
	ID          uuid.UUID `json:"id"          db:"id"`
	Name        string    `json:"name"        db:"name"`
	Description string    `json:"description" db:"description"`
	IsActive    bool      `json:"is_active"   db:"is_active"`
	CreatedAt   time.Time `json:"created_at"  db:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"  db:"updated_at"`
}

// ============================================================
// Column lists — single source of truth for SELECT / RETURNING
// ============================================================

// merchantApplicationSelectColumns is the canonical ordered column list for
// merchant_applications queries. All Scan calls must match this order exactly.
const merchantApplicationSelectColumns = `id, merchant_id, affiliate_program_id, status_id, deleted_at, applied_at, updated_at`

// merchantApplicationStatusSelectColumns is the canonical ordered column list
// for merchant_application_status queries.
const merchantApplicationStatusSelectColumns = `
	id,
	name,
	description,
	is_active,
	created_at,
	updated_at
`

// ============================================================
// Normalisation helpers
// ============================================================

// normalizeMerchantApplicationStatusName trims leading/trailing whitespace from
// a status name. Status names are title-cased by convention (e.g. "Pending",
// "Approved"); no lowercase coercion is applied here because the schema has no
// lowercase trigger on merchant_application_status.name, unlike slug columns.
// Per BE Guidance V1.4 §2.2, this asymmetry with slug normalisation is deliberate.
func normalizeMerchantApplicationStatusName(name string) string {
	return strings.TrimSpace(name)
}

// ============================================================
// Model structs
// ============================================================

// MerchantApplicationModel holds the DB pool and logger for merchant_applications CRUD.
type MerchantApplicationModel struct {
	DB     *pgxpool.Pool
	Logger *logging.Logger
}

// MerchantApplicationStatusModel holds the DB pool and logger for
// merchant_application_status CRUD.
type MerchantApplicationStatusModel struct {
	DB     *pgxpool.Pool
	Logger *logging.Logger
}

// ============================================================
// MerchantApplicationModel — CRUD
// ============================================================

// Insert persists a new merchant application.
//
// When app.StatusID is uuid.Nil the INSERT omits the status_id column entirely,
// allowing the DB default (public.pending_merchant_app_status_id()) to resolve
// to the seeded "pending" status. When app.StatusID is explicitly set that
// value is persisted instead. Passing NULL explicitly would violate the NOT NULL
// constraint; omitting the column is the only correct way to trigger the default.
//
// applied_at and updated_at are DB-owned; their canonical values are hydrated
// from the RETURNING clause rather than written by the application.
func (m *MerchantApplicationModel) Insert(ctx context.Context, app *MerchantApplication) error {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("InsertMerchantApplication")

	if app == nil {
		err := errors.New("merchant application is required")
		logger.Error("Validation failed", err)
		return err
	}
	if app.MerchantID == uuid.Nil {
		err := errors.New("merchant_id is required")
		logger.Error("Validation failed", err)
		return err
	}
	if app.AffiliateProgramID == uuid.Nil {
		err := errors.New("affiliate_program_id is required")
		logger.Error("Validation failed", err)
		return err
	}

	if app.ID == uuid.Nil {
		app.ID = uuid.New()
	}

	var err error

	if app.StatusID == uuid.Nil {
		// Omit status_id; let the DB default resolve to the seeded "pending" status.
		query := fmt.Sprintf(`
			INSERT INTO merchant_applications (
				id,
				merchant_id,
				affiliate_program_id
			)
			VALUES ($1, $2, $3)
			RETURNING %s
		`, merchantApplicationSelectColumns)

		err = m.DB.QueryRow(ctx, query,
			app.ID,
			app.MerchantID,
			app.AffiliateProgramID,
		).Scan(
			&app.ID,
			&app.MerchantID,
			&app.AffiliateProgramID,
			&app.StatusID,
			&app.DeletedAt,
			&app.AppliedAt,
			&app.UpdatedAt,
		)
	} else {
		// Caller supplied an explicit status; persist it directly.
		query := fmt.Sprintf(`
			INSERT INTO merchant_applications (
				id,
				merchant_id,
				affiliate_program_id,
				status_id
			)
			VALUES ($1, $2, $3, $4)
			RETURNING %s
		`, merchantApplicationSelectColumns)

		err = m.DB.QueryRow(ctx, query,
			app.ID,
			app.MerchantID,
			app.AffiliateProgramID,
			app.StatusID,
		).Scan(
			&app.ID,
			&app.MerchantID,
			&app.AffiliateProgramID,
			&app.StatusID,
			&app.AppliedAt,
			&app.UpdatedAt,
		)
	}
	if err != nil {
		logger.Error("Insert merchant application failed", err)
		return err
	}

	logger.Info("Insert merchant application successful", "application_id", app.ID)
	return nil
}

// GetByID retrieves a merchant application by its primary key.
// Returns ErrMerchantApplicationNotFound when no row matches.
func (m *MerchantApplicationModel) GetByID(ctx context.Context, id uuid.UUID) (*MerchantApplication, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("GetMerchantApplicationByID")

	if id == uuid.Nil {
		err := errors.New("application_id is required")
		logger.Error("Validation failed", err)
		return nil, err
	}

	query := fmt.Sprintf(`
		SELECT %s
		FROM merchant_applications
		WHERE id = $1
			AND deleted_at IS NULL
	`, merchantApplicationSelectColumns)

	var app MerchantApplication

	err := m.DB.QueryRow(ctx, query, id).Scan(
		&app.ID,
		&app.MerchantID,
		&app.AffiliateProgramID,
		&app.StatusID,
		&app.DeletedAt,
		&app.AppliedAt,
		&app.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			logger.Warn("Merchant application not found", "application_id", id)
			return nil, ErrMerchantApplicationNotFound
		}
		logger.Error("Query failed", err)
		return nil, err
	}

	logger.Info("Retrieved merchant application", "application_id", app.ID)
	return &app, nil
}

// GetByMerchantID retrieves all merchant applications for a given merchant,
// ordered by applied_at descending.
func (m *MerchantApplicationModel) GetByMerchantID(ctx context.Context, merchantID uuid.UUID) ([]*MerchantApplication, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("GetByMerchantID")

	if merchantID == uuid.Nil {
		err := errors.New("merchant_id is required")
		logger.Error("Validation failed", err)
		return nil, err
	}

	query := fmt.Sprintf(`
		SELECT %s
		FROM merchant_applications
		WHERE merchant_id = $1
      		AND deleted_at IS NULL
		ORDER BY applied_at DESC
	`, merchantApplicationSelectColumns)

	rows, err := m.DB.Query(ctx, query, merchantID)
	if err != nil {
		logger.Error("Query failed", err)
		return nil, err
	}
	defer rows.Close()

	var applications []*MerchantApplication
	for rows.Next() {
		var app MerchantApplication
		err = rows.Scan(
			&app.ID,
			&app.MerchantID,
			&app.AffiliateProgramID,
			&app.StatusID,
			&app.DeletedAt,
			&app.AppliedAt,
			&app.UpdatedAt,
		)
		if err != nil {
			logger.Error("Row scan failed", err)
			return nil, err
		}
		applications = append(applications, &app)
	}

	if err = rows.Err(); err != nil {
		logger.Error("Row iteration error", err)
		return nil, err
	}

	logger.Info("Retrieved merchant applications", "merchant_id", merchantID, "count", len(applications))
	return applications, nil
}

// GetByAffiliateProgramID retrieves merchant applications for a given affiliate
// program, optionally filtered by status, with bounded limit/offset pagination.
//
// Pass uuid.Nil for statusID to skip the status filter.
// limit is clamped to [1, 100]; offset is clamped to >= 0.
func (m *MerchantApplicationModel) GetByAffiliateProgramID(
	ctx context.Context,
	affiliateProgramID uuid.UUID,
	statusID uuid.UUID,
	limit, offset int,
) ([]*MerchantApplication, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("GetByAffiliateProgramID")

	if affiliateProgramID == uuid.Nil {
		err := errors.New("affiliate_program_id is required")
		logger.Error("Validation failed", err)
		return nil, err
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

	query := fmt.Sprintf(`
		SELECT %s
		FROM merchant_applications
		WHERE affiliate_program_id = $1
			AND deleted_at IS NULL
	`, merchantApplicationSelectColumns)

	args := []interface{}{affiliateProgramID}
	argIndex := 2

	if statusID != uuid.Nil {
		query += fmt.Sprintf(" AND status_id = $%d", argIndex)
		args = append(args, statusID)
		argIndex++
	}

	query += fmt.Sprintf(" ORDER BY applied_at DESC LIMIT $%d OFFSET $%d", argIndex, argIndex+1)
	args = append(args, limit, offset)

	rows, err := m.DB.Query(ctx, query, args...)
	if err != nil {
		logger.Error("Query failed", err)
		return nil, err
	}
	defer rows.Close()

	var applications []*MerchantApplication
	for rows.Next() {
		var app MerchantApplication
		err = rows.Scan(
			&app.ID,
			&app.MerchantID,
			&app.AffiliateProgramID,
			&app.StatusID,
			&app.DeletedAt,
			&app.AppliedAt,
			&app.UpdatedAt,
		)
		if err != nil {
			logger.Error("Row scan failed", err)
			return nil, err
		}
		applications = append(applications, &app)
	}

	if err = rows.Err(); err != nil {
		logger.Error("Row iteration error", err)
		return nil, err
	}

	logger.Info("Retrieved merchant applications",
		"count", len(applications),
		"affiliate_program_id", affiliateProgramID,
	)
	return applications, nil
}

// GetByStatusID retrieves merchant applications by status with bounded
// limit/offset pagination.
// limit is clamped to [1, 100]; offset is clamped to >= 0.
func (m *MerchantApplicationModel) GetByStatusID(
	ctx context.Context,
	statusID uuid.UUID,
	limit, offset int,
) ([]*MerchantApplication, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("GetByStatusID")

	if statusID == uuid.Nil {
		err := errors.New("status_id is required")
		logger.Error("Validation failed", err)
		return nil, err
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

	query := fmt.Sprintf(`
		SELECT %s
		FROM merchant_applications
		WHERE status_id = $1
			AND deleted_at IS NULL
		ORDER BY applied_at DESC
		LIMIT $2 OFFSET $3
	`, merchantApplicationSelectColumns)

	rows, err := m.DB.Query(ctx, query, statusID, limit, offset)
	if err != nil {
		logger.Error("Query failed", err)
		return nil, err
	}
	defer rows.Close()

	var results []*MerchantApplication
	for rows.Next() {
		var a MerchantApplication
		err = rows.Scan(
			&a.ID,
			&a.MerchantID,
			&a.AffiliateProgramID,
			&a.StatusID,
			&a.DeletedAt,
			&a.AppliedAt,
			&a.UpdatedAt,
		)
		if err != nil {
			logger.Error("Row scan failed", err)
			return nil, err
		}
		results = append(results, &a)
	}

	if err = rows.Err(); err != nil {
		logger.Error("Row iteration error", err)
		return nil, err
	}

	logger.Info("GetByStatusID successful", "status_id", statusID, "count", len(results))
	return results, nil
}

// GetAll retrieves all merchant applications ordered by applied_at descending.
// Intended for administrative use only. Callers requiring pagination should
// prefer GetByAffiliateProgramID or GetByStatusID. This method must be gated
// behind an admin role check at the handler layer before exposure via any API.
func (m *MerchantApplicationModel) GetAll(ctx context.Context) ([]*MerchantApplication, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("GetAllMerchantApplications")

	query := fmt.Sprintf(`
		SELECT %s
		FROM merchant_applications
		WHERE deleted_at IS NULL
		ORDER BY applied_at DESC
	`, merchantApplicationSelectColumns)

	rows, err := m.DB.Query(ctx, query)
	if err != nil {
		logger.Error("Query failed", err)
		return nil, err
	}
	defer rows.Close()

	var applications []*MerchantApplication
	for rows.Next() {
		var app MerchantApplication
		err = rows.Scan(
			&app.ID,
			&app.MerchantID,
			&app.AffiliateProgramID,
			&app.StatusID,
			&app.DeletedAt,
			&app.AppliedAt,
			&app.UpdatedAt,
		)
		if err != nil {
			logger.Error("Row scan failed", err)
			return nil, err
		}
		applications = append(applications, &app)
	}

	if err = rows.Err(); err != nil {
		logger.Error("Row iteration failed", err)
		return nil, err
	}

	logger.Info("Retrieved merchant applications", "count", len(applications))
	return applications, nil
}

// Update fully replaces the mutable fields of an existing merchant application.
// All three mutable fields (merchant_id, affiliate_program_id, status_id) are
// required. updated_at is DB-owned and is re-hydrated via RETURNING.
// Returns ErrMerchantApplicationNotFound when no row matches app.ID.
func (m *MerchantApplicationModel) Update(ctx context.Context, app *MerchantApplication) error {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("UpdateMerchantApplication")

	if app == nil {
		err := errors.New("merchant application is required")
		logger.Error("Validation failed", err)
		return err
	}
	if app.ID == uuid.Nil {
		err := errors.New("id is required")
		logger.Error("Validation failed", err)
		return err
	}
	if app.MerchantID == uuid.Nil {
		err := errors.New("merchant_id is required")
		logger.Error("Validation failed", err)
		return err
	}
	if app.AffiliateProgramID == uuid.Nil {
		err := errors.New("affiliate_program_id is required")
		logger.Error("Validation failed", err)
		return err
	}
	if app.StatusID == uuid.Nil {
		err := errors.New("status_id is required")
		logger.Error("Validation failed", err)
		return err
	}

	// updated_at is owned by the set_updated_at trigger; we do not write it.
	// RETURNING re-hydrates the canonical value from the DB after the trigger fires.
	query := fmt.Sprintf(`
		UPDATE merchant_applications
		SET merchant_id          = $1,
		    affiliate_program_id = $2,
		    status_id            = $3
		WHERE id = $4
			AND deleted_at IS NULL
		RETURNING %s
	`, merchantApplicationSelectColumns)

	err := m.DB.QueryRow(ctx, query,
		app.MerchantID,
		app.AffiliateProgramID,
		app.StatusID,
		app.ID,
	).Scan(
		&app.ID,
		&app.MerchantID,
		&app.AffiliateProgramID,
		&app.StatusID,
		&app.DeletedAt,
		&app.AppliedAt,
		&app.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			logger.Warn("Merchant application not found", "application_id", app.ID)
			return ErrMerchantApplicationNotFound
		}
		logger.Error("Update merchant application failed", err)
		return err
	}

	logger.Info("Update merchant application successful", "application_id", app.ID)
	return nil
}

// SoftDelete logically removes a merchant application by setting deleted_at.
//
// Workflow status remains an independent concern and must not be overloaded to
// stand in for deletion. Rejection, approval, or withdrawal must be modeled
// through status transitions on status_id; logical removal is modeled through
// deleted_at. Standard reads exclude soft-deleted rows by default.
//
// Returns ErrMerchantApplicationNotFound when no active row matches.
func (m *MerchantApplicationModel) SoftDelete(ctx context.Context, applicationID uuid.UUID) error {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("SoftDeleteMerchantApplication")

	if applicationID == uuid.Nil {
		err := errors.New("application_id is required")
		logger.Error("Validation failed", err)
		return err
	}

	var deletedAt time.Time
	err := m.DB.QueryRow(ctx, `
		UPDATE merchant_applications
		SET deleted_at = NOW()
		WHERE id = $1
		  AND deleted_at IS NULL
		RETURNING deleted_at
	`, applicationID).Scan(&deletedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			logger.Warn("Merchant application not found", "application_id", applicationID)
			return ErrMerchantApplicationNotFound
		}
		logger.Error("Soft delete merchant application failed", err)
		return err
	}

	logger.Info(
		"Soft delete merchant application successful",
		"application_id", applicationID,
		"deleted_at", deletedAt,
	)
	return nil
}

// Delete permanently removes a merchant application.
//
// This is the physical purge path. It must remain a true hard delete and must
// not become an alias for SoftDelete(). Because both FK references use
// ON DELETE RESTRICT, this will fail if dependent rows exist in other tables.
//
// Returns ErrMerchantApplicationNotFound when no row exists for the supplied ID.
func (m *MerchantApplicationModel) Delete(ctx context.Context, applicationID uuid.UUID) error {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("DeleteMerchantApplication")

	if applicationID == uuid.Nil {
		err := errors.New("application_id is required")
		logger.Error("Validation failed", err)
		return err
	}

	var deletedID uuid.UUID
	err := m.DB.QueryRow(ctx, `
		DELETE FROM merchant_applications
		WHERE id = $1
		RETURNING id
	`, applicationID).Scan(&deletedID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			logger.Warn("Merchant application not found", "application_id", applicationID)
			return ErrMerchantApplicationNotFound
		}
		logger.Error("Delete merchant application failed", err)
		return err
	}

	logger.Info("Delete merchant application successful", "application_id", deletedID)
	return nil
}

// ============================================================
// MerchantApplicationStatusModel — CRUD
// ============================================================

// Insert persists a new merchant application status.
// Name is trimmed of whitespace before validation and persistence.
// created_at and updated_at are DB-owned and are hydrated via RETURNING.
func (m *MerchantApplicationStatusModel) Insert(ctx context.Context, status *MerchantApplicationStatus) error {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("InsertMerchantApplicationStatus")

	if status == nil {
		err := errors.New("merchant application status is required")
		logger.Error("Validation failed", err)
		return err
	}

	status.Name = normalizeMerchantApplicationStatusName(status.Name)

	if status.Name == "" {
		err := errors.New("status name is required")
		logger.Error("Validation failed", err)
		return err
	}
	if status.Description == "" {
		err := errors.New("status description is required")
		logger.Error("Validation failed", err)
		return err
	}

	if status.ID == uuid.Nil {
		status.ID = uuid.New()
	}

	// Table name: merchant_application_status (singular — matches schema DDL exactly).
	query := fmt.Sprintf(`
		INSERT INTO merchant_application_status (
			id,
			name,
			description,
			is_active
		)
		VALUES ($1, $2, $3, $4)
		RETURNING %s
	`, merchantApplicationStatusSelectColumns)

	err := m.DB.QueryRow(ctx, query,
		status.ID,
		status.Name,
		status.Description,
		status.IsActive,
	).Scan(
		&status.ID,
		&status.Name,
		&status.Description,
		&status.IsActive,
		&status.CreatedAt,
		&status.UpdatedAt,
	)
	if err != nil {
		logger.Error("Insert merchant application status failed", err)
		return err
	}

	logger.Info("Insert merchant application status successful", "status_id", status.ID)
	return nil
}

// GetByID retrieves a merchant application status by its primary key.
// Returns ErrMerchantApplicationStatusNotFound when no row matches.
func (m *MerchantApplicationStatusModel) GetByID(ctx context.Context, id uuid.UUID) (*MerchantApplicationStatus, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("GetMerchantApplicationStatusByID")

	if id == uuid.Nil {
		err := errors.New("status_id is required")
		logger.Error("Validation failed", err)
		return nil, err
	}

	// Table name: merchant_application_status (singular).
	query := fmt.Sprintf(`
		SELECT %s
		FROM merchant_application_status
		WHERE id = $1
	`, merchantApplicationStatusSelectColumns)

	var status MerchantApplicationStatus

	err := m.DB.QueryRow(ctx, query, id).Scan(
		&status.ID,
		&status.Name,
		&status.Description,
		&status.IsActive,
		&status.CreatedAt,
		&status.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			logger.Warn("Status not found", "status_id", id)
			return nil, ErrMerchantApplicationStatusNotFound
		}
		logger.Error("Query failed", err)
		return nil, err
	}

	logger.Info("Retrieved merchant application status", "status_id", status.ID)
	return &status, nil
}

// GetByName retrieves a merchant application status by name (case-insensitive).
// Input is trimmed before querying. Returns ErrMerchantApplicationStatusNotFound
// when no row matches.
func (m *MerchantApplicationStatusModel) GetByName(ctx context.Context, name string) (*MerchantApplicationStatus, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("GetMerchantApplicationStatusByName")

	name = normalizeMerchantApplicationStatusName(name)
	if name == "" {
		err := errors.New("status name is required")
		logger.Error("Validation failed", err)
		return nil, err
	}

	// LOWER(name) = LOWER($1) provides case-insensitive equality without requiring
	// the caller to know the stored casing convention (title-case by convention).
	query := fmt.Sprintf(`
		SELECT %s
		FROM merchant_application_status
		WHERE LOWER(name) = LOWER($1)
	`, merchantApplicationStatusSelectColumns)

	var status MerchantApplicationStatus

	err := m.DB.QueryRow(ctx, query, name).Scan(
		&status.ID,
		&status.Name,
		&status.Description,
		&status.IsActive,
		&status.CreatedAt,
		&status.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			logger.Warn("Status not found", "name", name)
			return nil, ErrMerchantApplicationStatusNotFound
		}
		logger.Error("Get merchant application status by name failed", err)
		return nil, err
	}

	logger.Info("Get merchant application status by name successful", "status_id", status.ID)
	return &status, nil
}

// GetAll retrieves all merchant application statuses ordered by created_at ASC,
// then name ASC as a tiebreaker. Returns all statuses regardless of is_active
// state; intended for administrative use only.
func (m *MerchantApplicationStatusModel) GetAll(ctx context.Context) ([]*MerchantApplicationStatus, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("GetAllMerchantApplicationStatuses")

	query := fmt.Sprintf(`
		SELECT %s
		FROM merchant_application_status
		ORDER BY created_at ASC, name ASC
	`, merchantApplicationStatusSelectColumns)

	rows, err := m.DB.Query(ctx, query)
	if err != nil {
		logger.Error("Query execution failed", err)
		return nil, err
	}
	defer rows.Close()

	var statuses []*MerchantApplicationStatus
	for rows.Next() {
		var status MerchantApplicationStatus
		err = rows.Scan(
			&status.ID,
			&status.Name,
			&status.Description,
			&status.IsActive,
			&status.CreatedAt,
			&status.UpdatedAt,
		)
		if err != nil {
			logger.Error("Row scan failed", err)
			return nil, err
		}
		statuses = append(statuses, &status)
	}

	if err = rows.Err(); err != nil {
		logger.Error("Row iteration error", err)
		return nil, err
	}

	logger.Info("GetAll merchant application statuses successful", "count", len(statuses))
	return statuses, nil
}

// GetAllWithFilter returns a paginated list of merchant application statuses,
// optionally filtered by a case-insensitive partial name match and, when
// requested, limited to active rows only.
//
// Pass an empty string for name to skip the name filter.
// Pass activeOnly=true to exclude inactive statuses; false returns all.
// limit is clamped to [1, 100]; offset is clamped to >= 0.
func (m *MerchantApplicationStatusModel) GetAllWithFilter(
	ctx context.Context,
	name string,
	activeOnly bool,
	limit, offset int,
) ([]*MerchantApplicationStatus, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("GetAllWithFilter")

	name = normalizeMerchantApplicationStatusName(name)

	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	if offset < 0 {
		offset = 0
	}

	// WHERE 1=1 allows conditions to be appended uniformly without special-casing
	// the first predicate. activeOnly uses a literal rather than a parameter because
	// TRUE is not user input and parameterising it adds noise without benefit.
	query := fmt.Sprintf(`
		SELECT %s
		FROM merchant_application_status
		WHERE 1=1
	`, merchantApplicationStatusSelectColumns)

	args := make([]interface{}, 0, 4)
	argIndex := 1

	if name != "" {
		query += fmt.Sprintf(" AND LOWER(name) LIKE LOWER($%d)", argIndex)
		args = append(args, "%"+name+"%")
		argIndex++
	}

	if activeOnly {
		query += " AND is_active = TRUE"
	}

	query += fmt.Sprintf(`
		ORDER BY created_at ASC, name ASC
		LIMIT $%d OFFSET $%d
	`, argIndex, argIndex+1)
	args = append(args, limit, offset)

	rows, err := m.DB.Query(ctx, query, args...)
	if err != nil {
		logger.Error("Query execution failed", err)
		return nil, err
	}
	defer rows.Close()

	var statuses []*MerchantApplicationStatus
	for rows.Next() {
		var status MerchantApplicationStatus
		err = rows.Scan(
			&status.ID,
			&status.Name,
			&status.Description,
			&status.IsActive,
			&status.CreatedAt,
			&status.UpdatedAt,
		)
		if err != nil {
			logger.Error("Row scan failed", err)
			return nil, err
		}
		statuses = append(statuses, &status)
	}

	if err = rows.Err(); err != nil {
		logger.Error("Row iteration error", err)
		return nil, err
	}

	logger.Info("Fetched application statuses",
		"count", len(statuses),
		"filter", name,
		"active_only", activeOnly,
	)
	return statuses, nil
}

// Update fully replaces the mutable fields of an existing merchant application
// status. All mutable fields (name, description, is_active) must be supplied.
// updated_at is DB-owned and is re-hydrated via RETURNING.
// Returns ErrMerchantApplicationStatusNotFound when no row matches status.ID.
func (m *MerchantApplicationStatusModel) Update(ctx context.Context, status *MerchantApplicationStatus) error {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("UpdateMerchantApplicationStatus")

	if status == nil {
		err := errors.New("merchant application status is required")
		logger.Error("Validation failed", err)
		return err
	}
	if status.ID == uuid.Nil {
		err := errors.New("status_id is required")
		logger.Error("Validation failed", err)
		return err
	}

	status.Name = normalizeMerchantApplicationStatusName(status.Name)

	if status.Name == "" {
		err := errors.New("status name is required")
		logger.Error("Validation failed", err)
		return err
	}
	if status.Description == "" {
		err := errors.New("status description is required")
		logger.Error("Validation failed", err)
		return err
	}

	// updated_at is owned by the set_updated_at trigger; we do not write it.
	// RETURNING re-hydrates the canonical value after the trigger fires.
	query := fmt.Sprintf(`
		UPDATE merchant_application_status
		SET name        = $1,
		    description = $2,
		    is_active   = $3
		WHERE id = $4
		RETURNING %s
	`, merchantApplicationStatusSelectColumns)

	err := m.DB.QueryRow(ctx, query,
		status.Name,
		status.Description,
		status.IsActive,
		status.ID,
	).Scan(
		&status.ID,
		&status.Name,
		&status.Description,
		&status.IsActive,
		&status.CreatedAt,
		&status.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			logger.Warn("Merchant application status not found", "status_id", status.ID)
			return ErrMerchantApplicationStatusNotFound
		}
		logger.Error("Update merchant application status failed", err)
		return err
	}

	logger.Info("Update merchant application status successful", "status_id", status.ID)
	return nil
}

// SoftDelete deactivates a merchant application status by setting is_active = false.
//
// For merchant_application_status, is_active is the canonical lifecycle field.
// This table does not use deleted_at; the reference vocabulary is not temporally
// soft-deleted but deactivated. The method name SoftDelete is retained for
// compatibility with the existing action/permission naming contract
// (soft_delete_merchant_application_status), but the underlying semantic is
// deactivation, not deleted_at-based temporal soft delete.
//
// Returns:
//   - ErrMerchantApplicationStatusNotFound when no row matches statusID
//   - ErrMerchantApplicationStatusAlreadyInactive when the row exists but is already inactive
//   - nil on successful deactivation
func (m *MerchantApplicationStatusModel) SoftDelete(ctx context.Context, statusID uuid.UUID) error {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("SoftDeleteMerchantApplicationStatus")

	if statusID == uuid.Nil {
		err := errors.New("status_id is required")
		logger.Error("Validation failed", err)
		return err
	}

	// Step 1: resolve existence and current lifecycle state in one read.
	// This separates "row absent" from "row present but already inactive" so the
	// caller receives the correct sentinel rather than an ambiguous not-found error.
	var isActive bool
	checkQuery := `
		SELECT is_active
		FROM merchant_application_status
		WHERE id = $1
	`

	err := m.DB.QueryRow(ctx, checkQuery, statusID).Scan(&isActive)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			logger.Warn("Merchant application status not found", "status_id", statusID)
			return ErrMerchantApplicationStatusNotFound
		}
		logger.Error("Read merchant application status before deactivation failed", err)
		return err
	}

	// Step 2: guard against redundant deactivation before issuing the UPDATE.
	if !isActive {
		logger.Warn("Merchant application status already inactive", "status_id", statusID)
		return ErrMerchantApplicationStatusAlreadyInactive
	}

	// Step 3: deactivate. The set_updated_at trigger fires on this UPDATE.
	updateQuery := `
		UPDATE merchant_application_status
		SET is_active = FALSE
		WHERE id = $1
	`

	_, err = m.DB.Exec(ctx, updateQuery, statusID)
	if err != nil {
		logger.Error("Deactivate merchant application status failed", err)
		return err
	}

	logger.Info("Merchant application status deactivated successfully", "status_id", statusID)
	return nil
}
