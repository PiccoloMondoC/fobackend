// Package data provides private principal-owned registration persistence and revisioned lifecycle transitions.
// focodebase/fobackend/internal/data/merchant_registrations.go
//
// GTM:
//     Layer: 2.1 Persistence / Merchant Registration
//     Release Class: SPINE
//     Reason: Supports the complete Foundation Level 4 merchant-registration and account-navigation vertical.
//
// SPINE Rule:
//     Keep compiling and preserve established security, lifecycle, and privacy contracts.
//     Preserve canonical principal ownership and backend authorization.
//     Keep private registration values out of public projections, logs, and errors.
//
// Registration is a pre-account draft, never an operating authority.
// CE lock order: user -> effective role assignment/role -> registration ->
// merchant account -> merchant. All registration mutations use this order.
package data

import (
 "context"
 "errors"
 "fmt"
 "strings"
 "time"

 "github.com/PiccoloMondoC/focodebase/fobackend/internal/logging"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "github.com/jackc/pgx/v5/pgconn"
 "github.com/jackc/pgx/v5/pgxpool"
)

var (
 ErrMerchantRegistrationConflict = errors.New("merchant registration changed concurrently")
 ErrMerchantRegistrationNotDraft = errors.New("merchant registration is not a draft")
)
type MerchantRegistrationStatus string
const (
 MerchantRegistrationStatusDraft MerchantRegistrationStatus = "draft"
 MerchantRegistrationStatusCompleted MerchantRegistrationStatus = "completed"
)
type MerchantRegistration struct {
 ID uuid.UUID
 PrincipalUserID uuid.UUID
 Status MerchantRegistrationStatus
 Revision int64
 Fields MerchantRegistrationFields
 MerchantID *uuid.UUID
 CompletedAt *time.Time
 CreatedAt time.Time
 UpdatedAt time.Time
}
type MerchantRegistrationModel struct { DB *pgxpool.Pool; Logger *logging.Logger }

const merchantRegistrationSelectColumns = `id, principal_user_id, registration_status, revision,
 legal_name, dba_name, tax_id, address_line1, address_line2, address_city,
 address_region, address_postal_code, address_country, corporate_email,
 contact_name, phone, website, merchant_id, completed_at, created_at, updated_at`

// SQL names below are source constants, never request-derived identifiers.
var registrationFactColumns = []string{"legal_name", "dba_name", "tax_id", "address_line1",
 "address_line2", "address_city", "address_region", "address_postal_code",
 "address_country", "corporate_email", "contact_name", "phone", "website"}
func registrationFactArgs(f MerchantRegistrationFields) []any {
 return []any{f.LegalName,f.DBAName,f.TaxID,f.AddressLine1,f.AddressLine2,f.AddressCity,
 f.AddressRegion,f.AddressPostalCode,f.AddressCountry,f.CorporateEmail,f.ContactName,f.Phone,f.Website}
}
func scanMerchantRegistration(row scannableRow, r *MerchantRegistration) error {
 return row.Scan(&r.ID,&r.PrincipalUserID,&r.Status,&r.Revision,
 &r.Fields.LegalName,&r.Fields.DBAName,&r.Fields.TaxID,&r.Fields.AddressLine1,
 &r.Fields.AddressLine2,&r.Fields.AddressCity,&r.Fields.AddressRegion,
 &r.Fields.AddressPostalCode,&r.Fields.AddressCountry,&r.Fields.CorporateEmail,
 &r.Fields.ContactName,&r.Fields.Phone,&r.Fields.Website,&r.MerchantID,
 &r.CompletedAt,&r.CreatedAt,&r.UpdatedAt)
}

