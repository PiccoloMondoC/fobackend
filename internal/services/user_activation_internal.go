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
//	  ActivationTokenModel and UserModel boundaries for activation-credential
//	  issuance (link token plus manual confirmation code), link-token
//	  redemption, and attempt-limited manual-code redemption. User-facing
//	  surfaces call this capability "email confirmation"; the internal domain
//	  keeps the activation name deliberately.
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
//	Preserve manual-code TTL, attempt limits, and resend cooldown as validated
//	service configuration.
//	Preserve commit-on-counted-failure for manual-code verification so failed
//	guesses are durable.
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
	"strings"

	"github.com/PiccoloMondoC/focodebase/fobackend/internal/data"
	"github.com/PiccoloMondoC/focodebase/fobackend/internal/security"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// ErrEmailConfirmationResendCooldown reports that a cooldown-enforcing
// issuance was requested while the account's pending confirmation was issued
// too recently. Public boundaries must absorb it silently.
var ErrEmailConfirmationResendCooldown = errors.New(
	"email confirmation was issued too recently",
)

// IssueEmailConfirmationOptions controls issuance behavior that differs by
// caller.
type IssueEmailConfirmationOptions struct {
	// EnforceResendCooldown marks this issuance as a public resend and
	// rejects it with ErrEmailConfirmationResendCooldown when the previous
	// public resend for the account was within the configured cooldown.
	// Signup issuance never counts toward the cooldown, so the first resend
	// after signup always proceeds. Public resend sets it; signup and trusted
	// internal callers do not.
	EnforceResendCooldown bool
}

// IssueUserActivationTokenInternal creates or reissues the activation
// credentials for an eligible user and returns only the link token.
//
// It is retained for callers that need only the bearer link credential. It
// issues a complete fresh credential pair exactly like
// IssueUserEmailConfirmationInternal, so it also supersedes any previously
// issued manual code.
func (s *Service) IssueUserActivationTokenInternal(
	ctx context.Context,
	userID uuid.UUID,
) (string, error) {
	credentials, err := s.IssueUserEmailConfirmationInternal(
		ctx,
		userID,
		IssueEmailConfirmationOptions{},
	)
	if err != nil {
		return "", err
	}

	return credentials.LinkToken, nil
}

