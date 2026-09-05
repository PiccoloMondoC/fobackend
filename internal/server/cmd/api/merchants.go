// Package main contains the HTTP API handlers for merchant-domain resources.
//
// sdworkspace/sdbackend/internal/server/cmd/api/merchants.go
//
// GTM:
//
//	Layer: 3 API / Transport
//	Release Class: SPINE
//	Reason:
//	  Merchant identity, merchant classification, merchant-affiliate program
//	  relationships, and platform lookup are release-critical API surfaces.
//	  They expose the merchant/catalog foundation used by offer ownership,
//	  affiliate linkage, platform resolution, and monetization routing.
//
// SPINE Rule:
//
//	Keep compiling.
//	Keep production-ready.
//	Preserve merchant identity, display-name, slug, logo, website, and platform behavior.
//	Preserve merchant type classification.
//	Preserve merchant-affiliate program relationship integrity.
//	Preserve public-safe affiliate program summary reads.
//	Preserve platform lookup behavior.
//	Preserve trusted-context identifier handling.
//	Preserve soft-delete lifecycle semantics through the data layer.
//	Block deployment if this file breaks build, merchant persistence,
//	offer ownership, affiliate-program linkage, platform lookup,
//	authorization, auditability, or merchant/catalog integrity.
package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/PiccoloMondoC/focodebase/fobackend/internal/data"

	"github.com/google/uuid"
)

const (
	defaultMerchantPageSize = 20
	maxMerchantPageSize     = 100
)

func merchantPagination(app *Application, ctx context.Context) (int, int) {
	limit := parseIntOrDefault(
		app.getContextValueAsString(ctx, ctxPaginationLimit),
		defaultMerchantPageSize,
	)
	offset := parseIntOrDefault(
		app.getContextValueAsString(ctx, ctxPaginationOffset),
		0,
	)

	if limit < 1 {
		limit = defaultMerchantPageSize
	}
	if limit > maxMerchantPageSize {
		limit = maxMerchantPageSize
	}
	if offset < 0 {
		offset = 0
	}

	return limit, offset
}

func merchantTypeIDFromContext(
	app *Application,
	ctx context.Context,
) *uuid.UUID {
	raw := app.getContextValueAsString(ctx, ctxMerchantTypeID)
	if raw == "" {
		return nil
	}

	id, err := uuid.Parse(raw)
	if err != nil || id == uuid.Nil {
		return nil
	}

	return &id
}

func paginateMerchants(
	merchants []*data.Merchant,
	limit int,
	offset int,
) []*data.Merchant {
	if offset >= len(merchants) {
		return []*data.Merchant{}
	}

	end := offset + limit
	if end > len(merchants) {
		end = len(merchants)
	}

	return merchants[offset:end]
}

func (app *Application) writeMerchantDomainAudit(
	ctx context.Context,
	userID *uuid.UUID,
	actionName string,
	actionDescription string,
	entityTypeName string,
	entityTypeDescription string,
	entityID string,
) error {
	if userID == nil {
		return errors.New("user ID not found in context")
	}

	action, err := app.Models.Action.GetByName(ctx, actionName)
	if err != nil || action == nil {
		actionID, createErr := app.Models.Action.CreateIfNotExists(
			ctx,
			actionName,
			actionDescription,
		)
		if createErr != nil {
			if err != nil {
				return fmt.Errorf("resolve audit action: %w", err)
			}

			return fmt.Errorf("create audit action: %w", createErr)
		}

		action = &data.Action{
			ID: actionID,
		}
	}

	entityType, err := app.Models.EntityType.GetByName(
		ctx,
		entityTypeName,
	)
	if err != nil || entityType == nil {
		entityTypeID, createErr := app.Models.EntityType.CreateIfNotExists(
			ctx,
			entityTypeName,
			entityTypeDescription,
		)
		if createErr != nil {
			if err != nil {
				return fmt.Errorf("resolve audit entity type: %w", err)
			}

			return fmt.Errorf("create audit entity type: %w", createErr)
		}

		entityType = &data.EntityType{
			ID: entityTypeID,
		}
	}

	if action == nil || action.ID == uuid.Nil {
		return errors.New("audit action could not be resolved")
	}

	if entityType == nil || entityType.ID == uuid.Nil {
		return errors.New("audit entity type could not be resolved")
	}

	auditLog := &data.AuditLog{
		ID:           uuid.New(),
		UserID:       userID,
		ActionID:     action.ID,
		EntityTypeID: entityType.ID,
		EntityID:     entityID,
	}

	if err := app.Models.AuditLog.Insert(ctx, auditLog); err != nil {
		return fmt.Errorf("insert audit log: %w", err)
	}

	return nil
}

func (app *Application) requireMerchantPermission(
	w http.ResponseWriter,
	ctx context.Context,
	permission string,
) *uuid.UUID {
	if !app.HasPermission(ctx, permission) {
		app.respondWithError(
			w,
			errors.New("forbidden: insufficient permissions"),
			http.StatusForbidden,
		)
		return nil
	}

	userID := app.getUserIDFromContext(ctx)
	if userID == nil || *userID == uuid.Nil {
		app.respondWithError(
			w,
			errors.New("user ID not found in context"),
			http.StatusUnauthorized,
		)
		return nil
	}

	return userID
}

func (app *Application) respondWithMerchantAudit(
	w http.ResponseWriter,
	ctx context.Context,
	userID *uuid.UUID,
	actionName string,
	actionDescription string,
	entityTypeName string,
	entityTypeDescription string,
	entityID string,
	successStatus int,
	successMessage string,
	dataValue any,
) {
	if err := app.writeMerchantDomainAudit(
		ctx,
		userID,
		actionName,
		actionDescription,
		entityTypeName,
		entityTypeDescription,
		entityID,
	); err != nil {
		app.respondWithJSON(w, http.StatusPartialContent, jsonResponse{
			Error:   false,
			Message: successMessage + ", but audit logging failed",
			Data:    dataValue,
		})
		return
	}

	app.respondWithJSON(w, successStatus, jsonResponse{
		Error:   false,
		Message: successMessage,
		Data:    dataValue,
	})
}

