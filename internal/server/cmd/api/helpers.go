// Package main provides shared API helpers for trusted context extraction,
// authorization predicates, pagination, retry behavior, and common boundary utilities.
//
// sdworkspace/sdbackend/internal/server/cmd/api/helpers.go
//
// GTM:
//
//	Layer: 2.5 API / Authorization and Shared Helper Infrastructure
//	Release Class: SPINE
//	Reason:
//	  API helpers provide canonical trusted-context extraction, authorization
//	  predicates, request-scoped permission resolution, pagination, retry, and
//	  shared boundary utilities used across protected HTTP surfaces. These
//	  helpers are cross-cutting release infrastructure and must remain
//	  centralized, deterministic, fail-closed where security-sensitive, and
//	  production-ready.
//
// SPINE Rule:
//
//	Keep compiling.
//	Keep production-ready.
//	Preserve helpers.go as the API layer's single canonical home for shared
//  handler helpers. Organize shared helpers into clearly labeled functional
//  sections so they remain easy to locate, understand, and maintain. Do not
//  fragment shared helper categories across the handler layer.
//	Preserve canonical trusted-context identity and role extraction.
//	Preserve one canonical internal-role classification.
//	Preserve request-scoped permission-result reuse.
//	Preserve fail-closed behavior for missing or inconsistent authorization state.
//	Preserve exact permission enforcement without role-based bypass.
//	Do not introduce hidden authorization database fallbacks.
//	Do not introduce process-global authorization caches.
//	Do not duplicate central authorization predicates in handlers or middleware.
//	Block deployment if this file breaks authorization correctness,
//	context integrity, shared helper contracts, or protected API behavior.

// helpers.go remains the single canonical shared-helper file for the API
// layer, but its contents are internally organized into clearly marked functional sections:

// context helpers
// authorization helpers
// pagination helpers
// retry helpers
// value helpers
package main

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

// -----------------------------------------------------------------------------
// Context Helpers
// -----------------------------------------------------------------------------
// Use helper methods to enforce principle of pulling all trusted data from context only

// HasRole checks whether the authenticated user in ctx holds roleName by
// querying persistence directly. Unlike getRoleFromContext, this performs a
// fresh database lookup rather than trusting context — it exists for call
// sites that specifically need current, re-verified role state rather than
// the role resolved once by AuthMiddleware for this request.
func (app *Application) HasRole(ctx context.Context, roleName string) (bool, error) {
	uid := app.getUserIDFromContext(ctx)
	if uid == nil {
		return false, errors.New("userID missing from context")
	}
	ok, err := app.Models.Role.HasRole(ctx, *uid, roleName)
	if err != nil {
		app.Logger.GetLoggerWithContextFromContext(ctx).
			WithFunctionName("Application.HasRole").
			Error("role lookup failed", "err", err)
	}
	return ok, err
}

// HasAnyRole checks if the current user's trusted role name matches at
// least one of the specified roles (case-insensitive).
func (app *Application) HasAnyRole(ctx context.Context, roles ...string) bool {
	userRole, err := app.getRoleFromContextOrDB(ctx)
	if err != nil || userRole == "" {
		app.Logger.Warn("HasAnyRole: failed to resolve user role",
			"error", err,
			"resolvedRole", userRole,
		)
		return false
	}

	for _, allowed := range roles {
		if strings.EqualFold(userRole, allowed) {
			return true
		}
	}

	return false
}

// getRoleFromContextOrDB retrieves the user's role name from trusted request
// context. It does not fall back to persistence: AuthMiddleware is
// responsible for populating ctxRoleName for every authenticated request, so
// a missing value here means the request reached this code without passing
// through AuthMiddleware, and that must fail closed rather than trigger a
// hidden database lookup.
func (app *Application) getRoleFromContextOrDB(ctx context.Context) (string, error) {
	role := getRoleFromContext(ctx)
	if role == "" {
		return "", errors.New("role name missing from trusted request context")
	}
	return role, nil
}

