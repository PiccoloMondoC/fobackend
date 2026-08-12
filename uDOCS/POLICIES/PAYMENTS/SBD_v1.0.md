# Sagrenti Business Doctrine — Consolidated Summary

## 1. What Sagrenti is

Sagrenti is a **Future Offering Platform that delivers Market Anticipation Intelligence as a Service**.

Its central purpose is to help businesses understand what the market is saying **before launch**.

The defining question is:

> **What is the market telling us before launch?**

Sagrenti is fundamentally a software platform. It is not a consulting company, marketing agency, managed-service provider, product-development firm, or research consultancy. The software performs the measurement, analysis, reporting, and intelligence functions. 

## 2. The Future Offering belongs to the merchant

Every Future Offering is a merchant-owned project.

The merchant decides what it intends to offer, the target market, duration, goals, launch strategy, and which supported consumer engagement opportunities to make available.

Sagrenti does **not** sell the Future Offering to the merchant.

Sagrenti provides the intelligence infrastructure surrounding it.

This gives us the fundamental ownership distinction:

> **The Future Offering belongs to the merchant. Market Anticipation Intelligence is the Sagrenti SaaS product.** 

## 3. Market Anticipation Intelligence is the product

The canonical product/category name is:

> **Market Anticipation Intelligence**

Sagrenti measures and interprets market anticipation surrounding a Future Offering.

The intelligence may include:

* anticipation measurement;
* engagement analytics;
* trends and momentum;
* progress against merchant-defined goals;
* reporting;
* insights;
* recommendations.

“Market” matters because the intelligence is not fundamentally **Merchant Intelligence** or **Consumer Intelligence**. Consumers generate signals, merchants consume the resulting intelligence, but **market anticipation is what Sagrenti measures**.

For shorter operational and commercial terminology, **Anticipation Intelligence** is appropriate.

## 4. Consumer anticipation signals

Every Future Offering supports **Watch** as a platform-owned anticipation signal.

Depending upon the Future Offering, the merchant may additionally enable supported engagement opportunities such as:

* Waitlist;
* Early Access Request;
* Beta Application;
* Reservation Interest;
* Preorder Intent.

A Future Offering need not support every engagement type.

The intelligence comes from observing the relevant signals over time—not merely displaying cumulative counters.

## 5. Anticipation Intelligence Reports

While Anticipation Intelligence services operate, Sagrenti produces reporting around the Future Offering.

A report may show, for the engagement types applicable to that Future Offering:

```text
                         Goal       Achieved

Watch                   50,000       31,000
Waitlist                  5,000        3,860
Early Access              2,500        1,540
Beta Applications         1,000          542
Reservation Interest        500          241
Preorder Intent              250          128
```

But raw counts are only the foundation.

The eventual value proposition includes trends, momentum, changes, interpretation, insights, and recommendations.

This is what makes the SaaS model particularly compelling: a merchant can repeatedly return to Sagrenti to understand how market anticipation is developing.

## 6. Merchant goals are intelligence inputs, not billing triggers

Merchants may establish goals for their Future Offerings.

Goals support:

* measurement;
* progress analysis;
* reporting;
* insights;
* recommendations.

They do **not** determine whether Sagrenti gets paid.

We therefore rejected milestone billing.

A merchant cannot avoid an invoice simply because a goal was not reached, nor should an artificially ambitious goal distort Sagrenti's revenue.

## 7. Billing and reporting are related but independent

Each billing cycle can produce two conceptually distinct artifacts:

```text
Anticipation Intelligence Report
        │
        └── What is the market telling the merchant?

Anticipation Intelligence Invoice
        │
        └── What does the merchant owe Sagrenti?
```

The report demonstrates and explains value.

The invoice records the commercial obligation.

Goal achievement does not release the invoice.

## 8. Canonical fee taxonomy

We have reduced the core commercial taxonomy to four fees:

| Fee                                          | Purpose                                                                                           |
| -------------------------------------------- | ------------------------------------------------------------------------------------------------- |
| **Anticipation Intelligence Activation Fee** | One-time activation of Anticipation Intelligence services for a Future Offering                   |
| **Anticipation Intelligence Fee**            | Ongoing Anticipation Intelligence measurement, analytics, reporting, insights and recommendations |
| **Campaign Performance Fee**                 | Present-commerce Launch Campaign activity only                                                    |
| **Subscription Fee**                         | Optional; applies only when the configured commercial model uses subscriptions                    |

