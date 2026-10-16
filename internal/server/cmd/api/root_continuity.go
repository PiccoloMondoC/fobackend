// focodebase/fobackend/internal/server/cmd/api/root_continuity.go
// Authenticated, rate-limited continuity transport. No recovery executor route.
package main
import (
 "context"
 "errors"
 "net/http"
 "github.com/PiccoloMondoC/focodebase/fobackend/internal/data"
 "github.com/PiccoloMondoC/focodebase/fobackend/internal/services"
 "github.com/google/uuid"
 "github.com/go-chi/chi/v5"
)
func (app *Application) rootContinuityError(w http.ResponseWriter,err error) {
 switch {
 case errors.Is(err,data.ErrContinuityForbidden):app.respondWithErrorCode(w,403,"insufficient_authority","you do not have authority for this ceremony")
 case errors.Is(err,services.ErrGovernanceReauthenticationFailed):app.respondWithErrorCode(w,400,"reauthentication_failed","confirm your current password")
 case errors.Is(err,services.ErrGovernanceInputInvalid),errors.Is(err,services.ErrGovernanceSelfTarget):app.respondWithErrorCode(w,400,"invalid_request","check the ceremony details and confirmation address")
 case errors.Is(err,data.ErrContinuityConflict),errors.Is(err,data.ErrConcurrentGovernanceChange):app.respondWithErrorCode(w,409,"continuity_changed","the ceremony is unavailable or state changed; refresh before proceeding")
 default:app.Logger.Error("Root continuity ceremony failed", "error", err, "code", "server_error");app.respondWithErrorCode(w,500,"outcome_unconfirmed","the outcome could not be confirmed; refresh the current state before proceeding")
 }
}
func (app *Application) GetRootContinuityHandler(w http.ResponseWriter,r *http.Request) {
 ctx,cancel:=context.WithTimeout(r.Context(),cfgTimeout);defer cancel()
 actor,ok:=app.governanceActor(w,ctx);if !ok{return}
 view,err:=app.InternalServices.ReadRootContinuityInternal(ctx,actor);if err!=nil{app.rootContinuityError(w,err);return}
 w.Header().Set("Cache-Control","no-store")
 app.respondWithJSON(w,200,jsonResponse{Error:false,Message:"Root continuity",Data:view})
}
func (app *Application) ExecuteRootContinuityHandler(w http.ResponseWriter,r *http.Request) {
 ctx,cancel:=context.WithTimeout(r.Context(),cfgTimeout);defer cancel()
 actor,ok:=app.governanceActor(w,ctx);if !ok{return}
 var request services.ContinuityRequest
 if app.readJSON(w,r,&request)!=nil{app.respondWithErrorCode(w,400,"invalid_request","invalid ceremony request");return}
 id,err:=app.InternalServices.ExecuteRootContinuityInternal(ctx,actor,request);request.CurrentPassword=""
 if err!=nil{app.rootContinuityError(w,err);return}
 w.Header().Set("Cache-Control","no-store")
 app.respondWithJSON(w,200,jsonResponse{Error:false,Message:"Ceremony recorded. Refresh to see current standing.",Data:map[string]any{"ceremony_id":id}})
}
func (app *Application) ConsultingHandler(w http.ResponseWriter,r *http.Request) {
 ctx,cancel:=context.WithTimeout(r.Context(),cfgTimeout);defer cancel()
 actor,ok:=app.governanceActor(w,ctx);if !ok{return}
 capability:=chi.URLParam(r,"capability");target:=uuid.Nil
 if raw:=chi.URLParam(r,"targetID");raw!="" {var err error;target,err=uuid.Parse(raw);if err!=nil{app.respondWithErrorCode(w,400,"invalid_request","invalid account ID");return}}
 result,err:=app.InternalServices.ReadConsultingInternal(ctx,actor,capability,target);if err!=nil{app.rootContinuityError(w,err);return}
 w.Header().Set("Cache-Control","no-store")
 app.respondWithJSON(w,200,jsonResponse{Error:false,Message:"Consulting workspace",Data:result})
}
