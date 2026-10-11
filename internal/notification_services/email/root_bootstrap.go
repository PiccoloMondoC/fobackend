// focodebase/fobackend/internal/notification_services/email/root_bootstrap.go
// Installation invitations reuse the canonical email service and SMTP provider.
package email
import (
 "context"
 notificationservices "github.com/PiccoloMondoC/focodebase/fobackend/internal/notification_services"
)
func (e *EmailService) SendRootBootstrapInvitationContext(ctx context.Context, recipient string, content notificationservices.RootBootstrapContent) error {
 if ctx == nil { return ErrNilContext }
 if e == nil || e.provider == nil { return ErrEmailProviderNotConfigured }
 message, err := notificationservices.ComposeRootBootstrapInvitation(content, e.policy)
 if err != nil { return err }
 return e.send(ctx, recipient, message.Subject, message.Text, message.HTML)
}
var _ notificationservices.RootBootstrapInvitationSender = (*EmailService)(nil)
