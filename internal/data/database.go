// Package data provides models and database access methods for databases and other entities.
//
// sdworkspace/sdbackend/internal/data/database.go
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
//
// DEFERRED Rule:
//
//	DEFERRED schema may remain created and compile-safe, but it must not drive
//	v1 route work, UI expansion, service expansion, test priority, or release
//	blocking unless it breaks shared database initialization.
package data

import (
	"context"
	"fmt"
	"net/url"
	"time"

	"github.com/PiccoloMondoC/sdworkspace/sdbackend/internal/bootstrap"
	"github.com/PiccoloMondoC/sdworkspace/sdbackend/internal/logging"

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
	-- Trend-only editorial metadata enforcement
	-- CE patch 1: ensure_offer_is_trend now filters deleted_at IS NULL
	-- and uses the same "does not exist or is deleted" message pattern
	-- as ensure_offer_type for consistency.
	-- ===============================================================
	CREATE OR REPLACE FUNCTION public.ensure_offer_is_trend(p_offer_id UUID)
	RETURNS VOID AS $$
	DECLARE
		v_type TEXT;
	BEGIN
		SELECT type INTO v_type
		FROM public.offers
		WHERE id = p_offer_id
		  AND deleted_at IS NULL;

		IF NOT FOUND THEN
			RAISE EXCEPTION 'offer "%" does not exist or is deleted', p_offer_id;
		END IF;

		IF v_type <> 'trend' THEN
			RAISE EXCEPTION 'offer "%" is type "%", but only trend offers may carry this editorial metadata', p_offer_id, v_type;
		END IF;
	END;
	$$ LANGUAGE plpgsql;

	CREATE OR REPLACE FUNCTION public.enforce_trend_only_offer_metadata()
	RETURNS TRIGGER AS $$
	BEGIN
		PERFORM public.ensure_offer_is_trend(NEW.offer_id);
		RETURN NEW;
	END;
	$$ LANGUAGE plpgsql;

	-- ===============================================================
	-- Offer type enforcement helpers
	-- ===============================================================
	CREATE OR REPLACE FUNCTION public.ensure_offer_type(
		p_offer_id UUID,
		p_expected_type TEXT
	)
	RETURNS VOID AS $$
	DECLARE
		v_type TEXT;
	BEGIN
		SELECT type INTO v_type
		FROM public.offers
		WHERE id = p_offer_id
		  AND deleted_at IS NULL;

		IF NOT FOUND THEN
			RAISE EXCEPTION 'offer "%" does not exist or is deleted', p_offer_id;
		END IF;

		IF v_type <> p_expected_type THEN
			RAISE EXCEPTION 'offer "%" is type "%", expected "%"', p_offer_id, v_type, p_expected_type;
		END IF;
	END;
	$$ LANGUAGE plpgsql;

	CREATE OR REPLACE FUNCTION public.enforce_deal_offer()
	RETURNS TRIGGER AS $$
	BEGIN
		PERFORM public.ensure_offer_type(NEW.offer_id, 'deal');
		RETURN NEW;
	END;
	$$ LANGUAGE plpgsql;

	CREATE OR REPLACE FUNCTION public.enforce_trend_offer()
	RETURNS TRIGGER AS $$
	BEGIN
		PERFORM public.ensure_offer_type(NEW.offer_id, 'trend');
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
			CHECK (entity_type IN ('user', 'brand', 'merchant')),

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
	-- User wallets
	-- DEFERRED: Non-v1 wallet/rewards/account-balance infrastructure.
	-- Keep schema compile-safe, but do not expand routes, handlers, services,
	-- UI, or tests for Future Offering v1.
	-- ===============================================================
	CREATE TABLE IF NOT EXISTS user_wallets (
		id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
		user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		wallet_type TEXT NOT NULL CHECK (wallet_type IN ('rewards', 'gift_card', 'cash', 'merchant_balance')),
		unit_code TEXT NOT NULL DEFAULT 'SAGR_POINTS',
		balance NUMERIC(19,4) NOT NULL DEFAULT 0 CHECK (balance >= 0),
		lifetime_earned NUMERIC(19,4) NOT NULL DEFAULT 0 CHECK (lifetime_earned >= 0),
		status TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'inactive', 'suspended', 'closed')),
		is_active BOOLEAN NOT NULL DEFAULT TRUE,
		deleted_at TIMESTAMPTZ,
		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		UNIQUE (user_id, wallet_type, unit_code)
	);

	CREATE TABLE IF NOT EXISTS wallet_ledger_entries (
		id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
		wallet_id UUID NOT NULL REFERENCES user_wallets(id) ON DELETE CASCADE,
		entry_type TEXT NOT NULL CHECK (entry_type IN (
			'earn',
			'redeem',
			'expire',
			'reverse',
			'adjustment',
			'hold',
			'release'
		)),
		amount NUMERIC(19,4) NOT NULL CHECK (amount > 0),
		status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN (
			'pending',
			'confirmed',
			'reversed',
			'cancelled'
		)),
		unit_code TEXT NOT NULL DEFAULT 'SAGR_POINTS',
		reference_type TEXT,
		reference_id UUID,
		description TEXT,
		available_at TIMESTAMPTZ,
		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
	);

	CREATE INDEX IF NOT EXISTS idx_wallet_ledger_entries_wallet_id
		ON wallet_ledger_entries(wallet_id, created_at DESC);

	CREATE INDEX IF NOT EXISTS idx_user_wallets_user_type_unit_active
		ON user_wallets(user_id, wallet_type, unit_code)
		WHERE deleted_at IS NULL;

	CREATE INDEX IF NOT EXISTS idx_user_wallets_status_active
		ON user_wallets(status, updated_at DESC)
		WHERE deleted_at IS NULL;

	CREATE INDEX IF NOT EXISTS idx_wallet_ledger_entries_status
		ON wallet_ledger_entries(status, created_at DESC);

	CREATE INDEX IF NOT EXISTS idx_wallet_ledger_entries_reference
		ON wallet_ledger_entries(reference_type, reference_id)
		WHERE reference_type IS NOT NULL AND reference_id IS NOT NULL;

	-- ===============================================================
	-- DEFERRED: User Dashboards / Dashboard Reports / Dashboard Templates
	-- Non-v1 consumer dashboard infrastructure.
	-- Keep schema compile-safe, but do not expand routes, services, UI,
	-- handlers, or tests for Future Offering v1.
	-- ===============================================================
	CREATE TABLE IF NOT EXISTS user_dashboards (
		id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
		user_id UUID UNIQUE NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		user_type TEXT NOT NULL CHECK (user_type IN ('customer', 'merchant')),
		name TEXT NOT NULL DEFAULT '',
		description TEXT,
		layout JSONB NOT NULL DEFAULT '{}'::jsonb,
		widgets JSONB NOT NULL DEFAULT '[]'::jsonb,
		filters JSONB NOT NULL DEFAULT '{}'::jsonb,
		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
	);

	CREATE TABLE IF NOT EXISTS user_dashboard_reports (
		id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
		report_id UUID NOT NULL,
		total_dashboards INTEGER NOT NULL CHECK (total_dashboards >= 0),
		dashboards_with_widgets INTEGER NOT NULL CHECK (dashboards_with_widgets >= 0),
		user_type TEXT NOT NULL DEFAULT 'admin',
		generated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
	);

	CREATE TABLE IF NOT EXISTS dashboard_templates (
		id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
		name TEXT UNIQUE NOT NULL,
		description TEXT,
		layout JSONB NOT NULL DEFAULT '{}'::jsonb,
		widgets JSONB NOT NULL DEFAULT '[]'::jsonb,
		filters JSONB NOT NULL DEFAULT '{}'::jsonb,
		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
	);

	CREATE UNIQUE INDEX IF NOT EXISTS ux_dashboard_templates_name
		ON dashboard_templates(name);

	CREATE TABLE IF NOT EXISTS user_dashboard_templates (
		id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
		user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		dashboard_template_id UUID NOT NULL REFERENCES dashboard_templates(id) ON DELETE CASCADE,
		assigned_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		UNIQUE (user_id, dashboard_template_id)
	);

	-- ===============================================================
	-- DEFERRED: User Settings
	-- Non-v1 user preference and notification channel infrastructure.
	-- Keep schema compile-safe, but do not expand routes, services, UI,
	-- handlers, or tests for Future Offering v1.
	-- ===============================================================
	CREATE TABLE IF NOT EXISTS user_settings (
		user_id UUID PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
		preferred_channel_id UUID NOT NULL REFERENCES notification_channels(id) ON DELETE RESTRICT,
		marketing_notifications BOOLEAN NOT NULL DEFAULT TRUE,
		security_notifications BOOLEAN NOT NULL DEFAULT TRUE,
		privacy_data_sharing BOOLEAN NOT NULL DEFAULT FALSE,
		preferences JSONB NOT NULL DEFAULT '{}'::jsonb,
		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		deleted_at TIMESTAMPTZ
	);

	-- ===============================================================
	-- DEFERRED: Affiliate Programs
	-- Non-v1 present-commerce / affiliate-network infrastructure.
	-- Keep schema compile-safe, but do not expand routes, services, UI,
	-- handlers, or tests for Future Offering v1.
	-- ===============================================================
	CREATE TABLE IF NOT EXISTS affiliate_programs (
		id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
		name CITEXT UNIQUE NOT NULL,
		website TEXT NOT NULL,
		api_endpoint TEXT,
		api_auth_method TEXT NOT NULL DEFAULT 'None'
			CHECK (api_auth_method IN ('APIKey', 'None')),
		api_key_encrypted BYTEA,
		api_key_key_id TEXT,
		api_key_last_rotated_at TIMESTAMPTZ,
		deleted_at TIMESTAMPTZ,
		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		CONSTRAINT chk_affiliate_programs_api_key_storage
			CHECK (
				(api_auth_method = 'APIKey' AND api_key_encrypted IS NOT NULL AND api_key_key_id IS NOT NULL)
				OR
				(api_auth_method = 'None' AND api_key_encrypted IS NULL AND api_key_key_id IS NULL AND api_key_last_rotated_at IS NULL)
			)
	);

	CREATE INDEX IF NOT EXISTS idx_affiliate_programs_active_name
		ON affiliate_programs(name)
		WHERE deleted_at IS NULL;

	CREATE TABLE IF NOT EXISTS merchant_types (
		id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
		name TEXT UNIQUE NOT NULL,
		description TEXT NOT NULL,
		deleted_at TIMESTAMPTZ,
		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
	);

	-- ===============================================================
	-- DEFERRED: Platforms
	-- Non-v1 merchant platform taxonomy infrastructure.
	-- Keep schema compile-safe, but do not expand routes, services, UI,
	-- handlers, or tests for Future Offering v1.
	-- ===============================================================
	CREATE TABLE IF NOT EXISTS platforms (
		id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
		name CITEXT UNIQUE NOT NULL,
		description TEXT,
		website TEXT,
		deleted_at TIMESTAMPTZ,
		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
	);

	-- ===============================================================
	-- Merchants
	-- ===============================================================
	CREATE TABLE IF NOT EXISTS merchants (
		id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
		merchant_type_id UUID NOT NULL REFERENCES merchant_types(id) ON DELETE RESTRICT,
		name CITEXT UNIQUE NOT NULL,
		display_name TEXT NOT NULL,
		slug TEXT UNIQUE NOT NULL,
		logo_url TEXT,
		website TEXT,
		platform_id UUID REFERENCES platforms(id) ON DELETE SET NULL,
		deleted_at TIMESTAMPTZ,
		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
	);

	DROP TRIGGER IF EXISTS normalize_merchants_slug_trigger ON public.merchants;
	CREATE TRIGGER normalize_merchants_slug_trigger
	BEFORE INSERT OR UPDATE OF slug ON public.merchants
	FOR EACH ROW EXECUTE FUNCTION public.normalize_slug();

	CREATE INDEX IF NOT EXISTS idx_merchants_active_slug
		ON merchants(slug)
		WHERE deleted_at IS NULL;

	-- ===============================================================
	-- DEFERRED: Merchant Application Status / Merchant Applications
	-- Non-v1 affiliate program application workflow infrastructure.
	-- Keep schema compile-safe, but do not expand routes, services, UI,
	-- handlers, or tests for Future Offering v1.
	-- ===============================================================
	CREATE TABLE IF NOT EXISTS merchant_application_status (
		id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
		name TEXT UNIQUE NOT NULL,
		description TEXT NOT NULL,
		is_active BOOLEAN NOT NULL DEFAULT TRUE,
		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
	);

	CREATE UNIQUE INDEX IF NOT EXISTS ux_merchant_application_status_name
		ON merchant_application_status(name);

	-- CE patch: seed the 'pending' status row inside CreateTables so that
	-- pending_merchant_app_status_id() is never called on an empty table.
	-- ON CONFLICT makes this idempotent on repeated startup.
	INSERT INTO merchant_application_status (
		name,
		description,
		is_active
	)
	VALUES (
		'pending',
		'Merchant application has been submitted and is awaiting review.',
		TRUE
	)
	ON CONFLICT (name) DO UPDATE
	SET
		description = EXCLUDED.description,
		is_active = TRUE,
		updated_at = NOW();

	CREATE OR REPLACE FUNCTION public.pending_merchant_app_status_id()
	RETURNS UUID AS $$
	DECLARE
		v UUID;
	BEGIN
		SELECT id INTO STRICT v
		FROM public.merchant_application_status
		WHERE name = 'pending'
		  AND is_active = TRUE;
		RETURN v;
	END;
	$$ LANGUAGE plpgsql STABLE;

	CREATE TABLE IF NOT EXISTS merchant_applications (
		id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
		merchant_id UUID NOT NULL REFERENCES merchants(id) ON DELETE RESTRICT,
		affiliate_program_id UUID NOT NULL REFERENCES affiliate_programs(id) ON DELETE RESTRICT,
		status_id UUID NOT NULL DEFAULT public.pending_merchant_app_status_id()
			REFERENCES merchant_application_status(id) ON DELETE RESTRICT,
		deleted_at TIMESTAMPTZ,
		applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		CONSTRAINT chk_merchant_applications_deleted_after_applied
			CHECK (deleted_at IS NULL OR deleted_at >= applied_at)
	);

	CREATE UNIQUE INDEX IF NOT EXISTS ux_merchant_applications_active_merchant_program
		ON merchant_applications(merchant_id, affiliate_program_id)
		WHERE deleted_at IS NULL;

	CREATE INDEX IF NOT EXISTS idx_merchant_applications_active_status_applied_at
		ON merchant_applications(status_id, applied_at DESC)
		WHERE deleted_at IS NULL;

	CREATE INDEX IF NOT EXISTS idx_merchant_applications_merchant_id
		ON merchant_applications(merchant_id);

	CREATE INDEX IF NOT EXISTS idx_merchant_applications_affiliate_program_id
		ON merchant_applications(affiliate_program_id);

	CREATE INDEX IF NOT EXISTS idx_merchant_applications_deleted_at
		ON merchant_applications(deleted_at);

	-- ===============================================================
	-- DEFERRED: Merchant Affiliate Programs
	-- Non-v1 affiliate program membership infrastructure.
	-- Keep schema compile-safe, but do not expand routes, services, UI,
	-- handlers, or tests for Future Offering v1.
	-- ===============================================================
	CREATE TABLE IF NOT EXISTS merchant_affiliate_programs (
		merchant_id UUID NOT NULL REFERENCES merchants(id) ON DELETE CASCADE,
		affiliate_program_id UUID NOT NULL REFERENCES affiliate_programs(id) ON DELETE CASCADE,
		deleted_at TIMESTAMPTZ,
		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		PRIMARY KEY (merchant_id, affiliate_program_id)
	);

	-- ===============================================================
	-- DEFERRED: User Merchant Follows
	-- Non-v1 merchant follow/unfollow consumer relationship infrastructure.
	-- Keep schema compile-safe, but do not expand routes, services, UI,
	-- handlers, or tests for Future Offering v1.
	-- ===============================================================
	CREATE TABLE IF NOT EXISTS user_merchant_follows (
		id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
		user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		merchant_id UUID NOT NULL REFERENCES merchants(id) ON DELETE CASCADE,
		followed_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		unfollowed_at TIMESTAMPTZ,
		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		deleted_at TIMESTAMPTZ,

		CONSTRAINT chk_user_merchant_follows_unfollowed_deleted_pair
			CHECK (
				(deleted_at IS NULL AND unfollowed_at IS NULL)
				OR
				(deleted_at IS NOT NULL AND unfollowed_at IS NOT NULL)
			),

		CONSTRAINT chk_user_merchant_follows_unfollowed_after_followed
			CHECK (
				unfollowed_at IS NULL
				OR unfollowed_at >= followed_at
			)
	);

	CREATE UNIQUE INDEX IF NOT EXISTS ux_user_merchant_follows_active_user_merchant
		ON user_merchant_follows(user_id, merchant_id)
		WHERE deleted_at IS NULL;

	CREATE INDEX IF NOT EXISTS idx_user_merchant_follows_user_active
		ON user_merchant_follows(user_id, followed_at DESC)
		WHERE deleted_at IS NULL;

	CREATE INDEX IF NOT EXISTS idx_user_merchant_follows_merchant_active
		ON user_merchant_follows(merchant_id, followed_at DESC)
		WHERE deleted_at IS NULL;

	CREATE INDEX IF NOT EXISTS idx_user_merchant_follows_unfollowed
		ON user_merchant_follows(unfollowed_at DESC)
		WHERE deleted_at IS NOT NULL;

	-- ===============================================================
	-- Brands
	-- ===============================================================
	CREATE TABLE IF NOT EXISTS brands (
		id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
		name CITEXT UNIQUE NOT NULL,
		brand_handle CITEXT UNIQUE,
		deleted_at TIMESTAMPTZ,
		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
	);

	-- ===============================================================
	-- DEFERRED: Market Segments
	-- Non-v1 product market classification infrastructure.
	-- Keep schema compile-safe, but do not expand routes, services, UI,
	-- handlers, or tests for Future Offering v1.
	-- ===============================================================
	CREATE TABLE IF NOT EXISTS market_segments (
		id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
		name CITEXT NOT NULL UNIQUE,
		description TEXT,
		deleted_at TIMESTAMPTZ,
		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
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

	-- ===============================================================
	-- DEFERRED: Products / Merchant Products
	-- Non-v1 product catalog and merchant product linkage infrastructure.
	-- Keep schema compile-safe, but do not expand routes, services, UI,
	-- handlers, or tests for Future Offering v1.
	-- ===============================================================
	CREATE TABLE IF NOT EXISTS products (
		id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
		name CITEXT NOT NULL,
		brand_id UUID REFERENCES brands(id) ON DELETE SET NULL,
		market_segment_id UUID REFERENCES market_segments(id) ON DELETE SET NULL,
		category_id UUID REFERENCES categories(id) ON DELETE SET NULL,
		upc TEXT UNIQUE,
		sku TEXT,
		description TEXT,
		product_line TEXT,
		is_comparable BOOLEAN NOT NULL DEFAULT FALSE,
		deleted_at TIMESTAMPTZ,
		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
	);

	CREATE INDEX IF NOT EXISTS idx_products_sku
		ON products(sku);

	CREATE TABLE IF NOT EXISTS merchant_products (
		id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
		product_id UUID NOT NULL REFERENCES products(id) ON DELETE CASCADE,
		merchant_id UUID NOT NULL REFERENCES merchants(id) ON DELETE CASCADE,
		merchant_sku TEXT,
		merchant_product_url TEXT,
		merchant_title TEXT,
		is_active BOOLEAN NOT NULL DEFAULT TRUE,
		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		UNIQUE (merchant_id, product_id),
		UNIQUE (merchant_id, merchant_sku)
	);

	-- ===============================================================
	-- DEFERRED: Coupon Statuses / Offer Statuses
	-- Non-v1 present-commerce status vocabulary infrastructure.
	-- Keep schema compile-safe, but do not expand routes, services, UI,
	-- handlers, or tests for Future Offering v1.
	-- ===============================================================
	CREATE TABLE IF NOT EXISTS coupon_statuses (
		id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
		name TEXT UNIQUE NOT NULL,
		description TEXT NOT NULL,
		is_active BOOLEAN NOT NULL DEFAULT TRUE,
		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
	);

	CREATE TABLE IF NOT EXISTS offer_statuses (
		id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
		name TEXT UNIQUE NOT NULL,
		description TEXT NOT NULL,
		is_active BOOLEAN NOT NULL DEFAULT TRUE,
		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
	);

	-- ===============================================================
	-- Offers
	-- ===============================================================
	CREATE TABLE IF NOT EXISTS offers (
		id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
		offer_key TEXT NOT NULL UNIQUE,

		type TEXT NOT NULL CHECK (type IN ('deal', 'trend')),

		title CITEXT NOT NULL,
		description TEXT,
		image_url TEXT CHECK (image_url IS NULL OR image_url ~* '^https?://'),

		-- Canonical public destination for this offer.
		-- For Deals this may resolve to outbound commerce.
		-- For Trends this should normally resolve to the Platform Trend page/action flow.
		destination_url TEXT NOT NULL CHECK (destination_url ~* '^https?://'),

		price NUMERIC(19,4),
		starting_price NUMERIC(19,4)
			CHECK (starting_price IS NULL OR starting_price >= 0),
		list_price NUMERIC(19,4),
		currency CHAR(3) NOT NULL DEFAULT 'USD' CHECK (currency ~ '^[A-Z]{3}$'),
		discount_percent NUMERIC(5,2)
			CHECK (discount_percent IS NULL OR discount_percent BETWEEN 0 AND 100),

		coupon_code TEXT,

		product_id UUID REFERENCES products(id) ON DELETE SET NULL,
		merchant_id UUID NOT NULL REFERENCES merchants(id) ON DELETE CASCADE,
		category_id UUID NOT NULL REFERENCES categories(id) ON DELETE RESTRICT,

		avg_rating NUMERIC(3,2) NOT NULL DEFAULT 0 CHECK (avg_rating BETWEEN 0 AND 5),
		is_editorial_approved BOOLEAN NOT NULL DEFAULT FALSE,
		status_id UUID REFERENCES offer_statuses(id) ON DELETE RESTRICT,

		is_active BOOLEAN NOT NULL DEFAULT TRUE,
		deleted_at TIMESTAMPTZ,
		expires_at TIMESTAMPTZ,
		published_at TIMESTAMPTZ,
		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

		CONSTRAINT chk_offers_price_non_negative
			CHECK (price IS NULL OR price >= 0),

		CONSTRAINT chk_offers_list_price_non_negative
			CHECK (list_price IS NULL OR list_price >= 0),

		CONSTRAINT chk_offers_price_order
			CHECK (
				price IS NULL OR
				list_price IS NULL OR
				list_price >= price
			)
	);

	CREATE INDEX IF NOT EXISTS idx_offers_merchant_id
		ON offers(merchant_id);

	CREATE INDEX IF NOT EXISTS idx_offers_type
		ON offers(type);

	CREATE INDEX IF NOT EXISTS idx_offers_category_id
		ON offers(category_id);

	CREATE INDEX IF NOT EXISTS idx_offers_active_visible
		ON offers(is_active)
		WHERE is_active = TRUE AND deleted_at IS NULL;

	CREATE INDEX IF NOT EXISTS idx_offers_expires_at
		ON offers(expires_at);

	CREATE OR REPLACE FUNCTION public.normalize_offer_key()
	RETURNS TRIGGER AS $$
	BEGIN
		NEW.offer_key := LOWER(
			REGEXP_REPLACE(
				REGEXP_REPLACE(TRIM(NEW.offer_key), '\s+', '-', 'g'),
				'[^a-z0-9\-]', '', 'g'
			)
		);
		RETURN NEW;
	END;
	$$ LANGUAGE plpgsql;

	DROP TRIGGER IF EXISTS normalize_offer_key_trigger ON public.offers;
	CREATE TRIGGER normalize_offer_key_trigger
	BEFORE INSERT OR UPDATE OF offer_key ON public.offers
	FOR EACH ROW EXECUTE FUNCTION public.normalize_offer_key();


	CREATE TABLE IF NOT EXISTS value_tags (
		id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
		slug CITEXT NOT NULL UNIQUE,
		name CITEXT NOT NULL UNIQUE,
		description TEXT DEFAULT '',
		is_active BOOLEAN NOT NULL DEFAULT TRUE,
		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

		CONSTRAINT chk_value_tags_slug_not_blank
			CHECK (btrim(slug::text) <> ''),
		CONSTRAINT chk_value_tags_name_not_blank
			CHECK (btrim(name::text) <> ''),
		CONSTRAINT chk_value_tags_slug_format
			CHECK (slug::text ~ '^[a-z0-9]+(?:-[a-z0-9]+)*$')
	);

	CREATE INDEX IF NOT EXISTS idx_value_tags_is_active
		ON value_tags(is_active);

	CREATE TABLE IF NOT EXISTS audiences (
		id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
		slug CITEXT NOT NULL UNIQUE,
		name CITEXT NOT NULL UNIQUE,
		description TEXT DEFAULT '',
		is_active BOOLEAN NOT NULL DEFAULT TRUE,
		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

		CONSTRAINT chk_audiences_slug_not_blank
			CHECK (btrim(slug::text) <> ''),
		CONSTRAINT chk_audiences_name_not_blank
			CHECK (btrim(name::text) <> ''),
		CONSTRAINT chk_audiences_slug_format
			CHECK (slug::text ~ '^[a-z0-9]+(?:-[a-z0-9]+)*$')
	);

	CREATE INDEX IF NOT EXISTS idx_audiences_is_active
		ON audiences(is_active);

	-- ===============================================================
	-- DEFERRED: Seasonal Relevances
	-- Non-v1 editorial seasonal labeling vocabulary infrastructure.
	-- Keep schema compile-safe, but do not expand routes, services, UI,
	-- handlers, or tests for Future Offering v1.
	-- ===============================================================
	CREATE TABLE IF NOT EXISTS seasonal_relevances (
		id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

		-- Stable machine-safe identifier used in code, URLs, filtering, and API payloads.
		slug CITEXT NOT NULL UNIQUE,

		-- Human-readable label shown in UI/admin tools.
		name CITEXT NOT NULL UNIQUE,

		-- Optional explanation for editors/admins.
		description TEXT,

		-- Allows soft-retiring a vocabulary item without deleting history.
		is_active BOOLEAN NOT NULL DEFAULT TRUE,

		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

		CONSTRAINT chk_seasonal_relevances_slug_not_blank
			CHECK (btrim(slug::text) <> ''),
		CONSTRAINT chk_seasonal_relevances_name_not_blank
			CHECK (btrim(name::text) <> ''),
		CONSTRAINT chk_seasonal_relevances_slug_format
			CHECK (slug::text ~ '^[a-z0-9]+(?:-[a-z0-9]+)*$')
	);

	CREATE INDEX IF NOT EXISTS idx_seasonal_relevances_is_active
		ON seasonal_relevances(is_active);

	CREATE TABLE IF NOT EXISTS offer_videos (
		id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
		offer_id UUID NOT NULL REFERENCES offers(id) ON DELETE CASCADE,
		video_url TEXT NOT NULL CHECK (video_url ~* '^https?://'),
		caption TEXT,
		sort_order INT NOT NULL DEFAULT 0,
		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
	);

	CREATE TABLE IF NOT EXISTS offer_value_tags (
		offer_id UUID NOT NULL REFERENCES offers(id) ON DELETE CASCADE,
		value_tag_id UUID NOT NULL REFERENCES value_tags(id) ON DELETE CASCADE,
		assigned_by_type TEXT
			CHECK (assigned_by_type IS NULL OR assigned_by_type IN ('editor', 'merchant', 'automation')),
		assigned_by_id UUID REFERENCES users(id) ON DELETE SET NULL,
		confidence_score NUMERIC(5,4)
			CHECK (confidence_score IS NULL OR (confidence_score >= 0 AND confidence_score <= 1)),
		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		PRIMARY KEY (offer_id, value_tag_id),
		CONSTRAINT chk_offer_value_tags_confidence_requires_automation
			CHECK (confidence_score IS NULL OR assigned_by_type = 'automation')
	);

	CREATE TABLE IF NOT EXISTS offer_target_audiences (
		offer_id UUID NOT NULL REFERENCES offers(id) ON DELETE CASCADE,
		audience_id UUID NOT NULL REFERENCES audiences(id) ON DELETE CASCADE,
		assigned_by_type TEXT
			CHECK (assigned_by_type IS NULL OR assigned_by_type IN ('editor', 'merchant', 'automation')),
		assigned_by_id UUID REFERENCES users(id) ON DELETE SET NULL,
		confidence_score NUMERIC(5,4)
			CHECK (confidence_score IS NULL OR (confidence_score >= 0 AND confidence_score <= 1)),
		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		PRIMARY KEY (offer_id, audience_id),
		CONSTRAINT chk_offer_target_audiences_confidence_requires_automation
			CHECK (confidence_score IS NULL OR assigned_by_type = 'automation')
	);

	CREATE TABLE IF NOT EXISTS offer_seasonal_relevances (
		offer_id UUID NOT NULL REFERENCES offers(id) ON DELETE CASCADE,
		seasonal_relevance_id UUID NOT NULL REFERENCES seasonal_relevances(id) ON DELETE CASCADE,
		assigned_by_type TEXT
			CHECK (assigned_by_type IS NULL OR assigned_by_type IN ('editor', 'merchant', 'automation')),
		assigned_by_id UUID REFERENCES users(id) ON DELETE SET NULL,
		confidence_score NUMERIC(5,4)
			CHECK (confidence_score IS NULL OR (confidence_score >= 0 AND confidence_score <= 1)),
		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		PRIMARY KEY (offer_id, seasonal_relevance_id),
		CONSTRAINT chk_offer_seasonal_relevances_confidence_requires_automation
			CHECK (confidence_score IS NULL OR assigned_by_type = 'automation')
	);

	DROP TRIGGER IF EXISTS normalize_value_tags_slug_trigger ON public.value_tags;
	CREATE TRIGGER normalize_value_tags_slug_trigger
	BEFORE INSERT OR UPDATE OF slug ON public.value_tags
	FOR EACH ROW EXECUTE FUNCTION public.normalize_slug();

	DROP TRIGGER IF EXISTS normalize_audiences_slug_trigger ON public.audiences;
	CREATE TRIGGER normalize_audiences_slug_trigger
	BEFORE INSERT OR UPDATE OF slug ON public.audiences
	FOR EACH ROW EXECUTE FUNCTION public.normalize_slug();

	DROP TRIGGER IF EXISTS normalize_seasonal_relevances_slug_trigger ON public.seasonal_relevances;
	CREATE TRIGGER normalize_seasonal_relevances_slug_trigger
	BEFORE INSERT OR UPDATE OF slug ON public.seasonal_relevances
	FOR EACH ROW EXECUTE FUNCTION public.normalize_slug();

	DROP TRIGGER IF EXISTS enforce_trend_only_offer_videos ON public.offer_videos;
	CREATE TRIGGER enforce_trend_only_offer_videos
	BEFORE INSERT OR UPDATE OF offer_id ON public.offer_videos
	FOR EACH ROW EXECUTE FUNCTION public.enforce_trend_only_offer_metadata();

	DROP TRIGGER IF EXISTS enforce_trend_only_offer_value_tags ON public.offer_value_tags;
	CREATE TRIGGER enforce_trend_only_offer_value_tags
	BEFORE INSERT OR UPDATE OF offer_id ON public.offer_value_tags
	FOR EACH ROW EXECUTE FUNCTION public.enforce_trend_only_offer_metadata();

	DROP TRIGGER IF EXISTS enforce_trend_only_offer_target_audiences ON public.offer_target_audiences;
	CREATE TRIGGER enforce_trend_only_offer_target_audiences
	BEFORE INSERT OR UPDATE OF offer_id ON public.offer_target_audiences
	FOR EACH ROW EXECUTE FUNCTION public.enforce_trend_only_offer_metadata();

	DROP TRIGGER IF EXISTS enforce_trend_only_offer_seasonal_relevances ON public.offer_seasonal_relevances;
	CREATE TRIGGER enforce_trend_only_offer_seasonal_relevances
	BEFORE INSERT OR UPDATE OF offer_id ON public.offer_seasonal_relevances
	FOR EACH ROW EXECUTE FUNCTION public.enforce_trend_only_offer_metadata();

	-- ===============================================================
	-- DEFERRED: Coupons / Coupon Flags / Coupon Usages
	-- Non-v1 present-commerce coupon lifecycle infrastructure.
	-- Keep schema compile-safe, but do not expand routes, services, UI,
	-- handlers, or tests for Future Offering v1.
	-- ===============================================================
	CREATE TABLE IF NOT EXISTS coupons (
		id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
		offer_id UUID REFERENCES offers(id) ON DELETE CASCADE,
		coupon_status_id UUID REFERENCES coupon_statuses(id) ON DELETE SET NULL,
		code TEXT NOT NULL,
		discount_type TEXT NOT NULL CHECK (discount_type IN ('percentage', 'fixed', 'rebate')),
		discount_value NUMERIC(19,4) NOT NULL CHECK (discount_value > 0),
		min_purchase_amount NUMERIC(19,4) NOT NULL DEFAULT 0 CHECK (min_purchase_amount >= 0),
		start_date TIMESTAMPTZ NOT NULL,
		end_date TIMESTAMPTZ,
		affiliate_url TEXT,
		rejection_reason TEXT,
		deleted_at TIMESTAMPTZ,
		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		CONSTRAINT chk_coupons_dates CHECK (end_date IS NULL OR end_date > start_date)
	);

	CREATE UNIQUE INDEX IF NOT EXISTS ux_coupons_active_code_offer
		ON coupons(offer_id, code)
		WHERE deleted_at IS NULL;

	CREATE INDEX IF NOT EXISTS idx_coupons_active_offer_id_created_at
		ON coupons(offer_id, created_at DESC)
		WHERE deleted_at IS NULL;

	CREATE INDEX IF NOT EXISTS idx_coupons_active_status_start_end
		ON coupons(coupon_status_id, start_date, end_date)
		WHERE deleted_at IS NULL;

	DROP TRIGGER IF EXISTS enforce_coupons_deal_offer ON public.coupons;
	CREATE TRIGGER enforce_coupons_deal_offer
	BEFORE INSERT OR UPDATE OF offer_id ON public.coupons
	FOR EACH ROW
	WHEN (NEW.offer_id IS NOT NULL)
	EXECUTE FUNCTION public.enforce_deal_offer();

	CREATE TABLE IF NOT EXISTS coupon_flags (
		id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
		coupon_id UUID NOT NULL REFERENCES coupons(id) ON DELETE CASCADE,
		reason TEXT NOT NULL,
		flagged_by UUID REFERENCES users(id) ON DELETE SET NULL,
		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		resolved_at TIMESTAMPTZ,
		resolved_by UUID REFERENCES users(id) ON DELETE SET NULL,
		resolution_notes TEXT,
		resolution_status TEXT CHECK (resolution_status IS NULL OR resolution_status IN ('pending', 'resolved', 'dismissed'))
	);

	CREATE INDEX IF NOT EXISTS idx_coupon_flags_coupon_id ON coupon_flags(coupon_id);

	CREATE TABLE IF NOT EXISTS coupon_usages (
		id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
		user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		coupon_id UUID NOT NULL REFERENCES coupons(id) ON DELETE CASCADE,
		action TEXT NOT NULL CHECK (action IN ('clicked', 'redeemed')),
		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
	);

	CREATE INDEX IF NOT EXISTS idx_coupon_usages_coupon_id ON coupon_usages(coupon_id);
	CREATE INDEX IF NOT EXISTS idx_coupon_usages_user_id ON coupon_usages(user_id);

	-- ===============================================================
	-- DEFERRED: Offer Clicks / Offer Conversions
	-- Non-v1 present-commerce click and conversion tracking infrastructure.
	-- Keep schema compile-safe, but do not expand routes, services, UI,
	-- handlers, or tests for Future Offering v1.
	-- ===============================================================
	CREATE TABLE IF NOT EXISTS offer_clicks (
		id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
		offer_id UUID NOT NULL REFERENCES offers(id) ON DELETE CASCADE,
		user_id UUID REFERENCES users(id) ON DELETE SET NULL,
		ip_address TEXT,
		user_agent TEXT,
		referrer TEXT,
		clicked_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
	);

	CREATE INDEX IF NOT EXISTS idx_offer_clicks_offer_time ON offer_clicks(offer_id, clicked_at DESC);
	CREATE INDEX IF NOT EXISTS idx_offer_clicks_time ON offer_clicks(clicked_at DESC);
	CREATE INDEX IF NOT EXISTS idx_offer_clicks_user_id ON offer_clicks(user_id);

	CREATE TABLE IF NOT EXISTS offer_conversions (
		id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
		offer_id UUID NOT NULL REFERENCES offers(id) ON DELETE CASCADE,
		ip_address TEXT,
		user_agent TEXT,
		referrer TEXT,
		converted_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
	);

	CREATE INDEX IF NOT EXISTS idx_offer_conversions_offer_time ON offer_conversions(offer_id, converted_at DESC);
	CREATE INDEX IF NOT EXISTS idx_offer_conversions_time ON offer_conversions(converted_at DESC);

	-- ===============================================================
	-- DEFERRED: Price Drop Subscriptions
	-- Non-v1 deal price alert subscription infrastructure.
	-- Keep schema compile-safe, but do not expand routes, services, UI,
	-- handlers, or tests for Future Offering v1.
	-- ===============================================================
	CREATE TABLE IF NOT EXISTS price_drop_subscriptions (
		id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
		user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		offer_id UUID NOT NULL REFERENCES offers(id) ON DELETE CASCADE,
		threshold NUMERIC(19,4) NOT NULL CHECK (threshold > 0),
		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		UNIQUE (user_id, offer_id)
	);

	DROP TRIGGER IF EXISTS enforce_price_drop_subscriptions_deal_offer ON public.price_drop_subscriptions;
	CREATE TRIGGER enforce_price_drop_subscriptions_deal_offer
	BEFORE INSERT OR UPDATE OF offer_id ON public.price_drop_subscriptions
	FOR EACH ROW EXECUTE FUNCTION public.enforce_deal_offer();

	-- ===============================================================
	-- DEFERRED: Offer Flags
	-- Non-v1 offer moderation and flagging infrastructure.
	-- Keep schema compile-safe, but do not expand routes, services, UI,
	-- handlers, or tests for Future Offering v1.
	-- ===============================================================
	CREATE TABLE IF NOT EXISTS offer_flags (
		id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
		offer_id UUID NOT NULL REFERENCES offers(id) ON DELETE CASCADE,
		reason TEXT NOT NULL,
		flagged_by UUID REFERENCES users(id) ON DELETE SET NULL,
		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		resolved_at TIMESTAMPTZ,
		resolved_by UUID REFERENCES users(id) ON DELETE SET NULL,
		resolution_notes TEXT,
		resolution_status TEXT CHECK (resolution_status IS NULL OR resolution_status IN ('pending', 'resolved', 'dismissed'))
	);

	CREATE INDEX IF NOT EXISTS idx_offer_flags_offer_id ON offer_flags(offer_id);

	-- ===============================================================
	-- DEFERRED: Offer Price History
	-- Non-v1 deal price tracking and history infrastructure.
	-- Keep schema compile-safe, but do not expand routes, services, UI,
	-- handlers, or tests for Future Offering v1.
	-- ===============================================================
	CREATE TABLE IF NOT EXISTS offer_price_history (
		id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
		offer_id UUID NOT NULL REFERENCES offers(id) ON DELETE CASCADE,
		price NUMERIC(19,4) NOT NULL CHECK (price >= 0),
		recorded_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
	);

	-- CE patch 2: corrected index — was missing column specification entirely.
	CREATE INDEX IF NOT EXISTS idx_offer_price_history_offer_id
		ON offer_price_history(offer_id, recorded_at DESC);


	-- ===============================================================
	-- DEFERRED: User Wishlists / User Favorites
	-- Non-v1 consumer deal and offer affinity infrastructure.
	-- user_wishlists: deal offers only (purchase intent).
	-- user_favorites: unrestricted by offer type (affinity, not commerce).
	-- Keep schema compile-safe, but do not expand routes, services, UI,
	-- handlers, or tests for Future Offering v1.
	-- ===============================================================
	CREATE TABLE IF NOT EXISTS user_wishlists (
		id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
		user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		offer_id UUID NOT NULL REFERENCES offers(id) ON DELETE CASCADE,
		list_name TEXT NOT NULL DEFAULT 'default',
		notes TEXT,
		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		deleted_at TIMESTAMPTZ,
		UNIQUE (user_id, offer_id, list_name)
	);

	CREATE INDEX IF NOT EXISTS idx_user_wishlists_user_active
		ON user_wishlists(user_id, created_at DESC)
		WHERE deleted_at IS NULL;

	CREATE INDEX IF NOT EXISTS idx_user_wishlists_offer_active
		ON user_wishlists(offer_id)
		WHERE deleted_at IS NULL;

	DROP TRIGGER IF EXISTS enforce_user_wishlists_deal_offer
		ON public.user_wishlists;

	CREATE TRIGGER enforce_user_wishlists_deal_offer
	BEFORE INSERT OR UPDATE OF offer_id ON public.user_wishlists
	FOR EACH ROW EXECUTE FUNCTION public.enforce_deal_offer();


	CREATE TABLE IF NOT EXISTS user_favorites (
		id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
		user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		offer_id UUID NOT NULL REFERENCES offers(id) ON DELETE CASCADE,
		deleted_at TIMESTAMPTZ,
		favorited_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
	);

	CREATE UNIQUE INDEX IF NOT EXISTS ux_user_favorites_active_user_offer
		ON user_favorites(user_id, offer_id)
		WHERE deleted_at IS NULL;

	-- ===============================================================
	-- Merchant Program Plans / Entitlements / Subscriptions
	-- ===============================================================
	CREATE TABLE IF NOT EXISTS merchant_program_plans (
		id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
		code TEXT UNIQUE NOT NULL CHECK (code IN (
			'standard',
			'premium',
			'enterprise'
		)),
		name TEXT NOT NULL,
		description TEXT NOT NULL DEFAULT '',
		is_active BOOLEAN NOT NULL DEFAULT TRUE,
		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		deleted_at TIMESTAMPTZ
	);

	CREATE INDEX IF NOT EXISTS idx_merchant_program_plans_active
		ON merchant_program_plans(code)
		WHERE deleted_at IS NULL AND is_active = TRUE;

	
	-- Merchant Program Entitlements
	CREATE TABLE IF NOT EXISTS merchant_program_entitlements (
		id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
		plan_id UUID NOT NULL REFERENCES merchant_program_plans(id) ON DELETE CASCADE,
		entitlement_code TEXT NOT NULL CHECK (entitlement_code IN (
			'launch_campaign_access',
			'future_offering_access'
		)),
		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		UNIQUE (plan_id, entitlement_code)
	);

	CREATE INDEX IF NOT EXISTS idx_merchant_program_entitlements_plan
		ON merchant_program_entitlements(plan_id);

	CREATE OR REPLACE FUNCTION public.ensure_future_offering_includes_campaign()
	RETURNS TRIGGER AS $$
	BEGIN
		IF NEW.entitlement_code = 'future_offering_access' THEN
			INSERT INTO public.merchant_program_entitlements (
				plan_id,
				entitlement_code
			)
			VALUES (
				NEW.plan_id,
				'launch_campaign_access'
			)
			ON CONFLICT (plan_id, entitlement_code) DO NOTHING;
		END IF;

		RETURN NEW;
	END;
	$$ LANGUAGE plpgsql;

	DROP TRIGGER IF EXISTS ensure_future_offering_includes_campaign_trigger
		ON public.merchant_program_entitlements;

	CREATE TRIGGER ensure_future_offering_includes_campaign_trigger
	AFTER INSERT ON public.merchant_program_entitlements
	FOR EACH ROW EXECUTE FUNCTION public.ensure_future_offering_includes_campaign();


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
			CHECK (period_number > 0),

		period_starts_on DATE NOT NULL,
		period_ends_on DATE NOT NULL,

		supersedes_billing_period_id UUID,

		superseded_at TIMESTAMPTZ,

		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

		CONSTRAINT uq_merchant_future_offering_billing_periods_identity
			UNIQUE (
				id,
				service_term_id,
				future_offering_id
			),

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

		CONSTRAINT fk_merchant_future_offering_billing_periods_supersedes
			FOREIGN KEY (
				supersedes_billing_period_id,
				service_term_id,
				future_offering_id
			)
			REFERENCES merchant_future_offering_billing_periods (
				id,
				service_term_id,
				future_offering_id
			)
			ON DELETE RESTRICT,

		CONSTRAINT chk_merchant_future_offering_billing_periods_no_self_supersession
			CHECK (
				supersedes_billing_period_id IS NULL
					OR supersedes_billing_period_id <> id
			),

		CONSTRAINT chk_merchant_future_offering_billing_periods_window
			CHECK (
				period_ends_on > period_starts_on
			),

		CONSTRAINT chk_merchant_future_offering_billing_periods_superseded_after_created
			CHECK (
				superseded_at IS NULL
					OR superseded_at >= created_at
			),

		CONSTRAINT excl_merchant_future_offering_billing_periods_no_current_overlap
			EXCLUDE USING gist (
				service_term_id WITH =,
				daterange(
					period_starts_on,
					period_ends_on,
					'[)'
				) WITH &&
			)
			WHERE (
				superseded_at IS NULL
			)
	);

	-- Exactly one current revision may occupy a logical period number.
	CREATE UNIQUE INDEX IF NOT EXISTS
		ux_merchant_future_offering_billing_periods_current_term_number
	ON merchant_future_offering_billing_periods (
		service_term_id,
		period_number
	)
	WHERE superseded_at IS NULL;

	-- A superseded Billing Period may have at most one direct replacement.
	CREATE UNIQUE INDEX IF NOT EXISTS
		ux_merchant_future_offering_billing_periods_supersedes
	ON merchant_future_offering_billing_periods (
		supersedes_billing_period_id
	)
	WHERE supersedes_billing_period_id IS NOT NULL;

	-- Full Service Term history.
	CREATE INDEX IF NOT EXISTS
		idx_merchant_future_offering_billing_periods_term_timeline
	ON merchant_future_offering_billing_periods (
		service_term_id,
		period_starts_on,
		id
	);

	-- Full Future Offering history across Service Term revisions.
	CREATE INDEX IF NOT EXISTS
		idx_merchant_future_offering_billing_periods_fo_timeline
	ON merchant_future_offering_billing_periods (
		future_offering_id,
		period_starts_on,
		id
	);

	-- Current Billing Period schedule for a Future Offering.
	CREATE INDEX IF NOT EXISTS
		idx_merchant_future_offering_billing_periods_current_schedule
	ON merchant_future_offering_billing_periods (
		future_offering_id,
		period_starts_on,
		period_ends_on,
		id
	)
	WHERE superseded_at IS NULL;

	-- Efficient current point-in-time Billing Period lookup.
	CREATE INDEX IF NOT EXISTS
		idx_merchant_future_offering_billing_periods_current_window
	ON merchant_future_offering_billing_periods
	USING gist (
		future_offering_id,
		daterange(
			period_starts_on,
			period_ends_on,
			'[)'
		)
	)
	WHERE superseded_at IS NULL;


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

		v_superseded_period_number INTEGER;
		v_superseded_at TIMESTAMPTZ;
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
				'merchant_future_offering_billing_periods: service term % does not belong to future offering %',
				NEW.service_term_id,
				NEW.future_offering_id;
		END IF;

		IF v_term_status <> 'established' THEN
			RAISE EXCEPTION
				'merchant_future_offering_billing_periods: service term must be established before billing periods may be created (service_term_id=%, status=%)',
				NEW.service_term_id,
				v_term_status;
		END IF;

		IF v_term_starts_on IS NULL
			OR v_term_ends_on IS NULL
		THEN
			RAISE EXCEPTION
				'merchant_future_offering_billing_periods: established service term must have a complete service window (service_term_id=%)',
				NEW.service_term_id;
		END IF;

		IF NEW.period_starts_on < v_term_starts_on
			OR NEW.period_ends_on > v_term_ends_on
		THEN
			RAISE EXCEPTION
				'merchant_future_offering_billing_periods: billing period [%, %) must be contained within service term [%, %) (service_term_id=%)',
				NEW.period_starts_on,
				NEW.period_ends_on,
				v_term_starts_on,
				v_term_ends_on,
				NEW.service_term_id;
		END IF;

		IF NEW.supersedes_billing_period_id IS NOT NULL THEN
			SELECT
				period_number,
				superseded_at
			INTO
				v_superseded_period_number,
				v_superseded_at
			FROM merchant_future_offering_billing_periods
			WHERE id = NEW.supersedes_billing_period_id
				AND service_term_id = NEW.service_term_id
				AND future_offering_id = NEW.future_offering_id
			FOR SHARE;

			IF NOT FOUND THEN
				RAISE EXCEPTION
					'merchant_future_offering_billing_periods: superseded billing period % does not belong to service term % and future offering %',
					NEW.supersedes_billing_period_id,
					NEW.service_term_id,
					NEW.future_offering_id;
			END IF;

			IF v_superseded_at IS NULL THEN
				RAISE EXCEPTION
					'merchant_future_offering_billing_periods: replacement billing period may reference only an already-superseded billing period (supersedes_billing_period_id=%)',
					NEW.supersedes_billing_period_id;
			END IF;

			IF NEW.period_number <> v_superseded_period_number THEN
				RAISE EXCEPTION
					'merchant_future_offering_billing_periods: replacement billing period must retain predecessor period_number (supersedes_billing_period_id=%, expected_period_number=%, supplied_period_number=%)',
					NEW.supersedes_billing_period_id,
					v_superseded_period_number,
					NEW.period_number;
			END IF;
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
	-- Lifecycle enforcement
	-- =====================================================================

	CREATE OR REPLACE FUNCTION
		merchant_future_offering_billing_periods_enforce_lifecycle()
	RETURNS TRIGGER
	LANGUAGE plpgsql
	AS $$
	BEGIN
		IF OLD.superseded_at IS NOT NULL THEN
			RAISE EXCEPTION
				'merchant_future_offering_billing_periods: superseded billing periods are immutable (id=%)',
				OLD.id;
		END IF;

		IF NEW.service_term_id <> OLD.service_term_id
			OR NEW.future_offering_id <> OLD.future_offering_id
			OR NEW.period_number <> OLD.period_number
			OR NEW.period_starts_on <> OLD.period_starts_on
			OR NEW.period_ends_on <> OLD.period_ends_on
			OR NEW.supersedes_billing_period_id
				IS DISTINCT FROM OLD.supersedes_billing_period_id
			OR NEW.created_at <> OLD.created_at
		THEN
			RAISE EXCEPTION
				'merchant_future_offering_billing_periods: established billing period facts are immutable (id=%)',
				OLD.id;
		END IF;

		IF NEW.superseded_at IS NULL THEN
			RAISE EXCEPTION
				'merchant_future_offering_billing_periods: no mutable billing period facts were supplied (id=%)',
				OLD.id;
		END IF;

		IF OLD.period_starts_on <= CURRENT_DATE THEN
			RAISE EXCEPTION
				'merchant_future_offering_billing_periods: a billing period that has begun cannot be superseded (id=%, period_starts_on=%)',
				OLD.id,
				OLD.period_starts_on;
		END IF;

		RETURN NEW;
	END;
	$$;

	DROP TRIGGER IF EXISTS
		trg_merchant_future_offering_billing_periods_enforce_lifecycle
		ON merchant_future_offering_billing_periods;

	CREATE TRIGGER
		trg_merchant_future_offering_billing_periods_enforce_lifecycle
	BEFORE UPDATE ON merchant_future_offering_billing_periods
	FOR EACH ROW
	EXECUTE FUNCTION
		merchant_future_offering_billing_periods_enforce_lifecycle();


	-- =====================================================================
	-- Merchant Future Offering Payment Periods
	-- =====================================================================
	--
	-- One row represents one authoritative Payment Period within the
	-- settlement schedule established for a specific Future Offering
	-- Billing Period.
	--
	-- A Payment Period:
	--   * does not establish the amount owed;
	--   * is not a payment transaction;
	--   * is not an invoice;
	--   * is not a payment method;
	--   * does not encode provider state;
	--   * does not inherently mean "installment";
	--   * may occur before, during, or after the associated Billing Period
	--     where the governing commercial arrangement permits it.
	--
	-- The current non-superseded rows for a Billing Period constitute that
	-- Billing Period's authoritative Payment Period schedule.
	-- =====================================================================

