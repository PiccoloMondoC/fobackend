// Package utils provides general-purpose utility functions for string normalization,
// formatting, and lightweight transformations used across the application
// sdworkspace/sdbackend/internal/utils/urlutil.go
package utils

import (
	"strings"
)

// normalizeOfferURL simplifies URL for duplicate checks by stripping UTM params and anchors.
func normalizeOfferURL(raw string) string {
	// Trim and lower
	url := strings.ToLower(strings.TrimSpace(raw))
	// Remove UTM and fragments (basic)
	if idx := strings.Index(url, "?"); idx != -1 {
		url = url[:idx]
	}
	if idx := strings.Index(url, "#"); idx != -1 {
		url = url[:idx]
	}
	return url
}
