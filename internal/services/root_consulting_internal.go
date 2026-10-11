// focodebase/fobackend/internal/services/root_consulting_internal.go
// Time-bound, read-only consulting authorization. No blanket Admin role grant.
package services
import (
 "context"
 "encoding/json"
 "github.com/PiccoloMondoC/focodebase/fobackend/internal/data"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
)
func (s *Service) ReadConsultingInternal(ctx context.Context,actor uuid.UUID,capability string,target uuid.UUID) (json.RawMessage,error) {
 if err:=s.validate();err!=nil{return nil,err};if ctx==nil{return nil,ErrNilContext}
 ctx,cancel:=context.WithTimeout(ctx,s.Cfg.DBTimeout);defer cancel()
 var output json.RawMessage
 err:=s.withGovernanceTx(ctx,func(tx pgx.Tx)error {
  subjects,err:=s.governance().LockGovernanceSubjectsTx(ctx,tx,actor);if err!=nil{return err}
  subject:=subjects[actor];if subject==nil || !subject.IsActive || subject.IsDeleted{return data.ErrContinuityForbidden}
  if capability=="" {
   return tx.QueryRow(ctx,`SELECT COALESCE(jsonb_agg(jsonb_build_object('id',id,'issue_reference',issue_reference,'capabilities',capabilities,'expires_at',expires_at) ORDER BY expires_at),'[]'::jsonb) FROM root_consultant_grants WHERE user_id=$1 AND revoked_at IS NULL AND expires_at>clock_timestamp()`,actor).Scan(&output)
  }
  if capability!="read_continuity" && capability!="read_administrators" && capability!="read_governance_history"{return ErrGovernanceInputInvalid}
  var allowed bool;if err:=tx.QueryRow(ctx,`SELECT EXISTS(SELECT 1 FROM root_consultant_grants WHERE user_id=$1 AND revoked_at IS NULL AND expires_at>clock_timestamp() AND $2=ANY(capabilities))`,actor,capability).Scan(&allowed);err!=nil{return err};if !allowed{return data.ErrContinuityForbidden}
  switch capability {
  case "read_continuity":output,err=data.ReadContinuityStateTx(ctx,tx);return err
  case "read_administrators":return tx.QueryRow(ctx,`SELECT COALESCE(jsonb_agg(to_jsonb(a)),'[]'::jsonb) FROM (SELECT aa.user_id,u.email::text,aa.level,aa.status FROM administrative_appointments aa JOIN users u ON u.id=aa.user_id ORDER BY u.email LIMIT 500) a`).Scan(&output)
  default:
   if target==uuid.Nil{return ErrGovernanceInputInvalid}
   return tx.QueryRow(ctx,`SELECT COALESCE(jsonb_agg(to_jsonb(h)),'[]'::jsonb) FROM (SELECT al.occurred_at,a.name AS action,al.outcome,al.context FROM audit_logs al JOIN actions a ON a.id=al.action_id JOIN entity_types et ON et.id=al.entity_type_id WHERE et.name IN ('administrator','root_super_admin') AND al.entity_id=$1 ORDER BY al.occurred_at DESC LIMIT 100) h`,target.String()).Scan(&output)
  }
 });return output,err
}
