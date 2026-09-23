// Package data provides models and database access methods for departments,
// categories, and other entities.
//
// focodebase/fobackend/internal/data/departments.go
//
// GTM:
//
//	Layer: 2.5 Catalog / Offer Domain
//	Release Class: SPINE
//	Reason:
//	  Departments and categories are release-critical catalog taxonomy
//	  infrastructure. They define the department-first navigation model,
//	  category hierarchy, offer classification, slug-based routing, and
//	  soft-delete lifecycle used by the public offer experience.
//
// SPINE Rule:
//
//	Keep compiling.
//	Keep production-ready.
//	Preserve department-first taxonomy behavior.
//	Preserve category hierarchy and parent-child lookup semantics.
//	Preserve slug-based lookup behavior.
//	Preserve soft-delete lifecycle semantics.
//	Block deployment if this file breaks build, taxonomy persistence,
//	offer classification, department-first routing, or catalog integrity.
package data

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/PiccoloMondoC/focodebase/fobackend/internal/logging"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	departmentSelectColumns = `id, name, slug, description, sort_order, deleted_at, created_at, updated_at`

	categorySelectColumns = `id, name, slug, department_id, parent_id, sort_order, description, deleted_at, created_at, updated_at`
)

// rowScanner is implemented by pgx.Row and pgx.Rows for shared scan helpers.
type rowScanner interface {
	Scan(dest ...any) error
}

// Department represents a merchandising department.
type Department struct {
	ID          uuid.UUID  `json:"id"                    db:"id"`
	Name        string     `json:"name"                  db:"name"`
	Slug        string     `json:"slug"                  db:"slug"`
	Description *string    `json:"description,omitempty" db:"description"`
	SortOrder   int        `json:"sort_order"            db:"sort_order"`
	DeletedAt   *time.Time `json:"-"                     db:"deleted_at"`
	CreatedAt   time.Time  `json:"created_at"            db:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"            db:"updated_at"`
}

// Category represents a category within the department-rooted taxonomy.
type Category struct {
	ID           uuid.UUID  `json:"id"                    db:"id"`
	Name         string     `json:"name"                  db:"name"`
	Slug         string     `json:"slug"                  db:"slug"`
	DepartmentID uuid.UUID  `json:"department_id"         db:"department_id"`
	ParentID     *uuid.UUID `json:"parent_id,omitempty"   db:"parent_id"`
	SortOrder    int        `json:"sort_order"            db:"sort_order"`
	Description  *string    `json:"description,omitempty" db:"description"`
	DeletedAt    *time.Time `json:"-"                     db:"deleted_at"`
	CreatedAt    time.Time  `json:"created_at"            db:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"            db:"updated_at"`
}

// DepartmentModel holds the database pool and logger for department operations.
type DepartmentModel struct {
	DB     *pgxpool.Pool
	Logger *logging.Logger
}

// CategoryModel holds the database pool and logger for category operations.
type CategoryModel struct {
	DB     *pgxpool.Pool
	Logger *logging.Logger
}

// scanDepartment scans a department from a pgx row/result into the target struct.
func scanDepartment(s rowScanner, department *Department) error {
	return s.Scan(
		&department.ID,
		&department.Name,
		&department.Slug,
		&department.Description,
		&department.SortOrder,
		&department.DeletedAt,
		&department.CreatedAt,
		&department.UpdatedAt,
	)
}

// scanCategory scans a category from a pgx row/result into the target struct.
func scanCategory(s rowScanner, category *Category) error {
	return s.Scan(
		&category.ID,
		&category.Name,
		&category.Slug,
		&category.DepartmentID,
		&category.ParentID,
		&category.SortOrder,
		&category.Description,
		&category.DeletedAt,
		&category.CreatedAt,
		&category.UpdatedAt,
	)
}

