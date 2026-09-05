// Package services contains business logic for internal operations such as moderation,
// automation, audit logging, and data synchronization. It is used by async routines
// and internal system workflows, not exposed via public API routes.
//
// sdworkspace/sdbackend/internal/services/user_notifications_async.go
//
// GTM:
//
//	Layer: 2.3 Consumer Domain
//	Release Class: SPINE
//	Reason:
//	  User notification async services are release-critical consumer
//	  communication infrastructure. They provide safe non-blocking execution
//	  for automation-triggered notification persistence while preserving timeout,
//	  panic-safety, tracing, metrics, and structured logging.
//
// SPINE Rule:
//
//	Keep compiling.
//	Keep production-ready.
//	Preserve non-blocking async notification dispatch.
//	Preserve timeout boundaries.
//	Preserve panic recovery.
//	Preserve metrics emission.
//	Preserve tracing.
//	Preserve delegation to internal notification persistence.
//	Block deployment if this file breaks build, async notification dispatch,
//	observability, or consumer communication integrity.
package services

import (
	"context"
	"fmt"
	"time"

	"github.com/PiccoloMondoC/focodebase/fobackend/internal/observability/metrics"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
)

// NotifyUserEvent represents a background-triggered user notification.
type NotifyUserEvent struct {
	UserID         uuid.UUID
	OfferID        *uuid.UUID
	Type           string
	Message        string
	DeliveryMethod string
	CreatedBy      uuid.UUID
}

// NotifyUserAsync dispatches user notification persistence in the background.
func (s *Service) NotifyUserAsync(parentCtx context.Context, event NotifyUserEvent) {
	go s.runUserNotificationAsync(parentCtx, event, "notify_user")
}

// InsertNotificationAsync is retained as a compatibility wrapper for older callers.
func InsertNotificationAsync(parentCtx context.Context, svc *Service, event NotifyUserEvent) {
	go svc.runUserNotificationAsync(parentCtx, event, "insert_notification")
}

func (s *Service) runUserNotificationAsync(parentCtx context.Context, event NotifyUserEvent, operation string) {
	start := time.Now()

	ctx, cancel := context.WithTimeout(parentCtx, 10*time.Second)
	defer cancel()

	logger := s.Logger.WithFunctionName("runUserNotificationAsync")
	tracer := otel.Tracer("user.notifications.async")

	ctx, span := tracer.Start(ctx, "runUserNotificationAsync")
	defer span.End()

	span.SetAttributes(
		attribute.String("operation", operation),
		attribute.String("user_id", event.UserID.String()),
		attribute.String("type", event.Type),
		attribute.String("delivery_method", event.DeliveryMethod),
	)

	defer func() {
		metrics.UserNotificationOperationDuration.
			WithLabelValues(operation).
			Observe(time.Since(start).Seconds())

		if r := recover(); r != nil {
			err := fmt.Errorf("panic: %v", r)
			logger.Error("User notification async panic", "error", err)
			span.RecordError(err)

			metrics.UserNotificationResult.
				WithLabelValues("failure", event.UserID.String()).
				Inc()
		}
	}()

	if err := s.NotifyUserInternal(ctx, event); err != nil {
		logger.Error("User notification async insert failed", "error", err, "user_id", event.UserID)
		span.RecordError(err)

		metrics.UserNotificationResult.
			WithLabelValues("failure", event.UserID.String()).
			Inc()
		return
	}

	logger.Info("User notification async insert successful", "user_id", event.UserID, "type", event.Type)

	metrics.UserNotificationResult.
		WithLabelValues("success", event.UserID.String()).
		Inc()
}
