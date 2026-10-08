// focodebase/fobackend/internal/services/ce_governance_regression_test.go
// CE regression coverage. Runs only against the explicitly disposable database
// accepted by newGovernanceHarness; no production database reset is permitted.
package services

import (
 "context"
 "errors"
 "testing"
 "time"
 "github.com/PiccoloMondoC/focodebase/fobackend/internal/data"
 "github.com/PiccoloMondoC/focodebase/fobackend/internal/security"
 "github.com/jackc/pgx/v5"
)

func TestCEGovernanceRegressions(t *testing.T) {
 h:=newGovernanceHarness(t)
 rootID,rootEmail:=h.account("ce-root",true)
 _,_,err:=h.svc.EstablishRootSuperAdminInternal(h.ctx,rootEmail);expectOK(t,"bootstrap",err)
 adminID,_:=h.account("ce-admin",true)
 expectOK(t,"appoint admin",h.govern(rootID,adminID,GovernanceAppointAdmin,"",""))

 t.Run("password change invalidates an in-flight appointment proof",func(t *testing.T){
  targetID,email:=h.account("ce-stepup",true)
  tx,err:=h.pool.Begin(h.ctx);expectOK(t,"begin credential race",err)
  defer tx.Rollback(context.Background())
  expectOK(t,"hold governance mutex",h.svc.governance().AcquireGovernanceAdvisoryLockTx(h.ctx,tx))
  var oldHash string
  expectOK(t,"read old hash",tx.QueryRow(h.ctx,`SELECT password_hash FROM users WHERE id=$1`,rootID).Scan(&oldHash))
  replacement,err:=security.HashPassword("Another-Valid-Passw0rd!");expectOK(t,"replacement hash",err)
  defer h.pool.Exec(context.Background(),`UPDATE users SET password_hash=$2 WHERE id=$1`,rootID,oldHash)
  done:=make(chan error,1)
  go func(){done<-h.govern(rootID,targetID,GovernanceAppointSuperAdmin,testPassword,email)}()
  // Observe the request waiting on the advisory mutex, rather than assuming
  // a sleep proves password verification happened. It can reach this wait
  // only after it has verified the original credential.
  deadline:=time.NewTimer(10*time.Second);defer deadline.Stop()
  ticker:=time.NewTicker(20*time.Millisecond);defer ticker.Stop()
  waiting:=false
  for !waiting {
   select {
   case err:=<-done:t.Fatalf("appointment completed before credential race: %v",err)
   case <-deadline.C:t.Fatal("appointment did not reach governance mutex")
   case <-ticker.C:
    expectOK(t,"observe waiter",h.pool.QueryRow(h.ctx,`SELECT EXISTS(SELECT 1 FROM pg_stat_activity
     WHERE datname=current_database() AND wait_event='advisory' AND pid<>pg_backend_pid())`).Scan(&waiting))
   }
  }
  _,err=tx.Exec(h.ctx,`UPDATE users SET password_hash=$2 WHERE id=$1`,rootID,replacement);expectOK(t,"change password",err)
  expectOK(t,"credential commit",tx.Commit(h.ctx))
  select {case err:=<-done:expectErr(t,"stale step-up proof",err,ErrGovernanceReauthenticationFailed);case <-time.After(5*time.Second):t.Fatal("appointment did not finish")}
  if h.effectiveRole(targetID)!="consumer"{t.Fatal("stale credential appointed a Super Admin")}
 })
 t.Run("permission name cannot silently weaken root",func(t *testing.T){
  expectErr(t,"rename governing permission",h.exec(`UPDATE permissions SET name='root_permission_was_renamed' WHERE name='read_admin_console'`),data.ErrGovernanceRoleProtected)
 })
 t.Run("suspended appointment blocks account destruction",func(t *testing.T){
  expectOK(t,"suspend",h.govern(rootID,adminID,GovernanceSuspend,"",""))
  expectErr(t,"self closure",h.svc.CloseOwnAccountInternal(h.ctx,adminID),ErrGovernanceAdministratorSelfClosure)
  expectErr(t,"direct soft delete",h.exec(`UPDATE users SET deleted_at=NOW() WHERE id=$1`,adminID),data.ErrAdministrativeInconsistency)
  expectOK(t,"restore",h.govern(rootID,adminID,GovernanceRestore,"",""))
 })
 t.Run("repeated audit actions have independent identities",func(t *testing.T){
  err:=h.svc.withGovernanceTx(h.ctx,func(tx pgx.Tx) error{
   entry:=data.GovernanceAuditEntry{ActorID:&rootID,Action:"suspend_administrator",EntityType:"administrator",EntityID:adminID.String(),Outcome:data.GovernanceOutcomeDenied}
   if err:=h.svc.governance().InsertGovernanceAuditTx(h.ctx,tx,entry);err!=nil{return err}
   return h.svc.governance().InsertGovernanceAuditTx(h.ctx,tx,entry)
  });expectOK(t,"two same-action records",err)
 })
 var controlID,ordinaryID string
 expectOK(t,"control id",h.pool.QueryRow(h.ctx,`SELECT id::text FROM platform_settings WHERE setting_key='platform_settings_admin_enabled'`).Scan(&controlID))
 expectOK(t,"ordinary id",h.pool.QueryRow(h.ctx,`SELECT id::text FROM platform_settings WHERE setting_key='merchant_onboarding_enabled'`).Scan(&ordinaryID))
 control,err:=h.svc.Models.PlatformSetting.GetByKey(h.ctx,SettingsAdministrationKey);expectOK(t,"read control",err)
 ordinary,err:=h.svc.Models.PlatformSetting.GetByKey(h.ctx,"merchant_onboarding_enabled");expectOK(t,"read setting",err)

 t.Run("role permission replacement rolls back with its audit",func(t *testing.T){
  role,err:=h.svc.Models.Role.GetRoleByName(h.ctx,"viewer");expectOK(t,"viewer",err)
  _,err=h.pool.Exec(h.ctx,`INSERT INTO role_permissions(role_id,permission_id)
   SELECT $1,id FROM permissions WHERE name='read_admin_console' ON CONFLICT DO NOTHING`,role.ID);expectOK(t,"test grant",err)
  _,err=h.pool.Exec(h.ctx,`UPDATE actions SET name='ce_hidden_role_permissions_action' WHERE name='update_role_permissions'`);expectOK(t,"hide audit action",err)
  defer h.pool.Exec(context.Background(),`UPDATE actions SET name='update_role_permissions' WHERE name='ce_hidden_role_permissions_action'`)
  err=h.svc.WithRoleAdministrationInternal(h.ctx,rootID,"update_role_permissions","update_role_permissions",role.ID.String(),func(ctx context.Context,tx pgx.Tx) error{
   return h.svc.Models.RolePermission.ReplacePermissionsForRoleTx(ctx,tx,role.ID,nil)
  })
  expectErr(t,"unaudited replacement",err,data.ErrGovernanceAuditMetadataMissing)
  if n:=h.count(`SELECT COUNT(*) FROM role_permissions WHERE role_id=$1`,role.ID);n<1{t.Fatal("permission deletion escaped audit rollback")}
 })
 t.Run("setting mutation rolls back if audit metadata is absent",func(t *testing.T){
  // Metadata manipulation is test-only; save/restore around the assertion.
  _,err:=h.pool.Exec(h.ctx,`UPDATE actions SET name='ce_hidden_update_setting_action' WHERE name='update_platform_setting_value'`);expectOK(t,"hide action",err)
  defer h.pool.Exec(context.Background(),`UPDATE actions SET name='update_platform_setting_value' WHERE name='ce_hidden_update_setting_action'`)
  _,err=h.svc.MutatePlatformSettingInternal(h.ctx,SettingGovernanceRequest{ActorID:rootID,ID:ordinary.ID,Operation:SettingUpdate,Value:[]byte("false"),ValueType:data.PlatformSettingValueTypeBoolean})
  if !errors.Is(err,data.ErrGovernanceAuditMetadataMissing){t.Fatalf("want missing audit metadata, got %v",err)}
  current,err:=h.svc.Models.PlatformSetting.GetByID(h.ctx,ordinary.ID);expectOK(t,"read after rollback",err)
  if string(current.SettingValue)!="true"{t.Fatal("setting mutation escaped audit rollback")}
 })
 t.Run("disable and mutation are ordered by one transaction",func(t *testing.T){
  tx,err:=h.pool.Begin(h.ctx);expectOK(t,"begin disable",err)
  defer tx.Rollback(context.Background())
  expectOK(t,"governance mutex",h.svc.governance().AcquireGovernanceAdvisoryLockTx(h.ctx,tx))
  _,err=tx.Exec(h.ctx,`UPDATE platform_settings SET setting_value='false'::jsonb WHERE id=$1`,control.ID);expectOK(t,"disable write",err)
  done:=make(chan error,1)
  started:=make(chan struct{})
  go func(){close(started);_,err:=h.svc.MutatePlatformSettingInternal(h.ctx,SettingGovernanceRequest{ActorID:adminID,ID:ordinary.ID,Operation:SettingUpdate,Value:[]byte("false"),ValueType:data.PlatformSettingValueTypeBoolean});done<-err}()
  <-started
  select {case err:=<-done:t.Fatalf("mutation completed before disable commit: %v",err);case <-time.After(100*time.Millisecond):}
  expectOK(t,"disable commit",tx.Commit(h.ctx))
  select {case err:=<-done:expectErr(t,"ordered mutation",err,ErrSettingsAdministrationDisabled);case <-time.After(5*time.Second):t.Fatal("mutation stuck")}
  current,err:=h.svc.Models.PlatformSetting.GetByID(h.ctx,ordinary.ID);expectOK(t,"read unchanged",err)
  if string(current.SettingValue)!="true"{t.Fatal("mutation crossed disable")}
 })
 t.Run("only super admin can recover administration",func(t *testing.T){
  req:=SettingGovernanceRequest{ActorID:adminID,ID:control.ID,Operation:SettingUpdate,Value:[]byte("true"),ValueType:data.PlatformSettingValueTypeBoolean}
  _,err:=h.svc.MutatePlatformSettingInternal(h.ctx,req);expectErr(t,"admin recovery",err,ErrGovernanceInsufficientAuthority)
  req.ActorID=rootID;_,err=h.svc.MutatePlatformSettingInternal(h.ctx,req);expectOK(t,"root recovery",err)
 })
 t.Run("generic role mutation cannot govern suspended super admin",func(t *testing.T){
  superID,email:=h.account("ce-super",true)
  expectOK(t,"appoint super",h.govern(rootID,superID,GovernanceAppointSuperAdmin,testPassword,email))
  expectOK(t,"suspend super",h.govern(rootID,superID,GovernanceSuspend,"",""))
  role,err:=h.svc.Models.Role.GetRoleByName(h.ctx,"consumer");expectOK(t,"consumer",err)
  expectErr(t,"admin cannot edit suspended super",h.svc.ChangeOrdinaryRoleInternal(h.ctx,adminID,superID,role.ID,false,false),ErrGovernanceInsufficientAuthority)
 })
}

func TestCESettingsEnsureRecoveryPolicy(t *testing.T) {
 if code:=DecideSettingMutation(false,true,SettingEnsure,SettingsAdministrationKey,true,[]byte("true"));code!=""{t.Fatalf("missing control recovery refused: %s",code)}
 if code:=DecideSettingMutation(false,false,SettingEnsure,SettingsAdministrationKey,true,[]byte("true"));code!="insufficient_authority"{t.Fatalf("admin recovered missing control: %s",code)}
 if code:=DecideSettingMutation(false,true,SettingEnsure,SettingsAdministrationKey,false,[]byte("true"));code!="platform_settings_administration_disabled"{t.Fatalf("inactive control counted as recovery: %s",code)}
}
