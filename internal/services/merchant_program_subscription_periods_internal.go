// Package services contains internal orchestration for immutable merchant
// program subscription-period facts.
//
// sdworkspace/sdbackend/internal/services/merchant_program_subscription_periods_internal.go
//
// GTM:
//
//	Layer: 2.4 Merchant / Future Offering Domain
//	Release Class: SPINE
//	Reason:
//	  merchant_program_subscription_periods service orchestration provides the
//	  trusted internal boundary for resolving immutable subscription-period
//	  facts and recording new periods through caller-owned transactions.
//
//	  Subscription periods preserve the plan, billing cadence, and bounded
//	  commercial interval applicable to a merchant program subscription. They
//	  are authoritative source facts for downstream monetization workflows,
//	  including subscription-period billable events.
//
//	  Canonical subscription lifecycle state remains owned by
//	  merchant_program_subscriptions. Canonical subscription lifecycle history
//	  remains owned by merchant_program_subscription_events.
//
//	  This service does not determine whether subscriptions are commercially
//	  enabled, whether a plan or cadence is offered, whether a period should be
//	  renewed, whether a gap is acceptable operationally, or whether any charge
//	  should be calculated, invoiced, collected, or settled.
//
// Domain Boundary:
//
//	This service owns:
//	  - resolving a period by canonical ID;
//	  - resolving the latest recorded period for a subscription;
//	  - resolving the period containing an explicitly supplied instant;
//	  - listing a bounded deterministic subscription-period timeline; and
//	  - transaction-compatible creation of one immutable period.
//
//	This service does not own:
//	  - subscription lifecycle transitions;
//	  - subscription lifecycle-event selection or recording;
//	  - implicit "current" period policy;
//	  - period-readiness policy;
//	  - renewal policy;
//	  - fee calculation;
//	  - billable-event creation;
//	  - invoicing;
//	  - payment collection;
//	  - authorization;
//	  - commercial enablement or eligibility policy; or
//	  - asynchronous scheduling or retry behavior.
//
// SPINE Rule:
//
//	Keep compiling.
//	Keep production-ready.
//	Preserve immutable period semantics.
//	Preserve explicit point-in-time resolution.
//	Preserve half-open [period_start, period_end) semantics.
//	Preserve deterministic period timeline ordering.
//	Preserve transaction-compatible period creation.
//	Preserve database-owned overlap and relational integrity enforcement.
//	Preserve capability independently of commercial enablement policy.
//	Never create an implicit current-period policy.
//	Never turn absence of a period into commercial or operational policy.
//	Never expose update, upsert, delete, restore, or purge behavior.
//	Never begin, commit, or roll back a caller-owned transaction.
//	Never require the database pool for caller-owned transaction execution.
//	Never introduce handler authorization, fee calculation, invoicing, payment,
//	or commercial-policy behavior.
//	Block deployment if this file breaks period resolution, immutable period
//	integrity, deterministic timeline retrieval, or transaction-compatible
//	period creation.
package services

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/PiccoloMondoC/sdworkspace/sdbackend/internal/data"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// CreateMerchantProgramSubscriptionPeriodInput contains the canonical workflow
// input required to record one immutable merchant program subscription period.
//
// The coordinating workflow must already have resolved the subscription
// lifecycle action, applicable plan and billing cadence, and any commercial or
// operational policy required to decide that a period should be created.
//
// This input carries capability data only. It does not encode commercial
// eligibility or renewal policy.
type CreateMerchantProgramSubscriptionPeriodInput struct {
	SubscriptionID uuid.UUID
	PlanID         uuid.UUID
	BillingPeriod  data.MerchantProgramSubscriptionBillingPeriod
	PeriodStart    time.Time
	PeriodEnd      time.Time
}

// validateMerchantProgramSubscriptionPeriodService validates dependencies
// required for pool-backed subscription-period reads.
func validateMerchantProgramSubscriptionPeriodService(
	s *Service,
) error {
	if err := s.validate(); err != nil {
		return err
	}

	if s.Models.MerchantProgramSubscriptionPeriod.DB == nil {
		return fmt.Errorf(
			"%w: merchant program subscription period model database is nil",
			ErrInvalidServiceConfiguration,
		)
	}

	if s.Models.MerchantProgramSubscriptionPeriod.Logger == nil {
		return fmt.Errorf(
			"%w: merchant program subscription period model logger is nil",
			ErrInvalidServiceConfiguration,
		)
	}

	return nil
}

