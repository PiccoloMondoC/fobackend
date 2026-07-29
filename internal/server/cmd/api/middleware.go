// sdworkspace/sdbackend/internal/server/cmd/api/middleware.go
package main

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"sync"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"golang.org/x/time/rate"
)

// TODO: As the API grows, consider combining related routes or simplifying permission checks for maintainability.

type ctxKey string

const ctxActionID ctxKey = "actionID"
const ctxAdminID ctxKey = "adminID"
const ctxAffiliatePerformanceID ctxKey = "affiliatePerformanceID"
const ctxAffiliateProgramID ctxKey = "affiliateProgramID"

// const ctxArchivedTimeRange ctxKey = "archivedTimeRange"
const ctxAuditLogID ctxKey = "auditLogID"
const ctxBrandID ctxKey = "brandID"
const ctxBrandName ctxKey = "brandName"
const ctxCategoryID ctxKey = "categoryID"
const ctxCouponID ctxKey = "couponID"
const ctxClientID ctxKey = "clientID"
const ctxDashboardTemplateID ctxKey = "dashboardTemplateID"
const ctxOfferID ctxKey = "offerID"
const ctxOfferAlertID ctxKey = "offerAlertID"
const ctxOfferIDs ctxKey = "offerIDs"
const ctxOfferPriceHistoryID ctxKey = "offerPriceHistoryID"
const ctxOfferRatingID ctxKey = "offerRatingID"
const ctxOfferSponsorshipID ctxKey = "offerSponsorshipID"
const ctxOfferShareID ctxKey = "offerShareID"
const ctxOfferStatusID ctxKey = "offerStatusID"
const ctxEntityID ctxKey = "entityID"
const ctxEntityTypeID ctxKey = "entityTypeID"
const ctxMerchantApplicationID ctxKey = "merchantApplicationID"
const ctxMerchantID ctxKey = "merchantID"
const ctxMerchantPromotionID ctxKey = "merchantPromotionID"
const ctxMerchantTypeID ctxKey = "merchantTypeID"
const ctxPlatformID ctxKey = "platformID"
const ctxPriceDropThreshold ctxKey = "priceDropThreshold"
const ctxProductID ctxKey = "productID"
const ctxProductLine ctxKey = "productLine"
const ctxPromotionID ctxKey = "promotionID"
const ctxRoleID ctxKey = "roleID"
const ctxStatusID ctxKey = "statusID"
const ctxUPC ctxKey = "upc"
const ctxGuestUserID ctxKey = "guestUserID"
const ctxUserDashboardID ctxKey = "userDashboardID"
const ctxUserOfferPurchaseHistoryID ctxKey = "userOfferPurchaseHistoryID"
const ctxUserFavoriteID ctxKey = "userFavoriteID"
const ctxUserID ctxKey = "userID"

// ctxKeyUserID is an alias so any new code compiles without edits elsewhere.
// const ctxKeyUserID = ctxUserID
const ctxUserNotificationID ctxKey = "userNotificationID"
const ctxUserSettingsID ctxKey = "userSettingsID"
const ctxSourceUserID ctxKey = "sourceUserID"
const ctxTargetUserID ctxKey = "targetUserID"

// RateLimiterStore holds per-user or per-IP limiters
var (
	limiterStore sync.Map // Concurrency-safe map for storing limiters
)

var (
	failedAttempts sync.Map // Tracks failed activation attempts per IP/User
)

// NewLimiter creates a rate limiter (1 request per second, burst of 5)
func NewLimiter() *rate.Limiter {
	return rate.NewLimiter(1, 5)
}

// Track failed attempts
func trackFailedAttempt(identifier string) int {
	count, _ := failedAttempts.LoadOrStore(identifier, 0)
	newCount := count.(int) + 1
	failedAttempts.Store(identifier, newCount)

	// Increment Prometheus metric
	failedActivations.WithLabelValues(identifier).Inc()

	return newCount
}

// Reset failed attempts after successful activation
func resetFailedAttempts(identifier string) {
	failedAttempts.Delete(identifier)
}

// Pagination and filter context keys
const (
	ctxPaginationLimit  ctxKey = "limit"
	ctxPaginationOffset ctxKey = "offset"
	ctxIncludeDeleted   ctxKey = "include_deleted"
)

