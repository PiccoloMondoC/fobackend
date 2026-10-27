// Package main exposes the public-safe discovery taxonomy without merchant privileges.
// focodebase/fobackend/internal/server/cmd/api/future_offering_public_categories.go
package main

import (
 "context"
 "net/http"
)

// ListPublicFutureOfferingCategoriesHandler deliberately returns only selectable
// category identity and display ancestry. It grants no catalog mutation rights.
func (app *Application) ListPublicFutureOfferingCategoriesHandler(w http.ResponseWriter,r *http.Request) {
 ctx,cancel:=context.WithTimeout(r.Context(),cfgTimeout);defer cancel()
 w.Header().Set("Cache-Control","no-store")
 options,err:=app.Models.Category.ListFutureOfferingCategoryOptions(ctx,&app.Models.Department)
 if err!=nil {app.serverErrorResponse(app.Logger.GetLoggerWithContext(r),w,r,err);return}
 app.respondWithJSON(w,http.StatusOK,jsonResponse{Error:false,Message:"Categories retrieved",Data:futureOfferingCategoryOptionsResponse{Categories:options}})
}
