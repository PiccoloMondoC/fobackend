### Future Offering Commercial Architecture --- FOCA v1.02

1.  Purpose and governing commercial principle

FOCA defines the commercial architecture of Sagrenti's Future Offering
(FO) model.

Sagrenti is not selling clicks, advertising, individual engagement
actions, or merchant-account access.

Sagrenti sells Consumer Anticipation Intelligence.

The governing commercial principle is:

The Future Offering---not the merchant account---is Sagrenti's
fundamental commercial unit.

Creating and maintaining a merchant account is free. Commercial
obligations arise when a particular Future Offering enters a commercial
relationship with Sagrenti.

The governing progression is therefore:

Merchant ↓ Future Offering ↓ Activation Cycle ↓ Consumer Anticipation ↓
Qualified Anticipation Entry (QAE) ↓ FO Fees / Commercial Obligations ↓
Invoice ↓ Settlement

Every FO is its own commercial project with its own lifecycle,
participation, measurement, fees and resulting obligations.

1A. Consumer Anticipation Intelligence and FO lifecycle doctrine

Sagrenti is not a market feasibility analysis service. Sagrenti provides
Consumer Anticipation Intelligence through Future Offerings. That
intelligence may answer a merchant's market-feasibility question, but
its value does not end when sufficient evidence exists to make a build
decision.

The governing FO progression is:

Discover anticipation ↓ Establish feasibility ↓ Merchant commits ↓
Cultivate demand and measure anticipation ↓ Prepare for launch ↓ Launch
/ token release

These are lifecycle milestones and value stages, not mandatory
fixed-duration phases. An FO may establish useful feasibility evidence
quickly while continuing much longer as a pre-launch anticipation
environment.

The merchant's build decision is therefore a milestone within the FO
lifecycle, not the termination of the FO. After a go decision, the FO
may continue to cultivate consumer anticipation and demand, measure how
that anticipation develops, preserve and deepen engagement
relationships, and help the merchant prepare for launch.

The governing principle is:

A Future Offering is lifecycle-driven, not duration-driven.

An FO may remain in active continuing service for as long as the
merchant continues to receive value from the pre-launch anticipation
relationship and the FO remains eligible for continuing service under
applicable platform governance. FO architecture must not impose an
arbitrary one-month, three-month, six-month, annual or other
predetermined lifetime merely for commercial convenience.

FO lifetime, Service Term, Service Period, Billing Period, analytical
window and reporting cadence are distinct concepts. None should be used
as a substitute for another.

Consumer Anticipation Intelligence may be analyzed over whatever bounded
window is useful to the merchant, including daily, weekly, monthly,
custom-range and lifetime-to-date views. Producing or scheduling such
analysis does not create a new Service Term, Service Period, Billing
Period or QAE eligibility boundary. Reporting cadence is a presentation
and analytical concern over authoritative underlying data, not a
definition of service duration.

The FO therefore serves two complementary merchant needs:

1.  decision intelligence -- including whether anticipation supports a
    build, change, defer or other merchant decision; and

2.  continuing anticipation intelligence -- including cultivating demand
    and measuring how the future market develops as the offering moves
    toward launch.

Sagrenti's objective is not to force continuation through artificial
duration commitments. Continuing service should remain commercially
valuable because the merchant can see the benefit of preserving and
developing the anticipation environment already forming around the
Future Offering.

2.  Domain Language and Presentation Language Doctrine

Sagrenti separates canonical domain terminology from human-facing
presentation terminology.

The governing principle is:

Engineering owns stable domain semantics; Administration owns
configurable human-facing language. Frontend and other presentation
layers should resolve configured terminology at runtime wherever
practical rather than requiring source-code modification, recompilation
or redeployment when Sagrenti changes its public vocabulary.

This distinction is an architectural capability requirement, not merely
an Admin Console convenience.

Stable domain concepts

Engineering requires stable identities and semantics upon which platform
behavior, persistence, APIs, lifecycle rules, billing, auditability and
integrations can depend.

Examples include:

Future Offering Anticipation Intelligence Qualified Anticipation Entry
Activation Cycle Service Term Service Period Billing Period Payment
Period Fee Type Platform Credit

These canonical concepts should not be renamed merely because Sagrenti
discovers better language for explaining them to consumers or merchants.

Configurable presentation language

Administration may configure appropriate human-facing terminology
independently of the underlying domain identity.

