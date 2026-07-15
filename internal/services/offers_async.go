// Package services contains business logic for internal operations such as moderation,
// automation, audit logging, and data synchronization. It is used by async routines
// and internal system workflows, not exposed via public API routes.
//
// sdworkspace/sdbackend/internal/services/offers_async.go
//
// GTM:
//   Layer: 2.5 Catalog / Offer Domain
//   Release Class: SPINE
//   Reason:
//     Owns the asynchronous, fire-and-forget dispatch layer for the canonical
//     offer lifecycle operations defined in offers_internal.go. Provides the
//     production resilience contract that background workers and event consumers
//     depend on.
//
// SPINE Rule:
//   Keep compiling.
//   Keep production-ready.
//   Preserve non-blocking fire-and-forget dispatch semantics.
//   Preserve 10s context timeout and panic recovery on every goroutine.
//   Preserve Prometheus metrics and OpenTelemetry span coverage per operation.
//   Preserve delegation-only contract.
//   Block deployment if this file breaks build, async dispatch reliability,
//   observability coverage, or catalog integrity.
package services

import (
	"context"
	"fmt"
	"time"

	"github.com/PiccoloMondoC/sdworkspace/sdbackend/internal/observability/metrics"
	"github.com/PiccoloMondoC/sdworkspace/sdbackend/internal/shared/models"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel"           // Optional observability
	"go.opentelemetry.io/otel/attribute"
)

// CuratedOfferEvent represents a offer that has passed AI and editorial review.
type CuratedOfferEvent struct {
	Offer *models.OfferLite
}

// ApproveOfferEvent encapsulates the offer approval event payload.
// This is used to approve a curated offer asynchronously.
type ApproveOfferEvent struct {
	OfferID     string // UUID string of the offer to approve
	ApprovedBy string // UUID string of the editor/user performing the approval
}

// RejectOfferEvent encapsulates the offer rejection event payload.
// This is used to reject a curated offer asynchronously.
type RejectOfferEvent struct {
	OfferID     string // UUID string of the offer to reject
	RejectedBy string // UUID string of the moderator/user performing the rejection
}

// UpdateOfferStatusEvent encapsulates the payload for updating offer status asynchronously.
type UpdateOfferStatusEvent struct {
	OfferID   string // UUID string of the offer
	NewStatus string // New status to apply ("approved", "expired", etc.)
	ActorID  string // UUID string of the user/system making the update
}

// FlagOfferEvent represents the payload to flag a offer for internal review.
type FlagOfferEvent struct {
	OfferID    string  // UUID of the offer to flag
	FlaggedBy *string // Optional UUID of the user (nil = system flag)
	Reason    string  // Reason for flagging
}

// BlacklistOfferEvent encapsulates the payload for offer blacklisting.
type BlacklistOfferEvent struct {
	OfferID        string // UUID string of the offer to blacklist
	BlacklistedBy string // UUID string of the user blacklisting the offer
}


// InsertCuratedOfferAsync persists a curated offer in the background.
//
// Key guarantees
// --------------
// • Non‑blocking: fires a goroutine and returns immediately.  
// • Resilient: 10 s timeout, panic‑safe, metrics, tracing, structured logs.  
// • Self‑healing: on any error the offer is added to a retry queue.  
// • Side‑effects: kicks off GenerateOfferDescriptionAsync if the insert succeeds.
//
func InsertCuratedOfferAsync(parentCtx context.Context, svc *Service, ev CuratedOfferEvent) {
	go func() {
		start := time.Now()

		ctx, cancel := context.WithTimeout(parentCtx, 10*time.Second)
		defer cancel()

		log := svc.Logger.WithFunctionName("InsertCuratedOfferAsync")

		// ─── OpenTelemetry span ────────────────────────────────────────────────
		tr := otel.Tracer("offers.async")
		ctx, span := tr.Start(ctx, "InsertCuratedOfferAsync")
		defer span.End()

		// ─── Panic‑safety & metrics finaliser ─────────────────────────────────
		defer func() {
			metrics.OfferOperationDuration.WithLabelValues("insert").
				Observe(time.Since(start).Seconds())

			if r := recover(); r != nil {
				err := fmt.Errorf("panic: %v", r)
				log.Error("goroutine panic", err)
				span.RecordError(err)

				if ev.Offer != nil {
					merchantID := ev.Offer.MerchantID.String()
					metrics.OfferInsertResult.WithLabelValues("failure", merchantID).Inc()
					_ = svc.EnqueueFailedCuratedOfferForRetry(*ev.Offer)
				}
			}
		}()

		// ─── Input validation ────────────────────────────────────────────────
		if ev.Offer == nil {
			log.Error("nil offer event payload")
			span.SetAttributes(attribute.Bool("offer.nil", true))
			metrics.OfferInsertResult.WithLabelValues("failure", "unknown").Inc()
			return
		}
		merchantID := ev.Offer.MerchantID.String()

		// ─── Insert business logic ───────────────────────────────────────────
		if err := svc.InsertCuratedOfferInternal(ctx, ev.Offer); err != nil {
			log.Error("insert failed",
				"offer_id", ev.Offer.ID,
				"merchant_id", merchantID,
				"error", err)
			span.RecordError(err)
			metrics.OfferInsertResult.WithLabelValues("failure", merchantID).Inc()

			// Retry later
			if retryErr := svc.EnqueueFailedCuratedOfferForRetry(*ev.Offer); retryErr != nil {
				log.Error("retry‑enqueue failed", "offer_id", ev.Offer.ID, "error", retryErr)
			}
			return
		}

		// ─── Success path ────────────────────────────────────────────────────
		log.Info("offer inserted",
			"offer_id", ev.Offer.ID,
			"merchant_id", merchantID)
		metrics.OfferInsertResult.WithLabelValues("success", merchantID).Inc()
		span.SetAttributes(attribute.String("status", "success"))

		// Fire follow‑up description generation (fire & forget)
		GenerateOfferDescriptionAsync(ctx, svc, ev.Offer)
	}()
}