// getRoleFromContext retrieves the authenticated user's role *name* from
// trusted context (ctxRoleName). Returns empty string if not found or
// invalid. This is distinct from getRoleIDFromContext, which returns the
// role's identity (ctxRoleID), not its name.
func getRoleFromContext(ctx context.Context) string {
	role, ok := ctx.Value(ctxRoleName).(string)
	role = strings.TrimSpace(role)
	if !ok || role == "" {
		return ""
	}
	return role
}

// getUserIDFromContext returns *uuid.UUID or nil if absent. This is the
// single canonical way to read the authenticated user's identity from
// context; ctxUserID is always stored as uuid.UUID by AuthMiddleware.
func (app *Application) getUserIDFromContext(ctx context.Context) *uuid.UUID {
	val, ok := ctx.Value(ctxUserID).(uuid.UUID)
	if !ok {
		return nil
	}
	return &val
}

func (app *Application) getTargetUserIDFromContext(ctx context.Context) *uuid.UUID {
	val, ok := ctx.Value(ctxTargetUserID).(uuid.UUID)
	if !ok {
		return nil
	}
	return &val
}

// getGuestUserIDFromContext retrieves the guest user ID from context.
// Returns nil if not found or invalid.
func (app *Application) getGuestUserIDFromContext(ctx context.Context) *uuid.UUID {
	val := ctx.Value("guest_user_id")
	if id, ok := val.(uuid.UUID); ok {
		return &id
	}
	return nil
}

// getRoleIDFromContext extracts the authenticated user's role *identity*
// (ctxRoleID, a role ID, not a role name) from context. Returns a pointer
// to uuid.UUID or nil if not present or invalid.
func (app *Application) getRoleIDFromContext(ctx context.Context) *uuid.UUID {
	val, ok := ctx.Value(ctxRoleID).(string)
	if !ok || val == "" {
		return nil
	}
	id, err := uuid.Parse(val)
	if err != nil {
		return nil
	}
	return &id
}

// getDashboardIDFromContext extracts the user dashboard ID from the context using ctxUserDashboardID.
// Returns a pointer to uuid.UUID or nil if not present or invalid.
func (app *Application) getDashboardIDFromContext(ctx context.Context) *uuid.UUID {
	val, ok := ctx.Value(ctxUserDashboardID).(string)
	if !ok || val == "" {
		return nil
	}
	id, err := uuid.Parse(val)
	if err != nil {
		return nil
	}
	return &id
}

// getDashboardTemplateIDFromContext extracts the dashboard template ID from context using ctxDashboardTemplateID.
// Returns a pointer to uuid.UUID or nil if not present or invalid.
func (app *Application) getDashboardTemplateIDFromContext(ctx context.Context) *uuid.UUID {
	val, ok := ctx.Value(ctxDashboardTemplateID).(string)
	if !ok || val == "" {
		return nil
	}
	id, err := uuid.Parse(val)
	if err != nil {
		return nil
	}
	return &id
}

// getMerchantApplicationIDFromContext extracts the merchant application ID from the context
// using the ctxMerchantApplicationID key. Returns a pointer to uuid.UUID or nil if not present or invalid.
func (app *Application) getMerchantApplicationIDFromContext(ctx context.Context) *uuid.UUID {
	val, ok := ctx.Value(ctxMerchantApplicationID).(string)
	if !ok || val == "" {
		return nil
	}
	id, err := uuid.Parse(val)
	if err != nil {
		return nil
	}
	return &id
}

// getAffiliatePerformanceIDFromContext extracts the affiliate performance ID from the context
// using the ctxAffiliatePerformanceID key. Returns a pointer to uuid.UUID or nil if not present or invalid.
func (app *Application) getAffiliatePerformanceIDFromContext(ctx context.Context) *uuid.UUID {
	val, ok := ctx.Value(ctxAffiliatePerformanceID).(string)
	if !ok || val == "" {
		return nil
	}
	id, err := uuid.Parse(val)
	if err != nil {
		return nil
	}
	return &id
}

