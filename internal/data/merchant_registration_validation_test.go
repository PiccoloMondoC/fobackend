// Package data provides regression specifications for private merchant-registration validation, normalization, and progress assessment.
// focodebase/fobackend/internal/data/merchant_registration_validation_test.go
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
	"errors"
	"strings"
	"testing"
)

func regPtr(s string) *string { return &s }

func completeRegistrationInput() MerchantRegistrationFields {
	return MerchantRegistrationFields{
		LegalName:         regPtr("  Acme   Holdings  Ltd "),
		DBAName:           regPtr("Acme"),
		TaxID:             regPtr(" 12-3456789 "),
		AddressLine1:      regPtr("1 Market Street"),
		AddressLine2:      regPtr(""),
		AddressCity:       regPtr("San Francisco"),
		AddressRegion:     regPtr("CA"),
		AddressPostalCode: regPtr("94105"),
		AddressCountry:    regPtr("us"),
		CorporateEmail:    regPtr(" Ops@Acme.Example "),
		ContactName:       regPtr("Jordan Lee"),
		Phone:             regPtr("+1 (415) 555-0100"),
		Website:           regPtr(" https://acme.example/about "),
	}
}

func validationFields(t *testing.T, err error) MerchantRegistrationFieldErrors {
	t.Helper()
	var validation *MerchantRegistrationValidationError
	if !errors.As(err, &validation) {
		t.Fatalf("expected *MerchantRegistrationValidationError, got %v", err)
	}
	if !errors.Is(err, ErrMerchantRegistrationInvalid) {
		t.Fatal("validation error must wrap ErrMerchantRegistrationInvalid")
	}
	return validation.Fields
}

func TestNormalizeMerchantRegistrationFields_CanonicalizesEveryFact(t *testing.T) {
	out, err := NormalizeMerchantRegistrationFields(completeRegistrationInput())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	checks := map[string]struct {
		got  *string
		want string
	}{
		"legal_name":      {out.LegalName, "Acme Holdings Ltd"},
		"dba_name":        {out.DBAName, "Acme"},
		"tax_id":          {out.TaxID, "12-3456789"},
		"address_country": {out.AddressCountry, "US"},
		"corporate_email": {out.CorporateEmail, "ops@acme.example"},
		"phone":           {out.Phone, "+14155550100"},
		"website":         {out.Website, "https://acme.example/about"},
		"address_region":  {out.AddressRegion, "CA"},
	}
	for name, c := range checks {
		if c.got == nil || *c.got != c.want {
			t.Errorf("%s: got %v, want %q", name, c.got, c.want)
		}
	}
	if out.AddressLine2 != nil {
		t.Errorf("blank optional address line 2 must normalize to nil")
	}
}

func TestNormalizeMerchantRegistrationFields_DraftRequiresNothing(t *testing.T) {
	out, err := NormalizeMerchantRegistrationFields(MerchantRegistrationFields{})
	if err != nil {
		t.Fatalf("an empty draft must be valid: %v", err)
	}
	if !out.Equal(MerchantRegistrationFields{}) {
		t.Fatal("empty draft must stay empty")
	}

	partial, err := NormalizeMerchantRegistrationFields(MerchantRegistrationFields{
		DBAName: regPtr("Acme"),
		TaxID:   regPtr("   "),
	})
	if err != nil {
		t.Fatalf("a partial draft must be valid: %v", err)
	}
	if partial.DBAName == nil || partial.TaxID != nil {
		t.Fatal("partial draft must keep supplied facts and drop blank ones")
	}
}

