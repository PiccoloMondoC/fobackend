// Package services contains trusted internal service composition.
//
// focodebase/fobackend/internal/services/installation_internal.go
//
// GTM:
//
//	Layer: 2.6 Internal Services / Installation
//	Release Class: SPINE
//	Reason:
//	  Completion of the one-time installation bootstrap. The installer names
//	  the initial Root Super Admin's email interactively during the
//	  installation ceremony (cmd/install), which issues a one-time credential
//	  to that address and stores only its SHA-256. The entitled person proves
//	  control of the address by presenting the credential here and becomes
//	  Root in one transaction. Existing active password accounts additionally
//	  prove their current password; it is not replaced. A pending email/password
//	  signup is confirmed with a new password. Other inactive/closed accounts
//	  and active passwordless accounts are refused, preserving authentication
//	  and suspension boundaries.
//
//	    declare bootstrap ceremony (transaction-local)
//	    lock the pending claim by credential hash; refuse if expired
//	    lock the users row for the claimant address (create it if absent)
//	    account: create, prove existing password, or complete pending signup
//	    active Super Admin appointment; super_admin primary role
//	    ledger generation 1; incumbent row
//	    claim -> completed (address and hash erased); installation row
//	    audit
//	  COMMIT (deferred installation and SPINE governance invariants verified)
//
//	  After completion users.id is the only identity anchor. The address is
//	  never written to configuration, logs, audit context, or API responses.
//	  Once an installation exists the database refuses every bootstrap path,
//	  so process restart, redeployment, scale-out, scale-to-zero recovery and
//	  container replacement can never reopen it. A new database is a new
//	  installation and may perform the ceremony again.
//
// SPINE Rule:
//
//	Keep compiling.
//	Keep production-ready.
//	Never log, audit, or return the bootstrap credential or claimant address.
//	Report every credential failure as one indistinguishable outcome.
//	Block deployment if this file breaks single-installation safety or
//	bootstrap credential handling.
package services

