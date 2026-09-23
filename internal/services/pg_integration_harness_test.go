// Package services contains trusted internal service composition,
// automation support, moderation helpers, and shared internal workflow logic.
//
// focodebase/fobackend/internal/services/pg_integration_harness_test.go
//
// PostgreSQL integration-test infrastructure for service-layer transactional
// regression tests.
//
// This harness deliberately does NOT create or approximate application schema.
// The target database must already have been initialized through Sagrenti's
// canonical schema/bootstrap path.
//
// The purpose of these tests is to prove production PostgreSQL behavior against
// production schema semantics, not against a parallel test-owned schema.
package services

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/PiccoloMondoC/focodebase/fobackend/internal/data"
	"github.com/PiccoloMondoC/focodebase/fobackend/internal/logging"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const integrationDatabaseEnv = "SAGRENTI_TEST_DATABASE_URL"

// stubSessionTokenIssuer satisfies SessionTokenIssuer so the canonical Service
// composition can be constructed. Activation workflows do not issue sessions.
type stubSessionTokenIssuer struct{}

func (stubSessionTokenIssuer) GenerateTokensPair(
	_ context.Context,
	_ uuid.UUID,
) (string, string, error) {
	return "stub-access-token", "stub-refresh-token", nil
}

var (
	integrationLoggerOnce sync.Once
	integrationLoggerInst *logging.Logger
	integrationLoggerErr  error
)

func integrationLogger(t *testing.T) *logging.Logger {
	t.Helper()

	integrationLoggerOnce.Do(func() {
		integrationLoggerInst, integrationLoggerErr =
			logging.InitLogging("services-integration-test")
	})

	if integrationLoggerErr != nil {
		t.Fatalf(
			"integration harness: initialize logger: %v",
			integrationLoggerErr,
		)
	}

	return integrationLoggerInst
}

// requireDedicatedTestDatabase refuses to perform integration-test mutations
// unless PostgreSQL itself reports a database name containing "test".
//
// The DSN is deliberately NOT accepted as evidence. A production database must
// not become eligible merely because a host, username, password, or query
// parameter happens to contain the word "test".
func requireDedicatedTestDatabase(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
) {
	t.Helper()

	var dbName string

	if err := pool.QueryRow(
		ctx,
		`SELECT current_database()`,
	).Scan(&dbName); err != nil {
		t.Fatalf(
			"integration harness: read current database: %v",
			err,
		)
	}

	if !strings.Contains(
		strings.ToLower(dbName),
		"test",
	) {
		t.Fatalf(
			"integration harness: refusing to mutate database %q; "+
				"the PostgreSQL database name itself must contain \"test\"",
			dbName,
		)
	}
}

// requireIntegrationSchema verifies the minimum canonical schema surface used
// by the real activation and account-closure workflows.
//
// It intentionally creates nothing. Missing schema means the test database has
// not been initialized through the canonical application schema path and is a
// test-environment failure.
func requireIntegrationSchema(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
) {
	t.Helper()

	requiredTables := []string{
		"users",
		"activation_tokens",
		"user_profiles",
		"user_notifications",
		"user_role_assignments",
	}

	for _, table := range requiredTables {
		var exists bool

		if err := pool.QueryRow(
			ctx,
			`
				SELECT to_regclass($1) IS NOT NULL
			`,
			table,
		).Scan(&exists); err != nil {
			t.Fatalf(
				"integration harness: verify table %q: %v",
				table,
				err,
			)
		}

		if !exists {
			t.Fatalf(
				"integration harness: required production table %q "+
					"is missing; initialize the dedicated test database "+
					"through the canonical Sagrenti schema path",
				table,
			)
		}
	}

	requiredColumns := map[string][]string{
		"users": {
			"id",
			"email",
			"is_active",
			"deleted_at",
			"created_at",
			"updated_at",
		},
		"activation_tokens": {
			"id",
			"user_id",
			"token_hash",
			"expires_at",
			"created_at",
			"updated_at",
		},
		"user_profiles": {
			"user_id",
			"deleted_at",
			"updated_at",
		},
		"user_notifications": {
			"user_id",
			"deleted_at",
			"updated_at",
		},
		"user_role_assignments": {
			"user_id",
			"is_primary",
			"deleted_at",
			"updated_at",
		},
	}

	for table, columns := range requiredColumns {
		for _, column := range columns {
			var exists bool

			if err := pool.QueryRow(
				ctx,
				`
					SELECT EXISTS (
						SELECT 1
						FROM information_schema.columns
						WHERE table_schema = ANY (
							current_schemas(false)
						)
						  AND table_name = $1
						  AND column_name = $2
					)
				`,
				table,
				column,
			).Scan(&exists); err != nil {
				t.Fatalf(
					"integration harness: verify %s.%s: %v",
					table,
					column,
					err,
				)
			}

			if !exists {
				t.Fatalf(
					"integration harness: required production column "+
						"%s.%s is missing",
					table,
					column,
				)
			}
		}
	}
}

