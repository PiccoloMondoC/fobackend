// focodebase/fobackend/internal/server/cmd/install/main.go
// Operator-only issuance of installation entitlement. The API has no issuance
// route. No email flags, environment identity, printed link, or secret logging.
package main
import (
 "bufio"
 "context"
 "errors"
 "fmt"
 "io"
 "os"
 "strings"
 "time"
 "github.com/PiccoloMondoC/focodebase/fobackend/internal/bootstrap"
 "github.com/PiccoloMondoC/focodebase/fobackend/internal/data"
 "github.com/PiccoloMondoC/focodebase/fobackend/internal/installation"
 "github.com/PiccoloMondoC/focodebase/fobackend/internal/logging"
 notificationservices "github.com/PiccoloMondoC/focodebase/fobackend/internal/notification_services"
 notificationruntime "github.com/PiccoloMondoC/focodebase/fobackend/internal/notification_services/runtime"
 "github.com/joho/godotenv"
)
func main() { os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr)) }
func run(args []string, input io.Reader, output, diagnostics io.Writer) int {
 if len(args) != 1 || (args[0] != "status" && args[0] != "issue-root-claim") {
  fmt.Fprintln(diagnostics, "usage: install status | install issue-root-claim"); return 2
 }
 // Same established local environment loading as the API; no identity is read.
 _ = godotenv.Load(".env")
 var cfg *bootstrap.Config
 var err error
 if args[0] == "status" { cfg, err = bootstrap.LoadDatabaseConfig() } else { cfg, err = bootstrap.LoadInstallationConfig() }
 if err != nil { fmt.Fprintln(diagnostics, "installation configuration is invalid:", err); return 1 }
 logger, err := logging.InitLogging("install")
 if err != nil { fmt.Fprintln(diagnostics, "installation logger could not be initialized"); return 1 }
 model := &data.DBConnectionParamsModel{Logger: logger}
 pool, err := model.ConnectWithConnector(cfg)
 if err != nil { fmt.Fprintln(diagnostics, "installation database connection failed"); return 1 }
 defer pool.Close()
 ctx, cancel := context.WithTimeout(context.Background(), cfg.DBTimeout)
 status, err := installation.ReadStatus(ctx, pool, logger)
 cancel()
 if err != nil { fmt.Fprintln(diagnostics, "installation state could not be read"); return 1 }
 if status.Installation != nil {
  fmt.Fprintln(output, "initialized: yes")
  if args[0] == "issue-root-claim" { fmt.Fprintln(diagnostics, "already initialized; no invitation issued"); return 3 }
  return 0
 }
 if args[0] == "status" {
  fmt.Fprintln(output, "initialized: no")
  if status.PendingClaim != nil { fmt.Fprintln(output, "pending invitation expires:", status.PendingClaim.ExpiresAt.UTC().Format(time.RFC3339)) }
  return 0
 }
 // Construct the actual centralized delivery boundary before issuing entitlement.
 notifications, err := notificationruntime.NewServices(cfg, logger)
 if err != nil { fmt.Fprintln(diagnostics, "installation email delivery could not be configured"); return 5 }
 sender, ok := notifications.Email.(notificationservices.RootBootstrapInvitationSender)
 if !ok { fmt.Fprintln(diagnostics, "configured email service cannot deliver installation invitations"); return 5 }
 email, err := readConfirmedAddress(input, output)
 if err != nil { fmt.Fprintln(diagnostics, err); return 4 }
 ctx, cancel = context.WithTimeout(context.Background(), cfg.DBTimeout)
 claim, err := installation.IssueRootBootstrapClaim(ctx, pool, logger, email)
 cancel()
 if errors.Is(err, data.ErrInstallationAlreadyInitialized) { fmt.Fprintln(diagnostics, "already initialized; no invitation issued"); return 3 }
 if err != nil { fmt.Fprintln(diagnostics, "installation invitation could not be recorded; check installation state before retrying"); return 1 }
 // Issuance commits before delivery. Failure or an uncertain SMTP acknowledgement
 // never rolls back a sent entitlement; reissue supersedes it atomically.
 ctx, cancel = context.WithTimeout(context.Background(), 30*time.Second)
 err = sender.SendRootBootstrapInvitationContext(ctx, email, notificationservices.RootBootstrapContent{CompletionURL: claim.CompletionURL(cfg.FrontendURL), ExpiresAt: claim.ExpiresAt})
 cancel()
 if err != nil { fmt.Fprintln(diagnostics, "invitation recorded, but email delivery could not be confirmed; run issue-root-claim again for a replacement"); return 5 }
 fmt.Fprintln(output, "Installation invitation sent. It expires:", claim.ExpiresAt.UTC().Format(time.RFC3339))
 if claim.Superseded { fmt.Fprintln(output, "The previous invitation no longer works.") }
 return 0
}
func readConfirmedAddress(input io.Reader, output io.Writer) (string, error) {
 reader := bufio.NewReader(input)
 read := func(prompt string) (string, error) {
  fmt.Fprint(output, prompt)
  line, err := reader.ReadString('\n')
  if err != nil && !(errors.Is(err, io.EOF) && line != "") { return "", errors.New("no address entered") }
  return strings.TrimSpace(line), nil
 }
 first, err := read("Initial Root Super Admin email address: ")
 if err != nil { return "", err }
 second, err := read("Enter the same address again: ")
 if err != nil { return "", err }
 if first != second { return "", errors.New("addresses do not match; no invitation issued") }
 return installation.NormalizeClaimantEmail(first)
}
