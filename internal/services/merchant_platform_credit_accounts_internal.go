// Package services contains business orchestration for internal platform
// workflows.
//
// focodebase/fobackend/internal/services/merchant_platform_credit_accounts_internal.go
//
// GTM:
//
//	Layer: 2.4.b Commerce Architecture / Monetization Layer
//	Release Class: SPINE
//	Reason:
//	  merchant_platform_credit_accounts service orchestration is the trusted
//	  boundary through which administrative handlers, billing workflows, and
//	  future credit-application capabilities use platform-issued merchant
//	  commercial credit.
//
// Domain Boundary:
//
//	This service exposes the complete operational capability without deciding
//	commercial policy. It does not determine which merchants qualify for
//	credit, grant amounts, eligible fee types, promotion enablement, account
//	consumption order, or actor authorization. Those decisions belong to
//	Administration-governed configuration, eligibility, promotion, billing,
//	and handler authorization capabilities.
//
//	Credit consumption remains an internal orchestration capability and is not
//	exposed through the administrative HTTP handler.
//
// Monetary Boundary:
//
//	Amounts remain canonical decimal strings. This service never converts
//	monetary values through floating point and performs no application-side
//	monetary arithmetic.
//
// SPINE Rule:
//
//	Keep compiling.
//	Keep production-ready.
//	Preserve data-layer atomicity, currency isolation, database-time validity,
//	terminal lifecycle history, and exact decimal representation.
//	Preserve transaction-aware consumption for future billing composition.
//	Never implement commercial eligibility, grant, promotion, fee, or
//	consumption-order policy here.
//	Never treat platform credit as merchant-held funds.
//	Block deployment if this file breaks monetary integrity, concurrency
//	safety, lifecycle integrity, service-handler integration, or billing
//	composition readiness.
package services

