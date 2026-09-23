// Package services contains trusted internal service composition,
// automation support, moderation helpers, and shared internal workflow logic.
//
// focodebase/fobackend/internal/services/pagination.go
//
// GTM:
//
//	Layer: 2.6 Internal Services / Automation Foundation
//	Release Class: SPINE
//	Reason:
//	  Provides bounded pagination normalization for interactive internal reads
//	  and larger internal automation batches.
//
//	  The two established caller contracts intentionally retain different
//	  defaults and ceilings according to workload class.
//
// SPINE Rule:
//
//	Keep compiling.
//	Keep production-ready.
//	Preserve bounded limits for every internal query path.
//	Preserve the established interactive defaults of 10 and maximum of 100.
//	Preserve the established internal-batch defaults of 100 and maximum of 500.
//	Never permit negative offsets or unbounded limits.
//	Keep pagination normalization deterministic and side-effect free.
//	Do not add per-call logging to pure clamping operations.
package services

import "context"

const (
	defaultInteractiveLimit = 10
	maxInteractiveLimit     = 100
	defaultInternalLimit    = 100
	maxInternalLimit        = 500
)

// parseLimit preserves the established interactive pagination contract.
func (s *Service) parseLimit(_ context.Context, rawLimit int) int {
	return clampLimit(
		rawLimit,
		defaultInteractiveLimit,
		maxInteractiveLimit,
	)
}

// parseLimitOffsetInternal preserves the established internal-job pagination
// contract.
func (s *Service) parseLimitOffsetInternal(
	_ context.Context,
	rawLimit int,
	rawOffset int,
) (int, int) {
	limit := clampLimit(
		rawLimit,
		defaultInternalLimit,
		maxInternalLimit,
	)

	offset := rawOffset
	if offset < 0 {
		offset = 0
	}

	return limit, offset
}

func clampLimit(rawLimit, defaultLimit, maxLimit int) int {
	switch {
	case rawLimit <= 0:
		return defaultLimit
	case rawLimit > maxLimit:
		return maxLimit
	default:
		return rawLimit
	}
}
