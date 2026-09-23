package notificationservices

import "testing"

func TestValidateActivationURL_DefaultStrictRejectsHTTP(t *testing.T) {
	if _, err := ValidateActivationURL("http://localhost:8080/activate?token=abc"); err == nil {
		t.Fatal("expected strict validator to reject HTTP")
	}
}

func TestValidateActivationURLWithPolicy_DevLoopbackHTTP(t *testing.T) {
	policy := ActivationURLPolicy{AllowHTTPOnLoopback: true}

	accepted := []string{
		"http://localhost:8080/activate?token=abc",
		"http://127.0.0.1:8080/activate?token=abc",
		"http://[::1]:8080/activate?token=abc",
	}
	for _, raw := range accepted {
		if _, err := ValidateActivationURLWithPolicy(raw, policy); err != nil {
			t.Fatalf("expected %q to be accepted: %v", raw, err)
		}
	}

	rejected := []string{
		"http://example.com/activate?token=abc",
		"http://user:pass@localhost:8080/activate",
		"http://localhost:8080/activate#frag",
		"http://localhost:8080/../activate",
		"ftp://localhost:8080/activate",
	}
	for _, raw := range rejected {
		if _, err := ValidateActivationURLWithPolicy(raw, policy); err == nil {
			t.Fatalf("expected %q to be rejected", raw)
		}
	}
}

func TestValidateActivationURLWithPolicy_StrictAcceptsHTTPS(t *testing.T) {
	if _, err := ValidateActivationURLWithPolicy(
		"https://api.sagrenti.example/activate?token=abc",
		ActivationURLPolicy{},
	); err != nil {
		t.Fatalf("expected valid HTTPS URL: %v", err)
	}
}
