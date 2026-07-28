// Package data provides models and database access methods for seed data
// operations and other entities.
//
// sdworkspace/sdbackend/internal/data/dataseed.go
//
// GTM:
//
//	Layer: 2.1 Database / Governance Foundation
//	Release Class: SPINE
//	Reason:
//	  Dataseeding is release-critical foundation infrastructure. This file
//	  establishes required seed/reference data for roles, permissions, role
//	  mappings, audit metadata, statuses, lookup vocabularies, affiliate
//	  programs, merchant/catalog foundations, and initial offer data needed for
//	  the application to operate correctly.
//
// SPINE Rule:
//
//	Keep compiling.
//	Keep production-ready.
//	Preserve idempotent seed behavior.
//	Preserve role/permission governance.
//	Preserve audit action/entity metadata.
//	Preserve required lookup/reference data.
//	Preserve seed ordering dependencies.
//	Block deployment if this file breaks build, authorization setup,
//	audit metadata resolution, catalog bootstrap, or foundational seed integrity.
package data

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// OAuthClientSeedSecrets carries bootstrap-validated OAuth client secrets into
// the data seed layer. The seed layer must not read environment variables
// directly because bootstrap owns startup configuration loading and validation.
type OAuthClientSeedSecrets struct {
	WebClientSecret    string
	MobileClientSecret string
}

