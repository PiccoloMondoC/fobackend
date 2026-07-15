// Package main provides HTTP handlers for the SagrentiDeals API.
//
// sdworkspace/sdbackend/internal/server/cmd/api/offers.go
//
// GTM:
//
//	Layer: 3.1 API / Catalog / Offer Domain
//	Release Class: SPINE
//	Reason:
//	  Offer handlers expose the canonical consumer-facing Offer record over
//	  HTTP. This file owns merchant-side offer creation and update, public
//	  live-offer discovery, privileged internal offer reads, soft-delete
//	  lifecycle, and the administrative hard-delete maintenance path.
//
//	  Offers are release-critical because they are the public expression of
//	  Sagrenti commerce. An Offer is either a Deal representing Present
//	  Commerce or a Trend representing Future Commerce.
//
// SPINE Rule:
//
//	Keep compiling.
//	Keep production-ready.
//	Preserve Offer as the canonical Deal-or-Trend public commerce record.
//	Match internal/data/offers.go structs and method signatures exactly.
//	Preserve fixed-point money and decimal values as strings.
//	Preserve merchant identity extraction from trusted request context.
//	Preserve public visibility through OfferModel live-read methods.
//	Preserve permission-gated internal and administrative reads.
//	Preserve typed not-found handling through data.ErrOfferNotFound.
//	Preserve database-owned lifecycle timestamps.
//	Preserve best-effort audit logging.
//	Do not match persistence errors by error-message text.
//	Do not recreate model methods that do not exist.
//	Do not expose DEFERRED curation, rating, voting, flagging, popularity,
//	blacklisting, or rejection workflows through this SPINE file.
//	Block deployment if this file breaks build, offer persistence,
//	public offer discovery, offer lifecycle behavior, or catalog integrity.
package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/PiccoloMondoC/sdworkspace/sdbackend/internal/data"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

const (
	offerEntityTypeName        = "offer"
	offerEntityTypeDescription = "Offer entity"

	createOfferAction     = "create_offer"
	readOfferAction       = "read_offer"
	readAllOffersAction   = "read_all_offers"
	updateOfferAction     = "update_offer"
	softDeleteOfferAction = "soft_delete_offer"
	purgeOfferAction      = "purge_offer"

	defaultOfferLimit = 20
	maxOfferLimit     = 200
)

type createOfferInput struct {
	OfferKey        string     `json:"offer_key"`
	Type            string     `json:"type"`
	Title           string     `json:"title"`
	Description     *string    `json:"description,omitempty"`
	ImageURL        *string    `json:"image_url,omitempty"`
	AffiliateURL    string     `json:"affiliate_url"`
	Price           *string    `json:"price,omitempty"`
	StartingPrice   *string    `json:"starting_price,omitempty"`
	ListPrice       *string    `json:"list_price,omitempty"`
	Currency        string     `json:"currency"`
	DiscountPercent *string    `json:"discount_percent,omitempty"`
	CouponCode      *string    `json:"coupon_code,omitempty"`
	ProductID       *uuid.UUID `json:"product_id,omitempty"`
	CategoryID      uuid.UUID  `json:"category_id"`
	ExpiresAt       *time.Time `json:"expires_at,omitempty"`
}

type updateOfferInput struct {
	OfferKey        *string    `json:"offer_key,omitempty"`
	Type            *string    `json:"type,omitempty"`
	Title           *string    `json:"title,omitempty"`
	Description     *string    `json:"description,omitempty"`
	ImageURL        *string    `json:"image_url,omitempty"`
	AffiliateURL    *string    `json:"affiliate_url,omitempty"`
	Price           *string    `json:"price,omitempty"`
	StartingPrice   *string    `json:"starting_price,omitempty"`
	ListPrice       *string    `json:"list_price,omitempty"`
	Currency        *string    `json:"currency,omitempty"`
	DiscountPercent *string    `json:"discount_percent,omitempty"`
	CouponCode      *string    `json:"coupon_code,omitempty"`
	ProductID       *uuid.UUID `json:"product_id,omitempty"`
	CategoryID      *uuid.UUID `json:"category_id,omitempty"`
	ExpiresAt       *time.Time `json:"expires_at,omitempty"`
}

