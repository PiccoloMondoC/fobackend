// Package jwtutil provides reusable utilities for working with JWT access tokens,
// including parsing, validation, claim extraction, and EdDSA access-token
// generation.
//
// This package is importable by auth, data, middleware, and service packages
// without creating circular dependencies. It owns low-level JWT behavior only;
// persistence, revocation lookup, role resolution, refresh-token generation,
// random-token generation, and audit logging belong to callers at the appropriate
// application layer.
//
// sdworkspace/sdbackend/internal/security/jwtutil/token.go
//
// GTM:
//   Layer: 2.2 Identity / Auth Domain
//   Release Class: SPINE
//   Reason:
//     JWT utilities are release-critical authentication infrastructure. They
//     enforce token parsing, EdDSA signing-method validation, required time
//     claims, audience/issuer checks, subject/user_id consistency, JTI integrity,
//     key ID readiness, and access-token generation required by the initial
//     SagrentiDeals release spine.
//
// SPINE Rule:
//   Keep compiling.
//   Keep production-ready.
//   Preserve circular-dependency-safe JWT utility ownership.
//   Preserve EdDSA / Ed25519 signing-method enforcement.
//   Preserve public-key validation for token verification.
//   Preserve private-key validation for token signing.
//   Preserve expiration, issued-at, and not-before validation.
//   Preserve audience and issuer enforcement.
//   Preserve user_id, subject, and JTI consistency checks.
//   Preserve access-token generation invariants.
//   Block deployment if this file breaks build, JWT parsing,
//   token validation, access-token generation, or authentication integrity.
package jwtutil

