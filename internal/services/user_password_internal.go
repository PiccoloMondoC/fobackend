// Package services contains trusted internal service composition,
// automation support, moderation helpers, and shared internal workflow logic.
//
// focodebase/fobackend/internal/services/password_internal.go
//
// GTM:
//
//	Layer: 2.6 Internal Services / Identity Workflow
//	Release Class: SPINE
//	Reason:
//	  Owns transactional composition across the canonical PasswordResetModel
//	  and UserModel boundaries for password-reset issuance and redemption, and
//	  owns the plaintext-credential workflow for authenticated password
//	  change. PasswordResetModel remains the sole owner of password_resets.
//	  UserModel remains the sole owner of canonical users state, including
//	  password_hash. TokenModel remains the sole owner of refresh-credential
//	  revocation. This service owns BEGIN/COMMIT/ROLLBACK, plaintext
//	  hashing/verification via internal/security, workflow ordering, and the
//	  session consequences of password mutation.
//
//	  This file replaces the prior arrangement in which password-reset
//	  generation/consumption and password-change verification/hashing lived
//	  directly on UserModel.
//
// SPINE Rule:
//
//	Keep compiling.
//	Keep production-ready.
//	Preserve atomic password-reset-token issuance for eligible (active,
//	non-deleted) accounts only.
//	Preserve one canonical cross-model row-lock order for password reset,
//	mirroring activation: users before password_resets.
//	Preserve non-locking reset-token owner discovery solely as the mechanism
//	that lets redemption identify which canonical user row must be locked
//	first.
//	Preserve authoritative credential validation only after the canonical
//	user row has been locked.
//	Preserve existing-password verification under the same users row lock
//	that replaces the password.
//	Preserve refresh-session revocation after every committed password
//	mutation (reset and change).
//	Preserve strict model ownership: never issue direct users,
//	password_resets, or refresh-token SQL from this file.
//	Preserve plaintext password/reset-token material only at controlled
//	service input/output boundaries; never log, audit, trace, or publish it.
//	Preserve atomic, single-use reset-token consumption: successful reset
//	must not permit concurrent double redemption or partial completion.
//	Preserve database-owned persisted expiry and lifecycle timestamps.
//	Preserve reset-token lifetime as validated service configuration, never as
//	a hard-coded operational policy constant in this file.
//	Do not call notification providers from inside reset transactions.
//	Do not introduce process-local locks for database correctness.
//	Block deployment if this file breaks reset issuance, redemption,
//	transactional atomicity, lock-order safety, credential confidentiality,
//	password-change verification, session invalidation, or canonical model
//	ownership.
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

// RequestPasswordResetInternal creates or reissues a password-reset credential
// for a user who is currently active and eligible.
//
// Transaction order:
//
//	BEGIN
//	  UserModel.LockActiveUserTx
//	  PasswordResetModel.CreateResetTokenTx
//	COMMIT
//
// The canonical user row is locked first, so reset-token persistence is
// serialized behind canonical user lifecycle state, mirroring the
// users-before-activation_tokens order used by activation.
//
// Callers (handlers) are responsible for resolving userID from an email
// address in a non-enumerating way, exactly as ResendActivationLinkHandler
// already does for activation resend: this method itself makes no attempt to
// hide whether userID exists, because by the time it is called that decision
// has already been made by the caller's privacy boundary.
//
// If the user does not exist, is deleted, or is inactive, no reset-token
// mutation is attempted and ErrUserNotFound is returned; callers must not
// translate that into a response that discloses account state to an
// unauthenticated caller.
//
// A new issuance replaces the user's previous outstanding credential
// (PasswordResetModel enforces one row per user), so only the most recently
// delivered link can succeed.
func (s *Service) RequestPasswordResetInternal(
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

	opCtx, cancel := context.WithTimeout(ctx, s.Cfg.DBTimeout)
	defer cancel()

	logger := s.Logger.
		GetLoggerWithContextFromContext(opCtx).
		WithFunctionName("RequestPasswordResetInternal")

	tx, err := s.Models.DB.BeginTx(opCtx, pgx.TxOptions{})
	if err != nil {
		return "", fmt.Errorf("begin password-reset issuance transaction: %w", err)
	}

	committed := false
	defer func() {
		if committed {
			return
		}

		rollbackCtx, rollbackCancel := context.WithTimeout(
			context.WithoutCancel(opCtx),
			s.Cfg.DBTimeout,
		)
		defer rollbackCancel()

		if rollbackErr := tx.Rollback(rollbackCtx); rollbackErr != nil &&
			!errors.Is(rollbackErr, pgx.ErrTxClosed) {
			logger.Warn(
				"password-reset issuance rollback failed",
				"user_id", userID,
				"error", rollbackErr,
			)
		}
	}()

	if err := s.Models.User.LockActiveUserTx(opCtx, tx, userID); err != nil {
		return "", err
	}

	plainToken, err := s.Models.PasswordReset.CreateResetTokenTx(
		opCtx,
		tx,
		userID,
		s.Cfg.PasswordResetTokenTTL,
	)
	if err != nil {
		if data.IsForeignKeyViolation(err) {
			return "", errors.Join(
				data.ErrUserNotFound,
				fmt.Errorf("create reset token: %w", err),
			)
		}
		return "", fmt.Errorf("create reset token: %w", err)
	}

	if err := tx.Commit(opCtx); err != nil {
		return "", fmt.Errorf("commit password-reset issuance transaction: %w", err)
	}

	committed = true

	logger.Info("password reset token issuance committed", "user_id", userID)

	return plainToken, nil
}