// validateDepartment enforces shared application-layer invariants before persistence.
func validateDepartment(department *Department) error {
	if department == nil {
		return errors.New("department is required")
	}

	if strings.TrimSpace(department.Name) == "" {
		return errors.New("department name is required")
	}

	if strings.TrimSpace(department.Slug) == "" {
		return errors.New("department slug is required")
	}

	return nil
}

// validateDepartmentForUpdate enforces update-specific invariants.
func validateDepartmentForUpdate(department *Department) error {
	if err := validateDepartment(department); err != nil {
		return err
	}

	if department.ID == uuid.Nil {
		return errors.New("department ID is required")
	}

	return nil
}

// validateCategory enforces shared application-layer invariants before persistence.
func validateCategory(category *Category) error {
	if category == nil {
		return errors.New("category is required")
	}

	if strings.TrimSpace(category.Name) == "" {
		return errors.New("category name is required")
	}

	if strings.TrimSpace(category.Slug) == "" {
		return errors.New("category slug is required")
	}

	if category.DepartmentID == uuid.Nil {
		return errors.New("department_id is required")
	}

	return nil
}

// validateCategoryForUpdate enforces update-specific invariants.
func validateCategoryForUpdate(category *Category) error {
	if err := validateCategory(category); err != nil {
		return err
	}

	if category.ID == uuid.Nil {
		return errors.New("category ID is required")
	}

	return nil
}

// Insert inserts a new department and returns the canonical DB-owned row state.
func (m *DepartmentModel) Insert(ctx context.Context, department *Department) error {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("InsertDepartment")

	if err := validateDepartment(department); err != nil {
		logger.Error("Department validation failed", "error", err)
		return err
	}

	if department.ID == uuid.Nil {
		department.ID = uuid.New()
	}

	query := `
		INSERT INTO departments (
			id,
			name,
			slug,
			description,
			sort_order
		)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING ` + departmentSelectColumns

	err := scanDepartment(
		m.DB.QueryRow(
			ctx,
			query,
			department.ID,
			department.Name,
			department.Slug,
			department.Description,
			department.SortOrder,
		),
		department,
	)
	if err != nil {
		logger.Error("Insert department failed", "error", err)
		return err
	}

	logger.Info(
		"Insert department successful",
		"department_id", department.ID,
		"slug", department.Slug,
	)

	return nil
}

// GetByID retrieves a department by its ID.
func (m *DepartmentModel) GetByID(ctx context.Context, id uuid.UUID) (*Department, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("GetDepartmentByID")

	if id == uuid.Nil {
		err := errors.New("invalid department ID")
		logger.Error("Department validation failed", "error", err)
		return nil, err
	}

	var department Department

	query := `SELECT ` + departmentSelectColumns + ` FROM departments WHERE id = $1 AND deleted_at IS NULL`

	err := scanDepartment(m.DB.QueryRow(ctx, query, id), &department)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			logger.Warn("Department not found", "department_id", id)
			return nil, ErrDepartmentNotFound
		}

		logger.Error("Get department by ID failed", "error", err, "department_id", id)
		return nil, err
	}

	logger.Info("Get department by ID successful", "department_id", department.ID)

	return &department, nil
}

// GetBySlug retrieves a department by its canonical slug.
func (m *DepartmentModel) GetBySlug(ctx context.Context, slug string) (*Department, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("GetDepartmentBySlug")

	slug = strings.ToLower(strings.TrimSpace(slug))
	if slug == "" {
		err := errors.New("department slug is required")
		logger.Error("Department validation failed", "error", err)
		return nil, err
	}

	var department Department

	query := `SELECT ` + departmentSelectColumns + ` FROM departments WHERE slug = $1 AND deleted_at IS NULL`

	err := scanDepartment(m.DB.QueryRow(ctx, query, slug), &department)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			logger.Warn("Department not found", "slug", slug)
			return nil, ErrDepartmentNotFound
		}

		logger.Error("Get department by slug failed", "error", err, "slug", slug)
		return nil, err
	}

	logger.Info("Get department by slug successful", "department_id", department.ID, "slug", slug)

	return &department, nil
}

