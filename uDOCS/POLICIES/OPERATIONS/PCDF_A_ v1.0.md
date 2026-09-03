# Product Capability Delivery Framework (PCDF) v1.0

## Part A — Product Capability Framework

### 1. Purpose

The Product Capability Delivery Framework governs how Sagrenti translates defined Platform capabilities into complete, usable product experiences.

A Platform capability is not complete merely because its durable data model, backend services, or API exist. It becomes product-complete when the required Platform behavior and user experience operate together successfully and can be demonstrated through the working product.

PCDF therefore provides the bridge between **what the Platform must be capable of doing** and **the backend and frontend implementations through which that capability becomes real**.

---

## 2. Relationship to the Governing Architecture

PCDF does not replace FOPSC, canonical architecture and doctrine, or implementation classifications. It connects them.

```text
                    FOPSC
       Platform capability definition
                      │
                      ▼
       Canonical architecture/doctrine
                      │
                      ▼
                    PCDF
        End-to-end product realization
               ┌──────┴──────┐
               ▼             ▼
     Backend Release      Frontend
     Classification       Classification
               │             │
               ▼             ▼
        Go / DB / API     Angular artifacts
```

### 2.1 FOPSC

FOPSC defines the capabilities the Future Offering Platform must possess and the product principles governing those capabilities.

### 2.2 Canonical Architecture and Doctrine

Canonical architecture and doctrine define the authoritative semantics, boundaries, invariants, lifecycle rules, commercial rules, and other governing requirements under which those capabilities operate.

### 2.3 PCDF

PCDF defines what must come together end-to-end for those capabilities to become working parts of the Sagrenti product.

PCDF is concerned with **product realization**, not merely implementation existence.

### 2.4 Backend Release Classification

Backend Release Classification identifies and classifies the database, Go, service, API, infrastructure, and other backend artifacts through which the backend portion of a capability is implemented.

### 2.5 Frontend Classification

Frontend Classification performs the corresponding role for the frontend. It identifies and classifies the Angular applications, routes, components, services, models, functions, workflows, tests, and related artifacts through which the user-facing portion of a capability is implemented.

Backend Release Classification and Frontend Classification are parallel implementation views. Neither independently defines the product capability.

---

# 3. Product Capability

A **Product Capability** is a distinct end-to-end ability that Sagrenti provides to an Administration, Merchant, or Consumer actor.

A capability is defined by its product purpose rather than by a particular screen, API endpoint, database table, Go file, Angular component, or implementation mechanism.

For example:

```text
Future Offering Creation       Product Capability

Create FO                      Product behavior
Save draft                     Product behavior
Resume draft                   Product behavior
Submit for activation          Product behavior

POST /future-offerings         API implementation

merchant_future_offerings.go   Backend implementation

Angular components/services    Frontend implementation
```

This distinction allows product semantics to remain stable while implementation evolves.

---

# 4. Product Actors

PCDF organizes directly consumable product capabilities around three principal actors.

### 4.1 Administration

Administration governs the operational and commercial behavior of implemented Platform capabilities within the safe boundaries established by Engineering and exercises the operational oversight necessary to administer the Platform.

### 4.2 Merchant

The Merchant creates and operates Future Offering projects, engages with the anticipation surrounding those projects, receives intelligence generated from that activity, fulfills appropriate consumer relationships, and manages the commercial relationship between each FO and the Platform.

### 4.3 Consumer

The Consumer discovers Future Offerings, engages with FOs, follows merchants, participates in the continuing development of projects, and may ultimately choose to establish an appropriate contact or claim relationship with a merchant.

A fundamental semantic distinction applies:

> **Consumers engage with Future Offerings. Consumers follow merchants.**

MyRadar represents the consumer's relationship with FOs arising from engagement. Merchant following is a separate relationship.

---

# 5. Platform Behavior

Not every necessary Platform behavior constitutes an independent PCDF capability.

Sagrenti must perform substantial work without a human actor directly invoking that work, including:

* authoritative lifecycle processing;
* engagement processing;
* milestone processing;
* anticipation-intelligence derivation;
* notifications;
* token lifecycle processing;
* external integrations;
* asset processing and delivery;
* commercial metering;
* funding consumption;
* billing and reconciliation;
* auditability, reliability, and recoverable processing.

These behaviors are ordinarily specified as obligations of the product capabilities they enable.

A Platform behavior becomes a separate PCDF capability only when it represents an independently meaningful product capability rather than an internal mechanism through which another capability operates.

---

# 6. Capability and Behavior

PCDF distinguishes a **capability** from the individual **behaviors** through which that capability is exercised.

For example:

```text
Future Offering Engagement
        │
        ├── Watch
        ├── Waitlist
        ├── Early Access Request
        ├── Beta
        ├── Reservation Interest
        └── Preorder Intent
```

Future Offering Engagement is the capability.

The available engagement actions are behaviors or manifestations of that capability. They do not become independent PCDF capabilities merely because they produce different user actions or states.

The same principle applies throughout PCDF.

A capability should correspond to a **distinct product purpose**, not every individual action required to accomplish that purpose.

---

# 7. End-to-End Completion Doctrine

Backend completion is not product completion.

Frontend completion is not product completion.

A capability is product-complete only when all implementation layers necessary for that capability operate together successfully.

The normal realization path is:

```text
Durable facts
      ↓
Domain behavior
      ↓
Services
      ↓
API
      ↓
Frontend experience
      ↓
End-to-end operation
      ↓
Working-site review
      ↓
Product capability complete
```

