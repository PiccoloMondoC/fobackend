// Package mediaimage tests codec bounds, framing and installed encoding.
// focodebase/fobackend/internal/mediaimage/webp_test.go
package mediaimage

import (
	"bytes"
	"context"
	"encoding/binary"
	"image"
	"image/color"
	"image/png"
	"os"
	"testing"
)

// A framing fixture is not claimed to be decodable compressed pixels. Real
// codec coverage below separately exercises the installed WebP encoder.
func webpFrame(width,height int)[]byte{
	payload:=[]byte{0,0,0,0x9d,0x01,0x2a,byte(width),byte(width>>8),byte(height),byte(height>>8)}
	raw:=append([]byte("RIFF\x00\x00\x00\x00WEBPVP8 \x0a\x00\x00\x00"),payload...)
	binary.LittleEndian.PutUint32(raw[4:8],uint32(len(raw)-8));return raw
}
func TestWebPFramingRejectsUnexpectedDimensionsAndMetadata(t *testing.T){
	raw:=webpFrame(640,320)
	if !validWebP(raw,640,320)||validWebP(raw,320,640){t.Fatal("dimensions not enforced")}
	withMetadata:=append(append([]byte{},raw...),[]byte("EXIF\x02\x00\x00\x00xx")...)
	binary.LittleEndian.PutUint32(withMetadata[4:8],uint32(len(withMetadata)-8))
	if validWebP(withMetadata,640,320){t.Fatal("metadata accepted")}
	for i:=0;i<len(raw);i++{if validWebP(raw[:i],640,320){t.Fatalf("truncation %d accepted",i)}}
}
func TestEncoderOutputIsBounded(t *testing.T){
	var out limitedOutput
	if _,err:=out.Write(make([]byte,MaxOutputBytes));err!=nil{t.Fatal(err)}
	if _,err:=out.Write([]byte{1});err==nil||out.Len()!=MaxOutputBytes{t.Fatal("encoder output bound failed")}
}
func TestWebPEncoderInstalledIntegration(t *testing.T){
	if _,err:=os.Stat(Executable);os.IsNotExist(err){t.Skip("installed cwebp unavailable; run the Docker test target for real codec coverage")}
	img:=image.NewNRGBA(image.Rect(0,0,40,20));img.SetNRGBA(0,0,color.NRGBA{R:255,A:128})
	var input bytes.Buffer;if err:=png.Encode(&input,img);err!=nil{t.Fatal(err)}
	raw,err:=EncodePNG(context.Background(),input.Bytes(),20,10);if err!=nil{t.Fatal(err)}
	if !validWebP(raw,20,10){t.Fatal("real encoder framing/dimensions invalid")}
	if err=Check(context.Background());err!=nil{t.Fatal(err)}
	ctx,cancel:=context.WithCancel(context.Background());cancel()
	if _,err=EncodePNG(ctx,input.Bytes(),20,10);err==nil{t.Fatal("canceled encoding succeeded")}
}
