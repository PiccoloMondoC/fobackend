# Future Offering Consumption Billing Model (FOCBM)

## 1. Purpose

The Future Offering Consumption Billing Model defines the relationship among the Future Offering, Billing Period, consumption-based charges, FO financial account, prepayments, monthly invoicing, and settlement.

The model preserves strict separation between service performance, consumption measurement, financial accounting, invoicing, and payment.

## 2. Independent Domain Windows

Service Term, Service Period, and Billing Period are distinct concepts serving different domain purposes.

**Service Term** defines the overall duration during which Sagrenti provides the Future Offering service.

**Service Period** defines an individual performance window within the Service Term.

**Billing Period** defines the accounting and consumption window during which applicable Future Offering resource usage is accumulated for billing.

Service Periods and Billing Periods may both use monthly boundaries, but this does not couple them. Their monthly cadence exists for different domain reasons.

Billing Periods must not derive their identity, existence, or lifecycle from Service Periods.

## 3. Monthly Consumption Billing

Sagrenti uses monthly consumption billing for Future Offering resource usage.

The principal consumption-based charges are:

* Anticipation Intelligence Fees arising from actual Qualified Anticipation Entries (QAEs); and
* Asset Hosting Overage Fees arising from actual Asset Hosting Overages (AHOs).

These charges cannot be determined in advance because they depend upon actual resource consumption.

They therefore accrue during the applicable monthly Billing Period and are billed in arrears.

Sagrenti does not provide native annual Future Offering plans or recurring annual billing cycles.

## 4. Billing Period Semantics

A Billing Period is an authoritative monthly accounting window for a Future Offering.

Billing Periods are not payment schedules, funding schedules, or projections of future commercial activity.

Engineering defines their authoritative calendar semantics.

Every date during an established Service Term for which billable activity may occur must belong to exactly one authoritative Billing Period.

Billing Periods therefore must provide complete applicable coverage without overlap or unintended gaps capable of making legitimate billable activity unaccountable.

Administration may make permitted configuration choices within Engineering's implemented Billing Period capabilities, if and where such configurable choices exist.

## 5. Billing Period Lifecycle

Billing Periods do not require a normal-domain supersession architecture.

There is presently no identified commercial event requiring an authoritative Billing Period to be superseded, replaced, retired, or reconstructed through lineage chains.

Accordingly, Billing Period architecture should not be based upon:

* future schedules;
* schedule establishment;
* schedule tails;
* prospective schedule replacement;
* prospective retirement; or
* Billing Period supersession.

An authoritative Billing Period represents an accounting fact and is immutable.

If an erroneous Billing Period requires exceptional correction, that is a governed corrective-data concern rather than an ordinary Billing Period lifecycle.

## 6. Future Offering Financial Independence

Each Future Offering is an **independent contractual and financial entity, independently accounted for and financially separate from the merchant's other Future Offerings**.

Each FO may therefore maintain its own financial account and monetary position, including:

* prepaid funds or account credit;
* consumption charges;
* applied funds;
* outstanding amounts;
* funding transactions; and
* settlement history.

The merchant remains the commercial obligor and owns the payment methods used to fund or settle its FO accounts.

Funds, credits, charges, balances, and settlements attributable to one FO must not silently transfer to, subsidize, or be netted against another FO.

## 7. Prepaid-Credit and Monthly Post-Pay Model

Sagrenti combines FO-specific prepayment with monthly consumption/post-pay billing.

A merchant may add prepaid funds or account credit to an individual FO.

As QAEs and AHOs occur during the monthly Billing Period, their monetary charges accrue against that FO.

Available prepaid funds eligible for those charges are economically consumed as the applicable consumption occurs.

Prepayment therefore does not merely remain untouched until monthly invoice generation.

Prepayment affects settlement of consumption; it does not reduce, obscure, or redefine the gross cost of that consumption.

## 8. Prepayment and Exposure Control

Sagrenti must not implicitly provide every merchant with an arbitrary or unlimited B2B credit facility.

The merchant's FO funding behavior provides the basis for controlling permitted intra-month financial exposure.

The model supports prepayment/funding levels associated with corresponding automatic-charge thresholds, for example:

