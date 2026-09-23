// Package data provides models and database access methods for external user identities.
//
// focodebase/fobackend/internal/data/user_external_identity.go
//
// GTM:
//
//	Layer: 2.2.a External OAuth Login Consumption
//	Release Class: SPINE
//	Reason:
//	  External identity linking is release-critical identity infrastructure for
//	  v1 OAuth login consumption. It preserves provider-subject uniqueness,
//	  account-link integrity, provider allowlisting, safe avatar URL handling,
//	  soft unlink behavior, and DB-owned lifecycle timestamps.
//
// SPINE Rule:
//
//	Keep compiling.
//	Keep production-ready.
//	Preserve external identity ownership integrity.
//	Preserve provider-subject uniqueness.
//	Preserve Google, Apple, and Facebook provider support.
//	Preserve safe URL validation.
//	Preserve soft unlink lifecycle behavior.
//	Block deployment if this file breaks build, OAuth login linking,
//	external identity lookup, or account ownership integrity.
package data

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/PiccoloMondoC/focodebase/fobackend/internal/logging"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// UserExternalIdentity represents an external OAuth identity linked to a
// Platform user account. Provider-subject ownership must not silently move
// between users.
type UserExternalIdentity struct {
	ID              uuid.UUID  `json:"id" db:"id"`
	UserID          uuid.UUID  `json:"user_id" db:"user_id"`
	Provider        string     `json:"provider" db:"provider"`
	ProviderSubject string     `json:"provider_subject" db:"provider_subject"`
	Email           *string    `json:"email,omitempty" db:"email"`
	EmailVerified   bool       `json:"email_verified" db:"email_verified"`
	DisplayName     *string    `json:"display_name,omitempty" db:"display_name"`
	AvatarURL       *string    `json:"avatar_url,omitempty" db:"avatar_url"`
	LinkedAt        time.Time  `json:"linked_at" db:"linked_at"`
	LastLoginAt     *time.Time `json:"last_login_at,omitempty" db:"last_login_at"`
	CreatedAt       time.Time  `json:"created_at" db:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at" db:"updated_at"`
	DeletedAt       *time.Time `json:"-" db:"deleted_at"`
}

// UserExternalIdentityModel owns persistence for user_external_identities.
type UserExternalIdentityModel struct {
	DB     *pgxpool.Pool
	Logger *logging.Logger
}

const userExternalIdentityColumns = `
	id,
	user_id,
	provider,
	provider_subject,
	email,
	email_verified,
	display_name,
	avatar_url,
	linked_at,
	last_login_at,
	created_at,
	updated_at,
	deleted_at
`

func normalizeOAuthProvider(provider string) string {
	return strings.ToLower(strings.TrimSpace(provider))
}

func validateOAuthProvider(provider string) error {
	switch provider {
	case "google", "apple", "facebook":
		return nil
	default:
		return fmt.Errorf("unsupported oauth provider: %s", provider)
	}
}

func prepareExternalIdentity(input *UserExternalIdentity) (*UserExternalIdentity, error) {
	if input == nil {
		return nil, errors.New("external identity is required")
	}
	if input.UserID == uuid.Nil {
		return nil, errors.New("user_id is required")
	}

	prepared := *input
	prepared.Provider = normalizeOAuthProvider(prepared.Provider)
	prepared.ProviderSubject = strings.TrimSpace(prepared.ProviderSubject)
	prepared.Email = cleanOptionalString(prepared.Email)
	prepared.DisplayName = cleanOptionalString(prepared.DisplayName)
	prepared.AvatarURL = cleanOptionalString(prepared.AvatarURL)

	if err := validateOAuthProvider(prepared.Provider); err != nil {
		return nil, err
	}
	if prepared.ProviderSubject == "" {
		return nil, errors.New("provider_subject is required")
	}
	if prepared.AvatarURL != nil {
		validated, err := validateHTTPURL(*prepared.AvatarURL)
		if err != nil {
			return nil, fmt.Errorf("avatar_url: %w", err)
		}
		prepared.AvatarURL = &validated
	}

	return &prepared, nil
}

