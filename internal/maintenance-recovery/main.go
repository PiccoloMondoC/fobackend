// Infrastructure operator executable. Never linked into or exposed by the API.
// Trust keys are read from a protected directory selected by infrastructure.
package main

import (
 "context"
 "crypto/ed25519"
 "encoding/hex"
 "encoding/json"
 "errors"
 "flag"
 "fmt"
 "io"
 "os"
 "path/filepath"
 "strings"
 "time"

 "github.com/PiccoloMondoC/focodebase/fobackend/internal/governancerecovery"
 "github.com/PiccoloMondoC/focodebase/fobackend/internal/logging"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5/pgxpool"
)
func readJSON(path string,v any)error{
 file,err:=os.Open(path);if err!=nil{return err};defer file.Close()
 info,err:=file.Stat();if err!=nil{return err};if info.Size()>65536{return errors.New("input exceeds 64 KiB")}
 d:=json.NewDecoder(file);d.DisallowUnknownFields();if err:=d.Decode(v);err!=nil{return err}
 var extra any;if err:=d.Decode(&extra);err==nil{return errors.New("trailing JSON") }else if !errors.Is(err,io.EOF){return err};return nil
}
func hexFile(path string,n int)([]byte,error){
 info,err:=os.Stat(path);if err!=nil || !info.Mode().IsRegular() || info.Mode().Perm()&0022!=0{return nil,errors.New("key/signature file must be regular and not group/world writable")}
 raw,err:=os.ReadFile(path);if err!=nil{return nil,err};v,err:=hex.DecodeString(strings.TrimSpace(string(raw)));if err!=nil || len(v)!=n{return nil,errors.New("invalid hex key/signature")};return v,nil
}
func run()error{
 mode:=flag.String("mode","prepare","prepare, message, or execute")
 request:=flag.String("request","","JSON recovery request file")
 digest:=flag.String("digest","","reviewed plan digest")
 ceremony:=flag.String("ceremony","","ceremony UUID")
 boardSignature:=flag.String("board-signature","","hex detached signature file")
 cioSignature:=flag.String("cio-signature","","hex detached signature file")
 flag.Parse()
 var r governancerecovery.MaintenanceRecoveryRequest;if err:=readJSON(*request,&r);err!=nil{return err}
 id,_:=uuid.Parse(*ceremony)
 if *mode=="message"{message,err:=governancerecovery.MaintenanceApprovalMessage(r,*digest,id);if err!=nil{return err};_,err=os.Stdout.Write(message);return err}
 if *mode!="prepare" && *mode!="execute"{return errors.New("invalid mode")}
 ctx,cancel:=context.WithTimeout(context.Background(),30*time.Second);defer cancel()
 dsn:=os.Getenv("MAINTENANCE_RECOVERY_DATABASE_URL");if dsn==""{return errors.New("infrastructure database configuration missing")}
 pool,err:=pgxpool.New(ctx,dsn);if err!=nil{return errors.New("infrastructure database configuration invalid")};defer pool.Close()
 if *mode=="prepare"{plan,err:=governancerecovery.PrepareMaintenance(ctx,pool,r);if err!=nil{return err};return json.NewEncoder(os.Stdout).Encode(plan)}
 trust:=os.Getenv("MAINTENANCE_RECOVERY_TRUST_DIR");if !filepath.IsAbs(trust){return errors.New("protected absolute trust directory required")}
 info,err:=os.Stat(trust);if err!=nil || !info.IsDir() || info.Mode().Perm()&0022!=0{return errors.New("trust directory must not be group/world writable")}
 board,err:=hexFile(filepath.Join(trust,"board.pub"),ed25519.PublicKeySize);if err!=nil{return err}
 cio,err:=hexFile(filepath.Join(trust,"cio.pub"),ed25519.PublicKeySize);if err!=nil{return err}
 bs,err:=hexFile(*boardSignature,ed25519.SignatureSize);if err!=nil{return err}
 cs,err:=hexFile(*cioSignature,ed25519.SignatureSize);if err!=nil{return err}
 approval,err:=governancerecovery.VerifyMaintenanceApproval(r,*digest,id,board,cio,bs,cs);if err!=nil{return err}
 logger,err:=logging.InitLogging("maintenance-recovery");if err!=nil{return err};defer logger.Sync()
 repeated,err:=governancerecovery.ExecuteMaintenance(ctx,pool,logger,r,*digest,id,approval);if err!=nil{return err}
 return json.NewEncoder(os.Stdout).Encode(map[string]bool{"restored":true,"already_executed":repeated})
}
func main(){if err:=run();err!=nil{fmt.Fprintln(os.Stderr,"maintenance recovery refused or outcome unconfirmed; inspect the current state and retained evidence before retrying");os.Exit(1)}}
