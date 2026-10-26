// Package services provides regression specifications for the merchant-registration transaction state machine and canonical account establishment.
// focodebase/fobackend/internal/services/merchant_onboarding_internal_test.go
//
// GTM:
//     Layer: 2.4 Merchant Registration / Application Services
//     Release Class: SPINE
//     Reason: Supports the complete Foundation Level 4 merchant-registration and account-navigation vertical.
//
// SPINE Rule:
//     Keep compiling and preserve established security, lifecycle, and privacy contracts.
//     Preserve canonical principal ownership and backend authorization.
//     Keep private registration values out of public projections, logs, and errors.
//
// Database integration tests for the merchant registration lifecycle
// (Foundation Level 4 — Become a Merchant). They require
// FOBACKEND_TEST_DATABASE_URL; see merchant_registration_testdb_test.go.
package services

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/PiccoloMondoC/focodebase/fobackend/internal/data"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func regStr(s string) *string { return &s }

func fullRegistration(dba string) data.MerchantRegistrationFields {
	return data.MerchantRegistrationFields{
		LegalName:         regStr("Acme Holdings Ltd"),
		DBAName:           regStr(dba),
		TaxID:             regStr("12-3456789"),
		AddressLine1:      regStr("1 Market Street"),
		AddressCity:       regStr("San Francisco"),
		AddressRegion:     regStr("CA"),
		AddressPostalCode: regStr("94105"),
		AddressCountry:    regStr("US"),
		CorporateEmail:    regStr("ops@acme.example"),
		ContactName:       regStr("Jordan Lee"),
		Phone:             regStr("+14155550100"),
		Website:           regStr("https://acme.example/"),
	}
}

func uniqueBrand(prefix string) string {
	return fmt.Sprintf("%s %s", prefix, strings.ReplaceAll(uuid.NewString()[:13], "-", ""))
}

type registrationFixture struct {
	t       *testing.T
	service *Service
	pool    *pgxpool.Pool
	users   []uuid.UUID
}

func newRegistrationFixture(t *testing.T) *registrationFixture {
	t.Helper()
	service, pool := newMerchantRegistrationTestService(t)
	f := &registrationFixture{t: t, service: service, pool: pool}
	f.ensureRole("consumer", false)
	f.ensureRole("merchant", false)
	f.ensureRole("viewer", true)
	t.Cleanup(f.cleanup)
	return f
}

func (f *registrationFixture) exec(sql string, args ...any) {
	f.t.Helper()
	if _, err := f.pool.Exec(context.Background(), sql, args...); err != nil {
		f.t.Fatalf("exec %q: %v", strings.Fields(sql)[0], err)
	}
}

func (f *registrationFixture) count(sql string, args ...any) int {
	f.t.Helper()
	var n int
	if err := f.pool.QueryRow(context.Background(), sql, args...).Scan(&n); err != nil {
		f.t.Fatalf("count: %v", err)
	}
	return n
}

func (f *registrationFixture) ensureRole(name string, internal bool) {
	f.exec(`
		INSERT INTO roles (name, description, hierarchy_level, is_internal, assignable_at_signup)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (name) DO NOTHING`,
		name, name+" (test fixture)", map[bool]int{true: 1, false: 0}[internal], internal, !internal)
}

func (f *registrationFixture) user(role string, active bool) uuid.UUID {
	f.t.Helper()
	id := uuid.New()
	f.exec(`INSERT INTO users (id, email, is_active) VALUES ($1, $2, $3)`,
		id, fmt.Sprintf("reg-%s@test.example", id), active)
	f.exec(`
		INSERT INTO user_role_assignments (user_id, role_id, is_primary)
		SELECT $1, r.id, TRUE FROM roles r WHERE r.name = $2`, id, role)
	f.users = append(f.users, id)
	return id
}

