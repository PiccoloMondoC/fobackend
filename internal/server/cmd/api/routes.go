// Package main provides HTTP routing for the Platform API.
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
//	  for the Platform API. It establishes the middleware boundaries,
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
//
// Routing Convention:
//
//	Use permission string literals in RequirePermission(...) route
//	registration throughout this file for consistency. Do not introduce
//	isolated permission constants unless this file is intentionally
//	migrated to a different convention as a whole.
package main

import (
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
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
			al.With(app.RequirePermission("read_audit_log")).
				Post("/", app.GetAuditLogByIDHandler)

			// POST: Retrieve audit logs for authenticated user
			al.With(app.RequirePermission("read_audit_log")).
				Post("/by-user", app.GetAuditLogByUserIDHandler)

			// POST: Retrieve audit logs by entity ID and entity type ID (context-based only)
			al.With(app.RequirePermission("read_audit_log")).
				Post("/by-entity", app.GetAuditLogByEntityIDHandler)

			// POST: Retrieve audit logs by time range (start, end in RFC3339 format)
			al.With(app.RequirePermission("read_audit_logs_by_time_range")).
				Post("/by-time-range", app.GetAuditLogsByTimeRangeHandler)

			// POST: Restore archived audit logs
			al.With(app.RequirePermission("restore_archived_audit_logs")).
				Post("/archived/restore", app.RestoreArchivedLogsHandler)

			// POST: Retrieve archived audit logs by entity ID and entity type ID (context-based only)
			al.With(app.RequirePermission("read_archived_audit_log")).
				Post("/archived/by-entity", app.GetArchivedAuditLogByEntityHandler)

			// POST: Retrieve archived audit logs by time range
			al.With(app.RequirePermission("read_archived_audit_logs")).
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

		// Admin Console
		v1.Route("/admin-console", func(ac chi.Router) {
			ac.Use(app.AuthMiddleware)

			ac.With(
				app.RequirePermission(
					permissionReadAdminConsole,
				),
			).Get(
				"/",
				app.GetAdminConsoleOverviewHandler,
			)
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

		// Platform Setting History
		v1.Route("/platform-setting-history", func(psh chi.Router) {
			psh.Use(app.AuthMiddleware)

			psh.With(
				app.RequirePermission(
					"list_platform_setting_history",
				),
			).Post(
				"/by-key",
				app.ListPlatformSettingHistoryBySettingKeyHandler,
			)

			psh.With(
				app.RequirePermission(
					"list_platform_setting_history",
				),
			).Get(
				"/platform-setting/{platformSettingID}",
				app.ListPlatformSettingHistoryByPlatformSettingIDHandler,
			)

			psh.With(
				app.RequirePermission(
					"read_platform_setting_history",
				),
			).Get(
				"/{platformSettingHistoryID}",
				app.GetPlatformSettingHistoryByIDHandler,
			)
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

		// Merchant Accounts
		v1.Route("/merchant-accounts", func(ma chi.Router) {
			ma.Use(app.AuthMiddleware)

			ma.With(
				app.RequirePermission(
					"create_merchant_account",
				),
			).Post(
				"/",
				app.CreateMerchantAccountHandler,
			)

			ma.With(
				app.RequirePermission(
					"read_merchant_account_by_merchant",
				),
			).Post(
				"/by-merchant",
				app.GetMerchantAccountByMerchantIDHandler,
			)

			ma.With(
				app.RequirePermission(
					"read_deleted_merchant_account_by_merchant",
				),
			).Post(
				"/admin/by-merchant",
				app.GetMerchantAccountByMerchantIDIncludingDeletedHandler,
			)

			ma.With(
				app.RequirePermission(
					"read_merchant_account_by_merchant",
				),
			).Post(
				"/exists/by-merchant",
				app.ExistsMerchantAccountByMerchantIDHandler,
			)

			// Permission is resolved inside the handler according to
			// include_deleted:
			// list_merchant_accounts or list_deleted_merchant_accounts.
			ma.With(
				app.PaginationAndFilterMiddleware,
			).Get(
				"/all",
				app.GetAllMerchantAccountsHandler,
			)

			ma.With(
				app.RequirePermission(
					"read_deleted_merchant_account",
				),
			).Get(
				"/admin/{merchantAccountID}",
				app.GetMerchantAccountByIDIncludingDeletedHandler,
			)

			ma.With(
				app.RequirePermission(
					"read_merchant_account",
				),
			).Get(
				"/{merchantAccountID}",
				app.GetMerchantAccountByIDHandler,
			)

			ma.With(
				app.RequirePermission(
					"activate_merchant_account",
				),
			).Patch(
				"/{merchantAccountID}/activate",
				app.ActivateMerchantAccountHandler,
			)

			ma.With(
				app.RequirePermission(
					"suspend_merchant_account",
				),
			).Patch(
				"/{merchantAccountID}/suspend",
				app.SuspendMerchantAccountHandler,
			)

			ma.With(
				app.RequirePermission(
					"close_merchant_account",
				),
			).Patch(
				"/{merchantAccountID}/close",
				app.CloseMerchantAccountHandler,
			)

			ma.With(
				app.RequirePermission(
					"soft_delete_merchant_account",
				),
			).Delete(
				"/{merchantAccountID}/soft-delete",
				app.SoftDeleteMerchantAccountHandler,
			)

			ma.With(
				app.RequirePermission(
					"restore_merchant_account",
				),
			).Patch(
				"/{merchantAccountID}/restore",
				app.RestoreMerchantAccountHandler,
			)

			ma.With(
				app.RequirePermission(
					"hard_delete_merchant_account",
				),
			).Delete(
				"/{merchantAccountID}",
				app.HardDeleteMerchantAccountHandler,
			)
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

			// Merchant Program Subscription Events

			mps.With(app.RequirePermission("list_merchant_program_subscription_events")).
				Get("/{merchantProgramSubscriptionID}/events/latest", app.GetLatestMerchantProgramSubscriptionEventHandler)

			mps.With(app.RequirePermission("list_merchant_program_subscription_events")).
				Get("/{merchantProgramSubscriptionID}/events", app.ListMerchantProgramSubscriptionEventsHandler)

			// Merchant Program Subscription Periods
			//
			// Period history is privileged and read-only. Period creation is
			// service/orchestration-owned and is not exposed through HTTP.
			// Merchant actors must not receive these permissions until canonical
			// merchant-account ownership/delegation enforcement exists for this
			// route family.

			mps.With(app.RequirePermission("list_merchant_program_subscription_periods")).
				Get("/{merchantProgramSubscriptionID}/periods/latest", app.GetLatestMerchantProgramSubscriptionPeriodHandler)

			mps.With(app.RequirePermission("read_merchant_program_subscription_period")).
				Get("/{merchantProgramSubscriptionID}/periods/at-instant", app.GetMerchantProgramSubscriptionPeriodAtInstantHandler)

			mps.With(app.RequirePermission("list_merchant_program_subscription_periods")).
				Get("/{merchantProgramSubscriptionID}/periods", app.ListMerchantProgramSubscriptionPeriodsHandler)
		})

		// Merchant Program Subscription Events
		//
		// Event history is privileged and read-only. Merchant actors must not
		// receive these permissions until canonical merchant-account
		// ownership/delegation enforcement is implemented for this route
		// family.
		v1.Route(
			"/merchant-program-subscription-events",
			func(events chi.Router) {
				events.Use(app.AuthMiddleware)

				events.With(app.RequirePermission("read_merchant_program_subscription_event")).
					Get("/{eventID}", app.GetMerchantProgramSubscriptionEventByIDHandler)
			})

		// Merchant Program Subscription Periods
		//
		// Canonical direct lookup of one immutable subscription-period fact.
		// Period creation, update, deletion, restoration, and purge are not
		// exposed through HTTP.
		v1.Route(
			"/merchant-program-subscription-periods",
			func(periods chi.Router) {
				periods.Use(app.AuthMiddleware)

				periods.With(
					app.RequirePermission(
						"read_merchant_program_subscription_period",
					),
				).Get(
					"/{merchantProgramSubscriptionPeriodID}",
					app.GetMerchantProgramSubscriptionPeriodByIDHandler,
				)
			})

		// Merchant Platform Credit Accounts
		//
		// This is a privileged administrative surface. Merchant possession of a
		// merchant account does not itself authorize issuing, listing, correcting,
		// or cancelling platform commercial credit.
		v1.Route("/merchant-platform-credit-accounts", func(mpca chi.Router) {
			mpca.Use(app.AuthMiddleware)

			mpca.With(app.RequirePermission(actionCreateMerchantPlatformCreditAccount)).
				Post("/", app.CreateMerchantPlatformCreditAccountHandler)

			// Keep the more-specific merchant routes before the two-ID route.
			mpca.With(app.RequirePermission(actionListMerchantPlatformCreditAccounts)).
				Get("/merchant/{merchantID}/usable", app.ListCurrentlyUsableMerchantPlatformCreditAccountsHandler)

			mpca.With(app.RequirePermission(actionListMerchantPlatformCreditAccounts)).
				Get("/merchant/{merchantID}", app.ListMerchantPlatformCreditAccountsByMerchantHandler)

			mpca.With(app.RequirePermission(actionReadMerchantPlatformCreditAccount)).
				Get("/merchant/{merchantID}/{merchantPlatformCreditAccountID}", app.GetMerchantPlatformCreditAccountByIDForMerchantHandler)

			mpca.With(app.RequirePermission(actionReadMerchantPlatformCreditAccount)).
				Get("/{merchantPlatformCreditAccountID}", app.GetMerchantPlatformCreditAccountByIDHandler)
		})

		// Merchant Platform Credit Eligible Fee Types
		//
		// Fee-type eligibility is privileged administrative configuration owned by
		// the parent merchant platform credit account. One association is identified
		// by the composite key:
		//
		//	merchantPlatformCreditAccountID + feeType
		//
		// PUT on the collection performs complete atomic replacement of the
		// account's eligibility set. An explicitly supplied empty fee_types array
		// clears that set.
		v1.Route(
			"/merchant-platform-credit-accounts/"+
				"{merchantPlatformCreditAccountID}/eligible-fee-types",
			func(mpce chi.Router) {
				mpce.Use(app.AuthMiddleware)

				mpce.With(
					app.RequirePermission(
						"create_merchant_platform_credit_eligible_fee_type",
					),
				).Post(
					"/",
					app.CreateMerchantPlatformCreditEligibleFeeTypeHandler,
				)

				mpce.With(
					app.RequirePermission(
						"list_merchant_platform_credit_eligible_fee_types",
					),
				).Get(
					"/",
					app.ListMerchantPlatformCreditEligibleFeeTypesHandler,
				)

				mpce.With(
					app.RequirePermission(
						"replace_merchant_platform_credit_eligible_fee_type_set",
					),
				).Put(
					"/",
					app.ReplaceMerchantPlatformCreditEligibleFeeTypeSetHandler,
				)

				mpce.With(
					app.RequirePermission(
						"check_merchant_platform_credit_eligible_fee_type",
					),
				).Get(
					"/{feeType}/check",
					app.CheckMerchantPlatformCreditEligibleFeeTypeHandler,
				)

				mpce.With(
					app.RequirePermission(
						"read_merchant_platform_credit_eligible_fee_type",
					),
				).Get(
					"/{feeType}",
					app.GetMerchantPlatformCreditEligibleFeeTypeHandler,
				)

				mpce.With(
					app.RequirePermission(
						"delete_merchant_platform_credit_eligible_fee_type",
					),
				).Delete(
					"/{feeType}",
					app.DeleteMerchantPlatformCreditEligibleFeeTypeHandler,
				)
			},
		)

		// Merchant Platform Credit Applications
		v1.Route("/merchant-platform-credit-applications", func(mpcap chi.Router) {
			mpcap.Use(app.AuthMiddleware)

			// Keep static routes before the parameterized application-ID route.
			mpcap.With(app.RequirePermission("read_merchant_platform_credit_application")).
				Get(
					"/by-account/{merchantPlatformCreditAccountID}/fee-calculation/{feeCalculationID}",
					app.GetMerchantPlatformCreditApplicationByCreditAccountAndFeeCalculationHandler,
				)

			mpcap.With(app.RequirePermission("list_merchant_platform_credit_applications")).
				Get(
					"/by-account/{merchantPlatformCreditAccountID}",
					app.ListMerchantPlatformCreditApplicationsByCreditAccountHandler,
				)

			mpcap.With(app.RequirePermission("list_merchant_platform_credit_applications")).
				Get(
					"/by-fee-calculation/{feeCalculationID}",
					app.ListMerchantPlatformCreditApplicationsByFeeCalculationHandler,
				)

			mpcap.With(app.RequirePermission("read_merchant_platform_credit_application")).
				Get(
					"/{merchantPlatformCreditApplicationID}",
					app.GetMerchantPlatformCreditApplicationByIDHandler,
				)
		})

		// Merchant Billing Accounts
		//
		// merchant_id is the billing account's canonical primary key, so the
		// per-merchant resource is singular. This surface is privileged and
		// administrative; it does not expose merchant-held funds or payment
		// execution.
		v1.Route(
			"/merchants/{merchantID}/billing-account",
			func(mba chi.Router) {
				mba.Use(app.AuthMiddleware)

				mba.With(
					app.RequirePermission(
						"create_merchant_billing_account",
					),
				).Post(
					"/",
					app.CreateMerchantBillingAccountHandler,
				)

				mba.With(
					app.RequirePermission(
						"read_merchant_billing_account",
					),
				).Get(
					"/",
					app.GetMerchantBillingAccountByMerchantIDHandler,
				)

				mba.With(
					app.RequirePermission(
						"suspend_merchant_billing_account",
					),
				).Patch(
					"/suspend",
					app.SuspendMerchantBillingAccountHandler,
				)

				mba.With(
					app.RequirePermission(
						"reactivate_merchant_billing_account",
					),
				).Patch(
					"/reactivate",
					app.ReactivateMerchantBillingAccountHandler,
				)

				mba.With(
					app.RequirePermission(
						"close_merchant_billing_account",
					),
				).Patch(
					"/close",
					app.CloseMerchantBillingAccountHandler,
				)
			},
		)

		// Merchant Billing Accounts — cross-merchant administrative listing
		v1.Route(
			"/merchant-billing-accounts",
			func(mba chi.Router) {
				mba.Use(app.AuthMiddleware)

				mba.With(
					app.RequirePermission(
						"list_merchant_billing_accounts",
					),
				).Get(
					"/",
					app.ListMerchantBillingAccountsByStatusHandler,
				)
			},
		)

		// Merchant Billable Events
		//
		// Privileged, source-linked commercial-history and reconciliation surface.
		// Creation and lifecycle mutation are not exposed through HTTP. Merchant
		// actors must not receive these permissions unless a future route family has
		// canonical ownership/delegation enforcement and an explicitly approved
		// merchant-facing requirement.
		v1.Route("/merchant-billable-events", func(mbe chi.Router) {
			mbe.Use(app.AuthMiddleware)

			mbe.With(app.RequirePermission("read_merchant_billable_event")).
				Get(
					"/by-future-offering-event/{futureOfferingEventID}",
					app.GetMerchantBillableEventByFutureOfferingEventIDHandler,
				)

			mbe.With(app.RequirePermission("read_merchant_billable_event")).
				Get(
					"/by-subscription-period/{subscriptionPeriodID}",
					app.GetMerchantBillableEventBySubscriptionPeriodIDHandler,
				)

			mbe.With(app.RequirePermission("read_merchant_billable_event")).
				Get(
					"/by-engagement-event/{engagementEventID}",
					app.GetMerchantBillableEventByEngagementEventIDHandler,
				)

			mbe.With(app.RequirePermission("list_merchant_billable_events")).
				Get(
					"/by-merchant/{merchantID}/status",
					app.ListMerchantBillableEventsByMerchantAndStatusHandler,
				)

			mbe.With(app.RequirePermission("list_merchant_billable_events")).
				Get(
					"/by-merchant/{merchantID}",
					app.ListMerchantBillableEventsByMerchantHandler,
				)

			mbe.With(app.RequirePermission("read_merchant_billable_event")).
				Get(
					"/{merchantBillableEventID}",
					app.GetMerchantBillableEventByIDHandler,
				)
		})

		// Merchant Fee Calculations
		//
		// Privileged durable monetary-result history and reconciliation surface.
		// Creation and lifecycle mutation remain Commerce/service orchestration
		// responsibilities and are not exposed through HTTP.
		//
		// Merchant actors must not receive these permissions unless a future route
		// family has canonical ownership/delegation enforcement and an explicitly
		// approved merchant-facing requirement.
		v1.Route("/merchant-fee-calculations", func(mfc chi.Router) {
			mfc.Use(app.AuthMiddleware)

			mfc.With(app.RequirePermission("read_merchant_fee_calculation")).
				Get(
					"/by-billable-event/{billableEventID}/fee-type/{feeTypeID}/active",
					app.GetActiveMerchantFeeCalculationByBillableEventAndFeeTypeHandler,
				)

			mfc.With(app.RequirePermission("list_merchant_fee_calculations")).
				Get(
					"/by-billable-event/{billableEventID}",
					app.ListMerchantFeeCalculationsByBillableEventIDHandler,
				)

			mfc.With(app.RequirePermission("list_merchant_fee_calculations")).
				Get(
					"/by-merchant/{merchantID}/status",
					app.ListMerchantFeeCalculationsByMerchantAndStatusHandler,
				)

			mfc.With(app.RequirePermission("list_merchant_fee_calculations")).
				Get(
					"/by-merchant/{merchantID}",
					app.ListMerchantFeeCalculationsByMerchantHandler,
				)

			mfc.With(app.RequirePermission("read_merchant_fee_calculation")).
				Get(
					"/{merchantFeeCalculationID}",
					app.GetMerchantFeeCalculationByIDHandler,
				)
		})

		// Merchant Invoices
		//
		// Privileged durable commercial-obligation history. Invoice creation,
		// reconciliation, lifecycle mutation, overdue processing, and payment
		// application remain service/orchestration responsibilities.
		//
		// Merchant actors must not receive these permissions unless a future
		// merchant-facing route family has canonical ownership/delegation
		// enforcement and an explicitly approved requirement.
		v1.Route("/merchant-invoices", func(mi chi.Router) {
			mi.Use(app.AuthMiddleware)

			// Invoice numbers are not constrained to URI-segment-safe
			// characters, so lookup uses ?invoice_number=...
			mi.With(app.RequirePermission("read_merchant_invoice")).
				Get(
					"/by-number",
					app.GetMerchantInvoiceByInvoiceNumberHandler,
				)

			mi.With(app.RequirePermission("list_merchant_invoices")).
				Get(
					"/by-merchant/{merchantID}/future-offering/{futureOfferingID}",
					app.ListMerchantInvoicesByMerchantAndFutureOfferingHandler,
				)

			mi.With(app.RequirePermission("list_merchant_invoices")).
				Get(
					"/by-merchant/{merchantID}/status",
					app.ListMerchantInvoicesByMerchantAndStatusHandler,
				)

			mi.With(app.RequirePermission("list_merchant_invoices")).
				Get(
					"/by-merchant/{merchantID}",
					app.ListMerchantInvoicesByMerchantHandler,
				)

			mi.With(app.RequirePermission("read_merchant_invoice")).
				Get(
					"/{merchantInvoiceID}",
					app.GetMerchantInvoiceByIDHandler,
				)
		})

		// Merchant Invoice Items
		//
		// Privileged durable invoice-line history and reconciliation reads.
		// Draft-line insertion/removal and invoice-composition aggregation remain
		// service/orchestration responsibilities and are not exposed through HTTP.
		//
		// Merchant actors must not receive these permissions unless a future
		// merchant-facing route family has canonical ownership/delegation
		// enforcement and an explicitly approved requirement.
		v1.Route("/merchant-invoice-items", func(mii chi.Router) {
			mii.Use(app.AuthMiddleware)

			// Static routes must remain before the parameterized item-ID route.
			mii.With(app.RequirePermission("read_merchant_invoice_item")).
				Get(
					"/by-fee-calculation/{feeCalculationID}",
					app.GetMerchantInvoiceItemByFeeCalculationIDHandler,
				)

			mii.With(app.RequirePermission("list_merchant_invoice_items")).
				Get(
					"/by-invoice/{merchantInvoiceID}",
					app.ListMerchantInvoiceItemsByInvoiceIDHandler,
				)

			mii.With(app.RequirePermission("read_merchant_invoice_item")).
				Get(
					"/{merchantInvoiceItemID}",
					app.GetMerchantInvoiceItemByIDHandler,
				)
		})

		// Merchant Payment Methods
		v1.Route("/merchant-payment-methods", func(mpm chi.Router) {
			mpm.Use(app.AuthMiddleware)

			mpm.With(app.RequirePermission("create_merchant_payment_method")).
				Post("/", app.CreateMerchantPaymentMethodHandler)

			mpm.With(app.RequirePermission("read_default_merchant_payment_method")).
				Get("/default", app.GetDefaultMerchantPaymentMethodHandler)

			mpm.With(app.RequirePermission("list_merchant_payment_methods")).
				Get("/", app.ListMerchantPaymentMethodsHandler)

			mpm.With(app.RequirePermission("read_merchant_payment_method")).
				Get("/{merchantPaymentMethodID}", app.GetMerchantPaymentMethodByIDHandler)

			mpm.With(app.RequirePermission("update_merchant_payment_method")).
				Put("/{merchantPaymentMethodID}", app.UpdateMerchantPaymentMethodHandler)

			mpm.With(app.RequirePermission("set_default_merchant_payment_method")).
				Patch("/{merchantPaymentMethodID}/set-default", app.SetDefaultMerchantPaymentMethodHandler)

			mpm.With(app.RequirePermission("clear_default_merchant_payment_method")).
				Patch("/{merchantPaymentMethodID}/clear-default", app.ClearDefaultMerchantPaymentMethodHandler)

			mpm.With(app.RequirePermission("update_merchant_payment_method_status")).
				Patch("/{merchantPaymentMethodID}/status", app.UpdateMerchantPaymentMethodStatusHandler)

			mpm.With(app.RequirePermission("restore_merchant_payment_method")).
				Patch("/{merchantPaymentMethodID}/restore", app.RestoreMerchantPaymentMethodHandler)

			mpm.With(app.RequirePermission("soft_delete_merchant_payment_method")).
				Delete("/{merchantPaymentMethodID}", app.SoftDeleteMerchantPaymentMethodHandler)
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
			up.With(app.RequirePermission("read_reserved_handles")).
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
