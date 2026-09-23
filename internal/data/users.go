// Package data provides models and database access methods for users and auth-adjacent user workflows.
//
// focodebase/fobackend/internal/data/users.go
//
// GTM:
//
//	Layer: 2.2 Identity / Auth Domain
//	Release Class: SPINE
//	Reason:
//	  Users are release-critical identity and account lifecycle infrastructure.
//	  This file owns canonical user persistence, signup persistence,
//	  authentication, OAuth account linking, password-hash persistence,
//	  primary-role read projection, contact-info lookup, active-state
//	  control, and account soft-delete cascade behavior required by the
//	  initial Platform release spine.
//
//	  This file deliberately does NOT own: plaintext password preparation
//	  (owned by internal/security, invoked from the service layer), role or
//	  user_role_assignments persistence (owned by RoleModel), password-reset
//	  token persistence (owned by PasswordResetModel), or activation-token
//	  persistence (owned by ActivationTokenModel). Cross-model transaction
//	  composition for signup, password reset, and account closure is owned by
//	  the service layer, never by model-to-model calls from this file.
//
// SPINE Rule:
//
//	Keep compiling.
//	Keep production-ready.
//	Preserve canonical users table alignment.
//	Preserve exactly-one signup authentication method enforcement.
//	Preserve protected password-hash persistence; this file must never hash,
//	verify, or otherwise transform plaintext password material itself except
//	within Authenticate's credential-verification primitive, which delegates
//	the actual comparison to internal/security.
//	Preserve OAuth account-linking integrity, including typed duplicate-ID
//	sentinel translation on unique-constraint violations.
//	Preserve DB-owned lifecycle timestamp behavior.
//	Preserve strict non-ownership of activation_tokens, password_resets, and
//	roles/user_role_assignments persistence. This file must never read or
//	write those tables directly.
//	Preserve guarded hard-delete: Delete must never remove a user row that
//	has not already been soft-deleted.
//	Block deployment if this file breaks build, user signup persistence,
//	authentication, OAuth linking, account lifecycle, or identity integrity.
package data

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/PiccoloMondoC/focodebase/fobackend/internal/logging"
	"github.com/PiccoloMondoC/focodebase/fobackend/internal/security"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// User represents the canonical users table plus derived primary-role metadata.
//
// RoleID and RoleName are derived from user_role_assignments + roles via a
// read-side join. They are not columns on users, must not be inserted into or
// updated on users, and must never be treated as an authorization decision by
// any caller — RoleModel.GetRoleByUserID (used by AuthMiddleware) remains the
// sole authorization-facing role resolution path.
type User struct {
	ID           uuid.UUID  `json:"id" db:"id"`
	GoogleID     *string    `json:"google_id,omitempty" db:"google_id"`
	FacebookID   *string    `json:"facebook_id,omitempty" db:"facebook_id"`
	Email        string     `json:"email" db:"email"`
	PasswordHash string     `json:"-" db:"password_hash"`
	RoleID       uuid.UUID  `json:"role_id,omitempty" db:"-"`
	RoleName     string     `json:"role_name,omitempty" db:"-"`
	IsActive     bool       `json:"is_active" db:"is_active"`
	DeletedAt    *time.Time `json:"-" db:"deleted_at"`
	CreatedAt    time.Time  `json:"created_at" db:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at" db:"updated_at"`
}

// UserContactInfo holds the contact details for account notifications.
type UserContactInfo struct {
	Email           string `json:"email"`
	Phone           string `json:"phone"`
	PreferredMethod string `json:"preferred_contact"`
}

// UserModel is the data model for users.
type UserModel struct {
	DB     *pgxpool.Pool
	Logger *logging.Logger
}

const userSelectColumns = `
	u.id,
	u.google_id,
	u.facebook_id,
	u.email,
	COALESCE(u.password_hash, ''),
	u.is_active,
	u.deleted_at,
	u.created_at,
	u.updated_at,
	COALESCE(ura.role_id, '00000000-0000-0000-0000-000000000000'::uuid),
	COALESCE(r.name::text, '')
`

