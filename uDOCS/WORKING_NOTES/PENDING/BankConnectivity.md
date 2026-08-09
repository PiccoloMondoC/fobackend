### BANK CONNECTIVITY

It's important that we have bank connectivity. Plaid is identified as a preferable bank connectivity vendor.

**Plaid should not have its own domain file**.

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
