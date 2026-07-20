// Package data provides the centralized database model aggregate for the
// Sagrenti backend.
//
// File: sdworkspace/sdbackend/internal/data/models.go
//
// GTM:
//   Layer: 2.1 Database / Governance Foundation
//   Release Class: SPINE
//   Reason:
//     Models is the canonical data-layer aggregate and release-governance map.
//     It wires the database pool, logger-backed model structs, Future Offering
//     v1 SPINE domains, minimal foundation domains, and DEFERRED domains into
//     the application surface.
//
// Future Offering v1 Doctrine:
//
//   Sagrenti v1 is not a deals platform.
//   Sagrenti v1 is a Future Offering anticipation platform.
//
//   SPINE means only what is required to let a merchant publish a Future
//   Offering and let consumers discover, watch, and express future intent
//   toward it.
//
// SPINE Rule:
//   Keep compiling.
//   Keep production-ready.
//   Preserve Models as the canonical data-layer aggregate.
//   Preserve SPINE vs DEFERRED governance annotations.
//   Preserve dbTimeout.
//   Preserve New() initialization consistency.
//   Block deployment if this file breaks build, model wiring,
//   data-layer availability, or Future Offering v1 governance integrity.
package data

import (
	"time"

	"github.com/PiccoloMondoC/sdworkspace/sdbackend/internal/logging"

	"github.com/jackc/pgx/v5/pgxpool"
)

// dbTimeout is the canonical timeout for ordinary data-layer model operations.
//
// Keep this timeout centralized in models.go so individual model files do not
// introduce local timeout constants. Startup database connection attempts use
// bootstrap.Config.DBTimeout through DBConnectionParamsModel.ConnectWithConnector;
// they must not be confused with this query/mutation timeout.
const dbTimeout = 10 * time.Second

