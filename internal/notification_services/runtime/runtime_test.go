package notificationruntime

import (
	"testing"

	"github.com/PiccoloMondoC/focodebase/fobackend/internal/bootstrap"
)

func TestNewServicesRejectsNilConfig(t *testing.T) {
	if _, err := NewServices(nil, nil); err != ErrConfigRequired {
		t.Fatalf("expected ErrConfigRequired, got %v", err)
	}
}

func TestNewServicesBuildsTestProviderInDev(t *testing.T) {
	cfg := &bootstrap.Config{
		Env:           "dev",
		EmailProvider: bootstrap.EmailProviderTest,
	}
	svc, err := NewServices(cfg, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if svc == nil || svc.Email == nil || svc.SMS == nil {
		t.Fatal("expected complete notification services")
	}
}
