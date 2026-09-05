// Package main provides HTTP handlers for the Platform API.
//
// sdworkspace/sdbackend/internal/server/cmd/api/offer_sponsorships.go
//
// GTM:
//
//	Layer: 2.5 Catalog / Offer Domain
//	Release Class: DEFERRED
//	Reason:
//	  Offer sponsorships are valid future paid-placement and monetization
//	  infrastructure, but they are not required for the initial
//	  Platform release spine. The initial release prioritizes canonical
//	  offers, publication governance, commerce routing, attribution,
//	  price history, merchant foundations, and the Future Offering Platform
//	  supported by its Monetization Layer.
//
// DEFERRED Rule:
//
//	Keep compiling.
//	Keep safe.
//	Preserve canonical NUMERIC-safe decimal-string money contracts.
//	Preserve sponsorship bid-type and bid-minimum validation in the data layer.
//	Preserve CPD overlap protection.
//	Preserve the 30-minute sponsorship edit window.
//	Preserve immutable offer and merchant ownership.
//	Preserve soft-delete lifecycle behavior.
//	Do not duplicate data-layer bid or budget validation in handlers.
//	Do not add new API features.
//	Do not expose offer-sponsorship workflows in the v1 API.
//	Do not perform routing or UI expansion for this deferred domain.
//	Do not block deployment on this file unless it breaks the build.
package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/PiccoloMondoC/focodebase/fobackend/internal/data"
	"github.com/PiccoloMondoC/focodebase/fobackend/internal/utils/timeutil"

	"github.com/google/uuid"
)

// Principles:
// Always extract sensitive identifiers from a trusted context

// SponsorOfferHandler creates an offer sponsorship through the canonical
// data-layer sponsorship write path.
//
// This handler remains compile-safe while the offer-sponsorship domain is
// deferred and is not registered in the v1 router.
func (app *Application) SponsorOfferHandler(w http.ResponseWriter, r *http.Request) {
	logger := app.Logger.GetLoggerWithContext(r).WithFunctionName("SponsorOfferHandler")

	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	if !app.HasPermission(ctx, "sponsor_offer") {
		app.respondWithError(
			w,
			errors.New("forbidden: insufficient permissions"),
			http.StatusForbidden,
		)
		return
	}

	offerID := app.getOfferIDFromContext(ctx)
	merchantID := app.getMerchantIDFromContext(ctx)
	userID := app.getUserIDFromContext(ctx)
	if offerID == nil || merchantID == nil || userID == nil {
		app.respondWithError(
			w,
			errors.New("missing required context identifiers"),
			http.StatusBadRequest,
		)
		return
	}

	var input struct {
		BidTypeID uuid.UUID `json:"bid_type_id"`
		BidAmount string    `json:"bid_amount"`
		MaxBudget *string   `json:"max_budget,omitempty"`
		StartDate time.Time `json:"start_date"`
		EndDate   time.Time `json:"end_date"`
	}

	if err := app.readJSON(w, r, &input); err != nil {
		app.respondWithError(
			w,
			fmt.Errorf("invalid JSON input: %w", err),
			http.StatusBadRequest,
		)
		return
	}

	if input.BidTypeID == uuid.Nil {
		app.respondWithError(
			w,
			errors.New("bid_type_id is required"),
			http.StatusBadRequest,
		)
		return
	}

	if input.StartDate.IsZero() || input.EndDate.IsZero() {
		app.respondWithError(
			w,
			errors.New("start_date and end_date are required"),
			http.StatusBadRequest,
		)
		return
	}

	if !input.EndDate.After(input.StartDate) {
		app.respondWithError(
			w,
			errors.New("end_date must be after start_date"),
			http.StatusBadRequest,
		)
		return
	}

	if err := app.Models.OfferSponsorship.SponsorOffer(
		ctx,
		*offerID,
		*merchantID,
		input.StartDate,
		input.EndDate,
		input.BidTypeID,
		input.BidAmount,
		input.MaxBudget,
	); err != nil {
		logger.Error("Failed to sponsor offer", "error", err)
		app.respondWithError(
			w,
			fmt.Errorf("failed to sponsor offer: %w", err),
			http.StatusInternalServerError,
		)
		return
	}

	editUntil := timeutil.Now().Add(30 * time.Minute)

	action, err := app.Models.Action.GetByName(ctx, "sponsor_offer")
	if err != nil || action == nil {
		logger.Warn("Audit action not found; attempting creation", "error", err)

		actionID, createErr := app.Models.Action.CreateIfNotExists(
			ctx,
			"sponsor_offer",
			"Sponsor an offer",
		)
		if createErr != nil {
			logger.Error("Audit action creation failed", "error", createErr)
		} else {
			action = &data.Action{ID: actionID}
		}
	}

	entityType, err := app.Models.EntityType.GetByName(ctx, "offer_sponsorship")
	if err != nil || entityType == nil {
		logger.Warn("Audit entity type not found; attempting creation", "error", err)

		entityTypeID, createErr := app.Models.EntityType.CreateIfNotExists(
			ctx,
			"offer_sponsorship",
			"Offer sponsorship",
		)
		if createErr != nil {
			logger.Error("Audit entity type creation failed", "error", createErr)
		} else {
			entityType = &data.EntityType{ID: entityTypeID}
		}
	}

	if action != nil && entityType != nil {
		audit := &data.AuditLog{
			ID:           uuid.New(),
			UserID:       userID,
			ActionID:     action.ID,
			EntityTypeID: entityType.ID,
			EntityID:     offerID.String(),
		}

		if err := app.Models.AuditLog.Insert(ctx, audit); err != nil {
			logger.Warn(
				"Audit logging failed",
				"offer_id", offerID,
				"error", err,
			)

			app.respondWithJSON(w, http.StatusPartialContent, jsonResponse{
				Error:   false,
				Message: "Offer sponsorship created, but audit logging failed",
				Data: map[string]any{
					"offer_id":   offerID,
					"edit_until": editUntil.Format(time.RFC3339),
				},
			})
			return
		}
	}

	logger.Info(
		"Offer sponsored successfully",
		"offer_id", offerID,
	)

	app.respondWithJSON(w, http.StatusCreated, jsonResponse{
		Error:   false,
		Message: "Offer sponsored successfully",
		Data: map[string]any{
			"offer_id":   offerID,
			"edit_until": editUntil.Format(time.RFC3339),
		},
	})
}

