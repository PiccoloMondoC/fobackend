// BUSINESS-NEUTRAL SOURCE RULE — BEG 3.17–3.18
// CE and engineering own this naming rule. Use platform for neutral internal
// identifiers; use the platform in general UI prose. Capitalize Platform only
// where a heading, sentence position, or defined label requires it. When naming
// an installation's website or business, use its administrator-configured identity.
// These choices must be implemented and reviewed in source, not delegated to the
// project owner as editing guidance.
// Do not hardcode an installation's brand in identifiers, CSS tokens, public
// copy, defaults, or seed data. Public identity and business wording must come
// from administrator-managed configuration. Internal names describe purpose.
// Pre-startup call-site compatibility is not justification for weak naming.
// Repair dependent callers together. An exception requires a documented,
// intentional public, production, or migration-stable contract and CE review.
// Run tools/check_business_neutrality.py before source release. Existing
// violations are not automatically grandfathered or approved.

//
// internal/server/cmd/api/site_identity.go
// CE: presentation-only public DTO; privileged reads and replacements use the
// complete internal service boundary. No database writes occur in handlers.
package main
import (
 "errors"
 "net/http"
 "strconv"
 "github.com/jackc/pgx/v5/pgconn"
 "github.com/PiccoloMondoC/focodebase/fobackend/internal/data"
 "github.com/PiccoloMondoC/focodebase/fobackend/internal/services"
)
func (app *Application) siteIdentityError(w http.ResponseWriter,r *http.Request,err error){
 switch {
 case errors.Is(err,data.ErrSiteIdentityInvalidInput):app.respondWithErrorCode(w,400,"invalid_site_identity","Check the supplied wording and field lengths.")
 case errors.Is(err,data.ErrSiteIdentityVersionConflict):app.respondWithErrorCode(w,409,"site_identity_version_conflict","The configuration changed. Reload before saving.")
 case errors.Is(err,data.ErrSiteIdentityNotFound):app.respondWithErrorCode(w,503,"site_identity_unavailable","Site identity is temporarily unavailable.")
 default:
  var databaseError *pgconn.PgError
  if errors.As(err,&databaseError) {err=errors.New("site identity database operation failed")}
  app.respondWithGovernanceError(w,r,"site_identity",err)
 }
}
func (app *Application) GetPublicSiteConfigurationHandler(w http.ResponseWriter,r *http.Request){
 config,err:=app.InternalServices.PublicSiteConfigurationInternal(r.Context())
 if err!=nil{
  // Driver errors can include constraint DETAIL containing submitted values.
  // Public logs deliberately carry no driver error or presentation content.
  app.Logger.GetLoggerWithContext(r).WithFunctionName("GetPublicSiteConfigurationHandler").Error("Public site configuration unavailable")
  app.respondWithErrorCode(w,503,"site_configuration_unavailable","Site configuration is temporarily unavailable.");return
 }
 // One cache layer owns freshness: Node SSR. Browser/shared HTTP caches must
 // revalidate, so cache age does not accumulate across two independent TTLs.
 w.Header().Set("Cache-Control","no-store")
 app.respondWithJSON(w,200,jsonResponse{Error:false,Message:"Site configuration retrieved",Data:config})
}
func (app *Application) GetSiteIdentityHandler(w http.ResponseWriter,r *http.Request){
 w.Header().Set("Cache-Control","no-store")
 actor,ok:=app.governanceActor(w,r.Context());if !ok{return}
 config,err:=app.InternalServices.SiteIdentityAdministrationInternal(r.Context(),actor)
 if err!=nil{app.siteIdentityError(w,r,err);return}
 app.respondWithJSON(w,200,jsonResponse{Error:false,Message:"Site identity retrieved",Data:config})
}
func (app *Application) GetSiteIdentityHistoryHandler(w http.ResponseWriter,r *http.Request){
 w.Header().Set("Cache-Control","no-store")
 actor,ok:=app.governanceActor(w,r.Context());if !ok{return}
 limit:=50
 if raw:=r.URL.Query().Get("limit");raw!=""{v,err:=strconv.Atoi(raw);if err!=nil||v<1||v>100{app.respondWithErrorCode(w,400,"invalid_request","Limit must be between 1 and 100.");return};limit=v}
 entries,err:=app.InternalServices.SiteIdentityHistoryInternal(r.Context(),actor,limit)
 if err!=nil{app.siteIdentityError(w,r,err);return}
 app.respondWithJSON(w,200,jsonResponse{Error:false,Message:"Site identity history retrieved",Data:entries})
}
func (app *Application) ReplaceSiteIdentityHandler(w http.ResponseWriter,r *http.Request){
 w.Header().Set("Cache-Control","no-store")
 actor,ok:=app.governanceActor(w,r.Context());if !ok{return}
 // Pointers distinguish omission/null from intentionally blank optional text.
 var input struct{
  Identity *struct{
   SiteName *string `json:"site_name"`;Tagline *string `json:"tagline"`
   Description *string `json:"description"`;CopyrightHolder *string `json:"copyright_holder"`
  } `json:"identity"`
  Terms map[string]string `json:"terms"`
  ExpectedVersion int64 `json:"expected_version"`
  ChangeReason string `json:"change_reason"`
 }
 if err:=app.readJSON(w,r,&input);err!=nil{app.respondWithErrorCode(w,400,"invalid_request","Invalid request body.");return}
 i:=input.Identity
 if i==nil||i.SiteName==nil||i.Tagline==nil||i.Description==nil||i.CopyrightHolder==nil||input.Terms==nil{app.respondWithErrorCode(w,400,"invalid_request","Supply the complete identity and terminology.");return}
 result,err:=app.InternalServices.ReplaceSiteIdentityInternal(r.Context(),services.SiteIdentityChange{
  ActorID:actor,ExpectedVersion:input.ExpectedVersion,Reason:input.ChangeReason,
  Presentation:data.SitePresentation{Identity:data.SiteIdentityText{SiteName:*i.SiteName,Tagline:*i.Tagline,Description:*i.Description,CopyrightHolder:*i.CopyrightHolder},Terms:input.Terms},
 })
 if err!=nil{app.siteIdentityError(w,r,err);return}
 app.respondWithJSON(w,200,jsonResponse{Error:false,Message:"Site identity saved",Data:result})
}
