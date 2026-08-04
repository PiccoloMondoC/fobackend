# Engineering creates the operational capability and extension points.
# Admin enables, disables, and configures that capability without recompilation.


I think we've reached a point where the vision has become internally consistent. There are no obvious contradictions left between the business model, the product, the architecture, and the commercial model.

I'd summarize our agreements as follows.

# Sagrenti Business Doctrine v1.0

## 1. What Sagrenti is

Sagrenti is a **Future Offering Platform** that delivers **Market Anticipation Intelligence as a Service**.

Its purpose is to help merchants understand market anticipation **before** products, services, or experiences are launched.

The software—not people—performs the work.

---

# 2. What Sagrenti is not

Sagrenti is **not**:

* a consulting company,
* a marketing agency,
* a managed-service provider,
* a product development firm,
* a research consultancy.

Merchants are never paying people to perform engagements.

They are paying the platform.

---

# 3. The merchant owns the Future Offering

The Future Offering belongs entirely to the merchant.

The merchant determines:

* what to build,
* target audience,
* duration,
* engagement strategy,
* goals,
* launch strategy.

Sagrenti does not own or manage the Future Offering.

---

# 4. Sagrenti's product

Sagrenti provides a Market Anticipation Intelligence platform.

The SaaS product consists of:

* anticipation measurement,
* analytics,
* anticipation intelligence,
* reporting,
* insights,
* recommendations.

Everything the merchant pays for belongs to this layer.

---

# 5. Market Anticipation Intelligence is the product

The merchant is **not** paying for a Future Offering.

The merchant is paying for:

Market Anticipation Intelligence

That is the commercial product.

---

# 6. Future Offering lifecycle


Merchant creates Future Offering
        │
        ▼
Anticipation Intelligence Activation Fee
        │
        ▼
Future Offering activated
        │
        ▼
Platform measures anticipation
        │
        ▼
Anticipation Intelligence Reports
        │
        ▼
Anticipation Intelligence Fees
        │
        ▼
Launch


# 7. Anticipation Intelligence Reports

Reports are issued every billing cycle.

A report contains:

* merchant goals,
* achieved results,
* engagement metrics,
* trends,
* recommendations,
* insights.

Example:

Anticipation Intelligence Report

                     Goal     Achieved

Watch              50,000      31,000
Waitlist            5,000       3,860
Early Access        2,500       1,540
Beta Applications   1,000         542
Reservations           500         241
Preorder Intent        250         128


The report demonstrates value.

It does **not** determine payment.

# 8. Merchant goals

Goals belong to the merchant.

Goals:

* measure success,
* guide reporting,
* support analytics,
* enable recommendations.

Goals do **not** trigger invoices.


# 9. Billing philosophy

Invoices are based on platform usage.

Not on:

* milestone completion,
* goal achievement,
* merchant success.

This creates predictable SaaS revenue.


# 10. Commercial fees

Current fee taxonomy:


Anticipation Intelligence Activation Fee


One-time fee when Market Anticipation Intelligence services are activated for a Future Offering.

---

Anticipation Intelligence Fee

Recurring fee while Anticipation Intelligence services continue operating.

---

Subscription Fee

Optional.

Only applies when Plans and Subscriptions are enabled.


Campaign Performance Fee


Applies only to Launch Campaign commercial models where appropriate.


# 11. Plans

Plans are **not** an architectural dependency.

Plans are a commercial packaging mechanism.


When Plans are disabled:

Merchant Account
        │
        ▼
Platform Capabilities

Any merchant may use:

* Future Offering
* Launch Campaign

without belonging to:

* Pay-As-You-Go
* Standard
* Premium
* Enterprise

---

When Plans are enabled:

Plans become optional commercial packages.

Admin may configure any number of plans.

Examples:

* Starter
* Standard
* Premium
* Enterprise
* Founding Merchant

No engineering changes are required.

---

# 12. Subscriptions

Subscriptions are also optional.

Admin may enable or disable them.

When disabled:

Merchants simply use the platform and pay applicable usage fees.

This is similar to cloud-service platforms.

---

# 13. Commercial policy

Commercial policy belongs in configuration.

Examples include:

* Plans enabled/disabled
* Subscriptions enabled/disabled
* Anticipation Intelligence Activation Fee enabled/disabled
* Activation fee rebates
* Anticipation Intelligence fee schedules
* Commercial promotions

