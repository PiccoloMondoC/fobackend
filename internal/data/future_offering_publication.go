// Package data owns merchant-scoped offering media and publication persistence.
// focodebase/fobackend/internal/data/future_offering_publication.go
// Mutations require the caller to lock the FO first. Media objects are locked
// second; cleanup locks only objects and never waits for an FO. No method commits.
package data

import (
 "context"
 "errors"
 "encoding/json"
 "time"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
)

// FutureOfferingMedia exposes only presentation metadata, never storage location.
type FutureOfferingMedia struct {
 ID uuid.UUID `json:"id"`
 Kind string `json:"kind"`
 AltText string `json:"alt_text"`
 Order int `json:"display_order"`
 Main bool `json:"is_main"`
 ContentType string `json:"content_type"`
 ByteSize int64 `json:"byte_size"`
}
// FutureOfferingMediaStage is the durable immutable-object staging contract.
type FutureOfferingMediaStage struct {
 ID, Owner uuid.UUID
 Driver, Location, ContentType, Digest string
 Size int64
}
// StageMedia records reconciliation evidence before object bytes are written.
func (m *MerchantFutureOfferingModel) StageMediaTx(ctx context.Context, tx pgx.Tx, v FutureOfferingMediaStage) error {
 if _,err:=tx.Exec(ctx,`SELECT pg_advisory_xact_lock(hashtextextended($1,947))`,v.Owner.String());err!=nil{return err}
 var count int
 if err:=tx.QueryRow(ctx,`SELECT count(*) FROM media_objects WHERE owner_user_id=$1 AND purpose LIKE 'future_offering_asset:%' AND state='staged'`,v.Owner).Scan(&count);err!=nil{return err}
 if count>=4{return ErrMerchantFutureOfferingEditConflict}
 _,err:=tx.Exec(ctx,`INSERT INTO media_objects(id,owner_user_id,purpose,storage_driver,storage_location,content_type,byte_size,sha256,state)
 VALUES($1,$2,$3,$4,$5,$6,$7,$8,'staged')`,v.ID,v.Owner,"future_offering_asset:"+v.ID.String(),v.Driver,v.Location,v.ContentType,v.Size,v.Digest)
 return err
}
// ListMediaTx reads an ordered gallery inside the caller's coherent transaction.
func (m *MerchantFutureOfferingModel) ListMediaTx(ctx context.Context,tx pgx.Tx,fo uuid.UUID)([]FutureOfferingMedia,error){
 rows,err:=tx.Query(ctx,`SELECT a.id,a.media_kind,a.alt_text,a.display_order,a.is_main,o.content_type,o.byte_size FROM future_offering_media a JOIN media_objects o ON o.id=a.id AND o.state='active' WHERE a.future_offering_id=$1 ORDER BY a.display_order,a.id`,fo)
 if err!=nil{return nil,err};defer rows.Close();out:=[]FutureOfferingMedia{}
 for rows.Next(){var v FutureOfferingMedia;if err=rows.Scan(&v.ID,&v.Kind,&v.AltText,&v.Order,&v.Main,&v.ContentType,&v.ByteSize);err!=nil{return nil,err};out=append(out,v)};return out,rows.Err()
}
// AttachMediaTx activates a still-staged object and adds its FO-owned reference.
func (m *MerchantFutureOfferingModel) AttachMediaTx(ctx context.Context,tx pgx.Tx,fo uuid.UUID,v FutureOfferingMedia)error{
 var state string
 if err:=tx.QueryRow(ctx,`SELECT state FROM media_objects WHERE id=$1 FOR UPDATE`,v.ID).Scan(&state);err!=nil{return err}
 if state!="staged"{return ErrMerchantFutureOfferingEditConflict}
 if v.Main {if _,err:=tx.Exec(ctx,`UPDATE future_offering_media SET is_main=FALSE WHERE future_offering_id=$1 AND is_main`,fo);err!=nil{return err}}
 if _,err:=tx.Exec(ctx,`INSERT INTO future_offering_media(id,future_offering_id,media_kind,alt_text,display_order,is_main) VALUES($1,$2,$3,$4,$5,$6)`,v.ID,fo,v.Kind,v.AltText,v.Order,v.Main);err!=nil{return err}
 _,err:=tx.Exec(ctx,`UPDATE media_objects SET state='active' WHERE id=$1`,v.ID);return err
}
// EditMediaTx updates display facts or retires a reference after FO-level checks.
func(m *MerchantFutureOfferingModel) EditMediaTx(ctx context.Context,tx pgx.Tx,fo uuid.UUID,v FutureOfferingMedia,remove bool)error{
 var id uuid.UUID
 if err:=tx.QueryRow(ctx,`SELECT id FROM future_offering_media WHERE id=$1 AND future_offering_id=$2 FOR UPDATE`,v.ID,fo).Scan(&id);err!=nil{if errors.Is(err,pgx.ErrNoRows){return ErrMerchantFutureOfferingNotFound};return err}
 if remove {
  if _,err:=tx.Exec(ctx,`DELETE FROM future_offering_media WHERE id=$1`,id);err!=nil{return err}
  _,err:=tx.Exec(ctx,`UPDATE media_objects SET state='retired',cleanup_after=clock_timestamp()+INTERVAL '15 minutes' WHERE id=$1`,id);return err
 }
 if v.Main {if _,err:=tx.Exec(ctx,`UPDATE future_offering_media SET is_main=FALSE WHERE future_offering_id=$1 AND is_main AND id<>$2`,fo,id);err!=nil{return err}}
 if _,err:=tx.Exec(ctx,`UPDATE future_offering_media SET display_order=display_order+1 WHERE future_offering_id=$1 AND id<>$2 AND display_order>=$3`,fo,id,v.Order);err!=nil{return err}
 _,err:=tx.Exec(ctx,`UPDATE future_offering_media SET alt_text=$2,display_order=$3,is_main=$4 WHERE id=$1`,id,v.AltText,v.Order,v.Main);return err
}
// AdvanceEditableVersionTx advances the aggregate version for a draft or active FO.
func(m *MerchantFutureOfferingModel) AdvanceEditableVersionTx(ctx context.Context,tx pgx.Tx,merchant,fo uuid.UUID,expected time.Time)(*MerchantFutureOffering,error){
 out:=new(MerchantFutureOffering)
 err:=scanMerchantFutureOffering(tx.QueryRow(ctx,`UPDATE merchant_future_offerings SET updated_at=`+merchantFutureOfferingNextVersionSQL+` WHERE id=$1 AND merchant_id=$2 AND updated_at=$3 AND deleted_at IS NULL AND status IN ('draft','published') RETURNING `+merchantFutureOfferingSelectColumns,fo,merchant,expected),out)
 if errors.Is(err,pgx.ErrNoRows){return nil,ErrMerchantFutureOfferingEditConflict};return out,err
}
// ActivateTx establishes open-ended publication with no commercial side effects.
func(m *MerchantFutureOfferingModel) ActivateTx(ctx context.Context,tx pgx.Tx,merchant,fo uuid.UUID,expected time.Time)(*MerchantFutureOffering,error){
 out:=new(MerchantFutureOffering)
 err:=scanMerchantFutureOffering(tx.QueryRow(ctx,`UPDATE merchant_future_offerings SET status='published',submitted_at=clock_timestamp(),published_at=clock_timestamp(),updated_at=`+merchantFutureOfferingNextVersionSQL+` WHERE id=$1 AND merchant_id=$2 AND status='draft' AND updated_at=$3 AND deleted_at IS NULL RETURNING `+merchantFutureOfferingSelectColumns,fo,merchant,expected),out)
 if errors.Is(err,pgx.ErrNoRows){return nil,ErrMerchantFutureOfferingEditConflict};if err!=nil{return nil,err}
 _,err=tx.Exec(ctx,`INSERT INTO future_offering_publication(future_offering_id,activated_from_version) VALUES($1,$2)`,fo,expected);return out,err
}
// PublicationTx returns visibility and activation provenance for safe retries.
func(m *MerchantFutureOfferingModel) PublicationTx(ctx context.Context,tx pgx.Tx,fo uuid.UUID)(bool,time.Time,error){
 var visible bool;var source time.Time;err:=tx.QueryRow(ctx,`SELECT brand_visible,activated_from_version FROM future_offering_publication WHERE future_offering_id=$1`,fo).Scan(&visible,&source);return visible,source,err
}
// SetBrandVisibilityTx changes listing membership without changing publication.
func(m *MerchantFutureOfferingModel) SetBrandVisibilityTx(ctx context.Context,tx pgx.Tx,fo uuid.UUID,visible bool)error{
 _,err:=tx.Exec(ctx,`UPDATE future_offering_publication SET brand_visible=$2 WHERE future_offering_id=$1`,fo,visible);return err
}
// ClosePublishedTx retires an unlaunched offering without removing its history.
func(m *MerchantFutureOfferingModel) ClosePublishedTx(ctx context.Context,tx pgx.Tx,merchant,fo uuid.UUID,expected time.Time,reason string)(*MerchantFutureOffering,error){
 out:=new(MerchantFutureOffering)
 err:=scanMerchantFutureOffering(tx.QueryRow(ctx,`UPDATE merchant_future_offerings SET status='archived',archived_at=clock_timestamp(),updated_at=`+merchantFutureOfferingNextVersionSQL+` WHERE id=$1 AND merchant_id=$2 AND status='published' AND updated_at=$3 AND deleted_at IS NULL RETURNING `+merchantFutureOfferingSelectColumns,fo,merchant,expected),out)
 if errors.Is(err,pgx.ErrNoRows){return nil,ErrMerchantFutureOfferingEditConflict};if err!=nil{return nil,err}
 _,err=tx.Exec(ctx,`UPDATE future_offering_publication SET close_reason=$2,closed_at=clock_timestamp() WHERE future_offering_id=$1`,fo,reason);return out,err
}

