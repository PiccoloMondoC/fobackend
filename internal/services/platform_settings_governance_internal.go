// focodebase/fobackend/internal/services/platform_settings_governance_internal.go
// CE design: current authority + locked policy + mutation + audit in one
// transaction. Ordinary administration is serialized on a dedicated settings
// mutex after user locks. Missing/deleted controls fail closed, with a narrowly
// scoped Super Admin ensure recovery. Setting values never enter logs/audits.
package services

import (
 "context"
 "encoding/json"
 "errors"
 "github.com/PiccoloMondoC/focodebase/fobackend/internal/data"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
)

const SettingsAdministrationKey = "platform_settings_admin_enabled"
const SettingsHardDeleteKey = "platform_settings_hard_delete_enabled"
const settingsMutationLock int64 = 7301195002

type SettingOperation string
const (
 SettingCreate SettingOperation = "create"
 SettingEnsure SettingOperation = "ensure"
 SettingUpdate SettingOperation = "update_value"
 SettingActivate SettingOperation = "set_active"
 SettingSoftDelete SettingOperation = "soft_delete"
 SettingHardDelete SettingOperation = "hard_delete"
)

type SettingGovernanceRequest struct {
 ActorID uuid.UUID
 ID uuid.UUID
 Operation SettingOperation
 Key string
 Value json.RawMessage
 ValueType data.PlatformSettingValueType
 Description string
 Active bool
}

func settingPermission(op SettingOperation, active bool) string {
 switch op {
 case SettingCreate: return "create_platform_setting"
 case SettingEnsure: return "ensure_platform_setting"
 case SettingUpdate: return "update_platform_setting"
 case SettingActivate:
  if active { return "activate_platform_setting" }; return "deactivate_platform_setting"
 case SettingSoftDelete: return "soft_delete_platform_setting"
 case SettingHardDelete: return "hard_delete_platform_setting"
 default: return ""
 }
}
func settingAuditAction(op SettingOperation) string {
 switch op {
 case SettingCreate: return "create_platform_setting"
 case SettingEnsure: return "ensure_platform_setting"
 case SettingUpdate: return "update_platform_setting_value"
 case SettingActivate: return "set_platform_setting_active"
 case SettingSoftDelete: return "soft_delete_platform_setting"
 case SettingHardDelete: return "hard_delete_platform_setting"
 default: return ""
 }
}

func controlEnabled(row *data.PlatformSetting) bool {
 if row==nil || row.DeletedAt!=nil || !row.IsActive || row.ValueType!=data.PlatformSettingValueTypeBoolean { return false }
 var enabled bool
 return json.Unmarshal(row.SettingValue,&enabled)==nil && enabled
}

// DecideSettingMutation is a pure decision, also used in API policy tests.
func DecideSettingMutation(enabled,super bool, op SettingOperation,key string,active bool,value json.RawMessage) string {
 key=data.NormalizePlatformSettingKey(key)
 protected:=key==SettingsAdministrationKey || key==SettingsHardDeleteKey
 if protected {
  if op==SettingSoftDelete || op==SettingHardDelete { return "protected_platform_setting" }
  if !super { return "insufficient_authority" }
 }
 if enabled { return "" }
 var enable bool
 valueTrue:=json.Unmarshal(value,&enable)==nil && enable
 if key==SettingsAdministrationKey && super {
  if op==SettingActivate && active { return "" }
  if op==SettingUpdate && valueTrue { return "" }
  if (op==SettingCreate || op==SettingEnsure) && active && valueTrue { return "" }
 }
 return "platform_settings_administration_disabled"
}

