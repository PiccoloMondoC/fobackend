// Package main exposes explicitly scoped offering publication and media boundaries.
// focodebase/fobackend/internal/server/cmd/api/future_offering_publication.go
// Public DTOs never include private project names, legal/contact identity,
// storage keys, ownership accounts or commercial records. Asset URLs are internal.
package main

import (
 "context"
 "encoding/json"
 "errors"
 "io"
 "net/http"
 "strconv"
 "fmt"
 "strings"
 "time"
 "github.com/PiccoloMondoC/focodebase/fobackend/internal/data"
 "github.com/PiccoloMondoC/focodebase/fobackend/internal/services"
 "github.com/PiccoloMondoC/focodebase/fobackend/internal/mediastore"
 "github.com/go-chi/chi/v5"
 "github.com/google/uuid"
)

const offeringUploadBodyLimit = 64 << 20

type offeringVersionRequest struct {
 ExpectedUpdatedAt time.Time `json:"expected_updated_at"`
 Reason string `json:"reason"`
 BrandVisible *bool `json:"brand_visible"`
 AltText string `json:"alt_text"`
 DisplayOrder int `json:"display_order"`
 Main bool `json:"is_main"`
}
func(app *Application) offeringScope(w http.ResponseWriter,r *http.Request)(uuid.UUID,uuid.UUID,bool){
 merchant,err:=app.requireAuthorizedMerchantFutureOfferingMerchant(r.Context());if err!=nil{app.respondMerchantFutureOfferingAuthorizationError(w,r,err);return uuid.Nil,uuid.Nil,false}
 id,ok:=app.parseFutureOfferingIDParam(w,r);return merchant,id,ok
}
func(app *Application) offeringFailure(w http.ResponseWriter,r *http.Request,err error){
 var large *http.MaxBytesError
 if errors.As(err,&large){app.respondWithErrorCode(w,413,"media_too_large","The media file exceeds the upload limit.");return}
 if errors.Is(err,mediastore.ErrUnavailable){app.respondWithErrorCode(w,503,"media_unavailable","Media service is temporarily unavailable. Please try again.");return}
 app.respondMerchantFutureOfferingServiceError(w,r,err)
}
// GetFutureOfferingMediaHandler returns the merchant's ordered gallery.
func(app *Application) GetFutureOfferingMediaHandler(w http.ResponseWriter,r *http.Request){
 merchant,id,ok:=app.offeringScope(w,r);if !ok{return}
 result,err:=app.InternalServices.GetFutureOfferingGallery(r.Context(),merchant,id);if err!=nil{app.offeringFailure(w,r,err);return}
 w.Header().Set("Cache-Control","no-store");app.respondWithJSON(w,200,jsonResponse{Data:result,Message:"Media retrieved"})
}
// UploadFutureOfferingMediaHandler bounds raw bytes before decode or staging.
// Binary requests avoid multipart temporary files and never accept client paths.
func(app *Application) UploadFutureOfferingMediaHandler(w http.ResponseWriter,r *http.Request){
 merchant,id,ok:=app.offeringScope(w,r);if !ok{return}
 expected,err:=time.Parse(time.RFC3339Nano,r.URL.Query().Get("expected_updated_at"));if err!=nil{app.respondWithErrorCode(w,400,"invalid_version","Reload the project before uploading.");return}
 actor:=app.getUserIDFromContext(r.Context());if actor==nil{app.respondWithErrorCode(w,401,"authentication_required","Sign in again.");return}
 r.Body=http.MaxBytesReader(w,r.Body,offeringUploadBodyLimit)
 result,err:=app.InternalServices.UploadFutureOfferingMedia(r.Context(),*actor,merchant,id,expected,r.URL.Query().Get("alt_text"),r.Body);if err!=nil{app.offeringFailure(w,r,err);return}
 app.respondWithJSON(w,201,jsonResponse{Data:result,Message:"Media saved"})
}
// EditFutureOfferingMediaHandler updates or removes one gallery reference.
func(app *Application) EditFutureOfferingMediaHandler(w http.ResponseWriter,r *http.Request){
 merchant,id,ok:=app.offeringScope(w,r);if !ok{return}
 asset,err:=uuid.Parse(chi.URLParam(r,"assetID"));if err!=nil{app.respondWithErrorCode(w,400,"invalid_media","Invalid media reference.");return}
 var body offeringVersionRequest;if err=app.readJSON(w,r,&body);err!=nil{app.respondWithErrorCode(w,400,"invalid_request","Invalid media change.");return}
 actor:=app.getUserIDFromContext(r.Context());if actor==nil{app.respondWithErrorCode(w,401,"authentication_required","Sign in again.");return}
 result,err:=app.InternalServices.EditFutureOfferingMedia(r.Context(),*actor,merchant,id,body.ExpectedUpdatedAt,data.FutureOfferingMedia{ID:asset,AltText:body.AltText,Order:body.DisplayOrder,Main:body.Main},r.Method==http.MethodDelete);if err!=nil{app.offeringFailure(w,r,err);return}
 app.respondWithJSON(w,200,jsonResponse{Data:result,Message:"Media saved"})
}
// ActivateFutureOfferingHandler invokes the canonical automated activation service.
func(app *Application) ActivateFutureOfferingHandler(w http.ResponseWriter,r *http.Request){
 merchant,id,ok:=app.offeringScope(w,r);if !ok{return};actor:=app.getUserIDFromContext(r.Context());if actor==nil{app.respondWithErrorCode(w,401,"authentication_required","Sign in again.");return}
 var body offeringVersionRequest;if err:=app.readJSON(w,r,&body);err!=nil{app.respondWithErrorCode(w,400,"invalid_request","Invalid activation request.");return}
 result,err:=app.InternalServices.ActivateFutureOffering(r.Context(),*actor,merchant,id,body.ExpectedUpdatedAt);if err!=nil{app.offeringFailure(w,r,err);return}
 app.respondWithJSON(w,200,jsonResponse{Data:newMerchantFutureOfferingResponse(result),Message:"Activated"})
}
// ChangeFutureOfferingPublicationHandler changes listing membership or closes an FO.
func(app *Application) ChangeFutureOfferingPublicationHandler(w http.ResponseWriter,r *http.Request){
 merchant,id,ok:=app.offeringScope(w,r);if !ok{return};actor:=app.getUserIDFromContext(r.Context());if actor==nil{app.respondWithErrorCode(w,401,"authentication_required","Sign in again.");return}
 var body offeringVersionRequest;if err:=app.readJSON(w,r,&body);err!=nil{app.respondWithErrorCode(w,400,"invalid_request","Invalid publication change.");return}
 var result *data.MerchantFutureOffering;var err error
 if chi.URLParam(r,"operation")=="close"{result,err=app.InternalServices.CloseFutureOffering(r.Context(),*actor,merchant,id,body.ExpectedUpdatedAt,body.Reason)}else{if body.BrandVisible==nil{app.respondWithErrorCode(w,400,"invalid_request","Specify whether to show the offering on the brand page.");return};result,err=app.InternalServices.SetFutureOfferingBrandVisibility(r.Context(),*actor,merchant,id,body.ExpectedUpdatedAt,*body.BrandVisible)}
 if err!=nil{app.offeringFailure(w,r,err);return};app.respondWithJSON(w,200,jsonResponse{Data:newMerchantFutureOfferingResponse(result),Message:"Project updated"})
}
// OfferingPublicResponse is the publication contract, distinct from merchant DTOs.
type OfferingPublicResponse struct {
 ID uuid.UUID `json:"id"`
 BrandID uuid.UUID `json:"brand_id"`
 Brand string `json:"brand"`
 Title *string `json:"title"`
 Summary string `json:"summary"`
 Description *data.RichTextDocument `json:"description"`
 LaunchAt *time.Time `json:"launch_at,omitempty"`
 LaunchWindowPrecision *data.MerchantFutureOfferingLaunchPrecision `json:"launch_window_precision,omitempty"`
 LaunchWindowStart *string `json:"launch_window_start,omitempty"`
 LaunchDisplayText *string `json:"launch_display_text,omitempty"`
 Media []data.FutureOfferingMedia `json:"media"`
}
func offeringPublicResponse(v services.FutureOfferingPreview)OfferingPublicResponse{
 f:=v.Offering;var start *string;if f.LaunchWindowStart!=nil{s:=f.LaunchWindowStart.Format("2006-01-02");start=&s}
 return OfferingPublicResponse{ID:f.ID,BrandID:f.MerchantID,Brand:v.Brand,Title:f.Title,Summary:f.Summary,Description:f.Description,LaunchAt:f.LaunchAt,LaunchWindowPrecision:f.LaunchWindowPrecision,LaunchWindowStart:start,LaunchDisplayText:f.LaunchDisplayText,Media:v.Media}
}
// GetPublicFutureOfferingHandler has no merchant-authorization fallback.
func(app *Application) GetPublicFutureOfferingHandler(w http.ResponseWriter,r *http.Request){
 id,ok:=app.parseFutureOfferingIDParam(w,r);if !ok{return};v,err:=app.InternalServices.PublicFutureOffering(r.Context(),id);if err!=nil{app.offeringFailure(w,r,err);return}
 w.Header().Set("Cache-Control","no-store");app.respondWithJSON(w,200,jsonResponse{Data:offeringPublicResponse(v),Message:"Offering retrieved"})
}
// FutureOfferingMediaBytesHandler streams authorized private-store bytes internally.
func(app *Application) FutureOfferingMediaBytesHandler(w http.ResponseWriter,r *http.Request){
 id,ok:=app.parseFutureOfferingIDParam(w,r);if !ok{return};asset,err:=uuid.Parse(chi.URLParam(r,"assetID"));if err!=nil{app.respondWithErrorCode(w,404,"not_found","Media unavailable.");return}
 var merchant *uuid.UUID
 if chi.URLParam(r,"surface")=="merchant"{m,err:=app.requireAuthorizedMerchantFutureOfferingMerchant(r.Context());if err!=nil{app.respondMerchantFutureOfferingAuthorizationError(w,r,err);return};merchant=&m}
 ctx,cancel:=context.WithTimeout(r.Context(),2*time.Minute);defer cancel()
 source,meta,err:=app.InternalServices.OpenFutureOfferingMedia(ctx,merchant,id,asset);if err!=nil{app.offeringFailure(w,r,err);return};defer source.Close()
 w.Header().Set("Cache-Control","no-store");w.Header().Set("X-Content-Type-Options","nosniff");w.Header().Set("Content-Type",meta.ContentType);w.Header().Set("Content-Length",strconv.FormatInt(meta.ByteSize,10));w.Header().Set("Content-Security-Policy","default-src 'none'; sandbox")
 w.Header().Set("Accept-Ranges","bytes")
 start,end,partial,rangeErr:=offeringMediaRange(r.Header.Get("Range"),meta.ByteSize)
 if rangeErr!=nil{w.Header().Set("Content-Range",fmt.Sprintf("bytes */%d",meta.ByteSize));w.Header().Del("Content-Length");w.WriteHeader(http.StatusRequestedRangeNotSatisfiable);return}
 if partial {
  if _,err=io.CopyN(io.Discard,source,start);err!=nil{w.Header().Del("Content-Length");app.respondWithErrorCode(w,503,"media_unavailable","Media temporarily unavailable.");return}
  w.Header().Set("Content-Range",fmt.Sprintf("bytes %d-%d/%d",start,end,meta.ByteSize));w.Header().Set("Content-Length",strconv.FormatInt(end-start+1,10));w.WriteHeader(http.StatusPartialContent)
 }
 if _,err=io.CopyN(w,source,end-start+1);err!=nil{app.Logger.Warn("offering media stream interrupted")}
}

