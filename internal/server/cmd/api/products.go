// Package main provides HTTP handlers for the SagrentiDeals API.
//
// sdworkspace/sdbackend/internal/server/cmd/api/products.go
//
// GTM:
//
//	Layer: 2.5 Catalog / Offer Domain
//	Release Class: DEFERRED
//	Reason:
//	  Standalone product and brand HTTP administration is valid expanded
//	  catalog infrastructure, but it is not required for the initial
//	  SagrentiDeals release spine. The initial release prioritizes canonical
//	  offers, publication governance, commerce routing, attribution,
//	  merchant foundations, and the Future Offering Platform supported by
//	  its Monetization Layer.
//
//	  Canonical product and brand persistence remains available in the data
//	  layer for internal catalog identity and offer support. This release
//	  classification defers only the standalone HTTP management surface.
//
// DEFERRED Rule:
//
//	Keep compiling.
//	Keep safe.
//	Preserve alignment with the canonical product and brand data models.
//	Preserve database-owned IDs and lifecycle timestamps.
//	Preserve DeletedAt-based soft-delete semantics.
//	Do not recreate removed IsDeleted compatibility fields.
//	Do not application-write database-owned created_at, updated_at,
//	deleted_at, or equivalent persisted lifecycle timestamps.
//	Do not expose standalone product or brand management workflows in the
//	v1 API.
//	Do not add new product or brand HTTP capabilities while deferred.
//	Do not perform release-blocking expansion work in this file unless it
//	breaks compilation or corrupts canonical catalog behavior.
package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/PiccoloMondoC/sdworkspace/sdbackend/internal/data"

	"github.com/google/uuid"
)

// Principles:
// Always extract sensitive identifiers from a trusted context


// CreateProductHandler handles the creation of a new product.
// It enforces permission checks, extracts trusted identifiers from context,
// parses the request payload, validates required fields, performs the insertion operation,
// dynamically resolves audit action/entity metadata if missing, and logs the operation.
func (app *Application) CreateProductHandler(w http.ResponseWriter, r *http.Request) {
	logger := app.Logger.GetLoggerWithContext(r).WithFunctionName("CreateProductHandler")

	// Derive a bounded, cancellable context for DB work.
	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	// Permission enforcement
	if !app.HasPermission(ctx, "create_product") {
		app.respondWithError(w, errors.New("forbidden: insufficient permissions"), http.StatusForbidden)
		return
	}

	// Extract authenticated user ID (uuid.UUID, already type‑safe)
	userID := app.getUserIDFromContext(ctx)
	if userID == nil {
		app.respondWithError(w, errors.New("missing user ID in context"), http.StatusUnauthorized)
		return
	}

	// Parse request payload
	var input struct {
		Name        string  `json:"name"`        // Mandatory
		Description *string `json:"description"` // Optional
	}

	if err := app.readJSON(w, r, &input); err != nil {
		app.respondWithError(w, fmt.Errorf("invalid JSON input: %w", err), http.StatusBadRequest)
		return
	}

	// Validate input
	if input.Name == "" {
		app.respondWithError(w, errors.New("name is required"), http.StatusBadRequest)
		return
	}

	// Build the product from caller-owned input only.
	//
	// ProductModel.Insert owns ID generation and persisted lifecycle
	// timestamps through the database INSERT ... RETURNING contract.
	product := &data.Product{
		Name:        strings.TrimSpace(input.Name),
		Description: input.Description,
	}
	// Insert Product into database
	if err := app.Models.Product.Insert(ctx, product); err != nil {
		logger.Error("Failed to insert product", "error", err)
		app.respondWithError(w, fmt.Errorf("failed to create product: %w", err), http.StatusInternalServerError)
		return
	}

	// Resolve Audit Action
	action, err := app.Models.Action.GetByName(ctx, "create_product")
	if err != nil || action == nil {
		logger.Warn("Audit action 'create_product' not found, attempting to create...", "error", err)
		actionID, createErr := app.Models.Action.CreateIfNotExists(ctx, "create_product", "Create a new product")
		if createErr != nil {
			logger.Error("Failed to create missing audit action", "error", createErr)
			// Allow main operation to succeed
		} else {
			action = &data.Action{ID: actionID}
		}
	}

	// Resolve Audit Entity Type
	entityType, err := app.Models.EntityType.GetByName(ctx, "product")
	if err != nil || entityType == nil {
		logger.Warn("Audit entity type 'product' not found, attempting to create...", "error", err)
		entityTypeID, createErr := app.Models.EntityType.CreateIfNotExists(ctx, "product", "Product entity")
		if createErr != nil {
			logger.Error("Failed to create missing entity type", "error", createErr)
			// Allow main operation to succeed
		} else {
			entityType = &data.EntityType{ID: entityTypeID}
		}
	}

	// Insert Audit Log (fail gracefully if needed)
	if userID != nil && action != nil && entityType != nil {
		audit := data.AuditLog{
			ID:           uuid.New(),
			UserID:       userID,
			ActionID:     action.ID,
			EntityTypeID: entityType.ID,
			EntityID:     product.ID.String(),
		}
		if err := app.Models.AuditLog.Insert(ctx, &audit); err != nil {
			// Audit logging failed (partial success)
			logger.Warn("Audit logging failed", "product_id", product.ID, "error", err)
			app.respondWithJSON(w, http.StatusPartialContent, jsonResponse{
				Error:   false,
				Message: "Product created, but audit logging failed",
				Data:    product.ID,
			})
			return
		}
	}

	// Respond success
	logger.Info("Product created successfully", "product_id", product.ID)
	app.respondWithJSON(w, http.StatusCreated, jsonResponse{
		Error:   false,
		Message: "Product created successfully",
		Data:    product.ID,
	})
}


