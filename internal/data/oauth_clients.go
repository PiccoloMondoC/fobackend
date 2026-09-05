// Package data provides models and database access methods for OAuth clients.
//
// sdworkspace/sdbackend/internal/data/oauth_clients.go
//
// GTM:
//
//	Layer: 2.2 Identity / Auth Domain
//	Release Class: DEFERRED
//	Reason:
//	  OAuth clients are valid OAuth provider infrastructure, but the Platform
//	  acting as an OAuth provider for third-party applications is not required
//	  for the initial Platform release spine. Do not expand this file until
//	  the release spine is functionally complete.
//
// DEFERRED Rule:
//
//	Keep compiling.
//	Keep safe.
//	Preserve client secret hash protection.
//	Preserve redirect URI validation.
//	Preserve activation/deactivation semantics.
//	Do not add new features.
//	Do not route into v1 UI/API expansion.
//	Do not block deployment on this file unless it breaks the build.
package data

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/PiccoloMondoC/focodebase/fobackend/internal/logging"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type OauthClient struct {
	ID                  uuid.UUID `json:"id" db:"id"`
	ClientID            string    `json:"client_id" db:"client_id"`
	ClientSecretHash    string    `json:"-" db:"client_secret_hash"`
	IsActive            bool      `json:"is_active" db:"is_active"`
	AllowedRedirectURIs []string  `json:"allowed_redirect_uris" db:"allowed_redirect_uris"`
	CreatedAt           time.Time `json:"created_at" db:"created_at"`
	UpdatedAt           time.Time `json:"updated_at" db:"updated_at"`
}

type OauthClientModel struct {
	DB     *pgxpool.Pool
	Logger *logging.Logger
}

type oauthClientScanner interface {
	Scan(dest ...any) error
}

func scanOauthClient(row oauthClientScanner, c *OauthClient) error {
	return row.Scan(
		&c.ID,
		&c.ClientID,
		&c.ClientSecretHash,
		&c.IsActive,
		&c.AllowedRedirectURIs,
		&c.CreatedAt,
		&c.UpdatedAt,
	)
}

func normalizeClientID(clientID string) string {
	return strings.TrimSpace(clientID)
}

func normalizeRedirectURIs(values []string) ([]string, error) {
	if values == nil {
		return []string{}, nil
	}

	allowedCustomSchemes := map[string]struct{}{
		"sagrenti": {},
	}

	blockedSchemes := map[string]struct{}{
		"javascript": {},
		"data":       {},
		"vbscript":   {},
		"file":       {},
		"ftp":        {},
	}

	seen := make(map[string]struct{}, len(values))
	normalized := make([]string, 0, len(values))

	for _, raw := range values {
		value := strings.TrimSpace(raw)
		if value == "" {
			return nil, errors.New("allowed_redirect_uris cannot contain empty values")
		}

		parsed, err := url.Parse(value)
		if err != nil || parsed.Scheme == "" {
			return nil, fmt.Errorf("invalid redirect uri: %s", value)
		}

		scheme := strings.ToLower(parsed.Scheme)
		if _, blocked := blockedSchemes[scheme]; blocked {
			return nil, fmt.Errorf("unsafe redirect uri scheme: %s", scheme)
		}

		if scheme != "http" && scheme != "https" {
			if _, ok := allowedCustomSchemes[scheme]; !ok {
				return nil, fmt.Errorf("unsupported redirect uri scheme: %s", scheme)
			}
		}

		if _, exists := seen[value]; exists {
			continue
		}

		seen[value] = struct{}{}
		normalized = append(normalized, value)
	}

	return normalized, nil
}

func mapOauthClientInsertError(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return ErrDuplicateClientID
	}
	return err
}

