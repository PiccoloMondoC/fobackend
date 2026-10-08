// focodebase/fobackend/internal/data/administrative_governance_test.go
//
// Pure unit tests for governance data helpers. No database.
package data

import (
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestIsAdministrativeRoleNameIsTheSingleDefinition(t *testing.T) {
	for _, name := range []string{"super_admin", "admin", " Admin ", "SUPER_ADMIN"} {
		if !IsAdministrativeRoleName(name) {
			t.Fatalf("%q should be administrative", name)
		}
	}
	for _, name := range []string{"internal_operator", "editor", "viewer", "consumer", "merchant", ""} {
		if IsAdministrativeRoleName(name) {
			t.Fatalf("%q must not be administrative", name)
		}
	}
}

func TestGovernanceLockOrderIsDeterministic(t *testing.T) {
	a := uuid.MustParse("ffffffff-0000-0000-0000-000000000000")
	b := uuid.MustParse("00000000-0000-0000-0000-000000000001")
	got := SortedGovernanceLockOrder(a, b, a, uuid.Nil)
	if len(got) != 2 || got[0] != b || got[1] != a {
		t.Fatalf("lock order: %v", got)
	}
	again := SortedGovernanceLockOrder(b, a)
	if again[0] != got[0] || again[1] != got[1] {
		t.Fatal("lock order depends on argument order")
	}
}

func TestLikePatternEscaping(t *testing.T) {
	if got := EscapeLikePatternForTest(`a%b_c\d`); got != `a\%b\_c\\d` {
		t.Fatalf("escape: %q", got)
	}
}

func TestTranslateGovernanceError(t *testing.T) {
	cases := map[string]error{
		"SG001": ErrRootSuperAdminProtected,
		"SG002": ErrGovernanceRoleProtected,
		"SG003": ErrAdministrativeInconsistency,
		"SG004": ErrSuccessionLedgerImmutable,
		"40001": ErrConcurrentGovernanceChange,
		"40P01": ErrConcurrentGovernanceChange,
	}
	for code, want := range cases {
		got := TranslateGovernanceError(&pgconn.PgError{Code: code, Message: "trigger detail"})
		if !errors.Is(got, want) {
			t.Fatalf("%s: want %v got %v", code, want, got)
		}
	}
	other := errors.New("other")
	if TranslateGovernanceError(other) != other {
		t.Fatal("unrelated errors must pass through")
	}
}

func TestSubjectActiveLevel(t *testing.T) {
	s := &GovernanceSubject{IsActive: true, Appointment: &AdministrativeAppointment{
		Level: AdministrativeLevelAdmin, Status: AppointmentStatusSuspended}}
	if s.ActiveLevel() != AdministrativeLevelNone {
		t.Fatal("suspended appointment must confer no authority")
	}
	s.Appointment.Status = AppointmentStatusActive
	if s.ActiveLevel() != AdministrativeLevelAdmin {
		t.Fatal("active appointment confers its level")
	}
	s.IsDeleted = true
	if s.ActiveLevel() != AdministrativeLevelNone {
		t.Fatal("deleted account confers no authority")
	}
}
