// sdworkspace/sdbackend/internal/server/cmd/api/oauth_clients.go
//   Release Class: DEFERRED
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/PiccoloMondoC/sdworkspace/sdbackend/internal/data"
	"github.com/PiccoloMondoC/sdworkspace/sdbackend/internal/utils/timeutil"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
)

type envelope map[string]any

// RegisterOauthClientHandler registers a new OAuth 2.0 client.
//
// Flow
// ──────────────────────────────────────────────────────────────────────
// 1️ Caller already authenticated (uuid.UUID stored under ctxUserID).  
// 2️ Decode & validate JSON payload.  
// 3️ Hash client_secret (SHA‑256 + pepper → bcrypt).  
// 4️ Persist record; surface unique‑key violations as 409.  
// 5️ Best‑effort inline audit log (dynamic Action & EntityType).  
// 6️ Return 201 Created with public client metadata.
//
func (app *Application) RegisterOauthClientHandler(w http.ResponseWriter, r *http.Request) {
	logger := app.Logger.GetLoggerWithContext(r).WithFunctionName("RegisterOauthClientHandler")

	//----------------------------------------------------------------------
	// 1. Trusted caller identity (uuid.UUID injected by AuthMiddleware)
	//----------------------------------------------------------------------
	ctx := r.Context()
	userID, ok := ctx.Value(ctxUserID).(uuid.UUID) // already type‑safe
	if !ok || userID == uuid.Nil {
		app.respondWithError(w, errors.New("unauthorized"), http.StatusUnauthorized)
		return
	}

	//----------------------------------------------------------------------
	// 2. Decode & validate JSON payload
	//----------------------------------------------------------------------
	var input struct {
		ClientID     string `json:"client_id"`
		ClientSecret string `json:"client_secret"`
	}
	if err := app.readJSON(w, r, &input); err != nil {
		app.respondWithError(w, fmt.Errorf("invalid JSON payload: %w", err), http.StatusBadRequest)
		return
	}
	if input.ClientID == "" || input.ClientSecret == "" {
		app.respondWithError(w, errors.New("client_id and client_secret are required"), http.StatusBadRequest)
		return
	}
	// Enforce UUID format for client_id (best practice, keeps IDs opaque).
	if _, err := uuid.Parse(input.ClientID); err != nil {
		app.respondWithError(w, errors.New("client_id must be a valid UUID"), http.StatusBadRequest)
		return
	}

	//----------------------------------------------------------------------
	// 3. Hash secret (SHA‑256 + pepper → bcrypt)
	//----------------------------------------------------------------------
	sha := sha256.Sum256([]byte(app.Config.SecretPepper + input.ClientSecret))
	bcryptHash, err := bcrypt.GenerateFromPassword(sha[:], bcrypt.DefaultCost)
	if err != nil {
		logger.Error("bcrypt.GenerateFromPassword failed", "err", err)
		app.serverErrorResponse(logger, w, r, err)
		return
	}
	secretHashHex := hex.EncodeToString(bcryptHash) // store as ASCII

	//----------------------------------------------------------------------
	// 4. Persist client record
	//----------------------------------------------------------------------
	client := &data.OauthClient{
		ClientID:         input.ClientID,
		ClientSecretHash: secretHashHex,
		IsActive:         true,
		// Note: internal ID & timestamps are set by CreateClient().
	}

	ctxDB, cancel := context.WithTimeout(ctx, cfgTimeout)
	defer cancel()

	if err := app.Models.OauthClient.CreateClient(ctxDB, client); err != nil {
		if data.IsUniqueViolation(err) {
			app.respondWithError(w, errors.New("client_id already exists"), http.StatusConflict)
			return
		}
		logger.Error("OauthClient.CreateClient failed", "err", err)
		app.serverErrorResponse(logger, w, r, err)
		return
	}

	// ─── 5. Dynamic audit‑log (fail‑soft) ────────────────────────────

	// 5a. Resolve / create action “register_oauth_client”.
	action, err := app.Models.Action.GetByName(ctx, "register_oauth_client")
	if err != nil || action == nil {
		logger.Warn("audit action 'register_oauth_client' missing, attempting create", "err", err)
		if id, createErr := app.Models.Action.CreateIfNotExists(
			ctx, "register_oauth_client", "Register a new OAuth2 client"); createErr == nil {
			action = &data.Action{ID: id}
		} else {
			logger.Error("failed to create audit action", "err", createErr)
		}
	}

	// 5b. Resolve / create entity‑type “oauth_client”.
	entityType, err := app.Models.EntityType.GetByName(ctx, "oauth_client")
	if err != nil || entityType == nil {
		logger.Warn("entity type 'oauth_client' missing, attempting create", "err", err)
		if id, createErr := app.Models.EntityType.CreateIfNotExists(
			ctx, "oauth_client", "OAuth2 client credentials set"); createErr == nil {
			entityType = &data.EntityType{ID: id}
		} else {
			logger.Error("failed to create entity type", "err", createErr)
		}
	}

	// 5c. Insert audit‑log.
	if action != nil && entityType != nil {
		audit := data.AuditLog{
			ID:           uuid.New(),
			UserID:       &userID,
			ActionID:     action.ID,
			EntityTypeID: entityType.ID,
			EntityID:     client.ID.String(),
		}
		if err := app.Models.AuditLog.Insert(ctx, &audit); err != nil {
			logger.Warn("audit logging failed", "err", err)
		}
	}

	// ─── 6. Respond ──────────────────────────────────────────────────
	logger.Info("OAuth client retrieved", "user_id", userID, "client_id", client.ClientID)
	app.writeJSON(w, http.StatusOK, envelope{"oauth_client": client}, nil)
}


