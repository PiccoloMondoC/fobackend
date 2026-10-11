// focodebase/fobackend/internal/services/installation_integration_test.go
// Uses only the existing, explicitly disposable governance test harness.
package services
import (
 "context"
 "errors"
 "strings"
 "sync"
 "testing"
 "time"
 "github.com/PiccoloMondoC/focodebase/fobackend/internal/data"
 "github.com/PiccoloMondoC/focodebase/fobackend/internal/installation"
 "github.com/PiccoloMondoC/focodebase/fobackend/internal/security"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
)
func issueTestClaim(t *testing.T, h *governanceHarness, email string) *installation.IssuedClaim {
 t.Helper()
 claim, err := installation.IssueRootBootstrapClaim(h.ctx,h.pool,h.svc.Logger,email)
 expectOK(t,"issue claim",err)
 return claim
}
func TestInstallationAccountPaths(t *testing.T) {
 for _, kind := range []string{"new", "active", "pending", "inactive", "closed", "passwordless"} {
  t.Run(kind,func(t *testing.T) {
   h:=newGovernanceHarness(t)
   var id uuid.UUID
   email:="new-root@governance.test"
   if kind!="new" { id,email=h.account(kind,kind!="pending" && kind!="inactive") }
   switch kind {
   case "pending":
    expectOK(t,"activation record",h.exec(`INSERT INTO activation_tokens(user_id,token_hash,expires_at) VALUES($1,$2,NOW()+INTERVAL '1 hour')`,id,strings.Repeat("a",64)))
   case "closed": expectOK(t,"close account",h.exec(`UPDATE users SET deleted_at=NOW() WHERE id=$1`,id))
   case "passwordless": expectOK(t,"external account",h.exec(`UPDATE users SET password_hash=NULL,google_id=$2 WHERE id=$1`,id,id.String()))
   }
   var oldHash string
   if kind=="active" {
    expectOK(t,"read password",h.pool.QueryRow(h.ctx,`SELECT password_hash FROM users WHERE id=$1`,id).Scan(&oldHash))
   }
   claim:=issueTestClaim(t,h,email)
   if kind=="active" {
    _,err:=h.svc.CompleteRootBootstrapInternal(h.ctx,claim.Credential,"Wrong-Password-123!")
    expectErr(t,"email alone cannot replace established password",err,ErrBootstrapReauthenticationFailed)
    if h.count(`SELECT COUNT(*) FROM root_super_admin`)!=0 {t.Fatal("wrong password established Root")}
   }
   pi,err:=h.svc.CompleteRootBootstrapInternal(h.ctx,claim.Credential,testPassword)
   if kind=="inactive" || kind=="closed" || kind=="passwordless" {
    expectErr(t,"protected account",err,ErrBootstrapAccountUnavailable)
    if h.count(`SELECT COUNT(*) FROM root_super_admin`)!=0 || h.count(`SELECT COUNT(*) FROM platform_installation`)!=0 {t.Fatal("refusal wrote authority")}
    return
   }
   expectOK(t,"complete",err)
   if kind!="new" && pi.OriginalBootstrapUserID!=id {t.Fatal("immutable account ID replaced")}
   if h.effectiveRole(pi.OriginalBootstrapUserID)!="super_admin" {t.Fatal("wrong primary role")}
   if h.count(`SELECT COUNT(*) FROM users WHERE id=$1 AND is_active AND deleted_at IS NULL`,pi.OriginalBootstrapUserID)!=1 {t.Fatal("account not confirmed")}
   if kind=="pending" && h.count(`SELECT COUNT(*) FROM activation_tokens WHERE user_id=$1`,id)!=0 {t.Fatal("old activation still usable")}
   if kind=="active" {
    var current string
    expectOK(t,"read unchanged password",h.pool.QueryRow(h.ctx,`SELECT password_hash FROM users WHERE id=$1`,id).Scan(&current))
    if current!=oldHash {t.Fatal("established password changed")}
   }
   if h.count(`SELECT COUNT(*) FROM audit_logs WHERE context::text LIKE '%' || $1 || '%'`,claim.Credential)!=0 {t.Fatal("credential in audit")}
  })
 }
}
func TestInstallationReplacementExpiryAndRollback(t *testing.T) {
 h:=newGovernanceHarness(t)
 id,email:=h.account("rollback-root",true)
 first:=issueTestClaim(t,h,email)
 second:=issueTestClaim(t,h,email)
 if !second.Superseded {t.Fatal("replacement did not supersede")}
 _,err:=h.svc.CompleteRootBootstrapInternal(h.ctx,first.Credential,testPassword)
 expectErr(t,"superseded",err,ErrBootstrapCredentialInvalid)
 // Fault injection: missing audit metadata must roll back every authority write.
 expectOK(t,"hide audit metadata",h.exec(`UPDATE actions SET name='installation_test_hidden' WHERE name='initialize_installation'`))
 _,err=h.svc.CompleteRootBootstrapInternal(h.ctx,second.Credential,testPassword)
 expectErr(t,"audit rollback",err,data.ErrGovernanceAuditMetadataMissing)
 if h.count(`SELECT COUNT(*) FROM root_super_admin`)!=0 || h.count(`SELECT COUNT(*) FROM root_succession_events`)!=0 || h.count(`SELECT COUNT(*) FROM platform_installation`)!=0 || h.effectiveRole(id)!="consumer" {t.Fatal("partial authority after rollback")}
 if h.count(`SELECT COUNT(*) FROM platform_bootstrap_claims WHERE status='pending' AND credential_hash=$1`,data.BootstrapCredentialHash(second.Credential))!=1 {t.Fatal("rolled-back completion consumed claim")}
 expectOK(t,"restore audit metadata",h.exec(`UPDATE actions SET name='initialize_installation' WHERE name='installation_test_hidden'`))
 // Expired entitlement is inserted by a trusted issuance transaction; no
 // trigger disabling, clock sleeps, or production schema mutation is needed.
 err=h.svc.withGovernanceTx(h.ctx,func(tx pgx.Tx) error {
  if _,err:=tx.Exec(h.ctx,`SELECT set_config('sagrenti.installation_ceremony','issue',true)`);err!=nil{return err}
  inst:=h.svc.installation()
  if _,err:=inst.SupersedePendingClaimTx(h.ctx,tx);err!=nil{return err}
  _,err:=tx.Exec(h.ctx,`INSERT INTO platform_bootstrap_claims(claimant_email,credential_hash,issued_at,expires_at)
  VALUES($1,$2,clock_timestamp()-INTERVAL '40 minutes',clock_timestamp()-INTERVAL '20 minutes')`,email,data.BootstrapCredentialHash(second.Credential))
  return err
 });expectOK(t,"expired claim fixture",err)
 _,err=h.svc.CompleteRootBootstrapInternal(h.ctx,second.Credential,testPassword)
 expectErr(t,"expired",err,ErrBootstrapCredentialInvalid)
 third:=issueTestClaim(t,h,email)
 _,err=h.svc.CompleteRootBootstrapInternal(h.ctx,third.Credential,testPassword);expectOK(t,"retry after rollback and expiry",err)
 _,err=h.svc.CompleteRootBootstrapInternal(h.ctx,third.Credential,testPassword);expectErr(t,"replay",err,ErrBootstrapCredentialInvalid)
}
func TestInstallationConcurrentIssuance(t *testing.T) {
 h:=newGovernanceHarness(t)
 _,email:=h.account("race-root",true)
 const n=6
 claims:=make([]*installation.IssuedClaim,n);errs:=make([]error,n)
 var wg sync.WaitGroup
 for i:=0;i<n;i++ {wg.Add(1);go func(i int){defer wg.Done();claims[i],errs[i]=installation.IssueRootBootstrapClaim(h.ctx,h.pool,h.svc.Logger,email)}(i)}
 wg.Wait()
 for _,err:=range errs {expectOK(t,"concurrent issue",err)}
 if h.count(`SELECT COUNT(*) FROM platform_bootstrap_claims WHERE status='pending'`)!=1 {t.Fatal("multiple pending entitlements")}
 var winner int=-1
 for i,claim:=range claims {
  if h.count(`SELECT COUNT(*) FROM platform_bootstrap_claims WHERE status='pending' AND credential_hash=$1`,data.BootstrapCredentialHash(claim.Credential))==1 {winner=i}
 }
 if winner<0 {t.Fatal("no winning entitlement")}
 _,err:=h.svc.CompleteRootBootstrapInternal(h.ctx,claims[winner].Credential,testPassword);expectOK(t,"winning claim",err)
 for _,claim:=range claims {_,err=h.svc.CompleteRootBootstrapInternal(h.ctx,claim.Credential,testPassword);expectErr(t,"every issued link closed",err,ErrBootstrapCredentialInvalid)}
}
func TestInstallationDatabaseBackstops(t *testing.T) {
 h:=newGovernanceHarness(t)
 id,email:=h.account("guard-root",true)
 raw,err:=data.GenerateBootstrapCredential();expectOK(t,"credential",err)
 if err:=h.exec(`INSERT INTO platform_bootstrap_claims(claimant_email,credential_hash,expires_at) VALUES($1,$2,clock_timestamp()+INTERVAL '10 minutes')`,email,data.BootstrapCredentialHash(raw));!errors.Is(err,data.ErrInstallationProtected){t.Fatalf("unmarked issue: %v",err)}
 claim:=issueTestClaim(t,h,email)
 _,err=h.svc.CompleteRootBootstrapInternal(h.ctx,claim.Credential,testPassword);expectOK(t,"complete",err)
 for _,stmt:=range []string{
  `UPDATE platform_installation SET completed_at=NOW()`,
  `DELETE FROM platform_installation`,
  `TRUNCATE platform_installation`,
  `DELETE FROM platform_bootstrap_claims`,
  `TRUNCATE platform_bootstrap_claims`,
  `UPDATE platform_bootstrap_claims SET status='pending',claimant_email='attacker@governance.test',credential_hash=decode(repeat('ab',32),'hex'),resolved_at=NULL,completed_user_id=NULL`,
 } {if err:=h.exec(stmt);err==nil {t.Fatalf("backstop bypass: %s",stmt)}}
 // Restart-equivalent source steps cannot change account, authority, or receipt.
 before:=h.count(`SELECT COUNT(*) FROM audit_logs`)
 db:=&data.DBConnectionParamsModel{Logger:h.svc.Logger}
 expectOK(t,"repeat canonical schema",db.CreateTables(h.pool))
 expectOK(t,"repeat seeds",db.SeedAllData(h.pool,data.OAuthClientSeedSecrets{}))
 if h.count(`SELECT COUNT(*) FROM audit_logs`)!=before || h.count(`SELECT COUNT(*) FROM platform_installation WHERE original_bootstrap_user_id=$1`,id)!=1 {t.Fatal("restart changed installation")}
 // All bootstrap state is read-only to the API startup report.
 status,err:=h.svc.GetInstallationStatusInternal(context.Background());expectOK(t,"status",err)
 if !status.Installed {t.Fatal("installed state lost")}
}
func TestInstallationRecognizesEstablishedRoot(t *testing.T) {
 h:=newGovernanceHarness(t)
 id,email:=h.account("established-root",true)
 // Reconstruct the exact pre-Drop-2 ceremony using only removed Drop-2 gates
 // inside a rollback-safe transaction. Existing SPINE protections stay enabled.
 err:=h.svc.withGovernanceTx(h.ctx,func(tx pgx.Tx) error {
  for _,stmt:=range []string{
   `DROP TRIGGER require_installation_bootstrap_ceremony ON root_super_admin`,
   `DROP TRIGGER require_installation_bootstrap_ceremony ON root_succession_events`,
   `DROP TRIGGER assert_installation_consistency ON root_super_admin`,
   `DROP TRIGGER assert_installation_consistency ON root_succession_events`,
  } {if _,err:=tx.Exec(h.ctx,stmt);err!=nil{return err}}
  gov:=h.svc.governance()
  reason:="Pre-Drop-2 test fixture"
  if _,err:=gov.PutAppointmentTx(h.ctx,tx,data.AppointmentChange{UserID:id,Level:data.AdministrativeLevelSuperAdmin,Status:data.AppointmentStatusActive,Reason:&reason,NewTerm:true});err!=nil{return err}
  roleID,err:=h.svc.Models.Role.ResolveRoleIDByNameTx(h.ctx,tx,data.RoleNameSuperAdmin);if err!=nil{return err}
  if err:=h.svc.Models.Role.AssignPrimaryRoleTx(h.ctx,tx,id,roleID,nil);err!=nil{return err}
  return gov.InsertRootSuperAdminTx(h.ctx,tx,id)
 });expectOK(t,"existing Root fixture",err)
 var oldHash string
 expectOK(t,"existing hash",h.pool.QueryRow(h.ctx,`SELECT password_hash FROM users WHERE id=$1`,id).Scan(&oldHash))
 db:=&data.DBConnectionParamsModel{Logger:h.svc.Logger}
 expectOK(t,"add Drop 2 canonical definitions",db.CreateTables(h.pool))
 // Root existence alone closes issuance, before recognition is recorded.
 _,err=installation.IssueRootBootstrapClaim(h.ctx,h.pool,h.svc.Logger,email)
 expectErr(t,"existing Root refuses issue",err,data.ErrInstallationAlreadyInitialized)
 expectOK(t,"recognize existing",db.SeedAllData(h.pool,data.OAuthClientSeedSecrets{}))
 expectOK(t,"recognize idempotently",db.SeedAllData(h.pool,data.OAuthClientSeedSecrets{}))
 if h.count(`SELECT COUNT(*) FROM platform_installation WHERE original_bootstrap_user_id=$1 AND establishment_source='existing_root' AND bootstrap_claim_id IS NULL`,id)!=1 {t.Fatal("existing authority not recognized")}
 if h.count(`SELECT COUNT(*) FROM root_succession_events WHERE successor_user_id=$1`,id)!=1 {t.Fatal("existing ledger changed")}
 if h.count(`SELECT COUNT(*) FROM audit_logs al JOIN actions a ON a.id=al.action_id WHERE a.name='recognize_existing_installation'`)!=1 {t.Fatal("recognition audit not idempotent")}
 var current string
 expectOK(t,"unchanged hash",h.pool.QueryRow(h.ctx,`SELECT password_hash FROM users WHERE id=$1`,id).Scan(&current))
 if current!=oldHash || security.VerifyPassword(testPassword,current)!=nil {t.Fatal("existing password changed")}
}