// GetProductByIDHandler handles retrieving a product by its ID.
// It enforces permission checks, extracts the product ID from the context,
// retrieves the product from the database, performs audit logging,
// and responds with the product details.
func (app *Application) GetProductByIDHandler(w http.ResponseWriter, r *http.Request) {
	logger := app.Logger.GetLoggerWithContext(r).WithFunctionName("GetProductByIDHandler")
	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	// Permission enforcement
	if !app.HasPermission(ctx, "read_product") {
		app.respondWithError(w, errors.New("forbidden: insufficient permissions"), http.StatusForbidden)
		return
	}

	// Extract Product ID from context (injected via middleware)
	productID := app.getProductIDFromContext(ctx)
	if productID == nil {
		app.respondWithError(w, errors.New("missing product ID in context"), http.StatusBadRequest)
		return
	}

	// Retrieve the product from the database
	product, err := app.Models.Product.GetByID(ctx, *productID)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			app.respondWithError(w, fmt.Errorf("product not found"), http.StatusNotFound)
		} else {
			logger.Error("Failed to retrieve product", "error", err)
			app.respondWithError(w, fmt.Errorf("failed to retrieve product: %w", err), http.StatusInternalServerError)
		}
		return
	}

	// Resolve Audit Action
	action, err := app.Models.Action.GetByName(ctx, "read_product")
	if err != nil || action == nil {
		logger.Warn("Audit action 'read_product' not found, attempting to create...", "error", err)
		actionID, createErr := app.Models.Action.CreateIfNotExists(ctx, "read_product", "Retrieve a product by ID")
		if createErr != nil {
			logger.Error("Failed to create missing audit action", "error", createErr)
			// Allow main operation to proceed
		} else {
			action = &data.Action{ID: actionID}
		}
	}

	// Resolve Audit Entity Type
	entityType, err := app.Models.EntityType.GetByName(ctx, "product")
	if err != nil || entityType == nil {
		logger.Warn("Audit entity type 'product' not found, attempting to create...", "error", err)
		entityTypeID, createErr := app.Models.EntityType.CreateIfNotExists(ctx, "product", "Product details and information")
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
			EntityID:     product.ID.String(),
		}
		if err := app.Models.AuditLog.Insert(ctx, &audit); err != nil {
			// Audit logging failed (partial success)
			logger.Warn("Audit logging failed", "product_id", product.ID, "error", err)
			app.respondWithJSON(w, http.StatusPartialContent, jsonResponse{
				Error:   false,
				Message: "Product retrieved, but audit logging failed",
				Data:    product,
			})
			return
		}
	}

	// Success
	logger.Info("Successfully retrieved product", "product_id", product.ID)
	app.respondWithJSON(w, http.StatusOK, jsonResponse{
		Error:   false,
		Message: "Product retrieved successfully",
		Data:    product,
	})
}


func (app *Application) GetAllProductsHandler(w http.ResponseWriter, r *http.Request) {
	logger := app.Logger.GetLoggerWithContext(r).WithFunctionName("GetAllProductsHandler")
	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	// Permission enforcement
	if !app.HasPermission(ctx, "read_all_products") {
		app.respondWithError(w, errors.New("forbidden: insufficient permissions"), http.StatusForbidden)
		return
	}

	//  Parse pagination parameters (?limit & ?offset). Defaults: 50/0.
	limit, offset, err := app.parseLimitOffset(r)
	if err != nil {
		app.respondWithError(w, err, http.StatusBadRequest)
		return
	}

	// Retrieve all products from the database
	products, err := app.Models.Product.GetAll(ctx, limit, offset)
	if err != nil {
		logger.Error("Failed to retrieve products", "error", err)
		app.respondWithError(w, fmt.Errorf("failed to retrieve products: %w", err), http.StatusInternalServerError)
		return
	}

	// Resolve Audit Action
	action, err := app.Models.Action.GetByName(ctx, "read_all_products")
	if err != nil || action == nil {
		logger.Warn("Audit action 'read_all_products' not found, attempting to create...", "error", err)
		actionID, createErr := app.Models.Action.CreateIfNotExists(ctx, "read_all_products", "Retrieve all products")
		if createErr != nil {
			logger.Error("Failed to create missing audit action", "error", createErr)
			// Allow main operation to succeed
		} else {
			action = &data.Action{ID: actionID}
		}
	}

	// Resolve Audit Entity Type
	entityType, err := app.Models.EntityType.GetByName(ctx, "product")
	if err != nil || entityType == nil {
		logger.Warn("Audit entity type 'product' not found, attempting to create...", "error", err)
		entityTypeID, createErr := app.Models.EntityType.CreateIfNotExists(ctx, "product", "Product entity")
		if createErr != nil {
			logger.Error("Failed to create missing entity type", "error", createErr)
			// Allow main operation to succeed
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
			EntityID:     "all_products", // Using a generic ID for bulk operations
		}
		if err := app.Models.AuditLog.Insert(ctx, &audit); err != nil {
			// Audit logging failed (partial success)
			logger.Warn("Audit logging failed", "error", err)
			app.respondWithJSON(w, http.StatusPartialContent, jsonResponse{
				Error:   false,
				Message: "Products retrieved, but audit logging failed",
				Data:    products,
			})
			return
		}
	}

	// Success
	logger.Info("Successfully retrieved all products", "count", len(products))
	app.respondWithJSON(w, http.StatusOK, jsonResponse{
		Error:   false,
		Message: "Products retrieved successfully",
		Data:    products,
	})
}