| FO funding level | Auto-charge threshold |
| ---------------: | --------------------: |
|               $0 |                    $0 |
|              $25 |                   $25 |
|              $50 |                   $50 |
|             $250 |                  $250 |
|           $1,000 |                $1,000 |
|           $5,000 |                $5,000 |

Engineering implements the safe capability and invariant financial controls. Administration may configure permitted operating thresholds and choices within those Engineering boundaries.

An auto-charge threshold controls financial exposure. It does not define or modify a Billing Period.

## 9. Funding and Billing Period Independence

Funding and settlement events must never alter, split, shorten, extend, or otherwise redefine a Billing Period.

Accordingly:

* adding prepaid funds does not begin a new Billing Period;
* consuming prepaid funds does not end a Billing Period;
* exhausting prepaid funds does not end a Billing Period;
* reaching an auto-charge threshold does not split a Billing Period;
* an intra-month payment does not create a new Billing Period; and
* making a large prepayment does not create an annual Billing Period.

The monthly accounting window continues independently of funding and settlement activity.

## 10. Annual Prepayment

A merchant that wishes to fund approximately a year of Future Offering activity in advance may add sufficient funds or account credit to that FO.

Such funding is a **prepayment**, not an annual plan.

Sagrenti continues monthly consumption accounting and monthly invoicing.

As each month progresses, eligible consumption charges are satisfied from the FO's available prepaid balance according to the applicable financial rules.

Annual prepayment therefore does not create:

* an annual Service Term;
* an annual Billing Period;
* an annual billing cycle;
* an annual recurring invoice; or
* a different consumption-accounting model.

## 11. Monthly Invoice

Sagrenti generates a monthly invoice covering all fees properly billable to the Future Offering for that billing cycle.

The monthly invoice is not merely a usage invoice.

Subject to the applicable fee rules, it may include:

* Platform Service Fees;
* Anticipation Intelligence Fees;
* Asset Hosting Overage Fees; and
* other fees properly billable during that cycle.

The Anticipation Intelligence Activation Fee is ordinarily payable before activation is permitted and therefore normally does not appear on subsequent monthly invoices.

Where Sagrenti has expressly authorized an arrangement permitting deferred payment of an Activation Fee, the applicable Activation Fee may instead appear on the appropriate monthly invoice.

For example, assuming no Activation Fee is due:

```
September Platform Service Fee              $ 25
September Anticipation Intelligence Fee      180
September Asset Hosting Overage Fee            40
                                            ----
Gross September charges                     $245

Prepaid funds already applied               (150)
                                            ----
Remaining amount payable                     $95
```

The gross invoice must preserve the full economic cost of the month's services and consumption.

Amounts previously satisfied through eligible prepaid funds or other payments are presented as amounts already satisfied rather than being used to reduce the reported gross cost of the month.

## 12. Invoice Inclusion and Prepayment Eligibility

Whether a fee belongs on an invoice and whether prepaid FO funds may be applied to that fee are separate questions.

The monthly invoice comprehensively reports all fees properly billable to the FO during the applicable billing cycle.

Prepaid FO funds are consumed by fees designated as eligible for prepayment application, including Anticipation Intelligence Fees and Asset Hosting Overage Fees under the present model.

A fee's presence on an invoice does not, by itself, make that fee eligible for satisfaction from prepaid FO funds.

Accordingly:

> **Invoiceable and prepayment-applicable are distinct financial properties.**

## 13. Architectural Separation

The model maintains four principal financial concepts:

**Billing Period** — measures the monthly accounting and consumption window.

**FO Account** — carries the individual Future Offering's running monetary position, including prepaid funds, consumption charges, applied funds and outstanding amounts.

**Monthly Invoice** — authoritatively reconciles and documents the full month's billable charges, amounts already satisfied, and any remaining amount payable.

**Payment/Credit Policy** — governs funding, supported payment methods, automatic-charge thresholds, exposure controls, application of eligible prepaid funds, and settlement behavior.

The governing separation is:

> **Billing Period measures consumption; FO Account carries its financial consequences; Invoice reconciles the period; Payment fulfills the monetary obligation.**

These responsibilities must remain independently evolvable and must not be collapsed merely because they participate in the same commercial flow.
