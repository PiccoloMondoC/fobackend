// Package notificationruntime owns runtime composition of concrete
// notification transports behind the root notification-service interfaces.
//
// focodebase/fobackend/internal/notification_services/runtime/runtime.go
//
// GTM:
//
//	Layer: 3.1 API Server / Application Bootstrap
//	Release Class: SPINE
//	Reason:
//	  This package is the composition seam between validated bootstrap
//	  configuration and provider-neutral notification contracts. It prevents
//	  API startup code and handlers from importing concrete channel packages.
//
// SPINE Rule:
//
//	Keep compiling.
//	Keep production-ready.
//	Preserve provider-neutral return types.
//	Preserve fail-fast construction.
//	Preserve explicit activation URL policy derivation from canonical
//	bootstrap environment.
//	Do not read environment variables directly.
//	Do not leak concrete provider types to callers.
//	Block deployment if runtime notification composition is incomplete.
package notificationruntime

import (
	"errors"
	"fmt"

	"github.com/PiccoloMondoC/focodebase/fobackend/internal/bootstrap"
	"github.com/PiccoloMondoC/focodebase/fobackend/internal/logging"
	notificationservices "github.com/PiccoloMondoC/focodebase/fobackend/internal/notification_services"
	"github.com/PiccoloMondoC/focodebase/fobackend/internal/notification_services/email"
)

var ErrConfigRequired = errors.New("notification runtime: bootstrap config is required")

// NewServices builds the application-facing notification container from the
// already-validated canonical bootstrap configuration.
func NewServices(
	cfg *bootstrap.Config,
	logger *logging.Logger,
) (*notificationservices.Services, error) {
	if cfg == nil {
		return nil, ErrConfigRequired
	}

	policy := notificationservices.ActivationURLPolicy{}
	if cfg.Env == "dev" || cfg.Env == "test" {
		policy.AllowHTTPOnLoopback = true
	}

	var emailSender notificationservices.EmailSender

	switch cfg.EmailProvider {
	case bootstrap.EmailProviderTest:
		if cfg.Env == "staging" || cfg.Env == "prod" {
			return nil, fmt.Errorf("notification runtime: test email provider is not permitted in %s", cfg.Env)
		}
		emailSender = notificationservices.NewTestEmailServiceWithPolicy(logger, policy)

	case bootstrap.EmailProviderSMTP:
		provider, err := email.NewSMTPProvider(email.SMTPConfig{
			Host:     cfg.SMTPHost,
			Port:     cfg.SMTPPort,
			Username: cfg.SMTPUsername,
			Password: cfg.SMTPPassword,
			Security: cfg.SMTPSecurity,
		})
		if err != nil {
			return nil, fmt.Errorf("notification runtime: construct SMTP provider: %w", err)
		}

		emailSender, err = email.NewEmailService(email.Config{
			Provider:            provider,
			DefaultFrom:         cfg.SMTPFrom,
			ActivationURLPolicy: policy,
		})
		if err != nil {
			return nil, fmt.Errorf("notification runtime: construct email service: %w", err)
		}

	default:
		return nil, fmt.Errorf("notification runtime: unsupported email provider %q", cfg.EmailProvider)
	}

	// Production SMS-provider integration is outside this assignment. Keep the
	// existing zero-dependency implementation behind the SMSSender interface.
	smsSender := notificationservices.NewTestSMSServiceWithPolicy(logger, policy)

	return notificationservices.NewServices(emailSender, smsSender)
}
