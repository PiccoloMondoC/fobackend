// Package data provides account identity completion and administrative identity safeguards.
// focodebase/fobackend/internal/data/profile_transactions.go

// CE Foundation 3: users -> profile -> global handle -> audit lock order.
// Services own transaction lifetime. No transaction method commits independently.
package data
import (
 "context"
 "encoding/json"
 "errors"
 "strings"
 "time"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
)
const profileColumns = `user_id,first_name,last_name,user_handle,phone,avatar_url,bio,location,website,company,preferred_contact,is_flagged,moderation_notes,created_at,updated_at,deleted_at`

func ProfileDatabaseError(err error)error{
 if err==nil{return nil}
 if IsSerializationOrDeadlock(err){return ErrProfileConflict}
 if IsPgErrorCode(err,"23505") {
  switch PgErrorConstraintName(err){
  case "user_profiles_pkey":return ErrProfileExists
  case "user_profiles_user_handle_key","pk_global_handles","ux_global_handles_namespace":return ErrUserHandleTaken
  }
 }
 return err
}
// The users row serializes profile writes with administrative eligibility checks.
// Acquire it before profile, private identity, handle, or audit locks.
func (m *UserProfileModel) LockProfileAccountTx(ctx context.Context,tx pgx.Tx,id uuid.UUID)error{
 var active bool;var deleted *time.Time
 err:=tx.QueryRow(ctx,`SELECT is_active,deleted_at FROM users WHERE id=$1 FOR UPDATE`,id).Scan(&active,&deleted)
 if errors.Is(err,pgx.ErrNoRows){return ErrProfileAccount};if err!=nil{return err}
 if !active||deleted!=nil{return ErrProfileAccount};return nil
}
func (m *UserProfileModel) ReadProfileTx(ctx context.Context,tx pgx.Tx,id uuid.UUID,write bool)(*UserProfile,error){
 lock:=" FOR SHARE";if write{lock=" FOR UPDATE"}
 p,err:=m.scanUserProfile(tx.QueryRow(ctx,`SELECT `+profileColumns+` FROM user_profiles WHERE user_id=$1`+lock,id))
 if errors.Is(err,pgx.ErrNoRows){return nil,nil};if err!=nil{return nil,err}
 if p.IsDeleted(){return nil,ErrProfileUnavailable}
 if err=tx.QueryRow(ctx,`SELECT EXISTS(SELECT 1 FROM media_objects WHERE owner_user_id=$1 AND purpose='profile_photo' AND state='active')`,id).Scan(&p.HasPhoto);err!=nil{return nil,err}
 return p,nil
}
func (m *UserProfileModel) ProfileRelationsTx(ctx context.Context,tx pgx.Tx,p *UserProfile)error{
 if p==nil{return nil}
 links,err:=m.loadProfileSocialLinks(ctx,tx,p.UserID);if err!=nil{return err}
 preferences,err:=m.loadProfileNotificationPreferences(ctx,tx,p.UserID);if err!=nil{return err}
 p.SocialLinks=links;p.NotificationPreferences=preferences;return nil
}
func (m *UserProfileModel) claimProfileHandleTx(ctx context.Context,tx pgx.Tx,p *UserProfile)error{
 if p.UserHandle==nil{return nil}
 // The unique namespace index also arbitrates concurrent merchant claims.
 var other bool
 if err:=tx.QueryRow(ctx,`SELECT EXISTS(SELECT 1 FROM user_profiles WHERE user_handle=$1 AND user_id<>$2)`,*p.UserHandle,p.UserID).Scan(&other);err!=nil{return err}
 if other{return ErrUserHandleTaken}
 _,err:=tx.Exec(ctx,`INSERT INTO global_handles(handle,entity_type,entity_id) VALUES($1,'user',$2)`,*p.UserHandle,p.UserID)
 return ProfileDatabaseError(err)
}
func (m *UserProfileModel) CreateProfileTx(ctx context.Context,tx pgx.Tx,p *UserProfile)error{
 _,err:=tx.Exec(ctx,`INSERT INTO user_profiles(user_id,first_name,last_name,user_handle,phone,avatar_url,bio,location,website,company,preferred_contact) VALUES($1,COALESCE($2,''),COALESCE($3,''),$4,$5,$6,$7,$8,$9,$10,$11)`,p.UserID,p.FirstName,p.LastName,p.UserHandle,p.Phone,p.AvatarURL,p.Bio,p.Location,p.Website,p.Company,p.PreferredContact)
 if err!=nil{return ProfileDatabaseError(err)}
 if err=m.claimProfileHandleTx(ctx,tx,p);err!=nil{return err}
 return m.savePrivateProfileIdentityTx(ctx,tx,p)
}
func (m *UserProfileModel) SaveProfileTx(ctx context.Context,tx pgx.Tx,before,after *UserProfile)error{
 if !profileEqual(before.UserHandle,after.UserHandle){
  if before.UserHandle!=nil {
   _,err:=tx.Exec(ctx,`DELETE FROM global_handles WHERE handle=$1 AND entity_type='user' AND entity_id=$2`,*before.UserHandle,before.UserID);if err!=nil{return err}
  }
  if err:=m.claimProfileHandleTx(ctx,tx,after);err!=nil{return err}
 }
 if profileEqual(before.UserHandle,after.UserHandle) && after.UserHandle!=nil {
  var claimed bool
  if err:=tx.QueryRow(ctx,`SELECT EXISTS(SELECT 1 FROM global_handles WHERE handle=$1 AND entity_type='user' AND entity_id=$2)`,*after.UserHandle,after.UserID).Scan(&claimed);err!=nil{return err}
  if !claimed {if err:=m.claimProfileHandleTx(ctx,tx,after);err!=nil{return err}}
 }
 _,err:=tx.Exec(ctx,`UPDATE user_profiles SET user_handle=$2,phone=$3,avatar_url=$4,bio=$5,location=$6,website=$7,company=$8,preferred_contact=$9 WHERE user_id=$1 AND deleted_at IS NULL`,after.UserID,after.UserHandle,after.Phone,after.AvatarURL,after.Bio,after.Location,after.Website,after.Company,after.PreferredContact)
 if err!=nil{return ProfileDatabaseError(err)}
 return m.savePrivateProfileIdentityTx(ctx,tx,after)
}
func (m *UserProfileModel) ModerateProfileTx(ctx context.Context,tx pgx.Tx,actor *uuid.UUID,id uuid.UUID,flagged bool,notes string)error{
 if len([]rune(notes))>4000{return ErrUserProfileInvalidInput}
 p,err:=m.ReadProfileTx(ctx,tx,id,true);if err!=nil{return err};if p==nil{return ErrUserProfileNotFound}
 _,err=tx.Exec(ctx,`UPDATE user_profiles SET is_flagged=$2,moderation_notes=NULLIF($3,'') WHERE user_id=$1`,id,flagged,strings.TrimSpace(notes));if err!=nil{return err}
 action:="unflagged";if flagged{action="flagged"}
 _,err=tx.Exec(ctx,`INSERT INTO user_profile_moderation_logs(user_id,action,note,performed_by) VALUES($1,$2,NULLIF($3,''),$4)`,id,action,strings.TrimSpace(notes),actor);return err
}
// Profile audit detail is intentionally value-free. Metadata and event share
// the mutation transaction: audit failure rolls back the business write.
func (m *UserProfileModel) AuditProfileTx(ctx context.Context,tx pgx.Tx,actor *uuid.UUID,target,action string,fields []string)error{
 var actionID,entityID uuid.UUID
 err:=tx.QueryRow(ctx,`INSERT INTO actions(name,description) VALUES($1,'Profile operation') ON CONFLICT(name) DO UPDATE SET name=EXCLUDED.name RETURNING id`,action).Scan(&actionID);if err!=nil{return err}
 err=tx.QueryRow(ctx,`INSERT INTO entity_types(name,description) VALUES('user_profile','User profile') ON CONFLICT(name) DO UPDATE SET name=EXCLUDED.name RETURNING id`).Scan(&entityID);if err!=nil{return err}
 detail,err:=json.Marshal(struct{Fields []string `json:"changed_fields,omitempty"`}{fields});if err!=nil{return err}
 _,err=tx.Exec(ctx,`INSERT INTO audit_logs(id,user_id,action_id,entity_type_id,entity_id,context) VALUES($1,$2,$3,$4,$5,$6::jsonb)`,uuid.New(),actor,actionID,entityID,target,detail);return err
}
func (p *UserProfile) IsDeleted()bool{return p!=nil && p.DeletedAt!=nil}

