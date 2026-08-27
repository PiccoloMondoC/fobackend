# Release Classification — v1

**Module:** `github.com/PiccoloMondoC/sdworkspace/sdbackend`
**Status:** Living document — update as files are completed or reclassified
**Purpose:** Authoritative record of which backend files are SPINE (required for v1) versus DEFERRED (explicitly out of scope for v1). This is the single source of truth for release scope decisions — do not let SPINE and DEFERRED status live in two places.

## Doctrine

Engineering builds capabilities; operations exercises them through configuration rather than code changes.

`admin_console.go` is a first-class SPINE subsystem, not an afterthought. It doesn't necessarily need to be built before the backend APIs, but it is a core deliverable because it owns the administrative decisions that should never be hard-coded: 
>                                                        — Done

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

## SPINE (v1 build)

### Phase 1 — Admin Control Plane Foundation
- [x ] `admin_console.go` — console boundary, authorization, capability registry, and completed-domain overview
>                                                        — Done

### Phase 2 — Platform Architecture
- [x] `platform_settings.go`                             — Done
- [x] `platform_setting_history.go`                      — Done

### Phase 3 — Merchant Foundation
A merchant must exist before anything else.
- [x ] `merchant_accounts.go`                            — Done

### Phase 4 — Commerce Architecture
Everything needed to let the platform commercially operate, even if all fees are currently waived. Notice that billing ledger comes last — everything else generates financial activity; the ledger records it.
- [x] `merchant_program_plans.go`                        — Done
- [x] `merchant_program_entitlements.go`                 — Done
- [x] `merchant_program_fee_schedules.go`                — Done
>- [x] `merchant_program_subscriptions.go`          — Marked for deletion?
- [x] `merchant_future_offering_service_terms.go`        — Done
>- [x] `merchant_program_subscription_periods.go`   — Marked for deletion?
- [x] `merchant_future_offering_service_periods.go`      — Done
- [x] `merchant_future_offering_billing_periods.go`      — Done
>- [x] `merchant_program_subscription_events.go`    — Marked for deletion?
- [x] `merchant_platform_credit_accounts.go`             — Done
- [x] `merchant_platform_credit_eligible_fee_types.go`   — Done
>       Anticipation Intelligence Activation Fee (one-time)
        Charged when Anticipation Intelligence services are activated for a Future Offering.
>       Anticipation Intelligence Fee (recurring/usage-based)
        Charged while Anticipation Intelligence services continue operating for that Future Offering.
>       Campaign Performance Fee (Launch Campaign only)
        Applies only to present-commerce Launch Campaign services where that commercial model is enabled.
>       Subscription Fee (optional)
        Applies only when Plans and Subscriptions are enabled by Admin.
- [x] `merchant_billing_accounts.go`                     — Done
- [x] `merchant_billable_events.go`                      — Done
- [x] `merchant_fee_calculations.go`                     — Done
- [x] `merchant_platform_credit_applications.go`         — Done
- [x] `merchant_invoices.go`                             — Done
>- [ ] `merchant_invoice_items.go`                      Suspended
- [ ] `merchant_billing_ledger_entries.go`


- [ ] `merchant_future_offering_payment_periods`         — 

### Phase 5 — Merchant Payments Architecture
#### Direct Payment Mode — SPINE — v1
- [x] `merchant_payment_methods.go`                      — Done
- [ ] `merchant_payment_method_provider_links.go`
- [ ] `merchant_payments.go`
- [ ] `platform_commercial_promotions.go`
- [ ] `merchant_commercial_adjustments.go`

### Phase 6 — Future Offering Core
Now the merchant can actually publish.
- [ ] `merchant_future_offerings.go`
- [ ] `merchant_future_offerings_assets.go`
- [ ] `merchant_future_offering_engagement_options.go`
- [ ] `merchant_future_offering_goals.go`
- [ ] `merchant_future_offering_billing_terms.go`
- [ ] `merchant_future_offerings_events.go`

### Phase 7 — Merchant Intelligence
- [ ] `merchant_intelligence_reports.go`
- [ ] `merchant_intelligence_report_metrics.go`

### Phase 8 — Consumer Intelligence
Now consumers can interact.
- [ ] `user_trend_engagements.go`
- [ ] `user_trend_engagement_events.go`

### Phase 9 — Trust & Intelligence
These protect the ecosystem.
- [ ] `future_offering_trust_reviews.go`
- [ ] `future_offering_trust_review_decisions.go`
- [ ] `future_offering_risk_flags.go`
- [ ] `future_offering_watch_density_snapshots.go`

### Phase 10 — Infrastructure
Everything needed for routing and observability.
- [ ] `commerce_routes.go`
- [ ] `commerce_route_events.go`


## DEFERRED (explicitly out of scope for v1)

## Treasury Balance Mode — DEFERRED — post-v1
- Marked for early post-v1 implementation.
- `merchant_balances.go`
- `merchant_ledger_entries.go`
- `merchant_funding_transactions.go`
- `merchant_funding_sources.go`
- `merchant_funding_source_provider_links.go`
- `merchant_withdrawals.go`

## Commerce Architecture — DEFERRED — post-v1

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

## Change Log

| Date | Change |
|---|---|
| 2026-07-17 | Initial version — SPINE phases 1–8 and DEFERRED groupings established |