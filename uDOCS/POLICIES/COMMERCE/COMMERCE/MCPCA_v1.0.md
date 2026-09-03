Merchant Commercial Promotions & Platform Credits Architecture — MCPCA v1.0

1. Purpose and architectural boundary

MCPCA governs Sagrenti's merchant commercial promotions and Platform Credit capability.

It is intentionally separate from both Future Offering Commerce Architecture and Merchant Invoice & Settlement Architecture.

A promotion may influence what a merchant ultimately owes, but it is not an intrinsic property of a Future Offering, an invoice, or a payment.

The governing separation is:

FO Commerce establishes commercial charges.
        ↓
Configured Promotion / Platform Credit may reduce an eligible charge.
        ↓
Invoice Architecture presents the resulting obligation.
        ↓
Merchant Payments fulfills that obligation.

Administration may modify, suspend, replace, or discontinue a promotion without redesigning the underlying FO or invoice architecture.

2. Platform Credit nature

A Platform Credit is a Sagrenti-funded commercial or marketing reduction made available to an eligible merchant under configured promotional terms.

It is not:

merchant cash;

a merchant deposit;

merchant-held funds;

a stored-value balance or wallet;

a payment method;

a payment transaction; or

a settlement transaction.

Platform Credit therefore remains in the Commerce / commercial-adjustment domain, upstream of payment.

The fundamental boundary remains:

Commerce determines the obligation. Merchant Payments securely fulfills and records it.

3. Promotion and awarded credit are different concepts

MCPCA distinguishes the promotion that authorizes issuance from the credit already awarded to a merchant.

Promotion
   ↓ qualifying award
Merchant Platform Credit
   ↓ eligible use
Platform Credit Application

This distinction permits Administration to stop issuing new credits without invalidating credits already awarded under prior terms.

Accordingly, the system must distinguish at least:

promotion issuance period;

award date;

award amount;

award validity/utilization rules; and

application of awarded credit to eligible commercial charges.

4. Current promotional hypothesis

The current working commercial policy is:

$300 Platform Credit per qualifying merchant.

The full $300 becomes available when awarded. It is not three separate $100 annual awards.

A sufficiently active merchant may consume the entire award rapidly if eligible fee activity and application limits permit it.

The $300 amount is Admin commercial policy, not an engineering constant.

5. Promotion issuance period and award validity are separate clocks

The current working policy favors an initial:

12-month promotion issuance period.

During that period, qualifying merchants may receive the promotional award.

Once awarded, the credit may remain available for:

up to three years, subject to its utilization rules.

Thus:

Promotion availability
    ≠
Award validity

When the issuance period ends, Administration may renew, modify, replace, or discontinue the promotion without altering already-established commercial obligations.

6. Grandfathering of awarded credits

Already-awarded credits retain the commercial terms under which they were granted.

Ending or modifying a promotion governs future issuance; it must not silently rewrite the terms of an award already made to a merchant.

This requires durable historical provenance sufficient to determine which promotion and terms governed each award and application.

7. Annual utilization requirement

The current working policy establishes a:

$100 annual utilization expectation.

If less than $100 is consumed during a 12-month utilization period, the unused portion of that year's $100 expectation expires.

Example:

Annual utilization expectation      $100
Credit consumed                       60
                                     ----
Unused portion expiring               40

This rule prevents indefinite accumulation while allowing an active merchant to consume the full award more quickly.

The amount, period, and expiration behavior are Admin-configurable commercial policy within engineering safety boundaries.

8. Eligible fee types are configured, not hard-coded

Current commercial policy is:

Eligible

Activation Fee

Subscription Fee

Currently ineligible

Asset Hosting Overage Fee

Anticipation Intelligence Fee

These classifications are not engineering constants.

The authoritative conceptual path is:

merchant_fee_types
      ↓
merchant_platform_credit_eligible_fee_types
      ↓
configured eligibility
      ↓
service enforcement

The engineering invariant is:

A Platform Credit may be applied only when authoritative configuration permits application against that fee type.

Administration determines which fee types are eligible at a given time.

9. Maximum application and merchant cash participation

The current working maximum is:

Platform Credits may satisfy up to 50% of an eligible fee.

Example:

Eligible fee                         $100
Maximum Platform Credit               50
Required remaining obligation         50

At that policy level, consuming a full $300 award requires at least $600 of eligible fees and therefore preserves at least $300 of corresponding merchant cash participation on those charges.

The 50% value is policy, not an invariant. Engineering must support and enforce the configured permitted percentage without permitting an application to exceed the applicable charge, available credit, configured cap, or other integrity boundary.

10. Platform Credit application is charge-specific

A Platform Credit does not reduce a merchant account or invoice in the abstract.

It applies to a specific eligible commercial charge.

Conceptually:

Future Offering
      ↓
Fee Calculation
      ↓
Platform Credit Application
      ↓
Net Commercial Obligation

The application must preserve attribution sufficient to answer:

Which merchant?
Which Future Offering?
Which fee calculation / charge?
Which promotion or awarded credit?
How much was applied?
Under which governing terms?
When was it applied?

This charge-level attribution is fundamental to auditability and invoice composition.

11. Platform Credit does not rewrite the underlying charge

A Platform Credit changes the amount ultimately payable. It does not falsify the commercial charge that produced the obligation.

Example:

Activation Fee
Quantity                              1
Unit amount                         $100
Gross calculated charge             100
Platform Credit                      -50
                                    ----
Net obligation                        50

The original $100 charge remains historically and commercially true.

Therefore Platform Credit application must not rewrite:

quantity;

unit amount;

gross calculated fee; or

the fee calculation's commercial provenance.

12. Relationship to merchant_platform_credit_eligible_fee_types

