// Package services contains business orchestration for internal platform
// workflows.
//
// focodebase/fobackend/internal/services/merchant_platform_credit_eligible_fee_types_internal.go
//
// GTM:
//
//	Layer: 2.4.b Commerce Architecture / Monetization Layer
//	Release Class: SPINE
//	Reason:
//	  merchant_platform_credit_eligible_fee_types service orchestration is the
//	  trusted boundary through which administrative handlers and future
//	  credit-application/billing orchestration read and configure which
//	  canonical fee types may consume a platform-issued merchant commercial
//	  credit account.
//
// Domain Boundary:
//
//	This service exposes the complete eligibility operational capability
//	without deciding commercial policy. It does not determine which fee
//	types should normally be eligible, does not infer eligibility from
//	merchant status, or fee enablement,
//	does not select which credit account to consume, and does not apply
//	credit to an invoice or billable event. Those decisions belong to
//	Administration-governed configuration and higher-layer credit-application
//	orchestration.
//
//	This service performs no SQL, no direct pool access, no handler
//	authorization, and no governance auditing. Those remain handler
//	responsibilities.
//
// SPINE Rule:
//
//	Keep compiling.
//	Keep production-ready.
//	Preserve the composite identity (credit_account_id, fee_type) owned by
//	the data layer; never invent a synthetic identifier here.
//	Preserve parent-account foreign-key integrity and canonical fee
//	vocabulary as enforced by the data layer; never duplicate that
//	validation or the underlying SQL here.
//	Preserve atomic, serialized eligibility-set replacement by calling the
//	data layer's ReplaceSet/ReplaceSetTx rather than composing delete-then-
//	insert locally.
//	Preserve transaction ownership: transaction-aware methods must not wrap
//	the caller-supplied context with an unrelated service-level timeout,
//	since the data layer's *Tx methods already apply their own bounded
//	timeout to the transaction-scoped context.
//	Preserve errors.Is compatibility for every exported data-layer sentinel
//	by wrapping with %w rather than replacing errors.
//	Never hard-code, infer, or default commercial eligibility policy.
//	Never expose DeleteAllForCreditAccount as an independent administrative
//	capability; an empty eligibility set must be expressed through
//	ReplaceSet/ReplaceSetTx.
//	Preserve transaction-aware eligibility checks for credit-application and
//	billing composition by delegating to the data layer's IsEligibleTx
//	surface; never fall back to a pool read inside a caller-owned transaction.
//	Block deployment if this file breaks build, composite-identity
//	integrity, transaction ownership, or service-handler separation.
package services

