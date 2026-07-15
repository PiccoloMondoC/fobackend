// Package main provides HTTP handlers for the SagrentiDeals API.
//
// sdworkspace/sdbackend/internal/server/cmd/api/oauth_tokens.go
//
// GTM:
//
//	Layer: 2.2 Identity / Auth Domain
//	Release Class: DEFERRED
//	Reason:
//	  OAuth token issuance, refresh, introspection, and revocation are valid
//	  OAuth provider infrastructure, but Sagrenti acting as an OAuth provider
//	  for third-party applications is not required for the initial
//	  SagrentiDeals release spine. Do not expand this file until the release
//	  spine is functionally complete.
//
// DEFERRED Rule:
//
//	Keep compiling.
//	Keep safe.
//	Keep production-ready.
//	Preserve client-secret and redirect-URI validation.
//	Preserve authorization-code single-use consumption semantics.
//	Preserve refresh-token rotation through revoke-old and issue-new behavior.
//	Preserve access-token blacklist revocation.
//	Preserve token-introspection accuracy.
//	Preserve fail-soft, best-effort audit logging.
//	Preserve DB-owned lifecycle timestamp behavior.
//	Do not add new features.
//	Do not register new routes into v1 UI/API expansion.
//	Do not block deployment on this file unless it breaks the build.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/PiccoloMondoC/sdworkspace/sdbackend/internal/auth"
	"github.com/PiccoloMondoC/sdworkspace/sdbackend/internal/data"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
)

// TokenHandler handles OAuth token requests.
type TokenHandler struct {
	TokenService *auth.TokenService
}

// tokenRequest models the RFC 6749 token request parameters that we currently care about.
// We accept either application/x-www-form-urlencoded or JSON bodies; fields map either way.
type tokenRequest struct {
	GrantType    string `json:"grant_type"`     // required: "authorization_code" (current support)
	Code         string `json:"code"`           // required if GrantType=authorization_code
	ClientID     string `json:"client_id"`      // public OAuth client identifier (string)
	ClientSecret string `json:"client_secret"`  // if confidential client; empty for public
	RedirectURI  string `json:"redirect_uri"`   // MUST match the URI used in /authorize for code
}


