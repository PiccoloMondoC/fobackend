// Package data provides models and database access methods for merchant
// platform credit accounts.
//
// sdworkspace/sdbackend/internal/data/merchant_platform_credit_accounts.go
//
// GTM:
//
//	Layer: 2.4.b Commerce Architecture / Monetization Layer
//	Release Class: SPINE
//	Reason:
//	  merchant_platform_credit_accounts records platform-issued commercial
//	  credit granted to merchants. These credits may later be applied by
//	  billing orchestration against obligations that an independently
//	  governed eligibility capability has determined to be credit-eligible.
//
//	  This domain affects the amount commercially owed before invoice
//	  settlement. It therefore belongs to Commerce Architecture, not Merchant
//	  Payments Architecture.
//
// Domain Boundary:
//
//	A merchant platform credit account is not:
//
//	  - merchant-held money;
//	  - a treasury balance;
//	  - a wallet;
//	  - a deposit;
//	  - escrow;
//	  - stored value;
//	  - a payment method; or
//	  - a payment-provider balance.
//
//	This file records credit capacity only. It does not determine:
//
//	  - which merchants receive credit;
//	  - how much credit they receive;
//	  - which fee types are credit-eligible;
//	  - the order in which multiple accounts are consumed;
//	  - whether a promotion is commercially enabled;
//	  - whether subscriptions or fees are enabled; or
//	  - whether an actor is authorized to grant or cancel credit.
//
//	Those decisions belong to Administration-governed configuration,
//	commercial-promotion, credit-eligibility, service-orchestration, and
//	authorization capabilities.
//
// Monetary Boundary:
//
//	Amounts are persisted as PostgreSQL NUMERIC(19,4) values and represented
//	at the Go boundary as canonical decimal strings. Monetary values are
//	never converted through floating point.
//
// SPINE Rule:
//
//	Keep compiling.
//	Keep production-ready.
//	Preserve 0 <= remaining_amount <= original_amount.
//	Preserve atomic, guarded credit consumption.
//	Preserve terminal lifecycle history.
//	Preserve database-time evaluation of validity windows.
//	Preserve currency isolation.
//	Never implement commercial eligibility policy in this file.
//	Never treat platform credit as merchant-held funds.
//	Never expose destructive deletion.
//	Block deployment if this file breaks build, monetary integrity,
//	concurrency safety, lifecycle integrity, or billing readiness.
package data

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/PiccoloMondoC/sdworkspace/sdbackend/internal/logging"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// -----------------------------------------------------------------------------
// Controlled vocabulary
// -----------------------------------------------------------------------------

// MerchantPlatformCreditAccountStatus is the persisted lifecycle state of a
// merchant platform credit account.
//
// The lifecycle is one-way:
//
//	active -> exhausted
//	active -> expired
//	active -> cancelled
//
// Exhausted, expired, and cancelled are terminal. Additional credit must be
// represented by a new grant rather than by resurrecting a terminal account.
type MerchantPlatformCreditAccountStatus string

const (
	// MerchantPlatformCreditAccountStatusActive indicates that the credit
	// account has not entered a terminal lifecycle state.
	//
	// Active status alone does not make credit currently usable. starts_at,
	// expires_at, remaining_amount, and currency must also satisfy the guarded
	// consumption conditions.
	MerchantPlatformCreditAccountStatusActive MerchantPlatformCreditAccountStatus = "active"

	// MerchantPlatformCreditAccountStatusExhausted indicates that atomic
	// consumption reduced remaining_amount to exactly zero.
	MerchantPlatformCreditAccountStatusExhausted MerchantPlatformCreditAccountStatus = "exhausted"

	// MerchantPlatformCreditAccountStatusExpired indicates that the account
	// passed expires_at before its remaining credit was completely consumed.
	MerchantPlatformCreditAccountStatusExpired MerchantPlatformCreditAccountStatus = "expired"

	// MerchantPlatformCreditAccountStatusCancelled indicates an explicit
	// lifecycle cancellation. Cancellation preserves remaining_amount as
	// historical truth and does not imply refundability or cash value.
	MerchantPlatformCreditAccountStatusCancelled MerchantPlatformCreditAccountStatus = "cancelled"
)

// -----------------------------------------------------------------------------
// Structural bounds
// -----------------------------------------------------------------------------

const (
	merchantPlatformCreditAccountMaxListLimit        = 100
	merchantPlatformCreditAccountMaxBatchLimit       = 1000
	merchantPlatformCreditAccountMaxSourceCodeLength = 128
	merchantPlatformCreditAccountMaxNoteLength       = 2000
)

