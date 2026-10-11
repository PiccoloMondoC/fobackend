// Package installation implements the operator side of the one-time
// installation ceremony: issuing the Root bootstrap claim.
//
// focodebase/fobackend/internal/installation/installation.go
//
// GTM:
//
//	Layer: 2.1 Database / Governance Foundation
//	Release Class: SPINE
//	Reason:
//	  The installer supplies the initial Root Super Admin's email
//	  interactively (cmd/install). IssueRootBootstrapClaim records that
//	  entitlement as persistent database state and returns a one-time
//	  credential for delivery to that address. Only the credential's SHA-256
//	  is stored; issuing again supersedes (and so invalidates) the previous
//	  credential; issuance is refused once an installation exists.
//
//	  Completion (proof of control of the address, account establishment,
//	  incumbency) is services.CompleteRootBootstrapInternal, reached through
//	  the public completion endpoint.
//
//	  This package is imported only by cmd/install. The API binary does not
//	  link it (installation_boundary_test.go).
//
// SPINE Rule:
//
//	Keep compiling.
//	Keep production-ready.
//	Never persist, log, or audit the credential or the address.
//	Never issue a claim for an initialized installation.
//	Block deployment if this file breaks single-installation safety.
package installation

import (
	"context"
	"errors"
	"fmt"
	"net/mail"
	"strings"
	"time"

	"github.com/PiccoloMondoC/focodebase/fobackend/internal/data"
	"github.com/PiccoloMondoC/focodebase/fobackend/internal/logging"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ClaimValidity is how long an issued bootstrap credential can be used. It
// is an engineering policy; re-issuing is always possible before
// installation.
const ClaimValidity = 30 * time.Minute

// CompletionPath is the Angular route that completes bootstrap. The
// credential travels in the URL fragment so it never reaches server access
// logs, proxies, or Referer headers.
const CompletionPath = "/installation/complete"

// ErrInvalidClaimantEmail reports an unusable installer-supplied address.
var ErrInvalidClaimantEmail = errors.New("installation: enter a single plain email address")

// IssuedClaim is the result of issuance. Credential is plaintext bearer
// material for immediate delivery only.
type IssuedClaim struct {
	Credential string
	ExpiresAt  time.Time
	Superseded bool
}

// CompletionURL builds the browser link for frontendURL (a validated
// origin without a trailing slash).
func (c *IssuedClaim) CompletionURL(frontendURL string) string {
	return strings.TrimRight(frontendURL, "/") + CompletionPath + "#" + c.Credential
}

// NormalizeClaimantEmail validates the installer-supplied address: one plain
// address, no display name, no list.
func NormalizeClaimantEmail(raw string) (string, error) {
	value := strings.TrimSpace(raw)
	if value == "" || len(value) > 254 || strings.ContainsAny(value, " \t\r\n,;<>\"") {
		return "", ErrInvalidClaimantEmail
	}
	addr, err := mail.ParseAddress(value)
	if err != nil || addr.Name != "" || addr.Address != value {
		return "", ErrInvalidClaimantEmail
	}
	at := strings.LastIndex(value, "@")
	if at <= 0 || at == len(value)-1 || !strings.Contains(value[at+1:], ".") {
		return "", ErrInvalidClaimantEmail
	}
	return value, nil
}

// IssueRootBootstrapClaim records a pending bootstrap claim for email in one
// governance transaction and returns the one-time credential.
//
//	BEGIN
//	  advisory lock (serializes with every governance transaction)
//	  refuse if an installation exists
//	  supersede any pending claim (erases its address and hash)
//	  insert the new claim (address, SHA-256 of credential, expiry)
//	  audit issue_root_bootstrap_claim (no address, no credential)
//	COMMIT
//
// The credential is returned only after COMMIT. If delivery later fails the
// operator simply issues again, which supersedes this claim.
func IssueRootBootstrapClaim(ctx context.Context, pool *pgxpool.Pool, logger *logging.Logger, email string) (*IssuedClaim, error) {
	if ctx == nil || pool == nil || logger == nil {
		return nil, errors.New("installation: context, database pool and logger are required")
	}
	email, err := NormalizeClaimantEmail(email)
	if err != nil {
		return nil, err
	}
	credential, err := data.GenerateBootstrapCredential()
	if err != nil {
		return nil, err
	}

	gov := data.NewAdministrativeGovernanceModel(pool, logger)
	inst := data.NewPlatformInstallationModel(pool, logger)

	tx, err := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return nil, fmt.Errorf("installation: begin: %w", err)
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		defer cancel()
		_ = tx.Rollback(cleanup)
	}()

	if err := gov.AcquireGovernanceAdvisoryLockTx(ctx, tx); err != nil {
		return nil, err
	}
	existing, err := inst.GetInstallationTx(ctx, tx)
	if err != nil {
		return nil, err
	}
	root, err := gov.RootUserIDTx(ctx, tx)
	if err != nil { return nil, err }
	if existing != nil || root != nil {
		return nil, data.ErrInstallationAlreadyInitialized
	}
	if _, err := tx.Exec(ctx, `SELECT set_config('sagrenti.installation_ceremony', 'issue', true)`); err != nil { return nil, err }
	superseded, err := inst.SupersedePendingClaimTx(ctx, tx)
	if err != nil {
		return nil, err
	}
	claim, err := inst.InsertClaimTx(ctx, tx, email, data.BootstrapCredentialHash(credential), ClaimValidity)
	if err != nil {
		return nil, err
	}
	if err := gov.InsertGovernanceAuditTx(ctx, tx, data.GovernanceAuditEntry{
		Action:     "issue_root_bootstrap_claim",
		EntityType: "platform_installation",
		EntityID:   claim.ClaimID.String(),
		Outcome:    data.GovernanceOutcomeSucceeded,
		Context:    data.GovernanceAuditContext{Operation: "issue_root_bootstrap_claim"},
	}); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("installation: commit: %w", data.TranslateGovernanceError(err))
	}

	return &IssuedClaim{
		Credential: credential,
		ExpiresAt:  claim.ExpiresAt,
		Superseded: superseded > 0,
	}, nil
}

// Status is the operator view of installation state. It never includes an
// address.
type Status struct {
	Installation *data.PlatformInstallation
	PendingClaim *data.BootstrapClaimSummary
}

// ReadStatus reads installation state. It never writes.
func ReadStatus(ctx context.Context, pool *pgxpool.Pool, logger *logging.Logger) (*Status, error) {
	if ctx == nil || pool == nil || logger == nil { return nil, errors.New("installation: context, database pool and logger are required") }
	inst := data.NewPlatformInstallationModel(pool, logger)
	installation, err := inst.GetInstallation(ctx)
	if err != nil {
		return nil, err
	}
	status := &Status{Installation: installation}
	if installation == nil {
		if status.PendingClaim, err = inst.GetPendingClaimSummary(ctx); err != nil {
			return nil, err
		}
	}
	return status, nil
}

// InvitationDelivery delivers a completion link to the entitled address.
// The link contains the credential; implementations must never log it.
type InvitationDelivery interface {
	DeliverRootBootstrapInvitation(ctx context.Context, to, completionURL string, expiresAt time.Time) error
}