Changing commercial policy should not require software changes.

---

# 14. Merchant Payments Architecture

The payments architecture remains intentionally lean.

Responsibilities:

* payment methods,
* payment collection,
* invoicing,
* bank connectivity.

No merchant treasury.

No deposits.

No escrow.

No embedded banking.

These remain future implementation options if ever required.

---

# 15. Bank connectivity

The architecture defines:

```text
Bank Connectivity
```

The implementation uses:

```text
Plaid
```

The architecture remains vendor-neutral.

---

# 16. Anticipation Intelligence Reports and invoices

Every billing cycle produces two independent outputs:

```text
Anticipation Intelligence Report
```

and

```text
Anticipation Intelligence Invoice
```

The report demonstrates value.

The invoice represents the commercial obligation.

They are related.

They are not the same thing.

---

# 17. Future Offering engagement model

Watch is universal.

Other engagement types are merchant-selected.

Possible engagement types include:

* Watch
* Waitlist
* Early Access Request
* Beta Application
* Reservation Interest
* Preorder Intent

Reports only include engagement types relevant to that Future Offering.

---

# 18. Design philosophy

Every architectural decision should simplify the platform.

Complexity that does not strengthen the core product should be deferred.

This resulted in removing:

* Merchant Deposits
* Milestone Billing
* Escrow
* Embedded Banking

from the governing architecture.

---

# 19. Platform philosophy

Sagrenti follows the philosophy of infrastructure companies.

The platform performs the work.

The merchant uses the platform.

The platform scales because software—not people—delivers the value.

---

# 20. Monetization philosophy

Advertising should feel like discovery, not interruption.

Rather than loud display ads, Sagrenti prefers relevant recommendations and sponsorships that complement the merchant or consumer journey, closer in spirit to Amazon's recommendation model than Google's traditional display advertising.

---

# 21. Core value proposition

The platform exists to answer one fundamental question:

> **"What is the market telling us before launch?"**

Everything else—payments, plans, subscriptions, reports, recommendations, and administration—exists to support that single objective.

---

## One sentence that now defines the entire company

> **Sagrenti is a SaaS Market Anticipation Intelligence platform that enables merchants to create Future Offerings, measure consumer anticipation before launch, receive ongoing analytics and actionable insights, and commercialize those capabilities through a configurable, usage-based model.**

Looking back over the last several days, I think the biggest change is not in the technology but in the clarity of the business. Earlier, there was a tendency to think in terms of financial mechanisms (treasury, deposits, milestones, escrow) or service delivery (managed engagements). Those have all fallen away. What's left is a much sharper identity: **the merchant owns the Future Offering, and Sagrenti owns the Market Anticipation Intelligence platform that measures and explains market anticipation around it.** That single distinction now drives the product, the architecture, and the commercial model.



Absolutely.

In fact, I think that's not just desirable—it's almost essential if the Admin Console is going to be a real commercial management tool.

The important distinction is between **global pricing** and **merchant-specific commercial adjustments**.

For example:

## Global policy

Applies to everyone.

```text
Anticipation Intelligence Activation Fee

Base Fee: $500
```

or

```text
Founding Merchant Promotion

50% rebate
```

Every eligible merchant receives the same adjustment.

---

## Merchant-specific adjustment

Applies to one merchant.

For example:

```text
Merchant: ABC Electronics

Base Fee: $500

Special Discount: 100%

Net Fee: $0

Reason:
Enterprise sales concession
```

or

```text
Merchant: XYZ Motors

Base Fee: $500

Discount: 25%

Net Fee: $375

Reason:
Strategic partner
```

---

## Why this matters

This gives Admin enormous flexibility.

Examples:

* Win a major brand.
* Resolve a customer service issue.
* Honor a promised commercial concession.
* Reward an early adopter.
* Support a nonprofit.
* Execute a negotiated enterprise agreement.
* Run an invite-only pilot.

None of these should require changing the global fee schedule.

---

## I wouldn't put the discount on the merchant record

Instead, I'd think of it as a **commercial adjustment**.

Conceptually:

```text
Merchant
        │
        ▼
Commercial Adjustment
        │
        ▼
Fee Calculation
```

