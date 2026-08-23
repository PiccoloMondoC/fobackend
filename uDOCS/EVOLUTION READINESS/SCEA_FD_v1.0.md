### Sagrenti Cloud Evolution Architecture — Foundational Decisions

### 1. Go remains Sagrenti's long-term server-side foundation

We do **not** expect Sagrenti to outgrow Go. The likely evolution is:

**Go monolith → modular Go platform → distributed Go services → Sagrenti Cloud.**

We should not build microservices today merely because they may eventually be required. Instead, today's monolith should have sufficiently strong domain boundaries that capabilities can later be extracted without a major rewrite.

### 2. Future native applications have clear language ownership

If Sagrenti eventually develops fully native mobile applications:

| Technology             | Primary responsibility          |
| ---------------------- | ------------------------------- |
| **Go**                 | Sagrenti Cloud backend/services |
| **Kotlin**             | Native Android                  |
| **Swift**              | Native iOS/Apple platforms      |
| **Angular/TypeScript** | Web application                 |

**Although Kotlin is exceptionally well suited to JVM-specific backend integration, Sagrenti does not presently envision introducing JVM-based or other additional server-side technologies unless a compelling technical or platform requirement provides a material advantage that cannot reasonably be achieved within the Go architecture.**

**The governing principle is: additional server-side languages and runtimes must earn their architectural existence.**

**Distribution and language diversity are independent architectural decisions. Sagrenti may evolve toward distributed services while remaining entirely Go-based. Language diversity is not an architectural objective; distribution readiness is.**

### 3. Sagrenti Cloud should evolve from the monolith, not replace it

We envision eventual independently deployable capabilities such as:

```text
Future Offering
Anticipation Intelligence
Billing
Merchant Payments
Dimensions
Notifications
Search
etc.
```

But we want to extract mature domains from a disciplined monolith rather than undertake a future “great rewrite.”

### 4. Foundational capabilities should precede major decomposition

Before serious microservice decomposition, we identified foundational capabilities including:

* Identity & Access
* Administration / Configuration
* Dimensions
* Events & Messaging
* Workflow / Scheduling
* Observability
* API & Contract standards
* explicit Data Ownership
* Audit / Compliance
* Cloud deployment/runtime infrastructure

Importantly, many of these capabilities can and should begin **inside the monolith**.

### 5. “Analysis” became **Dimensions**

You described an existing Analysis concept under Configuration that allows Administration to define things such as:

* location
* category
* manager
* revenue/cost classifications
* other analytical perspectives

We recognized that **Dimensions** is the better foundational concept.

The distinction became:

> **Dimensions define the governed axes along which facts can be classified, segmented, aggregated, filtered, and compared. Analysis is what we subsequently do with those facts and dimensions.**

Dimensions therefore should **not own transactional facts**.

Billing owns billing/revenue facts; Payments owns payment facts; Future Offering owns FO facts; Anticipation Intelligence owns its facts. Dimensions provides the common dimensional framework through which those facts can subsequently be examined.

### 6. Dimensions should become a formal architecture

Rather than burying Dimensions inside Configuration, we agreed it is substantial enough to warrant a future:

> **Sagrenti Dimensions Architecture (SDA)**

A preliminary governing boundary emerged:

> **Dimensions define the governed axes by which Sagrenti facts can be classified, segmented, aggregated, filtered, and compared. They do not own the underlying facts.**

Administration can govern available classifications, hierarchies, labels and appropriate uses within Engineering-defined safe capabilities.

### 7. Dimensions can serve both Sagrenti and merchants

The same foundational dimensional architecture can support:

**Sagrenti operational analysis** — revenue by location, costs by manager, category performance, merchant activity, etc.

**Merchant analysis** — where consumer responses/anticipation are coming from, geographically or by other appropriate dimensions.

We also distinguished simple descriptive analysis from **Anticipation Intelligence**. For example, “31% of responses came from Texas” is an analytical fact; higher-order interpretation of those observations belongs to Anticipation Intelligence.

### 8. We need an event/messaging backbone

For eventual distributed services to communicate without tight coupling, Sagrenti needs an event and messaging architecture.

We distinguished:

**Synchronous APIs** — when one capability requires an immediate answer from another.

**Asynchronous domain events** — when a domain fact has occurred and zero, one, or many other capabilities may independently care about it.

For example:

```text
future_offering.activated
           │
           ▼
      Event Backbone
      /     |      \
 Billing Dimensions Notifications
```

Future Offering should not need to know every consumer interested in its activation.

### 9. Google Cloud Pub/Sub is our anticipated event transport

Because our long-term deployment direction is Google Cloud Run, we identified **Google Cloud Pub/Sub** as the anticipated managed event backbone.

But we established an important boundary:

> **Sagrenti defines the events. Pub/Sub transports them.**

Our domain model therefore should not become dependent upon Google/Pub/Sub semantics.

Pub/Sub is infrastructure; Sagrenti's event contracts belong to Sagrenti.

### 10. We should prepare for events before introducing Pub/Sub

We specifically agreed **not to wait for distributed Sagrenti Cloud before thinking about events**.

The Go monolith can establish domain-event semantics while producers and consumers still execute within one process.

Thus:

```text
Today

Go Monolith
  Future Offering
       │
       └── domain event
             ├── Billing
             ├── Dimensions
             └── Notifications
```

can later become:

```text
Sagrenti Cloud

Future Offering Service
          │
          ▼
       Pub/Sub
       /  |  \
Billing  Dimensions  Notifications
```

without redefining what `future_offering.activated` means.

### 11. This resulted in new **BEG §18.6D — Distributed Evolution Readiness**

We agreed to add §18.6D to BEG v1.09.

Its central requirement is:

> **Sagrenti shall be developed as a modular monolith with explicit domain ownership and communication boundaries that permit capabilities to be separated into independently deployable services without redesigning their domain semantics.**

It establishes explicit/versionable domain events where appropriate; producer ownership of event meaning; consumer independence; event identity, versioning, idempotency, ordering, retry safety, auditability and transactional-publication guarantees as Engineering concerns; and independence of domain contracts from deployment topology.

It also records the current infrastructure direction:

> **Google Cloud Pub/Sub is the anticipated managed event transport for a future Google Cloud Run distributed architecture. This direction does not make Pub/Sub-specific semantics part of Sagrenti's domain model.**

We deliberately retained **“should define explicit, versionable domain events”**, rather than *shall*, because synchronous communication remains appropriate in some circumstances.

### 12. The resulting architectural principle

I think the whole discussion ultimately condensed into one particularly important idea:

> **We are not building microservices today. We are building a monolith whose domain architecture does not unnecessarily prevent tomorrow's distribution.**

That means §18.6D should begin influencing our **current Future Offering and Monetization vertical reviews**, not something we put on a shelf until Sagrenti Cloud exists.

A future **Sagrenti Event & Messaging Architecture (SEMA)** can then formally define event architecture and eventually Pub/Sub integration, while **Sagrenti Dimensions Architecture (SDA)** formalizes the other major foundational capability we identified in this discussion.