// ApproveCuratedOfferAsync performs offer approval in a background goroutine.
//
// Production‑grade features
// -------------------------
// • Context timeout + panic‑safe defer guard                       (resilient)  
// • OpenTelemetry span with error recording                        (tracing)  
// • Prometheus counter + histogram for result + latency            (observability)  
// • Strict UUID / payload validation                               (defence in depth)  
// • Zero direct DB access – delegates to service.ApproveCuratedOfferInternal()
// • Placeholder for exponential‑back‑off retry                     (future‑safe)
func ApproveCuratedOfferAsync(ctx context.Context, service *Service, ev ApproveOfferEvent) {
	go func() {
		start := time.Now()                                   // latency timer
		defer func() {                                         // always record duration
			metrics.OfferOperationDuration.
				WithLabelValues("approve").
				Observe(time.Since(start).Seconds())
		}()

		// Panic‑recovery so one bad event never kills the worker.
		defer func() {
			if r := recover(); r != nil {
				err := fmt.Errorf("panic: %v", r)
				service.Logger.Error("panic in ApproveCuratedOfferAsync", err)
				metrics.OfferApprovalResult.
					WithLabelValues("failure", ev.ApprovedBy).Inc()
			}
		}()

		// ─────────────────────────────────── context & tracing
		innerCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		tracer := otel.Tracer("offers.async")
		innerCtx, span := tracer.Start(innerCtx, "ApproveCuratedOfferAsync")
		defer span.End()

		log := service.Logger.WithFunctionName("ApproveCuratedOfferAsync")

		// ─────────────────────────────────── payload validation
		if ev.OfferID == "" || ev.ApprovedBy == "" {
			log.Error("empty offer_id or approved_by in event")
			metrics.OfferApprovalResult.
				WithLabelValues("failure", ev.ApprovedBy).Inc()
			return
		}
		offerID, err1 := uuid.Parse(ev.OfferID)
		editorID, err2 := uuid.Parse(ev.ApprovedBy)
		if err1 != nil || err2 != nil {
			log.Error("invalid UUIDs in event",
				"offer_id", ev.OfferID, "approved_by", ev.ApprovedBy,
				"err_offer_id", err1, "err_approved_by", err2)
			span.RecordError(fmt.Errorf("invalid UUIDs"))
			metrics.OfferApprovalResult.
				WithLabelValues("failure", ev.ApprovedBy).Inc()
			return
		}

		// ─────────────────────────────────── core operation
		if err := service.ApproveCuratedOfferInternal(innerCtx, offerID, editorID); err != nil {
			log.Error("ApproveCuratedOfferInternal failed",
				"offer_id", offerID, "approved_by", editorID, "error", err)
			span.RecordError(err)
			metrics.OfferApprovalResult.
				WithLabelValues("failure", editorID.String()).Inc()
			// TODO: enqueue retry with back‑off
			return
		}

		// ─────────────────────────────────── success
		log.Info("offer approved",
			"offer_id", offerID, "approved_by", editorID)
		metrics.OfferApprovalResult.
			WithLabelValues("success", editorID.String()).Inc()
	}()
}


