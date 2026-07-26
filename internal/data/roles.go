// Package data provides models and database access methods for roles and other entities.
//
// sdworkspace/sdbackend/internal/data/roles.go
//
// GTM:
//
//	Layer: 2.2 Identity / Auth Domain
//	Release Class: SPINE
//	Reason:
//	  Roles and user role assignments are release-critical authorization
//	  infrastructure. They define role identity, hierarchy, internal/user-facing
//	  role boundaries, signup assignability, approval requirements, active-state
//	  behavior, and user-role membership needed by v1 access control.
//
// SPINE Rule:
//
//	Keep compiling.
//	Keep production-ready.
//	Preserve role hierarchy semantics.
//	Preserve active vs deleted lifecycle behavior.
//	Preserve user role assignment integrity.
//	Preserve primary-role behavior.
//	Preserve DB-owned lifecycle timestamp behavior.
//	Block deployment if this file breaks build, role lookup,
//	role assignment, hierarchy checks, or authorization integrity.
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

const roleSelectColumns = `
	id,
	name,
	description,
	hierarchy_level,
	is_internal,
	assignable_at_signup,
	approval_required,
	is_active,
	created_at,
	updated_at,
	deleted_at
`

// Role represents the canonical persisted role record.
// deleted_at is intentionally hidden from casual JSON emission.
type Role struct {
	ID                 uuid.UUID  `json:"id" db:"id"`
	Name               string     `json:"name" db:"name"`
	Description        string     `json:"description" db:"description"`
	HierarchyLevel     int        `json:"hierarchy_level" db:"hierarchy_level"`
	IsInternal         bool       `json:"is_internal" db:"is_internal"`
	AssignableAtSignup bool       `json:"assignable_at_signup" db:"assignable_at_signup"`
	ApprovalRequired   bool       `json:"approval_required" db:"approval_required"`
	IsActive           bool       `json:"is_active" db:"is_active"`
	CreatedAt          time.Time  `json:"created_at" db:"created_at"`
	UpdatedAt          time.Time  `json:"updated_at" db:"updated_at"`
	DeletedAt          *time.Time `json:"-" db:"deleted_at"`
}

// RoleModel is the structure which holds the DB instance.
type RoleModel struct {
	DB     *pgxpool.Pool
	Logger *logging.Logger
}

func scanRole(scanner interface{ Scan(...any) error }, role *Role) error {
	return scanner.Scan(
		&role.ID,
		&role.Name,
		&role.Description,
		&role.HierarchyLevel,
		&role.IsInternal,
		&role.AssignableAtSignup,
		&role.ApprovalRequired,
		&role.IsActive,
		&role.CreatedAt,
		&role.UpdatedAt,
		&role.DeletedAt,
	)
}

func normalizeRoleName(name string) string {
	return strings.TrimSpace(name)
}

// CreateRole inserts a new role and lets the database own all canonical stored timestamps.
// It returns the fully populated persisted record via RETURNING.
func (m *RoleModel) CreateRole(ctx context.Context, role *Role) error {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("CreateRole")

	if role == nil {
		err := errors.New("role is required")
		logger.Error("Validation failed", err)
		return err
	}

	role.Name = normalizeRoleName(role.Name)
	role.Description = strings.TrimSpace(role.Description)

	if role.Name == "" {
		err := errors.New("role name is required")
		logger.Error("Validation failed", err)
		return err
	}
	if role.Description == "" {
		err := errors.New("role description is required")
		logger.Error("Validation failed", err)
		return err
	}
	if role.HierarchyLevel < 0 {
		err := errors.New("hierarchy level must be non-negative")
		logger.Error("Validation failed", err)
		return err
	}

	query := fmt.Sprintf(`
		INSERT INTO roles (
			name,
			description,
			hierarchy_level,
			is_internal,
			assignable_at_signup,
			approval_required
		)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING %s
	`, roleSelectColumns)

	if err := scanRole(
		m.DB.QueryRow(
			ctx,
			query,
			role.Name,
			role.Description,
			role.HierarchyLevel,
			role.IsInternal,
			role.AssignableAtSignup,
			role.ApprovalRequired,
		),
		role,
	); err != nil {
		logger.Error("Create role failed", err)
		return err
	}

	logger.Info("Role created successfully", "role_id", role.ID, "role_name", role.Name)
	return nil
}