For example:

Stable Engineering Concept Possible Human-Facing Language

Future Offering Coming Up Anticipation Intelligence Signals / Early
Signals Activation Cycle Configured human terminology Service term
Configured merchant terminology Fee type Configured merchant-facing
label Platform Credit Configured commercial terminology

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

CANONICAL DOMAIN ──────────────────────────── Future Offering
Anticipation Intelligence Qualified Anticipation Entry Activation Cycle
Service Term Service Period Billing Period Payment Period Fee Type
Platform Credit

             ↓
     authoritative runtime

terminology configuration ↓

PRESENTATION ──────────────────────────── Coming Up Signals Early
Signals Configured plan names Configured fee names Configured commercial
language Future improved terminology

Changing:

Future Offering presentation Coming Up → Upcoming

or:

Anticipation Intelligence presentation Signals → Market Signals

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

Current UI → may resolve today's configured terminology

Issued historical record → preserves the authoritative terminology
applicable when issued

A future Administration change from Signals to Market Signals, for
example, must not silently rewrite previously issued invoices or other
records whose historical presentation must remain intact.

Ownership across the architecture

FOCA is the primary governing owner of product/domain vocabulary
separation.

Other architectures inherit the doctrine within their respective scopes:

invoice and financial presentation architecture applies it to financial
terminology and historical invoice rendering;

promotions and Platform Credit architecture applies it to promotion,
credit and merchant-facing commercial terminology;

frontend presentation applies configured terminology without redefining
canonical domain semantics.

The governing rule throughout Sagrenti is therefore:

Stable meaning belongs to the architecture. Changeable language belongs
to configuration where appropriate.

3.  Four distinct Future Offering concerns

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
Signup Intent
Enrollment Intent
Application Intent
Subscription Intent
Booking Intent
Purchase Intent
(Potential future) Draw Entry

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

4.  Watch and the anticipation relationship

Watch is platform-owned.

Unlike merchant-configurable engagement actions, Watch:

is always available;

cannot be disabled by the merchant;

represents the consumer's ongoing anticipation relationship with the FO.

A consumer may enter that relationship directly by Watching or
indirectly by performing another qualifying engagement action.

If the consumer selects Waitlist, Early Access Request, Beta,
Reservation Interest, Preorder Intent or another qualifying engagement
without already Watching, the platform establishes the Watch
relationship automatically.

Consumers with no engagement receive no ongoing intelligence for that
FO. Once the anticipation relationship exists, the consumer receives the
FO's continuing intelligence stream.

5.  Engagement is evolutionary, not prescriptive

A consumer might progress through:

Watch ↓ Waitlist ↓ Beta ↓ Reservation Interest ↓ Preorder Intent

But this is not a mandatory funnel.

Consumers may enter at different points, skip stages, add engagements
later, or stop participating.

Each engagement remains independently recorded as an event for
operational history and analytics.

Commercial billing does not count those events independently.

6.  Qualified Anticipation Entry --- QAE

The Qualified Anticipation Entry is Sagrenti's fundamental Anticipation
Intelligence usage unit.

A QAE represents:

One participant's first qualifying entry into one specific Future
Offering during one activation cycle.

Its uniqueness boundary is:

Participant × Future Offering × Activation Cycle = at most one QAE

For example:

Watch → first entry → 1 QAE Waitlist → → 0 additional QAE Beta → → 0
additional QAE Reservation Interest → → 0 additional QAE Preorder Intent
→ → 0 additional QAE

Subsequent engagement enriches Anticipation Intelligence but does not
repeatedly charge the merchant for the same participant relationship.

A participant engaging with 100 different FOs may generate 100 QAEs
because each FO is commercially independent.

7.  Activation cycles establish QAE lifetime

Every activated FO operates within an identifiable activation cycle.

The activation cycle defines the lifetime during which a participant can
become a new QAE only once.

A genuine FO relaunch establishes a new activation cycle and therefore a
new QAE eligibility boundary. Suspension, passage of time, a new Service
Period, a new Billing Period, a merchant build decision, or another
ordinary milestone must not silently manufacture a relaunch. Relaunch is
an explicit lifecycle event carrying commercial meaning.

An activation cycle may be short or long-running. Its lifetime follows
the actual FO lifecycle rather than a predetermined commercial duration
package.

