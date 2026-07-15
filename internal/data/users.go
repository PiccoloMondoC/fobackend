// Package data provides models and database access methods for users and auth-adjacent user workflows.
//
// sdworkspace/sdbackend/internal/data/users.go
//
// GTM:
//   Layer: 2.2 Identity / Auth Domain
//   Release Class: SPINE
//   Reason:
//     Users are release-critical identity and account lifecycle infrastructure.
//     This file owns canonical user persistence, registration, authentication,
//     OAuth account linking, password hashing, password reset, primary role
//     assignment, contact-info lookup, active-state control, and account
//     soft-delete cascade behavior required by the initial SagrentiDeals release
//     spine.
//
// SPINE Rule:
//   Keep compiling.
//   Keep production-ready.
//   Preserve canonical users table alignment.
//   Preserve exactly-one registration method enforcement.
//   Preserve password hashing and protected reset-token persistence.
//   Preserve OAuth account-linking integrity.
//   Preserve primary-role assignment through user_role_assignments.
//   Preserve soft-delete cascade behavior for user-owned records.
//   Preserve DB-owned lifecycle timestamp behavior.
//   Block deployment if this file breaks build, user registration,
//   authentication, password reset, OAuth linking, role assignment,
//   account lifecycle, or identity integrity.
package data

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/PiccoloMondoC/sdworkspace/sdbackend/internal/logging"
	"github.com/PiccoloMondoC/sdworkspace/sdbackend/internal/security"
	"github.com/PiccoloMondoC/sdworkspace/sdbackend/internal/utils/timeutil"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// User represents the canonical users table plus derived primary-role metadata.
//
// RoleID and RoleName are derived from user_role_assignments + roles. They are
// not columns on users and must not be inserted into or updated on users.
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

// PasswordReset represents a password reset request.
type PasswordReset struct {
	ID         uuid.UUID `json:"id" db:"id"`
	UserID     uuid.UUID `json:"user_id" db:"user_id"`
	ResetToken string    `json:"-" db:"reset_token"`
	ExpiresAt  time.Time `json:"expires_at" db:"expires_at"`
	CreatedAt  time.Time `json:"created_at" db:"created_at"`
}

// UserModel is the data model for users.
type UserModel struct {
	DB     *pgxpool.Pool
	Logger *logging.Logger
}

// PasswordResetModel is retained because Models currently exposes it.
// Password reset behavior remains on UserModel for method-surface continuity.
type PasswordResetModel struct {
	DB     *pgxpool.Pool
	Logger *logging.Logger
}

type scanner interface {
	Scan(dest ...any) error
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

const userPrimaryRoleJoin = `
	LEFT JOIN user_role_assignments ura
		ON ura.user_id = u.id
		AND ura.is_primary = TRUE
		AND ura.deleted_at IS NULL
	LEFT JOIN roles r
		ON r.id = ura.role_id
		AND r.deleted_at IS NULL
`

// scanUser scans the canonical user row plus derived primary-role metadata.
func scanUser(row scanner) (*User, error) {
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

// hashResetToken returns a stable protected representation for reset-token persistence.
func hashResetToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// newSecureResetToken creates an opaque reset token suitable for outbound delivery.
func newSecureResetToken() (string, error) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("generate secure reset token: %w", err)
	}

	return base64.RawURLEncoding.EncodeToString(b[:]), nil
}

