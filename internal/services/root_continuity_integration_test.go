// focodebase/fobackend/internal/services/root_continuity_integration_test.go
// Owner-run PostgreSQL tests using the existing explicitly disposable harness.
package services
import (
 "testing"
 "strings"
 "crypto/ed25519"
 "crypto/rand"
 "sync"
 "github.com/PiccoloMondoC/focodebase/fobackend/internal/data"
 "github.com/PiccoloMondoC/focodebase/fobackend/internal/governancerecovery"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
)
func continuityFixture(t *testing.T) (*governanceHarness,uuid.UUID,string,uuid.UUID,string) {
 t.Helper();h:=newGovernanceHarness(t)
 root,email:=h.account("continuity-root",true);claim:=issueTestClaim(t,h,email)
 _,err:=h.svc.CompleteRootBootstrapInternal(h.ctx,claim.Credential,testPassword);expectOK(t,"bootstrap",err)
 successor,successorEmail:=h.account("continuity-successor",true)
 expectOK(t,"Super Admin appointment",h.govern(root,successor,GovernanceAppointSuperAdmin,testPassword,successorEmail))
 return h,root,email,successor,successorEmail
}
func continuityRequest(t *testing.T,h *governanceHarness,actor uuid.UUID,req ContinuityRequest) (uuid.UUID,error) {
 t.Helper();view,err:=h.svc.ReadRootContinuityInternal(h.ctx,actor);expectOK(t,"read continuity",err)
 req.ExpectedDigest=view.Digest;req.CurrentPassword=testPassword
 return h.svc.ExecuteRootContinuityInternal(h.ctx,actor,req)
}
func appointTestCoRoot(t *testing.T,h *governanceHarness,root,successor uuid.UUID,email string) {
 t.Helper()
 _,err:=continuityRequest(t,h,root,ContinuityRequest{Operation:"designate",TargetID:successor,ConfirmEmail:email});expectOK(t,"designation",err)
 _,err=continuityRequest(t,h,root,ContinuityRequest{Operation:"appoint_co_root",TargetID:successor,ConfirmEmail:email});expectOK(t,"co-Root appointment",err)
}
func TestContinuityAppointmentProtectionsAndAuditRollback(t *testing.T) {
 h,root,_,co,email:=continuityFixture(t);appointTestCoRoot(t,h,root,co,email)
 other,otherEmail:=h.account("replacement",true);expectOK(t,"appointment",h.govern(root,other,GovernanceAppointSuperAdmin,testPassword,otherEmail))
 _,err:=continuityRequest(t,h,root,ContinuityRequest{Operation:"designate",TargetID:other,ConfirmEmail:otherEmail});expectOK(t,"replacement designation",err)
 _,err=continuityRequest(t,h,co,ContinuityRequest{Operation:"designate",TargetID:root,ConfirmEmail:email});expectErr(t,"co-Root cannot appoint",err,data.ErrContinuityForbidden)
 expectErr(t,"co-Root cannot demote incumbent",h.govern(co,root,GovernanceDemoteToAdmin,"",""),ErrGovernanceRootProtected)
 expectErr(t,"designation protects appointment",h.govern(root,other,GovernanceRevoke,"",""),data.ErrContinuityConflict)
 expectErr(t,"SQL co-Root demotion",h.exec(`UPDATE administrative_appointments SET level='admin' WHERE user_id=$1`,co),data.ErrContinuityConflict)
 expectOK(t,"hide audit action",h.exec(`UPDATE actions SET name='continuity_hidden' WHERE name='root_continuity'`))
 _,err=continuityRequest(t,h,root,ContinuityRequest{Operation:"replace_co_root",TargetID:other,ConfirmEmail:otherEmail,Disposition:"consumer"})
 expectErr(t,"audit failure rolls back replacement",err,data.ErrGovernanceAuditMetadataMissing)
 if h.count(`SELECT count(*) FROM root_co_holder WHERE user_id=$1`,co)!=1 || h.count(`SELECT count(*) FROM root_successor_designations WHERE user_id=$1`,other)!=1 || h.effectiveRole(co)!="super_admin" {t.Fatal("partial replacement escaped rollback")}
 expectOK(t,"restore audit",h.exec(`UPDATE actions SET name='root_continuity' WHERE name='continuity_hidden'`))
 _,err=continuityRequest(t,h,root,ContinuityRequest{Operation:"replace_co_root",TargetID:other,ConfirmEmail:otherEmail,Disposition:"admin"});expectOK(t,"replacement",err)
 if h.count(`SELECT count(*) FROM root_co_holder WHERE user_id=$1`,other)!=1 || h.effectiveRole(co)!="admin" || h.count(`SELECT count(*) FROM root_successor_designations WHERE user_id=$1`,co)!=0{t.Fatal("replacement disposition or standing incorrect")}
}
func TestContinuityVoluntaryTransferPreservesInstallationAndHonoraryAccount(t *testing.T) {
 h,root,rootEmail,co,email:=continuityFixture(t);appointTestCoRoot(t,h,root,co,email)
 _,err:=continuityRequest(t,h,root,ContinuityRequest{Operation:"propose",TargetID:co,ConfirmEmail:email});expectOK(t,"propose",err)
 var proposal uuid.UUID;expectOK(t,"pending proposal",h.pool.QueryRow(h.ctx,`SELECT id FROM root_succession_proposals WHERE status='pending'`).Scan(&proposal))
 _,err=continuityRequest(t,h,co,ContinuityRequest{Operation:"accept",ProposalID:proposal,ConfirmEmail:rootEmail});expectOK(t,"accept",err)
 if h.count(`SELECT count(*) FROM root_super_admin WHERE user_id=$1 AND succession_generation=2`,co)!=1 || h.count(`SELECT count(*) FROM root_honorary_standing WHERE user_id=$1`,root)!=1 || h.effectiveRole(root)!="consumer" || h.count(`SELECT count(*) FROM root_co_holder`)!=0 {t.Fatal("transfer result incorrect")}
 if h.count(`SELECT count(*) FROM platform_installation WHERE original_bootstrap_user_id=$1 AND bootstrap_succession_generation=1`,root)!=1{t.Fatal("installation provenance changed")}
 _,err=continuityRequest(t,h,co,ContinuityRequest{Operation:"accept",ProposalID:proposal,ConfirmEmail:rootEmail});expectErr(t,"replay",err,data.ErrContinuityForbidden)
 expectErr(t,"former incumbent has no console authority",func()error{_,e:=h.svc.ReadRootContinuityInternal(h.ctx,root);return e}(),data.ErrContinuityForbidden)
 expectOK(t,"repeat schema initialization",(&data.DBConnectionParamsModel{Logger:h.svc.Logger}).CreateTables(h.pool))
 expectOK(t,"repeat seed",(&data.DBConnectionParamsModel{Logger:h.svc.Logger}).SeedAllData(h.pool,data.OAuthClientSeedSecrets{}))
 if h.count(`SELECT count(*) FROM root_super_admin WHERE user_id=$1 AND succession_generation=2`,co)!=1 || h.count(`SELECT count(*) FROM platform_installation`)!=1{t.Fatal("restart reshaped authority")}
}
func TestContinuityConcurrentAcceptAndCancel(t *testing.T) {
 h,root,rootEmail,co,email:=continuityFixture(t);appointTestCoRoot(t,h,root,co,email)
 _,err:=continuityRequest(t,h,root,ContinuityRequest{Operation:"propose",TargetID:co,ConfirmEmail:email});expectOK(t,"propose",err)
 var proposal uuid.UUID;expectOK(t,"proposal",h.pool.QueryRow(h.ctx,`SELECT id FROM root_succession_proposals WHERE status='pending'`).Scan(&proposal))
 view,err:=h.svc.ReadRootContinuityInternal(h.ctx,root);expectOK(t,"snapshot",err)
 requests:=[]ContinuityRequest{{Operation:"cancel",ProposalID:proposal},{Operation:"accept",ProposalID:proposal,ConfirmEmail:rootEmail}}
 actors:=[]uuid.UUID{root,co};results:=make([]error,2);var wg sync.WaitGroup
 for i:=range requests {wg.Add(1);go func(i int){defer wg.Done();r:=requests[i];r.CurrentPassword=testPassword;r.ExpectedDigest=view.Digest;_,results[i]=h.svc.ExecuteRootContinuityInternal(h.ctx,actors[i],r)}(i)};wg.Wait()
 wins:=0;for _,e:=range results{if e==nil{wins++}};if wins!=1{t.Fatalf("expected one winner, got %d: %v",wins,results)}
 if h.count(`SELECT count(*) FROM root_succession_events`)>2 || h.count(`SELECT count(*) FROM root_succession_proposals WHERE status='pending'`)!=0{t.Fatal("race left partial authority")}
}
func TestProtectedRecoveryOutsideTeamAndConsulting(t *testing.T) {
 h,root,_,co,email:=continuityFixture(t);appointTestCoRoot(t,h,root,co,email)
 outsider,outsiderEmail:=h.account("Board-selected-outsider",true)
 request:=governancerecovery.Request{Operation:"recovery",TargetID:outsider,ConfirmEmail:outsiderEmail,BoardReference:"BOARD-TEST-1",CIOReference:"CIO-TEST-1",Executor:"test-engineering",Reason:"incumbent_incapacity",PredecessorDisposition:"consumer",DepartureWithoutConflict:true}
 plan,err:=governancerecovery.Prepare(h.ctx,h.pool,h.svc.Logger,request);expectOK(t,"recovery plan",err)
 changed:=request;changed.BoardReference="BOARD-DIFFERENT"
 _,err=executeTestRecovery(t,h,changed,plan.Digest,uuid.New());if err==nil{t.Fatal("changed authorization accepted old digest")}
 ceremony:=uuid.New();repeated,err:=executeTestRecovery(t,h,request,plan.Digest,ceremony);expectOK(t,"recovery",err);if repeated{t.Fatal("first execution marked replay")}
 repeated,err=executeTestRecovery(t,h,request,plan.Digest,ceremony);expectOK(t,"idempotent repeat",err);if !repeated{t.Fatal("repeat did not return recorded result")}
 if h.count(`SELECT count(*) FROM root_succession_events`)!=2 || h.effectiveRole(root)!="consumer" || h.effectiveRole(outsider)!="super_admin" || h.effectiveRole(co)!="super_admin"{t.Fatal("Board recovery disposition incorrect")}
 var oldEmail string;expectOK(t,"honorary email",h.pool.QueryRow(h.ctx,`SELECT email::text FROM users WHERE id=$1`,root).Scan(&oldEmail))
 consulting:=governancerecovery.Request{Operation:"consultant_grant",TargetID:root,ConfirmEmail:oldEmail,CIOReference:"CIO-CONSULT-1",Executor:"test-engineering",IssueReference:"ISSUE-1",Capabilities:[]string{"read_continuity"},DurationHours:1}
 p,err:=governancerecovery.Prepare(h.ctx,h.pool,h.svc.Logger,consulting);expectOK(t,"consulting plan",err)
 grant:=uuid.New();_,err=executeTestRecovery(t,h,consulting,p.Digest,grant);expectOK(t,"consulting grant",err)
 _,err=h.svc.ReadConsultingInternal(h.ctx,root,"read_continuity",uuid.Nil);expectOK(t,"allowed consulting read",err)
 _,err=h.svc.ReadConsultingInternal(h.ctx,root,"read_administrators",uuid.Nil);expectErr(t,"scope enforced",err,data.ErrContinuityForbidden)
 revoke:=governancerecovery.Request{Operation:"consultant_revoke",TargetID:root,ConfirmEmail:oldEmail,CIOReference:"CIO-CONSULT-END",Executor:"test-engineering",GrantID:grant}
 p,err=governancerecovery.Prepare(h.ctx,h.pool,h.svc.Logger,revoke);expectOK(t,"revoke plan",err)
 _,err=executeTestRecovery(t,h,revoke,p.Digest,uuid.New());expectOK(t,"revoke grant",err)
 _,err=h.svc.ReadConsultingInternal(h.ctx,root,"read_continuity",uuid.Nil);expectErr(t,"revocation enforced",err,data.ErrContinuityForbidden)
 if h.effectiveRole(root)!="consumer"{t.Fatal("consulting conferred administrative role")}
}

