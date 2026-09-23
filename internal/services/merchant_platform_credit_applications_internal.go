// Package services contains trusted internal Commerce Architecture
// orchestration.
//
// focodebase/fobackend/internal/services/merchant_platform_credit_applications_internal.go
//
// GTM:
//
//	Layer: 2.4.b Commerce Architecture / Monetization Layer
//	Release Class: SPINE
//	Reason:
//	  merchant_platform_credit_applications service orchestration is the
//	  canonical internal transaction boundary for applying platform-issued
//	  commercial credit against an approved merchant fee calculation.
//
//	  The service atomically composes fee-calculation serialization,
//	  aggregate-obligation protection, credit-account consumption, and
//	  immutable credit-application persistence.
//
// Domain Boundary:
//
//	A merchant platform credit application records that platform-issued
//	commercial credit reduced an approved merchant fee obligation before
//	payment collection.
//
//	This service does not decide:
//
//	  - which merchants receive platform credit;
//	  - how much credit is granted;
//	  - which fee types Administration makes credit-eligible;
//	  - which credit account should be selected;
//	  - the ordering in which multiple accounts should be consumed;
//	  - promotion policy;
//	  - fee pricing policy;
//	  - invoice policy;
//	  - actor authorization.
//
//	Those decisions must already have been resolved by governed configuration
//	and trusted upstream Commerce orchestration.
//
//	This service does enforce invariant monetary facts that may never be
//	configured away:
//
//	  - the fee calculation must represent an approved obligation;
//	  - the credit account and fee calculation must belong to the same merchant;
//	  - account, calculation, and application currencies must be identical;
//	  - the selected account must have sufficient currently usable credit;
//	  - aggregate applied credit must never exceed the calculated obligation;
//	  - account consumption and application history must commit atomically.
//
// Eligibility Boundary:
//
//	Fee-type eligibility and credit-account selection are upstream governed
//	commercial decisions. The caller must resolve those decisions before
//	invoking this service.
//
//	This service intentionally does not translate MerchantFeeCalculation.FeeTypeID
//	into another fee-type representation and does not hard-code eligible fee
//	types.
//
// Transaction Boundary:
//
//	ApplyMerchantPlatformCreditApplicationInternal owns one complete database
//	transaction.
//
//	It locks the authoritative merchant_fee_calculations row before reading
//	credit-application aggregates or consuming a credit account. That row is
//	the serialization point for every credit application against the same fee
//	calculation.
//
//	ApplyMerchantPlatformCreditApplicationTxInternal performs the same
//	orchestration inside a caller-owned transaction. It never begins, commits,
//	rolls back, or replaces that transaction.
//
//	Transaction-aware data methods inherit the caller's context unchanged.
//	The pool-backed entrypoint applies the single outer DBTimeout for the
//	complete operation.
//
// Monetary Boundary:
//
//	Monetary values remain exact decimal strings and PostgreSQL NUMERIC values.
//	This service performs comparisons using math/big.Rat only.
//
//	It never:
//
//	  - converts money through float32 or float64;
//	  - silently rounds;
//	  - performs currency conversion;
//	  - supplies a default currency;
//	  - mutates the immutable calculated fee amount.
//
// Concurrency Boundary:
//
//	The merchant_fee_calculations row is locked before the aggregate
//
// application total is read.
//
//	Every conforming application writer therefore serializes against the same
//
// fee calculation before calculating remaining creditable obligation.
//
//	The database UNIQUE (credit_account_id, fee_calculation_id) constraint
//
// remains the durable duplicate/idempotency authority for one account against
// one calculation.
//
// SPINE Rule:
//
//	Keep compiling.
//	Keep production-ready.
//	Preserve exact monetary handling.
//	Preserve deterministic fee-calculation-first lock ordering.
//	Preserve aggregate over-application protection.
//	Preserve same-merchant and same-currency integrity.
//	Preserve atomic credit consumption plus immutable application history.
//	Preserve database-owned duplicate enforcement.
//	Preserve upstream ownership of commercial eligibility and account selection.
//	Never apply credit to pending, settled, waived, or reversed calculations.
//	Never expose this workflow as generic administrative HTTP mutation.
//	Never introduce payment-provider work.
//	Never introduce asynchronous execution without a durable async owner.
//	Block deployment if this file breaks monetary integrity, transaction
//	atomicity, concurrency safety, lifecycle integrity, or billing readiness.
package services

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"regexp"
	"strings"

	"github.com/PiccoloMondoC/focodebase/fobackend/internal/data"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

