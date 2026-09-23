// Package data provides models and database access methods for tokens and other entities.
//
// focodebase/fobackend/internal/data/tokens.go
//
// GTM:
//
//	Layer: 2.2 Identity / Auth Domain
//	Release Class: SPINE
//	Reason:
//	  Tokens are release-critical authentication and session-security
//	  infrastructure. They protect refresh-token persistence, access-token
//	  revocation persistence, logout/session invalidation, and retained security
//	  records required by v1 identity and access control.
//
// SPINE Rule:
//
//	Keep compiling.
//	Keep production-ready.
//	Preserve plaintext-token boundary handling only at controlled inputs.
//	Preserve token_hash persistence and no raw-token JSON/log exposure.
//	Preserve refresh-token revoked_at lifecycle semantics.
//	Preserve access-token blacklist persistence and lookup.
//	Keep JWT parsing, signature verification, and claims validation outside
//	the data layer; those concerns belong to auth.TokenService.
//	Block deployment if this file breaks build, refresh-token persistence,
//	access-token revocation persistence, session security, or authentication integrity.
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

// Token represents the canonical persisted refresh-token record.
//
// Security note:
// The plaintext bearer token is never stored in canonical persistence and must
// never be exposed in JSON payloads or logs. The persisted form is token_hash.
type Token struct {
	ID        uuid.UUID  `json:"id" db:"id"`
	UserID    uuid.UUID  `json:"user_id" db:"user_id"`
	TokenHash string     `json:"-" db:"token_hash"`
	ExpiresAt time.Time  `json:"expires_at" db:"expires_at"`
	RevokedAt *time.Time `json:"-" db:"revoked_at"`
	CreatedAt time.Time  `json:"created_at" db:"created_at"`
}

// TokenModel owns refresh-token persistence and persistence-backed access-token
// revocation state. It does not parse, verify, or interpret JWTs.
type TokenModel struct {
	DB     *pgxpool.Pool
	Logger *logging.Logger
}

// StoreRefreshToken persists a refresh token in protected canonical form and
// returns the canonical inserted row.
//
// Lifecycle decision:
// Refresh tokens are retained security records. Normal logout / rotation uses
// revocation via revoked_at, not hard deletion.
//
// Time-source decision:
// created_at remains DB-owned through the table default. This method does not
// application-write persisted lifecycle timestamps.
func (m *TokenModel) StoreRefreshToken(ctx context.Context, userID uuid.UUID, token string, expiresAt time.Time) (*Token, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("StoreRefreshToken")

	if userID == uuid.Nil {
		err := errors.New("user ID is required")
		logger.Error("validation failed", err)
		return nil, err
	}

	token = strings.TrimSpace(token)
	if token == "" {
		err := errors.New("token is required")
		logger.Error("validation failed", err)
		return nil, err
	}

	if expiresAt.IsZero() {
		err := errors.New("expiration time is required")
		logger.Error("validation failed", err)
		return nil, err
	}

	query := `
		INSERT INTO refresh_tokens (user_id, token_hash, expires_at)
		VALUES ($1, encode(digest($2, 'sha256'), 'hex'), $3)
		RETURNING id, user_id, token_hash, expires_at, revoked_at, created_at
	`

	var stored Token
	err := m.DB.QueryRow(ctx, query, userID, token, expiresAt.UTC()).Scan(
		&stored.ID,
		&stored.UserID,
		&stored.TokenHash,
		&stored.ExpiresAt,
		&stored.RevokedAt,
		&stored.CreatedAt,
	)
	if err != nil {
		logger.Error("store refresh token failed", err, "user_id", userID)
		return nil, err
	}

	logger.Info("store refresh token successful", "token_id", stored.ID, "user_id", stored.UserID)
	return &stored, nil
}

// ValidateRefreshToken returns the persisted refresh-token record when the
// supplied plaintext token matches an active, non-revoked, non-expired record.
func (m *TokenModel) ValidateRefreshToken(ctx context.Context, token string) (*Token, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("ValidateRefreshToken")

	token = strings.TrimSpace(token)
	if token == "" {
		err := errors.New("token is required")
		logger.Error("validation failed", err)
		return nil, err
	}

	query := `
		SELECT id, user_id, token_hash, expires_at, revoked_at, created_at
		FROM refresh_tokens
		WHERE token_hash = encode(digest($1, 'sha256'), 'hex')
		  AND revoked_at IS NULL
		  AND expires_at > NOW()
	`

	var t Token
	err := m.DB.QueryRow(ctx, query, token).Scan(
		&t.ID,
		&t.UserID,
		&t.TokenHash,
		&t.ExpiresAt,
		&t.RevokedAt,
		&t.CreatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			logger.Warn("refresh token validation failed", "reason", "not_found_or_inactive")
			return nil, ErrRefreshTokenInvalid
		}
		logger.Error("refresh token validation query failed", err)
		return nil, err
	}

	logger.Info("refresh token validation successful", "token_id", t.ID, "user_id", t.UserID)
	return &t, nil
}