// RejectCuratedOfferAsync rejects a curated offer in the background.
//
// It is **fire‑and‑forget**: the caller never waits for completion.
// Safety measures included:
//   • hard 10 s context‑timeout  
//   • panic‑recovery so one bad offer can’t crash the worker pool  
//   • Prometheus duration + result metrics  
//   • OpenTelemetry span for distributed‑trace correlation  
//
// Errors are reported via structured logs and metrics only – the
// goroutine has no caller to reply to.
func RejectCuratedOfferAsync(
	parentCtx context.Context,
	service   *Service,
	event     RejectOfferEvent,
) {
	go func() { // <‑‑ detach from the request / caller
		start := time.Now()
		ctx, cancel := context.WithTimeout(parentCtx, 10*time.Second)
		defer cancel()

		logger := service.Logger.WithFunctionName("RejectCuratedOfferAsync")
		tracer := otel.Tracer("offers.events")
		ctx, span := tracer.Start(ctx, "RejectCuratedOfferAsync")
		defer span.End()

		// ─────────────────────────── panic‑safety & latency metric ───────────────────────────
		defer func() {
			metrics.OfferOperationDuration.WithLabelValues("reject").
				Observe(time.Since(start).Seconds())

			if r := recover(); r != nil {
				err := fmt.Errorf("panic: %v", r)
				logger.Error("panic in RejectCuratedOfferAsync", err)
				span.RecordError(err)
				metrics.OfferRejectionResult.WithLabelValues("failure", event.RejectedBy).Inc()
			}
		}()

		// ─────────────────────────────── input validation ───────────────────────────────
		if event.OfferID == "" || event.RejectedBy == "" {
			logger.Error("reject event missing offer_id or rejected_by")
			span.SetAttributes(attribute.Bool("event.invalid", true))
			metrics.OfferRejectionResult.WithLabelValues("failure", event.RejectedBy).Inc()
			return
		}

		offerID, err1 := uuid.Parse(event.OfferID)
		editorID, err2 := uuid.Parse(event.RejectedBy)
		if err1 != nil || err2 != nil {
			logger.Error(
				"invalid UUIDs in RejectOfferEvent",
				"offer_id",  event.OfferID,
				"rejected_by", event.RejectedBy,
				"err1", err1, "err2", err2,
			)
			span.RecordError(fmt.Errorf("uuid parse error"))
			metrics.OfferRejectionResult.WithLabelValues("failure", event.RejectedBy).Inc()
			return
		}

		// ───────────────────────────── core business logic ─────────────────────────────
		if err := service.RejectCuratedOfferInternal(ctx, offerID, editorID); err != nil {
			logger.Error(
				"RejectCuratedOfferInternal failed",
				"offer_id", offerID,
				"rejected_by", editorID,
				"error", err,
			)
			span.RecordError(err)
			metrics.OfferRejectionResult.WithLabelValues("failure", event.RejectedBy).Inc()
			return
		}

		// ─────────────────────────────── success path ───────────────────────────────
		logger.Info(
			"offer rejected",
			"offer_id", offerID,
			"rejected_by", editorID,
		)
		metrics.OfferRejectionResult.WithLabelValues("success", event.RejectedBy).Inc()
		span.SetAttributes(attribute.String("status", "success"))
	}()
}

/*
// AnalyzeMerchantPricingPatternsAsync performs asynchronous analysis of merchant pricing behavior.
//
// This background task launches a goroutine to analyze pricing strategies such as pseudo-discounts,
// volatility, and anchor pricing patterns. Results are logged and audit-logged for internal review.
//
// Features:
// - Context timeout (10s)
// - Panic recovery
// - Structured logging
// - OpenTelemetry tracing
// - Prometheus duration and result metrics
func AnalyzeMerchantPricingPatternsAsync(ctx context.Context, service *Service) {
	go func() {
		start := time.Now() // Timer for duration metric

		innerCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()

		logger := service.Logger.WithFunctionName("AnalyzeMerchantPricingPatternsAsync")

		// OpenTelemetry span
		tracer := otel.Tracer("offers.events")
		innerCtx, span := tracer.Start(innerCtx, "AnalyzeMerchantPricingPatternsAsync")
		defer span.End()

		// Recover from panic
		defer func() {
			MetricOfferOperationDuration.WithLabelValues("analyze_pricing").Observe(time.Since(start).Seconds())
			if r := recover(); r != nil {
				err := fmt.Errorf("panic: %v", r)
				logger.Error("panic in AnalyzeMerchantPricingPatternsAsync", err)
				span.RecordError(err)
				MetricMerchantPricingAnalysisResult.WithLabelValues("failure").Inc()
			}
		}()

		// Run the analysis
		err := service.AnalyzeMerchantPricingPatternsInternal(innerCtx)
		if err != nil {
			logger.Error("AnalyzeMerchantPricingPatternsAsync failed", "error", err)
			span.RecordError(err)
			span.SetAttributes(attribute.String("status", "failure"))
			MetricMerchantPricingAnalysisResult.WithLabelValues("failure").Inc()
			return
		}

		// Success path
		logger.Info("AnalyzeMerchantPricingPatternsAsync completed successfully")
		span.SetAttributes(attribute.String("status", "success"))
		MetricMerchantPricingAnalysisResult.WithLabelValues("success").Inc()
	}()
}*/


