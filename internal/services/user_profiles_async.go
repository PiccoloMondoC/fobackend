// Package services contains business logic for internal operations such as moderation,
// automation, audit logging, and data synchronization. It is used by async routines
// and internal system workflows, not exposed via public API routes.
//
// focodebase/fobackend/internal/services/user_profiles_async.go
//
// GTM:
//
//	Layer: 2.2 Identity / Auth Domain
//	Release Class: SPINE
//	Reason:
//	  User profile async services provide panic-safe, timeout-bounded,
//	  metrics-instrumented orchestration for profile moderation, handle
//	  validation, and ownership resolution. These wrappers protect the v1
//	  Future Offering release spine by keeping trust-and-safety and identity
//	  workflows non-blocking while delegating business logic to internal
//	  service methods.
//
// SPINE Rule:
//
//	Keep compiling.
//	Keep production-ready.
//	Preserve panic-safety for every goroutine.
//	Preserve timeout bounds for async profile workflows.
//	Preserve metrics and tracing instrumentation.
//	Preserve delegation-only behavior; do not call DB/model methods directly here.
//	Block deployment if this file breaks build, goroutine safety,
//	observability, or profile workflow orchestration.
package services

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/PiccoloMondoC/focodebase/fobackend/internal/observability/metrics"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
)

type CheckAndAutoFlagProfileEvent struct {
	UserID uuid.UUID
}

type TriggerAutoFlagProfilesEvent struct{}

type ValidateUserHandleEvent struct {
	RawHandle string
}

type ResolveProfileOwnerEvent struct {
	Handle string
	UserID uuid.UUID
}

func CheckAndAutoFlagProfileAsync(
	parentCtx context.Context,
	svc *Service,
	ev CheckAndAutoFlagProfileEvent,
) {
	go func() {
		start := time.Now()
		ctx, cancel := context.WithTimeout(parentCtx, 5*time.Second)
		defer cancel()

		log := svc.Logger.WithFunctionName("CheckAndAutoFlagProfileAsync")
		tr := otel.Tracer("user_profiles.async")
		ctx, span := tr.Start(ctx, "CheckAndAutoFlagProfileAsync")
		defer span.End()

		defer func() {
			metrics.ProfileOperationDuration.
				WithLabelValues("auto_flag_profile").
				Observe(time.Since(start).Seconds())

			if r := recover(); r != nil {
				err := fmt.Errorf("panic: %v", r)
				log.Error("goroutine panic", err)
				span.RecordError(err)
				metrics.ProfileAutoFlagResult.
					WithLabelValues("failure", ev.UserID.String()).
					Inc()
			}
		}()

		if ev.UserID == uuid.Nil {
			log.Error("missing user_id")
			span.SetAttributes(attribute.Bool("user_id.nil", true))
			metrics.ProfileAutoFlagResult.
				WithLabelValues("failure", "unknown").
				Inc()
			return
		}

		svc.CheckAndAutoFlagProfileInternal(ctx, ev.UserID)

		log.Info("auto-flag check completed", "user_id", ev.UserID)
		span.SetAttributes(attribute.String("status", "success"))
		metrics.ProfileAutoFlagResult.
			WithLabelValues("success", ev.UserID.String()).
			Inc()
	}()
}

func AutoFlagUserProfilesAsync(
	parentCtx context.Context,
	svc *Service,
	_ TriggerAutoFlagProfilesEvent,
) {
	go func() {
		start := time.Now()
		ctx, cancel := context.WithTimeout(parentCtx, 30*time.Second)
		defer cancel()

		log := svc.Logger.WithFunctionName("AutoFlagUserProfilesAsync")
		tr := otel.Tracer("user_profiles.async")
		ctx, span := tr.Start(ctx, "AutoFlagUserProfilesAsync")
		defer span.End()

		defer func() {
			metrics.ProfileOperationDuration.
				WithLabelValues("auto_flag_profiles").
				Observe(time.Since(start).Seconds())

			if r := recover(); r != nil {
				err := fmt.Errorf("panic: %v", r)
				log.Error("goroutine panic", err)
				span.RecordError(err)
				metrics.ProfileBatchAutoFlagResult.
					WithLabelValues("failure").
					Inc()
			}
		}()

		if err := svc.AutoFlagUserProfilesInternal(ctx); err != nil {
			log.Error("batch auto-flag failed", "error", err)
			span.RecordError(err)
			metrics.ProfileBatchAutoFlagResult.
				WithLabelValues("failure").
				Inc()
			return
		}

		log.Info("batch auto-flag completed successfully")
		span.SetAttributes(attribute.String("status", "success"))
		metrics.ProfileBatchAutoFlagResult.
			WithLabelValues("success").
			Inc()
	}()
}