func (f *registrationFixture) cleanup() {
	ctx := context.Background()
	for _, id := range f.users {
		_, _ = f.pool.Exec(ctx, `DELETE FROM merchant_registrations WHERE principal_user_id = $1`, id)
		_, _ = f.pool.Exec(ctx, `
			DELETE FROM outbox_events
			WHERE aggregate_type = 'merchant'
			  AND aggregate_id IN (SELECT merchant_id FROM merchant_accounts WHERE principal_user_id = $1)`, id)
		_, _ = f.pool.Exec(ctx, `
			WITH removed AS (
				DELETE FROM merchant_accounts WHERE principal_user_id = $1 RETURNING merchant_id
			)
			DELETE FROM merchants WHERE id IN (SELECT merchant_id FROM removed)`, id)
		_, _ = f.pool.Exec(ctx, `DELETE FROM users WHERE id = $1`, id)
	}
}

func (f *registrationFixture) save(user uuid.UUID, revision int64, fields data.MerchantRegistrationFields) (*MerchantRegistrationView, error) {
	return f.service.SaveOwnMerchantRegistrationInternal(context.Background(), user,
		MerchantRegistrationInput{ExpectedRevision: revision, Fields: fields})
}

func (f *registrationFixture) complete(user uuid.UUID, revision int64, fields data.MerchantRegistrationFields) (*MerchantRegistrationCompletion, error) {
	return f.service.CompleteOwnMerchantRegistrationInternal(context.Background(), user,
		MerchantRegistrationInput{ExpectedRevision: revision, Fields: fields})
}

func (f *registrationFixture) merchantCounts(user uuid.UUID) (accounts int, events int) {
	accounts = f.count(`SELECT COUNT(*) FROM merchant_accounts WHERE principal_user_id = $1`, user)
	events = f.count(`
		SELECT COUNT(*) FROM outbox_events
		WHERE event_type = 'merchant.onboarded.v1'
		  AND payload->>'principal_user_id' = $1::text`, user)
	return accounts, events
}

func fieldCodes(t *testing.T, err error) data.MerchantRegistrationFieldErrors {
	t.Helper()
	var validation *data.MerchantRegistrationValidationError
	if !errors.As(err, &validation) {
		t.Fatalf("expected registration validation error, got %v", err)
	}
	return validation.Fields
}

// 1, 2, 9: partial draft save, retrieval across sessions, and no authority.
func TestMerchantRegistration_PartialDraftSurvivesSessionsWithoutAuthority(t *testing.T) {
	f := newRegistrationFixture(t)
	user := f.user("consumer", true)

	brand := uniqueBrand("Draft")
	view, err := f.save(user, 0, data.MerchantRegistrationFields{
		DBAName:        regStr("  " + brand + "  "),
		CorporateEmail: regStr("Owner@Draft.Example"),
	})
	if err != nil {
		t.Fatalf("save partial draft: %v", err)
	}
	if view.Registration == nil || view.Registration.Status != data.MerchantRegistrationStatusDraft ||
		view.Registration.Revision != 1 {
		t.Fatalf("unexpected draft: %+v", view.Registration)
	}

	// A later session is a later request by the same authenticated user; the
	// server holds no session-scoped draft state.
	resumed, err := f.service.GetOwnMerchantRegistrationInternal(context.Background(), user)
	if err != nil {
		t.Fatalf("resume: %v", err)
	}
	if !resumed.Eligible || resumed.Registration == nil ||
		*resumed.Registration.Fields.DBAName != brand ||
		*resumed.Registration.Fields.CorporateEmail != "owner@draft.example" ||
		resumed.Registration.Fields.TaxID != nil {
		t.Fatalf("resumed draft does not match the saved facts: %+v", resumed.Registration)
	}
	if resumed.Account != nil || resumed.Merchant != nil {
		t.Fatal("a draft must not create a Merchant Account or Merchant")
	}

	contexts, err := f.service.Models.MerchantAccount.ListActiveOperatingContextsForPrincipal(context.Background(), user)
	if err != nil || len(contexts) != 0 {
		t.Fatalf("a draft must expose no active merchant context (contexts=%d, err=%v)", len(contexts), err)
	}

	state, eligible, err := f.service.GetOwnMerchantNavigationInternal(context.Background(), user)
	if err != nil || !eligible || state.RegistrationStatus == nil ||
		*state.RegistrationStatus != data.MerchantRegistrationStatusDraft || state.AccountStatus != nil {
		t.Fatalf("navigation must report an in-progress registration: %+v %v %v", state, eligible, err)
	}
}