func (m *OauthClientModel) CreateClient(ctx context.Context, c *OauthClient) error {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("CreateClient")

	if c == nil {
		err := errors.New("oauth client is nil")
		logger.Error("validation failed", "error", err)
		return err
	}

	c.ClientID = normalizeClientID(c.ClientID)
	if c.ClientID == "" {
		err := errors.New("client_id is required")
		logger.Error("validation failed", "error", err)
		return err
	}

	c.ClientSecretHash = strings.TrimSpace(c.ClientSecretHash)
	if c.ClientSecretHash == "" {
		err := errors.New("client_secret_hash is required")
		logger.Error("validation failed", "error", err, "client_id", c.ClientID)
		return err
	}

	redirectURIs, err := normalizeRedirectURIs(c.AllowedRedirectURIs)
	if err != nil {
		logger.Error("redirect uri validation failed", "error", err, "client_id", c.ClientID)
		return err
	}
	c.AllowedRedirectURIs = redirectURIs

	const stmt = `
		INSERT INTO oauth_clients (
			client_id,
			client_secret_hash,
			allowed_redirect_uris
		)
		VALUES ($1, $2, $3)
		RETURNING
			id,
			client_id,
			client_secret_hash,
			is_active,
			allowed_redirect_uris,
			created_at,
			updated_at;
	`

	err = scanOauthClient(
		m.DB.QueryRow(ctx, stmt, c.ClientID, c.ClientSecretHash, c.AllowedRedirectURIs),
		c,
	)
	if err != nil {
		mappedErr := mapOauthClientInsertError(err)
		if errors.Is(mappedErr, ErrDuplicateClientID) {
			logger.Warn("duplicate client_id", "client_id", c.ClientID)
			return mappedErr
		}

		logger.Error("database insert failed", "error", err, "client_id", c.ClientID)
		return err
	}

	logger.Info("oauth client created", "client_uuid", c.ID, "client_id", c.ClientID)
	return nil
}

// GetClientByClientID fetches an active OAuth client by public client_id.
//
// Normal OAuth flows must not receive inactive clients. Administrative/internal
// callers that need inactive records should use GetClientByUUID.
func (m *OauthClientModel) GetClientByClientID(ctx context.Context, clientID string) (*OauthClient, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	clientID = normalizeClientID(clientID)
	logger := m.Logger.GetLoggerWithContextFromContext(ctx).
		WithFunctionName("GetClientByClientID").
		WithCustomField("client_id", clientID)

	if clientID == "" {
		err := errors.New("client_id is required")
		logger.Error("validation failed", "error", err)
		return nil, err
	}

	const stmt = `
		SELECT
			id,
			client_id,
			client_secret_hash,
			is_active,
			allowed_redirect_uris,
			created_at,
			updated_at
		FROM oauth_clients
		WHERE client_id = $1
		  AND is_active = TRUE;
	`

	var c OauthClient
	if err := scanOauthClient(m.DB.QueryRow(ctx, stmt, clientID), &c); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			logger.Info("no active matching client")
			return nil, ErrRecordNotFound
		}

		logger.Error("query failed", "error", err)
		return nil, err
	}

	return &c, nil
}

func (m *OauthClientModel) GetClientByUUID(ctx context.Context, id uuid.UUID) (*OauthClient, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).
		WithFunctionName("GetClientByUUID").
		WithCustomField("client_uuid", id)

	if id == uuid.Nil {
		err := errors.New("id is required")
		logger.Error("validation failed", "error", err)
		return nil, err
	}

	const stmt = `
		SELECT
			id,
			client_id,
			client_secret_hash,
			is_active,
			allowed_redirect_uris,
			created_at,
			updated_at
		FROM oauth_clients
		WHERE id = $1;
	`

	var c OauthClient
	if err := scanOauthClient(m.DB.QueryRow(ctx, stmt, id), &c); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			logger.Info("no matching client")
			return nil, ErrRecordNotFound
		}

		logger.Error("query failed", "error", err)
		return nil, err
	}

	return &c, nil
}

