// Package main provides account identity completion and administrative identity safeguards.
// focodebase/fobackend/internal/server/cmd/api/identity_completion.go

package main

import "net/http"

// Exact method/path pairs prevent an incomplete account reaching other profile
// or administrative operations through a broad prefix exemption.
func identityCompletionTransport(method,path string)bool{
 switch method+" "+path {
 case "GET /api/v1/user-profile/self", "POST /api/v1/user-profile/self", "PATCH /api/v1/user-profile/self",
 "GET /api/v1/user-profile/self/identity", "GET /api/v1/user-profile/self/completeness",
 "GET /api/v1/user-profile/handle-availability", "POST /api/v1/user/logout",
 "POST /api/v1/user/token/refresh", "POST /api/v1/user/password/change", "POST /api/v1/user/password":return true
 };return false
}

// Completion and recovery remain reachable while the existing maintenance gate
// continues to own every ordinary operation. Their handlers retain authentication.
func(app *Application)identityMaintenanceGate(next http.Handler)http.Handler{
 gated:=app.SiteMaintenanceGateMiddleware(next)
 return http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
  if identityCompletionTransport(r.Method,r.URL.Path){next.ServeHTTP(w,r);return}
  gated.ServeHTTP(w,r)
 })
}
