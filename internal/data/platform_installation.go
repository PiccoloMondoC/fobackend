// Package data provides models and database access methods for the
// persistent installation record and its one-time Root bootstrap claim.
//
// focodebase/fobackend/internal/data/platform_installation.go
//
// GTM:
//
//	Layer: 2.1 Database / Governance Foundation
//	Release Class: SPINE
//	Reason:
//	  Installation state is persistent database state, never container
//	  lifecycle. platform_installation exists exactly when the incumbent Root
//	  exists. platform_bootstrap_claims holds the one-time entitlement the
//	  installer issues; only the SHA-256 of its credential is stored, and the
//	  claimant address is erased when the claim is resolved.
//
// SPINE Rule:
//
//	Keep compiling.
//	Keep production-ready.
//	Never store a bootstrap credential, only its hash.
//	Never return or log the claimant address outside the completion
//	transaction that consumes it.
//	Block deployment if this file breaks build or installation integrity.
package data

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"time"

	"github.com/PiccoloMondoC/focodebase/fobackend/internal/logging"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// InstallationCeremonyVersion identifies the bootstrap ceremony that wrote an
// installation record.
const InstallationCeremonyVersion = 1

// BootstrapCredentialLength is the encoded length of a bootstrap credential:
// 32 bytes from crypto/rand, unpadded base64url.
const BootstrapCredentialLength = 43

// GenerateBootstrapCredential returns a new high-entropy one-time credential.
// The plaintext exists only at the delivery boundary; persist only
// BootstrapCredentialHash of it.
func GenerateBootstrapCredential() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate bootstrap credential: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// BootstrapCredentialHash returns the SHA-256 of a credential. Issuance and
// completion both use this single definition.
func BootstrapCredentialHash(credential string) []byte {
	sum := sha256.Sum256([]byte(credential))
	return sum[:]
}

// WellFormedBootstrapCredential reports whether credential has the issued
// shape. It is a cheap pre-filter, never a validity decision.
func WellFormedBootstrapCredential(credential string) bool {
	if len(credential) != BootstrapCredentialLength {
		return false
	}
	for _, r := range credential {
		switch {
		case r >= 'A' && r <= 'Z', r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-', r == '_':
		default:
			return false
		}
	}
	return true
}

// PlatformInstallation is the installation record. It identifies the
// original bootstrap account by users.id only.
type PlatformInstallation struct {
	InstallationID          uuid.UUID `json:"installation_id"`
	OriginalBootstrapUserID uuid.UUID `json:"original_bootstrap_user_id"`
	CeremonyVersion         int       `json:"ceremony_version"`
	CompletedAt             time.Time `json:"completed_at"`
}

// BootstrapClaimSummary describes the pending claim without its address or
// credential.
type BootstrapClaimSummary struct {
	ClaimID   uuid.UUID `json:"claim_id"`
	IssuedAt  time.Time `json:"issued_at"`
	ExpiresAt time.Time `json:"expires_at"`
}

// LockedBootstrapClaim is a pending claim locked inside the completion
// transaction. ClaimantEmail exists only for that transaction.
type LockedBootstrapClaim struct {
	ClaimID       uuid.UUID
	ClaimantEmail string
	IssuedAt      time.Time
	ExpiresAt     time.Time
}

// PlatformInstallationModel owns installation persistence.
type PlatformInstallationModel struct {
	DB     *pgxpool.Pool
	Logger *logging.Logger
}

// NewPlatformInstallationModel constructs the model from the shared pool.
func NewPlatformInstallationModel(db *pgxpool.Pool, logger *logging.Logger) *PlatformInstallationModel {
	return &PlatformInstallationModel{DB: db, Logger: logger}
}

type installationQueryer interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

func getInstallation(ctx context.Context, q installationQueryer) (*PlatformInstallation, error) {
	var pi PlatformInstallation
	err := q.QueryRow(ctx, `
		SELECT installation_id, original_bootstrap_user_id, ceremony_version::int, completed_at
		FROM platform_installation
		WHERE singleton = TRUE
	`).Scan(&pi.InstallationID, &pi.OriginalBootstrapUserID, &pi.CeremonyVersion, &pi.CompletedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read installation: %w", err)
	}
	return &pi, nil
}

