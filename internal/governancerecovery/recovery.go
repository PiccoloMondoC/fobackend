// focodebase/fobackend/internal/governancerecovery/recovery.go
// Infrastructure operator boundary. Never imported by the HTTP API. The Board
// decides; CIO coordinates; Engineering reviews a bound plan then executes it.
package governancerecovery

import (
 "context"
 "crypto/sha256"
 "encoding/hex"
 "encoding/json"
 "errors"
 "fmt"
 "strings"
 "time"
 "github.com/PiccoloMondoC/focodebase/fobackend/internal/data"
 "github.com/PiccoloMondoC/focodebase/fobackend/internal/logging"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "github.com/jackc/pgx/v5/pgxpool"
)
type Request struct {
 Operation string `json:"operation"`
 TargetID uuid.UUID `json:"target_id"`
 ConfirmEmail string `json:"confirm_email"`
 BoardReference string `json:"board_reference"`
 CIOReference string `json:"cio_reference"`
 Executor string `json:"executor"`
 Reason string `json:"reason"`
 PredecessorDisposition string `json:"predecessor_disposition"`
 DepartureWithoutConflict bool `json:"departure_without_conflict"`
 IssueReference string `json:"issue_reference"`
 Capabilities []string `json:"capabilities"`
 DurationHours int `json:"duration_hours"`
 GrantID uuid.UUID `json:"grant_id"`
}
type Plan struct {
 Request Request `json:"request"`
 State json.RawMessage `json:"state"`
 IncumbentID uuid.UUID `json:"incumbent_id"`
 IncumbentEmail string `json:"incumbent_email"`
 Generation int `json:"generation"`
 TargetEmail string `json:"target_email"`
 Effects []string `json:"effects"`
 Digest string `json:"digest"`
}
func bounded(s string) bool {return strings.TrimSpace(s)!="" && len([]rune(s))<=200}
func ValidateRequest(r Request) error {
 if r.TargetID==uuid.Nil || !bounded(r.CIOReference) || !bounded(r.Executor) || strings.TrimSpace(r.ConfirmEmail)=="" || len(r.ConfirmEmail)>320{return errors.New("target, confirmed email, CIO authorization and executor are required")}
 switch r.Operation {
 case "recovery":
  if !bounded(r.BoardReference){return errors.New("Board authorization is required")}
  if r.Reason!="incumbent_incapacity" && r.Reason!="incumbent_unreachable" && r.Reason!="board_ordered_removal"{return errors.New("invalid recovery reason")}
  if r.PredecessorDisposition!="consumer" && r.PredecessorDisposition!="admin" && r.PredecessorDisposition!="super_admin"{return errors.New("Board must specify predecessor disposition")}
  if r.DepartureWithoutConflict && r.PredecessorDisposition!="consumer"{return errors.New("non-conflict departure confers honorary standing without administrative appointment")}
 case "consultant_grant":
  if !bounded(r.IssueReference) || r.DurationHours<1 || r.DurationHours>168 || len(r.Capabilities)==0 || len(r.Capabilities)>3{return errors.New("consulting issue, duration (1..168 hours), and capabilities required")}
  seen:=map[string]bool{};for _,c:=range r.Capabilities{if seen[c] || (c!="read_continuity" && c!="read_administrators" && c!="read_governance_history"){return errors.New("invalid or duplicate consulting capability")};seen[c]=true}
 case "consultant_revoke":if r.GrantID==uuid.Nil{return errors.New("grant ID required")}
 default:return errors.New("unknown operator ceremony")
 };return nil
}
func transaction(ctx context.Context,pool *pgxpool.Pool,fn func(pgx.Tx)error) error {
 tx,err:=pool.BeginTx(ctx,pgx.TxOptions{IsoLevel:pgx.ReadCommitted});if err!=nil{return err}
 defer func(){cleanup,cancel:=context.WithTimeout(context.WithoutCancel(ctx),5*time.Second);defer cancel();_ = tx.Rollback(cleanup)}()
 if _,err:=tx.Exec(ctx,`SELECT pg_advisory_xact_lock($1)`,data.GovernanceAdvisoryLockKey);err!=nil{return err}
 if err:=fn(tx);err!=nil{return err};return tx.Commit(ctx)
}
func makePlan(ctx context.Context,tx pgx.Tx,gov *data.AdministrativeGovernanceModel,r Request) (*Plan,error) {
 ids,err:=data.ContinuityParticipantIDsTx(ctx,tx,r.TargetID);if err!=nil{return nil,err}
 subjects,err:=gov.LockGovernanceSubjectsTx(ctx,tx,ids...);if err!=nil{return nil,err}
 incumbent,generation,err:=data.ReadRootTx(ctx,tx);if err!=nil{return nil,err}
 target:=subjects[r.TargetID];prior:=subjects[incumbent]
 if target==nil || prior==nil || target.IsDeleted || !target.IsActive || target.PasswordHash=="" || !strings.EqualFold(strings.TrimSpace(r.ConfirmEmail),target.Email){return nil,data.ErrContinuityConflict}
 if r.Operation=="recovery" && r.TargetID==incumbent{return nil,data.ErrContinuityConflict}
 if r.Operation!="recovery" {
  var honorary bool;if err:=tx.QueryRow(ctx,`SELECT EXISTS(SELECT 1 FROM root_honorary_standing WHERE user_id=$1)`,r.TargetID).Scan(&honorary);err!=nil{return nil,err};if !honorary{return nil,data.ErrContinuityConflict}
  if r.Operation=="consultant_revoke" {var live bool;if err:=tx.QueryRow(ctx,`SELECT EXISTS(SELECT 1 FROM root_consultant_grants WHERE id=$1 AND user_id=$2 AND revoked_at IS NULL)`,r.GrantID,r.TargetID).Scan(&live);err!=nil{return nil,err};if !live{return nil,data.ErrContinuityConflict}}
 }
 state,err:=data.ReadContinuityStateTx(ctx,tx);if err!=nil{return nil,err}
 p:=&Plan{Request:r,State:state,IncumbentID:incumbent,IncumbentEmail:prior.Email,Generation:generation,TargetEmail:target.Email}
 if r.Operation=="recovery" {p.Effects=[]string{"transfer incumbency to verified target","end appointed co-Root standing; retain its existing Super Admin appointment unless it is the successor","remove successor designation and preserve the remaining order","supersede pending voluntary proposal","apply Board-authorized predecessor disposition","revoke predecessor refresh sessions and existing consulting grants; live authorization applies to existing access sessions","record honorary standing only for a departure without conflict","append transfer evidence, continuity event and audit atomically"}} else if r.Operation=="consultant_grant" {p.Effects=[]string{"grant only the listed read capabilities until expiry","preserve the existing account and password","record consulting authorization and audit"}} else {p.Effects=[]string{"revoke the named consulting grant immediately","preserve honorary standing and ordinary account","record authorization and audit"}}
 // Bind account credentials and appointments to the digest, but do not expose
 // password hashes or their versions in the printed plan or audit evidence.
 versions:=map[string]any{}
 for id,s:=range subjects{hash:=sha256.Sum256([]byte(s.PasswordHash));versions[id.String()]=map[string]any{"credential_version":hex.EncodeToString(hash[:]),"appointment":s.Appointment,"active":s.IsActive,"deleted":s.IsDeleted,"email":s.Email}}
 raw,err:=json.Marshal(struct{Plan *Plan;Versions map[string]any}{p,versions});if err!=nil{return nil,err};sum:=sha256.Sum256(raw);p.Digest=hex.EncodeToString(sum[:]);return p,nil
}
func Prepare(ctx context.Context,pool *pgxpool.Pool,logger *logging.Logger,r Request) (*Plan,error) {
 if ctx==nil || pool==nil || logger==nil{return nil,errors.New("operator dependencies are required")}
 if err:=ValidateRequest(r);err!=nil{return nil,err};var p *Plan
 err:=transaction(ctx,pool,func(tx pgx.Tx)error{var err error;p,err=makePlan(ctx,tx,data.NewAdministrativeGovernanceModel(pool,logger),r);return err});return p,err
}
func Execute(ctx context.Context,pool *pgxpool.Pool,logger *logging.Logger,r Request,digest string,ceremonyID uuid.UUID,approval *VerifiedApproval) (bool,error) {
 if ctx==nil || pool==nil || logger==nil{return false,errors.New("operator dependencies are required")}
 if err:=ValidateRequest(r);err!=nil{return false,err};if len(digest)!=64 || ceremonyID==uuid.Nil{return false,errors.New("reviewed digest and ceremony UUID required")}
 if !approval.matches(r,digest,ceremonyID){return false,errors.New("verified authorization for this exact ceremony is required")}
 repeated:=false
 err:=transaction(ctx,pool,func(tx pgx.Tx)error {
  var existing json.RawMessage;var operation string
  err:=tx.QueryRow(ctx,`SELECT operation,evidence FROM root_continuity_events WHERE id=$1`,ceremonyID).Scan(&operation,&existing)
  if err==nil {
   var prior struct{Request Request `json:"request"`;Digest string `json:"digest"`};if json.Unmarshal(existing,&prior)!=nil{return data.ErrContinuityConflict}
   a,_:=json.Marshal(prior.Request);b,_:=json.Marshal(r)
   if operation!=r.Operation || string(a)!=string(b) || prior.Digest!=digest{return data.ErrContinuityConflict};repeated=true;return nil
  };if !errors.Is(err,pgx.ErrNoRows){return err}
  gov:=data.NewAdministrativeGovernanceModel(pool,logger)
  p,err:=makePlan(ctx,tx,gov,r);if err!=nil{return err};if p.Digest!=digest{return data.ErrContinuityConflict}
  if err:=data.SetContinuityOperationTx(ctx,tx,r.Operation);err!=nil{return err}
  if r.Operation=="recovery" {
   if _,err:=tx.Exec(ctx,`DELETE FROM root_co_holder WHERE singleton`);err!=nil{return err}
   if _,err:=tx.Exec(ctx,`DELETE FROM root_successor_designations WHERE user_id=$1`,r.TargetID);err!=nil{return err};if err:=data.RepackSuccessorsTx(ctx,tx);err!=nil{return err}
   roles:=&data.RoleModel{DB:pool,Logger:logger}
   // The Board may select an ordinary verified account from outside the team.
   // Establish its Super Admin appointment before advancing the Root pointer.
   reason:="Board-authorized Root recovery"
   if _,err:=gov.PutAppointmentTx(ctx,tx,data.AppointmentChange{UserID:r.TargetID,Level:data.AdministrativeLevelSuperAdmin,Status:data.AppointmentStatusActive,Reason:&reason,NewTerm:true});err!=nil{return err}
   roleID,err:=roles.ResolveRoleIDByNameTx(ctx,tx,data.RoleNameSuperAdmin);if err!=nil{return err}
   if err:=roles.AssignPrimaryRoleTx(ctx,tx,r.TargetID,roleID,nil);err!=nil{return err}
   if _,err:=tx.Exec(ctx,`UPDATE root_succession_proposals SET status='superseded',resolved_at=clock_timestamp() WHERE status='pending'`);err!=nil{return err}
   ledger,err:=data.AdvanceRootTx(ctx,tx,p.IncumbentID,r.TargetID,p.Generation,"emergency_succession",&r.BoardReference);if err!=nil{return err}
   if err:=data.RevokeAdministrativeAccessTx(ctx,tx,gov,roles,p.IncumbentID,nil,r.PredecessorDisposition);err!=nil{return err}
   if _,err:=tx.Exec(ctx,`UPDATE root_consultant_grants SET revoked_at=clock_timestamp() WHERE user_id=$1 AND revoked_at IS NULL`,p.IncumbentID);err!=nil{return err}
   if r.DepartureWithoutConflict {if err:=data.InsertHonoraryStandingTx(ctx,tx,p.IncumbentID,p.Generation+1);err!=nil{return err}}
   if _,err:=tx.Exec(ctx,`INSERT INTO root_transfer_evidence(ledger_id,ceremony_id,board_reference,cio_reference,executor,reason,predecessor_disposition,departure_without_conflict) VALUES($1,$2,$3,$4,$5,$6,$7,$8)`,ledger,ceremonyID,r.BoardReference,r.CIOReference,r.Executor,r.Reason,r.PredecessorDisposition,r.DepartureWithoutConflict);err!=nil{return err}
  } else if r.Operation=="consultant_grant" {
   if _,err:=tx.Exec(ctx,`INSERT INTO root_consultant_grants(id,user_id,issue_reference,cio_reference,executor,capabilities,granted_at,expires_at) VALUES($1,$2,$3,$4,$5,$6,statement_timestamp(),statement_timestamp()+make_interval(hours=>$7))`,ceremonyID,r.TargetID,r.IssueReference,r.CIOReference,r.Executor,r.Capabilities,r.DurationHours);err!=nil{return err}
  } else {
   tag,err:=tx.Exec(ctx,`UPDATE root_consultant_grants SET revoked_at=clock_timestamp() WHERE id=$1 AND user_id=$2 AND revoked_at IS NULL`,r.GrantID,r.TargetID);if err!=nil{return err};if tag.RowsAffected()!=1{return data.ErrContinuityConflict}
  }
  evidence:=struct{Request Request `json:"request"`;Digest string `json:"digest"`;Approval ApprovalEvidence `json:"approval"`}{r,digest,approval.evidence}
  return data.RecordContinuityEventTx(ctx,tx,gov,ceremonyID,r.Operation,nil,&r.TargetID,p.State,evidence)
 });if err!=nil{return false,fmt.Errorf("operator ceremony not confirmed: %w",err)};return repeated,nil
}
