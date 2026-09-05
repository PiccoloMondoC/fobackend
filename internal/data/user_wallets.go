// Package data provides model types and database access methods for user wallets.
//
// sdworkspace/sdbackend/internal/data/user_wallets.go
//
// GTM:
//
//	Layer: 2.3 Consumer Domain
//	Release Class: DEFERRED
//	Reason:
//	  User wallets and wallet ledger entries are valid future rewards,
//	  points, balance, and internal ledger infrastructure, but they are not
//	  required for the initial Platform release spine. The v1 spine
//	  requires account identity, user settings, notifications, favorites/stash,
//	  merchant follows, canonical offers, click tracking, and price history
//	  before expanding into rewards wallets and ledger-backed incentive flows.
//
// DEFERRED Rule:
//
//	Keep compiling.
//	Keep safe.
//	Preserve NUMERIC-safe decimal string behavior.
//	Preserve immutable wallet ledger-entry semantics.
//	Preserve reward-wallet creation semantics.
//	Preserve pending -> confirmed ledger lifecycle behavior.
//	Preserve wallet soft-delete and hard-delete distinction.
//	Do not add new features.
//	Do not route into v1 UI/API expansion.
//	Do not block deployment on this file unless it breaks the build.
package data

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/PiccoloMondoC/focodebase/fobackend/internal/logging"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	WalletTypeRewards         = "rewards"
	WalletTypeGiftCard        = "gift_card"
	WalletTypeCash            = "cash"
	WalletTypeMerchantBalance = "merchant_balance"

	WalletUnitSagrPoints = "SAGR_POINTS"

	WalletStatusActive    = "active"
	WalletStatusInactive  = "inactive"
	WalletStatusSuspended = "suspended"
	WalletStatusClosed    = "closed"

	LedgerEntryEarn       = "earn"
	LedgerEntryRedeem     = "redeem"
	LedgerEntryExpire     = "expire"
	LedgerEntryReverse    = "reverse"
	LedgerEntryAdjustment = "adjustment"
	LedgerEntryHold       = "hold"
	LedgerEntryRelease    = "release"

	LedgerStatusPending   = "pending"
	LedgerStatusConfirmed = "confirmed"
	LedgerStatusReversed  = "reversed"
	LedgerStatusCancelled = "cancelled"
)

var walletAmountPattern = regexp.MustCompile(`^\d+(\.\d{1,4})?$`)

// UserWallet represents a user's wallet/account container.
//
// For the Platform, the first supported production use case is rewards:
// wallet_type = "rewards", unit_code = "SAGR_POINTS".
//
// Balance is the confirmed available balance. LifetimeEarned is cumulative
// confirmed earning activity and should not decrease when points are redeemed.
type UserWallet struct {
	ID             uuid.UUID  `json:"id" db:"id"`
	UserID         uuid.UUID  `json:"user_id" db:"user_id"`
	WalletType     string     `json:"wallet_type" db:"wallet_type"`
	UnitCode       string     `json:"unit_code" db:"unit_code"`
	Balance        string     `json:"balance" db:"balance"`
	LifetimeEarned string     `json:"lifetime_earned" db:"lifetime_earned"`
	Status         string     `json:"status" db:"status"`
	IsActive       bool       `json:"is_active" db:"is_active"`
	DeletedAt      *time.Time `json:"-" db:"deleted_at"`
	CreatedAt      time.Time  `json:"created_at" db:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at" db:"updated_at"`
}

// WalletLedgerEntry represents an immutable wallet ledger event.
//
// Amount is stored and exposed as a string to avoid floating-point money/points
// corruption in Go. PostgreSQL NUMERIC remains the canonical persisted type.
type WalletLedgerEntry struct {
	ID            uuid.UUID  `json:"id" db:"id"`
	WalletID      uuid.UUID  `json:"wallet_id" db:"wallet_id"`
	EntryType     string     `json:"entry_type" db:"entry_type"`
	Amount        string     `json:"amount" db:"amount"`
	Status        string     `json:"status" db:"status"`
	UnitCode      string     `json:"unit_code" db:"unit_code"`
	ReferenceType *string    `json:"reference_type,omitempty" db:"reference_type"`
	ReferenceID   *uuid.UUID `json:"reference_id,omitempty" db:"reference_id"`
	Description   *string    `json:"description,omitempty" db:"description"`
	AvailableAt   *time.Time `json:"available_at,omitempty" db:"available_at"`
	CreatedAt     time.Time  `json:"created_at" db:"created_at"`
}

