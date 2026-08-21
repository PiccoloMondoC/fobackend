### Future Offering Commercial Architecture --- FOCA v1.01

Purpose and governing commercial principle

FOCA defines the commercial architecture of Sagrenti's Future Offering
(FO) model.

Sagrenti is not selling clicks, advertising, individual engagement
actions, or merchant-account access.

Sagrenti sells Anticipation Intelligence.

The governing commercial principle is:

The Future Offering---not the merchant account---is Sagrenti's
fundamental commercial unit.

Creating and maintaining a merchant account is free. Commercial
obligations arise when a particular Future Offering enters a commercial
relationship with Sagrenti.

The governing progression is therefore:

Merchant
↓
Future Offering
↓
Activation Cycle
↓
Consumer Anticipation
↓
Qualified Anticipation Entry (QAE)
↓
FO Fees / Commercial Obligations
↓
Invoice
↓
Settlement

Every FO is its own commercial project with its own lifecycle,
participation, measurement, fees and resulting obligations.

Domain Language and Presentation Language Doctrine

Sagrenti separates canonical domain terminology from human-facing
presentation terminology.

The governing principle is:

Engineering owns stable domain semantics; Administration owns
configurable human-facing language. Frontend and other presentation
layers should resolve configured terminology at runtime wherever
practical rather than requiring source-code modification,
recompilation or redeployment when Sagrenti changes its public
vocabulary.

This distinction is an architectural capability requirement, not
merely an Admin Console convenience.

Stable domain concepts

Engineering requires stable identities and semantics upon which platform
behavior, persistence, APIs, lifecycle rules, billing, auditability and
integrations can depend.

Examples include:

Future Offering
Anticipation Intelligence
Qualified Anticipation Entry
Activation Cycle
Service Term
Service Period
Billing Period
Payment Period
Fee Type
Platform Credit

These canonical concepts should not be renamed merely because Sagrenti
discovers better language for explaining them to consumers or merchants.

Configurable presentation language

Administration may configure appropriate human-facing terminology
independently of the underlying domain identity.

For example:

Stable Engineering Concept   Possible Human-Facing Language

Future Offering              Coming Up
Anticipation Intelligence    Signals / Early Signals
Activation Cycle             Configured human terminology
Service term                 Configured merchant terminology
Fee type                     Configured merchant-facing label
Platform Credit              Configured commercial terminology

Accordingly:

Coming Up is not the Engineering replacement for Future Offering.

And:

Signals is not the Engineering replacement for Anticipation
Intelligence.

They are current public expressions of stable underlying concepts.

Sagrenti has not renamed its architecture merely because it has improved
its public language. It has separated the language the platform requires
internally from the language people should be expected to understand.

Runtime terminology resolution

Where terminology is designated as administratively configurable,
Engineering should provide an authoritative runtime
terminology-resolution capability that presentation surfaces can
consume.

Conceptually:

CANONICAL DOMAIN
────────────────────────────
Future Offering
Anticipation Intelligence
Qualified Anticipation Entry
Activation Cycle
Service Term
Service Period
Billing Period
Payment Period
Fee Type
Platform Credit

         ↓
 authoritative runtime

terminology configuration
↓

PRESENTATION
────────────────────────────
Coming Up
Signals
Early Signals
Configured plan names
Configured fee names
Configured commercial language
Future improved terminology

Changing:

Future Offering presentation
Coming Up → Upcoming

or:

Anticipation Intelligence presentation
Signals → Market Signals

should therefore normally be an Administration configuration change
rather than a frontend source-code change.

Frontend code should depend upon stable semantic identities and resolve
the applicable presentation language at runtime wherever practical.

Presentation terminology is not domain identity

Human-facing labels must not become authoritative keys, enum values,
lifecycle identifiers, billing identities or other durable domain
identifiers merely because those labels are currently displayed.

Engineering behavior should depend on stable canonical identity.

Therefore changing a label must not silently change:

QAE qualification;

QAE uniqueness;

activation-cycle semantics;

engagement semantics;

fee-calculation behavior;

service-term, service-period, billing-period and payment-period
semantics;

commercial provenance;

lifecycle rules;

authorization;

auditability;

historical meaning.

Configuration changes what Sagrenti calls a concept, not what the
concept is.

Scope and boundaries

Not every word appearing in the frontend needs to become database
configuration.

Engineering should provide runtime configurability where terminology
represents legitimate product, commercial, consumer-facing or
merchant-facing vocabulary that Administration may reasonably need to
evolve.

Ordinary interface prose, explanatory copy and purely
implementation-specific language may continue to be managed through the
appropriate presentation or content mechanisms.

The architectural requirement is to avoid coupling legitimate business
vocabulary changes to recompilation or domain redesign.