import (
	"crypto/ed25519"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

const (
	defaultClockSkew = 30 * time.Second
	maxClockSkew     = 5 * time.Minute
)

func utcNow() time.Time {
	return time.Now().UTC()
}

var (
	// ErrTokenRequired indicates that no JWT string was provided.
	ErrTokenRequired = errors.New("jwtutil: token required")

	// ErrInvalidToken is the top-level sentinel for JWT parse or validation failure.
	ErrInvalidToken = errors.New("jwtutil: invalid token")

	// ErrMalformedToken indicates that the JWT structure cannot be parsed.
	ErrMalformedToken = errors.New("jwtutil: malformed token")

	// ErrExpiredToken indicates that the exp claim is in the past.
	ErrExpiredToken = errors.New("jwtutil: token expired")

	// ErrTokenNotYetValid indicates that the nbf claim is in the future.
	ErrTokenNotYetValid = errors.New("jwtutil: token not yet valid")

	// ErrTokenUsedBeforeIssued indicates that the token is being used before its iat claim.
	ErrTokenUsedBeforeIssued = errors.New("jwtutil: token used before issued")

	// ErrTokenSignatureInvalid indicates that signature verification failed.
	ErrTokenSignatureInvalid = errors.New("jwtutil: token signature invalid")

	// ErrTokenUnverifiable indicates that the token cannot be verified with the supplied key material.
	ErrTokenUnverifiable = errors.New("jwtutil: token unverifiable")

	// ErrUnexpectedSigningMethod indicates that the JWT alg is not EdDSA.
	ErrUnexpectedSigningMethod = errors.New("jwtutil: unexpected signing method")

	// ErrTokenSigningFailed indicates that EdDSA token signing failed.
	ErrTokenSigningFailed = errors.New("jwtutil: token signing failed")

	// ErrPublicKeyRequired indicates that token validation was attempted without an Ed25519 public key.
	ErrPublicKeyRequired = errors.New("jwtutil: Ed25519 public key required")

	// ErrInvalidPublicKey indicates that the supplied Ed25519 public key has an invalid shape.
	ErrInvalidPublicKey = errors.New("jwtutil: invalid Ed25519 public key")

	// ErrPrivateKeyRequired indicates that token generation was attempted without an Ed25519 private key.
	ErrPrivateKeyRequired = errors.New("jwtutil: Ed25519 private key required")

	// ErrInvalidPrivateKey indicates that the supplied Ed25519 private key has an invalid shape.
	ErrInvalidPrivateKey = errors.New("jwtutil: invalid Ed25519 private key")

	// ErrClockSkewNegative indicates that the configured validation leeway is negative.
	ErrClockSkewNegative = errors.New("jwtutil: clock skew cannot be negative")

	// ErrClockSkewTooLarge indicates that the configured validation leeway exceeds the allowed bound.
	ErrClockSkewTooLarge = errors.New("jwtutil: clock skew too large")

	// ErrMissingExpiresAt indicates that the required exp claim is absent.
	ErrMissingExpiresAt = errors.New("jwtutil: missing exp claim")

	// ErrMissingIssuedAt indicates that the required iat claim is absent.
	ErrMissingIssuedAt = errors.New("jwtutil: missing iat claim")

	// ErrMissingNotBefore indicates that the required nbf claim is absent.
	ErrMissingNotBefore = errors.New("jwtutil: missing nbf claim")

	// ErrMissingAudience indicates that audience enforcement was requested but the aud claim is absent.
	ErrMissingAudience = errors.New("jwtutil: missing audience claim")

	// ErrInvalidAudienceOption indicates that WithAudience was configured with an empty canonical value.
	ErrInvalidAudienceOption = errors.New("jwtutil: invalid audience option")

	// ErrAudienceRequired indicates that token generation was requested without a canonical audience.
	ErrAudienceRequired = errors.New("jwtutil: audience required")

	// ErrAudienceMismatch indicates that the token audience does not match the expected audience.
	ErrAudienceMismatch = errors.New("jwtutil: audience mismatch")

	// ErrMissingIssuer indicates that issuer enforcement was requested but the iss claim is absent.
	ErrMissingIssuer = errors.New("jwtutil: missing issuer claim")

	// ErrInvalidIssuerOption indicates that WithIssuer was configured with an empty canonical value.
	ErrInvalidIssuerOption = errors.New("jwtutil: invalid issuer option")

	// ErrIssuerRequired indicates that token generation was requested without a canonical issuer.
	ErrIssuerRequired = errors.New("jwtutil: issuer required")

	// ErrIssuerMismatch indicates that the token issuer does not match the expected issuer.
	ErrIssuerMismatch = errors.New("jwtutil: issuer mismatch")

	// ErrMissingUserID indicates that the application user_id claim is absent or nil.
	ErrMissingUserID = errors.New("jwtutil: missing or invalid user_id claim")

	// ErrMissingSubject indicates that the registered subject claim is absent.
	ErrMissingSubject = errors.New("jwtutil: missing subject claim")

	// ErrSubjectUserMismatch indicates that sub and user_id do not identify the same user.
	ErrSubjectUserMismatch = errors.New("jwtutil: subject/user_id mismatch")

	// ErrMissingJTI indicates that the registered JWT ID claim is absent.
	ErrMissingJTI = errors.New("jwtutil: missing jti claim")

	// ErrInvalidJTI indicates that the JWT ID claim is not a valid UUID.
	ErrInvalidJTI = errors.New("jwtutil: jti must be valid UUID")

	// ErrUserIDRequired indicates that token generation was requested without a user ID.
	ErrUserIDRequired = errors.New("jwtutil: userID required")

	// ErrTTLRequired indicates that token generation was requested without a positive TTL.
	ErrTTLRequired = errors.New("jwtutil: ttl must be positive")

	// ErrInvalidKeyID indicates that the JWT kid header value is malformed.
	ErrInvalidKeyID = errors.New("jwtutil: invalid key id")
)

// Claims wraps jwt.RegisteredClaims with Sagrenti application-specific fields.
//
// UserID mirrors the registered subject claim as a typed UUID.
// RegisteredClaims.ID is the JWT ID / JTI and must also be a UUID.
type Claims struct {
	UserID uuid.UUID `json:"user_id"`
	jwt.RegisteredClaims
}

type parseOptions struct {
	expectedAud string
	expectedIss string
	clockSkew   time.Duration
	audienceSet bool
	issuerSet   bool
}

type signOptions struct {
	keyID string
}

// ParseOption configures ParseAndValidate.
type ParseOption func(*parseOptions)

// SignOption configures GenerateAccessToken.
type SignOption func(*signOptions)

// WithAudience enforces an exact match against the JWT audience claim.
// Empty or whitespace-only values are invalid.
func WithAudience(audience string) ParseOption {
	return func(o *parseOptions) {
		o.expectedAud = strings.TrimSpace(audience)
		o.audienceSet = true
	}
}

// WithIssuer enforces an exact match against the JWT issuer claim.
// Empty or whitespace-only values are invalid.
func WithIssuer(issuer string) ParseOption {
	return func(o *parseOptions) {
		o.expectedIss = strings.TrimSpace(issuer)
		o.issuerSet = true
	}
}

// WithClockSkew sets bounded leeway for time-based claim verification.
func WithClockSkew(skew time.Duration) ParseOption {
	return func(o *parseOptions) {
		o.clockSkew = skew
	}
}

// WithKeyID sets the JWT header kid value for key-rotation readiness.
func WithKeyID(keyID string) SignOption {
	return func(o *signOptions) {
		o.keyID = strings.TrimSpace(keyID)
	}
}

// ParseAndValidate parses an EdDSA JWT and enforces Sagrenti access-token
// invariants.
//
// Audience and issuer enforcement are opt-in at this low-level utility boundary.
// Callers that validate Sagrenti production access tokens should normally pass
// WithAudience and WithIssuer so cryptographically valid tokens from the wrong
// audience or issuer are rejected. When those options are omitted, this function
// validates signature, signing algorithm, required time claims, subject/user_id
// consistency, and JTI integrity, but it does not reject a token solely because
// aud or iss is missing or different.
//
// Revocation and authorization checks are intentionally excluded. Callers must
// validate blacklist/revocation state and permission scope after this low-level
// token validation succeeds.
//
// Error ownership note:
// jwtutil owns JWT-specific sentinel errors because this package is a stable
// security utility boundary. It must remain circular-dependency safe and must
// not import sd/internal/data. Higher layers may map these sentinels to broader
// centralized application errors where needed.
//
// Time source note:
// This low-level security package uses its local canonical UTC helper to avoid
// pulling higher-level dependencies into jwtutil. The same frozen timestamp is
// passed to the JWT parser through jwt.WithTimeFunc so library validation and
// post-parse validation operate against one consistent reference time.
func ParseAndValidate(tokenStr string, publicKey ed25519.PublicKey, opts ...ParseOption) (*Claims, error) {
	tokenStr = strings.TrimSpace(tokenStr)
	if tokenStr == "" {
		return nil, ErrTokenRequired
	}
	if err := validatePublicKey(publicKey); err != nil {
		return nil, err
	}

	cfg := parseOptions{clockSkew: defaultClockSkew}
	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}
	if err := validateParseOptions(cfg); err != nil {
		return nil, err
	}

	claims := &Claims{}
	now := utcNow()

	token, err := jwt.ParseWithClaims(
		tokenStr,
		claims,
		func(t *jwt.Token) (any, error) {
			if t.Method != jwt.SigningMethodEdDSA {
				return nil, fmt.Errorf("%w: %q", ErrUnexpectedSigningMethod, t.Header["alg"])
			}
			return publicKey, nil
		},
		jwt.WithValidMethods([]string{jwt.SigningMethodEdDSA.Alg()}),
		jwt.WithLeeway(cfg.clockSkew),
		jwt.WithExpirationRequired(),
		jwt.WithIssuedAt(),
		jwt.WithTimeFunc(func() time.Time {
			return now
		}),
	)
	if err != nil {
		return nil, mapParseError(err)
	}
	if token == nil || !token.Valid {
		return nil, ErrInvalidToken
	}

	if err := validateRequiredTimeClaims(claims); err != nil {
		return nil, err
	}
	if err := validateExpectedAudience(claims, cfg); err != nil {
		return nil, err
	}
	if err := validateExpectedIssuer(claims, cfg); err != nil {
		return nil, err
	}
	if err := validateApplicationClaims(claims); err != nil {
		return nil, err
	}

	return claims, nil
}

