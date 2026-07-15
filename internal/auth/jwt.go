// Package auth provides authentication utilities for issuing and validating
// access tokens, issuing persisted refresh tokens, and validating third-party
// identity-provider tokens.
//
// sdworkspace/sdbackend/internal/auth/jwt.go
//
// GTM:
//
//	Layer: 2.1 Database / Governance Foundation
//	Release Class: SPINE
//	Reason:
//	  JWT and refresh-token orchestration are release-critical authentication
//	  infrastructure. This file issues EdDSA/Ed25519 signed access tokens,
//	  creates persisted refresh-token records for revocation and rotation,
//	  validates configured issuer/audience boundaries, supports public-key
//	  verification for distributed service trust, and verifies external
//	  identity-provider tokens used during authentication.
//
// SPINE Rule:
//
//	Keep compiling.
//	Keep production-ready.
//	Preserve EdDSA/Ed25519 access-token signing.
//	Preserve public-key JWT validation.
//	Preserve explicit rejection of non-EdDSA JWT algorithms.
//	Preserve mandatory refresh-token persistence.
//	Preserve issuer/audience validation.
//	Preserve UTC token expiry handling.
//	Preserve protected token lifecycle semantics.
//	Preserve third-party identity-token verification boundaries.
//	Block deployment if this file breaks authentication, token generation,
//	refresh-token persistence, revocation readiness, external login validation,
//	Ed25519 key-boundary enforcement, or protected token lifecycle integrity.
package auth

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/PiccoloMondoC/sdworkspace/sdbackend/internal/data"
	"github.com/PiccoloMondoC/sdworkspace/sdbackend/internal/logging"
	"github.com/PiccoloMondoC/sdworkspace/sdbackend/internal/security"
	"github.com/PiccoloMondoC/sdworkspace/sdbackend/internal/security/jwtutil"
	"github.com/PiccoloMondoC/sdworkspace/sdbackend/internal/utils/timeutil"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"
)

const (
	googleTokenInfoURL    = "https://oauth2.googleapis.com/tokeninfo"
	facebookDebugTokenURL = "https://graph.facebook.com/debug_token"

	identityProviderHTTPTimeout = 5 * time.Second

	// maxClockSkewLeeway tolerates small clock drift between issuer and verifier
	// nodes without materially extending short-lived access-token validity.
	maxClockSkewLeeway = 30 * time.Second
)

var (
	defaultTokenTracer = otel.Tracer("sd/internal/auth")

	identityProviderClient = &http.Client{
		Timeout: identityProviderHTTPTimeout,
	}

	errTokenServiceMisconfigured = errors.New("token service is misconfigured")
)

func wrapMisconfigured(detail string) error {
	return fmt.Errorf("%w: %s", errTokenServiceMisconfigured, detail)
}

// TokenService issues Sagrenti access and refresh tokens and validates
// access-token signatures using Ed25519 public keys.
type TokenService struct {
	DB              *pgxpool.Pool
	PrivateKey      ed25519.PrivateKey
	PublicKeys      map[string]ed25519.PublicKey
	SigningKeyID    string
	AccessTokenTTL  time.Duration
	RefreshTokenTTL time.Duration
	Issuer          string
	Audience        string
	Logger          *logging.Logger
	Tracer          trace.Tracer
	TokenModel      *data.TokenModel
}

// NewTokenService constructs a token service with normalized key IDs,
// issuer, and audience values.
func NewTokenService(
	db *pgxpool.Pool,
	privateKey ed25519.PrivateKey,
	publicKeys map[string]ed25519.PublicKey,
	signingKeyID string,
	accessTTL, refreshTTL time.Duration,
	issuer, audience string,
	logger *logging.Logger,
	tracer trace.Tracer,
	tokenModel *data.TokenModel,
) (*TokenService, error) {
	if tracer == nil {
		tracer = defaultTokenTracer
	}

	clonedKeys, err := clonePublicKeys(publicKeys)
	if err != nil {
		return nil, fmt.Errorf("new token service: %w", err)
	}

	return &TokenService{
		DB:              db,
		PrivateKey:      privateKey,
		PublicKeys:      clonedKeys,
		SigningKeyID:    strings.TrimSpace(signingKeyID),
		AccessTokenTTL:  accessTTL,
		RefreshTokenTTL: refreshTTL,
		Issuer:          strings.TrimSpace(issuer),
		Audience:        strings.TrimSpace(audience),
		Logger:          logger,
		Tracer:          tracer,
		TokenModel:      tokenModel,
	}, nil
}

