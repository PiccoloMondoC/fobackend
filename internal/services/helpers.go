// Package services contains business logic for internal operations such as moderation,
// automation, audit logging, and data synchronization. It is used by async routines
// and internal system workflows, not exposed via public API routes.
//
// sdworkspace/sdbackend/internal/services/internal-services/helpers.go

// CE, during your review, please consider splitting this file into
// ssdworkspace/sdbackendd/internal/services/internal-services/service.go
// sdworkspace/sdbackend/internal/services/internal-services/offer-description.go
// sdworkspace/sdbackend/internal/services/internal-services/offer-fraud.go
// sdworkspace/sdbackend/internal/services/internal-services/status-resolution.go
// sdworkspace/sdbackend/internal/services/internal-services/pagination.go
// sdworkspace/sdbackend/internal/services/internal-services/notification-jobs.go
//
// GTM:
//   Layer: 2.6 Internal Services / Automation Foundation
//   Release Class: SPINE
//   Reason:
//     Internal service helpers support moderation, offer lifecycle resolution,
//     pagination safety, and internal automation paths. These helpers are
//     load-bearing for production service behavior and must preserve canonical
//     decimal-string handling for offer monetary and percentage fields.
//
// SPINE Rule:
//   Keep compiling.
//   Preserve canonical decimal string handling via exact rational parsing.
//   Do not cast offer price or discount fields to float64.
//   Preserve bounded pagination defaults.
//   Preserve offer moderation safety checks.
//   Preserve DB-timeout behavior for status resolution.
//   Block deployment if this file breaks build.
package services

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/PiccoloMondoC/sdworkspace/sdbackend/internal/data"
	"github.com/PiccoloMondoC/sdworkspace/sdbackend/internal/logging"

	"github.com/google/uuid"
)

// Constants used in fraud heuristics and internal pagination.
const (
	minTitleLength          = 5   // Minimum acceptable title length
	defaultBatchSize        = 100
	autoValidationThreshold = 0.2 // 20% deviation
)

// suspiciousDiscountThresholdPercent is the canonical rational replacement for
// the old suspiciousDiscountThreshold*100 float math. DiscountPercent is a
// percentage string ("85" == 85%), per CE decision.
var suspiciousDiscountThresholdPercent = big.NewRat(85, 1)

// Config holds configuration needed by the service layer (e.g., timeouts).
type Config struct {
	DBTimeout time.Duration // Timeout to apply to database-bound operations
}

// Service exposes shared dependencies (logger, DB models, config) to internal services.
// It is instantiated once in main() and reused across async routines and moderation handlers.
type Service struct {
	Logger       *logging.Logger // Structured logging with context and function tagging
	Models       *data.Models    // Database abstraction layer (already initialized in main)
	Cfg          *Config         // Internal config (not tied to app.Config to avoid import cycles)
	ShutdownChan chan struct{}
}

// parseOfferDecimal converts a nullable decimal string field (offer price or
// discount percent) into *big.Rat. Returns (nil, nil) for a nil/empty input.
// BEG §12: offer monetary and percentage fields are canonical decimal strings;
// never cast to float64.
func parseOfferDecimal(raw *string) (*big.Rat, error) {
	if raw == nil || strings.TrimSpace(*raw) == "" {
		return nil, nil
	}

	r, ok := new(big.Rat).SetString(strings.TrimSpace(*raw))
	if !ok {
		return nil, fmt.Errorf("invalid decimal value %q", *raw)
	}

	return r, nil
}

// generateTextualDescriptionForOffer creates a human-readable, persuasive description for a offer.
//
// This function is invoked by internal automation or editorial workflows to enhance the offer's
// content quality. It does not rely on external APIs but follows heuristics for clarity,
// marketing appeal, and completeness.
//
// Parameters:
// - ctx: Timeout-aware context for cancellation and logging.
// - offer: Fully populated offer object (validated before call)
//
// Returns:
// - Generated description string on success
// - Error if offer is nil or metadata is insufficient
func (s *Service) generateTextualDescriptionForOffer(ctx context.Context, offer *data.Offer) (string, error) {
	logger := s.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("generateTextualDescriptionForOffer")

	if offer == nil {
		logger.Error("Nil offer received")
		return "", errors.New("offer must not be nil")
	}

	price, err := parseOfferDecimal(offer.Price)
	if err != nil {
		logger.Error("Invalid offer price", "offer_id", offer.ID, "error", err)
		return "", fmt.Errorf("offer price invalid: %w", err)
	}

	if strings.TrimSpace(offer.Title) == "" || price == nil || price.Sign() <= 0 || strings.TrimSpace(offer.Currency) == "" {
		logger.Error("Missing critical metadata", "offer_id", offer.ID)
		return "", errors.New("offer metadata incomplete")
	}

	var builder strings.Builder

	builder.WriteString(fmt.Sprintf(
		"Grab the offer on **%s** for just **%s %s**",
		offer.Title,
		price.FloatString(2),
		strings.ToUpper(offer.Currency),
	))

	discount, err := parseOfferDecimal(offer.DiscountPercent)
	if err != nil {
		logger.Warn("Invalid discount percent", "offer_id", offer.ID, "error", err)
	}

	if discount != nil && discount.Sign() > 0 {
		builder.WriteString(fmt.Sprintf(
			" — that's a **%s%% discount** off the regular price!",
			discount.FloatString(0),
		))
	} else {
		builder.WriteString(".")
	}

	if offer.IsEditorialApproved {
		builder.WriteString(" This offer is editorially approved, meaning it's been verified by our team for quality and value.")
	}

	if strings.TrimSpace(offer.AffiliateURL) != "" {
		builder.WriteString(" Click the link to shop now and support us at no extra cost.")
	}

	description := builder.String()
	logger.Debug("Generated textual description", "offer_id", offer.ID, "description", description)

	return description, nil
}

