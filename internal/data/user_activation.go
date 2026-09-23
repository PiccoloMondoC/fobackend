// Package data provides models and database access methods for activation-token
// persistence.
//
// focodebase/fobackend/internal/data/user_activation.go
//
// GTM:
//
//	Layer: 2.2 Identity / Auth Domain
//	Release Class: SPINE
//	Reason:
//	  Activation-token persistence is release-critical identity infrastructure.
//	  It owns activation-token generation, protected token-hash persistence,
//	  non-locking owner discovery required for canonical cross-model lock
//	  ordering, transactional token locking and expiry validation,
//	  transactional token consumption, and expired-token cleanup required by
//	  the v1 account activation lifecycle.
//
//	  Canonical user state is not owned here. User existence, deletion state,
//	  activation eligibility, and users.is_active belong to UserModel. Atomic
//	  account activation is composed outside this model by a workflow/service
//	  that calls ActivationTokenModel and UserModel within one transaction.
//
// SPINE Rule:
//
//	Keep compiling.
//	Keep production-ready.
//	Preserve plaintext activation tokens only at controlled inputs and outputs.
//	Persist only protected token hashes; never persist or log plaintext tokens.
//	Preserve DB-owned activation-token expiry and lifecycle timestamps.
//	Preserve expires_at <= NOW() as the canonical expired-token boundary.
//	Preserve hard deletion for transient expired activation-token material.
//	Preserve strict activation_tokens ownership: this file must never read or
//	write canonical users state.
//	Preserve caller-owned transaction composition for cross-model activation.
//	Preserve LookupActivationTokenOwnerTx as a non-locking, non-authoritative
//	owner-discovery primitive only; it must never substitute for authoritative
//	credential locking and validation.
//	Block deployment if this file breaks build, activation-token generation,
//	protected persistence, owner discovery, token validation, token consumption,
//	expired-token cleanup, or the activation_tokens/users ownership boundary.
package data

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/PiccoloMondoC/focodebase/fobackend/internal/logging"
	"github.com/PiccoloMondoC/focodebase/fobackend/internal/security"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ActivationToken represents the canonical persisted activation-token record.
//
// Plaintext activation credentials are never persisted or exposed through this
// structure. TokenHash contains only the one-way hash stored in
// activation_tokens.
type ActivationToken struct {
	ID        uuid.UUID `json:"id" db:"id"`
	UserID    uuid.UUID `json:"user_id" db:"user_id"`
	TokenHash string    `json:"-" db:"token_hash"`
	ExpiresAt time.Time `json:"expires_at" db:"expires_at"`
	CreatedAt time.Time `json:"created_at" db:"created_at"`
	UpdatedAt time.Time `json:"updated_at" db:"updated_at"`
}

// ActivationTokenModel owns persistence for activation_tokens.
//
// It must not read or mutate canonical users state. User existence, deletion
// state, active state, and activation eligibility belong to UserModel.
//
// Cross-model workflows supply their own pgx.Tx so activation-token persistence
// can participate atomically without this model assuming orchestration
// ownership.
type ActivationTokenModel struct {
	DB     *pgxpool.Pool
	Logger *logging.Logger
}