// 3, 8: all eight facts required; failed completion preserves the draft.
func TestMerchantRegistration_CompletionRequiresAllFactsAndPreservesDraft(t *testing.T) {
	f := newRegistrationFixture(t)
	user := f.user("consumer", true)

	partial := fullRegistration(uniqueBrand("Partial"))
	partial.Phone = nil
	partial.Website = nil

	_, err := f.complete(user, 0, partial)
	codes := fieldCodes(t, err)
	if codes["phone"] != "required" || codes["website"] != "required" || len(codes) != 2 {
		t.Fatalf("expected phone and website required, got %v", codes)
	}

	stored, err := f.service.Models.MerchantRegistration.GetByPrincipal(context.Background(), user)
	if err != nil || stored == nil {
		t.Fatalf("failed completion must preserve the submitted draft: %v", err)
	}
	if stored.Status != data.MerchantRegistrationStatusDraft || stored.Fields.LegalName == nil {
		t.Fatalf("draft facts were not preserved: %+v", stored)
	}
	if accounts, events := f.merchantCounts(user); accounts != 0 || events != 0 {
		t.Fatalf("incomplete registration created merchant state: accounts=%d events=%d", accounts, events)
	}

	// Recoverable: supply the missing facts against the preserved revision.
	completion, err := f.complete(user, stored.Revision, fullRegistration(*partial.DBAName))
	if err != nil || !completion.NewlyCompleted {
		t.Fatalf("completion after correction failed: %v", err)
	}
}

// 7, 10: atomic creation and correct activation of merchant context.
func TestMerchantRegistration_CompletionActivatesCanonicalMerchantContext(t *testing.T) {
	f := newRegistrationFixture(t)
	user := f.user("consumer", true)
	brand := uniqueBrand("Active")

	completion, err := f.complete(user, 0, fullRegistration(brand))
	if err != nil {
		t.Fatalf("complete: %v", err)
	}
	if !completion.NewlyCompleted || !completion.MerchantCreated {
		t.Fatalf("expected a newly created merchant: %+v", completion)
	}

	view := completion.View
	if view.Registration.Status != data.MerchantRegistrationStatusCompleted ||
		view.Registration.CompletedAt == nil || view.Registration.MerchantID == nil {
		t.Fatalf("registration not completed: %+v", view.Registration)
	}
	if view.Account == nil || view.Account.AccountStatus != data.MerchantAccountStatusActive ||
		view.Account.OnboardedAt == nil || view.Account.PrincipalUserID != user ||
		view.Account.MerchantID != *view.Registration.MerchantID {
		t.Fatalf("account not established as the active principal account: %+v", view.Account)
	}

	// 12: only the DBA becomes public Merchant identity.
	if view.Merchant == nil || view.Merchant.Name != brand || view.Merchant.Website != nil || view.Merchant.LogoURL != nil {
		t.Fatalf("Merchant must carry only the public DBA: %+v", view.Merchant)
	}

	contexts, err := f.service.Models.MerchantAccount.ListActiveOperatingContextsForPrincipal(context.Background(), user)
	if err != nil || len(contexts) != 1 || contexts[0].MerchantName != brand {
		t.Fatalf("completed registration must expose exactly one active context: %v %v", contexts, err)
	}
	authorized, err := f.service.Models.MerchantAccount.IsPrincipalForMerchant(context.Background(), user, view.Account.MerchantID)
	if err != nil || !authorized {
		t.Fatalf("principal authority not established: %v %v", authorized, err)
	}

	if accounts, events := f.merchantCounts(user); accounts != 1 || events != 1 {
		t.Fatalf("expected one account and one onboarding event, got %d/%d", accounts, events)
	}

	state, _, err := f.service.GetOwnMerchantNavigationInternal(context.Background(), user)
	if err != nil || state.AccountStatus == nil || *state.AccountStatus != data.MerchantAccountStatusActive ||
		state.PublicName == nil || *state.PublicName != brand {
		t.Fatalf("navigation must report the active merchant: %+v %v", state, err)
	}
}