// Insert inserts a new email/password user and assigns the provided primary role atomically.
//
// This method preserves the existing method surface for callers that create users
// with an explicit role. Registration should normally use Register(), which assigns
// the default customer role.
func (m *UserModel) Insert(ctx context.Context, user *User, plainPassword string) error {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("InsertUser")

	if user == nil {
		err := errors.New("user object is required")
		logger.Error("validation failed", err)
		return err
	}

	user.Email = normalizeUserEmail(user.Email)
	user.GoogleID = cleanOptionalString(user.GoogleID)
	user.FacebookID = cleanOptionalString(user.FacebookID)

	if user.Email == "" {
		err := errors.New("email is required")
		logger.Error("validation failed", err)
		return err
	}
	if plainPassword == "" {
		err := errors.New("password is required")
		logger.Error("validation failed", err)
		return err
	}
	if user.RoleID == uuid.Nil {
		err := errors.New("role ID is required")
		logger.Error("validation failed", err)
		return err
	}

	hashedPassword, err := security.HashPassword(plainPassword)
	if err != nil {
		logger.Error("password hashing failed", err)
		return err
	}

	tx, err := m.DB.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		logger.Error("begin transaction failed", err)
		return fmt.Errorf("begin insert user transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	if err := m.ensureAssignableRoleTx(ctx, tx, user.RoleID); err != nil {
		logger.Error("role validation failed", err, "role_id", user.RoleID)
		return err
	}

	if user.ID == uuid.Nil {
		user.ID = uuid.New()
	}

	user.PasswordHash = hashedPassword
	user.IsActive = true
	user.DeletedAt = nil

	if err := m.insertUserTx(ctx, tx, user); err != nil {
		logger.Error("insert user failed", err)
		return err
	}

	if err := m.assignPrimaryRoleTx(ctx, tx, user.ID, user.RoleID, nil); err != nil {
		logger.Error("assign primary role failed", err, "user_id", user.ID, "role_id", user.RoleID)
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		logger.Error("commit transaction failed", err, "user_id", user.ID)
		return fmt.Errorf("commit insert user transaction: %w", err)
	}

	logger.Info("user inserted successfully", "user_id", user.ID)
	return nil
}

// Register creates a new user account using exactly one auth method:
// email/password, Google OAuth, or Facebook OAuth.
//
// Registration assigns the default customer role through user_role_assignments.
func (m *UserModel) Register(ctx context.Context, user *User, plainPassword string) (uuid.UUID, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("Register")

	if user == nil {
		err := errors.New("user object is required")
		logger.Error("validation failed", err)
		return uuid.Nil, err
	}

	user.Email = normalizeUserEmail(user.Email)
	user.GoogleID = cleanOptionalString(user.GoogleID)
	user.FacebookID = cleanOptionalString(user.FacebookID)

	if user.Email == "" {
		err := errors.New("email is required")
		logger.Error("validation failed", err)
		return uuid.Nil, err
	}

	usingGoogle := user.GoogleID != nil
	usingFacebook := user.FacebookID != nil
	usingPassword := strings.TrimSpace(plainPassword) != ""

	methodCount := 0
	for _, used := range []bool{usingGoogle, usingFacebook, usingPassword} {
		if used {
			methodCount++
		}
	}
	if methodCount != 1 {
		err := errors.New("exactly one registration method is required")
		logger.Error("validation failed", err)
		return uuid.Nil, err
	}

	var hashedPassword string
	var err error
	if usingPassword {
		hashedPassword, err = security.HashPassword(plainPassword)
		if err != nil {
			logger.Error("password hashing failed", err)
			return uuid.Nil, err
		}
	}

	tx, err := m.DB.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		logger.Error("begin transaction failed", err)
		return uuid.Nil, fmt.Errorf("begin register transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	roleID, err := m.getDefaultCustomerRoleIDTx(ctx, tx)
	if err != nil {
		logger.Error("default role lookup failed", err)
		return uuid.Nil, err
	}

	if user.ID == uuid.Nil {
		user.ID = uuid.New()
	}

	user.RoleID = roleID
	user.PasswordHash = hashedPassword
	user.IsActive = true
	user.DeletedAt = nil

	if err := m.insertUserTx(ctx, tx, user); err != nil {
		logger.Error("insert registered user failed", err)
		return uuid.Nil, err
	}

	if err := m.assignPrimaryRoleTx(ctx, tx, user.ID, roleID, nil); err != nil {
		logger.Error("assign default primary role failed", err, "user_id", user.ID, "role_id", roleID)
		return uuid.Nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		logger.Error("commit register transaction failed", err, "user_id", user.ID)
		return uuid.Nil, fmt.Errorf("commit register transaction: %w", err)
	}

	logger.Info("user registered", "user_id", user.ID)
	return user.ID, nil
}

// insertUserTx inserts only columns that actually exist on users.
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

	return tx.QueryRow(ctx, query,
		user.ID,
		user.GoogleID,
		user.FacebookID,
		user.Email,
		user.PasswordHash,
		user.IsActive,
	).Scan(&user.CreatedAt, &user.UpdatedAt)
}

