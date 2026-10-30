// Package data tests actual publication gates, public-only matching and paging.
// focodebase/fobackend/internal/data/future_offering_search_test.go
package data

import(
	"context"
	"os"
	"strings"
	"testing"
	"time"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)
func TestPublicOfferingSearchCanonicalDatabase(t *testing.T){
	dsn:=os.Getenv("PLATFORM_TEST_DATABASE_URL");if dsn==""{t.Skip("canonical dedicated test database not configured")}
	ctx,cancel:=context.WithTimeout(context.Background(),20*time.Second);defer cancel()
	pool,err:=pgxpool.New(ctx,dsn);if err!=nil{t.Fatal("test database unavailable")};defer pool.Close()
	var name string;if err=pool.QueryRow(ctx,`SELECT current_database()`).Scan(&name);err!=nil{t.Fatal(err)}
	if !strings.Contains(strings.ToLower(name),"test"){t.Fatal("dedicated test database required")}
	tx,err:=pool.Begin(ctx);if err!=nil{t.Fatal(err)};defer tx.Rollback(context.Background())
	exec:=func(sql string,args ...any){t.Helper();if _,err:=tx.Exec(ctx,sql,args...);err!=nil{t.Fatal(err)}}
	actor,merchant:=uuid.New(),uuid.New();term:="term"+strings.ReplaceAll(uuid.New().String(),"-","")
	exec(`INSERT INTO users(id,email,is_active) VALUES($1,$2,true)`,actor,actor.String()+"@example.invalid")
	exec(`INSERT INTO merchants(id,name) VALUES($1,$2)`,merchant,"Public brand "+term)
	exec(`INSERT INTO merchant_accounts(merchant_id,principal_user_id,account_status) VALUES($1,$2,'active')`,merchant,actor)
	model:=MerchantFutureOfferingModel{DB:pool};var preferred uuid.UUID
	for i:=0;i<19;i++{
		id:=uuid.New();title:="Real offering";if i==0{title="AI assistant "+term;preferred=id}
		var version time.Time
		if err=tx.QueryRow(ctx,`INSERT INTO merchant_future_offerings(id,merchant_id,project_name,title,summary,access_policy) VALUES($1,$2,$3,$4,$5,'invite_only') RETURNING updated_at`,id,merchant,"privateonly"+term,title,"AI assistant public summary % _ "+term).Scan(&version);err!=nil{t.Fatal(err)}
		if i==18{continue} // Draft title/summary must never match publicly.
		active,err:=model.ActivateTx(ctx,tx,merchant,id,version);if err!=nil{t.Fatal(err)}
		if i==5{if _,err=model.ClosePublishedTx(ctx,tx,merchant,id,active.UpdatedAt,"Test closure");err!=nil{t.Fatal(err)}}
	}
	q:=PublicOfferingSearch{Terms:[]string{"AI","assistant",term},Sort:"relevance",Page:1}
	ids,total,err:=model.SearchPublicOfferingIDsTx(ctx,tx,q)
	if err!=nil||total!=17||len(ids)!=16||ids[0]!=preferred{t.Fatalf("search gates/ranking/count: total=%d items=%d error=%v",total,len(ids),err)}
	q.Page=2;second,count,err:=model.SearchPublicOfferingIDsTx(ctx,tx,q);if err!=nil||count!=17||len(second)!=1{t.Fatal("second page count invalid")}
	for _,id:=range ids{if id==second[0]{t.Fatal("page membership duplicated")}}
	q.Page=1;q.Terms=[]string{"privateonly"+term};ids,total,err=model.SearchPublicOfferingIDsTx(ctx,tx,q);if err!=nil||total!=0||len(ids)!=0{t.Fatal("private project name searched")}
	exec(`UPDATE merchant_future_offerings SET description=$2::jsonb WHERE id=$1`,preferred,`{"type":"doc","version":1,"blocks":[{"type":"paragraph","content":[{"text":"quantum "},{"text":"wid","marks":["bold"]},{"text":"get","marks":["italic"]}]}]}`)
	q.Terms=[]string{"quantum","widget",term};ids,total,err=model.SearchPublicOfferingIDsTx(ctx,tx,q);if err!=nil||total!=1||len(ids)!=1||ids[0]!=preferred{t.Fatal("subject matter in formatted description not searchable")}
	q.Terms=[]string{"bold",term};_,total,err=model.SearchPublicOfferingIDsTx(ctx,tx,q);if err!=nil||total!=0{t.Fatal("rich-text metadata searched as content")}
	department,category:=uuid.New(),uuid.New();label:="categorysubject"+term
	exec(`INSERT INTO departments(id,name,slug) VALUES($1,'Test discovery',$2)`,department,"search-"+department.String())
	exec(`INSERT INTO categories(id,name,slug,department_id) VALUES($1,$2,$3,$4)`,category,label,"search-"+category.String(),department)
	exec(`UPDATE merchant_future_offerings SET category_id=$2 WHERE id=$1`,preferred,category)
	q.Terms=[]string{label};ids,total,err=model.SearchPublicOfferingIDsTx(ctx,tx,q);if err!=nil||total!=1||len(ids)!=1||ids[0]!=preferred{t.Fatal("category subject not searchable")}
	q.Terms=[]string{term};q.Category=&category;_,total,err=model.SearchPublicOfferingIDsTx(ctx,tx,q);if err!=nil||total!=1{t.Fatal("category refinement not applied")};q.Category=nil
	q.Terms=[]string{"notpresent%"+term};_,total,err=model.SearchPublicOfferingIDsTx(ctx,tx,q);if err!=nil||total!=0{t.Fatal("percent treated as wildcard")}
	q.Terms=[]string{term};exec(`UPDATE merchant_accounts SET account_status='suspended' WHERE merchant_id=$1`,merchant)
	_,total,err=model.SearchPublicOfferingIDsTx(ctx,tx,q);if err!=nil||total!=0{t.Fatal("suspended merchant appeared in search")}
}
