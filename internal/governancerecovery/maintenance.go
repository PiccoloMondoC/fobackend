// Infrastructure-only maintenance recovery. No HTTP package imports this file.
// Detached Board/CIO approvals bind a maintenance-specific domain, full reviewed
// state, operator request and ceremony UUID. Recovery changes no Root standing.
//
// focodebase/fobackend/internal/governancerecovery/maintenance.go
package governancerecovery

import (
 "context"
 "crypto/ed25519"
 "crypto/sha256"
 "encoding/hex"
 "encoding/json"
 "errors"
 "fmt"
 "strings"
 "unicode"
 "unicode/utf8"

 "github.com/PiccoloMondoC/focodebase/fobackend/internal/data"
 "github.com/PiccoloMondoC/focodebase/fobackend/internal/logging"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "github.com/jackc/pgx/v5/pgxpool"
)

type MaintenanceRecoveryRequest struct {
 Reference string `json:"reference"`
 Executor string `json:"executor"`
 Reason string `json:"reason"`
}
type MaintenanceRecoveryPlan struct {
 Request MaintenanceRecoveryRequest `json:"request"`
 Version int64 `json:"version"`
 State data.MaintenanceDocument `json:"state"`
 Digest string `json:"digest"`
}
type MaintenanceApproval struct { evidence ApprovalEvidence; messageHash string }
func validMaintenanceRecovery(r MaintenanceRecoveryRequest) bool {
 for _,v:=range []string{r.Reference,r.Executor,r.Reason}{
  if v=="" || v!=strings.TrimSpace(v) || !utf8.ValidString(v) || utf8.RuneCountInString(v)>200{return false}
  for _,c:=range v{if unicode.IsControl(c){return false}}
 }
 return true
}
func maintenanceRecoveryPlan(r MaintenanceRecoveryRequest,s *data.MaintenanceState)(*MaintenanceRecoveryPlan,error){
 if s==nil || !s.Active{return nil,errors.New("maintenance is not active")}
 p:=&MaintenanceRecoveryPlan{Request:r,Version:s.Version,State:s.MaintenanceDocument}
 raw,err:=json.Marshal(struct{Domain string;Plan *MaintenanceRecoveryPlan}{"platform-maintenance-recovery-v1",p});if err!=nil{return nil,err}
 h:=sha256.Sum256(raw);p.Digest=hex.EncodeToString(h[:]);return p,nil
}
func PrepareMaintenance(ctx context.Context,pool *pgxpool.Pool,r MaintenanceRecoveryRequest)(*MaintenanceRecoveryPlan,error){
 if ctx==nil || pool==nil || !validMaintenanceRecovery(r){return nil,errors.New("valid recovery request and infrastructure database required")}
 var plan *MaintenanceRecoveryPlan
 err:=transaction(ctx,pool,func(tx pgx.Tx)error{state,err:=(&data.MaintenanceModel{DB:pool}).ReadTx(ctx,tx,true);if err!=nil{return err};plan,err=maintenanceRecoveryPlan(r,state);return err})
 return plan,err
}
func MaintenanceApprovalMessage(r MaintenanceRecoveryRequest,digest string,id uuid.UUID)([]byte,error){
 decoded,err:=hex.DecodeString(digest)
 if !validMaintenanceRecovery(r) || err!=nil || len(decoded)!=32 || digest!=strings.ToLower(digest) || id==uuid.Nil{return nil,errors.New("valid request, digest and ceremony ID required")}
 return json.Marshal(struct{Domain string `json:"domain"`;Request MaintenanceRecoveryRequest `json:"request"`;Digest string `json:"plan_digest"`;ID uuid.UUID `json:"ceremony_id"`}{"platform-maintenance-recovery-v1",r,digest,id})
}
func VerifyMaintenanceApproval(r MaintenanceRecoveryRequest,digest string,id uuid.UUID,board,cio ed25519.PublicKey,boardSignature,cioSignature []byte)(*MaintenanceApproval,error){
 message,err:=MaintenanceApprovalMessage(r,digest,id);if err!=nil{return nil,err}
 if len(board)!=ed25519.PublicKeySize || len(cio)!=ed25519.PublicKeySize || string(board)==string(cio) || !ed25519.Verify(board,message,boardSignature) || !ed25519.Verify(cio,message,cioSignature){return nil,errors.New("distinct trusted Board and CIO signatures are required")}
 h:=fingerprint(message)
 return &MaintenanceApproval{messageHash:h,evidence:ApprovalEvidence{MessageSHA256:h,BoardKeyFingerprint:fingerprint(board),CIOKeyFingerprint:fingerprint(cio),BoardSignature:hex.EncodeToString(boardSignature),CIOSignature:hex.EncodeToString(cioSignature)}},nil
}
func ExecuteMaintenance(ctx context.Context,pool *pgxpool.Pool,logger *logging.Logger,r MaintenanceRecoveryRequest,digest string,id uuid.UUID,approval *MaintenanceApproval)(bool,error){
 if ctx==nil || pool==nil || logger==nil{return false,errors.New("recovery dependencies required")}
 message,err:=MaintenanceApprovalMessage(r,digest,id)
 if err!=nil || approval==nil || fingerprint(message)!=approval.messageHash{return false,errors.New("verified approval for the exact ceremony is required")}
 repeated:=false
 err=transaction(ctx,pool,func(tx pgx.Tx)error{
  var prior []byte;var op string
  err:=tx.QueryRow(ctx,`SELECT operation,evidence FROM public.site_maintenance_transitions WHERE id=$1`,id).Scan(&op,&prior)
  if err==nil{
   var e struct{Request MaintenanceRecoveryRequest `json:"request"`;Digest string `json:"plan_digest"`;Approval ApprovalEvidence `json:"approval"`}
   if json.Unmarshal(prior,&e)!=nil || op!="engineering_restore" || e.Request!=r || e.Digest!=digest || e.Approval.MessageSHA256!=approval.messageHash{return data.ErrMaintenanceConflict}
   repeated=true;return nil
  };if !errors.Is(err,pgx.ErrNoRows){return err}
  model:=&data.MaintenanceModel{DB:pool};state,err:=model.ReadTx(ctx,tx,true);if err!=nil{return err}
  plan,err:=maintenanceRecoveryPlan(r,state);if err!=nil{return err};if plan.Digest!=digest{return data.ErrMaintenanceConflict}
  next:=state.MaintenanceDocument;next.Active=false;next.ActiveSince=nil;next.Message=nil;next.ExpectedEndAt=nil
  proof:=struct{Request MaintenanceRecoveryRequest `json:"request"`;Digest string `json:"plan_digest"`;Approval ApprovalEvidence `json:"approval"`}{r,digest,approval.evidence}
  // Use the ceremony UUID as the evidence identity, so an uncertain COMMIT can
  // be reconciled without executing a second restoration.
  oldRaw,err:=json.Marshal(state.MaintenanceDocument);if err!=nil{return err};newRaw,err:=json.Marshal(next);if err!=nil{return err};raw,err:=json.Marshal(proof);if err!=nil{return err}
  tag,err:=tx.Exec(ctx,`UPDATE public.site_maintenance_state SET version=version+1,document=$2::jsonb,updated_at=clock_timestamp() WHERE singleton AND version=$1`,state.Version,string(newRaw));if err!=nil{return err};if tag.RowsAffected()!=1{return data.ErrMaintenanceConflict}
  if _,err:=tx.Exec(ctx,`INSERT INTO public.site_maintenance_transitions(id,operation,mechanism,actor_id,version,before_state,after_state,reason,evidence) VALUES($1,'engineering_restore','engineering_recovery',NULL,$2,$3::jsonb,$4::jsonb,$5,$6::jsonb)`,id,state.Version+1,string(oldRaw),string(newRaw),r.Reason,string(raw));err!=nil{return err}
  return data.NewAdministrativeGovernanceModel(pool,logger).InsertGovernanceAuditTx(ctx,tx,data.GovernanceAuditEntry{Action:data.MaintenanceActionPrefix+"engineering_restore",EntityType:data.MaintenanceEntity,EntityID:id.String(),Outcome:data.GovernanceOutcomeSucceeded,Context:data.GovernanceAuditContext{Operation:"engineering_restore",Reason:r.Reason,PreviousStatus:"active",NewStatus:"normal",Mechanism:"engineering_recovery"}})
 })
 if err!=nil{return false,fmt.Errorf("maintenance recovery outcome unconfirmed: %w",err)};return repeated,nil
}