func TestInstallationRejectsStalePasswordProof(t *testing.T) {
 h:=newGovernanceHarness(t)
 id,email:=h.account("credential-race",true)
 claim:=issueTestClaim(t,h,email)
 tx,err:=h.pool.Begin(h.ctx);expectOK(t,"begin race",err)
 defer tx.Rollback(context.Background())
 expectOK(t,"hold governance mutex",h.svc.governance().AcquireGovernanceAdvisoryLockTx(h.ctx,tx))
 done:=make(chan error,1)
 go func(){_,err:=h.svc.CompleteRootBootstrapInternal(h.ctx,claim.Credential,testPassword);done<-err}()
 deadline:=time.NewTimer(10*time.Second);defer deadline.Stop()
 ticker:=time.NewTicker(20*time.Millisecond);defer ticker.Stop()
 waiting:=false
 for !waiting {
  select {
  case err:=<-done:t.Fatalf("completion returned before credential race: %v",err)
  case <-deadline.C:t.Fatal("completion did not reach governance mutex")
  case <-ticker.C:
   expectOK(t,"observe waiter",h.pool.QueryRow(h.ctx,`SELECT EXISTS(SELECT 1 FROM pg_stat_activity
    WHERE datname=current_database() AND wait_event='advisory' AND pid<>pg_backend_pid())`).Scan(&waiting))
  }
 }
 replacement,err:=security.HashPassword("Changed-Password-123!");expectOK(t,"replacement hash",err)
 _,err=tx.Exec(h.ctx,`UPDATE users SET password_hash=$2 WHERE id=$1`,id,replacement);expectOK(t,"replace credential",err)
 expectOK(t,"commit credential race",tx.Commit(h.ctx))
 select {
 case err:=<-done:expectErr(t,"stale proof",err,ErrBootstrapReauthenticationFailed)
 case <-time.After(5*time.Second):t.Fatal("completion did not finish")
 }
 if h.count(`SELECT COUNT(*) FROM root_super_admin`)!=0 || h.count(`SELECT COUNT(*) FROM platform_bootstrap_claims WHERE status='pending'`)!=1 {t.Fatal("stale proof established Root or consumed claim")}
}