// CreateOfferSponsorshipHandler creates an offer sponsorship through the
// canonical data-layer Insert path.
//
// Bid minimums, budget rules, decimal validation, and CPD overlap protection
// remain owned by the data layer.
func (app *Application) CreateOfferSponsorshipHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	logger := app.Logger.
		GetLoggerWithContext(r).
		WithFunctionName("CreateOfferSponsorshipHandler")

	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	if !app.HasPermission(ctx, "create_offer_sponsorship") {
		app.respondWithError(
			w,
			errors.New("forbidden: insufficient permissions"),
			http.StatusForbidden,
		)
		return
	}

	offerID := app.getOfferIDFromContext(ctx)
	merchantID := app.getMerchantIDFromContext(ctx)
	userID := app.getUserIDFromContext(ctx)

	if offerID == nil || merchantID == nil || userID == nil {
		app.respondWithError(
			w,
			errors.New("missing required context identifiers"),
			http.StatusBadRequest,
		)
		return
	}

	var input struct {
		SponsorshipBidTypeID uuid.UUID `json:"sponsorship_bid_type_id"`
		BidAmount            string    `json:"bid_amount"`
		MaxBudget            *string   `json:"max_budget,omitempty"`
		StartDate            time.Time `json:"start_date"`
		EndDate              time.Time `json:"end_date"`
	}

	if err := app.readJSON(w, r, &input); err != nil {
		app.respondWithError(
			w,
			fmt.Errorf("invalid JSON input: %w", err),
			http.StatusBadRequest,
		)
		return
	}

	if input.SponsorshipBidTypeID == uuid.Nil {
		app.respondWithError(
			w,
			errors.New("sponsorship_bid_type_id is required"),
			http.StatusBadRequest,
		)
		return
	}

	if input.StartDate.IsZero() || input.EndDate.IsZero() {
		app.respondWithError(
			w,
			errors.New("start_date and end_date are required"),
			http.StatusBadRequest,
		)
		return
	}

	if !input.EndDate.After(input.StartDate) {
		app.respondWithError(
			w,
			errors.New("end_date must be after start_date"),
			http.StatusBadRequest,
		)
		return
	}

	sponsorship := &data.OfferSponsorship{
		OfferID:              *offerID,
		MerchantID:           *merchantID,
		SponsorshipBidTypeID: input.SponsorshipBidTypeID,
		BidAmount:            input.BidAmount,
		MaxBudget:            input.MaxBudget,
		StartDate:            input.StartDate,
		EndDate:              input.EndDate,
		BudgetSpent:          "0.0000",
		ImpressionsServed:    0,
		ClicksServed:         0,
	}

	if err := app.Models.OfferSponsorship.Insert(ctx, sponsorship); err != nil {
		logger.Error(
			"Failed to insert offer sponsorship",
			"error", err,
		)

		app.respondWithError(
			w,
			fmt.Errorf("failed to create offer sponsorship: %w", err),
			http.StatusInternalServerError,
		)
		return
	}

	action, err := app.Models.Action.GetByName(
		ctx,
		"create_offer_sponsorship",
	)
	if err != nil || action == nil {
		logger.Warn(
			"Audit action not found; attempting creation",
			"error", err,
		)

		actionID, createErr := app.Models.Action.CreateIfNotExists(
			ctx,
			"create_offer_sponsorship",
			"Create an offer sponsorship",
		)
		if createErr != nil {
			logger.Error(
				"Audit action creation failed",
				"error", createErr,
			)
		} else {
			action = &data.Action{ID: actionID}
		}
	}

	entityType, err := app.Models.EntityType.GetByName(
		ctx,
		"offer_sponsorship",
	)
	if err != nil || entityType == nil {
		logger.Warn(
			"Audit entity type not found; attempting creation",
			"error", err,
		)

		entityTypeID, createErr := app.Models.EntityType.CreateIfNotExists(
			ctx,
			"offer_sponsorship",
			"Offer sponsorship",
		)
		if createErr != nil {
			logger.Error(
				"Audit entity type creation failed",
				"error", createErr,
			)
		} else {
			entityType = &data.EntityType{ID: entityTypeID}
		}
	}

	if action != nil && entityType != nil {
		audit := &data.AuditLog{
			ID:           uuid.New(),
			UserID:       userID,
			ActionID:     action.ID,
			EntityTypeID: entityType.ID,
			EntityID:     sponsorship.ID.String(),
		}

		if err := app.Models.AuditLog.Insert(ctx, audit); err != nil {
			logger.Warn(
				"Audit logging failed",
				"offer_sponsorship_id", sponsorship.ID,
				"error", err,
			)

			app.respondWithJSON(w, http.StatusPartialContent, jsonResponse{
				Error:   false,
				Message: "Offer sponsorship created, but audit logging failed",
				Data:    sponsorship.ID,
			})
			return
		}
	}

	logger.Info(
		"Offer sponsorship created",
		"offer_sponsorship_id", sponsorship.ID,
	)

	app.respondWithJSON(w, http.StatusCreated, jsonResponse{
		Error:   false,
		Message: "Offer sponsorship created successfully",
		Data:    sponsorship.ID,
	})
}