// CreateMerchantHandler creates a merchant using the canonical merchant
// persistence contract.
func (app *Application) CreateMerchantHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	logger := app.Logger.
		GetLoggerWithContext(r).
		WithFunctionName("CreateMerchantHandler")

	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	userID := app.requireMerchantPermission(
		w,
		ctx,
		"create_merchant",
	)
	if userID == nil {
		return
	}

	var input struct {
		MerchantTypeID uuid.UUID  `json:"merchant_type_id"`
		Name           string     `json:"name"`
		DisplayName    string     `json:"display_name"`
		Slug           string     `json:"slug"`
		LogoURL        *string    `json:"logo_url,omitempty"`
		Website        *string    `json:"website,omitempty"`
		PlatformID     *uuid.UUID `json:"platform_id,omitempty"`
	}

	if err := app.readJSON(w, r, &input); err != nil {
		app.respondWithError(
			w,
			fmt.Errorf("invalid JSON input: %w", err),
			http.StatusBadRequest,
		)
		return
	}

	merchant := &data.Merchant{
		MerchantTypeID: input.MerchantTypeID,
		Name:           input.Name,
		DisplayName:    input.DisplayName,
		Slug:           input.Slug,
		LogoURL:        input.LogoURL,
		Website:        input.Website,
		PlatformID:     input.PlatformID,
	}

	if err := app.Models.Merchant.Insert(ctx, merchant); err != nil {
		logger.Error(
			"Insert merchant failed",
			"error", err,
		)
		app.respondWithError(
			w,
			fmt.Errorf("failed to create merchant: %w", err),
			http.StatusInternalServerError,
		)
		return
	}

	app.respondWithMerchantAudit(
		w,
		ctx,
		userID,
		"create_merchant",
		"Create a merchant",
		"merchant",
		"Merchant entity",
		merchant.ID.String(),
		http.StatusCreated,
		"Merchant created successfully",
		merchant,
	)
}

// GetMerchantByIDHandler retrieves an active merchant by its trusted context ID.
func (app *Application) GetMerchantByIDHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	userID := app.requireMerchantPermission(
		w,
		ctx,
		"read_merchant",
	)
	if userID == nil {
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

	merchant, err := app.Models.Merchant.GetByID(ctx, *merchantID)
	if err != nil {
		app.respondWithError(
			w,
			fmt.Errorf("failed to retrieve merchant: %w", err),
			http.StatusInternalServerError,
		)
		return
	}

	if merchant == nil {
		app.respondWithError(
			w,
			errors.New("merchant not found"),
			http.StatusNotFound,
		)
		return
	}

	app.respondWithMerchantAudit(
		w,
		ctx,
		userID,
		"read_merchant",
		"Retrieve a merchant by ID",
		"merchant",
		"Merchant entity",
		merchant.ID.String(),
		http.StatusOK,
		"Merchant retrieved successfully",
		merchant,
	)
}

// GetMerchantByNameHandler retrieves an active merchant by canonical name.
func (app *Application) GetMerchantByNameHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	userID := app.requireMerchantPermission(
		w,
		ctx,
		"read_merchant",
	)
	if userID == nil {
		return
	}

	var input struct {
		Name string `json:"name"`
	}

	if err := app.readJSON(w, r, &input); err != nil {
		app.respondWithError(
			w,
			fmt.Errorf("invalid JSON input: %w", err),
			http.StatusBadRequest,
		)
		return
	}

	merchant, err := app.Models.Merchant.GetByName(ctx, input.Name)
	if err != nil {
		app.respondWithError(
			w,
			fmt.Errorf("failed to retrieve merchant: %w", err),
			http.StatusInternalServerError,
		)
		return
	}

	if merchant == nil {
		app.respondWithError(
			w,
			errors.New("merchant not found"),
			http.StatusNotFound,
		)
		return
	}

	app.respondWithMerchantAudit(
		w,
		ctx,
		userID,
		"read_merchant_by_name",
		"Retrieve a merchant by name",
		"merchant",
		"Merchant entity",
		merchant.ID.String(),
		http.StatusOK,
		"Merchant retrieved successfully",
		merchant,
	)
}

// GetMerchantBySlugHandler retrieves an active merchant by canonical slug.
func (app *Application) GetMerchantBySlugHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	userID := app.requireMerchantPermission(
		w,
		ctx,
		"read_merchant",
	)
	if userID == nil {
		return
	}

	var input struct {
		Slug string `json:"slug"`
	}

	if err := app.readJSON(w, r, &input); err != nil {
		app.respondWithError(
			w,
			fmt.Errorf("invalid JSON input: %w", err),
			http.StatusBadRequest,
		)
		return
	}

	merchant, err := app.Models.Merchant.GetBySlug(ctx, input.Slug)
	if err != nil {
		app.respondWithError(
			w,
			fmt.Errorf("failed to retrieve merchant: %w", err),
			http.StatusInternalServerError,
		)
		return
	}

	if merchant == nil {
		app.respondWithError(
			w,
			errors.New("merchant not found"),
			http.StatusNotFound,
		)
		return
	}

	app.respondWithMerchantAudit(
		w,
		ctx,
		userID,
		"read_merchant_by_slug",
		"Retrieve a merchant by slug",
		"merchant",
		"Merchant entity",
		merchant.ID.String(),
		http.StatusOK,
		"Merchant retrieved successfully",
		merchant,
	)
}

// GetMerchantByOfferIDHandler retrieves the active merchant that owns an offer.
func (app *Application) GetMerchantByOfferIDHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	userID := app.requireMerchantPermission(
		w,
		ctx,
		"read_merchant",
	)
	if userID == nil {
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

	merchant, err := app.Models.Merchant.GetByOfferID(ctx, *offerID)
	if err != nil {
		app.respondWithError(
			w,
			fmt.Errorf("failed to retrieve merchant: %w", err),
			http.StatusInternalServerError,
		)
		return
	}

	if merchant == nil {
		app.respondWithError(
			w,
			errors.New("merchant not found"),
			http.StatusNotFound,
		)
		return
	}

	app.respondWithMerchantAudit(
		w,
		ctx,
		userID,
		"read_merchant_by_offer",
		"Retrieve a merchant by offer ID",
		"merchant",
		"Merchant entity",
		merchant.ID.String(),
		http.StatusOK,
		"Merchant retrieved successfully",
		merchant,
	)
}

