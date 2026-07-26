// Package data provides models and database access methods for OAuth
// authorization codes, user consent records, and other entities.
//
// sdworkspace/sdbackend/internal/data/oauth_authorization.go
//
// GTM:
//
//	Layer: 2.2 Identity / Auth Domain
//	Release Class: DEFERRED
//	Reason:
//	  OAuth authorization codes and user consent records are valid OAuth
//	  provider infrastructure, but the Platform acting as an OAuth provider is not
//	  required for the initial Platform release spine. Do not expand this
//	  file until the release spine is functionally complete.
//
// DEFERRED Rule:
//
//	Keep compiling.
//	Keep safe.
//	Preserve authorization-code hashing.
//	Preserve revoked_at / revoked consent lifecycle semantics.
//	Preserve no generic SoftDelete/Delete behavior.
//	Do not add new features.
//	Do not route into v1 UI/API expansion.
//	Do not block deployment on this file unless it breaks the build.
package data

// NOTE TO MAINTAINERS:
//
// Do not introduce generic SoftDelete() or Delete() methods into this file.
//
// Rationale:
//   1. oauth_authorization_codes uses revoked_at as its canonical lifecycle
//      transition for one-time consumption / revocation.
//   2. oauth_user_consent uses revoked + revoked_at to preserve revocation
//      history while allowing a new active consent row later.
//   3. Adding deleted_at-based removal here would weaken OAuth audit/history
//      semantics and incorrectly treat security-state transitions as deletion.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/PiccoloMondoC/sdworkspace/sdbackend/internal/logging"
	"github.com/PiccoloMondoC/sdworkspace/sdbackend/internal/utils/timeutil"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type OauthAuthorizationCode struct {
	ID          uuid.UUID  `db:"id"           json:"id"`
	ClientID    uuid.UUID  `db:"client_id"    json:"client_id"`
	UserID      uuid.UUID  `db:"user_id"      json:"user_id"`
	CodeHash    string     `db:"code_hash"    json:"-"`
	RedirectURI string     `db:"redirect_uri" json:"redirect_uri"`
	ExpiresAt   time.Time  `db:"expires_at"   json:"expires_at"`
	CreatedAt   time.Time  `db:"created_at"   json:"created_at"`
	RevokedAt   *time.Time `db:"revoked_at"   json:"revoked_at,omitempty"`
}

type OauthUserConsent struct {
	ID        uuid.UUID  `db:"id"         json:"id"`
	UserID    uuid.UUID  `db:"user_id"    json:"user_id"`
	ClientID  uuid.UUID  `db:"client_id"  json:"client_id"`
	Scopes    []string   `db:"scopes"     json:"scopes"`
	CreatedAt time.Time  `db:"created_at" json:"created_at"`
	ExpiresAt time.Time  `db:"expires_at" json:"expires_at"`
	Revoked   bool       `db:"revoked"    json:"revoked"`
	RevokedAt *time.Time `db:"revoked_at" json:"revoked_at,omitempty"`
}

type OauthAuthorizationCodeModel struct {
	DB     *pgxpool.Pool
	Logger *logging.Logger
}

type OauthUserConsentModel struct {
	DB     *pgxpool.Pool
	Logger *logging.Logger
}

func hashOAuthAuthorizationCode(plainCode string) string {
	sum := sha256.Sum256([]byte(plainCode))
	return hex.EncodeToString(sum[:])
}

func normalizeOAuthScopes(scopes []string) []string {
	seen := make(map[string]struct{}, len(scopes))

	for _, scope := range scopes {
		scope = strings.TrimSpace(scope)
		if scope == "" {
			continue
		}
		seen[scope] = struct{}{}
	}

	out := make([]string, 0, len(seen))
	for scope := range seen {
		out = append(out, scope)
	}

	sort.Strings(out)
	return out
}

func scanOauthAuthorizationCode(row scannableRow, code *OauthAuthorizationCode) error {
	return row.Scan(
		&code.ID,
		&code.ClientID,
		&code.UserID,
		&code.CodeHash,
		&code.RedirectURI,
		&code.ExpiresAt,
		&code.CreatedAt,
		&code.RevokedAt,
	)
}

func scanOauthUserConsent(row scannableRow, c *OauthUserConsent) error {
	return row.Scan(
		&c.ID,
		&c.UserID,
		&c.ClientID,
		&c.Scopes,
		&c.CreatedAt,
		&c.ExpiresAt,
		&c.Revoked,
		&c.RevokedAt,
	)
}