// userPrimaryRoleJoin projects the user's current primary role.
//
// The role must be active, non-deleted, and the assignment itself must be the
// non-deleted primary assignment. Filtering on r.is_active mirrors
// RoleModel.GetRoleByUserID exactly, the method AuthMiddleware actually uses
// for authorization. A User read must never present role metadata that the
// authorization boundary would reject as inactive.
const userPrimaryRoleJoin = `
	LEFT JOIN user_role_assignments ura
		ON ura.user_id = u.id
		AND ura.is_primary = TRUE
		AND ura.deleted_at IS NULL
	LEFT JOIN roles r
		ON r.id = ura.role_id
		AND r.deleted_at IS NULL
		AND r.is_active = TRUE
`

// scanUser scans the canonical user row plus derived primary-role metadata.
func scanUser(row scannableRow) (*User, error) {
	var user User

	if err := row.Scan(
		&user.ID,
		&user.GoogleID,
		&user.FacebookID,
		&user.Email,
		&user.PasswordHash,
		&user.IsActive,
		&user.DeletedAt,
		&user.CreatedAt,
		&user.UpdatedAt,
		&user.RoleID,
		&user.RoleName,
	); err != nil {
		return nil, err
	}

	return &user, nil
}

// normalizeUserEmail trims email input. CITEXT handles case-insensitive uniqueness.
func normalizeUserEmail(email string) string {
	return strings.TrimSpace(email)
}

// cleanOptionalString converts blank optional provider IDs to nil.
func cleanOptionalString(value *string) *string {
	if value == nil {
		return nil
	}

	cleaned := strings.TrimSpace(*value)
	if cleaned == "" {
		return nil
	}

	return &cleaned
}

// insertUserTx inserts only columns that actually exist on users.
//
// Uniqueness failures (duplicate email, google_id, or facebook_id) are
// translated into typed data-layer sentinels via translateUserInsertError so
// callers can distinguish conflict conditions from infrastructure failures.
func (m *UserModel) insertUserTx(ctx context.Context, tx pgx.Tx, user *User) error {
	const query = `
		INSERT INTO users (
			id,
			google_id,
			facebook_id,
			email,
			password_hash,
			is_active
		)
		VALUES ($1, $2, $3, $4, NULLIF($5, ''), $6)
		RETURNING created_at, updated_at
	`

	if err := tx.QueryRow(ctx, query,
		user.ID,
		user.GoogleID,
		user.FacebookID,
		user.Email,
		user.PasswordHash,
		user.IsActive,
	).Scan(&user.CreatedAt, &user.UpdatedAt); err != nil {
		return translateUserInsertError(err)
	}

	return nil
}

// translateUserInsertError converts expected users-table uniqueness failures
// into stable data-layer sentinels without exposing PostgreSQL details to
// boundary layers.
func translateUserInsertError(err error) error {
	if err == nil {
		return nil
	}

	if !IsUniqueViolation(err) {
		return fmt.Errorf("insert user: %w", err)
	}

	switch PgErrorConstraintName(err) {
	case "users_email_key":
		return ErrDuplicateEmail
	case "users_google_id_key":
		return ErrDuplicateGoogleID
	case "users_facebook_id_key":
		return ErrDuplicateFacebookID
	default:
		return ErrDuplicate
	}
}

// Authenticate verifies user identity using email/password, Google ID, or Facebook ID.
//
// Infrastructure errors from sub-lookups are surfaced directly so callers can
// distinguish 500-class failures from 401-class credential rejections.
func (m *UserModel) Authenticate(ctx context.Context, email, password, googleID, facebookID string) (*User, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("Authenticate")

	email = normalizeUserEmail(email)
	googleID = strings.TrimSpace(googleID)
	facebookID = strings.TrimSpace(facebookID)

	methodCount := 0
	for _, used := range []bool{email != "" && password != "", googleID != "", facebookID != ""} {
		if used {
			methodCount++
		}
	}
	if methodCount != 1 {
		logger.Warn("invalid authentication method count")
		return nil, ErrInvalidCredentials
	}

	var user *User
	var err error

	switch {
	case googleID != "":
		user, err = m.GetByGoogleID(ctx, googleID)
	case facebookID != "":
		user, err = m.GetByFacebookID(ctx, facebookID)
	default:
		user, err = m.getByEmailForLogin(ctx, email)
	}

	if err != nil {
		if errors.Is(err, ErrUserNotFound) {
			logger.Warn("credential lookup returned no user")
			return nil, ErrInvalidCredentials
		}
		logger.Error("credential lookup failed", err)
		return nil, fmt.Errorf("authenticate credential lookup: %w", err)
	}

	if email != "" && password != "" {
		if user.PasswordHash == "" || !security.CheckPasswordHash(password, user.PasswordHash) {
			logger.Warn("invalid password", "user_id", user.ID)
			return nil, ErrInvalidCredentials
		}
	}

	if user.DeletedAt != nil {
		logger.Warn("login rejected: account is deleted", "user_id", user.ID)
		return nil, ErrInvalidCredentials
	}
	if !user.IsActive {
		logger.Warn("login rejected: account is inactive", "user_id", user.ID)
		return nil, ErrInvalidCredentials
	}

	logger.Info("authentication successful", "user_id", user.ID)
	return user, nil
}