const (
	// ---------------------------------------------------------------
	// role_permissions
	//
	// Design rationale per role:
	//
	// admin            — all permissions (CROSS JOIN)
	// editor           — full offer/coupon/product/category read+write,
	//                    no user management, no role assignment, no audit admin
	// OfferCurator     — read and curate offers, flag, approve/reject
	// QualityModerator — flag offers/coupons, read flagged content
	// CampaignManager  — sponsorship and merchant promotion full access
	// viewer           — read-only across offers, merchants, categories, products
	// customer         — public-facing actions, own profile/favorites/wallet
	// merchant         — own listings, sponsorships, promotions, performance
	// ---------------------------------------------------------------
	insertRolePermissionsQuery = `
	INSERT INTO role_permissions (role_id, permission_id)
	
	-- admin: all permissions
	SELECT r.id, p.id
	FROM roles r
	CROSS JOIN permissions p
	WHERE r.name = 'admin'
	
	UNION ALL
	
	-- editor
	SELECT r.id, p.id
	FROM roles r
	JOIN permissions p ON p.name IN (
		'create_offer','read_offer','read_all_offers','read_live_offers',
		'update_offer','soft_delete_offer','expire_offer','flag_offer',
		'read_flagged_offers','read_popular_offers','read_recent_offers',
		'read_top_rated_offers','read_trending_offers','reject_offer',
		'report_expired_offer','review_pending_offers','submit_feedback_on_offer',
		'vote_on_offer','blacklist_offer','read_offer_status','read_offer_statuses',
		'submit_offer_status_for_review',
		'approve_curated_offer','reject_curated_offer','review_curated_offers',
		'create_coupon','read_coupon','read_active_coupons','update_coupon',
		'soft_delete_coupon','read_flagged_coupons','read_popular_coupons',
		'create_offer_price_history','read_offer_price_history',
		'read_offer_price_history_by_offer','read_offer_price_trend',
		'update_offer_price_history','read_significant_price_drops',
		'read_offer_rating','read_offer_ratings','read_offer_rating_count',
		'read_offer_rating_analytics','update_offer_rating','delete_offer_rating',
		'create_department','read_department','list_departments','update_department','soft_delete_department',
		'create_category','read_category','list_categories','update_category','soft_delete_category',
		'create_product','read_product','read_all_products','update_product','soft_delete_product',
		'create_brand','read_brand','read_brands','update_brand','soft_delete_brand',
		'read_merchant','list_merchants','delete_merchant',
		'create_promotion','read_promotion','update_promotion','soft_delete_promotion',
		'create_merchant_promotion','read_merchant_promotion','read_merchant_promotions',
		'update_merchant_promotion','soft_delete_merchant_promotion',
		'read_active_merchant_promotions','read_upcoming_merchant_promotions',
		'read_expired_merchant_promotions','read_storewide_merchant_promotion',
		'read_audit_log','read_archived_audit_log','read_entity_type','read_action',
		'read_user_dashboard','read_user_dashboards','read_dashboard_reports',
		'generate_curation_reports','view_admin_dashboard_stats'
	)
	WHERE r.name = 'editor'
	
	UNION ALL
	
	-- OfferCurator
	SELECT r.id, p.id
	FROM roles r
	JOIN permissions p ON p.name IN (
		'read_offer','read_all_offers','read_live_offers','read_recent_offers',
		'read_popular_offers','read_flagged_offers','read_top_rated_offers',
		'read_trending_offers','flag_offer','approve_curated_offer',
		'reject_curated_offer','review_curated_offers','submit_feedback_on_offer',
		'vote_on_offer','create_offer_rating','read_offer_rating','read_offer_ratings',
		'read_offer_rating_count','read_category','read_merchant','list_merchants',
		'read_offer_price_history','read_offer_price_history_by_offer',
		'read_offer_price_trend','read_significant_price_drops'
	)
	WHERE r.name = 'OfferCurator'
	
	UNION ALL
	
	-- QualityModerator
	SELECT r.id, p.id
	FROM roles r
	JOIN permissions p ON p.name IN (
		'read_offer','read_live_offers','read_flagged_offers','flag_offer',
		'report_expired_offer','read_coupon','read_flagged_coupons','flag_coupon',
		'read_category','read_merchant','list_merchants',
		'read_offer_price_history','flag_offer_price_anomaly',
		'flag_offer_rating_review','read_offer_rating','read_offer_ratings'
	)
	WHERE r.name = 'QualityModerator'
	
	UNION ALL
	
	-- CampaignManager
	SELECT r.id, p.id
	FROM roles r
	JOIN permissions p ON p.name IN (
		'sponsor_offer','create_offer_sponsorship','read_offer_sponsorship',
		'read_offer_sponsorships','update_offer_sponsorship',
		'create_merchant_promotion','read_merchant_promotion','read_merchant_promotions',
		'update_merchant_promotion','soft_delete_merchant_promotion',
		'delete_merchant_promotion','bulk_delete_merchant_promotions',
		'bulk_soft_delete_merchant_promotions','search_merchant_promotions',
		'create_or_update_merchant_promotion','extend_merchant_promotion_dates',
		'read_active_merchant_promotions','read_upcoming_merchant_promotions',
		'read_expired_merchant_promotions','read_storewide_merchant_promotion',
		'create_promotion','read_promotion','update_promotion',
		'read_offer','read_live_offers','read_all_offers','read_popular_offers',
		'read_trending_offers','read_top_rated_offers',
		'read_merchant','list_merchants',
		'read_affiliate_performance','list_affiliate_performance',
		'read_dashboard_reports','generate_curation_reports'
	)
	WHERE r.name = 'CampaignManager'
	
	UNION ALL
	
	-- viewer
	SELECT r.id, p.id
	FROM roles r
	JOIN permissions p ON p.name IN (
		'read_offer','read_live_offers','read_all_offers','read_popular_offers',
		'read_recent_offers','read_top_rated_offers','read_trending_offers',
		'read_offer_rating','read_offer_ratings','read_offer_rating_count',
		'read_category','read_merchant','list_merchants',
		'read_product','read_all_products','read_brand','read_brands',
		'read_coupon','read_active_coupons','read_popular_coupons',
		'read_affiliate_performance','list_affiliate_performance',
		'read_audit_log','read_archived_audit_log'
	)
	WHERE r.name = 'viewer'
	
	UNION ALL
	
	-- customer
	SELECT r.id, p.id
	FROM roles r
	JOIN permissions p ON p.name IN (
		'read_live_offers','read_offer','read_popular_offers','read_recent_offers',
		'read_top_rated_offers','read_trending_offers',
		'read_coupon','read_active_coupons','read_popular_coupons','clip_coupon',
		'track_offer_click','subscribe_price_drop','vote_on_offer',
		'submit_feedback_on_offer','report_expired_offer','flag_offer',
		'create_offer_rating','read_offer_rating','read_offer_ratings',
		'read_offer_rating_count','read_user_offer_rating','update_offer_rating',
		'create_user_favorite','read_user_favorites','read_user_favorites_count',
		'save_user_favorite','unsave_user_favorite','remove_user_favorite',
		'set_offer_alert','share_offer','share_favorite_list',
		'read_most_favorited_offers','read_recently_favorited_offer',
		'read_personalized_offers','read_trending_offers_for_user',
		'follow_merchant','unfollow_merchant','read_followed_merchants_offers',
		'recommend_offers','request_restock_notification',
		'read_user_notification','read_user_notifications','soft_delete_user_notification',
		'create_user_profile','read_user_profile','update_user_profile',
		'soft_delete_user_profile',
		'save_user_settings','read_user_settings','update_user_settings',
		'soft_delete_user_settings',
		'read_user_wallet',
		'create_user_dashboard','read_user_dashboard','update_user_dashboard',
		'read_user_dashboard_widgets','save_user_dashboard_preferences',
		'delete_own_account'
	)
	WHERE r.name = 'customer'
	
	UNION ALL
	
	-- merchant
	SELECT r.id, p.id
	FROM roles r
	JOIN permissions p ON p.name IN (
		'create_offer','read_offer','read_all_offers','read_live_offers',
		'update_offer','soft_delete_offer','expire_offer',
		'submit_offer_status_for_review',
		'create_coupon','read_coupon','update_coupon','soft_delete_coupon',
		'sponsor_offer','create_offer_sponsorship','read_offer_sponsorship',
		'read_offer_sponsorships','update_offer_sponsorship',
		'create_merchant_promotion','read_merchant_promotion','read_merchant_promotions',
		'update_merchant_promotion','soft_delete_merchant_promotion',
		'read_active_merchant_promotions','read_storewide_merchant_promotion',
		'create_or_update_merchant_promotion',
		'read_affiliate_program','list_affiliate_programs',
		'create_merchant_application','read_merchant_application',
		'read_affiliate_performance','list_affiliate_performance',
		'read_merchant','update_merchant',
		'create_user_dashboard','read_user_dashboard','update_user_dashboard',
		'read_user_dashboard_widgets','save_user_dashboard_preferences',
		'read_user_wallet',
		'read_user_settings','save_user_settings','update_user_settings',
		'read_current_merchant_program_subscription',
		'read_active_merchant_program_subscription',
		'read_merchant_program_subscriptions_by_merchant',

		'create_merchant_payment_method',
		'read_merchant_payment_method',
		'read_default_merchant_payment_method',
		'list_merchant_payment_methods',
		'update_merchant_payment_method',
		'set_default_merchant_payment_method',
		'clear_default_merchant_payment_method',
		'update_merchant_payment_method_status',
		'soft_delete_merchant_payment_method',
		'restore_merchant_payment_method',
		
		'delete_own_account'
	)
	WHERE r.name = 'merchant'
	
	ON CONFLICT (role_id, permission_id) DO NOTHING;
	`

	// ---------------------------------------------------------------
	// oauth_clients
	//
	// OAuth client seed secrets are supplied by bootstrap after startup
	// configuration validation. Plaintext client secrets must not be embedded
	// in source, SQL literals, logs, traces, metrics, audit payloads, or public
	// JSON. This query accepts secrets only as bind parameters and stores only
	// pgcrypto-derived password hashes.
	// ---------------------------------------------------------------
	insertOAuthClientQuery = `
	INSERT INTO oauth_clients (
		client_id,
		client_secret_hash,
		is_active,
		allowed_redirect_uris
	) VALUES
		(
			'sd-web-client',
			crypt($1, gen_salt('bf', 10)),
			TRUE,
			ARRAY[
				'http://localhost:4200/auth/callback',
				'https://sagrenti.com/auth/callback'
			]
		),
		(
			'sd-mobile-client',
			crypt($2, gen_salt('bf', 10)),
			TRUE,
			ARRAY[
				'sagrenti://auth/callback'
			]
		)
	ON CONFLICT (client_id) DO NOTHING;
	`

	insertMarketSegmentQuery = `
	INSERT INTO market_segments (name, description) VALUES
		('Mass Market',       'Broadly available products targeting the general consumer population.'),
		('Premium',           'Higher-priced products positioned on quality, design, or brand prestige.'),
		('Luxury',            'Top-tier products with aspirational pricing and exclusivity.'),
		('Budget',            'Entry-level or value-oriented products prioritising affordability.'),
		('Professional',      'Products designed for business, trade, or specialist professional use.'),
		('Kids & Family',     'Products specifically designed or marketed for children and family use.'),
		('Eco & Sustainable', 'Products positioned on environmental responsibility or sustainable sourcing.')
	ON CONFLICT (name) DO NOTHING;
	`

	insertAffiliateProgramQuery = `
	INSERT INTO affiliate_programs (name, website, api_auth_method) VALUES
		('Amazon Associates',   'https://affiliate-program.amazon.com', 'None'),
		('ShareASale',          'https://www.shareasale.com',           'None'),
		('CJ Affiliate',        'https://www.cj.com',                   'None'),
		('Rakuten Advertising', 'https://rakutenadvertising.com',       'None'),
		('Impact',              'https://impact.com',                   'None'),
		('Awin',                'https://www.awin.com',                 'None'),
		('eBay Partner Network','https://partnernetwork.ebay.com',      'None'),
		('Walmart Affiliates',  'https://affiliates.walmart.com',       'None')
	ON CONFLICT (name) DO NOTHING;
	`

	// Amounts are in USD — adjust before launch per your pricing model.
	insertSponsorshipBidMinimumsQuery = `
	INSERT INTO sponsorship_bid_minimums (bid_type_id, min_bid_amount, min_max_budget)
	SELECT sbt.id, v.min_bid, v.min_budget
	FROM (VALUES
		('CPD', 5.00::NUMERIC(19,4),  50.00::NUMERIC(19,4)),
		('CPC', 0.10::NUMERIC(19,4),  25.00::NUMERIC(19,4)),
		('CPI', 0.01::NUMERIC(19,4),  10.00::NUMERIC(19,4))
	) AS v(code, min_bid, min_budget)
	JOIN sponsorship_bid_types sbt ON sbt.code = v.code
	ON CONFLICT (bid_type_id) DO NOTHING;
	`

	insertValueTagQuery = `
	INSERT INTO value_tags (slug, name, description) VALUES
		('best-seller',      'Best Seller',      'One of the top-selling items in its category.'),
		('price-drop',       'Price Drop',       'The price has recently decreased.'),
		('limited-stock',    'Limited Stock',    'Few units remaining — creates urgency.'),
		('free-shipping',    'Free Shipping',    'Qualifies for free delivery.'),
		('bundle-deal',      'Bundle Deal',      'Includes multiple items or accessories at a combined price.'),
		('exclusive',        'Exclusive',        'Only available through this platform or a specific channel.'),
		('clearance',        'Clearance',        'Discounted to clear existing inventory.'),
		('new-arrival',      'New Arrival',      'Recently added or newly launched product.'),
		('editor-pick',      'Editor Pick',      'Manually selected by the editorial team for quality or value.'),
		('trending',         'Trending',         'Currently popular based on clicks and engagement.'),
		('eco-friendly',     'Eco-Friendly',     'Made with sustainable materials or environmentally responsible practices.'),
		('flash-deal',       'Flash Deal',       'Steeply discounted for a very short window, typically under 72 hours.'),
		('member-exclusive', 'Member Exclusive', 'Accessible only to signed-in or loyalty members.')
	ON CONFLICT (slug) DO NOTHING;
	`

	insertAudienceQuery = `
	INSERT INTO audiences (slug, name, description) VALUES
		('general',              'General',              'No specific audience restriction — suitable for all shoppers.'),
		('students',             'Students',             'College and university students seeking affordable options.'),
		('professionals',        'Professionals',        'Working adults with purchasing power and specific work-related needs.'),
		('parents-and-families', 'Parents & Families',   'Shoppers buying for children, dependents, or managing a household.'),
		('gamers',               'Gamers',               'Enthusiasts of video games, consoles, and gaming accessories.'),
		('fitness-enthusiasts',  'Fitness Enthusiasts',  'People actively pursuing health, exercise, or sport goals.'),
		('tech-enthusiasts',     'Tech Enthusiasts',     'Early adopters and hobbyists interested in technology products.'),
		('home-decorators',      'Home Decorators',      'Shoppers focused on interior design, furniture, and home goods.'),
		('travellers',           'Travellers',           'Frequent or occasional travellers seeking deals on flights, hotels, and gear.'),
		('budget-shoppers',      'Budget Shoppers',      'Price-sensitive consumers prioritising value over brand or features.'),
		('eco-conscious',        'Eco-Conscious',        'Shoppers who prioritise sustainability and ethical sourcing.'),
		('seniors',              'Seniors',              'Older adults with preferences for accessibility and trusted brands.'),
		('millennials',          'Millennials',          'Adults broadly in the millennial age cohort, often balancing value, convenience, family, and lifestyle-driven purchasing decisions.'),
		('generation-z',         'Generation Z',         'Younger consumers with strong digital habits, often drawn to social trends, affordability, personal identity, and fast-moving product categories.'),
		('tech-savvy-homeowners','Tech Savvy Homeowners','Homeowners comfortable with digital tools and smart technology, often interested in connected devices, home improvement, and efficiency-focused products.')
	ON CONFLICT (slug) DO NOTHING;
	`

	insertSeasonalRelevanceQuery = `
	INSERT INTO seasonal_relevances (slug, name, description)
	VALUES
		('spring', 'Spring', 'Relevant to the spring season.'),
		('summer', 'Summer', 'Relevant to the summer season.'),
		('fall', 'Fall', 'Relevant to the fall or autumn season.'),
		('winter', 'Winter', 'Relevant to the winter season.'),
		('holiday', 'Holiday', 'Relevant to general holiday shopping periods.'),
		('back-to-school', 'Back to School', 'Relevant to school preparation and student shopping periods.'),
		('black-friday', 'Black Friday', 'Relevant to Black Friday promotions and shopping activity.'),
		('cyber-monday', 'Cyber Monday', 'Relevant to Cyber Monday promotions and online shopping activity.'),
		('christmas', 'Christmas', 'Relevant to Christmas shopping and gifting.'),
		('new-year', 'New Year', 'Relevant to New Year promotions, goals, and seasonal buying behavior.'),
		('valentines-day', 'Valentine''s Day', 'Relevant to Valentine''s Day gifting and seasonal shopping.'),
		('easter', 'Easter', 'Relevant to Easter-related shopping and promotions.'),
		('mothers-day', 'Mother''s Day', 'Relevant to Mother''s Day gifting and promotions.'),
		('fathers-day', 'Father''s Day', 'Relevant to Father''s Day gifting and promotions.'),
		('halloween', 'Halloween', 'Relevant to Halloween shopping and seasonal promotions.')
	ON CONFLICT (slug) DO NOTHING;
	`

	insertPlatformQuery = `
	INSERT INTO platforms (name, description, website) VALUES
		('Shopify',    'E-commerce platform for independent and mid-market merchants.',        'https://www.shopify.com'),
		('Etsy',       'Marketplace for handmade, vintage, and creative goods.',               'https://www.etsy.com'),
		('WooCommerce','Open-source e-commerce plugin for WordPress merchants.',               'https://woocommerce.com'),
		('BigCommerce','SaaS e-commerce platform for growing and enterprise merchants.',       'https://www.bigcommerce.com'),
		('Squarespace','Website builder with integrated e-commerce capabilities.',             'https://www.squarespace.com'),
		('Wix',        'Website builder with e-commerce and booking features.',                'https://www.wix.com'),
		('Amazon',     'Amazon seller platform covering first-party and third-party merchants.','https://sell.amazon.com'),
		('eBay',       'Global online marketplace for new and used goods.',                    'https://www.ebay.com')
	ON CONFLICT (name) DO NOTHING;
	`

	insertBrandQuery = `
	INSERT INTO brands (name) VALUES
		('Apple'),('Samsung'),('Sony'),('LG'),('Microsoft'),
		('Dell'),('HP'),('Lenovo'),('Asus'),('Acer'),
		('Google'),('Amazon'),('Anker'),('JBL'),('Bose'),
		('Nike'),('Adidas'),('Under Armour'),('Levi''s'),('The North Face'),
		('Nintendo'),('Instant Pot'),('KitchenAid'),('Dyson'),('Philips')
	ON CONFLICT (name) DO NOTHING;
	`

	insertSocialPlatformQuery = `
	INSERT INTO social_platforms (name, base_url, description) VALUES
		('Instagram', 'https://www.instagram.com/',  'Photo and video sharing social network.'),
		('X',         'https://x.com/',              'Short-form public microblogging platform, formerly Twitter.'),
		('Facebook',  'https://www.facebook.com/',   'Social networking platform for personal and business pages.'),
		('LinkedIn',  'https://www.linkedin.com/in/','Professional networking and career platform.'),
		('TikTok',    'https://www.tiktok.com/@',    'Short-form video sharing platform.'),
		('YouTube',   'https://www.youtube.com/@',   'Video hosting and streaming platform.'),
		('Pinterest', 'https://www.pinterest.com/',  'Visual discovery and idea-sharing platform.'),
		('Snapchat',  'https://www.snapchat.com/add/','Ephemeral photo and video messaging platform.'),
		('Threads',   'https://www.threads.net/@',   'Text-based social network by Meta.'),
		('Reddit',    'https://www.reddit.com/user/','Community-driven discussion and content aggregation platform.')
	ON CONFLICT (name) DO NOTHING;
	`

	insertDashboardTemplateQuery = `
	INSERT INTO dashboard_templates (name, description, layout, widgets, filters) VALUES
		(
			'Customer Default',
			'Standard starting dashboard for newly registered customers.',
			'{"columns": 2, "density": "comfortable"}'::jsonb,
			'[{"type": "recent_offers", "title": "Recent Offers"}, {"type": "saved_favorites", "title": "My Favorites"}, {"type": "price_drop_alerts", "title": "Price Drop Alerts"}]'::jsonb,
			'{}'::jsonb
		),
		(
			'Merchant Default',
			'Standard starting dashboard for approved merchant accounts.',
			'{"columns": 2, "density": "comfortable"}'::jsonb,
			'[{"type": "offer_performance", "title": "Offer Performance"}, {"type": "active_sponsorships", "title": "Active Sponsorships"}, {"type": "click_summary", "title": "Click Summary"}]'::jsonb,
			'{}'::jsonb
		),
		(
			'Admin Overview',
			'High-level platform overview for internal admin users.',
			'{"columns": 3, "density": "compact"}'::jsonb,
			'[{"type": "platform_stats", "title": "Platform Stats"}, {"type": "pending_offers", "title": "Pending Offers"}, {"type": "flagged_content", "title": "Flagged Content"}, {"type": "merchant_applications", "title": "Merchant Applications"}, {"type": "recent_audit_logs", "title": "Audit Log"}]'::jsonb,
			'{}'::jsonb
		)
	ON CONFLICT (name) DO NOTHING;
	`
	insertOfferDataQuery = `
	INSERT INTO offers (
	offer_key, type, title, image_url, affiliate_url, price, list_price, currency,
	merchant_id, product_id, category_id, is_editorial_approved, status_id,
	created_at, updated_at, expires_at, coupon_code, deleted_at, is_active, published_at
	)
	VALUES
	(
		'women-waffle-knit-henley-top-deal',
		'deal',
		'Women Waffle Knit Tops Henley Shirts Long Sleeve V Neck Solid Color Casual Tunic',
		'https://m.media-amazon.com/images/I/71YSVo6avLL._AC_SY879_.jpg',
		'https://amzn.to/3UjdiJJ',
		26.34,
		30.99,
		'USD',
		(SELECT id FROM merchants WHERE name = 'Amazon' LIMIT 1),
		NULL,
		(SELECT id FROM categories WHERE slug = 'women-clothing-tops' LIMIT 1),
		TRUE,
		(SELECT id FROM offer_statuses WHERE name = 'approved' LIMIT 1),
		now(),
		now(),
		now() + interval '14 days',
		NULL,
		NULL,
		TRUE,
		now()
	),
	(
		'women-summer-chiffon-blouse-deal',
		'deal',
		'Women''s Summer Tops Short Sleeve Casual Shirts V Neck Chiffon Dressy Blouse Tops',
		'https://m.media-amazon.com/images/I/719HV+rwtRL._AC_SX679_.jpg',
		'https://amzn.to/4oIMGQi',
		9.99,
		23.99,
		'USD',
		(SELECT id FROM merchants WHERE name = 'Amazon' LIMIT 1),
		NULL,
		(SELECT id FROM categories WHERE slug = 'women-clothing-tops' LIMIT 1),
		TRUE,
		(SELECT id FROM offer_statuses WHERE name = 'approved' LIMIT 1),
		now(),
		now(),
		now() + interval '14 days',
		NULL,
		NULL,
		TRUE,
		now()
	),
	(
		'airpods-pro-2-wireless-earbuds-deal',
		'deal',
		'AirPods Pro 2 Wireless Earbuds',
		'https://m.media-amazon.com/images/I/61SUj2aKoEL._AC_SX679_.jpg',
		'https://amzn.to/40QHDmB',
		169.00,
		249.00,
		'USD',
		(SELECT id FROM merchants WHERE name = 'Amazon' LIMIT 1),
		NULL,
		(SELECT id FROM categories WHERE slug = 'cell-phones-accessories' LIMIT 1),
		TRUE,
		(SELECT id FROM offer_statuses WHERE name = 'approved' LIMIT 1),
		now(),
		now(),
		now() + interval '14 days',
		NULL,
		NULL,
		TRUE,
		now()
	),
	(
		'anker-usb-c-charger-block-20w-nano-pro-deal',
		'deal',
		'Anker USB-C Charger Block 20W (Nano Pro)',
		'https://m.media-amazon.com/images/I/61drNXi5kFL._AC_SX679_.jpg',
		'https://amzn.to/4lY62yN',
		13.99,
		19.99,
		'USD',
		(SELECT id FROM merchants WHERE name = 'Amazon' LIMIT 1),
		NULL,
		(SELECT id FROM categories WHERE slug = 'cell-phones-accessories' LIMIT 1),
		TRUE,
		(SELECT id FROM offer_statuses WHERE name = 'approved' LIMIT 1),
		now(),
		now(),
		now() + interval '14 days',
		NULL,
		NULL,
		TRUE,
		now()
	),
	(
		'amazon-fire-tv-stick-4k-deal',
		'deal',
		'Amazon Fire TV Stick 4K',
		'https://m.media-amazon.com/images/I/61XGGd7lh+L._AC_SX569_.jpg',
		'https://amzn.to/4l93YmF',
		29.99,
		49.99,
		'USD',
		(SELECT id FROM merchants WHERE name = 'Amazon' LIMIT 1),
		NULL,
		(SELECT id FROM categories WHERE slug = 'tv-video-home-audio' LIMIT 1),
		TRUE,
		(SELECT id FROM offer_statuses WHERE name = 'approved' LIMIT 1),
		now(),
		now(),
		now() + interval '14 days',
		NULL,
		NULL,
		TRUE,
		now()
	),
	(
		'apple-2025-macbook-air-13-inch-m4-deal',
		'deal',
		'Apple 2025 MacBook Air 13-inch Laptop with M4 chip',
		'https://m.media-amazon.com/images/I/71cWZUr9SVL._AC_SX679_.jpg',
		'https://amzn.to/4lbSDSy',
		799.00,
		999.00,
		'USD',
		(SELECT id FROM merchants WHERE name = 'Amazon' LIMIT 1),
		NULL,
		(SELECT id FROM categories WHERE slug = 'laptops' LIMIT 1),
		TRUE,
		(SELECT id FROM offer_statuses WHERE name = 'approved' LIMIT 1),
		now(),
		now(),
		now() + interval '14 days',
		NULL,
		NULL,
		TRUE,
		now()
	),
	(
		'samsung-galaxy-s25-deal',
		'deal',
		'Samsung Galaxy S25',
		'https://m.media-amazon.com/images/I/61AeV7RJgYL._AC_SX679_.jpg',
		'https://amzn.to/4lULAPq',
		779.99,
		859.99,
		'USD',
		(SELECT id FROM merchants WHERE name = 'Amazon' LIMIT 1),
		NULL,
		(SELECT id FROM categories WHERE slug = 'cell-phones-accessories' LIMIT 1),
		TRUE,
		(SELECT id FROM offer_statuses WHERE name = 'approved' LIMIT 1),
		now(),
		now(),
		now() + interval '14 days',
		NULL,
		NULL,
		TRUE,
		now()
	),
	(
		'nintendo-switch-oled-deal',
		'deal',
		'Nintendo Switch OLED',
		'https://m.media-amazon.com/images/I/61nqNujSF2L._SX522_.jpg',
		'https://amzn.to/40QK74p',
		339.00,
		399.99,
		'USD',
		(SELECT id FROM merchants WHERE name = 'Amazon' LIMIT 1),
		NULL,
		(SELECT id FROM categories WHERE slug = 'video-games-accessories' LIMIT 1),
		TRUE,
		(SELECT id FROM offer_statuses WHERE name = 'approved' LIMIT 1),
		now(),
		now(),
		now() + interval '14 days',
		NULL,
		NULL,
		TRUE,
		now()
	),
	(
		'instant-pot-duo-7-in-1-6-quart-deal',
		'deal',
		'Instant Pot Duo 7-in-1 Electric Pressure Cooker (6 Quart)',
		'https://m.media-amazon.com/images/I/71thcs5a-WL._AC_SX679_.jpg',
		'https://amzn.to/46Myxep',
		99.99,
		109.99,
		'USD',
		(SELECT id FROM merchants WHERE name = 'Amazon' LIMIT 1),
		NULL,
		(SELECT id FROM categories WHERE slug = 'kitchen-dining' LIMIT 1),
		TRUE,
		(SELECT id FROM offer_statuses WHERE name = 'approved' LIMIT 1),
		now(),
		now(),
		now() + interval '14 days',
		NULL,
		NULL,
		TRUE,
		now()
	)
	ON CONFLICT (offer_key) DO NOTHING;
		`

	insertCouponStatusQuery = `
	INSERT INTO coupon_statuses (name, description) VALUES
		('pending_approval', 'Awaiting admin review'),
		('approved', 'Approved and ready for display'),
		('rejected', 'Rejected by admin'),
		('flagged', 'Flagged for review'),
		('active', 'Currently active and usable'),
		('inactive', 'Temporarily disabled'),
		('expired', 'Past the expiration date')
	ON CONFLICT (name) DO NOTHING;
	`
	insertPromotionsQuery = `
	INSERT INTO promotions (name, description) VALUES
		('Flash Sale', 'Promote offers with steep temporary discounts (e.g., 24–72 hours).'),
		('Limited-Time Offer', 'Offers available for a set short period.'),
		('Holiday Sale', 'Special offers for Black Friday, Christmas, New Year, Memorial Day, and other holidays.'),
		('Clearance Event', 'Promote old inventory marked for final sale.'),
		('Editor''s Pick', 'Highlight offers manually selected for quality, popularity, or uniqueness.'),
		('New Arrival Spotlight', 'Feature newly launched products or fresh offers.'),
		('Weekly Top Offers', 'Aggregate and promote the best-performing offers of the week.'),
		('Customer Favorite', 'Highlight offers voted highly or rated best by users.'),
		('Seasonal Promotion', 'Feature promotions tied to seasons like Spring, Back-to-School, or Summer.'),
		('Urgent: Low Stock', 'Promote offers nearing sold-out status to drive urgency.'),
		('Exclusive Member Offer', 'Offers visible only to signed-in members or loyalty customers.'),
		('Birthday Specials', 'Special promotions tied to user birthdays or anniversaries.'),
		('Free Shipping Offer', 'Highlight offers that offer free shipping as an incentive.'),
		('Price Drop Alert Feature', 'Showcase offers that recently dropped in price to attract buyers.'),
		('Trending Offer', 'Promote offers that are going viral based on clicks and engagement.'),
		('Tax Day Specials', 'Highlight promotions tied to U.S. Tax Day (April 15) to encourage refund spending.'),
		('Sagrenti Day Sale', 'Exclusive Sagrenti brand shopping event, similar to Amazon Prime Day.')
	ON CONFLICT (name) DO NOTHING;
    `
	insertOfferStatusQuery = `
	INSERT INTO offer_statuses (name, description) VALUES
		('pending_review', 'The offer has been submitted and is currently being reviewed before approval.'),
		('approved', 'The offer has been reviewed and approved for listing.'),
		('rejected', 'The offer has been reviewed and does not meet the necessary requirements.'),
		('expired', 'The offer is no longer active, either due to time constraints or merchant action.')
	ON CONFLICT (name) DO NOTHING;
	`
	insertSponsorshipBidTypesQuery = `
	INSERT INTO sponsorship_bid_types (code, name, description) VALUES
		('CPD','Cost Per Day','Fixed amount per day'),
		('CPC','Cost Per Click','Pay per click'),
		('CPI','Cost Per Impression','Pay per impression')
	ON CONFLICT (code) DO NOTHING;
	`
	insertMerchantQuery = `
	INSERT INTO merchants (
	merchant_type_id, name, display_name, slug, logo_url, website, platform_id, deleted_at, created_at, updated_at
	)
	VALUES (
		(SELECT id FROM merchant_types WHERE name = 'retail_giants' LIMIT 1),
		'Amazon',
		'Amazon',
		'amazon',
		'/static/logos/amazon/amazon.png',
		'https://www.amazon.com',
		NULL,
		NULL,
		now(),
		now()
	)
	ON CONFLICT (name) DO NOTHING;
	`
	insertMerchantTypeQuery = `
	INSERT INTO merchant_types (name, description) VALUES
		('retail_giants', 'Large-scale retailers such as Amazon, Walmart, and Target, typically operating dedicated or in-house affiliate programs and marketplaces.'),
		('global_brands', 'Iconic manufacturers and household names such as Nike, Dell, HP, and Apple, often selling directly and/or through retailers.'),
		('mid_market_brands', 'Recognizable, medium-sized brands like Gap, JC Penney, Expedia, primarily participating in affiliate networks such as CJ, ShareASale, and Rakuten.'),
		('emerging_brands', 'New or smaller-scale brands, local businesses, Shopify, and Etsy sellers, often developing their presence and affiliate programs.'),
		('affiliate_partner', 'Entities promoting and selling products/services on behalf of brands or retailers through affiliate networks but not directly manufacturing or holding inventory.'),
		('independent_merchant', 'Individual entrepreneurs or small local shops selling directly via platforms like Etsy or Shopify, typically without structured affiliate programs.')
	ON CONFLICT (name) DO NOTHING;
	`
	insertMerchantApplicationStatusQuery = `
	INSERT INTO merchant_application_status (name, description) VALUES
		('pending',  'Pending review by admin'),
		('approved', 'Application has been approved'),
		('rejected', 'Application was rejected')
	ON CONFLICT (name) DO NOTHING;
	`

	insertMerchantProgramPlansQuery = `
	INSERT INTO merchant_program_plans (code, name, description, is_active) VALUES
		(
			'standard',
			'Standard',
			'Standard merchant program plan with Launch Campaign access.',
			TRUE
		),
		(
			'premium',
			'Premium',
			'Premium merchant program plan with Future Offering and Launch Intelligence access.',
			TRUE
		),
		(
			'enterprise',
			'Enterprise',
			'Enterprise merchant program plan with Future Offering and Launch Intelligence access for larger merchant operations.',
			TRUE
		)
	ON CONFLICT (code) DO NOTHING;
	`

	insertMerchantProgramEntitlementsQuery = `
	INSERT INTO merchant_program_entitlements (plan_id, entitlement_code)
	SELECT p.id, v.entitlement_code
	FROM merchant_program_plans p
	JOIN (
		VALUES
			('standard', 'launch_campaign_access'),
			('premium', 'future_offering_access'),
			('enterprise', 'future_offering_access')
	) AS v(plan_code, entitlement_code)
		ON v.plan_code = p.code
	ON CONFLICT (plan_id, entitlement_code) DO NOTHING;
	`

	insertRolesQuery = `
	INSERT INTO roles (name, description, hierarchy_level, is_internal, assignable_at_signup, approval_required) VALUES
		('admin', 'Role for managing users, products, merchants, and more.', 3, TRUE, FALSE, FALSE),
		('editor', 'Role for editing products, coupons, and content, but cannot manage users.', 2, TRUE, FALSE, FALSE),
		('OfferCurator', 'Role for reviewing, selecting, and tagging good offers. Cannot manage users.', 1, TRUE, FALSE, FALSE),
		('QualityModerator', 'Role for flagging low-quality or expired content. Cannot manage users.', 1, TRUE, FALSE, FALSE),
		('CampaignManager', 'Role for overseeing merchant campaigns and sponsorships. Cannot manage users.', 1, TRUE, FALSE, FALSE),
		('viewer', 'Role for read-only access. Useful for analytics or auditing.', 1, TRUE, FALSE, FALSE),
		('customer', 'Role can log in, track their clicks, save favorite products, or access exclusive offers.', 0, FALSE, TRUE, FALSE),
		('merchant', 'Role can log in, manage listings, and participate in sponsorships. Requires approval.', 0, FALSE, TRUE, TRUE)
	ON CONFLICT (name) DO NOTHING;
		`
	insertPermissionsQuery = `
	INSERT INTO permissions (name, description) VALUES
		-- Admin Console
		(
			'read_admin_console',
			'Allows privileged access to the Admin Console control-plane overview'
		),
		-- Affiliate Performance
		('create_affiliate_performance', 'Allows creating affiliate performance'),
		('delete_affiliate_performance', 'Allows deleting an affiliate performance record'),
		('list_affiliate_performance', 'Allows listing affiliate performance records'),
		('read_affiliate_performance', 'Allows reading an affiliate performance record'),
		('update_affiliate_performance', 'Allows modifying an existing affiliate performance record'),
		-- Affiliate Programs
		('create_affiliate_program', 'Allows creating affiliate programs'),
		('list_affiliate_programs', 'Allows listing all affiliate programs'),
		('read_affiliate_program', 'Allows viewing a single affiliate program'),
		('update_affiliate_program', 'Allows updating affiliate programs'),
		('soft_delete_affiliate_program', 'Allows soft deleting affiliate programs'),
		('delete_affiliate_program', 'Allows permanently deleting affiliate programs'),
		-- Audit Logs
		('read_audit_log', 'Allows reading audit logs'),
		('read_archived_audit_log', 'Allows reading archived audit logs'),
		-- Audit Metadata
		('read_entity_type', 'Allows reading entity types'),
		('read_action', 'Allows reading action metadata'),
		-- Brands
		('create_brand', 'Allows creating a new brand record'),
		('read_brand', 'Allows reading a brand record'),
		('read_brands', 'Allows reading all brand records'),
		('soft_delete_brand', 'Allows soft deleting a brand'),
		('update_brand', 'Allows updating an existing brand record'),
		-- Departments
		('create_department', 'Allows creating a new department'),
		('read_department', 'Allows retrieving a department by ID'),
		('list_departments', 'Allows listing all departments'),
		('update_department', 'Allows updating an existing department'),
		('soft_delete_department', 'Allows soft deleting a department'),
		-- Categories
		('create_category', 'Allows creating new categories'),
		('read_category', 'Allows reading categories'),
		('list_categories', 'Allows listing all categories'),
		('update_category', 'Allows updating categories'),
		('soft_delete_category', 'Allows soft-deleting categories'),
		-- Coupons
		('clip_coupon', 'Allows users to clip coupons for later use'),
		('create_coupon', 'Allows creating new coupons'),
		('flag_coupon', 'Allows users to flag coupons for review'),
		('read_active_coupons', 'Allows viewing all currently active coupons'),
		('read_coupon', 'Allows retrieving a specific coupon by ID'),
		('read_coupon_performance', 'Allows viewing coupon performance analytics'),
		('read_flagged_coupons', 'Allows retrieving all flagged coupons for admin review'),
		('read_popular_coupons', 'Allows viewing the most popular (clicked or used) coupons'),
		('soft_delete_coupon', 'Allows soft-deleting coupons'),
		('suggest_coupons_for_user', 'Allows Suggesting personalized coupons for a user based on behavior'),
		('update_coupon', 'Allows updating coupon details'),
		-- Offer Clicks
		('track_offer_click', 'Allows tracking user interaction (click event) with an offer'),
		-- Offer Price History
		('compare_offer_price_with_competitors', 'Allows comparing offer price with competitor prices'),
		('create_offer_price_history', 'Allows creating a new offer price history record'),
		('flag_offer_price_anomaly', 'Allows flagging an offer price entry as an anomaly for manual review'),
		('read_offer_price_history', 'Allows retrieving a specific offer price history record'),
		('read_offer_price_history_by_offer', 'Allows retrieving all offer price history entries for an offer'),
		('read_offer_price_trend', 'Allows retrieving price trend data for an offer'),
		('read_significant_price_drops', 'Allows retrieving offers with significant price drops for alerting and promotions'),
		('update_offer_price_history', 'Allows updating an existing offer price history record'),
		('delete_offer_price_history', 'Allows deleting an offer price history record'),
		('subscribe_price_drop', 'Allows subscribing to price drop notifications for an offer'),
		-- Offer Ratings
		('create_offer_rating', 'Allows creating a new offer rating record'),
		('flag_offer_rating_review', 'Allows flagging an offer rating review for moderation'),
		('read_offer_rating', 'Allows retrieving a specific offer rating record'),
		('read_offer_ratings', 'Allows retrieving multiple offer ratings'),
		('read_offer_rating_count', 'Allows retrieving the total number of ratings for a specific offer'),
		('read_offer_rating_analytics', 'Allows generating and viewing user rating analytics reports'),
		('update_offer_rating', 'Allows updating an existing offer rating record'),
		('delete_offer_rating', 'Allows deleting an offer rating record permanently'),
		-- Offer Sponsorships
		('sponsor_offer', 'Allows sponsoring an offer'),
		('create_offer_sponsorship', 'Allows creating a new offer sponsorship record'),
		('read_offer_sponsorship', 'Allows reading an offer sponsorship record'),
		('update_offer_sponsorship', 'Allows updating an existing offer sponsorship record'),
		('read_offer_sponsorships', 'Allows reading offer sponsorship records'),
		-- Offer Status
		('create_offer_status', 'Allows creating a new offer status record'),
		('read_offer_status', 'Allows reading an offer status by ID'),
		('read_offer_statuses', 'Allows reading all offer statuses'),
		('update_offer_status', 'Allows updating the status of an offer'),
		('delete_offer_status', 'Allows deleting an offer status record'),
		('submit_offer_status_for_review', 'Allows submitting an offer status for review'),
		-- Offers
		('approve_curated_offer', 'Allows approving curated offers for publication'),
		('blacklist_offer', 'Allows blacklisting an offer'),
		('create_offer', 'Allows creating a new offer record'),
		('expire_offer', 'Allows expiring an offer'),
		('flag_offer', 'Allows flagging an offer for moderation'),
		('read_all_offers', 'Allows reading all offer records'),
		('read_offer', 'Allows reading an offer record'),
		('read_live_offers', 'Allows public listing of currently active, visible, unexpired, undeleted offers'),
		('read_flagged_offers', 'Allows reading all flagged offers'),
		('read_popular_offers', 'Allows reading popular offers'),
		('read_recent_offers', 'Allows reading recent offers'),
		('read_top_rated_offers', 'Allows reading top-rated offers'),
		('read_trending_offers', 'Allows reading trending offers'),
		('read_user_offer_rating', 'Allows reading a specific offer rating by the authenticated user'),
		('reject_curated_offer', 'Allows rejecting curated offers'),
		('review_curated_offers', 'Allows listing curated offers pending editorial review'),
		('reject_offer', 'Allows rejecting an offer'),
		('report_expired_offer', 'Allows reporting an expired offer'),
		('review_pending_offers', 'Allows reviewing pending offers'),
		('soft_delete_offer', 'Allows soft deleting an offer'),
		('submit_feedback_on_offer', 'Allows submitting feedback on an offer'),
		('update_offer', 'Allows updating an existing offer record'),
		('vote_on_offer', 'Allows voting on an offer'),
		-- Merchant-Affiliate Program Relationships
		('create_merchant_affiliate_program', 'Allows linking merchants to affiliate programs'),
		('read_merchant_affiliate_program', 'Allows reading merchant-affiliate program relationships'),
		('read_affiliate_programs_by_merchant', 'Allows reading affiliate programs for a given merchant'),
		('read_merchants_by_affiliate_program', 'Allows reading merchants for a given affiliate program'),
		('soft_delete_merchant_affiliate_program', 'Allows soft-deleting merchant-affiliate program links'),
		-- Merchant Applications
		('create_merchant_application', 'Allows submitting merchant applications'),
		('list_merchant_applications', 'Allows reading all or numerous merchant applications'),
		('read_merchant_application', 'Allows reading a merchant application'),
		('update_merchant_application', 'Allows updating merchant applications'),
		('soft_delete_merchant_application', 'Allows soft-deleting merchant applications'),
		-- Merchant Application Statuses
		('create_merchant_application_status', 'Allows creating application statuses'),
		('list_merchant_application_statuses', 'Allows reading all merchant application statuses'),
		('read_merchant_application_status', 'Allows reading a merchant application status'),
		('update_merchant_application_status', 'Allows updating application statuses'),
		('soft_delete_merchant_application_status', 'Allows soft-deleting application statuses'),
		-- Merchants
		('create_merchant', 'Allows creating a new merchant'),
		('read_merchant', 'Allows reading merchant info'),
		('list_merchants', 'Allows listing merchants'),
		('update_merchant', 'Allows updating merchant info'),
		('soft_delete_merchant', 'Allows soft-deleting merchants'),
		('delete_merchant', 'Allows permanently deleting a merchant'),
		-- Merchant Accounts
		('create_merchant_account', 'Allows creating the canonical platform account for a merchant'),
		('read_merchant_account', 'Allows reading a non-deleted merchant account by account ID'),
		('read_deleted_merchant_account', 'Allows privileged reading of a merchant account by account ID regardless of soft-delete state'),
		('read_merchant_account_by_merchant', 'Allows reading or checking the canonical merchant account by merchant ID'),
		('read_deleted_merchant_account_by_merchant', 'Allows privileged reading of a merchant account by merchant ID regardless of soft-delete state'),
		('list_merchant_accounts', 'Allows listing non-deleted merchant accounts'),
		('list_deleted_merchant_accounts', 'Allows privileged listing of merchant accounts including soft-deleted records'),
		('activate_merchant_account', 'Allows activating a pending or suspended merchant account'),
		('suspend_merchant_account', 'Allows suspending an active merchant account'),
		('close_merchant_account', 'Allows closing a merchant account'),
		('soft_delete_merchant_account', 'Allows soft-deleting a closed merchant account'),
		('restore_merchant_account', 'Allows restoring a soft-deleted merchant account while preserving its closed status'),
		('hard_delete_merchant_account', 'Allows permanently deleting a closed and soft-deleted merchant account'),
		-- Merchant Program Entitlements
		('create_merchant_program_entitlement', 'Allows creating merchant program entitlement records'),
		('ensure_merchant_program_entitlement', 'Allows idempotently ensuring merchant program entitlement records'),
		('read_merchant_program_entitlement', 'Allows reading merchant program entitlement records'),
		('list_merchant_program_entitlements', 'Allows listing merchant program entitlements by plan'),
		('delete_merchant_program_entitlement', 'Allows hard-deleting merchant program entitlement records'),
		-- Merchant Program Fee Schedules
		('create_merchant_program_fee_schedule', 'Allows creating a merchant program fee schedule'),
		('read_merchant_program_fee_schedule', 'Allows reading a merchant program fee schedule'),
		('list_merchant_program_fee_schedules', 'Allows listing merchant program fee schedules'),
		('resolve_merchant_program_fee_schedule', 'Allows resolving effective merchant program fee policy'),
		('activate_merchant_program_fee_schedule', 'Allows activating a merchant program fee schedule'),
		('deactivate_merchant_program_fee_schedule', 'Allows deactivating a merchant program fee schedule'),
		('retire_merchant_program_fee_schedule', 'Allows retiring a merchant program fee schedule'),
		('replace_merchant_program_fee_schedule', 'Allows atomically replacing a merchant program fee schedule'),
		('soft_delete_merchant_program_fee_schedule', 'Allows soft-deleting a merchant program fee schedule'),
		('restore_merchant_program_fee_schedule', 'Allows restoring a merchant program fee schedule'),
		('hard_delete_merchant_program_fee_schedule', 'Allows permanently deleting a merchant program fee schedule'),
		-- Merchant Program Plans
		('create_merchant_program_plan', 'Allows creating merchant program plan records'),
		('read_merchant_program_plan', 'Allows reading merchant program plan records'),
		('list_merchant_program_plans', 'Allows listing merchant program plan records'),
		('update_merchant_program_plan', 'Allows updating merchant program plan display fields'),
		('activate_merchant_program_plan', 'Allows activating merchant program plans'),
		('deactivate_merchant_program_plan', 'Allows deactivating merchant program plans'),
		('soft_delete_merchant_program_plan', 'Allows soft-deleting merchant program plans'),
		('restore_merchant_program_plan', 'Allows restoring soft-deleted merchant program plans'),
		-- Merchant Program Subscriptions
		('create_merchant_program_subscription', 'Allows creating merchant program subscription records'),
		('read_merchant_program_subscription', 'Allows reading merchant program subscription records'),
		('read_current_merchant_program_subscription', 'Allows reading the current merchant program subscription for a merchant'),
		('read_active_merchant_program_subscription', 'Allows reading the active merchant program subscription for a merchant'),
		('read_merchant_program_subscriptions_by_merchant', 'Allows listing merchant program subscriptions by merchant'),
		('read_merchant_program_subscriptions_by_plan', 'Allows listing merchant program subscriptions by plan and status'),
		('update_merchant_program_subscription_plan', 'Allows updating the plan assigned to a merchant program subscription'),
		('update_merchant_program_subscription_status', 'Allows transitioning merchant program subscription status'),
		('cancel_merchant_program_subscription', 'Allows cancelling merchant program subscriptions'),
		('soft_delete_merchant_program_subscription', 'Allows soft-deleting merchant program subscriptions'),
		('restore_merchant_program_subscription', 'Allows restoring soft-deleted merchant program subscriptions'),
		-- Merchant Program Subscription Events
		(
			'read_merchant_program_subscription_event',
			'Allows reading one immutable merchant program subscription lifecycle event'
		),
		(
			'list_merchant_program_subscription_events',
			'Allows listing immutable lifecycle events for a merchant program subscription'
		),
		-- Merchant Platform Credit Accounts
		('create_merchant_platform_credit_account', 'Allows creating merchant platform credit accounts'),
		('read_merchant_platform_credit_account', 'Allows reading merchant platform credit accounts'),
		('list_merchant_platform_credit_accounts', 'Allows listing merchant platform credit accounts'),
		(
			'update_merchant_platform_credit_account_descriptive_fields',
			'Allows updating merchant platform credit account descriptive fields'
		),
		('cancel_merchant_platform_credit_account', 'Allows cancelling merchant platform credit accounts'),
		-- Merchant Payment Methods
		('create_merchant_payment_method', 'Allows creating merchant-owned payment method references'),
		('read_merchant_payment_method', 'Allows reading a merchant-owned payment method reference'),
		('read_default_merchant_payment_method', 'Allows reading the merchant''s active default payment method reference'),
		('list_merchant_payment_methods', 'Allows listing merchant-owned payment method references'),
		('update_merchant_payment_method', 'Allows updating merchant payment method display metadata'),
		('set_default_merchant_payment_method', 'Allows assigning the merchant''s active default payment method'),
		('clear_default_merchant_payment_method', 'Allows clearing the merchant''s default payment method'),
		('update_merchant_payment_method_status', 'Allows transitioning merchant payment method status'),
		('soft_delete_merchant_payment_method', 'Allows soft-deleting a merchant-owned payment method reference'),
		('restore_merchant_payment_method', 'Allows restoring a soft-deleted merchant payment method reference'),
		-- Merchant Promotion
		('create_merchant_promotion', 'Allows creating a new merchant promotion record'),
		('extend_merchant_promotion_dates', 'Allows extending the start and/or end dates of an merchant promotion record'),
		('read_merchant_promotion', 'Allows retrieving a specific merchant promotion record'),
		('read_merchant_promotions', 'Allows retrieving multiple merchant promotion records'),
		('read_active_merchant_promotions', 'Allows retrieving all active merchant promotions across the platform'),
		('read_all_merchant_promotions', 'Allows retrieving all merchant promotions across the entire platform'),
		('read_upcoming_merchant_promotions', 'Allows retrieving all upcoming merchant promotions scheduled for the future'),
		('read_expired_merchant_promotions', 'Allows retrieving all expired merchant promotions across the platform'),
		('read_storewide_merchant_promotion', 'Allows retrieving the active storewide merchant promotion'),
		('update_merchant_promotion', 'Allows updating an existing merchant promotion record'),
		('soft_delete_merchant_promotion', 'Allows soft deleting an existing merchant promotion record'),
		('delete_merchant_promotion', 'Allows permanently deleting a merchant promotion record'),
		('bulk_delete_merchant_promotions', 'Allows permanently deleting multiple merchant promotion records'),
		('bulk_soft_delete_merchant_promotions', 'Allows soft deleting multiple merchant promotion records'),
		('search_merchant_promotions', 'Allows searching merchant promotions with optional filters'),
		('create_or_update_merchant_promotion', 'Allows creating or updating an merchant promotion record'),
		-- Merchant Types
		('create_merchant_type', 'Allows creating merchant types'),
		('read_merchant_type', 'Allows reading merchant types'),
		('update_merchant_type', 'Allows updating merchant types'),
		('soft_delete_merchant_type', 'Allows soft-deleting merchant types'),
		-- Platforms
		('create_platform', 'Allows creating platforms'),
		('read_platform', 'Allows reading platform info'),
		('update_platform', 'Allows updating platform info'),
		('soft_delete_platform', 'Allows soft-deleting platforms'),
		-- Platform Settings
		('create_platform_setting', 'Allows creating a new platform setting'),
		('ensure_platform_setting', 'Allows idempotently ensuring a platform setting'),
		('read_platform_setting', 'Allows reading platform setting records'),
		('list_platform_settings', 'Allows listing platform setting records'),
		('update_platform_setting', 'Allows updating platform setting value and type'),
		('activate_platform_setting', 'Allows activating a platform setting'),
		('deactivate_platform_setting', 'Allows deactivating a platform setting'),
		('soft_delete_platform_setting', 'Allows soft-deleting a platform setting'),
		('hard_delete_platform_setting', 'Allows permanently hard-deleting a platform setting'),
		-- Platform Settings
		('create_platform_setting', 'Allows creating a new platform setting'),
		('ensure_platform_setting', 'Allows idempotently ensuring a platform setting'),
		('read_platform_setting', 'Allows reading platform setting records'),
		('list_platform_settings', 'Allows listing platform setting records'),
		('update_platform_setting', 'Allows updating platform setting value and type'),
		('activate_platform_setting', 'Allows activating a platform setting'),
		('deactivate_platform_setting', 'Allows deactivating a platform setting'),
		('soft_delete_platform_setting', 'Allows soft-deleting a platform setting'),
		('hard_delete_platform_setting', 'Allows permanently hard-deleting a platform setting'),

		-- Platform Setting History
		('read_platform_setting_history', 'Allows privileged reading of one immutable platform-setting history record'),
		('list_platform_setting_history', 'Allows privileged listing of immutable value-history records for a platform setting'),
		-- Promotion
		('create_promotion', 'Allows creating a new promotion record'),
		('read_promotion', 'Allows retrieving a specific promotion record'),
		('update_promotion', 'Allows updating an existing promotion record'),
		('soft_delete_promotion', 'Allows soft deleting an existing promotion record'),
		-- Products
		('create_product', 'Allows creating a new product record'),
		('read_product', 'Allows reading product details'),
		('read_all_products', 'Allows reading all products'),
		('soft_delete_product', 'Allows soft deletion of a product'),
		('update_product', 'Allows updating an existing product record'),
		-- User Dashboards
		('assign_dashboard_template', 'Allows assigning a dashboard template to a user'),
		('audit_user_dashboard_activity', 'Allows auditing user dashboard activity'),
		('clone_dashboard', 'Allows cloning an existing dashboard'),
		('create_user_dashboard', 'Allows creating a new user dashboard'),
		('delete_user_dashboard', 'Allows deletion of a user dashboard'),
		('export_user_dashboard', 'Allows exporting user dashboard data'),
		('generate_curation_reports', 'Allows generating curation reports for affiliate offers'),
		('read_user_dashboard', 'Allows reading a user dashboard'),
		('read_user_dashboard_widgets', 'Allows reading user dashboard widgets'),
		('read_user_dashboards', 'Allows reading all user dashboards'),
		('read_dashboard_reports', 'Allows reading dashboard reports'),
		('save_user_dashboard_preferences', 'Allows saving user-specific dashboard preferences'),
		('update_user_dashboard', 'Allows updating user dashboard settings'),
		('view_admin_dashboard_stats', 'Allows viewing admin dashboard statistics'),
		-- User Favorites
		('auto_expire_old_favorites', 'Allows automatically expiring old favorites'),
		('batch_soft_delete_user_favorite', 'Allows batch soft deletion of a user favorite'),
		('create_user_favorite', 'Allows creating a new user favorite record'),
		('remove_user_favorite', 'Allows removing an item from user favorites'),
		('follow_merchant', 'Allows a user to follow a merchant and start receiving offer notifications'),
		('unfollow_merchant', 'Allows a user to unfollow a merchant and stop receiving offer notifications'),
		('migrate_favorites', 'Allows migrating favorite offers from one user to another'),
		('read_followed_merchants_offers', 'Allows reading offers from merchants that the user follows'),
		('read_most_favorited_offers', 'Allows reading the most favorited offers'),
		('read_personalized_offers', 'Allows reading personalized offers for a user'),
		('read_recently_favorited_offer', 'Allows reading the most recently favorited offer by a user'),
		('read_trending_offers_for_user', 'Allows reading trending offers for a specific user'),
		('read_user_offer_purchase_history', 'Allows reading the purchase history of offers for a user'),
		('read_user_favorites', 'Allows reading active user favorites'),
		('read_user_favorites_count', 'Allows reading the count of user favorites for a specific offer'),
		('read_users_who_favorited_offer', 'Allows reading users who have favorited a specific offer'),
		('recommend_offers', 'Allows recommending offers to a user'),
		('request_restock_notification', 'Allows requesting a restock notification for a product'),
		('save_user_favorite', 'Allows saving a user''s favorite offer'),
		('set_offer_alert', 'Allows setting an alert for an offer'),
		('share_offer', 'Allows sharing an offer'),
		('share_favorite_list', 'Allows sharing a user''s favorite list with another user'),
		('track_favorite_categories', 'Allows tracking user''s favorite categories'),
		('unsave_user_favorite', 'Allows unsaving a user favorite item'),
		-- User Notifications
		('notify_user', 'Allows sending notifications to users'),
		('read_user_notification', 'Permission to read one user notification'),
		('read_user_notifications', 'Permission to read multiple user notifications'),
		('soft_delete_user_notification', 'Allows dismissing a user notification without destroying the retained history row'),
		('delete_user_notification', 'Allows permanent deletion of a user notification as an exceptional maintenance path'),
		-- User Profiles
		('create_user_profile', 'Allows creating a new user profile'),
		('delete_user_profile', 'Allows deleting a user profile'),
		('moderate_user_profile', 'Allows internal users to flag or unflag user profiles'),
		('read_reserved_handles', 'Allows retrieving reserved user handles for validation UI'),
		('read_user_profile', 'Allows retrieving a user profile'),
		('read_user_profiles', 'Allows retrieving multiple user profiles via search or listing'),
		('soft_delete_user_profile', 'Allows soft deleting a user profile'),
		('update_user_profile', 'Allows updating a user profile'),
		-- roles
		('assign_role', 'Allows assigning a new role to a user'),
		('list_roles', 'Allows listing all roles in the system'),
		-- User Settings
		('delete_user_settings', 'Allows deleting user settings'),
		('read_user_settings', 'Allows reading all user settings records'),
		('save_user_settings', 'Allows saving user settings'),
		('soft_delete_user_settings', 'Allows soft deleting user settings records'),
		('update_user_settings', 'Allows updating user settings'),
		-- User Wallets
		('create_user_wallet', 'Allows creating a new user wallet'),
		('credit_user_wallet_rewards', 'Allows crediting reward points to a user wallet'),
		('confirm_wallet_ledger_entry', 'Allows confirming a pending wallet ledger entry'),
		('reverse_wallet_ledger_entry', 'Allows reversing a wallet ledger entry'),
		('deactivate_user_wallet', 'Allows deactivating a user wallet'),
		('delete_user_wallet', 'Allows deleting a user wallet'),
		('read_user_wallet', 'Allows reading a user wallet'),
		('read_user_wallets', 'Allows reading multiple user wallet records'),
		('soft_delete_user_wallet', 'Allows soft deletion of a user wallet'),
		-- Users
		('delete_own_account', 'Allows user to delete own account'),
		('expel_user', 'Allows admin to delete user account')
	ON CONFLICT (name) DO NOTHING;
    `

	insertEntityTypeQuery = `
	INSERT INTO entity_types (name, description) VALUES
		('action', 'Action entity for audit trail and metadata retrieval'),
		('admin_dashboard', 'Admin dashboard entity'),
		(
			'admin_console',
			'Privileged Admin Console control-plane access surface'
		),
		('affiliate_performance', 'Affiliate Performance entity'),
		('affiliate_program', 'Affiliate program entity'),
		('audit_log', 'Audit log entity used to track system and user actions'),
		('brand', 'Brand entity representing a company or product line'),
		('category', 'Category entity representing groupings for offers and products'),
		('coupon', 'Coupon entity representing discount offers'),
		('coupons', 'Tracks coupon-related actions.'),
		('coupon_status', 'Coupon status entity representing the life cycle stages of a discount coupon'),
		('curation_report', 'Curation report for affiliate offers'),
		('dashboard_report', 'Dashboard report entity'),
		('dashboard_template', 'Dashboard template that may be assigned to a user'),
		('offer', 'Offer entity'),
		('offers', 'Tracks offer-related actions.'),
		('offer_alert', 'Alert set for an offer'),
		('offer_click', 'Click event on a specific offer for tracking user engagement'),
		('offer_recommendation', 'Recommended offers for a user'),
		('offer_price_history', 'Historical price data for an offer'),
		('offer_rating', 'User rating and review for an offer'),
		('offer_share', 'Sharing of an offer'),
		('offer_sponsorship', 'Sponsorship details for an offer'),
		('offer_status', 'Status of an offer'),
		('entity_type', 'EntityType metadata object used to describe audit trail loggable system entities'),
		('merchant', 'Merchant entity'),
		('merchant_account', 'Canonical merchant platform-account lifecycle entity'),
		('merchant_program_entitlement', 'Merchant program entitlement capability-gate entity'),
		('merchant_program_fee_schedule', 'Merchant program fee schedule effective-dated commercial policy entity'),
		('merchant_program_plan', 'Merchant program plan entity'),
		('merchant_program_subscription', 'Merchant program subscription lifecycle entity'),
		('merchant_program_subscription_event', 'Immutable merchant program subscription lifecycle event entity'),
		('merchant_platform_credit_account', 'Platform-issued merchant commercial credit account entity'),
		('merchant_payment_method', 'Merchant billing payment-method reference entity'),
		('merchants', 'Tracks merchant-related actions.'),
		('merchant_follow', 'Follow relationship between user and merchant'),
		('merchant_promotion', 'Merchant-specific promotion applied to offers'),
		('merchant_type', 'Merchant type entity'),
		('merchant_application', 'Merchant application entity'),
		('merchant_application_status', 'Merchant application status entity'),
		('merchant_affiliate_program', 'Merchant-Affiliate Program join entity'),
		('platform', 'Platform entity'),
		('platform_setting', 'Platform setting configuration entity'),
		('platform_setting_history', 'Immutable platform-setting value-transition history entity'),
		('product', 'Product entity'),
		('products', 'Tracks product-related actions.'),
		('promotion', 'Promotion type for offers'),
		('restock_notification', 'Notification request for product restock'),
		('roles', 'User role assignments'),
		('system', 'System-wide automation or batch operation'),
		('user_dashboard_activity', 'User activity on dashboard'),
		('user_dashboard', 'User dashboard entity'),
		('user_dashboard_widget', 'Widgets for user dashboard'),
		('user_dashboard_preferences', 'User-specific dashboard preferences'),
		('user_offer_purchase_history', 'A user''s purchase history for offers'),
		('user_favorite', 'User favorite entity'),
		('user_favorite_categories', 'User''s favorite categories'),
		('user_favorite_list', 'A user''s list of favorite offers'),
		('user_notification', 'User Notification entity'),
		('user_profile', 'User profile entity'),
		('user_settings', 'User settings entity'),
		('user_wallet', 'User wallet entity'),
		('user', 'User entity'),
		('users', 'Tracks user-related actions.')
	ON CONFLICT (name) DO NOTHING;
	`
	insertActionQuery = `
	INSERT INTO actions (name, description) VALUES
		-- Activation Codes
		('create_activation_token', ''),
		-- Admin Console
		(
			'read_admin_console',
			'Read the Admin Console control-plane overview'
		),
		-- Affiliate Performance
		('create_affiliate_performance', 'Create a new affiliate performance record'),
		('delete_affiliate_performance', 'Delete an existing affiliate performance record'),
		('list_affiliate_performance', 'List affiliate performance records'),
		('read_affiliate_performance', 'Read affiliate performance by ID'),
		('update_affiliate_performance', 'Modify an existing affiliate performance record'),
		-- Affiliate Programs
		('create_affiliate_program', 'Create a new affiliate program'),
		('read_affiliate_program', 'Retrieve an affiliate program by ID'),
		('list_affiliate_programs', 'List affiliate programs'),
		('update_affiliate_program', 'Modify an existing affiliate program'),
		('soft_delete_affiliate_program', 'Soft delete an affiliate program'),
		('delete_affiliate_program', 'Permanently delete an affiliate program'),
		-- Audit Log
		('read_audit_log', 'Retrieve a specific audit log entry'),
		('read_audit_log_by_user', 'Retrieve audit logs for the authenticated user'),
		('read_audit_log_by_entity', 'Retrieve audit logs for a given entity ID and type'),
		('read_audit_log_by_time_range', 'Retrieve audit logs within a given time range'),
		('read_archived_audit_log_by_entity', 'Retrieve archived audit logs by entity type and ID'),
		('read_archived_audit_log_by_time_range', 'Retrieve archived audit logs within a specified time range'),
		-- Audit Actions
		('list_actions', 'List all actions'),
		('read_action', 'Retrieve a specific action by ID'),
		-- Audit Entity Types
		('list_entity_types', 'List all entity types'),
		('read_entity_type', 'Retrieve an entity type by ID'),
		-- Brands
		('create_brand', 'Create a new brand'),
		('read_brand', 'Retrieve a brand by ID'),
		('read_brand_by_name', 'Retrieve a brand by name'),
		('read_brands', 'Retrieve all brands'),
		('soft_delete_brand', 'Soft delete a brand'),
		('update_brand', 'Update an existing brand record'),
		-- Departments
		('create_department', 'Create a new department'),
		('list_departments', 'List all departments'),
		('read_department', 'Retrieve a department by ID'),
		('update_department', 'Update an existing department'),
		('soft_delete_department', 'Soft-delete a department'),
		('delete_department', 'Permanently delete a department'),
		-- Categories
		('create_category', 'Create a new product or offer category'),
		('list_categories', 'List all categories'),
		('read_category', 'Retrieve a category by ID'),
		('update_category', 'Update an existing category'),
		('soft_delete_category', 'Soft-delete a product or offer category'),
		('delete_category', 'Permanently delete a product or offer category'),
		-- Coupons
		('analyze_coupon_performance', 'Analyze performance metrics of a specific coupon'),
		('auto_expire_coupons', 'Automatically expire coupons with past end_date'),
		('clip_coupon', 'Clip (save) a coupon for later use by a user'),
		('create_coupon', 'Create a new discount coupon'),
		('flag_coupon', 'Flag a coupon for review and moderation'),
		('read_active_coupons', 'Retrieve all currently active coupons (available for use)'),
		('read_clipped_coupons', 'Retrieve all coupons clipped by a user'),
		('read_coupon', 'Retrieve a specific coupon by ID'),
		('read_coupons_by_category', 'Retrieve coupons associated with a category ID'),
		('read_coupon_by_offer', 'Retrieve coupons associated with an offer ID'),
		('read_flagged_coupons', 'Retrieve all flagged coupons for admin review'),
		('read_popular_coupons', 'Retrieve the most popular (clicked or used) coupons'),
		('soft_delete_coupon', 'Soft-delete a coupon'),
		('suggest_coupons_for_user', 'Suggest personalized coupons for a user based on behavior'),
		('track_coupon_usage', 'Track user interaction with a coupon (e.g., click or redemption)'),
		('update_coupon', 'Update an existing discount coupon'),
		-- Offer Clicks
		('track_offer_click', 'Track user interaction (click) with an offer'),
		-- Offer Price History
		('compare_offer_price_with_competitors', 'Compare offer price with competitor prices'),
		('create_offer_price_history', 'Create a new offer price history record'),
		('flag_offer_price_anomaly', 'Flag an offer price history record as suspicious or anomalous'),
		('read_offer_price_history', 'Retrieve a specific offer price history by ID'),
		('read_offer_price_history_by_offer', 'Retrieve offer price histories associated with an offer ID'),
		('read_offer_price_trend', 'Retrieve price trend over time for an offer'),
		('read_latest_offer_price', 'Retrieve latest offer price by offer ID'),
		('read_significant_price_drops', 'Retrieve offers with significant price drops for promotional or alerting purposes'),
		('update_offer_price_history', 'Update an existing offer price history record'),
		('delete_offer_price_history', 'Delete an offer price history record'),
		('subscribe_price_drop', 'Subscribe to a price drop alert for an offer'),
		('trigger_price_drop_alert', 'Notify users of offer price drop'),
		-- Offer Ratings
		('create_offer_rating', 'Create a new offer rating'),
		('flag_offer_rating_review', 'Flag an offer rating review for abuse or spam'),
		('read_offer_rating', 'Retrieve an offer rating by ID'),
		('read_offer_ratings_by_offer', 'Retrieve all offer ratings for a specific offer'),
		('read_offer_ratings_by_user', 'Retrieve all offer ratings created by a user'),
		('read_offer_rating_count', 'Retrieve the total number of ratings for a specific offer'),
		('read_offer_rating_analytics', 'Generate a user rating analytics report'),
		('update_offer_rating', 'Update an existing offer rating record'),
		('delete_offer_rating', 'Delete an existing offer rating record'),
		-- Offers
		('approve_curated_offer', 'Approve curated offer'),
		('blacklist_offer', 'Blacklist an offer'),
		('create_offer', 'Create a new offer'),
		('expire_offer', 'Expire an offer'),
		('flag_offer', 'Flag an offer for moderation'),
		('list_pending_curated_offers', 'View pending curated offers'),
		('read_all_offers', 'Retrieve all offers'),
		('read_offer', 'Retrieve an offer by ID'),
		('read_flagged_offers', 'Retrieve all flagged offers'),
		('read_popular_offers', 'Retrieve popular offers'),
		('read_recent_offers', 'Retrieve recent offers'),
		('read_top_rated_offers', 'Retrieve top-rated offers'),
		('read_trending_offers', 'Retrieve trending offers'),
		('read_user_offer_rating', 'Retrieve a specific offer rating by the authenticated user'),
		('reject_curated_offer', 'Reject curated offer'),
		('reject_offer', 'Reject an existing offer'),
		('report_expired_offer', 'Report an expired offer'),
		('review_pending_offers', 'Review pending offers'),
		('soft_delete_offer', 'Soft delete an offer'),
		('submit_feedback_on_offer', 'Submit feedback on an offer'),
		('update_offer', 'Update an existing offer'),
		('vote_on_offer', 'Vote on an offer'),
		-- Offer Sponsorships
		('sponsor_offer', 'Sponsor an offer'),
		('create_offer_sponsorship', 'Create a new offer sponsorship'),
		('read_offer_sponsorship', 'Retrieve an offer sponsorship by ID'),
		('update_offer_sponsorship', 'Update an offer sponsorship record'),
		('read_offer_sponsorships', 'Retrieve all offer sponsorships for a specific offer'),
		-- Offer Status
		('create_offer_status', 'Create a new offer status'),
		('read_offer_status', 'Retrieve an offer status by ID'),
		('read_offer_statuses', 'Retrieve all offer statuses'),
		('update_offer_status', 'Update the status of an offer'),
		('delete_offer_status', 'Delete an existing offer status'),
		('submit_offer_status_for_review', 'Submit an offer status for review'),
		-- Merchants
		('create_merchant', 'Create a new merchant'),
		('count_merchants', 'Retrieve the total count of merchants'),
		('list_merchants', 'List all merchants'),
		('read_merchant', 'Retrieve merchant by ID'),
		('read_merchant_by_brand', 'Retrieve merchants associated with a given brand ID'),
		('read_merchant_by_offer', 'Retrieve merchant using associated offer ID'),
		('read_merchant_by_name', 'Retrieve merchant by name'),
		('list_merchants_by_platform', 'Retrieve merchants associated with a platform'),
		('read_merchant_by_product', 'Retrieve merchants linked to a product ID'),
		('read_merchant_by_product_line', 'Retrieve merchants linked to a product line'),
		('read_merchant_by_website', 'Retrieve merchant using its website URL'),
		('soft_delete_merchant', 'Soft-delete a merchant'),
		('delete_merchant', 'Permanently delete a merchant'),
		('update_merchant', 'Update an existing merchant'),
		-- Merchant Accounts
		('create_merchant_account', 'Create the canonical platform account for a merchant'),
		('read_merchant_account', 'Read a non-deleted merchant account by account ID'),
		('read_deleted_merchant_account', 'Read a merchant account by account ID regardless of soft-delete state'),
		('read_merchant_account_by_merchant', 'Read or check the canonical merchant account by merchant ID'),
		('read_deleted_merchant_account_by_merchant', 'Read a merchant account by merchant ID regardless of soft-delete state'),
		('list_merchant_accounts', 'List non-deleted merchant accounts'),
		('list_deleted_merchant_accounts', 'List merchant accounts including soft-deleted records'),
		('activate_merchant_account', 'Activate a pending or suspended merchant account'),
		('suspend_merchant_account', 'Suspend an active merchant account'),
		('close_merchant_account', 'Close a merchant account'),
		('soft_delete_merchant_account', 'Soft-delete a closed merchant account'),
		('restore_merchant_account', 'Restore a soft-deleted merchant account while preserving its closed status'),
		('hard_delete_merchant_account', 'Permanently delete a closed and soft-deleted merchant account'),
		-- Merchant Types
		('create_merchant_type', 'Create a new merchant type'),
		('list_merchant_types', 'List merchant types'),
		('read_merchant_type', 'Retrieve merchant type by ID'),
		('read_merchant_type_by_name', 'Retrieve merchant type by name'),
		('update_merchant_type', 'Update an existing merchant type'),
		('soft_delete_merchant_type', 'Soft-delete a merchant type'),
		-- Merchant Affiliate Program
		('create_merchant_affiliate_program', 'Create association between merchant and affiliate program'),
		('list_merchant_affiliate_programs', 'List all merchant-affiliate program associations'),
		('read_merchant_affiliate_program', 'Retrieve association between merchant and affiliate program by IDs'),
		('read_merchant_affiliate_programs_by_merchant', 'Retrieve all merchant-affiliate program associations for a given merchant ID'),
		('read_merchant_affiliate_programs_by_affiliate_program', 'Retrieve all merchant-affiliate program associations for a given affiliate program ID'),
		('soft_delete_merchant_affiliate_program', 'Soft-delete an merchant-affiliate program association'),
		('soft_delete_merchant_affiliate_program_by_merchant', 'Soft-delete all merchant-affiliate program associations for a given merchant'),
		('soft_delete_merchant_affiliate_program_by_affiliate_program', 'Soft-delete all merchant-affiliate program associations for a given affiliate program ID'),
		-- Merchant Applications
		('create_merchant_application', 'Create a new merchant application'),
		('list_merchant_applications', 'Retrieve all merchant applications'),
		('list_merchant_applications_by_affiliate_program', 'Retrieve all merchant applications associated with an affiliate program ID'),
		('list_merchant_applications_by_status', 'Retrieve all merchant applications by their status ID'),
		('read_merchant_application', 'Retrieve merchant application by ID'),
		('read_merchant_application_by_merchant', 'Retrieve merchant application by the associated merchant ID'),
		('soft_delete_merchant_application', 'Soft-delete a merchant application'),
		('delete_merchant_application', 'Permanently delete a merchant application'),
		('update_merchant_application', 'Update an existing merchant application'),
		-- Merchant Application Status
		('create_merchant_application_status', 'Create a new merchant application status'),
		('list_merchant_application_statuses', 'Retrieve all merchant application statuses'),
		('read_merchant_application_status', 'Retrieve merchant application status by ID'),
		('read_merchant_application_status_by_name', 'Retrieve merchant application status by name'),
		('soft_delete_merchant_application_status', 'Soft-delete a merchant application status'),
		('update_merchant_application_status', 'Update an existing merchant application status'),
		-- Merchant Program Entitlements
		('create_merchant_program_entitlement', 'Create a merchant program entitlement'),
		('ensure_merchant_program_entitlement', 'Ensure a merchant program entitlement exists'),
		('read_merchant_program_entitlement', 'Read a merchant program entitlement by ID'),
		('read_merchant_program_entitlement_by_plan_and_code', 'Read a merchant program entitlement by plan and code'),
		('list_merchant_program_entitlements_by_plan', 'List merchant program entitlements by plan'),
		('check_merchant_program_entitlement', 'Check whether a merchant program plan has an entitlement'),
		('delete_merchant_program_entitlement', 'Delete a merchant program entitlement'),
		('delete_merchant_program_entitlement_by_plan_and_code', 'Delete a merchant program entitlement by plan and code'),
		-- Merchant Program Fee Schedules
		('create_merchant_program_fee_schedule', 'Create a merchant program fee schedule'),
		('read_merchant_program_fee_schedule', 'Read a merchant program fee schedule'),
		('list_merchant_program_fee_schedules', 'List merchant program fee schedules'),
		('resolve_merchant_program_fee_schedule', 'Resolve effective merchant program fee policy'),
		('activate_merchant_program_fee_schedule', 'Activate a merchant program fee schedule'),
		('deactivate_merchant_program_fee_schedule', 'Deactivate a merchant program fee schedule'),
		('retire_merchant_program_fee_schedule', 'Retire a merchant program fee schedule'),
		('replace_merchant_program_fee_schedule', 'Atomically replace a merchant program fee schedule'),
		('soft_delete_merchant_program_fee_schedule', 'Soft-delete a merchant program fee schedule'),
		('restore_merchant_program_fee_schedule', 'Restore a merchant program fee schedule'),
		('hard_delete_merchant_program_fee_schedule', 'Permanently delete a merchant program fee schedule'),
		-- Merchant Program Plans
		('create_merchant_program_plan', 'Create a merchant program plan'),
		('read_merchant_program_plan', 'Read a merchant program plan by ID'),
		('read_merchant_program_plan_by_code', 'Read a merchant program plan by code'),
		('list_merchant_program_plans', 'List merchant program plans'),
		('update_merchant_program_plan', 'Update a merchant program plan'),
		('activate_merchant_program_plan', 'Activate a merchant program plan'),
		('deactivate_merchant_program_plan', 'Deactivate a merchant program plan'),
		('soft_delete_merchant_program_plan', 'Soft-delete a merchant program plan'),
		('restore_merchant_program_plan', 'Restore a merchant program plan'),
		-- Merchant Program Subscriptions
		('create_merchant_program_subscription', 'Create a merchant program subscription'),
		('read_merchant_program_subscription', 'Read a merchant program subscription by ID'),
		('read_current_merchant_program_subscription', 'Read the current merchant program subscription for a merchant'),
		('read_active_merchant_program_subscription', 'Read the active merchant program subscription for a merchant'),
		('read_merchant_program_subscriptions_by_merchant', 'List merchant program subscriptions by merchant'),
		('read_merchant_program_subscriptions_by_plan', 'List merchant program subscriptions by plan and status'),
		('update_merchant_program_subscription_plan', 'Update the plan assigned to a merchant program subscription'),
		('update_merchant_program_subscription_status', 'Transition merchant program subscription status'),
		('cancel_merchant_program_subscription', 'Cancel a merchant program subscription'),
		('soft_delete_merchant_program_subscription', 'Soft-delete a merchant program subscription'),
		('restore_merchant_program_subscription', 'Restore a merchant program subscription'),
		-- Merchant Program Subscription Events
		('read_merchant_program_subscription_event', 'Read one immutable merchant program subscription lifecycle event'),
		('list_merchant_program_subscription_events', 'List immutable lifecycle events for a merchant program subscription'),
		('read_latest_merchant_program_subscription_event', 'Read the latest immutable lifecycle event for a merchant program subscription'),
		-- Merchant Platform Credit Account
		('create_merchant_platform_credit_account', 'Create a platform-issued merchant commercial credit account'),
		('read_merchant_platform_credit_account', 'Read a platform-issued merchant commercial credit account'),
		('list_merchant_platform_credit_accounts', 'List platform-issued merchant commercial credit accounts'),
		('update_merchant_platform_credit_account_descriptive_fields', 'Replace descriptive metadata for a merchant platform credit account'),
		('cancel_merchant_platform_credit_account', Cancel an active merchant platform credit account'),
		-- Merchant Payment Methods
		('create_merchant_payment_method', 'Create a merchant payment method reference'),
		('read_merchant_payment_method', 'Read a merchant payment method reference'),
		('read_default_merchant_payment_method', 'Read the merchant''s active default payment method reference'),
		('list_merchant_payment_methods', 'List merchant payment method references'),
		('update_merchant_payment_method', 'Update merchant payment method display metadata'),
		('set_default_merchant_payment_method', 'Set the merchant''s active default payment method'),
		('clear_default_merchant_payment_method', 'Clear the merchant''s default payment method'),
		('update_merchant_payment_method_status', 'Update merchant payment method status'),
		('soft_delete_merchant_payment_method', 'Soft-delete a merchant payment method reference'),
		('restore_merchant_payment_method', 'Restore a merchant payment method reference'),
		-- Merchant Promotion
		('create_merchant_promotion', 'Create a new merchant promotion record'),
		('extend_merchant_promotion_dates', 'Extend the start and/or end dates of an merchant promotion record'),
		('read_merchant_promotion', 'Read an merchant promotion record'),
		('read_merchant_promotions_by_merchant', 'Retrieve all merchant promotions for a specific merchant ID'),
		('read_merchant_promotions_by_promotion', 'Retrieve merchant promotions by promotion ID'),
		('read_merchant_promotions_by_offer', 'Retrieve merchant promotions targeting a specific offer ID'),
		('read_active_merchant_promotions', 'Retrieve active merchant promotions'),
		('read_all_merchant_promotions', 'Retrieve all merchant promotions across the platform'),
		('read_upcoming_merchant_promotions', 'Retrieve upcoming merchant promotions scheduled to start in the future'),
		('read_expired_merchant_promotions', 'Retrieve expired merchant promotions'),
		('read_storewide_merchant_promotion', 'Read active storewide merchant promotion for an merchant'),
		('update_merchant_promotion', 'Update an existing merchant promotion record'),
		('soft_delete_merchant_promotion', 'Soft delete an merchant promotion record'),
		('delete_merchant_promotion', 'Permanently delete a merchant promotion record'),
		('bulk_delete_merchant_promotions', 'Permanently delete multiple merchant promotion records'),
		('bulk_soft_delete_merchant_promotions', 'Soft delete multiple merchant promotion records'),
		('search_merchant_promotions', 'Search merchant promotions based on optional filters'),
		('create_or_update_merchant_promotion', 'Create or update an merchant promotion record'),
		-- Platforms
		('create_platform', 'Create a new platform'),
		('list_platforms', 'Retrieve a list of all non-deleted platforms'),
		('read_platform', 'Retrieve platform by ID'),
		('read_platform_by_name', 'Retrieve platform by name'),
		('update_platform', 'Update an existing platform'),
		('soft_delete_platform', 'Soft-delete a platform'),
		-- Platform Settings
		('create_platform_setting', 'Create a platform setting'),
		('ensure_platform_setting', 'Ensure a platform setting'),
		('update_platform_setting_value', 'Update platform setting value and type'),
		('set_platform_setting_active', 'Set platform setting active state'),
		('soft_delete_platform_setting', 'Soft delete a platform setting'),
		('hard_delete_platform_setting', 'Hard delete a platform setting'),
		-- Platform Setting History
		('read_platform_setting_history', 'Read one immutable platform-setting history record'),
		('list_platform_setting_history', 'List immutable value-history records for a platform setting'),
		-- Products
		('create_product', 'Create a new product'),
		('read_product', 'Retrieve a product by ID'),
		('read_product_by_upc', 'Retrieve a product by UPC'),
		('read_all_products', 'Retrieve all products'),
		('soft_delete_product', 'Soft delete an existing product'),
		('update_product', 'Update an existing product'),
		-- Promotion
		('create_promotion', 'Create a new promotion record'),
		('read_promotion', 'Read an existing promotion record'),
		('update_promotion', 'Update an existing promotion record'),
		('soft_delete_promotion', 'Soft delete an existing promotion record'),
		-- roles
		('assign_role', 'Assign a new role to a user'),
		('list_roles', 'List all roles in the system'),
		-- system
		('plan_suggestions_batch', 'Fetch users eligible for suggestion batch'),
		-- User Dashboards
		('assign_dashboard_template', 'Assign a dashboard template to a user'),
		('audit_user_dashboard_activity', 'Audit user dashboard activity'),
		('clone_dashboard', 'Clone an existing dashboard'),
		('create_user_dashboard', 'Create a new user dashboard'),
		('delete_user_dashboard', 'Delete a user dashboard'),
		('export_user_dashboard', 'Export user dashboard data'),
		('generate_curation_reports', 'Generate curation reports for offers'),
		('read_user_dashboard', 'Retrieve a user dashboard by ID'),
		('read_user_dashboard_widgets', 'Retrieve user dashboard widgets'),
		('read_user_dashboards', 'Retrieve all user dashboards'),
		('read_dashboard_reports', 'Retrieve dashboard reports'),
		('save_user_dashboard_preferences', 'Save user dashboard preferences'),
		('update_user_dashboard', 'Update user dashboard settings'),
		('view_admin_dashboard_stats', 'View admin dashboard statistics'),
		-- User Favorites
		('auto_expire_old_favorites', 'Automatically expire old favorites'),
		('batch_soft_delete_user_favorites', 'Batch soft delete user favorites by user ID'),
		('create_user_favorite', 'Create a new user favorite'),
		('remove_user_favorite', 'Remove an item from user favorites'),
		('follow_merchant', 'Follow a merchant'),
		('unfollow_merchant', 'Unfollow a merchant'),
		('migrate_favorites', 'Migrate favorites from one user to another'),
		('read_followed_merchant_offers', 'Retrieve offers from followed merchants'),
		('read_most_favorited_offers', 'Retrieve most favorited offers'),
		('read_personalized_offers', 'Retrieve personalized offers for a user'),
		('read_recently_favorited_offer', 'Retrieve the most recently favorited offer by user'),
		('read_trending_offers_for_user', 'Retrieve trending offers for a user'),
		('read_user_offer_purchase_history', 'Retrieve the purchase history of offers for a user'),
		('read_user_favorite_by_id', 'Retrieve a user favorite by ID'),
		('read_user_favorites', 'Retrieve active user favorites by user ID'),
		('read_user_favorites_count', 'Retrieve the count of user favorites for a specific offer'),
		('read_users_who_favorited_offer', 'Retrieve users who favorited an offer'),
		('recommend_offers', 'Recommend offers to a user'),
		('request_restock_notification', 'Request a restock notification'),
		('save_user_favorite', 'Save a user''s favorite offer'),
		('set_offer_alert', 'Set an alert for an offer'),
		('share_offer', 'Share an offer'),
		('share_favorite_list', 'Share a user''s favorite list with another user'),
		('soft_delete_user_favorite', 'Soft delete a user favorite'),
		('track_favorite_categories', 'Track user''s favorite categories'),
		('unsave_user_favorite', 'Unsave a user favorite item'),
		-- User Notifications
		('delete_user_notification', 'Delete a user notification'),
		('notify_user', 'Send user notification'),
		('read_user_notifications_by_offer', 'Retrieve user notifications by offer ID'),
		('read_user_notification', 'Retrieve a user notification by id'),
		('read_user_notifications_by_type', 'Retrieve user notifications by type id'),
		('read_user_notifications_by_user', 'Retrieve all user notifications for a specific user id'),
		('update_user_notification', 'Update a user notification'),
		-- User  Profiles
		('create_user_profile', 'Create a new user profile'),
		('delete_user_profile', 'Delete a user profile'),
		('moderate_user_profile', 'Flag or unflag user profile for violations'),
		('read_reserved_handles', 'Retrieve reserved user handles for validation UI'),
		('read_user_profile', 'Retrieve current user profile'),
		('read_user_profile_by_user_id', 'Retrieve a user profile by user ID'),
		('read_user_profiles', 'Retrieve multiple user profiles via search or listing'),
		('soft_delete_user_profile', 'Soft delete a user profile'),
		('update_user_profile', 'Update a user profile'),
		-- User Settings
		('delete_user_settings', 'Delete user settings'),
		('read_user_settings', 'Read user settings'),
		('save_user_settings', 'Save user settings'),
		('soft_delete_user_settings', 'Soft delete user settings'),
		('update_user_settings', 'Update user settings'),
		-- User Wallets
		('create_user_wallet', 'Create a new user wallet'),
		('deactivate_user_wallet', 'Deactivate a user wallet'),
		('delete_user_wallet', 'Delete a user wallet'),
		('read_user_wallet', 'Retrieve a user wallet by ID'),
		('read_user_wallet_by_user_id', 'Retrieve a user wallet by user ID'),
		('read_user_wallets', 'Retrieve all or multiple user wallets'),
		('soft_delete_user_wallet', 'Soft delete a user wallet'),
		-- Auth workflow actions
		('send_activation_link', 'Send activation link to a newly registered user'),
		('activate_user', 'Activate a user account based on a valid activation token'),
		('get_user_activation_status', 'Check if a user has completed account activation'),
		('save_user_consent', 'Record user''s consent to terms and policies'),
		('register_user', 'Register a new user account'),
		('login', 'Log in a user and generate authentication tokens'),
		('logout', 'Log out a user and revoke authentication tokens'),
		-- Users
		('delete_own_account', 'Delete own user account'),
		('expel_user', 'Expel existing user account')
	ON CONFLICT (name) DO NOTHING;
	`
	insertReasonQuery = `
	INSERT INTO reasons (name, description) VALUES
		('#1 Best Seller', 'This offer is the top seller in its category.'),
		('Trending Now', 'This offer is currently trending based on user engagement.'),
		('Editor Pick', 'This offer has been manually selected as a top recommendation.')
	ON CONFLICT (name) DO NOTHING;
	`
	insertNotificationTypesQuery = `
	INSERT INTO notification_types (type, description) VALUES
		('price_drop', 'Notify a user when a watched offer drops in price'),
		('new_offer', 'Notify a user when a newly relevant offer becomes available')
	ON CONFLICT (type) DO NOTHING;
	`
	insertNotificationChannelsQuery = `
	INSERT INTO notification_channels (channel, description) VALUES
		('email', 'Send a notification by email'),
		('push', 'Send a push notification to a registered device'),
		('in_app', 'Display a notification inside the application UI')
	ON CONFLICT (channel) DO NOTHING;
	`
	insertDepartmentsQuery = `
	-- Departments (top level)
	INSERT INTO departments (name, slug, description, sort_order) VALUES
		('Electronics', 'electronics', NULL, 10),
		('Home, Kitchen & Garden', 'home-kitchen-garden', NULL, 20),
		('Fashion','fashion',NULL,30),
		('Beauty, Health & Personal Care', 'beauty-health-personal-care', NULL, 40),
		('Sports, Fitness & Outdoors', 'sports-fitness-outdoors', NULL, 50),
		('Toys, Kids & Baby', 'toys-kids-baby', NULL, 60),
		('Automotive & Industrial', 'automotive-industrial', NULL, 70),
		('Travel & Experiences', 'travel-experiences', NULL, 80),
		('Grocery & Gourmet Food', 'grocery-gourmet-food', NULL, 90),
		('Entertainment & Digital', 'entertainment-digital', NULL, 100)
	ON CONFLICT (slug) DO NOTHING;
	`
	insertCategoryLevel1Query = `
	-- Level 1 Category under a Department
	INSERT INTO categories (name, slug, department_id, parent_id, sort_order) VALUES
	-- Electronics (Department)
		('Computers & Accessories','computers-accessories',
			(SELECT id FROM departments WHERE slug='electronics'), NULL, 100),
		('TV, Video & Home Audio','tv-video-home-audio',
			(SELECT id FROM departments WHERE slug='electronics'), NULL, 110),
		('Cell Phones & Accessories','cell-phones-accessories',
			(SELECT id FROM departments WHERE slug='electronics'), NULL, 120),
		('Cameras & Photography','cameras-photography',
			(SELECT id FROM departments WHERE slug='electronics'), NULL, 130),
		('Video Games & Accessories','video-games-accessories',
			(SELECT id FROM departments WHERE slug='electronics'), NULL, 140),
	-- Home, Kitchen & Garden (Department)
		('Furniture','furniture',(SELECT id FROM departments WHERE slug='home-kitchen-garden'),NULL,100),
		('Kitchen & Dining','kitchen-dining',(SELECT id FROM departments WHERE slug='home-kitchen-garden'),NULL,110),
		('Bedding & Bath','bedding-bath',(SELECT id FROM departments WHERE slug='home-kitchen-garden'),NULL,120),
		('Home Décor','home-decor',(SELECT id FROM departments WHERE slug='home-kitchen-garden'),NULL,130),
		('Garden & Outdoor','garden-outdoor',(SELECT id FROM departments WHERE slug='home-kitchen-garden'),NULL,140),
	-- Fashion (Department)
		('Women','women',(SELECT id FROM departments WHERE slug='fashion'),NULL,100),
		('Men','men',(SELECT id FROM departments WHERE slug='fashion'),NULL,110),
		('Kids & Baby','kids-baby',(SELECT id FROM departments WHERE slug='fashion'),NULL,120),
		('Unisex','unisex',(SELECT id FROM departments WHERE slug='fashion'),NULL,130),
	-- Beauty, Health & Personal Care (Department)
		('Beauty','beauty',(SELECT id FROM departments WHERE slug='beauty-health-personal-care'),NULL,100),
		('Health','health',(SELECT id FROM departments WHERE slug='beauty-health-personal-care'),NULL,110),
		('Personal Care','personal-care',(SELECT id FROM departments WHERE slug='beauty-health-personal-care'),NULL,120),
	-- Sports, Fitness & Outdoors (Department)
		('Exercise & Fitness','exercise-fitness',(SELECT id FROM departments WHERE slug='sports-fitness-outdoors'),NULL,100),
		('Outdoor Recreation','outdoor-recreation',(SELECT id FROM departments WHERE slug='sports-fitness-outdoors'),NULL,110),
		('Team Sports','team-sports',(SELECT id FROM departments WHERE slug='sports-fitness-outdoors'),NULL,120),
	-- Toys, Kids & Baby (Department)
		('Toys','toys',(SELECT id FROM departments WHERE slug='toys-kids-baby'),NULL,100),
		('Games & Puzzles','games-puzzles',(SELECT id FROM departments WHERE slug='toys-kids-baby'),NULL,110),
		('Baby Gear','baby-gear',(SELECT id FROM departments WHERE slug='toys-kids-baby'),NULL,120),
	-- Automotive & Industrial (Department)
		('Automotive','automotive',(SELECT id FROM departments WHERE slug='automotive-industrial'),NULL,100),
		('Industrial & Commercial','industrial-commercial',(SELECT id FROM departments WHERE slug='automotive-industrial'),NULL,110),
	-- Travel & Experiences (Department)
		('Flights','flights',(SELECT id FROM departments WHERE slug='travel-experiences'),NULL,100),
		('Hotels & Accommodation','hotels-accommodation',(SELECT id FROM departments WHERE slug='travel-experiences'),NULL,110),
		('Cruises','cruises',(SELECT id FROM departments WHERE slug='travel-experiences'),NULL,120),
		('Tours & Activities','tours-activities',(SELECT id FROM departments WHERE slug='travel-experiences'),NULL,130),
		('Car Rentals','car-rentals',(SELECT id FROM departments WHERE slug='travel-experiences'),NULL,140),
	-- Grocery & Gourmet Food (Department)
		('Pantry Staples','pantry-staples',(SELECT id FROM departments WHERE slug='grocery-gourmet-food'),NULL,100),
		('Snacks','snacks',(SELECT id FROM departments WHERE slug='grocery-gourmet-food'),NULL,110),
		('Beverages','beverages',(SELECT id FROM departments WHERE slug='grocery-gourmet-food'),NULL,120),
		('Specialty Foods','specialty-foods',(SELECT id FROM departments WHERE slug='grocery-gourmet-food'),NULL,130),
	-- Entertainment & Digital (Department)
		('Books','books',(SELECT id FROM departments WHERE slug='entertainment-digital'),NULL,100),
		('Movies & TV','movies-tv',(SELECT id FROM departments WHERE slug='entertainment-digital'),NULL,110),
		('Music','music',(SELECT id FROM departments WHERE slug='entertainment-digital'),NULL,120),
		('Video Games','video-games',(SELECT id FROM departments WHERE slug='entertainment-digital'),NULL,130),
		('Streaming Subscriptions','streaming-subscriptions',(SELECT id FROM departments WHERE slug='entertainment-digital'),NULL,140)
	ON CONFLICT (slug) DO NOTHING;
	`
	insertCategoryLevel2Query = `
	-- Level 2: nested under a Level 1 Category
	INSERT INTO categories (name, slug, department_id, parent_id, sort_order) VALUES
	-- Electronics ▸ Computers & Accessories
		('Laptops','laptops',
			(SELECT id FROM departments WHERE slug='electronics'),
			(SELECT id FROM categories WHERE slug='computers-accessories'),100),
		('Desktops','desktops',
			(SELECT id FROM departments WHERE slug='electronics'),
			(SELECT id FROM categories WHERE slug='computers-accessories'),110),
		('Monitors','monitors',
			(SELECT id FROM departments WHERE slug='electronics'),
			(SELECT id FROM categories WHERE slug='computers-accessories'),120),
		('Computer Components','computer-components',
			(SELECT id FROM departments WHERE slug='electronics'),
			(SELECT id FROM categories WHERE slug='computers-accessories'),130),
		('Storage Devices','storage-devices',
			(SELECT id FROM departments WHERE slug='electronics'),
			(SELECT id FROM categories WHERE slug='computers-accessories'),140),
		('Networking','networking',
			(SELECT id FROM departments WHERE slug='electronics'),
			(SELECT id FROM categories WHERE slug='computers-accessories'),150),
	-- Electronics ▸ TV, Video & Home Audio
		('Televisions','televisions',(SELECT id FROM departments WHERE slug='electronics'),
			(SELECT id FROM categories WHERE slug='tv-video-home-audio'),100),
		('Projectors','projectors',(SELECT id FROM departments WHERE slug='electronics'),
			(SELECT id FROM categories WHERE slug='tv-video-home-audio'),110),
		('Streaming Devices','streaming-devices',(SELECT id FROM departments WHERE slug='electronics'),
			(SELECT id FROM categories WHERE slug='tv-video-home-audio'),120),
		('Soundbars & Speakers','soundbars-speakers',(SELECT id FROM departments WHERE slug='electronics'),
			(SELECT id FROM categories WHERE slug='tv-video-home-audio'),130),
	-- Fashion ▸ Women
		('Clothing','women-clothing',(SELECT id FROM departments WHERE slug='fashion'),
			(SELECT id FROM categories WHERE slug='women'),100),
		('Shoes','women-shoes',(SELECT id FROM departments WHERE slug='fashion'),
			(SELECT id FROM categories WHERE slug='women'),110),
		('Accessories','women-accessories',(SELECT id FROM departments WHERE slug='fashion'),
			(SELECT id FROM categories WHERE slug='women'),120),
		('Jewelry','women-jewelry',(SELECT id FROM departments WHERE slug='fashion'),
			(SELECT id FROM categories WHERE slug='women'),130),
	-- Fashion ▸ Men
		('Clothing','men-clothing',(SELECT id FROM departments WHERE slug='fashion'),
			(SELECT id FROM categories WHERE slug='men'),100),
		('Shoes','men-shoes',(SELECT id FROM departments WHERE slug='fashion'),
			(SELECT id FROM categories WHERE slug='men'),110),
		('Accessories','men-accessories',(SELECT id FROM departments WHERE slug='fashion'),
			(SELECT id FROM categories WHERE slug='men'),120),
		('Watches','men-watches',(SELECT id FROM departments WHERE slug='fashion'),
			(SELECT id FROM categories WHERE slug='men'),130),
	-- Fashion ▸ Kids & Baby
		('Clothing','kids-clothing',(SELECT id FROM departments WHERE slug='fashion'),
			(SELECT id FROM categories WHERE slug='kids-baby'),100),
		('Shoes','kids-shoes',(SELECT id FROM departments WHERE slug='fashion'),
			(SELECT id FROM categories WHERE slug='kids-baby'),110),
		('Accessories','kids-accessories',(SELECT id FROM departments WHERE slug='fashion'),
			(SELECT id FROM categories WHERE slug='kids-baby'),120),
	-- Travel & Experiences ▸ Flights
		('Domestic','flights-domestic',(SELECT id FROM departments WHERE slug='travel-experiences'),
			(SELECT id FROM categories WHERE slug='flights'),100),
		('International','flights-international',(SELECT id FROM departments WHERE slug='travel-experiences'),
			(SELECT id FROM categories WHERE slug='flights'),110),
	-- Travel & Experiences ▸ Hotels & Accommodation
		('Budget Hotels','budget-hotels',(SELECT id FROM departments WHERE slug='travel-experiences'),
			(SELECT id FROM categories WHERE slug='hotels-accommodation'),100),
		('Luxury Hotels','luxury-hotels',(SELECT id FROM departments WHERE slug='travel-experiences'),
			(SELECT id FROM categories WHERE slug='hotels-accommodation'),110),
		('Vacation Rentals','vacation-rentals',(SELECT id FROM departments WHERE slug='travel-experiences'),
			(SELECT id FROM categories WHERE slug='hotels-accommodation'),120),
		('Boutique Hotels','boutique-hotels',(SELECT id FROM departments WHERE slug='travel-experiences'),
			(SELECT id FROM categories WHERE slug='hotels-accommodation'),130)
	ON CONFLICT (slug) DO NOTHING;
	`
	insertCategoryLevel3Query = `
	-- Level 3 seed blocks nested under a Level 2 Category
	INSERT INTO categories (name, slug, department_id, parent_id, sort_order) VALUES
	-- Laptops -> Level 2
		('Gaming Laptops','gaming-laptops',
			(SELECT id FROM departments WHERE slug='electronics'),
			(SELECT id FROM categories WHERE slug='laptops'),100),
		('Business Laptops','business-laptops',
			(SELECT id FROM departments WHERE slug='electronics'),
			(SELECT id FROM categories WHERE slug='laptops'),110),
		('Student Laptops','student-laptops',
			(SELECT id FROM departments WHERE slug='electronics'),
			(SELECT id FROM categories WHERE slug='laptops'),120),
	-- Televisions -> Level 2
		('4K TVs','4k-tvs',(SELECT id FROM departments WHERE slug='electronics'),
			(SELECT id FROM categories WHERE slug='televisions'),100),
		('8K TVs','8k-tvs',(SELECT id FROM departments WHERE slug='electronics'),
			(SELECT id FROM categories WHERE slug='televisions'),110),
		('OLED TVs','oled-tvs',(SELECT id FROM departments WHERE slug='electronics'),
			(SELECT id FROM categories WHERE slug='televisions'),120),
		('QLED TVs','qled-tvs',(SELECT id FROM departments WHERE slug='electronics'),
			(SELECT id FROM categories WHERE slug='televisions'),130),
	-- Men -> Shoes -> Level 2
		('Sneakers','men-shoes-sneakers',(SELECT id FROM departments WHERE slug='fashion'),
			(SELECT id FROM categories WHERE slug='men-shoes'),100),
		('Dress Shoes','men-shoes-dress',(SELECT id FROM departments WHERE slug='fashion'),
			(SELECT id FROM categories WHERE slug='men-shoes'),110),
		('Boots','men-shoes-boots',(SELECT id FROM departments WHERE slug='fashion'),
			(SELECT id FROM categories WHERE slug='men-shoes'),120),
	-- Women -> Clothing -> Level 2
		('Dresses','women-clothing-dresses',(SELECT id FROM departments WHERE slug='fashion'),
			(SELECT id FROM categories WHERE slug='women-clothing'),100),
		('Tops','women-clothing-tops',(SELECT id FROM departments WHERE slug='fashion'),
			(SELECT id FROM categories WHERE slug='women-clothing'),110),
		('Activewear','women-clothing-activewear',(SELECT id FROM departments WHERE slug='fashion'),
			(SELECT id FROM categories WHERE slug='women-clothing'),120),
	-- Women ▸ Shoes -> Level 2
		('Sneakers','women-shoes-sneakers',(SELECT id FROM departments WHERE slug='fashion'),
			(SELECT id FROM categories WHERE slug='women-shoes'),100),
		('Boots','women-shoes-boots',(SELECT id FROM departments WHERE slug='fashion'),
			(SELECT id FROM categories WHERE slug='women-shoes'),110),
		('Heels','women-shoes-heels',(SELECT id FROM departments WHERE slug='fashion'),
			(SELECT id FROM categories WHERE slug='women-shoes'),120),
	-- Kids ▸ Clothing -> Level 2
		('Girls Clothing','kids-girls-clothing',(SELECT id FROM departments WHERE slug='fashion'),
			(SELECT id FROM categories WHERE slug='kids-clothing'),100),
		('Boys Clothing','kids-boys-clothing',(SELECT id FROM departments WHERE slug='fashion'),
			(SELECT id FROM categories WHERE slug='kids-clothing'),110),
		('Baby Clothing','baby-clothing',(SELECT id FROM departments WHERE slug='fashion'),
			(SELECT id FROM categories WHERE slug='kids-clothing'),120),
	-- Flights -> International -> Level 2
		('Europe','flights-international-europe',(SELECT id FROM departments WHERE slug='travel-experiences'),
			(SELECT id FROM categories WHERE slug='flights-international'),100),
		('Africa','flights-international-africa',(SELECT id FROM departments WHERE slug='travel-experiences'),
			(SELECT id FROM categories WHERE slug='flights-international'),110),
		('Asia','flights-international-asia',(SELECT id FROM departments WHERE slug='travel-experiences'),
			(SELECT id FROM categories WHERE slug='flights-international'),120)
	ON CONFLICT (slug) DO NOTHING;
	`

	insertMerchantAccountRolesQuery = `
	INSERT INTO merchant_account_roles (code, name, description) VALUES
		('owner', 'Owner', 'Primary merchant account owner with full account authority.'),
		('admin', 'Admin', 'Merchant account administrator with broad management authority.'),
		('billing', 'Billing', 'Merchant account billing contact with billing and payment responsibilities.'),
		('campaign_manager', 'Campaign Manager', 'Merchant user responsible for campaigns, launches, promotions, and offer operations.'),
		('viewer', 'Viewer', 'Read-only merchant account user.')
	ON CONFLICT (code) DO NOTHING;
	`

	insertPlatformSettingsQuery = `
	INSERT INTO platform_settings (
		setting_key,
		setting_value,
		value_type,
		description
	) VALUES
		(
			'future_offering_enabled',
			'true',
			'boolean',
			'Enables or disables Future Offering functionality platform-wide.'
		),
		(
			'future_offering_watch_enabled',
			'true',
			'boolean',
			'Enables or disables the Watch action for Future Offerings.'
		),
		(
			'future_offering_creation_enabled',
			'true',
			'boolean',
			'Allows merchants to create Future Offerings.'
		),

		-- Merchant onboarding

		(
			'merchant_onboarding_enabled',
			'true',
			'boolean',
			'Allows new merchants to onboard.'
		),
		(
			'merchant_debit_card_required_at_onboarding',
			'false',
			'boolean',
			'Controls whether a merchant must provide a debit card during onboarding.'
		),
		(
			'merchant_debit_card_required_for_future_offering_fee',
			'true',
			'boolean',
			'Controls whether a merchant must have a debit card before Future Offering fee exposure. If debit card is required during onboarding, this setting is inherited as true.'
		),

		-- Merchant Center

		(
			'merchant_center_enabled',
			'true',
			'boolean',
			'Enables Merchant Center.'
		),
		(
			'merchant_center_future_offerings_enabled',
			'true',
			'boolean',
			'Enables Future Offering management within Merchant Center.'
		),

		-- User engagement

		(
			'user_trend_engagements_enabled',
			'true',
			'boolean',
			'Enables user engagement with Future Offerings, including Watch and all supported engagement options.'
		),
		(
			'consumer_notification_preferences_enabled',
			'true',
			'boolean',
			'Allows users to manage notification preferences for engagement-driven events.'
		),

		-- Platform administration

		(
			'platform_settings_admin_enabled',
			'true',
			'boolean',
			'Allows administrators to manage platform settings.'
		),
		(
			'platform_settings_hard_delete_enabled',
			'false',
			'boolean',
			'Allows permanent deletion of platform settings. Disabled by default for safety.'
		)

	ON CONFLICT (setting_key) DO NOTHING;
	`

	insertMerchantProgramBenefitsQuery = `
	INSERT INTO merchant_program_benefits (
		code,
		name,
		description,
		benefit_type,
		value_json
	) VALUES
		(
			'founding_setup_fee_waiver',
			'Founding Merchant Setup Fee Waiver',
			'Waives the setup fee for approved founding merchants.',
			'setup_fee_waiver',
			'{"waiver_type":"full"}'::jsonb
		),
		(
			'founding_free_subscription',
			'Founding Merchant Free Subscription',
			'Provides complimentary subscription access for approved founding merchants.',
			'subscription_free_months',
			'{"months":3}'::jsonb
		),
		(
			'founding_future_offering_credit',
			'Founding Merchant Future Offering Credit',
			'Provides Future Offering credit for approved founding merchants.',
			'future_offering_credit',
			'{"amount":"300.00","currency":"USD"}'::jsonb
		)
	ON CONFLICT (code) DO NOTHING;
	`
)

