Taking the initial **Merchant Plans & Pricing MPP_v1.0** summary together with the later invoice-item, Platform Credit, adjustment, tax, and FO-commercial-unit discussions, I think the consolidated agreement is now substantially stronger than the original summary.

# Merchant Plans, Pricing & FO Commercial Architecture — Consolidated Agreement

## 1. The Future Offering is the fundamental commercial unit

The governing commercial principle is:

> **The Future Offering—not the merchant account—is Sagrenti's fundamental commercial unit.**

Creating and maintaining a merchant account is free. A merchant can remain registered indefinitely without incurring charges simply by existing as a merchant. Commercial obligations begin when a particular FO enters a commercial relationship with Sagrenti. 

Accordingly:

**Merchant → Future Offering → commercial obligations**

A merchant may have 1, 10, 100, or 1,000+ FOs. Each FO has its own commercial lifecycle and obligations.

This is why we rejected merchant-wide packaging such as "10 FOs," "100 FOs," or "unlimited FOs." 

---

## 2. Current FO fee structure

We currently contemplate four FO-related fees:

| Fee                               | Commercial purpose                                   |
| --------------------------------- | ---------------------------------------------------- |
| **Activation Fee**                | Entering an FO into active service                   |
| **Subscription Fee**              | Maintaining an active FO                             |
| **Asset Hosting Overage Fee**     | Consumption beyond the FO's included asset allowance |
| **Anticipation Intelligence Fee** | Intelligence generated through QAEs                  |

The Subscription Fee is therefore **per active FO**, not a merchant-wide subscription. 

The subscription should include a reasonable asset allowance. Asset consumption should nevertheless be measured from the beginning so Sagrenti can understand actual FO economics, even if overage charging is initially dormant or generous.

---

## 3. Subscription commitment classifications

We settled on three duration classifications:

**FLEX** — ≤ 3 months
**STANDARD** — > 3 months through 1 year
**EXTENDED** — > 1 year

We specifically removed **"cancel anytime"** from FLEX. FLEX is a short commitment, not the absence of commitment.

**Enterprise is not a duration classification.**

Our current indicative pricing hypothesis remains:

| Commitment | Indicative monthly rate |
| ---------- | ----------------------: |
| FLEX       |                  $19–29 |
| STANDARD   |                  $15–19 |
| EXTENDED   |                  $12–15 |

The governing commercial principle is simply:

> **Greater commitment → better effective subscription economics.**

These are pricing hypotheses, not engineering constants. 

---

## 4. Commitment duration and payment cadence are different things

We deliberately separated **how long the merchant commits** from **how the merchant pays**.

Engineering should therefore be capable of supporting:

**Monthly | Quarterly | Annual | Term Prepaid**

Administration determines which cadences are actually offered for each commitment classification.

Our likely initial policy is approximately:

**FLEX → Monthly / Term Prepaid**
**STANDARD → Monthly / Annual**
**EXTENDED → Annual upfront**

The important architectural rule is that this initial commercial policy must **not hard-code annual payment into EXTENDED**. Administration must later be able to change available payment cadences without redesigning the subscription architecture. This builds on the separation already established in the initial summary. 

---

## 5. Platform Credit promotional model

Our current promotional hypothesis is:

> **$300 Platform Credit per qualifying merchant.**

The full $300 becomes available when awarded. It is **not** divided into three $100 annual awards.

A sufficiently active merchant can consume the entire $300 rapidly if eligible commercial activity permits it. 

This naturally rewards merchants with continuing or multiple FO activity without giving those merchants a larger nominal award.

---

## 6. Platform Credit longevity

The award can remain available for **up to three years**.

We also established a **$100 annual utilization expectation**. If less than $100 is consumed during a 12-month period, the unused portion of that year's $100 expectation expires.

Thus, if the merchant uses $60 during that period, $40 expires.

This prevents indefinite accumulation while still allowing an active merchant to consume the entire award rapidly. 

---

## 7. Platform Credit eligibility is Admin policy

Our current commercial policy is:

**Eligible**

* Activation Fee
* Subscription Fee