// PublicOfferingTx returns a canonical FO only through an explicit publication gate.
// access_policy governs participation, never the visibility of this projection.
// Activation publishes only these approved public facts; private facts stay in
// the merchant domain and are excluded by the HTTP DTO.
// Private projections must still be constructed by the HTTP boundary.
func(m *MerchantFutureOfferingModel) PublicOfferingTx(ctx context.Context,tx pgx.Tx,id uuid.UUID)(*MerchantFutureOffering,string,error){
 var brand string
 err:=tx.QueryRow(ctx,`SELECT m.name FROM merchant_future_offerings f JOIN merchants m ON m.id=f.merchant_id JOIN merchant_accounts a ON a.merchant_id=m.id JOIN users u ON u.id=a.principal_user_id AND u.is_active AND u.deleted_at IS NULL JOIN future_offering_publication p ON p.future_offering_id=f.id WHERE f.id=$1 AND f.status='published' AND f.deleted_at IS NULL AND a.account_status='active' AND a.deleted_at IS NULL AND m.deleted_at IS NULL AND p.closed_at IS NULL FOR SHARE OF f`,id).Scan(&brand)
 if errors.Is(err,pgx.ErrNoRows){return nil,"",ErrMerchantFutureOfferingNotFound};if err!=nil{return nil,"",err}
 fo,err:=m.getByIDViaQuerier(ctx,tx,id,false);return fo,brand,err
}
// PreviewBrandTx reads only the public brand label for a merchant-owned preview.
func(m *MerchantFutureOfferingModel) PreviewBrandTx(ctx context.Context,tx pgx.Tx,merchant uuid.UUID)(string,error){
 var name string;err:=tx.QueryRow(ctx,`SELECT name FROM merchants WHERE id=$1`,merchant).Scan(&name);return name,err
}

