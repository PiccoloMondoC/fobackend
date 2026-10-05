// Package notificationservices owns shared notification-service contracts,
// the application-facing dependency container, and validation helpers used
// by channel-specific notification packages.
//
// focodebase/fobackend/internal/notification_services/password_reset.go
//
// GTM:
//
//	Layer: 2.3 Consumer Domain
//	Release Class: SPINE
//	Reason:
//	  Password-reset message content is release-critical credential-recovery
//	  communication. This file owns the provider-neutral subject and body of
//	  the password-reset email and validates the reset link under the same
//	  explicit credential-link policy used for account activation.
//
//	  The message mirrors email confirmation: multipart text + HTML, with
//	  the HTML part carrying the reset link only in a button href so the
//	  credential is not printed as visible copy. Password reset has no
//	  manual-code fallback, so the plain-text alternative must carry the
//	  link itself; that is the only place the credential appears as text,
//	  and only because a text-only reader has no other way to use it.
//	  Delivery uses EmailSender.SendPasswordResetContext.
//
// SPINE Rule:
//
//	Keep compiling.
//	Keep production-ready.
//	Preserve strict HTTPS reset-link validation by default, with explicit
//	policy opt-in for dev/test loopback HTTP only.
//	Preserve user-info, fragment, and decoded traversal rejection by reusing
//	ValidateActivationURLWithPolicy, the canonical credential-link validator.
//	Never log the reset URL or the composed body; both carry bearer material.
//	Keep message copy non-enumerating: it must not reveal anything the public
//	reset-request response withholds.
//	Block deployment if this file breaks reset-link validation or
//	password-reset message composition.
package notificationservices

import (
	"bytes"
	"errors"
	"html/template"
	"strings"
	"time"
)

const (
	// FlowPasswordReset identifies password-reset delivery in notification
	// observability metadata.
	FlowPasswordReset = "password_reset"

	// PasswordResetSubject is the subject of the password-reset message.
	PasswordResetSubject = "Reset your Sagrenti password"
)

// ErrInvalidPasswordResetURL reports that the reset link failed credential-link
// validation.
var ErrInvalidPasswordResetURL = errors.New("notification: password reset URL is invalid")

// PasswordResetContent is the input to the password-reset message.
//
// ResetURL carries plaintext bearer material; never log, audit, or persist it.
type PasswordResetContent struct {
	// ResetURL is the browser reset link (FrontendURL + /reset-password?token=…).
	ResetURL string

	// ValidFor is the configured reset-credential lifetime. Zero omits the
	// expiry sentence rather than producing vague or broken copy.
	ValidFor time.Duration
}

// PasswordResetMessage is the composed password-reset email.
type PasswordResetMessage struct {
	Subject string
	Text    string
	HTML    string
}

// ComposePasswordResetMessage validates the reset link under policy and
// renders the subject, plain-text part, and HTML part.
//
// Copy is non-enumerating and free of internal terminology (no "token").
func ComposePasswordResetMessage(
	content PasswordResetContent,
	policy ActivationURLPolicy,
) (PasswordResetMessage, error) {
	var none PasswordResetMessage

	resetURL, err := ValidateActivationURLWithPolicy(content.ResetURL, policy)
	if err != nil {
		return none, ErrInvalidPasswordResetURL
	}

	view := passwordResetView{
		ResetURL: template.URL(resetURL),
		Expiry:   humanizeDuration(content.ValidFor),
	}

	var html bytes.Buffer
	if err := passwordResetHTML.Execute(&html, view); err != nil {
		return none, ErrInvalidPasswordResetURL
	}

	return PasswordResetMessage{
		Subject: PasswordResetSubject,
		Text:    composePasswordResetText(resetURL, view.Expiry),
		HTML:    html.String(),
	}, nil
}

type passwordResetView struct {
	ResetURL template.URL
	Expiry   string
}

func composePasswordResetText(resetURL, expiry string) string {
	var b strings.Builder

	b.WriteString("Reset your Sagrenti password\n\n")
	b.WriteString("We received a request to reset the password for your Sagrenti account.\n\n")
	b.WriteString("Open this link to choose a new password:\n")
	b.WriteString(resetURL)
	b.WriteString("\n\n")

	if expiry != "" {
		b.WriteString("The link works once and expires in " + expiry + ".\n")
	} else {
		b.WriteString("The link works once.\n")
	}

	b.WriteString("Choosing a new password signs your account out on every device.\n\n")
	b.WriteString("If you requested more than one email, only the newest one works.\n\n")
	b.WriteString("If you didn't ask to reset your password, you can ignore this email. Your password hasn't changed.\n")

	return b.String()
}

var passwordResetHTML = template.Must(template.New("password-reset").Parse(`<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Reset your Sagrenti password</title>
</head>
<body style="margin:0;padding:0;background:#f7f6f2;">
<table role="presentation" width="100%" cellspacing="0" cellpadding="0" border="0" style="background:#f7f6f2;">
<tr>
<td align="center" style="padding:32px 16px;">
<table role="presentation" width="100%" cellspacing="0" cellpadding="0" border="0" style="max-width:520px;background:#ffffff;border:1px solid #dce2e6;border-radius:8px;">
<tr>
<td style="padding:32px 32px 8px 32px;font-family:Inter,'Segoe UI',Helvetica,Arial,sans-serif;color:#1b2733;">
<p style="margin:0 0 24px 0;font-size:14px;font-weight:600;color:#14263d;">Sagrenti</p>
<h1 style="margin:0 0 12px 0;font-size:22px;line-height:1.3;font-weight:600;color:#14263d;">Reset your password</h1>
<p style="margin:0 0 24px 0;font-size:16px;line-height:1.5;">We received a request to reset the password for your Sagrenti account.</p>
<table role="presentation" cellspacing="0" cellpadding="0" border="0" style="margin:0 0 16px 0;">
<tr>
<td style="border-radius:6px;background:#14263d;">
<a href="{{.ResetURL}}" style="display:inline-block;padding:12px 24px;font-family:Inter,'Segoe UI',Helvetica,Arial,sans-serif;font-size:16px;font-weight:600;color:#ffffff;text-decoration:none;border-radius:6px;">Choose a new password</a>
</td>
</tr>
</table>
<p style="margin:0 0 24px 0;font-size:14px;line-height:1.5;color:#56636e;">{{if .Expiry}}The button works once and expires in {{.Expiry}}.{{else}}The button works once.{{end}} Choosing a new password signs your account out on every device.</p>
<hr style="border:none;border-top:1px solid #dce2e6;margin:0 0 24px 0;">
<p style="margin:0 0 8px 0;font-size:14px;line-height:1.5;color:#56636e;">If you requested more than one email, only the newest one works.</p>
<p style="margin:0 0 24px 0;font-size:14px;line-height:1.5;color:#56636e;">If you didn't ask to reset your password, you can ignore this email. Your password hasn't changed.</p>
</td>
</tr>
</table>
</td>
</tr>
</table>
</body>
</html>
`))