**Not eligible**

* Asset Hosting Overage Fee
* Anticipation Intelligence Fee

But this distinction must **not be hard-coded into Engineering**.

The authoritative eligibility mechanism should operate conceptually as:

`merchant_fee_types → merchant_platform_credit_eligible_fee_types → configured eligibility → service enforcement`

Engineering's invariant is:

> **A Platform Credit may be applied only when authoritative configuration permits application against that fee type.**

Administration decides which fee types are eligible at any particular time. 

---

## 8. Maximum Platform Credit application

We settled on a working maximum of:

> **Platform Credits may satisfy up to 50% of an eligible fee.**

That means a $100 eligible fee can be satisfied with at most:

**$50 Platform Credit + $50 cash**

Therefore, consuming the entire $300 award requires at least **$600 of eligible fees**, producing at least $300 of corresponding cash revenue from those charges.

Meanwhile, currently ineligible Anticipation Intelligence and Asset Hosting Overage Fees remain fully cash-payable. 

This gives the promotion meaningful merchant value without allowing credits to eliminate the merchant's cash participation.

---

## 9. Promotion duration and credit validity are separate clocks

We favored an initial **12-month promotional issuance period**.

That creates two separate clocks:

**Promotion availability:** roughly 12 months in which qualifying merchants may receive the $300 award.

**Award validity:** once awarded, the credit may remain usable for up to three years, subject to its utilization rules.

After the initial promotional period, Administration can evaluate actual behavior and renew, modify, replace, or discontinue the promotion.

Already-awarded credits retain the commercial terms under which they were granted. 

---

# Invoice Architecture

## 10. An invoice item represents a commercial product or service being charged

We clarified an important distinction:

> **Only the products and services Sagrenti is charging for belong in `merchant_invoice_items`.**

Therefore invoice items represent things such as:

* Activation Fee
* Subscription Fee
* Asset Hosting Overage Fee
* Anticipation Intelligence Fee

Credits, rebates, discounts, taxes, and surcharges are **not themselves products/services merely because they may be visually displayed underneath a line on the rendered invoice**.

---

## 11. The invoice item is the invoice snapshot/presentation of the fee calculation

The relationship is direct:

**Billable Event → Fee Calculation → Invoice Item**

For example:

`6,500 QAE × $0.25 = $1,625`

becomes an invoice presentation such as:

`Qty 6,500 | Unit $0.25 | Gross $1,625`

So `fee_calculation_id` is not merely a distant provenance reference. The invoice item is effectively the invoice's presentation/snapshot of that calculated commercial obligation. 

---

## 12. Adjustments do not rewrite the underlying charge

A Platform Credit, rebate, discount, tax treatment, surcharge, or other charge-related adjustment must not falsify:

**quantity**
**unit amount**
**gross calculated charge**

For example:

**Activation Fee**

Quantity: 1
Unit amount: $100
Gross charge: $100
Platform Credit: −$50
Net obligation: $50

The original $100 charge remains historically and commercially true.

The credit changes the amount ultimately payable; it does not rewrite the fee calculation. 

---

## 13. Adjustments belong at the lowest commercial grain that caused them

This became one of the most important conclusions of the later discussion:

> **Adjustments should be recorded and attributed at the lowest commercial grain that gives rise to them.**

If a Platform Credit applies to FO ABC's Subscription Fee, it belongs to that specific commercial charge.

The same principle applies to:

**Platform Credits | rebates | discounts | surcharges | taxes**

They should remain attributable to the **FO and the particular charge** that caused them. 

This provides permanent answers to:

**Which FO? Which fee? What adjustment? How much? Why? Who authorized it?**

---

## 14. Platform Credits should not become fake negative invoice items

Because `merchant_platform_credit_applications` already identifies the affected `fee_calculation_id`, we do **not** need to invent:

`Platform Credit     -$50`

as an independent `merchant_invoice_item`.

Nor should we reduce quantity or unit price.

The invoice composition/presentation layer can associate the normalized Platform Credit Application with the corresponding fee-calculation-backed invoice item when rendering the invoice. 

