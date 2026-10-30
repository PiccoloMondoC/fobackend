// Package services tests image preparation, private originals and delivery integrity.
// focodebase/fobackend/internal/services/future_offering_image_variants_test.go
package services

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"image"
	"image/color"
	"image/png"
	"io"
	"testing"

	"github.com/PiccoloMondoC/focodebase/fobackend/internal/data"
	"github.com/PiccoloMondoC/focodebase/fobackend/internal/mediastore"
)

func TestOfferingImageSetPreservesPrivateSourceAndSanitizesCodecInput(t *testing.T){
	img:=image.NewNRGBA(image.Rect(0,0,1280,640));img.SetNRGBA(0,0,color.NRGBA{R:255,A:128})
	var input bytes.Buffer;if err:=png.Encode(&input,img);err!=nil{t.Fatal(err)}
	input.WriteString("private trailing metadata")
	var dimensions [][2]int
	encode:=func(_ context.Context,raw []byte,w,h int)([]byte,error){
		dimensions=append(dimensions,[2]int{w,h})
		if bytes.Contains(raw,[]byte("private trailing metadata")){t.Fatal("source metadata reached codec")}
		decoded,_,err:=image.Decode(bytes.NewReader(raw));if err!=nil{t.Fatal(err)}
		_,_,_,a:=decoded.At(0,0).RGBA();if a==0||a==65535{t.Fatal("alpha not preserved")}
		return []byte("test codec output"),nil
	}
	set,err:=prepareOfferingMediaWith(context.Background(),input.Bytes(),encode);if err!=nil{t.Fatal(err)}
	if set.kind!="image"||len(set.parts)!=4||set.parts[3].role!="original"||!bytes.Equal(set.parts[3].bytes,input.Bytes()){t.Fatal("exact private original missing")}
	want:=[][2]int{{1280,640},{640,320},{160,80}}
	for i,size:=range want{if dimensions[i]!=size{t.Fatalf("size %d: %v",i,dimensions[i])};if set.parts[i].contentType!="image/webp"{t.Fatal("wrong display format")}}
}
func TestOfferingImagesDoNotUpscaleOrExposePartialEncodes(t *testing.T){
	for _,tc:=range []struct{w,h,edge,x,y int}{{100,50,640,100,50},{4000,2000,2048,2048,1024},{1,8000,160,1,160},{8000,1,160,160,1}}{
		w,h:=fitOfferingImage(tc.w,tc.h,tc.edge);if w!=tc.x||h!=tc.y{t.Fatalf("bad fit %d,%d",w,h)}
	}
	var input bytes.Buffer;_ = png.Encode(&input,image.NewNRGBA(image.Rect(0,0,2,2)))
	calls:=0
	set,err:=prepareOfferingMediaWith(context.Background(),input.Bytes(),func(context.Context,[]byte,int,int)([]byte,error){calls++;if calls==2{return nil,errors.New("codec failed")};return []byte{1},nil})
	if !errors.Is(err,mediastore.ErrUnavailable)||len(set.parts)!=0{t.Fatal("partial encoding accepted")}
}
func TestOfferingOriginalHasNoPublicResolution(t *testing.T){
	if offeringMediaVariantAllowed("original",false)||!offeringMediaVariantAllowed("original",true){t.Fatal("original access boundary failed")}
	for _,role:=range []string{"../original","ORIGINAL","http://example.invalid","unknown"}{if offeringMediaVariantAllowed(role,true){t.Fatal("unsupported role accepted")}}
	for _,role:=range []string{"","detail","card","thumbnail"}{if !offeringMediaVariantAllowed(role,false){t.Fatal("public size refused")}}
}
func TestOfferingImageIntegrityBeforeDelivery(t *testing.T){
	raw:=[]byte("stored image bytes");sum:=sha256.Sum256(raw)
	meta:=data.FutureOfferingMedia{ByteSize:int64(len(raw)),Digest:hex.EncodeToString(sum[:])}
	verified,err:=verifiedOfferingImage(io.NopCloser(bytes.NewReader(raw)),meta);if err!=nil{t.Fatal(err)}
	got,_:=io.ReadAll(verified);_ = verified.Close();if !bytes.Equal(got,raw){t.Fatal("valid bytes changed")}
	for _,bad:=range [][]byte{raw[:len(raw)-1],append(append([]byte{},raw...),1),bytes.Repeat([]byte{0},len(raw))}{
		if _,err=verifiedOfferingImage(io.NopCloser(bytes.NewReader(bad)),meta);!errors.Is(err,mediastore.ErrUnavailable){t.Fatal("corrupt bytes served")}
	}
}

func TestOfferingImageReadLeaseLivesUntilClose(t *testing.T){
	before:=len(offeringImageReads)
	release,err:=acquireOfferingImageRead(context.Background());if err!=nil{t.Fatal(err)}
	reader:=&offeringImageReader{ReadCloser:io.NopCloser(bytes.NewReader([]byte{1})),release:release}
	if len(offeringImageReads)!=before+1{t.Fatal("read has no memory lease")}
	_ = reader.Close();_ = reader.Close()
	if len(offeringImageReads)!=before{t.Fatal("image read lease leaked")}
	ctx,cancel:=context.WithCancel(context.Background());cancel()
	if release,err=acquireOfferingImageRead(ctx);err==nil{release();t.Fatal("canceled request acquired a read lease")}
}
