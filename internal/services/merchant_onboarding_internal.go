// Package services contains application-level orchestration for trusted
// internal workflows.
//
// focodebase/fobackend/internal/services/merchant_onboarding_internal.go
//
// GTM:
//
//	Layer: 2.4 Merchant / Future Offering Domain
//	Release Class: SPINE
//	Reason:
//	  Owns the canonical self-service merchant onboarding transaction required
//	  before PCDF-M01 can operate.
//
// SPINE Rule:
//
//	Keep compiling.
//	Keep production-ready.
//	Keep public Users signup unchanged.
//	Create Merchant + principal-owned Merchant Account atomically.
//	Do not require merchant classification for M01 onboarding.
//	Do not accept identity facts that Merchant persistence does not own.
//	Do not provide an administrator-created Merchant Account path.
package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/PiccoloMondoC/focodebase/fobackend/internal/data"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

var (
	// ErrMerchantOnboardingActorRequired indicates that no authenticated actor
	// was supplied to onboarding.
	ErrMerchantOnboardingActorRequired = errors.New(
		"merchant onboarding: actor user ID is required",
	)

	// ErrMerchantOnboardingInputInvalid indicates caller-correctable Merchant
	// identity input. It may wrap data.ErrMerchantInvalid.
	ErrMerchantOnboardingInputInvalid = errors.New(
		"merchant onboarding: merchant identity input is invalid",
	)

	// ErrMerchantOnboardingActorIneligible indicates that the actor is not an
	// active user holding the public merchant role.
	ErrMerchantOnboardingActorIneligible = errors.New(
		"merchant onboarding: actor is not an active merchant user",
	)

	// ErrMerchantOnboardingAlreadyCompleted indicates that the actor is
	// already the principal of a Merchant Account. v1 permits exactly one.
	ErrMerchantOnboardingAlreadyCompleted = errors.New(
		"merchant onboarding: actor already owns a merchant account",
	)
)

const (
	merchantOnboardedEventType    = "merchant.onboarded.v1"
	merchantOnboardedEventVersion = 1
	merchantAggregateType         = "merchant"
)

// MerchantOnboardingInput contains only the identity facts that canonical
// Merchant persistence owns. Merchant classification, display names, and
// public routing handles are intentionally absent: none is an M01 onboarding
// invariant and Merchant persistence does not store them.
type MerchantOnboardingInput struct {
	Name    string
	LogoURL *string
	Website *string
}

// MerchantOnboardingResult is the established Merchant and its active,
// principal-owned Merchant Account.
type MerchantOnboardingResult struct {
	Merchant        *data.Merchant
	MerchantAccount *data.MerchantAccount
}

type merchantOnboardedEventV1 struct {
	MerchantID        uuid.UUID `json:"merchant_id"`
	MerchantAccountID uuid.UUID `json:"merchant_account_id"`
	PrincipalUserID   uuid.UUID `json:"principal_user_id"`
}

// OnboardMerchantInternal atomically creates the Merchant and its active,
// principal-owned Merchant Account, then persists the onboarding occurrence in
// the transactional outbox. Users signup and activation remain separate; this
// operation requires an already-activated, authenticated merchant user.
func (s *Service) OnboardMerchantInternal(
	ctx context.Context,
	actorUserID uuid.UUID,
	in MerchantOnboardingInput,
) (*MerchantOnboardingResult, error) {
	if err := s.validate(); err != nil {
		return nil, err
	}
	if ctx == nil {
		return nil, ErrNilContext
	}
	if actorUserID == uuid.Nil {
		return nil, ErrMerchantOnboardingActorRequired
	}
	if strings.TrimSpace(in.Name) == "" {
		return nil, ErrMerchantOnboardingInputInvalid
	}

	ctx, cancel := context.WithTimeout(ctx, s.Cfg.DBTimeout)
	defer cancel()

	actor, err := s.Models.User.GetByID(ctx, actorUserID)
	if err != nil {
		return nil, fmt.Errorf("merchant onboarding: resolve actor: %w", err)
	}
	if actor == nil || !actor.IsActive || !strings.EqualFold(actor.RoleName, "merchant") {
		return nil, ErrMerchantOnboardingActorIneligible
	}

	logger := s.Logger.
		GetLoggerWithContextFromContext(ctx).
		WithFunctionName("OnboardMerchantInternal")

	tx, err := s.Models.DB.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, fmt.Errorf("merchant onboarding: begin transaction: %w", err)
	}

	committed := false
	defer func() {
		if committed {
			return
		}
		if rbErr := tx.Rollback(ctx); rbErr != nil && !errors.Is(rbErr, pgx.ErrTxClosed) {
			logger.Warn("merchant onboarding rollback failed", "error", rbErr)
		}
	}()

	// InsertTx owns validation and canonicalization; the canonical values it
	// writes back are the values used for the remainder of the workflow.
	merchant := &data.Merchant{
		Name:    in.Name,
		LogoURL: in.LogoURL,
		Website: in.Website,
	}
	if err := s.Models.Merchant.InsertTx(ctx, tx, merchant); err != nil {
		if errors.Is(err, data.ErrMerchantInvalid) {
			return nil, fmt.Errorf("%w: %w", ErrMerchantOnboardingInputInvalid, err)
		}
		// data.ErrMerchantIdentityConflict remains inspectable via errors.Is.
		return nil, fmt.Errorf("merchant onboarding: create merchant: %w", err)
	}

	account, err := s.Models.MerchantAccount.CreateForPrincipalTx(
		ctx,
		tx,
		merchant.ID,
		actorUserID,
	)
	if err != nil {
		if errors.Is(err, data.ErrMerchantPrincipalAlreadyOnboarded) {
			return nil, ErrMerchantOnboardingAlreadyCompleted
		}
		return nil, fmt.Errorf("merchant onboarding: create merchant account: %w", err)
	}

	payload, err := json.Marshal(merchantOnboardedEventV1{
		MerchantID:        merchant.ID,
		MerchantAccountID: account.ID,
		PrincipalUserID:   actorUserID,
	})
	if err != nil {
		return nil, fmt.Errorf("merchant onboarding: marshal event: %w", err)
	}

	if _, err := s.Models.OutboxEvent.InsertTx(
		ctx,
		tx,
		data.NewOutboxEvent{
			AggregateType: merchantAggregateType,
			AggregateID:   merchant.ID,
			EventType:     merchantOnboardedEventType,
			EventVersion:  merchantOnboardedEventVersion,
			Payload:       payload,
			IdempotencyKey: fmt.Sprintf(
				"%s:%s",
				merchantOnboardedEventType,
				merchant.ID,
			),
		},
	); err != nil {
		return nil, fmt.Errorf("merchant onboarding: persist event: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("merchant onboarding: commit transaction: %w", err)
	}
	committed = true

	return &MerchantOnboardingResult{
		Merchant:        merchant,
		MerchantAccount: account,
	}, nil
}
