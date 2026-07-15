// Package data provides models and database access methods for user activation and other entities.
//
// sdworkspace/sdbackend/internal/data/user_activation.go
//
// GTM:
//   Layer: 2.2 Identity / Auth Domain
//   Release Class: SPINE
//   Reason:
//     User activation is release-critical identity infrastructure. It protects
//     account activation, activation-token generation, token-hash persistence,
//     activation status reads, atomic user activation, and expired-token cleanup
//     required by v1 account lifecycle behavior.
//
// SPINE Rule:
//   Keep compiling.
//   Keep production-ready.
//   Preserve plaintext-token boundary handling only at controlled outputs/inputs.
//   Preserve token_hash persistence and no raw-token JSON/log exposure.
//   Preserve atomic activation transaction behavior.
//   Preserve DB-owned activation-token expiry and lifecycle timestamp behavior.
//   Preserve hard deletion for transient expired activation-token material.
//   Block deployment if this file breaks build, activation-token generation,
//   user activation, token cleanup, or account lifecycle integrity.
package data

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/PiccoloMondoC/sdworkspace/sdbackend/internal/logging"
	"github.com/PiccoloMondoC/sdworkspace/sdbackend/internal/security"
	"github.com/PiccoloMondoC/sdworkspace/sdbackend/internal/utils/timeutil"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ActivationToken represents the canonical persisted activation-token record.
//
// Protected token material is never exposed via JSON. Only the one-way token hash
// is persisted, and it remains internal to the data layer.
type ActivationToken struct {
	ID        uuid.UUID `json:"id" db:"id"`
	UserID    uuid.UUID `json:"user_id" db:"user_id"`
	TokenHash string    `json:"-" db:"token_hash"`
	ExpiresAt time.Time `json:"expires_at" db:"expires_at"`
	CreatedAt time.Time `json:"created_at" db:"created_at"`
	UpdatedAt time.Time `json:"updated_at" db:"updated_at"`
}

// ActivationTokenModel is the structure which holds the DB instance.
type ActivationTokenModel struct {
	DB     *pgxpool.Pool
	Logger *logging.Logger
}

// hashActivationToken returns the canonical one-way hash used for persistence and lookup.
func hashActivationToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// CreateActivationToken generates a new activation token for a user.
//
// The plaintext token is returned only to the caller so it can be delivered across the
// activation boundary. Canonical persistence stores only the token hash. The database
// owns the persisted expires_at value.
func (m *ActivationTokenModel) CreateActivationToken(ctx context.Context, userID uuid.UUID) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("CreateActivationToken")

	token, err := security.GenerateRandomToken(32)
	if err != nil {
		logger.Error("Failed to generate activation token", "error", err, "user_id", userID)
		return "", fmt.Errorf("generate activation token: %w", err)
	}

	tokenHash := hashActivationToken(token)

	const query = `
		INSERT INTO activation_tokens (user_id, token_hash, expires_at, updated_at)
		SELECT u.id, $2, NOW() + INTERVAL '24 hours', NOW()
		FROM users u
		WHERE u.id = $1
		  AND u.deleted_at IS NULL
		ON CONFLICT (user_id) DO UPDATE
		SET token_hash = EXCLUDED.token_hash,
			expires_at = EXCLUDED.expires_at,
			updated_at = NOW()
		RETURNING expires_at
	`

	var expiresAt time.Time
	err = m.DB.QueryRow(ctx, query, userID, tokenHash).Scan(&expiresAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			logger.Warn("Cannot create activation token for missing or deleted user", "user_id", userID)
			return "", ErrUserNotFound
		}

		logger.Error("Failed to create activation token", "error", err, "user_id", userID)
		return "", fmt.Errorf("create activation token: %w", err)
	}

	logger.Info("Activation token created", "user_id", userID, "expires_at", expiresAt)
	return token, nil
}

// GetUserActivationStatus checks whether a user account is active.
func (m *ActivationTokenModel) GetUserActivationStatus(ctx context.Context, userID uuid.UUID) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("GetUserActivationStatus")

	var isActive bool
	const query = `
		SELECT is_active
		FROM users
		WHERE id = $1
		  AND deleted_at IS NULL
	`

	err := m.DB.QueryRow(ctx, query, userID).Scan(&isActive)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			logger.Warn("User not found", "user_id", userID)
			return false, ErrUserNotFound
		}

		logger.Error("Failed to get activation status", "error", err, "user_id", userID)
		return false, fmt.Errorf("get activation status: %w", err)
	}

	return isActive, nil
}

