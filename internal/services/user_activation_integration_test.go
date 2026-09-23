// Package services contains PostgreSQL integration regression tests for the
// canonical user-activation service workflows.
//
// focodebase/fobackend/internal/services/user_activation_integration_test.go
package services

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/PiccoloMondoC/focodebase/fobackend/internal/data"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func TestActivationIssuance_Integration(t *testing.T) {
	svc, _, pool := setupIntegration(t)
	ctx := context.Background()

	t.Run(
		"eligible user receives and can reissue credential",
		func(t *testing.T) {
			userID := createTestUser(
				t,
				ctx,
				pool,
				false,
			)

			token1, err :=
				svc.IssueUserActivationTokenInternal(
					ctx,
					userID,
				)
			if err != nil {
				t.Fatalf("issue token: %v", err)
			}
			if token1 == "" {
				t.Fatal("issue returned empty token")
			}

			if got := countActivationTokens(
				t,
				ctx,
				pool,
				userID,
			); got != 1 {
				t.Fatalf(
					"activation token count = %d, want 1",
					got,
				)
			}

			token2, err :=
				svc.IssueUserActivationTokenInternal(
					ctx,
					userID,
				)
			if err != nil {
				t.Fatalf("reissue token: %v", err)
			}

			if token2 == "" {
				t.Fatal("reissue returned empty token")
			}
			if token2 == token1 {
				t.Fatal(
					"reissue returned identical plaintext token",
				)
			}

			if got := countActivationTokens(
				t,
				ctx,
				pool,
				userID,
			); got != 1 {
				t.Fatalf(
					"reissue token count = %d, want 1",
					got,
				)
			}

			if _, err :=
				svc.RedeemUserActivationTokenInternal(
					ctx,
					token1,
				); !errors.Is(
				err,
				data.ErrActivationTokenInvalid,
			) {
				t.Fatalf(
					"superseded token error = %v, want ErrActivationTokenInvalid",
					err,
				)
			}

			activatedID, err :=
				svc.RedeemUserActivationTokenInternal(
					ctx,
					token2,
				)
			if err != nil {
				t.Fatalf(
					"redeem current token: %v",
					err,
				)
			}

			if activatedID != userID {
				t.Fatalf(
					"activated user = %s, want %s",
					activatedID,
					userID,
				)
			}
		},
	)

	t.Run(
		"already active user receives no credential",
		func(t *testing.T) {
			userID := createTestUser(
				t,
				ctx,
				pool,
				true,
			)

			if _, err :=
				svc.IssueUserActivationTokenInternal(
					ctx,
					userID,
				); !errors.Is(
				err,
				data.ErrUserAlreadyActive,
			) {
				t.Fatalf(
					"error = %v, want ErrUserAlreadyActive",
					err,
				)
			}

			if got := countActivationTokens(
				t,
				ctx,
				pool,
				userID,
			); got != 0 {
				t.Fatalf(
					"ineligible issuance persisted %d tokens, want 0",
					got,
				)
			}
		},
	)
}

