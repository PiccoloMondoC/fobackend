package bootstrap

import (
	"strings"
	"testing"
)

func TestLoadEmailProviderEnvironmentBoundary(t *testing.T) {
	t.Run("dev defaults test", func(t *testing.T) {
		t.Setenv("EMAIL_PROVIDER", "")
		got, err := loadEmailProvider("dev")
		if err != nil || got != EmailProviderTest {
			t.Fatalf("got %q err=%v", got, err)
		}
	})

	t.Run("prod requires explicit provider", func(t *testing.T) {
		t.Setenv("EMAIL_PROVIDER", "")
		if _, err := loadEmailProvider("prod"); err == nil {
			t.Fatal("expected prod provider requirement")
		}
	})

	t.Run("prod forbids test provider", func(t *testing.T) {
		t.Setenv("EMAIL_PROVIDER", "test")
		if _, err := loadEmailProvider("prod"); err == nil {
			t.Fatal("expected prod to reject test provider")
		}
	})
}

func TestLoadSMTPSettingsSecurityBoundary(t *testing.T) {
	setBase := func(t *testing.T) {
		t.Setenv("SMTP_HOST", "mailpit")
		t.Setenv("SMTP_PORT", "1025")
		t.Setenv("SMTP_FROM", "Sagrenti <no-reply@sagrenti.local>")
		t.Setenv("SMTP_USERNAME", "")
		t.Setenv("SMTP_PASSWORD", "")
	}

	t.Run("dev accepts none", func(t *testing.T) {
		setBase(t)
		t.Setenv("SMTP_SECURITY", "none")
		cfg, err := loadSMTPSettings("dev", EmailProviderSMTP)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cfg.Security != SMTPSecurityNone {
			t.Fatalf("unexpected security: %q", cfg.Security)
		}
	})

	t.Run("prod rejects none", func(t *testing.T) {
		setBase(t)
		t.Setenv("SMTP_SECURITY", "none")
		if _, err := loadSMTPSettings("prod", EmailProviderSMTP); err == nil {
			t.Fatal("expected prod to reject plaintext SMTP")
		}
	})

	t.Run("credentials reject none", func(t *testing.T) {
		setBase(t)
		t.Setenv("SMTP_SECURITY", "none")
		t.Setenv("SMTP_USERNAME", "user")
		t.Setenv("SMTP_PASSWORD", "password")
		if _, err := loadSMTPSettings("dev", EmailProviderSMTP); err == nil {
			t.Fatal("expected plaintext authenticated SMTP to be rejected")
		}
	})
}

func TestValidateBaseURLForEnv(t *testing.T) {
	if err := validateBaseURLForEnv("dev", "http://localhost:8080"); err != nil {
		t.Fatalf("dev localhost should pass: %v", err)
	}
	if err := validateBaseURLForEnv("dev", "http://example.com"); err == nil {
		t.Fatal("dev remote HTTP should fail")
	}
	if err := validateBaseURLForEnv("prod", "http://localhost:8080"); err == nil {
		t.Fatal("prod HTTP should fail")
	}
	if err := validateBaseURLForEnv("prod", "https://api.sagrenti.example"); err != nil {
		t.Fatalf("prod HTTPS should pass: %v", err)
	}
}

func TestConfigRedactsSMTPCredentials(t *testing.T) {
	cfg := &Config{
		SMTPUsername: "secret-user",
		SMTPPassword: "secret-password",
	}
	for name, rendered := range map[string]string{
		"String":   cfg.String(),
		"GoString": cfg.GoString(),
		"LogValue": cfg.LogValue().String(),
	} {
		if strings.Contains(rendered, "secret-user") || strings.Contains(rendered, "secret-password") {
			t.Fatalf("%s leaked SMTP credentials", name)
		}
	}

	b, err := cfg.MarshalJSON()
	if err != nil {
		t.Fatalf("MarshalJSON: %v", err)
	}
	if strings.Contains(string(b), "secret-user") || strings.Contains(string(b), "secret-password") {
		t.Fatal("MarshalJSON leaked SMTP credentials")
	}
}
