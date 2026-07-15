// Package bootstrap provides startup configuration loading and validation.
//
// sdworkspace/sdbackend/internal/bootstrap/bootstrap.go
//
// GTM:
//   Layer: 2.1 Database / Governance Foundation
//   Release Class: SPINE
//   Reason:
//     Bootstrap configuration is release-critical startup infrastructure. This
//     file validates environment, public URL, server port, EdDSA JWT key
//     configuration, database connection settings, and service timeout values
//     before the application initializes dependent SPINE and DEFERRED domains.
//
// SPINE Rule:
//   Keep compiling.
//   Keep production-ready.
//   Preserve explicit startup configuration validation.
//   Preserve EdDSA / Ed25519 JWT configuration.
//   Preserve secret non-disclosure in logs, traces, and error output.
//   Preserve compatibility with data.DBConnectionParamsModel.
//   Preserve canonical validated values for downstream consumers.
//   Block deployment if this file breaks application startup,
//   database configuration, JWT configuration, timeout validation,
//   Ed25519 key validation, or protected configuration handling.
package bootstrap

import (
	"crypto/ed25519"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"log/slog"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	defaultEnv       = "dev"
	defaultWebPort   = "8080"
	defaultDBTimeout = 10 * time.Second

	defaultLocalJWTIssuer   = "sd-auth"
	defaultLocalJWTAudience = "sd-api"
	defaultLocalJWTKeyID    = "local-dev-ed25519"

	pemTypePrivateKey = "PRIVATE KEY"
	pemTypePublicKey  = "PUBLIC KEY"

	minDBTimeout = 1 * time.Second
	maxDBTimeout = 2 * time.Minute

	minDBPasswordBytes          = 16
	minOAuthClientSecretBytes   = 16
	redactedProtectedValueLabel = "[REDACTED]"
)

// Config contains canonical startup configuration after environment loading,
// validation, normalization, and protected-value handling.
//
// Secret-bearing fields are retained only because startup and security
// components need them at controlled internal boundaries. Callers must not log
// or serialize raw Config values expecting secrets to appear. String, GoString,
// MarshalJSON, and LogValue intentionally redact protected fields.
type Config struct {
	// Env is the canonical runtime environment: dev, test, staging, or prod.
	Env string

	// BaseURL is the canonical externally visible service base URL.
	BaseURL string

	// WebPort is the validated HTTP listener port.
	WebPort string

	// DBHost is the validated PostgreSQL host name or IP address.
	DBHost string

	// DBPort is the validated PostgreSQL port.
	DBPort string

	// DBUser is the validated PostgreSQL username.
	DBUser string

	// DBPass is the PostgreSQL password. It must never be logged or exposed.
	DBPass string

	// DBName is the validated PostgreSQL database name.
	DBName string

	// DBTimeout is the startup database connection-attempt timeout.
	//
	// This is distinct from data.dbTimeout, which is the canonical timeout for
	// ordinary data-layer model operations.
	DBTimeout time.Duration

	// OAuthWebClientSecret is the OAuth web client secret. It must never be
	// logged, serialized, traced, or exposed in API output.
	OAuthWebClientSecret string

	// OAuthMobileClientSecret is the OAuth mobile client secret. It must never
	// be logged, serialized, traced, or exposed in API output.
	OAuthMobileClientSecret string

	// JWTIssuer is the validated JWT issuer identifier.
	JWTIssuer string

	// JWTAudience is the validated JWT audience identifier.
	JWTAudience string

	// JWTKeyID is the validated JWT key identifier used for kid support.
	JWTKeyID string

	// JWTPrivateKey is the Ed25519 private signing key. It must never be logged,
	// serialized, traced, or distributed to validator-only services.
	JWTPrivateKey ed25519.PrivateKey

	// JWTPublicKey is the Ed25519 public verification key.
	JWTPublicKey ed25519.PublicKey
}

