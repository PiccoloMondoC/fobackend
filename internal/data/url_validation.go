// Package data provides shared data-layer validation helpers.
//
// sdworkspace/sdbackend/internal/data/url_validation.go
//
// GTM:
//
//	Layer: 2.1 Database / Governance Foundation
//	Release Class: SPINE
//	Reason:
//	  Shared URL validation is release-critical data-layer safety
//	  infrastructure. It protects persisted URL fields across model files by
//	  enforcing absolute HTTP/HTTPS URLs, rejecting embedded user-info, and
//	  blocking decoded path traversal segments before unsafe values can enter
//	  persistence.
//
// SPINE Rule:
//
//	Keep compiling.
//	Keep production-ready.
//	Preserve canonical validated-value return behavior.
//	Preserve HTTP/HTTPS-only validation.
//	Preserve user-info rejection.
//	Preserve decoded path traversal rejection.
//	Block deployment if this file breaks build, URL validation,
//	persistence safety, or shared data-layer input integrity.
package data

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
)

// validateHTTPURL validates and returns a trimmed absolute HTTP/HTTPS URL.
//
// This helper validates transport safety only. It does not verify that the URL
// exists, belongs to an approved merchant, points to an image, or satisfies
// affiliate-program-specific path rules.
//
// Security rule: URLs containing user-info, such as
// https://user:pass@example.com, are rejected so credentials cannot be accepted,
// persisted, or logged as part of URL fields.
//
// Security rule: decoded path traversal segments are rejected.
func validateHTTPURL(raw string) (string, error) {
	normalized := strings.TrimSpace(raw)
	if normalized == "" {
		return "", errors.New("url is required")
	}

	u, err := url.Parse(normalized)
	if err != nil {
		return "", fmt.Errorf("parse url: %w", err)
	}

	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" {
		return "", fmt.Errorf("unsupported url scheme: %q", u.Scheme)
	}

	if u.Hostname() == "" {
		return "", errors.New("url host is required")
	}

	if u.User != nil {
		return "", errors.New("url must not include user info")
	}

	if hasPathTraversal(u.Path) {
		return "", errors.New("url path must not contain traversal segments")
	}

	return u.String(), nil
}

// hasPathTraversal reports whether the already-parsed URL path contains a
// decoded traversal segment.
//
// Callers must pass url.URL.Path, not RawPath or the pre-parsed input string.
// net/url exposes Path after percent-decoding, so encoded traversal attempts
// such as %2e%2e, .%2e, and %2e. are evaluated as ".." here.
func hasPathTraversal(path string) bool {
	for _, segment := range strings.Split(path, "/") {
		if segment == "." || segment == ".." {
			return true
		}
	}

	return false
}
