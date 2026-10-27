// Package services owns open-ended offering publication and private-media attachment.
// focodebase/fobackend/internal/services/future_offering_publication.go
// Lock order: user -> merchant account -> FO -> media object -> history/outbox.
// Storage is outside write
// transactions; intent is durable first. Uncertain commits are reconciled by
// object state, never by unconditional deletion. Commercial processing is absent.
package services

import (
 "bytes"
 "context"
 "crypto/sha256"
 "encoding/hex"
 "encoding/json"
 "errors"
 "image"
 "image/draw"
 "image/jpeg"
 _ "image/png"
 "io"
 "net/http"
 "strings"
 "time"
 "unicode/utf8"
 "unicode"
 "github.com/PiccoloMondoC/focodebase/fobackend/internal/data"
 "github.com/PiccoloMondoC/focodebase/fobackend/internal/mediastore"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
)

// FutureOfferingGallery contains one version and its ordered immutable-object references.
type FutureOfferingGallery struct {
 Media []data.FutureOfferingMedia `json:"media"`
 UpdatedAt time.Time `json:"updated_at"`
}
// FutureOfferingPreview contains a canonical FO for projection at the HTTP boundary.
// Never serialize this structure publicly: FO contains merchant-private facts.
type FutureOfferingPreview struct { Offering *data.MerchantFutureOffering; Brand string; Media []data.FutureOfferingMedia }

const offeringMediaTransportLimit = 64 << 20

