// Package data provides models and database access methods for offer statuses and related entities.
//
// File: sdworkspace/sdbackend/internal/data/offer_status.go
//
// GTM:
//
//	Layer: 2.5 Catalog / Offer Domain
//	Release Class: DEFERRED
//	Reason:
//	  Offer statuses are release-critical catalog governance infrastructure.
//	  They define the canonical offer workflow vocabulary, support publication
//	  and review transitions, protect seeded pending_review behavior, and
//	  preserve referential integrity for offers.status_id.
//
// SPINE Rule:
//
//	Keep compiling.
//	Keep production-ready.
//	Preserve offer status reference-data semantics.
//	Preserve active-status lookup behavior.
//	Preserve pending_review seeded-status dependency.
//	Preserve merchant-owned submit-for-review behavior.
//	Preserve foreign-key protected delete behavior.
//	Block deployment if this file breaks build, offer status resolution,
//	review submission, publication workflow, or catalog governance integrity.
package data

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/PiccoloMondoC/sdworkspace/sdbackend/internal/logging"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	offerStatusPendingReview = "pending_review"

	createOfferStatusQuery = `
		INSERT INTO offer_statuses (id, name, description)
		VALUES ($1, $2, $3)
		RETURNING id
	`

	getOfferStatusByIDQuery = `
		SELECT id, name, description
		FROM offer_statuses
		WHERE id = $1
	`

	getOfferStatusByNameQuery = `
		SELECT id, name, description
		FROM offer_statuses
		WHERE LOWER(name) = LOWER($1)
	`

	getAllActiveOfferStatusesQuery = `
		SELECT id, name, description
		FROM offer_statuses
		WHERE is_active = TRUE
		ORDER BY name ASC
	`

	updateOfferStatusQuery = `
		UPDATE offer_statuses
		SET name = $1,
			description = $2,
			updated_at = NOW()
		WHERE id = $3
		RETURNING id
	`

	deleteOfferStatusQuery = `
		DELETE FROM offer_statuses
		WHERE id = $1
		RETURNING id
	`

	getPendingReviewOfferStatusIDQuery = `
		SELECT id
		FROM offer_statuses
		WHERE name = $1
		  AND is_active = TRUE
		LIMIT 1
	`

	submitOfferForReviewQuery = `
		UPDATE offers
		SET status_id = $1,
			updated_at = NOW()
		WHERE id = $2
		  AND merchant_id = $3
		  AND deleted_at IS NULL
		RETURNING id
	`
)

// OfferStatus represents an offer status reference row in the system.
type OfferStatus struct {
	ID          uuid.UUID `json:"id" db:"id"`                   // Canonical UUID primary key.
	Name        string    `json:"name" db:"name"`               // Unique internal status name.
	Description string    `json:"description" db:"description"` // Human-readable status description.
}

// OfferStatusModel holds the database pool and logger for offer status operations.
type OfferStatusModel struct {
	DB     *pgxpool.Pool
	Logger *logging.Logger
}

// Insert creates a new offer status reference row.
//
// This method writes the full canonical row shape required by the current schema.
// The offer_statuses table requires both name and description.
func (m *OfferStatusModel) Insert(ctx context.Context, offerStatus *OfferStatus) error {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("InsertOfferStatus")

	if offerStatus == nil {
		err := errors.New("offer status is required")
		logger.Error("Validation failed", err)
		return err
	}

	offerStatus.Name = strings.TrimSpace(offerStatus.Name)
	offerStatus.Description = strings.TrimSpace(offerStatus.Description)

	if offerStatus.Name == "" {
		logger.Error("Validation failed", ErrOfferStatusNameRequired)
		return ErrOfferStatusNameRequired
	}
	if offerStatus.Description == "" {
		logger.Error("Validation failed", ErrOfferStatusDescriptionRequired)
		return ErrOfferStatusDescriptionRequired
	}

	if offerStatus.ID == uuid.Nil {
		offerStatus.ID = uuid.New()
	}

	if err := m.DB.QueryRow(ctx, createOfferStatusQuery, offerStatus.ID, offerStatus.Name, offerStatus.Description).Scan(&offerStatus.ID); err != nil {
		logger.Error("Insert offer status failed", err, "id", offerStatus.ID, "name", offerStatus.Name)
		return err
	}

	logger.Info(
		"Insert offer status successful",
		"id", offerStatus.ID,
		"name", offerStatus.Name,
	)

	return nil
}