// PreviewFutureOfferingHandler exposes a public-safe preview to its merchant only.
func(app *Application) PreviewFutureOfferingHandler(w http.ResponseWriter,r *http.Request){
 merchant,id,ok:=app.offeringScope(w,r);if !ok{return};v,err:=app.InternalServices.PreviewFutureOffering(r.Context(),merchant,id);if err!=nil{app.offeringFailure(w,r,err);return}
 w.Header().Set("Cache-Control","no-store");app.respondWithJSON(w,200,jsonResponse{Data:offeringPublicResponse(v),Message:"Preview retrieved"})
}
// offeringMediaRange supports one HTTP byte range, including suffix ranges.
// Multiple ranges are intentionally refused; all arithmetic is bounded by size.
func offeringMediaRange(header string,size int64)(int64,int64,bool,error){
 invalid:=errors.New("unsupported media range")
 if size<=0{return 0,0,false,invalid};if header==""{return 0,size-1,false,nil}
 if !strings.HasPrefix(header,"bytes=")||strings.Contains(header,","){return 0,0,false,invalid}
 parts:=strings.Split(strings.TrimPrefix(header,"bytes="),"-");if len(parts)!=2{return 0,0,false,invalid}
 if parts[0]==""{suffix,err:=strconv.ParseInt(parts[1],10,64);if err!=nil||suffix<=0{return 0,0,false,invalid};if suffix>size{suffix=size};return size-suffix,size-1,true,nil}
 start,err:=strconv.ParseInt(parts[0],10,64);if err!=nil||start<0||start>=size{return 0,0,false,invalid};end:=size-1
 if parts[1]!=""{end,err=strconv.ParseInt(parts[1],10,64);if err!=nil||end<start{return 0,0,false,invalid};if end>=size{end=size-1}}
 return start,end,true,nil
}