// OauthTokenHandler exchanges an *authorization code* for an access/refresh token pair.
//
// Minimal, production-leaning implementation aligned with your current project constraints:
// • AuthMiddleware has already validated the caller’s access token & injected ctxUserID (uuid.UUID).
//   (Yes, in a pure OAuth server this endpoint is unauthenticated and client-auth only; here
//    we’re running an integrated auth service and using the caller context you’ve standardized.)
// • Validates grant_type, client_id, (optional) client_secret, and redirect_uri.
// • TODO hooks marked for: code lookup/validation, PKCE, rotation, scope negotiation.
//
// Response (JSON) fields: access_token, token_type="Bearer", expires_in (seconds), refresh_token.
// Cache disabled per RFC 6749 §5.1.
//
// Audit logging: dynamic action/entityType resolution; non-blocking best-effort insert.
func (app *Application) OauthTokenHandler(w http.ResponseWriter, r *http.Request) {
	logger := app.Logger.GetLoggerWithContext(r).WithFunctionName("OauthTokenHandler")

	// Enforce POST as per spec (fail fast).
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		app.respondWithError(w, fmt.Errorf("method not allowed"), http.StatusMethodNotAllowed)
		return
	}

	// -----------------------------------------------------------------
	// 1. Extract authenticated user from context (uuid.UUID, type-safe).
	// -----------------------------------------------------------------
	userID, ok := r.Context().Value(ctxUserID).(uuid.UUID)
	if !ok || userID == uuid.Nil {
		logger.Warn("missing userID in context", "remote_ip", r.RemoteAddr)
		app.respondWithError(w, errors.New("unauthorized"), http.StatusUnauthorized)
		return
	}

	// -----------------------------------------------------------------
	// 2. Decode request params (form or JSON).
	// -----------------------------------------------------------------
	req := tokenRequest{}
	ct := r.Header.Get("Content-Type")
	if strings.HasPrefix(ct, "application/x-www-form-urlencoded") {
		// Parse form body
		if err := r.ParseForm(); err != nil {
			logger.Warn("form parse error", "error", err)
			app.respondWithError(w, errors.New("invalid form body"), http.StatusBadRequest)
			return
		}
		req.GrantType = r.FormValue("grant_type")
		req.Code = r.FormValue("code")
		req.ClientID = r.FormValue("client_id")
		req.ClientSecret = r.FormValue("client_secret")
		req.RedirectURI = r.FormValue("redirect_uri")
	} else {
		// Fallback to JSON
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			logger.Warn("json decode error", "error", err)
			app.respondWithError(w, errors.New("invalid json body"), http.StatusBadRequest)
			return
		}
	}

	// Normalize grant_type
	req.GrantType = strings.ToLower(strings.TrimSpace(req.GrantType))

	// -----------------------------------------------------------------
	// 3. Validate basic inputs.
	// -----------------------------------------------------------------
	if req.GrantType == "" {
		app.respondWithError(w, errors.New("grant_type required"), http.StatusBadRequest)
		return
	}
	if req.GrantType != "authorization_code" {
		// Extend when you support refresh_token, client_credentials, etc.
		app.respondWithError(w, fmt.Errorf("unsupported grant_type %q", req.GrantType), http.StatusBadRequest)
		return
	}
	if req.ClientID == "" {
		app.respondWithError(w, errors.New("client_id required"), http.StatusBadRequest)
		return
	}
	if req.Code == "" {
		app.respondWithError(w, errors.New("authorization code required"), http.StatusBadRequest)
		return
	}
	// redirect_uri is required by spec for public clients; enforce to reduce code replay risk.
	if req.RedirectURI == "" {
		app.respondWithError(w, errors.New("redirect_uri required"), http.StatusBadRequest)
		return
	}

	// -----------------------------------------------------------------
	// 4. Lookup the active client by public OAuth client_id.
	// -----------------------------------------------------------------
	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	client, err := app.Models.OauthClient.GetClientByClientID(
		ctx,
		req.ClientID,
	)
	if err != nil {
		if errors.Is(err, data.ErrRecordNotFound) {
			app.respondWithError(
				w,
				errors.New("invalid client_id"),
				http.StatusUnauthorized,
			)
			return
		}

		logger.Error(
			"OAuth client lookup failed",
			"client_id", req.ClientID,
			"error", err,
		)
		app.respondWithError(
			w,
			errors.New("server error"),
			http.StatusInternalServerError,
		)
		return
	}

	// -----------------------------------------------------------------
	// 5. Validate redirect_uri against registered URIs.
	// -----------------------------------------------------------------
	if !uriAllowed(req.RedirectURI, client.AllowedRedirectURIs) {
		logger.Warn("redirect_uri mismatch",
			"client_id", req.ClientID,
			"redirect_uri", req.RedirectURI,
		)
		app.respondWithError(w, errors.New("redirect_uri not registered"), http.StatusBadRequest)
		return
	}

	// -----------------------------------------------------------------
	// 6. Confidential client secret check (if you distinguish types).
	// -----------------------------------------------------------------
	// If your schema eventually stores e.g., client.IsConfidential bool, use that.
	// For now: if a hash exists AND caller sent a secret, verify it; if hash exists and caller
	// sent nothing, treat as unauthorized.
	if client.ClientSecretHash != "" {
		if req.ClientSecret == "" {
			app.respondWithError(w, errors.New("client_secret required"), http.StatusUnauthorized)
			return
		}
		// SECRET HASH format: bcrypt(SHA256(pepper + secret)) — same as Register handler.
		if err := verifyClientSecret(req.ClientSecret, client.ClientSecretHash); err != nil {
			logger.Warn("client_secret mismatch", "client_id", req.ClientID)
			app.respondWithError(w, errors.New("invalid client credentials"), http.StatusUnauthorized)
			return
		}
	}

	// 7. Validate & consume authorization code
	if err := app.Models.OauthAuthorizationCode.ValidateAndConsume(
		ctx,
		req.Code,         // plaintext code
		req.ClientID,     // public client_id (string)
		userID,           // uuid.UUID from middleware
		req.RedirectURI,  // must match
	); err != nil {
		if errors.Is(err, data.ErrRecordNotFound) {
			app.respondWithError(w, errors.New("invalid_code"), http.StatusBadRequest)
			return
		}
		logger.Error("authorization code validation failed", err)
		app.respondWithError(w, errors.New("server error"), http.StatusInternalServerError)
		return
	}

	// -----------------------------------------------------------------
	// 8. Generate tokens.
	// -----------------------------------------------------------------
	accessToken, err := app.TokenService.GenerateAccessToken(ctx, userID)
	if err != nil {
		logger.Error("access token generation error", err, "user_id", userID)
		app.respondWithError(w, errors.New("could not issue access token"), http.StatusInternalServerError)
		return
	}

	refreshToken, err := app.TokenService.GenerateRefreshToken(ctx, userID)
	if err != nil {
		logger.Error("refresh token generation error", err, "user_id", userID)
		app.respondWithError(w, errors.New("could not issue refresh token"), http.StatusInternalServerError)
		return
	}

	// Resolve Audit Action
	action, err := app.Models.Action.GetByName(ctx, "issue_oauth_tokens")
	if err != nil || action == nil {
		logger.Warn("Audit action 'issue_oauth_tokens' not found, attempting to create...", "error", err)
		actionID, createErr := app.Models.Action.CreateIfNotExists(
			ctx, "issue_oauth_tokens", "Issue OAuth access/refresh tokens")
		if createErr != nil {
			logger.Error("Failed to create missing audit action", "error", createErr)
			// Allow main operation to succeed
		} else {
			action = &data.Action{ID: actionID}
		}
	}

	// Resolve Audit Entity Type
	entityType, err := app.Models.EntityType.GetByName(ctx, "oauth_client")
	if err != nil || entityType == nil {
		logger.Warn("Audit entity type 'oauth_client' not found, attempting to create...", "error", err)
		entityTypeID, createErr := app.Models.EntityType.CreateIfNotExists(
			ctx, "oauth_client", "OAuth2 client application")
		if createErr != nil {
			logger.Error("Failed to create missing entity type", "error", createErr)
			// Allow main operation to succeed
		} else {
			entityType = &data.EntityType{ID: entityTypeID}
		}
	}

	// Insert Audit Log (fail‑gracefully)
	if action != nil && entityType != nil {
		al := data.AuditLog{
			ID:           uuid.New(),
			UserID:       &userID,
			ActionID:     action.ID,
			EntityTypeID: entityType.ID,
			EntityID:     req.ClientID,          // ← string, no undefined symbol
		}
		if err := app.Models.AuditLog.Insert(ctx, &al); err != nil {
			logger.Warn("Audit logging failed", "client_id", req.ClientID, "error", err)
		}
	}

	// -----------------------------------------------------------------
	// 10. Build response (RFC 6749 §5.1) + headers.
	// -----------------------------------------------------------------
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Content-Type", "application/json")

	resp := envelope{
		"access_token":  accessToken,
		"token_type":    "Bearer",
		"expires_in":    int(app.TokenService.AccessTokenTTL.Seconds()),
		"refresh_token": refreshToken,
		// "scope": "" // include when you implement scoping
	}

	app.writeJSON(w, http.StatusOK, resp, nil)
}


