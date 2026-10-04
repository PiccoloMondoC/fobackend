// focodebase/fobackend/internal/bootstrap/bootstrap_frontend_test.go
//
// Tests for the browser-origin (FRONTEND_URL) and email-confirmation code
// configuration boundary.
package bootstrap

import (
	"strings"
	"testing"
)

func TestLoadFrontendURL(t *testing.T) {
	t.Run("dev defaults to the local Angular origin", func(t *testing.T) {
		t.Setenv("FRONTEND_URL", "")
		got, err := loadFrontendURL("dev")
		if err != nil || got != "http://localhost:4200" {
			t.Fatalf("got %q err=%v", got, err)
		}
	})

	t.Run("explicit value is canonicalized without trailing slash", func(t *testing.T) {
		t.Setenv("FRONTEND_URL", "http://127.0.0.1:4200/")
		got, err := loadFrontendURL("dev")
		if err != nil || got != "http://127.0.0.1:4200" {
			t.Fatalf("got %q err=%v", got, err)
		}
	})

	t.Run("prod requires an explicit value", func(t *testing.T) {
		t.Setenv("FRONTEND_URL", "")
		if _, err := loadFrontendURL("prod"); err == nil {
			t.Fatal("expected prod to require FRONTEND_URL")
		}
	})

	t.Run("prod requires https", func(t *testing.T) {
		t.Setenv("FRONTEND_URL", "http://localhost:4200")
		if _, err := loadFrontendURL("prod"); err == nil {
			t.Fatal("expected prod to reject http")
		}
	})

	t.Run("rejects a path-bearing frontend URL because CORS requires an origin", func(t *testing.T) {
		t.Setenv("FRONTEND_URL", "https://app.sagrenti.example/app")
		if _, err := loadFrontendURL("prod"); err == nil {
			t.Fatal("expected FRONTEND_URL path to be rejected")
		}
	})

	t.Run("prod accepts an https origin distinct from the API", func(t *testing.T) {
		t.Setenv("FRONTEND_URL", "https://app.sagrenti.example")
		got, err := loadFrontendURL("prod")
		if err != nil || got != "https://app.sagrenti.example" {
			t.Fatalf("got %q err=%v", got, err)
		}
	})

	t.Run("rejects query, fragment, and credentials", func(t *testing.T) {
		for _, raw := range []string{
			"https://app.sagrenti.example/?x=1",
			"https://app.sagrenti.example/#x",
			"https://user:pass@app.sagrenti.example",
		} {
			t.Setenv("FRONTEND_URL", raw)
			if _, err := loadFrontendURL("prod"); err == nil {
				t.Errorf("%q: expected rejection", raw)
			}
		}
	})
}

func TestLoadIntInRange(t *testing.T) {
	t.Setenv("ACTIVATION_CODE_MAX_ATTEMPTS", "")
	if got, err := loadIntInRange("ACTIVATION_CODE_MAX_ATTEMPTS", 5, 1, 10); err != nil || got != 5 {
		t.Fatalf("default: got %d err=%v", got, err)
	}

	t.Setenv("ACTIVATION_CODE_MAX_ATTEMPTS", "3")
	if got, err := loadIntInRange("ACTIVATION_CODE_MAX_ATTEMPTS", 5, 1, 10); err != nil || got != 3 {
		t.Fatalf("explicit: got %d err=%v", got, err)
	}

	for _, raw := range []string{"0", "11", "five"} {
		t.Setenv("ACTIVATION_CODE_MAX_ATTEMPTS", raw)
		if _, err := loadIntInRange("ACTIVATION_CODE_MAX_ATTEMPTS", 5, 1, 10); err == nil {
			t.Errorf("%q: expected rejection", raw)
		}
	}
}

func TestConfigRendersFrontendURL(t *testing.T) {
	cfg := &Config{FrontendURL: "https://app.sagrenti.example"}
	if !strings.Contains(cfg.String(), "https://app.sagrenti.example") {
		t.Fatal("String() omits FrontendURL")
	}
}
