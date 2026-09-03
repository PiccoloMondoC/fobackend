# PCDF-M01 — Future Offering Creation

## Capability / Delivery Sheet — Phase 6

### 1. Product Purpose

Enable a merchant to create a Future Offering project and bring it through the appropriate creation process to submission.

M01 is one coherent merchant capability. The individual database records, Go files, APIs, Angular components, and workflow steps are implementation contributors to that capability; they are not separate product capabilities.

### 2. Merchant Must Be Able To

The merchant must be able to:

* begin a new Future Offering project;
* save an incomplete FO as work in progress;
* leave and later resume the creation process;
* configure the FO information required for the services that will follow;
* add and manage the required FO assets;
* select the applicable merchant-configurable engagement options;
* define the FO's goals where required;
* establish the applicable service/commercial choices required during creation;
* establish initial project milestones where appropriate;
* review the completed FO before submission;
* correct incomplete or invalid information;
* submit the FO through the appropriate activation action.

FO creation is a guided product workflow. Activation is not a separate wizard. FOPSC requires the creation workflow to establish the authoritative facts needed by subsequent Platform services.

### 3. Capability Boundary

M01 begins when an authorized merchant starts creating a new FO.

M01 ends when the creation workflow has been validly submitted through the appropriate activation transition.

M01 does **not** include:

* ongoing management of an established FO — M02;
* continuing milestone/project management — M03;
* Anticipation Intelligence — M05 / Phase 7;
* the consumer engagement experience — Phase 8;
* FO funding/account management — M07;
* billing and reconciliation — M08.

The existence of engagement configuration, billing terms, milestones, and other underlying facts during creation does not move those later capabilities into M01. M01 establishes the facts those capabilities require.

### 4. Backend Contributors Already Identified

Phase 6 Release Classification identifies the following durable Go domains as contributors to the FO project:

* `merchant_future_offerings.go`
* `merchant_future_offerings_assets.go`
* `merchant_future_offering_engagement_options.go`
* `merchant_future_offering_goals.go`
* `merchant_future_offering_billing_terms.go`
* `merchant_future_offerings_events.go`
* `merchant_future_offering_milestones.go`

It also identifies:

* `future_offering_project_management.go`
* `future_offering_project_actions.go`

as Phase 6 derived capabilities. These primarily support M02/M03, although portions may become relevant to the creation experience where project state or next-action derivation is needed.

Release Classification expressly establishes that the FO creation workflow is primarily frontend/orchestration over these authoritative FO domains rather than a single backend file.

### 5. First Coding Target

The first implementation target is:

`merchant_future_offerings.go`

This is the authoritative FO domain around which the remaining M01 contributors depend.

We will complete its production vertical slice before blindly moving through the Phase 6 file list:

```text
Database / canonical facts
        ↓
Data-layer Go
        ↓
Service behavior
        ↓
Handler / API
        ↓
Angular consumption
        ↓
Working merchant experience
```

As each additional M01 domain becomes necessary, we add it to the working capability rather than completing files merely because they appear next in Release Classification.

### 6. Backend Work To Establish During Implementation

For M01, the backend must collectively provide:

* authoritative creation of a merchant-owned FO;
* retrieval of the merchant's incomplete FO for resumption;
* permitted mutation while the FO remains in the creation lifecycle;
* validation of authoritative FO facts;
* ownership and authorization enforcement;
* coordinated persistence of the supporting creation domains;
* lifecycle handling for submission/activation;
* reliable preservation of relevant FO events/history;
* transactional correctness where multiple facts must change atomically;
* appropriate domain-event/outbox publication where submission or another durable occurrence must be consumed independently.

No new orchestration Go file is assumed in advance.

If implementation proves that creation/submission contains a coherent backend responsibility that does not properly belong to the existing domain services, we will define that service capability then and add it to Release Classification.

### 7. API Required

The API must provide the Angular workflow with enough capability to:

* create an FO draft;
* retrieve an FO draft;
* update the mutable FO creation state;
* manage creation-time supporting facts through their authoritative domains;
* determine validation/readiness for submission;
* perform the submission/activation action;
* return useful validation and conflict failures to the frontend.

The exact endpoint topology will be settled against the existing API conventions while implementing the relevant backend slices.

We will not invent a separate endpoint merely for every Angular screen or workflow step.

### 8. Angular Experience Required

M01 requires an Angular **Future Offering Creation feature/workflow**.

The initial frontend classification for this capability must eventually identify:

* merchant route into FO creation;
* creation workflow container;
* logical workflow steps;
* draft loading and resumption;
* FO core-information editing;
* asset management;
* engagement-option configuration;
* goal configuration;
* required service/commercial selections;
* initial milestone configuration where appropriate;
* validation and readiness presentation;
* review experience;
* submission/activation action;
* loading states;
* recoverable error states;
* authorization/permission handling;
* API client/service integration;
* frontend models/state required by the workflow;
* automated tests appropriate to the feature.

Actual Angular filenames and component boundaries will be established from the real `sdfrontend` structure when implementation reaches them. We will not create a generic Angular taxonomy beforehand.

### 9. Administration / Configuration Boundary

M01 must consume governed Platform configuration wherever operational variation is intended.

Engineering provides the capability, supported values, validation, dependencies, safe defaults, and invariant boundaries.

Administration determines permitted operational choices within those boundaries.

The FO creation frontend must therefore not hard-code commercial or operational policy that belongs to Platform configuration.

### 10. Product Completion Test

M01 is **not complete** when `merchant_future_offerings.go` compiles.

It is not complete when all Phase 6 M01 backend files compile.

It is not complete when an API request succeeds manually.

It is complete when, through the actual Sagrenti merchant experience, an authorized merchant can successfully:

```text
Start FO
   ↓
Save draft
   ↓
Leave
   ↓
Resume
   ↓
Configure required FO facts
   ↓
Add required supporting information
   ↓
Receive meaningful validation
   ↓
Review
   ↓
Submit / Activate
   ↓
Receive a correct established result
```

and the resulting authoritative backend state, lifecycle history, and relevant supporting facts are correct.

PCDF requires completion to be observable through the working product, with the required backend and frontend layers operating successfully together.

### 11. Working Rule

This sheet is an implementation aid, not another architecture project.

We update it only when coding reveals a material correction to the capability boundary, contributing artifacts, API requirement, frontend requirement, or completion test.

**Next action: begin the CE review and implementation of `merchant_future_offerings.go`.**
