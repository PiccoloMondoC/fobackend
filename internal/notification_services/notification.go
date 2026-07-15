// Package notificationservices owns shared notification-service contracts,
// the application-facing dependency container, and validation helpers used
// by channel-specific notification packages.
//
// sdworkspace/sdbackend/internal/notification_services/notification.go
//
// GTM:
//   Layer: 2.3 Consumer Domain
//   Release Class: SPINE
//   Reason:
//     Shared notification-service contracts are release-critical communication
//     infrastructure. They support account activation, safe outbound
//     notification observability, activation-link validation, and consistent
//     provider-neutral behavior across SMS, email, and future notification
//     channels required by the initial release spine. This package is also
//     the application-facing composition boundary: the HTTP application
//     depends only on the interfaces and container defined here, never on
//     concrete channel packages.
//
// SPINE Rule:
//   Keep compiling.
//   Keep production-ready.
//   Preserve shared notification-service contract ownership.
//   Preserve context-aware EmailSender / SMSSender as the only types the API
//   application layer is allowed to depend on for notification delivery.
//   Preserve SendOptions observability metadata across the SMS contract
//   (correlation ID, flow) so request tracing survives the application
//   boundary.
//   Preserve centralized recipient, subject, and body validation so every
//   provider — test or commercial — enforces the same boundary.
//   Preserve activation URL validation.
//   Preserve HTTPS-only activation-link enforcement.
//   Preserve user-info, fragment, and decoded traversal rejection.
//   Preserve Services construction validation: no HTTP startup with a
//   partially-configured notification container.
//   Preserve vendor-neutral language in comments and messages; provider
//   names belong to governed configuration, not source code.
//   Do not allow this package to import notification_services/email or
//   notification_services/sms (import-cycle boundary).
//   Block deployment if this file breaks build, activation notification
//   validation, outbound notification safety, or shared notification integrity.
package notificationservices

import (
	"context"
	"errors"
	"fmt"
	"net/mail"
	"net/url"
	"strings"
)

const (
	maxActivationURLBytes = 4096
	maxPathDecodePasses   = 8

	// MaxEmailSubjectBytes bounds outbound email subject size and mirrors the
	// limit enforced by concrete email provider adapters.
	MaxEmailSubjectBytes = 998
	// MaxEmailBodyBytes bounds outbound email body size and mirrors the limit
	// enforced by concrete email provider adapters.
	MaxEmailBodyBytes = 1 << 20
	// MaxSMSBodyRunes bounds outbound SMS body size and mirrors the limit
	// enforced by concrete SMS provider adapters.
	MaxSMSBodyRunes = 1600

	// FlowAccountActivation is the stable structured-log flow value for account
	// activation notification delivery.
	FlowAccountActivation = "account_activation"

	// LogComponentNotificationSMS is the stable structured-log component value
	// for outbound SMS notification delivery through a provider-backed
	// (non-test) implementation.
	LogComponentNotificationSMS = "notification_sms"
	// LogComponentNotificationEmailTest is the stable structured-log
	// component value for outbound email captured by the local/test
	// implementation. It is intentionally distinct from any provider-backed
	// component so test delivery is never confused with real delivery in logs.
	LogComponentNotificationEmailTest = "notification_email_test"
	// LogComponentNotificationSMSTest is the stable structured-log component
	// value for outbound SMS captured by the local/test implementation. It is
	// intentionally distinct from LogComponentNotificationSMS so test delivery
	// is never confused with real delivery in logs.
	LogComponentNotificationSMSTest = "notification_sms_test"
)