// GenerateOfferDescriptionAsync produces an AI description for a curated offer.
//
// Guarantees
// ----------
// • Non‑blocking (runs in its own goroutine).  
// • 10 s timeout, panic‑safe.  
// • Prometheus: metrics.OfferOperationDuration & metrics.OfferDescriptionResult.  
// • OpenTelemetry span + error recording.  
// • Automatic retry queueing on any failure.
func GenerateOfferDescriptionAsync(parentCtx context.Context, svc *Service, dl *models.OfferLite) {
	go func() {
		start := time.Now()

		ctx, cancel := context.WithTimeout(parentCtx, 10*time.Second)
		defer cancel()

		log := svc.Logger.WithFunctionName("GenerateOfferDescriptionAsync")

		tr := otel.Tracer("offers.async")
		ctx, span := tr.Start(ctx, "GenerateOfferDescriptionAsync")
		defer span.End()

		// ─── Panic‑safety & duration metric ──────────────────────────────────
		defer func() {
			metrics.OfferOperationDuration.
				WithLabelValues("describe").
				Observe(time.Since(start).Seconds())

			if r := recover(); r != nil {
				err := fmt.Errorf("panic: %v", r)
				log.Error("goroutine panic", err)
				span.RecordError(err)

				if dl != nil {
					metrics.OfferDescriptionResult.
						WithLabelValues("failure", dl.ID.String()).
						Inc()
					// Re‑enqueue via existing retry queue for now
					_ = svc.EnqueueFailedCuratedOfferForRetry(*dl)
				}
			}
		}()

		// ─── Validate payload ───────────────────────────────────────────────
		if dl == nil {
			log.Error("nil OfferLite payload")
			span.SetAttributes(attribute.Bool("offer.nil", true))
			metrics.OfferDescriptionResult.
				WithLabelValues("failure", "unknown").
				Inc()
			return
		}

		// ─── Generate + persist description (single internal call) ──────────
		dataOffer := mapOfferLiteToData(dl) // helper converts OfferLite → data.Offer
		if err := svc.GenerateOfferDescriptionInternal(ctx, dataOffer); err != nil {
			log.Error("description generation failed",
				"offer_id", dl.ID,
				"error", err)
			span.RecordError(err)
			metrics.OfferDescriptionResult.
				WithLabelValues("failure", dl.ID.String()).
				Inc()
			_ = svc.EnqueueFailedCuratedOfferForRetry(*dl) // schedule retry
			return
		}

		// ─── Success ────────────────────────────────────────────────────────
		log.Info("description generated and saved", "offer_id", dl.ID)
		span.SetAttributes(attribute.String("status", "success"))
		metrics.OfferDescriptionResult.
			WithLabelValues("success", dl.ID.String()).
			Inc()
	}()
}


// DetectFraudulentOffersAsync launches a non‑blocking goroutine that runs our
// curated‑offer fraud scan.  It follows the exact resilience pattern used by
// InsertCuratedOfferAsync (timeout, panic‑safety, tracing, Prometheus).
func DetectFraudulentOffersAsync(parentCtx context.Context, svc *Service) {
	go func() {
		start := time.Now()

		// Hard stop after 10 s — async jobs must never leak goroutines.
		ctx, cancel := context.WithTimeout(parentCtx, 10*time.Second)
		defer cancel()

		log := svc.Logger.WithFunctionName("DetectFraudulentOffersAsync")

		// ─── OpenTelemetry span ───────────────────────────────────────────
		tr := otel.Tracer("offers.async")
		ctx, span := tr.Start(ctx, "DetectFraudulentOffersAsync")
		defer span.End()

		// ─── Panic recovery & metric finaliser ────────────────────────────
		defer func() {
			metrics.OfferOperationDuration.
				WithLabelValues("detect_fraud").
				Observe(time.Since(start).Seconds())

			if r := recover(); r != nil {
				err := fmt.Errorf("panic: %v", r)
				log.Error("goroutine panic", err)
				span.RecordError(err)
				metrics.OfferFraudScanResult.WithLabelValues("failure").Inc()
			}
		}()

		// ─── Execute business logic ──────────────────────────────────────
		if err := svc.DetectFraudulentOffersInternal(ctx); err != nil {
			log.Error("fraud scan failed", "error", err)
			span.RecordError(err)
			metrics.OfferFraudScanResult.WithLabelValues("failure").Inc()
			return
		}

		// ─── Success ─────────────────────────────────────────────────────
		log.Info("fraud scan completed successfully")
		metrics.OfferFraudScanResult.WithLabelValues("success").Inc()
		span.SetAttributes(attribute.String("status", "success"))
	}()
}


// AutoExpireOffersAsync marks expired offers as inactive in the background.
//
// Guarantees
// ----------
// • Non‑blocking: spawns a goroutine and returns immediately.  
// • Resilient: 10 s timeout, panic‑safe, metrics, tracing, structured logs.  
// • Self‑healing: any error is surfaced to Prometheus and recorded on the span.
//
func AutoExpireOffersAsync(parentCtx context.Context, svc *Service) {
	go func() {
		start := time.Now() // latency tracking

		ctx, cancel := context.WithTimeout(parentCtx, 10*time.Second)
		defer cancel()

		log := svc.Logger.WithFunctionName("AutoExpireOffersAsync")

		// ─── OpenTelemetry span ────────────────────────────────────────────
		tr := otel.Tracer("offers.async")
		ctx, span := tr.Start(ctx, "AutoExpireOffersAsync")
		defer span.End()

		// ─── Panic‑safety & metric finaliser ───────────────────────────────
		defer func() {
			metrics.OfferOperationDuration.
				WithLabelValues("auto_expire").
				Observe(time.Since(start).Seconds())

			if r := recover(); r != nil {
				err := fmt.Errorf("panic: %v", r)
				log.Error("goroutine panic", err)
				span.RecordError(err)
				metrics.OfferAutoExpireResult.WithLabelValues("failure").Inc()
			}
		}()

		// ─── Business logic ────────────────────────────────────────────────
		if err := svc.AutoExpireOffersInternal(ctx); err != nil {
			log.Error("auto‑expire run failed", "error", err)
			span.RecordError(err)
			metrics.OfferAutoExpireResult.WithLabelValues("failure").Inc()
			span.SetAttributes(attribute.String("status", "failure"))
			return
		}

		log.Info("auto‑expire run succeeded")
		metrics.OfferAutoExpireResult.WithLabelValues("success").Inc()
		span.SetAttributes(attribute.String("status", "success"))
	}()
}