// GetAll retrieves all departments in stable presentation order.
func (m *DepartmentModel) GetAll(ctx context.Context) ([]*Department, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("GetAllDepartments")

	query := `
		SELECT ` + departmentSelectColumns + `
		FROM departments
		WHERE deleted_at IS NULL
		ORDER BY sort_order ASC, name ASC, id ASC
	`

	rows, err := m.DB.Query(ctx, query)
	if err != nil {
		logger.Error("Get all departments query failed", "error", err)
		return nil, err
	}
	defer rows.Close()

	departments := make([]*Department, 0)

	for rows.Next() {
		var department Department

		if err := scanDepartment(rows, &department); err != nil {
			logger.Error("Department row scan failed", "error", err)
			return nil, err
		}

		departments = append(departments, &department)
	}

	if err := rows.Err(); err != nil {
		logger.Error("Department row iteration failed", "error", err)
		return nil, err
	}

	logger.Info("Get all departments successful", "count", len(departments))

	return departments, nil
}

// Update updates a department and returns the canonical DB-owned row state.
func (m *DepartmentModel) Update(ctx context.Context, department *Department) error {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("UpdateDepartment")

	if err := validateDepartmentForUpdate(department); err != nil {
		logger.Error("Department validation failed", "error", err)
		return err
	}

	query := `
		UPDATE departments
		SET
			name = $1,
			slug = $2,
			description = $3,
			sort_order = $4
		WHERE id = $5
			AND deleted_at IS NULL
		RETURNING ` + departmentSelectColumns

	err := scanDepartment(
		m.DB.QueryRow(
			ctx,
			query,
			department.Name,
			department.Slug,
			department.Description,
			department.SortOrder,
			department.ID,
		),
		department,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			logger.Warn("Department not found during update", "department_id", department.ID)
			return ErrDepartmentNotFound
		}

		logger.Error("Update department failed", "error", err, "department_id", department.ID)
		return err
	}

	logger.Info("Update department successful", "department_id", department.ID)

	return nil
}

// SoftDelete logically removes a department by setting deleted_at.
// This is the canonical business-lifecycle removal path for taxonomy roots.
//
// Departments are seeded root taxonomy structures referenced throughout
// categories and offers. Hard-deleting a department cascades to categories,
// which is far too destructive for a retention-sensitive taxonomy vocabulary.
// Standard removal must use SoftDelete(); Delete() is the physical purge path.
//
// Returns ErrDepartmentNotFound when no active row matches the supplied ID.
func (m *DepartmentModel) SoftDelete(ctx context.Context, id uuid.UUID) error {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("SoftDeleteDepartment")

	if id == uuid.Nil {
		err := errors.New("invalid department ID")
		logger.Error("Department validation failed", "error", err)
		return err
	}

	var deletedAt time.Time
	err := m.DB.QueryRow(ctx, `
		UPDATE departments
		SET deleted_at = NOW()
		WHERE id = $1
		  AND deleted_at IS NULL
		RETURNING deleted_at
	`, id).Scan(&deletedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			logger.Warn("Department not found during soft delete", "department_id", id)
			return ErrDepartmentNotFound
		}
		logger.Error("Soft delete department failed", "error", err, "department_id", id)
		return err
	}

	logger.Info("Soft delete department successful", "department_id", id, "deleted_at", deletedAt)
	return nil
}

// Delete permanently removes a department from the database.
// This is the physical purge path and must remain a true hard delete.
// It must not be used as the ordinary removal path; callers should use SoftDelete().
//
// Returns ErrDepartmentNotFound when no row exists for the supplied ID.
func (m *DepartmentModel) Delete(ctx context.Context, id uuid.UUID) error {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("DeleteDepartment")

	if id == uuid.Nil {
		err := errors.New("invalid department ID")
		logger.Error("Department validation failed", "error", err)
		return err
	}

	var deletedID uuid.UUID
	err := m.DB.QueryRow(ctx, `
		DELETE FROM departments
		WHERE id = $1
		RETURNING id
	`, id).Scan(&deletedID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			logger.Warn("Department not found during delete", "department_id", id)
			return ErrDepartmentNotFound
		}
		logger.Error("Delete department failed", "error", err, "department_id", id)
		return err
	}

	logger.Info("Delete department successful", "department_id", deletedID)
	return nil
}

