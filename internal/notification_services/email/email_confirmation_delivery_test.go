// focodebase/fobackend/internal/notification_services/email/email_confirmation_delivery_test.go
//
// End-to-end check that the email-confirmation message leaves the SMTP
// encoder as a real multipart/alternative email: a plain-text part and an
// HTML part with a clickable confirmation button carrying the token only in
// its href, and a link-free plain-text part.
package email

import (
	"context"
	"io"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net/mail"
	"regexp"
	"strings"
	"testing"
	"time"

	notificationservices "github.com/PiccoloMondoC/focodebase/fobackend/internal/notification_services"
)

func TestEmailConfirmationIsMultipartAlternativeOnTheWire(t *testing.T) {
	const token = "Ab3-_Zy9xW8vU7tS6rQ5pO4nM3lK2jI1hG0fE-_dCbA"
	const confirmURL = "http://localhost:4200/confirm-email?token=" + token

	policy := notificationservices.ActivationURLPolicy{AllowHTTPOnLoopback: true}
	provider := &recordingProvider{}
	service := newTestService(t, provider, policy)

	if err := service.SendEmailConfirmationContext(
		context.Background(),
		"user@example.com",
		notificationservices.EmailConfirmationContent{
			ConfirmURL:   confirmURL,
			CodeEntryURL: "http://localhost:4200/confirm-email",
			Code:         "314159",
			LinkValidFor: 24 * time.Hour,
			CodeValidFor: 15 * time.Minute,
		},
	); err != nil {
		t.Fatalf("send: %v", err)
	}

	if len(provider.messages) != 1 {
		t.Fatalf("messages = %d, want 1", len(provider.messages))
	}
	if provider.messages[0].Subject != activationEmailSubject {
		t.Errorf("subject = %q", provider.messages[0].Subject)
	}

	_, _, payload, err := prepareSMTPMessage(provider.messages[0])
	if err != nil {
		t.Fatalf("prepare SMTP message: %v", err)
	}

	msg, err := mail.ReadMessage(strings.NewReader(string(payload)))
	if err != nil {
		t.Fatalf("parse wire message: %v", err)
	}

	mediaType, params, err := mime.ParseMediaType(msg.Header.Get("Content-Type"))
	if err != nil || mediaType != "multipart/alternative" {
		t.Fatalf("content type = %q (%v), want multipart/alternative", mediaType, err)
	}

	parts := map[string]string{}
	reader := multipart.NewReader(msg.Body, params["boundary"])
	for {
		part, err := reader.NextRawPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("read part: %v", err)
		}
		partType, _, _ := mime.ParseMediaType(part.Header.Get("Content-Type"))
		decoded, err := io.ReadAll(quotedprintable.NewReader(part))
		if err != nil {
			t.Fatalf("decode %s part: %v", partType, err)
		}
		parts[partType] = string(decoded)
	}

	text, html := parts["text/plain"], parts["text/html"]
	if text == "" || html == "" {
		t.Fatalf("parts present: %v", keys(parts))
	}

	if !strings.Contains(html, `href="`+confirmURL+`"`) {
		t.Fatal("HTML part lacks a clickable confirmation link that survived encoding")
	}

	visible := regexp.MustCompile(`(?s)<[^>]*>`).ReplaceAllString(html, " ")
	if strings.Contains(visible, token) {
		t.Fatal("link token is visible as HTML copy")
	}
	if !strings.Contains(visible, "Confirm email") || !strings.Contains(visible, "314 159") {
		t.Fatal("HTML part lacks the action or manual code")
	}

	if strings.Contains(text, token) || strings.Contains(text, confirmURL) {
		t.Fatal("plain-text alternative exposes the confirmation link or token")
	}
	if !strings.Contains(text, "http://localhost:4200/confirm-email") || !strings.Contains(text, "314 159") {
		t.Fatal("plain-text alternative lacks the confirmation page or code")
	}

	if got := strings.Count(string(payloadDecoded(t, parts)), token); got != 1 {
		t.Fatalf("token occurs %d times across decoded parts, want exactly 1 (HTML href)", got)
	}
}

func TestPlainMessagesRemainSinglePart(t *testing.T) {
	_, _, payload, err := prepareSMTPMessage(Message{
		From:    "no-reply@sagrenti.local",
		To:      "user@example.com",
		Subject: "s",
		Body:    "b",
	})
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	if !strings.Contains(string(payload), "Content-Type: text/plain; charset=\"utf-8\"") ||
		strings.Contains(string(payload), "multipart") {
		t.Fatal("text-only message changed shape")
	}
}

func payloadDecoded(t *testing.T, parts map[string]string) string {
	t.Helper()
	return parts["text/plain"] + parts["text/html"]
}

func keys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
