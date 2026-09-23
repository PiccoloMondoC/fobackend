// Package main provides HTTP middleware for authentication, authorization,
// trusted request context, rate limiting, and other cross-cutting API controls.
//
// focodebase/fobackend/internal/server/cmd/api/middleware.go
//
// GTM:
//
//	Layer: 2.5 API / Authorization Infrastructure
//	Release Class: SPINE
//	Reason:
//	  Middleware establishes Sagrenti's HTTP authentication, trusted request
//	  context, authorization boundaries, request-scoped permission resolution,
//	  rate limiting, and other cross-cutting API controls. Authentication and
//	  authorization middleware are release-critical infrastructure and must
//	  remain production-ready for every protected API surface.
//
// SPINE Rule:
//
//	Keep compiling.
//	Keep production-ready.
//	Preserve fail-closed authentication and authorization behavior.
//	Preserve canonical authenticated identity as uuid.UUID.
//	Preserve distinct role-ID and role-name context semantics.
//	Preserve request-scoped authorization resolution; never introduce global
//	permission caching or cross-request authorization state.
//	Preserve defense-in-depth between route and handler authorization checks.
//	Preserve permission-first capability enforcement.
//	Do not introduce role-based permission bypasses without explicit CE review.
//	Do not trust client-supplied authenticated identity or authorization state.
//	Block deployment if this file breaks authentication, authorization,
//	trusted-context integrity, permission freshness, or protected route access.
package main

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"sync"

	"github.com/PiccoloMondoC/focodebase/fobackend/internal/data"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"golang.org/x/time/rate"
)

// TODO: As the API grows, consider combining related routes or simplifying permission checks for maintainability.

type ctxKey string

const ctxActionID ctxKey = "actionID"
const ctxAdminID ctxKey = "adminID"

// const ctxArchivedTimeRange ctxKey = "archivedTimeRange"
const ctxAuditLogID ctxKey = "auditLogID"
const ctxCategoryID ctxKey = "categoryID"
const ctxClientID ctxKey = "clientID"
const ctxEntityID ctxKey = "entityID"
const ctxEntityTypeID ctxKey = "entityTypeID"
const ctxMerchantID ctxKey = "merchantID"

// ctxRoleID holds the authenticated user's resolved role ID (string form of
// a uuid.UUID). It is the canonical identity of the role row, not the role's
// display name. Use ctxRoleName (and getRoleFromContext) when the role name
// is what's needed.
const ctxRoleID ctxKey = "roleID"

// ctxRoleName holds the authenticated user's resolved role name (e.g.
// "admin", "merchant", "internal_operator"). This is distinct from
// ctxRoleID: role identity and role name are different concepts and must not
// be conflated. AuthMiddleware is the sole writer of this key.
const ctxRoleName ctxKey = "roleName"

// ctxAuthzState holds a *authzState value: the request-scoped authorization
// resolution cache. AuthMiddleware is the sole writer of this key. It exists
// so that repeated HasPermission checks for the same permission during one
// request reuse the first authoritative persistence result instead of
// re-querying the database. See authzState in helpers.go.
const ctxAuthzState ctxKey = "authzState"

const ctxStatusID ctxKey = "statusID"
const ctxUPC ctxKey = "upc"
const ctxGuestUserID ctxKey = "guestUserID"
const ctxUserOfferPurchaseHistoryID ctxKey = "userOfferPurchaseHistoryID"

// ctxUserID holds the authenticated user's identity as a uuid.UUID. This is
// the sole canonical type for this key. AuthMiddleware is the sole trusted
// writer for authenticated requests; every consumer must assert uuid.UUID
// (use app.getUserIDFromContext), never string.
const ctxUserID ctxKey = "userID"

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
// • 1 req/s burst 5 (NewLimiter).
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

