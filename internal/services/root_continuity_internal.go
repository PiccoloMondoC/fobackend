// Package services provides account identity completion and administrative identity safeguards.
// focodebase/fobackend/internal/services/root_continuity_internal.go

// Root continuity policy: incumbent-only appointments and proposals; independent
// co-Root acceptance. This package cannot execute Board recovery.
package services

import (
 "context"
 "crypto/sha256"
 "encoding/hex"
 "encoding/json"
 "errors"
 "strings"
 "time"
 "github.com/PiccoloMondoC/focodebase/fobackend/internal/data"
 "github.com/PiccoloMondoC/focodebase/fobackend/internal/security"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
)

type ContinuityRequest struct {
 Operation string `json:"operation"`
 TargetID uuid.UUID `json:"target_id"`
 ProposalID uuid.UUID `json:"proposal_id"`
 Order []uuid.UUID `json:"order"`
 Disposition string `json:"disposition"`
 CurrentPassword string `json:"current_password"`
 ConfirmEmail string `json:"confirm_email"`
 ExpectedDigest string `json:"expected_digest"`
}
type ContinuityView struct {
 Identities map[uuid.UUID]*data.AdministrativeIdentity `json:"identities"`
 data.ContinuitySnapshot
 Digest string `json:"digest"`
}
func ContinuityDigest(state json.RawMessage) string { sum:=sha256.Sum256(state);return hex.EncodeToString(sum[:]) }
func ValidContinuityOperation(op string) bool {
 switch op {case "designate","reorder","undesignate","appoint_co_root","remove_co_root","replace_co_root","propose","cancel","accept":return true};return false
}
func (s *Service) ReadRootContinuityInternal(ctx context.Context,actorID uuid.UUID) (*ContinuityView,error) {
 if err:=s.validate();err!=nil{return nil,err};if ctx==nil{return nil,ErrNilContext}
 ctx,cancel:=context.WithTimeout(ctx,s.Cfg.DBTimeout);defer cancel()
 var view ContinuityView;view.History=[]data.ContinuityEvent{}
 err:=s.withGovernanceTx(ctx,func(tx pgx.Tx) error {
  subjects,err:=s.governance().LockGovernanceSubjectsTx(ctx,tx,actorID);if err!=nil{return err}
  if err:=s.governance().RequirePermissionTx(ctx,tx,actorID,"read_administrators");err!=nil{return data.ErrContinuityForbidden}
  actor:=subjects[actorID];if actor==nil || actor.ActiveLevel()!=data.AdministrativeLevelSuperAdmin{return data.ErrContinuityForbidden}
  view.ActorRole="super_admin";if actor.IsRoot {view.ActorRole="incumbent"} else if actor.IsCoRoot {view.ActorRole="co_root"}
  view.State,err=data.ReadContinuityStateTx(ctx,tx);if err!=nil{return err};view.Digest=ContinuityDigest(view.State)
  rows,err:=tx.Query(ctx,`SELECT id,operation,actor_id,target_id,evidence,occurred_at FROM root_continuity_events ORDER BY occurred_at DESC,id DESC LIMIT 100`)
  if err!=nil{return err};defer rows.Close()
  for rows.Next(){var e data.ContinuityEvent;if err:=rows.Scan(&e.ID,&e.Operation,&e.ActorID,&e.TargetID,&e.Evidence,&e.OccurredAt);err!=nil{return err};view.History=append(view.History,e)}
  if err=rows.Err();err!=nil{return err};rows.Close()
  ids,err:=continuityIdentityIDs(view.State,view.History);if err!=nil{return err}
  view.Identities,err=data.ReadAdministrativeIdentities(ctx,tx,ids...);return err
 });if err!=nil{return nil,err};return &view,nil
}
func (s *Service) ExecuteRootContinuityInternal(ctx context.Context,actorID uuid.UUID,req ContinuityRequest) (result uuid.UUID,resultErr error) {
 if err:=s.validate();err!=nil{return uuid.Nil,err};if ctx==nil{return uuid.Nil,ErrNilContext}
 ctx,cancel:=context.WithTimeout(ctx,s.Cfg.DBTimeout);defer cancel()
 defer func(){
  if resultErr!=nil && ValidContinuityOperation(req.Operation) && actorID!=uuid.Nil {
   target:=req.TargetID;if target==uuid.Nil{target=actorID}
   s.recordGovernanceDenial(ctx,actorID,target,"root_continuity",req.Operation,resultErr)
  }
 }()
 if !ValidContinuityOperation(req.Operation) || len(req.CurrentPassword)==0 || len(req.CurrentPassword)>72 || len(req.ExpectedDigest)!=64 || len(req.Order)>3{return uuid.Nil,ErrGovernanceInputInvalid}
 // Expensive proof outside every database lock. Current live authority and
 // exact credential version are checked again after acquiring participant locks.
 subject,err:=s.governance().GetGovernanceSubject(ctx,actorID);if err!=nil{return uuid.Nil,data.ErrContinuityForbidden}
 if subject.ActiveLevel()!=data.AdministrativeLevelSuperAdmin || (!subject.IsRoot && !(req.Operation=="accept" && subject.IsCoRoot)){return uuid.Nil,data.ErrContinuityForbidden}
 version:=subject.PasswordHash
 if security.VerifyPassword(req.CurrentPassword,version)!=nil{return uuid.Nil,ErrGovernanceReauthenticationFailed};req.CurrentPassword=""
 ceremonyID:=uuid.New()
 err=s.withGovernanceTx(ctx,func(tx pgx.Tx) error {
  gov:=s.governance();ids,err:=data.ContinuityParticipantIDsTx(ctx,tx,actorID,req.TargetID);if err!=nil{return err}
  subjects,err:=gov.LockGovernanceSubjectsTx(ctx,tx,ids...);if err!=nil{return err}
  actor:=subjects[actorID]
  if actor==nil || actor.PasswordHash!=version || actor.ActiveLevel()!=data.AdministrativeLevelSuperAdmin {return ErrGovernanceReauthenticationFailed}
  incumbent,generation,err:=data.ReadRootTx(ctx,tx);if err!=nil{return err}
  co,err:=data.ReadCoRootTx(ctx,tx);if err!=nil{return err}
  if req.Operation=="accept" {if co==nil || *co!=actorID{return data.ErrContinuityForbidden}} else if incumbent!=actorID{return data.ErrContinuityForbidden}
  before,err:=data.ReadContinuityStateTx(ctx,tx);if err!=nil{return err}
  if ContinuityDigest(before)!=req.ExpectedDigest{return data.ErrContinuityConflict}
  if err:=data.SetContinuityOperationTx(ctx,tx,req.Operation);err!=nil{return err}
  target:=subjects[req.TargetID]
  requireTarget:=func()error{if req.TargetID==incumbent || req.TargetID==actorID{return ErrGovernanceSelfTarget};if err:=data.EnsureContinuitySubject(target);err!=nil{return err};if !strings.EqualFold(strings.TrimSpace(req.ConfirmEmail),target.Email){return ErrGovernanceInputInvalid};return nil}
  switch req.Operation {
  case "designate","appoint_co_root","replace_co_root","propose":
   if err:=requireCompletedIdentityTx(ctx,tx,req.TargetID);err!=nil{return err}
  case "accept":
   if err:=requireCompletedIdentityTx(ctx,tx,actorID);err!=nil{return err}
  }
  evidence:=map[string]any{}
  switch req.Operation {
  case "designate":
   if err:=requireTarget();err!=nil{return err};if target.IsCoRoot || target.SuccessionRank>0{return data.ErrContinuityConflict}
   var count int;if err:=tx.QueryRow(ctx,`SELECT count(*) FROM root_successor_designations`).Scan(&count);err!=nil{return err};if count>=3{return data.ErrContinuityConflict}
   if _,err:=tx.Exec(ctx,`INSERT INTO root_successor_designations(user_id,rank,designated_by) VALUES($1,$2,$3)`,req.TargetID,count+1,actorID);err!=nil{return err}
  case "undesignate":
   if target==nil || target.SuccessionRank==0{return data.ErrContinuityConflict}
   if _,err:=tx.Exec(ctx,`DELETE FROM root_successor_designations WHERE user_id=$1`,req.TargetID);err!=nil{return err};if err:=data.RepackSuccessorsTx(ctx,tx);err!=nil{return err}
  case "reorder":
   existing:=map[uuid.UUID]bool{};for id,t:=range subjects{if t.SuccessionRank>0{existing[id]=true}}
   if len(req.Order)!=len(existing){return ErrGovernanceInputInvalid}
   for i,id:=range req.Order{if !existing[id]{return ErrGovernanceInputInvalid};delete(existing,id);if _,err:=tx.Exec(ctx,`UPDATE root_successor_designations SET rank=$2 WHERE user_id=$1`,id,i+1);err!=nil{return err}}
   evidence["order"]=req.Order
  case "appoint_co_root","replace_co_root":
   if err:=requireTarget();err!=nil{return err};if target.SuccessionRank==0{return data.ErrContinuityConflict}
   if req.Operation=="appoint_co_root" && co!=nil{return data.ErrContinuityConflict}
   if req.Operation=="replace_co_root" {if co==nil{return data.ErrContinuityConflict};if err:=s.removeServingCoRootTx(ctx,tx,*co,actorID,req.Disposition);err!=nil{return err};evidence["outgoing_co_root"]=*co;evidence["disposition"]=req.Disposition}
   if _,err:=tx.Exec(ctx,`DELETE FROM root_successor_designations WHERE user_id=$1`,req.TargetID);err!=nil{return err};if err:=data.RepackSuccessorsTx(ctx,tx);err!=nil{return err}
   if _,err:=tx.Exec(ctx,`INSERT INTO root_co_holder(user_id,appointed_by) VALUES($1,$2)`,req.TargetID,actorID);err!=nil{return err}
  case "remove_co_root":
   if co==nil || req.TargetID!=*co{return data.ErrContinuityConflict}
   if err:=s.removeServingCoRootTx(ctx,tx,*co,actorID,req.Disposition);err!=nil{return err};evidence["disposition"]=req.Disposition
  case "propose":
   if co==nil || req.TargetID!=*co{return data.ErrContinuityConflict};if err:=requireTarget();err!=nil{return err}
   if _,err:=tx.Exec(ctx,`UPDATE root_succession_proposals SET status='expired',resolved_at=clock_timestamp() WHERE status='pending' AND expires_at<=clock_timestamp()`);err!=nil{return err}
   var pending bool;if err:=tx.QueryRow(ctx,`SELECT EXISTS(SELECT 1 FROM root_succession_proposals WHERE status='pending')`).Scan(&pending);err!=nil{return err};if pending{return data.ErrContinuityConflict}
   proposalID:=uuid.New()
   if _,err:=tx.Exec(ctx,`INSERT INTO root_succession_proposals(id,incumbent_id,successor_id,generation,status,proposed_at,expires_at) VALUES($1,$2,$3,$4,'pending',statement_timestamp(),statement_timestamp()+INTERVAL '72 hours')`,proposalID,actorID,*co,generation);err!=nil{return err};evidence["proposal_id"]=proposalID
  case "cancel":
   tag,err:=tx.Exec(ctx,`UPDATE root_succession_proposals SET status='cancelled',resolved_at=clock_timestamp() WHERE id=$1 AND incumbent_id=$2 AND status='pending'`,req.ProposalID,actorID);if err!=nil{return err};if tag.RowsAffected()!=1{return data.ErrContinuityConflict};evidence["proposal_id"]=req.ProposalID
  case "accept":
   var predecessor,successor uuid.UUID;var proposalGeneration int;var expires time.Time;var status string
   err:=tx.QueryRow(ctx,`SELECT incumbent_id,successor_id,generation,expires_at,status FROM root_succession_proposals WHERE id=$1 FOR UPDATE`,req.ProposalID).Scan(&predecessor,&successor,&proposalGeneration,&expires,&status)
   if errors.Is(err,pgx.ErrNoRows){return data.ErrContinuityConflict};if err!=nil{return err}
   if status!="pending" || predecessor!=incumbent || successor!=actorID || proposalGeneration!=generation{return data.ErrContinuityConflict}
   var fresh bool;if err:=tx.QueryRow(ctx,`SELECT $1::timestamptz>clock_timestamp()`,expires).Scan(&fresh);err!=nil{return err};if !fresh{return data.ErrContinuityConflict}
   if !strings.EqualFold(strings.TrimSpace(req.ConfirmEmail),subjects[incumbent].Email){return ErrGovernanceInputInvalid}
   if _,err:=tx.Exec(ctx,`DELETE FROM root_co_holder WHERE singleton`);err!=nil{return err}
   ledger,err:=data.AdvanceRootTx(ctx,tx,incumbent,actorID,generation,"voluntary_transfer",nil);if err!=nil{return err}
   if err:=data.RevokeAdministrativeAccessTx(ctx,tx,gov,&s.Models.Role,incumbent,&actorID,"consumer");err!=nil{return err}
   if err:=data.InsertHonoraryStandingTx(ctx,tx,incumbent,generation+1);err!=nil{return err}
   tag,err:=tx.Exec(ctx,`UPDATE root_succession_proposals SET status='accepted',resolved_at=clock_timestamp() WHERE id=$1 AND status='pending' AND expires_at>clock_timestamp()`,req.ProposalID);if err!=nil{return err};if tag.RowsAffected()!=1{return data.ErrContinuityConflict}
   if _,err:=tx.Exec(ctx,`INSERT INTO root_transfer_evidence(ledger_id,ceremony_id,proposal_id,predecessor_disposition,departure_without_conflict) VALUES($1,$2,$3,'consumer',TRUE)`,ledger,ceremonyID,req.ProposalID);err!=nil{return err}
   evidence["proposal_id"]=req.ProposalID;evidence["predecessor"]=incumbent;evidence["generation"]=generation+1
  }
  var targetID *uuid.UUID;if req.TargetID!=uuid.Nil{targetID=&req.TargetID};if req.Operation=="accept"{targetID=&actorID}
  return data.RecordContinuityEventTx(ctx,tx,gov,ceremonyID,req.Operation,&actorID,targetID,before,evidence)
 })
 if err!=nil{return uuid.Nil,err};return ceremonyID,nil
}
func (s *Service) removeServingCoRootTx(ctx context.Context,tx pgx.Tx,id,actor uuid.UUID,disposition string) error {
 if disposition!="consumer" && disposition!="admin" && disposition!="super_admin" {return ErrGovernanceInputInvalid}
 if _,err:=tx.Exec(ctx,`DELETE FROM root_co_holder WHERE user_id=$1`,id);err!=nil{return err}
 if _,err:=tx.Exec(ctx,`UPDATE root_succession_proposals SET status='superseded',resolved_at=clock_timestamp() WHERE status='pending' AND successor_id=$1`,id);err!=nil{return err}
 return data.RevokeAdministrativeAccessTx(ctx,tx,s.governance(),&s.Models.Role,id,&actor,disposition)
}
