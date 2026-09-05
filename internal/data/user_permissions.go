// Package data provides models and database access methods for user roles and permissions.
//
// File: sdworkspace/sdbackend/internal/data/user_permissions.go
//
// GTM:
//
//	Layer: 2.2 Identity / Auth Domain
//	Release Class: SPINE
//	Reason:
//	  Permissions and role-permission mappings are release-critical
//	  authorization infrastructure. They define the canonical permission
//	  catalog, enforce role capability assignments, support permission checks,
//	  and preserve access-control integrity required by the initial
//	  Platform release spine.
//
// SPINE Rule:
//
//	Keep compiling.
//	Keep production-ready.
//	Preserve permission reference-data semantics.
//	Preserve role-permission assignment integrity.
//	Preserve duplicate-assignment rejection.
//	Preserve UUID validation for permission and role identifiers.
//	Preserve DB-owned lifecycle timestamp behavior.
//	Block deployment if this file breaks build, permission lookup,
//	role-permission assignment, permission checks, or authorization integrity.
package data

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/PiccoloMondoC/focodebase/fobackend/internal/logging"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Permission represents a canonical permission row.
//
// Notes:
// - ID and persisted lifecycle timestamps are DB-owned.
// - This struct mirrors the permissions reference table.
type Permission struct {
	ID          string    `json:"id" db:"id"`
	Name        string    `json:"name" db:"name"`
	Description *string   `json:"description,omitempty" db:"description"`
	CreatedAt   time.Time `json:"created_at" db:"created_at"`
	UpdatedAt   time.Time `json:"updated_at" db:"updated_at"`
}

// RolePermission represents a role-to-permission assignment row.
type RolePermission struct {
	RoleID       string `json:"role_id" db:"role_id"`
	PermissionID string `json:"permission_id" db:"permission_id"`
}

// PermissionModel holds the DB pool and logger for permission operations.
type PermissionModel struct {
	DB     *pgxpool.Pool
	Logger *logging.Logger
}

// RolePermissionModel holds the DB pool and logger for role-permission assignment operations.
type RolePermissionModel struct {
	DB     *pgxpool.Pool
	Logger *logging.Logger
}

// CreatePermission inserts a new permission.
//
// Production rules applied here:
//   - The database owns the canonical ID and lifecycle timestamps.
//   - The inserted row is read back via RETURNING so the caller receives the
//     canonical stored values rather than application-invented values.
func (m *PermissionModel) CreatePermission(ctx context.Context, permission *Permission) error {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("CreatePermission")

	if permission == nil {
		err := errors.New("permission is required")
		logger.Error("Validation failed", err)
		return err
	}

	permission.Name = strings.TrimSpace(permission.Name)
	if permission.Name == "" {
		err := errors.New("permission name is required")
		logger.Error("Validation failed", err)
		return err
	}

	query := `
		INSERT INTO permissions (name, description)
		VALUES ($1, $2)
		RETURNING id, name, description, created_at, updated_at
	`

	if err := m.DB.QueryRow(ctx, query, permission.Name, permission.Description).Scan(
		&permission.ID,
		&permission.Name,
		&permission.Description,
		&permission.CreatedAt,
		&permission.UpdatedAt,
	); err != nil {
		logger.Error("Insert permission failed", err, "permission_name", permission.Name)
		return err
	}

	logger.Info("Insert permission successful",
		"permission_id", permission.ID,
		"permission_name", permission.Name,
	)

	return nil
}

// GetPermissionByID retrieves a single permission by ID.
func (m *PermissionModel) GetPermissionByID(ctx context.Context, id string) (*Permission, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("GetPermissionByID")

	if _, err := uuid.Parse(id); err != nil {
		logger.Error("Invalid permission ID", err, "permission_id", id)
		return nil, fmt.Errorf("invalid permission ID: %w", err)
	}

	query := `
		SELECT id, name, description, created_at, updated_at
		FROM permissions
		WHERE id = $1
	`

	var permission Permission
	err := m.DB.QueryRow(ctx, query, id).Scan(
		&permission.ID,
		&permission.Name,
		&permission.Description,
		&permission.CreatedAt,
		&permission.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			logger.Warn("Permission not found", "permission_id", id)
			return nil, ErrPermissionNotFound
		}

		logger.Error("Query failed", err, "permission_id", id)
		return nil, err
	}

	logger.Info("Permission retrieved successfully", "permission_id", id)
	return &permission, nil
}

