// Package data provides CE regression protection for registration security, lifecycle, and compatibility.
// focodebase/fobackend/internal/data/merchant_registration_ce_test.go
//
// GTM:
//     Layer: 2.1 Persistence / Merchant Registration
//     Release Class: SPINE
//     Reason: Supports the complete Foundation Level 4 merchant-registration and account-navigation vertical.
//
// SPINE Rule:
//     Keep compiling and preserve established security, lifecycle, and privacy contracts.
//     Preserve canonical principal ownership and backend authorization.
//     Keep private registration values out of public projections, logs, and errors.
//
package data

import (
 "context"
 "errors"
 "strings"
 "testing"
 "github.com/jackc/pgx/v5/pgconn"
)
func TestMerchantRegistration_CEPrivateErrorsPreserveOnlySafeClassification(t *testing.T){
 cases:=[]error{
  &pgconn.PgError{Code:"23514",Message:"SECRET",Detail:"tax_id=SECRET"},
  &pgconn.PgError{Code:"40P01",Message:"SECRET",Detail:"SECRET"},
  errors.New("SECRET"),context.Canceled,context.DeadlineExceeded,
 }
 for _,input:=range cases{
  err:=SafeMerchantRegistrationError("write",input)
  if err==nil||strings.Contains(err.Error(),"SECRET"){t.Fatal("unsafe or missing error")}
  for _,sentinel:=range []error{context.Canceled,context.DeadlineExceeded}{
   if errors.Is(input,sentinel)&&!errors.Is(err,sentinel){t.Fatal("lost cancellation classification")}
  }
  var driver *pgconn.PgError;if errors.As(err,&driver){t.Fatal("driver details remain reachable")}
 }
}
func TestMerchantRegistration_CEBusinessAddressAndMailbox(t *testing.T){
 for _,address:=range []string{"P.O. Box 12","PO Box 999","Post Office Box 15"}{
  if _,err:=NormalizeMerchantRegistrationFields(MerchantRegistrationFields{AddressLine1:&address});err==nil{t.Fatal("postal box accepted")}
 }
 for _,email:=range []string{"owner@gmail.com","owner@Outlook.com"}{
  if _,err:=NormalizeMerchantRegistrationFields(MerchantRegistrationFields{CorporateEmail:&email});err==nil{t.Fatal("personal mailbox accepted")}
 }
}
