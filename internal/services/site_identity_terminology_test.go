// Package services verifies additive terminology without invalidating history.
// focodebase/fobackend/internal/services/site_identity_terminology_test.go
package services
import "testing"
func TestIntelligenceTerminologyPreservesHistoricalPresentations(t *testing.T){
 old:=DefaultSitePresentation()
 delete(old.Terms,"anticipation_intelligence.label")
 delete(old.Terms,"anticipation_intelligence.early_label")
 validated,err:=ValidateSitePresentation(old);if err!=nil{t.Fatal(err)}
 if _,exists:=validated.Terms["anticipation_intelligence.label"];exists{t.Fatal("validation silently rewrote history")}
 delete(old.Terms,"merchant.project")
 if _,err=ValidateSitePresentation(old);err==nil{t.Fatal("required existing terminology accepted when missing")}
}
func TestIntelligenceTerminologyAllowsIndependentWording(t *testing.T){
 presentation:=DefaultSitePresentation()
 presentation.Terms["anticipation_intelligence.label"]="Consumer insight"
 presentation.Terms["anticipation_intelligence.early_label"]="Initial insight"
 validated,err:=ValidateSitePresentation(presentation);if err!=nil{t.Fatal(err)}
 if validated.Terms["anticipation_intelligence.label"]!="Consumer insight"{t.Fatal("configured wording was replaced")}
}
