// Package data provides models and database access methods for merchant invoices.
//
// sdworkspace/sdbackend/internal/data/merchant_invoices.go
//
// GTM:
//
//	Layer: 2.4.b Commerce Architecture / Monetization Layer
//	Release Class: SPINE
//	Reason:
//	  merchant_invoices is the canonical durable statement of a merchant's
//	  commercial obligation to Sagrenti. It sits downstream of fee calculation
//	  and platform-credit application and upstream of payment collection.
//
// Domain Boundary:
//
//	An invoice records an obligation snapshot and its settlement lifecycle. It is
//	not a fee calculator, fee schedule, platform-credit allocator, payment
//	processor, payment attempt, billing-policy store, ledger, or invoice-line
//	substitute. Commercial policy decides whether and when an invoice is created,
//	which configured terms apply, which currency is selected, and what due date
//	(if any) applies. This model validates canonical values, preserves financial
//	integrity, exposes guarded lifecycle transitions, and translates persistence
//	failures.
//
// Monetary Boundary:
//
//	subtotal_amount >= 0
//	total_amount = subtotal_amount + adjustment_amount
//	total_amount >= 0
//	0 <= amount_paid <= total_amount
//
//	AdjustmentAmount may be positive, zero, or negative. Draft financial terms
//	may be revised through UpdateDraftFinancials while the row remains draft.
//	After issuance, SubtotalAmount, AdjustmentAmount, TotalAmount, and Currency
//	are immutable through this model. AmountPaid changes only through
//	ApplyPaymentTx. A zero-total issued invoice is settled through
//	SettleZeroBalance/SettleZeroBalanceTx; platform credits are not payments.
//
// Currency Boundary:
//
//	Each invoice contains exactly one uppercase three-letter currency. Merchant
//	domicile does not determine invoice currency. The service/configuration layer
//	selects currency before issuance. Currency may be revised while draft and is
//	immutable after issuance. This model contains no catalog of enabled currencies.
//
// Lifecycle:
//
//	draft -> issued
//	issued -> partially_paid
//	issued -> overdue
//	issued -> paid
//	issued -> void
//	partially_paid -> paid
//	overdue -> partially_paid
//	overdue -> paid
//
//	paid and void are terminal in v1. No backward transitions are exposed.
//	Per the locked v1 contract, partially_paid -> overdue is intentionally not
//	exposed. Service orchestration owns legal transition choice; database CHECK
//	constraints guarantee persisted status/timestamp/amount consistency.
//
// Due-Date Boundary:
//
//	due_at is policy-supplied and may be NULL for issued, partially_paid, paid,
//	or void invoices. overdue requires due_at. When supplied, due_at must not
//	precede issued_at. IssueDueNow/IssueDueNowTx exist so an Administration policy
//	of "due immediately" can be represented without application/DB clock skew;
//	they set issued_at and due_at from the same database timestamp. Engineering
//	does not decide which invoice class uses that capability.
//
// Transaction Boundary:
//
//	InsertDraftTx, UpdateDraftFinancialsTx, IssueTx, IssueDueNowTx,
//	SettleZeroBalanceTx, MarkOverdueTx, VoidTx, GetByIDForUpdateTx, and
//	ApplyPaymentTx accept caller-owned pgx.Tx values and never begin, commit, or
//	roll them back.
//
//	ApplyPaymentTx is transaction-aware only. The increment it persists must be
//	composed in the same service-owned transaction with the authoritative payment
//	record that explains that increment. The invoice model does not provide a
//	pool-backed payment mutation that could diverge from payment history.
//
// Concurrency and Retry:
//
//	Lifecycle mutations use single guarded UPDATE statements. ApplyPaymentTx
//	atomically increments amount_paid against the current row, preventing lost
//	updates when distinct payments race. Retry idempotency for a payment is owned
//	by the composed authoritative merchant_payments write (for example, its
//	provider/idempotency uniqueness) in the same transaction; this model does not
//	pretend that an amount alone identifies a payment.
//
// Invoice Number:
//
//	Invoice-number generation/sequence policy is outside this model. The model
//	trims and structurally bounds the supplied number and relies on the database
//	UNIQUE constraint for collision protection. It never SELECTs before INSERT to
//	claim uniqueness.
//
// Source Traceability:
//
//	merchant_invoices is an invoice header. It does not invent opaque JSON links
//	to fee calculations or platform-credit applications. Auditable source-to-
//	invoice association belongs in a normalized invoice-line/association domain.
//
// SPINE Rule:
//
//	Keep compiling.
//	Keep production-ready.
//	Preserve exact decimal-string handling.
//	Preserve draft-only financial revision and post-issuance monetary immutability.
//	Preserve one-currency-per-invoice and post-issuance currency immutability.
//	Preserve guarded forward-only lifecycle transitions.
//	Preserve transaction-only payment application.
//	Preserve deterministic bounded reads.
//	Do not expose generic update, upsert, delete, soft-delete, or restore methods.
//	Do not hard-code invoice-generation, due-date, currency-selection, pricing,
//	promotion, credit-allocation, or payment-collection policy.
//	Block deployment if this file breaks monetary precision, lifecycle integrity,
//	concurrency safety, historical integrity, or downstream payment composition.
package data

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/PiccoloMondoC/sdworkspace/sdbackend/internal/logging"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	merchantInvoiceMaxListLimit    = 100
	merchantInvoiceNumberMaxLength = 64
)

var (
	merchantInvoiceAmountPattern       = regexp.MustCompile(`^[0-9]{1,15}(?:\.[0-9]{1,4})?$`)
	merchantInvoiceSignedAmountPattern = regexp.MustCompile(`^-?[0-9]{1,15}(?:\.[0-9]{1,4})?$`)
)

const merchantInvoiceSelectColumns = `
	id,
	merchant_id,
	invoice_number,
	invoice_status,
	subtotal_amount,
	adjustment_amount,
	total_amount,
	amount_paid,
	currency,
	issued_at,
	due_at,
	paid_at,
	voided_at,
	created_at,
	updated_at
`

const (
	merchantInvoiceMerchantFKConstraint            = "merchant_invoices_merchant_id_fkey"
	merchantInvoiceNumberUniqueConstraint          = "merchant_invoices_invoice_number_key"
	merchantInvoiceNumberCanonicalConstraint       = "chk_merchant_invoice_number_canonical"
	merchantInvoiceNumberLengthConstraint          = "chk_merchant_invoices_invoice_number_length"
	merchantInvoiceStatusConstraint                = "merchant_invoices_invoice_status_check"
	merchantInvoiceSubtotalConstraint              = "merchant_invoices_subtotal_amount_check"
	merchantInvoiceTotalNonNegativeConstraint      = "merchant_invoices_total_amount_check"
	merchantInvoiceAmountPaidNonNegativeConstraint = "merchant_invoices_amount_paid_check"
	merchantInvoiceCurrencyConstraint              = "merchant_invoices_currency_check"
	merchantInvoiceTotalAmountConstraint           = "chk_merchant_invoice_total_amount"
	merchantInvoiceAmountPaidConstraint            = "chk_merchant_invoice_amount_paid"
	merchantInvoiceDraftStateConstraint            = "chk_merchant_invoice_draft_state"
	merchantInvoiceIssuedStateConstraint           = "chk_merchant_invoice_issued_state"
	merchantInvoicePartiallyPaidStateConstraint    = "chk_merchant_invoice_partially_paid_state"
	merchantInvoicePaidStateConstraint             = "chk_merchant_invoice_paid_state"
	merchantInvoiceOverdueStateConstraint          = "chk_merchant_invoice_overdue_state"
	merchantInvoiceVoidStateConstraint             = "chk_merchant_invoice_void_state"
	merchantInvoiceDueAfterIssueConstraint         = "chk_merchant_invoice_due_after_issue"
	merchantInvoicePaidAfterIssueConstraint        = "chk_merchant_invoice_paid_after_issue"
	merchantInvoiceVoidedAfterIssueConstraint      = "chk_merchant_invoice_voided_after_issue"
)

