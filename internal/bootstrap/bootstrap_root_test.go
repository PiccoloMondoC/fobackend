// focodebase/fobackend/internal/bootstrap/bootstrap_root_test.go
//
// Tests for the ROOT_SUPER_ADMIN_BOOTSTRAP_EMAIL configuration boundary.
package bootstrap

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestRootBootstrapEmailValidation(t *testing.T) {
	if got, err := validateRootSuperAdminBootstrapEmail("  "); err != nil || got != "" {
		t.Fatalf("unset must be valid and empty: %q %v", got, err)
	}
	if got, err := validateRootSuperAdminBootstrapEmail(" root@sagrenti.example "); err != nil || got != "root@sagrenti.example" {
		t.Fatalf("valid address: %q %v", got, err)
	}
	for _, bad := range []string{
		"root", "@sagrenti.example", "root@", "a@b@c.example", "root@localhost",
		"root@sagrenti.example,other@sagrenti.example", "root @sagrenti.example",
		"<root@sagrenti.example>", strings.Repeat("a", 250) + "@x.example",
	} {
		if _, err := validateRootSuperAdminBootstrapEmail(bad); err == nil {
			t.Fatalf("accepted %q", bad)
		}
	}
}

func TestRootBootstrapEmailIsNeverPrintedInDiagnostics(t *testing.T) {
	cfg := &Config{RootSuperAdminBootstrapEmail: "root@sagrenti.example"}
	if strings.Contains(cfg.String(), "root@sagrenti.example") {
		t.Fatal("String() exposed the bootstrap email")
	}
	out, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(out), "root@sagrenti.example") || !strings.Contains(string(out), `"root_super_admin_bootstrap_email":"set"`) {
		t.Fatalf("MarshalJSON: %s", out)
	}
}