// getDefaultCustomerRoleIDTx retrieves the signup role.
func (m *UserModel) getDefaultCustomerRoleIDTx(ctx context.Context, tx pgx.Tx) (uuid.UUID, error) {
	var roleID uuid.UUID

	const query = `
		SELECT id
		FROM roles
		WHERE name = 'customer'
		  AND is_active = TRUE
		  AND deleted_at IS NULL
		LIMIT 1
	`

	if err := tx.QueryRow(ctx, query).Scan(&roleID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return uuid.Nil, errors.New("default customer role is missing or inactive")
		}
		return uuid.Nil, fmt.Errorf("lookup default customer role: %w", err)
	}

	return roleID, nil
}

// ensureAssignableRoleTx verifies that a role can be assigned.
func (m *UserModel) ensureAssignableRoleTx(ctx context.Context, tx pgx.Tx, roleID uuid.UUID) error {
	var isActive bool

	const query = `
		SELECT is_active
		FROM roles
		WHERE id = $1
		  AND deleted_at IS NULL
	`

	if err := tx.QueryRow(ctx, query, roleID).Scan(&isActive); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("role not found: %s", roleID)
		}
		return fmt.Errorf("lookup role: %w", err)
	}

	if !isActive {
		return errors.New("cannot assign an inactive role")
	}

	return nil
}

// assignPrimaryRoleTx assigns or restores the user's primary role.
func (m *UserModel) assignPrimaryRoleTx(ctx context.Context, tx pgx.Tx, userID, roleID uuid.UUID, assignedBy *uuid.UUID) error {
	const clearExistingPrimary = `
		UPDATE user_role_assignments
		SET is_primary = FALSE,
		    updated_at = NOW()
		WHERE user_id = $1
		  AND is_primary = TRUE
		  AND deleted_at IS NULL
	`

	if _, err := tx.Exec(ctx, clearExistingPrimary, userID); err != nil {
		return fmt.Errorf("clear existing primary roles: %w", err)
	}

	const upsertPrimaryRole = `
		INSERT INTO user_role_assignments (
			user_id,
			role_id,
			assigned_by,
			is_primary
		)
		VALUES ($1, $2, $3, TRUE)
		ON CONFLICT (user_id, role_id)
		DO UPDATE SET
			is_primary = TRUE,
			assigned_by = EXCLUDED.assigned_by,
			deleted_at = NULL,
			updated_at = NOW()
		RETURNING updated_at
	`

	var updatedAt time.Time
	if err := tx.QueryRow(ctx, upsertPrimaryRole, userID, roleID, assignedBy).Scan(&updatedAt); err != nil {
		return fmt.Errorf("assign primary role: %w", err)
	}

	return nil
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
		logger.Error("OAuth link update failed", err, "user_id", userID, "provider", provider)
		return fmt.Errorf("link OAuth ID: %w", err)
	}

	logger.Info("OAuth ID linked", "user_id", userID, "provider", provider)
	return nil
}

// SetPasswordHash securely updates the password hash for a non-deleted user.
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

// Update modifies core user fields and, when RoleID is present, the primary role.
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

	tx, err := m.DB.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		logger.Error("begin update transaction failed", err)
		return fmt.Errorf("begin update user transaction: %w", err)
	}
	defer tx.Rollback(ctx)

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

	if err := tx.QueryRow(ctx, updateUser,
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

	if user.RoleID != uuid.Nil {
		if err := m.ensureAssignableRoleTx(ctx, tx, user.RoleID); err != nil {
			logger.Error("role validation failed", err, "user_id", user.ID, "role_id", user.RoleID)
			return err
		}
		if err := m.assignPrimaryRoleTx(ctx, tx, user.ID, user.RoleID, nil); err != nil {
			logger.Error("assign primary role failed", err, "user_id", user.ID, "role_id", user.RoleID)
			return err
		}
	}

	if err := tx.Commit(ctx); err != nil {
		logger.Error("commit update transaction failed", err, "user_id", user.ID)
		return fmt.Errorf("commit update user transaction: %w", err)
	}

	logger.Info("user updated", "user_id", user.ID)
	return nil
}