CREATE TABLE IF NOT EXISTS merchant_future_offering_service_periods (
	id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

	service_term_id UUID NOT NULL,
	future_offering_id UUID NOT NULL,

	period_number INTEGER NOT NULL
		CHECK (period_number > 0),

	period_starts_on DATE NOT NULL,
	period_ends_on DATE NOT NULL,

	superseded_at TIMESTAMPTZ,

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

	CONSTRAINT chk_merchant_future_offering_service_periods_superseded_after_created
		CHECK (
			superseded_at IS NULL
				OR superseded_at >= created_at
		),

	CONSTRAINT excl_merchant_future_offering_service_periods_no_current_overlap
		EXCLUDE USING gist (
			service_term_id WITH =,
			daterange(
				period_starts_on,
				period_ends_on,
				'[)'
			) WITH &&
		)
		WHERE (
			superseded_at IS NULL
		)
);

CREATE UNIQUE INDEX IF NOT EXISTS
	ux_merchant_future_offering_service_periods_current_term_number
ON merchant_future_offering_service_periods (
	service_term_id,
	period_number
)
WHERE superseded_at IS NULL;

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
	idx_merchant_future_offering_service_periods_current_term_schedule
ON merchant_future_offering_service_periods (
	service_term_id,
	period_number,
	id
)
WHERE superseded_at IS NULL;

