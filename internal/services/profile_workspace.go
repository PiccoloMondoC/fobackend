// Package services provides account identity completion and administrative identity safeguards.
// focodebase/fobackend/internal/services/profile_workspace.go

// CE Foundation 3 application boundary. Persisted facts only; no capability policy.
package services
import (
 "context"
 "fmt"
 "strings"
 "time"
 "github.com/PiccoloMondoC/focodebase/fobackend/internal/data"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
)
type ProfileFact struct {Field string `json:"field"`;Status string `json:"status"`;Reason string `json:"reason,omitempty"`}
type ProfileAssessment struct {Fields []ProfileFact `json:"fields"`;Complete int `json:"complete_count"`;Missing int `json:"missing_count"`;Invalid int `json:"invalid_count"`;Total int `json:"total_count"`}
// ProfileDetails explicitly excludes moderation, lifecycle and authority fields.
// Related records remain normalized and are read-only in this workspace.
type ProfileDetails struct {
 UserID uuid.UUID `json:"user_id"`
 Values map[string]*string `json:"values"`
 SocialLinks []data.UserProfileSocialLink `json:"social_links"`
 NotificationPreferences []data.UserProfileNotificationPreference `json:"notification_preferences"`
 HasPhoto bool `json:"has_photo"`
 CreatedAt time.Time `json:"created_at"`
 UpdatedAt time.Time `json:"updated_at"`
}
type ProfileWorkspace struct {State string `json:"state"`;Profile *ProfileDetails `json:"profile"`;Completeness ProfileAssessment `json:"completeness"`}
func AssessProfileInformation(p *data.UserProfile)ProfileAssessment{
 a:=ProfileAssessment{Fields:[]ProfileFact{}}
 for _,f:=range data.ProfileFieldNames(){
  value:=data.ProfileFieldValue(p,f);fact:=ProfileFact{Field:f,Status:"missing"}
  if value!=nil && strings.TrimSpace(*value)!="" {
   fact.Status="complete";fact.Reason=data.ProfileFieldProblem(f,value)
   if f=="preferred_contact" && *value=="phone" && (p.Phone==nil || strings.TrimSpace(*p.Phone)=="" || data.ProfileFieldProblem("phone",p.Phone)!="") {fact.Reason="phone_required"}
   if fact.Reason!=""{fact.Status="invalid"}
  }
  if f=="avatar_url" && p!=nil && p.HasPhoto { fact.Status="complete"; fact.Reason="" }
  switch fact.Status{case "complete":a.Complete++;case "invalid":a.Invalid++;default:a.Missing++}
  a.Fields=append(a.Fields,fact)
 }
 a.Total=len(a.Fields);return a
}
func profileWorkspace(p *data.UserProfile)*ProfileWorkspace{
 w:=&ProfileWorkspace{State:"not_started",Completeness:AssessProfileInformation(p)}
 if p==nil{return w}
 values:=map[string]*string{};for _,f:=range data.ProfileFieldNames(){v:=data.ProfileFieldValue(p,f);if v==nil{values[f]=nil}else{copy:=*v;values[f]=&copy}}
 links:=p.SocialLinks;if links==nil{links=[]data.UserProfileSocialLink{}}
 preferences:=p.NotificationPreferences;if preferences==nil{preferences=[]data.UserProfileNotificationPreference{}}
 w.State="active";w.Profile=&ProfileDetails{UserID:p.UserID,Values:values,SocialLinks:links,NotificationPreferences:preferences,HasPhoto:p.HasPhoto,CreatedAt:p.CreatedAt,UpdatedAt:p.UpdatedAt};return w
}
// ValidateProfileServicesInternal is called during construction and startup.
// It validates composition, not account data or activity eligibility.
func(s *Service)ValidateProfileServicesInternal()error{
 if s==nil||s.Models==nil||s.Models.DB==nil||s.Models.UserProfile.DB!=s.Models.DB||s.Models.UserProfile.Logger==nil{
  return fmt.Errorf("%w: canonical profile model and logger required",ErrInvalidServiceConfiguration)
 }
 return nil
}
func(s *Service)profileRequest(ctx context.Context,id uuid.UUID)(context.Context,context.CancelFunc,error){
 if err:=s.validate();err!=nil{return nil,nil,err};if err:=s.ValidateProfileServicesInternal();err!=nil{return nil,nil,err}
 if ctx==nil{return nil,nil,ErrNilContext};if id==uuid.Nil{return nil,nil,data.ErrProfileAccount}
 ctx,cancel:=context.WithTimeout(ctx,s.Cfg.DBTimeout);return ctx,cancel,nil
}
func(s *Service)profileTransaction(ctx context.Context,fn func(pgx.Tx)error)error{
 tx,err:=s.Models.DB.Begin(ctx);if err!=nil{return data.ProfileDatabaseError(err)}
 defer func(){cleanup,cancel:=context.WithTimeout(context.Background(),3*time.Second);defer cancel();_ = tx.Rollback(cleanup)}()
 if err=fn(tx);err!=nil{return data.ProfileDatabaseError(err)}
 // Unknown commit outcomes remain server errors. Clients reload rather than
 // blindly replaying a write. No credentials are issued or consumed here.
 return data.ProfileDatabaseError(tx.Commit(ctx))
}
func(s *Service)GetOwnProfileInternal(ctx context.Context,id uuid.UUID)(*ProfileWorkspace,error){
 ctx,cancel,err:=s.profileRequest(ctx,id);if err!=nil{return nil,err};defer cancel()
 var result *ProfileWorkspace
 err=s.profileTransaction(ctx,func(tx pgx.Tx)error{
  m:=&s.Models.UserProfile;if err:=m.LockProfileAccountTx(ctx,tx,id);err!=nil{return err}
  p,err:=m.ReadProfileTx(ctx,tx,id,false);if err!=nil{return err}
  if err=m.OwnProfileRelationsTx(ctx,tx,p);err!=nil{return err};result=profileWorkspace(p);return nil
 });if err!=nil{return nil,err};return result,nil
}
func(s *Service)SaveOwnProfileInternal(ctx context.Context,id uuid.UUID,changes data.ProfileChanges,expected *time.Time,create bool)(*ProfileWorkspace,error){
 ctx,cancel,err:=s.profileRequest(ctx,id);if err!=nil{return nil,err};defer cancel()
 if !create && expected==nil{return nil,data.ErrProfileVersionRequired}
 var result *ProfileWorkspace
 err=s.profileTransaction(ctx,func(tx pgx.Tx)error{
  m:=&s.Models.UserProfile;if err:=m.LockProfileAccountTx(ctx,tx,id);err!=nil{return err}
  before,err:=m.ReadProfileTx(ctx,tx,id,true);if err!=nil{return err}
  if create && before!=nil{return data.ErrProfileExists};if !create && before==nil{return data.ErrUserProfileNotFound}
  if err=m.LoadPrivateProfileIdentityTx(ctx,tx,before);err!=nil{return err}
  seed:=data.UserProfile{UserID:id,PreferredContact:"email"}
  if before!=nil{seed=*before;if !before.UpdatedAt.Equal(*expected){return data.ErrProfileConflict}}
  next,fields,err:=data.ApplyProfileChanges(seed,changes,create);if err!=nil{return err}
  missing:=data.ProfileFieldErrors{}
  if next.FullLegalName==nil||strings.TrimSpace(*next.FullLegalName)==""||data.ProfileFieldProblem("full_legal_name",next.FullLegalName)!=""{missing["full_legal_name"]="required"}
  if next.UserHandle==nil||strings.TrimSpace(*next.UserHandle)==""||data.ProfileFieldProblem("user_handle",next.UserHandle)!=""{missing["user_handle"]="required"}
  if len(missing)>0{return missing}
  if !create && len(fields)==0 && next.UserHandle!=nil {
   var claimed bool
   if err:=tx.QueryRow(ctx,`SELECT EXISTS(SELECT 1 FROM global_handles WHERE handle=$1 AND entity_type='user' AND entity_id=$2)`,*next.UserHandle,id).Scan(&claimed);err!=nil{return err}
   if !claimed{fields=append(fields,"user_handle")}
  }
  action:="update_user_profile"
  if create {action="create_user_profile";err=m.CreateProfileTx(ctx,tx,&next)}else if len(fields)>0{err=m.SaveProfileTx(ctx,tx,before,&next)}
  if err!=nil{return err}
  if create||len(fields)>0{if err=m.AuditProfileTx(ctx,tx,&id,id.String(),action,fields);err!=nil{return err}}
  stored,err:=m.ReadProfileTx(ctx,tx,id,false);if err!=nil{return err}
  if err=m.OwnProfileRelationsTx(ctx,tx,stored);err!=nil{return err};result=profileWorkspace(stored);return nil
 });if err!=nil{return nil,err};return result,nil
}
func(s *Service)AssessOwnProfileCompletenessInternal(ctx context.Context,id uuid.UUID)(ProfileAssessment,error){w,err:=s.GetOwnProfileInternal(ctx,id);if err!=nil{return ProfileAssessment{},err};return w.Completeness,nil}
func(s *Service)ProfileHandleAvailabilityInternal(ctx context.Context,id uuid.UUID,handle string)(string,error){
 ctx,cancel,err:=s.profileRequest(ctx,id);if err!=nil{return "",err};defer cancel()
 if len(handle)>120{return "invalid",nil};return s.Models.UserProfile.ProfileHandleStatus(ctx,id,handle)
}

