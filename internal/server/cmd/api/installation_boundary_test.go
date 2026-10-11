// focodebase/fobackend/internal/server/cmd/api/installation_boundary_test.go
package main
import (
 "go/parser"
 "go/token"
 "os"
 "path/filepath"
 "strconv"
 "strings"
 "testing"
)
func TestAPIHasNoInstallationIssuerDependency(t *testing.T) {
 // Walk actual local production imports, not test imports or go-list output.
 root:=filepath.Join("..","..","..")
 prefix:="github.com/PiccoloMondoC/focodebase/fobackend/internal/"
 queue:=[]string{"server/cmd/api"};seen:=map[string]bool{}
 for len(queue)>0 {
  dir:=queue[0];queue=queue[1:]
  if seen[dir] {continue};seen[dir]=true
  if dir=="installation" || dir=="server/cmd/install" || dir=="governancerecovery" {t.Fatalf("API links operator authority: %s",dir)}
  entries,err:=os.ReadDir(filepath.Join(root,dir));if err!=nil {t.Fatal(err)}
  for _,entry:=range entries {
   if entry.IsDir() || !strings.HasSuffix(entry.Name(),".go") || strings.HasSuffix(entry.Name(),"_test.go") {continue}
   path:=filepath.Join(root,dir,entry.Name())
   file,err:=parser.ParseFile(token.NewFileSet(),path,nil,parser.ImportsOnly);if err!=nil {t.Fatal(err)}
   for _,imp:=range file.Imports {
    name,err:=strconv.Unquote(imp.Path.Value);if err!=nil {t.Fatal(err)}
    if strings.HasPrefix(name,prefix) {queue=append(queue,strings.TrimPrefix(name,prefix))}
   }
  }
 }
}
func TestInstallationStartupAndRouteBoundary(t *testing.T) {
 source,err:=os.ReadFile("main.go");if err!=nil {t.Fatal(err)}
 for _,forbidden:=range []string{"EstablishRootSuperAdminInternal", "IssueRootBootstrapClaim", "RootSuperAdminBootstrapEmail", "ReconcileAdministrativeAppointmentsInternal"} {
  if strings.Contains(string(source),forbidden) {t.Fatalf("startup establishes authority through %s",forbidden)}
 }
 routes,err:=os.ReadFile("routes.go");if err!=nil {t.Fatal(err)}
 if !strings.Contains(string(routes),`v1.With(app.RateLimitMiddleware).Post("/installation/root-bootstrap/complete", app.CompleteRootBootstrapHandler)`) || !strings.Contains(string(routes),`app.registerPublic("POST", "/api/v1/installation/root-bootstrap/complete")`) {t.Fatal("completion is not a registered rate-limited public exchange")}
 if strings.Contains(string(routes),"IssueRootBootstrap") || strings.Contains(string(routes),"issue-root-claim") {t.Fatal("public installer authority")}
}