import (
	"context"
	"errors"
	"time"

	"github.com/PiccoloMondoC/focodebase/fobackend/internal/data"
	"github.com/PiccoloMondoC/focodebase/fobackend/internal/security"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// InstallationStatus reports installation state without identity detail.
type InstallationStatus struct {
	Installed      bool                        `json:"installed"`
	InstallationID *uuid.UUID                  `json:"installation_id,omitempty"`
	CompletedAt    *time.Time                  `json:"completed_at,omitempty"`
	PendingClaim   *data.BootstrapClaimSummary `json:"pending_claim,omitempty"`
}

func (s *Service) installation() *data.PlatformInstallationModel {
	return data.NewPlatformInstallationModel(s.Models.DB, s.Logger)
}

// GetInstallationStatusInternal reads installation state. It never writes.
func (s *Service) GetInstallationStatusInternal(ctx context.Context) (*InstallationStatus, error) {
	if ctx == nil { return nil, ErrNilContext }
	if err := s.validate(); err != nil {
		return nil, err
	}
	model := s.installation()
	installation, err := model.GetInstallation(ctx)
	if err != nil {
		return nil, err
	}
	status := &InstallationStatus{Installed: installation != nil}
	if installation != nil {
		status.InstallationID = &installation.InstallationID
		status.CompletedAt = &installation.CompletedAt
		return status, nil
	}
	if status.PendingClaim, err = model.GetPendingClaimSummary(ctx); err != nil {
		return nil, err
	}
	return status, nil
}

// CompleteRootBootstrapInternal consumes a bootstrap credential, establishes
// the claimant's account and makes it Root. password is the existing password
// for an active password account, or the new password for initial enrollment.
//
// Credential failures (unknown, malformed, superseded, expired, already
// used, installation already initialized) all return
// ErrBootstrapCredentialInvalid. Password policy failures return the
// security package's errors before any mutation. Password work happens before
// the governance mutex; active-account proof is rechecked under the users lock.
func (s *Service) CompleteRootBootstrapInternal(ctx context.Context, credential, newPassword string) (*data.PlatformInstallation, error) {
	if err := s.validate(); err != nil {
		return nil, err
	}
	if ctx == nil {
		return nil, ErrNilContext
	}
	if !data.WellFormedBootstrapCredential(credential) {
		return nil, ErrBootstrapCredentialInvalid
	}
	suppliedPassword := newPassword
	credentialHash := data.BootstrapCredentialHash(credential)
	credential = ""


 ctx, cancel := context.WithTimeout(ctx, s.Cfg.DBTimeout)
 defer cancel()
 gov := s.governance()
 inst := s.installation()
 // Reject unknown/expired claims before expensive password work. Verification
 // stays outside the global mutex; its exact account/hash snapshot is rechecked
 // under the users row lock before any promotion.
 proof, err := inst.ReadCompletionProof(ctx, credentialHash)
 if errors.Is(err, data.ErrBootstrapClaimNotFound) { return nil, ErrBootstrapCredentialInvalid }
 if err != nil { return nil, err }
 var passwordHash string
 if proof.IsActive && proof.PasswordHash != "" {
  if security.VerifyPassword(suppliedPassword, proof.PasswordHash) != nil { return nil, ErrBootstrapReauthenticationFailed }
 } else {
  passwordHash, err = security.HashPassword(newPassword)
  if err != nil { return nil, err }
 }
 newPassword = ""

	var result *data.PlatformInstallation
	err = s.withGovernanceTx(ctx, func(tx pgx.Tx) error {
		if err := inst.DeclareCompletionTx(ctx, tx); err != nil {
			return err
		}
		existing, err := inst.GetInstallationTx(ctx, tx)
		if err != nil {
			return err
		}
		root, err := gov.RootUserIDTx(ctx, tx)
		if err != nil { return err }
		if existing != nil || root != nil {
			return ErrBootstrapCredentialInvalid
		}

		// The claim row is locked before the claimant's users row because the
		// address is needed to find that row. Claims are written only inside
		// governance transactions, which already serialize on the advisory
		// lock, and public authentication never touches claims, so this
		// order cannot participate in a deadlock.
		claim, err := inst.LockPendingClaimByCredentialTx(ctx, tx, credentialHash)
		if err != nil {
			if errors.Is(err, data.ErrBootstrapClaimNotFound) {
				return ErrBootstrapCredentialInvalid
			}
			return err
		}
		

if _, err := s.reconcileAdministrativeAppointmentsTx(ctx, tx); err != nil { return err }

		account, err := inst.LockAccountByEmailTx(ctx, tx, claim.ClaimantEmail)
		if err != nil {
			return err
		}

if account != nil && proof.IsActive && !account.IsActive { return ErrBootstrapAccountUnavailable }

		var userID uuid.UUID
		switch {
		case account == nil:
            if proof.UserID != nil { return data.ErrConcurrentGovernanceChange }
			userID, err = s.Models.User.SignupPreparedTx(ctx, tx, &data.User{
				Email:        claim.ClaimantEmail,
				PasswordHash: passwordHash,
				IsActive:     true, // the credential proved control of the address
			})
			if errors.Is(err, data.ErrDuplicateEmail) {
				// A concurrent signup created the account; the caller retries
				// and takes the existing-account path.
				return data.ErrConcurrentGovernanceChange
			}
			if err != nil {
				return err
			}
			if _, err := gov.LockGovernanceSubjectsTx(ctx, tx, userID); err != nil {
				return err
			}
		case account.IsDeleted || (account.IsActive && account.PasswordHash == "") || (!account.IsActive && (!account.PendingActivation || account.HasExternalIdentity)) :
			return ErrBootstrapAccountUnavailable
		default:
			userID = account.UserID
			subjectRows, err := gov.LockGovernanceSubjectsTx(ctx, tx, userID)
            if err != nil { return err }
            subject := subjectRows[userID]
            if subject != nil && subject.Appointment != nil && subject.Appointment.Status != data.AppointmentStatusActive { return ErrBootstrapAccountUnavailable }
            if account.IsActive {
                // Existing active accounts keep their password and authentication identity.
                // Email entitlement does not authorize changing an established password.
                if !proof.IsActive || proof.UserID == nil || *proof.UserID != account.UserID || proof.PasswordHash != account.PasswordHash { return ErrBootstrapReauthenticationFailed }
            } else if err := inst.ReplaceAccountCredentialsTx(ctx, tx, userID, passwordHash); err != nil { return err }
            if subject != nil && subject.Appointment != nil && subject.Appointment.Level == data.AdministrativeLevelAdmin {
                roleID, err := s.Models.Role.ResolveRoleIDByNameTx(ctx, tx, data.RoleNameAdmin)
                if err != nil { return err }
                if _, err := s.Models.Role.RevokeRoleAssignmentTx(ctx, tx, userID, roleID, ""); err != nil { return err }
            }
		}

		reason := "Installation bootstrap"
		if _, err := gov.PutAppointmentTx(ctx, tx, data.AppointmentChange{
			UserID:  userID,
			Level:   data.AdministrativeLevelSuperAdmin,
			Status:  data.AppointmentStatusActive,
			Reason:  &reason,
			NewTerm: true,
		}); err != nil {
			return err
		}
		superAdminRoleID, err := s.Models.Role.ResolveRoleIDByNameTx(ctx, tx, data.RoleNameSuperAdmin)
		if err != nil {
			return err
		}
		if err := s.Models.Role.AssignPrimaryRoleTx(ctx, tx, userID, superAdminRoleID, nil); err != nil {
			return err
		}

		if err := gov.InsertRootSuperAdminTx(ctx, tx, userID); err != nil {
			if errors.Is(err, data.ErrInstallationAlreadyInitialized) {
				return ErrBootstrapCredentialInvalid
			}
			return err
		}
		if err := inst.CompleteClaimTx(ctx, tx, claim.ClaimID, userID); err != nil {
			if errors.Is(err, data.ErrBootstrapClaimNotFound) {
				return ErrBootstrapCredentialInvalid
			}
			return err
		}
		installation, err := inst.InsertInstallationTx(ctx, tx, userID, claim.ClaimID)
		if err != nil {
			if errors.Is(err, data.ErrInstallationAlreadyInitialized) {
				return ErrBootstrapCredentialInvalid
			}
			return err
		}

		if err := gov.InsertGovernanceAuditTx(ctx, tx, data.GovernanceAuditEntry{
			Action:     "bootstrap_root_super_admin",
			EntityType: data.GovernanceEntityRootSuperAdmin,
			EntityID:   userID.String(),
			Outcome:    data.GovernanceOutcomeSucceeded,
			Context: data.GovernanceAuditContext{
				Operation:            "bootstrap_root_super_admin",
				Mechanism:            "bootstrap",
				SuccessionGeneration: 1,
				NewLevel:             string(data.AdministrativeLevelSuperAdmin),
				NewStatus:            string(data.AppointmentStatusActive),
			},
		}); err != nil {
			return err
		}
		if err := gov.InsertGovernanceAuditTx(ctx, tx, data.GovernanceAuditEntry{
			Action:     "initialize_installation",
			EntityType: "platform_installation",
			EntityID:   installation.InstallationID.String(),
			Outcome:    data.GovernanceOutcomeSucceeded,
			Context:    data.GovernanceAuditContext{Operation: "initialize_installation"},
		}); err != nil {
			return err
		}

		result = installation
		return nil
	})
	if err != nil {
		if errors.Is(err, data.ErrInstallationProtected) { return nil, ErrBootstrapCredentialInvalid }
		return nil, err
	}
	return result, nil
}