// RemoveExpiredOffersAsync deletes offers whose EndDate has passed in a
// non‑blocking, observability‑rich background goroutine.
//
// Guarantees
// ----------
// • Fire‑and‑forget: never blocks the caller.  
// • Safe: 10 s timeout, panic recovery.  
// • Transparent: Prometheus metrics + OpenTelemetry span.  
// • Self‑describing: structured logs with function scope.
//
func RemoveExpiredOffersAsync(parentCtx context.Context, svc *Service) {
	go func() {
		start := time.Now()                                   // ── metrics timer
		ctx, cancel := context.WithTimeout(parentCtx, 10*time.Second)
		defer cancel()

		log := svc.Logger.WithFunctionName("RemoveExpiredOffersAsync")

		// ──────── tracing span ────────
		tr := otel.Tracer("offers.async")
		ctx, span := tr.Start(ctx, "RemoveExpiredOffersAsync")
		defer span.End()

		// ──────── panic & metric finaliser ────────
		defer func() {
			metrics.OfferOperationDuration.
				WithLabelValues("remove_expired").
				Observe(time.Since(start).Seconds())

			if r := recover(); r != nil {
				err := fmt.Errorf("panic: %v", r)
				log.Error("goroutine panic", err)
				span.RecordError(err)
				metrics.OfferRemovalResult.WithLabelValues("failure").Inc()
			}
		}()

		// ──────── core business logic ────────
		if err := svc.RemoveExpiredOffersInternal(ctx); err != nil {
			log.Error("expired‑offer removal failed", "error", err)
			span.RecordError(err)
			metrics.OfferRemovalResult.WithLabelValues("failure").Inc()
			span.SetAttributes(attribute.String("status", "failure"))
			return
		}

		// ──────── success path ────────
		log.Info("expired offers removed successfully")
		metrics.OfferRemovalResult.WithLabelValues("success").Inc()
		span.SetAttributes(attribute.String("status", "success"))
	}()
}


// SuggestOffersForUserAsync personalises offers for <userID> in the background.
//
// Key guarantees
// --------------
// • Fire‑and‑forget goroutine (non‑blocking for callers).  
// • 10 s hard timeout → no worker runaway.  
// • Panic‑safe with structured logging, OpenTelemetry span & Prometheus metrics.  
// • Self‑healing: on error callers can decide to enqueue a retry (left to svc‑layer).
//
// Metrics
// -------
// • OfferOperationDuration{operation="suggest"}  – latency histogram.  
// • OfferSuggestionResult{result, user_id}       – success|failure counter.
//
func SuggestOffersForUserAsync(parentCtx context.Context, svc *Service, userID uuid.UUID) {
	go func() {
		const fallbackThreshold = 3 // trigger extra recommender if we return < 3 suggestions

		start := time.Now()

		// ─── bounded context ────────────────────────────────────────────────
		ctx, cancel := context.WithTimeout(parentCtx, 10*time.Second)
		defer cancel()

		log := svc.Logger.WithFunctionName("SuggestOffersForUserAsync")

		// ─── tracing span ───────────────────────────────────────────────────
		tr := otel.Tracer("offers.async")
		ctx, span := tr.Start(ctx, "SuggestOffersForUserAsync")
		defer span.End()

		// ─── panic/metric guard ────────────────────────────────────────────
		defer func() {
			metrics.OfferOperationDuration.WithLabelValues("suggest").
				Observe(time.Since(start).Seconds())

			if r := recover(); r != nil {
				err := fmt.Errorf("panic: %v", r)
				log.Error("goroutine panic", err)
				span.RecordError(err)

				metrics.OfferSuggestionResult.
					WithLabelValues("failure", userID.String()).
					Inc()
			}
		}()

		// ─── input validation ─────────────────────────────────────────────-
		if userID == uuid.Nil {
			log.Error("nil UUID provided")
			span.SetAttributes(attribute.Bool("user_id.valid", false))

			metrics.OfferSuggestionResult.
				WithLabelValues("failure", "nil").
				Inc()
			return
		}

		// ─── primary suggestion engine ─────────────────────────────────────
		suggestions, err := svc.SuggestOffersForUserInternal(ctx, userID, 0 /* default limit */)
		if err != nil {
			log.Error("primary suggestion failed", "user_id", userID, "err", err)
			span.RecordError(err)

			metrics.OfferSuggestionResult.
				WithLabelValues("failure", userID.String()).
				Inc()
			return
		}

		// ─── success path ──────────────────────────────────────────────────
		log.Info("personalised offers generated",
			"user_id", userID,
			"count", len(suggestions))

		metrics.OfferSuggestionResult.
			WithLabelValues("success", userID.String()).
			Inc()
		span.SetAttributes(attribute.String("status", "success"))

		// ─── optional fallback ─────────────────────────────────────────────
		if len(suggestions) < fallbackThreshold {
			log.Info("fallback recommender triggered",
				"user_id", userID,
				"initial_count", len(suggestions))

			GetPersonalizedOffersAsync(ctx, svc, GetPersonalizedOffersEvent{
				UserID: userID,
			})
		}
	}()
}