// RegistrationPersistenceError retains only an allowlisted SQLSTATE. pgx error
// Detail can contain the entire private row; it must never escape this boundary.
type RegistrationPersistenceError struct { Operation, SQLState string; Cause error }
func(e *RegistrationPersistenceError) Error() string { return "merchant registration: "+e.Operation+" failed ("+e.SQLState+")" }
func(e *RegistrationPersistenceError) Unwrap() error { return e.Cause }
func SafeMerchantRegistrationError(op string, err error) error {
 if err == nil { return nil }
 if errors.Is(err,context.Canceled) { return &RegistrationPersistenceError{op,"canceled",context.Canceled} }
 if errors.Is(err,context.DeadlineExceeded) { return &RegistrationPersistenceError{op,"timeout",context.DeadlineExceeded} }
 var p *pgconn.PgError
 if errors.As(err,&p) {
  switch p.Code {
  case "23505": return ErrMerchantRegistrationConflict
  case "23514","23502","22001": return ErrMerchantRegistrationInvalid
  case "40001","40P01","57014","08006","08003","53300":
   return &RegistrationPersistenceError{Operation:op,SQLState:p.Code}
  }
 }
 return &RegistrationPersistenceError{Operation:op,SQLState:"database"}
}
func translateMerchantRegistrationWriteError(op string,err error) error { return SafeMerchantRegistrationError(op,err) }
func wrapMerchantRegistrationReadError(op string,err error) error { return SafeMerchantRegistrationError(op,err) }

func(m *MerchantRegistrationModel) GetByPrincipal(ctx context.Context, id uuid.UUID)(*MerchantRegistration,error) {
 if m==nil||m.DB==nil||ctx==nil { return nil,errors.New("merchant registration database and context are required") }
 ctx,cancel:=context.WithTimeout(ctx,dbTimeout); defer cancel()
 return readRegistration(ctx,m.DB,id,false)
}
type registrationQuerier interface { QueryRow(context.Context,string,...any) pgx.Row }
func readRegistration(ctx context.Context,q registrationQuerier,id uuid.UUID,lock bool)(*MerchantRegistration,error) {
 if ctx==nil||q==nil||id==uuid.Nil { return nil,errors.New("merchant registration query, context and principal are required") }
 sql:=`SELECT `+merchantRegistrationSelectColumns+` FROM merchant_registrations WHERE principal_user_id=$1`
 if lock { sql+=` FOR UPDATE` }
 r:=new(MerchantRegistration)
 if err:=scanMerchantRegistration(q.QueryRow(ctx,sql,id),r); err!=nil {
  if errors.Is(err,pgx.ErrNoRows) { return nil,nil }
  return nil,SafeMerchantRegistrationError("read",err)
 }
 return r,nil
}
func(m *MerchantRegistrationModel) ReadByPrincipalTx(ctx context.Context,tx pgx.Tx,id uuid.UUID)(*MerchantRegistration,error) {
 if tx==nil {return nil,errors.New("registration transaction is required")}
 return readRegistration(ctx,tx,id,false)
}
func(m *MerchantRegistrationModel) LockByPrincipalTx(ctx context.Context,tx pgx.Tx,id uuid.UUID)(*MerchantRegistration,error) {
 if tx==nil {return nil,errors.New("registration transaction is required")}
 return readRegistration(ctx,tx,id,true)
}
func(m *MerchantRegistrationModel) InsertDraftTx(ctx context.Context,tx pgx.Tx,id uuid.UUID,f MerchantRegistrationFields)(*MerchantRegistration,bool,error) {
 if tx==nil||ctx==nil||id==uuid.Nil {return nil,false,errors.New("registration transaction, context and principal are required")}
 normalized,err:=NormalizeMerchantRegistrationFields(f); if err!=nil { return nil,false,err }
 args:=append([]any{uuid.New(),id},registrationFactArgs(normalized)...)
 placeholders:=make([]string,len(args)); for i:=range args { placeholders[i]=fmt.Sprintf("$%d",i+1) }
 sql:=`INSERT INTO merchant_registrations (id,principal_user_id,`+strings.Join(registrationFactColumns,",")+`)
 VALUES (`+strings.Join(placeholders,",")+`) ON CONFLICT (principal_user_id) DO NOTHING RETURNING `+merchantRegistrationSelectColumns
 r:=new(MerchantRegistration)
 if err:=scanMerchantRegistration(tx.QueryRow(ctx,sql,args...),r); err!=nil {
  if errors.Is(err,pgx.ErrNoRows) { return nil,false,nil }
  return nil,false,SafeMerchantRegistrationError("insert",err)
 }
 return r,true,nil
}
func(m *MerchantRegistrationModel) UpdateFieldsTx(ctx context.Context,tx pgx.Tx,id,principal uuid.UUID,f MerchantRegistrationFields)(*MerchantRegistration,error) {
 if tx==nil||ctx==nil||id==uuid.Nil||principal==uuid.Nil {return nil,errors.New("registration transaction, context and identity are required")}
 normalized,err:=NormalizeMerchantRegistrationFields(f); if err!=nil { return nil,err }
 assignments:=make([]string,len(registrationFactColumns))
 for i,col:=range registrationFactColumns { assignments[i]=fmt.Sprintf("%s=$%d",col,i+3) }
 args:=append([]any{id,principal},registrationFactArgs(normalized)...)
 sql:=`UPDATE merchant_registrations SET `+strings.Join(assignments,",")+`,revision=revision+1,updated_at=NOW()
 WHERE id=$1 AND principal_user_id=$2 RETURNING `+merchantRegistrationSelectColumns
 r:=new(MerchantRegistration)
 if err:=scanMerchantRegistration(tx.QueryRow(ctx,sql,args...),r); err!=nil {
  if errors.Is(err,pgx.ErrNoRows) { return nil,ErrMerchantRegistrationConflict }
  return nil,SafeMerchantRegistrationError("update",err)
 }
 return r,nil
}
func(m *MerchantRegistrationModel) MarkCompletedTx(ctx context.Context,tx pgx.Tx,id,principal,merchant uuid.UUID)(*MerchantRegistration,error) {
 if tx==nil||ctx==nil||id==uuid.Nil||principal==uuid.Nil||merchant==uuid.Nil {return nil,errors.New("registration transaction, context and identities are required")}
 r:=new(MerchantRegistration)
 err:=scanMerchantRegistration(tx.QueryRow(ctx,`UPDATE merchant_registrations
 SET registration_status='completed', merchant_id=$3,completed_at=NOW(),revision=revision+1,updated_at=NOW()
 WHERE id=$1 AND principal_user_id=$2 AND registration_status='draft' RETURNING `+merchantRegistrationSelectColumns,id,principal,merchant),r)
 if errors.Is(err,pgx.ErrNoRows) { return nil,ErrMerchantRegistrationNotDraft }
 if err!=nil { return nil,SafeMerchantRegistrationError("complete",err) }
 return r,nil
}

