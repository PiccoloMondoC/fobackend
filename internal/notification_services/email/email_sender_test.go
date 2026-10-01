// focodebase/fobackend/internal/notification_services/email/email_sender_test.go
//
// Tests for the provider-neutral EmailService boundary: construction,
// validation before any provider call, provider-failure wrapping,
// activation-link policy, and an end-to-end check that an emailed
// password-reset link survives SMTP message encoding intact.
package email

import (
	"context"
	"errors"
	"io"
	"mime"
	"mime/quotedprintable"
	"strings"
	"testing"

	notificationservices "github.com/PiccoloMondoC/focodebase/fobackend/internal/notification_services"
)

// recordingProvider captures every message handed to the transport.
type recordingProvider struct {
	messages []Message
	err      error
}

func (p *recordingProvider) Send(_ context.Context, message Message) error {
	p.messages = append(p.messages, message)
	return p.err
}

func newTestService(
	t *testing.T,
	provider Provider,
	policy notificationservices.ActivationURLPolicy,
) *EmailService {
	t.Helper()

	service, err := NewEmailService(Config{
		Provider:            provider,
		DefaultFrom:         "Sagrenti <no-reply@sagrenti.local>",
		ActivationURLPolicy: policy,
	})
	if err != nil {
		t.Fatalf("construct email service: %v", err)
	}

	return service
}

func TestNewEmailServiceValidation(t *testing.T) {
	if _, err := NewEmailService(Config{
		DefaultFrom: "no-reply@sagrenti.local",
	}); !errors.Is(err, ErrEmailProviderNotConfigured) {
		t.Fatalf("missing provider: expected ErrEmailProviderNotConfigured, got %v", err)
	}

	for _, from := range []string{
		"",
		"not-an-address",
		"no-reply@sagrenti.local\r\nBcc: attacker@example.com",
	} {
		if _, err := NewEmailService(Config{
			Provider:    &recordingProvider{},
			DefaultFrom: from,
		}); !errors.Is(err, ErrInvalidSenderEmail) {
			t.Errorf("sender %q: expected ErrInvalidSenderEmail, got %v", from, err)
		}
	}
}

func TestSendEmailContextDeliversValidatedMessage(t *testing.T) {
	provider := &recordingProvider{}
	service := newTestService(t, provider, notificationservices.ActivationURLPolicy{})

	if err := service.SendEmailContext(
		context.Background(),
		"  user@example.com  ",
		"  Hello  ",
		"body",
	); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(provider.messages) != 1 {
		t.Fatalf("expected one delivered message, got %d", len(provider.messages))
	}

	got := provider.messages[0]

	if got.To != "user@example.com" {
		t.Errorf("recipient not canonicalized: %q", got.To)
	}
	if got.Subject != "Hello" {
		t.Errorf("subject not canonicalized: %q", got.Subject)
	}
	if !strings.Contains(got.From, "no-reply@sagrenti.local") {
		t.Errorf("default sender not applied: %q", got.From)
	}
}

func TestSendEmailContextRejectsBeforeReachingProvider(t *testing.T) {
	canceled, cancel := context.WithCancel(context.Background())
	cancel()

	var nilCtx context.Context

	cases := []struct {
		name    string
		ctx     context.Context
		to      string
		subject string
		body    string
		want    error
	}{
		{"nil context", nilCtx, "user@example.com", "s", "b", ErrNilContext},
		{"canceled context", canceled, "user@example.com", "s", "b", context.Canceled},
		{"empty recipient", context.Background(), " ", "s", "b", notificationservices.ErrEmptyRecipient},
		{"recipient injection", context.Background(), "user@example.com\r\nBcc: x@example.com", "s", "b", notificationservices.ErrInvalidEmailRecipient},
		{"subject injection", context.Background(), "user@example.com", "s\r\nBcc: x@example.com", "b", notificationservices.ErrEmailSubjectInjection},
		{"empty body", context.Background(), "user@example.com", "s", "  ", notificationservices.ErrEmptyMessageBody},
	}

	for _, tc := range cases {
		provider := &recordingProvider{}
		service := newTestService(t, provider, notificationservices.ActivationURLPolicy{})

		err := service.SendEmailContext(tc.ctx, tc.to, tc.subject, tc.body)
		if !errors.Is(err, tc.want) {
			t.Errorf("%s: expected %v, got %v", tc.name, tc.want, err)
		}
		if len(provider.messages) != 0 {
			t.Errorf("%s: provider was called", tc.name)
		}
	}
}

func TestSendEmailContextNilServiceIsNotConfigured(t *testing.T) {
	var service *EmailService

	err := service.SendEmailContext(context.Background(), "user@example.com", "s", "b")
	if !errors.Is(err, ErrEmailProviderNotConfigured) {
		t.Fatalf("expected ErrEmailProviderNotConfigured, got %v", err)
	}
}