// 6, 8: completion retries are idempotent.
func TestMerchantRegistration_CompletionRetryIsIdempotent(t *testing.T) {
	f := newRegistrationFixture(t)
	user := f.user("merchant", true)
	fields := fullRegistration(uniqueBrand("Retry"))

	first, err := f.complete(user, 0, fields)
	if err != nil {
		t.Fatalf("first completion: %v", err)
	}
	// The client lost the first response and retries with its stale revision.
	second, err := f.complete(user, 0, fields)
	if err != nil {
		t.Fatalf("retried completion must succeed: %v", err)
	}
	if second.NewlyCompleted || second.MerchantCreated {
		t.Fatal("a retry must not report a new completion")
	}
	if second.View.Account.MerchantID != first.View.Account.MerchantID {
		t.Fatal("a retry must resolve to the same Merchant")
	}
	if accounts, events := f.merchantCounts(user); accounts != 1 || events != 1 {
		t.Fatalf("retry duplicated merchant state: %d/%d", accounts, events)
	}
}

// 6: concurrent completion creates exactly one Merchant and account.
func TestMerchantRegistration_ConcurrentCompletionCreatesOneMerchant(t *testing.T) {
	f := newRegistrationFixture(t)
	user := f.user("consumer", true)
	fields := fullRegistration(uniqueBrand("Concurrent"))

	const workers = 8
	var (
		wg      sync.WaitGroup
		start   = make(chan struct{})
		results = make([]*MerchantRegistrationCompletion, workers)
		errs    = make([]error, workers)
	)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			results[i], errs[i] = f.complete(user, 0, fields)
		}(i)
	}
	close(start)
	wg.Wait()

	newly := 0
	var merchantID uuid.UUID
	for i := 0; i < workers; i++ {
		if errs[i] != nil {
			t.Fatalf("worker %d: identical concurrent completion must succeed: %v", i, errs[i])
		}
		if results[i].NewlyCompleted {
			newly++
		}
		if merchantID == uuid.Nil {
			merchantID = results[i].View.Account.MerchantID
		} else if results[i].View.Account.MerchantID != merchantID {
			t.Fatal("concurrent completions resolved to different Merchants")
		}
	}
	if newly != 1 {
		t.Fatalf("exactly one request must complete the registration, got %d", newly)
	}
	if accounts, events := f.merchantCounts(user); accounts != 1 || events != 1 {
		t.Fatalf("concurrency duplicated merchant state: %d/%d", accounts, events)
	}
	if n := f.count(`SELECT COUNT(*) FROM merchant_registrations WHERE principal_user_id = $1`, user); n != 1 {
		t.Fatalf("expected one registration row, got %d", n)
	}
}

// 6: concurrent first saves with different content are deterministic.
func TestMerchantRegistration_ConcurrentDraftCreationIsDeterministic(t *testing.T) {
	f := newRegistrationFixture(t)
	user := f.user("consumer", true)

	const workers = 6
	var (
		wg    sync.WaitGroup
		start = make(chan struct{})
		errs  = make([]error, workers)
	)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			_, errs[i] = f.save(user, 0, data.MerchantRegistrationFields{
				DBAName: regStr(fmt.Sprintf("Variant %d", i)),
			})
		}(i)
	}
	close(start)
	wg.Wait()

	succeeded := 0
	for _, err := range errs {
		switch {
		case err == nil:
			succeeded++
		case errors.Is(err, ErrMerchantRegistrationEditConflict):
		default:
			t.Fatalf("unexpected error: %v", err)
		}
	}
	if succeeded != 1 {
		t.Fatalf("exactly one differing first save may win, got %d", succeeded)
	}
	if n := f.count(`SELECT COUNT(*) FROM merchant_registrations WHERE principal_user_id = $1`, user); n != 1 {
		t.Fatalf("expected one registration row, got %d", n)
	}
}

