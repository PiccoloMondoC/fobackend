// focodebase/fobackend/internal/bootstrap/bootstrap_root_test.go
//
// Tests that startup configuration carries no privileged identity and
// refuses the retired ROOT_SUPER_ADMIN_BOOTSTRAP_EMAIL variable.
package bootstrap

import (
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"
)

func TestConfigCarriesNoPrivilegedIdentity(t *testing.T) {
	typ := reflect.TypeOf(Config{})
	for i := 0; i < typ.NumField(); i++ {
		name := typ.Field(i).Name
		lower := strings.ToLower(name)
		if strings.Contains(lower, "root") ||
			strings.Contains(lower, "bootstrapemail") ||
			strings.Contains(lower, "superadmin") {
			t.Fatalf("Config field %q carries privileged identity", name)
		}
	}
}

func TestRetiredPrivilegedIdentityVariableIsRefused(t *testing.T) {
	for _, value := range []string{"root@sagrenti.example", "", "  "} {
		t.Setenv(RetiredPrivilegedIdentityVariable, value)
		if err := refuseRetiredPrivilegedIdentity(); !errors.Is(err, ErrRetiredPrivilegedIdentity) {
			t.Fatalf("value %q: expected refusal, got %v", value, err)
		}
		if strings.Contains(ErrRetiredPrivilegedIdentity.Error(), "@") {
			t.Fatal("refusal message must not echo an address")
		}
	}
}

func TestRetiredPrivilegedIdentityVariableAbsentIsAccepted(t *testing.T) {
	t.Setenv(RetiredPrivilegedIdentityVariable, "")
	if err := os.Unsetenv(RetiredPrivilegedIdentityVariable); err != nil {
		t.Fatal(err)
	}
	if err := refuseRetiredPrivilegedIdentity(); err != nil {
		t.Fatalf("absent variable refused: %v", err)
	}
}

func TestLoadDatabaseConfigReadsNoSigningOrNotificationSecrets(t *testing.T) {
	t.Setenv(RetiredPrivilegedIdentityVariable, "")
	if err := os.Unsetenv(RetiredPrivilegedIdentityVariable); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GO_ENV", "dev")
	t.Setenv("DB_HOST", "localhost")
	t.Setenv("DB_PORT", "5432")
	t.Setenv("FOBACKEND_DB_USER", "fobackend")
	t.Setenv("FOBACKEND_DB_PASSWORD", "not-a-real-password")
	t.Setenv("FOBACKEND_DB_NAME", "sagrenti_dev")
	// Deliberately invalid: LoadDatabaseConfig must not read these.
	t.Setenv("JWT_PRIVATE_KEY_PATH", "/nonexistent/private.pem")
	t.Setenv("BASE_URL", "not a url")
	t.Setenv("EMAIL_PROVIDER", "invalid")

	cfg, err := LoadDatabaseConfig()
	if err != nil {
		t.Fatalf("LoadDatabaseConfig: %v", err)
	}
	if cfg.JWTPrivateKey != nil || cfg.SMTPPassword != "" || cfg.OAuthWebClientSecret != "" {
		t.Fatal("database-only configuration loaded signing or notification secrets")
	}
	if cfg.DBName != "sagrenti_dev" || cfg.Env != "dev" {
		t.Fatalf("unexpected database configuration: env=%q db=%q", cfg.Env, cfg.DBName)
	}
	if strings.Contains(cfg.String(), "not-a-real-password") {
		t.Fatal("String() exposed the database password")
	}
}

func TestLoadDatabaseConfigRefusesRetiredVariable(t *testing.T) {
	t.Setenv(RetiredPrivilegedIdentityVariable, "root@sagrenti.example")
	if _, err := LoadDatabaseConfig(); !errors.Is(err, ErrRetiredPrivilegedIdentity) {
		t.Fatalf("expected refusal, got %v", err)
	}
}
