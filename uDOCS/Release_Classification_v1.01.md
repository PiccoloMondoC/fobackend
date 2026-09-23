# Release Classification — v1.01

**Module:** `github.com/PiccoloMondoC/sdworkspace/sdbackend`  
**Status:** Living document — update as files are completed, added, removed, or reclassified  
**Purpose:** Authoritative record of which backend files and derived backend capabilities are SPINE (required for v1) versus DEFERRED (explicitly out of scope for v1). This is the single source of truth for release scope decisions; SPINE and DEFERRED status must not be maintained elsewhere.

## Classification Doctrine

Engineering builds complete production capabilities and defines their safe operating boundaries. Administration governs their commercial and operational use through configuration within those boundaries.

Release scope must recognise two forms of backend work:

- **Durable domain:** authoritative facts that require persistence and ordinarily produce database-backed data-layer files.
- **Derived capability:** production behaviour computed or orchestrated from authoritative facts and ordinarily implemented through service or application files without another table.

A capability does not require a table merely because it is important. Conversely, a release phase is not complete merely because its tables, handlers, and billable events exist.

Each service area must satisfy both completion tests:

1. Can the merchant complete the activity for which Sagrenti provides and may charge for service, and understand what is being received?
2. Can the consumer receive and use the experience whose participation makes that merchant service valuable?

`admin_console.go` is a first-class SPINE subsystem. It need not precede every backend API, but it must govern administrative decisions that must not be hard-coded, including:

- Program plans and entitlements
- Fee schedules, fee enablement, and waivers
- Platform credits
- Service operating limits
- Trust reviews
- Merchant suspensions
- Risk flags
- Billing policies
- Other completed-domain configuration exposed through the capability registry

## FOPSC Capability-to-Release Alignment

This mapping connects the services established in FOPSC v1.01 to the existing release foundation and the remaining architectural treatment.

| FOPSC capability | Existing release foundation | Required architectural treatment |
|---|---|---|
| **FO creation** | `merchant_future_offerings.go`, assets, engagement options, goals, and billing terms | Guided frontend and orchestration capability; no additional durable domain presently established |
| **Merchant milestones** | No explicit domain | Add `merchant_future_offering_milestones.go` as a durable domain |
| **FO project management** | FO and milestone facts | Derived service capability; no separate project-management table |
| **Consumer discovery** | FO core, `commerce_routes.go`, and route events | Add an explicit discovery service capability; supporting routes are not themselves discovery |
| **Consumer engagement** | `user_trend_engagements.go`, `user_trend_engagement_events.go`, and FO engagement options | Existing durable foundation; add explicit participation, background-Watch, QAE, and aggregate-exposure service behaviour |
| **Anticipation Intelligence** | `merchant_intelligence_reports.go`, `merchant_intelligence_report_metrics.go`, and watch-density snapshots | Existing foundation; review whether inherited report-oriented names adequately represent the service |
| **Merchant actions and decisions** | FO, milestones, and Anticipation Intelligence | Derived service capability |
| **Milestone reporting** | Milestones and Merchant Intelligence | Derived service capability; no additional durable domain presently established |
| **Release / Availability** | Milestones and consumer-token transition | No separate capability or domain |
| **Consumer tokens** | No explicit domain | Add `merchant_future_offering_consumer_tokens.go` as a durable domain |
| **Consumer contact** | Consumer-token lifecycle | Token-enabled transition; no separate `consumer_contact` table |
| **Reviews and reputation** | No explicit domain | Add `future_offering_reviews.go`; derive merchant reputation from verified reviews |
| **Outcome Intelligence** | No present foundation | Defer to v1.1 with the Merchant/Retailer Integration API |
| **Consumer quality of service and UX** | Cross-cutting | Apply as a quality requirement across the consumer experience; no isolated table |
| **Sagrenti User Drive** | Discovery and routing partially support it | Add `consumer_merchant_follows.go` if Follow enters v1; add a durable merchant-announcement domain if follower communications enter v1; derive audience resolution and delivery behaviour |

## SPINE — v1 Build

### Phase 1 — Admin Control Plane Foundation

- [x] `admin_console.go` — console boundary, authorisation, capability registry, and completed-domain overview

### Phase 2 — Platform Architecture