// ResetPasswordInternal validates and redeems a password-reset credential,
// atomically hashing and persisting the new password and consuming the exact
// credential used, then revokes every refresh session for the account.
//
// Transaction order:
//
//	BEGIN
//	  PasswordResetModel.LookupResetTokenOwnerTx  // non-locking
//	  UserModel.LockActiveUserTx
//	  PasswordResetModel.LockResetTokenTx
//	  UserModel.SetPasswordHashTx
//	  PasswordResetModel.ConsumeResetTokenTx
//	COMMIT
//	TokenModel.RevokeAllTokens                   // session invalidation
//
// Redemption does not know the owning user in advance. It therefore performs a
// non-locking reset-token lookup solely to discover the candidate owner. That
// lookup is not credential validation and establishes no lifecycle fact.
//
// The owning canonical user is then the first row locked by this transaction.
// Only after that lock is held is the exact reset-token row locked and
// authoritatively validated, mirroring activation redemption's lock order.
//
// Expired and not-found tokens are deliberately reported identically
// (ErrInvalidResetToken) so a caller cannot use this endpoint to enumerate
// which reset links were ever issued. An expired token, once found, is still
// consumed so it cannot be raced against a concurrent legitimate request.
//
// Session consequence: a password reset is the recovery path for a
// possibly-compromised credential, so every refresh session for the account is
// revoked once the new password commits. Revocation runs after commit because
// refresh-credential ownership belongs to TokenModel; a revocation failure at
// that point is logged at error level as a degraded security outcome rather
// than reported as a failed reset, since the password change itself is
// already durable. This mirrors the established account-closure pattern.
//
// The new password is hashed via internal/security before any persistence
// call; UserModel never receives plaintext. The returned user ID identifies
// the account whose password was reset, for audit targeting only.
func (s *Service) ResetPasswordInternal(
	ctx context.Context,
	plainToken string,
	newPlainPassword string,
) (uuid.UUID, error) {
	if ctx == nil {
		return uuid.Nil, ErrNilContext
	}
	if err := s.validate(); err != nil {
		return uuid.Nil, err
	}

	plainToken = strings.TrimSpace(plainToken)
	if plainToken == "" {
		return uuid.Nil, data.ErrInvalidResetToken
	}

	newHash, err := security.HashPassword(newPlainPassword)
	if err != nil {
		return uuid.Nil, fmt.Errorf("reset password: hash new password: %w", err)
	}

	opCtx, cancel := context.WithTimeout(ctx, s.Cfg.DBTimeout)
	defer cancel()

	logger := s.Logger.
		GetLoggerWithContextFromContext(opCtx).
		WithFunctionName("ResetPasswordInternal")

	tx, err := s.Models.DB.BeginTx(opCtx, pgx.TxOptions{})
	if err != nil {
		return uuid.Nil, fmt.Errorf("begin password-reset redemption transaction: %w", err)
	}

	committed := false
	defer func() {
		if committed {
			return
		}

		rollbackCtx, rollbackCancel := context.WithTimeout(
			context.WithoutCancel(opCtx),
			s.Cfg.DBTimeout,
		)
		defer rollbackCancel()

		if rollbackErr := tx.Rollback(rollbackCtx); rollbackErr != nil &&
			!errors.Is(rollbackErr, pgx.ErrTxClosed) {
			logger.Warn(
				"password-reset redemption rollback failed",
				"error", rollbackErr,
			)
		}
	}()

	// Step 1: discover the candidate owner without acquiring a row lock.
	candidateUserID, err := s.Models.PasswordReset.LookupResetTokenOwnerTx(
		opCtx,
		tx,
		plainToken,
	)
	if err != nil {
		return uuid.Nil, err
	}

	// Step 2: users is the first row-lock domain in the transaction.
	if err := s.Models.User.LockActiveUserTx(opCtx, tx, candidateUserID); err != nil {
		if errors.Is(err, data.ErrUserNotFound) {
			return uuid.Nil, data.ErrInvalidResetToken
		}
		return uuid.Nil, err
	}

	// Step 3: lock and authoritatively validate the exact reset credential.
	tokenID, authoritativeUserID, isExpired, err := s.Models.PasswordReset.LockResetTokenTx(
		opCtx,
		tx,
		plainToken,
	)
	if err != nil {
		return uuid.Nil, err
	}

	if authoritativeUserID != candidateUserID {
		logger.Warn(
			"reset token owner mismatch after authoritative lock",
			"candidate_user_id", candidateUserID,
			"authoritative_user_id", authoritativeUserID,
		)
		return uuid.Nil, data.ErrInvalidResetToken
	}

	if isExpired {
		// Consume the expired credential so it cannot be raced against a
		// concurrent legitimate request, but report the same outward error as
		// any other invalid token.
		if consumeErr := s.Models.PasswordReset.ConsumeResetTokenTx(opCtx, tx, tokenID); consumeErr != nil {
			logger.Warn(
				"expired reset token cleanup failed",
				"token_id", tokenID,
				"error", consumeErr,
			)
		}

		if err := tx.Commit(opCtx); err != nil {
			return uuid.Nil, fmt.Errorf("commit expired reset-token cleanup: %w", err)
		}
		committed = true

		return uuid.Nil, data.ErrInvalidResetToken
	}

	// Step 4: persist the new password. UserModel never sees plaintext.
	if err := s.Models.User.SetPasswordHashTx(opCtx, tx, authoritativeUserID, newHash); err != nil {
		return uuid.Nil, fmt.Errorf("reset password: persist new hash: %w", err)
	}

	// Step 5: consume exactly the locked credential authorizing the reset.
	if err := s.Models.PasswordReset.ConsumeResetTokenTx(opCtx, tx, tokenID); err != nil {
		return uuid.Nil, fmt.Errorf("reset password: consume reset token: %w", err)
	}

	if err := tx.Commit(opCtx); err != nil {
		return uuid.Nil, fmt.Errorf("commit password-reset redemption transaction: %w", err)
	}

	committed = true

	logger.Info("password reset committed", "user_id", authoritativeUserID)

	s.revokeSessionsAfterPasswordMutation(opCtx, authoritativeUserID, "reset")

	return authoritativeUserID, nil
}

