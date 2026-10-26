// Package services provides the merchant-registration transaction state machine and canonical account establishment.
// focodebase/fobackend/internal/services/merchant_onboarding_internal.go
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
// A draft is durable but powerless. Completion creates the canonical merchant,
// principal account and domain event in one transaction after a separate draft
// commit. User roles and authentication credentials are never changed here.
package services

import (
 "context"
 "encoding/json"
 "errors"
 "fmt"
 "strings"
 "time"

 "github.com/PiccoloMondoC/focodebase/fobackend/internal/data"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
)

var (
 ErrMerchantOnboardingActorRequired=errors.New("merchant registration requires an authenticated principal")
 ErrMerchantOnboardingActorIneligible=errors.New("merchant registration is unavailable for this account")
 ErrMerchantRegistrationEditConflict=errors.New("merchant registration changed; reload before editing")
 ErrMerchantRegistrationPrincipalUnavailable=errors.New("merchant account is unavailable")
 // Retained symbols for source compatibility; the old workflow cannot create
 // an active account without the required business registration facts.
 ErrMerchantOnboardingInputInvalid=errors.New("eight-field merchant registration is required")
 ErrMerchantOnboardingAlreadyCompleted=errors.New("merchant onboarding is already completed")
)
const (
 merchantOnboardedEventType="merchant.onboarded.v1"
 merchantOnboardedEventVersion=1
 merchantAggregateType="merchant"
)
type MerchantOnboardingInput struct { Name string; LogoURL,Website *string }
type MerchantOnboardingResult struct { Merchant *data.Merchant; MerchantAccount *data.MerchantAccount }
func(s *Service) OnboardMerchantInternal(ctx context.Context,id uuid.UUID,in MerchantOnboardingInput)(*MerchantOnboardingResult,error) {
 return nil,ErrMerchantOnboardingInputInvalid
}
type MerchantRegistrationInput struct { ExpectedRevision int64; Fields data.MerchantRegistrationFields }
type MerchantRegistrationView struct {
 Eligible bool
 Registration *data.MerchantRegistration
 Account *data.MerchantAccount
 Merchant *data.Merchant
}
type MerchantRegistrationCompletion struct { View *MerchantRegistrationView; NewlyCompleted,MerchantCreated bool }
type merchantOnboardedEventV1 struct {
 MerchantID uuid.UUID `json:"merchant_id"`
 MerchantAccountID uuid.UUID `json:"merchant_account_id"`
 PrincipalUserID uuid.UUID `json:"principal_user_id"`
}
func IsMerchantRegistrationEligibleRole(role string)bool {
 switch strings.ToLower(strings.TrimSpace(role)) { case "consumer","merchant":return true };return false
}
func(s *Service) merchantRegistrationPreconditions(ctx context.Context,id uuid.UUID)error {
 if ctx==nil { return ErrNilContext }; if id==uuid.Nil { return ErrMerchantOnboardingActorRequired };return s.validate()
}
func registrationServiceError(err error)error {
 if err==nil { return nil }
 if errors.Is(err,data.ErrMerchantRegistrationConflict)||errors.Is(err,data.ErrMerchantRegistrationNotDraft) { return ErrMerchantRegistrationEditConflict }
 return err
}
// A fresh cleanup context is essential after cancellation or a timed-out
// COMMIT. pgx closes an unusable connection rather than returning it in-tx.
func(s *Service) registrationTx(ctx context.Context,opts pgx.TxOptions,work func(pgx.Tx)error)error {
 tx,err:=s.Models.DB.BeginTx(ctx,opts)
 if err!=nil { return data.SafeMerchantRegistrationError("begin",err) }
 defer func(){
  cleanup,cancel:=context.WithTimeout(context.Background(),3*time.Second);defer cancel()
  if err:=tx.Rollback(cleanup);err!=nil&&!errors.Is(err,pgx.ErrTxClosed) {
   s.Logger.GetLoggerWithContextFromContext(ctx).Warn("merchant registration transaction cleanup failed")
  }
 }()
 if err:=work(tx);err!=nil { return registrationServiceError(err) }
 return data.SafeMerchantRegistrationError("commit",tx.Commit(ctx))
}
func(s *Service) lockRegistrationActor(ctx context.Context,tx pgx.Tx,id uuid.UUID)error {
 active,err:=s.Models.User.LockMerchantRegistrationActorTx(ctx,tx,id)
 if err!=nil{return err};if !active{return ErrMerchantOnboardingActorIneligible}
 role,err:=s.Models.Role.MerchantRegistrationRoleTx(ctx,tx,id)
 if err!=nil{return err};if !IsMerchantRegistrationEligibleRole(role){return ErrMerchantOnboardingActorIneligible};return nil
}

