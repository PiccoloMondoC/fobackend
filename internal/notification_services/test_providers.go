// Package notificationservices — local/test notification providers.
//
// sdworkspace/sdbackend/internal/notification_services/test_providers.go
//
// GTM:
//   Layer: 2.3 Consumer Domain
//   Release Class: SPINE
//   Reason:
//     Every environment — including local development and CI — must be able
//     to construct a fully functioning EmailSender/SMSSender pair without a
//     commercial provider. These implementations are release-critical because
//     they are the default construction path in main.go until governed
//     provider selection (NewServices) lands, and they are the only supported
//     way to exercise notification-dependent flows (activation, etc.) in
//     automated tests. They enforce the same shared validation boundary
//     (recipient, subject, body, size limits) as any provider-backed
//     implementation, so tests exercise the real contract rather than a
//     looser stand-in.
//
// SPINE Rule:
//   Keep compiling.
//   Keep production-ready.
//   Preserve concurrency-safe, bounded in-memory message capture.
//   Preserve reuse of the shared root validators (ValidateEmailAddress,
//   ValidateEmailSubject, ValidateEmailBody, ValidatePhoneRecipient,
//   ValidateSMSBody, ValidateActivationURL) rather than local, divergent
//   validation.
//   Preserve context-cancellation checks before capture.
//   Preserve SendOptions propagation (correlation ID, flow) into captured
//   SMS messages and structured logs.
//   Preserve non-disclosure of message bodies, subjects, activation URLs,
//   phone numbers, and email addresses in logs.
//   Preserve distinct test log components
//   (LogComponentNotificationEmailTest / LogComponentNotificationSMSTest) so
//   test delivery is never confused with provider-backed delivery in logs.
//   Preserve NewTestServices as a zero-dependency, zero-vendor, non-erroring
//   construction path.
//   Block deployment if this file breaks build, breaks local/test
//   notification delivery, diverges from shared validation, or leaks
//   sensitive values into logs.
package notificationservices

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/PiccoloMondoC/focodebase/fobackend/internal/logging"
	"github.com/PiccoloMondoC/focodebase/fobackend/internal/utils/timeutil"
)

// maxCapturedTestMessages bounds in-memory capture per test service so a
// long-running development server cannot grow this storage unboundedly. When
// exceeded, the oldest messages are dropped first (FIFO).
const maxCapturedTestMessages = 1000

// Compile-time assertions: these prevent silent drift if either interface or
// either implementation changes shape.
var (
	_ EmailSender = (*TestEmailService)(nil)
	_ SMSSender   = (*TestSMSService)(nil)
)

// EmailMessage is a captured outbound email, retained in memory for test
// inspection only. It is never persisted or transmitted externally.
type EmailMessage struct {
	To      string
	Subject string
	Body    string
	SentAt  time.Time
}

// SMSMessage is a captured outbound SMS, retained in memory for test
// inspection only. It is never persisted or transmitted externally.
type SMSMessage struct {
	To            string
	Body          string
	SentAt        time.Time
	CorrelationID string
	Flow          string
}

// TestEmailService is a fully functioning, non-vendor EmailSender
// implementation suitable for local development and automated tests. It
// enforces the same shared validation boundary as any provider-backed
// implementation and retains sent messages in memory for inspection via
// Messages().
type TestEmailService struct {
	logger *logging.Logger

	mu       sync.RWMutex
	messages []EmailMessage
}

// NewTestEmailService constructs a TestEmailService. logger may be nil.
func NewTestEmailService(logger *logging.Logger) *TestEmailService {
	return &TestEmailService{logger: logger}
}

// SendEmailContext validates the recipient, subject, and body against the
// shared root boundary, then captures the message in memory. No message
// content, subject, or recipient is logged.
func (s *TestEmailService) SendEmailContext(ctx context.Context, to, subject, body string) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	addr, err := ValidateEmailAddress(to)
	if err != nil {
		return fmt.Errorf("test email service: %w", err)
	}
	validSubject, err := ValidateEmailSubject(subject)
	if err != nil {
		return fmt.Errorf("test email service: %w", err)
	}
	validBody, err := ValidateEmailBody(body)
	if err != nil {
		return fmt.Errorf("test email service: %w", err)
	}

	s.record(EmailMessage{
		To:      addr,
		Subject: validSubject,
		Body:    validBody,
		SentAt:  timeutil.Now(),
	})

	if s.logger != nil {
		s.logger.Info("test email captured",
			"component", LogComponentNotificationEmailTest,
			"flow", "generic",
		)
	}
	return nil
}

// SendActivationEmailContext validates the recipient and the activation URL
// through the canonical root validator, then captures the resulting message
// in memory. Neither the recipient nor the activation URL is logged.
func (s *TestEmailService) SendActivationEmailContext(ctx context.Context, toEmail, activationURL string) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	addr, err := ValidateEmailAddress(toEmail)
	if err != nil {
		return fmt.Errorf("test email service: %w", err)
	}
	validatedURL, err := ValidateActivationURL(activationURL)
	if err != nil {
		return fmt.Errorf("test email service: %w", err)
	}

	subject, err := ValidateEmailSubject("Activate your account")
	if err != nil {
		return fmt.Errorf("test email service: %w", err)
	}
	body, err := ValidateEmailBody(fmt.Sprintf("Activate your account: %s", validatedURL))
	if err != nil {
		return fmt.Errorf("test email service: %w", err)
	}

	s.record(EmailMessage{
		To:      addr,
		Subject: subject,
		Body:    body,
		SentAt:  timeutil.Now(),
	})

	if s.logger != nil {
		s.logger.Info("activation email captured",
			"component", LogComponentNotificationEmailTest,
			"flow", FlowAccountActivation,
		)
	}
	return nil
}