// GetProductByUPCHandler handles retrieving a product by its UPC (Universal Product Code).
// It enforces permission checks, extracts the UPC from a trusted context, queries the database,
// performs structured logging, and logs an audit record for traceability.
func (app *Application) GetProductByUPCHandler(w http.ResponseWriter, r *http.Request) {
	logger := app.Logger.GetLoggerWithContext(r).WithFunctionName("GetProductByUPCHandler")
	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	// Permission enforcement
	if !app.HasPermission(ctx, "read_product") {
		app.respondWithError(w, errors.New("forbidden: insufficient permissions"), http.StatusForbidden)
		return
	}

	// Extract UPC from trusted context
	upc := app.getUPCFromContext(ctx)
	if upc == "" {
		app.respondWithError(w, errors.New("missing UPC in context"), http.StatusBadRequest)
		return
	}

	// Retrieve product by UPC from the database
	product, err := app.Models.Product.GetByUPC(ctx, upc)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			app.respondWithError(w, fmt.Errorf("product not found"), http.StatusNotFound)
		} else {
			logger.Error("Failed to retrieve product by UPC", "upc", upc, "error", err)
			app.respondWithError(w, fmt.Errorf("failed to retrieve product: %w", err), http.StatusInternalServerError)
		}
		return
	}

	// Resolve Audit Action
	action, err := app.Models.Action.GetByName(ctx, "read_product_by_upc")
	if err != nil || action == nil {
		logger.Warn("Audit action 'read_product_by_upc' not found, attempting to create...", "error", err)
		actionID, createErr := app.Models.Action.CreateIfNotExists(ctx, "read_product_by_upc", "Retrieve a product by UPC")
		if createErr != nil {
			logger.Error("Failed to create missing audit action", "error", createErr)
			// Allow main operation to succeed
		} else {
			action = &data.Action{ID: actionID}
		}
	}

	// Resolve Audit Entity Type
	entityType, err := app.Models.EntityType.GetByName(ctx, "product")
	if err != nil || entityType == nil {
		logger.Warn("Audit entity type 'product' not found, attempting to create...", "error", err)
		entityTypeID, createErr := app.Models.EntityType.CreateIfNotExists(ctx, "product", "Product entity")
		if createErr != nil {
			logger.Error("Failed to create missing entity type", "error", createErr)
			// Allow main operation to succeed
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
			EntityID:     product.ID.String(),
		}
		if err := app.Models.AuditLog.Insert(ctx, &audit); err != nil {
			// Audit logging failed (partial success)
			logger.Warn("Audit logging failed", "product_id", product.ID, "error", err)
			app.respondWithJSON(w, http.StatusPartialContent, jsonResponse{
				Error:   false,
				Message: "Product retrieved by UPC, but audit logging failed",
				Data:    product,
			})
			return
		}
	}

	// Respond with the product details
	logger.Info("Successfully retrieved product by UPC", "upc", upc, "product_id", product.ID)
	app.respondWithJSON(w, http.StatusOK, jsonResponse{
		Error:   false,
		Message: "Product retrieved successfully",
		Data:    product,
	})
}


