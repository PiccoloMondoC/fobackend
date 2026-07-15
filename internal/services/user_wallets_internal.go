// Package services contains business logic for internal operations such as moderation,
// automation, audit logging, and data synchronization. It is used by async routines
// and internal system workflows, not exposed via public API routes.
//
// sdworkspace/sdbackend/internal/services/user_wallets_internal.go
//
// GTM:
//   Layer: 2.3 Consumer Domain
//   Release Class: DEFERRED
//   Reason:
//     User wallets and wallet-ledger operations are valid future rewards,
//     points, balance, and incentive infrastructure, but they are not required
//     for the initial SagrentiDeals release spine. The v1 spine requires
//     account identity, user settings, notifications, favorites/stash,
//     merchant follows, canonical offers, click tracking, and price history
//     before rewards wallets and ledger-backed incentive flows are activated.
//
// DEFERRED Rule:
//   Keep compiling.
//   Keep production-ready.
//   Preserve NUMERIC-safe decimal string behavior.
//   Preserve immutable wallet ledger-entry semantics.
//   Preserve reward-wallet creation semantics.
//   Preserve pending -> confirmed ledger lifecycle behavior.
//   Preserve transactional reward reversal behavior.
//   Preserve wallet soft-delete and hard-delete distinction.
//   Do not introduce direct balance mutation.
//   Do not use float32 or float64 for wallet amounts.
//   Do not duplicate data-layer transaction logic.
//   Do not add new features.
//   Do not route into v1 UI/API expansion.
//   Do not block deployment on this file unless it breaks the build.
package services

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/PiccoloMondoC/sdworkspace/sdbackend/internal/data"

	"github.com/google/uuid"
)

// EnsureUserRewardWalletInternal creates or retrieves the user's canonical
// Sagrenti rewards wallet.
//
// This operation does not mutate the wallet balance and does not create a
// wallet ledger entry.
func (s *Service) EnsureUserRewardWalletInternal(
	ctx context.Context,
	userID uuid.UUID,
) (*data.UserWallet, error) {
	logger := s.Logger.
		GetLoggerWithContextFromContext(ctx).
		WithFunctionName("EnsureUserRewardWalletInternal")

	if userID == uuid.Nil {
		err := errors.New("user ID is required")
		logger.Error("validation failed", "error", err)
		return nil, err
	}

	wallet, err := s.Models.UserWallet.EnsureRewardWallet(ctx, userID)
	if err != nil {
		logger.Error(
			"ensure user reward wallet failed",
			"error", err,
			"user_id", userID,
		)
		return nil, err
	}

	logger.Info(
		"user reward wallet ensured",
		"user_id", userID,
		"wallet_id", wallet.ID,
	)

	return wallet, nil
}

// GetUserWalletByIDInternal retrieves a non-deleted wallet by wallet ID.
func (s *Service) GetUserWalletByIDInternal(
	ctx context.Context,
	walletID uuid.UUID,
) (*data.UserWallet, error) {
	logger := s.Logger.
		GetLoggerWithContextFromContext(ctx).
		WithFunctionName("GetUserWalletByIDInternal")

	if walletID == uuid.Nil {
		err := errors.New("wallet ID is required")
		logger.Error("validation failed", "error", err)
		return nil, err
	}

	wallet, err := s.Models.UserWallet.GetByID(ctx, walletID)
	if err != nil {
		logger.Error(
			"get user wallet by ID failed",
			"error", err,
			"wallet_id", walletID,
		)
		return nil, err
	}

	return wallet, nil
}

// GetUserWalletsByUserIDInternal retrieves all non-deleted wallets owned by a
// user.
func (s *Service) GetUserWalletsByUserIDInternal(
	ctx context.Context,
	userID uuid.UUID,
) ([]data.UserWallet, error) {
	logger := s.Logger.
		GetLoggerWithContextFromContext(ctx).
		WithFunctionName("GetUserWalletsByUserIDInternal")

	if userID == uuid.Nil {
		err := errors.New("user ID is required")
		logger.Error("validation failed", "error", err)
		return nil, err
	}

	wallets, err := s.Models.UserWallet.GetByUserID(ctx, userID)
	if err != nil {
		logger.Error(
			"get user wallets by user ID failed",
			"error", err,
			"user_id", userID,
		)
		return nil, err
	}

	return wallets, nil
}

