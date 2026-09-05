// Package services contains trusted internal orchestration for merchant fee
// calculation recording and lifecycle transitions.
//
// sdworkspace/sdbackend/internal/services/merchant_fee_calculations_internal.go
//
// GTM:
//
//	Layer: 2.4.b Commerce Architecture / Monetization Layer
//	Release Class: SPINE
//	Reason:
//	  merchant_fee_calculations is the canonical durable record of the monetary
//	  result produced for a merchant billable occurrence. This service provides
//	  the trusted internal boundary for recording an already-resolved calculation
//	  snapshot while proving its merchant ownership from the authoritative
//	  merchant_billable_events source.
//
//	  Calculation-policy resolution remains separate from durable calculation
//	  recording. Until the governing calculation contract defines authoritative
//	  percentage bases, fee-rate semantics, rounding behavior, and transaction-
//	  compatible fee-schedule resolution, this service must not manufacture those
//	  rules.
//
// Domain Boundary:
//
//	This service owns:
//	  - authoritative billable-event resolution before calculation insertion;
//	  - source-derived merchant ownership;
//	  - pool-backed recording against an already committed billable event;
//	  - caller-owned transaction-compatible recording;
//	  - guarded approve, waive, settle, and reverse lifecycle transitions.
//
//	This service does not own:
//	  - fee-schedule selection;
//	  - fee arithmetic or rounding policy;
//	  - commercial eligibility;
//	  - promotion or merchant-adjustment application;
//	  - platform-credit application;
//	  - invoicing;
//	  - payment execution or provider state;
//	  - HTTP authorization or mutation exposure;
//	  - asynchronous scheduling, retry, or reconciliation ownership.
//
// Source-Integrity Contract:
//
//	MerchantID is never accepted from the caller. It is derived from the
//	authoritative merchant_billable_events row through
//	MerchantFeeCalculationModel.GetBillableEventFact or
//	GetBillableEventFactTx before insertion.
//
//	The database foreign key proves billable-event existence, but cannot prove
//	that independently persisted merchant_id belongs to that event. This service
//	closes that relational-integrity gap by construction.
//
// Resolved-Calculation Contract:
//
//	RecordMerchantFeeCalculationInput represents calculation facts that have
//	already been resolved by trusted Commerce orchestration.
//
//	The service does not reinterpret, recompute, round, discount, promote,
//	credit, or otherwise transform those monetary facts. Structural validation
//	and canonical decimal/currency representation remain owned by
//	MerchantFeeCalculationModel.
//
//	A FeeScheduleID may be absent. Its absence must not be converted into a
//	commercial-policy decision because the data architecture deliberately permits
//	governed calculations whose pricing provenance is not represented by a
//	standard fee schedule.
//
// Transaction Boundary:
//
//	Pool-backed recording exists for reconciliation against an authoritative
//	billable event that is already durably committed.
//
//	Tx-suffixed methods perform source resolution and calculation persistence
//	exclusively through the supplied caller-owned pgx.Tx. They never begin,
//	commit, roll back, or silently replace that transaction.
//
// Concurrency:
//
//	The database partial unique index on
//	(billable_event_id, fee_type_id) WHERE status <> 'reversed'
//	is the durable concurrency and idempotency authority.
//
//	This service never performs SELECT-before-INSERT duplicate detection.
//
// Recalculation:
//
//	Recalculation is explicit and history-preserving. Coordinating Commerce
//	orchestration must reverse the current calculation and insert its replacement,
//	preferably within the same caller-owned transaction when atomicity is
//	required.
//
// SPINE Rule:
//
//	Keep compiling.
//	Keep production-ready.
//	Preserve source-derived merchant ownership.
//	Preserve immutable resolved calculation snapshots.
//	Preserve exact decimal strings.
//	Preserve caller-owned transaction semantics.
//	Preserve database-owned concurrency and idempotency.
//	Preserve guarded lifecycle transitions.
//	Preserve errors.Is compatibility through %w wrapping.
//	Never trust caller-supplied merchant ownership.
//	Never invent percentage, rate, rounding, fee-selection, promotion, credit,
//	invoicing, payment, or collection policy.
//	Never perform monetary arithmetic using float32 or float64.
//	Never silently repair malformed calculation provenance.
//	Never begin, commit, or roll back caller-owned transactions.
//	Never introduce asynchronous processing without a durable asynchronous owner.
//	Block deployment if this file breaks source integrity, transaction discipline,
//	monetary snapshot integrity, lifecycle safety, or billing reconciliation
//	readiness.
package services

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/PiccoloMondoC/focodebase/fobackend/internal/data"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// RecordMerchantFeeCalculationInput contains one already-resolved calculation
// snapshot to be recorded against an authoritative merchant billable event.
//
// MerchantID is deliberately absent. The service derives merchant ownership
// from BillableEventID before persistence.
//
// CalculationBasis contains write-once calculation inputs or provenance already
// produced by trusted Commerce orchestration. It is not a policy store and must
// not contain platform-credit, invoice, payment, promotion, lifecycle, secret,
// credential, or consumer-identity data.
type RecordMerchantFeeCalculationInput struct {
	BillableEventID uuid.UUID

	FeeScheduleID *uuid.UUID
	FeeTypeID     uuid.UUID

	CalculationMethod data.MerchantFeeCalculationMethod
	FeeRate           *string
	FlatFeeAmount     *string

	CalculatedFeeAmount string
	Currency            string
	CalculationBasis    json.RawMessage
}

