// Package services contains business orchestration for internal platform
// workflows.
//
// sdworkspace/sdbackend/internal/services/merchant_billing_accounts_internal.go
//
// GTM:
//
//	Layer: 2.4.b Commerce Architecture / Monetization Layer
//	Release Class: SPINE
//	Reason:
//	  merchant_billing_accounts service orchestration is the trusted internal
//	  boundary through which administrative handlers and approved cross-domain
//	  workflows use the canonical per-merchant billing relationship and
//	  billing-currency anchor.
//
// Domain Boundary:
//
//	This service exposes the complete merchant billing account operational
//	capability without deciding commercial or operational policy.
//
//	It does not determine:
//
//	  - whether a merchant commercially qualifies for a billing account;
//	  - which currencies Administration commercially enables;
//	  - why an account should be suspended, reactivated, or closed;
//	  - what the merchant owes;
//	  - whether a fee, plan, subscription, invoice, or payment is enabled;
//	  - whether another workflow may proceed because of account status;
//	  - which payment method or payment provider is used; or
//	  - whether payment must precede another platform capability.
//
//	Those decisions belong to authorization, Administration-governed
//	configuration, and the service orchestration that owns the relevant
//	commercial or operational workflow.
//
//	A consuming workflow may inspect billing-account state, but this service
//	does not assign automatic cross-domain gating meaning to that state.
//
// Monetary Boundary:
//
//	This service persists no amount, balance, minimum funding requirement, or
//	payable total. It performs no monetary arithmetic and contains no catalog
//	of commercially enabled currencies.
//
// Consumer Identity Sovereignty:
//
//	This domain contains no consumer identity or Future Offering engagement
//	data. It exposes no merchant-facing consumer disclosure or identity
//	transition capability.
//
// SPINE Rule:
//
//	Keep compiling.
//	Keep production-ready.
//	Preserve the data layer as authority for currency normalization,
//	persisted-state validation, lifecycle guards, idempotent target-state
//	success, terminal closure, concurrency control, and deterministic keyset
//	ordering.
//	Preserve transaction-aware seams for approved future atomic composition.
//	Never begin, commit, or roll back a caller-owned transaction.
//	Never silently fall back from a missing transaction to the connection pool.
//	Preserve errors.Is compatibility for wrapped data-layer errors.
//	Never duplicate data-layer validation or lifecycle logic.
//	Never perform preflight reads before guarded writes.
//	Never perform follow-up reads after successful lifecycle mutations.
//	Never implement authorization, commercial policy, operational policy,
//	invoice generation, payment execution, treasury behavior, or async work.
//	Block deployment if this file breaks lifecycle integrity, transaction
//	composition, concurrency safety, bounded execution, or error identity.
package services

