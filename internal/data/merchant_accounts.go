// Package data provides models and database access methods for merchant accounts.
//
// sdworkspace/sdbackend/internal/data/merchant_accounts.go
//
// GTM:
//
//	Layer: 2.4 Merchant / Future Offering Domain
//	Release Class: SPINE
//	Reason:
//	  merchant_accounts is the canonical platform-account lifecycle record for
//	  a merchant. It establishes whether the merchant platform relationship is
//	  pending, active, suspended, or closed and preserves the original
//	  onboarding milestone required by Merchant Center, Future Offering,
//	  subscription, entitlement, billing, and merchant-operability workflows.
//
// SPINE Rule:
//
//	Keep compiling.
//	Keep production-ready.
//	Preserve exactly one canonical merchant account per merchant.
//	Preserve merchant ownership immutability.
//	Preserve controlled account-status vocabulary.
//	Preserve explicit and retry-safe lifecycle transitions.
//	Preserve database-owned lifecycle timestamps.
//	Preserve onboarded_at as the original onboarding milestone.
//	Preserve initial_plan_id as historical onboarding context only.
//	Preserve separation between account status, soft deletion, restoration,
//	and permanent deletion.
//	Block deployment if this file breaks merchant-account persistence,
//	lifecycle integrity, Merchant Center readiness, or dependent merchant
//	monetization and Future Offering workflows.
package data

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/PiccoloMondoC/sdworkspace/sdbackend/internal/logging"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// MerchantAccountStatus is the controlled vocabulary for
// merchant_accounts.account_status.
type MerchantAccountStatus string

const (
	// MerchantAccountStatusPending indicates that the merchant account exists
	// but merchant onboarding has not yet completed.
	MerchantAccountStatusPending MerchantAccountStatus = "pending"

	// MerchantAccountStatusActive indicates that the merchant account is
	// operational and may participate in authorized platform workflows.
	MerchantAccountStatusActive MerchantAccountStatus = "active"

	// MerchantAccountStatusSuspended indicates that the merchant account is
	// retained but temporarily blocked from operational platform workflows.
	MerchantAccountStatusSuspended MerchantAccountStatus = "suspended"

	// MerchantAccountStatusClosed indicates that the merchant account has been
	// intentionally closed. Closure is distinct from soft deletion.
	MerchantAccountStatusClosed MerchantAccountStatus = "closed"
)

const merchantAccountSelectColumns = `
	id,
	merchant_id,
	account_status,
	onboarded_at,
	initial_plan_id,
	created_at,
	updated_at,
	deleted_at
`

// MerchantAccount represents the canonical platform-account lifecycle record
// associated with one merchant.
type MerchantAccount struct {
	ID uuid.UUID `json:"id" db:"id"`

	// MerchantID identifies the merchant that owns this platform account.
	// Ownership is immutable after insertion.
	MerchantID uuid.UUID `json:"merchant_id" db:"merchant_id"`

	// AccountStatus is the current merchant platform-account lifecycle status.
	AccountStatus MerchantAccountStatus `json:"account_status" db:"account_status"`

	// OnboardedAt records when merchant onboarding first completed.
	//
	// It is nil before first activation and is never overwritten after being
	// established.
	OnboardedAt *time.Time `json:"onboarded_at,omitempty" db:"onboarded_at"`

	// InitialPlanID records the merchant program plan associated with initial
	// onboarding.
	//
	// This is historical onboarding context only. Current subscription,
	// entitlement, billing, and access state belong to their respective
	// merchant program domains.
	InitialPlanID *uuid.UUID `json:"initial_plan_id,omitempty" db:"initial_plan_id"`

	CreatedAt time.Time  `json:"created_at" db:"created_at"`
	UpdatedAt time.Time  `json:"updated_at" db:"updated_at"`
	DeletedAt *time.Time `json:"deleted_at,omitempty" db:"deleted_at"`
}

// MerchantAccountModel owns merchant-account persistence.
type MerchantAccountModel struct {
	DB     *pgxpool.Pool
	Logger *logging.Logger
}

// NormalizeMerchantAccountStatus canonicalizes a merchant account status.
func NormalizeMerchantAccountStatus(status MerchantAccountStatus) MerchantAccountStatus {
	return MerchantAccountStatus(
		strings.ToLower(strings.TrimSpace(string(status))),
	)
}