// LoadConfig loads, validates, canonicalizes, and returns startup configuration.
//
// The returned Config contains validated values only. URL, host, port, duration,
// JWT identifier, and Ed25519 key validation are performed before dependent
// startup paths receive configuration.
func LoadConfig() (*Config, error) {
	env, err := loadEnv()
	if err != nil {
		return nil, err
	}

	baseURL, err := requiredURL("BASE_URL")
	if err != nil {
		return nil, err
	}

	webPort, err := loadPort("WEB_PORT", defaultWebPort)
	if err != nil {
		return nil, err
	}

	dbHost, err := requiredHost("DB_HOST")
	if err != nil {
		return nil, err
	}

	dbPort, err := requiredPort("DB_PORT")
	if err != nil {
		return nil, err
	}

	dbUser, err := requiredValue("SD_BACKEND_DB_USER")
	if err != nil {
		return nil, err
	}

	dbPass, err := requiredSecret("SD_BACKEND_DB_PASSWORD", minDBPasswordBytes)
	if err != nil {
		return nil, err
	}

	dbName, err := requiredValue("SD_BACKEND_DB_NAME")
	if err != nil {
		return nil, err
	}

	dbTimeout, err := loadDuration("DB_TIMEOUT", defaultDBTimeout, minDBTimeout, maxDBTimeout)
	if err != nil {
		return nil, err
	}

	oauthWebClientSecret, err := requiredSecret("OAUTH_WEB_CLIENT_SECRET", minOAuthClientSecretBytes)
	if err != nil {
		return nil, err
	}

	oauthMobileClientSecret, err := requiredSecret("OAUTH_MOBILE_CLIENT_SECRET", minOAuthClientSecretBytes)
	if err != nil {
		return nil, err
	}

	jwtIssuer, err := loadJWTIdentifier(env, "JWT_ISSUER", defaultLocalJWTIssuer)
	if err != nil {
		return nil, err
	}

	jwtAudience, err := loadJWTIdentifier(env, "JWT_AUDIENCE", defaultLocalJWTAudience)
	if err != nil {
		return nil, err
	}

	jwtKeyID, err := loadJWTIdentifier(env, "JWT_KEY_ID", defaultLocalJWTKeyID)
	if err != nil {
		return nil, err
	}

	jwtPrivateKeyPath, err := requiredValue("JWT_PRIVATE_KEY_PATH")
	if err != nil {
		return nil, err
	}

	jwtPublicKeyPath, err := requiredValue("JWT_PUBLIC_KEY_PATH")
	if err != nil {
		return nil, err
	}

	jwtPrivateKey, err := loadEd25519PrivateKey(jwtPrivateKeyPath)
	if err != nil {
		return nil, err
	}

	jwtPublicKey, err := loadEd25519PublicKey(jwtPublicKeyPath)
	if err != nil {
		return nil, err
	}

	derivedPublicKey, ok := jwtPrivateKey.Public().(ed25519.PublicKey)
	if !ok {
		return nil, fmt.Errorf("JWT private key did not expose an Ed25519 public key")
	}

	if !derivedPublicKey.Equal(jwtPublicKey) {
		return nil, fmt.Errorf("JWT_PRIVATE_KEY_PATH and JWT_PUBLIC_KEY_PATH do not form a matching Ed25519 key pair")
	}

	return &Config{
		Env:                     env,
		BaseURL:                 baseURL,
		WebPort:                 webPort,
		DBHost:                  dbHost,
		DBPort:                  dbPort,
		DBUser:                  dbUser,
		DBPass:                  dbPass,
		DBName:                  dbName,
		DBTimeout:               dbTimeout,
		OAuthWebClientSecret:    oauthWebClientSecret,
		OAuthMobileClientSecret: oauthMobileClientSecret,
		JWTIssuer:               jwtIssuer,
		JWTAudience:             jwtAudience,
		JWTKeyID:                jwtKeyID,
		JWTPrivateKey:           jwtPrivateKey,
		JWTPublicKey:            jwtPublicKey,
	}, nil
}

// String returns a redacted representation of Config safe for diagnostics.
func (c *Config) String() string {
	if c == nil {
		return "<nil>"
	}

	return fmt.Sprintf(
		"Config{Env:%q BaseURL:%q WebPort:%q DBHost:%q DBPort:%q DBUser:%q DBPass:%s DBName:%q DBTimeout:%s OAuthWebClientSecret:%s OAuthMobileClientSecret:%s JWTIssuer:%q JWTAudience:%q JWTKeyID:%q JWTPrivateKey:%s JWTPublicKey:%s}",
		c.Env,
		c.BaseURL,
		c.WebPort,
		c.DBHost,
		c.DBPort,
		c.DBUser,
		redactedProtectedValueLabel,
		c.DBName,
		c.DBTimeout,
		redactedProtectedValueLabel,
		redactedProtectedValueLabel,
		c.JWTIssuer,
		c.JWTAudience,
		c.JWTKeyID,
		redactedProtectedValueLabel,
		fmt.Sprintf("%x", []byte(c.JWTPublicKey)),
	)
}

// GoString returns a redacted representation of Config for %#v formatting.
func (c *Config) GoString() string {
	return c.String()
}

