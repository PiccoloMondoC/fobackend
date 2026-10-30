// Package services tests transactional image-set failures against canonical persistence.
// focodebase/fobackend/internal/services/future_offering_webp_integration_test.go
//
//go:build integration
//
// These tests use production schema and real encoding, with an injected private
// store for deterministic partial-write and deletion failures. Nothing is run
// against a non-disposable database.
package services

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/png"
	"io"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/PiccoloMondoC/focodebase/fobackend/internal/data"
	"github.com/PiccoloMondoC/focodebase/fobackend/internal/mediaimage"
	"github.com/PiccoloMondoC/focodebase/fobackend/internal/mediastore"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type offeringBundleStore struct{
	mu sync.Mutex
	location string
	objects map[string][]byte
	puts,failAt int
	failDelete bool
	afterPut func()
}
func(s *offeringBundleStore)Location()(string,string){return "filesystem",s.location}
func(s *offeringBundleStore)Close()error{return nil}
func(s *offeringBundleStore)Put(_ context.Context,key,_ string,reader io.Reader)error{
	raw,err:=io.ReadAll(reader);if err!=nil{return err}
	s.mu.Lock();s.puts++;s.objects[key]=raw;fail:=s.failAt==s.puts;callback:=s.afterPut;s.mu.Unlock()
	if callback!=nil{callback()};if fail{return mediastore.ErrUnavailable};return nil
}
func(s *offeringBundleStore)Open(_ context.Context,key string)(io.ReadCloser,error){
	s.mu.Lock();defer s.mu.Unlock();raw,ok:=s.objects[key];if !ok{return nil,mediastore.ErrNotFound}
	return io.NopCloser(bytes.NewReader(append([]byte{},raw...))),nil
}
func(s *offeringBundleStore)Delete(_ context.Context,key string)error{
	s.mu.Lock();defer s.mu.Unlock();if s.failDelete{return mediastore.ErrUnavailable};delete(s.objects,key);return nil
}
func TestOfferingWebPTransactionalFailures(t *testing.T){
	dsn:=os.Getenv("PLATFORM_TEST_DATABASE_URL")
	if dsn==""||os.Getenv("PLATFORM_TEST_DATABASE_DISPOSABLE")!="1"{t.Skip("explicit disposable offering test database required")}
	if _,err:=os.Stat(mediaimage.Executable);os.IsNotExist(err){t.Skip("cwebp not installed; run the Docker integration test target")}
	ctx,cancel:=context.WithTimeout(context.Background(),2*time.Minute);defer cancel()
	pool,err:=pgxpool.New(ctx,dsn);if err!=nil{t.Fatal("test database unavailable")};t.Cleanup(pool.Close)
	var database string;if err=pool.QueryRow(ctx,`SELECT current_database()`).Scan(&database);err!=nil{t.Fatal(err)}
	if !strings.Contains(strings.ToLower(database),"test"){t.Fatal("dedicated test database required")}
	models:=data.New(pool,integrationLogger(t))
	svc,err:=NewService(integrationLogger(t),&models,&Config{DBTimeout:10*time.Second,ActivationTokenTTL:time.Hour,PasswordResetTokenTTL:time.Hour},make(chan struct{}),stubSessionTokenIssuer{})
	if err!=nil{t.Fatal(err)}
	actor,merchant,fo:=uuid.New(),uuid.New(),uuid.New()
	store:=&offeringBundleStore{location:"offering-test-"+fo.String(),objects:map[string][]byte{}}
	svc.media=store;svc.photoSlots=make(chan struct{},2)
	exec:=func(sql string,args ...any){t.Helper();if _,e:=pool.Exec(ctx,sql,args...);e!=nil{t.Fatal(e)}}
	exec(`INSERT INTO users(id,email,is_active) VALUES($1,$2,true)`,actor,actor.String()+"@example.invalid")
	exec(`INSERT INTO merchants(id,name) VALUES($1,'Public test brand')`,merchant)
	exec(`INSERT INTO merchant_accounts(merchant_id,principal_user_id,account_status) VALUES($1,$2,'active')`,merchant,actor)
	var version time.Time
	if err=pool.QueryRow(ctx,`INSERT INTO merchant_future_offerings(id,merchant_id,project_name,title,summary,access_policy) VALUES($1,$2,'Private test','Public title','Summary','invite_only') RETURNING updated_at`,fo,merchant).Scan(&version);err!=nil{t.Fatal(err)}
	// The dedicated database intentionally retains immutable revisions/outbox
	// history. Cleanup makes test identities inaccessible and retires its bytes.
	t.Cleanup(func(){
		cleanup,finish:=context.WithTimeout(context.Background(),5*time.Second);defer finish()
		_,_ = pool.Exec(cleanup,`UPDATE users SET is_active=false WHERE id=$1`,actor)
		_,_ = pool.Exec(cleanup,`DELETE FROM future_offering_media WHERE future_offering_id=$1`,fo)
		_,_ = pool.Exec(cleanup,`UPDATE media_objects SET state='retired',cleanup_after=clock_timestamp() WHERE storage_location=$1`,store.location)
		_,_ = pool.Exec(cleanup,`UPDATE merchant_future_offerings SET deleted_at=clock_timestamp() WHERE id=$1`,fo)
	})
	var raw bytes.Buffer;if err=png.Encode(&raw,image.NewNRGBA(image.Rect(0,0,8,4)));err!=nil{t.Fatal(err)}
	upload:=func(v time.Time)(FutureOfferingGallery,error){return svc.UploadFutureOfferingMedia(ctx,actor,merchant,fo,v,"Main image",bytes.NewReader(raw.Bytes()))}
	gallery,err:=upload(version);if err!=nil{t.Fatal(err)};version=gallery.UpdatedAt
	if len(gallery.Media)!=1||gallery.Media[0].ContentType!="image/webp"{t.Fatal("image set not exposed as one asset")}
	first:=gallery.Media[0]
	// No public request, even with a valid asset UUID, can fetch its original.
	if _,_,err=svc.OpenFutureOfferingMediaVariant(ctx,nil,fo,first.ID,"original");!errors.Is(err,data.ErrMerchantFutureOfferingNotFound){t.Fatal("draft original publicly accessible")}
	original,_,err:=svc.OpenFutureOfferingMediaVariant(ctx,&merchant,fo,first.ID,"original");if err!=nil{t.Fatal(err)}
	got,_:=io.ReadAll(original);_ = original.Close();if !bytes.Equal(got,raw.Bytes()){t.Fatal("original changed")}
	// A partial Put leaves durable retirement evidence and preserves the gallery.
	store.failAt=store.puts+2
	if _,err=upload(version);!errors.Is(err,mediastore.ErrUnavailable){t.Fatal("partial write accepted")};store.failAt=0
	unchanged,err:=svc.GetFutureOfferingGallery(ctx,merchant,fo);if err!=nil||!unchanged.UpdatedAt.Equal(version)||len(unchanged.Media)!=1{t.Fatal("partial write changed aggregate")}
	// Authority revoked during external I/O must be rechecked at attachment.
	store.afterPut=func(){exec(`UPDATE users SET is_active=false WHERE id=$1`,actor)}
	if _,err=upload(version);!errors.Is(err,data.ErrMerchantAccountAccessDenied){t.Fatal("revoked principal attached media")}
	store.afterPut=nil;exec(`UPDATE users SET is_active=true WHERE id=$1`,actor)
	// A durable-history failure rolls back the complete attachment/version.
	name:="test_bundle_"+strings.ReplaceAll(fo.String(),"-","")
	exec(`CREATE FUNCTION `+name+`() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.future_offering_id='`+fo.String()+`'::uuid THEN RAISE EXCEPTION 'test history failure'; END IF; RETURN NEW; END $$`)
	exec(`CREATE TRIGGER `+name+` BEFORE INSERT ON future_offering_revisions FOR EACH ROW EXECUTE FUNCTION `+name+`() `)
	t.Cleanup(func(){cleanup,finish:=context.WithTimeout(context.Background(),3*time.Second);defer finish();_,_ = pool.Exec(cleanup,`DROP FUNCTION IF EXISTS `+name+`() CASCADE`)})
	if _,err=upload(version);err==nil{t.Fatal("history failure committed media")}
	exec(`DROP FUNCTION `+name+`() CASCADE`)
	unchanged,err=svc.GetFutureOfferingGallery(ctx,merchant,fo);if err!=nil||!unchanged.UpdatedAt.Equal(version)||len(unchanged.Media)!=1{t.Fatal("history failure changed aggregate")}
	// Concurrent writers starting from one version must have exactly one winner.
	start:=make(chan struct{});results:=make(chan error,2)
	for i:=0;i<2;i++{go func(){<-start;_,e:=upload(version);results<-e}()};close(start)
	winners,conflicts:=0,0
	for i:=0;i<2;i++{e:=<-results;if e==nil{winners++}else if errors.Is(e,data.ErrMerchantFutureOfferingEditConflict){conflicts++}else{t.Fatal(e)}}
	if winners!=1||conflicts!=1{t.Fatal("stale concurrent attachment was accepted")}
	gallery,err=svc.GetFutureOfferingGallery(ctx,merchant,fo);if err!=nil{t.Fatal(err)}
	// Compensation after an ambiguous acknowledgment must preserve all active
	// parent and companion objects. Only still-staged rows may be retired.
	rows,err:=pool.Query(ctx,`SELECT id FROM media_objects WHERE storage_location=$1 AND state='active'`,store.location);if err!=nil{t.Fatal(err)}
	active:=[]uuid.UUID{};for rows.Next(){var id uuid.UUID;if err=rows.Scan(&id);err!=nil{t.Fatal(err)};active=append(active,id)};err=rows.Err();rows.Close();if err!=nil{t.Fatal(err)}
	for _,id:=range active{svc.retireStagedMedia(id)}
	var count int;if err=pool.QueryRow(ctx,`SELECT count(*) FROM media_objects WHERE id=ANY($1::uuid[]) AND state='active'`,active).Scan(&count);err!=nil||count!=8{t.Fatal("active set retired by compensation")}
	// Removing an image retires its original and derivatives. Failed deletion
	// retains evidence; retry removes only retired objects, never the other set.
	if _,err=svc.EditFutureOfferingMedia(ctx,actor,merchant,fo,gallery.UpdatedAt,first,true);err!=nil{t.Fatal(err)}
	exec(`UPDATE media_objects SET cleanup_after=clock_timestamp() WHERE storage_location=$1 AND state='retired'`,store.location)
	store.failDelete=true;if err=svc.RunMediaCleanupInternal(ctx,32);err==nil{t.Fatal("cleanup failure hidden")};store.failDelete=false
	exec(`UPDATE media_objects SET cleanup_after=clock_timestamp() WHERE storage_location=$1 AND state='retired'`,store.location)
	if err=svc.RunMediaCleanupInternal(ctx,32);err!=nil{t.Fatal(err)}
	if err=pool.QueryRow(ctx,`SELECT count(*) FROM media_objects WHERE storage_location=$1`,store.location).Scan(&count);err!=nil||count!=4{t.Fatal("cleanup retained garbage or deleted active companions")}
}
