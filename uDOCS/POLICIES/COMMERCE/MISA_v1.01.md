### Merchant Invoice & Settlement Architecture

MISA v1.1

Consolidated CE Governing Agreement

MISA governs how an established Sagrenti commercial charge becomes an
invoice obligation, how that obligation is composed and preserved, and
how payment, settlement activity, the Billing Ledger, and the Statement
of Account relate to it.

Governing Question

How does an established Sagrenti commercial charge become an invoice
obligation, and how is that obligation preserved, presented and
settled?

Source Reconciliation

This version consolidates the Merchant Invoice & Settlement current
agreement, the locked invoice lifecycle and monetary invariants, the
QAE/activation/subscription invoice-item decisions, the invoice and
monetary-effect portions of the Merchant Plans & Pricing / FO commercial
architecture, and the later Merchant Invoice Composition Architecture.

Where earlier assumptions conflict, the later Invoice Composition
Architecture controls invoice composition. The established lifecycle
remains locked except where the obsolete generic adjustment_amount
equation is necessarily replaced by the newer composition model.

# 1. Architectural Boundary

BILLING / COMMERCE

Billable Event ↓ Fee Calculation ↓ Commercial monetary effects, if
applicable ↓ Invoice Item ↓ Invoice

PAYMENT / SETTLEMENT

Invoice ↓ Payment ↓ Settlement Adjustment, if applicable

ACCOUNTING / REPORTING

Billing Ledger ↓ Statement of Account

Commerce determines the obligation. Merchant Payments securely fulfills
and records it. Payment does not retrospectively redefine the commercial
charges that produced the invoice.

2. Invoice Identity

A Future Offering (FO) is an independent merchant commercial project.
Accordingly:

One invoice = one merchant + one Future Offering + one currency.

Charges from different FOs must not be combined on the same invoice.

future_offering_id is part of the invoice's commercial identity, not
merely reporting metadata.

A merchant may have many invoices for one FO over that FO's
commercial lifecycle.

Merchant domicile does not determine invoice currency.

Currency is singular per invoice and immutable after issuance.

3. Commercial Charge and Fee-Calculation Provenance

Every invoice obligation must remain traceable to the commercial facts
that created it. The normal provenance chain is:

Future Offering ↓ Billable Event ↓ Fee Calculation ↓ Invoice Item ↓
Invoice

The billable event answers why Sagrenti is entitled to calculate a
charge; the fee calculation answers how much that charge is under
applicable commercial terms; the invoice item snapshots the resulting
product/service charge for invoicing.

4. Invoice-Item Definition and Cardinality

An invoice item is one independently calculated Sagrenti product/service
charge represented by one line on an invoice. Invoice items are
construction data for the invoice; they are not themselves
merchant-facing documents.

1 Invoice ↓ many Invoice Items

1 Invoice Item ↓ exactly 1 Fee Calculation

1 Fee Calculation ↓ at most 1 Invoice Item

The direct fee_calculation_id relationship is therefore canonical; no
association table is required.

Typical quantity/unit-cost semantics remain meaningful:

Anticipation Intelligence Fee 10,000 QAE × $0.25 = $2,500

Activation Fee 1 × configured fee = line charge

5. What Belongs in Invoice Items

Invoice items describe what Sagrenti sold.

| Belongs in merchant_invoice_items | Does not become an independent
invoice item |

| --- | --- |

| Activation Fee | Platform Credit |

| Subscription Fee | Rebate |

| Asset Hosting Overage Fee | Discount |

| Anticipation Intelligence Fee | Tax or surcharge merely because it is
displayed |

| Other Sagrenti product/service charges | Payment or settlement
adjustment |

6. Engineering Identity vs. Merchant-Facing Language

Engineering owns stable domain semantics, provenance, arithmetic
integrity, lifecycle rules, currency integrity, auditability, and safe
configuration boundaries. Administration owns legitimate commercial
configuration and human-facing terminology within those boundaries.

Canonical fee types remain stable engineering identities.

Admin may configure merchant-facing invoice labels and presentation
terminology.

Frontend and document composition should resolve configured
terminology at runtime wherever practical.

Issued invoice descriptions must be snapshotted so later terminology
changes cannot rewrite historical invoices.

Presentation labels such as Items Subtotal, Pre-tax Total, Taxes &
Surcharges, and Invoice Total are not unnecessarily hard-coded
commercial vocabulary.

7. Line Pricing and Charge-Attributable Monetary Effects