// IssueUserEmailConfirmationInternal creates or reissues the activation
// credential pair — the high-entropy link token and the independent manual
// confirmation code — for a user who remains eligible for account activation.
//
// Transaction order:
//
//	BEGIN
//	  UserModel.LockActivationEligibleUserTx
//	  [ActivationTokenModel.ActivationResentWithinTx]   // cooldown only
//	  ActivationTokenModel.CreateActivationCredentialsTx
//	COMMIT
//
// The canonical user row is locked first. Credential persistence is therefore
// serialized behind canonical user lifecycle state and follows the same
// users-before-activation_tokens order used by redemption and account
// closure/expulsion.
//
// If the user does not exist, is deleted, or is already active, no
// activation-token mutation is attempted.
//
// The returned plaintext credentials exist only after successful commit and
// are intended solely for controlled delivery to the owning user.
func (s *Service) IssueUserEmailConfirmationInternal(
	ctx context.Context,
	userID uuid.UUID,
	opts IssueEmailConfirmationOptions,
) (data.ActivationCredentials, error) {
	var none data.ActivationCredentials

	if ctx == nil {
		return none, ErrNilContext
	}

	if err := s.validate(); err != nil {
		return none, err
	}

	if userID == uuid.Nil {
		return none, errors.New("user ID is required")
	}

	opCtx, cancel := context.WithTimeout(
		ctx,
		s.Cfg.DBTimeout,
	)
	defer cancel()

	logger := s.Logger.
		GetLoggerWithContextFromContext(opCtx).
		WithFunctionName(
			"IssueUserEmailConfirmationInternal",
		)

	tx, err := s.Models.DB.BeginTx(
		opCtx,
		pgx.TxOptions{},
	)
	if err != nil {
		return none, fmt.Errorf(
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
		return none, err
	}

	if opts.EnforceResendCooldown {
		recent, err := s.Models.
			ActivationToken.
			ActivationResentWithinTx(
				opCtx,
				tx,
				userID,
				s.Cfg.EffectiveActivationResendCooldown(),
			)
		if err != nil {
			return none, fmt.Errorf(
				"check activation resend cooldown: %w",
				err,
			)
		}

		if recent {
			logger.Info(
				"activation resend absorbed by cooldown",
				"user_id", userID,
			)
			return none, ErrEmailConfirmationResendCooldown
		}
	}

	// The canonical user row remains locked for the rest of this transaction.
	// Credential creation/reissue therefore cannot race account closure,
	// redemption, or another issuance workflow for this user outside the
	// users-before-activation_tokens ordering.
	credentials, err :=
		s.Models.
			ActivationToken.
			CreateActivationCredentialsTx(
				opCtx,
				tx,
				userID,
				s.Cfg.ActivationTokenTTL,
				s.Cfg.EffectiveActivationCodeTTL(),
				opts.EnforceResendCooldown,
			)
	if err != nil {
		// The locked canonical user should make an FK failure unreachable during
		// normal execution. Preserve defensive domain translation rather than
		// leaking an unexpected persistence detail through the service boundary.
		if data.IsForeignKeyViolation(err) {
			return none, errors.Join(
				data.ErrUserNotFound,
				fmt.Errorf(
					"create activation credentials: %w",
					err,
				),
			)
		}

		return none, fmt.Errorf(
			"create activation credentials: %w",
			err,
		)
	}

	if err := tx.Commit(opCtx); err != nil {
		return none, fmt.Errorf(
			"commit activation-token issuance transaction: %w",
			err,
		)
	}

	committed = true

	logger.Info(
		"activation credential issuance committed",
		"user_id", userID,
	)

	return credentials, nil
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

// NormalizeActivationCode canonicalizes a manually entered confirmation code.
// Spaces and hyphens are removed so "123 456" and "123-456" are accepted. It
// reports false when the result is not exactly security.ActivationCodeDigits
// ASCII digits.
func NormalizeActivationCode(raw string) (string, bool) {
	var b strings.Builder
	b.Grow(len(raw))

	for _, r := range raw {
		switch {
		case r == ' ' || r == '-' || r == '\t':
			continue
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		default:
			return "", false
		}
	}

	code := b.String()
	if len(code) != security.ActivationCodeDigits {
		return "", false
	}

	return code, true
}

// RedeemUserEmailConfirmationCodeInternal verifies a manually entered
// confirmation code for the pending account addressed by email and, on
// success, atomically activates the account and consumes its pending
// activation record (which also invalidates the emailed link token).
//
// Transaction order:
//
//	UserModel.GetByEmail                               // non-locking discovery
//	BEGIN
//	  UserModel.LockActivationEligibleUserTx
//	  ActivationTokenModel.VerifyActivationCodeTx
//	  UserModel.ActivateUserTx                        // success only
//	  ActivationTokenModel.ConsumeActivationTokenTx   // success only
//	COMMIT
//
// A wrong code is recorded by VerifyActivationCodeTx and this transaction is
// COMMITTED before the failure is returned, so attempt limits are durable.
//
// Errors carry precise internal classification (unknown account, already
// active, invalid, expired, attempts exceeded). Public boundaries must collapse
// all of them into one non-enumerating outcome.
func (s *Service) RedeemUserEmailConfirmationCodeInternal(
	ctx context.Context,
	email string,
	rawCode string,
) (uuid.UUID, error) {
	if ctx == nil {
		return uuid.Nil, ErrNilContext
	}

	if err := s.validate(); err != nil {
		return uuid.Nil, err
	}

	if strings.TrimSpace(email) == "" {
		return uuid.Nil, data.ErrUserNotFound
	}

	code, ok := NormalizeActivationCode(rawCode)
	if !ok {
		if strings.TrimSpace(rawCode) == "" {
			return uuid.Nil, data.ErrActivationCodeRequired
		}
		return uuid.Nil, data.ErrActivationCodeInvalid
	}

	opCtx, cancel := context.WithTimeout(
		ctx,
		s.Cfg.DBTimeout,
	)
	defer cancel()

	logger := s.Logger.
		GetLoggerWithContextFromContext(opCtx).
		WithFunctionName(
			"RedeemUserEmailConfirmationCodeInternal",
		)

	// Step 1: discover the candidate account without a lock. Eligibility is
	// re-established authoritatively under the users row lock below.
	user, err := s.Models.User.GetByEmail(opCtx, email)
	if err != nil {
		return uuid.Nil, err
	}
	if user == nil || user.ID == uuid.Nil {
		return uuid.Nil, data.ErrUserNotFound
	}

	userID := user.ID

	tx, err := s.Models.DB.BeginTx(
		opCtx,
		pgx.TxOptions{},
	)
	if err != nil {
		return uuid.Nil, fmt.Errorf(
			"begin activation-code redemption transaction: %w",
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
				"activation-code redemption rollback failed",
				"user_id", userID,
				"error", rollbackErr,
			)
		}
	}()

	// Step 2: users is the first row-lock domain in the transaction.
	if err := s.Models.User.LockActivationEligibleUserTx(
		opCtx,
		tx,
		userID,
	); err != nil {
		return uuid.Nil, err
	}

	// Step 3: lock the pending record and verify the code. Failures are
	// recorded inside this transaction.
	recordID, verifyErr := s.Models.
		ActivationToken.
		VerifyActivationCodeTx(
			opCtx,
			tx,
			userID,
			code,
			data.ActivationCodePolicy{
				MaxAttempts:      s.Cfg.EffectiveActivationCodeMaxAttempts(),
				MaxTotalFailures: s.Cfg.EffectiveActivationCodeMaxTotalFailures(),
			},
		)
	if verifyErr != nil {
		if errors.Is(verifyErr, data.ErrActivationCodeInvalid) ||
			errors.Is(verifyErr, data.ErrActivationCodeAttemptsExceeded) ||
			errors.Is(verifyErr, data.ErrActivationCodeExpired) {
			// Make the recorded failure durable before reporting it.
			if err := tx.Commit(opCtx); err != nil {
				return uuid.Nil, fmt.Errorf(
					"commit activation-code failure: %w",
					err,
				)
			}
			committed = true
		}

		return uuid.Nil, verifyErr
	}

	// Step 4: perform the canonical inactive -> active lifecycle transition.
	if err := s.Models.User.ActivateUserTx(
		opCtx,
		tx,
		userID,
	); err != nil {
		return uuid.Nil, fmt.Errorf(
			"activate user: %w",
			err,
		)
	}

	// Step 5: consume the pending record. This removes the code and the link
	// token together, so neither can be used again.
	if err := s.Models.
		ActivationToken.
		ConsumeActivationTokenTx(
			opCtx,
			tx,
			recordID,
		); err != nil {
		return uuid.Nil, fmt.Errorf(
			"consume activation record: %w",
			err,
		)
	}

	if err := tx.Commit(opCtx); err != nil {
		return uuid.Nil, fmt.Errorf(
			"commit activation-code redemption transaction: %w",
			err,
		)
	}

	committed = true

	logger.Info(
		"user activation by confirmation code committed",
		"user_id", userID,
	)

	return userID, nil
}
