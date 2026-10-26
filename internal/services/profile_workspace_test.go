// Package services tests profile completeness, private-field exclusion, and service composition.
// focodebase/fobackend/internal/services/profile_workspace_test.go

package services
import (
 "encoding/json"
 "strings"
 "testing"
 "github.com/PiccoloMondoC/focodebase/fobackend/internal/data"
)
func TestCEProfileAssessmentOnlyMeasures(t *testing.T){
 phone:="invalid";bio:="Biography";first:="Ada"
 p:=&data.UserProfile{FullLegalName:&first,Phone:&phone,Bio:&bio,PreferredContact:"phone"}
 a:=AssessProfileInformation(p)
 if a.Total!=9||a.Complete!=2||a.Invalid!=2||a.Missing!=5{t.Fatalf("incorrect assessment: %+v",a)}
 a=AssessProfileInformation(nil);if a.Missing!=9||a.Complete!=0||a.Invalid!=0{t.Fatal("missing profile not measured correctly")}
}
func TestCEProfileSelfProjectionDoesNotDiscloseModeration(t *testing.T){
 secret:="moderator-only";p:=&data.UserProfile{IsFlagged:true,ModerationNotes:&secret,PreferredContact:"email"}
 payload,err:=json.Marshal(profileWorkspace(p));if err!=nil{t.Fatal(err)}
 for _,private:=range []string{"moderation_notes","is_flagged","moderator-only","deleted_at","eligibility","threshold"}{if strings.Contains(string(payload),private){t.Fatalf("self projection discloses %s",private)}}
 if !strings.Contains(string(payload),`"social_links":[]`){t.Fatal("normalized relations must use stable arrays")}
}
func TestCEProfileCompositionFailsWithoutCanonicalModel(t *testing.T){
 var s *Service;if s.ValidateProfileServicesInternal()==nil{t.Fatal("nil service accepted")}
 s=&Service{Models:&data.Models{}};if s.ValidateProfileServicesInternal()==nil{t.Fatal("missing database accepted")}
}

func TestCEAdministrativeProfileProjectionExcludesAllLegalNames(t *testing.T){
 name:="Private Full Legal Name";legacy:="Legacy Private Name";handle:="sunrise"
 p:=&data.UserProfile{FullLegalName:&name,FirstName:&legacy,LastName:&legacy,UserHandle:&handle}
 payload,err:=json.Marshal(profileAdministrativeView(p));if err!=nil{t.Fatal(err)}
 for _,forbidden:=range []string{"full_legal_name","first_name","last_name","Private","Legacy"}{if strings.Contains(string(payload),forbidden){t.Fatalf("private legal identity exposed: %s",forbidden)}}
 if !strings.Contains(string(payload),"sunrise"){t.Fatal("public handle missing")}
}