// GetOfferSponsorshipByIDHandler handles retrieving a offer sponsorship by its ID.
// It enforces permission checks, extracts sponsorship ID from context,
// retrieves the sponsorship from the database, performs audit logging,
// and responds with the sponsorship details.
func (app *Application) GetOfferSponsorshipByIDHandler(w http.ResponseWriter, r *http.Request) {
	logger := app.Logger.GetLoggerWithContext(r).WithFunctionName("GetOfferSponsorshipByIDHandler")
	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	// Permission enforcement
	if !app.HasPermission(ctx, "read_offer_sponsorship") {
		app.respondWithError(w, errors.New("forbidden: insufficient permissions"), http.StatusForbidden)
		return
	}

	// Extract Offer Sponsorship ID from context
	offerSponsorshipID := app.getOfferSponsorshipIDFromContext(ctx)
	if offerSponsorshipID == nil {
		app.respondWithError(w, errors.New("missing ctxOfferSponsorshipID in context"), http.StatusBadRequest)
		return
	}

	// Retrieve the offer sponsorship
	offerSponsorship, err := app.Models.OfferSponsorship.GetByID(ctx, *offerSponsorshipID)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			app.respondWithError(w, fmt.Errorf("offer sponsorship not found"), http.StatusNotFound)
		} else {
			logger.Error("Failed to retrieve offer sponsorship", "error", err)
			app.respondWithError(w, fmt.Errorf("failed to retrieve offer sponsorship: %w", err), http.StatusInternalServerError)
		}
		return
	}

	// Resolve Audit Action
	action, err := app.Models.Action.GetByName(ctx, "read_offer_sponsorship")
	if err != nil || action == nil {
		logger.Warn("Audit action 'read_offer_sponsorship' not found, attempting to create...", "error", err)
		actionID, createErr := app.Models.Action.CreateIfNotExists(ctx, "read_offer_sponsorship", "Retrieve a offer sponsorship by ID")
		if createErr != nil {
			logger.Error("Failed to create missing audit action", "error", createErr)
			// Allow main operation to proceed
		} else {
			action = &data.Action{ID: actionID}
		}
	}

	// Resolve Audit Entity Type
	entityType, err := app.Models.EntityType.GetByName(ctx, "offer_sponsorship")
	if err != nil || entityType == nil {
		logger.Warn("Audit entity type 'offer_sponsorship' not found, attempting to create...", "error", err)
		entityTypeID, createErr := app.Models.EntityType.CreateIfNotExists(ctx, "offer_sponsorship", "Sponsorship details for a offer")
		if createErr != nil {
			logger.Error("Failed to create missing entity type", "error", createErr)
			// Allow main operation to proceed
		} else {
			entityType = &data.EntityType{ID: entityTypeID}
		}
	}

	// Insert Audit Log (fail gracefully if fails)
	userID := app.getUserIDFromContext(ctx)
	if userID != nil && action != nil && entityType != nil {
		audit := data.AuditLog{
			ID:           uuid.New(),
			UserID:       userID,
			ActionID:     action.ID,
			EntityTypeID: entityType.ID,
			EntityID:     offerSponsorship.ID.String(),
		}
		if err := app.Models.AuditLog.Insert(ctx, &audit); err != nil {
			// Audit logging failed (partial success)
			logger.Warn("Audit logging failed", "ctxOfferSponsorshipID", offerSponsorship.ID, "error", err)
			app.respondWithJSON(w, http.StatusPartialContent, jsonResponse{
				Error:   false,
				Message: "Offer sponsorship retrieved, but audit logging failed",
				Data:    offerSponsorship,
			})
			return
		}
	}

	// Respond success
	logger.Info("Successfully retrieved offer sponsorship", "ctxOfferSponsorshipID", offerSponsorship.ID)
	app.respondWithJSON(w, http.StatusOK, jsonResponse{
		Error:   false,
		Message: "Offer sponsorship retrieved successfully",
		Data:    offerSponsorship,
	})
}