func executeTestRecovery(t *testing.T,h *governanceHarness,r governancerecovery.Request,digest string,id uuid.UUID)(bool,error){
 t.Helper()
 boardPub,boardPrivate,err:=ed25519.GenerateKey(rand.Reader);expectOK(t,"Board key fixture",err)
 cioPub,cioPrivate,err:=ed25519.GenerateKey(rand.Reader);expectOK(t,"CIO key fixture",err)
 message,err:=governancerecovery.ApprovalMessage(r,digest,id);expectOK(t,"approval message",err)
 approval,err:=governancerecovery.VerifyApproval(r,digest,id,boardPub,cioPub,ed25519.Sign(boardPrivate,message),ed25519.Sign(cioPrivate,message));expectOK(t,"signed approval",err)
 return governancerecovery.Execute(h.ctx,h.pool,h.svc.Logger,r,digest,id,approval)
}

func TestSuccessorCapacityOrderingAndDatabaseEvidence(t *testing.T) {
 h,root,_,first,firstEmail:=continuityFixture(t)
 ids:=[]uuid.UUID{first};emails:=[]string{firstEmail}
 for i:=0;i<3;i++ {id,email:=h.account("successor-capacity",true);expectOK(t,"appointment",h.govern(root,id,GovernanceAppointSuperAdmin,testPassword,email));ids=append(ids,id);emails=append(emails,email)}
 for i:=0;i<3;i++ {_,err:=continuityRequest(t,h,root,ContinuityRequest{Operation:"designate",TargetID:ids[i],ConfirmEmail:emails[i]});expectOK(t,"designation",err)}
 _,err:=continuityRequest(t,h,root,ContinuityRequest{Operation:"designate",TargetID:ids[3],ConfirmEmail:emails[3]});expectErr(t,"fourth designation refused",err,data.ErrContinuityConflict)
 _,err=continuityRequest(t,h,root,ContinuityRequest{Operation:"reorder",Order:[]uuid.UUID{ids[2],ids[0],ids[1]}});expectOK(t,"permutation",err)
 if h.count(`SELECT count(*) FROM root_successor_designations WHERE user_id=$1 AND rank=1`,ids[2])!=1{t.Fatal("rank order did not change")}
 _,err=continuityRequest(t,h,root,ContinuityRequest{Operation:"reorder",Order:[]uuid.UUID{ids[0],ids[0],ids[1]}});expectErr(t,"duplicate order refused",err,ErrGovernanceInputInvalid)
 for _,sql:=range []string{
  `DELETE FROM root_successor_designations`,
  `TRUNCATE root_successor_designations`,
  `INSERT INTO root_co_holder(user_id,appointed_by) VALUES($1,$2)`,
  `INSERT INTO root_succession_events(succession_generation,predecessor_user_id,successor_user_id,mechanism) VALUES(2,$2,$1,'voluntary_transfer')`,
 } {
  args:=[]any{};if strings.Contains(sql,"$1"){args=[]any{ids[0],root}}
  if h.exec(sql,args...)==nil{t.Fatalf("unmarked SQL mutation succeeded: %s",sql)}
 }
 _,err=continuityRequest(t,h,root,ContinuityRequest{Operation:"undesignate",TargetID:ids[0]});expectOK(t,"remove designation",err)
 if h.count(`SELECT count(*) FROM root_successor_designations WHERE rank BETWEEN 1 AND 2`)!=2 {t.Fatal("rank gap after removal")}
}

