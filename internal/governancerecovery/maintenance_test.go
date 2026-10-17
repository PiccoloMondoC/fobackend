// focodebase/fobackend/internal/governancerecovery/maintenance_test.go
package governancerecovery

import (
 "crypto/ed25519"
 "crypto/rand"
 "strings"
 "testing"
 "github.com/google/uuid"
)
func TestCEMaintenanceApprovalBindsDomainRequestPlanAndCeremony(t *testing.T){
 r:=MaintenanceRecoveryRequest{Reference:"INCIDENT-1",Executor:"engineering-operator",Reason:"Restore service"}
 digest:=strings.Repeat("a",64);id:=uuid.New()
 bp,bk,err:=ed25519.GenerateKey(rand.Reader);if err!=nil{t.Fatal(err)};cp,ck,err:=ed25519.GenerateKey(rand.Reader);if err!=nil{t.Fatal(err)}
 message,err:=MaintenanceApprovalMessage(r,digest,id);if err!=nil{t.Fatal(err)}
 bs,cs:=ed25519.Sign(bk,message),ed25519.Sign(ck,message)
 if _,err:=VerifyMaintenanceApproval(r,digest,id,bp,cp,bs,cs);err!=nil{t.Fatal(err)}
 changed:=r;changed.Reason="A different action"
 for _,bad:=range []struct{r MaintenanceRecoveryRequest;digest string;id uuid.UUID}{{changed,digest,id},{r,strings.Repeat("b",64),id},{r,digest,uuid.New()}}{
  if _,err:=VerifyMaintenanceApproval(bad.r,bad.digest,bad.id,bp,cp,bs,cs);err==nil{t.Fatal("signature reused for a different ceremony")}
 }
 if _,err:=VerifyMaintenanceApproval(r,digest,id,bp,bp,bs,bs);err==nil{t.Fatal("same trust key supplied twice")}
 if _,err:=VerifyMaintenanceApproval(r,digest,id,bp,cp,nil,cs);err==nil{t.Fatal("missing Board signature accepted")}
}
