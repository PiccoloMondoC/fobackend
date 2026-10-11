// focodebase/fobackend/internal/data/root_continuity.go
// Shared persistence primitives; authority decisions belong to services or the
// isolated recovery operator. Callers acquire governance mutex then user locks.
package data

import (
 "context"
 "encoding/json"
 "errors"
 "fmt"
 "time"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
)

var ErrContinuityConflict = errors.New("continuity state changed or operation unavailable")
var ErrContinuityForbidden = errors.New("continuity authority required")

type ContinuitySnapshot struct {
 State json.RawMessage `json:"state"`
 ActorRole string `json:"actor_role"`
 History []ContinuityEvent `json:"history"`
}
type ContinuityEvent struct {
 ID uuid.UUID `json:"id"`
 Operation string `json:"operation"`
 ActorID *uuid.UUID `json:"actor_id"`
 TargetID *uuid.UUID `json:"target_id"`
 Evidence json.RawMessage `json:"evidence"`
 OccurredAt time.Time `json:"occurred_at"`
}
func ReadContinuityStateTx(ctx context.Context, tx pgx.Tx) (json.RawMessage,error) {
 var state json.RawMessage
 err:=tx.QueryRow(ctx,`SELECT public.root_continuity_state()`).Scan(&state)
 return state,err
}
func SetContinuityOperationTx(ctx context.Context,tx pgx.Tx,op string) error {
 _,err:=tx.Exec(ctx,`SELECT set_config('sagrenti.continuity_operation',$1,true)`,op);return err
}
// ContinuityParticipantIDsTx runs under the mutex, before any participant lock.
func ContinuityParticipantIDsTx(ctx context.Context,tx pgx.Tx,extra ...uuid.UUID) ([]uuid.UUID,error) {
 rows,err:=tx.Query(ctx,`SELECT user_id FROM root_super_admin UNION SELECT user_id FROM root_co_holder UNION SELECT user_id FROM root_successor_designations`)
 if err!=nil{return nil,err};defer rows.Close()
 ids:=append([]uuid.UUID{},extra...)
 for rows.Next(){var id uuid.UUID;if err:=rows.Scan(&id);err!=nil{return nil,err};ids=append(ids,id)}
 return SortedGovernanceLockOrder(ids...),rows.Err()
}
func RecordContinuityEventTx(ctx context.Context,tx pgx.Tx,gov *AdministrativeGovernanceModel,id uuid.UUID,op string,actor,target *uuid.UUID,before json.RawMessage,evidence any) error {
 after,err:=ReadContinuityStateTx(ctx,tx);if err!=nil{return err}
 payload,err:=json.Marshal(evidence);if err!=nil{return err}
 _,err=tx.Exec(ctx,`INSERT INTO root_continuity_events(id,operation,actor_id,target_id,before_state,after_state,evidence) VALUES($1,$2,$3,$4,$5::jsonb,$6::jsonb,$7::jsonb)`,id,op,actor,target,string(before),string(after),string(payload))
 if err!=nil{return err}
 return gov.InsertGovernanceAuditTx(ctx,tx,GovernanceAuditEntry{ActorID:actor,Action:"root_continuity",EntityType:GovernanceEntityRootSuperAdmin,EntityID:id.String(),Outcome:GovernanceOutcomeSucceeded,Context:GovernanceAuditContext{Operation:op}})
}
// RepackSuccessorsTx preserves relative order and removes rank gaps. The unique
// rank constraint is deferred, permitting both this update and permutations.
func RepackSuccessorsTx(ctx context.Context,tx pgx.Tx) error {
 _,err:=tx.Exec(ctx,`WITH ordered AS(SELECT user_id,row_number() OVER(ORDER BY rank) AS n FROM root_successor_designations) UPDATE root_successor_designations d SET rank=o.n FROM ordered o WHERE d.user_id=o.user_id`);return err
}
// RevokeAdministrativeAccessTx preserves the public account and all provenance.
// The Root pointer and continuity standings MUST move first. Public auth uses
// live role checks, so old access JWTs cannot retain administrative permissions.
func RevokeAdministrativeAccessTx(ctx context.Context,tx pgx.Tx,gov *AdministrativeGovernanceModel,roles *RoleModel,id uuid.UUID,actor *uuid.UUID,level string) error {
 if level!="consumer" && level!="admin" && level!="super_admin" {return errors.New("invalid predecessor disposition")}
 if level=="consumer" {
  if _,err:=tx.Exec(ctx,`UPDATE administrative_appointments SET status='revoked',status_changed_by=$2,status_changed_at=clock_timestamp(),status_reason='Root continuity ceremony' WHERE user_id=$1`,id,actor);err!=nil{return err}
 } else {
  reason:="Root continuity ceremony"
  if _,err:=gov.PutAppointmentTx(ctx,tx,AppointmentChange{UserID:id,Level:AdministrativeLevel(level),Status:AppointmentStatusActive,ActorID:actor,Reason:&reason});err!=nil{return err}
 }
 for _,name:=range []string{RoleNameAdmin,RoleNameSuperAdmin} {
  if name==level{continue}
  roleID,err:=roles.ResolveRoleIDByNameTx(ctx,tx,name);if err!=nil{return err}
  if _,err:=roles.RevokeRoleAssignmentTx(ctx,tx,id,roleID,RoleNameConsumer);err!=nil{return err}
 }
 roleID,err:=roles.ResolveRoleIDByNameTx(ctx,tx,level);if err!=nil{return err}
 if err:=roles.AssignPrimaryRoleTx(ctx,tx,id,roleID,actor);err!=nil{return err}
 _,err=tx.Exec(ctx,`UPDATE refresh_tokens SET revoked_at=clock_timestamp() WHERE user_id=$1 AND revoked_at IS NULL`,id)
 return err
}
// AdvanceRootTx writes to the original ledger and then advances its protected
// pointer. Evidence is inserted in the same transaction by the ceremony owner.
func AdvanceRootTx(ctx context.Context,tx pgx.Tx,predecessor,successor uuid.UUID,generation int,mechanism string,authorization *string) (uuid.UUID,error) {
 id:=uuid.New()
 if _,err:=tx.Exec(ctx,`INSERT INTO root_succession_events(id,succession_generation,predecessor_user_id,successor_user_id,mechanism,authorization_reference) VALUES($1,$2,$3,$4,$5,$6)`,id,generation+1,predecessor,successor,mechanism,authorization);err!=nil{return uuid.Nil,err}
 tag,err:=tx.Exec(ctx,`UPDATE root_super_admin SET user_id=$1,succession_generation=$2 WHERE singleton AND user_id=$3 AND succession_generation=$4`,successor,generation+1,predecessor,generation)
 if err!=nil{return uuid.Nil,err};if tag.RowsAffected()!=1{return uuid.Nil,ErrContinuityConflict};return id,nil
}
func InsertHonoraryStandingTx(ctx context.Context,tx pgx.Tx,id uuid.UUID,generation int) error {
 _,err:=tx.Exec(ctx,`INSERT INTO root_honorary_standing(user_id,succession_generation) VALUES($1,$2) ON CONFLICT(user_id) DO NOTHING`,id,generation);return err
}
func ReadRootTx(ctx context.Context,tx pgx.Tx) (uuid.UUID,int,error) {
 var id uuid.UUID;var generation int
 err:=tx.QueryRow(ctx,`SELECT user_id,succession_generation FROM root_super_admin WHERE singleton`).Scan(&id,&generation)
 if errors.Is(err,pgx.ErrNoRows){return id,0,ErrContinuityConflict};return id,generation,err
}
func ReadCoRootTx(ctx context.Context,tx pgx.Tx) (*uuid.UUID,error) {
 var id uuid.UUID;err:=tx.QueryRow(ctx,`SELECT user_id FROM root_co_holder WHERE singleton`).Scan(&id)
 if errors.Is(err,pgx.ErrNoRows){return nil,nil};if err!=nil{return nil,err};return &id,nil
}
func EnsureContinuitySubject(s *GovernanceSubject) error {
 if s==nil || s.IsDeleted || !s.IsActive || s.ActiveLevel()!=AdministrativeLevelSuperAdmin || s.PasswordHash=="" {return fmt.Errorf("%w: a live password-enabled Super Admin is required",ErrContinuityConflict)};return nil
}
