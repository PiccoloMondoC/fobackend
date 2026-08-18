Yes. I think we should consolidate the earlier summaries now because the last discussion corrected several assumptions. I would treat the following as the **current CE working agreement**, superseding the earlier interim summaries where they conflict.

# Merchant Invoice Composition Architecture — Consolidated Agreement

## 1. One invoice belongs to one Future Offering

A Future Offering is an independent merchant project. Different FOs may have different managers, budgets, markets, approval processes, and accounting requirements.

Therefore:

> **One invoice = one merchant + one Future Offering + one currency.**

Charges from different FOs must not be combined on the same invoice.

```text
Merchant
   │
   ├── FO A → Invoice(s)
   ├── FO B → Invoice(s)
   └── FO C → Invoice(s)
```

This makes `future_offering_id` part of the invoice's genuine commercial identity, not merely reporting metadata.

---

## 2. Invoice items represent Sagrenti products/services

Invoice lines represent the products/services Sagrenti charged for.

Examples under today's terminology might be:

```text
Anticipation Intelligence Activation Fee
Subscription Fee
Asset Hosting Overage Fee
Anticipation Intelligence Fee
```

But **those names are not permanent engineering vocabulary**.

Administration must be able to configure appropriate merchant-facing names as Sagrenti's language evolves.

For example:

```text
Engineering identity       Merchant-facing terminology
────────────────────       ───────────────────────────
canonical fee type    →    configured invoice label
```

Changing *Anticipation Intelligence* to *Signals*, for example, must not require rewriting the Commerce Architecture.

Once an invoice is issued, however, its displayed descriptions should be snapshotted so later configuration changes cannot rewrite historical invoices.

---

## 3. Engineering semantics and presentation language are separate

This is now an important application of BEG §18.6A/§18.6B.

Engineering owns stable concepts, monetary integrity, attribution, lifecycle rules, currency integrity and other invariants.

Administration owns legitimate commercial configuration and merchant-facing terminology.

Therefore labels such as:

```text
Items Subtotal
Subtotal
Pre-tax Total
Total Before Tax
Taxes & Surcharges
Estimated Taxes
Invoice Total
```

must not unnecessarily become hard-coded commercial policy.

The internal concept can remain stable while its presentation changes.

---

## 4. Line pricing can contain directly attributable effects

A line represents the resulting charge for a particular product/service.

Conceptually:

```text
quantity × unit cost
        ↓
directly attributable rebate / discount
        ↓
line charge
```

Thus a rebate or pricing discount that directly determines the price of that service participates in establishing the line charge rather than becoming an unexplained invoice-wide adjustment.

---

## 5. Platform Credits remain attributable to eligible fees

Platform Credits are different.

Under MPPFCA v1.0, a Platform Credit may satisfy up to the permitted percentage—currently 50%—of an **eligible fee**.

Example:

```text
Activation Fee                    $100
Platform Credit                    -50
                                  ----
Net                                 50

Subscription Fee                   200
Platform Credit                   -100
                                  ----
Net                                100

Asset Hosting Overage               50
Anticipation Intelligence Fee      500
                                  ----
Pre-tax Total                     $700
```

The underlying records preserve:

```text
Activation Fee
    └── Platform Credit Application $50

Subscription Fee
    └── Platform Credit Application $100
```

This is why Platform Credit eligibility and caps remain charge-specific.

---

## 6. Charge-associated Platform Credit presentation is preferred

We considered presenting:

```text
Subtotal                           $850
Platform Credits                  -150
                                  ----
Pre-tax Total                      700
```

but prefer the more informative charge-associated presentation:

```text
Activation Fee                    $100
Platform Credit                    -50
                                  ----
                                    50
```

The merchant immediately sees what received the credit and what remains.

Therefore we should **not introduce a generic invoice-header Platform Credit field simply to reproduce a summary presentation**.

---

## 7. But not every monetary effect belongs to an individual line

This was another important refinement.

Some costs can legitimately relate to the invoice/project as a whole rather than one particular line.

The physical-goods analogy illustrates it:

```text
Apples
Bananas
Oranges
                              ----
Items Subtotal

Shipping & Handling
                              ----
Total Before Tax

Taxes & Surcharges
                              ----
Invoice Total
```

Shipping & Handling belongs to the transaction containing all three items; forcing it artificially onto one product would be wrong.

Sagrenti may eventually have analogous shared charges or reductions.

Therefore the architecture must support **both**:

```text
Line-attributable monetary effects
```

and:

```text
Invoice-level monetary effects
```

without forcing one category into the other.

---

## 8. The invoice composition therefore has layers

Our more general model is now:

```text
PRODUCT / SERVICE LINES

Line 1
  ± directly attributable pricing effects
  ± attributable Platform Credit
  = net contribution

Line 2
  ± directly attributable effects
  = net contribution

Line 3 ...
        │
        ▼
    ITEMS SUBTOTAL
        │
        │ ± legitimate shared
        │   invoice-level effects
        ▼
    PRE-TAX TOTAL
        │
        │ + Taxes & Surcharges
        ▼
     INVOICE TOTAL
```

The architecture supplies the capabilities. It does not require every invoice to contain every layer.

---

## 9. `adjustment_amount` remains rejected

The old model:

```text
subtotal_amount
+ adjustment_amount
= total_amount
```

is too generic.

`adjustment_amount` can conceal materially different economic facts:

```text
credit
discount
rebate
shared charge
tax
surcharge
```

We should preserve their actual meaning rather than funneling everything through an unexplained signed number.

---

## 10. Monetary terminology should be concise

We also agreed not to mechanically append `_amount` everywhere.

Prefer concepts such as:

```text
items_subtotal
pre_tax_total
taxes_and_surcharges
total
```

rather than:

```text
items_subtotal_amount
pre_tax_total_amount
taxes_and_surcharges_amount
total_amount
```

`amount_paid` remains sensible because it answers a different question: **how much has been paid?**

Merchant-facing `total` could naturally be rendered as:

> **Invoice Total**

---

## 11. Activation invoicing depends on merchant payment terms

We corrected another assumption here.

For a merchant **without payment terms**, activation requires immediate payment:

```text
Activation Request
       ↓
Activation Invoice
       ↓
Immediate Payment
       ↓
Payment Satisfied
       ↓
Activation
```

That merchant can print/retain the Activation Invoice.

Consequently, the Activation Fee would **not subsequently reappear on the periodic invoice**.

For merchants **with payment terms**, the Activation Fee may instead participate in the appropriate periodic invoice according to configured terms.

Engineering must support both capabilities without hard-coding either commercial policy.

---

## 12. Invoice lifecycle remains unchanged

Nothing in these refinements requires reopening the lifecycle:

```text
draft
  ↓
issued
  ├── partially_paid
  ├── overdue
  ├── paid
  └── void
```

Likewise, the established principles remain:

* `paid` and `void` are terminal in v1.
* No overpayment is represented on an invoice.
* Currency is singular per invoice and immutable after issuance.
* Lifecycle timestamps represent actual financial facts.
* Service orchestration owns legal transitions.
* Database constraints protect invariant truth.
* Administration cannot configure away engineering invariants.

---

## 13. Payment remains downstream

Nothing changes the MPA boundary:

> **Commerce determines the obligation. Merchant Payments securely fulfills and records it.**

So:

```text
Future Offering
      ↓
Commercial Charges
      ↓
Invoice Composition
      ↓
Invoice Total
      ↓
Issued Invoice
      ↓
Payment
```

Payment does not retrospectively redefine the commercial charges that produced the invoice.

---

## 14. Statement of Account remains separate

An invoice establishes a particular FO's obligation.

A Statement of Account reports broader financial activity over time.

Therefore we should continue keeping:

```text
Invoice
Payment
Settlement activity
Billing Ledger
Statement of Account
```

as related but distinct concepts.

---

## 15. What remains to settle before rewriting `merchant_invoices`

I would **not change the Go file yet**.

We now understand the conceptual architecture much better, but before issuing another authoritative replacement we should settle exactly what needs to be persisted on the invoice header versus derived/composed from its underlying records.

In particular, we should determine the precise canonical role of:

```text
items_subtotal
pre_tax_total
taxes_and_surcharges
total
```

because the new realization about legitimate **invoice-level shared effects** changes the relationship among them.

That should be an architectural decision, not something we discover halfway through writing SQL.

### The central doctrine

I think the entire discussion now reduces rather elegantly to this:

> **Every Future Offering has its own invoices. Invoice lines describe the products and services charged to that project. Monetary effects attributable to a particular charge remain attributable to that charge; legitimate shared effects may operate at invoice level. The invoice then establishes the Pre-tax Total, Taxes & Surcharges, and Invoice Total. Engineering preserves the financial meaning and invariants; Administration controls commercial policy and merchant-facing language within those boundaries.**

That is the version I would carry forward into the final `merchant_invoices` redesign.
