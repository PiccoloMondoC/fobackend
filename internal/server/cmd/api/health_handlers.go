// sdworkspace/sdbackend/internal/server/cmd/api/health_handlers
package main

import (
	"context"
	"encoding/json"
	"net/http"
	"time"
)

// healthResp defines the JSON response for both /healthz and /readyz.
type healthResp struct {
	Status  string `json:"status"`  // "ok" or "ready"/"not_ready"
	Service string `json:"service"` // static service name
	Time    string `json:"time"`    // RFC3339 UTC timestamp
}

// LivenessHandler returns 200 if the process is up and serving HTTP.
// It intentionally avoids external calls to keep it cheap and reliable.
func (app *Application) LivenessHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	resp := healthResp{
		Status:  "ok",
		Service: "platform",
		Time:    time.Now().UTC().Format(time.RFC3339),
	}

	// json.NewEncoder handles errors; if it fails, fall back to 500.
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		// Best-effort error write; headers may already be sent.
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
}

// ReadinessHandler verifies critical dependencies (database).
// It times out fast to avoid hanging probes and returns 503 if not ready.
func (app *Application) ReadinessHandler(w http.ResponseWriter, r *http.Request) {
	// Keep this short; 2s is a common, reasonable default for probes.
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()

	// Call into the data layer's encapsulated health method.
	if err := app.Models.HealthCheck(ctx); err != nil {
		// Structured log for operators/observability (if your logger is available here).
		// app.Logger.Error("readiness check failed", "err", err)

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)

		resp := healthResp{
			Status:  "not_ready",
			Service: "platform",
			Time:    time.Now().UTC().Format(time.RFC3339),
		}
		_ = json.NewEncoder(w).Encode(resp) // best-effort; ignore encode error for probe
		return
	}

	w.Header().Set("Content-Type", "application/json")
	resp := healthResp{
		Status:  "ready",
		Service: "platform",
		Time:    time.Now().UTC().Format(time.RFC3339),
	}
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
}
