// Package data provides models and database access methods for merchant
// invoice items.
//
// sdworkspace/sdbackend/internal/data/merchant_invoice_items.go
//
// GTM:
//
//	Layer: 2.4.b Commerce Architecture / Monetization Layer
//	Release Class: SPINE
//	Reason:
//	  merchant_invoice_items is the normalized line-level bridge between
//	  merchant_fee_calculations and merchant_invoices. It preserves the
//	  auditable identity of what was charged on an invoice without absorbing
//	  the responsibilities of the domains on either side of it.
//
// Domain Boundary:
//
//	A merchant invoice item identifies one product or service being charged on
//	an invoice, expressed as quantity x unit_amount = line_amount. It is not:
//
//	  - a fee calculator or fee schedule;
//	  - a platform-credit application or platform-credit ledger;
//	  - a rebate, discount, or commercial adjustment record;
//	  - a payment, payment attempt, or settlement adjustment;
//	  - an invoice header or invoice-lifecycle substitute; or
//	  - a container for engagement-event line items (Watch, Waitlist, Beta, and
//	    similar consumer actions are intelligence dimensions, not separately
//	    billed products).
//
//	Invoice items may represent any canonical Sagrenti product/service charge
//	that has reached fee calculation, including Anticipation Intelligence
//	Activation Fees, Anticipation Intelligence usage fees, and Subscription Fees
//	where configured. Individual consumer engagement actions are not invoice
//	products.
//
// Provenance:
//
//	fee_calculation_id is the sole direct provenance reference and is
//	NOT NULL / ON DELETE RESTRICT. billable_event_id is deliberately not
//	duplicated onto this table: merchant_fee_calculations owns the calculation
//	provenance upstream, and this model reaches that history through the
//	fee_calculation_id rather than creating a second source of truth.
//
//	Each invoice item represents exactly one independently calculated
//	product/service charge, and each fee calculation may appear on at most one
//	invoice item. The database owns that anti-double-invoicing boundary through
//	UNIQUE(fee_calculation_id).
//
//	Description, quantity, unit_amount, and line_amount preserve the truthful
//	gross product/service charge represented by the referenced fee calculation.
//	For the supplied quantity, unit_amount and line_amount must represent the
//	calculated charge before Platform Credits, rebates, discounts, taxes,
//	surcharges, payments, or settlement adjustments are applied.
//
//	Those monetary effects do not become invoice items and must never be
//	disguised by rewriting quantity, unit_amount, or line_amount. They remain
//	separate structured records in their owning domains and may be associated
//	with the applicable charge by invoice composition/presentation logic.
//
//	Cross-table validation that the invoice item faithfully represents the
//	referenced fee calculation, belongs to the same merchant, uses the invoice
//	currency, and is eligible for invoicing belongs to service orchestration.
//
// Currency:
//
//	This table stores no currency column. An invoice item inherits the single
//	currency of its parent merchant_invoices row. Duplicating currency here
//	would create a second, potentially divergent source of truth for a value
//	merchant_invoices already owns exclusively.
//
// Lifecycle:
//
//	An invoice item may be inserted only while its parent invoice is draft,
//	and may be removed only while its parent invoice is draft. Both mutations
//	row-lock and verify the parent invoice within the same SQL statement as the
//	write, rather than relying on a race-prone SELECT-then-write sequence. Once
//	the parent invoice leaves draft, an invoice
//	item is permanent financial history: this model exposes no update,
//	upsert, soft-delete, or restore method at any lifecycle stage. A
//	correction while drafting is performed by removing and reinserting a
//	line, never by mutating a persisted row in place.
//
// Transaction Boundary:
//
//	Draft line insertion and removal are transaction-aware only. A line-item
//	mutation changes the invoice subtotal and therefore must never be persisted
//	as a legitimate standalone write that leaves the invoice header financially
//	out of sync. InsertDraftLineTx and RemoveDraftLineTx accept caller-owned
//	pgx.Tx values and never begin, commit, or roll them back.
//
//	Both mutations row-lock the parent invoice and require it to remain draft in
//	the same SQL statement as the write. The caller then uses
//	SumLineAmountByInvoiceIDTx and MerchantInvoiceModel.UpdateDraftFinancialsTx
//	inside that same transaction before commit.
//
//	SumLineAmountByInvoiceIDTx performs no locking of its own. Callers that use it
//	without first mutating a line must lock the parent invoice via
//	MerchantInvoiceModel.GetByIDForUpdateTx before relying on the aggregate for
//	reconciliation or issuance.
//
// Cross-Table Invariants Not Enforced Here:
//
//	This table cannot relationally prove that its parent invoice and
//	referenced fee calculation belong to the same merchant, or that the
//	invoice's currency matches the calculation's currency, or that the
//	calculation's lifecycle state is appropriate for the invoicing workflow, or
//	that description/quantity/unit_amount/line_amount faithfully represent the
//	calculated charge. Those are service-orchestration responsibilities using
//	the authoritative MerchantInvoiceModel, MerchantFeeCalculationModel, and any
//	relevant commercial-adjustment/credit records. Engineering must enforce
//	financial truth; Administration governs only the variable commercial terms
//	within those safe boundaries.
//
// Monetary Precision:
//
//	quantity and unit_amount are validated as NUMERIC(19,4)-compatible decimal
//	strings using exact big.Rat arithmetic; PostgreSQL NUMERIC values are never
//	converted through float32/float64. line_amount is never caller-supplied:
//	it is computed server-side as the exact rational product of quantity and
//	unit_amount and rejected outright if that product cannot be represented
//	exactly at four fractional digits. This mirrors and precedes the database
//	CHECK constraint enforcing line_amount = quantity * unit_amount.
//
// SPINE Rule:
//
//	Keep compiling.
//	Keep production-ready.
//	Preserve exact decimal-string handling.
//	Preserve fee_calculation_id as sole direct provenance; never duplicate
//	billable_event_id onto this table.
//	Preserve the one-invoice-item-per-fee-calculation idempotency boundary.
//	Preserve permanent immutability of description/quantity/unit_amount/
//	line_amount after insertion.
//	Preserve draft-only insertion and removal, guarded within the write
//	statement itself.
//	Preserve currency inheritance from merchant_invoices; never add a local
//	currency column.
//	Do not expose generic update, upsert, delete, soft-delete, or restore
//	methods.
//	Do not represent Platform Credits, rebates, discounts, payments, or
//	settlement adjustments as invoice items.
//	Do not represent individual consumer engagement actions as invoice items.
//	Block deployment if this file breaks build, monetary precision, provenance
//	integrity, idempotency, lifecycle integrity, concurrency safety, or
//	invoice-reconciliation readiness.
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

