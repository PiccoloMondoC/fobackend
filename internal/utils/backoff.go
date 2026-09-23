// Package utils provides general-purpose utility functions for string normalization,
// formatting, and lightweight transformations used across the application
// focodebase/fobackend/internal/utils/backoff.go
package utils

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"net/http"
	"time"
)

// Max retry attempts
const maxRetries = 5

// Initial delay before retrying
const initialBackoff = 1 * time.Second

// makeRequestWithBackoff attempts an HTTP request with exponential backoff.
func makeRequestWithBackoff(ctx context.Context, url string) (*http.Response, error) {
	var resp *http.Response
	var err error

	backoff := initialBackoff
	for attempt := 1; attempt <= maxRetries; attempt++ {
		// Make the HTTP request
		resp, err = http.Get(url)
		if err == nil && resp.StatusCode < 500 {
			// Success, return response
			return resp, nil
		}

		// If the request failed, calculate backoff time with jitter
		sleepDuration := backoff + time.Duration(rand.Intn(500))*time.Millisecond
		fmt.Printf("Attempt %d failed. Retrying in %v...\n", attempt, sleepDuration)

		// Wait before retrying
		select {
		case <-time.After(sleepDuration):
		case <-ctx.Done():
			return nil, errors.New("request cancelled")
		}

		// Double the backoff time
		backoff *= 2
	}

	return nil, errors.New("max retries reached")
}