// CreateOfferHandler creates a merchant-owned Offer.
//
// Merchant identity is extracted from trusted middleware context and is never
// accepted from the request body.
//
// Merchant-created Offers begin unapproved. OfferModel.Insert resolves the
// canonical pending-review status and preserves database ownership of
// publication, activity, and lifecycle timestamps.
func (app *Application) CreateOfferHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	logger := app.Logger.
		GetLoggerWithContext(r).
		WithFunctionName("CreateOfferHandler")

	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	if !app.HasPermission(ctx, createOfferAction) {
		app.respondWithError(
			w,
			errors.New("forbidden: insufficient permissions"),
			http.StatusForbidden,
		)
		return
	}

	merchantID := app.getMerchantIDFromContext(ctx)
	if merchantID == nil || *merchantID == uuid.Nil {
		app.respondWithError(
			w,
			errors.New("merchant ID not found in context"),
			http.StatusBadRequest,
		)
		return
	}

	var input createOfferInput
	if err := app.readJSON(w, r, &input); err != nil {
		app.respondWithError(
			w,
			fmt.Errorf("invalid JSON input: %w", err),
			http.StatusBadRequest,
		)
		return
	}

	input.OfferKey = strings.ToLower(strings.TrimSpace(input.OfferKey))
	input.Type = strings.ToLower(strings.TrimSpace(input.Type))
	input.Title = strings.TrimSpace(input.Title)
	input.AffiliateURL = strings.TrimSpace(input.AffiliateURL)
	input.Currency = strings.ToUpper(strings.TrimSpace(input.Currency))

	if input.OfferKey == "" {
		app.respondWithError(
			w,
			errors.New("offer_key is required"),
			http.StatusBadRequest,
		)
		return
	}

	if err := validateOfferInputType(input.Type); err != nil {
		app.respondWithError(w, err, http.StatusBadRequest)
		return
	}

	if input.Title == "" {
		app.respondWithError(
			w,
			errors.New("title is required"),
			http.StatusBadRequest,
		)
		return
	}

	if input.AffiliateURL == "" {
		app.respondWithError(
			w,
			errors.New("affiliate_url is required"),
			http.StatusBadRequest,
		)
		return
	}

	if len(input.Currency) != 3 {
		app.respondWithError(
			w,
			errors.New("currency must be a 3-letter ISO currency code"),
			http.StatusBadRequest,
		)
		return
	}

	if input.CategoryID == uuid.Nil {
		app.respondWithError(
			w,
			errors.New("category_id is required"),
			http.StatusBadRequest,
		)
		return
	}

	offer := &data.Offer{
		ID:                  uuid.New(),
		OfferKey:            input.OfferKey,
		Type:                input.Type,
		Title:               input.Title,
		Description:         input.Description,
		ImageURL:            input.ImageURL,
		AffiliateURL:        input.AffiliateURL,
		Price:               input.Price,
		StartingPrice:       input.StartingPrice,
		ListPrice:           input.ListPrice,
		Currency:            input.Currency,
		DiscountPercent:     input.DiscountPercent,
		CouponCode:          input.CouponCode,
		ProductID:           input.ProductID,
		MerchantID:          *merchantID,
		CategoryID:          input.CategoryID,
		AvgRating:           "0",
		IsEditorialApproved: false,
		ExpiresAt:           input.ExpiresAt,
	}

	if err := app.Models.Offer.Insert(ctx, offer); err != nil {
		logger.Error(
			"Create offer failed",
			"merchant_id", *merchantID,
			"offer_type", offer.Type,
			"error", err,
		)
		app.respondWithError(
			w,
			fmt.Errorf("failed to create offer: %w", err),
			http.StatusInternalServerError,
		)
		return
	}

	app.insertOfferAudit(
		ctx,
		logger,
		app.getUserIDFromContext(ctx),
		createOfferAction,
		"Create a new offer",
		offer.ID.String(),
	)

	logger.Info(
		"Offer created",
		"offer_id", offer.ID,
		"merchant_id", offer.MerchantID,
		"offer_type", offer.Type,
	)

	app.respondWithJSON(w, http.StatusCreated, jsonResponse{
		Error:   false,
		Message: "Offer created successfully",
		Data:    offer,
	})
}

