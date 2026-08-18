### Future Offering Commercial Architecture --- FOCA v1.0

# 1. Purpose and governing commercial principle

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

# 2. Domain Language and Presentation Language Doctrine

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

# 3. Four distinct Future Offering concerns

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

# 4. Watch and the anticipation relationship

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

# 5. Engagement is evolutionary, not prescriptive

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

# 6. Qualified Anticipation Entry --- QAE

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

# 7. Activation cycles establish QAE lifetime

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

# 8. Billing periods do not reset QAE eligibility

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

# 9. Current Future Offering fees

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

# 10. Continuing FO service is not a subscription abstraction

An FO is not a conventional subscription product. The merchant
establishes a commercial/service relationship with Sagrenti for the
duration of a particular Future Offering project.

FOCA therefore separates concepts previously collapsed under
subscription:

Concept                            Meaning

FO Lifecycle                   The operational life of the FO:
draft, activation,
active/published, expiration,
archive and any explicit relaunch

Service Term                   The administratively configured
commercial category through which
available service arrangements are
offered

Service Period                 The exact duration of service
selected for the FO

Billing Period                 The period according to which the
continuing-service obligation is
calculated/billed

The governing relationship is:

Future Offering
   ↓
Service Term
   ↓
Service Period
   ↓
Billing Period
   ↓
Payment Period

These concepts are related but must not be conflated.

The prior merchant_program_subscriptions abstraction should therefore
not govern FO continuing service. The preferred conceptual anchor is:

merchant_future_offering_service_terms

Related data structures should model service periods, billing periods
and payment periods according to their actual semantics rather than
preserving subscription terminology.

# 11. Service Terms and Service Periods

Service Terms are configurable commercial categories

FLEX and EXTENDED are initial Administration configurations, not
permanent Engineering enums.

Administration should be capable, within Engineering safety boundaries,
of:

creating new Service Terms;

activating or deactivating Service Terms for future selection;

configuring merchant-facing labels and descriptions;

changing display order;

configuring allowed Service Period choices or ranges;

associating appropriate Billing Period rules;

associating permitted Payment Period choices; and

evolving commercial policy without requiring recompilation or domain
redesign.

Service Terms require stable internal identity. Mutable labels and
display order must never become durable business identity.

Reordering terms therefore changes presentation, not meaning or
historical identity.

Current initial policy

Current policy is expected to begin with:

FLEX --- intended for genuinely shorter-duration FOs; merchant
selects the required duration, initially up to approximately 12
months.

EXTENDED --- intended for longer-duration FOs, beginning around
the one-year boundary.

There is currently no STANDARD Service Term.

FLEX does not mean cancel anytime, trial, introductory service, or
"see for yourself." Its flexibility is the merchant's ability to choose
the appropriate shorter Service Period.

For example, a FLEX merchant may select an exact period such as 1, 2, 3,
7 or 12 months. Once selected and agreed, that exact period is the FO's
authoritative Service Period.

EXTENDED likewise requires an exact Service Period. "At least one year"
describes a Service Term eligibility boundary, not the merchant's
historical duration.

Permitted EXTENDED choices may include, for example:

12 months
18 months
24 months
4 years
9 years

Administration may cap the practical commercial maximum and increase it
when genuine business need arises.

Engineering may impose an absolute safe upper duration boundary to
protect correctness and bounded operation. A 99-year maximum has been
discussed but is not yet adopted as a settled Engineering invariant.

# 12. Billing Periods, Payment Periods and payment economics

Billing Period

Every selected Service Period operates with an associated Billing
Period.

Current initial policy is expected to associate:

FLEX      → TERM billing period
EXTENDED  → ANNUAL billing period

These associations are Administration policy, not permanent Engineering
constants.

Payment Period

Payment Period is distinct from both Service Period and Billing Period.

Merchants choose from the Payment Periods permitted for the applicable
Billing Period.

Current policy contemplates:

MONTHLY PAYMENT      → full invoice amount
TERM PAYMENT         → may attract configured payment discount
ANNUAL PAYMENT       → may attract configured payment discount

Engineering should provide the capability to configure permitted
associations and safe payment scheduling without hard-coding today's
commercial policy.

Payment discounts are financial/accounting effects

A payment discount is not a production/service charge reduction.

The underlying invoice must preserve the truthful service obligation. A
discount arising because the merchant selected or satisfied a particular
Payment Period is a financial/accounting expense associated with payment
economics and may be recognized after the underlying production/service
charge or invoice.

Accordingly, payment discounts must not be forced onto the invoice as
reductions of the underlying product/service charge merely because they
affect the merchant's ultimate economics.

The financial/accounting architecture should preserve their recognition,
provenance and auditability separately from the production/service fee.

Configuration determines what may be agreed; it never redefines what was agreed

Administration may change future offerings of Service Terms, duration
choices, Billing Period associations, Payment Period choices, labels,
ordering and commercial policy.

But once an FO has entered an authoritative commercial arrangement, the
selected facts must be preserved.

For example:

FLEX
→ 8-month Service Period
→ TERM Billing Period
→ MONTHLY Payment Period

becomes historical commercial fact for that FO.

A later Administration change from FLEX 1--12 months to FLEX 1--6 months
must not alter the existing 8-month arrangement.

Likewise, renaming FLEX or reordering Service Terms must not rewrite
what was historically selected or agreed.

The governing doctrine is:

Configuration determines what may be agreed; it must never redefine
what was agreed.

Engineering must therefore preserve or snapshot the authoritative
commercial facts applicable when the arrangement becomes binding,
including the stable identities and material terms needed to reconstruct
its historical meaning.

# 13. FO Milestones, Merchant Responsibilities and Notifications

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

Milestones may justify Service Period amendments

A legitimate change in Release Date or another material FO milestone may
create a legitimate need to extend or shorten the Service Period.

A Service Period is an agreed commercial fact, but it may be
prospectively amended through an explicit and auditable process.

An increase may extend the FO's continuing-service relationship. A
reduction may shorten future service when the merchant's legitimate
schedule changes. Sagrenti should capture the reason for a reduction as
useful commercial and operational provenance, not as a basis for
automatically penalizing the merchant.

A Service Period amendment creates a new historical fact; it does
not rewrite the original agreement.

Sagrenti should encourage accurate, timely FO updates rather than
economically punish merchants for reporting legitimate schedule
changes.

Absent another configured arrangement, shortening a Service Period does
not retroactively erase service already provided or reverse amounts
attributable to a service period already paid for. Service may conclude
at the end of the applicable period through which service has been paid.

Any amendment that changes the applicable Service Term, Billing Period,
Payment Period, pricing or other material commercial terms must be
resolved explicitly and preserved as part of the amended commercial
arrangement rather than silently inferred from editing a duration field.

# 14. Activation creates the commercial event

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

# 15. Activation requires commercial authorization, not universal prepayment

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

# 16. Invoice items represent Sagrenti products and services

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

# 17. Invoice-item cardinality

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

# 18. Adjustments do not redefine what Sagrenti sold

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

# 19. Commercial effects remain attributable to their cause

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

# 20. Taxes reinforce FO-level commercial provenance

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

# 21. Platform Credits are commercial policy, not product charges

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

# 22. Merchant teams are not the commercial unit

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

# 23. Engineering and Administration boundary

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

Service Term names, descriptions and display order;

merchant-facing fee names and descriptions;

Service Term availability and associations;

Service Period choices, ranges and administrative caps;

continuing-service pricing;

Billing Period and Payment Period choices and associations;

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

# 24. Architectural qualification

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

FOCA v1.0 --- Governing Model

The architecture can be reduced to the following:

Join Sagrenti as a merchant for free.

Every Future Offering is its own commercial project.

Activate an FO → Activation Fee.

Keep it in continuing service → applicable configured
continuing-service fee.

Select a Service Term → choose an exact permitted Service Period →
inherit the associated Billing Period → choose an allowed Payment
Period.

FLEX and EXTENDED are current Admin-configured Service Terms, not
permanent Engineering enums; there is currently no STANDARD term.

FLEX means flexible selection of a shorter committed Service
Period---not cancel anytime or trial service.

Exceed included asset resources → Asset Hosting Overage Fee.

Generate new measurable anticipation relationships → Anticipation
Intelligence Fee based on QAEs.

One participant × one FO × one activation cycle → at most one QAE.

Billing periods aggregate new QAEs; they do not reset QAE
eligibility.

Payment Period is distinct from Service Period and Billing Period.
Payment discounts are financial/accounting effects, not reductions of
the truthful production/service charge.

Configuration determines what may be agreed; it must never redefine
what was agreed.

Every charge and every monetary effect upon that charge remains
attributable to the FO that caused it.

The invoice aggregates established commercial obligations; it does
not redefine them.

Canonical domain concepts remain stable while appropriate
human-facing terminology remains administratively configurable.

Presentation layers resolve configured terminology at runtime
wherever practical rather than turning vocabulary changes into
recompilation and redeployment.

Engineering builds and protects the commercial machinery, stable
identities, historical facts and safe boundaries. Administration
governs commercial policy and human-facing language within those
boundaries.

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
Service Term
      ↓
Exact Service Period
      ↓
Associated Billing Period
      ↓
Selected Payment Period
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

Together these establish the governing answer to FOCA's central
question:

Sagrenti sells Anticipation Intelligence through independently
commercial Future Offerings. Each FO creates its own activation and
continuing-service relationship. Service Terms define configurable
commercial choices; the merchant selects an exact Service Period,
inherits the applicable Billing Period and chooses an allowed Payment
Period. New consumer anticipation is measured through QAEs, configured
fees monetize the relationship, and every resulting commercial
obligation remains attributable to the Future Offering that created
it. Engineering preserves stable meaning, identity, historical fact
and safe operating boundaries while Administration governs appropriate
commercial policy and human-facing language. Configuration determines
what may be agreed; it never retroactively redefines what was
agreed.