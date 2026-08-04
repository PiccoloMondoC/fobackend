// Package data provides models and database access methods for merchant
// billing accounts.
//
// sdworkspace/sdbackend/internal/data/merchant_billing_accounts.go
//
// GTM:
//
//	Layer: 2.4.b Commerce Architecture / Monetization Layer
//	Release Class: SPINE
//	Reason:
//	  merchant_billing_accounts provides the canonical per-merchant billing
//	  relationship and billing-currency anchor used by monetization workflows.
//	  It preserves billing lifecycle state without calculating obligations,
//	  holding merchant funds, selecting payment methods, or executing payment.
//
// Domain Boundary:
//
//	A merchant billing account is not:
//
//	  - a merchant-held balance;
//	  - a deposit, wallet, treasury account, escrow account, or stored value;
//	  - an invoice, fee calculation, or billing-ledger entry;
//	  - a platform credit account;
//	  - a payment method; or
//	  - a payment-provider account.
//
//	This file records only:
//
//	  - the merchant owning the billing relationship;
//	  - the lifecycle state of that relationship; and
//	  - the canonical currency in which new billing obligations for that
//	    relationship are denominated.
//
//	Status does not erase or invalidate obligations already incurred. Suspension
//	or closure must not prevent settlement, reconciliation, refunds, disputes,
//	audit, or historical reads where those operations remain legally or
//	operationally required.
//
//	This file does not determine:
//
//	  - whether a fee, plan, subscription, invoice, or collection is enabled;
//	  - what a merchant owes;
//	  - why Administration suspends or closes an account;
//	  - whether a particular workflow may proceed in a given status;
//	  - which payment method or provider is used; or
//	  - whether an actor is authorized to mutate the account.
//
//	Those decisions belong to Administration-governed configuration,
//	service orchestration, authorization, Commerce Architecture, and Merchant
//	Payments Architecture.
//
// Monetary Boundary:
//
//	This model persists no amount, balance, minimum funding requirement, or
//	payable total. Currency is represented as a structural three-letter
//	uppercase identifier. The model intentionally embeds no catalog of
//	commercially enabled currencies.
//
//	Currency is immutable through this model after creation. A later correction
//	would require cross-domain orchestration proving that no persisted monetary
//	history would be reinterpreted.
//
// Lifecycle:
//
//	active    -> suspended
//	suspended -> active
//	active    -> closed
//	suspended -> closed
//
//	closed is terminal. Reopening requires a new, expressly approved lifecycle
//	design and is not supported by this model.
//
// SPINE Rule:
//
//	Keep compiling.
//	Keep production-ready.
//	Preserve exactly one billing account per merchant.
//	Preserve explicit creation currency.
//	Preserve guarded lifecycle transitions.
//	Preserve terminal closed-state history.
//	Preserve currency immutability through this model.
//	Never treat status as cancellation of an existing debt.
//	Never persist merchant-held funds or prepaid-balance requirements here.
//	Never expose destructive deletion.
//	Never implement commercial or operational policy in this file.
//	Block deployment if this file breaks build, lifecycle integrity,
//	concurrency safety, currency integrity, or billing orchestration readiness.
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

// MerchantBillingAccountStatus is the persisted lifecycle state of a merchant
// billing account.
type MerchantBillingAccountStatus string

const (
	// MerchantBillingAccountStatusActive indicates that the billing
	// relationship may participate in ordinary new billing workflows, subject
	// to authorization and Administration-governed policy.
	MerchantBillingAccountStatusActive MerchantBillingAccountStatus = "active"

	// MerchantBillingAccountStatusSuspended indicates a reversible
	// administrative halt on ordinary new billing activity. It does not erase
	// existing obligations or prohibit required settlement and reconciliation.
	MerchantBillingAccountStatusSuspended MerchantBillingAccountStatus = "suspended"

	// MerchantBillingAccountStatusClosed indicates that the billing
	// relationship is terminal for new ordinary billing activity. Historical
	// records and outstanding settlement responsibilities remain intact.
	MerchantBillingAccountStatusClosed MerchantBillingAccountStatus = "closed"
)

const merchantBillingAccountMaxListLimit = 100

var merchantBillingAccountCurrencyPattern = regexp.MustCompile(`^[A-Z]{3}$`)

const merchantBillingAccountSelectColumns = `
	merchant_id,
	status,
	currency,
	created_at,
	updated_at
`