The category is **Market Anticipation Intelligence**, while the commercial fee names deliberately use the shorter **Anticipation Intelligence** terminology. 

This replaces older terminology including **Setup Fee**, **Future Offering Fee**, and **Launch Intelligence Fee**.

## 9. Plans are capability, not mandatory business policy

Sagrenti has built Merchant Program Plans as a complete platform capability.

That does **not** mean Sagrenti must always operate using plans.

When Plans are disabled, merchants can use applicable platform capabilities without being classified as Pay-As-You-Go, Standard, Premium, Enterprise, etc.

When Plans are enabled, Admin may configure commercial packages such as those—or different plans in the future.

Therefore:

> **Merchant Plans are optional commercial packaging of platform capabilities, not an architectural prerequisite for using those capabilities.**

## 10. Subscriptions are equally optional

The Subscription capability remains production-ready even if Sagrenti launches without requiring subscriptions.

This is especially important because Future Offering activity can be episodic: a merchant may run a Future Offering, disappear for months, and return when it has another project.

Present-commerce Launch Campaign activity may be much more continuous.

The architecture therefore does not force one commercial model onto both.

Admin can enable Plans and Subscriptions when their economics make sense and disable them when they do not.

## 11. Commercial policy is configuration

This became one of the central architectural doctrines.

Engineering creates the capability.

Administration determines how the business currently uses that capability.

Examples include:

* Plans enabled/disabled;
* Subscriptions enabled/disabled;
* fee schedules;
* fee enablement or waiver;
* rebates and discounts;
* commercial promotions;
* merchant-specific commercial adjustments;
* credit eligibility;
* billing policy;
* supported provider selection.

Changing these supported policies should not require changing source code.

BEG §18.6A now formalizes this: Engineering must implement complete operational capabilities and governed extension points, while Administration operates those capabilities through configuration. 

## 12. But Admin is not sovereign over engineering

This was the important companion doctrine we subsequently identified.

> **Engineering invariants are not policy.**

Admin's configurable authority exists **inside boundaries established by Engineering**.

Administration must never be able to configure away such things as:

* security;
* authorization;
* data integrity;
* transactional consistency;
* required auditability;
* canonical validation;
* cryptographic guarantees;
* invariant lifecycle correctness;
* other fundamental system-safety requirements.

Hence the §18.6A/§18.6B relationship:

> **Engineering builds complete capabilities and defines their safe operating boundaries. Administration governs commercial and operational behavior within those boundaries.**

That prevents both extremes: commercial policy frozen into code and an Admin Console powerful enough to undermine the platform.

## 13. Standard pricing, platform promotions and merchant adjustments are distinct

We've also separated three commercial concepts:

```text
Standard Fee Schedule
        │
        ▼
Platform Commercial Promotion
        │
        ▼
Merchant Commercial Adjustment
        │
        ▼
Fee Calculation
```

**Fee schedules** answer:

> What do we ordinarily charge?

**Platform commercial promotions** answer:

> What commercial campaign is Sagrenti offering to eligible merchants?

Examples include a Founding Merchant Promotion or Early Adopter Promotion.

These belong to:

```text
platform_commercial_promotions.go
```

**Merchant commercial adjustments** answer:

> What exceptional commercial treatment has Sagrenti granted this particular merchant?

These belong to:

```text
merchant_commercial_adjustments.go
```

Thus a 50% Founding Merchant promotion and a negotiated 25% discount for one particular merchant are not the same thing.

## 14. Platform credits remain a separate capability

Platform Credits should not be confused with promotions or merchant-specific adjustments.

They represent credit value that can reduce an eligible commercial obligation.

So we retain separate concepts:

* standard pricing;
* commercial promotions;
* merchant-specific adjustments;
* platform credits.

That distinction preserves auditability and prevents pricing policy from becoming one undifferentiated discount mechanism.

## 15. Merchant Payments Architecture

We abandoned **Merchant Treasury Architecture** because “treasury” described a much broader financial domain than Sagrenti needs.

The governing domain is now:

> **Merchant Payments Architecture**

Its essential question is:

> **Now that Commerce Architecture has determined what the merchant owes, how does the merchant satisfy that obligation?**

Commerce Architecture determines the commercial obligation.

Merchant Payments Architecture fulfills it. 

The v1 architecture remains deliberately lean:

* merchant payment methods;
* provider connectivity;
* payment execution;
* payment recording;
* payment collection.

We do not need a treasury platform simply to collect money owed to Sagrenti.

## 16. Direct payment is the v1 collection model

The immediate operating model is invoice-and-collect/direct payment.

Payment may ultimately involve appropriate supported methods such as cards and bank payments.

We explored merchant deposits, prefunded balances, milestone payments, escrow, embedded banking, and treasury-style balances. Those discussions helped clarify the problem, but those mechanisms are **not required parts of the governing v1 business architecture**.

They remain possible engineering options if future economics, scale, regulation, or payment risk justify them.

## 17. Bank connectivity belongs inside Payments Architecture

Bank connectivity is a platform capability rather than a Plaid-specific domain concept.

The current implementation direction uses **Plaid**, while preserving a provider-neutral architecture.

The separation is:

```text
merchant_payment_methods.go
        │
        ▼
merchant_payment_method_provider_links.go
        │
        ▼
merchant_payments.go
```

`merchant_payment_method_provider_links.go` provides the seam through which Plaid—and potentially another provider later—can connect merchant payment methods without making Plaid itself part of the canonical payment-domain contract. 

## 18. Sagrenti remains SaaS, not managed services

We considered whether Sagrenti might operate Future Offering engagements on behalf of merchants.

We rejected that direction.

Sagrenti provides the platform.

Merchants conduct their Future Offering businesses using the software.

That is important economically and strategically: Sagrenti should be able to grow its merchant base without proportionally increasing staff performing merchant work.

Software performs the repeatable work so the company can continue investing in the intelligence product.

## 19. Why SaaS now matters more

The move toward SaaS became much more compelling once we clarified **what the SaaS actually delivers**.

The compelling experience isn't merely having access to software.

It's being able to open Sagrenti and see the market moving around a planned launch:

```text
Watch                 ↑
Waitlist               ↑
Reservation Interest   ↑
Preorder Intent        ↑
```

and eventually receive meaningful interpretation of those movements.

The merchant's reason to return becomes:

> **What is the market telling me today?**

For a small business owner deciding whether a product has genuine demand—or an executive planning a major launch—that information can carry real emotional intensity because significant commercial decisions depend upon it.

The product should create that engagement through **valuable evidence and insight**, not through artificial addictive mechanics.

## 20. Consumer identity remains protected

Consumer participation generates anticipation intelligence, but consumer identity is not itself the product sold to merchants.

The architecture preserves consumer identity sovereignty.

Future Offering engagement does not ordinarily disclose consumer identity to the merchant. Where a commercial transition eventually requires interaction with the merchant, that transition should occur through consumer-authorized mechanisms.

Thus Sagrenti can sell intelligence about market anticipation without turning individual consumer identities into merchant data.

## 21. The business in one architecture

I think the clearest representation of where we've landed is:

```text
Merchant
   │
   ▼
Creates Future Offering
   │
   ▼
Sagrenti activates Anticipation Intelligence services
   │
   ▼
Consumers generate anticipation signals
   │
   ▼
Sagrenti measures those signals
   │
   ▼
Market Anticipation Intelligence
   │
   ├── Measurement
   ├── Analytics
   ├── Reports
   ├── Insights
   └── Recommendations
   │
   ▼
Merchant makes better pre-launch decisions
```

Sagrenti monetizes **its intelligence service**, not ownership of the merchant's Future Offering and not the identity of participating consumers.

## Final doctrine

If I had to compress the entire SBD into three statements now, they would be:

> **The Future Offering belongs to the merchant. Market Anticipation Intelligence belongs to Sagrenti.**

> **Sagrenti is a Future Offering Platform that delivers Market Anticipation Intelligence as a Service, helping merchants understand what the market is telling them before launch.**

> **Engineering builds complete, production-ready capabilities and defines their invariant safety boundaries; Administration governs how those capabilities are commercially and operationally exercised through configuration within those boundaries.**

Those three propositions now connect the **business model, product model, commercial model, engineering model, and administrative model** far more cleanly than the original SBD summary did.  
