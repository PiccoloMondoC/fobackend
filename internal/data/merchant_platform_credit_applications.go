// Package data provides models and database access methods for merchant
// platform credit applications.
//
// sdworkspace/sdbackend/internal/data/merchant_platform_credit_applications.go
//
// GTM:
//
//	Layer: 2.4.b Commerce Architecture / Monetization Layer
//	Release Class: SPINE
//	Reason:
//	  merchant_platform_credit_applications records the durable historical
//	  fact that a specific amount of platform-issued commercial credit (see
//	  merchant_platform_credit_accounts.go) was applied against a specific
//	  fee calculation. This changes the amount commercially owed before
//	  payment collection. It therefore belongs to Commerce Architecture, not
//	  Merchant Payments Architecture.
//
// Domain Boundary:
//
//	A merchant platform credit application is not:
//
//	  - merchant-held money;
//	  - a wallet transaction;
//	  - a treasury transfer;
//	  - a deposit;
//	  - escrow;
//	  - stored value;
//	  - a payment;
//	  - a payment settlement;
//	  - a refund; or
//	  - a payment-provider transaction.
//
//	It is an append-only monetary record explaining why platform-issued
//	commercial credit was consumed. This file does not decide which
//	merchants receive credit, how much, which fee types are credit-eligible,
//	which account should be consumed, the order multiple accounts are
//	consumed in, or whether an actor is authorized to initiate the
//	application. Those responsibilities belong to governed eligibility data,
//	service orchestration, authorization, and commercial configuration.
//
// Lifecycle:
//
//	created once
//	    -> retained permanently
//
//	There is no update, delete, soft-delete, restore, cancellation, or
//	replacement lifecycle for a persisted application row. A correction to
//	an erroneous application must eventually be represented through an
//	explicit reversal or compensating financial record, not by mutating or
//	deleting historical truth. That reversal capability does not exist yet
//	and is intentionally not invented here.
//
// Idempotency Boundary:
//
//	The unique pair (credit_account_id, fee_calculation_id) is the durable
//	idempotency boundary for this domain: one credit account may apply
//	credit to a given fee calculation at most once. Multiple different
//	credit accounts may each apply credit to the same fee calculation
//	(partial coverage across accounts is supported). A retried logical
//	application from the same account against the same fee calculation is
//	rejected by the unique constraint, which — because insertion is only
//	ever performed inside the same transaction as the corresponding
//	MerchantPlatformCreditAccountModel.ConsumeTx decrement — causes the
//	entire operation, including the balance decrement, to roll back. This is
//	the mechanism that makes the composed operation safe under retry.
//
// Transaction Boundary:
//
//	A credit application must never be inserted independently of the
//	credit-account decrement it explains. This file intentionally exposes
//	only a transaction-aware insertion method, InsertTx, which accepts a
//	concrete pgx.Tx. It does not expose a pool-backed insertion method: a
//	pool-backed method would make it possible to persist an application row
//	without an atomic account decrement, which this domain must never allow.
//	Callers must compose InsertTx with
//	MerchantPlatformCreditAccountModel.ConsumeTx inside one service-owned
//	transaction.
//
// Currency and Ownership Boundary:
//
//	This model validates applied_amount and currency structurally (positive
//	NUMERIC(19,4)-compatible decimal string; uppercase three-letter
//	currency). It does not and cannot verify, at the database level, that
//	the application currency matches the credit account currency, that it
//	matches the fee calculation currency, or that the credit account and fee
//	calculation belong to the same merchant, because merchant_fee_calculations
//	was not supplied to this implementation. Callers composing InsertTx with
//	ConsumeTx must pass the identical normalized currency value that
//	ConsumeTx validated against the account. Cross-entity ownership
//	validation belongs to the service transaction that reads both entities.
//
// SPINE Rule:
//
//	Keep compiling.
//	Keep production-ready.
//	Preserve append-only monetary history: no update, delete, or restore.
//	Preserve the (credit_account_id, fee_calculation_id) idempotency
//	boundary.
//	Preserve atomic composition with
//	MerchantPlatformCreditAccountModel.ConsumeTx.
//	Never implement commercial eligibility or account-selection policy in
//	this file.
//	Never expose a pool-backed insertion method that implies a complete,
//	independent financial operation.
//	Block deployment if this file breaks build, monetary integrity,
//	concurrency safety, lifecycle integrity, or billing readiness.
package data

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/PiccoloMondoC/sdworkspace/sdbackend/internal/logging"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// -----------------------------------------------------------------------------
// Structural bounds
// -----------------------------------------------------------------------------

