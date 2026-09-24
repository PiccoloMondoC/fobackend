// Package data provides models and database access methods for databases and other entities.
//
// focodebase/fobackend/internal/data/database.go
//
// GTM:
//
//	Layer: 2.1 Database / Governance Foundation
//	Release Class: SPINE
//	Reason:
//	  Database connection and schema creation are release-critical foundation
//	  infrastructure. This file establishes PostgreSQL connectivity, required
//	  extensions, shared triggers, UTC session enforcement, canonical schema
//	  objects, constraints, indexes, and lifecycle rules used across Future
//	  Offering v1 SPINE, minimal foundation, and DEFERRED domains.
//
// Future Offering v1 Doctrine:
//
//	Platform v1 is not a deals platform.
//	Platform v1 is a Future Offering anticipation platform.
//
//	SPINE means only what is required to let a merchant publish a Future
//	Offering and let consumers discover, watch, and express future intent
//	toward it.
//
// SPINE Rule:
//
//	Keep compiling.
//	Keep production-ready.
//	Preserve PostgreSQL connection validation.
//	Preserve UTC session enforcement.
//	Preserve required extensions, constraints, indexes, and triggers.
//	Preserve canonical schema integrity across all model domains.
//	Block deployment if this file breaks build, database connectivity,
//	schema creation, timestamp governance, lifecycle constraints,
//	or Future Offering v1 persistence integrity.
package data

import (
	"context"
	"fmt"
	"net/url"
	"time"

	"github.com/PiccoloMondoC/focodebase/fobackend/internal/bootstrap"
	"github.com/PiccoloMondoC/focodebase/fobackend/internal/logging"

	validation "github.com/go-ozzo/ozzo-validation/v4"
	"github.com/go-ozzo/ozzo-validation/v4/is"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	maxRetries                  = 5
	retryDelay                  = 2 * time.Second
	schemaInitializationTimeout = 2 * time.Minute
)

// DBConnectionParams contains the canonical PostgreSQL connection inputs used
// to validate startup database configuration before constructing the pgx pool.
// Password is accepted only as boundary input and must never be logged.
type DBConnectionParams struct {
	DSN         string
	Instance    string
	PrivateIP   string
	Port        string
	User        string
	Password    string
	Database    string
	MaxAttempts int
	Logger      *logging.Logger
}

// DBConnectionParamsModel owns database bootstrap operations for the data
// package, including PostgreSQL pool validation and canonical schema creation.
// It is SPINE infrastructure and must fail clearly when connectivity or schema
// initialization cannot be proven during startup.
type DBConnectionParamsModel struct {
	DB     *pgxpool.Pool
	Logger *logging.Logger
}

// ConnectWithConnector initializes and verifies a PostgreSQL connection pool.
//
// pgxpool.NewWithConfig is lazy, so this method must Ping the database before
// reporting success. The AfterConnect hook enforces UTC for every pooled
// session, preserving the database/session timezone contract required by BEG.
func (m *DBConnectionParamsModel) ConnectWithConnector(cfg *bootstrap.Config) (*pgxpool.Pool, error) {
	if m == nil || m.Logger == nil {
		return nil, fmt.Errorf("database connection model is not configured")
	}
	if cfg == nil {
		m.Logger.Error("Database configuration is missing")
		return nil, fmt.Errorf("database configuration is required")
	}
	if cfg.DBTimeout <= 0 {
		m.Logger.Error("Database connection timeout is invalid", "db_timeout", cfg.DBTimeout)
		return nil, fmt.Errorf("database connection timeout must be greater than zero")
	}

	params := &DBConnectionParams{
		User:      cfg.DBUser,
		Password:  cfg.DBPass,
		Database:  cfg.DBName,
		PrivateIP: cfg.DBHost,
		Port:      cfg.DBPort,
	}

	if err := validation.ValidateStruct(params,
		validation.Field(&params.User, validation.Required),
		validation.Field(&params.Password, validation.Required),
		validation.Field(&params.Database, validation.Required),
		validation.Field(&params.PrivateIP, validation.Required, is.Host),
		validation.Field(&params.Port, validation.Required, is.Port),
	); err != nil {
		m.Logger.Error("Database configuration validation failed", "error", err)
		return nil, fmt.Errorf("validate database configuration: %w", err)
	}

	dbURL := &url.URL{
		Scheme: "postgresql",
		User:   url.UserPassword(cfg.DBUser, cfg.DBPass),
		Host:   cfg.DBHost + ":" + cfg.DBPort,
		Path:   cfg.DBName,
	}

	poolConfig, err := pgxpool.ParseConfig(dbURL.String())
	if err != nil {
		m.Logger.Error("Failed to parse DB pool config", "error", err)
		return nil, fmt.Errorf("parse db pool config: %w", err)
	}

	poolConfig.AfterConnect = func(ctx context.Context, conn *pgx.Conn) error {
		if _, err := conn.Exec(ctx, `SET TIME ZONE 'UTC'`); err != nil {
			return fmt.Errorf("set session time zone to UTC: %w", err)
		}
		return nil
	}

	var lastErr error
	for attempt := 1; attempt <= maxRetries; attempt++ {
		ctx, cancel := context.WithTimeout(context.Background(), cfg.DBTimeout)
		dbPool, err := pgxpool.NewWithConfig(ctx, poolConfig)
		if err == nil {
			err = dbPool.Ping(ctx)
		}
		cancel()

		if err == nil {
			m.Logger.Info("Connected to the database successfully", "attempt", attempt)
			return dbPool, nil
		}

		lastErr = err
		if dbPool != nil {
			dbPool.Close()
		}

		m.Logger.Warn("Failed to verify database connection, retrying", "attempt", attempt, "max_attempts", maxRetries, "error", err)
		if attempt < maxRetries {
			time.Sleep(retryDelay)
		}
	}

	return nil, fmt.Errorf("verify database connection after %d attempts: %w", maxRetries, lastErr)
}