Historical integrity

Runtime terminology changes must not rewrite authoritative history.

Where terminology becomes part of an issued financial record,
contractual representation, auditable event or other historically
authoritative artifact, the platform must preserve or snapshot the
terminology applicable when that representation became authoritative.

Thus:

Current UI
→ may resolve today's configured terminology

Issued historical record
→ preserves the authoritative terminology applicable when issued

A future Administration change from Signals to Market Signals,
for example, must not silently rewrite previously issued invoices or
other records whose historical presentation must remain intact.

Ownership across the architecture

FOCA is the primary governing owner of product/domain vocabulary
separation.

Other architectures inherit the doctrine within their respective scopes:

invoice and financial presentation architecture applies it to
financial terminology and historical invoice rendering;

promotions and Platform Credit architecture applies it to promotion,
credit and merchant-facing commercial terminology;

frontend presentation applies configured terminology without
redefining canonical domain semantics.

The governing rule throughout Sagrenti is therefore:

Stable meaning belongs to the architecture. Changeable language
belongs to configuration where appropriate.

Four distinct Future Offering concerns

The Future Offering architecture separates four concepts that must not
be conflated.

Consumer Engagement Actions

These describe what consumers can do:

Watch

Waitlist

Early Access Request

Beta

Reservation Interest

Preorder Intent

(Potential future) Draw Entry

Merchant Access Policies

These describe who may participate:

Public

Invite Only

(Potential future) Approval Required

Merchant Release Strategies

These describe how the FO is released:

Drop

Scheduled Release

Rolling Release

Limited Quantity

Commercial Measurement

This describes what Sagrenti measures and monetizes:

Activation

Continuing FO service

Asset usage

Qualified Anticipation Entries

These concerns evolve independently.

Watch and the anticipation relationship

Watch is platform-owned.

Unlike merchant-configurable engagement actions, Watch:

is always available;

cannot be disabled by the merchant;

represents the consumer's ongoing anticipation relationship with the
FO.

A consumer may enter that relationship directly by Watching or
indirectly by performing another qualifying engagement action.

If the consumer selects Waitlist, Early Access Request, Beta,
Reservation Interest, Preorder Intent or another qualifying engagement
without already Watching, the platform establishes the Watch
relationship automatically.

Consumers with no engagement receive no ongoing intelligence for that
FO. Once the anticipation relationship exists, the consumer receives the
FO's continuing intelligence stream.

Engagement is evolutionary, not prescriptive

A consumer might progress through:

Watch
↓
Waitlist
↓
Beta
↓
Reservation Interest
↓
Preorder Intent

But this is not a mandatory funnel.

Consumers may enter at different points, skip stages, add engagements
later, or stop participating.

Each engagement remains independently recorded as an event for
operational history and analytics.

Commercial billing does not count those events independently.

Qualified Anticipation Entry --- QAE

The Qualified Anticipation Entry is Sagrenti's fundamental Anticipation
Intelligence usage unit.

A QAE represents:

One participant's first qualifying entry into one specific Future
Offering during one activation cycle.

Its uniqueness boundary is:

Participant × Future Offering × Activation Cycle
=
at most one QAE

For example:

Watch                   → first entry → 1 QAE
Waitlist                →             → 0 additional QAE
Beta                    →             → 0 additional QAE
Reservation Interest    →             → 0 additional QAE
Preorder Intent         →             → 0 additional QAE

Subsequent engagement enriches Anticipation Intelligence but does not
repeatedly charge the merchant for the same participant relationship.

A participant engaging with 100 different FOs may generate 100 QAEs
because each FO is commercially independent.

Activation cycles establish QAE lifetime

Every activated FO operates within an identifiable activation cycle.

The activation cycle defines the lifetime during which a participant can
become a new QAE only once.

A genuine FO relaunch establishes a new activation cycle and therefore a
new QAE eligibility boundary. Simply reaching an expiration date must
not silently manufacture a relaunch. Relaunch is an explicit lifecycle
event carrying commercial meaning.

Activation cycles may support short or long-running projects, including
merchant-defined durations and end dates.

Engineering must provide the capability without imposing arbitrary
six-month, twelve-month or similar lifecycle assumptions. Administration
may govern available choices, defaults and limits within safe
Engineering boundaries.

Billing periods do not reset QAE eligibility

A billing period determines when a newly created QAE is billed.

It does not make an existing participant new again.

A participant entering a five-year FO in September 2026 produces one QAE
during that activation cycle. Continued participation in 2027, 2028 or
later does not create another QAE merely because another billing period
or year has begun.

Therefore periodic Anticipation Intelligence billing measures:

Specific FO
+
Specific billing period
+
QAEs whose first qualifying entry occurred during that period
+
Applicable configured QAE price
↓
Fee Calculation
↓
Invoice Item

QAE measures new anticipation relationships, not the continuing size
of an FO's participant population.

Current Future Offering fees

The current FO commercial architecture contemplates four product/service
fee concepts:

Canonical Fee Concept              Commercial purpose

Anticipation Intelligence        Entering an FO into active
Activation Fee                   commercial service

Continuing-Service Fee         Continuing platform service for an
(canonical name to be finalized) active FO

Asset Hosting Overage Fee      FO asset consumption beyond its
included allowance

These are FO-level charges, not merchant-account charges.

The former Subscription Fee terminology must not be treated as
settled merely because the earlier architecture used a subscription
abstraction. The canonical name of the continuing-service fee should be
finalized separately without reintroducing subscription semantics by
accident.

The canonical fee identity and semantics must remain stable
independently of the merchant-facing description.

Administration must be able to configure appropriate merchant-facing fee
names and descriptions without changing the underlying fee type or
requiring Commerce Architecture to be rewritten.

Invoice architecture determines the additional snapshot and
historical-integrity requirements that apply once such terminology
becomes part of an authoritative financial record.

Continuing FO service is not a subscription abstraction

An FO is not a conventional subscription product. The merchant establishes a
commercial/service relationship with Sagrenti for the duration of a particular
Future Offering project.

FOCA therefore separates concepts previously collapsed under subscription:

Concept                            Meaning

FO Lifecycle                       The operational life of the FO:
draft, activation, active/published,
expiration, archive and any explicit
relaunch

Service Term                       The overall agreed duration for which a
specific FO receives Sagrenti service

Service Period                     An individual performance window occurring
within the Service Term during which
Sagrenti renders continuing service,
measures applicable activity and provides
utility

Billing Period                     The period or cadence according to which
applicable continuing-service obligations
are calculated and billed

Payment Period                     The permitted period or schedule according
to which the merchant satisfies the
resulting financial obligation

These concepts are related but must not be conflated.

The governing relationship is:

Future Offering
↓
Service Term
↓
One or more Service Periods
↓
Applicable Billing Periods
↓
Permitted Payment Periods

A Service Term answers:

For how long has Sagrenti agreed to provide service to this Future Offering?

A Service Period answers:

What individual performance window within that Service Term is Sagrenti
presently serving?

A Billing Period answers:

Over what period or cadence is the applicable commercial obligation calculated
and billed?

A Payment Period answers:

Under what permitted schedule is that financial obligation satisfied?

The prior merchant_program_subscriptions abstraction should therefore not
govern FO continuing service. Related data structures should model Service
Terms, Service Periods, Billing Periods and Payment Periods according to their
actual semantics rather than preserving subscription terminology.

Service Terms

The Service Term is the FO's overall service duration.

A Service Term is the overall duration for which Sagrenti agrees to provide
service to a particular Future Offering.

Examples include:

3 months

5 months

7 months

12 months

19 months

or another merchant-selected duration within the currently permitted operating
boundaries.

The merchant determines how long the Future Offering project should run.

Sagrenti must not manufacture arbitrary commercial duration packages merely
because predefined packages are convenient to model.

A merchant may therefore legitimately choose durations such as 3, 5, 7, 11,
19, or other numbers of months, provided the selected duration falls within
the currently permitted operating boundaries.

Engineering implements duration capability. Administration governs the
practical operating range within that capability. The merchant determines the
actual Service Term required for the FO within those permitted boundaries.

Engineering establishes an absolute duration safety boundary

An effectively unlimited Service Term is not acceptable merely because the
database can store a large integer.

Extremely large or malicious duration values could eventually affect:

date arithmetic;

Service Period generation;

resource consumption;

job generation;

indexing and query behavior;

integer conversions; and

bounded platform operation.

Engineering therefore establishes the absolute supported Service Term envelope
as:

Minimum Engineering duration: > 0 months
Maximum Engineering duration: 1188 months / 99 years

The 99-year maximum is an Engineering safety invariant, not commercial policy.

Administration may narrow this envelope but may never widen it.

Administration governs the operating duration range

Within Engineering's absolute capability envelope, Administration may
configure the practical minimum and maximum Service Term currently available
to merchants.

For example:

Engineering capability

0 months ─────────────────────────────────────── 99 years

Administration policy
1 month ───────── 9 years

Administrative limits may change without schema migrations.

A future decision that merchants require, for example, at least 30, 60, or 90
days of service is therefore an operational/product policy decision unless
Engineering identifies an invariant technical reason for that minimum.

The governing separation is:

Engineering implements duration capability and its absolute safety boundary.
Administration determines the currently permitted operating range within that
capability. The merchant determines the actual Service Term required for the
Future Offering within those permitted boundaries.

Configuration determines what may be agreed; it must never redefine what was
agreed.

Once an FO enters an authoritative commercial arrangement, its selected
Service Term becomes historical commercial fact. Later changes to
Administration's permitted range must not alter that existing agreement.

Service Periods, Billing Periods, Payment Periods and payment economics

Service Period

A Service Period is an individual performance window occurring within a
Service Term during which Sagrenti renders continuing service, measures
applicable activity and provides utility.

A Service Period is not another name for the Service Term and is not the
merchant's overall selected FO duration.

For example, a 7-month Service Term may contain seven monthly Service Periods.

Service Term
↓
Service Period 1
Service Period 2
Service Period 3
Service Period 4
Service Period 5
Service Period 6
Service Period 7

where the applicable architecture establishes monthly Service Periods.

Likewise, a longer Service Term may contain many successive Service Periods.

Service Periods provide bounded operational windows within the larger agreed
Service Term. They may support continuing-service performance, period-specific
commercial attribution, usage measurement, service-state tracking, billing
inputs where applicable, historical reconstruction, operational scheduling
and auditable period-by-period service provenance.

Beginning a new Service Period does not create a new Service Term.

Billing Period

A Billing Period determines the applicable window or cadence according to
which a commercial obligation is calculated and billed.

A Billing Period is not the overall Service Term, is not automatically
identical to a Service Period, and is not a Payment Period.

The architecture may align a Billing Period with a Service Period where
commercial policy requires it, but semantic alignment in a particular
configuration must not collapse the two concepts into one domain identity.

Administration may configure permitted Billing Period rules and associations
within Engineering-supported boundaries. Engineering must implement the
capability rather than hard-code today's commercial cadence.

Payment Period

Payment Period is distinct from Service Term, Service Period and Billing
Period.

It governs the permitted schedule or arrangement under which the merchant
satisfies an established financial obligation.

Administration may configure permitted Payment Period choices and their
associations with applicable Billing Periods.

Engineering should provide safe payment scheduling capability without
embedding today's commercial policy as permanent domain behavior.

Payment discounts are financial/accounting effects

A payment discount is not a production/service charge reduction.

The underlying invoice must preserve the truthful service obligation. A
discount arising because the merchant selected or satisfied a particular
Payment Period is a financial/accounting expense associated with payment
economics and may be recognized after the underlying production/service charge
or invoice.

Accordingly, payment discounts must not be forced onto the invoice as
reductions of the underlying product/service charge merely because they affect
the merchant's ultimate economics.

The financial/accounting architecture should preserve their recognition,
provenance and auditability separately from the production/service fee.

Configuration determines what may be agreed; it never redefines what was agreed

Administration may change future permitted Service Term ranges, Service Period
configuration, Billing Period associations, Payment Period choices, labels,
pricing and commercial policy.

But once an FO has entered an authoritative commercial arrangement, the
material selected facts must be preserved.

A later Administration configuration change must not retroactively change an
existing FO's Service Term or rewrite Service Periods already performed.

Engineering must therefore preserve or snapshot the authoritative commercial
facts applicable when the arrangement becomes binding, including the stable
identities and material terms needed to reconstruct its historical meaning.

FO Milestones, Merchant Responsibilities and Notifications

Sagrenti should actively support merchants throughout the life of a
Future Offering by helping them establish, maintain and act upon an
accurate FO Milestone Schedule.

The merchant owns the operational milestones of its Future Offering.
Sagrenti provides the capability to record, surface, track and remind
the merchant about those milestones.

Activation establishes forward-looking expectations

Activation should make clear that the merchant is expected to maintain
the material dates and milestones needed to operate the FO successfully.

Based on the FO's enabled engagement types, Sagrenti may suggest
relevant milestones during Activation, while the merchant establishes
the actual dates appropriate to its own release plan.

Examples may include:

Beta access / Beta token issuance
Waitlist token issuance
Early Access transition
Preorder token issuance
Reservation token issuance
Release
Other merchant-defined milestones

Release Date is an important FO milestone, but it is not the sole clock
from which every other merchant action must be mechanically derived.
Different merchants may legitimately use different schedules even when
their release dates are identical.

Merchants maintain their own milestone schedule

During the active life of the FO, merchants should be able and expected
to keep material milestones current.

Sagrenti should support this through:

prominent Dashboard visibility;

highlighting of incomplete, approaching or overdue milestone
information;

in-app notification actions;

advance reminders;

appropriate prompts when milestone information appears stale or
requires confirmation; and

historical recording of material milestone changes.