func TestVoluntaryAcceptanceAuditFailureRollsBackIncumbency(t *testing.T) {
 h,root,rootEmail,co,email:=continuityFixture(t);appointTestCoRoot(t,h,root,co,email)
 _,err:=continuityRequest(t,h,root,ContinuityRequest{Operation:"propose",TargetID:co,ConfirmEmail:email});expectOK(t,"proposal",err)
 var proposal uuid.UUID;expectOK(t,"proposal ID",h.pool.QueryRow(h.ctx,`SELECT id FROM root_succession_proposals WHERE status='pending'`).Scan(&proposal))
 expectOK(t,"hide audit metadata",h.exec(`UPDATE actions SET name='continuity_audit_hidden' WHERE name='root_continuity'`))
 _,err=continuityRequest(t,h,co,ContinuityRequest{Operation:"accept",ProposalID:proposal,ConfirmEmail:rootEmail});expectErr(t,"accept audit failure",err,data.ErrGovernanceAuditMetadataMissing)
 if h.count(`SELECT count(*) FROM root_super_admin WHERE user_id=$1 AND succession_generation=1`,root)!=1 || h.count(`SELECT count(*) FROM root_succession_events`)!=1 || h.count(`SELECT count(*) FROM root_co_holder WHERE user_id=$1`,co)!=1 || h.count(`SELECT count(*) FROM root_succession_proposals WHERE status='pending'`)!=1 || h.count(`SELECT count(*) FROM root_honorary_standing`)!=0 || h.effectiveRole(root)!="super_admin"{t.Fatal("partial succession committed")}
 expectOK(t,"restore metadata",h.exec(`UPDATE actions SET name='root_continuity' WHERE name='continuity_audit_hidden'`))
 _,err=continuityRequest(t,h,co,ContinuityRequest{Operation:"accept",ProposalID:proposal,ConfirmEmail:rootEmail});expectOK(t,"accept after rollback",err)
}