// NUMERIC(19,4) permits at most 15 digits before the decimal point and four
// after it. The pattern intentionally rejects signs, exponent notation,
// whitespace, rational notation, and more than four fractional digits.
//
// Zero is syntactically accepted by this pattern and rejected separately when
// a strictly positive amount is required.
var merchantPlatformCreditAmountPattern = regexp.MustCompile(
	`^(?:0|[1-9][0-9]{0,14})(?:\.[0-9]{1,4})?$`,
)

// Currency is a structural three-letter uppercase identifier in this model.
// The model intentionally does not embed a list of commercially enabled
// currencies; that belongs to configuration and higher-layer governance.
var merchantPlatformCreditCurrencyPattern = regexp.MustCompile(`^[A-Z]{3}$`)

// merchantPlatformCreditAccountSelectColumns centralizes SQL projection order
// so every scan path remains aligned with MerchantPlatformCreditAccount.
const merchantPlatformCreditAccountSelectColumns = `
	id,
	merchant_id,
	status,
	original_amount,
	remaining_amount,
	currency,
	starts_at,
	expires_at,
	source_code,
	note,
	created_at,
	updated_at
`

// -----------------------------------------------------------------------------
// Entity and model
// -----------------------------------------------------------------------------

// MerchantPlatformCreditAccount represents one platform-issued commercial
// credit grant.
//
// OriginalAmount is immutable after creation. RemainingAmount may change only
// through the guarded Consume or ConsumeTx mutation.
//
// SourceCode and Note are descriptive metadata. They carry no executable
// commercial eligibility, pricing, entitlement, or fee-policy behavior.
type MerchantPlatformCreditAccount struct {
	ID         uuid.UUID                           `json:"id" db:"id"`
	MerchantID uuid.UUID                           `json:"merchant_id" db:"merchant_id"`
	Status     MerchantPlatformCreditAccountStatus `json:"status" db:"status"`

	OriginalAmount  string `json:"original_amount" db:"original_amount"`
	RemainingAmount string `json:"remaining_amount" db:"remaining_amount"`
	Currency        string `json:"currency" db:"currency"`

	StartsAt  time.Time  `json:"starts_at" db:"starts_at"`
	ExpiresAt *time.Time `json:"expires_at,omitempty" db:"expires_at"`

	SourceCode *string `json:"source_code,omitempty" db:"source_code"`
	Note       *string `json:"note,omitempty" db:"note"`

	CreatedAt time.Time `json:"created_at" db:"created_at"`
	UpdatedAt time.Time `json:"updated_at" db:"updated_at"`
}

// MerchantPlatformCreditAccountModel owns persistence for merchant platform
// credit accounts.
type MerchantPlatformCreditAccountModel struct {
	DB     *pgxpool.Pool
	Logger *logging.Logger
}

// merchantPlatformCreditAccountQuerier is the minimal query surface required
// by transaction-aware consumption.
//
// Both *pgxpool.Pool and pgx.Tx satisfy this interface. Do not expand it with
// unrelated operations.
type merchantPlatformCreditAccountQuerier interface {
	QueryRow(ctx context.Context, sql string, args ...interface{}) pgx.Row
}

// scanMerchantPlatformCreditAccount scans the canonical projection into
// account. scannableRow is the package-wide scan contract shared by pgx.Row
// and pgx.Rows.
func scanMerchantPlatformCreditAccount(
	row scannableRow,
	account *MerchantPlatformCreditAccount,
) error {
	return row.Scan(
		&account.ID,
		&account.MerchantID,
		&account.Status,
		&account.OriginalAmount,
		&account.RemainingAmount,
		&account.Currency,
		&account.StartsAt,
		&account.ExpiresAt,
		&account.SourceCode,
		&account.Note,
		&account.CreatedAt,
		&account.UpdatedAt,
	)
}

// -----------------------------------------------------------------------------
// Normalization and validation
// -----------------------------------------------------------------------------

// NormalizeMerchantPlatformCreditAccountStatus returns the canonical
// identifier form of status.
func NormalizeMerchantPlatformCreditAccountStatus(
	status MerchantPlatformCreditAccountStatus,
) MerchantPlatformCreditAccountStatus {
	return MerchantPlatformCreditAccountStatus(
		normalizeIdentifier(string(status)),
	)
}

// IsValidMerchantPlatformCreditAccountStatus reports whether status belongs
// to the persisted controlled vocabulary.
func IsValidMerchantPlatformCreditAccountStatus(
	status MerchantPlatformCreditAccountStatus,
) bool {
	switch NormalizeMerchantPlatformCreditAccountStatus(status) {
	case MerchantPlatformCreditAccountStatusActive,
		MerchantPlatformCreditAccountStatusExhausted,
		MerchantPlatformCreditAccountStatusExpired,
		MerchantPlatformCreditAccountStatusCancelled:
		return true
	default:
		return false
	}
}