// MerchantInvoiceStatus is the persisted lifecycle state of a merchant invoice.
type MerchantInvoiceStatus string

const (
	// MerchantInvoiceStatusDraft is the construction state before issuance.
	MerchantInvoiceStatusDraft MerchantInvoiceStatus = "draft"
	// MerchantInvoiceStatusIssued is an issued, unpaid merchant obligation.
	MerchantInvoiceStatusIssued MerchantInvoiceStatus = "issued"
	// MerchantInvoiceStatusPartiallyPaid has some but not all value satisfied.
	MerchantInvoiceStatusPartiallyPaid MerchantInvoiceStatus = "partially_paid"
	// MerchantInvoiceStatusPaid is fully satisfied and terminal in v1.
	MerchantInvoiceStatusPaid MerchantInvoiceStatus = "paid"
	// MerchantInvoiceStatusOverdue has an unpaid balance beyond its due point.
	MerchantInvoiceStatusOverdue MerchantInvoiceStatus = "overdue"
	// MerchantInvoiceStatusVoid is an invalidated issued-but-unpaid invoice and is terminal in v1.
	MerchantInvoiceStatusVoid MerchantInvoiceStatus = "void"
)

// NormalizeMerchantInvoiceStatus returns status in canonical identifier form.
func NormalizeMerchantInvoiceStatus(status MerchantInvoiceStatus) MerchantInvoiceStatus {
	return MerchantInvoiceStatus(normalizeIdentifier(string(status)))
}

// IsValidMerchantInvoiceStatus reports whether status belongs to the persisted vocabulary.
func IsValidMerchantInvoiceStatus(status MerchantInvoiceStatus) bool {
	switch NormalizeMerchantInvoiceStatus(status) {
	case MerchantInvoiceStatusDraft,
		MerchantInvoiceStatusIssued,
		MerchantInvoiceStatusPartiallyPaid,
		MerchantInvoiceStatusPaid,
		MerchantInvoiceStatusOverdue,
		MerchantInvoiceStatusVoid:
		return true
	default:
		return false
	}
}

// MerchantInvoice represents one durable merchant invoice header.
type MerchantInvoice struct {
	ID               uuid.UUID             `json:"id" db:"id"`
	MerchantID       uuid.UUID             `json:"merchant_id" db:"merchant_id"`
	InvoiceNumber    string                `json:"invoice_number" db:"invoice_number"`
	InvoiceStatus    MerchantInvoiceStatus `json:"invoice_status" db:"invoice_status"`
	SubtotalAmount   string                `json:"subtotal_amount" db:"subtotal_amount"`
	AdjustmentAmount string                `json:"adjustment_amount" db:"adjustment_amount"`
	TotalAmount      string                `json:"total_amount" db:"total_amount"`
	AmountPaid       string                `json:"amount_paid" db:"amount_paid"`
	Currency         string                `json:"currency" db:"currency"`
	IssuedAt         *time.Time            `json:"issued_at,omitempty" db:"issued_at"`
	DueAt            *time.Time            `json:"due_at,omitempty" db:"due_at"`
	PaidAt           *time.Time            `json:"paid_at,omitempty" db:"paid_at"`
	VoidedAt         *time.Time            `json:"voided_at,omitempty" db:"voided_at"`
	CreatedAt        time.Time             `json:"created_at" db:"created_at"`
	UpdatedAt        time.Time             `json:"updated_at" db:"updated_at"`
}

// MerchantInvoiceDraftFinancials is the complete mutable financial snapshot of a draft invoice.
type MerchantInvoiceDraftFinancials struct {
	SubtotalAmount   string
	AdjustmentAmount string
	Currency         string
}

// MerchantInvoiceModel owns merchant invoice persistence.
type MerchantInvoiceModel struct {
	DB     *pgxpool.Pool
	Logger *logging.Logger
}

type merchantInvoiceQueryRower interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

func (m *MerchantInvoiceModel) validateBase() error {
	if m == nil {
		return errors.New("merchant invoice model is required")
	}
	if m.Logger == nil {
		return errors.New("merchant invoice model logger is required")
	}
	return nil
}

func (m *MerchantInvoiceModel) validatePool() error {
	if err := m.validateBase(); err != nil {
		return err
	}
	if m.DB == nil {
		return errors.New("merchant invoice model database pool is required")
	}
	return nil
}

func scanMerchantInvoice(row scannableRow, invoice *MerchantInvoice) error {
	return row.Scan(
		&invoice.ID,
		&invoice.MerchantID,
		&invoice.InvoiceNumber,
		&invoice.InvoiceStatus,
		&invoice.SubtotalAmount,
		&invoice.AdjustmentAmount,
		&invoice.TotalAmount,
		&invoice.AmountPaid,
		&invoice.Currency,
		&invoice.IssuedAt,
		&invoice.DueAt,
		&invoice.PaidAt,
		&invoice.VoidedAt,
		&invoice.CreatedAt,
		&invoice.UpdatedAt,
	)
}

func merchantInvoiceInvalidInput(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrMerchantInvoiceInvalidInput, fmt.Sprintf(format, args...))
}