func (m *OauthAuthorizationCodeModel) CreateAuthorizationCode(
	ctx context.Context,
	code *OauthAuthorizationCode,
	plainCode string,
) error {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("CreateAuthorizationCode")

	if code == nil {
		return errors.New("code is nil")
	}
	if code.ID == uuid.Nil {
		code.ID = uuid.New()
	}
	if code.ClientID == uuid.Nil {
		return errors.New("client_id is required")
	}
	if code.UserID == uuid.Nil {
		return errors.New("user_id is required")
	}

	code.RedirectURI = strings.TrimSpace(code.RedirectURI)
	if code.RedirectURI == "" {
		return errors.New("redirect_uri is required")
	}
	if !code.ExpiresAt.After(timeutil.Now()) {
		return errors.New("expires_at must be in the future")
	}
	code.ExpiresAt = code.ExpiresAt.UTC()

	if len(plainCode) < 20 {
		return errors.New("code value too short (minimum 20 characters)")
	}

	codeHash := hashOAuthAuthorizationCode(plainCode)

	const q = `
INSERT INTO oauth_authorization_codes
	(id, client_id, user_id, code_hash, redirect_uri, expires_at)
VALUES
	($1, $2, $3, $4, $5, $6)
RETURNING
	id, client_id, user_id, code_hash, redirect_uri, expires_at, created_at, revoked_at`

	err := scanOauthAuthorizationCode(
		m.DB.QueryRow(ctx, q,
			code.ID,
			code.ClientID,
			code.UserID,
			codeHash,
			code.RedirectURI,
			code.ExpiresAt,
		),
		code,
	)

	switch pgErr, ok := err.(*pgconn.PgError); {
	case ok && pgErr.Code == "23505":
		logger.Warn("duplicate oauth authorization code hash",
			"client_id", code.ClientID,
			"user_id", code.UserID,
		)
		return ErrDuplicate
	case err != nil:
		logger.Error("failed to create oauth authorization code",
			"client_id", code.ClientID,
			"user_id", code.UserID,
			"error", err,
		)
		return err
	default:
		logger.Info("oauth authorization code created",
			"code_id", code.ID,
			"client_id", code.ClientID,
			"user_id", code.UserID,
		)
		return nil
	}
}

func (m *OauthAuthorizationCodeModel) ValidateAndConsume(
	ctx context.Context,
	plainCode string,
	publicClientID string,
	userID uuid.UUID,
	redirectURI string,
) error {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("ValidateAndConsume")

	plainCode = strings.TrimSpace(plainCode)
	publicClientID = strings.TrimSpace(publicClientID)
	redirectURI = strings.TrimSpace(redirectURI)

	if plainCode == "" {
		return errors.New("plain authorization code is required")
	}
	if publicClientID == "" {
		return errors.New("public_client_id is required")
	}
	if userID == uuid.Nil {
		return errors.New("user_id is required")
	}
	if redirectURI == "" {
		return errors.New("redirect_uri is required")
	}

	codeHash := hashOAuthAuthorizationCode(plainCode)

	const q = `
UPDATE oauth_authorization_codes AS oac
SET revoked_at = NOW()
FROM oauth_clients AS oc
WHERE oac.client_id = oc.id
  AND oac.code_hash = $1
  AND oc.client_id = $2
  AND oc.is_active = TRUE
  AND oac.user_id = $3
  AND oac.redirect_uri = $4
  AND oac.revoked_at IS NULL
  AND oac.expires_at > NOW()
RETURNING oac.id`

	var consumedCodeID uuid.UUID
	err := m.DB.QueryRow(ctx, q, codeHash, publicClientID, userID, redirectURI).Scan(&consumedCodeID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrRecordNotFound
		}

		logger.Error("failed to validate and consume oauth authorization code",
			"user_id", userID,
			"public_client_id", publicClientID,
			"error", err,
		)
		return err
	}

	logger.Info("oauth authorization code consumed",
		"code_id", consumedCodeID,
		"user_id", userID,
		"public_client_id", publicClientID,
	)

	return nil
}

func (m *OauthAuthorizationCodeModel) RevokeAuthorizationCodeByID(
	ctx context.Context,
	codeID uuid.UUID,
) (*OauthAuthorizationCode, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("RevokeAuthorizationCodeByID")

	if codeID == uuid.Nil {
		return nil, errors.New("code_id is required")
	}

	const revokeQuery = `
UPDATE oauth_authorization_codes
SET revoked_at = NOW()
WHERE id = $1
  AND revoked_at IS NULL
RETURNING
	id, client_id, user_id, code_hash, redirect_uri, expires_at, created_at, revoked_at`

	code := &OauthAuthorizationCode{}
	err := scanOauthAuthorizationCode(m.DB.QueryRow(ctx, revokeQuery, codeID), code)
	if err == nil {
		logger.Info("oauth authorization code revoked", "code_id", code.ID)
		return code, nil
	}

	if !errors.Is(err, pgx.ErrNoRows) {
		logger.Error("failed to revoke oauth authorization code",
			"code_id", codeID,
			"error", err,
		)
		return nil, err
	}

	// The UPDATE above returned no rows. Inspect the current row state to
	// distinguish "already revoked" from "does not exist". A narrow TOCTOU
	// window exists between the two queries, but the outcomes remain correct:
	// a concurrent revocation resolves to ErrAlreadyRevoked, while a concurrent
	// exceptional hard delete resolves to ErrRecordNotFound.
	const stateQuery = `
SELECT revoked_at
FROM oauth_authorization_codes
WHERE id = $1`

	var revokedAt *time.Time
	stateErr := m.DB.QueryRow(ctx, stateQuery, codeID).Scan(&revokedAt)
	if stateErr != nil {
		if errors.Is(stateErr, pgx.ErrNoRows) {
			return nil, ErrRecordNotFound
		}

		logger.Error("failed to inspect oauth authorization code revocation state",
			"code_id", codeID,
			"error", stateErr,
		)
		return nil, stateErr
	}

	if revokedAt != nil {
		return nil, ErrAlreadyRevoked
	}

	return nil, ErrRecordNotFound
}

