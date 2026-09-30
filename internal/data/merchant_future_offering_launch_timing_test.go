// focodebase/fobackend/internal/data/merchant_future_offering_launch_timing_test.go
package data

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func precision(p MerchantFutureOfferingLaunchPrecision) *MerchantFutureOfferingLaunchPrecision { return &p }
func date(y int, m time.Month, d int) *time.Time {
	t := time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
	return &t
}
func text(s string) *string { return &s }

func TestLaunchTimingAcceptsExactAndApproximateForms(t *testing.T) {
	exact := time.Date(2027, 3, 15, 16, 30, 0, 123456789, time.FixedZone("x", 3600))
	cases := map[string]MerchantFutureOfferingLaunchTiming{
		"nothing yet":         {},
		"exact instant":       {LaunchAt: &exact},
		"calendar date":       {LaunchWindowPrecision: precision("day"), LaunchWindowStart: date(2027, 3, 15)},
		"month":               {LaunchWindowPrecision: precision("month"), LaunchWindowStart: date(2027, 3, 1)},
		"quarter":             {LaunchWindowPrecision: precision("quarter"), LaunchWindowStart: date(2027, 4, 1)},
		"half with wording":   {LaunchWindowPrecision: precision("half"), LaunchWindowStart: date(2027, 1, 1), LaunchDisplayText: text("  Arriving early 2027 ")},
		"year":                {LaunchWindowPrecision: precision("year"), LaunchWindowStart: date(2027, 1, 1)},
		"exact with wording":  {LaunchAt: &exact, LaunchDisplayText: text("Doors open at dawn")},
		"blank wording is no": {LaunchDisplayText: text("   ")},
	}
	for name, in := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := NormalizeMerchantFutureOfferingLaunchTiming(in); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestLaunchTimingNormalizes(t *testing.T) {
	exact := time.Date(2027, 3, 15, 16, 30, 0, 123456789, time.FixedZone("x", 3600))
	out, err := NormalizeMerchantFutureOfferingLaunchTiming(MerchantFutureOfferingLaunchTiming{LaunchAt: &exact, LaunchDisplayText: text("  Soon  ")})
	if err != nil {
		t.Fatal(err)
	}
	if out.LaunchAt.Location() != time.UTC || out.LaunchAt.Nanosecond() != 123456000 {
		t.Fatalf("launch_at must be UTC at microsecond precision, got %v", out.LaunchAt)
	}
	if *out.LaunchDisplayText != "Soon" {
		t.Fatalf("display text not trimmed: %q", *out.LaunchDisplayText)
	}
}

// The existing launch_at invariant is preserved: an exact moment and a
// window can never coexist, and display text never stands in for timing.
func TestLaunchTimingRejectsIncoherentFacts(t *testing.T) {
	exact := time.Date(2027, 3, 15, 0, 0, 0, 0, time.UTC)
	withTime := time.Date(2027, 3, 15, 9, 0, 0, 0, time.UTC)
	cases := map[string]MerchantFutureOfferingLaunchTiming{
		"exact and window":         {LaunchAt: &exact, LaunchWindowPrecision: precision("day"), LaunchWindowStart: date(2027, 3, 15)},
		"precision without start":  {LaunchWindowPrecision: precision("month")},
		"start without precision":  {LaunchWindowStart: date(2027, 3, 1)},
		"unknown precision":        {LaunchWindowPrecision: precision("season"), LaunchWindowStart: date(2027, 3, 1)},
		"month not aligned":        {LaunchWindowPrecision: precision("month"), LaunchWindowStart: date(2027, 3, 2)},
		"quarter not aligned":      {LaunchWindowPrecision: precision("quarter"), LaunchWindowStart: date(2027, 2, 1)},
		"half not aligned":         {LaunchWindowPrecision: precision("half"), LaunchWindowStart: date(2027, 4, 1)},
		"year not aligned":         {LaunchWindowPrecision: precision("year"), LaunchWindowStart: date(2027, 7, 1)},
		"start has a time":         {LaunchWindowPrecision: precision("day"), LaunchWindowStart: &withTime},
		"year out of range":        {LaunchWindowPrecision: precision("year"), LaunchWindowStart: date(1999, 1, 1)},
		"wording without timing":   {LaunchDisplayText: text("Arriving early 2027")},
		"wording too long":         {LaunchWindowPrecision: precision("year"), LaunchWindowStart: date(2027, 1, 1), LaunchDisplayText: text(strings.Repeat("a", 81))},
		"wording multi-line":       {LaunchWindowPrecision: precision("year"), LaunchWindowStart: date(2027, 1, 1), LaunchDisplayText: text("a\nb")},
	}
	for name, in := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := NormalizeMerchantFutureOfferingLaunchTiming(in)
			if !errors.Is(err, ErrMerchantFutureOfferingLaunchTimingInvalid) || !errors.Is(err, ErrMerchantFutureOfferingInvalidInput) {
				t.Fatalf("want classified launch-timing input error, got %v", err)
			}
		})
	}
}

func TestLaunchWindowEnd(t *testing.T) {
	start := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
	want := map[MerchantFutureOfferingLaunchPrecision]time.Time{
		"day":     time.Date(2027, 1, 2, 0, 0, 0, 0, time.UTC),
		"month":   time.Date(2027, 2, 1, 0, 0, 0, 0, time.UTC),
		"quarter": time.Date(2027, 4, 1, 0, 0, 0, 0, time.UTC),
		"half":    time.Date(2027, 7, 1, 0, 0, 0, 0, time.UTC),
		"year":    time.Date(2028, 1, 1, 0, 0, 0, 0, time.UTC),
	}
	for p, end := range want {
		if got := LaunchWindowEnd(p, start); !got.Equal(end) {
			t.Fatalf("%s: got %v want %v", p, got, end)
		}
	}
}
