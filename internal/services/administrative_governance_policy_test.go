// focodebase/fobackend/internal/services/administrative_governance_policy_test.go
//
// Pure unit tests for the administrative authority policy. No database.
package services

import (
	"errors"
	"strings"
	"testing"

	"github.com/PiccoloMondoC/focodebase/fobackend/internal/data"

	"github.com/google/uuid"
)

var (
	idRoot  = uuid.MustParse("00000000-0000-0000-0000-0000000000a1")
	idSuper = uuid.MustParse("00000000-0000-0000-0000-0000000000b1")
	idSup2  = uuid.MustParse("00000000-0000-0000-0000-0000000000b2")
	idAdmin = uuid.MustParse("00000000-0000-0000-0000-0000000000c1")
	idAdm2  = uuid.MustParse("00000000-0000-0000-0000-0000000000c2")
	idUser  = uuid.MustParse("00000000-0000-0000-0000-0000000000d1")
)

func rootActor() GovernanceActor {
	return GovernanceActor{UserID: idRoot, Level: data.AdministrativeLevelSuperAdmin, IsRoot: true}
}
func superActor() GovernanceActor {
	return GovernanceActor{UserID: idSuper, Level: data.AdministrativeLevelSuperAdmin}
}
func adminActor() GovernanceActor {
	return GovernanceActor{UserID: idAdmin, Level: data.AdministrativeLevelAdmin}
}
func nobodyActor() GovernanceActor { return GovernanceActor{UserID: idUser} }

func rootTarget() GovernanceTarget {
	return GovernanceTarget{UserID: idRoot, Exists: true, IsActive: true, IsRoot: true,
		Level: data.AdministrativeLevelSuperAdmin, Status: data.AppointmentStatusActive}
}
func superTarget(status data.AppointmentStatus) GovernanceTarget {
	return GovernanceTarget{UserID: idSup2, Exists: true, IsActive: true,
		Level: data.AdministrativeLevelSuperAdmin, Status: status}
}
func adminTarget(status data.AppointmentStatus) GovernanceTarget {
	return GovernanceTarget{UserID: idAdm2, Exists: true, IsActive: true,
		Level: data.AdministrativeLevelAdmin, Status: status}
}
func userTarget() GovernanceTarget {
	return GovernanceTarget{UserID: idUser, Exists: true, IsActive: true}
}

var allOps = []GovernanceOperation{
	GovernanceAppointAdmin, GovernanceAppointSuperAdmin, GovernanceDemoteToAdmin,
	GovernanceSuspend, GovernanceRestore, GovernanceRevoke,
}

func TestRootIsProtectedFromEveryGovernanceOperation(t *testing.T) {
	for _, actor := range []GovernanceActor{superActor(), adminActor(), nobodyActor()} {
		for _, op := range allOps {
			err := AuthorizeGovernanceOperation(actor, rootTarget(), op)
			if err == nil {
				t.Fatalf("actor %v op %s against Root was allowed", actor, op)
			}
			if actor.Level == data.AdministrativeLevelSuperAdmin && !errors.Is(err, ErrGovernanceRootProtected) {
				t.Fatalf("super admin op %s against Root: want root_protected, got %v", op, err)
			}
		}
	}
	// A second Root-flagged account cannot be produced by ordinary operations,
	// and the Root cannot act on itself either.
	for _, op := range allOps {
		if err := AuthorizeGovernanceOperation(rootActor(), rootTarget(), op); !errors.Is(err, ErrGovernanceSelfTarget) {
			t.Fatalf("root self op %s: want self refusal, got %v", op, err)
		}
	}
}

func TestSelfTargetingIsAlwaysRefused(t *testing.T) {
	for _, op := range allOps {
		actor := superActor()
		self := superTarget(data.AppointmentStatusActive)
		self.UserID = actor.UserID
		if err := AuthorizeGovernanceOperation(actor, self, op); !errors.Is(err, ErrGovernanceSelfTarget) {
			t.Fatalf("op %s on self: want self refusal, got %v", op, err)
		}
	}
}

