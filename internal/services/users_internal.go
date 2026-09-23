// Package services contains trusted internal service composition,
// automation support, moderation helpers, and shared internal workflow logic.
//
// focodebase/fobackend/internal/services/users_internal.go
//
// GTM:
//
//	Layer: 2.6 Internal Services / Identity Workflow
//	Release Class: SPINE
//	Reason:
//	  Owns transport-neutral Users-domain workflow composition for public
//	  signup, session issuance and rotation, password establishment, logout,
//	  and controlled account lifecycle operations.
//
//	  Canonical user persistence remains owned by data.UserModel. Canonical
//	  role/user_role_assignments persistence remains owned by data.RoleModel.
//	  Neither model calls the other. This service is the sole place where
//	  signup composes both atomically, in one caller-owned transaction, along
//	  with the transactional outbox event. Credential signing remains owned
//	  by the injected session-token issuer. Account activation remains owned
//	  by the activation workflow. This service composes those capabilities
//	  without exposing HTTP, provider, broker, notification, or
//	  deployment-topology concerns.
//
// SPINE Rule:
//
//	Keep compiling.
//	Keep production-ready.
//	Preserve data-driven signup-role eligibility.
//	Preserve inactive -> active account activation semantics for public
//	email/password signup.
//	Preserve transactional user-signup + primary-role-assignment + outbox
//	publication as one atomic transaction, composed here — never inside
//	UserModel or RoleModel calling one another.
//	Preserve provider-neutral session issuance behind SessionTokenIssuer.
//	Preserve atomic initial-password establishment.
//	Preserve refresh-token rotation and ownership validation.
//	Preserve refresh-token revocation rather than destructive logout deletion.
//	Preserve actor/target identity for account lifecycle operations.
//	Preserve atomic account-closure composition: the canonical user soft-delete
//	cascade and activation-token cleanup commit in one transaction, preserving
//	the same users-before-activation_tokens lock order used by activation
//	issuance and redemption.
//	Do not synchronously provision Profile, Wallet, or other independent
//	domains from signup.
//	Do not publish plaintext passwords, activation credentials, provider bearer
//	credentials, refresh tokens, access tokens, or protected hashes.
//	Do not embed Pub/Sub or other transport-specific semantics.
//	Do not invent Users-owned asynchronous workers.
//	Block deployment if this file breaks signup, activation eligibility,
//	session issuance/rotation, password establishment, logout, account closure,
//	expulsion, or transactional domain-event publication.
package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/PiccoloMondoC/focodebase/fobackend/internal/data"
	"github.com/PiccoloMondoC/focodebase/fobackend/internal/security"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

const (
	userAggregateType = "user"

	userSignedUpEventType    = "user.signed_up.v1"
	userSignedUpEventVersion = 1
)

// SignupUserInput contains the transport-neutral facts accepted by the
// canonical public email/password signup workflow.
//
// RequestedRole is deliberately a name rather than a role ID. Eligibility,
// canonical normalization, and role resolution remain owned by RoleModel.
type SignupUserInput struct {
	Email         string
	PlainPassword string
	RequestedRole string
}

// SignupUserResult describes the durable result of successful public signup.
//
// ActivationToken is plaintext bearer material produced for immediate,
// controlled delivery through the account-activation notification boundary.
// It must never be logged, audited, persisted in plaintext, or placed in a
// domain event.
type SignupUserResult struct {
	UserID          uuid.UUID
	ActivationToken string
}

// UserSession contains the bearer credentials returned by successful
// authentication or refresh-token rotation.
//
// These values may exist only at controlled service/output boundaries and must
// never be logged, audited, traced, or persisted outside their canonical
// credential stores.
type UserSession struct {
	UserID       uuid.UUID
	AccessToken  string
	RefreshToken string
}

// userSignedUpEventV1 is the producer-owned payload for user.signed_up.v1.
//
// The event deliberately excludes email, password/hash material, provider IDs,
// activation credentials, and session credentials. Consumers that need
// additional user state must obtain it through an appropriate domain contract.
type userSignedUpEventV1 struct {
	UserID      uuid.UUID `json:"user_id"`
	PrimaryRole string    `json:"primary_role"`
}