var (
	// ErrInvalidActivationURL reports that an account-activation URL failed
	// notification-boundary validation.
	ErrInvalidActivationURL = errors.New("notification: activation URL is invalid")

	// ErrEmptyRecipient reports that a notification recipient was empty or
	// whitespace-only.
	ErrEmptyRecipient = errors.New("notification: recipient is empty")
	// ErrInvalidEmailRecipient reports that an email recipient failed
	// RFC 5322 parsing or contained a header-injection attempt.
	ErrInvalidEmailRecipient = errors.New("notification: email recipient is invalid")
	// ErrInvalidPhoneRecipient reports that an SMS recipient failed
	// E.164-compatible shape validation.
	ErrInvalidPhoneRecipient = errors.New("notification: phone recipient is invalid")

	// ErrEmptyEmailSubject reports that an email subject was empty or
	// whitespace-only. Distinct from ErrEmptyMessageBody: a subject is not a
	// body.
	ErrEmptyEmailSubject = errors.New("notification: email subject is empty")
	// ErrEmailSubjectInjection reports that an email subject contained a
	// CR/LF header-injection attempt.
	ErrEmailSubjectInjection = errors.New("notification: email subject contains invalid control characters")
	// ErrEmailSubjectTooLong reports that an email subject exceeded
	// MaxEmailSubjectBytes.
	ErrEmailSubjectTooLong = errors.New("notification: email subject exceeds maximum length")
	// ErrEmailBodyTooLong reports that an email body exceeded MaxEmailBodyBytes.
	ErrEmailBodyTooLong = errors.New("notification: email body exceeds maximum length")
	// ErrSMSBodyTooLong reports that an SMS body exceeded MaxSMSBodyRunes.
	ErrSMSBodyTooLong = errors.New("notification: SMS body exceeds maximum length")
	// ErrEmptyMessageBody reports that a message body was empty or
	// whitespace-only.
	ErrEmptyMessageBody = errors.New("notification: message body is empty")

	// ErrEmailSenderNotConfigured reports that a Services container was
	// constructed without an EmailSender.
	ErrEmailSenderNotConfigured = errors.New("notification: email sender is not configured")
	// ErrSMSSenderNotConfigured reports that a Services container was
	// constructed without an SMSSender.
	ErrSMSSenderNotConfigured = errors.New("notification: SMS sender is not configured")
)

// SendOptions carries safe operational metadata for notification observability.
//
// CorrelationID is written directly to structured logs. Callers must provide
// only non-sensitive opaque identifiers. It must not contain phone numbers,
// email addresses, names, bearer tokens, activation URLs, message content, or
// any other PII/protected value.
type SendOptions struct {
	CorrelationID string
	Flow          string
}

// EmailSender is the application-facing, context-aware contract for outbound
// email delivery. The HTTP application depends on this interface only; it
// must never depend on a concrete channel implementation (e.g.
// *email.EmailService). Handlers should pass request-scoped contexts so
// cancellation, deadlines, and tracing cross the notification boundary.
type EmailSender interface {
	SendEmailContext(ctx context.Context, to, subject, body string) error
	SendActivationEmailContext(ctx context.Context, toEmail, activationURL string) error
}

// SMSSender is the application-facing, context-aware, options-aware contract
// for outbound SMS delivery. The HTTP application depends on this interface
// only; it must never depend on a concrete channel implementation (e.g.
// *sms.SMSService). SendOptions preserves correlation and flow metadata
// across the application boundary for observability.
type SMSSender interface {
	SendSMSContextWithOptions(ctx context.Context, toPhone, body string, opts SendOptions) error
	SendActivationSMSContextWithOptions(ctx context.Context, toPhone, activationURL string, opts SendOptions) error
}

// Services is the single application-facing notification dependency
// container. main.go constructs exactly one *Services and wires its fields
// into the HTTP application; handlers depend only on the EmailSender and
// SMSSender interfaces, never on which concrete provider is active (local,
// sandboxed, or provider-backed).
type Services struct {
	Email EmailSender
	SMS   SMSSender
}

// NewServices constructs a Services container and rejects incomplete
// configuration before HTTP startup. This is the composition entry point for
// runtime/governed provider selection; local/test construction (see
// NewTestServices in test_providers.go) builds a Services directly, since its
// concrete dependencies are always non-nil by construction.
func NewServices(email EmailSender, sms SMSSender) (*Services, error) {
	if email == nil {
		return nil, ErrEmailSenderNotConfigured
	}
	if sms == nil {
		return nil, ErrSMSSenderNotConfigured
	}
	return &Services{Email: email, SMS: sms}, nil
}