// FutureOfferingBetaCapacityHandler reads/updates optional Beta seats with version control.
func(app *Application) FutureOfferingBetaCapacityHandler(w http.ResponseWriter,r *http.Request){
 merchant,fo,ok:=app.offeringScope(w,r);if !ok{return}
 var result data.FutureOfferingBetaCapacity;var err error
 if r.Method==http.MethodGet{result,err=app.InternalServices.FutureOfferingBetaCapacity(r.Context(),merchant,fo)}else{
  actor:=app.getUserIDFromContext(r.Context());if actor==nil{app.respondWithErrorCode(w,401,"authentication_required","Sign in again.");return}
  var request struct{ExpectedUpdatedAt time.Time `json:"expected_updated_at"`;Seats json.RawMessage `json:"seats"`}
  if err=app.readJSON(w,r,&request);err!=nil{app.respondWithErrorCode(w,400,"invalid_request","Invalid Beta seat declaration.");return}
  if len(request.Seats)==0{app.respondWithErrorCode(w,400,"invalid_request","Supply seats, or null to leave the declaration unset.");return}
  var seats *int;if err=json.Unmarshal(request.Seats,&seats);err!=nil{app.respondWithErrorCode(w,400,"invalid_request","Seats must be a whole number or null.");return}
  result,err=app.InternalServices.SetFutureOfferingBetaCapacity(r.Context(),*actor,merchant,fo,request.ExpectedUpdatedAt,seats)
 };if err!=nil{app.offeringFailure(w,r,err);return};app.respondWithJSON(w,200,jsonResponse{Data:result,Message:"Beta seat declaration retrieved"})
}