// GenerateAccessToken issues a single-audience EdDSA JWT for userID valid for ttl.
//
// The private key must remain limited to the issuing authority. Validators should
// receive only the corresponding Ed25519 public key.
//
// Multi-audience tokens are intentionally not supported by this function. If
// future service-to-service or OAuth flows require multiple audiences, add a
// separate explicit generator rather than widening this contract silently.
func GenerateAccessToken(
	privateKey ed25519.PrivateKey,
	userID uuid.UUID,
	ttl time.Duration,
	issuer string,
	audience string,
	opts ...SignOption,
) (string, error) {
	if err := validatePrivateKey(privateKey); err != nil {
		return "", err
	}

	issuer = strings.TrimSpace(issuer)
	audience = strings.TrimSpace(audience)

	switch {
	case userID == uuid.Nil:
		return "", ErrUserIDRequired
	case ttl <= 0:
		return "", ErrTTLRequired
	case issuer == "":
		return "", ErrIssuerRequired
	case audience == "":
		return "", ErrAudienceRequired
	}

	signCfg := signOptions{}
	for _, opt := range opts {
		if opt != nil {
			opt(&signCfg)
		}
	}
	if err := validateSignOptions(signCfg); err != nil {
		return "", err
	}

	now := utcNow()

	claims := &Claims{
		UserID: userID,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    issuer,
			Subject:   userID.String(),
			Audience:  jwt.ClaimStrings{audience},
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(ttl)),
			ID:        uuid.NewString(),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodEdDSA, claims)
	if signCfg.keyID != "" {
		token.Header["kid"] = signCfg.keyID
	}

	signed, err := token.SignedString(privateKey)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrTokenSigningFailed, err)
	}

	return signed, nil
}