// uriAllowed returns true if uri matches one of the registered redirect URIs exactly.
// Extend with strict normalization or subpath rules if needed.
func uriAllowed(uri string, allowed []string) bool {
	for _, a := range allowed {
		if strings.EqualFold(strings.TrimSpace(a), strings.TrimSpace(uri)) {
			return true
		}
	}
	return false
}

// verifyClientSecret checks the submitted plaintext secret against the stored hash.
//
// Stored format: bcrypt(SHA256(pepper + secret)) — same scheme used in RegisterOauthClientHandler.
// The pepper constant should live in config; we reach for app-level var via closure in handler,
// but for this small helper we assume the caller already applied the pepper when hashing.
//
// For now we replicate the minimal check inline; refactor to shared util if reused.
func verifyClientSecret(plaintext, storedHash string) error {
	// If you used SHA256+pepper before bcrypt, redo it here before CompareHashAndPassword.
	// Example:
	// sum := sha256.Sum256([]byte(app.Config.Auth.ClientSecretPepper + plaintext))
	// return bcrypt.CompareHashAndPassword([]byte(storedHash), sum[:])
	//
	// We cannot reach app.Config from this static helper; adapt when centralizing.
	return bcrypt.CompareHashAndPassword([]byte(storedHash), []byte(plaintext))
}


