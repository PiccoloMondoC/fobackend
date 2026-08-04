# Backend Engineer Guidance v1.09

## Horizontal Backend Engineering Doctrine

### 1. Purpose

1.1 Backend Engineer Guidance defines Sagrenti’s cross-cutting backend engineering doctrine.

1.2 It governs backend engineering across services, domains, and future business surfaces.

1.3 It is a horizontal engineering policy, not a product architecture document.

1.4 Product-specific architecture belongs in companion documents under SED, not in BEG.

1.5 BEG exists to keep backend systems production-grade, explicit, secure, maintainable, scalable, auditable, and architecturally coherent.

---

### 2. Scope

2.1 BEG governs:

* engineering philosophy
* persistence and data modeling
* IDs and readable keys
* timestamps and lifecycle handling
* relational integrity
* security and protected persistence
* token and JWT doctrine
* actor model and ownership semantics
* API and persistence separation
* services, transactions, events, and outbox behavior
* observability and auditability
* validation and canonicalization
* migration doctrine
* Chief Engineer review doctrine
* guidance voice and governance framing

2.2 BEG does not define product-specific behavior.

2.3 Product-specific architecture belongs in companion documents, including as applicable:

* Merchant Center Architecture
* Consumer Surface Architecture
* Admin Console Architecture
* Offer Architecture
* Provider Integration Architecture

---

### 3. Core Engineering Principles

3.1 **Canonical over convenient**
Persist canonical forms. Do not persist presentation-specific variants unless a real business need exists.

3.2 **Explicit over implicit**
Behavior must be deliberate, reviewable, and stable. Avoid hidden fallbacks, magical aliasing, silent repairs, and accidental policy drift.

3.3 **Production-first**
Design for reliability, auditability, observability, security, and maintainability from the beginning.

3.4 **UTC internally**
All internal time handling must use UTC.

3.5 **Integrity at the lowest safe layer**
Enforce invariants in the database where practical and in services where business semantics exceed simple relational constraints.

3.6 **Clear failure over silent corruption**
Malformed data, invalid states, and contract violations must fail clearly.

3.7 **Dev == Prod**
Development and production behavior must remain materially aligned.

3.8 **Lifecycle-first schema authority**
The intended lifecycle design is authoritative. Current table shape is evidence, not authority.

3.9 **Normalized domain concepts over opaque blobs**
Reusable, queryable, auditable domain concepts belong in normalized structures, not opaque JSON catch-alls.

3.10 **Protected persistence**
Secrets, bearer material, and protected values must follow protected-persistence rules, not ordinary data-storage rules.

3.11 **Publication contract over permission weakening**
Public exposure must be modeled as an explicit publication contract, not as weakened internal/admin access.

3.12 **Visibility and capability are separate concerns**
Visibility is governed by lifecycle/publication state. Capability is governed by actor class, authentication, ownership, delegation, and permission rules.

3.13 **Shared identity, distinct authorization classes**
A shared identity foundation may exist, but authorization must still distinguish materially different actor classes.

3.14 **Canonical validated value rule**
Where validation trims, normalizes, or canonicalizes input, the canonical validated value must be the value used downstream.

3.15 **Low-level ownership doctrine**
Low-level packages own low-level concerns. Security packages own security primitives and security-facing sentinels. Persistence packages own persistence/model-facing sentinels. Boundary layers may translate between them where domain or API stability requires it.

3.16 **Asymmetric trust doctrine**
For distributed backend architecture, prefer asymmetric trust boundaries over shared-secret sprawl wherever practical.

3.17 **Pre-startup canonical correction rule**
Before production launch, Sagrenti does not preserve erroneous code, unstable local contracts, weak naming, unsafe observability shapes, or legacy compatibility merely to avoid breaking call sites. The Chief Engineer must prefer canonical correctness and require downstream repair.

3.18 **No legacy preservation before startup**
In pre-startup code, breaking changes are acceptable when they remove drift, clarify contracts, improve security, improve observability, or align implementation with approved architecture. Compatibility preservation applies only to intentional public, production, or migration-stable contracts.

3.19 **Exported identifier documentation rule**
Production-grade Go code must include godoc comments for exported constants, variables, types, interfaces, functions, and methods. Refactors must not remove useful godoc context unless replacing it with clearer documentation.

3.20 **Documentation non-regression rule**
A drop-in replacement must preserve or improve documentation quality. Security, lifecycle, observability, cardinality, and operational warnings must supplement descriptive documentation, not replace it.

