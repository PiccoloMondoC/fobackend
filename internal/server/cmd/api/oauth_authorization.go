// sdworkspace/sdbackend/internal/server/cmd/api/oauth_authorization.go
//
// GTM:
//
//	Layer: 2.2 Identity / Auth Domain
//	Release Class: DEFERRED
//	Reason:
//	  OAuth Authorization Code and Consent handlers are valid OAuth provider
//	  infrastructure, but Platform acting as an OAuth provider for
//	  third-party applications is not required for the initial
//	  Platform release spine. Do not expand this file until the
//	  release spine is functionally complete.
//
// DEFERRED Rule:
//
//	Keep compiling.
//	Keep safe.
//	Preserve redirect URI whitelist and constant-time comparison.
//	Preserve authorization-code entropy and hashing-on-persist behavior.
//	Preserve fail-soft, best-effort audit logging.
//	Preserve DB-owned lifecycle timestamp behavior.
//	Do not add new features.
//	Do not register new routes into v1 UI/API expansion.
//	Do not block deployment on this file unless it breaks the build.
package main

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/PiccoloMondoC/focodebase/fobackend/internal/data"
	"github.com/PiccoloMondoC/focodebase/fobackend/internal/utils/timeutil"

	"github.com/google/uuid"
)

// OauthAuthorizeHandler implements the OAuth 2.0 *Authorization Code* flow.
//
//  1. Caller is already authenticated (ctxUserID injected by AuthMiddleware).
//  2. Validates response_type, client_id, and redirect_uri; echoes state verbatim.
//  3. Ensures the client is active and the redirect URI is on its whitelist.
//  4. Generates a 43‑char, URL‑safe code (256‑bit entropy) and persists only a
//     SHA‑256 hash (done inside the model) with a 10‑minute TTL.
//  5. Best‑effort audit log (action “create_oauth_authorization_code”).
//  6. Redirects with 302 → redirect_uri?code=…&state=… (no‑store, no‑cache).
func (app *Application) OauthAuthorizeHandler(w http.ResponseWriter, r *http.Request) {
	logger := app.Logger.GetLoggerWithContext(r).WithFunctionName("OauthAuthorizeHandler")
	ctx := r.Context()

	// ─── 1. Trusted caller identity (uuid.UUID) ───────────────────────────────
	userID, ok := ctx.Value(ctxUserID).(uuid.UUID)
	if !ok || userID == uuid.Nil {
		app.respondWithError(w, errors.New("unauthorized"), http.StatusUnauthorized)
		return
	}

	// ─── 2. Parse & validate query parameters ────────────────────────────────
	q := r.URL.Query()

	if !strings.EqualFold(q.Get("response_type"), "code") {
		app.respondWithError(w, errors.New("response_type must be \"code\""), http.StatusBadRequest)
		return
	}

	publicClientID := q.Get("client_id")
	if publicClientID == "" {
		app.respondWithError(w, errors.New("client_id is required"), http.StatusBadRequest)
		return
	}

	redirectURI := q.Get("redirect_uri")
	if redirectURI == "" {
		app.respondWithError(w, errors.New("redirect_uri is required"), http.StatusBadRequest)
		return
	}

	// Optional params
	rawScope := q.Get("scope") // space‑delimited, echoed only if you later need it
	state := q.Get("state")

	// ─── 3. Client lookup & redirect‑URI whitelisting ────────────────────────
	ctxDB, cancel := context.WithTimeout(ctx, cfgTimeout)
	defer cancel()

	client, err := app.Models.OauthClient.GetClientByClientID(ctxDB, publicClientID)
	switch {
	case err == data.ErrRecordNotFound:
		app.respondWithError(w, errors.New("unknown client_id"), http.StatusBadRequest)
		return
	case err != nil:
		logger.Error("failed to fetch OAuth client", "err", err)
		app.serverErrorResponse(logger, w, r, err)
		return
	}

	if !client.IsActive {
		app.respondWithError(w, errors.New("client inactive"), http.StatusBadRequest)
		return
	}
	if !isRedirectAllowed(client, redirectURI) {
		app.respondWithError(w, errors.New("redirect_uri not registered"), http.StatusBadRequest)
		return
	}

	// ─── 4. Generate cryptographically‑secure code ───────────────────────────
	random := make([]byte, 32) // 256‑bit entropy
	if _, err := io.ReadFull(rand.Reader, random); err != nil {
		logger.Error("crypto/rand failed", "err", err)
		app.serverErrorResponse(logger, w, r, err)
		return
	}
	codePlain := base64.RawURLEncoding.EncodeToString(random) // 43 chars

	// ─── 5. Persist authorization code (hashing inside model) ────────────────
	authCode := &data.OauthAuthorizationCode{
		ClientID:    client.ID,
		UserID:      userID,
		RedirectURI: redirectURI,
		ExpiresAt:   timeutil.Now().Add(10 * time.Minute),
		// Scope is not stored here; long-term scopes live in
		// OauthUserConsent.
		//
		// ID and CodeHash are populated by CreateAuthorizationCode.
		// CreatedAt is owned by the database.
	}

	if err := app.Models.OauthAuthorizationCode.CreateAuthorizationCode(
		ctxDB,
		authCode,
		codePlain,
	); err != nil {
		logger.Error("CreateAuthorizationCode failed", "err", err)
		app.serverErrorResponse(logger, w, r, err)
		return
	}

	// ─── 6. Dynamic audit‑log (fail‑soft, inline) ──────────────────────────
	action, err := app.Models.Action.GetByName(ctxDB, "create_oauth_authorization_code")
	if err != nil || action == nil {
		if id, e := app.Models.Action.CreateIfNotExists(
			ctxDB,
			"create_oauth_authorization_code",
			"Issue OAuth2 authorization code",
		); e == nil {
			action = &data.Action{ID: id}
		}
	}

	entityType, err := app.Models.EntityType.GetByName(ctxDB, "oauth_authorization_code")
	if err != nil || entityType == nil {
		if id, e := app.Models.EntityType.CreateIfNotExists(
			ctxDB,
			"oauth_authorization_code",
			"OAuth2 authorization code",
		); e == nil {
			entityType = &data.EntityType{ID: id}
		}
	}

	if action != nil && entityType != nil {
		audit := data.AuditLog{
			ID:           uuid.New(),
			UserID:       &userID,
			ActionID:     action.ID,
			EntityTypeID: entityType.ID,
			EntityID:     authCode.ID.String(),
		}
		if err := app.Models.AuditLog.Insert(ctxDB, &audit); err != nil {
			logger.Warn("audit insert failed", "err", err)
		}
	}

	//----------------------------------------------------------------------
	// 7. Redirect with code (+state) – spec‑compliant headers
	//----------------------------------------------------------------------
	v := url.Values{}
	v.Set("code", codePlain)
	if state != "" {
		v.Set("state", state)
	}
	target := fmt.Sprintf("%s?%s", redirectURI, v.Encode())

	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")
	http.Redirect(w, r, target, http.StatusFound)

	logger.Info("authorization code issued",
		"user_id", userID, "client_id", publicClientID, "scope", rawScope)
}

