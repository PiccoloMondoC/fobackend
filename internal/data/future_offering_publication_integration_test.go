// Package data checks publication privacy and transactional media against the
// canonical initialized schema; it never creates an approximate test schema.
// focodebase/fobackend/internal/data/future_offering_publication_integration_test.go
package data

import (
 "context"
 "errors"
 "os"
 "strings"
 "testing"
 "time"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5/pgxpool"
)

func TestOfferingPublicationCanonicalTransaction(t *testing.T){
 dsn:=os.Getenv("PLATFORM_TEST_DATABASE_URL")
 if dsn==""{t.Skip("PLATFORM_TEST_DATABASE_URL not set; canonical PostgreSQL integration test skipped")}
 ctx,cancel:=context.WithTimeout(context.Background(),15*time.Second);defer cancel()
 pool,err:=pgxpool.New(ctx,dsn);if err!=nil{t.Fatal("test database configuration unavailable")};defer pool.Close()
 var database string;if err=pool.QueryRow(ctx,`SELECT current_database()`).Scan(&database);err!=nil{t.Fatal("test database unavailable")}
 if !strings.Contains(strings.ToLower(database),"test"){t.Fatal("refusing mutations outside a dedicated test database")}
 tx,err:=pool.Begin(ctx);if err!=nil{t.Fatal(err)};defer tx.Rollback(context.Background())
 actor,merchant,fo,asset:=uuid.New(),uuid.New(),uuid.New(),uuid.New()
 exec:=func(sql string,args ...any){t.Helper();if _,err:=tx.Exec(ctx,sql,args...);err!=nil{t.Fatal(err)}}
 exec(`INSERT INTO users(id,email) VALUES($1,$2)`,actor,actor.String()+"@example.invalid")
 exec(`INSERT INTO merchants(id,name) VALUES($1,$2)`,merchant,"Publication test "+merchant.String())
 exec(`INSERT INTO merchant_accounts(merchant_id,principal_user_id,account_status) VALUES($1,$2,'active')`,merchant,actor)
 var version time.Time
 if err=tx.QueryRow(ctx,`INSERT INTO merchant_future_offerings(id,merchant_id,project_name,title,summary,access_policy) VALUES($1,$2,'Private plan','Public title','Public summary','invite_only') RETURNING updated_at`,fo,merchant).Scan(&version);err!=nil{t.Fatal(err)}
 model:=MerchantFutureOfferingModel{DB:pool}
 if _,_,err=model.PublicOfferingTx(ctx,tx,fo);!errors.Is(err,ErrMerchantFutureOfferingNotFound){t.Fatalf("draft public gate: %v",err)}
 if err=model.StageMediaTx(ctx,tx,FutureOfferingMediaStage{ID:asset,Owner:actor,Driver:"filesystem",Location:"test-only",ContentType:"image/jpeg",Size:10,Digest:strings.Repeat("0",64)});err!=nil{t.Fatal(err)}
 if err=model.AttachMediaTx(ctx,tx,fo,FutureOfferingMedia{ID:asset,Kind:"image",AltText:"Test image",Main:true});err!=nil{t.Fatal(err)}
 active,err:=model.ActivateTx(ctx,tx,merchant,fo,version);if err!=nil{t.Fatal(err)}
 if !active.UpdatedAt.After(version)||active.PublishedAt==nil{t.Fatal("activation did not advance publication/version")}
 visible,source,err:=model.PublicationTx(ctx,tx,fo);if err!=nil||!visible||!source.Equal(version){t.Fatalf("activation provenance: %v",err)}
 public,_,err:=model.PublicOfferingTx(ctx,tx,fo);if err!=nil||public==nil{t.Fatalf("active public gate: %v",err)}
 if _,err=model.ActivateTx(ctx,tx,merchant,fo,version);!errors.Is(err,ErrMerchantFutureOfferingEditConflict){t.Fatalf("duplicate data activation accepted: %v",err)}
 gallery,err:=model.ListMediaTx(ctx,tx,fo);if err!=nil||len(gallery)!=1||!gallery[0].Main{t.Fatalf("gallery: %v",err)}
 if _,err=model.RecordRevisionTx(ctx,tx,fo,actor,active.UpdatedAt,"media",[]FutureOfferingMedia{},gallery);err!=nil{t.Fatal(err)}
 sub,err:=tx.Begin(ctx);if err!=nil{t.Fatal(err)}
 if _,err=sub.Exec(ctx,`DELETE FROM future_offering_revisions WHERE future_offering_id=$1`,fo);err==nil{t.Fatal("immutable history deletion accepted")}
 if err=sub.Rollback(ctx);err!=nil{t.Fatal(err)}
 if _,err=model.ClosePublishedTx(ctx,tx,merchant,fo,active.UpdatedAt,"Validation: do not build");err!=nil{t.Fatal(err)}
 if _,_,err=model.PublicOfferingTx(ctx,tx,fo);!errors.Is(err,ErrMerchantFutureOfferingNotFound){t.Fatalf("closed offering public gate: %v",err)}
 // No fixture, media metadata, publication or history survives the transaction.
 if err=tx.Rollback(ctx);err!=nil{t.Fatal(err)}
 var exists bool;if err=pool.QueryRow(ctx,`SELECT EXISTS(SELECT 1 FROM merchant_future_offerings WHERE id=$1)`,fo).Scan(&exists);err!=nil||exists{t.Fatal("rollback did not remove the complete fixture")}
}
