// Package main tests fail-closed media configuration and use of the provisioned local root.
// focodebase/fobackend/internal/server/cmd/api/media_storage_test.go
package main

import (
 "context"
 "testing"
)
func TestMediaStorageCloudRunRejectsFilesystemFallback(t *testing.T){
 t.Setenv("K_SERVICE","test-revision")
 for _,driver:=range []string{"","filesystem","unknown"}{
  t.Setenv("MEDIA_STORAGE_DRIVER",driver)
  if store,err:=newMediaStorageWithCodec(context.Background(),func(context.Context)error{return nil});err==nil{_ = store.Close();t.Fatal("Cloud Run accepted filesystem storage")}
 }
}
func TestMediaStorageUsesExistingLocalRoot(t *testing.T){
 t.Setenv("K_SERVICE","");t.Setenv("MEDIA_STORAGE_DRIVER","")
 root:=t.TempDir();t.Setenv("MEDIA_ROOT",root)
 store,err:=newMediaStorageWithCodec(context.Background(),func(context.Context)error{return nil});if err!=nil{t.Fatal(err)};defer store.Close()
 driver,location:=store.Location();if driver!="filesystem"||location!=root{t.Fatal("local root not used")}
 t.Setenv("MEDIA_ROOT",root+"/not-provisioned")
 if store,err:=newMediaStorageWithCodec(context.Background(),func(context.Context)error{return nil});err==nil{_ = store.Close();t.Fatal("missing mount silently created")}
}

func TestMediaStorageRefusesBrokenImageEncoder(t *testing.T){
 store,err:=newMediaStorageWithCodec(context.Background(),func(context.Context)error{return context.DeadlineExceeded})
 if err==nil||store!=nil{t.Fatal("broken encoder accepted")}
}