// ErrMerchantBillingAccountInvalidInput indicates that caller-supplied data
// violates the structural input contract for merchant billing accounts.
var ErrMerchantBillingAccountInvalidInput = errors.New(
	"invalid merchant billing account input",
)

func merchantBillingAccountInvalidInput(format string, args ...interface{}) error {
	return fmt.Errorf(
		"%w: %s",
		ErrMerchantBillingAccountInvalidInput,
		fmt.Sprintf(format, args...),
	)
}

// MerchantBillingAccount represents one merchant's canonical billing
// relationship and billing currency.
type MerchantBillingAccount struct {
	MerchantID uuid.UUID                    `json:"merchant_id" db:"merchant_id"`
	Status     MerchantBillingAccountStatus `json:"status" db:"status"`
	Currency   string                       `json:"currency" db:"currency"`
	CreatedAt  time.Time                    `json:"created_at" db:"created_at"`
	UpdatedAt  time.Time                    `json:"updated_at" db:"updated_at"`
}

// MerchantBillingAccountModel owns persistence for merchant billing accounts.
type MerchantBillingAccountModel struct {
	DB     *pgxpool.Pool
	Logger *logging.Logger
}

type merchantBillingAccountQuerier interface {
	QueryRow(ctx context.Context, sql string, args ...interface{}) pgx.Row
}

func scanMerchantBillingAccount(
	row scannableRow,
	account *MerchantBillingAccount,
) error {
	return row.Scan(
		&account.MerchantID,
		&account.Status,
		&account.Currency,
		&account.CreatedAt,
		&account.UpdatedAt,
	)
}

// NormalizeMerchantBillingAccountStatus returns the canonical identifier form
// of status.
func NormalizeMerchantBillingAccountStatus(
	status MerchantBillingAccountStatus,
) MerchantBillingAccountStatus {
	return MerchantBillingAccountStatus(
		normalizeIdentifier(string(status)),
	)
}

// IsValidMerchantBillingAccountStatus reports whether status belongs to the
// persisted controlled vocabulary.
func IsValidMerchantBillingAccountStatus(
	status MerchantBillingAccountStatus,
) bool {
	switch NormalizeMerchantBillingAccountStatus(status) {
	case MerchantBillingAccountStatusActive,
		MerchantBillingAccountStatusSuspended,
		MerchantBillingAccountStatusClosed:
		return true
	default:
		return false
	}
}

func normalizeMerchantBillingAccountCurrency(currency string) string {
	return strings.ToUpper(strings.TrimSpace(currency))
}

func validateMerchantBillingAccountCurrency(currency string) error {
	if currency == "" {
		return merchantBillingAccountInvalidInput("currency is required")
	}
	if !merchantBillingAccountCurrencyPattern.MatchString(currency) {
		return merchantBillingAccountInvalidInput(
			"currency must be a three-letter uppercase identifier: %q",
			currency,
		)
	}
	return nil
}

func validateMerchantBillingAccountPersistedState(
	account *MerchantBillingAccount,
) error {
	if account == nil ||
		account.MerchantID == uuid.Nil ||
		!IsValidMerchantBillingAccountStatus(account.Status) ||
		!merchantBillingAccountCurrencyPattern.MatchString(account.Currency) ||
		account.CreatedAt.IsZero() ||
		account.UpdatedAt.IsZero() {
		return ErrMerchantBillingAccountInvalidState
	}
	return nil
}

func classifyMerchantBillingAccountWriteError(err error) error {
	switch {
	case IsUniqueViolation(err):
		return ErrMerchantBillingAccountAlreadyExists
	case IsForeignKeyViolation(err):
		return ErrMerchantNotFound
	case IsCheckViolation(err), IsNotNullViolation(err):
		return ErrMerchantBillingAccountInvalidState
	default:
		return err
	}
}

// Insert creates the merchant's billing account in active status.
//
// Currency must be supplied explicitly. The data layer does not infer currency
// from locale, market, payment provider, or deployment defaults.
func (m *MerchantBillingAccountModel) Insert(
	ctx context.Context,
	merchantID uuid.UUID,
	currency string,
) (*MerchantBillingAccount, error) {
	return m.InsertTx(ctx, m.DB, merchantID, currency)
}

