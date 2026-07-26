## Merchant Payments Architecture

> **Merchant Payments Architecture defines how merchants authorize, connect, and complete payments for platform services.**

Everything in the phase supports that one purpose.

It is **not** responsible for deciding *what* a merchant owes—that belongs to the Commerce Architecture.

Instead, it answers:

> **"Now that we know what the merchant owes, how do we collect it securely and reliably?"**

## Scope

The Merchant Payments Architecture is responsible for:

* Managing merchant payment methods.
* Securely connecting payment methods to external providers.
* Executing and recording payment transactions.
* Applying platform-wide commercial promotions during payment calculation.
* Applying merchant-specific commercial adjustments during payment calculation.

In the current SPINE, that corresponds to:

# Merchant Payments Architecture

merchant_payment_methods.go
        │
        ▼
merchant_payment_method_provider_links.go
        │
        ▼
merchant_payments.go
        │
        ▼
platform_commercial_promotions.go
        │
        ▼
merchant_commercial_adjustments.go

## Relationship to Commerce Architecture

I think it's useful to describe the relationship explicitly because it's one of the strongest aspects of the architecture.

### Commerce Architecture

Determines the commercial obligation.

Examples:

* What fees apply?
* Is the merchant on a plan?
* Is there subscription billing?
* What is the invoice amount?
* What should be recorded in the billing ledger?

### Merchant Payments Architecture

Fulfills the commercial obligation.

Examples:

* Which payment method is used?
* Is the bank account verified?
* Is the payment provider connected?
* Did the payment succeed?
* What payment transaction was recorded?

One determines the debt.

The other settles it.

## What it is not

Just as importantly, Merchant Payments Architecture is **not**:

* a treasury system,
* a banking platform,
* an escrow service,
* a deposit management system,
* an embedded finance platform.

Those were consciously removed to keep Sagrenti focused on its core product.

## One sentence

If I were writing the introductory paragraph for the governing document, I'd use something like this:

> **Merchant Payments Architecture defines the platform capabilities required to securely collect payment for Sagrenti services. It manages merchant payment methods, external payment provider connectivity, payment execution, and commercial adjustments while remaining independent of the Commerce Architecture, which determines what the merchant owes.**

I like this definition because it cleanly separates **commercial decision-making** (Commerce Architecture) from **payment execution** (Merchant Payments Architecture). That separation is one of the strongest architectural decisions we've made, and it will make the platform easier to evolve over time.

>=======================================================================

# Final doctrine

> **The Future Offering belongs to the merchant. Market Anticipation Intelligence is the Sagrenti SaaS product.**

Accordingly:

> **The Platform does not charge Future Offering Fee. It charges Anticipation Intelligence Fee.**

That is not merely a naming improvement. It correctly identifies ownership, product value, and the basis of the merchant’s commercial obligation.



Future Offering submitted
        │
        ▼
Anticipation Intelligence Activation invoice
        │
        ▼
Payment collected
        │
        ▼
Future Offering activated

## Recommended terminology

The commercial fees become:

Anticipation Intelligence Activation Fee
Anticipation Intelligence Fee
Subscription Fee             optional
Campaign Performance Fee     Launch Campaign only

The payment flow becomes:

At submission:
Anticipation Intelligence Activation Fee is invoiced and collected.

Each billing cycle:
Anticipation Intelligence Fee is invoiced and collected.

Anticipation Intelligence Activation Fee
    One-time fee when a Future Offering is submitted and activated.

Anticipation Intelligence Fee
    Recurring fee for ongoing measurement, analytics, reporting,
    insights, and recommendations.


Then the lifecycle is coherent:

Future Offering submitted
        │
        ▼
Anticipation Intelligence Activation Fee
        │
        ▼
Measurement begins
        │
        ▼
Monthly Anticipation Intelligence Fee
        │
        ▼
Monthly Anticipation Intelligence Report


## Anticipation Intelligence Activation Fee

Charged once when a merchant activates a new Future Offering on the platform.

What it pays for:

* activating Market Anticipation Intelligence for that Future Offering;
* initializing anticipation measurement;
* provisioning the analytics pipeline;
* enabling reporting;
* beginning recommendation generation.

In other words, it activates the SaaS capability for that specific Future Offering.

---

## Anticipation Intelligence Fee

Charged each billing cycle while the Future Offering remains active.

What it pays for:

* continued anticipation measurement;
* ongoing analytics;
* anticipation intelligence reports;
* insights;
* recommendations.

---

That gives us a very clean commercial lifecycle:

```text
Merchant creates Future Offering
        │
        ▼
Anticipation Intelligence Activation Fee
        │
        ▼
Future Offering becomes active
        │
        ▼
Platform measures anticipation
        │
        ▼
Monthly Anticipation Intelligence Report
        │
        ▼
Monthly Anticipation Intelligence Fee
        │
        ▼
Repeat until offering closes
```

I also like the language from a marketing perspective.

The merchant isn't paying to "register" a Future Offering.

They're **activating intelligence** for it.

That's much closer to the value proposition.

---

### I would make one small terminology adjustment

I'd avoid saying:

> *"...activates Anticipation Intelligence..."*

because Anticipation Intelligence is really the product.

I'd describe it as:

> **Activates Anticipation Intelligence services for a Future Offering.**

or

> **Activates the Anticipation Intelligence lifecycle for the Future Offering.**

That subtly reinforces that the merchant already owns the Future Offering; Sagrenti is activating its own services around it.

---

## I think we've now arrived at a very coherent fee taxonomy

