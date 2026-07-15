// Package logging provides zap-backed structured logging and contextual request
// enrichment for the Sagrenti backend.
//
// sdworkspace/sdbackend/internal/logging/logger.go
//
// GTM:
//   Layer: 2.1 Database / Governance Foundation
//   Release Class: SPINE
//   Reason:
//     Logging is release-critical observability infrastructure. It supports
//     structured logs, contextual request/user/remote-IP enrichment, service
//     identification, local stdout/stderr logging, and handler-level logger
//     injection needed across backend domains.
//
// SPINE Rule:
//   Keep compiling.
//   Keep production-ready.
//   Preserve zap-backed structured logging.
//   Preserve request/user/remote-IP contextual enrichment.
//   Preserve service-name tagging.
//   Preserve local stdout/stderr logging.
//   Block deployment if this file breaks build, contextual logging,
//   observability, handler logger injection, or service traceability.
package logging

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

type loggerKey string
type contextKey string

const (
	// RequestIDKey is the context key and HTTP header name used for request
	// correlation. Values are expected to be strings and are injected only when
	// non-empty.
	RequestIDKey contextKey = "X-Request-ID"

	// UserIDKey is the context key used for authenticated-user enrichment.
	// Values are expected to be strings and are injected only when non-empty.
	UserIDKey contextKey = "X-User-ID"

	// RemoteIPKey is the context key used for remote-IP enrichment. Values are
	// expected to be strings and are injected only when non-empty.
	RemoteIPKey contextKey = "remoteIP"

	contextLoggerKey loggerKey = "logger"

	defaultServiceName = "sd-backend"
	redactedValue      = "[REDACTED]"
)

// Logger wraps zap with Sagrenti-standard structured logging, service tagging,
// and contextual enrichment.
type Logger struct {
	zapLogger   *zap.Logger
	serviceName string
}

// InitLogging creates a production zap-backed logger for the given service.
// Empty service names are replaced with the package default service name.
func InitLogging(serviceName string) (*Logger, error) {
	serviceName = strings.TrimSpace(serviceName)
	if serviceName == "" {
		serviceName = defaultServiceName
	}

	config := zap.NewProductionConfig()
	config.EncoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder
	config.OutputPaths = []string{"stdout"}
	config.ErrorOutputPaths = []string{"stderr"}

	zapLogger, err := config.Build()
	if err != nil {
		return nil, fmt.Errorf("build zap logger: %w", err)
	}

	return &Logger{
		zapLogger:   zapLogger.With(zap.String("service", serviceName)),
		serviceName: serviceName,
	}, nil
}

// Info writes an informational structured log entry.
func (l *Logger) Info(msg string, keysAndValues ...interface{}) {
	l.log("INFO", msg, keysAndValues...)
}

// Warn writes a warning structured log entry.
func (l *Logger) Warn(msg string, keysAndValues ...interface{}) {
	l.log("WARN", msg, keysAndValues...)
}

// Debug writes a debug structured log entry.
func (l *Logger) Debug(msg string, keysAndValues ...interface{}) {
	l.log("DEBUG", msg, keysAndValues...)
}

// Error writes an error structured log entry.
func (l *Logger) Error(msg string, keysAndValues ...interface{}) {
	l.log("ERROR", msg, keysAndValues...)
}

// Critical writes a critical structured log entry.
func (l *Logger) Critical(msg string, keysAndValues ...interface{}) {
	l.log("CRITICAL", msg, keysAndValues...)
}

// Fatal writes a fatal structured log entry and then delegates to zap.Fatal.
// If the logger itself is unavailable, Fatal writes directly to stderr and
// exits with a non-zero status rather than silently returning.
func (l *Logger) Fatal(msg string, keysAndValues ...interface{}) {
	if l == nil || l.zapLogger == nil {
		_, _ = fmt.Fprintf(os.Stderr, "FATAL: %s\n", msg)
		os.Exit(1)
	}

	l.zapLogger.Fatal(msg, toZapFields(keysAndValues)...)
}

// Infof formats and writes an informational structured log entry.
func (l *Logger) Infof(format string, args ...interface{}) {
	l.Info(fmt.Sprintf(format, args...))
}

// Errorf formats and writes an error structured log entry.
func (l *Logger) Errorf(format string, args ...interface{}) {
	l.Error(fmt.Sprintf(format, args...))
}