// IsValidMerchantAccountStatus reports whether status is supported by the
// merchant account lifecycle.
func IsValidMerchantAccountStatus(status MerchantAccountStatus) bool {
	switch NormalizeMerchantAccountStatus(status) {
	case MerchantAccountStatusPending,
		MerchantAccountStatusActive,
		MerchantAccountStatusSuspended,
		MerchantAccountStatusClosed:
		return true
	default:
		return false
	}
}

func scanMerchantAccount(row pgx.Row, account *MerchantAccount) error {
	return row.Scan(
		&account.ID,
		&account.MerchantID,
		&account.AccountStatus,
		&account.OnboardedAt,
		&account.InitialPlanID,
		&account.CreatedAt,
		&account.UpdatedAt,
		&account.DeletedAt,
	)
}

func scanMerchantAccountRows(rows pgx.Rows, account *MerchantAccount) error {
	return rows.Scan(
		&account.ID,
		&account.MerchantID,
		&account.AccountStatus,
		&account.OnboardedAt,
		&account.InitialPlanID,
		&account.CreatedAt,
		&account.UpdatedAt,
		&account.DeletedAt,
	)
}

func isMerchantAccountUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

func isMerchantAccountForeignKeyViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23503"
}

// Insert creates the canonical merchant account for merchantID.
//
// Merchant accounts are always inserted in pending status. Activation and
// onboarding completion are handled through Activate.
func (m MerchantAccountModel) Insert(
	ctx context.Context,
	merchantID uuid.UUID,
	initialPlanID *uuid.UUID,
) (*MerchantAccount, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.
		GetLoggerWithContextFromContext(ctx).
		WithFunctionName("InsertMerchantAccount")

	if merchantID == uuid.Nil {
		err := errors.New("merchant ID is required")
		logger.Error("Merchant account validation failed", err)
		return nil, err
	}

	if initialPlanID != nil && *initialPlanID == uuid.Nil {
		err := errors.New("initial merchant program plan ID cannot be nil UUID")
		logger.Error("Merchant account validation failed", err)
		return nil, err
	}

	account := &MerchantAccount{
		ID:            uuid.New(),
		MerchantID:    merchantID,
		AccountStatus: MerchantAccountStatusPending,
		InitialPlanID: initialPlanID,
	}

	query := `
		INSERT INTO merchant_accounts (
			id,
			merchant_id,
			account_status,
			initial_plan_id
		)
		VALUES ($1, $2, $3, $4)
		RETURNING
			onboarded_at,
			created_at,
			updated_at,
			deleted_at
	`

	err := m.DB.QueryRow(
		ctx,
		query,
		account.ID,
		account.MerchantID,
		account.AccountStatus,
		account.InitialPlanID,
	).Scan(
		&account.OnboardedAt,
		&account.CreatedAt,
		&account.UpdatedAt,
		&account.DeletedAt,
	)
	if err != nil {
		switch {
		case isMerchantAccountUniqueViolation(err):
			err = fmt.Errorf(
				"merchant account already exists for merchant %s: %w",
				merchantID,
				err,
			)

		case isMerchantAccountForeignKeyViolation(err):
			err = fmt.Errorf(
				"merchant account references a missing merchant or initial merchant program plan: %w",
				err,
			)
		}

		logger.Error(
			"Insert merchant account failed",
			err,
			"merchant_id", merchantID,
		)
		return nil, err
	}

	logger.Info(
		"Insert merchant account successful",
		"merchant_account_id", account.ID,
		"merchant_id", account.MerchantID,
	)

	return account, nil
}

// GetByID retrieves a non-deleted merchant account by its canonical account ID.
func (m MerchantAccountModel) GetByID(
	ctx context.Context,
	id uuid.UUID,
) (*MerchantAccount, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.
		GetLoggerWithContextFromContext(ctx).
		WithFunctionName("GetMerchantAccountByID")

	if id == uuid.Nil {
		err := errors.New("merchant account ID is required")
		logger.Error("Merchant account validation failed", err)
		return nil, err
	}

	query := `
		SELECT ` + merchantAccountSelectColumns + `
		FROM merchant_accounts
		WHERE id = $1
		  AND deleted_at IS NULL
	`

	var account MerchantAccount

	err := scanMerchantAccount(
		m.DB.QueryRow(ctx, query, id),
		&account,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}

		logger.Error(
			"Get merchant account by ID failed",
			err,
			"merchant_account_id", id,
		)
		return nil, err
	}

	return &account, nil
}

