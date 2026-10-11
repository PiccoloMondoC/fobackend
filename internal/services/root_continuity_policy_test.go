// focodebase/fobackend/internal/services/root_continuity_policy_test.go
package services
import("testing";"github.com/PiccoloMondoC/focodebase/fobackend/internal/data";"github.com/google/uuid")
func TestAPICeremonyVocabularyExcludesOperatorAuthority(t *testing.T) {
 for _,op:=range []string{"recovery","consultant_grant","consultant_revoke","force_succession","bootstrap"} {if ValidContinuityOperation(op){t.Fatalf("API accepts operator operation %q",op)}}
}
func TestCoRootHasOperationalAuthorityButCannotGovernIncumbentOrStanding(t *testing.T) {
 actor:=GovernanceActor{UserID:uuid.New(),Level:data.AdministrativeLevelSuperAdmin,IsRoot:true}
 ordinary:=GovernanceTarget{UserID:uuid.New(),Exists:true,IsActive:true,Level:data.AdministrativeLevelSuperAdmin,Status:data.AppointmentStatusActive}
 if err:=AuthorizeGovernanceOperation(actor,ordinary,GovernanceSuspend);err!=nil{t.Fatal("co-Root lacks ordinary Root authority:",err)}
 incumbent:=ordinary;incumbent.IsRoot=true;if AuthorizeGovernanceOperation(actor,incumbent,GovernanceRevoke)==nil{t.Fatal("co-Root displaced incumbent")}
 designated:=ordinary;designated.SuccessionRank=1;if AuthorizeGovernanceOperation(actor,designated,GovernanceDemoteToAdmin)==nil{t.Fatal("co-Root silently removed successor standing")}
}
