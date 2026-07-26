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