3.21 **Central-file awareness rule**
Chief Engineer and backend engineers must not assume a helper, sentinel error, constant, scanner, validator, JWT/security utility, or shared parameter is missing merely because it was not included in the prompt. Before creating a local replacement, reviewers must check or expressly ask whether a central/shared implementation already exists.

3.22 **No local duplication of central concerns**
Shared concerns must remain centralized. Sentinel errors, scan helpers, context helpers, validation helpers, security utilities, metric constants, and cross-cutting configuration helpers must not be redeclared locally unless the Chief Engineer explicitly determines that the local concern is materially distinct.

3.23 **Prompt-scope limitation rule**
A reviewed file is not the whole codebase. Refactor decisions must distinguish between “not present in this file” and “not present in the system.” When central ownership is likely, the correct action is to reference, request, or preserve the central source, not invent a local equivalent.

3.24 **Engineering collaboration over internal competition**
Backend engineers and the Chief Engineer must operate as collaborators in service of production quality, not as personal competitors.

Technical disagreement, rigorous critique, and strong architectural challenge are expected. Reviews, proposals, and feedback must be grounded in evidence, standards compliance, correctness, reliability, security, maintainability, and production outcomes, never personal positioning, status competition, or point-scoring.

The objective of review is to improve the system, prevent defects, and strengthen implementation quality. Engineers must critique the artifact, behavior, implementation, or design decision, not the engineer.

Valid work must be acknowledged regardless of authorship. Correctness does not become invalid because it originated from another engineer. Disagreement does not justify dismissing standards, selectively applying guidance, or creating unnecessary local alternatives.

Competition belongs against defects, ambiguity, operational risk, regressions, performance issues, and architectural weakness, not against teammates.

---

### 4. Naming Conventions Policy

4.1 Names must be descriptive, stable, and semantically clear.

4.2 Table names should be plural, lowercase, and snake_case.

4.3 Column names should be lowercase snake_case.

4.4 Foreign keys must use `<entity>_id`.

4.5 Booleans must read clearly as true/false statements.

4.6 Primary keys should use `id`.

4.7 Schema object naming must be consistent across constraints and indexes.

4.8 Settled product terminology should remain stable in comments, DTOs, docs, and coordination materials, but product-specific naming belongs in the relevant product architecture document.

---

### 5. ID and Readable Key Policy

5.1 Use UUID as the default primary key unless a documented exception exists.

5.2 IDs must be opaque and must not encode business meaning.

5.3 Use separate readable keys such as `slug`, `code`, or equivalent where needed.

5.4 `id` is the canonical entity identifier. Readable keys are not substitutes for canonical IDs.

5.5 **Global public identity handle rule**
`global_handles` is a reserved public identity namespace, not a general slug registry.

5.6 **Allowed use of global_handles**
It may be used for scarce public identity names such as user handles, brand handles, and merchant public handles/slugs only when cross-surface collision prevention is an intentional product rule.

5.7 **Merchant-handle conditional rule**
Merchant slugs may be reserved in `global_handles` only when the product intends merchant public identity names to be protected against collision with user or brand handles.

5.8 **Disallowed use of global_handles**
It must not be used for department slugs, category slugs, offer keys, or other high-volume catalog identifiers.

5.9 **Canonical-table readable key rule**
Readable-key uniqueness for domain-local identifiers must be enforced in the canonical table of that domain.

---

### 6. Time Zone and Timestamp Policy

6.1 All backend systems must use UTC as the canonical time standard.

6.2 DB/session timezone must be UTC.

6.3 App/server timezone must be UTC.

6.4 Stored timestamp columns must use `TIMESTAMPTZ`.

6.5 Default lifecycle timestamps should use database-owned time, typically `NOW()`.

6.6 UI rendering should convert UTC into the user’s local timezone.

6.7 **Time-source ownership rule**
Persisted lifecycle timestamps must be DB-owned and derived from database time.

6.8 **Application reference time rule**
Application-side reference time may be used for non-persisted query windows, ranking windows, comparisons, timeout windows, retry windows, and orchestration timing, provided it comes from the canonical UTC time helper.

6.9 **Non-mixing rule**
Do not mix DB-owned persisted time and application reference time casually. A model method must not use application time to write DB-owned lifecycle fields when DB time is the intended source of truth.

---

### 7. Timestamp Field Standards

