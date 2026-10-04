// Package email centralises outbound e-mail delivery for the
// notification-services subsystem, abstracting away provider-specific details.
//
// focodebase/fobackend/internal/notification_services/email/email_sender.go
//
// GTM:
//
//	Layer: 2.3 Consumer Domain
//	Release Class: SPINE
//	Reason:
//	  Email delivery is release-critical communication infrastructure.
//
// SPINE Rule:
//
//	Keep compiling.
//	Keep production-ready.
//	Preserve centralized outbound email boundary.
//	Preserve the activation (email-confirmation) delivery path.
//	Preserve the email-confirmation message as multipart text + HTML composed
//	only by notificationservices.ComposeEmailConfirmationEmail.
//	Preserve provider-agnostic service abstraction.
//	Preserve reuse of root notification validators.
//	Preserve explicit activation URL policy injection.
//	Block deployment if this file breaks build, activation email delivery,
//	outbound email integration, or consumer communication integrity.
package email

import (
	"context"
	"errors"
	"fmt"
	"net/mail"
	"strings"

	notificationservices "github.com/PiccoloMondoC/focodebase/fobackend/internal/notification_services"
)

// activationEmailSubject is the subject of the account-activation message,
// presented to users as email confirmation.
const activationEmailSubject = notificationservices.EmailConfirmationSubject

var (
	ErrNilContext                 = errors.New("email: context must not be nil")
	ErrEmailProviderNotConfigured = errors.New("email: provider is not configured")
	ErrInvalidSenderEmail         = errors.New("email: sender email is invalid")
	ErrInvalidActivationURL       = errors.New("email: activation URL is invalid")
	ErrEmailDeliveryFailed        = errors.New("email: delivery failed")
)

// Message is the canonical provider-neutral outbound email payload.
//
// Body is the plain-text part. HTMLBody is optional; when set, the message is
// sent as multipart/alternative with Body as the text alternative.
type Message struct {
	From     string
	To       string
	Subject  string
	Body     string
	HTMLBody string
}

// Provider is implemented by concrete delivery transports.
type Provider interface {
	Send(ctx context.Context, message Message) error
}

// EmailService is the centralized outbound email boundary.
type EmailService struct {
	provider    Provider
	defaultFrom string
	policy      notificationservices.ActivationURLPolicy
}

// Config contains provider-neutral email service configuration.
type Config struct {
	Provider            Provider
	DefaultFrom         string
	ActivationURLPolicy notificationservices.ActivationURLPolicy
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
		policy:      cfg.ActivationURLPolicy,
	}, nil
}

// SendEmail preserves the legacy call shape while routing through the
// context-aware delivery path.
func (e *EmailService) SendEmail(to, subject, body string) error {
	return e.SendEmailContext(context.Background(), to, subject, body)
}

// SendEmailContext validates and sends a provider-neutral email message.
func (e *EmailService) SendEmailContext(ctx context.Context, to, subject, body string) error {
	return e.send(ctx, to, subject, body, "")
}

// send validates and delivers a message with an optional HTML alternative.
func (e *EmailService) send(ctx context.Context, to, subject, body, htmlBody string) error {
	if ctx == nil {
		return ErrNilContext
	}
	if e == nil || e.provider == nil {
		return ErrEmailProviderNotConfigured
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	recipient, err := notificationservices.ValidateEmailAddress(to)
	if err != nil {
		return err
	}

	validSubject, err := notificationservices.ValidateEmailSubject(subject)
	if err != nil {
		return err
	}

	validBody, err := notificationservices.ValidateEmailBody(body)
	if err != nil {
		return err
	}

	if htmlBody != "" {
		if htmlBody, err = notificationservices.ValidateEmailBody(htmlBody); err != nil {
			return err
		}
	}

	msg := Message{
		From:     e.defaultFrom,
		To:       recipient,
		Subject:  validSubject,
		Body:     validBody,
		HTMLBody: htmlBody,
	}

	if err := e.provider.Send(ctx, msg); err != nil {
		return fmt.Errorf("%w: %w", ErrEmailDeliveryFailed, err)
	}

	return nil
}

// SendEmailConfirmationContext composes and sends the email-confirmation
// message as multipart text + HTML. The raw link token appears only in the
// HTML button href; the text part is link-free. URLs and code are never
// logged.
func (e *EmailService) SendEmailConfirmationContext(
	ctx context.Context,
	toEmail string,
	content notificationservices.EmailConfirmationContent,
) error {
	if ctx == nil {
		return ErrNilContext
	}
	if e == nil || e.provider == nil {
		return ErrEmailProviderNotConfigured
	}

	message, err := notificationservices.ComposeEmailConfirmationEmail(content, e.policy)
	if err != nil {
		return ErrInvalidActivationURL
	}

	return e.send(ctx, toEmail, message.Subject, message.Text, message.HTML)
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