// TestActivationIssuanceLocksUserFirst_Integration directly proves the
// canonical users -> activation_tokens order.
//
// A separate transaction holds the target users row FOR UPDATE. Issuance must
// therefore block before it can create/reissue activation-token state.
func TestActivationIssuanceLocksUserFirst_Integration(
	t *testing.T,
) {
	svc, _, pool := setupIntegration(t)
	ctx := context.Background()

	userID := createTestUser(
		t,
		ctx,
		pool,
		false,
	)

	blockerTx, err := pool.BeginTx(
		ctx,
		pgx.TxOptions{},
	)
	if err != nil {
		t.Fatalf("begin blocker transaction: %v", err)
	}
	defer func() {
		_ = blockerTx.Rollback(context.Background())
	}()

	if _, err := blockerTx.Exec(
		ctx,
		`
			SELECT id
			FROM users
			WHERE id = $1
			FOR UPDATE
		`,
		userID,
	); err != nil {
		t.Fatalf("lock user row: %v", err)
	}

	type result struct {
		token string
		err   error
	}

	resultCh := make(chan result, 1)

	go func() {
		token, issueErr :=
			svc.IssueUserActivationTokenInternal(
				context.Background(),
				userID,
			)

		resultCh <- result{
			token: token,
			err:   issueErr,
		}
	}()

	// While the canonical user lock is held, issuance must not have touched
	// activation_tokens. Give the service a bounded opportunity to reach the
	// blocking lock; completion here would itself be a failure.
	select {
	case result := <-resultCh:
		t.Fatalf(
			"issuance completed while users row remained locked: token=%q error=%v",
			result.token,
			result.err,
		)

	case <-time.After(250 * time.Millisecond):
	}

	if got := countActivationTokens(
		t,
		ctx,
		pool,
		userID,
	); got != 0 {
		t.Fatalf(
			"activation_tokens changed before users lock released: count=%d",
			got,
		)
	}

	if err := blockerTx.Commit(ctx); err != nil {
		t.Fatalf(
			"commit blocker transaction: %v",
			err,
		)
	}

	select {
	case result := <-resultCh:
		if result.err != nil {
			t.Fatalf(
				"issuance after releasing users lock: %v",
				result.err,
			)
		}

		if result.token == "" {
			t.Fatal(
				"issuance returned empty token after users lock release",
			)
		}

	case <-time.After(5 * time.Second):
		t.Fatal(
			"issuance did not complete after users lock release",
		)
	}

	if got := countActivationTokens(
		t,
		ctx,
		pool,
		userID,
	); got != 1 {
		t.Fatalf(
			"activation token count after issuance = %d, want 1",
			got,
		)
	}
}

func TestActivationRedemption_Integration(t *testing.T) {
	svc, _, pool := setupIntegration(t)
	ctx := context.Background()

	userID := createTestUser(
		t,
		ctx,
		pool,
		false,
	)

	token, err :=
		svc.IssueUserActivationTokenInternal(
			ctx,
			userID,
		)
	if err != nil {
		t.Fatalf("issue token: %v", err)
	}

	activatedID, err :=
		svc.RedeemUserActivationTokenInternal(
			ctx,
			token,
		)
	if err != nil {
		t.Fatalf("redeem token: %v", err)
	}

	if activatedID != userID {
		t.Fatalf(
			"activated user = %s, want %s",
			activatedID,
			userID,
		)
	}

	state := fetchUserRowState(
		t,
		ctx,
		pool,
		userID,
	)
	if !state.IsActive {
		t.Fatal("user was not activated")
	}
	if state.DeletedAt != nil {
		t.Fatal("activated user unexpectedly deleted")
	}

	if got := countActivationTokens(
		t,
		ctx,
		pool,
		userID,
	); got != 0 {
		t.Fatalf(
			"consumed token count = %d, want 0",
			got,
		)
	}

	if _, err :=
		svc.RedeemUserActivationTokenInternal(
			ctx,
			token,
		); !errors.Is(
		err,
		data.ErrActivationTokenInvalid,
	) {
		t.Fatalf(
			"second redemption error = %v, want ErrActivationTokenInvalid",
			err,
		)
	}
}