// normalizeMerchantPlatformCreditCurrency returns the canonical structural
// currency identifier.
func normalizeMerchantPlatformCreditCurrency(currency string) string {
	return strings.ToUpper(strings.TrimSpace(currency))
}

// validateMerchantPlatformCreditCurrency validates structural currency shape.
//
// It deliberately does not maintain a hard-coded enabled-currency catalog.
func validateMerchantPlatformCreditCurrency(currency string) error {
	if currency == "" {
		return errors.New("merchant platform credit account currency is required")
	}
	if !merchantPlatformCreditCurrencyPattern.MatchString(currency) {
		return fmt.Errorf(
			"merchant platform credit account currency must be a three-letter uppercase identifier: %q",
			currency,
		)
	}
	return nil
}

// normalizeMerchantPlatformCreditAmount trims a decimal amount without
// performing lossy numeric conversion.
func normalizeMerchantPlatformCreditAmount(amount string) string {
	return strings.TrimSpace(amount)
}

// isZeroMerchantPlatformCreditAmount reports whether a syntactically valid
// decimal amount is numerically zero.
func isZeroMerchantPlatformCreditAmount(amount string) bool {
	for _, r := range amount {
		switch r {
		case '0', '.':
			continue
		default:
			return false
		}
	}
	return true
}

// validatePositiveMerchantPlatformCreditAmount validates a strictly positive
// canonical decimal compatible with PostgreSQL NUMERIC(19,4).
func validatePositiveMerchantPlatformCreditAmount(amount string) error {
	if amount == "" {
		return errors.New("merchant platform credit amount is required")
	}
	if !merchantPlatformCreditAmountPattern.MatchString(amount) {
		return fmt.Errorf(
			"merchant platform credit amount must be a positive decimal compatible with NUMERIC(19,4): %q",
			amount,
		)
	}
	if isZeroMerchantPlatformCreditAmount(amount) {
		return errors.New("merchant platform credit amount must be greater than zero")
	}
	return nil
}

// validateMerchantPlatformCreditOptionalText validates a normalized optional
// metadata value against its structural persistence limit.
//
// A nil value is valid and represents absence. Callers must normalize optional
// strings before invoking this function.
func validateMerchantPlatformCreditOptionalText(
	value *string,
	fieldName string,
	maxLength int,
) error {
	if value == nil {
		return nil
	}
	if len([]rune(*value)) > maxLength {
		return fmt.Errorf(
			"merchant platform credit account %s must not exceed %d characters",
			fieldName,
			maxLength,
		)
	}
	return nil
}

// normalizeMerchantPlatformCreditAccount canonicalizes caller-owned,
// non-database-owned fields.
func normalizeMerchantPlatformCreditAccount(
	account *MerchantPlatformCreditAccount,
) {
	account.Currency = normalizeMerchantPlatformCreditCurrency(account.Currency)
	account.OriginalAmount = normalizeMerchantPlatformCreditAmount(account.OriginalAmount)
	account.SourceCode = normalizeOptionalString(account.SourceCode)
	account.Note = normalizeOptionalString(account.Note)
}

// validateMerchantPlatformCreditAccountForInsert validates the safe creation
// contract.
//
// Status and RemainingAmount are not accepted as caller-controlled creation
// values. Insert initializes them to active and original_amount respectively.
func validateMerchantPlatformCreditAccountForInsert(
	account *MerchantPlatformCreditAccount,
) error {
	if account == nil {
		return errors.New("merchant platform credit account is required")
	}

	normalizeMerchantPlatformCreditAccount(account)

	if account.MerchantID == uuid.Nil {
		return errors.New("merchant platform credit account merchant_id is required")
	}

	if err := validatePositiveMerchantPlatformCreditAmount(account.OriginalAmount); err != nil {
		return err
	}

	if err := validateMerchantPlatformCreditCurrency(account.Currency); err != nil {
		return err
	}

	if err := validateMerchantPlatformCreditOptionalText(
		account.SourceCode,
		"source_code",
		merchantPlatformCreditAccountMaxSourceCodeLength,
	); err != nil {
		return err
	}

	if err := validateMerchantPlatformCreditOptionalText(
		account.Note,
		"note",
		merchantPlatformCreditAccountMaxNoteLength,
	); err != nil {
		return err
	}

	if account.ExpiresAt != nil &&
		!account.StartsAt.IsZero() &&
		!account.ExpiresAt.After(account.StartsAt) {
		return errors.New(
			"merchant platform credit account expires_at must be after starts_at",
		)
	}

	return nil
}