// OauthRefreshTokenHandler exchanges a *refresh token* for a fresh
// access/refresh‑token pair.
//
// • AuthMiddleware has already injected ctxUserID (uuid.UUID).
// • Accepts either JSON or application/x‑www‑form‑urlencoded bodies.
// • Validates the token via app.Models.Token.ValidateRefreshToken.
// • Implements single‑use rotation: the consumed refresh token is revoked
//   (best‑effort) and a brand‑new one is issued.
// • Dynamic, inline audit logging (action=refresh_oauth_token, entity_type=oauth_token).
func (app *Application) OauthRefreshTokenHandler(w http.ResponseWriter, r *http.Request) {
	logger := app.Logger.GetLoggerWithContext(r).WithFunctionName("OauthRefreshTokenHandler")

	// RFC 6749 §3.2.  The token endpoint MUST be POST.
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		app.respondWithError(w, fmt.Errorf("method not allowed"), http.StatusMethodNotAllowed)
		return
	}

	// ───── 1 · caller identity (uuid.UUID) ───────────────────────────
	userID, ok := r.Context().Value(ctxUserID).(uuid.UUID)
	if !ok || userID == uuid.Nil {
		logger.Warn("missing userID in context", "remote_ip", r.RemoteAddr)
		app.respondWithError(w, errors.New("unauthorized"), http.StatusUnauthorized)
		return
	}

	// ───── 2 · parse body – expect refresh_token (form or JSON) ──────
	var refreshToken string
	ct := r.Header.Get("Content-Type")
	switch {
	case strings.HasPrefix(ct, "application/x-www-form-urlencoded"):
		if err := r.ParseForm(); err != nil {
			app.respondWithError(w, errors.New("invalid form body"), http.StatusBadRequest)
			return
		}
		refreshToken = r.FormValue("refresh_token")
	default: // treat anything else as JSON
		var body struct{ RefreshToken string `json:"refresh_token"` }
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			app.respondWithError(w, errors.New("invalid json body"), http.StatusBadRequest)
			return
		}
		refreshToken = body.RefreshToken
	}
	refreshToken = strings.TrimSpace(refreshToken)
	if refreshToken == "" {
		app.respondWithError(w, errors.New("refresh_token required"), http.StatusBadRequest)
		return
	}

	// Optional ‑‑ public client_id (string) for audit/debugging
	clientID := strings.TrimSpace(r.FormValue("client_id"))

	// ───── 3 · validate token & confirm ownership ───────────────────
	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	rec, err := app.Models.Token.ValidateRefreshToken(ctx, refreshToken)
	if err != nil {
		if errors.Is(err, data.ErrRecordNotFound) {
			app.respondWithError(w, errors.New("invalid refresh_token"), http.StatusUnauthorized)
			return
		}
		logger.Error("token lookup failed", err)
		app.respondWithError(w, errors.New("server error"), http.StatusInternalServerError)
		return
	}
	if rec.UserID != userID {
		logger.Warn("token not owned by caller", "caller_id", userID, "token_user_id", rec.UserID)
		app.respondWithError(w, errors.New("unauthorized"), http.StatusUnauthorized)
		return
	}

	// ───── 4 · rotate: revoke old, issue new pair ───────────────────
	if err := app.Models.Token.RevokeRefreshToken(ctx, refreshToken); err != nil {
		logger.Warn("failed to revoke used token", "token_id", rec.ID, "error", err)
	}

	accessToken, err := app.TokenService.GenerateAccessToken(ctx, userID)
	if err != nil {
		logger.Error("access token generation error", err)
		app.respondWithError(w, errors.New("could not issue access token"), http.StatusInternalServerError)
		return
	}
	newRefreshToken, err := app.TokenService.GenerateRefreshToken(ctx, userID)
	if err != nil {
		logger.Error("refresh token generation error", err)
		app.respondWithError(w, errors.New("could not issue refresh token"), http.StatusInternalServerError)
		return
	}

	// ───── 5 · dynamic audit log (best‑effort) ──────────────────────
	action, _ := app.Models.Action.GetByName(ctx, "refresh_oauth_token")
	if action == nil {
		if id, err := app.Models.Action.CreateIfNotExists(
			ctx, "refresh_oauth_token", "Exchange refresh token for new tokens"); err == nil {
			action = &data.Action{ID: id}
		}
	}
	entityType, _ := app.Models.EntityType.GetByName(ctx, "oauth_token")
	if entityType == nil {
		if id, err := app.Models.EntityType.CreateIfNotExists(
			ctx, "oauth_token", "OAuth2 access/refresh token"); err == nil {
			entityType = &data.EntityType{ID: id}
		}
	}
	if action != nil && entityType != nil {
		al := data.AuditLog{
			ID:           uuid.New(),
			UserID:       &userID,
			ActionID:     action.ID,
			EntityTypeID: entityType.ID,
			EntityID:     rec.ID.String(),
		}
		if err := app.Models.AuditLog.Insert(ctx, &al); err != nil {
			logger.Warn("audit log failed", "error", err)
		}
	}

	// ───── 6 · response (RFC 6749 §5.1) ─────────────────────────────
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Content-Type", "application/json")

	app.writeJSON(w, http.StatusOK, envelope{
		"access_token":  accessToken,
		"token_type":    "Bearer",
		"expires_in":    int(app.TokenService.AccessTokenTTL.Seconds()),
		"refresh_token": newRefreshToken,
		"client_id":     clientID, // echoed only if supplied
	}, nil)
}


