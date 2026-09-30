// focodebase/fobackend/internal/services/merchant_future_offering_readiness_test.go
//
// Pure unit tests (no database) for the readiness rule sets. The
// transaction-bound contributors delegate to these functions.
package services

import (
	"strings"
	"testing"
	"time"

	"github.com/PiccoloMondoC/focodebase/fobackend/internal/data"

	"github.com/google/uuid"
)

var readinessNow = time.Date(2026, 9, 27, 15, 0, 0, 0, time.UTC)

func completeOffering() *data.MerchantFutureOffering {
	title := "Veltrix"
	category := uuid.New()
	offeringType := data.MerchantFutureOfferingType("product")
	release := data.MerchantFutureOfferingReleaseStrategy("scheduled")
	access := data.MerchantFutureOfferingAccessPolicy("public")
	launch := readinessNow.Add(90 * 24 * time.Hour)
	return &data.MerchantFutureOffering{
		ID: uuid.New(), Title: &title, Summary: "The first device that thinks at the speed you move.",
		CategoryID: &category, OfferingType: &offeringType, ReleaseStrategy: &release,
		AccessPolicy: &access, LaunchAt: &launch, Status: "draft",
	}
}

func codes(issues []MerchantFutureOfferingReadinessIssue) []string {
	out := make([]string, 0, len(issues))
	for _, i := range issues {
		out = append(out, i.Code)
	}
	return out
}

func TestCoreFactsCompleteHasNoIssues(t *testing.T) {
	if issues := coreFactsIssues(completeOffering(), readinessNow); len(issues) != 0 {
		t.Fatalf("want none, got %v", codes(issues))
	}
}

func TestCoreFactsReportsEveryMissingFact(t *testing.T) {
	fo := &data.MerchantFutureOffering{ID: uuid.New(), Summary: "  ", Status: "draft"}
	got := strings.Join(codes(coreFactsIssues(fo, readinessNow)), ",")
	want := strings.Join([]string{
		ReadinessCodeTitleRequired, ReadinessCodeSummaryRequired, ReadinessCodeCategoryRequired,
		ReadinessCodeOfferingTypeRequired, ReadinessCodeReleaseStrategyRequired,
		ReadinessCodeAccessPolicyRequired, ReadinessCodeLaunchAtRequired,
	}, ",")
	if got != want {
		t.Fatalf("got %s\nwant %s", got, want)
	}
}

func TestLaunchTimingReadiness(t *testing.T) {
	past := readinessNow.Add(-time.Minute)
	window := func(p string, y int, m time.Month, d int) *data.MerchantFutureOffering {
		fo := completeOffering()
		fo.LaunchAt = nil
		precision := data.MerchantFutureOfferingLaunchPrecision(p)
		start := time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
		fo.LaunchWindowPrecision, fo.LaunchWindowStart = &precision, &start
		return fo
	}
	pastExact := completeOffering()
	pastExact.LaunchAt = &past
	none := completeOffering()
	none.LaunchAt = nil

	cases := map[string]struct {
		fo   *data.MerchantFutureOffering
		want string
	}{
		"exact in future":            {completeOffering(), ""},
		"exact passed":               {pastExact, ReadinessCodeLaunchAtNotFuture},
		"no timing":                  {none, ReadinessCodeLaunchAtRequired},
		"early 2027 (first half)":    {window("half", 2027, 1, 1), ""},
		"this year is still future":  {window("year", 2026, 1, 1), ""},
		"this quarter still running": {window("quarter", 2026, 7, 1), ""},
		"today as a date":            {window("day", 2026, 9, 27), ""},
		"yesterday as a date":        {window("day", 2026, 9, 26), ReadinessCodeLaunchWindowNotFuture},
		"last quarter":               {window("quarter", 2026, 4, 1), ReadinessCodeLaunchWindowNotFuture},
		"last year":                  {window("year", 2025, 1, 1), ReadinessCodeLaunchWindowNotFuture},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got := strings.Join(codes(launchTimingIssues(tc.fo, readinessNow)), ",")
			if got != tc.want {
				t.Fatalf("got %q want %q", got, tc.want)
			}
		})
	}
}

func TestReadinessMessagesAvoidInternalVocabulary(t *testing.T) {
	fo := &data.MerchantFutureOffering{ID: uuid.New(), Status: "draft"}
	for _, issue := range coreFactsIssues(fo, readinessNow) {
		if strings.Contains(issue.Message, "Future Offering") {
			t.Fatalf("%s message uses internal vocabulary: %q", issue.Code, issue.Message)
		}
		if issue.Section != MerchantFutureOfferingReadinessSectionDetails || issue.Field == "" {
			t.Fatalf("%s must be a details issue with a field", issue.Code)
		}
	}
}

func TestEngagementConfigurationIssues(t *testing.T) {
	empty := &data.MerchantFutureOfferingEngagementActionGroup{ID: uuid.New()}
	two := 2
	ceiling := &data.MerchantFutureOfferingEngagementActionGroup{ID: uuid.New(), MaxSelections: &two}
	activeAction, retiredAction := uuid.New(), uuid.New()
	options := []*data.MerchantFutureOfferingEngagementOption{
		{ID: uuid.New(), EngagementActionGroupID: ceiling.ID, EngagementActionID: activeAction},
		{ID: uuid.New(), EngagementActionGroupID: uuid.New(), EngagementActionID: retiredAction},
	}
	active := map[uuid.UUID]*data.EngagementAction{activeAction: {ID: activeAction}}

	got := strings.Join(codes(engagementConfigurationIssues(
		[]*data.MerchantFutureOfferingEngagementActionGroup{empty, ceiling}, options, active)), ",")
	want := strings.Join([]string{
		ReadinessCodeEngagementGroupEmpty, ReadinessCodeEngagementCeilingTooHigh,
		ReadinessCodeEngagementOptionOrphaned, ReadinessCodeEngagementActionInactive,
	}, ",")
	if got != want {
		t.Fatalf("got %s\nwant %s", got, want)
	}
}

// Zero merchant actions is valid: Watch is Platform-owned and always
// available.
func TestNoEngagementActionsIsReady(t *testing.T) {
	if issues := engagementConfigurationIssues(nil, nil, nil); len(issues) != 0 {
		t.Fatalf("want none, got %v", codes(issues))
	}
}
