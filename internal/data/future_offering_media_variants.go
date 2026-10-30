// Package data persists privately stored image sets. Callers own transactions
// and hold the FO lock before these methods. No storage I/O occurs here.
// focodebase/fobackend/internal/data/future_offering_media_variants.go
package data

import (
	"context"
	"errors"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type FutureOfferingMediaObject struct {
	FutureOfferingMediaStage
	Role string
}

var ErrFutureOfferingMediaStagingBusy=errors.New("offering media staging busy")

// StageMediaBundleTx reserves all object keys in one transaction before any Put.
// Quotas count logical uploads, not their implementation-specific object count.
func(m *MerchantFutureOfferingModel) StageMediaBundleTx(ctx context.Context,tx pgx.Tx,asset uuid.UUID,parts []FutureOfferingMediaObject)error{
	if err:=validMediaBundle(asset,parts);err!=nil{return err}
	owner:=parts[0].Owner
	if _,err:=tx.Exec(ctx,`SELECT pg_advisory_xact_lock(hashtextextended($1,947))`,owner.String());err!=nil{return err}
	var count int
	if err:=tx.QueryRow(ctx,`SELECT count(DISTINCT split_part(purpose,':',2)) FROM media_objects WHERE owner_user_id=$1 AND purpose LIKE 'future_offering_asset:%' AND state='staged'`,owner).Scan(&count);err!=nil{return err}
	if count>=4{return ErrFutureOfferingMediaStagingBusy}
	for _,p:=range parts{
		_,err:=tx.Exec(ctx,`INSERT INTO media_objects(id,owner_user_id,purpose,storage_driver,storage_location,content_type,byte_size,sha256,state) VALUES($1,$2,$3,$4,$5,$6,$7,$8,'staged')`,p.ID,p.Owner,"future_offering_asset:"+asset.String()+":"+p.Role,p.Driver,p.Location,p.ContentType,p.Size,p.Digest)
		if err!=nil{return err}
	}
	return nil
}

func validMediaBundle(asset uuid.UUID,parts []FutureOfferingMediaObject)error{
	invalid:=ErrMerchantFutureOfferingInvalidInput
	if asset==uuid.Nil||(len(parts)!=1&&len(parts)!=4){return invalid}
	seen:=map[uuid.UUID]bool{};roles:=map[string]bool{}
	for _,p:=range parts{
		if p.ID==uuid.Nil||p.Owner==uuid.Nil||seen[p.ID]||roles[p.Role]||p.Owner!=parts[0].Owner||p.Driver!=parts[0].Driver||p.Location!=parts[0].Location||p.Location==""||(p.Driver!="filesystem"&&p.Driver!="gcs")||p.Size<1||p.Size>64<<20||len(p.Digest)!=64||strings.IndexFunc(p.Digest,func(r rune)bool{return !(r>='0'&&r<='9'||r>='a'&&r<='f')})>=0{return invalid}
		if p.Role=="detail" {if p.ID!=asset{return invalid}}else if p.ID==asset{return invalid}
		if len(parts)==1 {
			if p.Role!="detail"||(p.ContentType!="video/mp4"&&p.ContentType!="video/webm"){return invalid}
		}else{
			switch p.Role{
			case "detail","card","thumbnail":if p.ContentType!="image/webp"||p.Size>8<<20{return invalid}
			case "original":if (p.ContentType!="image/jpeg"&&p.ContentType!="image/png")||p.Size>20<<20{return invalid}
			default:return invalid
			}
		}
		seen[p.ID]=true;roles[p.Role]=true
	}
	if !roles["detail"]{return invalid}
	return nil
}

// AttachMediaBundleTx locks the complete set in UUID order and refuses expired,
// retired or foreign staging evidence. Publication never sees a partial set.
func(m *MerchantFutureOfferingModel) AttachMediaBundleTx(ctx context.Context,tx pgx.Tx,fo uuid.UUID,v FutureOfferingMedia,parts []FutureOfferingMediaObject)error{
	if err:=validMediaBundle(v.ID,parts);err!=nil{return err}
	if (v.Kind=="image")!=(len(parts)==4)||(v.Kind!="image"&&v.Kind!="video"){return ErrMerchantFutureOfferingInvalidInput}
	ids:=make([]uuid.UUID,0,len(parts));wanted:=map[uuid.UUID]FutureOfferingMediaObject{}
	for _,p:=range parts{ids=append(ids,p.ID);wanted[p.ID]=p}
	rows,err:=tx.Query(ctx,`SELECT id,state,owner_user_id,purpose,content_type,byte_size,sha256,storage_driver,storage_location FROM media_objects WHERE id=ANY($1::uuid[]) ORDER BY id FOR UPDATE`,ids)
	if err!=nil{return err}
	count:=0
	for rows.Next(){
		var id,owner uuid.UUID;var state,purpose,mime,digest,driver,location string;var size int64
		if err=rows.Scan(&id,&state,&owner,&purpose,&mime,&size,&digest,&driver,&location);err!=nil{rows.Close();return err}
		p:=wanted[id]
		if state!="staged"||owner!=p.Owner||purpose!="future_offering_asset:"+v.ID.String()+":"+p.Role||mime!=p.ContentType||size!=p.Size||digest!=p.Digest||driver!=p.Driver||location!=p.Location{rows.Close();return ErrMerchantFutureOfferingEditConflict};count++
	}
	err=rows.Err();rows.Close();if err!=nil{return err};if count!=len(parts){return ErrMerchantFutureOfferingEditConflict}
	if err=m.AttachMediaTx(ctx,tx,fo,v);err!=nil{return err}
	for _,p:=range parts{
		if p.Role=="detail"{continue}
		if _,err=tx.Exec(ctx,`INSERT INTO future_offering_media_variants(asset_id,role,object_id) VALUES($1,$2,$3)`,v.ID,p.Role,p.ID);err!=nil{return err}
	}
	_,err=tx.Exec(ctx,`UPDATE media_objects SET state='active' WHERE id=ANY($1::uuid[])`,ids);return err
}

// ResolveMediaVariantTx is called only after the parent gallery and public or
// merchant scope have been verified. It does not grant access by object UUID.
// Legacy normalized uploads have no original; small views may use their parent.
func(m *MerchantFutureOfferingModel) ResolveMediaVariantTx(ctx context.Context,tx pgx.Tx,asset FutureOfferingMedia,role string)(uuid.UUID,FutureOfferingMedia,error){
	if role==""||role=="detail"{return asset.ID,asset,nil}
	if role!="card"&&role!="thumbnail"&&role!="original"{return uuid.Nil,asset,ErrMerchantFutureOfferingInvalidInput}
	if asset.Kind!="image"{return uuid.Nil,asset,ErrMerchantFutureOfferingNotFound}
	var object uuid.UUID
	err:=tx.QueryRow(ctx,`SELECT o.id,o.content_type,o.byte_size,o.sha256 FROM future_offering_media_variants v JOIN media_objects o ON o.id=v.object_id AND o.state='active' WHERE v.asset_id=$1 AND v.role=$2`,asset.ID,role).Scan(&object,&asset.ContentType,&asset.ByteSize,&asset.Digest)
	if errors.Is(err,pgx.ErrNoRows)&&role!="original"{
		var exists bool
		if err=tx.QueryRow(ctx,`SELECT EXISTS(SELECT 1 FROM future_offering_media_variants WHERE asset_id=$1)`,asset.ID).Scan(&exists);err!=nil{return uuid.Nil,asset,err}
		if !exists{return asset.ID,asset,nil}
		return uuid.Nil,asset,ErrMerchantFutureOfferingNotFound
	}
	if errors.Is(err,pgx.ErrNoRows){err=ErrMerchantFutureOfferingNotFound}
	return object,asset,err
}

// RetireMediaBundleTx holds all object locks before deleting gallery references.
// References cascade away, while registry tombstones survive until byte cleanup.
func(m *MerchantFutureOfferingModel) RetireMediaBundleTx(ctx context.Context,tx pgx.Tx,asset uuid.UUID)error{
	rows,err:=tx.Query(ctx,`SELECT id FROM media_objects WHERE id=$1 OR id IN (SELECT object_id FROM future_offering_media_variants WHERE asset_id=$1) ORDER BY id FOR UPDATE`,asset)
	if err!=nil{return err};ids:=[]uuid.UUID{}
	for rows.Next(){var id uuid.UUID;if err=rows.Scan(&id);err!=nil{rows.Close();return err};ids=append(ids,id)}
	err=rows.Err();rows.Close();if err!=nil{return err}
	_,err=tx.Exec(ctx,`UPDATE media_objects SET state='retired',cleanup_after=clock_timestamp()+INTERVAL '15 minutes' WHERE id=ANY($1::uuid[])`,ids)
	return err
}