// GetInstallation returns the installation record, or nil before
// installation. It is a read; nothing about installation state is derived
// from process or container lifecycle.
func (m *PlatformInstallationModel) GetInstallation(ctx context.Context) (*PlatformInstallation, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()
	return getInstallation(ctx, m.DB)
}

// GetInstallationTx returns the installation record inside tx.
func (m *PlatformInstallationModel) GetInstallationTx(ctx context.Context, tx pgx.Tx) (*PlatformInstallation, error) {
	if tx == nil {
		return nil, errors.New("transaction is required")
	}
	return getInstallation(ctx, tx)
}

// GetPendingClaimSummary returns the pending claim's timing, or nil.
func (m *PlatformInstallationModel) GetPendingClaimSummary(ctx context.Context) (*BootstrapClaimSummary, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	var c BootstrapClaimSummary
	err := m.DB.QueryRow(ctx, `
		SELECT id, issued_at, expires_at
		FROM platform_bootstrap_claims
		WHERE status = 'pending'
	`).Scan(&c.ClaimID, &c.IssuedAt, &c.ExpiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read pending bootstrap claim: %w", err)
	}
	return &c, nil
}

// SupersedePendingClaimTx resolves any pending claim as superseded and
// erases its address and credential hash, so a re-issued claim always
// invalidates the previous credential. Returns how many were superseded.
func (m *PlatformInstallationModel) SupersedePendingClaimTx(ctx context.Context, tx pgx.Tx) (int64, error) {
	if tx == nil {
		return 0, errors.New("transaction is required")
	}
	tag, err := tx.Exec(ctx, `
		UPDATE platform_bootstrap_claims
		SET status = 'superseded',
		    resolved_at = clock_timestamp(),
		    claimant_email = NULL,
		    credential_hash = NULL
		WHERE status = 'pending'
	`)
	if err != nil {
		return 0, fmt.Errorf("supersede pending bootstrap claim: %w", TranslateGovernanceError(err))
	}
	return tag.RowsAffected(), nil
}

// InsertClaimTx records a pending claim. credentialHash must be the 32-byte
// SHA-256 of the credential; the credential itself is never passed here.
func (m *PlatformInstallationModel) InsertClaimTx(
	ctx context.Context,
	tx pgx.Tx,
	claimantEmail string,
	credentialHash []byte,
	validFor time.Duration,
) (*BootstrapClaimSummary, error) {
	if tx == nil {
		return nil, errors.New("transaction is required")
	}
	claimantEmail = normalizeUserEmail(claimantEmail)
	if claimantEmail == "" || len(credentialHash) != 32 || validFor <= 0 || validFor > 30*time.Minute {
		return nil, errors.New("bootstrap claim is incomplete")
	}

	var c BootstrapClaimSummary
	err := tx.QueryRow(ctx, `
		WITH stamp AS (SELECT clock_timestamp() AS issued)
		INSERT INTO platform_bootstrap_claims (claimant_email, credential_hash, issued_at, expires_at)
		SELECT $1, $2, issued, issued + ($3::bigint * interval '1 microsecond') FROM stamp
		RETURNING id, issued_at, expires_at
	`, claimantEmail, credentialHash, validFor.Microseconds()).Scan(&c.ClaimID, &c.IssuedAt, &c.ExpiresAt)
	if err != nil {
		translated := TranslateGovernanceError(err)
		if errors.Is(translated, ErrInstallationProtected) {
			return nil, ErrInstallationAlreadyInitialized
		}
		return nil, fmt.Errorf("insert bootstrap claim: %w", translated)
	}
	return &c, nil
}

// LockPendingClaimByCredentialTx locks the pending claim whose credential
// hash matches. Lookup is by the unique hash index; a miss, a resolved
// claim, and an unknown credential are indistinguishable to the caller.
func (m *PlatformInstallationModel) LockPendingClaimByCredentialTx(ctx context.Context, tx pgx.Tx, credentialHash []byte) (*LockedBootstrapClaim, error) {
	if tx == nil {
		return nil, errors.New("transaction is required")
	}
	if len(credentialHash) != 32 {
		return nil, ErrBootstrapClaimNotFound
	}
	var c LockedBootstrapClaim
	err := tx.QueryRow(ctx, `
		SELECT id, claimant_email::text, issued_at, expires_at
		FROM platform_bootstrap_claims
		WHERE credential_hash = $1
		  AND status = 'pending' AND expires_at > clock_timestamp()
		FOR UPDATE
	`, credentialHash).Scan(&c.ClaimID, &c.ClaimantEmail, &c.IssuedAt, &c.ExpiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrBootstrapClaimNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("lock bootstrap claim: %w", err)
	}
	return &c, nil
}