// ListEligibleUsersForSuggestionsAsync fetches users who should receive
// personalised offer suggestions and kicks off SuggestOffersForUserAsync
// for each of them.
//
// Guarantees
// ──────────
// • Non‑blocking – launches a goroutine and returns immediately.  
// • Resilient – 10 s timeout, panic‑safe, tracing, structured logs, metrics.  
// • Self‑healing – on failure marks the batch in metrics for visibility.  
// • Side‑effects – triggers SuggestOffersForUserAsync (fire‑and‑forget).
func ListEligibleUsersForSuggestionsAsync(parentCtx context.Context, svc *Service) {
	go func() {
		start := time.Now()

		ctx, cancel := context.WithTimeout(parentCtx, 10*time.Second)
		defer cancel()

		log := svc.Logger.WithFunctionName("ListEligibleUsersForSuggestionsAsync")

		// ─── OpenTelemetry span ───────────────────────────────────────────
		tr := otel.Tracer("offers.async")
		ctx, span := tr.Start(ctx, "ListEligibleUsersForSuggestionsAsync")
		defer span.End()

		// ─── Panic‑safety & metrics finaliser ─────────────────────────────
		defer func() {
			metrics.OfferOperationDuration.
				WithLabelValues("plan_suggestions").
				Observe(time.Since(start).Seconds())

			if r := recover(); r != nil {
				err := fmt.Errorf("panic: %v", r)
				log.Error("goroutine panic", err)
				span.RecordError(err)
				metrics.SuggestionPlanResult.WithLabelValues("failure").Inc()
			}
		}()

		// ─── Core business logic ─────────────────────────────────────────
		userIDs, err := svc.ListEligibleUsersForSuggestionsInternal(ctx)
		if err != nil {
			log.Error("failed to list eligible users", "error", err)
			span.RecordError(err)
			metrics.SuggestionPlanResult.WithLabelValues("failure").Inc()
			return
		}

		// ─── Success path ────────────────────────────────────────────────
		count := len(userIDs)
		log.Info("eligible users fetched", "user_count", count)
		span.SetAttributes(
			attribute.String("status", "success"),
			attribute.Int("user_count", count),
		)
		metrics.SuggestionPlanResult.WithLabelValues("success").Inc()

		// Fire personalised‑suggestion workers (fire & forget)
		for _, uid := range userIDs {
			SuggestOffersForUserAsync(ctx, svc, uid) // existing async helper
		}
	}()
}


// UpdateOfferStatusAsync updates a offer's status in the background.
//
// Guarantees
// ----------
// • Fire‑and‑forget goroutine with 10 s timeout & panic‑safety.
// • Tracing, structured logs, Prometheus duration + result metrics.
// • Graceful failure without killing the caller.
func UpdateOfferStatusAsync(parentCtx context.Context, svc *Service, ev UpdateOfferStatusEvent) {
	go func() {
		start := time.Now()

		ctx, cancel := context.WithTimeout(parentCtx, 10*time.Second)
		defer cancel()

		log := svc.Logger.WithFunctionName("UpdateOfferStatusAsync")
		tr := otel.Tracer("offers.async")
		ctx, span := tr.Start(ctx, "UpdateOfferStatusAsync")
		defer span.End()

		defer func() {
			metrics.OfferOperationDuration.WithLabelValues("update_status").
				Observe(time.Since(start).Seconds())

			if r := recover(); r != nil {
				err := fmt.Errorf("panic: %v", r)
				log.Error("goroutine panic", err)
				span.RecordError(err)
				metrics.OfferStatusUpdateResult.WithLabelValues("failure").Inc()
			}
		}()

		// ─── Input validation ───────────────────────────────────────────────
		offerID, err := uuid.Parse(ev.OfferID)
		if err != nil {
			log.Error("invalid offer UUID", "input", ev.OfferID, "error", err)
			metrics.OfferStatusUpdateResult.WithLabelValues("failure").Inc()
			return
		}
		actorID, err := uuid.Parse(ev.ActorID)
		if err != nil {
			log.Error("invalid actor UUID", "input", ev.ActorID, "error", err)
			metrics.OfferStatusUpdateResult.WithLabelValues("failure").Inc()
			return
		}
		if ev.NewStatus == "" {
			log.Error("missing new status")
			metrics.OfferStatusUpdateResult.WithLabelValues("failure").Inc()
			return
		}

		// ─── Business logic ─────────────────────────────────────────────────
		if err := svc.UpdateOfferStatusInternal(ctx, offerID, ev.NewStatus, actorID); err != nil {
			log.Error("status update failed", "offer_id", offerID, "error", err)
			span.RecordError(err)
			metrics.OfferStatusUpdateResult.WithLabelValues("failure").Inc()
			return
		}

		log.Info("offer status updated", "offer_id", offerID, "new_status", ev.NewStatus)
		span.SetAttributes(attribute.String("status", "success"))
		metrics.OfferStatusUpdateResult.WithLabelValues("success").Inc()
	}()
}


