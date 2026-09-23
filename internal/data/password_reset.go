// Package data provides models and database access methods for password-reset
// token persistence.
//
// focodebase/fobackend/internal/data/password_reset.go
//
// GTM:
//
//	Layer: 2.2 Identity / Auth Domain
//	Release Class: SPINE
//	Reason:
//	  Password-reset token persistence is release-critical identity
//	  infrastructure. This file is the sole canonical owner of
//	  password_resets: protected token-hash persistence, non-locking owner
//	  discovery required for canonical cross-model lock ordering,
//	  transactional token locking and expiry evaluation, transactional token
//	  consumption, and expired-token cleanup.
//
//	  This file supersedes the prior arrangement in which an empty
//	  PasswordResetModel placeholder existed in Models while all
//	  password-reset behavior actually lived on UserModel "for method-surface
//	  continuity." That arrangement is retired; this file is now the real,
//	  fully-implemented owner.
//
//	  Canonical user state is not owned here. User existence, active state,
//	  and password-hash mutation belong to UserModel. Atomic password reset is
//	  composed outside this model by Service.ResetPasswordInternal, which
//	  calls PasswordResetModel and UserModel within one transaction — the same
//	  shape already used correctly for account activation.
//
// SPINE Rule:
//
//	Keep compiling.
//	Keep production-ready.
//	Preserve plaintext reset tokens only at controlled inputs and outputs.
//	Persist only protected token hashes via internal/security; never persist
//	or log plaintext tokens.
//	Preserve DB-owned reset-token expiry and lifecycle timestamps.
//	Preserve expires_at <= NOW() as the canonical expired-token boundary.
//	Preserve hard deletion for transient expired reset-token material.
//	Preserve strict password_resets ownership: this file must never read or
//	write canonical users state.
//	Preserve caller-owned transaction composition for cross-model password
//	reset.
//	Preserve LookupResetTokenOwnerTx as a non-locking, non-authoritative
//	owner-discovery primitive only; it must never substitute for authoritative
//	credential locking and validation.
//	Block deployment if this file breaks build, reset-token generation,
//	protected persistence, owner discovery, token validation, token
//	consumption, expired-token cleanup, or the password_resets/users
//	ownership boundary.
package data

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/PiccoloMondoC/focodebase/fobackend/internal/logging"
	"github.com/PiccoloMondoC/focodebase/fobackend/internal/security"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PasswordResetToken represents the canonical persisted password-reset token
// record.
//
// Plaintext reset credentials are never persisted or exposed through this
// structure. TokenHash contains only the one-way hash stored in
// password_resets.
type PasswordResetToken struct {
	ID        uuid.UUID `json:"id" db:"id"`
	UserID    uuid.UUID `json:"user_id" db:"user_id"`
	TokenHash string    `json:"-" db:"token_hash"`
	ExpiresAt time.Time `json:"expires_at" db:"expires_at"`
	CreatedAt time.Time `json:"created_at" db:"created_at"`
}

// PasswordResetModel owns persistence for password_resets.
//
// It must not read or mutate canonical users state. User existence, active
// state, and password-hash mutation belong to UserModel. Cross-model
// workflows supply their own pgx.Tx so reset-token persistence can
// participate atomically without this model assuming orchestration
// ownership.
type PasswordResetModel struct {
	DB     *pgxpool.Pool
	Logger *logging.Logger
}