// UpdateProductHandler handles the update of an existing product.
// It enforces permission checks, extracts trusted identifiers from context,
// parses the request payload, validates required fields, performs the update operation,
// dynamically resolves audit action/entity metadata if missing, and logs the operation.
func (app *Application) UpdateProductHandler(w http.ResponseWriter, r *http.Request) {
	logger := app.Logger.GetLoggerWithContext(r).WithFunctionName("UpdateProductHandler")
	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	// Permission enforcement
	if !app.HasPermission(ctx, "update_product") {
		app.respondWithError(w, errors.New("forbidden: insufficient permissions"), http.StatusForbidden)
		return
	}

	// Extract Product ID from context (middleware-injected)
	productID := app.getProductIDFromContext(ctx)
	if productID == nil {
		app.respondWithError(w, errors.New("missing product ID in context"), http.StatusBadRequest)
		return
	}

	// Fetch current record 
	current, err := app.Models.Product.GetByID(ctx, *productID)
	if err != nil {
		app.respondWithError(w, fmt.Errorf("product not found: %w", err), http.StatusNotFound)
		return
	}

	// Decode PATCH payload
	var input struct {
		Name        *string  `json:"name,omitempty"`        // Optional
		Description *string  `json:"description,omitempty"` // Optional
	}

	if err := app.readJSON(w, r, &input); err != nil {
		app.respondWithError(w, fmt.Errorf("invalid JSON input: %w", err), http.StatusBadRequest)
		return
	}

	// Apply caller-owned field changes.
	//
	// ProductModel.Update owns updated_at through PostgreSQL NOW() and
	// returns the canonical updated row.
	updated := *current
	if input.Name != nil {
		updated.Name = strings.TrimSpace(*input.Name)
	}
	if input.Description != nil {
		updated.Description = input.Description
	}

	// Persist
	if err := app.Models.Product.Update(ctx, &updated); err != nil {
		logger.Error("update failed", "error", err)
		app.respondWithError(w, fmt.Errorf("failed to update product: %w", err), http.StatusInternalServerError)
		return
	}

	// Resolve Audit Action
	action, err := app.Models.Action.GetByName(ctx, "update_product")
	if err != nil || action == nil {
		logger.Warn("Audit action 'update_product' not found, attempting to create...", "error", err)
		actionID, createErr := app.Models.Action.CreateIfNotExists(ctx, "update_product", "Update an existing product record")
		if createErr != nil {
			logger.Error("Failed to create missing audit action", "error", createErr)
			// Allow main operation to succeed
		} else {
			action = &data.Action{ID: actionID}
		}
	}

	// Resolve Audit Entity Type
	entityType, err := app.Models.EntityType.GetByName(ctx, "product")
	if err != nil || entityType == nil {
		logger.Warn("Audit entity type 'product' not found, attempting to create...", "error", err)
		entityTypeID, createErr := app.Models.EntityType.CreateIfNotExists(ctx, "product", "Product information")
		if createErr != nil {
			logger.Error("Failed to create missing entity type", "error", createErr)
			// Allow main operation to succeed
		} else {
			entityType = &data.EntityType{ID: entityTypeID}
		}
	}

	// Insert Audit Log (fail gracefully if needed)
	userID := app.getUserIDFromContext(ctx)
	if userID != nil && action != nil && entityType != nil {
		audit := data.AuditLog{
			ID:           uuid.New(),
			UserID:       userID,
			ActionID:     action.ID,
			EntityTypeID: entityType.ID,
			EntityID:     updated.ID.String(),
		}
		if err := app.Models.AuditLog.Insert(ctx, &audit); err != nil {
			// Audit logging failed (partial success)
			logger.Warn("Audit logging failed", "product_id", updated.ID, "error", err)
			app.respondWithJSON(w, http.StatusPartialContent, jsonResponse{
				Error:   false,
				Message: "Product updated, but audit logging failed",
				Data:    updated.ID,
			})
			return
		}
	}

	// Respond success
	logger.Info("Product updated successfully", "product_id", updated.ID)
	app.respondWithJSON(w, http.StatusOK, jsonResponse{
		Error:   false,
		Message: "Product updated successfully",
		Data:    updated.ID,
	})
}


// SoftDeleteProductHandler handles the soft deletion of a product.
// It ensures permission enforcement, extracts trusted identifiers from context,
// performs the soft delete operation, logs the action, and records an audit log.
func (app *Application) SoftDeleteProductHandler(w http.ResponseWriter, r *http.Request) {
	logger := app.Logger.WithFunctionName("SoftDeleteProductHandler")
	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	// Permission enforcement
	if !app.HasPermission(ctx, "soft_delete_product") {
		app.respondWithError(w, errors.New("forbidden: insufficient permissions"), http.StatusForbidden)
		return
	}

	// Extract Product ID from trusted context
	productID := app.getProductIDFromContext(ctx)
	if productID == nil {
		app.respondWithError(w, errors.New("missing product ID in context"), http.StatusBadRequest)
		return
	}

	// Perform soft delete operation
	err := app.Models.Product.SoftDelete(ctx, *productID)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			app.respondWithError(w, fmt.Errorf("product not found"), http.StatusNotFound)
		} else {
			logger.Error("Failed to soft delete product", "product_id", productID, "error", err)
			app.respondWithError(w, fmt.Errorf("failed to soft delete product: %w", err), http.StatusInternalServerError)
		}
		return
	}

	// Resolve Audit Action
	action, err := app.Models.Action.GetByName(ctx, "soft_delete_product")
	if err != nil || action == nil {
		logger.Warn("Audit action 'soft_delete_product' not found, attempting to create...", "error", err)
		actionID, createErr := app.Models.Action.CreateIfNotExists(ctx, "soft_delete_product", "Soft delete an existing product")
		if createErr != nil {
			logger.Error("Failed to create missing audit action", "error", createErr)
			// Allow main operation to succeed
		} else {
			action = &data.Action{ID: actionID}
		}
	}

	// Resolve Audit Entity Type
	entityType, err := app.Models.EntityType.GetByName(ctx, "product")
	if err != nil || entityType == nil {
		logger.Warn("Audit entity type 'product' not found, attempting to create...", "error", err)
		entityTypeID, createErr := app.Models.EntityType.CreateIfNotExists(ctx, "product", "Product entity")
		if createErr != nil {
			logger.Error("Failed to create missing entity type", "error", createErr)
			// Allow main operation to succeed
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
			EntityID:     productID.String(),
		}
		if err := app.Models.AuditLog.Insert(ctx, &audit); err != nil {
			// Audit logging failed (partial success)
			logger.Warn("Audit logging failed", "product_id", productID, "error", err)
			app.respondWithJSON(w, http.StatusPartialContent, jsonResponse{
				Error:   false,
				Message: "Product soft deleted, but audit logging failed",
				Data:    productID,
			})
			return
		}
	}

	// Respond success
	logger.Info("Product soft deleted successfully", "product_id", productID)
	app.respondWithJSON(w, http.StatusOK, jsonResponse{
		Error:   false,
		Message: "Product soft deleted successfully",
		Data:    productID,
	})
}


