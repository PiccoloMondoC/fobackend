package services

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

// resolveStatusID resolves an offer status using the configured DB timeout.
//
// Underlying datastore errors are preserved so callers can distinguish
// cancellation, timeout, not-found, and infrastructure failures using
// errors.Is/errors.As according to the data-layer contract.
func (s *Service) resolveStatusID(
	ctx context.Context,
	statusName string,
) (*uuid.UUID, error) {
	if ctx == nil {
		return nil, ErrNilContext
	}
	if err := s.validate(); err != nil {
		return nil, err
	}

	name := strings.TrimSpace(statusName)
	if name == "" {
		return nil, ErrStatusNameRequired
	}

	queryCtx, cancel := context.WithTimeout(ctx, s.Cfg.DBTimeout)
	defer cancel()

	status, err := s.Models.OfferStatus.GetByName(queryCtx, name)
	if err != nil {
		logger := s.Logger.
			GetLoggerWithContextFromContext(ctx).
			WithFunctionName("resolveStatusID")

		logger.Warn(
			"Offer status lookup failed",
			"status_name", name,
			"error", err,
		)

		return nil, fmt.Errorf(
			"resolve offer status %q: %w",
			name,
			err,
		)
	}

	id := status.ID
	return &id, nil
}