// validateMerchantProgramSubscriptionPeriodTxService validates only the
// dependencies required for transaction-backed period creation.
//
// The caller owns transaction lifecycle and the surrounding workflow context.
// Therefore this validation deliberately does not require:
//
//   - Service.Cfg;
//   - Service.Cfg.DBTimeout; or
//   - MerchantProgramSubscriptionPeriod.DB.
//
// Persistence occurs exclusively through the supplied pgx.Tx. The period
// model's logger remains required because InsertTx performs model-level
// validation and observability.
func validateMerchantProgramSubscriptionPeriodTxService(
	s *Service,
) error {
	if s == nil {
		return fmt.Errorf(
			"%w: merchant program subscription period service is nil",
			ErrInvalidServiceConfiguration,
		)
	}

	if s.Logger == nil {
		return fmt.Errorf(
			"%w: merchant program subscription period service logger is nil",
			ErrInvalidServiceConfiguration,
		)
	}

	if s.Models == nil {
		return fmt.Errorf(
			"%w: merchant program subscription period service models are nil",
			ErrInvalidServiceConfiguration,
		)
	}

	if s.Models.MerchantProgramSubscriptionPeriod.Logger == nil {
		return fmt.Errorf(
			"%w: merchant program subscription period model logger is nil",
			ErrInvalidServiceConfiguration,
		)
	}

	return nil
}

// merchantProgramSubscriptionPeriodContext validates the service and derives a
// bounded context for one pool-backed period operation.
func (s *Service) merchantProgramSubscriptionPeriodContext(
	ctx context.Context,
) (context.Context, context.CancelFunc, error) {
	if ctx == nil {
		return nil, nil, ErrNilContext
	}

	if err := validateMerchantProgramSubscriptionPeriodService(s); err != nil {
		return nil, nil, err
	}

	dbCtx, cancel := context.WithTimeout(
		ctx,
		s.Cfg.DBTimeout,
	)

	return dbCtx, cancel, nil
}

// normalizeCreateMerchantProgramSubscriptionPeriodInput validates and
// canonicalizes workflow-owned period creation input.
//
// The data layer remains authoritative for persistence validation and database
// integrity, including foreign keys, duplicate starts, and non-overlap.
// Service validation here provides a stable workflow boundary and ensures
// canonical values are passed downstream.
func normalizeCreateMerchantProgramSubscriptionPeriodInput(
	input CreateMerchantProgramSubscriptionPeriodInput,
) (CreateMerchantProgramSubscriptionPeriodInput, error) {
	if input.SubscriptionID == uuid.Nil {
		return CreateMerchantProgramSubscriptionPeriodInput{},
			errors.New(
				"merchant program subscription period subscription ID is required",
			)
	}

	if input.PlanID == uuid.Nil {
		return CreateMerchantProgramSubscriptionPeriodInput{},
			errors.New(
				"merchant program subscription period plan ID is required",
			)
	}

	input.BillingPeriod =
		data.NormalizeMerchantProgramSubscriptionBillingPeriod(
			input.BillingPeriod,
		)

	if !data.IsValidMerchantProgramSubscriptionBillingPeriod(
		input.BillingPeriod,
	) {
		return CreateMerchantProgramSubscriptionPeriodInput{},
			fmt.Errorf(
				"invalid merchant program subscription period billing period: %s",
				input.BillingPeriod,
			)
	}

	if input.PeriodStart.IsZero() {
		return CreateMerchantProgramSubscriptionPeriodInput{},
			errors.New(
				"merchant program subscription period start is required",
			)
	}

	if input.PeriodEnd.IsZero() {
		return CreateMerchantProgramSubscriptionPeriodInput{},
			errors.New(
				"merchant program subscription period end is required",
			)
	}

	input.PeriodStart = input.PeriodStart.UTC()
	input.PeriodEnd = input.PeriodEnd.UTC()

	if !input.PeriodEnd.After(input.PeriodStart) {
		return CreateMerchantProgramSubscriptionPeriodInput{},
			errors.New(
				"merchant program subscription period end must be after start",
			)
	}

	return input, nil
}

// newMerchantProgramSubscriptionPeriod constructs the persistence model for one
// validated immutable subscription period.
//
// ID and CreatedAt remain persistence-owned.
func newMerchantProgramSubscriptionPeriod(
	input CreateMerchantProgramSubscriptionPeriodInput,
) *data.MerchantProgramSubscriptionPeriod {
	return &data.MerchantProgramSubscriptionPeriod{
		SubscriptionID: input.SubscriptionID,
		PlanID:         input.PlanID,
		BillingPeriod:  input.BillingPeriod,
		PeriodStart:    input.PeriodStart,
		PeriodEnd:      input.PeriodEnd,
	}
}

