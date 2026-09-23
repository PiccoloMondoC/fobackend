// Package services intentionally provides no asynchronous merchant program
// subscription-period workflow at this stage.
//
// focodebase/fobackend/internal/services/merchant_program_subscription_periods_async.go
//
// GTM:
//
//	Layer: 2.4 Merchant / Future Offering Domain
//	Release Class: SPINE
//	Reason:
//	  Merchant program subscription periods are immutable source facts whose
//	  creation and resolution are synchronous responsibilities.
//
//	  Period creation may participate in caller-owned transactional workflows.
//	  Moving that creation into detached background execution would weaken the
//	  atomicity boundary between subscription state, lifecycle history, period
//	  history, and downstream monetization facts.
//
//	  No approved queue consumer, scheduled job, retry worker, provider
//	  callback, batch processor, reconciliation workflow, outbox consumer, or
//	  other independent asynchronous responsibility belongs to this domain.
//
//	  This file records that deliberate boundary and prevents asynchronous
//	  behavior from being introduced merely to mirror another service vertical.
//
// SPINE Rule:
//
//	Keep compiling.
//	Preserve synchronous period resolution.
//	Preserve transaction-compatible period creation.
//	Do not move transaction-dependent period creation into background work.
//	Do not add direct database access, SQL, handler concerns, permissions,
//	auditing, fee calculation, invoicing, payment, or commercial policy.
//	Do not add detached goroutines, speculative retries, polling loops, or
//	unbounded background work.
//	Add asynchronous behavior only when a distinct durable operational
//	responsibility is explicitly approved with defined ownership, shutdown,
//	observability, idempotency, and failure semantics.
//	Block deployment if this file weakens subscription-period integrity or
//	introduces ungoverned background behavior.
package services