// ListOfferSponsorshipsHandler handles the retrieval of all offer sponsorships.
// It enforces permission checks, extracts trusted identifiers from context,
// queries the database, performs audit logging, and responds with the retrieved sponsorships.
func (app *Application) ListOfferSponsorshipsHandler(w http.ResponseWriter, r *http.Request) {
	logger := app.Logger.GetLoggerWithContext(r).WithFunctionName("ListOfferSponsorshipsHandler")
	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	// Permission enforcement
	if !app.HasPermission(ctx, "read_offer_sponsorships") {
		app.respondWithError(w, errors.New("forbidden: insufficient permissions"), http.StatusForbidden)
		return
	}

	// --- Parse pagination query parameters ---
	limit, offset, err := app.parseLimitOffset(r)
	if err != nil {
		app.respondWithError(w, fmt.Errorf("invalid pagination parameters: %w", err), http.StatusBadRequest)
		return
	}

	// --- Retrieve offer sponsorships from DB ---
	sponsorships, err := app.Models.OfferSponsorship.List(ctx, limit, offset)
	if err != nil {
		logger.Error("Failed to retrieve offer sponsorships", "limit", limit, "offset", offset, "error", err)
		app.respondWithError(w, fmt.Errorf("failed to retrieve offer sponsorships: %w", err), http.StatusInternalServerError)
		return
	}

	// Resolve Audit Action (create if missing)
	action, err := app.Models.Action.GetByName(ctx, "read_offer_sponsorships")
	if err != nil || action == nil {
		logger.Warn("Audit action 'read_offer_sponsorships' not found, attempting to create...", "error", err)
		actionID, createErr := app.Models.Action.CreateIfNotExists(ctx, "read_offer_sponsorships", "Retrieve all offer sponsorships for a specific offer")
		if createErr != nil {
			logger.Error("Failed to create missing audit action", "error", createErr)
			// Allow main operation to succeed
		} else {
			action = &data.Action{ID: actionID}
		}
	}

	// Resolve Audit Entity Type (should exist already)
	entityType, err := app.Models.EntityType.GetByName(ctx, "offer_sponsorship")
	if err != nil || entityType == nil {
		logger.Warn("Audit entity type 'offer_sponsorship' not found, attempting to create...", "error", err)
		entityTypeID, createErr := app.Models.EntityType.CreateIfNotExists(ctx, "offer_sponsorship", "Sponsorship details for a offer")
		if createErr != nil {
			logger.Error("Failed to create missing entity type", "error", createErr)
			// Allow main operation to succeed
		} else {
			entityType = &data.EntityType{ID: entityTypeID}
		}
	}

	// Insert Audit Log (fail gracefully if audit logging fails)
	userID := app.getUserIDFromContext(ctx)
	if userID != nil && action != nil && entityType != nil {
		audit := data.AuditLog{
			ID:           uuid.New(),
			UserID:       userID,
			ActionID:     action.ID,
			EntityTypeID: entityType.ID,
			EntityID:     "N/A",
		}
		if err := app.Models.AuditLog.Insert(ctx, &audit); err != nil {
			// Audit logging failed (partial success)
			logger.Warn("Audit logging failed", "error", err)
			app.respondWithJSON(w, http.StatusPartialContent, jsonResponse{
				Error:   false,
				Message: "Offer sponsorships retrieved, but audit logging failed",
				Data: map[string]any{
					"sponsorships": sponsorships,
					"limit":        limit,
					"offset":       offset,
					"count":        len(sponsorships),
				},
			})
			return
		}
	}

	// --- Success response ---
	logger.Info("Offer sponsorships listed", "count", len(sponsorships), "limit", limit, "offset", offset)
	app.respondWithJSON(w, http.StatusOK, jsonResponse{
		Error:   false,
		Message: "Offer sponsorships retrieved successfully",
		Data: map[string]any{
			"sponsorships": sponsorships,
			"limit":        limit,
			"offset":       offset,
			"count":        len(sponsorships),
		},
	})
}

