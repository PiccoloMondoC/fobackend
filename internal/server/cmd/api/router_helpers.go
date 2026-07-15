// sdworkspace/sdbackend/internal/server/cmd/api/router_helpers.go
package main

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

// PublicGet registers a GET route and records it as public.
func (app *Application) PublicGet(r chi.Router, path string, h http.HandlerFunc) {
	r.Get(path, h)
	app.publicRoutes.Register("GET", path)
}

// PublicPost registers a POST route and records it as public (use sparingly for true-public).
func (app *Application) PublicPost(r chi.Router, path string, h http.HandlerFunc) {
	r.Post(path, h)
	app.publicRoutes.Register("POST", path)
}

// PublicHandle registers a generic handler (e.g., /metrics) and records it as public (GET).
func (app *Application) PublicHandle(r chi.Router, path string, h http.Handler) {
	r.Handle(path, h)
	app.publicRoutes.Register("GET", path)
}