func TestAdminsGovernNoAdministrators(t *testing.T) {
	targets := []GovernanceTarget{userTarget(), adminTarget(data.AppointmentStatusActive),
		superTarget(data.AppointmentStatusActive), adminTarget(data.AppointmentStatusSuspended)}
	for _, target := range targets {
		for _, op := range allOps {
			if err := AuthorizeGovernanceOperation(adminActor(), target, op); !errors.Is(err, ErrGovernanceInsufficientAuthority) {
				t.Fatalf("admin op %s on %+v: want insufficient authority, got %v", op, target, err)
			}
		}
	}
}

func TestNonAdministratorsGovernNothing(t *testing.T) {
	for _, op := range allOps {
		if err := AuthorizeGovernanceOperation(nobodyActor(), adminTarget(data.AppointmentStatusActive), op); !errors.Is(err, ErrGovernanceInsufficientAuthority) {
			t.Fatalf("op %s: want insufficient authority, got %v", op, err)
		}
	}
}

func TestSuperAdminAppointments(t *testing.T) {
	cases := []struct {
		name   string
		actor  GovernanceActor
		target GovernanceTarget
		op     GovernanceOperation
		want   error
	}{
		{"super appoints admin", superActor(), userTarget(), GovernanceAppointAdmin, nil},
		{"root appoints admin", rootActor(), userTarget(), GovernanceAppointAdmin, nil},
		{"super appoints super", superActor(), userTarget(), GovernanceAppointSuperAdmin, nil},
		{"super promotes admin", superActor(), adminTarget(data.AppointmentStatusActive), GovernanceAppointSuperAdmin, nil},
		{"re-appoint revoked admin", superActor(), adminTarget(data.AppointmentStatusRevoked), GovernanceAppointAdmin, nil},
		{"appoint existing admin", superActor(), adminTarget(data.AppointmentStatusActive), GovernanceAppointAdmin, ErrGovernanceInvalidTransition},
		{"appoint suspended admin", superActor(), adminTarget(data.AppointmentStatusSuspended), GovernanceAppointAdmin, ErrGovernanceInvalidTransition},
		{"promote suspended admin", superActor(), adminTarget(data.AppointmentStatusSuspended), GovernanceAppointSuperAdmin, ErrGovernanceInvalidTransition},
		{"appoint existing super", superActor(), superTarget(data.AppointmentStatusActive), GovernanceAppointSuperAdmin, ErrGovernanceInvalidTransition},
		{"inactive account", superActor(), GovernanceTarget{UserID: idUser, Exists: true}, GovernanceAppointAdmin, ErrGovernanceTargetIneligible},
		{"missing account", superActor(), GovernanceTarget{UserID: idUser}, GovernanceAppointAdmin, ErrGovernanceTargetNotFound},
		{"deleted account", superActor(), GovernanceTarget{UserID: idUser, Exists: true, IsDeleted: true}, GovernanceAppointAdmin, ErrGovernanceTargetNotFound},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := AuthorizeGovernanceOperation(c.actor, c.target, c.op)
			if c.want == nil && err != nil {
				t.Fatalf("want allowed, got %v", err)
			}
			if c.want != nil && !errors.Is(err, c.want) {
				t.Fatalf("want %v, got %v", c.want, err)
			}
		})
	}
}

func TestGoverningSuperAdminsIsRootOnly(t *testing.T) {
	for _, op := range []GovernanceOperation{GovernanceDemoteToAdmin, GovernanceSuspend, GovernanceRevoke} {
		if err := AuthorizeGovernanceOperation(superActor(), superTarget(data.AppointmentStatusActive), op); !errors.Is(err, ErrGovernanceInsufficientAuthority) {
			t.Fatalf("super %s super: want insufficient authority, got %v", op, err)
		}
		if err := AuthorizeGovernanceOperation(rootActor(), superTarget(data.AppointmentStatusActive), op); err != nil {
			t.Fatalf("root %s super: want allowed, got %v", op, err)
		}
	}
	if err := AuthorizeGovernanceOperation(superActor(), superTarget(data.AppointmentStatusSuspended), GovernanceRestore); !errors.Is(err, ErrGovernanceInsufficientAuthority) {
		t.Fatalf("super restore super: want insufficient authority, got %v", err)
	}
	if err := AuthorizeGovernanceOperation(rootActor(), superTarget(data.AppointmentStatusSuspended), GovernanceRestore); err != nil {
		t.Fatalf("root restore super: %v", err)
	}
}