func offeringEditable(fo *data.MerchantFutureOffering,expected time.Time)error{
 if fo.Status!="draft" && fo.Status!="published"{return data.ErrMerchantFutureOfferingInvalidTransition}
 if expected.IsZero() || !fo.UpdatedAt.Equal(expected){return data.ErrMerchantFutureOfferingEditConflict};return nil
}
func offeringAlt(value string)(string,error){
 value=strings.TrimSpace(value)
 if !utf8.ValidString(value)||utf8.RuneCountInString(value)>300||strings.IndexFunc(value,unicode.IsControl)>=0{return "",data.ErrMerchantFutureOfferingInvalidInput};return value,nil
}
func normalizeOfferingMedia(raw []byte)([]byte,string,string,error){
 invalid:=data.ErrMerchantFutureOfferingInvalidInput
 if len(raw)==0||len(raw)>offeringMediaTransportLimit{return nil,"","",invalid}
 mime:=http.DetectContentType(raw)
 if mime=="image/jpeg"||mime=="image/png" {
  if len(raw)>20<<20{return nil,"","",invalid}
  cfg,_,err:=image.DecodeConfig(bytes.NewReader(raw));if err!=nil||cfg.Width<1||cfg.Height<1||cfg.Width>8000||cfg.Height>8000||int64(cfg.Width)*int64(cfg.Height)>16000000{return nil,"","",invalid}
  decoded,_,err:=image.Decode(bytes.NewReader(raw));if err!=nil{return nil,"","",invalid}
  canvas:=image.NewRGBA(decoded.Bounds());draw.Draw(canvas,canvas.Bounds(),image.White,image.Point{},draw.Src);draw.Draw(canvas,canvas.Bounds(),decoded,decoded.Bounds().Min,draw.Over)
  var out bytes.Buffer
  if err=jpeg.Encode(&out,orientProfilePhoto(canvas,profilePhotoOrientation(raw)),&jpeg.Options{Quality:90});err!=nil{return nil,"","",invalid}
  return out.Bytes(),"image/jpeg","image",nil
 }
 // MP4/WebM bytes are never rendered as markup. Codec support is the browser's
 // responsibility; content-type sniffing is not content moderation or transcoding.
 if mime=="video/mp4"||mime=="video/webm"{return raw,mime,"video",nil}
 return nil,"","",invalid
}
func offeringPreviousStatus(kind data.MerchantFutureOfferingEventType)*data.MerchantFutureOfferingStatus{
 status:=data.MerchantFutureOfferingStatusDraft
 if kind==data.MerchantFutureOfferingEventArchived{status=data.MerchantFutureOfferingStatusPublished}
 return &status
}
func(s *Service) offeringEventTx(ctx context.Context,tx pgx.Tx,fo *data.MerchantFutureOffering,actor uuid.UUID,kind data.MerchantFutureOfferingEventType,note *string)error{
 status:=fo.Status
 history,err:=s.Models.MerchantFutureOfferingEvent.InsertTx(ctx,tx,data.NewMerchantFutureOfferingEvent{FutureOfferingID:fo.ID,EventType:kind,FromStatus:offeringPreviousStatus(kind),ToStatus:&status,Note:note,PerformedBy:&actor});if err!=nil{return err}
 payload,err:=json.Marshal(map[string]any{"future_offering_id":fo.ID,"event_id":history.ID,"merchant_id":fo.MerchantID,"version":fo.UpdatedAt});if err!=nil{return err}
 _,err=s.Models.OutboxEvent.InsertTx(ctx,tx,data.NewOutboxEvent{AggregateType:merchantFutureOfferingAggregateType,AggregateID:fo.ID,EventType:"future_offering."+string(kind),EventVersion:1,OccurredAt:&fo.UpdatedAt,Payload:payload,IdempotencyKey:"future_offering_history:"+history.ID.String()});return err
}
// runOfferingMutationTx rechecks live principal authority at each commit boundary.
// Lock order is user -> merchant account -> FO -> media -> history/outbox.
// Upload staging and attachment use separate transactions around storage I/O.
func(s *Service) runOfferingMutationTx(ctx context.Context,name string,actor,merchant uuid.UUID,fn func(context.Context,pgx.Tx)error)error{
 if actor==uuid.Nil||merchant==uuid.Nil{return data.ErrMerchantAccountAccessDenied}
 return s.runMerchantFutureOfferingTx(ctx,name,true,func(ctx context.Context,tx pgx.Tx)error{
  var id uuid.UUID
  err:=tx.QueryRow(ctx,`SELECT id FROM users WHERE id=$1 AND is_active AND deleted_at IS NULL FOR SHARE`,actor).Scan(&id)
  if errors.Is(err,pgx.ErrNoRows){return data.ErrMerchantAccountAccessDenied};if err!=nil{return err}
  err=tx.QueryRow(ctx,`SELECT a.id FROM merchant_accounts a JOIN merchants m ON m.id=a.merchant_id WHERE a.principal_user_id=$1 AND a.merchant_id=$2 AND a.account_status='active' AND a.deleted_at IS NULL AND m.deleted_at IS NULL FOR SHARE OF a`,actor,merchant).Scan(&id)
  if errors.Is(err,pgx.ErrNoRows){return data.ErrMerchantAccountAccessDenied};if err!=nil{return err}
  return fn(ctx,tx)
 })
}
// GetFutureOfferingGallery returns merchant-scoped media and aggregate version.
func(s *Service) GetFutureOfferingGallery(ctx context.Context,merchant,fo uuid.UUID)(FutureOfferingGallery,error){
 result:=FutureOfferingGallery{Media:[]data.FutureOfferingMedia{}}
 err:=s.runMerchantFutureOfferingTx(ctx,"GetFutureOfferingGallery",false,func(ctx context.Context,tx pgx.Tx)error{
  v,err:=s.Models.MerchantFutureOffering.GetByIDForUpdateTx(ctx,tx,merchant,fo);if err!=nil{return err}
  result.UpdatedAt=v.UpdatedAt;result.Media,err=s.Models.MerchantFutureOffering.ListMediaTx(ctx,tx,fo);return err
 });return result,err
}
// UploadFutureOfferingMedia validates and stages before attachment. No external
// calls occur while an FO lock is held; a stale attachment fails without linking.
func(s *Service) UploadFutureOfferingMedia(ctx context.Context,actor,merchant,fo uuid.UUID,expected time.Time,alt string,body io.Reader)(FutureOfferingGallery,error){
 result:=FutureOfferingGallery{}
 if err:=s.validate();err!=nil{return result,err};if actor==uuid.Nil||ctx==nil{return result,data.ErrMerchantFutureOfferingInvalidInput};if s.media==nil{return result,ErrInvalidServiceConfiguration}
 select{case s.photoSlots<-struct{}{}:defer func(){<-s.photoSlots}();default:return result,mediastore.ErrUnavailable}
 if body==nil{return result,data.ErrMerchantFutureOfferingInvalidInput}
 raw,err:=io.ReadAll(io.LimitReader(body,offeringMediaTransportLimit+1));if err!=nil{return result,err}
 alt,err=offeringAlt(alt);if err!=nil{return result,err}
 normalized,mime,kind,err:=normalizeOfferingMedia(raw);if err!=nil{return result,err}
 id:=uuid.New();driver,location:=s.media.Location();sum:=sha256.Sum256(normalized)
 err=s.runOfferingMutationTx(ctx,"StageFutureOfferingMedia",actor,merchant,func(ctx context.Context,tx pgx.Tx)error{
  v,err:=s.Models.MerchantFutureOffering.GetByIDForUpdateTx(ctx,tx,merchant,fo);if err!=nil{return err};if err=offeringEditable(v,expected);err!=nil{return err}
  return s.Models.MerchantFutureOffering.StageMediaTx(ctx,tx,data.FutureOfferingMediaStage{ID:id,Owner:actor,Driver:driver,Location:location,ContentType:mime,Digest:hex.EncodeToString(sum[:]),Size:int64(len(normalized))})
 });if err!=nil{return result,err};defer s.retireStagedMedia(id)
 upload,cancel:=context.WithTimeout(ctx,30*time.Second);defer cancel()
 if err=s.media.Put(upload,id.String(),mime,bytes.NewReader(normalized));err!=nil{return result,err}
 err=s.runOfferingMutationTx(ctx,"AttachFutureOfferingMedia",actor,merchant,func(ctx context.Context,tx pgx.Tx)error{
  v,err:=s.Models.MerchantFutureOffering.GetByIDForUpdateTx(ctx,tx,merchant,fo);if err!=nil{return err};if err=offeringEditable(v,expected);err!=nil{return err}
  gallery,err:=s.Models.MerchantFutureOffering.ListMediaTx(ctx,tx,fo);if err!=nil{return err}
  main:=kind=="image";order:=0;for _,a:=range gallery{if a.Main{main=false};if a.Order>=order{order=a.Order+1}}
  if len(gallery)>=100{return data.ErrMerchantFutureOfferingInvalidInput}
  if err=s.Models.MerchantFutureOffering.AttachMediaTx(ctx,tx,fo,data.FutureOfferingMedia{ID:id,Kind:kind,AltText:alt,Order:order,Main:main});err!=nil{return err}
  v,err=s.Models.MerchantFutureOffering.AdvanceEditableVersionTx(ctx,tx,merchant,fo,expected);if err!=nil{return err}
  result.UpdatedAt=v.UpdatedAt;result.Media,err=s.Models.MerchantFutureOffering.ListMediaTx(ctx,tx,fo);if err!=nil{return err};return s.offeringRevisionTx(ctx,tx,v,actor,"media",gallery,result.Media)
 });return result,err
}
// EditFutureOfferingMedia protects the active main image and preserves optimistic concurrency.
func(s *Service) EditFutureOfferingMedia(ctx context.Context,actor,merchant,fo uuid.UUID,expected time.Time,edit data.FutureOfferingMedia,remove bool)(FutureOfferingGallery,error){
 if actor==uuid.Nil{return FutureOfferingGallery{},data.ErrMerchantFutureOfferingInvalidInput}
 result:=FutureOfferingGallery{}
 alt,err:=offeringAlt(edit.AltText);if err!=nil||edit.Order<0||edit.Order>10000{return result,data.ErrMerchantFutureOfferingInvalidInput};edit.AltText=alt
 err=s.runOfferingMutationTx(ctx,"EditFutureOfferingMedia",actor,merchant,func(ctx context.Context,tx pgx.Tx)error{
  v,err:=s.Models.MerchantFutureOffering.GetByIDForUpdateTx(ctx,tx,merchant,fo);if err!=nil{return err};if err=offeringEditable(v,expected);err!=nil{return err}
  gallery,err:=s.Models.MerchantFutureOffering.ListMediaTx(ctx,tx,fo);if err!=nil{return err};found:=false
  for _,a:=range gallery{if a.ID==edit.ID{found=true;if edit.Main&&a.Kind!="image"{return data.ErrMerchantFutureOfferingInvalidInput};if v.Status=="published"&&a.Main&&(remove||!edit.Main){return data.ErrMerchantFutureOfferingInvalidTransition}}};if !found{return data.ErrMerchantFutureOfferingNotFound}
  if err=s.Models.MerchantFutureOffering.EditMediaTx(ctx,tx,fo,edit,remove);err!=nil{return err}
  v,err=s.Models.MerchantFutureOffering.AdvanceEditableVersionTx(ctx,tx,merchant,fo,expected);if err!=nil{return err}
  result.UpdatedAt=v.UpdatedAt;result.Media,err=s.Models.MerchantFutureOffering.ListMediaTx(ctx,tx,fo);if err!=nil{return err};return s.offeringRevisionTx(ctx,tx,v,actor,"media",gallery,result.Media)
 });return result,err
}
// ActivateFutureOffering performs canonical validation and publication atomically.
// Replay succeeds only for the exact activation source version. No billable event,
// payment record, service term or commercial waiver is manufactured.
func(s *Service) ActivateFutureOffering(ctx context.Context,actor,merchant,fo uuid.UUID,expected time.Time)(*data.MerchantFutureOffering,error){
 if actor==uuid.Nil||expected.IsZero(){return nil,data.ErrMerchantFutureOfferingInvalidInput}
 var out *data.MerchantFutureOffering
 err:=s.runOfferingMutationTx(ctx,"ActivateFutureOffering",actor,merchant,func(ctx context.Context,tx pgx.Tx)error{
  v,err:=s.Models.MerchantFutureOffering.GetByIDForUpdateTx(ctx,tx,merchant,fo);if err!=nil{return err}
  if v.Status=="published"{_,source,err:=s.Models.MerchantFutureOffering.PublicationTx(ctx,tx,fo);if err!=nil{return err};if !source.Equal(expected){return data.ErrMerchantFutureOfferingEditConflict};out=v;return nil}
  if err=offeringEditable(v,expected);err!=nil{return err}
  ready,err:=s.merchantFutureOfferingReadiness().Evaluate(ctx,tx,v);if err!=nil{return err};if !ready.Ready{return &MerchantFutureOfferingNotReadyError{Readiness:ready}}
  out,err=s.Models.MerchantFutureOffering.ActivateTx(ctx,tx,merchant,fo,expected);if err!=nil{return err}
  return s.offeringEventTx(ctx,tx,out,actor,data.MerchantFutureOfferingEventActivated,nil)
 });return out,err
}
// PublicFutureOffering resolves only public-safe eligible offerings for projection.
func(s *Service) PublicFutureOffering(ctx context.Context,fo uuid.UUID)(FutureOfferingPreview,error){
 var out FutureOfferingPreview
 err:=s.runMerchantFutureOfferingTx(ctx,"PublicFutureOffering",false,func(ctx context.Context,tx pgx.Tx)error{
  var err error;out.Offering,out.Brand,err=s.Models.MerchantFutureOffering.PublicOfferingTx(ctx,tx,fo);if err!=nil{return err};out.Media,err=s.Models.MerchantFutureOffering.ListMediaTx(ctx,tx,fo);return err
 });return out,err
}
// OpenFutureOfferingMedia checks public eligibility or explicit merchant scope
// before opening private immutable bytes. Callers must close the returned reader.
func(s *Service) OpenFutureOfferingMedia(ctx context.Context,merchant *uuid.UUID,fo,id uuid.UUID)(io.ReadCloser,data.FutureOfferingMedia,error){
 var found data.FutureOfferingMedia
 err:=s.runMerchantFutureOfferingTx(ctx,"OpenFutureOfferingMedia",false,func(ctx context.Context,tx pgx.Tx)error{
  if merchant==nil{if _,_,err:=s.Models.MerchantFutureOffering.PublicOfferingTx(ctx,tx,fo);err!=nil{return err}}else{if _,err:=s.Models.MerchantFutureOffering.GetByIDForMerchantTx(ctx,tx,*merchant,fo);err!=nil{return err}}
  gallery,err:=s.Models.MerchantFutureOffering.ListMediaTx(ctx,tx,fo);if err!=nil{return err};for _,a:=range gallery{if a.ID==id{found=a;return nil}};return data.ErrMerchantFutureOfferingNotFound
 });if err!=nil{return nil,found,err};if s.media==nil{return nil,found,ErrInvalidServiceConfiguration};r,err:=s.media.Open(ctx,id.String());return r,found,err
}
// SetFutureOfferingBrandVisibility changes only membership, keeping the FO public.
func(s *Service) SetFutureOfferingBrandVisibility(ctx context.Context,actor,merchant,fo uuid.UUID,expected time.Time,visible bool)(*data.MerchantFutureOffering,error){
 if actor==uuid.Nil{return nil,data.ErrMerchantFutureOfferingInvalidInput}
 var out *data.MerchantFutureOffering
 err:=s.runOfferingMutationTx(ctx,"SetFutureOfferingBrandVisibility",actor,merchant,func(ctx context.Context,tx pgx.Tx)error{
  v,err:=s.Models.MerchantFutureOffering.GetByIDForUpdateTx(ctx,tx,merchant,fo);if err!=nil{return err};if v.Status!="published"{return data.ErrMerchantFutureOfferingInvalidTransition};if err=offeringEditable(v,expected);err!=nil{return err}
  previousVisible,_,err:=s.Models.MerchantFutureOffering.PublicationTx(ctx,tx,fo);if err!=nil{return err}
  if err=s.Models.MerchantFutureOffering.SetBrandVisibilityTx(ctx,tx,fo,visible);err!=nil{return err};out,err=s.Models.MerchantFutureOffering.AdvanceEditableVersionTx(ctx,tx,merchant,fo,expected);if err!=nil{return err};return s.offeringRevisionTx(ctx,tx,out,actor,"brand_visibility",previousVisible,visible)
 });return out,err
}
// CloseFutureOffering retires a prelaunch FO with an immutable reason event.
func(s *Service) CloseFutureOffering(ctx context.Context,actor,merchant,fo uuid.UUID,expected time.Time,reason string)(*data.MerchantFutureOffering,error){
 reason=strings.TrimSpace(reason);if reason==""||!utf8.ValidString(reason)||utf8.RuneCountInString(reason)>1000||actor==uuid.Nil{return nil,data.ErrMerchantFutureOfferingInvalidInput}
 var out *data.MerchantFutureOffering
 err:=s.runOfferingMutationTx(ctx,"CloseFutureOffering",actor,merchant,func(ctx context.Context,tx pgx.Tx)error{
  v,err:=s.Models.MerchantFutureOffering.GetByIDForUpdateTx(ctx,tx,merchant,fo);if err!=nil{return err};if err=offeringEditable(v,expected);err!=nil{return err}
  out,err=s.Models.MerchantFutureOffering.ClosePublishedTx(ctx,tx,merchant,fo,expected,reason);if err!=nil{return err};return s.offeringEventTx(ctx,tx,out,actor,data.MerchantFutureOfferingEventArchived,&reason)
 });return out,err
}

