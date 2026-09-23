// Package data provides canonical persistence for Platform data models.
//
// focodebase/fobackend/internal/data/refresh_token_consumption.go
//
// GTM:
//
//	Layer: 2.2 Identity / Auth Domain
//	Release Class: SPINE
//	Reason:
//	  Extends TokenModel with the atomic single-use refresh-token consumption
//	  primitive required by mandatory refresh-token rotation.
//
// SPINE Rule:
//
//	Keep compiling.
//	Keep production-ready.
//	Preserve refresh-token hashes as the only persisted token representation.
//	Preserve DB-owned expiry and revocation timestamps.
//	Preserve atomic single-use consumption: one active refresh token may be
//	consumed successfully by at most one concurrent caller.
//	Never log or persist plaintext refresh credentials.
package data

import (
	"context"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"
)

// ConsumeRefreshToken atomically validates and revokes the active, unexpired
// refresh-token row identified by plainToken and returns the consumed record.
//
// This method is the persistence-level single-use boundary for refresh-token
// rotation. Validation and revocation are deliberately one SQL statement: the
// guarded UPDATE serializes concurrent consumers on the token row, and only one
// caller can transition revoked_at from NULL. A concurrent or repeated consumer
// receives ErrRefreshTokenInvalid.
//
// The plaintext token is used only as a parameter to the database hash
// comparison. It is never stored, returned, or logged.
func (m *TokenModel) ConsumeRefreshToken(
	ctx context.Context,
	plainToken string,
) (*Token, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.
		GetLoggerWithContextFromContext(ctx).
		WithFunctionName("ConsumeRefreshToken")

	plainToken = strings.TrimSpace(plainToken)
	if plainToken == "" {
		return nil, ErrRefreshTokenInvalid
	}

	const query = `
		UPDATE refresh_tokens
		SET revoked_at = NOW()
		WHERE token_hash = encode(digest($1, 'sha256'), 'hex')
		  AND revoked_at IS NULL
		  AND expires_at > NOW()
		RETURNING
			id,
			user_id,
			token_hash,
			expires_at,
			revoked_at,
			created_at
	`

	var token Token
	if err := m.DB.QueryRow(ctx, query, plainToken).Scan(
		&token.ID,
		&token.UserID,
		&token.TokenHash,
		&token.ExpiresAt,
		&token.RevokedAt,
		&token.CreatedAt,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			logger.Warn(
				"refresh token consumption rejected",
				"reason",
				"not_found_expired_revoked_or_already_consumed",
			)
			return nil, ErrRefreshTokenInvalid
		}

		logger.Error(
			"refresh token consumption failed",
			"error",
			err,
		)
		return nil, err
	}

	logger.Info(
		"refresh token consumed",
		"token_id",
		token.ID,
		"user_id",
		token.UserID,
	)

	return &token, nil
}