// GenerateTokensPair creates an EdDSA access token and a persisted refresh token.
func (ts *TokenService) GenerateTokensPair(ctx context.Context, userID uuid.UUID) (string, string, error) {
	ctx, span := ts.startSpan(ctx, "auth.GenerateTokensPair")
	defer span.End()

	if err := ts.validateTokenService(true, true); err != nil {
		ts.logError(ctx, "GenerateTokensPair", "token service validation failed", err, "user_id", userID)
		return "", "", err
	}

	accessToken, err := ts.GenerateAccessToken(ctx, userID)
	if err != nil {
		ts.logError(ctx, "GenerateTokensPair", "access token generation failed", err, "user_id", userID)
		return "", "", err
	}

	refreshToken, err := ts.GenerateRefreshToken(ctx, userID)
	if err != nil {
		ts.logError(ctx, "GenerateTokensPair", "refresh token generation failed", err, "user_id", userID)
		return "", "", err
	}

	ts.logInfo(ctx, "GenerateTokensPair", "token pair generated", "user_id", userID)
	return accessToken, refreshToken, nil
}

// GenerateAccessToken creates an EdDSA JWT using the canonical jwtutil signer.
func (ts *TokenService) GenerateAccessToken(ctx context.Context, userID uuid.UUID) (string, error) {
	ctx, span := ts.startSpan(ctx, "auth.GenerateAccessToken")
	defer span.End()

	if err := ts.validateTokenService(true, false); err != nil {
		ts.logError(ctx, "GenerateAccessToken", "token service validation failed", err, "user_id", userID)
		return "", err
	}

	return jwtutil.GenerateAccessToken(
		ts.PrivateKey,
		userID,
		ts.AccessTokenTTL,
		ts.Issuer,
		ts.Audience,
		jwtutil.WithKeyID(ts.SigningKeyID),
	)
}

// GenerateRefreshToken creates a refresh token and persists it through TokenModel.
func (ts *TokenService) GenerateRefreshToken(
	ctx context.Context,
	userID uuid.UUID,
) (string, error) {
	ctx, span := ts.startSpan(ctx, "auth.GenerateRefreshToken")
	defer span.End()

	if err := ts.validateTokenService(false, true); err != nil {
		ts.logError(
			ctx,
			"GenerateRefreshToken",
			"token service validation failed",
			err,
			"user_id",
			userID,
		)
		return "", err
	}

	if userID == uuid.Nil {
		return "", jwtutil.ErrUserIDRequired
	}

	refreshToken, err := security.GenerateRefreshToken()
	if err != nil {
		return "", fmt.Errorf("generate refresh token: %w", err)
	}

	expiresAt := timeutil.Now().Add(ts.RefreshTokenTTL)

	if _, err := ts.TokenModel.StoreRefreshToken(
		ctx,
		userID,
		refreshToken,
		expiresAt,
	); err != nil {
		return "", fmt.Errorf("store refresh token: %w", err)
	}

	return refreshToken, nil
}

// ParseToken validates an EdDSA JWT and returns canonical Sagrenti claims.
func (ts *TokenService) ParseToken(tokenStr string) (*jwtutil.Claims, error) {
	if err := ts.validateTokenService(false, false); err != nil {
		return nil, err
	}

	kid, err := jwtutil.KeyID(tokenStr)
	if err != nil {
		return nil, err
	}
	if kid == "" {
		return nil, jwtutil.ErrInvalidKeyID
	}

	publicKey, ok := ts.PublicKeys[kid]
	if !ok {
		return nil, jwtutil.ErrInvalidToken
	}

	return jwtutil.ParseAndValidate(
		tokenStr,
		publicKey,
		jwtutil.WithAudience(ts.Audience),
		jwtutil.WithIssuer(ts.Issuer),
		jwtutil.WithClockSkew(maxClockSkewLeeway),
	)
}

// ValidateToken reports whether tokenStr is a valid Sagrenti access token.
func (ts *TokenService) ValidateToken(tokenStr string) bool {
	_, err := ts.ParseToken(tokenStr)
	return err == nil
}

// GoogleIDTokenInfo models the Google tokeninfo response used for ID-token validation.
type GoogleIDTokenInfo struct {
	Sub           string `json:"sub"`
	Email         string `json:"email"`
	EmailVerified string `json:"email_verified"`
	Aud           string `json:"aud"`
	Iss           string `json:"iss"`
	Exp           string `json:"exp"`
}

