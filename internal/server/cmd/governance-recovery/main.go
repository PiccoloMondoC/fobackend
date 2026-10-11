// focodebase/fobackend/internal/server/cmd/governance-recovery/main.go
// Reviewable operator-only recovery and honorary consulting ceremonies.
package main
import (
 "context"
 "crypto/ed25519"
 "encoding/hex"
 "encoding/json"
 "flag"
 "io"
 "fmt"
 "os"
 "strings"
 "github.com/PiccoloMondoC/focodebase/fobackend/internal/bootstrap"
 "github.com/PiccoloMondoC/focodebase/fobackend/internal/data"
 "github.com/PiccoloMondoC/focodebase/fobackend/internal/governancerecovery"
 "github.com/PiccoloMondoC/focodebase/fobackend/internal/logging"
 "github.com/google/uuid"
 "github.com/joho/godotenv"
)
func main(){os.Exit(run())}
func run() int {
 if len(os.Args)<2 || (os.Args[1]!="plan" && os.Args[1]!="execute") {fmt.Fprintln(os.Stderr,"usage: sagrenti-recovery plan|execute --request /path/request.json [--plan-digest SHA256 --ceremony-id UUID]");return 2}
 flags:=flag.NewFlagSet("governance-recovery",flag.ContinueOnError)
 path:=flags.String("request","","Board/CIO-authorized ceremony request JSON")
 cioSignaturePath:=flags.String("cio-signature","","detached CIO signature file (hex)")
 boardSignaturePath:=flags.String("board-signature","","detached Board signature file (hex); recovery only")
 approvalPath:=flags.String("approval-message-out","","write exact approval message for external signing (plan only)")
 ceremonyForPlan:=flags.String("ceremony-id","","unique ceremony UUID retained for approval and retries")
 digest:=flags.String("plan-digest","","reviewed plan digest")
 ceremony:=ceremonyForPlan
 if flags.Parse(os.Args[2:])!=nil || flags.NArg()!=0 || *path=="" {return 2}
 f,err:=os.Open(*path);if err!=nil{fmt.Fprintln(os.Stderr,"cannot read authorization request");return 2};defer f.Close()
 info,err:=f.Stat();if err!=nil || info.Size()>65536{fmt.Fprintln(os.Stderr,"authorization request exceeds permitted size");return 2}
 decoder:=json.NewDecoder(io.LimitReader(f,65537));decoder.DisallowUnknownFields();var request governancerecovery.Request
 if err:=decoder.Decode(&request);err!=nil{fmt.Fprintln(os.Stderr,"invalid request JSON");return 2}
 var trailing any; if decoder.Decode(&trailing)!=io.EOF{fmt.Fprintln(os.Stderr,"request must contain exactly one JSON object");return 2}
 if err:=governancerecovery.ValidateRequest(request);err!=nil{fmt.Fprintln(os.Stderr,err);return 2}
 _=godotenv.Load(".env")
 cfg,err:=bootstrap.LoadDatabaseConfig();if err!=nil{fmt.Fprintln(os.Stderr,"invalid operator database configuration:",err);return 1}
 logger,err:=logging.InitLogging("governance-recovery");if err!=nil{return 1}
 model:=&data.DBConnectionParamsModel{Logger:logger};pool,err:=model.ConnectWithConnector(cfg);if err!=nil{fmt.Fprintln(os.Stderr,"operator connection failed");return 1};defer pool.Close()
 ctx,cancel:=context.WithTimeout(context.Background(),cfg.DBTimeout);defer cancel()
 if os.Args[1]=="plan" {
  plan,err:=governancerecovery.Prepare(ctx,pool,logger,request);if err!=nil{fmt.Fprintln(os.Stderr,"plan refused:",err);return 1}
  if *approvalPath!="" {
   id,err:=uuid.Parse(*ceremony);if err!=nil{fmt.Fprintln(os.Stderr,"ceremony UUID required for approval message");return 2}
   message,err:=governancerecovery.ApprovalMessage(request,plan.Digest,id);if err!=nil{return 1}
   if err:=os.WriteFile(*approvalPath,message,0600);err!=nil{fmt.Fprintln(os.Stderr,"cannot write approval message");return 1}
  }
  out:=json.NewEncoder(os.Stdout);out.SetIndent("","  ");if out.Encode(plan)!=nil{return 1};return 0
 }
 id,err:=uuid.Parse(*ceremony);if err!=nil{fmt.Fprintln(os.Stderr,"valid ceremony UUID required");return 2}
 cioKey,err:=readHexFile(os.Getenv("SAGRENTI_CIO_APPROVAL_PUBLIC_KEY_FILE"));if err!=nil{fmt.Fprintln(os.Stderr,"trusted CIO public key is unavailable");return 1}
 cioSignature,err:=readHexFile(*cioSignaturePath);if err!=nil{fmt.Fprintln(os.Stderr,"CIO approval signature is unavailable");return 1}
 var boardKey,boardSignature []byte
 if request.Operation=="recovery" {
  boardKey,err=readHexFile(os.Getenv("SAGRENTI_BOARD_APPROVAL_PUBLIC_KEY_FILE"));if err!=nil{fmt.Fprintln(os.Stderr,"trusted Board public key is unavailable");return 1}
  boardSignature,err=readHexFile(*boardSignaturePath);if err!=nil{fmt.Fprintln(os.Stderr,"Board approval signature is unavailable");return 1}
 }
 approval,err:=governancerecovery.VerifyApproval(request,*digest,id,ed25519.PublicKey(boardKey),ed25519.PublicKey(cioKey),boardSignature,cioSignature)
 if err!=nil{fmt.Fprintln(os.Stderr,err);return 1}
 repeated,err:=governancerecovery.Execute(ctx,pool,logger,request,*digest,id,approval)
 if err!=nil{fmt.Fprintln(os.Stderr,"execution not confirmed:",err,"; inspect state or repeat the identical request with the SAME ceremony ID and digest");return 1}
 fmt.Println("ceremony:",id,"completed; already recorded:",repeated);return 0
}

func readHexFile(path string)([]byte,error){raw,err:=os.ReadFile(path);if err!=nil{return nil,err};return hex.DecodeString(strings.TrimSpace(string(raw)))}
