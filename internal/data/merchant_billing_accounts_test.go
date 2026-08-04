package data

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestNormalizeMerchantBillingAccountStatus(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   MerchantBillingAccountStatus
		want MerchantBillingAccountStatus
	}{
		{name: "active", in: " ACTIVE ", want: MerchantBillingAccountStatusActive},
		{name: "suspended", in: "Suspended", want: MerchantBillingAccountStatusSuspended},
		{name: "closed", in: " closed\t", want: MerchantBillingAccountStatusClosed},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := NormalizeMerchantBillingAccountStatus(tt.in); got != tt.want {
				t.Fatalf("NormalizeMerchantBillingAccountStatus(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestIsValidMerchantBillingAccountStatus(t *testing.T) {
	t.Parallel()

	valid := []MerchantBillingAccountStatus{
		MerchantBillingAccountStatusActive,
		MerchantBillingAccountStatusSuspended,
		MerchantBillingAccountStatusClosed,
		" ACTIVE ",
	}
	for _, status := range valid {
		if !IsValidMerchantBillingAccountStatus(status) {
			t.Fatalf("expected status %q to be valid", status)
		}
	}

	invalid := []MerchantBillingAccountStatus{"", "pending", "deleted", "active-now"}
	for _, status := range invalid {
		if IsValidMerchantBillingAccountStatus(status) {
			t.Fatalf("expected status %q to be invalid", status)
		}
	}
}

func TestNormalizeAndValidateMerchantBillingAccountCurrency(t *testing.T) {
	t.Parallel()

	if got := normalizeMerchantBillingAccountCurrency(" usd "); got != "USD" {
		t.Fatalf("normalizeMerchantBillingAccountCurrency() = %q, want USD", got)
	}

	valid := []string{"USD", "GBP", "GHS"}
	for _, currency := range valid {
		if err := validateMerchantBillingAccountCurrency(currency); err != nil {
			t.Fatalf("validateMerchantBillingAccountCurrency(%q) returned error: %v", currency, err)
		}
	}

	invalid := []string{"", "US", "USDX", "12A", "usd", "U D"}
	for _, currency := range invalid {
		err := validateMerchantBillingAccountCurrency(currency)
		if !errors.Is(err, ErrMerchantBillingAccountInvalidInput) {
			t.Fatalf("validateMerchantBillingAccountCurrency(%q) error = %v, want ErrMerchantBillingAccountInvalidInput", currency, err)
		}
	}
}

func TestValidateMerchantBillingAccountPersistedState(t *testing.T) {
	t.Parallel()

	valid := &MerchantBillingAccount{
		MerchantID: uuid.New(),
		Status:     MerchantBillingAccountStatusActive,
		Currency:   "USD",
		CreatedAt:  nonZeroTestTime(),
		UpdatedAt:  nonZeroTestTime(),
	}
	if err := validateMerchantBillingAccountPersistedState(valid); err != nil {
		t.Fatalf("valid account rejected: %v", err)
	}

	tests := []struct {
		name   string
		mutate func(*MerchantBillingAccount)
	}{
		{name: "nil merchant", mutate: func(a *MerchantBillingAccount) { a.MerchantID = uuid.Nil }},
		{name: "invalid status", mutate: func(a *MerchantBillingAccount) { a.Status = "pending" }},
		{name: "invalid currency", mutate: func(a *MerchantBillingAccount) { a.Currency = "usd" }},
		{name: "zero created at", mutate: func(a *MerchantBillingAccount) { a.CreatedAt = time.Time{} }},
		{name: "zero updated at", mutate: func(a *MerchantBillingAccount) { a.UpdatedAt = time.Time{} }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			account := *valid
			tt.mutate(&account)
			if err := validateMerchantBillingAccountPersistedState(&account); !errors.Is(err, ErrMerchantBillingAccountInvalidState) {
				t.Fatalf("error = %v, want ErrMerchantBillingAccountInvalidState", err)
			}
		})
	}
}

func TestMerchantBillingAccountStatusAmong(t *testing.T) {
	t.Parallel()

	candidates := []MerchantBillingAccountStatus{
		MerchantBillingAccountStatusActive,
		MerchantBillingAccountStatusSuspended,
	}
	if !merchantBillingAccountStatusAmong(MerchantBillingAccountStatusActive, candidates) {
		t.Fatal("expected active to be found")
	}
	if merchantBillingAccountStatusAmong(MerchantBillingAccountStatusClosed, candidates) {
		t.Fatal("did not expect closed to be found")
	}
}

func nonZeroTestTime() (t time.Time) {
	return time.Date(2026, 8, 3, 12, 0, 0, 0, time.UTC)
}
