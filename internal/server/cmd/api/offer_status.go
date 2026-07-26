// sdworkspace/sdbackend/internal/server/cmd/api/offer_status.go
//
//	Release Class: DEFERRED
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

// CreateOfferStatusHandler handles the creation of a new offer status.
// It enforces permission checks, extracts trusted identifiers from context,
// validates input, performs the creation operation, logs the action, and
// inserts an audit log entry.
func (app *Application) CreateOfferStatusHandler(w http.ResponseWriter, r *http.Request) {
	// Initialize logger with function context
	logger := app.Logger.WithFunctionName("CreateOfferStatusHandler")

	// Set a timeout for the request context
	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	// Permission enforcement
	if !app.HasPermission(ctx, "create_offer_status") {
		app.respondWithError(w, errors.New("forbidden: insufficient permissions"), http.StatusForbidden)
		return
	}

	// Parse request body
	var input struct {
		Name        string `json:"name"`        // Mandatory
		Description string `json:"description"` // Mandatory
	}
	if err := app.readJSON(w, r, &input); err != nil {
		app.respondWithError(w, fmt.Errorf("invalid JSON input: %w", err), http.StatusBadRequest)
		return
	}

	// Validate input
	if strings.TrimSpace(input.Name) == "" || strings.TrimSpace(input.Description) == "" {
		app.respondWithError(w, errors.New("name and description are required"), http.StatusBadRequest)
		return
	}

	// Create OfferStatus object
	offerStatus := &data.OfferStatus{
		Name:        input.Name,
		Description: input.Description,
	}

	// Insert OfferStatus into the database
	if err := app.Models.OfferStatus.Insert(ctx, offerStatus); err != nil {
		logger.Error("Failed to insert offer status", "error", err)
		app.respondWithError(w, fmt.Errorf("failed to create offer status: %w", err), http.StatusInternalServerError)
		return
	}

	// Resolve Audit Action
	action, err := app.Models.Action.GetByName(ctx, "create_offer_status")
	if err != nil || action == nil {
		logger.Warn("Audit action 'create_offer_status' not found, attempting to create...", "error", err)
		actionID, createErr := app.Models.Action.CreateIfNotExists(ctx, "create_offer_status", "Create a new offer status")
		if createErr != nil {
			logger.Error("Failed to create missing audit action", "error", createErr)
			// Allow main operation to proceed
		} else {
			action = &data.Action{ID: actionID}
		}
	}

	// Resolve Audit Entity Type
	entityType, err := app.Models.EntityType.GetByName(ctx, "offer_status")
	if err != nil || entityType == nil {
		logger.Warn("Audit entity type 'offer_status' not found, attempting to create...", "error", err)
		entityTypeID, createErr := app.Models.EntityType.CreateIfNotExists(ctx, "offer_status", "Status of a offer")
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
			EntityID:     offerStatus.ID.String(),
		}
		if err := app.Models.AuditLog.Insert(ctx, &audit); err != nil {
			// Audit logging failed (partial success)
			logger.Warn("Audit logging failed", "ctxOfferStatusID", offerStatus.ID, "error", err)
			app.respondWithJSON(w, http.StatusPartialContent, jsonResponse{
				Error:   false,
				Message: "Offer status created, but audit logging failed",
				Data:    offerStatus.ID,
			})
			return
		}
	}

	// Respond success
	logger.Info("Offer status created successfully", "ctxOfferStatusID", offerStatus.ID)
	app.respondWithJSON(w, http.StatusCreated, jsonResponse{
		Error:   false,
		Message: "Offer status created successfully",
		Data:    offerStatus.ID,
	})
}