Not every capability requires every layer in precisely this form. The governing requirement is that **every layer necessary to realize the capability must exist and operate correctly together**.

Completion must therefore be observable.

Where a capability is user-facing, completion means that the relevant actor can successfully accomplish the intended product purpose through the actual Sagrenti experience—not merely that the underlying implementation can theoretically support it.

---

# 8. Canonical Capability Spine

PCDF v1.0 establishes the following initial product capability spine.

## 8.1 Administration

**PCDF-A01 — Platform Configuration**
Configure the operational and commercial behavior of implemented Platform capabilities within Engineering-defined safe boundaries.

**PCDF-A02 — Platform Oversight**
Observe and administer Platform operations, states, exceptions, merchants, and Future Offerings requiring administrative oversight.

## 8.2 Merchant

**PCDF-M01 — Future Offering Creation**
Create a Future Offering project and bring it through the appropriate creation process to submission.

**PCDF-M02 — Future Offering Management**
Manage an established Future Offering through its permitted project lifecycle.

**PCDF-M03 — Project Milestone Management**
Define, maintain, and communicate meaningful milestones in the development of a Future Offering.

**PCDF-M04 — Consumer Communication**
Communicate appropriate project announcements and developments to consumers with whom the relevant relationship exists.

**PCDF-M05 — Engagement & Anticipation Intelligence**
Observe consumer participation and engagement and receive useful intelligence derived by Sagrenti from the anticipation surrounding an FO.

**PCDF-M06 — Consumer Fulfillment**
Participate in the fulfillment process through which eligible consumers can progress from anticipation to an appropriate merchant contact or claim relationship.

**PCDF-M07 — FO Funding & Commercial Account**
Fund and manage the commercial position of an individual FO, including consumption, available funds, and replenishment.

**PCDF-M08 — Billing & Reconciliation**
Understand and reconcile invoiced Platform consumption, charges, applied funds or credits, payments, and outstanding amounts attributable to the FO.

## 8.3 Consumer

**PCDF-C01 — Future Offering Discovery**
Discover and explore forthcoming offerings and the projects and merchants behind them.

**PCDF-C02 — Future Offering Engagement**
Express anticipation toward an FO through one or more available consumer engagement actions.

**PCDF-C03 — MyRadar**
Provide a persistent personal experience centered on Future Offerings with which the consumer has engaged.

**PCDF-C04 — Merchant Following**
Establish and manage an ongoing following relationship with a merchant independently of engagement with any particular FO.

**PCDF-C05 — Project Participation & Updates**
Experience relevant milestones, developments, announcements, and participation opportunities associated with FOs in which the consumer participates.

**PCDF-C06 — Project Review**
Review an eligible Future Offering project under the applicable Platform rules.

**PCDF-C07 — Token & Merchant Contact**
Receive an eligible consumer token and choose whether to use it to establish the permitted contact or claim relationship with the merchant.

**PCDF-C08 — Privacy & Participation Control**
Exercise appropriate control over engagement, merchant following, project participation, and disclosure of consumer identity or contact information.

---

# 9. Evolutionary Capability Specification

The capability spine established by Part A does not require every capability to be exhaustively specified before implementation proceeds.

Detailed capability definition belongs to **PCDF Part B — Capability Specifications**.

Part B evolves with development.

When a development phase reaches a capability, that capability is reconciled against FOPSC, applicable canonical doctrine, existing implementation, and the intended real-world product experience.

Its required behaviors, Platform obligations, user experience obligations, boundaries, dependencies, and completion criteria can then be specified with the knowledge available at the point of implementation.

This avoids premature specification while preventing implementation from proceeding without an explicit product objective.

---

# 10. Evolutionary Delivery Reconciliation

Implementation reconciliation belongs to **PCDF Part C — Capability Delivery Map**.

Part C evolves alongside Part B and implementation.

For each capability it identifies and reconciles:

```text
Product Capability
       │
       ├── Backend implementation
       │     Database
       │     Go
       │     Services
       │     API
       │     Supporting infrastructure
       │
       ├── Frontend implementation
       │     Angular application/feature
       │     Routes
       │     Components
       │     Services
       │     Models/functions
       │     Workflows
       │     Tests
       │
       └── End-to-end verification
```

Part C therefore provides the living record of whether architectural capability has actually become working product capability.

---

# 11. Development Method

Beginning with the applicable development phase, Sagrenti development should proceed as **vertical product slices** wherever the architectural dependencies permit.

For each relevant capability:

```text
Identify capability
        ↓
Develop/reconcile Part B specification
        ↓
Identify backend requirements
        ↓
Identify frontend requirements
        ↓
Implement required backend and frontend
        ↓
Integrate
        ↓
Exercise through the working product
        ↓
Reconcile Part C
        ↓
Declare capability complete
```

Foundation work may necessarily precede its visible product experience. Such work may be architecturally complete without causing the associated PCDF capability to be classified as product-complete.

---

# 12. Governing Principle

The purpose of PCDF is not to force frontend development onto backend architecture or backend implementation onto frontend design.

It is to ensure that both are ultimately accountable to the same product capability.

The governing test is therefore:

> **Can the intended actor successfully exercise the capability through the working Sagrenti product, with the required Platform behavior operating correctly beneath the experience?**

Until the answer is yes, the capability may be architecturally advanced or partially implemented, but it is not product-complete.

**— End of PCDF v1.0, Part A —**