type MerchantNavigationState struct {
 RegistrationStatus *MerchantRegistrationStatus
 AccountStatus *MerchantAccountStatus
 PublicName *string
}
func(m *MerchantRegistrationModel) GetNavigationStateForPrincipal(ctx context.Context,id uuid.UUID)(*MerchantNavigationState,error) {
 if m==nil||m.DB==nil||ctx==nil||id==uuid.Nil {return nil,errors.New("registration database, context and principal are required")}
 ctx,cancel:=context.WithTimeout(ctx,dbTimeout); defer cancel()
 state:=new(MerchantNavigationState)
 // A deleted account still occupies the principal slot and must not be
 // presented as a new registration opportunity.
 var deleted bool
 err:=m.DB.QueryRow(ctx,`SELECT r.registration_status,ma.account_status,m.name::text,
 COALESCE(ma.deleted_at IS NOT NULL,FALSE)
 FROM (SELECT $1::uuid principal_user_id) p
 LEFT JOIN merchant_registrations r USING(principal_user_id)
 LEFT JOIN merchant_accounts ma USING(principal_user_id)
 LEFT JOIN merchants m ON m.id=ma.merchant_id`,id).Scan(
 &state.RegistrationStatus,&state.AccountStatus,&state.PublicName,&deleted)
 if err!=nil { return nil,SafeMerchantRegistrationError("navigation",err) }
 if deleted { closed:=MerchantAccountStatusClosed; state.AccountStatus=&closed }
 return state,nil
}