// Deterministic repeated saves and simultaneous updates.
func TestMerchantRegistration_RevisionsGuardSimultaneousEdits(t *testing.T) {
	f := newRegistrationFixture(t)
	user := f.user("consumer", true)

	v1, err := f.save(user, 0, data.MerchantRegistrationFields{LegalName: regStr("First Legal Name")})
	if err != nil {
		t.Fatal(err)
	}
	// Identical repeat with a stale revision is an idempotent no-op.
	again, err := f.save(user, 0, data.MerchantRegistrationFields{LegalName: regStr("First Legal Name")})
	if err != nil || again.Registration.Revision != v1.Registration.Revision {
		t.Fatalf("identical repeat must be a no-op: %v", err)
	}

	tabA, err := f.save(user, 1, data.MerchantRegistrationFields{LegalName: regStr("Tab A Legal Name")})
	if err != nil || tabA.Registration.Revision != 2 {
		t.Fatalf("first editor must win: %v", err)
	}
	_, err = f.save(user, 1, data.MerchantRegistrationFields{LegalName: regStr("Tab B Legal Name")})
	if !errors.Is(err, ErrMerchantRegistrationEditConflict) {
		t.Fatalf("stale editor must conflict, got %v", err)
	}
	stored, _ := f.service.Models.MerchantRegistration.GetByPrincipal(context.Background(), user)
	if *stored.Fields.LegalName != "Tab A Legal Name" || stored.Revision != 2 {
		t.Fatalf("conflict must not overwrite: %+v", stored)
	}

	_, err = f.save(user, 0, data.MerchantRegistrationFields{TaxID: regStr("bad#tax")})
	if codes := fieldCodes(t, err); codes["tax_id"] != "invalid" {
		t.Fatalf("invalid draft facts must be refused: %v", codes)
	}
}

// 7, 8: a taken DBA rolls the completion back and the draft stays usable.
func TestMerchantRegistration_TakenBrandRollsBackAndIsRecoverable(t *testing.T) {
	f := newRegistrationFixture(t)
	owner := f.user("consumer", true)
	other := f.user("consumer", true)
	brand := uniqueBrand("Shared")

	if _, err := f.complete(owner, 0, fullRegistration(brand)); err != nil {
		t.Fatalf("owner completion: %v", err)
	}

	_, err := f.complete(other, 0, fullRegistration(strings.ToUpper(brand)))
	if codes := fieldCodes(t, err); codes["dba_name"] != "taken" {
		t.Fatalf("a case-insensitively taken DBA must be reported, got %v", codes)
	}
	if accounts, events := f.merchantCounts(other); accounts != 0 || events != 0 {
		t.Fatalf("failed completion left merchant state: %d/%d", accounts, events)
	}
	stored, _ := f.service.Models.MerchantRegistration.GetByPrincipal(context.Background(), other)
	if stored == nil || stored.Status != data.MerchantRegistrationStatusDraft || stored.Fields.TaxID == nil {
		t.Fatalf("draft must survive a failed completion: %+v", stored)
	}

	if _, err := f.complete(other, stored.Revision, fullRegistration(uniqueBrand("Distinct"))); err != nil {
		t.Fatalf("completion with a distinct DBA must succeed: %v", err)
	}
}

// 7: any failure inside the completion transaction rolls back every write.
func TestMerchantRegistration_CompletionFailureRollsBackAtomically(t *testing.T) {
	f := newRegistrationFixture(t)
	user := f.user("consumer", true)
	brand := uniqueBrand("Atomic")

	functionName := "test_fail_registration_" + strings.ReplaceAll(user.String(), "-", "")
	f.exec(fmt.Sprintf(`
		CREATE FUNCTION %s() RETURNS trigger LANGUAGE plpgsql AS $$
		BEGIN
			IF NEW.registration_status = 'completed' AND NEW.principal_user_id = '%s'::uuid THEN
				RAISE EXCEPTION 'injected completion failure';
			END IF;
			RETURN NEW;
		END $$`, functionName, user))
	f.exec(fmt.Sprintf(`CREATE TRIGGER %s BEFORE UPDATE ON merchant_registrations
		FOR EACH ROW EXECUTE FUNCTION %s()`, functionName, functionName))
	dropped := false
	drop := func() {
		if dropped {
			return
		}
		dropped = true
		f.exec(fmt.Sprintf(`DROP TRIGGER IF EXISTS %s ON merchant_registrations`, functionName))
		f.exec(fmt.Sprintf(`DROP FUNCTION IF EXISTS %s()`, functionName))
	}
	t.Cleanup(drop)

	if _, err := f.complete(user, 0, fullRegistration(brand)); err == nil {
		t.Fatal("injected failure must fail completion")
	}
	if n := f.count(`SELECT COUNT(*) FROM merchants WHERE name = $1`, brand); n != 0 {
		t.Fatal("Merchant insert was not rolled back")
	}
	if accounts, events := f.merchantCounts(user); accounts != 0 || events != 0 {
		t.Fatalf("account or event was not rolled back: %d/%d", accounts, events)
	}
	stored, _ := f.service.Models.MerchantRegistration.GetByPrincipal(context.Background(), user)
	if stored == nil || stored.Status != data.MerchantRegistrationStatusDraft {
		t.Fatal("draft saved before completion must remain")
	}

	drop()
	completion, err := f.complete(user, stored.Revision, fullRegistration(brand))
	if err != nil || !completion.NewlyCompleted {
		t.Fatalf("retry after a recoverable failure must succeed: %v", err)
	}
}

