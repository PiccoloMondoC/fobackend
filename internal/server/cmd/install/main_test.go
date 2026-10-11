// focodebase/fobackend/internal/server/cmd/install/main_test.go
package main
import (
 "bytes"
 "strings"
 "testing"
)
func TestInstallerReadsIdentityOnlyInteractively(t *testing.T) {
 for _,input:=range []string{"", "one@example.com\ntwo@example.com\n", "name <root@example.com>\nname <root@example.com>\n"} {
  var output bytes.Buffer
  if _,err:=readConfirmedAddress(strings.NewReader(input),&output);err==nil {t.Fatal("invalid identity accepted")}
 }
 var output bytes.Buffer
 email,err:=readConfirmedAddress(strings.NewReader("root@example.com\nroot@example.com\n"),&output)
 if err!=nil || email!="root@example.com" {t.Fatal("matching input refused")}
 if strings.Contains(output.String(),email) {t.Fatal("installer echoes identity")}
 for _,args:=range [][]string{{"issue-root-claim","root@example.com"},{"issue-root-claim","-deliver=terminal"},{"status","extra"}} {
  var diagnostics bytes.Buffer
  if code:=run(args,strings.NewReader(""),&output,&diagnostics);code!=2 {t.Fatal("unsafe command accepted")}
 }
}
