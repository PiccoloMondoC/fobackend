// Package main provides CE regression protection for registration security, lifecycle, and compatibility.
// focodebase/fobackend/internal/server/cmd/api/merchant_registration_ce_test.go
//
// GTM:
//     Layer: 3.2 Merchant Registration / HTTP and Authorization
//     Release Class: SPINE
//     Reason: Supports the complete Foundation Level 4 merchant-registration and account-navigation vertical.
//
// SPINE Rule:
//     Keep compiling and preserve established security, lifecycle, and privacy contracts.
//     Preserve canonical principal ownership and backend authorization.
//     Keep private registration values out of public projections, logs, and errors.
//
package main

import (
 "context"
 "net/http"
 "net/http/httptest"
 "strings"
 "testing"
)
func TestMerchantRegistration_CEDecoderRejectsAmbiguousPayloads(t *testing.T){
 app:=&Application{}
 for _,body:=range []string{`null`,`[]`,`{}`,`{"expected_revision":-1}`,`{"expected_revision":0,"dba_name":"First","dba_name":"Second"}`,`{"expected_revision":0} {}`}{
  w:=httptest.NewRecorder();r:=httptest.NewRequest(http.MethodPut,"/",strings.NewReader(body))
  if _,ok:=app.decodeMerchantRegistrationRequest(w,r);ok||w.Code!=400{t.Fatalf("ambiguous body accepted (%d)",w.Code)}
 }
}
func TestMerchantRegistration_CELegacyEndpointCannotCreateMerchant(t *testing.T){
 app:=&Application{};w:=httptest.NewRecorder()
 app.OnboardMerchantHandler(w,httptest.NewRequest(http.MethodPost,"/merchant-onboarding/",nil))
 if w.Code!=410||w.Header().Get("Cache-Control")!="no-store"{t.Fatal("legacy bypass or cacheable response")}
}
func TestMerchantRegistration_CEPrincipalCapabilityIsBounded(t *testing.T){
 for _,name:=range []string{"expel_user","read_administrators","create_merchant_invoice","update_merchant","read_user_profiles",""}{
  if isMerchantPrincipalCapability(name){t.Fatalf("unrelated capability %q entered bridge",name)}
 }
 app:=&Application{}
 if app.hasMerchantPrincipalCapability(context.Background(),"read_merchant_future_offering"){t.Fatal("missing trusted identity gained authority")}
}