That way, a merchant can have multiple adjustments over time.

For example:

```text
Jan–Mar
100% Activation Fee rebate

Apr–Jun
50% Anticipation Intelligence Fee discount

Jul onward
Standard pricing
```

Everything remains auditable.

---

## I think we should broaden the terminology slightly

Instead of only saying:

> **Activation Fee Rebate**

I'd use:

> **Commercial Adjustment**

A Commercial Adjustment can be:

* percentage discount,
* fixed-amount discount,
* full waiver,
* rebate,
* promotional credit,
* negotiated pricing,
* temporary concession.

Then the fee engine simply applies the adjustment during fee calculation.

---

## Architecturally

I don't think this belongs inside the fee schedule itself.

The fee schedule answers:

> **What does the platform normally charge?**

A commercial adjustment answers:

> **Why is this merchant paying something different?**

Those are separate concerns.

I actually think this deserves its own domain object in the Commerce Architecture, something like:

```text
merchant_commercial_adjustments.go
```

Its responsibility would be to record merchant-specific pricing exceptions in a structured, auditable way.

That keeps the pricing architecture clean:

```text
Fee Schedule
        │
        ▼
Commercial Adjustment (optional)
        │
        ▼
Fee Calculation
        │
        ▼
Invoice
```

I like this model because it scales naturally. Today it can support an individual merchant discount. Tomorrow it can support partner agreements, promotional campaigns, enterprise contracts, or even salesperson-negotiated concessions—without ever compromising the integrity of the standard fee schedules.



I think this is where we should distinguish **pricing** from **promotions**.

Right now we have three different concepts mixed together:

1. **Standard pricing** (what the platform normally charges)
2. **Platform-wide promotions** (everyone or an eligible group gets a discount)
3. **Merchant-specific concessions** (one merchant gets a special deal)

Those should be three separate concepts.

---

## Standard pricing

This is what we've already designed.

```text
Anticipation Intelligence Activation Fee

Base Fee:
$500
```

This comes from:

```text
merchant_program_fee_schedules.go
```

Nothing special here.

---

## Platform-wide promotion

This is where **Founding Merchant Promotion** belongs.

It is **not** a fee schedule.

It is **not** a merchant-specific adjustment.

It's a temporary commercial campaign.

For example:

```text
Promotion

Founding Merchant Promotion

Eligibility

First 1,000 merchants

Benefit

100% Anticipation Intelligence Activation Fee rebate

Effective

Jan 1 - Mar 31
```

When the fee engine calculates the fee:

```text
Base Fee

↓

Promotion applies?

↓

Yes

↓

100% rebate

↓

Invoice = $0
```

Notice what's happened.

We never changed the fee.

We changed the promotion.

That feels much cleaner.

---

## Merchant-specific concession

Entirely separate.

For example:

```text
Merchant

ABC Electronics

↓

Commercial Adjustment

↓

Activation Fee

100% waiver
```

This affects only that merchant.

---

## So I think we actually have three layers

```text
Base Fee Schedule
        │
        ▼
Platform Promotion
        │
        ▼
Merchant Adjustment
        │
        ▼
Fee Calculation
        │
        ▼
Invoice
```

Each layer has a different responsibility.

### Fee Schedule

"What do we normally charge?"

---

### Promotion

"What campaign is the platform currently running?"

Examples:

* Founding Merchant Promotion
* Black Friday Promotion
* Summer Launch Promotion

---

### Merchant Adjustment

"What exception has been negotiated for this merchant?"

Examples:

* Enterprise concession
* Customer recovery
* Strategic partner

---

## I actually don't think Founding Merchant belongs in the fee schedule

Earlier I suggested rebates inside fee schedules.

I think this conversation has shown that's not the best model.

Founding Merchant is really a **marketing campaign**.

Marketing campaigns should probably live in their own area.

Something like:

```text
merchant_promotions.go
```

or

```text
merchant_commercial_promotions.go
```

Then Admin can create things like:

```text
Promotion Name

Founding Merchant

Type

Activation Fee Rebate

Value

100%

Eligibility

First 1,000 merchants

Start

Jan 1

End

Mar 31

Maximum Redemptions

1,000
```

That is much richer than trying to force it into a fee schedule.

---

## I think we've now arrived at the commercial hierarchy