func validateMerchantFeeCalculationService(
	s *Service,
) error {
	if err := s.validate(); err != nil {
		return err
	}

	if s.Models.MerchantFeeCalculation.DB == nil {
		return fmt.Errorf(
			"%w: merchant fee calculation model database is nil",
			ErrInvalidServiceConfiguration,
		)
	}

	if s.Models.MerchantFeeCalculation.Logger == nil {
		return fmt.Errorf(
			"%w: merchant fee calculation model logger is nil",
			ErrInvalidServiceConfiguration,
		)
	}

	return nil
}

func validateMerchantFeeCalculationTxService(
	s *Service,
) error {
	if s == nil {
		return fmt.Errorf(
			"%w: merchant fee calculation service is nil",
			ErrInvalidServiceConfiguration,
		)
	}

	if s.Logger == nil {
		return fmt.Errorf(
			"%w: merchant fee calculation service logger is nil",
			ErrInvalidServiceConfiguration,
		)
	}

	if s.Models == nil {
		return fmt.Errorf(
			"%w: merchant fee calculation service models are nil",
			ErrInvalidServiceConfiguration,
		)
	}

	if s.Models.MerchantFeeCalculation.Logger == nil {
		return fmt.Errorf(
			"%w: merchant fee calculation model logger is nil",
			ErrInvalidServiceConfiguration,
		)
	}

	return nil
}

func (s *Service) merchantFeeCalculationContext(
	ctx context.Context,
) (context.Context, context.CancelFunc, error) {
	if ctx == nil {
		return nil, nil, ErrNilContext
	}

	if err := validateMerchantFeeCalculationService(s); err != nil {
		return nil, nil, err
	}

	dbCtx, cancel := context.WithTimeout(
		ctx,
		s.Cfg.DBTimeout,
	)

	return dbCtx, cancel, nil
}

func validateMerchantFeeCalculationTx(
	tx pgx.Tx,
) error {
	if tx == nil {
		return fmt.Errorf(
			"%w: transaction is required",
			data.ErrMerchantFeeCalculationInvalidInput,
		)
	}

	return nil
}