import (
	"context"
	"fmt"

	"github.com/PiccoloMondoC/focodebase/fobackend/internal/data"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func validateMerchantPlatformCreditAccountService(s *Service) error {
	if err := s.validate(); err != nil {
		return err
	}
	if s.Models.MerchantPlatformCreditAccount.DB == nil {
		return fmt.Errorf(
			"%w: merchant platform credit account model database is nil",
			ErrInvalidServiceConfiguration,
		)
	}
	if s.Models.MerchantPlatformCreditAccount.Logger == nil {
		return fmt.Errorf(
			"%w: merchant platform credit account model logger is nil",
			ErrInvalidServiceConfiguration,
		)
	}
	return nil
}

func validateMerchantPlatformCreditAccountTxService(
	s *Service,
) error {
	if s == nil {
		return fmt.Errorf(
			"%w: service is nil",
			ErrInvalidServiceConfiguration,
		)
	}

	if s.Logger == nil {
		return fmt.Errorf(
			"%w: logger is nil",
			ErrInvalidServiceConfiguration,
		)
	}

	if s.Models == nil {
		return fmt.Errorf(
			"%w: models is nil",
			ErrInvalidServiceConfiguration,
		)
	}

	if s.Models.MerchantPlatformCreditAccount.Logger == nil {
		return fmt.Errorf(
			"%w: merchant platform credit account model logger is nil",
			ErrInvalidServiceConfiguration,
		)
	}

	return nil
}

func (s *Service) merchantPlatformCreditAccountContext(
	ctx context.Context,
) (context.Context, context.CancelFunc, error) {
	if ctx == nil {
		return nil, nil, ErrNilContext
	}
	if err := validateMerchantPlatformCreditAccountService(s); err != nil {
		return nil, nil, err
	}

	dbCtx, cancel := context.WithTimeout(ctx, s.Cfg.DBTimeout)
	return dbCtx, cancel, nil
}

// CreateMerchantPlatformCreditAccountInternal creates one platform-issued
// merchant commercial credit account.
//
// The caller must already have resolved authorization and commercial grant
// policy. This method preserves the data-layer rule that status begins active
// and remaining_amount begins equal to original_amount.
func (s *Service) CreateMerchantPlatformCreditAccountInternal(
	ctx context.Context,
	account *data.MerchantPlatformCreditAccount,
) error {
	dbCtx, cancel, err := s.merchantPlatformCreditAccountContext(ctx)
	if err != nil {
		return err
	}
	defer cancel()

	if account == nil {
		return fmt.Errorf(
			"%w: account is required",
			data.ErrMerchantPlatformCreditAccountInvalidInput,
		)
	}

	if err := s.Models.MerchantPlatformCreditAccount.Insert(dbCtx, account); err != nil {
		return fmt.Errorf("create merchant platform credit account: %w", err)
	}
	return nil
}

// GetMerchantPlatformCreditAccountByIDInternal retrieves one account by its
// canonical ID. Terminal accounts remain readable.
func (s *Service) GetMerchantPlatformCreditAccountByIDInternal(
	ctx context.Context,
	id uuid.UUID,
) (*data.MerchantPlatformCreditAccount, error) {
	dbCtx, cancel, err := s.merchantPlatformCreditAccountContext(ctx)
	if err != nil {
		return nil, err
	}
	defer cancel()

	account, err := s.Models.MerchantPlatformCreditAccount.GetByID(dbCtx, id)
	if err != nil {
		return nil, fmt.Errorf("get merchant platform credit account by ID: %w", err)
	}
	return account, nil
}

// GetMerchantPlatformCreditAccountByIDForMerchantInternal retrieves one
// account only when it belongs to merchantID.
func (s *Service) GetMerchantPlatformCreditAccountByIDForMerchantInternal(
	ctx context.Context,
	merchantID uuid.UUID,
	id uuid.UUID,
) (*data.MerchantPlatformCreditAccount, error) {
	dbCtx, cancel, err := s.merchantPlatformCreditAccountContext(ctx)
	if err != nil {
		return nil, err
	}
	defer cancel()

	account, err := s.Models.MerchantPlatformCreditAccount.GetByIDForMerchant(
		dbCtx,
		merchantID,
		id,
	)
	if err != nil {
		return nil, fmt.Errorf(
			"get merchant platform credit account by ID for merchant: %w",
			err,
		)
	}
	return account, nil
}

// ListMerchantPlatformCreditAccountsByMerchantInternal returns the bounded
// lifecycle history of merchant platform credit accounts for one merchant.
func (s *Service) ListMerchantPlatformCreditAccountsByMerchantInternal(
	ctx context.Context,
	merchantID uuid.UUID,
	limit int,
	offset int,
) ([]*data.MerchantPlatformCreditAccount, error) {
	dbCtx, cancel, err := s.merchantPlatformCreditAccountContext(ctx)
	if err != nil {
		return nil, err
	}
	defer cancel()

	accounts, err := s.Models.MerchantPlatformCreditAccount.ListByMerchant(
		dbCtx,
		merchantID,
		limit,
		offset,
	)
	if err != nil {
		return nil, fmt.Errorf(
			"list merchant platform credit accounts by merchant: %w",
			err,
		)
	}
	return accounts, nil
}

// ListCurrentlyUsableMerchantPlatformCreditAccountsInternal returns a bounded
// point-in-time view of accounts satisfying the data-layer usability guards.
//
// Result ordering is deterministic availability ordering only. This service
// does not establish commercial consumption-order policy.
func (s *Service) ListCurrentlyUsableMerchantPlatformCreditAccountsInternal(
	ctx context.Context,
	merchantID uuid.UUID,
	currency string,
	limit int,
) ([]*data.MerchantPlatformCreditAccount, error) {
	dbCtx, cancel, err := s.merchantPlatformCreditAccountContext(ctx)
	if err != nil {
		return nil, err
	}
	defer cancel()

	accounts, err := s.Models.MerchantPlatformCreditAccount.ListCurrentlyUsableByMerchant(
		dbCtx,
		merchantID,
		currency,
		limit,
	)
	if err != nil {
		return nil, fmt.Errorf(
			"list currently usable merchant platform credit accounts: %w",
			err,
		)
	}
	return accounts, nil
}

// UpdateMerchantPlatformCreditAccountDescriptiveFieldsInternal replaces the
// account's source_code and note without changing monetary or lifecycle state.
func (s *Service) UpdateMerchantPlatformCreditAccountDescriptiveFieldsInternal(
	ctx context.Context,
	id uuid.UUID,
	sourceCode *string,
	note *string,
) error {
	dbCtx, cancel, err := s.merchantPlatformCreditAccountContext(ctx)
	if err != nil {
		return err
	}
	defer cancel()

	if err := s.Models.MerchantPlatformCreditAccount.UpdateDescriptiveFields(
		dbCtx,
		id,
		sourceCode,
		note,
	); err != nil {
		return fmt.Errorf(
			"update merchant platform credit account descriptive fields: %w",
			err,
		)
	}
	return nil
}

// CancelMerchantPlatformCreditAccountInternal transitions an active account
// to cancelled while preserving terminal lifecycle history.
func (s *Service) CancelMerchantPlatformCreditAccountInternal(
	ctx context.Context,
	id uuid.UUID,
) error {
	dbCtx, cancel, err := s.merchantPlatformCreditAccountContext(ctx)
	if err != nil {
		return err
	}
	defer cancel()

	if err := s.Models.MerchantPlatformCreditAccount.Cancel(dbCtx, id); err != nil {
		return fmt.Errorf("cancel merchant platform credit account: %w", err)
	}
	return nil
}

// ExpireMerchantPlatformCreditAccountsBatchInternal transitions one bounded
// batch of eligible active accounts to expired using database time.
//
// This method performs no scheduling. Worker cadence, enablement, and batch
// sizing are operational configuration and must be supplied by startup or a
// governed scheduler rather than hard-coded into this domain service.
func (s *Service) ExpireMerchantPlatformCreditAccountsBatchInternal(
	ctx context.Context,
	limit int,
) ([]uuid.UUID, error) {
	dbCtx, cancel, err := s.merchantPlatformCreditAccountContext(ctx)
	if err != nil {
		return nil, err
	}
	defer cancel()

	expiredIDs, err := s.Models.MerchantPlatformCreditAccount.ExpireBatch(
		dbCtx,
		limit,
	)
	if err != nil {
		return nil, fmt.Errorf(
			"expire merchant platform credit account batch: %w",
			err,
		)
	}
	return expiredIDs, nil
}

// ConsumeMerchantPlatformCreditAccountInternal atomically consumes amount
// from one currently usable account.
//
// The caller must already have resolved credit eligibility, the account to
// consume, and durable business-operation idempotency. This method prevents
// overspending but does not deduplicate repeated business operations.
func (s *Service) ConsumeMerchantPlatformCreditAccountInternal(
	ctx context.Context,
	id uuid.UUID,
	amount string,
	currency string,
) (*data.MerchantPlatformCreditAccount, error) {
	dbCtx, cancel, err := s.merchantPlatformCreditAccountContext(ctx)
	if err != nil {
		return nil, err
	}
	defer cancel()

	account, err := s.Models.MerchantPlatformCreditAccount.Consume(
		dbCtx,
		id,
		amount,
		currency,
	)
	if err != nil {
		return nil, fmt.Errorf("consume merchant platform credit account: %w", err)
	}
	return account, nil
}

// ConsumeMerchantPlatformCreditAccountTxInternal is the transaction-aware
// consumption boundary for credit-application, fee-calculation,
// billing-ledger, and invoice orchestration.
//
// The supplied transaction remains owned by the caller. This method never
// begins, commits, rolls back, or replaces tx and applies no independent
// timeout. The complete caller-owned transaction retains one outer deadline.
func (s *Service) ConsumeMerchantPlatformCreditAccountTxInternal(
	ctx context.Context,
	tx pgx.Tx,
	id uuid.UUID,
	amount string,
	currency string,
) (*data.MerchantPlatformCreditAccount, error) {
	if ctx == nil {
		return nil, ErrNilContext
	}

	if err := validateMerchantPlatformCreditAccountTxService(s); err != nil {
		return nil, err
	}

	if tx == nil {
		return nil, fmt.Errorf(
			"%w: transaction is required",
			data.ErrMerchantPlatformCreditAccountInvalidInput,
		)
	}

	account, err :=
		s.Models.MerchantPlatformCreditAccount.ConsumeTx(
			ctx,
			tx,
			id,
			amount,
			currency,
		)
	if err != nil {
		return nil, fmt.Errorf(
			"consume merchant platform credit account in transaction: %w",
			err,
		)
	}

	return account, nil
}
