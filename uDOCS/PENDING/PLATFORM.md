I think that's the right choice.

It's simple, durable, and doesn't tie the engineering architecture to a particular business strategy.

I'd make it an engineering standard.

---

## Platform Naming Standard

### Brand

**Sagrenti**

Used in:

* Public website
* Marketing
* Consumer UI
* Merchant UI
* Legal documents

---

### Engineering

**Platform**

Used in:

* Backend code
* APIs
* Architecture
* Engineering documents
* Internal services

Examples:

```go
type PlatformService struct
type PlatformConfig struct
type PlatformSettings struct
type PlatformManager struct
type PlatformContext struct
```

---

### Business Domains

The Platform contains independent domains:

* Identity
* Merchant
* Consumer
* Future Offering
* Launch Campaigns
* Commerce
* Billing
* Payments
* Analytics
* Administration
* Trust & Risk

Each domain owns its own models and services.

---

I also think this gives you a long-term cleanup roadmap.

Instead of asking:

> "Should this be called Sagrenti?"

the question becomes:

> "Is this part of the Platform, or is it a business domain?"

For example:

❌

```go
type SagrentiService struct
```

✅

```go
type PlatformService struct
```

❌

```go
type SagrentiConfig struct
```

✅

```go
type PlatformConfig struct
```

❌

```go
type SagrentiSettings struct
```

✅

```go
type PlatformSettings struct
```

And domain-specific services remain domain-specific:

```go
type MerchantService struct
type ConsumerService struct
type FutureOfferingService struct
type BillingService struct
type PaymentService struct
```

I would even update **BEG** to include this as an engineering doctrine, because it's exactly the kind of naming rule that prevents architectural drift. It establishes a clear separation between:

* **Brand** (Sagrenti)
* **Platform** (the engineering system)
* **Business Domains** (Merchant, Future Offering, Billing, Payments, etc.)

That's a naming convention that should still make sense decades from now, even if the platform expands far beyond its current scope.