// Snapshot reads avoid mixing a draft revision from before completion with an
// account from after completion. The same snapshot determines read eligibility.
func(s *Service) GetOwnMerchantRegistrationInternal(ctx context.Context,id uuid.UUID)(*MerchantRegistrationView,error) {
 if err:=s.merchantRegistrationPreconditions(ctx,id);err!=nil{return nil,err}
 ctx,cancel:=context.WithTimeout(ctx,s.Cfg.DBTimeout);defer cancel()
 view:=new(MerchantRegistrationView)
 err:=s.registrationTx(ctx,pgx.TxOptions{IsoLevel:pgx.RepeatableRead,AccessMode:pgx.ReadOnly},func(tx pgx.Tx)error{
  var active bool
  err:=tx.QueryRow(ctx,`SELECT is_active FROM users WHERE id=$1 AND deleted_at IS NULL`,id).Scan(&active)
  if errors.Is(err,pgx.ErrNoRows)||!active&&err==nil{return ErrMerchantOnboardingActorIneligible}
  if err!=nil{return data.SafeMerchantRegistrationError("read actor",err)}
  // The role method is read-only in this snapshot (no locking required).
  role,err:=s.Models.Role.MerchantRegistrationRoleSnapshotTx(ctx,tx,id)
  if err!=nil{return err};view.Eligible=IsMerchantRegistrationEligibleRole(role)
  // An active account holder retains access to their own private business
  // record after a role change; write eligibility is separately revalidated.
  view.Registration,err=s.Models.MerchantRegistration.ReadByPrincipalTx(ctx,tx,id)
  if err!=nil{return err}
  view.Account,err=s.Models.MerchantAccount.ReadRegistrationAccountTx(ctx,tx,id)
  if err!=nil{return err}
  if view.Account!=nil {
   view.Merchant,err=s.Models.Merchant.ReadRegistrationMerchantTx(ctx,tx,view.Account.MerchantID)
  }
  return err
 })
 return view,err
}
func(s *Service) GetOwnMerchantNavigationInternal(ctx context.Context,id uuid.UUID)(*data.MerchantNavigationState,bool,error) {
 if err:=s.merchantRegistrationPreconditions(ctx,id);err!=nil{return nil,false,err}
 ctx,cancel:=context.WithTimeout(ctx,s.Cfg.DBTimeout);defer cancel()
 actor,err:=s.Models.User.GetByID(ctx,id)
 if err!=nil{return nil,false,data.SafeMerchantRegistrationError("navigation actor",err)}
 if actor==nil||!actor.IsActive{return nil,false,ErrMerchantOnboardingActorIneligible}
 state,err:=s.Models.MerchantRegistration.GetNavigationStateForPrincipal(ctx,id)
 return state,IsMerchantRegistrationEligibleRole(actor.RoleName),err
}