// Insert inserts a new category and returns the canonical DB-owned row state.
func (m *CategoryModel) Insert(ctx context.Context, category *Category) error {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("InsertCategory")

	if err := validateCategory(category); err != nil {
		logger.Error("Category validation failed", "error", err)
		return err
	}

	if category.ID == uuid.Nil {
		category.ID = uuid.New()
	}

	query := `
		INSERT INTO categories (
			id,
			name,
			slug,
			department_id,
			parent_id,
			sort_order,
			description
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING ` + categorySelectColumns

	err := scanCategory(
		m.DB.QueryRow(
			ctx,
			query,
			category.ID,
			category.Name,
			category.Slug,
			category.DepartmentID,
			category.ParentID,
			category.SortOrder,
			category.Description,
		),
		category,
	)
	if err != nil {
		logger.Error("Insert category failed", "error", err)
		return err
	}

	logger.Info(
		"Insert category successful",
		"category_id", category.ID,
		"department_id", category.DepartmentID,
		"slug", category.Slug,
	)

	return nil
}

// GetByID retrieves a category by its ID.
func (m *CategoryModel) GetByID(ctx context.Context, id uuid.UUID) (*Category, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("GetCategoryByID")

	if id == uuid.Nil {
		err := errors.New("invalid category ID")
		logger.Error("Category validation failed", "error", err)
		return nil, err
	}

	var category Category

	query := `SELECT ` + categorySelectColumns + ` FROM categories WHERE id = $1 AND deleted_at IS NULL`

	err := scanCategory(m.DB.QueryRow(ctx, query, id), &category)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			logger.Warn("Category not found", "category_id", id)
			return nil, ErrCategoryNotFound
		}

		logger.Error("Get category by ID failed", "error", err, "category_id", id)
		return nil, err
	}

	logger.Info("Get category by ID successful", "category_id", category.ID)

	return &category, nil
}

// GetBySlug retrieves a category by its canonical slug.
func (m *CategoryModel) GetBySlug(ctx context.Context, slug string) (*Category, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("GetCategoryBySlug")

	slug = strings.ToLower(strings.TrimSpace(slug))
	if slug == "" {
		err := errors.New("category slug is required")
		logger.Error("Category validation failed", "error", err)
		return nil, err
	}

	var category Category

	query := `SELECT ` + categorySelectColumns + ` FROM categories WHERE slug = $1 AND deleted_at IS NULL`

	err := scanCategory(m.DB.QueryRow(ctx, query, slug), &category)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			logger.Warn("Category not found", "slug", slug)
			return nil, ErrCategoryNotFound
		}

		logger.Error("Get category by slug failed", "error", err, "slug", slug)
		return nil, err
	}

	logger.Info("Get category by slug successful", "category_id", category.ID, "slug", slug)

	return &category, nil
}

// GetAll retrieves all categories in stable presentation order.
// This is a bulk/admin-oriented listing and intentionally does not try to group
// by department UUID, since UUID ordering is not presentation-safe.
func (m *CategoryModel) GetAll(ctx context.Context) ([]*Category, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("GetAllCategories")

	query := `
		SELECT ` + categorySelectColumns + `
		FROM categories
			WHERE deleted_at IS NULL
		ORDER BY sort_order ASC, name ASC, id ASC
	`

	rows, err := m.DB.Query(ctx, query)
	if err != nil {
		logger.Error("Get all categories query failed", "error", err)
		return nil, err
	}
	defer rows.Close()

	categories := make([]*Category, 0)

	for rows.Next() {
		var category Category

		if err := scanCategory(rows, &category); err != nil {
			logger.Error("Category row scan failed", "error", err)
			return nil, err
		}

		categories = append(categories, &category)
	}

	if err := rows.Err(); err != nil {
		logger.Error("Category row iteration failed", "error", err)
		return nil, err
	}

	logger.Info("Get all categories successful", "count", len(categories))

	return categories, nil
}