// GetByIDIncludingDeleted retrieves a merchant account regardless of its
// soft-delete state.
//
// This method is intended for controlled administrative, restoration, and
// diagnostic workflows.
func (m MerchantAccountModel) GetByIDIncludingDeleted(
	ctx context.Context,
	id uuid.UUID,
) (*MerchantAccount, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.
		GetLoggerWithContextFromContext(ctx).
		WithFunctionName("GetMerchantAccountByIDIncludingDeleted")

	if id == uuid.Nil {
		err := errors.New("merchant account ID is required")
		logger.Error("Merchant account validation failed", err)
		return nil, err
	}

	query := `
		SELECT ` + merchantAccountSelectColumns + `
		FROM merchant_accounts
		WHERE id = $1
	`

	var account MerchantAccount

	err := scanMerchantAccount(
		m.DB.QueryRow(ctx, query, id),
		&account,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}

		logger.Error(
			"Get merchant account including deleted failed",
			err,
			"merchant_account_id", id,
		)
		return nil, err
	}

	return &account, nil
}

// GetByMerchantID retrieves the non-deleted canonical account belonging to
// merchantID.
func (m MerchantAccountModel) GetByMerchantID(
	ctx context.Context,
	merchantID uuid.UUID,
) (*MerchantAccount, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.
		GetLoggerWithContextFromContext(ctx).
		WithFunctionName("GetMerchantAccountByMerchantID")

	if merchantID == uuid.Nil {
		err := errors.New("merchant ID is required")
		logger.Error("Merchant account validation failed", err)
		return nil, err
	}

	query := `
		SELECT ` + merchantAccountSelectColumns + `
		FROM merchant_accounts
		WHERE merchant_id = $1
		  AND deleted_at IS NULL
	`

	var account MerchantAccount

	err := scanMerchantAccount(
		m.DB.QueryRow(ctx, query, merchantID),
		&account,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}

		logger.Error(
			"Get merchant account by merchant ID failed",
			err,
			"merchant_id", merchantID,
		)
		return nil, err
	}

	return &account, nil
}

// GetByMerchantIDIncludingDeleted retrieves the canonical merchant account
// regardless of its soft-delete state.
func (m MerchantAccountModel) GetByMerchantIDIncludingDeleted(
	ctx context.Context,
	merchantID uuid.UUID,
) (*MerchantAccount, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.
		GetLoggerWithContextFromContext(ctx).
		WithFunctionName("GetMerchantAccountByMerchantIDIncludingDeleted")

	if merchantID == uuid.Nil {
		err := errors.New("merchant ID is required")
		logger.Error("Merchant account validation failed", err)
		return nil, err
	}

	query := `
		SELECT ` + merchantAccountSelectColumns + `
		FROM merchant_accounts
		WHERE merchant_id = $1
	`

	var account MerchantAccount

	err := scanMerchantAccount(
		m.DB.QueryRow(ctx, query, merchantID),
		&account,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}

		logger.Error(
			"Get merchant account by merchant ID including deleted failed",
			err,
			"merchant_id", merchantID,
		)
		return nil, err
	}

	return &account, nil
}

// ExistsByMerchantID reports whether the canonical merchant account row exists
// for merchantID, including when the row is soft-deleted.
//
// merchant_id has unconditional table-level uniqueness. A soft-deleted account
// therefore still occupies the canonical merchant-account relationship.
func (m MerchantAccountModel) ExistsByMerchantID(
	ctx context.Context,
	merchantID uuid.UUID,
) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.
		GetLoggerWithContextFromContext(ctx).
		WithFunctionName("ExistsMerchantAccountByMerchantID")

	if merchantID == uuid.Nil {
		err := errors.New("merchant ID is required")
		logger.Error("Merchant account validation failed", err)
		return false, err
	}

	query := `
		SELECT EXISTS (
			SELECT 1
			FROM merchant_accounts
			WHERE merchant_id = $1
		)
	`

	var exists bool

	if err := m.DB.QueryRow(ctx, query, merchantID).Scan(&exists); err != nil {
		logger.Error(
			"Merchant account existence check failed",
			err,
			"merchant_id", merchantID,
		)
		return false, err
	}

	return exists, nil
}