// merchantPlatformCreditApplicationMaxListLimit bounds offset-paginated list
// reads for this domain.
const merchantPlatformCreditApplicationMaxListLimit = 100

// merchantPlatformCreditApplicationSelectColumns centralizes SQL projection
// order so every scan path remains aligned with
// MerchantPlatformCreditApplication.
const merchantPlatformCreditApplicationSelectColumns = `
	id,
	credit_account_id,
	fee_calculation_id,
	applied_amount,
	currency,
	applied_at
`

// -----------------------------------------------------------------------------
// Persisted constraint names
// -----------------------------------------------------------------------------

// The following constant values must match the constraint names defined in
// the authoritative migration for merchant_platform_credit_applications.
// Stable classification of foreign-key and uniqueness failures depends on
// these names remaining aligned with the persisted schema.
const (
	merchantPlatformCreditApplicationCreditAccountFKConstraint  = "merchant_platform_credit_applications_credit_account_id_fkey"
	merchantPlatformCreditApplicationFeeCalculationFKConstraint = "merchant_platform_credit_applications_fee_calculation_id_fkey"
	merchantPlatformCreditApplicationAccountFeeUniqueConstraint = "merchant_platform_credit_applications_account_fee_uniq"
)

// -----------------------------------------------------------------------------
// Domain error helpers
// -----------------------------------------------------------------------------

// merchantPlatformCreditApplicationInvalidInput wraps
// ErrMerchantPlatformCreditApplicationInvalidInput with a human-readable
// diagnostic message while preserving errors.Is classification stability for
// callers above this model.
func merchantPlatformCreditApplicationInvalidInput(
	format string,
	args ...interface{},
) error {
	return fmt.Errorf(
		"%w: %s",
		ErrMerchantPlatformCreditApplicationInvalidInput,
		fmt.Sprintf(format, args...),
	)
}

// classifyMerchantPlatformCreditApplicationWriteError translates persistence
// constraint failures into stable data-layer domain errors.
//
// Foreign-key and uniqueness violations are classified by persisted
// constraint name first, because this table has two independent foreign keys
// and a composite uniqueness rule that a caller must be able to distinguish.
// If the underlying error does not carry a recognized constraint name (for
// example because the migration has not been kept aligned with the names in
// this file), classification falls back to the shared, centrally owned
// violation-kind classifiers.
func classifyMerchantPlatformCreditApplicationWriteError(err error) error {
	switch {
	case IsPgConstraint(err, merchantPlatformCreditApplicationCreditAccountFKConstraint):
		return ErrMerchantPlatformCreditApplicationCreditAccountNotFound
	case IsPgConstraint(err, merchantPlatformCreditApplicationFeeCalculationFKConstraint):
		return ErrMerchantPlatformCreditApplicationFeeCalculationNotFound
	case IsPgConstraint(err, merchantPlatformCreditApplicationAccountFeeUniqueConstraint):
		return ErrMerchantPlatformCreditApplicationDuplicate
	case IsUniqueViolation(err):
		return ErrMerchantPlatformCreditApplicationDuplicate
	case IsForeignKeyViolation(err), IsCheckViolation(err), IsNotNullViolation(err):
		return ErrMerchantPlatformCreditApplicationInvalidState
	default:
		return err
	}
}

// -----------------------------------------------------------------------------
// Entity and model
// -----------------------------------------------------------------------------

// MerchantPlatformCreditApplication represents one durable, append-only
// record of platform-issued commercial credit being applied against a fee
// calculation.
//
// AppliedAmount and Currency are immutable after insertion. AppliedAt is
// database-owned. There is no update, delete, or restore path for this
// entity; see the package-level doc comment for the lifecycle and reversal
// doctrine.
type MerchantPlatformCreditApplication struct {
	ID               uuid.UUID `json:"id" db:"id"`
	CreditAccountID  uuid.UUID `json:"credit_account_id" db:"credit_account_id"`
	FeeCalculationID uuid.UUID `json:"fee_calculation_id" db:"fee_calculation_id"`
	AppliedAmount    string    `json:"applied_amount" db:"applied_amount"`
	Currency         string    `json:"currency" db:"currency"`
	AppliedAt        time.Time `json:"applied_at" db:"applied_at"`
}

// MerchantPlatformCreditApplicationModel owns persistence for merchant
// platform credit applications.
type MerchantPlatformCreditApplicationModel struct {
	DB     *pgxpool.Pool
	Logger *logging.Logger
}

