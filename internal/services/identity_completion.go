// Package services provides account identity completion and administrative identity safeguards.
// focodebase/fobackend/internal/services/identity_completion.go

package services

import (
 "context"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "github.com/PiccoloMondoC/focodebase/fobackend/internal/data"
)
func (s *Service) AccountIdentityInternal(ctx context.Context,id uuid.UUID)(*data.AdministrativeIdentity,error){
 if err:=s.validate();err!=nil{return nil,err};if ctx==nil{return nil,ErrNilContext};if id==uuid.Nil{return nil,ErrGovernanceInputInvalid}
 ctx,cancel:=context.WithTimeout(ctx,s.Cfg.DBTimeout);defer cancel()
 identities,err:=data.ReadAdministrativeIdentities(ctx,s.Models.DB,id);if err!=nil{return nil,err};return identities[id],nil
}
// The caller already holds the target users row FOR UPDATE. Profile writers
// acquire that same row first, so eligibility cannot change before commit.
func requireCompletedIdentityTx(ctx context.Context,tx pgx.Tx,id uuid.UUID)error{
 identities,err:=data.ReadAdministrativeIdentities(ctx,tx,id);if err!=nil{return err}
 if identities[id]==nil || !identities[id].ProfileComplete{return ErrGovernanceTargetIneligible};return nil
}
func identityEligibleActions(actions []string,identity *data.AdministrativeIdentity)[]string{
 if identity!=nil && identity.ProfileComplete{return actions}
 result:=make([]string,0,len(actions));for _,action:=range actions{
  switch action {case string(GovernanceAppointAdmin),string(GovernanceAppointSuperAdmin),string(GovernanceRestore),ActionPromoteToSuperAdmin:continue};result=append(result,action)
 };return result
}
