// Package services contains trusted internal Commerce Architecture
// orchestration for merchant invoice lifecycle mutation.
//
// sdworkspace/sdbackend/internal/services/merchant_invoices_internal.go
//
// GTM:
//
//	Layer: 2.4.b Commerce Architecture / Monetization Layer
//	Release Class: SPINE
//	Reason:
//	  merchant_invoices is the canonical durable statement of a merchant's
//	  commercial obligation to Sagrenti. This service provides the trusted
//	  internal capability boundary for constructing draft invoices, revising
//	  draft financial identity, issuing invoices, settling zero-balance
//	  obligations, marking eligible invoices overdue, and voiding eligible
//	  issued invoices.
//
// Domain Boundary:
//
//	This service owns:
//	  - trusted internal entrypoints for merchant-invoice lifecycle mutation;
//	  - pool-backed and caller-owned-transaction forms where composition may
//	    require them;
//	  - DBTimeout-bounded execution for pool-backed service calls;
//	  - preservation of caller-owned transaction semantics for Tx variants;
//	  - a stable service boundary above MerchantInvoiceModel for future
//	    Commerce orchestration.
//
//	This service does not own:
//	  - invoice-generation policy;
//	  - invoice-number sequencing policy;
//	  - payment-term or grace-period policy;
//	  - currency selection or merchant-domicile assumptions;
//	  - the decision to use IssueDueNow rather than Issue;
//	  - overdue-processing cadence or collection policy;
//	  - fee calculation or fee-schedule selection;
//	  - platform-credit eligibility, allocation, or account selection;
//	  - promotions or merchant-specific commercial policy;
//	  - payment execution or authoritative merchant_payments persistence;
//	  - HTTP authorization or mutation exposure;
//	  - asynchronous scheduling, retry, worker claiming, or failure handling.
//
// Resolved-Fact Contract:
//
//	CreateMerchantInvoiceDraftInput currently contains commercial facts supplied
//	by trusted upstream Commerce orchestration: merchant ownership, invoice
//	number, and the draft financial snapshot.
//
//	The current merchant_invoices header does not contain normalized structural
//	relationships to the fee calculations, Platform Credit applications, or
//	other authoritative commercial records from which an invoice obligation is
//	derived. MerchantID therefore cannot yet be derived from invoice-source
//	provenance at this boundary.
//
//	This service must not invent that missing provenance through opaque JSON,
//	notes, invoice numbers, logs, or caller conventions.
//
//	PRODUCTION CREATION GATE:
//
//	CreateMerchantInvoiceDraftInternal and
//	CreateMerchantInvoiceDraftTxInternal are valid low-level service capabilities,
//	but the merchant-invoice creation workflow is not commercially complete until
//	a normalized invoice source/line domain can durably associate each invoice
//	with the authoritative obligations and reductions that produced it.
//
//	No production Commerce workflow may treat caller-supplied subtotal,
//	adjustment, currency, or merchant ownership as sufficient financial
//	provenance merely because MerchantInvoiceModel validates those values.
//
//	Before production invoice generation is enabled, coordinating Commerce
//	orchestration must be capable of composing the invoice header with its
//	normalized source evidence atomically where required.
//
//	Structural validation, monetary canonicalization, currency canonicalization,
//	merchant foreign-key enforcement, invoice-number uniqueness, and persisted
//	invoice-state validation remain owned by MerchantInvoiceModel.
//
// Monetary Boundary:
//
//	This service performs no monetary arithmetic.
//
//	SubtotalAmount, AdjustmentAmount, TotalAmount, and AmountPaid remain exact
//	decimal strings / PostgreSQL NUMERIC values. This service never converts
//	monetary values through float32 or float64, never silently rounds, and never
//	performs currency conversion.
//
//	Draft financial revision replaces the complete mutable draft financial
//	identity represented by subtotal, adjustment, derived total, and currency.
//	After issuance, the data-layer guards prevent those financial facts from
//	being rewritten.
//
// Transaction Boundary:
//
//	Pool-backed entrypoints use one outer service DBTimeout. The underlying data
//	model may enforce its own defensive database-operation timeout; the service
//	timeout remains the outer workflow bound.
//
//	Tx-suffixed entrypoints inherit the caller's context and supplied pgx.Tx.
//	They never begin, commit, roll back, or replace that transaction and do not
//	introduce an independent service timeout.
//
//	The invoice mutations themselves use guarded INSERT/UPDATE statements in the
//	data layer. Where a guarded lifecycle UPDATE does not match, the data model
//	may perform a follow-up read through the same persistence boundary to
//	distinguish not-found, already-at-target idempotency, and invalid transition.
//
// Payment-Composition Boundary:
//
//	This file intentionally does not expose ApplyMerchantInvoicePaymentInternal
//	or ApplyMerchantInvoicePaymentTxInternal.
//
//	MerchantInvoiceModel.ApplyPaymentTx is transaction-only because the
//	amount_paid increment must commit atomically with the authoritative
//	merchant_payments record that explains that increment.
//
//	The future Merchant Payments orchestration owns that transaction and must
//	compose its authoritative payment persistence with
//	MerchantInvoiceModel.ApplyPaymentTx in the same transaction.
//
//	This service therefore never:
//	  - applies payment independently;
//	  - creates an invoice-only payment transaction;
//	  - treats payment amount as an idempotency key;
//	  - infers settlement from a provider attempt;
//	  - moves Merchant Payments responsibility into Commerce.
//
// Concurrency:
//
//	Lifecycle mutations rely on database row locking plus guarded state
//	predicates. Concurrent or retried transitions cannot bypass the persisted
//	lifecycle guards.
//
//	Simple lifecycle transitions resolve an unsuccessful guarded UPDATE by
//	reading current state. Already-at-target state may therefore be treated
//	idempotently while incompatible state remains an invalid transition.
//
//	Issue and IssueDueNow additionally preserve their requested due-date
//	identity when determining whether a retry is equivalent.
//
//	Draft financial revisions are safe database writes but intentionally do not
//	claim optimistic-concurrency semantics. If multiple trusted writers revise
//	the same draft concurrently while it remains draft, PostgreSQL serializes the
//	updates and the final committed writer determines the resulting draft
//	financial snapshot. A future interactive/concurrent editing surface may
//	require an explicit version/check-token contract rather than pretending that
//	this service already provides one.
//
// Payment concurrency and payment retry idempotency belong to the future
// authoritative merchant_payments transaction and its durable idempotency
// boundary.
//
// Asynchronous Responsibility:
//
//	Issued invoices reaching due_at are genuine future time-based work.
//
//	merchant_invoices_async.go is intentionally absent until Sagrenti's durable
//	Automation Foundation provides the required worker execution, durable
//	scheduling/claiming, retries, failure handling, concurrency coordination,
//	observability, and shutdown behavior.
//
//	MerchantInvoiceModel.ListIssuedDueForOverdueProcessing and the
//	MarkMerchantInvoiceOverdueInternal / TxInternal capabilities provide the
//	domain operations that future Automation Foundation workers may compose.
//
//	No OS cron dependency, domain-specific worker engine, or in-memory timer is
//	introduced here.
//
// Observability:
//
//	MerchantInvoiceModel already emits structured persistence/lifecycle logs.
//	This service avoids duplicating those logs.
//
//	Invoice monetary values must not be added to service logs, audit metadata,
//	traces, or metrics merely for convenience. High-cardinality identifiers must
//	not be introduced as metrics labels.
//
// SPINE Rule:
//
//	Keep compiling.
//	Keep production-ready.
//	Preserve exact monetary representation.
//	Preserve draft-only financial revision.
//	Preserve post-issuance monetary and currency immutability.
//	Preserve guarded forward-only lifecycle behavior.
//	Preserve caller-owned transaction semantics.
//	Preserve transaction-only payment application at the Merchant Payments
//	composition boundary.
//	Preserve errors.Is compatibility through %w wrapping.
//	Never invent invoice-generation, payment-term, due-date, currency-selection,
//	pricing, promotion, Platform Credit, payment, or collection policy.
//	Never expose an invoice-only or pool-backed payment-application capability.
//	Never begin, commit, or roll back caller-owned transactions.
//	Never introduce domain asynchronous execution without the durable Automation
//	Foundation required by BEG.
//	Block deployment if this file breaks monetary integrity, lifecycle safety,
//	transaction discipline, historical integrity, payment composition, or
//	billing reconciliation readiness.
package services