// PreviewFutureOffering uses the same public presentation facts while keeping
// access merchant-scoped. Preview never changes publication or brand visibility.
func(s *Service) PreviewFutureOffering(ctx context.Context,merchant,fo uuid.UUID)(FutureOfferingPreview,error){
 var out FutureOfferingPreview
 err:=s.runMerchantFutureOfferingTx(ctx,"PreviewFutureOffering",false,func(ctx context.Context,tx pgx.Tx)error{
  var err error;out.Offering,err=s.Models.MerchantFutureOffering.GetByIDForUpdateTx(ctx,tx,merchant,fo);if err!=nil{return err}
  out.Brand,err=s.Models.MerchantFutureOffering.PreviewBrandTx(ctx,tx,merchant);if err!=nil{return err};out.Media,err=s.Models.MerchantFutureOffering.ListMediaTx(ctx,tx,fo);return err
 });return out,err
}
func(s *Service) offeringRevisionTx(ctx context.Context,tx pgx.Tx,fo *data.MerchantFutureOffering,actor uuid.UUID,operation string,before,after any)error{
 id,err:=s.Models.MerchantFutureOffering.RecordRevisionTx(ctx,tx,fo.ID,actor,fo.UpdatedAt,operation,before,after);if err!=nil{return err}
 payload,err:=json.Marshal(map[string]any{"future_offering_id":fo.ID,"revision_id":id,"operation":operation,"version":fo.UpdatedAt});if err!=nil{return err}
 _,err=s.Models.OutboxEvent.InsertTx(ctx,tx,data.NewOutboxEvent{AggregateType:merchantFutureOfferingAggregateType,AggregateID:fo.ID,EventType:"future_offering.updated",EventVersion:1,OccurredAt:&fo.UpdatedAt,Payload:payload,IdempotencyKey:"future_offering_revision:"+id.String()});return err
}
// UpdateFutureOfferingFacts preserves readiness of active offerings and records
// the before/after facts and independent update occurrence in one transaction.
func(s *Service) UpdateFutureOfferingFacts(ctx context.Context,actor,merchant,fo uuid.UUID,facts data.MerchantFutureOfferingDraftFacts,expected time.Time)(*data.MerchantFutureOffering,error){
 if actor==uuid.Nil{return nil,data.ErrMerchantFutureOfferingInvalidInput};var out *data.MerchantFutureOffering
 err:=s.runOfferingMutationTx(ctx,"UpdateFutureOfferingFacts",actor,merchant,func(ctx context.Context,tx pgx.Tx)error{
  previous,err:=s.Models.MerchantFutureOffering.GetByIDForUpdateTx(ctx,tx,merchant,fo);if err!=nil{return err};if err=offeringEditable(previous,expected);err!=nil{return err}
  out,err=s.Models.MerchantFutureOffering.UpdateEditableFactsTx(ctx,tx,merchant,fo,facts,expected);if err!=nil{return err}
  if out.Status=="published"{ready,err:=s.merchantFutureOfferingReadiness().Evaluate(ctx,tx,out);if err!=nil{return err};if !ready.Ready{ready.EvaluatedVersion=previous.UpdatedAt;return &MerchantFutureOfferingNotReadyError{Readiness:ready}}}
  return s.offeringRevisionTx(ctx,tx,out,actor,"details",previous,out)
 });return out,err
}