// Retained internal handle helpers use the actual schema column name.
func(m *UserProfileModel)insertGlobalHandleTx(ctx context.Context,tx pgx.Tx,handle,entityType string,id uuid.UUID)error{
 _,err:=tx.Exec(ctx,`INSERT INTO global_handles(handle,entity_type,entity_id) VALUES($1,$2,$3)`,handle,entityType,id);return ProfileDatabaseError(err)
}
func(m *UserProfileModel)deleteGlobalHandleTx(ctx context.Context,tx pgx.Tx,handle,entityType string)error{
 _,err:=tx.Exec(ctx,`DELETE FROM global_handles WHERE handle=$1 AND entity_type=$2`,handle,entityType);return err
}

func rollbackProfileTransaction(tx pgx.Tx){
 ctx,cancel:=context.WithTimeout(context.Background(),3*time.Second);defer cancel();_ = tx.Rollback(ctx)
}

// Only explicitly supplied full legal names enter private storage. Old first/
// last values remain unchanged, are not guessed/concatenated, and imply no verification.
func(m *UserProfileModel)savePrivateProfileIdentityTx(ctx context.Context,tx pgx.Tx,p *UserProfile)error{
 if p.FullLegalName==nil{return nil}
 _,err:=tx.Exec(ctx,`INSERT INTO user_profile_private_identities(user_id,full_legal_name) VALUES($1,$2) ON CONFLICT(user_id) DO UPDATE SET full_legal_name=EXCLUDED.full_legal_name,updated_at=clock_timestamp() WHERE user_profile_private_identities.full_legal_name IS DISTINCT FROM EXCLUDED.full_legal_name`,p.UserID,*p.FullLegalName);return err
}

// Private legal identity is loaded only for owner workflows, never directory,
// moderation or general administrative reads. Names are not auto-converted.
func(m *UserProfileModel)LoadPrivateProfileIdentityTx(ctx context.Context,tx pgx.Tx,p *UserProfile)error{
 if p==nil{return nil}
 return tx.QueryRow(ctx,`SELECT (SELECT full_legal_name FROM user_profile_private_identities WHERE user_id=$1)`,p.UserID).Scan(&p.FullLegalName)
}
func(m *UserProfileModel)OwnProfileRelationsTx(ctx context.Context,tx pgx.Tx,p *UserProfile)error{
 if err:=m.LoadPrivateProfileIdentityTx(ctx,tx,p);err!=nil{return err}
 return m.ProfileRelationsTx(ctx,tx,p)
}
