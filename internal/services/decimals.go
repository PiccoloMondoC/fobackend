// Package services contains trusted internal service composition,
// automation support, moderation helpers, and shared internal workflow logic.
//
// sdworkspace/sdbackend/internal/services/internal-services/decimals.go
//
// GTM:
//
//	Layer: 2.6 Internal Services / Automation Foundation
//	Release Class: SPINE
//	Reason:
//	  Provides exact canonical decimal parsing for offer monetary and percentage
//	  values used by internal moderation and description workflows.
//
//	  Offer prices and discount percentages remain decimal strings and are
//	  parsed without binary floating-point conversion.
//
// SPINE Rule:
//
//	Keep compiling.
//	Keep production-ready.
//	Preserve exact rational parsing for monetary and percentage values.
//	Never convert offer prices or discount percentages to float32 or float64.
//	Reject fraction notation, exponent notation, malformed decimals, excessive
//	input length, non-positive prices, and percentages outside 0 through 100.
//	Keep absence distinct from invalid input.
//	Do not silently round or normalize stored business values.
package services

import (
	"fmt"
	"math/big"
	"regexp"
	"strings"
)

const maxOfferDecimalLength = 128

var canonicalDecimalPattern = regexp.MustCompile(
	`^[+-]?(?:[0-9]+(?:\.[0-9]*)?|\.[0-9]+)$`,
)

// parseOfferDecimal parses a canonical base-10 decimal string exactly.
//
// It intentionally rejects fraction notation and scientific notation even
// though math/big.Rat accepts them. Nil and blank values remain absent.
func parseOfferDecimal(raw *string) (*big.Rat, error) {
	if raw == nil {
		return nil, nil
	}

	value := strings.TrimSpace(*raw)
	if value == "" {
		return nil, nil
	}
	if len(value) > maxOfferDecimalLength ||
		!canonicalDecimalPattern.MatchString(value) {
		return nil, fmt.Errorf("%w: %q", ErrInvalidDecimal, value)
	}

	rat, ok := new(big.Rat).SetString(value)
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrInvalidDecimal, value)
	}

	return rat, nil
}

func parseOfferPrice(raw *string) (*big.Rat, error) {
	price, err := parseOfferDecimal(raw)
	if err != nil || price == nil {
		return price, err
	}
	if price.Sign() <= 0 {
		return nil, fmt.Errorf(
			"%w: offer price must be greater than zero",
			ErrInvalidDecimal,
		)
	}
	return price, nil
}

func parseOfferDiscountPercent(raw *string) (*big.Rat, error) {
	discount, err := parseOfferDecimal(raw)
	if err != nil || discount == nil {
		return discount, err
	}
	if discount.Sign() < 0 || discount.Cmp(big.NewRat(100, 1)) > 0 {
		return nil, fmt.Errorf(
			"%w: discount percent must be between 0 and 100",
			ErrInvalidDecimal,
		)
	}
	return discount, nil
}