7.1 Most business tables should include `created_at` and `updated_at`.

7.2 Where applicable, include `deleted_at`, `published_at`, `expires_at`, `approved_at`, `rejected_at`, and other lifecycle timestamps with clear semantics.

7.3 `created_at` records initial insert time.

7.4 `updated_at` records the latest meaningful mutation time.

7.5 Lifecycle timestamps must reflect real workflow transitions.

7.6 Where a timestamp is part of canonical lifecycle state, the database remains the source of truth.

---

### 8. Nullability and Default Value Policy

8.1 Use `NOT NULL` by default.

8.2 Use `NULL` only where absence has clear semantic meaning.

8.3 Every nullable field must answer: what does null mean?

8.4 Defaults must reflect safe and intentional system behavior.

8.5 Avoid defaults that invent business meaning.

8.6 Avoid defaults that conceal incomplete writes.

8.7 Avoid environment-specific defaults that change business behavior.

---

### 9. Status and Lifecycle Policy

9.1 Lifecycle state must be explicit.

9.2 Prefer one authoritative status field over conflicting boolean flags for multi-step workflows.

9.3 Use `TEXT + CHECK` for small stable sets. Use reference tables where values are dynamic, metadata-rich, or operationally managed.

9.4 Determine the proper lifecycle before accepting current schema shape.

9.5 If lifecycle requires soft delete, the table must include `deleted_at`.

9.6 Standard reads should exclude soft-deleted rows unless the method is explicitly administrative, audit, or recovery oriented.

9.7 Comments must match the real lifecycle contract.

9.8 Where public visibility exists, it must be based on publication/lifecycle state.

---

### 10. Foreign Key, Uniqueness, and Check Constraint Policy

10.1 Use foreign keys unless a documented technical reason prevents it.

10.2 Choose `ON DELETE` behavior deliberately.

10.3 `RESTRICT` is the default for important business relationships.

10.4 `CASCADE` is for clear ownership semantics only.

10.5 `SET NULL` is valid only when null remains meaningful.

10.6 Use unique constraints for business invariants that must never be duplicated.

10.7 Use check constraints for small, stable domain rules.

10.8 Examples include:

* unique emails
* unique readable keys
* composite user/role uniqueness
* non-negative amounts
* bounded percentages
* stable status values

---

### 11. Enum and Controlled Value Policy

11.1 Preferred order:

* `TEXT + CHECK` for small stable sets
* reference tables for dynamic/admin-managed sets
* native DB enums only when operationally justified

11.2 Reusable, metadata-bearing, or operationally managed values should prefer reference tables.

---

### 12. Money and Decimal Handling Policy

12.1 Never use floating point types for money.

12.2 Use fixed-precision numeric types such as `NUMERIC(19,4)`.

12.3 Store amount separately from currency code.

---

### 13. Soft Delete and Lifecycle Correction Policy

13.1 Use soft deletes only where recovery, moderation, traceability, retention, or historical referential meaning justify them.

13.2 Prefer `deleted_at TIMESTAMPTZ NULL` over `is_deleted` where temporal traceability matters.

13.3 Begin with lifecycle intent, not current schema shape.

13.4 If soft delete is correct, support logical removal distinctly from true hard delete where both are appropriate.

13.5 Hard delete must remain true hard delete, not an alias for soft delete.

13.6 Persisted lifecycle transitions should use DB-owned time.

13.7 If a record type clearly requires retention-aware removal but lacks the right schema support, that is a schema defect to be corrected.

---

### 14. Security and Protected Persistence Policy

14.1 Secrets must never be stored in plaintext in canonical persistence.

14.2 Where secrets must be persisted, they must be encrypted at rest using the platform-approved mechanism.

14.3 Plaintext secret values may exist only at controlled input boundaries.

14.4 Backend models that represent persisted rows should model the canonical persisted form.

14.5 Secret fields must never be exposed in API responses, logs, traces, metrics labels, audit payloads, or public JSON.

14.6 Refresh tokens, activation tokens, reset tokens, and similar bearer credentials must never be stored or logged in plaintext.

14.7 Canonical persistence must store only a protected derived form, such as a hash, unless a documented and approved exception exists.

14.8 Decryption should occur only in tightly scoped service paths that require it for an approved operational purpose.

14.9 **Security sentinel ownership rule**
Low-level security packages own low-level security sentinels.

14.10 **Persistence sentinel ownership rule**
Data-layer `errors.go` owns persistence/model-facing sentinels.

