// Package notificationservices — email-confirmation message composition.
//
// focodebase/fobackend/internal/notification_services/email_confirmation.go
//
// GTM:
//
//	Layer: 2.3 Consumer Domain
//	Release Class: SPINE
//	Reason:
//	  The email-confirmation message is the delivery half of
//	  Signup -> Email Confirmation. This file composes its subject, plain-text
//	  alternative, and HTML body from already-constructed browser URLs and an
//	  optional manual confirmation code, so every EmailSender implementation
//	  sends the same validated content.
//
// SPINE Rule:
//
//	Keep compiling.
//	Keep production-ready.
//	Preserve strict credential-link validation by reusing
//	ValidateActivationURLWithPolicy for both URLs.
//	Never expose the raw link token as visible email copy. It may appear only
//	inside the HTML confirmation button's href attribute. The plain-text
//	alternative must never contain the confirmation link or the token; it
//	directs the reader to the bearer-free confirmation page and the separate
//	six-digit code.
//	Keep the manual code page URL free of bearer material.
//	Keep the code TTL and the link TTL as distinct inputs.
//	Use user-facing confirmation language; internal activation terminology
//	must not appear in message copy.
//	Never log the URLs, code, or composed bodies; they carry credentials.
//	Block deployment if this file breaks confirmation-link validation or
//	confirmation-email composition.
package notificationservices

import (
	"bytes"
	"errors"
	"fmt"
	"html/template"
	"net/url"
	"strings"
	"time"
)

// EmailConfirmationSubject is the subject of the email-confirmation message.
const EmailConfirmationSubject = "Confirm your email"

// ErrInvalidEmailConfirmationContent reports that confirmation-email input
// failed validation. It wraps nothing that could disclose a URL or code.
var ErrInvalidEmailConfirmationContent = errors.New(
	"notification: email confirmation content is invalid",
)

// EmailConfirmationContent is the provider-neutral input for the
// email-confirmation message.
//
// ConfirmURL carries the high-entropy link token and is bearer material.
// Code is the independent manual confirmation code. Both must be handed
// straight to an EmailSender and never logged, traced, audited, or persisted.
//
// Every field except the TTLs is required: the plain-text alternative cannot
// carry the link, so the code and the code-entry page are the text reader's
// only way to confirm.
type EmailConfirmationContent struct {
	// ConfirmURL is the browser confirmation link containing the link token.
	ConfirmURL string

	// CodeEntryURL is the browser confirmation page where the manual code is
	// entered. It must not carry a query string or any bearer material.
	CodeEntryURL string

	// Code is the manual confirmation code (6–10 ASCII digits).
	Code string

	// LinkValidFor (activation-link TTL) and CodeValidFor (code TTL) are
	// distinct and drive the expiry wording. Zero omits the sentence.
	LinkValidFor time.Duration
	CodeValidFor time.Duration
}

// EmailConfirmationMessage is a composed confirmation email.
type EmailConfirmationMessage struct {
	Subject string
	Text    string
	HTML    string
}

// ComposeEmailConfirmationEmail validates content under policy and returns
// the confirmation message with a plain-text alternative and an HTML body.
//
// The raw link token exists in exactly one place: the href of the HTML
// "Confirm email" button. The plain-text alternative contains neither the
// confirmation link nor the token; it gives the bearer-free confirmation
// page address and the six-digit code instead.
func ComposeEmailConfirmationEmail(
	content EmailConfirmationContent,
	policy ActivationURLPolicy,
) (EmailConfirmationMessage, error) {
	var none EmailConfirmationMessage

	confirmURL, err := ValidateActivationURLWithPolicy(content.ConfirmURL, policy)
	if err != nil {
		return none, ErrInvalidEmailConfirmationContent
	}

	code := strings.TrimSpace(content.Code)
	if !isASCIIDigits(code) || len(code) < 6 || len(code) > 10 {
		return none, ErrInvalidEmailConfirmationContent
	}

	codeEntryURL, err := ValidateActivationURLWithPolicy(content.CodeEntryURL, policy)
	if err != nil {
		return none, ErrInvalidEmailConfirmationContent
	}

	parsed, err := url.Parse(codeEntryURL)
	if err != nil || parsed.RawQuery != "" || parsed.ForceQuery {
		// The confirmation page address is printed as visible copy, so it
		// must never carry a query string that could hold bearer material.
		return none, ErrInvalidEmailConfirmationContent
	}

	if codeEntryURL == confirmURL {
		return none, ErrInvalidEmailConfirmationContent
	}

	view := emailConfirmationView{
		ConfirmURL:   template.URL(confirmURL),
		CodeEntryURL: template.URL(codeEntryURL),
		Code:         code,
		CodeDisplay:  groupCode(code),
		LinkExpiry:   humanizeDuration(content.LinkValidFor),
		CodeExpiry:   humanizeDuration(content.CodeValidFor),
	}

	var htmlBody bytes.Buffer
	if err := emailConfirmationHTML.Execute(&htmlBody, view); err != nil {
		return none, fmt.Errorf("%w: render html", ErrInvalidEmailConfirmationContent)
	}

	text := composeEmailConfirmationText(codeEntryURL, view)

	// Defense in depth for the security invariant: the plain-text part must
	// never contain the confirmation link (and therefore the token).
	if strings.Contains(text, confirmURL) || strings.Contains(text, "token=") {
		return none, ErrInvalidEmailConfirmationContent
	}

	return EmailConfirmationMessage{
		Subject: EmailConfirmationSubject,
		Text:    text,
		HTML:    htmlBody.String(),
	}, nil
}

