// Package services contains business logic for internal operations such as moderation,
// automation, audit logging, and data synchronization. It is used by async routines
// and internal system workflows, not exposed via public API routes.
//
// sdworkspace/sdbackend/internal/services/retry_queue.go
package services

import (
	"github.com/PiccoloMondoC/sdworkspace/sdbackend/internal/shared/models"
)

// EnqueueFailedCuratedOfferForRetry sends a failed offer to your preferred queue
// (DB table, Redis list, NATS JetStream…).
// A no‑op stub keeps compilation green until the queue is wired‑up.
func (s *Service) EnqueueFailedCuratedOfferForRetry(offer models.OfferLite) error {
	// TODO: replace with real enqueue logic
	s.Logger.Warn("enqueue fallback retry – stub implementation",
		"offer_id", offer.ID, "merchant_id", offer.MerchantID)
	return nil
}