// RecordRevisionTx stores immutable private history and a versioned, minimal
// downstream event atomically with the change. Public DTOs never expose snapshots.
func(m *MerchantFutureOfferingModel) RecordRevisionTx(ctx context.Context,tx pgx.Tx,fo,actor uuid.UUID,version time.Time,operation string,before,after any)(uuid.UUID,error){
 var id uuid.UUID
 b,err:=json.Marshal(before);if err!=nil{return id,err};a,err:=json.Marshal(after);if err!=nil{return id,err}
 err=tx.QueryRow(ctx,`INSERT INTO future_offering_revisions(future_offering_id,version,operation,actor_id,before_facts,after_facts) VALUES($1,$2,$3,$4,$5::jsonb,$6::jsonb) RETURNING id`,fo,version,operation,actor,string(b),string(a)).Scan(&id);return id,err
}

// FutureOfferingBetaCapacity separates optional offered seats from expressed demand.
type FutureOfferingBetaCapacity struct {
 Seats *int `json:"seats"`
 Taken int64 `json:"taken"`
 UpdatedAt time.Time `json:"updated_at"`
}
// BetaCapacityTx must be called after the FO lock; no consumer identities leave it.
func(m *MerchantFutureOfferingModel) BetaCapacityTx(ctx context.Context,tx pgx.Tx,fo uuid.UUID)(FutureOfferingBetaCapacity,error){
 var result FutureOfferingBetaCapacity
 err:=tx.QueryRow(ctx,`SELECT c.seats,f.updated_at,COALESCE((SELECT sum(COALESCE(s.quantity,1)) FROM user_future_offering_engagement_action_selections s JOIN user_future_offering_engagements e ON e.id=s.engagement_id JOIN merchant_future_offering_engagement_options o ON o.id=s.engagement_option_id JOIN engagement_actions a ON a.id=o.engagement_action_id WHERE e.future_offering_id=f.id AND e.deleted_at IS NULL AND e.engagement_status IN ('active','muted') AND a.code='beta'),0) FROM merchant_future_offerings f LEFT JOIN future_offering_beta_capacity c ON c.future_offering_id=f.id WHERE f.id=$1`,fo).Scan(&result.Seats,&result.UpdatedAt,&result.Taken)
 return result,err
}
// SetBetaCapacityTx preserves all current expressed Beta demand.
func(m *MerchantFutureOfferingModel) SetBetaCapacityTx(ctx context.Context,tx pgx.Tx,fo uuid.UUID,seats *int)error{
 if seats!=nil&&(*seats<1||*seats>1000000000){return ErrMerchantFutureOfferingInvalidInput}
 current,err:=m.BetaCapacityTx(ctx,tx,fo);if err!=nil{return err};if seats!=nil&&int64(*seats)<current.Taken{return ErrMerchantFutureOfferingInvalidInput}
 _,err=tx.Exec(ctx,`INSERT INTO future_offering_beta_capacity(future_offering_id,seats) VALUES($1,$2) ON CONFLICT(future_offering_id) DO UPDATE SET seats=EXCLUDED.seats`,fo,seats);return err
}

