// Package services contains trusted internal service composition,
// automation support, moderation helpers, and shared internal workflow logic.
//
// focodebase/fobackend/internal/services/container.go
//
// GTM:
//
//	Layer: 2.6 Internal Services / Automation Foundation
//	Release Class: SPINE
//	Reason:
//	  Defines the validated dependency container shared by trusted internal
//	  workflows while preserving narrow capability interfaces across package
//	  boundaries.
//
// SPINE Rule:
//
//	Keep compiling.
//	Keep production-ready.
//	Reject invalid service composition before internal workflows execute.
//	Never permit DB-bound operations with nil models, nil logging, nil
//	configuration, or a non-positive database timeout.
//	Preserve validated activation-token and password-reset-token lifetime
//	configuration at the internal-service boundary.
//	Preserve narrow capability interfaces instead of concrete cross-package
//	service coupling.
//	Do not turn this container into a general service locator.
package services

import (
	"context"
	"fmt"
	"time"

	"github.com/PiccoloMondoC/focodebase/fobackend/internal/data"
	"github.com/PiccoloMondoC/focodebase/fobackend/internal/logging"
	"github.com/google/uuid"
)

// SessionTokenIssuer is the minimum authentication capability required by
// internal Users workflows.
//
// The concrete auth.TokenService satisfies this interface. Services therefore
// depend on credential-issuance semantics rather than auth package structure.
type SessionTokenIssuer interface {
	GenerateTokensPair(ctx context.Context, userID uuid.UUID) (accessToken string, refreshToken string, err error)
}

// Config contains configuration owned by internal services.
type Config struct {
	DBTimeout time.Duration

	// ActivationTokenTTL is the configured validity duration for newly issued
	// account-activation bearer credentials.
	ActivationTokenTTL time.Duration

	// PasswordResetTokenTTL is the configured validity duration for newly issued
	// password-reset bearer credentials.
	PasswordResetTokenTTL time.Duration
}

const (
	maxActivationTokenTTL    = 30 * 24 * time.Hour
	maxPasswordResetTokenTTL = 30 * 24 * time.Hour
)

// Validate verifies that the internal-services configuration is usable.
func (c *Config) Validate() error {
	if c == nil {
		return fmt.Errorf("%w: config is nil", ErrInvalidServiceConfiguration)
	}
	if c.DBTimeout <= 0 {
		return fmt.Errorf("%w: DBTimeout must be greater than zero", ErrInvalidServiceConfiguration)
	}
	if c.ActivationTokenTTL <= 0 {
		return fmt.Errorf(
			"%w: ActivationTokenTTL must be greater than zero",
			ErrInvalidServiceConfiguration,
		)
	}
	if c.ActivationTokenTTL > maxActivationTokenTTL {
		return fmt.Errorf(
			"%w: ActivationTokenTTL must not exceed %s",
			ErrInvalidServiceConfiguration,
			maxActivationTokenTTL,
		)
	}
	if c.PasswordResetTokenTTL <= 0 {
		return fmt.Errorf(
			"%w: PasswordResetTokenTTL must be greater than zero",
			ErrInvalidServiceConfiguration,
		)
	}
	if c.PasswordResetTokenTTL > maxPasswordResetTokenTTL {
		return fmt.Errorf(
			"%w: PasswordResetTokenTTL must not exceed %s",
			ErrInvalidServiceConfiguration,
			maxPasswordResetTokenTTL,
		)
	}
	return nil
}

// Service contains validated dependencies shared by trusted internal workflows.
type Service struct {
	Logger        *logging.Logger
	Models        *data.Models
	Cfg           *Config
	ShutdownChan  chan struct{}
	SessionTokens SessionTokenIssuer
}

// NewService constructs the validated internal-service composition root.
func NewService(logger *logging.Logger, models *data.Models, cfg *Config, shutdownChan chan struct{}, sessionTokens SessionTokenIssuer) (*Service, error) {
	if logger == nil {
		return nil, fmt.Errorf("%w: logger is nil", ErrInvalidServiceConfiguration)
	}
	if models == nil {
		return nil, fmt.Errorf("%w: models is nil", ErrInvalidServiceConfiguration)
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	if sessionTokens == nil {
		return nil, fmt.Errorf("%w: session token issuer is nil", ErrInvalidServiceConfiguration)
	}
	return &Service{Logger: logger, Models: models, Cfg: cfg, ShutdownChan: shutdownChan, SessionTokens: sessionTokens}, nil
}

// validate verifies dependencies required by DB-bound internal-service work.
func (s *Service) validate() error {
	if s == nil {
		return fmt.Errorf("%w: service is nil", ErrInvalidServiceConfiguration)
	}
	if s.Logger == nil {
		return fmt.Errorf("%w: logger is nil", ErrInvalidServiceConfiguration)
	}
	if s.Models == nil || s.Models.DB == nil {
		return fmt.Errorf("%w: models/database is nil", ErrInvalidServiceConfiguration)
	}
	if s.SessionTokens == nil {
		return fmt.Errorf("%w: session token issuer is nil", ErrInvalidServiceConfiguration)
	}
	return s.Cfg.Validate()
}
