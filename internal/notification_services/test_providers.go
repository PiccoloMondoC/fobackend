// Package notificationservices — local/test notification providers.
//
// focodebase/fobackend/internal/notification_services/test_providers.go
//
// GTM:
//
//	Layer: 2.3 Consumer Domain
//	Release Class: SPINE
//	Reason:
//	  Local development and CI require zero-vendor EmailSender/SMSSender
//	  implementations that enforce the same shared validation boundary.
//
// SPINE Rule:
//
//	Keep compiling.
//	Keep production-ready.
//	Preserve concurrency-safe, bounded in-memory message capture.
//	Preserve reuse of shared root validators.
//	Preserve context-cancellation checks before capture.
//	Preserve SendOptions propagation.
//	Preserve non-disclosure of message content and recipients in logs.
//	Preserve NewTestServices as a zero-dependency construction path.
//	Block deployment if this file breaks local/test notification delivery.
package notificationservices

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/PiccoloMondoC/focodebase/fobackend/internal/logging"
	"github.com/PiccoloMondoC/focodebase/fobackend/internal/utils/timeutil"
)

const maxCapturedTestMessages = 1000

var (
	_ EmailSender = (*TestEmailService)(nil)
	_ SMSSender   = (*TestSMSService)(nil)
)

type EmailMessage struct {
	To      string
	Subject string
	Body    string
	SentAt  time.Time
}

type SMSMessage struct {
	To            string
	Body          string
	SentAt        time.Time
	CorrelationID string
	Flow          string
}

type TestEmailService struct {
	logger *logging.Logger
	policy ActivationURLPolicy

	mu       sync.RWMutex
	messages []EmailMessage
}

func NewTestEmailService(logger *logging.Logger) *TestEmailService {
	return &TestEmailService{logger: logger}
}

func NewTestEmailServiceWithPolicy(
	logger *logging.Logger,
	policy ActivationURLPolicy,
) *TestEmailService {
	return &TestEmailService{logger: logger, policy: policy}
}

func (s *TestEmailService) SendEmailContext(ctx context.Context, to, subject, body string) error {
	if ctx == nil {
		return context.Canceled
	}
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

func (s *TestEmailService) SendActivationEmailContext(
	ctx context.Context,
	toEmail, activationURL string,
) error {
	if ctx == nil {
		return context.Canceled
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	addr, err := ValidateEmailAddress(toEmail)
	if err != nil {
		return fmt.Errorf("test email service: %w", err)
	}
	validatedURL, err := ValidateActivationURLWithPolicy(activationURL, s.policy)
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

func (s *TestEmailService) Messages() []EmailMessage {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]EmailMessage, len(s.messages))
	copy(out, s.messages)
	return out
}

func (s *TestEmailService) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.messages = nil
}

type TestSMSService struct {
	logger *logging.Logger
	policy ActivationURLPolicy

	mu       sync.RWMutex
	messages []SMSMessage
}

func NewTestSMSService(logger *logging.Logger) *TestSMSService {
	return &TestSMSService{logger: logger}
}

func NewTestSMSServiceWithPolicy(
	logger *logging.Logger,
	policy ActivationURLPolicy,
) *TestSMSService {
	return &TestSMSService{logger: logger, policy: policy}
}

func (s *TestSMSService) SendSMSContextWithOptions(
	ctx context.Context,
	toPhone, body string,
	opts SendOptions,
) error {
	if ctx == nil {
		return context.Canceled
	}
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

func (s *TestSMSService) SendActivationSMSContextWithOptions(
	ctx context.Context,
	toPhone, activationURL string,
	opts SendOptions,
) error {
	if ctx == nil {
		return context.Canceled
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	phone, err := ValidatePhoneRecipient(toPhone)
	if err != nil {
		return fmt.Errorf("test SMS service: %w", err)
	}
	validatedURL, err := ValidateActivationURLWithPolicy(activationURL, s.policy)
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

func (s *TestSMSService) Messages() []SMSMessage {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]SMSMessage, len(s.messages))
	copy(out, s.messages)
	return out
}

func (s *TestSMSService) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.messages = nil
}

func NewTestServices(logger *logging.Logger) *Services {
	return &Services{
		Email: NewTestEmailService(logger),
		SMS:   NewTestSMSService(logger),
	}
}

func NewTestServicesWithPolicy(
	logger *logging.Logger,
	policy ActivationURLPolicy,
) *Services {
	return &Services{
		Email: NewTestEmailServiceWithPolicy(logger, policy),
		SMS:   NewTestSMSServiceWithPolicy(logger, policy),
	}
}