// GetOauthClientHandler retrieves an OAuth 2.0 client by its public UUID.
//
// Flow
// ──────────────────────────────────────────────────────────────────────
// 1️ Caller already authenticated (uuid.UUID stored under ctxUserID).  
// 2️ Extract client UUID from context (ctxClientID).                    // ← set by param‑binding middleware/router
// 3️ Look up the client (404 on miss).  
// 4️ Best‑effort inline audit log (dynamic Action & EntityType).  
// 5️ Return 200 OK with client metadata.
//
// NOTE: No restructuring – follows the approach in RegisterOauthClientHandler.
func (app *Application) GetOauthClientHandler(w http.ResponseWriter, r *http.Request) {
	logger := app.Logger.GetLoggerWithContext(r).
		WithFunctionName("GetOauthClientHandler")

	//----------------------------------------------------------------------
	// 1. Trusted caller identity (uuid.UUID injected by AuthMiddleware)
	//----------------------------------------------------------------------
	ctx := r.Context()
	userID, ok := ctx.Value(ctxUserID).(uuid.UUID)
	if !ok || userID == uuid.Nil {
		app.respondWithError(w, errors.New("unauthorized"), http.StatusUnauthorized)
		return
	}

	//----------------------------------------------------------------------
	// 2. Extract and validate the internal OAuth client UUID.
	//----------------------------------------------------------------------
	rawClientID, ok := ctx.Value(ctxClientID).(string)
	if !ok {
		app.respondWithError(
			w,
			errors.New("client_id required"),
			http.StatusBadRequest,
		)
		return
	}

	rawClientID = strings.TrimSpace(rawClientID)
	if rawClientID == "" {
		app.respondWithError(
			w,
			errors.New("client_id required"),
			http.StatusBadRequest,
		)
		return
	}

	clientUUID, err := uuid.Parse(rawClientID)
	if err != nil {
		app.respondWithError(
			w,
			errors.New("client_id must be a valid UUID"),
			http.StatusBadRequest,
		)
		return
	}

	//----------------------------------------------------------------------
	// 3. Fetch client record (time‑boxed DB call)
	//----------------------------------------------------------------------
	ctxDB, cancel := context.WithTimeout(ctx, cfgTimeout)
	defer cancel()

	client, err := app.Models.OauthClient.GetClientByUUID(ctxDB, clientUUID)
	if err != nil {
		if errors.Is(err, data.ErrRecordNotFound) {
			app.respondWithError(
				w,
				errors.New("client not found"),
				http.StatusNotFound,
			)
			return
		}

		logger.Error(
			"OauthClient.GetClientByUUID failed",
			"client_uuid", clientUUID,
			"error", err,
		)
		app.serverErrorResponse(logger, w, r, err)
		return
	}


	// ─── 4. Dynamic audit‑log resolution (best‑effort) ───────────────────────
	//
	// 4a. Resolve / create audit‑action “get_oauth_client”.
	action, err := app.Models.Action.GetByName(ctx, "get_oauth_client")
	if err != nil || action == nil {
		logger.Warn("audit action 'get_oauth_client' missing, attempting create", "err", err)
		if id, createErr := app.Models.Action.CreateIfNotExists(
			ctx,
			"get_oauth_client",
			"Retrieve an OAuth2 client"); createErr == nil {
			action = &data.Action{ID: id}
		} else {
			logger.Error("failed to create audit action", "err", createErr)
		}
	}

	// 4b. Resolve / create entity‑type “oauth_client”.
	entityType, err := app.Models.EntityType.GetByName(ctx, "oauth_client")
	if err != nil || entityType == nil {
		logger.Warn("entity type 'oauth_client' missing, attempting create", "err", err)
		if id, createErr := app.Models.EntityType.CreateIfNotExists(
			ctx,
			"oauth_client",
			"OAuth2 client credentials set"); createErr == nil {
			entityType = &data.EntityType{ID: id}
		} else {
			logger.Error("failed to create entity type", "err", createErr)
		}
	}

	// 4c. Insert audit‑log (fail‑soft).
	if action != nil && entityType != nil {
		audit := data.AuditLog{
			ID:           uuid.New(),
			UserID:       &userID,
			ActionID:     action.ID,
			EntityTypeID: entityType.ID,
			EntityID:     client.ID.String(),
		}
		if err := app.Models.AuditLog.Insert(ctx, &audit); err != nil {
			logger.Warn("audit logging failed", "client_uuid", client.ID, "err", err)
		}
	}

	// ─── 5. Respond ──────────────────────────────────────────────────────────
	logger.Info("OAuth client retrieved",
		"user_id", userID,
		"client_uuid", client.ID,
		"client_id", client.ClientID)

	app.writeJSON(w, http.StatusOK, envelope{"oauth_client": client}, nil)
}


