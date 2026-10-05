// focodebase/fobackend/internal/notification_services/password_reset_test.go
package notificationservices

import (
	"errors"
	"strings"
	"testing"
	"time"
)

const testResetURL = "https://app.sagrenti.example/reset-password?token=abc123secret"

func TestComposePasswordResetMessageButtonAndTextLink(t *testing.T) {
	msg, err := ComposePasswordResetMessage(
		PasswordResetContent{ResetURL: testResetURL, ValidFor: time.Hour},
		ActivationURLPolicy{},
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, err := ValidateEmailSubject(msg.Subject); err != nil {
		t.Fatalf("subject fails shared validation: %v", err)
	}
	for name, body := range map[string]string{"text": msg.Text, "html": msg.HTML} {
		if _, err := ValidateEmailBody(body); err != nil {
			t.Fatalf("%s body fails shared validation: %v", name, err)
		}
	}

	// HTML: the credential appears exactly once, inside the button href.
	if strings.Count(msg.HTML, "abc123secret") != 1 ||
		!strings.Contains(msg.HTML, `href="`+testResetURL+`"`) {
		t.Fatalf("HTML must carry the reset link only in the button href")
	}

	// Text: a text-only reader needs the link itself.
	if !strings.Contains(msg.Text, testResetURL) {
		t.Fatalf("text part does not contain the reset link")
	}

	for name, body := range map[string]string{"text": msg.Text, "html": msg.HTML} {
		if !strings.Contains(body, "expires in 1 hour.") {
			t.Errorf("%s: missing concrete expiry", name)
		}
		if strings.Contains(strings.ToLower(body), "expires soon") {
			t.Errorf("%s: vague expiry copy", name)
		}
		lower := strings.ToLower(strings.ReplaceAll(body, "abc123secret", ""))
		if strings.Contains(strings.ReplaceAll(lower, "token=", ""), "token") {
			t.Errorf("%s: internal terminology in copy", name)
		}
	}
}

func TestComposePasswordResetMessageWithoutExpiryHasNoDanglingSentence(t *testing.T) {
	msg, err := ComposePasswordResetMessage(
		PasswordResetContent{ResetURL: testResetURL},
		ActivationURLPolicy{},
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for name, body := range map[string]string{"text": msg.Text, "html": msg.HTML} {
		if strings.Contains(body, "expires in .") || strings.Contains(body, "expires in  ") ||
			strings.Contains(body, " .") || strings.Contains(body, "..") {
			t.Errorf("%s: malformed punctuation", name)
		}
		if !strings.Contains(body, "works once.") {
			t.Errorf("%s: missing single-use sentence", name)
		}
	}
}

func TestComposePasswordResetMessageRejectsUnsafeLinks(t *testing.T) {
	cases := []string{
		"",
		"http://app.sagrenti.example/reset-password?token=abc",
		"https://user:pass@app.sagrenti.example/reset-password?token=abc",
		"https://app.sagrenti.example/reset-password?token=abc#frag",
		"https://app.sagrenti.example/%2e%2e/reset-password?token=abc",
		"javascript:alert(1)",
	}

	for _, link := range cases {
		_, err := ComposePasswordResetMessage(PasswordResetContent{ResetURL: link}, ActivationURLPolicy{})
		if !errors.Is(err, ErrInvalidPasswordResetURL) {
			t.Errorf("link %q: expected ErrInvalidPasswordResetURL, got %v", link, err)
		}
	}
}

func TestComposePasswordResetMessageLoopbackHTTPRequiresPolicy(t *testing.T) {
	const link = "http://localhost:4200/reset-password?token=abc"

	if _, err := ComposePasswordResetMessage(PasswordResetContent{ResetURL: link}, ActivationURLPolicy{}); err == nil {
		t.Fatalf("production-safe policy accepted HTTP loopback link")
	}

	if _, err := ComposePasswordResetMessage(
		PasswordResetContent{ResetURL: link},
		ActivationURLPolicy{AllowHTTPOnLoopback: true},
	); err != nil {
		t.Fatalf("dev policy rejected HTTP loopback link: %v", err)
	}
}