// Brand Handler Functions

// CreateBrandHandler handles the creation of a new brand.
// It enforces permission checks, extracts trusted identifiers from context,
// parses the request payload, validates required fields, performs the creation operation,
// dynamically resolves audit action/entity metadata if missing, and logs the operation.
func (app *Application) CreateBrandHandler(w http.ResponseWriter, r *http.Request) {
	logger := app.Logger.GetLoggerWithContext(r).WithFunctionName("CreateBrandHandler")

	// Derive a bounded context for DB ops and downstream calls.
	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	// Enforce permission.
	if !app.HasPermission(ctx, "create_brand") {
		app.respondWithError(w, errors.New("forbidden: insufficient permissions"), http.StatusForbidden)
		return
	}

	// Extract authenticated user ID (guaranteed uuid.UUID by middleware).
	userID := app.getUserIDFromContext(ctx)
	if userID == nil {
		// Should never happen, but defend in depth.
		app.respondWithError(w, errors.New("missing user ID in context"), http.StatusUnauthorized)
		return
	}

	// Parse request body
	var input struct {
		Name        string  `json:"name"`                   // required
		BrandHandle *string `json:"brand_handle,omitempty"` // optional, globally unique
	}

	if err := app.readJSON(w, r, &input); err != nil {
		app.respondWithError(w, fmt.Errorf("invalid JSON input: %w", err), http.StatusBadRequest)
		return
	}

	// Validate input
	if strings.TrimSpace(input.Name) == "" {
		app.respondWithError(w, errors.New("name is required"), http.StatusBadRequest)
		return
	}

	// Build the Brand entity.
	brand := &data.Brand{
		Name:        strings.TrimSpace(input.Name),
		BrandHandle: input.BrandHandle, // nil‑safe – Insert() will normalise & validate
	}

	// Insert Brand into database
	if err := app.Models.Brand.Insert(ctx, brand); err != nil {
		logger.Error("Failed to insert brand", "error", err)
		app.respondWithError(w, fmt.Errorf("failed to create brand: %w", err), http.StatusInternalServerError)
		return
	}

	// Resolve Audit Action
	action, err := app.Models.Action.GetByName(ctx, "create_brand")
	if err != nil || action == nil {
		logger.Warn("Audit action 'create_brand' not found, attempting to create...", "error", err)
		actionID, createErr := app.Models.Action.CreateIfNotExists(ctx, "create_brand", "Create a new brand")
		if createErr != nil {
			logger.Error("Failed to create missing audit action", "error", createErr)
			// Allow main operation to succeed
		} else {
			action = &data.Action{ID: actionID}
		}
	}

	// Resolve Audit Entity Type
	entityType, err := app.Models.EntityType.GetByName(ctx, "brand")
	if err != nil || entityType == nil {
		logger.Warn("Audit entity type 'brand' not found, attempting to create...", "error", err)
		entityTypeID, createErr := app.Models.EntityType.CreateIfNotExists(ctx, "brand", "Brand entity")
		if createErr != nil {
			logger.Error("Failed to create missing entity type", "error", createErr)
			// Allow main operation to succeed
		} else {
			entityType = &data.EntityType{ID: entityTypeID}
		}
	}

	// Insert Audit Log (fail gracefully if fails)
	if userID != nil && action != nil && entityType != nil {
		audit := data.AuditLog{
			ID:           uuid.New(),
			UserID:       userID,
			ActionID:     action.ID,
			EntityTypeID: entityType.ID,
			EntityID:     brand.ID.String(),
		}
		if err := app.Models.AuditLog.Insert(ctx, &audit); err != nil {
			// Audit logging failed (partial success)
			logger.Warn("Audit logging failed", "brand_id", brand.ID, "error", err)
			app.respondWithJSON(w, http.StatusPartialContent, jsonResponse{
				Error:   false,
				Message: "Brand created, but audit logging failed",
				Data:    brand.ID,
			})
			return
		}
	}

	// Respond success
	logger.Info("Brand created successfully", "brand_id", brand.ID)
	app.respondWithJSON(w, http.StatusCreated, jsonResponse{
		Error:   false,
		Message: "Brand created successfully",
		Data:    brand.ID,
	})
}


