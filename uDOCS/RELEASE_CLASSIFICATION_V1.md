# Release Classification — v1

**Module:** `github.com/PiccoloMondoC/sdworkspace/sdbackend`
**Status:** Living document — update as files are completed or reclassified
**Purpose:** Authoritative record of which backend files are SPINE (required for v1) versus DEFERRED (explicitly out of scope for v1). This is the single source of truth for release scope decisions — do not let SPINE and DEFERRED status live in two places.

---

## Doctrine

Engineering builds capabilities; operations exercises them through configuration rather than code changes.

`admin_console.go` is a first-class SPINE subsystem, not an afterthought. It doesn't necessarily need to be built before the backend APIs, but it is a core deliverable because it owns the administrative decisions that should never be hard-coded:

- Program plans
- Fee schedules
- Setup fee enable/disable
- Subscription pricing
- Founding Merchant waivers
- Platform credits
- Trust reviews
- Merchant suspensions
- Risk flags
- Billing policies

---

## SPINE (v1 build)

### Phase 1 — Admin Control Plane Foundation
- [ ] `admin_console.go` — console boundary, authorization, capability registry, and completed-domain overview

### Phase 2 — Platform Architecture
- [x] `platform_settings.go` — Done
- [x] `platform_setting_history.go` — Done

### Phase 3 — Merchant Foundation
A merchant must exist before anything else.
- [x ] `merchant_accounts.go` — Done

### Phase 4 — Commercial Foundation
Everything needed to let the platform commercially operate, even if all fees are currently waived. Notice that billing ledger comes last — everything else generates financial activity; the ledger records it.
- [x] `merchant_program_plans.go` — Done
- [x] `merchant_program_entitlements.go` — Done
- [ ] `merchant_program_fee_schedules.go`
- [x] `merchant_program_subscriptions.go` — Done
- [ ] `merchant_program_subscription_events.go`
- [ ] `merchant_payment_methods.go`
- [ ] `merchant_platform_credit_accounts.go`
- [ ] `merchant_platform_credit_eligible_fee_types.go`
- [ ] `merchant_platform_credit_applications.go`
- [ ] `merchant_billing_accounts.go`
- [ ] `merchant_billable_events.go`
- [ ] `merchant_fee_calculations.go`
- [ ] `merchant_invoices.go`
- [ ] `merchant_payments.go`
- [ ] `merchant_billing_ledger_entries.go`

### Phase 5 — Future Offering Core
Now the merchant can actually publish.
- [ ] `merchant_future_offerings.go`
- [ ] `merchant_future_offerings_assets.go`
- [ ] `merchant_future_offering_engagement_options.go`
- [ ] `merchant_future_offerings_events.go`

### Phase 6 — Consumer Intelligence
Now consumers can interact.
- [ ] `user_trend_engagements.go`
- [ ] `user_trend_engagement_events.go`

### Phase 7 — Trust & Intelligence
These protect the ecosystem.
- [ ] `future_offering_trust_reviews.go`
- [ ] `future_offering_trust_review_decisions.go`
- [ ] `future_offering_risk_flags.go`
- [ ] `future_offering_watch_density_snapshots.go`

### Phase 8 — Infrastructure
Everything needed for routing and observability.
- [ ] `commerce_routes.go`
- [ ] `commerce_route_events.go`

---

## DEFERRED (explicitly out of scope for v1)

### Enterprise collaboration
- `merchant_account_roles.go`
- `merchant_account_members.go`

### Present commerce
- `merchant_launch_campaigns.go`
- `merchant_launch_campaign_clicks.go`
- `merchant_launch_campaign_attribution_events.go`

### Settlement & reconciliation
- `merchant_settlement_batches.go`
- `merchant_settlement_batch_items.go`
- `merchant_attribution_matches.go`
- `merchant_postback_configs.go`
- `merchant_postback_events.go`
- `merchant_fee_reversals.go`

---

## Change Log

| Date | Change |
|---|---|
| 2026-07-17 | Initial version — SPINE phases 1–8 and DEFERRED groupings established |