func (s *TestEmailService) record(msg EmailMessage) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.messages = append(s.messages, msg)
	if overflow := len(s.messages) - maxCapturedTestMessages; overflow > 0 {
		copy(s.messages, s.messages[overflow:])
		s.messages = s.messages[:maxCapturedTestMessages]
	}
}

// Messages returns a snapshot copy of all captured emails, safe for
// concurrent use with ongoing sends.
func (s *TestEmailService) Messages() []EmailMessage {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]EmailMessage, len(s.messages))
	copy(out, s.messages)
	return out
}

// Reset clears all captured emails. Intended for use between test cases so
// callers don't need to reconstruct the application. Not part of the
// EmailSender interface.
func (s *TestEmailService) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.messages = nil
}

// TestSMSService is a fully functioning, non-vendor SMSSender implementation
// suitable for local development and automated tests. It enforces the same
// shared validation boundary as any provider-backed implementation and
// retains sent messages in memory for inspection via Messages().
type TestSMSService struct {
	logger *logging.Logger

	mu       sync.RWMutex
	messages []SMSMessage
}

// NewTestSMSService constructs a TestSMSService. logger may be nil.
func NewTestSMSService(logger *logging.Logger) *TestSMSService {
	return &TestSMSService{logger: logger}
}

// SendSMSContextWithOptions validates the recipient and body against the
// shared root boundary, then captures the message in memory along with the
// caller-supplied correlation/flow metadata. No message content or recipient
// is logged.
func (s *TestSMSService) SendSMSContextWithOptions(ctx context.Context, toPhone, body string, opts SendOptions) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	phone, err := ValidatePhoneRecipient(toPhone)
	if err != nil {
		return fmt.Errorf("test SMS service: %w", err)
	}
	validBody, err := ValidateSMSBody(body)
	if err != nil {
		return fmt.Errorf("test SMS service: %w", err)
	}

	flow := opts.Flow
	if flow == "" {
		flow = "generic"
	}

	s.record(SMSMessage{
		To:            phone,
		Body:          validBody,
		SentAt:        timeutil.Now(),
		CorrelationID: opts.CorrelationID,
		Flow:          flow,
	})

	if s.logger != nil {
		s.logger.Info("test SMS captured",
			"component", LogComponentNotificationSMSTest,
			"flow", flow,
			"correlation_id", opts.CorrelationID,
		)
	}
	return nil
}

// SendActivationSMSContextWithOptions validates the recipient and the
// activation URL through the canonical root validator, then captures the
// resulting message in memory along with the caller-supplied correlation ID.
// The flow is always recorded as FlowAccountActivation regardless of
// opts.Flow, since this method's semantics are fixed. Neither the recipient
// nor the activation URL is logged.
func (s *TestSMSService) SendActivationSMSContextWithOptions(ctx context.Context, toPhone, activationURL string, opts SendOptions) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	phone, err := ValidatePhoneRecipient(toPhone)
	if err != nil {
		return fmt.Errorf("test SMS service: %w", err)
	}
	validatedURL, err := ValidateActivationURL(activationURL)
	if err != nil {
		return fmt.Errorf("test SMS service: %w", err)
	}
	body, err := ValidateSMSBody(fmt.Sprintf("Activate your account: %s", validatedURL))
	if err != nil {
		return fmt.Errorf("test SMS service: %w", err)
	}

	s.record(SMSMessage{
		To:            phone,
		Body:          body,
		SentAt:        timeutil.Now(),
		CorrelationID: opts.CorrelationID,
		Flow:          FlowAccountActivation,
	})

	if s.logger != nil {
		s.logger.Info("activation SMS captured",
			"component", LogComponentNotificationSMSTest,
			"flow", FlowAccountActivation,
			"correlation_id", opts.CorrelationID,
		)
	}
	return nil
}

func (s *TestSMSService) record(msg SMSMessage) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.messages = append(s.messages, msg)
	if overflow := len(s.messages) - maxCapturedTestMessages; overflow > 0 {
		copy(s.messages, s.messages[overflow:])
		s.messages = s.messages[:maxCapturedTestMessages]
	}
}

// Messages returns a snapshot copy of all captured SMS messages, safe for
// concurrent use with ongoing sends.
func (s *TestSMSService) Messages() []SMSMessage {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]SMSMessage, len(s.messages))
	copy(out, s.messages)
	return out
}

// Reset clears all captured SMS messages. Intended for use between test
// cases so callers don't need to reconstruct the application. Not part of
// the SMSSender interface.
func (s *TestSMSService) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.messages = nil
}

// NewTestServices constructs a complete local/test notification environment:
// a TestEmailService and TestSMSService wired into a single *Services
// container. It requires no commercial provider (no Vonage, Amazon,
// SendGrid, or Twilio) and cannot fail — its dependencies are always
// non-nil by construction — so it returns *Services directly rather than
// inventing an error branch. It is the default construction path used by
// main.go today; NewServices (see notification.go) is reserved for runtime/
// governed provider composition, where dependency configuration can
// genuinely be incomplete.
func NewTestServices(logger *logging.Logger) *Services {
	return &Services{
		Email: NewTestEmailService(logger),
		SMS:   NewTestSMSService(logger),
	}
}