// getByEmailForLogin retrieves a non-deleted user by email for login.
func (m *UserModel) getByEmailForLogin(ctx context.Context, email string) (*User, error) {
	return m.getByUniqueField(ctx, "email", normalizeUserEmail(email))
}

// IsOAuthIDLinkedToOtherUser checks whether an OAuth ID is linked to another active user.
//
// NOTE: whether users.google_id / users.facebook_id remain the canonical OAuth
// linkage columns, versus that ownership having moved to UserExternalIdentityModel,
// is an open architecture question this consolidation does not resolve — see the
// SE review's Finding J. No change was made here pending that evidence.
func (m *UserModel) IsOAuthIDLinkedToOtherUser(ctx context.Context, currentUserID uuid.UUID, provider, oauthID string) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	if currentUserID == uuid.Nil {
		return false, errors.New("current user ID is required")
	}

	oauthID = strings.TrimSpace(oauthID)
	if oauthID == "" {
		return false, errors.New("OAuth ID cannot be empty")
	}

	var column string
	switch provider {
	case "google":
		column = "google_id"
	case "facebook":
		column = "facebook_id"
	default:
		return false, fmt.Errorf("unsupported provider: %s", provider)
	}

	query := fmt.Sprintf(`
		SELECT EXISTS (
			SELECT 1
			FROM users
			WHERE %s = $1
			  AND id <> $2
			  AND deleted_at IS NULL
		)
	`, column)

	var exists bool
	if err := m.DB.QueryRow(ctx, query, oauthID, currentUserID).Scan(&exists); err != nil {
		m.Logger.Error("failed to check OAuth ID linkage", "provider", provider, "error", err)
		return false, fmt.Errorf("check OAuth ID linkage: %w", err)
	}

	return exists, nil
}

// LinkOAuthID links a Google or Facebook OAuth ID to an active, non-deleted user.
//
// Uniqueness failures (the OAuth ID is already linked to a different user) are
// translated into typed data-layer sentinels rather than surfaced as opaque
// wrapped database errors, mirroring insertUserTx's duplicate-ID handling.
func (m *UserModel) LinkOAuthID(ctx context.Context, userID uuid.UUID, provider, oauthID string) error {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("LinkOAuthID")

	if userID == uuid.Nil {
		return errors.New("user ID is required")
	}

	oauthID = strings.TrimSpace(oauthID)
	if oauthID == "" {
		return errors.New("OAuth ID is required")
	}

	var column string
	switch provider {
	case "google":
		column = "google_id"
	case "facebook":
		column = "facebook_id"
	default:
		return fmt.Errorf("unsupported provider: %s", provider)
	}

	query := fmt.Sprintf(`
		UPDATE users
		SET %s = $1,
		    updated_at = NOW()
		WHERE id = $2
		  AND deleted_at IS NULL
		RETURNING updated_at
	`, column)

	var updatedAt time.Time
	if err := m.DB.QueryRow(ctx, query, oauthID, userID).Scan(&updatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrUserNotFound
		}

		if IsUniqueViolation(err) {
			logger.Warn(
				"OAuth ID already linked to a different user",
				"user_id", userID,
				"provider", provider,
			)
			switch provider {
			case "google":
				return ErrDuplicateGoogleID
			case "facebook":
				return ErrDuplicateFacebookID
			default:
				return ErrDuplicate
			}
		}

		logger.Error("OAuth link update failed", err, "user_id", userID, "provider", provider)
		return fmt.Errorf("link OAuth ID: %w", err)
	}

	logger.Info("OAuth ID linked", "user_id", userID, "provider", provider)
	return nil
}

