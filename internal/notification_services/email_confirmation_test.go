// focodebase/fobackend/internal/notification_services/email_confirmation_test.go
//
// Tests for the email-confirmation message composer: link and code
// validation, terminology, the link-free plain-text alternative, and that the
// raw link token appears only in the HTML button href.
package notificationservices

import (
	"errors"
	"regexp"
	"strings"
	"testing"
	"time"
)

const (
	testConfirmToken = "Ab3-_Zy9xW8vU7tS6rQ5pO4nM3lK2jI1"
	testConfirmURL   = "http://localhost:4200/confirm-email?token=" + testConfirmToken
	testCodeEntryURL = "http://localhost:4200/confirm-email"
)

var devPolicy = ActivationURLPolicy{AllowHTTPOnLoopback: true}

func composeTestConfirmation(t *testing.T) EmailConfirmationMessage {
	t.Helper()

	message, err := ComposeEmailConfirmationEmail(EmailConfirmationContent{
		ConfirmURL:   testConfirmURL,
		CodeEntryURL: testCodeEntryURL,
		Code:         "042917",
		LinkValidFor: 24 * time.Hour,
		CodeValidFor: 15 * time.Minute,
	}, devPolicy)
	if err != nil {
		t.Fatalf("compose: %v", err)
	}
	return message
}

var (
	htmlTagPattern  = regexp.MustCompile(`(?s)<[^>]*>`)
	htmlHeadPattern = regexp.MustCompile(`(?s)<head>.*?</head>`)
)

// visibleHTMLText approximates what a reader sees: markup and attribute
// values (including href) removed.
func visibleHTMLText(html string) string {
	return htmlTagPattern.ReplaceAllString(htmlHeadPattern.ReplaceAllString(html, ""), " ")
}

func TestComposeEmailConfirmationUsesConfirmationLanguage(t *testing.T) {
	message := composeTestConfirmation(t)

	if message.Subject != "Confirm your email" {
		t.Errorf("subject = %q", message.Subject)
	}

	for name, body := range map[string]string{"text": message.Text, "html": visibleHTMLText(message.HTML)} {
		if !strings.Contains(body, "Confirm your email") {
			t.Errorf("%s body lacks the principal instruction", name)
		}
		if strings.Contains(strings.ToLower(body), "activat") {
			t.Errorf("%s body exposes internal activation terminology", name)
		}
		if !strings.Contains(body, "042 917") {
			t.Errorf("%s body lacks the grouped manual code", name)
		}
		if !strings.Contains(body, "15 minutes") {
			t.Errorf("%s body lacks the code expiry", name)
		}
	}

	if !strings.Contains(visibleHTMLText(message.HTML), "Confirm email") {
		t.Error("html body lacks the Confirm email action")
	}
}

func TestComposeEmailConfirmationHTMLHidesLinkToken(t *testing.T) {
	message := composeTestConfirmation(t)

	if strings.Contains(visibleHTMLText(message.HTML), testConfirmToken) {
		t.Fatal("link token is visible as HTML copy")
	}

	if got := strings.Count(message.HTML, testConfirmToken); got != 1 {
		t.Fatalf("link token occurs %d times in HTML, want exactly once (the button href)", got)
	}

	if !strings.Contains(message.HTML, `href="`+testConfirmURL+`"`) {
		t.Fatal("confirmation button does not link to the browser confirmation URL")
	}

	if strings.Contains(message.HTML, "localhost:8080") {
		t.Fatal("message links to the API origin")
	}

	// The plain-text alternative never carries the link or the token.
	if strings.Contains(message.Text, testConfirmToken) ||
		strings.Contains(message.Text, testConfirmURL) ||
		strings.Contains(message.Text, "token=") {
		t.Fatal("plain-text alternative exposes the confirmation link or token")
	}

	// Markdown-style link syntax must not be emitted.
	if strings.Contains(message.Text, "](") || strings.Contains(message.HTML, "](") {
		t.Fatal("message contains Markdown link syntax")
	}
}

