// focodebase/fobackend/internal/server/cmd/api/site_maintenance_test.go
//
//
package main

import (
 "context"
 "errors"
 "net/http"
 "net/http/httptest"
 "strings"
 "testing"
 "github.com/PiccoloMondoC/focodebase/fobackend/internal/data"
 "github.com/PiccoloMondoC/focodebase/fobackend/internal/services"
)
func TestCEMaintenanceAdmissionAndFailure(t *testing.T){
 active:=false;failed:=false;reads:=0;calls:=0
 gate:=maintenanceGate(func(context.Context)(*data.MaintenanceState,error){reads++;if failed{return nil,errors.New("database unavailable")};return &data.MaintenanceState{MaintenanceDocument:data.MaintenanceDocument{Active:active}},nil})(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){calls++;w.WriteHeader(204)}))
 check:=func(path string,want int,code string){t.Helper();w:=httptest.NewRecorder();gate.ServeHTTP(w,httptest.NewRequest("GET",path,nil));if w.Code!=want{t.Fatalf("%s: %d",path,w.Code)};if code!="" && (!strings.Contains(w.Body.String(),code)||w.Header().Get("Cache-Control")!="no-store"||w.Header().Get("Retry-After")==""){t.Fatal("missing maintenance/error contract")}}
 check("/api/v1/future-offerings/",204,"")
 active=true;check("/api/v1/future-offerings/",503,"site_maintenance_active")
 active=false;failed=true;check("/api/v1/future-offerings/",503,"service_unavailable")
 failed=false;check("/api/v1/future-offerings/",204,"")
 if reads!=4 || calls!=2{t.Fatal("admission cached an earlier observation")}
}
func TestCEMaintenanceExceptionsPreserveAuthentication(t *testing.T){
 gate:=maintenanceGate(func(context.Context)(*data.MaintenanceState,error){return &data.MaintenanceState{MaintenanceDocument:data.MaintenanceDocument{Active:true}},nil})(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){w.WriteHeader(http.StatusUnauthorized)}))
 for _,path:=range []string{"/api/v1/site-maintenance","/api/v1/site-maintenance/status","/api/v1/site-configuration"}{w:=httptest.NewRecorder();gate.ServeHTTP(w,httptest.NewRequest("GET",path,nil));if w.Code!=401{t.Fatal("exception replaced downstream auth semantics")}}
 for _,path:=range []string{"/api/v1/admin-console/","/api/v1/site-maintenance/extra","/api/v1/site-maintenance/","/api/v1/%73ite-maintenance"}{w:=httptest.NewRecorder();gate.ServeHTTP(w,httptest.NewRequest("GET",path,nil));if w.Code!=503{t.Fatalf("broad exception: %s",path)}}
 for _,path:=range []string{"/api/v1/user/login","/api/v1/user/token/refresh","/api/v1/user/logout","/api/v1/user/password/forgot","/api/v1/user/password/reset","/api/v1/site-maintenance/restore"}{if !maintenanceEssential(httptest.NewRequest("POST",path,nil)){t.Fatalf("continuity blocked: %s",path)}}
 if maintenanceEssential(httptest.NewRequest("DELETE","/api/v1/site-maintenance",nil)){t.Fatal("wrong method admitted")}
}
func TestCEMaintenanceErrorSemantics(t *testing.T){
 for _,err:=range []error{services.ErrMaintenanceInput,services.ErrGovernanceInsufficientAuthority,services.ErrGovernanceReauthenticationFailed,data.ErrMaintenanceConflict,services.ErrMaintenanceTransition,errors.New("unknown")}{status,_,_:=maintenanceHTTPError(err);if status==401{t.Fatal("domain failure reported as invalid session")}}
}
