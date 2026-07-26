// Package services contains business logic for internal operations such as moderation,
// automation, audit logging, and data synchronization. It is used by async routines
// and internal system workflows, not exposed via public API routes.
//
// sdworkspace/sdbackend/internal/services/internal-services/offer_price_history_internal.go
//
// GTM:
//
//	Layer: 2.5 Catalog / Offer Domain
//	Release Class: DEFERRED
//	Reason:
//	  Offer price history internal automation is deferred until price monitoring,
//	  anomaly policy, subscription cleanup, and notification dispatch are fully
//	  stabilized. This file must compile and remain production-safe, but it is
//	  not a v1 release blocker.
//
// DEFERRED Rule:
//
//	Keep compiling.
//	Keep production-safe.
//	Preserve existing internal automation boundaries.
//	Preserve audit/logging behavior where already implemented.
//	Do not treat price-history automation as a v1 release blocker.
//	Do not expand price monitoring, anomaly policy, subscription cleanup,
//	or notification dispatch scope until this file is promoted from DEFERRED.
//	Block deployment only if this file breaks build, corrupts data,
//	violates security policy, or creates unsafe runtime behavior.
package services

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/PiccoloMondoC/sdworkspace/sdbackend/internal/data"
	"github.com/PiccoloMondoC/sdworkspace/sdbackend/internal/utils/timeutil"

	"github.com/google/uuid"
)

const (
	offerPriceAnomalyThreshold = "0.75"
	priceDropCleanupDays       = 90
)

// FlagOfferPriceAnomalyInternal evaluates the two latest price-history records
// for an offer and flags the offer when the price movement is anomalous.
func (s *Service) FlagOfferPriceAnomalyInternal(ctx context.Context, offerID uuid.UUID) error {
	logger := s.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("FlagOfferPriceAnomalyInternal")

	if offerID == uuid.Nil {
		err := errors.New("offer_id is required")
		logger.Warn("Validation failed", "error", err)
		return err
	}

	history, err := s.Models.OfferPriceHistory.GetByOfferID(ctx, offerID)
	if err != nil {
		logger.Error("Failed to fetch offer price history", "offer_id", offerID, "error", err)
		return err
	}

	if len(history) < 2 {
		logger.Info("Insufficient price history for anomaly evaluation", "offer_id", offerID)
		return nil
	}

	newestPrice, err := parsePositiveRat(history[0].Price, "latest price")
	if err != nil {
		logger.Warn("Invalid latest price for anomaly evaluation", "offer_id", offerID, "price", history[0].Price, "error", err)
		return nil
	}

	previousPrice, err := parsePositiveRat(history[1].Price, "previous price")
	if err != nil {
		logger.Warn("Invalid previous price for anomaly evaluation", "offer_id", offerID, "price", history[1].Price, "error", err)
		return nil
	}

	change := new(big.Rat).Sub(newestPrice, previousPrice)
	change.Quo(change, previousPrice)

	absChange := new(big.Rat).Set(change)
	if absChange.Sign() < 0 {
		absChange.Neg(absChange)
	}

	threshold, err := parsePositiveRat(offerPriceAnomalyThreshold, "anomaly threshold")
	if err != nil {
		logger.Error("Invalid anomaly threshold", "error", err)
		return err
	}

	if absChange.Cmp(threshold) < 0 {
		logger.Info("Price change within normal range", "offer_id", offerID, "change", change.FloatString(6))
		return nil
	}

	if err := s.Models.OfferPriceHistory.FlagPriceAnomaly(ctx, offerID); err != nil {
		logger.Error("Failed to flag offer price anomaly", "offer_id", offerID, "error", err)
		return err
	}

	if err := s.insertSystemAuditLog(ctx, "flag_price_anomaly", "Flagged offer price anomaly", "offer", "Offer listing", offerID.String()); err != nil {
		logger.Warn("Audit logging failed", "offer_id", offerID, "error", err)
	}

	logger.Warn("Offer price anomaly detected", "offer_id", offerID, "change", change.FloatString(6))
	return nil
}