// UserWalletModel holds the database pool and logger.
type UserWalletModel struct {
	DB     *pgxpool.Pool
	Logger *logging.Logger
}

type walletScanner interface {
	Scan(dest ...any) error
}

// validateWalletAmount validates a NUMERIC(19,4)-compatible positive amount.
func validateWalletAmount(amount string) error {
	amount = strings.TrimSpace(amount)
	if amount == "" {
		return errors.New("amount is required")
	}
	if !walletAmountPattern.MatchString(amount) {
		return errors.New("amount must be a positive decimal with up to 4 decimal places")
	}
	if amount == "0" || amount == "0.0" || amount == "0.00" || amount == "0.000" || amount == "0.0000" {
		return errors.New("amount must be greater than zero")
	}
	return nil
}

func normalizeWalletType(walletType string) string {
	return strings.ToLower(strings.TrimSpace(walletType))
}

func normalizeUnitCode(unitCode string) string {
	return strings.ToUpper(strings.TrimSpace(unitCode))
}

func scanUserWallet(row walletScanner) (*UserWallet, error) {
	var wallet UserWallet

	err := row.Scan(
		&wallet.ID,
		&wallet.UserID,
		&wallet.WalletType,
		&wallet.UnitCode,
		&wallet.Balance,
		&wallet.LifetimeEarned,
		&wallet.Status,
		&wallet.IsActive,
		&wallet.DeletedAt,
		&wallet.CreatedAt,
		&wallet.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}

	return &wallet, nil
}

func scanWalletLedgerEntry(row walletScanner) (*WalletLedgerEntry, error) {
	var entry WalletLedgerEntry

	err := row.Scan(
		&entry.ID,
		&entry.WalletID,
		&entry.EntryType,
		&entry.Amount,
		&entry.Status,
		&entry.UnitCode,
		&entry.ReferenceType,
		&entry.ReferenceID,
		&entry.Description,
		&entry.AvailableAt,
		&entry.CreatedAt,
	)
	if err != nil {
		return nil, err
	}

	return &entry, nil
}

// EnsureRewardWallet creates or retrieves the user's Platform reward wallet.
//
// This method is safe to call after registration and before reward issuance.
// It does not mutate balances and does not create ledger activity.
func (m *UserWalletModel) EnsureRewardWallet(ctx context.Context, userID uuid.UUID) (*UserWallet, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("EnsureRewardWallet")

	if userID == uuid.Nil {
		err := errors.New("user ID is required")
		logger.Error("validation failed", "error", err)
		return nil, err
	}

	tx, err := m.DB.Begin(ctx)
	if err != nil {
		logger.Error("begin transaction failed", "error", err)
		return nil, err
	}
	defer tx.Rollback(ctx)

	wallet, err := ensureRewardWalletTx(ctx, tx, userID)
	if err != nil {
		logger.Error("ensure reward wallet failed", "error", err, "user_id", userID)
		return nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		logger.Error("commit transaction failed", "error", err)
		return nil, err
	}

	logger.Info("reward wallet ensured", "user_id", userID, "wallet_id", wallet.ID)
	return wallet, nil
}

// GetByID retrieves a non-deleted wallet by wallet ID.
func (m *UserWalletModel) GetByID(ctx context.Context, id uuid.UUID) (*UserWallet, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("GetUserWalletByID")

	if id == uuid.Nil {
		err := errors.New("wallet ID is required")
		logger.Error("validation failed", "error", err)
		return nil, err
	}

	query := `
		SELECT
			id,
			user_id,
			wallet_type,
			unit_code,
			balance::TEXT,
			lifetime_earned::TEXT,
			status,
			is_active,
			deleted_at,
			created_at,
			updated_at
		FROM user_wallets
		WHERE id = $1
		  AND deleted_at IS NULL
	`

	wallet, err := scanUserWallet(m.DB.QueryRow(ctx, query, id))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			logger.Warn("user wallet not found", "wallet_id", id)
			return nil, ErrWalletNotFound
		}
		logger.Error("get user wallet failed", "error", err, "wallet_id", id)
		return nil, err
	}

	return wallet, nil
}

