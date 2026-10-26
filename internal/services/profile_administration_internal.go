// Package services provides governance-authorized profile administration and audited administrative reads.

// focodebase/fobackend/internal/services/profile_administration_internal.go
// CE Foundation 3. Governance remains authoritative for cross-account writes.
package services
import (
 "context"
 "time"
 "github.com/PiccoloMondoC/focodebase/fobackend/internal/data"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
)
func(s *Service)profileAdministration(ctx context.Context,actor,target uuid.UUID,action string,fn func(context.Context,pgx.Tx)error)error{
 if err:=s.validate();err!=nil{return err};if ctx==nil{return ErrNilContext}
 if actor==uuid.Nil||target==uuid.Nil{return ErrGovernanceInputInvalid};if actor==target{return ErrGovernanceSelfTarget}
 ctx,cancel:=context.WithTimeout(ctx,s.Cfg.DBTimeout);defer cancel()
 err := s.withGovernanceTx(ctx,func(tx pgx.Tx)error{
  subjects,err:=s.governance().LockGovernanceSubjectsTx(ctx,tx,actor,target);if err!=nil{return err}
  if err=AuthorizeAccountStewardship(actorFromSubject(subjects[actor]),targetFromSubject(target,subjects[target]));err!=nil{return err}
  if err=s.governance().RequirePermissionTx(ctx,tx,actor,action);err!=nil{return err}
  if err=s.Models.UserProfile.LockProfileAccountTx(ctx,tx,target);err!=nil{return err}
  if err=fn(ctx,tx);err!=nil{return data.ProfileDatabaseError(err)}
  return s.governance().InsertGovernanceAuditTx(ctx,tx,data.GovernanceAuditEntry{ActorID:&actor,Action:action,EntityType:"user_profile",EntityID:target.String(),Outcome:data.GovernanceOutcomeSucceeded,Context:data.GovernanceAuditContext{Operation:action}})
 })
 if isAuditableDenial(err) {
  auditCtx,auditCancel:=context.WithTimeout(context.WithoutCancel(ctx),s.Cfg.DBTimeout)
  defer auditCancel()
  if auditErr:=s.governance().InsertGovernanceAuditTx(auditCtx,s.Models.DB,data.GovernanceAuditEntry{ActorID:&actor,Action:action,EntityType:"user_profile",EntityID:target.String(),Outcome:data.GovernanceOutcomeDenied,Context:data.GovernanceAuditContext{Operation:action,DenialCode:GovernanceDenialCode(err)}});auditErr!=nil{
   s.Logger.GetLoggerWithContextFromContext(auditCtx).WithFunctionName("profileAdministration").Warn("profile administration denial audit failed")
  }
 }
 return err
}
func(s *Service)PatchProfileAdministrativeInternal(ctx context.Context,actor,target uuid.UUID,changes data.ProfileChanges,expected *time.Time)(map[string]any,error){
 if _,private:=changes["full_legal_name"];private{return nil,ErrGovernanceInputInvalid}
 if expected==nil{return nil,data.ErrProfileVersionRequired}
 var result map[string]any
 err:=s.profileAdministration(ctx,actor,target,"update_user_profile",func(ctx context.Context,tx pgx.Tx)error{
  p,err:=s.Models.UserProfile.ReadProfileTx(ctx,tx,target,true);if err!=nil{return err};if p==nil{return data.ErrUserProfileNotFound}
  if !p.UpdatedAt.Equal(*expected){return data.ErrProfileConflict}
  next,fields,err:=data.ApplyProfileChanges(*p,changes,false);if err!=nil{return err}
  if len(fields)>0{if err=s.Models.UserProfile.SaveProfileTx(ctx,tx,p,&next);err!=nil{return err}}
  stored,err:=s.Models.UserProfile.ReadProfileTx(ctx,tx,target,false);if err!=nil{return err}
  if err=s.Models.UserProfile.ProfileRelationsTx(ctx,tx,stored);err!=nil{return err};result=profileAdministrativeDetail(stored);return nil
 });if err!=nil{return nil,err};return result,nil
}
func(s *Service)ModerateProfileAdministrativeInternal(ctx context.Context,actor,target uuid.UUID,flagged bool,notes string)error{
 return s.profileAdministration(ctx,actor,target,"moderate_user_profile",func(ctx context.Context,tx pgx.Tx)error{return s.Models.UserProfile.ModerateProfileTx(ctx,tx,&actor,target,flagged,notes)})
}
// Retained signature for trusted existing callers. A full-record replacement
// must carry the version it was read with. Moderation still has its own path.
func(s *Service)ChangeProfileAdministrativeInternal(ctx context.Context,actor,target uuid.UUID,p *data.UserProfile,flagged bool,notes string)error{
 if p==nil{return s.ModerateProfileAdministrativeInternal(ctx,actor,target,flagged,notes)}
 changes:=data.ProfileChanges{};for _,f:=range data.ProfileFieldNames(){if f=="full_legal_name"{continue};changes[f]=data.ProfileFieldValue(p,f)}
 _,err:=s.PatchProfileAdministrativeInternal(ctx,actor,target,changes,&p.UpdatedAt);return err
}
// Administrative reads remain private and audited. Role and read permission
// are checked at the route and handler using the existing authorization model.
func(s *Service)ReadProfileAdministrationInternal(ctx context.Context,actor,target uuid.UUID)(map[string]any,error){
 ctx,cancel,err:=s.profileRequest(ctx,actor);if err!=nil{return nil,err};defer cancel()
 var result map[string]any
 err=s.profileTransaction(ctx,func(tx pgx.Tx)error{
  if _,err:=s.governance().LockGovernanceSubjectsTx(ctx,tx,actor,target);err!=nil{return err}
  if err:=s.governance().RequirePermissionTx(ctx,tx,actor,"read_user_profile");err!=nil{return err}
  m:=&s.Models.UserProfile;if err:=m.LockProfileAccountTx(ctx,tx,target);err!=nil{return err}
  p,err:=m.ReadProfileTx(ctx,tx,target,false);if err!=nil{return err};if p==nil{return data.ErrUserProfileNotFound}
  if err=m.ProfileRelationsTx(ctx,tx,p);err!=nil{return err}
  if err=m.AuditProfileTx(ctx,tx,&actor,target.String(),"read_user_profile_by_user_id",nil);err!=nil{return err}
  result=profileAdministrativeDetail(p);return nil
 });if err!=nil{return nil,err};return result,nil
}
func(s *Service)ListProfileAdministrationInternal(ctx context.Context,actor uuid.UUID,q string,limit,offset int)([]map[string]any,error){
 ctx,cancel,err:=s.profileRequest(ctx,actor);if err!=nil{return nil,err};defer cancel()
 var profiles []*data.UserProfile
 err=s.profileTransaction(ctx,func(tx pgx.Tx)error{
  if _,err:=s.governance().LockGovernanceSubjectsTx(ctx,tx,actor);err!=nil{return err}
  if err:=s.governance().RequirePermissionTx(ctx,tx,actor,"read_user_profiles");err!=nil{return err}
  var readErr error;profiles,readErr=s.Models.UserProfile.ProfileListTx(ctx,tx,q,limit,offset);if readErr!=nil{return readErr}
  return s.Models.UserProfile.AuditProfileTx(ctx,tx,&actor,"batch","read_user_profiles",nil)
 });if err!=nil{return nil,err}
 result:=make([]map[string]any,0,len(profiles));for _,p:=range profiles{result=append(result,profileAdministrativeView(p))};return result,nil
}
func(s *Service)ReservedProfileHandlesInternal(ctx context.Context,actor uuid.UUID)([]string,error){
 ctx,cancel,err:=s.profileRequest(ctx,actor);if err!=nil{return nil,err};defer cancel()
 var handles []string
 err=s.profileTransaction(ctx,func(tx pgx.Tx)error{
  if _,err:=s.governance().LockGovernanceSubjectsTx(ctx,tx,actor);err!=nil{return err}
  if err:=s.governance().RequirePermissionTx(ctx,tx,actor,"read_reserved_handles");err!=nil{return err}
  var readErr error;handles,readErr=s.Models.UserProfile.ReservedProfileHandlesTx(ctx,tx);if readErr!=nil{return readErr}
  return s.Models.UserProfile.AuditProfileTx(ctx,tx,&actor,"reserved","read_reserved_handles",nil)
 });return handles,err
}
