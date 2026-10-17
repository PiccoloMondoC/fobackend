// Owner-run only. Reuses the established explicitly disposable governance DB
// harness and its reset guards. No new database-reset convention is introduced.
//
// focodebase/fobackend/internal/services/site_maintenance_integration_test.go
package services

import (
 "context"
 "crypto/ed25519"
 "crypto/rand"
 "errors"
 "sync"
 "testing"
 "time"
 "github.com/PiccoloMondoC/focodebase/fobackend/internal/data"
 "github.com/PiccoloMondoC/focodebase/fobackend/internal/governancerecovery"
 "github.com/PiccoloMondoC/focodebase/fobackend/internal/security"
 "github.com/google/uuid"
)
func TestCEMaintenanceVertical(t *testing.T){
 h,root,_,co,email:=continuityFixture(t)
 expectOK(t,"startup validation",h.svc.InitializeMaintenanceInternal(h.ctx))
 admin,_:=h.account("maintenance-admin",true);expectOK(t,"appoint admin",h.govern(root,admin,GovernanceAppointAdmin,"",""))
 read:=func()*MaintenanceWorkspace{v,e:=h.svc.ReadMaintenanceInternal(h.ctx,root);expectOK(t,"read maintenance",e);return v}
 command:=func(c MaintenanceCommand)(*MaintenanceWorkspace,error){if c.ActorID==uuid.Nil{c.ActorID=root};if c.ExpectedVersion==0{c.ExpectedVersion=read().State.Version};return h.svc.ExecuteMaintenanceInternal(h.ctx,c)}
 for _,actor:=range []uuid.UUID{admin,co}{_,err:=h.svc.ReadMaintenanceInternal(h.ctx,actor);expectErr(t,"non-Root read",err,ErrGovernanceInsufficientAuthority)}
 if h.count(`SELECT count(*) FROM role_permissions rp JOIN roles r ON r.id=rp.role_id JOIN permissions p ON p.id=rp.permission_id WHERE r.name='admin' AND p.name IN ('read_site_maintenance','manage_site_maintenance')`)!=0{t.Fatal("admin received maintenance permission")}
 if h.count(`SELECT count(*) FROM audit_logs l JOIN entity_types e ON e.id=l.entity_type_id WHERE e.name='site_maintenance' AND l.outcome='denied'`)<2{t.Fatal("denials were not audited")}
 appointTestCoRoot(t,h,root,co,email)
 _,err:=h.svc.ReadMaintenanceInternal(h.ctx,co);expectOK(t,"co-Root access",err)
 v,err:=command(MaintenanceCommand{Operation:"save",StartsAt:time.Now().UTC().Add(time.Hour),DurationMinutes:60,Message:"Routine work"});expectOK(t,"publish",err)
 if v.State.Active{t.Fatal("announcement took platform offline")};firstID:=v.State.Announcement.ID
 v,err=command(MaintenanceCommand{Operation:"activate",CurrentPassword:testPassword,LinkAnnouncement:true});expectOK(t,"activate",err)
 if !v.State.Active || v.State.Announcement.ID!=firstID{t.Fatal("activation state incorrect")}
 expectOK(t,"restart schema",(&data.DBConnectionParamsModel{Logger:h.svc.Logger}).CreateTables(h.pool))
 expectOK(t,"restart seed",(&data.DBConnectionParamsModel{Logger:h.svc.Logger}).SeedAllData(h.pool,data.OAuthClientSeedSecrets{}))
 if !read().State.Active{t.Fatal("restart restored platform")}
 t.Run("audit rollback",func(t *testing.T){
  before:=read().State.Version
  _,err:=h.pool.Exec(h.ctx,`UPDATE actions SET name='ce_maintenance_hidden_restore' WHERE name='site_maintenance_restore'`);expectOK(t,"hide audit metadata",err)
  defer h.pool.Exec(context.Background(),`UPDATE actions SET name='site_maintenance_restore' WHERE name='ce_maintenance_hidden_restore'`)
  _,err=command(MaintenanceCommand{Operation:"restore",CurrentPassword:testPassword});expectErr(t,"audit failure",err,data.ErrGovernanceAuditMetadataMissing)
  after:=read().State;if !after.Active || after.Version!=before{t.Fatal("unaudited restoration escaped rollback")}
 })
 t.Run("database backstops",func(t *testing.T){
  for _,sql:=range []string{`DELETE FROM site_maintenance_state`,`TRUNCATE site_maintenance_state`,`DELETE FROM site_maintenance_transitions`,`TRUNCATE site_maintenance_transitions`,`UPDATE site_maintenance_state SET version=version+1`,`UPDATE site_maintenance_transitions SET reason='changed'`}{if _,err:=h.pool.Exec(h.ctx,sql);err==nil{t.Fatalf("unwitnessed/tampered write accepted: %s",sql)}}
 })
 t.Run("parallel restore one winner",func(t *testing.T){
  version:=read().State.Version;var wg sync.WaitGroup;results:=make(chan error,4)
  for i:=0;i<4;i++{wg.Add(1);go func(){defer wg.Done();_,err:=h.svc.ExecuteMaintenanceInternal(h.ctx,MaintenanceCommand{ActorID:root,Operation:"restore",ExpectedVersion:version,CurrentPassword:testPassword});results<-err}()};wg.Wait();close(results)
  wins:=0;for err:=range results{if err==nil{wins++}else if !errors.Is(err,data.ErrMaintenanceConflict){t.Fatalf("unexpected race failure: %v",err)}}
  if wins!=1 || read().State.Active{t.Fatal("restoration race violated version boundary")}
 })
 t.Run("credential change invalidates proof",func(t *testing.T){
  version:=read().State.Version;tx,err:=h.pool.Begin(h.ctx);expectOK(t,"begin race",err);defer tx.Rollback(context.Background())
  expectOK(t,"hold mutex",h.svc.governance().AcquireGovernanceAdvisoryLockTx(h.ctx,tx))
  var old string;expectOK(t,"old credential",tx.QueryRow(h.ctx,`SELECT password_hash FROM users WHERE id=$1`,root).Scan(&old))
  replacement,err:=security.HashPassword("Changed-Maintenance-Passw0rd!");expectOK(t,"hash replacement",err)
  defer h.pool.Exec(context.Background(),`UPDATE users SET password_hash=$2 WHERE id=$1`,root,old)
  done:=make(chan error,1);go func(){_,err:=h.svc.ExecuteMaintenanceInternal(h.ctx,MaintenanceCommand{ActorID:root,Operation:"activate",ExpectedVersion:version,CurrentPassword:testPassword});done<-err}()
  deadline:=time.NewTimer(10*time.Second);defer deadline.Stop();ticker:=time.NewTicker(20*time.Millisecond);defer ticker.Stop();waiting:=false
  for !waiting{select{case err:=<-done:t.Fatalf("did not wait for lock: %v",err);case <-deadline.C:t.Fatal("mutex wait not observed");case <-ticker.C:expectOK(t,"observe waiter",h.pool.QueryRow(h.ctx,`SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND wait_event='advisory' AND pid<>pg_backend_pid())`).Scan(&waiting))}}
  _,err=tx.Exec(h.ctx,`UPDATE users SET password_hash=$2 WHERE id=$1`,root,replacement);expectOK(t,"change credential",err);expectOK(t,"commit credential",tx.Commit(h.ctx))
  select{case err:=<-done:expectErr(t,"stale proof",err,ErrGovernanceReauthenticationFailed);case <-time.After(5*time.Second):t.Fatal("proof did not finish")}
  if read().State.Active{t.Fatal("stale proof activated maintenance")}
 })
 t.Run("signed Engineering recovery and replay",func(t *testing.T){
  _,err:=command(MaintenanceCommand{Operation:"activate",CurrentPassword:testPassword});expectOK(t,"activate for recovery",err)
  request:=governancerecovery.MaintenanceRecoveryRequest{Reference:"RECOVERY-TEST",Executor:"engineering-test",Reason:"Public application unavailable"}
  plan,err:=governancerecovery.PrepareMaintenance(h.ctx,h.pool,request);expectOK(t,"prepare recovery",err)
  bp,bk,err:=ed25519.GenerateKey(rand.Reader);expectOK(t,"Board fixture",err);cp,ck,err:=ed25519.GenerateKey(rand.Reader);expectOK(t,"CIO fixture",err)
  id:=uuid.New();message,err:=governancerecovery.MaintenanceApprovalMessage(request,plan.Digest,id);expectOK(t,"message",err)
  approval,err:=governancerecovery.VerifyMaintenanceApproval(request,plan.Digest,id,bp,cp,ed25519.Sign(bk,message),ed25519.Sign(ck,message));expectOK(t,"approvals",err)
  repeated,err:=governancerecovery.ExecuteMaintenance(h.ctx,h.pool,h.svc.Logger,request,plan.Digest,id,approval);expectOK(t,"execute recovery",err);if repeated || read().State.Active{t.Fatal("recovery failed")}
  repeated,err=governancerecovery.ExecuteMaintenance(h.ctx,h.pool,h.svc.Logger,request,plan.Digest,id,approval);expectOK(t,"reconcile retry",err);if !repeated{t.Fatal("retry executed twice")}
  if h.count(`SELECT count(*) FROM root_super_admin WHERE user_id=$1`,root)!=1{t.Fatal("maintenance recovery changed incumbency")}
 })
}
