// Package main provides API server startup, configuration, dependency wiring,
// route registration, and HTTP lifecycle management.
//
// focodebase/fobackend/internal/server/cmd/api/notification_dispatch.go
//
// GTM:
//
//	Layer: 2.2 Identity / Auth Domain
//	Release Class: SPINE
//	Reason:
//	  Non-enumerating credential requests (password-reset request and
//	  email-confirmation resend) must answer in the same shape and in about
//	  the same time whether or not an account exists. Delivering mail inside
//	  the request made existing accounts measurably slower, a timing side
//	  channel. This file moves that delivery off the request path.
//
// SPINE Rule:
//
//	Keep compiling.
//	Keep production-ready.
//	Detach delivery from request cancellation, but always bound it.
//	Never let a delivery failure or panic affect the HTTP response or the
//	already-committed credential transaction.
//	Never log credentials or credential links.
//	Let in-flight deliveries finish (bounded) during graceful shutdown.
package main

import (
	"context"
	"fmt"
	"time"
)

// notificationDeliveryTimeout bounds one detached credential-email delivery.
const notificationDeliveryTimeout = 30 * time.Second

type dispatchLogger interface {
	Warn(msg string, keysAndValues ...interface{})
	Error(msg string, keysAndValues ...interface{})
}

// dispatchCredentialNotification runs deliver in the background with a
// context that keeps the request's values (request ID, tracing) but not its
// cancellation, bounded by notificationDeliveryTimeout.
//
// The credential has already been issued and committed by the caller. The
// replacement-invalidates-previous rule is enforced at issuance, so a
// delivery that fails or is lost cannot leave two usable credentials.
func (app *Application) dispatchCredentialNotification(
	parent context.Context,
	logger dispatchLogger,
	flow string,
	deliver func(ctx context.Context) error,
) {
	app.notifications.Add(1)

	go func() {
		defer app.notifications.Done()

		defer func() {
			if r := recover(); r != nil {
				logger.Error(
					"credential notification delivery panicked",
					"flow", flow,
					"error", fmt.Sprint(r),
				)
			}
		}()

		ctx, cancel := context.WithTimeout(
			context.WithoutCancel(parent),
			notificationDeliveryTimeout,
		)
		defer cancel()

		if err := deliver(ctx); err != nil {
			logger.Warn(
				"credential notification delivery failed",
				"flow", flow,
				"error", err,
			)
		}
	}()
}

// WaitForNotifications blocks until in-flight credential deliveries finish or
// ctx ends, whichever is first. Called during graceful shutdown.
func (app *Application) WaitForNotifications(ctx context.Context) bool {
	done := make(chan struct{})

	go func() {
		app.notifications.Wait()
		close(done)
	}()

	select {
	case <-done:
		return true
	case <-ctx.Done():
		return false
	}
}
