## QAE, Activation Cycle, Subscription & Invoice-Item Architecture — v1

### 1. What a QAE actually means

A **Qualified Anticipation Entry (QAE)** represents:

> **One participant's first qualifying entry into one specific Future Offering during one activation cycle.**

The uniqueness boundary is therefore:


`Participant × Future Offering × Activation Cycle = at most one QAE`


Every FO is completely independent.

A participant entering 100 different FOs can therefore generate 100 QAEs—one for each FO.

Within the same activation cycle of one FO:

`Watch                       → first entry → 1 QAE`
`Waitlist                    → no additional QAE`
`Beta                        → no additional QAE`
`Reservation Interest        → no additional QAE`
`Preorder Intent             → no additional QAE`


Within one FO activation cycle, subsequent engagements enrich Anticipation Intelligence but do not create additional QAEs.

---

### 2. Billing periods do not reset QAE eligibility

This distinction is critical.

A billing period determines **when a newly created QAE is billed**. It does not make an existing participant new again.

For a five-year FO:

Participant enters: September 2026
→ 1 QAE attributed to September 2026

2027 → still participating → 0 new QAE
2028 → still participating → 0 new QAE
2029 → still participating → 0 new QAE
...

A participant could remain associated with that FO for decades without producing another QAE during that activation cycle.

---

### 3. Activation cycles define the QAE lifetime

Every activated FO operates within an identifiable **activation cycle**.

A genuine FO relaunch creates a **new activation cycle** and therefore a **new QAE eligibility boundary**.

Consequently:

FO UM120
`Cycle 1 → Participant A → 1 QAE`

FO UM120 formally relaunched

`Cycle 2 → Participant A may qualify → 1 new QAE`


A cycle merely reaching its end date (expiration) must **not silently manufacture a relaunch**.

Relaunch is an explicit lifecycle event with commercial meaning.

---

### 4. Activation-cycle duration

Every activated FO needs a defined activation cycle.

The Activation Wizard should support merchant-defined cycle duration/end date, including short and long-running projects:

3 months
6 months
12 months
2 years
3 years
5 years
custom

Engineering provides the capability. Administration can govern defaults, available choices, maximums, or similar operational policy within Engineering's safe boundaries.

There should be no arbitrary engineering assumption that an FO must end after six or twelve months.


### 5. Long-running FOs do not justify redefining QAE

A long-running FO exposes a legitimate commercial question: Sagrenti continues providing intelligence concerning an established participant population even though those participants aren't repeatedly generating QAEs.

If an FO remains active for years, existing participants are not charged as new QAEs every year.

Only genuinely new participants entering that activation cycle generate new QAEs.

For example:

For a 5-year FO

Year 1: 20,000 new participants → 20,000 QAE
Year 2:  7,000 new participants →  7,000 QAE
Year 3:  4,000 new participants →  4,000 QAE

The original participants remain valuable intelligence relationships, but they are not new QAEs.

---

### 6. Subscription monetizes continuing service

The existing subscription architecture provides a clean mechanism for monetizing continuing platform value without corrupting QAE.

Conceptually:

Future Offering
      │
      ├── Anticipation Intelligence Activation Fee
      │
      ├── Anticipation Intelligence Fee (QAE usage)
      │
      └── Subscription Fee, where applicable


Administration could configure a no-cost subscription for shorter projects and paid subscriptions for longer or multi-year projects.

Short-duration FO
→ Admin-named subscription
→ $0 subscription fee

Long-duration / multi-year FO
→ Admin-named subscription
→ recurring configured fee

Our discussion used **six months** as a possible commercial dividing point, but that is **not an Engineering invariant**.

Admin owns the threshold, plan names, prices, billing intervals, eligibility and whether such plans are enabled.

Engineering supplies the capability.

### 7. Activation Fee does not necessarily mean prepayment

This is the important addition.

The **Anticipation Intelligence Activation Fee** arises from activation of the FO, but Engineering must **not equate the existence of that fee with mandatory payment before activation**.

The platform must support at least:

# PRE-ACTIVATION SETTLEMENT

`Activation Fee calculated`
        ↓
`Invoice / payment obligation`
        ↓
`Payment satisfied`
        ↓
`FO activated`

and:

# AUTHORIZED INVOICED TERMS

`Activation Fee calculated`
        ↓
`Authorized deferred/invoiced treatment`
        ↓
`FO activated`
        ↓
`Activation Fee included on periodic invoice`
        ↓
`Settlement according to configured terms`

This accommodates governments, large organizations, universities, enterprises and other merchants operating under procurement or negotiated billing arrangements.

Who qualifies for such terms is **Administration policy.**


# 8. Activation therefore requires commercial authorization, not universally prior payment

We should no longer encode the invariant:

Activation Fee must always be paid before an FO can activate.

Instead, activation requires that the Activation Fee has reached an authorized commercial disposition.

That might be:

`paid`

Or:

`authorized for invoiced/deferred settlement`

Those are not the same financial fact.

