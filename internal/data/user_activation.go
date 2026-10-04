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
//	  It owns activation-credential generation (the high-entropy link token and
//	  the independent short manual confirmation code), protected hash
//	  persistence for both, non-locking owner discovery required for canonical
//	  cross-model lock ordering, transactional token locking and expiry
//	  validation, transactional attempt-limited code verification,
//	  transactional consumption, and expired-credential cleanup required by the
//	  v1 account activation (user-facing: email confirmation) lifecycle.
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
//	Preserve plaintext activation tokens and codes only at controlled inputs
//	and outputs.
//	Persist only protected hashes; never persist or log plaintext tokens or
//	codes.
//	Preserve the link token and the manual code as separate, independently
//	generated credentials; never derive one from the other.
//	Preserve reissue as replacement of both credentials, so the newest issuance
//	supersedes every earlier credential for the account.
//	Preserve code_failed_total across reissue so reissue cannot mint unlimited
//	code guesses.
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
	"crypto/subtle"
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

// Email-confirmation code classifications are owned by the activation data
// domain alongside the persistence operations that produce them. Public HTTP
// boundaries collapse these classifications into a single non-enumerating
// response.
var (
	ErrActivationCodeRequired         = errors.New("activation code is required")
	ErrActivationCodeInvalid          = errors.New("invalid activation code")
	ErrActivationCodeExpired          = errors.New("activation code expired")
	ErrActivationCodeAttemptsExceeded = errors.New("activation code attempts exceeded")
)

// ActivationToken represents the canonical persisted activation-token record.
//
// Plaintext activation credentials are never persisted or exposed through this
// structure. TokenHash contains only the one-way hash stored in
// activation_tokens.
type ActivationToken struct {
	ID                 uuid.UUID  `json:"id" db:"id"`
	UserID             uuid.UUID  `json:"user_id" db:"user_id"`
	TokenHash          string     `json:"-" db:"token_hash"`
	ExpiresAt          time.Time  `json:"expires_at" db:"expires_at"`
	CodeHash           *string    `json:"-" db:"code_hash"`
	CodeExpiresAt      *time.Time `json:"code_expires_at,omitempty" db:"code_expires_at"`
	CodeFailedAttempts int        `json:"code_failed_attempts" db:"code_failed_attempts"`
	CodeFailedTotal    int        `json:"code_failed_total" db:"code_failed_total"`
	LastResentAt       *time.Time `json:"last_resent_at,omitempty" db:"last_resent_at"`
	CreatedAt          time.Time  `json:"created_at" db:"created_at"`
	UpdatedAt          time.Time  `json:"updated_at" db:"updated_at"`
}

// ActivationCredentials is the plaintext credential pair produced by one
// issuance. Both values exist only at the controlled delivery boundary and
// must never be logged, audited, traced, or persisted.
type ActivationCredentials struct {
	// LinkToken is the high-entropy bearer credential embedded in the
	// emailed confirmation link.
	LinkToken string

	// Code is the independent short manual confirmation code.
	Code string
}

