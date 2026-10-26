//go:build integration

// Package data tests profile transactions, account locks, handle integrity, and moderation against PostgreSQL.
// focodebase/fobackend/internal/data/profile_integration_test.go

package data

import (
 "context"
 "errors"
 "os"
 "testing"
 "time"
 "github.com/PiccoloMondoC/focodebase/fobackend/internal/logging"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "github.com/jackc/pgx/v5/pgxpool"
)

// Owner-run only. Requires an explicitly designated disposable database.
// The canonical complete schema is installed; no mocks or substitute schemas.
func TestCEProfileIntegration(t *testing.T) {
 dsn:=os.Getenv("CE_PROFILE_TEST_DATABASE_URL")
 if dsn==""||os.Getenv("CE_PROFILE_TEST_DATABASE_DISPOSABLE")!="1"{t.Skip("explicit disposable profile test database not configured")}
 ctx,cancel:=context.WithTimeout(context.Background(),45*time.Second);defer cancel()
 logger,err:=logging.InitLogging("profile-integration");if err!=nil{t.Fatal(err)};defer logger.Sync()
 cfg,err:=pgxpool.ParseConfig(dsn);if err!=nil{t.Fatal(err)};cfg.MaxConns=6
 pool,err:=pgxpool.NewWithConfig(ctx,cfg);if err!=nil{t.Fatal(err)};defer pool.Close()
 if err=(&DBConnectionParamsModel{Logger:logger}).CreateTables(pool);err!=nil{t.Fatal(err)}
 model:=&UserProfileModel{DB:pool,Logger:logger}
 user:=func(t *testing.T)uuid.UUID{
  t.Helper();id:=uuid.New();_,err:=pool.Exec(ctx,`INSERT INTO users(id,email,is_active) VALUES($1,$2,true)`,id,"profile-"+id.String()+"@example.test");if err!=nil{t.Fatal(err)};return id
 }
 begin:=func(t *testing.T)pgx.Tx{t.Helper();tx,err:=pool.Begin(ctx);if err!=nil{t.Fatal(err)};t.Cleanup(func(){_ = tx.Rollback(context.Background())});return tx}
 seed:=func(id uuid.UUID)UserProfile{return UserProfile{UserID:id,FirstName:ceProfileString("Ada"),LastName:ceProfileString("Lovelace"),PreferredContact:"email"}}
 create:=func(t *testing.T,id uuid.UUID)*UserProfile{
  t.Helper();tx:=begin(t);if err:=model.LockProfileAccountTx(ctx,tx,id);err!=nil{t.Fatal(err)}
  p:=seed(id);if err:=model.CreateProfileTx(ctx,tx,&p);err!=nil{t.Fatal(err)}
  stored,err:=model.ReadProfileTx(ctx,tx,id,false);if err!=nil{t.Fatal(err)};if err=tx.Commit(ctx);err!=nil{t.Fatal(err)};return stored
 }
 t.Run("audit failure rolls back profile and handle",func(t *testing.T){
  id:=user(t);tx:=begin(t);if err:=model.LockProfileAccountTx(ctx,tx,id);err!=nil{t.Fatal(err)}
  p:=seed(id);p.FullLegalName=ceProfileString("Private Legal Name");handle:="p-"+id.String()[:20];p.UserHandle=&handle
  if err:=model.CreateProfileTx(ctx,tx,&p);err!=nil{t.Fatal(err)}
  missingActor:=uuid.New();if err:=model.AuditProfileTx(ctx,tx,&missingActor,id.String(),"create_user_profile",nil);err==nil{t.Fatal("invalid audit actor accepted")}
  _ = tx.Rollback(ctx)
  var count int;if err:=pool.QueryRow(ctx,`SELECT (SELECT count(*) FROM user_profiles WHERE user_id=$1)+(SELECT count(*) FROM global_handles WHERE entity_id=$1)+(SELECT count(*) FROM user_profile_private_identities WHERE user_id=$1)`,id).Scan(&count);err!=nil{t.Fatal(err)};if count!=0{t.Fatal("partial write escaped rollback")}
 })
 t.Run("private legal identity is additive and legacy names are retained",func(t *testing.T){
  id:=user(t);legacy:=create(t,id)
  tx:=begin(t);if err:=model.LockProfileAccountTx(ctx,tx,id);err!=nil{t.Fatal(err)}
  before,err:=model.ReadProfileTx(ctx,tx,id,true);if err!=nil{t.Fatal(err)}
  if before.FullLegalName!=nil{t.Fatal("legacy names were silently converted")}
  next,_,err:=ApplyProfileChanges(*before,ProfileChanges{"full_legal_name":ceProfileString("Dr. Ada Augusta Lovelace III"),"user_handle":ceProfileString("n-"+id.String()[:20])},false);if err!=nil{t.Fatal(err)}
  if err=model.SaveProfileTx(ctx,tx,before,&next);err!=nil{t.Fatal(err)}
  stored,err:=model.ReadProfileTx(ctx,tx,id,false);if err!=nil{t.Fatal(err)}
  if err=model.LoadPrivateProfileIdentityTx(ctx,tx,stored);err!=nil{t.Fatal(err)}
  if stored.FullLegalName==nil||*stored.FullLegalName!="Dr. Ada Augusta Lovelace III"{t.Fatal("full legal name was split or lost")}
  if *stored.FirstName!=*legacy.FirstName||*stored.LastName!=*legacy.LastName{t.Fatal("legacy identity was overwritten")}
 })
 t.Run("transaction start time cannot reverse profile version",func(t *testing.T){
  id:=user(t);original:=create(t,id)
  early:=begin(t) // starts before the first writer's transaction
  if _,err:=early.Exec(ctx,`SELECT NOW()`);err!=nil{t.Fatal(err)}
  first:=begin(t);if err:=model.LockProfileAccountTx(ctx,first,id);err!=nil{t.Fatal(err)}
  before,err:=model.ReadProfileTx(ctx,first,id,true);if err!=nil{t.Fatal(err)}
  next:=*before;next.Bio=ceProfileString("first")
  if err=model.SaveProfileTx(ctx,first,before,&next);err!=nil{t.Fatal(err)}
  stored,err:=model.ReadProfileTx(ctx,first,id,false);if err!=nil{t.Fatal(err)}
  if !stored.UpdatedAt.After(original.UpdatedAt){t.Fatal("first version failed to advance")}
  if err=first.Commit(ctx);err!=nil{t.Fatal(err)}
  if err=model.LockProfileAccountTx(ctx,early,id);err!=nil{t.Fatal(err)}
  before,err=model.ReadProfileTx(ctx,early,id,true);if err!=nil{t.Fatal(err)}
  next=*before;next.Bio=ceProfileString("second");if err=model.SaveProfileTx(ctx,early,before,&next);err!=nil{t.Fatal(err)}
  latest,err:=model.ReadProfileTx(ctx,early,id,false);if err!=nil{t.Fatal(err)}
  if !latest.UpdatedAt.After(stored.UpdatedAt){t.Fatal("waiting/older transaction regressed version")}
 })
 t.Run("account closure waits for account profile lock",func(t *testing.T){
  id:=user(t);holder:=begin(t);if err:=model.LockProfileAccountTx(ctx,holder,id);err!=nil{t.Fatal(err)}
  done:=make(chan error,1);go func(){_,err:=pool.Exec(ctx,`UPDATE users SET is_active=false WHERE id=$1`,id);done<-err}()
  select{case err:=<-done:t.Fatalf("closure passed active share lock: %v",err);case <-time.After(100*time.Millisecond):}
  _ = holder.Rollback(ctx)
  select{case err:=<-done:if err!=nil{t.Fatal(err)};case <-ctx.Done():t.Fatal(ctx.Err())}
  tx:=begin(t);if err:=model.LockProfileAccountTx(ctx,tx,id);!errors.Is(err,ErrProfileAccount){t.Fatal("inactive account allowed to create/update")}
 })
 t.Run("two versioned writers have exactly one winner",func(t *testing.T){
  id:=user(t);original:=create(t,id)
  start:=make(chan struct{});results:=make(chan error,2)
  writer:=func(value string){
   <-start
   tx,err:=pool.Begin(ctx);if err!=nil{results<-err;return};defer tx.Rollback(context.Background())
   if err=model.LockProfileAccountTx(ctx,tx,id);err!=nil{results<-err;return}
   before,err:=model.ReadProfileTx(ctx,tx,id,true);if err!=nil{results<-err;return}
   if !before.UpdatedAt.Equal(original.UpdatedAt){results<-ErrProfileConflict;return}
   next:=*before;next.Bio=&value
   if err=model.SaveProfileTx(ctx,tx,before,&next);err!=nil{results<-err;return}
   results<-tx.Commit(ctx)
  }
  go writer("one");go writer("two");close(start)
  successes,conflicts:=0,0
  for i:=0;i<2;i++{select{case err:=<-results:if err==nil{successes++}else if errors.Is(err,ErrProfileConflict){conflicts++}else{t.Fatal(err)};case <-ctx.Done():t.Fatal(ctx.Err())}}
  if successes!=1||conflicts!=1{t.Fatalf("got %d successes and %d conflicts",successes,conflicts)}
 })
 t.Run("namespace also rejects merchant and retained handle collisions",func(t *testing.T){
  id:=user(t);p:=create(t,id);tx:=begin(t);if err:=model.LockProfileAccountTx(ctx,tx,id);err!=nil{t.Fatal(err)}
  next:=*p;handle:="p-"+id.String()[:20];next.UserHandle=&handle
  if err:=model.SaveProfileTx(ctx,tx,p,&next);err!=nil{t.Fatal(err)};if err:=tx.Commit(ctx);err!=nil{t.Fatal(err)}
  _,err:=pool.Exec(ctx,`INSERT INTO global_handles(handle,entity_type,entity_id) VALUES($1,'merchant',$2)`,handle,uuid.New())
  if !errors.Is(ProfileDatabaseError(err),ErrUserHandleTaken){t.Fatal("merchant collision was not rejected")}
  if err=model.SoftDelete(ctx,id);err!=nil{t.Fatal(err)}
  status,err:=model.ProfileHandleStatus(ctx,uuid.New(),handle);if err!=nil||status!="unavailable"{t.Fatal("retained profile handle became available")}
 })
 t.Run("moderation retains actor and ordinary edits preserve notes",func(t *testing.T){
  actor,id:=user(t),user(t);create(t,id);tx:=begin(t)
  if err:=model.LockProfileAccountTx(ctx,tx,id);err!=nil{t.Fatal(err)}
  if err:=model.ModerateProfileTx(ctx,tx,&actor,id,true,"review note");err!=nil{t.Fatal(err)}
  p,err:=model.ReadProfileTx(ctx,tx,id,true);if err!=nil{t.Fatal(err)}
  next,_,err:=ApplyProfileChanges(*p,ProfileChanges{"bio":ceProfileString("updated")},false);if err!=nil{t.Fatal(err)}
  if err=model.SaveProfileTx(ctx,tx,p,&next);err!=nil{t.Fatal(err)}
  saved,err:=model.ReadProfileTx(ctx,tx,id,false);if err!=nil{t.Fatal(err)}
  if !saved.IsFlagged||saved.ModerationNotes==nil||*saved.ModerationNotes!="review note"{t.Fatal("edit overwrote moderation")}
  var recorded uuid.UUID;if err=tx.QueryRow(ctx,`SELECT performed_by FROM user_profile_moderation_logs WHERE user_id=$1`,id).Scan(&recorded);err!=nil||recorded!=actor{t.Fatal("moderation actor lost")}
 })
}