// GetByUserID retrieves all non-deleted wallets for a user.
func (m *UserWalletModel) GetByUserID(ctx context.Context, userID uuid.UUID) ([]UserWallet, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("GetUserWalletsByUserID")

	if userID == uuid.Nil {
		err := errors.New("user ID is required")
		logger.Error("validation failed", "error", err)
		return nil, err
	}

	query := `
		SELECT
			id,
			user_id,
			wallet_type,
			unit_code,
			balance::TEXT,
			lifetime_earned::TEXT,
			status,
			is_active,
			deleted_at,
			created_at,
			updated_at
		FROM user_wallets
		WHERE user_id = $1
		  AND deleted_at IS NULL
		ORDER BY wallet_type, unit_code
	`

	rows, err := m.DB.Query(ctx, query, userID)
	if err != nil {
		logger.Error("query user wallets failed", "error", err, "user_id", userID)
		return nil, err
	}
	defer rows.Close()

	wallets := make([]UserWallet, 0)

	for rows.Next() {
		wallet, err := scanUserWallet(rows)
		if err != nil {
			logger.Error("scan user wallet failed", "error", err)
			return nil, err
		}
		wallets = append(wallets, *wallet)
	}

	if err := rows.Err(); err != nil {
		logger.Error("iterate user wallets failed", "error", err)
		return nil, err
	}

	return wallets, nil
}

// GetByUserIDAndType retrieves a specific non-deleted wallet by owner, type, and unit.
func (m *UserWalletModel) GetByUserIDAndType(
	ctx context.Context,
	userID uuid.UUID,
	walletType string,
	unitCode string,
) (*UserWallet, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("GetUserWalletByUserIDAndType")

	walletType = normalizeWalletType(walletType)
	unitCode = normalizeUnitCode(unitCode)

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

	query := `
		SELECT
			id,
			user_id,
			wallet_type,
			unit_code,
			balance::TEXT,
			lifetime_earned::TEXT,
			status,
			is_active,
			deleted_at,
			created_at,
			updated_at
		FROM user_wallets
		WHERE user_id = $1
		  AND wallet_type = $2
		  AND unit_code = $3
		  AND deleted_at IS NULL
	`

	wallet, err := scanUserWallet(m.DB.QueryRow(ctx, query, userID, walletType, unitCode))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			logger.Warn("user wallet not found", "user_id", userID, "wallet_type", walletType, "unit_code", unitCode)
			return nil, ErrWalletNotFound
		}
		logger.Error("get user wallet by type failed", "error", err, "user_id", userID, "wallet_type", walletType, "unit_code", unitCode)
		return nil, err
	}
	return wallet, nil
}

// List retrieves non-deleted wallets with pagination.
func (m *UserWalletModel) List(ctx context.Context, limit, offset int) ([]UserWallet, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("ListUserWallets")

	if limit <= 0 {
		err := errors.New("limit must be greater than zero")
		logger.Error("validation failed", "error", err)
		return nil, err
	}
	if limit > 100 {
		limit = 100
	}
	if offset < 0 {
		err := errors.New("offset cannot be negative")
		logger.Error("validation failed", "error", err)
		return nil, err
	}

	query := `
		SELECT
			id,
			user_id,
			wallet_type,
			unit_code,
			balance::TEXT,
			lifetime_earned::TEXT,
			status,
			is_active,
			deleted_at,
			created_at,
			updated_at
		FROM user_wallets
		WHERE deleted_at IS NULL
		ORDER BY created_at DESC
		LIMIT $1 OFFSET $2
	`

	rows, err := m.DB.Query(ctx, query, limit, offset)
	if err != nil {
		logger.Error("list user wallets failed", "error", err)
		return nil, err
	}
	defer rows.Close()

	wallets := make([]UserWallet, 0)

	for rows.Next() {
		wallet, err := scanUserWallet(rows)
		if err != nil {
			logger.Error("scan user wallet failed", "error", err)
			return nil, err
		}
		wallets = append(wallets, *wallet)
	}

	if err := rows.Err(); err != nil {
		logger.Error("iterate user wallets failed", "error", err)
		return nil, err
	}

	return wallets, nil
}