func TestActivationInvalidAndExpiredCredentials_Integration(
	t *testing.T,
) {
	svc, _, pool := setupIntegration(t)
	ctx := context.Background()

	t.Run("empty", func(t *testing.T) {
		if _, err :=
			svc.RedeemUserActivationTokenInternal(
				ctx,
				"",
			); !errors.Is(
			err,
			data.ErrActivationTokenRequired,
		) {
			t.Fatalf(
				"error = %v, want ErrActivationTokenRequired",
				err,
			)
		}
	})

	t.Run("invalid", func(t *testing.T) {
		if _, err :=
			svc.RedeemUserActivationTokenInternal(
				ctx,
				"not-a-real-activation-token",
			); !errors.Is(
			err,
			data.ErrActivationTokenInvalid,
		) {
			t.Fatalf(
				"error = %v, want ErrActivationTokenInvalid",
				err,
			)
		}
	})

	t.Run("expired", func(t *testing.T) {
		userID := createTestUser(
			t,
			ctx,
			pool,
			false,
		)

		token, err :=
			svc.IssueUserActivationTokenInternal(
				ctx,
				userID,
			)
		if err != nil {
			t.Fatalf("issue token: %v", err)
		}

		expireActivationToken(
			t,
			ctx,
			pool,
			userID,
		)

		if _, err :=
			svc.RedeemUserActivationTokenInternal(
				ctx,
				token,
			); !errors.Is(
			err,
			data.ErrActivationTokenExpired,
		) {
			t.Fatalf(
				"error = %v, want ErrActivationTokenExpired",
				err,
			)
		}

		state := fetchUserRowState(
			t,
			ctx,
			pool,
			userID,
		)
		if state.IsActive {
			t.Fatal(
				"expired credential activated user",
			)
		}

		if got := countActivationTokens(
			t,
			ctx,
			pool,
			userID,
		); got != 1 {
			t.Fatalf(
				"expired credential row count = %d, want 1",
				got,
			)
		}
	})
}

func TestActivationIssuanceReissueConcurrency_Integration(
	t *testing.T,
) {
	svc, _, pool := setupIntegration(t)
	ctx := context.Background()

	userID := createTestUser(
		t,
		ctx,
		pool,
		false,
	)

	const n = 8

	tokens := make([]string, n)
	errs := make([]error, n)

	runConcurrentlyBounded(
		t,
		15*time.Second,
		n,
		func(i int) {
			tokens[i], errs[i] =
				svc.IssueUserActivationTokenInternal(
					ctx,
					userID,
				)
		},
	)

	seen := make(map[string]struct{}, n)

	for i := range errs {
		if errs[i] != nil {
			t.Fatalf(
				"issuance[%d]: %v",
				i,
				errs[i],
			)
		}

		if tokens[i] == "" {
			t.Fatalf(
				"issuance[%d] returned empty token",
				i,
			)
		}

		if _, exists := seen[tokens[i]]; exists {
			t.Fatalf(
				"issuance[%d] returned duplicate plaintext token",
				i,
			)
		}

		seen[tokens[i]] = struct{}{}
	}

	if got := countActivationTokens(
		t,
		ctx,
		pool,
		userID,
	); got != 1 {
		t.Fatalf(
			"activation token count = %d, want 1",
			got,
		)
	}

	successes := 0

	for i, token := range tokens {
		_, err :=
			svc.RedeemUserActivationTokenInternal(
				ctx,
				token,
			)

		switch {
		case err == nil:
			successes++

		case errors.Is(
			err,
			data.ErrActivationTokenInvalid,
		):

		default:
			t.Fatalf(
				"redeem issuance[%d]: unexpected error %v",
				i,
				err,
			)
		}
	}

	if successes != 1 {
		t.Fatalf(
			"successful concurrently-issued credentials = %d, want 1",
			successes,
		)
	}
}