// classifyMerchantPlatformCreditAccountWriteError translates persistence
// constraint failures into stable data-layer domain errors.
func classifyMerchantPlatformCreditAccountWriteError(err error) error {
	switch {
	case IsForeignKeyViolation(err):
		return ErrMerchantNotFound
	case IsCheckViolation(err), IsNotNullViolation(err):
		return ErrMerchantPlatformCreditAccountInvalidState
	default:
		return err
	}
}

// -----------------------------------------------------------------------------
// Creation
// -----------------------------------------------------------------------------

// Insert creates a new merchant platform credit account.
//
// Creation invariants:
//
//   - ID is generated when absent.
//   - status is always initialized to active.
//   - remaining_amount is always initialized to original_amount.
//   - starts_at uses database NOW() when omitted.
//   - original_amount, merchant ownership, and currency are immutable after
//     creation through this model.
//
// Insert does not decide whether the merchant qualifies for credit or whether
// the amount is commercially appropriate.
func (m *MerchantPlatformCreditAccountModel) Insert(
	ctx context.Context,
	account *MerchantPlatformCreditAccount,
) error {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.
		GetLoggerWithContextFromContext(ctx).
		WithFunctionName("InsertMerchantPlatformCreditAccount")

	if err := validateMerchantPlatformCreditAccountForInsert(account); err != nil {
		logger.Error("validation failed", err)
		return err
	}

	if account.ID == uuid.Nil {
		account.ID = uuid.New()
	}

	account.Status = MerchantPlatformCreditAccountStatusActive
	account.RemainingAmount = account.OriginalAmount

	var startsAt *time.Time
	if !account.StartsAt.IsZero() {
		startsAt = &account.StartsAt
	}

	const query = `
		INSERT INTO merchant_platform_credit_accounts (
			id,
			merchant_id,
			status,
			original_amount,
			remaining_amount,
			currency,
			starts_at,
			expires_at,
			source_code,
			note
		)
		VALUES (
			$1,
			$2,
			'active',
			$3::numeric,
			$3::numeric,
			$4,
			COALESCE($5::timestamptz, NOW()),
			$6,
			$7,
			$8
		)
		RETURNING ` + merchantPlatformCreditAccountSelectColumns

	err := scanMerchantPlatformCreditAccount(
		m.DB.QueryRow(
			ctx,
			query,
			account.ID,
			account.MerchantID,
			account.OriginalAmount,
			account.Currency,
			startsAt,
			account.ExpiresAt,
			account.SourceCode,
			account.Note,
		),
		account,
	)
	if err != nil {
		err = classifyMerchantPlatformCreditAccountWriteError(err)
		logger.Error(
			"insert merchant platform credit account failed",
			err,
			"merchant_platform_credit_account_id", account.ID,
			"merchant_id", account.MerchantID,
		)
		return err
	}

	logger.Info(
		"insert merchant platform credit account successful",
		"merchant_platform_credit_account_id", account.ID,
		"merchant_id", account.MerchantID,
		"status", account.Status,
		"currency", account.Currency,
	)

	return nil
}

// -----------------------------------------------------------------------------
// Reads
// -----------------------------------------------------------------------------

// GetByID retrieves a merchant platform credit account by canonical ID.
//
// This is an unrestricted persistence lookup intended for privileged internal
// or administrative use. Merchant-owned boundaries must use
// GetByIDForMerchant so merchant ownership remains part of the SQL predicate.
//
// Terminal accounts remain readable. Absence is a normal read result and
// returns (nil, nil).
func (m *MerchantPlatformCreditAccountModel) GetByID(
	ctx context.Context,
	id uuid.UUID,
) (*MerchantPlatformCreditAccount, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.
		GetLoggerWithContextFromContext(ctx).
		WithFunctionName("GetMerchantPlatformCreditAccountByID")

	if id == uuid.Nil {
		err := errors.New("merchant platform credit account id is required")
		logger.Error("validation failed", err)
		return nil, err
	}

	const query = `
		SELECT ` + merchantPlatformCreditAccountSelectColumns + `
		FROM merchant_platform_credit_accounts
		WHERE id = $1
	`

	var account MerchantPlatformCreditAccount
	err := scanMerchantPlatformCreditAccount(
		m.DB.QueryRow(ctx, query, id),
		&account,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}

		logger.Error(
			"get merchant platform credit account by id failed",
			err,
			"merchant_platform_credit_account_id", id,
		)
		return nil, err
	}

	return &account, nil
}