// scanMerchantPlatformCreditApplication scans the canonical projection into
// application. scannableRow is the package-wide scan contract shared by
// pgx.Row and pgx.Rows.
func scanMerchantPlatformCreditApplication(
	row scannableRow,
	application *MerchantPlatformCreditApplication,
) error {
	return row.Scan(
		&application.ID,
		&application.CreditAccountID,
		&application.FeeCalculationID,
		&application.AppliedAmount,
		&application.Currency,
		&application.AppliedAt,
	)
}

// -----------------------------------------------------------------------------
// Validation
// -----------------------------------------------------------------------------

// validateMerchantPlatformCreditApplicationForInsert validates and
// normalizes the safe creation contract.
//
// AppliedAmount and Currency normalization/validation reuse the package-wide
// helpers established in merchant_platform_credit_accounts.go
// (normalizeMerchantPlatformCreditAmount, validatePositiveMerchantPlatformCreditAmount,
// normalizeMerchantPlatformCreditCurrency, validateMerchantPlatformCreditCurrency)
// rather than duplicating amount/currency structural rules locally, per BEG
// §3.21-3.22.
func validateMerchantPlatformCreditApplicationForInsert(
	application *MerchantPlatformCreditApplication,
) error {
	if application == nil {
		return merchantPlatformCreditApplicationInvalidInput(
			"application is required",
		)
	}

	application.Currency = normalizeMerchantPlatformCreditCurrency(application.Currency)
	application.AppliedAmount = normalizeMerchantPlatformCreditAmount(application.AppliedAmount)

	if application.CreditAccountID == uuid.Nil {
		return merchantPlatformCreditApplicationInvalidInput(
			"credit_account_id is required",
		)
	}

	if application.FeeCalculationID == uuid.Nil {
		return merchantPlatformCreditApplicationInvalidInput(
			"fee_calculation_id is required",
		)
	}

	if err := validatePositiveMerchantPlatformCreditAmount(application.AppliedAmount); err != nil {
		return merchantPlatformCreditApplicationInvalidInput(
			"applied_amount is invalid: %v",
			err,
		)
	}

	if err := validateMerchantPlatformCreditCurrency(application.Currency); err != nil {
		return merchantPlatformCreditApplicationInvalidInput(
			"currency is invalid: %v",
			err,
		)
	}

	return nil
}

// -----------------------------------------------------------------------------
// Transaction-aware creation
// -----------------------------------------------------------------------------

// InsertTx creates a new merchant platform credit application record within
// an existing, caller-owned transaction.
//
// InsertTx applies dbTimeout only to this database operation. The service that
// owns the transaction must impose the outer deadline for the complete
// multi-model transaction so aggregate lock-hold time remains bounded.
//
// InsertTx does not commit or roll back tx; the caller retains ownership of
// the transaction boundary and must compose this call with
// MerchantPlatformCreditAccountModel.ConsumeTx (and any fee-calculation or
// billing-ledger mutation the operation requires) before calling tx.Commit.
// A successful return from InsertTx means the row is persisted within tx,
// not that the overall credit-application workflow has committed.
//
// Creation invariants:
//
//   - ID is generated when absent.
//   - applied_at is database-owned via DEFAULT NOW().
//   - applied_amount and currency are immutable after creation through this
//     model; there is no corresponding update method.
//   - A duplicate (credit_account_id, fee_calculation_id) pair is rejected
//     via ErrMerchantPlatformCreditApplicationDuplicate, causing the caller
//     to roll back the entire transaction, including any account decrement
//     already performed in tx.
//
// InsertTx does not decide which credit account should be consumed, whether
// the fee calculation is credit-eligible, or whether the currency matches
// the credit account; those are service-orchestration responsibilities. The
// caller must pass the same normalized currency that was validated against
// the credit account.
func (m *MerchantPlatformCreditApplicationModel) InsertTx(
	ctx context.Context,
	tx pgx.Tx,
	application *MerchantPlatformCreditApplication,
) (*MerchantPlatformCreditApplication, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.
		GetLoggerWithContextFromContext(ctx).
		WithFunctionName("InsertMerchantPlatformCreditApplicationTx")

	if tx == nil {
		err := merchantPlatformCreditApplicationInvalidInput(
			"transaction is required",
		)
		logger.Warn("validation failed", "error", err)
		return nil, err
	}

	if err := validateMerchantPlatformCreditApplicationForInsert(application); err != nil {
		logger.Warn("validation failed", "error", err)
		return nil, err
	}

	id := application.ID
	if id == uuid.Nil {
		id = uuid.New()
	}

	const query = `
		INSERT INTO merchant_platform_credit_applications (
			id,
			credit_account_id,
			fee_calculation_id,
			applied_amount,
			currency
		)
		VALUES (
			$1,
			$2,
			$3,
			$4::numeric,
			$5
		)
		RETURNING ` + merchantPlatformCreditApplicationSelectColumns

	var result MerchantPlatformCreditApplication
	err := scanMerchantPlatformCreditApplication(
		tx.QueryRow(
			ctx,
			query,
			id,
			application.CreditAccountID,
			application.FeeCalculationID,
			application.AppliedAmount,
			application.Currency,
		),
		&result,
	)
	if err != nil {
		err = classifyMerchantPlatformCreditApplicationWriteError(err)
		logger.Error(
			"insert merchant platform credit application failed within caller-owned transaction; caller must roll back",
			err,
			"credit_account_id", application.CreditAccountID,
			"fee_calculation_id", application.FeeCalculationID,
		)
		return nil, err
	}

	return &result, nil
}

