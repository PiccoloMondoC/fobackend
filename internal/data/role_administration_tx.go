// focodebase/fobackend/internal/data/role_administration_tx.go
// CE authoritative role catalog/assignment transaction primitives.
package data

import (
 "context"
 "errors"
 "strings"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
)

func validGovernedRole(r *Role) error {
 r.Name=normalizeRoleName(r.Name);r.Description=strings.TrimSpace(r.Description)
 if IsAdministrativeRoleName(r.Name){return ErrGovernanceRoleProtected}
 if r.Name=="" || r.Description=="" || r.HierarchyLevel<0 || (r.IsInternal && r.AssignableAtSignup){return ErrRoleInvalidInput}
 return nil
}
func (m *RoleModel) CreateRoleTx(ctx context.Context,tx pgx.Tx,r *Role) error {
 if r==nil{return ErrRoleInvalidInput};if err:=validGovernedRole(r);err!=nil{return err}
 err:=scanRole(tx.QueryRow(ctx,`INSERT INTO roles(name,description,hierarchy_level,is_internal,assignable_at_signup,approval_required)
 VALUES($1,$2,$3,$4,$5,$6) RETURNING `+roleSelectColumns(),r.Name,r.Description,r.HierarchyLevel,r.IsInternal,r.AssignableAtSignup,r.ApprovalRequired),r)
 if IsUniqueViolation(err){return ErrRoleNameTaken};return err
}
func (m *RoleModel) LockRoleTx(ctx context.Context,tx pgx.Tx,id uuid.UUID) (*Role,error) {
 var role Role
 err:=scanRole(tx.QueryRow(ctx,`SELECT `+roleSelectColumns()+` FROM roles WHERE id=$1 AND deleted_at IS NULL FOR UPDATE`,id),&role)
 if errors.Is(err,pgx.ErrNoRows){return nil,ErrRoleNotFound};if err!=nil{return nil,err};return &role,nil
}
func (m *RoleModel) UpdateRolePartialTx(ctx context.Context,tx pgx.Tx,id uuid.UUID,patch RolePatch) (*Role,error) {
 r,err:=m.LockRoleTx(ctx,tx,id);if err!=nil{return nil,err}
 if IsAdministrativeRoleName(r.Name){return nil,ErrGovernanceRoleProtected}
 if patch.Name!=nil{r.Name=*patch.Name};if patch.Description!=nil{r.Description=*patch.Description}
 if patch.HierarchyLevel!=nil{r.HierarchyLevel=*patch.HierarchyLevel};if patch.IsInternal!=nil{r.IsInternal=*patch.IsInternal}
 if patch.AssignableAtSignup!=nil{r.AssignableAtSignup=*patch.AssignableAtSignup};if patch.ApprovalRequired!=nil{r.ApprovalRequired=*patch.ApprovalRequired}
 if patch.IsActive!=nil{r.IsActive=*patch.IsActive}
 if err:=validGovernedRole(r);err!=nil{return nil,err}
 if !r.IsActive {
  var inUse bool
  if err:=tx.QueryRow(ctx,`SELECT EXISTS(SELECT 1 FROM user_role_assignments WHERE role_id=$1 AND deleted_at IS NULL)`,id).Scan(&inUse);err!=nil{return nil,err}
  if inUse{return nil,ErrRoleInUse}
 }
 err=scanRole(tx.QueryRow(ctx,`UPDATE roles SET name=$2,description=$3,hierarchy_level=$4,is_internal=$5,
 assignable_at_signup=$6,approval_required=$7,is_active=$8 WHERE id=$1 RETURNING `+roleSelectColumns(),id,r.Name,r.Description,r.HierarchyLevel,r.IsInternal,r.AssignableAtSignup,r.ApprovalRequired,r.IsActive),r)
 if IsUniqueViolation(err){return nil,ErrRoleNameTaken};if err!=nil{return nil,TranslateGovernanceError(err)};return r,nil
}
func (m *RoleModel) SoftDeleteUnusedTx(ctx context.Context,tx pgx.Tx,id uuid.UUID) error {
 role,err:=m.LockRoleTx(ctx,tx,id);if err!=nil{return err}
 if IsAdministrativeRoleName(role.Name){return ErrGovernanceRoleProtected}
 var inUse bool
 if err:=tx.QueryRow(ctx,`SELECT EXISTS(SELECT 1 FROM user_role_assignments WHERE role_id=$1 AND deleted_at IS NULL)`,id).Scan(&inUse);err!=nil{return err}
 if inUse{return ErrRoleInUse}
 _,err=tx.Exec(ctx,`UPDATE roles SET is_active=FALSE,deleted_at=NOW() WHERE id=$1`,id);return err
}

