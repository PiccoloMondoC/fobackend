// focodebase/fobackend/internal/services/email_confirmation_integration_test.go
//
// PostgreSQL integration tests for the email-confirmation credential pair:
// link token plus independent manual six-digit code.
//
// These reuse the package's existing integration harness (setupIntegration,
// createTestUser, countActivationTokens, fetchUserRowState,
// runConcurrentlyBounded) exactly as user_activation_integration_test.go
// does.
package services

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/PiccoloMondoC/focodebase/fobackend/internal/data"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func userEmail(t *testing.T, ctx context.Context, pool *pgxpool.Pool, userID uuid.UUID) string {
	t.Helper()

	var email string
	if err := pool.QueryRow(ctx, `SELECT email FROM users WHERE id = $1`, userID).Scan(&email); err != nil {
		t.Fatalf("read user email: %v", err)
	}
	return email
}

func expireActivationCode(t *testing.T, ctx context.Context, pool *pgxpool.Pool, userID uuid.UUID) {
	t.Helper()

	if _, err := pool.Exec(ctx, `
		UPDATE activation_tokens
		SET code_expires_at = NOW() - INTERVAL '1 second'
		WHERE user_id = $1
	`, userID); err != nil {
		t.Fatalf("expire activation code: %v", err)
	}
}

func backdateActivationResend(t *testing.T, ctx context.Context, pool *pgxpool.Pool, userID uuid.UUID) {
	t.Helper()

	if _, err := pool.Exec(ctx, `
		UPDATE activation_tokens
		SET last_resent_at = NOW() - INTERVAL '1 day'
		WHERE user_id = $1
	`, userID); err != nil {
		t.Fatalf("backdate activation resend: %v", err)
	}
}

func codeFailureCounters(t *testing.T, ctx context.Context, pool *pgxpool.Pool, userID uuid.UUID) (attempts, total int, hasCode bool) {
	t.Helper()

	if err := pool.QueryRow(ctx, `
		SELECT code_failed_attempts, code_failed_total, code_hash IS NOT NULL
		FROM activation_tokens
		WHERE user_id = $1
	`, userID).Scan(&attempts, &total, &hasCode); err != nil {
		t.Fatalf("read code counters: %v", err)
	}
	return attempts, total, hasCode
}

// wrongCode returns a well-formed code guaranteed to differ from code.
func wrongCode(code string) string {
	if code == "000000" {
		return "111111"
	}
	return "000000"
}

func issueConfirmation(t *testing.T, ctx context.Context, svc *Service, userID uuid.UUID) data.ActivationCredentials {
	t.Helper()

	credentials, err := svc.IssueUserEmailConfirmationInternal(ctx, userID, IssueEmailConfirmationOptions{})
	if err != nil {
		t.Fatalf("issue confirmation: %v", err)
	}
	if credentials.LinkToken == "" || len(credentials.Code) != 6 {
		t.Fatalf("issued credentials are malformed: token=%t code-length=%d", credentials.LinkToken != "", len(credentials.Code))
	}
	if credentials.LinkToken == credentials.Code {
		t.Fatal("link token and code are the same credential")
	}
	return credentials
}

func TestEmailConfirmationCodeRedemption_Integration(t *testing.T) {
	svc, _, pool := setupIntegration(t)
	ctx := context.Background()

	userID := createTestUser(t, ctx, pool, false)
	email := userEmail(t, ctx, pool, userID)
	credentials := issueConfirmation(t, ctx, svc, userID)

	activatedID, err := svc.RedeemUserEmailConfirmationCodeInternal(ctx, email, credentials.Code[:3]+" "+credentials.Code[3:])
	if err != nil {
		t.Fatalf("redeem code: %v", err)
	}
	if activatedID != userID {
		t.Fatalf("activated %s, want %s", activatedID, userID)
	}

	if state := fetchUserRowState(t, ctx, pool, userID); !state.IsActive {
		t.Fatal("code redemption did not verify the account")
	}

	if got := countActivationTokens(t, ctx, pool, userID); got != 0 {
		t.Fatalf("pending records after code redemption = %d, want 0", got)
	}

	// One-time: the same code cannot be used again.
	if _, err := svc.RedeemUserEmailConfirmationCodeInternal(ctx, email, credentials.Code); !errors.Is(err, data.ErrUserAlreadyActive) {
		t.Fatalf("second code use error = %v, want ErrUserAlreadyActive", err)
	}

	// Successful code confirmation invalidates the link token too.
	if _, err := svc.RedeemUserActivationTokenInternal(ctx, credentials.LinkToken); !errors.Is(err, data.ErrActivationTokenInvalid) {
		t.Fatalf("link after code confirmation error = %v, want ErrActivationTokenInvalid", err)
	}
}