func TestActivationRedemptionConcurrency_Integration(
	t *testing.T,
) {
	svc, _, pool := setupIntegration(t)
	ctx := context.Background()

	userID := createTestUser(
		t,
		ctx,
		pool,
		false,
	)

	token, err :=
		svc.IssueUserActivationTokenInternal(
			ctx,
			userID,
		)
	if err != nil {
		t.Fatalf("issue token: %v", err)
	}

	const n = 8

	results := make([]uuid.UUID, n)
	errs := make([]error, n)

	runConcurrentlyBounded(
		t,
		15*time.Second,
		n,
		func(i int) {
			results[i], errs[i] =
				svc.RedeemUserActivationTokenInternal(
					ctx,
					token,
				)
		},
	)

	successes := 0

	for i := range errs {
		switch {
		case errs[i] == nil:
			successes++

			if results[i] != userID {
				t.Fatalf(
					"redemption[%d] activated %s, want %s",
					i,
					results[i],
					userID,
				)
			}

		case errors.Is(
			errs[i],
			data.ErrUserAlreadyActive,
		):

		case errors.Is(
			errs[i],
			data.ErrActivationTokenInvalid,
		):

		default:
			t.Fatalf(
				"redemption[%d]: unexpected error %v",
				i,
				errs[i],
			)
		}
	}

	if successes != 1 {
		t.Fatalf(
			"successful redemptions = %d, want 1",
			successes,
		)
	}

	state := fetchUserRowState(
		t,
		ctx,
		pool,
		userID,
	)
	if !state.IsActive {
		t.Fatal("user was not activated")
	}

	if got := countActivationTokens(
		t,
		ctx,
		pool,
		userID,
	); got != 0 {
		t.Fatalf(
			"remaining activation tokens = %d, want 0",
			got,
		)
	}
}

func TestActivationRedemptionVsAccountClosure_Integration(
	t *testing.T,
) {
	svc, _, pool := setupIntegration(t)

	const iterations = 10

	for i := 0; i < iterations; i++ {
		t.Run(
			fmt.Sprintf("iteration_%d", i),
			func(t *testing.T) {
				ctx := context.Background()

				userID := createTestUser(
					t,
					ctx,
					pool,
					false,
				)

				token, err :=
					svc.IssueUserActivationTokenInternal(
						ctx,
						userID,
					)
				if err != nil {
					t.Fatalf(
						"issue token: %v",
						err,
					)
				}

				var redeemErr error
				var closeErr error

				runConcurrentlyBounded(
					t,
					10*time.Second,
					2,
					func(index int) {
						switch index {
						case 0:
							_, redeemErr =
								svc.RedeemUserActivationTokenInternal(
									ctx,
									token,
								)

						case 1:
							closeErr =
								svc.closeAccountTx(
									ctx,
									userID,
								)
						}
					},
				)

				if closeErr != nil {
					t.Fatalf(
						"close account: %v",
						closeErr,
					)
				}

				switch {
				case redeemErr == nil:

				case errors.Is(
					redeemErr,
					data.ErrUserNotFound,
				):

				case errors.Is(
					redeemErr,
					data.ErrActivationTokenInvalid,
				):

				default:
					t.Fatalf(
						"redemption/closure race: unexpected redemption error %v",
						redeemErr,
					)
				}

				state := fetchUserRowState(
					t,
					ctx,
					pool,
					userID,
				)

				if state.DeletedAt == nil {
					t.Fatal(
						"user was not closed",
					)
				}
				if state.IsActive {
					t.Fatal(
						"closed user remained active",
					)
				}

				if got := countActivationTokens(
					t,
					ctx,
					pool,
					userID,
				); got != 0 {
					t.Fatalf(
						"activation tokens after closure = %d, want 0",
						got,
					)
				}
			},
		)
	}
}