// SoftDeleteWithCascade soft-deletes the user and user-owned records atomically.
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
		`UPDATE user_favorites SET deleted_at = NOW(), updated_at = NOW() WHERE user_id = $1 AND deleted_at IS NULL`,
		`UPDATE user_dashboards SET deleted_at = NOW(), updated_at = NOW() WHERE user_id = $1 AND deleted_at IS NULL`,
		`UPDATE user_wallets SET deleted_at = NOW(), updated_at = NOW(), is_active = FALSE WHERE user_id = $1 AND deleted_at IS NULL`,
		`UPDATE user_role_assignments SET deleted_at = NOW(), updated_at = NOW(), is_primary = FALSE WHERE user_id = $1 AND deleted_at IS NULL`,
	}

	for _, query := range cascadeQueries {
		if _, err := tx.Exec(ctx, query, userID); err != nil {
			logger.Error("cascade soft delete failed", err, "user_id", userID)
			return fmt.Errorf("cascade soft delete user-owned record: %w", err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		logger.Error("commit soft delete transaction failed", err, "user_id", userID)
		return fmt.Errorf("commit soft delete transaction: %w", err)
	}

	logger.Info("user and user-owned records soft-deleted", "user_id", userID)
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

// ChangePassword validates the current password and stores the new password hash.
func (m *UserModel) ChangePassword(ctx context.Context, userID uuid.UUID, currentPasswordPlain, newPasswordPlain, confirmNewPasswordPlain string) error {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("ChangePassword")

	if userID == uuid.Nil {
		return errors.New("user ID is required")
	}
	if currentPasswordPlain == "" || newPasswordPlain == "" || confirmNewPasswordPlain == "" {
		return errors.New("all password fields are required")
	}
	if newPasswordPlain != confirmNewPasswordPlain {
		return errors.New("new password and confirmation do not match")
	}

	var storedHash string
	const getHash = `
		SELECT COALESCE(password_hash, '')
		FROM users
		WHERE id = $1
		  AND deleted_at IS NULL
		  AND is_active = TRUE
	`

	if err := m.DB.QueryRow(ctx, getHash, userID).Scan(&storedHash); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrUserNotFound
		}
		logger.Error("password hash lookup failed", err, "user_id", userID)
		return fmt.Errorf("lookup password hash: %w", err)
	}

	if storedHash == "" || !security.CheckPasswordHash(currentPasswordPlain, storedHash) {
		return errors.New("current password is incorrect")
	}

	newHash, err := security.HashPassword(newPasswordPlain)
	if err != nil {
		logger.Error("new password hashing failed", err, "user_id", userID)
		return fmt.Errorf("hash new password: %w", err)
	}

	return m.SetPasswordHash(ctx, userID, newHash)
}

// GenerateResetToken generates and stores a protected password reset token.
//
// The returned token is plaintext for outbound delivery only. The database stores
// a SHA-256-derived value in password_resets.reset_token to avoid persisting the
// bearer token itself. The expiry is DB-owned via NOW() + INTERVAL '1 hour'.
// User existence and active state are enforced inside the INSERT ... SELECT so
// the existence check and the upsert are atomic with no extra round trip.
func (m *UserModel) GenerateResetToken(ctx context.Context, userID uuid.UUID) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()
	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("GenerateResetToken")
	if userID == uuid.Nil {
		err := errors.New("user ID is required")
		logger.Error("validation failed", err)
		return "", err
	}
	resetToken, err := newSecureResetToken()
	if err != nil {
		logger.Error("secure reset token generation failed", err, "user_id", userID)
		return "", err
	}
	resetTokenHash := hashResetToken(resetToken)
	const query = `
		INSERT INTO password_resets (
			user_id,
			reset_token,
			expires_at
		)
		SELECT
			u.id,
			$2,
			NOW() + INTERVAL '1 hour'
		FROM users u
		WHERE u.id = $1
		  AND u.deleted_at IS NULL
		  AND u.is_active = TRUE
		ON CONFLICT (user_id)
		DO UPDATE SET
			reset_token = EXCLUDED.reset_token,
			expires_at = EXCLUDED.expires_at
		RETURNING expires_at, created_at
	`
	var expiresAt time.Time
	var createdAt time.Time
	if err := m.DB.QueryRow(ctx, query, userID, resetTokenHash).Scan(&expiresAt, &createdAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			logger.Warn("reset token skipped: user not found or inactive", "user_id", userID)
			return "", ErrUserNotFound
		}
		logger.Error("upsert password reset token failed", err, "user_id", userID)
		return "", fmt.Errorf("generate reset token: %w", err)
	}
	logger.Info("password reset token generated", "user_id", userID, "expires_at", expiresAt, "created_at", createdAt)
	return resetToken, nil
}