Engineering must support continuing activation-cycle operation without
requiring arbitrary six-month, twelve-month or similar lifecycle
assumptions. Administration may govern safe operating policy within
Engineering boundaries, but duration policy must not redefine the
underlying FO lifecycle or manufacture new QAE eligibility.

8.  Billing periods do not reset QAE eligibility

A billing period determines when a newly created QAE is billed.

It does not make an existing participant new again.

A participant entering a five-year FO in September 2026 produces one QAE
during that activation cycle. Continued participation in 2027, 2028 or
later does not create another QAE merely because another billing period
or year has begun.

Therefore periodic Anticipation Intelligence billing measures:

Specific FO + Specific billing period + QAEs whose first qualifying
entry occurred during that period + Applicable configured QAE price ↓
Fee Calculation ↓ Invoice Item

QAE measures new anticipation relationships, not the continuing size of
an FO's participant population.

9.  Current Future Offering fees

The current FO commercial architecture contemplates four product/service
fee concepts:

Canonical Fee Concept Commercial purpose

Anticipation Intelligence Entering an FO into active Activation Fee
commercial service

Platform-Service Fee Continuing platform service for an (canonical name
to be finalized) active FO

Asset Hosting Overage Fee FO asset consumption beyond its included
allowance

Anticipation Intelligence Fee Anticipation Intelligence generated
through QAEs

These are FO-level charges, not merchant-account charges.

The former Subscription Fee terminology must not be treated as settled
merely because the earlier architecture used a subscription abstraction.
The canonical name of the continuing-service fee should be finalized
separately without reintroducing subscription semantics by accident.

The canonical fee identity and semantics must remain stable
independently of the merchant-facing description.

Administration must be able to configure appropriate merchant-facing fee
names and descriptions without changing the underlying fee type or
requiring Commerce Architecture to be rewritten.

Invoice architecture determines the additional snapshot and
historical-integrity requirements that apply once such terminology
becomes part of an authoritative financial record.

10. Continuing FO service is not a subscription abstraction

An FO is not a conventional subscription product. Activation establishes
an FO-specific commercial/service relationship with Sagrenti. That
relationship may continue while the FO remains in active pre-launch
service and may conclude through the applicable lifecycle and commercial
process.

FOCA therefore separates concepts previously collapsed under
subscription:

Concept Meaning

FO Lifecycle The operational life of the FO and its progression from
creation through active anticipation, merchant decision, demand
cultivation, launch preparation and eventual launch/token release,
conclusion, archive or explicit relaunch

Service Term The authoritative commercial relationship under which
Sagrenti provides continuing service to a specific FO; open-ended by
default where permitted, or fixed-term when explicitly agreed

Service Period An individual bounded performance window within
continuing service during which Sagrenti renders service, measures
applicable activity and provides utility

Billing Period The accounting window or cadence according to which
applicable commercial obligations are calculated and billed

Payment Period The permitted period or schedule, where such an
arrangement exists, according to which the merchant satisfies the
resulting financial obligation

Analytical Window A merchant-selected or system-selected range over
which Consumer Anticipation Intelligence is analyzed or summarized

Reporting Cadence How often an analytical summary or report is produced
or surfaced

These concepts are related but must not be conflated.

The governing relationship is:

Future Offering lifecycle ↓ Continuing Service Term ↓ Successive Service
Periods created as service is performed ↓ Applicable Billing Periods ↓
Settlement under applicable payment terms

while analytical windows and reporting cadence operate across the
authoritative intelligence data without redefining any of those
commercial periods.

A Service Term answers:

Under what continuing commercial arrangement is Sagrenti providing
service to this Future Offering?

A Service Period answers:

What bounded performance window is Sagrenti presently serving?

A Billing Period answers:

Over what accounting window or cadence is the applicable commercial
obligation calculated and billed?

An Analytical Window answers:

Over what range of authoritative anticipation data does the merchant
want intelligence calculated or summarized?

A Reporting Cadence answers:

How often should that intelligence be produced or surfaced?

The prior merchant_program_subscriptions abstraction must therefore not
govern FO continuing service. Related data structures should model their
actual domain semantics rather than preserve subscription terminology.

11. Service Terms

A Service Term governs the authoritative continuing-service relationship
for a particular Future Offering. It does not define the inherent
lifetime of the FO.

Open-ended continuing service is the normal architectural model. Once
activated into continuing service, an eligible FO may continue receiving
Sagrenti service until that relationship is prospectively concluded
through an explicit lifecycle or commercial action.

