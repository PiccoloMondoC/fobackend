// Package main verifies public minimization and bounded HTTP media range semantics.
// focodebase/fobackend/internal/server/cmd/api/future_offering_publication_test.go
package main
import (
 "encoding/json"
 "strings"
 "testing"
 "github.com/PiccoloMondoC/focodebase/fobackend/internal/data"
 "github.com/PiccoloMondoC/focodebase/fobackend/internal/services"
 "github.com/google/uuid"
)
func TestOfferingPublicProjectionExcludesMerchantPrivateFacts(t *testing.T){
 title:="Public product";fo:=&data.MerchantFutureOffering{ID:uuid.New(),MerchantID:uuid.New(),ProjectName:"private planning identifier",Title:&title,Summary:"Public summary"}
 raw,err:=json.Marshal(offeringPublicResponse(services.FutureOfferingPreview{Offering:fo,Brand:"Public brand",Media:[]data.FutureOfferingMedia{}}));if err!=nil{t.Fatal(err)}
 for _,forbidden:=range []string{"private planning identifier","project_name","legal_name","website","owner_user_id","storage_location","submitted_at","updated_by"}{if strings.Contains(string(raw),forbidden){t.Fatalf("private fact %q leaked",forbidden)}}
 if !strings.Contains(string(raw),"Public brand"){t.Fatal("public brand missing")}
}
func TestOfferingMediaRanges(t *testing.T){
 tests:=[]struct{header string;size,start,end int64;partial,bad bool}{
 {"",10,0,9,false,false},{"bytes=0-0",10,0,0,true,false},{"bytes=8-",10,8,9,true,false},{"bytes=-3",10,7,9,true,false},{"bytes=-99",10,0,9,true,false},{"bytes=2-99",10,2,9,true,false},
 {"bytes=10-",10,0,0,false,true},{"bytes=4-2",10,0,0,false,true},{"bytes=-0",10,0,0,false,true},{"bytes=0-1,3-4",10,0,0,false,true},{"bytes=9223372036854775808-",10,0,0,false,true},{"items=0-1",10,0,0,false,true},
 }
 for _,tc:=range tests{t.Run(tc.header,func(t *testing.T){start,end,partial,err:=offeringMediaRange(tc.header,tc.size);if (err!=nil)!=tc.bad{t.Fatalf("error=%v",err)};if !tc.bad&&(start!=tc.start||end!=tc.end||partial!=tc.partial){t.Fatalf("got %d-%d partial=%v",start,end,partial)}})}
}