// InsertTx is the transaction-aware form of Insert.
//
// q may be *pgxpool.Pool or pgx.Tx. Use this method when billing-account
// creation must commit atomically with merchant onboarding or another
// orchestrated workflow.
func (m *MerchantBillingAccountModel) InsertTx(
	ctx context.Context,
	q merchantBillingAccountQuerier,
	merchantID uuid.UUID,
	currency string,
) (*MerchantBillingAccount, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.
		GetLoggerWithContextFromContext(ctx).
		WithFunctionName("InsertMerchantBillingAccount")

	if q == nil {
		err := merchantBillingAccountInvalidInput("querier is required")
		logger.Error("validation failed", err)
		return nil, err
	}
	if merchantID == uuid.Nil {
		err := merchantBillingAccountInvalidInput("merchant_id is required")
		logger.Error("validation failed", err)
		return nil, err
	}

	currency = normalizeMerchantBillingAccountCurrency(currency)
	if err := validateMerchantBillingAccountCurrency(currency); err != nil {
		logger.Error("validation failed", err)
		return nil, err
	}

	const query = `
		INSERT INTO merchant_billing_accounts (
			merchant_id,
			status,
			currency
		)
		VALUES ($1, 'active', $2)
		RETURNING ` + merchantBillingAccountSelectColumns

	var account MerchantBillingAccount
	if err := scanMerchantBillingAccount(
		q.QueryRow(ctx, query, merchantID, currency),
		&account,
	); err != nil {
		classifiedErr := classifyMerchantBillingAccountWriteError(err)
		logger.Error(
			"insert merchant billing account failed",
			classifiedErr,
			"merchant_id", merchantID,
			"currency", currency,
		)
		return nil, classifiedErr
	}

	if err := validateMerchantBillingAccountPersistedState(&account); err != nil {
		logger.Error(
			"insert merchant billing account returned invalid state",
			err,
			"merchant_id", merchantID,
		)
		return nil, err
	}

	logger.Info(
		"merchant billing account created",
		"merchant_id", account.MerchantID,
		"status", account.Status,
		"currency", account.Currency,
	)

	return &account, nil
}

// GetByMerchantID retrieves the billing account for merchantID.
func (m *MerchantBillingAccountModel) GetByMerchantID(
	ctx context.Context,
	merchantID uuid.UUID,
) (*MerchantBillingAccount, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.
		GetLoggerWithContextFromContext(ctx).
		WithFunctionName("GetMerchantBillingAccountByMerchantID")

	if merchantID == uuid.Nil {
		err := merchantBillingAccountInvalidInput("merchant_id is required")
		logger.Error("validation failed", err)
		return nil, err
	}

	const query = `
		SELECT ` + merchantBillingAccountSelectColumns + `
		FROM merchant_billing_accounts
		WHERE merchant_id = $1
	`

	var account MerchantBillingAccount
	if err := scanMerchantBillingAccount(
		m.DB.QueryRow(ctx, query, merchantID),
		&account,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrMerchantBillingAccountNotFound
		}

		logger.Error(
			"get merchant billing account by merchant id failed",
			err,
			"merchant_id", merchantID,
		)
		return nil, err
	}

	if err := validateMerchantBillingAccountPersistedState(&account); err != nil {
		logger.Error(
			"merchant billing account contains invalid persisted state",
			err,
			"merchant_id", merchantID,
		)
		return nil, err
	}

	return &account, nil
}

