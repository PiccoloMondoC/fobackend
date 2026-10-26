// Package data provides account identity completion and administrative identity safeguards.
// focodebase/fobackend/internal/data/admin_identity_test.go

package data

import (
 "encoding/json"
 "strings"
 "testing"
 "time"
 "github.com/google/uuid"
)

func TestAdministrativeIdentityRequiredProfileAndOwnership(t *testing.T) {
 id:=uuid.MustParse("11111111-1111-4111-8111-111111111111")
 handle:="ada";phone:="+12025550123"
 p:=&UserProfile{UserID:id,UserHandle:&handle,PreferredContact:"email"}
 cases:=[]struct{name string;p *UserProfile;private,claimed,complete bool}{
  {"no profile",nil,false,false,false},
  {"required identity valid",p,true,true,true},
  {"private required name absent",p,false,true,false},
  {"handle not claimed",p,true,false,false},
  {"optional details absent",p,true,true,true},
  {"missing optional contact",&UserProfile{UserHandle:&handle},true,true,true},
  {"optional phone contact missing phone",&UserProfile{UserHandle:&handle,PreferredContact:"phone"},true,true,true},
  {"valid phone contact",&UserProfile{UserHandle:&handle,PreferredContact:"phone",Phone:&phone},true,true,true},
 }
 for _,c:=range cases{t.Run(c.name,func(t *testing.T){
  got,err:=administrativeIdentity(id,"ada@private.example",c.p,c.private,c.claimed)
  if err!=nil{t.Fatal(err)}
  if got.UserID!=id || got.EmailPrefix!="ada" || got.ProfileComplete!=c.complete{t.Fatalf("unexpected identity: %+v",got)}
  if c.complete!=(got.UserHandle!=nil){t.Fatal("handle must appear only with completed required identity")}
  raw,err:=json.Marshal(got);if err!=nil{t.Fatal(err)}
  if strings.Contains(string(raw),"private.example") || strings.Contains(string(raw),"full_legal_name") {t.Fatal("private identity leaked")}
 })}
}
func TestAdministrativeIdentityTransitionsAndCollisions(t *testing.T){
 a:=uuid.MustParse("11111111-1111-4111-8111-111111111111");b:=uuid.MustParse("22222222-2222-4222-8222-222222222222")
 first,_:=administrativeIdentity(a,"same@one.example",nil,false,false)
 second,_:=administrativeIdentity(b,"same@two.example",nil,false,false)
 if first.EmailPrefix!=second.EmailPrefix || first.UserID==second.UserID{t.Fatal("collision lost immutable association")}
 for _,handle:=range []string{"first","second"}{
  p:=&UserProfile{UserHandle:&handle,PreferredContact:"email"}
  got,err:=administrativeIdentity(a,"same@one.example",p,true,true)
  if err!=nil || got.UserID!=a || got.UserHandle==nil || *got.UserHandle!=handle {t.Fatal("handle transition failed")}
  now:=time.Now();p.DeletedAt=&now
  got,err=administrativeIdentity(a,"same@one.example",p,true,true)
  if err!=nil || got.ProfileComplete || got.UserHandle!=nil {t.Fatal("deleted profile retained verified display")}
 }
}
func TestAdministrativeIdentityRejectsCorruptAccountEmail(t *testing.T){
 id:=uuid.MustParse("11111111-1111-4111-8111-111111111111")
 for _,email:=range []string{"","no-at","@example.test","prefix@","a@b@example.test","a\n@example.test","a\u202e@example.test"}{
  if _,err:=administrativeIdentity(id,email,nil,false,false);err==nil{t.Fatal("invalid account accepted")}
 }
}