// CompleteClaimTx consumes the claim for userID and erases its address and
// credential hash.
func (m *PlatformInstallationModel) CompleteClaimTx(ctx context.Context, tx pgx.Tx, claimID, userID uuid.UUID) error {
	if tx == nil {
		return errors.New("transaction is required")
	}
	tag, err := tx.Exec(ctx, `
		UPDATE platform_bootstrap_claims
		SET status = 'completed',
		    resolved_at = clock_timestamp(),
		    completed_user_id = $2,
		    claimant_email = NULL,
		    credential_hash = NULL
		WHERE id = $1 AND status = 'pending' AND expires_at > clock_timestamp()
	`, claimID, userID)
	if err != nil {
		return fmt.Errorf("complete bootstrap claim: %w", TranslateGovernanceError(err))
	}
	if tag.RowsAffected() != 1 {
		return ErrBootstrapClaimNotFound
	}
	return nil
}

// InsertInstallationTx writes the installation record. The bootstrap
// ceremony must be declared and the claim completed in this transaction.
func (m *PlatformInstallationModel) InsertInstallationTx(ctx context.Context, tx pgx.Tx, userID, claimID uuid.UUID) (*PlatformInstallation, error) {
	if tx == nil {
		return nil, errors.New("transaction is required")
	}
	var pi PlatformInstallation
	err := tx.QueryRow(ctx, `
		INSERT INTO platform_installation (
			singleton,
			original_bootstrap_user_id,
			bootstrap_succession_generation,
			bootstrap_claim_id,
			ceremony_version
		)
		VALUES (TRUE, $1, 1, $2, $3)
		RETURNING installation_id, original_bootstrap_user_id, ceremony_version::int, completed_at
	`, userID, claimID, InstallationCeremonyVersion).
		Scan(&pi.InstallationID, &pi.OriginalBootstrapUserID, &pi.CeremonyVersion, &pi.CompletedAt)
	if err != nil {
		if IsUniqueViolation(err) {
			return nil, ErrInstallationAlreadyInitialized
		}
		return nil, fmt.Errorf("insert installation: %w", TranslateGovernanceError(err))
	}
	return &pi, nil
}

// BootstrapAccount is the users row a bootstrap completion acts on.
type BootstrapAccount struct {
	UserID    uuid.UUID
	PendingActivation bool
	HasExternalIdentity bool
	PasswordHash string
	IsActive  bool
	IsDeleted bool
}

// LockAccountByEmailTx locks the users row with email, if any, FOR UPDATE.
// Returns (nil, nil) when no account has the address.
func (m *PlatformInstallationModel) LockAccountByEmailTx(ctx context.Context, tx pgx.Tx, email string) (*BootstrapAccount, error) {
	if tx == nil {
		return nil, errors.New("transaction is required")
	}
	var a BootstrapAccount
	err := tx.QueryRow(ctx, `
		SELECT id, is_active, deleted_at IS NOT NULL, COALESCE(password_hash, ''),
		 EXISTS(SELECT 1 FROM activation_tokens WHERE user_id = users.id),
		 (google_id IS NOT NULL OR facebook_id IS NOT NULL)
		FROM users
		WHERE email = $1
		FOR UPDATE
	`, normalizeUserEmail(email)).Scan(&a.UserID, &a.IsActive, &a.IsDeleted, &a.PasswordHash, &a.PendingActivation, &a.HasExternalIdentity)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("lock bootstrap account: %w", err)
	}
	return &a, nil
}