A fixed Service Term remains a supported commercial arrangement where
the merchant and Sagrenti explicitly agree to one. Fixed-term capability
is therefore an option within the architecture, not the default
definition of a Future Offering.

The governing principle is:

FO lifecycle ≠ fixed Service Term duration.

A merchant may reach a feasibility or build decision after days or weeks
and still continue the same FO because cultivating demand and measuring
anticipation remain valuable. Conversely, a merchant may conclude
continuing service when the FO no longer requires it, subject to
applicable commercial obligations and lifecycle rules.

Engineering must therefore support both:

Open-ended Service Term → continues until explicitly concluded

and

Fixed Service Term → continues until its authoritative end unless
prospectively amended or concluded under applicable rules

Fixed-term Engineering safety boundary

Where a fixed Service Term is used, Engineering retains the absolute
supported duration envelope:

Minimum fixed duration: \> 0 months Maximum fixed duration: 1188 months
/ 99 years

The 99-year maximum is an Engineering safety invariant for
fixed-duration arithmetic and bounded platform operation. It is not a
requirement that an open-ended FO predeclare a duration or an implied
maximum FO lifetime.

Administration may govern which Service Term arrangements are presently
offered, including whether fixed terms are available and any practical
fixed-term minimum or maximum within Engineering's supported envelope.
Administration must not impose duration merely to redefine what
constitutes a Future Offering.

Configuration determines what may be agreed; it must never redefine what
was agreed.

Once a Service Term or amendment becomes authoritative, its material
commercial facts must be preserved historically. Open-ended does not
mean undefined: commencement, status, performed Service Periods,
conclusion and material amendments remain explicit and auditable facts.

12. Service Periods, Billing Periods, Payment Periods, analytical
    windows and payment economics

Service Period

Service Period cadence is an Engineering invariant. A Service Period is
a calendar-month performance window during continuing FO service,
generated according to STCD calendar semantics from the authoritative
service anchor. Service Periods are created as needed rather than as a
speculative schedule extending to an assumed FO end date.

The first or final Service Period may be shorter where necessary to
represent the authoritative commencement or conclusion of continuing
service. Administration may not alter Service Period cadence or redefine
the calendar meaning of a Service Period.

A Service Period is not another name for the Service Term and does not
determine how long the FO may remain active.

Open-ended continuing service therefore operates as:

Service Term begins ↓ Service Period 1 ↓ Service Period 2 ↓ Service
Period 3 ↓ ... created Just-in-Time while service continues ↓ Final
Service Period when continuing service concludes

Service Periods provide bounded operational windows for
continuing-service performance, period-specific commercial attribution,
usage measurement, service-state tracking, billing inputs where
applicable, historical reconstruction, operational scheduling and
auditable period-by-period service provenance.

Beginning a new Service Period does not create a new Service Term,
extend the FO by implication, reset QAE eligibility, or create a new
activation cycle. Once a Service Period has begun, its authoritative
boundaries are historical facts and must not be retroactively redefined
by subsequent configuration or policy changes.

Billing Period

A Billing Period determines the applicable accounting window or cadence
according to which a commercial obligation is calculated and billed.

A Billing Period is not the FO lifecycle, Service Term, Service Period,
analytical window, reporting cadence or payment arrangement.

The architecture may align a Billing Period with a Service Period where
commercial policy requires it, but alignment in a particular
configuration must not collapse the concepts into one domain identity.

Administration may configure permitted Billing Period rules and
associations within Engineering-supported boundaries. Engineering
implements the capability rather than using Billing Periods to impose an
FO duration.

Analytical Window and Reporting Cadence

Consumer Anticipation Intelligence may be calculated or summarized over
daily, weekly, monthly, custom or lifetime-to-date analytical windows.
Reports or dashboard summaries may likewise be produced daily, weekly,
monthly, on demand or according to another supported cadence.

These analytical operations reuse authoritative FO intelligence. They do
not create a new commercial service period merely because another report
is produced. Engineering should therefore permit efficient repeated
analysis and summary generation without coupling reporting frequency to
FO duration, QAE eligibility or Billing Period creation.

Payment Period

Where payment scheduling is supported, Payment Period remains distinct
from Service Term, Service Period and Billing Period. It governs the
permitted schedule or arrangement under which an established financial
obligation is satisfied.

Administration may configure permitted payment arrangements within
Engineering-supported financial controls.

