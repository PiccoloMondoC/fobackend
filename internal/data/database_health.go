// Package data provides shared data-layer models and database access methods.
//
// focodebase/fobackend/internal/data/database_health.go
//
// GTM:
//
//	Layer: 2.1 Database / Governance Foundation
//	Release Class: SPINE
//	Reason:
//	  Database health checking is release-critical infrastructure. It verifies
//	  that the canonical Models database pool is initialized, reachable, and
//	  observable before the application is treated as ready.
//
// SPINE Rule:
//
//	Keep compiling.
//	Keep production-ready.
//	Preserve Models.DB as the canonical pool source.
//	Preserve logger initialization validation.
//	Preserve context timeout protection.
//	Block deployment if this file breaks build, readiness checks,
//	database observability, or pool reachability verification.
package data

import (
	"context"
	"errors"
	"fmt"
)

// HealthCheck verifies that the configured PostgreSQL connection pool is reachable.
//
// This method belongs on Models because Models is the aggregate data-layer surface
// and carries the canonical *pgxpool.Pool through Models.DB. It must not use the
// package-level db variable.
func (m Models) HealthCheck(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	if m.DB == nil {
		return errors.New("database health check failed: model database pool is not initialized")
	}

	if m.Logger == nil {
		return errors.New("database health check failed: model logger is not initialized")
	}

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("HealthCheck")

	if err := m.DB.Ping(ctx); err != nil {
		logger.Error(
			"database health check failed",
			"error", err,
			"component", "database",
			"operation", "ping",
		)
		return fmt.Errorf("database health check failed: %w", err)
	}

	logger.Debug(
		"database health check passed",
		"component", "database",
		"operation", "ping",
	)

	return nil
}