func (m *RoleModel) AssignNonAdministrativeRoleTx(ctx context.Context,tx pgx.Tx,userID,roleID,actorID uuid.UUID,primary bool) error {
 role,err:=m.LockRoleTx(ctx,tx,roleID);if err!=nil{return err}
 if IsAdministrativeRoleName(role.Name){return ErrGovernanceRoleProtected};if !role.IsActive{return ErrRoleInactive}
 if primary{return m.AssignPrimaryRoleTx(ctx,tx,userID,roleID,&actorID)}
 // Preserve an existing primary bit when adding an already-held secondary
 // role; an idempotent assignment must not strip the account's primary role.
 _,err=tx.Exec(ctx,`INSERT INTO user_role_assignments(user_id,role_id,assigned_by,is_primary)
 VALUES($1,$2,$3,FALSE) ON CONFLICT(user_id,role_id) DO UPDATE SET deleted_at=NULL,
 assigned_by=EXCLUDED.assigned_by,is_primary=CASE WHEN user_role_assignments.deleted_at IS NULL THEN user_role_assignments.is_primary ELSE FALSE END`,userID,roleID,actorID)
 return TranslateGovernanceError(err)
}
func (m *RoleModel) RevokeNonAdministrativeRoleTx(ctx context.Context,tx pgx.Tx,userID,roleID uuid.UUID) error {
 role,err:=m.LockRoleTx(ctx,tx,roleID);if err!=nil{return err}
 if IsAdministrativeRoleName(role.Name){return ErrGovernanceRoleProtected}
 found,err:=m.RevokeRoleAssignmentTx(ctx,tx,userID,roleID,"");if err!=nil{return err};if !found{return ErrRoleNotFound}
 var count int
 if err:=tx.QueryRow(ctx,`SELECT COUNT(*) FROM user_role_assignments ura JOIN roles r ON r.id=ura.role_id
 WHERE ura.user_id=$1 AND ura.deleted_at IS NULL AND r.deleted_at IS NULL AND r.is_active`,userID).Scan(&count);err!=nil{return err}
 if count==0{return ErrLastActiveRole};return nil
}
func (m *RolePermissionModel) ReplacePermissionsForRoleTx(ctx context.Context,tx pgx.Tx,roleID uuid.UUID,ids []uuid.UUID) error {
 var roleName string
 if err:=tx.QueryRow(ctx,`SELECT name FROM roles WHERE id=$1 AND deleted_at IS NULL FOR UPDATE`,roleID).Scan(&roleName);err!=nil {
  if errors.Is(err,pgx.ErrNoRows){return ErrRoleNotFound};return err
 }
 if IsAdministrativeRoleName(roleName){return ErrGovernanceRoleProtected}
 if ids==nil{ids=[]uuid.UUID{}}
 if _,err:=tx.Exec(ctx,`DELETE FROM role_permissions WHERE role_id=$1 AND NOT(permission_id=ANY($2::uuid[]))`,roleID,ids);err!=nil{return err}
 _,err:=tx.Exec(ctx,`INSERT INTO role_permissions(role_id,permission_id) SELECT $1,x FROM unnest($2::uuid[]) AS x
 ON CONFLICT(role_id,permission_id) DO NOTHING`,roleID,ids)
 if IsForeignKeyViolation(err){return ErrPermissionNotFound};return err
}