func TestSuperAdminGovernsAdmins(t *testing.T) {
	steps := []struct {
		target GovernanceTarget
		op     GovernanceOperation
		want   error
	}{
		{adminTarget(data.AppointmentStatusActive), GovernanceSuspend, nil},
		{adminTarget(data.AppointmentStatusSuspended), GovernanceRestore, nil},
		{adminTarget(data.AppointmentStatusActive), GovernanceRevoke, nil},
		{adminTarget(data.AppointmentStatusSuspended), GovernanceRevoke, nil},
		{adminTarget(data.AppointmentStatusActive), GovernanceRestore, ErrGovernanceInvalidTransition},
		{adminTarget(data.AppointmentStatusSuspended), GovernanceSuspend, ErrGovernanceInvalidTransition},
		{adminTarget(data.AppointmentStatusRevoked), GovernanceRevoke, ErrGovernanceInvalidTransition},
		{adminTarget(data.AppointmentStatusActive), GovernanceDemoteToAdmin, ErrGovernanceInsufficientAuthority},
	}
	for _, s := range steps {
		err := AuthorizeGovernanceOperation(superActor(), s.target, s.op)
		if (s.want == nil) != (err == nil) || (s.want != nil && !errors.Is(err, s.want)) {
			t.Fatalf("%s on %s admin: want %v got %v", s.op, s.target.Status, s.want, err)
		}
	}
	if err := AuthorizeGovernanceOperation(rootActor(), adminTarget(data.AppointmentStatusActive), GovernanceDemoteToAdmin); !errors.Is(err, ErrGovernanceInvalidTransition) {
		t.Fatalf("demoting an admin: want invalid transition, got %v", err)
	}
}

func TestAccountRemovalSafeguards(t *testing.T) {
	if err := AuthorizeAccountRemoval(adminActor(), userTarget()); err != nil {
		t.Fatalf("admin expels ordinary account: %v", err)
	}
	self := userTarget()
	self.UserID = idAdmin
	if err := AuthorizeAccountRemoval(adminActor(), self); !errors.Is(err, ErrGovernanceSelfTarget) {
		t.Fatalf("self expulsion: got %v", err)
	}
	if err := AuthorizeAccountRemoval(rootActor(), superTarget(data.AppointmentStatusActive)); !errors.Is(err, ErrGovernanceAdministratorAccount) {
		t.Fatalf("expel super admin: got %v", err)
	}
	if err := AuthorizeAccountRemoval(superActor(), adminTarget(data.AppointmentStatusSuspended)); !errors.Is(err, ErrGovernanceAdministratorAccount) {
		t.Fatalf("expel suspended admin: got %v", err)
	}
	if err := AuthorizeAccountRemoval(superActor(), adminTarget(data.AppointmentStatusRevoked)); err != nil {
		t.Fatalf("expel former admin: %v", err)
	}
	for _, actor := range []GovernanceActor{superActor(), adminActor()} {
		if err := AuthorizeAccountRemoval(actor, rootTarget()); !errors.Is(err, ErrGovernanceRootProtected) {
			t.Fatalf("expel Root: got %v", err)
		}
	}
	if err := AuthorizeAccountRemoval(nobodyActor(), adminTarget(data.AppointmentStatusRevoked)); !errors.Is(err, ErrGovernanceInsufficientAuthority) {
		t.Fatalf("non-admin expels: got %v", err)
	}
}