// GetByIDForMerchant retrieves an account only when it belongs to merchantID.
//
// This method provides an ownership-safe persistence primitive for future
// merchant-facing boundaries. Authorization still belongs above the data
// layer.
func (m *MerchantPlatformCreditAccountModel) GetByIDForMerchant(
	ctx context.Context,
	merchantID uuid.UUID,
	id uuid.UUID,
) (*MerchantPlatformCreditAccount, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.
		GetLoggerWithContextFromContext(ctx).
		WithFunctionName("GetMerchantPlatformCreditAccountByIDForMerchant")

	if merchantID == uuid.Nil || id == uuid.Nil {
		err := errors.New(
			"merchant id and merchant platform credit account id are required",
		)
		logger.Error("validation failed", err)
		return nil, err
	}

	const query = `
		SELECT ` + merchantPlatformCreditAccountSelectColumns + `
		FROM merchant_platform_credit_accounts
		WHERE id = $1
		  AND merchant_id = $2
	`

	var account MerchantPlatformCreditAccount
	err := scanMerchantPlatformCreditAccount(
		m.DB.QueryRow(ctx, query, id, merchantID),
		&account,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}

		logger.Error(
			"get merchant platform credit account by id for merchant failed",
			err,
			"merchant_platform_credit_account_id", id,
			"merchant_id", merchantID,
		)
		return nil, err
	}

	return &account, nil
}

// ListByMerchant returns all lifecycle states for merchantID using bounded
// offset pagination, newest first.
func (m *MerchantPlatformCreditAccountModel) ListByMerchant(
	ctx context.Context,
	merchantID uuid.UUID,
	limit int,
	offset int,
) ([]*MerchantPlatformCreditAccount, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.
		GetLoggerWithContextFromContext(ctx).
		WithFunctionName("ListMerchantPlatformCreditAccountsByMerchant")

	if merchantID == uuid.Nil {
		err := errors.New("merchant id is required")
		logger.Error("validation failed", err)
		return nil, err
	}
	if limit <= 0 || limit > merchantPlatformCreditAccountMaxListLimit {
		err := fmt.Errorf(
			"limit must be between 1 and %d",
			merchantPlatformCreditAccountMaxListLimit,
		)
		logger.Error("validation failed", err)
		return nil, err
	}
	if offset < 0 {
		err := errors.New("offset must be non-negative")
		logger.Error("validation failed", err)
		return nil, err
	}

	const query = `
		SELECT ` + merchantPlatformCreditAccountSelectColumns + `
		FROM merchant_platform_credit_accounts
		WHERE merchant_id = $1
		ORDER BY created_at DESC, id DESC
		LIMIT $2
		OFFSET $3
	`

	rows, err := m.DB.Query(ctx, query, merchantID, limit, offset)
	if err != nil {
		logger.Error(
			"list merchant platform credit accounts by merchant failed",
			err,
			"merchant_id", merchantID,
		)
		return nil, err
	}
	defer rows.Close()

	accounts := make([]*MerchantPlatformCreditAccount, 0)

	for rows.Next() {
		var account MerchantPlatformCreditAccount
		if err := scanMerchantPlatformCreditAccount(rows, &account); err != nil {
			logger.Error(
				"scan merchant platform credit account failed",
				err,
				"merchant_id", merchantID,
			)
			return nil, err
		}

		accounts = append(accounts, &account)
	}

	if err := rows.Err(); err != nil {
		logger.Error(
			"iterate merchant platform credit accounts failed",
			err,
			"merchant_id", merchantID,
		)
		return nil, err
	}

	return accounts, nil
}

