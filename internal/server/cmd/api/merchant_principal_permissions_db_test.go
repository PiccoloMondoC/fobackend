// Package main provides database integration coverage for canonical merchant-principal capabilities and request-scoped authorization.
// focodebase/fobackend/internal/server/cmd/api/merchant_principal_permissions_db_test.go
//
// GTM:
//     Layer: 3.2 Merchant Registration / HTTP and Authorization
//     Release Class: SPINE
//     Reason: Supports the complete Foundation Level 4 merchant-registration and account-navigation vertical.
//
// SPINE Rule:
//     Keep compiling and preserve established security, lifecycle, and privacy contracts.
//     Preserve canonical principal ownership and backend authorization.
//     Keep private registration values out of public projections, logs, and errors.
//
package main

import (
 "context"
 "os"
 "net/http"
 "net/http/httptest"
 "testing"
 "time"

 "github.com/PiccoloMondoC/focodebase/fobackend/internal/data"
 "github.com/PiccoloMondoC/focodebase/fobackend/internal/logging"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5/pgxpool"
)

// This integration specification exercises the actual HasPermission path,
// including the request-local cache, rather than a stand-in authorization API.
func TestMerchantRegistration_PrincipalPermissionBridge(t *testing.T){
 dsn:=os.Getenv("FOBACKEND_TEST_DATABASE_URL");if dsn==""{t.Skip("dedicated integration database not configured")}
 logger,err:=logging.InitLogging("merchant-capability-test");if err!=nil{t.Fatal(err)}
 ctx,cancel:=context.WithTimeout(context.Background(),30*time.Second);defer cancel()
 pool,err:=pgxpool.New(ctx,dsn);if err!=nil{t.Fatal(err)};defer pool.Close()
 schema:=&data.DBConnectionParamsModel{DB:pool,Logger:logger};if err=schema.CreateTables(pool);err!=nil{t.Fatal(err)}
 exec:=func(sql string,args ...any){t.Helper();if _,err:=pool.Exec(ctx,sql,args...);err!=nil{t.Fatal(err)}}
 // Existing reference rows are preserved. This test never grants a consumer
 // role a merchant permission; the principal bridge must do the work.
 exec(`INSERT INTO roles(name,description,hierarchy_level,is_internal,assignable_at_signup)
 VALUES('consumer','Consumer',0,FALSE,TRUE),('merchant','Merchant',0,FALSE,TRUE) ON CONFLICT(name) DO NOTHING`)
 var consumer,merchantRole uuid.UUID
 if err=pool.QueryRow(ctx,`SELECT id FROM roles WHERE name='consumer'`).Scan(&consumer);err!=nil{t.Fatal(err)}
 if err=pool.QueryRow(ctx,`SELECT id FROM roles WHERE name='merchant'`).Scan(&merchantRole);err!=nil{t.Fatal(err)}
 const capability="list_merchant_future_offerings"
 const catalogCapability="list_categories"
 exec(`INSERT INTO permissions(name) VALUES($1),($2) ON CONFLICT(name) DO NOTHING`,capability,catalogCapability)
 exec(`INSERT INTO role_permissions(role_id,permission_id) SELECT $1,id FROM permissions WHERE name IN ($2,$3) ON CONFLICT DO NOTHING`,merchantRole,capability,catalogCapability)
 owner,other,merchant,account:=uuid.New(),uuid.New(),uuid.New(),uuid.New()
 exec(`INSERT INTO users(id,email,is_active) VALUES($1,$2,TRUE),($3,$4,TRUE)`,owner,owner.String()+"@example.test",other,other.String()+"@example.test")
 exec(`INSERT INTO user_role_assignments(user_id,role_id,is_primary) VALUES($1,$2,TRUE),($3,$2,TRUE)`,owner,consumer,other)
 exec(`INSERT INTO merchants(id,name) VALUES($1,$2)`,merchant,"Permission fixture "+merchant.String())
 exec(`INSERT INTO merchant_accounts(id,merchant_id,principal_user_id,account_status,onboarded_at) VALUES($1,$2,$3,'active',NOW())`,account,merchant,owner)
 defer func(){
  cleanup,done:=context.WithTimeout(context.Background(),10*time.Second);defer done()
  _,_=pool.Exec(cleanup,`DELETE FROM merchant_accounts WHERE id=$1`,account)
  _,_=pool.Exec(cleanup,`DELETE FROM merchants WHERE id=$1`,merchant)
  _,_=pool.Exec(cleanup,`DELETE FROM users WHERE id=$1 OR id=$2`,owner,other)
 }()
 // A separate non-consumer role with no grants proves ownership authority
 // is independent of a consumer-only role-name check or inherited grants.
 principalRole:=uuid.New()
 exec(`INSERT INTO roles(id,name,description,hierarchy_level,is_internal,assignable_at_signup)
 VALUES($1,$2,'Principal permission regression',0,FALSE,FALSE)`,principalRole,"pt_"+principalRole.String())
 defer func(){cleanup,done:=context.WithTimeout(context.Background(),10*time.Second);defer done();_,_=pool.Exec(cleanup,`DELETE FROM roles WHERE id=$1`,principalRole)}()
 app:=&Application{Models:data.New(pool,logger),Logger:logger}
 // Exercise the real development fallback that inserts a placeholder
 // merchant when a Platform catalog request has no merchant header.
 app.Config.Bootstrap.Env="development"
 requestContext:=func(actor uuid.UUID)context.Context{
  c:=context.WithValue(ctx,ctxUserID,actor)
  c=context.WithValue(c,ctxRoleID,consumer.String())
  c=context.WithValue(c,ctxRoleName,"consumer")
  c=context.WithValue(c,ctxMerchantID,merchant)
  return context.WithValue(c,ctxAuthzState,newAuthzState(consumer))
 }
 checkGate:=func(c context.Context,permission string,path string,want int){
  t.Helper()
  recorder:=httptest.NewRecorder()
  request:=httptest.NewRequest(http.MethodGet,path,nil).WithContext(c)
  handler:=app.RequirePermission(permission)(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){w.WriteHeader(http.StatusNoContent)}))
  app.DevFallbackContextMiddleware(handler).ServeHTTP(recorder,request)
  if recorder.Code!=want{t.Fatalf("permission gate status = %d, want %d",recorder.Code,want)}
 }
 // Test setup must itself fail if the consumer role already has this grant;
 // otherwise it would mask a broken bridge.
 granted,err:=app.Models.RolePermission.RoleHasPermission(ctx,consumer.String(),capability)
 if err!=nil||granted{t.Fatal("test database must have the canonical consumer permission boundary")}
 // Taxonomy is Platform data; this context intentionally has no merchant
 // selector, matching the actual category request from the browser.
 catalogContext:=func(actor uuid.UUID)context.Context{
  c:=context.WithValue(ctx,ctxUserID,actor)
  c=context.WithValue(c,ctxRoleID,principalRole.String())
  c=context.WithValue(c,ctxRoleName,"pt_"+principalRole.String())
  return context.WithValue(c,ctxAuthzState,newAuthzState(principalRole))
 }
 granted,err=app.Models.RolePermission.RoleHasPermission(ctx,consumer.String(),catalogCapability)
 if err!=nil||granted{t.Fatal("test database must not grant category listing to consumers")}
 checkGate(catalogContext(owner),catalogCapability,"/api/v1/future-offering-categories",http.StatusNoContent)
 checkGate(catalogContext(other),catalogCapability,"/api/v1/future-offering-categories",http.StatusForbidden)
 // The same absence of a selector must not authorize merchant-scoped FO data.
 checkGate(catalogContext(owner),capability,"/api/v1/future-offerings/?limit=24",http.StatusForbidden)
 wrongCatalog:=context.WithValue(catalogContext(owner),ctxMerchantID,uuid.New())
 // A selected merchant is irrelevant to shared taxonomy, while the actor
 // must still own an active account. Merchant-scoped reads remain denied.
 checkGate(wrongCatalog,catalogCapability,"/api/v1/future-offering-categories",http.StatusNoContent)
 checkGate(wrongCatalog,capability,"/api/v1/future-offerings/?limit=24",http.StatusForbidden)
 for _,permission:=range []string{"create_category","update_category","soft_delete_category","list_merchant_invoices"}{
  if app.HasPermission(catalogContext(owner),permission){t.Fatalf("unlisted capability %s authorized",permission)}
 }
 if !app.HasPermission(requestContext(owner),capability){t.Fatal("active consumer principal denied")}
 if app.HasPermission(requestContext(other),capability){t.Fatal("unrelated principal authorized")}
 independent:=context.WithValue(requestContext(owner),ctxRoleID,principalRole.String())
 independent=context.WithValue(independent,ctxRoleName,"pt_"+principalRole.String())
 independent=context.WithValue(independent,ctxAuthzState,newAuthzState(principalRole))
 checkGate(independent,capability,"/api/v1/future-offerings/?limit=24",http.StatusNoContent)
 if !app.HasPermission(independent,capability){t.Fatal("active non-consumer principal denied")}
 wrongMerchant:=context.WithValue(requestContext(owner),ctxMerchantID,uuid.New())
 checkGate(wrongMerchant,capability,"/api/v1/future-offerings/?limit=24",http.StatusForbidden)
 if app.HasPermission(wrongMerchant,capability){t.Fatal("header selected an unowned merchant")}
 missingMerchant:=context.WithValue(requestContext(owner),ctxMerchantID,uuid.Nil)
 if app.HasPermission(missingMerchant,capability){t.Fatal("missing merchant selection authorized")}
 for _,name:=range []string{"consumer","merchant","admin","super_admin","root_super_admin"}{
  c:=context.WithValue(requestContext(owner),ctxRoleName,name)
  if !app.hasMerchantPrincipalCapability(c,capability){t.Fatalf("principal helper denied role %s",name)}
  if app.hasMerchantPrincipalCapability(c,"update_user_role"){t.Fatalf("unlisted capability allowed for %s",name)}
 }
 exec(`UPDATE merchant_accounts SET account_status='suspended' WHERE id=$1`,account)
 // An already resolved result is reused only within its original request;
 // the next request must observe suspension.
 if !app.HasPermission(independent,capability){t.Fatal("request-scoped permission result was not reused")}
 fresh:=context.WithValue(independent,ctxAuthzState,newAuthzState(principalRole))
 checkGate(fresh,capability,"/api/v1/future-offerings/?limit=24",http.StatusForbidden)
 if app.HasPermission(fresh,capability){t.Fatal("suspended non-consumer principal authorized on new request")}

 if app.HasPermission(requestContext(owner),capability){t.Fatal("suspended principal authorized on new request")}
 checkGate(catalogContext(owner),catalogCapability,"/api/v1/future-offering-categories",http.StatusForbidden)
 for _,status:=range []string{"pending","closed"}{
  exec(`UPDATE merchant_accounts SET account_status=$2 WHERE id=$1`,account,status)
  checkGate(catalogContext(owner),catalogCapability,"/api/v1/future-offering-categories",http.StatusForbidden)
 }
 exec(`UPDATE merchant_accounts SET account_status='active',deleted_at=NOW() WHERE id=$1`,account)
 checkGate(catalogContext(owner),catalogCapability,"/api/v1/future-offering-categories",http.StatusForbidden)
 exec(`UPDATE merchant_accounts SET deleted_at=NULL WHERE id=$1`,account)
 exec(`UPDATE merchants SET deleted_at=NOW() WHERE id=$1`,merchant)
 checkGate(catalogContext(owner),catalogCapability,"/api/v1/future-offering-categories",http.StatusForbidden)

}