// GetUserWalletByUserIDAndTypeInternal retrieves a specific non-deleted wallet
// by owner, wallet type, and unit code.
func (s *Service) GetUserWalletByUserIDAndTypeInternal(
	ctx context.Context,
	userID uuid.UUID,
	walletType string,
	unitCode string,
) (*data.UserWallet, error) {
	logger := s.Logger.
		GetLoggerWithContextFromContext(ctx).
		WithFunctionName("GetUserWalletByUserIDAndTypeInternal")

	walletType = strings.TrimSpace(walletType)
	unitCode = strings.TrimSpace(unitCode)

	if userID == uuid.Nil {
		err := errors.New("user ID is required")
		logger.Error("validation failed", "error", err)
		return nil, err
	}

	if walletType == "" {
		err := errors.New("wallet type is required")
		logger.Error("validation failed", "error", err)
		return nil, err
	}

	if unitCode == "" {
		err := errors.New("unit code is required")
		logger.Error("validation failed", "error", err)
		return nil, err
	}

	wallet, err := s.Models.UserWallet.GetByUserIDAndType(
		ctx,
		userID,
		walletType,
		unitCode,
	)
	if err != nil {
		logger.Error(
			"get user wallet by owner and type failed",
			"error", err,
			"user_id", userID,
			"wallet_type", walletType,
			"unit_code", unitCode,
		)
		return nil, err
	}

	return wallet, nil
}

// ListUserWalletsInternal retrieves non-deleted wallets with bounded
// pagination.
func (s *Service) ListUserWalletsInternal(
	ctx context.Context,
	limit int,
	offset int,
) ([]data.UserWallet, error) {
	logger := s.Logger.
		GetLoggerWithContextFromContext(ctx).
		WithFunctionName("ListUserWalletsInternal")

	if limit <= 0 {
		err := errors.New("limit must be greater than zero")
		logger.Error(
			"validation failed",
			"error", err,
			"limit", limit,
		)
		return nil, err
	}

	if offset < 0 {
		err := errors.New("offset cannot be negative")
		logger.Error(
			"validation failed",
			"error", err,
			"offset", offset,
		)
		return nil, err
	}

	wallets, err := s.Models.UserWallet.List(ctx, limit, offset)
	if err != nil {
		logger.Error(
			"list user wallets failed",
			"error", err,
			"limit", limit,
			"offset", offset,
		)
		return nil, err
	}

	return wallets, nil
}

// CreditUserRewardsInternal creates a pending reward-credit ledger entry.
//
// The reward is not added directly to the available wallet balance. The
// resulting ledger entry must later be confirmed after the relevant
// qualification, return, or reversal window.
func (s *Service) CreditUserRewardsInternal(
	ctx context.Context,
	userID uuid.UUID,
	amount string,
	referenceType string,
	referenceID *uuid.UUID,
	description string,
	availableAt *time.Time,
) (*data.WalletLedgerEntry, error) {
	logger := s.Logger.
		GetLoggerWithContextFromContext(ctx).
		WithFunctionName("CreditUserRewardsInternal")

	amount = strings.TrimSpace(amount)
	referenceType = strings.TrimSpace(referenceType)
	description = strings.TrimSpace(description)

	if userID == uuid.Nil {
		err := errors.New("user ID is required")
		logger.Error("validation failed", "error", err)
		return nil, err
	}

	if amount == "" {
		err := errors.New("amount is required")
		logger.Error("validation failed", "error", err)
		return nil, err
	}

	if referenceID != nil && *referenceID == uuid.Nil {
		err := errors.New("reference ID must not be the nil UUID")
		logger.Error("validation failed", "error", err)
		return nil, err
	}

	entry, err := s.Models.UserWallet.CreditRewards(
		ctx,
		userID,
		amount,
		referenceType,
		referenceID,
		description,
		availableAt,
	)
	if err != nil {
		logger.Error(
			"credit user rewards failed",
			"error", err,
			"user_id", userID,
			"reference_type", referenceType,
		)
		return nil, err
	}

	logger.Info(
		"pending reward ledger entry created",
		"user_id", userID,
		"wallet_id", entry.WalletID,
		"ledger_entry_id", entry.ID,
	)

	return entry, nil
}