// GetBrandByIDHandler handles retrieving a brand by its ID.
// It enforces permission checks, extracts the brand ID from the context,
// retrieves the brand from the database, performs audit logging, and responds with the brand details.
func (app *Application) GetBrandByIDHandler(w http.ResponseWriter, r *http.Request) {
	logger := app.Logger.GetLoggerWithContext(r).WithFunctionName("GetBrandByIDHandler")
	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	// Permission enforcement
	if !app.HasPermission(ctx, "read_brand") {
		app.respondWithError(w, errors.New("forbidden: insufficient permissions"), http.StatusForbidden)
		return
	}

	// Extract Brand ID from context
	brandID := app.getBrandIDFromContext(ctx)
	if brandID == nil {
		app.respondWithError(w, errors.New("missing brand ID in context"), http.StatusBadRequest)
		return
	}

	// Retrieve the brand from the database
	brand, err := app.Models.Brand.GetByID(ctx, *brandID)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			app.respondWithError(w, fmt.Errorf("brand not found"), http.StatusNotFound)
		} else {
			logger.Error("Failed to retrieve brand", "error", err)
			app.respondWithError(w, fmt.Errorf("failed to retrieve brand: %w", err), http.StatusInternalServerError)
		}
		return
	}

	// Resolve Audit Action
	action, err := app.Models.Action.GetByName(ctx, "read_brand")
	if err != nil || action == nil {
		logger.Warn("Audit action 'read_brand' not found, attempting to create...", "error", err)
		actionID, createErr := app.Models.Action.CreateIfNotExists(ctx, "read_brand", "Retrieve a brand by ID")
		if createErr != nil {
			logger.Error("Failed to create missing audit action", "error", createErr)
			// Allow main operation to proceed
		} else {
			action = &data.Action{ID: actionID}
		}
	}

	// Resolve Audit Entity Type
	entityType, err := app.Models.EntityType.GetByName(ctx, "brand")
	if err != nil || entityType == nil {
		logger.Warn("Audit entity type 'brand' not found, attempting to create...", "error", err)
		entityTypeID, createErr := app.Models.EntityType.CreateIfNotExists(ctx, "brand", "Brand entity")
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
			EntityID:     brand.ID.String(),
		}
		if err := app.Models.AuditLog.Insert(ctx, &audit); err != nil {
			// Audit logging failed (partial success)
			logger.Warn("Audit logging failed", "brand_id", brand.ID, "error", err)
			app.respondWithJSON(w, http.StatusPartialContent, jsonResponse{
				Error:   false,
				Message: "Brand retrieved, but audit logging failed",
				Data:    brand,
			})
			return
		}
	}

	// Success
	logger.Info("Successfully retrieved brand", "brand_id", brand.ID)
	app.respondWithJSON(w, http.StatusOK, jsonResponse{
		Error:   false,
		Message: "Brand retrieved successfully",
		Data:    brand,
	})
}


// GetBrandByNameHandler handles retrieving a brand by its name.
// It enforces permission checks, extracts the brand name from the context,
// queries the database for the brand, performs audit logging, and responds with the brand details.
func (app *Application) GetBrandByNameHandler(w http.ResponseWriter, r *http.Request) {
	logger := app.Logger.GetLoggerWithContext(r).WithFunctionName("GetBrandByNameHandler")
	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	// Permission enforcement
	if !app.HasPermission(ctx, "read_brand") {
		app.respondWithError(w, errors.New("forbidden: insufficient permissions"), http.StatusForbidden)
		return
	}

	// Extract Brand Name from context
	brandName := app.getBrandNameFromContext(ctx)
	if brandName == "" {
		app.respondWithError(w, errors.New("missing brand name in context"), http.StatusBadRequest)
		return
	}

	// Retrieve the brand from the database
	brand, err := app.Models.Brand.GetByName(ctx, brandName)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			app.respondWithError(w, fmt.Errorf("brand not found"), http.StatusNotFound)
		} else {
			logger.Error("Failed to retrieve brand", "error", err)
			app.respondWithError(w, fmt.Errorf("failed to retrieve brand: %w", err), http.StatusInternalServerError)
		}
		return
	}

	// Resolve Audit Action
	action, err := app.Models.Action.GetByName(ctx, "read_brand_by_name")
	if err != nil || action == nil {
		logger.Warn("Audit action 'read_brand_by_name' not found, attempting to create...", "error", err)
		actionID, createErr := app.Models.Action.CreateIfNotExists(ctx, "read_brand_by_name", "Retrieve a brand by name")
		if createErr != nil {
			logger.Error("Failed to create missing audit action", "error", createErr)
			// Allow main operation to proceed
		} else {
			action = &data.Action{ID: actionID}
		}
	}

	// Resolve Audit Entity Type
	entityType, err := app.Models.EntityType.GetByName(ctx, "brand")
	if err != nil || entityType == nil {
		logger.Warn("Audit entity type 'brand' not found, attempting to create...", "error", err)
		entityTypeID, createErr := app.Models.EntityType.CreateIfNotExists(ctx, "brand", "Brand entity")
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
			EntityID:     brand.ID.String(),
		}
		if err := app.Models.AuditLog.Insert(ctx, &audit); err != nil {
			// Audit logging failed (partial success)
			logger.Warn("Audit logging failed", "brand_id", brand.ID, "error", err)
			app.respondWithJSON(w, http.StatusPartialContent, jsonResponse{
				Error:   false,
				Message: "Brand retrieved, but audit logging failed",
				Data:    brand,
			})
			return
		}
	}

	// Success
	logger.Info("Successfully retrieved brand", "brand_id", brand.ID)
	app.respondWithJSON(w, http.StatusOK, jsonResponse{
		Error:   false,
		Message: "Brand retrieved successfully",
		Data:    brand,
	})
}