// MarshalJSON serializes Config with protected fields redacted.
func (c *Config) MarshalJSON() ([]byte, error) {
	if c == nil {
		return []byte("null"), nil
	}

	type redactedConfig struct {
		Env                     string        `json:"env"`
		BaseURL                 string        `json:"base_url"`
		WebPort                 string        `json:"web_port"`
		DBHost                  string        `json:"db_host"`
		DBPort                  string        `json:"db_port"`
		DBUser                  string        `json:"db_user"`
		DBPass                  string        `json:"db_pass"`
		DBName                  string        `json:"db_name"`
		DBTimeout               time.Duration `json:"db_timeout"`
		OAuthWebClientSecret    string        `json:"oauth_web_client_secret"`
		OAuthMobileClientSecret string        `json:"oauth_mobile_client_secret"`
		JWTIssuer               string        `json:"jwt_issuer"`
		JWTAudience             string        `json:"jwt_audience"`
		JWTKeyID                string        `json:"jwt_key_id"`
		JWTPrivateKey           string        `json:"jwt_private_key"`
		JWTPublicKey            string        `json:"jwt_public_key"`
	}

	return json.Marshal(redactedConfig{
		Env:                     c.Env,
		BaseURL:                 c.BaseURL,
		WebPort:                 c.WebPort,
		DBHost:                  c.DBHost,
		DBPort:                  c.DBPort,
		DBUser:                  c.DBUser,
		DBPass:                  redactedProtectedValueLabel,
		DBName:                  c.DBName,
		DBTimeout:               c.DBTimeout,
		OAuthWebClientSecret:    redactedProtectedValueLabel,
		OAuthMobileClientSecret: redactedProtectedValueLabel,
		JWTIssuer:               c.JWTIssuer,
		JWTAudience:             c.JWTAudience,
		JWTKeyID:                c.JWTKeyID,
		JWTPrivateKey:           redactedProtectedValueLabel,
		JWTPublicKey:            fmt.Sprintf("%x", []byte(c.JWTPublicKey)),
	})
}

// LogValue returns a structured redacted value for slog-compatible loggers.
func (c *Config) LogValue() slog.Value {
	if c == nil {
		return slog.StringValue("<nil>")
	}

	return slog.GroupValue(
		slog.String("env", c.Env),
		slog.String("base_url", c.BaseURL),
		slog.String("web_port", c.WebPort),
		slog.String("db_host", c.DBHost),
		slog.String("db_port", c.DBPort),
		slog.String("db_user", c.DBUser),
		slog.String("db_pass", redactedProtectedValueLabel),
		slog.String("db_name", c.DBName),
		slog.Duration("db_timeout", c.DBTimeout),
		slog.String("oauth_web_client_secret", redactedProtectedValueLabel),
		slog.String("oauth_mobile_client_secret", redactedProtectedValueLabel),
		slog.String("jwt_issuer", c.JWTIssuer),
		slog.String("jwt_audience", c.JWTAudience),
		slog.String("jwt_key_id", c.JWTKeyID),
		slog.String("jwt_private_key", redactedProtectedValueLabel),
		slog.String("jwt_public_key", fmt.Sprintf("%x", []byte(c.JWTPublicKey))),
	)
}

func loadEnv() (string, error) {
	env := optionalValue("GO_ENV", defaultEnv)

	switch env {
	case "dev", "development":
		return "dev", nil
	case "test":
		return "test", nil
	case "staging":
		return "staging", nil
	case "prod", "production":
		return "prod", nil
	default:
		return "", fmt.Errorf("GO_ENV must be one of dev, development, test, staging, prod, production")
	}
}

func requiredValue(key string) (string, error) {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return "", fmt.Errorf("%s is required", key)
	}
	return value, nil
}

func optionalValue(key, fallback string) string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	return value
}

func requiredSecret(key string, minBytes int) (string, error) {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return "", fmt.Errorf("%s is required", key)
	}

	if len([]byte(value)) < minBytes {
		return "", fmt.Errorf("%s must be at least %d bytes", key, minBytes)
	}

	return value, nil
}

func loadJWTIdentifier(env, key, localDefault string) (string, error) {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		if env == "dev" || env == "test" {
			value = localDefault
		} else {
			return "", fmt.Errorf("%s is required for %s environment", key, env)
		}
	}

	if strings.ContainsAny(value, " \t\r\n") {
		return "", fmt.Errorf("%s must not contain whitespace", key)
	}

	return value, nil
}

func requiredURL(key string) (string, error) {
	raw, err := requiredValue(key)
	if err != nil {
		return "", err
	}

	parsed, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("%s is invalid: %w", key, err)
	}

	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", fmt.Errorf("%s must use http or https", key)
	}

	if parsed.Host == "" {
		return "", fmt.Errorf("%s must include a host", key)
	}

	if parsed.User != nil {
		return "", fmt.Errorf("%s must not include user-info credentials", key)
	}

	if parsed.RawQuery != "" {
		return "", fmt.Errorf("%s must not include query parameters", key)
	}

	if parsed.Fragment != "" {
		return "", fmt.Errorf("%s must not include a fragment", key)
	}

	if hasTraversalPathSegment(parsed.EscapedPath()) {
		return "", fmt.Errorf("%s must not include traversal path segments", key)
	}

	return strings.TrimRight(parsed.String(), "/"), nil
}