// getAffiliateProgramIDFromContext extracts the affiliate program ID from the context using the ctxAffiliateProgramID key.
// Returns a pointer to uuid.UUID or nil if not present or invalid.
func (app *Application) getAffiliateProgramIDFromContext(ctx context.Context) *uuid.UUID {
	val, ok := ctx.Value(ctxAffiliateProgramID).(string)
	if !ok || val == "" {
		return nil
	}
	id, err := uuid.Parse(val)
	if err != nil {
		return nil
	}
	return &id
}

// getMerchantIDFromContext extracts the merchant ID from the context
// using the ctxMerchantID key. Returns a pointer to uuid.UUID or nil if not present or invalid.
func (app *Application) getMerchantIDFromContext(ctx context.Context) *uuid.UUID {
	val, ok := ctx.Value(ctxMerchantID).(string)
	if !ok || val == "" {
		return nil
	}
	id, err := uuid.Parse(val)
	if err != nil {
		return nil
	}
	return &id
}

// getStatusIDFromContext extracts the merchant application status ID from context.
// Returns a pointer to uuid.UUID or nil if not found or invalid.
func (app *Application) getStatusIDFromContext(ctx context.Context) *uuid.UUID {
	val, ok := ctx.Value(ctxStatusID).(string)
	if !ok || val == "" {
		return nil
	}
	id, err := uuid.Parse(val)
	if err != nil {
		return nil
	}
	return &id
}

// getOfferIDFromContext extracts the offer ID from context using ctxOfferID.
// Returns a pointer to uuid.UUID or nil if not present or invalid.
func (app *Application) getOfferIDFromContext(ctx context.Context) *uuid.UUID {
	val, ok := ctx.Value(ctxOfferID).(string)
	if !ok || val == "" {
		return nil
	}
	id, err := uuid.Parse(val)
	if err != nil {
		return nil
	}
	return &id
}

// getProductIDFromContext extracts the product ID from context.
// Returns a pointer to uuid.UUID or nil if not present or invalid.
func (app *Application) getProductIDFromContext(ctx context.Context) *uuid.UUID {
	val, ok := ctx.Value(ctxProductID).(string)
	if !ok || val == "" {
		return nil
	}
	id, err := uuid.Parse(val)
	if err != nil {
		return nil
	}
	return &id
}

// getBrandIDFromContext extracts the brand ID from the context using the ctxBrandID key.
// Returns a pointer to uuid.UUID or nil if not present or invalid.
func (app *Application) getBrandIDFromContext(ctx context.Context) *uuid.UUID {
	val, ok := ctx.Value(ctxBrandID).(string)
	if !ok || val == "" {
		return nil
	}
	id, err := uuid.Parse(val)
	if err != nil {
		return nil
	}
	return &id
}

// getUPCFromContext extracts the UPC (Universal Product Code) from the context using the ctxUPC key.
// Returns the string value or an empty string if not present or invalid.
func (app *Application) getUPCFromContext(ctx context.Context) string {
	val, ok := ctx.Value(ctxUPC).(string)
	if !ok || val == "" {
		return ""
	}
	return val
}

// getProductLineFromContext extracts the product line string from context.
func (app *Application) getProductLineFromContext(ctx context.Context) string {
	val, ok := ctx.Value(ctxProductLine).(string)
	if !ok || val == "" {
		return ""
	}
	return val
}

// getPlatformIDFromContext extracts the platform ID from context.
func (app *Application) getPlatformIDFromContext(ctx context.Context) *uuid.UUID {
	val, ok := ctx.Value(ctxPlatformID).(string)
	if !ok || val == "" {
		return nil
	}
	id, err := uuid.Parse(val)
	if err != nil {
		return nil
	}
	return &id
}

// getContextValueAsString retrieves a string value from context using the provided key.
func (app *Application) getContextValueAsString(ctx context.Context, key ctxKey) string {
	val, ok := ctx.Value(key).(string)
	if !ok || val == "" {
		return ""
	}
	return val
}