// GetOfferByIDHandler retrieves one publicly visible Offer by canonical ID.
//
// Public offer reads must use GetLiveOfferByID so approval, publication,
// active-state, expiration, and soft-delete visibility remain enforced by the
// canonical data-layer publication contract.
func (app *Application) GetOfferByIDHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	logger := app.Logger.
		GetLoggerWithContext(r).
		WithFunctionName("GetOfferByIDHandler")

	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	rawOfferID := strings.TrimSpace(chi.URLParam(r, "id"))
	if rawOfferID == "" {
		app.respondWithError(
			w,
			errors.New("offer ID is required"),
			http.StatusBadRequest,
		)
		return
	}

	offerID, err := uuid.Parse(rawOfferID)
	if err != nil {
		app.respondWithError(
			w,
			fmt.Errorf("invalid offer ID: %w", err),
			http.StatusBadRequest,
		)
		return
	}

	offer, err := app.Models.Offer.GetLiveOfferByID(ctx, offerID)
	if err != nil {
		if errors.Is(err, data.ErrOfferNotFound) {
			app.respondWithError(
				w,
				errors.New("offer not found"),
				http.StatusNotFound,
			)
			return
		}

		logger.Error(
			"Retrieve live offer by ID failed",
			"offer_id", offerID,
			"error", err,
		)
		app.respondWithError(
			w,
			fmt.Errorf("failed to retrieve offer: %w", err),
			http.StatusInternalServerError,
		)
		return
	}

	app.insertOfferAudit(
		ctx,
		logger,
		app.getUserIDFromContext(ctx),
		readOfferAction,
		"Read a public offer by ID",
		offer.ID.String(),
	)

	logger.Info(
		"Live offer retrieved",
		"offer_id", offer.ID,
		"offer_key", offer.OfferKey,
	)

	app.respondWithJSON(w, http.StatusOK, jsonResponse{
		Error:   false,
		Message: "Offer retrieved successfully",
		Data:    offer,
	})
}

// GetOfferByKeyHandler retrieves one publicly visible Offer by readable key.
//
// OfferModel.GetLiveOfferByKey owns canonical key normalization and public
// visibility enforcement.
func (app *Application) GetOfferByKeyHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	logger := app.Logger.
		GetLoggerWithContext(r).
		WithFunctionName("GetOfferByKeyHandler")

	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	offerKey := strings.TrimSpace(chi.URLParam(r, "offer_key"))
	if offerKey == "" {
		app.respondWithError(
			w,
			errors.New("offer key is required"),
			http.StatusBadRequest,
		)
		return
	}

	offer, err := app.Models.Offer.GetLiveOfferByKey(ctx, offerKey)
	if err != nil {
		if errors.Is(err, data.ErrOfferNotFound) {
			app.respondWithError(
				w,
				errors.New("offer not found"),
				http.StatusNotFound,
			)
			return
		}

		logger.Error(
			"Retrieve live offer by key failed",
			"offer_key", offerKey,
			"error", err,
		)
		app.respondWithError(
			w,
			fmt.Errorf("failed to retrieve offer: %w", err),
			http.StatusInternalServerError,
		)
		return
	}

	app.insertOfferAudit(
		ctx,
		logger,
		app.getUserIDFromContext(ctx),
		readOfferAction,
		"Read a public offer by key",
		offer.ID.String(),
	)

	logger.Info(
		"Live offer retrieved by key",
		"offer_id", offer.ID,
		"offer_key", offer.OfferKey,
	)

	app.respondWithJSON(w, http.StatusOK, jsonResponse{
		Error:   false,
		Message: "Offer retrieved successfully",
		Data:    offer,
	})
}