CREATE INDEX IF NOT EXISTS
	idx_merchant_future_offering_service_periods_current_window
ON merchant_future_offering_service_periods
USING gist (
	service_term_id,
	daterange(
		period_starts_on,
		period_ends_on,
		'[)'
	)
)
WHERE superseded_at IS NULL;


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
	FOR KEY SHARE;

	IF NOT FOUND THEN
		RAISE EXCEPTION
			'merchant_future_offering_service_periods: service term % does not belong to future offering %',
			NEW.service_term_id,
			NEW.future_offering_id;
	END IF;

	IF v_term_status <> 'established' THEN
		RAISE EXCEPTION
			'merchant_future_offering_service_periods: service term must be established before service periods may be created (service_term_id=%, status=%)',
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

	IF NEW.superseded_at IS NOT NULL THEN
		RAISE EXCEPTION
			'merchant_future_offering_service_periods: a new service period cannot already be superseded';
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
DECLARE
	v_term_status TEXT;
BEGIN
	IF TG_OP = 'DELETE' THEN
		RAISE EXCEPTION
			'merchant_future_offering_service_periods: service periods cannot be deleted (id=%)',
			OLD.id;
	END IF;

	IF OLD.superseded_at IS NOT NULL THEN
		RAISE EXCEPTION
			'merchant_future_offering_service_periods: superseded service periods are immutable (id=%)',
			OLD.id;
	END IF;

	IF NEW.service_term_id <> OLD.service_term_id
		OR NEW.future_offering_id <> OLD.future_offering_id
		OR NEW.period_number <> OLD.period_number
		OR NEW.period_starts_on <> OLD.period_starts_on
		OR NEW.period_ends_on <> OLD.period_ends_on
		OR NEW.created_at <> OLD.created_at
	THEN
		RAISE EXCEPTION
			'merchant_future_offering_service_periods: established service period facts are immutable (id=%)',
			OLD.id;
	END IF;

	IF NEW.superseded_at IS NULL THEN
		RAISE EXCEPTION
			'merchant_future_offering_service_periods: no mutable service period facts were supplied (id=%)',
			OLD.id;
	END IF;

	SELECT
		term_status
	INTO
		v_term_status
	FROM merchant_future_offering_service_terms
	WHERE id = OLD.service_term_id
		AND future_offering_id = OLD.future_offering_id
	FOR KEY SHARE;

	IF NOT FOUND
		OR v_term_status <> 'established'
	THEN
		RAISE EXCEPTION
			'merchant_future_offering_service_periods: only periods belonging to the established service term may be prospectively superseded (id=%)',
			OLD.id;
	END IF;

	IF OLD.period_starts_on <= CURRENT_DATE THEN
		RAISE EXCEPTION
			'merchant_future_offering_service_periods: a service period that has begun cannot be superseded (id=%, period_starts_on=%)',
			OLD.id,
			OLD.period_starts_on;
	END IF;

	-- Persisted lifecycle time is database-owned.
	NEW.superseded_at := NOW();

	RETURN NEW;
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


	-- 
	CREATE TABLE IF NOT EXISTS merchant_program_subscriptions (
		id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
		merchant_id UUID NOT NULL REFERENCES merchants(id) ON DELETE CASCADE,
		plan_id UUID NOT NULL REFERENCES merchant_program_plans(id) ON DELETE RESTRICT,

		status TEXT NOT NULL DEFAULT 'pending'
			CHECK (status IN ('pending', 'active', 'paused', 'cancelled', 'expired', 'suspended')),

		billing_period TEXT NOT NULL DEFAULT 'monthly'
			CHECK (billing_period IN ('monthly', 'annual', 'custom')),

		started_at TIMESTAMPTZ,
		expires_at TIMESTAMPTZ,
		cancelled_at TIMESTAMPTZ,

		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		deleted_at TIMESTAMPTZ,

		CONSTRAINT chk_merchant_program_subscriptions_dates
			CHECK (
				started_at IS NULL
				OR expires_at IS NULL
				OR expires_at > started_at
			)
	);

	CREATE UNIQUE INDEX IF NOT EXISTS ux_merchant_program_subscriptions_one_active
		ON merchant_program_subscriptions(merchant_id)
		WHERE deleted_at IS NULL
		  AND status IN ('pending', 'active', 'paused', 'suspended');

	CREATE INDEX IF NOT EXISTS idx_merchant_program_subscriptions_plan_status
		ON merchant_program_subscriptions(plan_id, status)
		WHERE deleted_at IS NULL;

	CREATE INDEX IF NOT EXISTS idx_merchant_program_subscriptions_merchant
		ON merchant_program_subscriptions(merchant_id)
		WHERE deleted_at IS NULL;


	-- Merchant Program Subscription Periods
	CREATE TABLE IF NOT EXISTS merchant_program_subscription_periods (
		id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

		subscription_id UUID NOT NULL
			CONSTRAINT fk_merchant_program_subscription_periods_subscription
			REFERENCES merchant_program_subscriptions(id)
			ON DELETE RESTRICT,

		plan_id UUID NOT NULL
			CONSTRAINT fk_merchant_program_subscription_periods_plan
			REFERENCES merchant_program_plans(id)
			ON DELETE RESTRICT,

		billing_period TEXT NOT NULL
			CONSTRAINT chk_merchant_program_subscription_periods_billing_period
			CHECK (
				billing_period IN (
					'monthly',
					'annual',
					'custom'
				)
			),

		period_start TIMESTAMPTZ NOT NULL,
		period_end TIMESTAMPTZ NOT NULL,

		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

		CONSTRAINT chk_merchant_program_subscription_periods_window
			CHECK (
				period_end > period_start
			),

		CONSTRAINT uq_merchant_program_subscription_periods_subscription_start
			UNIQUE (
				subscription_id,
				period_start
			),

		CONSTRAINT excl_merchant_program_subscription_periods_no_overlap
			EXCLUDE USING gist (
				subscription_id WITH =,
				tstzrange(period_start, period_end, '[)') WITH &&
			)
	);

	CREATE INDEX IF NOT EXISTS
		idx_merchant_program_subscription_periods_subscription_timeline
	ON merchant_program_subscription_periods (
		subscription_id,
		period_start DESC,
		id DESC
	);

	-- Merchant Program Subscription Events
	CREATE TABLE IF NOT EXISTS merchant_program_subscription_events (
		id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

		subscription_id UUID NOT NULL
			REFERENCES merchant_program_subscriptions(id)
			ON DELETE RESTRICT,

		event_type TEXT NOT NULL
			CHECK (
				event_type IN (
					'created',
					'activated',
					'plan_changed',
					'paused',
					'resumed',
					'cancelled',
					'expired',
					'suspended'
				)
			),

		note TEXT,

		performed_by UUID
			REFERENCES users(id)
			ON DELETE SET NULL,

		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
	);

	CREATE INDEX IF NOT EXISTS
		idx_merchant_program_subscription_events_subscription_timeline
	ON merchant_program_subscription_events (
		subscription_id,
		created_at,
		id
	);

	CREATE INDEX IF NOT EXISTS
		idx_merchant_program_subscription_events_subscription_type_timeline
	ON merchant_program_subscription_events (
		subscription_id,
		event_type,
		created_at,
		id
	);


	-- ===============================================================
	-- DEFERRED: Merchant Platform Credits / Merchant Platform Credit Eligible Fee Types
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

	-- ===============================================================
	-- Future-commerce anticipation infrastructure.
	-- Watchable by consumers.
	-- ===============================================================

	CREATE TABLE IF NOT EXISTS merchant_future_offerings (
		id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

		merchant_id UUID NOT NULL REFERENCES merchants(id) ON DELETE CASCADE,
		offer_id UUID UNIQUE REFERENCES offers(id) ON DELETE SET NULL,
		product_id UUID REFERENCES products(id) ON DELETE SET NULL,
		category_id UUID NOT NULL REFERENCES categories(id) ON DELETE RESTRICT,
		subscription_id UUID NOT NULL REFERENCES merchant_program_subscriptions(id) ON DELETE RESTRICT,

		offering_type TEXT NOT NULL CHECK (offering_type IN (
			'product',
			'service',
			'event',
			'venue',
			'development',
			'experience'
		)),

		project_name TEXT NOT NULL CHECK (btrim(project_name) <> ''),
		title TEXT NOT NULL CHECK (btrim(title) <> ''),
		summary TEXT NOT NULL DEFAULT '',
		description TEXT,

		launch_kind TEXT NOT NULL DEFAULT 'standard'
			CHECK (launch_kind IN (
				'standard',
				'product_launch',
				'drop',
				'limited_release',
				'creator_launch',
				'startup_launch',
				'collaboration',
				'preorder',
				'waitlist',
				'early_access',
				'invite_only'
			)),

		status TEXT NOT NULL DEFAULT 'draft'
			CHECK (status IN (
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
			)),

		launch_at TIMESTAMPTZ,
		countdown_starts_at TIMESTAMPTZ,

		submitted_at TIMESTAMPTZ,
		approved_at TIMESTAMPTZ,
		published_at TIMESTAMPTZ,
		rejected_at TIMESTAMPTZ,
		unpublished_at TIMESTAMPTZ,
		archived_at TIMESTAMPTZ,

		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		deleted_at TIMESTAMPTZ,

		CONSTRAINT chk_merchant_future_offerings_countdown_before_launch
			CHECK (
				launch_at IS NULL
				OR countdown_starts_at IS NULL
				OR countdown_starts_at <= launch_at
			)
	);

	CREATE INDEX IF NOT EXISTS idx_merchant_future_offerings_merchant_status
		ON merchant_future_offerings(merchant_id, status)
		WHERE deleted_at IS NULL;

	CREATE INDEX IF NOT EXISTS idx_merchant_future_offerings_category_status
		ON merchant_future_offerings(category_id, status, launch_at)
		WHERE deleted_at IS NULL;

	CREATE INDEX IF NOT EXISTS idx_merchant_future_offerings_published
		ON merchant_future_offerings(published_at DESC)
		WHERE deleted_at IS NULL AND status = 'published';

	DROP TRIGGER IF EXISTS enforce_merchant_future_offerings_trend_offer
		ON public.merchant_future_offerings;

	CREATE TRIGGER enforce_merchant_future_offerings_trend_offer
	BEFORE INSERT OR UPDATE OF offer_id ON public.merchant_future_offerings
	FOR EACH ROW
	WHEN (NEW.offer_id IS NOT NULL)
	EXECUTE FUNCTION public.enforce_trend_offer();


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

	CREATE TABLE IF NOT EXISTS merchant_future_offering_engagement_options (
		id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

		future_offering_id UUID NOT NULL
			REFERENCES merchant_future_offerings(id) ON DELETE CASCADE,

		action_type TEXT NOT NULL CHECK (action_type IN (
			'watch',
			'waitlist',
			'early_access',
			'preorder'
		)),

		fulfillment_mode TEXT NOT NULL DEFAULT 'platform_hosted'
			CHECK (fulfillment_mode IN (
				'platform_hosted',
				'merchant_hosted',
				'disabled'
			)),

		action_url TEXT NOT NULL CHECK (action_url ~* '^https?://'),

		is_primary BOOLEAN NOT NULL DEFAULT FALSE,
		is_active BOOLEAN NOT NULL DEFAULT TRUE,

		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

		UNIQUE (future_offering_id, action_type)
	);

	-- CE patch 4: renamed index suffix from _launch to _active.
	CREATE INDEX IF NOT EXISTS idx_merchant_future_offering_engagement_options_active
		ON merchant_future_offering_engagement_options(future_offering_id)
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
	-- DEFERRED: Merchant Program Benefits / Founding Merchant Incentives
	--
	-- Purpose:
	--   Preserves the architecture for Founding Merchant benefits, waivers,
	--   credits, free months, and future fee incentives.
	--
	-- Release status:
	--   DEFERRED. Not routed, not serviced, not release-blocking for v1.
	-- =====================================================================

	-- Merchant Program Benefits
	CREATE TABLE IF NOT EXISTS merchant_program_benefits (
		id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

		code TEXT NOT NULL UNIQUE
			CHECK (btrim(code) <> ''),

		name TEXT NOT NULL
			CHECK (btrim(name) <> ''),

		description TEXT NOT NULL DEFAULT '',

		benefit_type TEXT NOT NULL
			CHECK (benefit_type IN (
				'fee_waiver',
				'fee_discount',
				'fee_credit',
				'subscription_free_period'
			)),

		value_json JSONB NOT NULL DEFAULT '{}'::jsonb,

		is_active BOOLEAN NOT NULL DEFAULT TRUE,

		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		deleted_at TIMESTAMPTZ,

		CONSTRAINT chk_merchant_program_benefits_code_format
			CHECK (code ~ '^[a-z][a-z0-9_]*$')
	);


	CREATE TABLE IF NOT EXISTS merchant_program_plan_benefits (
		id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

		plan_id UUID NOT NULL REFERENCES merchant_program_plans(id) ON DELETE CASCADE,
		benefit_id UUID NOT NULL REFERENCES merchant_program_benefits(id) ON DELETE RESTRICT,

		is_active BOOLEAN NOT NULL DEFAULT TRUE,

		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		deleted_at TIMESTAMPTZ,

		CONSTRAINT merchant_program_plan_benefits_unique
			UNIQUE (plan_id, benefit_id)
	);

	CREATE TABLE IF NOT EXISTS merchant_benefit_grants (
		id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

		merchant_id UUID NOT NULL REFERENCES merchants(id) ON DELETE CASCADE,
		benefit_id UUID NOT NULL REFERENCES merchant_program_benefits(id) ON DELETE RESTRICT,

		grant_reason TEXT NOT NULL DEFAULT '',
		starts_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		ends_at TIMESTAMPTZ,

		is_active BOOLEAN NOT NULL DEFAULT TRUE,

		created_by UUID REFERENCES users(id) ON DELETE SET NULL,
		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		deleted_at TIMESTAMPTZ,

		CONSTRAINT merchant_benefit_grants_unique_active_window
			UNIQUE (merchant_id, benefit_id, starts_at)
	);


	-- Merchant Fee Credits
	CREATE TABLE IF NOT EXISTS merchant_fee_credits (
		id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

		merchant_id UUID NOT NULL
			REFERENCES merchants(id)
			ON DELETE CASCADE,

		benefit_grant_id UUID
			REFERENCES merchant_benefit_grants(id)
			ON DELETE SET NULL,

		fee_type_id UUID NOT NULL
			REFERENCES merchant_fee_types(id)
			ON DELETE RESTRICT,

		amount NUMERIC(19,4) NOT NULL
			CHECK (amount >= 0),

		currency CHAR(3) NOT NULL DEFAULT 'USD'
			CHECK (currency ~ '^[A-Z]{3}$'),

		remaining_amount NUMERIC(19,4) NOT NULL
			CHECK (remaining_amount >= 0),

		expires_at TIMESTAMPTZ,

		is_active BOOLEAN NOT NULL DEFAULT TRUE,

		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		deleted_at TIMESTAMPTZ,

		CONSTRAINT chk_merchant_fee_credits_remaining_not_greater_than_amount
			CHECK (remaining_amount <= amount)
	);


	-- Merchant Fee Waivers
	CREATE TABLE IF NOT EXISTS merchant_fee_waivers (
		id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

		merchant_id UUID NOT NULL
			REFERENCES merchants(id)
			ON DELETE CASCADE,

		benefit_grant_id UUID
			REFERENCES merchant_benefit_grants(id)
			ON DELETE SET NULL,

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
			CHECK (waiver_value >= 0),

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
	-- User Trend Engagements (My Radar)
	-- Every future-commerce consumer action begins from, or implies, a watch.
	-- Notification is a watch setting, not a separate intent type.
	-- trend offers only — enforced by trigger.
	-- ===============================================================

	CREATE TABLE IF NOT EXISTS user_trend_engagements (
		id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

		user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		offer_id UUID NOT NULL REFERENCES offers(id) ON DELETE CASCADE,

		engagement_status TEXT NOT NULL DEFAULT 'active'
			CHECK (engagement_status IN ('active', 'muted', 'removed')),

		is_watching BOOLEAN NOT NULL DEFAULT TRUE,
		notification_enabled BOOLEAN NOT NULL DEFAULT TRUE,

		watched_at TIMESTAMPTZ,
		waitlisted_at TIMESTAMPTZ,
		early_access_requested_at TIMESTAMPTZ,
		preorder_interest_at TIMESTAMPTZ,

		source_surface TEXT NOT NULL DEFAULT 'direct'
			CHECK (source_surface IN (
				'trend_card',
				'trend_detail',
				'notification',
				'direct'
			)),

		first_engaged_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		last_activity_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		deleted_at TIMESTAMPTZ,

		CONSTRAINT chk_user_trend_engagements_has_action
			CHECK (
				is_watching = TRUE
				OR waitlisted_at IS NOT NULL
				OR early_access_requested_at IS NOT NULL
				OR preorder_interest_at IS NOT NULL
			)
	);

	CREATE UNIQUE INDEX IF NOT EXISTS ux_user_trend_engagements_active_user_offer
		ON user_trend_engagements(user_id, offer_id)
		WHERE deleted_at IS NULL;

	CREATE INDEX IF NOT EXISTS idx_user_trend_engagements_user_active
		ON user_trend_engagements(user_id, last_activity_at DESC)
		WHERE deleted_at IS NULL;

	CREATE INDEX IF NOT EXISTS idx_user_trend_engagements_offer_active
		ON user_trend_engagements(offer_id)
		WHERE deleted_at IS NULL AND engagement_status IN ('active', 'muted');

	CREATE INDEX IF NOT EXISTS idx_user_trend_engagements_waitlisted
		ON user_trend_engagements(offer_id, waitlisted_at DESC)
		WHERE waitlisted_at IS NOT NULL AND deleted_at IS NULL;

	CREATE INDEX IF NOT EXISTS idx_user_trend_engagements_early_access
		ON user_trend_engagements(offer_id, early_access_requested_at DESC)
		WHERE early_access_requested_at IS NOT NULL AND deleted_at IS NULL;

	CREATE INDEX IF NOT EXISTS idx_user_trend_engagements_preorder
		ON user_trend_engagements(offer_id, preorder_interest_at DESC)
		WHERE preorder_interest_at IS NOT NULL AND deleted_at IS NULL;

	DROP TRIGGER IF EXISTS enforce_user_trend_engagements_trend_offer
		ON public.user_trend_engagements;

	CREATE TRIGGER enforce_user_trend_engagements_trend_offer
	BEFORE INSERT OR UPDATE OF offer_id ON public.user_trend_engagements
	FOR EACH ROW EXECUTE FUNCTION public.enforce_trend_offer();


	-- User Trend Engagement Events
	CREATE TABLE IF NOT EXISTS user_trend_engagement_events (
		id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

		engagement_id UUID NOT NULL
			REFERENCES user_trend_engagements(id)
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
					'trend_card',
					'trend_detail',
					'notification',
					'direct'
				)
			),

		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
	);

	CREATE INDEX IF NOT EXISTS idx_user_trend_engagement_events_engagement
		ON user_trend_engagement_events (
			engagement_id,
			created_at DESC
		);


	-- ===============================================================
	-- DEFERRED: Merchant Launch Campaigns / Campaign Clicks / Campaign Attribution Events
	-- Non-v1 merchant-direct affiliate/performance campaign infrastructure.
	-- Keep schema compile-safe, but do not expand routes, services, UI,
	-- handlers, or tests for Future Offering v1.
	-- ===============================================================
	CREATE TABLE IF NOT EXISTS merchant_launch_campaigns (
		id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

		merchant_id UUID NOT NULL REFERENCES merchants(id) ON DELETE CASCADE,
		offer_id UUID REFERENCES offers(id) ON DELETE SET NULL,
		category_id UUID REFERENCES categories(id) ON DELETE SET NULL,
		subscription_id UUID REFERENCES merchant_program_subscriptions(id) ON DELETE SET NULL,

		title TEXT NOT NULL CHECK (btrim(title) <> ''),
		summary TEXT NOT NULL DEFAULT '',
		description TEXT,

		campaign_kind TEXT NOT NULL DEFAULT 'promotion'
			CHECK (campaign_kind IN (
				'promotion',
				'seasonal_sale',
				'limited_time_offer',
				'bundle',
				'featured_offer',
				'merchant_push',
				'launch_period_campaign'
			)),

		status TEXT NOT NULL DEFAULT 'draft'
			CHECK (status IN (
				'draft',
				'submitted',
				'approved',
				'active',
				'paused',
				'ended',
				'rejected',
				'archived'
			)),

		destination_url TEXT NOT NULL CHECK (destination_url ~* '^https?://'),

		tracking_method TEXT NOT NULL DEFAULT 'route_token'
			CHECK (tracking_method IN (
				'route_token',
				'utm',
				'coupon_code',
				'postback',
				'merchant_reported'
			)),

		economics_type TEXT NOT NULL DEFAULT 'campaign_fee'
			CHECK (economics_type IN (
				'campaign_fee',
				'merchant_direct_commission',
				'affiliate_commission',
				'flat_fee',
				'none'
			)),

		starts_at TIMESTAMPTZ,
		ends_at TIMESTAMPTZ,

		submitted_at TIMESTAMPTZ,
		approved_at TIMESTAMPTZ,
		activated_at TIMESTAMPTZ,
		ended_at TIMESTAMPTZ,

		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		deleted_at TIMESTAMPTZ,

		CONSTRAINT chk_merchant_launch_campaigns_dates
			CHECK (
				starts_at IS NULL
				OR ends_at IS NULL
				OR ends_at > starts_at
			)
	);

	CREATE INDEX IF NOT EXISTS idx_merchant_launch_campaigns_merchant_status
		ON merchant_launch_campaigns(merchant_id, status)
		WHERE deleted_at IS NULL;

	CREATE INDEX IF NOT EXISTS idx_merchant_launch_campaigns_active_window
		ON merchant_launch_campaigns(starts_at, ends_at)
		WHERE deleted_at IS NULL AND status = 'active';

	DROP TRIGGER IF EXISTS enforce_merchant_launch_campaign_deal_offer
		ON public.merchant_launch_campaigns;

	CREATE TRIGGER enforce_merchant_launch_campaign_deal_offer
	BEFORE INSERT OR UPDATE OF offer_id ON public.merchant_launch_campaigns
	FOR EACH ROW
	WHEN (NEW.offer_id IS NOT NULL)
	EXECUTE FUNCTION public.enforce_deal_offer();


	CREATE TABLE IF NOT EXISTS merchant_launch_campaign_clicks (
		id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

		campaign_id UUID NOT NULL REFERENCES merchant_launch_campaigns(id) ON DELETE CASCADE,
		user_id UUID REFERENCES users(id) ON DELETE SET NULL,

		route_token_hash TEXT NOT NULL,
		session_id TEXT,
		source_surface TEXT CHECK (source_surface IS NULL OR source_surface IN (
			'campaign_card',
			'campaign_detail',
			'deal_card',
			'deal_detail',
			'merchant_page',
			'notification',
			'direct'
		)),

		ip_hash TEXT,
		user_agent_hash TEXT,
		referrer TEXT,
		clicked_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
	);

	CREATE INDEX IF NOT EXISTS idx_merchant_launch_campaign_clicks_campaign_time
		ON merchant_launch_campaign_clicks(campaign_id, clicked_at DESC);

	CREATE INDEX IF NOT EXISTS idx_merchant_launch_campaign_clicks_user_time
		ON merchant_launch_campaign_clicks(user_id, clicked_at DESC)
		WHERE user_id IS NOT NULL;


	CREATE TABLE IF NOT EXISTS merchant_launch_campaign_attribution_events (
		id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

		campaign_id UUID NOT NULL REFERENCES merchant_launch_campaigns(id) ON DELETE CASCADE,
		click_id UUID REFERENCES merchant_launch_campaign_clicks(id) ON DELETE SET NULL,
		user_id UUID REFERENCES users(id) ON DELETE SET NULL,

		event_type TEXT NOT NULL CHECK (event_type IN (
			'merchant_reported_conversion',
			'postback_conversion',
			'coupon_reported_conversion',
			'commission_approved',
			'commission_rejected',
			'commission_reversed'
		)),

		external_event_id TEXT,
		order_reference_hash TEXT,
		amount NUMERIC(19,4) CHECK (amount IS NULL OR amount >= 0),
		currency CHAR(3) CHECK (currency IS NULL OR currency ~ '^[A-Z]{3}$'),
		commission_amount NUMERIC(19,4) CHECK (commission_amount IS NULL OR commission_amount >= 0),

		metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
		occurred_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
	);

	CREATE INDEX IF NOT EXISTS idx_merchant_launch_campaign_attribution_campaign_time
		ON merchant_launch_campaign_attribution_events(campaign_id, occurred_at DESC);

	CREATE UNIQUE INDEX IF NOT EXISTS ux_merchant_launch_campaign_attribution_external_event
		ON merchant_launch_campaign_attribution_events(campaign_id, external_event_id)
		WHERE external_event_id IS NOT NULL;


	-- ===============================================================
	-- Launch Intelligence Trust Review
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
	-- DEFERRED: Commerce Routing / Attribution Bridge / Commerce Route Events / Merchant Direct Attribution Configs
	-- Non-v1 deal routing, click tracking, and attribution infrastructure.
	-- Keep schema compile-safe, but do not expand routes, services, UI,
	-- handlers, or tests for Future Offering v1.
	-- ===============================================================
	CREATE TABLE IF NOT EXISTS commerce_routes (
		id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

		offer_id UUID NOT NULL REFERENCES offers(id) ON DELETE CASCADE,
		merchant_id UUID NOT NULL REFERENCES merchants(id) ON DELETE CASCADE,

		route_domain TEXT NOT NULL DEFAULT 'deal'
			CHECK (route_domain IN ('deal')),

		destination_url TEXT NOT NULL CHECK (destination_url ~* '^https?://'),
		route_token_hash TEXT NOT NULL UNIQUE,

		route_type TEXT NOT NULL CHECK (route_type IN (
			'deal_click',
			'coupon_click',
			'launch_campaign_click'
		)),

		economics_type TEXT NOT NULL CHECK (economics_type IN (
			'affiliate_commission',
			'campaign_fee',
			'merchant_direct_commission',
			'none'
		)),

		is_active BOOLEAN NOT NULL DEFAULT TRUE,
		expires_at TIMESTAMPTZ,

		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		deleted_at TIMESTAMPTZ
	);

	CREATE INDEX IF NOT EXISTS idx_commerce_routes_offer
		ON commerce_routes(offer_id)
		WHERE deleted_at IS NULL;

	CREATE INDEX IF NOT EXISTS idx_commerce_routes_domain_offer
		ON commerce_routes(route_domain, offer_id)
		WHERE deleted_at IS NULL;

	CREATE INDEX IF NOT EXISTS idx_commerce_routes_domain_economics
		ON commerce_routes(route_domain, economics_type)
		WHERE deleted_at IS NULL;

	DROP TRIGGER IF EXISTS enforce_commerce_routes_deal_offer
		ON public.commerce_routes;

	CREATE TRIGGER enforce_commerce_routes_deal_offer
	BEFORE INSERT OR UPDATE OF offer_id ON public.commerce_routes
	FOR EACH ROW EXECUTE FUNCTION public.enforce_deal_offer();

	CREATE OR REPLACE FUNCTION public.enforce_commerce_route_offer_domain()
	RETURNS TRIGGER AS $$
	DECLARE
		v_offer_type TEXT;
		v_merchant_id UUID;
	BEGIN
		SELECT type, merchant_id
		INTO v_offer_type, v_merchant_id
		FROM public.offers
		WHERE id = NEW.offer_id
		  AND deleted_at IS NULL;

		IF NOT FOUND THEN
			RAISE EXCEPTION 'offer "%" does not exist or is deleted', NEW.offer_id;
		END IF;

		IF NEW.route_domain <> v_offer_type THEN
			RAISE EXCEPTION 'commerce route domain "%" does not match offer type "%"', NEW.route_domain, v_offer_type;
		END IF;

		IF NEW.merchant_id <> v_merchant_id THEN
			RAISE EXCEPTION 'commerce route merchant "%" does not match offer merchant "%"', NEW.merchant_id, v_merchant_id;
		END IF;

		RETURN NEW;
	END;
	$$ LANGUAGE plpgsql;

	DROP TRIGGER IF EXISTS enforce_commerce_route_offer_domain
		ON public.commerce_routes;

	CREATE TRIGGER enforce_commerce_route_offer_domain
	BEFORE INSERT OR UPDATE OF offer_id, merchant_id, route_domain
	ON public.commerce_routes
	FOR EACH ROW EXECUTE FUNCTION public.enforce_commerce_route_offer_domain();

	CREATE TABLE IF NOT EXISTS commerce_route_events (
		id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

		route_id UUID NOT NULL REFERENCES commerce_routes(id) ON DELETE CASCADE,
		offer_id UUID NOT NULL REFERENCES offers(id) ON DELETE CASCADE,
		user_id UUID REFERENCES users(id) ON DELETE SET NULL,

		session_id TEXT,

		source_surface TEXT CHECK (source_surface IS NULL OR source_surface IN (
			'deal_card',
			'deal_detail',
			'notification',
			'direct'
		)),

		ip_hash TEXT,
		user_agent_hash TEXT,
		clicked_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
	);

	CREATE INDEX IF NOT EXISTS idx_commerce_route_events_route_clicked
		ON commerce_route_events(route_id, clicked_at DESC);

	CREATE INDEX IF NOT EXISTS idx_commerce_route_events_offer_clicked
		ON commerce_route_events(offer_id, clicked_at DESC);

	-- CE patch 6: commerce route event integrity trigger.
	-- Enforces that (a) the route exists and is active, (b) the event's
	-- offer_id matches the route's offer_id, and (c) the offer is a deal.
	-- Stronger than a plain enforce_deal_offer trigger because it also
	-- validates route/offer consistency in a single function.
	CREATE OR REPLACE FUNCTION public.enforce_commerce_route_event_integrity()
	RETURNS TRIGGER AS $$
	DECLARE
		v_route_offer_id UUID;
	BEGIN
		SELECT offer_id
		INTO v_route_offer_id
		FROM public.commerce_routes
		WHERE id = NEW.route_id
		  AND deleted_at IS NULL
		  AND is_active = TRUE;

		IF NOT FOUND THEN
			RAISE EXCEPTION 'commerce route "%" does not exist, is deleted, or is inactive', NEW.route_id;
		END IF;

		IF NEW.offer_id <> v_route_offer_id THEN
			RAISE EXCEPTION 'commerce route event offer "%" does not match route offer "%"', NEW.offer_id, v_route_offer_id;
		END IF;

		PERFORM public.ensure_offer_type(NEW.offer_id, 'deal');

		RETURN NEW;
	END;
	$$ LANGUAGE plpgsql;

	DROP TRIGGER IF EXISTS enforce_commerce_route_event_integrity
		ON public.commerce_route_events;

	CREATE TRIGGER enforce_commerce_route_event_integrity
	BEFORE INSERT OR UPDATE OF route_id, offer_id
	ON public.commerce_route_events
	FOR EACH ROW EXECUTE FUNCTION public.enforce_commerce_route_event_integrity();

	CREATE TABLE IF NOT EXISTS merchant_direct_attribution_configs (
		id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
		merchant_id UUID NOT NULL REFERENCES merchants(id) ON DELETE CASCADE,
		attribution_method TEXT NOT NULL CHECK (attribution_method IN (
			'route_token',
			'utm',
			'merchant_reported',
			'postback',
			'coupon_code'
		)),
		attribution_window_days INTEGER NOT NULL DEFAULT 30 CHECK (attribution_window_days > 0),
		config JSONB NOT NULL DEFAULT '{}'::jsonb,
		is_active BOOLEAN NOT NULL DEFAULT TRUE,
		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		deleted_at TIMESTAMPTZ
	);

	-- ===============================================================
	-- DEFERRED: Future Offering Watch Density Snapshots
	-- Non-v1 launch watch density proof metric infrastructure.
	-- Keep schema compile-safe, but do not expand routes, services, UI,
	-- handlers, or tests for Future Offering v1.
	-- ===============================================================
	CREATE TABLE IF NOT EXISTS future_offering_watch_density_snapshots (
		id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

		future_offering_id UUID NOT NULL REFERENCES merchant_future_offerings(id) ON DELETE CASCADE,
		merchant_id UUID NOT NULL REFERENCES merchants(id) ON DELETE CASCADE,

		watch_count INTEGER NOT NULL DEFAULT 0 CHECK (watch_count >= 0),
		active_watch_count INTEGER NOT NULL DEFAULT 0 CHECK (active_watch_count >= 0),
		muted_watch_count INTEGER NOT NULL DEFAULT 0 CHECK (muted_watch_count >= 0),

		snapshot_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

		CONSTRAINT chk_future_offering_watch_density_counts
			CHECK (watch_count >= active_watch_count AND watch_count >= muted_watch_count)
	);

	CREATE INDEX IF NOT EXISTS idx_future_offering_watch_density_future_offering
		ON future_offering_watch_density_snapshots(future_offering_id, snapshot_at DESC);

	CREATE INDEX IF NOT EXISTS idx_future_offering_watch_density_merchant
		ON future_offering_watch_density_snapshots(merchant_id, snapshot_at DESC);

	-- ===============================================================
	-- DEFERRED: Offer Ratings
	-- Non-v1 offer review and rating infrastructure.
	-- Keep schema compile-safe, but do not expand routes, services, UI,
	-- handlers, or tests for Future Offering v1.
	-- ===============================================================
	CREATE TABLE IF NOT EXISTS offer_ratings (
		id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
		user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		offer_id UUID NOT NULL REFERENCES offers(id) ON DELETE CASCADE,
		rating INTEGER NOT NULL CHECK (rating BETWEEN 1 AND 5),
		review TEXT,
		deleted_at TIMESTAMPTZ,
		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
	);

	CREATE UNIQUE INDEX IF NOT EXISTS ux_offer_ratings_active_user_offer
		ON offer_ratings(user_id, offer_id)
		WHERE deleted_at IS NULL;

	-- ===============================================================
	-- DEFERRED: Affiliate Performance
	-- Non-v1 aggregate affiliate click and revenue tracking infrastructure.
	-- Keep schema compile-safe, but do not expand routes, services, UI,
	-- handlers, or tests for Future Offering v1.
	-- ===============================================================
	CREATE TABLE IF NOT EXISTS affiliate_performance (
		id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
		offer_id UUID NOT NULL UNIQUE REFERENCES offers(id) ON DELETE CASCADE,
		total_clicks INT NOT NULL DEFAULT 0 CHECK (total_clicks >= 0),
		estimated_revenue NUMERIC(19,4) NOT NULL DEFAULT 0.00 CHECK (estimated_revenue >= 0),
		conversion_rate NUMERIC(7,4) NOT NULL DEFAULT 0 CHECK (conversion_rate >= 0),
		avg_order_value NUMERIC(19,4) NOT NULL DEFAULT 0 CHECK (avg_order_value >= 0),
		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
	);

	CREATE INDEX IF NOT EXISTS idx_affiliate_performance_updated_at
		ON affiliate_performance(updated_at DESC);

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
		offer_id UUID REFERENCES offers(id) ON DELETE SET NULL,
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

	CREATE INDEX IF NOT EXISTS idx_user_notifications_offer_id
		ON user_notifications(offer_id)
		WHERE deleted_at IS NULL AND offer_id IS NOT NULL;

	CREATE INDEX IF NOT EXISTS idx_user_notifications_type_id
		ON user_notifications(notification_type_id)
		WHERE deleted_at IS NULL;

	CREATE TABLE IF NOT EXISTS audit_logs_archive (
		id UUID PRIMARY KEY,
		user_id UUID REFERENCES users(id) ON DELETE SET NULL,
		action_id UUID NOT NULL REFERENCES actions(id) ON DELETE RESTRICT,
		entity_type_id UUID NOT NULL REFERENCES entity_types(id) ON DELETE RESTRICT,
		entity_id TEXT NOT NULL,
		occurred_at TIMESTAMPTZ NOT NULL,
		archived_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		CONSTRAINT audit_logs_archive_entity_composite_key UNIQUE (entity_type_id, entity_id, action_id, occurred_at)
	);

	CREATE INDEX IF NOT EXISTS idx_audit_logs_archive_entity ON audit_logs_archive(entity_type_id, entity_id);
	CREATE INDEX IF NOT EXISTS idx_audit_logs_archive_user ON audit_logs_archive(user_id);
	CREATE INDEX IF NOT EXISTS idx_audit_logs_archive_timestamp ON audit_logs_archive(occurred_at DESC, entity_type_id);
	CREATE INDEX IF NOT EXISTS idx_audit_logs_archive_archived_at ON audit_logs_archive(archived_at);

	-- ===============================================================
	-- DEFERRED: Sponsorships / Promotions
	-- Non-v1 offer sponsorship, bidding, and merchant promotion infrastructure.
	-- Keep schema compile-safe, but do not expand routes, services, UI,
	-- handlers, or tests for Future Offering v1.
	-- ===============================================================
	CREATE TABLE IF NOT EXISTS sponsorship_bid_types (
		id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
		code TEXT NOT NULL UNIQUE CHECK (code IN ('CPD','CPC','CPI')),
		name TEXT NOT NULL,
		description TEXT NOT NULL,
		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
	);

	CREATE TABLE IF NOT EXISTS offer_sponsorships (
		id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
		offer_id UUID NOT NULL REFERENCES offers(id) ON DELETE CASCADE,
		merchant_id UUID NOT NULL REFERENCES merchants(id) ON DELETE CASCADE,
		start_date TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		end_date TIMESTAMPTZ NOT NULL,
		sponsorship_bid_type_id UUID NOT NULL REFERENCES sponsorship_bid_types(id) ON DELETE RESTRICT,
		bid_amount NUMERIC(19,4) NOT NULL CHECK (bid_amount > 0),
		max_budget NUMERIC(19,4),
		budget_spent NUMERIC(19,4) NOT NULL DEFAULT 0.00 CHECK (budget_spent >= 0),
		impressions_served INT NOT NULL DEFAULT 0 CHECK (impressions_served >= 0),
		clicks_served INT NOT NULL DEFAULT 0 CHECK (clicks_served >= 0),
		deleted_at TIMESTAMPTZ,
		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		CONSTRAINT chk_offer_sponsorship_dates CHECK (end_date > start_date),
		CONSTRAINT chk_offer_sponsorship_budget CHECK (max_budget IS NULL OR max_budget >= 0),
		CONSTRAINT chk_offer_sponsorship_budget_spent CHECK (max_budget IS NULL OR budget_spent <= max_budget)
	);

	CREATE TABLE IF NOT EXISTS sponsorship_bid_minimums (
		id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
		bid_type_id UUID NOT NULL UNIQUE REFERENCES sponsorship_bid_types(id) ON DELETE CASCADE,
		min_bid_amount NUMERIC(19,4) NOT NULL CHECK (min_bid_amount > 0),
		min_max_budget NUMERIC(19,4) NOT NULL CHECK (min_max_budget > 0),
		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
	);

	CREATE INDEX IF NOT EXISTS idx_offer_sponsorships_active_id
		ON offer_sponsorships(id)
		WHERE deleted_at IS NULL;

	CREATE INDEX IF NOT EXISTS idx_offer_sponsorships_offer_id_created_at
		ON offer_sponsorships(offer_id, created_at DESC);

	CREATE INDEX IF NOT EXISTS idx_offer_sponsorships_merchant_id_created_at
		ON offer_sponsorships(merchant_id, created_at DESC);

	CREATE INDEX IF NOT EXISTS idx_offer_sponsorships_bid_type_offer_dates
		ON offer_sponsorships(sponsorship_bid_type_id, offer_id, start_date, end_date);

	CREATE OR REPLACE FUNCTION public.enforce_offer_sponsorship_integrity()
	RETURNS TRIGGER AS $$
	DECLARE
		v_offer_type TEXT;
		v_offer_merchant_id UUID;
	BEGIN
		SELECT type, merchant_id
		INTO v_offer_type, v_offer_merchant_id
		FROM public.offers
		WHERE id = NEW.offer_id
		  AND deleted_at IS NULL;

		IF NOT FOUND THEN
			RAISE EXCEPTION 'offer "%" does not exist or is deleted', NEW.offer_id;
		END IF;

		IF v_offer_type <> 'deal' THEN
			RAISE EXCEPTION 'offer "%" is type "%", expected "deal"', NEW.offer_id, v_offer_type;
		END IF;

		IF NEW.merchant_id <> v_offer_merchant_id THEN
			RAISE EXCEPTION 'offer sponsorship merchant "%" does not match offer merchant "%"', NEW.merchant_id, v_offer_merchant_id;
		END IF;

		RETURN NEW;
	END;
	$$ LANGUAGE plpgsql;

	DROP TRIGGER IF EXISTS enforce_offer_sponsorship_integrity ON public.offer_sponsorships;
	CREATE TRIGGER enforce_offer_sponsorship_integrity
	BEFORE INSERT OR UPDATE OF offer_id, merchant_id
	ON public.offer_sponsorships
	FOR EACH ROW EXECUTE FUNCTION public.enforce_offer_sponsorship_integrity();

	CREATE TABLE IF NOT EXISTS promotions (
		id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
		name TEXT NOT NULL UNIQUE,
		description TEXT NOT NULL,
		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
	);

	CREATE TABLE IF NOT EXISTS merchant_promotions (
		id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
		merchant_id UUID NOT NULL REFERENCES merchants(id) ON DELETE CASCADE,
		promotion_id UUID NOT NULL REFERENCES promotions(id) ON DELETE RESTRICT,
		storewide BOOLEAN NOT NULL DEFAULT FALSE,
		start_date TIMESTAMPTZ NOT NULL,
		end_date TIMESTAMPTZ NOT NULL,
		deleted_at TIMESTAMPTZ,
		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		CONSTRAINT chk_merchant_promotions_dates CHECK (end_date > start_date)
	);

	CREATE TABLE IF NOT EXISTS merchant_promotion_offers (
		merchant_promotion_id UUID NOT NULL REFERENCES merchant_promotions(id) ON DELETE CASCADE,
		offer_id UUID NOT NULL REFERENCES offers(id) ON DELETE CASCADE,
		PRIMARY KEY (merchant_promotion_id, offer_id)
	);

	CREATE INDEX IF NOT EXISTS idx_merchant_promotions_active_merchant_dates
		ON merchant_promotions(merchant_id, start_date, end_date)
		WHERE deleted_at IS NULL;

	CREATE INDEX IF NOT EXISTS idx_merchant_promotions_active_promotion_dates
		ON merchant_promotions(promotion_id, start_date, end_date)
		WHERE deleted_at IS NULL;

	CREATE INDEX IF NOT EXISTS idx_merchant_promotions_active_storewide_dates
		ON merchant_promotions(merchant_id, start_date, end_date)
		WHERE deleted_at IS NULL AND storewide = TRUE;

	CREATE INDEX IF NOT EXISTS idx_merchant_promotion_offers_offer_id
		ON merchant_promotion_offers(offer_id);

	CREATE INDEX IF NOT EXISTS idx_merchant_promotion_offers_promotion_id
		ON merchant_promotion_offers(merchant_promotion_id);

	CREATE TABLE IF NOT EXISTS reasons (
		id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
		name TEXT NOT NULL UNIQUE,
		description TEXT NOT NULL DEFAULT '',
		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
	);

	-- ===============================================================
	-- Auth / OAuth
	-- ===============================================================
	CREATE TABLE IF NOT EXISTS password_resets (
		id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
		user_id UUID NOT NULL UNIQUE REFERENCES users(id) ON DELETE CASCADE,
		reset_token_hash TEXT UNIQUE NOT NULL,
		expires_at TIMESTAMPTZ NOT NULL,
		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
	);

	-- ===============================================================
	-- DEFERRED: OAuth Clients / Authorization Codes / User Consent
	-- Non-v1 Platform-as-OAuth-provider infrastructure (Layer 2.2.b).
	-- Keep schema compile-safe, but do not expand routes, services, UI,
	-- handlers, or tests for Future Offering v1.
	-- ===============================================================
	CREATE TABLE IF NOT EXISTS oauth_clients (
		id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
		client_id TEXT NOT NULL UNIQUE,
		client_secret_hash TEXT NOT NULL,
		is_active BOOLEAN NOT NULL DEFAULT TRUE,
		allowed_redirect_uris TEXT[] NOT NULL DEFAULT '{}',
		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
	);

	CREATE UNIQUE INDEX IF NOT EXISTS idx_oauth_clients_client_id
		ON oauth_clients(client_id);

	CREATE INDEX IF NOT EXISTS idx_oauth_clients_redirect_uris_gin
		ON oauth_clients USING GIN (allowed_redirect_uris);

	CREATE TABLE IF NOT EXISTS oauth_authorization_codes (
		id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
		client_id UUID NOT NULL REFERENCES oauth_clients(id) ON DELETE CASCADE,
		user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		code_hash CHAR(64) NOT NULL,
		redirect_uri TEXT NOT NULL,
		expires_at TIMESTAMPTZ NOT NULL,
		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		revoked_at TIMESTAMPTZ NULL
	);

	CREATE UNIQUE INDEX IF NOT EXISTS uniq_oauth_code_hash
		ON oauth_authorization_codes(code_hash);

	CREATE TABLE IF NOT EXISTS oauth_user_consent (
		id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
		user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		client_id UUID NOT NULL REFERENCES oauth_clients(id) ON DELETE CASCADE,
		scopes TEXT[] NOT NULL DEFAULT ARRAY[]::TEXT[],
		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		expires_at TIMESTAMPTZ NOT NULL,
		revoked BOOLEAN NOT NULL DEFAULT FALSE,
		revoked_at TIMESTAMPTZ NULL,
		CONSTRAINT chk_oauth_user_consent_expiry CHECK (expires_at > created_at)
	);

	CREATE INDEX IF NOT EXISTS idx_oauth_user_consent_user_id ON oauth_user_consent(user_id);
	CREATE INDEX IF NOT EXISTS idx_oauth_user_consent_client_id ON oauth_user_consent(client_id);
	CREATE UNIQUE INDEX IF NOT EXISTS ux_oauth_user_consent_active
		ON oauth_user_consent(user_id, client_id)
		WHERE revoked = FALSE;


	-- ===============================================================
	-- Merchant Accounts / Solo & Multi-User Merchant Access
	-- ===============================================================
	CREATE TABLE IF NOT EXISTS merchant_accounts (
		id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
		merchant_id UUID NOT NULL UNIQUE REFERENCES merchants(id) ON DELETE CASCADE,

		account_status TEXT NOT NULL DEFAULT 'pending'
			CHECK (account_status IN ('pending', 'active', 'suspended', 'closed')),

		onboarded_at TIMESTAMPTZ,
		initial_plan_id UUID REFERENCES merchant_program_plans(id) ON DELETE SET NULL,

		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		deleted_at TIMESTAMPTZ
	);

	CREATE TABLE IF NOT EXISTS merchant_account_roles (
		id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
		code TEXT UNIQUE NOT NULL CHECK (code IN (
			'owner',
			'admin',
			'billing',
			'campaign_manager',
			'viewer'
		)),
		name TEXT NOT NULL,
		description TEXT NOT NULL DEFAULT '',
		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
	);

	CREATE TABLE IF NOT EXISTS merchant_account_members (
		id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
		merchant_account_id UUID NOT NULL REFERENCES merchant_accounts(id) ON DELETE CASCADE,
		user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		role_id UUID NOT NULL REFERENCES merchant_account_roles(id) ON DELETE RESTRICT,

		relationship_type TEXT NOT NULL DEFAULT 'employee'
			CHECK (relationship_type IN ('owner', 'employee', 'external_agent')),

		status TEXT NOT NULL DEFAULT 'active'
			CHECK (status IN ('invited', 'active', 'suspended', 'removed')),

		invited_by UUID REFERENCES users(id) ON DELETE SET NULL,
		invited_at TIMESTAMPTZ,

		joined_at TIMESTAMPTZ,

		removed_by UUID REFERENCES users(id) ON DELETE SET NULL,
		removed_at TIMESTAMPTZ,

		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

		UNIQUE (merchant_account_id, user_id),

		CONSTRAINT chk_merchant_account_members_removed_state
			CHECK (
				(status = 'removed' AND removed_at IS NOT NULL)
				OR
				(status <> 'removed' AND removed_at IS NULL AND removed_by IS NULL)
			),

		CONSTRAINT chk_merchant_account_members_joined_at
			CHECK (
				status = 'invited'
				OR joined_at IS NOT NULL
			),

		CONSTRAINT chk_merchant_account_members_invited_state
			CHECK (
				(status = 'invited' AND invited_at IS NOT NULL)
				OR
				(status <> 'invited')
			),

		CONSTRAINT chk_merchant_account_members_owner_relationship
			CHECK (
				relationship_type <> 'owner'
				OR status <> 'invited'
			)
	);

	CREATE INDEX IF NOT EXISTS idx_merchant_account_members_user_status
		ON merchant_account_members(user_id, status);

	CREATE INDEX IF NOT EXISTS idx_merchant_account_members_account_status
		ON merchant_account_members(merchant_account_id, status);

	CREATE INDEX IF NOT EXISTS idx_merchant_account_members_relationship_type
		ON merchant_account_members(merchant_account_id, relationship_type);

	-- ===============================================================
	-- Program Fee Schedules
	-- CE patch 8: added missing comma after chk_fee_schedule_hybrid_requires_both,
	-- and tightened chk_fee_schedule_type_interval so adjustment_fee/refund/reversal
	-- are constrained to billing_interval = 'event'.
	-- ===============================================================

	-- Merchant Program Fee Schedules
	CREATE TABLE IF NOT EXISTS merchant_program_fee_schedules (
		id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

		fee_scope TEXT NOT NULL DEFAULT 'plan'
			CHECK (fee_scope IN ('global', 'plan')),

		plan_id UUID
			REFERENCES merchant_program_plans(id)
			ON DELETE RESTRICT,

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

		included_seats INTEGER NOT NULL DEFAULT 1
			CHECK (included_seats >= 1),

		extra_seat_fee NUMERIC(19,4)
			CHECK (
				extra_seat_fee IS NULL
				OR extra_seat_fee >= 0
			),

		currency CHAR(3) NOT NULL DEFAULT 'USD'
			CHECK (currency ~ '^[A-Z]{3}$'),

		is_active BOOLEAN NOT NULL DEFAULT TRUE,

		effective_from TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		effective_to TIMESTAMPTZ,

		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		deleted_at TIMESTAMPTZ,

		CONSTRAINT chk_fee_schedule_scope_plan
			CHECK (
				(
					fee_scope = 'global'
					AND plan_id IS NULL
				)
				OR
				(
					fee_scope = 'plan'
					AND plan_id IS NOT NULL
				)
			),

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
				fee_scope WITH =,

				(
					COALESCE(
						plan_id,
						'00000000-0000-0000-0000-000000000000'::uuid
					)
				) WITH =,

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

	CREATE INDEX IF NOT EXISTS idx_merchant_program_fee_schedules_plan
		ON merchant_program_fee_schedules (
			plan_id,
			fee_type_id,
			billing_interval,
			effective_from DESC
		)
		WHERE (
			fee_scope = 'plan'
			AND deleted_at IS NULL
		);

	CREATE INDEX IF NOT EXISTS idx_merchant_program_fee_schedules_global
		ON merchant_program_fee_schedules (
			fee_type_id,
			billing_interval,
			effective_from DESC
		)
		WHERE (
			fee_scope = 'global'
			AND plan_id IS NULL
			AND deleted_at IS NULL
		);

	CREATE INDEX IF NOT EXISTS idx_merchant_program_fee_schedules_effective
		ON merchant_program_fee_schedules (
			fee_scope,
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
			REFERENCES merchants(id)
			ON DELETE RESTRICT,

		future_offering_event_id UUID
			REFERENCES merchant_future_offerings_events(id)
			ON DELETE RESTRICT,

		subscription_period_id UUID
			CONSTRAINT fk_merchant_billable_events_subscription_period
			REFERENCES merchant_program_subscription_periods(id)
			ON DELETE RESTRICT,

		engagement_event_id UUID
			REFERENCES user_trend_engagement_events(id)
			ON DELETE RESTRICT,

		billable_event_type TEXT NOT NULL
			CHECK (billable_event_type IN (
				'activation',
				'subscription_period',
				'watch',
				'waitlist',
				'early_access_request',
				'beta',
				'reservation_interest',
				'preorder_intent'
			)),

		gross_event_value NUMERIC(19,4)
			CHECK (
				gross_event_value IS NULL
				OR gross_event_value >= 0
			),

		currency CHAR(3)
			CHECK (
				currency IS NULL
				OR currency ~ '^[A-Z]{3}$'
			),

		occurred_at TIMESTAMPTZ NOT NULL,

		confirmed_at TIMESTAMPTZ,
		rejected_at TIMESTAMPTZ,
		reversed_at TIMESTAMPTZ,

		status TEXT NOT NULL DEFAULT 'pending'
			CHECK (status IN (
				'pending',
				'confirmed',
				'rejected',
				'reversed'
			)),

		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

		CONSTRAINT chk_merchant_billable_events_source_count
			CHECK (
				num_nonnulls(
					future_offering_event_id,
					subscription_period_id,
					engagement_event_id
				) = 1
			),

		CONSTRAINT chk_merchant_billable_events_source_type
			CHECK (
				(
					billable_event_type = 'activation'
					AND future_offering_event_id IS NOT NULL
				)
				OR
				(
					billable_event_type = 'subscription_period'
					AND subscription_period_id IS NOT NULL
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

	CREATE UNIQUE INDEX IF NOT EXISTS uq_merchant_billable_events_future_offering_event
		ON merchant_billable_events (future_offering_event_id)
		WHERE future_offering_event_id IS NOT NULL;

	CREATE UNIQUE INDEX IF NOT EXISTS uq_merchant_billable_events_subscription_period
		ON merchant_billable_events (subscription_period_id)
		WHERE subscription_period_id IS NOT NULL;

	CREATE UNIQUE INDEX IF NOT EXISTS uq_merchant_billable_events_engagement_event
		ON merchant_billable_events (engagement_event_id)
		WHERE engagement_event_id IS NOT NULL;

	CREATE INDEX IF NOT EXISTS idx_merchant_billable_events_merchant_occurred
		ON merchant_billable_events (
			merchant_id,
			occurred_at DESC,
			id DESC
		);

	CREATE INDEX IF NOT EXISTS idx_merchant_billable_events_merchant_status_occurred
		ON merchant_billable_events (
			merchant_id,
			status,
			occurred_at DESC,
			id DESC
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
	-- DEFERRED: Settlement
	-- Non-v1 merchant fee settlement batch and invoice item infrastructure.
	-- Keep schema compile-safe, but do not expand routes, services, UI,
	-- handlers, or tests for Future Offering v1.
	-- ===============================================================
	CREATE TABLE IF NOT EXISTS merchant_settlement_batches (
		id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
		merchant_id UUID NOT NULL REFERENCES merchants(id) ON DELETE CASCADE,

		settlement_status TEXT NOT NULL DEFAULT 'pending'
			CHECK (settlement_status IN ('pending', 'invoiced', 'settled', 'disputed', 'cancelled')),

		-- Snapshot total preserved for invoice/settlement integrity.
		total_fee_amount NUMERIC(19,4) NOT NULL DEFAULT 0 CHECK (total_fee_amount >= 0),

		currency CHAR(3) NOT NULL DEFAULT 'USD'
			CHECK (currency ~ '^[A-Z]{3}$'),

		period_start TIMESTAMPTZ NOT NULL,
		period_end TIMESTAMPTZ NOT NULL,

		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		settled_at TIMESTAMPTZ,

		CONSTRAINT chk_merchant_settlement_period
			CHECK (period_end > period_start),

		CONSTRAINT chk_merchant_settlement_batches_settled_at
			CHECK (
				settlement_status <> 'settled'
				OR settled_at IS NOT NULL
			)
	);

	CREATE TABLE IF NOT EXISTS merchant_settlement_batch_items (
		settlement_batch_id UUID NOT NULL REFERENCES merchant_settlement_batches(id) ON DELETE CASCADE,
		fee_calculation_id UUID NOT NULL REFERENCES merchant_fee_calculations(id) ON DELETE RESTRICT,
		PRIMARY KEY (settlement_batch_id, fee_calculation_id)
	);

	-- ===============================================================
	-- Billing Indexes
	-- ===============================================================

	CREATE INDEX IF NOT EXISTS idx_merchant_accounts_status
		ON merchant_accounts(account_status)
		WHERE deleted_at IS NULL;

	CREATE INDEX IF NOT EXISTS idx_merchant_program_fee_schedules_plan_fee
		ON merchant_program_fee_schedules(plan_id, fee_type, billing_interval)
		WHERE deleted_at IS NULL AND is_active = TRUE;

	CREATE INDEX IF NOT EXISTS idx_merchant_program_fee_schedules_global_fee
		ON merchant_program_fee_schedules(fee_type, billing_interval)
		WHERE deleted_at IS NULL
		  AND is_active = TRUE
		  AND fee_scope = 'global';

	CREATE INDEX IF NOT EXISTS idx_merchant_billing_ledger_entries_merchant
		ON merchant_billing_ledger_entries(merchant_id, created_at DESC);

	CREATE INDEX IF NOT EXISTS idx_merchant_billing_ledger_entries_reference
		ON merchant_billing_ledger_entries(reference_type, reference_id)
		WHERE reference_type IS NOT NULL AND reference_id IS NOT NULL;

	CREATE INDEX IF NOT EXISTS idx_merchant_billable_events_merchant_status
		ON merchant_billable_events(merchant_id, status, created_at DESC);

	CREATE INDEX IF NOT EXISTS idx_merchant_billable_events_route_event
		ON merchant_billable_events(commerce_route_event_id)
		WHERE commerce_route_event_id IS NOT NULL;

	CREATE INDEX IF NOT EXISTS idx_merchant_billable_events_coupon_usage
		ON merchant_billable_events(coupon_usage_id)
		WHERE coupon_usage_id IS NOT NULL;

	CREATE INDEX IF NOT EXISTS idx_merchant_fee_calculations_merchant_status
		ON merchant_fee_calculations(merchant_id, status, calculated_at DESC);

	CREATE INDEX IF NOT EXISTS idx_merchant_settlement_batches_merchant_status
		ON merchant_settlement_batches(merchant_id, settlement_status, created_at DESC);

	CREATE INDEX IF NOT EXISTS idx_merchant_settlement_batch_items_fee_calculation
		ON merchant_settlement_batch_items(fee_calculation_id);

	-- ===============================================================
	-- DEFERRED: Merchant Attribution Matches
	-- Non-v1 proprietary attribution proof and conflict resolution infrastructure.
	-- Keep schema compile-safe, but do not expand routes, services, UI,
	-- handlers, or tests for Future Offering v1.
	-- ===============================================================
	CREATE TABLE IF NOT EXISTS merchant_attribution_matches (
		id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

		merchant_id UUID NOT NULL REFERENCES merchants(id) ON DELETE CASCADE,

		commerce_route_event_id UUID
			REFERENCES commerce_route_events(id) ON DELETE RESTRICT,

		billable_event_id UUID NOT NULL
			REFERENCES merchant_billable_events(id) ON DELETE CASCADE,

		attribution_config_id UUID
			REFERENCES merchant_direct_attribution_configs(id) ON DELETE SET NULL,

		coupon_usage_id UUID
			REFERENCES coupon_usages(id) ON DELETE SET NULL,

		attribution_method TEXT NOT NULL CHECK (attribution_method IN (
			'route_token',
			'utm',
			'coupon_code',
			'postback',
			'merchant_reported'
		)),

		attribution_window_days INTEGER NOT NULL CHECK (attribution_window_days >= 0),
		days_to_conversion INTEGER NOT NULL CHECK (days_to_conversion >= 0),

		match_confidence NUMERIC(5,2)
			CHECK (
				match_confidence IS NULL
				OR (match_confidence >= 0 AND match_confidence <= 100)
			),

		conflict_resolution_reason TEXT,
		is_primary BOOLEAN NOT NULL DEFAULT TRUE,

		matched_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

		CONSTRAINT chk_attribution_match_route_event_required
			CHECK (
				attribution_method = 'merchant_reported'
				OR commerce_route_event_id IS NOT NULL
			),

		CONSTRAINT chk_attribution_match_within_window
			CHECK (days_to_conversion <= attribution_window_days),

		CONSTRAINT chk_attribution_match_coupon_usage_method
			CHECK (
				attribution_method <> 'coupon_code'
				OR coupon_usage_id IS NOT NULL
			),

		CONSTRAINT chk_attribution_match_conflict_reason
			CHECK (
				is_primary = TRUE
				OR conflict_resolution_reason IS NOT NULL
			)
	);

	CREATE UNIQUE INDEX IF NOT EXISTS ux_merchant_attribution_matches_primary
		ON merchant_attribution_matches(billable_event_id)
		WHERE is_primary = TRUE;

	CREATE INDEX IF NOT EXISTS idx_merchant_attribution_matches_merchant
		ON merchant_attribution_matches(merchant_id);

	CREATE INDEX IF NOT EXISTS idx_merchant_attribution_matches_route_event
		ON merchant_attribution_matches(commerce_route_event_id)
		WHERE commerce_route_event_id IS NOT NULL;

	CREATE INDEX IF NOT EXISTS idx_merchant_attribution_matches_billable_event
		ON merchant_attribution_matches(billable_event_id);


	-- ===============================================================
	-- DEFERRED: Merchant Postback Infrastructure
	-- Non-v1 merchant postback config and event validation infrastructure.
	-- Keep schema compile-safe, but do not expand routes, services, UI,
	-- handlers, or tests for Future Offering v1.
	-- ===============================================================
	CREATE TABLE IF NOT EXISTS merchant_postback_configs (
		id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

		merchant_id UUID NOT NULL REFERENCES merchants(id) ON DELETE CASCADE,

		endpoint_url TEXT NOT NULL CHECK (endpoint_url ~* '^https?://'),

		authentication_method TEXT NOT NULL CHECK (authentication_method IN (
			'secret_token',
			'api_key',
			'hmac_signature',
			'none'
		)),

		secret_hash TEXT,

		is_active BOOLEAN NOT NULL DEFAULT TRUE,

		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		deleted_at TIMESTAMPTZ,

		CONSTRAINT chk_postback_config_secret_required
			CHECK (
				authentication_method = 'none'
				OR secret_hash IS NOT NULL
			)
	);

	CREATE INDEX IF NOT EXISTS idx_merchant_postback_configs_merchant
		ON merchant_postback_configs(merchant_id)
		WHERE is_active = TRUE
		  AND deleted_at IS NULL;


	CREATE TABLE IF NOT EXISTS merchant_postback_events (
		id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

		merchant_id UUID NOT NULL REFERENCES merchants(id) ON DELETE CASCADE,

		postback_config_id UUID
			REFERENCES merchant_postback_configs(id) ON DELETE SET NULL,

		commerce_route_event_id UUID
			REFERENCES commerce_route_events(id) ON DELETE SET NULL,

		payload JSONB NOT NULL,

		validation_status TEXT NOT NULL DEFAULT 'pending'
			CHECK (validation_status IN ('pending', 'accepted', 'rejected')),

		validated_at TIMESTAMPTZ,
		rejection_reason TEXT,

		received_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

		CONSTRAINT chk_postback_event_validated_at
			CHECK (
				validation_status = 'pending'
				OR validated_at IS NOT NULL
			),

		CONSTRAINT chk_postback_event_rejection_reason
			CHECK (
				validation_status <> 'rejected'
				OR rejection_reason IS NOT NULL
			)
	);

	CREATE INDEX IF NOT EXISTS idx_merchant_postback_events_merchant
		ON merchant_postback_events(merchant_id, received_at DESC);

	CREATE INDEX IF NOT EXISTS idx_merchant_postback_events_route_event
		ON merchant_postback_events(commerce_route_event_id)
		WHERE commerce_route_event_id IS NOT NULL;


	-- ===============================================================
	-- DEFERRED: Fee Reversal Causality
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
	-- DEFERRED: Merchant Payment Methods
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
	-- DEFERRED: Merchant Invoices / Invoice Items
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
	-- DEFERRED: Merchant Payments / Receipts
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


	-- ===============================================================
	-- DEFERRED: Affiliate Feed Sources
	-- Non-v1 deals ingestion and affiliate feed source infrastructure.
	-- Keep schema compile-safe, but do not expand routes, services, UI,
	-- handlers, or tests for Future Offering v1.
	-- ===============================================================
	CREATE TABLE IF NOT EXISTS affiliate_feed_sources (
		id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

		merchant_id UUID
			REFERENCES merchants(id)
			ON DELETE SET NULL,

		source_name TEXT NOT NULL,

		source_type TEXT NOT NULL CHECK (source_type IN (
			'amazon_associates',
			'csv',
			'json',
			'xml',
			'rss',
			'api'
		)),

		feed_url TEXT
			CHECK (
				feed_url IS NULL
				OR feed_url ~* '^https?://'
			),

		auth_method TEXT NOT NULL DEFAULT 'none'
			CHECK (auth_method IN (
				'none',
				'api_key',
				'oauth',
				'basic_auth',
				'bearer_token'
			)),

		credentials_encrypted BYTEA,
		credentials_key_id TEXT,
		credentials_last_rotated_at TIMESTAMPTZ,

		mapping_config JSONB NOT NULL DEFAULT '{}'::jsonb,

		sync_frequency_minutes INTEGER NOT NULL DEFAULT 1440
			CHECK (sync_frequency_minutes > 0),

		last_synced_at TIMESTAMPTZ,
		is_active BOOLEAN NOT NULL DEFAULT TRUE,

		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		deleted_at TIMESTAMPTZ,

		CONSTRAINT chk_feed_source_credentials
			CHECK (
				auth_method = 'none'
				OR
				(
					credentials_encrypted IS NOT NULL
					AND credentials_key_id IS NOT NULL
				)
			),

		CONSTRAINT chk_feed_source_credentials_none
			CHECK (
				auth_method <> 'none'
				OR
				(
					credentials_encrypted IS NULL
					AND credentials_key_id IS NULL
					AND credentials_last_rotated_at IS NULL
				)
			)
	);

	CREATE INDEX IF NOT EXISTS idx_affiliate_feed_sources_active
		ON affiliate_feed_sources(source_type, is_active)
		WHERE deleted_at IS NULL;


	-- ===============================================================
	-- DEFERRED: Affiliate Feed Imports
	-- Non-v1 affiliate feed import run tracking infrastructure.
	-- Keep schema compile-safe, but do not expand routes, services, UI,
	-- handlers, or tests for Future Offering v1.
	-- ===============================================================
	CREATE TABLE IF NOT EXISTS affiliate_feed_imports (
		id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

		source_id UUID NOT NULL
			REFERENCES affiliate_feed_sources(id)
			ON DELETE CASCADE,

		import_status TEXT NOT NULL DEFAULT 'pending'
			CHECK (import_status IN (
				'pending',
				'running',
				'completed',
				'partial_success',
				'failed'
			)),

		records_received INTEGER NOT NULL DEFAULT 0
			CHECK (records_received >= 0),

		offers_created INTEGER NOT NULL DEFAULT 0
			CHECK (offers_created >= 0),

		offers_updated INTEGER NOT NULL DEFAULT 0
			CHECK (offers_updated >= 0),

		offers_failed INTEGER NOT NULL DEFAULT 0
			CHECK (offers_failed >= 0),

		started_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		completed_at TIMESTAMPTZ,
		error_summary TEXT,

		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

		CONSTRAINT chk_affiliate_feed_import_completion
			CHECK (
				import_status IN ('pending', 'running')
				OR completed_at IS NOT NULL
			)
	);

	CREATE INDEX IF NOT EXISTS idx_affiliate_feed_imports_source
		ON affiliate_feed_imports(source_id, started_at DESC);


	-- ===============================================================
	-- DEFERRED: Affiliate Feed Import Errors
	-- Non-v1 affiliate feed import error capture infrastructure.
	-- Keep schema compile-safe, but do not expand routes, services, UI,
	-- handlers, or tests for Future Offering v1.
	-- ===============================================================
	CREATE TABLE IF NOT EXISTS affiliate_feed_import_errors (
		id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

		import_id UUID NOT NULL
			REFERENCES affiliate_feed_imports(id)
			ON DELETE CASCADE,

		source_record_reference TEXT,
		error_message TEXT NOT NULL,

		raw_payload JSONB,

		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
	);

	CREATE INDEX IF NOT EXISTS idx_affiliate_feed_import_errors_import
		ON affiliate_feed_import_errors(import_id);


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
	-- updated_at triggers
	-- CE patches 9 & 10: all inline duplicate triggers removed from
	-- their table sections above. Only this canonical consolidated
	-- section fires set_updated_at on each table.
	-- merchant_future_offerings_assets added (patch 10).
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

	DROP TRIGGER IF EXISTS set_updated_at_user_wallets ON user_wallets;
	CREATE TRIGGER set_updated_at_user_wallets
	BEFORE UPDATE ON user_wallets
	FOR EACH ROW EXECUTE FUNCTION public.set_updated_at();

	DROP TRIGGER IF EXISTS set_updated_at_user_dashboards ON user_dashboards;
	CREATE TRIGGER set_updated_at_user_dashboards
	BEFORE UPDATE ON user_dashboards
	FOR EACH ROW EXECUTE FUNCTION public.set_updated_at();

	DROP TRIGGER IF EXISTS set_updated_at_user_favorites ON user_favorites;
	CREATE TRIGGER set_updated_at_user_favorites
	BEFORE UPDATE ON user_favorites
	FOR EACH ROW EXECUTE FUNCTION public.set_updated_at();

	DROP TRIGGER IF EXISTS set_updated_at_user_dashboard_reports ON user_dashboard_reports;
	CREATE TRIGGER set_updated_at_user_dashboard_reports
	BEFORE UPDATE ON user_dashboard_reports
	FOR EACH ROW EXECUTE FUNCTION public.set_updated_at();

	DROP TRIGGER IF EXISTS set_updated_at_dashboard_templates ON dashboard_templates;
	CREATE TRIGGER set_updated_at_dashboard_templates
	BEFORE UPDATE ON dashboard_templates
	FOR EACH ROW EXECUTE FUNCTION public.set_updated_at();

	DROP TRIGGER IF EXISTS set_updated_at_user_settings ON user_settings;
	CREATE TRIGGER set_updated_at_user_settings
	BEFORE UPDATE ON user_settings
	FOR EACH ROW EXECUTE FUNCTION public.set_updated_at();

	DROP TRIGGER IF EXISTS set_updated_at_affiliate_programs ON affiliate_programs;
	CREATE TRIGGER set_updated_at_affiliate_programs
	BEFORE UPDATE ON affiliate_programs
	FOR EACH ROW EXECUTE FUNCTION public.set_updated_at();

	DROP TRIGGER IF EXISTS set_updated_at_affiliate_performance ON affiliate_performance;
	CREATE TRIGGER set_updated_at_affiliate_performance
	BEFORE UPDATE ON affiliate_performance
	FOR EACH ROW EXECUTE FUNCTION public.set_updated_at();

	DROP TRIGGER IF EXISTS set_updated_at_merchant_types ON merchant_types;
	CREATE TRIGGER set_updated_at_merchant_types
	BEFORE UPDATE ON merchant_types
	FOR EACH ROW EXECUTE FUNCTION public.set_updated_at();

	DROP TRIGGER IF EXISTS set_updated_at_platforms ON platforms;
	CREATE TRIGGER set_updated_at_platforms
	BEFORE UPDATE ON platforms
	FOR EACH ROW EXECUTE FUNCTION public.set_updated_at();

	DROP TRIGGER IF EXISTS set_updated_at_merchants ON merchants;
	CREATE TRIGGER set_updated_at_merchants
	BEFORE UPDATE ON merchants
	FOR EACH ROW EXECUTE FUNCTION public.set_updated_at();

	DROP TRIGGER IF EXISTS set_updated_at_merchant_application_status ON merchant_application_status;
	CREATE TRIGGER set_updated_at_merchant_application_status
	BEFORE UPDATE ON merchant_application_status
	FOR EACH ROW EXECUTE FUNCTION public.set_updated_at();

	DROP TRIGGER IF EXISTS set_updated_at_merchant_applications ON merchant_applications;
	CREATE TRIGGER set_updated_at_merchant_applications
	BEFORE UPDATE ON merchant_applications
	FOR EACH ROW EXECUTE FUNCTION public.set_updated_at();

	DROP TRIGGER IF EXISTS set_updated_at_merchant_affiliate_programs ON merchant_affiliate_programs;
	CREATE TRIGGER set_updated_at_merchant_affiliate_programs
	BEFORE UPDATE ON merchant_affiliate_programs
	FOR EACH ROW EXECUTE FUNCTION public.set_updated_at();

	DROP TRIGGER IF EXISTS set_updated_at_merchant_program_plans ON merchant_program_plans;
	CREATE TRIGGER set_updated_at_merchant_program_plans
	BEFORE UPDATE ON merchant_program_plans
	FOR EACH ROW EXECUTE FUNCTION public.set_updated_at();

	DROP TRIGGER IF EXISTS set_updated_at_merchant_program_subscriptions ON merchant_program_subscriptions;
	CREATE TRIGGER set_updated_at_merchant_program_subscriptions
	BEFORE UPDATE ON merchant_program_subscriptions
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

	DROP TRIGGER IF EXISTS set_updated_at_user_trend_engagements ON user_trend_engagements;
	CREATE TRIGGER set_updated_at_user_trend_engagements
	BEFORE UPDATE ON user_trend_engagements
	FOR EACH ROW EXECUTE FUNCTION public.set_updated_at();

	DROP TRIGGER IF EXISTS set_updated_at_merchant_launch_campaigns ON merchant_launch_campaigns;
	CREATE TRIGGER set_updated_at_merchant_launch_campaigns
	BEFORE UPDATE ON merchant_launch_campaigns
	FOR EACH ROW EXECUTE FUNCTION public.set_updated_at();

	DROP TRIGGER IF EXISTS set_updated_at_future_offering_trust_reviews ON future_offering_trust_reviews;
	CREATE TRIGGER set_updated_at_future_offering_trust_reviews
	BEFORE UPDATE ON future_offering_trust_reviews
	FOR EACH ROW EXECUTE FUNCTION public.set_updated_at();

	DROP TRIGGER IF EXISTS set_updated_at_commerce_routes ON commerce_routes;
	CREATE TRIGGER set_updated_at_commerce_routes
	BEFORE UPDATE ON commerce_routes
	FOR EACH ROW EXECUTE FUNCTION public.set_updated_at();

	DROP TRIGGER IF EXISTS set_updated_at_merchant_direct_attribution_configs ON merchant_direct_attribution_configs;
	CREATE TRIGGER set_updated_at_merchant_direct_attribution_configs
	BEFORE UPDATE ON merchant_direct_attribution_configs
	FOR EACH ROW EXECUTE FUNCTION public.set_updated_at();

	DROP TRIGGER IF EXISTS set_updated_at_merchant_promotions ON merchant_promotions;
	CREATE TRIGGER set_updated_at_merchant_promotions
	BEFORE UPDATE ON merchant_promotions
	FOR EACH ROW EXECUTE FUNCTION public.set_updated_at();

	DROP TRIGGER IF EXISTS set_updated_at_brands ON brands;
	CREATE TRIGGER set_updated_at_brands
	BEFORE UPDATE ON brands
	FOR EACH ROW EXECUTE FUNCTION public.set_updated_at();

	DROP TRIGGER IF EXISTS set_updated_at_market_segments ON market_segments;
	CREATE TRIGGER set_updated_at_market_segments
	BEFORE UPDATE ON market_segments
	FOR EACH ROW EXECUTE FUNCTION public.set_updated_at();

	DROP TRIGGER IF EXISTS set_updated_at_departments ON departments;
	CREATE TRIGGER set_updated_at_departments
	BEFORE UPDATE ON departments
	FOR EACH ROW EXECUTE FUNCTION public.set_updated_at();

	DROP TRIGGER IF EXISTS set_updated_at_categories ON categories;
	CREATE TRIGGER set_updated_at_categories
	BEFORE UPDATE ON categories
	FOR EACH ROW EXECUTE FUNCTION public.set_updated_at();

	DROP TRIGGER IF EXISTS set_updated_at_products ON products;
	CREATE TRIGGER set_updated_at_products
	BEFORE UPDATE ON products
	FOR EACH ROW EXECUTE FUNCTION public.set_updated_at();

	DROP TRIGGER IF EXISTS set_updated_at_merchant_products ON merchant_products;
	CREATE TRIGGER set_updated_at_merchant_products
	BEFORE UPDATE ON merchant_products
	FOR EACH ROW EXECUTE FUNCTION public.set_updated_at();

	DROP TRIGGER IF EXISTS set_updated_at_coupon_statuses ON coupon_statuses;
	CREATE TRIGGER set_updated_at_coupon_statuses
	BEFORE UPDATE ON coupon_statuses
	FOR EACH ROW EXECUTE FUNCTION public.set_updated_at();

	DROP TRIGGER IF EXISTS set_updated_at_offer_statuses ON offer_statuses;
	CREATE TRIGGER set_updated_at_offer_statuses
	BEFORE UPDATE ON offer_statuses
	FOR EACH ROW EXECUTE FUNCTION public.set_updated_at();

	DROP TRIGGER IF EXISTS set_updated_at_offers ON offers;
	CREATE TRIGGER set_updated_at_offers
	BEFORE UPDATE ON offers
	FOR EACH ROW EXECUTE FUNCTION public.set_updated_at();

	DROP TRIGGER IF EXISTS set_updated_at_value_tags ON value_tags;
	CREATE TRIGGER set_updated_at_value_tags
	BEFORE UPDATE ON value_tags
	FOR EACH ROW EXECUTE FUNCTION public.set_updated_at();

	DROP TRIGGER IF EXISTS set_updated_at_audiences ON audiences;
	CREATE TRIGGER set_updated_at_audiences
	BEFORE UPDATE ON audiences
	FOR EACH ROW EXECUTE FUNCTION public.set_updated_at();

	DROP TRIGGER IF EXISTS set_updated_at_seasonal_relevances ON seasonal_relevances;
	CREATE TRIGGER set_updated_at_seasonal_relevances
	BEFORE UPDATE ON seasonal_relevances
	FOR EACH ROW EXECUTE FUNCTION public.set_updated_at();

	DROP TRIGGER IF EXISTS set_updated_at_offer_videos ON offer_videos;
	CREATE TRIGGER set_updated_at_offer_videos
	BEFORE UPDATE ON offer_videos
	FOR EACH ROW EXECUTE FUNCTION public.set_updated_at();

	DROP TRIGGER IF EXISTS set_updated_at_offer_ratings ON offer_ratings;
	CREATE TRIGGER set_updated_at_offer_ratings
	BEFORE UPDATE ON offer_ratings
	FOR EACH ROW EXECUTE FUNCTION public.set_updated_at();

	DROP TRIGGER IF EXISTS set_updated_at_coupons ON coupons;
	CREATE TRIGGER set_updated_at_coupons
	BEFORE UPDATE ON coupons
	FOR EACH ROW EXECUTE FUNCTION public.set_updated_at();

	DROP TRIGGER IF EXISTS set_updated_at_actions ON actions;
	CREATE TRIGGER set_updated_at_actions
	BEFORE UPDATE ON actions
	FOR EACH ROW EXECUTE FUNCTION public.set_updated_at();

	DROP TRIGGER IF EXISTS set_updated_at_offer_sponsorships ON offer_sponsorships;
	CREATE TRIGGER set_updated_at_offer_sponsorships
	BEFORE UPDATE ON offer_sponsorships
	FOR EACH ROW EXECUTE FUNCTION public.set_updated_at();

	DROP TRIGGER IF EXISTS set_updated_at_sponsorship_bid_minimums ON sponsorship_bid_minimums;
	CREATE TRIGGER set_updated_at_sponsorship_bid_minimums
	BEFORE UPDATE ON sponsorship_bid_minimums
	FOR EACH ROW EXECUTE FUNCTION public.set_updated_at();

	DROP TRIGGER IF EXISTS set_updated_at_promotions ON promotions;
	CREATE TRIGGER set_updated_at_promotions
	BEFORE UPDATE ON promotions
	FOR EACH ROW EXECUTE FUNCTION public.set_updated_at();

	DROP TRIGGER IF EXISTS set_updated_at_reasons ON reasons;
	CREATE TRIGGER set_updated_at_reasons
	BEFORE UPDATE ON reasons
	FOR EACH ROW EXECUTE FUNCTION public.set_updated_at();

	DROP TRIGGER IF EXISTS set_updated_at_oauth_clients ON oauth_clients;
	CREATE TRIGGER set_updated_at_oauth_clients
	BEFORE UPDATE ON oauth_clients
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

	DROP TRIGGER IF EXISTS set_updated_at_merchant_account_roles ON merchant_account_roles;
	CREATE TRIGGER set_updated_at_merchant_account_roles
	BEFORE UPDATE ON merchant_account_roles
	FOR EACH ROW EXECUTE FUNCTION public.set_updated_at();

	DROP TRIGGER IF EXISTS set_updated_at_merchant_account_members ON merchant_account_members;
	CREATE TRIGGER set_updated_at_merchant_account_members
	BEFORE UPDATE ON merchant_account_members
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

	DROP TRIGGER IF EXISTS set_updated_at_merchant_settlement_batches ON merchant_settlement_batches;
	CREATE TRIGGER set_updated_at_merchant_settlement_batches
	BEFORE UPDATE ON merchant_settlement_batches
	FOR EACH ROW EXECUTE FUNCTION public.set_updated_at();

	DROP TRIGGER IF EXISTS set_updated_at_merchant_attribution_matches ON merchant_attribution_matches;
	CREATE TRIGGER set_updated_at_merchant_attribution_matches
	BEFORE UPDATE ON merchant_attribution_matches
	FOR EACH ROW EXECUTE FUNCTION public.set_updated_at();

	DROP TRIGGER IF EXISTS set_updated_at_merchant_postback_configs ON merchant_postback_configs;
	CREATE TRIGGER set_updated_at_merchant_postback_configs
	BEFORE UPDATE ON merchant_postback_configs
	FOR EACH ROW EXECUTE FUNCTION public.set_updated_at();

	DROP TRIGGER IF EXISTS set_updated_at_merchant_postback_events ON merchant_postback_events;
	CREATE TRIGGER set_updated_at_merchant_postback_events
	BEFORE UPDATE ON merchant_postback_events
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

	DROP TRIGGER IF EXISTS set_updated_at_affiliate_feed_sources ON affiliate_feed_sources;
	CREATE TRIGGER set_updated_at_affiliate_feed_sources
	BEFORE UPDATE ON affiliate_feed_sources
	FOR EACH ROW EXECUTE FUNCTION public.set_updated_at();

	DROP TRIGGER IF EXISTS set_updated_at_affiliate_feed_imports ON affiliate_feed_imports;
	CREATE TRIGGER set_updated_at_affiliate_feed_imports
	BEFORE UPDATE ON affiliate_feed_imports
	FOR EACH ROW EXECUTE FUNCTION public.set_updated_at();

	DROP TRIGGER IF EXISTS set_updated_at_user_wishlists ON user_wishlists;
	CREATE TRIGGER set_updated_at_user_wishlists
	BEFORE UPDATE ON user_wishlists
	FOR EACH ROW EXECUTE FUNCTION public.set_updated_at();

	DROP TRIGGER IF EXISTS set_updated_at_user_merchant_follows ON user_merchant_follows;
	CREATE TRIGGER set_updated_at_user_merchant_follows
	BEFORE UPDATE ON user_merchant_follows
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