func TestEmailConfirmationLinkInvalidatesCode_Integration(t *testing.T) {
	svc, _, pool := setupIntegration(t)
	ctx := context.Background()

	userID := createTestUser(t, ctx, pool, false)
	email := userEmail(t, ctx, pool, userID)
	credentials := issueConfirmation(t, ctx, svc, userID)

	if _, err := svc.RedeemUserActivationTokenInternal(ctx, credentials.LinkToken); err != nil {
		t.Fatalf("redeem link: %v", err)
	}

	if _, err := svc.RedeemUserEmailConfirmationCodeInternal(ctx, email, credentials.Code); !errors.Is(err, data.ErrUserAlreadyActive) {
		t.Fatalf("code after link confirmation error = %v, want ErrUserAlreadyActive", err)
	}
}

func TestEmailConfirmationReissueSupersedesBothCredentials_Integration(t *testing.T) {
	svc, _, pool := setupIntegration(t)
	ctx := context.Background()

	userID := createTestUser(t, ctx, pool, false)
	email := userEmail(t, ctx, pool, userID)

	first := issueConfirmation(t, ctx, svc, userID)
	second := issueConfirmation(t, ctx, svc, userID)

	if got := countActivationTokens(t, ctx, pool, userID); got != 1 {
		t.Fatalf("pending records after reissue = %d, want 1", got)
	}

	if first.Code != second.Code {
		if _, err := svc.RedeemUserEmailConfirmationCodeInternal(ctx, email, first.Code); !errors.Is(err, data.ErrActivationCodeInvalid) {
			t.Fatalf("superseded code error = %v, want ErrActivationCodeInvalid", err)
		}
	}

	if _, err := svc.RedeemUserActivationTokenInternal(ctx, first.LinkToken); !errors.Is(err, data.ErrActivationTokenInvalid) {
		t.Fatalf("superseded link error = %v, want ErrActivationTokenInvalid", err)
	}

	if _, err := svc.RedeemUserEmailConfirmationCodeInternal(ctx, email, second.Code); err != nil {
		t.Fatalf("newest code: %v", err)
	}
}

func TestEmailConfirmationInvalidAndExpiredCode_Integration(t *testing.T) {
	svc, _, pool := setupIntegration(t)
	ctx := context.Background()

	t.Run("wrong code is counted durably", func(t *testing.T) {
		userID := createTestUser(t, ctx, pool, false)
		email := userEmail(t, ctx, pool, userID)
		credentials := issueConfirmation(t, ctx, svc, userID)

		if _, err := svc.RedeemUserEmailConfirmationCodeInternal(ctx, email, wrongCode(credentials.Code)); !errors.Is(err, data.ErrActivationCodeInvalid) {
			t.Fatalf("error = %v, want ErrActivationCodeInvalid", err)
		}

		attempts, total, hasCode := codeFailureCounters(t, ctx, pool, userID)
		if attempts != 1 || total != 1 || !hasCode {
			t.Fatalf("counters = (%d, %d, %t), want (1, 1, true)", attempts, total, hasCode)
		}

		if state := fetchUserRowState(t, ctx, pool, userID); state.IsActive {
			t.Fatal("wrong code verified the account")
		}
	})

	t.Run("expired code", func(t *testing.T) {
		userID := createTestUser(t, ctx, pool, false)
		email := userEmail(t, ctx, pool, userID)
		credentials := issueConfirmation(t, ctx, svc, userID)

		expireActivationCode(t, ctx, pool, userID)

		if _, err := svc.RedeemUserEmailConfirmationCodeInternal(ctx, email, credentials.Code); !errors.Is(err, data.ErrActivationCodeExpired) {
			t.Fatalf("error = %v, want ErrActivationCodeExpired", err)
		}
		if state := fetchUserRowState(t, ctx, pool, userID); state.IsActive {
			t.Fatal("expired code verified the account")
		}

		// The link token is a separate credential and still works.
		if _, err := svc.RedeemUserActivationTokenInternal(ctx, credentials.LinkToken); err != nil {
			t.Fatalf("link after code expiry: %v", err)
		}
	})

	t.Run("unknown email", func(t *testing.T) {
		if _, err := svc.RedeemUserEmailConfirmationCodeInternal(ctx, fmt.Sprintf("nobody-%s@example.com", uuid.NewString()), "123456"); !errors.Is(err, data.ErrUserNotFound) {
			t.Fatalf("error = %v, want ErrUserNotFound", err)
		}
	})

	t.Run("malformed code", func(t *testing.T) {
		userID := createTestUser(t, ctx, pool, false)
		email := userEmail(t, ctx, pool, userID)
		issueConfirmation(t, ctx, svc, userID)

		if _, err := svc.RedeemUserEmailConfirmationCodeInternal(ctx, email, "12ab56"); !errors.Is(err, data.ErrActivationCodeInvalid) {
			t.Fatalf("error = %v, want ErrActivationCodeInvalid", err)
		}
		if attempts, _, _ := codeFailureCounters(t, ctx, pool, userID); attempts != 0 {
			t.Fatalf("malformed input consumed %d attempts, want 0", attempts)
		}
	})
}