// 5, 12: principal isolation.
func TestMerchantRegistration_PrincipalsAreIsolated(t *testing.T) {
	f := newRegistrationFixture(t)
	alice := f.user("consumer", true)
	bob := f.user("consumer", true)

	if _, err := f.save(alice, 0, data.MerchantRegistrationFields{TaxID: regStr("ALICE-0001")}); err != nil {
		t.Fatal(err)
	}

	bobView, err := f.service.GetOwnMerchantRegistrationInternal(context.Background(), bob)
	if err != nil || bobView.Registration != nil {
		t.Fatalf("another principal's draft must be invisible: %+v %v", bobView, err)
	}

	// Bob's revision-1 save cannot reach Alice's revision-1 row.
	if _, err := f.save(bob, 1, data.MerchantRegistrationFields{TaxID: regStr("BOB-0001")}); !errors.Is(err, ErrMerchantRegistrationEditConflict) {
		t.Fatalf("a revision for a row the caller does not own must conflict, got %v", err)
	}
	alices, _ := f.service.Models.MerchantRegistration.GetByPrincipal(context.Background(), alice)
	if *alices.Fields.TaxID != "ALICE-0001" || alices.Revision != 1 {
		t.Fatalf("Alice's draft changed: %+v", alices)
	}
}

// 5: only active public-role account holders may register.
func TestMerchantRegistration_IneligibleAccountsCannotRegister(t *testing.T) {
	f := newRegistrationFixture(t)
	staff := f.user("viewer", true)
	inactive := f.user("consumer", false)

	for name, user := range map[string]uuid.UUID{"staff": staff, "inactive": inactive} {
		if _, err := f.save(user, 0, data.MerchantRegistrationFields{}); !errors.Is(err, ErrMerchantOnboardingActorIneligible) {
			t.Fatalf("%s save: expected ineligible, got %v", name, err)
		}
		if _, err := f.complete(user, 0, fullRegistration(uniqueBrand("Nope"))); !errors.Is(err, ErrMerchantOnboardingActorIneligible) {
			t.Fatalf("%s complete: expected ineligible, got %v", name, err)
		}
		view, err := f.service.GetOwnMerchantRegistrationInternal(context.Background(), user)
		if name == "inactive" {
            if !errors.Is(err, ErrMerchantOnboardingActorIneligible) { t.Fatalf("inactive read must be denied: %v",err) }
        } else if err != nil || view.Eligible {
            t.Fatalf("%s must be reported ineligible: %v",name,err)
        }
		_, eligible, err := f.service.GetOwnMerchantNavigationInternal(context.Background(), user)
		if name == "inactive" {
            if !errors.Is(err,ErrMerchantOnboardingActorIneligible) {t.Fatalf("inactive navigation must be denied: %v",err)}
        } else if err != nil || eligible {t.Fatalf("%s navigation must not offer registration",name)}
	}
}