// -----------------------------------------------------------------------------
// Structural bounds and canonical projections
// -----------------------------------------------------------------------------

const (
	merchantInvoiceItemMaxListLimit         = 100
	merchantInvoiceItemDescriptionMaxLength = 500
)

var merchantInvoiceItemAmountPattern = regexp.MustCompile(`^[0-9]{1,15}(?:\.[0-9]{1,4})?$`)

const merchantInvoiceItemSelectColumns = `
	id,
	invoice_id,
	fee_calculation_id,
	description,
	quantity,
	unit_amount,
	line_amount,
	created_at
`

// -----------------------------------------------------------------------------
// Persisted constraint names
// -----------------------------------------------------------------------------

// The following constant values must match the constraint names defined in
// the authoritative migration for merchant_invoice_items. Stable
// classification of foreign-key, uniqueness, and check failures depends on
// these names remaining aligned with the persisted schema.
const (
	merchantInvoiceItemInvoiceFKConstraint        = "fk_merchant_invoice_items_invoice"
	merchantInvoiceItemFeeCalculationFKConstraint = "fk_merchant_invoice_items_fee_calculation"

	merchantInvoiceItemFeeCalculationUniqueConstraint = "uq_merchant_invoice_items_fee_calculation"

	merchantInvoiceItemDescriptionLengthConstraint  = "chk_merchant_invoice_items_description_length"
	merchantInvoiceItemDescriptionTrimmedConstraint = "chk_merchant_invoice_items_description_trimmed"
	merchantInvoiceItemLineAmountConstraint         = "chk_merchant_invoice_items_line_amount"
	merchantInvoiceItemQuantityCheckConstraint      = "chk_merchant_invoice_items_quantity"
	merchantInvoiceItemUnitAmountCheckConstraint    = "chk_merchant_invoice_items_unit_amount"
)