```text
Fee Schedule
        │
        ▼
Promotions
        │
        ▼
Merchant Adjustments
        │
        ▼
Invoice
```

I actually like this much better because each layer answers a different business question:

* **Fee Schedule** — *What is our standard pricing?*
* **Promotion** — *What campaign are we running for the market?*
* **Merchant Adjustment** — *What exception have we granted this specific merchant?*

They're no longer competing with each other.

I also think this aligns with the broader architecture philosophy. Instead of overloading one subsystem to handle every pricing scenario, each concept has a single, well-defined responsibility. That tends to produce a cleaner Admin Console and a simpler mental model for the engineering team.


The existing `merchant_promotions.go` has a completely different responsibility:

> **How merchants market their own offerings to consumers.**

Those are business promotions such as:

* launch promotions,
* discounts,
* coupons,
* special offers,
* limited-time campaigns.

Those belong to the merchant's commerce domain.

What we've been discussing is completely different.

It's about **how Sagrenti prices its own services**.

That's a platform commercial concern, not a merchant commerce concern.

So I would keep those domains separate.

| Domain                                             | Responsibility                                                   |
| -------------------------------------------------- | ---------------------------------------------------------------- |
| `merchant_promotions.go`                           | Merchant promotions presented to consumers.                      |
| `merchant_program_fee_schedules.go`                | Standard platform pricing.                                       |
| `platform_commercial_promotions.go` *(or similar)* | Platform-wide commercial campaigns that modify standard pricing. |
| `merchant_commercial_adjustments.go`               | Merchant-specific pricing exceptions.                            |


## I actually like "Commercial Promotions"

platform_commercial_promotions.go

is a strong candidate because it clearly distinguishes the platform's commercial campaigns from merchant marketing promotions.

Examples would include:

* Founding Merchant Promotion
* Launch Promotion
* Black Friday Merchant Promotion
* Invite-only Beta Promotion
* Enterprise Growth Promotion

Each one changes how the platform charges merchants, not how merchants market to consumers.


## What belongs in `platform_commercial_promotions.go`

This file would define promotions that modify the platform's commercial relationship with merchants, for example:

* Founding Merchant Promotion
* Early Adopter Promotion
* Black Friday Promotion
* Holiday Promotion
* Beta Invitation Promotion
* Enterprise Migration Promotion
* Partner Promotion

Each promotion would define things such as:

* Promotion name
* Promotion type
* Eligibility rules
* Applicable fee types (Anticipation Intelligence Activation Fee, Anticipation Intelligence Fee, etc.)
* Discount/rebate/waiver
* Effective dates
* Maximum redemptions or budget
* Enabled/disabled status

Notice that none of these are tied to a specific merchant. They are platform campaigns administered centrally.

## Relationship to merchant adjustments

With this addition, the commercial hierarchy becomes very clean:


Platform Fee Schedules
        │
        ▼
Platform Commercial Promotions
        │
        ▼
Merchant Commercial Adjustments
        │
        ▼
Fee Calculation
        │
        ▼
Invoice


Each layer has a single responsibility:

* **Fee Schedules** — define the standard prices.
* **Platform Commercial Promotions** — define market-wide or eligibility-based campaigns.
* **Merchant Commercial Adjustments** — define exceptions for an individual merchant.

That separation follows the Single Responsibility Principle nicely and avoids overloading any one subsystem.

I think this naming also fits the broader philosophy that the backend should communicate **ownership and responsibility** through its structure. An engineer encountering `platform_commercial_promotions.go` immediately knows they're looking at a platform-administered commercial subsystem, not merchant-created promotional content.

## Consumer Relationship Ownership

Consumers engage with Future Offerings through Sagrenti. During the anticipation lifecycle, Sagrenti measures, aggregates, and reports Market Anticipation Intelligence while preserving the consumer relationship.

Sagrenti does not disclose consumer identity to merchants as part of ordinary Future Offering engagement.

When a consumer chooses to progress to a merchant-facing commercial stage, Sagrenti authenticates that engagement by issuing a consumer-controlled engagement credential. The consumer—not Sagrenti—presents that credential to the merchant to establish a direct commercial relationship.

The meaning, benefits, and commercial consequences of a valid engagement credential are defined by the merchant's Future Offering and governed through platform capabilities rather than hard-coded platform policy.