// hashActivationToken returns the canonical one-way hash used for
// activation-token persistence and lookup.
func hashActivationToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// CreateActivationTokenTx generates and persists an activation token for userID
// within a caller-owned transaction.
//
// The caller must establish through UserModel, within the same transaction,
// that userID is eligible to receive an activation credential. This method does
// not inspect users and does not decide activation eligibility.
//
// validFor is an operational input supplied by the owning workflow/service.
// The database remains authoritative for persisted lifecycle time: expires_at
// is calculated from PostgreSQL NOW(), not from an application clock.
//
// Only the protected token hash is persisted. The plaintext token is returned
// once to the caller for controlled delivery.
func (m *ActivationTokenModel) CreateActivationTokenTx(
	ctx context.Context,
	tx pgx.Tx,
	userID uuid.UUID,
	validFor time.Duration,
) (string, error) {
	logger := m.Logger.
		GetLoggerWithContextFromContext(ctx).
		WithFunctionName("CreateActivationTokenTx")

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
		err := errors.New(
			"activation token validity duration must be positive",
		)
		logger.Error(
			"validation failed",
			"error", err,
			"user_id", userID,
		)
		return "", err
	}

	plainToken, err := security.GenerateRandomToken(32)
	if err != nil {
		logger.Error(
			"activation token generation failed",
			"error", err,
			"user_id", userID,
		)
		return "", fmt.Errorf(
			"generate activation token: %w",
			err,
		)
	}

	tokenHash := hashActivationToken(plainToken)

	// PostgreSQL owns the persisted expiry timestamp. The duration is supplied
	// by the workflow rather than hard-coded here as operational policy.
	const query = `
		INSERT INTO activation_tokens (
			user_id,
			token_hash,
			expires_at,
			updated_at
		)
		VALUES (
			$1,
			$2,
			NOW() + make_interval(secs => $3),
			NOW()
		)
		ON CONFLICT (user_id) DO UPDATE
		SET token_hash = EXCLUDED.token_hash,
			expires_at = EXCLUDED.expires_at,
			updated_at = NOW()
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
		logger.Error(
			"activation token persistence failed",
			"error", err,
			"user_id", userID,
		)
		return "", fmt.Errorf(
			"persist activation token: %w",
			err,
		)
	}

	logger.Info(
		"activation token created",
		"user_id", userID,
		"expires_at", expiresAt,
	)

	return plainToken, nil
}

// LookupActivationTokenOwnerTx returns the canonical user ID currently
// associated with plainToken's protected hash inside the caller-owned
// transaction without taking a row lock.
//
// This method exists solely to support canonical cross-model lock ordering.
// Activation redemption does not know the owning user before inspecting
// activation_tokens, yet users must be the first row-lock domain. This lookup
// supplies that candidate identity without taking an activation_tokens row lock.
//
// A successful lookup is not proof that the credential is valid. This method
// deliberately does not evaluate expiry and does not lock or otherwise stabilize
// the row. Callers must subsequently lock the canonical user and then invoke
// LockActivationTokenTx to establish authoritative token existence, ownership,
// expiry state, and exact token identity.
//
// Absence is returned as ErrActivationTokenInvalid so callers do not disclose
// whether a presented bearer credential was never issued or has ceased to exist.
func (m *ActivationTokenModel) LookupActivationTokenOwnerTx(
	ctx context.Context,
	tx pgx.Tx,
	plainToken string,
) (uuid.UUID, error) {
	logger := m.Logger.
		GetLoggerWithContextFromContext(ctx).
		WithFunctionName(
			"LookupActivationTokenOwnerTx",
		)

	if tx == nil {
		err := errors.New("transaction is required")
		logger.Error("validation failed", "error", err)
		return uuid.Nil, err
	}

	if plainToken == "" {
		return uuid.Nil,
			ErrActivationTokenRequired
	}

	tokenHash := hashActivationToken(plainToken)

	const query = `
		SELECT user_id
		FROM activation_tokens
		WHERE token_hash = $1
	`

	var userID uuid.UUID

	if err := tx.QueryRow(
		ctx,
		query,
		tokenHash,
	).Scan(&userID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			logger.Warn(
				"activation token owner lookup found no match",
			)
			return uuid.Nil,
				ErrActivationTokenInvalid
		}

		logger.Error(
			"activation token owner lookup failed",
			"error", err,
		)

		return uuid.Nil, fmt.Errorf(
			"lookup activation token owner: %w",
			err,
		)
	}

	return userID, nil
}

// LockActivationTokenTx locates, locks, and validates the activation-token row
// corresponding to plainToken within a caller-owned transaction.
//
// The plaintext credential is hashed before lookup and is never logged or
// persisted.
//
// Expiry is evaluated using PostgreSQL time. A token is valid only while
// expires_at > NOW(); therefore expires_at <= NOW() is expired.
//
// On success the exact activation-token ID and owning user ID are returned so
// the workflow can perform the canonical lifecycle transition and later consume
// that exact locked credential.
//
// This method performs no users-table access. Cross-model redemption callers
// must first discover the candidate owner non-lockingly, lock that canonical
// user through UserModel, and only then call this method. LockActivationTokenTx
// remains the authoritative credential-validation operation.
func (m *ActivationTokenModel) LockActivationTokenTx(
	ctx context.Context,
	tx pgx.Tx,
	plainToken string,
) (uuid.UUID, uuid.UUID, error) {
	logger := m.Logger.
		GetLoggerWithContextFromContext(ctx).
		WithFunctionName("LockActivationTokenTx")

	if tx == nil {
		err := errors.New("transaction is required")
		logger.Error("validation failed", "error", err)
		return uuid.Nil, uuid.Nil, err
	}

	if plainToken == "" {
		return uuid.Nil, uuid.Nil,
			ErrActivationTokenRequired
	}

	tokenHash := hashActivationToken(plainToken)

	const query = `
		SELECT
			id,
			user_id,
			expires_at <= NOW() AS is_expired
		FROM activation_tokens
		WHERE token_hash = $1
		FOR UPDATE
	`

	var (
		tokenID   uuid.UUID
		userID    uuid.UUID
		isExpired bool
	)

	if err := tx.QueryRow(
		ctx,
		query,
		tokenHash,
	).Scan(
		&tokenID,
		&userID,
		&isExpired,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			logger.Warn("activation token not found")
			return uuid.Nil, uuid.Nil,
				ErrActivationTokenInvalid
		}

		logger.Error(
			"activation token lookup failed",
			"error", err,
		)

		return uuid.Nil, uuid.Nil, fmt.Errorf(
			"lock activation token: %w",
			err,
		)
	}

	if isExpired {
		logger.Warn(
			"activation token expired",
			"token_id", tokenID,
			"user_id", userID,
		)
		return uuid.Nil, uuid.Nil,
			ErrActivationTokenExpired
	}

	return tokenID, userID, nil
}

// ConsumeActivationTokenTx hard-deletes the exact activation-token row
// identified by tokenID within a caller-owned transaction.
//
// The workflow is expected to pass the token ID returned by
// LockActivationTokenTx in the same transaction. Because that row is held by
// FOR UPDATE until the transaction ends, a missing row here is not an ordinary
// user-facing absence; it indicates that the workflow contract was violated or
// the token was otherwise consumed unexpectedly.
//
// ErrActivationTokenNotFound is retained as the canonical classification for
// that invariant failure. Other deletion failures preserve both
// ErrActivationTokenDeleteFailed and the underlying database error.
func (m *ActivationTokenModel) ConsumeActivationTokenTx(
	ctx context.Context,
	tx pgx.Tx,
	tokenID uuid.UUID,
) error {
	logger := m.Logger.
		GetLoggerWithContextFromContext(ctx).
		WithFunctionName("ConsumeActivationTokenTx")

	if tx == nil {
		err := errors.New("transaction is required")
		logger.Error("validation failed", "error", err)
		return err
	}

	if tokenID == uuid.Nil {
		err := errors.New("activation token ID is required")
		logger.Error("validation failed", "error", err)
		return err
	}

	const query = `
		DELETE FROM activation_tokens
		WHERE id = $1
		RETURNING id
	`

	var deletedID uuid.UUID

	if err := tx.QueryRow(
		ctx,
		query,
		tokenID,
	).Scan(&deletedID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			logger.Error(
				"locked activation token missing at consumption",
				"token_id", tokenID,
			)

			return fmt.Errorf(
				"consume activation token %s: %w",
				tokenID,
				ErrActivationTokenNotFound,
			)
		}

		logger.Error(
			"activation token consumption failed",
			"error", err,
			"token_id", tokenID,
		)

		return fmt.Errorf(
			"consume activation token: %w",
			errors.Join(
				ErrActivationTokenDeleteFailed,
				err,
			),
		)
	}

	logger.Info(
		"activation token consumed",
		"token_id", deletedID,
	)

	return nil
}

// DeleteByUserIDTx hard-deletes any activation-token row owned by userID
// within a caller-owned transaction.
//
// This is the activation-token side of account-closure/expulsion composition.
// It performs no users-table access and never owns the transaction. Callers that
// combine it with canonical user lifecycle mutation must acquire/mutate users
// first, preserving the canonical users-before-activation_tokens ordering.
//
// A missing row is not an error because an account may have no pending
// activation credential.
func (m *ActivationTokenModel) DeleteByUserIDTx(
	ctx context.Context,
	tx pgx.Tx,
	userID uuid.UUID,
) error {
	logger := m.Logger.
		GetLoggerWithContextFromContext(ctx).
		WithFunctionName("DeleteByUserIDTx")

	if tx == nil {
		err := errors.New("transaction is required")
		logger.Error("validation failed", "error", err)
		return err
	}

	if userID == uuid.Nil {
		err := errors.New("user ID is required")
		logger.Error("validation failed", "error", err)
		return err
	}

	const query = `
		DELETE FROM activation_tokens
		WHERE user_id = $1
	`

	if _, err := tx.Exec(
		ctx,
		query,
		userID,
	); err != nil {
		logger.Error(
			"activation token cleanup by user ID failed",
			"error", err,
			"user_id", userID,
		)

		return fmt.Errorf(
			"delete activation token by user ID: %w",
			err,
		)
	}

	return nil
}

// DeleteExpiredActivationTokens hard-deletes expired activation credentials.
//
// Activation tokens are transient security material rather than retained
// lifecycle history. PostgreSQL owns expiry evaluation, and expires_at <= NOW()
// is the canonical expired-token boundary.
func (m *ActivationTokenModel) DeleteExpiredActivationTokens(
	ctx context.Context,
) error {
	ctx, cancel := context.WithTimeout(
		ctx,
		dbTimeout,
	)
	defer cancel()

	logger := m.Logger.
		GetLoggerWithContextFromContext(ctx).
		WithFunctionName(
			"DeleteExpiredActivationTokens",
		)

	const query = `
		DELETE FROM activation_tokens
		WHERE expires_at <= NOW()
	`

	tag, err := m.DB.Exec(ctx, query)
	if err != nil {
		logger.Error(
			"expired activation-token cleanup failed",
			"error", err,
		)

		return fmt.Errorf(
			"delete expired activation tokens: %w",
			err,
		)
	}

	logger.Info(
		"expired activation tokens deleted",
		"rows_affected", tag.RowsAffected(),
	)

	return nil
}