// CreateMerchantProgramSubscriptionPeriodTxInternal records one immutable
// subscription period through an existing transaction.
//
// The coordinating workflow owns the decision to create the period and must
// use the same transaction for every state change that must commit atomically
// with that period, which may include:
//
//   - canonical merchant_program_subscriptions mutation;
//   - merchant_program_subscription_events insertion; and
//   - downstream monetization source-fact creation.
//
// This method does not inspect subscription lifecycle state and does not decide
// whether the supplied plan, cadence, or interval is commercially applicable.
// Those decisions belong to the coordinating workflow and governed
// configuration.
//
// This method does not begin, commit, or roll back tx.
func (s *Service) CreateMerchantProgramSubscriptionPeriodTxInternal(
	ctx context.Context,
	tx pgx.Tx,
	input CreateMerchantProgramSubscriptionPeriodInput,
) (*data.MerchantProgramSubscriptionPeriod, error) {
	if ctx == nil {
		return nil, ErrNilContext
	}

	if err := validateMerchantProgramSubscriptionPeriodTxService(s); err != nil {
		return nil, err
	}

	if tx == nil {
		return nil, errors.New(
			"merchant program subscription period transaction is required",
		)
	}

	canonicalInput, err :=
		normalizeCreateMerchantProgramSubscriptionPeriodInput(
			input,
		)
	if err != nil {
		return nil, err
	}

	logger := s.Logger.
		GetLoggerWithContextFromContext(ctx).
		WithFunctionName(
			"CreateMerchantProgramSubscriptionPeriodTxInternal",
		)

	period := newMerchantProgramSubscriptionPeriod(
		canonicalInput,
	)

	if err :=
		s.Models.
			MerchantProgramSubscriptionPeriod.
			InsertTx(
				ctx,
				tx,
				period,
			); err != nil {
		logger.Error(
			"Create merchant program subscription period in transaction failed",
			"subscription_id",
			canonicalInput.SubscriptionID,
			"plan_id",
			canonicalInput.PlanID,
			"billing_period",
			canonicalInput.BillingPeriod,
			"error",
			err,
		)

		return nil, fmt.Errorf(
			"create merchant program subscription period for subscription %s in transaction: %w",
			canonicalInput.SubscriptionID,
			err,
		)
	}

	logger.Info(
		"Merchant program subscription period created in transaction",
		"period_id",
		period.ID,
		"subscription_id",
		period.SubscriptionID,
		"plan_id",
		period.PlanID,
		"billing_period",
		period.BillingPeriod,
	)

	return period, nil
}

// ResolveMerchantProgramSubscriptionPeriodByIDInternal resolves one immutable
// merchant program subscription period by canonical ID.
//
// A nil period with nil error means no matching period exists.
func (s *Service) ResolveMerchantProgramSubscriptionPeriodByIDInternal(
	ctx context.Context,
	periodID uuid.UUID,
) (*data.MerchantProgramSubscriptionPeriod, error) {
	dbCtx, cancel, err :=
		s.merchantProgramSubscriptionPeriodContext(
			ctx,
		)
	if err != nil {
		return nil, err
	}
	defer cancel()

	if periodID == uuid.Nil {
		return nil, errors.New(
			"merchant program subscription period ID is required",
		)
	}

	logger := s.Logger.
		GetLoggerWithContextFromContext(ctx).
		WithFunctionName(
			"ResolveMerchantProgramSubscriptionPeriodByIDInternal",
		)

	period, err :=
		s.Models.
			MerchantProgramSubscriptionPeriod.
			GetByID(
				dbCtx,
				periodID,
			)
	if err != nil {
		logger.Error(
			"Resolve merchant program subscription period by ID failed",
			"period_id",
			periodID,
			"error",
			err,
		)

		return nil, fmt.Errorf(
			"resolve merchant program subscription period %s: %w",
			periodID,
			err,
		)
	}

	return period, nil
}