// ListCurrentlyUsableByMerchant returns accounts that are currently eligible
// for guarded consumption according to engineering invariants:
//
//   - merchant ownership;
//   - matching currency;
//   - active persisted status;
//   - positive remaining balance;
//   - starts_at reached according to database time; and
//   - expires_at not reached according to database time.
//
// Results are ordered by soonest expiration first, with non-expiring accounts
// last, then by oldest creation time. This is deterministic availability
// ordering only. The method does not decide that callers must consume credits
// in this order.
//
// limit is caller-supplied so operational pagination policy remains outside
// this model.
func (m *MerchantPlatformCreditAccountModel) ListCurrentlyUsableByMerchant(
	ctx context.Context,
	merchantID uuid.UUID,
	currency string,
	limit int,
) ([]*MerchantPlatformCreditAccount, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.
		GetLoggerWithContextFromContext(ctx).
		WithFunctionName("ListCurrentlyUsableMerchantPlatformCreditAccounts")

	if merchantID == uuid.Nil {
		err := errors.New("merchant id is required")
		logger.Error("validation failed", err)
		return nil, err
	}

	currency = normalizeMerchantPlatformCreditCurrency(currency)
	if err := validateMerchantPlatformCreditCurrency(currency); err != nil {
		logger.Error("validation failed", err)
		return nil, err
	}

	if limit <= 0 || limit > merchantPlatformCreditAccountMaxListLimit {
		err := fmt.Errorf(
			"limit must be between 1 and %d",
			merchantPlatformCreditAccountMaxListLimit,
		)
		logger.Error("validation failed", err)
		return nil, err
	}

	const query = `
		SELECT ` + merchantPlatformCreditAccountSelectColumns + `
		FROM merchant_platform_credit_accounts
		WHERE merchant_id = $1
		  AND currency = $2
		  AND status = 'active'
		  AND remaining_amount > 0
		  AND starts_at <= NOW()
		  AND (
				expires_at IS NULL
				OR expires_at > NOW()
		  )
		ORDER BY
			expires_at ASC NULLS LAST,
			created_at ASC,
			id ASC
		LIMIT $3
	`

	rows, err := m.DB.Query(ctx, query, merchantID, currency, limit)
	if err != nil {
		logger.Error(
			"list currently usable merchant platform credit accounts failed",
			err,
			"merchant_id", merchantID,
			"currency", currency,
		)
		return nil, err
	}
	defer rows.Close()

	accounts := make([]*MerchantPlatformCreditAccount, 0)

	for rows.Next() {
		var account MerchantPlatformCreditAccount
		if err := scanMerchantPlatformCreditAccount(rows, &account); err != nil {
			logger.Error(
				"scan currently usable merchant platform credit account failed",
				err,
				"merchant_id", merchantID,
			)
			return nil, err
		}

		accounts = append(accounts, &account)
	}

	if err := rows.Err(); err != nil {
		logger.Error(
			"iterate currently usable merchant platform credit accounts failed",
			err,
			"merchant_id", merchantID,
		)
		return nil, err
	}

	return accounts, nil
}

// -----------------------------------------------------------------------------
// Descriptive metadata
// -----------------------------------------------------------------------------

// UpdateDescriptiveFields updates source_code and note without changing
// ownership, amount, currency, validity window, or lifecycle state.
//
// Descriptive correction is permitted for terminal rows because it does not
// alter their monetary or lifecycle meaning.
func (m *MerchantPlatformCreditAccountModel) UpdateDescriptiveFields(
	ctx context.Context,
	id uuid.UUID,
	sourceCode *string,
	note *string,
) error {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.
		GetLoggerWithContextFromContext(ctx).
		WithFunctionName("UpdateMerchantPlatformCreditAccountDescriptiveFields")

	if id == uuid.Nil {
		err := errors.New("merchant platform credit account id is required")
		logger.Error("validation failed", err)
		return err
	}

	sourceCode = normalizeOptionalString(sourceCode)
	note = normalizeOptionalString(note)

	if err := validateMerchantPlatformCreditOptionalText(
		sourceCode,
		"source_code",
		merchantPlatformCreditAccountMaxSourceCodeLength,
	); err != nil {
		logger.Error("validation failed", err)
		return err
	}

	if err := validateMerchantPlatformCreditOptionalText(
		note,
		"note",
		merchantPlatformCreditAccountMaxNoteLength,
	); err != nil {
		logger.Error("validation failed", err)
		return err
	}

	const query = `
		UPDATE merchant_platform_credit_accounts
		SET
			source_code = $2,
			note = $3,
			updated_at = NOW()
		WHERE id = $1
		RETURNING updated_at
	`

	var updatedAt time.Time
	err := m.DB.QueryRow(
		ctx,
		query,
		id,
		sourceCode,
		note,
	).Scan(&updatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrMerchantPlatformCreditAccountNotFound
		}

		err = classifyMerchantPlatformCreditAccountWriteError(err)
		logger.Error(
			"update merchant platform credit account descriptive fields failed",
			err,
			"merchant_platform_credit_account_id", id,
		)
		return err
	}

	logger.Info(
		"update merchant platform credit account descriptive fields successful",
		"merchant_platform_credit_account_id", id,
		"updated_at", updatedAt,
	)

	return nil
}

// -----------------------------------------------------------------------------
// Lifecycle
// -----------------------------------------------------------------------------

