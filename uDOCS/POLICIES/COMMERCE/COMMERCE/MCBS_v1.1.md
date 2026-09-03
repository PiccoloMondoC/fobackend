# Merchant Commercial & Billing Strategy

## MCBS v1.1

### 1. Purpose

The Merchant Commercial & Billing Strategy defines the durable commercial doctrine governing how Sagrenti creates, measures, bills, and collects merchant value.

Sagrenti is fundamentally a **Future Offering Platform supported by a Monetization Layer**.

Its principal merchant product is **Anticipation Intelligence**: measurable intelligence about consumer anticipation before commercial commitment or availability.

Sagrenti does not sell clicks, advertising impressions, or individual engagement actions. Engagement activity produces intelligence; the intelligence is the merchant value.

---

### 2. Commercial Doctrine

The merchant commercial model follows these principles:

* charge for identifiable merchant value;
* preserve the distinction between platform access, anticipation intelligence, and measurable campaign performance;
* maintain explicit and auditable billing records;
* separate engineering capability from commercial and operational policy;
* allow commercial policy to evolve through Administration without redesigning the underlying architecture;
* preserve merchant and consumer trust while monetizing intelligence.

Engineering implements the complete commercial capability and its invariant safety, security, integrity, correctness, and auditability boundaries.

Administration governs commercial and operational behavior through configuration within those boundaries.

---

### 3. Merchant Offering

Sagrenti supports merchants creating and operating **Future Offerings** across products, services, events, venues, developments, experiences, and other supported offering categories.

A Future Offering allows a merchant to measure market anticipation before or during the transition toward commercial availability.

The merchant may make supported engagement opportunities available through the Future Offering Activation process.

**Watch is platform-owned.** It is not a merchant-configurable engagement option.

Merchant-configurable engagement opportunities may include:

* Waitlist
* Early Access Request
* Beta
* Reservation Interest
* Preorder Intent

The detailed merchant offering catalogue and capability definitions belong to the governing Merchant Offering architecture rather than MCBS.

---

### 4. Merchant Plans

The commercial architecture supports administratively governed merchant plans, including:

* Pay-As-You-Go
* Standard
* Premium
* Enterprise

Plans define commercial packaging, entitlements, pricing relationships, and other configurable merchant terms.

No particular plan structure, price, billing interval, entitlement combination, or promotional treatment is an engineering invariant.

Administration may enable, disable, introduce, or revise commercial configurations within the capabilities and safe boundaries implemented by Engineering.

---

### 5. Canonical Fee Types

Sagrenti recognizes four principal merchant fee categories:

**Activation Fee**
Prices activation of a merchant Future Offering or other supported commercial capability.

**Anticipation Intelligence Fee**
Prices measurable Anticipation Intelligence produced by the platform.

**Campaign Performance Fee**
Prices measurable performance associated with supported Launch Campaign activity.

**Subscription Fee**
An optional recurring access charge where the governing merchant plan requires one.

These fee categories express distinct forms of merchant value and must remain distinguishable throughout billing and financial records.

Their prices, applicability, timing, eligibility, thresholds, grace periods, and other commercial terms are administrative policy unless required by an engineering invariant.

---

### 6. Anticipation Intelligence and QAE

The canonical usage unit for Anticipation Intelligence is the **Qualified Anticipation Entry (QAE)**.

A QAE represents one qualifying participant associated with one Future Offering.

Consumer engagement types are intelligence dimensions, not independent billing units to be summed together.

A consumer who Watches, joins a Waitlist, requests Early Access, enters Beta, expresses Reservation Interest, and records Preorder Intent for the same Future Offering does not thereby become six independently billable anticipation participants.

The billing architecture must prevent engagement-level double counting while preserving the underlying engagement detail for intelligence, analytics, and reporting.

---

### 7. Merchant Invoicing

Merchant obligations are presented through merchant invoices, including the **Anticipation Intelligence Invoice** for Anticipation Intelligence charges.

Invoices represent financial obligations, while detailed engagement and intelligence information remains available through the appropriate merchant reporting surfaces.

An invoice must preserve monetary truth, including:

* merchant;
* currency;
* charges;
* credits and adjustments;
* amount due;
* payments;
* lifecycle state;
* relevant timestamps;
* underlying billing provenance.

Once an invoice is issued, its monetary identity and historical truth must not be silently rewritten.