// UpdateClient updates mutable OAuth client fields.
//
// client_id is immutable after creation. If ClientSecretHash is empty or
// whitespace-only, the existing protected secret hash is preserved.
func (m *OauthClientModel) UpdateClient(ctx context.Context, client *OauthClient) error {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("UpdateClient")

	if client == nil {
		err := errors.New("oauth client is nil")
		logger.Error("validation failed", "error", err)
		return err
	}

	if client.ID == uuid.Nil {
		err := errors.New("id is required")
		logger.Error("validation failed", "error", err)
		return err
	}

	redirectURIs, err := normalizeRedirectURIs(client.AllowedRedirectURIs)
	if err != nil {
		logger.Error("redirect uri validation failed", "error", err, "client_uuid", client.ID)
		return err
	}

	client.ClientSecretHash = strings.TrimSpace(client.ClientSecretHash)
	client.AllowedRedirectURIs = redirectURIs

	const stmt = `
		UPDATE oauth_clients
		SET
			client_secret_hash = COALESCE(NULLIF($1, ''), client_secret_hash),
			is_active = $2,
			allowed_redirect_uris = $3
		WHERE id = $4
		RETURNING
			id,
			client_id,
			client_secret_hash,
			is_active,
			allowed_redirect_uris,
			created_at,
			updated_at;
	`

	err = scanOauthClient(
		m.DB.QueryRow(ctx, stmt,
			client.ClientSecretHash,
			client.IsActive,
			client.AllowedRedirectURIs,
			client.ID,
		),
		client,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			logger.Warn("no matching client", "client_uuid", client.ID)
			return ErrRecordNotFound
		}

		logger.Error("database update failed", "error", err, "client_uuid", client.ID)
		return err
	}

	logger.Info("oauth client updated", "client_uuid", client.ID, "client_id", client.ClientID)
	return nil
}

func (m *OauthClientModel) DeactivateClient(ctx context.Context, clientID string) error {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	clientID = normalizeClientID(clientID)
	logger := m.Logger.GetLoggerWithContextFromContext(ctx).
		WithFunctionName("DeactivateClient").
		WithCustomField("client_id", clientID)

	if clientID == "" {
		err := errors.New("client_id is required")
		logger.Error("validation failed", "error", err)
		return err
	}

	const stmt = `
		UPDATE oauth_clients
		SET is_active = FALSE
		WHERE client_id = $1
		RETURNING id, is_active, updated_at;
	`

	var clientUUID uuid.UUID
	var isActive bool
	var updatedAt time.Time

	err := m.DB.QueryRow(ctx, stmt, clientID).Scan(&clientUUID, &isActive, &updatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			logger.Info("no matching client")
			return ErrRecordNotFound
		}

		logger.Error("deactivation failed", "error", err)
		return err
	}

	logger.Info(
		"oauth client deactivated",
		"client_uuid", clientUUID,
		"is_active", isActive,
		"updated_at", updatedAt,
	)
	return nil
}

func (m *OauthClientModel) ActivateClient(ctx context.Context, clientID string) error {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	clientID = normalizeClientID(clientID)
	logger := m.Logger.GetLoggerWithContextFromContext(ctx).
		WithFunctionName("ActivateClient").
		WithCustomField("client_id", clientID)

	if clientID == "" {
		err := errors.New("client_id is required")
		logger.Error("validation failed", "error", err)
		return err
	}

	const stmt = `
		UPDATE oauth_clients
		SET is_active = TRUE
		WHERE client_id = $1
		RETURNING id, is_active, updated_at;
	`

	var clientUUID uuid.UUID
	var isActive bool
	var updatedAt time.Time

	err := m.DB.QueryRow(ctx, stmt, clientID).Scan(&clientUUID, &isActive, &updatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			logger.Info("no matching client")
			return ErrRecordNotFound
		}

		logger.Error("activation failed", "error", err)
		return err
	}

	logger.Info(
		"oauth client activated",
		"client_uuid", clientUUID,
		"is_active", isActive,
		"updated_at", updatedAt,
	)
	return nil
}