// FutureOfferingBetaCapacity returns an optional declaration and aggregate usage.
func(s *Service) FutureOfferingBetaCapacity(ctx context.Context,merchant,fo uuid.UUID)(data.FutureOfferingBetaCapacity,error){
 var result data.FutureOfferingBetaCapacity
 err:=s.runMerchantFutureOfferingTx(ctx,"FutureOfferingBetaCapacity",false,func(ctx context.Context,tx pgx.Tx)error{
  if _,err:=s.Models.MerchantFutureOffering.GetByIDForUpdateTx(ctx,tx,merchant,fo);err!=nil{return err};var err error;result,err=s.Models.MerchantFutureOffering.BetaCapacityTx(ctx,tx,fo);return err
 });return result,err
}
// SetFutureOfferingBetaCapacity preserves participation and never cancels Beta.
func(s *Service) SetFutureOfferingBetaCapacity(ctx context.Context,actor,merchant,fo uuid.UUID,expected time.Time,seats *int)(data.FutureOfferingBetaCapacity,error){
 if actor==uuid.Nil{return data.FutureOfferingBetaCapacity{},data.ErrMerchantFutureOfferingInvalidInput}
 var result data.FutureOfferingBetaCapacity
 err:=s.runOfferingMutationTx(ctx,"SetFutureOfferingBetaCapacity",actor,merchant,func(ctx context.Context,tx pgx.Tx)error{
  f,err:=s.Models.MerchantFutureOffering.GetByIDForUpdateTx(ctx,tx,merchant,fo);if err!=nil{return err};if err=offeringEditable(f,expected);err!=nil{return err}
  before,err:=s.Models.MerchantFutureOffering.BetaCapacityTx(ctx,tx,fo);if err!=nil{return err}
  if err=s.Models.MerchantFutureOffering.SetBetaCapacityTx(ctx,tx,fo,seats);err!=nil{return err}
  f,err=s.Models.MerchantFutureOffering.AdvanceEditableVersionTx(ctx,tx,merchant,fo,expected);if err!=nil{return err}
  result,err=s.Models.MerchantFutureOffering.BetaCapacityTx(ctx,tx,fo);if err!=nil{return err};return s.offeringRevisionTx(ctx,tx,f,actor,"beta_capacity",before,result)
 });return result,err
}

