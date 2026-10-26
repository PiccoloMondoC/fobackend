// Package mediastore tests GCS create-only requests, credential isolation and generation-guarded cleanup.
// focodebase/fobackend/internal/mediastore/gcs_test.go
package mediastore

import (
 "context"
 "io"
 "net/http"
 "strings"
 "testing"
 "time"
)
type roundTripFunc func(*http.Request)(*http.Response,error)
func(f roundTripFunc)RoundTrip(r *http.Request)(*http.Response,error){return f(r)}
func response(status int,body string)*http.Response{return &http.Response{StatusCode:status,Body:io.NopCloser(strings.NewReader(body)),Header:make(http.Header)}}
func TestGCSImmutableUploadAndConditionalDelete(t *testing.T){
 s,err:=NewGCS("private-media-test");if err!=nil{t.Fatal(err)};defer s.Close()
 s.token="test-only-token";s.expiry=time.Now().Add(time.Hour);calls:=0
 s.client.Transport=roundTripFunc(func(r *http.Request)(*http.Response,error){
  calls++;if r.URL.Scheme!="https"||r.URL.Host!="storage.googleapis.com"||r.Header.Get("Authorization")!="Bearer test-only-token"{t.Fatal("incorrect authenticated destination")}
  switch calls{
  case 1:
   if r.Method!="POST"||r.URL.Query().Get("ifGenerationMatch")!="0"||r.URL.Query().Get("name")!="media/"+testKey{t.Fatal("upload is not create-only")}
   return response(200,`{"generation":"42"}`),nil
  case 2:
   if r.Method!="GET"||r.URL.Query().Get("alt")!="media"{t.Fatal("not a media read")};return response(200,"photo"),nil
  case 3:return response(200,`{"generation":"42"}`),nil
  case 4:
   if r.Method!="DELETE"||r.URL.Query().Get("ifGenerationMatch")!="42"||r.URL.Query().Get("generation")!="42"{t.Fatal("unguarded delete")};return response(204,""),nil
  default:t.Fatal("unexpected request");return nil,nil
  }
 })
 if err=s.Put(context.Background(),testKey,"image/jpeg",strings.NewReader("photo"));err!=nil{t.Fatal(err)}
 f,err:=s.Open(context.Background(),testKey);if err!=nil{t.Fatal(err)};_ = f.Close()
 if err=s.Delete(context.Background(),testKey);err!=nil{t.Fatal(err)};if calls!=4{t.Fatal("incomplete lifecycle")}
}
func TestGCSRejectsUnsafeBucketAndPublicConfiguration(t *testing.T){
 for _,name:=range []string{"","https://example.test","bucket/path","../bucket"}{if _,err:=NewGCS(name);err==nil{t.Fatal("unsafe bucket accepted")}}
 s,_:=NewGCS("private-media-test");defer s.Close();s.token="test-only-token";s.expiry=time.Now().Add(time.Hour)
 s.client.Transport=roundTripFunc(func(*http.Request)(*http.Response,error){return response(200,`{"iamConfiguration":{"uniformBucketLevelAccess":{"enabled":true},"publicAccessPrevention":"inherited"}}`),nil})
 if s.Validate(context.Background())==nil{t.Fatal("bucket without explicit public access prevention accepted")}
 s.client.Transport=roundTripFunc(func(*http.Request)(*http.Response,error){return response(200,`{"iamConfiguration":{"uniformBucketLevelAccess":{"enabled":true},"publicAccessPrevention":"enforced"}}`),nil})
 if err:=s.Validate(context.Background());err!=nil{t.Fatal(err)}
}