The purpose is to help merchants remain operationally prepared, not to
punish them for reporting legitimate changes.

Notifications ride the established milestone schedule

Once a merchant has established a milestone, Sagrenti has a clear basis
for providing timely reminders.

Merchant establishes milestone
↓
Sagrenti tracks milestone
↓
Configured advance-reminder threshold reached
↓
Dashboard / in-app notification
↓
Merchant takes required action
↓
Completion or updated milestone recorded

Reminder timing such as X days before a milestone is legitimate
Administration configuration within Engineering-supported boundaries.

Milestone changes preserve history and reschedule the future

A merchant may legitimately change Release Date or another future
milestone because the underlying product, service, venue, event or
project schedule has changed.

Engineering must preserve the history of material changes rather than
silently overwriting authoritative past state. Completed historical
actions remain facts; new dates become authoritative prospectively;
applicable future reminders may be recalculated; and previously
completed events are not manufactured again merely because a future date
moved.

Merchants issue consumer tokens

Where an engagement requires a token or credential through which a
qualified consumer can claim a merchant-controlled position, access
opportunity or commercial transition:

The merchant issues the token. Sagrenti does not issue the
merchant's commercial entitlement.

Sagrenti provides supporting infrastructure to help the merchant
identify the relevant engagement population, manage the milestone,
perform or record token distribution, track completion and receive
reminders.

Possible token-related milestones include merchant issuance of tokens
for Waitlist, Beta, Early Access where applicable, Preorder,
Reservation, and future engagement types requiring a merchant-controlled
transition.

Engagement type does not determine fulfillment priority

Beta, Waitlist, Preorder, Reservation and other engagement types
represent different relationships and stages. Their names must not
silently impose a universal fulfillment hierarchy.

Engagement type does not inherently determine fulfillment
priority.

The merchant's applicable release, allocation or fulfillment policy
determines the relationship among eligible populations, subject to safe
Engineering boundaries and commitments already made to consumers.

Milestones may justify Service Term amendments

A legitimate change in Release Date or another material FO milestone may
create a legitimate need to extend or shorten the Service Term.

A Service Term is an agreed commercial fact, but it may be
prospectively amended through an explicit and auditable process.

An increase may extend the FO's continuing-service relationship. A
reduction may shorten future service when the merchant's legitimate
schedule changes. Sagrenti should capture the reason for a reduction as
useful commercial and operational provenance, not as a basis for
automatically penalizing the merchant.

A Service Term amendment creates a new historical fact; it does
not rewrite the original agreement.

Sagrenti should encourage accurate, timely FO updates rather than
economically punish merchants for reporting legitimate schedule
changes.

Absent another configured arrangement, shortening a Service Term does
not retroactively erase service already provided or reverse amounts
attributable to a service period already paid for. Service may conclude
at the end of the applicable period through which service has been paid.

Any amendment that changes the Service Term, applicable Service Period schedule, Billing Period,
Payment Period, pricing or other material commercial terms must be
resolved explicitly and preserved as part of the amended commercial
arrangement rather than silently inferred from editing a duration field.

Future Offering eligibility and governance boundaries

Service duration must not become the definition of a Future Offering

A minimum Service Term alone cannot protect Sagrenti from becoming an ordinary
ecommerce marketplace.

For example, requiring a merchant to purchase three months of service would
not transform existing ready-to-ship inventory into a Future Offering.

Service Term duration and Future Offering eligibility are therefore separate
concerns.

Future Offering eligibility requires genuine future commerce

A Future Offering represents a genuine future market offering for which
meaningful pre-market anticipation exists before ordinary commercial
availability.

The governing question is not merely:

Will this product be sold sometime after today?

The stronger question is:

Is there a genuine forthcoming product or service proposition for which
anticipation can meaningfully exist and be measured before ordinary market
availability?

This distinction protects Sagrenti's Future Offering model from collapsing
into conventional present-commerce retail.

Sagrenti is not an ordinary store or resale marketplace

Sagrenti's Future Offering capability is intended to support merchants
bringing forthcoming products, services, experiences, projects and other
offerings toward market.

It is not intended to provide another storefront for ordinary resale of
already-available commodities or inventory.

Repackaging or reselling an existing product, with no genuine forthcoming
product/service proposition, should therefore not qualify merely because the
merchant assigns it a future date.

Where inventory is already commercially ready for immediate ordinary sale and
shipment, conventional ecommerce marketplaces are the appropriate commerce
infrastructure.

A Future Offering does not have to be physically unfinished

Future Offering eligibility must not be defined as:

The product must still be unfinished.

That would incorrectly exclude legitimate future-market situations.

A finished prototype may be an especially strong Future Offering use case.