Payment discounts are financial/accounting effects

A payment discount is not a production/service charge reduction.

The underlying invoice must preserve the truthful service obligation. A
discount arising because the merchant selected or satisfied a particular
payment arrangement is a financial/accounting effect associated with
payment economics and may be recognized after the underlying
production/service charge or invoice.

Accordingly, payment discounts must not be forced onto the invoice as
reductions of the underlying product/service charge merely because they
affect the merchant's ultimate economics.

The financial/accounting architecture should preserve their recognition,
provenance and auditability separately from the production/service fee.

Configuration determines what may be agreed; it never redefines what was
agreed

Administration may change future permitted Service Term arrangements,
Billing Period associations, payment arrangements, labels, pricing and
commercial policy. Service Period cadence and calendar semantics remain
Engineering invariants.

Once an FO has entered an authoritative commercial arrangement, material
selected facts must be preserved. A later Administration configuration
change must not retroactively rewrite an existing Service Term or
Service Period already performed.

Engineering must therefore preserve or snapshot the authoritative
commercial facts applicable when an arrangement becomes binding,
including the stable identities and material terms needed to reconstruct
its historical meaning.

14. FO Milestones, Merchant Responsibilities and Notifications

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

Beta access / Beta token issuance Waitlist token issuance Early Access
transition Preorder token issuance Reservation token issuance Release
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

appropriate prompts when milestone information appears stale or requires
confirmation; and

historical recording of material milestone changes.

The purpose is to help merchants remain operationally prepared, not to
punish them for reporting legitimate changes.

Notifications ride the established milestone schedule

Once a merchant has established a milestone, Sagrenti has a clear basis
for providing timely reminders.

Merchant establishes milestone ↓ Sagrenti tracks milestone ↓ Configured
advance-reminder threshold reached ↓ Dashboard / in-app notification ↓
Merchant takes required action ↓ Completion or updated milestone
recorded

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

The merchant issues the token. Sagrenti does not issue the merchant's
commercial entitlement.

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

Engagement type does not inherently determine fulfillment priority.

The merchant's applicable release, allocation or fulfillment policy
determines the relationship among eligible populations, subject to safe
Engineering boundaries and commitments already made to consumers.

Milestones may affect continuing-service decisions

A legitimate change in Release Date or another material FO milestone may
affect how long the merchant wishes continuing service to remain active,
but the milestone does not mechanically rewrite the Service Term.

For an open-ended Service Term, the FO simply continues while continuing
service remains valuable and eligible, unless the merchant or platform
prospectively concludes it through the applicable process. A later
release date therefore does not require manufacturing a term extension.

For an explicitly fixed Service Term, a material milestone change may
justify a prospective amendment. Such an amendment creates a new
historical commercial fact; it does not rewrite the original agreement.

Sagrenti should encourage accurate, timely FO updates rather than
economically punish merchants for reporting legitimate schedule changes.
Service already performed remains an authoritative fact and applicable
charges are not retroactively erased merely because a future milestone
changes.

Any amendment that changes a fixed Service Term, applicable commercial
terms, pricing or other material commercial facts must be resolved
explicitly and preserved as part of the amended arrangement rather than
silently inferred from editing a milestone.

15. Future Offering eligibility and governance boundaries

Service duration must not become the definition of a Future Offering

A minimum Service Term cannot protect Sagrenti from becoming an ordinary
ecommerce marketplace, and a predetermined duration is not required to
make an offering genuinely future-facing.

Requiring a merchant to purchase three months of service would not
transform existing ready-to-ship inventory into a Future Offering.
Conversely, a legitimate FO does not become less genuine because useful
feasibility intelligence emerges in two weeks.

Service duration and Future Offering eligibility are therefore separate
concerns.

Future Offering eligibility requires genuine future commerce

A Future Offering represents a genuine future market offering for which
meaningful pre-market anticipation exists before ordinary commercial
availability.

The governing question is not merely:

Will this product be sold sometime after today?

The stronger question is:

Is there a genuine forthcoming product or service proposition for which
anticipation can meaningfully exist, be cultivated and be measured
before ordinary market availability?

This distinction protects Sagrenti's Future Offering model from
collapsing into conventional present-commerce retail.

Sagrenti is not an ordinary store or resale marketplace

Sagrenti's Future Offering capability supports merchants bringing
forthcoming products, services, experiences, projects and other
offerings toward market. It is not another storefront for ordinary
resale of already-available commodities or inventory.

