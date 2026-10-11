// focodebase/fobackend/internal/governancerecovery/approval_test.go
package governancerecovery
import("crypto/ed25519";"crypto/rand";"testing";"github.com/google/uuid")
func TestApprovalsBindRequestPlanAndCeremony(t *testing.T) {
 board,boardPrivate,err:=ed25519.GenerateKey(rand.Reader);if err!=nil{t.Fatal(err)}
 cio,cioPrivate,err:=ed25519.GenerateKey(rand.Reader);if err!=nil{t.Fatal(err)}
 r:=Request{Operation:"recovery",TargetID:uuid.New(),ConfirmEmail:"root@example.test",BoardReference:"BOARD-1",CIOReference:"CIO-1",Executor:"engineering",Reason:"incumbent_unreachable",PredecessorDisposition:"consumer"}
 id:=uuid.New();digest:="0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
 message,err:=ApprovalMessage(r,digest,id);if err!=nil{t.Fatal(err)}
 b:=ed25519.Sign(boardPrivate,message);c:=ed25519.Sign(cioPrivate,message)
 approval,err:=VerifyApproval(r,digest,id,board,cio,b,c);if err!=nil || !approval.matches(r,digest,id){t.Fatal("valid approval refused",err)}
 for _,wrongID:=range []uuid.UUID{uuid.New(),uuid.Nil}{if _,err:=VerifyApproval(r,digest,wrongID,board,cio,b,c);err==nil{t.Fatal("ceremony substitution accepted")}}
 changed:=r;changed.TargetID=uuid.New();if _,err:=VerifyApproval(changed,digest,id,board,cio,b,c);err==nil{t.Fatal("successor substitution accepted")}
 if _,err:=VerifyApproval(r,digest,id,board,cio,nil,c);err==nil{t.Fatal("missing Board approval accepted")}
 if _,err:=VerifyApproval(r,digest,id,board,board,b,b);err==nil{t.Fatal("one trust key occupied both approval roles")}
 if (*VerifiedApproval)(nil).matches(r,digest,id){t.Fatal("nil approval accepted")}
}
