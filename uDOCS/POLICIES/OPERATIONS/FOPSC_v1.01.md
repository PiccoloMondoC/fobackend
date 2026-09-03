# Future Offering Platform Service Capabilities (FOPSC)

## v1.01 — Consolidated Doctrine

**Status:** Authoritative service-capability summary for the Future Offering Platform.

**Purpose:** Define the merchant and consumer services the Platform must provide, the durable facts required to support them, and the derived capabilities that turn those facts into a coherent product experience.

# 1. Governing Doctrine

The Future Offering Platform exists to provide valuable services to merchants and consumers. The Monetization Layer supports those services; the Platform does not exist merely to generate billable events.

Capability must therefore be designed in this order:

Capability → Quality of Service → User Experience → Economics, if warranted.

A commercially complete backend is not sufficient if a merchant cannot complete the activity for which the merchant is billed, cannot understand the service received, or if consumers cannot receive and use the experience that makes the merchant service valuable.

This doctrine changes the development emphasis without invalidating the Monetization work already completed. The remaining product capabilities must now be defined and built before monetization is treated as the centre of the Platform.

# 2. Established Commercial Boundary

Asset Hosting is a Platform capability. It is not presently an established fee family. Asset Hosting Overage Fee (AHO) is withdrawn from the current fee model and must not remain in the schema or codebase as though its commercial legitimacy were settled.

The established fee families are:

- Anticipation Intelligence Activation Fee — charged when Anticipation Intelligence service is activated for an FO.

- Platform Service Fee — charged for the continuing Platform service that enables the merchant to operate and manage an FO.

- Anticipation Intelligence Fee — charged for the continuing or usage-based Anticipation Intelligence service provided for an FO.

Any future fee must follow a defined, production-quality service capability. The existence of a measurable event does not itself justify a charge.

# 3. Architectural Classification

## 3.1 Durable domains

A durable domain preserves an authoritative fact that must survive process execution. It ordinarily has database persistence and a corresponding data-layer Go file. Milestones, issued tokens, reviews, follows, and authored announcements are examples because their identity, history, state, or provenance must be retained.

## 3.2 Derived service capabilities

A derived service capability computes or orchestrates useful behaviour from authoritative facts. It may have Go service or application files without a backing database table. Project status, next actions, aggregate exposure, intelligence, reputation, discovery results, and audience resolution are examples.

## 3.3 Persistence rule

A capability does not earn a table merely because it is important. New schema is warranted only for a genuinely new durable fact. Derived outputs must not become redundant sources of truth.

Release Classification must therefore recognise both persistence-backed domain files and service/application capability files. A table-derived file inventory alone understates the Platform work required for a production-quality product.

# 4. Merchant Service Capabilities

## 4.1 FO creation

Merchants require one or more guided workflows to create, configure, prepare, and submit an FO. Activation is not itself a wizard; Activate may be the submission action at the conclusion of the appropriate creation workflow. The workflow must establish the authoritative facts needed for the services that follow.

## 4.2 Merchant-defined milestones

Every FO is a merchant project. The merchant defines the milestones and dates relevant to that project. Sagrenti preserves those facts and uses them to support project progress, follow-up actions, reminders, notifications, and milestone-aware intelligence.

Likely durable domain: `merchant_future_offering_milestones`.

## 4.3 FO project management

Sagrenti must help the merchant keep the FO moving. The merchant experience must make the milestone plan, current project status, upcoming actions, due and overdue matters, and other attention requirements intelligible and actionable. These outputs should normally be derived from the FO, its milestones, and related authoritative facts.

## 4.4 Merchant actions and decisions

At activation, the merchant should understand the actions Sagrenti will expect as the FO progresses. As milestones approach, the Platform must support timely action with relevant reminders, notifications, project context, and Anticipation Intelligence.

## 4.5 Milestone reporting

At meaningful milestones, the merchant should receive concrete information about how anticipation is developing and how the FO performed during that stage. Milestone reporting is part of Anticipation Intelligence, not merely administrative reporting.

# 5. Consumer Service Capabilities

## 5.1 Discovery

Sagrenti must help consumers discover FOs they did not already know about. Discovery may include What’s Next feeds, search, categories, relevant-location exploration, merchant pages, trending or popular FOs, sharing, and eventually personalised recommendations.

What’s Next remains the internal working decoy for the public-facing expression Coming Up.

## 5.2 Engagement

Consumers participate through established actions such as Watch, Waitlist, Early Access Request, Beta, Reservation Interest, and Preorder Intent. The Platform must provide a clear and useful experience around these actions rather than merely record engagement events.

Watch is the canonical entry point for QAE eligibility and counting. When a consumer begins participation by selecting another engagement action without first selecting Watch, the Platform automatically enables Watch in the background. This preserves a single, consistent basis for QAE counting regardless of which engagement action begins the consumer’s participation.

Background activation exists solely to establish the canonical QAE basis. It does not constitute the consumer’s explicit selection of Watch, remove Watch as an available consumer choice, or determine which engagement aggregates the consumer may see. Aggregate exposure is governed separately under §5.3.

## 5.3 Aggregate exposure

Engagement aggregates are privacy-safe aggregate anticipation information. Their consumer exposure is governed as follows:

- No interaction with an FO: expose no engagement aggregates for that FO.

- Watching an FO: expose aggregates for all engagement actions enabled on that FO.

