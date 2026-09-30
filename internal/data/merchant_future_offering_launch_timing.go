// Package data provides Future Offering launch-timing facts.
//
// focodebase/fobackend/internal/data/merchant_future_offering_launch_timing.go
//
// GTM:
//
//	Layer: 2.4 Merchant / Future Offering Domain
//	Release Class: SPINE
//	Reason:
//	  A merchant may know an exact launch moment, only a calendar date, or
//	  only a broader period ("early 2027"). All are legitimate anticipation
//	  facts. This file keeps machine-usable timing facts separate from
//	  merchant-authored presentation wording.
//
// Representation (mutually exclusive timing facts, all nullable in draft):
//
//	launch_at                 TIMESTAMPTZ  exact moment (unchanged column,
//	                                       unchanged meaning and invariants)
//	launch_window_precision   TEXT         day | month | quarter | half | year
//	launch_window_start       DATE         first day of the window, aligned
//	                                       to the precision
//	launch_display_text       TEXT         optional merchant wording shown to
//	                                       consumers, e.g. "Arriving early
//	                                       2027"; never interpreted
//
//	The window is half-open: [start, LaunchWindowEnd(precision, start)).
//	A calendar date with no time is precision "day". An instant is launch_at.
//	Display text requires one of the two timing facts; it annotates timing
//	and never replaces it.
//
// SPINE Rule:
//
//	Keep compiling.
//	Keep production-ready.
//	Mirror chk_merchant_future_offerings_launch_timing and
//	chk_merchant_future_offerings_launch_window_alignment exactly.
package data

