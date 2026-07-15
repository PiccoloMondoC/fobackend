// Package services contains business logic for internal operations such as moderation,
// automation, audit logging, and data synchronization. It is used by async routines
// and internal system workflows, not exposed via public API routes.
//
// sdworkspace/sdbackend/internal/services/user_wallets_async.go
//
// GTM:
//   Layer: 2.3 Consumer Domain
//   Release Class: DEFERRED
//   Reason:
//     User-wallet async orchestration supports future rewards, points, balance,
//     and ledger-backed incentive automation, but it is not required for the
//     initial SagrentiDeals release spine. It remains deferred until reward
//     qualification, confirmation, reversal, expiry, and incentive automation
//     are activated as production product behavior.
//
// DEFERRED Rule:
//   Keep compiling.
//   Keep production-ready.
//   Preserve delegation to canonical internal wallet services.
//   Preserve NUMERIC-safe decimal string behavior.
//   Preserve pending -> confirmed ledger lifecycle behavior.
//   Preserve transactional reward reversal behavior.
//   Preserve bounded background execution.
//   Preserve panic containment.
//   Do not introduce direct balance mutation.
//   Do not use float32 or float64 for wallet amounts.
//   Do not duplicate data-layer transaction logic.
//   Do not add new features.
//   Do not route into v1 UI/API expansion.
//   Do not block deployment on this file unless it breaks the build.
package services

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

const userWalletAsyncTimeout = 10 * time.Second

// EnsureUserRewardWalletEvent requests creation or retrieval of a user's
// canonical Sagrenti rewards wallet.
type EnsureUserRewardWalletEvent struct {
	UserID uuid.UUID
}

// CreditUserRewardsEvent requests creation of a pending reward-credit ledger
// entry.
type CreditUserRewardsEvent struct {
	UserID        uuid.UUID
	Amount        string
	ReferenceType string
	ReferenceID   *uuid.UUID
	Description   string
	AvailableAt   *time.Time
}

// ConfirmUserWalletLedgerEntryEvent identifies a pending ledger entry that
// should be confirmed.
type ConfirmUserWalletLedgerEntryEvent struct {
	LedgerEntryID uuid.UUID
}

// ReverseUserRewardEvent identifies a reward ledger entry that should be
// reversed.
type ReverseUserRewardEvent struct {
	LedgerEntryID uuid.UUID
}

// DeactivateUserWalletEvent identifies a wallet that should be deactivated.
type DeactivateUserWalletEvent struct {
	WalletID uuid.UUID
}

// SoftDeleteUserWalletEvent identifies a wallet that should be soft deleted.
type SoftDeleteUserWalletEvent struct {
	WalletID uuid.UUID
}

// DeleteUserWalletEvent identifies a wallet that should be physically deleted
// through the exceptional administrative cleanup path.
type DeleteUserWalletEvent struct {
	WalletID uuid.UUID
}

// runUserWalletAsync executes a wallet operation in a bounded goroutine.
//
// The supplied operation must delegate business and persistence behavior to the
// canonical internal service and data-layer methods.
func runUserWalletAsync(
	ctx context.Context,
	service *Service,
	functionName string,
	operation func(context.Context) error,
) {
	if service == nil || operation == nil {
		return
	}

	go func() {
		innerCtx, cancel := context.WithTimeout(ctx, userWalletAsyncTimeout)
		defer cancel()

		logger := service.Logger.
			GetLoggerWithContextFromContext(innerCtx).
			WithFunctionName(functionName)

		defer func() {
			if recovered := recover(); recovered != nil {
				logger.Error(
					"user wallet async operation panicked",
					"error", fmt.Errorf("%v", recovered),
				)
			}
		}()

		if err := operation(innerCtx); err != nil {
			logger.Error(
				"user wallet async operation failed",
				"error", err,
			)
		}
	}()
}

// EnsureUserRewardWalletAsync asynchronously creates or retrieves the user's
// canonical rewards wallet.
func EnsureUserRewardWalletAsync(
	ctx context.Context,
	service *Service,
	event EnsureUserRewardWalletEvent,
) {
	runUserWalletAsync(
		ctx,
		service,
		"EnsureUserRewardWalletAsync",
		func(innerCtx context.Context) error {
			if event.UserID == uuid.Nil {
				return fmt.Errorf("user ID is required")
			}

			_, err := service.EnsureUserRewardWalletInternal(
				innerCtx,
				event.UserID,
			)
			return err
		},
	)
}