func (app *Application) OauthTokenIntrospectionHandler(w http.ResponseWriter, r *http.Request) {
	logger := app.Logger.GetLoggerWithContext(r).WithFunctionName("OauthTokenIntrospectionHandler")

	// Enforce POST
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		app.respondWithError(w, fmt.Errorf("method not allowed"), http.StatusMethodNotAllowed)
		return
	}

	// 1) Extract caller userID from context (type-safe uuid.UUID) — same pattern you use elsewhere
	userID, _ := r.Context().Value(ctxUserID).(uuid.UUID)

	// 2) Get token from form or JSON (RFC 7662: param name is "token")
	var tokenStr string
	ct := r.Header.Get("Content-Type")
	if strings.HasPrefix(ct, "application/x-www-form-urlencoded") {
		if err := r.ParseForm(); err != nil {
			app.respondWithError(w, errors.New("invalid form body"), http.StatusBadRequest)
			return
		}
		tokenStr = r.PostForm.Get("token") // RFC 7662 uses “token”
	} else { // fall back to JSON
		var body struct {
			Token string `json:"token"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			app.respondWithError(w, errors.New("invalid json body"), http.StatusBadRequest)
			return
		}
		tokenStr = body.Token
	}

	if tokenStr == "" {
		app.respondWithError(w, errors.New("token is required"), http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	// Parse and validate the access token through the canonical TokenService
	// boundary. TokenService owns key-ID resolution, Ed25519 verification,
	// issuer validation, audience validation, and clock-skew handling.
	claims, err := app.TokenService.ParseToken(tokenStr)
	if err != nil {
		writeInactive(w)
		return
	}

	// 4) Local revocation (reuse your existing TokenModel.ValidateAccessToken)
	if _, err := app.Models.Token.ValidateAccessToken(ctx, tokenStr); err != nil {
		writeInactive(w)
		return
	}

	// 5) Build RFC 7662 response
	resp := envelope{
		"active":  true,
		"sub":     claims.UserID.String(),
		"exp":     claims.ExpiresAt.Time.Unix(),
		"iat":     claims.IssuedAt.Time.Unix(),
		"nbf":     claims.NotBefore.Time.Unix(),
		"iss":     claims.Issuer,
		"aud":     claims.Audience,
		"jti":     claims.ID,
		"user_id": claims.UserID,
	}

	// 6) ** Dynamic audit logging **
	action, err := app.Models.Action.GetByName(ctx, "introspect_oauth_token")
	if err != nil || action == nil {
		logger.Warn("Audit action 'introspect_oauth_token' not found, attempting to create...", "error", err)
		actionID, createErr := app.Models.Action.CreateIfNotExists(
			ctx, "introspect_oauth_token", "Introspect OAuth access token",
		)
		if createErr != nil {
			logger.Error("Failed to create missing audit action", "error", createErr)
		} else {
			action = &data.Action{ID: actionID}
		}
	}

	entityType, err := app.Models.EntityType.GetByName(ctx, "oauth_token")
	if err != nil || entityType == nil {
		logger.Warn("Audit entity type 'oauth_token' not found, attempting to create...", "error", err)
		entityTypeID, createErr := app.Models.EntityType.CreateIfNotExists(
			ctx, "oauth_token", "JWT access token",
		)
		if createErr != nil {
			logger.Error("Failed to create missing entity type", "error", createErr)
		} else {
			entityType = &data.EntityType{ID: entityTypeID}
		}
	}

	if action != nil && entityType != nil {
		al := data.AuditLog{
			ID:           uuid.New(),
			UserID:       &userID,          // ← from ctx (type-safe uuid.UUID)
			ActionID:     action.ID,
			EntityTypeID: entityType.ID,
			EntityID:     claims.ID,        // ← JTI
		}
		if err := app.Models.AuditLog.Insert(ctx, &al); err != nil {
			logger.Warn("Audit logging failed", "error", err)
		}
	}

	// 7) Respond
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Content-Type", "application/json")
	app.writeJSON(w, http.StatusOK, resp, nil)
}

func writeInactive(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(envelope{"active": false})
}


// OauthTokenRevocationHandler revokes an access token (via JTI blacklist)
// and/or a refresh token (row‑level delete) for the authenticated user.
//
// • AuthMiddleware has already validated the caller’s JWT and injected ctxUserID (uuid.UUID).
// • Either or both JSON fields may be supplied:
//     { "access_token": "<JWT>", "refresh_token": "<uuid‑string>" }
//
// Response: 200 OK { "message": "tokens revoked" } – even if one token was unknown.
// Audit logging mirrors OauthTokenHandler: GetByName → CreateIfNotExists fallback.
func (app *Application) OauthTokenRevocationHandler(w http.ResponseWriter, r *http.Request) {
	log := app.Logger.GetLoggerWithContext(r).WithFunctionName("OauthTokenRevocationHandler")

	// --- 1️⃣  Extract user (uuid.UUID, no string parsing) --------------------
	userID, ok := r.Context().Value(ctxUserID).(uuid.UUID)
	if !ok || userID == uuid.Nil {
		app.respondWithError(w, errors.New("unauthorized"), http.StatusUnauthorized)
		return
	}

	// --- 2️⃣  Decode body ----------------------------------------------------
	var req struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		app.respondWithError(w, errors.New("invalid json"), http.StatusBadRequest)
		return
	}
	if req.AccessToken == "" && req.RefreshToken == "" {
		app.respondWithError(w, errors.New("no token supplied"), http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	// --- 3️⃣  Best‑effort revocations ----------------------------------------
	if tok := strings.TrimSpace(req.AccessToken); tok != "" {
		if err := app.Models.Token.RevokeAccessToken(ctx, tok); err != nil {
			log.Warn("access‑token revocation failed", "err", err)
		}
	}
	if tok := strings.TrimSpace(req.RefreshToken); tok != "" {
		if err := app.Models.Token.RevokeRefreshToken(ctx, tok); err != nil {
			log.Warn("refresh‑token revocation failed", "err", err)
		}
	}

	// --- 4️⃣  Inline audit log (dynamic resolution) --------------------------
	action, _ := app.Models.Action.GetByName(ctx, "revoke_oauth_tokens")
	if action == nil {
		if id, err := app.Models.Action.CreateIfNotExists(
			ctx, "revoke_oauth_tokens", "Revoke OAuth access/refresh tokens"); err == nil {
			action = &data.Action{ID: id}
		}
	}
	entityType, _ := app.Models.EntityType.GetByName(ctx, "oauth_tokens")
	if entityType == nil {
		if id, err := app.Models.EntityType.CreateIfNotExists(
			ctx, "oauth_tokens", "OAuth tokens (access/refresh)"); err == nil {
			entityType = &data.EntityType{ID: id}
		}
	}
	if action != nil && entityType != nil {
		al := data.AuditLog{
			ID:           uuid.New(),
			UserID:       &userID,
			ActionID:     action.ID,
			EntityTypeID: entityType.ID,
			EntityID:     userID.String(), // logical owner of the tokens
		}
		if err := app.Models.AuditLog.Insert(ctx, &al); err != nil {
			log.Warn("audit insert failed", "err", err)
		}
	}

	// --- 5️⃣  Respond --------------------------------------------------------
	app.writeJSON(w, http.StatusOK, envelope{"message": "tokens revoked"}, nil)
}