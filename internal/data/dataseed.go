// Package data owns canonical seed installation and restart-safe catalog replacement.
// focodebase/fobackend/internal/data/dataseed.go
// BUSINESS-NEUTRAL SOURCE RULE — BEG 3.17–3.18
// CE and engineering own this naming rule. Use platform for neutral internal
// identifiers; use the platform in general UI prose. Capitalize Platform only
// where a heading, sentence position, or defined label requires it. When naming
// an installation's website or business, use its administrator-configured identity.
// These choices must be implemented and reviewed in source, not delegated to the
// project owner as editing guidance.
// Do not hardcode an installation's brand in identifiers, CSS tokens, public
// copy, defaults, or seed data. Public identity and business wording must come
// from administrator-managed configuration. Internal names describe purpose.
// Pre-startup call-site compatibility is not justification for weak naming.
// Repair dependent callers together. An exception requires a documented,
// intentional public, production, or migration-stable contract and CE review.
// Run tools/check_business_neutrality.py before source release. Existing
// violations are not automatically grandfathered or approved.

//
// Package data provides models and database access methods for seed data
// operations and other entities.
//
// focodebase/fobackend/internal/data/dataseed.go
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

// OAuthClientSeedSecrets carries optional OAuth client secrets from bootstrap
// into the data seed layer. OAuth seed data is created only when OAuth is
// explicitly configured.
type OAuthClientSeedSecrets struct {
	WebClientSecret    string
	MobileClientSecret string
}

// superAdminOnlyPermissionList is the SQL literal list of permissions held by
// super_admin but never by admin. Hierarchy (Root > Super Admin > Admin) is
// additionally enforced per target by the governance service; these
// permissions are the coarse route gate.
const superAdminOnlyPermissionList = `
        'read_site_maintenance',
        'manage_site_maintenance',
		'appoint_admin',
		'appoint_super_admin',
		'demote_administrator',
		'suspend_administrator',
		'restore_administrator',
		'revoke_administrator',
		'create_role',
		'update_role',
		'delete_role',
		'update_role_permissions'
	`