Commercial matters such as payment terms, grace periods, collection timing, and similar operating rules remain administratively configurable where system safety and financial integrity permit.

---

### 8. Platform Credits

Platform Credits allow Sagrenti to reduce a merchant's financial obligation before payment.

They are not merchant deposits, stored merchant money, bank balances, or cash equivalents.

Credits must be represented explicitly and audibly so that the original charge and the credit applied against it remain distinguishable.

The architecture supports configurable credit eligibility by fee type.

Which fees are eligible, the amount of credit granted, expiration, promotional programs, founding-merchant treatment, and similar commercial decisions are administrative policy rather than permanent engineering rules.

---

### 9. Billing and Monetary Truth

The Monetization Layer must preserve the progression from commercial activity to financial settlement.

Conceptually:

**Billable Event → Fee Calculation → Credit Application → Invoice → Payment**

Each stage has a distinct responsibility.

The system must preserve sufficient provenance to determine:

* what commercial event occurred;
* why it was billable;
* how the charge was calculated;
* what credits were applied;
* what obligation was invoiced;
* what payment satisfied that obligation;
* what subsequent reversal, correction, or adjustment occurred.

Financial history must remain auditable. Corrections must preserve historical truth rather than erase it.

---

### 10. Payment and Currency

Sagrenti supports Direct Payment collection as the initial operating model.

Supported payment capabilities may include ACH, debit card, credit card, wire, manual transfer, and other approved mechanisms as the platform evolves.

Merchant geography does not determine an immutable account currency.

A merchant may conduct Future Offering activity across markets and currencies.

Each invoice, however, has a defined currency, and its monetary amounts and settlement must remain internally consistent with that currency.

Currency conversion, payment-provider selection, payment-method requirements, and similar operating decisions are governed separately from the underlying monetary invariants.

---

### 11. Commercial Configuration

Commercial policy is expected to change as Sagrenti learns from actual merchant behavior.

Administration must therefore be capable of governing matters such as:

* pricing;
* fee schedules;
* plans and entitlements;
* billing intervals;
* credit eligibility;
* promotional treatment;
* payment terms;
* grace periods;
* activation requirements;
* thresholds and limits;
* other supported commercial parameters.

Engineering must provide the capability for such configuration without embedding temporary business policy into application logic.

Administrative configuration may not weaken or override engineering invariants protecting correctness, security, data integrity, financial integrity, auditability, or platform reliability.

---

### 12. Commercial Records and Auditability

Sagrenti's commercial architecture must maintain clear separation between:

* merchant commercial activity;
* billable events;
* fee calculations;
* Platform Credits;
* invoices;
* payments;
* subscriptions and subscription periods where applicable;
* administrative configuration;
* audit history.

These records collectively form the financial evidence of the merchant relationship.

Commercial reporting may evolve, but the underlying records must remain capable of explaining how every material merchant obligation arose and how it was resolved.

---

### 13. Relationship to Governing Architecture

MCBS defines **merchant commercial and billing doctrine**.

It does not duplicate detailed platform architecture, merchant offering definitions, Future Offering engagement architecture, monetization implementation contracts, or invoice lifecycle specifications maintained by their respective governing documents.

Where earlier MCBS doctrine conflicts with the subsequently adopted Sagrenti Business Doctrine or Merchant Platform Architecture, the newer governing doctrine prevails.

MCBS should therefore remain concise and change only when Sagrenti's commercial doctrine changes—not whenever an implementation detail changes.

---

### 14. Permanent Commercial Doctrine

Sagrenti sells **Anticipation Intelligence**.

Future Offering engagement generates the evidence from which that intelligence is produced; individual engagement actions are not themselves the product.

The commercial architecture must distinguish Activation, Anticipation Intelligence, Campaign Performance, and optional Subscription value.

QAE is the canonical usage unit for Anticipation Intelligence and must not be inflated by counting multiple engagement types from the same participant as multiple anticipation participants.

Commercial policy belongs to Administration. Engineering provides complete capability and protects the invariants within which that policy operates.

Billing must preserve financial truth from the originating commercial event through calculation, credits, invoicing, payment, adjustment, and audit.

The architecture must be capable of evolving as Sagrenti learns how merchants value Anticipation Intelligence without requiring commercial experimentation to become hard-coded system doctrine.

**That is the governing commercial and billing strategy of Sagrenti.**
