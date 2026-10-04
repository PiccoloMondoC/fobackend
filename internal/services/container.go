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
//	Preserve validated email-confirmation code and resend policy, with
//	documented defaults applied when a field is left unset.
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

	// ActivationCodeTTL is the validity duration of the manual six-digit
	// email-confirmation code. It never exceeds ActivationTokenTTL in effect
	// (the data layer caps the code expiry at the link expiry). Zero selects
	// DefaultActivationCodeTTL.
	ActivationCodeTTL time.Duration

	// ActivationCodeMaxAttempts is the number of wrong guesses allowed against
	// one issued code before it is discarded. Zero selects
	// DefaultActivationCodeMaxAttempts.
	ActivationCodeMaxAttempts int

	// ActivationCodeMaxTotalFailures is the number of wrong guesses allowed
	// across every code issued to one pending account; reissue does not reset
	// it. Zero selects DefaultActivationCodeMaxTotalFailures.
	ActivationCodeMaxTotalFailures int

	// ActivationResendCooldown is the minimum interval between public resend
	// issuances for one pending account. Requests inside the window are
	// silently absorbed. Zero selects DefaultActivationResendCooldown; a
	// negative value is invalid.
	ActivationResendCooldown time.Duration
}

const (
	maxActivationTokenTTL    = 30 * 24 * time.Hour
	maxPasswordResetTokenTTL = 30 * 24 * time.Hour

	// Email-confirmation code defaults. These are implementation defaults,
	// not settled product policy; deployments override them through
	// configuration. See the Signup -> Email Confirmation implementation
	// report for rationale.
	DefaultActivationCodeTTL              = 15 * time.Minute
	DefaultActivationCodeMaxAttempts      = 5
	DefaultActivationCodeMaxTotalFailures = 20
	DefaultActivationResendCooldown       = 60 * time.Second

	minActivationCodeTTL           = 1 * time.Minute
	maxActivationCodeTTL           = 1 * time.Hour
	maxActivationCodeMaxAttempts   = 10
	maxActivationCodeTotalFailures = 50
	maxActivationResendCooldown    = 1 * time.Hour
)

// EffectiveActivationCodeTTL returns the configured code lifetime or its
// default.
func (c *Config) EffectiveActivationCodeTTL() time.Duration {
	if c == nil || c.ActivationCodeTTL == 0 {
		return DefaultActivationCodeTTL
	}
	return c.ActivationCodeTTL
}

// EffectiveActivationCodeMaxAttempts returns the configured per-code attempt
// limit or its default.
func (c *Config) EffectiveActivationCodeMaxAttempts() int {
	if c == nil || c.ActivationCodeMaxAttempts == 0 {
		return DefaultActivationCodeMaxAttempts
	}
	return c.ActivationCodeMaxAttempts
}

// EffectiveActivationCodeMaxTotalFailures returns the configured per-record
// failure limit or its default.
func (c *Config) EffectiveActivationCodeMaxTotalFailures() int {
	if c == nil || c.ActivationCodeMaxTotalFailures == 0 {
		return DefaultActivationCodeMaxTotalFailures
	}
	return c.ActivationCodeMaxTotalFailures
}

// EffectiveActivationResendCooldown returns the configured resend cooldown or
// its default.
func (c *Config) EffectiveActivationResendCooldown() time.Duration {
	if c == nil || c.ActivationResendCooldown == 0 {
		return DefaultActivationResendCooldown
	}
	return c.ActivationResendCooldown
}

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
	if codeTTL := c.EffectiveActivationCodeTTL(); codeTTL < minActivationCodeTTL ||
		codeTTL > maxActivationCodeTTL {
		return fmt.Errorf(
			"%w: ActivationCodeTTL must be between %s and %s",
			ErrInvalidServiceConfiguration,
			minActivationCodeTTL,
			maxActivationCodeTTL,
		)
	}
	if attempts := c.EffectiveActivationCodeMaxAttempts(); attempts < 1 ||
		attempts > maxActivationCodeMaxAttempts {
		return fmt.Errorf(
			"%w: ActivationCodeMaxAttempts must be between 1 and %d",
			ErrInvalidServiceConfiguration,
			maxActivationCodeMaxAttempts,
		)
	}
	if total := c.EffectiveActivationCodeMaxTotalFailures(); total < c.EffectiveActivationCodeMaxAttempts() ||
		total > maxActivationCodeTotalFailures {
		return fmt.Errorf(
			"%w: ActivationCodeMaxTotalFailures must be between ActivationCodeMaxAttempts and %d",
			ErrInvalidServiceConfiguration,
			maxActivationCodeTotalFailures,
		)
	}
	if cooldown := c.EffectiveActivationResendCooldown(); cooldown < 0 ||
		cooldown > maxActivationResendCooldown {
		return fmt.Errorf(
			"%w: ActivationResendCooldown must be between 0 and %s",
			ErrInvalidServiceConfiguration,
			maxActivationResendCooldown,
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