// GetMerchantByProductIDHandler retrieves active merchants associated with a
// product.
func (app *Application) GetMerchantByProductIDHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	userID := app.requireMerchantPermission(
		w,
		ctx,
		"read_merchant",
	)
	if userID == nil {
		return
	}

	productID := app.getProductIDFromContext(ctx)
	if productID == nil || *productID == uuid.Nil {
		app.respondWithError(
			w,
			errors.New("product ID not found in context"),
			http.StatusBadRequest,
		)
		return
	}

	merchants, err := app.Models.Merchant.GetByProductID(
		ctx,
		*productID,
	)
	if err != nil {
		app.respondWithError(
			w,
			fmt.Errorf("failed to retrieve merchants: %w", err),
			http.StatusInternalServerError,
		)
		return
	}

	limit, offset := merchantPagination(app, ctx)
	merchants = paginateMerchants(merchants, limit, offset)

	app.respondWithMerchantAudit(
		w,
		ctx,
		userID,
		"read_merchants_by_product",
		"Retrieve merchants by product ID",
		"merchant",
		"Merchant entity",
		productID.String(),
		http.StatusOK,
		"Merchants retrieved successfully",
		merchants,
	)
}

// GetMerchantByBrandIDHandler retrieves active merchants associated with a
// brand's products.
func (app *Application) GetMerchantByBrandIDHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	userID := app.requireMerchantPermission(
		w,
		ctx,
		"read_merchant",
	)
	if userID == nil {
		return
	}

	brandID := app.getBrandIDFromContext(ctx)
	if brandID == nil || *brandID == uuid.Nil {
		app.respondWithError(
			w,
			errors.New("brand ID not found in context"),
			http.StatusBadRequest,
		)
		return
	}

	merchants, err := app.Models.Merchant.GetByBrandID(
		ctx,
		*brandID,
	)
	if err != nil {
		app.respondWithError(
			w,
			fmt.Errorf("failed to retrieve merchants: %w", err),
			http.StatusInternalServerError,
		)
		return
	}

	limit, offset := merchantPagination(app, ctx)
	merchants = paginateMerchants(merchants, limit, offset)

	app.respondWithMerchantAudit(
		w,
		ctx,
		userID,
		"read_merchants_by_brand",
		"Retrieve merchants by brand ID",
		"merchant",
		"Merchant entity",
		brandID.String(),
		http.StatusOK,
		"Merchants retrieved successfully",
		merchants,
	)
}

// GetMerchantByProductLineHandler retrieves active merchants associated with a
// product line.
func (app *Application) GetMerchantByProductLineHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	userID := app.requireMerchantPermission(
		w,
		ctx,
		"read_merchant",
	)
	if userID == nil {
		return
	}

	productLine := strings.TrimSpace(
		app.getProductLineFromContext(ctx),
	)
	if productLine == "" {
		app.respondWithError(
			w,
			errors.New("product line not found in context"),
			http.StatusBadRequest,
		)
		return
	}

	merchants, err := app.Models.Merchant.GetByProductLine(
		ctx,
		productLine,
	)
	if err != nil {
		app.respondWithError(
			w,
			fmt.Errorf("failed to retrieve merchants: %w", err),
			http.StatusInternalServerError,
		)
		return
	}

	limit, offset := merchantPagination(app, ctx)
	merchants = paginateMerchants(merchants, limit, offset)

	app.respondWithMerchantAudit(
		w,
		ctx,
		userID,
		"read_merchants_by_product_line",
		"Retrieve merchants by product line",
		"merchant",
		"Merchant entity",
		productLine,
		http.StatusOK,
		"Merchants retrieved successfully",
		merchants,
	)
}

// GetMerchantByWebsiteHandler retrieves an active merchant by website.
func (app *Application) GetMerchantByWebsiteHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	userID := app.requireMerchantPermission(
		w,
		ctx,
		"read_merchant",
	)
	if userID == nil {
		return
	}

	var input struct {
		Website string `json:"website"`
	}

	if err := app.readJSON(w, r, &input); err != nil {
		app.respondWithError(
			w,
			fmt.Errorf("invalid JSON input: %w", err),
			http.StatusBadRequest,
		)
		return
	}

	merchant, err := app.Models.Merchant.GetByWebsite(
		ctx,
		input.Website,
	)
	if err != nil {
		app.respondWithError(
			w,
			fmt.Errorf("failed to retrieve merchant: %w", err),
			http.StatusInternalServerError,
		)
		return
	}

	if merchant == nil {
		app.respondWithError(
			w,
			errors.New("merchant not found"),
			http.StatusNotFound,
		)
		return
	}

	app.respondWithMerchantAudit(
		w,
		ctx,
		userID,
		"read_merchant_by_website",
		"Retrieve a merchant by website",
		"merchant",
		"Merchant entity",
		merchant.ID.String(),
		http.StatusOK,
		"Merchant retrieved successfully",
		merchant,
	)
}

// GetMerchantByPlatformIDHandler retrieves active merchants associated with a
// platform.
func (app *Application) GetMerchantByPlatformIDHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	userID := app.requireMerchantPermission(
		w,
		ctx,
		"read_merchant",
	)
	if userID == nil {
		return
	}

	platformID := app.getPlatformIDFromContext(ctx)
	if platformID == nil || *platformID == uuid.Nil {
		app.respondWithError(
			w,
			errors.New("platform ID not found in context"),
			http.StatusBadRequest,
		)
		return
	}

	merchants, err := app.Models.Merchant.GetByPlatform(
		ctx,
		*platformID,
	)
	if err != nil {
		app.respondWithError(
			w,
			fmt.Errorf("failed to retrieve merchants: %w", err),
			http.StatusInternalServerError,
		)
		return
	}

	limit, offset := merchantPagination(app, ctx)
	merchants = paginateMerchants(merchants, limit, offset)

	app.respondWithMerchantAudit(
		w,
		ctx,
		userID,
		"read_merchants_by_platform",
		"Retrieve merchants by platform ID",
		"merchant",
		"Merchant entity",
		platformID.String(),
		http.StatusOK,
		"Merchants retrieved successfully",
		merchants,
	)
}