- [x] `platform_settings.go`
- [x] `platform_setting_history.go`

### Phase 3 — Merchant Foundation

A merchant must exist before merchant-owned commercial or Future Offering facts can exist.

- [x] `merchant_accounts.go`

### Phase 4 — Commerce Architecture

This phase provides the commercial machinery required to operate the Platform, including when configured fees are waived. The billing ledger records financial activity generated by the preceding commercial domains.

#### Program and fee configuration

- [x] `merchant_program_fee_schedules.go`

#### FO service and billing periods

- [x] `merchant_future_offering_service_terms.go`
- [x] `merchant_future_offering_service_periods.go`
- [x] `merchant_future_offering_billing_periods.go`

#### Platform credits and fee eligibility

- [x] `merchant_platform_credit_accounts.go`
- [x] `merchant_platform_credit_eligible_fee_types.go`

The established fee families are:

- **Anticipation Intelligence Activation Fee** — charged when Anticipation Intelligence service is activated for an FO.
- **Platform Service Fee** — charged for the continuing Platform service that enables the merchant to operate and manage an FO.
- **Anticipation Intelligence Fee** — charged for the continuing or usage-based Anticipation Intelligence service provided for an FO.

Asset Hosting is a Platform capability, not an established fee family. Asset Hosting Overage Fee is withdrawn from the current fee model and must be removed from affected schema and code.

#### Billing, calculation, invoicing, and ledger

- [x] `merchant_billing_accounts.go`
- [x] `merchant_billable_events.go`
- [x] `merchant_fee_calculations.go`
- [x] `merchant_platform_credit_applications.go`
- [x] `merchant_invoices.go`
- [ ] `merchant_invoice_items.go` — suspended pending completion of the current product-capability and commercial-spine reconciliation
- [ ] `merchant_billing_ledger_entries.go`

### Phase 5 — Merchant Payments Architecture

#### Direct Payment Mode — SPINE

- [x] `merchant_payment_methods.go`
- [ ] `merchant_payment_method_provider_links.go`
- [ ] `merchant_payments.go`
- [ ] `platform_commercial_promotions.go`
- [ ] `merchant_commercial_adjustments.go`

### Phase 6 — Future Offering Core and Merchant Project

This phase must enable a merchant to create, configure, submit, and manage the project for which Sagrenti provides service.

#### Durable domains

- [.] `merchant_future_offerings.go`
- [ ] `merchant_future_offerings_assets.go`
- [ ] `merchant_future_offering_engagement_options.go`
- [ ] `merchant_future_offering_goals.go`
- [ ] `merchant_future_offering_billing_terms.go`
- [ ] `merchant_future_offerings_events.go`
- [.] `merchant_future_offering_milestones.go`



- [.] `.go`
- [.] `.go`
- [.] `.go`
- [.] `user_future_offering_engagements.go`
- [.] `merchant_future_offering_engagement_action_groups.go`
- [.] `merchant_future_offering_engagement_options.go`
- [.] `user_future_offering_engagement_action_selections.go`
- [.] `user_future_offering_engagement_events.go`
- [.] `user_future_offering_engagement_submissions.go`
- [.] `user_future_offering_engagement_submission_items.go`


#### Derived capabilities

- [.] `future_offering_project_management.go` — milestone plan, project status, upcoming actions, and due or overdue matters
- [ ] `future_offering_project_actions.go` — milestone-aware merchant actions, reminders, and decision support

FO creation wizard behaviour is primarily frontend and orchestration work over the authoritative FO domains. Activation is the submission action at the end of the appropriate workflow, not a separate wizard.

### Phase 7 — Merchant Anticipation Intelligence

This phase must turn consumer engagement facts into decision-useful intelligence rather than merely preserve or report counts.

#### Durable domains

- [ ] `merchant_intelligence_reports.go` — existing name subject to review against the Anticipation Intelligence service boundary
- [ ] `merchant_intelligence_report_metrics.go` — existing name subject to the same review
- [ ] `future_offering_watch_density_snapshots.go`

#### Derived capabilities

- [ ] Milestone-aware Anticipation Intelligence service capability — trends, velocity, engagement mix, dimensional analysis, and milestone-to-milestone change; final file boundary and name to be settled during design
- [ ] Milestone reporting service capability — derived from milestones and Anticipation Intelligence; final file boundary and name to be settled during design

