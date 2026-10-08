// Package services contains trusted internal service composition,
// automation support, moderation helpers, and shared internal workflow logic.
//
// focodebase/fobackend/internal/services/administrative_governance_internal.go
//
// GTM:
//
//	Layer: 2.6 Internal Services / Administrative Governance
//	Release Class: SPINE
//	Reason:
//	  Owns the administrative hierarchy (Root Super Admin -> Super Admin ->
//	  Admin): the authority policy, Root bootstrap, reconciliation of
//	  pre-governance role assignments, appointment lifecycle, account-removal
//	  safeguards, and the administrative directory reads.
//
//	  Authority is decided here from state read under the governance lock in
//	  the same transaction that mutates it, never from request context. The
//	  database triggers (database.go) are the backstop for every invariant
//	  enforced here.
//
// SPINE Rule:
//
//	Keep compiling.
//	Keep production-ready.
//	Preserve exactly one protected Root Super Admin.
//	Preserve AuthorizeGovernanceOperation as the single authority policy.
//	Preserve re-authentication for Super Admin appointment.
//	Preserve in-transaction audit of every successful governance mutation and
//	best-effort audit of every denial.
//	Do not authorize from request context or cached standing.
//	Block deployment if this file breaks Root protection, hierarchy
//	enforcement, bootstrap safety, or governance auditing.
package services

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/PiccoloMondoC/focodebase/fobackend/internal/data"
	"github.com/PiccoloMondoC/focodebase/fobackend/internal/security"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// GovernanceOperation names an administrative-standing change.
type GovernanceOperation string

const (
	GovernanceAppointAdmin      GovernanceOperation = "appoint_admin"
	GovernanceAppointSuperAdmin GovernanceOperation = "appoint_super_admin" // also promotes an active Admin
	GovernanceDemoteToAdmin     GovernanceOperation = "demote_to_admin"
	GovernanceSuspend           GovernanceOperation = "suspend"
	GovernanceRestore           GovernanceOperation = "restore"
	GovernanceRevoke            GovernanceOperation = "revoke"
)

// Valid reports whether op is a known operation.
func (op GovernanceOperation) Valid() bool {
	switch op {
	case GovernanceAppointAdmin, GovernanceAppointSuperAdmin, GovernanceDemoteToAdmin,
		GovernanceSuspend, GovernanceRestore, GovernanceRevoke:
		return true
	}
	return false
}

// Available-action vocabulary shared with the Admin Console. It is advisory
// presentation only: every action is re-authorized when requested.
const (
	ActionAppointAdmin        = "appoint_admin"
	ActionAppointSuperAdmin   = "appoint_super_admin"
	ActionPromoteToSuperAdmin = "promote_to_super_admin"
	ActionDemoteToAdmin       = "demote_to_admin"
	ActionSuspend             = "suspend"
	ActionRestore             = "restore"
	ActionRevoke              = "revoke"
	ActionExpel               = "expel"
)

// MaxGovernanceReasonLength bounds the optional reason (runes).
const MaxGovernanceReasonLength = 500

// GovernanceActor is the acting account's authority, read under lock.
type GovernanceActor struct {
	UserID uuid.UUID
	Level  data.AdministrativeLevel // active level only
	IsRoot bool
}

// GovernanceTarget is the target account's state, read under lock.
type GovernanceTarget struct {
	UserID    uuid.UUID
	Exists    bool
	IsActive  bool
	IsDeleted bool
	IsRoot    bool
	Level     data.AdministrativeLevel // appointment level regardless of status
	Status    data.AppointmentStatus
}

func actorFromSubject(s *data.GovernanceSubject) GovernanceActor {
	if s == nil {
		return GovernanceActor{}
	}
	return GovernanceActor{UserID: s.UserID, Level: s.ActiveLevel(), IsRoot: s.IsRoot && s.ActiveLevel() == data.AdministrativeLevelSuperAdmin}
}

func targetFromSubject(id uuid.UUID, s *data.GovernanceSubject) GovernanceTarget {
	if s == nil {
		return GovernanceTarget{UserID: id}
	}
	return GovernanceTarget{
		UserID:    s.UserID,
		Exists:    true,
		IsActive:  s.IsActive,
		IsDeleted: s.IsDeleted,
		IsRoot:    s.IsRoot,
		Level:     s.AppointmentLevel(),
		Status:    s.AppointmentStatus(),
	}
}

// requiredRootFor reports whether op against a target at targetLevel is
// reserved to the Root (otherwise any active Super Admin suffices).
func requiredRootFor(op GovernanceOperation, targetLevel data.AdministrativeLevel) bool {
	switch op {
	case GovernanceAppointAdmin, GovernanceAppointSuperAdmin:
		return false
	case GovernanceDemoteToAdmin:
		return true
	default:
		// Suspend, restore, revoke: governing a Super Admin is Root-only.
		return targetLevel == data.AdministrativeLevelSuperAdmin
	}
}