// KeyID extracts the JWT header kid value without validating the token.
//
// This is intended only for key-selection before full signature validation.
// Callers must still validate the token with ParseAndValidate after selecting
// the appropriate public key.
func KeyID(tokenStr string) (string, error) {
	tokenStr = strings.TrimSpace(tokenStr)
	if tokenStr == "" {
		return "", ErrTokenRequired
	}

	parser := jwt.NewParser()
	token, _, err := parser.ParseUnverified(tokenStr, jwt.MapClaims{})
	if err != nil {
		return "", fmt.Errorf("%w: parse unverified header: %v", ErrInvalidToken, err)
	}
	if token == nil {
		return "", ErrInvalidToken
	}

	raw, ok := token.Header["kid"]
	if !ok {
		return "", nil
	}

	kid, ok := raw.(string)
	if !ok {
		return "", ErrInvalidKeyID
	}

	kid = strings.TrimSpace(kid)
	if kid == "" {
		return "", ErrInvalidKeyID
	}

	return kid, nil
}

func validatePublicKey(publicKey ed25519.PublicKey) error {
	switch {
	case len(publicKey) == 0:
		return ErrPublicKeyRequired
	case len(publicKey) != ed25519.PublicKeySize:
		return fmt.Errorf("%w: expected %d bytes", ErrInvalidPublicKey, ed25519.PublicKeySize)
	default:
		return nil
	}
}

func validatePrivateKey(privateKey ed25519.PrivateKey) error {
	switch {
	case len(privateKey) == 0:
		return ErrPrivateKeyRequired
	case len(privateKey) != ed25519.PrivateKeySize:
		return fmt.Errorf("%w: expected %d bytes", ErrInvalidPrivateKey, ed25519.PrivateKeySize)
	default:
		return nil
	}
}

