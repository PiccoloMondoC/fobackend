// focodebase/fobackend/internal/server/cmd/api/user_password_test.go
//
// Boundary tests for password recovery, password change, and shared
// credential-link construction. These exercise the security-relevant public
// contracts that do not require a database: uniform treatment of reset
// credential failures, separation of wrong-current-password from session
// failure, bearer extraction for session-ending revocation, and link
// construction for emailed bearer credentials.
package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/PiccoloMondoC/focodebase/fobackend/internal/data"
	notificationservices "github.com/PiccoloMondoC/focodebase/fobackend/internal/notification_services"
	"github.com/PiccoloMondoC/focodebase/fobackend/internal/security"
	"github.com/PiccoloMondoC/focodebase/fobackend/internal/services"
)

func TestClassifyResetPasswordErrorCollapsesCredentialFailures(t *testing.T) {
	t.Parallel()

	// Every credential condition reaches the handler as ErrInvalidResetToken,
	// possibly wrapped. All must produce the identical public response.
	credentialFailures := []error{
		data.ErrInvalidResetToken,
		fmt.Errorf("reset password: lock credential: %w", data.ErrInvalidResetToken),
		fmt.Errorf("outer: %w", fmt.Errorf("inner: %w", data.ErrInvalidResetToken)),
	}

	for _, err := range credentialFailures {
		got := classifyResetPasswordError(err)
		if got.status != http.StatusUnauthorized ||
			got.message != resetCredentialInvalidMsg ||
			got.serverError || got.silent {
			t.Fatalf("credential failure %v mapped to %+v", err, got)
		}
	}
}

func TestClassifyResetPasswordErrorPasswordRequirements(t *testing.T) {
	t.Parallel()

	for _, err := range []error{
		security.ErrEmptyPassword,
		security.ErrPasswordLength,
		fmt.Errorf("reset password: hash password: %w", security.ErrPasswordLength),
	} {
		got := classifyResetPasswordError(err)
		if got.status != http.StatusBadRequest ||
			got.message != passwordRequirementsMessage {
			t.Fatalf("password error %v mapped to %+v", err, got)
		}
	}
}

func TestClassifyResetPasswordErrorInfrastructureIsServerError(t *testing.T) {
	t.Parallel()

	got := classifyResetPasswordError(errors.New("connection refused"))
	if !got.serverError {
		t.Fatalf("infrastructure failure mapped to %+v", got)
	}

	if got := classifyResetPasswordError(context.Canceled); !got.silent {
		t.Fatalf("cancellation mapped to %+v", got)
	}

	if got := classifyResetPasswordError(context.DeadlineExceeded); got.status != http.StatusGatewayTimeout {
		t.Fatalf("deadline mapped to %+v", got)
	}
}

func TestClassifyChangePasswordErrorNeverUses401ForCredentialFailure(t *testing.T) {
	t.Parallel()

	// A wrong current password must never look like an expired session,
	// otherwise the client refreshes and retries instead of reporting it.
	got := classifyChangePasswordError(
		fmt.Errorf("wrapped: %w", services.ErrUsersCurrentPasswordIncorrect),
	)
	if got.status != http.StatusForbidden {
		t.Fatalf("wrong current password mapped to %+v, want 403", got)
	}
}

func TestClassifyChangePasswordErrorMapping(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		err    error
		status int
	}{
		{"missing input", services.ErrUsersAuthenticationInputInvalid, http.StatusBadRequest},
		{"confirmation mismatch", services.ErrUsersPasswordConfirmationMismatch, http.StatusBadRequest},
		{"empty new", security.ErrEmptyPassword, http.StatusBadRequest},
		{"length", security.ErrPasswordLength, http.StatusBadRequest},
		{"unchanged", services.ErrUsersPasswordUnchanged, http.StatusBadRequest},
		{"not established", services.ErrUsersPasswordNotEstablished, http.StatusConflict},
		{"account gone", data.ErrUserNotFound, http.StatusUnauthorized},
		{"deadline", context.DeadlineExceeded, http.StatusGatewayTimeout},
	}

	for _, tc := range cases {
		got := classifyChangePasswordError(tc.err)
		if got.status != tc.status || got.serverError {
			t.Errorf("%s: mapped to %+v, want status %d", tc.name, got, tc.status)
		}
	}

	if got := classifyChangePasswordError(errors.New("db down")); !got.serverError {
		t.Errorf("infrastructure failure mapped to %+v", got)
	}
}