// SetPasswordHash securely updates the password hash for a non-deleted user.
//
// This method persists an already-prepared hash only. Callers must hash
// plaintext material through internal/security before calling this method.
func (m *UserModel) SetPasswordHash(ctx context.Context, userID uuid.UUID, hash string) error {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("SetPasswordHash")

	if userID == uuid.Nil {
		return errors.New("user ID is required")
	}
	if strings.TrimSpace(hash) == "" {
		return errors.New("password hash is required")
	}

	const query = `
		UPDATE users
		SET password_hash = $1,
		    updated_at = NOW()
		WHERE id = $2
		  AND deleted_at IS NULL
		RETURNING updated_at
	`

	var updatedAt time.Time
	if err := m.DB.QueryRow(ctx, query, hash, userID).Scan(&updatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrUserNotFound
		}
		logger.Error("password hash update failed", err, "user_id", userID)
		return fmt.Errorf("update password hash: %w", err)
	}

	logger.Info("password hash updated", "user_id", userID)
	return nil
}

// SetPasswordHashTx persists an already-prepared password hash for an active,
// non-deleted user within a caller-owned transaction.
//
// This is the transactional counterpart to SetPasswordHash, required so the
// password-reset workflow can compose the password mutation atomically with
// reset-token consumption in one transaction. It never receives or hashes
// plaintext; callers must hash through internal/security first.
func (m *UserModel) SetPasswordHashTx(
	ctx context.Context,
	tx pgx.Tx,
	userID uuid.UUID,
	hash string,
) error {
	if tx == nil {
		return errors.New("transaction is required")
	}
	if userID == uuid.Nil {
		return errors.New("user ID is required")
	}
	if strings.TrimSpace(hash) == "" {
		return errors.New("password hash is required")
	}

	const query = `
		UPDATE users
		SET password_hash = $1,
		    updated_at = NOW()
		WHERE id = $2
		  AND deleted_at IS NULL
		  AND is_active = TRUE
		RETURNING updated_at
	`

	var updatedAt time.Time
	if err := tx.QueryRow(ctx, query, hash, userID).Scan(&updatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrUserNotFound
		}
		return fmt.Errorf("set password hash (tx): %w", err)
	}

	return nil
}

// getByUniqueField retrieves a non-deleted user by an allowlisted unique field.
// Returns ErrUserNotFound when no matching row exists.
func (m *UserModel) getByUniqueField(ctx context.Context, fieldName string, value any) (*User, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("getByUniqueField")

	if value == nil {
		return nil, fmt.Errorf("%s is required", fieldName)
	}

	allowedFields := map[string]string{
		"id":          "u.id",
		"email":       "u.email",
		"google_id":   "u.google_id",
		"facebook_id": "u.facebook_id",
	}

	column, ok := allowedFields[fieldName]
	if !ok {
		return nil, fmt.Errorf("unsupported user lookup field: %s", fieldName)
	}

	query := fmt.Sprintf(`
		SELECT %s
		FROM users u
		%s
		WHERE %s = $1
		  AND u.deleted_at IS NULL
		LIMIT 1
	`, userSelectColumns, userPrimaryRoleJoin, column)

	user, err := scanUser(m.DB.QueryRow(ctx, query, value))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrUserNotFound
		}
		logger.Error("user lookup failed", err, "field", fieldName)
		return nil, fmt.Errorf("query user by %s: %w", fieldName, err)
	}

	return user, nil
}

// GetByID retrieves a non-deleted user by UUID.
func (m *UserModel) GetByID(ctx context.Context, id uuid.UUID) (*User, error) {
	if id == uuid.Nil {
		return nil, errors.New("user ID is required")
	}
	return m.getByUniqueField(ctx, "id", id)
}

// GetByEmail retrieves a non-deleted user by email.
func (m *UserModel) GetByEmail(ctx context.Context, email string) (*User, error) {
	email = normalizeUserEmail(email)
	if email == "" {
		return nil, errors.New("email is required")
	}
	return m.getByUniqueField(ctx, "email", email)
}

// GetByGoogleID retrieves a non-deleted user by Google ID.
func (m *UserModel) GetByGoogleID(ctx context.Context, googleID string) (*User, error) {
	googleID = strings.TrimSpace(googleID)
	if googleID == "" {
		return nil, errors.New("google ID is required")
	}
	return m.getByUniqueField(ctx, "google_id", googleID)
}

// GetByFacebookID retrieves a non-deleted user by Facebook ID.
func (m *UserModel) GetByFacebookID(ctx context.Context, facebookID string) (*User, error) {
	facebookID = strings.TrimSpace(facebookID)
	if facebookID == "" {
		return nil, errors.New("facebook ID is required")
	}
	return m.getByUniqueField(ctx, "facebook_id", facebookID)
}