A merchant may have completed a fully functioning prototype while still
needing to:

validate market anticipation;

understand likely demand;

determine manufacturing scale;

prepare production;

secure distribution; and

establish launch readiness.

Sagrenti can provide meaningful intelligence precisely during that interval.

The distinction is therefore between future market availability and present
ordinary availability, not simply between unfinished and finished products.

Completed digital and creative works require the same distinction

Physical incompleteness cannot be the eligibility test because many legitimate
Future Offerings may already exist in completed form before market release.

Examples may include:

an unpublished completed book;

a completed album awaiting release;

completed software awaiting launch; and

other digital or creative works with a genuine future market event.

Completion does not automatically disqualify an offering.

Conversely, an already ordinarily available digital download should not become
a Future Offering merely because a merchant chooses to list or promote it
through Sagrenti.

The relevant distinction remains whether a genuine future market availability
or launch event exists.

Three separate governance boundaries must be preserved

Future Offering architecture must distinguish:

Engineering safety boundary

Determines what the platform can safely and correctly support.

For Service Terms:

0 < Service Term ≤ 1188 months / 99 years

Future Offering eligibility boundary

Determines whether something genuinely belongs within future commerce rather
than present-commerce retail.

This includes preserving the requirement for meaningful pre-market
anticipation before ordinary commercial availability.

Administration operating policy

Determines how Sagrenti presently operates within Engineering's capability and
the FO domain boundary.

Examples may include:

practical minimum Service Term;

practical maximum Service Term;

Service Period configuration;

minimum anticipation runway;

eligibility/review policies;

Billing Period policy;

Payment Period policy; and

other configurable operating requirements.

Administration may govern these policies dynamically, but may not redefine
Engineering safety invariants or transform ordinary present-commerce inventory
into a Future Offering merely through configuration.

The resulting architecture preserves this separation:

Engineering determines what Sagrenti can safely support. Administration
determines how Sagrenti presently operates within those boundaries. Future
Offering Architecture determines what genuinely constitutes future commerce.

A merchant's freedom to determine the duration of a project must therefore
coexist with two protections:

Engineering's absolute safety envelope; and

the requirement that the underlying project genuinely qualify as a Future
Offering.

Neither arbitrary duration packages nor arbitrary future dates should
substitute for those principles.

Activation creates the commercial event

Submission of an FO for activation creates the Activation billable
event.

That event answers:

Why is Sagrenti entitled to calculate an Activation Fee?

The resulting fee calculation answers:

How much is owed under the applicable commercial terms?

Invoicing answers:

When and on which invoice is that calculated charge presented?

Payment answers:

How is that obligation ultimately satisfied?

Payment therefore must not be treated as the Activation billable
event.

Activation requires commercial authorization, not universal prepayment

The existence of an Activation Fee does not imply that every merchant
must pay it before activation.

Engineering must support at least two legitimate paths.

Immediate settlement

Activation Fee calculated
↓
Activation obligation/invoice
↓
Payment satisfied
↓
FO activated

Authorized deferred settlement

Activation Fee calculated
↓
Deferred/invoiced settlement authorized
↓
FO activated
↓
Activation Fee included on applicable invoice
↓
Settlement under configured terms

This supports governments, universities, enterprises and other
organizations operating under authorized procurement or invoiced terms.

The Engineering invariant is therefore not:

Activation Fee must always be paid before activation.

Instead:

The Activation Fee must reach an authorized commercial disposition
before activation proceeds.

That disposition may be payment or authorized deferred/invoiced
settlement. The platform must preserve the distinction and must never
record payment as satisfied when no payment occurred.

Administration determines who qualifies for deferred terms.

Invoice items represent Sagrenti products and services

An invoice item represents:

One independently calculated product/service charge represented by
one line on an invoice.

The commercial progression is:

Billable Event
↓
Fee Calculation
↓
Invoice Item
↓
Invoice

Invoice items therefore represent Sagrenti products/services such as:

Anticipation Intelligence Activation Fee

Continuing-Service Fee (canonical name to be finalized)

Asset Hosting Overage Fee

Anticipation Intelligence Fee

Those names express the canonical commercial concepts. Merchant-facing
invoice terminology may be administratively configured subject to the
Domain Language and Presentation Language Doctrine and the
historical-integrity requirements of the invoice architecture.

Credits, rebates, discounts, taxes, surcharges and payments are not
transformed into products merely because they may appear visually near a
product line on a rendered invoice.

Invoice-item cardinality

The agreed relationship is:

1 Invoice
↓
many Invoice Items

1 Invoice Item
↓
exactly 1 Fee Calculation

1 Fee Calculation
↓
at most 1 Invoice Item