// GetByID retrieves a single offer status by UUID.
//
// This returns the full canonical status record.
func (m *OfferStatusModel) GetByID(ctx context.Context, id uuid.UUID) (*OfferStatus, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("GetOfferStatusByID")

	if id == uuid.Nil {
		logger.Error("Validation failed", ErrOfferStatusIDRequired)
		return nil, ErrOfferStatusIDRequired
	}

	var offerStatus OfferStatus

	err := m.DB.QueryRow(ctx, getOfferStatusByIDQuery, id).Scan(
		&offerStatus.ID,
		&offerStatus.Name,
		&offerStatus.Description,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			logger.Warn("Offer status not found", "id", id)
			return nil, ErrOfferStatusNotFound
		}

		logger.Error("Query failed", err, "id", id)
		return nil, err
	}

	logger.Info("Retrieved offer status", "id", offerStatus.ID, "name", offerStatus.Name)

	return &offerStatus, nil
}

// GetByName retrieves a single offer status by name.
//
// This returns the full canonical status record.
func (m *OfferStatusModel) GetByName(ctx context.Context, name string) (*OfferStatus, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("GetOfferStatusByName")

	name = strings.TrimSpace(name)
	if name == "" {
		logger.Error("Validation failed", ErrOfferStatusNameRequired)
		return nil, ErrOfferStatusNameRequired
	}

	var status OfferStatus

	err := m.DB.QueryRow(ctx, getOfferStatusByNameQuery, name).Scan(
		&status.ID,
		&status.Name,
		&status.Description,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			logger.Warn("Offer status not found", "name", name)
			return nil, ErrOfferStatusNotFound
		}

		logger.Error("Query failed", err, "name", name)
		return nil, err
	}

	logger.Info("Retrieved offer status", "id", status.ID, "name", status.Name)

	return &status, nil
}

// GetAll retrieves all active offer statuses.
//
// Reference-data list reads should return active rows by default.
func (m *OfferStatusModel) GetAll(ctx context.Context) ([]*OfferStatus, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("GetAllOfferStatuses")

	rows, err := m.DB.Query(ctx, getAllActiveOfferStatusesQuery)
	if err != nil {
		logger.Error("Failed to retrieve offer statuses", err)
		return nil, err
	}
	defer rows.Close()

	offerStatuses, err := pgx.CollectRows(rows, pgx.RowToAddrOfStructByPos[OfferStatus])
	if err != nil {
		logger.Error("Failed to collect offer statuses", err)
		return nil, err
	}

	logger.Info("Successfully retrieved offer statuses", "count", len(offerStatuses))

	return offerStatuses, nil
}

// Update updates an existing offer status reference row.
//
// This method updates the full canonical mutable shape used by the schema.
func (m *OfferStatusModel) Update(ctx context.Context, offerStatus *OfferStatus) error {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("UpdateOfferStatus")

	if offerStatus == nil {
		err := errors.New("offer status is required")
		logger.Error("Validation failed", err)
		return err
	}

	offerStatus.Name = strings.TrimSpace(offerStatus.Name)
	offerStatus.Description = strings.TrimSpace(offerStatus.Description)

	if offerStatus.ID == uuid.Nil {
		logger.Error("Validation failed", ErrOfferStatusIDRequired)
		return ErrOfferStatusIDRequired
	}
	if offerStatus.Name == "" {
		logger.Error("Validation failed", ErrOfferStatusNameRequired)
		return ErrOfferStatusNameRequired
	}
	if offerStatus.Description == "" {
		logger.Error("Validation failed", ErrOfferStatusDescriptionRequired)
		return ErrOfferStatusDescriptionRequired
	}

	var updatedID uuid.UUID

	err := m.DB.QueryRow(ctx, updateOfferStatusQuery, offerStatus.Name, offerStatus.Description, offerStatus.ID).Scan(&updatedID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			logger.Warn("Offer status not found for update", "id", offerStatus.ID)
			return ErrOfferStatusNotFound
		}

		logger.Error("Update offer status failed", err, "id", offerStatus.ID, "name", offerStatus.Name)
		return err
	}

	logger.Info("Update offer status successful", "id", updatedID, "name", offerStatus.Name)

	return nil
}