// GetLiveOffersHandler retrieves a bounded page of publicly visible Offers.
//
// This endpoint is intentionally public. Public visibility remains enforced by
// OfferModel.GetLiveOffers rather than by weakening an internal read contract.
func (app *Application) GetLiveOffersHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	logger := app.Logger.
		GetLoggerWithContext(r).
		WithFunctionName("GetLiveOffersHandler")

	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	limit, offset, err := parseOfferPagination(r)
	if err != nil {
		app.respondWithError(w, err, http.StatusBadRequest)
		return
	}

	offers, err := app.Models.Offer.GetLiveOffers(ctx, limit, offset)
	if err != nil {
		logger.Error(
			"Retrieve live offers failed",
			"limit", limit,
			"offset", offset,
			"error", err,
		)
		app.respondWithError(
			w,
			fmt.Errorf("failed to retrieve live offers: %w", err),
			http.StatusInternalServerError,
		)
		return
	}

	logger.Info(
		"Live offers retrieved",
		"count", len(offers),
		"limit", limit,
		"offset", offset,
	)

	app.respondWithJSON(w, http.StatusOK, jsonResponse{
		Error:   false,
		Message: "Live offers retrieved successfully",
		Data: struct {
			Offers []*data.Offer `json:"offers"`
			Count  int           `json:"count"`
			Limit  int           `json:"limit"`
			Offset int           `json:"offset"`
		}{
			Offers: offers,
			Count:  len(offers),
			Limit:  limit,
			Offset: offset,
		},
	})
}

// GetAllOffersHandler retrieves a bounded internal or administrative Offer
// listing using the exact filter whitelist supported by OfferModel.GetAll.
//
// Unlike public live reads, this handler may return inactive, unpublished, or
// unapproved non-deleted Offers. Access therefore remains permission-gated.
func (app *Application) GetAllOffersHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	logger := app.Logger.
		GetLoggerWithContext(r).
		WithFunctionName("GetAllOffersHandler")

	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	if !app.HasPermission(ctx, readAllOffersAction) {
		app.respondWithError(
			w,
			errors.New("forbidden: insufficient permissions"),
			http.StatusForbidden,
		)
		return
	}

	limit, offset, err := parseOfferPagination(r)
	if err != nil {
		app.respondWithError(w, err, http.StatusBadRequest)
		return
	}

	filters, err := parseOfferFilters(r)
	if err != nil {
		app.respondWithError(w, err, http.StatusBadRequest)
		return
	}

	offers, err := app.Models.Offer.GetAll(
		ctx,
		filters,
		limit,
		offset,
	)
	if err != nil {
		logger.Error(
			"Retrieve internal offers failed",
			"limit", limit,
			"offset", offset,
			"error", err,
		)
		app.respondWithError(
			w,
			fmt.Errorf("failed to retrieve offers: %w", err),
			http.StatusInternalServerError,
		)
		return
	}

	app.insertOfferAudit(
		ctx,
		logger,
		app.getUserIDFromContext(ctx),
		readAllOffersAction,
		"Read internal offer listing",
		"all_offers",
	)

	logger.Info(
		"Internal offers retrieved",
		"count", len(offers),
		"limit", limit,
		"offset", offset,
	)

	app.respondWithJSON(w, http.StatusOK, jsonResponse{
		Error:   false,
		Message: "Offers retrieved successfully",
		Data: struct {
			Offers []*data.Offer `json:"offers"`
			Count  int           `json:"count"`
			Limit  int           `json:"limit"`
			Offset int           `json:"offset"`
		}{
			Offers: offers,
			Count:  len(offers),
			Limit:  limit,
			Offset: offset,
		},
	})
}