Accordingly, fee_calculation_id belongs directly on
merchant_invoice_items and must be unique there. A separate
invoice-item/fee-calculation association table is unnecessary.

For periodic Anticipation Intelligence usage:

FO UM120 — August

6,500 new QAE
× configured CPQAE
= calculated Anticipation Intelligence Fee
↓
one Fee Calculation
↓
one Invoice Item

Adjustments do not redefine what Sagrenti sold

Invoice items preserve the truthful underlying product/service charge.

Platform Credits, rebates, discounts, taxes, surcharges and similar
monetary effects must not falsify:

quantity;

unit amount;

gross calculated charge.

For example:

Activation Fee

Quantity          1
Unit amount       $100
Gross charge      $100
Platform Credit   -$50
Net obligation     $50

The $100 Activation Fee remains commercially true. The Platform Credit
changes the resulting obligation rather than rewriting the underlying
fee calculation.

Commercial effects remain attributable to their cause

The governing provenance principle is:

Adjustments should be recorded and attributed at the lowest
commercial grain that gives rise to them.

Taxes, surcharges, Platform Credits, rebates and discounts therefore
remain attributable to the specific FO and commercial charge they
affect.

This permanently answers:

Which FO?
Which charge?
What monetary effect?
How much?
Why?
Who authorized it?

Such effects should not become artificial negative invoice items merely
for presentation convenience.

The invoice aggregates established obligations; it does not redefine
their provenance.

Taxes reinforce FO-level commercial provenance

Different FOs appearing on the same invoice may carry different
jurisdictional and tax consequences.

Therefore invoice-wide tax or adjustment values cannot serve as the
authoritative source of commercial provenance.

Taxes and other charge-related monetary effects must be calculated and
preserved against the FO-specific charge to which they apply.
Invoice-level totals may aggregate them for presentation.

The broader hierarchy is:

Merchant
↓
Future Offering
│
├── Commercial Charges
│      ├── Activation Fee
│      ├── Continuing-Service Fee
│      ├── Asset Hosting Overage Fee
│      └── Anticipation Intelligence Fee
│
└── Charge-related Monetary Effects
├── Taxes
├── Surcharges
├── Platform Credits
├── Rebates
└── Discounts

Platform Credits are commercial policy, not product charges

Platform Credits are commercial reductions rather than invoice products.

Current promotional policy hypotheses include:

$300 award per qualifying merchant;

eligibility currently focused on Activation and continuing-service
fees;

maximum application of 50% of an eligible fee;

approximately 12-month promotional issuance period;

award validity of up to three years, subject to utilization rules.

These values and eligibility rules are Administration policy, not
Engineering invariants.

Engineering provides configurable, auditable enforcement and preserves
the relationship between each credit application and the affected
commercial charge.

The canonical Platform Credit concept is also distinct from its
merchant-facing name. Promotion and credit architecture may configure
appropriate public terminology without changing the underlying identity
or semantics.

Merchant teams are not the commercial unit

Merchant teams are an administrative capability.

Sagrenti charges merchants for the commercial services and Anticipation
Intelligence associated with their Future Offerings---not simply for
adding employees to the merchant account.

Multi-user access therefore does not inherently create additional
Activation Fees or Anticipation Intelligence Fees.

Future enterprise administration capabilities such as SSO, governance,
compliance or advanced collaboration may justify different commercial
products or plans because they provide additional value, but that is
distinct from charging for ordinary team membership.

Engineering and Administration boundary

Engineering implements the complete commercial capability and safe
operating boundaries.

Administration governs commercial and operational behavior within those
boundaries.

This includes a deliberate separation of responsibility:

Engineering owns stable domain semantics and identities.
Administration owns legitimate configurable commercial policy and
human-facing terminology.

Administration should be capable of configuring matters such as:

consumer-facing and merchant-facing terminology where designated
configurable;

Service Term operating minimums, maximums and duration policy;

merchant-facing fee names and descriptions;

Service Period cadence and generation policy;

Billing Period choices, cadence and associations;

continuing-service pricing;

Payment Period choices and associations;

activation settlement policies;

QAE pricing;

included asset allowances and overage policy;

Platform Credit eligibility and limits;

promotion names and terms;

commercial thresholds and availability.

Engineering owns invariants including:

canonical domain identity;

stable domain semantics;

commercial provenance;

exact monetary arithmetic;

lifecycle integrity;

QAE uniqueness;

authorization boundaries;

auditability;

configuration safety;

historical integrity;

data integrity.

Engineering must provide the configuration and runtime-resolution
capability needed to prevent legitimate terminology changes from
becoming unnecessary software changes.

Administration may change configured presentation language and
commercial policy, but cannot use configuration to redefine canonical
domain semantics or weaken Engineering invariants.

