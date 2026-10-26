// Package data provides profile persistence, discovery, handle, and lifecycle operations.
// focodebase/fobackend/internal/data/profile_model_operations.go

package data
import (
 "context"
 "errors"
 "sort"
 "strings"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
)
// profileModelTx keeps compatibility callers on the same account-first lock
// order. Request handlers use the service transaction instead, with audit.
func(m *UserProfileModel)profileModelTx(ctx context.Context,id uuid.UUID,fn func(pgx.Tx)error)error{
 ctx,cancel:=context.WithTimeout(ctx,dbTimeout);defer cancel()
 tx,err:=m.DB.Begin(ctx);if err!=nil{return err};defer rollbackProfileTransaction(tx)
 if err=m.LockProfileAccountTx(ctx,tx,id);err!=nil{return err}
 if err=fn(tx);err!=nil{return ProfileDatabaseError(err)};return ProfileDatabaseError(tx.Commit(ctx))
}
func profileReplacement(p *UserProfile)ProfileChanges{
 changes:=ProfileChanges{};for _,f:=range ProfileFieldNames(){if f=="full_legal_name" && p.FullLegalName==nil{continue};changes[f]=ProfileFieldValue(p,f)};return changes
}
func(m *UserProfileModel)Insert(ctx context.Context,p *UserProfile)error{
 if p==nil||p.UserID==uuid.Nil{return ErrUserProfileInvalidInput}
 seed:=UserProfile{UserID:p.UserID,PreferredContact:"email",FirstName:p.FirstName,LastName:p.LastName}
 changes:=profileReplacement(p);if p.PreferredContact==""{delete(changes,"preferred_contact")}
 next,_,err:=ApplyProfileChanges(seed,changes,true);if err!=nil{return err}
 err=m.profileModelTx(ctx,p.UserID,func(tx pgx.Tx)error{
  existing,err:=m.ReadProfileTx(ctx,tx,p.UserID,true);if err!=nil{return err};if existing!=nil{return ErrProfileExists}
  if err=m.CreateProfileTx(ctx,tx,&next);err!=nil{return err}
  stored,err:=m.ReadProfileTx(ctx,tx,p.UserID,true);if err!=nil{return err};if err=m.LoadPrivateProfileIdentityTx(ctx,tx,stored);err!=nil{return err};next=*stored;return nil
 });if err==nil{*p=next};return err
}
func(m *UserProfileModel)Update(ctx context.Context,p *UserProfile)error{
 if p==nil{return ErrUserProfileInvalidInput};next:=*p
 err:=m.profileModelTx(ctx,p.UserID,func(tx pgx.Tx)error{return m.UpdateTx(ctx,tx,&next)});if err==nil{*p=next};return err
}
func(m *UserProfileModel)FlagUserProfile(ctx context.Context,id uuid.UUID,flagged bool,notes string)error{
 return m.profileModelTx(ctx,id,func(tx pgx.Tx)error{return m.FlagUserProfileTx(ctx,tx,id,flagged,notes)})
}
func(m *UserProfileModel)GetByUserID(ctx context.Context,id uuid.UUID)(*UserProfile,error){
 p,err:=m.scanUserProfile(m.DB.QueryRow(ctx,`SELECT `+profileColumns+` FROM user_profiles WHERE user_id=$1 AND deleted_at IS NULL`,id))
 if errors.Is(err,pgx.ErrNoRows){return nil,nil};if err!=nil{return nil,err};if err=m.populateRelations(ctx,p);err!=nil{return nil,err};return p,nil
}
func(m *UserProfileModel)GetByUserHandle(ctx context.Context,handle string)(*UserProfile,error){
 id,err:=m.GetUserIDByHandle(ctx,handle);if errors.Is(err,ErrUserHandleNotFound){return nil,nil};if err!=nil{return nil,err};return m.GetByUserID(ctx,id)
}
func(m *UserProfileModel)profileList(ctx context.Context,query string,limit,offset int)([]*UserProfile,error){return m.ProfileListTx(ctx,m.DB,query,limit,offset)}
func(m *UserProfileModel)ProfileListTx(ctx context.Context,q profileQueryer,query string,limit,offset int)([]*UserProfile,error){
 if limit<1||limit>100{limit=20};if offset<0{offset=0}
 // strpos implements literal case-insensitive substring matching, not a
 // client-controlled wildcard language. Stable user_id breaks timestamp ties.
 rows,err:=q.Query(ctx,`SELECT `+profileColumns+` FROM user_profiles WHERE deleted_at IS NULL AND ($1='' OR strpos(lower(concat_ws(' ',user_handle,company,location,bio)),lower($1))>0) ORDER BY updated_at DESC,user_id LIMIT $2 OFFSET $3`,query,limit,offset)
 if err!=nil{return nil,err};defer rows.Close()
 result:=[]*UserProfile{};for rows.Next(){p,err:=m.scanUserProfile(rows);if err!=nil{return nil,err};result=append(result,p)}
 return result,rows.Err() // no nested pool acquisition with an open cursor
}
func(m *UserProfileModel)GetAll(ctx context.Context,limit,offset int)([]*UserProfile,error){
 profiles,err:=m.profileList(ctx,"",limit,offset);if err!=nil{return nil,err}
 for _,p:=range profiles{if err=m.populateRelations(ctx,p);err!=nil{return nil,err}};return profiles,nil
}
func(m *UserProfileModel)SearchUserProfiles(ctx context.Context,q string,limit,offset int)([]*UserProfile,error){
 q=strings.TrimSpace(q);if q==""||len([]rune(q))>200{return nil,ErrUserProfileInvalidInput};profiles,err:=m.profileList(ctx,q,limit,offset);if err!=nil{return nil,err}
 for _,p:=range profiles{if err=m.populateRelations(ctx,p);err!=nil{return nil,err}};return profiles,nil
}
func(m *UserProfileModel)GetUserIDByHandle(ctx context.Context,handle string)(uuid.UUID,error){
 handle=strings.ToLower(strings.TrimSpace(handle));if err:=validateUserHandle(handle);err!=nil{return uuid.Nil,err}
 var id uuid.UUID;err:=m.DB.QueryRow(ctx,`SELECT user_id FROM user_profiles WHERE user_handle=$1 AND deleted_at IS NULL`,handle).Scan(&id)
 if errors.Is(err,pgx.ErrNoRows){return uuid.Nil,ErrUserHandleNotFound};return id,err
}
func(m *UserProfileModel)ExistsByUserID(ctx context.Context,id uuid.UUID)(bool,error){var exists bool;err:=m.DB.QueryRow(ctx,`SELECT EXISTS(SELECT 1 FROM user_profiles WHERE user_id=$1 AND deleted_at IS NULL)`,id).Scan(&exists);return exists,err}
func(m *UserProfileModel)ValidateUserHandle(ctx context.Context,handle string)error{
 status,err:=m.ProfileHandleStatus(ctx,uuid.Nil,handle);if err!=nil{return err};if status=="invalid"{return ErrUserProfileInvalidInput};if status=="unavailable"{return ErrUserHandleTaken};return nil
}
func(m *UserProfileModel)ProfileHandleStatus(ctx context.Context,id uuid.UUID,handle string)(string,error){
 handle=strings.ToLower(strings.TrimSpace(handle));if validateUserHandle(handle)!=nil{return "invalid",nil}
 var held,current bool
 err:=m.DB.QueryRow(ctx,`SELECT EXISTS(SELECT 1 FROM global_handles WHERE lower(handle)=$1 AND NOT(entity_type='user' AND entity_id=$2)) OR EXISTS(SELECT 1 FROM user_profiles WHERE user_handle=$1 AND user_id<>$2), EXISTS(SELECT 1 FROM user_profiles WHERE user_id=$2 AND user_handle=$1 AND deleted_at IS NULL)`,handle,id).Scan(&held,&current)
 if err!=nil{return "",err};if held{return "unavailable",nil};if current{return "current",nil};return "available",nil
}
func(m *UserProfileModel)GetReservedHandles(ctx context.Context)([]string,error){return m.ReservedProfileHandlesTx(ctx,m.DB)}
func(m *UserProfileModel)ReservedProfileHandlesTx(ctx context.Context,q profileQueryer)([]string,error){
 rows,err:=q.Query(ctx,`SELECT handle FROM global_handles UNION SELECT user_handle::text FROM user_profiles WHERE user_handle IS NOT NULL`);if err!=nil{return nil,err};defer rows.Close()
 set:=map[string]bool{};for h:=range reservedHandles{set[h]=true};for rows.Next(){var h string;if err:=rows.Scan(&h);err!=nil{return nil,err};set[h]=true};if err:=rows.Err();err!=nil{return nil,err}
 result:=make([]string,0,len(set));for h:=range set{result=append(result,h)};sort.Strings(result);return result,nil
}
// Soft deletion retains the reservation, matching the retained UNIQUE profile
// row and account closure. This does not introduce a handle recycling policy.
func(m *UserProfileModel)SoftDelete(ctx context.Context,id uuid.UUID)error{
 return m.profileModelTx(ctx,id,func(tx pgx.Tx)error{
  p,err:=m.ReadProfileTx(ctx,tx,id,true);if err!=nil{return err};if p==nil{return ErrUserProfileNotFound}
  _,err=tx.Exec(ctx,`UPDATE user_profiles SET deleted_at=clock_timestamp() WHERE user_id=$1`,id);return err
 })
}
func(m *UserProfileModel)Delete(ctx context.Context,id uuid.UUID)error{
 // Permanent deletion is a trusted internal lifecycle operation, never routed.
 tx,err:=m.DB.Begin(ctx);if err!=nil{return err};defer rollbackProfileTransaction(tx)
 var account uuid.UUID
 if err=tx.QueryRow(ctx,`SELECT id FROM users WHERE id=$1 FOR UPDATE`,id).Scan(&account);err!=nil{return err}
 var found uuid.UUID
 if err=tx.QueryRow(ctx,`DELETE FROM user_profiles WHERE user_id=$1 RETURNING user_id`,id).Scan(&found);err!=nil{return err}
 if _,err=tx.Exec(ctx,`DELETE FROM global_handles WHERE entity_type='user' AND entity_id=$1`,id);err!=nil{return err};return tx.Commit(ctx)
}