// AuthMiddleware authenticates the Bearer access token through the canonical
// TokenService boundary and establishes the trusted authorization context
// consumed by every downstream authorization helper
// in this package. It is the sole writer of ctxUserID, ctxRoleID,
// ctxRoleName, and ctxAuthzState.
//
// Authorization Resolution Invariant: permission state is resolved from
// authoritative persistence no more than once per distinct permission per
// request. AuthMiddleware establishes the request-scoped cache (authzState)
// that makes this possible; it does not itself resolve any permission.
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

		// 2️⃣  Authenticate through the canonical TokenService boundary.
		// Cryptographic/claims validation and persistence-backed revocation are
		// enforced there; middleware receives only trusted user identity.
		userID, err := app.TokenService.AuthenticateAccessToken(r.Context(), token)
		if err != nil || userID == uuid.Nil {
			logger.Warn("invalid, expired, or revoked token", "error", err, "remote_ip", r.RemoteAddr)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		// 3️⃣  Resolve the authenticated user's current authorization class.
		// A missing role is an authorization outcome; persistence failures and
		// impossible role records are server failures and must not be disguised as
		// authentication failures.
		role, err := app.Models.Role.GetRoleByUserID(r.Context(), userID)
		if err != nil {
			if errors.Is(err, data.ErrRoleNotFound) {
				logger.Warn("authenticated user has no active role assignment", "user_id", userID)
				http.Error(w, "role required", http.StatusForbidden)
				return
			}

			logger.Error("authenticated user role resolution failed", "error", err, "user_id", userID)
			app.serverErrorResponse(logger, w, r, err)
			return
		}

		if role == nil || role.ID == uuid.Nil || strings.TrimSpace(role.Name) == "" {
			err := errors.New("authenticated user role resolution returned an invalid role")
			logger.Error("authenticated user role resolution returned invalid role", "error", err, "user_id", userID)
			app.serverErrorResponse(logger, w, r, err)
			return
		}

		// 4️⃣  Enrich context. ctxRoleID carries role identity; ctxRoleName
		// carries the role's display name. These are distinct concepts and
		// must not be conflated by downstream consumers.
		ctx := context.WithValue(r.Context(), ctxUserID, userID)  // uuid.UUID
		ctx = context.WithValue(ctx, ctxRoleID, role.ID.String()) // string (role identity)
		ctx = context.WithValue(ctx, ctxRoleName, role.Name)      // string (role name)

		// 5️⃣  Establish the request-scoped authorization resolution cache.
		// This is the single source HasPermission consults; it is never
		// shared across requests and carries no invalidation logic.
		ctx = context.WithValue(ctx, ctxAuthzState, newAuthzState(role.ID))

		// Only non-internal actors are treated as their own target by
		// default. isInternalRole is the single canonical definition of
		// "internal actor" — used here, in RequireInternalPermission, and
		// in IsInternalUser, so this classification cannot drift out of
		// sync with permission enforcement elsewhere.
		if !isInternalRole(role.Name) {
			ctx = context.WithValue(ctx, ctxTargetUserID, userID)
		}

		logger.Info("authorized", "user_id", userID, "role", role.Name)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// RequirePermission allows the request to proceed when the authenticated
// actor's current role holds the required permission.
//
// AuthMiddleware establishes ctxUserID (uuid.UUID), ctxRoleID, ctxRoleName,
// and ctxAuthzState. RequirePermission and the underlying HasPermission
// primitive rely entirely on that trusted context; neither independently
// reloads the user or role from persistence.
func (app *Application) RequirePermission(permission string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()

			userID := app.getUserIDFromContext(ctx)
			if userID == nil || *userID == uuid.Nil {
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
					"user_id", *userID,
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

// RequireMinimumRole grants access only when the authenticated actor's role
// hierarchy level meets or exceeds minRole's hierarchy level.
//
// Requires AuthMiddleware upstream: it reads the trusted role ID already
// resolved into context rather than reloading the user and role
// independently from persistence.
func (app *Application) RequireMinimumRole(minRole string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()

			userID := app.getUserIDFromContext(ctx)
			if userID == nil || *userID == uuid.Nil {
				app.Logger.Warn("RequireMinimumRole: missing user ID in context", "min_role", minRole)
				app.respondWithError(w, errors.New("unauthorized"), http.StatusUnauthorized)
				return
			}

			roleID := app.getRoleIDFromContext(ctx)
			if roleID == nil {
				app.Logger.Warn("RequireMinimumRole: missing role ID in context", "user_id", *userID, "min_role", minRole)
				app.respondWithError(w, errors.New("forbidden"), http.StatusForbidden)
				return
			}

			role, err := app.Models.Role.GetRoleByID(ctx, *roleID)
			if err != nil {
				app.Logger.Warn("RequireMinimumRole: role not found", "role_id", *roleID, "error", err)
				app.respondWithError(w, errors.New("forbidden"), http.StatusForbidden)
				return
			}

			requiredRole, err := app.Models.Role.GetRoleByName(ctx, minRole)
			if err != nil {
				app.Logger.Warn("RequireMinimumRole: required role not found", "min_role", minRole, "error", err)
				app.respondWithError(w, errors.New("internal server error"), http.StatusInternalServerError)
				return
			}

			if role.HierarchyLevel < requiredRole.HierarchyLevel {
				app.Logger.Warn("RequireMinimumRole: access denied", "user_id", *userID, "role", role.Name, "min_role", minRole)
				app.respondWithError(w, errors.New("forbidden"), http.StatusForbidden)
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
//   - X-Audit-Log-ID
//   - X-Category-ID
//   - X-Client-ID
//   - X-Entity-ID
//   - X-Entity-Type-ID
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
		injectUUID("X-Admin-ID", ctxAdminID)
		injectUUID("X-Merchant-ID", ctxMerchantID)
		injectUUID("X-Audit-Log-ID", ctxAuditLogID)
		injectUUID("X-Category-ID", ctxCategoryID)
		injectUUID("X-Client-ID", ctxClientID)
		injectUUID("X-Entity-ID", ctxEntityID)
		injectUUID("X-Entity-Type-ID", ctxEntityTypeID)
		injectUUID("X-Guest-User-ID", ctxGuestUserID)
		injectUUID("X-User-Offer-Purchase-History-ID", ctxUserOfferPurchaseHistoryID)
		injectUUID("X-User-Notification-ID", ctxUserNotificationID)
		injectUUID("X-User-Settings-ID", ctxUserSettingsID)
		injectUUID("X-Source-User-ID", ctxSourceUserID)

		// X-User-ID is intentionally not accepted here. ctxUserID is authenticated
		// identity and may only be established by AuthMiddleware. X-Target-User-ID
		// is a resource selector, not caller identity; retain it for existing routes.
		injectUUID("X-Target-User-ID", ctxTargetUserID)

		// --- Optional string headers
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

// RequireInternalRole grants access only to an authenticated internal actor.
// Internal-role classification is owned exclusively by isInternalRole so that
// admin, super_admin, and internal_operator semantics cannot drift between
// authorization entry points.
func (app *Application) RequireInternalRole(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		userID := app.getUserIDFromContext(ctx)
		if userID == nil || *userID == uuid.Nil {
			app.Logger.Warn("RequireInternalRole: missing user ID in context")
			app.respondWithError(w, errors.New("unauthorized"), http.StatusUnauthorized)
			return
		}

		role := getRoleFromContext(ctx)
		if !isInternalRole(role) {
			app.Logger.Warn("RequireInternalRole: access denied", "user_id", *userID, "role", role)
			app.respondWithError(w, errors.New("forbidden: internal role required"), http.StatusForbidden)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// RequireRole ensures that only users whose trusted role name is one of the
// allowed roles can access the endpoint.
//
// Requires AuthMiddleware upstream: it reads ctxRoleName from trusted
// context and fails closed if it is absent. It no longer falls back to
// persistence — a missing role name indicates the request did not pass
// through AuthMiddleware, and that must not be silently repaired by a
// hidden database lookup.
func (app *Application) RequireRole(allowedRoles ...string) func(http.Handler) http.Handler {
	roleSet := make(map[string]struct{}, len(allowedRoles))
	for _, r := range allowedRoles {
		roleSet[strings.ToLower(strings.TrimSpace(r))] = struct{}{}
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()

			userID := app.getUserIDFromContext(ctx)
			if userID == nil || *userID == uuid.Nil {
				app.Logger.Warn("RequireRole: missing user ID in context")
				app.respondWithError(w, errors.New("unauthorized"), http.StatusUnauthorized)
				return
			}

			role := getRoleFromContext(ctx)
			if role == "" {
				app.Logger.Warn("RequireRole: missing role name in trusted context", "user_id", *userID)
				app.respondWithError(w, errors.New("forbidden"), http.StatusForbidden)
				return
			}

			if _, ok := roleSet[strings.ToLower(strings.TrimSpace(role))]; !ok {
				app.Logger.Warn("RequireRole: access denied", "user_id", *userID, "role", role, "allowed_roles", allowedRoles)
				app.respondWithError(w, errors.New("forbidden: insufficient role"), http.StatusForbidden)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// RequireAuthenticatedUser ensures that the request has a valid user ID in context.
// This middleware assumes AuthMiddleware has already validated the token and injected ctxUserID.
// It is used to block unauthenticated users from accessing user-specific endpoints.
func (app *Application) RequireAuthenticatedUser(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()

		userID := app.getUserIDFromContext(ctx)
		if userID == nil || *userID == uuid.Nil {
			app.Logger.Warn("RequireAuthenticatedUser: missing or invalid user ID")
			app.respondWithError(w, errors.New("unauthorized: login required"), http.StatusUnauthorized)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// RequireSelfOrPrivileged blocks the request unless the caller is either:
//   - the owner of the resource (requesterID == targetID), OR
//   - an internal user (admin | super_admin | internal_operator) and,
//     when permission is non-empty, already holds the supplied permission.
//
// The "internal actor" classification is not reproduced here: it is owned
// entirely by isInternalRole (used directly for the permission == "" case)
// and by RequireInternalPermission (internal AND permission), so this
// function maintains exactly one implementation of "internal actor," not
// two. RequireSelfOrPrivileged itself is responsible only for requester/
// target validation, HTTP denial behavior, logging, and calling next.
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

			var authorized bool
			if permission == "" {
				authorized = isInternalRole(getRoleFromContext(ctx))
			} else {
				authorized = app.RequireInternalPermission(ctx, permission)
			}

			if !authorized {
				logger.Warn("access denied: not self and not privileged",
					"requester_id", *requesterID,
					"target_id", *targetID,
					"permission", permission,
				)
				app.respondWithError(w, errors.New("forbidden: cannot act on other users"), http.StatusForbidden)
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
