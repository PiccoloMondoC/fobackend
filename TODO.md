#
# TODO.md — Deployment-Sensitive Cleanup

Use this checklist to track development conveniences and cleanup steps required before production deployment.

---

## Guest User Implementation in Handlers
- [✅ Check offer-price-history.go `GetLatestOfferPriceHandler` for example of how to use Guest User in handlers. We will need that in a few handlers to make content accessible to unauthenticated users. The incentive to authenticate lies in several other handlers we incluse to make certain features accessible only to authenticated users.



## Middleware

- [✅ Critical] Remove `DevFallbackContextMiddleware`
- [✅ Critical] Ensure `InjectApplicationContextMiddleware` is covered by secure upstream injection
- [✅ Critical] Strip sensitive headers from public requests

---

## Routes / Debug Endpoints

- [✅ Critical] Remove or restrict access to `/debug/context`
- [✅ Critical] Validate all temporary/test routes are cleaned up or protected

---

## Hardcoded Dev Values

- [✅ Critical] Remove fallback UUIDs
- [✅ Critical] Replace with runtime-injected values from headers or token claims

---

## Logging

- [✅ Critical] Lower dev-only logs (fallbacks, warnings) to appropriate levels in production
- [ ] Switch log mode to `json` if needed by GCP logging

---

## Config

- [✅ Critical] `app.Config.Environment` is properly set to `"production"` via env
- [✅ Critical] Rate limit and timeout configurations are reviewed

---

## Background Tasks
- [✅ Critical] Migrate internal background triggers (e.g., AutoValidateOfferPrice) to Cloud Scheduler or Cloud Tasks.
    - Internal background goroutines work in early Cloud Run deployments.
    - Cloud Run may shut down idle instances, so for production reliability, triggers must be externalized.


## Created User Wallets

To trigger wallet balance updates via automation, you need upstream sources (e.g., offer rewards, refunds, affiliate payouts).

### Affiliate Payouts (core use case)

- When a user earns a commission for a offer or referral

- Automatically fires a BalanceAdjustmentEvent

### Offer Rewards / Cashback

- After a offer is confirmed and marked as redeemed

- Fire a balance update to credit the reward

### Refunds / Disputes

- Deduct balance for chargebacks or disputes

- Could trigger negative adjustment

### Promotional Bonuses

- Signup rewards, seasonal bonuses, etc.

### Manual Admin Adjustments (via secure tool)

- Edge case: internal tools, no public access


## ✅ Minimal Requirements to Start Affiliate Payout Automation:

- affiliate_earnings table (per offer or aggregate)

- Background job to:

  - Check for cleared earnings (e.g. after return window)

  - Compute payout amount per user

  - Fire BalanceAdjustmentEvent{UserID, Amount}

- Optional: Flag earnings as paid (to avoid double payout)

Next step: Draft a schema and an event driven background goroutine for the affiliate payout loop:

- A clean affiliate_earnings schema

- Indexing for efficient aggregation

- A background payout processor

- Safeguards against double payout

- Audit trail per earnings transfer


## ✅ Needed: Dashboard
Yes, because users need:

Saved favorites

Notifications

Settings (email, phone, preferred channel)

Offer alerts / subscriptions

Wallet balance / rewards tracking


## ❌ Not Needed: Public Profile
Unless you plan to:

Let users post reviews

Have user-to-user interactions

Show usernames publicly

There’s no reason to have a profile page or editable bio/picture.

## 📌 Recommendation:
Just treat user_settings + user_wallets + user_notifications + user_favorites as the functional components of a dashboard, not a profile. You can drop any UI/DB effort toward user bios, usernames, avatars, or public visibility.

Let me know if you'd like a minimalist GET /me/dashboard endpoint bundling all these.



 ## Phase 2 Features:
✅ User reviews (yes, worth doing)

✅ Rewards / affiliate sharing

✅ Offer sharing by users

❌ Public usernames or social graph (not planned)

❌ User-to-user messaging (not needed)


## 🎯 Key Design Principle:
Treat users as curators, not influencers.


## 🧱 Suggested Model:
Reviews: Let users post reviews with optional display names like “Verified Buyer” or “Offer Hunter.” No need to tie to public profiles.

Shared Offers: Generate clean shareable links with referral tracking (/offer/:id?ref=abc123). Users get credit via wallet, not visibility.

Referral Leaderboards (Optional): Can gamify it privately, showing their rank only to them.

User Dashboard: Central hub for wallet, referrals, favorites, and subscriptions. No public-facing profile.


## 🔐 Clean Implementation Without Social Media Bloat:
No follower/following

No comment threads

No public feeds

No display names required

All user interactions are platform-mediated, not peer-to-peer


## Here’s a focused Phase 2 Architecture Checklist for supporting user reviews and affiliate offer sharing without turning your platform into a social network:

✅ 1. User Reviews (on Offers)
🗃️ Database
offer_reviews table:

id UUID PK

user_id UUID

offer_id UUID

rating INTEGER (1–5)

review TEXT

is_verified_purchase BOOLEAN

created_at, updated_at, deleted_at

🔐 Privacy
Do not show usernames

Use generic labels: “Verified Buyer”, “Early Reviewer”

🚦 Moderation
Add is_flagged, flagged_reason, review_status (pending/approved/rejected)

Build internal tooling to moderate reviews before they appear publicly

🧩 API
POST /offer/:id/review

GET /offer/:id/reviews (public, paginated)

PATCH /offer/:id/review/flag

✅ 2. Affiliate Offer Sharing
🗃️ Database
user_affiliate_links table:

id UUID PK

user_id UUID

offer_id UUID

referral_code TEXT (e.g., abc123)

clicks_count INT

conversions_count INT

created_at, last_clicked_at

🧠 Logic
On /offer/:id, generate: sagrenti.com/offer/:id?ref=abc123

When link is clicked:

Log click in offer_clicks

Track if the offer results in an action (e.g., conversion, purchase)

Schedule rewards payout to wallet after return window

🧩 API
GET /me/affiliate-links

POST /offer/:id/generate-link (returns user’s link)

GET /referral/:code/track (backend-only redirect)

✅ 3. User Wallet Integration
Reuse existing user_wallets table

Add logic to reward:

Per conversion (fixed or percentage)

Per first-time referral

Schedule payouts with background job after hold period (e.g., 30 days)

✅ 4. User Dashboard Updates
Add:

My Reviews

Shared Offers + Performance

Wallet balance + Reward history

✅ 5. Anti-Abuse & Security
Rate-limit review posting

Prevent self-referrals via cookies/IP/device fingerprinting

Require email verification for affiliate sharing

Admin moderation queue for flagged reviews or suspicious activity


Potential Errors

1. Should SendActivationLinkHandler be a manual handler? I doubt it.
2. Should CreateUserDashboardHandler be manual? A handler? I doubt it.









## POST DEPLOYMENT ##

# Task: Implement audit failure tracking across handlers

# Context:
Currently, handlers tolerate audit logging failures using continue, allowing the main business logic to proceed. This is acceptable for launch, but we need reliable tracking of all failed audit logging attempts post-deployment.

# Objectives:

Detect all audit log failures during handler execution

Persist failed attempts for later inspection, alerting, or replay

Ensure tracking does not interfere with handler flow

# Action Plan:

1. Create a new database table: audit_log_failures
Fields: id, action, entity_id, entity_type, user_id, error_msg, occurred_at

2. Add InsertAuditLogFailure(ctx, failure *AuditLogFailure) to the model layer

3. In every handler that writes to AuditLog, update the failure branch to:

4. Log the failure as a structured warning

5. Insert a record into audit_log_failures (non-blocking)

(Optional) Emit a pub/sub event like audit_log_failed for async alerting or recovery

Build an internal view or tool for admins to review and replay failed audit entries

# Priority: High (Immediately after deployment)
# Owner: [Assign name]