// SignupUserInternal creates an inactive public email/password account,
// resolves and assigns its primary signup role, and durably records
// user.signed_up.v1 — all in one atomic transaction — then commits, and only
// afterward initializes the separately owned account-activation workflow.
//
// Transaction order (one caller-owned transaction):
//
//	BEGIN
//	  RoleModel.ResolveSignupRoleTx        // validate/resolve eligible role
//	  UserModel.SignupPreparedTx           // insert users row only
//	  RoleModel.AssignPrimaryRoleTx        // assign primary role
//	  OutboxEvent.InsertTx                 // user.signed_up.v1
//	COMMIT
//
// UserModel and RoleModel never call one another; this service is the sole
// orchestrator of the cross-model signup invariant.
//
// Activation-token issuance is intentionally a second transaction owned by the
// activation capability: activation-token lifecycle is not part of canonical
// Users/Role persistence.
//
// A failure after the signup transaction commits is represented explicitly by
// a non-nil result containing UserID together with
// ErrUsersActivationInitialization. Callers must not report that the account
// itself failed to be created.
func (s *Service) SignupUserInternal(
	ctx context.Context,
	in SignupUserInput,
) (SignupUserResult, error) {
	var result SignupUserResult

	if err := s.validate(); err != nil {
		return result, err
	}
	if ctx == nil {
		return result, ErrNilContext
	}

	email := strings.TrimSpace(in.Email)
	requestedRole := strings.TrimSpace(in.RequestedRole)

	if email == "" || requestedRole == "" {
		return result, ErrUsersSignupInputInvalid
	}

	passwordHash, err := security.HashPassword(in.PlainPassword)
	if err != nil {
		return result, fmt.Errorf(
			"users signup: hash password: %w",
			err,
		)
	}

	ctx, cancel := context.WithTimeout(ctx, s.Cfg.DBTimeout)
	defer cancel()

	logger := s.Logger.
		GetLoggerWithContextFromContext(ctx).
		WithFunctionName("SignupUserInternal")

	tx, err := s.Models.DB.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return result, fmt.Errorf(
			"users signup: begin transaction: %w",
			err,
		)
	}

	committed := false
	defer func() {
		if committed {
			return
		}
		if rbErr := tx.Rollback(ctx); rbErr != nil &&
			!errors.Is(rbErr, pgx.ErrTxClosed) {
			logger.Warn(
				"users signup rollback failed",
				"error", rbErr,
			)
		}
	}()

	// Role eligibility is resolved before any users-table mutation so an
	// ineligible role selection fails cleanly with no side effects.
	roleID, err := s.Models.Role.ResolveSignupRoleTx(ctx, tx, requestedRole)
	if err != nil {
		return result, err
	}

	user := &data.User{
		Email:        email,
		PasswordHash: passwordHash,
		IsActive:     false,
	}

	userID, err := s.Models.User.SignupPreparedTx(ctx, tx, user)
	if err != nil {
		return result, fmt.Errorf(
			"users signup: persist account: %w",
			err,
		)
	}

	if err := s.Models.Role.AssignPrimaryRoleTx(
		ctx,
		tx,
		userID,
		roleID,
		nil,
	); err != nil {
		return result, fmt.Errorf(
			"users signup: assign primary role: %w",
			err,
		)
	}

	payload, err := json.Marshal(userSignedUpEventV1{
		UserID:      userID,
		PrimaryRole: requestedRole,
	})
	if err != nil {
		return result, fmt.Errorf(
			"users signup: marshal signed-up event: %w",
			err,
		)
	}

	_, err = s.Models.OutboxEvent.InsertTx(
		ctx,
		tx,
		data.NewOutboxEvent{
			AggregateType: userAggregateType,
			AggregateID:   userID,
			EventType:     userSignedUpEventType,
			EventVersion:  userSignedUpEventVersion,
			Payload:       payload,
			IdempotencyKey: fmt.Sprintf(
				"%s:%s",
				userSignedUpEventType,
				userID,
			),
		},
	)
	if err != nil {
		return result, fmt.Errorf(
			"users signup: persist signed-up event: %w",
			err,
		)
	}

	if err := tx.Commit(ctx); err != nil {
		return result, fmt.Errorf(
			"users signup: commit transaction: %w",
			err,
		)
	}
	committed = true

	result.UserID = userID

	// Activation owns this second transaction. Do not widen the signup
	// transaction around activation-token generation.
	token, err := s.IssueUserActivationTokenInternal(ctx, userID)
	if err != nil {
		logger.Warn(
			"user account created but activation initialization failed",
			"user_id", userID,
			"error", err,
		)

		return result, fmt.Errorf(
			"%w: %v",
			ErrUsersActivationInitialization,
			err,
		)
	}

	result.ActivationToken = token

	logger.Info(
		"user signup completed",
		"user_id", userID,
	)

	return result, nil
}