14.11 **Boundary translation rule**
Boundary layers may map security errors to centralized public/domain errors when needed, but low-level security packages must not depend on persistence-layer sentinel ownership to define their own low-level behavior.

14.12 **Dependency-direction rule**
Security primitives and security errors must not be architected to depend downward on persistence-layer error ownership in ways that invite package inversion or import-cycle drift.

---

### 15. Token, JWT, and Revocation Doctrine

15.1 Refresh tokens are retained security records.

15.2 Logout, rotation, and session invalidation should be modeled as token revocation, not hard deletion.

15.3 Refresh-token lifecycle should use `revoked_at` or equivalent explicit revocation state.

15.4 Hard delete for refresh tokens is exceptional cleanup or administrative maintenance only.

15.5 For access-token blacklist tables, row presence is enough to represent revocation unless additional state is explicitly justified.

15.6 Token lifecycle must follow the real security contract, not convenience deletion.

15.7 **JWT signing standard: EdDSA (Ed25519)**
Sagrenti standardizes on EdDSA using Ed25519 for JWT signing across services.

15.8 **Canonical JWT signing algorithm**
EdDSA / Ed25519 is the canonical JWT signing algorithm for Sagrenti production architecture.

15.9 **Rationale**
Sagrenti is architected for a distributed / microservice environment. EdDSA supports asymmetric trust boundaries where:

* only the issuing authority holds the private signing key
* downstream services validate tokens using public verification keys
* services do not need shared signing secrets

15.10 **Security architecture benefit**
This reduces blast radius, secret proliferation, and inter-service trust coupling.

15.11 **HS-family prohibition**
HS-family JWT signing (`HS256`, `HS384`, `HS512`) is not the architectural standard for Sagrenti production systems.

15.12 **Limited tolerance for HMAC**
HMAC-based JWT signing may be tolerated only for:

* temporary local experimentation
* isolated prototypes
* explicitly approved migration periods

15.13 **Non-canonical status of HMAC**
HMAC must not become the canonical production signing model.

15.14 **Validation rule**
JWT validation must explicitly enforce `jwt.SigningMethodEdDSA`.

15.15 **No algorithm confusion rule**
Algorithm confusion, implicit algorithm acceptance, or fallback acceptance is prohibited.

15.16 **Key ownership rule**
Private key: issuer/auth authority only. Public key: distributed to validating services.

15.17 **Validator capability rule**
Validators must never possess signing capability unless they are explicitly acting as an issuer.

15.18 **Monolith transition rule**
Even when running in a monolith, the authentication layer should operate as if services were distributed.

15.19 **Monolith implementation consequence**
Tokens should still be signed with Ed25519 private keys, validation should still use public keys, and symmetric shortcuts should be avoided to prevent architectural drift.

15.20 **Rotation rule**
JWT signing must support future key rotation through `kid` support and multi-key validation readiness.

15.21 **Security posture objective**
JWT signing architecture must optimize for least trust, minimal secret sharing, compartmentalization, future distributed deployment, and zero architectural rewrites during monolith-to-microservice transition.

15.22 **Review implication**
Future reviews should reject HS256 regressions unless explicitly justified as temporary migration or prototype code outside the canonical production architecture.

---

### 16. Actor Model and Ownership Semantics

16.1 The durable cross-cutting actor model is:

* guest consumer
* authenticated consumer
* merchant actor
* internal/admin actor

16.2 **Shared identity foundation rule**
Different actor classes may share a common identity/authentication foundation.

16.3 **Authorization separation rule**
Actor classes must remain distinct at the authorization layer.

16.4 **Actor-appropriate ownership semantics**
Canonical ownership values must reflect the actual actor class that owns the row.

16.5 **User-owned configuration ownership rule**
User-owned configuration records must use actor-appropriate ownership semantics. Consumer-owned rows remain consumer-scoped. Merchant-owned rows remain merchant-scoped when the actor acts in merchant capacity. Internal/admin visibility alone does not create an internal/admin-owned canonical record class.

16.6 **Owner-value guardrail**
Internal/admin visibility over a domain does not by itself make admin a valid owner value for that domain’s canonical table.

16.7 **Retained actor record doctrine**
User accounts are retained actor records.

16.8 **Ordinary lifecycle rule for users**
A user may close, deactivate, anonymize, or release reusable identifiers from an account, but ordinary product behavior must not hard-delete the canonical user row when that user has transaction, audit, authorization, commerce, or activity history.

