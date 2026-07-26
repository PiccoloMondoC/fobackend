// Package services contains business logic for internal operations such as moderation,
// automation, audit logging, and data synchronization. It is used by async routines
// and internal system workflows, not exposed via public API routes.
//
// sdworkspace/sdbackend/internal/services/offers_internal.go
//
// GTM:
//
//	Layer: 2.5 Catalog / Offer Domain
//	Release Class: SPINE
//	Reason:
//	  Owns internal service-layer business logic for the canonical offer
//	  lifecycle: curated-offer intake, editorial approval/rejection, AI
//	  description generation, fraud detection, expiration and removal,
//	  personalized suggestion generation, status transitions, flagging, and
//	  blacklisting. These operations mutate the SPINE-tier offers table and
//	  drive audit-logged catalog integrity for Platform.
//
// SPINE Rule:
//
//	Keep compiling.
//	Keep production-ready.
//	Preserve offer lifecycle state transitions.
//	Preserve audit logging on every mutating operation.
//	Preserve editorial-approval gating before catalog exposure.
//	Preserve duplicate-detection and fraud-heuristic integrity.
//	Block deployment if this file breaks build, offer lifecycle correctness,
//	audit traceability, or catalog integrity.
package services

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/PiccoloMondoC/sdworkspace/sdbackend/internal/data"
	"github.com/PiccoloMondoC/sdworkspace/sdbackend/internal/shared/models"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

const suspiciousDiscountThreshold = 90.0

func mapOfferLiteToData(lite *models.OfferLite) *data.Offer {
	if lite == nil {
		return nil
	}

	return &data.Offer{
		ID:                  lite.ID,
		Title:               strings.TrimSpace(lite.Title),
		MerchantID:          lite.MerchantID,
		IsEditorialApproved: lite.IsEditorialApproved,
		StatusID:            &lite.StatusID,
	}
}

func (s *Service) resolveOfferStatusIDInternal(ctx context.Context, statusName string) (uuid.UUID, error) {
	statusName = strings.ToLower(strings.TrimSpace(statusName))
	if statusName == "" {
		return uuid.Nil, errors.New("offer status name is required")
	}

	var id uuid.UUID
	err := s.Models.Offer.DB.QueryRow(ctx, `
		SELECT id
		FROM offer_statuses
		WHERE name = $1
	`, statusName).Scan(&id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return uuid.Nil, fmt.Errorf("offer status %q not found", statusName)
		}
		return uuid.Nil, err
	}

	return id, nil
}

func (s *Service) insertOfferAuditInternal(
	ctx context.Context,
	actionName string,
	actionDescription string,
	entityID uuid.UUID,
	actorID *uuid.UUID,
) error {
	if entityID == uuid.Nil {
		return errors.New("entity ID is required")
	}

	actionID, err := s.Models.Action.CreateIfNotExists(ctx, actionName, actionDescription)
	if err != nil {
		return fmt.Errorf("resolve audit action: %w", err)
	}

	entityTypeID, err := s.Models.EntityType.CreateIfNotExists(ctx, "offer", "Offer")
	if err != nil {
		return fmt.Errorf("resolve audit entity type: %w", err)
	}

	return s.Models.AuditLog.Insert(ctx, &data.AuditLog{
		ID:           uuid.New(),
		UserID:       actorID,
		ActionID:     actionID,
		EntityTypeID: entityTypeID,
		EntityID:     entityID.String(),
	})
}

func offerDecimalGreaterThanZero(v *string) bool {
	if v == nil {
		return false
	}

	raw := strings.TrimSpace(*v)
	if raw == "" {
		return false
	}

	n, err := strconv.ParseFloat(raw, 64)
	return err == nil && n > 0
}

func offerDecimalGreaterThan(v *string, threshold float64) bool {
	if v == nil {
		return false
	}

	raw := strings.TrimSpace(*v)
	if raw == "" {
		return false
	}

	n, err := strconv.ParseFloat(raw, 64)
	return err == nil && n > threshold
}