const merchantPlatformCreditApplicationMaxDecimalLength = 128

var merchantPlatformCreditApplicationAmountPattern = regexp.MustCompile(
	`^(?:0|[1-9][0-9]{0,14})(?:\.[0-9]{1,4})?$`,
)

// ApplyMerchantPlatformCreditApplicationInput contains only the facts the
// caller legitimately owns for one credit application.
//
// Merchant ownership and currency are deliberately absent. They are derived
// from persisted authoritative records inside the transaction.
type ApplyMerchantPlatformCreditApplicationInput struct {
	CreditAccountID  uuid.UUID
	FeeCalculationID uuid.UUID
	AppliedAmount    string
}

func validateMerchantPlatformCreditApplicationService(
	s *Service,
) error {
	if err := s.validate(); err != nil {
		return err
	}

	if s.Models.MerchantPlatformCreditApplication.DB == nil {
		return fmt.Errorf(
			"%w: merchant platform credit application model database is nil",
			ErrInvalidServiceConfiguration,
		)
	}
	if s.Models.MerchantPlatformCreditApplication.Logger == nil {
		return fmt.Errorf(
			"%w: merchant platform credit application model logger is nil",
			ErrInvalidServiceConfiguration,
		)
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

func validateMerchantPlatformCreditApplicationTxService(
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

	if s.Models.MerchantPlatformCreditApplication.Logger == nil {
		return fmt.Errorf(
			"%w: merchant platform credit application model logger is nil",
			ErrInvalidServiceConfiguration,
		)
	}

	if s.Models.MerchantFeeCalculation.Logger == nil {
		return fmt.Errorf(
			"%w: merchant fee calculation model logger is nil",
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

func (s *Service) merchantPlatformCreditApplicationContext(
	ctx context.Context,
) (context.Context, context.CancelFunc, error) {
	if ctx == nil {
		return nil, nil, ErrNilContext
	}

	if err := validateMerchantPlatformCreditApplicationService(s); err != nil {
		return nil, nil, err
	}

	dbCtx, cancel := context.WithTimeout(ctx, s.Cfg.DBTimeout)
	return dbCtx, cancel, nil
}

func parseMerchantPlatformCreditApplicationDecimal(
	raw string,
) (string, *big.Rat, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return "", nil, ErrInvalidDecimal
	}

	if len(value) > merchantPlatformCreditApplicationMaxDecimalLength ||
		!canonicalDecimalPattern.MatchString(value) {
		return "", nil, ErrInvalidDecimal
	}

	rat, ok := new(big.Rat).SetString(value)
	if !ok {
		return "", nil, ErrInvalidDecimal
	}

	return value, rat, nil
}

func validateApplyMerchantPlatformCreditApplicationInput(
	input ApplyMerchantPlatformCreditApplicationInput,
) (string, *big.Rat, error) {
	if input.CreditAccountID == uuid.Nil {
		return "", nil, fmt.Errorf(
			"%w: credit_account_id is required",
			data.ErrMerchantPlatformCreditApplicationInvalidInput,
		)
	}

	if input.FeeCalculationID == uuid.Nil {
		return "", nil, fmt.Errorf(
			"%w: fee_calculation_id is required",
			data.ErrMerchantPlatformCreditApplicationInvalidInput,
		)
	}

	amount, amountRat, err :=
		parseMerchantPlatformCreditApplicationDecimal(
			input.AppliedAmount,
		)
	if err != nil ||
		amountRat.Sign() <= 0 ||
		!merchantPlatformCreditApplicationAmountPattern.MatchString(amount) {
		return "", nil, fmt.Errorf(
			"%w: applied_amount must be a positive decimal compatible with NUMERIC(19,4)",
			data.ErrMerchantPlatformCreditApplicationInvalidInput,
		)
	}

	return amount, amountRat, nil
}

func parsePersistedMerchantPlatformCreditApplicationDecimal(
	raw string,
	fieldName string,
) (*big.Rat, error) {
	_, rat, err := parseMerchantPlatformCreditApplicationDecimal(raw)
	if err != nil {
		return nil, fmt.Errorf(
			"%w: invalid persisted %s",
			data.ErrMerchantPlatformCreditApplicationInvalidState,
			fieldName,
		)
	}

	if rat.Sign() < 0 {
		return nil, fmt.Errorf(
			"%w: persisted %s cannot be negative",
			data.ErrMerchantPlatformCreditApplicationInvalidState,
			fieldName,
		)
	}

	return rat, nil
}

// ApplyMerchantPlatformCreditApplicationInternal atomically applies one amount
// from a caller-selected platform credit account against one approved fee
// calculation.
//
// Credit eligibility and account selection must already have been resolved by
// trusted upstream Commerce orchestration.
//
// The method owns the complete transaction and returns success only after the
// account decrement and immutable application row have both committed.
func (s *Service) ApplyMerchantPlatformCreditApplicationInternal(
	ctx context.Context,
	input ApplyMerchantPlatformCreditApplicationInput,
) (*data.MerchantPlatformCreditApplication, error) {
	dbCtx, cancel, err :=
		s.merchantPlatformCreditApplicationContext(ctx)
	if err != nil {
		return nil, err
	}
	defer cancel()

	logger := s.Logger.
		GetLoggerWithContextFromContext(dbCtx).
		WithFunctionName(
			"ApplyMerchantPlatformCreditApplicationInternal",
		)

	tx, err :=
		s.Models.MerchantPlatformCreditApplication.DB.Begin(dbCtx)
	if err != nil {
		return nil, fmt.Errorf(
			"begin merchant platform credit application transaction: %w",
			err,
		)
	}

	defer func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(
			context.Background(),
			s.Cfg.DBTimeout,
		)
		defer cleanupCancel()

		if rollbackErr := tx.Rollback(cleanupCtx); rollbackErr != nil &&
			!errors.Is(rollbackErr, pgx.ErrTxClosed) {
			logger.Error(
				"merchant platform credit application rollback failed",
				"error",
				rollbackErr,
			)
		}
	}()

	application, err :=
		s.applyMerchantPlatformCreditApplicationTx(
			dbCtx,
			tx,
			input,
		)
	if err != nil {
		return nil, err
	}

	if err := tx.Commit(dbCtx); err != nil {
		return nil, fmt.Errorf(
			"commit merchant platform credit application transaction: %w",
			err,
		)
	}

	logger.Info(
		"merchant platform credit application committed",
		"merchant_platform_credit_application_id",
		application.ID,
		"credit_account_id",
		application.CreditAccountID,
		"fee_calculation_id",
		application.FeeCalculationID,
	)

	return application, nil
}

// ApplyMerchantPlatformCreditApplicationTxInternal applies platform credit
// inside an existing caller-owned transaction.
//
// The caller owns transaction begin, commit, rollback, and timeout. This method
// inherits ctx unchanged and never begins, commits, rolls back, or replaces the
// supplied transaction.
func (s *Service) ApplyMerchantPlatformCreditApplicationTxInternal(
	ctx context.Context,
	tx pgx.Tx,
	input ApplyMerchantPlatformCreditApplicationInput,
) (*data.MerchantPlatformCreditApplication, error) {
	if ctx == nil {
		return nil, ErrNilContext
	}

	if err := validateMerchantPlatformCreditApplicationTxService(s); err != nil {
		return nil, err
	}

	if tx == nil {
		return nil, fmt.Errorf(
			"%w: transaction is required",
			data.ErrMerchantPlatformCreditApplicationInvalidInput,
		)
	}

	return s.applyMerchantPlatformCreditApplicationTx(
		ctx,
		tx,
		input,
	)
}

func (s *Service) applyMerchantPlatformCreditApplicationTx(
	ctx context.Context,
	tx pgx.Tx,
	input ApplyMerchantPlatformCreditApplicationInput,
) (*data.MerchantPlatformCreditApplication, error) {
	amount, amountRat, err :=
		validateApplyMerchantPlatformCreditApplicationInput(input)
	if err != nil {
		return nil, err
	}

	calculation, err :=
		s.Models.MerchantFeeCalculation.GetByIDForUpdateTx(
			ctx,
			tx,
			input.FeeCalculationID,
		)
	if err != nil {
		return nil, fmt.Errorf(
			"lock merchant fee calculation for platform credit application: %w",
			err,
		)
	}
	if calculation == nil {
		return nil, data.ErrMerchantPlatformCreditApplicationFeeCalculationNotFound
	}

	if data.NormalizeMerchantFeeCalculationStatus(calculation.Status) !=
		data.MerchantFeeCalculationStatusApproved {
		return nil, fmt.Errorf(
			"%w: fee calculation %s has status %q",
			ErrMerchantPlatformCreditApplicationFeeCalculationNotApproved,
			calculation.ID,
			calculation.Status,
		)
	}

	calculatedAmountRat, err :=
		parsePersistedMerchantPlatformCreditApplicationDecimal(
			calculation.CalculatedFeeAmount,
			"calculated_fee_amount",
		)
	if err != nil {
		return nil, err
	}

	appliedAmount, err :=
		s.Models.MerchantPlatformCreditApplication.
			SumAppliedAmountByFeeCalculationTx(
				ctx,
				tx,
				input.FeeCalculationID,
			)
	if err != nil {
		return nil, fmt.Errorf(
			"sum existing merchant platform credit applications: %w",
			err,
		)
	}

	appliedAmountRat, err :=
		parsePersistedMerchantPlatformCreditApplicationDecimal(
			appliedAmount,
			"aggregate applied_amount",
		)
	if err != nil {
		return nil, err
	}

	if appliedAmountRat.Cmp(calculatedAmountRat) > 0 {
		return nil, fmt.Errorf(
			"%w: persisted applications already exceed calculated obligation",
			data.ErrMerchantPlatformCreditApplicationInvalidState,
		)
	}

	newAggregate :=
		new(big.Rat).Add(
			new(big.Rat).Set(appliedAmountRat),
			amountRat,
		)

	if newAggregate.Cmp(calculatedAmountRat) > 0 {
		return nil, fmt.Errorf(
			"%w: fee calculation %s",
			ErrMerchantPlatformCreditApplicationExceedsObligation,
			calculation.ID,
		)
	}

	account, err :=
		s.Models.MerchantPlatformCreditAccount.ConsumeTx(
			ctx,
			tx,
			input.CreditAccountID,
			amount,
			calculation.Currency,
		)
	if err != nil {
		return nil, fmt.Errorf(
			"consume merchant platform credit account for application: %w",
			err,
		)
	}

	if account.MerchantID != calculation.MerchantID {
		return nil, fmt.Errorf(
			"%w: credit account %s merchant %s, fee calculation %s merchant %s",
			ErrMerchantPlatformCreditApplicationMerchantMismatch,
			account.ID,
			account.MerchantID,
			calculation.ID,
			calculation.MerchantID,
		)
	}

	application := &data.MerchantPlatformCreditApplication{
		CreditAccountID:  input.CreditAccountID,
		FeeCalculationID: input.FeeCalculationID,
		AppliedAmount:    amount,
		Currency:         calculation.Currency,
	}

	persisted, err :=
		s.Models.MerchantPlatformCreditApplication.InsertTx(
			ctx,
			tx,
			application,
		)
	if err != nil {
		return nil, fmt.Errorf(
			"insert merchant platform credit application: %w",
			err,
		)
	}

	return persisted, nil
}