// GetRoleByID returns a non-deleted role by ID.
// Inactive roles are still readable; deleted roles are not returned by normal reads.
func (m *RoleModel) GetRoleByID(ctx context.Context, id uuid.UUID) (*Role, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("GetRoleByID")

	if id == uuid.Nil {
		err := errors.New("role ID is required")
		logger.Error("Validation failed", err)
		return nil, err
	}

	query := fmt.Sprintf(`
		SELECT %s
		FROM roles
		WHERE id = $1
		  AND deleted_at IS NULL
	`, roleSelectColumns)

	var role Role
	if err := scanRole(m.DB.QueryRow(ctx, query, id), &role); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			logger.Warn("Role not found", "role_id", id)
			return nil, ErrRoleNotFound
		}
		logger.Error("Query failed", err)
		return nil, err
	}

	return &role, nil
}

// GetRoleNameByID returns the name for a non-deleted role.
func (m *RoleModel) GetRoleNameByID(ctx context.Context, id uuid.UUID) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("GetRoleNameByID")

	if id == uuid.Nil {
		err := errors.New("role ID is required")
		logger.Error("Validation failed", err)
		return "", err
	}

	const query = `
		SELECT name
		FROM roles
		WHERE id = $1
		  AND deleted_at IS NULL
	`

	var name string
	err := m.DB.QueryRow(ctx, query, id).Scan(&name)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			logger.Warn("Role not found", "role_id", id)
			return "", ErrRoleNotFound
		}
		logger.Error("Query failed", err)
		return "", err
	}

	return name, nil
}

// GetRoleByName returns a non-deleted role by canonical name.
// Inactive roles remain queryable here; deletion is the default exclusion boundary.
func (m *RoleModel) GetRoleByName(ctx context.Context, name string) (*Role, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("GetRoleByName")

	name = normalizeRoleName(name)
	if name == "" {
		err := errors.New("role name is required")
		logger.Error("Validation failed", err)
		return nil, err
	}

	query := fmt.Sprintf(`
		SELECT %s
		FROM roles
		WHERE name = $1
		  AND deleted_at IS NULL
	`, roleSelectColumns)

	var role Role
	if err := scanRole(m.DB.QueryRow(ctx, query, name), &role); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			logger.Warn("Role not found", "role_name", name)
			return nil, ErrRoleNotFound
		}
		logger.Error("Query failed", err)
		return nil, err
	}

	return &role, nil
}

// ListRoles returns all non-deleted roles, including inactive roles.
func (m *RoleModel) ListRoles(ctx context.Context) ([]*Role, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("ListRoles")

	query := fmt.Sprintf(`
		SELECT %s
		FROM roles
		WHERE deleted_at IS NULL
		ORDER BY hierarchy_level ASC, name ASC
	`, roleSelectColumns)

	rows, err := m.DB.Query(ctx, query)
	if err != nil {
		logger.Error("Query roles failed", err)
		return nil, err
	}
	defer rows.Close()

	var roles []*Role
	for rows.Next() {
		var role Role
		if err := scanRole(rows, &role); err != nil {
			logger.Error("Scanning role failed", err)
			return nil, err
		}
		roles = append(roles, &role)
	}

	if err := rows.Err(); err != nil {
		logger.Error("Rows iteration error", err)
		return nil, err
	}

	return roles, nil
}

