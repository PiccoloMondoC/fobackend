// BUSINESS-NEUTRAL SOURCE RULE — BEG 3.17–3.18
// CE and engineering own this naming rule. Use platform for neutral internal
// identifiers; use the platform in general UI prose. Capitalize Platform only
// where a heading, sentence position, or defined label requires it. When naming
// an installation's website or business, use its administrator-configured identity.
// These choices must be implemented and reviewed in source, not delegated to the
// project owner as editing guidance.
// Do not hardcode an installation's brand in identifiers, CSS tokens, public
// copy, defaults, or seed data. Public identity and business wording must come
// from administrator-managed configuration. Internal names describe purpose.
// Pre-startup call-site compatibility is not justification for weak naming.
// Repair dependent callers together. An exception requires a documented,
// intentional public, production, or migration-stable contract and CE review.
// Run tools/check_business_neutrality.py before source release. Existing
// violations are not automatically grandfathered or approved.

//
// focodebase/fobackend/internal/data/site_identity.go
// CE: one versioned presentation aggregate, one immutable history event per commit.
// Schema ownership remains exclusively in database.go. Transaction policy belongs
// to services; these methods never commit a caller's transaction.
package data

import (
 "context"
 "encoding/json"
 "errors"
 "time"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "github.com/jackc/pgx/v5/pgxpool"
)

var (
 ErrSiteIdentityInvalidInput = errors.New("invalid site identity")
 ErrSiteIdentityVersionConflict = errors.New("site identity version conflict")
 ErrSiteIdentityNotFound = errors.New("site identity unavailable")
)

const SiteIdentityMutationLock int64 = 7301195004

type SiteIdentityText struct {
 SiteName string `json:"site_name"`
 Tagline string `json:"tagline"`
 Description string `json:"description"`
 CopyrightHolder string `json:"copyright_holder"`
}
type SitePresentation struct {
 Identity SiteIdentityText `json:"identity"`
 Terms map[string]string `json:"terms"`
}
type SiteIdentityRecord struct {
 SitePresentation
 Version int64 `json:"version"`
 UpdatedBy *uuid.UUID `json:"updated_by"`
 UpdatedAt time.Time `json:"updated_at"`
}
type SiteIdentityHistoryEntry struct {
 ID uuid.UUID `json:"id"`
 Version int64 `json:"version"`
 PreviousValue json.RawMessage `json:"previous_value"`
 NewValue json.RawMessage `json:"new_value"`
 ChangeReason string `json:"change_reason"`
 ChangedBy *uuid.UUID `json:"changed_by"`
 ChangedAt time.Time `json:"changed_at"`
}
type SiteIdentityModel struct { DB *pgxpool.Pool }

func readSiteIdentity(row pgx.Row) (*SiteIdentityRecord,error) {
 var out SiteIdentityRecord
 var raw []byte
 err:=row.Scan(&raw,&out.Version,&out.UpdatedBy,&out.UpdatedAt)
 if errors.Is(err,pgx.ErrNoRows) {return nil,ErrSiteIdentityNotFound}
 if err!=nil{return nil,err}
 if err=json.Unmarshal(raw,&out.SitePresentation);err!=nil{return nil,err}
 return &out,nil
}
func (m SiteIdentityModel) Get(ctx context.Context) (*SiteIdentityRecord,error) {
 return readSiteIdentity(m.DB.QueryRow(ctx,`SELECT presentation,version,updated_by,updated_at FROM site_identity WHERE singleton`))
}
func (m SiteIdentityModel) GetForUpdateTx(ctx context.Context,tx pgx.Tx) (*SiteIdentityRecord,error) {
 return readSiteIdentity(tx.QueryRow(ctx,`SELECT presentation,version,updated_by,updated_at FROM site_identity WHERE singleton FOR UPDATE`))
}
// EnsureTx is serialized with mutations and other startup processes. Existing
// values are not rewritten by deploy/restart, including installation wording.
func (m SiteIdentityModel) EnsureTx(ctx context.Context,tx pgx.Tx,presentation SitePresentation) error {
 raw,err:=json.Marshal(presentation);if err!=nil{return err}
 _,err=tx.Exec(ctx,`INSERT INTO site_identity(singleton,presentation) VALUES(TRUE,$1::jsonb) ON CONFLICT(singleton) DO NOTHING`,string(raw))
 return err
}
func (m SiteIdentityModel) ReplaceTx(ctx context.Context,tx pgx.Tx,presentation SitePresentation,expected int64,actor uuid.UUID) (*SiteIdentityRecord,error) {
 raw,err:=json.Marshal(presentation);if err!=nil{return nil,err}
 out,err:=readSiteIdentity(tx.QueryRow(ctx,`UPDATE site_identity SET presentation=$1::jsonb,version=version+1,updated_by=$2,updated_at=clock_timestamp() WHERE singleton AND version=$3 RETURNING presentation,version,updated_by,updated_at`,string(raw),actor,expected))
 if errors.Is(err,ErrSiteIdentityNotFound){return nil,ErrSiteIdentityVersionConflict}
 return out,err
}
func (m SiteIdentityModel) RecordChangeTx(ctx context.Context,tx pgx.Tx,before,after *SiteIdentityRecord,reason string,actor uuid.UUID) error {
 oldJSON,err:=json.Marshal(before.SitePresentation);if err!=nil{return err}
 newJSON,err:=json.Marshal(after.SitePresentation);if err!=nil{return err}
 _,err=tx.Exec(ctx,`INSERT INTO site_identity_history(version,previous_value,new_value,change_reason,changed_by) VALUES($1,$2::jsonb,$3::jsonb,$4,$5)`,after.Version,string(oldJSON),string(newJSON),reason,actor)
 return err
}
func (m SiteIdentityModel) ListHistoryTx(ctx context.Context,tx pgx.Tx,limit int) ([]SiteIdentityHistoryEntry,error) {
 rows,err:=tx.Query(ctx,`SELECT id,version,previous_value,new_value,change_reason,changed_by,changed_at FROM site_identity_history ORDER BY version DESC LIMIT $1`,limit)
 if err!=nil{return nil,err};defer rows.Close()
 out:=make([]SiteIdentityHistoryEntry,0)
 for rows.Next(){var v SiteIdentityHistoryEntry;if err=rows.Scan(&v.ID,&v.Version,&v.PreviousValue,&v.NewValue,&v.ChangeReason,&v.ChangedBy,&v.ChangedAt);err!=nil{return nil,err};out=append(out,v)}
 return out,rows.Err()
}

// SnapshotTx performs one statement without acquiring a presentation write lock.
func (m SiteIdentityModel) SnapshotTx(ctx context.Context,tx pgx.Tx)(*SiteIdentityRecord,error){
 return readSiteIdentity(tx.QueryRow(ctx,`SELECT presentation,version,updated_by,updated_at FROM site_identity WHERE singleton`))
}