// saveRegistration serializes first creation on the users row. Optimistic
// revisions guard real changes; identical replay is harmless at any revision.
// A completion request never edits an already-completed registration.
func(s *Service) saveRegistration(ctx context.Context,id uuid.UUID,in MerchantRegistrationInput,completing bool)(*data.MerchantRegistration,error) {
 if in.ExpectedRevision<0{return nil,data.ErrMerchantRegistrationInvalid}
 fields,err:=data.NormalizeMerchantRegistrationFields(in.Fields);if err!=nil{return nil,err}
 var saved *data.MerchantRegistration
 err=s.registrationTx(ctx,pgx.TxOptions{},func(tx pgx.Tx)error{
  if err:=s.lockRegistrationActor(ctx,tx,id);err!=nil{return err}
  r,err:=s.Models.MerchantRegistration.LockByPrincipalTx(ctx,tx,id);if err!=nil{return err}
  if r==nil {
   if in.ExpectedRevision!=0{return ErrMerchantRegistrationEditConflict}
   var inserted bool
   saved,inserted,err=s.Models.MerchantRegistration.InsertDraftTx(ctx,tx,id,fields)
   if err!=nil{return err};if !inserted{return ErrMerchantRegistrationEditConflict};return nil
  }
  saved=r
  if completing&&r.Status==data.MerchantRegistrationStatusCompleted{return nil}
  if r.Fields.Equal(fields){return nil}
  if r.Revision!=in.ExpectedRevision{return ErrMerchantRegistrationEditConflict}
  if r.Status==data.MerchantRegistrationStatusCompleted {
   fields,err=data.ValidateMerchantRegistrationForCompletion(fields);if err!=nil{return err}
   account,err:=s.Models.MerchantAccount.LockByPrincipalIncludingDeletedTx(ctx,tx,id)
   if err!=nil{return data.SafeMerchantRegistrationError("lock account",err)}
   if account==nil||account.DeletedAt!=nil||r.MerchantID==nil||account.MerchantID!=*r.MerchantID {
    return ErrMerchantRegistrationPrincipalUnavailable
   }
   if _,err=s.Models.Merchant.UpdateNameTx(ctx,tx,account.MerchantID,*fields.DBAName);err!=nil{return translateMerchantNameError(err)}
  }
  saved,err=s.Models.MerchantRegistration.UpdateFieldsTx(ctx,tx,r.ID,id,fields);return err
 })
 return saved,err
}
func(s *Service) SaveOwnMerchantRegistrationInternal(ctx context.Context,id uuid.UUID,in MerchantRegistrationInput)(*MerchantRegistrationView,error) {
 if err:=s.merchantRegistrationPreconditions(ctx,id);err!=nil{return nil,err}
 ctx,cancel:=context.WithTimeout(ctx,s.Cfg.DBTimeout);defer cancel()
 if _,err:=s.saveRegistration(ctx,id,in,false);err!=nil{return nil,err}
 return s.GetOwnMerchantRegistrationInternal(ctx,id)
}
func(s *Service) CompleteOwnMerchantRegistrationInternal(ctx context.Context,id uuid.UUID,in MerchantRegistrationInput)(*MerchantRegistrationCompletion,error) {
 if err:=s.merchantRegistrationPreconditions(ctx,id);err!=nil{return nil,err}
 ctx,cancel:=context.WithTimeout(ctx,s.Cfg.DBTimeout);defer cancel()
 saved,err:=s.saveRegistration(ctx,id,in,true);if err!=nil{return nil,err}
 result:=new(MerchantRegistrationCompletion)
 if saved.Status==data.MerchantRegistrationStatusDraft {
  err=s.registrationTx(ctx,pgx.TxOptions{},func(tx pgx.Tx)error{
   if err:=s.lockRegistrationActor(ctx,tx,id);err!=nil{return err}
   r,err:=s.Models.MerchantRegistration.LockByPrincipalTx(ctx,tx,id);if err!=nil{return err}
   if r==nil{return ErrMerchantRegistrationEditConflict}
   if r.Status==data.MerchantRegistrationStatusCompleted{return nil}
   if r.Revision!=saved.Revision{return ErrMerchantRegistrationEditConflict}
   f,err:=data.ValidateMerchantRegistrationForCompletion(r.Fields);if err!=nil{return err}
   account,err:=s.Models.MerchantAccount.LockByPrincipalIncludingDeletedTx(ctx,tx,id)
   if err!=nil{return data.SafeMerchantRegistrationError("lock completion account",err)}
   if account!=nil&&account.DeletedAt!=nil{return ErrMerchantRegistrationPrincipalUnavailable}
   var merchantID uuid.UUID
   if account!=nil {
    // Preserve pending/suspended/closed status; never reactivate as a side
    // effect of filling business information for an earlier account.
    merchantID=account.MerchantID
    if _,err:=s.Models.Merchant.UpdateNameTx(ctx,tx,merchantID,*f.DBAName);err!=nil{return translateMerchantNameError(err)}
   }else{
    merchant:=&data.Merchant{Name:*f.DBAName}
    if err:=s.Models.Merchant.InsertTx(ctx,tx,merchant);err!=nil{return translateMerchantNameError(err)}
    account,err=s.Models.MerchantAccount.CreateForPrincipalTx(ctx,tx,merchant.ID,id)
    if err!=nil{return data.SafeMerchantRegistrationError("create account",err)}
    merchantID=merchant.ID
    payload,err:=json.Marshal(merchantOnboardedEventV1{merchantID,account.ID,id});if err!=nil{return errors.New("merchant registration event serialization failed")}
    if _,err=s.Models.OutboxEvent.InsertTx(ctx,tx,data.NewOutboxEvent{
     AggregateType:merchantAggregateType,AggregateID:merchantID,
     EventType:merchantOnboardedEventType,EventVersion:merchantOnboardedEventVersion,
     Payload:payload,IdempotencyKey:fmt.Sprintf("%s:%s",merchantOnboardedEventType,merchantID),
    });err!=nil{return data.SafeMerchantRegistrationError("outbox",err)}
    result.MerchantCreated=true
   }
   if _,err:=s.Models.MerchantRegistration.MarkCompletedTx(ctx,tx,r.ID,id,merchantID);err!=nil{return err}
   result.NewlyCompleted=true
   return nil
  })
  if err!=nil{return nil,err}
 }
 result.View,err=s.GetOwnMerchantRegistrationInternal(ctx,id)
 return result,err
}
func translateMerchantNameError(err error)error {
 if errors.Is(err,data.ErrMerchantIdentityConflict){return data.NewMerchantRegistrationValidationError(data.MerchantRegistrationFieldErrors{data.MerchantRegistrationFieldDBAName:data.MerchantRegistrationErrTaken})}
 if errors.Is(err,data.ErrMerchantInvalid){return data.NewMerchantRegistrationValidationError(data.MerchantRegistrationFieldErrors{data.MerchantRegistrationFieldDBAName:data.MerchantRegistrationErrInvalid})}
 return data.SafeMerchantRegistrationError("merchant identity",err)
}
