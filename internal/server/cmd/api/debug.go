// focodebase/fobackend/internal/server/cmd/api/debug.go
package main

import (
	"encoding/json"
	"net/http"
)

// DebugContextHandler **prints all non‑nil context values that were injected by your middleware.**
// Enabled only when `app.Config.Bootstrap.Env` is `"development"`; never mount this route in prod.
//
// GET /debug/context
func (app *Application) DebugContextHandler(w http.ResponseWriter, r *http.Request) {
	// Collect the keys you care about (kept short on purpose—expand if needed).
	keys := []ctxKey{
		ctxUserID, ctxTargetUserID,
		ctxMerchantID, ctxRoleID,
	}

	out := make(map[string]any, len(keys))
	for _, k := range keys {
		if v := r.Context().Value(k); v != nil {
			out[string(k)] = v
		}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(out) // ignore error – nothing useful to do on failure
}
