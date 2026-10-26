// Package mediastore tests immutable storage, traversal rejection, cancellation and symlink isolation.
// focodebase/fobackend/internal/mediastore/filesystem_linux_test.go
package mediastore

import (
 "context"
 "errors"
 "io"
 "os"
 "path/filepath"
 "strings"
 "testing"
)
const testKey="d1c0a588-9660-4ba0-9b69-2d12be381f41"
func TestFilesystemPrivateImmutableLifecycle(t *testing.T){
 dir:=t.TempDir();s,err:=NewFilesystem(dir);if err!=nil{t.Fatal(err)};defer s.Close()
 ctx:=context.Background()
 if err=s.Put(ctx,testKey,"image/jpeg",strings.NewReader("original"));err!=nil{t.Fatal(err)}
 if err=s.Put(ctx,testKey,"image/jpeg",strings.NewReader("overwrite"));err==nil{t.Fatal("immutable key overwritten")}
 f,err:=s.Open(ctx,testKey);if err!=nil{t.Fatal(err)};raw,err:=io.ReadAll(f);_ = f.Close()
 if err!=nil||string(raw)!="original"{t.Fatal("stored object changed")}
 info,err:=os.Stat(filepath.Join(dir,testKey));if err!=nil||info.Mode().Perm()!=0600{t.Fatal("object permissions are not private")}
 if err=s.Delete(ctx,testKey);err!=nil{t.Fatal(err)};if err=s.Delete(ctx,testKey);err!=nil{t.Fatal("delete is not idempotent")}
 if _,err=s.Open(ctx,testKey);!errors.Is(err,ErrNotFound){t.Fatal("deleted object remains available")}
}
func TestFilesystemRejectsTraversalAndSymlinkReads(t *testing.T){
 dir:=t.TempDir();s,err:=NewFilesystem(dir);if err!=nil{t.Fatal(err)};defer s.Close();ctx:=context.Background()
 for _,key:=range []string{"../secret","/etc/passwd",testKey+"/","",strings.ToUpper(testKey)}{
  if _,err=s.Open(ctx,key);!errors.Is(err,ErrInvalidKey){t.Fatal("unsafe key accepted")}
  if err=s.Put(ctx,key,"image/jpeg",strings.NewReader("x"));!errors.Is(err,ErrInvalidKey){t.Fatal("unsafe write accepted")}
  if err=s.Delete(ctx,key);!errors.Is(err,ErrInvalidKey){t.Fatal("unsafe delete accepted")}
 }
 outside:=filepath.Join(t.TempDir(),"private");if err=os.WriteFile(outside,[]byte("secret"),0600);err!=nil{t.Fatal(err)}
 if err=os.Symlink(outside,filepath.Join(dir,testKey));err!=nil{t.Fatal(err)}
 if _,err=s.Open(ctx,testKey);err==nil{t.Fatal("symlink followed")}
 if err=s.Put(ctx,testKey,"image/jpeg",strings.NewReader("x"));err==nil{t.Fatal("symlink overwritten")}
 if err=s.Delete(ctx,testKey);err!=nil{t.Fatal(err)}
 raw,err:=os.ReadFile(outside);if err!=nil||string(raw)!="secret"{t.Fatal("outside file touched")}
 cancelled,cancel:=context.WithCancel(ctx);cancel()
 if err=s.Put(cancelled,testKey,"image/jpeg",strings.NewReader("x"));!errors.Is(err,context.Canceled){t.Fatal("cancelled write started")}
}