// CreateResetTokenTx generates and persists a password-reset token for userID
// within a caller-owned transaction.
//
// The caller must establish through UserModel, within the same transaction,
// that userID is eligible to receive a reset credential (UserModel.LockActiveUserTx).
// This method does not inspect users and does not decide reset eligibility.
//
// validFor is an operational input supplied by the owning workflow/service.
// The database remains authoritative for persisted lifecycle time: expires_at
// is calculated from PostgreSQL NOW(), not from an application clock.
//
// A user may have at most one outstanding reset token at a time: a new
// request replaces the prior one (ON CONFLICT (user_id) DO UPDATE), which
// implicitly invalidates any earlier unconsumed link.
//
// Only the protected token hash is persisted, via internal/security. The
// plaintext token is returned once to the caller for controlled delivery.
func (m *PasswordResetModel) CreateResetTokenTx(
	ctx context.Context,
	tx pgx.Tx,
	userID uuid.UUID,
	validFor time.Duration,
) (string, error) {
	logger := m.Logger.
		GetLoggerWithContextFromContext(ctx).
		WithFunctionName("CreateResetTokenTx")

	if tx == nil {
		err := errors.New("transaction is required")
		logger.Error("validation failed", "error", err)
		return "", err
	}

	if userID == uuid.Nil {
		err := errors.New("user ID is required")
		logger.Error("validation failed", "error", err)
		return "", err
	}

	if validFor <= 0 {
		err := errors.New("password reset token validity duration must be positive")
		logger.Error("validation failed", "error", err, "user_id", userID)
		return "", err
	}

	plainToken, err := security.GenerateRandomToken(security.PasswordResetTokenSize)
	if err != nil {
		logger.Error("reset token generation failed", "error", err, "user_id", userID)
		return "", fmt.Errorf("generate reset token: %w", err)
	}

	tokenHash, err := security.HashTokenStrict(plainToken)
	if err != nil {
		logger.Error("reset token hashing failed", "error", err, "user_id", userID)
		return "", fmt.Errorf("hash reset token: %w", err)
	}

	// PostgreSQL owns the persisted expiry timestamp. The duration is supplied
	// by the workflow rather than hard-coded here as operational policy.
	const query = `
		INSERT INTO password_resets (
			user_id,
			token_hash,
			expires_at
		)
		VALUES (
			$1,
			$2,
			NOW() + make_interval(secs => $3)
		)
		ON CONFLICT (user_id) DO UPDATE
		SET token_hash = EXCLUDED.token_hash,
			expires_at = EXCLUDED.expires_at
		RETURNING expires_at
	`

	var expiresAt time.Time

	if err := tx.QueryRow(
		ctx,
		query,
		userID,
		tokenHash,
		validFor.Seconds(),
	).Scan(&expiresAt); err != nil {
		logger.Error("reset token persistence failed", "error", err, "user_id", userID)
		return "", fmt.Errorf("persist reset token: %w", err)
	}

	logger.Info("reset token created", "user_id", userID, "expires_at", expiresAt)

	return plainToken, nil
}

// LookupResetTokenOwnerTx returns the canonical user ID currently associated
// with plainToken's protected hash inside the caller-owned transaction
// without taking a row lock.
//
// This method exists solely to support canonical cross-model lock ordering.
// Reset redemption does not know the owning user before inspecting
// password_resets, yet users must be the first row-lock domain. This lookup
// supplies that candidate identity without taking a password_resets row lock.
//
// A successful lookup is not proof that the credential is valid. Callers must
// subsequently lock the canonical user and then invoke LockResetTokenTx to
// establish authoritative token existence, ownership, and expiry state.
//
// Absence is returned as ErrInvalidResetToken so callers do not disclose
// whether a presented bearer credential was never issued or has ceased to
// exist.
func (m *PasswordResetModel) LookupResetTokenOwnerTx(
	ctx context.Context,
	tx pgx.Tx,
	plainToken string,
) (uuid.UUID, error) {
	logger := m.Logger.
		GetLoggerWithContextFromContext(ctx).
		WithFunctionName("LookupResetTokenOwnerTx")

	if tx == nil {
		err := errors.New("transaction is required")
		logger.Error("validation failed", "error", err)
		return uuid.Nil, err
	}

	if plainToken == "" {
		return uuid.Nil, ErrInvalidResetToken
	}

	tokenHash, err := security.HashTokenStrict(plainToken)
	if err != nil {
		return uuid.Nil, ErrInvalidResetToken
	}

	const query = `
		SELECT user_id
		FROM password_resets
		WHERE token_hash = $1
	`

	var userID uuid.UUID

	if err := tx.QueryRow(ctx, query, tokenHash).Scan(&userID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			logger.Warn("reset token owner lookup found no match")
			return uuid.Nil, ErrInvalidResetToken
		}

		logger.Error("reset token owner lookup failed", "error", err)
		return uuid.Nil, fmt.Errorf("lookup reset token owner: %w", err)
	}

	return userID, nil
}

