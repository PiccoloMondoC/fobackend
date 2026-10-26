// Package services provides dedicated-database fixtures for merchant-registration integration specifications.
// focodebase/fobackend/internal/services/merchant_registration_testdb_test.go
//
// GTM:
//
//	Layer: 2.4 Merchant Registration / Application Services
//	Release Class: SPINE
//	Reason: Supports the complete Foundation Level 4 merchant-registration and account-navigation vertical.
//
// SPINE Rule:
//
//	Keep compiling and preserve established security, lifecycle, and privacy contracts.
//	Preserve canonical principal ownership and backend authorization.
//	Keep private registration values out of public projections, logs, and errors.
//
// Database fixture for merchant registration integration tests.
//
// The tests run only when FOBACKEND_TEST_DATABASE_URL names a dedicated,
// disposable PostgreSQL database. They create the canonical schema through
// DBConnectionParamsModel.CreateTables (idempotent), seed the public roles
// they need, and remove every row they create.
package services

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/PiccoloMondoC/focodebase/fobackend/internal/data"
	"github.com/PiccoloMondoC/focodebase/fobackend/internal/logging"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

const merchantRegistrationTestDatabaseEnv = "FOBACKEND_TEST_DATABASE_URL"

type merchantRegistrationTestIssuer struct{}

func (merchantRegistrationTestIssuer) GenerateTokensPair(context.Context, uuid.UUID) (string, string, error) {
	return "test-access", "test-refresh", nil
}

// newMerchantRegistrationTestService returns a Service bound to the test
// database, or skips the test when no test database is configured.
func newMerchantRegistrationTestService(t *testing.T) (*Service, *pgxpool.Pool) {
	t.Helper()

	dsn := os.Getenv(merchantRegistrationTestDatabaseEnv)
	if dsn == "" {
		t.Skipf("%s is not set; skipping database integration test", merchantRegistrationTestDatabaseEnv)
	}

	logger, err := logging.InitLogging("merchant-registration-test")
	if err != nil {
		t.Fatalf("logging could not be initialized for integration tests: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("connect test database: %v", err)
	}
	t.Cleanup(pool.Close)

	if err := pool.Ping(ctx); err != nil {
		t.Fatalf("ping test database: %v", err)
	}

	schema := &data.DBConnectionParamsModel{DB: pool, Logger: logger}
	if err := schema.CreateTables(pool); err != nil {
		t.Fatalf("create canonical schema: %v", err)
	}

	models := data.New(pool, logger)
	service := &Service{
		Logger: logger,
		Models: &models,
		Cfg: &Config{
			DBTimeout:             10 * time.Second,
			ActivationTokenTTL:    time.Hour,
			PasswordResetTokenTTL: time.Hour,
		},
		SessionTokens: merchantRegistrationTestIssuer{},
	}
	return service, pool
}
