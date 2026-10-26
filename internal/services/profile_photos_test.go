// Package services tests bounded photo decoding, safe normalization, and rejection of active content.
// focodebase/fobackend/internal/services/profile_photos_test.go

package services
import (
 "bytes"
 "image"
 "image/png"
 "testing"
)
func TestCEProfilePhotoValidationAndNormalization(t *testing.T){
 for _,raw:=range [][]byte{nil,[]byte(`<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`),make([]byte,ProfilePhotoMaxBytes+1)}{
  if _,err:=normalizeProfilePhoto(raw);err==nil{t.Fatal("invalid photo accepted")}
 }
 var source bytes.Buffer
 if err:=png.Encode(&source,image.NewRGBA(image.Rect(0,0,2,2)));err!=nil{t.Fatal(err)}
 result,err:=normalizeProfilePhoto(source.Bytes());if err!=nil{t.Fatal(err)}
 cfg,format,err:=image.DecodeConfig(bytes.NewReader(result));if err!=nil||format!="jpeg"||cfg.Width!=2||cfg.Height!=2{t.Fatal("normalized image contract failed")}
}