// isRedirectAllowed checks whether the supplied redirect URI exactly matches one
// of the URIs registered on the client. The comparison is done in constant time
// to avoid leaking information via timing side‑channels.
//
// • Empty whitelist → deny all (defence‑in‑depth).
// • Uses crypto/subtle to ensure constant‑time equality.
// • Accepts the *exact* string stored in the DB (case‑sensitive, no trimming).
func isRedirectAllowed(c *data.OauthClient, uri string) bool {
	if len(c.AllowedRedirectURIs) == 0 || uri == "" {
		return false
	}

	// Constant‑time compare each candidate.
	for _, allowed := range c.AllowedRedirectURIs {
		if subtle.ConstantTimeCompare([]byte(uri), []byte(allowed)) == 1 {
			return true
		}
	}
	return false
}

// OauthConsentHandler records (or refreshes) a user’s consent for a specific OAuth2 client.
//
// Route:  POST /oauth/consent?client_id=<uuid>
// Body:   { "scopes": ["profile","email"] }   ← optional
//
// Behaviour
// ────────────────────────────────────────────────────────────────────────
// 1️ Caller must already be authenticated; AuthMiddleware injected ctxUserID (uuid.UUID).
// 2️ Validates client_id query‑param.
// 3️ Optionally decodes JSON body listing scopes (idempotent upsert).
// 4️ Persists/refreshes consent for 12 months.
// 5️ Writes best‑effort audit log (action “save_oauth_user_consent”).
// 6️ Responds 204 No‑Content on success.
func (app *Application) OauthConsentHandler(w http.ResponseWriter, r *http.Request) {
	logger := app.Logger.GetLoggerWithContext(r).WithFunctionName("OauthConsentHandler")

	// ─── 1. Trusted caller identity (uuid.UUID already in context) ────────────
	ctx := r.Context()
	userID, ok := ctx.Value(ctxUserID).(uuid.UUID) // type‑safe read
	if !ok || userID == uuid.Nil {
		app.respondWithError(w, errors.New("unauthorized"), http.StatusUnauthorized)
		return
	}

	// ─── 2. Validate client_id query parameter ───────────────────────────────
	rawClientID := r.URL.Query().Get("client_id")
	if rawClientID == "" {
		app.respondWithError(w, errors.New("client_id query parameter required"), http.StatusBadRequest)
		return
	}
	clientID, err := uuid.Parse(rawClientID)
	if err != nil {
		app.respondWithError(w, errors.New("invalid client_id"), http.StatusBadRequest)
		return
	}

	// ─── 3. Decode optional JSON body (scopes) ───────────────────────────────
	var body struct {
		Scopes []string `json:"scopes"` // free‑form list, stored verbatim
	}
	if err := app.readJSON(w, r, &body); err != nil && !errors.Is(err, io.EOF) {
		app.respondWithError(w, err, http.StatusBadRequest)
		return
	}

	// ─── 4. Upsert consent (1‑year TTL, idempotent) ──────────────────────────
	ctxDB, cancel := context.WithTimeout(ctx, cfgTimeout)
	defer cancel()

	consent := &data.OauthUserConsent{
		ID:        uuid.New(), // ignored on update
		UserID:    userID,
		ClientID:  clientID,
		Scopes:    body.Scopes,
		ExpiresAt: timeutil.Now().Add(365 * 24 * time.Hour),
	}

	if err := app.Models.OauthUserConsent.SaveUserConsent(ctxDB, consent); err != nil {
		logger.Error("SaveUserConsent failed", "err", err)
		app.serverErrorResponse(logger, w, r, err)
		return
	}

	// ------------------------------------------------------------------- //
	// 5. Dynamic audit log (fail‑soft)                                    //
	// ------------------------------------------------------------------- //

	// 5a. Resolve / create action “save_oauth_user_consent”.
	action, err := app.Models.Action.GetByName(ctxDB, "save_oauth_user_consent")
	if err != nil || action == nil {
		logger.Warn("audit action missing, attempting create", "err", err)
		if id, createErr := app.Models.Action.CreateIfNotExists(
			ctxDB,
			"save_oauth_user_consent",
			"Save OAuth2 user consent",
		); createErr == nil {
			action = &data.Action{ID: id}
		} else {
			logger.Error("failed to create audit action", "err", createErr)
		}
	}

	// 5b. Resolve / create entity‑type “oauth_user_consent”.
	entityType, err := app.Models.EntityType.GetByName(ctxDB, "oauth_user_consent")
	if err != nil || entityType == nil {
		logger.Warn("entity type missing, attempting create", "err", err)
		if id, createErr := app.Models.EntityType.CreateIfNotExists(
			ctxDB,
			"oauth_user_consent",
			"OAuth2 user consent",
		); createErr == nil {
			entityType = &data.EntityType{ID: id}
		} else {
			logger.Error("failed to create entity type", "err", createErr)
		}
	}

	// 5c. Insert audit log when metadata is available.
	if action != nil && entityType != nil {
		audit := data.AuditLog{
			ID:           uuid.New(),
			UserID:       &userID,
			ActionID:     action.ID,
			EntityTypeID: entityType.ID,
			EntityID:     consent.ID.String(),
		}
		if err := app.Models.AuditLog.Insert(ctxDB, &audit); err != nil {
			logger.Warn("audit log insertion failed", "err", err)
		}
	}

	logger.Info("user consent recorded",
		"user_id", userID,
		"client_id", clientID,
		"consent_id", consent.ID,
	)

	w.WriteHeader(http.StatusNoContent)
}