// GetOfferStatusByIDHandler handles retrieving the status of a offer by its ID.
// It enforces permission checks, extracts the offer ID from trusted context,
// retrieves the offer status from the database, performs audit logging,
// and responds with the offer status details.
func (app *Application) GetOfferStatusByIDHandler(w http.ResponseWriter, r *http.Request) {
	logger := app.Logger.GetLoggerWithContext(r).WithFunctionName("GetOfferStatusByIDHandler")
	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	// Permission enforcement
	if !app.HasPermission(ctx, "read_offer_status") {
		app.respondWithError(w, errors.New("forbidden: insufficient permissions"), http.StatusForbidden)
		return
	}

	// Extract Offer ID from context (injected via middleware)
	offerID := app.getOfferIDFromContext(ctx)
	if offerID == nil {
		app.respondWithError(w, errors.New("missing offer ID in context"), http.StatusBadRequest)
		return
	}

	// Retrieve the offer status from the database
	offerStatus, err := app.Models.OfferStatus.GetByID(ctx, *offerID)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			app.respondWithError(w, fmt.Errorf("offer status not found"), http.StatusNotFound)
		} else {
			logger.Error("Failed to retrieve offer status", "error", err)
			app.respondWithError(w, fmt.Errorf("failed to retrieve offer status: %w", err), http.StatusInternalServerError)
		}
		return
	}

	// Resolve Audit Action
	action, err := app.Models.Action.GetByName(ctx, "read_offer_status")
	if err != nil || action == nil {
		logger.Warn("Audit action 'read_offer_status' not found, attempting to create...", "error", err)
		actionID, createErr := app.Models.Action.CreateIfNotExists(ctx, "read_offer_status", "Retrieve a offer status by ID")
		if createErr != nil {
			logger.Error("Failed to create missing audit action", "error", createErr)
			// Allow main operation to proceed
		} else {
			action = &data.Action{ID: actionID}
		}
	}

	// Resolve Audit Entity Type
	entityType, err := app.Models.EntityType.GetByName(ctx, "offer_status")
	if err != nil || entityType == nil {
		logger.Warn("Audit entity type 'offer_status' not found, attempting to create...", "error", err)
		entityTypeID, createErr := app.Models.EntityType.CreateIfNotExists(ctx, "offer_status", "Status of a offer")
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
			EntityID:     offerID.String(),
		}
		if err := app.Models.AuditLog.Insert(ctx, &audit); err != nil {
			// Audit logging failed (partial success)
			logger.Warn("Audit logging failed", "ctxOfferID", offerID, "error", err)
			app.respondWithJSON(w, http.StatusPartialContent, jsonResponse{
				Error:   false,
				Message: "Offer status retrieved, but audit logging failed",
				Data: struct {
					OfferID     uuid.UUID         `json:"ctxOfferID"`
					OfferStatus *data.OfferStatus `json:"offer_status"`
				}{
					OfferID:     *offerID,
					OfferStatus: offerStatus,
				},
			})
			return
		}
	}

	// Success
	logger.Info("Successfully retrieved offer status", "ctxOfferID", offerID)
	app.respondWithJSON(w, http.StatusOK, jsonResponse{
		Error:   false,
		Message: "Offer status retrieved successfully",
		Data: struct {
			OfferID     uuid.UUID         `json:"ctxOfferID"`
			OfferStatus *data.OfferStatus `json:"offer_status"`
		}{
			OfferID:     *offerID,
			OfferStatus: offerStatus,
		},
	})
}

// GetAllOfferStatusesHandler retrieves all offer statuses from the database.
// It enforces permission checks, extracts trusted identifiers from context,
// performs the database query, logs the operation, and handles audit logging.
func (app *Application) GetAllOfferStatusesHandler(w http.ResponseWriter, r *http.Request) {
	logger := app.Logger.GetLoggerWithContext(r).WithFunctionName("GetAllOfferStatusesHandler")
	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	// Permission enforcement
	if !app.HasPermission(ctx, "read_offer_statuses") {
		app.respondWithError(w, errors.New("forbidden: insufficient permissions"), http.StatusForbidden)
		return
	}

	// Retrieve all offer statuses from the database
	offerStatuses, err := app.Models.OfferStatus.GetAll(ctx)
	if err != nil {
		logger.Error("Failed to retrieve offer statuses", "error", err)
		app.respondWithError(w, fmt.Errorf("failed to retrieve offer statuses: %w", err), http.StatusInternalServerError)
		return
	}

	// Resolve Audit Action
	action, err := app.Models.Action.GetByName(ctx, "read_offer_statuses")
	if err != nil || action == nil {
		logger.Warn("Audit action 'read_offer_statuses' not found, attempting to create...", "error", err)
		actionID, createErr := app.Models.Action.CreateIfNotExists(ctx, "read_offer_statuses", "Retrieve all offer statuses")
		if createErr != nil {
			logger.Error("Failed to create missing audit action", "error", createErr)
			// Allow main operation to succeed
		} else {
			action = &data.Action{ID: actionID}
		}
	}

	// Resolve Audit Entity Type
	entityType, err := app.Models.EntityType.GetByName(ctx, "offer_status")
	if err != nil || entityType == nil {
		logger.Warn("Audit entity type 'offer_status' not found, attempting to create...", "error", err)
		entityTypeID, createErr := app.Models.EntityType.CreateIfNotExists(ctx, "offer_status", "Status of a offer")
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
			EntityID:     "all_offer_statuses",
		}
		if err := app.Models.AuditLog.Insert(ctx, &audit); err != nil {
			// Audit logging failed (partial success)
			logger.Warn("Audit logging failed", "error", err)
			app.respondWithJSON(w, http.StatusPartialContent, jsonResponse{
				Error:   false,
				Message: "Offer statuses retrieved, but audit logging failed",
				Data: struct {
					OfferStatuses any `json:"offer_statuses"`
				}{
					OfferStatuses: offerStatuses,
				},
			})
			return
		}
	}

	// Success
	logger.Info("Successfully retrieved all offer statuses", "count", len(offerStatuses))
	app.respondWithJSON(w, http.StatusOK, jsonResponse{
		Error:   false,
		Message: "Offer statuses retrieved successfully",
		Data: struct {
			OfferStatuses any `json:"offer_statuses"`
		}{
			OfferStatuses: offerStatuses,
		},
	})
}