// seedSpec pairs a human-readable seed name with its SQL and optional bind
// arguments for ordered atomic seeding.
type seedSpec struct {
	name  string
	query string
	args  []any
}

// SeedAllData inserts all required static data in a single transaction.
// Failure at any step rolls back the entire seed operation, keeping the
// database in a consistent all-or-nothing state.
func (m *DBConnectionParamsModel) SeedAllData(
	db *pgxpool.Pool,
	oauthSecrets OAuthClientSeedSecrets,
) error {
	if m == nil {
		return fmt.Errorf("seed data: DBConnectionParamsModel is nil")
	}
	if m.Logger == nil {
		return fmt.Errorf("seed data: logger is nil")
	}
	if db == nil {
		return fmt.Errorf("seed data: database pool is nil")
	}

	webClientSecret := strings.TrimSpace(oauthSecrets.WebClientSecret)
	if webClientSecret == "" {
		return fmt.Errorf("seed data: web OAuth client secret is required")
	}

	mobileClientSecret := strings.TrimSpace(oauthSecrets.MobileClientSecret)
	if mobileClientSecret == "" {
		return fmt.Errorf("seed data: mobile OAuth client secret is required")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("SeedAllData")
	logger.Info("Beginning database seed operation")

	seeds := []seedSpec{
		{name: "roles", query: insertRolesQuery},
		{name: "permissions", query: insertPermissionsQuery},
		{name: "merchant_application_status", query: insertMerchantApplicationStatusQuery},
		{name: "merchant_account_roles", query: insertMerchantAccountRolesQuery},
		{name: "merchant_program_plans", query: insertMerchantProgramPlansQuery},
		{name: "merchant_program_entitlements", query: insertMerchantProgramEntitlementsQuery},
		{name: "coupon_statuses", query: insertCouponStatusQuery},
		{name: "promotions", query: insertPromotionsQuery},
		{name: "offer_statuses", query: insertOfferStatusQuery},
		{name: "sponsorship_bid_types", query: insertSponsorshipBidTypesQuery},
		{name: "sponsorship_bid_minimums", query: insertSponsorshipBidMinimumsQuery},
		{name: "market_segments", query: insertMarketSegmentQuery},
		{name: "merchant_types", query: insertMerchantTypeQuery},
		{name: "platforms", query: insertPlatformQuery},
		{name: "merchants", query: insertMerchantQuery},
		{name: "affiliate_programs", query: insertAffiliateProgramQuery},
		{name: "brands", query: insertBrandQuery},
		{name: "social_platforms", query: insertSocialPlatformQuery},
		{name: "entity_types", query: insertEntityTypeQuery},
		{name: "actions", query: insertActionQuery},
		{name: "reasons", query: insertReasonQuery},
		{name: "notification_types", query: insertNotificationTypesQuery},
		{name: "notification_channels", query: insertNotificationChannelsQuery},
		{name: "value_tags", query: insertValueTagQuery},
		{name: "audiences", query: insertAudienceQuery},
		{name: "seasonal_relevances", query: insertSeasonalRelevanceQuery},
		{name: "dashboard_templates", query: insertDashboardTemplateQuery},
		{name: "departments", query: insertDepartmentsQuery},
		{name: "categories_level_1", query: insertCategoryLevel1Query},
		{name: "categories_level_2", query: insertCategoryLevel2Query},
		{name: "categories_level_3", query: insertCategoryLevel3Query},
		{name: "offers", query: insertOfferDataQuery},
		{name: "role_permissions", query: insertRolePermissionsQuery},
		{
			name:  "oauth_clients",
			query: insertOAuthClientQuery,
			args:  []any{webClientSecret, mobileClientSecret},
		},
	}

	tx, err := db.Begin(ctx)
	if err != nil {
		logger.Error("Failed to begin seed transaction", "error", err)
		return fmt.Errorf("begin seed transaction: %w", err)
	}

	committed := false
	defer func() {
		if committed {
			return
		}
		if rbErr := tx.Rollback(ctx); rbErr != nil && !errors.Is(rbErr, pgx.ErrTxClosed) {
			logger.Warn("Seed rollback failed", "error", rbErr)
		}
	}()

	for _, seed := range seeds {
		if err := m.insertSeedDataTx(ctx, tx, seed); err != nil {
			return err
		}
	}

	if err := tx.Commit(ctx); err != nil {
		logger.Error("Failed to commit seed transaction", "error", err)
		return fmt.Errorf("commit seed transaction: %w", err)
	}
	committed = true

	logger.Info("Database seed operation completed successfully")
	return nil
}

// insertSeedDataTx executes one seed statement inside an existing transaction.
// Callers must use SeedAllData to preserve atomicity and seed ordering.
func (m *DBConnectionParamsModel) insertSeedDataTx(
	ctx context.Context,
	tx pgx.Tx,
	seed seedSpec,
) error {
	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("insertSeedDataTx")
	logger.Infof("Seeding %s", seed.name)

	tag, err := tx.Exec(ctx, seed.query, seed.args...)
	if err != nil {
		logger.Error("Failed to seed table", "table", seed.name, "error", err)
		return fmt.Errorf("seed %s: %w", seed.name, err)
	}

	logger.Infof("Seeded %s (%d rows affected)", seed.name, tag.RowsAffected())
	return nil
}
