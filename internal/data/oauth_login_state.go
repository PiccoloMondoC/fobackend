// Package data provides models and database access methods for OAuth login state.
//
// sdworkspace/sdbackend/internal/data/oauth_login_state.go
//
// GTM:
//
//	Layer: 2.2.a External OAuth Login Consumption
//	Release Class: SPINE
//	Reason:
//	  OAuth login state is release-critical identity security infrastructure for
//	  v1 external login consumption. It preserves CSRF state validation, provider
//	  allowlisting, protected derived-value persistence, one-time state
//	  consumption, expiration enforcement, and cleanup of consumed/expired state.
//
// SPINE Rule:
//
//	Keep compiling.
//	Keep production-ready.
//	Preserve state hashing.
//	Preserve PKCE verifier hashing.
//	Preserve nonce hashing.
//	Preserve one-time consumption semantics.
//	Preserve expired-state rejection.
//	Preserve cleanup of consumed and expired state.
//	Block deployment if this file breaks build, OAuth callback validation,
//	state replay protection, or external-login integrity.
package data

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/PiccoloMondoC/focodebase/fobackend/internal/logging"
	"github.com/PiccoloMondoC/focodebase/fobackend/internal/utils/timeutil"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// OAuthLoginState represents a short-lived OAuth login state record.
//
// StateHash, CodeVerifierHash, and NonceHash are protected derived values.
// Plain state, PKCE verifier, and nonce values must never be persisted or logged.
type OAuthLoginState struct {
	ID uuid.UUID `json:"id" db:"id"`

	StateHash string `json:"-" db:"state_hash"`

	Provider string `json:"provider" db:"provider"`

	RedirectURI *string `json:"redirect_uri,omitempty" db:"redirect_uri"`

	CodeVerifierHash *string `json:"-" db:"code_verifier_hash"`

	NonceHash *string `json:"-" db:"nonce_hash"`

	ExpiresAt time.Time `json:"expires_at" db:"expires_at"`

	// ConsumedAt is nil while the state is usable. A non-nil value means the
	// state has already been consumed and must not validate again.
	ConsumedAt *time.Time `json:"consumed_at,omitempty" db:"consumed_at"`

	CreatedAt time.Time `json:"created_at" db:"created_at"`
}

// OAuthLoginStateModel owns persistence for short-lived OAuth login state.
type OAuthLoginStateModel struct {
	DB     *pgxpool.Pool
	Logger *logging.Logger
}

const oauthLoginStateColumns = `
	id,
	state_hash,
	provider,
	redirect_uri,
	code_verifier_hash,
	nonce_hash,
	expires_at,
	consumed_at,
	created_at
`