func classifyMerchantInvoiceWriteError(err error) error {
	switch {
	case IsPgConstraint(err, merchantInvoiceMerchantFKConstraint):
		return ErrMerchantInvoiceMerchantNotFound
	case IsPgConstraint(err, merchantInvoiceNumberUniqueConstraint):
		return ErrMerchantInvoiceDuplicateNumber
	case IsPgConstraint(err, merchantInvoiceNumberCanonicalConstraint),
		IsPgConstraint(err, merchantInvoiceNumberLengthConstraint),
		IsPgConstraint(err, merchantInvoiceStatusConstraint),
		IsPgConstraint(err, merchantInvoiceSubtotalConstraint),
		IsPgConstraint(err, merchantInvoiceTotalNonNegativeConstraint),
		IsPgConstraint(err, merchantInvoiceAmountPaidNonNegativeConstraint),
		IsPgConstraint(err, merchantInvoiceCurrencyConstraint),
		IsPgConstraint(err, merchantInvoiceTotalAmountConstraint),
		IsPgConstraint(err, merchantInvoiceAmountPaidConstraint),
		IsPgConstraint(err, merchantInvoiceDraftStateConstraint),
		IsPgConstraint(err, merchantInvoiceIssuedStateConstraint),
		IsPgConstraint(err, merchantInvoicePartiallyPaidStateConstraint),
		IsPgConstraint(err, merchantInvoicePaidStateConstraint),
		IsPgConstraint(err, merchantInvoiceOverdueStateConstraint),
		IsPgConstraint(err, merchantInvoiceVoidStateConstraint),
		IsPgConstraint(err, merchantInvoiceDueAfterIssueConstraint),
		IsPgConstraint(err, merchantInvoicePaidAfterIssueConstraint),
		IsPgConstraint(err, merchantInvoiceVoidedAfterIssueConstraint):
		return ErrMerchantInvoiceInvalidState
	case IsUniqueViolation(err):
		return ErrMerchantInvoiceDuplicateNumber
	case IsForeignKeyViolation(err):
		return ErrMerchantInvoiceMerchantNotFound
	case IsCheckViolation(err), IsNotNullViolation(err):
		return ErrMerchantInvoiceInvalidState
	default:
		return err
	}
}

func normalizeMerchantInvoiceNumber(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", merchantInvoiceInvalidInput("invoice_number is required")
	}
	if utf8.RuneCountInString(value) > merchantInvoiceNumberMaxLength {
		return "", merchantInvoiceInvalidInput(
			"invoice_number must not exceed %d characters",
			merchantInvoiceNumberMaxLength,
		)
	}
	return value, nil
}

func parseMerchantInvoiceDecimal(value, fieldName string, allowNegative bool) (*big.Rat, string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, "", merchantInvoiceInvalidInput("%s is required", fieldName)
	}

	pattern := merchantInvoiceAmountPattern
	if allowNegative {
		pattern = merchantInvoiceSignedAmountPattern
	}
	if !pattern.MatchString(value) {
		return nil, "", merchantInvoiceInvalidInput(
			"%s must be a NUMERIC(19,4)-compatible decimal", fieldName,
		)
	}

	r, ok := new(big.Rat).SetString(value)
	if !ok {
		return nil, "", merchantInvoiceInvalidInput("%s must be a valid decimal value", fieldName)
	}
	if !allowNegative && r.Sign() < 0 {
		return nil, "", merchantInvoiceInvalidInput("%s must be greater than or equal to zero", fieldName)
	}
	return r, value, nil
}

func normalizeMerchantInvoiceFinancials(
	subtotalAmount string,
	adjustmentAmount string,
	currency string,
) (subtotal string, adjustment string, total string, canonicalCurrency string, err error) {
	subtotalRat, subtotal, err := parseMerchantInvoiceDecimal(subtotalAmount, "subtotal_amount", false)
	if err != nil {
		return "", "", "", "", err
	}
	adjustmentRat, adjustment, err := parseMerchantInvoiceDecimal(adjustmentAmount, "adjustment_amount", true)
	if err != nil {
		return "", "", "", "", err
	}

	totalRat := new(big.Rat).Add(subtotalRat, adjustmentRat)
	if totalRat.Sign() < 0 {
		return "", "", "", "", merchantInvoiceInvalidInput(
			"adjustment_amount must not reduce total_amount below zero",
		)
	}
	total = totalRat.FloatString(4)
	if !merchantInvoiceAmountPattern.MatchString(total) {
		return "", "", "", "", merchantInvoiceInvalidInput(
			"total_amount exceeds NUMERIC(19,4) bounds",
		)
	}

	canonicalCurrency = strings.ToUpper(strings.TrimSpace(currency))
	if !isCanonicalCurrency(canonicalCurrency) {
		return "", "", "", "", merchantInvoiceInvalidInput("invalid currency: %q", canonicalCurrency)
	}
	return subtotal, adjustment, total, canonicalCurrency, nil
}

func validateMerchantInvoiceID(id uuid.UUID) error {
	if id == uuid.Nil {
		return merchantInvoiceInvalidInput("id is required")
	}
	return nil
}

func validateMerchantInvoiceMerchantID(id uuid.UUID) error {
	if id == uuid.Nil {
		return merchantInvoiceInvalidInput("merchant_id is required")
	}
	return nil
}

func validateMerchantInvoiceLimit(limit int) error {
	if limit <= 0 || limit > merchantInvoiceMaxListLimit {
		return merchantInvoiceInvalidInput("limit must be between 1 and %d", merchantInvoiceMaxListLimit)
	}
	return nil
}

func validateMerchantInvoiceCreatedCursor(beforeCreatedAt *time.Time, beforeID *uuid.UUID) error {
	if (beforeCreatedAt == nil) != (beforeID == nil) {
		return merchantInvoiceInvalidInput("before_created_at and before_id must be supplied together")
	}
	if beforeCreatedAt == nil {
		return nil
	}
	if beforeCreatedAt.IsZero() || *beforeID == uuid.Nil {
		return merchantInvoiceInvalidInput("invalid merchant invoice created cursor")
	}
	return nil
}

func validateMerchantInvoiceDueCursor(afterDueAt *time.Time, afterID *uuid.UUID) error {
	if (afterDueAt == nil) != (afterID == nil) {
		return merchantInvoiceInvalidInput("after_due_at and after_id must be supplied together")
	}
	if afterDueAt == nil {
		return nil
	}
	if afterDueAt.IsZero() || *afterID == uuid.Nil {
		return merchantInvoiceInvalidInput("invalid merchant invoice due cursor")
	}
	return nil
}

