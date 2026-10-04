// focodebase/fobackend/internal/deployment_readiness_register.go
package deployment

// Sagrenti Deployment Readiness Register
//
// PURPOSE
//
// This file is the authoritative live register for engineering work that has
// been deliberately deferred but must be resolved, verified, or explicitly
// accepted before production deployment.
//
// It is intentionally non-executable. Entries are engineering control records,
// not runtime configuration.
//
// GOVERNANCE
//
//  1. A release-blocking issue may not be deferred merely by mentioning it in
//     an implementation report, review, chat, or TODO elsewhere.
//
//  2. When CE accepts a production-readiness issue for later resolution, CE
//     records it here as part of the same engineering work.
//
//  3. The user is not responsible for remembering, discovering, or maintaining
//     this register.
//
//  4. An entry remains OPEN until its closure condition has been satisfied and
//     verified.
//
//  5. If an issue belongs to the capability currently being completed and
//     nothing prevents its completion, it should be completed now rather than
//     entered here.
//
//  6. Before production deployment, CE must review every OPEN entry. Production
//     readiness cannot be declared while an unresolved release-blocking entry
//     remains.
//
//  7. OPEN entries are ordered newest-first. A newly opened entry is added at
//     the top of the OPEN section.
//
//  8. CLOSED entries are ordered by closure, oldest-first. When an entry is
//     closed, it is moved from OPEN to the bottom of the CLOSED section together
//     with its closure evidence.
//
// ============================================================================
// OPEN
// ============================================================================
//
// DRR-002 — Email enumeration resistance: notification response timing
//
// Capability:
//   Authentication / Notifications
//
// Reason deferred:
//   Public responses intentionally do not disclose whether an email address
//   belongs to an account, but existing and unknown addresses may currently
//   require different amounts of backend work.
//
// Architectural dependency:
//   Asynchronous notification / outbox delivery.
//
// Closure condition:
//   Public confirmation/resend workflows no longer expose a practically useful
//   account-existence signal through materially different synchronous work, and
//   the behavior has been verified.
//
// ---------------------------------------------------------------------------
//
// DRR-001 — Email confirmation: retire legacy /activate compatibility route
//
// Capability:
//   Authentication / Email Confirmation
//
// Reason deferred:
//   Previously issued confirmation emails may still contain /activate links.
//   Removing the compatibility route immediately could invalidate those emails.
//
// Closure condition:
//   The maximum lifetime of all confirmation links issued before the
//   /confirm-email transition has elapsed, including an appropriate operational
//   margin, and the legacy route is no longer required.
//
// Relevant code:
//   fofrontend/src/app/auth/auth.routes.ts
//
// ============================================================================
// CLOSED
// ============================================================================
//
// Closed entries are retained below with their closure evidence so this file
// remains a record of deployment-readiness decisions.