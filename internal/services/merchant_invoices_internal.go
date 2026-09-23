// Package services contains trusted internal Commerce Architecture
// orchestration for merchant invoice lifecycle mutation.
//
// focodebase/fobackend/internal/services/merchant_invoices_internal.go
//
// GTM:
//
//	Layer: 2.4.b Commerce Architecture / Monetization Layer
//	Release Class: SPINE
//	Reason:
//	  merchant_invoices is the canonical durable statement of one merchant's
//	  commercial obligation for one Future Offering in one currency.
//
//	  This service provides the trusted internal mutation boundary above
//	  MerchantInvoiceModel for draft construction, transaction-owned invoice
//	  composition, reconciliation-coupled issuance, zero-balance settlement,
//	  overdue transition, and voiding.
//
// Domain Boundary:
//
//	This service owns:
//	  - trusted internal entrypoints for merchant-invoice lifecycle mutation;
//	  - construction of empty draft invoice headers from already-resolved identity;
//	  - caller-owned-transaction composition entrypoints for row locking,
//	    reconciliation, and issuance;
//	  - DBTimeout-bounded execution for legitimate pool-backed service calls;
//	  - preservation of caller-owned transaction semantics for Tx variants;
//	  - the stable service boundary through which future Commerce orchestration
//	    composes merchant invoice mutation.
//
//	This service does not own:
//	  - invoice-generation policy;
//	  - invoice-number sequencing policy;
//	  - fee calculation or fee-schedule selection;
//	  - invoice-item or normalized monetary-effect calculation;
//	  - payment-term or grace-period policy;
//	  - currency selection or merchant-domicile assumptions;
//	  - the decision that an invoice is due immediately;
//	  - overdue-processing cadence or collection policy;
//	  - Platform Credit eligibility, allocation, or account selection;
//	  - promotions or merchant-specific commercial policy;
//	  - payment execution or authoritative merchant_payments persistence;
//	  - HTTP authorization or mutation exposure;
//	  - asynchronous scheduling, retry, worker claiming, or failure handling.
//
// Invoice Identity:
//
//	One merchant invoice represents exactly:
//
//	  one merchant
//	  + one Future Offering
//	  + one currency
//	  + one invoice obligation.
//
//	MerchantID, FutureOfferingID, InvoiceNumber, and Currency are therefore
//	required draft-creation facts.
//
// Draft Construction:
//
//	Draft creation establishes only the invoice header and immutable commercial
//	identity needed before normalized invoice composition exists.
//
//	A new draft starts with:
//
//	  subtotal_amount = 0
//	  total_amount    = 0
//	  amount_paid     = 0
//
//	Callers must not supply subtotal or total as draft-creation truth.
//
//	MerchantInvoiceModel validates Future Offering ownership, invoice number,
//	currency, structural state, and persistence invariants.
//
// Production Creation Gate:
//
//	CreateMerchantInvoiceDraftInternal and
//	CreateMerchantInvoiceDraftTxInternal are valid low-level capabilities.
//
//	They do not by themselves constitute a production invoice-generation
//	workflow.
//
//	Production invoice generation remains gated until normalized invoice
//	composition can durably associate the invoice with the authoritative
//	fee calculations, invoice items, Platform Credit applications, taxes,
//	surcharges, discounts, rebates, or other economically meaningful records
//	that produced the final obligation.
//
//	This service must never substitute opaque JSON, notes, invoice numbers,
//	logs, or caller conventions for that normalized provenance.
//
// Monetary Boundary:
//
//	This service performs no monetary arithmetic.
//
//	There is no generic invoice-wide adjustment scalar.
//
//	SubtotalAmount is the reconciled gross aggregate represented by the
//	canonical invoice composition contract.
//
//	TotalAmount is the independently reconciled final pre-settlement merchant
//	obligation after authoritative normalized monetary effects.
//
//	Reconciliation values remain exact decimal strings / PostgreSQL NUMERIC
//	values. This service never converts monetary values through float32 or
//	float64, silently rounds them, or performs currency conversion.
//
//	Currency is selected before draft creation and cannot be mutated through
//	this service after the draft exists.
//
// Composition and Issuance Boundary:
//
//	Reconciliation and issuance are transaction-only.
//
//	The coordinating Commerce workflow must:
//
//	  1. own the database transaction;
//	  2. row-lock the invoice through LockMerchantInvoiceForCompositionTxInternal;
//	  3. derive normalized authoritative invoice composition;
//	  4. derive MerchantInvoiceReconciliation from that composition;
//	  5. reconcile the draft or reconcile-and-issue it inside the same
//	     transaction;
//	  6. commit only when the complete composition workflow succeeds.
//
//	There is deliberately no pool-backed reconciliation or issuance method.
//
//	This prevents a draft from being issued using stale, caller-invented, or
//	unreconciled financial aggregates.
//
// Due-Date Boundary:
//
//	ReconcileAndIssueMerchantInvoiceTxInternal accepts an optional already-
//	resolved dueAt.
//
//	ReconcileAndIssueMerchantInvoiceDueNowTxInternal provides the Engineering
//	capability for an invoice whose configured commercial policy has already
//	resolved to "due immediately."
//
//	This service never decides which merchant, Future Offering, invoice class,
//	fee, or commercial arrangement receives either treatment.
//
// Transaction Boundary:
//
//	Pool-backed entrypoints use one outer service DBTimeout.
//
//	Tx-suffixed entrypoints inherit the caller's context and supplied pgx.Tx.
//	They never begin, commit, roll back, or replace that transaction and do not
//	introduce an independent service timeout.
//
//	The data model may still apply its defensive per-database-operation timeout;
//	transaction ownership remains with the caller.
//
// Payment-Composition Boundary:
//
//	This file intentionally does not expose ApplyMerchantInvoicePaymentInternal
//	or ApplyMerchantInvoicePaymentTxInternal.
//
//	MerchantInvoiceModel.ApplyPaymentTx is transaction-only because an
//	amount_paid increment must commit atomically with the authoritative
//	merchant_payments record that explains it.
//
//	Merchant Payments orchestration owns that future transaction.
//
//	This Commerce service therefore never:
//	  - executes payment;
//	  - applies payment independently;
//	  - creates an invoice-only payment transaction;
//	  - treats an amount as a payment identity or idempotency key;
//	  - infers settlement from a provider attempt;
//	  - moves Merchant Payments responsibility into Commerce.
//
// Zero-Balance Boundary:
//
//	A reconciled invoice may legitimately have total_amount = 0.
//
//	Settling such an issued invoice to paid records that no payment obligation
//	remains. It does not manufacture a zero-value payment.
//
//	Platform Credits and other pre-settlement commercial reductions are not
//	payments.
//
// Concurrency:
//
//	Composition workflows acquire the invoice row lock before deriving and
//	persisting authoritative aggregates.
//
//	Simple lifecycle transitions use data-layer guarded UPDATE predicates.
//	Concurrent or retried operations cannot bypass persisted lifecycle guards.
//
//	Payment concurrency and payment retry idempotency remain owned by the future
//	authoritative merchant_payments transaction and its durable idempotency
//	boundary.
//
// Asynchronous Responsibility:
//
//	Issued invoices reaching due_at represent genuine time-based future work.
//
//	This service provides the transition capability required by future durable
//	Automation Foundation workers but does not start timers, worker engines,
//	cron jobs, retry loops, or scheduler infrastructure.
//
// Observability:
//
//	MerchantInvoiceModel owns persistence/lifecycle logging.
//
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
//	Preserve one-merchant + one-Future-Offering + one-currency invoice identity.
//	Preserve empty zero-value draft construction.
//	Preserve exact monetary representation.
//	Preserve normalized composition provenance.
//	Preserve transaction-only draft reconciliation.
//	Preserve reconciliation-coupled transaction-only issuance.
//	Preserve post-issuance monetary and currency immutability.
//	Preserve guarded forward-only lifecycle behavior.
//	Preserve caller-owned transaction semantics.
//	Preserve transaction-only payment application at the Merchant Payments
//	composition boundary.
//	Preserve errors.Is compatibility through %w wrapping.
//	Never reintroduce adjustment_amount or another generic monetary catch-all.
//	Never invent invoice-generation, payment-term, due-date, currency-selection,
//	pricing, promotion, Platform Credit, payment, or collection policy.
//	Never expose standalone or pool-backed invoice issuance.
//	Never expose an invoice-only or pool-backed payment-application capability.
//	Never begin, commit, or roll back caller-owned transactions.
//	Never introduce domain asynchronous execution without the durable Automation
//	Foundation required by BEG.
//	Block deployment if this file breaks monetary integrity, provenance,
//	lifecycle safety, transaction discipline, historical integrity, payment
//	composition, or billing-reconciliation readiness.
package services

