// Package main provides HTTP handlers for the Platform API.
//
// focodebase/fobackend/internal/server/cmd/api/user_profiles.go
//
// GTM:
//
//	Layer: 2.2 Identity / Auth Domain
//	Release Class: SPINE
//	Reason:
//	  User profile HTTP handlers are release-critical identity and account
//	  lifecycle infrastructure. They provide authenticated self-service profile
//	  access, controlled internal profile administration, canonical user-handle
//	  validation, normalized related-data reads, profile moderation, reserved-
//	  handle governance, and soft-delete lifecycle operations required by the
//	  initial Platform release spine.
//
// SPINE Rule:
//
//	Keep compiling.
//	Keep production-ready.
//	Preserve authenticated self-profile access boundaries.
//	Preserve separation between self-service and internal profile operations.
//	Preserve canonical user-handle validation and normalization.
//	Preserve normalized social-link and notification-preference ownership;
//	do not persist those relations through the canonical user_profiles row.
//	Preserve profile moderation and moderation-log behavior.
//	Preserve sensitive-field redaction for non-privileged viewers.
//	Preserve database-owned lifecycle timestamps.
//	Do not introduce handler-local service registries or unsupported workflow
//	dependencies.
//	Do not expose batch moderation automation until its canonical service
//	contract, transaction boundary, policy, observability, and application
//	wiring exist.
//	Block deployment if this file breaks build, profile access control,
//	profile persistence, handle integrity, moderation integrity,
//	sensitive-data redaction, or identity/account lifecycle behavior.
// CE Foundation 3 HTTP adapters: trusted ownership, strict bodies and safe errors.
package main
import (
 "encoding/json"
 "errors"
 "io"
 "net/http"
 "strings"
 "time"
 "github.com/PiccoloMondoC/focodebase/fobackend/internal/data"
 "github.com/PiccoloMondoC/focodebase/fobackend/internal/mediastore"
 "github.com/PiccoloMondoC/focodebase/fobackend/internal/services"
 "github.com/google/uuid"
)
// profileBody rejects duplicate/unknown keys, non-string values, null root,
// trailing JSON and invalid preconditions. JSON null clears optional fields.
func decodeProfileBody(r io.Reader,create bool)(data.ProfileChanges,*time.Time,error){
 decoder:=json.NewDecoder(r)
 token,err:=decoder.Token();if err!=nil||token!=json.Delim('{'){return nil,nil,data.ErrUserProfileInvalidInput}
 changes:=data.ProfileChanges{};seen:=map[string]bool{};known:=map[string]bool{};for _,f:=range data.ProfileFieldNames(){known[f]=true}
 var expected *time.Time
 for decoder.More(){
  token,err:=decoder.Token();if err!=nil{return nil,nil,err};name,ok:=token.(string);if !ok||seen[name]{return nil,nil,data.ErrUserProfileInvalidInput};seen[name]=true
  var raw json.RawMessage;if err=decoder.Decode(&raw);err!=nil{return nil,nil,err}
  if name=="expected_updated_at" {
   if create{return nil,nil,data.ErrUserProfileInvalidInput}
   var text string;if err=json.Unmarshal(raw,&text);err!=nil||text==""{return nil,nil,data.ErrUserProfileInvalidInput}
   parsed,err:=time.Parse(time.RFC3339Nano,text);if err!=nil{return nil,nil,data.ErrUserProfileInvalidInput};expected=&parsed;continue
  }
  if !known[name]{return nil,nil,data.ErrUserProfileInvalidInput}
  var value *string;if err=json.Unmarshal(raw,&value);err!=nil{return nil,nil,data.ErrUserProfileInvalidInput};changes[name]=value
 }
 if _,err=decoder.Token();err!=nil{return nil,nil,err};var extra any;if err=decoder.Decode(&extra);err!=io.EOF{return nil,nil,data.ErrUserProfileInvalidInput}
 if !create&&expected==nil{return nil,nil,data.ErrProfileVersionRequired};return changes,expected,nil
}
func(app *Application)profileActor(w http.ResponseWriter,r *http.Request)(uuid.UUID,bool){
 w.Header().Set("Cache-Control","no-store")
 id:=app.getUserIDFromContext(r.Context());if id==nil||*id==uuid.Nil{app.respondWithErrorCode(w,401,"authentication_required","Please sign in.");return uuid.Nil,false};return *id,true
}
func(app *Application)profileError(w http.ResponseWriter,r *http.Request,err error){
 var fields data.ProfileFieldErrors
 if errors.As(err,&fields){app.respondWithJSON(w,400,jsonResponse{Error:true,Message:"Please check the highlighted details.",Data:map[string]any{"code":"validation_failed","fields":fields}});return}
 code,message,status:="profile_failed","Your profile could not be loaded or saved. Please reload before trying again.",500
 switch {
 case errors.Is(err,mediastore.ErrNotFound):code,message,status="photo_not_found","No saved profile photo is available.",404
 case errors.Is(err,mediastore.ErrUnavailable):code,message,status="media_unavailable","Your photo could not be loaded or saved. Please try again shortly.",503
 case errors.Is(err,data.ErrProfileVersionRequired):code,message,status="profile_version_required","Reload your profile before saving.",428
 case errors.Is(err,data.ErrProfileConflict):code,message,status="profile_edit_conflict","Your profile changed elsewhere. Load the latest version before saving.",409
 case errors.Is(err,data.ErrProfileExists):code,message,status="profile_already_exists","Your profile is already set up. Load it to continue.",409
 case errors.Is(err,data.ErrProfileUnavailable):code,message,status="profile_unavailable","Your profile is unavailable. Please contact support.",409
 case errors.Is(err,data.ErrProfileAccount):code,message,status="account_unavailable","Please sign in again to continue.",403
 case errors.Is(err,data.ErrUserHandleTaken):code,message,status="handle_taken","That handle is already taken.",409
 case errors.Is(err,data.ErrUserProfileNotFound):code,message,status="profile_not_found","Set up your profile to continue.",404
 case errors.Is(err,data.ErrUserProfileInvalidInput):code,message,status="invalid_request","Please check the profile information.",400
 default:
  // Do not log driver error text: constraint detail can contain personal data.
  app.Logger.GetLoggerWithContext(r).WithFunctionName("profileError").Error("profile operation failed")
 }
 app.respondWithErrorCode(w,status,code,message)
}
func(app *Application)GetOwnUserProfileHandler(w http.ResponseWriter,r *http.Request){
 id,ok:=app.profileActor(w,r);if !ok{return};value,err:=app.InternalServices.GetOwnProfileInternal(r.Context(),id);if err!=nil{app.profileError(w,r,err);return};app.respondWithJSON(w,200,jsonResponse{Data:value})
}
func(app *Application)saveOwnProfile(w http.ResponseWriter,r *http.Request,create bool){
 id,ok:=app.profileActor(w,r);if !ok{return}
 changes,expected,err:=decodeProfileBody(http.MaxBytesReader(w,r.Body,16384),create);if err!=nil{app.profileBodyError(w,r,err);return}
 value,err:=app.InternalServices.SaveOwnProfileInternal(r.Context(),id,changes,expected,create);if err!=nil{app.profileError(w,r,err);return}
 status:=200;if create{status=201};app.respondWithJSON(w,status,jsonResponse{Message:"Profile saved.",Data:value})
}
func(app *Application)CreateOwnUserProfileHandler(w http.ResponseWriter,r *http.Request){app.saveOwnProfile(w,r,true)}
func(app *Application)UpdateOwnUserProfileHandler(w http.ResponseWriter,r *http.Request){app.saveOwnProfile(w,r,false)}
func(app *Application)GetOwnProfileCompletenessHandler(w http.ResponseWriter,r *http.Request){
 id,ok:=app.profileActor(w,r);if !ok{return};a,err:=app.InternalServices.AssessOwnProfileCompletenessInternal(r.Context(),id);if err!=nil{app.profileError(w,r,err);return};app.respondWithJSON(w,200,jsonResponse{Data:a})
}
func(app *Application)ProfileHandleAvailabilityHandler(w http.ResponseWriter,r *http.Request){
 id,ok:=app.profileActor(w,r);if !ok{return};handle:=strings.ToLower(strings.TrimSpace(r.URL.Query().Get("handle")))
 status,err:=app.InternalServices.ProfileHandleAvailabilityInternal(r.Context(),id,handle);if err!=nil{app.profileError(w,r,err);return};app.respondWithJSON(w,200,jsonResponse{Data:map[string]string{"status":status}})
}
func(app *Application)profileAdmin(w http.ResponseWriter,r *http.Request,permission string)(uuid.UUID,bool){
 id,ok:=app.profileActor(w,r);if !ok{return uuid.Nil,false}
 if !app.RequireInternalPermission(r.Context(),permission){app.respondWithErrorCode(w,403,"permission_denied","You do not have access to this operation.");return uuid.Nil,false};return id,true
}
func(app *Application)resolveProfileTargetUserID(r *http.Request)*uuid.UUID{
 // Explicit target text wins validation; malformed input cannot silently act
 // on a different context target. A normal user's target remains their self.
 if !isInternalRole(getRoleFromContext(r.Context())){return app.getUserIDFromContext(r.Context())}
 raw:=strings.TrimSpace(r.Header.Get("X-Target-User-ID"));if raw==""{raw=strings.TrimSpace(r.URL.Query().Get("user_id"))}
 if raw!=""{id,err:=uuid.Parse(raw);if err!=nil||id==uuid.Nil{return nil};return &id}
 return app.getTargetUserIDFromContext(r.Context())
}
func(app *Application)GetUserProfileByUserIDHandler(w http.ResponseWriter,r *http.Request){
 actor,ok:=app.profileActor(w,r);if !ok{return};target:=app.resolveProfileTargetUserID(r);if target==nil{app.respondWithErrorCode(w,400,"target_required","Choose an account.");return}
 if actor==*target{app.GetOwnUserProfileHandler(w,r);return}
 if _,ok=app.profileAdmin(w,r,"read_user_profile");!ok{return}
 result,err:=app.InternalServices.ReadProfileAdministrationInternal(r.Context(),actor,*target);if err!=nil{app.profileError(w,r,err);return};app.respondWithJSON(w,200,jsonResponse{Data:result})
}
func(app *Application)ListUserProfilesHandler(w http.ResponseWriter,r *http.Request){app.listProfiles(w,r,false)}
func(app *Application)SearchUserProfilesHandler(w http.ResponseWriter,r *http.Request){app.listProfiles(w,r,true)}
func(app *Application)listProfiles(w http.ResponseWriter,r *http.Request,search bool){
 actor,ok:=app.profileAdmin(w,r,"read_user_profiles");if !ok{return}
 limit,offset,err:=app.parseLimitOffset(r);if err!=nil{app.profileError(w,r,data.ErrUserProfileInvalidInput);return}
 q:="";if search{q=strings.TrimSpace(r.URL.Query().Get("q"));if q==""||len([]rune(q))>200{app.profileError(w,r,data.ErrUserProfileInvalidInput);return}}
 result,err:=app.InternalServices.ListProfileAdministrationInternal(r.Context(),actor,q,limit,offset);if err!=nil{app.profileError(w,r,err);return};app.respondWithJSON(w,200,jsonResponse{Data:result})
}
func(app *Application)UpdateUserProfileHandler(w http.ResponseWriter,r *http.Request){
 actor,ok:=app.profileAdmin(w,r,"update_user_profile");if !ok{return};target:=app.resolveProfileTargetUserID(r);if target==nil{app.respondWithErrorCode(w,400,"target_required","Choose an account.");return}
 changes,expected,err:=decodeProfileBody(http.MaxBytesReader(w,r.Body,16384),false);if err!=nil{app.profileBodyError(w,r,err);return}
 result,err:=app.InternalServices.PatchProfileAdministrativeInternal(r.Context(),actor,*target,changes,expected);if err!=nil{app.respondWithProfileAdministrationError(w,r,err);return};app.respondWithJSON(w,200,jsonResponse{Message:"Profile saved.",Data:result})
}
func(app *Application)ModerateUserProfileHandler(w http.ResponseWriter,r *http.Request){
 actor,ok:=app.profileAdmin(w,r,"moderate_user_profile");if !ok{return};target:=app.resolveProfileTargetUserID(r);if target==nil{app.respondWithErrorCode(w,400,"target_required","Choose an account.");return}
 var input struct{Flagged *bool `json:"is_flagged"`;Notes string `json:"notes"`}
 if err:=app.readJSON(w,r,&input);err!=nil||input.Flagged==nil||len([]rune(input.Notes))>4000{app.profileError(w,r,data.ErrUserProfileInvalidInput);return}
 err:=app.InternalServices.ModerateProfileAdministrativeInternal(r.Context(),actor,*target,*input.Flagged,input.Notes);if err!=nil{app.respondWithProfileAdministrationError(w,r,err);return};app.respondWithJSON(w,200,jsonResponse{Message:"Profile moderation updated."})
}
func(app *Application)GetReservedHandlesHandler(w http.ResponseWriter,r *http.Request){
 actor,ok:=app.profileAdmin(w,r,"read_reserved_handles");if !ok{return};value,err:=app.InternalServices.ReservedProfileHandlesInternal(r.Context(),actor);if err!=nil{app.profileError(w,r,err);return};app.respondWithJSON(w,200,jsonResponse{Data:value})
}
func(app *Application)respondWithProfileAdministrationError(w http.ResponseWriter,r *http.Request,err error){
 if errors.Is(err,data.ErrUserProfileInvalidInput)||errors.Is(err,data.ErrUserProfileNotFound)||errors.Is(err,data.ErrProfileConflict)||errors.Is(err,data.ErrProfileVersionRequired)||errors.Is(err,data.ErrUserHandleTaken)||errors.Is(err,data.ErrProfileUnavailable)||errors.Is(err,data.ErrProfileAccount){app.profileError(w,r,err);return}
 app.respondWithGovernanceError(w,r,"profile_administration",err)
}

func(app *Application)profileBodyError(w http.ResponseWriter,r *http.Request,err error){
 var tooLarge *http.MaxBytesError
 if errors.As(err,&tooLarge){app.respondWithErrorCode(w,413,"request_too_large","The profile request is too large.");return}
 if !errors.Is(err,data.ErrProfileVersionRequired){err=data.ErrUserProfileInvalidInput}
 app.profileError(w,r,err)
}

func(app *Application)profileServiceContext(next http.Handler)http.Handler{
 return http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
  if id:=app.getUserIDFromContext(r.Context());id!=nil{r=r.WithContext(services.WithProfileActorContext(r.Context(),*id))}
  next.ServeHTTP(w,r)
 })
}

func(app *Application)GetOwnPublicIdentityHandler(w http.ResponseWriter,r *http.Request){
 id,ok:=app.profileActor(w,r);if !ok{return}
 result,err:=app.InternalServices.GetOwnPublicIdentityInternal(r.Context(),id);if err!=nil{app.profileError(w,r,err);return}
 app.respondWithJSON(w,200,jsonResponse{Data:result})
}