// AutoValidateOfferPriceInternal runs deferred automated price validation.
//
// Current canonical behavior:
// - Reuses existing data-layer significant-drop detection.
// - Flags matching offers through offer_flags.
// - Does not invent historical-average methods in the data layer.
func (s *Service) AutoValidateOfferPriceInternal(ctx context.Context) error {
	logger := s.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("AutoValidateOfferPriceInternal")

	ctx, cancel := context.WithTimeout(ctx, s.Cfg.DBTimeout)
	defer cancel()

	histories, err := s.Models.OfferPriceHistory.GetOffersWithSignificantPriceDrops(ctx, offerPriceAnomalyThreshold)
	if err != nil {
		logger.Error("Failed to fetch significant price drops", "error", err)
		return err
	}

	for _, history := range histories {
		if ctx.Err() != nil {
			logger.Warn("Context cancelled during auto price validation", "error", ctx.Err())
			return ctx.Err()
		}

		if err := s.Models.OfferPriceHistory.FlagPriceAnomaly(ctx, history.OfferID); err != nil {
			logger.Warn("Failed to flag significant price drop", "offer_id", history.OfferID, "error", err)
			continue
		}

		if err := s.insertSystemAuditLog(
			ctx,
			"auto_validate_offer_price",
			"Automatically validate offer prices for anomalies",
			"offer_price",
			"Validated and curated offer price information",
			history.OfferID.String(),
		); err != nil {
			logger.Warn("Audit logging failed during auto price validation", "offer_id", history.OfferID, "error", err)
		}
	}

	logger.Info("Auto price validation complete", "flagged_count", len(histories))
	return nil
}

// CleanupExpiredSubscriptionsInternal removes old price-drop subscriptions.
func (s *Service) CleanupExpiredSubscriptionsInternal(ctx context.Context) error {
	logger := s.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("CleanupExpiredSubscriptionsInternal")

	cutoff := timeutil.Now().AddDate(0, 0, -priceDropCleanupDays)

	count, err := s.Models.OfferPriceHistory.DeleteExpiredSubscriptions(ctx, cutoff)
	if err != nil {
		logger.Error("Failed to clean up expired price-drop subscriptions", "cutoff", cutoff, "error", err)
		return err
	}

	if count > 0 {
		if err := s.insertSystemAuditLog(
			ctx,
			"cleanup_expired_subscriptions",
			"Cleaned up expired price drop subscriptions",
			"price_drop_subscription",
			"Price drop subscription",
			fmt.Sprintf("cleanup-%s", cutoff.Format(time.RFC3339)),
		); err != nil {
			logger.Warn("Audit logging failed for subscription cleanup", "deleted_count", count, "error", err)
		}
	}

	logger.Info("Expired price-drop subscriptions cleanup complete", "deleted_count", count, "cutoff", cutoff)
	return nil
}

// RecordPriceHistoryInternal records a new immutable price-history point when
// the observed price differs from the latest known price.
func (s *Service) RecordPriceHistoryInternal(ctx context.Context, offerID uuid.UUID, newPrice string) error {
	logger := s.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("RecordPriceHistoryInternal")

	if offerID == uuid.Nil {
		err := errors.New("offer_id is required")
		logger.Warn("Validation failed", "error", err)
		return err
	}

	newPrice = strings.TrimSpace(newPrice)
	if _, err := parsePositiveRat(newPrice, "new price"); err != nil {
		logger.Warn("Invalid new price", "offer_id", offerID, "price", newPrice, "error", err)
		return err
	}

	latest, err := s.Models.OfferPriceHistory.GetLatestPrice(ctx, offerID)
	if err != nil && !errors.Is(err, data.ErrOfferPriceHistoryNotFound) {
		logger.Error("Failed to retrieve latest price", "offer_id", offerID, "error", err)
		return err
	}

	if err == nil && strings.TrimSpace(latest) == newPrice {
		logger.Info("Price unchanged; skipping price-history insert", "offer_id", offerID, "price", newPrice)
		return nil
	}

	entry := &data.OfferPriceHistory{
		OfferID: offerID,
		Price:   newPrice,
	}

	if err := s.Models.OfferPriceHistory.Insert(ctx, entry); err != nil {
		logger.Error("Failed to insert offer price history", "offer_id", offerID, "price", newPrice, "error", err)
		return err
	}

	if err := s.insertSystemAuditLog(
		ctx,
		"record_offer_price",
		"Record offer price history",
		"offer_price_history",
		"Offer price history",
		entry.ID.String(),
	); err != nil {
		logger.Warn("Audit logging failed for price-history insert", "entry_id", entry.ID, "error", err)
	}

	if err == nil {
		latestRat, latestErr := parsePositiveRat(latest, "latest price")
		newRat, newErr := parsePositiveRat(newPrice, "new price")
		if latestErr == nil && newErr == nil && newRat.Cmp(latestRat) < 0 {
			if alertErr := s.TriggerPriceDropAlertsInternal(ctx, offerID, newPrice); alertErr != nil {
				logger.Warn("Price drop alert dispatch failed after price-history insert", "offer_id", offerID, "price", newPrice, "error", alertErr)
			}
		}
	}

	logger.Info("Price history recorded", "offer_id", offerID, "entry_id", entry.ID, "price", newPrice)
	return nil
}