// CreateTables creates the canonical Platform schema objects required at startup.
//
// Schema creation runs inside a transaction so a failure cannot leave a partially
// initialized foundation. The timeout is intentionally longer than request-path
// timeouts because this SPINE initializer creates extensions, tables, indexes,
// triggers, and shared functions on cold databases.
func (m *DBConnectionParamsModel) CreateTables(db *pgxpool.Pool) error {
	if m == nil || m.Logger == nil {
		return fmt.Errorf("database connection model is not configured")
	}
	if db == nil {
		m.Logger.Error("Cannot create tables without a database pool")
		return fmt.Errorf("database pool is required")
	}

	ctx, cancel := context.WithTimeout(context.Background(), schemaInitializationTimeout)
	defer cancel()

	m.Logger.Info("Creating database schema")

	tx, err := db.Begin(ctx)
	if err != nil {
		m.Logger.Error("Failed to begin schema initialization transaction", "error", err)
		return fmt.Errorf("begin schema initialization transaction: %w", err)
	}

	committed := false
	defer func() {
		if committed {
			return
		}
		_ = tx.Rollback(context.Background())
	}()

	_, err = tx.Exec(ctx, `

	CREATE EXTENSION IF NOT EXISTS "uuid-ossp";
	CREATE EXTENSION IF NOT EXISTS "citext";
	CREATE EXTENSION IF NOT EXISTS "pgcrypto";
	CREATE EXTENSION IF NOT EXISTS btree_gist;

	-- ===============================================================
	-- Shared trigger to keep updated_at current
	-- ===============================================================
	CREATE OR REPLACE FUNCTION public.set_updated_at()
	RETURNS TRIGGER AS $$
	BEGIN
		NEW.updated_at = NOW();
		RETURN NEW;
	END;
	$$ LANGUAGE plpgsql;

	-- ===============================================================
	-- Shared slug normalizer
	-- ===============================================================
	CREATE OR REPLACE FUNCTION public.normalize_slug()
	RETURNS TRIGGER AS $$
	BEGIN
		IF NEW.slug IS NOT NULL THEN
			NEW.slug := LOWER(
				REGEXP_REPLACE(
					REGEXP_REPLACE(TRIM(NEW.slug), '\s+', '-', 'g'),
					'[^a-z0-9\-]', '', 'g'
				)
			);
		END IF;
		RETURN NEW;
	END;
	$$ LANGUAGE plpgsql;

	-- ===============================================================
	-- User-handle helpers
	-- ===============================================================
	CREATE OR REPLACE FUNCTION public.enforce_lowercase_user_handle()
	RETURNS TRIGGER AS $$
	BEGIN
		IF NEW.user_handle IS NOT NULL THEN
			NEW.user_handle := LOWER(NEW.user_handle);
		END IF;
		RETURN NEW;
	END;
	$$ LANGUAGE plpgsql;

	CREATE OR REPLACE FUNCTION public.prevent_reserved_user_handles()
	RETURNS TRIGGER AS $$
	DECLARE
		reserved TEXT[] := ARRAY['admin','support','root','system','help'];
	BEGIN
		IF NEW.user_handle IS NOT NULL AND NEW.user_handle = ANY(reserved) THEN
			RAISE EXCEPTION 'user_handle "%" is reserved', NEW.user_handle;
		END IF;
		RETURN NEW;
	END;
	$$ LANGUAGE plpgsql;

	-- ===============================================================
	-- Roles
	-- ===============================================================
	CREATE TABLE IF NOT EXISTS roles (
		id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
		name CITEXT UNIQUE NOT NULL,
		description TEXT NOT NULL,
		hierarchy_level INT NOT NULL,
		is_internal BOOLEAN NOT NULL DEFAULT FALSE,
		assignable_at_signup BOOLEAN NOT NULL DEFAULT FALSE,
		approval_required BOOLEAN NOT NULL DEFAULT FALSE,
		is_active BOOLEAN NOT NULL DEFAULT TRUE,
		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		deleted_at TIMESTAMPTZ
	);

	CREATE INDEX IF NOT EXISTS idx_roles_active_not_deleted
		ON roles (hierarchy_level, name)
		WHERE deleted_at IS NULL AND is_active = TRUE;

	-- ===============================================================
	-- Users
	-- ===============================================================
	CREATE TABLE IF NOT EXISTS users (
		id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
		google_id TEXT UNIQUE,
		facebook_id TEXT UNIQUE,
		email CITEXT UNIQUE NOT NULL,
		password_hash TEXT,
		is_active BOOLEAN NOT NULL DEFAULT TRUE,
		deleted_at TIMESTAMPTZ,
		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		CONSTRAINT chk_users_password_hash_not_blank
			CHECK (
				password_hash IS NULL OR btrim(password_hash) <> ''
			)
	);

	CREATE TABLE IF NOT EXISTS user_profiles (
		user_id UUID PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
		first_name TEXT NOT NULL,
		last_name TEXT NOT NULL,
		user_handle CITEXT UNIQUE,
		phone TEXT CHECK (phone ~ '^\+[1-9][0-9]{9,14}$'),
		preferred_contact TEXT NOT NULL DEFAULT 'email'
			CHECK (preferred_contact IN ('email', 'phone')),
		avatar_url TEXT CHECK (avatar_url ~* '^https?://'),
		bio TEXT CHECK (char_length(bio) <= 1000),
		location TEXT,
		company TEXT,
		website TEXT CHECK (website ~* '^https?://'),
		is_flagged BOOLEAN NOT NULL DEFAULT FALSE,
		moderation_notes TEXT,
		deleted_at TIMESTAMPTZ,
		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
	);

	CREATE INDEX IF NOT EXISTS idx_user_profiles_phone
		ON user_profiles(phone);

	CREATE INDEX IF NOT EXISTS idx_user_profiles_active_handle
		ON user_profiles(user_handle)
		WHERE deleted_at IS NULL;

	CREATE INDEX IF NOT EXISTS idx_user_profiles_active_updated_at
		ON user_profiles(updated_at DESC)
		WHERE deleted_at IS NULL;

	CREATE INDEX IF NOT EXISTS idx_user_profiles_search
		ON user_profiles
		USING GIN (
			to_tsvector(
				'simple',
				coalesce(first_name,'') || ' ' ||
				coalesce(last_name,'')  || ' ' ||
				coalesce(company,'')    || ' ' ||
				coalesce(location,'')
			)
		);

	CREATE TABLE IF NOT EXISTS global_handles (
		handle      TEXT        NOT NULL,
		entity_type TEXT        NOT NULL,
		entity_id   UUID        NOT NULL,
		created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),

		CONSTRAINT chk_global_handles_entity_type
			CHECK (entity_type IN ('user', 'merchant')),

		CONSTRAINT chk_global_handles_lowercase
			CHECK (handle = LOWER(TRIM(handle))),

		CONSTRAINT pk_global_handles
			PRIMARY KEY (handle, entity_type),

		CONSTRAINT ux_global_handles_entity
			UNIQUE (entity_type, entity_id)
	);

	CREATE INDEX IF NOT EXISTS idx_global_handles_entity_type
		ON global_handles(entity_type);

	CREATE INDEX IF NOT EXISTS idx_global_handles_entity_id
		ON global_handles(entity_id);

	DROP TRIGGER IF EXISTS lowercase_user_handle_trigger ON public.user_profiles;
	CREATE TRIGGER lowercase_user_handle_trigger
	BEFORE INSERT OR UPDATE ON public.user_profiles
	FOR EACH ROW EXECUTE FUNCTION public.enforce_lowercase_user_handle();

	DROP TRIGGER IF EXISTS prevent_reserved_handles_trigger ON public.user_profiles;
	CREATE TRIGGER prevent_reserved_handles_trigger
	BEFORE INSERT OR UPDATE ON public.user_profiles
	FOR EACH ROW EXECUTE FUNCTION public.prevent_reserved_user_handles();

	CREATE TABLE IF NOT EXISTS user_profile_moderation_logs (
		id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
		user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		action TEXT NOT NULL CHECK (action IN ('flagged', 'unflagged', 'note_added', 'suspended', 'restored')),
		note TEXT,
		performed_by UUID REFERENCES users(id) ON DELETE SET NULL,
		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
	);

	CREATE TABLE IF NOT EXISTS social_platforms (
		id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
		name CITEXT UNIQUE NOT NULL,
		base_url TEXT,
		description TEXT NOT NULL DEFAULT '',
		is_active BOOLEAN NOT NULL DEFAULT TRUE,
		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
	);

	CREATE TABLE IF NOT EXISTS user_profile_social_links (
		id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
		user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		platform_id UUID NOT NULL REFERENCES social_platforms(id) ON DELETE RESTRICT,
		url TEXT NOT NULL CHECK (url ~* '^https?://'),
		handle TEXT,
		is_primary BOOLEAN NOT NULL DEFAULT FALSE,
		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		UNIQUE (user_id, platform_id, url)
	);

	CREATE UNIQUE INDEX IF NOT EXISTS ux_user_profile_social_links_primary
		ON user_profile_social_links(user_id, platform_id)
		WHERE is_primary = TRUE;

	-- ===============================================================
	-- Notification lookups (must exist before dependent tables)
	-- ===============================================================
	CREATE TABLE IF NOT EXISTS notification_types (
		id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
		type CITEXT UNIQUE NOT NULL,
		description TEXT NOT NULL DEFAULT '',
		is_active BOOLEAN NOT NULL DEFAULT TRUE,
		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
	);

	CREATE TABLE IF NOT EXISTS notification_channels (
		id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
		channel CITEXT UNIQUE NOT NULL,
		description TEXT NOT NULL DEFAULT '',
		is_active BOOLEAN NOT NULL DEFAULT TRUE,
		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
	);

	CREATE TABLE IF NOT EXISTS user_notification_preferences (
		id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
		user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		notification_type_id UUID NOT NULL REFERENCES notification_types(id) ON DELETE RESTRICT,
		channel_id UUID NOT NULL REFERENCES notification_channels(id) ON DELETE RESTRICT,
		is_enabled BOOLEAN NOT NULL DEFAULT TRUE,
		frequency TEXT NOT NULL DEFAULT 'instant'
			CHECK (frequency IN ('instant', 'daily_digest', 'weekly_digest', 'disabled')),
		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		UNIQUE (user_id, notification_type_id, channel_id)
	);

	CREATE TABLE IF NOT EXISTS user_external_identities (
		id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
		user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		provider TEXT NOT NULL CHECK (provider IN ('google', 'apple', 'facebook')),
		provider_subject TEXT NOT NULL CHECK (btrim(provider_subject) <> ''),
		email CITEXT,
		email_verified BOOLEAN NOT NULL DEFAULT FALSE,
		display_name TEXT,
		avatar_url TEXT CHECK (avatar_url IS NULL OR avatar_url ~* '^https?://'),
		linked_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		last_login_at TIMESTAMPTZ,
		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		deleted_at TIMESTAMPTZ,
		UNIQUE (provider, provider_subject)
	);

	CREATE UNIQUE INDEX IF NOT EXISTS ux_user_external_identities_active_user_provider
		ON user_external_identities(user_id, provider)
		WHERE deleted_at IS NULL;

	CREATE INDEX IF NOT EXISTS idx_user_external_identities_user_id
		ON user_external_identities(user_id)
		WHERE deleted_at IS NULL;

	-- set_updated_at for user_external_identities is in the consolidated trigger section below.

	CREATE TABLE IF NOT EXISTS oauth_login_states (
		id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
		state_hash TEXT NOT NULL UNIQUE,
		provider TEXT NOT NULL CHECK (provider IN ('google', 'apple', 'facebook')),
		redirect_uri TEXT CHECK (redirect_uri IS NULL OR redirect_uri ~* '^https?://'),
		code_verifier_hash TEXT,
		nonce_hash TEXT,
		expires_at TIMESTAMPTZ NOT NULL,
		consumed_at TIMESTAMPTZ,
		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
	);

	CREATE INDEX IF NOT EXISTS idx_oauth_login_states_expires_at
		ON oauth_login_states(expires_at);

	CREATE INDEX IF NOT EXISTS idx_oauth_login_states_consumable
		ON oauth_login_states(state_hash, expires_at)
		WHERE consumed_at IS NULL;

	CREATE TABLE IF NOT EXISTS activation_tokens (
		id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
		user_id UUID NOT NULL UNIQUE REFERENCES users(id) ON DELETE CASCADE,
		token_hash TEXT NOT NULL UNIQUE,
		expires_at TIMESTAMPTZ NOT NULL,
		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
	);

	CREATE INDEX IF NOT EXISTS idx_activation_tokens_expires_at
		ON activation_tokens(expires_at);

	CREATE TABLE IF NOT EXISTS refresh_tokens (
		id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
		user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		token_hash TEXT NOT NULL UNIQUE,
		expires_at TIMESTAMPTZ NOT NULL,
		revoked_at TIMESTAMPTZ,
		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
	);

	CREATE INDEX IF NOT EXISTS idx_refresh_tokens_user_id
		ON refresh_tokens(user_id);

	CREATE TABLE IF NOT EXISTS auth_token_blacklist (
		jti TEXT PRIMARY KEY,
		user_id UUID REFERENCES users(id) ON DELETE SET NULL,
		revoked_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
	);

	CREATE INDEX IF NOT EXISTS idx_auth_token_blacklist_user_id
		ON auth_token_blacklist(user_id);

	CREATE INDEX IF NOT EXISTS idx_auth_token_blacklist_revoked_at
		ON auth_token_blacklist(revoked_at DESC);

	CREATE TABLE IF NOT EXISTS user_role_assignments (
		id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
		user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		role_id UUID NOT NULL REFERENCES roles(id) ON DELETE RESTRICT,
		assigned_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		assigned_by UUID REFERENCES users(id) ON DELETE SET NULL,
		is_primary BOOLEAN NOT NULL DEFAULT FALSE,
		updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		deleted_at TIMESTAMPTZ,
		UNIQUE (user_id, role_id)
	);

	CREATE UNIQUE INDEX IF NOT EXISTS ux_user_role_assignments_primary_role
		ON user_role_assignments(user_id)
		WHERE is_primary = TRUE AND deleted_at IS NULL;

	CREATE TABLE IF NOT EXISTS permissions (
		id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
		name TEXT UNIQUE NOT NULL,
		description TEXT,
		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
	);

	CREATE TABLE IF NOT EXISTS role_permissions (
		role_id UUID NOT NULL REFERENCES roles(id) ON DELETE CASCADE,
		permission_id UUID NOT NULL REFERENCES permissions(id) ON DELETE CASCADE,
		PRIMARY KEY (role_id, permission_id)
	);


	CREATE TABLE IF NOT EXISTS platform_settings (
		id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

		setting_key TEXT NOT NULL UNIQUE,
		setting_value JSONB NOT NULL,

		value_type TEXT NOT NULL CHECK (value_type IN (
			'boolean',
			'integer',
			'decimal',
			'string',
			'json'
		)),

		description TEXT NOT NULL DEFAULT '',
		is_active BOOLEAN NOT NULL DEFAULT TRUE,

		created_by UUID REFERENCES users(id) ON DELETE SET NULL,
		updated_by UUID REFERENCES users(id) ON DELETE SET NULL,

		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		deleted_at TIMESTAMPTZ,

		CONSTRAINT chk_platform_settings_key_format
			CHECK (setting_key ~ '^[a-z][a-z0-9_]*$')
	);

	CREATE INDEX IF NOT EXISTS idx_platform_settings_active_key
		ON platform_settings(setting_key)
		WHERE deleted_at IS NULL AND is_active = TRUE;


	CREATE TABLE IF NOT EXISTS platform_setting_history (
		id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

		platform_setting_id UUID NOT NULL REFERENCES platform_settings(id) ON DELETE RESTRICT,
		setting_key TEXT NOT NULL,

		previous_value JSONB,
		new_value JSONB NOT NULL,

		change_reason TEXT,
		changed_by UUID REFERENCES users(id) ON DELETE SET NULL,
		changed_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
	);

	CREATE INDEX IF NOT EXISTS idx_platform_setting_history_setting
		ON platform_setting_history(platform_setting_id, changed_at DESC);

	CREATE INDEX IF NOT EXISTS idx_platform_setting_history_key
		ON platform_setting_history(setting_key, changed_at DESC);


	-- ===============================================================
	-- Merchants
	-- ===============================================================
	-- Canonical development/pre-production definitions for the M01 merchant
	-- onboarding slice. Apply by correcting the canonical CREATE TABLE definitions
	-- and rebuilding the development database; do not accumulate ALTER TABLE drift.

	CREATE TABLE IF NOT EXISTS merchants (
		id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

		name CITEXT NOT NULL,
		display_name TEXT,
		logo_url TEXT,
		website TEXT,

		deleted_at TIMESTAMPTZ,
		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

		CONSTRAINT ux_merchants_name UNIQUE (name)
	);


	CREATE TABLE IF NOT EXISTS departments (
		id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
		name CITEXT NOT NULL,
		slug TEXT UNIQUE NOT NULL,
		description TEXT,
		sort_order INT NOT NULL DEFAULT 0,
		deleted_at TIMESTAMPTZ,
		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
	);

	DROP TRIGGER IF EXISTS normalize_departments_slug_trigger ON public.departments;
	CREATE TRIGGER normalize_departments_slug_trigger
	BEFORE INSERT OR UPDATE OF slug ON public.departments
	FOR EACH ROW EXECUTE FUNCTION public.normalize_slug();

	CREATE TABLE IF NOT EXISTS categories (
		id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
		name CITEXT NOT NULL,
		slug TEXT UNIQUE NOT NULL,
		department_id UUID NOT NULL REFERENCES departments(id) ON DELETE CASCADE,
		parent_id UUID REFERENCES categories(id) ON DELETE CASCADE,
		sort_order INT NOT NULL DEFAULT 0,
		description TEXT,
		deleted_at TIMESTAMPTZ,
		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
	);

	DROP TRIGGER IF EXISTS normalize_categories_slug_trigger ON public.categories;
	CREATE TRIGGER normalize_categories_slug_trigger
	BEFORE INSERT OR UPDATE OF slug ON public.categories
	FOR EACH ROW EXECUTE FUNCTION public.normalize_slug();

	CREATE INDEX IF NOT EXISTS idx_departments_deleted_at
		ON departments(deleted_at);

	CREATE INDEX IF NOT EXISTS idx_categories_parent_id ON categories(parent_id);
	CREATE INDEX IF NOT EXISTS idx_categories_department_id ON categories(department_id);
	CREATE INDEX IF NOT EXISTS idx_categories_deleted_at ON categories(deleted_at);


	-- Merchant Future Offerings
	CREATE TABLE IF NOT EXISTS merchant_future_offerings (
		id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

		merchant_id UUID NOT NULL
			REFERENCES merchants(id)
			ON DELETE CASCADE,

		/*
		* Merchant-owned project identity.
		*
		* project_name is required from creation because every Future Offering
		* is a merchant project and must have enough identity to be resumed.
		*/
		project_name TEXT NOT NULL
			CHECK (btrim(project_name) <> ''),

		/*
		* Authoritative Future Offering facts.
		*
		* These may be incomplete while status = 'draft'. Submission readiness
		* is therefore not expressed by unconditional NOT NULL constraints here.
		*/
		title TEXT
			CHECK (
				title IS NULL
					OR btrim(title) <> ''
			),

		summary TEXT NOT NULL DEFAULT '',

		description TEXT,

		category_id UUID
			REFERENCES categories(id)
			ON DELETE RESTRICT,

		offering_type TEXT
			CHECK (
				offering_type IS NULL
					OR offering_type IN (
						'product',
						'service',
						'event',
						'venue',
						'development',
						'experience'
					)
			),

		/*
		* How the Future Offering is intended to become available.
		*
		* This is deliberately separate from:
		*   - engagement options such as waitlist or preorder intent; and
		*   - access policy such as invite-only.
		*/
		release_strategy TEXT
			CHECK (
				release_strategy IS NULL
					OR release_strategy IN (
						'drop',
						'scheduled',
						'rolling',
						'limited_quantity'
					)
			),

		/*
		* Who may participate in the Future Offering.
		*
		* Access policy is distinct from release strategy and engagement
		* capabilities.
		*/
		access_policy TEXT
			CHECK (
				access_policy IS NULL
					OR access_policy IN (
						'public',
						'invite_only',
						'approval_required'
					)
			),

		status TEXT NOT NULL DEFAULT 'draft'
			CHECK (
				status IN (
					'draft',
					'submitted',
					'trust_review',
					'changes_requested',
					'approved',
					'published',
					'paused',
					'expired',
					'rejected',
					'unpublished',
					'archived'
				)
			),

		/*
		* Planned availability.
		*
		* NULL is valid while the Future Offering is incomplete or where the
		* merchant has not yet established an authoritative launch time.
		*/
		launch_at TIMESTAMPTZ,

		/*
		* Lifecycle occurrence timestamps.
		*
		* These record facts that have occurred. They are not substitutes for
		* the corresponding Future Offering event/history records.
		*/
		submitted_at TIMESTAMPTZ,
		approved_at TIMESTAMPTZ,
		published_at TIMESTAMPTZ,
		rejected_at TIMESTAMPTZ,
		unpublished_at TIMESTAMPTZ,
		archived_at TIMESTAMPTZ,

		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		deleted_at TIMESTAMPTZ
	);

	CREATE INDEX IF NOT EXISTS
		idx_merchant_future_offerings_merchant_status_updated
	ON merchant_future_offerings (
		merchant_id,
		status,
		updated_at DESC,
		id DESC
	)
	WHERE deleted_at IS NULL;

	CREATE INDEX IF NOT EXISTS
		idx_merchant_future_offerings_merchant_updated
	ON merchant_future_offerings (
		merchant_id,
		updated_at DESC,
		id DESC
	)
	WHERE deleted_at IS NULL;

	CREATE INDEX IF NOT EXISTS
		idx_merchant_future_offerings_category_status
	ON merchant_future_offerings (
		category_id,
		status,
		launch_at
	)
	WHERE deleted_at IS NULL
		AND category_id IS NOT NULL;

	CREATE INDEX IF NOT EXISTS
		idx_merchant_future_offerings_published
	ON merchant_future_offerings (
		published_at DESC
	)
	WHERE deleted_at IS NULL
		AND status = 'published';


	-- Merchant Future Offering Service Terms
	CREATE TABLE IF NOT EXISTS merchant_future_offering_service_terms (
		id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

		future_offering_id UUID NOT NULL
			REFERENCES merchant_future_offerings(id)
			ON DELETE RESTRICT,

		duration_months INTEGER NOT NULL
			CHECK (
				duration_months > 0
				AND duration_months <= 1188
			),

		term_starts_on DATE,
		term_ends_on DATE,

		term_status TEXT NOT NULL DEFAULT 'proposed'
			CHECK (
				term_status IN (
					'proposed',
					'established',
					'superseded'
				)
			),

		supersedes_service_term_id UUID,

		established_at TIMESTAMPTZ,
		superseded_at TIMESTAMPTZ,

		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

		CONSTRAINT uq_merchant_future_offering_service_terms_identity
			UNIQUE (
				id,
				future_offering_id
			),

		CONSTRAINT fk_merchant_future_offering_service_terms_supersedes
			FOREIGN KEY (
				supersedes_service_term_id,
				future_offering_id
			)
			REFERENCES merchant_future_offering_service_terms (
				id,
				future_offering_id
			)
			ON DELETE RESTRICT,

		CONSTRAINT chk_merchant_future_offering_service_terms_no_self_supersession
			CHECK (
				supersedes_service_term_id IS NULL
				OR supersedes_service_term_id <> id
			),

		CONSTRAINT chk_merchant_future_offering_service_terms_window
			CHECK (
				term_starts_on IS NULL
				OR term_ends_on IS NULL
				OR term_ends_on > term_starts_on
			),

		CONSTRAINT chk_merchant_future_offering_service_terms_proposed
			CHECK (
				term_status <> 'proposed'
				OR (
					established_at IS NULL
					AND superseded_at IS NULL
					AND supersedes_service_term_id IS NULL
				)
			),

		CONSTRAINT chk_merchant_future_offering_service_terms_established
			CHECK (
				term_status <> 'established'
				OR (
					term_starts_on IS NOT NULL
					AND term_ends_on IS NOT NULL
					AND established_at IS NOT NULL
					AND superseded_at IS NULL
				)
			),

		CONSTRAINT chk_merchant_future_offering_service_terms_superseded
			CHECK (
				term_status <> 'superseded'
				OR (
					term_starts_on IS NOT NULL
					AND term_ends_on IS NOT NULL
					AND established_at IS NOT NULL
					AND superseded_at IS NOT NULL
				)
			),

		CONSTRAINT chk_merchant_future_offering_service_terms_established_after_created
			CHECK (
				established_at IS NULL
				OR established_at >= created_at
			),

		CONSTRAINT chk_merchant_future_offering_service_terms_superseded_after_established
			CHECK (
				superseded_at IS NULL
				OR (
					established_at IS NOT NULL
					AND superseded_at >= established_at
				)
			)
		);

	CREATE UNIQUE INDEX IF NOT EXISTS
		ux_merchant_future_offering_service_terms_one_proposed
	ON merchant_future_offering_service_terms (
		future_offering_id
	)
	WHERE term_status = 'proposed';

	CREATE UNIQUE INDEX IF NOT EXISTS
		ux_merchant_future_offering_service_terms_one_established
	ON merchant_future_offering_service_terms (
		future_offering_id
	)
	WHERE term_status = 'established';

	CREATE UNIQUE INDEX IF NOT EXISTS
		ux_merchant_future_offering_service_terms_supersedes
	ON merchant_future_offering_service_terms (
		supersedes_service_term_id
	)
	WHERE supersedes_service_term_id IS NOT NULL;

	CREATE INDEX IF NOT EXISTS
		idx_merchant_future_offering_service_terms_timeline
	ON merchant_future_offering_service_terms (
		future_offering_id,
		created_at DESC,
		id DESC
	);

	CREATE OR REPLACE FUNCTION
		merchant_future_offering_service_terms_enforce_lifecycle()
	RETURNS TRIGGER
	LANGUAGE plpgsql
	AS $$
	BEGIN
		IF TG_OP = 'INSERT' THEN
			IF NEW.term_status <> 'proposed' THEN
				RAISE EXCEPTION
					'merchant_future_offering_service_terms: service terms must be created in proposed status (id=%)',
					NEW.id;
			END IF;

			RETURN NEW;
		END IF;

		IF OLD.term_status = 'superseded' THEN
			RAISE EXCEPTION
				'merchant_future_offering_service_terms: superseded service terms are immutable (id=%)',
				OLD.id;
		END IF;

		IF OLD.future_offering_id <> NEW.future_offering_id THEN
			RAISE EXCEPTION
				'merchant_future_offering_service_terms: future_offering_id is immutable (id=%)',
				OLD.id;
		END IF;

		IF OLD.term_status = 'established' THEN
			IF NEW.duration_months <> OLD.duration_months
				OR NEW.term_starts_on IS DISTINCT FROM OLD.term_starts_on
				OR NEW.term_ends_on IS DISTINCT FROM OLD.term_ends_on
				OR NEW.established_at IS DISTINCT FROM OLD.established_at
				OR NEW.supersedes_service_term_id
					IS DISTINCT FROM OLD.supersedes_service_term_id
			THEN
				RAISE EXCEPTION
					'merchant_future_offering_service_terms: established service term facts are immutable (id=%)',
					OLD.id;
			END IF;

			IF NEW.term_status NOT IN (
				'established',
				'superseded'
			) THEN
				RAISE EXCEPTION
					'merchant_future_offering_service_terms: invalid transition from established to % (id=%)',
					NEW.term_status,
					OLD.id;
			END IF;
		END IF;

		IF OLD.term_status = 'proposed'
			AND NEW.term_status NOT IN (
				'proposed',
				'established'
			)
		THEN
			RAISE EXCEPTION
				'merchant_future_offering_service_terms: invalid transition from proposed to % (id=%)',
				NEW.term_status,
				OLD.id;
		END IF;

		RETURN NEW;
	END;
	$$;

	DROP TRIGGER IF EXISTS
		trg_merchant_future_offering_service_terms_enforce_lifecycle
		ON merchant_future_offering_service_terms;

	CREATE TRIGGER
		trg_merchant_future_offering_service_terms_enforce_lifecycle
	BEFORE INSERT OR UPDATE ON merchant_future_offering_service_terms
	FOR EACH ROW
	EXECUTE FUNCTION
		merchant_future_offering_service_terms_enforce_lifecycle();


	-- Merchant Future Offering Service Periods
	CREATE TABLE IF NOT EXISTS merchant_future_offering_service_periods (
		id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

		service_term_id UUID NOT NULL,
		future_offering_id UUID NOT NULL,

		period_number INTEGER NOT NULL
			CHECK (period_number > 0),

		period_starts_on DATE NOT NULL,
		period_ends_on DATE NOT NULL,

		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

		CONSTRAINT fk_merchant_future_offering_service_periods_service_term
			FOREIGN KEY (
				service_term_id,
				future_offering_id
			)
			REFERENCES merchant_future_offering_service_terms (
				id,
				future_offering_id
			)
			ON DELETE RESTRICT,

		CONSTRAINT chk_merchant_future_offering_service_periods_window
			CHECK (
				period_ends_on > period_starts_on
			),

		CONSTRAINT excl_merchant_future_offering_service_periods_no_overlap
			EXCLUDE USING gist (
				future_offering_id WITH =,
				daterange(
					period_starts_on,
					period_ends_on,
					'[)'
				) WITH &&
			)
	);

	CREATE UNIQUE INDEX IF NOT EXISTS
		ux_merchant_future_offering_service_periods_term_number
	ON merchant_future_offering_service_periods (
		service_term_id,
		period_number
	);

	CREATE INDEX IF NOT EXISTS
		idx_merchant_future_offering_service_periods_term_timeline
	ON merchant_future_offering_service_periods (
		service_term_id,
		period_starts_on,
		id
	);

	CREATE INDEX IF NOT EXISTS
		idx_merchant_future_offering_service_periods_fo_timeline
	ON merchant_future_offering_service_periods (
		future_offering_id,
		period_starts_on,
		id
	);

	CREATE INDEX IF NOT EXISTS
		idx_merchant_future_offering_service_periods_term_sequence
	ON merchant_future_offering_service_periods (
		service_term_id,
		period_number,
		id
	);

	CREATE INDEX IF NOT EXISTS
		idx_merchant_future_offering_service_periods_fo_window
	ON merchant_future_offering_service_periods (
		future_offering_id,
		period_starts_on,
		period_ends_on,
		id
	);


	CREATE OR REPLACE FUNCTION
		merchant_future_offering_service_periods_validate_insert()
	RETURNS TRIGGER
	LANGUAGE plpgsql
	AS $$
	DECLARE
		v_term_starts_on DATE;
		v_term_ends_on DATE;
		v_term_status TEXT;
	BEGIN
		SELECT
			term_starts_on,
			term_ends_on,
			term_status
		INTO
			v_term_starts_on,
			v_term_ends_on,
			v_term_status
		FROM merchant_future_offering_service_terms
		WHERE id = NEW.service_term_id
			AND future_offering_id = NEW.future_offering_id
		FOR SHARE;

		IF NOT FOUND THEN
			RAISE EXCEPTION
				'merchant_future_offering_service_periods: service term % does not belong to future offering %',
				NEW.service_term_id,
				NEW.future_offering_id;
		END IF;

		IF v_term_status <> 'established' THEN
			RAISE EXCEPTION
				'merchant_future_offering_service_periods: service term must be established before a service period may be created (service_term_id=%, status=%)',
				NEW.service_term_id,
				v_term_status;
		END IF;

		IF v_term_starts_on IS NULL
			OR v_term_ends_on IS NULL
		THEN
			RAISE EXCEPTION
				'merchant_future_offering_service_periods: established service term must have a complete service window (service_term_id=%)',
				NEW.service_term_id;
		END IF;

		IF NEW.period_starts_on < v_term_starts_on
			OR NEW.period_ends_on > v_term_ends_on
		THEN
			RAISE EXCEPTION
				'merchant_future_offering_service_periods: service period [%, %) must be contained within service term [%, %) (service_term_id=%)',
				NEW.period_starts_on,
				NEW.period_ends_on,
				v_term_starts_on,
				v_term_ends_on,
				NEW.service_term_id;
		END IF;

		RETURN NEW;
	END;
	$$;

	DROP TRIGGER IF EXISTS
		trg_merchant_future_offering_service_periods_validate_insert
		ON merchant_future_offering_service_periods;

	CREATE TRIGGER
		trg_merchant_future_offering_service_periods_validate_insert
	BEFORE INSERT ON merchant_future_offering_service_periods
	FOR EACH ROW
	EXECUTE FUNCTION
		merchant_future_offering_service_periods_validate_insert();


	CREATE OR REPLACE FUNCTION
		merchant_future_offering_service_periods_enforce_lifecycle()
	RETURNS TRIGGER
	LANGUAGE plpgsql
	AS $$
	BEGIN
		IF TG_OP = 'DELETE' THEN
			RAISE EXCEPTION
				'merchant_future_offering_service_periods: service periods cannot be deleted (id=%)',
				OLD.id;
		END IF;

		RAISE EXCEPTION
			'merchant_future_offering_service_periods: established service period facts are immutable (id=%)',
			OLD.id;
	END;
	$$;

	DROP TRIGGER IF EXISTS
		trg_merchant_future_offering_service_periods_enforce_lifecycle
		ON merchant_future_offering_service_periods;

	CREATE TRIGGER
		trg_merchant_future_offering_service_periods_enforce_lifecycle
	BEFORE UPDATE OR DELETE
	ON merchant_future_offering_service_periods
	FOR EACH ROW
	EXECUTE FUNCTION
		merchant_future_offering_service_periods_enforce_lifecycle();


	-- =====================================================================
	-- Merchant Future Offering Billing Periods
	-- =====================================================================

	CREATE TABLE IF NOT EXISTS merchant_future_offering_billing_periods (
		id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

		service_term_id UUID NOT NULL,
		future_offering_id UUID NOT NULL,

		period_number INTEGER NOT NULL
			CHECK (
				period_number > 0
				AND period_number <= 1188
			),

		period_starts_on DATE NOT NULL,
		period_ends_on DATE NOT NULL,

		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

		CONSTRAINT fk_merchant_future_offering_billing_periods_service_term
			FOREIGN KEY (
				service_term_id,
				future_offering_id
			)
			REFERENCES merchant_future_offering_service_terms (
				id,
				future_offering_id
			)
			ON DELETE RESTRICT,

		CONSTRAINT chk_merchant_future_offering_billing_periods_window
			CHECK (
				period_ends_on > period_starts_on
			),

		/*
		* A Future Offering may have only one authoritative Billing Period
		* covering any given date, including across Service Term revisions.
		*
		* Billing Periods use half-open intervals:
		*
		*     [period_starts_on, period_ends_on)
		*/
		CONSTRAINT excl_merchant_future_offering_billing_periods_no_overlap
			EXCLUDE USING gist (
				future_offering_id WITH =,
				daterange(
					period_starts_on,
					period_ends_on,
					'[)'
				) WITH &&
			)
	);


	-- Exactly one authoritative Billing Period number exists within a
	-- Service Term.
	CREATE UNIQUE INDEX IF NOT EXISTS
		ux_merchant_future_offering_billing_periods_term_number
	ON merchant_future_offering_billing_periods (
		service_term_id,
		period_number
	);


	-- Chronological Billing Period history for one Service Term.
	CREATE INDEX IF NOT EXISTS
		idx_merchant_future_offering_billing_periods_term_timeline
	ON merchant_future_offering_billing_periods (
		service_term_id,
		period_starts_on,
		id
	);


	-- Chronological Billing Period history for the Future Offering across
	-- Service Term revisions.
	CREATE INDEX IF NOT EXISTS
		idx_merchant_future_offering_billing_periods_fo_timeline
	ON merchant_future_offering_billing_periods (
		future_offering_id,
		period_starts_on,
		id
	);


	-- =====================================================================
	-- Canonical Billing Period calendar boundary
	-- =====================================================================
	--
	-- Billing Periods are monthly accounting windows.
	--
	-- Boundaries are calculated independently from the authoritative
	-- Service Term start anchor. They do not derive from Service Period rows.
	--
	-- Calendar semantics:
	--
	--   ordinary anchor:
	--       Jan 30 + 1 month -> Feb 28
	--       Jan 30 + 2 months -> Mar 30
	--
	--   month-end anchor:
	--       Jan 31 + 1 month -> Feb 28
	--       Jan 31 + 2 months -> Mar 31
	--
	-- This preserves the original anchor and prevents short-month drift.
	-- =====================================================================

	CREATE OR REPLACE FUNCTION
		merchant_future_offering_billing_period_boundary(
			p_anchor DATE,
			p_months INTEGER
		)
	RETURNS DATE
	LANGUAGE plpgsql
	IMMUTABLE
	STRICT
	AS $$
	DECLARE
		v_anchor_month_end DATE;
		v_target_month_start DATE;
		v_target_month_end DATE;
		v_target_day INTEGER;
	BEGIN
		IF p_months < 0 THEN
			RAISE EXCEPTION
				'merchant_future_offering_billing_period_boundary: month offset must not be negative';
		END IF;

		v_anchor_month_end :=
			(
				date_trunc(
					'month',
					p_anchor::timestamp
				)
				+ interval '1 month'
				- interval '1 day'
			)::date;

		v_target_month_start :=
			(
				date_trunc(
					'month',
					p_anchor::timestamp
				)
				+ make_interval(months => p_months)
			)::date;

		v_target_month_end :=
			(
				v_target_month_start
				+ interval '1 month'
				- interval '1 day'
			)::date;

		-- Original anchor is month-end:
		-- month-end remains month-end.
		IF p_anchor = v_anchor_month_end THEN
			RETURN v_target_month_end;
		END IF;

		v_target_day :=
			LEAST(
				EXTRACT(DAY FROM p_anchor)::INTEGER,
				EXTRACT(DAY FROM v_target_month_end)::INTEGER
			);

		RETURN
			v_target_month_start
			+ (v_target_day - 1);
	END;
	$$;


	-- =====================================================================
	-- Insert validation
	-- =====================================================================

	CREATE OR REPLACE FUNCTION
		merchant_future_offering_billing_periods_validate_insert()
	RETURNS TRIGGER
	LANGUAGE plpgsql
	AS $$
	DECLARE
		v_term_starts_on DATE;
		v_term_ends_on DATE;
		v_term_status TEXT;

		v_successor_starts_on DATE;
		v_effective_term_ends_on DATE;

		v_latest_period_number INTEGER;
		v_latest_period_ends_on DATE;

		v_expected_period_number INTEGER;
		v_expected_starts_on DATE;
		v_expected_ends_on DATE;
	BEGIN
		/*
		* Serialize Billing Period creation through the owning Service Term.
		*
		* Established terms support ordinary JIT creation.
		* Superseded terms support historical catch-up only.
		*/
		SELECT
			st.term_starts_on,
			st.term_ends_on,
			st.term_status,
			successor.term_starts_on
		INTO
			v_term_starts_on,
			v_term_ends_on,
			v_term_status,
			v_successor_starts_on
		FROM merchant_future_offering_service_terms AS st
		LEFT JOIN merchant_future_offering_service_terms AS successor
			ON successor.supersedes_service_term_id = st.id
			AND successor.future_offering_id = st.future_offering_id
		WHERE st.id = NEW.service_term_id
			AND st.future_offering_id = NEW.future_offering_id
		FOR UPDATE OF st;

		IF NOT FOUND THEN
			RAISE EXCEPTION
				'merchant_future_offering_billing_periods: service term % does not belong to future offering %',
				NEW.service_term_id,
				NEW.future_offering_id;
		END IF;

		IF v_term_status NOT IN (
			'established',
			'superseded'
		) THEN
			RAISE EXCEPTION
				'merchant_future_offering_billing_periods: service term must be established or superseded before billing periods may be created (service_term_id=%, status=%)',
				NEW.service_term_id,
				v_term_status;
		END IF;

		IF v_term_starts_on IS NULL
			OR v_term_ends_on IS NULL
		THEN
			RAISE EXCEPTION
				'merchant_future_offering_billing_periods: service term must have a complete authoritative service window (service_term_id=%)',
				NEW.service_term_id;
		END IF;

		/*
		* For an established Service Term, its authoritative term end is the
		* Billing Period coverage ceiling.
		*
		* For a superseded Service Term, historical catch-up may continue only
		* through the boundary at which its direct successor became authoritative.
		* The predecessor's original immutable term_ends_on must not reclaim dates
		* that now belong to the successor.
		*/
		v_effective_term_ends_on := v_term_ends_on;

		IF v_term_status = 'superseded' THEN
			IF v_successor_starts_on IS NULL THEN
				RAISE EXCEPTION
					'merchant_future_offering_billing_periods: superseded service term requires a direct successor boundary for historical billing catch-up (service_term_id=%)',
					NEW.service_term_id;
			END IF;

			v_effective_term_ends_on :=
				LEAST(
					v_term_ends_on,
					v_successor_starts_on
				);
		END IF;

		IF v_effective_term_ends_on <= v_term_starts_on THEN
			RAISE EXCEPTION
				'merchant_future_offering_billing_periods: effective billing window is invalid for service term %',
				NEW.service_term_id;
		END IF;

		/*
		* Billing Periods are accounting facts, not future schedules.
		*
		* Historical catch-up is permitted. Future pre-generation is not.
		*/
		IF NEW.period_starts_on > CURRENT_DATE THEN
			RAISE EXCEPTION
				'merchant_future_offering_billing_periods: future billing periods must not be pre-generated (period_starts_on=%)',
				NEW.period_starts_on;
		END IF;

		SELECT
			period_number,
			period_ends_on
		INTO
			v_latest_period_number,
			v_latest_period_ends_on
		FROM merchant_future_offering_billing_periods
		WHERE service_term_id = NEW.service_term_id
			AND future_offering_id = NEW.future_offering_id
		ORDER BY period_number DESC
		LIMIT 1;

		IF NOT FOUND THEN
			v_expected_period_number := 1;
		ELSE
			v_expected_period_number :=
				v_latest_period_number + 1;
		END IF;

		IF NEW.period_number <> v_expected_period_number THEN
			RAISE EXCEPTION
				'merchant_future_offering_billing_periods: billing period number must be sequential (service_term_id=%, expected_period_number=%, supplied_period_number=%)',
				NEW.service_term_id,
				v_expected_period_number,
				NEW.period_number;
		END IF;

		v_expected_starts_on :=
			merchant_future_offering_billing_period_boundary(
				v_term_starts_on,
				NEW.period_number - 1
			);

		v_expected_ends_on :=
			LEAST(
				merchant_future_offering_billing_period_boundary(
					v_term_starts_on,
					NEW.period_number
				),
				v_effective_term_ends_on
			);

		IF v_expected_starts_on >= v_effective_term_ends_on THEN
			RAISE EXCEPTION
				'merchant_future_offering_billing_periods: no further billing period exists within authoritative service coverage for service term %',
				NEW.service_term_id;
		END IF;

		IF NEW.period_starts_on <> v_expected_starts_on
			OR NEW.period_ends_on <> v_expected_ends_on
		THEN
			RAISE EXCEPTION
				'merchant_future_offering_billing_periods: invalid authoritative monthly window [%, %); expected [%, %) for service_term_id=% period_number=%',
				NEW.period_starts_on,
				NEW.period_ends_on,
				v_expected_starts_on,
				v_expected_ends_on,
				NEW.service_term_id,
				NEW.period_number;
		END IF;

		IF v_latest_period_number IS NOT NULL
			AND v_latest_period_ends_on <> v_expected_starts_on
		THEN
			RAISE EXCEPTION
				'merchant_future_offering_billing_periods: persisted billing chronology is not contiguous (service_term_id=%, preceding_period_number=%, preceding_ends_on=%, expected_next_starts_on=%)',
				NEW.service_term_id,
				v_latest_period_number,
				v_latest_period_ends_on,
				v_expected_starts_on;
		END IF;

		RETURN NEW;
	END;
	$$;


	DROP TRIGGER IF EXISTS
		trg_merchant_future_offering_billing_periods_validate_insert
		ON merchant_future_offering_billing_periods;

	CREATE TRIGGER
		trg_merchant_future_offering_billing_periods_validate_insert
	BEFORE INSERT ON merchant_future_offering_billing_periods
	FOR EACH ROW
	EXECUTE FUNCTION
		merchant_future_offering_billing_periods_validate_insert();


	-- =====================================================================
	-- Immutable accounting-fact lifecycle
	-- =====================================================================

	CREATE OR REPLACE FUNCTION
		merchant_future_offering_billing_periods_enforce_immutability()
	RETURNS TRIGGER
	LANGUAGE plpgsql
	AS $$
	BEGIN
		IF TG_OP = 'DELETE' THEN
			RAISE EXCEPTION
				'merchant_future_offering_billing_periods: billing periods cannot be deleted (id=%)',
				OLD.id;
		END IF;

		RAISE EXCEPTION
			'merchant_future_offering_billing_periods: authoritative billing period facts are immutable (id=%)',
			OLD.id;
	END;
	$$;


	DROP TRIGGER IF EXISTS
		trg_merchant_future_offering_billing_periods_enforce_immutability
		ON merchant_future_offering_billing_periods;

	CREATE TRIGGER
		trg_merchant_future_offering_billing_periods_enforce_immutability
	BEFORE UPDATE OR DELETE
	ON merchant_future_offering_billing_periods
	FOR EACH ROW
	EXECUTE FUNCTION
		merchant_future_offering_billing_periods_enforce_immutability();

	-- ===============================================================
	-- Merchant Platform Credits / Merchant Platform Credit Eligible Fee Types
	-- Non-v1 merchant credit account and eligibility infrastructure.
	-- Keep schema compile-safe, but do not expand routes, services, UI,
	-- handlers, or tests for Future Offering v1.
	-- ===============================================================
	-- Merchant Platform Credit Accounts
	CREATE TABLE IF NOT EXISTS merchant_platform_credit_accounts (
		id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

		merchant_id UUID NOT NULL
			REFERENCES merchants(id)
			ON DELETE RESTRICT,

		status TEXT NOT NULL DEFAULT 'active'
			CHECK (status IN (
				'active',
				'exhausted',
				'expired',
				'cancelled'
			)),

		original_amount NUMERIC(19,4) NOT NULL
			CHECK (original_amount > 0),

		remaining_amount NUMERIC(19,4) NOT NULL
			CHECK (remaining_amount >= 0),

		currency CHAR(3) NOT NULL
			CHECK (currency ~ '^[A-Z]{3}$'),

		starts_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		expires_at TIMESTAMPTZ,

		source_code TEXT
			CHECK (
				source_code IS NULL
				OR char_length(source_code) <= 128
			),

		note TEXT
			CHECK (
				note IS NULL
				OR char_length(note) <= 2000
			),

		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

		CONSTRAINT chk_merchant_platform_credit_accounts_amounts
			CHECK (
				remaining_amount <= original_amount
			),

		CONSTRAINT chk_merchant_platform_credit_accounts_dates
			CHECK (
				expires_at IS NULL
				OR expires_at > starts_at
			)
	);

	-- Merchant history and administrative listing.
	CREATE INDEX IF NOT EXISTS
		idx_merchant_platform_credit_accounts_merchant_created
	ON merchant_platform_credit_accounts (
		merchant_id,
		created_at DESC,
		id DESC
	);

	-- Currently usable-account lookup and deterministic availability ordering.
	CREATE INDEX IF NOT EXISTS
		idx_merchant_platform_credit_accounts_usable
	ON merchant_platform_credit_accounts (
		merchant_id,
		currency,
		expires_at,
		created_at,
		id
	)
	WHERE status = 'active'
	AND remaining_amount > 0;

	-- Cross-merchant expiration processing.
	CREATE INDEX IF NOT EXISTS
		idx_merchant_platform_credit_accounts_active_expiration
	ON merchant_platform_credit_accounts (
		expires_at,
		id
	)
	WHERE status = 'active'
	AND expires_at IS NOT NULL;


	CREATE TABLE IF NOT EXISTS merchant_fee_types (
		id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

		code TEXT NOT NULL UNIQUE
			CHECK (btrim(code) <> ''),

		display_name TEXT NOT NULL
			CHECK (btrim(display_name) <> ''),

		is_active BOOLEAN NOT NULL DEFAULT TRUE,

		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
	);


	-- Merchant Platform Credit Eligible Fee Types
	CREATE TABLE IF NOT EXISTS merchant_platform_credit_eligible_fee_types (
		credit_account_id UUID NOT NULL
			REFERENCES merchant_platform_credit_accounts(id)
			ON DELETE CASCADE,

		fee_type_id UUID NOT NULL
			REFERENCES merchant_fee_types(id)
			ON DELETE RESTRICT,

		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

		PRIMARY KEY (credit_account_id, fee_type_id)
	);


	-- ===============================================================
	-- Merchant Trust / Verification
	-- ===============================================================

	CREATE TABLE IF NOT EXISTS merchant_trust_profiles (
		merchant_id UUID PRIMARY KEY REFERENCES merchants(id) ON DELETE CASCADE,
		trust_tier TEXT NOT NULL DEFAULT 'pending'
			CHECK (trust_tier IN ('pending', 'verified', 'trusted', 'fast_track', 'restricted', 'suspended')),
		risk_level TEXT NOT NULL DEFAULT 'normal'
			CHECK (risk_level IN ('low', 'normal', 'elevated', 'high')),
		last_reviewed_at TIMESTAMPTZ,
		reviewed_by UUID REFERENCES users(id) ON DELETE SET NULL,
		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
	);

	CREATE TABLE IF NOT EXISTS merchant_verifications (
		id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
		merchant_id UUID NOT NULL REFERENCES merchants(id) ON DELETE CASCADE,
		verification_type TEXT NOT NULL CHECK (verification_type IN (
			'email',
			'phone',
			'domain',
			'business',
			'identity'
		)),
		status TEXT NOT NULL DEFAULT 'pending'
			CHECK (status IN ('pending', 'verified', 'failed', 'expired', 'revoked')),
		verified_at TIMESTAMPTZ,
		expires_at TIMESTAMPTZ,
		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		UNIQUE (merchant_id, verification_type)
	);

	CREATE TABLE IF NOT EXISTS merchant_trust_events (
		id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
		merchant_id UUID NOT NULL REFERENCES merchants(id) ON DELETE CASCADE,
		event_type TEXT NOT NULL,
		previous_trust_tier TEXT,
		new_trust_tier TEXT,
		note TEXT,
		performed_by UUID REFERENCES users(id) ON DELETE SET NULL,
		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
	);

	CREATE TABLE IF NOT EXISTS merchant_trust_notes (
		id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
		merchant_id UUID NOT NULL REFERENCES merchants(id) ON DELETE CASCADE,
		note TEXT NOT NULL,
		created_by UUID REFERENCES users(id) ON DELETE SET NULL,
		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
	);

	CREATE TABLE IF NOT EXISTS merchant_domains (
		id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
		merchant_id UUID NOT NULL REFERENCES merchants(id) ON DELETE CASCADE,
		domain TEXT NOT NULL,
		verification_status TEXT NOT NULL DEFAULT 'pending'
			CHECK (verification_status IN ('pending', 'verified', 'failed', 'revoked')),
		verified_at TIMESTAMPTZ,
		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		UNIQUE (merchant_id, domain)
	);

	CREATE TABLE IF NOT EXISTS merchant_contacts (
		id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
		merchant_id UUID NOT NULL REFERENCES merchants(id) ON DELETE CASCADE,
		contact_type TEXT NOT NULL CHECK (contact_type IN ('primary', 'billing', 'technical', 'support')),
		name TEXT NOT NULL,
		email CITEXT NOT NULL,
		phone TEXT,
		is_active BOOLEAN NOT NULL DEFAULT TRUE,
		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
	);


	-- Merchant Future Offerings Assets
	-- CE patch 3: merchant_future_offerings_assets — added updated_at column,
	-- fixed index to target this table (not the parent), renamed index suffix
	-- from _launch to _future_offering for clarity.

	CREATE TABLE IF NOT EXISTS merchant_future_offerings_assets (
		id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
		future_offering_id UUID NOT NULL REFERENCES merchant_future_offerings(id) ON DELETE CASCADE,
		asset_type TEXT NOT NULL CHECK (asset_type IN (
			'hero_image',
			'gallery_image',
			'video',
			'document',
			'promo_asset'
		)),
		url TEXT NOT NULL CHECK (url ~* '^https?://'),
		alt_text TEXT,
		display_order INTEGER NOT NULL DEFAULT 0,
		is_primary BOOLEAN NOT NULL DEFAULT FALSE,
		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
	);

	-- CE patch 3: index now correctly targets merchant_future_offerings_assets.
	CREATE INDEX IF NOT EXISTS idx_merchant_future_offerings_assets_future_offering
		ON merchant_future_offerings_assets(future_offering_id, display_order);


	-- =====================================================================
	-- Merchant Future Offering Engagement Action Groups
	-- =====================================================================
	-- Defines the consumer-selection groups through which a merchant organizes
	-- the merchant-controlled Engagement Actions available for a Future Offering.
	--
	-- Every Future Offering that makes merchant Engagement Actions available
	-- uses at least one Engagement Action Group. Each available merchant
	-- Engagement Action belongs to exactly one group.
	--
	-- A merchant may use a single group containing all available Engagement
	-- Actions or multiple groups containing different Engagement Actions.
	--
	-- Each group establishes the maximum number of Engagement Actions that a
	-- consumer may select from that group. A NULL max_selections permits the
	-- consumer to select any number of the available actions in the group.
	--
	-- Group participation is always voluntary. A consumer may bypass any group
	-- without making a selection; max_selections limits participation where the
	-- consumer chooses to participate and does not establish a minimum.
	--
	-- Quantity does not belong to the group. Quantity capability and permitted
	-- quantity boundaries belong to the individual Engagement Action.
	-- =====================================================================

	CREATE TABLE IF NOT EXISTS merchant_future_offering_engagement_action_groups (
		id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

		future_offering_id UUID NOT NULL
			REFERENCES merchant_future_offerings(id)
			ON DELETE CASCADE,

		name TEXT,

		display_order INTEGER NOT NULL DEFAULT 0
			CHECK (display_order >= 0),

		max_selections INTEGER
			CHECK (
				max_selections IS NULL
				OR max_selections >= 1
			),

		is_active BOOLEAN NOT NULL DEFAULT TRUE,

		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

		CONSTRAINT uq_merchant_future_offering_engagement_action_groups_identity
			UNIQUE (
				id,
				future_offering_id
			)
	);

	CREATE INDEX IF NOT EXISTS
		idx_merchant_future_offering_engagement_action_groups_active
	ON merchant_future_offering_engagement_action_groups (
		future_offering_id,
		display_order,
		id
	)
	WHERE is_active = TRUE;


	-- =====================================================================
	-- Engagement Actions
	-- =====================================================================
	-- Defines the Platform-governed catalog of merchant-selectable Engagement
	-- Actions that may be made available for Future Offerings.
	--
	-- Engagement Action identity is persistent and independent of presentation.
	-- The stable code provides a machine-readable semantic identifier; the UUID
	-- is the canonical database identity.
	--
	-- Administration may add new Engagement Actions as legitimate business
	-- needs emerge without requiring changes to the Future Offering persistence
	-- model. Existing actions must not be repurposed to represent materially
	-- different consumer intent. A materially different action receives a new
	-- identity.
	--
	-- Merchants select applicable Engagement Actions from the active Platform
	-- catalog for each Future Offering. Merchant selection does not create,
	-- redefine, or govern canonical Engagement Action types.
	--
	-- Watch is Platform-owned and is intentionally absent from this catalog.
	-- =====================================================================

	CREATE TABLE IF NOT EXISTS engagement_actions (
		id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

		code TEXT NOT NULL
			CHECK (
				code = LOWER(code)
				AND code ~ '^[a-z][a-z0-9_]*$'
			),

		name TEXT NOT NULL
			CHECK (BTRIM(name) <> ''),

		description TEXT,

		is_active BOOLEAN NOT NULL DEFAULT TRUE,

		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

		CONSTRAINT ux_engagement_actions_code
			UNIQUE (code)
	);


	-- =====================================================================
	-- Merchant Future Offering Engagement Options
	-- =====================================================================
	-- Defines the merchant-controlled Engagement Actions made available for
	-- a Future Offering.
	--
	-- Watch is Platform-owned and is intentionally absent from this table.
	--
	-- Every available merchant Engagement Action belongs to exactly one
	-- Engagement Action Group for the Future Offering. Each option references
	-- a canonical Platform-governed Engagement Action.
	--
	-- The merchant determines which Engagement Actions are available and
	-- which group contains each action. Group-level selection ceilings are
	-- owned by merchant_future_offering_engagement_action_groups.
	--
	-- Where quantity is meaningful to an Engagement Action, the merchant may
	-- enable quantity and establish the permitted minimum and maximum quantity
	-- that one participant may indicate for that action.
	--
	-- Quantity boundaries govern consumer-indicated anticipated demand only.
	-- They do not represent inventory, allocation, guaranteed future
	-- availability, or a merchant commitment to fulfill the indicated quantity.
	--
	-- Engineering enforces Future Offering ownership, grouping, quantity
	-- configuration, and structural integrity. Merchant-facing configuration
	-- exposes understandable participation choices rather than persistence
	-- machinery.
	-- =====================================================================

	CREATE TABLE IF NOT EXISTS merchant_future_offering_engagement_options (
		id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

		future_offering_id UUID NOT NULL
			REFERENCES merchant_future_offerings(id)
			ON DELETE CASCADE,

		engagement_action_group_id UUID NOT NULL,

		engagement_action_id UUID NOT NULL
			REFERENCES engagement_actions(id)
			ON DELETE RESTRICT,

		quantity_enabled BOOLEAN NOT NULL DEFAULT FALSE,

		min_quantity INTEGER,

		max_quantity INTEGER,

		display_order INTEGER NOT NULL DEFAULT 0
			CHECK (display_order >= 0),

		is_active BOOLEAN NOT NULL DEFAULT TRUE,

		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

		CONSTRAINT ux_merchant_future_offering_engagement_options_action
			UNIQUE (
				future_offering_id,
				engagement_action_id
			),

		CONSTRAINT chk_merchant_future_offering_engagement_options_quantity
			CHECK (
				(
					quantity_enabled = FALSE
					AND min_quantity IS NULL
					AND max_quantity IS NULL
				)
				OR
				(
					quantity_enabled = TRUE
					AND min_quantity IS NOT NULL
					AND min_quantity >= 1
					AND (
						max_quantity IS NULL
						OR max_quantity >= min_quantity
					)
				)
			),

		CONSTRAINT fk_merchant_future_offering_engagement_options_group
			FOREIGN KEY (
				engagement_action_group_id,
				future_offering_id
			)
			REFERENCES merchant_future_offering_engagement_action_groups (
				id,
				future_offering_id
			)
			ON DELETE RESTRICT
	);

	CREATE INDEX IF NOT EXISTS
		idx_merchant_future_offering_engagement_options_active
	ON merchant_future_offering_engagement_options (
		future_offering_id,
		engagement_action_group_id,
		display_order,
		id
	)
	WHERE is_active = TRUE;


	-- Merchant Future Offerings Events
	CREATE TABLE IF NOT EXISTS merchant_future_offerings_events (
		id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

		future_offering_id UUID NOT NULL
			REFERENCES merchant_future_offerings(id)
			ON DELETE CASCADE,

		event_type TEXT NOT NULL
			CHECK (event_type IN (
				'created',
				'submitted_for_activation',
				'sent_to_trust_review',
				'changes_requested',
				'approved',
				'activation_payment_satisfied',
				'activated',
				'published',
				'paused',
				'expired',
				'rejected',
				'unpublished',
				'archived',
				'restored'
			)),

		from_status TEXT,
		to_status TEXT,
		note TEXT,

		performed_by UUID
			REFERENCES users(id)
			ON DELETE SET NULL,

		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
	);

	CREATE INDEX IF NOT EXISTS idx_merchant_future_offerings_events_future_offering
		ON merchant_future_offerings_events (
			future_offering_id,
			created_at DESC
		);


	-- =====================================================================
	-- User Future Offering Engagements
	-- =====================================================================
	-- Represents the durable anticipation relationship between one consumer
	-- and one Future Offering.
	--
	-- Watch is Platform-owned and is intentionally represented at the FO
	-- relationship level rather than as a merchant Engagement Action.
	--
	-- A relationship may originate through an explicit Watch or through the
	-- consumer's first merchant Engagement Action. In the latter case the
	-- Platform may establish background Watch state without treating that
	-- Watch as an explicit consumer selection.
	-- =====================================================================

	CREATE TABLE IF NOT EXISTS user_future_offering_engagements (
		id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

		user_id UUID NOT NULL
			REFERENCES users(id)
			ON DELETE CASCADE,

		future_offering_id UUID NOT NULL
			REFERENCES merchant_future_offerings(id)
			ON DELETE CASCADE,

		engagement_status TEXT NOT NULL DEFAULT 'active'
			CHECK (engagement_status IN (
				'active',
				'muted',
				'removed'
			)),

		watch_state TEXT NOT NULL
			CHECK (watch_state IN (
				'explicit',
				'background'
			)),

		notification_enabled BOOLEAN NOT NULL DEFAULT TRUE,

		source_surface TEXT NOT NULL DEFAULT 'direct'
			CHECK (source_surface IN (
				'notification',
				'direct'
			)),

		first_engaged_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		last_activity_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		deleted_at TIMESTAMPTZ
	);

	CREATE UNIQUE INDEX IF NOT EXISTS
		ux_user_future_offering_engagements_active_user_fo
	ON user_future_offering_engagements (
		user_id,
		future_offering_id
	)
	WHERE deleted_at IS NULL;

	CREATE INDEX IF NOT EXISTS
		idx_user_future_offering_engagements_user_active
	ON user_future_offering_engagements (
		user_id,
		last_activity_at DESC
	)
	WHERE deleted_at IS NULL;

	CREATE INDEX IF NOT EXISTS
		idx_user_future_offering_engagements_fo_active
	ON user_future_offering_engagements (
		future_offering_id
	)
	WHERE deleted_at IS NULL
	AND engagement_status IN ('active', 'muted');


	-- =====================================================================
	-- User Future Offering Engagement Action Selections
	-- =====================================================================
	-- Represents merchant Engagement Actions currently selected by a consumer
	-- for a Future Offering.
	--
	-- Watch is intentionally absent. It is Platform-owned and represented by
	-- user_future_offering_engagements.
	--
	-- Each selection must belong to the same Future Offering as its owning
	-- consumer engagement.
	--
	-- Where the selected Engagement Action has quantity enabled, quantity
	-- records the consumer's indicated anticipated demand for that action.
	-- Where quantity is disabled, quantity must be absent.
	--
	-- Quantity is not an order, inventory allocation, reservation guarantee,
	-- statement of future availability, or merchant fulfillment commitment.
	--
	-- Selection validity is protected at the database boundary against the
	-- merchant's current Engagement Action configuration, including Future
	-- Offering ownership, active availability, quantity configuration, and
	-- Engagement Action Group selection ceilings. The service layer must
	-- enforce the same rules before persistence as defense in depth.
	-- =====================================================================

	CREATE TABLE IF NOT EXISTS user_future_offering_engagement_action_selections (
		id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

		engagement_id UUID NOT NULL
			REFERENCES user_future_offering_engagements(id)
			ON DELETE CASCADE,

		engagement_option_id UUID NOT NULL
			REFERENCES merchant_future_offering_engagement_options(id)
			ON DELETE RESTRICT,

		quantity INTEGER
			CHECK (
				quantity IS NULL
				OR quantity >= 1
			),

		selected_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

		CONSTRAINT ux_user_future_offering_engagement_action_selection
			UNIQUE (
				engagement_id,
				engagement_option_id
			)
	);

	CREATE INDEX IF NOT EXISTS
		idx_user_future_offering_engagement_action_selections_option
	ON user_future_offering_engagement_action_selections (
		engagement_option_id,
		selected_at DESC
	);

	CREATE OR REPLACE FUNCTION
		public.enforce_user_engagement_action_selection_integrity()
	RETURNS TRIGGER AS $$
	DECLARE
		v_engagement_future_offering_id UUID;
		v_option_future_offering_id UUID;
		v_group_id UUID;
		v_quantity_enabled BOOLEAN;
		v_min_quantity INTEGER;
		v_max_quantity INTEGER;
		v_option_is_active BOOLEAN;
		v_group_is_active BOOLEAN;
		v_group_max_selections INTEGER;
		v_group_selection_count INTEGER;
	BEGIN
		/*
		* Lock the owning consumer engagement so concurrent selection changes
		* for the same engagement serialize through one durable row.
		*/
		SELECT future_offering_id
		INTO v_engagement_future_offering_id
		FROM user_future_offering_engagements
		WHERE id = NEW.engagement_id
		FOR UPDATE;

		IF NOT FOUND THEN
			RAISE EXCEPTION
				'consumer engagement % does not exist',
				NEW.engagement_id
				USING ERRCODE = '23503';
		END IF;

		/*
		* Resolve the authoritative merchant Engagement Action configuration.
		*/
		SELECT
			future_offering_id,
			engagement_action_group_id,
			quantity_enabled,
			min_quantity,
			max_quantity,
			is_active
		INTO
			v_option_future_offering_id,
			v_group_id,
			v_quantity_enabled,
			v_min_quantity,
			v_max_quantity,
			v_option_is_active
		FROM merchant_future_offering_engagement_options
		WHERE id = NEW.engagement_option_id;

		IF NOT FOUND THEN
			RAISE EXCEPTION
				'engagement option % does not exist',
				NEW.engagement_option_id
				USING ERRCODE = '23503';
		END IF;

		IF v_engagement_future_offering_id
			IS DISTINCT FROM v_option_future_offering_id THEN
			RAISE EXCEPTION
				'consumer engagement and engagement option must belong to the same future offering'
				USING ERRCODE = '23514';
		END IF;

		IF v_option_is_active = FALSE THEN
			RAISE EXCEPTION
				'consumer cannot select an inactive engagement option'
				USING ERRCODE = '23514';
		END IF;

		/*
		* Quantity must agree with the merchant's authoritative configuration.
		*/
		IF v_quantity_enabled = FALSE THEN
			IF NEW.quantity IS NOT NULL THEN
				RAISE EXCEPTION
					'quantity must be absent when quantity is disabled for the engagement option'
					USING ERRCODE = '23514';
			END IF;
		ELSE
			IF NEW.quantity IS NULL THEN
				RAISE EXCEPTION
					'quantity is required when quantity is enabled for the engagement option'
					USING ERRCODE = '23514';
			END IF;

			IF NEW.quantity < v_min_quantity THEN
				RAISE EXCEPTION
					'quantity is below the permitted minimum for the engagement option'
					USING ERRCODE = '23514';
			END IF;

			IF v_max_quantity IS NOT NULL
				AND NEW.quantity > v_max_quantity THEN
				RAISE EXCEPTION
					'quantity exceeds the permitted maximum for the engagement option'
					USING ERRCODE = '23514';
			END IF;
		END IF;

		/*
		* Resolve the owning group and its current selection ceiling.
		*/
		SELECT
			max_selections,
			is_active
		INTO
			v_group_max_selections,
			v_group_is_active
		FROM merchant_future_offering_engagement_action_groups
		WHERE id = v_group_id
			AND future_offering_id = v_option_future_offering_id;

		IF NOT FOUND THEN
			RAISE EXCEPTION
				'engagement option does not have a valid engagement action group'
				USING ERRCODE = '23514';
		END IF;

		IF v_group_is_active = FALSE THEN
			RAISE EXCEPTION
				'consumer cannot select an engagement action from an inactive group'
				USING ERRCODE = '23514';
		END IF;

		/*
		* NULL means that every available action in the group may be selected.
		* Otherwise enforce the merchant-configured ceiling.
		*/
		IF v_group_max_selections IS NOT NULL THEN
			SELECT COUNT(*)
			INTO v_group_selection_count
			FROM user_future_offering_engagement_action_selections AS selection
			JOIN merchant_future_offering_engagement_options AS option
				ON option.id = selection.engagement_option_id
			WHERE selection.engagement_id = NEW.engagement_id
				AND option.engagement_action_group_id = v_group_id
				AND (
					TG_OP <> 'UPDATE'
					OR selection.id <> OLD.id
				);

			IF v_group_selection_count >= v_group_max_selections THEN
				RAISE EXCEPTION
					'engagement action group selection maximum has been reached'
					USING ERRCODE = '23514';
			END IF;
		END IF;

		RETURN NEW;
	END;
	$$ LANGUAGE plpgsql;

	DROP TRIGGER IF EXISTS
		enforce_user_engagement_action_selection_ownership
		ON user_future_offering_engagement_action_selections;

	DROP TRIGGER IF EXISTS
		enforce_user_engagement_action_selection_integrity
		ON user_future_offering_engagement_action_selections;

	CREATE TRIGGER
		enforce_user_engagement_action_selection_integrity
	BEFORE INSERT OR UPDATE OF
		engagement_id,
		engagement_option_id,
		quantity
	ON user_future_offering_engagement_action_selections
	FOR EACH ROW
	EXECUTE FUNCTION
		public.enforce_user_engagement_action_selection_integrity();

		

	-- User Future Offering Engagement Events
	CREATE TABLE IF NOT EXISTS user_future_offering_engagement_events (
		id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

		engagement_id UUID NOT NULL
			REFERENCES user_future_offering_engagements(id)
			ON DELETE CASCADE,

		event_type TEXT NOT NULL
			CHECK (event_type IN (
				'watched',
				'muted',
				'unmuted',
				'unwatched',
				'notification_enabled',
				'notification_disabled',

				'waitlisted',
				'early_access_requested',
				'beta_joined',
				'reservation_interest_recorded',
				'preorder_intent_recorded',

				'engagement_removed',
				'expired'
			)),

		source_surface TEXT
			CHECK (
				source_surface IS NULL
				OR source_surface IN (
					'notification',
					'direct'
				)
			),

		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
	);

	CREATE INDEX IF NOT EXISTS idx_user_future_offering_engagement_events_engagement
		ON user_future_offering_engagement_events (
			engagement_id,
			created_at DESC
		);


	-- =====================================================================
	-- User Future Offering Engagement Submissions
	-- =====================================================================
	-- Represents a consumer's deliberate submission of engagement interests
	-- for a Future Offering.
	--
	-- A consumer engagement may have multiple independent submissions.
	-- Each submission has its own lifecycle and may independently become
	-- eligible for cancellation or QR tokenization.
	--
	-- Submission contents are stored separately as immutable submission
	-- items. QR issuance may tokenize one or more qualifying submissions.
	-- Successful tokenization completes only the submissions actually
	-- tokenized; it does not complete the consumer's overall FO engagement.
	-- =====================================================================

	CREATE TABLE IF NOT EXISTS user_future_offering_engagement_submissions (
		id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

		engagement_id UUID NOT NULL
			REFERENCES user_future_offering_engagements(id)
			ON DELETE CASCADE,

		status TEXT NOT NULL DEFAULT 'submitted'
			CHECK (
				status IN (
					'submitted',
					'cancelled',
					'completed'
				)
			),

		submitted_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

		cancelled_at TIMESTAMPTZ,

		completed_at TIMESTAMPTZ,

		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

		CONSTRAINT chk_user_future_offering_engagement_submissions_lifecycle
			CHECK (
				(
					status = 'submitted'
					AND cancelled_at IS NULL
					AND completed_at IS NULL
				)
				OR
				(
					status = 'cancelled'
					AND cancelled_at IS NOT NULL
					AND completed_at IS NULL
				)
				OR
				(
					status = 'completed'
					AND cancelled_at IS NULL
					AND completed_at IS NOT NULL
				)
			),

		CONSTRAINT chk_user_future_offering_engagement_submissions_cancelled_time
			CHECK (
				cancelled_at IS NULL
					OR cancelled_at >= submitted_at
			),

		CONSTRAINT chk_user_future_offering_engagement_submissions_completed_time
			CHECK (
				completed_at IS NULL
					OR completed_at >= submitted_at
			)
	);

	CREATE INDEX IF NOT EXISTS
		idx_user_future_offering_engagement_submissions_engagement
	ON user_future_offering_engagement_submissions (
		engagement_id,
		submitted_at DESC,
		id
	);

	CREATE INDEX IF NOT EXISTS
		idx_user_future_offering_engagement_submissions_pending
	ON user_future_offering_engagement_submissions (
		engagement_id,
		submitted_at,
		id
	)
	WHERE status = 'submitted';


	-- =====================================================================
	-- User Future Offering Engagement Submission Items
	-- =====================================================================
	-- Stores the immutable merchant Engagement Actions and applicable
	-- quantities captured by a consumer submission.
	--
	-- Each row represents one Engagement Action contained in one submission.
	-- A submission may contain multiple Engagement Actions, while the same
	-- Engagement Action may legitimately appear in separate submissions.
	--
	-- These rows are authoritative submitted participation state. They are
	-- not reconstructed from current selections or engagement event history.
	-- =====================================================================

	CREATE TABLE IF NOT EXISTS user_future_offering_engagement_submission_items (
		id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

		submission_id UUID NOT NULL
			REFERENCES user_future_offering_engagement_submissions(id)
			ON DELETE CASCADE,

		engagement_option_id UUID NOT NULL
			REFERENCES merchant_future_offering_engagement_options(id)
			ON DELETE RESTRICT,

		quantity INTEGER
			CHECK (
				quantity IS NULL
					OR quantity >= 1
			),

		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

		CONSTRAINT uq_user_future_offering_engagement_submission_items_option
			UNIQUE (
				submission_id,
				engagement_option_id
			)
	);

	CREATE INDEX IF NOT EXISTS
		idx_user_future_offering_engagement_submission_items_submission
	ON user_future_offering_engagement_submission_items (
		submission_id,
		id
	);

	CREATE INDEX IF NOT EXISTS
		idx_user_future_offering_engagement_submission_items_option
	ON user_future_offering_engagement_submission_items (
		engagement_option_id,
		submission_id
	);


	-- Merchant Fee Waivers
	CREATE TABLE IF NOT EXISTS merchant_fee_waivers (
		id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

		merchant_id UUID NOT NULL
			REFERENCES merchants(id)
			ON DELETE CASCADE,

		fee_type_id UUID NOT NULL
			REFERENCES merchant_fee_types(id)
			ON DELETE RESTRICT,

		waiver_type TEXT NOT NULL
			CHECK (waiver_type IN (
				'full',
				'percentage',
				'fixed_amount'
			)),

		waiver_value NUMERIC(19,4) NOT NULL DEFAULT 0
			CHECK (
				waiver_value >= 0
				AND (
					waiver_type <> 'percentage'
					OR waiver_value <= 100
				)
			),

		currency CHAR(3) NOT NULL DEFAULT 'USD'
			CHECK (currency ~ '^[A-Z]{3}$'),

		starts_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		ends_at TIMESTAMPTZ,

		is_active BOOLEAN NOT NULL DEFAULT TRUE,

		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		deleted_at TIMESTAMPTZ,

		CONSTRAINT chk_merchant_fee_waivers_effective_window
			CHECK (
				ends_at IS NULL
				OR ends_at > starts_at
			)
	);

	CREATE INDEX IF NOT EXISTS idx_merchant_fee_waivers_merchant_active
		ON merchant_fee_waivers (
			merchant_id,
			fee_type_id,
			starts_at DESC
		)
		WHERE (
			deleted_at IS NULL
			AND is_active = TRUE
		);


	-- ===============================================================
	-- Future Offering Trust Review and Risk
	-- ===============================================================

	CREATE TABLE IF NOT EXISTS future_offering_trust_reviews (
		id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
		future_offering_id UUID NOT NULL REFERENCES merchant_future_offerings(id) ON DELETE CASCADE,

		review_status TEXT NOT NULL DEFAULT 'pending'
			CHECK (review_status IN ('pending', 'in_review', 'approved', 'rejected', 'changes_requested')),

		risk_level TEXT NOT NULL DEFAULT 'normal'
			CHECK (risk_level IN ('low', 'normal', 'elevated', 'high')),

		assigned_to UUID REFERENCES users(id) ON DELETE SET NULL,
		reviewed_by UUID REFERENCES users(id) ON DELETE SET NULL,
		reviewed_at TIMESTAMPTZ,

		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
	);

	CREATE INDEX IF NOT EXISTS idx_future_offering_trust_reviews_future_offering
		ON future_offering_trust_reviews(future_offering_id);

	CREATE TABLE IF NOT EXISTS future_offering_trust_review_decisions (
		id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
		review_id UUID NOT NULL REFERENCES future_offering_trust_reviews(id) ON DELETE CASCADE,
		decision TEXT NOT NULL CHECK (decision IN ('approved', 'rejected', 'changes_requested')),
		reason TEXT,
		decided_by UUID REFERENCES users(id) ON DELETE SET NULL,
		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
	);

	CREATE TABLE IF NOT EXISTS future_offering_risk_flags (
		id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
		future_offering_id UUID NOT NULL REFERENCES merchant_future_offerings(id) ON DELETE CASCADE,
		flag_type TEXT NOT NULL CHECK (btrim(flag_type) <> ''),
		severity TEXT NOT NULL DEFAULT 'medium'
			CHECK (severity IN ('low', 'medium', 'high', 'critical')),
		note TEXT,
		resolved_at TIMESTAMPTZ,
		resolved_by UUID REFERENCES users(id) ON DELETE SET NULL,
		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
	);

	CREATE INDEX IF NOT EXISTS idx_future_offering_risk_flags_future_offering
		ON future_offering_risk_flags(future_offering_id)
		WHERE resolved_at IS NULL;


	-- ===============================================================
	-- Commerce Routes
	-- FO-native consumer discovery and routing infrastructure.
	-- A route belongs directly to a Future Offering and its owning merchant.
	-- Deals, Launch Campaigns, affiliate attribution, and downstream commerce
	-- monetization are outside the Future Offering domain.
	-- ===============================================================
	CREATE TABLE IF NOT EXISTS commerce_routes (
		id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

		future_offering_id UUID NOT NULL
			REFERENCES merchant_future_offerings(id)
			ON DELETE CASCADE,

		merchant_id UUID NOT NULL
			REFERENCES merchants(id)
			ON DELETE CASCADE,

		destination_url TEXT NOT NULL
			CHECK (destination_url ~* '^https?://'),

		route_token_hash TEXT NOT NULL UNIQUE,

		is_active BOOLEAN NOT NULL DEFAULT TRUE,
		expires_at TIMESTAMPTZ,

		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		deleted_at TIMESTAMPTZ
	);

	CREATE INDEX IF NOT EXISTS idx_commerce_routes_future_offering
		ON commerce_routes(future_offering_id)
		WHERE deleted_at IS NULL;

	CREATE INDEX IF NOT EXISTS idx_commerce_routes_merchant
		ON commerce_routes(merchant_id, future_offering_id)
		WHERE deleted_at IS NULL;


	CREATE OR REPLACE FUNCTION public.enforce_commerce_route_future_offering_ownership()
	RETURNS TRIGGER AS $$
	DECLARE
		v_merchant_id UUID;
	BEGIN
		SELECT merchant_id
		INTO v_merchant_id
		FROM public.merchant_future_offerings
		WHERE id = NEW.future_offering_id
		AND deleted_at IS NULL;

		IF NOT FOUND THEN
			RAISE EXCEPTION
				'future offering "%" does not exist or is deleted',
				NEW.future_offering_id;
		END IF;

		IF NEW.merchant_id <> v_merchant_id THEN
			RAISE EXCEPTION
				'commerce route merchant "%" does not match future offering merchant "%"',
				NEW.merchant_id,
				v_merchant_id;
		END IF;

		RETURN NEW;
	END;
	$$ LANGUAGE plpgsql;

	DROP TRIGGER IF EXISTS enforce_commerce_route_future_offering_ownership
		ON public.commerce_routes;

	CREATE TRIGGER enforce_commerce_route_future_offering_ownership
	BEFORE INSERT OR UPDATE OF future_offering_id, merchant_id
	ON public.commerce_routes
	FOR EACH ROW
	EXECUTE FUNCTION public.enforce_commerce_route_future_offering_ownership();


	-- ===============================================================
	-- Commerce Route Events
	-- FO-native route observability infrastructure.
	-- Records traversal of a Commerce Route for a Future Offering.
	-- ===============================================================
	CREATE TABLE IF NOT EXISTS commerce_route_events (
		id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

		route_id UUID NOT NULL
			REFERENCES commerce_routes(id)
			ON DELETE CASCADE,

		future_offering_id UUID NOT NULL
			REFERENCES merchant_future_offerings(id)
			ON DELETE CASCADE,

		user_id UUID
			REFERENCES users(id)
			ON DELETE SET NULL,

		session_id TEXT,

		source_surface TEXT CHECK (
			source_surface IS NULL
			OR source_surface IN (
				'notification',
				'direct'
			)
		),

		ip_hash TEXT,
		user_agent_hash TEXT,

		clicked_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
	);

	CREATE INDEX IF NOT EXISTS idx_commerce_route_events_route_clicked
		ON commerce_route_events(route_id, clicked_at DESC);

	CREATE INDEX IF NOT EXISTS idx_commerce_route_events_future_offering_clicked
		ON commerce_route_events(future_offering_id, clicked_at DESC);


	-- A route event must reference an active Commerce Route and the
	-- event's Future Offering must match the route's Future Offering.
	CREATE OR REPLACE FUNCTION public.enforce_commerce_route_event_integrity()
	RETURNS TRIGGER AS $$
	DECLARE
		v_route_future_offering_id UUID;
	BEGIN
		SELECT future_offering_id
		INTO v_route_future_offering_id
		FROM public.commerce_routes
		WHERE id = NEW.route_id
		AND deleted_at IS NULL
		AND is_active = TRUE;

		IF NOT FOUND THEN
			RAISE EXCEPTION
				'commerce route "%" does not exist, is deleted, or is inactive',
				NEW.route_id;
		END IF;

		IF NEW.future_offering_id <> v_route_future_offering_id THEN
			RAISE EXCEPTION
				'commerce route event future offering "%" does not match route future offering "%"',
				NEW.future_offering_id,
				v_route_future_offering_id;
		END IF;

		RETURN NEW;
	END;
	$$ LANGUAGE plpgsql;

	DROP TRIGGER IF EXISTS enforce_commerce_route_event_integrity
		ON public.commerce_route_events;

	CREATE TRIGGER enforce_commerce_route_event_integrity
	BEFORE INSERT OR UPDATE OF route_id, future_offering_id
	ON public.commerce_route_events
	FOR EACH ROW
	EXECUTE FUNCTION public.enforce_commerce_route_event_integrity();


	-- ===============================================================
	-- Audit
	-- ===============================================================
	CREATE TABLE IF NOT EXISTS entity_types (
		id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
		name TEXT UNIQUE NOT NULL,
		description TEXT NOT NULL,
		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
	);

	CREATE TABLE IF NOT EXISTS actions (
		id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
		name TEXT UNIQUE NOT NULL,
		description TEXT NOT NULL,
		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
	);

	-- Audit log partitioning review: Existing yearly partitioning predates 
	-- production operation and currently lacks a demonstrated v1 scaling 
	-- requirement. Preserve during M01 cleanup; reassess when the 
	-- audit/governance vertical is deliberately reviewed.
	CREATE TABLE IF NOT EXISTS audit_logs (
		id UUID NOT NULL DEFAULT gen_random_uuid(),
		user_id UUID REFERENCES users(id) ON DELETE SET NULL,
		action_id UUID NOT NULL REFERENCES actions(id) ON DELETE RESTRICT,
		entity_type_id UUID NOT NULL REFERENCES entity_types(id) ON DELETE RESTRICT,
		entity_id TEXT NOT NULL,
		occurred_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		CONSTRAINT audit_logs_entity_composite_key UNIQUE (entity_type_id, entity_id, action_id, occurred_at),
		PRIMARY KEY (id, occurred_at)
	) PARTITION BY RANGE (occurred_at);

	CREATE TABLE IF NOT EXISTS audit_logs_y2025 PARTITION OF audit_logs
		FOR VALUES FROM ('2025-01-01 00:00:00+00') TO ('2026-01-01 00:00:00+00');

	CREATE TABLE IF NOT EXISTS audit_logs_y2026 PARTITION OF audit_logs
		FOR VALUES FROM ('2026-01-01 00:00:00+00') TO ('2027-01-01 00:00:00+00');

	CREATE TABLE IF NOT EXISTS audit_logs_y2027 PARTITION OF audit_logs
		FOR VALUES FROM ('2027-01-01 00:00:00+00') TO ('2028-01-01 00:00:00+00');

	CREATE TABLE IF NOT EXISTS audit_logs_default PARTITION OF audit_logs DEFAULT;

	CREATE INDEX IF NOT EXISTS idx_audit_logs_entity ON audit_logs(entity_type_id, entity_id);
	CREATE INDEX IF NOT EXISTS idx_audit_logs_user ON audit_logs(user_id);
	CREATE INDEX IF NOT EXISTS idx_audit_logs_timestamp ON audit_logs(occurred_at DESC, entity_type_id);

	-- ===============================================================
	-- User Notifications
	-- ===============================================================
	CREATE TABLE IF NOT EXISTS user_notifications (
		id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
		user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		future_offering_id UUID
			REFERENCES merchant_future_offerings(id)
			ON DELETE SET NULL,
		notification_type_id UUID NOT NULL REFERENCES notification_types(id) ON DELETE RESTRICT,
		sent_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		deleted_at TIMESTAMPTZ,
		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
	);

	CREATE TABLE IF NOT EXISTS user_notification_channels (
		id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
		user_notification_id UUID NOT NULL REFERENCES user_notifications(id) ON DELETE CASCADE,
		channel_id UUID NOT NULL REFERENCES notification_channels(id) ON DELETE RESTRICT,
		UNIQUE (user_notification_id, channel_id)
	);

	CREATE INDEX IF NOT EXISTS idx_user_notifications_user_id
		ON user_notifications(user_id)
		WHERE deleted_at IS NULL;

	CREATE INDEX IF NOT EXISTS idx_user_notifications_future_offering_id
		ON user_notifications(future_offering_id)
		WHERE deleted_at IS NULL AND future_offering_id IS NOT NULL;

	CREATE INDEX IF NOT EXISTS idx_user_notifications_type_id
		ON user_notifications(notification_type_id)
		WHERE deleted_at IS NULL;


	-- ===============================================================
	-- Auth
	-- ===============================================================
	CREATE TABLE IF NOT EXISTS password_resets (
		id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
		user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		token_hash TEXT NOT NULL,
		expires_at TIMESTAMPTZ NOT NULL,
		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		CONSTRAINT ux_password_resets_user_id UNIQUE (user_id)
	);

	CREATE UNIQUE INDEX IF NOT EXISTS ux_password_resets_token_hash
		ON password_resets (token_hash);

	CREATE INDEX IF NOT EXISTS idx_password_resets_expires_at
		ON password_resets (expires_at);


	-- ===============================================================
	-- Merchant Accounts
	-- ===============================================================
	CREATE TABLE IF NOT EXISTS merchant_accounts (
		id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
		merchant_id UUID NOT NULL
			REFERENCES merchants(id) ON DELETE CASCADE,

		-- capability and is not represented through this column.
		principal_user_id UUID NOT NULL
			REFERENCES users(id) ON DELETE RESTRICT,

		account_status TEXT NOT NULL DEFAULT 'pending'
			CHECK (account_status IN ('pending', 'active', 'suspended', 'closed')),

		onboarded_at TIMESTAMPTZ,

		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		deleted_at TIMESTAMPTZ,

		CONSTRAINT ux_merchant_accounts_merchant_id UNIQUE (merchant_id),
		CONSTRAINT ux_merchant_accounts_principal_user_id UNIQUE (principal_user_id)
	);

	CREATE INDEX IF NOT EXISTS idx_merchant_accounts_principal_active
		ON merchant_accounts(principal_user_id, created_at, id)
		WHERE deleted_at IS NULL AND account_status = 'active';


	-- ===============================================================
	-- Merchant Program Fee Schedules
	-- ===============================================================

	-- Merchant Program Fee Schedules
	CREATE TABLE IF NOT EXISTS merchant_program_fee_schedules (
		id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

		fee_type_id UUID NOT NULL
			REFERENCES merchant_fee_types(id)
			ON DELETE RESTRICT,

		billing_interval TEXT NOT NULL
			CHECK (billing_interval IN (
				'one_time',
				'monthly',
				'annual',
				'event'
			)),

		calculation_method TEXT NOT NULL
			CHECK (calculation_method IN (
				'flat',
				'percentage',
				'hybrid',
				'negotiated'
			)),

		flat_amount NUMERIC(19,4)
			CHECK (
				flat_amount IS NULL
				OR flat_amount >= 0
			),

		percentage_rate NUMERIC(9,6)
			CHECK (
				percentage_rate IS NULL
				OR percentage_rate >= 0
			),

		minimum_fee NUMERIC(19,4)
			CHECK (
				minimum_fee IS NULL
				OR minimum_fee >= 0
			),

		maximum_fee NUMERIC(19,4)
			CHECK (
				maximum_fee IS NULL
				OR maximum_fee >= 0
			),

		currency CHAR(3) NOT NULL DEFAULT 'USD'
			CHECK (currency ~ '^[A-Z]{3}$'),

		is_active BOOLEAN NOT NULL DEFAULT TRUE,

		effective_from TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		effective_to TIMESTAMPTZ,

		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		deleted_at TIMESTAMPTZ,

		CONSTRAINT chk_fee_schedule_effective_window
			CHECK (
				effective_to IS NULL
				OR effective_to > effective_from
			),

		CONSTRAINT chk_fee_schedule_min_max
			CHECK (
				maximum_fee IS NULL
				OR minimum_fee IS NULL
				OR maximum_fee >= minimum_fee
			),

		CONSTRAINT chk_fee_schedule_flat_requires_amount
			CHECK (
				calculation_method <> 'flat'
				OR flat_amount IS NOT NULL
			),

		CONSTRAINT chk_fee_schedule_percentage_requires_rate
			CHECK (
				calculation_method <> 'percentage'
				OR percentage_rate IS NOT NULL
			),

		CONSTRAINT chk_fee_schedule_hybrid_requires_both
			CHECK (
				calculation_method <> 'hybrid'
				OR (
					flat_amount IS NOT NULL
					AND percentage_rate IS NOT NULL
				)
			),

		CONSTRAINT excl_merchant_program_fee_schedules_active_window
			EXCLUDE USING gist (
				fee_type_id WITH =,

				billing_interval WITH =,

				(
					tstzrange(
						effective_from,
						effective_to,
						'[)'
					)
				) WITH &&
			)
			WHERE (
				is_active = TRUE
				AND deleted_at IS NULL
			)
	);

	CREATE INDEX IF NOT EXISTS idx_merchant_program_fee_schedules_lookup
		ON merchant_program_fee_schedules (
			fee_type_id,
			billing_interval,
			effective_from DESC
		)
		WHERE deleted_at IS NULL;

	CREATE INDEX IF NOT EXISTS idx_merchant_program_fee_schedules_effective
		ON merchant_program_fee_schedules (
			fee_type_id,
			billing_interval,
			effective_from,
			effective_to
		)
		WHERE (
			is_active = TRUE
			AND deleted_at IS NULL
		);


	-- ===============================================================
	-- Merchant Billing Account / Prepaid Balance
	-- ===============================================================
	
	-- Merchant Billing Account
	CREATE TABLE IF NOT EXISTS merchant_billing_accounts (
		merchant_id UUID PRIMARY KEY
			REFERENCES merchants(id) ON DELETE RESTRICT,

		status TEXT NOT NULL
			CHECK (status IN ('active', 'suspended', 'closed')),

		currency CHAR(3) NOT NULL
			CHECK (currency ~ '^[A-Z]{3}$'),

		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
	);

	CREATE INDEX IF NOT EXISTS idx_merchant_billing_accounts_status_created
		ON merchant_billing_accounts (
			status,
			created_at DESC,
			merchant_id DESC
		);


	-- Balance is derived from this ledger, not manually trusted.
	CREATE TABLE IF NOT EXISTS merchant_billing_ledger_entries (
		id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

		merchant_id UUID NOT NULL
			REFERENCES merchants(id)
			ON DELETE CASCADE,

		entry_type TEXT NOT NULL
			CHECK (entry_type IN (
				'deposit_credit',
				'platform_credit_grant',
				'platform_credit_application',
				'fee_debit',
				'refund_credit',
				'adjustment_credit',
				'adjustment_debit',
				'reversal_credit',
				'reversal_debit'
			)),

		fee_type_id UUID
			REFERENCES merchant_fee_types(id)
			ON DELETE RESTRICT,

		amount NUMERIC(19,4) NOT NULL
			CHECK (amount > 0),

		currency CHAR(3) NOT NULL DEFAULT 'USD'
			CHECK (currency ~ '^[A-Z]{3}$'),

		reference_type TEXT,
		reference_id UUID,

		note TEXT,

		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

		CONSTRAINT chk_merchant_billing_ledger_fee_type
			CHECK (
				(
					entry_type = 'fee_debit'
					AND fee_type_id IS NOT NULL
				)
				OR
				(
					entry_type <> 'fee_debit'
					AND fee_type_id IS NULL
				)
			)
	);


	-- ===============================================================
	-- Billable Events / Fee Calculations
	-- ===============================================================

	CREATE TABLE IF NOT EXISTS merchant_billable_events (
		id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

		merchant_id UUID NOT NULL
			CONSTRAINT fk_merchant_billable_events_merchant
			REFERENCES merchants(id)
			ON DELETE RESTRICT,

		future_offering_id UUID NOT NULL
			CONSTRAINT fk_merchant_billable_events_future_offering
			REFERENCES merchant_future_offerings(id)
			ON DELETE RESTRICT,

		future_offering_event_id UUID
			CONSTRAINT fk_merchant_billable_events_future_offering_event
			REFERENCES merchant_future_offerings_events(id)
			ON DELETE RESTRICT,

		billing_period_id UUID
			CONSTRAINT fk_merchant_billable_events_billing_period
			REFERENCES merchant_future_offering_billing_periods(id)
			ON DELETE RESTRICT,

		engagement_event_id UUID
			CONSTRAINT fk_merchant_billable_events_engagement_event
			REFERENCES user_future_offering_engagement_events(id)
			ON DELETE RESTRICT,

		billable_event_type TEXT NOT NULL
			CONSTRAINT chk_merchant_billable_events_type
			CHECK (
				billable_event_type IN (
					'anticipation_intelligence_activation',
					'platform_service',
					'watch',
					'waitlist',
					'early_access_request',
					'beta',
					'reservation_interest',
					'preorder_intent'
				)
			),

		gross_event_value NUMERIC(19,4)
			CONSTRAINT chk_merchant_billable_events_gross_event_value
			CHECK (
				gross_event_value IS NULL
					OR gross_event_value >= 0
			),

		currency CHAR(3)
			CONSTRAINT chk_merchant_billable_events_currency
			CHECK (
				currency IS NULL
					OR currency ~ '^[A-Z]{3}$'
			),

		occurred_at TIMESTAMPTZ NOT NULL,

		confirmed_at TIMESTAMPTZ,
		rejected_at TIMESTAMPTZ,
		reversed_at TIMESTAMPTZ,

		status TEXT NOT NULL DEFAULT 'pending'
			CONSTRAINT chk_merchant_billable_events_status
			CHECK (
				status IN (
					'pending',
					'confirmed',
					'rejected',
					'reversed'
				)
			),

		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

		CONSTRAINT chk_merchant_billable_events_source_count
			CHECK (
				num_nonnulls(
					future_offering_event_id,
					billing_period_id,
					engagement_event_id
				) = 1
			),

		CONSTRAINT chk_merchant_billable_events_source_type
			CHECK (
				(
					billable_event_type = 'anticipation_intelligence_activation'
					AND future_offering_event_id IS NOT NULL
					AND billing_period_id IS NULL
					AND engagement_event_id IS NULL
				)
				OR
				(
					billable_event_type = 'platform_service'
					AND future_offering_event_id IS NULL
					AND billing_period_id IS NOT NULL
					AND engagement_event_id IS NULL
				)
				OR
				(
					billable_event_type IN (
						'watch',
						'waitlist',
						'early_access_request',
						'beta',
						'reservation_interest',
						'preorder_intent'
					)
					AND future_offering_event_id IS NULL
					AND billing_period_id IS NULL
					AND engagement_event_id IS NOT NULL
				)
			),

		CONSTRAINT chk_merchant_billable_events_value_currency
			CHECK (
				(
					gross_event_value IS NULL
					AND currency IS NULL
				)
				OR
				(
					gross_event_value IS NOT NULL
					AND currency IS NOT NULL
				)
			),

		CONSTRAINT chk_merchant_billable_events_status_timestamps
			CHECK (
				(
					status = 'pending'
					AND confirmed_at IS NULL
					AND rejected_at IS NULL
					AND reversed_at IS NULL
				)
				OR
				(
					status = 'confirmed'
					AND confirmed_at IS NOT NULL
					AND rejected_at IS NULL
					AND reversed_at IS NULL
				)
				OR
				(
					status = 'rejected'
					AND confirmed_at IS NULL
					AND rejected_at IS NOT NULL
					AND reversed_at IS NULL
				)
				OR
				(
					status = 'reversed'
					AND confirmed_at IS NOT NULL
					AND rejected_at IS NULL
					AND reversed_at IS NOT NULL
				)
			)
	);

	CREATE UNIQUE INDEX IF NOT EXISTS
		uq_merchant_billable_events_future_offering_event
	ON merchant_billable_events (
		future_offering_event_id
	)
	WHERE future_offering_event_id IS NOT NULL;

	CREATE UNIQUE INDEX IF NOT EXISTS
		uq_merchant_billable_events_billing_period
	ON merchant_billable_events (
		billing_period_id
	)
	WHERE billing_period_id IS NOT NULL;

	CREATE UNIQUE INDEX IF NOT EXISTS
		uq_merchant_billable_events_engagement_event
	ON merchant_billable_events (
		engagement_event_id
	)
	WHERE engagement_event_id IS NOT NULL;

	CREATE INDEX IF NOT EXISTS
		idx_merchant_billable_events_merchant_occurred
	ON merchant_billable_events (
		merchant_id,
		occurred_at DESC,
		id DESC
	);

	CREATE INDEX IF NOT EXISTS
		idx_merchant_billable_events_merchant_status_occurred
	ON merchant_billable_events (
		merchant_id,
		status,
		occurred_at DESC,
		id DESC
	);

	CREATE INDEX IF NOT EXISTS
		idx_merchant_billable_events_future_offering_occurred
	ON merchant_billable_events (
		future_offering_id,
		occurred_at DESC,
		id DESC
	);

	CREATE OR REPLACE FUNCTION public.enforce_merchant_billable_event_source_identity()
	RETURNS TRIGGER AS $$
	DECLARE
		v_merchant_id UUID;
		v_future_offering_id UUID;
	BEGIN
		IF TG_OP = 'UPDATE' AND (
			NEW.merchant_id IS DISTINCT FROM OLD.merchant_id
			OR NEW.future_offering_id IS DISTINCT FROM OLD.future_offering_id
			OR NEW.future_offering_event_id IS DISTINCT FROM OLD.future_offering_event_id
			OR NEW.billing_period_id IS DISTINCT FROM OLD.billing_period_id
			OR NEW.engagement_event_id IS DISTINCT FROM OLD.engagement_event_id
			OR NEW.billable_event_type IS DISTINCT FROM OLD.billable_event_type
			OR NEW.gross_event_value IS DISTINCT FROM OLD.gross_event_value
			OR NEW.currency IS DISTINCT FROM OLD.currency
			OR NEW.occurred_at IS DISTINCT FROM OLD.occurred_at
		) THEN
			RAISE EXCEPTION
				'merchant billable event commercial source identity is immutable'
				USING
					ERRCODE = '23514',
					CONSTRAINT = 'chk_merchant_billable_events_source_identity';
		END IF;

		IF TG_OP = 'UPDATE' THEN
			RETURN NEW;
		END IF;

		IF NEW.future_offering_event_id IS NOT NULL THEN
			SELECT
				mfo.merchant_id,
				mfoe.future_offering_id
			INTO
				v_merchant_id,
				v_future_offering_id
			FROM merchant_future_offerings_events AS mfoe
			JOIN merchant_future_offerings AS mfo
				ON mfo.id = mfoe.future_offering_id
			WHERE mfoe.id = NEW.future_offering_event_id;

		ELSIF NEW.billing_period_id IS NOT NULL THEN
			SELECT
				mfo.merchant_id,
				mfobp.future_offering_id
			INTO
				v_merchant_id,
				v_future_offering_id
			FROM merchant_future_offering_billing_periods AS mfobp
			JOIN merchant_future_offerings AS mfo
				ON mfo.id = mfobp.future_offering_id
			WHERE mfobp.id = NEW.billing_period_id;

		ELSIF NEW.engagement_event_id IS NOT NULL THEN
			SELECT
				mfo.merchant_id,
				mfo.id
			INTO
				v_merchant_id,
				v_future_offering_id
			FROM user_future_offering_engagement_events AS utee
			JOIN user_future_offering_engagements AS ute
				ON ute.id = utee.engagement_id
			JOIN merchant_future_offerings AS mfo
				ON mfo.future_offering_id = ute.future_offering_id
			WHERE utee.id = NEW.engagement_event_id;

		ELSE
			RAISE EXCEPTION
				'merchant billable event requires exactly one authoritative source'
				USING
					ERRCODE = '23514',
					CONSTRAINT = 'chk_merchant_billable_events_source_count';
		END IF;

		IF v_merchant_id IS NULL OR v_future_offering_id IS NULL THEN
			RAISE EXCEPTION
				'merchant billable event authoritative source could not be resolved'
				USING
					ERRCODE = '23503';
		END IF;

		IF NEW.merchant_id <> v_merchant_id THEN
			RAISE EXCEPTION
				'merchant billable event merchant does not match authoritative source merchant'
				USING
					ERRCODE = '23514',
					CONSTRAINT = 'chk_merchant_billable_events_source_identity';
		END IF;

		NEW.future_offering_id := v_future_offering_id;

		RETURN NEW;
	END;
	$$ LANGUAGE plpgsql;

	DROP TRIGGER IF EXISTS
		enforce_merchant_billable_event_source_identity_trigger
	ON public.merchant_billable_events;

	CREATE TRIGGER
		enforce_merchant_billable_event_source_identity_trigger
	BEFORE INSERT OR UPDATE OF
		merchant_id,
		future_offering_id,
		future_offering_event_id,
		billing_period_id,
		engagement_event_id,
		billable_event_type,
		gross_event_value,
		currency,
		occurred_at
	ON public.merchant_billable_events
	FOR EACH ROW
	EXECUTE FUNCTION public.enforce_merchant_billable_event_source_identity();


	CREATE TABLE IF NOT EXISTS merchant_billable_event_fee_types (
		id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

		billable_event_type TEXT NOT NULL,

		fee_type_id UUID NOT NULL
			CONSTRAINT fk_merchant_billable_event_fee_types_fee_type
			REFERENCES merchant_fee_types(id)
			ON DELETE RESTRICT,

		is_active BOOLEAN NOT NULL DEFAULT TRUE,

		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

		CONSTRAINT uq_merchant_billable_event_fee_types_event_type
			UNIQUE (billable_event_type)
	);


	-- Merchant Fee Calculations
	CREATE TABLE IF NOT EXISTS merchant_fee_calculations (
		id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

		merchant_id UUID NOT NULL
			CONSTRAINT fk_merchant_fee_calculations_merchant
			REFERENCES merchants(id)
			ON DELETE CASCADE,

		billable_event_id UUID NOT NULL
			CONSTRAINT fk_merchant_fee_calculations_billable_event
			REFERENCES merchant_billable_events(id)
			ON DELETE RESTRICT,

		fee_schedule_id UUID
			CONSTRAINT fk_merchant_fee_calculations_fee_schedule
			REFERENCES merchant_program_fee_schedules(id)
			ON DELETE RESTRICT,

		fee_type_id UUID NOT NULL
			CONSTRAINT fk_merchant_fee_calculations_fee_type
			REFERENCES merchant_fee_types(id)
			ON DELETE RESTRICT,

		calculation_method TEXT NOT NULL
			CONSTRAINT chk_merchant_fee_calculations_calculation_method
			CHECK (
				calculation_method IN (
					'flat',
					'percentage',
					'hybrid',
					'negotiated'
				)
			),

		fee_rate NUMERIC(9,6)
			CONSTRAINT chk_merchant_fee_calculations_fee_rate
			CHECK (
				fee_rate IS NULL
				OR fee_rate >= 0
			),

		flat_fee_amount NUMERIC(19,4)
			CONSTRAINT chk_merchant_fee_calculations_flat_fee_amount
			CHECK (
				flat_fee_amount IS NULL
				OR flat_fee_amount >= 0
			),

		calculated_fee_amount NUMERIC(19,4) NOT NULL
			CONSTRAINT chk_merchant_fee_calculations_calculated_fee_amount
			CHECK (calculated_fee_amount >= 0),

		currency CHAR(3) NOT NULL DEFAULT 'USD'
			CONSTRAINT chk_merchant_fee_calculations_currency
			CHECK (currency ~ '^[A-Z]{3}$'),

		calculation_basis JSONB NOT NULL DEFAULT '{}'::jsonb
			CONSTRAINT chk_merchant_fee_calculations_basis_object
			CHECK (jsonb_typeof(calculation_basis) = 'object'),

		calculated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

		status TEXT NOT NULL DEFAULT 'pending'
			CONSTRAINT chk_merchant_fee_calculations_status
			CHECK (
				status IN (
					'pending',
					'approved',
					'settled',
					'waived',
					'reversed'
				)
			),

		approved_at TIMESTAMPTZ,
		settled_at TIMESTAMPTZ,
		waived_at TIMESTAMPTZ,
		reversed_at TIMESTAMPTZ,

		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

		CONSTRAINT chk_merchant_fee_calculations_method_inputs
			CHECK (
				(
					calculation_method = 'flat'
					AND flat_fee_amount IS NOT NULL
					AND fee_rate IS NULL
				)
				OR (
					calculation_method = 'percentage'
					AND fee_rate IS NOT NULL
					AND flat_fee_amount IS NULL
				)
				OR (
					calculation_method = 'hybrid'
					AND fee_rate IS NOT NULL
					AND flat_fee_amount IS NOT NULL
				)
				OR calculation_method = 'negotiated'
			),

		CONSTRAINT chk_merchant_fee_calculations_status_timestamps
			CHECK (
				(
					status = 'pending'
					AND approved_at IS NULL
					AND settled_at IS NULL
					AND waived_at IS NULL
					AND reversed_at IS NULL
				)
				OR (
					status = 'approved'
					AND approved_at IS NOT NULL
					AND settled_at IS NULL
					AND waived_at IS NULL
					AND reversed_at IS NULL
				)
				OR (
					status = 'settled'
					AND approved_at IS NOT NULL
					AND settled_at IS NOT NULL
					AND waived_at IS NULL
					AND reversed_at IS NULL
				)
				OR (
					status = 'waived'
					AND settled_at IS NULL
					AND waived_at IS NOT NULL
					AND reversed_at IS NULL
				)
				OR (
					status = 'reversed'
					AND reversed_at IS NOT NULL
					AND (
						(
							settled_at IS NULL
							AND waived_at IS NULL
						)
						OR (
							approved_at IS NOT NULL
							AND settled_at IS NOT NULL
							AND waived_at IS NULL
						)
						OR (
							settled_at IS NULL
							AND waived_at IS NOT NULL
						)
					)
				)
			),

		CONSTRAINT chk_merchant_fee_calculations_lifecycle_times
			CHECK (
				(approved_at IS NULL OR approved_at >= calculated_at)
				AND (
					settled_at IS NULL
					OR (
						approved_at IS NOT NULL
						AND settled_at >= approved_at
					)
				)
				AND (
					waived_at IS NULL
					OR waived_at >= COALESCE(
						approved_at,
						calculated_at
					)
				)
				AND (
					reversed_at IS NULL
					OR reversed_at >= COALESCE(
						settled_at,
						waived_at,
						approved_at,
						calculated_at
					)
				)
			)
	);

	CREATE UNIQUE INDEX IF NOT EXISTS uq_merchant_fee_calculations_billable_event_fee_type_active
		ON merchant_fee_calculations (
			billable_event_id,
			fee_type_id
		)
		WHERE status <> 'reversed';

	CREATE INDEX IF NOT EXISTS idx_merchant_fee_calculations_merchant_timeline
		ON merchant_fee_calculations (
			merchant_id,
			calculated_at DESC,
			id DESC
		);

	CREATE INDEX IF NOT EXISTS idx_merchant_fee_calculations_merchant_status_timeline
		ON merchant_fee_calculations (
			merchant_id,
			status,
			calculated_at DESC,
			id DESC
		);

	CREATE INDEX IF NOT EXISTS idx_merchant_fee_calculations_billable_event_timeline
		ON merchant_fee_calculations (
			billable_event_id,
			calculated_at DESC,
			id DESC
		);

	CREATE INDEX IF NOT EXISTS idx_merchant_fee_calculations_fee_schedule
		ON merchant_fee_calculations (fee_schedule_id)
		WHERE fee_schedule_id IS NOT NULL;


	-- Merchant Platform Credit Applications
	CREATE TABLE IF NOT EXISTS merchant_platform_credit_applications (
		id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

		credit_account_id UUID NOT NULL
			CONSTRAINT merchant_platform_credit_applications_credit_account_id_fkey
			REFERENCES merchant_platform_credit_accounts(id)
			ON DELETE RESTRICT,

		fee_calculation_id UUID NOT NULL
			CONSTRAINT merchant_platform_credit_applications_fee_calculation_id_fkey
			REFERENCES merchant_fee_calculations(id)
			ON DELETE RESTRICT,

		applied_amount NUMERIC(19,4) NOT NULL
			CONSTRAINT merchant_platform_credit_applications_applied_amount_positive_chk
			CHECK (applied_amount > 0),

		currency CHAR(3) NOT NULL
			CONSTRAINT merchant_platform_credit_applications_currency_format_chk
			CHECK (currency ~ '^[A-Z]{3}$'),

		applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

		CONSTRAINT merchant_platform_credit_applications_account_fee_uniq
			UNIQUE (credit_account_id, fee_calculation_id)
	);

	CREATE INDEX IF NOT EXISTS idx_merchant_platform_credit_applications_credit
		ON merchant_platform_credit_applications (
			credit_account_id,
			applied_at DESC,
			id DESC
		);

	CREATE INDEX IF NOT EXISTS idx_merchant_platform_credit_applications_fee
		ON merchant_platform_credit_applications (
			fee_calculation_id,
			applied_at DESC,
			id DESC
		);




	-- ===============================================================
	-- Billing Indexes
	-- ===============================================================

	CREATE INDEX IF NOT EXISTS idx_merchant_accounts_status
		ON merchant_accounts(account_status)
		WHERE deleted_at IS NULL;

	CREATE INDEX IF NOT EXISTS idx_merchant_billing_ledger_entries_merchant
		ON merchant_billing_ledger_entries(merchant_id, created_at DESC);

	CREATE INDEX IF NOT EXISTS idx_merchant_billing_ledger_entries_reference
		ON merchant_billing_ledger_entries(reference_type, reference_id)
		WHERE reference_type IS NOT NULL AND reference_id IS NOT NULL;

	CREATE INDEX IF NOT EXISTS idx_merchant_billable_events_merchant_status
		ON merchant_billable_events(merchant_id, status, created_at DESC);

	CREATE INDEX IF NOT EXISTS idx_merchant_fee_calculations_merchant_status
		ON merchant_fee_calculations(merchant_id, status, calculated_at DESC);


	-- ===============================================================
	-- Merchant Fee Reversal
	-- Non-v1 fee reversal linkage and audit trail infrastructure.
	-- Keep schema compile-safe, but do not expand routes, services, UI,
	-- handlers, or tests for Future Offering v1.
	-- ===============================================================
	CREATE TABLE IF NOT EXISTS merchant_fee_reversals (
		id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

		merchant_id UUID NOT NULL REFERENCES merchants(id) ON DELETE CASCADE,

		original_fee_calculation_id UUID NOT NULL
			REFERENCES merchant_fee_calculations(id) ON DELETE RESTRICT,

		reversal_fee_calculation_id UUID NOT NULL UNIQUE
			REFERENCES merchant_fee_calculations(id) ON DELETE CASCADE,

		reversal_reason TEXT NOT NULL,

		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

		CONSTRAINT chk_fee_reversal_different_calculations
			CHECK (original_fee_calculation_id <> reversal_fee_calculation_id)
	);

	CREATE INDEX IF NOT EXISTS idx_merchant_fee_reversals_merchant
		ON merchant_fee_reversals(merchant_id, created_at DESC);

	CREATE INDEX IF NOT EXISTS idx_merchant_fee_reversals_original
		ON merchant_fee_reversals(original_fee_calculation_id);

	-- ===============================================================
	-- Merchant Payment Methods
	-- Non-v1 merchant payment instrument storage infrastructure.
	-- Keep schema compile-safe, but do not expand routes, services, UI,
	-- handlers, or tests for Future Offering v1.
	-- ===============================================================

	CREATE TABLE IF NOT EXISTS merchant_payment_methods (
		id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

		merchant_id UUID NOT NULL
			REFERENCES merchants(id)
			ON DELETE CASCADE,

		payment_method_type TEXT NOT NULL
			CHECK (payment_method_type IN (
				'card',
				'bank_account',
				'wire',
				'manual_invoice'
			)),

		display_label TEXT
			CHECK (
				display_label IS NULL
				OR char_length(display_label) <= 255
			),

		last_four TEXT
			CHECK (
				last_four IS NULL
				OR last_four ~ '^[0-9]{4}$'
			),

		status TEXT NOT NULL DEFAULT 'active'
			CHECK (status IN (
				'active',
				'inactive',
				'expired',
				'revoked'
			)),

		is_default BOOLEAN NOT NULL DEFAULT FALSE,

		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		deleted_at TIMESTAMPTZ,

		CONSTRAINT ck_merchant_payment_methods_expired_card_only
			CHECK (
				status <> 'expired'
				OR payment_method_type = 'card'
			),

		CONSTRAINT ck_merchant_payment_methods_default_operational
			CHECK (
				is_default = FALSE
				OR (
					status = 'active'
					AND deleted_at IS NULL
				)
			)
	);

	CREATE UNIQUE INDEX IF NOT EXISTS ux_merchant_payment_methods_default
		ON merchant_payment_methods(merchant_id)
		WHERE is_default = TRUE
		AND deleted_at IS NULL
		AND status = 'active';

	CREATE INDEX IF NOT EXISTS idx_merchant_payment_methods_merchant
		ON merchant_payment_methods(merchant_id)
		WHERE deleted_at IS NULL;

	CREATE INDEX IF NOT EXISTS idx_merchant_payment_methods_merchant_status
		ON merchant_payment_methods(merchant_id, status)
		WHERE deleted_at IS NULL;


	-- ===============================================================
	-- Merchant Invoices / Invoice Items
	-- Non-v1 merchant invoice generation and line item infrastructure.
	-- Keep schema compile-safe, but do not expand routes, services, UI,
	-- handlers, or tests for Future Offering v1.
	-- ===============================================================
	CREATE TABLE IF NOT EXISTS merchant_invoices (
		id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

		merchant_id UUID NOT NULL
			REFERENCES merchants(id) ON DELETE RESTRICT,

		future_offering_id UUID NOT NULL
			REFERENCES merchant_future_offerings(id) ON DELETE RESTRICT,

		invoice_number TEXT NOT NULL UNIQUE,

		invoice_status TEXT NOT NULL DEFAULT 'draft'
			CHECK (invoice_status IN (
				'draft',
				'issued',
				'partially_paid',
				'paid',
				'overdue',
				'void'
			)),

		-- Gross invoice-item aggregate. Reconciled from merchant_invoice_items.
		-- This is not MISA's presentation label "Items Subtotal"; presentation
		-- aggregates may be derived from the normalized composition domains.
		subtotal_amount NUMERIC(19,4) NOT NULL DEFAULT 0
			CHECK (subtotal_amount >= 0),

		-- Final merchant obligation after all authoritative pre-settlement monetary
		-- effects included in invoice composition. No generic adjustment scalar.
		total_amount NUMERIC(19,4) NOT NULL DEFAULT 0
			CHECK (total_amount >= 0),

		amount_paid NUMERIC(19,4) NOT NULL DEFAULT 0
			CHECK (amount_paid >= 0),

		currency CHAR(3) NOT NULL
			CHECK (currency ~ '^[A-Z]{3}$'),

		issued_at TIMESTAMPTZ,
		due_at TIMESTAMPTZ,
		paid_at TIMESTAMPTZ,
		voided_at TIMESTAMPTZ,

		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

		CONSTRAINT chk_merchant_invoice_number_canonical
			CHECK (invoice_number = btrim(invoice_number) AND invoice_number <> ''),

		CONSTRAINT chk_merchant_invoices_invoice_number_length
			CHECK (char_length(invoice_number) <= 64),

		CONSTRAINT chk_merchant_invoice_amount_paid
			CHECK (amount_paid <= total_amount),

		CONSTRAINT chk_merchant_invoice_draft_state
			CHECK (
				invoice_status <> 'draft'
				OR (
					amount_paid = 0
					AND issued_at IS NULL
					AND due_at IS NULL
					AND paid_at IS NULL
					AND voided_at IS NULL
				)
			),

		CONSTRAINT chk_merchant_invoice_issued_state
			CHECK (
				invoice_status <> 'issued'
				OR (
					amount_paid = 0
					AND issued_at IS NOT NULL
					AND paid_at IS NULL
					AND voided_at IS NULL
				)
			),

		CONSTRAINT chk_merchant_invoice_partially_paid_state
			CHECK (
				invoice_status <> 'partially_paid'
				OR (
					amount_paid > 0
					AND amount_paid < total_amount
					AND issued_at IS NOT NULL
					AND paid_at IS NULL
					AND voided_at IS NULL
				)
			),

		CONSTRAINT chk_merchant_invoice_paid_state
			CHECK (
				invoice_status <> 'paid'
				OR (
					amount_paid = total_amount
					AND issued_at IS NOT NULL
					AND paid_at IS NOT NULL
					AND voided_at IS NULL
				)
			),

		CONSTRAINT chk_merchant_invoice_overdue_state
			CHECK (
				invoice_status <> 'overdue'
				OR (
					amount_paid < total_amount
					AND issued_at IS NOT NULL
					AND due_at IS NOT NULL
					AND paid_at IS NULL
					AND voided_at IS NULL
				)
			),

		CONSTRAINT chk_merchant_invoice_void_state
			CHECK (
				invoice_status <> 'void'
				OR (
					amount_paid = 0
					AND issued_at IS NOT NULL
					AND paid_at IS NULL
					AND voided_at IS NOT NULL
				)
			),

		CONSTRAINT chk_merchant_invoice_due_after_issue
			CHECK (due_at IS NULL OR (issued_at IS NOT NULL AND due_at >= issued_at)),

		CONSTRAINT chk_merchant_invoice_paid_after_issue
			CHECK (paid_at IS NULL OR (issued_at IS NOT NULL AND paid_at >= issued_at)),

		CONSTRAINT chk_merchant_invoice_voided_after_issue
			CHECK (voided_at IS NULL OR (issued_at IS NOT NULL AND voided_at >= issued_at))
	);

	CREATE INDEX IF NOT EXISTS idx_merchant_invoices_merchant_created
		ON merchant_invoices(merchant_id, created_at DESC, id DESC);

	CREATE INDEX IF NOT EXISTS idx_merchant_invoices_merchant_future_offering_created
		ON merchant_invoices(merchant_id, future_offering_id, created_at DESC, id DESC);

	CREATE INDEX IF NOT EXISTS idx_merchant_invoices_merchant_status
		ON merchant_invoices(merchant_id, invoice_status, created_at DESC, id DESC);

	CREATE INDEX IF NOT EXISTS idx_merchant_invoices_due
		ON merchant_invoices(due_at, id)
		WHERE invoice_status = 'issued' AND due_at IS NOT NULL;


	-- Merchant Invoice Items
	CREATE TABLE IF NOT EXISTS merchant_invoice_items (
		id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

		invoice_id UUID NOT NULL
			CONSTRAINT fk_merchant_invoice_items_invoice
			REFERENCES merchant_invoices(id)
			ON DELETE RESTRICT,

		fee_calculation_id UUID NOT NULL
			CONSTRAINT fk_merchant_invoice_items_fee_calculation
			REFERENCES merchant_fee_calculations(id)
			ON DELETE RESTRICT,

		description TEXT NOT NULL,

		quantity NUMERIC(19,4) NOT NULL
			CONSTRAINT chk_merchant_invoice_items_quantity
			CHECK (quantity > 0),

		unit_amount NUMERIC(19,4) NOT NULL
			CONSTRAINT chk_merchant_invoice_items_unit_amount
			CHECK (unit_amount >= 0),

		line_amount NUMERIC(19,4) NOT NULL
			CHECK (line_amount >= 0),

		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

		CONSTRAINT chk_merchant_invoice_items_description_length
			CHECK (char_length(description) BETWEEN 1 AND 500),

		CONSTRAINT chk_merchant_invoice_items_description_trimmed
			CHECK (description = btrim(description)),

		CONSTRAINT chk_merchant_invoice_items_line_amount
			CHECK (line_amount = quantity * unit_amount),

		CONSTRAINT uq_merchant_invoice_items_fee_calculation
			UNIQUE (fee_calculation_id)
	);

	CREATE INDEX IF NOT EXISTS idx_merchant_invoice_items_invoice
		ON merchant_invoice_items (
			invoice_id,
			created_at ASC,
			id ASC
		);


	-- ===============================================================
	-- Merchant Payments / Receipts
	-- Non-v1 merchant payment transaction and receipt infrastructure.
	-- Keep schema compile-safe, but do not expand routes, services, UI,
	-- handlers, or tests for Future Offering v1.
	-- ===============================================================
	CREATE TABLE IF NOT EXISTS merchant_payments (
		id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

		merchant_id UUID NOT NULL REFERENCES merchants(id) ON DELETE CASCADE,

		invoice_id UUID
			REFERENCES merchant_invoices(id) ON DELETE SET NULL,

		payment_method_id UUID
			REFERENCES merchant_payment_methods(id) ON DELETE SET NULL,

		payment_status TEXT NOT NULL DEFAULT 'pending'
			CHECK (payment_status IN (
				'pending',
				'processing',
				'succeeded',
				'failed',
				'refunded',
				'cancelled'
			)),

		amount NUMERIC(19,4) NOT NULL CHECK (amount > 0),

		currency CHAR(3) NOT NULL DEFAULT 'USD'
			CHECK (currency ~ '^[A-Z]{3}$'),

		processor_name TEXT,
		processor_transaction_id TEXT,

		received_at TIMESTAMPTZ,
		failed_at TIMESTAMPTZ,
		failure_reason TEXT,

		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

		CONSTRAINT chk_merchant_payment_received_at
			CHECK (
				payment_status <> 'succeeded'
				OR received_at IS NOT NULL
			),

		CONSTRAINT chk_merchant_payment_failed_at
			CHECK (
				payment_status <> 'failed'
				OR failed_at IS NOT NULL
			),

		CONSTRAINT chk_merchant_payment_failure_reason
			CHECK (
				payment_status <> 'failed'
				OR failure_reason IS NOT NULL
			),

		CONSTRAINT chk_merchant_payment_processor_transaction_id
			CHECK (
				payment_status <> 'succeeded'
				OR processor_transaction_id IS NOT NULL
			)
	);

	CREATE INDEX IF NOT EXISTS idx_merchant_payments_merchant_status
		ON merchant_payments(merchant_id, payment_status, created_at DESC);

	CREATE INDEX IF NOT EXISTS idx_merchant_payments_invoice
		ON merchant_payments(invoice_id)
		WHERE invoice_id IS NOT NULL;


	-- Outbox Events
	CREATE TABLE IF NOT EXISTS outbox_events (
		id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

		aggregate_type TEXT NOT NULL
			CHECK (btrim(aggregate_type) <> ''),

		aggregate_id UUID NOT NULL,

		event_type TEXT NOT NULL
			CHECK (btrim(event_type) <> ''),

		event_version INTEGER NOT NULL
			CHECK (event_version > 0),

		payload JSONB NOT NULL,

		idempotency_key TEXT NOT NULL
			CHECK (btrim(idempotency_key) <> ''),

		correlation_id UUID,
		causation_id UUID,

		occurred_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

		published_at TIMESTAMPTZ,

		attempt_count INTEGER NOT NULL DEFAULT 0
			CHECK (attempt_count >= 0),

		next_attempt_at TIMESTAMPTZ,

		claimed_at TIMESTAMPTZ,
		claimed_by TEXT,

		last_error TEXT,
		dead_lettered_at TIMESTAMPTZ,

		CONSTRAINT ux_outbox_events_idempotency_key
			UNIQUE (idempotency_key),

		CONSTRAINT chk_outbox_events_published_after_created
			CHECK (
				published_at IS NULL
				OR published_at >= created_at
			),

		CONSTRAINT chk_outbox_events_claim_identity
			CHECK (
				(claimed_at IS NULL AND claimed_by IS NULL)
				OR
				(claimed_at IS NOT NULL
					AND claimed_by IS NOT NULL
					AND btrim(claimed_by) <> '')
			),

		CONSTRAINT chk_outbox_events_dead_letter_after_created
			CHECK (
				dead_lettered_at IS NULL
				OR dead_lettered_at >= created_at
			)
	);

	CREATE INDEX IF NOT EXISTS
		idx_outbox_events_publishable
	ON outbox_events (
		next_attempt_at,
		created_at,
		id
	)
	WHERE
		published_at IS NULL
		AND dead_lettered_at IS NULL;

	CREATE INDEX IF NOT EXISTS
		idx_outbox_events_aggregate_timeline
	ON outbox_events (
		aggregate_type,
		aggregate_id,
		created_at,
		id
	);


	-- ===============================================================
	-- updated_at Triggers
	-- Canonical consolidated set_updated_at trigger registrations.
	-- ===============================================================
	DROP TRIGGER IF EXISTS set_updated_at_roles ON roles;
	CREATE TRIGGER set_updated_at_roles
	BEFORE UPDATE ON roles
	FOR EACH ROW EXECUTE FUNCTION public.set_updated_at();

	DROP TRIGGER IF EXISTS set_updated_at_platform_settings ON platform_settings;
	CREATE TRIGGER set_updated_at_platform_settings
	BEFORE UPDATE ON platform_settings
	FOR EACH ROW EXECUTE FUNCTION public.set_updated_at();

	DROP TRIGGER IF EXISTS set_updated_at_notification_types ON notification_types;
	CREATE TRIGGER set_updated_at_notification_types
	BEFORE UPDATE ON notification_types
	FOR EACH ROW EXECUTE FUNCTION public.set_updated_at();

	DROP TRIGGER IF EXISTS set_updated_at_notification_channels ON notification_channels;
	CREATE TRIGGER set_updated_at_notification_channels
	BEFORE UPDATE ON notification_channels
	FOR EACH ROW EXECUTE FUNCTION public.set_updated_at();

	DROP TRIGGER IF EXISTS set_updated_at_users ON users;
	CREATE TRIGGER set_updated_at_users
	BEFORE UPDATE ON users
	FOR EACH ROW EXECUTE FUNCTION public.set_updated_at();

	DROP TRIGGER IF EXISTS set_updated_at_user_profiles ON user_profiles;
	CREATE TRIGGER set_updated_at_user_profiles
	BEFORE UPDATE ON user_profiles
	FOR EACH ROW EXECUTE FUNCTION public.set_updated_at();

	DROP TRIGGER IF EXISTS set_updated_at_social_platforms ON social_platforms;
	CREATE TRIGGER set_updated_at_social_platforms
	BEFORE UPDATE ON social_platforms
	FOR EACH ROW EXECUTE FUNCTION public.set_updated_at();

	DROP TRIGGER IF EXISTS set_updated_at_user_profile_social_links ON user_profile_social_links;
	CREATE TRIGGER set_updated_at_user_profile_social_links
	BEFORE UPDATE ON user_profile_social_links
	FOR EACH ROW EXECUTE FUNCTION public.set_updated_at();

	DROP TRIGGER IF EXISTS set_updated_at_user_notification_preferences ON user_notification_preferences;
	CREATE TRIGGER set_updated_at_user_notification_preferences
	BEFORE UPDATE ON user_notification_preferences
	FOR EACH ROW EXECUTE FUNCTION public.set_updated_at();

	DROP TRIGGER IF EXISTS set_updated_at_permissions ON permissions;
	CREATE TRIGGER set_updated_at_permissions
	BEFORE UPDATE ON permissions
	FOR EACH ROW EXECUTE FUNCTION public.set_updated_at();

	DROP TRIGGER IF EXISTS set_updated_at_merchants ON merchants;
	CREATE TRIGGER set_updated_at_merchants
	BEFORE UPDATE ON merchants
	FOR EACH ROW EXECUTE FUNCTION public.set_updated_at();

	DROP TRIGGER IF EXISTS set_updated_at_merchant_trust_profiles ON merchant_trust_profiles;
	CREATE TRIGGER set_updated_at_merchant_trust_profiles
	BEFORE UPDATE ON merchant_trust_profiles
	FOR EACH ROW EXECUTE FUNCTION public.set_updated_at();

	DROP TRIGGER IF EXISTS set_updated_at_merchant_verifications ON merchant_verifications;
	CREATE TRIGGER set_updated_at_merchant_verifications
	BEFORE UPDATE ON merchant_verifications
	FOR EACH ROW EXECUTE FUNCTION public.set_updated_at();

	DROP TRIGGER IF EXISTS set_updated_at_merchant_domains ON merchant_domains;
	CREATE TRIGGER set_updated_at_merchant_domains
	BEFORE UPDATE ON merchant_domains
	FOR EACH ROW EXECUTE FUNCTION public.set_updated_at();

	DROP TRIGGER IF EXISTS set_updated_at_merchant_contacts ON merchant_contacts;
	CREATE TRIGGER set_updated_at_merchant_contacts
	BEFORE UPDATE ON merchant_contacts
	FOR EACH ROW EXECUTE FUNCTION public.set_updated_at();

	DROP TRIGGER IF EXISTS set_updated_at_merchant_future_offerings ON merchant_future_offerings;
	CREATE TRIGGER set_updated_at_merchant_future_offerings
	BEFORE UPDATE ON merchant_future_offerings
	FOR EACH ROW EXECUTE FUNCTION public.set_updated_at();

	DROP TRIGGER IF EXISTS set_updated_at_merchant_future_offerings_assets ON merchant_future_offerings_assets;
	CREATE TRIGGER set_updated_at_merchant_future_offerings_assets
	BEFORE UPDATE ON merchant_future_offerings_assets
	FOR EACH ROW EXECUTE FUNCTION public.set_updated_at();

	DROP TRIGGER IF EXISTS set_updated_at_merchant_future_offering_engagement_options ON merchant_future_offering_engagement_options;
	CREATE TRIGGER set_updated_at_merchant_future_offering_engagement_options
	BEFORE UPDATE ON merchant_future_offering_engagement_options
	FOR EACH ROW EXECUTE FUNCTION public.set_updated_at();

	DROP TRIGGER IF EXISTS set_updated_at_user_future_offering_engagements ON user_future_offering_engagements;
	CREATE TRIGGER set_updated_at_user_future_offering_engagements
	BEFORE UPDATE ON user_future_offering_engagements
	FOR EACH ROW EXECUTE FUNCTION public.set_updated_at();

	DROP TRIGGER IF EXISTS set_updated_at_future_offering_trust_reviews ON future_offering_trust_reviews;
	CREATE TRIGGER set_updated_at_future_offering_trust_reviews
	BEFORE UPDATE ON future_offering_trust_reviews
	FOR EACH ROW EXECUTE FUNCTION public.set_updated_at();

	DROP TRIGGER IF EXISTS set_updated_at_commerce_routes ON commerce_routes;
	CREATE TRIGGER set_updated_at_commerce_routes
	BEFORE UPDATE ON commerce_routes
	FOR EACH ROW EXECUTE FUNCTION public.set_updated_at();

	DROP TRIGGER IF EXISTS set_updated_at_departments ON departments;
	CREATE TRIGGER set_updated_at_departments
	BEFORE UPDATE ON departments
	FOR EACH ROW EXECUTE FUNCTION public.set_updated_at();

	DROP TRIGGER IF EXISTS set_updated_at_categories ON categories;
	CREATE TRIGGER set_updated_at_categories
	BEFORE UPDATE ON categories
	FOR EACH ROW EXECUTE FUNCTION public.set_updated_at();

	DROP TRIGGER IF EXISTS set_updated_at_actions ON actions;
	CREATE TRIGGER set_updated_at_actions
	BEFORE UPDATE ON actions
	FOR EACH ROW EXECUTE FUNCTION public.set_updated_at();

	DROP TRIGGER IF EXISTS set_updated_at_activation_tokens ON activation_tokens;
	CREATE TRIGGER set_updated_at_activation_tokens
	BEFORE UPDATE ON activation_tokens
	FOR EACH ROW EXECUTE FUNCTION public.set_updated_at();

	DROP TRIGGER IF EXISTS set_updated_at_user_role_assignments ON user_role_assignments;
	CREATE TRIGGER set_updated_at_user_role_assignments
	BEFORE UPDATE ON user_role_assignments
	FOR EACH ROW EXECUTE FUNCTION public.set_updated_at();

	DROP TRIGGER IF EXISTS set_updated_at_user_notifications ON user_notifications;
	CREATE TRIGGER set_updated_at_user_notifications
	BEFORE UPDATE ON user_notifications
	FOR EACH ROW EXECUTE FUNCTION public.set_updated_at();

	DROP TRIGGER IF EXISTS set_updated_at_merchant_accounts ON merchant_accounts;
	CREATE TRIGGER set_updated_at_merchant_accounts
	BEFORE UPDATE ON merchant_accounts
	FOR EACH ROW EXECUTE FUNCTION public.set_updated_at();

	DROP TRIGGER IF EXISTS set_updated_at_merchant_program_fee_schedules ON merchant_program_fee_schedules;
	CREATE TRIGGER set_updated_at_merchant_program_fee_schedules
	BEFORE UPDATE ON merchant_program_fee_schedules
	FOR EACH ROW EXECUTE FUNCTION public.set_updated_at();

	DROP TRIGGER IF EXISTS set_updated_at_merchant_billing_accounts ON merchant_billing_accounts;
	CREATE TRIGGER set_updated_at_merchant_billing_accounts
	BEFORE UPDATE ON merchant_billing_accounts
	FOR EACH ROW EXECUTE FUNCTION public.set_updated_at();

	DROP TRIGGER IF EXISTS set_updated_at_merchant_platform_credit_accounts ON merchant_platform_credit_accounts;
	CREATE TRIGGER set_updated_at_merchant_platform_credit_accounts
	BEFORE UPDATE ON merchant_platform_credit_accounts
	FOR EACH ROW EXECUTE FUNCTION public.set_updated_at();

	DROP TRIGGER IF EXISTS set_updated_at_merchant_billable_events ON merchant_billable_events;
	CREATE TRIGGER set_updated_at_merchant_billable_events
	BEFORE UPDATE ON merchant_billable_events
	FOR EACH ROW EXECUTE FUNCTION public.set_updated_at();

	DROP TRIGGER IF EXISTS set_updated_at_merchant_fee_calculations ON merchant_fee_calculations;
	CREATE TRIGGER set_updated_at_merchant_fee_calculations
	BEFORE UPDATE ON merchant_fee_calculations
	FOR EACH ROW EXECUTE FUNCTION public.set_updated_at();

	DROP TRIGGER IF EXISTS set_updated_at_merchant_payment_methods ON merchant_payment_methods;
	CREATE TRIGGER set_updated_at_merchant_payment_methods
	BEFORE UPDATE ON merchant_payment_methods
	FOR EACH ROW EXECUTE FUNCTION public.set_updated_at();

	DROP TRIGGER IF EXISTS set_updated_at_merchant_invoices ON merchant_invoices;
	CREATE TRIGGER set_updated_at_merchant_invoices
	BEFORE UPDATE ON merchant_invoices
	FOR EACH ROW EXECUTE FUNCTION public.set_updated_at();

	DROP TRIGGER IF EXISTS set_updated_at_merchant_payments ON merchant_payments;
	CREATE TRIGGER set_updated_at_merchant_payments
	BEFORE UPDATE ON merchant_payments
	FOR EACH ROW EXECUTE FUNCTION public.set_updated_at();

	DROP TRIGGER IF EXISTS set_updated_at_user_external_identities ON user_external_identities;
	CREATE TRIGGER set_updated_at_user_external_identities
	BEFORE UPDATE ON user_external_identities
	FOR EACH ROW EXECUTE FUNCTION public.set_updated_at();
	`)

	if err != nil {
		m.Logger.Error("Failed to execute schema initialization SQL", "error", err)
		return fmt.Errorf("execute schema initialization sql: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		m.Logger.Error("Failed to commit schema initialization transaction", "error", err)
		return fmt.Errorf("commit schema initialization transaction: %w", err)
	}
	committed = true

	m.Logger.Info("Database schema created successfully")
	return nil
}