func TestAccountStewardshipHierarchy(t *testing.T) {
	if err := AuthorizeAccountStewardship(adminActor(), userTarget()); err != nil {
		t.Fatalf("admin moderates user: %v", err)
	}
	if err := AuthorizeAccountStewardship(adminActor(), adminTarget(data.AppointmentStatusActive)); !errors.Is(err, ErrGovernanceInsufficientAuthority) {
		t.Fatalf("admin moderates admin: %v", err)
	}
	if err := AuthorizeAccountStewardship(superActor(), superTarget(data.AppointmentStatusActive)); !errors.Is(err, ErrGovernanceInsufficientAuthority) {
		t.Fatalf("super moderates super: %v", err)
	}
	if err := AuthorizeAccountStewardship(rootActor(), superTarget(data.AppointmentStatusActive)); err != nil {
		t.Fatalf("root moderates super: %v", err)
	}
	if err := AuthorizeAccountStewardship(superActor(), rootTarget()); !errors.Is(err, ErrGovernanceRootProtected) {
		t.Fatalf("super moderates root: %v", err)
	}
}

func TestAvailableActionsMirrorPolicy(t *testing.T) {
	got := strings.Join(AvailableGovernanceActions(superActor(), adminTarget(data.AppointmentStatusActive)), ",")
	if got != "promote_to_super_admin,suspend,revoke" {
		t.Fatalf("super on active admin: %q", got)
	}
	got = strings.Join(AvailableGovernanceActions(rootActor(), superTarget(data.AppointmentStatusActive)), ",")
	if got != "demote_to_admin,suspend,revoke" {
		t.Fatalf("root on active super: %q", got)
	}
	got = strings.Join(AvailableGovernanceActions(superActor(), userTarget()), ",")
	if got != "appoint_admin,appoint_super_admin,expel" {
		t.Fatalf("super on user: %q", got)
	}
	if n := len(AvailableGovernanceActions(superActor(), rootTarget())); n != 0 {
		t.Fatalf("actions offered against Root: %d", n)
	}
	got = strings.Join(AvailableGovernanceActions(adminActor(), userTarget()), ",")
	if got != "expel" {
		t.Fatalf("admin on user: %q", got)
	}
}

func TestStandingCapabilities(t *testing.T) {
	if s := StandingFor(rootActor()); !s.IsRoot || !s.CanGovernSuperAdmins || !s.CanAppointSuperAdmins {
		t.Fatalf("root standing: %+v", s)
	}
	if s := StandingFor(superActor()); s.IsRoot || s.CanGovernSuperAdmins || !s.CanGovernAdmins {
		t.Fatalf("super standing: %+v", s)
	}
	if s := StandingFor(adminActor()); s.CanGovernAdmins || s.CanAppointSuperAdmins {
		t.Fatalf("admin standing: %+v", s)
	}
}

func TestDenialCodesAreStable(t *testing.T) {
	cases := map[error]string{
		ErrGovernanceSelfTarget:                  "self_governance_refused",
		ErrGovernanceRootProtected:               "root_protected",
		data.ErrRootSuperAdminProtected:          "root_protected",
		ErrGovernanceInsufficientAuthority:       "insufficient_authority",
		ErrGovernanceReauthenticationFailed:      "reauthentication_failed",
		ErrGovernanceReauthenticationUnavailable: "reauthentication_unavailable",
		ErrGovernanceConfirmationMismatch:        "confirmation_mismatch",
		data.ErrAdministrativeInconsistency:      "governance_invariant",
		data.ErrConcurrentGovernanceChange:       "concurrent_change",
		errors.New("boom"):                       "server_error",
	}
	for err, want := range cases {
		if got := GovernanceDenialCode(err); got != want {
			t.Fatalf("%v: want %s got %s", err, want, got)
		}
	}
}

func TestReasonNormalization(t *testing.T) {
	if r, err := normalizeGovernanceReason("   "); err != nil || r != nil {
		t.Fatalf("blank reason: %v %v", r, err)
	}
	if _, err := normalizeGovernanceReason(strings.Repeat("é", MaxGovernanceReasonLength+1)); !errors.Is(err, ErrGovernanceInputInvalid) {
		t.Fatalf("long reason accepted")
	}
	if r, err := normalizeGovernanceReason(strings.Repeat("é", MaxGovernanceReasonLength)); err != nil || r == nil {
		t.Fatalf("max-length reason rejected: %v", err)
	}
}
