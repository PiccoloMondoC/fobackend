// Package services verifies technical media normalization and lifecycle guards.
// focodebase/fobackend/internal/services/future_offering_publication_test.go
package services
import (
 "bytes"
 "image"
 "image/png"
 "testing"
 "time"
 "github.com/PiccoloMondoC/focodebase/fobackend/internal/data"
)
func TestOfferingImagesAreDecodedAndReencoded(t *testing.T){
 var raw bytes.Buffer;if err:=png.Encode(&raw,image.NewRGBA(image.Rect(0,0,2,2)));err!=nil{t.Fatal(err)}
 raw.WriteString("<script>not retained image metadata</script>")
 normalized,mime,kind,err:=normalizeOfferingMedia(raw.Bytes());if err!=nil{t.Fatal(err)}
 if mime!="image/jpeg"||kind!="image"||bytes.Contains(normalized,[]byte("script")){t.Fatal("image not canonicalized")}
 if _,_,err=image.Decode(bytes.NewReader(normalized));err!=nil{t.Fatal(err)}
}
func TestOfferingMediaRejectsMarkupAndEmptyPayloads(t *testing.T){
 for _,raw:=range [][]byte{nil,[]byte("<svg xmlns='http://www.w3.org/2000/svg'><script/></svg>"),[]byte("not media")}{if _,_,_,err:=normalizeOfferingMedia(raw);err==nil{t.Fatal("unsafe payload accepted")}}
}
func TestOfferingVersionAndLifecycleGuard(t *testing.T){
 version:=time.Date(2026,1,1,0,0,0,123456000,time.UTC)
 for _,status:=range []data.MerchantFutureOfferingStatus{"draft","published"}{if err:=offeringEditable(&data.MerchantFutureOffering{Status:status,UpdatedAt:version},version);err!=nil{t.Fatal(err)}}
 if offeringEditable(&data.MerchantFutureOffering{Status:"archived",UpdatedAt:version},version)==nil{t.Fatal("closed offering editable")}
 if offeringEditable(&data.MerchantFutureOffering{Status:"published",UpdatedAt:version},version.Add(time.Microsecond))==nil{t.Fatal("stale write accepted")}
}