// GetUserContactInfo retrieves email, phone, and preferred contact method.
func (m *UserModel) GetUserContactInfo(ctx context.Context, userID uuid.UUID) (*UserContactInfo, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("GetUserContactInfo")

	if userID == uuid.Nil {
		return nil, errors.New("user ID is required")
	}

	const query = `
		SELECT
			u.email,
			COALESCE(up.phone, ''),
			COALESCE(up.preferred_contact, 'email')
		FROM users u
		LEFT JOIN user_profiles up
			ON up.user_id = u.id
			AND up.deleted_at IS NULL
		WHERE u.id = $1
		  AND u.deleted_at IS NULL
		LIMIT 1
	`

	info := &UserContactInfo{}
	if err := m.DB.QueryRow(ctx, query, userID).Scan(
		&info.Email,
		&info.Phone,
		&info.PreferredMethod,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrUserNotFound
		}
		logger.Error("contact info lookup failed", err, "user_id", userID)
		return nil, fmt.Errorf("get user contact info: %w", err)
	}

	return info, nil
}

// Update modifies core user fields for a non-deleted user.
//
// Update deliberately does not accept or apply role changes. Role assignment
// is owned by RoleModel and composed at the service layer; UserModel must
// never call into RoleModel. A future Service.ChangeUserRoleInternal (not
// implemented by this consolidation — no caller or requirement was in
// evidence) is the correct home for authenticated role changes outside signup.
func (m *UserModel) Update(ctx context.Context, user *User) error {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("UpdateUser")

	if user == nil {
		return errors.New("user object is required")
	}
	if user.ID == uuid.Nil {
		return errors.New("user ID is required")
	}

	user.Email = normalizeUserEmail(user.Email)
	user.GoogleID = cleanOptionalString(user.GoogleID)
	user.FacebookID = cleanOptionalString(user.FacebookID)

	if user.Email == "" {
		return errors.New("email is required")
	}

	const updateUser = `
		UPDATE users
		SET email = $1,
		    google_id = $2,
		    facebook_id = $3,
		    is_active = $4,
		    password_hash = CASE WHEN NULLIF($5, '') IS NULL THEN password_hash ELSE $5 END,
		    updated_at = NOW()
		WHERE id = $6
		  AND deleted_at IS NULL
		RETURNING updated_at
	`

	if err := m.DB.QueryRow(ctx, updateUser,
		user.Email,
		user.GoogleID,
		user.FacebookID,
		user.IsActive,
		user.PasswordHash,
		user.ID,
	).Scan(&user.UpdatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrUserNotFound
		}
		logger.Error("update user failed", err, "user_id", user.ID)
		return fmt.Errorf("update user: %w", err)
	}

	logger.Info("user updated", "user_id", user.ID)
	return nil
}

// SoftDeleteWithCascade soft-deletes the user and user-owned records atomically.
//
// This pool-backed entry point is for standalone account-closure operations.
// Workflows that must compose closure atomically with other persistence owners,
// such as activation-token cleanup, must use SoftDeleteWithCascadeTx with their
// caller-owned transaction.
func (m *UserModel) SoftDeleteWithCascade(ctx context.Context, userID uuid.UUID) error {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("SoftDeleteWithCascade")

	if userID == uuid.Nil {
		return errors.New("user ID is required")
	}

	tx, err := m.DB.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		logger.Error("begin soft delete transaction failed", err)
		return fmt.Errorf("begin soft delete transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	if err := m.SoftDeleteWithCascadeTx(ctx, tx, userID); err != nil {
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		logger.Error("commit soft delete transaction failed", err, "user_id", userID)
		return fmt.Errorf("commit soft delete transaction: %w", err)
	}

	logger.Info("user and user-owned records soft-deleted", "user_id", userID)
	return nil
}