// getAuditLogIDFromContext extracts the audit log ID from context.
// Returns a pointer to uuid.UUID or nil if missing or invalid.
func (app *Application) getAuditLogIDFromContext(ctx context.Context) *uuid.UUID {
	val, ok := ctx.Value(ctxAuditLogID).(string)
	if !ok || val == "" {
		return nil
	}
	id, err := uuid.Parse(val)
	if err != nil {
		return nil
	}
	return &id
}

// getEntityTypeIDFromContext extracts the entity type ID from the context using the ctxEntityTypeID key.
// Returns a pointer to uuid.UUID or nil if not present or invalid.
func (app *Application) getEntityTypeIDFromContext(ctx context.Context) *uuid.UUID {
	val, ok := ctx.Value(ctxEntityTypeID).(string)
	if !ok || val == "" {
		return nil
	}
	id, err := uuid.Parse(val)
	if err != nil {
		return nil
	}
	return &id
}

// getActionIDFromContext extracts the action ID from the context using the ctxActionID key.
// Returns a pointer to uuid.UUID or nil if not present or invalid.
func (app *Application) getActionIDFromContext(ctx context.Context) *uuid.UUID {
	val, ok := ctx.Value(ctxActionID).(string)
	if !ok || val == "" {
		return nil
	}
	id, err := uuid.Parse(val)
	if err != nil {
		return nil
	}
	return &id
}

// getCategoryIDFromContext extracts the category ID from context using the ctxCategoryID key.
// Returns a pointer to uuid.UUID or nil if not present or invalid.
func (app *Application) getCategoryIDFromContext(ctx context.Context) *uuid.UUID {
	val, ok := ctx.Value(ctxCategoryID).(string)
	if !ok || val == "" {
		return nil
	}
	id, err := uuid.Parse(val)
	if err != nil {
		return nil
	}
	return &id
}

// getContextValueAsUUID retrieves a UUID value from context using the provided ctxKey.
// Returns a pointer to uuid.UUID or nil if missing or invalid.
func (app *Application) getContextValueAsUUID(ctx context.Context, key ctxKey) *uuid.UUID {
	val, ok := ctx.Value(key).(string)
	if !ok || val == "" {
		return nil
	}
	id, err := uuid.Parse(val)
	if err != nil {
		return nil
	}
	return &id
}

// getCouponIDFromContext extracts the coupon ID from the context using the ctxCouponID key.
// Returns a pointer to uuid.UUID or nil if not present or invalid.
func (app *Application) getCouponIDFromContext(ctx context.Context) *uuid.UUID {
	val, ok := ctx.Value(ctxCouponID).(string)
	if !ok || val == "" {
		return nil
	}
	id, err := uuid.Parse(val)
	if err != nil {
		return nil
	}
	return &id
}

// getOfferPriceHistoryIDFromContext extracts offer_price_history_id from context
func (app *Application) getOfferPriceHistoryIDFromContext(ctx context.Context) *uuid.UUID {
	val, ok := ctx.Value(ctxOfferPriceHistoryID).(string)
	if !ok || val == "" {
		return nil
	}
	id, err := uuid.Parse(val)
	if err != nil {
		return nil
	}
	return &id
}

// getPriceDropThresholdFromContext extracts a price drop threshold (float64) from the context
// using the ctxPriceDropThreshold key. Returns a pointer to float64 or nil if not present or invalid.
func (app *Application) getPriceDropThresholdFromContext(ctx context.Context) *float64 {
	val, ok := ctx.Value(ctxPriceDropThreshold).(string)
	if !ok || val == "" {
		return nil
	}
	threshold, err := strconv.ParseFloat(val, 64)
	if err != nil {
		return nil
	}
	return &threshold
}

// getPromotionIDFromContext extracts the promotion ID from the context.
func (app *Application) getPromotionIDFromContext(ctx context.Context) *uuid.UUID {
	val, ok := ctx.Value(ctxPromotionID).(string)
	if !ok || val == "" {
		return nil
	}
	id, err := uuid.Parse(val)
	if err != nil {
		return nil
	}
	return &id
}