// CreditUserRewardsAsync asynchronously creates a pending reward-credit ledger
// entry.
//
// This operation does not directly increase the wallet's available balance.
// Confirmation remains a separate ledger lifecycle operation.
func CreditUserRewardsAsync(
	ctx context.Context,
	service *Service,
	event CreditUserRewardsEvent,
) {
	runUserWalletAsync(
		ctx,
		service,
		"CreditUserRewardsAsync",
		func(innerCtx context.Context) error {
			if event.UserID == uuid.Nil {
				return fmt.Errorf("user ID is required")
			}

			if strings.TrimSpace(event.Amount) == "" {
				return fmt.Errorf("amount is required")
			}

			if event.ReferenceID != nil && *event.ReferenceID == uuid.Nil {
				return fmt.Errorf("reference ID must not be the nil UUID")
			}

			_, err := service.CreditUserRewardsInternal(
				innerCtx,
				event.UserID,
				event.Amount,
				event.ReferenceType,
				event.ReferenceID,
				event.Description,
				event.AvailableAt,
			)
			return err
		},
	)
}

// ConfirmUserWalletLedgerEntryAsync asynchronously confirms an eligible
// pending reward ledger entry.
func ConfirmUserWalletLedgerEntryAsync(
	ctx context.Context,
	service *Service,
	event ConfirmUserWalletLedgerEntryEvent,
) {
	runUserWalletAsync(
		ctx,
		service,
		"ConfirmUserWalletLedgerEntryAsync",
		func(innerCtx context.Context) error {
			if event.LedgerEntryID == uuid.Nil {
				return fmt.Errorf("ledger entry ID is required")
			}

			_, err := service.ConfirmUserWalletLedgerEntryInternal(
				innerCtx,
				event.LedgerEntryID,
			)
			return err
		},
	)
}

// ReverseUserRewardAsync asynchronously reverses a pending or confirmed reward
// ledger entry.
func ReverseUserRewardAsync(
	ctx context.Context,
	service *Service,
	event ReverseUserRewardEvent,
) {
	runUserWalletAsync(
		ctx,
		service,
		"ReverseUserRewardAsync",
		func(innerCtx context.Context) error {
			if event.LedgerEntryID == uuid.Nil {
				return fmt.Errorf("ledger entry ID is required")
			}

			_, err := service.ReverseUserRewardInternal(
				innerCtx,
				event.LedgerEntryID,
			)
			return err
		},
	)
}

// DeactivateUserWalletAsync asynchronously deactivates a wallet while
// preserving its wallet and ledger history.
func DeactivateUserWalletAsync(
	ctx context.Context,
	service *Service,
	event DeactivateUserWalletEvent,
) {
	runUserWalletAsync(
		ctx,
		service,
		"DeactivateUserWalletAsync",
		func(innerCtx context.Context) error {
			if event.WalletID == uuid.Nil {
				return fmt.Errorf("wallet ID is required")
			}

			return service.DeactivateUserWalletInternal(
				innerCtx,
				event.WalletID,
			)
		},
	)
}

// SoftDeleteUserWalletAsync asynchronously closes and soft deletes a wallet
// while preserving its historical records.
func SoftDeleteUserWalletAsync(
	ctx context.Context,
	service *Service,
	event SoftDeleteUserWalletEvent,
) {
	runUserWalletAsync(
		ctx,
		service,
		"SoftDeleteUserWalletAsync",
		func(innerCtx context.Context) error {
			if event.WalletID == uuid.Nil {
				return fmt.Errorf("wallet ID is required")
			}

			return service.SoftDeleteUserWalletInternal(
				innerCtx,
				event.WalletID,
			)
		},
	)
}

// DeleteUserWalletAsync asynchronously performs an exceptional administrative
// hard deletion of a wallet.
func DeleteUserWalletAsync(
	ctx context.Context,
	service *Service,
	event DeleteUserWalletEvent,
) {
	runUserWalletAsync(
		ctx,
		service,
		"DeleteUserWalletAsync",
		func(innerCtx context.Context) error {
			if event.WalletID == uuid.Nil {
				return fmt.Errorf("wallet ID is required")
			}

			return service.DeleteUserWalletInternal(
				innerCtx,
				event.WalletID,
			)
		},
	)
}