type emailConfirmationView struct {
	ConfirmURL   template.URL
	CodeEntryURL template.URL
	Code         string
	CodeDisplay  string
	LinkExpiry   string
	CodeExpiry   string
}

// composeEmailConfirmationText builds the plain-text alternative. It is
// deliberately link-free: text-only readers open the bearer-free
// confirmation page and enter the six-digit code.
func composeEmailConfirmationText(
	codeEntryURL string,
	view emailConfirmationView,
) string {
	var b strings.Builder

	b.WriteString("Confirm your email\n\n")
	b.WriteString("Confirm your email address to finish setting up your Sagrenti account.\n\n")
	b.WriteString("Open the Sagrenti confirmation page:\n")
	b.WriteString(codeEntryURL)
	b.WriteString("\n\n")
	b.WriteString("Then enter your email address and this confirmation code:\n\n")
	b.WriteString(view.CodeDisplay)
	b.WriteString("\n\n")

	if view.CodeExpiry != "" {
		b.WriteString("The code expires in " + view.CodeExpiry + ".\n\n")
	}

	b.WriteString("If you requested more than one email, only the newest one works.\n\n")
	b.WriteString("If you didn't create a Sagrenti account, you can ignore this email.\n")

	return b.String()
}

// The HTML body uses table layout and inline styles because mail clients
// ignore <style> blocks and modern layout inconsistently. html/template
// escapes every value; the only place the confirmation URL appears is the
// button's href.
var emailConfirmationHTML = template.Must(template.New("email-confirmation").Parse(`<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Confirm your email</title>
</head>
<body style="margin:0;padding:0;background:#f7f6f2;">
<table role="presentation" width="100%" cellspacing="0" cellpadding="0" border="0" style="background:#f7f6f2;">
<tr>
<td align="center" style="padding:32px 16px;">
<table role="presentation" width="100%" cellspacing="0" cellpadding="0" border="0" style="max-width:520px;background:#ffffff;border:1px solid #dce2e6;border-radius:8px;">
<tr>
<td style="padding:32px 32px 8px 32px;font-family:Inter,'Segoe UI',Helvetica,Arial,sans-serif;color:#1b2733;">
<p style="margin:0 0 24px 0;font-size:14px;font-weight:600;color:#14263d;">Sagrenti</p>
<h1 style="margin:0 0 12px 0;font-size:22px;line-height:1.3;font-weight:600;color:#14263d;">Confirm your email</h1>
<p style="margin:0 0 24px 0;font-size:16px;line-height:1.5;">Confirm your email address to finish setting up your Sagrenti account.</p>
<table role="presentation" cellspacing="0" cellpadding="0" border="0" style="margin:0 0 16px 0;">
<tr>
<td style="border-radius:6px;background:#14263d;">
<a href="{{.ConfirmURL}}" style="display:inline-block;padding:12px 24px;font-family:Inter,'Segoe UI',Helvetica,Arial,sans-serif;font-size:16px;font-weight:600;color:#ffffff;text-decoration:none;border-radius:6px;">Confirm email</a>
</td>
</tr>
</table>
{{- if .LinkExpiry}}
<p style="margin:0 0 24px 0;font-size:14px;line-height:1.5;color:#56636e;">The button works once and expires in {{.LinkExpiry}}.</p>
{{- end}}
<hr style="border:none;border-top:1px solid #dce2e6;margin:0 0 24px 0;">
<p style="margin:0 0 12px 0;font-size:15px;line-height:1.5;">Can't use the button? Enter this code on the <a href="{{.CodeEntryURL}}" style="color:#14263d;">confirmation page</a> instead:</p>
<p style="margin:0 0 12px 0;font-size:28px;line-height:1.2;font-weight:600;letter-spacing:4px;color:#14263d;">{{.CodeDisplay}}</p>
{{- if .CodeExpiry}}
<p style="margin:0 0 24px 0;font-size:14px;line-height:1.5;color:#56636e;">The code expires in {{.CodeExpiry}}.</p>
{{- end}}
<p style="margin:0 0 8px 0;font-size:14px;line-height:1.5;color:#56636e;">If you requested more than one email, only the newest one works.</p>
<p style="margin:0 0 24px 0;font-size:14px;line-height:1.5;color:#56636e;">If you didn't create a Sagrenti account, you can ignore this email.</p>
</td>
</tr>
</table>
</td>
</tr>
</table>
</body>
</html>
`))

// groupCode formats a six-digit code as "123 456" for readability. Other
// lengths are returned unchanged.
func groupCode(code string) string {
	if len(code) != 6 {
		return code
	}
	return code[:3] + " " + code[3:]
}

func isASCIIDigits(value string) bool {
	if value == "" {
		return false
	}
	for i := 0; i < len(value); i++ {
		if value[i] < '0' || value[i] > '9' {
			return false
		}
	}
	return true
}

// humanizeDuration renders a positive duration as plain English using its
// largest whole unit ("15 minutes", "1 hour", "2 days"). Zero or negative
// durations render as "".
func humanizeDuration(d time.Duration) string {
	if d <= 0 {
		return ""
	}

	plural := func(n int64, unit string) string {
		if n == 1 {
			return "1 " + unit
		}
		return fmt.Sprintf("%d %ss", n, unit)
	}

	switch {
	case d >= 24*time.Hour && d%(24*time.Hour) == 0:
		return plural(int64(d/(24*time.Hour)), "day")
	case d >= time.Hour && d%time.Hour == 0:
		return plural(int64(d/time.Hour), "hour")
	case d >= time.Minute:
		return plural(int64(d/time.Minute), "minute")
	default:
		return plural(int64(d/time.Second), "second")
	}
}