- Participating without Watching: expose only the aggregates corresponding to the consumer’s selected engagement actions, even though Watch is automatically activated in the background, leaving it for the user to still be able to select.

Where permitted, expressions such as “1,247 watching · 183 waitlisted” are legitimate anticipation information.

Watch is turned on automatically in the background, so the user can still select it, if a user chooses any other participation action without first chooing Watch.

## 5.4 Consumer quality of service

Consumer quality is a cross-cutting Platform responsibility. Discovery, engagement, updates, aggregate statistics, privacy, tokens, contact transitions, accessibility, responsiveness, and clarity must collectively make anticipation useful and enjoyable. Consumers are participants receiving a service, not merely sources of merchant data.

# 6. Anticipation Intelligence

Sagrenti must turn engagement facts into decision-useful intelligence. Counts alone are insufficient. The service may include trends, velocity, engagement mix, milestone-to-milestone change, relevant dimensions, watch density, and other indicators that show how consumer anticipation is developing.

Anticipation Intelligence is a core merchant value proposition. Existing report and metric domains must be reviewed against this product purpose; inherited filenames must not narrow the service to conventional report generation.

# 7. Consumer Opportunity, Contact, Reviews, and Reputation

## 7.1 Merchant-issued consumer tokens

The merchant issues appropriate consumer opportunities through the Platform. Sagrenti provides issuance and delivery infrastructure without simply transferring consumer identity information to the merchant.

Likely durable domain: `merchant_future_offering_consumer_tokens`.

## 7.2 Consumer-controlled contact

The consumer voluntarily acts on a merchant-issued token to claim, access, reserve, purchase, or otherwise pursue the product or service through the merchant or retailer. The consumer decides whether to make this transition. Consumer Contact is therefore a token-enabled transition, not a separate durable domain merely because the concept exists.

## 7.3 Reviews and merchant reputation

A consumer who acts on a qualifying token may become eligible to review the FO. Reviews belong to the FO; because every FO belongs to a merchant, verified FO reviews accumulate into a persistent merchant reputation.

Likely durable domain: `future_offering_reviews`. Merchant reputation should ordinarily be derived from verified reviews rather than stored as an independent source of truth.

Unlike FO engagement aggregates, merchant reputation may be publicly visible on every FO without prior consumer interaction with that FO.

# 8. Merchant Followership and User Drive

Watch applies to an individual FO. Follow establishes a consumer-to-merchant relationship through which a consumer chooses to keep up with that merchant.

The relationship must create value in both directions without becoming a reciprocal follow model. Consumer consent creates a merchant audience. Sagrenti should enable the merchant to serve that audience through relevant communications, including announcements and follower-first notice of a new FO or other Coming Up activity.

Likely durable domains:

- `consumer_merchant_follows` — the consumer’s durable relationship with the merchant.

- `merchant_follower_announcements` — durable merchant-authored communications, if announcements enter the release scope.

Audience resolution, eligibility, delivery, and notification distribution should normally be derived capabilities.

This contributes to the Platform flywheel: more worthwhile FOs create more reasons to visit; greater consumer participation produces better intelligence; better intelligence increases merchant value; stronger merchant audiences encourage merchants to bring further FOs to Sagrenti.

# 9. External Integration and Outcome Intelligence

External APIs are not required for Release v1.0. A later v1.1 Merchant/Retailer Integration API should expose a stable Sagrenti contract rather than retailer-specific platform semantics.

The integration direction is bidirectional:

- Sagrenti to external platform: token or opportunity and the relevant FO context.

- External platform to Sagrenti: verified token action and outcome information.

The resulting transition is: merchant issues token through Sagrenti; the consumer acts through the merchant or retailer; the token and outcome are recognised; the claim or action occurs; and the result is reported automatically to Sagrenti.

This avoids manual reporting burdens, strengthens Consumer Contact and verified reviews, and provides the proper foundation for Outcome Intelligence: understanding what happened after anticipation and how anticipation related to actual outcomes.

# 10. Release Classification Alignment

Phases 6–10 must be reconstructed around the services Sagrenti promises. Existing domains should be retained where they support those services; genuinely missing durable facts should be added; and derived backend capabilities should be represented explicitly even when they require no new table.

The release inventory should distinguish:

- Durable domain — authoritative persisted facts.

- Derived capability — production service or application behaviour computed or orchestrated from those facts.

The principal durable-domain gaps presently identified are:

- `merchant_future_offering_milestones`

- `merchant_future_offering_consumer_tokens`

- `future_offering_reviews`

- `consumer_merchant_follows`, if Follow is included in v1

- `merchant_follower_announcements`, if follower communications are included in v1

Project management, project actions, discovery, aggregate exposure, Anticipation Intelligence, milestone reporting, reputation, audience resolution, and related experiences should primarily be implemented as derived capabilities over authoritative facts.

Release / Availability remains withdrawn as a separate capability. Its relevant behaviour is represented by merchant milestones and the later token-enabled consumer transition.

# 11. Product-Experience Completion Test

A release phase is not complete merely because its tables, handlers, and billable events exist. Each service area must answer both questions affirmatively:

1.  Can the merchant actually complete the activity for which Sagrenti provides and may charge for service, and can the merchant understand what is being received?

2.  Can the consumer actually receive and use the experience whose participation makes that merchant service valuable?

These questions connect FOPSC to Release Classification and should govern the product-experience inventory, backend capability plan, API surface, and eventual frontend implementation.
