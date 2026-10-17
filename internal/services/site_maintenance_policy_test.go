// focodebase/fobackend/internal/services/site_maintenance_policy_test.go
package services

import (
 "errors"
 "reflect"
 "testing"
 "time"
 "github.com/PiccoloMondoC/focodebase/fobackend/internal/data"
 "github.com/google/uuid"
)
func TestCEMaintenanceIndependentAspects(t *testing.T){
 now:=time.Date(2026,10,16,17,0,0,0,time.UTC);actor:=uuid.New();oldID:=uuid.New()
 a:=&data.MaintenanceAnnouncement{ID:oldID,StartsAt:now.Add(time.Hour),DurationMinutes:60,Message:"Routine work"}
 since:=now.Add(-time.Hour);msg:="Work in progress"
 active:=&data.MaintenanceState{MaintenanceDocument:data.MaintenanceDocument{Active:true,ActiveSince:&since,Message:&msg,Announcement:a},Version:8,ObservedAt:now}
 for _,op:=range []string{"save","cancel"}{
  req:=MaintenanceCommand{ActorID:actor,Operation:op,ExpectedVersion:8,StartsAt:now.Add(time.Hour),DurationMinutes:30,Message:"Revised work"}
  next,_,err:=PlanMaintenance(active,req,uuid.New());if err!=nil{t.Fatal(err)}
  if !next.Active || !reflect.DeepEqual(next.ActiveSince,active.ActiveSince) || !reflect.DeepEqual(next.Message,active.Message){t.Fatal("announcement changed availability")}
  if op=="save" && next.Announcement.ID==oldID{t.Fatal("revision retained dismissal identity")}
 }
 offline:=*active;offline.MaintenanceDocument.Active=false;offline.MaintenanceDocument.ActiveSince=nil;offline.MaintenanceDocument.Message=nil
 req:=MaintenanceCommand{ActorID:actor,Operation:"activate",ExpectedVersion:8,LinkAnnouncement:true}
 next,_,err:=PlanMaintenance(&offline,req,uuid.New());if err!=nil{t.Fatal(err)}
 if !next.Active || next.Announcement.ID!=oldID || next.ExpectedEndAt==nil{t.Fatal("activation did not preserve linked announcement")}
 req.Operation="restore";next,_,err=PlanMaintenance(active,req,uuid.New());if err!=nil{t.Fatal(err)}
 if next.Active || next.Message!=nil || next.ActiveSince!=nil || next.Announcement.ID!=oldID{t.Fatal("restore changed the independent notice")}
 req.RetireAnnouncement=true;next,_,err=PlanMaintenance(active,req,uuid.New());if err!=nil || next.Announcement!=nil{t.Fatal("explicit retirement failed")}
 if active.Announcement.ID!=oldID || !active.Active {t.Fatal("planner mutated input")}
}
func TestCEMaintenanceNoTimerTransitions(t *testing.T){
 past:=time.Date(2025,1,1,0,0,0,0,time.UTC);now:=past.Add(365*24*time.Hour)
 state:=data.MaintenanceState{MaintenanceDocument:data.MaintenanceDocument{Active:true,ActiveSince:&past,ExpectedEndAt:&past,Announcement:&data.MaintenanceAnnouncement{ID:uuid.New(),StartsAt:past,DurationMinutes:5,Message:"Old notice"}},Version:4,ObservedAt:now}
 if !state.Public().Active{t.Fatal("expected end reopened the platform")}
 state.Active=false;state.ActiveSince=nil;state.ExpectedEndAt=nil
 if state.Public().Active{t.Fatal("announcement time activated maintenance")}
}
func TestCEMaintenanceAuthorityAndValidation(t *testing.T){
 for _,actor:=range []GovernanceActor{
  {UserID:uuid.New(),Level:data.AdministrativeLevelAdmin},
  {UserID:uuid.New(),Level:data.AdministrativeLevelSuperAdmin},
  {UserID:uuid.New(),IsRoot:true},
 }{if !errors.Is(AuthorizeMaintenance(actor),ErrGovernanceInsufficientAuthority){t.Fatal("non-Root admitted")}}
 if err:=AuthorizeMaintenance(GovernanceActor{UserID:uuid.New(),Level:data.AdministrativeLevelSuperAdmin,IsRoot:true});err!=nil{t.Fatal(err)}
 for _,value:=range []string{"message\nline", "control\u0085", " padded ", string([]byte{0xff})}{if maintenanceText(value,500,true){t.Fatal("invalid public text admitted")}}
 state:=&data.MaintenanceState{Version:2,ObservedAt:time.Now()}
 _,_,err:=PlanMaintenance(state,MaintenanceCommand{ActorID:uuid.New(),Operation:"activate",ExpectedVersion:1},uuid.New())
 if !errors.Is(err,data.ErrMaintenanceConflict){t.Fatal("stale version accepted")}
}
