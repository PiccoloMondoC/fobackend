// Package services contains business logic for internal operations such as moderation,
// automation, audit logging, and data synchronization. It is used by async routines
// and internal system workflows, not exposed via public API routes.
//
// sdworkspace/sdbackend/internal/services/async/user_activation_async.go
package services

/*
func (s *Service) DeleteExpiredActivationTokensAsync()

{
	Name:     "cleanup_activation_tokens",
	Interval: 1 * time.Hour,
	Action: func(ctx context.Context, service *services.Service) {
		logger := service.Logger.WithFunctionName("CleanupActivationTokensJob")

		ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()

		err := service.ActivationTokens.DeleteExpiredActivationTokens(ctx)
		if err != nil {
			logger.Error("failed to delete expired activation tokens", "error", err)
		} else {
			logger.Info("expired activation tokens successfully deleted")
		}
	}
}*/