func (s *Service) MutatePlatformSettingInternal(ctx context.Context, req SettingGovernanceRequest) (*data.PlatformSetting,error) {
 if err:=s.validate();err!=nil { return nil,err }
 if ctx==nil { return nil,ErrNilContext }
 permission:=settingPermission(req.Operation,req.Active)
 if req.ActorID==uuid.Nil || permission=="" { return nil,data.ErrPlatformSettingInvalidInput }
 if req.Operation!=SettingCreate && req.Operation!=SettingEnsure && req.ID==uuid.Nil { return nil,data.ErrPlatformSettingInvalidInput }
 req.Key=data.NormalizePlatformSettingKey(req.Key)
 if req.Operation==SettingCreate || req.Operation==SettingEnsure {
  if err:=data.ValidatePlatformSettingMutation(req.Key,req.Value,req.ValueType);err!=nil{return nil,err}
 }
 ctx,cancel:=context.WithTimeout(ctx,s.Cfg.DBTimeout);defer cancel()
 gov:=s.governance()
 model:=s.Models.PlatformSetting
 var result *data.PlatformSetting
 err:=s.withGovernanceTx(ctx,func(tx pgx.Tx) error {
  subjects,err:=gov.LockGovernanceSubjectsTx(ctx,tx,req.ActorID);if err!=nil{return err}
  actor:=actorFromSubject(subjects[req.ActorID])
  if actor.Level==data.AdministrativeLevelNone{return ErrGovernanceInsufficientAuthority}
  if err:=gov.RequirePermissionTx(ctx,tx,req.ActorID,permission);err!=nil{return err}
  if _,err:=tx.Exec(ctx,`SELECT pg_advisory_xact_lock($1)`,settingsMutationLock);err!=nil{return err}
  controls,err:=model.LockControlSettingsTx(ctx,tx);if err!=nil{return err}
  var current *data.PlatformSetting
  key:=req.Key
  if req.Operation!=SettingCreate && req.Operation!=SettingEnsure {
   current,err=model.GetByIDTx(ctx,tx,req.ID);if err!=nil{return err}
   key=current.SettingKey
   if current.DeletedAt!=nil && req.Operation!=SettingHardDelete{return data.ErrPlatformSettingNotFound}
  }
  code:=DecideSettingMutation(controlEnabled(controls[SettingsAdministrationKey]),actor.Level==data.AdministrativeLevelSuperAdmin,req.Operation,key,req.Active,req.Value)
  switch code {
  case "protected_platform_setting": return ErrProtectedPlatformSetting
  case "insufficient_authority": return ErrGovernanceInsufficientAuthority
  case "platform_settings_administration_disabled": return ErrSettingsAdministrationDisabled
  }
  if key==SettingsAdministrationKey || key==SettingsHardDeleteKey {
   if (req.Operation==SettingCreate || req.Operation==SettingEnsure || req.Operation==SettingUpdate) && data.NormalizePlatformSettingValueType(req.ValueType)!=data.PlatformSettingValueTypeBoolean{return data.ErrPlatformSettingTypeMismatch}
  }
  switch req.Operation {
  case SettingCreate,SettingEnsure:
   result,err=model.CreateTx(ctx,tx,&data.PlatformSetting{SettingKey:key,SettingValue:req.Value,ValueType:req.ValueType,Description:req.Description,IsActive:req.Active,UpdatedBy:&req.ActorID},req.Operation==SettingEnsure)
  case SettingUpdate:
   result,err=model.UpdateValueTx(ctx,tx,current,req.Value,req.ValueType,req.Description,req.ActorID)
  case SettingActivate:
   result,err=model.SetActiveTx(ctx,tx,req.ID,req.Active,req.ActorID)
  case SettingSoftDelete,SettingHardDelete:
   if req.Operation==SettingHardDelete && !controlEnabled(controls[SettingsHardDeleteKey]) { return ErrSettingsHardDeleteDisabled }
   err=model.DeleteTx(ctx,tx,req.ID,req.ActorID,req.Operation==SettingHardDelete)
   result=current
  }
  if err!=nil{return err}
  return gov.InsertGovernanceAuditTx(ctx,tx,data.GovernanceAuditEntry{ActorID:&req.ActorID,
   Action:settingAuditAction(req.Operation),EntityType:"platform_setting",EntityID:result.ID.String(),Outcome:data.GovernanceOutcomeSucceeded,
   Context:data.GovernanceAuditContext{Operation:string(req.Operation)}})
 })
 if err!=nil { return nil,err }
 return result,nil
}

var (
 ErrSettingsAdministrationDisabled=errors.New("platform settings administration is disabled")
 ErrSettingsHardDeleteDisabled=errors.New("platform settings hard delete is disabled")
 ErrProtectedPlatformSetting=errors.New("protected platform setting")
)