func scanUserExternalIdentity(row interface{ Scan(...any) error }, identity *UserExternalIdentity) error {
	return row.Scan(
		&identity.ID,
		&identity.UserID,
		&identity.Provider,
		&identity.ProviderSubject,
		&identity.Email,
		&identity.EmailVerified,
		&identity.DisplayName,
		&identity.AvatarURL,
		&identity.LinkedAt,
		&identity.LastLoginAt,
		&identity.CreatedAt,
		&identity.UpdatedAt,
		&identity.DeletedAt,
	)
}

func collectUserExternalIdentity(row pgx.CollectableRow) (*UserExternalIdentity, error) {
	var identity UserExternalIdentity
	if err := scanUserExternalIdentity(row, &identity); err != nil {
		return nil, err
	}
	return &identity, nil
}

// Upsert links or refreshes an external OAuth identity for a user.
// It rejects cross-user provider-subject conflicts and rejects multiple active
// identities for the same user/provider pair.
func (m *UserExternalIdentityModel) Upsert(ctx context.Context, identity *UserExternalIdentity) error {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("UpsertUserExternalIdentity")

	prepared, err := prepareExternalIdentity(identity)
	if err != nil {
		return err
	}

	tx, err := m.DB.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		logger.Error("begin external identity upsert transaction failed", err, "user_id", prepared.UserID, "provider", prepared.Provider)
		return fmt.Errorf("begin external identity upsert transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	var existingOwnerID uuid.UUID
	err = tx.QueryRow(ctx, `
		SELECT user_id
		FROM user_external_identities
		WHERE provider = $1
		  AND provider_subject = $2
		FOR UPDATE
	`, prepared.Provider, prepared.ProviderSubject).Scan(&existingOwnerID)

	switch {
	case err == nil && existingOwnerID != prepared.UserID:
		logger.Warn("external identity provider-subject already belongs to another user",
			"user_id", prepared.UserID,
			"provider", prepared.Provider,
		)
		return ErrDuplicate

	case err == nil:
		query := fmt.Sprintf(`
			UPDATE user_external_identities
			SET
				email = $3,
				email_verified = $4,
				display_name = $5,
				avatar_url = $6,
				last_login_at = NOW(),
				deleted_at = NULL,
				updated_at = NOW()
			WHERE provider = $1
			  AND provider_subject = $2
			  AND user_id = $7
			RETURNING %s
		`, userExternalIdentityColumns)

		if err := scanUserExternalIdentity(
			tx.QueryRow(ctx, query,
				prepared.Provider,
				prepared.ProviderSubject,
				prepared.Email,
				prepared.EmailVerified,
				prepared.DisplayName,
				prepared.AvatarURL,
				prepared.UserID,
			),
			prepared,
		); err != nil {
			logger.Error("external identity refresh failed", err, "user_id", prepared.UserID, "provider", prepared.Provider)
			return fmt.Errorf("refresh external identity: %w", err)
		}

	case errors.Is(err, pgx.ErrNoRows):
		var activeSameProviderID uuid.UUID
		activeErr := tx.QueryRow(ctx, `
			SELECT id
			FROM user_external_identities
			WHERE user_id = $1
			  AND provider = $2
			  AND deleted_at IS NULL
			FOR UPDATE
		`, prepared.UserID, prepared.Provider).Scan(&activeSameProviderID)

		if activeErr == nil {
			logger.Warn("user already has active external identity for provider",
				"user_id", prepared.UserID,
				"provider", prepared.Provider,
			)
			return ErrDuplicate
		}
		if !errors.Is(activeErr, pgx.ErrNoRows) {
			logger.Error("active external identity cardinality check failed", activeErr, "user_id", prepared.UserID, "provider", prepared.Provider)
			return fmt.Errorf("check active external identity cardinality: %w", activeErr)
		}

		query := fmt.Sprintf(`
			INSERT INTO user_external_identities (
				user_id,
				provider,
				provider_subject,
				email,
				email_verified,
				display_name,
				avatar_url,
				last_login_at
			)
			VALUES ($1, $2, $3, $4, $5, $6, $7, NOW())
			RETURNING %s
		`, userExternalIdentityColumns)

		if err := scanUserExternalIdentity(
			tx.QueryRow(ctx, query,
				prepared.UserID,
				prepared.Provider,
				prepared.ProviderSubject,
				prepared.Email,
				prepared.EmailVerified,
				prepared.DisplayName,
				prepared.AvatarURL,
			),
			prepared,
		); err != nil {
			if IsUniqueViolation(err) {
				logger.Warn("external identity unique constraint violation",
					"user_id", prepared.UserID,
					"provider", prepared.Provider,
				)
				return ErrDuplicate
			}
			logger.Error("external identity insert failed", err, "user_id", prepared.UserID, "provider", prepared.Provider)
			return fmt.Errorf("insert external identity: %w", err)
		}

	default:
		logger.Error("external identity ownership check failed", err, "user_id", prepared.UserID, "provider", prepared.Provider)
		return fmt.Errorf("check external identity ownership: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		logger.Error("commit external identity upsert transaction failed", err, "user_id", prepared.UserID, "provider", prepared.Provider)
		return fmt.Errorf("commit external identity upsert transaction: %w", err)
	}

	*identity = *prepared
	logger.Info("external identity upserted", "user_id", identity.UserID, "provider", identity.Provider)
	return nil
}

// GetByProviderSubject returns the active external identity for a provider-issued
// subject identifier. It returns ErrRecordNotFound when no active row exists.
func (m *UserExternalIdentityModel) GetByProviderSubject(ctx context.Context, provider, providerSubject string) (*UserExternalIdentity, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("GetExternalIdentityByProviderSubject")

	provider = normalizeOAuthProvider(provider)
	providerSubject = strings.TrimSpace(providerSubject)

	if err := validateOAuthProvider(provider); err != nil {
		return nil, err
	}
	if providerSubject == "" {
		return nil, errors.New("provider_subject is required")
	}

	query := fmt.Sprintf(`
		SELECT %s
		FROM user_external_identities
		WHERE provider = $1
		  AND provider_subject = $2
		  AND deleted_at IS NULL
	`, userExternalIdentityColumns)

	var identity UserExternalIdentity
	if err := scanUserExternalIdentity(m.DB.QueryRow(ctx, query, provider, providerSubject), &identity); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			logger.Warn("external identity not found", "provider", provider)
			return nil, ErrRecordNotFound
		}
		logger.Error("external identity lookup failed", err, "provider", provider)
		return nil, fmt.Errorf("get external identity by provider subject: %w", err)
	}

	logger.Info("external identity resolved", "user_id", identity.UserID, "provider", provider)
	return &identity, nil
}

