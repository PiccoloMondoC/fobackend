package main

import (
	"encoding/json"
	"testing"

	"github.com/PiccoloMondoC/focodebase/fobackend/internal/data"

	"github.com/google/uuid"
)

// The merchant_contexts collection must always serialize as a JSON array.
// Angular's MerchantContextService rejects null as an unusable collection,
// which would prevent routing a context-less merchant to onboarding.
func TestBuildOwnMerchantContextsResponse_EmptyIsJSONArray(t *testing.T) {
	inputs := map[string][]*data.MerchantAccount{
		"nil slice":   nil,
		"empty slice": {},
		"nil entry":   {nil},
	}

	for name, in := range inputs {
		t.Run(name, func(t *testing.T) {
			b, err := json.Marshal(buildOwnMerchantContextsResponse(in))
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			if got, want := string(b), `{"merchant_contexts":[]}`; got != want {
				t.Fatalf("got %s, want %s", got, want)
			}
		})
	}
}

func TestBuildOwnMerchantContextsResponse_MapsPrincipalContext(t *testing.T) {
	account := &data.MerchantAccount{
		ID:            uuid.New(),
		MerchantID:    uuid.New(),
		AccountStatus: data.MerchantAccountStatusActive,
	}

	got := buildOwnMerchantContextsResponse([]*data.MerchantAccount{account})

	if len(got.MerchantContexts) != 1 {
		t.Fatalf("expected 1 context, got %d", len(got.MerchantContexts))
	}

	c := got.MerchantContexts[0]
	if c.MerchantAccountID != account.ID ||
		c.MerchantID != account.MerchantID ||
		c.AccountStatus != "active" {
		t.Fatalf("unexpected context mapping: %+v", c)
	}
}