func validateMerchantInvoicePersistedState(invoice *MerchantInvoice) error {
	if invoice == nil ||
		invoice.ID == uuid.Nil ||
		invoice.MerchantID == uuid.Nil ||
		strings.TrimSpace(invoice.InvoiceNumber) == "" ||
		invoice.InvoiceNumber != strings.TrimSpace(invoice.InvoiceNumber) ||
		utf8.RuneCountInString(invoice.InvoiceNumber) > merchantInvoiceNumberMaxLength ||
		invoice.CreatedAt.IsZero() ||
		invoice.UpdatedAt.IsZero() ||
		!isCanonicalCurrency(invoice.Currency) {
		return ErrMerchantInvoiceInvalidState
	}

	status := NormalizeMerchantInvoiceStatus(invoice.InvoiceStatus)
	if !IsValidMerchantInvoiceStatus(status) {
		return ErrMerchantInvoiceInvalidState
	}

	subtotal, ok := new(big.Rat).SetString(invoice.SubtotalAmount)
	if !ok || subtotal.Sign() < 0 {
		return ErrMerchantInvoiceInvalidState
	}
	adjustment, ok := new(big.Rat).SetString(invoice.AdjustmentAmount)
	if !ok {
		return ErrMerchantInvoiceInvalidState
	}
	total, ok := new(big.Rat).SetString(invoice.TotalAmount)
	if !ok || total.Sign() < 0 || new(big.Rat).Add(subtotal, adjustment).Cmp(total) != 0 {
		return ErrMerchantInvoiceInvalidState
	}
	paid, ok := new(big.Rat).SetString(invoice.AmountPaid)
	if !ok || paid.Sign() < 0 || paid.Cmp(total) > 0 {
		return ErrMerchantInvoiceInvalidState
	}

	switch status {
	case MerchantInvoiceStatusDraft:
		if paid.Sign() != 0 || invoice.IssuedAt != nil || invoice.DueAt != nil || invoice.PaidAt != nil || invoice.VoidedAt != nil {
			return ErrMerchantInvoiceInvalidState
		}
	case MerchantInvoiceStatusIssued:
		if paid.Sign() != 0 || invoice.IssuedAt == nil || invoice.PaidAt != nil || invoice.VoidedAt != nil {
			return ErrMerchantInvoiceInvalidState
		}
	case MerchantInvoiceStatusPartiallyPaid:
		if paid.Sign() <= 0 || paid.Cmp(total) >= 0 || invoice.IssuedAt == nil || invoice.PaidAt != nil || invoice.VoidedAt != nil {
			return ErrMerchantInvoiceInvalidState
		}
	case MerchantInvoiceStatusOverdue:
		if paid.Cmp(total) >= 0 || invoice.IssuedAt == nil || invoice.DueAt == nil || invoice.PaidAt != nil || invoice.VoidedAt != nil {
			return ErrMerchantInvoiceInvalidState
		}
	case MerchantInvoiceStatusPaid:
		if paid.Cmp(total) != 0 || invoice.IssuedAt == nil || invoice.PaidAt == nil || invoice.VoidedAt != nil {
			return ErrMerchantInvoiceInvalidState
		}
	case MerchantInvoiceStatusVoid:
		if paid.Sign() != 0 || invoice.IssuedAt == nil || invoice.PaidAt != nil || invoice.VoidedAt == nil {
			return ErrMerchantInvoiceInvalidState
		}
	}

	if invoice.DueAt != nil {
		if invoice.IssuedAt == nil || invoice.DueAt.Before(*invoice.IssuedAt) {
			return ErrMerchantInvoiceInvalidState
		}
	}
	if invoice.PaidAt != nil && (invoice.IssuedAt == nil || invoice.PaidAt.Before(*invoice.IssuedAt)) {
		return ErrMerchantInvoiceInvalidState
	}
	if invoice.VoidedAt != nil && (invoice.IssuedAt == nil || invoice.VoidedAt.Before(*invoice.IssuedAt)) {
		return ErrMerchantInvoiceInvalidState
	}
	return nil
}

func (m *MerchantInvoiceModel) getByIDViaQuerier(
	ctx context.Context,
	querier merchantInvoiceQueryRower,
	id uuid.UUID,
) (*MerchantInvoice, error) {
	const query = `
		SELECT ` + merchantInvoiceSelectColumns + `
		FROM merchant_invoices
		WHERE id = $1
	`
	var invoice MerchantInvoice
	if err := scanMerchantInvoice(querier.QueryRow(ctx, query, id), &invoice); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	if err := validateMerchantInvoicePersistedState(&invoice); err != nil {
		return nil, err
	}
	return &invoice, nil
}

func (m *MerchantInvoiceModel) insertDraft(
	ctx context.Context,
	querier merchantInvoiceQueryRower,
	functionName string,
	invoice *MerchantInvoice,
) (*MerchantInvoice, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName(functionName)
	if invoice == nil {
		return nil, merchantInvoiceInvalidInput("invoice is required")
	}
	if err := validateMerchantInvoiceMerchantID(invoice.MerchantID); err != nil {
		return nil, err
	}
	invoiceNumber, err := normalizeMerchantInvoiceNumber(invoice.InvoiceNumber)
	if err != nil {
		return nil, err
	}
	subtotal, adjustment, total, currency, err := normalizeMerchantInvoiceFinancials(
		invoice.SubtotalAmount,
		invoice.AdjustmentAmount,
		invoice.Currency,
	)
	if err != nil {
		return nil, err
	}
	if invoice.InvoiceStatus != "" && NormalizeMerchantInvoiceStatus(invoice.InvoiceStatus) != MerchantInvoiceStatusDraft {
		return nil, merchantInvoiceInvalidInput("invoice_status must be draft at creation")
	}
	if invoice.IssuedAt != nil || invoice.DueAt != nil || invoice.PaidAt != nil || invoice.VoidedAt != nil {
		return nil, merchantInvoiceInvalidInput("lifecycle timestamps must not be supplied at draft creation")
	}
	if strings.TrimSpace(invoice.AmountPaid) != "" {
		paid, _, parseErr := parseMerchantInvoiceDecimal(invoice.AmountPaid, "amount_paid", false)
		if parseErr != nil || paid.Sign() != 0 {
			return nil, merchantInvoiceInvalidInput("amount_paid must be zero at draft creation")
		}
	}

	id := invoice.ID
	if id == uuid.Nil {
		id = uuid.New()
	}

	const query = `
		INSERT INTO merchant_invoices (
			id,
			merchant_id,
			invoice_number,
			subtotal_amount,
			adjustment_amount,
			total_amount,
			amount_paid,
			currency
		)
		VALUES ($1, $2, $3, $4::numeric, $5::numeric, $6::numeric, 0, $7)
		RETURNING ` + merchantInvoiceSelectColumns

	var result MerchantInvoice
	if err := scanMerchantInvoice(
		querier.QueryRow(ctx, query, id, invoice.MerchantID, invoiceNumber, subtotal, adjustment, total, currency),
		&result,
	); err != nil {
		err = classifyMerchantInvoiceWriteError(err)
		logger.Error("Insert merchant invoice draft failed", err, "merchant_id", invoice.MerchantID, "invoice_number", invoiceNumber)
		return nil, err
	}
	if err := validateMerchantInvoicePersistedState(&result); err != nil {
		return nil, err
	}
	logger.Info(
		"Insert merchant invoice draft successful",
		"invoice_id", result.ID,
		"merchant_id", result.MerchantID,
		"invoice_number", result.InvoiceNumber,
		"status", result.InvoiceStatus,
	)
	return &result, nil
}

// InsertDraft creates a draft merchant invoice through the model pool.
func (m *MerchantInvoiceModel) InsertDraft(ctx context.Context, invoice *MerchantInvoice) (*MerchantInvoice, error) {
	if err := m.validatePool(); err != nil {
		return nil, err
	}
	return m.insertDraft(ctx, m.DB, "InsertMerchantInvoiceDraft", invoice)
}

// InsertDraftTx creates a draft merchant invoice through caller-owned tx.
func (m *MerchantInvoiceModel) InsertDraftTx(ctx context.Context, tx pgx.Tx, invoice *MerchantInvoice) (*MerchantInvoice, error) {
	if err := m.validateBase(); err != nil {
		return nil, err
	}
	if tx == nil {
		return nil, merchantInvoiceInvalidInput("transaction is required")
	}
	return m.insertDraft(ctx, tx, "InsertMerchantInvoiceDraftTx", invoice)
}

