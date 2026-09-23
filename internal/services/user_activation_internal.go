// Package services contains trusted internal service composition,
// automation support, moderation helpers, and shared internal workflow logic.
//
// focodebase/fobackend/internal/services/user_activation_internal.go
//
// GTM:
//
//	Layer: 2.6 Internal Services / Automation Foundation
//	Release Class: SPINE
//	Reason:
//	  User activation is release-critical identity workflow infrastructure.
//	  This file owns transactional composition across the canonical
//	  ActivationTokenModel and UserModel boundaries for activation-token
//	  issuance and redemption.
//
//	  ActivationTokenModel remains the sole owner of activation_tokens.
//	  UserModel remains the sole owner of canonical users state.
//	  This service owns BEGIN/COMMIT/ROLLBACK and workflow ordering only.
//
// SPINE Rule:
//
//	Keep compiling.
//	Keep production-ready.
//	Preserve atomic activation-token issuance and account activation.
//	Preserve one canonical cross-model row-lock order across activation
//	issuance, activation redemption, and account closure/expulsion:
//	users before activation_tokens.
//	Preserve non-locking activation-token owner discovery solely as the
//	mechanism that lets redemption identify which canonical user row must
//	be locked first.
//	Preserve authoritative credential validation only after the canonical
//	user row has been locked.
//	Preserve strict model ownership: never issue direct users or
//	activation_tokens SQL from this file.
//	Preserve plaintext activation credentials only at controlled service
//	output/input boundaries; never log, audit, trace, persist, or publish them.
//	Preserve database-owned persisted expiry and lifecycle timestamps.
//	Preserve activation-token lifetime as validated service configuration,
//	never as hard-coded operational policy in this file.
//	Preserve exact-token consumption by activation-token ID.
//	Preserve caller cancellation and independently bounded rollback timing.
//	Do not publish speculative activation domain events.
//	Do not call notification providers from inside activation transactions.
//	Do not introduce process-local locks for database correctness.
//	Block deployment if this file breaks activation issuance, redemption,
//	transactional atomicity, lock-order safety, credential confidentiality,
//	or canonical model ownership.
package services