// ResolveLatestMerchantProgramSubscriptionPeriodInternal resolves the period
// having the latest recorded PeriodStart for one subscription.
//
// "Latest" is a deterministic historical ordering concept. It does not mean
// that the returned period contains the current instant.
//
// A nil period with nil error means the subscription has no recorded periods.
func (s *Service) ResolveLatestMerchantProgramSubscriptionPeriodInternal(
	ctx context.Context,
	subscriptionID uuid.UUID,
) (*data.MerchantProgramSubscriptionPeriod, error) {
	dbCtx, cancel, err :=
		s.merchantProgramSubscriptionPeriodContext(
			ctx,
		)
	if err != nil {
		return nil, err
	}
	defer cancel()

	if subscriptionID == uuid.Nil {
		return nil, errors.New(
			"merchant program subscription period subscription ID is required",
		)
	}

	logger := s.Logger.
		GetLoggerWithContextFromContext(ctx).
		WithFunctionName(
			"ResolveLatestMerchantProgramSubscriptionPeriodInternal",
		)

	period, err :=
		s.Models.
			MerchantProgramSubscriptionPeriod.
			GetLatestBySubscriptionID(
				dbCtx,
				subscriptionID,
			)
	if err != nil {
		logger.Error(
			"Resolve latest merchant program subscription period failed",
			"subscription_id",
			subscriptionID,
			"error",
			err,
		)

		return nil, fmt.Errorf(
			"resolve latest merchant program subscription period for subscription %s: %w",
			subscriptionID,
			err,
		)
	}

	return period, nil
}

// ResolveMerchantProgramSubscriptionPeriodAtInstantInternal resolves the
// immutable period containing an explicitly supplied instant for one
// subscription.
//
// Containment uses the data layer's canonical half-open:
//
//	[period_start, period_end)
//
// semantics.
//
// The instant is required. This method never substitutes the current time.
// A nil period with nil error means no period contains the supplied instant.
func (s *Service) ResolveMerchantProgramSubscriptionPeriodAtInstantInternal(
	ctx context.Context,
	subscriptionID uuid.UUID,
	instant time.Time,
) (*data.MerchantProgramSubscriptionPeriod, error) {
	dbCtx, cancel, err :=
		s.merchantProgramSubscriptionPeriodContext(
			ctx,
		)
	if err != nil {
		return nil, err
	}
	defer cancel()

	if subscriptionID == uuid.Nil {
		return nil, errors.New(
			"merchant program subscription period subscription ID is required",
		)
	}

	if instant.IsZero() {
		return nil, errors.New(
			"merchant program subscription period lookup instant is required",
		)
	}

	instant = instant.UTC()

	logger := s.Logger.
		GetLoggerWithContextFromContext(ctx).
		WithFunctionName(
			"ResolveMerchantProgramSubscriptionPeriodAtInstantInternal",
		)

	period, err :=
		s.Models.
			MerchantProgramSubscriptionPeriod.
			GetContainingInstant(
				dbCtx,
				subscriptionID,
				instant,
			)
	if err != nil {
		logger.Error(
			"Resolve merchant program subscription period at instant failed",
			"subscription_id",
			subscriptionID,
			"instant",
			instant,
			"error",
			err,
		)

		return nil, fmt.Errorf(
			"resolve merchant program subscription period at instant for subscription %s: %w",
			subscriptionID,
			err,
		)
	}

	return period, nil
}

// ListMerchantProgramSubscriptionPeriodsBySubscriptionInternal retrieves a
// bounded, deterministic newest-first period timeline for one subscription.
//
// Ordering and pagination bounds remain owned by the canonical data model:
//
//	period_start DESC, id DESC
func (s *Service) ListMerchantProgramSubscriptionPeriodsBySubscriptionInternal(
	ctx context.Context,
	subscriptionID uuid.UUID,
	limit int,
	offset int,
) ([]*data.MerchantProgramSubscriptionPeriod, error) {
	dbCtx, cancel, err :=
		s.merchantProgramSubscriptionPeriodContext(
			ctx,
		)
	if err != nil {
		return nil, err
	}
	defer cancel()

	if subscriptionID == uuid.Nil {
		return nil, errors.New(
			"merchant program subscription period subscription ID is required",
		)
	}

	logger := s.Logger.
		GetLoggerWithContextFromContext(ctx).
		WithFunctionName(
			"ListMerchantProgramSubscriptionPeriodsBySubscriptionInternal",
		)

	periods, err :=
		s.Models.
			MerchantProgramSubscriptionPeriod.
			ListBySubscriptionID(
				dbCtx,
				subscriptionID,
				limit,
				offset,
			)
	if err != nil {
		logger.Error(
			"List merchant program subscription periods failed",
			"subscription_id",
			subscriptionID,
			"limit",
			limit,
			"offset",
			offset,
			"error",
			err,
		)

		return nil, fmt.Errorf(
			"list merchant program subscription periods for subscription %s: %w",
			subscriptionID,
			err,
		)
	}

	return periods, nil
}