func (m *MerchantInvoiceModel) updateDraftFinancials(
	ctx context.Context,
	querier merchantInvoiceQueryRower,
	functionName string,
	id uuid.UUID,
	financials MerchantInvoiceDraftFinancials,
) (*MerchantInvoice, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName(functionName)
	if err := validateMerchantInvoiceID(id); err != nil {
		return nil, err
	}
	subtotal, adjustment, total, currency, err := normalizeMerchantInvoiceFinancials(
		financials.SubtotalAmount,
		financials.AdjustmentAmount,
		financials.Currency,
	)
	if err != nil {
		return nil, err
	}

	const query = `
		UPDATE merchant_invoices
		SET
			subtotal_amount = $2::numeric,
			adjustment_amount = $3::numeric,
			total_amount = $4::numeric,
			currency = $5,
			updated_at = NOW()
		WHERE id = $1
		  AND invoice_status = 'draft'
		RETURNING ` + merchantInvoiceSelectColumns

	var invoice MerchantInvoice
	if err := scanMerchantInvoice(querier.QueryRow(ctx, query, id, subtotal, adjustment, total, currency), &invoice); err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			err = classifyMerchantInvoiceWriteError(err)
			logger.Error("Update merchant invoice draft financials failed", err, "invoice_id", id)
			return nil, err
		}
		current, getErr := m.getByIDViaQuerier(ctx, querier, id)
		if getErr != nil {
			return nil, getErr
		}
		if current == nil {
			return nil, ErrMerchantInvoiceNotFound
		}
		return nil, ErrMerchantInvoiceInvalidTransition
	}
	if err := validateMerchantInvoicePersistedState(&invoice); err != nil {
		return nil, err
	}
	logger.Info(
		"Update merchant invoice draft financials successful",
		"invoice_id", invoice.ID,
		"merchant_id", invoice.MerchantID,
		"invoice_number", invoice.InvoiceNumber,
		"status", invoice.InvoiceStatus,
	)
	return &invoice, nil
}

// UpdateDraftFinancials replaces the complete mutable financial snapshot of a draft invoice.
func (m *MerchantInvoiceModel) UpdateDraftFinancials(
	ctx context.Context,
	id uuid.UUID,
	financials MerchantInvoiceDraftFinancials,
) (*MerchantInvoice, error) {
	if err := m.validatePool(); err != nil {
		return nil, err
	}
	return m.updateDraftFinancials(ctx, m.DB, "UpdateMerchantInvoiceDraftFinancials", id, financials)
}

// UpdateDraftFinancialsTx is the transaction-aware form of UpdateDraftFinancials.
func (m *MerchantInvoiceModel) UpdateDraftFinancialsTx(
	ctx context.Context,
	tx pgx.Tx,
	id uuid.UUID,
	financials MerchantInvoiceDraftFinancials,
) (*MerchantInvoice, error) {
	if err := m.validateBase(); err != nil {
		return nil, err
	}
	if tx == nil {
		return nil, merchantInvoiceInvalidInput("transaction is required")
	}
	return m.updateDraftFinancials(ctx, tx, "UpdateMerchantInvoiceDraftFinancialsTx", id, financials)
}

// GetByID retrieves a merchant invoice by ID. Absence returns nil, nil.
func (m *MerchantInvoiceModel) GetByID(ctx context.Context, id uuid.UUID) (*MerchantInvoice, error) {
	if err := m.validatePool(); err != nil {
		return nil, err
	}
	if err := validateMerchantInvoiceID(id); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()
	invoice, err := m.getByIDViaQuerier(ctx, m.DB, id)
	if err != nil {
		return nil, fmt.Errorf("get merchant invoice by ID: %w", err)
	}
	return invoice, nil
}

// GetByIDForUpdateTx retrieves and row-locks one invoice through caller-owned tx. Absence returns nil, nil.
func (m *MerchantInvoiceModel) GetByIDForUpdateTx(ctx context.Context, tx pgx.Tx, id uuid.UUID) (*MerchantInvoice, error) {
	if err := m.validateBase(); err != nil {
		return nil, err
	}
	if tx == nil {
		return nil, merchantInvoiceInvalidInput("transaction is required")
	}
	if err := validateMerchantInvoiceID(id); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	const query = `
		SELECT ` + merchantInvoiceSelectColumns + `
		FROM merchant_invoices
		WHERE id = $1
		FOR UPDATE
	`
	var invoice MerchantInvoice
	if err := scanMerchantInvoice(tx.QueryRow(ctx, query, id), &invoice); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("get merchant invoice by ID for update: %w", err)
	}
	if err := validateMerchantInvoicePersistedState(&invoice); err != nil {
		return nil, err
	}
	return &invoice, nil
}

// GetByInvoiceNumber retrieves an invoice by its unique invoice number. Absence returns nil, nil.
func (m *MerchantInvoiceModel) GetByInvoiceNumber(ctx context.Context, invoiceNumber string) (*MerchantInvoice, error) {
	if err := m.validatePool(); err != nil {
		return nil, err
	}
	invoiceNumber, err := normalizeMerchantInvoiceNumber(invoiceNumber)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	const query = `
		SELECT ` + merchantInvoiceSelectColumns + `
		FROM merchant_invoices
		WHERE invoice_number = $1
	`
	var invoice MerchantInvoice
	if err := scanMerchantInvoice(m.DB.QueryRow(ctx, query, invoiceNumber), &invoice); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("get merchant invoice by invoice number: %w", err)
	}
	if err := validateMerchantInvoicePersistedState(&invoice); err != nil {
		return nil, err
	}
	return &invoice, nil
}

// ListByMerchant returns bounded keyset-paginated merchant invoice history ordered newest first.
func (m *MerchantInvoiceModel) ListByMerchant(
	ctx context.Context,
	merchantID uuid.UUID,
	limit int,
	beforeCreatedAt *time.Time,
	beforeID *uuid.UUID,
) ([]*MerchantInvoice, error) {
	return m.listByMerchant(ctx, merchantID, nil, limit, beforeCreatedAt, beforeID)
}

// ListByMerchantAndStatus returns bounded keyset-paginated merchant invoice history filtered by status.
func (m *MerchantInvoiceModel) ListByMerchantAndStatus(
	ctx context.Context,
	merchantID uuid.UUID,
	status MerchantInvoiceStatus,
	limit int,
	beforeCreatedAt *time.Time,
	beforeID *uuid.UUID,
) ([]*MerchantInvoice, error) {
	status = NormalizeMerchantInvoiceStatus(status)
	if !IsValidMerchantInvoiceStatus(status) {
		return nil, merchantInvoiceInvalidInput("invalid invoice status: %q", status)
	}
	return m.listByMerchant(ctx, merchantID, &status, limit, beforeCreatedAt, beforeID)
}