func (s *Service) InsertCuratedOfferInternal(ctx context.Context, lite *models.OfferLite) error {
	logger := s.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("InsertCuratedOfferInternal")

	if lite == nil {
		return errors.New("offer must not be nil")
	}

	offer := mapOfferLiteToData(lite)
	if offer == nil {
		return errors.New("offer mapping failed")
	}
	if !offer.IsEditorialApproved {
		return errors.New("offer is not editorially approved")
	}
	if offer.StatusID == nil || *offer.StatusID == uuid.Nil {
		return errors.New("offer status ID is required")
	}

	if offer.ID == uuid.Nil {
		offer.ID = uuid.New()
	}

	isDup, err := s.Models.Offer.IsDuplicate(ctx, offer)
	if err != nil {
		logger.Error("Duplicate check failed", "error", err)
		return err
	}
	if isDup {
		return errors.New("duplicate offer")
	}

	if err := s.Models.Offer.Insert(ctx, offer); err != nil {
		logger.Error("Offer insertion failed", "offer_id", offer.ID, "error", err)
		return err
	}

	if err := s.insertOfferAuditInternal(ctx, "insert_curated_offer", "Insert curated offer into system", offer.ID, nil); err != nil {
		logger.Warn("Audit log insertion failed", "offer_id", offer.ID, "error", err)
	}

	return nil
}