// Existing heuristic is retained. Recheck the current persisted profile under
// lock, skip already flagged records, and persist moderation and audit together.
func(s *Service)autoFlagProfileAtomic(ctx context.Context,id uuid.UUID)error{
 ctx,cancel,err:=s.profileRequest(ctx,id);if err!=nil{return err};defer cancel()
 return s.profileTransaction(ctx,func(tx pgx.Tx)error{
  m:=&s.Models.UserProfile;if err:=m.LockProfileAccountTx(ctx,tx,id);err!=nil{return err}
  p,err:=m.ReadProfileTx(ctx,tx,id,true);if err!=nil{return err};if p==nil||p.IsFlagged{return nil}
  if err=m.ProfileRelationsTx(ctx,tx,p);err!=nil{return err};if !shouldFlagProfile(p){return nil}
  if err=m.ModerateProfileTx(ctx,tx,nil,id,true,userProfileAutoFlagReason);err!=nil{return err}
  return m.AuditProfileTx(ctx,tx,nil,id.String(),userProfileAuditActionAutoFlagProfile,[]string{"is_flagged"})
 })
}

// Typed identity bridge for existing asynchronous profile helpers. New request
// workflows also receive the actor explicitly; no body value selects an owner.
type profileActorContextKey struct{}
func WithProfileActorContext(ctx context.Context,id uuid.UUID)context.Context{return context.WithValue(ctx,profileActorContextKey{},id)}

