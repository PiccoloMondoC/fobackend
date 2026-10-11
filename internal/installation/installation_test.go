// focodebase/fobackend/internal/installation/installation_test.go
package installation
import (
 "strings"
 "testing"
 "time"
)
func TestClaimantAddressAndLifetime(t *testing.T) {
 if ClaimValidity != 30*time.Minute {t.Fatal("unexpected privileged enrollment lifetime")}
 for _,bad:=range []string{"", "not-an-email", "name <root@example.com>", "one@example.com,two@example.com", "root@example.com\nBcc: attacker@example.com", "root@localhost",strings.Repeat("a",250)+"@example.com"} {
  if _,err:=NormalizeClaimantEmail(bad);err==nil {t.Fatalf("accepted %q",bad)}
 }
 email,err:=NormalizeClaimantEmail(" root@example.com ");if err!=nil || email!="root@example.com" {t.Fatal("plain mailbox rejected")}
 claim:=IssuedClaim{Credential:strings.Repeat("a",43)}
 if got:=claim.CompletionURL("https://example.com/");got!="https://example.com/installation/complete#"+claim.Credential {t.Fatal("link does not isolate credential in fragment")}
}