// AuthorizeGovernanceOperation is the single authority policy for
// administrative-standing changes. It is pure: the caller supplies state
// read under the governance lock. Order of checks is deliberate: identity
// and Root protection first, then authority, then transition validity, so a
// lower-tier actor learns nothing about a higher-tier target's state.
func AuthorizeGovernanceOperation(actor GovernanceActor, target GovernanceTarget, op GovernanceOperation) error {
	if !op.Valid() {
		return ErrGovernanceInputInvalid
	}
	if actor.UserID == uuid.Nil || target.UserID == uuid.Nil {
		return ErrGovernanceInputInvalid
	}
	if actor.UserID == target.UserID {
		return ErrGovernanceSelfTarget
	}
	if actor.Level != data.AdministrativeLevelSuperAdmin {
		// Admins govern no administrators; non-administrators govern nothing.
		return ErrGovernanceInsufficientAuthority
	}
	if target.IsRoot {
		return ErrGovernanceRootProtected
	}
	if !target.Exists || target.IsDeleted {
		return ErrGovernanceTargetNotFound
	}
	if requiredRootFor(op, target.Level) && !actor.IsRoot {
		return ErrGovernanceInsufficientAuthority
	}

	switch op {
	case GovernanceAppointAdmin:
		if target.Status == data.AppointmentStatusActive || target.Status == data.AppointmentStatusSuspended {
			return ErrGovernanceInvalidTransition
		}
		if !target.IsActive {
			return ErrGovernanceTargetIneligible
		}
	case GovernanceAppointSuperAdmin:
		switch {
		case target.Status == data.AppointmentStatusNone, target.Status == data.AppointmentStatusRevoked:
		case target.Status == data.AppointmentStatusActive && target.Level == data.AdministrativeLevelAdmin:
		default:
			return ErrGovernanceInvalidTransition
		}
		if !target.IsActive {
			return ErrGovernanceTargetIneligible
		}
	case GovernanceDemoteToAdmin:
		if target.Level != data.AdministrativeLevelSuperAdmin ||
			(target.Status != data.AppointmentStatusActive && target.Status != data.AppointmentStatusSuspended) {
			return ErrGovernanceInvalidTransition
		}
	case GovernanceSuspend:
		if target.Status != data.AppointmentStatusActive {
			return ErrGovernanceInvalidTransition
		}
	case GovernanceRestore:
		if target.Status != data.AppointmentStatusSuspended {
			return ErrGovernanceInvalidTransition
		}
		if !target.IsActive {
			return ErrGovernanceTargetIneligible
		}
	case GovernanceRevoke:
		if target.Status != data.AppointmentStatusActive && target.Status != data.AppointmentStatusSuspended {
			return ErrGovernanceInvalidTransition
		}
	}
	return nil
}

// AuthorizeAccountRemoval decides whether actor may close (expel) target's
// account. Administrators — active or suspended — must have their standing
// revoked first, so account destruction can never silently remove authority.
func AuthorizeAccountRemoval(actor GovernanceActor, target GovernanceTarget) error {
	if actor.UserID == uuid.Nil || target.UserID == uuid.Nil {
		return ErrGovernanceInputInvalid
	}
	if actor.UserID == target.UserID {
		return ErrGovernanceSelfTarget
	}
	if actor.Level == data.AdministrativeLevelNone {
		return ErrGovernanceInsufficientAuthority
	}
	if target.IsRoot {
		return ErrGovernanceRootProtected
	}
	if !target.Exists || target.IsDeleted {
		return ErrGovernanceTargetNotFound
	}
	if target.Status == data.AppointmentStatusActive || target.Status == data.AppointmentStatusSuspended {
		return ErrGovernanceAdministratorAccount
	}
	return nil
}

// AuthorizeAccountStewardship decides whether actor may moderate or edit
// target's profile: the actor must outrank the target's active standing.
func AuthorizeAccountStewardship(actor GovernanceActor, target GovernanceTarget) error {
	if actor.Level == data.AdministrativeLevelNone {
		return ErrGovernanceInsufficientAuthority
	}
	if target.IsRoot && !(actor.IsRoot && actor.UserID == target.UserID) {
		return ErrGovernanceRootProtected
	}
	if !target.Exists || target.IsDeleted {
		return ErrGovernanceTargetNotFound
	}
	if target.Status == data.AppointmentStatusActive || target.Status == data.AppointmentStatusSuspended {
		switch target.Level {
		case data.AdministrativeLevelSuperAdmin:
			if !actor.IsRoot {
				return ErrGovernanceInsufficientAuthority
			}
		case data.AdministrativeLevelAdmin:
			if actor.Level != data.AdministrativeLevelSuperAdmin {
				return ErrGovernanceInsufficientAuthority
			}
		}
	}
	return nil
}

// AvailableGovernanceActions lists the actions actor could request against
// target right now. Presentation only.
func AvailableGovernanceActions(actor GovernanceActor, target GovernanceTarget) []string {
	actions := make([]string, 0, 4)
	add := func(op GovernanceOperation, name string) {
		if AuthorizeGovernanceOperation(actor, target, op) == nil {
			actions = append(actions, name)
		}
	}
	add(GovernanceAppointAdmin, ActionAppointAdmin)
	if target.Status == data.AppointmentStatusActive && target.Level == data.AdministrativeLevelAdmin {
		add(GovernanceAppointSuperAdmin, ActionPromoteToSuperAdmin)
	} else {
		add(GovernanceAppointSuperAdmin, ActionAppointSuperAdmin)
	}
	add(GovernanceDemoteToAdmin, ActionDemoteToAdmin)
	add(GovernanceSuspend, ActionSuspend)
	add(GovernanceRestore, ActionRestore)
	add(GovernanceRevoke, ActionRevoke)
	if AuthorizeAccountRemoval(actor, target) == nil {
		actions = append(actions, ActionExpel)
	}
	return actions
}

