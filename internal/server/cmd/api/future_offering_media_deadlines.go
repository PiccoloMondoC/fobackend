// Package main bounds the offering upload transport deadlines.
// focodebase/fobackend/internal/server/cmd/api/future_offering_media_deadlines.go
package main

import (
	"net/http"
	"time"
)

// ResponseController traverses middleware exposing Unwrap. Unsupported deadline
// control fails before upload work; it must not silently use a shorter deadline.
func offeringUploadDeadlines(w http.ResponseWriter)error{
	controller:=http.NewResponseController(w)
	if err:=controller.SetReadDeadline(time.Now().Add(30*time.Second));err!=nil{return err}
	return controller.SetWriteDeadline(time.Now().Add(150*time.Second))
}