// ListPublicOfferingsHandler provides bounded discovery or a merchant's brand page.
func(app *Application) ListPublicOfferingsHandler(w http.ResponseWriter,r *http.Request){
 var merchant,beforeID,category *uuid.UUID;var before *time.Time
 if raw:=chi.URLParam(r,"merchantID");raw!=""{id,err:=uuid.Parse(raw);if err!=nil{app.respondWithErrorCode(w,400,"invalid_request","Invalid brand reference.");return};merchant=&id}
 q:=r.URL.Query()
 if raw:=q.Get("category_id");raw!=""{id,err:=uuid.Parse(raw);if err!=nil||id==uuid.Nil{app.respondWithErrorCode(w,400,"invalid_category","Select a valid category.");return};category=&id}
 if q.Get("before_published_at")!=""||q.Get("before_id")!=""{t,err:=time.Parse(time.RFC3339Nano,q.Get("before_published_at"));id,idErr:=uuid.Parse(q.Get("before_id"));if err!=nil||idErr!=nil{app.respondWithErrorCode(w,400,"invalid_request","Invalid page cursor.");return};before=&t;beforeID=&id}
 result,err:=app.InternalServices.ListPublicOfferings(r.Context(),merchant,before,beforeID,category);if err!=nil{app.offeringFailure(w,r,err);return};items:=[]OfferingPublicResponse{};for _,v:=range result.Items{items=append(items,offeringPublicResponse(v))}
 w.Header().Set("Cache-Control","no-store");app.respondWithJSON(w,200,jsonResponse{Data:struct{Items []OfferingPublicResponse `json:"items"`;Before *time.Time `json:"next_before_published_at,omitempty"`;BeforeID *uuid.UUID `json:"next_before_id,omitempty"`}{items,result.Before,result.BeforeID},Message:"Offerings retrieved"})
}

// GetFutureOfferingPublicationHandler returns stored listing membership, never an inferred default.
func(app *Application) GetFutureOfferingPublicationHandler(w http.ResponseWriter,r *http.Request){
 merchant,fo,ok:=app.offeringScope(w,r);if !ok{return}
 result,err:=app.InternalServices.GetFutureOfferingPublication(r.Context(),merchant,fo);if err!=nil{app.offeringFailure(w,r,err);return}
 w.Header().Set("Cache-Control","no-store");app.respondWithJSON(w,200,jsonResponse{Data:result,Message:"Publication retrieved"})
}