// Models is the canonical data-layer aggregate exposed to the application.
//
// Any model declared here must also be initialized in New(). A missing New()
// initializer is a wiring defect because callers may reasonably expect every
// field on app.Models to be usable once the aggregate is constructed.
//
// GTM convention:
//   - SPINE: Future Offering v1 means release-critical to the wedge.
//   - SPINE: minimal foundation means required infrastructure/support for v1.
//   - DEFERRED means valid future domain; it may compile and remain wired,
//     but it must not drive v1 work, routes, UI expansion, or release blocking.
//
// This is convention-enforced. Do not use //go:build deferred for v1.
type Models struct {
	DB     *pgxpool.Pool
	Logger *logging.Logger

	// Layer 2.1 — Database / Governance Foundation / system configuration infrastructure
	DBConnectionParams DBConnectionParamsModel // SPINE: minimal foundation — database connection configuration
	AuditLog           AuditLogModel           // SPINE: minimal foundation — audit trail foundation
	EntityType         EntityTypeModel         // SPINE: minimal foundation — audit/entity lookup governance
	Action             ActionModel             // SPINE: minimal foundation — audit/action lookup governance
	PlatformSetting        PlatformSettingModel        // SPINE: minimal foundation — system configuration infrastructure
	PlatformSettingHistory PlatformSettingHistoryModel // SPINE: minimal foundation — immutable platform-setting value history

	// Layer 2.2 — Identity / Auth Domain
	User            UserModel            // SPINE: minimal foundation — user account identity
	ActivationToken ActivationTokenModel // SPINE: minimal foundation — account activation
	PasswordReset   PasswordResetModel   // SPINE: minimal foundation — password recovery
	Token           TokenModel           // SPINE: minimal foundation — refresh-token persistence and token revocation lifecycle
	Role            RoleModel            // SPINE: minimal foundation — role-based access control
	Permission      PermissionModel      // SPINE: minimal foundation — permission catalog
	RolePermission  RolePermissionModel  // SPINE: minimal foundation — role-permission mapping
	GlobalHandle    GlobalHandleModel    // SPINE: minimal foundation — scarce public identity namespace

	// Layer 2.2.a — External OAuth Login Consumption
	UserExternalIdentity UserExternalIdentityModel // SPINE: minimal foundation — external identity/account linking
	OauthLoginState      OAuthLoginStateModel      // SPINE: minimal foundation — external-login state/nonce tracking

	// Layer 2.2.b — OAuth Provider Domain
	OauthClient            OauthClientModel            // DEFERRED: OAuth2 client registry
	OauthAuthorizationCode OauthAuthorizationCodeModel // DEFERRED: OAuth2 authorization-code lifecycle
	OauthUserConsent       OauthUserConsentModel       // DEFERRED: OAuth2 user-consent lifecycle

	// Layer 2.3 — Consumer Domain
	UserProfile         UserProfileModel         // SPINE: minimal foundation — user profile
	//UserTrendEngagement UserTrendEngagementModel // SPINE: Future Offering v1 — consumer future-offering engagement / My Radar
	UserNotification    UserNotificationModel    // SPINE: minimal foundation — user notification record
	NotificationChannel NotificationChannelModel // SPINE: minimal foundation — notification channel lookup
	NotificationType    NotificationTypeModel    // SPINE: minimal foundation — notification type lookup

	UserFavorite        UserFavoriteModel        // DEFERRED: affinity / adoration / virality signal
	UserWishlist        UserWishlistModel        // DEFERRED: present-commerce buying-intent saved-offer record
	UserMerchantFollow  UserMerchantFollowModel  // DEFERRED: generic user-to-merchant follow record
	UserSettings        UserSettingsModel        // DEFERRED: user preference settings
	UserWallet          UserWalletModel          // DEFERRED: user wallet/account balance
	UserDashboard       UserDashboardModel       // DEFERRED: user dashboard
	UserDashboardReport UserDashboardReportModel // DEFERRED: dashboard reporting
	DashboardTemplate   DashboardTemplateModel   // DEFERRED: dashboard templates

	// Layer 2.4 — Merchant / Future Offering Domain
	Merchant            MerchantModel            // SPINE: Future Offering v1 — merchant identity
	MerchantAccount     MerchantAccountModel     // SPINE: merchant platform-account lifecycle
	MerchantType        MerchantTypeModel        // SPINE: minimal foundation — merchant classification lookup
	MerchantProgramPlan         MerchantProgramPlanModel        // SPINE: Future Offering v1 — merchant program plan reference table
	MerchantProgramFeeSchedule  MerchantProgramFeeScheduleModel // SPINE: Future Offering v1 — effective-dated merchant monetization policy
	MerchantProgramEntitlement  MerchantProgramEntitlementModel // SPINE: Future Offering v1 — merchant program entitlement/capability gate
	MerchantProgramSubscription MerchantProgramSubscriptionModel // SPINE: Future Offering v1 — merchant program subscription lifecycle
	MerchantProgramSubscriptionEvent MerchantProgramSubscriptionEventModel // SPINE: Future Offering v1 — append-only merchant program subscription lifecycle history

	//MerchantCenter MerchantCenterModel // DEFERRED: full merchant self-service workspace

	// Layer 2.4.a — Merchant / Affiliate Domain
	MerchantAffiliateProgram MerchantAffiliateProgramModel // DEFERRED: merchant affiliate-program mapping
	AffiliateProgram         AffiliateProgramModel         // DEFERRED: affiliate program catalog
	AffiliatePerformance     AffiliatePerformanceModel     // DEFERRED: affiliate performance reporting

	MerchantApplication       MerchantApplicationModel       // DEFERRED: merchant application workflow
	MerchantApplicationStatus MerchantApplicationStatusModel // DEFERRED: merchant application status lookup

	// Layer 2.5 — Minimal Catalog / Future Offering Classification
	Brand      BrandModel      // SPINE: minimal foundation — brand identity
	Department DepartmentModel // SPINE: minimal foundation — top-level taxonomy
	Category   CategoryModel   // SPINE: minimal foundation — future-offering taxonomy
	Product    ProductModel    // DEFERRED: present-commerce product catalog record
	Platform   PlatformModel   // DEFERRED: platform/source lookup

	// Layer 2.5.a — Present Commerce / Offers Domain
	Offer             OfferModel             // SPINE: Future Offering v1 — canonical future offering record
	OfferClick        OfferClickModel        // DEFERRED: present-commerce offer click tracking
	OfferFlag         OfferFlagModel         // DEFERRED: offer moderation flag
	OfferPriceHistory OfferPriceHistoryModel // DEFERRED: offer price-history record
	OfferStatus       OfferStatusModel       // DEFERRED: offer status lookup

	ProductMerchant        ProductMerchantModel        // DEFERRED: merchant-product mapping
	Coupon                 CouponModel                 // DEFERRED: coupon record
	CouponStatus           CouponStatusModel           // DEFERRED: coupon status lookup
	CouponUsage            CouponUsageModel            // DEFERRED: coupon usage tracking
	CouponPerformanceStats CouponPerformanceStatsModel // DEFERRED: coupon performance reporting
	OfferConversion        OfferConversionModel        // DEFERRED: offer conversion record
	//Promotion              PromotionModel              // DEFERRED: promotion record
	MerchantPromotion      MerchantPromotionModel      // DEFERRED: merchant promotion record
	OfferRating            OfferRatingModel            // DEFERRED: offer rating record
	OfferSponsorship       OfferSponsorshipModel       // DEFERRED: offer sponsorship record
	SponsorshipBidType     SponsorshipBidTypeModel     // DEFERRED: sponsorship bid-type lookup
	SponsorshipBidMinimum  SponsorshipBidMinimumModel  // DEFERRED: sponsorship bid minimum
}

