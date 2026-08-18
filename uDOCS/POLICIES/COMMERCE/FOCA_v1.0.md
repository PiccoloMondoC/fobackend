# Merchant Payments Architecture (MPA) v1.0

## 1. Purpose

Merchant Payments Architecture defines the platform capabilities required to securely collect and record payment for Sagrenti services.

Its governing boundary is:

> **Commerce determines the obligation. Merchant Payments securely fulfills and records it.**

MPA does **not** determine what a merchant owes. That responsibility belongs to the Commerce Architecture. MPA begins once a valid commercial obligation exists.

---

## 2. Architectural Responsibilities

Merchant Payments Architecture is responsible for:

* managing merchant payment methods;
* securely connecting payment methods to external providers;
* validating the operational readiness of payment methods;
* initiating payment against established merchant obligations;
* recording payment transactions and their outcomes;
* supporting commercial promotions and merchant-specific adjustments where they participate in payment calculation;
* maintaining the integrity, auditability, and reliability of payment execution.

MPA does not independently establish fees, billing obligations, invoice amounts, subscription obligations, or billable events.

---

## 3. Relationship to Commerce Architecture

The two architectures form separate but complementary stages of the commercial lifecycle.

### Commerce Architecture

Commerce establishes and records the merchant's commercial obligation.

It answers questions such as:

* What fee applies?
* What billable event occurred?
* What fee calculation results?
* Does a Platform Credit reduce the obligation?
* What amount should be invoiced?
* What does the merchant owe?

### Merchant Payments Architecture

MPA fulfills the resulting obligation.

It answers questions such as:

* Which payment method will be used?
* Is that payment method usable and appropriately connected?
* Which external provider participates in the transaction?
* Was collection attempted?
* Did the payment succeed or fail?
* What payment transaction must be recorded?

Therefore:

> **An obligation may exist independently of payment, and payment must not redefine the obligation it is satisfying.**

---

## 4. Direct Payment Mode — SPINE v1

MPA v1 implements **Direct Payment Mode**.

The merchant's established obligation is collected through an authorized payment method rather than satisfied from merchant-held platform funds.

The current MPA SPINE is:

```text
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
```

These components form the payment capability from merchant payment-method ownership through provider connectivity and payment execution, including applicable payment-stage commercial adjustments.

---

## 5. Payment Methods

`merchant_payment_methods.go` represents the payment methods a merchant has authorized for use with the platform.

A payment method is a Sagrenti domain object. External payment providers are implementation dependencies and must not define the platform's payment-domain model.

The architecture must support the payment capabilities required by the platform without coupling the merchant account or commercial obligation to a particular provider.

---

## 6. Provider Connectivity

`merchant_payment_method_provider_links.go` represents the relationship between a merchant payment method and an external connectivity or payment provider.

This separation is intentional:

```text
Merchant Payment Method
        │
        ▼
Provider Link
        │
        ├── Plaid
        ├── another provider
        └── future providers
```

Plaid therefore does **not** constitute its own Sagrenti architectural domain.

Provider links may represent provider identity, external references, connection and verification state, relevant metadata, synchronization state, and disconnection or revocation information. They do not themselves represent payment transactions.

This boundary allows providers to be replaced, supplemented, or used concurrently without redesigning the core payment-method model.

---

## 7. Payments

`merchant_payments.go` owns payment execution and the durable record of payment transactions.

A payment must be traceable to the commercial obligation it is intended to satisfy and to the authorized payment capability used to execute it.

Payment execution must preserve sufficient state to determine the outcome of an attempted transaction without rewriting the underlying commercial obligation.

Provider failures, retries, duplicate callbacks, delayed responses, or other operational conditions must not create duplicate settlement or corrupt payment state.

---

## 8. Commercial Adjustments at Payment

`platform_commercial_promotions.go` and `merchant_commercial_adjustments.go` provide controlled mechanisms for commercial adjustments that participate in payment calculation.

Their presence does not transfer ownership of commercial policy to MPA.

Engineering supplies the capability and enforces its invariants. Administration determines the applicable commercial behavior through configuration within those boundaries.

MPA must not embed hard-coded promotional or merchant-specific commercial policy merely because the adjustment is applied during payment.

---

## 9. Engineering Invariants

MPA must preserve the following architectural invariants:

1. **Payment does not create or redefine the underlying commercial obligation.**
2. **A payment transaction must be attributable to the obligation it is intended to satisfy.**
3. **Money and currency must be handled explicitly and consistently across the payment lifecycle.**
4. **Payment execution must be idempotent wherever retries or repeated provider communication can occur.**
5. **External provider identifiers must not replace Sagrenti's own domain identity.**
6. **Provider-specific implementation details must remain behind provider-neutral platform boundaries.**
7. **Payment state transitions must preserve historical and audit integrity.**
8. **Successful settlement must not be inferred merely from an attempted provider operation.**
9. **Engineering safeguards protecting correctness, security, integrity, auditability, and reliability cannot be weakened by administrative configuration.**

These are engineering constraints, not commercial policy.

---

## 10. Administration Boundary

Consistent with Sagrenti's governing engineering doctrine:

> **Engineering implements complete payment capabilities and their safe operating boundaries. Administration governs commercial and operational behavior through configuration within those boundaries.**

Accordingly, configurable matters may include provider selection, enabled payment capabilities, commercial adjustments, operational thresholds, and other legitimate payment policies where the architecture provides such configuration.

Administration must not be able to configure away an engineering invariant.

---

## 11. Explicitly Outside MPA

Merchant Payments Architecture is not:

* the Commerce Architecture;
* a merchant billing ledger;
* a fee-definition system;
* a treasury system;
* a banking platform;
* an escrow service;
* a deposit-management system;
* a merchant wallet;
* an embedded-finance platform.

Treasury Balance Mode and merchant-held platform funds are outside the Direct Payment Mode SPINE and are not prerequisites for MPA v1.

The deliberate exclusion of treasury, banking, escrow, deposit management, and embedded finance keeps MPA focused on collecting payment for Sagrenti services.

---

## 12. Architectural Doctrine

Merchant Payments Architecture should remain narrow.

Commerce may evolve new fee types, pricing models, billing schedules, subscriptions, credits, or other commercial mechanisms without requiring MPA to become the owner of those concepts.

Likewise, payment providers may change without requiring Commerce to understand provider-specific implementation.

The durable separation is:

```text
Commerce Architecture
        │
        │ establishes obligation
        ▼
Anticipation Intelligence Invoice
        │
        │ requires settlement
        ▼
Merchant Payments Architecture
        │
        │ executes collection
        ▼
Payment Transaction
```

> **Commerce determines what the merchant owes. Merchant Payments securely fulfills and records it.**

That boundary is the foundation of MPA v1.0.