// InfoWithRequest writes an informational structured log entry enriched with
// HTTP request metadata.
func (l *Logger) InfoWithRequest(r *http.Request, msg string, keysAndValues ...interface{}) {
	l.GetLoggerWithContext(r).Info(msg, keysAndValues...)
}

// WarnWithRequest writes a warning structured log entry enriched with HTTP
// request metadata.
func (l *Logger) WarnWithRequest(r *http.Request, msg string, keysAndValues ...interface{}) {
	l.GetLoggerWithContext(r).Warn(msg, keysAndValues...)
}

// DebugWithRequest writes a debug structured log entry enriched with HTTP
// request metadata.
func (l *Logger) DebugWithRequest(r *http.Request, msg string, keysAndValues ...interface{}) {
	l.GetLoggerWithContext(r).Debug(msg, keysAndValues...)
}

// ErrorWithRequest writes an error structured log entry enriched with HTTP
// request metadata.
func (l *Logger) ErrorWithRequest(r *http.Request, msg string, keysAndValues ...interface{}) {
	l.GetLoggerWithContext(r).Error(msg, keysAndValues...)
}

// CriticalWithRequest writes a critical structured log entry enriched with HTTP
// request metadata.
func (l *Logger) CriticalWithRequest(r *http.Request, msg string, keysAndValues ...interface{}) {
	l.GetLoggerWithContext(r).Critical(msg, keysAndValues...)
}

// InfofWithRequest formats and writes an informational structured log entry
// enriched with HTTP request metadata.
func (l *Logger) InfofWithRequest(r *http.Request, format string, args ...interface{}) {
	l.InfoWithRequest(r, fmt.Sprintf(format, args...))
}

// ErrorfWithRequest formats and writes an error structured log entry enriched
// with HTTP request metadata.
func (l *Logger) ErrorfWithRequest(r *http.Request, format string, args ...interface{}) {
	l.ErrorWithRequest(r, fmt.Sprintf(format, args...))
}

func (l *Logger) log(level string, msg string, keysAndValues ...interface{}) {
	if l == nil || l.zapLogger == nil {
		return
	}

	fields := toZapFields(keysAndValues)

	switch level {
	case "DEBUG":
		l.zapLogger.Debug(msg, fields...)
	case "INFO":
		l.zapLogger.Info(msg, fields...)
	case "WARN":
		l.zapLogger.Warn(msg, fields...)
	case "ERROR", "CRITICAL":
		l.zapLogger.Error(msg, fields...)
	default:
		l.zapLogger.Info(msg, fields...)
	}
}

// Sync flushes buffered zap log entries.
func (l *Logger) Sync() error {
	if l == nil || l.zapLogger == nil {
		return nil
	}

	return l.zapLogger.Sync()
}

// Shutdown preserves the central logger shutdown signature for stable lifecycle
// calls, but the current monolith logger has no external dispatcher or remote
// forwarding lifecycle. It therefore ignores ctx and flushes zap only.
func (l *Logger) Shutdown(ctx context.Context) error {
	_ = ctx
	return l.Sync()
}

// ZapLogger returns the underlying zap logger for integrations that require
// direct zap access.
func (l *Logger) ZapLogger() *zap.Logger {
	if l == nil {
		return nil
	}

	return l.zapLogger
}

// ServiceName returns the service name attached to logs from this logger.
func (l *Logger) ServiceName() string {
	if l == nil {
		return ""
	}

	return l.serviceName
}

// WithFunctionName returns a derived logger enriched with a function name when
// name is non-empty after trimming.
func (l *Logger) WithFunctionName(name string) *Logger {
	return l.withNonEmptyString("function", name)
}

// WithRequestID returns a derived logger enriched with a request ID when
// requestID is non-empty after trimming.
func (l *Logger) WithRequestID(requestID string) *Logger {
	return l.withNonEmptyString("requestID", requestID)
}

// WithUserID returns a derived logger enriched with a user ID when userID is
// non-empty after trimming.
func (l *Logger) WithUserID(userID string) *Logger {
	return l.withNonEmptyString("userID", userID)
}

// WithRemoteIP returns a derived logger enriched with a remote IP address when
// remoteIP is non-empty after trimming.
func (l *Logger) WithRemoteIP(remoteIP string) *Logger {
	return l.withNonEmptyString("remoteIP", remoteIP)
}

