// Package notificationservices owns shared notification-service contracts,
// the application-facing dependency container, and validation helpers used
// by channel-specific notification packages.
//
// focodebase/fobackend/internal/notification_services/notification.go
//
// GTM:
//
//	Layer: 2.3 Consumer Domain
//	Release Class: SPINE
//	Reason:
//	  Shared notification-service contracts are release-critical communication
//	  infrastructure. They support account activation, safe outbound
//	  notification observability, activation-link validation, and consistent
//	  provider-neutral behavior across SMS, email, and future notification
//	  channels required by the initial release spine. This package is also
//	  the application-facing composition boundary: the HTTP application
//	  depends only on the interfaces and container defined here, never on
//	  concrete channel packages.
//
// SPINE Rule:
//
//	Keep compiling.
//	Keep production-ready.
//	Preserve shared notification-service contract ownership.
//	Preserve context-aware EmailSender / SMSSender as the only types the API
//	application layer is allowed to depend on for notification delivery.
//	Preserve SendOptions observability metadata across the SMS contract.
//	Preserve centralized recipient, subject, and body validation.
//	Preserve strict HTTPS activation URL validation by default.
//	Preserve explicit policy opt-in for dev/test loopback HTTP only.
//	Preserve user-info, fragment, and decoded traversal rejection.
//	Preserve Services construction validation.
//	Preserve vendor-neutral language.
//	Do not allow this package to import notification_services/email or
//	notification_services/sms.
//	Block deployment if this file breaks build, activation notification
//	validation, outbound notification safety, or shared notification integrity.
package notificationservices

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/mail"
	"net/url"
	"strings"
)

const (
	maxActivationURLBytes = 4096
	maxPathDecodePasses   = 8

	MaxEmailSubjectBytes = 998
	MaxEmailBodyBytes    = 1 << 20
	MaxSMSBodyRunes      = 1600

	FlowAccountActivation = "account_activation"

	LogComponentNotificationSMS       = "notification_sms"
	LogComponentNotificationEmailTest = "notification_email_test"
	LogComponentNotificationSMSTest   = "notification_sms_test"
)

var (
	ErrInvalidActivationURL = errors.New("notification: activation URL is invalid")

	ErrEmptyRecipient           = errors.New("notification: recipient is empty")
	ErrInvalidEmailRecipient    = errors.New("notification: email recipient is invalid")
	ErrInvalidPhoneRecipient    = errors.New("notification: phone recipient is invalid")
	ErrEmptyEmailSubject        = errors.New("notification: email subject is empty")
	ErrEmailSubjectInjection    = errors.New("notification: email subject contains invalid control characters")
	ErrEmailSubjectTooLong      = errors.New("notification: email subject exceeds maximum length")
	ErrEmailBodyTooLong         = errors.New("notification: email body exceeds maximum length")
	ErrSMSBodyTooLong           = errors.New("notification: SMS body exceeds maximum length")
	ErrEmptyMessageBody         = errors.New("notification: message body is empty")
	ErrEmailSenderNotConfigured = errors.New("notification: email sender is not configured")
	ErrSMSSenderNotConfigured   = errors.New("notification: SMS sender is not configured")
)

// ActivationURLPolicy is an explicit capability policy supplied by trusted
// composition code. Its zero value is the production-safe HTTPS-only policy.
type ActivationURLPolicy struct {
	// AllowHTTPOnLoopback permits http:// only when the parsed host is
	// localhost or an IP literal whose address is loopback.
	AllowHTTPOnLoopback bool
}

// SendOptions carries safe operational metadata for notification observability.
type SendOptions struct {
	CorrelationID string
	Flow          string
}

// EmailSender is the application-facing, context-aware contract for outbound
// email delivery.
type EmailSender interface {
	SendEmailContext(ctx context.Context, to, subject, body string) error
	SendActivationEmailContext(ctx context.Context, toEmail, activationURL string) error
}

// SMSSender is the application-facing, context-aware, options-aware contract
// for outbound SMS delivery.
type SMSSender interface {
	SendSMSContextWithOptions(ctx context.Context, toPhone, body string, opts SendOptions) error
	SendActivationSMSContextWithOptions(ctx context.Context, toPhone, activationURL string, opts SendOptions) error
}

// Services is the single application-facing notification dependency container.
type Services struct {
	Email EmailSender
	SMS   SMSSender
}

// NewServices constructs a Services container and rejects incomplete
// configuration before HTTP startup.
func NewServices(email EmailSender, sms SMSSender) (*Services, error) {
	if email == nil {
		return nil, ErrEmailSenderNotConfigured
	}
	if sms == nil {
		return nil, ErrSMSSenderNotConfigured
	}
	return &Services{Email: email, SMS: sms}, nil
}

// ValidateActivationURL validates under the strict production-safe policy.
// Existing callers therefore remain HTTPS-only.
func ValidateActivationURL(raw string) (string, error) {
	return ValidateActivationURLWithPolicy(raw, ActivationURLPolicy{})
}

// ValidateActivationURLWithPolicy validates and canonicalizes an account
// activation URL under an explicitly supplied policy.
func ValidateActivationURLWithPolicy(raw string, policy ActivationURLPolicy) (string, error) {
	value := strings.TrimSpace(raw)
	if value == "" || len([]byte(value)) > maxActivationURLBytes {
		return "", ErrInvalidActivationURL
	}

	parsed, err := url.Parse(value)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrInvalidActivationURL, err)
	}

	parsed.Scheme = strings.ToLower(parsed.Scheme)

	switch parsed.Scheme {
	case "https":
		// Always accepted, subject to the remaining structural checks below.
	case "http":
		if !policy.AllowHTTPOnLoopback || !isLoopbackActivationHost(parsed.Hostname()) {
			return "", ErrInvalidActivationURL
		}
	default:
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
		decoded = strings.TrimSpace(decoded)
		if decoded == "." || decoded == ".." {
			return "", ErrInvalidActivationURL
		}
	}

	return parsed.String(), nil
}

func isLoopbackActivationHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
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
// and rejects CR/LF header-injection attempts.
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

// ValidateEmailSubject validates an email subject.
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

// ValidateEmailBody validates an email body.
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
// E.164-compatible boundary.
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

// ValidateSMSBody validates an SMS body.
func ValidateSMSBody(raw string) (string, error) {
	if strings.TrimSpace(raw) == "" {
		return "", ErrEmptyMessageBody
	}
	if len([]rune(raw)) > MaxSMSBodyRunes {
		return "", ErrSMSBodyTooLong
	}
	return raw, nil
}