// ListActive returns active, non-deleted roles with bounded pagination.
func (m *RoleModel) ListActive(ctx context.Context, limit, offset int) ([]*Role, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("ListActive")

	if limit <= 0 {
		err := errors.New("limit must be greater than zero")
		logger.Error("Validation failed", err)
		return nil, err
	}
	if limit > 100 {
		limit = 100
	}
	if offset < 0 {
		err := errors.New("offset cannot be negative")
		logger.Error("Validation failed", err)
		return nil, err
	}

	query := fmt.Sprintf(`
		SELECT %s
		FROM roles
		WHERE deleted_at IS NULL
		  AND is_active = TRUE
		ORDER BY hierarchy_level ASC, name ASC
		LIMIT $1 OFFSET $2
	`, roleSelectColumns)

	rows, err := m.DB.Query(ctx, query, limit, offset)
	if err != nil {
		logger.Error("Query failed", err)
		return nil, err
	}
	defer rows.Close()

	var roles []*Role
	for rows.Next() {
		var role Role
		if err := scanRole(rows, &role); err != nil {
			logger.Error("Row scan failed", err)
			return nil, err
		}
		roles = append(roles, &role)
	}

	if err := rows.Err(); err != nil {
		logger.Error("Row iteration error", err)
		return nil, err
	}

	return roles, nil
}

// UpdateRole updates a non-deleted role and lets the database own updated_at.
func (m *RoleModel) UpdateRole(ctx context.Context, role *Role) error {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("UpdateRole")

	if role == nil {
		err := errors.New("role is required")
		logger.Error("Validation failed", err)
		return err
	}
	if role.ID == uuid.Nil {
		err := errors.New("role ID is required")
		logger.Error("Validation failed", err)
		return err
	}

	role.Name = normalizeRoleName(role.Name)
	role.Description = strings.TrimSpace(role.Description)

	if role.Name == "" {
		err := errors.New("role name is required")
		logger.Error("Validation failed", err)
		return err
	}
	if role.Description == "" {
		err := errors.New("role description is required")
		logger.Error("Validation failed", err)
		return err
	}
	if role.HierarchyLevel < 0 {
		err := errors.New("hierarchy level must be non-negative")
		logger.Error("Validation failed", err)
		return err
	}

	query := fmt.Sprintf(`
		UPDATE roles
		SET
			name = $2,
			description = $3,
			hierarchy_level = $4,
			is_internal = $5,
			assignable_at_signup = $6,
			approval_required = $7,
			is_active = $8,
			updated_at = NOW()
		WHERE id = $1
		  AND deleted_at IS NULL
		RETURNING %s
	`, roleSelectColumns)

	if err := scanRole(
		m.DB.QueryRow(
			ctx,
			query,
			role.ID,
			role.Name,
			role.Description,
			role.HierarchyLevel,
			role.IsInternal,
			role.AssignableAtSignup,
			role.ApprovalRequired,
			role.IsActive,
		),
		role,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			logger.Warn("Role not found for update", "role_id", role.ID)
			return ErrRoleNotFound
		}
		logger.Error("Update role failed", err)
		return err
	}

	return nil
}

// SetRoleActiveStatus toggles the active capability of a non-deleted role.
// This is not deletion.
func (m *RoleModel) SetRoleActiveStatus(ctx context.Context, roleID uuid.UUID, isActive bool) error {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("SetRoleActiveStatus")

	if roleID == uuid.Nil {
		err := errors.New("role ID is required")
		logger.Error("Validation failed", err)
		return err
	}

	const query = `
		UPDATE roles
		SET
			is_active = $2,
			updated_at = NOW()
		WHERE id = $1
		  AND deleted_at IS NULL
		RETURNING id
	`

	var updatedRoleID uuid.UUID
	err := m.DB.QueryRow(ctx, query, roleID, isActive).Scan(&updatedRoleID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			logger.Warn("Role not found for active-status update", "role_id", roleID)
			return ErrRoleNotFound
		}
		logger.Error("Failed to update role active status", err)
		return err
	}

	return nil
}

// GetRoleHierarchyLevel returns the hierarchy level for an active, non-deleted role.
func (m *RoleModel) GetRoleHierarchyLevel(ctx context.Context, roleName string) (int, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("GetRoleHierarchyLevel")

	roleName = normalizeRoleName(roleName)
	if roleName == "" {
		err := errors.New("role name is required")
		logger.Error("Validation failed", err)
		return 0, err
	}

	const query = `
		SELECT hierarchy_level
		FROM roles
		WHERE name = $1
		  AND deleted_at IS NULL
		  AND is_active = TRUE
	`

	var level int
	err := m.DB.QueryRow(ctx, query, roleName).Scan(&level)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			logger.Warn("Role not found", "role_name", roleName)
			return 0, ErrRoleNotFound
		}
		logger.Error("Query failed", err)
		return 0, err
	}

	return level, nil
}