// GetByDepartmentID retrieves all categories for a given department in stable taxonomy order.
func (m *CategoryModel) GetByDepartmentID(ctx context.Context, departmentID uuid.UUID) ([]*Category, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("GetCategoriesByDepartmentID")

	if departmentID == uuid.Nil {
		err := errors.New("invalid department ID")
		logger.Error("Category validation failed", "error", err)
		return nil, err
	}

	query := `
		SELECT ` + categorySelectColumns + `
		FROM categories
		WHERE department_id = $1
			AND deleted_at IS NULL
		ORDER BY sort_order ASC, name ASC, id ASC
	`

	rows, err := m.DB.Query(ctx, query, departmentID)
	if err != nil {
		logger.Error("Get categories by department ID query failed", "error", err, "department_id", departmentID)
		return nil, err
	}
	defer rows.Close()

	categories := make([]*Category, 0)

	for rows.Next() {
		var category Category

		if err := scanCategory(rows, &category); err != nil {
			logger.Error("Category row scan failed", "error", err)
			return nil, err
		}

		categories = append(categories, &category)
	}

	if err := rows.Err(); err != nil {
		logger.Error("Category row iteration failed", "error", err)
		return nil, err
	}

	logger.Info("Get categories by department ID successful", "department_id", departmentID, "count", len(categories))

	return categories, nil
}

// GetRootsByDepartmentID retrieves root categories for a department.
// Root categories are those with parent_id IS NULL.
func (m *CategoryModel) GetRootsByDepartmentID(ctx context.Context, departmentID uuid.UUID) ([]*Category, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("GetRootCategoriesByDepartmentID")

	if departmentID == uuid.Nil {
		err := errors.New("invalid department ID")
		logger.Error("Category validation failed", "error", err)
		return nil, err
	}

	query := `
		SELECT ` + categorySelectColumns + `
		FROM categories
		WHERE department_id = $1
		  AND parent_id IS NULL
		  AND deleted_at IS NULL
		ORDER BY sort_order ASC, name ASC, id ASC
	`

	rows, err := m.DB.Query(ctx, query, departmentID)
	if err != nil {
		logger.Error("Get root categories by department ID query failed", "error", err, "department_id", departmentID)
		return nil, err
	}
	defer rows.Close()

	categories := make([]*Category, 0)

	for rows.Next() {
		var category Category

		if err := scanCategory(rows, &category); err != nil {
			logger.Error("Category row scan failed", "error", err)
			return nil, err
		}

		categories = append(categories, &category)
	}

	if err := rows.Err(); err != nil {
		logger.Error("Category row iteration failed", "error", err)
		return nil, err
	}

	logger.Info("Get root categories by department ID successful", "department_id", departmentID, "count", len(categories))

	return categories, nil
}

// GetByParentID retrieves child categories for a parent category.
func (m *CategoryModel) GetByParentID(ctx context.Context, parentID uuid.UUID) ([]*Category, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("GetCategoriesByParentID")

	if parentID == uuid.Nil {
		err := errors.New("invalid parent category ID")
		logger.Error("Category validation failed", "error", err)
		return nil, err
	}

	query := `
		SELECT ` + categorySelectColumns + `
		FROM categories
		WHERE parent_id = $1
			AND deleted_at IS NULL
		ORDER BY sort_order ASC, name ASC, id ASC
	`

	rows, err := m.DB.Query(ctx, query, parentID)
	if err != nil {
		logger.Error("Get categories by parent ID query failed", "error", err, "parent_id", parentID)
		return nil, err
	}
	defer rows.Close()

	categories := make([]*Category, 0)

	for rows.Next() {
		var category Category

		if err := scanCategory(rows, &category); err != nil {
			logger.Error("Category row scan failed", "error", err)
			return nil, err
		}

		categories = append(categories, &category)
	}

	if err := rows.Err(); err != nil {
		logger.Error("Category row iteration failed", "error", err)
		return nil, err
	}

	logger.Info("Get categories by parent ID successful", "parent_id", parentID, "count", len(categories))

	return categories, nil
}