Monetary effects attributable to a particular commercial charge remain
attributable to that charge. They must not falsify the original
quantity, unit price, or gross calculated charge.

gross product/service charge ↓ directly attributable rebate / discount ↓
Platform Credit application, if applicable ↓ net contribution to invoice
composition

The authoritative records preserve which FO, which fee calculation, what
monetary effect, how much, why, and---where applicable---who authorized
it.

8. Platform Credits

Platform Credits are commercial/marketing instruments, not cash,
deposits, merchant-held funds, wallet balances, payments, or settlement
transactions.

Platform Credit eligibility and application mechanics are governed
by MCPCA, not MISA.

MISA owns how an already-established Platform Credit is attributed
and represented during invoice composition.

A Platform Credit remains associated with the eligible fee
calculation it reduces.

It must not become a fake negative merchant_invoice_item.

Charge-associated presentation is preferred because it shows the
merchant which charge received the credit.

No generic invoice-header Platform Credit field should be introduced
merely to reproduce a summary presentation.

9. Invoice-Level Monetary Effects

Not every monetary effect must belong to an individual product/service
line. The architecture must also support legitimate shared effects that
arise from the FO invoice/project as a whole. Such effects must retain
their own economic identity and provenance rather than being forced onto
an arbitrary line.

This capability does not imply that Sagrenti currently has a particular
shared charge. It prevents the architecture from incorrectly assuming
that every future monetary effect must be line-attributable.

10. Taxes and Surcharges

Taxes and surcharges must be preserved at the lowest commercial grain
that actually gives rise to them. Where a tax or surcharge is
attributable to a specific FO charge, its authoritative provenance
remains with that charge. Legitimate invoice-level tax or surcharge
effects may be supported when their economic basis genuinely applies at
that level.

The invoice may aggregate tax and surcharge amounts for presentation,
but an invoice-wide total must not erase the underlying jurisdictional,
exemption, fee, or FO provenance.

11. Canonical Invoice Composition

The general composition model is:

PRODUCT / SERVICE LINES gross calculated charge ± directly attributable
commercial effects ± attributable Platform Credit = net line
contribution │ ▼ ITEMS SUBTOTAL │ │ ± legitimate shared invoice-level
effects ▼ PRE-TAX TOTAL │ │ + Taxes & Surcharges ▼ INVOICE TOTAL

The architecture supplies these capabilities without requiring every
invoice to contain every layer.

12. Rejection of Generic adjustment_amount

The earlier invoice-header equation subtotal_amount + adjustment_amount
= total_amount is superseded. A generic signed adjustment destroys the
economic meaning of credits, rebates, discounts, shared charges, taxes,
and surcharges and can duplicate normalized underlying records.

MISA therefore rejects adjustment_amount as an independent catch-all
financial input. Invoice totals must be composed from economically
meaningful underlying obligations and effects.

13. Canonical Monetary Totals

MISA v1.1 establishes the semantic role of the invoice totals even where
the final persistence choice remains a data-layer design decision:

| Concept | Meaning |

| --- | --- |

| items_subtotal | Aggregate net contribution of product/service lines
after line-attributable effects. |

| pre_tax_total | Items subtotal after legitimate shared invoice-level
pre-tax effects. |

| taxes_and_surcharges | Aggregate taxes and surcharges included in the
invoice total, without replacing detailed provenance. |

| total | Final invoice obligation before settlement. |

| amount_paid | Amount of the issued invoice obligation satisfied by
payment. |

Whether each aggregate must be persisted on merchant_invoices or safely
derived from authoritative underlying records should be settled in the
merchant_invoices data-contract review. The semantic meanings above are
fixed.

14. Activation Invoice vs. Periodic Invoicing

The Activation Fee arises from activation of the FO, but Engineering
must not equate the existence of that fee with universal prepayment.

Merchant without authorized payment terms Activation Request ↓
Activation Invoice ↓ Immediate Payment ↓ Authorized activation

Merchant with authorized invoiced/deferred terms Activation Fee
calculated ↓ Authorized deferred treatment ↓ FO activated ↓ Activation
Fee included on appropriate periodic invoice ↓ Settlement under
configured terms

An Activation Fee settled through an Activation Invoice must not later
reappear on a periodic invoice. Qualification for payment terms is
Administration policy. Engineering supports both commercial dispositions
without falsely recording payment when none occurred.

15. Invoice Lifecycle --- Locked v1

draft ↓ issued ├──────────────→ paid ├→ partially_paid → paid ├→ overdue
──────→ paid │ └────────→ partially_paid → paid └→ void