// getMerchantPromotionIDFromContext extracts the merchant promotion ID from the context using the ctxMerchantPromotionID key.
// Returns a pointer to uuid.UUID or nil if not present or invalid.
func (app *Application) getMerchantPromotionIDFromContext(ctx context.Context) *uuid.UUID {
	val, ok := ctx.Value(ctxMerchantPromotionID).(string)
	if !ok || val == "" {
		return nil
	}
	id, err := uuid.Parse(val)
	if err != nil {
		return nil
	}
	return &id
}

// getOfferRatingIDFromContext extracts the offer rating ID from the context using the ctxOfferRatingID key.
// Returns a pointer to uuid.UUID or nil if not present or invalid.
func (app *Application) getOfferRatingIDFromContext(ctx context.Context) *uuid.UUID {
	val, ok := ctx.Value(ctxOfferRatingID).(string)
	if !ok || val == "" {
		return nil
	}
	id, err := uuid.Parse(val)
	if err != nil {
		return nil
	}
	return &id
}

// getOfferSponsorshipIDFromContext extracts the offer sponsorship ID from the context using the ctxOfferSponsorshipID key.
// Returns a pointer to uuid.UUID or nil if not present or invalid.
func (app *Application) getOfferSponsorshipIDFromContext(ctx context.Context) *uuid.UUID {
	val, ok := ctx.Value(ctxOfferSponsorshipID).(string)
	if !ok || val == "" {
		return nil
	}
	id, err := uuid.Parse(val)
	if err != nil {
		return nil
	}
	return &id
}

// getOfferStatusIDFromContext extracts the offer status ID from the context using the ctxOfferStatusID key.
// Returns a pointer to uuid.UUID or nil if not present or invalid.
func (app *Application) getOfferStatusIDFromContext(ctx context.Context) *uuid.UUID {
	val, ok := ctx.Value(ctxOfferStatusID).(string)
	if !ok || val == "" {
		return nil
	}
	id, err := uuid.Parse(val)
	if err != nil {
		return nil
	}
	return &id
}

// getAdminIDFromContext extracts the admin ID from context using ctxAdminID.
func (app *Application) getAdminIDFromContext(ctx context.Context) *uuid.UUID {
	val, ok := ctx.Value(ctxAdminID).(string)
	if !ok || val == "" {
		return nil
	}
	id, err := uuid.Parse(val)
	if err != nil {
		return nil
	}
	return &id
}

// getBrandNameFromContext extracts the brand name from context using ctxBrandName.
// Returns the string value or empty string if not present.
func (app *Application) getBrandNameFromContext(ctx context.Context) string {
	val, ok := ctx.Value(ctxBrandName).(string)
	if !ok {
		return ""
	}
	return val
}

// getUserFavoriteIDFromContext extracts the user favorite ID from context.
func (app *Application) getUserFavoriteIDFromContext(ctx context.Context) *uuid.UUID {
	val, ok := ctx.Value(ctxUserFavoriteID).(string)
	if !ok || val == "" {
		return nil
	}
	id, err := uuid.Parse(val)
	if err != nil {
		return nil
	}
	return &id
}

// -----------------------------------------------------------------------------
// Pagination Helpers
// -----------------------------------------------------------------------------
// parseLimitOffset parses pagination parameters from the request query string.
// It returns sanitized limit and offset values, defaulting to limit=50 and offset=0.
// It returns an error if either value is present but invalid.
//
// Usage Example:
//
//	limit, offset, err := app.parseLimitOffset(r)
func (app *Application) parseLimitOffset(r *http.Request) (int, int, error) {
	q := r.URL.Query()

	// --- Defaults ---
	const defaultLimit = 50
	const defaultOffset = 0
	limit := defaultLimit
	offset := defaultOffset

	// --- Parse 'limit' ---
	if raw := q.Get("limit"); raw != "" {
		val, err := strconv.Atoi(raw)
		if err != nil || val <= 0 {
			return 0, 0, fmt.Errorf("'limit' must be a positive integer")
		}
		limit = val
	}

	// --- Parse 'offset' ---
	if raw := q.Get("offset"); raw != "" {
		val, err := strconv.Atoi(raw)
		if err != nil || val < 0 {
			return 0, 0, fmt.Errorf("'offset' must be a non-negative integer")
		}
		offset = val
	}

	return limit, offset, nil
}

