// CE HTTP boundary. Availability enforcement never constructs domain services.
//
// focodebase/fobackend/internal/server/cmd/api/site_maintenance.go
package main

import (
 "context"
 "encoding/json"
 "net/http"

 "github.com/PiccoloMondoC/focodebase/fobackend/internal/data"
 "github.com/PiccoloMondoC/focodebase/fobackend/internal/services"
)

// Exact method/path exceptions are capabilities, never a role-based bypass.
// Recovery authentication deliberately keeps password reset reachable. Every
// exchange retains its existing rate limits, token lifetimes and one-use rules.
func maintenanceEssential(r *http.Request) bool {
 if r.URL.RawPath!="" {return false}
 switch r.Method+" "+r.URL.Path {
 case "GET /api/v1/site-configuration", "GET /api/v1/site-maintenance/status",
 "POST /api/v1/user/login", "POST /api/v1/user/token/refresh", "POST /api/v1/user/logout",
 "POST /api/v1/user/password/forgot", "POST /api/v1/user/password/reset",
 "GET /api/v1/site-maintenance", "PUT /api/v1/site-maintenance/announcement",
 "POST /api/v1/site-maintenance/announcement/cancel", "POST /api/v1/site-maintenance/activate",
 "POST /api/v1/site-maintenance/restore":return true
 default:return false
 }
}
func maintenanceUnavailable(w http.ResponseWriter,active bool) {
 code,message:="service_unavailable","Service temporarily unavailable."
 if active{code="site_maintenance_active";message="The platform is temporarily unavailable for maintenance."}
 w.Header().Set("Cache-Control","no-store")
 w.Header().Set("Retry-After","60")
 w.Header().Set("Content-Type","application/json")
 w.WriteHeader(http.StatusServiceUnavailable)
 _=json.NewEncoder(w).Encode(jsonResponse{Error:true,Message:message,Data:apiErrorDetail{Code:code}})
}
// The observation is one primary-database statement for each admission. Already
// admitted requests may complete. No stale online state, timers or process cache.
func maintenanceGate(read func(context.Context)(*data.MaintenanceState,error)) func(http.Handler)http.Handler {
 return func(next http.Handler)http.Handler{return http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
  if maintenanceEssential(r){w.Header().Set("Cache-Control","no-store");next.ServeHTTP(w,r);return}
  state,err:=read(r.Context())
  if err!=nil || state==nil {maintenanceUnavailable(w,false);return}
  if state.Active{maintenanceUnavailable(w,true);return}
  next.ServeHTTP(w,r)
 })}
}
func (app *Application) SiteMaintenanceGateMiddleware(next http.Handler) http.Handler {
 return maintenanceGate(app.InternalServices.PublicMaintenanceInternal)(next)
}
func (app *Application) PublicMaintenanceHandler(w http.ResponseWriter,r *http.Request) {
 state,err:=app.InternalServices.PublicMaintenanceInternal(r.Context())
 if err!=nil{maintenanceUnavailable(w,false);return}
 w.Header().Set("Cache-Control","no-store")
 app.respondWithJSON(w,http.StatusOK,jsonResponse{Error:false,Message:"Site availability",Data:state.Public()})
}
func maintenanceHTTPError(err error)(int,string,string) {
 code:=services.MaintenanceErrorCode(err)
 switch code {
 case "invalid_request":return 400,code,"Check the maintenance details and try again."
 case "insufficient_authority":return 403,code,"Root Super Admin authority is required."
 case "reauthentication_failed":return 403,code,"Your password was not accepted."
 case "reauthentication_unavailable":return 409,code,"This action requires an account password."
 case "maintenance_state_changed","concurrent_change":return 409,code,"Maintenance details changed. Reload and review them before trying again."
 case "invalid_transition":return 409,code,"That action does not apply to the current maintenance state."
 default:return 500,"outcome_unconfirmed","The outcome could not be confirmed. Reload the current state before proceeding."
 }
}
func (app *Application) maintenanceResponse(w http.ResponseWriter,r *http.Request,view *services.MaintenanceWorkspace,err error) {
 w.Header().Set("Cache-Control","no-store")
 if err!=nil {
  status,code,message:=maintenanceHTTPError(err)
  // Log a classification, not request bodies, passwords, reasons, SQL or PII.
  if status>=500 {app.Logger.Error("maintenance outcome unconfirmed","code",code)}
  app.respondWithErrorCode(w,status,code,message);return
 }
 app.respondWithJSON(w,http.StatusOK,jsonResponse{Error:false,Message:"Maintenance details",Data:view})
}
func (app *Application) MaintenanceWorkspaceHandler(w http.ResponseWriter,r *http.Request) {
 ctx,cancel:=context.WithTimeout(r.Context(),cfgTimeout);defer cancel()
 actor,ok:=app.governanceActor(w,ctx);if !ok{return}
 view,err:=app.InternalServices.ReadMaintenanceInternal(ctx,actor);app.maintenanceResponse(w,r,view,err)
}
func (app *Application) maintenanceCommandHandler(op string) http.HandlerFunc {
 return func(w http.ResponseWriter,r *http.Request){
  w.Header().Set("Cache-Control","no-store")
  ctx,cancel:=context.WithTimeout(r.Context(),cfgTimeout);defer cancel()
  actor,ok:=app.governanceActor(w,ctx);if !ok{return}
  var c services.MaintenanceCommand
  if err:=app.readJSON(w,r,&c);err!=nil{app.respondWithErrorCode(w,400,"invalid_request","Invalid request body.");return}
  c.ActorID=actor;c.Operation=op
  view,err:=app.InternalServices.ExecuteMaintenanceInternal(ctx,c);c.CurrentPassword=""
  app.maintenanceResponse(w,r,view,err)
 }
}
