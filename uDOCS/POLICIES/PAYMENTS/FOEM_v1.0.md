I would make only a few changes. The architecture itself hasn't changed much; we've mostly sharpened the boundaries and terminology.

---

# 1. Sagrenti's product

Sagrenti is **not** selling clicks, conversions, or advertising.

Sagrenti is selling **Anticipation Intelligence**.

That distinction should influence both the architecture and the commercial model.

---

# 2. Invoice

The merchant receives an **Anticipation Intelligence Invoice**, not a "Watch Invoice" or an invoice itemized by individual engagement types.

The invoice reflects the value delivered by the platform, while the detailed engagement analytics remain available in reporting.

---

# 3. Watch

Watch is fundamentally different from the other engagement actions.

* It is **platform-owned**, not merchant-owned.
* It is **always available** and is not configurable by the merchant.
* It represents the consumer's ongoing anticipation relationship with a Future Offering.
* Entering any merchant engagement action automatically establishes the Watch relationship if it does not already exist.

---

# 4. Merchant engagement options

The merchant controls which engagement opportunities are available for a Future Offering.

Current engagement actions include:

* Waitlist
* Early Access Request
* Beta
* Reservation Interest
* Preorder Intent
* *(Potential future)* Draw Entry

These are configured by the merchant.

Watch is **not** part of merchant configuration.

---

# 5. Merchant configuration

Some concepts are **merchant configuration**, not engagement actions.

Examples include:

* Invite Only
* Public
* *(Potential future)* Approval Required

These determine **who may participate**, not **how consumers participate**.

---

# 6. Merchant release strategy

Some concepts describe **how the merchant releases** the Future Offering.

Examples include:

* Drop
* Scheduled Release
* Rolling Release
* Limited Quantity

These are release characteristics rather than consumer engagement actions.

---

# 7. Consumer intelligence

Consumers who have no engagement with a Future Offering receive no ongoing intelligence about it.

Once a consumer enters the anticipation relationship—either by explicitly Watching or by selecting any engagement action—Watch becomes active and the consumer receives the full intelligence stream for that Future Offering.

---

# 8. Consumer engagement

The consumer journey is evolutionary rather than prescriptive.

Typical progression may be:

```text
Watch
↓
Waitlist
↓
Beta
↓
Reservation Interest
↓
Preorder Intent
```

Consumers may skip stages, enter at different points, or stop at any stage.

---

# 9. Engagement events

Every engagement is recorded as an event for analytics and operational history.

Examples include:

* Watch
* Waitlist
* Early Access Request
* Beta
* Reservation Interest
* Preorder Intent
* *(Future)* Draw Entry

---

# 10. Billing principle

The billable unit is **not** every engagement event.

The agreed principle is:

> **One qualified anticipation entry per participant per Future Offering.**

A participant is billed once when they first enter the anticipation relationship for that Future Offering.

Subsequent engagement actions enrich analytics but do not create additional billable units.

---

# 11. Double counting

This approach prevents double charging.

Example:

```text
Watch
Waitlist
Beta
Reservation Interest
```

produces:

* One Qualified Anticipation Entry.
* Four engagement events.

The merchant is charged once while still receiving complete engagement analytics.

---

# 12. Merchant billing metric

Merchant usage charges are based on the number of **Qualified Anticipation Entries** created during the billing period.

This is **not**:

* total consumers currently Watching,
* total Waitlists,
* total Betas,
* or the sum of all engagement events.

It is the count of participants who **newly entered anticipation** during the billing period.

---

# 13. Merchant teams

Merchant teams are an administrative capability rather than a commercial billing unit.

The current conclusions are:

* Multi-user merchant accounts should not automatically incur higher activation or usage fees.
* Merchants are charged for the anticipation intelligence Sagrenti delivers, not for the number of employees with platform access.
* Enterprise administration and collaboration capabilities (for example, SSO, advanced workflows, compliance, or governance) may justify higher-tier plans in the future because they provide additional value.

---

# 14. Architectural separation

The platform now has four distinct architectural concerns:

* **Consumer Engagement** — what consumers do.
* **Merchant Configuration** — who may participate.
* **Merchant Release Strategy** — how the Future Offering is released.
* **Analytics & Billing** — what Sagrenti measures and what it charges for.

Each concern evolves independently.

---

## One final observation

The biggest architectural change is actually the language.

Earlier, we talked about **Watch, Waitlist, Beta, Reservation Interest,** etc. almost interchangeably. Now they've naturally separated into four categories:

* **Consumer Engagement Actions** (Watch, Waitlist, Beta, ...)
* **Merchant Access Policies** (Invite Only, Public, ...)
* **Merchant Release Strategies** (Drop, Scheduled Release, ...)
* **Commercial Billing** (Activation and Qualified Anticipation Entry)

That separation is valuable because each category answers a different question:

* **What can the consumer do?**
* **Who is allowed to participate?**
* **How is the offering released?**
* **What does Sagrenti charge for?**

I think that's one of the clearest architectural refinements we've made to the Future Offering model.