// parsePaginationParams returns a 1‑based page number and page size.
// Defaults: page=1, page_size=50. Rejects non‑positive values.
func (app *Application) parsePaginationParams(r *http.Request) (int, int, error) {
	q := r.URL.Query()

	const (
		defaultPage     = 1
		defaultPageSize = 50
		maxPageSize     = 500
	)

	page := defaultPage
	pageSize := defaultPageSize

	if raw := q.Get("page"); raw != "" {
		val, err := strconv.Atoi(raw)
		if err != nil || val <= 0 {
			return 0, 0, fmt.Errorf("'page' must be a positive integer")
		}
		page = val
	}

	if raw := q.Get("page_size"); raw != "" {
		val, err := strconv.Atoi(raw)
		if err != nil || val <= 0 || val > maxPageSize {
			return 0, 0, fmt.Errorf("'page_size' must be 1–%d", maxPageSize)
		}
		pageSize = val
	}

	return page, pageSize, nil
}

// -----------------------------------------------------------------------------
// Authorization Helpers
// -----------------------------------------------------------------------------
// authzState is the authorization resolution state for exactly one authenticated
// HTTP request and exactly one authenticated role. It must never be shared
// between requests or stored at package scope.
//
// The mutex intentionally covers both cache lookup and first persistence
// resolution. Authorization checks normally execute serially, so there is no
// useful request-level parallelism to preserve here; holding the request-local
// lock guarantees that concurrent checks for the same permission cannot issue
// duplicate queries or observe competing first-resolution results.
type authzState struct {
	mu      sync.Mutex
	roleID  uuid.UUID
	results map[string]bool
}

func newAuthzState(roleID uuid.UUID) *authzState {
	return &authzState{
		roleID:  roleID,
		results: make(map[string]bool),
	}
}

