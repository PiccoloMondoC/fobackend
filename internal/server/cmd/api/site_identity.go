// Package main provides account identity completion and administrative identity safeguards.
// focodebase/fobackend/internal/server/cmd/api/site_identity.go

package main
import (
 "context"
 "encoding/json"
 "errors"
 "net/http"
 "strconv"
 "github.com/google/uuid"
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
  app.Logger.GetLoggerWithContext(r).WithFunctionName("GetPublicSiteConfigurationHandler").Error("Public site configuration unavailable")
  app.respondWithErrorCode(w,503,"site_configuration_unavailable","Site configuration is temporarily unavailable.");return
 }
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
 // Authorization remains in the existing service. Add a minimized current
 // identity projection without changing the immutable changed_by association.
 encoded,err:=json.Marshal(entries);if err!=nil{app.siteIdentityError(w,r,err);return}
 var display []map[string]json.RawMessage
 if err=json.Unmarshal(encoded,&display);err!=nil{app.siteIdentityError(w,r,err);return}
 if display==nil{display=[]map[string]json.RawMessage{}}
 ids:=make([]uuid.UUID,0,len(display))
 for _,entry:=range display {
  var id uuid.UUID
  if err=json.Unmarshal(entry["changed_by"],&id);err!=nil || id==uuid.Nil{app.siteIdentityError(w,r,errors.New("invalid history actor reference"));return}
  ids=append(ids,id)
 }
 ctx,cancel:=context.WithTimeout(r.Context(),app.InternalServices.Cfg.DBTimeout);defer cancel()
 // Site history intentionally survives physical account deletion. Resolve
 // retained actors only; an absent account gets a neutral, non-ID label.
 retained:=[]uuid.UUID{}
 rows,err:=app.Models.DB.Query(ctx,`SELECT id FROM users WHERE id=ANY($1::uuid[])`,ids)
 if err!=nil{app.respondWithErrorCode(w,503,"site_identity_unavailable","Site identity history is temporarily unavailable.");return}
 for rows.Next(){var id uuid.UUID;if err=rows.Scan(&id);err!=nil{rows.Close();app.siteIdentityError(w,r,errors.New("history identity read failed"));return};retained=append(retained,id)}
 err=rows.Err();rows.Close();if err!=nil{app.siteIdentityError(w,r,errors.New("history identity read failed"));return}
 identities,err:=data.ReadAdministrativeIdentities(ctx,app.Models.DB,retained...)
 if err!=nil{app.respondWithErrorCode(w,503,"site_identity_unavailable","Site identity history is temporarily unavailable.");return}
 for i,entry:=range display {value,err:=json.Marshal(identities[ids[i]]);if err!=nil{app.siteIdentityError(w,r,err);return};entry["actor_identity"]=value}
 app.respondWithJSON(w,200,jsonResponse{Error:false,Message:"Site identity history retrieved",Data:display})
}
func (app *Application) ReplaceSiteIdentityHandler(w http.ResponseWriter,r *http.Request){
 w.Header().Set("Cache-Control","no-store")
 actor,ok:=app.governanceActor(w,r.Context());if !ok{return}
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