func TestNormalizeMerchantRegistrationFields_RejectsInvalidFormats(t *testing.T) {
	cases := []struct {
		name  string
		in    MerchantRegistrationFields
		field string
		code  string
	}{
		{"legal name too short", MerchantRegistrationFields{LegalName: regPtr("A")}, "legal_name", "too_short"},
		{"legal name too long", MerchantRegistrationFields{LegalName: regPtr(strings.Repeat("a", 201))}, "legal_name", "too_long"},
		{"dba too long", MerchantRegistrationFields{DBAName: regPtr(strings.Repeat("b", 121))}, "dba_name", "too_long"},
		{"dba punctuation only", MerchantRegistrationFields{DBAName: regPtr("--")}, "dba_name", "invalid"},
		{"dba bidi override", MerchantRegistrationFields{DBAName: regPtr("Ac\u202eme")}, "dba_name", "invalid"},
		{"dba control char", MerchantRegistrationFields{DBAName: regPtr("Ac\u0007me")}, "dba_name", "invalid"},
		{"tax id symbols", MerchantRegistrationFields{TaxID: regPtr("12#4567")}, "tax_id", "invalid"},
		{"tax id too few alphanumerics", MerchantRegistrationFields{TaxID: regPtr("1-2-3")}, "tax_id", "invalid"},
		{"tax id too short", MerchantRegistrationFields{TaxID: regPtr("123")}, "tax_id", "too_short"},
		{"tax id too long", MerchantRegistrationFields{TaxID: regPtr(strings.Repeat("9", 33))}, "tax_id", "too_long"},
		{"country unknown", MerchantRegistrationFields{AddressCountry: regPtr("XX")}, "address_country", "invalid"},
		{"country name", MerchantRegistrationFields{AddressCountry: regPtr("USA")}, "address_country", "invalid"},
		{"postal symbols", MerchantRegistrationFields{AddressPostalCode: regPtr("94_105")}, "address_postal_code", "invalid"},
		{"email missing domain dot", MerchantRegistrationFields{CorporateEmail: regPtr("ops@acme")}, "corporate_email", "invalid"},
		{"email display name", MerchantRegistrationFields{CorporateEmail: regPtr("Ops <ops@acme.example>")}, "corporate_email", "invalid"},
		{"email long local part", MerchantRegistrationFields{CorporateEmail: regPtr(strings.Repeat("a", 65) + "@acme.example")}, "corporate_email", "invalid"},
		{"phone without country code", MerchantRegistrationFields{Phone: regPtr("415 555 0100")}, "phone", "invalid"},
		{"phone letters", MerchantRegistrationFields{Phone: regPtr("+1 415 CALL ACME")}, "phone", "invalid"},
		{"website no scheme", MerchantRegistrationFields{Website: regPtr("acme.example")}, "website", "invalid"},
		{"website ftp", MerchantRegistrationFields{Website: regPtr("ftp://acme.example")}, "website", "invalid"},
		{"website credentials", MerchantRegistrationFields{Website: regPtr("https://user:pw@acme.example")}, "website", "invalid"},
		{"website traversal", MerchantRegistrationFields{Website: regPtr("https://acme.example/a/../b")}, "website", "invalid"},
		{"website encoded traversal", MerchantRegistrationFields{Website: regPtr("https://acme.example/%2e%2e/b")}, "website", "invalid"},
		{"website single label host", MerchantRegistrationFields{Website: regPtr("https://localhost/")}, "website", "invalid"},
		{"website inner space", MerchantRegistrationFields{Website: regPtr("https://acme.example/a b")}, "website", "invalid"},
		{"website opaque", MerchantRegistrationFields{Website: regPtr("https:acme.example")}, "website", "invalid"},
		{"contact too short", MerchantRegistrationFields{ContactName: regPtr("J")}, "contact_name", "too_short"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NormalizeMerchantRegistrationFields(tc.in)
			fields := validationFields(t, err)
			if got := fields[tc.field]; got != tc.code {
				t.Fatalf("field %s: got code %q, want %q (all: %v)", tc.field, got, tc.code, fields)
			}
		})
	}
}

func TestMerchantRegistrationValidationError_NeverContainsValues(t *testing.T) {
	secret := "TAX-SECRET-998877"
	_, err := NormalizeMerchantRegistrationFields(MerchantRegistrationFields{
		TaxID:          regPtr(secret + "#"),
		CorporateEmail: regPtr("private-person@nowhere"),
	})
	if err == nil {
		t.Fatal("expected validation error")
	}
	if strings.Contains(err.Error(), "SECRET") || strings.Contains(err.Error(), "private-person") {
		t.Fatalf("validation error leaked submitted values: %q", err.Error())
	}
}

