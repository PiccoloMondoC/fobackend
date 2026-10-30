// Package services composes all-or-nothing offering image sets.
// focodebase/fobackend/internal/services/future_offering_image_variants.go
package services

import (
	"bytes"
	"context"
	"image"
	"image/draw"
	_ "image/jpeg"
	"image/png"
	"net/http"
	"time"

	"github.com/PiccoloMondoC/focodebase/fobackend/internal/data"
	"github.com/PiccoloMondoC/focodebase/fobackend/internal/mediaimage"
	"github.com/PiccoloMondoC/focodebase/fobackend/internal/mediastore"
)

type offeringMediaPart struct { role, contentType string; bytes []byte }
type offeringPreparedMedia struct { kind string; parts []offeringMediaPart }

// fitOfferingImage never manufactures resolution. A small original produces
// small derivatives; a merchant must supply a better source for better detail.
func fitOfferingImage(width,height,edge int)(int,int){
	if width<=edge&&height<=edge{return width,height}
	if width>=height {h:=height*edge/width;if h<1{h=1};return edge,h}
	w:=width*edge/height;if w<1{w=1};return w,edge
}

// prepareOfferingMedia retains the exact accepted image privately. Only decoded,
// oriented, metadata-free pixels feed the codec. Video behaviour is unchanged.
func prepareOfferingMedia(ctx context.Context,raw []byte)(offeringPreparedMedia,error){
	return prepareOfferingMediaWith(ctx,raw,mediaimage.EncodePNG)
}
func prepareOfferingMediaWith(ctx context.Context,raw []byte,encode func(context.Context,[]byte,int,int)([]byte,error))(offeringPreparedMedia,error){
	var result offeringPreparedMedia
	invalid:=data.ErrMerchantFutureOfferingInvalidInput
	if ctx==nil||encode==nil||len(raw)==0||len(raw)>offeringMediaTransportLimit{return result,invalid}
	if err:=ctx.Err();err!=nil{return result,err}
	mime:=http.DetectContentType(raw)
	if mime=="video/mp4"||mime=="video/webm"{return offeringPreparedMedia{kind:"video",parts:[]offeringMediaPart{{role:"detail",contentType:mime,bytes:raw}}},nil}
	if (mime!="image/jpeg"&&mime!="image/png")||len(raw)>20<<20{return result,invalid}
	cfg,_,err:=image.DecodeConfig(bytes.NewReader(raw))
	if err!=nil||cfg.Width<1||cfg.Height<1||cfg.Width>8000||cfg.Height>8000||int64(cfg.Width)*int64(cfg.Height)>16000000{return result,invalid}
	decoded,_,err:=image.Decode(bytes.NewReader(raw));if err!=nil{return result,invalid}
	canvas:=image.NewRGBA(decoded.Bounds())
	draw.Draw(canvas,canvas.Bounds(),decoded,decoded.Bounds().Min,draw.Src)
	oriented:=orientProfilePhoto(canvas,profilePhotoOrientation(raw))
	var canonical bytes.Buffer
	if err=png.Encode(&canonical,oriented);err!=nil{return result,invalid}
	run,cancel:=context.WithTimeout(ctx,45*time.Second);defer cancel()
	result.kind="image"
	for _,profile:=range []struct{role string;edge int}{{"detail",2048},{"card",640},{"thumbnail",160}}{
		if err=run.Err();err!=nil{return offeringPreparedMedia{},err}
		width,height:=fitOfferingImage(oriented.Bounds().Dx(),oriented.Bounds().Dy(),profile.edge)
		encoded,err:=encode(run,canonical.Bytes(),width,height)
		if err!=nil{
			if ctx.Err()!=nil{return offeringPreparedMedia{},ctx.Err()}
			return offeringPreparedMedia{},mediastore.ErrUnavailable
		}
		result.parts=append(result.parts,offeringMediaPart{role:profile.role,contentType:"image/webp",bytes:encoded})
	}
	result.parts=append(result.parts,offeringMediaPart{role:"original",contentType:mime,bytes:raw})
	return result,nil
}