// GetAllBrandsHandler handles retrieving all brands.
// It enforces permission checks, retrieves brands from the database,
// performs structured logging, and logs an audit record for traceability.
func (app *Application) GetAllBrandsHandler(w http.ResponseWriter, r *http.Request) {
	logger := app.Logger.GetLoggerWithContext(r).WithFunctionName("GetAllBrandsHandler")
	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	// Permission enforcement
	if !app.HasPermission(ctx, "read_brands") {
		app.respondWithError(w, errors.New("forbidden: insufficient permissions"), http.StatusForbidden)
		return
	}

	// Pagination params – ?limit & ?offset
	limit, offset, err := app.parseLimitOffset(r)
	if err != nil {
		app.respondWithError(w, err, http.StatusBadRequest)
		return
	}

	// Retrieve all brands from the database
	brands, err := app.Models.Brand.GetAll(ctx, limit, offset)
	if err != nil {
		logger.Error("Failed to retrieve brands", "error", err)
		app.respondWithError(w, fmt.Errorf("failed to retrieve brands: %w", err), http.StatusInternalServerError)
		return
	}

	// Resolve Audit Action
	action, err := app.Models.Action.GetByName(ctx, "read_brands")
	if err != nil || action == nil {
		logger.Warn("Audit action 'read_brands' not found, attempting to create...", "error", err)
		actionID, createErr := app.Models.Action.CreateIfNotExists(ctx, "read_brands", "Retrieve all brands")
		if createErr != nil {
			logger.Error("Failed to create missing audit action", "error", createErr)
			// Allow main operation to succeed
		} else {
			action = &data.Action{ID: actionID}
		}
	}

	// Resolve Audit Entity Type
	entityType, err := app.Models.EntityType.GetByName(ctx, "brand")
	if err != nil || entityType == nil {
		logger.Warn("Audit entity type 'brand' not found, attempting to create...", "error", err)
		entityTypeID, createErr := app.Models.EntityType.CreateIfNotExists(ctx, "brand", "Brand entity")
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
			EntityID:     "all_brands", // Static identifier for the operation
		}
		if err := app.Models.AuditLog.Insert(ctx, &audit); err != nil {
			// Audit logging failed (partial success)
			logger.Warn("Audit logging failed", "error", err)
			app.respondWithJSON(w, http.StatusPartialContent, jsonResponse{
				Error:   false,
				Message: "Brands retrieved, but audit logging failed",
				Data:    brands,
			})
			return
		}
	}

	// Respond success
	logger.Info("Brands retrieved successfully", "count", len(brands))
	app.respondWithJSON(w, http.StatusOK, jsonResponse{
		Error:   false,
		Message: "Brands retrieved successfully",
		Data:    brands,
	})
}


// UpdateBrandHandler handles the update of an existing brand.
// It enforces permission checks, extracts trusted identifiers from context,
// parses the request payload, validates required fields, performs the update operation,
// dynamically resolves audit action/entity metadata if missing, and logs the operation.
func (app *Application) UpdateBrandHandler(w http.ResponseWriter, r *http.Request) {
	logger := app.Logger.WithFunctionName("UpdateBrandHandler")

	// Timeout + permission
	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	if !app.HasPermission(ctx, "update_brand") {
		app.respondWithError(w, errors.New("forbidden: insufficient permissions"), http.StatusForbidden)
		return
	}

	// Brand ID (type-safe)
	brandID, ok := ctx.Value(ctxBrandID).(uuid.UUID)
	if !ok || brandID == uuid.Nil {
		app.respondWithError(w, errors.New("missing or invalid brand ID"), http.StatusBadRequest)
		return
	}

	// Parse JSON (PATCH semantics)
	var input struct {
		Name *string `json:"name"` // optional; only update when present
	}
	if err := app.readJSON(w, r, &input); err != nil {
		app.respondWithError(w, fmt.Errorf("invalid JSON body: %w", err), http.StatusBadRequest)
		return
	}
	if input.Name == nil {
		app.respondWithError(w, errors.New("no updatable fields supplied"), http.StatusBadRequest)
		return
	}

	name := strings.TrimSpace(*input.Name)
	if name == "" {
		app.respondWithError(w, errors.New("brand name cannot be empty"), http.StatusBadRequest)
		return
	}

	// Execute update
	brand := &data.Brand{
		ID:   brandID,
		Name: name,
	}
	if err := app.Models.Brand.Update(ctx, brand); err != nil {
		logger.Error("failed to update brand", "brand_id", brandID, "error", err)
		app.respondWithError(w, errors.New("failed to update brand"), http.StatusInternalServerError)
		return
	}

	// Resolve Audit Action
	action, err := app.Models.Action.GetByName(ctx, "update_brand")
	if err != nil || action == nil {
		logger.Warn("Audit action 'update_brand' not found, attempting to create...", "error", err)
		actionID, createErr := app.Models.Action.CreateIfNotExists(ctx, "update_brand", "Update an existing brand record")
		if createErr != nil {
			logger.Error("Failed to create missing audit action", "error", createErr)
			// Allow main operation to succeed
		} else {
			action = &data.Action{ID: actionID}
		}
	}

	// Resolve Audit Entity Type
	entityType, err := app.Models.EntityType.GetByName(ctx, "brand")
	if err != nil || entityType == nil {
		logger.Warn("Audit entity type 'brand' not found, attempting to create...", "error", err)
		entityTypeID, createErr := app.Models.EntityType.CreateIfNotExists(ctx, "brand", "Brand entity representing a company or product line")
		if createErr != nil {
			logger.Error("Failed to create missing entity type", "error", createErr)
			// Allow main operation to succeed
		} else {
			entityType = &data.EntityType{ID: entityTypeID}
		}
	}

	// Insert Audit Log (fail gracefully if needed)
	userID := app.getUserIDFromContext(ctx)
	if userID != nil && action != nil && entityType != nil {
		audit := data.AuditLog{
			ID:           uuid.New(),
			UserID:       userID,
			ActionID:     action.ID,
			EntityTypeID: entityType.ID,
			EntityID:     brand.ID.String(),
		}
		if err := app.Models.AuditLog.Insert(ctx, &audit); err != nil {
			// Audit logging failed (partial success)
			logger.Warn("Audit logging failed", "brand_id", brand.ID, "error", err)
			app.respondWithJSON(w, http.StatusPartialContent, jsonResponse{
				Error:   false,
				Message: "Brand updated, but audit logging failed",
				Data:    brand.ID,
			})
			return
		}
	}

	// Respond success
	logger.Info("Brand updated successfully", "brand_id", brand.ID)
	app.respondWithJSON(w, http.StatusOK, jsonResponse{
		Error:   false,
		Message: "Brand updated successfully",
		Data:    brand.ID,
	})
}