// WithCustomField returns a derived logger enriched with one sanitized custom
// field. Empty keys are not used and are represented by malformed_log_field.
func (l *Logger) WithCustomField(key string, value interface{}) *Logger {
	key = strings.TrimSpace(key)
	if key == "" {
		return l.with(zap.Bool("malformed_log_field", true))
	}

	return l.with(zap.Any(key, sanitizeFieldValue(key, value)))
}

func (l *Logger) withNonEmptyString(key, value string) *Logger {
	value = strings.TrimSpace(value)
	if value == "" {
		return l
	}

	return l.with(zap.String(key, value))
}

func (l *Logger) with(fields ...zap.Field) *Logger {
	if l == nil {
		return nil
	}

	if l.zapLogger == nil {
		return l
	}

	return &Logger{
		zapLogger:   l.zapLogger.With(fields...),
		serviceName: l.serviceName,
	}
}

// WrapHTTPHandler injects a request-scoped logger into the request context and
// conditionally adds request ID, user ID, and remote IP context values when
// those values are non-empty.
func (l *Logger) WrapHTTPHandler(handlerName string, handler http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestID := r.Header.Get(string(RequestIDKey))
		remoteIP := r.RemoteAddr
		userID, _ := r.Context().Value(UserIDKey).(string)

		logger := l.
			WithFunctionName(handlerName).
			WithRequestID(requestID).
			WithUserID(userID).
			WithRemoteIP(remoteIP)

		ctx := context.WithValue(r.Context(), contextLoggerKey, logger)

		if requestID != "" {
			ctx = context.WithValue(ctx, RequestIDKey, requestID)
		}
		if userID != "" {
			ctx = context.WithValue(ctx, UserIDKey, userID)
		}
		if remoteIP != "" {
			ctx = context.WithValue(ctx, RemoteIPKey, remoteIP)
		}

		handler.ServeHTTP(w, r.WithContext(ctx))
	})
}

// GetLoggerWithContext returns a derived logger enriched from an HTTP request.
// It reads the request ID from the request header, user ID from context, and
// remote IP from the request.
func (l *Logger) GetLoggerWithContext(r *http.Request) *Logger {
	if r == nil {
		return l
	}

	requestID := r.Header.Get(string(RequestIDKey))
	userID, _ := r.Context().Value(UserIDKey).(string)

	return l.
		WithRequestID(requestID).
		WithUserID(userID).
		WithRemoteIP(r.RemoteAddr)
}

// GetLoggerWithContextFromContext returns a request-scoped logger injected by
// WrapHTTPHandler when present. If none exists, it reconstructs enrichment from
// known context keys.
func (l *Logger) GetLoggerWithContextFromContext(ctx context.Context) *Logger {
	if ctx == nil {
		return l
	}

	if logger, ok := ctx.Value(contextLoggerKey).(*Logger); ok && logger != nil {
		return logger
	}

	requestID, _ := ctx.Value(RequestIDKey).(string)
	userID, _ := ctx.Value(UserIDKey).(string)
	remoteIP, _ := ctx.Value(RemoteIPKey).(string)

	return l.
		WithRequestID(requestID).
		WithUserID(userID).
		WithRemoteIP(remoteIP)
}

func toZapFields(keysAndValues []interface{}) []zap.Field {
	fields := make([]zap.Field, 0, len(keysAndValues)/2+1)

	for i := 0; i+1 < len(keysAndValues); i += 2 {
		key, ok := keysAndValues[i].(string)
		if !ok || strings.TrimSpace(key) == "" {
			fields = append(fields, zap.Bool("malformed_log_field", true))
			continue
		}

		fields = append(fields, zap.Any(key, sanitizeFieldValue(key, keysAndValues[i+1])))
	}

	if len(keysAndValues)%2 != 0 {
		fields = append(fields, zap.Bool("malformed_log_field", true))
	}

	return fields
}

func sanitizeFieldValue(key string, value interface{}) interface{} {
	if isSensitiveLogKey(key) {
		return redactedValue
	}

	return value
}

func isSensitiveLogKey(key string) bool {
	normalized := strings.ToLower(strings.TrimSpace(key))

	sensitiveFragments := []string{
		"api_key",
		"apikey",
		"authorization",
		"bearer",
		"client_secret",
		"cookie",
		"credential",
		"ed25519",
		"jwt",
		"passphrase",
		"password",
		"private",
		"refresh",
		"secret",
		"signing",
		"token",
	}

	for _, fragment := range sensitiveFragments {
		if strings.Contains(normalized, fragment) {
			return true
		}
	}

	return false
}