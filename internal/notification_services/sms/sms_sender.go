// Package sms provides a thin, provider-agnostic layer for sending outbound
// text messages from the notification-services subsystem. It centralizes all
// third-party SMS gateway interaction behind a small application-facing API.
//
// sdworkspace/sdbackend/internal/notification_services/sms/sms_sender.go
//
// GTM:
//   Layer: 2.3 Consumer Domain
//   Release Class: SPINE
//   Reason:
//     SMS delivery is release-critical communication infrastructure. It
//     supports account activation, security notifications, price-drop alerts,
//     offer-related notifications, and future provider-backed outbound SMS
//     delivery required by the initial Platform release spine.
//
// SPINE Rule:
//   Keep compiling.
//   Keep production-ready.
//   Preserve centralized outbound SMS boundary.
//   Preserve activation-SMS delivery path.
//   Preserve provider-agnostic service abstraction.
//   Preserve notification-service injection readiness.
//   Preserve structured observability without leaking protected values.
//   Block deployment if this file breaks build, activation SMS delivery,
//   outbound SMS integration, or consumer communication integrity.
package sms

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/PiccoloMondoC/sdworkspace/sdbackend/internal/logging"
	notificationservices "github.com/PiccoloMondoC/sdworkspace/sdbackend/internal/notification_services"
)

const maxSMSBodyRunes = 1600

var (
	ErrSMSProviderNotConfigured = errors.New("sms: provider not configured")
	ErrSMSLoggerNotConfigured   = errors.New("sms: logger not configured")
	ErrSMSContextRequired       = errors.New("sms: context must not be nil")
	ErrEmptyPhoneNumber         = errors.New("sms: phone number must not be empty")
	ErrEmptySMSBody             = errors.New("sms: body must not be empty")
	ErrSMSBodyTooLong           = errors.New("sms: body exceeds maximum supported length")
	ErrInvalidActivationURL     = errors.New("sms: activation URL is invalid")
	ErrSMSSendFailed            = errors.New("sms: send failed")
)

// Provider defines the low-level outbound SMS gateway contract.
//
// Implementations must not log SMS bodies, activation URLs, bearer tokens,
// destination phone numbers, raw provider request payloads, or any other
// plaintext protected value. Provider-specific failures should be returned as
// errors; SMSService is responsible for converting those failures into a safe
// package-level error before they leave the notification boundary.
type Provider interface {
	SendSMS(ctx context.Context, toPhone string, body string) error
}

// SMSService is the provider-agnostic outbound SMS boundary.
type SMSService struct {
	provider Provider
	logger   *logging.Logger
}

// NewSMSService constructs an SMS service with its provider and logger.
func NewSMSService(provider Provider, logger *logging.Logger) *SMSService {
	return &SMSService{provider: provider, logger: logger}
}

// SendSMS sends a text message using context.Background.
func (s *SMSService) SendSMS(toPhone, body string) error {
	return s.SendSMSContext(context.Background(), toPhone, body)
}

// SendSMSContext sends a text message without caller-supplied metadata.
func (s *SMSService) SendSMSContext(ctx context.Context, toPhone, body string) error {
	return s.SendSMSContextWithOptions(ctx, toPhone, body, notificationservices.SendOptions{})
}

// SendSMSContextWithOptions validates and sends a text message with safe
// caller-supplied observability metadata.
func (s *SMSService) SendSMSContextWithOptions(ctx context.Context, toPhone, body string, opts notificationservices.SendOptions) error {
	if err := s.validateReady(ctx); err != nil {
		return err
	}
	return s.sendSMSReady(ctx, toPhone, body, opts)
}

// SendActivationSMS sends an account-activation SMS using context.Background.
func (s *SMSService) SendActivationSMS(toPhone, activationURL string) error {
	return s.SendActivationSMSContext(context.Background(), toPhone, activationURL)
}

// SendActivationSMSContext sends an account-activation SMS without
// caller-supplied metadata.
func (s *SMSService) SendActivationSMSContext(ctx context.Context, toPhone, activationURL string) error {
	return s.SendActivationSMSContextWithOptions(ctx, toPhone, activationURL, notificationservices.SendOptions{
		Flow: notificationservices.FlowAccountActivation,
	})
}

// SendActivationSMSContextWithOptions validates the activation URL and sends an
// account-activation SMS with safe caller-supplied observability metadata.
func (s *SMSService) SendActivationSMSContextWithOptions(ctx context.Context, toPhone, activationURL string, opts notificationservices.SendOptions) error {
	if err := s.validateReady(ctx); err != nil {
		return err
	}

	activationURL, err := notificationservices.ValidateActivationURL(activationURL)
	if err != nil {
		return ErrInvalidActivationURL
	}

	if strings.TrimSpace(opts.Flow) == "" {
		opts.Flow = notificationservices.FlowAccountActivation
	}

	body := fmt.Sprintf("Activate your Sagrenti account: %s", activationURL)
	return s.sendSMSReady(ctx, toPhone, body, opts)
}

// validateReady verifies that the SMS service has the minimum dependencies
// required to execute a send operation.
func (s *SMSService) validateReady(ctx context.Context) error {
	if ctx == nil {
		return ErrSMSContextRequired
	}
	if s == nil || s.provider == nil {
		return ErrSMSProviderNotConfigured
	}
	if s.logger == nil {
		return ErrSMSLoggerNotConfigured
	}
	return nil
}

// sendSMSReady validates message-level inputs and sends the SMS.
//
// Precondition: validateReady must have already succeeded for this service and
// context. This helper intentionally does not repeat service dependency guards.
func (s *SMSService) sendSMSReady(ctx context.Context, toPhone, body string, opts notificationservices.SendOptions) error {
	toPhone = strings.TrimSpace(toPhone)
	if toPhone == "" {
		return ErrEmptyPhoneNumber
	}

	body = strings.TrimSpace(body)
	if body == "" {
		return ErrEmptySMSBody
	}

	if utf8.RuneCountInString(body) > maxSMSBodyRunes {
		return ErrSMSBodyTooLong
	}

	if err := ctx.Err(); err != nil {
		return err
	}

	if err := s.provider.SendSMS(ctx, toPhone, body); err != nil {
		s.logger.GetLoggerWithContextFromContext(ctx).Error(
			"sms send failed",
			"component", notificationservices.LogComponentNotificationSMS,
			"operation", "send_sms",
			"flow", safeLogValue(opts.Flow),
			"correlation_id", safeLogValue(opts.CorrelationID),
			"to_phone_masked", maskPhoneForLog(toPhone),
			"provider_error_type", fmt.Sprintf("%T", err),
		)
		return ErrSMSSendFailed
	}

	return nil
}

// maskPhoneForLog returns a non-reversible masked phone representation suitable
// for structured logs.
func maskPhoneForLog(phone string) string {
	digits := make([]rune, 0, len(phone))
	for _, r := range phone {
		if r >= '0' && r <= '9' {
			digits = append(digits, r)
		}
	}

	if len(digits) == 0 {
		return "unavailable"
	}
	if len(digits) <= 4 {
		return "****"
	}
	return "****" + string(digits[len(digits)-4:])
}

// safeLogValue normalizes optional log values without inventing sensitive data.
func safeLogValue(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "unspecified"
	}
	return value
}