// GetAllMerchantsHandler retrieves a bounded list of active merchants.
func (app *Application) GetAllMerchantsHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	userID := app.requireMerchantPermission(
		w,
		ctx,
		"list_merchants",
	)
	if userID == nil {
		return
	}

	limit, offset := merchantPagination(app, ctx)

	var input struct {
		MerchantTypeID *uuid.UUID `json:"merchant_type_id,omitempty"`
	}

	if r.ContentLength > 0 {
		if err := app.readJSON(w, r, &input); err != nil {
			app.respondWithError(
				w,
				fmt.Errorf("invalid JSON input: %w", err),
				http.StatusBadRequest,
			)
			return
		}
	}

	merchants, err := app.Models.Merchant.GetAll(
		ctx,
		input.MerchantTypeID,
		limit,
		offset,
	)
	if err != nil {
		app.respondWithError(
			w,
			fmt.Errorf("failed to retrieve merchants: %w", err),
			http.StatusInternalServerError,
		)
		return
	}

	app.respondWithMerchantAudit(
		w,
		ctx,
		userID,
		"list_merchants",
		"List merchants",
		"merchant",
		"Merchant entity",
		"*",
		http.StatusOK,
		"Merchants retrieved successfully",
		merchants,
	)
}

// CountMerchantsHandler returns the number of active merchants, optionally
// restricted to one merchant type.
func (app *Application) CountMerchantsHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	userID := app.requireMerchantPermission(
		w,
		ctx,
		"list_merchants",
	)
	if userID == nil {
		return
	}

	var input struct {
		MerchantTypeID *uuid.UUID `json:"merchant_type_id,omitempty"`
	}

	if r.ContentLength > 0 {
		if err := app.readJSON(w, r, &input); err != nil {
			app.respondWithError(
				w,
				fmt.Errorf("invalid JSON input: %w", err),
				http.StatusBadRequest,
			)
			return
		}
	}

	merchantTypeID := uuid.Nil
	if input.MerchantTypeID != nil {
		merchantTypeID = *input.MerchantTypeID
	}

	count, err := app.Models.Merchant.Count(ctx, merchantTypeID)
	if err != nil {
		app.respondWithError(
			w,
			fmt.Errorf("failed to count merchants: %w", err),
			http.StatusInternalServerError,
		)
		return
	}

	app.respondWithMerchantAudit(
		w,
		ctx,
		userID,
		"count_merchants",
		"Count merchants",
		"merchant",
		"Merchant entity",
		"global_count",
		http.StatusOK,
		"Merchant count retrieved successfully",
		count,
	)
}

// UpdateMerchantHandler applies a partial HTTP update to an active merchant.
//
// The current canonical row is loaded first because the data-layer Update
// contract writes the complete required merchant state.
func (app *Application) UpdateMerchantHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	userID := app.requireMerchantPermission(
		w,
		ctx,
		"update_merchant",
	)
	if userID == nil {
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

	current, err := app.Models.Merchant.GetByID(ctx, *merchantID)
	if err != nil {
		app.respondWithError(
			w,
			fmt.Errorf("failed to retrieve merchant: %w", err),
			http.StatusInternalServerError,
		)
		return
	}

	if current == nil {
		app.respondWithError(
			w,
			errors.New("merchant not found"),
			http.StatusNotFound,
		)
		return
	}

	var input struct {
		MerchantTypeID *uuid.UUID  `json:"merchant_type_id,omitempty"`
		Name           *string     `json:"name,omitempty"`
		DisplayName    *string     `json:"display_name,omitempty"`
		Slug           *string     `json:"slug,omitempty"`
		LogoURL        **string    `json:"logo_url,omitempty"`
		Website        **string    `json:"website,omitempty"`
		PlatformID     **uuid.UUID `json:"platform_id,omitempty"`
	}

	if err := app.readJSON(w, r, &input); err != nil {
		app.respondWithError(
			w,
			fmt.Errorf("invalid JSON input: %w", err),
			http.StatusBadRequest,
		)
		return
	}

	if input.MerchantTypeID != nil {
		current.MerchantTypeID = *input.MerchantTypeID
	}
	if input.Name != nil {
		current.Name = *input.Name
	}
	if input.DisplayName != nil {
		current.DisplayName = *input.DisplayName
	}
	if input.Slug != nil {
		current.Slug = *input.Slug
	}
	if input.LogoURL != nil {
		current.LogoURL = *input.LogoURL
	}
	if input.Website != nil {
		current.Website = *input.Website
	}
	if input.PlatformID != nil {
		current.PlatformID = *input.PlatformID
	}

	if err := app.Models.Merchant.Update(ctx, current); err != nil {
		app.respondWithError(
			w,
			fmt.Errorf("failed to update merchant: %w", err),
			http.StatusInternalServerError,
		)
		return
	}

	app.respondWithMerchantAudit(
		w,
		ctx,
		userID,
		"update_merchant",
		"Update a merchant",
		"merchant",
		"Merchant entity",
		current.ID.String(),
		http.StatusOK,
		"Merchant updated successfully",
		current,
	)
}

// SoftDeleteMerchantHandler soft-deletes an active merchant.
func (app *Application) SoftDeleteMerchantHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	userID := app.requireMerchantPermission(
		w,
		ctx,
		"soft_delete_merchant",
	)
	if userID == nil {
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

	if err := app.Models.Merchant.SoftDelete(ctx, *merchantID); err != nil {
		app.respondWithError(
			w,
			fmt.Errorf("failed to delete merchant: %w", err),
			http.StatusInternalServerError,
		)
		return
	}

	app.respondWithMerchantAudit(
		w,
		ctx,
		userID,
		"delete_merchant",
		"Soft-delete a merchant",
		"merchant",
		"Merchant entity",
		merchantID.String(),
		http.StatusOK,
		"Merchant deleted successfully",
		merchantID,
	)
}

// CreateMerchantTypeHandler creates a merchant classification.
func (app *Application) CreateMerchantTypeHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	userID := app.requireMerchantPermission(
		w,
		ctx,
		"create_merchant_type",
	)
	if userID == nil {
		return
	}

	var input struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}

	if err := app.readJSON(w, r, &input); err != nil {
		app.respondWithError(
			w,
			fmt.Errorf("invalid JSON input: %w", err),
			http.StatusBadRequest,
		)
		return
	}

	merchantType := &data.MerchantType{
		Name:        input.Name,
		Description: input.Description,
	}

	if err := app.Models.MerchantType.Insert(
		ctx,
		merchantType,
	); err != nil {
		app.respondWithError(
			w,
			fmt.Errorf("failed to create merchant type: %w", err),
			http.StatusInternalServerError,
		)
		return
	}

	app.respondWithMerchantAudit(
		w,
		ctx,
		userID,
		"create_merchant_type",
		"Create a merchant type",
		"merchant_type",
		"Merchant type entity",
		merchantType.ID.String(),
		http.StatusCreated,
		"Merchant type created successfully",
		merchantType,
	)
}

