// Package data provides shared data-layer lookup helpers.
//
// sdworkspace/sdbackend/internal/data/category_lookups.go
//
// GTM:
//   Layer: 2.5 Catalog / Offer Domain
//   Release Class: SPINE
//   Reason:
//     Category-to-department lookup is release-critical because Sagrenti persists
//     only the leaf category_id on offers while the frontend remains
//     department-first. This helper preserves the canonical offer contract by
//     resolving the root department from the category tree.
//
// SPINE Rule:
//   Keep compiling.
//   Keep production-ready.
//   Preserve leaf-category persistence with department-first resolution.
//   Preserve category soft-delete filtering.
//   Preserve typed ErrCategoryNotFound behavior.
//   Block deployment if this file breaks build, offer hydration,
//   department-first routing/rendering, or category integrity.
package data

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// lookupDepartmentIDForCategory resolves the root department for a category.
//
// Sagrenti persists only the leaf category_id on offers, while the frontend
// remains department-first. This helper preserves that contract by resolving
// the category's canonical department_id.
//
// Timeout and structured logging belong to the caller/model method.
func lookupDepartmentIDForCategory(ctx context.Context, db *pgxpool.Pool, categoryID uuid.UUID) (uuid.UUID, error) {
	if categoryID == uuid.Nil {
		return uuid.Nil, errors.New("category ID is required")
	}

	const query = `
		SELECT c.department_id
		FROM categories c
		WHERE c.id = $1
		  AND c.deleted_at IS NULL
	`

	var departmentID uuid.UUID
	if err := db.QueryRow(ctx, query, categoryID).Scan(&departmentID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return uuid.Nil, ErrCategoryNotFound
		}

		return uuid.Nil, fmt.Errorf("lookup department ID for category %s: %w", categoryID, err)
	}

	return departmentID, nil
}