16.9 **Exceptional hard-delete rule for users**
Hard deletion of users is exceptional administrative cleanup only, generally limited to erroneous/test records or legally required erasure after approved retention handling.

16.10 **Actor foreign-key deletion rule**
Actor foreign keys may use `ON DELETE SET NULL` only as a safety valve for exceptional hard deletion, not as the normal lifecycle path.

16.11 Detailed merchant product behavior, consumer product behavior, delegation workflows, route families, and serializer families belong in companion architecture documents.

---

### 17. API and Persistence Separation

17.1 Persist canonical backend values and transform for API/UI as needed.

17.2 Store UTC timestamps, not local presentation time.

17.3 Store normalized status values, not presentation labels.

17.4 Secret-bearing models must reflect canonical persisted form.

17.5 Public-safe serializers must remain distinct from privileged internal serializers.

17.6 Persistence shape and transport shape must not be conflated.

17.7 Time-source ownership belongs with persistence rules: DB-owned time for stored lifecycle state, application reference time for non-persisted read-side or orchestration concerns.

17.8 Token-bearing credentials follow the same separation rule: plaintext only at controlled boundaries, protected form in canonical persistence.

17.9 User-owned configuration surfaces must preserve actor-appropriate ownership semantics.

17.10 Route-family topology, endpoint-family topology, and product-surface topology belong in companion architecture documents, not BEG.

---

### 18. Policy-Driven Architecture

18.1 **Business policy must not be hard-coded**
Engineering must not encode operational business decisions directly in source code when those decisions can reasonably be represented as configurable data.

**18.2 Capabilities are permanent; policies are variable**
Engineering builds platform capabilities. Administration determines when, how, and under what policy those capabilities are exercised through governed configuration rather than code changes.

18.3 **Separate capability from configuration**
The existence of a feature must not be coupled to whether it is currently enabled, charged, required, or waived.

18.4 **Admin-controlled platform rule**
Operational business decisions should be manageable through administrative tooling wherever practical.

18.5 **Prefer data over constants**
Avoid source-code constants for operational values. Prefer database configuration, fee schedules, policy tables, entitlement tables, feature flags where appropriate, and environment configuration for deployment concerns.

18.6 **Engineering boundary**
Engineers implement business capabilities. Product, Operations, Finance, Trust & Safety, and Support exercise those capabilities through administrative configuration.

18.6A **Operational capability doctrine**

Engineering is responsible for implementing complete operational capabilities together with the extension points required to support future evolution. Administrative operation of those capabilities belongs outside the code.

**Engineering must:**
* implement the complete operational capability;
* expose governed configuration and extension points where operational variation is expected;
* avoid hard-coded commercial, operational, or policy decisions that can reasonably be represented as configurable data;
* ensure supported operational changes can be performed through administrative configuration without source-code modification, recompilation, or redeployment;
* preserve safe defaults, validation, and guardrails; and
* keep provider selection, policy selection, and implementation composition outside canonical domain contracts whenever practical.

**Administration is responsible for enabling, disabling, configuring, and operating those capabilities through approved administrative mechanisms.**

18.6B **Engineering invariants are not policy**
Engineering invariants are not policy. Administrative configuration may govern commercial and operational behavior, but it must not weaken or override the engineering invariants that preserve the platform's correctness, security, integrity, auditability, or reliability.

§18.6C **Consumer Identity Sovereignty**

Engineering shall preserve consumer identity sovereignty throughout the Future Offering engagement lifecycle. Future Offering capabilities shall not disclose consumer identity or personal information to merchants as part of ordinary engagement. Where the platform supports progression to a merchant-facing commercial stage, engineering shall ensure that any transition to a direct merchant-consumer relationship occurs only through explicit consumer authorization using platform-authenticated capabilities. Administrative configuration shall not weaken or bypass this invariant.

18.7 **Waiver-does-not-remove-capability rule**
Engineering invariants are not administrative policy. Administrative configuration may govern commercial and operational behavior, but it must not weaken, disable, bypass, or override the engineering invariants that preserve the platform’s correctness, security, data integrity, auditability, reliability, authorization boundaries, transactional consistency, or other non-negotiable system guarantees.

Administrative flexibility must operate only within the safe boundaries established by Engineering. Where a configurable policy would conflict with an engineering invariant, the engineering invariant takes precedence.