Repackaging or reselling an existing product, with no genuine
forthcoming product/service proposition, should therefore not qualify
merely because the merchant assigns it a future date. Where inventory is
already commercially ready for immediate ordinary sale and shipment,
conventional ecommerce infrastructure is the appropriate model.

A Future Offering does not have to be physically unfinished

Future Offering eligibility must not be defined as: The product must
still be unfinished.

A finished prototype may be an especially strong Future Offering use
case. A merchant may have completed a fully functioning prototype while
still needing to validate market anticipation, understand likely demand,
determine manufacturing scale, prepare production, secure distribution,
cultivate demand and establish launch readiness. Sagrenti can provide
meaningful intelligence throughout that interval.

The distinction is therefore between future market availability and
present ordinary availability, not simply between unfinished and
finished products.

Completed digital and creative works require the same distinction

Physical incompleteness cannot be the eligibility test because many
legitimate Future Offerings may already exist in completed form before
market release. Examples include an unpublished completed book, a
completed album awaiting release, completed software awaiting launch,
and other digital or creative works with a genuine future market event.

Completion does not automatically disqualify an offering. Conversely, an
already ordinarily available digital download should not become a Future
Offering merely because a merchant chooses to list or promote it through
Sagrenti. The relevant distinction remains whether a genuine future
market availability or launch event exists.

Three separate governance boundaries must be preserved

Future Offering architecture must distinguish:

Engineering safety boundary

Determines what the platform can safely and correctly support, including
safe fixed-term arithmetic, bounded resource behavior, lifecycle
integrity and Just-in-Time period creation.

Future Offering eligibility boundary

Determines whether something genuinely belongs within future commerce
rather than present-commerce retail. This includes preserving the
requirement for meaningful pre-market anticipation before ordinary
commercial availability.

Administration operating policy

Determines how Sagrenti presently operates within Engineering's
capability and the FO domain boundary. Examples may include
eligibility/review policies, available Service Term arrangements,
Billing Period policy, payment policy, reporting features, commercial
thresholds and other configurable operating requirements.

Administration may govern these policies dynamically, but may not
redefine Engineering safety invariants or transform ordinary
present-commerce inventory into a Future Offering merely through
configuration.

The resulting architecture preserves this separation:

Engineering determines what Sagrenti can safely support. Administration
determines how Sagrenti presently operates within those boundaries.
Future Offering Architecture determines what genuinely constitutes
future commerce.

Neither arbitrary duration packages nor arbitrary future dates
substitute for those principles.

16. Activation creates the commercial event

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

Payment therefore must not be treated as the Activation billable event.

17. Activation requires commercial authorization, not universal
    prepayment

The existence of an Activation Fee does not imply that every merchant
must pay it before activation.

Engineering must support at least two legitimate paths.

Immediate settlement

Activation Fee calculated ↓ Activation obligation/invoice ↓ Payment
satisfied ↓ FO activated

Authorized deferred settlement

Activation Fee calculated ↓ Deferred/invoiced settlement authorized ↓ FO
activated ↓ Activation Fee included on applicable invoice ↓ Settlement
under configured terms

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

18. Invoice items represent Sagrenti products and services

An invoice item represents:

One independently calculated product/service charge represented by one
line on an invoice.

The commercial progression is:

Billable Event ↓ Fee Calculation ↓ Invoice Item ↓ Invoice

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

19. Invoice-item cardinality

The agreed relationship is:

1 Invoice ↓ many Invoice Items

1 Invoice Item ↓ exactly 1 Fee Calculation

1 Fee Calculation ↓ at most 1 Invoice Item

Accordingly, fee_calculation_id belongs directly on
merchant_invoice_items and must be unique there. A separate
invoice-item/fee-calculation association table is unnecessary.

For periodic Anticipation Intelligence usage:

FO UM120 --- August

6,500 new QAE × configured CPQAE = calculated Anticipation Intelligence
Fee ↓ one Fee Calculation ↓ one Invoice Item

20. Adjustments do not redefine what Sagrenti sold

Invoice items preserve the truthful underlying product/service charge.

Platform Credits, rebates, discounts, taxes, surcharges and similar
monetary effects must not falsify:

quantity;

unit amount;

gross calculated charge.

For example:

Activation Fee

