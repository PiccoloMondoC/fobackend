// focodebase/fobackend/internal/governancerecovery/recovery_test.go
package governancerecovery
import("testing";"github.com/google/uuid")
func TestRecoveryRequiresBoardCIOAndExplicitDisposition(t *testing.T) {
 r:=Request{Operation:"recovery",TargetID:uuid.New(),ConfirmEmail:"verified@example.test",BoardReference:"BOARD-1",CIOReference:"CIO-1",Executor:"engineering-1",Reason:"incumbent_unreachable",PredecessorDisposition:"consumer",DepartureWithoutConflict:true}
 if err:=ValidateRequest(r);err!=nil{t.Fatal(err)}
 for _,mutate:=range []func(*Request){func(r *Request){r.BoardReference=""},func(r *Request){r.CIOReference=""},func(r *Request){r.Executor=""},func(r *Request){r.PredecessorDisposition=""},func(r *Request){r.TargetID=uuid.Nil},func(r *Request){r.PredecessorDisposition="super_admin"}} {bad:=r;mutate(&bad);if ValidateRequest(bad)==nil{t.Fatal("incomplete authorization accepted")}}
 r.DepartureWithoutConflict=false;r.PredecessorDisposition="super_admin";if err:=ValidateRequest(r);err!=nil{t.Fatal("Board-authorized disposition refused:",err)}
}
func TestConsultingIsBoundedAndReadOnly(t *testing.T) {
 r:=Request{Operation:"consultant_grant",TargetID:uuid.New(),ConfirmEmail:"honorary@example.test",CIOReference:"CIO-1",Executor:"engineering",IssueReference:"ISSUE-1",Capabilities:[]string{"read_continuity"},DurationHours:24}
 if err:=ValidateRequest(r);err!=nil{t.Fatal(err)}
 r.Capabilities=[]string{"appoint_super_admin"};if ValidateRequest(r)==nil{t.Fatal("write authority granted")}
 r.Capabilities=[]string{"read_continuity"};r.DurationHours=169;if ValidateRequest(r)==nil{t.Fatal("unbounded access accepted")}
}