// UpdateOfferHandler updates the mutable canonical fields of an existing Offer.
//
// The Offer ID is extracted from trusted middleware context. This handler does
// not own editorial approval, publication, graduation, rating, voting,
// flagging, or other workflow transitions.
func (app *Application) UpdateOfferHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	logger := app.Logger.
		GetLoggerWithContext(r).
		WithFunctionName("UpdateOfferHandler")

	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	if !app.HasPermission(ctx, updateOfferAction) {
		app.respondWithError(
			w,
			errors.New("forbidden: insufficient permissions"),
			http.StatusForbidden,
		)
		return
	}

	offerID := app.getOfferIDFromContext(ctx)
	if offerID == nil || *offerID == uuid.Nil {
		app.respondWithError(
			w,
			errors.New("offer ID not found in context"),
			http.StatusBadRequest,
		)
		return
	}

	var input updateOfferInput
	if err := app.readJSON(w, r, &input); err != nil {
		app.respondWithError(
			w,
			fmt.Errorf("invalid JSON input: %w", err),
			http.StatusBadRequest,
		)
		return
	}

	offer, err := app.Models.Offer.GetByID(ctx, *offerID)
	if err != nil {
		if errors.Is(err, data.ErrOfferNotFound) {
			app.respondWithError(
				w,
				errors.New("offer not found"),
				http.StatusNotFound,
			)
			return
		}

		logger.Error(
			"Retrieve offer for update failed",
			"offer_id", *offerID,
			"error", err,
		)
		app.respondWithError(
			w,
			fmt.Errorf("failed to retrieve offer: %w", err),
			http.StatusInternalServerError,
		)
		return
	}

	if input.OfferKey != nil {
		value := strings.ToLower(strings.TrimSpace(*input.OfferKey))
		if value == "" {
			app.respondWithError(
				w,
				errors.New("offer_key cannot be empty"),
				http.StatusBadRequest,
			)
			return
		}
		offer.OfferKey = value
	}

	if input.Type != nil {
		value := strings.ToLower(strings.TrimSpace(*input.Type))
		if err := validateOfferInputType(value); err != nil {
			app.respondWithError(w, err, http.StatusBadRequest)
			return
		}
		offer.Type = value
	}

	if input.Title != nil {
		value := strings.TrimSpace(*input.Title)
		if value == "" {
			app.respondWithError(
				w,
				errors.New("title cannot be empty"),
				http.StatusBadRequest,
			)
			return
		}
		offer.Title = value
	}

	if input.Description != nil {
		offer.Description = input.Description
	}

	if input.ImageURL != nil {
		offer.ImageURL = input.ImageURL
	}

	if input.AffiliateURL != nil {
		value := strings.TrimSpace(*input.AffiliateURL)
		if value == "" {
			app.respondWithError(
				w,
				errors.New("affiliate_url cannot be empty"),
				http.StatusBadRequest,
			)
			return
		}
		offer.AffiliateURL = value
	}

	if input.Price != nil {
		offer.Price = input.Price
	}

	if input.StartingPrice != nil {
		offer.StartingPrice = input.StartingPrice
	}

	if input.ListPrice != nil {
		offer.ListPrice = input.ListPrice
	}

	if input.Currency != nil {
		value := strings.ToUpper(strings.TrimSpace(*input.Currency))
		if len(value) != 3 {
			app.respondWithError(
				w,
				errors.New("currency must be a 3-letter ISO currency code"),
				http.StatusBadRequest,
			)
			return
		}
		offer.Currency = value
	}

	if input.DiscountPercent != nil {
		offer.DiscountPercent = input.DiscountPercent
	}

	if input.CouponCode != nil {
		offer.CouponCode = input.CouponCode
	}

	if input.ProductID != nil {
		offer.ProductID = input.ProductID
	}

	if input.CategoryID != nil {
		if *input.CategoryID == uuid.Nil {
			app.respondWithError(
				w,
				errors.New("category_id cannot be empty"),
				http.StatusBadRequest,
			)
			return
		}
		offer.CategoryID = *input.CategoryID
	}

	if input.ExpiresAt != nil {
		offer.ExpiresAt = input.ExpiresAt
	}

	if err := app.Models.Offer.Update(ctx, offer); err != nil {
		if errors.Is(err, data.ErrOfferNotFound) {
			app.respondWithError(
				w,
				errors.New("offer not found"),
				http.StatusNotFound,
			)
			return
		}

		logger.Error(
			"Update offer failed",
			"offer_id", offer.ID,
			"error", err,
		)
		app.respondWithError(
			w,
			fmt.Errorf("failed to update offer: %w", err),
			http.StatusInternalServerError,
		)
		return
	}

	app.insertOfferAudit(
		ctx,
		logger,
		app.getUserIDFromContext(ctx),
		updateOfferAction,
		"Update an existing offer",
		offer.ID.String(),
	)

	logger.Info(
		"Offer updated",
		"offer_id", offer.ID,
		"offer_type", offer.Type,
	)

	app.respondWithJSON(w, http.StatusOK, jsonResponse{
		Error:   false,
		Message: "Offer updated successfully",
		Data:    offer,
	})
}