// SoftDeleteBrandHandler handles the soft deletion of a brand.
// It enforces permission checks, extracts the brand ID from the trusted context,
// marks the brand as deleted in the database, dynamically resolves audit action/entity,
// and performs structured audit logging with fail-safe behavior.
func (app *Application) SoftDeleteBrandHandler(w http.ResponseWriter, r *http.Request) {
	logger := app.Logger.GetLoggerWithContext(r).WithFunctionName("SoftDeleteBrandHandler")
	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	// Permission enforcement
	if !app.HasPermission(ctx, "soft_delete_brand") {
		app.respondWithError(w, errors.New("forbidden: insufficient permissions"), http.StatusForbidden)
		return
	}

	// Extract brand_id from context
	brandID := app.getBrandIDFromContext(ctx)
	if brandID == nil {
		app.respondWithError(w, errors.New("missing brand ID in context"), http.StatusBadRequest)
		return
	}

	// Perform soft delete operation
	if err := app.Models.Brand.SoftDelete(ctx, *brandID); err != nil {
		if strings.Contains(err.Error(), "not found") {
			app.respondWithError(w, fmt.Errorf("brand not found"), http.StatusNotFound)
		} else {
			logger.Error("Failed to soft delete brand", "brand_id", brandID, "error", err)
			app.respondWithError(w, fmt.Errorf("failed to soft delete brand: %w", err), http.StatusInternalServerError)
		}
		return
	}

	// Resolve Audit Action
	action, err := app.Models.Action.GetByName(ctx, "soft_delete_brand")
	if err != nil || action == nil {
		logger.Warn("Audit action 'soft_delete_brand' not found, attempting to create...", "error", err)
		actionID, createErr := app.Models.Action.CreateIfNotExists(ctx, "soft_delete_brand", "Soft delete a brand")
		if createErr != nil {
			logger.Error("Failed to create missing audit action", "error", createErr)
			// Allow main operation to succeed
		} else {
			action = &data.Action{ID: actionID}
		}
	}

	// Resolve Audit Entity Type
	entityType, err := app.Models.EntityType.GetByName(ctx, "brand")
	if err != nil || entityType == nil {
		logger.Warn("Audit entity type 'brand' not found, attempting to create...", "error", err)
		entityTypeID, createErr := app.Models.EntityType.CreateIfNotExists(ctx, "brand", "Brand entity")
		if createErr != nil {
			logger.Error("Failed to create missing entity type", "error", createErr)
			// Allow main operation to succeed
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
			EntityID:     brandID.String(),
		}
		if err := app.Models.AuditLog.Insert(ctx, &audit); err != nil {
			// Audit logging failed (partial success)
			logger.Warn("Audit logging failed", "brand_id", brandID, "error", err)
			app.respondWithJSON(w, http.StatusPartialContent, jsonResponse{
				Error:   false,
				Message: "Brand soft deleted, but audit logging failed",
				Data:    brandID,
			})
			return
		}
	}

	// Respond success
	logger.Info("Brand soft deleted successfully", "brand_id", brandID)
	app.respondWithJSON(w, http.StatusOK, jsonResponse{
		Error:   false,
		Message: "Brand soft deleted successfully",
		Data:    brandID,
	})
}
