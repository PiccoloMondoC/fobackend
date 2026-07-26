// Package services contains business logic for internal operations such as moderation,
// automation, audit logging, and data synchronization. It is used by async routines
// and internal system workflows, not exposed via public API routes.
//
// sdworkspace/sdbackend/internal/services/async/offer_price_history_async.go
//
// GTM:
//
//	Layer: 2.5 Catalog / Offer Domain
//	Release Class: DEFERRED
//	Reason:
//	  Offer price history async automation is deferred until external price
//	  monitoring, anomaly policy, subscription cleanup, and notification
//	  dispatch behavior are fully stabilized. This file must compile and remain
//	  production-safe, but it is not a v1 release blocker.
//
// DEFERRED Rule:
//
//	Keep compiling.
//	Keep production-safe.
//	Preserve non-blocking fire-and-forget dispatch semantics.
//	Preserve context timeout and panic recovery on every goroutine.
//	Preserve existing metrics, tracing, and structured logging.
//	Preserve delegation-only contract (no direct business logic in this file).
//	Do not expand external price monitoring, anomaly policy,
//	subscription cleanup, or notification dispatch scope until this
//	file is promoted from DEFERRED.
//	Block deployment only if this file breaks build, violates async
//	reliability guarantees, compromises observability, or creates
//	unsafe runtime behavior.
package services

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
)

type FlagOfferPriceAnomalyEvent struct {
	OfferID uuid.UUID
}

type RecordPriceHistoryEvent struct {
	OfferID  uuid.UUID
	NewPrice string
}

type TriggerPriceDropAlertsEvent struct {
	OfferID  uuid.UUID
	NewPrice string
}

func FlagOfferPriceAnomalyAsync(
	ctx context.Context,
	service *Service,
	event FlagOfferPriceAnomalyEvent,
) {
	go func() {
		innerCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()

		logger := service.Logger.WithFunctionName("FlagOfferPriceAnomalyAsync")

		defer func() {
			if r := recover(); r != nil {
				logger.Error("panic in FlagOfferPriceAnomalyAsync", fmt.Errorf("%v", r))
			}
		}()

		if err := service.FlagOfferPriceAnomalyInternal(innerCtx, event.OfferID); err != nil {
			logger.Error("Failed to flag offer price anomaly", "offer_id", event.OfferID, "error", err)
			return
		}

		logger.Info("Offer price anomaly check completed", "offer_id", event.OfferID)
	}()
}

func AutoValidateOfferPriceAsync(
	ctx context.Context,
	service *Service,
) {
	go func() {
		innerCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()

		logger := service.Logger.WithFunctionName("AutoValidateOfferPriceAsync")

		defer func() {
			if r := recover(); r != nil {
				logger.Error("panic in AutoValidateOfferPriceAsync", fmt.Errorf("%v", r))
			}
		}()

		if err := service.AutoValidateOfferPriceInternal(innerCtx); err != nil {
			logger.Error("Auto price validation failed", "error", err)
			return
		}

		logger.Info("Auto price validation completed")
	}()
}

func CleanupExpiredSubscriptionsAsync(
	ctx context.Context,
	service *Service,
) {
	go func() {
		innerCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
		defer cancel()

		logger := service.Logger.WithFunctionName("CleanupExpiredSubscriptionsAsync")

		defer func() {
			if r := recover(); r != nil {
				logger.Error("panic in CleanupExpiredSubscriptionsAsync", fmt.Errorf("%v", r))
			}
		}()

		if err := service.CleanupExpiredSubscriptionsInternal(innerCtx); err != nil {
			logger.Error("Expired subscription cleanup failed", "error", err)
			return
		}

		logger.Info("Expired subscription cleanup completed")
	}()
}

func RecordPriceHistoryAsync(
	ctx context.Context,
	service *Service,
	event RecordPriceHistoryEvent,
) {
	go func() {
		innerCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()

		logger := service.Logger.WithFunctionName("RecordPriceHistoryAsync")

		defer func() {
			if r := recover(); r != nil {
				logger.Error("panic in RecordPriceHistoryAsync", fmt.Errorf("%v", r))
			}
		}()

		if err := service.RecordPriceHistoryInternal(innerCtx, event.OfferID, event.NewPrice); err != nil {
			logger.Error("Failed to record offer price history", "offer_id", event.OfferID, "price", event.NewPrice, "error", err)
			return
		}

		logger.Info("Offer price history recorded", "offer_id", event.OfferID, "price", event.NewPrice)
	}()
}

func TriggerPriceDropAlertsAsync(
	ctx context.Context,
	service *Service,
	event TriggerPriceDropAlertsEvent,
) {
	go func() {
		innerCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()

		logger := service.Logger.WithFunctionName("TriggerPriceDropAlertsAsync")

		defer func() {
			if r := recover(); r != nil {
				logger.Error("panic in TriggerPriceDropAlertsAsync", fmt.Errorf("%v", r))
			}
		}()

		if err := service.TriggerPriceDropAlertsInternal(innerCtx, event.OfferID, event.NewPrice); err != nil {
			logger.Error("Price-drop alert dispatch failed", "offer_id", event.OfferID, "price", event.NewPrice, "error", err)
			return
		}

		logger.Info("Price-drop alerts dispatched", "offer_id", event.OfferID, "price", event.NewPrice)
	}()
}
