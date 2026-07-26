// sdworkspace/sdbackend/internal/server/cmd/api/endpoints.go
package main

import (
	"net/http"
	"sort"
	"sync"

	"github.com/go-chi/chi/v5"
)

// endpointRegistry tracks public endpoints as a tiny thread-safe set.
// Entries are stored as "METHOD path" and returned sorted for stable UX.
type endpointRegistry struct {
	mu  sync.RWMutex
	set map[string]struct{}
}

func (app *Application) PublicEndpointsSnapshot() []string {
	var eps []string
	_ = chi.Walk(app.Router, func(method string, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		// If you tag public routes with a middleware/ctx flag, filter here.
		// For now assume all GETs intended for public surfacing:
		if method == "GET" {
			eps = append(eps, method+" "+route)
		}
		return nil
	})
	// Ensure well-knowns are present; Walk should catch them if registered.
	ensure := map[string]struct{}{
		"GET /": {}, "GET /healthz": {}, "GET /readyz": {}, "GET /metrics": {},
	}
	for e := range ensure {
		eps = append(eps, e)
	}
	// Dedup + stable order
	sort.Strings(eps)
	out := eps[:0]
	for i, s := range eps {
		if i == 0 || s != eps[i-1] {
			out = append(out, s)
		}
	}
	return out
}

func newEndpointRegistry() *endpointRegistry {
	return &endpointRegistry{set: make(map[string]struct{})}
}

func (er *endpointRegistry) add(method, path string) {
	er.mu.Lock()
	er.set[method+" "+path] = struct{}{}
	er.mu.Unlock()
}

func (er *endpointRegistry) listWithEnsure(baseline ...string) []string {
	er.mu.Lock()
	for _, b := range baseline {
		er.set[b] = struct{}{}
	}
	er.mu.Unlock()

	er.mu.RLock()
	out := make([]string, 0, len(er.set))
	for k := range er.set {
		out = append(out, k)
	}
	er.mu.RUnlock()

	sort.Strings(out)
	return out
}
