// Package data tests profile validation, partial updates, and database error classification.
// focodebase/fobackend/internal/data/profile_contract_test.go

package data
import (
 "errors"
 "strings"
 "testing"
 "github.com/jackc/pgx/v5/pgconn"
)
func ceProfileString(s string)*string{return &s}
func TestCEProfilePatchPreservesAndClears(t *testing.T){
 p:=UserProfile{FullLegalName:ceProfileString("Ada Lovelace"),UserHandle:ceProfileString("ada"),Location:ceProfileString("London"),PreferredContact:"email",IsFlagged:true,ModerationNotes:ceProfileString("retain")}
 next,fields,err:=ApplyProfileChanges(p,ProfileChanges{"bio":ceProfileString("Biography")},false)
 if err!=nil||next.Location==nil||*next.Location!="London"||!next.IsFlagged||*next.ModerationNotes!="retain"||len(fields)!=1{t.Fatalf("partial update changed unrelated data: %v",err)}
 next,fields,err=ApplyProfileChanges(p,ProfileChanges{"location":nil},false)
 if err!=nil||next.Location!=nil||p.Location==nil||len(fields)!=1{t.Fatal("clear or input immutability failed")}
}
func TestCEProfileInvalidPatchIsAtomic(t *testing.T){
 p:=UserProfile{FullLegalName:ceProfileString("Ada Lovelace"),UserHandle:ceProfileString("ada"),PreferredContact:"email"}
 _,_,err:=ApplyProfileChanges(p,ProfileChanges{"full_legal_name":nil,"location":ceProfileString("Paris")},false)
 if !errors.Is(err,ErrUserProfileInvalidInput)||*p.FullLegalName!="Ada Lovelace"||p.Location!=nil{t.Fatal("validation was not atomic")}
 if strings.Contains(err.Error(),"Ada"){t.Fatal("error contains personal data")}
}
func TestCEProfileFieldRules(t *testing.T){
 cases:=[]struct{field,value,problem string}{
  {"phone","1234","invalid_phone"},{"phone","+12025550123",""},
  {"website","https://example.test/path",""},{"website","javascript:alert(1)","invalid_url"},
  {"avatar_url","https://user:pass@example.test/p","invalid_url"},
  {"full_legal_name","A\u202eB","invalid_characters"},{"bio",strings.Repeat("é",1001),"too_long"},
  {"bio","A\nB",""},{"user_handle","admin","invalid_handle"},{"user_handle","alpha_2",""},
 }
 for _,c:=range cases{t.Run(c.field+"/"+c.problem,func(t *testing.T){if got:=ProfileFieldProblem(c.field,&c.value);got!=c.problem{t.Fatalf("got %q, want %q",got,c.problem)}})}
}
func TestCEProfileCreationAndNormalization(t *testing.T){
 seed:=UserProfile{PreferredContact:"email"}
 _,_,err:=ApplyProfileChanges(seed,ProfileChanges{},true);if !errors.Is(err,ErrUserProfileInvalidInput){t.Fatal("missing names accepted")}
 p,_,err:=ApplyProfileChanges(seed,ProfileChanges{"full_legal_name":ceProfileString(" Ada Lovelace "),"user_handle":ceProfileString(" ADA_1 ")},true)
 if err!=nil||*p.FullLegalName!="Ada Lovelace"||*p.UserHandle!="ada_1"{t.Fatal("normalization failed")}
 _,_,err=ApplyProfileChanges(p,ProfileChanges{"is_flagged":ceProfileString("false")},false);if !errors.Is(err,ErrUserProfileInvalidInput){t.Fatal("moderation accepted as ordinary field")}
 _,_,err=ApplyProfileChanges(p,ProfileChanges{"preferred_contact":ceProfileString("phone")},false);if !errors.Is(err,ErrUserProfileInvalidInput){t.Fatal("phone preference accepted without phone")}
}
func TestCEProfileDatabaseErrorClassification(t *testing.T){
 for _,name:=range []string{"user_profiles_user_handle_key","ux_global_handles_namespace","pk_global_handles"}{
  if !errors.Is(ProfileDatabaseError(&pgconn.PgError{Code:"23505",ConstraintName:name}),ErrUserHandleTaken){t.Fatalf("unclassified handle constraint %s",name)}
 }
 if !errors.Is(ProfileDatabaseError(&pgconn.PgError{Code:"40P01"}),ErrProfileConflict){t.Fatal("deadlock not classified")}
 if errors.Is(ProfileDatabaseError(&pgconn.PgError{Code:"23505",ConstraintName:"unrelated"}),ErrUserHandleTaken){t.Fatal("unrelated unique failure misclassified")}
}

func TestCEInitialProfileRequiresLegalNameAndHandle(t *testing.T){
 for _,changes:=range []ProfileChanges{{"full_legal_name":ceProfileString("Sukarno")},{"user_handle":ceProfileString("sunrise")}}{
  if _,_,err:=ApplyProfileChanges(UserProfile{PreferredContact:"email"},changes,true);!errors.Is(err,ErrUserProfileInvalidInput){t.Fatal("incomplete initial identity accepted")}
 }
 for _,name:=range []string{"Sukarno","王小明","Dr. María del Carmen García III"}{
  p,_,err:=ApplyProfileChanges(UserProfile{PreferredContact:"email"},ProfileChanges{"full_legal_name":&name,"user_handle":ceProfileString("sunrise")},true)
  if err!=nil||p.FullLegalName==nil||*p.FullLegalName!=name{t.Fatal("legitimate full legal name was split or rejected")}
 }
}