// SoftDeleteWithCascadeTx soft-deletes the user and user-owned records inside a
// caller-owned transaction.
//
// This remains a UserModel-owned orchestration primitive rather than being
// delegated to each owning model's own Tx-scoped soft-delete method, because no
// such methods exist yet for user_profiles/user_settings/user_notifications/
// in the current evidence set. This
// is a documented, deliberate distributed-evolution deferral (BEG §18.6D):
// authorization safety does not depend on this cascade completing synchronously
// (users.deleted_at/is_active alone already deny access), so a future
// user.closed.v1 outbox event consumed by each owning domain remains a valid
// path if/when those domains are extracted. Not required for the current
// release.
func (m *UserModel) SoftDeleteWithCascadeTx(
	ctx context.Context,
	tx pgx.Tx,
	userID uuid.UUID,
) error {
	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("SoftDeleteWithCascadeTx")

	if tx == nil {
		return errors.New("transaction is required")
	}
	if userID == uuid.Nil {
		return errors.New("user ID is required")
	}

	const softDeleteUser = `
		UPDATE users
		SET deleted_at = NOW(),
		    updated_at = NOW(),
		    is_active = FALSE
		WHERE id = $1
		  AND deleted_at IS NULL
		RETURNING deleted_at
	`

	var deletedAt time.Time
	if err := tx.QueryRow(ctx, softDeleteUser, userID).Scan(&deletedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrUserNotFound
		}
		logger.Error("soft delete user failed", err, "user_id", userID)
		return fmt.Errorf("soft delete user: %w", err)
	}

	cascadeQueries := []string{
		`UPDATE user_profiles SET deleted_at = NOW(), updated_at = NOW() WHERE user_id = $1 AND deleted_at IS NULL`,
		`UPDATE user_settings SET deleted_at = NOW(), updated_at = NOW() WHERE user_id = $1 AND deleted_at IS NULL`,
		`UPDATE user_notifications SET deleted_at = NOW(), updated_at = NOW() WHERE user_id = $1 AND deleted_at IS NULL`,
		`UPDATE user_role_assignments SET deleted_at = NOW(), updated_at = NOW(), is_primary = FALSE WHERE user_id = $1 AND deleted_at IS NULL`,
	}

	for _, query := range cascadeQueries {
		if _, err := tx.Exec(ctx, query, userID); err != nil {
			logger.Error("cascade soft delete failed", err, "user_id", userID)
			return fmt.Errorf("cascade soft delete user-owned record: %w", err)
		}
	}

	return nil
}

// ListActive retrieves active, non-deleted users with pagination.
func (m *UserModel) ListActive(ctx context.Context, limit, offset int) ([]*User, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("ListActive")

	if limit <= 0 {
		return nil, errors.New("limit must be greater than zero")
	}
	if limit > 100 {
		limit = 100
	}
	if offset < 0 {
		return nil, errors.New("offset cannot be negative")
	}

	query := fmt.Sprintf(`
		SELECT %s
		FROM users u
		%s
		WHERE u.is_active = TRUE
		  AND u.deleted_at IS NULL
		ORDER BY u.created_at DESC
		LIMIT $1 OFFSET $2
	`, userSelectColumns, userPrimaryRoleJoin)

	rows, err := m.DB.Query(ctx, query, limit, offset)
	if err != nil {
		logger.Error("list active users query failed", err)
		return nil, fmt.Errorf("list active users: %w", err)
	}

	users, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (*User, error) {
		return scanUser(row)
	})
	if err != nil {
		logger.Error("collect active users failed", err)
		return nil, fmt.Errorf("collect active users: %w", err)
	}

	return users, nil
}

// SetActiveStatus updates the active status for a non-deleted user.
func (m *UserModel) SetActiveStatus(ctx context.Context, userID uuid.UUID, isActive bool) error {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("SetActiveStatus")

	if userID == uuid.Nil {
		return errors.New("user ID is required")
	}

	const query = `
		UPDATE users
		SET is_active = $1,
		    updated_at = NOW()
		WHERE id = $2
		  AND deleted_at IS NULL
		RETURNING updated_at
	`

	var updatedAt time.Time
	if err := m.DB.QueryRow(ctx, query, isActive, userID).Scan(&updatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrUserNotFound
		}
		logger.Error("set active status failed", err, "user_id", userID)
		return fmt.Errorf("set active status: %w", err)
	}

	logger.Info("user active status updated", "user_id", userID, "is_active", isActive)
	return nil
}