// LockResetTokenTx locates and locks the password-reset token row
// corresponding to plainToken within a caller-owned transaction.
//
// The plaintext credential is hashed before lookup and is never logged or
// persisted. isExpired reports whether the row's expires_at has already
// passed; the caller decides how to respond (the established external
// contract treats expired and invalid tokens identically to avoid disclosing
// which case applies, while still allowing the caller to opportunistically
// consume/clean up the expired row).
//
// This method performs no users-table access. Cross-model redemption callers
// must first discover the candidate owner non-lockingly
// (LookupResetTokenOwnerTx), lock that canonical user through
// UserModel.LockActiveUserTx, and only then call this method.
func (m *PasswordResetModel) LockResetTokenTx(
	ctx context.Context,
	tx pgx.Tx,
	plainToken string,
) (tokenID uuid.UUID, userID uuid.UUID, isExpired bool, err error) {
	logger := m.Logger.
		GetLoggerWithContextFromContext(ctx).
		WithFunctionName("LockResetTokenTx")

	if tx == nil {
		return uuid.Nil, uuid.Nil, false, errors.New("transaction is required")
	}

	if plainToken == "" {
		return uuid.Nil, uuid.Nil, false, ErrInvalidResetToken
	}

	tokenHash, hashErr := security.HashTokenStrict(plainToken)
	if hashErr != nil {
		return uuid.Nil, uuid.Nil, false, ErrInvalidResetToken
	}

	const query = `
		SELECT
			id,
			user_id,
			expires_at <= NOW() AS is_expired
		FROM password_resets
		WHERE token_hash = $1
		FOR UPDATE
	`

	if scanErr := tx.QueryRow(ctx, query, tokenHash).Scan(
		&tokenID,
		&userID,
		&isExpired,
	); scanErr != nil {
		if errors.Is(scanErr, pgx.ErrNoRows) {
			logger.Warn("reset token not found")
			return uuid.Nil, uuid.Nil, false, ErrInvalidResetToken
		}

		logger.Error("reset token lookup failed", "error", scanErr)
		return uuid.Nil, uuid.Nil, false, fmt.Errorf("lock reset token: %w", scanErr)
	}

	return tokenID, userID, isExpired, nil
}

// ConsumeResetTokenTx hard-deletes the exact password-reset token row
// identified by tokenID within a caller-owned transaction.
//
// The workflow is expected to pass the token ID returned by LockResetTokenTx
// in the same transaction. Because that row is held by FOR UPDATE until the
// transaction ends, a missing row here indicates the workflow contract was
// violated or the token was otherwise consumed unexpectedly.
func (m *PasswordResetModel) ConsumeResetTokenTx(
	ctx context.Context,
	tx pgx.Tx,
	tokenID uuid.UUID,
) error {
	logger := m.Logger.
		GetLoggerWithContextFromContext(ctx).
		WithFunctionName("ConsumeResetTokenTx")

	if tx == nil {
		return errors.New("transaction is required")
	}
	if tokenID == uuid.Nil {
		return errors.New("password reset token ID is required")
	}

	const query = `
		DELETE FROM password_resets
		WHERE id = $1
		RETURNING id
	`

	var deletedID uuid.UUID

	if err := tx.QueryRow(ctx, query, tokenID).Scan(&deletedID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			logger.Error(
				"locked reset token missing at consumption",
				"token_id", tokenID,
			)
			return fmt.Errorf("consume reset token %s: token not found", tokenID)
		}

		logger.Error("reset token consumption failed", "error", err, "token_id", tokenID)
		return fmt.Errorf("consume reset token: %w", err)
	}

	logger.Info("reset token consumed", "token_id", deletedID)

	return nil
}

// DeleteExpiredResetTokens hard-deletes expired password-reset credentials.
//
// Reset tokens are transient security material rather than retained lifecycle
// history, mirroring ActivationTokenModel.DeleteExpiredActivationTokens.
// PostgreSQL owns expiry evaluation, and expires_at <= NOW() is the canonical
// expired-token boundary.
func (m *PasswordResetModel) DeleteExpiredResetTokens(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.
		GetLoggerWithContextFromContext(ctx).
		WithFunctionName("DeleteExpiredResetTokens")

	const query = `
		DELETE FROM password_resets
		WHERE expires_at <= NOW()
	`

	tag, err := m.DB.Exec(ctx, query)
	if err != nil {
		logger.Error("expired reset-token cleanup failed", "error", err)
		return fmt.Errorf("delete expired reset tokens: %w", err)
	}

	logger.Info("expired reset tokens deleted", "rows_affected", tag.RowsAffected())

	return nil
}
