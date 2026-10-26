//go:build integration

// Package services tests photo ownership, atomic replacement, rollback and durable cleanup using an explicit disposable database.
// focodebase/fobackend/internal/services/profile_media_integration_test.go
package services

import (
 "bytes"
 "context"
 "crypto/sha256"
 "encoding/hex"
 "errors"
 "image"
 "image/color"
 "image/png"
 "io"
 "os"
 "sync"
 "testing"
 "time"
 "github.com/PiccoloMondoC/focodebase/fobackend/internal/data"
 "github.com/PiccoloMondoC/focodebase/fobackend/internal/logging"
 "github.com/PiccoloMondoC/focodebase/fobackend/internal/mediastore"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5/pgxpool"
)
type mediaTestIssuer struct{}
func(mediaTestIssuer)GenerateTokensPair(context.Context,uuid.UUID)(string,string,error){return "","",nil}
type mediaTestStore struct{
 mu sync.Mutex
 objects map[string][]byte
 failPut,failDelete bool
 afterPut func()
}
func(m *mediaTestStore)Location()(string,string){return "filesystem","ce-test-memory"}
func(m *mediaTestStore)Close()error{return nil}
func(m *mediaTestStore)Put(_ context.Context,key,_ string,source io.Reader)error{
 raw,err:=io.ReadAll(source);if err!=nil{return err}
 m.mu.Lock();m.objects[key]=raw;fail:=m.failPut;callback:=m.afterPut;m.mu.Unlock()
 if callback!=nil{callback()};if fail{return mediastore.ErrUnavailable};return nil
}
func(m *mediaTestStore)Open(_ context.Context,key string)(io.ReadCloser,error){
 m.mu.Lock();defer m.mu.Unlock();raw,ok:=m.objects[key];if !ok{return nil,mediastore.ErrNotFound};return io.NopCloser(bytes.NewReader(raw)),nil
}
func(m *mediaTestStore)Delete(_ context.Context,key string)error{
 m.mu.Lock();defer m.mu.Unlock();if m.failDelete{return mediastore.ErrUnavailable};delete(m.objects,key);return nil
}
func TestCEProfileMediaIntegration(t *testing.T){
 dsn:=os.Getenv("CE_PROFILE_TEST_DATABASE_URL")
 if dsn==""||os.Getenv("CE_PROFILE_TEST_DATABASE_DISPOSABLE")!="1"{t.Skip("explicit disposable profile test database not configured")}
 ctx,cancel:=context.WithTimeout(context.Background(),90*time.Second);t.Cleanup(cancel)
 logger,err:=logging.InitLogging("profile-media-integration");if err!=nil{t.Fatal(err)};t.Cleanup(func(){_ = logger.Sync()})
 pool,err:=pgxpool.New(ctx,dsn);if err!=nil{t.Fatal(err)};t.Cleanup(pool.Close)
 if err=(&data.DBConnectionParamsModel{Logger:logger}).CreateTables(pool);err!=nil{t.Fatal(err)}
 models:=data.New(pool,logger)
 svc,err:=NewService(logger,&models,&Config{DBTimeout:10*time.Second,ActivationTokenTTL:time.Hour,PasswordResetTokenTTL:time.Hour},make(chan struct{}),mediaTestIssuer{})
 if err!=nil{t.Fatal(err)}
 store:=&mediaTestStore{objects:map[string][]byte{}}
 // This disposable test uses its own store directly: production initialization
 // separately refuses legacy binaries and recorded storage-location switches.
 svc.media=store;svc.photoSlots=make(chan struct{},2)
 owner:=uuid.New();other:=uuid.New()
 for _,id:=range []uuid.UUID{owner,other}{
  if _,err=pool.Exec(ctx,`INSERT INTO users(id,email,is_active) VALUES($1,$2,true)`,id,id.String()+"@example.test");err!=nil{t.Fatal(err)}
 }
 t.Cleanup(func(){cleanup,finish:=context.WithTimeout(context.Background(),3*time.Second);defer finish();_,_ = pool.Exec(cleanup,`DELETE FROM media_objects WHERE owner_user_id IN ($1,$2)`,owner,other)})
 text:=func(v string)*string{return &v}
 w,err:=svc.SaveOwnProfileInternal(ctx,owner,data.ProfileChanges{"full_legal_name":text("Private Name"),"user_handle":text("p-"+owner.String()[:20])},nil,true)
 if err!=nil{t.Fatal(err)}
 photo:=func(c color.Color)[]byte{img:=image.NewRGBA(image.Rect(0,0,2,2));img.Set(0,0,c);var out bytes.Buffer;if err:=png.Encode(&out,img);err!=nil{t.Fatal(err)};return out.Bytes()}
 first:=photo(color.RGBA{R:255,A:255});second:=photo(color.RGBA{B:255,A:255})
 w,err=svc.SaveOwnProfilePhotoInternal(ctx,owner,first,&w.Profile.UpdatedAt,false);if err!=nil{t.Fatal(err)}
 original,err:=svc.GetOwnProfilePhotoInternal(ctx,owner);if err!=nil{t.Fatal(err)}
 if !w.Profile.HasPhoto{t.Fatal("photo missing from owner assessment")}
 if _,err=svc.GetOwnProfilePhotoInternal(ctx,other);!errors.Is(err,data.ErrUserProfileNotFound){t.Fatal("other account could read photo")}
 // Put can leave bytes yet return an error. The old reference must survive,
 // and the failed object's durable record must remain eligible for cleanup.
 store.failPut=true
 if _,err=svc.SaveOwnProfilePhotoInternal(ctx,owner,second,&w.Profile.UpdatedAt,false);err==nil{t.Fatal("failed storage write accepted")}
 store.failPut=false
 actual,err:=svc.GetOwnProfilePhotoInternal(ctx,owner);if err!=nil||!bytes.Equal(actual,original){t.Fatal("failed write replaced current photo")}
 // Closure after upload but before activation must fail live account recheck.
 store.afterPut=func(){if _,e:=pool.Exec(ctx,`UPDATE users SET is_active=false WHERE id=$1`,owner);e!=nil{t.Error(e)}}
 if _,err=svc.SaveOwnProfilePhotoInternal(ctx,owner,second,&w.Profile.UpdatedAt,false);!errors.Is(err,data.ErrProfileAccount){t.Fatal("closure lost race to activation")}
 store.afterPut=nil
 if _,err=pool.Exec(ctx,`UPDATE users SET is_active=true WHERE id=$1`,owner);err!=nil{t.Fatal(err)}
 // An audit failure must roll back activation, retirement and profile version.
 suffix:=owner.String();suffix=string(bytes.ReplaceAll([]byte(suffix),[]byte("-"),nil))
 functionName:="ce_photo_audit_"+suffix;triggerName:="ce_photo_trigger_"+suffix
 ddl:=`CREATE FUNCTION `+functionName+`() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN
   IF NEW.user_id='`+owner.String()+`'::uuid AND NEW.context->'changed_fields' ? 'profile_photo' THEN RAISE EXCEPTION 'test audit failure'; END IF;
   RETURN NEW; END $$;
   CREATE TRIGGER `+triggerName+` BEFORE INSERT ON audit_logs FOR EACH ROW EXECUTE FUNCTION `+functionName+`();`
 if _,err=pool.Exec(ctx,ddl);err!=nil{t.Fatal(err)}
 dropped:=false
 drop:=func(){if dropped{return};cleanup,finish:=context.WithTimeout(context.Background(),3*time.Second);defer finish();if _,e:=pool.Exec(cleanup,`DROP FUNCTION IF EXISTS `+functionName+`() CASCADE`);e!=nil{t.Error(e)}else{dropped=true}}
 t.Cleanup(drop)
 if _,err=svc.SaveOwnProfilePhotoInternal(ctx,owner,second,&w.Profile.UpdatedAt,false);err==nil{t.Fatal("audit failure accepted")}
 drop()
 actual,err=svc.GetOwnProfilePhotoInternal(ctx,owner);if err!=nil||!bytes.Equal(actual,original){t.Fatal("audit failure changed current photo")}
 snapshot,e:=svc.GetOwnProfileInternal(ctx,owner);if e!=nil||!snapshot.Profile.UpdatedAt.Equal(w.Profile.UpdatedAt){t.Fatal("audit failure changed profile version")}
 // Two writers using one version: exactly one activation wins.
 version:=w.Profile.UpdatedAt;start:=make(chan struct{});results:=make(chan error,2)
 for i:=0;i<2;i++{go func(){<-start;_,e:=svc.SaveOwnProfilePhotoInternal(ctx,owner,second,&version,false);results<-e}()};close(start)
 successes,conflicts:=0,0
 for i:=0;i<2;i++{e:=<-results;if e==nil{successes++}else if errors.Is(e,data.ErrProfileConflict){conflicts++}else{t.Fatal(e)}}
 if successes!=1||conflicts!=1{t.Fatalf("writers: %d successes, %d conflicts",successes,conflicts)}
 w,err=svc.GetOwnProfileInternal(ctx,owner);if err!=nil{t.Fatal(err)}
 // An uncertain acknowledgment must never retire a row already made active.
 var current uuid.UUID
 if err=pool.QueryRow(ctx,`SELECT id FROM media_objects WHERE owner_user_id=$1 AND state='active'`,owner).Scan(&current);err!=nil{t.Fatal(err)}
 svc.retireStagedMedia(current)
 var state string;if err=pool.QueryRow(ctx,`SELECT state FROM media_objects WHERE id=$1`,current).Scan(&state);err!=nil||state!="active"{t.Fatal("active object retired by staging compensation")}
 // Failed deletion rolls back metadata removal; retry is safe.
 if _,err=pool.Exec(ctx,`UPDATE media_objects SET cleanup_after=clock_timestamp()-INTERVAL '1 minute' WHERE owner_user_id=$1 AND state='retired'`,owner);err!=nil{t.Fatal(err)}
 store.failDelete=true
 if err=svc.RunMediaCleanupInternal(ctx,32);err==nil{t.Fatal("cleanup failure hidden")};store.failDelete=false
 if _,err=pool.Exec(ctx,`UPDATE media_objects SET cleanup_after=clock_timestamp()-INTERVAL '1 minute' WHERE owner_user_id=$1 AND state='retired'`,owner);err!=nil{t.Fatal(err)}
 if err=svc.RunMediaCleanupInternal(ctx,32);err!=nil{t.Fatal(err)}
 raw,err:=svc.GetOwnProfilePhotoInternal(ctx,owner);if err!=nil||len(raw)==0{t.Fatal("cleanup deleted active photo")}
 // Integrity failures do not return corrupted or substituted storage bytes.
 store.mu.Lock();saved:=store.objects[current.String()];store.objects[current.String()]=[]byte("substituted");store.mu.Unlock()
 if _,err=svc.GetOwnProfilePhotoInternal(ctx,owner);!errors.Is(err,mediastore.ErrUnavailable){t.Fatal("integrity failure served")}
 store.mu.Lock();store.objects[current.String()]=saved;store.mu.Unlock()
 w,err=svc.SaveOwnProfilePhotoInternal(ctx,owner,nil,&w.Profile.UpdatedAt,true);if err!=nil||w.Profile.HasPhoto{t.Fatal("removal failed")}
 if _,err=svc.GetOwnProfilePhotoInternal(ctx,owner);!errors.Is(err,mediastore.ErrNotFound){t.Fatal("removed photo accessible")}
 // Simulate a crash before Put/activation: aged staging must be reclaimed.
 orphan:=uuid.New();sum:=sha256.Sum256([]byte("partial"))
 if _,err=pool.Exec(ctx,`INSERT INTO media_objects(id,owner_user_id,purpose,storage_driver,storage_location,content_type,byte_size,sha256,state,cleanup_after) VALUES($1,$2,'profile_photo','filesystem','ce-test-memory','image/jpeg',7,$3,'staged',clock_timestamp()-INTERVAL '1 minute')`,orphan,owner,hex.EncodeToString(sum[:]));err!=nil{t.Fatal(err)}
 store.objects[orphan.String()]=[]byte("partial")
 if err=svc.RunMediaCleanupInternal(ctx,32);err!=nil{t.Fatal(err)}
 if _,ok:=store.objects[orphan.String()];ok{t.Fatal("orphan bytes retained")}
 // Deleting a profile must not cascade away the only object cleanup record.
 w,err=svc.SaveOwnProfilePhotoInternal(ctx,owner,first,&w.Profile.UpdatedAt,false);if err!=nil{t.Fatal(err)}
 var deletedProfilePhoto uuid.UUID
 if err=pool.QueryRow(ctx,`SELECT id FROM media_objects WHERE owner_user_id=$1 AND state='active'`,owner).Scan(&deletedProfilePhoto);err!=nil{t.Fatal(err)}
 if err=svc.Models.UserProfile.Delete(ctx,owner);err!=nil{t.Fatal(err)}
 var retained bool
 if err=pool.QueryRow(ctx,`SELECT EXISTS(SELECT 1 FROM media_objects WHERE id=$1)`,deletedProfilePhoto).Scan(&retained);err!=nil||!retained{t.Fatal("profile deletion discarded cleanup metadata")}
 if _,err=svc.GetOwnProfilePhotoInternal(ctx,owner);!errors.Is(err,data.ErrUserProfileNotFound){t.Fatal("deleted profile photo accessible")}
 if err=svc.RunMediaCleanupInternal(ctx,32);err!=nil{t.Fatal(err)}
 if _,ok:=store.objects[deletedProfilePhoto.String()];ok{t.Fatal("deleted profile bytes retained")}

}