// Delete permanently deletes a user.
//
// This is intentionally a true hard delete, and it is intentionally guarded:
// only a user that has already been soft-deleted (deleted_at IS NOT NULL) may
// be permanently removed. This mirrors MerchantAccountModel.HardDelete's
// two-step safety gate and satisfies BEG §16.9 (hard deletion of retained
// actor records is exceptional administrative cleanup only, never a
// generally-callable primitive against a live account). Normal account
// removal remains SoftDeleteWithCascade / SoftDeleteWithCascadeTx.
func (m *UserModel) Delete(ctx context.Context, userID uuid.UUID) error {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("DeleteUser")

	if userID == uuid.Nil {
		return errors.New("user ID is required")
	}

	const query = `
		DELETE FROM users
		WHERE id = $1
		  AND deleted_at IS NOT NULL
		RETURNING id
	`

	var deletedID uuid.UUID
	if err := m.DB.QueryRow(ctx, query, userID).Scan(&deletedID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// Distinguish "does not exist" from "exists but not yet
			// soft-deleted" for operator clarity without an extra round trip
			// participating in the deletion's correctness.
			var exists bool
			existsErr := m.DB.QueryRow(
				ctx,
				`SELECT EXISTS (SELECT 1 FROM users WHERE id = $1)`,
				userID,
			).Scan(&exists)
			if existsErr == nil && exists {
				return fmt.Errorf(
					"user %s must be soft-deleted before it can be permanently deleted",
					userID,
				)
			}
			return ErrUserNotFound
		}
		logger.Error("hard delete user failed", err, "user_id", userID)
		return fmt.Errorf("hard delete user: %w", err)
	}

	logger.Info("user permanently deleted", "user_id", userID)
	return nil
}

// LockActivationEligibleUserTx locks a canonical non-deleted user within the
// caller-owned transaction and establishes that the account is still eligible
// for activation.
//
// Activation eligibility at the data boundary means the canonical account
// exists, has not been deleted, and is currently inactive.
//
// The method performs no mutation. The row remains locked until the caller
// commits or rolls back the supplied transaction.
func (m *UserModel) LockActivationEligibleUserTx(
	ctx context.Context,
	tx pgx.Tx,
	userID uuid.UUID,
) error {
	logger := m.Logger.
		GetLoggerWithContextFromContext(ctx).
		WithFunctionName(
			"LockActivationEligibleUserTx",
		)

	if tx == nil {
		return errors.New("transaction is required")
	}

	if userID == uuid.Nil {
		return errors.New("user ID is required")
	}

	const query = `
		SELECT is_active
		FROM users
		WHERE id = $1
		  AND deleted_at IS NULL
		FOR UPDATE
	`

	var isActive bool

	if err := tx.QueryRow(
		ctx,
		query,
		userID,
	).Scan(&isActive); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrUserNotFound
		}

		logger.Error(
			"activation eligibility lookup failed",
			"user_id", userID,
			"error", err,
		)

		return fmt.Errorf(
			"lock activation-eligible user: %w",
			err,
		)
	}

	if isActive {
		return ErrUserAlreadyActive
	}

	return nil
}

// LockActiveUserTx locks a canonical active, non-deleted user row within the
// caller-owned transaction.
//
// This is the eligibility primitive for workflows that require an already
// live account — currently password-reset issuance. It performs no mutation.
// A missing, inactive, or deleted user is reported uniformly as
// ErrUserNotFound: this method intentionally does not distinguish those cases
// from one another, since doing so at this layer would create a disclosure
// channel for privacy-sensitive callers (e.g. password-reset request) that
// must not reveal account existence or state.
func (m *UserModel) LockActiveUserTx(
	ctx context.Context,
	tx pgx.Tx,
	userID uuid.UUID,
) error {
	logger := m.Logger.
		GetLoggerWithContextFromContext(ctx).
		WithFunctionName("LockActiveUserTx")

	if tx == nil {
		return errors.New("transaction is required")
	}
	if userID == uuid.Nil {
		return errors.New("user ID is required")
	}

	const query = `
		SELECT 1
		FROM users
		WHERE id = $1
		  AND deleted_at IS NULL
		  AND is_active = TRUE
		FOR UPDATE
	`

	var sentinel int

	if err := tx.QueryRow(ctx, query, userID).Scan(&sentinel); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrUserNotFound
		}

		logger.Error(
			"active-user lock failed",
			"user_id", userID,
			"error", err,
		)

		return fmt.Errorf("lock active user: %w", err)
	}

	return nil
}

