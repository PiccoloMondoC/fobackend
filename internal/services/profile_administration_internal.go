// focodebase/fobackend/internal/services/profile_administration_internal.go
package services
import (
 "context"
 "github.com/PiccoloMondoC/focodebase/fobackend/internal/data"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
)
func (s *Service) ChangeProfileAdministrativeInternal(ctx context.Context,actorID,targetID uuid.UUID,profile *data.UserProfile,flagged bool,notes string) error {
 if err:=s.validate();err!=nil{return err};if ctx==nil{return ErrNilContext}
 if actorID==uuid.Nil || targetID==uuid.Nil{return ErrGovernanceInputInvalid}
 if actorID==targetID{return ErrGovernanceSelfTarget}
 ctx,cancel:=context.WithTimeout(ctx,s.Cfg.DBTimeout);defer cancel()
 action:="moderate_user_profile";if profile!=nil{action="update_user_profile"}
 return s.withGovernanceTx(ctx,func(tx pgx.Tx) error {
  subjects,err:=s.governance().LockGovernanceSubjectsTx(ctx,tx,actorID,targetID);if err!=nil{return err}
  if err:=AuthorizeAccountStewardship(actorFromSubject(subjects[actorID]),targetFromSubject(targetID,subjects[targetID]));err!=nil{return err}
  if err:=s.governance().RequirePermissionTx(ctx,tx,actorID,action);err!=nil{return err}
  if profile!=nil{profile.UserID=targetID;err=s.Models.UserProfile.UpdateTx(ctx,tx,profile)
  }else{err=s.Models.UserProfile.FlagUserProfileTx(ctx,tx,targetID,flagged,notes)}
  if err!=nil{return err}
  return s.governance().InsertGovernanceAuditTx(ctx,tx,data.GovernanceAuditEntry{ActorID:&actorID,Action:action,
   EntityType:"user_profile",EntityID:targetID.String(),Outcome:data.GovernanceOutcomeSucceeded,Context:data.GovernanceAuditContext{Operation:action}})
 })
}