// UpdateOfferStatusHandler handles updating the name/status of an existing offer status entry.
// It enforces permission checks, extracts identifiers from trusted context, validates input,
// performs the update operation, and logs the action for audit trail purposes.
func (app *Application) UpdateOfferStatusHandler(w http.ResponseWriter, r *http.Request) {
	logger := app.Logger.GetLoggerWithContext(r).WithFunctionName("UpdateOfferStatusHandler")
	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	// --- Permission check ---
	if !app.HasPermission(ctx, "update_offer_status_entry") {
		app.respondWithError(w, errors.New("forbidden: insufficient permissions"), http.StatusForbidden)
		return
	}

	// --- Trusted ID extraction ---
	offerStatusID := app.getOfferStatusIDFromContext(ctx)
	if offerStatusID == nil {
		app.respondWithError(w, errors.New("missing offer status ID in context"), http.StatusBadRequest)
		return
	}

	// --- Parse and validate input ---
	var input struct {
		Status string `json:"status"` // Required
	}
	if err := app.readJSON(w, r, &input); err != nil {
		app.respondWithError(w, fmt.Errorf("invalid JSON input: %w", err), http.StatusBadRequest)
		return
	}
	if input.Status == "" {
		app.respondWithError(w, errors.New("status cannot be empty"), http.StatusBadRequest)
		return
	}

	// --- Perform update ---
	offerStatus := &data.OfferStatus{
		ID:   *offerStatusID,
		Name: input.Status,
	}
	err := app.Models.OfferStatus.Update(ctx, offerStatus)
	if err != nil {
		if strings.Contains(err.Error(), "no record found") {
			app.respondWithError(w, errors.New("offer status not found"), http.StatusNotFound)
			return
		}
		logger.Error("Failed to update offer status", "offer_status_id", offerStatusID, "error", err)
		app.respondWithError(w, fmt.Errorf("failed to update offer status: %w", err), http.StatusInternalServerError)
		return
	}

	// --- Audit metadata resolution ---
	action, err := app.Models.Action.GetByName(ctx, "update_offer_status_entry")
	if err != nil || action == nil {
		logger.Warn("Audit action 'update_offer_status_entry' not found, creating...", "error", err)
		if id, createErr := app.Models.Action.CreateIfNotExists(ctx, "update_offer_status_entry", "Update a offer status entry"); createErr == nil {
			action = &data.Action{ID: id}
		} else {
			logger.Error("Failed to create audit action", "error", createErr)
		}
	}

	entityType, err := app.Models.EntityType.GetByName(ctx, "offer_status")
	if err != nil || entityType == nil {
		logger.Warn("Audit entity type 'offer_status' not found, creating...", "error", err)
		if id, createErr := app.Models.EntityType.CreateIfNotExists(ctx, "offer_status", "Offer status entity"); createErr == nil {
			entityType = &data.EntityType{ID: id}
		} else {
			logger.Error("Failed to create entity type", "error", createErr)
		}
	}

	// --- Audit logging (fail gracefully) ---
	userID := app.getUserIDFromContext(ctx)
	if userID != nil && action != nil && entityType != nil {
		audit := data.AuditLog{
			ID:           uuid.New(),
			UserID:       userID,
			ActionID:     action.ID,
			EntityTypeID: entityType.ID,
			EntityID:     offerStatusID.String(),
		}
		if err := app.Models.AuditLog.Insert(ctx, &audit); err != nil {
			// Audit logging failed (partial success)
			logger.Warn("Audit logging failed", "offer_status_id", offerStatusID, "error", err)
			app.respondWithJSON(w, http.StatusPartialContent, jsonResponse{
				Error:   false,
				Message: "Offer status updated, but audit logging failed",
				Data: struct {
					OfferStatusID uuid.UUID `json:"offer_status_id"`
					Status        string    `json:"status"`
				}{
					OfferStatusID: *offerStatusID,
					Status:        input.Status,
				},
			})
			return
		}
	}

	// --- Success response ---
	logger.Info("Offer status updated successfully", "offer_status_id", offerStatusID, "status", input.Status)
	app.respondWithJSON(w, http.StatusOK, jsonResponse{
		Error:   false,
		Message: "Offer status updated successfully",
		Data: struct {
			OfferStatusID uuid.UUID `json:"offer_status_id"`
			Status        string    `json:"status"`
		}{
			OfferStatusID: *offerStatusID,
			Status:        input.Status,
		},
	})
}