// LoginUserPasswordInternal authenticates an active, non-deleted user through
// the canonical password-authentication model and issues a fresh session.
//
// This method never logs or returns the supplied password except by passing the
// exact plaintext value to the canonical password-verification boundary.
func (s *Service) LoginUserPasswordInternal(
	ctx context.Context,
	email string,
	plainPassword string,
) (UserSession, error) {
	var result UserSession

	if err := s.validate(); err != nil {
		return result, err
	}
	if ctx == nil {
		return result, ErrNilContext
	}

	email = strings.TrimSpace(email)
	if email == "" || plainPassword == "" {
		return result, ErrUsersAuthenticationInputInvalid
	}

	ctx, cancel := context.WithTimeout(ctx, s.Cfg.DBTimeout)
	defer cancel()

	user, err := s.Models.User.Authenticate(
		ctx,
		email,
		plainPassword,
		"",
		"",
	)
	if err != nil {
		return result, err
	}

	if user == nil ||
		user.ID == uuid.Nil ||
		user.DeletedAt != nil ||
		!user.IsActive {
		return result, data.ErrInvalidCredentials
	}

	return s.issueUserSessionInternal(ctx, user.ID)
}

// IssueUserSessionInternal performs the common post-verification session
// boundary for externally verified identities.
//
// Provider proof verification remains outside this method. The caller supplies
// only the canonical Platform user ID obtained after successful provider
// verification and account resolution. This method re-reads authoritative
// lifecycle state before issuing credentials.
func (s *Service) IssueUserSessionInternal(
	ctx context.Context,
	userID uuid.UUID,
) (UserSession, error) {
	var result UserSession

	if err := s.validate(); err != nil {
		return result, err
	}
	if ctx == nil {
		return result, ErrNilContext
	}
	if userID == uuid.Nil {
		return result, ErrUsersActorRequired
	}

	ctx, cancel := context.WithTimeout(ctx, s.Cfg.DBTimeout)
	defer cancel()

	user, err := s.Models.User.GetByID(ctx, userID)
	if err != nil {
		if errors.Is(err, data.ErrUserNotFound) {
			return result, data.ErrInvalidCredentials
		}
		return result, fmt.Errorf(
			"issue user session: read account: %w",
			err,
		)
	}

	if user == nil ||
		user.DeletedAt != nil ||
		!user.IsActive {
		return result, data.ErrInvalidCredentials
	}

	return s.issueUserSessionInternal(ctx, userID)
}

func (s *Service) issueUserSessionInternal(
	ctx context.Context,
	userID uuid.UUID,
) (UserSession, error) {
	var result UserSession

	accessToken, refreshToken, err :=
		s.SessionTokens.GenerateTokensPair(ctx, userID)
	if err != nil {
		return result, fmt.Errorf(
			"issue user session: generate token pair: %w",
			err,
		)
	}

	result.UserID = userID
	result.AccessToken = accessToken
	result.RefreshToken = refreshToken

	return result, nil
}

// RefreshUserSessionInternal performs mandatory refresh-token rotation.
//
// The presented refresh token is atomically consumed exactly once, its owning
// account is rechecked, and only then is a replacement pair issued. Concurrent
// reuse of the same refresh credential cannot produce multiple replacement
// sessions. Failure after consumption is fail-safe: the caller must authenticate
// again rather than retaining a reusable old refresh credential.
func (s *Service) RefreshUserSessionInternal(
	ctx context.Context,
	plainRefreshToken string,
) (UserSession, error) {
	var result UserSession

	if err := s.validate(); err != nil {
		return result, err
	}
	if ctx == nil {
		return result, ErrNilContext
	}

	plainRefreshToken = strings.TrimSpace(plainRefreshToken)
	if plainRefreshToken == "" {
		return result, ErrUsersRefreshTokenRequired
	}

	ctx, cancel := context.WithTimeout(ctx, s.Cfg.DBTimeout)
	defer cancel()

	// Consume the presented refresh credential atomically before doing any
	// further work. Exactly one concurrent caller may transition an active,
	// unexpired token to revoked; every loser receives an invalid-session result.
	// This is the single-use security boundary for refresh rotation.
	dbToken, err := s.Models.Token.ConsumeRefreshToken(
		ctx,
		plainRefreshToken,
	)
	if err != nil {
		return result, ErrUsersSessionInvalid
	}

	// The consumed token identifies the account, but it does not establish
	// current account eligibility. Re-read canonical user state before issuing
	// replacement credentials. The old token remains consumed on every failure
	// from this point forward: rotation is deliberately fail-safe.
	user, err := s.Models.User.GetByID(ctx, dbToken.UserID)
	if err != nil {
		if errors.Is(err, data.ErrUserNotFound) {
			return result, ErrUsersSessionInvalid
		}

		return result, fmt.Errorf(
			"refresh user session: read user: %w",
			err,
		)
	}

	if user == nil ||
		user.DeletedAt != nil ||
		!user.IsActive {
		return result, ErrUsersSessionInvalid
	}

	return s.issueUserSessionInternal(ctx, dbToken.UserID)
}