// ActivationCodePolicy carries the operational limits applied to manual
// confirmation-code verification. Values are supplied by validated service
// configuration rather than hard-coded here.
type ActivationCodePolicy struct {
	// MaxAttempts is the number of failed attempts allowed against one issued
	// code before that code is discarded.
	MaxAttempts int

	// MaxTotalFailures is the number of failed attempts allowed across all
	// codes issued to one pending account (reissue does not reset it). Once
	// reached, code verification is unavailable for that pending record; the
	// emailed link continues to work.
	MaxTotalFailures int
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

// activationCodeHashDomain separates manual-code hashes from every other
// hashed credential in the platform.
const activationCodeHashDomain = "sagrenti:activation-code:v1"

// hashActivationCode returns the protected hash of a manual confirmation code
// bound to its owning account.
//
// Binding the user ID into the hash means a stored code hash is meaningful
// only for its own account, and lookup is always by user_id rather than by a
// low-entropy code value. A six-digit code is not secret against offline
// guessing if this hash leaks; its protection is the short lifetime, the
// per-code and per-record attempt limits, and the account binding.
func hashActivationCode(userID uuid.UUID, code string) string {
	sum := sha256.Sum256(
		[]byte(activationCodeHashDomain + ":" + userID.String() + ":" + code),
	)
	return hex.EncodeToString(sum[:])
}

// CreateActivationCredentialsTx generates and persists a fresh activation
// credential pair for userID within a caller-owned transaction: a high-entropy
// link token and an independently generated manual confirmation code.
//
// The caller must establish through UserModel, within the same transaction,
// that userID is eligible to receive an activation credential. This method does
// not inspect users and does not decide activation eligibility.
//
// Reissue replaces both credentials on the account's single pending record, so
// any previously issued link token or code stops working immediately. The
// per-code failure counter resets with the new code; code_failed_total is
// deliberately preserved so repeated reissue cannot mint unlimited guesses.
//
// markResent records this issuance as a public resend (last_resent_at =
// NOW()) for the resend cooldown. Signup and trusted internal issuance pass
// false and leave any previous resend time unchanged.
//
// linkValidFor and codeValidFor are operational inputs supplied by the owning
// workflow/service. PostgreSQL owns persisted lifecycle time: both expiries are
// calculated from NOW(), and the code never outlives the link.
//
// Only protected hashes are persisted. The plaintext credentials are returned
// once to the caller for controlled delivery.
func (m *ActivationTokenModel) CreateActivationCredentialsTx(
	ctx context.Context,
	tx pgx.Tx,
	userID uuid.UUID,
	linkValidFor time.Duration,
	codeValidFor time.Duration,
	markResent bool,
) (ActivationCredentials, error) {
	var none ActivationCredentials

	logger := m.Logger.
		GetLoggerWithContextFromContext(ctx).
		WithFunctionName("CreateActivationCredentialsTx")

	if tx == nil {
		err := errors.New("transaction is required")
		logger.Error("validation failed", "error", err)
		return none, err
	}

	if userID == uuid.Nil {
		err := errors.New("user ID is required")
		logger.Error("validation failed", "error", err)
		return none, err
	}

	if linkValidFor <= 0 || codeValidFor <= 0 {
		err := errors.New(
			"activation credential validity durations must be positive",
		)
		logger.Error(
			"validation failed",
			"error", err,
			"user_id", userID,
		)
		return none, err
	}

	plainToken, err := security.GenerateRandomToken(32)
	if err != nil {
		logger.Error(
			"activation token generation failed",
			"error", err,
			"user_id", userID,
		)
		return none, fmt.Errorf(
			"generate activation token: %w",
			err,
		)
	}

	plainCode, err := security.GenerateNumericCode(
		security.ActivationCodeDigits,
	)
	if err != nil {
		logger.Error(
			"activation code generation failed",
			"error", err,
			"user_id", userID,
		)
		return none, fmt.Errorf(
			"generate activation code: %w",
			err,
		)
	}

	tokenHash := hashActivationToken(plainToken)
	codeHash := hashActivationCode(userID, plainCode)

	// PostgreSQL owns the persisted expiry timestamps. The durations are
	// supplied by the workflow rather than hard-coded here as policy.
	const query = `
		INSERT INTO activation_tokens (
			user_id,
			token_hash,
			expires_at,
			code_hash,
			code_expires_at,
			code_failed_attempts,
			code_failed_total,
			last_resent_at,
			updated_at
		)
		VALUES (
			$1,
			$2,
			NOW() + make_interval(secs => $3),
			$4,
			NOW() + make_interval(secs => LEAST($3, $5)),
			0,
			0,
			CASE WHEN $6 THEN NOW() ELSE NULL END,
			NOW()
		)
		ON CONFLICT (user_id) DO UPDATE
		SET token_hash = EXCLUDED.token_hash,
			expires_at = EXCLUDED.expires_at,
			code_hash = EXCLUDED.code_hash,
			code_expires_at = EXCLUDED.code_expires_at,
			code_failed_attempts = 0,
			code_failed_total = activation_tokens.code_failed_total,
			last_resent_at = CASE
				WHEN $6 THEN NOW()
				ELSE activation_tokens.last_resent_at
			END,
			updated_at = NOW()
		RETURNING expires_at, code_expires_at
	`

	var (
		expiresAt     time.Time
		codeExpiresAt time.Time
	)

	if err := tx.QueryRow(
		ctx,
		query,
		userID,
		tokenHash,
		linkValidFor.Seconds(),
		codeHash,
		codeValidFor.Seconds(),
		markResent,
	).Scan(&expiresAt, &codeExpiresAt); err != nil {
		logger.Error(
			"activation credential persistence failed",
			"error", err,
			"user_id", userID,
		)
		return none, fmt.Errorf(
			"persist activation credentials: %w",
			err,
		)
	}

	logger.Info(
		"activation credentials created",
		"user_id", userID,
		"expires_at", expiresAt,
		"code_expires_at", codeExpiresAt,
	)

	return ActivationCredentials{
		LinkToken: plainToken,
		Code:      plainCode,
	}, nil
}

// ActivationResentWithinTx reports whether userID's pending activation record
// was last reissued by a public resend less than window ago, using PostgreSQL
// time.
//
// It supports the resend cooldown. Signup issuance does not count, so the
// first resend after signup (for example when the signup email could not be
// delivered) always proceeds. A missing record reports false. The
// caller must already hold the canonical users row lock so this read cannot
// race a concurrent issuance outside the users-before-activation_tokens order.
func (m *ActivationTokenModel) ActivationResentWithinTx(
	ctx context.Context,
	tx pgx.Tx,
	userID uuid.UUID,
	window time.Duration,
) (bool, error) {
	logger := m.Logger.
		GetLoggerWithContextFromContext(ctx).
		WithFunctionName("ActivationResentWithinTx")

	if tx == nil {
		err := errors.New("transaction is required")
		logger.Error("validation failed", "error", err)
		return false, err
	}

	if userID == uuid.Nil {
		return false, errors.New("user ID is required")
	}

	if window <= 0 {
		return false, nil
	}

	const query = `
		SELECT COALESCE(last_resent_at > NOW() - make_interval(secs => $2), FALSE)
		FROM activation_tokens
		WHERE user_id = $1
	`

	var recent bool

	if err := tx.QueryRow(
		ctx,
		query,
		userID,
		window.Seconds(),
	).Scan(&recent); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, nil
		}

		logger.Error(
			"activation issuance recency lookup failed",
			"error", err,
			"user_id", userID,
		)

		return false, fmt.Errorf(
			"lookup activation issuance recency: %w",
			err,
		)
	}

	return recent, nil
}

