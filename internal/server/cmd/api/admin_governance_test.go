// focodebase/fobackend/internal/server/cmd/api/admin_governance_test.go
//
// Pure unit tests for governance error mapping and Platform Settings
// administration governance. No database or HTTP server.
package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/PiccoloMondoC/focodebase/fobackend/internal/data"
	"github.com/PiccoloMondoC/focodebase/fobackend/internal/services"
)

func TestGovernanceErrorsNeverReportAuthenticationExpiry(t *testing.T) {
	errs := []error{
		services.ErrGovernanceSelfTarget,
		services.ErrGovernanceRootProtected,
		services.ErrGovernanceInsufficientAuthority,
		services.ErrGovernanceTargetNotFound,
		services.ErrGovernanceTargetIneligible,
		services.ErrGovernanceInvalidTransition,
		services.ErrGovernanceAdministratorAccount,
		services.ErrGovernanceReauthenticationFailed,
		services.ErrGovernanceReauthenticationUnavailable,
		services.ErrGovernanceConfirmationMismatch,
		services.ErrGovernanceInputInvalid,
		data.ErrConcurrentGovernanceChange,
		data.ErrAdministrativeInconsistency,
		errors.New("database exploded: password=secret"),
	}
	for _, err := range errs {
		status, code, message := governanceErrorResponse(fmt.Errorf("wrapped: %w", err))
		if status == http.StatusUnauthorized {
			t.Fatalf("%v mapped to 401", err)
		}
		if code == "" || message == "" {
			t.Fatalf("%v produced empty code or message", err)
		}
		if status >= 500 && (code != "server_error" || message != "internal server error") {
			t.Fatalf("server failure leaked detail: %s %s", code, message)
		}
	}
}

func TestGovernanceErrorCategories(t *testing.T) {
	cases := []struct {
		err    error
		status int
		code   string
	}{
		{services.ErrGovernanceInsufficientAuthority, 403, "insufficient_authority"},
		{services.ErrGovernanceRootProtected, 409, "root_protected"},
		{data.ErrRootSuperAdminProtected, 409, "root_protected"},
		{services.ErrGovernanceSelfTarget, 409, "self_governance_refused"},
		{services.ErrGovernanceTargetNotFound, 404, "account_not_found"},
		{services.ErrGovernanceInvalidTransition, 409, "invalid_transition"},
		{services.ErrGovernanceConfirmationMismatch, 400, "confirmation_mismatch"},
		{services.ErrGovernanceReauthenticationFailed, 403, "reauthentication_failed"},
	}
	for _, c := range cases {
		status, code, _ := governanceErrorResponse(c.err)
		if status != c.status || code != c.code {
			t.Fatalf("%v: got %d %s", c.err, status, code)
		}
	}
}

func TestRoleErrorsDoNotLeak(t *testing.T) {
	status, code, msg := roleErrorResponse(errors.New(`pq: relation "roles" ...`))
	if status != 500 || code != "server_error" || msg != "internal server error" {
		t.Fatalf("leak: %d %s %s", status, code, msg)
	}
	if s, c, _ := roleErrorResponse(data.ErrGovernanceRoleProtected); s != 409 || c != "protected_role" {
		t.Fatalf("protected role: %d %s", s, c)
	}
	if s, c, _ := roleErrorResponse(data.ErrLastActiveRole); s != 409 || c != "last_role" {
		t.Fatalf("last role: %d %s", s, c)
	}
}

func TestPlatformSettingsSwitchIsEnforced(t *testing.T) {
	ordinary := func(kind platformSettingMutationKind) platformSettingMutation {
		return platformSettingMutation{Kind: kind, Key: "merchant_onboarding_enabled", NewValue: json.RawMessage(`false`)}
	}
	kinds := []platformSettingMutationKind{
		platformSettingMutationCreate, platformSettingMutationEnsure, platformSettingMutationUpdateValue,
		platformSettingMutationSetActive, platformSettingMutationSoftDelete, platformSettingMutationHardDelete,
	}
	for _, kind := range kinds {
		for _, super := range []bool{false, true} {
			if code := decidePlatformSettingMutation(false, super, ordinary(kind)); code != "platform_settings_administration_disabled" {
				t.Fatalf("disabled %s super=%v: got %q", kind, super, code)
			}
			if code := decidePlatformSettingMutation(true, super, ordinary(kind)); code != "" {
				t.Fatalf("enabled %s super=%v: got %q", kind, super, code)
			}
		}
	}
}

func TestPlatformSettingsRecoveryPath(t *testing.T) {
	key := adminConsolePlatformSettingsAdminEnabledKey
	reEnable := platformSettingMutation{Kind: platformSettingMutationUpdateValue, Key: key, NewValue: json.RawMessage(" true ")}
	reActivate := platformSettingMutation{Kind: platformSettingMutationSetActive, Key: key, Activate: true}

	if code := decidePlatformSettingMutation(false, true, reEnable); code != "" {
		t.Fatalf("super admin cannot re-enable: %q", code)
	}
	if code := decidePlatformSettingMutation(false, true, reActivate); code != "" {
		t.Fatalf("super admin cannot re-activate: %q", code)
	}
	if code := decidePlatformSettingMutation(false, false, reEnable); code != "insufficient_authority" {
		t.Fatalf("admin recovered the switch: %q", code)
	}
	keepOff := platformSettingMutation{Kind: platformSettingMutationUpdateValue, Key: key, NewValue: json.RawMessage("false")}
	if code := decidePlatformSettingMutation(false, true, keepOff); code != "platform_settings_administration_disabled" {
		t.Fatalf("non-recovery change allowed while disabled: %q", code)
	}
}

func TestProtectedSettingsCannotBeDeleted(t *testing.T) {
	for _, key := range []string{adminConsolePlatformSettingsAdminEnabledKey, platformSettingsHardDeleteEnabledKey} {
		for _, kind := range []platformSettingMutationKind{platformSettingMutationSoftDelete, platformSettingMutationHardDelete} {
			if code := decidePlatformSettingMutation(true, true, platformSettingMutation{Kind: kind, Key: key}); code != "protected_platform_setting" {
				t.Fatalf("%s %s: %q", kind, key, code)
			}
		}
		if code := decidePlatformSettingMutation(true, false, platformSettingMutation{Kind: platformSettingMutationUpdateValue, Key: key, NewValue: json.RawMessage("false")}); code != "insufficient_authority" {
			t.Fatalf("admin changed governance setting %s: %q", key, code)
		}
	}
}

func TestInternalRoleClassificationIsCanonical(t *testing.T) {
	if !isInternalRole("super_admin") || !isInternalRole("admin") {
		t.Fatal("administrative roles must be internal")
	}
	if isInternalRole("internal_operator") || isInternalRole("editor") || isInternalRole("consumer") {
		t.Fatal("non-administrative roles must not be internal actors")
	}
}