// ResetPassword validates a reset token, hashes the new plaintext password,
// updates the password, and hard-deletes the consumed token.
func (m *UserModel) ResetPassword(ctx context.Context, resetToken string, newPasswordPlain string) error {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()
	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("ResetPassword")
	resetToken = strings.TrimSpace(resetToken)
	if resetToken == "" {
		err := errors.New("reset token is required")
		logger.Error("validation failed", err)
		return err
	}
	if newPasswordPlain == "" {
		err := errors.New("new password is required")
		logger.Error("validation failed", err)
		return err
	}
	newPasswordHash, err := security.HashPassword(newPasswordPlain)
	if err != nil {
		logger.Error("password hashing failed", err)
		return fmt.Errorf("hash reset password: %w", err)
	}
	resetTokenHash := hashResetToken(resetToken)
	tx, err := m.DB.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		logger.Error("begin reset password transaction failed", err)
		return fmt.Errorf("begin reset password transaction: %w", err)
	}
	defer tx.Rollback(ctx)
	var userID uuid.UUID
	var expiresAt time.Time
	const lookup = `
		SELECT pr.user_id, pr.expires_at
		FROM password_resets pr
		JOIN users u ON u.id = pr.user_id
		WHERE pr.reset_token = $1
		  AND u.deleted_at IS NULL
		  AND u.is_active = TRUE
	`
	if err := tx.QueryRow(ctx, lookup, resetTokenHash).Scan(&userID, &expiresAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			logger.Warn("invalid reset token")
			return ErrInvalidResetToken
		}
		logger.Error("reset token lookup failed", err)
		return fmt.Errorf("lookup reset token: %w", err)
	}
	if timeutil.Now().After(expiresAt) {
		_, _ = tx.Exec(ctx, `DELETE FROM password_resets WHERE reset_token = $1`, resetTokenHash)
		return ErrInvalidResetToken
	}
	const updatePassword = `
		UPDATE users
		SET password_hash = $1,
		    updated_at = NOW()
		WHERE id = $2
		  AND deleted_at IS NULL
		  AND is_active = TRUE
		RETURNING updated_at
	`
	var updatedAt time.Time
	if err := tx.QueryRow(ctx, updatePassword, newPasswordHash, userID).Scan(&updatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrUserNotFound
		}
		logger.Error("reset password update failed", err, "user_id", userID)
		return fmt.Errorf("update password: %w", err)
	}
	if err := tx.QueryRow(ctx,
		`DELETE FROM password_resets WHERE reset_token = $1 RETURNING user_id`,
		resetTokenHash,
	).Scan(&userID); err != nil {
		logger.Error("delete used reset token failed", err, "user_id", userID)
		return fmt.Errorf("delete used reset token: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		logger.Error("commit reset password transaction failed", err, "user_id", userID)
		return fmt.Errorf("commit reset password transaction: %w", err)
	}
	logger.Info("password reset completed", "user_id", userID, "updated_at", updatedAt)
	return nil
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
// This is intentionally a true hard delete. Normal account removal should use
// SoftDeleteWithCascade.
func (m *UserModel) Delete(ctx context.Context, userID uuid.UUID) error {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("DeleteUser")

	if userID == uuid.Nil {
		return errors.New("user ID is required")
	}

	const query = `DELETE FROM users WHERE id = $1 RETURNING id`

	var deletedID uuid.UUID
	if err := m.DB.QueryRow(ctx, query, userID).Scan(&deletedID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrUserNotFound
		}
		logger.Error("hard delete user failed", err, "user_id", userID)
		return fmt.Errorf("hard delete user: %w", err)
	}

	logger.Info("user permanently deleted", "user_id", userID)
	return nil
}