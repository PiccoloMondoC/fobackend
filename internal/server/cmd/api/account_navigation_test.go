// Package main provides regression specifications for authenticated principal-scoped account-navigation eligibility.
// focodebase/fobackend/internal/server/cmd/api/account_navigation_test.go
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
	"encoding/json"
	"strings"
	"testing"

	"github.com/PiccoloMondoC/focodebase/fobackend/internal/data"
)

func navStatus[T any](v T) *T { return &v }

func TestBuildAccountNavigationResponse_MerchantStates(t *testing.T) {
	cases := []struct {
		name         string
		state        *data.MerchantNavigationState
		eligible     bool
		wantState    string
		wantAvailble bool
		wantName     *string
	}{
		{"no relationship, eligible", &data.MerchantNavigationState{}, true, "none", true, nil},
		{"no relationship, ineligible", &data.MerchantNavigationState{}, false, "none", false, nil},
		{"nil state", nil, true, "none", true, nil},
		{
			"draft",
			&data.MerchantNavigationState{RegistrationStatus: navStatus(data.MerchantRegistrationStatusDraft)},
			true, "registration_in_progress", false, nil,
		},
		{
			"active",
			&data.MerchantNavigationState{
				RegistrationStatus: navStatus(data.MerchantRegistrationStatusCompleted),
				AccountStatus:      navStatus(data.MerchantAccountStatusActive),
				PublicName:         navStatus("Acme"),
			},
			true, "active", false, navStatus("Acme"),
		},
		{
			"legacy active without registration",
			&data.MerchantNavigationState{
				AccountStatus: navStatus(data.MerchantAccountStatusActive),
				PublicName:    navStatus("Legacy"),
			},
			true, "active", false, navStatus("Legacy"),
		},
		{
			"suspended",
			&data.MerchantNavigationState{
				AccountStatus: navStatus(data.MerchantAccountStatusSuspended),
				PublicName:    navStatus("Acme"),
			},
			true, "restricted", false, navStatus("Acme"),
		},
		{
			"completed without account",
			&data.MerchantNavigationState{RegistrationStatus: navStatus(data.MerchantRegistrationStatusCompleted)},
			true, "restricted", false, nil,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := buildAccountNavigationResponse(tc.state, tc.eligible, false)
			if got.MerchantCenter.State != tc.wantState ||
				got.MerchantCenter.RegistrationAvailable != tc.wantAvailble {
				t.Fatalf("got %+v", got.MerchantCenter)
			}
			if (tc.wantName == nil) != (got.MerchantCenter.PublicName == nil) ||
				(tc.wantName != nil && *tc.wantName != *got.MerchantCenter.PublicName) {
				t.Fatalf("public name: got %v want %v", got.MerchantCenter.PublicName, tc.wantName)
			}
		})
	}
}

func TestBuildAccountNavigationResponse_AdminAvailabilityIsPassedThrough(t *testing.T) {
	if !buildAccountNavigationResponse(nil, false, true).AdminConsole.Available {
		t.Fatal("admin availability must reflect the backend decision")
	}
	if buildAccountNavigationResponse(nil, true, false).AdminConsole.Available {
		t.Fatal("admin availability must never be inferred")
	}
}

func TestBuildAccountNavigationResponse_ContractShape(t *testing.T) {
	body, err := json.Marshal(buildAccountNavigationResponse(&data.MerchantNavigationState{}, true, false))
	if err != nil {
		t.Fatal(err)
	}
	want := `{"merchant_center":{"state":"none","registration_available":true,"public_name":null},"admin_console":{"available":false}}`
	if string(body) != want {
		t.Fatalf("got %s", body)
	}
	for _, private := range []string{"legal_name", "tax_id", "corporate_email", "phone", "website"} {
		if strings.Contains(string(body), private) {
			t.Fatalf("navigation must not carry private registration facts: %s", body)
		}
	}
}