// GetMerchantTypeByIDHandler retrieves an active merchant type by ID.
func (app *Application) GetMerchantTypeByIDHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	userID := app.requireMerchantPermission(
		w,
		ctx,
		"read_merchant_type",
	)
	if userID == nil {
		return
	}

	merchantTypeID := merchantTypeIDFromContext(app, ctx)
	if merchantTypeID == nil {
		app.respondWithError(
			w,
			errors.New("merchant type ID not found in context"),
			http.StatusBadRequest,
		)
		return
	}

	merchantType, err := app.Models.MerchantType.GetByID(
		ctx,
		*merchantTypeID,
	)
	if err != nil {
		app.respondWithError(
			w,
			fmt.Errorf("failed to retrieve merchant type: %w", err),
			http.StatusInternalServerError,
		)
		return
	}

	if merchantType == nil {
		app.respondWithError(
			w,
			errors.New("merchant type not found"),
			http.StatusNotFound,
		)
		return
	}

	app.respondWithMerchantAudit(
		w,
		ctx,
		userID,
		"read_merchant_type",
		"Retrieve a merchant type by ID",
		"merchant_type",
		"Merchant type entity",
		merchantType.ID.String(),
		http.StatusOK,
		"Merchant type retrieved successfully",
		merchantType,
	)
}

// GetMerchantTypeByNameHandler retrieves an active merchant type by name.
func (app *Application) GetMerchantTypeByNameHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	userID := app.requireMerchantPermission(
		w,
		ctx,
		"read_merchant_type",
	)
	if userID == nil {
		return
	}

	var input struct {
		Name string `json:"name"`
	}

	if err := app.readJSON(w, r, &input); err != nil {
		app.respondWithError(
			w,
			fmt.Errorf("invalid JSON input: %w", err),
			http.StatusBadRequest,
		)
		return
	}

	merchantType, err := app.Models.MerchantType.GetByName(
		ctx,
		input.Name,
	)
	if err != nil {
		app.respondWithError(
			w,
			fmt.Errorf("failed to retrieve merchant type: %w", err),
			http.StatusInternalServerError,
		)
		return
	}

	if merchantType == nil {
		app.respondWithError(
			w,
			errors.New("merchant type not found"),
			http.StatusNotFound,
		)
		return
	}

	app.respondWithMerchantAudit(
		w,
		ctx,
		userID,
		"read_merchant_type_by_name",
		"Retrieve a merchant type by name",
		"merchant_type",
		"Merchant type entity",
		merchantType.ID.String(),
		http.StatusOK,
		"Merchant type retrieved successfully",
		merchantType,
	)
}

// GetAllMerchantTypesHandler retrieves a bounded list of active merchant types.
func (app *Application) GetAllMerchantTypesHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	userID := app.requireMerchantPermission(
		w,
		ctx,
		"list_merchant_types",
	)
	if userID == nil {
		return
	}

	limit, offset := merchantPagination(app, ctx)

	var input struct {
		AffiliateProgramID *uuid.UUID `json:"affiliate_program_id,omitempty"`
	}

	if r.ContentLength > 0 {
		if err := app.readJSON(w, r, &input); err != nil {
			app.respondWithError(
				w,
				fmt.Errorf("invalid JSON input: %w", err),
				http.StatusBadRequest,
			)
			return
		}
	}

	merchantTypes, err := app.Models.MerchantType.GetAll(
		ctx,
		limit,
		offset,
		input.AffiliateProgramID,
	)
	if err != nil {
		app.respondWithError(
			w,
			fmt.Errorf("failed to retrieve merchant types: %w", err),
			http.StatusInternalServerError,
		)
		return
	}

	app.respondWithMerchantAudit(
		w,
		ctx,
		userID,
		"list_merchant_types",
		"List merchant types",
		"merchant_type",
		"Merchant type entity",
		"*",
		http.StatusOK,
		"Merchant types retrieved successfully",
		merchantTypes,
	)
}

// UpdateMerchantTypeHandler applies a partial HTTP update to an active merchant
// type.
func (app *Application) UpdateMerchantTypeHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	userID := app.requireMerchantPermission(
		w,
		ctx,
		"update_merchant_type",
	)
	if userID == nil {
		return
	}

	merchantTypeID := merchantTypeIDFromContext(app, ctx)
	if merchantTypeID == nil {
		app.respondWithError(
			w,
			errors.New("merchant type ID not found in context"),
			http.StatusBadRequest,
		)
		return
	}

	current, err := app.Models.MerchantType.GetByID(
		ctx,
		*merchantTypeID,
	)
	if err != nil {
		app.respondWithError(
			w,
			fmt.Errorf("failed to retrieve merchant type: %w", err),
			http.StatusInternalServerError,
		)
		return
	}

	if current == nil {
		app.respondWithError(
			w,
			errors.New("merchant type not found"),
			http.StatusNotFound,
		)
		return
	}

	var input struct {
		Name        *string `json:"name,omitempty"`
		Description *string `json:"description,omitempty"`
	}

	if err := app.readJSON(w, r, &input); err != nil {
		app.respondWithError(
			w,
			fmt.Errorf("invalid JSON input: %w", err),
			http.StatusBadRequest,
		)
		return
	}

	if input.Name != nil {
		current.Name = *input.Name
	}
	if input.Description != nil {
		current.Description = *input.Description
	}

	if err := app.Models.MerchantType.Update(ctx, current); err != nil {
		app.respondWithError(
			w,
			fmt.Errorf("failed to update merchant type: %w", err),
			http.StatusInternalServerError,
		)
		return
	}

	app.respondWithMerchantAudit(
		w,
		ctx,
		userID,
		"update_merchant_type",
		"Update a merchant type",
		"merchant_type",
		"Merchant type entity",
		current.ID.String(),
		http.StatusOK,
		"Merchant type updated successfully",
		current,
	)
}