// CreditRewards records a reward earning event for a user's rewards wallet.
//
// By default this creates a pending ledger entry. ConfirmLedgerEntry must be
// called after the relevant affiliate return/reversal window before points are
// added to the available balance.
func (m *UserWalletModel) CreditRewards(
	ctx context.Context,
	userID uuid.UUID,
	amount string,
	referenceType string,
	referenceID *uuid.UUID,
	description string,
	availableAt *time.Time,
) (*WalletLedgerEntry, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("CreditRewards")

	amount = strings.TrimSpace(amount)
	referenceType = strings.TrimSpace(referenceType)
	description = strings.TrimSpace(description)

	if userID == uuid.Nil {
		err := errors.New("user ID is required")
		logger.Error("validation failed", "error", err)
		return nil, err
	}
	if err := validateWalletAmount(amount); err != nil {
		logger.Error("validation failed", "error", err, "amount", amount)
		return nil, err
	}
	if referenceID != nil && *referenceID == uuid.Nil {
		err := errors.New("reference ID must not be nil UUID when provided")
		logger.Error("validation failed", "error", err)
		return nil, err
	}

	tx, err := m.DB.Begin(ctx)
	if err != nil {
		logger.Error("begin transaction failed", "error", err)
		return nil, err
	}
	defer tx.Rollback(ctx)

	wallet, err := ensureRewardWalletTx(ctx, tx, userID)
	if err != nil {
		logger.Error("ensure reward wallet in transaction failed", "error", err, "user_id", userID)
		return nil, err
	}

	var refType *string
	if referenceType != "" {
		refType = &referenceType
	}

	var desc *string
	if description != "" {
		desc = &description
	}

	query := `
		INSERT INTO wallet_ledger_entries (
			wallet_id,
			entry_type,
			amount,
			status,
			unit_code,
			reference_type,
			reference_id,
			description,
			available_at
		)
		VALUES (
			$1,
			'earn',
			$2::NUMERIC(19,4),
			'pending',
			'SAGR_POINTS',
			$3,
			$4,
			$5,
			$6
		)
		RETURNING
			id,
			wallet_id,
			entry_type,
			amount::TEXT,
			status,
			unit_code,
			reference_type,
			reference_id,
			description,
			available_at,
			created_at
	`

	entry, err := scanWalletLedgerEntry(tx.QueryRow(ctx, query, wallet.ID, amount, refType, referenceID, desc, availableAt))
	if err != nil {
		logger.Error("insert reward ledger entry failed", "error", err, "wallet_id", wallet.ID)
		return nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		logger.Error("commit transaction failed", "error", err)
		return nil, err
	}

	logger.Info("reward credit ledger entry created", "user_id", userID, "wallet_id", wallet.ID, "ledger_entry_id", entry.ID)
	return entry, nil
}