18.8 **Examples of variable policy domains**
This doctrine applies to pricing, fees, credits, promotional programs, onboarding requirements, trust policies, platform operating rules, billing requirements, invoice generation, payment collection, and similar operational business controls.

### 19. Services, Transactions, and Workflow Doctrine

19.1 Protect invariants using database constraints plus service validation where appropriate.

19.2 Application validation complements database integrity; it does not replace it.

19.3 Workflow-heavy concerns belong primarily in services and workflow-supporting structures unless a specific relational constraint belongs in the canonical table.

19.4 Use transactions where atomicity matters.

19.5 Keep transactions short.

19.6 Do not wrap unrelated operations together.

19.7 Validation-normalized values used in a write transaction must be the same canonical values that were actually validated.

19.8 Merchant-owned mutations, consumer-owned mutations, and internal/admin mutations must preserve correct actor scope, but detailed workflow architecture belongs in product-specific documents.

---

### 20. Event and Outbox Doctrine

20.1 Where a database write must reliably cause downstream publication or async processing, use an outbox pattern.

20.2 Business write and outbox insert must occur in one transaction.

20.3 Publication must be retryable.

20.4 Consumers must tolerate duplicate delivery.

20.5 Failure must be observable.

20.6 Secret-bearing events must never publish plaintext secrets or reversible secret payloads unless explicitly approved and protected.

20.7 Token-related events must not publish plaintext bearer material.

20.8 Event payloads must preserve the correct actor and ownership semantics where relevant.

---

### 21. Audit and Observability Doctrine

21.1 Important backend actions must be auditable.

21.2 Audit should cover create, update, delete, approve/reject, moderation, role/permission changes, sensitive reads, and critical automation.

21.3 Audit logs must not expose secrets.

21.4 Automated assignments and moderation actions must be traceable to source where provenance matters.

21.5 Secret creation, rotation, revocation, and validation changes should be auditable without logging secret values.

21.6 Sensitive privileged reads should be auditable.

21.7 Anonymous public catalog reads normally belong in access logs, metrics, CDN logs, and analytics, not per-request audit rows.

21.8 Use structured logs with meaningful context.

21.9 Do not use scattered console-style debugging in production paths.

21.10 Distinguish hard failures, soft failures, and audit events.

21.11 Logs, traces, metrics labels, panic handlers, and debug dumps must not expose plaintext secrets or reversible credential material.

21.12 Where observability uses application reference time, it should use the canonical UTC helper.

21.13 Bearer tokens must never appear in logs, traces, panic output, or debug dumps in plaintext.

21.14 **Metrics contracts must be canonical before startup**
Pre-production metric names, labels, and buckets must be corrected to the clean operational contract, even when that requires call-site updates.

21.15 **Metrics labels must control cardinality**
Unbounded identifiers such as `user_id`, `offer_id`, `merchant_id`, and `editor_id` must not be added casually. When temporarily retained, they must be explicitly documented as high-cardinality labels and reviewed before production fan-out.

21.16 **Duration histograms must match workload class**
HTTP/default latency buckets must not be reused for async worker, AI, batch, notification, or moderation workloads when those operations routinely exceed request-latency ranges.

---

### 22. Validation and Canonicalization Doctrine

22.1 URL fields must reject user-info components and decoded traversal path segments.

22.2 Validation must not approve URLs that embed credentials or that resolve to traversal semantics after decoding or normalization.

22.3 Where validators trim or canonicalize input, they must return the canonical validated value for downstream persistence and comparison.

22.4 Callers must not continue using the pre-trimmed or pre-normalized input after validation succeeds.

22.5 Validation/canonicalization steps should be idempotent: repeated normalization of the same accepted input should resolve to the same canonical validated value.

---

### 23. Indexing Policy

23.1 Indexes must support real query patterns.

23.2 Common candidates include foreign keys, status fields, active/non-deleted filters, date-range queries, and unique readable-key lookups.

23.3 Avoid speculative over-indexing.

23.4 User-owned configuration domains should index real owner-scope query paths according to valid actor classes.

23.5 Token-revocation and blacklist query paths should be indexed according to real validation and revocation checks.

23.6 Product-specific indexing rules such as merchant-bound operational query families belong in the relevant product architecture document, though the general principle remains: index for real query patterns.

---

### 24. Seeded Reference Data Policy

24.1 Reference and lookup data required for defaults, workflow initialization, authorization, auditing, or lifecycle transitions are part of the system contract.