// SoftDelete is the canonical removal path for roles.
// It marks the role as deleted and inactive using DB-owned timestamps.
func (m *RoleModel) SoftDelete(ctx context.Context, roleID uuid.UUID) error {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("SoftDelete")

	if roleID == uuid.Nil {
		err := errors.New("role ID is required")
		logger.Error("Validation failed", err)
		return err
	}

	const query = `
		UPDATE roles
		SET
			is_active = FALSE,
			deleted_at = NOW(),
			updated_at = NOW()
		WHERE id = $1
		  AND deleted_at IS NULL
	`

	tag, err := m.DB.Exec(ctx, query, roleID)
	if err != nil {
		logger.Error("Soft delete failed", err)
		return err
	}
	if tag.RowsAffected() == 0 {
		logger.Warn("Role not found for soft delete", "role_id", roleID)
		return ErrRoleNotFound
	}

	return nil
}

// DeleteRole permanently removes a previously soft-deleted role.
// Hard delete remains exceptional cleanup, not normal business removal.
func (m *RoleModel) DeleteRole(ctx context.Context, id uuid.UUID) error {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("DeleteRole")

	if id == uuid.Nil {
		err := errors.New("role ID is required")
		logger.Error("Validation failed", err)
		return err
	}

	const query = `
		DELETE FROM roles
		WHERE id = $1
		  AND deleted_at IS NOT NULL
	`

	tag, err := m.DB.Exec(ctx, query, id)
	if err != nil {
		logger.Error("Failed to hard delete role", err)
		return err
	}
	if tag.RowsAffected() == 0 {
		logger.Warn("Role not found for hard delete or not yet soft deleted", "role_id", id)
		return ErrRoleNotFound
	}

	return nil
}

// GetRolesByUserID returns all active, non-deleted current role assignments for a user.
// Roles are ordered with primary first, then higher hierarchy.
func (m *RoleModel) GetRolesByUserID(ctx context.Context, userID uuid.UUID) ([]*Role, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("GetRolesByUserID")

	if userID == uuid.Nil {
		err := errors.New("user ID is required")
		logger.Error("Validation failed", err)
		return nil, err
	}

	query := fmt.Sprintf(`
		SELECT %s
		FROM user_role_assignments ura
		INNER JOIN roles r ON r.id = ura.role_id
		INNER JOIN users u ON u.id = ura.user_id
		WHERE ura.user_id = $1
		  AND ura.deleted_at IS NULL
		  AND r.deleted_at IS NULL
		  AND r.is_active = TRUE
		  AND u.deleted_at IS NULL
		ORDER BY ura.is_primary DESC, r.hierarchy_level DESC, r.name ASC
	`, roleSelectColumns)

	rows, err := m.DB.Query(ctx, query, userID)
	if err != nil {
		logger.Error("Query failed", err)
		return nil, err
	}
	defer rows.Close()

	var roles []*Role
	for rows.Next() {
		var role Role
		if err := scanRole(rows, &role); err != nil {
			logger.Error("Row scan failed", err)
			return nil, err
		}
		roles = append(roles, &role)
	}

	if err := rows.Err(); err != nil {
		logger.Error("Rows iteration error", err)
		return nil, err
	}

	return roles, nil
}

// GetRoleByUserID returns the user's current primary role if present;
// otherwise it returns the highest-hierarchy active current role.
func (m *RoleModel) GetRoleByUserID(ctx context.Context, userID uuid.UUID) (*Role, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("GetRoleByUserID")

	if userID == uuid.Nil {
		err := errors.New("user ID is required")
		logger.Error("Validation failed", err)
		return nil, err
	}

	query := fmt.Sprintf(`
		SELECT %s
		FROM user_role_assignments ura
		INNER JOIN roles r ON r.id = ura.role_id
		INNER JOIN users u ON u.id = ura.user_id
		WHERE ura.user_id = $1
		  AND ura.deleted_at IS NULL
		  AND r.deleted_at IS NULL
		  AND r.is_active = TRUE
		  AND u.deleted_at IS NULL
		ORDER BY ura.is_primary DESC, r.hierarchy_level DESC, r.name ASC
		LIMIT 1
	`, roleSelectColumns)

	var role Role
	if err := scanRole(m.DB.QueryRow(ctx, query, userID), &role); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			logger.Warn("No current role found for user", "user_id", userID)
			return nil, ErrRoleNotFound
		}
		logger.Error("Query failed", err)
		return nil, err
	}

	return &role, nil
}