func TestActivationIssuanceVsAccountClosure_Integration(
	t *testing.T,
) {
	svc, _, pool := setupIntegration(t)

	const iterations = 10

	for i := 0; i < iterations; i++ {
		t.Run(
			fmt.Sprintf("iteration_%d", i),
			func(t *testing.T) {
				ctx := context.Background()

				userID := createTestUser(
					t,
					ctx,
					pool,
					false,
				)

				var issueErr error
				var closeErr error

				runConcurrentlyBounded(
					t,
					10*time.Second,
					2,
					func(index int) {
						switch index {
						case 0:
							_, issueErr =
								svc.IssueUserActivationTokenInternal(
									ctx,
									userID,
								)

						case 1:
							closeErr =
								svc.closeAccountTx(
									ctx,
									userID,
								)
						}
					},
				)

				if closeErr != nil {
					t.Fatalf(
						"close account: %v",
						closeErr,
					)
				}

				switch {
				case issueErr == nil:

				case errors.Is(
					issueErr,
					data.ErrUserNotFound,
				):

				default:
					t.Fatalf(
						"issuance/closure race: unexpected issuance error %v",
						issueErr,
					)
				}

				state := fetchUserRowState(
					t,
					ctx,
					pool,
					userID,
				)

				if state.DeletedAt == nil {
					t.Fatal(
						"user was not closed",
					)
				}

				if got := countActivationTokens(
					t,
					ctx,
					pool,
					userID,
				); got != 0 {
					t.Fatalf(
						"activation tokens after closure = %d, want 0",
						got,
					)
				}
			},
		)
	}
}

func TestActivationOwnerDiscoveryRevalidation_Integration(
	t *testing.T,
) {
	svc, models, pool := setupIntegration(t)
	ctx := context.Background()

	userID := createTestUser(
		t,
		ctx,
		pool,
		false,
	)

	tokenA, err :=
		svc.IssueUserActivationTokenInternal(
			ctx,
			userID,
		)
	if err != nil {
		t.Fatalf(
			"issue initial token: %v",
			err,
		)
	}

	tx, err := models.DB.BeginTx(
		ctx,
		pgx.TxOptions{},
	)
	if err != nil {
		t.Fatalf(
			"begin redemption transaction: %v",
			err,
		)
	}

	defer func() {
		_ = tx.Rollback(context.Background())
	}()

	candidateUserID, err :=
		models.
			ActivationToken.
			LookupActivationTokenOwnerTx(
				ctx,
				tx,
				tokenA,
			)
	if err != nil {
		t.Fatalf(
			"lookup activation owner: %v",
			err,
		)
	}

	if candidateUserID != userID {
		t.Fatalf(
			"candidate user = %s, want %s",
			candidateUserID,
			userID,
		)
	}

	// No row lock was acquired by owner discovery, so an independently
	// committed reissue must remain possible here.
	tokenB, err :=
		svc.IssueUserActivationTokenInternal(
			ctx,
			userID,
		)
	if err != nil {
		t.Fatalf(
			"reissue token: %v",
			err,
		)
	}

	if tokenB == tokenA {
		t.Fatal(
			"reissue produced identical plaintext token",
		)
	}

	if err :=
		models.User.LockActivationEligibleUserTx(
			ctx,
			tx,
			candidateUserID,
		); err != nil {
		t.Fatalf(
			"lock candidate user: %v",
			err,
		)
	}

	if _, _, err :=
		models.
			ActivationToken.
			LockActivationTokenTx(
				ctx,
				tx,
				tokenA,
			); !errors.Is(
		err,
		data.ErrActivationTokenInvalid,
	) {
		t.Fatalf(
			"authoritative stale-token validation error = %v, want ErrActivationTokenInvalid",
			err,
		)
	}

	if err := tx.Rollback(ctx); err != nil &&
		!errors.Is(
			err,
			pgx.ErrTxClosed,
		) {
		t.Fatalf(
			"rollback redemption transaction: %v",
			err,
		)
	}

	if _, err :=
		svc.RedeemUserActivationTokenInternal(
			ctx,
			tokenA,
		); !errors.Is(
		err,
		data.ErrActivationTokenInvalid,
	) {
		t.Fatalf(
			"service stale-token error = %v, want ErrActivationTokenInvalid",
			err,
		)
	}

	activatedID, err :=
		svc.RedeemUserActivationTokenInternal(
			ctx,
			tokenB,
		)
	if err != nil {
		t.Fatalf(
			"redeem current token: %v",
			err,
		)
	}

	if activatedID != userID {
		t.Fatalf(
			"activated user = %s, want %s",
			activatedID,
			userID,
		)
	}
}