24.2 Required seed data must exist before the application is considered ready.

24.3 If missing seed data would break ordinary write paths, startup must fail fast.

24.4 Examples include status tables, notification types/channels, audit action metadata, role/permission metadata, and other reference data required for ordinary operation.

---

### 25. Migration Doctrine

25.1 Schema changes must be production-aware.

25.2 Preferred sequence:

* additive change
* safe backfill
* deploy behavior change
* enforce new constraint
* remove deprecated path later

25.3 Where lifecycle analysis shows schema is incomplete, correct the schema and align downstream code accordingly.

25.4 Where token tables still store plaintext or use delete instead of revocation, migrate them toward protected persistence and explicit revocation semantics.

25.5 Where ownership values are invalid because governance visibility was confused with ownership, restore actor-appropriate owner semantics.

25.6 Where validators normalize input but callers still use the original value, align code paths to use the canonical validated value.

25.7 Where URL fields still allow user-info or decoded traversal semantics, tighten validation and remove acceptance of unsafe forms.

25.8 Where JWT signing paths still drift toward HMAC in production-oriented code, migrate them toward Ed25519 signing and public-key validation.

25.9 Where user lifecycle behavior still treats actor rows as ordinarily disposable despite retained history, migrate toward closure, deactivation, anonymization, and reusable-identifier release flows instead of ordinary hard deletion.

---

### 26. Retry and Idempotency Doctrine

26.1 Operations that may be retried must be safe under retry conditions.

26.2 Use idempotency keys, deduplication, or explicit status transitions where appropriate.

26.3 Token revocation and blacklist flows must be idempotent.

26.4 Validation/canonicalization steps should also be idempotent.

26.5 Product-specific retry behavior belongs in the relevant product architecture documents, but the doctrine of idempotent retry remains horizontal.

---

### 27. Chief Engineer Review and Correction Doctrine

27.1 The Chief Engineer determines whether persistence and lifecycle design are correct. Current schema shape is evidence, not authority.

27.2 Review order:

* inspect current model behavior
* inspect current table shape
* decide whether lifecycle/design is actually correct
* correct schema where needed
* then align models, handlers, comments, seed data, and contracts

27.3 Review must not rubber-stamp omissions.

27.4 When reviewing handler surfaces, determine whether the domain belongs to public catalog, authenticated consumer, a product-specific merchant surface, or internal/admin governance.

27.5 If surfaces differ materially, authorize distinct DTOs/serializers rather than forcing one internal shape everywhere.

27.6 When reviewing readable-key design, distinguish scarce public identity handles from ordinary domain-local readable keys.

27.7 When reviewing validation code, ensure URL validators reject user-info and decoded traversal path segments, and ensure validators that trim or canonicalize input return the canonical value downstream code will use.

27.8 Product-specific conclusions should be moved into the correct product architecture documents rather than expanding BEG beyond its horizontal scope.

27.9 When reviewing auth and JWT code, reject drift back toward HMAC as canonical production architecture unless explicitly justified as temporary migration or isolated prototype code.

27.10 When reviewing low-level package boundaries, preserve correct ownership: security packages own low-level security sentinels, persistence packages own persistence-facing sentinels, and boundary layers perform translation where needed.

27.11 In pre-startup code, prefer canonical correction over compatibility preservation. Require downstream call-site repair when that is what correctness, clarity, security, or observability demands.

27.12 Refactors must preserve or improve exported-identifier godoc quality and must not trade away useful documentation context in the name of brevity.

27.13 During centralized-file reviews, absence from the prompt must not be treated as absence from the codebase. Central/shared files such as `errors.go`, `helpers.go`, `scan.go`, `commons.go`, `jwtutil`, `security.go`, and `metrics.go` must be treated as authoritative before local duplication is introduced.

27.14 In user-lifecycle reviews, the default assumption for real user accounts with history is retention, not ordinary hard deletion.

---

### 28. Guidance Document Voice and Attribution Policy

28.1 Within BEG documents, the line **“Thanks, Sagrenti BE”** is an acknowledgment from the Sagrenti Chief Engineer to the Sagrenti Backend Engineers who read and implement the guidance.

28.2 It is not a farewell or conversational sign-off.

28.3 The guidance voice is that of the Chief Engineer issuing standards, corrections, and architectural direction to the backend engineering function.

28.4 In this context, **Sagrenti BE** means the backend engineering team role and implementation audience.

Thanks, Sagrenti BE