// ConfirmLedgerEntry confirms a pending earning ledger entry and applies it to
// the wallet's available balance in the same transaction.
func (m *UserWalletModel) ConfirmLedgerEntry(ctx context.Context, entryID uuid.UUID) (*WalletLedgerEntry, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("ConfirmWalletLedgerEntry")

	if entryID == uuid.Nil {
		err := errors.New("ledger entry ID is required")
		logger.Error("validation failed", "error", err)
		return nil, err
	}

	tx, err := m.DB.Begin(ctx)
	if err != nil {
		logger.Error("begin transaction failed", "error", err)
		return nil, err
	}
	defer tx.Rollback(ctx)

	entry, err := scanWalletLedgerEntry(tx.QueryRow(ctx, `
		SELECT
			id,
			wallet_id,
			entry_type,
			amount::TEXT,
			status,
			unit_code,
			reference_type,
			reference_id,
			description,
			available_at,
			created_at
		FROM wallet_ledger_entries
		WHERE id = $1
		FOR UPDATE
	`, entryID))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			logger.Warn("wallet ledger entry not found", "ledger_entry_id", entryID)
			return nil, ErrLedgerEntryNotFound
		}
		logger.Error("get wallet ledger entry failed", "error", err, "ledger_entry_id", entryID)
		return nil, err
	}

	if entry.Status != LedgerStatusPending {
		err := fmt.Errorf("ledger entry must be pending to confirm: current status %q", entry.Status)
		logger.Warn("invalid ledger confirmation", "ledger_entry_id", entryID, "status", entry.Status)
		return nil, err
	}

	if entry.EntryType != LedgerEntryEarn && entry.EntryType != LedgerEntryAdjustment {
		err := fmt.Errorf("ledger entry type %q cannot be confirmed as reward credit", entry.EntryType)
		logger.Warn("invalid ledger entry type for confirmation", "ledger_entry_id", entryID, "entry_type", entry.EntryType)
		return nil, err
	}

	cmdTag, err := tx.Exec(ctx, `
		UPDATE user_wallets
		SET
			balance = balance + $1::NUMERIC(19,4),
			lifetime_earned = CASE
				WHEN $2 = 'earn' THEN lifetime_earned + $1::NUMERIC(19,4)
				ELSE lifetime_earned
			END
		WHERE id = $3
		AND deleted_at IS NULL
		AND status = 'active'
		AND is_active = TRUE
	`, entry.Amount, entry.EntryType, entry.WalletID)
	if err != nil {
		logger.Error("update wallet balance for confirmation failed", "error", err, "wallet_id", entry.WalletID)
		return nil, err
	}
	if cmdTag.RowsAffected() == 0 {
		err := fmt.Errorf("%w: wallet is missing, inactive, closed, suspended, or deleted", ErrInvalidWalletState)
		logger.Warn("wallet balance update skipped during confirmation", "wallet_id", entry.WalletID, "ledger_entry_id", entryID)
		return nil, err
	}

	confirmed, err := scanWalletLedgerEntry(tx.QueryRow(ctx, `
		UPDATE wallet_ledger_entries
		SET status = 'confirmed'
		WHERE id = $1
		RETURNING
			id,
			wallet_id,
			entry_type,
			amount::TEXT,
			status,
			unit_code,
			reference_type,
			reference_id,
			description,
			available_at,
			created_at
	`, entryID))
	if err != nil {
		logger.Error("confirm ledger entry failed", "error", err, "ledger_entry_id", entryID)
		return nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		logger.Error("commit transaction failed", "error", err)
		return nil, err
	}

	logger.Info("wallet ledger entry confirmed", "ledger_entry_id", entryID, "wallet_id", confirmed.WalletID)
	return confirmed, nil
}

