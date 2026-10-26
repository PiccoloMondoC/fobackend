// Package main tests profile request decoding, ownership field restrictions, and edit preconditions.
// focodebase/fobackend/internal/server/cmd/api/profile_contract_test.go

package main
import (
 "errors"
 "strings"
 "testing"
 "github.com/PiccoloMondoC/focodebase/fobackend/internal/data"
)
func TestCEProfileRequestDistinguishesAbsentAndNull(t *testing.T){
 changes,version,err:=decodeProfileBody(strings.NewReader(`{"bio":null,"expected_updated_at":"2026-10-17T12:30:00.123456Z"}`),false)
 if err!=nil||version==nil{t.Fatal(err)}
 if _,present:=changes["full_legal_name"];present{t.Fatal("omitted field became present")}
 if value,present:=changes["bio"];!present||value!=nil{t.Fatal("null was lost")}
}
func TestCEProfileRequestRejectsAmbiguityAndAuthorityFields(t *testing.T){
 cases:=[]string{`null`,`[]`,`{"bio":"a","bio":"b"}`,`{"user_id":"other"}`,`{"is_flagged":false}`,`{"bio":22}`,`{"full_legal_name":"A"} {}`,`{"full_legal_name":`, `{"expected_updated_at":null}`,`{"expected_updated_at":"invalid"}`}
 for _,body:=range cases{if _,_,err:=decodeProfileBody(strings.NewReader(body),true);err==nil{t.Fatalf("invalid create request accepted: %s",body)}}
 if _,_,err:=decodeProfileBody(strings.NewReader(`{"bio":"x"}`),false);!errors.Is(err,data.ErrProfileVersionRequired){t.Fatal("unversioned edit accepted")}
 if _,_,err:=decodeProfileBody(strings.NewReader(`{"bio":"x","expected_updated_at":null}`),false);err==nil{t.Fatal("null version accepted")}
}

func TestCELegacySplitNamesAreNotAcceptedAsProfileInput(t *testing.T){
 for _,body:=range []string{`{"first_name":"Ada"}`,`{"last_name":"Lovelace"}`,`{"display_name":"Ada"}`} {
  if _,_,err:=decodeProfileBody(strings.NewReader(body),true);err==nil{t.Fatal("unsupported identity field accepted")}
 }
}
