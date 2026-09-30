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
	inputs := map[string][]*data.MerchantOperatingContext{
		"nil slice":   nil,
		"empty slice": {},
		"nil entry":   {nil},
	}

	for name, in := range inputs {
		t.Run(name, func(t *testing.T) {
			b, err := json.Marshal(
				buildOwnMerchantContextsResponse(
					in,
					map[uuid.UUID]bool{},
				),
			)
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
	merchantContext := &data.MerchantOperatingContext{
		MerchantAccountID: uuid.New(),
		MerchantID:        uuid.New(),
		MerchantName:      "Her Majesty's Super Gadget Company",
		AccountStatus:     data.MerchantAccountStatusActive,
	}

	firstVisits := map[uuid.UUID]bool{
		merchantContext.MerchantAccountID: true,
	}

	got := buildOwnMerchantContextsResponse(
		[]*data.MerchantOperatingContext{merchantContext},
		firstVisits,
	)

	if len(got.MerchantContexts) != 1 {
		t.Fatalf("expected 1 context, got %d", len(got.MerchantContexts))
	}

	c := got.MerchantContexts[0]
	if c.MerchantAccountID != merchantContext.MerchantAccountID ||
		c.MerchantID != merchantContext.MerchantID ||
		c.MerchantName != merchantContext.MerchantName ||
		c.AccountStatus != "active" ||
		!c.FirstPlatformVisit {
		t.Fatalf("unexpected context mapping: %+v", c)
	}
}
