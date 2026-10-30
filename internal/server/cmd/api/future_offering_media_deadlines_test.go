// Package main tests offering upload deadline control.
// focodebase/fobackend/internal/server/cmd/api/future_offering_media_deadlines_test.go
package main

import (
	"net/http/httptest"
	"testing"
	"time"
)

type mediaDeadlineWriter struct{
	*httptest.ResponseRecorder
	read,write time.Time
}
func(w *mediaDeadlineWriter)SetReadDeadline(value time.Time)error{w.read=value;return nil}
func(w *mediaDeadlineWriter)SetWriteDeadline(value time.Time)error{w.write=value;return nil}
func TestOfferingUploadHasBoundedLocalDeadlines(t *testing.T){
	w:=&mediaDeadlineWriter{ResponseRecorder:httptest.NewRecorder()}
	start:=time.Now();if err:=offeringUploadDeadlines(w);err!=nil{t.Fatal(err)}
	if w.read.Sub(start)<29*time.Second||w.read.Sub(start)>31*time.Second||w.write.Sub(start)<149*time.Second||w.write.Sub(start)>151*time.Second{t.Fatal("upload deadline bounds changed")}
	if err:=offeringUploadDeadlines(httptest.NewRecorder());err==nil{t.Fatal("unsupported deadline control silently accepted")}
}