// UpdateOfferSponsorshipHandler partially updates an existing offer
// sponsorship.
//
// The handler first loads the canonical persisted record and merges supplied
// fields because OfferSponsorshipModel.Update validates the complete resulting
// sponsorship rather than a sparse patch structure.
func (app *Application) UpdateOfferSponsorshipHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	logger := app.Logger.
		GetLoggerWithContext(r).
		WithFunctionName("UpdateOfferSponsorshipHandler")

	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	if !app.HasPermission(ctx, "update_offer_sponsorship") {
		app.respondWithError(
			w,
			errors.New("forbidden: insufficient permissions"),
			http.StatusForbidden,
		)
		return
	}

	sponsorshipID := app.getOfferSponsorshipIDFromContext(ctx)
	if sponsorshipID == nil || *sponsorshipID == uuid.Nil {
		app.respondWithError(
			w,
			errors.New("offer sponsorship ID not found in context"),
			http.StatusBadRequest,
		)
		return
	}

	userID := app.getUserIDFromContext(ctx)
	if userID == nil || *userID == uuid.Nil {
		app.respondWithError(
			w,
			errors.New("user ID not found in context"),
			http.StatusUnauthorized,
		)
		return
	}

	var input struct {
		SponsorshipBidTypeID *uuid.UUID `json:"sponsorship_bid_type_id,omitempty"`
		BidAmount            *string    `json:"bid_amount,omitempty"`
		MaxBudget            *string    `json:"max_budget,omitempty"`
		StartDate            *time.Time `json:"start_date,omitempty"`
		EndDate              *time.Time `json:"end_date,omitempty"`
	}

	if err := app.readJSON(w, r, &input); err != nil {
		app.respondWithError(
			w,
			fmt.Errorf("invalid JSON input: %w", err),
			http.StatusBadRequest,
		)
		return
	}

	existing, err := app.Models.OfferSponsorship.GetByID(
		ctx,
		*sponsorshipID,
	)
	if err != nil {
		logger.Error(
			"Failed to retrieve offer sponsorship for update",
			"offer_sponsorship_id", *sponsorshipID,
			"error", err,
		)

		app.respondWithError(
			w,
			fmt.Errorf("failed to retrieve offer sponsorship: %w", err),
			http.StatusInternalServerError,
		)
		return
	}

	if existing == nil {
		app.respondWithError(
			w,
			errors.New("offer sponsorship not found"),
			http.StatusNotFound,
		)
		return
	}

	if input.SponsorshipBidTypeID != nil {
		if *input.SponsorshipBidTypeID == uuid.Nil {
			app.respondWithError(
				w,
				errors.New("sponsorship_bid_type_id must not be empty"),
				http.StatusBadRequest,
			)
			return
		}

		existing.SponsorshipBidTypeID = *input.SponsorshipBidTypeID
	}

	if input.BidAmount != nil {
		existing.BidAmount = *input.BidAmount
	}

	if input.MaxBudget != nil {
		existing.MaxBudget = input.MaxBudget
	}

	if input.StartDate != nil {
		existing.StartDate = *input.StartDate
	}

	if input.EndDate != nil {
		existing.EndDate = *input.EndDate
	}

	if err := app.Models.OfferSponsorship.Update(ctx, existing); err != nil {
		logger.Error(
			"Failed to update offer sponsorship",
			"offer_sponsorship_id", *sponsorshipID,
			"error", err,
		)

		app.respondWithError(
			w,
			fmt.Errorf("failed to update offer sponsorship: %w", err),
			http.StatusInternalServerError,
		)
		return
	}

	action, err := app.Models.Action.GetByName(
		ctx,
		"update_offer_sponsorship",
	)
	if err != nil || action == nil {
		logger.Warn(
			"Audit action not found; attempting creation",
			"error", err,
		)

		actionID, createErr := app.Models.Action.CreateIfNotExists(
			ctx,
			"update_offer_sponsorship",
			"Update an offer sponsorship",
		)
		if createErr != nil {
			logger.Error(
				"Audit action creation failed",
				"error", createErr,
			)
		} else {
			action = &data.Action{ID: actionID}
		}
	}

	entityType, err := app.Models.EntityType.GetByName(
		ctx,
		"offer_sponsorship",
	)
	if err != nil || entityType == nil {
		logger.Warn(
			"Audit entity type not found; attempting creation",
			"error", err,
		)

		entityTypeID, createErr := app.Models.EntityType.CreateIfNotExists(
			ctx,
			"offer_sponsorship",
			"Offer sponsorship",
		)
		if createErr != nil {
			logger.Error(
				"Audit entity type creation failed",
				"error", createErr,
			)
		} else {
			entityType = &data.EntityType{ID: entityTypeID}
		}
	}

	if action != nil && entityType != nil {
		audit := &data.AuditLog{
			ID:           uuid.New(),
			UserID:       userID,
			ActionID:     action.ID,
			EntityTypeID: entityType.ID,
			EntityID:     sponsorshipID.String(),
		}

		if err := app.Models.AuditLog.Insert(ctx, audit); err != nil {
			logger.Warn(
				"Audit logging failed",
				"offer_sponsorship_id", *sponsorshipID,
				"error", err,
			)

			app.respondWithJSON(w, http.StatusPartialContent, jsonResponse{
				Error:   false,
				Message: "Offer sponsorship updated, but audit logging failed",
				Data:    sponsorshipID,
			})
			return
		}
	}

	logger.Info(
		"Offer sponsorship updated successfully",
		"offer_sponsorship_id", *sponsorshipID,
	)

	app.respondWithJSON(w, http.StatusOK, jsonResponse{
		Error:   false,
		Message: "Offer sponsorship updated successfully",
		Data:    sponsorshipID,
	})
}

