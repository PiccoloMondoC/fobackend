// focodebase/fobackend/internal/notification_services/email/root_bootstrap_test.go
package email
import (
 "context"
 "strings"
 "testing"
 "time"
 notificationservices "github.com/PiccoloMondoC/focodebase/fobackend/internal/notification_services"
)
func TestRootBootstrapUsesCentralEmailBoundary(t *testing.T) {
 provider:=&recordingProvider{}
 service:=newTestService(t,provider,notificationservices.ActivationURLPolicy{})
 content:=notificationservices.RootBootstrapContent{CompletionURL:"https://example.com/installation/complete#"+strings.Repeat("a",43),ExpiresAt:time.Now().Add(30*time.Minute)}
 if err:=service.SendRootBootstrapInvitationContext(context.Background(),"root@example.com",content);err!=nil {t.Fatal(err)}
 if len(provider.messages)!=1 {t.Fatal("invitation not delivered")}
 msg:=provider.messages[0]
 if msg.HTMLBody=="" || strings.Contains(msg.Body,strings.Repeat("a",43)) {t.Fatal("invitation content boundary violated")}
 _,_,payload,err:=prepareSMTPMessage(msg)
 if err!=nil || !strings.Contains(string(payload),"multipart/alternative") {t.Fatal("SMTP invitation is not multipart")}
 if err:=service.SendRootBootstrapInvitationContext(nil,"root@example.com",content);err==nil {t.Fatal("nil context accepted")}
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if err:=service.SendRootBootstrapInvitationContext(ctx,"root@example.com",content);err==nil {t.Fatal("canceled delivery accepted")}
}