// DeleteMerchantTypeHandler soft-deletes an active merchant type.
func (app *Application) DeleteMerchantTypeHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	userID := app.requireMerchantPermission(
		w,
		ctx,
		"soft_delete_merchant_type",
	)
	if userID == nil {
		return
	}

	merchantTypeID := merchantTypeIDFromContext(app, ctx)
	if merchantTypeID == nil {
		app.respondWithError(
			w,
			errors.New("merchant type ID not found in context"),
			http.StatusBadRequest,
		)
		return
	}

	if err := app.Models.MerchantType.SoftDelete(
		ctx,
		*merchantTypeID,
	); err != nil {
		app.respondWithError(
			w,
			fmt.Errorf("failed to delete merchant type: %w", err),
			http.StatusInternalServerError,
		)
		return
	}

	app.respondWithMerchantAudit(
		w,
		ctx,
		userID,
		"delete_merchant_type",
		"Soft-delete a merchant type",
		"merchant_type",
		"Merchant type entity",
		merchantTypeID.String(),
		http.StatusOK,
		"Merchant type deleted successfully",
		merchantTypeID,
	)
}

// CreateMerchantAffiliateProgramHandler creates an active merchant-affiliate
// program association.
func (app *Application) CreateMerchantAffiliateProgramHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	userID := app.requireMerchantPermission(
		w,
		ctx,
		"create_merchant_affiliate_program",
	)
	if userID == nil {
		return
	}

	merchantID := app.getMerchantIDFromContext(ctx)
	affiliateProgramID := app.getAffiliateProgramIDFromContext(ctx)

	if merchantID == nil ||
		*merchantID == uuid.Nil ||
		affiliateProgramID == nil ||
		*affiliateProgramID == uuid.Nil {
		app.respondWithError(
			w,
			errors.New(
				"merchant or affiliate program ID not found in context",
			),
			http.StatusBadRequest,
		)
		return
	}

	association := &data.MerchantAffiliateProgram{
		MerchantID:         *merchantID,
		AffiliateProgramID: *affiliateProgramID,
	}

	if err := app.Models.MerchantAffiliateProgram.Insert(
		ctx,
		association,
	); err != nil {
		app.respondWithError(
			w,
			fmt.Errorf("failed to create association: %w", err),
			http.StatusInternalServerError,
		)
		return
	}

	entityID := merchantID.String() + ":" + affiliateProgramID.String()

	app.respondWithMerchantAudit(
		w,
		ctx,
		userID,
		"create_merchant_affiliate_program",
		"Create a merchant-affiliate program association",
		"merchant_affiliate_program",
		"Merchant-affiliate program association",
		entityID,
		http.StatusCreated,
		"Association created successfully",
		association,
	)
}

// GetMerchantAffiliateProgramByIDsHandler retrieves one active association.
func (app *Application) GetMerchantAffiliateProgramByIDsHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	userID := app.requireMerchantPermission(
		w,
		ctx,
		"read_merchant_affiliate_program",
	)
	if userID == nil {
		return
	}

	merchantID := app.getMerchantIDFromContext(ctx)
	affiliateProgramID := app.getAffiliateProgramIDFromContext(ctx)

	if merchantID == nil ||
		*merchantID == uuid.Nil ||
		affiliateProgramID == nil ||
		*affiliateProgramID == uuid.Nil {
		app.respondWithError(
			w,
			errors.New(
				"merchant or affiliate program ID not found in context",
			),
			http.StatusBadRequest,
		)
		return
	}

	association, err :=
		app.Models.MerchantAffiliateProgram.GetByIDs(
			ctx,
			*merchantID,
			*affiliateProgramID,
		)
	if err != nil {
		app.respondWithError(
			w,
			fmt.Errorf("failed to retrieve association: %w", err),
			http.StatusInternalServerError,
		)
		return
	}

	if association == nil {
		app.respondWithError(
			w,
			errors.New("association not found"),
			http.StatusNotFound,
		)
		return
	}

	entityID := merchantID.String() + ":" + affiliateProgramID.String()

	app.respondWithMerchantAudit(
		w,
		ctx,
		userID,
		"read_merchant_affiliate_program",
		"Retrieve a merchant-affiliate program association",
		"merchant_affiliate_program",
		"Merchant-affiliate program association",
		entityID,
		http.StatusOK,
		"Association retrieved successfully",
		association,
	)
}

// GetMerchantAffiliateProgramByMerchantIDHandler retrieves public-safe
// affiliate-program summaries associated with one merchant.
func (app *Application) GetMerchantAffiliateProgramByMerchantIDHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	userID := app.requireMerchantPermission(
		w,
		ctx,
		"read_merchant_affiliate_programs",
	)
	if userID == nil {
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

	limit, offset := merchantPagination(app, ctx)

	programs, err :=
		app.Models.MerchantAffiliateProgram.
			GetFullAffiliateProgramsByMerchantID(
				ctx,
				*merchantID,
				limit,
				offset,
			)
	if err != nil {
		app.respondWithError(
			w,
			fmt.Errorf(
				"failed to retrieve affiliate programs: %w",
				err,
			),
			http.StatusInternalServerError,
		)
		return
	}

	app.respondWithMerchantAudit(
		w,
		ctx,
		userID,
		"read_affiliate_programs_by_merchant",
		"Retrieve affiliate programs by merchant ID",
		"merchant_affiliate_program",
		"Merchant-affiliate program association",
		merchantID.String(),
		http.StatusOK,
		"Affiliate programs retrieved successfully",
		programs,
	)
}

// GetMerchantAffiliateProgramByAffiliateProgramIDHandler retrieves active
// merchant associations for one affiliate program.
func (app *Application) GetMerchantAffiliateProgramByAffiliateProgramIDHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	userID := app.requireMerchantPermission(
		w,
		ctx,
		"read_merchant_affiliate_programs",
	)
	if userID == nil {
		return
	}

	affiliateProgramID := app.getAffiliateProgramIDFromContext(ctx)
	if affiliateProgramID == nil || *affiliateProgramID == uuid.Nil {
		app.respondWithError(
			w,
			errors.New("affiliate program ID not found in context"),
			http.StatusBadRequest,
		)
		return
	}

	associations, err :=
		app.Models.MerchantAffiliateProgram.
			GetByAffiliateProgramID(
				ctx,
				*affiliateProgramID,
			)
	if err != nil {
		app.respondWithError(
			w,
			fmt.Errorf("failed to retrieve associations: %w", err),
			http.StatusInternalServerError,
		)
		return
	}

	app.respondWithMerchantAudit(
		w,
		ctx,
		userID,
		"read_merchants_by_affiliate_program",
		"Retrieve merchant associations by affiliate program ID",
		"merchant_affiliate_program",
		"Merchant-affiliate program association",
		affiliateProgramID.String(),
		http.StatusOK,
		"Associations retrieved successfully",
		associations,
	)
}

