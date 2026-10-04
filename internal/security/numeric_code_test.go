// focodebase/fobackend/internal/security/numeric_code_test.go
//
// Tests for GenerateNumericCode, the generator for the short manual
// email-confirmation credential.
package security

import (
	"errors"
	"testing"
)

func TestGenerateNumericCodeShape(t *testing.T) {
	for i := 0; i < 200; i++ {
		code, err := GenerateNumericCode(ActivationCodeDigits)
		if err != nil {
			t.Fatalf("generate: %v", err)
		}
		if len(code) != ActivationCodeDigits {
			t.Fatalf("code %q has length %d, want %d", code, len(code), ActivationCodeDigits)
		}
		for _, r := range code {
			if r < '0' || r > '9' {
				t.Fatalf("code %q contains non-digit %q", code, r)
			}
		}
	}
}

func TestGenerateNumericCodeIsNotConstant(t *testing.T) {
	seen := make(map[string]struct{})
	for i := 0; i < 50; i++ {
		code, err := GenerateNumericCode(ActivationCodeDigits)
		if err != nil {
			t.Fatalf("generate: %v", err)
		}
		seen[code] = struct{}{}
	}
	// 50 draws from 10^6 values colliding into a single value is not a
	// plausible outcome of a working CSPRNG.
	if len(seen) < 2 {
		t.Fatalf("generator produced %d distinct codes in 50 draws", len(seen))
	}
}

func TestGenerateNumericCodeRejectsOutOfBoundsLength(t *testing.T) {
	for _, digits := range []int{-1, 0, 4, 5, 11} {
		if _, err := GenerateNumericCode(digits); !errors.Is(err, ErrInvalidNumericCodeLength) {
			t.Errorf("digits=%d: error = %v, want ErrInvalidNumericCodeLength", digits, err)
		}
	}
}
