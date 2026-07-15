// Package services contains business logic for internal operations such as moderation,
// automation, audit logging, and data synchronization. It is used by async routines
// and internal system workflows, not exposed via public API routes.
//
// sdworkspace/sdbackend/internal/services/async/orchestrator.go
package services

import (
	"context"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// ───────────────────────────────────────────────────────────────────────────────
// Metrics
// ───────────────────────────────────────────────────────────────────────────────

// MetricAutomationLastTick reports the last time each job ran (UNIX seconds).
var MetricAutomationLastTick = prometheus.NewGaugeVec(
	prometheus.GaugeOpts{
		Name: "automation_last_tick_unix",
		Help: "UNIX timestamp of the last tick for each automation job.",
	},
	[]string{"job"},
)

func init() {
	prometheus.MustRegister(MetricAutomationLastTick)
}

// ───────────────────────────────────────────────────────────────────────────────
// Orchestrator types
// ───────────────────────────────────────────────────────────────────────────────

// Job describes a periodic background task executed by the orchestrator.
type Job struct {
	Name     string
	Interval time.Duration
	Action   func(context.Context, *Service)
}

// ───────────────────────────────────────────────────────────────────────────────
// Public entry‑point
// ───────────────────────────────────────────────────────────────────────────────

// StartOfferAutomationOrchestrator spins up one goroutine per scheduled job and
// listens for a shutdown signal on service.ShutdownChan.
//
// Call this exactly **once** during application bootstrap.
func StartOfferAutomationOrchestrator(service *Service) {
	logger := service.Logger.WithFunctionName("StartOfferAutomationOrchestrator")
	logger.Info("launching automation orchestrator")

	// Root context that is cancelled on graceful shutdown.
	ctx, cancel := context.WithCancel(context.Background())

	// Forward the shutdown signal injected by main.go.
	go func() {
		<-service.ShutdownChan
		logger.Info("automation orchestrator received shutdown signal")
		cancel()
	}()

	// Register all periodic jobs here.
	jobs := []Job{
		// Offer‑centric automations
		{
			Name:     "fraud_detection",
			Interval: 30 * time.Minute,
			Action:   DetectFraudulentOffersAsync,
		},
		{
			Name:     "auto_expire_offers",
			Interval: 1 * time.Hour,
			Action:   AutoExpireOffersAsync,
		},
		{
			Name:     "remove_expired_offers",
			Interval: 2 * time.Hour,
			Action:   RemoveExpiredOffersAsync,
		},
		{
			Name:     "auto_flag_offers",
			Interval: 4 * time.Hour,
			Action:   ListOffersToAutoFlagAsync,
		},
		{
			Name:     "suggest_offers",
			Interval: 2 * time.Hour,
			Action: func(ctx context.Context, svc *Service) {
				log := svc.Logger.WithFunctionName("SuggestOffersJob")

				userIDs, err := svc.ListEligibleUsersForSuggestionsInternal(ctx)
				if err != nil {
					log.Error("failed to list users for suggestions", "error", err)
					return
				}

				for _, uid := range userIDs {
					// Pass the UUID directly (no .String()).
					SuggestOffersForUserAsync(ctx, svc, uid)
				}

				log.Info("suggestion job complete", "user_count", len(userIDs))
			},
		},

		// Favorite‑centric automations
		{
			Name:     "auto_expire_old_favorites",
			Interval: 1 * time.Hour,
			Action:   AutoExpireOldFavoritesAsync,
		},
		{
			Name:     "expire_inactive_favorites",
			Interval: 1 * time.Hour,
			Action:   BatchSoftDeleteInactiveFavoritesAsync,
		},
		{
			Name:     "purge_soft_deleted_favorites",
			Interval: 2 * time.Hour,
			Action:   PurgeDeletedFavoritesAsync,
		},
		{
			Name:     "system_notifications",
			Interval: 6 * time.Hour,
			Action:   SendSystemMaintenanceNotificationAsync, // ⬅️  wrapper, not InsertNotificationAsync
		},
	}

	// Kick off each job in its own ticker loop.
	for _, job := range jobs {
		j := job // loop variable capture
		go func() {
			ticker := time.NewTicker(j.Interval)
			defer ticker.Stop()

			for {
				select {
				case <-ctx.Done():
					logger.Info("stopping automation job", "job", j.Name)
					return
				case <-ticker.C:
					setGaugeToCurrentTime(MetricAutomationLastTick, j.Name)
					logger.Debug("running automation job", "job", j.Name)
					j.Action(ctx, service)
				}
			}
		}()
	}
}

// ───────────────────────────────────────────────────────────────────────────────
// Helpers
// ───────────────────────────────────────────────────────────────────────────────

// setGaugeToCurrentTime updates a Prometheus gauge vec label to the
// current UNIX timestamp.
func setGaugeToCurrentTime(g *prometheus.GaugeVec, labels ...string) {
	g.WithLabelValues(labels...).Set(float64(time.Now().Unix()))
}