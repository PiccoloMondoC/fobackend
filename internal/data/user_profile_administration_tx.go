// Package data provides models and database access methods for user profiles and related entities.
//
// focodebase/fobackend/internal/data/user_profile_administration_tx.go
//
// CE transaction composition retains the canonical profile validators,
// handle maintenance and moderation ledger; the service owns commit/audit.
package data
import (
 "context"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
)
// Compatibility for trusted full-record callers; HTTP uses explicit changes.
// Moderation fields are never written through this method.
func(m *UserProfileModel)UpdateTx(ctx context.Context,tx pgx.Tx,p *UserProfile)error{
 if p==nil||p.UserID==uuid.Nil{return ErrUserProfileInvalidInput}
 before,err:=m.ReadProfileTx(ctx,tx,p.UserID,true);if err!=nil{return err};if before==nil{return ErrUserProfileNotFound}
 next,fields,err:=ApplyProfileChanges(*before,profileReplacement(p),false);if err!=nil{return err}
 if len(fields)>0{if err=m.SaveProfileTx(ctx,tx,before,&next);err!=nil{return err}}
 stored,err:=m.ReadProfileTx(ctx,tx,p.UserID,true);if err==nil{*p=*stored};return err
}
func(m *UserProfileModel)FlagUserProfileTx(ctx context.Context,tx pgx.Tx,id uuid.UUID,flagged bool,notes string)error{
 return m.ModerateProfileTx(ctx,tx,nil,id,flagged,notes)
}
