// Package main owns release-critical API process startup and lifecycle
// orchestration for the Platform backend.
//
// sdworkspace/sdbackend/internal/server/cmd/api/main.go
//
// GTM:
//
//	Layer: 2.1 Database / Governance Foundation
//	Release Class: SPINE
//	Reason:
//	  API process startup is release-critical application infrastructure. This
//	  file loads and validates canonical configuration, initializes structured
//	  logging, establishes the PostgreSQL connection, creates and seeds the
//	  database, preloads governance identifiers, constructs EdDSA token
//	  infrastructure, ensures readiness-critical SPINE reference data, starts
//	  background automation, wires HTTP dependencies, and coordinates graceful
//	  shutdown.
//
// SPINE Rule:
//
//	Keep compiling.
//	Keep production-ready.
//	Preserve fail-fast configuration validation.
//	Preserve structured logger initialization and shutdown flushing.
//	Preserve retry-bounded database startup.
//	Preserve schema creation before atomic data seeding.
//	Preserve protected OAuth seed-secret injection.
//	Preserve EdDSA/Ed25519 token-service construction.
//	Preserve readiness-critical SPINE seed ordering.
//	Preserve background-service shutdown signalling.
//	Preserve HTTP server timeout boundaries.
//	Preserve SIGINT/SIGTERM graceful shutdown.
//	Preserve secret non-disclosure in logs, traces, and errors.
//	Preserve composition-only notification-service construction: main.go
//	imports the root notification_services package only, and constructs
//	notification delivery through notificationservices.NewTestServices (or,
//	after governed provider selection lands, notificationservices.NewServices,
//	which does fail fast on incomplete configuration). main.go must never
//	import notification_services/email or notification_services/sms directly.
//	Block deployment if this file breaks startup, database readiness,
//	authentication construction, SPINE reference-data readiness, HTTP serving,
//	background-service lifecycle, notification-service construction, or
//	graceful shutdown.
package main

import (
	"context"
	"crypto/ed25519"
	"errors"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/PiccoloMondoC/sdworkspace/sdbackend/internal/auth"
	"github.com/PiccoloMondoC/sdworkspace/sdbackend/internal/bootstrap"
	"github.com/PiccoloMondoC/sdworkspace/sdbackend/internal/data"
	"github.com/PiccoloMondoC/sdworkspace/sdbackend/internal/logging"
	notificationservices "github.com/PiccoloMondoC/sdworkspace/sdbackend/internal/notification_services"
	services "github.com/PiccoloMondoC/sdworkspace/sdbackend/internal/services"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/joho/godotenv"
	"go.opentelemetry.io/otel"
)

// ───────────────────────────────────────────────────────────────────────────────
// Retry helper – exponential back‑off with jitter
// ───────────────────────────────────────────────────────────────────────────────

func retryWithBackoff(ctx context.Context, operation func() error) error {
	const maxRetries = 5
	backoff := 1 * time.Second

	for attempt := 1; attempt <= maxRetries; attempt++ {
		err := operation()
		if err == nil {
			return nil
		}

		sleep := backoff + time.Duration(rand.Intn(500))*time.Millisecond
		fmt.Printf("Attempt %d failed: %v. Retrying in %v...\n", attempt, err, sleep)

		select {
		case <-time.After(sleep):
		case <-ctx.Done():
			return errors.New("operation cancelled")
		}

		backoff *= 2
	}
	return errors.New("max retries reached")
}

// ───────────────────────────────────────────────────────────────────────────────
// main – bootstrap & run
// ───────────────────────────────────────────────────────────────────────────────