// New creates an initialized Models aggregate.
//
// SPINE and DEFERRED models are both allowed to compile under the v1
// convention-enforced approach. DEFERRED means "do not expand, route, or block
// release on this domain," not "remove from compilation."
func New(dbPool *pgxpool.Pool, logger *logging.Logger) Models {
	return Models{
		DB:     dbPool, // SPINE: minimal foundation
		Logger: logger, // SPINE: minimal foundation

		// Layer 2.1 — Database / Governance Foundation
		DBConnectionParams: DBConnectionParamsModel{DB: dbPool, Logger: logger}, // SPINE: minimal foundation
		AuditLog:           AuditLogModel{DB: dbPool, Logger: logger},           // SPINE: minimal foundation
		EntityType:         EntityTypeModel{DB: dbPool, Logger: logger},         // SPINE: minimal foundation
		Action:             ActionModel{DB: dbPool, Logger: logger},             // SPINE: minimal foundation
		PlatformSetting:    PlatformSettingModel{DB: dbPool, Logger: logger},    // SPINE: minimal foundation
		PlatformSettingHistory: PlatformSettingHistoryModel{
			DB:     dbPool,
			Logger: logger,
		}, // SPINE: minimal foundation — immutable platform-setting value history

		// Layer 2.2 — Identity / Auth Domain
		User:            UserModel{DB: dbPool, Logger: logger},            // SPINE: minimal foundation
		ActivationToken: ActivationTokenModel{DB: dbPool, Logger: logger}, // SPINE: minimal foundation
		PasswordReset:   PasswordResetModel{DB: dbPool, Logger: logger},   // SPINE: minimal foundation
		Token:           TokenModel{DB: dbPool, Logger: logger},           // SPINE: minimal foundation
		Role:            RoleModel{DB: dbPool, Logger: logger},            // SPINE: minimal foundation
		Permission:      PermissionModel{DB: dbPool, Logger: logger},      // SPINE: minimal foundation
		RolePermission:  RolePermissionModel{DB: dbPool, Logger: logger},  // SPINE: minimal foundation
		GlobalHandle:    GlobalHandleModel{DB: dbPool, Logger: logger},    // SPINE: minimal foundation

		// Layer 2.2.a — External OAuth Login Consumption
		UserExternalIdentity: UserExternalIdentityModel{DB: dbPool, Logger: logger}, // SPINE
		OauthLoginState:      OAuthLoginStateModel{DB: dbPool, Logger: logger},      // SPINE

		// Layer 2.2.b — OAuth Provider Domain
		OauthClient:            OauthClientModel{DB: dbPool, Logger: logger},            // DEFERRED
		OauthAuthorizationCode: OauthAuthorizationCodeModel{DB: dbPool, Logger: logger}, // DEFERRED
		OauthUserConsent:       OauthUserConsentModel{DB: dbPool, Logger: logger},       // DEFERRED

		// Layer 2.3 — Consumer Domain
		UserProfile:         UserProfileModel{DB: dbPool, Logger: logger},         // SPINE: minimal foundation
		//UserTrendEngagement: UserTrendEngagementModel{DB: dbPool, Logger: logger}, // SPINE: Future Offering v1
		UserNotification:    UserNotificationModel{DB: dbPool, Logger: logger},    // SPINE: minimal foundation
		NotificationChannel: NotificationChannelModel{DB: dbPool, Logger: logger}, // SPINE: minimal foundation
		NotificationType:    NotificationTypeModel{DB: dbPool, Logger: logger},    // SPINE: minimal foundation

		UserFavorite:        UserFavoriteModel{DB: dbPool, Logger: logger},        // DEFERRED
		UserWishlist:        UserWishlistModel{DB: dbPool, Logger: logger},        // DEFERRED
		UserMerchantFollow:  UserMerchantFollowModel{DB: dbPool, Logger: logger},  // DEFERRED
		UserSettings:        UserSettingsModel{DB: dbPool, Logger: logger},        // DEFERRED
		UserWallet:          UserWalletModel{DB: dbPool, Logger: logger},          // DEFERRED
		UserDashboard:       UserDashboardModel{DB: dbPool, Logger: logger},       // DEFERRED
		UserDashboardReport: UserDashboardReportModel{DB: dbPool, Logger: logger}, // DEFERRED
		DashboardTemplate:   DashboardTemplateModel{DB: dbPool, Logger: logger},   // DEFERRED

		// Layer 2.4 — Merchant / Future Offering Domain
		Merchant:              MerchantModel{DB: dbPool, Logger: logger},              // SPINE: Future Offering v1
		MerchantAccount:       MerchantAccountModel{DB: dbPool, Logger: logger},
		MerchantType:          MerchantTypeModel{DB: dbPool, Logger: logger},          // SPINE: minimal foundation
		MerchantProgramPlan:   MerchantProgramPlanModel{DB: dbPool, Logger: logger},   // SPINE: Future Offering v1
		MerchantProgramFeeSchedule: MerchantProgramFeeScheduleModel{DB: dbPool, Logger: logger}, // SPINE: Future Offering v1
		MerchantProgramEntitlement:  MerchantProgramEntitlementModel{DB: dbPool, Logger: logger}, // SPINE: Future Offering v1
		MerchantProgramSubscription: MerchantProgramSubscriptionModel{DB: dbPool, Logger: logger}, // SPINE: Future Offering v1
		MerchantProgramSubscriptionEvent: MerchantProgramSubscriptionEventModel{DB: dbPool, Logger: logger}, // SPINE: Future Offering v1

		//MerchantCenter: MerchantCenterModel{DB: dbPool, Logger: logger}, // DEFERRED

		// Layer 2.4.a — Merchant / Affiliate Domain
		MerchantAffiliateProgram: MerchantAffiliateProgramModel{DB: dbPool, Logger: logger}, // DEFERRED
		AffiliateProgram:         AffiliateProgramModel{DB: dbPool, Logger: logger},         // DEFERRED
		AffiliatePerformance:     AffiliatePerformanceModel{DB: dbPool, Logger: logger},     // DEFERRED

		MerchantApplication:       MerchantApplicationModel{DB: dbPool, Logger: logger},       // DEFERRED
		MerchantApplicationStatus: MerchantApplicationStatusModel{DB: dbPool, Logger: logger}, // DEFERRED

		// Layer 2.5 — Minimal Catalog / Future Offering Classification
		Brand:      BrandModel{DB: dbPool, Logger: logger},      // SPINE: minimal foundation
		Department: DepartmentModel{DB: dbPool, Logger: logger}, // SPINE: minimal foundation
		Category:   CategoryModel{DB: dbPool, Logger: logger},   // SPINE: minimal foundation
		Product:    ProductModel{DB: dbPool, Logger: logger},    // DEFERRED
		Platform:   PlatformModel{DB: dbPool, Logger: logger},   // DEFERRED

		// Layer 2.5.a — Present Commerce / Offers Domain
		Offer:             OfferModel{DB: dbPool, Logger: logger},             // SPINE
		OfferClick:        OfferClickModel{DB: dbPool, Logger: logger},        // DEFERRED
		OfferFlag:         OfferFlagModel{DB: dbPool, Logger: logger},         // DEFERRED
		OfferPriceHistory: OfferPriceHistoryModel{DB: dbPool, Logger: logger}, // DEFERRED
		OfferStatus:       OfferStatusModel{DB: dbPool, Logger: logger},       // DEFERRED

		ProductMerchant:        ProductMerchantModel{DB: dbPool, Logger: logger},        // DEFERRED
		Coupon:                 CouponModel{DB: dbPool, Logger: logger},                 // DEFERRED
		CouponStatus:           CouponStatusModel{DB: dbPool, Logger: logger},           // DEFERRED
		CouponUsage:            CouponUsageModel{DB: dbPool, Logger: logger},            // DEFERRED
		CouponPerformanceStats: CouponPerformanceStatsModel{DB: dbPool, Logger: logger}, // DEFERRED
		OfferConversion:        OfferConversionModel{DB: dbPool, Logger: logger},        // DEFERRED
		//Promotion:              PromotionModel{DB: dbPool, Logger: logger},              // DEFERRED
		MerchantPromotion:      MerchantPromotionModel{DB: dbPool, Logger: logger},      // DEFERRED
		OfferRating:            OfferRatingModel{DB: dbPool, Logger: logger},            // DEFERRED
		OfferSponsorship:       OfferSponsorshipModel{DB: dbPool, Logger: logger},       // DEFERRED
		SponsorshipBidType:     SponsorshipBidTypeModel{DB: dbPool, Logger: logger},     // DEFERRED
		SponsorshipBidMinimum:  SponsorshipBidMinimumModel{DB: dbPool, Logger: logger},  // DEFERRED
	}
}