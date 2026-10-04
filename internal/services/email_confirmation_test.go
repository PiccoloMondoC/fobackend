// focodebase/fobackend/internal/services/email_confirmation_test.go
//
// Database-free tests for manual confirmation-code normalization and the
// email-confirmation service configuration.
package services

import (
	"errors"
	"testing"
	"time"
)

func TestNormalizeActivationCode(t *testing.T) {
	accepted := map[string]string{
		"123456":     "123456",
		" 123456 ":   "123456",
		"123 456":    "123456",
		"123-456":    "123456",
		"012345":     "012345",
		"1\t2 3-456": "123456",
	}
	for in, want := range accepted {
		got, ok := NormalizeActivationCode(in)
		if !ok || got != want {
			t.Errorf("NormalizeActivationCode(%q) = %q, %v; want %q, true", in, got, ok, want)
		}
	}

	for _, in := range []string{"", "12345", "1234567", "12a456", "１２３４５６", "123456\n"} {
		if got, ok := NormalizeActivationCode(in); ok {
			t.Errorf("NormalizeActivationCode(%q) accepted as %q", in, got)
		}
	}
}

func validServiceConfig() *Config {
	return &Config{
		DBTimeout:             5 * time.Second,
		ActivationTokenTTL:    24 * time.Hour,
		PasswordResetTokenTTL: time.Hour,
	}
}

func TestConfigAppliesEmailConfirmationDefaults(t *testing.T) {
	cfg := validServiceConfig()

	if err := cfg.Validate(); err != nil {
		t.Fatalf("legacy configuration without code settings must stay valid: %v", err)
	}
	if cfg.EffectiveActivationCodeTTL() != DefaultActivationCodeTTL ||
		cfg.EffectiveActivationCodeMaxAttempts() != DefaultActivationCodeMaxAttempts ||
		cfg.EffectiveActivationCodeMaxTotalFailures() != DefaultActivationCodeMaxTotalFailures ||
		cfg.EffectiveActivationResendCooldown() != DefaultActivationResendCooldown {
		t.Fatal("defaults not applied")
	}
}

func TestConfigRejectsInvalidEmailConfirmationPolicy(t *testing.T) {
	cases := map[string]func(*Config){
		"code ttl too short":      func(c *Config) { c.ActivationCodeTTL = 10 * time.Second },
		"code ttl too long":       func(c *Config) { c.ActivationCodeTTL = 2 * time.Hour },
		"attempts negative":       func(c *Config) { c.ActivationCodeMaxAttempts = -1 },
		"attempts too high":       func(c *Config) { c.ActivationCodeMaxAttempts = 11 },
		"total below per-code":    func(c *Config) { c.ActivationCodeMaxAttempts = 5; c.ActivationCodeMaxTotalFailures = 3 },
		"total too high":          func(c *Config) { c.ActivationCodeMaxTotalFailures = 51 },
		"cooldown negative":       func(c *Config) { c.ActivationResendCooldown = -time.Second },
		"cooldown above one hour": func(c *Config) { c.ActivationResendCooldown = 2 * time.Hour },
	}

	for name, mutate := range cases {
		cfg := validServiceConfig()
		mutate(cfg)
		if err := cfg.Validate(); !errors.Is(err, ErrInvalidServiceConfiguration) {
			t.Errorf("%s: error = %v, want ErrInvalidServiceConfiguration", name, err)
		}
	}
}