// GovernanceDenialCode is the stable code recorded for a denial and returned
// to clients. It never contains target state beyond what the code implies.
func GovernanceDenialCode(err error) string {
	switch {
	case errors.Is(err, ErrGovernanceSelfTarget):
		return "self_governance_refused"
	case errors.Is(err, ErrGovernanceRootProtected), errors.Is(err, data.ErrRootSuperAdminProtected):
		return "root_protected"
	case errors.Is(err, ErrGovernanceInsufficientAuthority), errors.Is(err,data.ErrAdministrativePermissionDenied):
		return "insufficient_authority"
	case errors.Is(err, ErrGovernanceTargetNotFound), errors.Is(err, data.ErrUserNotFound):
		return "account_not_found"
	case errors.Is(err, ErrGovernanceTargetIneligible):
		return "account_ineligible"
	case errors.Is(err, ErrGovernanceInvalidTransition):
		return "invalid_transition"
	case errors.Is(err, ErrGovernanceAdministratorAccount):
		return "administrator_account"
	case errors.Is(err, ErrGovernanceReauthenticationFailed):
		return "reauthentication_failed"
	case errors.Is(err, ErrGovernanceReauthenticationUnavailable):
		return "reauthentication_unavailable"
	case errors.Is(err, ErrGovernanceConfirmationMismatch):
		return "confirmation_mismatch"
	case errors.Is(err, ErrGovernanceInputInvalid):
		return "invalid_request"
	case errors.Is(err, data.ErrConcurrentGovernanceChange):
		return "concurrent_change"
	case errors.Is(err, data.ErrAdministrativeInconsistency), errors.Is(err, data.ErrGovernanceRoleProtected):
		return "governance_invariant"
	case errors.Is(err, ErrGovernanceAdministratorSelfClosure):
		return "administrator_self_closure"
	default:
		return "server_error"
	}
}

// isAuditableDenial reports whether a refusal is security-significant enough
// to record (attempts against authority, the Root, or re-authentication).
func isAuditableDenial(err error) bool {
	return errors.Is(err, ErrGovernanceSelfTarget) ||
		errors.Is(err, ErrGovernanceRootProtected) ||
		errors.Is(err, data.ErrRootSuperAdminProtected) ||
		errors.Is(err, ErrGovernanceInsufficientAuthority) ||
		errors.Is(err, ErrGovernanceReauthenticationFailed) ||
		errors.Is(err, ErrGovernanceReauthenticationUnavailable) ||
		errors.Is(err, ErrGovernanceConfirmationMismatch) ||
		errors.Is(err, ErrGovernanceAdministratorAccount)
}

// ---------------------------------------------------------------------------
// Standing
// ---------------------------------------------------------------------------

// AdministrativeStanding is the caller's administrative standing, reported
// to the Admin Console for presentation. It is never used to authorize.
type AdministrativeStanding struct {
	Level                 data.AdministrativeLevel `json:"level"`
	IsRoot                bool                     `json:"is_root"`
	CanGovernAdmins       bool                     `json:"can_govern_admins"`
	CanAppointSuperAdmins bool                     `json:"can_appoint_super_admins"`
	CanGovernSuperAdmins  bool                     `json:"can_govern_super_admins"`
}

// StandingFor derives standing from an actor.
func StandingFor(actor GovernanceActor) AdministrativeStanding {
	isSuper := actor.Level == data.AdministrativeLevelSuperAdmin
	return AdministrativeStanding{
		Level:                 actor.Level,
		IsRoot:                actor.IsRoot,
		CanGovernAdmins:       isSuper,
		CanAppointSuperAdmins: isSuper,
		CanGovernSuperAdmins:  actor.IsRoot,
	}
}

func (s *Service) governance() *data.AdministrativeGovernanceModel {
	return data.NewAdministrativeGovernanceModel(s.Models.DB, s.Logger)
}

func (s *Service) readActor(ctx context.Context, actorID uuid.UUID) (GovernanceActor, error) {
	subject, err := s.governance().GetGovernanceSubject(ctx, actorID)
	if err != nil {
		if errors.Is(err, data.ErrUserNotFound) {
			return GovernanceActor{UserID: actorID}, nil
		}
		return GovernanceActor{}, err
	}
	return actorFromSubject(subject), nil
}

// GetAdministrativeStandingInternal returns userID's current standing.
func (s *Service) GetAdministrativeStandingInternal(ctx context.Context, userID uuid.UUID) (AdministrativeStanding, error) {
	if err := s.validate(); err != nil {
		return AdministrativeStanding{}, err
	}
	if userID == uuid.Nil {
		return AdministrativeStanding{}, ErrUsersActorRequired
	}
	actor, err := s.readActor(ctx, userID)
	if err != nil {
		return AdministrativeStanding{}, fmt.Errorf("read administrative standing: %w", err)
	}
	return StandingFor(actor), nil
}

// ---------------------------------------------------------------------------
// Transactions
// ---------------------------------------------------------------------------

