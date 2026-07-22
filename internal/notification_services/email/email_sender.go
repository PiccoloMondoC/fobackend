// Package email centralises outbound e-mail delivery for the
// notification-services subsystem, abstracting away provider-specific details.
//
// sdworkspace/sdbackend/internal/notification_services/email/email_sender.go
//
// GTM:
//   Layer: 2.3 Consumer Domain
//   Release Class: SPINE
//   Reason:
//     Email delivery is release-critical communication infrastructure. It
//     supports account activation, security notifications, price-drop alerts,
//     offer-related notifications, and future provider-backed outbound email
//     delivery required by the initial Platform release spine.
//
// SPINE Rule:
//   Keep compiling.
//   Keep production-ready.
//   Preserve centralized outbound email boundary.
//   Preserve activation-email delivery path.
//   Preserve provider-agnostic service abstraction.
//   Preserve notification-service injection readiness.
//   Block deployment if this file breaks build, activation email delivery,
//   outbound email integration, or consumer communication integrity.
package email

import (
	"context"
	"errors"
	"fmt"
	"net/mail"
	"strings"

	notificationservices "github.com/PiccoloMondoC/sdworkspace/sdbackend/internal/notification_services"
)

const (
	activationEmailSubject = "Activate Your Account"

	// maxSubjectBytes follows the practical RFC 5322 subject/header-line limit.
	maxSubjectBytes = 998

	// maxBodyBytes guards provider-neutral delivery from unexpectedly large payloads.
	maxBodyBytes = 1 << 20 // 1 MiB.
)

var (
	ErrNilContext                 = errors.New("email: context must not be nil")
	ErrEmailProviderNotConfigured = errors.New("email: provider is not configured")
	ErrInvalidRecipientEmail      = errors.New("email: recipient email is invalid")
	ErrInvalidSenderEmail         = errors.New("email: sender email is invalid")
	ErrInvalidSubject             = errors.New("email: subject is invalid")
	ErrInvalidBody                = errors.New("email: body is invalid")
	ErrInvalidActivationURL       = errors.New("email: activation URL is invalid")
	ErrEmailDeliveryFailed        = errors.New("email: delivery failed")
)

// Message is the canonical provider-neutral outbound email payload.
//
// From may contain either a bare mailbox address or an RFC 5322 display-name
// sender such as "Platform Support" <support@example.com>. To is normalized
// to a bare mailbox address for provider portability.
type Message struct {
	From    string
	To      string
	Subject string
	Body    string
}

// Provider is implemented by concrete email providers such as SendGrid,
// AWS SES, Mailgun, or an internal test adapter.
type Provider interface {
	Send(ctx context.Context, message Message) error
}

// EmailService is the centralized outbound email boundary for Platform
// notification delivery.
type EmailService struct {
	provider    Provider
	defaultFrom string
}

// Config contains provider-neutral email service configuration.
type Config struct {
	Provider    Provider
	DefaultFrom string
}

// NewEmailService constructs a production-ready email service.
func NewEmailService(cfg Config) (*EmailService, error) {
	if cfg.Provider == nil {
		return nil, ErrEmailProviderNotConfigured
	}

	from, err := normalizeSenderEmail(cfg.DefaultFrom)
	if err != nil {
		return nil, err
	}

	return &EmailService{
		provider:    cfg.Provider,
		defaultFrom: from,
	}, nil
}

// SendEmail preserves the legacy call shape while routing through the
// context-aware delivery path.
func (e *EmailService) SendEmail(to, subject, body string) error {
	return e.SendEmailContext(context.Background(), to, subject, body)
}

// SendEmailContext validates and sends a provider-neutral email message.
func (e *EmailService) SendEmailContext(ctx context.Context, to, subject, body string) error {
	if ctx == nil {
		return ErrNilContext
	}
	if e == nil || e.provider == nil {
		return ErrEmailProviderNotConfigured
	}

	recipient, err := normalizeRecipientEmail(to)
	if err != nil {
		return err
	}

	subject, err = normalizeSubject(subject)
	if err != nil {
		return err
	}

	body, err = normalizeBody(body)
	if err != nil {
		return err
	}

	msg := Message{
		From:    e.defaultFrom,
		To:      recipient,
		Subject: subject,
		Body:    body,
	}

	if err := e.provider.Send(ctx, msg); err != nil {
		return fmt.Errorf("%w: %w", ErrEmailDeliveryFailed, err)
	}

	return nil
}

// SendActivationEmail preserves the legacy activation-email path.
func (e *EmailService) SendActivationEmail(toEmail, activationURL string) error {
	return e.SendActivationEmailContext(context.Background(), toEmail, activationURL)
}

// SendActivationEmailContext validates and sends an account-activation email.
// The activation URL is never logged by this package because it may contain
// bearer material or one-time credential material.
func (e *EmailService) SendActivationEmailContext(ctx context.Context, toEmail, activationURL string) error {
	if ctx == nil {
		return ErrNilContext
	}
	if e == nil || e.provider == nil {
		return ErrEmailProviderNotConfigured
	}

	activationURL, err := notificationservices.ValidateActivationURL(activationURL)
	if err != nil {
		return ErrInvalidActivationURL
	}

	body := fmt.Sprintf(
		"Click the link below to activate your Sagrenti account:\n\n%s\n\nIf you did not request this account, you can ignore this email.",
		activationURL,
	)

	return e.SendEmailContext(ctx, toEmail, activationEmailSubject, body)
}

func normalizeSenderEmail(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" || strings.ContainsAny(value, "\r\n") {
		return "", ErrInvalidSenderEmail
	}

	addr, err := mail.ParseAddress(value)
	if err != nil || addr.Address == "" {
		return "", ErrInvalidSenderEmail
	}

	return addr.String(), nil
}

func normalizeRecipientEmail(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" || strings.ContainsAny(value, "\r\n") {
		return "", ErrInvalidRecipientEmail
	}

	addr, err := mail.ParseAddress(value)
	if err != nil || addr.Address == "" {
		return "", ErrInvalidRecipientEmail
	}

	return addr.Address, nil
}

func normalizeSubject(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" || len([]byte(value)) > maxSubjectBytes || strings.ContainsAny(value, "\r\n") {
		return "", ErrInvalidSubject
	}

	return value, nil
}

func normalizeBody(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" || len([]byte(value)) > maxBodyBytes {
		return "", ErrInvalidBody
	}

	return value, nil
}