// DeleteOfferStatusHandler handles the deletion of a offer status.
// It ensures permission checks, extracts trusted identifiers from context,
// performs the delete operation, logs the action, and handles audit logging.
func (app *Application) DeleteOfferStatusHandler(w http.ResponseWriter, r *http.Request) {
	// Initialize logger with function context for structured logging
	logger := app.Logger.GetLoggerWithContext(r).WithFunctionName("DeleteOfferStatusHandler")

	// Set a timeout for the request context to ensure timely execution
	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	// Enforce permission check for deleting a offer status
	if !app.HasPermission(ctx, "delete_offer_status") {
		app.respondWithError(w, errors.New("forbidden: insufficient permissions"), http.StatusForbidden)
		return
	}

	// Extract the offer status ID from the context, injected by middleware
	offerStatusID := app.getOfferStatusIDFromContext(ctx)
	if offerStatusID == nil {
		app.respondWithError(w, errors.New("missing offer status ID in context"), http.StatusBadRequest)
		return
	}

	// Perform the deletion operation in the database
	if err := app.Models.OfferStatus.Delete(ctx, *offerStatusID); err != nil {
		if strings.Contains(err.Error(), "not found") {
			app.respondWithError(w, fmt.Errorf("offer status not found"), http.StatusNotFound)
		} else {
			logger.Error("Failed to delete offer status", "ctxOfferStatusID", offerStatusID, "error", err)
			app.respondWithError(w, fmt.Errorf("failed to delete offer status: %w", err), http.StatusInternalServerError)
		}
		return
	}

	// Resolve Audit Action
	action, err := app.Models.Action.GetByName(ctx, "delete_offer_status")
	if err != nil || action == nil {
		logger.Warn("Audit action 'delete_offer_status' not found, attempting to create...", "error", err)
		actionID, createErr := app.Models.Action.CreateIfNotExists(ctx, "delete_offer_status", "Delete an existing offer status")
		if createErr != nil {
			logger.Error("Failed to create missing audit action", "error", createErr)
			// Allow main operation to proceed
		} else {
			action = &data.Action{ID: actionID}
		}
	}

	// Resolve Audit Entity Type
	entityType, err := app.Models.EntityType.GetByName(ctx, "offer_status")
	if err != nil || entityType == nil {
		logger.Warn("Audit entity type 'offer_status' not found, attempting to create...", "error", err)
		entityTypeID, createErr := app.Models.EntityType.CreateIfNotExists(ctx, "offer_status", "Status of a offer")
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
			EntityID:     offerStatusID.String(),
		}
		if err := app.Models.AuditLog.Insert(ctx, &audit); err != nil {
			// Audit logging failed (partial success)
			logger.Warn("Audit logging failed", "ctxOfferStatusID", offerStatusID, "error", err)
			app.respondWithJSON(w, http.StatusPartialContent, jsonResponse{
				Error:   false,
				Message: "Offer status deleted, but audit logging failed",
				Data: struct {
					OfferStatusID uuid.UUID `json:"ctxOfferStatusID"`
				}{
					OfferStatusID: *offerStatusID,
				},
			})
			return
		}
	}

	// Respond with success
	logger.Info("Offer status deleted successfully", "ctxOfferStatusID", offerStatusID)
	app.respondWithJSON(w, http.StatusOK, jsonResponse{
		Error:   false,
		Message: "Offer status deleted successfully",
		Data: struct {
			OfferStatusID uuid.UUID `json:"ctxOfferStatusID"`
		}{
			OfferStatusID: *offerStatusID,
		},
	})
}