// -----------------------------------------------------------------------------
// Entity and model
// -----------------------------------------------------------------------------

// MerchantInvoiceItem represents one durable, normalized invoice line.
//
// Description, Quantity, UnitAmount, and LineAmount are immutable after
// insertion. There is no update path for a persisted row through this model.
type MerchantInvoiceItem struct {
	ID               uuid.UUID `json:"id" db:"id"`
	InvoiceID        uuid.UUID `json:"invoice_id" db:"invoice_id"`
	FeeCalculationID uuid.UUID `json:"fee_calculation_id" db:"fee_calculation_id"`
	Description      string    `json:"description" db:"description"`
	Quantity         string    `json:"quantity" db:"quantity"`
	UnitAmount       string    `json:"unit_amount" db:"unit_amount"`
	LineAmount       string    `json:"line_amount" db:"line_amount"`
	CreatedAt        time.Time `json:"created_at" db:"created_at"`
}

// MerchantInvoiceItemDraftLine is the caller-supplied contract for creating
// one invoice line while the parent invoice remains draft.
//
// LineAmount is deliberately absent: this model computes it as the exact
// rational product of Quantity and UnitAmount and rejects the operation if
// that product cannot be represented exactly at NUMERIC(19,4) precision.
type MerchantInvoiceItemDraftLine struct {
	// ID is optional. A new UUID is generated when it is uuid.Nil.
	ID uuid.UUID

	// InvoiceID identifies the parent invoice. The invoice must currently be
	// in draft status or insertion is rejected.
	InvoiceID uuid.UUID

	// FeeCalculationID is the sole direct provenance reference. At most one
	// invoice item may ever exist for a given fee calculation.
	FeeCalculationID uuid.UUID

	// Description is the durable, caller-supplied snapshot of what was
	// charged. It is never regenerated from fee-calculation metadata.
	Description string

	// Quantity must be a positive NUMERIC(19,4)-compatible decimal string.
	Quantity string

	// UnitAmount must be a non-negative NUMERIC(19,4)-compatible decimal
	// string representing the gross unit charge for this product/service line.
	// Platform Credits and other monetary effects must not be netted into or
	// otherwise hidden by rewriting this value.
	UnitAmount string
}

// MerchantInvoiceItemModel owns merchant invoice item persistence.
type MerchantInvoiceItemModel struct {
	DB     *pgxpool.Pool
	Logger *logging.Logger
}

type merchantInvoiceItemQueryRower interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

func (m *MerchantInvoiceItemModel) validateBase() error {
	if m == nil {
		return errors.New("merchant invoice item model is required")
	}
	if m.Logger == nil {
		return errors.New("merchant invoice item model logger is required")
	}
	return nil
}

func (m *MerchantInvoiceItemModel) validatePool() error {
	if err := m.validateBase(); err != nil {
		return err
	}
	if m.DB == nil {
		return errors.New("merchant invoice item model database pool is required")
	}
	return nil
}

func scanMerchantInvoiceItem(row scannableRow, item *MerchantInvoiceItem) error {
	return row.Scan(
		&item.ID,
		&item.InvoiceID,
		&item.FeeCalculationID,
		&item.Description,
		&item.Quantity,
		&item.UnitAmount,
		&item.LineAmount,
		&item.CreatedAt,
	)
}

// -----------------------------------------------------------------------------
// Stable input and persistence errors
// -----------------------------------------------------------------------------

func merchantInvoiceItemInvalidInput(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrMerchantInvoiceItemInvalidInput, fmt.Sprintf(format, args...))
}