// ValidateActivationURL validates and canonicalizes account-activation URLs.
//
// Activation URLs must be absolute HTTPS URLs, must not include user-info or
// fragments, and must not contain decoded "." or ".." path traversal segments.
func ValidateActivationURL(raw string) (string, error) {
	value := strings.TrimSpace(raw)
	if value == "" || len([]byte(value)) > maxActivationURLBytes {
		return "", ErrInvalidActivationURL
	}
	parsed, err := url.Parse(value)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrInvalidActivationURL, err)
	}
	parsed.Scheme = strings.ToLower(parsed.Scheme)
	if parsed.Scheme != "https" {
		return "", ErrInvalidActivationURL
	}
	if parsed.Hostname() == "" || parsed.User != nil || parsed.Fragment != "" {
		return "", ErrInvalidActivationURL
	}
	for _, segment := range strings.Split(parsed.EscapedPath(), "/") {
		decoded, err := fullyUnescapePathSegment(segment)
		if err != nil {
			return "", ErrInvalidActivationURL
		}
		if decoded == "." || decoded == ".." {
			return "", ErrInvalidActivationURL
		}
	}
	return parsed.String(), nil
}

func fullyUnescapePathSegment(segment string) (string, error) {
	current := segment
	for i := 0; i < maxPathDecodePasses; i++ {
		next, err := url.PathUnescape(current)
		if err != nil {
			return "", err
		}
		if next == current {
			return next, nil
		}
		current = next
	}
	return "", ErrInvalidActivationURL
}

// ValidateEmailAddress validates an email recipient using RFC 5322 parsing
// and rejects CR/LF header-injection attempts. It returns the canonical
// address on success. This is the single source of truth for "is this a
// valid email recipient" and must be reused by every EmailSender
// implementation, test or provider-backed.
func ValidateEmailAddress(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", ErrEmptyRecipient
	}
	if strings.ContainsAny(trimmed, "\r\n") {
		return "", ErrInvalidEmailRecipient
	}
	addr, err := mail.ParseAddress(trimmed)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrInvalidEmailRecipient, err)
	}
	return addr.Address, nil
}

// ValidateEmailSubject validates an email subject: non-empty, free of CR/LF
// header-injection attempts, and within MaxEmailSubjectBytes. It returns the
// trimmed subject on success.
func ValidateEmailSubject(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", ErrEmptyEmailSubject
	}
	if strings.ContainsAny(trimmed, "\r\n") {
		return "", ErrEmailSubjectInjection
	}
	if len([]byte(trimmed)) > MaxEmailSubjectBytes {
		return "", ErrEmailSubjectTooLong
	}
	return trimmed, nil
}

// ValidateEmailBody validates an email body: non-empty and within
// MaxEmailBodyBytes.
func ValidateEmailBody(raw string) (string, error) {
	if strings.TrimSpace(raw) == "" {
		return "", ErrEmptyMessageBody
	}
	if len([]byte(raw)) > MaxEmailBodyBytes {
		return "", ErrEmailBodyTooLong
	}
	return raw, nil
}

// ValidatePhoneRecipient validates an SMS recipient against an
// E.164-compatible boundary: a leading "+", 8-15 digits, and no leading zero
// after the "+". It returns the trimmed value on success. This is the single
// source of truth for "is this a valid phone recipient" and must be reused
// by every SMSSender implementation, test or provider-backed.
func ValidatePhoneRecipient(raw string) (string, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return "", ErrEmptyRecipient
	}
	if !strings.HasPrefix(value, "+") {
		return "", ErrInvalidPhoneRecipient
	}
	digits := value[1:]
	if len(digits) < 8 || len(digits) > 15 {
		return "", ErrInvalidPhoneRecipient
	}
	if digits[0] == '0' {
		return "", ErrInvalidPhoneRecipient
	}
	for _, r := range digits {
		if r < '0' || r > '9' {
			return "", ErrInvalidPhoneRecipient
		}
	}
	return value, nil
}

// ValidateSMSBody validates an SMS body: non-empty and within
// MaxSMSBodyRunes.
func ValidateSMSBody(raw string) (string, error) {
	if strings.TrimSpace(raw) == "" {
		return "", ErrEmptyMessageBody
	}
	if len([]rune(raw)) > MaxSMSBodyRunes {
		return "", ErrSMSBodyTooLong
	}
	return raw, nil
}