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
