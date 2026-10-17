// Package data persists the CE maintenance state and its immutable evidence.
// Availability is read from the primary database for every admission decision.
//
// focodebase/fobackend/internal/data/site_maintenance.go
package data

import (
 "context"
 "encoding/json"
 "errors"
 "fmt"
 "time"

 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "github.com/jackc/pgx/v5/pgxpool"
)

const MaintenanceEntity = "site_maintenance"
const MaintenanceActionPrefix = "site_maintenance_"
const MaintenanceIntegritySQLState = "SG008"
var ErrMaintenanceConflict = errors.New("maintenance version conflict")
var ErrMaintenanceUnavailable = errors.New("maintenance state unavailable")

type MaintenanceAnnouncement struct {
 ID uuid.UUID `json:"id"`
 StartsAt time.Time `json:"starts_at"`
 DurationMinutes int `json:"duration_minutes"`
 Message string `json:"message"`
}
// MaintenanceDocument contains exactly the durable, public presentation fields.
// A time is descriptive; the passage of time never changes availability.
type MaintenanceDocument struct {
 Active bool `json:"active"`
 ActiveSince *time.Time `json:"active_since"`
 Message *string `json:"message"`
 ExpectedEndAt *time.Time `json:"expected_end_at"`
 Announcement *MaintenanceAnnouncement `json:"announcement"`
}
type MaintenanceState struct {
 MaintenanceDocument
 Version int64 `json:"version"`
 UpdatedAt time.Time `json:"updated_at"`
 ObservedAt time.Time `json:"observed_at"`
}
type MaintenanceStatus struct {
 MaintenanceDocument
 ContractVersion int `json:"contract_version"`
 ObservedAt time.Time `json:"observed_at"`
}
func (s MaintenanceState) Public() MaintenanceStatus {
 return MaintenanceStatus{MaintenanceDocument:s.MaintenanceDocument,ContractVersion:1,ObservedAt:s.ObservedAt}
}
type MaintenanceEvent struct {
 ID uuid.UUID `json:"id"`
 Operation string `json:"operation"`
 Mechanism string `json:"mechanism"`
 ActorID *uuid.UUID `json:"actor_id"`
 Version int64 `json:"version"`
 Reason string `json:"reason"`
 OccurredAt time.Time `json:"occurred_at"`
}
type MaintenanceModel struct { DB *pgxpool.Pool }
type maintenanceQuery interface { QueryRow(context.Context,string,...any) pgx.Row }
func (m *MaintenanceModel) Read(ctx context.Context) (*MaintenanceState,error) {
 if m==nil || m.DB==nil {return nil,ErrMaintenanceUnavailable}
 return m.ReadTx(ctx,m.DB,false)
}
func (m *MaintenanceModel) ReadTx(ctx context.Context,q maintenanceQuery,lock bool) (*MaintenanceState,error) {
 sql:=`SELECT version, document, updated_at, clock_timestamp() FROM public.site_maintenance_state WHERE singleton`
 if lock {sql+=` FOR UPDATE`}
 var s MaintenanceState;var raw []byte
 if err:=q.QueryRow(ctx,sql).Scan(&s.Version,&raw,&s.UpdatedAt,&s.ObservedAt);err!=nil{return nil,fmt.Errorf("%w: %w",ErrMaintenanceUnavailable,err)}
 if err:=json.Unmarshal(raw,&s.MaintenanceDocument);err!=nil{return nil,fmt.Errorf("%w: invalid document",ErrMaintenanceUnavailable)}
 if s.Version<1 || (s.Active && s.ActiveSince==nil) || (!s.Active && (s.ActiveSince!=nil || s.Message!=nil || s.ExpectedEndAt!=nil)) {return nil,ErrMaintenanceUnavailable}
 return &s,nil
}
// WriteTx requires the common governance mutex and the singleton row lock.
// The deferred database constraint also requires the matching success audit.
func (m *MaintenanceModel) WriteTx(ctx context.Context,tx pgx.Tx,before *MaintenanceState,next MaintenanceDocument,op,mechanism string,actor *uuid.UUID,reason string,evidence any) (uuid.UUID,error) {
 id:=uuid.New();oldRaw,err:=json.Marshal(before.MaintenanceDocument);if err!=nil{return uuid.Nil,err}
 newRaw,err:=json.Marshal(next);if err!=nil{return uuid.Nil,err}
 proof,err:=json.Marshal(evidence);if err!=nil{return uuid.Nil,err}
 tag,err:=tx.Exec(ctx,`UPDATE public.site_maintenance_state SET document=$2::jsonb,version=version+1,updated_at=clock_timestamp() WHERE singleton AND version=$1`,before.Version,string(newRaw))
 if err!=nil{return uuid.Nil,err};if tag.RowsAffected()!=1{return uuid.Nil,ErrMaintenanceConflict}
 _,err=tx.Exec(ctx,`INSERT INTO public.site_maintenance_transitions(id,operation,mechanism,actor_id,version,before_state,after_state,reason,evidence) VALUES($1,$2,$3,$4,$5,$6::jsonb,$7::jsonb,$8,$9::jsonb)`,id,op,mechanism,actor,before.Version+1,string(oldRaw),string(newRaw),reason,string(proof))
 return id,err
}
func (m *MaintenanceModel) History(ctx context.Context,tx pgx.Tx) ([]MaintenanceEvent,error) {
 rows,err:=tx.Query(ctx,`SELECT id,operation,mechanism,actor_id,version,reason,occurred_at FROM public.site_maintenance_transitions ORDER BY version DESC LIMIT 20`)
 if err!=nil{return nil,err};defer rows.Close();out:=make([]MaintenanceEvent,0)
 for rows.Next(){var e MaintenanceEvent;if err:=rows.Scan(&e.ID,&e.Operation,&e.Mechanism,&e.ActorID,&e.Version,&e.Reason,&e.OccurredAt);err!=nil{return nil,err};out=append(out,e)}
 return out,rows.Err()
}