Quantity 1 Unit amount \$100 Gross charge \$100 Platform Credit -\$50
Net obligation \$50

The \$100 Activation Fee remains commercially true. The Platform Credit
changes the resulting obligation rather than rewriting the underlying
fee calculation.

21. Commercial effects remain attributable to their cause

The governing provenance principle is:

Adjustments should be recorded and attributed at the lowest commercial
grain that gives rise to them.

Taxes, surcharges, Platform Credits, rebates and discounts therefore
remain attributable to the specific FO and commercial charge they
affect.

This permanently answers:

Which FO? Which charge? What monetary effect? How much? Why? Who
authorized it?

Such effects should not become artificial negative invoice items merely
for presentation convenience.

The invoice aggregates established obligations; it does not redefine
their provenance.

22. Taxes reinforce FO-level commercial provenance

Different FOs appearing on the same invoice may carry different
jurisdictional and tax consequences.

Therefore invoice-wide tax or adjustment values cannot serve as the
authoritative source of commercial provenance.

Taxes and other charge-related monetary effects must be calculated and
preserved against the FO-specific charge to which they apply.
Invoice-level totals may aggregate them for presentation.

The broader hierarchy is:

Merchant ↓ Future Offering │ ├── Commercial Charges │ ├── Activation Fee
│ ├── Continuing-Service Fee │ ├── Asset Hosting Overage Fee │ └──
Anticipation Intelligence Fee │ └── Charge-related Monetary Effects ├──
Taxes ├── Surcharges ├── Platform Credits ├── Rebates └── Discounts

23. Platform Credits are commercial policy, not product charges

Platform Credits are commercial reductions rather than invoice products.

Current promotional policy hypotheses include:

\$300 award per qualifying merchant;

eligibility currently focused on Activation and continuing-service fees;

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

24. Merchant teams are not the commercial unit

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

25. Engineering and Administration boundary

Engineering implements the complete commercial capability and safe
operating boundaries.

Administration governs commercial and operational behavior within those
boundaries.

This includes a deliberate separation of responsibility:

Engineering owns stable domain semantics and identities. Administration
owns legitimate configurable commercial policy and human-facing
terminology.

Administration should be capable of configuring matters such as:

consumer-facing and merchant-facing terminology where designated
configurable;

available Service Term arrangements and fixed-term operating policy;

merchant-facing fee names and descriptions;

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

Service Period cadence and calendar-boundary semantics. Service Periods
are calendar-month performance windows generated Just-in-Time according
to STCD semantics while continuing service remains active; a first or
final period may be shorter where required by authoritative service
commencement or conclusion. Administration may not alter Service Period
cadence or redefine the calendar meaning of a Service Period;

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

Administration may change: "What do we call this?"

Administration may configure: "How do we commercially operate this?"

Administration may not redefine: "What is this?"

Administration may not override: "What must remain true for the platform
to be correct and safe?"

This is the governing Engineering/Admin boundary throughout FOCA.

26. Architectural qualification

FOCA does not declare that Sagrenti can never introduce a merchant-level
commercial product.

Its governing doctrine is narrower:

Under the current commercial model, merchant-account existence is
non-billable and Sagrenti's merchant commercial obligations originate at
the Future Offering level.

Every current fee and every charge-related tax, surcharge, credit,
rebate or discount must remain attributable to the FO and commercial
charge that caused it.

This protects the present architecture without unnecessarily preventing
future commercial models.

FOCA v1.02 --- Governing Model

The architecture can be reduced to the following:

Join Sagrenti as a merchant for free.

Every Future Offering is its own commercial project.

Sagrenti provides Consumer Anticipation Intelligence; market feasibility
is one question that intelligence can answer, not the boundary of the
service.

The FO value progression is:

Discover anticipation ↓ Establish feasibility ↓ Merchant commits ↓
Cultivate demand and measure anticipation ↓ Prepare for launch ↓ Launch
/ token release

The build decision is a milestone, not an automatic end to the FO.

Activate an FO → Activation Fee.

Keep it in continuing service → applicable configured continuing-service
fee.

Continuing service is open-ended by default where permitted. A fixed
Service Term may be explicitly agreed when appropriate, but fixed
duration does not define the FO lifecycle.

Successive Service Periods are created Just-in-Time while continuing
service remains active. Billing Periods govern applicable accounting
windows/cadence. Payment arrangements govern settlement. Analytical
windows and reporting cadence remain independent of all of them.