// FlagOfferAsync flags a offer for internal review in a fire‑and‑forget goroutine.
//
// Guarantees
// ----------
// • Non‑blocking: returns immediately after spawning the goroutine.  
// • Resilient: 10 s timeout, panic‑safe, structured logs, tracing, Prometheus metrics.  
// • Self‑healing: enqueue to retry queue on failure (if you implement one).  
//
// Metrics
// -------
// • metrics.OfferOperationDuration{operation="flag"} — latency histogram.  
// • metrics.OfferFlagResult{result, offer_id}        — success|failure counter.  
//
func FlagOfferAsync(parentCtx context.Context, svc *Service, ev FlagOfferEvent) {
	go func() {
		start := time.Now()

		// ─── Bounded context ────────────────────────────────────────────────
		ctx, cancel := context.WithTimeout(parentCtx, 10*time.Second)
		defer cancel()

		log := svc.Logger.WithFunctionName("FlagOfferAsync")

		// ─── OpenTelemetry span (optional) ─────────────────────────────────
		tr := otel.Tracer("offers.async")
		ctx, span := tr.Start(ctx, "FlagOfferAsync")
		defer span.End()

		// ─── Panic‑safety & metrics finaliser ──────────────────────────────
		defer func() {
			metrics.OfferOperationDuration.WithLabelValues("flag").
				Observe(time.Since(start).Seconds())

			if r := recover(); r != nil {
				err := fmt.Errorf("panic: %v", r)
				log.Error("goroutine panic", err)
				span.RecordError(err)
				metrics.OfferFlagResult.WithLabelValues("failure", "unknown").Inc()
			}
		}()

		// ─── Input validation ──────────────────────────────────────────────
		offerID, err := uuid.Parse(ev.OfferID)
		if err != nil {
			log.Error("invalid offer UUID", "offer_id", ev.OfferID, "error", err)
			span.RecordError(err)
			metrics.OfferFlagResult.WithLabelValues("failure", "invalid_id").Inc()
			return
		}

		var flaggedBy *uuid.UUID
		if ev.FlaggedBy != nil {
			if uid, err := uuid.Parse(*ev.FlaggedBy); err == nil {
				flaggedBy = &uid
			} else {
				log.Warn("ignoring invalid user UUID", "flagged_by", *ev.FlaggedBy, "error", err)
			}
		}

		// ─── Business logic ────────────────────────────────────────────────
		if err := svc.FlagOfferInternal(ctx, offerID, flaggedBy, ev.Reason); err != nil {
			log.Error("flag failed", "offer_id", offerID, "error", err)
			span.RecordError(err)
			metrics.OfferFlagResult.WithLabelValues("failure", offerID.String()).Inc()
			// TODO: enqueue to retry queue if desired
			return
		}

		// ─── Success ───────────────────────────────────────────────────────
		log.Info("offer flagged", "offer_id", offerID, "flagged_by", flaggedBy, "reason", ev.Reason)
		metrics.OfferFlagResult.WithLabelValues("success", offerID.String()).Inc()
		span.SetAttributes(attribute.String("status", "success"))
	}()
}


