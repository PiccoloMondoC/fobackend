// Package main provides API server startup, configuration, dependency wiring,
// route registration, and HTTP lifecycle management.
//
// sdworkspace/sdbackend/internal/server/cmd/api/config.go
//
// GTM:
//
//	Layer: 3.1 API Server / Application Bootstrap
//	Release Class: SPINE
//	Reason:
//	  API configuration is release-critical server infrastructure. It defines
//	  the application dependency container and the canonical runtime
//	  configuration required to initialize database access, authentication,
//	  routing, logging, bootstrap behavior, OAuth integration, and outbound
//	  email and SMS services before HTTP traffic is accepted.
//
// SPINE Rule:
//
//	Keep compiling.
//	Keep production-ready.
//	Preserve explicit application dependency wiring.
//	Preserve canonical database and model injection.
//	Preserve authentication token-service wiring.
//	Preserve email and SMS service wiring through the root
//	notification_services EmailSender / SMSSender interfaces only — never
//	through concrete channel packages (notification_services/email,
//	notification_services/sms).
//	Preserve bootstrap configuration availability.
//	Preserve bounded database-operation timeout configuration.
//	Preserve OAuth credential separation by provider.
//	Preserve secret-value opacity in logs and errors.
//	Preserve vendor-neutral language in this file: which notification
//	provider is active is governed configuration, not something the
//	Application container's shape should imply.
//	Do not introduce package-global mutable application dependencies.
//	Do not allow the HTTP server to start with incomplete critical
//	configuration.
//	Block deployment if this file breaks build, dependency construction,
//	authentication initialization, notification-service initialization,
//	database connectivity, or API startup safety.
package main

import (
	"time"

	"github.com/PiccoloMondoC/sdworkspace/sdbackend/internal/auth"
	"github.com/PiccoloMondoC/sdworkspace/sdbackend/internal/bootstrap"
	"github.com/PiccoloMondoC/sdworkspace/sdbackend/internal/data"
	"github.com/PiccoloMondoC/sdworkspace/sdbackend/internal/logging"
	notificationservices "github.com/PiccoloMondoC/sdworkspace/sdbackend/internal/notification_services"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const cfgTimeout = 10 * time.Second

type Application struct {
	Config       Config
	Logger       *logging.Logger
	Models       data.Models
	Router       chi.Router
	Preloaded    *PreloadedIDs
	TokenService *auth.TokenService

	// EmailService and SMSService depend on the root notification_services
	// interfaces only. Handlers must not know or care whether the active
	// implementation is local, sandboxed, or provider-backed.
	EmailService notificationservices.EmailSender
	SMSService   notificationservices.SMSSender

	// Tracks public, unauthenticated endpoints for display at "/".
	publicRoutes *endpointRegistry
}

type Config struct {
	Bootstrap bootstrap.Config
	DB        *pgxpool.Pool
	Models    data.Models
	Logger    *logging.Logger
	// DBTimeout bounds database and dependent service operations.
	DBTimeout time.Duration
	// Database and secret configuration.
	SecretUser   string
	SecretPass   string
	SecretDBName string
	SecretPepper string

	JWTIssuer   string
	JWTAudience string

	OAuth struct {
		GoogleClientID     string
		GoogleClientSecret string
		FacebookAppID      string
		FacebookAppSecret  string
	}
}