A Service Period is a calendar-month performance window generated
according to STCD semantics, with a first or final period permitted to
be shorter where necessary to represent authoritative service
commencement or conclusion. Service Period cadence and calendar
semantics are Engineering invariants, not Administration policy.

Daily, weekly, monthly, custom-range and lifetime-to-date Consumer
Anticipation Intelligence summaries may be produced from authoritative
FO data without manufacturing additional Service Periods, Billing
Periods, QAEs or activation cycles.

Exceed included asset resources → Asset Hosting Overage Fee.

Generate new measurable anticipation relationships → Anticipation
Intelligence Fee based on QAEs.

One participant × one FO × one activation cycle → at most one QAE.

Billing periods aggregate new QAEs; they do not reset QAE eligibility.

Beginning a new Service Period or Billing Period does not create a new
Service Term, activation cycle or QAE eligibility boundary.

Payment discounts are financial/accounting effects, not reductions of
the truthful production/service charge.

Configuration determines what may be agreed; it must never redefine what
was agreed.

Every charge and every monetary effect upon that charge remains
attributable to the FO that caused it.

The invoice aggregates established commercial obligations; it does not
redefine them.

Canonical domain concepts remain stable while appropriate human-facing
terminology remains administratively configurable. Presentation layers
resolve configured terminology at runtime wherever practical rather than
turning vocabulary changes into recompilation and redeployment.

Engineering builds and protects the commercial machinery, stable
identities, historical facts and safe boundaries. Administration governs
commercial policy and human-facing language within those boundaries.

The resulting commercial chain is:

FUTURE OFFERING ↓ Activation Cycle ↓ Consumer enters anticipation ↓
Qualified Anticipation Entry ↓ Billing-period aggregation ↓ Fee
Calculation ↓ Invoice Item ↓ Invoice ↓ Settlement

while continuing FO service operates alongside it:

ACTIVE FUTURE OFFERING ↓ Continuing Service Term (open-ended by default;
fixed where explicitly agreed) ↓ Successive Just-in-Time Service Periods
↓ Applicable Billing Periods ↓ Applicable settlement terms ↓ Configured
Continuing-Service Economics

while Consumer Anticipation Intelligence can be viewed independently:

AUTHORITATIVE FO INTELLIGENCE ↓ Daily / Weekly / Monthly / Custom /
Lifetime Analytical Window ↓ Dashboard / Report / Decision Support ↓ No
change to FO lifecycle or commercial-period identity

while merchant operations are supported by the FO milestone schedule:

FO ACTIVATION ↓ Merchant establishes / confirms milestones ↓ Sagrenti
tracks approaching dates ↓ Dashboard highlighting / in-app reminders ↓
Merchant decision / action / token issuance / milestone update ↓
Completion and history preserved

and the presentation architecture sits above the stable domain:

STABLE DOMAIN SEMANTICS ↓ Authorized Administration Configuration ↓
Runtime Terminology Resolution ↓ Consumer / Merchant Presentation

Together these establish the governing answer to FOCA's central
question:

Sagrenti sells Consumer Anticipation Intelligence through independently
commercial Future Offerings. Each FO creates its own activation and
continuing-service relationship. The FO is lifecycle-driven rather than
duration-driven: useful feasibility evidence may emerge quickly, while
the same FO may continue cultivating demand and measuring anticipation
until launch or another legitimate conclusion. Continuing service is
open-ended by default where permitted, with fixed Service Terms
available when explicitly agreed. Sagrenti performs continuing service
through successive Just-in-Time Service Periods. Billing Periods govern
applicable accounting windows or cadence, while payment arrangements
govern settlement. Analytical windows and reporting cadence remain
independent and may operate over the authoritative intelligence without
redefining service or billing periods. New consumer anticipation
relationships are measured through QAEs, configured fees monetize the
relationship, and every resulting commercial obligation remains
attributable to the Future Offering that created it. Engineering
preserves stable meaning, identity, historical fact and safe operating
boundaries while Administration governs appropriate commercial policy
and human-facing language. Configuration determines what may be agreed;
it never retroactively redefines what was agreed.

Waitlist, Beta, Preorder, Reservation, etc. are independently enabled
engagement options. A merchant may choose any applicable
combination---including Waitlist without Preorder or Preorder without
Waitlist. Where multiple engagement types coexist, their coexistence
does not establish an inherent fulfillment priority.