// SoftDeleteOfferHandler performs canonical logical removal of an Offer.
//
// OfferModel.SoftDelete owns deleted_at, updated_at, and the inactive-state
// transition. This handler must not write those fields directly.
func (app *Application) SoftDeleteOfferHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	logger := app.Logger.
		GetLoggerWithContext(r).
		WithFunctionName("SoftDeleteOfferHandler")

	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	if !app.HasPermission(ctx, softDeleteOfferAction) {
		app.respondWithError(
			w,
			errors.New("forbidden: insufficient permissions"),
			http.StatusForbidden,
		)
		return
	}

	offerID := app.getOfferIDFromContext(ctx)
	if offerID == nil || *offerID == uuid.Nil {
		app.respondWithError(
			w,
			errors.New("offer ID not found in context"),
			http.StatusBadRequest,
		)
		return
	}

	if err := app.Models.Offer.SoftDelete(ctx, *offerID); err != nil {
		if errors.Is(err, data.ErrOfferNotFound) {
			app.respondWithError(
				w,
				errors.New("offer not found"),
				http.StatusNotFound,
			)
			return
		}

		logger.Error(
			"Soft delete offer failed",
			"offer_id", *offerID,
			"error", err,
		)
		app.respondWithError(
			w,
			fmt.Errorf("failed to soft delete offer: %w", err),
			http.StatusInternalServerError,
		)
		return
	}

	app.insertOfferAudit(
		ctx,
		logger,
		app.getUserIDFromContext(ctx),
		softDeleteOfferAction,
		"Soft delete an offer",
		offerID.String(),
	)

	logger.Info(
		"Offer soft deleted",
		"offer_id", *offerID,
	)

	app.respondWithJSON(w, http.StatusOK, jsonResponse{
		Error:   false,
		Message: "Offer soft deleted successfully",
		Data:    *offerID,
	})
}

// PurgeOfferHandler permanently deletes an Offer.
//
// This is an administrative maintenance path, not ordinary merchant lifecycle
// behavior. Route registration must protect it with the purge_offer permission
// and the applicable platform hard-delete governance.
func (app *Application) PurgeOfferHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	logger := app.Logger.
		GetLoggerWithContext(r).
		WithFunctionName("PurgeOfferHandler")

	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	if !app.HasPermission(ctx, purgeOfferAction) {
		app.respondWithError(
			w,
			errors.New("forbidden: insufficient permissions"),
			http.StatusForbidden,
		)
		return
	}

	offerID := app.getOfferIDFromContext(ctx)
	if offerID == nil || *offerID == uuid.Nil {
		app.respondWithError(
			w,
			errors.New("offer ID not found in context"),
			http.StatusBadRequest,
		)
		return
	}

	if err := app.Models.Offer.Delete(ctx, *offerID); err != nil {
		if errors.Is(err, data.ErrOfferNotFound) {
			app.respondWithError(
				w,
				errors.New("offer not found"),
				http.StatusNotFound,
			)
			return
		}

		logger.Error(
			"Purge offer failed",
			"offer_id", *offerID,
			"error", err,
		)
		app.respondWithError(
			w,
			fmt.Errorf("failed to purge offer: %w", err),
			http.StatusInternalServerError,
		)
		return
	}

	app.insertOfferAudit(
		ctx,
		logger,
		app.getUserIDFromContext(ctx),
		purgeOfferAction,
		"Hard delete an offer",
		offerID.String(),
	)

	logger.Info(
		"Offer purged",
		"offer_id", *offerID,
	)

	app.respondWithJSON(w, http.StatusOK, jsonResponse{
		Error:   false,
		Message: "Offer purged successfully",
		Data:    *offerID,
	})
}

