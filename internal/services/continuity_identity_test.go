// Package services verifies historical and current continuity identity references without changing ceremony policy.
// focodebase/fobackend/internal/services/continuity_identity_test.go

package services

import (
 "encoding/json"
 "testing"
 "github.com/google/uuid"
 "github.com/PiccoloMondoC/focodebase/fobackend/internal/data"
)
func TestContinuityIdentityReferencesIncludeFormerActors(t *testing.T){
 a:=uuid.MustParse("11111111-1111-4111-8111-111111111111");b:=uuid.MustParse("22222222-2222-4222-8222-222222222222");past:=uuid.MustParse("33333333-3333-4333-8333-333333333333")
 raw:=json.RawMessage(`{"incumbent":{"user_id":"11111111-1111-4111-8111-111111111111"},"co_root":null,"successors":[{"user_id":"22222222-2222-4222-8222-222222222222","designated_by":"11111111-1111-4111-8111-111111111111"}],"honorary":[],"proposals":[]}`)
 digest:=ContinuityDigest(raw)
 ids,err:=continuityIdentityIDs(raw,[]data.ContinuityEvent{{ActorID:&past,TargetID:&b}});if err!=nil{t.Fatal(err)}
 if len(ids)!=3 || ids[0]!=a || ids[1]!=b || ids[2]!=past{t.Fatal("historical actor missing or references changed")}
 if ContinuityDigest(raw)!=digest{t.Fatal("identity extraction changed ceremony state")}
}
func TestContinuityIdentityReferencesRejectMalformedState(t *testing.T){
 if _,err:=continuityIdentityIDs(json.RawMessage(`{"incumbent":{"user_id":"bad"}}`),nil);err==nil{t.Fatal("malformed reference accepted")}
}