func (m *MerchantInvoiceModel) listByMerchant(
	ctx context.Context,
	merchantID uuid.UUID,
	status *MerchantInvoiceStatus,
	limit int,
	beforeCreatedAt *time.Time,
	beforeID *uuid.UUID,
) ([]*MerchantInvoice, error) {
	if err := m.validatePool(); err != nil {
		return nil, err
	}
	if err := validateMerchantInvoiceMerchantID(merchantID); err != nil {
		return nil, err
	}
	if err := validateMerchantInvoiceLimit(limit); err != nil {
		return nil, err
	}
	if err := validateMerchantInvoiceCreatedCursor(beforeCreatedAt, beforeID); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	var rows pgx.Rows
	var err error
	switch {
	case status == nil && beforeCreatedAt == nil:
		const query = `
			SELECT ` + merchantInvoiceSelectColumns + `
			FROM merchant_invoices
			WHERE merchant_id = $1
			ORDER BY created_at DESC, id DESC
			LIMIT $2
		`
		rows, err = m.DB.Query(ctx, query, merchantID, limit)
	case status == nil:
		const query = `
			SELECT ` + merchantInvoiceSelectColumns + `
			FROM merchant_invoices
			WHERE merchant_id = $1
			  AND (created_at, id) < ($2, $3)
			ORDER BY created_at DESC, id DESC
			LIMIT $4
		`
		rows, err = m.DB.Query(ctx, query, merchantID, beforeCreatedAt.UTC(), *beforeID, limit)
	case beforeCreatedAt == nil:
		const query = `
			SELECT ` + merchantInvoiceSelectColumns + `
			FROM merchant_invoices
			WHERE merchant_id = $1
			  AND invoice_status = $2
			ORDER BY created_at DESC, id DESC
			LIMIT $3
		`
		rows, err = m.DB.Query(ctx, query, merchantID, *status, limit)
	default:
		const query = `
			SELECT ` + merchantInvoiceSelectColumns + `
			FROM merchant_invoices
			WHERE merchant_id = $1
			  AND invoice_status = $2
			  AND (created_at, id) < ($3, $4)
			ORDER BY created_at DESC, id DESC
			LIMIT $5
		`
		rows, err = m.DB.Query(ctx, query, merchantID, *status, beforeCreatedAt.UTC(), *beforeID, limit)
	}
	if err != nil {
		return nil, fmt.Errorf("list merchant invoices by merchant: %w", err)
	}
	defer rows.Close()

	invoices := make([]*MerchantInvoice, 0, limit)
	for rows.Next() {
		var invoice MerchantInvoice
		if err := scanMerchantInvoice(rows, &invoice); err != nil {
			return nil, err
		}
		if err := validateMerchantInvoicePersistedState(&invoice); err != nil {
			return nil, err
		}
		invoices = append(invoices, &invoice)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return invoices, nil
}

// ListIssuedDueForOverdueProcessing returns issued invoices whose due point is at or before cutoff.
//
// The caller supplies cutoff. This method does not decide scheduler cadence or commercial grace policy.
func (m *MerchantInvoiceModel) ListIssuedDueForOverdueProcessing(
	ctx context.Context,
	cutoff time.Time,
	limit int,
	afterDueAt *time.Time,
	afterID *uuid.UUID,
) ([]*MerchantInvoice, error) {
	if err := m.validatePool(); err != nil {
		return nil, err
	}
	if cutoff.IsZero() {
		return nil, merchantInvoiceInvalidInput("cutoff is required")
	}
	if err := validateMerchantInvoiceLimit(limit); err != nil {
		return nil, err
	}
	if err := validateMerchantInvoiceDueCursor(afterDueAt, afterID); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	var rows pgx.Rows
	var err error
	if afterDueAt == nil {
		const query = `
			SELECT ` + merchantInvoiceSelectColumns + `
			FROM merchant_invoices
			WHERE invoice_status = 'issued'
			  AND due_at IS NOT NULL
			  AND due_at <= $1
			ORDER BY due_at ASC, id ASC
			LIMIT $2
		`
		rows, err = m.DB.Query(ctx, query, cutoff.UTC(), limit)
	} else {
		const query = `
			SELECT ` + merchantInvoiceSelectColumns + `
			FROM merchant_invoices
			WHERE invoice_status = 'issued'
			  AND due_at IS NOT NULL
			  AND due_at <= $1
			  AND (due_at, id) > ($2, $3)
			ORDER BY due_at ASC, id ASC
			LIMIT $4
		`
		rows, err = m.DB.Query(ctx, query, cutoff.UTC(), afterDueAt.UTC(), *afterID, limit)
	}
	if err != nil {
		return nil, fmt.Errorf("list issued merchant invoices due for overdue processing: %w", err)
	}
	defer rows.Close()

	invoices := make([]*MerchantInvoice, 0, limit)
	for rows.Next() {
		var invoice MerchantInvoice
		if err := scanMerchantInvoice(rows, &invoice); err != nil {
			return nil, err
		}
		if err := validateMerchantInvoicePersistedState(&invoice); err != nil {
			return nil, err
		}
		invoices = append(invoices, &invoice)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return invoices, nil
}

func (m *MerchantInvoiceModel) resolveSimpleTransitionNoMatch(
	ctx context.Context,
	querier merchantInvoiceQueryRower,
	id uuid.UUID,
	target MerchantInvoiceStatus,
) (*MerchantInvoice, error) {
	current, err := m.getByIDViaQuerier(ctx, querier, id)
	if err != nil {
		return nil, err
	}
	if current == nil {
		return nil, ErrMerchantInvoiceNotFound
	}
	if NormalizeMerchantInvoiceStatus(current.InvoiceStatus) == target {
		return current, nil
	}
	return nil, ErrMerchantInvoiceInvalidTransition
}

func (m *MerchantInvoiceModel) simpleTransition(
	ctx context.Context,
	querier merchantInvoiceQueryRower,
	functionName string,
	query string,
	id uuid.UUID,
	target MerchantInvoiceStatus,
) (*MerchantInvoice, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()
	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName(functionName)
	if err := validateMerchantInvoiceID(id); err != nil {
		return nil, err
	}

	var invoice MerchantInvoice
	if err := scanMerchantInvoice(querier.QueryRow(ctx, query, id), &invoice); err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			err = classifyMerchantInvoiceWriteError(err)
			logger.Error("Merchant invoice lifecycle transition failed", err, "invoice_id", id, "to_status", target)
			return nil, err
		}
		return m.resolveSimpleTransitionNoMatch(ctx, querier, id, target)
	}
	if err := validateMerchantInvoicePersistedState(&invoice); err != nil {
		return nil, err
	}
	logger.Info(
		"Merchant invoice lifecycle transition successful",
		"invoice_id", invoice.ID,
		"merchant_id", invoice.MerchantID,
		"status", invoice.InvoiceStatus,
	)
	return &invoice, nil
}

func normalizeOptionalDueAt(dueAt *time.Time) *time.Time {
	if dueAt == nil {
		return nil
	}
	value := dueAt.UTC().Truncate(time.Microsecond)
	return &value
}

func sameOptionalTime(a, b *time.Time) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return a.Equal(*b)
}