func main() {
	// ─── Load .env File for Local Development ──────────────────────
	// This loads environment variables from a local .env file if present.
	// In production, these variables should be provided via the environment,
	// so failure to load .env is not fatal.
	if err := godotenv.Load(".env"); err != nil {
		log.Println("No .env file found (this is expected in production)")
	}

	// Load env / flag configuration
	cfg, err := bootstrap.LoadConfig()
	if err != nil {
		log.Fatalf("config error: %v", err)
	}

	// Initialize structured logger (JSON) – service‑name + env pre‑wired
	logger, err := logging.InitLogging("api")
	if err != nil {
		log.Fatalf("logger init error: %v", err)
	}

	defer func() {
		if syncErr := logger.Sync(); syncErr != nil {
			log.Printf("logger sync error: %v", syncErr)
		}
	}()

	// PostgreSQL connection (pool) with retry
	ctx := context.Background()
	dbModel := &data.DBConnectionParamsModel{Logger: logger}

	var db *pgxpool.Pool
	if err := retryWithBackoff(ctx, func() error {
		var connErr error
		db, connErr = dbModel.ConnectWithConnector(cfg)
		return connErr
	}); err != nil {
		logger.Fatal("DB connection failed", "error", err)
	}
	defer db.Close()

	// Create tables using the active DB connection
	if err := dbModel.CreateTables(db); err != nil {
		logger.Fatal("table creation failed", "error", err)
	}

	// After CreateTables(db)
	oauthSeedSecrets := data.OAuthClientSeedSecrets{
		WebClientSecret:    cfg.OAuthWebClientSecret,
		MobileClientSecret: cfg.OAuthMobileClientSecret,
	}

	if err := dbModel.SeedAllData(db, oauthSeedSecrets); err != nil {
		logger.Fatal("data seeding failed", "error", err)
	}

	// Now preload entity/action UUIDs
	preloaded, err := preloadEntityAndActionIDs(ctx, db)
	if err != nil {
		logger.Fatal("preload IDs failed", "error", err)
	}

	// Data layer & JWT/refresh token service
	models := data.New(db, logger)

	// Build TokenService (signature changed)
	publicKeys := map[string]ed25519.PublicKey{
		cfg.JWTKeyID: cfg.JWTPublicKey,
	}

	tokenService, err := auth.NewTokenService(
		db,
		cfg.JWTPrivateKey,
		publicKeys,
		cfg.JWTKeyID,
		15*time.Minute,
		30*24*time.Hour,
		cfg.JWTIssuer,
		cfg.JWTAudience,
		logger,
		otel.Tracer("auth"),
		&models.Token,
	)
	if err != nil {
		logger.Fatal("token service init failed", "error", err)
	}

	// Construct notification services through the root notification_services
	// composition boundary only. Today this is the local/test implementation,
	// which cannot fail. The later evolution is replacing this single call
	// with:
	//
	//   notificationServices, err := notificationservices.NewServices(
	//       configuredEmailSender,
	//       configuredSMSSender,
	//   )
	//   if err != nil {
	//       logger.Fatal("notification service initialization failed", "error", err)
	//   }
	//
	// once governed provider selection lands. Handlers never see this
	// decision — they only see EmailSender / SMSSender.
	notificationServices := notificationservices.NewTestServices(logger)

	// Construct the validated internal service container before any readiness-
	// critical internal workflow executes or HTTP traffic is accepted.
	shutdownChan := make(chan struct{})

	svc, err := services.NewService(
		logger,
		&models,
		&services.Config{
			DBTimeout: cfg.DBTimeout,
		},
		shutdownChan,
	)
	if err != nil {
		logger.Fatal(
			"internal service initialization failed",
			"error",
			err,
		)
	}

	// Ensure SPINE merchant program plan seed data synchronously before the
	// application is allowed to accept HTTP traffic. This is readiness-critical
	// reference data for Future Offering access, subscriptions, entitlements,
	// billing, and Merchant Center plan selection.
	if err := svc.EnsureDefaultMerchantProgramPlansInternal(ctx); err != nil {
		logger.Fatal("merchant program plan bootstrap failed", "error", err)
	}

	// Ensure SPINE merchant program entitlement seed data after canonical plans
	// exist and before the application accepts HTTP traffic. This establishes the
	// capability gates for Launch Campaign and Future Offering workflows.
	if err := svc.EnsureDefaultMerchantProgramEntitlementsInternal(ctx); err != nil {
		logger.Fatal("merchant program entitlement bootstrap failed", "error", err)
	}

	// Ensure SPINE platform settings seed data synchronously before the
	// application is allowed to accept HTTP traffic. These settings establish
	// platform-level governance defaults for Future Offering availability,
	// Watch behavior, platform settings administration, hard-delete safety, and
	// the explicit absence of a separate Future Commerce Notify Me feature.
	if err := svc.EnsureDefaultPlatformSettingsInternal(ctx); err != nil {
		logger.Fatal("platform setting bootstrap failed", "error", err)
	}

	// Start automation orchestrator after readiness-critical seed data has been ensured.
	services.StartOfferAutomationOrchestrator(svc)

	// Construct the HTTP application with the same validated internal service
	// container used for readiness-critical startup work. Handlers invoke domain
	// service methods through app.InternalServices; they do not construct service
	// dependencies or bypass the service boundary.
	app := &Application{
		Config: Config{
			Bootstrap:   *cfg,
			DB:          db,
			Models:      models,
			Logger:      logger,
			DBTimeout:   cfg.DBTimeout,
			JWTIssuer:   cfg.JWTIssuer,
			JWTAudience: cfg.JWTAudience,
		},
		Logger:           logger,
		Models:           models,
		Preloaded:        preloaded,
		TokenService:     tokenService,
		EmailService:     notificationServices.Email,
		SMSService:       notificationServices.SMS,
		InternalServices: svc,

		publicRoutes: newEndpointRegistry(),
	}

	if app.InternalServices == nil {
		logger.Fatal(
			"application internal services are required",
		)
	}

	// Start HTTP server
	srv := &http.Server{
		Addr:         ":" + cfg.WebPort,
		Handler:      app.Routes(),
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Run server in separate goroutine so we can catch signals
	go func() {
		logger.Info("HTTP server starting", "port", cfg.WebPort)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Fatal("HTTP server error", "error", err)
		}
	}()

	// Wait for SIGINT/SIGTERM and clean up
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop
	logger.Info("shutdown signal received")

	close(svc.ShutdownChan) // notify async orchestrator

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Error("graceful shutdown failed", "error", err)
	} else {
		logger.Info("server shut down cleanly")
	}
}