// isSuspiciousOffer checks if a offer matches known fraud heuristics.
// Intended for internal use during fraud scans and moderation automation.
//
// Heuristics:
// - Discount percent unparsable, or > 85%
// - Title is missing or too short
// - Missing merchant ID or affiliate URL
func (s *Service) isSuspiciousOffer(ctx context.Context, offer data.Offer) bool {
	logger := s.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("isSuspiciousOffer")

	discount, err := parseOfferDecimal(offer.DiscountPercent)
	if err != nil {
		logger.Warn("Invalid discount percent", "offer_id", offer.ID, "error", err)
		return true
	}

	if discount != nil && discount.Cmp(suspiciousDiscountThresholdPercent) > 0 {
		logger.Warn("Suspicious discount detected", "offer_id", offer.ID, "discount_percent", discount.FloatString(2))
		return true
	}

	if strings.TrimSpace(offer.Title) == "" || len(strings.TrimSpace(offer.Title)) < minTitleLength {
		logger.Warn("Title too short or missing", "offer_id", offer.ID, "title", offer.Title)
		return true
	}

	if strings.TrimSpace(offer.AffiliateURL) == "" || offer.MerchantID == uuid.Nil {
		logger.Warn("Missing affiliate URL or merchant ID", "offer_id", offer.ID)
		return true
	}

	return false
}

// resolveStatusID looks up a offer status by its name and returns the corresponding UUID.
// It applies a DB timeout and performs structured logging.
//
// Parameters:
// - ctx: Context for timeout and tracing
// - statusName: Case-insensitive name of the status (e.g., "pending", "approved")
//
// Returns:
// - UUID pointer to the status if found
// - Error if status is not found or lookup fails
func (s *Service) resolveStatusID(ctx context.Context, statusName string) (*uuid.UUID, error) {
	logger := s.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("resolveStatusID")

	if strings.TrimSpace(statusName) == "" {
		return nil, errors.New("status name cannot be empty")
	}

	ctx, cancel := context.WithTimeout(ctx, s.Cfg.DBTimeout)
	defer cancel()

	status, err := s.Models.OfferStatus.GetByName(ctx, statusName)
	if err != nil {
		logger.Warn("status lookup failed", "status_name", statusName, "error", err)
		return nil, fmt.Errorf("status '%s' not found", statusName)
	}

	return &status.ID, nil
}

// parseLimit ensures a safe, bounded limit value for internal queries.
//
// This function is used by background services to sanitize any caller-provided
// limit inputs. It applies defaults and maximum thresholds to prevent unbounded loads.
//
// Parameters:
// - ctx: Context containing request metadata (used for logging)
// - rawLimit: User-supplied or automation-supplied integer (may be <= 0 or very large)
//
// Returns:
// - Clamped and validated limit value between defaultLimit and maxLimit
func (s *Service) parseLimit(ctx context.Context, rawLimit int) int {
	logger := s.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("parseLimit")

	const (
		defaultLimit = 10  // Fallback when rawLimit is zero or negative
		maxLimit     = 100 // Protection against runaway or abusive limits
	)

	var finalLimit int
	switch {
	case rawLimit <= 0:
		finalLimit = defaultLimit
	case rawLimit > maxLimit:
		finalLimit = maxLimit
	default:
		finalLimit = rawLimit
	}

	logger.Debug("Parsed limit value",
		"raw_limit", rawLimit,
		"final_limit", finalLimit,
	)

	return finalLimit
}

// parseLimitOffsetInternal returns safe pagination values (limit and offset) for internal service logic.
//
// This method is intended for automation and non-HTTP routines. It applies internal defaults
// and clamped bounds to prevent unbounded DB calls.
//
// Parameters:
// - rawLimit: Desired page size (can be zero or negative for default fallback)
// - rawOffset: Desired start offset (must be zero or positive)
//
// Returns:
// - Sanitized limit and offset values for DB queries
func (s *Service) parseLimitOffsetInternal(ctx context.Context, rawLimit, rawOffset int) (int, int) {
	logger := s.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("parseLimitOffsetInternal")

	const (
		defaultLimit = 100 // Default batch size for internal jobs
		maxLimit     = 500 // Max allowed limit to prevent abuse
	)

	limit := defaultLimit
	switch {
	case rawLimit > 0 && rawLimit <= maxLimit:
		limit = rawLimit
	case rawLimit > maxLimit:
		limit = maxLimit
	}

	offset := 0
	if rawOffset > 0 {
		offset = rawOffset
	}

	logger.Debug("Pagination parameters resolved",
		"raw_limit", rawLimit,
		"raw_offset", rawOffset,
		"final_limit", limit,
		"final_offset", offset,
	)

	return limit, offset
}

// SendSystemMaintenanceNotificationAsync is a thin adapter that creates a
// NotifyUserEvent and forwards it to InsertNotificationAsync.  It satisfies
// the orchestrator's Job.Action signature (ctx, *Service).
func SendSystemMaintenanceNotificationAsync(ctx context.Context, svc *Service) {
	// TODO: replace with a real system/automation UUID.
	adminID := uuid.New()

	ev := NotifyUserEvent{
		UserID:         adminID,                     // recipient
		Type:           "system_maintenance_notice",  // resolves NotificationType
		Message:        "Platform maintenance at 02:00 UTC. Expect brief downtime.",
		DeliveryMethod: "email",                      // resolves NotificationChannel
		CreatedBy:      adminID,                       // actor (system)
	}

	// Fire-and-forget insert.
	InsertNotificationAsync(ctx, svc, ev)
}