Accordingly:

Administration may change:
"What do we call this?"

Administration may configure:
"How do we commercially operate this?"

Administration may not redefine:
"What is this?"

Administration may not override:
"What must remain true for the platform to be correct and safe?"

This is the governing Engineering/Admin boundary throughout FOCA.

Architectural qualification

FOCA does not declare that Sagrenti can never introduce a merchant-level
commercial product.

Its governing doctrine is narrower:

Under the current commercial model, merchant-account existence is
non-billable and Sagrenti's merchant commercial obligations originate
at the Future Offering level.

Every current fee and every charge-related tax, surcharge, credit,
rebate or discount must remain attributable to the FO and commercial
charge that caused it.

This protects the present architecture without unnecessarily preventing
future commercial models.

FOCA v1.01 --- Governing Model

The architecture can be reduced to the following:

Join Sagrenti as a merchant for free.

Every Future Offering is its own commercial project.

Activate an FO → Activation Fee.

Keep it in continuing service → applicable configured continuing-service fee.

Merchant determines required FO duration → Service Term established within
Engineering's absolute safety envelope and Administration's permitted operating
range → successive Service Periods occur within that Service Term → applicable
Billing Periods determine billing windows/cadence → permitted Payment Period
governs settlement scheduling.

The Service Term is the FO's overall agreed service duration. A Service Period
is an individual performance window within that Service Term. Billing Period
and Payment Period remain separate concepts.

Exceed included asset resources → Asset Hosting Overage Fee.

Generate new measurable anticipation relationships → Anticipation Intelligence
Fee based on QAEs.

One participant × one FO × one activation cycle → at most one QAE.

Billing periods aggregate new QAEs; they do not reset QAE eligibility.

Beginning a new Service Period or Billing Period does not create a new Service
Term and does not reset QAE eligibility.

Payment discounts are financial/accounting effects, not reductions of the
truthful production/service charge.

Configuration determines what may be agreed; it must never redefine what was
agreed.

Every charge and every monetary effect upon that charge remains attributable
to the FO that caused it.

The invoice aggregates established commercial obligations; it does not
redefine them.

Canonical domain concepts remain stable while appropriate human-facing
terminology remains administratively configurable.

Presentation layers resolve configured terminology at runtime wherever
practical rather than turning vocabulary changes into recompilation and
redeployment.

Engineering builds and protects the commercial machinery, stable identities,
historical facts and safe boundaries. Administration governs commercial policy
and human-facing language within those boundaries.

The resulting commercial chain is:

FUTURE OFFERING
↓
Activation Cycle
↓
Consumer enters anticipation
↓
Qualified Anticipation Entry
↓
Billing-period aggregation
↓
Fee Calculation
↓
Invoice Item
↓
Invoice
↓
Settlement

while continuing FO service operates alongside it:

ACTIVE FUTURE OFFERING
↓
Merchant-selected Service Term
↓
Successive Service Periods
↓
Applicable Billing Periods
↓
Permitted Payment Period
↓
Configured Continuing-Service Economics

while merchant operations are supported by the FO milestone schedule:

FO ACTIVATION
↓
Merchant establishes / confirms milestones
↓
Sagrenti tracks approaching dates
↓
Dashboard highlighting / in-app reminders
↓
Merchant action / token issuance / milestone update
↓
Completion and history preserved

and the presentation architecture sits above the stable domain:

STABLE DOMAIN SEMANTICS
↓
Authorized Administration Configuration
↓
Runtime Terminology Resolution
↓
Consumer / Merchant Presentation

Together these establish the governing answer to FOCA's central question:

Sagrenti sells Anticipation Intelligence through independently commercial
Future Offerings. Each FO creates its own activation and continuing-service
relationship. The merchant determines the required overall Service Term within
Engineering's absolute safety envelope and Administration's currently
permitted operating range. Sagrenti performs service through individual
Service Periods occurring within that Service Term. Billing Periods govern
applicable billing windows or cadence, while Payment Periods govern permitted
settlement scheduling. New consumer anticipation is measured through QAEs,
configured fees monetize the relationship, and every resulting commercial
obligation remains attributable to the Future Offering that created it.
Engineering preserves stable meaning, identity, historical fact and safe
operating boundaries while Administration governs appropriate commercial
policy and human-facing language. Configuration determines what may be agreed;
it never retroactively redefines what was agreed.

Waitlist, Beta, Preorder, Reservation, etc. are independently enabled
engagement options. A merchant may choose any applicable combination—including
Waitlist without Preorder or Preorder without Waitlist. Where multiple
engagement types coexist, their coexistence does not establish an inherent
fulfillment priority.