func TestValidateMerchantRegistrationForCompletion_RequiresAllEightFacts(t *testing.T) {
	if _, err := ValidateMerchantRegistrationForCompletion(completeRegistrationInput()); err != nil {
		t.Fatalf("complete input must pass: %v", err)
	}

	_, err := ValidateMerchantRegistrationForCompletion(MerchantRegistrationFields{})
	fields := validationFields(t, err)
	for _, key := range []string{
		"legal_name", "dba_name", "tax_id", "address_line1", "address_city",
		"address_country", "corporate_email", "contact_name", "phone", "website",
	} {
		if fields[key] != MerchantRegistrationErrRequired {
			t.Errorf("%s: expected required, got %q", key, fields[key])
		}
	}
	for _, optional := range []string{"address_line2", "address_region", "address_postal_code"} {
		if _, ok := fields[optional]; ok {
			t.Errorf("%s must be optional at completion", optional)
		}
	}

	for _, drop := range []func(*MerchantRegistrationFields){
		func(f *MerchantRegistrationFields) { f.LegalName = nil },
		func(f *MerchantRegistrationFields) { f.DBAName = regPtr(" ") },
		func(f *MerchantRegistrationFields) { f.Website = nil },
		func(f *MerchantRegistrationFields) { f.AddressCountry = nil },
	} {
		in := completeRegistrationInput()
		drop(&in)
		if _, err := ValidateMerchantRegistrationForCompletion(in); !errors.Is(err, ErrMerchantRegistrationInvalid) {
			t.Fatalf("missing fact must fail completion, got %v", err)
		}
	}
}

func TestAssessMerchantRegistrationProgress_CountsLogicalFacts(t *testing.T) {
	empty := AssessMerchantRegistrationProgress(MerchantRegistrationFields{})
	if empty.TotalFacts != 8 || empty.CompletedFacts != 0 || len(empty.MissingFacts) != 8 {
		t.Fatalf("unexpected empty progress: %+v", empty)
	}

	full, err := NormalizeMerchantRegistrationFields(completeRegistrationInput())
	if err != nil {
		t.Fatal(err)
	}
	complete := AssessMerchantRegistrationProgress(full)
	if complete.CompletedFacts != 8 || len(complete.MissingFacts) != 0 {
		t.Fatalf("unexpected complete progress: %+v", complete)
	}

	full.AddressCity = nil
	partial := AssessMerchantRegistrationProgress(full)
	if partial.CompletedFacts != 7 || len(partial.MissingFacts) != 1 ||
		partial.MissingFacts[0] != MerchantRegistrationFactRegisteredAddress {
		t.Fatalf("address parts must count as one fact: %+v", partial)
	}
}

func TestMerchantRegistrationFields_Equal(t *testing.T) {
	a, _ := NormalizeMerchantRegistrationFields(completeRegistrationInput())
	b, _ := NormalizeMerchantRegistrationFields(completeRegistrationInput())
	if !a.Equal(b) {
		t.Fatal("identical normalized inputs must be equal")
	}
	b.AddressLine2 = regPtr("Suite 5")
	if a.Equal(b) {
		t.Fatal("different optional parts must not be equal")
	}
}

func TestMerchantRegistrationCountryCodes_AreTheAssignedSet(t *testing.T) {
	if got := len(merchantRegistrationCountryCodes); got != 249 {
		t.Fatalf("expected 249 ISO 3166-1 alpha-2 codes, got %d", got)
	}
	for _, code := range []string{"US", "GB", "DE", "JP", "BR", "ZA", "SS"} {
		if !IsMerchantRegistrationCountryCode(code) {
			t.Errorf("%s must be accepted", code)
		}
	}
	for _, code := range []string{"UK", "EU", "us", ""} {
		if IsMerchantRegistrationCountryCode(code) {
			t.Errorf("%q must be rejected", code)
		}
	}
}

func TestCollapseMerchantRegistrationText_MatchesJavaScriptWhitespace(t *testing.T) {
	got := collapseMerchantRegistrationText("\u00a0 Acme\t\u2003Labs\ufeff ")
	if got != "Acme Labs" {
		t.Fatalf("got %q", got)
	}
	// U+0085 is not JavaScript whitespace; it must reach the forbidden-rune
	// check instead of being collapsed.
	if _, code := normalizeMerchantRegistrationText("Acme\u0085Labs", 2, 120); code != MerchantRegistrationErrInvalid {
		t.Fatalf("U+0085 must be rejected, got %q", code)
	}
}

func TestTranslateMerchantRegistrationWriteError_HidesDriverDetail(t *testing.T) {
	err := translateMerchantRegistrationWriteError("update", errors.New(`duplicate key value (tax_id)=(SECRET)`))
	if strings.Contains(err.Error(), "SECRET") {
		t.Fatalf("driver detail leaked: %v", err)
	}
}