func (m *OauthUserConsentModel) SaveUserConsent(
	ctx context.Context,
	c *OauthUserConsent,
) error {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("SaveUserConsent")

	if c == nil {
		return errors.New("consent is nil")
	}
	if c.UserID == uuid.Nil {
		return errors.New("user_id is required")
	}
	if c.ClientID == uuid.Nil {
		return errors.New("client_id is required")
	}

	c.Scopes = normalizeOAuthScopes(c.Scopes)
	if len(c.Scopes) == 0 {
		return errors.New("at least one scope is required")
	}

	if !c.ExpiresAt.After(timeutil.Now()) {
		return errors.New("expires_at must be in the future")
	}
	c.ExpiresAt = c.ExpiresAt.UTC()

	// This UUID is only the candidate insert ID. If an active consent row
	// already exists, ON CONFLICT updates that row and RETURNING overwrites c.ID
	// with the existing row's canonical persisted ID.
	if c.ID == uuid.Nil {
		c.ID = uuid.New()
	}

	const q = `
INSERT INTO oauth_user_consent
	(id, user_id, client_id, scopes, expires_at, revoked, revoked_at)
VALUES
	($1, $2, $3, $4, $5, FALSE, NULL)
ON CONFLICT (user_id, client_id)
WHERE revoked = FALSE
DO UPDATE SET
	scopes = EXCLUDED.scopes,
	expires_at = EXCLUDED.expires_at,
	revoked = FALSE,
	revoked_at = NULL
RETURNING
	id, user_id, client_id, scopes, created_at, expires_at, revoked, revoked_at`

	err := scanOauthUserConsent(
		m.DB.QueryRow(ctx, q,
			c.ID,
			c.UserID,
			c.ClientID,
			c.Scopes,
			c.ExpiresAt,
		),
		c,
	)
	if err != nil {
		logger.Error("failed to save oauth user consent",
			"user_id", c.UserID,
			"client_id", c.ClientID,
			"error", err,
		)
		return err
	}

	logger.Info("oauth user consent saved",
		"consent_id", c.ID,
		"user_id", c.UserID,
		"client_id", c.ClientID,
	)

	return nil
}

func (m *OauthUserConsentModel) GetActiveUserConsent(
	ctx context.Context,
	userID uuid.UUID,
	clientID uuid.UUID,
) (*OauthUserConsent, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("GetActiveUserConsent")

	if userID == uuid.Nil {
		return nil, errors.New("user_id is required")
	}
	if clientID == uuid.Nil {
		return nil, errors.New("client_id is required")
	}

	const q = `
SELECT
	id, user_id, client_id, scopes, created_at, expires_at, revoked, revoked_at
FROM oauth_user_consent
WHERE user_id = $1
  AND client_id = $2
  AND revoked = FALSE
  AND expires_at > NOW()`

	consent := &OauthUserConsent{}
	err := scanOauthUserConsent(m.DB.QueryRow(ctx, q, userID, clientID), consent)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrRecordNotFound
		}

		logger.Error("failed to get active oauth user consent",
			"user_id", userID,
			"client_id", clientID,
			"error", err,
		)
		return nil, err
	}

	return consent, nil
}

func (m *OauthUserConsentModel) RevokeUserConsent(
	ctx context.Context,
	userID uuid.UUID,
	clientID uuid.UUID,
) (*OauthUserConsent, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("RevokeUserConsent")

	if userID == uuid.Nil {
		return nil, errors.New("user_id is required")
	}
	if clientID == uuid.Nil {
		return nil, errors.New("client_id is required")
	}

	const q = `
UPDATE oauth_user_consent
SET revoked = TRUE,
    revoked_at = NOW()
WHERE user_id = $1
  AND client_id = $2
  AND revoked = FALSE
  AND expires_at > NOW()
RETURNING
	id, user_id, client_id, scopes, created_at, expires_at, revoked, revoked_at`

	consent := &OauthUserConsent{}
	err := scanOauthUserConsent(m.DB.QueryRow(ctx, q, userID, clientID), consent)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrRecordNotFound
		}

		logger.Error("failed to revoke oauth user consent",
			"user_id", userID,
			"client_id", clientID,
			"error", err,
		)
		return nil, err
	}

	logger.Info("oauth user consent revoked",
		"consent_id", consent.ID,
		"user_id", userID,
		"client_id", clientID,
	)

	return consent, nil
}
