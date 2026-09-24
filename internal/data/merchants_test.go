package data

import (
	"errors"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

func merchantTestStrPtr(s string) *string { return &s }

func TestValidateAndNormalizeMerchant_RejectsInvalidIdentity(t *testing.T) {
	tests := []struct {
		name string
		in   *Merchant
	}{
		{"blank name", &Merchant{Name: "   "}},
		{
			"credentialed website",
			&Merchant{Name: "Acme", Website: merchantTestStrPtr("https://user:secret@example.com")},
		},
		{
			"credentialed logo",
			&Merchant{Name: "Acme", LogoURL: merchantTestStrPtr("https://user:secret@example.com/logo.png")},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := validateAndNormalizeMerchant(tc.in)
			if !errors.Is(err, ErrMerchantInvalid) {
				t.Fatalf("expected ErrMerchantInvalid, got %v", err)
			}
		})
	}
}

func TestValidateAndNormalizeMerchant_NilIsProgrammingError(t *testing.T) {
	err := validateAndNormalizeMerchant(nil)
	if err == nil {
		t.Fatal("expected error for nil merchant")
	}
	if errors.Is(err, ErrMerchantInvalid) {
		t.Fatal("nil merchant must not be classified as caller input")
	}
}

func TestValidateAndNormalizeMerchant_CanonicalizesAcceptedIdentity(t *testing.T) {
	m := &Merchant{
		Name:    "  Acme Labs  ",
		Website: merchantTestStrPtr("  https://example.com  "),
		LogoURL: merchantTestStrPtr("   "),
	}

	if err := validateAndNormalizeMerchant(m); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m.Name != "Acme Labs" {
		t.Fatalf("name not canonicalized: %q", m.Name)
	}
	if m.Website == nil {
		t.Fatal("valid website must be retained")
	}
	if m.LogoURL != nil {
		t.Fatalf("blank logo URL must normalize to nil, got %q", *m.LogoURL)
	}
}

func TestIsMerchantNameConflict(t *testing.T) {
	nameConflict := &pgconn.PgError{Code: "23505", ConstraintName: merchantNameUniqueConstraint}
	otherUnique := &pgconn.PgError{Code: "23505", ConstraintName: "ux_something_else"}
	otherCode := &pgconn.PgError{Code: "23503", ConstraintName: merchantNameUniqueConstraint}

	if !isMerchantNameConflict(fmt.Errorf("wrapped: %w", nameConflict)) {
		t.Fatal("expected wrapped merchant-name violation to be detected")
	}
	if isMerchantNameConflict(otherUnique) {
		t.Fatal("unrelated unique constraint must not map to a merchant-name conflict")
	}
	if isMerchantNameConflict(otherCode) {
		t.Fatal("non-unique SQLSTATE must not map to a merchant-name conflict")
	}
	if isMerchantNameConflict(errors.New("plain")) {
		t.Fatal("non-PostgreSQL error must not map to a merchant-name conflict")
	}
}
