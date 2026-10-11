// focodebase/fobackend/internal/data/platform_installation_test.go
package data
import (
 "bytes"
 "crypto/sha256"
 "errors"
 "strings"
 "testing"
 "github.com/jackc/pgx/v5/pgconn"
)
func TestBootstrapCredentialSecurity(t *testing.T) {
 seen:=make(map[string]bool)
 for i:=0;i<64;i++ {
  raw,err:=GenerateBootstrapCredential();if err!=nil {t.Fatal(err)}
  if !WellFormedBootstrapCredential(raw) || seen[raw] {t.Fatal("malformed or reused credential")};seen[raw]=true
  want:=sha256.Sum256([]byte(raw))
  if !bytes.Equal(BootstrapCredentialHash(raw),want[:]) {t.Fatal("credential hashing mismatch")}
 }
 for _,bad:=range []string{"",strings.Repeat("a",42),strings.Repeat("a",44),strings.Repeat("a",42)+"=",strings.Repeat("a",42)+" ",strings.Repeat("a",42)+"\n"} {
  if WellFormedBootstrapCredential(bad) {t.Fatal("malformed credential accepted")}
 }
 if !errors.Is(TranslateGovernanceError(&pgconn.PgError{Code:"SG006",Message:"must not expose detail"}),ErrInstallationProtected) {t.Fatal("installation SQLSTATE not translated")}
}