// resolve returns the authoritative result for permission for this request.
// Each distinct permission is resolved from persistence at most once. Errors
// fail closed and the denial is retained for the remainder of the request.
func (s *authzState) resolve(ctx context.Context, app *Application, permission string) bool {
	if s == nil || s.roleID == uuid.Nil || permission == "" {
		return false
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if result, ok := s.results[permission]; ok {
		return result
	}

	ok, err := app.Models.RolePermission.RoleHasPermission(ctx, s.roleID.String(), permission)
	if err != nil {
		app.Logger.GetLoggerWithContextFromContext(ctx).
			WithFunctionName("authzState.resolve").
			Error("permission lookup failed",
				"permission", permission,
				"role_id", s.roleID,
				"err", err,
			)
		ok = false
	}

	s.results[permission] = ok
	return ok
}

// HasPermission checks whether the authenticated actor's role holds the
// specified permission using the request-scoped authorization state established
// by AuthMiddleware. Missing, malformed, mismatched, or empty authorization
// context fails closed; there is no hidden persistence fallback.
func (app *Application) HasPermission(ctx context.Context, permissionName string) bool {
	permissionName = strings.TrimSpace(permissionName)
	if permissionName == "" {
		app.Logger.Warn("HasPermission: empty permission name")
		return false
	}

	roleID := app.getRoleIDFromContext(ctx)
	if roleID == nil || *roleID == uuid.Nil {
		app.Logger.Warn("HasPermission: missing role ID in context", "permission", permissionName)
		return false
	}

	state, ok := ctx.Value(ctxAuthzState).(*authzState)
	if !ok || state == nil {
		app.Logger.Warn("HasPermission: missing request-scoped authorization state",
			"permission", permissionName,
			"role_id", *roleID,
		)
		return false
	}

	if state.roleID != *roleID {
		app.Logger.Warn("HasPermission: authorization state role mismatch",
			"permission", permissionName,
			"context_role_id", *roleID,
			"state_role_id", state.roleID,
		)
		return false
	}

	return state.resolve(ctx, app, permissionName)
}

// RequireInternalPermission checks whether the current actor is an internal
// actor (per the canonical isInternalRole classification) AND holds the
// specified permission. Returns true only if both conditions are met.
//
// Used for enforcing stricter access policies in sensitive internal handlers,
// and as the single reusable "internal AND permission" predicate consumed by
// RequireSelfOrPrivileged.
func (app *Application) RequireInternalPermission(ctx context.Context, permission string) bool {
	role := getRoleFromContext(ctx)
	if role == "" {
		return false
	}

	if !isInternalRole(role) {
		return false
	}

	return app.HasPermission(ctx, permission)
}

// IsInternalUser returns true if the caller's trusted role name is one of
// Sagrenti's internal roles, per the canonical isInternalRole classification.
func (app *Application) IsInternalUser(ctx context.Context) bool {
	return isInternalRole(getRoleFromContext(ctx))
}

// isInternalRole is the single canonical definition of "internal actor" for
// Sagrenti authorization. AuthMiddleware, RequireInternalPermission, and
// IsInternalUser all delegate to this function so the classification cannot
// drift out of sync between them.
func isInternalRole(roleName string) bool {
	switch strings.ToLower(strings.TrimSpace(roleName)) {
	case "admin", "super_admin", "internal_operator":
		return true
	default:
		return false
	}
}

// -----------------------------------------------------------------------------
// Retry Helpers
// -----------------------------------------------------------------------------
// retryActivationTokenWithBackoff attempts the provided operation with exponential backoff and jitter.
//
// It retries the operation up to `maxRetries` times. On each failure, it waits for a backoff duration
// (starting from `initialBackoff`) plus a random jitter before retrying. If all attempts fail,
// it returns an error indicating that the maximum number of retries was reached.
func retryActivationTokenWithBackoff(ctx context.Context, operation func() (string, error)) (string, error) {
	const (
		initialBackoff = 500 * time.Millisecond // Initial backoff duration
		maxRetries     = 5                      // Maximum number of retry attempts
		maxJitter      = 500                    // Maximum jitter in milliseconds
	)

	backoff := initialBackoff

	for attempt := 1; attempt <= maxRetries; attempt++ {
		// Respect context deadline or cancellation
		select {
		case <-ctx.Done():
			return "", fmt.Errorf("operation canceled or timed out: %w", ctx.Err())
		default:
			// continue with attempt
		}

		// Attempt the operation
		result, err := operation()
		if err == nil {
			return result, nil // Success
		}

		// Calculate jittered backoff
		jitter := time.Duration(rand.Intn(maxJitter)) * time.Millisecond
		sleepDuration := backoff + jitter

		time.Sleep(sleepDuration)
		backoff *= 2 // Exponential backoff
	}

	return "", errors.New("retryActivationTokenWithBackoff: maximum retries reached")
}

// -----------------------------------------------------------------------------
// Value Helpers
// -----------------------------------------------------------------------------
// getUUIDOrNil safely returns the UUID value if the pointer is non-nil; otherwise, returns uuid.Nil.
func getUUIDOrNil(id *uuid.UUID) uuid.UUID {
	if id == nil {
		return uuid.Nil
	}
	return *id
}

// parseIntOrDefault parses a string into an int, returning the result if successful.
// Returns the provided default value if the input is invalid or cannot be parsed.
func parseIntOrDefault(s string, def int) int {
	n, err := strconv.Atoi(s)
	if err != nil {
		return def
	}
	return n
}

// helper: fall back to default when pointer is nil
func derefOr(def string, v *string) string {
	if v != nil {
		return *v
	}
	return def
}