func TestBearerTokenFromRequest(t *testing.T) {
	t.Parallel()

	cases := []struct {
		header string
		want   string
	}{
		{"Bearer abc.def.ghi", "abc.def.ghi"},
		{"bearer abc", "abc"},
		{"BEARER   abc  ", "abc"},
		{"", ""},
		{"Bearer", ""},
		{"Bearer ", ""},
		{"Basic dXNlcjpwYXNz", ""},
		{"Bearerabc", ""},
	}

	for _, tc := range cases {
		r := httptest.NewRequest(http.MethodPost, "/api/v1/user/logout", nil)
		if tc.header != "" {
			r.Header.Set("Authorization", tc.header)
		}

		if got := bearerTokenFromRequest(r); got != tc.want {
			t.Errorf("header %q: got %q, want %q", tc.header, got, tc.want)
		}
	}

	if got := bearerTokenFromRequest(nil); got != "" {
		t.Errorf("nil request: got %q", got)
	}
}

func TestBuildCredentialLinkURLPlacesEncodedTokenOnPath(t *testing.T) {
	t.Parallel()

	token := "a+b/c=d&e"

	got, err := buildCredentialLinkURL(
		"https://app.sagrenti.example/",
		passwordResetPath,
		token,
		notificationservices.ActivationURLPolicy{},
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	parsed, err := url.Parse(got)
	if err != nil {
		t.Fatalf("result is not a URL: %v", err)
	}

	if parsed.Path != "/reset-password" {
		t.Errorf("path = %q, want /reset-password", parsed.Path)
	}
	if parsed.Query().Get("token") != token {
		t.Errorf("token did not round-trip through query encoding")
	}
	if parsed.Fragment != "" {
		t.Errorf("fragment must be empty, got %q", parsed.Fragment)
	}
}

func TestBuildActivationURLUsesActivationPath(t *testing.T) {
	t.Parallel()

	got, err := buildActivationURL(
		"https://app.sagrenti.example",
		"tok",
		notificationservices.ActivationURLPolicy{},
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.HasPrefix(got, "https://app.sagrenti.example/activate?") {
		t.Errorf("activation URL = %q", got)
	}
}

func TestBuildCredentialLinkURLRejectsInvalidInput(t *testing.T) {
	t.Parallel()

	policy := notificationservices.ActivationURLPolicy{}

	cases := []struct {
		name, base, path, token string
	}{
		{"blank base", " ", passwordResetPath, "tok"},
		{"relative base", "app.sagrenti.example", passwordResetPath, "tok"},
		{"blank token", "https://app.sagrenti.example", passwordResetPath, "  "},
		{"relative path", "https://app.sagrenti.example", "reset-password", "tok"},
	}

	for _, tc := range cases {
		if _, err := buildCredentialLinkURL(tc.base, tc.path, tc.token, policy); err == nil {
			t.Errorf("%s: expected error", tc.name)
		}
	}
}

func TestBuildCredentialLinkURLLoopbackHTTPRequiresPolicy(t *testing.T) {
	t.Parallel()

	const base = "http://localhost:4200"

	if _, err := buildCredentialLinkURL(
		base,
		passwordResetPath,
		"tok",
		notificationservices.ActivationURLPolicy{},
	); err == nil {
		t.Errorf("production-safe policy accepted HTTP loopback link")
	}

	if _, err := buildCredentialLinkURL(
		base,
		passwordResetPath,
		"tok",
		notificationservices.ActivationURLPolicy{AllowHTTPOnLoopback: true},
	); err != nil {
		t.Errorf("development policy rejected HTTP loopback link: %v", err)
	}
}