// VerifyActivationCodeTx locks userID's pending activation record and checks
// plainCode against its current manual confirmation code within a
// caller-owned transaction.
//
// The caller must already hold the canonical users row lock for userID
// (users before activation_tokens).
//
// On success the exact activation-record ID is returned so the workflow can
// perform the lifecycle transition and consume the record, which also
// invalidates the link token.
//
// On a wrong code the failure counters are incremented in the same
// transaction, and the current code is discarded once the per-code or
// per-record limit is reached. The caller MUST commit the transaction after a
// counted failure (ErrActivationCodeInvalid or ErrActivationCodeAttemptsExceeded
// returned from a mismatch); rolling back would erase the attempt and defeat
// the limit.
//
// The plaintext code is never logged or persisted.
func (m *ActivationTokenModel) VerifyActivationCodeTx(
	ctx context.Context,
	tx pgx.Tx,
	userID uuid.UUID,
	plainCode string,
	policy ActivationCodePolicy,
) (uuid.UUID, error) {
	logger := m.Logger.
		GetLoggerWithContextFromContext(ctx).
		WithFunctionName("VerifyActivationCodeTx")

	if tx == nil {
		err := errors.New("transaction is required")
		logger.Error("validation failed", "error", err)
		return uuid.Nil, err
	}

	if userID == uuid.Nil {
		return uuid.Nil, errors.New("user ID is required")
	}

	if plainCode == "" {
		return uuid.Nil, ErrActivationCodeRequired
	}

	if policy.MaxAttempts <= 0 || policy.MaxTotalFailures <= 0 {
		return uuid.Nil, errors.New(
			"activation code policy limits must be positive",
		)
	}

	const lockQuery = `
		SELECT
			id,
			code_hash,
			code_expires_at IS NOT NULL AND code_expires_at <= NOW() AS code_expired,
			code_failed_attempts,
			code_failed_total
		FROM activation_tokens
		WHERE user_id = $1
		FOR UPDATE
	`

	var (
		recordID       uuid.UUID
		storedCodeHash *string
		codeExpired    bool
		failedAttempts int
		failedTotal    int
	)

	if err := tx.QueryRow(
		ctx,
		lockQuery,
		userID,
	).Scan(
		&recordID,
		&storedCodeHash,
		&codeExpired,
		&failedAttempts,
		&failedTotal,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return uuid.Nil, ErrActivationCodeInvalid
		}

		logger.Error(
			"activation code record lock failed",
			"error", err,
			"user_id", userID,
		)

		return uuid.Nil, fmt.Errorf(
			"lock activation code record: %w",
			err,
		)
	}

	if storedCodeHash == nil {
		return uuid.Nil, ErrActivationCodeInvalid
	}

	if failedTotal >= policy.MaxTotalFailures {
		if err := m.discardActivationCodeTx(ctx, tx, recordID); err != nil {
			return uuid.Nil, err
		}
		return uuid.Nil, ErrActivationCodeAttemptsExceeded
	}

	if codeExpired {
		return uuid.Nil, ErrActivationCodeExpired
	}

	presentedHash := hashActivationCode(userID, plainCode)

	if subtle.ConstantTimeCompare(
		[]byte(presentedHash),
		[]byte(*storedCodeHash),
	) == 1 {
		return recordID, nil
	}

	nextAttempts := failedAttempts + 1
	nextTotal := failedTotal + 1
	exhausted := nextAttempts >= policy.MaxAttempts ||
		nextTotal >= policy.MaxTotalFailures

	const failQuery = `
		UPDATE activation_tokens
		SET code_failed_attempts = $2,
			code_failed_total = $3,
			code_hash = CASE WHEN $4 THEN NULL ELSE code_hash END,
			code_expires_at = CASE WHEN $4 THEN NULL ELSE code_expires_at END,
			updated_at = NOW()
		WHERE id = $1
	`

	if _, err := tx.Exec(
		ctx,
		failQuery,
		recordID,
		nextAttempts,
		nextTotal,
		exhausted,
	); err != nil {
		logger.Error(
			"activation code failure recording failed",
			"error", err,
			"user_id", userID,
		)

		return uuid.Nil, fmt.Errorf(
			"record activation code failure: %w",
			err,
		)
	}

	logger.Warn(
		"activation code mismatch",
		"user_id", userID,
		"code_failed_attempts", nextAttempts,
		"code_failed_total", nextTotal,
		"code_discarded", exhausted,
	)

	if exhausted {
		return uuid.Nil, ErrActivationCodeAttemptsExceeded
	}

	return uuid.Nil, ErrActivationCodeInvalid
}

// discardActivationCodeTx removes the current manual code from a locked
// activation record without touching the link token.
func (m *ActivationTokenModel) discardActivationCodeTx(
	ctx context.Context,
	tx pgx.Tx,
	recordID uuid.UUID,
) error {
	const query = `
		UPDATE activation_tokens
		SET code_hash = NULL,
			code_expires_at = NULL,
			updated_at = NOW()
		WHERE id = $1
		  AND code_hash IS NOT NULL
	`

	if _, err := tx.Exec(ctx, query, recordID); err != nil {
		return fmt.Errorf(
			"discard activation code: %w",
			err,
		)
	}

	return nil
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