func (s *Service) withGovernanceTx(ctx context.Context, fn func(tx pgx.Tx) error) error {
 tx,err:=s.Models.DB.BeginTx(ctx,pgx.TxOptions{IsoLevel:pgx.ReadCommitted})
 if err!=nil{return fmt.Errorf("begin administrative transaction: %w",err)}
 defer func(){
  cleanup,cancel:=context.WithTimeout(context.WithoutCancel(ctx),s.Cfg.DBTimeout);defer cancel()
  if err:=tx.Rollback(cleanup);err!=nil && !errors.Is(err,pgx.ErrTxClosed){s.Logger.Warn("administrative rollback failed","error",err)}
 }()
 // One bounded control-plane mutex provides a common order across appointment,
 // bootstrap, expulsion, catalog edits and settings. Public auth does not take
 // this mutex: it retains users-first row locking, with no reverse dependency.
 if err:=s.governance().AcquireGovernanceAdvisoryLockTx(ctx,tx);err!=nil{return err}
 if err:=fn(tx);err!=nil{return data.TranslateGovernanceError(err)}
 if err:=tx.Commit(ctx);err!=nil {
  // Do not retry credential-backed writes. Connection loss during COMMIT may
  // have committed; the HTTP/UI contract treats unknown server errors as an
  // unconfirmed outcome and requires a fresh read.
  return fmt.Errorf("commit administrative transaction: %w",data.TranslateGovernanceError(err))
 }
 return nil
}

func (s *Service) recordGovernanceDenial(
	ctx context.Context,
	actorID uuid.UUID,
	targetID uuid.UUID,
	auditAction string,
	op string,
	cause error,
) {
	if !isAuditableDenial(cause) {
		return
	}
	auditCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), s.Cfg.DBTimeout)
	defer cancel()

	entityType := data.GovernanceEntityAdministrator
	actor := actorID
	if err := s.governance().InsertGovernanceAuditTx(auditCtx, s.Models.DB, data.GovernanceAuditEntry{
		ActorID:    &actor,
		Action:     auditAction,
		EntityType: entityType,
		EntityID:   targetID.String(),
		Outcome:    data.GovernanceOutcomeDenied,
		Context: data.GovernanceAuditContext{
			Operation:  op,
			DenialCode: GovernanceDenialCode(cause),
		},
	}); err != nil {
		s.Logger.Warn("governance denial could not be audited",
			"actor_user_id", actorID,
			"target_user_id", targetID,
			"operation", op,
			"error", err,
		)
	}
}

// ---------------------------------------------------------------------------
// Appointment lifecycle
// ---------------------------------------------------------------------------

// GovernanceRequest is the transport-neutral input to a governance change.
type GovernanceRequest struct {
	ActorUserID  uuid.UUID
	TargetUserID uuid.UUID
	Operation    GovernanceOperation
	Reason       string

	// Required only for GovernanceAppointSuperAdmin. CurrentPassword is the
	// actor's own password (re-authentication); it is verified and then
	// discarded, never logged or audited. ConfirmTargetEmail must equal the
	// target account's email.
	CurrentPassword    string
	ConfirmTargetEmail string
}

func auditActionFor(op GovernanceOperation, target GovernanceTarget) string {
	switch op {
	case GovernanceAppointAdmin:
		return "appoint_admin"
	case GovernanceAppointSuperAdmin:
		if target.Status == data.AppointmentStatusActive && target.Level == data.AdministrativeLevelAdmin {
			return "promote_to_super_admin"
		}
		return "appoint_super_admin"
	case GovernanceDemoteToAdmin:
		return "demote_super_admin"
	case GovernanceSuspend:
		return "suspend_administrator"
	case GovernanceRestore:
		return "restore_administrator"
	default:
		return "revoke_administrator"
	}
}

func normalizeGovernanceReason(reason string) (*string, error) {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return nil, nil
	}
	if utf8.RuneCountInString(reason) > MaxGovernanceReasonLength {
		return nil, ErrGovernanceInputInvalid
	}
	return &reason, nil
}