func hashOAuthLoginValue(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func scanOAuthLoginState(row interface{ Scan(...any) error }, s *OAuthLoginState) error {
	return row.Scan(
		&s.ID,
		&s.StateHash,
		&s.Provider,
		&s.RedirectURI,
		&s.CodeVerifierHash,
		&s.NonceHash,
		&s.ExpiresAt,
		&s.ConsumedAt,
		&s.CreatedAt,
	)
}

// Create persists a short-lived OAuth login state.
//
// The plain state, PKCE verifier, and nonce are hashed before persistence.
// Plain values must not be logged, returned, or stored.
func (m *OAuthLoginStateModel) Create(
	ctx context.Context,
	provider string,
	plainState string,
	redirectURI *string,
	plainCodeVerifier *string,
	plainNonce *string,
	expiresAt time.Time,
) (*OAuthLoginState, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("CreateOAuthLoginState")

	provider = normalizeOAuthProvider(provider)
	plainState = strings.TrimSpace(plainState)

	if err := validateOAuthProvider(provider); err != nil {
		return nil, err
	}
	if len(plainState) < 20 {
		return nil, errors.New("state value too short")
	}
	if !expiresAt.After(timeutil.Now()) {
		return nil, errors.New("expires_at must be in the future")
	}

	state := &OAuthLoginState{
		Provider:  provider,
		StateHash: hashOAuthLoginValue(plainState),
		ExpiresAt: expiresAt.UTC(),
	}

	if redirectURI != nil {
		trimmed := strings.TrimSpace(*redirectURI)
		if trimmed != "" {
			validated, err := validateHTTPURL(trimmed)
			if err != nil {
				return nil, fmt.Errorf("redirect_uri: %w", err)
			}
			state.RedirectURI = &validated
		}
	}

	if plainCodeVerifier != nil {
		trimmed := strings.TrimSpace(*plainCodeVerifier)
		if trimmed != "" {
			hashed := hashOAuthLoginValue(trimmed)
			state.CodeVerifierHash = &hashed
		}
	}

	if plainNonce != nil {
		trimmed := strings.TrimSpace(*plainNonce)
		if trimmed != "" {
			hashed := hashOAuthLoginValue(trimmed)
			state.NonceHash = &hashed
		}
	}

	query := fmt.Sprintf(`
		INSERT INTO oauth_login_states (
			state_hash,
			provider,
			redirect_uri,
			code_verifier_hash,
			nonce_hash,
			expires_at
		)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING %s
	`, oauthLoginStateColumns)

	if err := scanOAuthLoginState(
		m.DB.QueryRow(ctx, query,
			state.StateHash,
			state.Provider,
			state.RedirectURI,
			state.CodeVerifierHash,
			state.NonceHash,
			state.ExpiresAt,
		),
		state,
	); err != nil {
		if IsUniqueViolation(err) {
			logger.Warn("duplicate oauth login state", "provider", provider)
			return nil, ErrDuplicate
		}
		logger.Error("oauth login state create failed", err, "provider", provider)
		return nil, fmt.Errorf("create oauth login state: %w", err)
	}

	logger.Info("oauth login state created", "state_id", state.ID, "provider", state.Provider, "expires_at", state.ExpiresAt)
	return state, nil
}

// Consume validates and consumes a login state exactly once.
//
// Consumed states are retained with consumed_at set until DeleteExpired removes
// them. Replay attempts, expired states, and unknown states return ErrRecordNotFound.
func (m *OAuthLoginStateModel) Consume(ctx context.Context, provider string, plainState string) (*OAuthLoginState, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("ConsumeOAuthLoginState")

	provider = normalizeOAuthProvider(provider)
	plainState = strings.TrimSpace(plainState)

	if err := validateOAuthProvider(provider); err != nil {
		return nil, err
	}
	if plainState == "" {
		return nil, errors.New("state is required")
	}

	stateHash := hashOAuthLoginValue(plainState)

	query := fmt.Sprintf(`
		UPDATE oauth_login_states
		SET consumed_at = NOW()
		WHERE state_hash = $1
		  AND provider = $2
		  AND consumed_at IS NULL
		  AND expires_at > NOW()
		RETURNING %s
	`, oauthLoginStateColumns)

	state := &OAuthLoginState{}
	if err := scanOAuthLoginState(m.DB.QueryRow(ctx, query, stateHash, provider), state); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			logger.Warn("oauth login state not consumable", "provider", provider)
			return nil, ErrRecordNotFound
		}
		logger.Error("oauth login state consume failed", err, "provider", provider)
		return nil, fmt.Errorf("consume oauth login state: %w", err)
	}

	logger.Info("oauth login state consumed", "state_id", state.ID, "provider", state.Provider)
	return state, nil
}

// DeleteExpired removes expired or already-consumed OAuth login state records.
//
// This is the authoritative cleanup path for both expired states and consumed
// states. Consume marks successful validation with consumed_at; it does not
// delete the row inline.
func (m *OAuthLoginStateModel) DeleteExpired(ctx context.Context) (int64, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("DeleteExpiredOAuthLoginStates")

	const query = `
		DELETE FROM oauth_login_states
		WHERE expires_at <= NOW()
		   OR consumed_at IS NOT NULL
	`

	tag, err := m.DB.Exec(ctx, query)
	if err != nil {
		logger.Error("oauth login state cleanup failed", err)
		return 0, fmt.Errorf("delete expired oauth login states: %w", err)
	}

	rowsAffected := tag.RowsAffected()
	logger.Info("oauth login state cleanup completed", "rows_affected", rowsAffected)
	return rowsAffected, nil
}
