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

## Canonical Build Sequence

The numbered phases classify backend capabilities by architectural responsibility. They do not prescribe a strictly numerical implementation order.

The immediate build sequence is:

# Phase 6 — Future Offering Core and Merchant Project

Establish the FO, its configuration, milestones, project-management capabilities, API surface, and Angular merchant experience.

# Phase 8 — Consumer Experience and Participation

Establish discovery, engagement, background Watch, QAE eligibility, aggregate exposure, and the Angular consumer experience required to generate authoritative participation facts.

# Phase 7 — Merchant Anticipation Intelligence

Derive useful merchant intelligence from the FO project context, milestones, and consumer participation facts established by the preceding stages.

Phase 8 precedes Phase 7 in the build sequence because Anticipation Intelligence depends on real consumer-engagement facts. Phase numbering remains unchanged because it expresses architectural classification rather than delivery dependency.

Each capability must be delivered vertically:

# Durable facts → Go services → API → Angular experience → working-site review

Work proceeds through one short Capability Delivery Sheet at a time. Each sheet must state:

What the user must be able to accomplish
Which existing files contribute
What backend work is missing
Which API surface is required
Which Angular artifacts are required
What proves the capability works

The first delivery sheet is:

# M01 — Future Offering Creation

A capability is complete only when its backend and frontend operate together and the resulting experience has been reviewed on the running site.

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
- [x] `merchant_program_entitlements.go`                 — Done
- [x] `merchant_program_fee_schedules.go`                — Done
- [x] `merchant_future_offering_service_terms.go`        — Done
- [x] `merchant_future_offering_service_periods.go`      — Done
- [x] `merchant_future_offering_billing_periods.go`      — Done
- [x] `merchant_platform_credit_accounts.go`             — Done
- [x] `merchant_platform_credit_eligible_fee_types.go`   — Done
>       Anticipation Intelligence Activation Fee (one-time)
        Charged when Anticipation Intelligence services are activated for a Future Offering.
>       Anticipation Intelligence Fee (recurring/usage-based)
        Charged while Anticipation Intelligence services continue operating for that Future Offering.
>       Asset Hosting Overage Fee
        Applies only when Please complete definition
>       Platform Service Fee 
        Please define
- [x] `merchant_billing_accounts.go`                     — Done
- [x] `merchant_billable_events.go`                      — Done
- [x] `merchant_fee_calculations.go`                     — Done
- [x] `merchant_platform_credit_applications.go`         — Done
- [x] `merchant_invoices.go`                             — Done
>- [ ] `merchant_invoice_items.go`                      Suspended
- [ ] `merchant_billing_ledger_entries.go`


### Phase 5 — Merchant Payments Architecture
#### Direct Payment Mode — SPINE — v1
- [x] `merchant_payment_methods.go`                      — Done
- [ ] `merchant_payment_method_provider_links.go`
- [ ] `merchant_payments.go`
- [ ] `platform_commercial_promotions.go`
- [ ] `merchant_commercial_adjustments.go`

>ORGANIZED IN BUILD ORDER

### Phase 6 — Future Offering Core
Now the merchant can actually publish.
- [x] `merchant_future_offerings.go`                     — Done
        Create FO Wizard(s); engagement options, goals, billing terms, assets
        Primarily frontend/orchestration; probably no new durable domain
- [ ] `merchant_future_offerings_assets.go`
- [ ] `merchant_future_offering_engagement_options.go`
- [ ] `merchant_future_offering_goals.go`               Derived capability
- [ ] `merchant_future_offering_billing_terms.go`       Derived capability
- [ ] `merchant_future_offerings_events.go`

- [ ] `merchant_future_offering_milestones.go`          Derived capability
        Merchant Milestones
        FO Project Management; Derived Go capability, not new table


### Phase 8 — Consumer Intelligence
Now consumers can interact.
- [ ] `user_trend_engagements.go`
        Consumer Engagement; Already substantially represented; aggregate/exposure capability needs explicit service treatment
- [ ] `user_trend_engagement_events.go`
        Consumer Engagement, FO engagement options; 
        Already substantially represented; aggregate/exposure capability needs explicit service treatment
- [ ] `merchant_future_offering_consumer_tokens.go`
        Consumer Tokens
- [ ] `future_offering_reviews.go`
        Reviews / Reputation; merchant reputation derived
- [ ] `consumer_merchant_follows.go`
        Sagrenti User Drive; if Follow enters v1; sharing/recommendations mostly derived/application capabilities


### Phase 7 — Merchant Intelligence
- [ ] `merchant_intelligence_reports.go`
        Anticipation Intelligence; Already represented, although present names may need reconsideration
- [ ] `merchant_intelligence_report_metrics.go`
        Anticipation Intelligence; watch-density snapshots

### Phase 9 — Trust & Intelligence
These protect the ecosystem.
- [ ] `future_offering_trust_reviews.go`
- [ ] `future_offering_trust_review_decisions.go`
- [ ] `future_offering_risk_flags.go`
- [ ] `future_offering_watch_density_snapshots.go`

### Phase 10 — Infrastructure
Everything needed for routing and observability.
- [ ] `commerce_routes.go`
        Consumer Discovery; route events; FO core
        Existing pieces are insufficiently explicit; discovery service capability should be identified
- [ ] `commerce_route_events.go`

- `merchant_fee_reversals.go`

## Change Log

| Date | Change |
|---|---|
| 2026-07-17 | Initial version — SPINE phases 1–8 and DEFERRED groupings established |