func classifyMerchantInvoiceItemWriteError(err error) error {
	switch {
	case IsPgConstraint(err, merchantInvoiceItemFeeCalculationUniqueConstraint):
		return ErrMerchantInvoiceItemDuplicateFeeCalculation
	case IsPgConstraint(err, merchantInvoiceItemInvoiceFKConstraint):
		return ErrMerchantInvoiceItemInvoiceNotFound
	case IsPgConstraint(err, merchantInvoiceItemFeeCalculationFKConstraint):
		return ErrMerchantInvoiceItemFeeCalculationNotFound
	case IsPgConstraint(err, merchantInvoiceItemDescriptionLengthConstraint),
		IsPgConstraint(err, merchantInvoiceItemDescriptionTrimmedConstraint),
		IsPgConstraint(err, merchantInvoiceItemLineAmountConstraint),
		IsPgConstraint(err, merchantInvoiceItemQuantityCheckConstraint),
		IsPgConstraint(err, merchantInvoiceItemUnitAmountCheckConstraint):
		return ErrMerchantInvoiceItemInvalidState
	case IsUniqueViolation(err):
		return ErrMerchantInvoiceItemDuplicateFeeCalculation
	case IsForeignKeyViolation(err):
		return ErrMerchantInvoiceItemInvalidState
	case IsCheckViolation(err), IsNotNullViolation(err):
		return ErrMerchantInvoiceItemInvalidState
	default:
		return err
	}
}

// -----------------------------------------------------------------------------
// Normalization and validation
// -----------------------------------------------------------------------------

func normalizeMerchantInvoiceItemDescription(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", merchantInvoiceItemInvalidInput("description is required")
	}
	if utf8.RuneCountInString(value) > merchantInvoiceItemDescriptionMaxLength {
		return "", merchantInvoiceItemInvalidInput(
			"description must not exceed %d characters",
			merchantInvoiceItemDescriptionMaxLength,
		)
	}
	return value, nil
}

func parseMerchantInvoiceItemDecimal(value, fieldName string, requirePositive bool) (*big.Rat, string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, "", merchantInvoiceItemInvalidInput("%s is required", fieldName)
	}
	if !merchantInvoiceItemAmountPattern.MatchString(value) {
		return nil, "", merchantInvoiceItemInvalidInput(
			"%s must be a NUMERIC(19,4)-compatible decimal", fieldName,
		)
	}

	r, ok := new(big.Rat).SetString(value)
	if !ok {
		return nil, "", merchantInvoiceItemInvalidInput("%s must be a valid decimal value", fieldName)
	}
	if requirePositive {
		if r.Sign() <= 0 {
			return nil, "", merchantInvoiceItemInvalidInput("%s must be greater than zero", fieldName)
		}
	} else if r.Sign() < 0 {
		return nil, "", merchantInvoiceItemInvalidInput("%s must be greater than or equal to zero", fieldName)
	}
	return r, value, nil
}

// computeMerchantInvoiceItemLineAmount computes the exact rational product of
// quantity and unitAmount and returns its canonical NUMERIC(19,4) decimal
// string form. It fails closed if the exact product cannot be represented
// without rounding at four fractional digits, or if the result exceeds
// NUMERIC(19,4) bounds.
func computeMerchantInvoiceItemLineAmount(quantity, unitAmount *big.Rat) (string, error) {
	product := new(big.Rat).Mul(quantity, unitAmount)

	candidate := product.FloatString(4)
	reparsed, ok := new(big.Rat).SetString(candidate)
	if !ok || reparsed.Cmp(product) != 0 {
		return "", merchantInvoiceItemInvalidInput(
			"quantity and unit_amount must produce a line_amount exactly representable at NUMERIC(19,4) precision",
		)
	}
	if !merchantInvoiceItemAmountPattern.MatchString(candidate) {
		return "", merchantInvoiceItemInvalidInput("line_amount exceeds NUMERIC(19,4) bounds")
	}
	return candidate, nil
}

func validateMerchantInvoiceItemForInsert(line *MerchantInvoiceItemDraftLine) (*MerchantInvoiceItem, error) {
	if line == nil {
		return nil, merchantInvoiceItemInvalidInput("invoice item is required")
	}
	if line.InvoiceID == uuid.Nil {
		return nil, merchantInvoiceItemInvalidInput("invoice_id is required")
	}
	if line.FeeCalculationID == uuid.Nil {
		return nil, merchantInvoiceItemInvalidInput("fee_calculation_id is required")
	}

	description, err := normalizeMerchantInvoiceItemDescription(line.Description)
	if err != nil {
		return nil, err
	}

	quantityRat, quantity, err := parseMerchantInvoiceItemDecimal(line.Quantity, "quantity", true)
	if err != nil {
		return nil, err
	}
	unitAmountRat, unitAmount, err := parseMerchantInvoiceItemDecimal(line.UnitAmount, "unit_amount", false)
	if err != nil {
		return nil, err
	}

	lineAmount, err := computeMerchantInvoiceItemLineAmount(quantityRat, unitAmountRat)
	if err != nil {
		return nil, err
	}

	id := line.ID
	if id == uuid.Nil {
		id = uuid.New()
	}

	return &MerchantInvoiceItem{
		ID:               id,
		InvoiceID:        line.InvoiceID,
		FeeCalculationID: line.FeeCalculationID,
		Description:      description,
		Quantity:         quantity,
		UnitAmount:       unitAmount,
		LineAmount:       lineAmount,
	}, nil
}