// ChangePasswordInternal establishes a new password for an authenticated user
// who supplies their current password, then revokes every refresh session for
// the account.
//
// This is the authenticated-known-password workflow, distinct from
// EstablishInitialPasswordInternal (active account with no password yet) and
// ResetPasswordInternal (forgotten-password, token-based).
//
// Authority comes from two independent facts: the caller's authenticated
// identity (actorUserID, supplied by the trusted boundary) and possession of
// the current password. An access token alone is not sufficient.
//
// Transaction order:
//
//	security.HashPassword                   // outside the lock: bcrypt is slow
//	BEGIN
//	  UserModel.LockActiveUserPasswordHashTx // lock + read current hash
//	  security.VerifyPassword               // verify under the lock
//	  UserModel.SetPasswordHashTx           // replace
//	COMMIT
//	TokenModel.RevokeAllTokens              // session invalidation
//
// The current hash is read and verified under the same row lock that replaces
// it. Two concurrent changes for one account therefore serialize: the second
// blocks on the lock, then verifies against the first's replacement hash and
// fails unless its caller knows the new password. An unlocked read-then-write
// would let both succeed with the last writer winning.
//
// Session consequence: every refresh session for the account is revoked,
// including the caller's own, so every device must sign in again with the new
// password. The HTTP boundary additionally revokes the presented access token;
// other outstanding access tokens expire within their configured TTL.
func (s *Service) ChangePasswordInternal(
	ctx context.Context,
	actorUserID uuid.UUID,
	currentPlainPassword string,
	newPlainPassword string,
	confirmNewPlainPassword string,
) error {
	if ctx == nil {
		return ErrNilContext
	}
	if err := s.validate(); err != nil {
		return err
	}
	if actorUserID == uuid.Nil {
		return ErrUsersActorRequired
	}
	if currentPlainPassword == "" || newPlainPassword == "" || confirmNewPlainPassword == "" {
		return ErrUsersAuthenticationInputInvalid
	}
	if newPlainPassword != confirmNewPlainPassword {
		return ErrUsersPasswordConfirmationMismatch
	}
	if newPlainPassword == currentPlainPassword {
		return ErrUsersPasswordUnchanged
	}

	newHash, err := security.HashPassword(newPlainPassword)
	if err != nil {
		return fmt.Errorf("change password: hash new password: %w", err)
	}

	opCtx, cancel := context.WithTimeout(ctx, s.Cfg.DBTimeout)
	defer cancel()

	logger := s.Logger.
		GetLoggerWithContextFromContext(opCtx).
		WithFunctionName("ChangePasswordInternal")

	tx, err := s.Models.DB.BeginTx(opCtx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin password-change transaction: %w", err)
	}

	committed := false
	defer func() {
		if committed {
			return
		}

		rollbackCtx, rollbackCancel := context.WithTimeout(
			context.WithoutCancel(opCtx),
			s.Cfg.DBTimeout,
		)
		defer rollbackCancel()

		if rollbackErr := tx.Rollback(rollbackCtx); rollbackErr != nil &&
			!errors.Is(rollbackErr, pgx.ErrTxClosed) {
			logger.Warn(
				"password-change rollback failed",
				"user_id", actorUserID,
				"error", rollbackErr,
			)
		}
	}()

	currentHash, err := s.Models.User.LockActiveUserPasswordHashTx(opCtx, tx, actorUserID)
	if err != nil {
		return err
	}

	if currentHash == "" {
		return ErrUsersPasswordNotEstablished
	}

	if err := security.VerifyPassword(currentPlainPassword, currentHash); err != nil {
		if errors.Is(err, security.ErrIncorrectPassword) {
			return ErrUsersCurrentPasswordIncorrect
		}
		// A malformed persisted hash is an integrity failure, not a wrong
		// password; surface it as an infrastructure error.
		return fmt.Errorf("change password: verify current password: %w", err)
	}

	if err := s.Models.User.SetPasswordHashTx(opCtx, tx, actorUserID, newHash); err != nil {
		return fmt.Errorf("change password: persist new hash: %w", err)
	}

	if err := tx.Commit(opCtx); err != nil {
		return fmt.Errorf("commit password-change transaction: %w", err)
	}

	committed = true

	logger.Info("password change committed", "user_id", actorUserID)

	s.revokeSessionsAfterPasswordMutation(opCtx, actorUserID, "change")

	return nil
}

// revokeSessionsAfterPasswordMutation revokes every refresh credential for
// userID after a committed password mutation.
//
// The mutation has already committed and cannot be undone here. Failure is
// logged at error level because it leaves earlier refresh sessions usable
// until their own expiry — a security degradation operators must see.
func (s *Service) revokeSessionsAfterPasswordMutation(
	ctx context.Context,
	userID uuid.UUID,
	mutation string,
) {
	if err := s.Models.Token.RevokeAllTokens(ctx, userID); err != nil {
		s.Logger.
			GetLoggerWithContextFromContext(ctx).
			WithFunctionName("revokeSessionsAfterPasswordMutation").
			Error(
				"password mutation committed but refresh-session revocation failed",
				"user_id", userID,
				"mutation", mutation,
				"error", err,
			)
	}
}