// PaginationAndFilterMiddleware parses optional query parameters (limit, offset, include_deleted)
// and adds them to the context so downstream handlers can retrieve them in a uniform way.
func (app *Application) PaginationAndFilterMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()

		// Extract limit and offset from query params (default: 20/0)
		limit := parseIntOrDefault(r.URL.Query().Get("limit"), 20)
		if limit > 100 {
			limit = 100
		}
		offset := parseIntOrDefault(r.URL.Query().Get("offset"), 0)

		// Extract include_deleted as a boolean flag (true if value == "true")
		includeDeleted := strings.ToLower(r.URL.Query().Get("include_deleted")) == "true"

		// Inject values into context as strings (for consistency with existing helpers)
		ctx = context.WithValue(ctx, ctxPaginationLimit, strconv.Itoa(limit))
		ctx = context.WithValue(ctx, ctxPaginationOffset, strconv.Itoa(offset))
		ctx = context.WithValue(ctx, ctxIncludeDeleted, strconv.FormatBool(includeDeleted))

		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// RateLimitMiddleware throttles by *user* when authenticated, otherwise by IP.
//
// • Uses uuid.UUID from ctxUserID (set by AuthMiddleware) → String() for key.
// • 1 req/s burst 5 (NewLimiter).
func (app *Application) RateLimitMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id, ok := r.Context().Value(ctxUserID).(uuid.UUID)
		key := r.RemoteAddr // default = caller IP
		if ok && id != uuid.Nil {
			key = id.String() // user‑specific bucket
		}

		lim, _ := limiterStore.LoadOrStore(key, NewLimiter())
		if !lim.(*rate.Limiter).Allow() {
			if n := trackFailedAttempt(key); n >= 5 {
				http.Error(w, "rate‑limit exceeded", http.StatusTooManyRequests)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// AuthMiddleware validates Bearer‑JWT, injects uuid.UUID + role info.
func (app *Application) AuthMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		logger := app.Logger.GetLoggerWithContext(r).WithFunctionName("AuthMiddleware")

		// 1️⃣  Extract token
		const prefix = "Bearer "
		auth := r.Header.Get("Authorization")
		if !strings.HasPrefix(auth, prefix) {
			logger.Warn("missing bearer token", "remote_ip", r.RemoteAddr)
			http.Error(w, "missing bearer token", http.StatusUnauthorized)
			return
		}
		token := strings.TrimPrefix(auth, prefix)

		// 2️⃣  Validate & get user‑ID
		userID, err := app.Models.Token.ValidateAccessToken(r.Context(), token)
		if err != nil || userID == uuid.Nil {
			logger.Warn("invalid or expired token", "error", err, "remote_ip", r.RemoteAddr)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		// 3️⃣  Resolve primary role
		role, err := app.Models.Role.GetRoleByUserID(r.Context(), userID)
		if err != nil || role == nil || role.ID == uuid.Nil {
			logger.Warn("no role found for user", "error", err, "user_id", userID)
			http.Error(w, "role required", http.StatusUnauthorized)
			return
		}

		// 4️⃣  Enrich context
		ctx := context.WithValue(r.Context(), ctxUserID, userID)  // uuid.UUID
		ctx = context.WithValue(ctx, ctxRoleID, role.ID.String()) // string
		if role.Name != "admin" && role.Name != "internal_operator" {
			ctx = context.WithValue(ctx, ctxTargetUserID, userID)
		}

		logger.Info("authorized", "user_id", userID, "role", role.Name)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// RequirePermission allows the request to proceed when the authenticated
// actor's current role holds the required permission.
//
// AuthMiddleware establishes ctxUserID as uuid.UUID and ctxRoleID as the
// authenticated user's resolved role ID. Permission evaluation therefore uses
// that trusted context rather than reloading the user and role independently.
func (app *Application) RequirePermission(permission string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()

			userID, ok := ctx.Value(ctxUserID).(uuid.UUID)
			if !ok || userID == uuid.Nil {
				app.Logger.Warn(
					"RequirePermission: authenticated user ID missing from context",
					"permission", permission,
				)
				app.respondWithError(
					w,
					errors.New("unauthorized"),
					http.StatusUnauthorized,
				)
				return
			}

			if !app.HasPermission(ctx, permission) {
				app.Logger.Warn(
					"RequirePermission: access denied",
					"user_id", userID,
					"permission", permission,
				)
				app.respondWithError(
					w,
					errors.New("forbidden: insufficient permissions"),
					http.StatusForbidden,
				)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

func (app *Application) RequireMinimumRole(minRole string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			userID, ok := r.Context().Value(ctxUserID).(string)
			if !ok || userID == "" {
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
				return
			}

			id, err := uuid.Parse(userID)
			if err != nil {
				app.Logger.Warn("Invalid userID format", "userID", userID)
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
				return
			}

			user, err := app.Models.User.GetByID(r.Context(), id)
			if err != nil {
				http.Error(w, "Forbidden", http.StatusForbidden)
				return
			}

			role, err := app.Models.Role.GetRoleByID(r.Context(), user.RoleID)
			if err != nil {
				app.Logger.Warn("User role not found", "roleID", user.RoleID)
				http.Error(w, "Forbidden", http.StatusForbidden)
				return
			}

			requiredRole, err := app.Models.Role.GetRoleByName(r.Context(), minRole)
			if err != nil {
				app.Logger.Warn("Required role not found", "requiredRole", minRole)
				http.Error(w, "Internal Server Error", http.StatusInternalServerError)
				return
			}

			if role.HierarchyLevel < requiredRole.HierarchyLevel {
				app.Logger.Warn("Access denied", "userID", userID, "role", role.Name, "required", minRole)
				http.Error(w, "Forbidden", http.StatusForbidden)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// InjectApplicationContextMiddleware extracts trusted application-level identifiers
// (e.g., merchant_id, affiliate_program_id, client_id, brand_id, etc.)
// from known headers and injects them into the request context.
//
// Expected headers (case-insensitive):
//   - X-Merchant-Application-ID
//   - X-Merchant-ID
//   - X-Merchant-Type-ID
//   - X-Affiliate-Program-ID
//   - X-Audit-Log-ID
//   - X-Brand-ID
//   - X-Brand-Name
//   - X-Category-ID
//   - X-Client-ID
//   - X-Coupon-ID
//   - X-Offer-ID
//   - X-Entity-ID
//   - X-Entity-Type-ID
//   - X-Platform-ID
//   - X-Product-ID
//   - X-Product-Line

func (app *Application) InjectApplicationContextMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()

		// --- Helper: Inject UUID from headers (skip if already injected)
		injectUUID := func(header string, contextKey ctxKey) {
			val := r.Header.Get(header)
			if val == "" {
				return
			}
			if parsed, err := uuid.Parse(val); err == nil && ctx.Value(contextKey) == nil {
				ctx = context.WithValue(ctx, contextKey, parsed)
			} else if err != nil {
				app.Logger.Warn("Invalid UUID in header", "header", header, "value", val)
			}
		}

		// --- Inject all trusted UUID headers (same list as before)
		injectUUID("X-Merchant-Application-ID", ctxMerchantApplicationID)
		injectUUID("X-Admin-ID", ctxAdminID)
		injectUUID("X-Merchant-ID", ctxMerchantID)
		injectUUID("X-Merchant-Promotion-ID", ctxMerchantPromotionID)
		injectUUID("X-Merchant-Type-ID", ctxMerchantTypeID)
		injectUUID("X-Affiliate-Program-ID", ctxAffiliateProgramID)
		injectUUID("X-Audit-Log-ID", ctxAuditLogID)
		injectUUID("X-Brand-ID", ctxBrandID)
		injectUUID("X-Category-ID", ctxCategoryID)
		injectUUID("X-Client-ID", ctxClientID)
		injectUUID("X-Coupon-ID", ctxCouponID)
		injectUUID("X-Dashboard-Template-ID", ctxDashboardTemplateID)
		injectUUID("X-Offer-ID", ctxOfferID)
		injectUUID("X-Offer-Alert-ID", ctxOfferAlertID)
		injectUUID("X-Offer-Price-History-ID", ctxOfferPriceHistoryID)
		injectUUID("X-Offer-Rating-ID", ctxOfferRatingID)
		injectUUID("X-Offer-Sponsorship-ID", ctxOfferSponsorshipID)
		injectUUID("X-Offer-Share-ID", ctxOfferShareID)
		injectUUID("X-Offer-Status-ID", ctxOfferStatusID)
		injectUUID("X-Entity-ID", ctxEntityID)
		injectUUID("X-Entity-Type-ID", ctxEntityTypeID)
		injectUUID("X-Platform-ID", ctxPlatformID)
		injectUUID("X-Product-ID", ctxProductID)
		injectUUID("X-Promotion-ID", ctxPromotionID)
		injectUUID("X-Guest-User-ID", ctxGuestUserID)
		injectUUID("X-User-Dashboard-ID", ctxUserDashboardID)
		injectUUID("X-User-Offer-Purchase-History-ID", ctxUserOfferPurchaseHistoryID)
		injectUUID("X-User-Favorite-ID", ctxUserFavoriteID)
		injectUUID("X-User-Notification-ID", ctxUserNotificationID)
		injectUUID("X-User-Settings-ID", ctxUserSettingsID)
		injectUUID("X-Source-User-ID", ctxSourceUserID)

		// Inject user identity fields only if not set by JWT AuthMiddleware
		injectUUID("X-User-ID", ctxUserID)
		injectUUID("X-Target-User-ID", ctxTargetUserID)

		// --- Optional string headers
		if val := r.Header.Get("X-Brand-Name"); val != "" {
			ctx = context.WithValue(ctx, ctxBrandName, val)
		}
		if val := r.Header.Get("X-Product-Line"); val != "" {
			ctx = context.WithValue(ctx, ctxProductLine, val)
		}
		if val := r.Header.Get("X-Price-Drop-Threshold"); val != "" {
			ctx = context.WithValue(ctx, ctxPriceDropThreshold, val)
		}
		if val := r.Header.Get("X-Offer-IDs"); val != "" {
			ctx = context.WithValue(ctx, ctxOfferIDs, val)
		}
		if val := r.Header.Get("X-UPC"); val != "" {
			ctx = context.WithValue(ctx, ctxUPC, val)
		}

		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// TODO: Remove DevFallbackContextMiddleware before production.
//       This middleware injects dummy UUIDs for local development.
//       Trusted IDs should come from a secure upstream (e.g., API Gateway).

// DevFallbackContextMiddleware injects placeholder UUIDs during local development.
//
// **DO NOT** enable this in production.
// It exists solely to make local/Postman testing easier when an upstream
// gateway or another middleware would normally populate these context keys.
//
// Behaviour:
//   - Active only when GO_ENV is "dev" or "development".
//   - If a required ID is missing from context, a hard‑coded UUID is injected
//     and a warning is written to the structured logger.
func (app *Application) DevFallbackContextMiddleware(next http.Handler) http.Handler {
	// Helper for clean injection + logging.
	ensureUUID := func(ctx context.Context, key ctxKey, fallback string) context.Context {
		if ctx.Value(key) != nil {
			return ctx // already present – leave untouched
		}

		id, err := uuid.Parse(fallback)
		if err != nil {
			// This should never happen with static values, but fail gracefully.
			app.Logger.Errorf("DevFallbackContextMiddleware: invalid fallback UUID for %s: %v", key, err)
			return ctx
		}

		app.Logger.Warn("DevFallbackContextMiddleware: injected dev placeholder for %s", key)
		return context.WithValue(ctx, key, id)
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Only run in local‑dev environment.
		env := strings.ToLower(app.Config.Bootstrap.Env)
		if env != "dev" && env != "development" {
			next.ServeHTTP(w, r)
			return
		}

		ctx := r.Context()
		ctx = ensureUUID(ctx, ctxMerchantID, "00000000-0000-0000-0000-000000000001")
		ctx = ensureUUID(ctx, ctxAffiliateProgramID, "00000000-0000-0000-0000-000000000002")
		ctx = ensureUUID(ctx, ctxBrandID, "00000000-0000-0000-0000-000000000003")

		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// GuestSessionMiddleware injects a synthetic guest user ID into the context
// for unauthenticated requests using a session ID stored in a secure cookie.
func (app *Application) GuestSessionMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Attempt to read the guest_session_id cookie
		cookie, err := r.Cookie("guest_session_id")
		var sessionID string

		if err != nil || cookie.Value == "" {
			// Generate a new session ID
			newSessionID := uuid.New().String()
			http.SetCookie(w, &http.Cookie{
				Name:     "guest_session_id",
				Value:    newSessionID,
				Path:     "/",
				HttpOnly: true,
				Secure:   true,
				SameSite: http.SameSiteStrictMode,
				MaxAge:   60 * 60 * 24 * 30, // 30 days
			})
			sessionID = newSessionID
		} else {
			sessionID = cookie.Value
		}

		// Generate a deterministic UUID from the session ID
		guestUserID := uuid.NewSHA1(uuid.NameSpaceOID, []byte(sessionID))

		// Inject guest_user_id into context
		ctx := context.WithValue(r.Context(), ctxGuestUserID, guestUserID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// RequireInternalRole grants access only to *internal* staff
// (i.e. users whose primary role is **admin** or **internal_operator**).
//
// It is a convenience wrapper so routes can use:
//
//	.With(app.RequireInternalRole, app.RequirePermission("…"))
//
// instead of spelling out `app.RequireRole("admin","internal_operator")`
// everywhere.
//
// Implementation simply delegates to RequireRole, so all auditing,
// fallback look‑ups, and structured‑logging behaviour stay identical.
func (app *Application) RequireInternalRole(next http.Handler) http.Handler {
	// Re‑use the existing role‑based middleware generator.
	return app.RequireRole("admin", "internal_operator")(next)
}

// RequireRole ensures that only users with one of the allowed roles can access the endpoint.
// It supports fast lookup and structured logging, with a fallback to the database if needed.
func (app *Application) RequireRole(allowedRoles ...string) func(http.Handler) http.Handler {
	roleSet := make(map[string]struct{}, len(allowedRoles))
	for _, r := range allowedRoles {
		roleSet[r] = struct{}{}
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()
			userID, ok := ctx.Value(ctxUserID).(string)
			if !ok || userID == "" {
				app.Logger.Warn("RequireRole: missing user ID")
				app.respondWithError(w, errors.New("unauthorized"), http.StatusUnauthorized)
				return
			}

			role := getRoleFromContext(ctx)
			if role == "" {
				// fallback: load role from DB
				id, err := uuid.Parse(userID)
				if err != nil {
					app.Logger.Warn("RequireRole: invalid UUID", "userID", userID)
					app.respondWithError(w, errors.New("unauthorized"), http.StatusUnauthorized)
					return
				}
				user, err := app.Models.User.GetByID(ctx, id)
				if err != nil {
					app.Logger.Warn("RequireRole: failed to load user", "userID", userID, "error", err)
					app.respondWithError(w, errors.New("forbidden"), http.StatusForbidden)
					return
				}
				roleModel, err := app.Models.Role.GetRoleByID(ctx, user.RoleID)
				if err != nil {
					app.Logger.Warn("RequireRole: failed to load role", "roleID", user.RoleID)
					app.respondWithError(w, errors.New("forbidden"), http.StatusForbidden)
					return
				}
				role = roleModel.Name
			}

			if _, ok := roleSet[role]; !ok {
				app.Logger.Warn("RequireRole: access denied", "userID", userID, "role", role, "allowedRoles", allowedRoles)
				app.respondWithError(w, errors.New("forbidden: insufficient role"), http.StatusForbidden)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// RequirePermissionOrRole grants access if the user has the required permission or is in one of the allowed roles.
// It includes fallback role resolution and structured logging for security visibility.
func (app *Application) RequirePermissionOrRole(permission string, allowedRoles ...string) func(http.Handler) http.Handler {
	roleSet := make(map[string]struct{}, len(allowedRoles))
	for _, r := range allowedRoles {
		roleSet[r] = struct{}{}
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()
			userID, ok := ctx.Value(ctxUserID).(string)
			if !ok || userID == "" {
				app.Logger.Warn("RequirePermissionOrRole: missing user ID")
				app.respondWithError(w, errors.New("unauthorized"), http.StatusUnauthorized)
				return
			}

			role := getRoleFromContext(ctx)
			if role == "" {
				// fallback: load role from DB
				id, err := uuid.Parse(userID)
				if err != nil {
					app.Logger.Warn("RequirePermissionOrRole: invalid UUID", "userID", userID)
					app.respondWithError(w, errors.New("unauthorized"), http.StatusUnauthorized)
					return
				}
				user, err := app.Models.User.GetByID(ctx, id)
				if err != nil {
					app.Logger.Warn("RequirePermissionOrRole: failed to load user", "userID", userID)
					app.respondWithError(w, errors.New("forbidden"), http.StatusForbidden)
					return
				}
				roleModel, err := app.Models.Role.GetRoleByID(ctx, user.RoleID)
				if err != nil {
					app.Logger.Warn("RequirePermissionOrRole: failed to load role", "roleID", user.RoleID)
					app.respondWithError(w, errors.New("forbidden"), http.StatusForbidden)
					return
				}
				role = roleModel.Name
			}

			if _, ok := roleSet[role]; ok {
				next.ServeHTTP(w, r)
				return
			}

			// Fallback: check for permission
			if app.HasPermission(ctx, permission) {
				next.ServeHTTP(w, r)
				return
			}

			app.Logger.Warn("RequirePermissionOrRole: access denied", "userID", userID, "role", role, "required_permission", permission, "allowed_roles", allowedRoles)
			app.respondWithError(w, errors.New("forbidden: requires permission or role"), http.StatusForbidden)
		})
	}
}

// RequireAuthenticatedUser ensures that the request has a valid user ID in context.
// This middleware assumes AuthMiddleware has already validated the token and injected ctxUserID.
// It is used to block unauthenticated users from accessing user-specific endpoints.
func (app *Application) RequireAuthenticatedUser(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()

		userID, ok := ctx.Value(ctxUserID).(string)
		if !ok || userID == "" {
			app.Logger.Warn("RequireAuthenticatedUser: missing or invalid user ID")
			app.respondWithError(w, errors.New("unauthorized: login required"), http.StatusUnauthorized)
			return
		}

		// You could add optional UUID validation here if paranoia is desired
		if _, err := uuid.Parse(userID); err != nil {
			app.Logger.Warn("RequireAuthenticatedUser: invalid UUID format", "userID", userID)
			app.respondWithError(w, errors.New("unauthorized: invalid user ID"), http.StatusUnauthorized)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// RequireSelfOrPrivileged blocks the request unless the caller is either:
//   - the owner of the resource (requesterID == targetID), OR
//   - an internal user (admin | internal_operator) **and** already holds the
//     supplied permission string.
//
// Usage:
//
//	router.With(app.RequireSelfOrPrivileged("update_users")).Put("/users/{id}", h)
func (app *Application) RequireSelfOrPrivileged(permission string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			logger := app.Logger.GetLoggerWithContext(r).WithFunctionName("RequireSelfOrPrivileged")
			ctx := r.Context()

			requesterID := app.getUserIDFromContext(ctx)    // injected by AuthMiddleware
			targetID := app.getTargetUserIDFromContext(ctx) // set earlier in the chain (e.g. param middleware)

			if requesterID == nil || targetID == nil {
				logger.Warn("missing requester/target IDs in context")
				app.respondWithError(w, errors.New("unauthorized or malformed request"), http.StatusUnauthorized)
				return
			}

			// Short‑circuit: user operating on their own record.
			if *requesterID == *targetID {
				next.ServeHTTP(w, r)
				return
			}

			// Internal role check.
			isAdmin, _ := app.HasRole(ctx, "admin")
			isOperator, _ := app.HasRole(ctx, "internal_operator")
			if !(isAdmin || isOperator) {
				logger.Warn("caller not privileged", "requester_id", *requesterID)
				app.respondWithError(w, errors.New("forbidden: cannot act on other users"), http.StatusForbidden)
				return
			}

			// Fine‑grained permission check for privileged calls.
			if permission != "" && !app.HasPermission(ctx, permission) {
				logger.Warn("missing permission", "permission", permission, "requester_id", *requesterID)
				app.respondWithError(w, errors.New("forbidden: insufficient permissions"), http.StatusForbidden)
				return
			}

			logger.Debug("access granted", "requester_id", *requesterID, "target_id", *targetID, "permission", permission)
			next.ServeHTTP(w, r)
		})
	}
}

// InjectTargetUserID pulls {userID} from the URL (or X‑Target-User-ID header)
// validates it as a UUID, then stores it under ctxTargetUserID so downstream
// handlers can treat it as a trusted identifier.
func (app *Application) InjectTargetUserID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		logger := app.Logger.GetLoggerWithContext(r).WithFunctionName("InjectTargetUserID")

		// Prefer Chi path param `{userID}`, fall back to header if present.
		rawID := chi.URLParam(r, "userID")
		if rawID == "" {
			rawID = r.Header.Get("X-Target-User-ID")
		}

		id, err := uuid.Parse(rawID)
		if err != nil || id == uuid.Nil {
			logger.Warn("invalid or missing userID", "value", rawID)
			app.respondWithError(w, errors.New("invalid or missing userID"), http.StatusBadRequest)
			return
		}

		ctx := context.WithValue(r.Context(), ctxTargetUserID, id)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