func validateMerchantInvoiceItemPersistedState(item *MerchantInvoiceItem) error {
	if item == nil ||
		item.ID == uuid.Nil ||
		item.InvoiceID == uuid.Nil ||
		item.FeeCalculationID == uuid.Nil ||
		item.CreatedAt.IsZero() {
		return ErrMerchantInvoiceItemInvalidState
	}

	description := strings.TrimSpace(item.Description)
	if description == "" ||
		description != item.Description ||
		utf8.RuneCountInString(description) > merchantInvoiceItemDescriptionMaxLength {
		return ErrMerchantInvoiceItemInvalidState
	}

	quantity, ok := new(big.Rat).SetString(item.Quantity)
	if !ok || quantity.Sign() <= 0 {
		return ErrMerchantInvoiceItemInvalidState
	}
	unitAmount, ok := new(big.Rat).SetString(item.UnitAmount)
	if !ok || unitAmount.Sign() < 0 {
		return ErrMerchantInvoiceItemInvalidState
	}
	lineAmount, ok := new(big.Rat).SetString(item.LineAmount)
	if !ok || lineAmount.Sign() < 0 {
		return ErrMerchantInvoiceItemInvalidState
	}

	expected := new(big.Rat).Mul(quantity, unitAmount)
	if expected.Cmp(lineAmount) != 0 {
		return ErrMerchantInvoiceItemInvalidState
	}

	return nil
}

func validateMerchantInvoiceItemID(id uuid.UUID) error {
	if id == uuid.Nil {
		return merchantInvoiceItemInvalidInput("id is required")
	}
	return nil
}

func validateMerchantInvoiceItemInvoiceID(id uuid.UUID) error {
	if id == uuid.Nil {
		return merchantInvoiceItemInvalidInput("invoice_id is required")
	}
	return nil
}

func validateMerchantInvoiceItemFeeCalculationID(id uuid.UUID) error {
	if id == uuid.Nil {
		return merchantInvoiceItemInvalidInput("fee_calculation_id is required")
	}
	return nil
}

func validateMerchantInvoiceItemListLimit(limit int) error {
	if limit <= 0 || limit > merchantInvoiceItemMaxListLimit {
		return merchantInvoiceItemInvalidInput("limit must be between 1 and %d", merchantInvoiceItemMaxListLimit)
	}
	return nil
}

func validateMerchantInvoiceItemAfterCursor(afterCreatedAt *time.Time, afterID *uuid.UUID) error {
	if (afterCreatedAt == nil) != (afterID == nil) {
		return merchantInvoiceItemInvalidInput("after_created_at and after_id must be supplied together")
	}
	if afterCreatedAt == nil {
		return nil
	}
	if afterCreatedAt.IsZero() || *afterID == uuid.Nil {
		return merchantInvoiceItemInvalidInput("invalid merchant invoice item after cursor")
	}
	return nil
}

// -----------------------------------------------------------------------------
// Parent-invoice draft-status resolution (no-match diagnosis)
// -----------------------------------------------------------------------------

func (m *MerchantInvoiceItemModel) getInvoiceStatusViaQuerier(
	ctx context.Context,
	querier merchantInvoiceItemQueryRower,
	invoiceID uuid.UUID,
) (*MerchantInvoiceStatus, error) {
	const query = `
		SELECT invoice_status
		FROM merchant_invoices
		WHERE id = $1
	`
	var status MerchantInvoiceStatus
	if err := querier.QueryRow(ctx, query, invoiceID).Scan(&status); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &status, nil
}

// -----------------------------------------------------------------------------
// Single-record reads
// -----------------------------------------------------------------------------