import (
	"context"
	"fmt"

	"github.com/PiccoloMondoC/focodebase/fobackend/internal/data"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// validateMerchantPlatformCreditEligibleFeeTypeService verifies that the
// service container and the merchant platform credit eligible fee type
// model's dependencies are usable before any database-bound work executes.
func validateMerchantPlatformCreditEligibleFeeTypeService(s *Service) error {
	if err := s.validate(); err != nil {
		return err
	}
	if s.Models.MerchantPlatformCreditEligibleFeeType.DB == nil {
		return fmt.Errorf(
			"%w: merchant platform credit eligible fee type model database is nil",
			ErrInvalidServiceConfiguration,
		)
	}
	if s.Models.MerchantPlatformCreditEligibleFeeType.Logger == nil {
		return fmt.Errorf(
			"%w: merchant platform credit eligible fee type model logger is nil",
			ErrInvalidServiceConfiguration,
		)
	}
	return nil
}

// merchantPlatformCreditEligibleFeeTypeContext validates the service and a
// caller-supplied context, then returns a pool-bound, service-timeout-scoped
// context for pool-based data-layer calls.
//
// This helper must be used only for pool-based methods. Transaction-aware
// methods must not wrap a caller-owned transaction context with this
// service-level timeout; see
// merchantPlatformCreditEligibleFeeTypeTxPreconditions.
func (s *Service) merchantPlatformCreditEligibleFeeTypeContext(
	ctx context.Context,
) (context.Context, context.CancelFunc, error) {
	if ctx == nil {
		return nil, nil, ErrNilContext
	}
	if err := validateMerchantPlatformCreditEligibleFeeTypeService(s); err != nil {
		return nil, nil, err
	}

	dbCtx, cancel := context.WithTimeout(ctx, s.Cfg.DBTimeout)
	return dbCtx, cancel, nil
}

// merchantPlatformCreditEligibleFeeTypeTxPreconditions validates the
// service, a caller-supplied context, and a caller-supplied transaction for
// transaction-aware methods.
//
// Unlike merchantPlatformCreditEligibleFeeTypeContext, this helper does not
// wrap ctx with a new timeout and does not return a cancel function. The
// caller retains ownership of the transaction's context and lifetime; the
// data layer's *Tx methods independently apply their own bounded timeout to
// the transaction-scoped context.
func (s *Service) merchantPlatformCreditEligibleFeeTypeTxPreconditions(
	ctx context.Context,
	tx pgx.Tx,
) error {
	if ctx == nil {
		return ErrNilContext
	}
	if err := validateMerchantPlatformCreditEligibleFeeTypeService(s); err != nil {
		return err
	}
	if tx == nil {
		return fmt.Errorf(
			"%w: transaction is required",
			data.ErrMerchantPlatformCreditEligibleFeeTypeInvalidInput,
		)
	}
	return nil
}

// CreateMerchantPlatformCreditEligibleFeeTypeInternal creates one
// eligibility association between a merchant platform credit account and a
// canonical fee type.
//
// The caller must already have resolved authorization and the decision that
// this association should exist. This method performs no commercial-policy
// selection; it records the association the caller has already decided on.
// A duplicate association returns data.ErrMerchantPlatformCreditEligibleFeeTypeAlreadyExists.
func (s *Service) CreateMerchantPlatformCreditEligibleFeeTypeInternal(
	ctx context.Context,
	creditAccountID uuid.UUID,
	feeType data.MerchantFeeType,
) (*data.MerchantPlatformCreditEligibleFeeType, error) {
	dbCtx, cancel, err := s.merchantPlatformCreditEligibleFeeTypeContext(ctx)
	if err != nil {
		return nil, err
	}
	defer cancel()

	association, err := s.Models.MerchantPlatformCreditEligibleFeeType.Insert(
		dbCtx,
		creditAccountID,
		feeType,
	)
	if err != nil {
		return nil, fmt.Errorf(
			"create merchant platform credit eligible fee type: %w",
			err,
		)
	}
	return association, nil
}

// GetMerchantPlatformCreditEligibleFeeTypeInternal retrieves one association
// by its composite key.
//
// Absence is a normal result and returns (nil, nil), matching the data
// layer's Get behavior. Callers that require a not-found error must check
// for a nil result themselves.
func (s *Service) GetMerchantPlatformCreditEligibleFeeTypeInternal(
	ctx context.Context,
	creditAccountID uuid.UUID,
	feeType data.MerchantFeeType,
) (*data.MerchantPlatformCreditEligibleFeeType, error) {
	dbCtx, cancel, err := s.merchantPlatformCreditEligibleFeeTypeContext(ctx)
	if err != nil {
		return nil, err
	}
	defer cancel()

	association, err := s.Models.MerchantPlatformCreditEligibleFeeType.Get(
		dbCtx,
		creditAccountID,
		feeType,
	)
	if err != nil {
		return nil, fmt.Errorf(
			"get merchant platform credit eligible fee type: %w",
			err,
		)
	}
	return association, nil
}

// ListMerchantPlatformCreditEligibleFeeTypesByCreditAccountInternal returns
// the complete eligibility set for one credit account, ordered by canonical
// fee type.
//
// No rows returns a non-nil empty slice, matching the data layer's
// ListByCreditAccount behavior.
func (s *Service) ListMerchantPlatformCreditEligibleFeeTypesByCreditAccountInternal(
	ctx context.Context,
	creditAccountID uuid.UUID,
) ([]*data.MerchantPlatformCreditEligibleFeeType, error) {
	dbCtx, cancel, err := s.merchantPlatformCreditEligibleFeeTypeContext(ctx)
	if err != nil {
		return nil, err
	}
	defer cancel()

	associations, err := s.Models.MerchantPlatformCreditEligibleFeeType.ListByCreditAccount(
		dbCtx,
		creditAccountID,
	)
	if err != nil {
		return nil, fmt.Errorf(
			"list merchant platform credit eligible fee types by credit account: %w",
			err,
		)
	}
	return associations, nil
}

// IsMerchantPlatformCreditEligibleFeeTypeInternal reports whether the
// composite-key association currently exists using the model's connection
// pool.
//
// false with a nil error is a valid negative result. Database failure remains
// distinguishable through the returned error.
//
// Use IsMerchantPlatformCreditEligibleFeeTypeTxInternal when eligibility must
// be checked as part of a caller-owned credit-application or billing
// transaction.
func (s *Service) IsMerchantPlatformCreditEligibleFeeTypeInternal(
	ctx context.Context,
	creditAccountID uuid.UUID,
	feeType data.MerchantFeeType,
) (bool, error) {
	dbCtx, cancel, err :=
		s.merchantPlatformCreditEligibleFeeTypeContext(ctx)
	if err != nil {
		return false, err
	}
	defer cancel()

	eligible, err :=
		s.Models.
			MerchantPlatformCreditEligibleFeeType.
			IsEligible(
				dbCtx,
				creditAccountID,
				feeType,
			)
	if err != nil {
		return false, fmt.Errorf(
			"check merchant platform credit eligible fee type: %w",
			err,
		)
	}

	return eligible, nil
}

// IsMerchantPlatformCreditEligibleFeeTypeTxInternal reports whether the
// composite-key association exists inside a caller-owned transaction.
//
// This method exists for credit-application and billing workflows that must
// evaluate eligibility and perform dependent monetary mutations against one
// transaction snapshot.
//
// The supplied transaction remains owned by the caller. This method never
// commits or rolls it back and does not apply a second service-level timeout;
// the data layer's IsEligibleTx bounds the individual database operation.
func (s *Service) IsMerchantPlatformCreditEligibleFeeTypeTxInternal(
	ctx context.Context,
	tx pgx.Tx,
	creditAccountID uuid.UUID,
	feeType data.MerchantFeeType,
) (bool, error) {
	if err :=
		s.merchantPlatformCreditEligibleFeeTypeTxPreconditions(
			ctx,
			tx,
		); err != nil {
		return false, err
	}

	eligible, err :=
		s.Models.
			MerchantPlatformCreditEligibleFeeType.
			IsEligibleTx(
				ctx,
				tx,
				creditAccountID,
				feeType,
			)
	if err != nil {
		return false, fmt.Errorf(
			"check merchant platform credit eligible fee type "+
				"in transaction: %w",
			err,
		)
	}

	return eligible, nil
}

// DeleteMerchantPlatformCreditEligibleFeeTypeInternal removes one
// eligibility association.
//
// Delete is not idempotent. Absence returns
// data.ErrMerchantPlatformCreditEligibleFeeTypeNotFound.
func (s *Service) DeleteMerchantPlatformCreditEligibleFeeTypeInternal(
	ctx context.Context,
	creditAccountID uuid.UUID,
	feeType data.MerchantFeeType,
) error {
	dbCtx, cancel, err := s.merchantPlatformCreditEligibleFeeTypeContext(ctx)
	if err != nil {
		return err
	}
	defer cancel()

	if err := s.Models.MerchantPlatformCreditEligibleFeeType.Delete(
		dbCtx,
		creditAccountID,
		feeType,
	); err != nil {
		return fmt.Errorf(
			"delete merchant platform credit eligible fee type: %w",
			err,
		)
	}
	return nil
}

// ReplaceMerchantPlatformCreditEligibleFeeTypeSetInternal atomically makes
// feeTypes the complete eligibility set for one credit account.
//
// An empty or nil feeTypes is valid and produces an empty eligibility set
// for an existing credit account. This is the canonical way to clear a
// credit account's eligibility set; there is no independent delete-all
// capability at the service boundary.
//
// The caller must already have resolved which fee types should be eligible.
// This method performs no commercial-policy selection.
func (s *Service) ReplaceMerchantPlatformCreditEligibleFeeTypeSetInternal(
	ctx context.Context,
	creditAccountID uuid.UUID,
	feeTypes []data.MerchantFeeType,
) ([]*data.MerchantPlatformCreditEligibleFeeType, error) {
	dbCtx, cancel, err := s.merchantPlatformCreditEligibleFeeTypeContext(ctx)
	if err != nil {
		return nil, err
	}
	defer cancel()

	replaced, err := s.Models.MerchantPlatformCreditEligibleFeeType.ReplaceSet(
		dbCtx,
		creditAccountID,
		feeTypes,
	)
	if err != nil {
		return nil, fmt.Errorf(
			"replace merchant platform credit eligible fee type set: %w",
			err,
		)
	}
	return replaced, nil
}

// CreateMerchantPlatformCreditEligibleFeeTypeTxInternal is the
// transaction-aware form of CreateMerchantPlatformCreditEligibleFeeTypeInternal.
//
// The supplied transaction remains owned by the caller; this method never
// commits or rolls it back. This method does not apply an additional
// service-level timeout on top of the caller-owned transaction context; the
// data layer's InsertTx independently bounds its own operation.
func (s *Service) CreateMerchantPlatformCreditEligibleFeeTypeTxInternal(
	ctx context.Context,
	tx pgx.Tx,
	creditAccountID uuid.UUID,
	feeType data.MerchantFeeType,
) (*data.MerchantPlatformCreditEligibleFeeType, error) {
	if err := s.merchantPlatformCreditEligibleFeeTypeTxPreconditions(ctx, tx); err != nil {
		return nil, err
	}

	association, err := s.Models.MerchantPlatformCreditEligibleFeeType.InsertTx(
		ctx,
		tx,
		creditAccountID,
		feeType,
	)
	if err != nil {
		return nil, fmt.Errorf(
			"create merchant platform credit eligible fee type in transaction: %w",
			err,
		)
	}
	return association, nil
}

// DeleteMerchantPlatformCreditEligibleFeeTypeTxInternal is the
// transaction-aware form of DeleteMerchantPlatformCreditEligibleFeeTypeInternal.
//
// The supplied transaction remains owned by the caller; this method never
// commits or rolls it back. This method does not apply an additional
// service-level timeout on top of the caller-owned transaction context; the
// data layer's DeleteTx independently bounds its own operation.
func (s *Service) DeleteMerchantPlatformCreditEligibleFeeTypeTxInternal(
	ctx context.Context,
	tx pgx.Tx,
	creditAccountID uuid.UUID,
	feeType data.MerchantFeeType,
) error {
	if err := s.merchantPlatformCreditEligibleFeeTypeTxPreconditions(ctx, tx); err != nil {
		return err
	}

	if err := s.Models.MerchantPlatformCreditEligibleFeeType.DeleteTx(
		ctx,
		tx,
		creditAccountID,
		feeType,
	); err != nil {
		return fmt.Errorf(
			"delete merchant platform credit eligible fee type in transaction: %w",
			err,
		)
	}
	return nil
}

// ReplaceMerchantPlatformCreditEligibleFeeTypeSetTxInternal is the
// transaction-aware form of
// ReplaceMerchantPlatformCreditEligibleFeeTypeSetInternal.
//
// The supplied transaction remains owned by the caller; this method never
// commits or rolls it back. It exists so a future credit-application or
// billing workflow can compose set replacement with other mutations inside
// one atomic unit of work. This method does not apply an additional
// service-level timeout on top of the caller-owned transaction context; the
// data layer's ReplaceSetTx independently bounds its own operation and locks
// the parent credit-account row to serialize concurrent replacements.
func (s *Service) ReplaceMerchantPlatformCreditEligibleFeeTypeSetTxInternal(
	ctx context.Context,
	tx pgx.Tx,
	creditAccountID uuid.UUID,
	feeTypes []data.MerchantFeeType,
) ([]*data.MerchantPlatformCreditEligibleFeeType, error) {
	if err := s.merchantPlatformCreditEligibleFeeTypeTxPreconditions(ctx, tx); err != nil {
		return nil, err
	}

	replaced, err := s.Models.MerchantPlatformCreditEligibleFeeType.ReplaceSetTx(
		ctx,
		tx,
		creditAccountID,
		feeTypes,
	)
	if err != nil {
		return nil, fmt.Errorf(
			"replace merchant platform credit eligible fee type set in transaction: %w",
			err,
		)
	}
	return replaced, nil
}