const (
	// ---------------------------------------------------------------
	// revoke_admin_governance_permissions
	//
	// Earlier seeds granted admin every permission. Seeding only adds rows,
	// so the Super-Admin-tier set is removed from admin explicitly and
	// idempotently on every startup.
	// ---------------------------------------------------------------
	revokeAdminGovernancePermissionsQuery = `
	DELETE FROM role_permissions rp
	USING roles r, permissions p
	WHERE rp.role_id = r.id
	  AND rp.permission_id = p.id
	  AND r.name = 'admin'
	  AND p.name IN (` + superAdminOnlyPermissionList + `);
	`

	// ---------------------------------------------------------------
	// role_permissions
	//
	// admin    — all permissions
	// consumer — public consumer capabilities and own-account actions
	// merchant — merchant-owned Future Offering and account capabilities
	// ---------------------------------------------------------------
	insertRolePermissionsQuery = `
	INSERT INTO role_permissions (role_id, permission_id)

	-- super_admin: every permission, always (re-run on every startup so new
	-- permissions are granted automatically). Protected by trigger.
	SELECT r.id, p.id
	FROM roles r
	CROSS JOIN permissions p
	WHERE r.name = 'super_admin'

	UNION ALL

	-- admin: every permission except the Super-Admin-tier governance set.
	SELECT r.id, p.id
	FROM roles r
	CROSS JOIN permissions p
	WHERE r.name = 'admin'
	  AND p.name NOT IN (` + superAdminOnlyPermissionList + `)

	UNION ALL

	-- consumer
	SELECT r.id, p.id
	FROM roles r
	JOIN permissions p ON p.name IN (
		'follow_merchant',
		'unfollow_merchant',
		'read_followed_merchants_future_offerings',
		'recommend_future_offerings',
		'request_new_fo_notification',
		'read_user_notification',
		'read_user_notifications',
		'soft_delete_user_notification',
		'create_user_profile',
		'read_user_profile',
		'update_user_profile',
		'soft_delete_user_profile',
		'delete_own_account'
	)
	WHERE r.name = 'consumer'

	UNION ALL

	-- merchant
	SELECT r.id, p.id
	FROM roles r
	JOIN permissions p ON p.name IN (
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

		'create_merchant_future_offering',
		'read_merchant_future_offering',
		'list_merchant_future_offerings',
		'update_merchant_future_offering_draft',
		'discard_merchant_future_offering_draft',
		'list_engagement_actions',
		'list_categories',

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
	insertEngagementActionsQuery = `
	INSERT INTO engagement_actions (
		code,
		name
	)
	VALUES
		('waitlist', 'Waitlist'),
		('early_access_request', 'Early Access Request'),
		('beta', 'Beta'),
		('reservation_interest', 'Reservation Interest'),
		('preorder_intent', 'Preorder Intent'),
		('signup_intent', 'Signup Intent'),
		('enrollment_intent', 'Enrollment Intent'),
		('application_intent', 'Application Intent'),
		('subscription_intent', 'Subscription Intent'),
		('booking_intent', 'Booking Intent'),
		('purchase_intent', 'Purchase Intent')
	ON CONFLICT (code) DO NOTHING;
`
	insertMerchantFeeTypesQuery = `
	INSERT INTO merchant_fee_types (code, display_name) VALUES
		('anticipation_intelligence_activation_fee', 'Anticipation Intelligence Activation Fee'),
		('platform_service_fee', 'Platform Service Fee'),
		('anticipation_intelligence_fee', 'Anticipation Intelligence Fee')
	ON CONFLICT (code) DO NOTHING;
	`
	insertMerchantBillableEventFeeTypesQuery = `
	INSERT INTO merchant_billable_event_fee_types (
		billable_event_type,
		fee_type_id
	)
	SELECT
		mapping.billable_event_type,
		mft.id
	FROM (
		VALUES
			('anticipation_intelligence_activation', 'anticipation_intelligence_activation_fee'),
			('platform_service', 'platform_service_fee'),
			('watch', 'anticipation_intelligence_fee'),
			('waitlist', 'anticipation_intelligence_fee'),
			('early_access_request', 'anticipation_intelligence_fee'),
			('beta', 'anticipation_intelligence_fee'),
			('reservation_interest', 'anticipation_intelligence_fee'),
			('preorder_intent', 'anticipation_intelligence_fee'),
			('signup_intent', 'anticipation_intelligence_fee'),
			('enrollment_intent', 'anticipation_intelligence_fee'),
			('application_intent', 'anticipation_intelligence_fee'),
			('subscription_intent', 'anticipation_intelligence_fee'),
			('booking_intent', 'anticipation_intelligence_fee'),
			('purchase_intent', 'anticipation_intelligence_fee')
	) AS mapping (
		billable_event_type,
		fee_type_code
	)
	JOIN merchant_fee_types AS mft
		ON mft.code = mapping.fee_type_code
	ON CONFLICT (billable_event_type) DO NOTHING;
	`
	insertRolesQuery = `
	INSERT INTO roles (name, description, hierarchy_level, is_internal, assignable_at_signup, approval_required) VALUES
		('super_admin', 'Administrative governance role. Appoints Admins and Super Admins. Held by the Root Super Admin.', 4, TRUE, FALSE, FALSE),
		('admin', 'Administrative role for operating the platform. Cannot govern administrators.', 3, TRUE, FALSE, FALSE),
		('editor', 'Role for editing products, coupons, and content, but cannot manage users.', 2, TRUE, FALSE, FALSE),
		('OfferCurator', 'Role for reviewing, selecting, and tagging good offers. Cannot manage users.', 1, TRUE, FALSE, FALSE),
		('QualityModerator', 'Role for flagging low-quality or expired content. Cannot manage users.', 1, TRUE, FALSE, FALSE),
		('CampaignManager', 'Role for overseeing merchant campaigns and sponsorships. Cannot manage users.', 1, TRUE, FALSE, FALSE),
		('viewer', 'Role for read-only access. Useful for analytics or auditing.', 1, TRUE, FALSE, FALSE),
		('consumer', 'Role can log in, track their clicks, save favorite products, or access exclusive offers.', 0, FALSE, TRUE, FALSE),
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
		-- Audit Logs
		('read_audit_log', 'Allows reading audit logs'),
		('read_archived_audit_log', 'Allows reading archived audit logs'),
		-- Audit Metadata
		('read_entity_type', 'Allows reading entity types'),
		('read_action', 'Allows reading action metadata'),
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
		-- Merchants
		('create_merchant', 'Allows creating a new merchant'),
		('read_merchant', 'Allows reading merchant info'),
		('list_merchants', 'Allows listing merchants'),
		('update_merchant', 'Allows updating merchant info'),
		('soft_delete_merchant', 'Allows soft-deleting merchants'),
		('delete_merchant', 'Allows permanently deleting a merchant'),
		-- Merchant Accounts
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
		-- Merchant Future Offering Service Terms
		(
			'propose_merchant_future_offering_service_term',
			'Allows proposing a Future Offering Service Term'
		),
		(
			'read_merchant_future_offering_service_term',
			'Allows privileged reading of a Future Offering Service Term'
		),
		(
			'list_merchant_future_offering_service_terms',
			'Allows privileged listing of Future Offering Service Term history'
		),
		(
			'update_merchant_future_offering_service_term_proposal',
			'Allows replacing the mutable facts of a proposed Future Offering Service Term'
		),
		(
			'establish_merchant_future_offering_service_term',
			'Allows establishing a proposed Future Offering Service Term'
		),
		(
			'replace_merchant_future_offering_service_term',
			'Allows atomically replacing an established Future Offering Service Term'
		),
		-- Merchant Future Offering Service Periods
		(
			'read_merchant_future_offering_service_period',
			'Allows privileged reading of a Future Offering Service Period'
		),
		(
			'list_merchant_future_offering_service_periods',
			'Allows privileged listing of Future Offering Service Period history'
		),
		-- Merchant Future Offering Billing Periods
		(
			'read_merchant_future_offering_billing_period',
			'Allows privileged reading of a Future Offering Billing Period'
		),
		(
			'list_merchant_future_offering_billing_periods',
			'Allows privileged listing of Future Offering Billing Period history'
		),
		-- Merchant Platform Credit Accounts
		('create_merchant_platform_credit_account', 'Allows creating merchant platform credit accounts'),
		('read_merchant_platform_credit_account', 'Allows reading merchant platform credit accounts'),
		('list_merchant_platform_credit_accounts', 'Allows listing merchant platform credit accounts'),
		(
			'update_merchant_platform_credit_account_descriptive_fields',
			'Allows replacing merchant platform credit account descriptive metadata'
		),
		('cancel_merchant_platform_credit_account', 'Allows cancelling merchant platform credit accounts'),
		-- Merchant Platform Credit Eligible Fee Types
		(
			'create_merchant_platform_credit_eligible_fee_type',
			'Allows creating a merchant platform credit fee-type eligibility association'
		),
		(
			'read_merchant_platform_credit_eligible_fee_type',
			'Allows reading a merchant platform credit fee-type eligibility association'
		),
		(
			'list_merchant_platform_credit_eligible_fee_types',
			'Allows listing the fee-type eligibility set for a merchant platform credit account'
		),
		(
			'check_merchant_platform_credit_eligible_fee_type',
			'Allows checking whether a merchant platform credit account is eligible for a fee type'
		),
		(
			'delete_merchant_platform_credit_eligible_fee_type',
			'Allows deleting a merchant platform credit fee-type eligibility association'
		),
		(
			'replace_merchant_platform_credit_eligible_fee_type_set',
			'Allows atomically replacing the fee-type eligibility set for a merchant platform credit account'
		),
		-- Merchant Billing Accounts
		(
			'create_merchant_billing_account',
			'Allows creating a merchant billing account'
		),
		(
			'read_merchant_billing_account',
			'Allows reading a merchant billing account'
		),
		(
			'list_merchant_billing_accounts',
			'Allows listing merchant billing accounts by status'
		),
		(
			'suspend_merchant_billing_account',
			'Allows suspending a merchant billing account'
		),
		(
			'reactivate_merchant_billing_account',
			'Allows reactivating a merchant billing account'
		),
		(
			'close_merchant_billing_account',
			'Allows terminally closing a merchant billing account'
		),
		-- Merchant Billable Events
		('read_merchant_billable_event', 'Allows privileged reading of merchant billable-event history'),
		('list_merchant_billable_events', 'Allows privileged listing of merchant billable-event history'),
		-- Merchant Fee Calculations
		('read_merchant_fee_calculation', 'Allows privileged reading of a merchant fee calculation'),
		('list_merchant_fee_calculations', 'Allows privileged listing of merchant fee calculation history'),
		-- Merchant Platform Credit Applications
		(
			'read_merchant_platform_credit_application',
			'Allows privileged reading of a merchant platform credit application'
		),
		(
			'list_merchant_platform_credit_applications',
			'Allows privileged listing of merchant platform credit applications'
		),
		-- Merchant Invoices
		('read_merchant_invoice', 'Allows privileged reading of a merchant invoice'),
		('list_merchant_invoices', 'Allows privileged listing of merchant invoice history'),
		-- Merchant Invoice Items
		('read_merchant_invoice_item', 'Allows privileged reading of a merchant invoice item'),
		('list_merchant_invoice_items', 'Allows privileged listing of merchant invoice item history'),
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
		-- Merchant Future Offerings
		(
			'create_merchant_future_offering',
			'Allows an authorized merchant to create a Future Offering draft'
		),
		(
			'read_merchant_future_offering',
			'Allows an authorized merchant to read its Future Offering'
		),
		(
			'list_merchant_future_offerings',
			'Allows an authorized merchant to list its Future Offerings'
		),
		(
			'update_merchant_future_offering_draft',
			'Allows an authorized merchant to update mutable Future Offering draft facts'
		),
		(
			'discard_merchant_future_offering_draft',
			'Allows an authorized merchant to discard a Future Offering draft'
		),
		('list_engagement_actions', 'Allows an authorized merchant to list active Platform-governed Engagement Actions'),
		-- Platform Settings
		('read_site_maintenance', 'Read Root maintenance workspace'),
        ('manage_site_maintenance', 'Manage announcements and platform availability'),
        ('read_site_identity', 'Read installation identity and terminology'),
		('update_site_identity', 'Manage installation identity and terminology'),
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
		-- Roles and permissions (route and handler names are identical)
		('list_roles', 'Allows listing all roles in the system'),
		('read_role', 'Allows reading one role definition'),
		('create_role', 'Allows creating a role definition'),
		('update_role', 'Allows updating a role definition'),
		('delete_role', 'Allows soft-deleting an unused role definition'),
		('assign_role', 'Allows assigning a non-administrative role to an account'),
		('revoke_role', 'Allows revoking a non-administrative role from an account'),
		('read_user_roles', 'Allows reading the roles held by an account'),
		('list_permissions', 'Allows listing the permission catalog'),
		('check_user_permission', 'Allows checking whether an account holds a permission'),
		('read_role_permissions', 'Allows reading the permissions granted to a role'),
		('update_role_permissions', 'Allows replacing the permissions granted to a non-governance role'),
		-- Administrative governance
		('read_administrators', 'Allows reading administrators, their standing, and governance history'),
		('read_accounts', 'Allows reading the administrative account directory'),
		('appoint_admin', 'Allows appointing an Admin'),
		('appoint_super_admin', 'Allows appointing or promoting a Super Admin'),
		('demote_administrator', 'Allows demoting a Super Admin to Admin'),
		('suspend_administrator', 'Allows suspending an administrator'),
		('restore_administrator', 'Allows restoring a suspended administrator'),
		('revoke_administrator', 'Allows revoking administrative standing'),
		-- Users
		('delete_own_account', 'Allows user to delete own account'),
		('expel_user', 'Allows admin to delete user account')
	ON CONFLICT (name) DO NOTHING;
    `

	insertEntityTypeQuery = `
	INSERT INTO entity_types (name, description) VALUES
        ('site_maintenance', 'Audited maintenance state transitions'),
        ('site_identity', 'Installation identity and terminology aggregate'),
		('action', 'Action entity for audit trail and metadata retrieval'),
		('admin_dashboard', 'Admin dashboard entity'),
		(
			'admin_console',
			'Privileged Admin Console control-plane access surface'
		),
		('audit_log', 'Audit log entity used to track system and user actions'),
		('category', 'Category entity representing groupings for offers and products'),
		('entity_type', 'EntityType metadata object used to describe audit trail loggable system entities'),
		('merchant', 'Merchant entity'),
		('merchant_account', 'Canonical merchant platform-account lifecycle entity'),
		('merchant_program_fee_schedule', 'Merchant program fee schedule effective-dated commercial policy entity'),
		('merchant_future_offering_service_term', 'Future Offering Service Term overall-duration commercial lifecycle entity'),
		(
			'merchant_future_offering_service_period',
			'Future Offering Service Period bounded performance-window history entity'
		),
		(
			'merchant_future_offering_billing_period',
			'Immutable Future Offering Billing Period accounting-window history entity'
		),
		('merchant_platform_credit_account', 'Platform-issued merchant commercial credit account entity'),
		('merchant_platform_credit_eligible_fee_type', 'Merchant platform credit fee-type eligibility association'),
		('merchant_billing_account', 'Merchant billing relationship and canonical billing-currency entity'),
		('merchant_billable_event', 'Canonical source-linked merchant billable-occurrence history'),
		('merchant_fee_calculation', 'Canonical durable merchant fee calculation monetary-result history'),
		('merchant_platform_credit_application', 'Platform-issued merchant commercial credit application entity'),
		('merchant_invoice', 'Canonical durable merchant commercial-obligation and settlement-lifecycle history'),
		('merchant_invoice_item', 'Canonical durable merchant invoice-line history'),
		('merchant_payment_method', 'Merchant billing payment-method reference entity'),
		(
			'merchant_future_offering',
			'Canonical merchant-owned Future Offering aggregate'
		),
		('merchants', 'Tracks merchant-related actions.'),
		('merchant_follow', 'Follow relationship between user and merchant'),
		('roles', 'User role assignments'),
		('administrator', 'Administrative standing of an account (appointment lifecycle)'),
		('root_super_admin', 'The protected Root Super Admin and its succession ledger'),
		('platform_installation', 'Persistent one-time installation state'),
		('permissions', 'Permission catalog and role-permission grants'),
		('system', 'System-wide automation or batch operation'),
		('user_notification', 'User Notification entity'),
		('user_profile', 'User profile entity'),
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
		-- Merchants
		('create_merchant', 'Create a new merchant'),
		('count_merchants', 'Retrieve the total count of merchants'),
		('list_merchants', 'List all merchants'),
		('read_merchant', 'Retrieve merchant by ID'),
		('read_merchant_by_brand', 'Retrieve merchants associated with a given brand ID'),
		('read_merchant_by_offer', 'Retrieve merchant using associated offer ID'),
		('read_merchant_by_name', 'Retrieve merchant by name'),
		('read_merchant_by_website', 'Retrieve merchant using its website URL'),
		('soft_delete_merchant', 'Soft-delete a merchant'),
		('delete_merchant', 'Permanently delete a merchant'),
		('update_merchant', 'Update an existing merchant'),
		-- Merchant Accounts
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
		-- Merchant Future Offering Service Terms
		('propose_merchant_future_offering_service_term', 'Propose a Future Offering Service Term'),
		('read_merchant_future_offering_service_term', 'Read a Future Offering Service Term'),
		('list_merchant_future_offering_service_terms', 'List Future Offering Service Term history'),
		(
			'update_merchant_future_offering_service_term_proposal',
			'Replace the mutable facts of a proposed Future Offering Service Term'
		),
		(
			'establish_merchant_future_offering_service_term',
			'Establish a proposed Future Offering Service Term'
		),
		(
			'replace_merchant_future_offering_service_term',
			'Atomically replace an established Future Offering Service Term'
		),
		-- Merchant Future Offering Service Periods
		(
			'read_merchant_future_offering_service_period',
			'Read a Future Offering Service Period'
		),
		(
			'list_merchant_future_offering_service_periods',
			'List Future Offering Service Period history'
		),
		-- Merchant Future Offering Billing Periods
		(
			'read_merchant_future_offering_billing_period',
			'Read a Future Offering Billing Period'
		),
		(
			'list_merchant_future_offering_billing_periods',
			'List Future Offering Billing Period history'
		),
		-- Merchant Platform Credit Account
		('create_merchant_platform_credit_account', 'Create a platform-issued merchant commercial credit account'),
		('read_merchant_platform_credit_account', 'Read a platform-issued merchant commercial credit account'),
		('list_merchant_platform_credit_accounts', 'List platform-issued merchant commercial credit accounts'),
		('update_merchant_platform_credit_account_descriptive_fields', 'Replace descriptive metadata for a merchant platform credit account'),
		('cancel_merchant_platform_credit_account', 'Cancel an active merchant platform credit account'),
		-- Merchant Platform Credit Eligible Fee Types
		(
			'create_merchant_platform_credit_eligible_fee_type',
			'Create a merchant platform credit fee-type eligibility association'
		),
		(
			'read_merchant_platform_credit_eligible_fee_type',
			'Read a merchant platform credit fee-type eligibility association'
		),
		(
			'list_merchant_platform_credit_eligible_fee_types',
			'List the fee-type eligibility set for a merchant platform credit account'
		),
		(
			'check_merchant_platform_credit_eligible_fee_type',
			'Check whether a merchant platform credit account is eligible for a fee type'
		),
		(
			'delete_merchant_platform_credit_eligible_fee_type',
			'Delete a merchant platform credit fee-type eligibility association'
		),
		(
			'replace_merchant_platform_credit_eligible_fee_type_set',
			'Atomically replace the fee-type eligibility set for a merchant platform credit account'
		),
		-- Merchant Billing Accounts
		('create_merchant_billing_account', 'Create a merchant billing account'),
		('read_merchant_billing_account', 'Read a merchant billing account'),
		('list_merchant_billing_accounts', 'List merchant billing accounts by status'),
		('suspend_merchant_billing_account', 'Suspend a merchant billing account'),
		('reactivate_merchant_billing_account', 'Reactivate a merchant billing account'),
		('close_merchant_billing_account', 'Terminally close a merchant billing account'),
		-- Merchant Billable Events
		('read_merchant_billable_event', 'Read merchant billable-event history'),
		('list_merchant_billable_events', 'List merchant billable-event history'),
		-- Merchant Fee Calculations
		('read_merchant_fee_calculation', 'Read a merchant fee calculation'),
		('list_merchant_fee_calculations', 'List merchant fee calculation history'),
		-- Merchant Platform Credit Applications
		('read_merchant_platform_credit_application', 'Read a merchant platform credit application'),
		('list_merchant_platform_credit_applications', 'List merchant platform credit applications'),
		-- Merchant Invoices
		('read_merchant_invoice', 'Read a merchant invoice'),
		('list_merchant_invoices', 'List merchant invoice history'),
		-- Merchant Invoice Items
		('read_merchant_invoice_item', 'Read a merchant invoice item'),
		('list_merchant_invoice_items', 'List merchant invoice item history'),
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
		-- Merchant Future Offerings
		(
			'create_merchant_future_offering',
			'Create a merchant Future Offering draft'
		),
		(
			'update_merchant_future_offering_draft',
			'Update mutable facts of a merchant Future Offering draft'
		),
		(
			'discard_merchant_future_offering_draft',
			'Discard a merchant Future Offering draft'
		),
		('update_merchant_future_offering_engagement', 'Replace engagement configuration of a merchant Future Offering draft'),
		-- Platform Settings
		('create_platform_setting', 'Create a platform setting'),
		('ensure_platform_setting', 'Ensure a platform setting'),
		('update_site_identity', 'Replace installation identity and terminology'),
		('update_platform_setting_value', 'Update platform setting value and type'),
		('set_platform_setting_active', 'Set platform setting active state'),
		('soft_delete_platform_setting', 'Soft delete a platform setting'),
		('hard_delete_platform_setting', 'Hard delete a platform setting'),
		-- Roles and permissions
		('list_roles', 'List all roles in the system'),
		('read_role', 'Read one role definition'),
		('create_role', 'Create a role definition'),
		('update_role', 'Update a role definition'),
		('delete_role', 'Soft-delete a role definition'),
		('assign_role', 'Assign a role to an account'),
		('revoke_role', 'Revoke a role from an account'),
		('read_user_roles', 'Read the roles held by an account'),
		('list_permissions', 'List the permission catalog'),
		('check_user_permission', 'Check whether an account holds a permission'),
		('read_role_permissions', 'Read the permissions granted to a role'),
		('update_role_permissions', 'Replace the permissions granted to a role'),
		-- Administrative governance
		('site_maintenance_publish', 'Publish maintenance announcement'),
        ('site_maintenance_update', 'Revise maintenance announcement'),
        ('site_maintenance_cancel', 'Cancel maintenance announcement'),
        ('site_maintenance_activate', 'Activate platform maintenance'),
        ('site_maintenance_restore', 'Restore platform operation'),
        ('site_maintenance_engineering_restore', 'Signed Engineering maintenance recovery'),
        ('site_maintenance_read', 'Record refused maintenance workspace access'),
        ('root_continuity', 'Record a Root continuity ceremony'),
		('bootstrap_root_super_admin', 'Establish the initial Root Super Admin'),
		('issue_root_bootstrap_claim', 'Issue or replace the installation email entitlement'),
		('initialize_installation', 'Record completed installation'),
		('recognize_existing_installation', 'Recognize the existing Root without changing authority'),
		('reconcile_administrator_appointment', 'Reconcile a pre-governance administrative role assignment'),
		('appoint_admin', 'Appoint an Admin'),
		('appoint_super_admin', 'Appoint a Super Admin'),
		('promote_to_super_admin', 'Promote an Admin to Super Admin'),
		('demote_super_admin', 'Demote a Super Admin to Admin'),
		('suspend_administrator', 'Suspend an administrator'),
		('restore_administrator', 'Restore a suspended administrator'),
		('revoke_administrator', 'Revoke administrative standing'),
		('list_administrators', 'List administrators'),
		('read_administrator_history', 'Read administrative governance history'),
		('search_accounts', 'Search the administrative account directory'),
		('read_account', 'Read one account in the administrative directory'),
		-- system
		('plan_suggestions_batch', 'Fetch users eligible for suggestion batch'),
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
		-- Auth workflow actions
		('send_activation_link', 'Send an activation link to an eligible user'),
		('activate_user', 'Activate a user account based on a valid activation token'),
		('get_user_activation_status', 'Check user account activation status'),
		('save_user_consent', 'Record user consent to terms and policies'),
		('signup_user', 'Signup a new user account'),
		('login', 'Log in a user and generate authentication tokens'),
		('set_password', 'Establish an initial password for a user account'),
		('refresh_auth_tokens', 'Rotate a refresh token and issue replacement authentication tokens'),
		('logout', 'Log out a user and revoke authentication tokens'),
		('request_password_reset', 'Issue a password reset credential for a user account'),
		('reset_password', 'Reset a user account password using a password reset credential'),
		('change_password', 'Change the authenticated user''s password'),

		-- Users
		('delete_own_account', 'Delete own user account'),
		('expel_user', 'Expel existing user account')
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
	installFutureOfferingCatalogQuery = `
DO $catalog$
DECLARE catalog_department UUID;
BEGIN
 PERFORM pg_advisory_xact_lock(7301195001);
 IF NOT EXISTS (SELECT 1 FROM catalog_installations WHERE installation_key='future-offering-categories-v1') THEN
  -- Same order as catalog administration: department before category.
  LOCK TABLE departments, categories IN SHARE ROW EXCLUSIVE MODE;
  INSERT INTO catalog_installations(installation_key,prior_active_categories)
  SELECT 'future-offering-categories-v1',COALESCE(jsonb_agg(jsonb_build_object('id',id,'name',name,'department_id',department_id)),'[]'::jsonb)
  FROM categories WHERE deleted_at IS NULL;
  UPDATE categories SET deleted_at=clock_timestamp(),updated_at=clock_timestamp() WHERE deleted_at IS NULL;
  UPDATE departments SET deleted_at=clock_timestamp(),updated_at=clock_timestamp() WHERE deleted_at IS NULL;
  INSERT INTO departments(name,slug,sort_order) VALUES('Discovery','future-offering-discovery',10)
  ON CONFLICT(slug) DO UPDATE SET name=EXCLUDED.name,deleted_at=NULL,updated_at=clock_timestamp()
  RETURNING id INTO catalog_department;
  INSERT INTO categories(name,slug,department_id,parent_id,sort_order) VALUES
('AI & Smart Assistants','fo-ai-smart-assistants',catalog_department,NULL,10),
('Gadgets & Connected Devices','fo-gadgets-connected-devices',catalog_department,NULL,20),
('Home & Everyday Living','fo-home-everyday-living',catalog_department,NULL,30),
('Health & Wellness','fo-health-wellness',catalog_department,NULL,40),
('Food & Drink','fo-food-drink',catalog_department,NULL,50),
('Learning & Education','fo-learning-education',catalog_department,NULL,60),
('Work & Business','fo-work-business',catalog_department,NULL,70),
('Travel & Experiences','fo-travel-experiences',catalog_department,NULL,80),
('Entertainment & Creativity','fo-entertainment-creativity',catalog_department,NULL,90),
('Style & Personal Care','fo-style-personal-care',catalog_department,NULL,100),
('Family & Community','fo-family-community',catalog_department,NULL,110),
('Nature & Outdoors','fo-nature-outdoors',catalog_department,NULL,120),
('Energy & Environment','fo-energy-environment',catalog_department,NULL,130),
('Robotics & Automation','fo-robotics-automation',catalog_department,NULL,140),
('Automotive & Mobility','fo-automotive-mobility',catalog_department,NULL,150),
('Aviation, Marine & Drones','fo-aviation-marine-drones',catalog_department,NULL,160),
('Housing & Construction','fo-housing-construction',catalog_department,NULL,170),
('Other & Emerging Ideas','fo-other-emerging-ideas',catalog_department,NULL,180)
  ON CONFLICT(slug) DO UPDATE SET name=EXCLUDED.name,department_id=EXCLUDED.department_id,parent_id=NULL,sort_order=EXCLUDED.sort_order,deleted_at=NULL,updated_at=clock_timestamp();
 END IF;
END;
$catalog$;
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
			'false',
			'boolean',
			'Engineering-controlled administrative setting; Not Allowed for administrative mutation. Does not disable Future Offering functionality.'
		),
		(
			'future_offering_watch_enabled',
			'false',
			'boolean',
			'Engineering-controlled administrative setting; Not Allowed for administrative mutation. Does not disable Watch.'
		),
		(
			'future_offering_creation_enabled',
			'false',
			'boolean',
			'Engineering-controlled administrative setting; Not Allowed for administrative mutation. Does not disable FO creation.'
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
			'false',
			'boolean',
			'Engineering-controlled administrative setting; Not Allowed for administrative mutation. Does not disable FO management.'
		),

		-- User engagement

		(
			'user_trend_engagements_enabled',
			'false',
			'boolean',
			'Engineering-controlled administrative setting; Not Allowed for administrative mutation. Does not disable FO engagement.'
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

	// The five FO administrative controls are Engineering-owned policy,
	// not switches that govern FO business capability. Reconcile only these
	// keys; other platform settings remain administrator-configurable.
	reconcileFOAdministrativeSettingsQuery = `
		UPDATE platform_settings
		SET setting_value = 'false'::jsonb,
		    value_type = 'boolean',
		    description = CASE setting_key
		        WHEN 'future_offering_enabled' THEN 'Engineering-controlled administrative setting; Not Allowed for administrative mutation. Does not disable Future Offering functionality.'
		        WHEN 'future_offering_watch_enabled' THEN 'Engineering-controlled administrative setting; Not Allowed for administrative mutation. Does not disable Watch.'
		        WHEN 'future_offering_creation_enabled' THEN 'Engineering-controlled administrative setting; Not Allowed for administrative mutation. Does not disable FO creation.'
		        WHEN 'merchant_center_future_offerings_enabled' THEN 'Engineering-controlled administrative setting; Not Allowed for administrative mutation. Does not disable FO management.'
		        WHEN 'user_trend_engagements_enabled' THEN 'Engineering-controlled administrative setting; Not Allowed for administrative mutation. Does not disable FO engagement.'
		    END,
		    is_active = TRUE,
		    deleted_at = NULL,
		    updated_at = NOW()
		WHERE setting_key IN (
		    'future_offering_enabled',
		    'future_offering_watch_enabled',
		    'future_offering_creation_enabled',
		    'merchant_center_future_offerings_enabled',
		    'user_trend_engagements_enabled'
		)
		AND (
		    setting_value IS DISTINCT FROM 'false'::jsonb
		    OR value_type IS DISTINCT FROM 'boolean'
		    OR description IS DISTINCT FROM CASE setting_key
		        WHEN 'future_offering_enabled' THEN 'Engineering-controlled administrative setting; Not Allowed for administrative mutation. Does not disable Future Offering functionality.'
		        WHEN 'future_offering_watch_enabled' THEN 'Engineering-controlled administrative setting; Not Allowed for administrative mutation. Does not disable Watch.'
		        WHEN 'future_offering_creation_enabled' THEN 'Engineering-controlled administrative setting; Not Allowed for administrative mutation. Does not disable FO creation.'
		        WHEN 'merchant_center_future_offerings_enabled' THEN 'Engineering-controlled administrative setting; Not Allowed for administrative mutation. Does not disable FO management.'
		        WHEN 'user_trend_engagements_enabled' THEN 'Engineering-controlled administrative setting; Not Allowed for administrative mutation. Does not disable FO engagement.'
		    END
		    OR is_active IS DISTINCT FROM TRUE
		    OR deleted_at IS NOT NULL
		);
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
	mobileClientSecret := strings.TrimSpace(oauthSecrets.MobileClientSecret)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("SeedAllData")
	logger.Info("Beginning database seed operation")

	seeds := []seedSpec{
		{name: "roles", query: insertRolesQuery},
		{name: "permissions", query: insertPermissionsQuery},

		{name: "social_platforms", query: insertSocialPlatformQuery},

		{name: "entity_types", query: insertEntityTypeQuery},
		{name: "actions", query: insertActionQuery},
		{name: "notification_types", query: insertNotificationTypesQuery},
		{name: "notification_channels", query: insertNotificationChannelsQuery},

		// Future Offering engagement and monetization foundations.
		{name: "engagement_actions", query: insertEngagementActionsQuery},
		{name: "merchant_fee_types", query: insertMerchantFeeTypesQuery},
		{name: "merchant_billable_event_fee_types", query: insertMerchantBillableEventFeeTypesQuery},

		// Category hierarchy
		{name: "future_offering_catalog_v1", query: installFutureOfferingCatalogQuery},

		{name: "platform_settings", query: insertPlatformSettingsQuery},
		{name: "fo_administrative_settings_reconciliation", query: reconcileFOAdministrativeSettingsQuery},

		{name: "role_permissions", query: insertRolePermissionsQuery},
		{name: "revoke_admin_governance_permissions", query: revokeAdminGovernancePermissionsQuery},
	}

	if webClientSecret != "" || mobileClientSecret != "" {
		if webClientSecret == "" {
			return fmt.Errorf("seed data: web OAuth client secret is required when OAuth seeding is configured")
		}

		if mobileClientSecret == "" {
			return fmt.Errorf("seed data: mobile OAuth client secret is required when OAuth seeding is configured")
		}

		seeds = append(seeds, seedSpec{
			name:  "oauth_clients",
			query: insertOAuthClientQuery,
			args:  []any{webClientSecret, mobileClientSecret},
		})
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

	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(7301195001)`); err != nil {
		return err
	}

	for _, seed := range seeds {
		if err := m.insertSeedDataTx(ctx, tx, seed); err != nil {
			return err
		}
	}

	if err := recognizeExistingInstallationTx(ctx, tx, m.Logger); err != nil {
		return err
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
