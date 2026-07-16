// Package main provides HTTP routing for the SagrentiDeals API.
//
// sdworkspace/sdbackend/internal/server/cmd/api/routes.go
//
// GTM:
//
//	Layer: 2.1 API / Routing Foundation
//	Release Class: SPINE
//	Reason:
//	  Canonical HTTP route registration is release-critical platform
//	  infrastructure. This file defines the public, authenticated, privileged,
//	  administrative, health, observability, and development-only entry points
//	  for the SagrentiDeals API. It establishes the middleware boundaries,
//	  authentication requirements, permission enforcement, public-route
//	  registry, route ordering, and versioned API surface required by every
//	  release-critical domain.
//
// SPINE Rule:
//
//	Keep compiling.
//	Keep production-ready.
//	Preserve canonical route ownership.
//	Preserve public versus authenticated route separation.
//	Preserve authentication and permission enforcement.
//	Preserve static routes before conflicting parameterized routes.
//	Preserve development-only middleware and diagnostics boundaries.
//	Preserve public-route registry synchronization.
//	Preserve health, readiness, and metrics endpoints.
//	Preserve JSON not-found behavior.
//	Do not register DEFERRED domains.
//	Do not retain duplicate, conflicting, stale, or commented-out routes.
//	Do not introduce generic route namespaces without a canonical domain owner.
//	Do not weaken middleware, actor, role, or permission boundaries.
//	Do not block release-critical SPINE domains through unrelated deferred work.
package main

