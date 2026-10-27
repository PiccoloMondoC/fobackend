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

// internal/services/site_identity.go
// CE Site Identity boundary: validate -> lock active actor -> permission ->
// presentation mutex -> singleton -> revision -> write -> history -> audit.
// Startup takes only the presentation mutex; it never waits on actor rows.
// No configuration text is logged; commit/rollback belongs to withGovernanceTx.
// Identity is independent of operational Platform Settings and FO availability.
package services

import (
	"context"
	"fmt"
	"reflect"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/PiccoloMondoC/focodebase/fobackend/internal/data"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)
const (
 PermissionReadSiteIdentity="read_site_identity"
 PermissionUpdateSiteIdentity="update_site_identity"
 MaxSiteTerms=256
)
var siteTermKeyPattern=regexp.MustCompile(`^[a-z][a-z0-9_]*(\.[a-z][a-z0-9_]*)*$`)
// Stable semantics, unrestricted business wording. Existing stored labels are
// retained on every restart. Defaults initialize a missing aggregate only.
func DefaultSitePresentation() data.SitePresentation {
 return data.SitePresentation{Identity:data.SiteIdentityText{SiteName:"Website"},Terms:map[string]string{
  "future_offerings.area":"What's New",
  "anticipation_intelligence.label":"Signals",
  "anticipation_intelligence.early_label":"Early Signals",
  "future_offerings.singular":"Future Offering",
  "future_offerings.plural":"Future Offerings",
  "merchant.project":"project",
  "merchant.projects":"projects",
  "merchant.watch":"watch for updates",
  "merchant_center.name":"Merchant Center",
  "discovery.prompt":"Discover what’s next.",
  "home.hero_eyebrow":"Coming up",
  "home.hero_headline":"See what’s coming before it arrives.",
  "home.hero_lede":"Discover products and services taking shape, understand what is changing, and decide what belongs on your radar.",
  "home.hero_action":"Explore what’s coming",
 }}
}
// Validation errors identify a field, never the submitted content.
func siteText(field,value string,max int,required,multiline bool)(string,error){
 if !utf8.ValidString(value){return "",fmt.Errorf("%w: %s contains invalid text",data.ErrSiteIdentityInvalidInput,field)}
 value=strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(value,"\r\n","\n"),"\r","\n"))
 if utf8.RuneCountInString(value)>max || (required && value==""){return "",fmt.Errorf("%w: %s has an invalid length",data.ErrSiteIdentityInvalidInput,field)}
 for _,ch:=range value {if unicode.IsControl(ch) && !(multiline && ch=='\n'){return "",fmt.Errorf("%w: %s contains control characters",data.ErrSiteIdentityInvalidInput,field)}}
 return value,nil
}
func ValidateSitePresentation(in data.SitePresentation)(data.SitePresentation,error){
 out:=data.SitePresentation{Terms:make(map[string]string,len(in.Terms))}
 fields:=[]struct{name string;src string;dst *string;max int;required,multi bool}{
  {"site_name",in.Identity.SiteName,&out.Identity.SiteName,80,true,false},
  {"tagline",in.Identity.Tagline,&out.Identity.Tagline,160,false,false},
  {"description",in.Identity.Description,&out.Identity.Description,500,false,true},
  {"copyright_holder",in.Identity.CopyrightHolder,&out.Identity.CopyrightHolder,120,false,false},
 }
 for _,f:=range fields {v,err:=siteText(f.name,f.src,f.max,f.required,f.multi);if err!=nil{return out,err};*f.dst=v}
 if in.Terms==nil || len(in.Terms)>MaxSiteTerms{return out,data.ErrSiteIdentityInvalidInput}
 for key,label:=range in.Terms {
  if len(key)>100 || !siteTermKeyPattern.MatchString(key){return out,fmt.Errorf("%w: invalid term key",data.ErrSiteIdentityInvalidInput)}
  v,err:=siteText("term label",label,200,true,false);if err!=nil{return out,err};out.Terms[key]=v
 }
 // Required bindings must remain present. Arbitrary additional semantic keys
 // may be stored without a database catalogue of permissible business language.
 for key:=range DefaultSitePresentation().Terms {if key=="anticipation_intelligence.label" || key=="anticipation_intelligence.early_label" {continue};if _,ok:=out.Terms[key];!ok{return out,fmt.Errorf("%w: required term is missing",data.ErrSiteIdentityInvalidInput)}}
 return out,nil
}
func (s *Service) siteIdentityContext(ctx context.Context)(context.Context,context.CancelFunc,error){
 if err:=s.validate();err!=nil{return nil,nil,err}
 if ctx==nil{return nil,nil,ErrNilContext}
 if s.Models.SiteIdentity.DB==nil || s.Models.SiteIdentity.DB!=s.Models.DB{return nil,nil,fmt.Errorf("%w: site identity database composition",ErrInvalidServiceConfiguration)}
 bounded,cancel:=context.WithTimeout(ctx,s.Cfg.DBTimeout);return bounded,cancel,nil
}
func (s *Service) EnsureSiteIdentityInternal(ctx context.Context) error {
 ctx,cancel,err:=s.siteIdentityContext(ctx);if err!=nil{return err};defer cancel()
 def,err:=ValidateSitePresentation(DefaultSitePresentation());if err!=nil{return err}
 return pgx.BeginFunc(ctx,s.Models.DB,func(tx pgx.Tx)error{
  if _,err:=tx.Exec(ctx,`SELECT pg_advisory_xact_lock($1)`,data.SiteIdentityMutationLock);err!=nil{return err}
  return s.Models.SiteIdentity.EnsureTx(ctx,tx,def)
 })
}
func (s *Service) PublicSiteConfigurationInternal(ctx context.Context)(*data.SitePresentation,error){
 ctx,cancel,err:=s.siteIdentityContext(ctx);if err!=nil{return nil,err};defer cancel()
 record,err:=s.Models.SiteIdentity.Get(ctx);if err!=nil{return nil,err}
 validated,err:=ValidateSitePresentation(record.SitePresentation);if err!=nil{return nil,err}
 if validated.Identity.CopyrightHolder==""{validated.Identity.CopyrightHolder=validated.Identity.SiteName}
 return &validated,nil
}
// Privileged reads enforce current standing at the service boundary as well as
// the route gate. Holding actor locks prevents concurrent demotion mid-read.
func (s *Service) siteIdentityAuthorizedTx(ctx context.Context,actor uuid.UUID,permission string,fn func(pgx.Tx)error) error {
 if actor==uuid.Nil{return ErrGovernanceInsufficientAuthority}
 gov:=s.governance()
 return s.withGovernanceTx(ctx,func(tx pgx.Tx)error{
  subjects,err:=gov.LockGovernanceSubjectsTx(ctx,tx,actor);if err!=nil{return err}
  if actorFromSubject(subjects[actor]).Level==data.AdministrativeLevelNone{return ErrGovernanceInsufficientAuthority}
  if err=gov.RequirePermissionTx(ctx,tx,actor,permission);err!=nil{return err}
  return fn(tx)
 })
}
func (s *Service) SiteIdentityAdministrationInternal(ctx context.Context,actor uuid.UUID)(*data.SiteIdentityRecord,error){
 ctx,cancel,err:=s.siteIdentityContext(ctx);if err!=nil{return nil,err};defer cancel()
 var out *data.SiteIdentityRecord
 err=s.siteIdentityAuthorizedTx(ctx,actor,PermissionReadSiteIdentity,func(tx pgx.Tx)error{
  out,err=s.Models.SiteIdentity.SnapshotTx(ctx,tx);return err
 });return out,err
}
func (s *Service) SiteIdentityHistoryInternal(ctx context.Context,actor uuid.UUID,limit int)([]data.SiteIdentityHistoryEntry,error){
 ctx,cancel,err:=s.siteIdentityContext(ctx);if err!=nil{return nil,err};defer cancel()
 if limit<1||limit>100{return nil,data.ErrSiteIdentityInvalidInput}
 var out []data.SiteIdentityHistoryEntry
 err=s.siteIdentityAuthorizedTx(ctx,actor,PermissionReadSiteIdentity,func(tx pgx.Tx)error{out,err=s.Models.SiteIdentity.ListHistoryTx(ctx,tx,limit);return err});return out,err
}
type SiteIdentityChange struct {
 ActorID uuid.UUID
 ExpectedVersion int64
 Presentation data.SitePresentation
 Reason string
}
type SiteIdentityChangeResult struct {
 Configuration *data.SiteIdentityRecord `json:"configuration"`
 Changed bool `json:"changed"`
}
func (s *Service) ReplaceSiteIdentityInternal(ctx context.Context,req SiteIdentityChange)(*SiteIdentityChangeResult,error){
 ctx,cancel,err:=s.siteIdentityContext(ctx);if err!=nil{return nil,err};defer cancel()
 if req.ActorID==uuid.Nil||req.ExpectedVersion<1{return nil,data.ErrSiteIdentityInvalidInput}
 presentation,err:=ValidateSitePresentation(req.Presentation);if err!=nil{return nil,err}
 reason,err:=siteText("change_reason",req.Reason,500,false,true);if err!=nil{return nil,err}
 var result SiteIdentityChangeResult
 err=s.siteIdentityAuthorizedTx(ctx,req.ActorID,PermissionUpdateSiteIdentity,func(tx pgx.Tx)error{
  if _,err:=tx.Exec(ctx,`SELECT pg_advisory_xact_lock($1)`,data.SiteIdentityMutationLock);err!=nil{return err}
  current,err:=s.Models.SiteIdentity.GetForUpdateTx(ctx,tx);if err!=nil{return err}
  if current.Version!=req.ExpectedVersion{return data.ErrSiteIdentityVersionConflict}
  if reflect.DeepEqual(current.SitePresentation,presentation){result.Configuration=current;return nil}
  updated,err:=s.Models.SiteIdentity.ReplaceTx(ctx,tx,presentation,req.ExpectedVersion,req.ActorID);if err!=nil{return err}
  if err=s.Models.SiteIdentity.RecordChangeTx(ctx,tx,current,updated,reason,req.ActorID);err!=nil{return err}
  if err=s.governance().InsertGovernanceAuditTx(ctx,tx,data.GovernanceAuditEntry{
   ActorID:&req.ActorID,Action:"update_site_identity",EntityType:"site_identity",EntityID:"identity",
   Outcome:data.GovernanceOutcomeSucceeded,Context:data.GovernanceAuditContext{Operation:"replace_presentation"},
  });err!=nil{return err}
  result=SiteIdentityChangeResult{Configuration:updated,Changed:true};return nil
 })
 if err!=nil{return nil,err}
 s.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("ReplaceSiteIdentityInternal").Info("Site identity transaction completed","actor_id",req.ActorID,"version",result.Configuration.Version,"changed",result.Changed)
 return &result,nil
}