// ExecuteGovernanceOperationInternal performs one administrative-standing
// change atomically:
//
//	(re-authenticate actor, Super Admin appointment only, before any lock)
//	BEGIN
//	  lock users rows of actor and target (ascending ID)
//	  re-read standing; AuthorizeGovernanceOperation
//	  re-check permission and the verified credential version
//	  write appointment; adjust governance role assignment (RoleModel)
//	  audit (succeeded); read the response snapshot
//	COMMIT  (deferred consistency triggers verify the result)
//
// Denials are audited best-effort after rollback.
func (s *Service) ExecuteGovernanceOperationInternal(ctx context.Context,req GovernanceRequest) (*data.AdministratorRecord,error) {
 if err:=s.validate();err!=nil{return nil,err}
 if ctx==nil{return nil,ErrNilContext}
 if req.ActorUserID==uuid.Nil || req.TargetUserID==uuid.Nil || !req.Operation.Valid(){return nil,ErrGovernanceInputInvalid}
 reason,err:=normalizeGovernanceReason(req.Reason);if err!=nil{return nil,err}
 ctx,cancel:=context.WithTimeout(ctx,s.Cfg.DBTimeout);defer cancel()
 gov:=s.governance()
 action:=auditActionFor(req.Operation,GovernanceTarget{})
 // Password verification is expensive and happens before transactional locks.
 // The exact persisted hash used here is compared again after user locks. A
 // password reset/change therefore invalidates this proof instead of racing it.
 var credentialVersion string
 if req.Operation==GovernanceAppointSuperAdmin {
  if req.CurrentPassword=="" || len(req.CurrentPassword)>72 || req.ConfirmTargetEmail==""{return nil,ErrGovernanceInputInvalid}
  subject,readErr:=gov.GetGovernanceSubject(ctx,req.ActorUserID)
  if readErr!=nil{return nil,readErr}
  if subject.ActiveLevel()!=data.AdministrativeLevelSuperAdmin {
   s.recordGovernanceDenial(ctx,req.ActorUserID,req.TargetUserID,action,string(req.Operation),ErrGovernanceInsufficientAuthority)
   return nil,ErrGovernanceInsufficientAuthority
  }
  credentialVersion=subject.PasswordHash
  if credentialVersion==""{err=ErrGovernanceReauthenticationUnavailable
  }else if security.VerifyPassword(req.CurrentPassword,credentialVersion)!=nil{err=ErrGovernanceReauthenticationFailed}
  req.CurrentPassword=""
  if err!=nil{s.recordGovernanceDenial(ctx,req.ActorUserID,req.TargetUserID,action,string(req.Operation),err);return nil,err}
 }
 var result *data.AdministratorRecord
 err=s.withGovernanceTx(ctx,func(tx pgx.Tx) error {
  subjects,err:=gov.LockGovernanceSubjectsTx(ctx,tx,req.ActorUserID,req.TargetUserID);if err!=nil{return err}
  actor:=actorFromSubject(subjects[req.ActorUserID]);actor.UserID=req.ActorUserID
  targetSubject:=subjects[req.TargetUserID]
  target:=targetFromSubject(req.TargetUserID,targetSubject)
  if err:=AuthorizeGovernanceOperation(actor,target,req.Operation);err!=nil{return err}
  if err:=gov.RequirePermissionTx(ctx,tx,actor.UserID,governancePermission(req.Operation));err!=nil{return err}
  if req.Operation==GovernanceAppointSuperAdmin {
   if subjects[req.ActorUserID].PasswordHash!=credentialVersion{return ErrGovernanceReauthenticationFailed}
   // The UI asks for the canonical address exactly. No trimming or case-fold
   // here: confirmation must represent the target shown to this operator.
   if req.ConfirmTargetEmail!=targetSubject.Email{return ErrGovernanceConfirmationMismatch}
  }
  action=auditActionFor(req.Operation,target)
  level,status,err:=s.applyGovernanceChange(ctx,tx,gov,actor,target,req.Operation,reason);if err!=nil{return err}
  audit:=data.GovernanceAuditContext{Operation:string(req.Operation),PreviousLevel:string(target.Level),PreviousStatus:string(target.Status),NewLevel:string(level),NewStatus:string(status)}
  if reason!=nil{audit.Reason=*reason}
  if err:=gov.InsertGovernanceAuditTx(ctx,tx,data.GovernanceAuditEntry{ActorID:&req.ActorUserID,Action:action,
   EntityType:data.GovernanceEntityAdministrator,EntityID:req.TargetUserID.String(),Outcome:data.GovernanceOutcomeSucceeded,Context:audit});err!=nil{return err}
  // Snapshot the response before commit; it cannot accidentally describe a
  // later concurrent operation. Any failed read aborts the whole change.
  result,err=gov.GetAdministratorTx(ctx,tx,req.TargetUserID)
  return err
 })
 if err!=nil{s.recordGovernanceDenial(ctx,req.ActorUserID,req.TargetUserID,action,string(req.Operation),err);return nil,err}
 return result,nil
}

func governancePermission(op GovernanceOperation) string {
 switch op {
 case GovernanceAppointAdmin:return "appoint_admin"
 case GovernanceAppointSuperAdmin:return "appoint_super_admin"
 case GovernanceDemoteToAdmin:return "demote_administrator"
 case GovernanceSuspend:return "suspend_administrator"
 case GovernanceRestore:return "restore_administrator"
 case GovernanceRevoke:return "revoke_administrator"
 default:return ""
 }
}