// LogoutUserSessionInternal revokes the authenticated caller's presented
// refresh token.
//
// Logout uses canonical revocation rather than hard deletion so token lifecycle
// provenance remains available to the security subsystem.
func (s *Service) LogoutUserSessionInternal(
	ctx context.Context,
	actorUserID uuid.UUID,
	plainRefreshToken string,
) error {
	if err := s.validate(); err != nil {
		return err
	}
	if ctx == nil {
		return ErrNilContext
	}
	if actorUserID == uuid.Nil {
		return ErrUsersActorRequired
	}

	plainRefreshToken = strings.TrimSpace(plainRefreshToken)
	if plainRefreshToken == "" {
		return ErrUsersRefreshTokenRequired
	}

	ctx, cancel := context.WithTimeout(ctx, s.Cfg.DBTimeout)
	defer cancel()

	token, err := s.Models.Token.ValidateRefreshToken(
		ctx,
		plainRefreshToken,
	)
	if err != nil {
		return ErrUsersSessionInvalid
	}

	if token == nil || token.UserID != actorUserID {
		return ErrUsersSessionOwnershipMismatch
	}

	if err := s.Models.Token.RevokeRefreshToken(
		ctx,
		plainRefreshToken,
	); err != nil {
		return fmt.Errorf(
			"logout user session: revoke refresh token: %w",
			err,
		)
	}

	return nil
}

// EstablishInitialPasswordInternal establishes a password for an active account
// that currently has no password.
//
// The data-layer mutation itself is conditional on password_hash IS NULL. The
// service's later read is classification only; correctness does not depend on
// a read-then-write race.
func (s *Service) EstablishInitialPasswordInternal(
	ctx context.Context,
	actorUserID uuid.UUID,
	newPlainPassword string,
) error {
	if err := s.validate(); err != nil {
		return err
	}
	if ctx == nil {
		return ErrNilContext
	}
	if actorUserID == uuid.Nil {
		return ErrUsersActorRequired
	}

	hash, err := security.HashPassword(newPlainPassword)
	if err != nil {
		return fmt.Errorf(
			"establish initial password: hash password: %w",
			err,
		)
	}

	ctx, cancel := context.WithTimeout(ctx, s.Cfg.DBTimeout)
	defer cancel()

	updated, err := s.Models.User.SetInitialPasswordHash(
		ctx,
		actorUserID,
		hash,
	)
	if err != nil {
		return fmt.Errorf(
			"establish initial password: persist: %w",
			err,
		)
	}
	if updated {
		return nil
	}

	// Classification only. The atomic conditional UPDATE above owns the
	// no-overwrite invariant.
	user, err := s.Models.User.GetByID(ctx, actorUserID)
	if err != nil {
		if errors.Is(err, data.ErrUserNotFound) {
			return err
		}
		return fmt.Errorf(
			"establish initial password: classify miss: %w",
			err,
		)
	}

	if user == nil || user.DeletedAt != nil {
		return data.ErrUserNotFound
	}
	if !user.IsActive {
		return ErrUsersAccountNotEligible
	}
	if user.PasswordHash != "" {
		return ErrUsersPasswordAlreadyEstablished
	}

	return ErrUsersPasswordEstablishmentConflict
}