// This projection contains public identity only. No private profile data is
// returned to greeting consumers, persisted in browser storage, or inferred.
func(s *Service)GetOwnPublicIdentityInternal(ctx context.Context,id uuid.UUID)(map[string]any,error){
 ctx,cancel,err:=s.profileRequest(ctx,id);if err!=nil{return nil,err};defer cancel()
 identity,err:=s.AccountIdentityInternal(ctx,id);if err!=nil{return nil,err}
 return map[string]any{"user_handle":identity.UserHandle,"profile_complete":identity.ProfileComplete,"email_prefix":identity.EmailPrefix},nil
}
func profileAdministrativeView(p *data.UserProfile)map[string]any{
 // Flat directory contract retained. Legal names (new and old) never enter it.
 return map[string]any{"user_id":p.UserID,"user_handle":p.UserHandle,"company":p.Company,"is_flagged":p.IsFlagged,"updated_at":p.UpdatedAt}
}

// Authorized single-record administration retains non-legal detail and
// normalized relationships. Directory listings keep their smaller projection.
func profileAdministrativeDetail(p *data.UserProfile)map[string]any{
 result:=profileAdministrativeView(p)
 result["phone"]=p.Phone;result["preferred_contact"]=p.PreferredContact
 result["avatar_url"]=p.AvatarURL;result["bio"]=p.Bio;result["location"]=p.Location;result["website"]=p.Website
 result["moderation_notes"]=p.ModerationNotes;result["social_links"]=p.SocialLinks;result["notification_preferences"]=p.NotificationPreferences
 return result
}
