// Package services contains business logic for internal operations such as moderation,
// automation, audit logging, and data synchronization. It is used by async routines
// and internal system workflows, not exposed via public API routes.
//
// sdworkspace/sdbackend/internal/services/user_notifications_internal.go
//
// GTM:
//   Layer: 2.3 Consumer Domain
//   Release Class: SPINE
//   Reason:
//     User notification internal services are release-critical consumer
//     communication infrastructure. They persist retained notification history,
//     resolve notification type/channel metadata, link delivery channels, and
//     record audit history for internal automation-triggered notifications.
//
// SPINE Rule:
//   Keep compiling.
//   Keep production-ready.
//   Preserve notification persistence.
//   Preserve notification type resolution.
//   Preserve notification channel resolution.
//   Preserve channel-link persistence.
//   Preserve audit logging as non-fatal.
//   Preserve DB-owned notification sent_at and lifecycle timestamps.
//   Block deployment if this file breaks build, notification persistence,
//   channel linking, or consumer communication integrity.
package services

import (
	"context"
	"errors"
	"strings"

	"github.com/PiccoloMondoC/sdworkspace/sdbackend/internal/data"

	"github.com/google/uuid"
)

const (
	userNotificationAuditAction = "create_user_notification"
	userNotificationEntityType  = "user_notification"
)

// NotifyUserInternal inserts a retained user notification and links it to a delivery channel.
func (s *Service) NotifyUserInternal(ctx context.Context, event NotifyUserEvent) error {
	return s.insertUserNotificationInternal(ctx, event)
}

// InsertNotificationInternal is retained for compatibility with older async callers.
func (s *Service) InsertNotificationInternal(ctx context.Context, event NotifyUserEvent) error {
	return s.insertUserNotificationInternal(ctx, event)
}

func (s *Service) insertUserNotificationInternal(ctx context.Context, event NotifyUserEvent) error {
	logger := s.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("insertUserNotificationInternal")

	if event.UserID == uuid.Nil {
		err := errors.New("user_id is required")
		logger.Error("Validation failed", err)
		return err
	}

	if event.CreatedBy == uuid.Nil {
		err := errors.New("created_by is required")
		logger.Error("Validation failed", err)
		return err
	}

	if event.OfferID == nil || *event.OfferID == uuid.Nil {
		err := errors.New("offer_id is required")
		logger.Error("Validation failed", err)
		return err
	}

	notificationType := strings.TrimSpace(event.Type)
	if notificationType == "" {
		err := errors.New("notification type is required")
		logger.Error("Validation failed", err)
		return err
	}

	deliveryMethod := strings.TrimSpace(event.DeliveryMethod)
	if deliveryMethod == "" {
		err := errors.New("delivery method is required")
		logger.Error("Validation failed", err)
		return err
	}

	notificationTypeID, err := s.Models.NotificationType.ResolveIDByName(ctx, notificationType)
	if err != nil {
		logger.Error("Failed to resolve notification type", err, "notification_type", notificationType)
		return err
	}

	channelID, err := s.Models.NotificationChannel.ResolveIDByName(ctx, deliveryMethod)
	if err != nil {
		logger.Error("Failed to resolve notification channel", err, "delivery_method", deliveryMethod)
		return err
	}

	notification := data.UserNotification{
		UserID:             event.UserID,
		OfferID:            *event.OfferID,
		NotificationTypeID: notificationTypeID,
	}

	if err := s.Models.UserNotification.Insert(ctx, &notification); err != nil {
		logger.Error("Failed to insert user notification", err, "user_id", event.UserID, "offer_id", *event.OfferID)
		return err
	}

	link := data.UserNotificationChannel{
		UserNotificationID: notification.ID,
		ChannelID:          channelID,
	}

	if err := s.Models.UserNotification.InsertChannelLink(ctx, &link); err != nil {
		logger.Error("Failed to insert notification channel link", err, "notification_id", notification.ID, "channel_id", channelID)
		return err
	}

	s.insertUserNotificationAudit(ctx, event.CreatedBy, notification.ID)

	logger.Info("User notification inserted", "notification_id", notification.ID, "user_id", event.UserID)
	return nil
}

func (s *Service) insertUserNotificationAudit(ctx context.Context, actorID uuid.UUID, notificationID uuid.UUID) {
	logger := s.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("insertUserNotificationAudit")

	action, err := s.Models.Action.GetByName(ctx, userNotificationAuditAction)
	if err != nil || action == nil {
		actionID, createErr := s.Models.Action.CreateIfNotExists(
			ctx,
			userNotificationAuditAction,
			"Create user notification",
		)
		if createErr != nil {
			logger.Warn("Failed to resolve audit action", "action", userNotificationAuditAction, "error", createErr)
			return
		}
		action = &data.Action{ID: actionID}
	}

	entityType, err := s.Models.EntityType.GetByName(ctx, userNotificationEntityType)
	if err != nil || entityType == nil {
		entityTypeID, createErr := s.Models.EntityType.CreateIfNotExists(
			ctx,
			userNotificationEntityType,
			"User notification",
		)
		if createErr != nil {
			logger.Warn("Failed to resolve audit entity type", "entity_type", userNotificationEntityType, "error", createErr)
			return
		}
		entityType = &data.EntityType{ID: entityTypeID}
	}

	audit := data.AuditLog{
		ID:           uuid.New(),
		UserID:       &actorID,
		ActionID:     action.ID,
		EntityTypeID: entityType.ID,
		EntityID:     notificationID.String(),
	}

	if err := s.Models.AuditLog.Insert(ctx, &audit); err != nil {
		logger.Warn("Audit logging failed", "notification_id", notificationID, "error", err)
	}
}