merchant_platform_credit_eligible_fee_types represents the authoritative configuration boundary for determining which fee types may participate in Platform Credit programs.

Its architectural purpose is eligibility configuration, not award ownership and not application history.

Engineering should consult authoritative eligibility configuration at the point where an application is evaluated and must fail closed when the requested fee type is not eligible under the governing configuration.

13. Relationship to merchant_platform_credit_applications

merchant_platform_credit_applications represents the auditable application of Platform Credit against a particular eligible calculated fee.

It is therefore the authoritative bridge between:

awarded Platform Credit
        ↓
eligible fee calculation
        ↓
commercial reduction

The application remains a first-class commercial record. It must not be collapsed into the fee calculation, hidden in an invoice header adjustment, or converted into a synthetic negative invoice item.

The existing application architecture should remain charge-specific unless a later independent CE review identifies a concrete integrity defect.

14. Invoice presentation without becoming an invoice item

merchant_invoice_items identifies the products and services Sagrenti charged for.

A Platform Credit is not a product or service and therefore does not become an independent invoice item.

Preferred invoice composition associates the credit with the charge it reduced:

Activation Fee                    $100
Platform Credit                    -50
                                  ----
Net                                 50

rather than manufacturing:

Platform Credit                    -50

as a standalone merchant_invoice_item.

Likewise, a generic invoice-header Platform Credit field should not be introduced merely to reproduce a presentation subtotal. The normalized application record preserves the authoritative financial meaning.

15. Platform Credit remains pre-invoice

Platform Credit is a commercial reduction applied after the underlying fee has been calculated but before the resulting obligation is established on the invoice.

Billable Event
      ↓
Fee Calculation
      ↓
Platform Credit Application, if applicable
      ↓
Net Commercial Obligation
      ↓
Invoice Item
      ↓
Invoice

It therefore remains distinct from payment and settlement adjustments.

A later payment must not retrospectively redefine the Platform Credit application or the fee calculation it reduced.

16. Platform Credits and settlement adjustments are different

A Platform Credit exists because Sagrenti has granted a promotional commercial benefit.

A settlement adjustment exists because of the conditions under which an already-issued obligation is settled.

For example, an early-payment discount belongs downstream of the invoice:

Invoice
   ↓
Payment
   ↓
Settlement Adjustment

That is categorically different from:

Fee Calculation
   ↓
Platform Credit Application
   ↓
Invoice

MCPCA governs the latter, not the former.

17. Engineering and Administration boundary

Engineering owns the stable capability and invariants required to make promotions safe and auditable.

Engineering must preserve, at minimum:

authoritative promotion and award provenance;

authoritative fee-type eligibility;

charge-specific application provenance;

exact monetary arithmetic;

prevention of over-application;

prevention of use beyond available awarded credit;

enforcement of configured application limits;

lifecycle and expiration integrity;

historical auditability; and

grandfathering of already-awarded terms.

Administration governs commercial policy within those boundaries, including:

whether a promotion is active;

qualification rules;

award amount;

issuance period;

award validity period;

utilization requirements;

eligible fee types;

maximum application percentage; and

merchant-facing terminology.

The governing doctrine is:

Engineering implements the complete promotional capability and protects its invariants; Administration determines how that capability is commercially operated.

18. Human-facing terminology is configurable

Stable engineering semantics must not force Sagrenti to preserve today's merchant-facing vocabulary indefinitely.

Internal concepts such as Platform Credit, promotion, eligibility, award, and application may remain stable domain identities while Administration controls appropriate public or merchant-facing labels where presentation requires them.

Where practical, frontend and invoice presentation should resolve configured terminology at runtime rather than requiring recompilation merely because Sagrenti changes its commercial vocabulary.

Historical issued documents must preserve their snapshotted presentation so later terminology changes do not rewrite the past.

19. Architectural position of MCPCA

The three commercial architectures fit together as follows:

┌─────────────────────────────────────────────┐
│ FUTURE OFFERING COMMERCIAL ARCHITECTURE     │
│                                             │
│ FO → Activation Cycle → QAE                 │
│    → Fees / Subscription                    │
│    → Commercial Charge                      │
└──────────────────────┬──────────────────────┘
                       │
                       ▼
┌─────────────────────────────────────────────┐
│ MERCHANT INVOICE & SETTLEMENT ARCHITECTURE  │
│                                             │
│ Fee Calculation → Invoice Item → Invoice    │
│ → Payment → Settlement → Ledger/Statement   │
└─────────────────────────────────────────────┘

              ▲
              │ applies where configured
              │
┌─────────────┴───────────────────────────────┐
│ MCPCA                                      │
│ COMMERCIAL PROMOTIONS & PLATFORM CREDITS   │
│                                             │
│ Promotion → Award → Eligibility             │
│ → Application → Audit                       │
└─────────────────────────────────────────────┘

MCPCA is independent because promotions are optional commercial programs, not structural requirements of Future Offerings, invoicing, or payments.

20. Compact governing statement

MCPCA v1.0 can be reduced to the following doctrine:

Platform Credits are configurable Sagrenti-funded commercial incentives, not cash or payment. A promotion governs whether credits may be awarded; an awarded credit retains the terms under which it was granted. Credits may be applied only to configured eligible fees, within configured limits, and every application remains attributable to the particular commercial charge it reduced. The underlying fee remains historically true. Platform Credits operate before invoicing, may be shown alongside the affected charge without becoming invoice items, and remain entirely separate from merchant funds, payment, and settlement. Engineering protects provenance, arithmetic, lifecycle, and auditability; Administration governs the commercial program within those boundaries.

Version: MCPCA v1.0
Status: Consolidated current agreement