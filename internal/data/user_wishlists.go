// Package data provides models and database access methods for user wishlists.
//
// sdworkspace/sdbackend/internal/data/user_wishlists.go
//
// GTM:
//   Layer: 2.3 Consumer Domain
//   Release Class: DEFERRED
//   Reason:
//     User wishlists are release-critical consumer purchase-intent
//     infrastructure. They support saved offers, My Stash behavior, return
//     visits, consumer personalization, gifting intent, and price-alert-adjacent
//     purchase planning required by the initial Platform release spine.
//
// SPINE Rule:
//   Keep compiling.
//   Keep production-ready.
//   Preserve consumer-owned wishlist semantics.
//   Preserve saved-offer and purchase-intent behavior.
//   Preserve soft-delete lifecycle behavior.
//   Preserve canonical list-name normalization.
//   Preserve DB-owned lifecycle timestamp behavior.
//   Block deployment if this file breaks build, wishlist persistence,
//   saved-offer behavior, purchase-intent tracking, or consumer engagement
//   integrity.
package data

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/PiccoloMondoC/sdworkspace/sdbackend/internal/logging"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// UserWishlist represents a consumer-owned saved offer in My Stash.
//
// Wishlist rows are user-scoped purchase-intent records. They are soft-deleted
// with deleted_at so removal preserves lifecycle meaning while ordinary reads
// exclude removed items.
type UserWishlist struct {
	ID        uuid.UUID  `json:"id"`
	UserID    uuid.UUID  `json:"user_id"`
	OfferID   uuid.UUID  `json:"offer_id"`
	ListName  string     `json:"list_name"`
	Notes     *string    `json:"notes,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
	DeletedAt *time.Time `json:"deleted_at,omitempty"`
}

// UserWishlistModel owns consumer wishlist persistence.
//
// This model is consumer-owned: every read and mutation is scoped by user_id.
// Delete is a soft-delete lifecycle transition. CreateOrRestore intentionally
// restores a previously soft-deleted matching row because the table has a
// canonical uniqueness contract on user_id, offer_id, and list_name.
type UserWishlistModel struct {
	DB     *pgxpool.Pool
	Logger *logging.Logger
}

// Create inserts a wishlist item or restores an existing soft-deleted match.
//
// Use CreateOrRestore when the caller needs to know whether the operation
// restored an older soft-deleted row.
func (m UserWishlistModel) Create(ctx context.Context, userID, offerID uuid.UUID, listName string, notes *string) (*UserWishlist, error) {
	item, _, err := m.CreateOrRestore(ctx, userID, offerID, listName, notes)
	return item, err
}

// CreateOrRestore inserts a wishlist item or restores an existing soft-deleted match.
//
// The returned boolean is true when the operation restored a row whose
// deleted_at was previously non-null.
func (m UserWishlistModel) CreateOrRestore(ctx context.Context, userID, offerID uuid.UUID, listName string, notes *string) (*UserWishlist, bool, error) {
	if m.DB == nil {
		return nil, false, fmt.Errorf("user wishlist model database pool is nil")
	}
	if userID == uuid.Nil {
		return nil, false, fmt.Errorf("user id is required")
	}
	if offerID == uuid.Nil {
		return nil, false, fmt.Errorf("offer id is required")
	}

	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	listName = normalizeWishlistListName(listName)
	notes = normalizeOptionalString(notes)

	const query = `
		INSERT INTO user_wishlists (user_id, offer_id, list_name, notes)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (user_id, offer_id, list_name)
		DO UPDATE SET
			notes = EXCLUDED.notes,
			deleted_at = NULL
		RETURNING
			id,
			user_id,
			offer_id,
			list_name,
			notes,
			created_at,
			updated_at,
			deleted_at,
			(xmax <> 0) AS conflict_update
	`

	var item UserWishlist
	var conflictUpdate bool

	err := m.DB.QueryRow(ctx, query, userID, offerID, listName, notes).Scan(
		&item.ID,
		&item.UserID,
		&item.OfferID,
		&item.ListName,
		&item.Notes,
		&item.CreatedAt,
		&item.UpdatedAt,
		&item.DeletedAt,
		&conflictUpdate,
	)
	if err != nil {
		return nil, false, fmt.Errorf("create or restore user wishlist item: %w", err)
	}

	return &item, conflictUpdate, nil
}

// Get retrieves one active wishlist item by ID and owner.
func (m UserWishlistModel) Get(ctx context.Context, userID, id uuid.UUID) (*UserWishlist, error) {
	if m.DB == nil {
		return nil, fmt.Errorf("user wishlist model database pool is nil")
	}
	if userID == uuid.Nil {
		return nil, fmt.Errorf("user id is required")
	}
	if id == uuid.Nil {
		return nil, fmt.Errorf("user wishlist id is required")
	}

	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	const query = `
		SELECT
			id,
			user_id,
			offer_id,
			list_name,
			notes,
			created_at,
			updated_at,
			deleted_at
		FROM user_wishlists
		WHERE id = $1
		  AND user_id = $2
		  AND deleted_at IS NULL
	`

	item, err := scanUserWishlist(m.DB.QueryRow(ctx, query, id, userID))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrUserWishlistNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get user wishlist item: %w", err)
	}

	return item, nil
}

// ListForUser returns active wishlist items for a user.
func (m UserWishlistModel) ListForUser(ctx context.Context, userID uuid.UUID, listName string, limit, offset int) ([]*UserWishlist, error) {
	if m.DB == nil {
		return nil, fmt.Errorf("user wishlist model database pool is nil")
	}
	if userID == uuid.Nil {
		return nil, fmt.Errorf("user id is required")
	}
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}

	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	listName = strings.TrimSpace(listName)
	if listName == "" {
		const query = `
			SELECT
				id,
				user_id,
				offer_id,
				list_name,
				notes,
				created_at,
				updated_at,
				deleted_at
			FROM user_wishlists
			WHERE user_id = $1
			  AND deleted_at IS NULL
			ORDER BY created_at DESC, id DESC
			LIMIT $2 OFFSET $3
		`

		rows, err := m.DB.Query(ctx, query, userID, limit, offset)
		if err != nil {
			return nil, fmt.Errorf("list user wishlist items: %w", err)
		}

		items, err := pgx.CollectRows(rows, collectUserWishlist)
		if err != nil {
			return nil, fmt.Errorf("collect user wishlist items: %w", err)
		}

		return items, nil
	}

	const query = `
		SELECT
			id,
			user_id,
			offer_id,
			list_name,
			notes,
			created_at,
			updated_at,
			deleted_at
		FROM user_wishlists
		WHERE user_id = $1
		  AND list_name = $2
		  AND deleted_at IS NULL
		ORDER BY created_at DESC, id DESC
		LIMIT $3 OFFSET $4
	`

	rows, err := m.DB.Query(ctx, query, userID, normalizeWishlistListName(listName), limit, offset)
	if err != nil {
		return nil, fmt.Errorf("list user wishlist items by list name: %w", err)
	}

	items, err := pgx.CollectRows(rows, collectUserWishlist)
	if err != nil {
		return nil, fmt.Errorf("collect user wishlist items by list name: %w", err)
	}

	return items, nil
}

// UpdateNotes updates the notes on an active wishlist item.
func (m UserWishlistModel) UpdateNotes(ctx context.Context, userID, id uuid.UUID, notes *string) (*UserWishlist, error) {
	if m.DB == nil {
		return nil, fmt.Errorf("user wishlist model database pool is nil")
	}
	if userID == uuid.Nil {
		return nil, fmt.Errorf("user id is required")
	}
	if id == uuid.Nil {
		return nil, fmt.Errorf("user wishlist id is required")
	}

	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	notes = normalizeOptionalString(notes)

	const query = `
		UPDATE user_wishlists
		SET notes = $3
		WHERE id = $1
		  AND user_id = $2
		  AND deleted_at IS NULL
		RETURNING
			id,
			user_id,
			offer_id,
			list_name,
			notes,
			created_at,
			updated_at,
			deleted_at
	`

	item, err := scanUserWishlist(m.DB.QueryRow(ctx, query, id, userID, notes))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrUserWishlistNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("update user wishlist notes: %w", err)
	}

	return item, nil
}

// Delete soft-deletes an active wishlist item.
func (m UserWishlistModel) Delete(ctx context.Context, userID, id uuid.UUID) error {
	if m.DB == nil {
		return fmt.Errorf("user wishlist model database pool is nil")
	}
	if userID == uuid.Nil {
		return fmt.Errorf("user id is required")
	}
	if id == uuid.Nil {
		return fmt.Errorf("user wishlist id is required")
	}

	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	const query = `
		UPDATE user_wishlists
		SET deleted_at = NOW()
		WHERE id = $1
		  AND user_id = $2
		  AND deleted_at IS NULL
	`

	tag, err := m.DB.Exec(ctx, query, id, userID)
	if err != nil {
		return fmt.Errorf("delete user wishlist item: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrUserWishlistNotFound
	}

	return nil
}

func collectUserWishlist(row pgx.CollectableRow) (*UserWishlist, error) {
	var item UserWishlist
	err := row.Scan(
		&item.ID,
		&item.UserID,
		&item.OfferID,
		&item.ListName,
		&item.Notes,
		&item.CreatedAt,
		&item.UpdatedAt,
		&item.DeletedAt,
	)
	if err != nil {
		return nil, err
	}

	return &item, nil
}

func scanUserWishlist(row pgx.Row) (*UserWishlist, error) {
	var item UserWishlist
	err := row.Scan(
		&item.ID,
		&item.UserID,
		&item.OfferID,
		&item.ListName,
		&item.Notes,
		&item.CreatedAt,
		&item.UpdatedAt,
		&item.DeletedAt,
	)
	if err != nil {
		return nil, err
	}

	return &item, nil
}

func normalizeWishlistListName(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return "default"
	}
	return value
}