func (m *MerchantInvoiceItemModel) getByIDViaQuerier(
	ctx context.Context,
	querier merchantInvoiceItemQueryRower,
	id uuid.UUID,
) (*MerchantInvoiceItem, error) {
	const query = `
		SELECT ` + merchantInvoiceItemSelectColumns + `
		FROM merchant_invoice_items
		WHERE id = $1
	`
	var item MerchantInvoiceItem
	if err := scanMerchantInvoiceItem(querier.QueryRow(ctx, query, id), &item); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	if err := validateMerchantInvoiceItemPersistedState(&item); err != nil {
		return nil, err
	}
	return &item, nil
}

// GetByID retrieves a merchant invoice item by ID. Absence returns nil, nil.
func (m *MerchantInvoiceItemModel) GetByID(ctx context.Context, id uuid.UUID) (*MerchantInvoiceItem, error) {
	if err := m.validatePool(); err != nil {
		return nil, err
	}
	if err := validateMerchantInvoiceItemID(id); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	item, err := m.getByIDViaQuerier(ctx, m.DB, id)
	if err != nil {
		return nil, fmt.Errorf("get merchant invoice item by ID: %w", err)
	}
	return item, nil
}

// GetByFeeCalculationID retrieves the invoice item, if any, already recorded
// for feeCalculationID.
//
// This read supports audit, reconciliation, and idempotent outcome resolution.
// Callers must not use SELECT-before-INSERT as the uniqueness mechanism: the
// database UNIQUE constraint on fee_calculation_id is the concurrency boundary.
// Absence returns nil, nil.
func (m *MerchantInvoiceItemModel) GetByFeeCalculationID(
	ctx context.Context,
	feeCalculationID uuid.UUID,
) (*MerchantInvoiceItem, error) {
	if err := m.validatePool(); err != nil {
		return nil, err
	}
	if err := validateMerchantInvoiceItemFeeCalculationID(feeCalculationID); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	const query = `
		SELECT ` + merchantInvoiceItemSelectColumns + `
		FROM merchant_invoice_items
		WHERE fee_calculation_id = $1
	`
	var item MerchantInvoiceItem
	if err := scanMerchantInvoiceItem(m.DB.QueryRow(ctx, query, feeCalculationID), &item); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("get merchant invoice item by fee calculation: %w", err)
	}
	if err := validateMerchantInvoiceItemPersistedState(&item); err != nil {
		return nil, err
	}
	return &item, nil
}

