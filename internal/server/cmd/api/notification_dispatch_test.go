// focodebase/fobackend/internal/server/cmd/api/notification_dispatch_test.go
package main

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

type recordingDispatchLogger struct {
	mu    sync.Mutex
	warns []string
	errs  []string
}

func (l *recordingDispatchLogger) Warn(msg string, _ ...interface{}) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.warns = append(l.warns, msg)
}

func (l *recordingDispatchLogger) Error(msg string, _ ...interface{}) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.errs = append(l.errs, msg)
}

func TestDispatchCredentialNotificationOutlivesRequestCancellation(t *testing.T) {
	app := &Application{}
	logger := &recordingDispatchLogger{}

	parent, cancelRequest := context.WithCancel(context.Background())

	release := make(chan struct{})
	var deliveredErr error

	app.dispatchCredentialNotification(parent, logger, "test", func(ctx context.Context) error {
		<-release
		deliveredErr = ctx.Err()
		return nil
	})

	// The handler returns and the request context is cancelled.
	cancelRequest()
	close(release)

	waitCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if !app.WaitForNotifications(waitCtx) {
		t.Fatal("delivery did not finish")
	}
	if deliveredErr != nil {
		t.Fatalf("delivery context was cancelled with the request: %v", deliveredErr)
	}
	if _, ok := parent.Deadline(); ok {
		t.Fatal("unexpected deadline on parent")
	}
}

func TestDispatchCredentialNotificationIsBounded(t *testing.T) {
	app := &Application{}
	logger := &recordingDispatchLogger{}

	var hasDeadline bool
	app.dispatchCredentialNotification(context.Background(), logger, "test", func(ctx context.Context) error {
		_, hasDeadline = ctx.Deadline()
		return nil
	})

	waitCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	app.WaitForNotifications(waitCtx)

	if !hasDeadline {
		t.Fatal("delivery context has no deadline")
	}
}

func TestDispatchCredentialNotificationContainsFailuresAndPanics(t *testing.T) {
	app := &Application{}
	logger := &recordingDispatchLogger{}

	app.dispatchCredentialNotification(context.Background(), logger, "fail", func(context.Context) error {
		return errors.New("smtp down")
	})
	app.dispatchCredentialNotification(context.Background(), logger, "panic", func(context.Context) error {
		panic("boom")
	})

	waitCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if !app.WaitForNotifications(waitCtx) {
		t.Fatal("deliveries did not finish")
	}

	logger.mu.Lock()
	defer logger.mu.Unlock()
	if len(logger.warns) != 1 || len(logger.errs) != 1 {
		t.Fatalf("warns=%v errs=%v", logger.warns, logger.errs)
	}
}