// UpdateOauthClientHandler updates mutable fields of an existing OAuth 2.0 client.
//
// Flow
// ──────────────────────────────────────────────────────────────────────────────
// 1️  Caller already authenticated (uuid.UUID stored under ctxUserID).          │
// 2️  Decode JSON payload { id, client_secret?, is_active? }.                   │
// 3️  Fetch target record (404 if missing).                                     │
// 4️  Apply PATCH‑style mutations:                                              │
//        • If client_secret present → hash (SHA‑256 + pepper → bcrypt).         │
//        • If is_active present → toggle.                                      │
//        • Abort early if no change.                                           │
// 5️  Persist update; surface unique / FK errors as 409 / 400.                  │
// 6️  Best‑effort inline audit log (“update_oauth_client”).                     │
// 7️  Return 200 OK with updated public metadata.                               │
//
func (app *Application) UpdateOauthClientHandler(w http.ResponseWriter, r *http.Request) {
	logger := app.Logger.GetLoggerWithContext(r).WithFunctionName("UpdateOauthClientHandler")

	//----------------------------------------------------------------------
	// 1. Caller identity (already trusted by AuthMiddleware)
	//----------------------------------------------------------------------
	ctx := r.Context()
	userID, ok := ctx.Value(ctxUserID).(uuid.UUID) // type‑safe extract
	if !ok || userID == uuid.Nil {
		app.respondWithError(w, errors.New("unauthorized"), http.StatusUnauthorized)
		return
	}

	//----------------------------------------------------------------------
	// 2. Decode & validate JSON payload
	//----------------------------------------------------------------------
	var input struct {
		ID           string `json:"id"`            // required – internal PK (uuid.UUID)
		ClientSecret string `json:"client_secret"` // optional – plain text
		IsActive     *bool  `json:"is_active"`     // optional – tri‑state
	}

	if err := app.readJSON(w, r, &input); err != nil {
		app.respondWithError(w, fmt.Errorf("invalid JSON payload: %w", err), http.StatusBadRequest)
		return
	}
	clientUUID, err := uuid.Parse(input.ID)
	if err != nil {
		app.respondWithError(w, errors.New("id must be a valid UUID"), http.StatusBadRequest)
		return
	}

	//----------------------------------------------------------------------
	// 3. Fetch current record (404 if not found)
	//----------------------------------------------------------------------
	ctxDB, cancel := context.WithTimeout(ctx, cfgTimeout)
	defer cancel()

	client, err := app.Models.OauthClient.GetClientByUUID(ctxDB, clientUUID)
	if err != nil {
		if errors.Is(err, data.ErrRecordNotFound) {
			app.respondWithError(w, errors.New("oauth client not found"), http.StatusNotFound)
		} else {
			logger.Error("OauthClient.GetClientByUUID failed", "err", err)
			app.serverErrorResponse(logger, w, r, err)
		}
		return
	}

	//----------------------------------------------------------------------
	// 4. Apply PATCH mutations
	//----------------------------------------------------------------------
	changed := false

	// 4a. Rotate secret if provided
	if input.ClientSecret != "" {
		sha := sha256.Sum256([]byte(app.Config.SecretPepper + input.ClientSecret))
		hash, err := bcrypt.GenerateFromPassword(sha[:], bcrypt.DefaultCost)
		if err != nil {
			logger.Error("bcrypt.GenerateFromPassword failed", "err", err)
			app.serverErrorResponse(logger, w, r, err)
			return
		}
		newHashHex := hex.EncodeToString(hash)
		if newHashHex != client.ClientSecretHash {
			client.ClientSecretHash = newHashHex
			changed = true
		}
	}

	// 4b. Toggle active flag
	if input.IsActive != nil && *input.IsActive != client.IsActive {
		client.IsActive = *input.IsActive
		changed = true
	}

	if !changed {
		// Nothing to do – respond early
		app.writeJSON(w, http.StatusOK, envelope{"oauth_client": client}, nil)
		return
	}
	client.UpdatedAt = timeutil.Now()

	//----------------------------------------------------------------------
	// 5. Persist update
	//----------------------------------------------------------------------
	if err := app.Models.OauthClient.UpdateClient(ctxDB, client); err != nil {
		logger.Error("OauthClient.UpdateClient failed", "err", err)
		app.serverErrorResponse(logger, w, r, err)
		return
	}

	// ─── 6. Dynamic audit‑log resolution (best‑effort) ────────────────────────
	//
	// 6a.  Resolve / create audit‑action “update_oauth_client”.
	action, err := app.Models.Action.GetByName(ctx, "update_oauth_client")
	if err != nil || action == nil {
		logger.Warn("audit action 'update_oauth_client' not found, attempting to create...", "error", err)
		if id, createErr := app.Models.Action.CreateIfNotExists(
			ctx,
			"update_oauth_client",
			"Update an existing OAuth2 client"); createErr == nil {
			action = &data.Action{ID: id}
		} else {
			logger.Error("failed to create missing audit action", "error", createErr)
		}
	}

	// 6b.  Resolve / create entity‑type “oauth_client”.
	entityType, err := app.Models.EntityType.GetByName(ctx, "oauth_client")
	if err != nil || entityType == nil {
		logger.Warn("entity‑type 'oauth_client' not found, attempting to create...", "error", err)
		if id, createErr := app.Models.EntityType.CreateIfNotExists(
			ctx,
			"oauth_client",
			"OAuth2 client credentials set"); createErr == nil {
			entityType = &data.EntityType{ID: id}
		} else {
			logger.Error("failed to create missing entity‑type", "error", createErr)
		}
	}

	// 6c.  Insert audit‑log (fail‑soft).
	if action != nil && entityType != nil {
		audit := data.AuditLog{
			ID:           uuid.New(),
			UserID:       &userID,
			ActionID:     action.ID,
			EntityTypeID: entityType.ID,
			EntityID:     client.ID.String(),
		}
		if err := app.Models.AuditLog.Insert(ctx, &audit); err != nil {
			logger.Warn("audit logging failed", "client_id", client.ID, "error", err)
			// Continue – main operation succeeded
		}
	}

	// ─── 7. Respond ───────────────────────────────────────────────────────────
	logger.Info("OAuth client updated",
		"user_id", userID,
		"client_uuid", client.ID,
		"client_id", client.ClientID)

	app.writeJSON(w, http.StatusOK,
		envelope{"oauth_client": client}, nil)
}