// 11: completed business-information management.
func TestMerchantRegistration_CompletedBusinessInformationManagement(t *testing.T) {
	f := newRegistrationFixture(t)
	user := f.user("consumer", true)
	fields := fullRegistration(uniqueBrand("Before"))

	completion, err := f.complete(user, 0, fields)
	if err != nil {
		t.Fatal(err)
	}
	revision := completion.View.Registration.Revision

	missing := fields
	missing.ContactName = nil
	if _, err := f.save(user, revision, missing); fieldCodes(t, err)["contact_name"] != "required" {
		t.Fatal("completed business information must keep every required fact")
	}

	renamed := fields
	renamed.DBAName = regStr(uniqueBrand("After"))
	renamed.Website = regStr("https://new.acme.example/")
	view, err := f.save(user, revision, renamed)
	if err != nil {
		t.Fatalf("business information update: %v", err)
	}
	if view.Merchant.Name != *renamed.DBAName {
		t.Fatalf("Merchant name must follow the registered DBA, got %q", view.Merchant.Name)
	}
	if view.Merchant.Website != nil {
		t.Fatal("the private website must never be copied to the Merchant")
	}
	if *view.Registration.Fields.Website != "https://new.acme.example/" {
		t.Fatal("private website not updated in the registration")
	}
	contexts, _ := f.service.Models.MerchantAccount.ListActiveOperatingContextsForPrincipal(context.Background(), user)
	if len(contexts) != 1 || contexts[0].MerchantName != *renamed.DBAName {
		t.Fatal("active context must present the updated DBA")
	}
}

// A principal onboarded before registration existed is linked, not
// duplicated.
func TestMerchantRegistration_LinksPreExistingPrincipalAccount(t *testing.T) {
	f := newRegistrationFixture(t)
	user := f.user("merchant", true)
	ctx := context.Background()

	tx, err := f.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		t.Fatal(err)
	}
	legacy := &data.Merchant{Name: uniqueBrand("Legacy")}
	if err := f.service.Models.Merchant.InsertTx(ctx, tx, legacy); err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.Models.MerchantAccount.CreateForPrincipalTx(ctx, tx, legacy.ID, user); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}

	brand := uniqueBrand("Registered")
	completion, err := f.complete(user, 0, fullRegistration(brand))
	if err != nil {
		t.Fatalf("legacy completion: %v", err)
	}
	if !completion.NewlyCompleted || completion.MerchantCreated {
		t.Fatalf("legacy principal must be linked, not recreated: %+v", completion)
	}
	if *completion.View.Registration.MerchantID != legacy.ID || completion.View.Merchant.Name != brand {
		t.Fatal("registration must reference the existing Merchant and align its name")
	}
	if accounts, _ := f.merchantCounts(user); accounts != 1 {
		t.Fatalf("legacy linking duplicated the account: %d", accounts)
	}
}

// The database independently refuses invalid facts and incoherent lifecycle
// rows, even when written outside the service.
func TestMerchantRegistration_DatabaseConstraintsAreAuthoritative(t *testing.T) {
	f := newRegistrationFixture(t)
	user := f.user("consumer", true)
	ctx := context.Background()

	for name, sql := range map[string]string{
		"phone":     `INSERT INTO merchant_registrations (principal_user_id, phone) VALUES ($1, '5550100')`,
		"country":   `INSERT INTO merchant_registrations (principal_user_id, address_country) VALUES ($1, 'us')`,
		"email":     `INSERT INTO merchant_registrations (principal_user_id, corporate_email) VALUES ($1, 'Ops@Acme.Example')`,
		"website":   `INSERT INTO merchant_registrations (principal_user_id, website) VALUES ($1, 'javascript:alert(1)')`,
		"tax":       `INSERT INTO merchant_registrations (principal_user_id, tax_id) VALUES ($1, '12#45')`,
		"completed": `INSERT INTO merchant_registrations (principal_user_id, registration_status, completed_at) VALUES ($1, 'completed', NOW())`,
	} {
		_, err := f.pool.Exec(ctx, sql, user)
		if !data.IsCheckViolation(err) {
			t.Errorf("%s: expected a CHECK violation, got %v", name, err)
		}
	}
}

func TestMerchantRegistration_ContextTimeoutIsReported(t *testing.T) {
	f := newRegistrationFixture(t)
	user := f.user("consumer", true)

	ctx, cancel := context.WithTimeout(context.Background(), time.Nanosecond)
	defer cancel()
	time.Sleep(time.Millisecond)

	_, err := f.service.SaveOwnMerchantRegistrationInternal(ctx, user,
		MerchantRegistrationInput{Fields: data.MerchantRegistrationFields{}})
	if err == nil {
		t.Fatal("an expired context must fail")
	}
}