import (
	"context"
	"fmt"
	"time"

	"github.com/PiccoloMondoC/focodebase/fobackend/internal/data"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// CreateMerchantInvoiceDraftInput contains the already-resolved identity facts
// required to create one empty draft merchant invoice.
//
// MerchantID, FutureOfferingID, InvoiceNumber, and Currency must already have
// been resolved by trusted upstream Commerce orchestration or governed
// configuration.
//
// This service does not decide whether an invoice should exist, generate its
// number, choose its currency, calculate charges, or derive invoice composition.
type CreateMerchantInvoiceDraftInput struct {
	MerchantID       uuid.UUID
	FutureOfferingID uuid.UUID
	InvoiceNumber    string
	Currency         string
}

func validateMerchantInvoicePoolService(s *Service) error {
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
	if err := s.validate(); err != nil {
		return err
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

	if err := validateMerchantInvoicePoolService(s); err != nil {
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

func validateMerchantInvoiceTxCall(
	s *Service,
	ctx context.Context,
	tx pgx.Tx,
) error {
	if ctx == nil {
		return ErrNilContext
	}

	if err := validateMerchantInvoiceTxService(s); err != nil {
		return err
	}

	return validateMerchantInvoiceTx(tx)
}

func merchantInvoiceDraftFromInput(
	input CreateMerchantInvoiceDraftInput,
) *data.MerchantInvoice {
	return &data.MerchantInvoice{
		MerchantID:       input.MerchantID,
		FutureOfferingID: input.FutureOfferingID,
		InvoiceNumber:    input.InvoiceNumber,
		Currency:         input.Currency,
	}
}

// CreateMerchantInvoiceDraftInternal creates one empty draft merchant invoice
// from already-resolved invoice identity facts.
//
// MerchantInvoiceModel remains authoritative for structural validation,
// merchant/Future Offering ownership enforcement, invoice-number
// canonicalization and uniqueness, currency canonicalization, and persisted
// draft-state invariants.
//
// Draft financial aggregates are intentionally not accepted here. They are
// derived later from normalized invoice composition.
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
// The caller owns transaction begin, commit, rollback, and workflow timeout.
// This method never replaces or completes the supplied transaction.
func (s *Service) CreateMerchantInvoiceDraftTxInternal(
	ctx context.Context,
	tx pgx.Tx,
	input CreateMerchantInvoiceDraftInput,
) (*data.MerchantInvoice, error) {
	if err := validateMerchantInvoiceTxCall(s, ctx, tx); err != nil {
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

// LockMerchantInvoiceForCompositionTxInternal retrieves and row-locks one
// merchant invoice inside a caller-owned transaction.
//
// Commerce orchestration uses this capability before deriving normalized invoice
// composition for draft reconciliation or issuance. Keeping the lock acquisition
// behind the service boundary prevents coordinating workflows from bypassing the
// canonical merchant-invoice service contract.
func (s *Service) LockMerchantInvoiceForCompositionTxInternal(
	ctx context.Context,
	tx pgx.Tx,
	id uuid.UUID,
) (*data.MerchantInvoice, error) {
	if err := validateMerchantInvoiceTxCall(s, ctx, tx); err != nil {
		return nil, err
	}

	result, err :=
		s.Models.
			MerchantInvoice.
			GetByIDForUpdateTx(
				ctx,
				tx,
				id,
			)
	if err != nil {
		return nil, fmt.Errorf(
			"lock merchant invoice for composition: %w",
			err,
		)
	}

	return result, nil
}

// ReconcileMerchantInvoiceDraftTxInternal persists the authoritative gross
// subtotal and final pre-settlement obligation of a draft invoice.
//
// The caller must already own the transaction, hold the invoice row lock through
// LockMerchantInvoiceForCompositionTxInternal, and have derived reconciliation
// from authoritative normalized invoice composition inside that transaction.
//
// This method does not calculate fees, credits, rebates, discounts, taxes,
// surcharges, or any other monetary effect.
func (s *Service) ReconcileMerchantInvoiceDraftTxInternal(
	ctx context.Context,
	tx pgx.Tx,
	id uuid.UUID,
	reconciliation data.MerchantInvoiceReconciliation,
) (*data.MerchantInvoice, error) {
	if err := validateMerchantInvoiceTxCall(s, ctx, tx); err != nil {
		return nil, err
	}

	result, err :=
		s.Models.
			MerchantInvoice.
			ReconcileDraftTx(
				ctx,
				tx,
				id,
				reconciliation,
			)
	if err != nil {
		return nil, fmt.Errorf(
			"reconcile merchant invoice draft in transaction: %w",
			err,
		)
	}

	return result, nil
}

// ReconcileAndIssueMerchantInvoiceTxInternal atomically persists final
// reconciled invoice aggregates and transitions the draft to issued.
//
// dueAt is an already-resolved commercial-policy result. Nil is permitted where
// the governing commercial arrangement has no due date.
//
// The caller must own the transaction, hold the invoice row lock through
// LockMerchantInvoiceForCompositionTxInternal, and derive reconciliation from
// authoritative normalized composition in the same transaction.
func (s *Service) ReconcileAndIssueMerchantInvoiceTxInternal(
	ctx context.Context,
	tx pgx.Tx,
	id uuid.UUID,
	reconciliation data.MerchantInvoiceReconciliation,
	dueAt *time.Time,
) (*data.MerchantInvoice, error) {
	if err := validateMerchantInvoiceTxCall(s, ctx, tx); err != nil {
		return nil, err
	}

	result, err :=
		s.Models.
			MerchantInvoice.
			ReconcileAndIssueTx(
				ctx,
				tx,
				id,
				reconciliation,
				dueAt,
			)
	if err != nil {
		return nil, fmt.Errorf(
			"reconcile and issue merchant invoice in transaction: %w",
			err,
		)
	}

	return result, nil
}

// ReconcileAndIssueMerchantInvoiceDueNowTxInternal is the due-immediately
// capability for reconciled invoice issuance.
//
// Calling this method represents an already-resolved commercial-policy decision
// that the invoice is due immediately. Engineering provides the capability but
// this service does not decide when it applies.
//
// The data model uses one database timestamp for issued_at and due_at, avoiding
// application/database clock skew.
func (s *Service) ReconcileAndIssueMerchantInvoiceDueNowTxInternal(
	ctx context.Context,
	tx pgx.Tx,
	id uuid.UUID,
	reconciliation data.MerchantInvoiceReconciliation,
) (*data.MerchantInvoice, error) {
	if err := validateMerchantInvoiceTxCall(s, ctx, tx); err != nil {
		return nil, err
	}

	result, err :=
		s.Models.
			MerchantInvoice.
			ReconcileAndIssueDueNowTx(
				ctx,
				tx,
				id,
				reconciliation,
			)
	if err != nil {
		return nil, fmt.Errorf(
			"reconcile and issue due-now merchant invoice in transaction: %w",
			err,
		)
	}

	return result, nil
}

// SettleMerchantInvoiceZeroBalanceInternal transitions an issued zero-total
// invoice to paid without recording a payment.
//
// Platform Credits and other pre-settlement monetary reductions are not
// payments. This capability records only that the issued invoice has no
// remaining payment obligation.
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
	if err := validateMerchantInvoiceTxCall(s, ctx, tx); err != nil {
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
// MerchantInvoiceModel enforces the invariant eligibility conditions. This
// service does not determine grace periods, collection policy, scheduler
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
// A future durable worker may use this form when the invoice transition and its
// durable work-completion state must commit atomically in one caller-owned
// transaction.
func (s *Service) MarkMerchantInvoiceOverdueTxInternal(
	ctx context.Context,
	tx pgx.Tx,
	id uuid.UUID,
) (*data.MerchantInvoice, error) {
	if err := validateMerchantInvoiceTxCall(s, ctx, tx); err != nil {
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

// VoidMerchantInvoiceInternal transitions an eligible issued unpaid invoice to
// terminal void state.
//
// This capability introduces no refund, cancellation, payment reversal, credit,
// or other commercial semantics beyond the locked merchant-invoice lifecycle.
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
	if err := validateMerchantInvoiceTxCall(s, ctx, tx); err != nil {
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
