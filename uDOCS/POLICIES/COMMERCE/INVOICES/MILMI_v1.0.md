## Merchant Invoice Lifecycle & Monetary Invariants — v1

### 1. Lifecycle

Canonical states:

```text
draft
  ↓
issued
  ├──────────────→ paid
  ├→ partially_paid → paid
  ├→ overdue ──────→ paid
  │       └────────→ partially_paid → paid
  └→ void
```

The service layer owns legal transitions. The database guarantees that whatever status is persisted is internally consistent.

Key rules:

* `draft` — invoice is being constructed and has no financial settlement.
* `issued` — invoice has formally become a merchant obligation.
* `partially_paid` — some, but not all, of the invoice has been satisfied.
* `overdue` — an unpaid balance remains beyond its configured due point.
* `paid` — the entire invoice has been satisfied.
* `void` — an issued but unpaid invoice has been invalidated.
* `paid` and `void` are terminal v1 states.
* No backward lifecycle transitions.
* A partially paid invoice cannot be voided. Future correction/refund/credit capabilities handle that accounting situation.

### 2. Monetary invariants

Always:

```text
subtotal_amount >= 0

total_amount =
    subtotal_amount + adjustment_amount

total_amount >= 0

0 <= amount_paid <= total_amount
```

`adjustment_amount` may be positive, zero, or negative.

State-specific:

```text
draft            amount_paid = 0
issued           amount_paid = 0
partially_paid   0 < amount_paid < total_amount
overdue          amount_paid < total_amount
paid             amount_paid = total_amount
void             amount_paid = 0
```

No overpayment is representable on an invoice.

### 3. Currency

An invoice contains exactly **one currency**.

Merchant domicile does **not** determine invoice currency.

A US merchant can incur GBP billing for a UK-market Future Offering; a Japanese merchant can incur USD billing for a US-market Future Offering.

The appropriate billing context/configuration determines currency.

Once an invoice is **issued**, its currency is immutable.

We should consequently never combine monetary obligations denominated in different currencies into one invoice.

### 4. Issuance and due dates

`issued_at` records when the draft becomes an actual invoice.

`due_at` represents the commercially configured payment deadline.

Engineering **does not decide the payment term**.

Administration may configure, for example:

```text
Activation Fee                   → due immediately
Anticipation Intelligence Invoice → configured grace period
```

Those are examples of policy, not hard-coded rules.

Invariant:

```text
due_at IS NULL
OR
due_at >= issued_at
```

If an invoice is classified `overdue`, however, it must have a `due_at`.

### 5. Timestamp integrity

The timestamps describe facts, not merely optional metadata:

* `draft` → no issuance/payment/void timestamps.
* `issued` → `issued_at` required.
* `partially_paid` → `issued_at` required.
* `overdue` → `issued_at` and `due_at` required.
* `paid` → `issued_at` and `paid_at` required.
* `void` → `issued_at` and `voided_at` required.

And:

```text
paid_at >= issued_at
voided_at >= issued_at
```

### 6. DB versus service responsibility

This is an important architectural boundary.

**Database invariants** protect facts that must never be false: monetary equations, valid status values, payment bounds, timestamp/status consistency, currency format, and referential integrity.

**Service orchestration** owns behavior: legal status transitions, issuance, applying payments, determining when an invoice becomes overdue, voiding, obtaining configured payment terms/currency, and coordinating related billing records.

**Administration** owns policy: grace periods, payment terms, applicable currencies/defaults, fee behavior, and other configurable commercial decisions.

### 7. Historical integrity

Invoices are durable financial records.

Merchant account cancellation does **not** delete invoices. Physical deletion of the merchant must be prevented where financial history depends upon it.

Therefore:

```sql
merchant_id UUID NOT NULL
    REFERENCES merchants(id) ON DELETE RESTRICT
```

remains the correct relationship.

---

**CE decision:** I would consider the **merchant invoice lifecycle and monetary invariants LOCKED for v1** on this basis.

That gives us a sufficiently precise contract to move to the next box:

**[ ] Build/review `merchant_invoices.go` data layer.**