// ReplaceAccountCredentialsTx is the bootstrap counterpart of password
// reset for an existing account whose address the claimant has just proven
// control of: it activates the account if its email was unconfirmed, sets
// the new password hash, revokes every refresh session, and removes any
// outstanding activation and password-reset credential. The caller holds
// the users row lock.
func (m *PlatformInstallationModel) ReplaceAccountCredentialsTx(ctx context.Context, tx pgx.Tx, userID uuid.UUID, passwordHash string) error {
	if tx == nil {
		return errors.New("transaction is required")
	}
	if userID == uuid.Nil || passwordHash == "" {
		return errors.New("bootstrap credential replacement is incomplete")
	}
	tag, err := tx.Exec(ctx, `
		UPDATE users
		SET password_hash = $2,
		    is_active = TRUE,
		    updated_at = NOW()
		WHERE id = $1
		  AND deleted_at IS NULL
	`, userID, passwordHash)
	if err != nil {
		return fmt.Errorf("replace bootstrap account credentials: %w", TranslateGovernanceError(err))
	}
	if tag.RowsAffected() != 1 {
		return ErrUserNotFound
	}
	for _, stmt := range []string{
		`UPDATE refresh_tokens SET revoked_at = NOW() WHERE user_id = $1 AND revoked_at IS NULL`,
		`DELETE FROM activation_tokens WHERE user_id = $1`,
		`DELETE FROM password_resets WHERE user_id = $1`,
	} {
		if _, err := tx.Exec(ctx, stmt, userID); err != nil {
			return fmt.Errorf("invalidate prior bootstrap account credentials: %w", err)
		}
	}
	return nil
}

// DeclareCompletionTx is transaction-local; it cannot survive a pooled connection.
func (m *PlatformInstallationModel) DeclareCompletionTx(ctx context.Context, tx pgx.Tx) error {
 if tx == nil { return errors.New("transaction is required") }
 _, err := tx.Exec(ctx, `SELECT set_config('sagrenti.installation_ceremony', 'complete', true)`)
 return err
}

// recognizeExistingInstallationTx records existing authority; it cannot establish
// or replace a Root. The canonical seed transaction owns the governance mutex.
func recognizeExistingInstallationTx(ctx context.Context, tx pgx.Tx, logger *logging.Logger) error {
 model := NewPlatformInstallationModel(nil, logger)
 pi, err := model.GetInstallationTx(ctx, tx)
 if err != nil || pi != nil { return err }
 var id uuid.UUID
 err = tx.QueryRow(ctx, `SELECT user_id FROM root_super_admin WHERE singleton = TRUE AND succession_generation = 1 FOR UPDATE`).Scan(&id)
 if errors.Is(err, pgx.ErrNoRows) { return nil }
 if err != nil { return err }
 if _, err := tx.Exec(ctx, `SELECT set_config('sagrenti.installation_ceremony', 'recognize_existing', true)`); err != nil { return err }
 var installationID uuid.UUID
 if err := tx.QueryRow(ctx, `INSERT INTO platform_installation
 (singleton, original_bootstrap_user_id, bootstrap_succession_generation, ceremony_version, establishment_source, completed_at)
 SELECT TRUE, user_id, 1, 1, 'existing_root', established_at FROM root_super_admin WHERE singleton = TRUE
 RETURNING installation_id`).Scan(&installationID); err != nil { return TranslateGovernanceError(err) }
 gov := NewAdministrativeGovernanceModel(nil, logger)
 return gov.InsertGovernanceAuditTx(ctx, tx, GovernanceAuditEntry{
  Action: "recognize_existing_installation", EntityType: "platform_installation", EntityID: installationID.String(),
  Outcome: GovernanceOutcomeSucceeded, Context: GovernanceAuditContext{Operation: "recognize_existing_installation"},
 })
}

// BootstrapCompletionProof is a read-only credential-version snapshot, never
// an authorization decision. Completion must recheck it under the users lock.
type BootstrapCompletionProof struct {
 UserID *uuid.UUID
 IsActive bool
 PasswordHash string
}
func (m *PlatformInstallationModel) ReadCompletionProof(ctx context.Context, hash []byte) (*BootstrapCompletionProof, error) {
 if len(hash) != 32 { return nil, ErrBootstrapClaimNotFound }
 var proof BootstrapCompletionProof
 err := m.DB.QueryRow(ctx, `SELECT u.id, COALESCE(u.is_active,FALSE), COALESCE(u.password_hash,'')
 FROM platform_bootstrap_claims c LEFT JOIN users u ON u.email=c.claimant_email
 WHERE c.credential_hash=$1 AND c.status='pending' AND c.expires_at>clock_timestamp()` ,hash).
 Scan(&proof.UserID,&proof.IsActive,&proof.PasswordHash)
 if errors.Is(err,pgx.ErrNoRows) { return nil, ErrBootstrapClaimNotFound }
 if err != nil { return nil, fmt.Errorf("read bootstrap completion proof: %w",err) }
 return &proof,nil
}