```text
Anticipation Intelligence Activation Fee
    One-time fee charged when a Future Offering is activated.

Anticipation Intelligence Fee
    Recurring fee charged while Anticipation Intelligence services
    continue to operate for that Future Offering.

Subscription Fee
    Optional fee when Plans and Subscriptions are enabled.

Campaign Performance Fee
    Applies only to Launch Campaign services where applicable.
```

Notice something elegant about this.

Every fee now answers the question:

> **"What Sagrenti service is the merchant paying for?"**

rather than:

> **"What merchant asset or activity is being charged?"**

That's a subtle but important shift. It keeps ownership clear:

* The **Future Offering** belongs to the merchant.
* The **Market Anticipation Intelligence services** belong to Sagrenti.

The merchant pays for the latter, never for the former. I think that's the cleanest commercial model we've developed so far.




I think this is actually a more powerful capability than it first appears.

What we've built is **commercial flexibility** without changing the platform architecture.

The key insight is this:

> **Plans are a commercial policy, not an architectural dependency.**

That's a very clean separation.

Instead of:

```text
Merchant
    │
    ▼
Must choose a Plan
    │
    ▼
Can use Future Offering
```

the architecture becomes:

```text
Merchant
    │
    ▼
Platform capabilities
    │
    ├── Future Offering
    └── Launch Campaign
```

Then Admin decides whether Plans participate in the commercial model.

## Mode 1 — Plans Disabled

```text
Plans = Disabled
```

Every merchant can use:

* Future Offering
* Launch Campaign

No merchant is required to belong to:

* Pay-As-You-Go
* Standard
* Premium
* Enterprise

The merchant simply pays the applicable fees for the services they actually use.

This is the model we've been evolving toward, and it resembles cloud platforms:

> The account grants access; the usage creates commercial obligations.

---

## Mode 2 — Plans Enabled

```text
Plans = Enabled
```

Now the same platform can operate differently.

Admin can configure any number of plans, for example:

* Starter
* Standard
* Premium
* Enterprise
* Partner
* Founding Merchant

Each plan can define:

* available capabilities,
* included services,
* pricing,
* discounts,
* fee waivers,
* entitlements.

Nothing in the engineering architecture changes.

Only commercial policy changes.

---

## Why I like this

This preserves something we've worked hard to achieve over the past few weeks:

**Capabilities are independent of pricing.**

Future Offering is a platform capability.

Launch Campaign is a platform capability.

Plans are merely one way of commercializing those capabilities.

That's much cleaner than making capabilities depend on plans.

## I would express the doctrine like this

> **Merchant Plans are optional commercial packaging of platform capabilities. They are not required for platform operation.**

That's an important sentence because it explains why we invested in building the Plans subsystem even though we may launch with it disabled.

## The Admin Console becomes very powerful

Admin can choose between two commercial strategies without redeploying software.

### Strategy A — Open Platform

Plans
    Disabled

Subscriptions
    Disabled

Merchant pays only:

* Anticipation Intelligence Activation Fee
* Anticipation Intelligence Fee
* other applicable usage fees

### Strategy B — Packaged Platform

Plans
    Enabled

Subscriptions
    Enabled

Now the same capabilities can be bundled into commercial offerings.


I actually think there's an architectural principle hiding here.

It isn't:

> "Plans can be enabled or disabled."

It's broader:

> **Commercial policy is configuration, not code.**

That principle already fits many of the decisions we've made:

* Plans can be enabled or disabled.
* Subscriptions can be enabled or disabled.
* Anticipation Intelligence Activation Fees can be enabled, disabled, or rebated.
* Anticipation Intelligence Fees can be adjusted through fee schedules.
* Capabilities remain the same; only the commercial rules change.

That's a remarkably flexible architecture. It lets Sagrenti experiment with business models over time without changing the core platform. The software remains focused on delivering Future Offering and Launch Campaign capabilities, while the Admin Console controls **how** those capabilities are commercialized.

We had agreed that **Plaid should not have its own domain file**.

Instead, it belongs as the implementation of the **bank connectivity** portion of the **Merchant Payments Architecture**.

The file we discussed was:

`merchant_payment_method_provider_links.go`

The reasoning was:

* `merchant_payment_methods.go` defines **what** payment methods a merchant has (e.g., bank account, debit card).
* `merchant_payment_method_provider_links.go` defines **how those methods are connected to external providers** such as Plaid.

For example:

Merchant Payment Method
        │
        ▼
Provider Link
        │
        ├── Plaid
        ├── Stripe Financial Connections (future)
        ├── MX (future)
        └── Other providers

That keeps the architecture vendor-neutral. The platform knows about a **provider link**, not about Plaid specifically.

### Responsibilities of `merchant_payment_method_provider_links.go`

This file would typically store information such as:

* Merchant payment method ID
* Provider (e.g., Plaid)
* Provider account/item identifier
* Connection status
* Verification status
* Linked account metadata
* Last synchronization time
* Connection timestamps
* Disconnect/revocation information

Notice that it **does not process payments**. It simply manages the secure relationship between a merchant payment method and an external connectivity provider.

Then:

* `merchant_payment_methods.go` → defines the merchant's payment methods.
* `merchant_payment_method_provider_links.go` → links those methods to providers like Plaid.
* `merchant_payments.go` → initiates and records payment transactions using verified payment methods.

I still think that's the cleanest separation of responsibilities because it allows us to replace Plaid with another provider—or support multiple providers simultaneously—without changing the core payment method or payment transaction models.
