// Package services intentionally provides no asynchronous merchant program
// fee-schedule workflow at this stage.
//
// focodebase/fobackend/internal/services/merchant_program_fee_schedules_async.go
//
// GTM:
//
//	Layer: 2.4 Merchant / Future Offering Domain
//	Release Class: SPINE
//	Reason:
//	  Merchant program fee-schedule readiness and effective commercial-policy
//	  resolution are synchronous monetization responsibilities.
//
//	  No approved background reconciliation, cache invalidation, scheduled
//	  inspection, or other asynchronous fee-schedule workflow currently
//	  exists. This file records that deliberate boundary and prevents an
//	  unjustified goroutine wrapper from being introduced merely to mirror
//	  another service vertical.
//
// SPINE Rule:
//
//	Keep compiling.
//	Do not move monetization-readiness validation into background execution.
//	Do not add direct database access, SQL, handler concerns, permissions,
//	auditing, fee calculation, or commercial-policy mutation.
//	Do not add detached goroutines, unbounded loops, or speculative retry
//	behavior.
//	Add asynchronous behavior only when a distinct operational responsibility
//	is explicitly approved and has bounded context, shutdown, observability,
//	and failure semantics.
//	Block deployment if this file weakens synchronous commercial-policy
//	readiness or introduces ungoverned background work.
package services
