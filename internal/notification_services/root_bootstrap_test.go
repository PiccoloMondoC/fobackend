// focodebase/fobackend/internal/notification_services/root_bootstrap_test.go
package notificationservices
import (
 "regexp"
 "strings"
 "testing"
 "time"
)
func TestRootBootstrapInvitationPrivacyAndURLPolicy(t *testing.T) {
 raw:=strings.Repeat("a",43)
 content:=RootBootstrapContent{CompletionURL:"https://example.com/installation/complete#"+raw,ExpiresAt:time.Date(2026,10,10,8,0,0,0,time.UTC)}
 message,err:=ComposeRootBootstrapInvitation(content,ActivationURLPolicy{});if err!=nil {t.Fatal(err)}
 if strings.Contains(message.Text,raw) || strings.Contains(regexp.MustCompile(`<[^>]+>`).ReplaceAllString(message.HTML,""),raw) {t.Fatal("credential visible in email copy")}
 if !strings.Contains(message.HTML,`href="`+content.CompletionURL+`"`) {t.Fatal("no clickable invitation")}
 for _,bad:=range []string{
  "http://example.com/installation/complete#"+raw,
  "https://user@example.com/installation/complete#"+raw,
  "https://example.com/installation/complete?token="+raw,
  "https://example.com/installation/complete?x=1#"+raw,
  "https://example.com/other#"+raw,
  "https://example.com/installation/%63omplete#"+raw,
  "https://example.com/installation/complete#short",
  "https://example.com/installation/complete#"+raw+"%0a",
 } {
  content.CompletionURL=bad
  if _,err:=ComposeRootBootstrapInvitation(content,ActivationURLPolicy{});err==nil {t.Fatal("unsafe invitation accepted")}
 }
 content.CompletionURL="http://localhost:4200/installation/complete#"+raw
 if _,err:=ComposeRootBootstrapInvitation(content,ActivationURLPolicy{});err==nil {t.Fatal("HTTP allowed without explicit loopback policy")}
 if _,err:=ComposeRootBootstrapInvitation(content,ActivationURLPolicy{AllowHTTPOnLoopback:true});err!=nil {t.Fatal("dev loopback refused")}
}
