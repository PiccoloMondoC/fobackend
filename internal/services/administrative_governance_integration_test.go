// focodebase/fobackend/internal/services/administrative_governance_integration_test.go
//
// PostgreSQL integration tests for administrative governance. They prove the
// invariants that only the database can prove: Root uniqueness under races,
// trigger backstops on every path that could affect the Root, appointment /
// role-assignment consistency, transaction rollback, and audit outcomes.
//
// These tests DROP and recreate the public schema. They run only when both
// variables are set, and the URL must name a disposable database:
//
//	SAGRENTI_TEST_DATABASE_URL=postgres://user:pass@localhost:5432/sagrenti_governance_test
//	SAGRENTI_TEST_DATABASE_RESET=1
//
// The database role must be able to create extensions (uuid-ossp, citext,
// pgcrypto, btree_gist). The reconciliation subtest additionally needs
// superuser (session_replication_role) and skips itself otherwise.
package services

import (
	"context"
	"errors"
	"fmt"
	"os"
 "strings"
	"sync"
	"testing"
	"time"

	"github.com/PiccoloMondoC/focodebase/fobackend/internal/data"
	"github.com/PiccoloMondoC/focodebase/fobackend/internal/logging"
	"github.com/PiccoloMondoC/focodebase/fobackend/internal/security"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type stubSessionIssuer struct{}

func (stubSessionIssuer) GenerateTokensPair(context.Context, uuid.UUID) (string, string, error) {
	return "access", "refresh", nil
}

const testPassword = "Governance-Test-Passw0rd!"

type governanceHarness struct {
	t    *testing.T
	ctx  context.Context
	pool *pgxpool.Pool
	svc  *Service
}

func newGovernanceHarness(t *testing.T) *governanceHarness {
	t.Helper()
	url := os.Getenv("SAGRENTI_TEST_DATABASE_URL")
	if url == "" || os.Getenv("SAGRENTI_TEST_DATABASE_RESET") != "1" {
		t.Skip("set SAGRENTI_TEST_DATABASE_URL and SAGRENTI_TEST_DATABASE_RESET=1 to run governance integration tests")
	}
 parsed,err:=pgxpool.ParseConfig(url)
 if err!=nil{t.Fatalf("invalid test database configuration: %v",err)}
 if !strings.HasSuffix(parsed.ConnConfig.Database,"_governance_test") {
  t.Fatal("reset refused: database name must end in _governance_test")
 }
 ctx := context.Background()

 logger, err := logging.InitLogging("governance-integration-test")
	if err != nil {
		t.Fatalf("init logging: %v", err)
	}
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(pool.Close)

	if _, err := pool.Exec(ctx, `DROP SCHEMA public CASCADE; CREATE SCHEMA public;`); err != nil {
		t.Fatalf("reset schema: %v", err)
	}
	dbModel := &data.DBConnectionParamsModel{Logger: logger}
	if err := dbModel.CreateTables(pool); err != nil {
		t.Fatalf("create tables: %v", err)
	}
	if err := dbModel.SeedAllData(pool, data.OAuthClientSeedSecrets{}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	// Schema creation and seeding must be idempotent on an existing database.
	if err := dbModel.CreateTables(pool); err != nil {
		t.Fatalf("re-run create tables: %v", err)
	}
	if err := dbModel.SeedAllData(pool, data.OAuthClientSeedSecrets{}); err != nil {
		t.Fatalf("re-run seed: %v", err)
	}

	models := data.New(pool, logger)
	svc, err := NewService(logger, &models, &Config{
		DBTimeout:             15 * time.Second,
		ActivationTokenTTL:    time.Hour,
		PasswordResetTokenTTL: time.Hour,
	}, make(chan struct{}), stubSessionIssuer{})
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	return &governanceHarness{t: t, ctx: ctx, pool: pool, svc: svc}
}

// account creates a signup-shaped account: users row plus consumer primary role.
func (h *governanceHarness) account(label string, active bool) (uuid.UUID, string) {
	h.t.Helper()
	id := uuid.New()
	email := fmt.Sprintf("%s-%s@governance.test", label, id.String()[:8])
	hash, err := security.HashPassword(testPassword)
	if err != nil {
		h.t.Fatalf("hash: %v", err)
	}
	if _, err := h.pool.Exec(h.ctx, `INSERT INTO users (id, email, password_hash, is_active) VALUES ($1, $2, $3, $4)`,
		id, email, hash, active); err != nil {
		h.t.Fatalf("insert user: %v", err)
	}
	if _, err := h.pool.Exec(h.ctx, `
		INSERT INTO user_role_assignments (user_id, role_id, is_primary)
		SELECT $1, id, TRUE FROM roles WHERE name = 'consumer'`, id); err != nil {
		h.t.Fatalf("assign consumer: %v", err)
	}
	return id, email
}

func (h *governanceHarness) effectiveRole(id uuid.UUID) string {
	h.t.Helper()
	role, err := h.svc.Models.Role.GetRoleByUserID(h.ctx, id)
	if err != nil {
		if errors.Is(err, data.ErrRoleNotFound) {
			return ""
		}
		h.t.Fatalf("effective role: %v", err)
	}
	return role.Name
}

func (h *governanceHarness) exec(sql string, args ...any) error {
	_, err := h.pool.Exec(h.ctx, sql, args...)
	return data.TranslateGovernanceError(err)
}

func (h *governanceHarness) govern(actor, target uuid.UUID, op GovernanceOperation, password, confirm string) error {
	_, err := h.svc.ExecuteGovernanceOperationInternal(h.ctx, GovernanceRequest{
		ActorUserID:        actor,
		TargetUserID:       target,
		Operation:          op,
		Reason:             "integration test",
		CurrentPassword:    password,
		ConfirmTargetEmail: confirm,
	})
	return err
}

func (h *governanceHarness) count(sql string, args ...any) int {
	h.t.Helper()
	var n int
	if err := h.pool.QueryRow(h.ctx, sql, args...).Scan(&n); err != nil {
		h.t.Fatalf("count: %v", err)
	}
	return n
}

func expectErr(t *testing.T, what string, err, want error) {
	t.Helper()
	if !errors.Is(err, want) {
		t.Fatalf("%s: want %v, got %v", what, want, err)
	}
}

func expectOK(t *testing.T, what string, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("%s: unexpected error %v", what, err)
	}
}

func TestAdministrativeGovernanceIntegration(t *testing.T) {
	h := newGovernanceHarness(t)

	rootID, rootEmail := h.account("root", true)
	otherID, otherEmail := h.account("other", true)
	inactiveID, inactiveEmail := h.account("unconfirmed", false)
	_ = inactiveID

	t.Run("public signup cannot request administrative roles", func(t *testing.T) {
		tx, err := h.pool.Begin(h.ctx)
		expectOK(t, "begin", err)
		defer tx.Rollback(h.ctx)
		for _, name := range []string{"admin", "super_admin", "editor"} {
			_, err := h.svc.Models.Role.ResolveSignupRoleTx(h.ctx, tx, name)
			expectErr(t, "signup role "+name, err, data.ErrRoleNotAssignableAtSignup)
		}
		if err := h.exec(`UPDATE roles SET assignable_at_signup = TRUE WHERE name = 'admin'`); err == nil {
			t.Fatal("admin role was made signup-assignable")
		}
	})

	t.Run("seeded permissions separate admin from super admin", func(t *testing.T) {
		if n := h.count(`
			SELECT COUNT(*) FROM role_permissions rp JOIN roles r ON r.id = rp.role_id JOIN permissions p ON p.id = rp.permission_id
			WHERE r.name = 'admin' AND p.name IN ('appoint_admin','appoint_super_admin','update_role_permissions')`); n != 0 {
			t.Fatalf("admin holds %d super-admin-tier permissions", n)
		}
		if n := h.count(`
			SELECT COUNT(*) FROM permissions p WHERE NOT EXISTS (
				SELECT 1 FROM role_permissions rp JOIN roles r ON r.id = rp.role_id
				WHERE r.name = 'super_admin' AND rp.permission_id = p.id)`); n != 0 {
			t.Fatalf("super_admin is missing %d permissions", n)
		}
		if n := h.count(`SELECT COUNT(*) FROM permissions WHERE name IN ('list_roles','read_role','assign_role','revoke_role','read_user_roles','list_permissions','check_user_permission','read_role_permissions','update_role_permissions')`); n != 9 {
			t.Fatalf("route permissions not seeded: %d", n)
		}
	})

	t.Run("bootstrap refuses missing and unconfirmed accounts", func(t *testing.T) {
		_, _, err := h.svc.EstablishRootSuperAdminInternal(h.ctx, "nobody@governance.test")
		expectErr(t, "missing", err, ErrRootBootstrapAccountNotFound)
		_, _, err = h.svc.EstablishRootSuperAdminInternal(h.ctx, inactiveEmail)
		expectErr(t, "unconfirmed", err, ErrRootBootstrapAccountIneligible)
		if n := h.count(`SELECT COUNT(*) FROM root_super_admin`); n != 0 {
			t.Fatalf("root rows after refused bootstraps: %d", n)
		}
	})

	t.Run("concurrent bootstrap establishes exactly one root", func(t *testing.T) {
		const n = 8
		var wg sync.WaitGroup
		outcomes := make([]RootBootstrapOutcome, n)
		errs := make([]error, n)
		for i := 0; i < n; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				outcomes[i], _, errs[i] = h.svc.EstablishRootSuperAdminInternal(h.ctx, rootEmail)
			}(i)
		}
		wg.Wait()
		established := 0
		for i := range outcomes {
			expectOK(t, "concurrent bootstrap", errs[i])
			if outcomes[i] == RootBootstrapEstablished {
				established++
			}
		}
		if established != 1 {
			t.Fatalf("established %d times", established)
		}
		if c := h.count(`SELECT COUNT(*) FROM root_super_admin WHERE user_id = $1`, rootID); c != 1 {
			t.Fatalf("root rows: %d", c)
		}
		if c := h.count(`SELECT COUNT(*) FROM root_succession_events`); c != 1 {
			t.Fatalf("succession events: %d", c)
		}
		if role := h.effectiveRole(rootID); role != "super_admin" {
			t.Fatalf("root effective role: %q", role)
		}
		if c := h.count(`
			SELECT COUNT(*) FROM audit_logs al JOIN actions a ON a.id = al.action_id
			WHERE a.name = 'bootstrap_root_super_admin' AND al.entity_id = $1 AND al.outcome = 'succeeded'`, rootID.String()); c != 1 {
			t.Fatalf("bootstrap audit records: %d", c)
		}
	})

	t.Run("repeat and conflicting bootstrap never replace the root", func(t *testing.T) {
		outcome, id, err := h.svc.EstablishRootSuperAdminInternal(h.ctx, rootEmail)
		expectOK(t, "repeat", err)
		if outcome != RootBootstrapAlreadyEstablished || id != rootID {
			t.Fatalf("repeat: %s %s", outcome, id)
		}
		_, _, err = h.svc.EstablishRootSuperAdminInternal(h.ctx, otherEmail)
		expectErr(t, "other account", err, ErrRootBootstrapConflict)
		_, _, err = h.svc.EstablishRootSuperAdminInternal(h.ctx, "nobody@governance.test")
		expectErr(t, "missing account after root", err, ErrRootBootstrapConflict)
		gov := data.NewAdministrativeGovernanceModel(h.pool, h.svc.Logger)
		err = h.svc.withGovernanceTx(h.ctx, func(tx pgx.Tx) error {
			return gov.InsertRootSuperAdminTx(h.ctx, tx, otherID)
		})
		if err == nil {
			t.Fatal("a second Root row was inserted")
		}
	})

	t.Run("database protects the root on every direct path", func(t *testing.T) {
		statements := []string{
			`UPDATE users SET is_active = FALSE WHERE id = $1`,
			`UPDATE users SET deleted_at = NOW() WHERE id = $1`,
			`DELETE FROM users WHERE id = $1`,
			`UPDATE user_role_assignments SET deleted_at = NOW() WHERE user_id = $1`,
			`UPDATE user_role_assignments SET is_primary = FALSE WHERE user_id = $1`,
			`DELETE FROM user_role_assignments WHERE user_id = $1`,
			`UPDATE administrative_appointments SET status = 'suspended' WHERE user_id = $1`,
			`UPDATE administrative_appointments SET level = 'admin' WHERE user_id = $1`,
			`DELETE FROM administrative_appointments WHERE user_id = $1`,
			`DELETE FROM root_super_admin WHERE user_id = $1`,
			`UPDATE root_super_admin SET established_at = NOW() WHERE user_id = $1`,
			`UPDATE root_succession_events SET authorization_reference = 'x' WHERE successor_user_id = $1`,
		}
		for _, sql := range statements {
			if err := h.exec(sql, rootID); err == nil {
				t.Fatalf("statement succeeded against Root: %s", sql)
			}
		}
		if err := h.exec(`TRUNCATE root_super_admin`); err == nil {
			t.Fatal("root table truncated")
		}
		if role := h.effectiveRole(rootID); role != "super_admin" {
			t.Fatalf("root role after attacks: %q", role)
		}
		consumerID := h.roleID("consumer")
		superID := h.roleID("super_admin")
		if err := h.svc.Models.Role.AssignRoleToUser(h.ctx, rootID, consumerID, nil, true); err == nil {
			t.Fatal("generic route made another role the Root's primary")
		}
		if err := h.svc.Models.Role.RevokeRole(h.ctx, rootID, superID); err == nil {
			t.Fatal("generic route revoked the Root's governing role")
		}
		if err := h.exec(`UPDATE roles SET is_active = FALSE WHERE name = 'super_admin'`); !errors.Is(err, data.ErrGovernanceRoleProtected) {
			t.Fatalf("super_admin deactivated: %v", err)
		}
		if err := h.exec(`DELETE FROM role_permissions WHERE role_id = $1`, superID); !errors.Is(err, data.ErrGovernanceRoleProtected) {
			t.Fatalf("super_admin permissions stripped: %v", err)
		}
	})

	superID, superEmail := h.account("super", true)
	adminID, _ := h.account("admin", true)
	bID, bEmail := h.account("bravo", true)
	userID, _ := h.account("user", true)

	t.Run("root appoints an admin and a super admin with re-authentication", func(t *testing.T) {
		expectOK(t, "appoint admin", h.govern(rootID, adminID, GovernanceAppointAdmin, "", ""))
		if r := h.effectiveRole(adminID); r != "admin" {
			t.Fatalf("admin role: %q", r)
		}
		expectErr(t, "missing password", h.govern(rootID, superID, GovernanceAppointSuperAdmin, "", superEmail), ErrGovernanceInputInvalid)
		expectErr(t, "wrong password", h.govern(rootID, superID, GovernanceAppointSuperAdmin, "wrong-password", superEmail), ErrGovernanceReauthenticationFailed)
		expectErr(t, "wrong confirmation", h.govern(rootID, superID, GovernanceAppointSuperAdmin, testPassword, otherEmail), ErrGovernanceConfirmationMismatch)
		expectOK(t, "appoint super", h.govern(rootID, superID, GovernanceAppointSuperAdmin, testPassword, superEmail))
		if r := h.effectiveRole(superID); r != "super_admin" {
			t.Fatalf("super role: %q", r)
		}
		if c := h.count(`
			SELECT COUNT(*) FROM audit_logs al JOIN actions a ON a.id = al.action_id
			WHERE a.name = 'appoint_super_admin' AND al.entity_id = $1 AND al.outcome = 'denied'
			  AND al.context->>'denial_code' = 'reauthentication_failed'`, superID.String()); c != 1 {
			t.Fatalf("denied re-authentication audits: %d", c)
		}
		if c := h.count(`SELECT COUNT(*) FROM audit_logs WHERE context::text ILIKE '%' || $1 || '%'`, testPassword); c != 0 {
			t.Fatal("a password reached the audit log")
		}
	})

	t.Run("super admin appoints and promotes; hierarchy is enforced", func(t *testing.T) {
		expectOK(t, "super appoints admin", h.govern(superID, bID, GovernanceAppointAdmin, "", ""))
		expectOK(t, "super promotes admin", h.govern(superID, bID, GovernanceAppointSuperAdmin, testPassword, bEmail))
		if r := h.effectiveRole(bID); r != "super_admin" {
			t.Fatalf("promoted role: %q", r)
		}
		expectErr(t, "super suspends super", h.govern(superID, bID, GovernanceSuspend, "", ""), ErrGovernanceInsufficientAuthority)
		expectErr(t, "super demotes super", h.govern(superID, bID, GovernanceDemoteToAdmin, "", ""), ErrGovernanceInsufficientAuthority)
		expectErr(t, "admin appoints", h.govern(adminID, userID, GovernanceAppointAdmin, "", ""), ErrGovernanceInsufficientAuthority)
		expectErr(t, "admin suspends admin", h.govern(adminID, bID, GovernanceSuspend, "", ""), ErrGovernanceInsufficientAuthority)
		expectErr(t, "super governs root", h.govern(superID, rootID, GovernanceSuspend, "", ""), ErrGovernanceRootProtected)
		expectErr(t, "self demotion", h.govern(superID, superID, GovernanceRevoke, "", ""), ErrGovernanceSelfTarget)
		expectOK(t, "root demotes super", h.govern(rootID, bID, GovernanceDemoteToAdmin, "", ""))
		if r := h.effectiveRole(bID); r != "admin" {
			t.Fatalf("demoted role: %q", r)
		}
	})

	t.Run("suspension, restoration, and revocation move authority, not accounts", func(t *testing.T) {
		expectOK(t, "suspend admin", h.govern(superID, adminID, GovernanceSuspend, "", ""))
		if r := h.effectiveRole(adminID); r != "consumer" {
			t.Fatalf("suspended admin role: %q", r)
		}
		if c := h.count(`SELECT COUNT(*) FROM users WHERE id = $1 AND is_active = TRUE AND deleted_at IS NULL`, adminID); c != 1 {
			t.Fatal("suspension touched the account itself")
		}
		expectOK(t, "restore admin", h.govern(superID, adminID, GovernanceRestore, "", ""))
		if r := h.effectiveRole(adminID); r != "admin" {
			t.Fatalf("restored role: %q", r)
		}
		expectOK(t, "revoke admin", h.govern(superID, adminID, GovernanceRevoke, "", ""))
		if r := h.effectiveRole(adminID); r != "consumer" {
			t.Fatalf("revoked role: %q", r)
		}
	})

	t.Run("generic role routes cannot create, strip, or bypass administrators", func(t *testing.T) {
		adminRole := h.roleID("admin")
		err := h.svc.Models.Role.AssignRoleToUser(h.ctx, userID, adminRole, nil, true)
		expectErr(t, "generic admin grant", err, data.ErrAdministrativeInconsistency)
		if r := h.effectiveRole(userID); r != "consumer" {
			t.Fatalf("generic grant left role %q", r)
		}
		err = h.svc.Models.Role.RevokeRole(h.ctx, bID, adminRole)
		expectErr(t, "generic admin strip", err, data.ErrAdministrativeInconsistency)
		err = h.svc.Models.Role.AssignRoleToUser(h.ctx, bID, h.roleID("editor"), nil, true)
		expectErr(t, "generic primary override", err, data.ErrAdministrativeInconsistency)
		if r := h.effectiveRole(bID); r != "admin" {
			t.Fatalf("admin role after generic attempts: %q", r)
		}
	})

	t.Run("destructive account operations respect the hierarchy", func(t *testing.T) {
		expectErr(t, "expel active admin", h.svc.ExpelUserInternal(h.ctx, superID, bID), ErrGovernanceAdministratorAccount)
		expectErr(t, "expel root", h.svc.ExpelUserInternal(h.ctx, superID, rootID), ErrGovernanceRootProtected)
		expectErr(t, "expel self", h.svc.ExpelUserInternal(h.ctx, superID, superID), ErrGovernanceSelfTarget)
		expectErr(t, "non-admin expels", h.svc.ExpelUserInternal(h.ctx, userID, otherID), ErrGovernanceInsufficientAuthority)
		expectOK(t, "expel ordinary account", h.svc.ExpelUserInternal(h.ctx, bID, otherID))
		if c := h.count(`SELECT COUNT(*) FROM users WHERE id = $1 AND deleted_at IS NOT NULL`, otherID); c != 1 {
			t.Fatal("ordinary account was not closed")
		}
		expectErr(t, "admin self-closure", h.svc.CloseOwnAccountInternal(h.ctx, bID), ErrGovernanceAdministratorSelfClosure)
		expectErr(t, "root self-closure", h.svc.CloseOwnAccountInternal(h.ctx, rootID), ErrGovernanceRootProtected)
		expectOK(t, "former admin may close", h.svc.CloseOwnAccountInternal(h.ctx, adminID))
		if c := h.count(`
			SELECT COUNT(*) FROM audit_logs al JOIN actions a ON a.id = al.action_id
			WHERE a.name = 'expel_user' AND al.entity_id = $1 AND al.outcome = 'denied'`, rootID.String()); c < 1 {
			t.Fatal("denied expulsion of the Root was not audited")
		}
	})

	t.Run("concurrent appointments of one account apply exactly once", func(t *testing.T) {
		vID, _ := h.account("victor", true)
		var wg sync.WaitGroup
		results := make([]error, 2)
		for i, actor := range []uuid.UUID{rootID, superID} {
			wg.Add(1)
			go func(i int, actor uuid.UUID) {
				defer wg.Done()
				results[i] = h.govern(actor, vID, GovernanceAppointAdmin, "", "")
			}(i, actor)
		}
		wg.Wait()
		ok := 0
		for _, err := range results {
			if err == nil {
				ok++
			} else if !errors.Is(err, ErrGovernanceInvalidTransition) && !errors.Is(err, data.ErrConcurrentGovernanceChange) {
				t.Fatalf("unexpected race error: %v", err)
			}
		}
		if ok != 1 {
			t.Fatalf("successful concurrent appointments: %d", ok)
		}
		if c := h.count(`SELECT COUNT(*) FROM user_role_assignments ura JOIN roles r ON r.id = ura.role_id WHERE ura.user_id = $1 AND r.name = 'admin' AND ura.deleted_at IS NULL`, vID); c != 1 {
			t.Fatalf("admin assignments after race: %d", c)
		}
	})

	t.Run("a failed audit rolls back the governed change", func(t *testing.T) {
		wID, _ := h.account("whiskey", true)
		gov := data.NewAdministrativeGovernanceModel(h.pool, h.svc.Logger)
		err := h.svc.withGovernanceTx(h.ctx, func(tx pgx.Tx) error {
			if _, err := gov.LockGovernanceSubjectsTx(h.ctx, tx, wID); err != nil {
				return err
			}
			if _, err := gov.PutAppointmentTx(h.ctx, tx, data.AppointmentChange{
				UserID: wID, Level: data.AdministrativeLevelAdmin, Status: data.AppointmentStatusRevoked, NewTerm: true,
			}); err != nil {
				return err
			}
			return gov.InsertGovernanceAuditTx(h.ctx, tx, data.GovernanceAuditEntry{
				Action: "not_a_seeded_action", EntityType: data.GovernanceEntityAdministrator,
				EntityID: wID.String(), Outcome: data.GovernanceOutcomeSucceeded,
			})
		})
		expectErr(t, "unseeded audit action", err, data.ErrGovernanceAuditMetadataMissing)
		if c := h.count(`SELECT COUNT(*) FROM administrative_appointments WHERE user_id = $1`, wID); c != 0 {
			t.Fatal("appointment survived a rolled-back transaction")
		}
	})

	t.Run("directory, history, and standing", func(t *testing.T) {
		dir, err := h.svc.ListAdministratorsInternal(h.ctx, rootID, true)
		expectOK(t, "list administrators", err)
		if !dir.Actor.IsRoot || len(dir.Administrators) == 0 || !dir.Administrators[0].IsRoot {
			t.Fatalf("directory: %+v", dir.Actor)
		}
		if len(dir.Administrators[0].AvailableActions) != 0 {
			t.Fatal("actions offered against the Root")
		}
		_, err = h.svc.ListAdministratorsInternal(h.ctx, userID, false)
		expectErr(t, "non-admin directory", err, ErrGovernanceInsufficientAuthority)
		accounts, err := h.svc.SearchAccountsInternal(h.ctx, superID, "%", 25, 0)
		expectOK(t, "literal percent search", err)
		if len(accounts) != 0 {
			t.Fatal("'%' was treated as a wildcard")
		}
		history, err := h.svc.ListAdministratorHistoryInternal(h.ctx, rootID, superID, 50)
		expectOK(t, "history", err)
		if len(history) < 2 {
			t.Fatalf("history entries: %d", len(history))
		}
		standing, err := h.svc.GetAdministrativeStandingInternal(h.ctx, superID)
		expectOK(t, "standing", err)
		if standing.Level != data.AdministrativeLevelSuperAdmin || standing.IsRoot || standing.CanGovernSuperAdmins {
			t.Fatalf("super standing: %+v", standing)
		}
	})

	t.Run("startup reconciliation adopts pre-governance admins", func(t *testing.T) {
		legacyID, _ := h.account("legacy", true)
		tx, err := h.pool.Begin(h.ctx)
		expectOK(t, "begin", err)
		if _, err := tx.Exec(h.ctx, `SET LOCAL session_replication_role = replica`); err != nil {
			_ = tx.Rollback(h.ctx)
			t.Skip("reconciliation test needs superuser to simulate pre-governance data")
		}
		_, err = tx.Exec(h.ctx, `
			UPDATE user_role_assignments SET is_primary = FALSE WHERE user_id = $1;
		`, legacyID)
		expectOK(t, "clear primary", err)
		_, err = tx.Exec(h.ctx, `
			INSERT INTO user_role_assignments (user_id, role_id, is_primary)
			SELECT $1, id, TRUE FROM roles WHERE name = 'admin'`, legacyID)
		expectOK(t, "legacy grant", err)
		expectOK(t, "commit legacy", tx.Commit(h.ctx))

		n, err := h.svc.ReconcileAdministrativeAppointmentsInternal(h.ctx)
		expectOK(t, "reconcile", err)
		if n != 1 {
			t.Fatalf("reconciled %d", n)
		}
		if c := h.count(`SELECT COUNT(*) FROM administrative_appointments WHERE user_id = $1 AND level = 'admin' AND status = 'active'`, legacyID); c != 1 {
			t.Fatal("legacy admin not adopted")
		}
		n, err = h.svc.ReconcileAdministrativeAppointmentsInternal(h.ctx)
		expectOK(t, "reconcile again", err)
		if n != 0 {
			t.Fatalf("reconciliation is not idempotent: %d", n)
		}
	})
}

func (h *governanceHarness) roleID(name string) uuid.UUID {
	h.t.Helper()
	var id uuid.UUID
	if err := h.pool.QueryRow(h.ctx, `SELECT id FROM roles WHERE name = $1`, name).Scan(&id); err != nil {
		h.t.Fatalf("role %s: %v", name, err)
	}
	return id
}