func merchantFeeCalculationFromSourceFact(
	input RecordMerchantFeeCalculationInput,
	fact *data.MerchantFeeCalculationBillableEventFact,
) (*data.MerchantFeeCalculation, error) {
	if fact == nil {
		return nil, fmt.Errorf(
			"%w: billable event %s",
			data.ErrMerchantFeeCalculationBillableEventNotFound,
			input.BillableEventID,
		)
	}

	return &data.MerchantFeeCalculation{
		MerchantID:          fact.MerchantID,
		BillableEventID:     input.BillableEventID,
		FeeScheduleID:       input.FeeScheduleID,
		FeeTypeID:           input.FeeTypeID,
		CalculationMethod:   input.CalculationMethod,
		FeeRate:             input.FeeRate,
		FlatFeeAmount:       input.FlatFeeAmount,
		CalculatedFeeAmount: input.CalculatedFeeAmount,
		Currency:            input.Currency,
		CalculationBasis:    input.CalculationBasis,
	}, nil
}

// RecordMerchantFeeCalculationInternal records an already-resolved merchant fee
// calculation against an authoritative billable event that is already durably
// committed.
//
// The method derives MerchantID from the billable event and relies on the
// MerchantFeeCalculationModel for structural validation, decimal/currency
// canonicalization, foreign-key enforcement, and database-owned active
// calculation uniqueness.
func (s *Service) RecordMerchantFeeCalculationInternal(
	ctx context.Context,
	input RecordMerchantFeeCalculationInput,
) (*data.MerchantFeeCalculation, error) {
	dbCtx, cancel, err := s.merchantFeeCalculationContext(ctx)
	if err != nil {
		return nil, err
	}
	defer cancel()

	logger := s.Logger.
		GetLoggerWithContextFromContext(ctx).
		WithFunctionName(
			"RecordMerchantFeeCalculationInternal",
		)

	fact, err :=
		s.Models.
			MerchantFeeCalculation.
			GetBillableEventFact(
				dbCtx,
				input.BillableEventID,
			)
	if err != nil {
		return nil, fmt.Errorf(
			"resolve merchant fee calculation billable event: %w",
			err,
		)
	}

	calc, err :=
		merchantFeeCalculationFromSourceFact(
			input,
			fact,
		)
	if err != nil {
		return nil, err
	}

	result, err :=
		s.Models.
			MerchantFeeCalculation.
			Insert(
				dbCtx,
				calc,
			)
	if err != nil {
		return nil, fmt.Errorf(
			"record merchant fee calculation: %w",
			err,
		)
	}

	logger.Info(
		"Merchant fee calculation recorded",
		"fee_calculation_id", result.ID,
		"merchant_id", result.MerchantID,
		"billable_event_id", result.BillableEventID,
		"fee_type_id", result.FeeTypeID,
		"calculation_method", result.CalculationMethod,
		"status", result.Status,
	)

	return result, nil
}

// RecordMerchantFeeCalculationTxInternal is the transaction-aware form of
// RecordMerchantFeeCalculationInternal.
//
// Source resolution and calculation insertion execute exclusively through tx.
// The caller owns transaction lifecycle.
func (s *Service) RecordMerchantFeeCalculationTxInternal(
	ctx context.Context,
	tx pgx.Tx,
	input RecordMerchantFeeCalculationInput,
) (*data.MerchantFeeCalculation, error) {
	if ctx == nil {
		return nil, ErrNilContext
	}

	if err := validateMerchantFeeCalculationTxService(s); err != nil {
		return nil, err
	}

	if err := validateMerchantFeeCalculationTx(tx); err != nil {
		return nil, err
	}

	logger := s.Logger.
		GetLoggerWithContextFromContext(ctx).
		WithFunctionName(
			"RecordMerchantFeeCalculationTxInternal",
		)

	fact, err :=
		s.Models.
			MerchantFeeCalculation.
			GetBillableEventFactTx(
				ctx,
				tx,
				input.BillableEventID,
			)
	if err != nil {
		return nil, fmt.Errorf(
			"resolve merchant fee calculation billable event in transaction: %w",
			err,
		)
	}

	calc, err :=
		merchantFeeCalculationFromSourceFact(
			input,
			fact,
		)
	if err != nil {
		return nil, err
	}

	result, err :=
		s.Models.
			MerchantFeeCalculation.
			InsertTx(
				ctx,
				tx,
				calc,
			)
	if err != nil {
		return nil, fmt.Errorf(
			"record merchant fee calculation in transaction: %w",
			err,
		)
	}

	logger.Info(
		"Merchant fee calculation recorded in transaction",
		"fee_calculation_id", result.ID,
		"merchant_id", result.MerchantID,
		"billable_event_id", result.BillableEventID,
		"fee_type_id", result.FeeTypeID,
		"calculation_method", result.CalculationMethod,
		"status", result.Status,
	)

	return result, nil
}