// PublicOfferingPage carries a bounded page and an optional keyset cursor.
type PublicOfferingPage struct{ Items []FutureOfferingPreview; Before *time.Time; BeforeID *uuid.UUID }
// ListPublicOfferings resolves public publication facts in one read transaction.
func(s *Service) ListPublicOfferings(ctx context.Context,merchant *uuid.UUID,before *time.Time,beforeID *uuid.UUID,category *uuid.UUID)(PublicOfferingPage,error){
 result:=PublicOfferingPage{Items:[]FutureOfferingPreview{}}
 if (before==nil)!=(beforeID==nil){return result,data.ErrMerchantFutureOfferingInvalidInput}
 err:=s.runMerchantFutureOfferingTx(ctx,"ListPublicOfferings",false,func(ctx context.Context,tx pgx.Tx)error{
  if category!=nil {
   choice,err:=s.Models.Category.IsSelectableForFutureOfferingTx(ctx,tx,*category);if err!=nil{return err}
   if choice!=data.FutureOfferingCategorySelectable{return data.ErrMerchantFutureOfferingNotFound}
  }
  ids,err:=s.Models.MerchantFutureOffering.PublicOfferingPageTx(ctx,tx,merchant,before,beforeID,category);if err!=nil{return err};more:=len(ids)>24;if more{ids=ids[:24]}
  for _,id:=range ids{f,brand,err:=s.Models.MerchantFutureOffering.PublicOfferingTx(ctx,tx,id);if err!=nil{return err};assets,err:=s.Models.MerchantFutureOffering.ListMediaTx(ctx,tx,id);if err!=nil{return err};result.Items=append(result.Items,FutureOfferingPreview{Offering:f,Brand:brand,Media:assets})}
  if more&&len(result.Items)>0{last:=result.Items[len(result.Items)-1].Offering;result.Before=last.PublishedAt;id:=last.ID;result.BeforeID=&id};return nil
 });return result,err
}

