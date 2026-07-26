// Package services contains trusted internal service composition,
// automation support, moderation helpers, and shared internal workflow logic.
//
// sdworkspace/sdbackend/internal/services/internal-services/notification-jobs.go
//
// GTM:
//
//	Layer: 2.6 Internal Services / Automation Foundation
//	Release Class: SPINE
//	Reason:
//	  Defines governed internal notification-job entry points used by the
//	  automation orchestrator.
//
//	  The maintenance notification entry point remains present for caller
//	  compatibility but is intentionally disabled until legitimate system
//	  identity, recipient selection, message configuration, and delivery
//	  accountability are supplied by composition.
//
// SPINE Rule:
//
//	Keep compiling.
//	Keep production-ready.
//	Never generate random actor or recipient identities.
//	Never fabricate notification recipients or operational message content.
//	Never silently claim successful delivery when dispatch did not occur.
//	Preserve the orchestrator Job.Action signature until coordinated migration.
//	Do not enable this job without governed identity, recipient selection,
//	configuration, observability, retry, and idempotency contracts.
package services

import "context"

// SendSystemMaintenanceNotificationAsync preserves the orchestrator's
// Job.Action signature.
//
// The former implementation generated random recipient and actor UUIDs and
// therefore could not produce a valid production notification. The job remains
// deliberately disabled until composition supplies a governed system actor,
// an explicit recipient-selection contract, and operational message data.
func SendSystemMaintenanceNotificationAsync(
	ctx context.Context,
	svc *Service,
) {
	if ctx == nil || svc == nil || svc.Logger == nil {
		return
	}

	logger := svc.Logger.
		GetLoggerWithContextFromContext(ctx).
		WithFunctionName("SendSystemMaintenanceNotificationAsync")

	logger.Warn(
		"System maintenance notification job is disabled",
		"error", ErrMaintenanceJobDisabled,
	)
}