// ListByInvoiceID returns a bounded, keyset-paginated page of invoice items
// for invoiceID ordered created_at ASC, id ASC. This provides deterministic
// persistence order; merchant-facing presentation may apply its own composition
// rules without changing invoice-item identity.
//
// Pagination is forward keyset-based. afterCreatedAt and afterID must either
// both be nil for the first page or both be supplied for a subsequent page.
func (m *MerchantInvoiceItemModel) ListByInvoiceID(
	ctx context.Context,
	invoiceID uuid.UUID,
	limit int,
	afterCreatedAt *time.Time,
	afterID *uuid.UUID,
) ([]*MerchantInvoiceItem, error) {
	if err := m.validatePool(); err != nil {
		return nil, err
	}
	if err := validateMerchantInvoiceItemInvoiceID(invoiceID); err != nil {
		return nil, err
	}
	if err := validateMerchantInvoiceItemListLimit(limit); err != nil {
		return nil, err
	}
	if err := validateMerchantInvoiceItemAfterCursor(afterCreatedAt, afterID); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	var (
		rows pgx.Rows
		err  error
	)
	if afterCreatedAt == nil {
		const query = `
			SELECT ` + merchantInvoiceItemSelectColumns + `
			FROM merchant_invoice_items
			WHERE invoice_id = $1
			ORDER BY created_at ASC, id ASC
			LIMIT $2
		`
		rows, err = m.DB.Query(ctx, query, invoiceID, limit)
	} else {
		const query = `
			SELECT ` + merchantInvoiceItemSelectColumns + `
			FROM merchant_invoice_items
			WHERE invoice_id = $1
			  AND (created_at, id) > ($2, $3)
			ORDER BY created_at ASC, id ASC
			LIMIT $4
		`
		rows, err = m.DB.Query(ctx, query, invoiceID, afterCreatedAt.UTC(), *afterID, limit)
	}
	if err != nil {
		return nil, fmt.Errorf("list merchant invoice items by invoice: %w", err)
	}
	defer rows.Close()

	items := make([]*MerchantInvoiceItem, 0, limit)
	for rows.Next() {
		var item MerchantInvoiceItem
		if err := scanMerchantInvoiceItem(rows, &item); err != nil {
			return nil, err
		}
		if err := validateMerchantInvoiceItemPersistedState(&item); err != nil {
			return nil, err
		}
		items = append(items, &item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

// SumLineAmountByInvoiceIDTx returns the exact aggregate line_amount already
// persisted for invoiceID, using only the supplied caller-owned transaction.
//
// This method performs no locking. The caller must first lock the canonical
// merchant_invoices row for invoiceID in the same transaction (via
// MerchantInvoiceModel.GetByIDForUpdateTx). That lock is the serialization
// point that makes this aggregate safe under concurrent line composition.
//
// No item rows returns "0".
func (m *MerchantInvoiceItemModel) SumLineAmountByInvoiceIDTx(
	ctx context.Context,
	tx pgx.Tx,
	invoiceID uuid.UUID,
) (string, error) {
	if err := m.validateBase(); err != nil {
		return "", err
	}
	if tx == nil {
		return "", merchantInvoiceItemInvalidInput("transaction is required")
	}
	if err := validateMerchantInvoiceItemInvoiceID(invoiceID); err != nil {
		return "", err
	}

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("SumMerchantInvoiceItemLineAmountByInvoiceIDTx")

	const query = `
		SELECT COALESCE(SUM(line_amount), 0)::text
		FROM merchant_invoice_items
		WHERE invoice_id = $1
	`
	var amount string
	if err := tx.QueryRow(ctx, query, invoiceID).Scan(&amount); err != nil {
		logger.Error("sum merchant invoice item line amounts failed", err, "invoice_id", invoiceID)
		return "", err
	}
	return amount, nil
}

// -----------------------------------------------------------------------------
// Draft-only creation
// -----------------------------------------------------------------------------

func merchantInvoiceItemLogFields(item *MerchantInvoiceItem) []any {
	if item == nil {
		return nil
	}
	return []any{
		"invoice_item_id", item.ID,
		"invoice_id", item.InvoiceID,
		"fee_calculation_id", item.FeeCalculationID,
	}
}

func (m *MerchantInvoiceItemModel) insertDraftLine(
	ctx context.Context,
	querier merchantInvoiceItemQueryRower,
	functionName string,
	line *MerchantInvoiceItemDraftLine,
) (*MerchantInvoiceItem, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName(functionName)

	item, err := validateMerchantInvoiceItemForInsert(line)
	if err != nil {
		logger.Error("Validation failed", err)
		return nil, err
	}

	const query = `
		WITH locked_invoice AS (
			SELECT id
			FROM merchant_invoices
			WHERE id = $2
			  AND invoice_status = 'draft'
			FOR UPDATE
		)
		INSERT INTO merchant_invoice_items (
			id,
			invoice_id,
			fee_calculation_id,
			description,
			quantity,
			unit_amount,
			line_amount
		)
		SELECT $1, locked_invoice.id, $3, $4, $5::numeric, $6::numeric, $7::numeric
		FROM locked_invoice
		RETURNING ` + merchantInvoiceItemSelectColumns

	var result MerchantInvoiceItem
	if err := scanMerchantInvoiceItem(
		querier.QueryRow(
			ctx,
			query,
			item.ID,
			item.InvoiceID,
			item.FeeCalculationID,
			item.Description,
			item.Quantity,
			item.UnitAmount,
			item.LineAmount,
		),
		&result,
	); err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			err = classifyMerchantInvoiceItemWriteError(err)
			fields := append([]any{err}, merchantInvoiceItemLogFields(item)...)
			if errors.Is(err, ErrMerchantInvoiceItemDuplicateFeeCalculation) {
				logger.Warn("Merchant invoice item already exists for fee calculation", fields...)
				return nil, err
			}
			logger.Error("Insert merchant invoice item failed", fields...)
			return nil, err
		}

		status, statusErr := m.getInvoiceStatusViaQuerier(ctx, querier, item.InvoiceID)
		if statusErr != nil {
			return nil, statusErr
		}
		if status == nil {
			return nil, ErrMerchantInvoiceItemInvoiceNotFound
		}
		return nil, ErrMerchantInvoiceItemInvoiceNotDraft
	}

	if err := validateMerchantInvoiceItemPersistedState(&result); err != nil {
		logger.Error("Insert merchant invoice item returned invalid state", err, "invoice_item_id", result.ID)
		return nil, err
	}

	logger.Info(
		"Insert merchant invoice item successful",
		"invoice_item_id", result.ID,
		"invoice_id", result.InvoiceID,
		"fee_calculation_id", result.FeeCalculationID,
		"line_amount", result.LineAmount,
	)
	return &result, nil
}

// InsertDraftLineTx creates one draft invoice line through a caller-owned transaction.
//
// InsertDraftLineTx does not begin, commit, or roll back tx. It row-locks the
// parent invoice and permits insertion only while that invoice remains draft.
// The caller must reconcile the invoice subtotal with SumLineAmountByInvoiceIDTx
// and MerchantInvoiceModel.UpdateDraftFinancialsTx in the same transaction
// before commit.
func (m *MerchantInvoiceItemModel) InsertDraftLineTx(
	ctx context.Context,
	tx pgx.Tx,
	line *MerchantInvoiceItemDraftLine,
) (*MerchantInvoiceItem, error) {
	if err := m.validateBase(); err != nil {
		return nil, err
	}
	if tx == nil {
		return nil, merchantInvoiceItemInvalidInput("transaction is required")
	}
	return m.insertDraftLine(ctx, tx, "InsertMerchantInvoiceItemDraftLineTx", line)
}

// -----------------------------------------------------------------------------
// Draft-only removal
// -----------------------------------------------------------------------------

func (m *MerchantInvoiceItemModel) removeDraftLine(
	ctx context.Context,
	querier merchantInvoiceItemQueryRower,
	functionName string,
	invoiceID uuid.UUID,
	itemID uuid.UUID,
) error {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName(functionName)

	if err := validateMerchantInvoiceItemInvoiceID(invoiceID); err != nil {
		return err
	}
	if err := validateMerchantInvoiceItemID(itemID); err != nil {
		return err
	}

	const query = `
		WITH locked_invoice AS (
			SELECT id
			FROM merchant_invoices
			WHERE id = $2
			  AND invoice_status = 'draft'
			FOR UPDATE
		)
		DELETE FROM merchant_invoice_items
		USING locked_invoice
		WHERE merchant_invoice_items.id = $1
		  AND merchant_invoice_items.invoice_id = locked_invoice.id
		RETURNING merchant_invoice_items.id
	`
	var deletedID uuid.UUID
	if err := querier.QueryRow(ctx, query, itemID, invoiceID).Scan(&deletedID); err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			logger.Error("Remove merchant invoice item failed", err, "invoice_item_id", itemID, "invoice_id", invoiceID)
			return err
		}

		existing, getErr := m.getByIDViaQuerier(ctx, querier, itemID)
		if getErr != nil {
			return getErr
		}
		if existing == nil || existing.InvoiceID != invoiceID {
			return ErrMerchantInvoiceItemNotFound
		}
		return ErrMerchantInvoiceItemInvoiceNotDraft
	}

	logger.Info("Remove merchant invoice item successful", "invoice_item_id", deletedID, "invoice_id", invoiceID)
	return nil
}

// RemoveDraftLineTx removes one draft invoice line through a caller-owned transaction.
//
// RemoveDraftLineTx does not begin, commit, or roll back tx. It row-locks the
// parent invoice and permits removal only while that invoice remains draft.
// The caller must reconcile the invoice subtotal with SumLineAmountByInvoiceIDTx
// and MerchantInvoiceModel.UpdateDraftFinancialsTx in the same transaction
// before commit.
func (m *MerchantInvoiceItemModel) RemoveDraftLineTx(ctx context.Context, tx pgx.Tx, invoiceID, itemID uuid.UUID) error {
	if err := m.validateBase(); err != nil {
		return err
	}
	if tx == nil {
		return merchantInvoiceItemInvalidInput("transaction is required")
	}
	return m.removeDraftLine(ctx, tx, "RemoveMerchantInvoiceItemDraftLineTx", invoiceID, itemID)
}