// ListOffersToAutoFlagAsync scans for offers that match the auto‑flag
// heuristics (excessive discount, invalid price, duplicates, …) and
// queues each offender for FlagOfferAsync.
//
// Behaviour & guarantees
// ----------------------
// • Fire‑and‑forget goroutine – never blocks the caller.  
// • 10 s context timeout (configurable centrally later).  
// • Panic‑safe; all panics are logged & recorded to the span.  
// • Prometheus: duration + success|failure counter.  
// • OpenTelemetry tracing with useful attributes.  
// • Resilient – any internal failures are surfaced via metrics & logs
//   without crashing the worker.
func ListOffersToAutoFlagAsync(parentCtx context.Context, svc *Service) {
	go func() {
		start := time.Now()

		// ─── Per‑call context with timeout ────────────────────────────────
		ctx, cancel := context.WithTimeout(parentCtx, 10*time.Second)
		defer cancel()

		log := svc.Logger.WithFunctionName("ListOffersToAutoFlagAsync")

		// ─── OpenTelemetry span ───────────────────────────────────────────
		tr := otel.Tracer("offers.async")
		ctx, span := tr.Start(ctx, "ListOffersToAutoFlagAsync")
		defer span.End()

		// ─── Metrics & panic‑safety finaliser ────────────────────────────
		defer func() {
			metrics.OfferOperationDuration.
				WithLabelValues("auto_flag_list").
				Observe(time.Since(start).Seconds())

			if r := recover(); r != nil {
				err := fmt.Errorf("panic: %v", r)
				log.Error("goroutine panic", err)
				span.RecordError(err)
				metrics.OfferAutoFlagResult.WithLabelValues("failure").Inc()
			}
		}()

		// ─── Retrieve candidate offer IDs ─────────────────────────────────
		offerIDs, err := svc.ListOffersToAutoFlagInternal(ctx)
		if err != nil {
			log.Error("auto‑flag query failed", "error", err)
			span.RecordError(err)
			metrics.OfferAutoFlagResult.WithLabelValues("failure").Inc()
			return
		}

		// No violators – still count as “success” for observability.
		if len(offerIDs) == 0 {
			log.Info("no offers matched auto‑flag heuristics")
			metrics.OfferAutoFlagResult.WithLabelValues("success").Inc()
			return
		}

		// ─── Queue each offer for flagging ────────────────────────────────
		for _, id := range offerIDs {
			FlagOfferAsync(ctx, svc, FlagOfferEvent{
				OfferID:    id.String(),
				FlaggedBy: nil,                    // system
				Reason:    "automated‑heuristics", // audit trail
			})
		}

		log.Info("auto‑flag batch dispatched", "count", len(offerIDs))
		span.SetAttributes(attribute.Int("offer_auto_flag_count", len(offerIDs)))
		metrics.OfferAutoFlagResult.WithLabelValues("success").Inc()
	}()
}


// BlacklistOfferAsync blacklists a offer in the background.
//
// Guarantees
// ----------
// • **Non‑blocking** – returns immediately; work runs in a goroutine.  
// • **Resilient**    – 10 s timeout, panic‑safe, retry‑ready hook.  
// • **Observable**   – structured logs, OTEL span, Prometheus metrics.  
// • **Self‑healing** – on any failure we increment failure counters; your
//   retry strategy (e.g. a dead‑letter queue) can hook in afterwards.
func BlacklistOfferAsync(parentCtx context.Context, svc *Service, ev BlacklistOfferEvent) {
	go func() {
		start := time.Now()

		ctx, cancel := context.WithTimeout(parentCtx, 10*time.Second)
		defer cancel()

		log := svc.Logger.WithFunctionName("BlacklistOfferAsync")

		// ─── OpenTelemetry span ─────────────────────────────────────────────
		tr := otel.Tracer("offers.async")
		ctx, span := tr.Start(ctx, "BlacklistOfferAsync")
		defer span.End()

		// ─── Finaliser: duration metric + panic safety ──────────────────────
		defer func() {
			metrics.OfferOperationDuration.WithLabelValues("blacklist").
				Observe(time.Since(start).Seconds())

			if r := recover(); r != nil {
				err := fmt.Errorf("panic: %v", r)
				log.Error("goroutine panic", err)
				span.RecordError(err)

				metrics.OfferStatusUpdateResult.WithLabelValues("failure").Inc()
				metrics.OfferBlacklistResult.WithLabelValues("failure").Inc()
			}
		}()

		// ─── Basic payload validation ──────────────────────────────────────
		if ev.OfferID == "" || ev.BlacklistedBy == "" {
			log.Error("missing offerID / blacklistedBy", "payload", ev)
			metrics.OfferStatusUpdateResult.WithLabelValues("failure").Inc()
			metrics.OfferBlacklistResult.WithLabelValues("failure").Inc()
			span.RecordError(fmt.Errorf("invalid event payload"))
			return
		}

		offerID, err := uuid.Parse(ev.OfferID)
		if err != nil {
			log.Error("invalid offer UUID", "offer_id", ev.OfferID, "error", err)
			metrics.OfferStatusUpdateResult.WithLabelValues("failure").Inc()
			metrics.OfferBlacklistResult.WithLabelValues("failure").Inc()
			span.RecordError(err)
			return
		}
		userID, err := uuid.Parse(ev.BlacklistedBy)
		if err != nil {
			log.Error("invalid user UUID", "user_id", ev.BlacklistedBy, "error", err)
			metrics.OfferStatusUpdateResult.WithLabelValues("failure").Inc()
			metrics.OfferBlacklistResult.WithLabelValues("failure").Inc()
			span.RecordError(err)
			return
		}

		// ─── Core business logic ───────────────────────────────────────────
		if err := svc.BlacklistOfferInternal(ctx, offerID, userID); err != nil {
			log.Error("blacklist failed", "offer_id", offerID, "error", err)
			metrics.OfferStatusUpdateResult.WithLabelValues("failure").Inc()
			metrics.OfferBlacklistResult.WithLabelValues("failure").Inc()
			span.RecordError(err)
			// Optional: enqueue for retry here if you implement one
			return
		}

		// ─── Success ───────────────────────────────────────────────────────
		log.Info("offer blacklisted", "offer_id", offerID, "blacklisted_by", userID)
		metrics.OfferStatusUpdateResult.WithLabelValues("success").Inc()
		metrics.OfferBlacklistResult.WithLabelValues("success").Inc()
		span.SetAttributes(attribute.String("status", "success"))
	}()
}
