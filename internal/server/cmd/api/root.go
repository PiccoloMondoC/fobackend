// sdworkspace/sdbackend/internal/server/cmd/api/root.go
//
// Root ("GET /") handler and a JSON NotFound handler for consistent API UX.
// The root endpoint is a lightweight, human-friendly entry point that confirms
// the service is running and hints at commonly used endpoints.

package main

import (
	"encoding/json"
	"net/http"
	"time"
)

// rootResp defines the JSON payload returned by the "/" route.
type rootResp struct {
	Service   string   `json:"service"`
	Status    string   `json:"status"`
	Time      string   `json:"time"`
	Endpoints []string `json:"endpoints"`
}

// RootHandler uses the live snapshot
func (app *Application) RootHandler(w http.ResponseWriter, r *http.Request) {
    w.Header().Set("Content-Type", "application/json")
    resp := rootResp{
        Service: "sagrentideals",
        Status:  "ok",
        Time:    time.Now().UTC().Format(time.RFC3339),
        Endpoints: app.publicRoutes.listWithEnsure(
            "GET /", "GET /healthz", "GET /readyz", "GET /metrics",
        ),
    }
    if err := json.NewEncoder(w).Encode(resp); err != nil {
        http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
        return
    }
}


// NotFoundHandler provides a consistent JSON error for unknown routes.
// Register it with r.NotFound(app.NotFoundHandler) in routes.go.
func (app *Application) NotFoundHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusNotFound)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"error":   "route not found",
		"method":  r.Method,
		"path":    r.URL.Path,
		"service": "sagrentideals",
	})
}