// setupIntegration connects to the explicitly selected dedicated test database
// and constructs the real production model/service composition.
//
// If SAGRENTI_TEST_DATABASE_URL is unset, integration tests are skipped.
func setupIntegration(
	t *testing.T,
) (*Service, *data.Models, *pgxpool.Pool) {
	t.Helper()

	dsn := strings.TrimSpace(
		os.Getenv(integrationDatabaseEnv),
	)
	if dsn == "" {
		t.Skip(
			integrationDatabaseEnv +
				" not set; skipping PostgreSQL integration test",
		)
	}

	setupCtx, cancel := context.WithTimeout(
		context.Background(),
		15*time.Second,
	)
	defer cancel()

	poolConfig, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatalf(
			"integration harness: parse database URL: %v",
			err,
		)
	}

	// Match the production database-session invariant.
	poolConfig.AfterConnect = func(
		ctx context.Context,
		conn *pgx.Conn,
	) error {
		if _, err := conn.Exec(
			ctx,
			`SET TIME ZONE 'UTC'`,
		); err != nil {
			return fmt.Errorf(
				"set integration session time zone UTC: %w",
				err,
			)
		}

		return nil
	}

	pool, err := pgxpool.NewWithConfig(
		setupCtx,
		poolConfig,
	)
	if err != nil {
		t.Fatalf(
			"integration harness: connect: %v",
			err,
		)
	}

	if err := pool.Ping(setupCtx); err != nil {
		pool.Close()
		t.Fatalf(
			"integration harness: ping: %v",
			err,
		)
	}

	requireDedicatedTestDatabase(
		t,
		setupCtx,
		pool,
	)
	requireIntegrationSchema(
		t,
		setupCtx,
		pool,
	)

	logger := integrationLogger(t)
	models := data.New(pool, logger)

	svc, err := NewService(
		logger,
		&models,
		&Config{
			DBTimeout:          5 * time.Second,
			ActivationTokenTTL: time.Hour,
		},
		make(chan struct{}),
		stubSessionTokenIssuer{},
	)
	if err != nil {
		pool.Close()
		t.Fatalf(
			"integration harness: construct service: %v",
			err,
		)
	}

	t.Cleanup(func() {
		pool.Close()
	})

	return svc, &models, pool
}

// createTestUser creates one test-owned canonical user.
//
// Direct insertion is intentional. Signup orchestration is outside the scope of
// this test slice; the fixture establishes only the authoritative lifecycle
// state activation requires.
//
// Cleanup is exact-ID scoped. It does not truncate or bulk-delete any table.
func createTestUser(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	isActive bool,
) uuid.UUID {
	t.Helper()

	userID := uuid.New()
	email := fmt.Sprintf(
		"activation-itest-%s@example.test",
		userID,
	)

	const query = `
		INSERT INTO users (
			id,
			email,
			is_active
		)
		VALUES ($1, $2, $3)
	`

	if _, err := pool.Exec(
		ctx,
		query,
		userID,
		email,
		isActive,
	); err != nil {
		t.Fatalf(
			"integration harness: insert user: %v",
			err,
		)
	}

	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(
			context.Background(),
			5*time.Second,
		)
		defer cancel()

		_, _ = pool.Exec(
			cleanupCtx,
			`DELETE FROM users WHERE id = $1`,
			userID,
		)
	})

	return userID
}

type userRowState struct {
	IsActive  bool
	DeletedAt *time.Time
}

func fetchUserRowState(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	userID uuid.UUID,
) userRowState {
	t.Helper()

	var state userRowState

	if err := pool.QueryRow(
		ctx,
		`
			SELECT
				is_active,
				deleted_at
			FROM users
			WHERE id = $1
		`,
		userID,
	).Scan(
		&state.IsActive,
		&state.DeletedAt,
	); err != nil {
		t.Fatalf(
			"integration harness: fetch user state: %v",
			err,
		)
	}

	return state
}

func countActivationTokens(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	userID uuid.UUID,
) int {
	t.Helper()

	var count int

	if err := pool.QueryRow(
		ctx,
		`
			SELECT count(*)
			FROM activation_tokens
			WHERE user_id = $1
		`,
		userID,
	).Scan(&count); err != nil {
		t.Fatalf(
			"integration harness: count activation tokens: %v",
			err,
		)
	}

	return count
}

func expireActivationToken(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	userID uuid.UUID,
) {
	t.Helper()

	tag, err := pool.Exec(
		ctx,
		`
			UPDATE activation_tokens
			SET expires_at = NOW() - interval '1 hour'
			WHERE user_id = $1
		`,
		userID,
	)
	if err != nil {
		t.Fatalf(
			"integration harness: expire activation token: %v",
			err,
		)
	}

	if tag.RowsAffected() != 1 {
		t.Fatalf(
			"integration harness: expire activation token affected %d rows, want 1",
			tag.RowsAffected(),
		)
	}
}

func runConcurrentlyBounded(
	t *testing.T,
	timeout time.Duration,
	n int,
	fn func(i int),
) {
	t.Helper()

	if n <= 0 {
		t.Fatalf("concurrency harness requires n > 0")
	}

	start := make(chan struct{})
	done := make(chan struct{})

	var wg sync.WaitGroup
	wg.Add(n)

	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			<-start
			fn(i)
		}(i)
	}

	close(start)

	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(timeout):
		t.Fatalf(
			"concurrency test exceeded %s; possible deadlock/hang regression",
			timeout,
		)
	}
}