// GetAll retrieves merchant accounts using deterministic offset pagination.
func (m MerchantAccountModel) GetAll(
	ctx context.Context,
	includeDeleted bool,
	limit int,
	offset int,
) ([]*MerchantAccount, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.
		GetLoggerWithContextFromContext(ctx).
		WithFunctionName("GetAllMerchantAccounts")

	if limit < 1 || limit > 100 {
		err := errors.New("limit must be between 1 and 100")
		logger.Error("Merchant account validation failed", err)
		return nil, err
	}

	if offset < 0 {
		err := errors.New("offset must be non-negative")
		logger.Error("Merchant account validation failed", err)
		return nil, err
	}

	query := `
		SELECT ` + merchantAccountSelectColumns + `
		FROM merchant_accounts
	`

	if !includeDeleted {
		query += `
			WHERE deleted_at IS NULL
		`
	}

	query += `
		ORDER BY created_at ASC, id ASC
		LIMIT $1
		OFFSET $2
	`

	rows, err := m.DB.Query(ctx, query, limit, offset)
	if err != nil {
		logger.Error("Get all merchant accounts query failed", err)
		return nil, err
	}
	defer rows.Close()

	accounts := make([]*MerchantAccount, 0)

	for rows.Next() {
		var account MerchantAccount

		if err := scanMerchantAccountRows(rows, &account); err != nil {
			logger.Error("Merchant account row scan failed", err)
			return nil, err
		}

		accounts = append(accounts, &account)
	}

	if err := rows.Err(); err != nil {
		logger.Error("Merchant account row iteration failed", err)
		return nil, err
	}

	return accounts, nil
}

// Activate transitions a merchant account from pending or suspended to active.
//
// Activation is retry-safe when the account is already active. onboarded_at is
// established only on first activation and is never overwritten.
func (m MerchantAccountModel) Activate(
	ctx context.Context,
	id uuid.UUID,
) (*MerchantAccount, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.
		GetLoggerWithContextFromContext(ctx).
		WithFunctionName("ActivateMerchantAccount")

	if id == uuid.Nil {
		err := errors.New("merchant account ID is required")
		logger.Error("Merchant account validation failed", err)
		return nil, err
	}

	query := `
		UPDATE merchant_accounts
		SET
			account_status = $1,
			onboarded_at = COALESCE(onboarded_at, NOW()),
			updated_at = CASE
				WHEN account_status <> $1 OR onboarded_at IS NULL
					THEN NOW()
				ELSE updated_at
			END
		WHERE id = $2
		  AND deleted_at IS NULL
		  AND account_status IN ($1, $3, $4)
		RETURNING ` + merchantAccountSelectColumns

	var account MerchantAccount

	err := scanMerchantAccount(
		m.DB.QueryRow(
			ctx,
			query,
			MerchantAccountStatusActive,
			id,
			MerchantAccountStatusPending,
			MerchantAccountStatusSuspended,
		),
		&account,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, m.lifecycleConflict(ctx, id, "activate")
		}

		logger.Error(
			"Activate merchant account failed",
			err,
			"merchant_account_id", id,
		)
		return nil, err
	}

	logger.Info(
		"Activate merchant account successful",
		"merchant_account_id", account.ID,
		"merchant_id", account.MerchantID,
	)

	return &account, nil
}

// Suspend transitions an active merchant account to suspended.
//
// Suspension is retry-safe when the account is already suspended.
func (m MerchantAccountModel) Suspend(
	ctx context.Context,
	id uuid.UUID,
) (*MerchantAccount, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.
		GetLoggerWithContextFromContext(ctx).
		WithFunctionName("SuspendMerchantAccount")

	if id == uuid.Nil {
		err := errors.New("merchant account ID is required")
		logger.Error("Merchant account validation failed", err)
		return nil, err
	}

	query := `
		UPDATE merchant_accounts
		SET
			account_status = $1,
			updated_at = CASE
				WHEN account_status <> $1
					THEN NOW()
				ELSE updated_at
			END
		WHERE id = $2
		  AND deleted_at IS NULL
		  AND account_status IN ($1, $3)
		RETURNING ` + merchantAccountSelectColumns

	var account MerchantAccount

	err := scanMerchantAccount(
		m.DB.QueryRow(
			ctx,
			query,
			MerchantAccountStatusSuspended,
			id,
			MerchantAccountStatusActive,
		),
		&account,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, m.lifecycleConflict(ctx, id, "suspend")
		}

		logger.Error(
			"Suspend merchant account failed",
			err,
			"merchant_account_id", id,
		)
		return nil, err
	}

	logger.Info(
		"Suspend merchant account successful",
		"merchant_account_id", account.ID,
		"merchant_id", account.MerchantID,
	)

	return &account, nil
}