import (
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

// MerchantFutureOfferingLaunchPrecision is the granularity of a launch window.
type MerchantFutureOfferingLaunchPrecision string

// Launch window precisions.
const (
	MerchantFutureOfferingLaunchPrecisionDay     MerchantFutureOfferingLaunchPrecision = "day"
	MerchantFutureOfferingLaunchPrecisionMonth   MerchantFutureOfferingLaunchPrecision = "month"
	MerchantFutureOfferingLaunchPrecisionQuarter MerchantFutureOfferingLaunchPrecision = "quarter"
	MerchantFutureOfferingLaunchPrecisionHalf    MerchantFutureOfferingLaunchPrecision = "half"
	MerchantFutureOfferingLaunchPrecisionYear    MerchantFutureOfferingLaunchPrecision = "year"
)

// MaxMerchantFutureOfferingLaunchDisplayText bounds the display wording.
const MaxMerchantFutureOfferingLaunchDisplayText = 80

const (
	minLaunchWindowYear = 2000
	maxLaunchWindowYear = 2200
)

// IsValidMerchantFutureOfferingLaunchPrecision reports vocabulary membership.
func IsValidMerchantFutureOfferingLaunchPrecision(p MerchantFutureOfferingLaunchPrecision) bool {
	switch p {
	case MerchantFutureOfferingLaunchPrecisionDay,
		MerchantFutureOfferingLaunchPrecisionMonth,
		MerchantFutureOfferingLaunchPrecisionQuarter,
		MerchantFutureOfferingLaunchPrecisionHalf,
		MerchantFutureOfferingLaunchPrecisionYear:
		return true
	}
	return false
}

// MerchantFutureOfferingLaunchTiming is the complete set of timing facts.
type MerchantFutureOfferingLaunchTiming struct {
	LaunchAt              *time.Time
	LaunchWindowPrecision *MerchantFutureOfferingLaunchPrecision
	LaunchWindowStart     *time.Time
	LaunchDisplayText     *string
}

func launchTimingInvalid(format string, args ...any) error {
	return fmt.Errorf("%w: %w: %s", ErrMerchantFutureOfferingInvalidInput, ErrMerchantFutureOfferingLaunchTimingInvalid, fmt.Sprintf(format, args...))
}

// IsLaunchWindowStartAligned reports whether start is the first day of a
// window of the given precision.
func IsLaunchWindowStartAligned(p MerchantFutureOfferingLaunchPrecision, start time.Time) bool {
	if p == MerchantFutureOfferingLaunchPrecisionDay {
		return true
	}
	if start.Day() != 1 {
		return false
	}
	switch p {
	case MerchantFutureOfferingLaunchPrecisionMonth:
		return true
	case MerchantFutureOfferingLaunchPrecisionQuarter:
		m := start.Month()
		return m == time.January || m == time.April || m == time.July || m == time.October
	case MerchantFutureOfferingLaunchPrecisionHalf:
		return start.Month() == time.January || start.Month() == time.July
	case MerchantFutureOfferingLaunchPrecisionYear:
		return start.Month() == time.January
	}
	return false
}

// LaunchWindowEnd returns the exclusive end date of an aligned window.
func LaunchWindowEnd(p MerchantFutureOfferingLaunchPrecision, start time.Time) time.Time {
	switch p {
	case MerchantFutureOfferingLaunchPrecisionMonth:
		return start.AddDate(0, 1, 0)
	case MerchantFutureOfferingLaunchPrecisionQuarter:
		return start.AddDate(0, 3, 0)
	case MerchantFutureOfferingLaunchPrecisionHalf:
		return start.AddDate(0, 6, 0)
	case MerchantFutureOfferingLaunchPrecisionYear:
		return start.AddDate(1, 0, 0)
	default:
		return start.AddDate(0, 0, 1)
	}
}

// NormalizeMerchantFutureOfferingLaunchTiming validates and canonicalizes a
// complete set of timing facts. It mirrors the database constraints so
// callers receive a classified input error instead of a CHECK violation.
func NormalizeMerchantFutureOfferingLaunchTiming(in MerchantFutureOfferingLaunchTiming) (MerchantFutureOfferingLaunchTiming, error) {
	out := MerchantFutureOfferingLaunchTiming{
		LaunchAt: normalizeMerchantFutureOfferingOptionalTime(in.LaunchAt),
	}

	if (in.LaunchWindowPrecision == nil) != (in.LaunchWindowStart == nil) {
		return out, launchTimingInvalid("launch window precision and start must be supplied together")
	}
	if in.LaunchWindowPrecision != nil {
		p := MerchantFutureOfferingLaunchPrecision(strings.ToLower(strings.TrimSpace(string(*in.LaunchWindowPrecision))))
		if !IsValidMerchantFutureOfferingLaunchPrecision(p) {
			return out, launchTimingInvalid("invalid launch window precision %q", *in.LaunchWindowPrecision)
		}
		s := in.LaunchWindowStart.UTC()
		start := time.Date(s.Year(), s.Month(), s.Day(), 0, 0, 0, 0, time.UTC)
		if !start.Equal(s) {
			return out, launchTimingInvalid("launch window start must be a calendar date")
		}
		if start.Year() < minLaunchWindowYear || start.Year() > maxLaunchWindowYear {
			return out, launchTimingInvalid("launch window year is out of range")
		}
		if !IsLaunchWindowStartAligned(p, start) {
			return out, launchTimingInvalid("launch window start is not aligned to its %s", p)
		}
		out.LaunchWindowPrecision = &p
		out.LaunchWindowStart = &start
	}
	if out.LaunchAt != nil && out.LaunchWindowStart != nil {
		return out, launchTimingInvalid("an exact launch time and a launch window are mutually exclusive")
	}

	if text := normalizeOptionalString(in.LaunchDisplayText); text != nil {
		if utf8.RuneCountInString(*text) > MaxMerchantFutureOfferingLaunchDisplayText {
			return out, launchTimingInvalid("launch display text exceeds %d characters", MaxMerchantFutureOfferingLaunchDisplayText)
		}
		if strings.ContainsFunc(*text, func(c rune) bool { return c < 0x20 || (c >= 0x7f && c <= 0x9f) }) {
			return out, launchTimingInvalid("launch display text must be a single line")
		}
		if out.LaunchAt == nil && out.LaunchWindowStart == nil {
			return out, launchTimingInvalid("launch display text requires a launch date or period")
		}
		out.LaunchDisplayText = text
	}
	return out, nil
}