import (
	"net/http"
	"strings"
	"github.com/go-chi/cors"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

func (app *Application) Routes() http.Handler {
	r := chi.NewRouter()

	// Global middleware (applies to all routes)
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)

	var allowedOrigins []string
	if strings.ToLower(app.Config.Bootstrap.Env) == "development" {
		allowedOrigins = []string{"http://localhost:4300"}
	} else {
		allowedOrigins = []string{"https://your-production-domain.com"}
	}

	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   allowedOrigins,
		AllowedMethods:   []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", "X-CSRF-Token"},
		ExposedHeaders:   []string{"Link"},
		AllowCredentials: true,
		MaxAge:           300,
	}))

	// ---- Public routes (no auth) ----
	r.Get("/", app.RootHandler)
	app.registerPublic("GET", "/")

	r.Get("/healthz", app.LivenessHandler)
	app.registerPublic("GET", "/healthz")

	r.Get("/readyz", app.ReadinessHandler)
	app.registerPublic("GET", "/readyz")

	// Prometheus (public by design; protect via network policy if needed)
	r.Handle("/metrics", promhttp.Handler())
	app.registerPublic("GET", "/metrics")

	// Development-only (don't advertise)
	if strings.ToLower(app.Config.Bootstrap.Env) == "development" {
		r.Get("/debug/context", app.DebugContextHandler)
	}

	// API v1 routes
	r.Route("/api/v1", func(v1 chi.Router) {
		// Inject trusted IDs into context for all v1 endpoints
		v1.Use(app.InjectApplicationContextMiddleware)

		// Development-only fallback context.
		// Never install synthetic trusted identifiers in production.
		if strings.EqualFold(app.Config.Bootstrap.Env, "development") {
			v1.Use(app.DevFallbackContextMiddleware)
		}

		// ---- PUBLIC API ROUTES (no auth required) ----
		
		// Public offers routes — registered first for public accessibility.
		v1.Group(func(publicOffers chi.Router) {
			// GET /api/v1/offers — public live offers
			publicOffers.Get("/offers", app.GetLiveOffersHandler)
			app.registerPublic("GET", "/api/v1/offers")

			// GET /api/v1/offers/key/{offer_key} — public offer by readable key
			//
			// Keep this static-prefix route before /offers/{id} so a readable key is
			// never interpreted as a canonical UUID path parameter.
			publicOffers.Get(
				"/offers/key/{offer_key}",
				app.GetOfferByKeyHandler,
			)
			app.registerPublic(
				"GET",
				"/api/v1/offers/key/{offer_key}",
			)

			// GET /api/v1/offers/{id} — public offer by canonical ID
			publicOffers.Get("/offers/{id}", app.GetOfferByIDHandler)
			app.registerPublic("GET", "/api/v1/offers/{id}")
		})

		// ---- PUBLIC IDENTITY AND AUTHENTICATION ROUTES ----

		// User Activation Routes
		v1.With(app.RateLimitMiddleware).Post("/user/activation", app.CreateActivationTokenHandler)
		v1.Post("/user/activate", app.ActivateUserHandler)
		v1.Get("/user/activation/status", app.GetUserActivationStatusHandler)

		// Authentication Routes
		v1.Post("/user/register", app.RegisterUserHandler)
		v1.Post("/user/login", app.LoginHandler)
		v1.Post("/user/token/refresh", app.RefreshTokenHandler)

		// Logout revokes an authenticated session.
		v1.With(app.AuthMiddleware).
			Post("/user/logout", app.LogoutHandler)

		// ---- AUTHENTICATED AND PRIVILEGED API ROUTES ----

		// Role & Permission Routes
		v1.Route("/roles", func(rr chi.Router) {
			rr.Use(app.AuthMiddleware)
			rr.With(app.RequirePermission("role:list")).Get("/", app.ListRolesHandler)
			rr.With(app.RequirePermission("role:read")).Get("/{roleID}", app.GetRoleByIDHandler)
			rr.With(app.RequirePermission("role:create")).Post("/", app.CreateRoleHandler)
			rr.With(app.RequirePermission("role:update")).Patch("/{roleID}", app.UpdateRoleHandler)
			rr.With(app.RequirePermission("role:delete")).Delete("/{roleID}", app.DeleteRoleHandler)
			rr.With(app.RequirePermission("role:assign")).Post("/assign", app.AssignRoleToUserHandler)
			rr.With(app.RequirePermission("role:revoke")).Post("/revoke", app.RevokeRoleFromUserHandler)
			rr.With(app.RequirePermission("role:view_roles")).Get("/user/{userID}", app.GetRolesForUserHandler)
		//	rr.With(app.RequirePermission("role:permissions:list")).Get("/{roleID}/permissions", app.GetRolePermissionsHandler)
		//	rr.With(app.RequirePermission("role:permissions:update")).Patch("/{roleID}/permissions", app.UpdateRolePermissionsHandler)
		})

		// Permissions
		v1.Route("/permissions", func(pr chi.Router) {
			pr.Use(app.AuthMiddleware)
			pr.With(app.RequirePermission("permission:list")).Get("/", app.ListAllPermissionsHandler)
			pr.With(app.RequirePermission("permission:check")).Get("/check/{userID}/{permission}", app.CheckUserPermissionHandler)
		})

		
		// Audit Logs
		v1.Route("/audit-logs", func(al chi.Router) {
			al.Use(app.AuthMiddleware)

			// POST: Retrieve audit log by ID (context-based only)
			al.With(app.RequirePermissionOrRole("read_audit_log", "admin", "internal_moderator")).
				Post("/", app.GetAuditLogByIDHandler)

			// POST: Retrieve audit logs for authenticated user
			al.With(app.RequirePermissionOrRole("read_audit_log", "admin", "internal_moderator")).
				Post("/by-user", app.GetAuditLogByUserIDHandler)

			// POST: Retrieve audit logs by entity ID and entity type ID (context-based only)
			al.With(app.RequirePermissionOrRole("read_audit_log", "admin", "internal_moderator")).
				Post("/by-entity", app.GetAuditLogByEntityIDHandler)

			// POST: Retrieve audit logs by time range (start, end in RFC3339 format)
			al.With(app.RequirePermissionOrRole("read_audit_logs_by_time_range", "admin", "internal_moderator")).
				Post("/by-time-range", app.GetAuditLogsByTimeRangeHandler)

			// POST: Restore archived audit logs (admin/internal_moderator only)
			al.With(app.RequirePermissionOrRole("restore_archived_audit_logs", "admin", "internal_moderator")).
				Post("/archived/restore", app.RestoreArchivedLogsHandler)

			// POST: Retrieve archived audit logs by entity ID and entity type ID (context-based only)
			al.With(app.RequirePermissionOrRole("read_archived_audit_log", "admin", "internal_moderator")).
				Post("/archived/by-entity", app.GetArchivedAuditLogByEntityHandler)

			// POST: Retrieve archived audit logs by time range (admin/internal_moderator only)
			al.With(app.RequirePermissionOrRole("read_archived_audit_logs", "admin", "internal_moderator")).
				Post("/archived/by-time-range", app.GetArchivedAuditLogsByTimeRangeHandler)
		})

		// Entity Types
		v1.Route("/entity-types", func(et chi.Router) {
			et.Use(app.AuthMiddleware)

			// GET: Retrieve an entity type by ID (context-based only)
			et.With(app.RequirePermission("read_entity_type")).
				Get("/", app.GetEntityTypeByIDHandler)

			// GET: Retrieve all entity types (audit + permission protected)
			et.With(app.RequirePermission("read_entity_type")).
				Get("/all", app.GetAllEntityTypesHandler)
		})

		// Actions
		v1.Route("/actions", func(act chi.Router) {
			act.Use(app.AuthMiddleware)

			// GET: Retrieve an action by ID (context-based only)
			act.With(app.RequirePermission("read_action")).
				Get("/", app.GetActionByIDHandler)

			// GET: Retrieve all actions (with audit logging)
			act.With(app.RequirePermission("read_action")).
				Get("/all", app.GetAllActionsHandler)
		})


		// Platform Settings
		v1.Route("/platform-settings", func(ps chi.Router) {
			ps.Use(app.AuthMiddleware)

			ps.With(app.RequirePermission("create_platform_setting")).
				Post("/", app.CreatePlatformSettingHandler)

			ps.With(app.RequirePermission("ensure_platform_setting")).
				Post("/ensure", app.EnsurePlatformSettingHandler)

			ps.With(app.RequirePermission("read_platform_setting")).
				Post("/by-key", app.GetPlatformSettingByKeyHandler)

			ps.With(app.RequirePermission("read_platform_setting")).
				Post("/active/by-key", app.GetActivePlatformSettingByKeyHandler)

			ps.With(app.RequirePermission("read_platform_setting")).
				Post("/admin/by-key", app.GetPlatformSettingByKeyIncludingDeletedHandler)

			ps.With(app.RequirePermission("read_platform_setting")).
				Post("/exists/active/by-key", app.ExistsActivePlatformSettingByKeyHandler)

			ps.With(app.RequirePermission("list_platform_settings")).
				Get("/active", app.ListActivePlatformSettingsHandler)

			ps.With(app.RequirePermission("list_platform_settings")).
				Get("/all", app.ListPlatformSettingsHandler)

			ps.With(app.RequirePermission("list_platform_settings")).
				Get("/admin/all", app.ListPlatformSettingsIncludingDeletedHandler)

			ps.With(app.RequirePermission("read_platform_setting")).
				Get("/{platformSettingID}", app.GetPlatformSettingByIDHandler)

			ps.With(app.RequirePermission("read_platform_setting")).
				Get("/admin/{platformSettingID}", app.GetPlatformSettingByIDIncludingDeletedHandler)

			ps.With(app.RequirePermission("update_platform_setting")).
				Patch("/{platformSettingID}/value", app.UpdatePlatformSettingValueHandler)

			// Permission is resolved inside the handler from request body:
			// activate_platform_setting or deactivate_platform_setting.
			ps.Patch("/{platformSettingID}/active", app.SetPlatformSettingActiveHandler)

			ps.With(app.RequirePermission("soft_delete_platform_setting")).
				Delete("/{platformSettingID}/soft-delete", app.SoftDeletePlatformSettingHandler)

			ps.With(app.RequirePermission("hard_delete_platform_setting")).
				Delete("/{platformSettingID}", app.HardDeletePlatformSettingHandler)
		})


		// Merchants
		v1.Route("/merchants", func(ar chi.Router) {
			ar.Use(app.AuthMiddleware)

			// POST: Create a new merchant (context-free, JSON input)
			ar.With(app.RequirePermission("create_merchant")).
				Post("/", app.CreateMerchantHandler)
			
			// POST: Retrieve merchant by name (JSON input)
			ar.With(app.RequirePermission("read_merchant")).
				Post("/by-name", app.GetMerchantByNameHandler)

			// PATCH: Partially update an merchant (context-based only)
			ar.With(app.RequirePermission("update_merchant")).
				Patch("/", app.UpdateMerchantHandler)

			// DELETE: Soft-delete an merchant (context-based only)
			ar.With(app.RequirePermission("soft_delete_merchant")).
				Delete("/soft-delete", app.SoftDeleteMerchantHandler)

			// GET: Retrieve a single merchant by ID (from context only)
			ar.With(app.RequirePermission("read_merchant")).
				Get("/", app.GetMerchantByIDHandler)

			// GET: Retrieve merchants by brand ID (context-based only)
			ar.With(app.RequirePermission("read_merchant_by_brand")).
				Get("/by-brand", app.GetMerchantByBrandIDHandler)

			// GET: Retrieve merchant by offer ID (context-based only)
			ar.With(app.RequirePermission("read_merchant")).
				Get("/by-offer", app.GetMerchantByOfferIDHandler)

			// GET: Retrieve merchants by platform ID (context-based only)
			ar.With(app.RequirePermission("read_merchant")).
				Get("/by-platform", app.GetMerchantByPlatformIDHandler)

			// GET: Retrieve merchants by product ID (context-based only)
			ar.With(app.PaginationAndFilterMiddleware).
				With(app.RequirePermission("read_merchant_by_product")).
				Get("/by-product", app.GetMerchantByProductIDHandler)

			// GET: Retrieve merchants by product line (context-based only)
			ar.With(app.PaginationAndFilterMiddleware).
				With(app.RequirePermission("read_merchant_by_product_line")).
				Get("/by-product-line", app.GetMerchantByProductLineHandler)

			// GET: Retrieve merchant by website (context-based only)
			ar.With(app.RequirePermission("read_merchant")).
				Get("/by-website", app.GetMerchantByWebsiteHandler)

			// GET: List all merchants (paginated, optional filters from context)
			ar.With(app.PaginationAndFilterMiddleware).
				With(app.RequirePermission("list_merchants")).
				Get("/all", app.GetAllMerchantsHandler)
				
			// GET: Count all non-deleted merchants
			ar.With(app.RequirePermission("list_merchants")).
				Get("/count", app.CountMerchantsHandler)
		})


		// Merchant Program Entitlements
		v1.Route("/merchant-program-entitlements", func(mpe chi.Router) {
			mpe.Use(app.AuthMiddleware)

			mpe.With(app.RequirePermission("create_merchant_program_entitlement")).
				Post("/", app.CreateMerchantProgramEntitlementHandler)

			mpe.With(app.RequirePermission("ensure_merchant_program_entitlement")).
				Post("/ensure", app.EnsureMerchantProgramEntitlementHandler)

			mpe.With(app.RequirePermission("read_merchant_program_entitlement")).
				Post("/by-plan-and-code", app.GetMerchantProgramEntitlementByPlanAndCodeHandler)

			mpe.With(app.RequirePermission("list_merchant_program_entitlements")).
				Get("/plan/{merchantProgramPlanID}", app.ListMerchantProgramEntitlementsByPlanIDHandler)

			mpe.With(app.RequirePermission("read_merchant_program_entitlement")).
				Post("/plan-has-entitlement", app.PlanHasMerchantProgramEntitlementHandler)

			mpe.With(app.RequirePermission("delete_merchant_program_entitlement")).
				Post("/delete-by-plan-and-code", app.DeleteMerchantProgramEntitlementByPlanAndCodeHandler)

			mpe.With(app.RequirePermission("read_merchant_program_entitlement")).
				Get("/{merchantProgramEntitlementID}", app.GetMerchantProgramEntitlementByIDHandler)

			mpe.With(app.RequirePermission("delete_merchant_program_entitlement")).
				Delete("/{merchantProgramEntitlementID}", app.DeleteMerchantProgramEntitlementHandler)
		})


		// Merchant Program Plans
		v1.Route("/merchant-program-plans", func(mpp chi.Router) {
			mpp.Use(app.AuthMiddleware)

			mpp.With(app.RequirePermission("create_merchant_program_plan")).
				Post("/", app.CreateMerchantProgramPlanHandler)

			mpp.With(app.RequirePermission("read_merchant_program_plan")).
				Post("/by-code", app.GetMerchantProgramPlanByCodeHandler)

			mpp.With(app.RequirePermission("read_merchant_program_plan")).
				Post("/active/by-code", app.GetActiveMerchantProgramPlanByCodeHandler)

			mpp.With(app.PaginationAndFilterMiddleware).
				With(app.RequirePermission("list_merchant_program_plans")).
				Get("/all", app.GetAllMerchantProgramPlansHandler)

			mpp.With(app.RequirePermission("list_merchant_program_plans")).
				Get("/active", app.ListActiveMerchantProgramPlansHandler)

			mpp.With(app.RequirePermission("read_merchant_program_plan")).
				Get("/{merchantProgramPlanID}", app.GetMerchantProgramPlanByIDHandler)

			mpp.With(app.RequirePermission("update_merchant_program_plan")).
				Patch("/{merchantProgramPlanID}", app.UpdateMerchantProgramPlanHandler)

			mpp.With(app.RequirePermission("activate_merchant_program_plan")).
				Patch("/{merchantProgramPlanID}/activate", app.ActivateMerchantProgramPlanHandler)

			mpp.With(app.RequirePermission("deactivate_merchant_program_plan")).
				Patch("/{merchantProgramPlanID}/deactivate", app.DeactivateMerchantProgramPlanHandler)

			mpp.With(app.RequirePermission("soft_delete_merchant_program_plan")).
				Delete("/{merchantProgramPlanID}/soft-delete", app.SoftDeleteMerchantProgramPlanHandler)

			mpp.With(app.RequirePermission("restore_merchant_program_plan")).
				Patch("/{merchantProgramPlanID}/restore", app.RestoreMerchantProgramPlanHandler)
		})


		// Merchant Program Subscriptions
		v1.Route("/merchant-program-subscriptions", func(mps chi.Router) {
			mps.Use(app.AuthMiddleware)

			mps.With(app.RequirePermission("create_merchant_program_subscription")).
				Post("/", app.CreateMerchantProgramSubscriptionHandler)

			mps.With(app.RequirePermission("read_current_merchant_program_subscription")).
				Get("/current", app.GetCurrentMerchantProgramSubscriptionByMerchantIDHandler)

			mps.With(app.RequirePermission("read_active_merchant_program_subscription")).
				Get("/active", app.GetActiveMerchantProgramSubscriptionByMerchantIDHandler)

			mps.With(app.RequirePermission("read_merchant_program_subscriptions_by_merchant")).
				Get("/by-merchant", app.ListMerchantProgramSubscriptionsByMerchantIDHandler)

			mps.With(app.RequirePermission("read_merchant_program_subscriptions_by_plan")).
				Get("/by-plan-and-status", app.ListMerchantProgramSubscriptionsByPlanAndStatusHandler)

			mps.With(app.RequirePermission("read_merchant_program_subscription")).
				Get("/{merchantProgramSubscriptionID}", app.GetMerchantProgramSubscriptionByIDHandler)

			mps.With(app.RequirePermission("update_merchant_program_subscription_plan")).
				Patch("/{merchantProgramSubscriptionID}/plan", app.UpdateMerchantProgramSubscriptionPlanHandler)

			mps.With(app.RequirePermission("update_merchant_program_subscription_status")).
				Patch("/{merchantProgramSubscriptionID}/activate", app.ActivateMerchantProgramSubscriptionHandler)

			mps.With(app.RequirePermission("update_merchant_program_subscription_status")).
				Patch("/{merchantProgramSubscriptionID}/pause", app.PauseMerchantProgramSubscriptionHandler)

			mps.With(app.RequirePermission("update_merchant_program_subscription_status")).
				Patch("/{merchantProgramSubscriptionID}/suspend", app.SuspendMerchantProgramSubscriptionHandler)

			mps.With(app.RequirePermission("update_merchant_program_subscription_status")).
				Patch("/{merchantProgramSubscriptionID}/expire", app.ExpireMerchantProgramSubscriptionHandler)

			mps.With(app.RequirePermission("cancel_merchant_program_subscription")).
				Patch("/{merchantProgramSubscriptionID}/cancel", app.CancelMerchantProgramSubscriptionHandler)

			mps.With(app.RequirePermission("soft_delete_merchant_program_subscription")).
				Delete("/{merchantProgramSubscriptionID}/soft-delete", app.SoftDeleteMerchantProgramSubscriptionHandler)

			mps.With(app.RequirePermission("restore_merchant_program_subscription")).
				Patch("/{merchantProgramSubscriptionID}/restore", app.RestoreMerchantProgramSubscriptionHandler)
		})


		// Categories
		v1.Route("/categories", func(cat chi.Router) {
			cat.Use(app.AuthMiddleware)

			// POST: Create a new category (requires permission)
			cat.With(app.RequirePermission("create_category")).
				Post("/", app.CreateCategoryHandler)

			// PATCH: Update an existing category (requires permission and context-injected ID)
			cat.With(app.RequirePermission("update_category")).
				Patch("/", app.UpdateCategoryHandler)

			// DELETE: Soft-delete a category (context-based only)
			cat.With(app.RequirePermission("soft_delete_category")).
				Delete("/soft-delete", app.DeleteCategoryHandler)

			// GET: Retrieve all categories (requires permission)
			cat.With(app.RequirePermission("list_categories")).
				Get("/all", app.GetAllCategoriesHandler)

			// GET: Retrieve a category by ID (requires permission)
			cat.With(app.RequirePermission("read_category")).
				Get("/by-id", app.GetCategoryByIDHandler)
		})


		// Offers
		// Auth-protected offer routes.
		v1.Route("/offers", func(d chi.Router) {
			d.Use(app.AuthMiddleware)

			// POST /api/v1/offers/create — create a new offer
			d.With(app.RequirePermission("create_offer")).
				Post("/create", app.CreateOfferHandler)

			// PATCH /api/v1/offers/update — update an existing offer
			d.With(app.RequirePermission("update_offer")).
				Patch("/update", app.UpdateOfferHandler)

			// DELETE /api/v1/offers/soft-delete — logically remove an offer
			d.With(app.RequirePermission("soft_delete_offer")).
				Delete("/soft-delete", app.SoftDeleteOfferHandler)

			// DELETE /api/v1/offers/purge — permanently delete an offer
			//
			// Purge is an administrative maintenance operation and must remain
			// separate from ordinary soft-delete lifecycle behavior.
			d.With(app.RequirePermission("purge_offer")).
				Delete("/purge", app.PurgeOfferHandler)

			// GET /api/v1/offers/all — retrieve internal offer records
			d.With(app.RequirePermission("read_all_offers")).
				Get("/all", app.GetAllOffersHandler)
		})

		// User Dashboards
		v1.Route("/user-dashboards", func(ud chi.Router) {
			ud.Use(app.AuthMiddleware)

			// POST: Create a new user dashboard
			ud.With(app.RequirePermission("create_user_dashboard")).
				Post("/", app.CreateUserDashboardHandler)

			// PATCH: Update an existing user dashboard
			ud.With(app.RequirePermission("update_user_dashboard")).
				Patch("/", app.UpdateUserDashboardHandler)

			// GET: Retrieve a user dashboard by user ID
			ud.With(app.RequirePermission("read_user_dashboard")).
				Get("/", app.GetUserDashboardByUserIDHandler)

			// GET: Retrieve a user dashboard by ID
			ud.With(app.RequirePermission("read_user_dashboard")).
				Get("/by-id", app.GetUserDashboardByIDHandler)

			// GET: Retrieve admin dashboard statistics
			ud.With(app.RequirePermission("view_admin_dashboard_stats")).
				Get("/dashboard-stats", app.AdminDashboardStatsHandler)
		})


		// User Notifications (system-managed)
		v1.Route("/user-notifications", func(un chi.Router) {
			un.Use(app.AuthMiddleware) // Require authentication

			// GET: Retrieve user notifications by user ID (admin/internal only)
			un.With(
				app.RequireInternalRole, // admin or internal_operator
				app.RequirePermission("read_user_notifications"),
			).Get("/by-user", app.GetUserNotificationByUserIDHandler)
		})

		// User Profile
		v1.Route("/user-profile", func(up chi.Router) {
			up.Use(app.AuthMiddleware)

			// GET /user-profile/self
			up.With(app.RequireAuthenticatedUser).
				Get("/self", app.GetOwnUserProfileHandler)

			// PATCH: Update own profile
			up.With(app.RequireAuthenticatedUser).
				Patch("/self", app.UpdateOwnUserProfileHandler)

			// PATCH: Admin or Operator updates another user's profile
			up.With(app.RequirePermission("update_user_profile")).
				Patch("/", app.UpdateUserProfileHandler)

			// PATCH: Flag or unflag user profile (internal moderation only)
			up.With(app.RequirePermission("moderate_user_profile")).
				Patch("/moderate", app.ModerateUserProfileHandler)

			// GET: Authenticated access to a user profile by user ID
			// (visibility remains enforced inside the handler).
			up.With(app.RequireAuthenticatedUser).
				Get("/by-user", app.GetUserProfileByUserIDHandler)

			// GET: List user profiles
			up.With(app.RequirePermission("read_user_profiles")).
				Get("/all", app.ListUserProfilesHandler)

			// GET: Search user profiles
			up.With(app.RequirePermission("read_user_profiles")).
				Get("/search", app.SearchUserProfilesHandler)

			// GET: Return reserved user handles for UI validation
			up.With(app.RequirePermissionOrRole("read_reserved_handles", "admin", "internal_operator")).
				Get("/reserved-handles", app.GetReservedHandlesHandler)
		})

		// User Settings
		v1.Route("/user-settings", func(us chi.Router) {
			us.Use(app.AuthMiddleware)

			// POST: Save user settings
			us.With(app.RequirePermission("save_user_settings")).
				Post("/", app.SaveUserSettingsHandler)

			// PATCH: Update user settings
			us.With(app.RequireAuthenticatedUser, app.RequireSelfOrPrivileged("update_user_settings")).
				Patch("/", app.UpdateUserSettingsHandler)

			// GET: Retrieve user settings by user ID
			us.With(app.RequirePermission("read_user_settings")).
				Get("/", app.GetUserSettingsByUserIDHandler)

			// GET: Retrieve user settings by user ID
			us.With(app.RequirePermission("read_user_settings")).
				Get("/{id}", app.GetUserSettingsByIDHandler)

			// GET: Retrieve all user settings
			us.With(app.PaginationAndFilterMiddleware).
				With(app.RequirePermission("read_user_settings")).
				Get("/all", app.GetAllUserSettingsHandler)
		})

		// Users
		v1.Route("/users", func(u chi.Router) {
			u.Use(app.AuthMiddleware)

			// DELETE: Delete per existing user's own request
			u.With(app.RequireAuthenticatedUser).
				Delete("/", app.DeleteMeHandler)

			// DELETE: Delete an existing user
			u.With(app.RequireRole("admin"), app.InjectTargetUserID).
				Delete("/admin/users/{userID}", app.AdminDeleteUserHandler)
		})

	})


	// JSON 404 for everything else
    r.NotFound(app.NotFoundHandler)

	app.Router = r
	return r
}


func (app *Application) registerPublic(method, path string) {
    app.publicRoutes.add(method, path)
}