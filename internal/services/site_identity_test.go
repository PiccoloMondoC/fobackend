// internal/services/site_identity_test.go
package services
import (
 "errors"
 "strings"
 "testing"
 "github.com/PiccoloMondoC/focodebase/fobackend/internal/data"
)
func TestSitePresentationIndependentVocabulary(t *testing.T){
 in:=DefaultSitePresentation();in.Identity.SiteName="  展望 & <Works>  ";in.Terms["future_offerings.area"]="新しい世界 🌍";in.Terms["business.unanticipated_collection"]="A new kind of collection"
 out,err:=ValidateSitePresentation(in);if err!=nil{t.Fatal(err)}
 if out.Identity.SiteName!="展望 & <Works>"||out.Terms["business.unanticipated_collection"]!="A new kind of collection"{t.Fatal("business vocabulary was restricted")}
 out.Terms["merchant.project"]="Changed"
 if in.Terms["merchant.project"]=="Changed"{t.Fatal("validation aliased the caller map")}
}
func TestSitePresentationRejectsMissingContractAndControls(t *testing.T){
 cases:=[]struct{name string;change func(*data.SitePresentation)}{
  {"missing binding",func(p *data.SitePresentation){delete(p.Terms,"future_offerings.area")}},
  {"blank identity",func(p *data.SitePresentation){p.Identity.SiteName=" \t "}},
  {"control",func(p *data.SitePresentation){p.Terms["merchant.project"]="bad\x00word"}},
  {"invalid UTF8",func(p *data.SitePresentation){p.Identity.SiteName=string([]byte{0xff})}},
  {"invalid key",func(p *data.SitePresentation){p.Terms["__proto__"]="word"}},
  {"too long",func(p *data.SitePresentation){p.Identity.SiteName=strings.Repeat("界",81)}},
 }
 for _,test:=range cases{t.Run(test.name,func(t *testing.T){p:=DefaultSitePresentation();test.change(&p);_,err:=ValidateSitePresentation(p);if !errors.Is(err,data.ErrSiteIdentityInvalidInput){t.Fatalf("expected invalid presentation; got %v",err)}})}
}
func TestSitePresentationCountsUnicodeAndNormalizesDescription(t *testing.T){
 p:=DefaultSitePresentation();p.Identity.SiteName=strings.Repeat("界",80);p.Identity.Description="  first\r\nsecond\rthird  "
 out,err:=ValidateSitePresentation(p);if err!=nil{t.Fatal(err)}
 if out.Identity.Description!="first\nsecond\nthird"{t.Fatal("description normalization differs from browser")}
}
func TestSiteDefaultsAreFreshAndBrandNeutral(t *testing.T){
 a,b:=DefaultSitePresentation(),DefaultSitePresentation();a.Terms["merchant.project"]="mutated"
 if b.Terms["merchant.project"]=="mutated"{t.Fatal("shared defaults")}
 if strings.Contains(strings.ToLower(b.Identity.SiteName),"sagrenti"){t.Fatal("installation brand in engineering defaults")}
}