// GetChildren is an alias for GetByParentID to make taxonomy intent explicit at call sites.
func (m *CategoryModel) GetChildren(ctx context.Context, parentID uuid.UUID) ([]*Category, error) {
	return m.GetByParentID(ctx, parentID)
}

// Update updates a category and returns the canonical DB-owned row state.
func (m *CategoryModel) Update(ctx context.Context, category *Category) error {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("UpdateCategory")

	if err := validateCategoryForUpdate(category); err != nil {
		logger.Error("Category validation failed", "error", err)
		return err
	}

	query := `
		UPDATE categories
		SET
			name = $1,
			slug = $2,
			department_id = $3,
			parent_id = $4,
			sort_order = $5,
			description = $6
		WHERE id = $7
			AND deleted_at IS NULL
		RETURNING ` + categorySelectColumns

	err := scanCategory(
		m.DB.QueryRow(
			ctx,
			query,
			category.Name,
			category.Slug,
			category.DepartmentID,
			category.ParentID,
			category.SortOrder,
			category.Description,
			category.ID,
		),
		category,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			logger.Warn("Category not found during update", "category_id", category.ID)
			return ErrCategoryNotFound
		}

		logger.Error("Update category failed", "error", err, "category_id", category.ID)
		return err
	}

	logger.Info("Update category successful", "category_id", category.ID)

	return nil
}

// SoftDelete logically removes a category by setting deleted_at.
// This is the canonical business-lifecycle removal path for taxonomy nodes.
//
// The soft_delete_category action is seeded as a system contract. Standard
// removal must use SoftDelete(); Delete() is the physical purge path only.
//
// Returns ErrCategoryNotFound when no active row matches the supplied ID.
func (m *CategoryModel) SoftDelete(ctx context.Context, id uuid.UUID) error {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("SoftDeleteCategory")

	if id == uuid.Nil {
		err := errors.New("invalid category ID")
		logger.Error("Category validation failed", "error", err)
		return err
	}

	var deletedAt time.Time
	err := m.DB.QueryRow(ctx, `
		UPDATE categories
		SET deleted_at = NOW()
		WHERE id = $1
		  AND deleted_at IS NULL
		RETURNING deleted_at
	`, id).Scan(&deletedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			logger.Warn("Category not found during soft delete", "category_id", id)
			return ErrCategoryNotFound
		}
		logger.Error("Soft delete category failed", "error", err, "category_id", id)
		return err
	}

	logger.Info("Soft delete category successful", "category_id", id, "deleted_at", deletedAt)
	return nil
}

// Delete permanently removes a category from the database.
// This is the physical purge path and must remain a true hard delete.
// It must not be used as the ordinary removal path; callers should use SoftDelete().
//
// Returns ErrCategoryNotFound when no row exists for the supplied ID.
func (m *CategoryModel) Delete(ctx context.Context, id uuid.UUID) error {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("DeleteCategory")

	if id == uuid.Nil {
		err := errors.New("invalid category ID")
		logger.Error("Category validation failed", "error", err)
		return err
	}

	var deletedID uuid.UUID
	err := m.DB.QueryRow(ctx, `
		DELETE FROM categories
		WHERE id = $1
		RETURNING id
	`, id).Scan(&deletedID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			logger.Warn("Category not found during delete", "category_id", id)
			return ErrCategoryNotFound
		}
		logger.Error("Delete category failed", "error", err, "category_id", id)
		return err
	}

	logger.Info("Delete category successful", "category_id", deletedID)
	return nil
}