// GetAllMerchantAffiliateProgramsHandler retrieves all active
// merchant-affiliate-program associations.
func (app *Application) GetAllMerchantAffiliateProgramsHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	userID := app.requireMerchantPermission(
		w,
		ctx,
		"list_merchant_affiliate_programs",
	)
	if userID == nil {
		return
	}

	associations, err :=
		app.Models.MerchantAffiliateProgram.GetAll(ctx)
	if err != nil {
		app.respondWithError(
			w,
			fmt.Errorf("failed to retrieve associations: %w", err),
			http.StatusInternalServerError,
		)
		return
	}

	app.respondWithMerchantAudit(
		w,
		ctx,
		userID,
		"list_merchant_affiliate_programs",
		"List merchant-affiliate program associations",
		"merchant_affiliate_program",
		"Merchant-affiliate program association",
		"*",
		http.StatusOK,
		"Associations retrieved successfully",
		associations,
	)
}

// DeleteMerchantAffiliateProgramHandler soft-deletes one active association.
func (app *Application) DeleteMerchantAffiliateProgramHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	userID := app.requireMerchantPermission(
		w,
		ctx,
		"soft_delete_merchant_affiliate_program",
	)
	if userID == nil {
		return
	}

	merchantID := app.getMerchantIDFromContext(ctx)
	affiliateProgramID := app.getAffiliateProgramIDFromContext(ctx)

	if merchantID == nil ||
		*merchantID == uuid.Nil ||
		affiliateProgramID == nil ||
		*affiliateProgramID == uuid.Nil {
		app.respondWithError(
			w,
			errors.New(
				"merchant or affiliate program ID not found in context",
			),
			http.StatusBadRequest,
		)
		return
	}

	if err := app.Models.MerchantAffiliateProgram.SoftDelete(
		ctx,
		*merchantID,
		*affiliateProgramID,
	); err != nil {
		app.respondWithError(
			w,
			fmt.Errorf("failed to delete association: %w", err),
			http.StatusInternalServerError,
		)
		return
	}

	entityID := merchantID.String() + ":" + affiliateProgramID.String()

	app.respondWithMerchantAudit(
		w,
		ctx,
		userID,
		"delete_merchant_affiliate_program",
		"Soft-delete a merchant-affiliate program association",
		"merchant_affiliate_program",
		"Merchant-affiliate program association",
		entityID,
		http.StatusOK,
		"Association deleted successfully",
		entityID,
	)
}

// DeleteMerchantAffiliateProgramByMerchantIDHandler soft-deletes all active
// associations owned by one merchant.
func (app *Application) DeleteMerchantAffiliateProgramByMerchantIDHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	userID := app.requireMerchantPermission(
		w,
		ctx,
		"soft_delete_merchant_affiliate_program",
	)
	if userID == nil {
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

	if err := app.Models.MerchantAffiliateProgram.
		SoftDeleteByMerchantID(
			ctx,
			*merchantID,
		); err != nil {
		app.respondWithError(
			w,
			fmt.Errorf("failed to delete associations: %w", err),
			http.StatusInternalServerError,
		)
		return
	}

	app.respondWithMerchantAudit(
		w,
		ctx,
		userID,
		"delete_merchant_affiliate_programs_by_merchant",
		"Soft-delete associations by merchant ID",
		"merchant_affiliate_program",
		"Merchant-affiliate program association",
		merchantID.String(),
		http.StatusOK,
		"Associations deleted successfully",
		merchantID,
	)
}

// DeleteMerchantAffiliateProgramByAffiliateProgramIDHandler soft-deletes all
// active associations for one affiliate program.
func (app *Application) DeleteMerchantAffiliateProgramByAffiliateProgramIDHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	userID := app.requireMerchantPermission(
		w,
		ctx,
		"soft_delete_merchant_affiliate_program",
	)
	if userID == nil {
		return
	}

	affiliateProgramID := app.getAffiliateProgramIDFromContext(ctx)
	if affiliateProgramID == nil || *affiliateProgramID == uuid.Nil {
		app.respondWithError(
			w,
			errors.New("affiliate program ID not found in context"),
			http.StatusBadRequest,
		)
		return
	}

	if err := app.Models.MerchantAffiliateProgram.
		SoftDeleteByAffiliateProgramID(
			ctx,
			*affiliateProgramID,
		); err != nil {
		app.respondWithError(
			w,
			fmt.Errorf("failed to delete associations: %w", err),
			http.StatusInternalServerError,
		)
		return
	}

	app.respondWithMerchantAudit(
		w,
		ctx,
		userID,
		"delete_merchant_affiliate_programs_by_program",
		"Soft-delete associations by affiliate program ID",
		"merchant_affiliate_program",
		"Merchant-affiliate program association",
		affiliateProgramID.String(),
		http.StatusOK,
		"Associations deleted successfully",
		affiliateProgramID,
	)
}

// CreatePlatformHandler creates a merchant platform.
func (app *Application) CreatePlatformHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	userID := app.requireMerchantPermission(
		w,
		ctx,
		"create_platform",
	)
	if userID == nil {
		return
	}

	var input struct {
		Name        string  `json:"name"`
		Description *string `json:"description,omitempty"`
		Website     *string `json:"website,omitempty"`
	}

	if err := app.readJSON(w, r, &input); err != nil {
		app.respondWithError(
			w,
			fmt.Errorf("invalid JSON input: %w", err),
			http.StatusBadRequest,
		)
		return
	}

	platform := &data.Platform{
		Name:        input.Name,
		Description: input.Description,
		Website:     input.Website,
	}

	if err := app.Models.Platform.Insert(ctx, platform); err != nil {
		app.respondWithError(
			w,
			fmt.Errorf("failed to create platform: %w", err),
			http.StatusInternalServerError,
		)
		return
	}

	app.respondWithMerchantAudit(
		w,
		ctx,
		userID,
		"create_platform",
		"Create a platform",
		"platform",
		"Platform entity",
		platform.ID.String(),
		http.StatusCreated,
		"Platform created successfully",
		platform,
	)
}

