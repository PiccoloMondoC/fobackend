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