import (
	"context"
	"fmt"
	"time"

	"github.com/PiccoloMondoC/focodebase/fobackend/internal/data"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// validateMerchantBillingAccountService verifies that the shared internal
// service container contains every dependency required for merchant billing
// account work.
func validateMerchantBillingAccountService(
	s *Service,
) error {
	if err := s.validate(); err != nil {
		return err
	}

	if s.Models.MerchantBillingAccount.DB == nil {
		return fmt.Errorf(
			"%w: merchant billing account model database is nil",
			ErrInvalidServiceConfiguration,
		)
	}

	if s.Models.MerchantBillingAccount.Logger == nil {
		return fmt.Errorf(
			"%w: merchant billing account model logger is nil",
			ErrInvalidServiceConfiguration,
		)
	}

	return nil
}

// merchantBillingAccountContext validates the service and derives a bounded
// child context for one merchant billing account operation.
//
// The derived context preserves parent cancellation and is bounded by the
// configured internal-service database timeout.
func (s *Service) merchantBillingAccountContext(
	ctx context.Context,
) (context.Context, context.CancelFunc, error) {
	if ctx == nil {
		return nil, nil, ErrNilContext
	}

	if err := validateMerchantBillingAccountService(s); err != nil {
		return nil, nil, err
	}

	dbCtx, cancel :=
		context.WithTimeout(
			ctx,
			s.Cfg.DBTimeout,
		)

	return dbCtx, cancel, nil
}

// CreateMerchantBillingAccountInternal creates the merchant's one canonical
// billing account.
//
// Authorization and any commercial qualification decision must already have
// been resolved by the caller. The data layer owns currency normalization,
// structural validation, initial active status, and uniqueness.
func (s *Service) CreateMerchantBillingAccountInternal(
	ctx context.Context,
	merchantID uuid.UUID,
	currency string,
) (*data.MerchantBillingAccount, error) {
	dbCtx, cancel, err :=
		s.merchantBillingAccountContext(ctx)
	if err != nil {
		return nil, err
	}
	defer cancel()

	account, err :=
		s.Models.MerchantBillingAccount.Insert(
			dbCtx,
			merchantID,
			currency,
		)
	if err != nil {
		return nil, fmt.Errorf(
			"create merchant billing account: %w",
			err,
		)
	}

	return account, nil
}

// GetMerchantBillingAccountByMerchantIDInternal retrieves the merchant's
// canonical billing account.
//
// Terminal accounts remain readable.
func (s *Service) GetMerchantBillingAccountByMerchantIDInternal(
	ctx context.Context,
	merchantID uuid.UUID,
) (*data.MerchantBillingAccount, error) {
	dbCtx, cancel, err :=
		s.merchantBillingAccountContext(ctx)
	if err != nil {
		return nil, err
	}
	defer cancel()

	account, err :=
		s.Models.MerchantBillingAccount.
			GetByMerchantID(
				dbCtx,
				merchantID,
			)
	if err != nil {
		return nil, fmt.Errorf(
			"get merchant billing account by merchant ID: %w",
			err,
		)
	}

	return account, nil
}

// ListMerchantBillingAccountsByStatusInternal returns a bounded,
// deterministically ordered page of accounts in status.
//
// Pagination remains the data layer's descending keyset contract using
// created_at and merchant_id. This service introduces no offset conversion
// or listing policy.
func (s *Service) ListMerchantBillingAccountsByStatusInternal(
	ctx context.Context,
	status data.MerchantBillingAccountStatus,
	limit int,
	beforeCreatedAt *time.Time,
	beforeMerchantID *uuid.UUID,
) ([]*data.MerchantBillingAccount, error) {
	dbCtx, cancel, err :=
		s.merchantBillingAccountContext(ctx)
	if err != nil {
		return nil, err
	}
	defer cancel()

	accounts, err :=
		s.Models.MerchantBillingAccount.
			ListByStatus(
				dbCtx,
				status,
				limit,
				beforeCreatedAt,
				beforeMerchantID,
			)
	if err != nil {
		return nil, fmt.Errorf(
			"list merchant billing accounts by status: %w",
			err,
		)
	}

	return accounts, nil
}

// SuspendMerchantBillingAccountInternal performs the guarded transition from
// active to suspended.
//
// An account already suspended is treated by the data layer as idempotent
// target-state success. This method does not determine why suspension should
// occur or what suspension means to another workflow.
func (s *Service) SuspendMerchantBillingAccountInternal(
	ctx context.Context,
	merchantID uuid.UUID,
) error {
	dbCtx, cancel, err :=
		s.merchantBillingAccountContext(ctx)
	if err != nil {
		return err
	}
	defer cancel()

	if err :=
		s.Models.MerchantBillingAccount.Suspend(
			dbCtx,
			merchantID,
		); err != nil {
		return fmt.Errorf(
			"suspend merchant billing account: %w",
			err,
		)
	}

	return nil
}

// ReactivateMerchantBillingAccountInternal performs the guarded transition
// from suspended to active.
//
// An account already active is treated by the data layer as idempotent
// target-state success. This method does not determine why reactivation
// should occur or what active status means to another workflow.
func (s *Service) ReactivateMerchantBillingAccountInternal(
	ctx context.Context,
	merchantID uuid.UUID,
) error {
	dbCtx, cancel, err :=
		s.merchantBillingAccountContext(ctx)
	if err != nil {
		return err
	}
	defer cancel()

	if err :=
		s.Models.MerchantBillingAccount.Reactivate(
			dbCtx,
			merchantID,
		); err != nil {
		return fmt.Errorf(
			"reactivate merchant billing account: %w",
			err,
		)
	}

	return nil
}

// CloseMerchantBillingAccountInternal performs the guarded transition from
// active or suspended to closed.
//
// Closed is terminal through this model. Closure does not erase existing
// obligations, invoices, payment history, credits, reconciliation records,
// or audit history.
func (s *Service) CloseMerchantBillingAccountInternal(
	ctx context.Context,
	merchantID uuid.UUID,
) error {
	dbCtx, cancel, err :=
		s.merchantBillingAccountContext(ctx)
	if err != nil {
		return err
	}
	defer cancel()

	if err :=
		s.Models.MerchantBillingAccount.Close(
			dbCtx,
			merchantID,
		); err != nil {
		return fmt.Errorf(
			"close merchant billing account: %w",
			err,
		)
	}

	return nil
}

// CreateMerchantBillingAccountTxInternal is the transaction-aware creation
// seam for approved atomic cross-domain composition.
//
// The caller owns tx and remains responsible for beginning, committing, or
// rolling it back. A missing transaction is rejected and never causes a
// fallback to the model's connection pool.
func (s *Service) CreateMerchantBillingAccountTxInternal(
	ctx context.Context,
	tx pgx.Tx,
	merchantID uuid.UUID,
	currency string,
) (*data.MerchantBillingAccount, error) {
	dbCtx, cancel, err :=
		s.merchantBillingAccountContext(ctx)
	if err != nil {
		return nil, err
	}
	defer cancel()

	if tx == nil {
		return nil, fmt.Errorf(
			"%w: transaction is required",
			data.ErrMerchantBillingAccountInvalidInput,
		)
	}

	account, err :=
		s.Models.MerchantBillingAccount.InsertTx(
			dbCtx,
			tx,
			merchantID,
			currency,
		)
	if err != nil {
		return nil, fmt.Errorf(
			"create merchant billing account in transaction: %w",
			err,
		)
	}

	return account, nil
}

// SuspendMerchantBillingAccountTxInternal is the transaction-aware suspension
// seam for approved atomic cross-domain composition.
//
// The caller owns tx. This method never begins, commits, or rolls back the
// transaction and never falls back to the connection pool.
func (s *Service) SuspendMerchantBillingAccountTxInternal(
	ctx context.Context,
	tx pgx.Tx,
	merchantID uuid.UUID,
) error {
	dbCtx, cancel, err :=
		s.merchantBillingAccountContext(ctx)
	if err != nil {
		return err
	}
	defer cancel()

	if tx == nil {
		return fmt.Errorf(
			"%w: transaction is required",
			data.ErrMerchantBillingAccountInvalidInput,
		)
	}

	if err :=
		s.Models.MerchantBillingAccount.SuspendTx(
			dbCtx,
			tx,
			merchantID,
		); err != nil {
		return fmt.Errorf(
			"suspend merchant billing account in transaction: %w",
			err,
		)
	}

	return nil
}

// ReactivateMerchantBillingAccountTxInternal is the transaction-aware
// reactivation seam for approved atomic cross-domain composition.
//
// The caller owns tx. This method never begins, commits, or rolls back the
// transaction and never falls back to the connection pool.
func (s *Service) ReactivateMerchantBillingAccountTxInternal(
	ctx context.Context,
	tx pgx.Tx,
	merchantID uuid.UUID,
) error {
	dbCtx, cancel, err :=
		s.merchantBillingAccountContext(ctx)
	if err != nil {
		return err
	}
	defer cancel()

	if tx == nil {
		return fmt.Errorf(
			"%w: transaction is required",
			data.ErrMerchantBillingAccountInvalidInput,
		)
	}

	if err :=
		s.Models.MerchantBillingAccount.ReactivateTx(
			dbCtx,
			tx,
			merchantID,
		); err != nil {
		return fmt.Errorf(
			"reactivate merchant billing account in transaction: %w",
			err,
		)
	}

	return nil
}

// CloseMerchantBillingAccountTxInternal is the transaction-aware closure seam
// for approved atomic cross-domain composition.
//
// The caller owns tx. Closed remains terminal; this method never exposes
// reopening and never falls back to the connection pool.
func (s *Service) CloseMerchantBillingAccountTxInternal(
	ctx context.Context,
	tx pgx.Tx,
	merchantID uuid.UUID,
) error {
	dbCtx, cancel, err :=
		s.merchantBillingAccountContext(ctx)
	if err != nil {
		return err
	}
	defer cancel()

	if tx == nil {
		return fmt.Errorf(
			"%w: transaction is required",
			data.ErrMerchantBillingAccountInvalidInput,
		)
	}

	if err :=
		s.Models.MerchantBillingAccount.CloseTx(
			dbCtx,
			tx,
			merchantID,
		); err != nil {
		return fmt.Errorf(
			"close merchant billing account in transaction: %w",
			err,
		)
	}

	return nil
}
