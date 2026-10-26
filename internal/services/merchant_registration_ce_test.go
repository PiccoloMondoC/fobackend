// Package services provides CE regression protection for registration security, lifecycle, and compatibility.
// focodebase/fobackend/internal/services/merchant_registration_ce_test.go
//
// GTM:
//     Layer: 2.4 Merchant Registration / Application Services
//     Release Class: SPINE
//     Reason: Supports the complete Foundation Level 4 merchant-registration and account-navigation vertical.
//
// SPINE Rule:
//     Keep compiling and preserve established security, lifecycle, and privacy contracts.
//     Preserve canonical principal ownership and backend authorization.
//     Keep private registration values out of public projections, logs, and errors.
//
package services

import (
 "context"
 "errors"
 "testing"

 "github.com/PiccoloMondoC/focodebase/fobackend/internal/data"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
)

func TestMerchantRegistration_CECompletionReplayCannotEditCompletedFacts(t *testing.T){
 f:=newRegistrationFixture(t);owner:=f.user("consumer",true)
 first,err:=f.complete(owner,0,fullRegistration(uniqueBrand("Original")));if err!=nil{t.Fatal(err)}
 r:=first.View.Registration
 edited:=r.Fields;edited.ContactName=regStr("New responsible person")
 saved,err:=f.save(owner,r.Revision,edited);if err!=nil{t.Fatal(err)}
 replay,err:=f.complete(owner,0,fullRegistration(uniqueBrand("Stale replay")));if err!=nil{t.Fatal(err)}
 if replay.NewlyCompleted||replay.View.Registration.Revision!=saved.Registration.Revision||
 !replay.View.Registration.Fields.Equal(saved.Registration.Fields){t.Fatal("completion replay edited completed business information")}
 if a,e:=f.merchantCounts(owner);a!=1||e!=1{t.Fatalf("duplicate lifecycle result: %d/%d",a,e)}
}
func TestMerchantRegistration_CECanonicalTupleCannotBypassRegistration(t *testing.T){
 f:=newRegistrationFixture(t);owner:=f.user("consumer",true)
 completed,err:=f.complete(owner,0,fullRegistration(uniqueBrand("Canonical")));if err!=nil{t.Fatal(err)}
 merchant:=*completed.View.Merchant;merchant.Name=uniqueBrand("Bypass")
 if err=f.service.Models.Merchant.Update(context.Background(),&merchant);!errors.Is(err,data.ErrMerchantRegistrationManaged){
  t.Fatalf("legacy tuple update should be rejected, got %v",err)
 }
 r,err:=f.service.GetOwnMerchantRegistrationInternal(context.Background(),owner);if err!=nil{t.Fatal(err)}
 if r.Merchant.Name!=*r.Registration.Fields.DBAName{t.Fatal("canonical name diverged")}
}
func TestMerchantRegistration_CEDeactivationPreservesCommittedDraft(t *testing.T){
 f:=newRegistrationFixture(t);owner:=f.user("consumer",true)
 saved,err:=f.save(owner,0,fullRegistration(uniqueBrand("Inactive draft")));if err!=nil{t.Fatal(err)}
 f.exec(`UPDATE users SET is_active=FALSE WHERE id=$1`,owner)
 _,err=f.complete(owner,saved.Registration.Revision,saved.Registration.Fields)
 if !errors.Is(err,ErrMerchantOnboardingActorIneligible){t.Fatalf("deactivated principal completed: %v",err)}
 persisted,err:=f.service.Models.MerchantRegistration.GetByPrincipal(context.Background(),owner)
 if err!=nil||persisted.Status!=data.MerchantRegistrationStatusDraft{t.Fatalf("draft was lost: %v",err)}
 if a,e:=f.merchantCounts(owner);a!=0||e!=0{t.Fatalf("deactivated principal acquired account/event: %d/%d",a,e)}
}
func TestMerchantRegistration_CECanceledWorkReleasesTransaction(t *testing.T){
 f:=newRegistrationFixture(t);owner:=f.user("consumer",true)
 ctx,cancel:=context.WithCancel(context.Background())
 err:=f.service.registrationTx(ctx,pgx.TxOptions{},func(tx pgx.Tx)error{
  if err:=f.service.lockRegistrationActor(ctx,tx,owner);err!=nil{return err}
  cancel();return context.Canceled
 })
 if !errors.Is(err,context.Canceled){t.Fatalf("cancellation not retained: %v",err)}
 if _,err=f.save(owner,0,data.MerchantRegistrationFields{});err!=nil{t.Fatalf("canceled transaction retained lock: %v",err)}
}
func TestMerchantRegistration_CEOwnershipImmutableInDatabase(t *testing.T){
 f:=newRegistrationFixture(t);owner:=f.user("consumer",true);other:=f.user("consumer",true)
 saved,err:=f.save(owner,0,data.MerchantRegistrationFields{});if err!=nil{t.Fatal(err)}
 _,err=f.pool.Exec(context.Background(),`UPDATE merchant_registrations SET principal_user_id=$2,revision=revision+1 WHERE id=$1`,saved.Registration.ID,other)
 if err==nil{t.Fatal("database permitted registration reassignment")}
}
func TestMerchantRegistration_CENoCredentialsOrRoleMutation(t *testing.T){
 f:=newRegistrationFixture(t);owner:=f.user("consumer",true)
 before,err:=f.service.Models.Role.GetRoleByUserID(context.Background(),owner);if err!=nil{t.Fatal(err)}
 if _,err=f.complete(owner,0,fullRegistration(uniqueBrand("No role switch")));err!=nil{t.Fatal(err)}
 after,err:=f.service.Models.Role.GetRoleByUserID(context.Background(),owner);if err!=nil{t.Fatal(err)}
 if before.ID!=after.ID||after.Name!="consumer"{t.Fatal("registration changed account role")}
 if after.ID==uuid.Nil{t.Fatal("missing effective role")}
}