// -----------------------------------------------------------------------------
// Reads
// -----------------------------------------------------------------------------

// GetByID retrieves a merchant platform credit application by canonical ID.
//
// Absence is a normal read result and returns (nil, nil), consistent with
// MerchantPlatformCreditAccountModel.GetByID.
func (m *MerchantPlatformCreditApplicationModel) GetByID(
	ctx context.Context,
	id uuid.UUID,
) (*MerchantPlatformCreditApplication, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.
		GetLoggerWithContextFromContext(ctx).
		WithFunctionName("GetMerchantPlatformCreditApplicationByID")

	if id == uuid.Nil {
		err := merchantPlatformCreditApplicationInvalidInput(
			"id is required",
		)
		logger.Warn("validation failed", "error", err)
		return nil, err
	}

	const query = `
		SELECT ` + merchantPlatformCreditApplicationSelectColumns + `
		FROM merchant_platform_credit_applications
		WHERE id = $1
	`

	var application MerchantPlatformCreditApplication
	err := scanMerchantPlatformCreditApplication(
		m.DB.QueryRow(ctx, query, id),
		&application,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}

		logger.Error(
			"get merchant platform credit application by id failed",
			err,
			"merchant_platform_credit_application_id", id,
		)
		return nil, err
	}

	return &application, nil
}

// GetByCreditAccountAndFeeCalculation retrieves the application row, if any,
// representing the given credit account's contribution to the given fee
// calculation.
//
// This is the canonical read for checking the (credit_account_id,
// fee_calculation_id) idempotency boundary from the service layer prior to
// composing InsertTx. It must not be used as the concurrency guarantee
// itself: the unique constraint enforced at insertion time is the
// concurrency guarantee. Absence returns (nil, nil).
func (m *MerchantPlatformCreditApplicationModel) GetByCreditAccountAndFeeCalculation(
	ctx context.Context,
	creditAccountID uuid.UUID,
	feeCalculationID uuid.UUID,
) (*MerchantPlatformCreditApplication, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.
		GetLoggerWithContextFromContext(ctx).
		WithFunctionName("GetMerchantPlatformCreditApplicationByCreditAccountAndFeeCalculation")

	if creditAccountID == uuid.Nil || feeCalculationID == uuid.Nil {
		err := merchantPlatformCreditApplicationInvalidInput(
			"credit_account_id and fee_calculation_id are required",
		)
		logger.Warn("validation failed", "error", err)
		return nil, err
	}

	const query = `
		SELECT ` + merchantPlatformCreditApplicationSelectColumns + `
		FROM merchant_platform_credit_applications
		WHERE credit_account_id = $1
		  AND fee_calculation_id = $2
	`

	var application MerchantPlatformCreditApplication
	err := scanMerchantPlatformCreditApplication(
		m.DB.QueryRow(ctx, query, creditAccountID, feeCalculationID),
		&application,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}

		logger.Error(
			"get merchant platform credit application by credit account and fee calculation failed",
			err,
			"credit_account_id", creditAccountID,
			"fee_calculation_id", feeCalculationID,
		)
		return nil, err
	}

	return &application, nil
}