// PublicOfferingPageTx implements keyset discovery. A brand filter also applies
// merchant-chosen brand-page membership; hidden membership never unpublishes FO.
func(m *MerchantFutureOfferingModel) PublicOfferingPageTx(ctx context.Context,tx pgx.Tx,merchant *uuid.UUID,before *time.Time,beforeID *uuid.UUID,category *uuid.UUID)([]uuid.UUID,error){
 rows,err:=tx.Query(ctx,`SELECT f.id FROM merchant_future_offerings f JOIN future_offering_publication p ON p.future_offering_id=f.id JOIN merchant_accounts a ON a.merchant_id=f.merchant_id JOIN merchants m ON m.id=f.merchant_id AND m.deleted_at IS NULL JOIN users u ON u.id=a.principal_user_id AND u.is_active AND u.deleted_at IS NULL WHERE f.status='published' AND f.deleted_at IS NULL AND a.account_status='active' AND a.deleted_at IS NULL AND m.deleted_at IS NULL AND p.closed_at IS NULL AND ($1::uuid IS NULL OR (f.merchant_id=$1 AND p.brand_visible)) AND ($2::timestamptz IS NULL OR (f.published_at,f.id)<($2,$3::uuid)) AND ($4::uuid IS NULL OR f.category_id=$4) ORDER BY f.published_at DESC,f.id DESC LIMIT 25`,merchant,before,beforeID,category)
 if err!=nil{return nil,err};defer rows.Close();out:=[]uuid.UUID{};for rows.Next(){var id uuid.UUID;if err=rows.Scan(&id);err!=nil{return nil,err};out=append(out,id)};return out,rows.Err()
}