// ActivateUser validates a plaintext activation token, activates the user,
// and deletes the consumed activation-token row atomically.
func (m *ActivationTokenModel) ActivateUser(ctx context.Context, plainToken string) (uuid.UUID, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("ActivateUser")

	if plainToken == "" {
		return uuid.Nil, ErrActivationTokenRequired
	}

	tokenHash := hashActivationToken(plainToken)

	tx, err := m.DB.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		logger.Error("Failed to begin activation transaction", "error", err)
		return uuid.Nil, fmt.Errorf("begin activation transaction: %w", err)
	}

	defer func() {
		if rbErr := tx.Rollback(ctx); rbErr != nil && !errors.Is(rbErr, pgx.ErrTxClosed) {
			logger.Warn("Activation transaction rollback failed", "error", rbErr)
		}
	}()

	var userID uuid.UUID
	var expiresAt time.Time

	const queryToken = `
		SELECT user_id, expires_at
		FROM activation_tokens
		WHERE token_hash = $1
		FOR UPDATE
	`

	err = tx.QueryRow(ctx, queryToken, tokenHash).Scan(&userID, &expiresAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			logger.Warn("Activation token not found")
			return uuid.Nil, ErrActivationTokenInvalid
		}

		logger.Error("Failed to load activation token", "error", err)
		return uuid.Nil, fmt.Errorf("load activation token: %w", err)
	}

	// Read-side comparison may use the canonical UTC helper.
	if !expiresAt.After(timeutil.Now()) {
		logger.Warn("Activation token expired", "user_id", userID, "expires_at", expiresAt)
		return uuid.Nil, ErrActivationTokenExpired
	}

	const queryActivate = `
		UPDATE users
		SET is_active = TRUE,
			updated_at = NOW()
		WHERE id = $1
		  AND deleted_at IS NULL
		RETURNING id
	`

	var activatedUserID uuid.UUID
	err = tx.QueryRow(ctx, queryActivate, userID).Scan(&activatedUserID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			logger.Warn("Activation target user not found or deleted", "user_id", userID)
			return uuid.Nil, ErrUserNotFound
		}

		logger.Error("Failed to activate user", "error", err, "user_id", userID)
		return uuid.Nil, fmt.Errorf("activate user: %w", err)
	}

	const queryDelete = `
		DELETE FROM activation_tokens
		WHERE user_id = $1
		RETURNING id
	`

	var deletedTokenID uuid.UUID
	err = tx.QueryRow(ctx, queryDelete, activatedUserID).Scan(&deletedTokenID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			logger.Warn("Activation token row missing during post-activation delete", "user_id", activatedUserID)
			return uuid.Nil, ErrActivationTokenNotFound
		}

		logger.Error("Failed to delete activation token after activation", "error", err, "user_id", activatedUserID)
		return uuid.Nil, fmt.Errorf("%w: %v", ErrActivationTokenDeleteFailed, err)
	}

	if err := tx.Commit(ctx); err != nil {
		logger.Error("Failed to commit activation transaction", "error", err, "user_id", activatedUserID)
		return uuid.Nil, fmt.Errorf("commit activation transaction: %w", err)
	}

	logger.Info("User activated successfully", "user_id", activatedUserID, "deleted_token_id", deletedTokenID)
	return activatedUserID, nil
}

// DeleteExpiredActivationTokens removes expired activation-token rows.
//
// Activation tokens are transient proof material, so hard deletion after expiry
// is the correct lifecycle.
func (m *ActivationTokenModel) DeleteExpiredActivationTokens(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("DeleteExpiredActivationTokens")

	const query = `
		DELETE FROM activation_tokens
		WHERE expires_at < NOW()
	`

	tag, err := m.DB.Exec(ctx, query)
	if err != nil {
		logger.Error("Failed to delete expired activation tokens", "error", err)
		return fmt.Errorf("delete expired activation tokens: %w", err)
	}

	logger.Info("Expired activation tokens deleted", "rows_affected", tag.RowsAffected())
	return nil
}