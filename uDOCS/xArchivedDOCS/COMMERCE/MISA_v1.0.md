Yes. I think we have enough clarity now to move into the `merchant_invoice_items` design review without carrying contradictory assumptions forward.

## Merchant Invoice & Settlement Architecture — Current Agreement

### 1. The commercial chain

The core Commerce progression is:

```text
Billable Event
      ↓
Fee Calculation
      ↓
Platform Credit Application, if applicable
      ↓
Invoice Item
      ↓
Invoice
```

The invoice establishes the merchant's resulting commercial obligation.

After that:

```text
Invoice
      ↓
Payment
      ↓
Settlement Adjustment, if applicable
```

The later billing ledger/Statement of Account records the merchant's financial history across invoices, payments and other relevant activity.

---

### 2. `merchant_invoice_items` has a specific, narrow purpose

`merchant_invoice_items` identifies the **products/services being charged on an invoice**.

For example:

| Item                                     | Quantity / QAE | Unit Cost |     Total |
| ---------------------------------------- | -------------: | --------: | --------: |
| Anticipation Intelligence Activation Fee |              1 |    $50.00 |    $50.00 |
| Anticipation Intelligence Fee            |         10,000 |     $0.25 | $2,500.00 |

Thus:

```text
quantity × unit_amount = line_amount
```

has genuine commercial meaning.

For Anticipation Intelligence usage:

```text
10,000 QAE × $0.25 = $2,500
```

For a fixed Activation Fee:

```text
1 × $50 = $50
```

The invoice items are **data used to construct the invoice**. They are not themselves a merchant-facing document.

---

### 3. Only products/services belong in invoice items

This is now an important boundary.

`merchant_invoice_items` should **not** contain:

```text
Discounts
Rebates
Platform Credits
Payments
Early-payment discounts
Other settlement adjustments
```

Those are not products or services being sold.

An invoice item answers only:

> **What product/service was charged, in what quantity, at what unit cost, producing what line amount?**

---

### 4. Commercial rebates and discounts

A marketing rebate or commercial discount belongs to the **commercial calculation**, not settlement.

Example:

```text
Anticipation Intelligence Activation Fee     $10,000
Less 10% Rebate                               -1,000
                                               ------
Net Activation Fee                             9,000
```

The rebate exists because there is an underlying commercial charge. It is therefore associated with that charge rather than becoming another `merchant_invoice_item`.

The fee-calculation/commercial-adjustment architecture preserves the underlying reason for that reduction.

---

### 5. Platform Credits remain pre-invoice

We **do not presently reopen `merchant_platform_credit_applications` for redesign**.

Platform Credits are marketing/commercial instruments. They are:

* not merchant deposits;
* not merchant-held funds;
* not a wallet;
* not cash;
* not payment transactions.

They can therefore remain where the existing architecture places them: reducing an established calculated obligation **before the resulting amount is invoiced**.

Conceptually:

```text
Fee Calculation
      ↓
Platform Credit Application
      ↓
Net Commercial Obligation
      ↓
Invoice Item / Invoice
```

Platform Credit remains its own auditable domain record; we do not collapse it into the fee calculation or manufacture a negative invoice item for it.

---

### 6. Payment is different

Payment occurs against the obligation established by the invoice.

That preserves MPA's fundamental boundary:

> **Commerce determines the obligation. Merchant Payments securely fulfills and records it.**

Therefore:

```text
Commerce                           Merchant Payments

Fee Calculation
      ↓
Invoice Item
      ↓
Invoice ─────────────────────────────→ Payment
```

Payment must not retrospectively redefine what Commerce charged.

---

### 7. Early-payment discount is a settlement adjustment

An early-payment discount is fundamentally different from a marketing rebate.

For terms such as:

> **5% discount if settled within 3 days**

the discount exists **because settlement occurred according to particular payment terms**.

For example:

```text
Invoice                              $35,000

ACH Payment                         -33,250
Early Settlement Discount            -1,750
                                    -------
Remaining obligation                     $0
```

Therefore:

```text
Invoice
   ↓
Payment
   ↓
Settlement Adjustment
      └── Early-payment discount
```

The payment and settlement adjustment are separate financial facts even when they participate together in satisfying the obligation.

---

### 8. Statement of Account is a different concern

The Statement of Account reports financial/account activity over time.

For example:

```text
Opening Balance                         65,000
Invoice 1234                           +35,000
Platform/other applicable credit       -25,000
ACH Payment                            -71,500
Early Settlement Discount               -3,500
                                       -------
Balance                                      0
```

The Statement therefore must not be confused with either the invoice or `merchant_invoice_items`.

Eventually the billing ledger should provide the durable accounting history from which such statements can be produced.

---

### 9. Invoice provenance problem is now much clearer

Our earlier service review discovered that `merchant_invoices` is essentially an **invoice header**.

What was missing was the structural explanation of what produced that invoice.

We now recognize that this was not necessarily evidence that we needed to invent another domain. The already-planned:

```text
merchant_invoice_items
```

is the natural missing layer.

It gives us:

```text
merchant_fee_calculations
          ↓
merchant_invoice_items
          ↓
merchant_invoices
```

and therefore provides the foundation for tracing the actual products/services represented by the invoice.

---

### 10. Existing `merchant_invoice_items` design is conceptually promising

The existing:

```sql
quantity NUMERIC(19,4)
unit_amount NUMERIC(19,4)
line_amount NUMERIC(19,4)

CHECK (quantity > 0)
CHECK (unit_amount >= 0)
CHECK (line_amount >= 0)

CHECK (line_amount = quantity * unit_amount)
```

now makes considerably more sense.

There is no need for negative invoice items because discounts, rebates, credits and settlement adjustments aren't invoice items.

However, the table still deserves a full CE design review—particularly its provenance FK, deletion semantics, relationship to fee calculations, monetary snapshot behavior, and how invoice totals reconcile to their items.

---

## The resulting architecture

We now have a substantially cleaner separation:

```text
BILLING / COMMERCIAL

Billable Event
      ↓
Fee Calculation
      ├── Commercial rebate/discount
      │
      └── Platform Credit Application
                    ↓
              Net Obligation
                    ↓
              Invoice Item
                    ↓
                 Invoice


PAYMENT / SETTLEMENT

                 Invoice
                    ↓
                 Payment
                    ↓
          Settlement Adjustment
          e.g. early-payment discount


ACCOUNTING / REPORTING

             Billing Ledger
                    ↓
          Statement of Account
```

And the governing distinctions are:

> **Invoice items describe what Sagrenti sold.**

> **Commercial adjustments determine what Sagrenti charges.**

> **Platform Credits are Sagrenti commercial/marketing reductions applied before invoicing.**

> **Invoices establish the resulting obligation.**

> **Payments satisfy invoices.**

> **Settlement adjustments arise from the conditions under which payment settles an obligation.**

> **Statements of Account report the resulting financial history.**

With those boundaries established, **yes: I think we are ready to take `merchant_invoice_items` to the next level and review its table as an architectural contract before touching its data layer.**