// DeleteOauthClientHandler deactivates (soft‑deletes) an OAuth 2.0 client.
//
// Behaviour
// ─────────
// • Authenticated caller only – AuthMiddleware has already validated the JWT
//   and injected the caller’s uuid.UUID under ctxUserID.
// • Expects the public client‑ID (UUID string) in the query param ?id=.
// • Responds 404 if the client does not exist or is already inactive.
// • Emits an inline audit‑log entry (best‑effort; never blocks the main flow).
func (app *Application) DeleteOauthClientHandler(w http.ResponseWriter, r *http.Request) {
	logger := app.Logger.
		GetLoggerWithContext(r).
		WithFunctionName("DeleteOauthClientHandler")

	//----------------------------------------------------------------------
	// 1. Extract caller’s identity (uuid.UUID – already type‑safe)
	//----------------------------------------------------------------------
	userID, ok := r.Context().Value(ctxUserID).(uuid.UUID)
	if !ok || userID == uuid.Nil {
		app.respondWithError(w, errors.New("unauthorized"), http.StatusUnauthorized)
		return
	}

	//----------------------------------------------------------------------
	// 2. Parse & validate target client‑ID (public string)
	//----------------------------------------------------------------------
	rawClientID := strings.TrimSpace(r.URL.Query().Get("id"))
	if rawClientID == "" {
		app.respondWithError(w, errors.New("client_id query parameter is required"), http.StatusBadRequest)
		return
	}

	// clientID is public but we enforce UUID format across the platform.
	clientUUID, err := uuid.Parse(rawClientID)
	if err != nil {
		app.respondWithError(w, errors.New("client_id must be a valid UUID"), http.StatusBadRequest)
		return
	}

	//----------------------------------------------------------------------
	// 3. Deactivate the client (soft delete)
	//----------------------------------------------------------------------
	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	if err := app.Models.OauthClient.DeactivateClient(ctx, rawClientID); err != nil {
		// Treat “not found” and “already inactive” the same → 404
		if errors.Is(err, data.ErrRecordNotFound) {
			app.respondWithError(w, errors.New("oauth client not found"), http.StatusNotFound)
			return
		}
		logger.Error("OauthClient.DeactivateClient failed",
			"err", err,
			"client_id", rawClientID)
		app.serverErrorResponse(logger, w, r, err)
		return
	}
	
	//----------------------------------------------------------------------
	// 4. Inline audit‑log (best effort – soft failure tolerated)
	//----------------------------------------------------------------------
	// 4a. Resolve / create action “delete_oauth_client”
	action, err := app.Models.Action.GetByName(ctx, "delete_oauth_client")
	if err != nil || action == nil {
		logger.Warn("audit action 'delete_oauth_client' not found, attempting to create...", "error", err)
		actionID, createErr := app.Models.Action.CreateIfNotExists(ctx,
			"delete_oauth_client",
			"Delete an existing OAuth2 client")
		if createErr != nil {
			logger.Error("failed to create missing audit action", "error", createErr)
			// Allow main operation to succeed
		} else {
			action = &data.Action{ID: actionID}
		}
	}

	// 4b. Resolve / create entity‑type “oauth_client”
	entityType, err := app.Models.EntityType.GetByName(ctx, "oauth_client")
	if err != nil || entityType == nil {
		logger.Warn("entity‑type missing 'oauth_client' not found, attempting to create...", "error", err)
		entityTypeID, createErr := app.Models.EntityType.CreateIfNotExists(ctx,
			"oauth_client",
			"OAuth2 client credentials set")
		if createErr != nil {
			logger.Error("failed to create missing entity‑type", "error", createErr)
		} else {
			entityType = &data.EntityType{ID: entityTypeID}
		} 
		
	}

	// 4c. Insert Audit Log (fail gracefully if audit logging fails)
	if action != nil && entityType != nil {
		audit := data.AuditLog{
			ID:           uuid.New(),
			UserID:       &userID,
			ActionID:     action.ID,
			EntityTypeID: entityType.ID,
			EntityID:     clientUUID.String(),
		}
		if err := app.Models.AuditLog.Insert(ctx, &audit); err != nil {
			logger.Warn("audit logging failed", "client_id", rawClientID, "error", err)
			// Continue – main operation succeeded
		}
	}

	//----------------------------------------------------------------------
	// 5. Success response
	//----------------------------------------------------------------------
	logger.Info("oauth client deleted",
		"user_id", userID,
		"client_id", rawClientID)

	app.writeJSON(w, http.StatusOK, envelope{
		"message":     "oauth client deleted",
		"client_id":   rawClientID,
		"deleted_at":  timeutil.Now(),
	}, nil)
}