func hasTraversalPathSegment(escapedPath string) bool {
	if escapedPath == "" || escapedPath == "/" {
		return false
	}

	segments := strings.Split(escapedPath, "/")
	for _, segment := range segments {
		if segment == "" {
			continue
		}

		decoded, err := url.PathUnescape(segment)
		if err != nil {
			return true
		}

		decoded = strings.TrimSpace(decoded)
		if decoded == "." || decoded == ".." {
			return true
		}
	}

	return false
}

func requiredHost(key string) (string, error) {
	host, err := requiredValue(key)
	if err != nil {
		return "", err
	}

	if strings.Contains(host, "://") {
		return "", fmt.Errorf("%s must be a host name or IP address, not a URL", key)
	}

	if strings.Contains(host, "/") {
		return "", fmt.Errorf("%s must not contain path segments", key)
	}

	if ip := net.ParseIP(host); ip != nil {
		return host, nil
	}

	if !isLikelyDNSName(host) {
		return "", fmt.Errorf("%s must be a valid host name or IP address", key)
	}

	return host, nil
}

func loadPort(key, fallback string) (string, error) {
	value := optionalValue(key, fallback)
	if err := validatePort(key, value); err != nil {
		return "", err
	}
	return value, nil
}

func requiredPort(key string) (string, error) {
	value, err := requiredValue(key)
	if err != nil {
		return "", err
	}

	if err := validatePort(key, value); err != nil {
		return "", err
	}

	return value, nil
}

func validatePort(key, value string) error {
	port, err := strconv.Atoi(value)
	if err != nil {
		return fmt.Errorf("%s must be numeric", key)
	}

	if port < 1 || port > 65535 {
		return fmt.Errorf("%s must be between 1 and 65535", key)
	}

	return nil
}

func loadDuration(key string, fallback, minValue, maxValue time.Duration) (time.Duration, error) {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback, nil
	}

	value, err := time.ParseDuration(raw)
	if err != nil {
		return 0, fmt.Errorf("%s is invalid: %w", key, err)
	}

	if value < minValue || value > maxValue {
		return 0, fmt.Errorf("%s must be between %s and %s", key, minValue, maxValue)
	}

	return value, nil
}

func loadEd25519PrivateKey(path string) (ed25519.PrivateKey, error) {
	pemBytes, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read JWT private key: %w", err)
	}

	block, _ := pem.Decode(pemBytes)
	if block == nil {
		return nil, fmt.Errorf("JWT private key must be PEM encoded")
	}

	if block.Type != pemTypePrivateKey {
		return nil, fmt.Errorf("JWT private key PEM block must be %q, got %q", pemTypePrivateKey, block.Type)
	}

	key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse JWT private key as PKCS#8 Ed25519: %w", err)
	}

	privateKey, ok := key.(ed25519.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("JWT private key must be Ed25519")
	}

	if len(privateKey) != ed25519.PrivateKeySize {
		return nil, fmt.Errorf("JWT private key has invalid Ed25519 length")
	}

	return privateKey, nil
}

func loadEd25519PublicKey(path string) (ed25519.PublicKey, error) {
	pemBytes, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read JWT public key: %w", err)
	}

	block, _ := pem.Decode(pemBytes)
	if block == nil {
		return nil, fmt.Errorf("JWT public key must be PEM encoded")
	}

	if block.Type != pemTypePublicKey {
		return nil, fmt.Errorf("JWT public key PEM block must be %q, got %q", pemTypePublicKey, block.Type)
	}

	key, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse JWT public key as PKIX Ed25519: %w", err)
	}

	publicKey, ok := key.(ed25519.PublicKey)
	if !ok {
		return nil, fmt.Errorf("JWT public key must be Ed25519")
	}

	if len(publicKey) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("JWT public key has invalid Ed25519 length")
	}

	return publicKey, nil
}

func isLikelyDNSName(host string) bool {
	if len(host) > 253 {
		return false
	}

	labels := strings.Split(host, ".")
	for _, label := range labels {
		if label == "" || len(label) > 63 {
			return false
		}

		if strings.HasPrefix(label, "-") || strings.HasSuffix(label, "-") {
			return false
		}

		for _, r := range label {
			if (r >= 'a' && r <= 'z') ||
				(r >= 'A' && r <= 'Z') ||
				(r >= '0' && r <= '9') ||
				r == '-' {
				continue
			}
			return false
		}
	}

	return true
}