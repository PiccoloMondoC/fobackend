// Package services owns the CE maintenance state machine and authority boundary.
// Lock order: governance advisory mutex -> actor users row -> maintenance row
// -> transition evidence -> success audit. No password hashing under locks.
//
// focodebase/fobackend/internal/services/site_maintenance_internal.go
package services

import (
 "context"
 "errors"
 "fmt"
 "strings"
 "time"
 "unicode"
 "unicode/utf8"

 "github.com/PiccoloMondoC/focodebase/fobackend/internal/data"
 "github.com/PiccoloMondoC/focodebase/fobackend/internal/security"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
)

const PermissionReadMaintenance="read_site_maintenance"
const PermissionManageMaintenance="manage_site_maintenance"
var ErrMaintenanceInput=errors.New("invalid maintenance request")
var ErrMaintenanceTransition=errors.New("invalid maintenance transition")

type MaintenanceCommand struct {
 ActorID uuid.UUID `json:"-"`
 Operation string `json:"-"`
 ExpectedVersion int64 `json:"expected_version"`
 StartsAt time.Time `json:"starts_at"`
 DurationMinutes int `json:"duration_minutes"`
 Message string `json:"message"`
 LinkAnnouncement bool `json:"link_announcement"`
 RetireAnnouncement bool `json:"retire_announcement"`
 Reason string `json:"reason"`
 CurrentPassword string `json:"current_password"`
}
type MaintenanceWorkspace struct {
 State *data.MaintenanceState `json:"state"`
 History []data.MaintenanceEvent `json:"history"`
}
func maintenanceText(value string,max int,required bool) bool {
 if !utf8.ValidString(value) || value!=strings.TrimSpace(value) || utf8.RuneCountInString(value)>max || (required && value=="") {return false}
 for _,r:=range value {if unicode.IsControl(r){return false}}
 return true
}
func ValidateMaintenanceCommand(c MaintenanceCommand) error {
 if c.ActorID==uuid.Nil || c.ExpectedVersion<1 || !maintenanceText(c.Reason,500,false){return ErrMaintenanceInput}
 switch c.Operation {
 case "save":
  if c.StartsAt.IsZero() || c.StartsAt.Year()<1 || c.StartsAt.Year()>9998 || c.DurationMinutes<5 || c.DurationMinutes>10080 || !maintenanceText(c.Message,280,true){return ErrMaintenanceInput}
 case "cancel":
 case "activate":if !maintenanceText(c.Message,280,false){return ErrMaintenanceInput}
 case "restore":
 default:return ErrMaintenanceInput
 }
 return nil
}
func AuthorizeMaintenance(actor GovernanceActor) error {
 if actor.UserID==uuid.Nil || actor.Level!=data.AdministrativeLevelSuperAdmin || !actor.IsRoot {return ErrGovernanceInsufficientAuthority}
 return nil
}
// PlanMaintenance is deterministic. It copies the unowned aspect unchanged;
// the only combined transition is restore with explicit retire_announcement.
func PlanMaintenance(s *data.MaintenanceState,c MaintenanceCommand,id uuid.UUID) (data.MaintenanceDocument,string,error) {
 if err:=ValidateMaintenanceCommand(c);err!=nil{return data.MaintenanceDocument{},"",err}
 if s==nil{return data.MaintenanceDocument{},"",data.ErrMaintenanceUnavailable}
 if c.ExpectedVersion!=s.Version{return data.MaintenanceDocument{},"",data.ErrMaintenanceConflict}
 next:=s.MaintenanceDocument
 switch c.Operation {
 case "save":
  start:=c.StartsAt.UTC().Truncate(time.Second)
  if id==uuid.Nil || start.After(s.ObservedAt.Add(366*24*time.Hour)) || !start.Add(time.Duration(c.DurationMinutes)*time.Minute).After(s.ObservedAt.Add(time.Minute)){return next,"",ErrMaintenanceInput}
  next.Announcement=&data.MaintenanceAnnouncement{ID:id,StartsAt:start,DurationMinutes:c.DurationMinutes,Message:c.Message}
  op:="publish";if s.Announcement!=nil{op="update"};return next,op,nil
 case "cancel":
  if s.Announcement==nil{return next,"",ErrMaintenanceTransition};next.Announcement=nil
 case "activate":
  if s.Active{return next,"",ErrMaintenanceTransition}
  now:=s.ObservedAt.UTC();next.Active=true;next.ActiveSince=&now;next.Message=nil;next.ExpectedEndAt=nil
  if c.Message!=""{v:=c.Message;next.Message=&v}
  if c.LinkAnnouncement {
   a:=s.Announcement;if a==nil{return next,"",ErrMaintenanceTransition}
   if next.Message==nil{v:=a.Message;next.Message=&v}
   end:=a.StartsAt.Add(time.Duration(a.DurationMinutes)*time.Minute)
   if end.After(now){next.ExpectedEndAt=&end}
  }
 case "restore":
  if !s.Active{return next,"",ErrMaintenanceTransition}
  next.Active=false;next.ActiveSince=nil;next.Message=nil;next.ExpectedEndAt=nil
  if c.RetireAnnouncement{next.Announcement=nil}
 }
 return next,c.Operation,nil
}
func MaintenanceErrorCode(err error) string {
 switch {
 case errors.Is(err,ErrMaintenanceInput),errors.Is(err,ErrGovernanceInputInvalid):return "invalid_request"
 case errors.Is(err,ErrGovernanceInsufficientAuthority),errors.Is(err,data.ErrAdministrativePermissionDenied):return "insufficient_authority"
 case errors.Is(err,ErrGovernanceReauthenticationFailed):return "reauthentication_failed"
 case errors.Is(err,ErrGovernanceReauthenticationUnavailable):return "reauthentication_unavailable"
 case errors.Is(err,data.ErrMaintenanceConflict):return "maintenance_state_changed"
 case errors.Is(err,ErrMaintenanceTransition):return "invalid_transition"
 case errors.Is(err,data.ErrConcurrentGovernanceChange):return "concurrent_change"
 default:return "outcome_unconfirmed"
 }
}
func (s *Service) maintenanceReady() error {
 if err:=s.validate();err!=nil{return err}
 if s.maintenance==nil || s.maintenance.DB!=s.Models.DB{return fmt.Errorf("%w: maintenance model must use canonical database",ErrInvalidServiceConfiguration)}
 return nil
}
// InitializeMaintenanceInternal fails startup on a missing/corrupt singleton or
// absent audit/permission metadata. It never changes availability.
func (s *Service) InitializeMaintenanceInternal(ctx context.Context) error {
 if ctx==nil{return ErrNilContext};if err:=s.maintenanceReady();err!=nil{return err}
 ctx,cancel:=context.WithTimeout(ctx,s.Cfg.DBTimeout);defer cancel()
 if _,err:=s.maintenance.Read(ctx);err!=nil{return err}
 var count int
 if err:=s.Models.DB.QueryRow(ctx,`SELECT count(*) FROM actions WHERE name IN ('site_maintenance_publish','site_maintenance_update','site_maintenance_cancel','site_maintenance_activate','site_maintenance_restore','site_maintenance_engineering_restore','site_maintenance_read')`).Scan(&count);err!=nil{return err}
 if count!=7{return errors.New("maintenance audit metadata missing")}
 var ready bool
 err:=s.Models.DB.QueryRow(ctx,`SELECT EXISTS(SELECT 1 FROM entity_types WHERE name='site_maintenance') AND (SELECT count(*) FROM permissions WHERE name IN ('read_site_maintenance','manage_site_maintenance'))=2`).Scan(&ready)
 if err!=nil{return err};if !ready{return errors.New("maintenance authorization metadata missing")};return nil
}
// PublicMaintenanceInternal is the one uncached availability observation used by
// the API gate and public endpoint. Read failure never means online or maintenance.
func (s *Service) PublicMaintenanceInternal(ctx context.Context) (*data.MaintenanceState,error) {
 if ctx==nil{return nil,ErrNilContext};if err:=s.maintenanceReady();err!=nil{return nil,err}
 ctx,cancel:=context.WithTimeout(ctx,s.Cfg.DBTimeout);defer cancel();return s.maintenance.Read(ctx)
}
func (s *Service) maintenanceActorTx(ctx context.Context,tx pgx.Tx,id uuid.UUID,permission string) (*data.GovernanceSubject,error) {
 subjects,err:=s.governance().LockGovernanceSubjectsTx(ctx,tx,id);if err!=nil{return nil,err}
 subject:=subjects[id];actor:=actorFromSubject(subject);actor.UserID=id
 if err:=AuthorizeMaintenance(actor);err!=nil{return nil,err}
 if err:=s.governance().RequirePermissionTx(ctx,tx,id,permission);err!=nil{return nil,err}
 return subject,nil
}
func (s *Service) maintenanceViewTx(ctx context.Context,tx pgx.Tx) (*MaintenanceWorkspace,error) {
 state,err:=s.maintenance.ReadTx(ctx,tx,false);if err!=nil{return nil,err}
 history,err:=s.maintenance.History(ctx,tx);if err!=nil{return nil,err}
 return &MaintenanceWorkspace{State:state,History:history},nil
}
func (s *Service) maintenanceDenial(ctx context.Context,id uuid.UUID,op string,cause error) {
 code:=MaintenanceErrorCode(cause)
 if id==uuid.Nil || (code!="insufficient_authority" && code!="reauthentication_failed" && code!="reauthentication_unavailable"){return}
 if op=="save"{op="publish"}
 auditCtx,cancel:=context.WithTimeout(context.WithoutCancel(ctx),s.Cfg.DBTimeout);defer cancel()
 err:=s.governance().InsertGovernanceAuditTx(auditCtx,s.Models.DB,data.GovernanceAuditEntry{ActorID:&id,Action:data.MaintenanceActionPrefix+op,EntityType:data.MaintenanceEntity,EntityID:"denied",Outcome:data.GovernanceOutcomeDenied,Context:data.GovernanceAuditContext{Operation:op,DenialCode:code,Mechanism:"administrative"}})
 if err!=nil{s.Logger.Warn("maintenance denial audit failed","operation",op,"code",code)}
}
func (s *Service) ReadMaintenanceInternal(ctx context.Context,id uuid.UUID) (*MaintenanceWorkspace,error) {
 if ctx==nil{return nil,ErrNilContext};if err:=s.maintenanceReady();err!=nil{return nil,err};if id==uuid.Nil{return nil,ErrGovernanceInputInvalid}
 ctx,cancel:=context.WithTimeout(ctx,s.Cfg.DBTimeout);defer cancel()
 var view *MaintenanceWorkspace
 err:=s.withGovernanceTx(ctx,func(tx pgx.Tx) error {
  if _,err:=s.maintenanceActorTx(ctx,tx,id,PermissionReadMaintenance);err!=nil{return err}
  var err error;view,err=s.maintenanceViewTx(ctx,tx);return err
 })
 if err!=nil{s.maintenanceDenial(ctx,id,"read",err);return nil,err};return view,nil
}
func (s *Service) ExecuteMaintenanceInternal(ctx context.Context,c MaintenanceCommand) (*MaintenanceWorkspace,error) {
 if ctx==nil{return nil,ErrNilContext};if err:=s.maintenanceReady();err!=nil{return nil,err}
 if err:=ValidateMaintenanceCommand(c);err!=nil{return nil,err}
 ctx,cancel:=context.WithTimeout(ctx,s.Cfg.DBTimeout);defer cancel()
 stepUp:=c.Operation=="activate" || c.Operation=="restore"
 var hash string
 if stepUp {
  if len(c.CurrentPassword)<1 || len(c.CurrentPassword)>72{return nil,ErrMaintenanceInput}
  subject,err:=s.governance().GetGovernanceSubject(ctx,c.ActorID)
  if err!=nil && !errors.Is(err,data.ErrUserNotFound){return nil,err}
  actor:=actorFromSubject(subject);actor.UserID=c.ActorID
  if err:=AuthorizeMaintenance(actor);err!=nil{s.maintenanceDenial(ctx,c.ActorID,c.Operation,err);return nil,err}
  hash=subject.PasswordHash
  var proofErr error
  if hash==""{proofErr=ErrGovernanceReauthenticationUnavailable}else if security.VerifyPassword(c.CurrentPassword,hash)!=nil{proofErr=ErrGovernanceReauthenticationFailed}
  c.CurrentPassword=""
  if proofErr!=nil{s.maintenanceDenial(ctx,c.ActorID,c.Operation,proofErr);return nil,proofErr}
 }
 c.CurrentPassword="" // never included in a persisted document or audit
 var view *MaintenanceWorkspace
 err:=s.withGovernanceTx(ctx,func(tx pgx.Tx) error {
  subject,err:=s.maintenanceActorTx(ctx,tx,c.ActorID,PermissionManageMaintenance);if err!=nil{return err}
  if stepUp && subject.PasswordHash!=hash{return ErrGovernanceReauthenticationFailed}
  current,err:=s.maintenance.ReadTx(ctx,tx,true);if err!=nil{return err}
  next,op,err:=PlanMaintenance(current,c,uuid.New());if err!=nil{return err}
  event,err:=s.maintenance.WriteTx(ctx,tx,current,next,op,"administrative",&c.ActorID,c.Reason,nil);if err!=nil{return err}
  from,to:="normal","normal";if current.Active{from="active"};if next.Active{to="active"}
  if err:=s.governance().InsertGovernanceAuditTx(ctx,tx,data.GovernanceAuditEntry{ActorID:&c.ActorID,Action:data.MaintenanceActionPrefix+op,EntityType:data.MaintenanceEntity,EntityID:event.String(),Outcome:data.GovernanceOutcomeSucceeded,Context:data.GovernanceAuditContext{Operation:op,PreviousStatus:from,NewStatus:to,Reason:c.Reason,Mechanism:"administrative"}});err!=nil{return err}
  view,err=s.maintenanceViewTx(ctx,tx);return err
 })
 if err!=nil{s.maintenanceDenial(ctx,c.ActorID,c.Operation,err);return nil,err};return view,nil
}