func TestEmailConfirmationCodeAttemptLimits_Integration(t *testing.T) {
	svc, _, pool := setupIntegration(t)
	ctx := context.Background()

	maxAttempts := svc.Cfg.EffectiveActivationCodeMaxAttempts()
	maxTotal := svc.Cfg.EffectiveActivationCodeMaxTotalFailures()

	t.Run("per-code limit discards the code even for the right answer", func(t *testing.T) {
		userID := createTestUser(t, ctx, pool, false)
		email := userEmail(t, ctx, pool, userID)
		credentials := issueConfirmation(t, ctx, svc, userID)

		var lastErr error
		for i := 0; i < maxAttempts; i++ {
			_, lastErr = svc.RedeemUserEmailConfirmationCodeInternal(ctx, email, wrongCode(credentials.Code))
		}
		if !errors.Is(lastErr, data.ErrActivationCodeAttemptsExceeded) {
			t.Fatalf("final wrong attempt error = %v, want ErrActivationCodeAttemptsExceeded", lastErr)
		}

		if _, _, hasCode := codeFailureCounters(t, ctx, pool, userID); hasCode {
			t.Fatal("code survived exhausting its attempts")
		}

		if _, err := svc.RedeemUserEmailConfirmationCodeInternal(ctx, email, credentials.Code); !errors.Is(err, data.ErrActivationCodeInvalid) {
			t.Fatalf("correct code after exhaustion error = %v, want ErrActivationCodeInvalid", err)
		}

		// The link is a separate credential and remains usable.
		if _, err := svc.RedeemUserActivationTokenInternal(ctx, credentials.LinkToken); err != nil {
			t.Fatalf("link after code exhaustion: %v", err)
		}
	})

	t.Run("reissue resets the per-code budget but not the per-account total", func(t *testing.T) {
		userID := createTestUser(t, ctx, pool, false)
		email := userEmail(t, ctx, pool, userID)

		failures := 0
		for failures < maxTotal {
			credentials := issueConfirmation(t, ctx, svc, userID)
			for i := 0; i < maxAttempts && failures < maxTotal; i++ {
				_, _ = svc.RedeemUserEmailConfirmationCodeInternal(ctx, email, wrongCode(credentials.Code))
				failures++
			}
		}

		_, total, _ := codeFailureCounters(t, ctx, pool, userID)
		if total != maxTotal {
			t.Fatalf("total failures = %d, want %d", total, maxTotal)
		}

		fresh := issueConfirmation(t, ctx, svc, userID)
		if _, err := svc.RedeemUserEmailConfirmationCodeInternal(ctx, email, fresh.Code); !errors.Is(err, data.ErrActivationCodeAttemptsExceeded) {
			t.Fatalf("fresh code after total exhaustion error = %v, want ErrActivationCodeAttemptsExceeded", err)
		}

		if _, err := svc.RedeemUserActivationTokenInternal(ctx, fresh.LinkToken); err != nil {
			t.Fatalf("link after total exhaustion: %v", err)
		}
	})

	t.Run("concurrent guesses cannot exceed the per-code limit", func(t *testing.T) {
		userID := createTestUser(t, ctx, pool, false)
		email := userEmail(t, ctx, pool, userID)
		credentials := issueConfirmation(t, ctx, svc, userID)

		const n = 12
		runConcurrentlyBounded(t, 15*time.Second, n, func(int) {
			_, _ = svc.RedeemUserEmailConfirmationCodeInternal(ctx, email, wrongCode(credentials.Code))
		})

		attempts, total, hasCode := codeFailureCounters(t, ctx, pool, userID)
		if attempts > maxAttempts || total > maxAttempts || hasCode {
			t.Fatalf("after %d concurrent guesses: counters = (%d, %d, code present %t)", n, attempts, total, hasCode)
		}
	})
}