---

## 15. We reversed the earlier position on invoice-wide `adjustment_amount`

This is an important evolution from the initial discussion.

Initially, we considered allowing:

`merchant_invoices.adjustment_amount`

to act as an aggregate roll-up of line-specific adjustments.

After examining attribution, management accountability, large multi-FO invoices, and taxes, we concluded that this creates an unnecessary second representation of the same financial effect.

So the stronger conclusion is:

> **`merchant_invoices.adjustment_amount` should not be an independent financial input representing charge-specific adjustments.**

Instead:

**Line 1 net obligation**
**+ Line 2 net obligation**
**+ Line 3 net obligation**
**= Invoice total**

We still need to decide whether a gross `subtotal_amount` is useful as an informational aggregate, but the invoice header should principally aggregate already-established line obligations rather than independently reinterpret their adjustments. 

---

## 16. Taxes confirm why FO/charge-level provenance matters

Taxes made the architectural issue particularly clear.

Different FOs on the same invoice can have different jurisdictional and tax consequences. An FO may target one jurisdiction while another FO targets another; exemptions or other tax treatments may also differ.

Therefore an invoice-wide tax amount cannot be the authoritative source of tax provenance.

The rule is:

> **Calculate and preserve taxes and other charge-related monetary effects against the FO-specific charge to which they apply.**

The invoice may aggregate them for presentation, but the authoritative commercial attribution remains below the invoice header. 

---

## 17. The broader monetary hierarchy

The commercial architecture we have now established is essentially:

```text
Merchant
   │
   └── Future Offering
          │
          ├── Commercial Charge
          │      ├── Activation Fee
          │      ├── Subscription Fee
          │      ├── Asset Hosting Overage Fee
          │      └── Anticipation Intelligence Fee
          │
          └── Charge-related monetary effects
                 ├── Taxes
                 ├── Surcharges
                 ├── Platform Credits
                 ├── Rebates
                 └── Discounts
```

Everything remains attributable back to the FO and the commercial charge that caused it. 

---

## 18. One deliberate architectural qualification

We should **not** encode the proposition that Sagrenti can *never* have a merchant-level charge.

Our doctrine for the commercial model we are building is narrower and safer:

> **Sagrenti's current merchant commercial obligations originate at the Future Offering level. Merchant-account existence itself is non-billable. Fees and charge-related taxes, surcharges, credits, rebates, and discounts must remain attributable to the Future Offering and commercial charge that gave rise to them.**

That preserves today's architecture without unnecessarily preventing Sagrenti from introducing some genuinely new merchant-level product years from now. 

---

# 19. Engineering versus Administration

Finally, almost all of the numbers we've discussed are **commercial policy rather than engineering invariants**.

Administration should ultimately be capable of governing:

subscription pricing, commitment ranges, payment cadences, Platform Credit award amounts, promotion duration, credit validity, utilization requirements, maximum credit-application percentages, eligible fee types, included asset allowances, and overage policy. 

Engineering provides the complete capability, authoritative provenance, arithmetic integrity, lifecycle safety, auditability, and configuration boundaries.

Administration operates the commercial model **within those boundaries**.

---

## The model in one compact statement

I think our entire agreement can now be reduced to this:

> **Join as a merchant for free.**
>
> **Every Future Offering is its own commercial project.**
>
> **Activate an FO → Activation Fee.**
>
> **Keep it active → Subscription Fee.**
>
> **Exceed its included asset resources → Asset Hosting Overage Fee.**
>
> **Generate measurable anticipation → Anticipation Intelligence Fee.**
>
> **Commit longer → better subscription economics.**
>
> **Platform Credits can encourage activation and continued participation while preserving a merchant cash contribution.**
>
> **Every charge and every monetary effect upon that charge remains attributable to the FO that caused it.**
>
> **The invoice aggregates those established obligations; it does not redefine them.**
>
> **Engineering builds and protects the commercial machinery. Administration governs the commercial policy.**

I think **that** is the point we've actually reached—not merely a pricing model, but a coherent governing commercial architecture for the FO Monetization Layer.