// GetAllPermissions retrieves all permissions in stable name order.
func (m *PermissionModel) GetAllPermissions(ctx context.Context) ([]*Permission, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("GetAllPermissions")

	query := `
		SELECT id, name, description, created_at, updated_at
		FROM permissions
		ORDER BY name ASC, id ASC
	`

	rows, err := m.DB.Query(ctx, query)
	if err != nil {
		logger.Error("Query failed", err)
		return nil, err
	}
	defer rows.Close()

	permissions, err := pgx.CollectRows(rows, pgx.RowToAddrOfStructByName[Permission])
	if err != nil {
		logger.Error("Collect rows failed", err)
		return nil, err
	}

	logger.Info("Retrieved permissions", "count", len(permissions))
	return permissions, nil
}

// UpdatePermission updates a permission and returns the canonical stored row after mutation.
//
// Notes:
// - The DB remains the owner of updated_at.
// - We do not application-write lifecycle timestamps.
// - Not-found returns the typed sentinel ErrPermissionNotFound.
func (m *PermissionModel) UpdatePermission(ctx context.Context, permission *Permission) error {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("UpdatePermission")

	if permission == nil {
		err := errors.New("permission is required")
		logger.Error("Validation failed", err)
		return err
	}

	if _, err := uuid.Parse(permission.ID); err != nil {
		logger.Error("Invalid permission ID", err, "permission_id", permission.ID)
		return fmt.Errorf("invalid permission ID: %w", err)
	}

	permission.Name = strings.TrimSpace(permission.Name)
	if permission.Name == "" {
		err := errors.New("permission name is required")
		logger.Error("Validation failed", err)
		return err
	}

	query := `
		UPDATE permissions
		SET name = $1,
		    description = $2,
		    updated_at = NOW()
		WHERE id = $3
		RETURNING id, name, description, created_at, updated_at
	`

	err := m.DB.QueryRow(ctx, query, permission.Name, permission.Description, permission.ID).Scan(
		&permission.ID,
		&permission.Name,
		&permission.Description,
		&permission.CreatedAt,
		&permission.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			logger.Warn("Update failed - permission not found", "permission_id", permission.ID)
			return ErrPermissionNotFound
		}

		logger.Error("Update permission failed", err, "permission_id", permission.ID)
		return err
	}

	logger.Info("Update permission successful",
		"permission_id", permission.ID,
		"permission_name", permission.Name,
	)

	return nil
}

// DeletePermission permanently deletes a permission row.
//
// This is a true hard delete. Permissions are modeled as a reference table and do not
// currently carry soft-delete lifecycle semantics.
func (m *PermissionModel) DeletePermission(ctx context.Context, id string) error {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("DeletePermission")

	if _, err := uuid.Parse(id); err != nil {
		logger.Error("Invalid permission ID", err, "permission_id", id)
		return fmt.Errorf("invalid permission ID: %w", err)
	}

	query := `
		DELETE FROM permissions
		WHERE id = $1
		RETURNING id
	`

	var deletedID string
	if err := m.DB.QueryRow(ctx, query, id).Scan(&deletedID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			logger.Warn("Delete failed - permission not found", "permission_id", id)
			return ErrPermissionNotFound
		}

		logger.Error("Delete permission failed", err, "permission_id", id)
		return err
	}

	logger.Info("Delete permission successful", "permission_id", deletedID)
	return nil
}

// AssignPermissionToRole creates a role-permission assignment.
//
// Production rules applied here:
// - Both IDs must be valid UUIDs.
// - Duplicate assignment must fail clearly rather than silently succeeding.
func (m *RolePermissionModel) AssignPermissionToRole(ctx context.Context, roleID, permissionID string) error {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("AssignPermissionToRole")

	if _, err := uuid.Parse(roleID); err != nil {
		logger.Error("Invalid role ID", err, "role_id", roleID)
		return fmt.Errorf("invalid role ID: %w", err)
	}

	if _, err := uuid.Parse(permissionID); err != nil {
		logger.Error("Invalid permission ID", err, "permission_id", permissionID)
		return fmt.Errorf("invalid permission ID: %w", err)
	}

	query := `
		INSERT INTO role_permissions (role_id, permission_id)
		VALUES ($1, $2)
	`

	_, err := m.DB.Exec(ctx, query, roleID, permissionID)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			logger.Warn("Duplicate role-permission assignment blocked",
				"role_id", roleID,
				"permission_id", permissionID,
			)
			return ErrRolePermissionAlreadyAssigned
		}

		logger.Error("Assign permission to role failed", err,
			"role_id", roleID,
			"permission_id", permissionID,
		)
		return err
	}

	logger.Info("Assigned permission to role successfully",
		"role_id", roleID,
		"permission_id", permissionID,
	)

	return nil
}