// Close transitions a pending, active, or suspended merchant account to
// closed.
//
// Closure is retry-safe when the account is already closed. It does not
// soft-delete the account.
func (m MerchantAccountModel) Close(
	ctx context.Context,
	id uuid.UUID,
) (*MerchantAccount, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.
		GetLoggerWithContextFromContext(ctx).
		WithFunctionName("CloseMerchantAccount")

	if id == uuid.Nil {
		err := errors.New("merchant account ID is required")
		logger.Error("Merchant account validation failed", err)
		return nil, err
	}

	query := `
		UPDATE merchant_accounts
		SET
			account_status = $1,
			updated_at = CASE
				WHEN account_status <> $1
					THEN NOW()
				ELSE updated_at
			END
		WHERE id = $2
		  AND deleted_at IS NULL
		  AND account_status IN ($1, $3, $4, $5)
		RETURNING ` + merchantAccountSelectColumns

	var account MerchantAccount

	err := scanMerchantAccount(
		m.DB.QueryRow(
			ctx,
			query,
			MerchantAccountStatusClosed,
			id,
			MerchantAccountStatusPending,
			MerchantAccountStatusActive,
			MerchantAccountStatusSuspended,
		),
		&account,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, m.lifecycleConflict(ctx, id, "close")
		}

		logger.Error(
			"Close merchant account failed",
			err,
			"merchant_account_id", id,
		)
		return nil, err
	}

	logger.Info(
		"Close merchant account successful",
		"merchant_account_id", account.ID,
		"merchant_id", account.MerchantID,
	)

	return &account, nil
}

// SoftDelete logically removes a closed merchant account from ordinary reads.
//
// Closure is an explicit precondition. Soft deletion does not alter the
// account's closed status.
func (m MerchantAccountModel) SoftDelete(
	ctx context.Context,
	id uuid.UUID,
) error {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.
		GetLoggerWithContextFromContext(ctx).
		WithFunctionName("SoftDeleteMerchantAccount")

	if id == uuid.Nil {
		err := errors.New("merchant account ID is required")
		logger.Error("Merchant account validation failed", err)
		return err
	}

	query := `
		UPDATE merchant_accounts
		SET
			deleted_at = COALESCE(deleted_at, NOW()),
			updated_at = CASE
				WHEN deleted_at IS NULL
					THEN NOW()
				ELSE updated_at
			END
		WHERE id = $1
		  AND account_status = $2
		RETURNING deleted_at
	`

	var deletedAt time.Time

	err := m.DB.QueryRow(
		ctx,
		query,
		id,
		MerchantAccountStatusClosed,
	).Scan(&deletedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return m.lifecycleConflict(ctx, id, "soft-delete")
		}

		logger.Error(
			"Soft delete merchant account failed",
			err,
			"merchant_account_id", id,
		)
		return err
	}

	logger.Info(
		"Soft delete merchant account successful",
		"merchant_account_id", id,
		"deleted_at", deletedAt,
	)

	return nil
}