func validateParseOptions(cfg parseOptions) error {
	switch {
	case cfg.clockSkew < 0:
		return ErrClockSkewNegative
	case cfg.clockSkew > maxClockSkew:
		return fmt.Errorf("%w: maximum %s", ErrClockSkewTooLarge, maxClockSkew)
	case cfg.audienceSet && cfg.expectedAud == "":
		return ErrInvalidAudienceOption
	case cfg.issuerSet && cfg.expectedIss == "":
		return ErrInvalidIssuerOption
	default:
		return nil
	}
}

func validateSignOptions(cfg signOptions) error {
	if cfg.keyID == "" {
		return nil
	}
	if strings.ContainsAny(cfg.keyID, "\r\n\t ") {
		return ErrInvalidKeyID
	}
	return nil
}

func validateRequiredTimeClaims(claims *Claims) error {
	switch {
	case claims.ExpiresAt == nil:
		return ErrMissingExpiresAt
	case claims.IssuedAt == nil:
		return ErrMissingIssuedAt
	case claims.NotBefore == nil:
		return ErrMissingNotBefore
	default:
		return nil
	}
}

func validateExpectedAudience(claims *Claims, cfg parseOptions) error {
	if !cfg.audienceSet {
		return nil
	}
	if len(claims.Audience) == 0 {
		return ErrMissingAudience
	}

	for _, aud := range claims.Audience {
		if aud == cfg.expectedAud {
			return nil
		}
	}

	return fmt.Errorf("%w: expected %q", ErrAudienceMismatch, cfg.expectedAud)
}

func validateExpectedIssuer(claims *Claims, cfg parseOptions) error {
	if !cfg.issuerSet {
		return nil
	}
	if claims.Issuer == "" {
		return ErrMissingIssuer
	}
	if claims.Issuer != cfg.expectedIss {
		return fmt.Errorf("%w: expected %q got %q", ErrIssuerMismatch, cfg.expectedIss, claims.Issuer)
	}

	return nil
}

func validateApplicationClaims(claims *Claims) error {
	switch {
	case claims.UserID == uuid.Nil:
		return ErrMissingUserID
	case claims.Subject == "":
		return ErrMissingSubject
	case claims.Subject != claims.UserID.String():
		return ErrSubjectUserMismatch
	case claims.ID == "":
		return ErrMissingJTI
	}

	if _, err := uuid.Parse(claims.ID); err != nil {
		return ErrInvalidJTI
	}

	return nil
}

func mapParseError(err error) error {
	switch {
	case errors.Is(err, ErrUnexpectedSigningMethod):
		return fmt.Errorf("%w: %w: %v", ErrInvalidToken, ErrUnexpectedSigningMethod, err)
	case errors.Is(err, jwt.ErrTokenExpired):
		return fmt.Errorf("%w: %w: %v", ErrInvalidToken, ErrExpiredToken, err)
	case errors.Is(err, jwt.ErrTokenNotValidYet):
		return fmt.Errorf("%w: %w: %v", ErrInvalidToken, ErrTokenNotYetValid, err)
	case errors.Is(err, jwt.ErrTokenUsedBeforeIssued):
		return fmt.Errorf("%w: %w: %v", ErrInvalidToken, ErrTokenUsedBeforeIssued, err)
	case errors.Is(err, jwt.ErrTokenSignatureInvalid):
		return fmt.Errorf("%w: %w: %v", ErrInvalidToken, ErrTokenSignatureInvalid, err)
	case errors.Is(err, jwt.ErrTokenMalformed):
		return fmt.Errorf("%w: %w: %v", ErrInvalidToken, ErrMalformedToken, err)
	case errors.Is(err, jwt.ErrTokenUnverifiable):
		return fmt.Errorf("%w: %w: %v", ErrInvalidToken, ErrTokenUnverifiable, err)
	default:
		return fmt.Errorf("%w: %v", ErrInvalidToken, err)
	}
}