// IsAccessTokenRevoked reports whether jti is present in the canonical
// access-token blacklist. The JTI must come from an access token that has
// already passed cryptographic and claims validation at the auth boundary.
//
// TokenModel deliberately performs no JWT parsing or claims interpretation.
func (m *TokenModel) IsAccessTokenRevoked(ctx context.Context, jti string) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("IsAccessTokenRevoked")

	jti = strings.TrimSpace(jti)
	if jti == "" {
		err := errors.New("jti is required")
		logger.Error("validation failed", err)
		return false, err
	}

	var revoked bool
	if err := m.DB.QueryRow(
		ctx,
		`SELECT EXISTS (SELECT 1 FROM auth_token_blacklist WHERE jti = $1)`,
		jti,
	).Scan(&revoked); err != nil {
		logger.Error("access token revocation lookup failed", err, "jti", jti)
		return false, err
	}

	return revoked, nil
}

// DeleteRefreshToken physically deletes a refresh-token row by ID.
//
// This is a true hard delete and is reserved for exceptional cleanup or
// administrative maintenance. It is not the normal logout path.
func (m *TokenModel) DeleteRefreshToken(ctx context.Context, tokenID uuid.UUID) error {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("DeleteRefreshToken")

	if tokenID == uuid.Nil {
		err := errors.New("token ID is required")
		logger.Error("validation failed", err)
		return err
	}

	res, err := m.DB.Exec(ctx, `DELETE FROM refresh_tokens WHERE id = $1`, tokenID)
	if err != nil {
		logger.Error("hard delete refresh token failed", err, "token_id", tokenID)
		return err
	}

	if res.RowsAffected() == 0 {
		logger.Warn("hard delete refresh token missed", "token_id", tokenID)
		return ErrRefreshTokenNotFound
	}

	logger.Info("hard delete refresh token successful", "token_id", tokenID)
	return nil
}

// RevokeAllTokens revokes all active refresh tokens for the given user.
//
// Lifecycle decision:
// Revocation is the canonical logout / session invalidation path for refresh
// tokens. Records are retained for security traceability.
func (m *TokenModel) RevokeAllTokens(ctx context.Context, userID uuid.UUID) error {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("RevokeAllTokens")

	if userID == uuid.Nil {
		err := errors.New("user ID is required")
		logger.Error("validation failed", err)
		return err
	}

	query := `
		UPDATE refresh_tokens
		SET revoked_at = NOW()
		WHERE user_id = $1
		  AND revoked_at IS NULL
	`

	res, err := m.DB.Exec(ctx, query, userID)
	if err != nil {
		logger.Error("revoke all refresh tokens failed", err, "user_id", userID)
		return err
	}

	logger.Info("revoke all refresh tokens successful", "user_id", userID, "rows_affected", res.RowsAffected())
	return nil
}

// RevokeAccessTokenJTI persists the revocation fact for an already-validated
// access token. Revocation is idempotent: an existing blacklist row remains the
// canonical revocation record.
//
// Time-source decision:
// revoked_at is DB-owned through NOW() in SQL, not application-generated.
func (m *TokenModel) RevokeAccessTokenJTI(ctx context.Context, jti string, userID uuid.UUID) error {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("RevokeAccessTokenJTI")

	jti = strings.TrimSpace(jti)
	if jti == "" {
		err := errors.New("jti is required")
		logger.Error("validation failed", err)
		return err
	}
	if userID == uuid.Nil {
		err := errors.New("user ID is required")
		logger.Error("validation failed", err)
		return err
	}

	_, err := m.DB.Exec(ctx, `
		INSERT INTO auth_token_blacklist (jti, user_id, revoked_at)
		VALUES ($1, $2, NOW())
		ON CONFLICT (jti) DO NOTHING
	`, jti, userID)
	if err != nil {
		logger.Error("access token blacklist insert failed", err, "jti", jti)
		return err
	}

	logger.Info("access token revoked", "jti", jti, "user_id", userID)
	return nil
}

// RevokeRefreshToken revokes a refresh token using the supplied plaintext token.
//
// The plaintext token is accepted only at the controlled input boundary. The
// canonical persistence lookup uses the derived token hash and never stores or
// logs the raw token value.
func (m *TokenModel) RevokeRefreshToken(ctx context.Context, token string) error {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("RevokeRefreshToken")

	token = strings.TrimSpace(token)
	if token == "" {
		return errors.New("token is required")
	}

	res, err := m.DB.Exec(ctx, `
		UPDATE refresh_tokens
		SET revoked_at = NOW()
		WHERE token_hash = encode(digest($1, 'sha256'), 'hex')
		  AND revoked_at IS NULL
	`, token)
	if err != nil {
		logger.Error("revoke refresh token failed", err)
		return err
	}

	if res.RowsAffected() == 0 {
		logger.Warn("revoke refresh token missed", "reason", "not_found_or_already_revoked")
		return ErrRefreshTokenNotFound
	}

	logger.Info("revoke refresh token successful", "rows_affected", res.RowsAffected())
	return nil
}
