// sdworkspace/sdbackend/internal/server/cmd/api/helpers.go

// CE, during your review, please consider splitting this file into
// sdworkspace/sdbackend/internal/server/cmd/api/context-helpers.go
// sdworkspace/sdbackend/internal/server/cmd/api/authz-helpers.go
// sdworkspace/sdbackend/internal/server/cmd/api/pagination-helpers.go
// sdworkspace/sdbackend/internal/server/cmd/api/retry-helpers.go
// sdworkspace/sdbackend/internal/server/cmd/api/value-helpers.go
package main

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Use helper methods to enforce principle of pulling all trusted data from context only

// HasRole checks whether the authenticated user in ctx holds roleName.
// It delegates to RoleModel.HasRole and logs (never panics) on DB failure.
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

// HasAnyRole checks if the current user has at least one of the specified roles.
//
// It first tries to retrieve the user's role from context (via getRoleFromContext).
// If not found, it attempts to load the role from the database using the user ID in context.
// Logging is performed if role resolution fails. Returns true on the first match (case-insensitive).
func (app *Application) HasAnyRole(ctx context.Context, roles ...string) bool {
	// Resolve the user's role from context or fallback to DB
	userRole, err := app.getRoleFromContextOrDB(ctx)
	if err != nil || userRole == "" {
		app.Logger.Warn("HasAnyRole: failed to resolve user role",
			"error", err,
			"resolvedRole", userRole,
		)
		return false
	}

	// Compare the resolved role with the list of allowed roles
	for _, allowed := range roles {
		if strings.EqualFold(userRole, allowed) {
			return true
		}
	}

	return false
}

// getRoleFromContextOrDB retrieves the user role name from context if available,
// or loads it from the database as a fallback. Returns role name and any error encountered.
func (app *Application) getRoleFromContextOrDB(ctx context.Context) (string, error) {
	role := getRoleFromContext(ctx)
	if role != "" {
		return role, nil
	}

	userIDStr, ok := ctx.Value(ctxUserID).(string)
	if !ok || userIDStr == "" {
		return "", errors.New("missing user ID in context")
	}

	userID, err := uuid.Parse(userIDStr)
	if err != nil {
		return "", fmt.Errorf("invalid user ID: %w", err)
	}

	user, err := app.Models.User.GetByID(ctx, userID)
	if err != nil {
		return "", fmt.Errorf("failed to load user: %w", err)
	}

	roleModel, err := app.Models.Role.GetRoleByID(ctx, user.RoleID)
	if err != nil {
		return "", fmt.Errorf("failed to load user role: %w", err)
	}

	return roleModel.Name, nil
}

// getRoleFromContext retrieves the role name from the context.
// Returns empty string if not found or invalid.
func getRoleFromContext(ctx context.Context) string {
	role, ok := ctx.Value(ctxRoleID).(string)
	if !ok || role == "" {
		return ""
	}
	return role
}

/*
// getUserIDFromContext returns the *uuid.UUID stored in ctx or an error if absent / malformed.
func (app *Application) getUserIDFromContext(ctx context.Context) (*uuid.UUID, error) {
	v := ctx.Value(ctxKeyUserID)
	id, ok := v.(uuid.UUID)
	if !ok {
		return nil, errors.New("user ID missing from context")
	}
	return &id, nil
}*/

// getUserIDFromContext returns *uuid.UUID or nil if absent.
func (app *Application) getUserIDFromContext(ctx context.Context) *uuid.UUID {
	val, ok := ctx.Value(ctxUserID).(uuid.UUID) // <- keeps existing ctxUserID key
	if !ok {
		return nil
	}
	return &val
}

/*
func (app *Application) mustUserID(ctx context.Context) (*uuid.UUID, error) {
	if id := app.getUserIDFromContext(ctx); id != nil {
		return id, nil
	}
	return nil, errors.New("user ID missing from context")
}*/

func (app *Application) getTargetUserIDFromContext(ctx context.Context) *uuid.UUID {
	val, ok := ctx.Value(ctxTargetUserID).(uuid.UUID)
	if !ok {
		return nil
	}
	return &val
}

/*
// getAdminUserIDForNotifications returns a hardcoded or config-driven admin user ID.
// You may later replace this with a real lookup or broadcast logic.
func (app *Application) getAdminUserIDForNotifications(ctx context.Context) *uuid.UUID {
	// TODO: Replace with real admin user resolution logic
	adminID, err := uuid.Parse("00000000-0000-0000-0000-000000000001") // placeholder
	if err != nil {
		return nil
	}
	return &adminID
}*/

