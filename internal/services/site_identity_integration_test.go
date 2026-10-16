// internal/services/site_identity_integration_test.go
// Opt-in full service tests. Use a disposable database initialized by canonical
// startup with an active incumbent. Saves and restoration are both audited.
package services

import (
 "context"
 "errors"
 "os"
 "sync"
 "testing"
 "time"

 "github.com/PiccoloMondoC/focodebase/fobackend/internal/data"
 "github.com/PiccoloMondoC/focodebase/fobackend/internal/logging"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5/pgxpool"
)

type siteIdentityUnusedTokenIssuer struct{}
func (siteIdentityUnusedTokenIssuer) GenerateTokensPair(context.Context,uuid.UUID)(string,string,error) {
 return "","",errors.New("site identity must never issue session credentials")
}

func TestSiteIdentityServiceConcurrentCommitAndAccountability(t *testing.T) {
 dsn:=os.Getenv("DROP4_TEST_DSN")
 if dsn=="" {t.Skip("DROP4_TEST_DSN not supplied; full service integration not executed")}
 ctx,cancel:=context.WithTimeout(context.Background(),30*time.Second)
 defer cancel()
 pool,err:=pgxpool.New(ctx,dsn)
 if err!=nil {t.Fatal(err)}
 defer pool.Close()
 logger,err:=logging.InitLogging("drop4-test")
 if err!=nil {t.Fatal(err)}
 defer logger.Sync()
 models:=data.New(pool,logger)
 svc,err:=NewService(logger,&models,&Config{DBTimeout:5*time.Second,ActivationTokenTTL:time.Hour,PasswordResetTokenTTL:time.Hour},nil,siteIdentityUnusedTokenIssuer{})
 if err!=nil {t.Fatal(err)}
 var actor uuid.UUID
 if err=pool.QueryRow(ctx,`SELECT user_id FROM root_super_admin WHERE singleton`).Scan(&actor);err!=nil {t.Fatal("initialize an active incumbent in the disposable test database before integration checks")}
 before,err:=svc.SiteIdentityAdministrationInternal(ctx,actor)
 if err!=nil {t.Fatal(err)}
 defer func(){
  cleanup,cancelCleanup:=context.WithTimeout(context.Background(),10*time.Second)
  defer cancelCleanup()
  current,readErr:=svc.SiteIdentityAdministrationInternal(cleanup,actor)
  if readErr!=nil {t.Errorf("test restoration read failed: %v",readErr);return}
  if _,restoreErr:=svc.ReplaceSiteIdentityInternal(cleanup,SiteIdentityChange{ActorID:actor,ExpectedVersion:current.Version,Presentation:before.SitePresentation,Reason:"Restore after Drop 4 integration verification"});restoreErr!=nil {t.Errorf("test restoration failed: %v",restoreErr)}
 }()

 // A no-op checks authority but advances neither revision nor history.
 unchanged,err:=svc.ReplaceSiteIdentityInternal(ctx,SiteIdentityChange{ActorID:actor,ExpectedVersion:before.Version,Presentation:before.SitePresentation})
 if err!=nil || unchanged.Changed || unchanged.Configuration.Version!=before.Version {t.Fatalf("invalid no-op result: %v",err)}

 // Both administrators observed the same revision. Exactly one whole save
 // may commit; the loser cannot persist either identity or terminology.
 var wait sync.WaitGroup
 start:=make(chan struct{})
 results:=make(chan error,2)
 for _,name:=range []string{"Independent Collection A","Independent Collection B"} {
  wait.Add(1)
  go func(name string){
   defer wait.Done()
   candidate:=data.SitePresentation{Identity:before.Identity,Terms:make(map[string]string,len(before.Terms))}
   for key,value:=range before.Terms {candidate.Terms[key]=value}
   candidate.Identity.SiteName=name
   candidate.Terms["future_offerings.area"]=name
   <-start
   _,changeErr:=svc.ReplaceSiteIdentityInternal(ctx,SiteIdentityChange{ActorID:actor,ExpectedVersion:before.Version,Presentation:candidate,Reason:"Concurrent Drop 4 verification"})
   results<-changeErr
  }(name)
 }
 close(start)
 wait.Wait()
 close(results)
 successes,conflicts:=0,0
 for result:=range results {
  if result==nil {successes++} else if errors.Is(result,data.ErrSiteIdentityVersionConflict) {conflicts++} else {t.Fatalf("unexpected mutation error: %v",result)}
 }
 if successes!=1 || conflicts!=1 {t.Fatalf("successes=%d conflicts=%d",successes,conflicts)}
 current,err:=svc.SiteIdentityAdministrationInternal(ctx,actor)
 if err!=nil {t.Fatal(err)}
 if current.Version!=before.Version+1 || current.Identity.SiteName!=current.Terms["future_offerings.area"] {t.Fatal("partial or extra configuration commit")}
 history,err:=svc.SiteIdentityHistoryInternal(ctx,actor,1)
 if err!=nil {t.Fatal(err)}
 if len(history)!=1 || history[0].Version!=current.Version || history[0].ChangedBy==nil || *history[0].ChangedBy!=actor {t.Fatal("commit history is missing accountability")}

 // A non-administrator cannot read or mutate privileged configuration through
 // direct service calls, even if route middleware were bypassed.
 outsider:=uuid.New()
 if _,err=svc.SiteIdentityAdministrationInternal(ctx,outsider);err==nil {t.Fatal("unauthorized service read succeeded")}
 if _,err=svc.ReplaceSiteIdentityInternal(ctx,SiteIdentityChange{ActorID:outsider,ExpectedVersion:current.Version,Presentation:current.SitePresentation});err==nil {t.Fatal("unauthorized service mutation succeeded")}
}
