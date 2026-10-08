// focodebase/fobackend/internal/services/role_administration_internal.go
// CE authoritative role administration. No HTTP-context authority, nested
// model transactions or unaudited successful catalog/assignment changes.
package services

import (
 "context"
 "github.com/PiccoloMondoC/focodebase/fobackend/internal/data"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
)

// WithRoleAdministrationInternal is the transaction owner for catalog edits.
// The callback may use only Tx primitives. It is private service composition
// exposed for the existing API boundary, not a general database transaction API.
func (s *Service) WithRoleAdministrationInternal(ctx context.Context,actorID uuid.UUID,permission,action,entityID string,change func(context.Context,pgx.Tx) error) error {
 if err:=s.validate();err!=nil{return err}
 if ctx==nil{return ErrNilContext};if actorID==uuid.Nil || change==nil{return ErrGovernanceInputInvalid}
 // Limit the capability: only the explicit catalog operations may use it.
 if !((permission=="create_role" && action=="create_role") || (permission=="update_role" && action=="update_role") ||
 (permission=="delete_role" && action=="delete_role") || (permission=="update_role_permissions" && action=="update_role_permissions")){return ErrGovernanceInputInvalid}
 ctx,cancel:=context.WithTimeout(ctx,s.Cfg.DBTimeout);defer cancel()
 return s.withGovernanceTx(ctx,func(tx pgx.Tx) error {
  subjects,err:=s.governance().LockGovernanceSubjectsTx(ctx,tx,actorID);if err!=nil{return err}
  actor:=actorFromSubject(subjects[actorID]);if actor.Level!=data.AdministrativeLevelSuperAdmin{return ErrGovernanceInsufficientAuthority}
  if err:=s.governance().RequirePermissionTx(ctx,tx,actorID,permission);err!=nil{return err}
  if err:=change(ctx,tx);err!=nil{return err}
  entityType:="roles";if permission=="update_role_permissions"{entityType="permissions"}
  return s.governance().InsertGovernanceAuditTx(ctx,tx,data.GovernanceAuditEntry{ActorID:&actorID,Action:action,EntityType:entityType,
  EntityID:entityID,Outcome:data.GovernanceOutcomeSucceeded,Context:data.GovernanceAuditContext{Operation:action}})
 })
}

func (s *Service) ChangeOrdinaryRoleInternal(ctx context.Context,actorID,targetID,roleID uuid.UUID,primary,revoke bool) error {
 if err:=s.validate();err!=nil{return err};if ctx==nil{return ErrNilContext}
 if actorID==uuid.Nil || targetID==uuid.Nil || roleID==uuid.Nil{return ErrGovernanceInputInvalid}
 if actorID==targetID{return ErrGovernanceSelfTarget}
 ctx,cancel:=context.WithTimeout(ctx,s.Cfg.DBTimeout);defer cancel()
 action:="assign_role";if revoke{action="revoke_role"}
 err:=s.withGovernanceTx(ctx,func(tx pgx.Tx) error {
  subjects,err:=s.governance().LockGovernanceSubjectsTx(ctx,tx,actorID,targetID);if err!=nil{return err}
  actor:=actorFromSubject(subjects[actorID])
  target:=targetFromSubject(targetID,subjects[targetID])
  if err:=AuthorizeAccountStewardship(actor,target);err!=nil{return err}
  if target.IsRoot{return ErrGovernanceRootProtected}
  if primary && (target.Status==data.AppointmentStatusActive || target.Status==data.AppointmentStatusSuspended){return data.ErrAdministrativeInconsistency}
  if err:=s.governance().RequirePermissionTx(ctx,tx,actorID,action);err!=nil{return err}
  if revoke{err=s.Models.Role.RevokeNonAdministrativeRoleTx(ctx,tx,targetID,roleID)
  }else{err=s.Models.Role.AssignNonAdministrativeRoleTx(ctx,tx,targetID,roleID,actorID,primary)}
  if err!=nil{return err}
  return s.governance().InsertGovernanceAuditTx(ctx,tx,data.GovernanceAuditEntry{ActorID:&actorID,Action:action,EntityType:"roles",
   EntityID:targetID.String(),Outcome:data.GovernanceOutcomeSucceeded,Context:data.GovernanceAuditContext{Operation:action}})
 })
 if err!=nil{s.recordGovernanceDenial(ctx,actorID,targetID,action,action,err)};return err
}

func (s *Service) CreateOrdinaryRoleInternal(ctx context.Context,actorID uuid.UUID,role *data.Role) error {
 if err:=s.validate();err!=nil{return err};if ctx==nil{return ErrNilContext}
 if actorID==uuid.Nil || role==nil{return ErrGovernanceInputInvalid}
 ctx,cancel:=context.WithTimeout(ctx,s.Cfg.DBTimeout);defer cancel()
 return s.withGovernanceTx(ctx,func(tx pgx.Tx) error {
  subjects,err:=s.governance().LockGovernanceSubjectsTx(ctx,tx,actorID);if err!=nil{return err}
  if actorFromSubject(subjects[actorID]).Level!=data.AdministrativeLevelSuperAdmin{return ErrGovernanceInsufficientAuthority}
  if err:=s.governance().RequirePermissionTx(ctx,tx,actorID,"create_role");err!=nil{return err}
  if err:=s.Models.Role.CreateRoleTx(ctx,tx,role);err!=nil{return err}
  return s.governance().InsertGovernanceAuditTx(ctx,tx,data.GovernanceAuditEntry{ActorID:&actorID,Action:"create_role",EntityType:"roles",
   EntityID:role.ID.String(),Outcome:data.GovernanceOutcomeSucceeded,Context:data.GovernanceAuditContext{Operation:"create_role"}})
 })
}
