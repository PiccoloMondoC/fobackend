// Package services contains business logic for internal operations such as moderation,
// automation, audit logging, and data synchronization. It is used by async routines
// and internal system workflows, not exposed via public API routes.
//
// focodebase/fobackend/internal/services/async/render_async.go
package services

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// RenderNotificationMessage generates a user-facing message from event metadata.
// Uses notification type and offer title (if available) to generate the final message.
func RenderNotificationMessage(ctx context.Context, db *pgxpool.Pool, notifType string, offerID *uuid.UUID) (string, error) {
	switch notifType {
	case "price_drop":
		if offerID == nil {
			return "", errors.New("offer ID is required for price_drop notification")
		}
		var offerTitle string
		err := db.QueryRow(ctx, `SELECT title FROM offers WHERE id = $1`, offerID).Scan(&offerTitle)
		if err != nil {
			return "", fmt.Errorf("fetching offer title: %w", err)
		}
		return fmt.Sprintf("Good news! '%s' just dropped in price!", offerTitle), nil

	case "new_offer":
		if offerID == nil {
			return "", errors.New("offer ID is required for new_offer notification")
		}
		var offerTitle string
		err := db.QueryRow(ctx, `SELECT title FROM offers WHERE id = $1`, offerID).Scan(&offerTitle)
		if err != nil {
			return "", fmt.Errorf("fetching offer title: %w", err)
		}
		return fmt.Sprintf("Check out this new offer: '%s'", offerTitle), nil

	default:
		return "", fmt.Errorf("unsupported notification type: %s", notifType)
	}
}