// ApproveMerchantFeeCalculationInternal transitions a pending calculation to
// approved.
//
// The coordinating Commerce workflow owns the commercial reason for approval.
// This method adds no approval policy beyond the guarded persistence lifecycle.
func (s *Service) ApproveMerchantFeeCalculationInternal(
	ctx context.Context,
	id uuid.UUID,
) (*data.MerchantFeeCalculation, error) {
	dbCtx, cancel, err := s.merchantFeeCalculationContext(ctx)
	if err != nil {
		return nil, err
	}
	defer cancel()

	result, err :=
		s.Models.
			MerchantFeeCalculation.
			Approve(
				dbCtx,
				id,
			)
	if err != nil {
		return nil, fmt.Errorf(
			"approve merchant fee calculation: %w",
			err,
		)
	}

	return result, nil
}

// ApproveMerchantFeeCalculationTxInternal is the transaction-aware form of
// ApproveMerchantFeeCalculationInternal.
func (s *Service) ApproveMerchantFeeCalculationTxInternal(
	ctx context.Context,
	tx pgx.Tx,
	id uuid.UUID,
) (*data.MerchantFeeCalculation, error) {
	if ctx == nil {
		return nil, ErrNilContext
	}

	if err := validateMerchantFeeCalculationTxService(s); err != nil {
		return nil, err
	}

	if err := validateMerchantFeeCalculationTx(tx); err != nil {
		return nil, err
	}

	result, err :=
		s.Models.
			MerchantFeeCalculation.
			ApproveTx(
				ctx,
				tx,
				id,
			)
	if err != nil {
		return nil, fmt.Errorf(
			"approve merchant fee calculation in transaction: %w",
			err,
		)
	}

	return result, nil
}

// WaiveMerchantFeeCalculationInternal transitions a pending or approved
// calculation to waived.
//
// The coordinating Commerce workflow owns the commercial reason for waiver.
// Waiver remains distinct from reversal.
func (s *Service) WaiveMerchantFeeCalculationInternal(
	ctx context.Context,
	id uuid.UUID,
) (*data.MerchantFeeCalculation, error) {
	dbCtx, cancel, err := s.merchantFeeCalculationContext(ctx)
	if err != nil {
		return nil, err
	}
	defer cancel()

	result, err :=
		s.Models.
			MerchantFeeCalculation.
			Waive(
				dbCtx,
				id,
			)
	if err != nil {
		return nil, fmt.Errorf(
			"waive merchant fee calculation: %w",
			err,
		)
	}

	return result, nil
}

// WaiveMerchantFeeCalculationTxInternal is the transaction-aware form of
// WaiveMerchantFeeCalculationInternal.
func (s *Service) WaiveMerchantFeeCalculationTxInternal(
	ctx context.Context,
	tx pgx.Tx,
	id uuid.UUID,
) (*data.MerchantFeeCalculation, error) {
	if ctx == nil {
		return nil, ErrNilContext
	}

	if err := validateMerchantFeeCalculationTxService(s); err != nil {
		return nil, err
	}

	if err := validateMerchantFeeCalculationTx(tx); err != nil {
		return nil, err
	}

	result, err :=
		s.Models.
			MerchantFeeCalculation.
			WaiveTx(
				ctx,
				tx,
				id,
			)
	if err != nil {
		return nil, fmt.Errorf(
			"waive merchant fee calculation in transaction: %w",
			err,
		)
	}

	return result, nil
}