// closeAccountTx performs the atomic composition of account closure: the
// canonical user soft-delete cascade followed by activation-token cleanup,
// committed as a single transaction.
//
// The order is deliberate. Activation issuance locks users before touching
// activation_tokens. Redemption first resolves the token owner without taking a
// lock, then also locks users before activation_tokens. Closure therefore locks
// and mutates users first, then removes activation_tokens, preserving one
// cross-model lock order instead of reintroducing an inversion.
//
// Activation-token cleanup is data hygiene, not the security boundary: once the
// user cascade commits, canonical account state already prevents successful
// activation. Keeping the cleanup in the same transaction prevents stale
// credential material from surviving a completed closure.
func (s *Service) closeAccountTx(
	ctx context.Context,
	targetUserID uuid.UUID,
) error {
	logger := s.Logger.
		GetLoggerWithContextFromContext(ctx).
		WithFunctionName("closeAccountTx")

	tx, err := s.Models.DB.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf(
			"begin account closure transaction: %w",
			err,
		)
	}

	committed := false
	defer func() {
		if committed {
			return
		}

		rollbackCtx, rollbackCancel := context.WithTimeout(
			context.WithoutCancel(ctx),
			s.Cfg.DBTimeout,
		)
		defer rollbackCancel()

		if rollbackErr := tx.Rollback(rollbackCtx); rollbackErr != nil &&
			!errors.Is(rollbackErr, pgx.ErrTxClosed) {
			logger.Warn(
				"account closure rollback failed",
				"user_id", targetUserID,
				"error", rollbackErr,
			)
		}
	}()

	if err := s.Models.User.SoftDeleteWithCascadeTx(
		ctx,
		tx,
		targetUserID,
	); err != nil {
		return err
	}

	if err := s.Models.ActivationToken.DeleteByUserIDTx(
		ctx,
		tx,
		targetUserID,
	); err != nil {
		return fmt.Errorf(
			"remove pending activation token: %w",
			err,
		)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf(
			"commit account closure transaction: %w",
			err,
		)
	}

	committed = true

	logger.Info(
		"account closed",
		"user_id", targetUserID,
	)

	return nil
}

// CloseOwnAccountInternal performs the canonical self-service account closure
// workflow.
//
// The canonical user cascade and pending activation-token cleanup commit
// atomically through closeAccountTx. Session revocation follows as defense in
// depth; authorization is already rendered unusable by account/role lifecycle
// state even if revocation itself encounters a transient failure.
func (s *Service) CloseOwnAccountInternal(
	ctx context.Context,
	actorUserID uuid.UUID,
) error {
	if err := s.validate(); err != nil {
		return err
	}
	if ctx == nil {
		return ErrNilContext
	}
	if actorUserID == uuid.Nil {
		return ErrUsersActorRequired
	}

	ctx, cancel := context.WithTimeout(ctx, s.Cfg.DBTimeout)
	defer cancel()

	logger := s.Logger.
		GetLoggerWithContextFromContext(ctx).
		WithFunctionName("CloseOwnAccountInternal")

	if err := s.closeAccountTx(ctx, actorUserID); err != nil {
		return fmt.Errorf(
			"close own account: %w",
			err,
		)
	}

	if err := s.Models.Token.RevokeAllTokens(
		ctx,
		actorUserID,
	); err != nil {
		logger.Warn(
			"account closed but refresh-token revocation degraded",
			"user_id", actorUserID,
			"error", err,
		)
	}

	return nil
}

// ExpelUserInternal performs privileged account closure of targetUserID.
//
// Authorization remains owned by the trusted calling boundary. The service
// preserves actor and target as distinct semantic inputs but does not invent an
// operational prohibition against actorUserID == targetUserID.
//
// The canonical user cascade and pending activation-token cleanup for
// targetUserID commit atomically through closeAccountTx, mirroring
// CloseOwnAccountInternal.
func (s *Service) ExpelUserInternal(
	ctx context.Context,
	actorUserID uuid.UUID,
	targetUserID uuid.UUID,
) error {
	if err := s.validate(); err != nil {
		return err
	}
	if ctx == nil {
		return ErrNilContext
	}
	if actorUserID == uuid.Nil {
		return ErrUsersActorRequired
	}
	if targetUserID == uuid.Nil {
		return ErrUsersTargetRequired
	}

	ctx, cancel := context.WithTimeout(ctx, s.Cfg.DBTimeout)
	defer cancel()

	logger := s.Logger.
		GetLoggerWithContextFromContext(ctx).
		WithFunctionName("ExpelUserInternal")

	if err := s.closeAccountTx(ctx, targetUserID); err != nil {
		return fmt.Errorf(
			"expel user: %w",
			err,
		)
	}

	if err := s.Models.Token.RevokeAllTokens(
		ctx,
		targetUserID,
	); err != nil {
		logger.Warn(
			"user expelled but refresh-token revocation degraded",
			"actor_user_id", actorUserID,
			"target_user_id", targetUserID,
			"error", err,
		)
	}

	return nil
}