### Phase 8 — Consumer Experience and Participation

Consumers must be able to discover worthwhile FOs, participate in anticipation, understand the experience, and receive useful value in return.

#### Durable domains

- [ ] `user_trend_engagements.go`
- [ ] `user_trend_engagement_events.go`
- [ ] `consumer_merchant_follows.go` — consumer-to-merchant Follow relationship

#### Derived capabilities

- [ ] `future_offering_discovery.go` — discovery across FOs, categories, relevant locations, merchants, trends, and follows
- [ ] `future_offering_engagement_aggregates.go` — privacy-safe aggregate calculation and consumer exposure
- [ ] Participation/QAE service capability — background Watch establishment, canonical QAE eligibility, and single-QAE enforcement; final file boundary and name to be settled during design

Watch is the canonical entry point for QAE eligibility and counting. When a consumer selects another participation action without first selecting Watch, the Platform automatically enables Watch in the background. Background activation does not constitute explicit Watch selection or broaden aggregate exposure.

### Phase 9 — Consumer Opportunity, Communication, and Reputation

This phase governs the transition from anticipation to a merchant-issued opportunity while preserving consumer control and providing the foundation for verified reputation.

#### Durable domains

- [ ] `merchant_future_offering_consumer_tokens.go`
- [ ] `future_offering_reviews.go` — verified FO reviews; merchant reputation is derived
- [ ] `merchant_follower_announcements.go` — merchant-authored follower communications, if follower communications are confirmed for v1

#### Derived capabilities

- [ ] Consumer-token issuance and delivery capability — final file boundary and name to be settled during design
- [ ] Merchant reputation capability — derived from verified FO reviews
- [ ] Follower audience resolution and delivery capability — derived from consumer consent, follows, announcement state, and delivery eligibility

Consumer Contact is a token-enabled transition controlled by the consumer. It does not require a separate `consumer_contact` table merely because the concept exists.

### Phase 10 — Trust and Safety

These capabilities protect the Platform ecosystem. Platform trust review is distinct from a consumer’s verified review of an FO.

- [ ] `future_offering_trust_reviews.go`
- [ ] `future_offering_trust_review_decisions.go`
- [ ] `future_offering_risk_flags.go`

### Phase 11 — Routing and Observability Infrastructure

Routing supports discovery, sharing, attribution, and navigation but is not itself the Consumer Discovery capability.

- [ ] `commerce_routes.go`
- [ ] `commerce_route_events.go`

## DEFERRED — Explicitly Out of Scope for v1

### Merchant/Retailer Integration and Outcome Intelligence — v1.1

External integration should use a stable Sagrenti contract rather than retailer-specific domain semantics.

- Merchant/Retailer Integration API
- Verified external token-action and outcome ingestion
- Outcome Intelligence derived from trustworthy external outcome facts

The corresponding durable domains and Go file boundaries will be designed when this capability is undertaken.

### Treasury Balance Mode — Early post-v1

- `merchant_balances.go`
- `merchant_ledger_entries.go`
- `merchant_funding_transactions.go`
- `merchant_funding_sources.go`
- `merchant_funding_source_provider_links.go`
- `merchant_withdrawals.go`

## Maintenance Rule

Update a checklist item when its release status changes or its full production slice is completed under the project’s vertical-build standard. A derived capability without a database table remains independently trackable. When a provisional capability boundary is converted into one or more settled Go files, replace the capability line with those filenames rather than maintaining both.

All additions, removals, phase moves, SPINE/DEFERRED reclassifications, and material naming changes must be recorded in the Change Log.

## Change Log

| Date | Version | Change |
|---|---|---|
| 2026-07-17 | v1.0 | Initial SPINE phases and DEFERRED groupings established. |
| 2026-09-02 | v1.01 | Reconciled release scope with FOPSC; distinguished durable domains from derived capabilities; added milestones, discovery, tokens, reviews, Follow, follower communications, and product-experience completion rules; moved watch-density into Anticipation Intelligence; separated consumer reviews from Trust and Safety; deferred external integration and Outcome Intelligence to v1.1; removed Asset Hosting Overage Fee from the established fee model. |