// GetPlatformByIDHandler retrieves an active platform by ID.
func (app *Application) GetPlatformByIDHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	userID := app.requireMerchantPermission(
		w,
		ctx,
		"read_platform",
	)
	if userID == nil {
		return
	}

	platformID := app.getPlatformIDFromContext(ctx)
	if platformID == nil || *platformID == uuid.Nil {
		app.respondWithError(
			w,
			errors.New("platform ID not found in context"),
			http.StatusBadRequest,
		)
		return
	}

	platform, err := app.Models.Platform.GetByID(ctx, *platformID)
	if err != nil {
		app.respondWithError(
			w,
			fmt.Errorf("failed to retrieve platform: %w", err),
			http.StatusInternalServerError,
		)
		return
	}

	if platform == nil {
		app.respondWithError(
			w,
			errors.New("platform not found"),
			http.StatusNotFound,
		)
		return
	}

	app.respondWithMerchantAudit(
		w,
		ctx,
		userID,
		"read_platform",
		"Retrieve a platform by ID",
		"platform",
		"Platform entity",
		platform.ID.String(),
		http.StatusOK,
		"Platform retrieved successfully",
		platform,
	)
}

// GetPlatformByNameHandler retrieves an active platform by name.
func (app *Application) GetPlatformByNameHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	userID := app.requireMerchantPermission(
		w,
		ctx,
		"read_platform",
	)
	if userID == nil {
		return
	}

	var input struct {
		Name string `json:"name"`
	}

	if err := app.readJSON(w, r, &input); err != nil {
		app.respondWithError(
			w,
			fmt.Errorf("invalid JSON input: %w", err),
			http.StatusBadRequest,
		)
		return
	}

	platform, err := app.Models.Platform.GetByName(ctx, input.Name)
	if err != nil {
		app.respondWithError(
			w,
			fmt.Errorf("failed to retrieve platform: %w", err),
			http.StatusInternalServerError,
		)
		return
	}

	if platform == nil {
		app.respondWithError(
			w,
			errors.New("platform not found"),
			http.StatusNotFound,
		)
		return
	}

	app.respondWithMerchantAudit(
		w,
		ctx,
		userID,
		"read_platform_by_name",
		"Retrieve a platform by name",
		"platform",
		"Platform entity",
		platform.ID.String(),
		http.StatusOK,
		"Platform retrieved successfully",
		platform,
	)
}

// GetAllPlatformsHandler retrieves all active platforms.
func (app *Application) GetAllPlatformsHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	userID := app.requireMerchantPermission(
		w,
		ctx,
		"list_platforms",
	)
	if userID == nil {
		return
	}

	platforms, err := app.Models.Platform.GetAll(ctx)
	if err != nil {
		app.respondWithError(
			w,
			fmt.Errorf("failed to retrieve platforms: %w", err),
			http.StatusInternalServerError,
		)
		return
	}

	app.respondWithMerchantAudit(
		w,
		ctx,
		userID,
		"list_platforms",
		"List platforms",
		"platform",
		"Platform entity",
		"*",
		http.StatusOK,
		"Platforms retrieved successfully",
		platforms,
	)
}

// UpdatePlatformHandler applies a partial HTTP update to an active platform.
func (app *Application) UpdatePlatformHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	userID := app.requireMerchantPermission(
		w,
		ctx,
		"update_platform",
	)
	if userID == nil {
		return
	}

	platformID := app.getPlatformIDFromContext(ctx)
	if platformID == nil || *platformID == uuid.Nil {
		app.respondWithError(
			w,
			errors.New("platform ID not found in context"),
			http.StatusBadRequest,
		)
		return
	}

	current, err := app.Models.Platform.GetByID(ctx, *platformID)
	if err != nil {
		app.respondWithError(
			w,
			fmt.Errorf("failed to retrieve platform: %w", err),
			http.StatusInternalServerError,
		)
		return
	}

	if current == nil {
		app.respondWithError(
			w,
			errors.New("platform not found"),
			http.StatusNotFound,
		)
		return
	}

	var input struct {
		Name        *string  `json:"name,omitempty"`
		Description **string `json:"description,omitempty"`
		Website     **string `json:"website,omitempty"`
	}

	if err := app.readJSON(w, r, &input); err != nil {
		app.respondWithError(
			w,
			fmt.Errorf("invalid JSON input: %w", err),
			http.StatusBadRequest,
		)
		return
	}

	if input.Name != nil {
		current.Name = *input.Name
	}
	if input.Description != nil {
		current.Description = *input.Description
	}
	if input.Website != nil {
		current.Website = *input.Website
	}

	if err := app.Models.Platform.Update(ctx, current); err != nil {
		app.respondWithError(
			w,
			fmt.Errorf("failed to update platform: %w", err),
			http.StatusInternalServerError,
		)
		return
	}

	app.respondWithMerchantAudit(
		w,
		ctx,
		userID,
		"update_platform",
		"Update a platform",
		"platform",
		"Platform entity",
		current.ID.String(),
		http.StatusOK,
		"Platform updated successfully",
		current,
	)
}

// SoftDeletePlatformHandler soft-deletes an active platform.
func (app *Application) SoftDeletePlatformHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	userID := app.requireMerchantPermission(
		w,
		ctx,
		"soft_delete_platform",
	)
	if userID == nil {
		return
	}

	platformID := app.getPlatformIDFromContext(ctx)
	if platformID == nil || *platformID == uuid.Nil {
		app.respondWithError(
			w,
			errors.New("platform ID not found in context"),
			http.StatusBadRequest,
		)
		return
	}

	if err := app.Models.Platform.SoftDelete(
		ctx,
		*platformID,
	); err != nil {
		app.respondWithError(
			w,
			fmt.Errorf("failed to delete platform: %w", err),
			http.StatusInternalServerError,
		)
		return
	}

	app.respondWithMerchantAudit(
		w,
		ctx,
		userID,
		"delete_platform",
		"Soft-delete a platform",
		"platform",
		"Platform entity",
		platformID.String(),
		http.StatusOK,
		"Platform deleted successfully",
		platformID,
	)
}