import (
	"context"
	"fmt"
	"time"

	"github.com/PiccoloMondoC/sdworkspace/sdbackend/internal/data"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// CreateMerchantInvoiceDraftInput contains the already-resolved facts required
// to create one draft merchant invoice.
//
// MerchantID, InvoiceNumber, and Financials must already have been resolved by
// trusted upstream Commerce orchestration or governed configuration. This
// service does not decide invoice-generation policy, invoice-number sequencing,
// pricing, adjustments, or currency selection.
type CreateMerchantInvoiceDraftInput struct {
	MerchantID    uuid.UUID
	InvoiceNumber string
	Financials    data.MerchantInvoiceDraftFinancials
}

func validateMerchantInvoiceService(s *Service) error {
	if err := s.validate(); err != nil {
		return err
	}

	if s.Models.MerchantInvoice.DB == nil {
		return fmt.Errorf(
			"%w: merchant invoice model database is nil",
			ErrInvalidServiceConfiguration,
		)
	}

	if s.Models.MerchantInvoice.Logger == nil {
		return fmt.Errorf(
			"%w: merchant invoice model logger is nil",
			ErrInvalidServiceConfiguration,
		)
	}

	return nil
}

func validateMerchantInvoiceTxService(s *Service) error {
	if s == nil {
		return fmt.Errorf(
			"%w: merchant invoice service is nil",
			ErrInvalidServiceConfiguration,
		)
	}

	if s.Logger == nil {
		return fmt.Errorf(
			"%w: merchant invoice service logger is nil",
			ErrInvalidServiceConfiguration,
		)
	}

	if s.Models == nil {
		return fmt.Errorf(
			"%w: merchant invoice service models are nil",
			ErrInvalidServiceConfiguration,
		)
	}

	if s.Models.MerchantInvoice.Logger == nil {
		return fmt.Errorf(
			"%w: merchant invoice model logger is nil",
			ErrInvalidServiceConfiguration,
		)
	}

	return nil
}

func (s *Service) merchantInvoiceContext(
	ctx context.Context,
) (context.Context, context.CancelFunc, error) {
	if ctx == nil {
		return nil, nil, ErrNilContext
	}

	if err := validateMerchantInvoiceService(s); err != nil {
		return nil, nil, err
	}

	dbCtx, cancel := context.WithTimeout(
		ctx,
		s.Cfg.DBTimeout,
	)

	return dbCtx, cancel, nil
}

func validateMerchantInvoiceTx(tx pgx.Tx) error {
	if tx == nil {
		return fmt.Errorf(
			"%w: transaction is required",
			data.ErrMerchantInvoiceInvalidInput,
		)
	}

	return nil
}

func merchantInvoiceDraftFromInput(
	input CreateMerchantInvoiceDraftInput,
) *data.MerchantInvoice {
	return &data.MerchantInvoice{
		MerchantID:       input.MerchantID,
		InvoiceNumber:    input.InvoiceNumber,
		SubtotalAmount:   input.Financials.SubtotalAmount,
		AdjustmentAmount: input.Financials.AdjustmentAmount,
		Currency:         input.Financials.Currency,
	}
}

// CreateMerchantInvoiceDraftInternal creates one draft merchant invoice from
// already-resolved commercial facts.
//
// MerchantInvoiceModel remains authoritative for structural validation,
// monetary/currency canonicalization, merchant foreign-key enforcement,
// invoice-number uniqueness, and draft-state persistence invariants.
func (s *Service) CreateMerchantInvoiceDraftInternal(
	ctx context.Context,
	input CreateMerchantInvoiceDraftInput,
) (*data.MerchantInvoice, error) {
	dbCtx, cancel, err := s.merchantInvoiceContext(ctx)
	if err != nil {
		return nil, err
	}
	defer cancel()

	result, err :=
		s.Models.
			MerchantInvoice.
			InsertDraft(
				dbCtx,
				merchantInvoiceDraftFromInput(input),
			)
	if err != nil {
		return nil, fmt.Errorf(
			"create merchant invoice draft: %w",
			err,
		)
	}

	return result, nil
}

// CreateMerchantInvoiceDraftTxInternal is the transaction-aware form of
// CreateMerchantInvoiceDraftInternal.
//
// The caller owns transaction begin, commit, rollback, and timeout. This method
// never replaces the supplied transaction.
func (s *Service) CreateMerchantInvoiceDraftTxInternal(
	ctx context.Context,
	tx pgx.Tx,
	input CreateMerchantInvoiceDraftInput,
) (*data.MerchantInvoice, error) {
	if ctx == nil {
		return nil, ErrNilContext
	}

	if err := validateMerchantInvoiceTxService(s); err != nil {
		return nil, err
	}

	if err := validateMerchantInvoiceTx(tx); err != nil {
		return nil, err
	}

	result, err :=
		s.Models.
			MerchantInvoice.
			InsertDraftTx(
				ctx,
				tx,
				merchantInvoiceDraftFromInput(input),
			)
	if err != nil {
		return nil, fmt.Errorf(
			"create merchant invoice draft in transaction: %w",
			err,
		)
	}

	return result, nil
}

// UpdateMerchantInvoiceDraftFinancialsInternal replaces the complete mutable
// financial snapshot of a draft invoice.
//
// The data model derives TotalAmount from SubtotalAmount + AdjustmentAmount and
// enforces that revision is possible only while the invoice remains draft.
func (s *Service) UpdateMerchantInvoiceDraftFinancialsInternal(
	ctx context.Context,
	id uuid.UUID,
	financials data.MerchantInvoiceDraftFinancials,
) (*data.MerchantInvoice, error) {
	dbCtx, cancel, err := s.merchantInvoiceContext(ctx)
	if err != nil {
		return nil, err
	}
	defer cancel()

	result, err :=
		s.Models.
			MerchantInvoice.
			UpdateDraftFinancials(
				dbCtx,
				id,
				financials,
			)
	if err != nil {
		return nil, fmt.Errorf(
			"update merchant invoice draft financials: %w",
			err,
		)
	}

	return result, nil
}

// UpdateMerchantInvoiceDraftFinancialsTxInternal is the transaction-aware form
// of UpdateMerchantInvoiceDraftFinancialsInternal.
func (s *Service) UpdateMerchantInvoiceDraftFinancialsTxInternal(
	ctx context.Context,
	tx pgx.Tx,
	id uuid.UUID,
	financials data.MerchantInvoiceDraftFinancials,
) (*data.MerchantInvoice, error) {
	if ctx == nil {
		return nil, ErrNilContext
	}

	if err := validateMerchantInvoiceTxService(s); err != nil {
		return nil, err
	}

	if err := validateMerchantInvoiceTx(tx); err != nil {
		return nil, err
	}

	result, err :=
		s.Models.
			MerchantInvoice.
			UpdateDraftFinancialsTx(
				ctx,
				tx,
				id,
				financials,
			)
	if err != nil {
		return nil, fmt.Errorf(
			"update merchant invoice draft financials in transaction: %w",
			err,
		)
	}

	return result, nil
}

// IssueMerchantInvoiceInternal transitions a draft invoice to issued with an
// optional already-resolved due date.
//
// dueAt is a commercial-policy result supplied by the caller. This method does
// not derive grace periods, payment terms, or invoice-class-specific due rules.
func (s *Service) IssueMerchantInvoiceInternal(
	ctx context.Context,
	id uuid.UUID,
	dueAt *time.Time,
) (*data.MerchantInvoice, error) {
	dbCtx, cancel, err := s.merchantInvoiceContext(ctx)
	if err != nil {
		return nil, err
	}
	defer cancel()

	result, err :=
		s.Models.
			MerchantInvoice.
			Issue(
				dbCtx,
				id,
				dueAt,
			)
	if err != nil {
		return nil, fmt.Errorf(
			"issue merchant invoice: %w",
			err,
		)
	}

	return result, nil
}

// IssueMerchantInvoiceTxInternal is the transaction-aware form of
// IssueMerchantInvoiceInternal.
func (s *Service) IssueMerchantInvoiceTxInternal(
	ctx context.Context,
	tx pgx.Tx,
	id uuid.UUID,
	dueAt *time.Time,
) (*data.MerchantInvoice, error) {
	if ctx == nil {
		return nil, ErrNilContext
	}

	if err := validateMerchantInvoiceTxService(s); err != nil {
		return nil, err
	}

	if err := validateMerchantInvoiceTx(tx); err != nil {
		return nil, err
	}

	result, err :=
		s.Models.
			MerchantInvoice.
			IssueTx(
				ctx,
				tx,
				id,
				dueAt,
			)
	if err != nil {
		return nil, fmt.Errorf(
			"issue merchant invoice in transaction: %w",
			err,
		)
	}

	return result, nil
}

// IssueMerchantInvoiceDueNowInternal transitions a draft invoice to issued and
// uses one database timestamp for both issued_at and due_at.
//
// Calling this method represents an already-resolved policy decision that the
// invoice is due immediately. The service does not decide when that policy
// applies.
func (s *Service) IssueMerchantInvoiceDueNowInternal(
	ctx context.Context,
	id uuid.UUID,
) (*data.MerchantInvoice, error) {
	dbCtx, cancel, err := s.merchantInvoiceContext(ctx)
	if err != nil {
		return nil, err
	}
	defer cancel()

	result, err :=
		s.Models.
			MerchantInvoice.
			IssueDueNow(
				dbCtx,
				id,
			)
	if err != nil {
		return nil, fmt.Errorf(
			"issue merchant invoice due now: %w",
			err,
		)
	}

	return result, nil
}

// IssueMerchantInvoiceDueNowTxInternal is the transaction-aware form of
// IssueMerchantInvoiceDueNowInternal.
func (s *Service) IssueMerchantInvoiceDueNowTxInternal(
	ctx context.Context,
	tx pgx.Tx,
	id uuid.UUID,
) (*data.MerchantInvoice, error) {
	if ctx == nil {
		return nil, ErrNilContext
	}

	if err := validateMerchantInvoiceTxService(s); err != nil {
		return nil, err
	}

	if err := validateMerchantInvoiceTx(tx); err != nil {
		return nil, err
	}

	result, err :=
		s.Models.
			MerchantInvoice.
			IssueDueNowTx(
				ctx,
				tx,
				id,
			)
	if err != nil {
		return nil, fmt.Errorf(
			"issue merchant invoice due now in transaction: %w",
			err,
		)
	}

	return result, nil
}

// SettleMerchantInvoiceZeroBalanceInternal transitions an issued zero-total
// invoice to paid without recording a payment.
//
// Platform Credits and other pre-payment reductions are not payments. This
// capability records that an issued invoice has no remaining payment
// obligation; it must not manufacture a zero-value merchant payment.
func (s *Service) SettleMerchantInvoiceZeroBalanceInternal(
	ctx context.Context,
	id uuid.UUID,
) (*data.MerchantInvoice, error) {
	dbCtx, cancel, err := s.merchantInvoiceContext(ctx)
	if err != nil {
		return nil, err
	}
	defer cancel()

	result, err :=
		s.Models.
			MerchantInvoice.
			SettleZeroBalance(
				dbCtx,
				id,
			)
	if err != nil {
		return nil, fmt.Errorf(
			"settle zero-balance merchant invoice: %w",
			err,
		)
	}

	return result, nil
}

// SettleMerchantInvoiceZeroBalanceTxInternal is the transaction-aware form of
// SettleMerchantInvoiceZeroBalanceInternal.
func (s *Service) SettleMerchantInvoiceZeroBalanceTxInternal(
	ctx context.Context,
	tx pgx.Tx,
	id uuid.UUID,
) (*data.MerchantInvoice, error) {
	if ctx == nil {
		return nil, ErrNilContext
	}

	if err := validateMerchantInvoiceTxService(s); err != nil {
		return nil, err
	}

	if err := validateMerchantInvoiceTx(tx); err != nil {
		return nil, err
	}

	result, err :=
		s.Models.
			MerchantInvoice.
			SettleZeroBalanceTx(
				ctx,
				tx,
				id,
			)
	if err != nil {
		return nil, fmt.Errorf(
			"settle zero-balance merchant invoice in transaction: %w",
			err,
		)
	}

	return result, nil
}

// MarkMerchantInvoiceOverdueInternal transitions an eligible issued invoice to
// overdue.
//
// Eligibility facts are enforced by MerchantInvoiceModel: the invoice must
// still be issued, have an unpaid balance, have due_at, and have passed its due
// point. This service does not determine commercial grace policy, scheduler
// cadence, or processing cutoff policy.
func (s *Service) MarkMerchantInvoiceOverdueInternal(
	ctx context.Context,
	id uuid.UUID,
) (*data.MerchantInvoice, error) {
	dbCtx, cancel, err := s.merchantInvoiceContext(ctx)
	if err != nil {
		return nil, err
	}
	defer cancel()

	result, err :=
		s.Models.
			MerchantInvoice.
			MarkOverdue(
				dbCtx,
				id,
			)
	if err != nil {
		return nil, fmt.Errorf(
			"mark merchant invoice overdue: %w",
			err,
		)
	}

	return result, nil
}

// MarkMerchantInvoiceOverdueTxInternal is the transaction-aware form of
// MarkMerchantInvoiceOverdueInternal.
//
// A future durable worker may use this method when the invoice transition and
// durable worker completion state must participate in the same caller-owned
// transaction.
func (s *Service) MarkMerchantInvoiceOverdueTxInternal(
	ctx context.Context,
	tx pgx.Tx,
	id uuid.UUID,
) (*data.MerchantInvoice, error) {
	if ctx == nil {
		return nil, ErrNilContext
	}

	if err := validateMerchantInvoiceTxService(s); err != nil {
		return nil, err
	}

	if err := validateMerchantInvoiceTx(tx); err != nil {
		return nil, err
	}

	result, err :=
		s.Models.
			MerchantInvoice.
			MarkOverdueTx(
				ctx,
				tx,
				id,
			)
	if err != nil {
		return nil, fmt.Errorf(
			"mark merchant invoice overdue in transaction: %w",
			err,
		)
	}

	return result, nil
}

// VoidMerchantInvoiceInternal transitions an issued, unpaid invoice to terminal
// void state.
//
// This method introduces no cancellation, refund, reversal, or credit semantics
// beyond the locked v1 invoice lifecycle.
func (s *Service) VoidMerchantInvoiceInternal(
	ctx context.Context,
	id uuid.UUID,
) (*data.MerchantInvoice, error) {
	dbCtx, cancel, err := s.merchantInvoiceContext(ctx)
	if err != nil {
		return nil, err
	}
	defer cancel()

	result, err :=
		s.Models.
			MerchantInvoice.
			Void(
				dbCtx,
				id,
			)
	if err != nil {
		return nil, fmt.Errorf(
			"void merchant invoice: %w",
			err,
		)
	}

	return result, nil
}

// VoidMerchantInvoiceTxInternal is the transaction-aware form of
// VoidMerchantInvoiceInternal.
func (s *Service) VoidMerchantInvoiceTxInternal(
	ctx context.Context,
	tx pgx.Tx,
	id uuid.UUID,
) (*data.MerchantInvoice, error) {
	if ctx == nil {
		return nil, ErrNilContext
	}

	if err := validateMerchantInvoiceTxService(s); err != nil {
		return nil, err
	}

	if err := validateMerchantInvoiceTx(tx); err != nil {
		return nil, err
	}

	result, err :=
		s.Models.
			MerchantInvoice.
			VoidTx(
				ctx,
				tx,
				id,
			)
	if err != nil {
		return nil, fmt.Errorf(
			"void merchant invoice in transaction: %w",
			err,
		)
	}

	return result, nil
}
