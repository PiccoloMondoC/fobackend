// Package data tests image-set validation and canonical persistence.
// focodebase/fobackend/internal/data/future_offering_media_variants_test.go
package data

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func mediaBundleFixture(owner,asset uuid.UUID)[]FutureOfferingMediaObject{
	parts:=[]FutureOfferingMediaObject{}
	for _,role:=range []string{"detail","card","thumbnail","original"}{
		id,mime:=uuid.New(),"image/webp";if role=="detail"{id=asset};if role=="original"{mime="image/png"}
		parts=append(parts,FutureOfferingMediaObject{FutureOfferingMediaStage:FutureOfferingMediaStage{ID:id,Owner:owner,Driver:"filesystem",Location:"test-only",ContentType:mime,Size:10,Digest:strings.Repeat("0",64)},Role:role})
	};return parts
}
func TestMediaBundleRefusesIncompleteAndMixedOwnership(t *testing.T){
	owner,asset:=uuid.New(),uuid.New();parts:=mediaBundleFixture(owner,asset)
	if err:=validMediaBundle(asset,parts);err!=nil{t.Fatal(err)}
	if validMediaBundle(asset,parts[:3])==nil{t.Fatal("incomplete image set accepted")}
	parts[1].Owner=uuid.New();if validMediaBundle(asset,parts)==nil{t.Fatal("foreign object accepted")}
	parts=mediaBundleFixture(owner,asset);parts[2].Role="original";if validMediaBundle(asset,parts)==nil{t.Fatal("duplicate role accepted")}
}
func TestOfferingMediaBundleCanonicalDatabase(t *testing.T){
	dsn:=os.Getenv("PLATFORM_TEST_DATABASE_URL");if dsn==""{t.Skip("PLATFORM_TEST_DATABASE_URL not set; canonical database test skipped")}
	ctx,cancel:=context.WithTimeout(context.Background(),20*time.Second);defer cancel()
	pool,err:=pgxpool.New(ctx,dsn);if err!=nil{t.Fatal("test database unavailable")};defer pool.Close()
	var database string;if err=pool.QueryRow(ctx,`SELECT current_database()`).Scan(&database);err!=nil{t.Fatal(err)}
	if !strings.Contains(strings.ToLower(database),"test"){t.Fatal("dedicated test database required")}
	tx,err:=pool.Begin(ctx);if err!=nil{t.Fatal(err)};defer tx.Rollback(context.Background())
	actor,merchant,fo,asset:=uuid.New(),uuid.New(),uuid.New(),uuid.New()
	exec:=func(sql string,args ...any){t.Helper();if _,err:=tx.Exec(ctx,sql,args...);err!=nil{t.Fatal(err)}}
	exec(`INSERT INTO users(id,email,is_active) VALUES($1,$2,true)`,actor,actor.String()+"@example.invalid")
	exec(`INSERT INTO merchants(id,name) VALUES($1,'Bundle test')`,merchant)
	exec(`INSERT INTO merchant_accounts(merchant_id,principal_user_id,account_status) VALUES($1,$2,'active')`,merchant,actor)
	exec(`INSERT INTO merchant_future_offerings(id,merchant_id,project_name,title,summary,access_policy) VALUES($1,$2,'Private plan','Public title','Summary','invite_only')`,fo,merchant)
	model:=MerchantFutureOfferingModel{DB:pool};parts:=mediaBundleFixture(actor,asset)
	if err=model.StageMediaBundleTx(ctx,tx,asset,parts);err!=nil{t.Fatal(err)}
	gallery,err:=model.ListMediaTx(ctx,tx,fo);if err!=nil||len(gallery)!=0{t.Fatal("staging became visible")}
	if err=model.AttachMediaBundleTx(ctx,tx,fo,FutureOfferingMedia{ID:asset,Kind:"image",Main:true},parts);err!=nil{t.Fatal(err)}
	gallery,err=model.ListMediaTx(ctx,tx,fo);if err!=nil||len(gallery)!=1||gallery[0].ContentType!="image/webp"{t.Fatal("complete set not attached")}
	for _,role:=range []string{"detail","card","thumbnail","original"}{
		id,meta,err:=model.ResolveMediaVariantTx(ctx,tx,gallery[0],role);if err!=nil{t.Fatal(err)}
		if meta.ID!=asset||meta.ByteSize!=10||meta.Digest!=strings.Repeat("0",64){t.Fatal("variant metadata invalid")}
		if (role=="detail")!=(id==asset){t.Fatal("object role invalid")}
	}
	raw,err:=json.Marshal(gallery);if err!=nil{t.Fatal(err)}
	if strings.Contains(string(raw),parts[3].ID.String())||strings.Contains(string(raw),"original")||strings.Contains(string(raw),"digest"){t.Fatal("private object evidence leaked")}
	if err=model.EditMediaTx(ctx,tx,fo,gallery[0],true);err!=nil{t.Fatal(err)}
	var count int
	if err=tx.QueryRow(ctx,`SELECT count(*) FROM media_objects WHERE owner_user_id=$1 AND state='retired'`,actor).Scan(&count);err!=nil||count!=4{t.Fatal("incomplete retirement")}
	exec(`DELETE FROM media_objects WHERE owner_user_id=$1 AND state='retired'`,actor)
	if err=tx.QueryRow(ctx,`SELECT count(*) FROM future_offering_media_variants WHERE asset_id=$1`,asset).Scan(&count);err!=nil||count!=0{t.Fatal("cleanup mappings retained")}
	// An aged staging row retired by cleanup must prevent whole-set attachment.
	asset=uuid.New();parts=mediaBundleFixture(actor,asset)
	if err=model.StageMediaBundleTx(ctx,tx,asset,parts);err!=nil{t.Fatal(err)}
	exec(`UPDATE media_objects SET state='retired' WHERE id=$1`,parts[1].ID)
	if err=model.AttachMediaBundleTx(ctx,tx,fo,FutureOfferingMedia{ID:asset,Kind:"image"},parts);!errors.Is(err,ErrMerchantFutureOfferingEditConflict){t.Fatal("incomplete attachment accepted")}
	// Legacy images remain displayable but no invented original is available.
	legacy:=FutureOfferingMedia{ID:uuid.New(),Kind:"image",ContentType:"image/jpeg",ByteSize:10}
	if id,_,err:=model.ResolveMediaVariantTx(ctx,tx,legacy,"card");err!=nil||id!=legacy.ID{t.Fatal("legacy display fallback lost")}
	if _,_,err:=model.ResolveMediaVariantTx(ctx,tx,legacy,"original");!errors.Is(err,ErrMerchantFutureOfferingNotFound){t.Fatal("legacy original manufactured")}
	// The complete fixture rolls back; canonical schema is never approximated.
}