// ListByCreditAccount returns applications for creditAccountID using bounded
// offset pagination, ordered applied_at DESC, id DESC.
func (m *MerchantPlatformCreditApplicationModel) ListByCreditAccount(
	ctx context.Context,
	creditAccountID uuid.UUID,
	limit int,
	offset int,
) ([]*MerchantPlatformCreditApplication, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.
		GetLoggerWithContextFromContext(ctx).
		WithFunctionName("ListMerchantPlatformCreditApplicationsByCreditAccount")

	if creditAccountID == uuid.Nil {
		err := merchantPlatformCreditApplicationInvalidInput(
			"credit_account_id is required",
		)
		logger.Warn("validation failed", "error", err)
		return nil, err
	}
	if limit <= 0 || limit > merchantPlatformCreditApplicationMaxListLimit {
		err := merchantPlatformCreditApplicationInvalidInput(
			"limit must be between 1 and %d",
			merchantPlatformCreditApplicationMaxListLimit,
		)
		logger.Warn("validation failed", "error", err)
		return nil, err
	}
	if offset < 0 {
		err := merchantPlatformCreditApplicationInvalidInput(
			"offset must be non-negative",
		)
		logger.Warn("validation failed", "error", err)
		return nil, err
	}

	const query = `
		SELECT ` + merchantPlatformCreditApplicationSelectColumns + `
		FROM merchant_platform_credit_applications
		WHERE credit_account_id = $1
		ORDER BY applied_at DESC, id DESC
		LIMIT $2
		OFFSET $3
	`

	rows, err := m.DB.Query(ctx, query, creditAccountID, limit, offset)
	if err != nil {
		logger.Error(
			"list merchant platform credit applications by credit account failed",
			err,
			"credit_account_id", creditAccountID,
		)
		return nil, err
	}
	defer rows.Close()

	applications := make([]*MerchantPlatformCreditApplication, 0)

	for rows.Next() {
		var application MerchantPlatformCreditApplication
		if err := scanMerchantPlatformCreditApplication(rows, &application); err != nil {
			logger.Error(
				"scan merchant platform credit application failed",
				err,
				"credit_account_id", creditAccountID,
			)
			return nil, err
		}

		applications = append(applications, &application)
	}

	if err := rows.Err(); err != nil {
		logger.Error(
			"iterate merchant platform credit applications by credit account failed",
			err,
			"credit_account_id", creditAccountID,
		)
		return nil, err
	}

	return applications, nil
}

// ListByFeeCalculation returns applications for feeCalculationID using
// bounded offset pagination, ordered applied_at DESC, id DESC.
func (m *MerchantPlatformCreditApplicationModel) ListByFeeCalculation(
	ctx context.Context,
	feeCalculationID uuid.UUID,
	limit int,
	offset int,
) ([]*MerchantPlatformCreditApplication, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.
		GetLoggerWithContextFromContext(ctx).
		WithFunctionName("ListMerchantPlatformCreditApplicationsByFeeCalculation")

	if feeCalculationID == uuid.Nil {
		err := merchantPlatformCreditApplicationInvalidInput(
			"fee_calculation_id is required",
		)
		logger.Warn("validation failed", "error", err)
		return nil, err
	}
	if limit <= 0 || limit > merchantPlatformCreditApplicationMaxListLimit {
		err := merchantPlatformCreditApplicationInvalidInput(
			"limit must be between 1 and %d",
			merchantPlatformCreditApplicationMaxListLimit,
		)
		logger.Warn("validation failed", "error", err)
		return nil, err
	}
	if offset < 0 {
		err := merchantPlatformCreditApplicationInvalidInput(
			"offset must be non-negative",
		)
		logger.Warn("validation failed", "error", err)
		return nil, err
	}

	const query = `
		SELECT ` + merchantPlatformCreditApplicationSelectColumns + `
		FROM merchant_platform_credit_applications
		WHERE fee_calculation_id = $1
		ORDER BY applied_at DESC, id DESC
		LIMIT $2
		OFFSET $3
	`

	rows, err := m.DB.Query(ctx, query, feeCalculationID, limit, offset)
	if err != nil {
		logger.Error(
			"list merchant platform credit applications by fee calculation failed",
			err,
			"fee_calculation_id", feeCalculationID,
		)
		return nil, err
	}
	defer rows.Close()

	applications := make([]*MerchantPlatformCreditApplication, 0)

	for rows.Next() {
		var application MerchantPlatformCreditApplication
		if err := scanMerchantPlatformCreditApplication(rows, &application); err != nil {
			logger.Error(
				"scan merchant platform credit application failed",
				err,
				"fee_calculation_id", feeCalculationID,
			)
			return nil, err
		}

		applications = append(applications, &application)
	}

	if err := rows.Err(); err != nil {
		logger.Error(
			"iterate merchant platform credit applications by fee calculation failed",
			err,
			"fee_calculation_id", feeCalculationID,
		)
		return nil, err
	}

	return applications, nil
}
