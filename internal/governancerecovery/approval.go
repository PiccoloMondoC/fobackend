// focodebase/fobackend/internal/governancerecovery/approval.go
// Detached Ed25519 approvals bind the exact reviewed plan and ceremony UUID.
// Trusted public keys are infrastructure-managed; no private signing key belongs
// in this application, runtime environment, request file, or source delivery.
package governancerecovery
import (
 "crypto/ed25519"
 "crypto/sha256"
 "encoding/hex"
 "encoding/json"
 "errors"
 "github.com/google/uuid"
)
type ApprovalEvidence struct {
 MessageSHA256 string `json:"message_sha256"`
 BoardKeyFingerprint string `json:"board_key_fingerprint,omitempty"`
 CIOKeyFingerprint string `json:"cio_key_fingerprint"`
 BoardSignature string `json:"board_signature,omitempty"`
 CIOSignature string `json:"cio_signature"`
}
type VerifiedApproval struct { evidence ApprovalEvidence }
// ApprovalMessage is the exact UTF-8 payload signed outside this application.
func ApprovalMessage(r Request,digest string,ceremonyID uuid.UUID) ([]byte,error) {
 return json.Marshal(struct {
  Domain string `json:"domain"`
  Request Request `json:"request"`
  PlanDigest string `json:"plan_digest"`
  CeremonyID uuid.UUID `json:"ceremony_id"`
 }{"sagrenti-root-governance-v1",r,digest,ceremonyID})
}
func fingerprint(b []byte)string{sum:=sha256.Sum256(b);return hex.EncodeToString(sum[:])}
func VerifyApproval(r Request,digest string,id uuid.UUID,boardKey,cioKey ed25519.PublicKey,boardSignature,cioSignature []byte) (*VerifiedApproval,error) {
 if err:=ValidateRequest(r);err!=nil{return nil,err}
 if len(digest)!=64 || id==uuid.Nil{return nil,errors.New("reviewed plan and ceremony ID required")}
 message,err:=ApprovalMessage(r,digest,id);if err!=nil{return nil,err}
 if len(cioKey)!=ed25519.PublicKeySize || len(cioSignature)!=ed25519.SignatureSize || !ed25519.Verify(cioKey,message,cioSignature){return nil,errors.New("CIO approval signature could not be verified")}
 e:=ApprovalEvidence{MessageSHA256:fingerprint(message),CIOKeyFingerprint:fingerprint(cioKey),CIOSignature:hex.EncodeToString(cioSignature)}
 if r.Operation=="recovery" {
  if len(boardKey)!=ed25519.PublicKeySize || len(boardSignature)!=ed25519.SignatureSize || !ed25519.Verify(boardKey,message,boardSignature){return nil,errors.New("Board approval signature could not be verified")}
  if string(boardKey)==string(cioKey){return nil,errors.New("Board and CIO trust keys must be distinct")}
  e.BoardKeyFingerprint=fingerprint(boardKey);e.BoardSignature=hex.EncodeToString(boardSignature)
 }
 return &VerifiedApproval{evidence:e},nil
}
func (a *VerifiedApproval) matches(r Request,digest string,id uuid.UUID) bool {
 if a==nil{return false};message,err:=ApprovalMessage(r,digest,id);return err==nil && fingerprint(message)==a.evidence.MessageSHA256
}
