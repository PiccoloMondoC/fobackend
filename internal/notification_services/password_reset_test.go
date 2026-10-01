// focodebase/fobackend/internal/notification_services/password_reset_test.go
package notificationservices

import (
	"errors"
	"strings"
	"testing"
)

func TestComposePasswordResetEmailIncludesValidatedLink(t *testing.T) {
	const link = "https://app.sagrenti.example/reset-password?token=abc"

	subject, body, err := ComposePasswordResetEmail(link, ActivationURLPolicy{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, err := ValidateEmailSubject(subject); err != nil {
		t.Fatalf("subject fails shared validation: %v", err)
	}
	if _, err := ValidateEmailBody(body); err != nil {
		t.Fatalf("body fails shared validation: %v", err)
	}
	if !strings.Contains(body, link) {
		t.Fatalf("body does not contain the reset link")
	}
}

func TestComposePasswordResetEmailRejectsUnsafeLinks(t *testing.T) {
	cases := []string{
		"",
		"http://app.sagrenti.example/reset-password?token=abc",
		"https://user:pass@app.sagrenti.example/reset-password?token=abc",
		"https://app.sagrenti.example/reset-password?token=abc#frag",
		"https://app.sagrenti.example/%2e%2e/reset-password?token=abc",
		"javascript:alert(1)",
	}

	for _, link := range cases {
		_, _, err := ComposePasswordResetEmail(link, ActivationURLPolicy{})
		if !errors.Is(err, ErrInvalidPasswordResetURL) {
			t.Errorf("link %q: expected ErrInvalidPasswordResetURL, got %v", link, err)
		}
	}
}

func TestComposePasswordResetEmailLoopbackHTTPRequiresPolicy(t *testing.T) {
	const link = "http://localhost:4200/reset-password?token=abc"

	if _, _, err := ComposePasswordResetEmail(link, ActivationURLPolicy{}); err == nil {
		t.Fatalf("production-safe policy accepted HTTP loopback link")
	}

	if _, _, err := ComposePasswordResetEmail(
		link,
		ActivationURLPolicy{AllowHTTPOnLoopback: true},
	); err != nil {
		t.Fatalf("dev policy rejected HTTP loopback link: %v", err)
	}
}