// SubmitOfferStatusForReviewHandler handles submitting a offer status for review.
// It ensures permission enforcement, extracts identifiers from the context, performs the business operation,
// logs structured audit entries, and handles error scenarios gracefully.
func (app *Application) SubmitOfferStatusForReviewHandler(w http.ResponseWriter, r *http.Request) {
	// Initialize logger with function name for structured logging
	logger := app.Logger.GetLoggerWithContext(r).WithFunctionName("SubmitOfferStatusForReviewHandler")

	// Set a timeout for the request context to ensure timely response
	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	// Enforce permission for submitting offer status for review
	if !app.HasPermission(ctx, "submit_offer_status_for_review") {
		app.respondWithError(w, errors.New("forbidden: insufficient permissions"), http.StatusForbidden)
		return
	}

	// Extract Offer Status ID from trusted context
	offerStatusID := app.getOfferStatusIDFromContext(ctx)
	if offerStatusID == nil {
		app.respondWithError(w, errors.New("missing offer status ID in context"), http.StatusBadRequest)
		return
	}

	userID := app.getUserIDFromContext(ctx)
	if userID == nil {
		app.respondWithError(w, errors.New("missing user ID in context"), http.StatusBadRequest)
		return
	}

	// Perform the business operation: Submit the offer status for review
	err := app.Models.OfferStatus.SubmitForReview(ctx, *offerStatusID, *userID)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			app.respondWithError(w, fmt.Errorf("offer status not found"), http.StatusNotFound)
		} else {
			logger.Error("Failed to submit offer status for review", "ctxOfferStatusID", offerStatusID, "error", err)
			app.respondWithError(w, fmt.Errorf("failed to submit offer status for review: %w", err), http.StatusInternalServerError)
		}
		return
	}

	// Resolve Audit Action
	action, err := app.Models.Action.GetByName(ctx, "submit_offer_status_for_review")
	if err != nil || action == nil {
		logger.Warn("Audit action 'submit_offer_status_for_review' not found, attempting to create...", "error", err)
		actionID, createErr := app.Models.Action.CreateIfNotExists(ctx, "submit_offer_status_for_review", "Submit a offer status for review")
		if createErr != nil {
			logger.Error("Failed to create missing audit action", "error", createErr)
			// Allow main operation to proceed
		} else {
			action = &data.Action{ID: actionID}
		}
	}

	// Resolve Audit Entity Type
	entityType, err := app.Models.EntityType.GetByName(ctx, "offer_status")
	if err != nil || entityType == nil {
		logger.Warn("Audit entity type 'offer_status' not found, attempting to create...", "error", err)
		entityTypeID, createErr := app.Models.EntityType.CreateIfNotExists(ctx, "offer_status", "Offer status for review")
		if createErr != nil {
			logger.Error("Failed to create missing entity type", "error", createErr)
			// Allow main operation to proceed
		} else {
			entityType = &data.EntityType{ID: entityTypeID}
		}
	}

	// Insert Audit Log (fail gracefully if audit logging fails)
	if userID != nil && action != nil && entityType != nil {
		audit := data.AuditLog{
			ID:           uuid.New(),
			UserID:       userID,
			ActionID:     action.ID,
			EntityTypeID: entityType.ID,
			EntityID:     offerStatusID.String(),
		}
		if err := app.Models.AuditLog.Insert(ctx, &audit); err != nil {
			// Audit logging failed (partial success)
			logger.Warn("Audit logging failed", "ctxOfferStatusID", offerStatusID, "error", err)
			app.respondWithJSON(w, http.StatusPartialContent, jsonResponse{
				Error:   false,
				Message: "Offer status submitted for review, but audit logging failed",
				Data: struct {
					OfferStatusID uuid.UUID `json:"ctxOfferStatusID"`
				}{
					OfferStatusID: *offerStatusID,
				},
			})
			return
		}
	}

	// Respond with success
	logger.Info("Offer status submitted for review successfully", "ctxOfferStatusID", offerStatusID)
	app.respondWithJSON(w, http.StatusOK, jsonResponse{
		Error:   false,
		Message: "Offer status submitted for review successfully",
		Data: struct {
			OfferStatusID uuid.UUID `json:"ctxOfferStatusID"`
		}{
			OfferStatusID: *offerStatusID,
		},
	})
}