// ListByUserID returns active external identities linked to a user.
func (m *UserExternalIdentityModel) ListByUserID(ctx context.Context, userID uuid.UUID) ([]*UserExternalIdentity, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("ListExternalIdentitiesByUserID")

	if userID == uuid.Nil {
		return nil, errors.New("user_id is required")
	}

	query := fmt.Sprintf(`
		SELECT %s
		FROM user_external_identities
		WHERE user_id = $1
		  AND deleted_at IS NULL
		ORDER BY provider ASC, linked_at ASC
	`, userExternalIdentityColumns)

	rows, err := m.DB.Query(ctx, query, userID)
	if err != nil {
		logger.Error("external identity list query failed", err, "user_id", userID)
		return nil, fmt.Errorf("list external identities by user ID: %w", err)
	}

	identities, err := pgx.CollectRows(rows, collectUserExternalIdentity)
	if err != nil {
		logger.Error("external identity collection failed", err, "user_id", userID)
		return nil, fmt.Errorf("collect external identities by user ID: %w", err)
	}

	return identities, nil
}

// Unlink soft-removes the active external identity link for a user and provider.
func (m *UserExternalIdentityModel) Unlink(ctx context.Context, userID uuid.UUID, provider string) error {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("UnlinkExternalIdentity")

	provider = normalizeOAuthProvider(provider)

	if userID == uuid.Nil {
		return errors.New("user_id is required")
	}
	if err := validateOAuthProvider(provider); err != nil {
		return err
	}

	const query = `
		UPDATE user_external_identities
		SET deleted_at = NOW(),
		    updated_at = NOW()
		WHERE user_id = $1
		  AND provider = $2
		  AND deleted_at IS NULL
	`

	tag, err := m.DB.Exec(ctx, query, userID, provider)
	if err != nil {
		logger.Error("external identity unlink failed", err, "user_id", userID, "provider", provider)
		return fmt.Errorf("unlink external identity: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrRecordNotFound
	}

	logger.Info("external identity unlinked", "user_id", userID, "provider", provider)
	return nil
}
