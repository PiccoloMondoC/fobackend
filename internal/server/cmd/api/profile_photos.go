// Package main provides owner-only profile photo HTTP endpoints with bounded request bodies.
// focodebase/fobackend/internal/server/cmd/api/profile_photos.go

package main
import (
 "io"
 "net/http"
 "time"
 "github.com/PiccoloMondoC/focodebase/fobackend/internal/data"
 "github.com/PiccoloMondoC/focodebase/fobackend/internal/services"
)
func(app *Application)GetOwnProfilePhotoHandler(w http.ResponseWriter,r *http.Request){
 id,ok:=app.profileActor(w,r);if !ok{return}
 raw,err:=app.InternalServices.GetOwnProfilePhotoInternal(r.Context(),id);if err!=nil{app.profileError(w,r,err);return}
 w.Header().Set("Content-Type","image/jpeg");w.Header().Set("X-Content-Type-Options","nosniff")
 w.Header().Set("Content-Disposition",`inline; filename="profile-photo.jpg"`)
 w.WriteHeader(http.StatusOK);if _,err=w.Write(raw);err!=nil{app.Logger.Warn("profile photo response interrupted")}
}
func(app *Application)SaveOwnProfilePhotoHandler(w http.ResponseWriter,r *http.Request){app.changeOwnProfilePhoto(w,r,false)}
func(app *Application)DeleteOwnProfilePhotoHandler(w http.ResponseWriter,r *http.Request){app.changeOwnProfilePhoto(w,r,true)}
func(app *Application)changeOwnProfilePhoto(w http.ResponseWriter,r *http.Request,remove bool){
 id,ok:=app.profileActor(w,r);if !ok{return}
 text:=r.URL.Query().Get("expected_updated_at")
 if text==""{app.profileError(w,r,data.ErrProfileVersionRequired);return}
 expected,err:=time.Parse(time.RFC3339Nano,text);if err!=nil{app.profileError(w,r,data.ErrUserProfileInvalidInput);return}
 var raw []byte
 if !remove{raw,err=io.ReadAll(http.MaxBytesReader(w,r.Body,services.ProfilePhotoMaxBytes));if err!=nil{app.profileBodyError(w,r,err);return}}
 result,err:=app.InternalServices.SaveOwnProfilePhotoInternal(r.Context(),id,raw,&expected,remove);if err!=nil{app.profileError(w,r,err);return}
 app.respondWithJSON(w,200,jsonResponse{Message:"Profile photo updated.",Data:result})
}