// TriggerPriceDropAlertsInternal sends in-app notifications to eligible users
// subscribed to a threshold met by the new price.
func (s *Service) TriggerPriceDropAlertsInternal(ctx context.Context, offerID uuid.UUID, newPrice string) error {
	logger := s.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("TriggerPriceDropAlertsInternal")

	if offerID == uuid.Nil {
		err := errors.New("offer_id is required")
		logger.Warn("Validation failed", "error", err)
		return err
	}

	newPrice = strings.TrimSpace(newPrice)
	if _, err := parsePositiveRat(newPrice, "new price"); err != nil {
		logger.Warn("Invalid new price for price-drop alerts", "offer_id", offerID, "price", newPrice, "error", err)
		return err
	}

	subscribers, err := s.Models.OfferPriceHistory.GetSubscribersForPriceDrop(ctx, offerID, newPrice)
	if err != nil {
		logger.Error("Failed to fetch price-drop subscribers", "offer_id", offerID, "price", newPrice, "error", err)
		return err
	}

	if len(subscribers) == 0 {
		logger.Info("No price-drop subscribers to notify", "offer_id", offerID, "price", newPrice)
		return nil
	}

	var failureCount int
	for _, sub := range subscribers {
		if ctx.Err() != nil {
			logger.Warn("Context cancelled during price-drop notification loop", "offer_id", offerID, "error", ctx.Err())
			return ctx.Err()
		}

		err := s.NotifyUserInternal(ctx, NotifyUserEvent{
			UserID:         sub.UserID,
			OfferID:        &offerID,
			Type:           "offer_price_drop",
			Message:        fmt.Sprintf("Offer %s dropped to %s.", offerID.String(), newPrice),
			DeliveryMethod: "in_app",
			CreatedBy:      uuid.Nil,
		})
		if err != nil {
			failureCount++
			logger.Warn("Price-drop notification failed", "user_id", sub.UserID, "offer_id", offerID, "error", err)
		}
	}

	if err := s.insertSystemAuditLog(
		ctx,
		"send_price_drop_alerts",
		"Notify subscribers of a price drop",
		"offer",
		"Offer entity",
		offerID.String(),
	); err != nil {
		logger.Warn("Audit logging failed for price-drop alerts", "offer_id", offerID, "error", err)
	}

	if failureCount > 0 {
		return fmt.Errorf("price drop alert dispatch completed with %d failures", failureCount)
	}

	logger.Info("Price-drop alerts dispatched", "offer_id", offerID, "price", newPrice, "notified_count", len(subscribers))
	return nil
}

func (s *Service) insertSystemAuditLog(
	ctx context.Context,
	actionName string,
	actionDescription string,
	entityTypeName string,
	entityTypeDescription string,
	entityID string,
) error {
	action, err := s.Models.Action.GetByName(ctx, actionName)
	if err != nil || action == nil {
		id, createErr := s.Models.Action.CreateIfNotExists(ctx, actionName, actionDescription)
		if createErr != nil {
			return createErr
		}
		action = &data.Action{ID: id}
	}

	entityType, err := s.Models.EntityType.GetByName(ctx, entityTypeName)
	if err != nil || entityType == nil {
		id, createErr := s.Models.EntityType.CreateIfNotExists(ctx, entityTypeName, entityTypeDescription)
		if createErr != nil {
			return createErr
		}
		entityType = &data.EntityType{ID: id}
	}

	audit := &data.AuditLog{
		ID:           uuid.New(),
		UserID:       nil,
		ActionID:     action.ID,
		EntityTypeID: entityType.ID,
		EntityID:     entityID,
	}

	return s.Models.AuditLog.Insert(ctx, audit)
}

func parsePositiveRat(value string, field string) (*big.Rat, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, fmt.Errorf("%s is required", field)
	}

	rat := new(big.Rat)
	if _, ok := rat.SetString(value); !ok {
		return nil, fmt.Errorf("%s must be a valid decimal string", field)
	}

	if rat.Sign() <= 0 {
		return nil, fmt.Errorf("%s must be greater than zero", field)
	}

	return rat, nil
}