// RemovePermissionFromRole permanently removes a role-permission assignment.
func (m *RolePermissionModel) RemovePermissionFromRole(ctx context.Context, roleID, permissionID string) error {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("RemovePermissionFromRole")

	if _, err := uuid.Parse(roleID); err != nil {
		logger.Error("Invalid role ID", err, "role_id", roleID)
		return fmt.Errorf("invalid role ID: %w", err)
	}

	if _, err := uuid.Parse(permissionID); err != nil {
		logger.Error("Invalid permission ID", err, "permission_id", permissionID)
		return fmt.Errorf("invalid permission ID: %w", err)
	}

	query := `
		DELETE FROM role_permissions
		WHERE role_id = $1
		  AND permission_id = $2
		RETURNING role_id, permission_id
	`

	var deleted RolePermission
	if err := m.DB.QueryRow(ctx, query, roleID, permissionID).Scan(
		&deleted.RoleID,
		&deleted.PermissionID,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			logger.Warn("Delete failed - role-permission assignment not found",
				"role_id", roleID,
				"permission_id", permissionID,
			)
			return ErrRolePermissionNotFound
		}

		logger.Error("Delete role-permission failed", err,
			"role_id", roleID,
			"permission_id", permissionID,
		)
		return err
	}

	logger.Info("Permission successfully removed from role",
		"role_id", deleted.RoleID,
		"permission_id", deleted.PermissionID,
	)

	return nil
}

// GetPermissionsByRole retrieves all permissions assigned to a role in stable order.
func (m *RolePermissionModel) GetPermissionsByRole(ctx context.Context, roleID string) ([]*Permission, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("GetPermissionsByRole")

	if _, err := uuid.Parse(roleID); err != nil {
		logger.Error("Invalid role ID", err, "role_id", roleID)
		return nil, fmt.Errorf("invalid role ID: %w", err)
	}

	query := `
		SELECT p.id, p.name, p.description, p.created_at, p.updated_at
		FROM permissions p
		INNER JOIN role_permissions rp
			ON rp.permission_id = p.id
		WHERE rp.role_id = $1
		ORDER BY p.name ASC, p.id ASC
	`

	rows, err := m.DB.Query(ctx, query, roleID)
	if err != nil {
		logger.Error("Query execution failed", err, "role_id", roleID)
		return nil, err
	}
	defer rows.Close()

	permissions, err := pgx.CollectRows(rows, pgx.RowToAddrOfStructByName[Permission])
	if err != nil {
		logger.Error("Collect rows failed", err, "role_id", roleID)
		return nil, err
	}

	logger.Info("Retrieved permissions for role",
		"role_id", roleID,
		"count", len(permissions),
	)

	return permissions, nil
}

// RoleHasPermission checks whether a role currently has a named permission.
func (m *RolePermissionModel) RoleHasPermission(ctx context.Context, roleID, permissionName string) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("RoleHasPermission")

	if _, err := uuid.Parse(roleID); err != nil {
		logger.Error("Invalid role ID", err, "role_id", roleID)
		return false, fmt.Errorf("invalid role ID: %w", err)
	}

	permissionName = strings.TrimSpace(permissionName)
	if permissionName == "" {
		err := errors.New("permission name is required")
		logger.Error("Validation failed", err)
		return false, err
	}

	query := `
		SELECT EXISTS (
			SELECT 1
			FROM role_permissions rp
			INNER JOIN permissions p
				ON rp.permission_id = p.id
			WHERE rp.role_id = $1
			  AND p.name = $2
		)
	`

	var hasPermission bool
	if err := m.DB.QueryRow(ctx, query, roleID, permissionName).Scan(&hasPermission); err != nil {
		logger.Error("Permission existence query failed", err,
			"role_id", roleID,
			"permission_name", permissionName,
		)
		return false, err
	}

	logger.Info("Permission check complete",
		"role_id", roleID,
		"permission_name", permissionName,
		"has_permission", hasPermission,
	)

	return hasPermission, nil
}