// ListByStatus returns a bounded, deterministically ordered page of billing
// accounts in status.
//
// Pagination is keyset-based. beforeCreatedAt and beforeMerchantID must either
// both be nil for the first page or both be non-nil for a subsequent page.
func (m *MerchantBillingAccountModel) ListByStatus(
	ctx context.Context,
	status MerchantBillingAccountStatus,
	limit int,
	beforeCreatedAt *time.Time,
	beforeMerchantID *uuid.UUID,
) ([]*MerchantBillingAccount, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.
		GetLoggerWithContextFromContext(ctx).
		WithFunctionName("ListMerchantBillingAccountsByStatus")

	status = NormalizeMerchantBillingAccountStatus(status)
	if !IsValidMerchantBillingAccountStatus(status) {
		err := merchantBillingAccountInvalidInput("invalid status: %q", status)
		logger.Error("validation failed", err)
		return nil, err
	}
	if limit <= 0 || limit > merchantBillingAccountMaxListLimit {
		err := merchantBillingAccountInvalidInput(
			"limit must be between 1 and %d",
			merchantBillingAccountMaxListLimit,
		)
		logger.Error("validation failed", err)
		return nil, err
	}
	if (beforeCreatedAt == nil) != (beforeMerchantID == nil) {
		err := merchantBillingAccountInvalidInput(
			"before_created_at and before_merchant_id must be supplied together",
		)
		logger.Error("validation failed", err)
		return nil, err
	}
	if beforeCreatedAt != nil && beforeCreatedAt.IsZero() {
		err := merchantBillingAccountInvalidInput(
			"before_created_at must not be zero",
		)
		logger.Error("validation failed", err)
		return nil, err
	}
	if beforeMerchantID != nil && *beforeMerchantID == uuid.Nil {
		err := merchantBillingAccountInvalidInput(
			"before_merchant_id must not be nil UUID",
		)
		logger.Error("validation failed", err)
		return nil, err
	}

	var (
		rows pgx.Rows
		err  error
	)

	if beforeCreatedAt == nil {
		const query = `
			SELECT ` + merchantBillingAccountSelectColumns + `
			FROM merchant_billing_accounts
			WHERE status = $1
			ORDER BY created_at DESC, merchant_id DESC
			LIMIT $2
		`
		rows, err = m.DB.Query(ctx, query, status, limit)
	} else {
		const query = `
			SELECT ` + merchantBillingAccountSelectColumns + `
			FROM merchant_billing_accounts
			WHERE status = $1
			  AND (created_at, merchant_id) < ($2, $3)
			ORDER BY created_at DESC, merchant_id DESC
			LIMIT $4
		`
		rows, err = m.DB.Query(
			ctx,
			query,
			status,
			*beforeCreatedAt,
			*beforeMerchantID,
			limit,
		)
	}
	if err != nil {
		logger.Error(
			"list merchant billing accounts by status failed",
			err,
			"status", status,
		)
		return nil, err
	}
	defer rows.Close()

	accounts := make([]*MerchantBillingAccount, 0, limit)
	for rows.Next() {
		var account MerchantBillingAccount
		if err := scanMerchantBillingAccount(rows, &account); err != nil {
			logger.Error(
				"scan merchant billing account failed",
				err,
				"status", status,
			)
			return nil, err
		}
		if err := validateMerchantBillingAccountPersistedState(&account); err != nil {
			logger.Error(
				"merchant billing account contains invalid persisted state",
				err,
				"merchant_id", account.MerchantID,
			)
			return nil, err
		}
		accounts = append(accounts, &account)
	}

	if err := rows.Err(); err != nil {
		logger.Error(
			"iterate merchant billing accounts failed",
			err,
			"status", status,
		)
		return nil, err
	}

	return accounts, nil
}

// Suspend transitions an active billing account to suspended.
//
// The method enforces lifecycle integrity only. It does not decide why the
// account should be suspended or which downstream operations suspension
// affects.
func (m *MerchantBillingAccountModel) Suspend(
	ctx context.Context,
	merchantID uuid.UUID,
) error {
	return m.SuspendTx(ctx, m.DB, merchantID)
}

// SuspendTx is the transaction-aware form of Suspend.
func (m *MerchantBillingAccountModel) SuspendTx(
	ctx context.Context,
	q merchantBillingAccountQuerier,
	merchantID uuid.UUID,
) error {
	return m.transitionTx(
		ctx,
		q,
		"SuspendMerchantBillingAccount",
		merchantID,
		[]MerchantBillingAccountStatus{
			MerchantBillingAccountStatusActive,
		},
		MerchantBillingAccountStatusSuspended,
	)
}

// Reactivate transitions a suspended billing account to active.
//
// The method enforces lifecycle integrity only. It does not decide whether
// Administration should permit reactivation.
func (m *MerchantBillingAccountModel) Reactivate(
	ctx context.Context,
	merchantID uuid.UUID,
) error {
	return m.ReactivateTx(ctx, m.DB, merchantID)
}

// ReactivateTx is the transaction-aware form of Reactivate.
func (m *MerchantBillingAccountModel) ReactivateTx(
	ctx context.Context,
	q merchantBillingAccountQuerier,
	merchantID uuid.UUID,
) error {
	return m.transitionTx(
		ctx,
		q,
		"ReactivateMerchantBillingAccount",
		merchantID,
		[]MerchantBillingAccountStatus{
			MerchantBillingAccountStatusSuspended,
		},
		MerchantBillingAccountStatusActive,
	)
}