func TestComposeEmailConfirmationPlainTextDirectsToPageAndCode(t *testing.T) {
	message := composeTestConfirmation(t)

	if !strings.Contains(message.Text, testCodeEntryURL) {
		t.Error("plain-text alternative lacks the confirmation page address")
	}
	if !strings.Contains(message.Text, "042 917") {
		t.Error("plain-text alternative lacks the six-digit code")
	}
	if strings.Contains(message.Text, testConfirmToken) {
		t.Error("plain-text alternative contains the link token")
	}
}

func TestComposeEmailConfirmationKeepsTTLsDistinct(t *testing.T) {
	message, err := ComposeEmailConfirmationEmail(EmailConfirmationContent{
		ConfirmURL:   testConfirmURL,
		CodeEntryURL: testCodeEntryURL,
		Code:         "123456",
		LinkValidFor: 48 * time.Hour,
		CodeValidFor: 10 * time.Minute,
	}, devPolicy)
	if err != nil {
		t.Fatalf("compose: %v", err)
	}

	html := visibleHTMLText(message.HTML)
	if !strings.Contains(html, "button works once and expires in 2 days") {
		t.Error("HTML body does not state the link TTL")
	}
	if !strings.Contains(html, "code expires in 10 minutes") ||
		!strings.Contains(message.Text, "code expires in 10 minutes") {
		t.Error("code TTL is not stated separately")
	}
}

func TestComposeEmailConfirmationRejectsInvalidContent(t *testing.T) {
	cases := []struct {
		name    string
		content EmailConfirmationContent
		policy  ActivationURLPolicy
	}{
		{"http link under strict policy", EmailConfirmationContent{ConfirmURL: testConfirmURL, CodeEntryURL: testCodeEntryURL, Code: "123456"}, ActivationURLPolicy{}},
		{"missing link", EmailConfirmationContent{CodeEntryURL: testCodeEntryURL, Code: "123456"}, devPolicy},
		{"missing code", EmailConfirmationContent{ConfirmURL: testConfirmURL, CodeEntryURL: testCodeEntryURL}, devPolicy},
		{"entry page equal to the link", EmailConfirmationContent{ConfirmURL: testCodeEntryURL, CodeEntryURL: testCodeEntryURL, Code: "123456"}, devPolicy},
		{"non-digit code", EmailConfirmationContent{ConfirmURL: testConfirmURL, CodeEntryURL: testCodeEntryURL, Code: "12a456"}, devPolicy},
		{"short code", EmailConfirmationContent{ConfirmURL: testConfirmURL, CodeEntryURL: testCodeEntryURL, Code: "123"}, devPolicy},
		{"code without entry page", EmailConfirmationContent{ConfirmURL: testConfirmURL, Code: "123456"}, devPolicy},
		{"entry page carrying a query", EmailConfirmationContent{ConfirmURL: testConfirmURL, CodeEntryURL: testConfirmURL, Code: "123456"}, devPolicy},
	}

	for _, tc := range cases {
		if _, err := ComposeEmailConfirmationEmail(tc.content, tc.policy); !errors.Is(err, ErrInvalidEmailConfirmationContent) {
			t.Errorf("%s: error = %v, want ErrInvalidEmailConfirmationContent", tc.name, err)
		}
	}
}

func TestHumanizeDuration(t *testing.T) {
	cases := map[time.Duration]string{
		0:                "",
		30 * time.Second: "30 seconds",
		time.Minute:      "1 minute",
		15 * time.Minute: "15 minutes",
		90 * time.Minute: "90 minutes",
		time.Hour:        "1 hour",
		24 * time.Hour:   "1 day",
		48 * time.Hour:   "2 days",
		36 * time.Hour:   "36 hours",
	}
	for in, want := range cases {
		if got := humanizeDuration(in); got != want {
			t.Errorf("humanizeDuration(%s) = %q, want %q", in, got, want)
		}
	}
}