// ActivateUserTx performs the canonical inactive-to-active account transition
// within the caller-owned transaction.
//
// The caller is expected to have already locked and established eligibility
// through LockActivationEligibleUserTx in the same transaction.
//
// This method cannot deactivate users and cannot treat an already-active row as
// a successful activation. Activation must remain a genuine lifecycle
// transition.
func (m *UserModel) ActivateUserTx(
	ctx context.Context,
	tx pgx.Tx,
	userID uuid.UUID,
) error {
	logger := m.Logger.
		GetLoggerWithContextFromContext(ctx).
		WithFunctionName("ActivateUserTx")

	if tx == nil {
		return errors.New("transaction is required")
	}

	if userID == uuid.Nil {
		return errors.New("user ID is required")
	}

	const query = `
		UPDATE users
		SET is_active = TRUE,
		    updated_at = NOW()
		WHERE id = $1
		  AND deleted_at IS NULL
		  AND is_active = FALSE
		RETURNING id
	`

	var activatedUserID uuid.UUID

	if err := tx.QueryRow(
		ctx,
		query,
		userID,
	).Scan(&activatedUserID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			logger.Error(
				"activation transition invariant failed",
				"user_id", userID,
			)

			return fmt.Errorf(
				"activate user: eligible inactive user row was not available: %w",
				ErrUserAlreadyActive,
			)
		}

		logger.Error(
			"user activation update failed",
			"user_id", userID,
			"error", err,
		)

		return fmt.Errorf(
			"activate user: %w",
			err,
		)
	}

	return nil
}

// SignupPreparedTx persists one already-security-prepared signup inside the
// caller-owned transaction.
//
// For password signup, PasswordHash must already contain the protected password
// hash produced by the shared security package. The plaintext password never
// enters this method.
//
// IsActive is an authoritative lifecycle fact supplied by the owning workflow.
// Public email/password signup currently supplies false so account activation
// can prove contact-channel possession before enabling authentication.
//
// Exactly one of PasswordHash, GoogleID, or FacebookID must be present.
//
// This method performs ONLY users-table persistence. It does not resolve,
// validate, or assign any role. Role eligibility resolution and primary-role
// assignment are owned by RoleModel and are composed by the calling service
// within the same transaction — see Service.SignupUserInternal.
func (m *UserModel) SignupPreparedTx(
	ctx context.Context,
	tx pgx.Tx,
	user *User,
) (uuid.UUID, error) {
	if tx == nil {
		return uuid.Nil, errors.New("signup transaction is required")
	}
	if user == nil {
		return uuid.Nil, errors.New("user object is required")
	}

	user.Email = normalizeUserEmail(user.Email)
	user.GoogleID = cleanOptionalString(user.GoogleID)
	user.FacebookID = cleanOptionalString(user.FacebookID)

	if user.Email == "" {
		return uuid.Nil, errors.New("email is required")
	}

	usingPassword := strings.TrimSpace(user.PasswordHash) != ""
	usingGoogle := user.GoogleID != nil
	usingFacebook := user.FacebookID != nil

	methodCount := 0
	for _, used := range []bool{
		usingPassword,
		usingGoogle,
		usingFacebook,
	} {
		if used {
			methodCount++
		}
	}

	if methodCount != 1 {
		return uuid.Nil, errors.New(
			"exactly one signup authentication method is required",
		)
	}

	if user.ID == uuid.Nil {
		user.ID = uuid.New()
	}

	user.DeletedAt = nil

	if err := m.insertUserTx(ctx, tx, user); err != nil {
		return uuid.Nil, err
	}

	return user.ID, nil
}

// SetInitialPasswordHash atomically establishes passwordHash only for an
// active, non-deleted account that currently has no password.
//
// The returned boolean is true only when this call performed the transition.
// A false result means the caller must classify the miss if it needs to
// distinguish not-found, inactive, and already-established states.
func (m *UserModel) SetInitialPasswordHash(
	ctx context.Context,
	userID uuid.UUID,
	passwordHash string,
) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	if userID == uuid.Nil {
		return false, errors.New("user ID is required")
	}

	passwordHash = strings.TrimSpace(passwordHash)
	if passwordHash == "" {
		return false, errors.New("password hash is required")
	}

	const query = `
		UPDATE users
		SET
			password_hash = $1,
			updated_at = NOW()
		WHERE id = $2
		  AND deleted_at IS NULL
		  AND is_active = TRUE
		  AND password_hash IS NULL
	`

	result, err := m.DB.Exec(
		ctx,
		query,
		passwordHash,
		userID,
	)
	if err != nil {
		return false, fmt.Errorf(
			"set initial password hash: %w",
			err,
		)
	}

	return result.RowsAffected() == 1, nil
}