In particular, we must not falsely record activation_payment_satisfied when no payment occurred.

This means the existing FO activation event/lifecycle vocabulary deserves review when we reach that slice.


### 9. Invoice-item definition

An **invoice item is one independently calculated product/service charge represented by one line on an invoice.**

Your store analogy is exactly right:

`Bananas → Fee Calculation A → Invoice Item A`
`Oranges → Fee Calculation B → Invoice Item B`

For Sagrenti:

Anticipation Intelligence Fee — Project UM120
`6,500 QAE × $0.25 = $1,625`
        ↓
Fee Calculation A
        ↓
Invoice Item A

and:

Anticipation Intelligence Fee — Project AIT17
`25,000 QAE × $0.25 = $6,250`
        ↓
Fee Calculation B
        ↓
Invoice Item B

Both can appear on the same periodic invoice.


# 10. Activation Fee follows exactly the same invoice-item principle

An Activation Fee also has its own fee calculation and invoice item:

Activation Fee — Project GOV27
1 × configured fee
        ↓
Fee Calculation
        ↓
Invoice Item

What changes according to commercial terms is **which invoice receives that item and when settlement is required**.

Therefore an institutional periodic invoice could legitimately contain:

Description                                      Qty/QAE    Unit     Total

Activation Fee — GOV27                              1       $50       $50
Anticipation Intelligence Fee — GOV27          12,500      $.25    $3,125
Anticipation Intelligence Fee — AIT17           6,000      $.25    $1,500
                                                                  -------
                                                                   $4,675

Nothing special needs to happen to merchant_invoice_items merely because the Activation Fee was deferred.


### 11. Invoice-item cardinality

Therefore the invoice-item cardinality is locked

We agree on:

1 Invoice
    ↓
many Invoice Items

1 Invoice Item
    ↓
exactly 1 Fee Calculation

1 Fee Calculation
    ↓
at most 1 Invoice Item

Therefore SE's fundamental structure:

```sql
fee_calculation_id UUID NOT NULL
    REFERENCES merchant_fee_calculations(id) ON DELETE RESTRICT,

CONSTRAINT uq_merchant_invoice_items_fee_calculation
    UNIQUE (fee_calculation_id)
```

is conceptually correct.

**My proposed `merchant_invoice_item_fee_calculations` association table is withdrawn.**

We do not need it.

---

### 12. Periodic QAE calculation grain

For periodic Anticipation Intelligence usage billing, the calculation grain should conceptually be:


Specific FO
+
specific billing period
+
QAEs whose first qualifying entry
occurred during that period
+
applicable CPQAE
        ↓
ONE Fee Calculation
        ↓
ONE Invoice Item


Example:

UM120 — August

6,500 new QAE
× $0.25 CPQAE
= $1,625

→ one Fee Calculation
→ one Invoice Item

We have identified an upstream review requirement: merchant_fee_calculations and merchant_billable_events must ultimately support this aggregation correctly.

That should be fixed in their own architecture rather than distorting merchant_invoice_items.

---

### 13. Credits and adjustments remain separate

Invoice items describe what Sagrenti sold.

They do not become containers for:

* Platform Credits
* rebates
* discounts
* payments
* settlement adjustments


Therefore:

Invoice Item = product/service charge

Anticipation Intelligence Fee
6,500 × $0.25 = $1,625


remains the truthful invoice-item calculation even when some separate commercial mechanism reduces what the merchant ultimately owes.



## Final architecture

We have therefore arrived at a fairly clean hierarchy:

FUTURE OFFERING
       ↓
Activation Cycle
       ↓
Participant's first qualifying entry
       ↓
QAE
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


CONTINUING LONG-TERM SERVICE
       ↓
Subscription
       ↓
Configured Subscription Fee


ACTIVATION-FEE SETTLEMENT

Engineering capability
       ├── pre-activation settlement
       └── authorized invoiced/deferred settlement

Administration
       ↓
determines applicable terms


The three concepts no longer need to fight each other:

> **QAE measures a new anticipation relationship.**

> **The activation cycle determines the lifetime within which that relationship is considered new only once.**

> **Subscription can monetize continuing platform service avoiding the temptation to repeatedly charge for the same QAE.**

# Submission of an FO for activation creates the Activation billable event.

That event answers: **Why is Sagrenti entitled to calculate an Activation Fee**?

The fee calculation then answers: **How much is the Activation Fee under the merchant's applicable commercial terms**?

Invoicing answers: **When and on which invoice is that calculated charge presented as an obligation**?

Payment answers: **How is that invoice obligation ultimately satisfied**?

This also handles our government/enterprise case correctly. Both merchants have the **same underlying activation commercial event and fee calculation**. What differs is their authorized invoicing/settlement terms.

So payment must definitely not be the Activation billable event.


If subscriptions are enabled, the merchant chooses from Admin-configured subscription options, including the permitted billing cadence. Monthly cadence produces recurring subscription periods; annual cadence produces a single upfront annual period, potentially at an Admin-configured discounted annual price.