// Close transitions an active or suspended billing account to closed.
//
// closed is terminal for new ordinary billing activity. Closure does not erase
// existing obligations, payment history, credits, invoices, or audit records.
func (m *MerchantBillingAccountModel) Close(
	ctx context.Context,
	merchantID uuid.UUID,
) error {
	return m.CloseTx(ctx, m.DB, merchantID)
}

// CloseTx is the transaction-aware form of Close.
func (m *MerchantBillingAccountModel) CloseTx(
	ctx context.Context,
	q merchantBillingAccountQuerier,
	merchantID uuid.UUID,
) error {
	return m.transitionTx(
		ctx,
		q,
		"CloseMerchantBillingAccount",
		merchantID,
		[]MerchantBillingAccountStatus{
			MerchantBillingAccountStatusActive,
			MerchantBillingAccountStatusSuspended,
		},
		MerchantBillingAccountStatusClosed,
	)
}

func (m *MerchantBillingAccountModel) transitionTx(
	ctx context.Context,
	q merchantBillingAccountQuerier,
	functionName string,
	merchantID uuid.UUID,
	fromStatuses []MerchantBillingAccountStatus,
	toStatus MerchantBillingAccountStatus,
) error {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.
		GetLoggerWithContextFromContext(ctx).
		WithFunctionName(functionName)

	if q == nil {
		err := merchantBillingAccountInvalidInput("querier is required")
		logger.Error("validation failed", err)
		return err
	}
	if merchantID == uuid.Nil {
		err := merchantBillingAccountInvalidInput("merchant_id is required")
		logger.Error("validation failed", err)
		return err
	}
	if len(fromStatuses) == 0 || !IsValidMerchantBillingAccountStatus(toStatus) {
		err := ErrMerchantBillingAccountInvalidState
		logger.Error("invalid internal lifecycle configuration", err)
		return err
	}

	fromValues := make([]string, 0, len(fromStatuses))
	for _, status := range fromStatuses {
		status = NormalizeMerchantBillingAccountStatus(status)
		if !IsValidMerchantBillingAccountStatus(status) {
			err := ErrMerchantBillingAccountInvalidState
			logger.Error("invalid internal lifecycle configuration", err)
			return err
		}
		fromValues = append(fromValues, string(status))
	}

	const updateQuery = `
		UPDATE merchant_billing_accounts
		SET
			status = $2,
			updated_at = NOW()
		WHERE merchant_id = $1
		  AND status = ANY($3::text[])
		RETURNING updated_at
	`

	var updatedAt time.Time
	err := q.QueryRow(
		ctx,
		updateQuery,
		merchantID,
		toStatus,
		fromValues,
	).Scan(&updatedAt)
	if err == nil {
		logger.Info(
			"merchant billing account lifecycle transition successful",
			"merchant_id", merchantID,
			"to_status", toStatus,
			"updated_at", updatedAt,
		)
		return nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		classifiedErr := classifyMerchantBillingAccountWriteError(err)
		logger.Error(
			"merchant billing account lifecycle transition failed",
			classifiedErr,
			"merchant_id", merchantID,
			"to_status", toStatus,
		)
		return classifiedErr
	}

	const statusQuery = `
		SELECT status
		FROM merchant_billing_accounts
		WHERE merchant_id = $1
	`

	var currentStatus MerchantBillingAccountStatus
	if err := q.QueryRow(ctx, statusQuery, merchantID).Scan(&currentStatus); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrMerchantBillingAccountNotFound
		}

		logger.Error(
			"read merchant billing account status after transition no-match failed",
			err,
			"merchant_id", merchantID,
			"to_status", toStatus,
		)
		return err
	}

	currentStatus = NormalizeMerchantBillingAccountStatus(currentStatus)
	if !IsValidMerchantBillingAccountStatus(currentStatus) {
		return ErrMerchantBillingAccountInvalidState
	}
	if currentStatus == toStatus {
		return nil
	}
	if !merchantBillingAccountStatusAmong(currentStatus, fromStatuses) {
		return ErrMerchantBillingAccountInvalidTransition
	}

	// The row is presently in an allowed source state even though the guarded
	// UPDATE matched no row. That can occur only when concurrent mutation
	// changed the row between the failed UPDATE and this diagnostic read.
	return ErrMerchantBillingAccountMutationConflict
}

func merchantBillingAccountStatusAmong(
	status MerchantBillingAccountStatus,
	candidates []MerchantBillingAccountStatus,
) bool {
	for _, candidate := range candidates {
		if status == NormalizeMerchantBillingAccountStatus(candidate) {
			return true
		}
	}
	return false
}