// insertOfferAudit performs best-effort audit insertion.
//
// A completed Offer operation is not rolled back or converted into an HTTP
// partial-content response merely because audit metadata resolution or audit
// persistence is temporarily unavailable.
func (app *Application) insertOfferAudit(
	ctx context.Context,
	logger interface {
		Warn(string, ...any)
	},
	userID *uuid.UUID,
	actionName string,
	actionDescription string,
	entityID string,
) {
	if userID == nil || *userID == uuid.Nil {
		return
	}

	actionID, entityTypeID, err := app.resolveAuditMetadata(
		ctx,
		actionName,
		actionDescription,
		offerEntityTypeName,
		offerEntityTypeDescription,
	)
	if err != nil {
		logger.Warn(
			"Offer audit metadata resolution failed",
			"action", actionName,
			"entity_id", entityID,
			"error", err,
		)
		return
	}

	audit := &data.AuditLog{
		ID:           uuid.New(),
		UserID:       userID,
		ActionID:     actionID,
		EntityTypeID: entityTypeID,
		EntityID:     entityID,
	}

	if err := app.Models.AuditLog.Insert(ctx, audit); err != nil {
		logger.Warn(
			"Offer audit insertion failed",
			"action", actionName,
			"entity_id", entityID,
			"error", err,
		)
	}
}

func validateOfferInputType(value string) error {
	switch value {
	case "deal", "trend":
		return nil
	default:
		return errors.New("type must be either deal or trend")
	}
}

func parseOfferPagination(r *http.Request) (int, int, error) {
	query := r.URL.Query()

	limit := defaultOfferLimit
	if rawLimit := strings.TrimSpace(query.Get("limit")); rawLimit != "" {
		value, err := strconv.Atoi(rawLimit)
		if err != nil {
			return 0, 0, fmt.Errorf("invalid limit: %w", err)
		}
		limit = value
	}

	offset := 0
	if rawOffset := strings.TrimSpace(query.Get("offset")); rawOffset != "" {
		value, err := strconv.Atoi(rawOffset)
		if err != nil {
			return 0, 0, fmt.Errorf("invalid offset: %w", err)
		}
		offset = value
	}

	if limit < 1 {
		return 0, 0, errors.New("limit must be greater than 0")
	}

	if limit > maxOfferLimit {
		limit = maxOfferLimit
	}

	if offset < 0 {
		return 0, 0, errors.New("offset cannot be negative")
	}

	return limit, offset, nil
}

func parseOfferFilters(r *http.Request) (map[string]any, error) {
	query := r.URL.Query()
	filters := make(map[string]any)

	uuidFilters := map[string]string{
		"merchant_id": "merchant_id",
		"category_id": "category_id",
		"status_id":   "status_id",
		"product_id":  "product_id",
	}

	for queryName, filterName := range uuidFilters {
		rawValue := strings.TrimSpace(query.Get(queryName))
		if rawValue == "" {
			continue
		}

		id, err := uuid.Parse(rawValue)
		if err != nil {
			return nil, fmt.Errorf("invalid %s: %w", queryName, err)
		}

		filters[filterName] = id
	}

	if rawType := strings.TrimSpace(query.Get("type")); rawType != "" {
		offerType := strings.ToLower(rawType)
		if err := validateOfferInputType(offerType); err != nil {
			return nil, err
		}
		filters["type"] = offerType
	}

	booleanFilters := map[string]string{
		"is_active":             "is_active",
		"is_editorial_approved": "is_editorial_approved",
	}

	for queryName, filterName := range booleanFilters {
		rawValue := strings.TrimSpace(query.Get(queryName))
		if rawValue == "" {
			continue
		}

		value, err := strconv.ParseBool(rawValue)
		if err != nil {
			return nil, fmt.Errorf("invalid %s: %w", queryName, err)
		}

		filters[filterName] = value
	}

	// Price filters remain strings. They are passed to PostgreSQL NUMERIC
	// comparisons without introducing binary floating-point conversion.
	if value := strings.TrimSpace(query.Get("min_price")); value != "" {
		filters["min_price"] = value
	}

	if value := strings.TrimSpace(query.Get("max_price")); value != "" {
		filters["max_price"] = value
	}

	if value := strings.TrimSpace(query.Get("title")); value != "" {
		filters["title"] = value
	}

	return filters, nil
}