// sdworkspace/sdbackend/internal/server/cmd/api/routes.go
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
		v1.Use(app.DevFallbackContextMiddleware)

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

		// ---- AUTHENTICATED API ROUTES ----

		// User Activation Routes
		v1.With(app.RateLimitMiddleware).Post("/user/activation", app.CreateActivationTokenHandler)
		v1.Post("/user/activate", app.ActivateUserHandler)
		v1.Get("/user/activation/status", app.GetUserActivationStatusHandler)

		// Authentication Routes
		v1.Post("/user/register", app.RegisterUserHandler)
		v1.Post("/user/login", app.LoginHandler)
		v1.Post("/user/token/refresh", app.RefreshTokenHandler)
		v1.Post("/user/logout", app.LogoutHandler)

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


		// Merchant Applications
		v1.Route("/merchant-applications", func(aa chi.Router) {
			aa.Use(app.AuthMiddleware)

			// POST: Create a new merchant application (requires permission)
			aa.With(app.RequirePermission("create_merchant_application")).
				Post("/", app.CreateMerchantApplicationHandler)

			// PATCH: Update an merchant application (context-driven, partial update)
			aa.With(app.RequirePermission("update_merchant_application")).
				Patch("/", app.UpdateMerchantApplicationHandler)

			// DELETE: Soft-delete an merchant application (context-driven, updates status_id)
			aa.With(app.RequirePermission("soft_delete_merchant_application")).
				Delete("/soft-delete", app.SoftDeleteMerchantApplicationHandler)

			// GET: Retrieve a single application by ID (from context)
			aa.With(app.RequirePermission("read_merchant_application")).
				Get("/", app.GetMerchantApplicationByIDHandler)

			// GET: Retrieve all applications for an merchant (context-based merchant ID)
			aa.With(app.RequirePermission("read_merchant_application")).
				Get("/by-merchant", app.GetMerchantApplicationByMerchantIDHandler)

			// GET: Retrieve all applications for an affiliate program (context-based)
			aa.With(app.RequirePermission("list_merchant_applications")).
				Get("/by-affiliate-program", app.GetMerchantApplicationsByAffiliateProgramIDHandler)

			// GET: Retrieve all applications filtered by status_id (from context), paginated
			aa.With(app.PaginationAndFilterMiddleware).
				With(app.RequirePermission("list_merchant_applications")).
				Get("/by-status", app.GetMerchantApplicationsByStatusIDHandler)

			// GET: Retrieve all applications (filtered by affiliate program, paginated)
			aa.With(app.PaginationAndFilterMiddleware).
				With(app.RequirePermission("list_merchant_applications")).
				Get("/all", app.GetAllMerchantApplicationsHandler)
		})

		// Merchant Application Statuses
		v1.Route("/merchant-application-statuses", func(s chi.Router) {
			s.Use(app.AuthMiddleware)

			// POST: Create a new application status
			s.With(app.RequirePermission("create_merchant_application_status")).
				Post("/", app.CreateApplicationStatusHandler)

			// POST: Retrieve a status by name (JSON body input, not path param)
			s.With(app.RequirePermission("read_merchant_application_status")).
				Post("/by-name", app.GetApplicationStatusByNameHandler)

			// PATCH: Update an application status by ID (context-based only)
			s.With(app.RequirePermission("update_merchant_application_status")).
				Patch("/", app.UpdateApplicationStatusHandler)

			// DELETE: Soft-delete a status by ID (context-based)
			s.With(app.RequirePermission("soft_delete_merchant_application_status")).
				Delete("/soft-delete", app.SoftDeleteApplicationStatusHandler)

			// GET: Retrieve a single application status by ID (from context only)
			s.With(app.RequirePermission("read_merchant_application_status")).
				Get("/", app.GetApplicationStatusByIDHandler)

			// GET: Retrieve all application statuses (supports pagination + name filter)
			s.With(app.PaginationAndFilterMiddleware).
				With(app.RequirePermission("list_merchant_application_statuses")).
				Get("/all", app.GetAllApplicationStatusHandler)
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


		// Merchant Type
		v1.Route("/merchant-type", func(at chi.Router) {
			at.Use(app.AuthMiddleware)

			// POST: Create a new merchant type (context-free, JSON input)
			at.With(app.RequirePermission("create_merchant_type")).
				Post("/", app.CreateMerchantTypeHandler)

			// POST: Retrieve merchant type by name (JSON input)
			at.With(app.RequirePermission("read_merchant_type")).
				Post("/by-name", app.GetMerchantTypeByNameHandler)

			// PATCH: Partially update an merchant type by ID (context-based only)
			at.With(app.RequirePermission("update_merchant_type")).
				Patch("/", app.UpdateMerchantTypeHandler)

			// DELETE: Soft delete an merchant type (context-based only)
			at.With(app.RequirePermission("soft_delete_merchant_type")).
				Delete("/soft-delete", app.DeleteMerchantTypeHandler)

			// GET: Retrieve a single merchant type by ID (from context only)
			at.With(app.RequirePermission("read_merchant_type")).
				Get("/", app.GetMerchantTypeByIDHandler)

			// GET: Retrieve all merchant types (with optional filters from context)
			at.With(app.PaginationAndFilterMiddleware).
				With(app.RequirePermission("list_merchant_types")).
				Get("/all", app.GetAllMerchantTypesHandler)
		})

		// Merchant Affiliate Program
		v1.Route("/merchant-affiliate-programs", func(ap chi.Router) {
			ap.Use(app.AuthMiddleware)

			// POST: Create merchant-affiliate program association (context-based only)
			ap.With(app.RequirePermission("create_merchant_affiliate_program")).
				Post("/affiliate-program-association", app.CreateMerchantAffiliateProgramHandler)

			// DELETE: Soft-delete merchant-affiliate program association (context-based only)
			ap.With(app.RequirePermission("soft_delete_merchant_affiliate_program")).
				Delete("/soft-delete", app.DeleteMerchantAffiliateProgramHandler)

			// DELETE: Soft-delete all affiliate program associations for a given merchant (context-based)
			ap.With(app.RequirePermission("soft_delete_merchant_affiliate_program")).
				Delete("/soft-delete/by-merchant", app.DeleteMerchantAffiliateProgramByMerchantIDHandler)

			// DELETE: Soft-delete all merchant associations for a given affiliate program (context-based)
			ap.With(app.RequirePermission("soft_delete_merchant_affiliate_program")).
				Delete("/soft-delete/by-affiliate-program", app.DeleteMerchantAffiliateProgramByAffiliateProgramIDHandler)

			// GET: Retrieve association by merchant ID and affiliate program ID from context
			ap.With(app.RequirePermission("read_merchant_affiliate_program")).
				Get("/association", app.GetMerchantAffiliateProgramByIDsHandler)

			// GET: Retrieve all affiliate programs linked to an merchant (context-based only)
			ap.With(app.PaginationAndFilterMiddleware).
				With(app.RequirePermission("read_merchant_affiliate_programs")).
				Get("/by-merchant", app.GetMerchantAffiliateProgramByMerchantIDHandler)

			// GET: Retrieve all merchants associated with an affiliate program (context-based only)
			ap.With(app.RequirePermission("read_merchant_affiliate_programs")).
				Get("/by-affiliate-program", app.GetMerchantAffiliateProgramByAffiliateProgramIDHandler)

			// GET: Retrieve all merchant-affiliate program associations (filtered + audited)
			ap.With(app.RequirePermission("list_merchant_affiliate_programs")).
				Get("/all", app.GetAllMerchantAffiliateProgramsHandler)
		})

		// Platform
		v1.Route("/platforms", func(p chi.Router) {
			p.Use(app.AuthMiddleware)

			// POST: Create a new platform (requires create_platform permission)
			p.With(app.RequirePermission("create_platform")).
				Post("/", app.CreatePlatformHandler)

			// POST: Retrieve platform by name (JSON input, context-only)
			p.With(app.RequirePermission("read_platform")).
				Post("/by-name", app.GetPlatformByNameHandler)

			// PATCH: Partially update a platform (context-based only)
			p.With(app.RequirePermission("update_platform")).
				Patch("/", app.UpdatePlatformHandler)

			// DELETE: Soft-delete a platform (context-based only)
			p.With(app.RequirePermission("soft_delete_platform")).
				Delete("/soft-delete", app.SoftDeletePlatformHandler)

			// GET: Retrieve a platform by ID (context-only)
			p.With(app.RequirePermission("read_platform")).
				Get("/", app.GetPlatformByIDHandler)

			// GET: Retrieve all non-deleted platforms
			p.With(app.RequirePermission("list_platforms")).
				Get("/all", app.GetAllPlatformsHandler)
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


		// Offer Status
		v1.Route("/offer-statuses", func(dst chi.Router) {
			dst.Use(app.AuthMiddleware)

			// POST: Create a new offer status
			dst.With(app.RequirePermission("create_offer_status")).
				Post("/", app.CreateOfferStatusHandler)

			// POST: Submit a offer status for review
			dst.With(app.RequirePermission("submit_offer_status_for_review")).
				Post("/submit-for-review", app.SubmitOfferStatusForReviewHandler)

			// PATCH: Update the status of a offer
			dst.With(app.RequirePermission("update_offer_status")).
				Patch("/status", app.UpdateOfferStatusHandler)

			// DELETE: Delete a offer status (requires permission)
			dst.With(app.RequirePermission("delete_offer_status")).
				Delete("/", app.DeleteOfferStatusHandler)

			// GET: Retrieve a offer status by ID
			dst.With(app.RequirePermission("read_offer_status")).
				Get("/by-id", app.GetOfferStatusByIDHandler)

			// GET: Retrieve all offer statuses
			dst.With(app.RequirePermission("read_offer_statuses")).
				Get("/all", app.GetAllOfferStatusesHandler)
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
		v1.Route("/admin", func(ud chi.Router) {
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

		// User Favorites
		v1.Route("/favorites", func(uf chi.Router) {
			uf.Use(app.AuthMiddleware)

			// POST: Create a new user favorite
			uf.With(app.RequirePermission("create_user_favorite")).
				Post("/", app.CreateUserFavoriteHandler)
/*
			// POST: Migrate favorites from one user to another
			uf.With(app.RequirePermission("migrate_favorites")).
				Post("/migrate", app.MigrateFavoritesToNewUserHandler)

			// POST: Set offer allert lets user request notification when similar offer is posted
			uf.With(app.RequireAuthenticatedUser).
				Post("/", app.SetOfferAlertHandler)

			// POST: Share a favorite generates a shareable favorite list for sharing with another user
			uf.With(app.RequireAuthenticatedUser).
				Post("/share", app.ShareFavoriteListHandler)

			// POST: Share a offer generates a shareable link for a user to send a offer to friends
			uf.With(app.RequireAuthenticatedUser).
				Post("/", app.ShareOfferHandler)

			// POST: Let users request a restock notification when out of stock offer is back in stock
			uf.With(app.RequireAuthenticatedUser).
				Post("/request", app.RequestRestockNotificationHandler)

			// POST: Save a user's favorite offer allows logged in user to favorite a offer
			uf.With(app.RequireAuthenticatedUser).
				Post("/", app.SaveUserFavoriteHandler)
*/
			// DELETE: Unsave a user favorite item removes a users favorite offer item
			uf.With(app.RequireAuthenticatedUser).
				Delete("/", app.UnsaveUserFavoriteHandler)

			// GET: Retrieve user favorites by user ID
			uf.With(app.RequirePermission("read_user_favorites")).
				Get("/me", app.GetUserFavoriteByUserIDHandler)

			// GET: Retrieve a user favorite by ID
			uf.With(app.RequirePermission("read_user_favorite")).
				Get("/by-id", app.GetUserFavoriteByIDHandler)
/*
			// GET: Retrieves all active favorites for a given user.
			// Requires login, but permission is enforced internally (only for non-self access).
			uf.With(app.RequireAuthenticatedUser).
				Get("/active", app.GetActiveUserFavoritesByUserIDHandler)

			// GET: Retrieve the purchase history of offers for a user
			uf.With(app.RequirePermission("read_user_offer_purchase_history")).
				Get("/purchase-history", app.GetUserOfferPurchaseHistoryHandler)

			// GET: Retrieve most favorited offers
			uf.With(app.RequirePermission("read_most_favorited_offers")).
				Get("/most-favorited", app.GetMostFavoritedOffersHandler)

			// GET: Retrieve the most recently favorited offer by the authenticated user
			uf.With(app.RequirePermission("read_recently_favorited_offer")).
				Get("/recent", app.GetRecentlyFavoritedOfferByUserHandler)

			// GET: Retrieve trending offers for a specific user
			uf.With(app.RequirePermission("read_trending_offers_for_user")).
				Get("/trending-for-user", app.GetTrendingOffersForUserHandler)

			// GET: Retrieve the count of favorite items for a specific user
			uf.With(app.RequirePermission("read_user_favorites_count")).
				Get("/count", app.GetUserFavoritesCountByUserHandler)

			// GET: Retrieve user favorites count for a specific offer
			uf.With(app.RequirePermission("read_user_favorites_count")).
				Get("/count/by-offer", app.GetUserFavoritesCountByOfferHandler)

			// GET: Retrieve users who favorited a specific offer
			uf.With(app.RequirePermission("read_users_who_favorited_offer")).
				Get("/by-offer", app.GetUsersWhoFavoritedOfferHandler)*/
		})

		// Merchant Follow/Unfollow
		v1.Route("/merchant-follows", func(mf chi.Router) {
			mf.Use(app.AuthMiddleware)
/*
			// POST: Follow a merchant lets a user follow a merchant to get notified of offers from that merchant
			mf.With(app.RequireAuthenticatedUser).
				Post("/", app.FollowMerchantHandler)

			// DELETE: Unfollow a merchant to stop reciving notifications of offers from that merchant
			mf.With(app.RequireAuthenticatedUser).
				Delete("/", app.UnfollowMerchantHandler)

			// GET: Offers from followed merchants
			mf.With(app.RequirePermission("read_followed_merchants_offers")).
				Get("/offers", app.GetFollowedMerchantsOffersHandler)*/
		})

		// User Notifications (system-managed)
		v1.Route("/user-notifications", func(un chi.Router) {
			un.Use(app.AuthMiddleware) // Require authentication
/*
			// PATCH: Update a user notification (internal diagnostics only)
			un.With(
				app.RequireInternalRole, // admin or internal_operator
				app.RequirePermission("update_user_notification"),
			).Patch("/{notificationID}", app.UpdateUserNotificationHandler)

			// GET: Retrieve a specific user notification by ID
			un.With(
				app.RequireInternalRole,
				app.RequirePermission("read_user_notification"),
			).Get("/{id}", app.GetUserNotificationByIDHandler)
*/
			// GET: Retrieve user notifications by user ID (admin/internal only)
			un.With(
				app.RequireInternalRole, // admin or internal_operator
				app.RequirePermission("read_user_notifications"),
			).Get("/by-user", app.GetUserNotificationByUserIDHandler)
/*
			// GET: Retrieve user notifications by associated offer ID
			un.With(
				app.RequireInternalRole,
				app.RequirePermission("read_user_notifications"),
			).Get("/by-offer", app.GetUserNotificationByOfferIDHandler)

			// GET: Retrieve user notifications by type
			un.With(
				app.RequireInternalRole,
				app.RequirePermission("read_user_notification_by_type"),
			).Get("/by-type", app.GetUserNotificationByTypeHandler)*/
		})

		// User Profile
		v1.Route("/user-profile", func(up chi.Router) {
			up.Use(app.AuthMiddleware)
/*
			// POST: Create a new user profile (invoked post-registration, not meant for UI trigger)
			up.With(app.RequirePermission("create_user_profile")).
				Post("/", app.CreateUserProfileHandler)

			// POST: Trigger auto flag profiles
			up.With(app.RequirePermission("trigger_auto_flag_profiles")).
				Post("/trigger-auto-flag", app.TriggerAutoFlagUserProfilesHandler)
*/
			// GET /user-profile/self
			up.With(app.RequireAuthenticatedUser).
				Get("/self", app.GetOwnUserProfileHandler)

			// PATCH: Update own profile
			up.With(app.RequireAuthenticatedUser).
				Patch("/self", app.UpdateOwnUserProfileHandler)
/*
			// PATCH /api/v1/user-profile/self/visibility
			up.With(app.RequireAuthenticatedUser).
				Patch("/self/visibility", app.UpdateUserProfileVisibilityHandler)
*/
			// PATCH: Admin or Operator updates another user's profile
			up.With(app.RequirePermission("update_user_profile")).
				Patch("/", app.UpdateUserProfileHandler)

			// PATCH: Flag or unflag user profile (internal moderation only)
			up.With(app.RequirePermission("moderate_user_profile")).
				Patch("/moderate", app.ModerateUserProfileHandler)

			// GET: Public access to a user profile by ID (enforces visibility internally)
			up.Get("/", app.GetUserProfileByUserIDHandler)

			// GET: List user profiles
			up.With(app.RequirePermission("read_user_profiles")).
				Get("/", app.ListUserProfilesHandler)

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

		// User Wallets
		v1.Route("/user-wallets", func(uw chi.Router) {
			uw.Use(app.AuthMiddleware)
		/*
			// POST: Create a new user wallet
			uw.With(app.RequirePermission("create_user_wallet")).
				Post("/", app.CreateUserWalletHandler)
			*/

			// GET: Retrieve a user wallet by user ID
			uw.With(app.RequirePermission("read_user_wallet")).
				Get("/me", app.GetUserWalletByUserIDHandler)

			// GET: Retrieve a user wallet by ID
			uw.With(app.RequirePermission("read_user_wallet")).
				Get("/by-id", app.GetUserWalletByIDHandler)

			// GET: Retrieve all user wallets (paginated)
			uw.With(app.PaginationAndFilterMiddleware).
				With(app.RequirePermission("read_user_wallets")).
				Get("/all", app.ListUserWalletsHandler)
		})
	})

/*
    // Health endpoints for probes/monitors
    r.Get("/healthz", app.LivenessHandler)
    r.Get("/readyz", app.ReadinessHandler)

	// Prometheus Metrics (not versioned)
	r.Handle("/metrics", promhttp.Handler())

	// Development only
	if strings.ToLower(app.Config.Bootstrap.Env) == "development" {
		r.Get("/debug/context", app.DebugContextHandler)
	}*/

	// JSON 404 for everything else
    r.NotFound(app.NotFoundHandler)

	app.Router = r
	return r
}


func (app *Application) registerPublic(method, path string) {
    app.publicRoutes.add(method, path)
}