// Delete permanently removes an offer status row by ID.
//
// The underlying schema already protects referential integrity through the
// offers.status_id foreign key with ON DELETE RESTRICT.
func (m *OfferStatusModel) Delete(ctx context.Context, id uuid.UUID) error {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("DeleteOfferStatus")

	if id == uuid.Nil {
		logger.Error("Validation failed", ErrOfferStatusIDRequired)
		return ErrOfferStatusIDRequired
	}

	var deletedID uuid.UUID

	err := m.DB.QueryRow(ctx, deleteOfferStatusQuery, id).Scan(&deletedID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			logger.Warn("No offer status found to delete", "id", id)
			return ErrOfferStatusNotFound
		}

		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23503" {
			wrappedErr := fmt.Errorf("cannot delete offer status %s: status is still referenced by one or more offers: %w", id, err)
			logger.Error("Delete offer status blocked by foreign key", wrappedErr, "id", id)
			return wrappedErr
		}

		logger.Error("Delete offer status failed", err, "id", id)
		return err
	}

	logger.Info("Delete offer status successful", "id", deletedID)

	return nil
}

// SubmitForReview transitions an offer owned by a given merchant into pending_review.
//
// This method aligns with the current offers schema:
//   - offers uses merchant_id, not seller_id
//   - offers does not have submitted_at
//   - pending review is represented by the seeded offer_statuses row named pending_review
func (m *OfferStatusModel) SubmitForReview(ctx context.Context, offerID uuid.UUID, merchantID uuid.UUID) error {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("SubmitOfferForReview")

	if offerID == uuid.Nil {
		logger.Error("Validation failed", ErrOfferIDRequired)
		return ErrOfferIDRequired
	}

	if merchantID == uuid.Nil {
		logger.Error("Validation failed", ErrMerchantIDRequired)
		return ErrMerchantIDRequired
	}

	var pendingReviewStatusID uuid.UUID

	err := m.DB.QueryRow(ctx, getPendingReviewOfferStatusIDQuery, offerStatusPendingReview).Scan(&pendingReviewStatusID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			logger.Error("Required seeded offer status missing", ErrPendingReviewStatusMissing, "status_name", offerStatusPendingReview)
			return ErrPendingReviewStatusMissing
		}

		logger.Error("Failed to resolve pending review status", err, "status_name", offerStatusPendingReview)
		return err
	}

	var updatedOfferID uuid.UUID

	err = m.DB.QueryRow(ctx, submitOfferForReviewQuery, pendingReviewStatusID, offerID, merchantID).Scan(&updatedOfferID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			logger.Warn("Offer not found for merchant review submission", "offer_id", offerID, "merchant_id", merchantID)
			return ErrOfferNotFoundForMerchant
		}

		logger.Error(
			"Submit offer for review failed",
			err,
			"offer_id", offerID,
			"merchant_id", merchantID,
			"status_id", pendingReviewStatusID,
		)
		return err
	}

	logger.Info(
		"Offer submitted for review successfully",
		"offer_id", updatedOfferID,
		"merchant_id", merchantID,
		"status_id", pendingReviewStatusID,
	)

	return nil
}