import (
	"context"
	"errors"
	"fmt"

	"github.com/PiccoloMondoC/focodebase/fobackend/internal/data"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// IssueUserActivationTokenInternal creates or reissues an activation credential
// for a user who remains eligible for account activation.
//
// Transaction order:
//
//	BEGIN
//	  UserModel.LockActivationEligibleUserTx
//	  ActivationTokenModel.CreateActivationTokenTx
//	COMMIT
//
// The canonical user row is locked first. Activation-token persistence is
// therefore serialized behind canonical user lifecycle state and follows the
// same users-before-activation_tokens order used by activation redemption and
// account closure/expulsion.
//
// If the user does not exist, is deleted, or is already active, no
// activation-token mutation is attempted.
//
// The returned plaintext credential exists only after successful commit and is
// intended solely for controlled delivery to the owning user.
func (s *Service) IssueUserActivationTokenInternal(
	ctx context.Context,
	userID uuid.UUID,
) (string, error) {
	if ctx == nil {
		return "", ErrNilContext
	}

	if err := s.validate(); err != nil {
		return "", err
	}

	if userID == uuid.Nil {
		return "", errors.New("user ID is required")
	}

	opCtx, cancel := context.WithTimeout(
		ctx,
		s.Cfg.DBTimeout,
	)
	defer cancel()

	logger := s.Logger.
		GetLoggerWithContextFromContext(opCtx).
		WithFunctionName(
			"IssueUserActivationTokenInternal",
		)

	tx, err := s.Models.DB.BeginTx(
		opCtx,
		pgx.TxOptions{},
	)
	if err != nil {
		return "", fmt.Errorf(
			"begin activation-token issuance transaction: %w",
			err,
		)
	}

	committed := false

	defer func() {
		if committed {
			return
		}

		rollbackCtx, rollbackCancel :=
			context.WithTimeout(
				context.WithoutCancel(opCtx),
				s.Cfg.DBTimeout,
			)
		defer rollbackCancel()

		if rollbackErr := tx.Rollback(rollbackCtx); rollbackErr != nil &&
			!errors.Is(rollbackErr, pgx.ErrTxClosed) {
			logger.Warn(
				"activation-token issuance rollback failed",
				"user_id", userID,
				"error", rollbackErr,
			)
		}
	}()

	// Canonical cross-model lock order begins with users.
	if err := s.Models.User.LockActivationEligibleUserTx(
		opCtx,
		tx,
		userID,
	); err != nil {
		return "", err
	}

	// The canonical user row remains locked for the rest of this transaction.
	// Activation-token creation/reissue therefore cannot race account closure,
	// activation redemption, or another issuance workflow for this user outside
	// the users-before-activation_tokens ordering.
	plainToken, err :=
		s.Models.
			ActivationToken.
			CreateActivationTokenTx(
				opCtx,
				tx,
				userID,
				s.Cfg.ActivationTokenTTL,
			)
	if err != nil {
		// The locked canonical user should make an FK failure unreachable during
		// normal execution. Preserve defensive domain translation rather than
		// leaking an unexpected persistence detail through the service boundary.
		if data.IsForeignKeyViolation(err) {
			return "", errors.Join(
				data.ErrUserNotFound,
				fmt.Errorf(
					"create activation token: %w",
					err,
				),
			)
		}

		return "", fmt.Errorf(
			"create activation token: %w",
			err,
		)
	}

	if err := tx.Commit(opCtx); err != nil {
		return "", fmt.Errorf(
			"commit activation-token issuance transaction: %w",
			err,
		)
	}

	committed = true

	logger.Info(
		"activation token issuance committed",
		"user_id", userID,
	)

	return plainToken, nil
}

// RedeemUserActivationTokenInternal validates and redeems an activation
// credential, atomically transitioning its owning user from inactive to active
// and consuming the exact credential used.
//
// Transaction order:
//
//	BEGIN
//	  ActivationTokenModel.LookupActivationTokenOwnerTx  // non-locking
//	  UserModel.LockActivationEligibleUserTx
//	  ActivationTokenModel.LockActivationTokenTx
//	  UserModel.ActivateUserTx
//	  ActivationTokenModel.ConsumeActivationTokenTx
//	COMMIT
//
// Redemption does not know the owning user in advance. It therefore performs a
// non-locking activation-token lookup solely to discover the candidate owner.
// That lookup is not credential validation and establishes no lifecycle fact.
//
// The owning canonical user is then the first row locked by this transaction.
// Only after that lock is held is the exact activation-token row locked and
// authoritatively validated. This preserves users-before-activation_tokens
// ordering across issuance, redemption, and account closure/expulsion.
//
// The plaintext activation credential is never logged, audited, traced,
// persisted, or published from this service.
func (s *Service) RedeemUserActivationTokenInternal(
	ctx context.Context,
	plainToken string,
) (uuid.UUID, error) {
	if ctx == nil {
		return uuid.Nil, ErrNilContext
	}

	if err := s.validate(); err != nil {
		return uuid.Nil, err
	}

	if plainToken == "" {
		return uuid.Nil,
			data.ErrActivationTokenRequired
	}

	opCtx, cancel := context.WithTimeout(
		ctx,
		s.Cfg.DBTimeout,
	)
	defer cancel()

	logger := s.Logger.
		GetLoggerWithContextFromContext(opCtx).
		WithFunctionName(
			"RedeemUserActivationTokenInternal",
		)

	tx, err := s.Models.DB.BeginTx(
		opCtx,
		pgx.TxOptions{},
	)
	if err != nil {
		return uuid.Nil, fmt.Errorf(
			"begin activation-token redemption transaction: %w",
			err,
		)
	}

	committed := false

	defer func() {
		if committed {
			return
		}

		rollbackCtx, rollbackCancel :=
			context.WithTimeout(
				context.WithoutCancel(opCtx),
				s.Cfg.DBTimeout,
			)
		defer rollbackCancel()

		if rollbackErr := tx.Rollback(rollbackCtx); rollbackErr != nil &&
			!errors.Is(rollbackErr, pgx.ErrTxClosed) {
			logger.Warn(
				"activation-token redemption rollback failed",
				"error", rollbackErr,
			)
		}
	}()

	// Step 1: discover the candidate owner without acquiring a row lock.
	//
	// This establishes only which canonical users row must be locked first.
	// Expiry, continued existence, exact token identity, and authoritative
	// ownership are deliberately re-established after that lock is held.
	candidateUserID, err :=
		s.Models.
			ActivationToken.
			LookupActivationTokenOwnerTx(
				opCtx,
				tx,
				plainToken,
			)
	if err != nil {
		return uuid.Nil, err
	}

	// Step 2: users is the first row-lock domain in the transaction.
	if err := s.Models.User.LockActivationEligibleUserTx(
		opCtx,
		tx,
		candidateUserID,
	); err != nil {
		return uuid.Nil, err
	}

	// Step 3: lock and authoritatively validate the exact activation credential.
	//
	// The token may have been reissued, consumed, expired, or otherwise ceased
	// to match between the initial non-locking read and this point. Only this
	// operation establishes credential validity.
	tokenID, authoritativeUserID, err :=
		s.Models.
			ActivationToken.
			LockActivationTokenTx(
				opCtx,
				tx,
				plainToken,
			)
	if err != nil {
		return uuid.Nil, err
	}

	// Never act on a canonical user row different from the one this transaction
	// locked. A mismatch represents an invalid credential/workflow state.
	if authoritativeUserID != candidateUserID {
		logger.Warn(
			"activation token owner mismatch after authoritative lock",
			"candidate_user_id", candidateUserID,
			"authoritative_user_id", authoritativeUserID,
		)

		return uuid.Nil,
			data.ErrActivationTokenInvalid
	}

	// Step 4: perform the canonical inactive -> active lifecycle transition.
	if err := s.Models.User.ActivateUserTx(
		opCtx,
		tx,
		authoritativeUserID,
	); err != nil {
		return uuid.Nil, fmt.Errorf(
			"activate user: %w",
			err,
		)
	}

	// Step 5: consume exactly the locked credential authorizing the transition.
	if err := s.Models.
		ActivationToken.
		ConsumeActivationTokenTx(
			opCtx,
			tx,
			tokenID,
		); err != nil {
		return uuid.Nil, fmt.Errorf(
			"consume activation token: %w",
			err,
		)
	}

	if err := tx.Commit(opCtx); err != nil {
		return uuid.Nil, fmt.Errorf(
			"commit activation-token redemption transaction: %w",
			err,
		)
	}

	committed = true

	logger.Info(
		"user activation committed",
		"user_id", authoritativeUserID,
	)

	return authoritativeUserID, nil
}