// ConfirmUserWalletLedgerEntryInternal confirms an eligible pending reward
// ledger entry and applies its amount to the wallet balance atomically.
func (s *Service) ConfirmUserWalletLedgerEntryInternal(
	ctx context.Context,
	entryID uuid.UUID,
) (*data.WalletLedgerEntry, error) {
	logger := s.Logger.
		GetLoggerWithContextFromContext(ctx).
		WithFunctionName("ConfirmUserWalletLedgerEntryInternal")

	if entryID == uuid.Nil {
		err := errors.New("ledger entry ID is required")
		logger.Error("validation failed", "error", err)
		return nil, err
	}

	entry, err := s.Models.UserWallet.ConfirmLedgerEntry(ctx, entryID)
	if err != nil {
		logger.Error(
			"confirm user wallet ledger entry failed",
			"error", err,
			"ledger_entry_id", entryID,
		)
		return nil, err
	}

	logger.Info(
		"user wallet ledger entry confirmed",
		"ledger_entry_id", entry.ID,
		"wallet_id", entry.WalletID,
	)

	return entry, nil
}

// ReverseUserRewardInternal reverses a pending or confirmed reward-credit
// ledger entry using the data layer's transactional reversal behavior.
func (s *Service) ReverseUserRewardInternal(
	ctx context.Context,
	entryID uuid.UUID,
) (*data.WalletLedgerEntry, error) {
	logger := s.Logger.
		GetLoggerWithContextFromContext(ctx).
		WithFunctionName("ReverseUserRewardInternal")

	if entryID == uuid.Nil {
		err := errors.New("ledger entry ID is required")
		logger.Error("validation failed", "error", err)
		return nil, err
	}

	entry, err := s.Models.UserWallet.ReverseReward(ctx, entryID)
	if err != nil {
		logger.Error(
			"reverse user reward failed",
			"error", err,
			"ledger_entry_id", entryID,
		)
		return nil, err
	}

	logger.Info(
		"user reward ledger entry reversed",
		"ledger_entry_id", entry.ID,
		"wallet_id", entry.WalletID,
	)

	return entry, nil
}

// DeactivateUserWalletInternal marks a wallet inactive while preserving its
// wallet and ledger history.
func (s *Service) DeactivateUserWalletInternal(
	ctx context.Context,
	walletID uuid.UUID,
) error {
	logger := s.Logger.
		GetLoggerWithContextFromContext(ctx).
		WithFunctionName("DeactivateUserWalletInternal")

	if walletID == uuid.Nil {
		err := errors.New("wallet ID is required")
		logger.Error("validation failed", "error", err)
		return err
	}

	if err := s.Models.UserWallet.Deactivate(ctx, walletID); err != nil {
		logger.Error(
			"deactivate user wallet failed",
			"error", err,
			"wallet_id", walletID,
		)
		return err
	}

	logger.Info(
		"user wallet deactivated",
		"wallet_id", walletID,
	)

	return nil
}

// SoftDeleteUserWalletInternal logically removes a wallet while preserving its
// historical records.
func (s *Service) SoftDeleteUserWalletInternal(
	ctx context.Context,
	walletID uuid.UUID,
) error {
	logger := s.Logger.
		GetLoggerWithContextFromContext(ctx).
		WithFunctionName("SoftDeleteUserWalletInternal")

	if walletID == uuid.Nil {
		err := errors.New("wallet ID is required")
		logger.Error("validation failed", "error", err)
		return err
	}

	if err := s.Models.UserWallet.SoftDelete(ctx, walletID); err != nil {
		logger.Error(
			"soft delete user wallet failed",
			"error", err,
			"wallet_id", walletID,
		)
		return err
	}

	logger.Info(
		"user wallet soft deleted",
		"wallet_id", walletID,
	)

	return nil
}

// DeleteUserWalletInternal physically removes a wallet.
//
// This is an exceptional administrative cleanup path. Normal wallet removal
// must use SoftDeleteUserWalletInternal.
func (s *Service) DeleteUserWalletInternal(
	ctx context.Context,
	walletID uuid.UUID,
) error {
	logger := s.Logger.
		GetLoggerWithContextFromContext(ctx).
		WithFunctionName("DeleteUserWalletInternal")

	if walletID == uuid.Nil {
		err := errors.New("wallet ID is required")
		logger.Error("validation failed", "error", err)
		return err
	}

	if err := s.Models.UserWallet.Delete(ctx, walletID); err != nil {
		logger.Error(
			"hard delete user wallet failed",
			"error", err,
			"wallet_id", walletID,
		)
		return err
	}

	logger.Info(
		"user wallet hard deleted",
		"wallet_id", walletID,
	)

	return nil
}