func TestExpiredProposalCannotTransferIncumbency(t *testing.T) {
 h,root,rootEmail,co,email:=continuityFixture(t);appointTestCoRoot(t,h,root,co,email)
 proposal:=uuid.New()
 err:=h.svc.withGovernanceTx(h.ctx,func(tx pgx.Tx)error {
  gov:=h.svc.governance();if _,err:=gov.LockGovernanceSubjectsTx(h.ctx,tx,root,co);err!=nil{return err}
  before,err:=data.ReadContinuityStateTx(h.ctx,tx);if err!=nil{return err}
  if err:=data.SetContinuityOperationTx(h.ctx,tx,"propose");err!=nil{return err}
  if _,err:=tx.Exec(h.ctx,`INSERT INTO root_succession_proposals(id,incumbent_id,successor_id,generation,status,proposed_at,expires_at) VALUES($1,$2,$3,1,'pending',statement_timestamp()-INTERVAL '80 hours',statement_timestamp()-INTERVAL '8 hours')`,proposal,root,co);err!=nil{return err}
  return data.RecordContinuityEventTx(h.ctx,tx,gov,uuid.New(),"propose",&root,&co,before,map[string]any{"proposal_id":proposal})
 });expectOK(t,"expired proposal fixture",err)
 _,err=continuityRequest(t,h,co,ContinuityRequest{Operation:"accept",ProposalID:proposal,ConfirmEmail:rootEmail});expectErr(t,"expired acceptance",err,data.ErrContinuityConflict)
 if h.count(`SELECT count(*) FROM root_succession_events`)!=1{t.Fatal("expired proposal advanced ledger")}
 _,err=continuityRequest(t,h,root,ContinuityRequest{Operation:"propose",TargetID:co,ConfirmEmail:email});expectOK(t,"replacement proposal",err)
 if h.count(`SELECT count(*) FROM root_succession_proposals WHERE status='expired'`)!=1 || h.count(`SELECT count(*) FROM root_succession_proposals WHERE status='pending'`)!=1{t.Fatal("expired proposal not resolved atomically")}
}