func TestEmailConfirmationResendCooldown_Integration(t *testing.T) {
	svc, _, pool := setupIntegration(t)
	ctx := context.Background()

	userID := createTestUser(t, ctx, pool, false)
	issueConfirmation(t, ctx, svc, userID) // signup-style issuance

	cooldown := IssueEmailConfirmationOptions{EnforceResendCooldown: true}

	// Signup issuance does not start the cooldown: the first resend proceeds
	// (this is what rescues an undelivered signup email).
	resent, err := svc.IssueUserEmailConfirmationInternal(ctx, userID, cooldown)
	if err != nil {
		t.Fatalf("first resend after signup: %v", err)
	}

	if _, err := svc.IssueUserEmailConfirmationInternal(ctx, userID, cooldown); !errors.Is(err, ErrEmailConfirmationResendCooldown) {
		t.Fatalf("second resend inside cooldown error = %v, want ErrEmailConfirmationResendCooldown", err)
	}

	// The absorbed resend must not have superseded the outstanding link.
	if _, err := svc.RedeemUserActivationTokenInternal(ctx, resent.LinkToken); err != nil {
		t.Fatalf("outstanding link after absorbed resend: %v", err)
	}

	other := createTestUser(t, ctx, pool, false)
	if _, err := svc.IssueUserEmailConfirmationInternal(ctx, other, cooldown); err != nil {
		t.Fatalf("first resend: %v", err)
	}
	backdateActivationResend(t, ctx, pool, other)

	if _, err := svc.IssueUserEmailConfirmationInternal(ctx, other, cooldown); err != nil {
		t.Fatalf("resend after cooldown: %v", err)
	}

	// Already-verified accounts receive nothing, cooldown or not.
	active := createTestUser(t, ctx, pool, true)
	if _, err := svc.IssueUserEmailConfirmationInternal(ctx, active, cooldown); !errors.Is(err, data.ErrUserAlreadyActive) {
		t.Fatalf("verified account resend error = %v, want ErrUserAlreadyActive", err)
	}
}

func TestSignupIssuesConfirmationCredentialPair_Integration(t *testing.T) {
	svc, _, pool := setupIntegration(t)
	ctx := context.Background()

	result, err := svc.SignupUserInternal(ctx, SignupUserInput{
		Email:         fmt.Sprintf("signup-%s@example.com", uuid.NewString()),
		PlainPassword: "a long passphrase",
		RequestedRole: "consumer",
	})
	if err != nil {
		t.Fatalf("signup: %v", err)
	}

	if result.ActivationToken == "" || len(result.ActivationCode) != 6 {
		t.Fatal("signup did not issue both confirmation credentials")
	}

	if state := fetchUserRowState(t, ctx, pool, result.UserID); state.IsActive {
		t.Fatal("signup produced a verified account")
	}

	if got := countActivationTokens(t, ctx, pool, result.UserID); got != 1 {
		t.Fatalf("pending records after signup = %d, want 1", got)
	}
}

func TestEmailConfirmationTTLsAreDistinct_Integration(t *testing.T) {
	svc, _, pool := setupIntegration(t)
	ctx := context.Background()

	userID := createTestUser(t, ctx, pool, false)
	issueConfirmation(t, ctx, svc, userID)

	var linkSeconds, codeSeconds float64
	if err := pool.QueryRow(ctx, `
		SELECT
			EXTRACT(EPOCH FROM (expires_at - NOW())),
			EXTRACT(EPOCH FROM (code_expires_at - NOW()))
		FROM activation_tokens
		WHERE user_id = $1
	`, userID).Scan(&linkSeconds, &codeSeconds); err != nil {
		t.Fatalf("read expiries: %v", err)
	}

	within := func(got float64, want time.Duration) bool {
		const slack = 30.0 // seconds of test execution time
		return got <= want.Seconds() && got >= want.Seconds()-slack
	}

	linkTTL := svc.Cfg.ActivationTokenTTL
	codeTTL := svc.Cfg.EffectiveActivationCodeTTL()
	if codeTTL > linkTTL {
		codeTTL = linkTTL
	}

	if !within(linkSeconds, svc.Cfg.ActivationTokenTTL) {
		t.Fatalf("link expires in %.0fs, want the unchanged ActivationTokenTTL %s", linkSeconds, svc.Cfg.ActivationTokenTTL)
	}
	if !within(codeSeconds, codeTTL) {
		t.Fatalf("code expires in %.0fs, want the separate code TTL %s", codeSeconds, codeTTL)
	}
}
