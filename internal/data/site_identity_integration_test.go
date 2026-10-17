// focodebase/fobackend/internal/data/site_identity_integration_test.go
// Opt-in tests use an already initialized disposable canonical database.
// All changes are rolled back. No schema is defined by this test file.
package data

import (
 "context"
 "errors"
 "os"
 "testing"
 "time"

 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "github.com/jackc/pgx/v5/pgconn"
 "github.com/jackc/pgx/v5/pgxpool"
)

func siteIdentityTestPool(t *testing.T) *pgxpool.Pool {
 t.Helper()
 dsn := os.Getenv("DROP4_TEST_DSN")
 if dsn == "" { t.Skip("DROP4_TEST_DSN not supplied; canonical database integration not executed") }
 pool, err := pgxpool.New(context.Background(), dsn)
 if err != nil { t.Fatal(err) }
 t.Cleanup(pool.Close)
 return pool
}

func TestSiteIdentityDataRollbackAndRevision(t *testing.T) {
 pool := siteIdentityTestPool(t)
 ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
 defer cancel()
 model := SiteIdentityModel{DB:pool}
 tx, err := pool.Begin(ctx)
 if err != nil { t.Fatal(err) }
 defer tx.Rollback(context.Background())
 if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, SiteIdentityMutationLock); err != nil { t.Fatal(err) }
 before, err := model.GetForUpdateTx(ctx, tx)
 if err != nil { t.Fatal(err) }
 changed := before.SitePresentation
 changed.Identity.SiteName = "Independent Test Installation"
 actor := uuid.New()
 after, err := model.ReplaceTx(ctx, tx, changed, before.Version, actor)
 if err != nil { t.Fatal(err) }
 if after.Version != before.Version+1 { t.Fatal("revision did not advance once") }
 if err = model.RecordChangeTx(ctx, tx, before, after, "Rollback verification", actor); err != nil { t.Fatal(err) }
 if _, err = model.ReplaceTx(ctx, tx, before.SitePresentation, before.Version, actor); !errors.Is(err, ErrSiteIdentityVersionConflict) { t.Fatalf("stale update accepted: %v",err) }
 if err = tx.Rollback(ctx); err != nil { t.Fatal(err) }
 restored, err := model.Get(ctx)
 if err != nil { t.Fatal(err) }
 if restored.Version != before.Version || restored.Identity != before.Identity { t.Fatal("rolled-back configuration escaped its transaction") }
}

func TestSiteIdentitySharedMutexSerializesWriters(t *testing.T) {
 pool := siteIdentityTestPool(t)
 ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
 defer cancel()
 first, err := pool.Begin(ctx)
 if err != nil { t.Fatal(err) }
 defer first.Rollback(context.Background())
 second, err := pool.Begin(ctx)
 if err != nil { t.Fatal(err) }
 defer second.Rollback(context.Background())
 if _, err = first.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, SiteIdentityMutationLock); err != nil { t.Fatal(err) }
 if _, err = second.Exec(ctx, `SET LOCAL lock_timeout = '100ms'`); err != nil { t.Fatal(err) }
 _, err = second.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, SiteIdentityMutationLock)
 var pgErr *pgconn.PgError
 if !errors.As(err,&pgErr) || pgErr.Code != "55P03" { t.Fatalf("shared mutex did not block a competing writer: %v",err) }
}

func TestSiteIdentityHistoryCannotBeRewritten(t *testing.T) {
 pool := siteIdentityTestPool(t)
 ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
 defer cancel()
 for _,operation := range []string{
  `UPDATE site_identity_history SET change_reason='rewritten'`,
  `DELETE FROM site_identity_history`,
  `TRUNCATE site_identity_history`,
 } {
  err := pgx.BeginFunc(ctx,pool,func(tx pgx.Tx) error {
   if _,err:=tx.Exec(ctx,`SELECT pg_advisory_xact_lock($1)`,SiteIdentityMutationLock);err!=nil{return err}
   model:=SiteIdentityModel{DB:pool}
   before,err:=model.GetForUpdateTx(ctx,tx);if err!=nil{return err}
   after:=*before;after.Version++
   if err=model.RecordChangeTx(ctx,tx,before,&after,"Immutable history verification",uuid.New());err!=nil{return err}
   _,err=tx.Exec(ctx,operation)
   return err
  })
  var pgErr *pgconn.PgError
  if !errors.As(err,&pgErr) || pgErr.Code != "P0001" { t.Fatalf("history operation was not blocked: %v",err) }
 }
}