func (s *Service) ApproveCuratedOfferInternal(ctx context.Context, offerID, approvedBy uuid.UUID) error {
	logger := s.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("ApproveCuratedOfferInternal")

	if offerID == uuid.Nil || approvedBy == uuid.Nil {
		return errors.New("offerID and approvedBy are required")
	}

	statusID, err := s.resolveOfferStatusIDInternal(ctx, "approved")
	if err != nil {
		return err
	}

	tag, err := s.Models.Offer.DB.Exec(ctx, `
		UPDATE offers
		SET
			is_editorial_approved = TRUE,
			status_id = $2,
			is_active = TRUE,
			published_at = COALESCE(published_at, NOW()),
			updated_at = NOW()
		WHERE id = $1
		  AND deleted_at IS NULL
	`, offerID, statusID)
	if err != nil {
		return fmt.Errorf("approve curated offer: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return data.ErrOfferNotFound
	}

	if err := s.insertOfferAuditInternal(ctx, "approve_curated_offer", "Approve curated offer by editorial staff", offerID, &approvedBy); err != nil {
		logger.Warn("Audit log insertion failed", "offer_id", offerID, "error", err)
	}

	return nil
}

func (s *Service) RejectCuratedOfferInternal(ctx context.Context, offerID, rejectedBy uuid.UUID) error {
	logger := s.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("RejectCuratedOfferInternal")

	if offerID == uuid.Nil || rejectedBy == uuid.Nil {
		return errors.New("offerID and rejectedBy are required")
	}

	statusID, err := s.resolveOfferStatusIDInternal(ctx, "rejected")
	if err != nil {
		return err
	}

	tag, err := s.Models.Offer.DB.Exec(ctx, `
		UPDATE offers
		SET
			is_editorial_approved = FALSE,
			status_id = $2,
			is_active = FALSE,
			published_at = NULL,
			updated_at = NOW()
		WHERE id = $1
		  AND deleted_at IS NULL
	`, offerID, statusID)
	if err != nil {
		return fmt.Errorf("reject curated offer: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return data.ErrOfferNotFound
	}

	if err := s.insertOfferAuditInternal(ctx, "reject_curated_offer", "Reject curated offer via moderation", offerID, &rejectedBy); err != nil {
		logger.Warn("Audit log insertion failed", "offer_id", offerID, "error", err)
	}

	return nil
}

func (s *Service) GenerateOfferDescriptionInternal(ctx context.Context, offer *data.Offer) error {
	logger := s.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("GenerateOfferDescriptionInternal")

	if offer == nil {
		return errors.New("offer must not be nil")
	}
	if offer.ID == uuid.Nil {
		return errors.New("offer ID is required")
	}
	if strings.TrimSpace(offer.Title) == "" {
		return errors.New("offer title is required")
	}
	if offer.Description != nil && strings.TrimSpace(*offer.Description) != "" {
		return errors.New("description already exists")
	}

	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	description, err := s.generateTextualDescriptionForOffer(ctx, offer)
	if err != nil {
		return fmt.Errorf("generate offer description: %w", err)
	}

	tag, err := s.Models.Offer.DB.Exec(ctx, `
		UPDATE offers
		SET description = $2,
			updated_at = NOW()
		WHERE id = $1
		  AND deleted_at IS NULL
	`, offer.ID, strings.TrimSpace(description))
	if err != nil {
		return fmt.Errorf("persist generated offer description: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return data.ErrOfferNotFound
	}

	if err := s.insertOfferAuditInternal(ctx, "generate_offer_description", "AI-generated description for curated offer", offer.ID, nil); err != nil {
		logger.Warn("Audit log insertion failed", "offer_id", offer.ID, "error", err)
	}

	return nil
}

func (s *Service) DetectFraudulentOffersInternal(ctx context.Context) error {
	logger := s.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("DetectFraudulentOffersInternal")

	offers, err := s.Models.Offer.GetAll(ctx, map[string]any{
		"is_active":             true,
		"is_editorial_approved": true,
		"include_soft_deleted":  false,
	}, 100, 0)
	if err != nil {
		return err
	}

	for _, offer := range offers {
		if offer == nil || offer.ID == uuid.Nil {
			continue
		}

		reasons := make([]string, 0, 2)

		if offerDecimalGreaterThan(offer.DiscountPercent, suspiciousDiscountThreshold) {
			reasons = append(reasons, "suspiciously high discount percent")
		}
		if offer.Price != nil && offer.ListPrice != nil {
			price, priceErr := strconv.ParseFloat(strings.TrimSpace(*offer.Price), 64)
			listPrice, listErr := strconv.ParseFloat(strings.TrimSpace(*offer.ListPrice), 64)
			if priceErr == nil && listErr == nil && listPrice > 0 && price > listPrice {
				reasons = append(reasons, "price exceeds list price")
			}
		}

		if len(reasons) == 0 {
			continue
		}

		if err := s.FlagOfferInternal(ctx, offer.ID, nil, strings.Join(reasons, "; ")); err != nil {
			logger.Warn("Fraud flag failed", "offer_id", offer.ID, "error", err)
		}
	}

	return nil
}

func (s *Service) AutoExpireOffersInternal(ctx context.Context) error {
	logger := s.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("AutoExpireOffersInternal")

	statusID, err := s.resolveOfferStatusIDInternal(ctx, "expired")
	if err != nil {
		return err
	}

	rows, err := s.Models.Offer.DB.Query(ctx, `
		UPDATE offers
		SET
			is_active = FALSE,
			status_id = $1,
			updated_at = NOW()
		WHERE deleted_at IS NULL
		  AND is_active = TRUE
		  AND expires_at IS NOT NULL
		  AND expires_at <= NOW()
		RETURNING id
	`, statusID)
	if err != nil {
		return fmt.Errorf("auto-expire offers: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var offerID uuid.UUID
		if err := rows.Scan(&offerID); err != nil {
			return err
		}
		if err := s.insertOfferAuditInternal(ctx, "auto_expire_offer", "Automatically expire offer after expiration time", offerID, nil); err != nil {
			logger.Warn("Audit log insertion failed", "offer_id", offerID, "error", err)
		}
	}

	return rows.Err()
}

func (s *Service) RemoveExpiredOffersInternal(ctx context.Context) error {
	logger := s.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("RemoveExpiredOffersInternal")

	rows, err := s.Models.Offer.DB.Query(ctx, `
		UPDATE offers
		SET
			deleted_at = NOW(),
			is_active = FALSE,
			updated_at = NOW()
		WHERE deleted_at IS NULL
		  AND expires_at IS NOT NULL
		  AND expires_at <= NOW() - INTERVAL '30 days'
		RETURNING id
	`)
	if err != nil {
		return fmt.Errorf("remove expired offers: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var offerID uuid.UUID
		if err := rows.Scan(&offerID); err != nil {
			return err
		}
		if err := s.insertOfferAuditInternal(ctx, "remove_expired_offer", "Soft-remove expired offer after retention window", offerID, nil); err != nil {
			logger.Warn("Audit log insertion failed", "offer_id", offerID, "error", err)
		}
	}

	return rows.Err()
}

func (s *Service) SuggestOffersForUserInternal(ctx context.Context, userID uuid.UUID, limit int) ([]uuid.UUID, error) {
	if userID == uuid.Nil {
		return nil, errors.New("userID is required")
	}
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}

	rows, err := s.Models.Offer.DB.Query(ctx, `
		SELECT id
		FROM offers
		WHERE is_active = TRUE
		  AND is_editorial_approved = TRUE
		  AND deleted_at IS NULL
		  AND published_at IS NOT NULL
		  AND (expires_at IS NULL OR expires_at > NOW())
		ORDER BY published_at DESC, created_at DESC
		LIMIT $1
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	ids := make([]uuid.UUID, 0, limit)
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}

	return ids, rows.Err()
}

func (s *Service) ListEligibleUsersForSuggestionsInternal(ctx context.Context) ([]uuid.UUID, error) {
	rows, err := s.Models.Offer.DB.Query(ctx, `
		SELECT DISTINCT id
		FROM users
		WHERE deleted_at IS NULL
		ORDER BY id
		LIMIT 1000
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	userIDs := make([]uuid.UUID, 0)
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		userIDs = append(userIDs, id)
	}

	return userIDs, rows.Err()
}

func (s *Service) UpdateOfferStatusInternal(ctx context.Context, offerID uuid.UUID, newStatus string, actorID uuid.UUID) error {
	logger := s.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("UpdateOfferStatusInternal")

	if offerID == uuid.Nil || actorID == uuid.Nil {
		return errors.New("offerID and actorID are required")
	}

	statusID, err := s.resolveOfferStatusIDInternal(ctx, newStatus)
	if err != nil {
		return err
	}

	normalizedStatus := strings.ToLower(strings.TrimSpace(newStatus))

	tag, err := s.Models.Offer.DB.Exec(ctx, `
		UPDATE offers
		SET
			status_id = $2,
			is_editorial_approved = CASE
				WHEN $3 = 'approved' THEN TRUE
				WHEN $3 IN ('rejected', 'expired', 'blacklisted') THEN FALSE
				ELSE is_editorial_approved
			END,
			is_active = CASE
				WHEN $3 = 'approved' THEN TRUE
				WHEN $3 IN ('rejected', 'expired', 'blacklisted') THEN FALSE
				ELSE is_active
			END,
			published_at = CASE
				WHEN $3 = 'approved' THEN COALESCE(published_at, NOW())
				WHEN $3 IN ('rejected', 'blacklisted') THEN NULL
				ELSE published_at
			END,
			updated_at = NOW()
		WHERE id = $1
		  AND deleted_at IS NULL
	`, offerID, statusID, normalizedStatus)
	if err != nil {
		return fmt.Errorf("update offer status: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return data.ErrOfferNotFound
	}

	if err := s.insertOfferAuditInternal(ctx, "update_offer_status", "Update offer lifecycle status", offerID, &actorID); err != nil {
		logger.Warn("Audit log insertion failed", "offer_id", offerID, "error", err)
	}

	return nil
}

func (s *Service) FlagOfferInternal(ctx context.Context, offerID uuid.UUID, flaggedBy *uuid.UUID, reason string) error {
	logger := s.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("FlagOfferInternal")

	if offerID == uuid.Nil {
		return errors.New("offerID is required")
	}
	if flaggedBy != nil && *flaggedBy == uuid.Nil {
		return errors.New("flaggedBy must be nil or a valid UUID")
	}
	if strings.TrimSpace(reason) == "" {
		return errors.New("flag reason is required")
	}

	tag, err := s.Models.Offer.DB.Exec(ctx, `
		UPDATE offers
		SET updated_at = NOW()
		WHERE id = $1
		  AND deleted_at IS NULL
	`, offerID)
	if err != nil {
		return fmt.Errorf("flag offer: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return data.ErrOfferNotFound
	}

	if err := s.insertOfferAuditInternal(ctx, "flag_offer", "Flag offer for internal review: "+strings.TrimSpace(reason), offerID, flaggedBy); err != nil {
		logger.Warn("Audit log insertion failed", "offer_id", offerID, "error", err)
	}

	return nil
}

func (s *Service) ListOffersToAutoFlagInternal(ctx context.Context) ([]uuid.UUID, error) {
	rows, err := s.Models.Offer.DB.Query(ctx, `
		SELECT id
		FROM offers
		WHERE deleted_at IS NULL
		  AND is_active = TRUE
		  AND (
			discount_percent > $1
			OR (
				price IS NOT NULL
				AND list_price IS NOT NULL
				AND price > list_price
			)
		  )
		ORDER BY updated_at DESC
		LIMIT 500
	`, suspiciousDiscountThreshold)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	offerIDs := make([]uuid.UUID, 0)
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		offerIDs = append(offerIDs, id)
	}

	return offerIDs, rows.Err()
}

func (s *Service) BlacklistOfferInternal(ctx context.Context, offerID uuid.UUID, blacklistedBy uuid.UUID) error {
	logger := s.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("BlacklistOfferInternal")

	if offerID == uuid.Nil || blacklistedBy == uuid.Nil {
		return errors.New("offerID and blacklistedBy are required")
	}

	statusID, err := s.resolveOfferStatusIDInternal(ctx, "blacklisted")
	if err != nil {
		return err
	}

	tag, err := s.Models.Offer.DB.Exec(ctx, `
		UPDATE offers
		SET
			status_id = $2,
			is_editorial_approved = FALSE,
			is_active = FALSE,
			published_at = NULL,
			updated_at = NOW()
		WHERE id = $1
		  AND deleted_at IS NULL
	`, offerID, statusID)
	if err != nil {
		return fmt.Errorf("blacklist offer: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return data.ErrOfferNotFound
	}

	if err := s.insertOfferAuditInternal(ctx, "blacklist_offer", "Blacklist offer from catalog exposure", offerID, &blacklistedBy); err != nil {
		logger.Warn("Audit log insertion failed", "offer_id", offerID, "error", err)
	}

	return nil
}