// VerifyGoogleToken validates a Google ID token and returns its verified email.
func (ts *TokenService) VerifyGoogleToken(ctx context.Context, idToken, expectedClientID string) (string, error) {
	ctx, span := ts.startSpan(ctx, "auth.VerifyGoogleToken")
	defer span.End()

	email, err := verifyGoogleToken(ctx, idToken, expectedClientID)
	if err != nil {
		if errors.Is(err, data.ErrInvalidCredentials) {
			ts.logInfo(ctx, "VerifyGoogleToken", "google id token rejected")
		} else {
			ts.logError(ctx, "VerifyGoogleToken", "google token verification failed", err)
		}
		return "", err
	}

	ts.logInfo(ctx, "VerifyGoogleToken", "google token verified")
	return email, nil
}

func verifyGoogleToken(ctx context.Context, idToken, expectedClientID string) (string, error) {
	idToken = strings.TrimSpace(idToken)
	expectedClientID = strings.TrimSpace(expectedClientID)

	if idToken == "" || expectedClientID == "" {
		return "", data.ErrInvalidCredentials
	}

	endpoint, err := url.Parse(googleTokenInfoURL)
	if err != nil {
		return "", fmt.Errorf("parse google tokeninfo endpoint: %w", err)
	}

	q := endpoint.Query()
	q.Set("id_token", idToken)
	endpoint.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return "", fmt.Errorf("create google token verification request: %w", err)
	}

	resp, err := identityProviderClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("verify google token: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		if resp.StatusCode >= http.StatusInternalServerError {
			return "", fmt.Errorf("google tokeninfo returned server error %d", resp.StatusCode)
		}
		return "", fmt.Errorf("google tokeninfo returned status %d: %w", resp.StatusCode, data.ErrInvalidCredentials)
	}

	var tokenInfo GoogleIDTokenInfo
	if err := json.NewDecoder(resp.Body).Decode(&tokenInfo); err != nil {
		return "", fmt.Errorf("decode google tokeninfo response: %w", err)
	}

	if tokenInfo.Aud != expectedClientID {
		return "", data.ErrInvalidCredentials
	}

	if tokenInfo.Iss != "accounts.google.com" && tokenInfo.Iss != "https://accounts.google.com" {
		return "", data.ErrInvalidCredentials
	}

	if tokenInfo.Email == "" || tokenInfo.EmailVerified != "true" {
		return "", data.ErrInvalidCredentials
	}

	exp, err := strconv.ParseInt(tokenInfo.Exp, 10, 64)
	if err != nil {
		return "", data.ErrInvalidCredentials
	}

	if timeutil.Now().After(time.Unix(exp, 0).UTC()) {
		return "", data.ErrInvalidCredentials
	}

	return tokenInfo.Email, nil
}

// FacebookDebugTokenResponse models the Facebook debug_token validation response.
type FacebookDebugTokenResponse struct {
	Data struct {
		AppID     string `json:"app_id"`
		UserID    string `json:"user_id"`
		IsValid   bool   `json:"is_valid"`
		ExpiresAt int64  `json:"expires_at"`
		Error     *struct {
			Message string `json:"message"`
		} `json:"error,omitempty"`
	} `json:"data"`
}

// VerifyFacebookToken validates a Facebook access token and returns its user ID.
func (ts *TokenService) VerifyFacebookToken(ctx context.Context, accessToken, appToken, expectedAppID string) (string, error) {
	ctx, span := ts.startSpan(ctx, "auth.VerifyFacebookToken")
	defer span.End()

	userID, err := verifyFacebookToken(ctx, accessToken, appToken, expectedAppID)
	if err != nil {
		if errors.Is(err, data.ErrInvalidCredentials) {
			ts.logInfo(ctx, "VerifyFacebookToken", "facebook access token rejected")
		} else {
			ts.logError(ctx, "VerifyFacebookToken", "facebook token verification failed", err)
		}
		return "", err
	}

	ts.logInfo(ctx, "VerifyFacebookToken", "facebook token verified")
	return userID, nil
}

func verifyFacebookToken(ctx context.Context, accessToken, appToken, expectedAppID string) (string, error) {
	accessToken = strings.TrimSpace(accessToken)
	appToken = strings.TrimSpace(appToken)
	expectedAppID = strings.TrimSpace(expectedAppID)

	if accessToken == "" || appToken == "" || expectedAppID == "" {
		return "", data.ErrInvalidCredentials
	}

	endpoint, err := url.Parse(facebookDebugTokenURL)
	if err != nil {
		return "", fmt.Errorf("parse facebook debug-token endpoint: %w", err)
	}

	q := endpoint.Query()
	q.Set("input_token", accessToken)
	q.Set("access_token", appToken)
	endpoint.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return "", fmt.Errorf("create facebook token verification request: %w", err)
	}

	resp, err := identityProviderClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("verify facebook token: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		if resp.StatusCode >= http.StatusInternalServerError {
			return "", fmt.Errorf("facebook debug_token returned server error %d", resp.StatusCode)
		}
		return "", fmt.Errorf("facebook debug_token returned status %d: %w", resp.StatusCode, data.ErrInvalidCredentials)
	}

	var tokenResp FacebookDebugTokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return "", fmt.Errorf("decode facebook debug-token response: %w", err)
	}

	if tokenResp.Data.Error != nil {
		return "", data.ErrInvalidCredentials
	}

	if !tokenResp.Data.IsValid || tokenResp.Data.AppID != expectedAppID || tokenResp.Data.UserID == "" {
		return "", data.ErrInvalidCredentials
	}

	if tokenResp.Data.ExpiresAt > 0 && timeutil.Now().After(time.Unix(tokenResp.Data.ExpiresAt, 0).UTC()) {
		return "", data.ErrInvalidCredentials
	}

	return tokenResp.Data.UserID, nil
}