func ValidateAndNormalizeHandleAsync(
	parentCtx context.Context,
	svc *Service,
	ev ValidateUserHandleEvent,
) {
	go func() {
		start := time.Now()
		ctx, cancel := context.WithTimeout(parentCtx, 5*time.Second)
		defer cancel()

		log := svc.Logger.WithFunctionName("ValidateAndNormalizeHandleAsync")
		tr := otel.Tracer("user_profiles.async")
		ctx, span := tr.Start(ctx, "ValidateAndNormalizeHandleAsync")
		defer span.End()

		defer func() {
			metrics.ProfileOperationDuration.
				WithLabelValues("validate_handle").
				Observe(time.Since(start).Seconds())

			if r := recover(); r != nil {
				err := fmt.Errorf("panic: %v", r)
				log.Error("goroutine panic", err)
				span.RecordError(err)
				metrics.ProfileHandleValidationResult.
					WithLabelValues("failure").
					Inc()
			}
		}()

		if strings.TrimSpace(ev.RawHandle) == "" {
			log.Error("missing or empty handle")
			span.SetAttributes(attribute.Bool("handle.empty", true))
			metrics.ProfileHandleValidationResult.
				WithLabelValues("failure").
				Inc()
			return
		}

		normalized, err := svc.ValidateAndNormalizeHandleInternal(ctx, ev.RawHandle)
		if err != nil {
			log.Warn("handle validation failed", "raw_handle", ev.RawHandle, "error", err)
			span.RecordError(err)
			metrics.ProfileHandleValidationResult.
				WithLabelValues("failure").
				Inc()
			return
		}

		log.Info("handle validated", "raw_handle", ev.RawHandle, "normalized_handle", normalized)
		span.SetAttributes(
			attribute.String("status", "success"),
			attribute.String("normalized_handle", normalized),
		)
		metrics.ProfileHandleValidationResult.
			WithLabelValues("success").
			Inc()
	}()
}

func ResolveProfileOwnerAsync(
	parentCtx context.Context,
	svc *Service,
	ev ResolveProfileOwnerEvent,
) {
	go func() {
		start := time.Now()
		ctx, cancel := context.WithTimeout(parentCtx, 5*time.Second)
		defer cancel()

		log := svc.Logger.WithFunctionName("ResolveProfileOwnerAsync")
		tr := otel.Tracer("user_profiles.async")
		ctx, span := tr.Start(ctx, "ResolveProfileOwnerAsync")
		defer span.End()

		defer func() {
			metrics.ProfileOperationDuration.
				WithLabelValues("resolve_profile_owner").
				Observe(time.Since(start).Seconds())

			if r := recover(); r != nil {
				err := fmt.Errorf("panic: %v", r)
				log.Error("goroutine panic", err)
				span.RecordError(err)
			}
		}()

		ok, err := svc.ResolveProfileOwnerInternal(ctx, ev.Handle, ev.UserID)
		if err != nil {
			log.Error("ownership resolution failed", "handle", ev.Handle, "user_id", ev.UserID, "error", err)
			span.RecordError(err)
			return
		}

		if ok {
			log.Info("ownership confirmed", "handle", ev.Handle, "user_id", ev.UserID)
			span.SetAttributes(attribute.Bool("owner.resolved", true))
			return
		}

		log.Info("ownership denied", "handle", ev.Handle, "user_id", ev.UserID)
		span.SetAttributes(attribute.Bool("owner.resolved", false))
	}()
}