// SoftDeleteOfferSponsorshipHandler handles the soft deletion of a offer sponsorship.
// It enforces permission checks, extracts trusted identifiers from context,
// marks the sponsorship as deleted in the database, dynamically resolves audit action/entity,
// and performs structured audit logging with fail-safe behavior.
func (app *Application) SoftDeleteOfferSponsorshipHandler(w http.ResponseWriter, r *http.Request) {
	// Initialize logger with function context for structured logging
	logger := app.Logger.GetLoggerWithContext(r).WithFunctionName("SoftDeleteOfferSponsorshipHandler")

	// Set a timeout for the request context to ensure timely execution
	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	// Permission enforcement: Ensure the user has the required permission
	if !app.HasPermission(ctx, "soft_delete_offer_sponsorship") {
		app.respondWithError(w, errors.New("forbidden: insufficient permissions"), http.StatusForbidden)
		return
	}

	// Extract Offer Sponsorship ID from context (injected via middleware)
	offerSponsorshipID := app.getOfferSponsorshipIDFromContext(ctx)
	if offerSponsorshipID == nil {
		app.respondWithError(w, errors.New("missing ctxOfferSponsorshipID in context"), http.StatusBadRequest)
		return
	}

	// Perform soft delete operation in the database
	if err := app.Models.OfferSponsorship.SoftDelete(ctx, *offerSponsorshipID); err != nil {
		logger.Error("Failed to soft delete offer sponsorship", "ctxOfferSponsorshipID", offerSponsorshipID, "error", err)
		app.respondWithError(w, fmt.Errorf("failed to soft delete offer sponsorship: %w", err), http.StatusInternalServerError)
		return
	}

	// Resolve Audit Action
	action, err := app.Models.Action.GetByName(ctx, "soft_delete_offer_sponsorship")
	if err != nil || action == nil {
		logger.Warn("Audit action 'soft_delete_offer_sponsorship' not found, attempting to create...", "error", err)
		actionID, createErr := app.Models.Action.CreateIfNotExists(ctx, "soft_delete_offer_sponsorship", "Soft delete a offer sponsorship")
		if createErr != nil {
			logger.Error("Failed to create missing audit action", "error", createErr)
			// Allow main operation to continue
		} else {
			action = &data.Action{ID: actionID}
		}
	}

	// Resolve Audit Entity Type
	entityType, err := app.Models.EntityType.GetByName(ctx, "offer_sponsorship")
	if err != nil || entityType == nil {
		logger.Warn("Audit entity type 'offer_sponsorship' not found, attempting to create...", "error", err)
		entityTypeID, createErr := app.Models.EntityType.CreateIfNotExists(ctx, "offer_sponsorship", "Offer sponsorship entity")
		if createErr != nil {
			logger.Error("Failed to create missing entity type", "error", createErr)
			// Allow main operation to continue
		} else {
			entityType = &data.EntityType{ID: entityTypeID}
		}
	}

	// Insert Audit Log (fail gracefully if fails)
	userID := app.getUserIDFromContext(ctx)
	if userID != nil && action != nil && entityType != nil {
		audit := data.AuditLog{
			ID:           uuid.New(),
			UserID:       userID,
			ActionID:     action.ID,
			EntityTypeID: entityType.ID,
			EntityID:     offerSponsorshipID.String(),
		}
		if err := app.Models.AuditLog.Insert(ctx, &audit); err != nil {
			// Audit logging failed (partial success)
			logger.Warn("Audit logging failed", "ctxOfferSponsorshipID", offerSponsorshipID, "error", err)
			app.respondWithJSON(w, http.StatusPartialContent, jsonResponse{
				Error:   false,
				Message: "Offer sponsorship soft deleted, but audit logging failed",
				Data:    offerSponsorshipID,
			})
			return
		}
	}

	// Respond success
	logger.Info("Offer sponsorship soft deleted successfully", "ctxOfferSponsorshipID", offerSponsorshipID)
	app.respondWithJSON(w, http.StatusOK, jsonResponse{
		Error:   false,
		Message: "Offer sponsorship soft deleted successfully",
		Data:    offerSponsorshipID,
	})
}
