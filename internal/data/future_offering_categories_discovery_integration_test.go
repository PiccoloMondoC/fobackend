// Package data verifies taxonomy replacement, retained references and public category gates.
// focodebase/fobackend/internal/data/future_offering_categories_discovery_integration_test.go
package data

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestCategoryReplacementAndPublicDiscovery(t *testing.T) {
 dsn:=os.Getenv("PLATFORM_TEST_DATABASE_URL")
 if dsn=="" {t.Skip("PLATFORM_TEST_DATABASE_URL not set; canonical PostgreSQL integration test skipped")}
 ctx,cancel:=context.WithTimeout(context.Background(),30*time.Second);defer cancel()
 pool,err:=pgxpool.New(ctx,dsn);if err!=nil{t.Fatal("test database configuration unavailable")};defer pool.Close()
 var database string
 if err=pool.QueryRow(ctx,`SELECT current_database()`).Scan(&database);err!=nil{t.Fatal("test database unavailable")}
 if !strings.Contains(strings.ToLower(database),"test"){t.Fatal("refusing mutations outside a dedicated test database")}
 tx,err:=pool.Begin(ctx);if err!=nil{t.Fatal(err)};defer tx.Rollback(context.Background())
 exec:=func(sql string,args ...any){t.Helper();if _,err:=tx.Exec(ctx,sql,args...);err!=nil{t.Fatal(err)}}
 // Exercise the one-time install even when the canonical test DB was seeded.
 exec(`DELETE FROM catalog_installations WHERE installation_key IN ('future-offering-categories-v1','future-offering-technology-category-v1')`)
 dept,legacy:=uuid.New(),uuid.New()
 exec(`INSERT INTO departments(id,name,slug) VALUES($1,'Legacy test department',$2)`,dept,"legacy-"+dept.String())
 exec(`INSERT INTO categories(id,name,slug,department_id) VALUES($1,'Temporary category',$2,$3)`,legacy,"legacy-"+legacy.String(),dept)
 actor,merchant,fo:=uuid.New(),uuid.New(),uuid.New()
 exec(`INSERT INTO users(id,email,is_active) VALUES($1,$2,TRUE)`,actor,actor.String()+"@example.invalid")
 exec(`INSERT INTO merchants(id,name) VALUES($1,$2)`,merchant,"Catalog fixture "+merchant.String())
 exec(`INSERT INTO merchant_accounts(merchant_id,principal_user_id,account_status) VALUES($1,$2,'active')`,merchant,actor)
 var version time.Time
 if err=tx.QueryRow(ctx,`INSERT INTO merchant_future_offerings(id,merchant_id,project_name,title,summary,category_id) VALUES($1,$2,'Private fixture','Public fixture','Summary',$3) RETURNING updated_at`,fo,merchant,legacy).Scan(&version);err!=nil{t.Fatal(err)}
 exec(installFutureOfferingCatalogQuery)
 var active int
 if err=tx.QueryRow(ctx,`SELECT count(*) FROM categories WHERE deleted_at IS NULL`).Scan(&active);err!=nil||active!=19{t.Fatalf("active catalog count=%d error=%v",active,err)}
 // Reapply the additive install on an already installed catalog; preserve identity,
 // unrelated labels and later administrator edits, including explicit retirement.
 var technology uuid.UUID
 if err=tx.QueryRow(ctx,`SELECT id FROM categories WHERE slug='fo-technology-digital-platforms' AND name='Technology / Digital Platforms' AND deleted_at IS NULL`).Scan(&technology);err!=nil{t.Fatal(err)}
 exec(`UPDATE categories SET name='Admin technology label' WHERE id=$1`,technology)
 exec(`DELETE FROM catalog_installations WHERE installation_key='future-offering-technology-category-v1'`)
 exec(installFutureOfferingCatalogQuery)
 var technologyLabel string
 if err=tx.QueryRow(ctx,`SELECT name FROM categories WHERE id=$1`,technology).Scan(&technologyLabel);err!=nil||technologyLabel!="Admin technology label"{t.Fatal("additive installation overwrote existing category")}
 exec(`UPDATE categories SET deleted_at=clock_timestamp() WHERE id=$1`,technology)
 exec(installFutureOfferingCatalogQuery)
 var retired bool
 if err=tx.QueryRow(ctx,`SELECT deleted_at IS NOT NULL FROM categories WHERE id=$1`,technology).Scan(&retired);err!=nil||!retired{t.Fatal("restart reactivated administrator-retired technology category")}
 var retained bool
 if err=tx.QueryRow(ctx,`SELECT EXISTS(SELECT 1 FROM merchant_future_offerings f JOIN categories c ON c.id=f.category_id WHERE f.id=$1 AND c.id=$2 AND c.deleted_at IS NOT NULL)`,fo,legacy).Scan(&retained);err!=nil||!retained{t.Fatal("legacy reference was not preserved")}
 var ai,other uuid.UUID
 if err=tx.QueryRow(ctx,`SELECT id FROM categories WHERE slug='fo-ai-smart-assistants'`).Scan(&ai);err!=nil{t.Fatal(err)}
 if err=tx.QueryRow(ctx,`SELECT id FROM categories WHERE slug='fo-gadgets-connected-devices'`).Scan(&other);err!=nil{t.Fatal(err)}
 exec(`UPDATE categories SET name='Admin chosen label' WHERE id=$1`,ai)
 exec(installFutureOfferingCatalogQuery)
 var label string
 if err=tx.QueryRow(ctx,`SELECT name FROM categories WHERE id=$1`,ai).Scan(&label);err!=nil||label!="Admin chosen label"{t.Fatal("restart overwrote administrator wording")}
 if err=tx.QueryRow(ctx,`UPDATE merchant_future_offerings SET category_id=$2 WHERE id=$1 RETURNING updated_at`,fo,ai).Scan(&version);err!=nil{t.Fatal(err)}
 // Direct data fixture activation exercises listing predicates independently of readiness.
 model:=MerchantFutureOfferingModel{DB:pool}
 draftIDs,err:=model.PublicOfferingPageTx(ctx,tx,&merchant,nil,nil,&ai)
 if err!=nil||len(draftIDs)!=0{t.Fatal("draft crossed the public category gate")}
 if _,err=model.ActivateTx(ctx,tx,merchant,fo,version);err!=nil{t.Fatal(err)}
 ids,err:=model.PublicOfferingPageTx(ctx,tx,&merchant,nil,nil,&ai)
 if err!=nil||len(ids)!=1||ids[0]!=fo{t.Fatalf("matching category listing failed: %v",err)}
 ids,err=model.PublicOfferingPageTx(ctx,tx,&merchant,nil,nil,&other)
 if err!=nil||len(ids)!=0{t.Fatalf("category filter included unrelated offering: %v",err)}
 if err=model.SetBrandVisibilityTx(ctx,tx,fo,false);err!=nil{t.Fatal(err)}
 ids,err=model.PublicOfferingPageTx(ctx,tx,&merchant,nil,nil,&ai)
 if err!=nil||len(ids)!=0{t.Fatal("brand-hidden offering crossed brand listing gate")}
 ids,err=model.PublicOfferingPageTx(ctx,tx,nil,nil,nil,&ai)
 found:=false;for _,id:=range ids{if id==fo{found=true}}
 if err!=nil||!found{t.Fatal("brand visibility incorrectly hid the public offering")}
 exec(`UPDATE future_offering_publication SET closed_at=clock_timestamp(),close_reason='Fixture closed' WHERE future_offering_id=$1`,fo)
 ids,err=model.PublicOfferingPageTx(ctx,tx,&merchant,nil,nil,&ai)
 if err!=nil||len(ids)!=0{t.Fatalf("closed offering crossed public gate: %v",err)}
 // Rollback includes both the catalog replacement and every fixture.
 if err=tx.Rollback(ctx);err!=nil{t.Fatal(err)}
}