func (ts *TokenService) validateTokenService(requirePrivateKey, requireRefreshModel bool) error {
	if ts == nil {
		return wrapMisconfigured("receiver is nil")
	}

	if requirePrivateKey {
		if len(ts.PrivateKey) != ed25519.PrivateKeySize {
			return wrapMisconfigured("ed25519 private key has wrong length")
		}

		if strings.TrimSpace(ts.SigningKeyID) == "" {
			return wrapMisconfigured("signing key ID must not be empty")
		}
	}

	if len(ts.PublicKeys) == 0 {
		return wrapMisconfigured("at least one ed25519 public key is required")
	}

	for kid, publicKey := range ts.PublicKeys {
		if strings.TrimSpace(kid) == "" {
			return wrapMisconfigured("public key map contains a blank key ID")
		}

		if len(publicKey) != ed25519.PublicKeySize {
			return wrapMisconfigured(fmt.Sprintf("public key for kid %q has wrong length", kid))
		}
	}

	if requirePrivateKey {
		if _, ok := ts.PublicKeys[strings.TrimSpace(ts.SigningKeyID)]; !ok {
			return wrapMisconfigured("signing key ID must match a configured public key")
		}
	}

	if ts.AccessTokenTTL <= 0 {
		return wrapMisconfigured("access token TTL must be greater than zero")
	}

	if requireRefreshModel {
		if ts.RefreshTokenTTL <= 0 {
			return wrapMisconfigured("refresh token TTL must be greater than zero")
		}

		if ts.TokenModel == nil {
			return wrapMisconfigured("token model is required for refresh token persistence")
		}
	}

	if strings.TrimSpace(ts.Issuer) == "" || strings.TrimSpace(ts.Audience) == "" {
		return wrapMisconfigured("issuer and audience must not be empty")
	}

	return nil
}

func (ts *TokenService) startSpan(ctx context.Context, name string) (context.Context, trace.Span) {
	if ts != nil && ts.Tracer != nil {
		return ts.Tracer.Start(ctx, name)
	}

	return defaultTokenTracer.Start(ctx, name)
}

func (ts *TokenService) logInfo(ctx context.Context, functionName, msg string, keysAndValues ...interface{}) {
	if ts == nil || ts.Logger == nil {
		return
	}

	ts.Logger.GetLoggerWithContextFromContext(ctx).
		WithFunctionName(functionName).
		Info(msg, keysAndValues...)
}

func (ts *TokenService) logError(ctx context.Context, functionName, msg string, err error, keysAndValues ...interface{}) {
	if ts == nil || ts.Logger == nil {
		return
	}

	args := append([]interface{}{"error", err}, keysAndValues...)

	ts.Logger.GetLoggerWithContextFromContext(ctx).
		WithFunctionName(functionName).
		Error(msg, args...)
}

func clonePublicKeys(publicKeys map[string]ed25519.PublicKey) (map[string]ed25519.PublicKey, error) {
	if len(publicKeys) == 0 {
		return nil, wrapMisconfigured("at least one ed25519 public key is required")
	}

	cloned := make(map[string]ed25519.PublicKey, len(publicKeys))

	for kid, publicKey := range publicKeys {
		normalizedKid := strings.TrimSpace(kid)
		if normalizedKid == "" {
			return nil, wrapMisconfigured("public key map contains a blank key ID")
		}

		if _, exists := cloned[normalizedKid]; exists {
			return nil, fmt.Errorf("public key kid collision after whitespace normalization: %q", normalizedKid)
		}

		if len(publicKey) != ed25519.PublicKeySize {
			return nil, wrapMisconfigured(fmt.Sprintf("public key for kid %q has wrong length", normalizedKid))
		}

		cloned[normalizedKid] = append(ed25519.PublicKey(nil), publicKey...)
	}

	return cloned, nil
}