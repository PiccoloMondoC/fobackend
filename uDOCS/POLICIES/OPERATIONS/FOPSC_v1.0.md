# Future Offering Platform Service Capabilities (FOPSC)

## Discussion Summary

### Governing Principle

The Future Offering Platform exists to provide valuable services to merchants and consumers. The Monetization Layer supports those services; the Platform does not exist merely to generate billable events.

Capability should therefore be designed in this order:

**Capability → Quality of Service → User Experience → Economics, if warranted.**

Asset Hosting is presently a **platform capability**, not an established fee. `Asset Hosting Overage Fee` (AHO) is withdrawn from the current fee model and should be removed from the schema and codebase.

The currently established fee families are:

* Anticipation Intelligence Activation Fee
* Platform Service Fee
* Anticipation Intelligence Fee

---

## Platform Capabilities

### a. Create FO Wizard(s)

Merchants require one or more guided workflows for creating and preparing Future Offerings.

**Activation is not a wizard.** `Activate` may simply be the submission action at the completion of the appropriate FO creation workflow.

The creation process should establish the facts Sagrenti needs to provide the subsequent service.

### b. Merchant-Defined Project Milestones

Every FO is a merchant project.

The merchant defines relevant milestones and dates. Sagrenti persists these facts and uses them to support follow-up actions, reminders, notifications, intelligence, and project progress.

Likely durable domain:

`merchant_future_offering_milestones`

### c. FO Project Management

Sagrenti helps the merchant keep the FO moving according to its project plan.

The merchant experience should provide the milestone plan, project status, upcoming actions, due/overdue matters, and other information needed to understand what requires attention.

### d. Consumer Discovery

Sagrenti helps consumers discover FOs they did not already know about.

This can include What’s Next feeds, search, categories, relevant location exploration, merchant pages, trending/popular FOs, and eventually personalized recommendations.

**“What’s Next” is presently our working decoy for the consumer-facing term “Coming Up.”**

### e. Consumer Engagement

Consumers interact with FOs through established engagement actions such as:

* Watch
* Waitlist
* Early Access Request
* Beta
* Reservation Interest
* Preorder Intent

The Platform must provide a clear, useful experience around these actions rather than merely recording engagement events.

#### Aggregate exposure

Engagement aggregates are privacy-safe aggregate information.

Their exposure follows the already-established rules:

**No interaction with an FO:**
No engagement aggregates for that FO are exposed to that consumer.

**Watching an FO:**
Aggregates for all engagement actions enabled on that FO are exposed.

**Participating without Watching:**
Only aggregates corresponding to the consumer's selected engagement actions are exposed.

For example:

`1,247 watching · 183 waitlisted`

is legitimate aggregate anticipation information where the exposure rules permit it.

### f. Anticipation Intelligence

Sagrenti must turn engagement facts into useful intelligence.

This goes beyond storing engagement counts. It can include counts, trends, velocity, engagement mix, changes between milestones, relevant dimensions, and other indicators showing merchants how consumer anticipation is developing.

This is a core merchant value proposition.

### g. Merchant Actions / Decisions

At activation, merchants should understand the actions Sagrenti will expect from them as their FO progresses.

As relevant milestones approach, the Platform supports timely merchant action through reminders, notifications, project information, and relevant anticipation intelligence.

### h. Milestone Reporting

At meaningful milestones, merchants should receive concrete information about how anticipation is tracking and how the project performed during that stage.

This is part of the Anticipation Intelligence service rather than merely conventional administrative reporting.

### i. Release / Availability

**Withdrawn as a separate capability.**

The relevant behavior is already represented by merchant milestones and the eventual token/consumer transition.

### j. Merchant-Issued Consumer Tokens

The merchant issues appropriate consumer opportunities through the Platform.

Sagrenti provides the infrastructure for issuance and delivery without simply handing consumer personal identity information to the merchant.

Likely durable domain:

`merchant_future_offering_consumer_tokens`

### k. Consumer Contact

The consumer voluntarily acts on a merchant-issued token to claim, access, reserve, purchase, or otherwise pursue the product/service through the merchant or retailer.

The consumer decides whether to make this transition.

With future external integration:

**Merchant issues token through Sagrenti → consumer acts through merchant/retailer → token/outcome is recognized → claim/action occurs → result is automatically reported to Sagrenti.**

This avoids creating manual reporting chores for merchants.

### l. Consumer Reviews and Merchant Reputation

Consumers who actually acted on qualifying tokens may become eligible to review the FO.

Reviews belong to the FO, but because every FO belongs to a merchant, verified reviews accumulate into a persistent merchant reputation.

For example:

**Merchant reputation: 4.8 ★ · 326 verified reviews**

Unlike FO engagement aggregates, merchant reputation may be publicly visible on every FO **without requiring the consumer to interact with that FO first**.

Likely durable review domain:

`future_offering_reviews`

Merchant reputation itself should ordinarily be derived from the underlying verified reviews rather than treated as an independent source of truth.

### m. Outcome Intelligence

Outcome Intelligence is a valuable future capability: understanding what happened after anticipation and how consumer anticipation related to actual outcomes.

Manual merchant reporting is undesirable because it creates unnecessary work for merchants.

External merchant/retailer integration provides the better foundation for obtaining trustworthy outcome information automatically.

This is therefore a strong candidate for **v1.1 integration/API capability**, rather than something that must be forced into v1.0.

### n. Consumer Quality of Service + UX

Consumer quality is a cross-cutting Platform responsibility rather than one isolated backend domain.

Discovery, engagement actions, updates, aggregate statistics, privacy, tokens, contact transitions, accessibility, responsiveness, and clarity must collectively make anticipation useful and enjoyable for consumers.

Consumers are participants receiving a service—not merely sources of engagement data.

### o. Sagrenti User Drive

Sagrenti must attract sufficient consumers and worthwhile merchant FOs to make the service valuable to both sides.

The basic flywheel is:

**More worthwhile FOs → more consumer reasons to visit → more anticipation activity → better merchant intelligence → greater merchant value → more worthwhile FOs.**

Discovery, sharing, merchant following, updates, recommendations, and similar mechanisms can contribute to this.

**Watch** applies to an individual FO.

**Follow** can have a distinct role at the merchant level: a consumer follows a merchant to discover and keep up with that merchant's projects.

---

## External Integration Direction

External APIs do not have to be part of Release v1.0.

A later **v1.1 Merchant/Retailer Integration API** can allow Sagrenti to integrate with merchants, Amazon or other major retailers, merchant-owned commerce systems, and other external platforms.

The integration should be based on a stable Sagrenti contract rather than being designed specifically around any one retailer.

It can ultimately support both:

**Sagrenti → external platform:** token/opportunity and relevant FO context.

**External platform → Sagrenti:** verified token action and outcome information.

This capability strengthens Consumer Contact, verified reviews, and eventual Outcome Intelligence.

---

## Persistence Principle

These capabilities do **not** imply that each requires another database table.

New schema should be introduced only where Sagrenti needs to preserve a genuinely new durable fact.

Likely new persistent domains identified so far include:

* `merchant_future_offering_milestones`
* `merchant_future_offering_consumer_tokens`
* `future_offering_reviews`
* `consumer_merchant_follows`, if merchant following is implemented

External integration/outcome persistence should be designed when that capability is undertaken.

Most other capabilities—project status, upcoming actions, aggregate exposure, Anticipation Intelligence, milestone reporting, merchant reputation, discovery, and related experiences—should primarily be **derived through Go services and application logic from authoritative platform facts**, rather than creating redundant sources of truth.
