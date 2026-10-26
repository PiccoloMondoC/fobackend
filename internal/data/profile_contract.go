// Package data provides account identity completion and administrative identity safeguards.
// focodebase/fobackend/internal/data/profile_contract.go

// CE Foundation 3. Profile field rules are data validity, never activity eligibility.
package data

import (
 "errors"
 "net/url"
 "regexp"
 "sort"
 "strings"
 "unicode"
 "unicode/utf8"
)

var (
 ErrProfileExists = errors.New("profile already exists")
 ErrProfileUnavailable = errors.New("profile unavailable")
 ErrProfileConflict = errors.New("profile version conflict")
 ErrProfileAccount = errors.New("account unavailable")
 ErrProfileVersionRequired = errors.New("profile version required")
)

// A missing map key preserves the stored value. A nil value clears it.
// Identity, timestamps, moderation and normalized relations are not writable here.
type ProfileChanges map[string]*string

type ProfileFieldErrors map[string]string
func (e ProfileFieldErrors) Error() string { return "invalid profile fields" }
func (e ProfileFieldErrors) Is(target error) bool { return target == ErrUserProfileInvalidInput }

func ProfileFieldNames() []string { return []string{"full_legal_name","user_handle","phone","preferred_contact","avatar_url","bio","location","company","website"} }
func ProfileFieldValue(p *UserProfile, field string) *string {
 if p == nil {return nil}
 switch field {
 case "full_legal_name":return p.FullLegalName
 case "user_handle":return p.UserHandle
 case "phone":return p.Phone
 case "preferred_contact":return &p.PreferredContact
 case "avatar_url":return p.AvatarURL
 case "bio":return p.Bio
 case "location":return p.Location
 case "company":return p.Company
 case "website":return p.Website
 }
 return nil
}
var profilePhone = regexp.MustCompile(`^\+[1-9][0-9]{9,14}$`)
func profileClean(value string, multiline bool) bool {
 if !utf8.ValidString(value) { return false }
 for _,c:= range value {
  if unicode.IsControl(c) && !(multiline && (c=='\n'||c=='\t')) {return false}
  if c==0x061c || c==0x200e || c==0x200f || (c>=0x202a && c<=0x202e) || (c>=0x2066 && c<=0x2069) {return false}
 }
 return true
}
// ProfileFieldProblem assesses persisted values without normalizing them away.
// Empty values are missing, not invalid. Database-backed handle availability
// is separate: measurement must not mistake the owner's claim for a conflict.
func ProfileFieldProblem(field string, value *string) string {
 if value==nil || strings.TrimSpace(*value)=="" {return ""}
 v:=*value
 if !profileClean(v, field=="bio") {return "invalid_characters"}
 limit:=200
 switch field {
 case "full_legal_name":limit=300
 case "bio":limit=1000
 case "phone":if !profilePhone.MatchString(v){return "invalid_phone"}
 case "user_handle":if validateUserHandle(v)!=nil{return "invalid_handle"}
 case "preferred_contact":if v!="email" && v!="phone" {return "invalid_choice"}
 case "avatar_url","website":
  limit=2048
  u,err:=url.Parse(v)
  if err!=nil || u.Hostname()=="" || u.User!=nil || (u.Scheme!="https" && u.Scheme!="http") {return "invalid_url"}
 }
 if utf8.RuneCountInString(v)>limit {return "too_long"}
 return ""
}
func setProfileField(p *UserProfile, field string,v *string) {
 switch field {
 case "full_legal_name":p.FullLegalName=v
 case "user_handle":p.UserHandle=v
 case "phone":p.Phone=v
 case "preferred_contact":p.PreferredContact="";if v!=nil{p.PreferredContact=*v}
 case "avatar_url":p.AvatarURL=v
 case "bio":p.Bio=v
 case "location":p.Location=v
 case "company":p.Company=v
 case "website":p.Website=v
 }
}
func profileEqual(a,b *string)bool {return a==nil && b==nil || a!=nil && b!=nil && *a==*b}
// ApplyProfileChanges copies the record. No caller-visible mutation occurs on error.
// Existing invalid unrelated fields remain visible in the assessment and do not
// prevent an account holder repairing the profile one field at a time.
func ApplyProfileChanges(p UserProfile, changes ProfileChanges, creating bool)(UserProfile,[]string,error){
 known:=map[string]bool{};for _,f:=range ProfileFieldNames(){known[f]=true}
 problems:=ProfileFieldErrors{}; changed:=[]string{}
 for field,value:=range changes {
  if !known[field] {problems[field]="unknown_field";continue}
  var v *string
  if value!=nil {s:=strings.TrimSpace(strings.ReplaceAll(*value,"\r\n","\n"));if field=="user_handle" || field=="preferred_contact" {s=strings.ToLower(s)};if s!=""{v=&s}}
  if (field=="full_legal_name"||field=="user_handle"||field=="preferred_contact") && v==nil {problems[field]="required";continue}
  if code:=ProfileFieldProblem(field,v);code!="" {problems[field]=code;continue}
  if !profileEqual(ProfileFieldValue(&p,field),v){changed=append(changed,field);setProfileField(&p,field,v)}
 }
 if creating {for _,f:=range []string{"full_legal_name","user_handle"}{v:=ProfileFieldValue(&p,f);if v==nil || strings.TrimSpace(*v)=="" {problems[f]="required"}}}
 _, changingPhone := changes["phone"]; _, changingContact := changes["preferred_contact"]
 if (creating || changingPhone || changingContact) && p.PreferredContact=="phone" && (p.Phone==nil || !profilePhone.MatchString(*p.Phone)) {problems["phone"]="phone_required"}
 if len(problems)>0 {return UserProfile{},nil,problems}
 sort.Strings(changed);return p,changed,nil
}
func validateUserHandle(handle string) error {
 if len(handle)<3||len(handle)>30{return ErrUserProfileInvalidInput}
 for _,c:=range handle {if !(c=='_'||c=='-'||c>='a'&&c<='z'||c>='0'&&c<='9'){return ErrUserProfileInvalidInput}}
 if reservedHandles[handle]{return ErrUserProfileInvalidInput};return nil
}

func normalizeRequiredProfileString(value *string,field string)(string,error){
 if value==nil||strings.TrimSpace(*value)==""{return "",ErrUserProfileInvalidInput}
 v:=strings.TrimSpace(*value);if ProfileFieldProblem(field,&v)!=""{return "",ErrUserProfileInvalidInput};return v,nil
}
func normalizeOptionalUserHandle(value *string)(*string,error){
 if value==nil{return nil,nil};v:=strings.ToLower(strings.TrimSpace(*value));if v==""{return nil,nil};if err:=validateUserHandle(v);err!=nil{return nil,err};return &v,nil
}
func validatePreferredContact(value string)error{
 value=strings.ToLower(strings.TrimSpace(value));if value!="email"&&value!="phone"{return ErrUserProfileInvalidInput};return nil
}