// getGuestUserIDFromContext retrieves the guest user ID from context.
// Returns nil if not found or invalid.
func (app *Application) getGuestUserIDFromContext(ctx context.Context) *uuid.UUID {
	val := ctx.Value("guest_user_id")
	if id, ok := val.(uuid.UUID); ok {
		return &id
	}
	return nil
}

// getRoleIDFromContext extracts the role ID from the context using the ctxRoleID key.
// Returns a pointer to uuid.UUID or nil if not present or invalid.
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

/*
// getOfferIDsFromContext extracts a list of offer IDs from the context using the ctxOfferIDs key.
// Expects a comma-separated list of UUIDs in string form. Returns a slice of parsed UUIDs, omitting invalid ones.
func (app *Application) getOfferIDsFromContext(ctx context.Context) []uuid.UUID {
	val, ok := ctx.Value(ctxOfferIDs).(string)
	if !ok || val == "" {
		return nil
	}

	ids := strings.Split(val, ",")
	var offerIDs []uuid.UUID
	for _, idStr := range ids {
		id, err := uuid.Parse(strings.TrimSpace(idStr))
		if err == nil {
			offerIDs = append(offerIDs, id)
		}
	}

	return offerIDs
}*/

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

/*
// getMerchantTypeIDFromContext extracts the merchant type ID from context using ctxMerchantTypeID.
func (app *Application) getMerchantTypeIDFromContext(ctx context.Context) *uuid.UUID {
	val, ok := ctx.Value(ctxMerchantTypeID).(string)
	if !ok || val == "" {
		return nil
	}
	id, err := uuid.Parse(val)
	if err != nil {
		return nil
	}
	return &id
}*/

/*
// getArchivedTimeRangeFromContext extracts the archived time range from context using ctxArchivedTimeRange.
// Returns the string value directly (e.g. "last_30_days") or empty string if not present.
func (app *Application) getArchivedTimeRangeFromContext(ctx context.Context) string {
	val, ok := ctx.Value(ctxArchivedTimeRange).(string)
	if !ok {
		return ""
	}
	return val
}*/

// getBrandNameFromContext extracts the brand name from context using ctxBrandName.
// Returns the string value or empty string if not present.
func (app *Application) getBrandNameFromContext(ctx context.Context) string {
	val, ok := ctx.Value(ctxBrandName).(string)
	if !ok {
		return ""
	}
	return val
}

/*
// getClientIDFromContext extracts the client ID from context using ctxClientID.
func (app *Application) getClientIDFromContext(ctx context.Context) *uuid.UUID {
	val, ok := ctx.Value(ctxClientID).(string)
	if !ok || val == "" {
		return nil
	}
	id, err := uuid.Parse(val)
	if err != nil {
		return nil
	}
	return &id
}*/

/*
// getOfferAlertIDFromContext extracts the offer alert ID from context using ctxOfferAlertID.
func (app *Application) getOfferAlertIDFromContext(ctx context.Context) *uuid.UUID {
	val, ok := ctx.Value(ctxOfferAlertID).(string)
	if !ok || val == "" {
		return nil
	}
	id, err := uuid.Parse(val)
	if err != nil {
		return nil
	}
	return &id
}*/

/*
// getOfferShareIDFromContext extracts the offer share ID from context using ctxOfferShareID.
func (app *Application) getOfferShareIDFromContext(ctx context.Context) *uuid.UUID {
	val, ok := ctx.Value(ctxOfferShareID).(string)
	if !ok || val == "" {
		return nil
	}
	id, err := uuid.Parse(val)
	if err != nil {
		return nil
	}
	return &id
}*/

/*
// getEntityIDFromContext extracts the entity ID from context using ctxEntityID.
func (app *Application) getEntityIDFromContext(ctx context.Context) *uuid.UUID {
	val, ok := ctx.Value(ctxEntityID).(string)
	if !ok || val == "" {
		return nil
	}
	id, err := uuid.Parse(val)
	if err != nil {
		return nil
	}
	return &id
}*/

/*
// getUserOfferPurchaseHistoryIDFromContext extracts the user offer purchase history ID from context.
func (app *Application) getUserOfferPurchaseHistoryIDFromContext(ctx context.Context) *uuid.UUID {
	val, ok := ctx.Value(ctxUserOfferPurchaseHistoryID).(string)
	if !ok || val == "" {
		return nil
	}
	id, err := uuid.Parse(val)
	if err != nil {
		return nil
	}
	return &id
}*/

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