// applyGovernanceChange writes the appointment and governance role
// assignment for an authorized operation. Caller holds both users locks.
func (s *Service) applyGovernanceChange(
	ctx context.Context,
	tx pgx.Tx,
	gov *data.AdministrativeGovernanceModel,
	actor GovernanceActor,
	target GovernanceTarget,
	op GovernanceOperation,
	reason *string,
) (data.AdministrativeLevel, data.AppointmentStatus, error) {
	actorID := actor.UserID

	revoke := func(level data.AdministrativeLevel, fallback string) error {
		roleID, err := s.Models.Role.ResolveRoleIDByNameTx(ctx, tx, level.RoleName())
		if err != nil {
			return err
		}
		_, err = s.Models.Role.RevokeRoleAssignmentTx(ctx, tx, target.UserID, roleID, fallback)
		return err
	}
	grantPrimary := func(level data.AdministrativeLevel) error {
		roleID, err := s.Models.Role.ResolveRoleIDByNameTx(ctx, tx, level.RoleName())
		if err != nil {
			return err
		}
		return s.Models.Role.AssignPrimaryRoleTx(ctx, tx, target.UserID, roleID, &actorID)
	}
	put := func(level data.AdministrativeLevel, status data.AppointmentStatus, newTerm bool) error {
		_, err := gov.PutAppointmentTx(ctx, tx, data.AppointmentChange{
			UserID:  target.UserID,
			Level:   level,
			Status:  status,
			ActorID: &actorID,
			Reason:  reason,
			NewTerm: newTerm,
		})
		return err
	}

	switch op {
	case GovernanceAppointAdmin:
		if err := put(data.AdministrativeLevelAdmin, data.AppointmentStatusActive, true); err != nil {
			return "", "", err
		}
		return data.AdministrativeLevelAdmin, data.AppointmentStatusActive, grantPrimary(data.AdministrativeLevelAdmin)

	case GovernanceAppointSuperAdmin:
		if target.Status == data.AppointmentStatusActive && target.Level == data.AdministrativeLevelAdmin {
			if err := revoke(data.AdministrativeLevelAdmin, ""); err != nil {
				return "", "", err
			}
		}
		if err := put(data.AdministrativeLevelSuperAdmin, data.AppointmentStatusActive, true); err != nil {
			return "", "", err
		}
		return data.AdministrativeLevelSuperAdmin, data.AppointmentStatusActive, grantPrimary(data.AdministrativeLevelSuperAdmin)

	case GovernanceDemoteToAdmin:
		if err := put(data.AdministrativeLevelAdmin, target.Status, false); err != nil {
			return "", "", err
		}
		if target.Status == data.AppointmentStatusActive {
			if err := revoke(data.AdministrativeLevelSuperAdmin, ""); err != nil {
				return "", "", err
			}
			if err := grantPrimary(data.AdministrativeLevelAdmin); err != nil {
				return "", "", err
			}
		}
		return data.AdministrativeLevelAdmin, target.Status, nil

	case GovernanceSuspend:
		if err := put(target.Level, data.AppointmentStatusSuspended, false); err != nil {
			return "", "", err
		}
		return target.Level, data.AppointmentStatusSuspended, revoke(target.Level, data.RoleNameConsumer)

	case GovernanceRestore:
		if err := put(target.Level, data.AppointmentStatusActive, false); err != nil {
			return "", "", err
		}
		return target.Level, data.AppointmentStatusActive, grantPrimary(target.Level)

	case GovernanceRevoke:
		if err := put(target.Level, data.AppointmentStatusRevoked, false); err != nil {
			return "", "", err
		}
		if target.Status == data.AppointmentStatusActive {
			if err := revoke(target.Level, data.RoleNameConsumer); err != nil {
				return "", "", err
			}
		}
		return target.Level, data.AppointmentStatusRevoked, nil
	}
	return "", "", ErrGovernanceInputInvalid
}

// ---------------------------------------------------------------------------
// Root bootstrap and reconciliation
// ---------------------------------------------------------------------------

// RootBootstrapOutcome reports what EstablishRootSuperAdminInternal did.
type RootBootstrapOutcome string

const (
	RootBootstrapEstablished        RootBootstrapOutcome = "established"
	RootBootstrapAlreadyEstablished RootBootstrapOutcome = "already_established"
)

