// focodebase/fobackend/internal/notification_services/root_bootstrap.go
// Provider-neutral installation invitation. Bearer material appears only in the
// secure HTML button href, never as visible copy or a separately printed token.
package notificationservices

import (
 "bytes"
 "context"
 "errors"
 "html/template"
 "net/url"
 "strings"
 "time"
)

// RootBootstrapInvitationSender is an optional, narrow notification capability.
// Existing EmailSender implementations and ordinary authentication are unchanged.
type RootBootstrapInvitationSender interface {
 SendRootBootstrapInvitationContext(context.Context, string, RootBootstrapContent) error
}
type RootBootstrapContent struct { CompletionURL string; ExpiresAt time.Time }
type RootBootstrapMessage struct { Subject, Text, HTML string }
var ErrInvalidRootBootstrapInvitation = errors.New("notification: invalid installation invitation")

func ComposeRootBootstrapInvitation(content RootBootstrapContent, policy ActivationURLPolicy) (RootBootstrapMessage, error) {
 none := RootBootstrapMessage{}
 parsed, err := url.Parse(content.CompletionURL)
 if err != nil || parsed == nil || parsed.RawQuery != "" || parsed.ForceQuery || parsed.Path != "/installation/complete" || parsed.RawPath != "" || content.ExpiresAt.IsZero() { return none, ErrInvalidRootBootstrapInvitation }
 credential := parsed.Fragment
 if len(credential) != 43 || strings.ContainsAny(credential, " \t\r\n") { return none, ErrInvalidRootBootstrapInvitation }
 for _, r := range credential {
  if !(r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '_' || r == '-') { return none, ErrInvalidRootBootstrapInvitation }
 }
 parsed.Fragment = ""; parsed.RawFragment = ""
 base, err := ValidateActivationURLWithPolicy(parsed.String(), policy)
 if err != nil { return none, ErrInvalidRootBootstrapInvitation }
 link := base + "#" + credential
 view := struct { Link template.URL; Expires string }{template.URL(link), content.ExpiresAt.UTC().Format("2 Jan 2006 at 15:04 UTC")}
 var html bytes.Buffer
 if err := rootBootstrapHTML.Execute(&html, view); err != nil { return none, ErrInvalidRootBootstrapInvitation }
 return RootBootstrapMessage{
  Subject: "Complete your Sagrenti installation",
  Text: "You have been invited to complete the Sagrenti installation as its initial Root Super Admin.\n\nUse the Complete installation button in the HTML version of this email. Enter your existing account password, or choose one for a new or unconfirmed account.\n\nThe link works once and expires on " + view.Expires + ". Only the newest invitation works.\n\nIf your email reader cannot display the button, open this email in an HTML-capable reader or ask the installer to send a new invitation.\n\nIf you were not expecting this invitation, do not use it. Contact the installer.\n",
  HTML: html.String(),
 }, nil
}
var rootBootstrapHTML = template.Must(template.New("root-bootstrap").Parse(`<!DOCTYPE html>
<html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1"><title>Complete installation</title></head>
<body style="font-family:Arial,sans-serif;color:#14263d;background:#f7f6f2;padding:24px">
<div style="max-width:520px;margin:auto;background:white;padding:32px;border-radius:8px">
<p>Sagrenti</p><h1>Complete installation</h1>
<p>You have been invited to complete the installation as its initial Root Super Admin.</p>
<p>Enter your existing account password, or choose one for a new or unconfirmed account.</p>
<p><a href="{{.Link}}" style="display:inline-block;background:#14263d;color:white;padding:12px 20px;text-decoration:none;border-radius:6px">Complete installation</a></p>
<p>The link works once and expires on {{.Expires}}. Only the newest invitation works.</p>
<p>If you were not expecting this invitation, do not use it. Contact the installer.</p>
</div></body></html>`))