// Restore reverses soft deletion without changing account_status.
//
// A restored merchant account remains closed and requires an explicit,
// separately governed reopening policy before it could ever become active
// again.
func (m MerchantAccountModel) Restore(
	ctx context.Context,
	id uuid.UUID,
) error {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.
		GetLoggerWithContextFromContext(ctx).
		WithFunctionName("RestoreMerchantAccount")

	if id == uuid.Nil {
		err := errors.New("merchant account ID is required")
		logger.Error("Merchant account validation failed", err)
		return err
	}

	query := `
		UPDATE merchant_accounts
		SET
			deleted_at = NULL,
			updated_at = CASE
				WHEN deleted_at IS NOT NULL
					THEN NOW()
				ELSE updated_at
			END
		WHERE id = $1
		  AND account_status = $2
		RETURNING deleted_at
	`

	var deletedAt *time.Time

	err := m.DB.QueryRow(
		ctx,
		query,
		id,
		MerchantAccountStatusClosed,
	).Scan(&deletedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return m.lifecycleConflict(ctx, id, "restore")
		}

		logger.Error(
			"Restore merchant account failed",
			err,
			"merchant_account_id", id,
		)
		return err
	}

	logger.Info(
		"Restore merchant account successful",
		"merchant_account_id", id,
	)

	return nil
}

// HardDelete permanently removes a soft-deleted merchant account.
//
// Permanent deletion is an exceptional administrative operation and requires
// the account to have already completed the explicit close and soft-delete
// lifecycle.
func (m MerchantAccountModel) HardDelete(
	ctx context.Context,
	id uuid.UUID,
) error {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.
		GetLoggerWithContextFromContext(ctx).
		WithFunctionName("HardDeleteMerchantAccount")

	if id == uuid.Nil {
		err := errors.New("merchant account ID is required")
		logger.Error("Merchant account validation failed", err)
		return err
	}

	query := `
		DELETE FROM merchant_accounts
		WHERE id = $1
		  AND account_status = $2
		  AND deleted_at IS NOT NULL
		RETURNING id
	`

	var deletedID uuid.UUID

	err := m.DB.QueryRow(
		ctx,
		query,
		id,
		MerchantAccountStatusClosed,
	).Scan(&deletedID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			err = fmt.Errorf(
				"merchant account %s must be closed and soft-deleted before permanent deletion",
				id,
			)
		}

		logger.Error(
			"Hard delete merchant account failed",
			err,
			"merchant_account_id", id,
		)
		return err
	}

	logger.Info(
		"Hard delete merchant account successful",
		"merchant_account_id", deletedID,
	)

	return nil
}

// lifecycleConflict classifies why an atomic lifecycle mutation could not be
// applied.
//
// This diagnostic read never participates in mutation correctness. The
// conditional UPDATE or DELETE remains the sole atomic lifecycle authority.
func (m MerchantAccountModel) lifecycleConflict(
	ctx context.Context,
	id uuid.UUID,
	operation string,
) error {
	var status MerchantAccountStatus
	var deletedAt *time.Time

	query := `
		SELECT
			account_status,
			deleted_at
		FROM merchant_accounts
		WHERE id = $1
	`

	err := m.DB.QueryRow(ctx, query, id).Scan(
		&status,
		&deletedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf(
				"merchant account %s does not exist",
				id,
			)
		}

		return fmt.Errorf(
			"merchant account %s %s operation could not be classified: %w",
			id,
			operation,
			err,
		)
	}

	switch operation {
	case "activate":
		if deletedAt != nil {
			return fmt.Errorf(
				"merchant account %s is soft-deleted and cannot be activated",
				id,
			)
		}

		return fmt.Errorf(
			"merchant account %s cannot be activated from status %q",
			id,
			status,
		)

	case "suspend":
		if deletedAt != nil {
			return fmt.Errorf(
				"merchant account %s is soft-deleted and cannot be suspended",
				id,
			)
		}

		return fmt.Errorf(
			"merchant account %s cannot be suspended from status %q",
			id,
			status,
		)

	case "close":
		if deletedAt != nil {
			return fmt.Errorf(
				"merchant account %s is soft-deleted and cannot be closed",
				id,
			)
		}

		return fmt.Errorf(
			"merchant account %s cannot be closed from status %q",
			id,
			status,
		)

	case "soft-delete":
		return fmt.Errorf(
			"merchant account %s must be closed before it can be soft-deleted; current status is %q",
			id,
			status,
		)

	case "restore":
		if status != MerchantAccountStatusClosed {
			return fmt.Errorf(
				"merchant account %s cannot be restored from status %q",
				id,
				status,
			)
		}

		return fmt.Errorf(
			"merchant account %s exists but the restore operation could not be applied",
			id,
		)

	default:
		return fmt.Errorf(
			"merchant account %s cannot perform operation %q from status %q",
			id,
			operation,
			status,
		)
	}
}