// Cancel transitions an active account to cancelled.
//
// Cancellation is idempotent for an account already in cancelled status.
// Exhausted and expired accounts cannot be rewritten as cancelled because
// doing so would destroy the historical reason the account became unusable.
func (m *MerchantPlatformCreditAccountModel) Cancel(
	ctx context.Context,
	id uuid.UUID,
) error {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.
		GetLoggerWithContextFromContext(ctx).
		WithFunctionName("CancelMerchantPlatformCreditAccount")

	if id == uuid.Nil {
		err := errors.New("merchant platform credit account id is required")
		logger.Error("validation failed", err)
		return err
	}

	const updateQuery = `
		UPDATE merchant_platform_credit_accounts
		SET
			status = 'cancelled',
			updated_at = NOW()
		WHERE id = $1
		  AND status = 'active'
		RETURNING updated_at
	`

	var updatedAt time.Time
	err := m.DB.QueryRow(ctx, updateQuery, id).Scan(&updatedAt)
	if err == nil {
		logger.Info(
			"cancel merchant platform credit account successful",
			"merchant_platform_credit_account_id", id,
			"updated_at", updatedAt,
		)
		return nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		err = classifyMerchantPlatformCreditAccountWriteError(err)
		logger.Error(
			"cancel merchant platform credit account failed",
			err,
			"merchant_platform_credit_account_id", id,
		)
		return err
	}

	const statusQuery = `
		SELECT status
		FROM merchant_platform_credit_accounts
		WHERE id = $1
	`

	var status MerchantPlatformCreditAccountStatus
	err = m.DB.QueryRow(ctx, statusQuery, id).Scan(&status)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrMerchantPlatformCreditAccountNotFound
		}

		logger.Error(
			"read merchant platform credit account status after cancel conflict failed",
			err,
			"merchant_platform_credit_account_id", id,
		)
		return err
	}

	switch status {
	case MerchantPlatformCreditAccountStatusCancelled:
		return nil
	case MerchantPlatformCreditAccountStatusExhausted,
		MerchantPlatformCreditAccountStatusExpired:
		return ErrMerchantPlatformCreditAccountInvalidTransition
	default:
		return ErrMerchantPlatformCreditAccountInvalidState
	}
}

// ExpireBatch transitions up to limit eligible active rows to expired using
// database time.
//
// FOR UPDATE SKIP LOCKED permits multiple workers to process disjoint batches.
// This method performs one batch only. Scheduling and worker lifecycle belong
// outside the data model.
func (m *MerchantPlatformCreditAccountModel) ExpireBatch(
	ctx context.Context,
	limit int,
) ([]uuid.UUID, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.
		GetLoggerWithContextFromContext(ctx).
		WithFunctionName("ExpireBatchMerchantPlatformCreditAccounts")

	if limit <= 0 || limit > merchantPlatformCreditAccountMaxBatchLimit {
		err := fmt.Errorf(
			"limit must be between 1 and %d",
			merchantPlatformCreditAccountMaxBatchLimit,
		)
		logger.Error("validation failed", err)
		return nil, err
	}

	const query = `
		WITH candidates AS (
			SELECT id
			FROM merchant_platform_credit_accounts
			WHERE status = 'active'
			  AND expires_at IS NOT NULL
			  AND expires_at <= NOW()
			ORDER BY expires_at ASC, id ASC
			LIMIT $1
			FOR UPDATE SKIP LOCKED
		)
		UPDATE merchant_platform_credit_accounts AS account
		SET
			status = 'expired',
			updated_at = NOW()
		FROM candidates
		WHERE account.id = candidates.id
		RETURNING account.id
	`

	rows, err := m.DB.Query(ctx, query, limit)
	if err != nil {
		logger.Error(
			"expire merchant platform credit account batch failed",
			err,
			"limit", limit,
		)
		return nil, err
	}
	defer rows.Close()

	expiredIDs := make([]uuid.UUID, 0)

	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			logger.Error(
				"scan expired merchant platform credit account id failed",
				err,
			)
			return nil, err
		}

		expiredIDs = append(expiredIDs, id)
	}

	if err := rows.Err(); err != nil {
		logger.Error(
			"iterate expired merchant platform credit account ids failed",
			err,
		)
		return nil, err
	}

	logger.Info(
		"expire merchant platform credit account batch successful",
		"count", len(expiredIDs),
	)

	return expiredIDs, nil
}

// -----------------------------------------------------------------------------
// Atomic consumption
// -----------------------------------------------------------------------------

// Consume atomically consumes amount from a currently usable account using
// the model's connection pool.
//
// Use ConsumeTx when the decrement must commit atomically with a future credit
// application, fee calculation, billing-ledger, or invoice mutation.
//
// Idempotency boundary:
//
//	This method prevents overspending and negative balances under concurrent
//	callers. It does not deduplicate repeated business operations. A durable
//	idempotency key belongs to the future credit-application or billing record
//	that explains why the credit was consumed.
func (m *MerchantPlatformCreditAccountModel) Consume(
	ctx context.Context,
	id uuid.UUID,
	amount string,
	currency string,
) (*MerchantPlatformCreditAccount, error) {
	return m.ConsumeTx(ctx, m.DB, id, amount, currency)
}

