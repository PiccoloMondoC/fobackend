// focodebase/fobackend/internal/server/cmd/api/email_confirmation_links_test.go
//
// Boundary tests for browser-origin link construction and confirmation-email
// delivery. They need no database: links are built from the configured
// FrontendURL and delivered through the in-memory test email provider.
package main

import (
	"context"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/PiccoloMondoC/focodebase/fobackend/internal/bootstrap"
	"github.com/PiccoloMondoC/focodebase/fobackend/internal/data"
	notificationservices "github.com/PiccoloMondoC/focodebase/fobackend/internal/notification_services"

	"github.com/google/uuid"
)

const testLinkToken = "Zx9-_linktoken_abcdefghijklmnopq"

func newLinkTestApp(env, frontendURL string) (*Application, *notificationservices.TestEmailService) {
	policy := notificationservices.ActivationURLPolicy{AllowHTTPOnLoopback: env == "dev" || env == "test"}
	emails := notificationservices.NewTestEmailServiceWithPolicy(nil, policy)

	app := &Application{
		Config: Config{
			Bootstrap: bootstrap.Config{
				Env:         env,
				BaseURL:     "http://localhost:8080",
				FrontendURL: frontendURL,
			},
		},
		EmailService: emails,
	}

	return app, emails
}

func TestConfirmationLinksUseFrontendOriginNotAPI(t *testing.T) {
	t.Parallel()

	app, _ := newLinkTestApp("dev", "http://localhost:4200")

	content, err := app.emailConfirmationContent(data.ActivationCredentials{
		LinkToken: testLinkToken,
		Code:      "123456",
	})
	if err != nil {
		t.Fatalf("build content: %v", err)
	}

	confirm, err := url.Parse(content.ConfirmURL)
	if err != nil {
		t.Fatalf("parse confirm URL: %v", err)
	}
	if confirm.Host != "localhost:4200" || confirm.Path != "/confirm-email" {
		t.Errorf("confirm URL = %q, want localhost:4200/confirm-email", content.ConfirmURL)
	}
	if confirm.Query().Get("token") != testLinkToken {
		t.Error("confirm URL does not carry the link token")
	}

	if content.CodeEntryURL != "http://localhost:4200/confirm-email" {
		t.Errorf("code entry URL = %q", content.CodeEntryURL)
	}
	if strings.Contains(content.CodeEntryURL, testLinkToken) {
		t.Error("code entry URL carries the link token")
	}
	if content.Code != "123456" {
		t.Error("manual code not carried to the message")
	}
}

func TestConfirmationLinksFollowProductionFrontendOrigin(t *testing.T) {
	t.Parallel()

	app, _ := newLinkTestApp("prod", "https://app.sagrenti.example")

	content, err := app.emailConfirmationContent(data.ActivationCredentials{
		LinkToken: testLinkToken,
		Code:      "123456",
	})
	if err != nil {
		t.Fatalf("build content: %v", err)
	}

	if !strings.HasPrefix(content.ConfirmURL, "https://app.sagrenti.example/confirm-email?token=") {
		t.Errorf("confirm URL = %q", content.ConfirmURL)
	}
	if content.CodeEntryURL != "https://app.sagrenti.example/confirm-email" {
		t.Errorf("code entry URL = %q", content.CodeEntryURL)
	}
}

func TestConfirmationLinksRequireFrontendOrigin(t *testing.T) {
	t.Parallel()

	app, _ := newLinkTestApp("dev", "")

	if _, err := app.emailConfirmationContent(data.ActivationCredentials{
		LinkToken: testLinkToken,
		Code:      "123456",
	}); err == nil {
		t.Fatal("expected failure without a configured frontend origin")
	}
}

func TestConfirmationContentRequiresCode(t *testing.T) {
	t.Parallel()

	app, _ := newLinkTestApp("dev", "http://localhost:4200")

	if _, err := app.emailConfirmationContent(data.ActivationCredentials{
		LinkToken: testLinkToken,
	}); err == nil {
		t.Fatal("expected failure without a confirmation code")
	}
}

func TestSendActivationNotificationDeliversConfirmationEmail(t *testing.T) {
	t.Parallel()

	app, emails := newLinkTestApp("dev", "http://localhost:4200")

	channel, err := app.sendActivationNotification(
		context.Background(),
		uuid.New(),
		&data.UserContactInfo{Email: "new@example.com"},
		data.ActivationCredentials{LinkToken: testLinkToken, Code: "654321"},
	)
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	if channel != activationChannelEmail {
		t.Errorf("channel = %q", channel)
	}

	messages := emails.Messages()
	if len(messages) != 1 {
		t.Fatalf("messages = %d, want 1", len(messages))
	}

	msg := messages[0]
	if msg.Subject != "Confirm your email" {
		t.Errorf("subject = %q", msg.Subject)
	}
	if !strings.Contains(msg.HTMLBody, `href="http://localhost:4200/confirm-email?token=`+testLinkToken+`"`) {
		t.Error("HTML body lacks the browser confirmation button link")
	}
	if strings.Contains(msg.HTMLBody, "localhost:8080") || strings.Contains(msg.Body, "localhost:8080") {
		t.Error("message points at the API origin")
	}

	if strings.Contains(msg.Body, testLinkToken) || strings.Contains(msg.Body, "token=") {
		t.Error("plain-text body exposes the link token")
	}
	if !strings.Contains(msg.Body, "http://localhost:4200/confirm-email") || !strings.Contains(msg.Body, "654 321") {
		t.Error("plain-text body lacks the confirmation page or code")
	}

	visible := regexp.MustCompile(`(?s)<[^>]*>`).ReplaceAllString(msg.HTMLBody, " ")
	if strings.Contains(visible, testLinkToken) {
		t.Error("link token is visible HTML copy")
	}
	if !strings.Contains(visible, "654 321") {
		t.Error("manual code missing from HTML body")
	}
}

func TestBuildFrontendPageURLRejectsInvalidInput(t *testing.T) {
	t.Parallel()

	policy := notificationservices.ActivationURLPolicy{}
	for _, tc := range []struct{ base, path string }{
		{"", confirmEmailPath},
		{"app.sagrenti.example", confirmEmailPath},
		{"https://app.sagrenti.example", "confirm-email"},
		{"http://localhost:4200", confirmEmailPath}, // http needs dev policy
	} {
		if _, err := buildFrontendPageURL(tc.base, tc.path, policy); err == nil {
			t.Errorf("%q + %q: expected error", tc.base, tc.path)
		}
	}
}