func (m *MerchantInvoiceModel) issue(
	ctx context.Context,
	querier merchantInvoiceQueryRower,
	functionName string,
	id uuid.UUID,
	dueAt *time.Time,
) (*MerchantInvoice, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()
	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName(functionName)
	if err := validateMerchantInvoiceID(id); err != nil {
		return nil, err
	}
	dueAt = normalizeOptionalDueAt(dueAt)

	const query = `
		UPDATE merchant_invoices
		SET
			invoice_status = 'issued',
			issued_at = NOW(),
			due_at = $2::timestamptz,
			updated_at = NOW()
		WHERE id = $1
		  AND invoice_status = 'draft'
		RETURNING ` + merchantInvoiceSelectColumns

	var invoice MerchantInvoice
	if err := scanMerchantInvoice(querier.QueryRow(ctx, query, id, dueAt), &invoice); err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			err = classifyMerchantInvoiceWriteError(err)
			logger.Error("Issue merchant invoice failed", err, "invoice_id", id)
			return nil, err
		}
		current, getErr := m.getByIDViaQuerier(ctx, querier, id)
		if getErr != nil {
			return nil, getErr
		}
		if current == nil {
			return nil, ErrMerchantInvoiceNotFound
		}
		if NormalizeMerchantInvoiceStatus(current.InvoiceStatus) == MerchantInvoiceStatusIssued && sameOptionalTime(current.DueAt, dueAt) {
			return current, nil
		}
		return nil, ErrMerchantInvoiceInvalidTransition
	}
	if err := validateMerchantInvoicePersistedState(&invoice); err != nil {
		return nil, err
	}
	logger.Info(
		"Issue merchant invoice successful",
		"invoice_id", invoice.ID,
		"merchant_id", invoice.MerchantID,
		"status", invoice.InvoiceStatus,
		"due_at", invoice.DueAt,
	)
	return &invoice, nil
}

// Issue transitions a draft invoice to issued with an optional caller-supplied due date.
func (m *MerchantInvoiceModel) Issue(ctx context.Context, id uuid.UUID, dueAt *time.Time) (*MerchantInvoice, error) {
	if err := m.validatePool(); err != nil {
		return nil, err
	}
	return m.issue(ctx, m.DB, "IssueMerchantInvoice", id, dueAt)
}

// IssueTx is the transaction-aware form of Issue.
func (m *MerchantInvoiceModel) IssueTx(ctx context.Context, tx pgx.Tx, id uuid.UUID, dueAt *time.Time) (*MerchantInvoice, error) {
	if err := m.validateBase(); err != nil {
		return nil, err
	}
	if tx == nil {
		return nil, merchantInvoiceInvalidInput("transaction is required")
	}
	return m.issue(ctx, tx, "IssueMerchantInvoiceTx", id, dueAt)
}

const merchantInvoiceIssueDueNowQuery = `
	UPDATE merchant_invoices
	SET
		invoice_status = 'issued',
		issued_at = NOW(),
		due_at = NOW(),
		updated_at = NOW()
	WHERE id = $1
	  AND invoice_status = 'draft'
	RETURNING ` + merchantInvoiceSelectColumns

func (m *MerchantInvoiceModel) issueDueNow(
	ctx context.Context,
	querier merchantInvoiceQueryRower,
	functionName string,
	id uuid.UUID,
) (*MerchantInvoice, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()
	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName(functionName)
	if err := validateMerchantInvoiceID(id); err != nil {
		return nil, err
	}
	var invoice MerchantInvoice
	if err := scanMerchantInvoice(querier.QueryRow(ctx, merchantInvoiceIssueDueNowQuery, id), &invoice); err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			err = classifyMerchantInvoiceWriteError(err)
			logger.Error("Issue merchant invoice due now failed", err, "invoice_id", id)
			return nil, err
		}
		current, getErr := m.getByIDViaQuerier(ctx, querier, id)
		if getErr != nil {
			return nil, getErr
		}
		if current == nil {
			return nil, ErrMerchantInvoiceNotFound
		}
		if NormalizeMerchantInvoiceStatus(current.InvoiceStatus) == MerchantInvoiceStatusIssued &&
			current.IssuedAt != nil && current.DueAt != nil && current.DueAt.Equal(*current.IssuedAt) {
			return current, nil
		}
		return nil, ErrMerchantInvoiceInvalidTransition
	}
	if err := validateMerchantInvoicePersistedState(&invoice); err != nil {
		return nil, err
	}
	logger.Info(
		"Issue merchant invoice due now successful",
		"invoice_id", invoice.ID,
		"merchant_id", invoice.MerchantID,
		"status", invoice.InvoiceStatus,
		"due_at", invoice.DueAt,
	)
	return &invoice, nil
}

// IssueDueNow transitions a draft invoice to issued and uses one database timestamp for issued_at and due_at.
func (m *MerchantInvoiceModel) IssueDueNow(ctx context.Context, id uuid.UUID) (*MerchantInvoice, error) {
	if err := m.validatePool(); err != nil {
		return nil, err
	}
	return m.issueDueNow(ctx, m.DB, "IssueMerchantInvoiceDueNow", id)
}

// IssueDueNowTx is the transaction-aware form of IssueDueNow.
func (m *MerchantInvoiceModel) IssueDueNowTx(ctx context.Context, tx pgx.Tx, id uuid.UUID) (*MerchantInvoice, error) {
	if err := m.validateBase(); err != nil {
		return nil, err
	}
	if tx == nil {
		return nil, merchantInvoiceInvalidInput("transaction is required")
	}
	return m.issueDueNow(ctx, tx, "IssueMerchantInvoiceDueNowTx", id)
}

const merchantInvoiceSettleZeroBalanceQuery = `
	UPDATE merchant_invoices
	SET
		invoice_status = 'paid',
		paid_at = NOW(),
		updated_at = NOW()
	WHERE id = $1
	  AND invoice_status = 'issued'
	  AND total_amount = 0
	  AND amount_paid = 0
	RETURNING ` + merchantInvoiceSelectColumns

func (m *MerchantInvoiceModel) settleZeroBalance(
	ctx context.Context,
	querier merchantInvoiceQueryRower,
	functionName string,
	id uuid.UUID,
) (*MerchantInvoice, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()
	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName(functionName)
	if err := validateMerchantInvoiceID(id); err != nil {
		return nil, err
	}

	var invoice MerchantInvoice
	if err := scanMerchantInvoice(querier.QueryRow(ctx, merchantInvoiceSettleZeroBalanceQuery, id), &invoice); err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			err = classifyMerchantInvoiceWriteError(err)
			logger.Error("Settle zero-balance merchant invoice failed", err, "invoice_id", id)
			return nil, err
		}
		current, getErr := m.getByIDViaQuerier(ctx, querier, id)
		if getErr != nil {
			return nil, getErr
		}
		if current == nil {
			return nil, ErrMerchantInvoiceNotFound
		}
		total, ok := new(big.Rat).SetString(current.TotalAmount)
		if !ok {
			return nil, ErrMerchantInvoiceInvalidState
		}
		if NormalizeMerchantInvoiceStatus(current.InvoiceStatus) == MerchantInvoiceStatusPaid && total.Sign() == 0 {
			return current, nil
		}
		return nil, ErrMerchantInvoiceInvalidTransition
	}
	if err := validateMerchantInvoicePersistedState(&invoice); err != nil {
		return nil, err
	}
	logger.Info(
		"Settle zero-balance merchant invoice successful",
		"invoice_id", invoice.ID,
		"merchant_id", invoice.MerchantID,
		"status", invoice.InvoiceStatus,
	)
	return &invoice, nil
}