// ConsumeTx is the transaction-aware form of Consume.
//
// q may be *pgxpool.Pool or pgx.Tx. Callers that require atomic composition
// must pass their existing transaction rather than invoking Consume.
func (m *MerchantPlatformCreditAccountModel) ConsumeTx(
	ctx context.Context,
	q merchantPlatformCreditAccountQuerier,
	id uuid.UUID,
	amount string,
	currency string,
) (*MerchantPlatformCreditAccount, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.
		GetLoggerWithContextFromContext(ctx).
		WithFunctionName("ConsumeMerchantPlatformCreditAccount")

	if q == nil {
		err := errors.New(
			"merchant platform credit account querier is required",
		)
		logger.Error("validation failed", err)
		return nil, err
	}

	if id == uuid.Nil {
		err := errors.New("merchant platform credit account id is required")
		logger.Error("validation failed", err)
		return nil, err
	}

	amount = normalizeMerchantPlatformCreditAmount(amount)
	if err := validatePositiveMerchantPlatformCreditAmount(amount); err != nil {
		logger.Error("validation failed", err)
		return nil, err
	}

	currency = normalizeMerchantPlatformCreditCurrency(currency)
	if err := validateMerchantPlatformCreditCurrency(currency); err != nil {
		logger.Error("validation failed", err)
		return nil, err
	}

	const consumeQuery = `
		UPDATE merchant_platform_credit_accounts
		SET
			remaining_amount = remaining_amount - $2::numeric,
			status = CASE
				WHEN remaining_amount - $2::numeric = 0
					THEN 'exhausted'
				ELSE status
			END,
			updated_at = NOW()
		WHERE id = $1
		  AND currency = $3
		  AND status = 'active'
		  AND remaining_amount >= $2::numeric
		  AND starts_at <= NOW()
		  AND (
				expires_at IS NULL
				OR expires_at > NOW()
		  )
		RETURNING ` + merchantPlatformCreditAccountSelectColumns

	var account MerchantPlatformCreditAccount
	err := scanMerchantPlatformCreditAccount(
		q.QueryRow(ctx, consumeQuery, id, amount, currency),
		&account,
	)
	if err == nil {
		logger.Info(
			"consume merchant platform credit account successful",
			"merchant_platform_credit_account_id", account.ID,
			"merchant_id", account.MerchantID,
			"status", account.Status,
			"currency", account.Currency,
		)
		return &account, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		err = classifyMerchantPlatformCreditAccountWriteError(err)
		logger.Error(
			"consume merchant platform credit account failed",
			err,
			"merchant_platform_credit_account_id", id,
			"currency", currency,
		)
		return nil, err
	}

	// The guarded mutation matched no row. This diagnostic provides a useful
	// current-state classification. Under concurrent mutation, it must not be
	// interpreted as a historical proof of the exact state at the instant the
	// guarded UPDATE was evaluated.
	const diagnosticQuery = `
		SELECT
			currency,
			status,
			remaining_amount >= $2::numeric AS has_sufficient_balance,
			starts_at <= NOW()
				AND (
					expires_at IS NULL
					OR expires_at > NOW()
				) AS inside_validity_window
		FROM merchant_platform_credit_accounts
		WHERE id = $1
	`

	var persistedCurrency string
	var status MerchantPlatformCreditAccountStatus
	var hasSufficientBalance bool
	var insideValidityWindow bool

	err = q.QueryRow(
		ctx,
		diagnosticQuery,
		id,
		amount,
	).Scan(
		&persistedCurrency,
		&status,
		&hasSufficientBalance,
		&insideValidityWindow,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrMerchantPlatformCreditAccountNotFound
		}

		logger.Error(
			"diagnose merchant platform credit account consumption failure failed",
			err,
			"merchant_platform_credit_account_id", id,
		)
		return nil, err
	}

	switch {
	case persistedCurrency != currency:
		return nil, ErrMerchantPlatformCreditAccountCurrencyMismatch

	case status != MerchantPlatformCreditAccountStatusActive ||
		!insideValidityWindow:
		return nil, ErrMerchantPlatformCreditAccountNotUsable

	case !hasSufficientBalance:
		return nil, ErrMerchantPlatformCreditAccountInsufficientBalance

	default:
		// A concurrent mutation may have changed the row between the guarded
		// UPDATE and this read. The safe result is a mutation conflict; callers
		// must not assume that retrying is idempotent.
		return nil, ErrMerchantPlatformCreditAccountMutationConflict
	}
}