// AssignRoleToUser assigns or reactivates a role assignment for a user.
// DB-owned time remains canonical; assignment rows are filtered by deleted_at.
func (m *RoleModel) AssignRoleToUser(ctx context.Context, userID, roleID uuid.UUID, assignedBy *uuid.UUID, makePrimary bool) error {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("AssignRoleToUser")

	if userID == uuid.Nil || roleID == uuid.Nil {
		err := errors.New("userID and roleID must be valid UUIDs")
		logger.Error("Validation failed", err)
		return err
	}
	if assignedBy != nil && *assignedBy == uuid.Nil {
		err := errors.New("assignedBy cannot be a nil UUID")
		logger.Error("Validation failed", err)
		return err
	}

	tx, err := m.DB.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		logger.Error("Failed to begin transaction", err)
		return err
	}
	defer tx.Rollback(ctx)

	// Validate user exists and is not deleted.
	var userExists bool
	if err := tx.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM users WHERE id = $1 AND deleted_at IS NULL)`,
		userID,
	).Scan(&userExists); err != nil {
		logger.Error("Failed to verify user", err)
		return err
	}
	if !userExists {
		err := errors.New("user does not exist or is deleted")
		logger.Error("Validation failed", err)
		return err
	}

	// Validate target role exists, is active, and is not deleted.
	var roleExists bool
	if err := tx.QueryRow(ctx, `
		SELECT EXISTS(
			SELECT 1
			FROM roles
			WHERE id = $1
			  AND deleted_at IS NULL
			  AND is_active = TRUE
		)
	`, roleID).Scan(&roleExists); err != nil {
		logger.Error("Failed to verify role", err)
		return err
	}
	if !roleExists {
		err := errors.New("role does not exist, is deleted, or is inactive")
		logger.Error("Validation failed", err)
		return err
	}

	if makePrimary {
		if _, err := tx.Exec(ctx, `
			UPDATE user_role_assignments
			SET
				is_primary = FALSE,
				updated_at = NOW()
			WHERE user_id = $1
			  AND deleted_at IS NULL
			  AND is_primary = TRUE
		`, userID); err != nil {
			logger.Error("Failed to clear existing primary role", err)
			return err
		}
	}

	// Insert or reactivate the assignment.
	_, err = tx.Exec(ctx, `
		INSERT INTO user_role_assignments (
			user_id,
			role_id,
			assigned_by,
			is_primary
		)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (user_id, role_id)
		DO UPDATE SET
			deleted_at = NULL,
			assigned_by = EXCLUDED.assigned_by,
			is_primary = EXCLUDED.is_primary,
			updated_at = NOW()
	`, userID, roleID, assignedBy, makePrimary)
	if err != nil {
		logger.Error("Role assignment failed", err)
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		logger.Error("Failed to commit transaction", err)
		return err
	}

	return nil
}

// RevokeRole soft-deletes a specific current role assignment for a user.
//
// A user may hold multiple roles concurrently, so both userID and roleID are
// required to identify the assignment. Revoking an assignment does not delete
// or deactivate the underlying role.
//
// When the revoked assignment is primary, the highest-hierarchy remaining
// active assignment is promoted to primary within the same transaction.
// Persisted lifecycle timestamps remain database-owned.
func (m *RoleModel) RevokeRole(
	ctx context.Context,
	userID uuid.UUID,
	roleID uuid.UUID,
) error {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.
		GetLoggerWithContextFromContext(ctx).
		WithFunctionName("RevokeRole")

	if userID == uuid.Nil {
		err := errors.New("user ID is required")
		logger.Error("Validation failed", err)
		return err
	}

	if roleID == uuid.Nil {
		err := errors.New("role ID is required")
		logger.Error("Validation failed", err)
		return err
	}

	tx, err := m.DB.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		logger.Error("Failed to begin transaction", err)
		return err
	}
	defer tx.Rollback(ctx)

	var wasPrimary bool

	const lockAssignmentQuery = `
		SELECT is_primary
		FROM user_role_assignments
		WHERE user_id = $1
		  AND role_id = $2
		  AND deleted_at IS NULL
		FOR UPDATE
	`

	err = tx.QueryRow(
		ctx,
		lockAssignmentQuery,
		userID,
		roleID,
	).Scan(&wasPrimary)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			logger.Warn(
				"Current role assignment not found",
				"user_id", userID,
				"role_id", roleID,
			)
			return ErrRoleNotFound
		}

		logger.Error("Failed to lock role assignment", err)
		return err
	}

	const revokeAssignmentQuery = `
		UPDATE user_role_assignments
		SET
			is_primary = FALSE,
			deleted_at = NOW(),
			updated_at = NOW()
		WHERE user_id = $1
		  AND role_id = $2
		  AND deleted_at IS NULL
	`

	tag, err := tx.Exec(
		ctx,
		revokeAssignmentQuery,
		userID,
		roleID,
	)
	if err != nil {
		logger.Error("Failed to revoke role assignment", err)
		return err
	}

	if tag.RowsAffected() != 1 {
		logger.Warn(
			"Role assignment changed before revoke completed",
			"user_id", userID,
			"role_id", roleID,
		)
		return ErrRoleNotFound
	}

	if wasPrimary {
		const promoteReplacementQuery = `
			WITH replacement AS (
				SELECT ura.role_id
				FROM user_role_assignments ura
				INNER JOIN roles r
					ON r.id = ura.role_id
				WHERE ura.user_id = $1
				  AND ura.deleted_at IS NULL
				  AND r.deleted_at IS NULL
				  AND r.is_active = TRUE
				ORDER BY
					r.hierarchy_level DESC,
					r.name ASC,
					ura.role_id ASC
				LIMIT 1
			)
			UPDATE user_role_assignments ura
			SET
				is_primary = TRUE,
				updated_at = NOW()
			FROM replacement
			WHERE ura.user_id = $1
			  AND ura.role_id = replacement.role_id
			  AND ura.deleted_at IS NULL
		`

		if _, err := tx.Exec(ctx, promoteReplacementQuery, userID); err != nil {
			logger.Error("Failed to promote replacement primary role", err)
			return err
		}
	}

	if err := tx.Commit(ctx); err != nil {
		logger.Error("Failed to commit role revocation", err)
		return err
	}

	logger.Info(
		"Role assignment revoked successfully",
		"user_id", userID,
		"role_id", roleID,
		"was_primary", wasPrimary,
	)

	return nil
}

// HasRole returns true if the user currently holds the named active role.
func (m *RoleModel) HasRole(ctx context.Context, userID uuid.UUID, roleName string) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("HasRole")

	if userID == uuid.Nil {
		err := errors.New("userID must be non-nil")
		logger.Error("Validation failed", err)
		return false, err
	}

	roleName = normalizeRoleName(roleName)
	if roleName == "" {
		err := errors.New("roleName is required")
		logger.Error("Validation failed", err)
		return false, err
	}

	const query = `
		SELECT 1
		FROM user_role_assignments ura
		INNER JOIN roles r ON r.id = ura.role_id
		INNER JOIN users u ON u.id = ura.user_id
		WHERE ura.user_id = $1
		  AND r.name = $2
		  AND ura.deleted_at IS NULL
		  AND r.deleted_at IS NULL
		  AND r.is_active = TRUE
		  AND u.deleted_at IS NULL
		LIMIT 1
	`

	var sentinel int
	err := m.DB.QueryRow(ctx, query, userID, roleName).Scan(&sentinel)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, nil
		}
		logger.Error("Role lookup failed", err)
		return false, err
	}

	return true, nil
}