draft: under construction; no settlement has occurred.

issued: formally established merchant obligation.

partially_paid: some, but not all, of the invoice has been
satisfied.

overdue: unpaid balance remains beyond the configured due point.

paid: the entire invoice has been satisfied.

void: an issued but unpaid invoice has been invalidated.

paid and void are terminal v1 states.

No backward lifecycle transitions.

A partially paid invoice cannot be voided in v1.

16. Monetary and Currency Invariants

All canonical invoice totals are non-negative.

0 <= amount_paid <= total.

No overpayment is represented on an invoice.

draft, issued, and void require amount_paid = 0.

partially_paid requires 0 < amount_paid < total.

overdue requires amount_paid < total.

paid requires amount_paid = total.

One invoice contains exactly one currency.

Currency is immutable after issuance.

Different-currency obligations cannot share an invoice.

The obsolete subtotal_amount + adjustment_amount equation is not
retained; composition integrity must instead enforce the canonical
totals and their authoritative underlying components.

17. Issuance, Due Dates, and Timestamp Integrity

issued_at records when a draft becomes an actual invoice.

due_at is commercially configured; Engineering does not choose the
payment term.

due_at is NULL or due_at >= issued_at.

overdue requires issued_at and due_at.

paid requires issued_at and paid_at; paid_at >= issued_at.

void requires issued_at and voided_at; voided_at >= issued_at.

draft has no issuance, payment, or void timestamps.

Lifecycle timestamps record financial facts, not decorative
metadata.

18. Database, Service, and Administration Responsibilities

| Owner | Responsibility |

| --- | --- |

| Database | Invariant truth: referential integrity, arithmetic
consistency, valid statuses, payment bounds, timestamp/status
consistency, currency format, durable historical relationships. |

| Service orchestration | Legal transitions, issuance, payment
application, overdue determination, voiding, configured billing
context, and coordination of related commercial records. |

| Administration | Commercial policy: payment terms, grace periods,
allowed/default currencies, merchant-facing terminology, fee behavior,
and other configurable decisions within engineering boundaries. |

19. Historical Integrity

Invoices are durable financial records. Merchant cancellation does not
delete them. Physical deletion must be prevented where financial history
depends upon the merchant relationship; merchant invoice referential
integrity therefore remains restrictive.

20. Payment and Settlement

An issued invoice establishes the obligation. Payment satisfies that
obligation; it does not alter the historical commercial calculation.

A settlement adjustment is a distinct financial fact that arises from
the conditions under which an obligation is settled. For example, an
authorized early-payment discount belongs to settlement rather than
being retroactively rewritten into the original product/service charge.

Invoice ↓ Payment ↓ Settlement Adjustment, if applicable └──
e.g. early-payment discount

21. Billing Ledger and Statement of Account

An invoice establishes one FO's particular obligation. A Statement of
Account reports broader financial activity over time. Invoice, payment,
settlement activity, Billing Ledger, and Statement of Account are
therefore related but distinct concepts.

Invoice(s) Payment(s) Settlement activity ↓ Billing Ledger ↓ Statement
of Account

The Statement of Account must not be confused with
merchant_invoice_items or with the invoice itself.

22. Boundary with FOCA and MCPCA

FOCA owns what creates and governs the FO commercial relationship
and the commercial charges that may arise.

MCPCA owns promotion and Platform Credit award, eligibility,
validity, caps, and application policy/mechanics.

MISA owns how already-established commercial obligations and
monetary effects are composed into an invoice, preserved through
lifecycle, and related to payment, settlement, ledger, and statement
reporting.

23. Governing Doctrine

Every Future Offering has its own invoices. Invoice lines describe the
products and services charged to that project. Monetary effects
attributable to a particular charge remain attributable to that charge;
legitimate shared effects may operate at invoice level. The invoice
establishes Items Subtotal, Pre-tax Total, Taxes & Surcharges, and
Invoice Total. Engineering preserves financial meaning, provenance,
lifecycle, currency integrity, auditability, and safe boundaries;
Administration controls commercial policy and merchant-facing language
within those boundaries. Commerce establishes the obligation; payment
and settlement satisfy it; the Billing Ledger and Statement of Account
report the resulting financial history.

CE Status

MISA v1.1 is the governing invoice and settlement architecture for the
current v1 design. The invoice lifecycle is locked. The generic
adjustment_amount model is superseded. The next implementation review
may use this document as the architectural contract for
merchant_invoices and merchant_invoice_items, including the final
persistence-versus-derivation decision for canonical invoice aggregates.