/*
// getSourceUserIDFromContext extracts the source user ID from context.
func (app *Application) getSourceUserIDFromContext(ctx context.Context) *uuid.UUID {
	val, ok := ctx.Value(ctxSourceUserID).(string)
	if !ok || val == "" {
		return nil
	}
	id, err := uuid.Parse(val)
	if err != nil {
		return nil
	}
	return &id
}*/

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

/*
// extractUUIDFromURL extracts a UUID from the last segment of a URL string.
// Returns uuid.Nil if parsing fails.
func (app *Application) extractUUIDFromURL(url string) uuid.UUID {
	parts := strings.Split(url, "/")
	if len(parts) == 0 {
		return uuid.Nil
	}
	id, err := uuid.Parse(parts[len(parts)-1])
	if err != nil {
		return uuid.Nil
	}
	return id
}*/

// getUUIDOrNil safely returns the UUID value if the pointer is non-nil; otherwise, returns uuid.Nil.
func getUUIDOrNil(id *uuid.UUID) uuid.UUID {
	if id == nil {
		return uuid.Nil
	}
	return *id
}

// HasPermission checks if the user’s current role (from context) has the specified permission.
//
// It extracts the role ID from context and verifies it against the required permission.
// If the role ID is nil or an internal error occurs, the function fails closed and logs the issue.
// Returns true if the role has the permission, false otherwise.
func (app *Application) HasPermission(ctx context.Context, permissionName string) bool {
	// Extract role ID from context
	roleID := app.getRoleIDFromContext(ctx)
	if roleID == nil {
		app.Logger.Warn("HasPermission: missing role ID in context",
			"permission", permissionName,
		)
		return false
	}

	// Convert *uuid.UUID to string for database query
	roleIDStr := roleID.String()

	// Check permission via model
	ok, err := app.Models.RolePermission.RoleHasPermission(ctx, roleIDStr, permissionName)
	if err != nil {
		app.Logger.Error("HasPermission: error checking permission",
			"permission", permissionName,
			"role_id", roleIDStr,
			"error", err,
		)
		return false
	}

	return ok
}

// RequireInternalPermission checks whether the current actor has the specified permission
// AND is an internal role (admin or internal_operator). Returns true if both conditions are met.
//
// Used for enforcing stricter access policies in sensitive internal handlers.
func (app *Application) RequireInternalPermission(ctx context.Context, permission string) bool {
	role := getRoleFromContext(ctx)
	if role == "" {
		return false
	}

	// Enforce both role and permission checks
	isInternal := strings.EqualFold(role, "admin") || strings.EqualFold(role, "internal_operator")
	if !isInternal {
		return false
	}

	return app.HasPermission(ctx, permission)
}

// IsInternalUser returns true if the caller’s role is one of your trusted
// internal roles.  This replaces the missing app.IsInternalUser() referenced in
// earlier drafts.
func (app *Application) IsInternalUser(ctx context.Context) bool {
	role := getRoleFromContext(ctx)
	switch role {
	case "admin", "super_admin", "internal_operator":
		return true
	default:
		return false
	}
}

// ───────────────────────────────────────────────────────────────────────────────
// helper: isInternalRole  – true when the supplied role name is one of your
//
//	trusted “internal” roles.
//
// ───────────────────────────────────────────────────────────────────────────────
func isInternalRole(roleName string) bool {
	switch strings.ToLower(roleName) {
	case "admin", "super_admin", "internal_operator":
		return true
	default:
		return false
	}
}

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

		// Optional: log retry attempt and backoff duration (can be enhanced with a logger)
		// fmt.Printf("Attempt %d failed: %v. Retrying in %v...\n", attempt, err, sleepDuration)

		time.Sleep(sleepDuration)
		backoff *= 2 // Exponential backoff
	}

	return "", errors.New("retryActivationTokenWithBackoff: maximum retries reached")
}

/*
// parseOptionalInt parses a string into an int and returns a pointer to the value.
// Returns nil if the string is empty or parsing fails.
func parseOptionalInt(s string) *int {
	if s == "" {
		return nil
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return nil
	}
	return &n
}*/

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

/*
func defaultIfBlank(value, def string) string {
	if strings.TrimSpace(value) == "" {
		return def
	}
	return value
}*/