// SettleMerchantFeeCalculationInternal transitions an approved calculation to
// settled.
//
// Settled is a Commerce-layer lifecycle fact. It is not a payment-provider
// transaction status. The coordinating workflow owns any payment coordination
// required before invoking this capability.
func (s *Service) SettleMerchantFeeCalculationInternal(
	ctx context.Context,
	id uuid.UUID,
) (*data.MerchantFeeCalculation, error) {
	dbCtx, cancel, err := s.merchantFeeCalculationContext(ctx)
	if err != nil {
		return nil, err
	}
	defer cancel()

	result, err :=
		s.Models.
			MerchantFeeCalculation.
			Settle(
				dbCtx,
				id,
			)
	if err != nil {
		return nil, fmt.Errorf(
			"settle merchant fee calculation: %w",
			err,
		)
	}

	return result, nil
}

// SettleMerchantFeeCalculationTxInternal is the transaction-aware form of
// SettleMerchantFeeCalculationInternal.
func (s *Service) SettleMerchantFeeCalculationTxInternal(
	ctx context.Context,
	tx pgx.Tx,
	id uuid.UUID,
) (*data.MerchantFeeCalculation, error) {
	if ctx == nil {
		return nil, ErrNilContext
	}

	if err := validateMerchantFeeCalculationTxService(s); err != nil {
		return nil, err
	}

	if err := validateMerchantFeeCalculationTx(tx); err != nil {
		return nil, err
	}

	result, err :=
		s.Models.
			MerchantFeeCalculation.
			SettleTx(
				ctx,
				tx,
				id,
			)
	if err != nil {
		return nil, fmt.Errorf(
			"settle merchant fee calculation in transaction: %w",
			err,
		)
	}

	return result, nil
}

// ReverseMerchantFeeCalculationInternal invalidates a non-reversed calculation
// while preserving its durable history.
//
// Reversal also releases the database-owned active-calculation uniqueness slot,
// permitting an explicit replacement calculation to be inserted.
func (s *Service) ReverseMerchantFeeCalculationInternal(
	ctx context.Context,
	id uuid.UUID,
) (*data.MerchantFeeCalculation, error) {
	dbCtx, cancel, err := s.merchantFeeCalculationContext(ctx)
	if err != nil {
		return nil, err
	}
	defer cancel()

	result, err :=
		s.Models.
			MerchantFeeCalculation.
			Reverse(
				dbCtx,
				id,
			)
	if err != nil {
		return nil, fmt.Errorf(
			"reverse merchant fee calculation: %w",
			err,
		)
	}

	return result, nil
}

// ReverseMerchantFeeCalculationTxInternal is the transaction-aware form of
// ReverseMerchantFeeCalculationInternal.
//
// An atomic recalculation workflow should reverse the current calculation and
// insert its replacement through the same caller-owned transaction.
func (s *Service) ReverseMerchantFeeCalculationTxInternal(
	ctx context.Context,
	tx pgx.Tx,
	id uuid.UUID,
) (*data.MerchantFeeCalculation, error) {
	if ctx == nil {
		return nil, ErrNilContext
	}

	if err := validateMerchantFeeCalculationTxService(s); err != nil {
		return nil, err
	}

	if err := validateMerchantFeeCalculationTx(tx); err != nil {
		return nil, err
	}

	result, err :=
		s.Models.
			MerchantFeeCalculation.
			ReverseTx(
				ctx,
				tx,
				id,
			)
	if err != nil {
		return nil, fmt.Errorf(
			"reverse merchant fee calculation in transaction: %w",
			err,
		)
	}

	return result, nil
}