func TestSendEmailContextWrapsProviderFailure(t *testing.T) {
	transportErr := errors.New("transport unavailable")
	service := newTestService(
		t,
		&recordingProvider{err: transportErr},
		notificationservices.ActivationURLPolicy{},
	)

	err := service.SendEmailContext(context.Background(), "user@example.com", "s", "b")

	if !errors.Is(err, ErrEmailDeliveryFailed) {
		t.Errorf("expected ErrEmailDeliveryFailed, got %v", err)
	}
	if !errors.Is(err, transportErr) {
		t.Errorf("underlying transport error not preserved: %v", err)
	}
}

func TestSendActivationEmailContextEnforcesURLPolicy(t *testing.T) {
	const devLink = "http://localhost:4200/activate?token=abc"

	strictProvider := &recordingProvider{}
	strict := newTestService(t, strictProvider, notificationservices.ActivationURLPolicy{})

	if err := strict.SendActivationEmailContext(
		context.Background(),
		"user@example.com",
		devLink,
	); !errors.Is(err, ErrInvalidActivationURL) {
		t.Fatalf("production-safe policy: expected ErrInvalidActivationURL, got %v", err)
	}
	if len(strictProvider.messages) != 0 {
		t.Fatalf("rejected activation link reached the provider")
	}

	devProvider := &recordingProvider{}
	dev := newTestService(
		t,
		devProvider,
		notificationservices.ActivationURLPolicy{AllowHTTPOnLoopback: true},
	)

	if err := dev.SendActivationEmailContext(
		context.Background(),
		"user@example.com",
		devLink,
	); err != nil {
		t.Fatalf("dev policy rejected loopback activation link: %v", err)
	}

	got := devProvider.messages[0]
	if got.Subject != activationEmailSubject {
		t.Errorf("unexpected activation subject: %q", got.Subject)
	}
	if !strings.Contains(got.Body, devLink) {
		t.Errorf("activation body does not contain the link")
	}
}

// TestPasswordResetEmailSurvivesSMTPEncoding sends a composed reset email
// through EmailService and the SMTP message encoder, then decodes the wire
// payload the way a mail client would. The link must come back byte-for-byte:
// quoted-printable turns '=' into "=3D" and soft-wraps lines longer than 76
// characters, and either transformation would break the link if decoding were
// not symmetric.
func TestPasswordResetEmailSurvivesSMTPEncoding(t *testing.T) {
	// 43 characters, including both URL-safe base64 punctuation characters,
	// long enough that the link line exceeds the quoted-printable line limit.
	const token = "Ab3-_Zy9xW8vU7tS6rQ5pO4nM3lK2jI1hG0fE-_dCbA"
	const resetURL = "http://localhost:4200/reset-password?token=" + token

	policy := notificationservices.ActivationURLPolicy{AllowHTTPOnLoopback: true}

	subject, body, err := notificationservices.ComposePasswordResetEmail(resetURL, policy)
	if err != nil {
		t.Fatalf("compose reset email: %v", err)
	}

	provider := &recordingProvider{}
	service := newTestService(t, provider, policy)

	if err := service.SendEmailContext(context.Background(), "user@example.com", subject, body); err != nil {
		t.Fatalf("send reset email: %v", err)
	}

	_, _, payload, err := prepareSMTPMessage(provider.messages[0])
	if err != nil {
		t.Fatalf("prepare SMTP message: %v", err)
	}

	headers, encodedBody, found := strings.Cut(string(payload), "\r\n\r\n")
	if !found {
		t.Fatalf("SMTP payload has no header/body separator")
	}

	if !strings.Contains(headers, "Content-Transfer-Encoding: quoted-printable") {
		t.Fatalf("expected quoted-printable body encoding")
	}

	decoded, err := io.ReadAll(quotedprintable.NewReader(strings.NewReader(encodedBody)))
	if err != nil {
		t.Fatalf("decode quoted-printable body: %v", err)
	}

	if !strings.Contains(string(decoded), resetURL) {
		t.Fatalf("reset link did not survive SMTP encoding; decoded body:\n%s", decoded)
	}

	var subjectHeader string
	for _, line := range strings.Split(headers, "\r\n") {
		if value, ok := strings.CutPrefix(line, "Subject: "); ok {
			subjectHeader = value
			break
		}
	}

	decodedSubject, err := new(mime.WordDecoder).DecodeHeader(subjectHeader)
	if err != nil {
		t.Fatalf("decode subject header: %v", err)
	}
	if decodedSubject != subject {
		t.Errorf("subject did not survive encoding: got %q, want %q", decodedSubject, subject)
	}
}
