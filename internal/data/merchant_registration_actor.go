// Package data provides transaction-scoped principal and role resolution for merchant registration.
// focodebase/fobackend/internal/data/merchant_registration_actor.go
//
// GTM:
//
//	Layer: 2.1 Persistence / Merchant Registration
//	Release Class: SPINE
//	Reason: Supports the complete Foundation Level 4 merchant-registration and account-navigation vertical.
//
// SPINE Rule:
//
//	Keep compiling and preserve established security, lifecycle, and privacy contracts.
//	Preserve canonical principal ownership and backend authorization.
//	Keep private registration values out of public projections, logs, and errors.
package data

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// LockMerchantRegistrationActorTx serializes writes for a principal before a
// draft exists, and prevents deactivation/deletion until commit.
func(m *UserModel) LockMerchantRegistrationActorTx(ctx context.Context,tx pgx.Tx,id uuid.UUID)(bool,error) {
 var active bool
 err:=tx.QueryRow(ctx,`SELECT is_active FROM users WHERE id=$1 AND deleted_at IS NULL FOR UPDATE`,id).Scan(&active)
 if errors.Is(err,pgx.ErrNoRows) { return false,nil }
 if err!=nil { return false,SafeMerchantRegistrationError("lock actor",err) }
 return active,nil
}

// MerchantRegistrationRoleTx resolves the same effective role ordering as
// GetRoleByUserID, using the caller's transaction and holding assignment and
// role share locks through commit. Role SQL remains owned by RoleModel.
func(m *RoleModel) MerchantRegistrationRoleTx(ctx context.Context,tx pgx.Tx,id uuid.UUID)(string,error) {
 var name string
 err:=tx.QueryRow(ctx,`SELECT r.name::text FROM user_role_assignments ura
 JOIN roles r ON r.id=ura.role_id
 WHERE ura.user_id=$1 AND ura.deleted_at IS NULL AND r.deleted_at IS NULL AND r.is_active
 ORDER BY ura.is_primary DESC,r.hierarchy_level DESC,r.name ASC LIMIT 1
 FOR SHARE OF ura,r`,id).Scan(&name)
 if errors.Is(err,pgx.ErrNoRows) { return "",nil }
 if err!=nil { return "",SafeMerchantRegistrationError("resolve role",err) }
 return name,nil
}

func(m *RoleModel) MerchantRegistrationRoleSnapshotTx(ctx context.Context,tx pgx.Tx,id uuid.UUID)(string,error) {
 var name string
 err:=tx.QueryRow(ctx,`SELECT r.name::text FROM user_role_assignments ura JOIN roles r ON r.id=ura.role_id
 WHERE ura.user_id=$1 AND ura.deleted_at IS NULL AND r.deleted_at IS NULL AND r.is_active
 ORDER BY ura.is_primary DESC,r.hierarchy_level DESC,r.name ASC LIMIT 1`,id).Scan(&name)
 if errors.Is(err,pgx.ErrNoRows){return "",nil}
 if err!=nil{return "",SafeMerchantRegistrationError("read role",err)}
 return name,nil
}
func(m *MerchantAccountModel) ReadRegistrationAccountTx(ctx context.Context,tx pgx.Tx,id uuid.UUID)(*MerchantAccount,error) {
 a:=new(MerchantAccount)
 err:=scanMerchantAccount(tx.QueryRow(ctx,`SELECT `+merchantAccountSelectColumns+` FROM merchant_accounts
 WHERE principal_user_id=$1 AND deleted_at IS NULL`,id),a)
 if errors.Is(err,pgx.ErrNoRows){return nil,nil}
 if err!=nil{return nil,SafeMerchantRegistrationError("read account",err)}
 return a,nil
}
func(m *MerchantModel) ReadRegistrationMerchantTx(ctx context.Context,tx pgx.Tx,id uuid.UUID)(*Merchant,error) {
 merchant:=new(Merchant)
 err:=scanMerchant(tx.QueryRow(ctx,`SELECT `+merchantSelectColumns+` FROM merchants WHERE id=$1`,id),merchant)
 if errors.Is(err,pgx.ErrNoRows){return nil,nil}
 if err!=nil{return nil,SafeMerchantRegistrationError("read merchant",err)}
 return merchant,nil
}