// ReverseReward reverses a pending or confirmed reward-credit ledger entry.
//
// Pending entries are marked reversed without wallet balance mutation because
// they were never applied. Confirmed reward credits debit the wallet balance and
// mark the original ledger entry reversed in the same transaction.
//
// This method does not reduce lifetime_earned; that field tracks gross earning
// history, not current spendable balance.
func (m *UserWalletModel) ReverseReward(ctx context.Context, entryID uuid.UUID) (*WalletLedgerEntry, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("ReverseReward")

	if entryID == uuid.Nil {
		err := errors.New("ledger entry ID is required")
		logger.Error("validation failed", "error", err)
		return nil, err
	}

	tx, err := m.DB.Begin(ctx)
	if err != nil {
		logger.Error("begin transaction failed", "error", err)
		return nil, err
	}
	defer tx.Rollback(ctx)

	original, err := scanWalletLedgerEntry(tx.QueryRow(ctx, `
		SELECT
			id,
			wallet_id,
			entry_type,
			amount::TEXT,
			status,
			unit_code,
			reference_type,
			reference_id,
			description,
			available_at,
			created_at
		FROM wallet_ledger_entries
		WHERE id = $1
		FOR UPDATE
	`, entryID))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			logger.Warn("wallet ledger entry not found", "ledger_entry_id", entryID)
			return nil, ErrLedgerEntryNotFound
		}
		logger.Error("get wallet ledger entry failed", "error", err, "ledger_entry_id", entryID)
		return nil, err
	}

	if original.EntryType != LedgerEntryEarn && original.EntryType != LedgerEntryAdjustment {
		err := fmt.Errorf("%w: ledger entry type %q cannot be reversed as reward credit", ErrInvalidLedgerState, original.EntryType)
		logger.Warn("invalid ledger entry type for reward reversal", "ledger_entry_id", entryID, "entry_type", original.EntryType)
		return nil, err
	}

	switch original.Status {
	case LedgerStatusPending:
		reversedOriginal, err := scanWalletLedgerEntry(tx.QueryRow(ctx, `
			UPDATE wallet_ledger_entries
			SET status = 'reversed'
			WHERE id = $1
			RETURNING
				id,
				wallet_id,
				entry_type,
				amount::TEXT,
				status,
				unit_code,
				reference_type,
				reference_id,
				description,
				available_at,
				created_at
		`, entryID))
		if err != nil {
			logger.Error("mark pending ledger entry reversed failed", "error", err, "ledger_entry_id", entryID)
			return nil, err
		}

		if err := tx.Commit(ctx); err != nil {
			logger.Error("commit transaction failed", "error", err)
			return nil, err
		}

		logger.Info("pending wallet ledger entry reversed", "ledger_entry_id", entryID)
		return reversedOriginal, nil

	case LedgerStatusConfirmed:
		cmdTag, err := tx.Exec(ctx, `
			UPDATE user_wallets
			SET balance = balance - $1::NUMERIC(19,4)
			WHERE id = $2
			  AND deleted_at IS NULL
			  AND status = 'active'
			  AND is_active = TRUE
			  AND balance >= $1::NUMERIC(19,4)
		`, original.Amount, original.WalletID)
		if err != nil {
			logger.Error("debit wallet balance for reversal failed", "error", err, "wallet_id", original.WalletID, "ledger_entry_id", entryID)
			return nil, err
		}
		if cmdTag.RowsAffected() == 0 {
			err := fmt.Errorf("%w: wallet is missing, inactive, closed, suspended, deleted, or has insufficient balance", ErrInvalidWalletState)
			logger.Warn("wallet balance debit skipped during reversal", "wallet_id", original.WalletID, "ledger_entry_id", entryID)
			return nil, err
		}

		reversedOriginal, err := scanWalletLedgerEntry(tx.QueryRow(ctx, `
			UPDATE wallet_ledger_entries
			SET status = 'reversed'
			WHERE id = $1
			RETURNING
				id,
				wallet_id,
				entry_type,
				amount::TEXT,
				status,
				unit_code,
				reference_type,
				reference_id,
				description,
				available_at,
				created_at
		`, entryID))
		if err != nil {
			logger.Error("mark confirmed ledger entry reversed failed", "error", err, "ledger_entry_id", entryID)
			return nil, err
		}

		if err := tx.Commit(ctx); err != nil {
			logger.Error("commit transaction failed", "error", err)
			return nil, err
		}

		logger.Info("confirmed wallet ledger entry reversed", "ledger_entry_id", entryID, "wallet_id", original.WalletID)
		return reversedOriginal, nil

	case LedgerStatusReversed, LedgerStatusCancelled:
		err := fmt.Errorf("%w: ledger entry is already terminal: current status %q", ErrInvalidLedgerState, original.Status)
		logger.Warn("invalid ledger reversal", "ledger_entry_id", entryID, "status", original.Status)
		return nil, err

	default:
		err := fmt.Errorf("%w: ledger entry status %q cannot be reversed", ErrInvalidLedgerState, original.Status)
		logger.Warn("invalid ledger reversal status", "ledger_entry_id", entryID, "status", original.Status)
		return nil, err
	}
}