// EstablishRootSuperAdminInternal establishes the first Root Super Admin
// from an existing account identified by email. It never creates accounts,
// never handles passwords, and is safe to re-run:
//
//   - Root absent, account eligible: established (one transaction, audited).
//   - Root present and it is this account: already established, no change.
//   - Root present and it is another account: ErrRootBootstrapConflict.
//   - Account missing or deleted: ErrRootBootstrapAccountNotFound.
//   - Account inactive (email not confirmed): ErrRootBootstrapAccountIneligible.
//
// Concurrent bootstraps across instances serialize on the governance
// advisory lock; the singleton key makes a second Root impossible regardless.
func (s *Service) EstablishRootSuperAdminInternal(ctx context.Context, email string) (RootBootstrapOutcome, uuid.UUID, error) {
	if err := s.validate(); err != nil {
		return "", uuid.Nil, err
	}
	email = strings.TrimSpace(email)
	if email == "" {
		return "", uuid.Nil, ErrGovernanceInputInvalid
	}

	ctx, cancel := context.WithTimeout(ctx, s.Cfg.DBTimeout)
	defer cancel()

	gov := s.governance()
	var outcome RootBootstrapOutcome
	var rootID uuid.UUID

	err := s.withGovernanceTx(ctx, func(tx pgx.Tx) error {
		if err := gov.AcquireGovernanceAdvisoryLockTx(ctx, tx); err != nil {
			return err
		}

		existing, err := gov.RootUserIDTx(ctx, tx)
		if err != nil {
			return err
		}

		subject, err := gov.LockGovernanceSubjectByEmailTx(ctx, tx, email)
		if err != nil {
			if errors.Is(err, data.ErrUserNotFound) {
				if existing != nil {
					return ErrRootBootstrapConflict
				}
				return ErrRootBootstrapAccountNotFound
			}
			return err
		}

		if existing != nil {
			if *existing == subject.UserID {
				outcome = RootBootstrapAlreadyEstablished
				rootID = *existing
				return nil
			}
			return ErrRootBootstrapConflict
		}

		if subject.IsDeleted {
			return ErrRootBootstrapAccountNotFound
		}
		if !subject.IsActive {
			return ErrRootBootstrapAccountIneligible
		}

			if subject.Appointment != nil && subject.Appointment.Status == data.AppointmentStatusActive &&
			subject.Appointment.Level == data.AdministrativeLevelAdmin {
			adminRoleID, err := s.Models.Role.ResolveRoleIDByNameTx(ctx, tx, data.RoleNameAdmin)
			if err != nil {
				return err
			}
			if _, err := s.Models.Role.RevokeRoleAssignmentTx(ctx, tx, subject.UserID, adminRoleID, ""); err != nil {
				return err
			}
		}

		reason := "Root Super Admin bootstrap"
		if _, err := gov.PutAppointmentTx(ctx, tx, data.AppointmentChange{
			UserID:  subject.UserID,
			Level:   data.AdministrativeLevelSuperAdmin,
			Status:  data.AppointmentStatusActive,
			ActorID: nil,
			Reason:  &reason,
			NewTerm: true,
		}); err != nil {
			return err
		}

		superAdminRoleID, err := s.Models.Role.ResolveRoleIDByNameTx(ctx, tx, data.RoleNameSuperAdmin)
		if err != nil {
			return err
		}
		if err := s.Models.Role.AssignPrimaryRoleTx(ctx, tx, subject.UserID, superAdminRoleID, nil); err != nil {
			return err
		}

		if err := gov.InsertRootSuperAdminTx(ctx, tx, subject.UserID); err != nil {
			return err
		}

		if err := gov.InsertGovernanceAuditTx(ctx, tx, data.GovernanceAuditEntry{
			ActorID:    nil,
			Action:     "bootstrap_root_super_admin",
			EntityType: data.GovernanceEntityRootSuperAdmin,
			EntityID:   subject.UserID.String(),
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

		outcome = RootBootstrapEstablished
		rootID = subject.UserID
		return nil
	})
	if err != nil {
		if errors.Is(err, data.ErrRootSuperAdminAlreadyEstablished) {
			return "", uuid.Nil, ErrRootBootstrapConflict
		}
		return "", uuid.Nil, err
	}
	return outcome, rootID, nil
}

// ReconcileAdministrativeAppointmentsInternal brings role assignments made
// before administrative governance existed under the appointment model. It
// runs at every startup and is idempotent:
//
//   - an account whose effective (primary) role is admin or super_admin
//     receives an active appointment at that level;
//   - any other active governance-role assignment on that account, and every
//     governance-role assignment on a non-primary or deleted account,
//     conferred no authority and is revoked.
//
// Each reconciled account is audited.
func (s *Service) ReconcileAdministrativeAppointmentsInternal(ctx context.Context) (int, error) {
	if err := s.validate(); err != nil {
		return 0, err
	}
	ctx, cancel := context.WithTimeout(ctx, s.Cfg.DBTimeout)
	defer cancel()

	gov := s.governance()
	reconciled := 0

	err := s.withGovernanceTx(ctx, func(tx pgx.Tx) error {
		if err := gov.AcquireGovernanceAdvisoryLockTx(ctx, tx); err != nil {
			return err
		}
		assignments, err := s.Models.Role.ListAdministrativeAssignmentsWithoutAppointmentTx(ctx, tx)
		if err != nil {
			return err
		}

		byUser := make(map[uuid.UUID][]data.AdministrativeAssignment)
		order := make([]uuid.UUID, 0)
		for _, a := range assignments {
			if _, ok := byUser[a.UserID]; !ok {
				order = append(order, a.UserID)
			}
			byUser[a.UserID] = append(byUser[a.UserID], a)
		}

		for _, userID := range data.SortedGovernanceLockOrder(order...) {
			if _, err := gov.LockGovernanceSubjectsTx(ctx, tx, userID); err != nil {
				return err
			}
			var keep *data.AdministrativeAssignment
			for i := range byUser[userID] {
				a := byUser[userID][i]
				if keep == nil && a.IsPrimary && !a.UserDeleted {
					keep = &a
					continue
				}
				if _, err := s.Models.Role.RevokeRoleAssignmentTx(ctx, tx, userID, a.RoleID, ""); err != nil {
					return err
				}
			}

			auditContext := data.GovernanceAuditContext{Operation: "reconcile"}
			if keep != nil {
				reason := "Reconciled from a pre-governance role assignment"
				level := data.AdministrativeLevel(keep.RoleName)
				if _, err := gov.PutAppointmentTx(ctx, tx, data.AppointmentChange{
					UserID:  userID,
					Level:   level,
					Status:  data.AppointmentStatusActive,
					Reason:  &reason,
					NewTerm: true,
				}); err != nil {
					return err
				}
				auditContext.NewLevel = string(level)
				auditContext.NewStatus = string(data.AppointmentStatusActive)
				auditContext.Reason = reason
			} else {
				auditContext.Reason = "Removed a governance role that conferred no authority"
			}

			if err := gov.InsertGovernanceAuditTx(ctx, tx, data.GovernanceAuditEntry{
				Action:     "reconcile_administrator_appointment",
				EntityType: data.GovernanceEntityAdministrator,
				EntityID:   userID.String(),
				Outcome:    data.GovernanceOutcomeSucceeded,
				Context:    auditContext,
			}); err != nil {
				return err
			}
			reconciled++
		}
		return nil
	})
	return reconciled, err
}

// ---------------------------------------------------------------------------
// Account removal guards
// ---------------------------------------------------------------------------

// ensureNotAdministratorForSelfClosure refuses self-service closure while the
// account holds any administrative standing (or is the Root).
func (s *Service) ensureNotAdministratorForSelfClosure(ctx context.Context, userID uuid.UUID) error {
	subject, err := s.governance().GetGovernanceSubject(ctx, userID)
	if err != nil {
		if errors.Is(err, data.ErrUserNotFound) {
			return err
		}
		return fmt.Errorf("check administrative standing: %w", err)
	}
	if subject.IsRoot {
		return ErrGovernanceRootProtected
	}
	switch subject.AppointmentStatus() {
	case data.AppointmentStatusActive, data.AppointmentStatusSuspended:
		return ErrGovernanceAdministratorSelfClosure
	}
	return nil
}

// AuthorizeAccountStewardshipInternal checks, from current state, whether
// actor may moderate or edit target's profile.
func (s *Service) AuthorizeAccountStewardshipInternal(ctx context.Context, actorID, targetID uuid.UUID) error {
	if err := s.validate(); err != nil {
		return err
	}
	actor, err := s.readActor(ctx, actorID)
	if err != nil {
		return err
	}
	subject, err := s.governance().GetGovernanceSubject(ctx, targetID)
	if err != nil && !errors.Is(err, data.ErrUserNotFound) {
		return err
	}
	return AuthorizeAccountStewardship(actor, targetFromSubject(targetID, subject))
}

// ---------------------------------------------------------------------------
// Directory reads
// ---------------------------------------------------------------------------

// AdministratorView is one administrator with the caller's available actions.
type AdministratorView struct {
	*data.AdministratorRecord
	AvailableActions []string `json:"available_actions"`
}

// AccountView is one account with the caller's available actions.
type AccountView struct {
	*data.AccountRecord
	AvailableActions []string `json:"available_actions"`
}

// AdministratorDirectory is the administrators list with the caller's standing.
type AdministratorDirectory struct {
	Actor          AdministrativeStanding `json:"actor"`
	Administrators []AdministratorView     `json:"administrators"`
}

func targetFromAdministrator(r *data.AdministratorRecord) GovernanceTarget {
	return GovernanceTarget{
		UserID:   r.UserID,
		Exists:   true,
		IsActive: r.AccountActive,
		IsRoot:   r.IsRoot,
		Level:    r.Level,
		Status:   r.Status,
	}
}

func targetFromAccount(r *data.AccountRecord) GovernanceTarget {
	return GovernanceTarget{
		UserID:   r.UserID,
		Exists:   true,
		IsActive: r.AccountActive,
		IsRoot:   r.IsRoot,
		Level:    r.AdministrativeLevel,
		Status:   r.AppointmentStatus,
	}
}

// ListAdministratorsInternal returns the administrator directory.
func (s *Service) ListAdministratorsInternal(ctx context.Context, actorID uuid.UUID, includeRevoked bool) (*AdministratorDirectory, error) {
	if err := s.validate(); err != nil {
		return nil, err
	}
	actor, err := s.readActor(ctx, actorID)
	if err != nil {
		return nil, err
	}
	if actor.Level == data.AdministrativeLevelNone {
		return nil, ErrGovernanceInsufficientAuthority
	}
	records, err := s.governance().ListAdministrators(ctx, includeRevoked)
	if err != nil {
		return nil, err
	}
	views := make([]AdministratorView, 0, len(records))
	for _, r := range records {
		views = append(views, AdministratorView{
			AdministratorRecord: r,
			AvailableActions:    AvailableGovernanceActions(actor, targetFromAdministrator(r)),
		})
	}
	return &AdministratorDirectory{Actor: StandingFor(actor), Administrators: views}, nil
}

// SearchAccountsInternal returns one page of the account directory.
func (s *Service) SearchAccountsInternal(ctx context.Context, actorID uuid.UUID, query string, limit, offset int) ([]AccountView, error) {
	if err := s.validate(); err != nil {
		return nil, err
	}
	if utf8.RuneCountInString(query) > 254 {
		return nil, ErrGovernanceInputInvalid
	}
	actor, err := s.readActor(ctx, actorID)
	if err != nil {
		return nil, err
	}
	if actor.Level == data.AdministrativeLevelNone {
		return nil, ErrGovernanceInsufficientAuthority
	}
	records, err := s.governance().SearchAccounts(ctx, query, limit, offset)
	if err != nil {
		return nil, err
	}
	views := make([]AccountView, 0, len(records))
	for _, r := range records {
		views = append(views, AccountView{AccountRecord: r, AvailableActions: AvailableGovernanceActions(actor, targetFromAccount(r))})
	}
	return views, nil
}

// GetAccountInternal returns one account with available actions.
func (s *Service) GetAccountInternal(ctx context.Context, actorID, targetID uuid.UUID) (*AccountView, error) {
	if err := s.validate(); err != nil {
		return nil, err
	}
	actor, err := s.readActor(ctx, actorID)
	if err != nil {
		return nil, err
	}
	if actor.Level == data.AdministrativeLevelNone {
		return nil, ErrGovernanceInsufficientAuthority
	}
	record, err := s.governance().GetAccount(ctx, targetID)
	if err != nil {
		if errors.Is(err, data.ErrUserNotFound) {
			return nil, ErrGovernanceTargetNotFound
		}
		return nil, err
	}
	return &AccountView{AccountRecord: record, AvailableActions: AvailableGovernanceActions(actor, targetFromAccount(record))}, nil
}

// ListAdministratorHistoryInternal returns governance history for target.
func (s *Service) ListAdministratorHistoryInternal(ctx context.Context, actorID, targetID uuid.UUID, limit int) ([]*data.GovernanceHistoryEntry, error) {
	if err := s.validate(); err != nil {
		return nil, err
	}
	actor, err := s.readActor(ctx, actorID)
	if err != nil {
		return nil, err
	}
	if actor.Level == data.AdministrativeLevelNone {
		return nil, ErrGovernanceInsufficientAuthority
	}
	return s.governance().ListGovernanceHistory(ctx, targetID, limit)
}