// FutureOfferingPublication exposes current merchant-controlled listing membership.
type FutureOfferingPublication struct {BrandVisible bool `json:"brand_visible"`;UpdatedAt time.Time `json:"updated_at"`}
func(s *Service) GetFutureOfferingPublication(ctx context.Context,merchant,fo uuid.UUID)(FutureOfferingPublication,error){
 var result FutureOfferingPublication
 err:=s.runMerchantFutureOfferingTx(ctx,"GetFutureOfferingPublication",false,func(ctx context.Context,tx pgx.Tx)error{
  f,err:=s.Models.MerchantFutureOffering.GetByIDForUpdateTx(ctx,tx,merchant,fo);if err!=nil{return err}
  result.UpdatedAt=f.UpdatedAt;result.BrandVisible,_,err=s.Models.MerchantFutureOffering.PublicationTx(ctx,tx,fo);return err
 });return result,err
}

// DiscardFutureOfferingDraft retires all linked objects in the same transaction
// as soft deletion. Cleanup can never remove bytes of a retained active reference.
func(s *Service) DiscardFutureOfferingDraft(ctx context.Context,actor,merchant,fo uuid.UUID,expected time.Time)(*data.MerchantFutureOffering,error){
 var out *data.MerchantFutureOffering
 err:=s.runOfferingMutationTx(ctx,"DiscardFutureOfferingDraft",actor,merchant,func(ctx context.Context,tx pgx.Tx)error{
  previous,err:=s.lockDraftAtVersion(ctx,tx,merchant,fo,expected);if err!=nil{return err}
  gallery,err:=s.Models.MerchantFutureOffering.ListMediaTx(ctx,tx,fo);if err!=nil{return err}
  for _,asset:=range gallery{if err=s.Models.MerchantFutureOffering.EditMediaTx(ctx,tx,fo,asset,true);err!=nil{return err}}
  out,err=s.Models.MerchantFutureOffering.DiscardDraftTx(ctx,tx,merchant,fo,expected);if err!=nil{return err}
  return s.offeringRevisionTx(ctx,tx,out,actor,"discard",struct{Offering *data.MerchantFutureOffering;Media []data.FutureOfferingMedia}{previous,gallery},out)
 });return out,err
}