// ───────────────────────────────────────────────────────────────────────────────
// Helpers
// ───────────────────────────────────────────────────────────────────────────────

type PreloadedIDs struct {
	EntityTypeIDs map[string]uuid.UUID
	ActionIDs     map[string]uuid.UUID
}

// preloadEntityAndActionIDs pulls common audit IDs into memory to avoid repeated
// SELECTs on hot paths.
func preloadEntityAndActionIDs(
	ctx context.Context,
	db *pgxpool.Pool,
) (*PreloadedIDs, error) {
	entityRows, err := db.Query(ctx, `
		SELECT name, id
		FROM entity_types
	`)
	if err != nil {
		return nil, fmt.Errorf("query entity type preload rows: %w", err)
	}
	defer entityRows.Close()

	entityIDs := make(map[string]uuid.UUID)

	for entityRows.Next() {
		var name string
		var id uuid.UUID

		if err := entityRows.Scan(&name, &id); err != nil {
			return nil, fmt.Errorf("scan entity type preload row: %w", err)
		}

		entityIDs[name] = id
	}

	if err := entityRows.Err(); err != nil {
		return nil, fmt.Errorf("iterate entity type preload rows: %w", err)
	}

	actionRows, err := db.Query(ctx, `
		SELECT name, id
		FROM actions
	`)
	if err != nil {
		return nil, fmt.Errorf("query action preload rows: %w", err)
	}
	defer actionRows.Close()

	actionIDs := make(map[string]uuid.UUID)

	for actionRows.Next() {
		var name string
		var id uuid.UUID

		if err := actionRows.Scan(&name, &id); err != nil {
			return nil, fmt.Errorf("scan action preload row: %w", err)
		}

		actionIDs[name] = id
	}

	if err := actionRows.Err(); err != nil {
		return nil, fmt.Errorf("iterate action preload rows: %w", err)
	}

	return &PreloadedIDs{
		EntityTypeIDs: entityIDs,
		ActionIDs:     actionIDs,
	}, nil
}