// SettleZeroBalance transitions an issued zero-total invoice to paid without recording a payment.
func (m *MerchantInvoiceModel) SettleZeroBalance(ctx context.Context, id uuid.UUID) (*MerchantInvoice, error) {
	if err := m.validatePool(); err != nil {
		return nil, err
	}
	return m.settleZeroBalance(ctx, m.DB, "SettleMerchantInvoiceZeroBalance", id)
}

// SettleZeroBalanceTx is the transaction-aware form of SettleZeroBalance.
func (m *MerchantInvoiceModel) SettleZeroBalanceTx(ctx context.Context, tx pgx.Tx, id uuid.UUID) (*MerchantInvoice, error) {
	if err := m.validateBase(); err != nil {
		return nil, err
	}
	if tx == nil {
		return nil, merchantInvoiceInvalidInput("transaction is required")
	}
	return m.settleZeroBalance(ctx, tx, "SettleMerchantInvoiceZeroBalanceTx", id)
}

const merchantInvoiceMarkOverdueQuery = `
	UPDATE merchant_invoices
	SET
		invoice_status = 'overdue',
		updated_at = NOW()
	WHERE id = $1
	  AND invoice_status = 'issued'
	  AND due_at IS NOT NULL
	  AND due_at < NOW()
	  AND amount_paid < total_amount
	RETURNING ` + merchantInvoiceSelectColumns

// MarkOverdue transitions an issued invoice with an unpaid balance beyond due_at to overdue.
func (m *MerchantInvoiceModel) MarkOverdue(ctx context.Context, id uuid.UUID) (*MerchantInvoice, error) {
	if err := m.validatePool(); err != nil {
		return nil, err
	}
	return m.simpleTransition(ctx, m.DB, "MarkMerchantInvoiceOverdue", merchantInvoiceMarkOverdueQuery, id, MerchantInvoiceStatusOverdue)
}

// MarkOverdueTx is the transaction-aware form of MarkOverdue.
func (m *MerchantInvoiceModel) MarkOverdueTx(ctx context.Context, tx pgx.Tx, id uuid.UUID) (*MerchantInvoice, error) {
	if err := m.validateBase(); err != nil {
		return nil, err
	}
	if tx == nil {
		return nil, merchantInvoiceInvalidInput("transaction is required")
	}
	return m.simpleTransition(ctx, tx, "MarkMerchantInvoiceOverdueTx", merchantInvoiceMarkOverdueQuery, id, MerchantInvoiceStatusOverdue)
}

const merchantInvoiceVoidQuery = `
	UPDATE merchant_invoices
	SET
		invoice_status = 'void',
		voided_at = NOW(),
		updated_at = NOW()
	WHERE id = $1
	  AND invoice_status = 'issued'
	  AND amount_paid = 0
	RETURNING ` + merchantInvoiceSelectColumns

// Void transitions an issued unpaid invoice to terminal void state.
func (m *MerchantInvoiceModel) Void(ctx context.Context, id uuid.UUID) (*MerchantInvoice, error) {
	if err := m.validatePool(); err != nil {
		return nil, err
	}
	return m.simpleTransition(ctx, m.DB, "VoidMerchantInvoice", merchantInvoiceVoidQuery, id, MerchantInvoiceStatusVoid)
}

// VoidTx is the transaction-aware form of Void.
func (m *MerchantInvoiceModel) VoidTx(ctx context.Context, tx pgx.Tx, id uuid.UUID) (*MerchantInvoice, error) {
	if err := m.validateBase(); err != nil {
		return nil, err
	}
	if tx == nil {
		return nil, merchantInvoiceInvalidInput("transaction is required")
	}
	return m.simpleTransition(ctx, tx, "VoidMerchantInvoiceTx", merchantInvoiceVoidQuery, id, MerchantInvoiceStatusVoid)
}

// ApplyPaymentTx atomically applies a positive payment increment to an open invoice.
//
// paymentAmount is the amount explained by the authoritative payment record being
// persisted in the same caller-owned transaction. This method never executes a
// payment and has no pool-backed counterpart. Retry idempotency must come from
// the composed payment record's durable uniqueness/idempotency boundary; if that
// payment insert fails as a duplicate, the transaction must roll back this increment.
func (m *MerchantInvoiceModel) ApplyPaymentTx(
	ctx context.Context,
	tx pgx.Tx,
	id uuid.UUID,
	paymentAmount string,
) (*MerchantInvoice, error) {
	if err := m.validateBase(); err != nil {
		return nil, err
	}
	if tx == nil {
		return nil, merchantInvoiceInvalidInput("transaction is required")
	}
	if err := validateMerchantInvoiceID(id); err != nil {
		return nil, err
	}
	paymentRat, normalizedPayment, err := parseMerchantInvoiceDecimal(paymentAmount, "payment_amount", false)
	if err != nil {
		return nil, err
	}
	if paymentRat.Sign() <= 0 {
		return nil, merchantInvoiceInvalidInput("payment_amount must be greater than zero")
	}

	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()
	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("ApplyMerchantInvoicePaymentTx")

	const query = `
		UPDATE merchant_invoices
		SET
			amount_paid = amount_paid + $2::numeric,
			invoice_status = CASE
				WHEN amount_paid + $2::numeric = total_amount THEN 'paid'
				ELSE 'partially_paid'
			END,
			paid_at = CASE
				WHEN amount_paid + $2::numeric = total_amount THEN NOW()
				ELSE NULL
			END,
			updated_at = NOW()
		WHERE id = $1
		  AND invoice_status IN ('issued', 'partially_paid', 'overdue')
		  AND amount_paid + $2::numeric <= total_amount
		RETURNING ` + merchantInvoiceSelectColumns

	var invoice MerchantInvoice
	if err := scanMerchantInvoice(tx.QueryRow(ctx, query, id, normalizedPayment), &invoice); err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			err = classifyMerchantInvoiceWriteError(err)
			logger.Error("Apply merchant invoice payment failed", err, "invoice_id", id)
			return nil, err
		}
		current, getErr := m.getByIDViaQuerier(ctx, tx, id)
		if getErr != nil {
			return nil, getErr
		}
		if current == nil {
			return nil, ErrMerchantInvoiceNotFound
		}
		status := NormalizeMerchantInvoiceStatus(current.InvoiceStatus)
		if status == MerchantInvoiceStatusPaid || status == MerchantInvoiceStatusVoid || status == MerchantInvoiceStatusDraft {
			return nil, ErrMerchantInvoiceInvalidTransition
		}
		return nil, ErrMerchantInvoicePaymentExceedsBalance
	}
	if err := validateMerchantInvoicePersistedState(&invoice); err != nil {
		return nil, err
	}
	logger.Info(
		"Apply merchant invoice payment successful",
		"invoice_id", invoice.ID,
		"merchant_id", invoice.MerchantID,
		"status", invoice.InvoiceStatus,
		"payment_amount", normalizedPayment,
		"amount_paid", invoice.AmountPaid,
	)
	return &invoice, nil
}