// Deactivate marks a wallet inactive without deleting historical records.
func (m *UserWalletModel) Deactivate(ctx context.Context, id uuid.UUID) error {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("DeactivateUserWallet")

	if id == uuid.Nil {
		err := errors.New("wallet ID is required")
		logger.Error("validation failed", "error", err)
		return err
	}

	cmdTag, err := m.DB.Exec(ctx, `
		UPDATE user_wallets
		SET status = 'inactive',
		    is_active = FALSE
		WHERE id = $1
		  AND deleted_at IS NULL
	`, id)
	if err != nil {
		logger.Error("deactivate user wallet failed", "error", err, "wallet_id", id)
		return err
	}

	if cmdTag.RowsAffected() == 0 {
		err := ErrWalletNotFound
		logger.Warn("deactivate user wallet failed", "wallet_id", id)
		return err
	}

	return nil
}

// SoftDelete logically removes a wallet by setting deleted_at and closing it.
//
// Normal reads exclude deleted wallets. This preserves wallet and ledger history
// while removing the wallet from active product behavior.
func (m *UserWalletModel) SoftDelete(ctx context.Context, id uuid.UUID) error {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("SoftDeleteUserWallet")

	if id == uuid.Nil {
		err := errors.New("wallet ID is required")
		logger.Error("validation failed", "error", err)
		return err
	}

	cmdTag, err := m.DB.Exec(ctx, `
		UPDATE user_wallets
		SET deleted_at = NOW(),
		    status = 'closed',
		    is_active = FALSE
		WHERE id = $1
		  AND deleted_at IS NULL
	`, id)
	if err != nil {
		logger.Error("soft delete user wallet failed", "error", err, "wallet_id", id)
		return err
	}

	if cmdTag.RowsAffected() == 0 {
		err := ErrWalletNotFound
		logger.Warn("soft delete user wallet failed", "wallet_id", id)
		return err
	}

	return nil
}

// Delete physically removes a wallet.
//
// This is a true hard delete and should remain an exceptional administrative
// cleanup path. Normal business removal should use SoftDelete.
func (m *UserWalletModel) Delete(ctx context.Context, id uuid.UUID) error {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("DeleteUserWallet")

	if id == uuid.Nil {
		err := errors.New("wallet ID is required")
		logger.Error("validation failed", "error", err)
		return err
	}

	cmdTag, err := m.DB.Exec(ctx, `DELETE FROM user_wallets WHERE id = $1`, id)
	if err != nil {
		logger.Error("delete user wallet failed", "error", err, "wallet_id", id)
		return err
	}

	if cmdTag.RowsAffected() == 0 {
		err := ErrWalletNotFound
		logger.Warn("delete user wallet failed", "wallet_id", id)
		return err
	}

	return nil
}

func ensureRewardWalletTx(ctx context.Context, tx pgx.Tx, userID uuid.UUID) (*UserWallet, error) {
	query := `
		WITH inserted AS (
			INSERT INTO user_wallets (
				user_id,
				wallet_type,
				unit_code,
				balance,
				lifetime_earned,
				status,
				is_active
			)
			VALUES ($1, 'rewards', 'SAGR_POINTS', 0, 0, 'active', TRUE)
			ON CONFLICT (user_id, wallet_type, unit_code) DO NOTHING
			RETURNING
				id,
				user_id,
				wallet_type,
				unit_code,
				balance::TEXT,
				lifetime_earned::TEXT,
				status,
				is_active,
				deleted_at,
				created_at,
				updated_at
		)
		SELECT *
		FROM inserted

		UNION ALL

		SELECT
			id,
			user_id,
			wallet_type,
			unit_code,
			balance::TEXT,
			lifetime_earned::TEXT,
			status,
			is_active,
			deleted_at,
			created_at,
			updated_at
		FROM user_wallets
		WHERE user_id = $1
		  AND wallet_type = 'rewards'
		  AND unit_code = 'SAGR_POINTS'
		  AND deleted_at IS NULL
		  AND NOT EXISTS (SELECT 1 FROM inserted)
		LIMIT 1
	`

	return scanUserWallet(tx.QueryRow(ctx, query, userID))
}
