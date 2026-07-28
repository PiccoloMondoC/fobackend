// Package main provides HTTP handlers for the Platform API.
//
// sdworkspace/sdbackend/internal/server/cmd/api/user_favorites.go
//
// GTM:
//
//	Layer: 2.3 Consumer Domain
//	Release Class: DEFERRED
//	Reason:
//	  User favorites, My Stash behavior, merchant follows, favorite-derived
//	  recommendations, personalized retrieval, sharing, migration, alerts,
//	  and favorite analytics are valid future consumer capabilities, but they
//	  are not required for the initial Platform release spine. The initial
//	  release prioritizes canonical offers, publication governance, commerce
//	  routing, attribution, merchant foundations, and the Future Offering
//	  Platform supported by its Monetization Layer.
//
// DEFERRED Rule:
//
//	Keep compiling.
//	Keep safe.
//	Preserve existing handler names while dependent package code is retired
//	or migrated.
//	Preserve authenticated user ownership for consumer favorite mutations.
//	Preserve trusted-context identifier extraction.
//	Preserve centralized data-layer lifecycle and sentinel-error contracts.
//	Do not recreate handler-local persistence contracts.
//	Do not add new favorite, My Stash, merchant-follow, recommendation,
//	sharing, alert, migration, or favorite-analytics workflows.
//	Do not expose deferred user-favorite workflows in the v1 router.
//	Return explicit deferred responses where legacy handlers must temporarily
//	remain registered.
//	Do not block deployment on this file unless it breaks compilation or
//	compromises a SPINE-dependent package.
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

// User Favorites

// CreateUserFavoriteHandler handles the creation of a user favorite offer.
// It enforces permission checks, extracts trusted identifiers from context,
// performs the database operation, logs the action, and handles audit logging.
func (app *Application) CreateUserFavoriteHandler(w http.ResponseWriter, r *http.Request) {
	logger := app.Logger.GetLoggerWithContext(r).WithFunctionName("CreateUserFavoriteHandler")
	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	// Permission enforcement
	if !app.HasPermission(ctx, "create_user_favorite") {
		app.respondWithError(w, errors.New("forbidden: insufficient permissions"), http.StatusForbidden)
		return
	}

	// Extract User ID from context
	userID := app.getUserIDFromContext(ctx)
	if userID == nil {
		app.respondWithError(w, errors.New("missing user ID in context"), http.StatusUnauthorized)
		return
	}

	// Extract Offer ID from context
	offerID := app.getOfferIDFromContext(ctx)
	if offerID == nil {
		app.respondWithError(w, errors.New("missing offer ID in context"), http.StatusBadRequest)
		return
	}

	// Build the UserFavorite object
	userFavorite := &data.UserFavorite{
		UserID:  *userID,
		OfferID: *offerID,
	}

	// Insert UserFavorite into the database
	if err := app.Models.UserFavorite.Insert(ctx, userFavorite); err != nil {
		logger.Error("Failed to insert user favorite", "error", err)
		app.respondWithError(w, fmt.Errorf("failed to create user favorite: %w", err), http.StatusInternalServerError)
		return
	}

	// Resolve Audit Action
	action, err := app.Models.Action.GetByName(ctx, "create_user_favorite")
	if err != nil || action == nil {
		logger.Warn("Audit action 'create_user_favorite' not found, attempting to create...", "error", err)
		actionID, createErr := app.Models.Action.CreateIfNotExists(ctx, "create_user_favorite", "Create a new user favorite")
		if createErr != nil {
			logger.Error("Failed to create missing audit action", "error", createErr)
			// Allow main operation to succeed
		} else {
			action = &data.Action{ID: actionID}
		}
	}

	// Resolve Audit Entity Type
	entityType, err := app.Models.EntityType.GetByName(ctx, "user_favorite")
	if err != nil || entityType == nil {
		logger.Warn("Audit entity type 'user_favorite' not found, attempting to create...", "error", err)
		entityTypeID, createErr := app.Models.EntityType.CreateIfNotExists(ctx, "user_favorite", "User favorite offer")
		if createErr != nil {
			logger.Error("Failed to create missing entity type", "error", createErr)
			// Allow main operation to succeed
		} else {
			entityType = &data.EntityType{ID: entityTypeID}
		}
	}

	// Insert Audit Log (fail gracefully if audit logging fails)
	if action != nil && entityType != nil {
		audit := data.AuditLog{
			ID:           uuid.New(),
			UserID:       userID,
			ActionID:     action.ID,
			EntityTypeID: entityType.ID,
			EntityID:     userFavorite.ID.String(),
		}
		if err := app.Models.AuditLog.Insert(ctx, &audit); err != nil {
			// Audit logging failed (partial success)
			logger.Warn("Audit logging failed", "ctxUserFavoriteID", userFavorite.ID, "error", err)
			app.respondWithJSON(w, http.StatusPartialContent, jsonResponse{
				Error:   false,
				Message: "User favorite created, but audit logging failed",
				Data:    userFavorite.ID,
			})
			return
		}
	}

	// Respond success
	logger.Info("User favorite created successfully", "ctxUserFavoriteID", userFavorite.ID)
	app.respondWithJSON(w, http.StatusCreated, jsonResponse{
		Error:   false,
		Message: "User favorite created successfully",
		Data:    userFavorite.ID,
	})
}

// GetUserFavoriteByIDHandler retrieves a single user favorite by its ID.
func (app *Application) GetUserFavoriteByIDHandler(w http.ResponseWriter, r *http.Request) {
	logger := app.Logger.GetLoggerWithContext(r).WithFunctionName("GetUserFavoriteByIDHandler")
	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	// Permission enforcement
	if !app.HasPermission(ctx, "read_user_favorite") {
		app.respondWithError(w, errors.New("forbidden: insufficient permissions"), http.StatusForbidden)
		return
	}

	// Extract User Favorite ID from context
	userFavoriteID := app.getUserFavoriteIDFromContext(ctx)
	if userFavoriteID == nil {
		app.respondWithError(w, errors.New("missing ctxUserFavoriteID in context"), http.StatusBadRequest)
		return
	}

	// Retrieve the user favorite from the database
	userFavorite, err := app.Models.UserFavorite.GetByID(ctx, *userFavoriteID)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			app.respondWithError(w, fmt.Errorf("user favorite not found"), http.StatusNotFound)
		} else {
			logger.Error("Failed to retrieve user favorite", "error", err)
			app.respondWithError(w, fmt.Errorf("failed to retrieve user favorite: %w", err), http.StatusInternalServerError)
		}
		return
	}

	// Resolve Audit Action
	action, err := app.Models.Action.GetByName(ctx, "read_user_favorite")
	if err != nil || action == nil {
		logger.Warn("Audit action 'read_user_favorite' not found, attempting to create...", "error", err)
		actionID, createErr := app.Models.Action.CreateIfNotExists(ctx, "read_user_favorite", "Retrieve a user favorite by ID")
		if createErr != nil {
			logger.Error("Failed to create missing audit action", "error", createErr)
			// Allow main operation to proceed
		} else {
			action = &data.Action{ID: actionID}
		}
	}

	// Resolve Audit Entity Type
	entityType, err := app.Models.EntityType.GetByName(ctx, "user_favorite")
	if err != nil || entityType == nil {
		logger.Warn("Audit entity type 'user_favorite' not found, attempting to create...", "error", err)
		entityTypeID, createErr := app.Models.EntityType.CreateIfNotExists(ctx, "user_favorite", "User favorite item")
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
			EntityID:     userFavorite.ID.String(),
		}
		if err := app.Models.AuditLog.Insert(ctx, &audit); err != nil {
			// Audit logging failed (partial success)
			logger.Warn("Audit logging failed", "ctxUserFavoriteID", userFavorite.ID, "error", err)
			app.respondWithJSON(w, http.StatusPartialContent, jsonResponse{
				Error:   false,
				Message: "User favorite retrieved, but audit logging failed",
				Data:    userFavorite,
			})
			return
		}
	}

	// Respond with the user favorite
	logger.Info("Successfully retrieved user favorite", "ctxUserFavoriteID", userFavorite.ID)
	app.respondWithJSON(w, http.StatusOK, jsonResponse{
		Error:   false,
		Message: "User favorite retrieved successfully",
		Data:    userFavorite,
	})
}

// GetUserFavoriteByUserIDHandler handles retrieving a user's favorite items by their user ID.
// It enforces permission checks, extracts the user ID from the context, retrieves the favorite items from the database,
// performs audit logging, and responds with the favorite items.
func (app *Application) GetUserFavoriteByUserIDHandler(w http.ResponseWriter, r *http.Request) {
	logger := app.Logger.GetLoggerWithContext(r).WithFunctionName("GetUserFavoriteByUserIDHandler")
	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	// Permission enforcement
	if !app.HasPermission(ctx, "read_user_favorites") {
		app.respondWithError(w, errors.New("forbidden: insufficient permissions"), http.StatusForbidden)
		return
	}

	// Extract authenticated UserID from context
	userID := app.getUserIDFromContext(ctx)
	if userID == nil {
		app.respondWithError(w, errors.New("missing user ID in context"), http.StatusUnauthorized)
		return
	}

	// Retrieve user's favorite items from the database
	favorites, err := app.Models.UserFavorite.GetByUserID(ctx, *userID)
	if err != nil {
		logger.Error("Failed to retrieve user favorites", "ctxUserID", userID, "error", err)
		app.respondWithError(w, fmt.Errorf("failed to retrieve user favorites: %w", err), http.StatusInternalServerError)
		return
	}

	// Resolve Audit Action
	action, err := app.Models.Action.GetByName(ctx, "read_user_favorites")
	if err != nil || action == nil {
		logger.Warn("Audit action 'read_user_favorites' not found, attempting to create...", "error", err)
		actionID, createErr := app.Models.Action.CreateIfNotExists(ctx, "read_user_favorites", "Retrieve user favorites by user ID")
		if createErr != nil {
			logger.Error("Failed to create missing audit action", "error", createErr)
			// Allow main operation to proceed
		} else {
			action = &data.Action{ID: actionID}
		}
	}

	// Resolve Audit Entity Type
	entityType, err := app.Models.EntityType.GetByName(ctx, "user_favorite")
	if err != nil || entityType == nil {
		logger.Warn("Audit entity type 'user_favorite' not found, attempting to create...", "error", err)
		entityTypeID, createErr := app.Models.EntityType.CreateIfNotExists(ctx, "user_favorite", "User favorite items")
		if createErr != nil {
			logger.Error("Failed to create missing entity type", "error", createErr)
			// Allow main operation to proceed
		} else {
			entityType = &data.EntityType{ID: entityTypeID}
		}
	}

	// Insert Audit Log (fail gracefully if audit logging fails)
	if action != nil && entityType != nil {
		audit := data.AuditLog{
			ID:           uuid.New(),
			UserID:       userID,
			ActionID:     action.ID,
			EntityTypeID: entityType.ID,
			EntityID:     userID.String(), // EntityID here is the UserID we retrieved favorites for
		}
		if err := app.Models.AuditLog.Insert(ctx, &audit); err != nil {
			// Audit logging failed (partial success)
			logger.Warn("Audit logging failed", "ctxUserID", userID, "error", err)
			app.respondWithJSON(w, http.StatusPartialContent, jsonResponse{
				Error:   false,
				Message: "User favorites retrieved, but audit logging failed",
				Data: struct {
					CtxUserID *uuid.UUID `json:"ctxUserID"`
					Favorites any        `json:"favorites"`
				}{
					CtxUserID: userID,
					Favorites: favorites,
				},
			})
			return
		}
	}

	// Success response
	logger.Info("Retrieved user favorites successfully", "ctxUserID", userID, "count", len(favorites))
	app.respondWithJSON(w, http.StatusOK, jsonResponse{
		Error:   false,
		Message: "User favorites retrieved successfully",
		Data: struct {
			CtxUserID *uuid.UUID `json:"ctxUserID"`
			Favorites any        `json:"favorites"`
		}{
			CtxUserID: userID,
			Favorites: favorites,
		},
	})
}

/*
// GetActiveUserFavoritesByUserIDHandler retrieves all active favorites for a specific user.
// - Allows users to view their own favorites without needing special permissions.
// - Requires internal users (admin/internal_operator) to have explicit permission.
// - Performs full audit logging with fallback creation of action/entity type metadata.
func (app *Application) GetActiveUserFavoritesByUserIDHandler(w http.ResponseWriter, r *http.Request) {
	logger := app.Logger.GetLoggerWithContext(r).WithFunctionName("GetActiveUserFavoritesByUserIDHandler")
	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	// --- Parse input (UserID expected in body, e.g., { "user_id": "<uuid>" }) ---
	var input struct {
		UserID uuid.UUID `json:"user_id"`
	}
	if err := app.readJSON(w, r, &input); err != nil {
		app.respondWithError(w, err, http.StatusBadRequest)
		return
	}

	// --- Extract authenticated UserID from JWT-injected context ---
	userID := app.getUserIDFromContext(ctx)
	if userID == nil {
		app.respondWithError(w, errors.New("unauthorized: login required"), http.StatusUnauthorized)
		return
	}

	// --- Access Rules ---
	isSelf := input.UserID == *userID
	isAdmin := app.HasRole(ctx, "admin")
	isOperator := app.HasRole(ctx, "internal_operator")
	isInternal := isAdmin || isOperator

	if !isSelf {
		// Block non-internal users from accessing others' favorites
		if !isInternal {
			app.respondWithError(w, errors.New("forbidden: cannot view other users' favorites"), http.StatusForbidden)
			return
		}
		// Internal users must have permission
		if !app.HasPermission(ctx, "read_user_favorites") {
			app.respondWithError(w, errors.New("forbidden: insufficient permissions"), http.StatusForbidden)
			return
		}
	} else {
		// For self, override to prevent tampering
		input.UserID = *userID
	}

	// --- Query active favorites from database ---
	activeFavorites, err := app.Models.UserFavorites.GetActiveByUserID(ctx, input.UserID)
	if err != nil {
		logger.Error("Failed to retrieve active user favorites", "ctxUserID", userID, "error", err)
		app.respondWithError(w, fmt.Errorf("failed to retrieve active user favorites: %w", err), http.StatusInternalServerError)
		return
	}

	// --- Resolve Audit Action (create if missing) ---
	action, err := app.Models.Action.GetByName(ctx, "read_user_favorites")
	if err != nil || action == nil {
		logger.Warn("Audit action 'read_user_favorites' not found, attempting to create...", "error", err)
		actionID, createErr := app.Models.Action.CreateIfNotExists(ctx, "read_user_favorites", "Retrieve active user favorites")
		if createErr != nil {
			logger.Error("Failed to create missing audit action", "error", createErr)
		} else {
			action = &data.Action{ID: actionID}
		}
	}

	// --- Resolve Audit Entity Type (create if missing) ---
	entityType, err := app.Models.EntityType.GetByName(ctx, "user_favorite")
	if err != nil || entityType == nil {
		logger.Warn("Audit entity type 'user_favorite' not found, attempting to create...", "error", err)
		entityTypeID, createErr := app.Models.EntityType.CreateIfNotExists(ctx, "user_favorite", "User favorite offers or items")
		if createErr != nil {
			logger.Error("Failed to create missing entity type", "error", createErr)
		} else {
			entityType = &data.EntityType{ID: entityTypeID}
		}
	}

	// --- Insert Audit Log (failsafe) ---
	if userID != nil && action != nil && entityType != nil {
		audit := data.AuditLog{
			ID:           uuid.New(),
			UserID:       userID,
			ActionID:     action.ID,
			EntityTypeID: entityType.ID,
			EntityID:     input.UserID.String(), // Track whose favorites were read
		}
		if err := app.Models.AuditLog.Insert(ctx, &audit); err != nil {
			logger.Warn("Audit logging failed", "ctxUserID", userID, "error", err)
			// Continue gracefully
		}
	}

	// --- Success Response ---
	logger.Info("Retrieved active user favorites successfully", "ctxUserID", userID, "count", len(activeFavorites))
	app.respondWithJSON(w, http.StatusOK, map[string]any{
		"ctxUserID": userID,
		"favorites": activeFavorites,
		"count":     len(activeFavorites),
	})
}


// SaveUserFavoriteHandler allows a logged-in user to favorite a offer.
//
// This is a strictly user-facing endpoint:
// - Only accessible to authenticated users
// - Validates and extracts the user ID from JWT-injected context
// - Requires "manage_favorites" permission
// - Uses soft-save logic (re-activates if previously deleted)
// - Logs and audits the action
func (app *Application) SaveUserFavoriteHandler(w http.ResponseWriter, r *http.Request) {
	logger := app.Logger.GetLoggerWithContext(r).WithFunctionName("SaveUserFavoriteHandler")
	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	// --- Extract user ID from context (injected by JWT middleware) ---
	userID := getUserIDFromContext(ctx)
	if userID == uuid.Nil {
		app.respondWithError(w, errors.New("unauthorized: login required"), http.StatusUnauthorized)
		return
	}

	// --- Extract offer ID from URL ---
	offerIDStr := chi.URLParam(r, "offerID")
	offerID, err := uuid.Parse(offerIDStr)
	if err != nil || offerID == uuid.Nil {
		app.respondWithError(w, errors.New("invalid or missing offer ID"), http.StatusBadRequest)
		return
	}

	// --- Save favorite ---
	err = app.Models.UserFavorite.SaveFavorite(ctx, userID, offerID)
	if err != nil {
		logger.Error("Failed to save favorite", "userID", userID, "offerID", offerID, "error", err)
		app.respondWithError(w, fmt.Errorf("failed to save favorite: %w", err), http.StatusInternalServerError)
		return
	}

	// --- Resolve or create audit metadata ---
	actionID, _ := app.resolveOrCreateAuditAction(ctx, logger, "save_user_favorite", "User saves a favorite offer")
	entityTypeID, _ := app.resolveOrCreateEntityType(ctx, logger, "user_favorite", "User's favorite offer")

	// --- Insert audit log (fail-safe) ---
	if actionID != nil && entityTypeID != nil {
		audit := data.AuditLog{
			ID:           uuid.New(),
			UserID:       &userID,
			ActionID:     actionID,
			EntityTypeID: *entityTypeID,
			EntityID:     offerID.String(),
			Timestamp:    timeutil.Now(),
		}
		if err := app.Models.AuditLog.Insert(ctx, &audit); err != nil {
			logger.Warn("Audit logging failed", "userID", userID, "offerID", offerID, "error", err)
			app.respondWithJSON(w, http.StatusPartialContent, map[string]any{
				"status":  "saved",
				"offer_id": offerID,
				"note":    "audit logging failed",
			})
			return
		}
	}

	// --- Success response ---
	logger.Info("User favorite saved", "userID", userID, "offerID", offerID)
	app.respondWithJSON(w, http.StatusOK, map[string]any{
		"status":  "saved",
		"offer_id": offerID,
	})
}
*/

// UnsaveUserFavoriteHandler soft-deletes the authenticated user's active
// favorite mapping for the trusted offer.
//
// This handler remains compile-safe while user-favorite workflows are deferred
// and must not be registered in the v1 router.
func (app *Application) UnsaveUserFavoriteHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	logger := app.Logger.
		GetLoggerWithContext(r).
		WithFunctionName("UnsaveUserFavoriteHandler")

	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	userID := app.getUserIDFromContext(ctx)
	if userID == nil || *userID == uuid.Nil {
		app.respondWithError(
			w,
			errors.New("unauthorized: login required"),
			http.StatusUnauthorized,
		)
		return
	}

	offerID := app.getOfferIDFromContext(ctx)
	if offerID == nil || *offerID == uuid.Nil {
		app.respondWithError(
			w,
			errors.New("bad request: missing offer ID in context"),
			http.StatusBadRequest,
		)
		return
	}

	if err := app.Models.UserFavorite.UnsaveFavorite(
		ctx,
		*userID,
		*offerID,
	); err != nil {
		if errors.Is(err, data.ErrUserFavoriteNotFound) {
			app.respondWithError(
				w,
				errors.New("favorite not found"),
				http.StatusNotFound,
			)
			return
		}

		logger.Error(
			"Failed to unsave user favorite",
			"error", err,
			"user_id", *userID,
			"offer_id", *offerID,
		)

		app.respondWithError(
			w,
			fmt.Errorf("failed to unsave favorite: %w", err),
			http.StatusInternalServerError,
		)
		return
	}

	action, err := app.Models.Action.GetByName(ctx, "unsave_user_favorite")
	if err != nil || action == nil {
		logger.Warn(
			"Audit action not found; attempting creation",
			"action", "unsave_user_favorite",
			"error", err,
		)

		actionID, createErr := app.Models.Action.CreateIfNotExists(
			ctx,
			"unsave_user_favorite",
			"Remove a user favorite offer",
		)
		if createErr != nil {
			logger.Error(
				"Failed to create audit action",
				"error", createErr,
			)
		} else {
			action = &data.Action{ID: actionID}
		}
	}

	entityType, err := app.Models.EntityType.GetByName(ctx, "user_favorite")
	if err != nil || entityType == nil {
		logger.Warn(
			"Audit entity type not found; attempting creation",
			"entity_type", "user_favorite",
			"error", err,
		)

		entityTypeID, createErr := app.Models.EntityType.CreateIfNotExists(
			ctx,
			"user_favorite",
			"User favorite offer",
		)
		if createErr != nil {
			logger.Error(
				"Failed to create audit entity type",
				"error", createErr,
			)
		} else {
			entityType = &data.EntityType{ID: entityTypeID}
		}
	}

	if action != nil && entityType != nil {
		audit := data.AuditLog{
			ID:           uuid.New(),
			UserID:       userID,
			ActionID:     action.ID,
			EntityTypeID: entityType.ID,
			EntityID:     offerID.String(),
		}

		if err := app.Models.AuditLog.Insert(ctx, &audit); err != nil {
			logger.Warn(
				"Audit logging failed after user favorite unsave",
				"error", err,
				"user_id", *userID,
				"offer_id", *offerID,
			)
		}
	}

	logger.Info(
		"User favorite unsaved successfully",
		"user_id", *userID,
		"offer_id", *offerID,
	)

	app.respondWithJSON(w, http.StatusOK, jsonResponse{
		Error:   false,
		Message: "User favorite unsaved successfully",
		Data: struct {
			OfferID uuid.UUID `json:"offer_id"`
			Status  string    `json:"status"`
		}{
			OfferID: *offerID,
			Status:  "unsaved",
		},
	})
}

/*
// ShareOfferHandler generates a shareable link for a user to send a offer to friends via
// email, SMS, or social media. It validates the input, logs the process, and
// returns a formatted shareable URL.
func (app *Application) ShareOfferHandler(w http.ResponseWriter, r *http.Request) {
	logger := app.Logger.GetLoggerWithContext(r).WithFunctionName("ShareOfferHandler")
	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	// --- Extract User ID from context (injected by JWT middleware) ---
	userID := app.getUserIDFromContext(ctx)
	if userID == nil {
		app.respondWithError(w, errors.New("unauthorized: login required"), http.StatusUnauthorized)
		return
	}

	// Extract Offer ID from trusted context
	offerID := app.getOfferIDFromContext(ctx)
	if offerID == nil {
		app.respondWithError(w, errors.New("missing offer ID in context"), http.StatusBadRequest)
		return
	}

	// Parse request body for share details
	var input struct {
		Platform string `json:"platform"` // Mandatory
		Message  string `json:"message"`  // Optional
	}

	if err := app.readJSON(w, r, &input); err != nil {
		app.respondWithError(w, fmt.Errorf("invalid JSON input: %w", err), http.StatusBadRequest)
		return
	}

	// Perform the share operation (e.g., log the share action in the database)
	share := &data.OfferShare{
		UserID:    *userID,
		OfferID:    *offerID,
		Platform:  input.Platform,
		Message:   input.Message,
		Timestamp: timeutil.Now(),
	}

	if err := app.Models.OfferShare.Insert(ctx, share); err != nil {
		logger.Error("Failed to share offer", "error", err)
		app.respondWithError(w, fmt.Errorf("failed to share offer: %w", err), http.StatusInternalServerError)
		return
	}

	// Resolve Audit Action
	action, err := app.Models.Action.GetByName(ctx, "share_offer")
	if err != nil || action == nil {
		logger.Warn("Audit action 'share_offer' not found, attempting to create...", "error", err)
		actionID, createErr := app.Models.Action.CreateIfNotExists(ctx, "share_offer", "Share a offer")
		if createErr != nil {
			logger.Error("Failed to create missing audit action", "error", createErr)
			// Allow main operation to succeed
		} else {
			action = &data.Action{ID: actionID}
		}
	}

	// Resolve Audit Entity Type
	entityType, err := app.Models.EntityType.GetByName(ctx, "offer_share")
	if err != nil || entityType == nil {
		logger.Warn("Audit entity type 'offer_share' not found, attempting to create...", "error", err)
		entityTypeID, createErr := app.Models.EntityType.CreateIfNotExists(ctx, "offer_share", "Sharing of a offer")
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
			EntityID:     share.ID.String(),
		}
		if err := app.Models.AuditLog.Insert(ctx, &audit); err != nil {
			logger.Warn("Audit logging failed", "ctxOfferShareID", share.ID, "error", err)
			app.respondWithJSON(w, http.StatusPartialContent, map[string]any{
				"ctxOfferShareID": share.ID,
				"error":         "Offer shared, but audit logging failed",
			})
			return
		}
	}

	// Respond success
	logger.Info("Offer shared successfully", "ctxOfferShareID", share.ID)
	app.respondWithJSON(w, http.StatusOK, map[string]any{
		"ctxOfferShareID": share.ID,
	})
}


// SetOfferAlertHandler handles setting an alert for a specific offer.
// It ensures permission enforcement, extracts trusted identifiers from context,
// performs the alert setup operation, logs audit events, and returns a response.
func (app *Application) SetOfferAlertHandler(w http.ResponseWriter, r *http.Request) {
	logger := app.Logger.GetLoggerWithContext(r).WithFunctionName("SetOfferAlertHandler")
	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	// --- Extract User ID from context (injected by JWT middleware) ---
	userID := app.getUserIDFromContext(ctx)
	if userID == nil {
		app.respondWithError(w, errors.New("unauthorized: login required"), http.StatusUnauthorized)
		return
	}

	// Extract Offer ID from context (middleware-injected)
	offerID := app.getOfferIDFromContext(ctx)
	if offerID == nil {
		app.respondWithError(w, errors.New("missing offer ID in context"), http.StatusBadRequest)
		return
	}

	// Parse request body
	var input struct {
		AlertThreshold float64 `json:"alert_threshold"` // Mandatory
	}

	if err := app.readJSON(w, r, &input); err != nil {
		app.respondWithError(w, fmt.Errorf("invalid JSON input: %w", err), http.StatusBadRequest)
		return
	}

	// Perform the alert setup operation
	err := app.Models.OfferAlert.SetAlert(ctx, *userID, *offerID, input.AlertThreshold)
	if err != nil {
		logger.Error("Failed to set offer alert", "ctxUserID", userID, "ctxOfferID", offerID, "error", err)
		app.respondWithError(w, fmt.Errorf("failed to set offer alert: %w", err), http.StatusInternalServerError)
		return
	}

	// Resolve Audit Action
	action, err := app.Models.Action.GetByName(ctx, "set_offer_alert")
	if err != nil || action == nil {
		logger.Warn("Audit action 'set_offer_alert' not found, attempting to create...", "error", err)
		actionID, createErr := app.Models.Action.CreateIfNotExists(ctx, "set_offer_alert", "Set an alert for a offer")
		if createErr != nil {
			logger.Error("Failed to create missing audit action", "error", createErr)
			// Allow main operation to succeed
		} else {
			action = &data.Action{ID: actionID}
		}
	}

	// Resolve Audit Entity Type
	entityType, err := app.Models.EntityType.GetByName(ctx, "offer_alert")
	if err != nil || entityType == nil {
		logger.Warn("Audit entity type 'offer_alert' not found, attempting to create...", "error", err)
		entityTypeID, createErr := app.Models.EntityType.CreateIfNotExists(ctx, "offer_alert", "Alert set for a offer")
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
			EntityID:     offerID.String(),
		}
		if err := app.Models.AuditLog.Insert(ctx, &audit); err != nil {
			logger.Warn("Audit logging failed", "ctxOfferID", offerID, "error", err)
			app.respondWithJSON(w, http.StatusPartialContent, map[string]any{
				"ctxOfferID": offerID,
				"error":   "Offer alert set, but audit logging failed",
			})
			return
		}
	}

	// Respond success
	logger.Info("Offer alert set successfully", "ctxOfferID", offerID)
	app.respondWithJSON(w, http.StatusOK, map[string]any{
		"ctxOfferID": offerID,
	})
}


// RequestRestockNotificationHandler handles requests for restock notifications.
// It ensures permission checks, extracts identifiers from trusted context,
// performs the business operation of requesting a restock notification,
// logs the operation, and handles audit logging.
func (app *Application) RequestRestockNotificationHandler(w http.ResponseWriter, r *http.Request) {
	logger := app.Logger.GetLoggerWithContext(r).WithFunctionName("RequestRestockNotificationHandler")
	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	// --- Extract User ID from context (injected by JWT middleware) ---
	userID := app.getUserIDFromContext(ctx)
	if userID == nil {
		app.respondWithError(w, errors.New("unauthorized: login required"), http.StatusUnauthorized)
		return
	}

	// Extract Product ID from trusted context
	productID := app.getProductIDFromContext(ctx)
	if productID == nil {
		app.respondWithError(w, errors.New("missing product ID in context"), http.StatusBadRequest)
		return
	}

	// Perform the business operation to request a restock notification
	err := app.Models.RestockNotification.Request(ctx, *productID, *userID)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			app.respondWithError(w, errors.New("product not found"), http.StatusNotFound)
		} else {
			logger.Error("Failed to request restock notification", "ctxProductID", productID, "error", err)
			app.respondWithError(w, errors.New("failed to request restock notification"), http.StatusInternalServerError)
		}
		return
	}

	// Resolve Audit Action
	action, err := app.Models.Action.GetByName(ctx, "request_restock_notification")
	if err != nil || action == nil {
		logger.Warn("Audit action 'request_restock_notification' not found, attempting to create...", "error", err)
		actionID, createErr := app.Models.Action.CreateIfNotExists(ctx, "request_restock_notification", "Request a restock notification")
		if createErr != nil {
			logger.Error("Failed to create missing audit action", "error", createErr)
			// Allow main operation to succeed
		} else {
			action = &data.Action{ID: actionID}
		}
	}

	// Resolve Audit Entity Type
	entityType, err := app.Models.EntityType.GetByName(ctx, "restock_notification")
	if err != nil || entityType == nil {
		logger.Warn("Audit entity type 'restock_notification' not found, attempting to create...", "error", err)
		entityTypeID, createErr := app.Models.EntityType.CreateIfNotExists(ctx, "restock_notification", "Notification request for product restock")
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
			EntityID:     productID.String(),
		}
		if err := app.Models.AuditLog.Insert(ctx, &audit); err != nil {
			logger.Warn("Audit logging failed", "ctxProductID", productID, "error", err)
			app.respondWithJSON(w, http.StatusPartialContent, map[string]any{
				"ctxProductID": productID,
				"error":      "Restock notification requested, but audit logging failed",
			})
			return
		}
	}

	// Respond success
	logger.Info("Restock notification requested successfully", "ctxProductID", productID)
	app.respondWithJSON(w, http.StatusOK, map[string]any{
		"ctxProductID": productID,
		"status":     "notification_requested",
	})
}


// GetUserOfferPurchaseHistoryHandler handles retrieving the list of offers a user has marked as purchased.
// It enforces permission checks, extracts the user ID from trusted context, retrieves the purchase history from the database,
// performs audit logging, and responds with the result.
func (app *Application) GetUserOfferPurchaseHistoryHandler(w http.ResponseWriter, r *http.Request) {
	logger := app.Logger.GetLoggerWithContext(r).WithFunctionName("GetUserOfferPurchaseHistoryHandler")
	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	// Enforce required permission
	if !app.HasPermission(ctx, "read_user_offer_purchase_history") {
		app.respondWithError(w, errors.New("forbidden: insufficient permissions"), http.StatusForbidden)
		return
	}

	// Extract user ID from trusted context
	userID := app.getUserIDFromContext(ctx)
	if userID == nil {
		app.respondWithError(w, errors.New("missing user ID in context"), http.StatusBadRequest)
		return
	}

	// Retrieve purchase history for the user
	purchaseHistory, err := app.Models.UserFavorites.GetPurchaseHistory(ctx, *userID, 100) // Limit is hardcoded to 100 for now
	if err != nil {
		logger.Error("Failed to retrieve user offer purchase history", "ctxUserID", userID, "error", err)
		app.respondWithError(w, fmt.Errorf("failed to retrieve purchase history: %w", err), http.StatusInternalServerError)
		return
	}

	// Resolve audit action (or create it if missing)
	action, err := app.Models.Action.GetByName(ctx, "read_user_offer_purchase_history")
	if err != nil || action == nil {
		logger.Warn("Audit action 'read_user_offer_purchase_history' missing, attempting creation", "error", err)
		actionID, createErr := app.Models.Action.CreateIfNotExists(ctx, "read_user_offer_purchase_history", "Retrieve the purchase history of offers for a user")
		if createErr != nil {
			logger.Error("Failed to create audit action", "error", createErr)
			// Allow main operation to succeed
		} else {
			action = &data.Action{ID: actionID}
		}
	}

	// Resolve audit entity type (or create it if missing)
	entityType, err := app.Models.EntityType.GetByName(ctx, "user_offer_purchase_history")
	if err != nil || entityType == nil {
		logger.Warn("Audit entity type 'user_offer_purchase_history' missing, attempting creation", "error", err)
		entityTypeID, createErr := app.Models.EntityType.CreateIfNotExists(ctx, "user_offer_purchase_history", "A user's purchase history for offers")
		if createErr != nil {
			logger.Error("Failed to create audit entity type", "error", createErr)
			// Allow main operation to succeed
		} else {
			entityType = &data.EntityType{ID: entityTypeID}
		}
	}

	// Perform audit logging (non-fatal)
	if action != nil && entityType != nil {
		audit := data.AuditLog{
			ID:           uuid.New(),
			UserID:       userID,
			ActionID:     action.ID,
			EntityTypeID: entityType.ID,
			EntityID:     userID.String(), // Logs against the user whose history was fetched
		}
		if err := app.Models.AuditLog.Insert(ctx, &audit); err != nil {
			logger.Warn("Audit logging failed", "ctxUserID", userID, "error", err)
		}
	}

	// Respond with the retrieved purchase history
	logger.Info("Successfully retrieved user offer purchase history", "ctxUserID", userID, "count", len(purchaseHistory))
	app.respondWithJSON(w, http.StatusOK, map[string]any{
		"ctxUserID":          userID,
		"purchase_history": purchaseHistory,
	})
}


// GetMostFavoritedOffersHandler handles retrieving the most favorited offers.
// It enforces permission checks, extracts trusted identifiers from context,
// queries the database for the most favorited offers, performs audit logging,
// and responds with the list of offers.
func (app *Application) GetMostFavoritedOffersHandler(w http.ResponseWriter, r *http.Request) {
	logger := app.Logger.GetLoggerWithContext(r).WithFunctionName("GetMostFavoritedOffersHandler")
	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	// Permission enforcement
	if !app.HasPermission(ctx, "read_most_favorited_offers") {
		app.respondWithError(w, errors.New("forbidden: insufficient permissions"), http.StatusForbidden)
		return
	}

	// Retrieve most favorited offers from database
	offers, err := app.Models.Offer.GetMostFavoritedOffers(ctx)
	if err != nil {
		logger.Error("Failed to retrieve most favorited offers", "error", err)
		app.respondWithError(w, fmt.Errorf("failed to retrieve most favorited offers: %w", err), http.StatusInternalServerError)
		return
	}

	// Resolve Audit Action
	action, err := app.Models.Action.GetByName(ctx, "read_most_favorited_offers")
	if err != nil || action == nil {
		logger.Warn("Audit action 'read_most_favorited_offers' not found, attempting to create...", "error", err)
		actionID, createErr := app.Models.Action.CreateIfNotExists(ctx, "read_most_favorited_offers", "Retrieve most favorited offers")
		if createErr != nil {
			logger.Error("Failed to create missing audit action", "error", createErr)
			// Allow main operation to succeed
		} else {
			action = &data.Action{ID: actionID}
		}
	}

	// Resolve Audit Entity Type
	entityType, err := app.Models.EntityType.GetByName(ctx, "offer")
	if err != nil || entityType == nil {
		logger.Warn("Audit entity type 'offer' not found, attempting to create...", "error", err)
		entityTypeID, createErr := app.Models.EntityType.CreateIfNotExists(ctx, "offer", "Offer entity")
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
			EntityID:     "most_favorited_offers", // Static ID for this operation
		}
		if err := app.Models.AuditLog.Insert(ctx, &audit); err != nil {
			logger.Warn("Audit logging failed", "error", err)
			// Proceed without failing the main operation
		}
	}

	// Respond success
	logger.Info("Successfully retrieved most favorited offers", "count", len(offers))
	app.respondWithJSON(w, http.StatusOK, map[string]any{
		"offers": offers,
		"count": len(offers),
	})
}

// GetRecentlyFavoritedOfferByUserHandler retrieves the most recently favorited offer by a specific user.
// It enforces permission checks, extracts user ID from the trusted context, queries the database,
// performs structured logging, and logs an audit record for traceability.
func (app *Application) GetRecentlyFavoritedOfferByUserHandler(w http.ResponseWriter, r *http.Request) {
	logger := app.Logger.GetLoggerWithContext(r).WithFunctionName("GetRecentlyFavoritedOfferByUserHandler")
	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	// Permission enforcement
	if !app.HasPermission(ctx, "read_recently_favorited_offer") {
		app.respondWithError(w, errors.New("forbidden: insufficient permissions"), http.StatusForbidden)
		return
	}

	// Extract authenticated User ID
	userID := app.getUserIDFromContext(ctx)
	if userID == nil {
		app.respondWithError(w, errors.New("missing user ID in context"), http.StatusUnauthorized)
		return
	}

	// Retrieve the most recently favorited offer for the user
	offer, err := app.Models.Offer.GetRecentlyFavoritedByUserID(ctx, *userID)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			app.respondWithError(w, fmt.Errorf("recently favorited offer not found"), http.StatusNotFound)
		} else {
			logger.Error("Failed to retrieve recently favorited offer", "error", err)
			app.respondWithError(w, fmt.Errorf("failed to retrieve recently favorited offer: %w", err), http.StatusInternalServerError)
		}
		return
	}

	// Resolve Audit Action
	action, err := app.Models.Action.GetByName(ctx, "read_recently_favorited_offer")
	if err != nil || action == nil {
		logger.Warn("Audit action 'read_recently_favorited_offer' not found, attempting to create...", "error", err)
		actionID, createErr := app.Models.Action.CreateIfNotExists(ctx, "read_recently_favorited_offer", "Retrieve the most recently favorited offer by user")
		if createErr != nil {
			logger.Error("Failed to create missing audit action", "error", createErr)
			// Allow main operation to succeed
		} else {
			action = &data.Action{ID: actionID}
		}
	}

	// Resolve Audit Entity Type
	entityType, err := app.Models.EntityType.GetByName(ctx, "offer")
	if err != nil || entityType == nil {
		logger.Warn("Audit entity type 'offer' not found, attempting to create...", "error", err)
		entityTypeID, createErr := app.Models.EntityType.CreateIfNotExists(ctx, "offer", "Offer entity")
		if createErr != nil {
			logger.Error("Failed to create missing entity type", "error", createErr)
			// Allow main operation to succeed
		} else {
			entityType = &data.EntityType{ID: entityTypeID}
		}
	}

	// Insert Audit Log (fail gracefully if audit fails)
	if userID != nil && action != nil && entityType != nil {
		audit := data.AuditLog{
			ID:           uuid.New(),
			UserID:       userID,
			ActionID:     action.ID,
			EntityTypeID: entityType.ID,
			EntityID:     offer.ID.String(),
		}
		if err := app.Models.AuditLog.Insert(ctx, &audit); err != nil {
			logger.Warn("Audit logging failed", "ctxOfferID", offer.ID, "error", err)
			// Proceed despite audit failure
		}
	}

	// Respond with the recently favorited offer
	logger.Info("Successfully retrieved recently favorited offer", "ctxOfferID", offer.ID)
	app.respondWithJSON(w, http.StatusOK, offer)
}


// Administrative Control & Analytics

// GetUsersWhoFavoritedOfferHandler retrieves a list of users who have favorited a specific offer.
// It enforces permission checks, extracts the offer ID from the context, queries the database,
// performs structured logging, and logs an audit entry for the operation.
func (app *Application) GetUsersWhoFavoritedOfferHandler(w http.ResponseWriter, r *http.Request) {
    logger := app.Logger.GetLoggerWithContext(r).WithFunctionName("GetUsersWhoFavoritedOfferHandler")
    ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
    defer cancel()

    // Permission enforcement
    if !app.HasPermission(ctx, "read_users_who_favorited_offer") {
        app.respondWithError(w, errors.New("forbidden: insufficient permissions"), http.StatusForbidden)
        return
    }

    // Extract Offer ID from context (injected via middleware)
    offerID := app.getOfferIDFromContext(ctx)
    if offerID == nil {
        app.respondWithError(w, errors.New("missing offer ID in context"), http.StatusBadRequest)
        return
    }

    // Retrieve users who favorited the offer from the database
    users, err := app.Models.Offer.GetUsersWhoFavoritedOffer(ctx, *offerID)
    if err != nil {
        logger.Error("Failed to retrieve users who favorited offer", "ctxOfferID", offerID, "error", err)
        app.respondWithError(w, fmt.Errorf("failed to retrieve users: %w", err), http.StatusInternalServerError)
        return
    }

    // Resolve Audit Action
    action, err := app.Models.Action.GetByName(ctx, "read_users_who_favorited_offer")
    if err != nil || action == nil {
        logger.Warn("Audit action 'read_users_who_favorited_offer' not found, attempting to create...", "error", err)
        actionID, createErr := app.Models.Action.CreateIfNotExists(ctx, "read_users_who_favorited_offer", "Retrieve users who favorited a offer")
        if createErr != nil {
            logger.Error("Failed to create missing audit action", "error", createErr)
			// Allow main operation to succeed
        } else {
            action = &data.Action{ID: actionID}
        }
    }

    // Resolve Audit Entity Type
    entityType, err := app.Models.EntityType.GetByName(ctx, "offer_favorite")
    if err != nil || entityType == nil {
        logger.Warn("Audit entity type 'offer_favorite' not found, attempting to create...", "error", err)
        entityTypeID, createErr := app.Models.EntityType.CreateIfNotExists(ctx, "offer_favorite", "User favorite action on a offer")
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
            EntityID:     offerID.String(),
        }
        if err := app.Models.AuditLog.Insert(ctx, &audit); err != nil {
            logger.Warn("Audit logging failed", "ctxOfferID", offerID, "error", err)
            // Proceed without failing the main operation
        }
    }

    // Respond success
    logger.Info("Successfully retrieved users who favorited offer", "ctxOfferID", offerID, "user_count", len(users))
    app.respondWithJSON(w, http.StatusOK, map[string]any{
        "ctxOfferID": offerID,
        "users":   users,
    })
}


// GetUserFavoritesCountByOfferHandler handles retrieving the count of user favorites for a specific offer.
// It enforces permission checks, extracts offer ID from trusted context, retrieves the count from the database,
// performs audit logging, and responds with the result.
func (app *Application) GetUserFavoritesCountByOfferHandler(w http.ResponseWriter, r *http.Request) {
	logger := app.Logger.GetLoggerWithContext(r).WithFunctionName("GetUserFavoritesCountByOfferHandler")
	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	// Permission enforcement
	if !app.HasPermission(ctx, "read_user_favorites_count") {
		app.respondWithError(w, errors.New("forbidden: insufficient permissions"), http.StatusForbidden)
		return
	}

	// Extract Offer ID from trusted context
	offerID := app.getOfferIDFromContext(ctx)
	if offerID == nil {
		app.respondWithError(w, errors.New("missing offer ID in context"), http.StatusBadRequest)
		return
	}

	// Retrieve favorites count from the database
	count, err := app.Models.UserFavorites.GetFavoritesCountByOfferID(ctx, *offerID)
	if err != nil {
		logger.Error("Failed to retrieve user favorites count", "ctxOfferID", offerID, "error", err)
		app.respondWithError(w, fmt.Errorf("failed to retrieve favorites count: %w", err), http.StatusInternalServerError)
		return
	}

	// Resolve Audit Action
	action, err := app.Models.Action.GetByName(ctx, "read_user_favorites_count")
	if err != nil || action == nil {
		logger.Warn("Audit action 'read_user_favorites_count' not found, attempting to create...", "error", err)
		actionID, createErr := app.Models.Action.CreateIfNotExists(ctx, "read_user_favorites_count", "Retrieve the count of user favorites for a specific offer")
		if createErr != nil {
			logger.Error("Failed to create missing audit action", "error", createErr)
			// Allow main operation to succeed
		} else {
			action = &data.Action{ID: actionID}
		}
	}

	// Resolve Audit Entity Type
	entityType, err := app.Models.EntityType.GetByName(ctx, "user_favorites")
	if err != nil || entityType == nil {
		logger.Warn("Audit entity type 'user_favorites' not found, attempting to create...", "error", err)
		entityTypeID, createErr := app.Models.EntityType.CreateIfNotExists(ctx, "user_favorites", "User favorites for a offer")
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
			EntityID:     offerID.String(),
		}
		if err := app.Models.AuditLog.Insert(ctx, &audit); err != nil {
			logger.Warn("Audit logging failed", "ctxOfferID", offerID, "error", err)
			// Proceed without failing the main operation
		}
	}

	// Success response
	logger.Info("Successfully retrieved user favorites count", "ctxOfferID", offerID, "count", count)
	app.respondWithJSON(w, http.StatusOK, map[string]any{
		"ctxOfferID":        offerID,
		"favorites_count": count,
	})
}


// GetUserFavoritesCountByUserHandler handles retrieving the count of favorite items for a specific user.
// It enforces permission checks, extracts the user ID from trusted context, queries the database,
// performs audit logging, and responds with the count of favorites.
func (app *Application) GetUserFavoritesCountByUserHandler(w http.ResponseWriter, r *http.Request) {
	logger := app.Logger.GetLoggerWithContext(r).WithFunctionName("GetUserFavoritesCountByUserHandler")
	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	// Permission enforcement
	if !app.HasPermission(ctx, "read_user_favorites_count") {
		app.respondWithError(w, errors.New("forbidden: insufficient permissions"), http.StatusForbidden)
		return
	}

	// Extract User ID from trusted context
	userID := app.getUserIDFromContext(ctx)
	if userID == nil {
		app.respondWithError(w, errors.New("missing user ID in context"), http.StatusUnauthorized)
		return
	}

	// Retrieve favorites count from the database
	count, err := app.Models.Favorites.GetCountByUserID(ctx, *userID)
	if err != nil {
		logger.Error("Failed to retrieve user favorites count", "ctxUserID", userID, "error", err)
		app.respondWithError(w, fmt.Errorf("failed to retrieve favorites count: %w", err), http.StatusInternalServerError)
		return
	}

	// Resolve Audit Action
	action, err := app.Models.Action.GetByName(ctx, "read_user_favorites_count")
	if err != nil || action == nil {
		logger.Warn("Audit action 'read_user_favorites_count' not found, attempting to create...", "error", err)
		actionID, createErr := app.Models.Action.CreateIfNotExists(ctx, "read_user_favorites_count", "Retrieve the count of user favorites")
		if createErr != nil {
			logger.Error("Failed to create missing audit action", "error", createErr)
			// Allow main operation to succeed
		} else {
			action = &data.Action{ID: actionID}
		}
	}

	// Resolve Audit Entity Type
	entityType, err := app.Models.EntityType.GetByName(ctx, "user_favorites")
	if err != nil || entityType == nil {
		logger.Warn("Audit entity type 'user_favorites' not found, attempting to create...", "error", err)
		entityTypeID, createErr := app.Models.EntityType.CreateIfNotExists(ctx, "user_favorites", "User's favorite items")
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
			EntityID:     userID.String(),
		}
		if err := app.Models.AuditLog.Insert(ctx, &audit); err != nil {
			logger.Warn("Audit logging failed", "ctxUserID", userID, "error", err)
			app.respondWithJSON(w, http.StatusPartialContent, map[string]any{
				"ctxUserID": userID,
				"count":   count,
				"error":   "Favorites count retrieved, but audit logging failed",
			})
			return
		}
	}

	// Success response
	logger.Info("Successfully retrieved user favorites count", "ctxUserID", userID, "count", count)
	app.respondWithJSON(w, http.StatusOK, map[string]any{
		"ctxUserID": userID,
		"count":   count,
	})
}

// System Automation &


// GetTrendingOffersForUserHandler retrieves trending offers for a specific user.
// It enforces permission checks, extracts user ID from trusted context,
// queries the database for trending offers, performs structured audit logging,
// and responds with the trending offers.
func (app *Application) GetTrendingOffersForUserHandler(w http.ResponseWriter, r *http.Request) {
	logger := app.Logger.GetLoggerWithContext(r).WithFunctionName("GetTrendingOffersForUserHandler")
	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	// Permission enforcement
	if !app.HasPermission(ctx, "read_trending_offers_for_user") {
		app.respondWithError(w, errors.New("forbidden: insufficient permissions"), http.StatusForbidden)
		return
	}

	// Extract User ID from context
	userID := app.getUserIDFromContext(ctx)
	if userID == nil {
		app.respondWithError(w, errors.New("missing user ID in context"), http.StatusUnauthorized)
		return
	}

	// Retrieve trending offers for the user from the database
	trendingOffers, err := app.Models.Offer.GetTrendingOffersForUser(ctx, *userID)
	if err != nil {
		logger.Error("Failed to retrieve trending offers", "ctxUserID", userID, "error", err)
		app.respondWithError(w, fmt.Errorf("failed to retrieve trending offers: %w", err), http.StatusInternalServerError)
		return
	}

	// Resolve Audit Action
	action, err := app.Models.Action.GetByName(ctx, "read_trending_offers_for_user")
	if err != nil || action == nil {
		logger.Warn("Audit action 'read_trending_offers_for_user' not found, attempting to create...", "error", err)
		actionID, createErr := app.Models.Action.CreateIfNotExists(ctx, "read_trending_offers_for_user", "Retrieve trending offers for a user")
		if createErr != nil {
			logger.Error("Failed to create missing audit action", "error", createErr)
			// Allow main operation to succeed
		} else {
			action = &data.Action{ID: actionID}
		}
	}

	// Resolve Audit Entity Type
	entityType, err := app.Models.EntityType.GetByName(ctx, "offer")
	if err != nil || entityType == nil {
		logger.Warn("Audit entity type 'offer' not found, attempting to create...", "error", err)
		entityTypeID, createErr := app.Models.EntityType.CreateIfNotExists(ctx, "offer", "Offer entity")
		if createErr != nil {
			logger.Error("Failed to create missing entity type", "error", createErr)
			// Allow main operation to succeed
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
			EntityID:     "trending_offers_for_user",
		}
		if err := app.Models.AuditLog.Insert(ctx, &audit); err != nil {
			logger.Warn("Audit logging failed", "ctxUserID", userID, "error", err)
			app.respondWithJSON(w, http.StatusPartialContent, map[string]any{
				"trending_offers": trendingOffers,
				"warning":        "Trending offers retrieved, but audit logging failed",
			})
			return
		}
	}

	// Respond with the trending offers
	logger.Info("Successfully retrieved trending offers for user", "ctxUserID", userID)
	app.respondWithJSON(w, http.StatusOK, map[string]any{
		"ctxUserID":        userID,
		"trending_offers": trendingOffers,
	})
}


// ShareFavoriteListHandler handles the generation of a shareable URL for a user's favorite list.
// It enforces permission checks, extracts the authenticated user ID from context,
// invokes the model-level method to generate a shareable list, and performs audit logging.
func (app *Application) ShareFavoriteListHandler(w http.ResponseWriter, r *http.Request) {
	logger := app.Logger.GetLoggerWithContext(r).WithFunctionName("ShareFavoriteListHandler")
	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	// --- Extract User ID from context (injected by JWT middleware) ---
	userID := app.getUserIDFromContext(ctx)
	if userID == nil {
		app.respondWithError(w, errors.New("unauthorized: login required"), http.StatusUnauthorized)
		return
	}

	// Generate shareable favorite list URL
	shareURL, err := app.Models.UserFavorite.ShareFavoriteList(ctx, *userID)
	if err != nil {
		logger.Error("Failed to generate shareable favorite list", "user_id", userID, "error", err)
		app.respondWithError(w, fmt.Errorf("failed to share favorite list: %w", err), http.StatusInternalServerError)
		return
	}

	// Extract share ID from the generated URL
	shareID := app.extractUUIDFromURL(shareURL)
	if shareID == uuid.Nil {
		logger.Warn("Could not extract share ID from generated URL", "share_url", shareURL)
	}

	// Resolve or create audit action
	action, err := app.Models.Action.GetByName(ctx, "share_favorite_list")
	if err != nil || action == nil {
		logger.Warn("Audit action 'share_favorite_list' not found", "error", err)
		actionID, createErr := app.Models.Action.CreateIfNotExists(ctx, "share_favorite_list", "Generate a shareable URL for favorite list")
		if createErr != nil {
			logger.Error("Failed to create missing audit action", "error", createErr)
			// Allow main operation to succeed
		} else {
			action = &data.Action{ID: actionID}
		}
	}

	// Resolve or create audit entity type
	entityType, err := app.Models.EntityType.GetByName(ctx, "favorite_list")
	if err != nil || entityType == nil {
		logger.Warn("Audit entity type 'favorite_list' not found", "error", err)
		entityTypeID, createErr := app.Models.EntityType.CreateIfNotExists(ctx, "favorite_list", "A user's list of favorite offers")
		if createErr != nil {
			logger.Error("Failed to create missing entity type", "error", createErr)
			// Allow main operation to succeed
		} else {
			entityType = &data.EntityType{ID: entityTypeID}
		}
	}

	// Insert audit log (fail gracefully if logging fails)
	if action != nil && entityType != nil {
		audit := data.AuditLog{
			ID:           uuid.New(),
			UserID:       userID,
			ActionID:     action.ID,
			EntityTypeID: entityType.ID,
			EntityID:     shareID.String(),
		}
		if err := app.Models.AuditLog.Insert(ctx, &audit); err != nil {
			logger.Warn("Audit logging failed", "share_id", shareID, "error", err)
		}
	}

	// Respond with the generated shareable URL
	logger.Info("Favorite list shared successfully", "user_id", userID, "share_url", shareURL)
	app.successJSON(w, http.StatusOK, envelope{
		"share_url": shareURL,
	})
}


// MigrateFavoritesToNewUserHandler handles the migration of favorite offers from one user to another.
// It ensures permission enforcement, extracts trusted identifiers from context, performs the migration,
// logs the operation, and handles audit logging.
func (app *Application) MigrateFavoritesToNewUserHandler(w http.ResponseWriter, r *http.Request) {
	logger := app.Logger.GetLoggerWithContext(r).WithFunctionName("MigrateFavoritesToNewUserHandler")
	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	// --- Role Enforcement (internal-only) ---
	isInternal := app.HasRole(ctx, "admin") || app.HasRole(ctx, "internal_operator")
	if !isInternal {
		app.respondWithError(w, errors.New("forbidden: internal access only"), http.StatusForbidden)
		return
	}

	// --- Permission Check ---
	if !app.HasPermission(ctx, "migrate_favorites") {
		app.respondWithError(w, errors.New("forbidden: insufficient permissions"), http.StatusForbidden)
		return
	}

	// Extract source and target user IDs from context
	sourceUserID := app.getSourceUserIDFromContext(ctx)
	targetUserID := app.getTargetUserIDFromContext(ctx)

	if sourceUserID == nil || targetUserID == nil {
		app.respondWithError(w, errors.New("missing user IDs in context"), http.StatusBadRequest)
		return
	}

	// Perform the migration operation
	err := app.Models.Favorites.MigrateFavoritesToNewUser(ctx, *sourceUserID, *targetUserID)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			app.respondWithError(w, errors.New("source user not found"), http.StatusNotFound)
		} else {
			logger.Error("Failed to migrate favorites", "error", err)
			app.respondWithError(w, errors.New("failed to migrate favorites"), http.StatusInternalServerError)
		}
		return
	}

	// Resolve Audit Action
	action, err := app.Models.Action.GetByName(ctx, "migrate_favorites")
	if err != nil || action == nil {
		logger.Warn("Audit action 'migrate_favorites' not found, attempting to create...", "error", err)
		actionID, createErr := app.Models.Action.CreateIfNotExists(ctx, "migrate_favorites", "Migrate favorites from one user to another")
		if createErr != nil {
			logger.Error("Failed to create missing audit action", "error", createErr)
			// Allow main operation to succeed
		} else {
			action = &data.Action{ID: actionID}
		}
	}

	// Resolve Audit Entity Type
	entityType, err := app.Models.EntityType.GetByName(ctx, "favorites")
	if err != nil || entityType == nil {
		logger.Warn("Audit entity type 'favorites' not found, attempting to create...", "error", err)
		entityTypeID, createErr := app.Models.EntityType.CreateIfNotExists(ctx, "favorites", "User favorite offers")
		if createErr != nil {
			logger.Error("Failed to create missing entity type", "error", createErr)
			// Allow main operation to succeed
		} else {
			entityType = &data.EntityType{ID: entityTypeID}
		}
	}

	// Insert Audit Log (fail gracefully if audit logging fails)
	if action != nil && entityType != nil {
		audit := data.AuditLog{
			ID:           uuid.New(),
			UserID:       targetUserID,
			ActionID:     action.ID,
			EntityTypeID: entityType.ID,
			EntityID:     sourceUserID.String(),
		}
		if err := app.Models.AuditLog.Insert(ctx, &audit); err != nil {
			logger.Warn("Audit logging failed", "ctxSourceUserID", sourceUserID, "ctxTargetUserID", targetUserID, "error", err)
			app.respondWithJSON(w, http.StatusPartialContent, map[string]any{
				"ctxSourceUserID": sourceUserID,
				"ctxTargetUserID": targetUserID,
				"error":          "Favorites migrated, but audit logging failed",
			})
			return
		}
	}

	// Respond success
	logger.Info("Favorites migrated successfully", "ctxSourceUserID", sourceUserID, "ctxTargetUserID", targetUserID)
	app.respondWithJSON(w, http.StatusOK, map[string]any{
		"ctxSourceUserID": sourceUserID,
		"ctxTargetUserID": targetUserID,
	})
}


// User follows

// FollowMerchantHandler handles the operation of following a merchant.
// It enforces permission checks, extracts trusted identifiers from context,
// performs the follow operation, resolves audit action/entity, and logs the operation.
func (app *Application) FollowMerchantHandler(w http.ResponseWriter, r *http.Request) {
	logger := app.Logger.GetLoggerWithContext(r).WithFunctionName("FollowMerchantHandler")
	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	// --- Extract user ID from context (injected by JWT middleware) ---
	userID := getUserIDFromContext(ctx)
	if userID == uuid.Nil {
		app.respondWithError(w, errors.New("unauthorized: login required"), http.StatusUnauthorized)
		return
	}

	// Extract Merchant ID from context (injected via middleware)
	merchantID := app.getMerchantIDFromContext(ctx)
	if merchantID == nil {
		app.respondWithError(w, errors.New("missing merchant ID in context"), http.StatusBadRequest)
		return
	}

	// Perform follow operation in the database
	if err := app.Models.MerchantFollow.FollowMerchant(ctx, *userID, *merchantID); err != nil {
		logger.Error("Failed to follow merchant", "ctxUserID", userID, "ctxMerchantID", merchantID, "error", err)
		app.respondWithError(w, fmt.Errorf("failed to follow merchant: %w", err), http.StatusInternalServerError)
		return
	}

	// Resolve Audit Action
	action, err := app.Models.Action.GetByName(ctx, "follow_merchant")
	if err != nil || action == nil {
		logger.Warn("Audit action 'follow_merchant' not found, attempting to create...", "error", err)
		actionID, createErr := app.Models.Action.CreateIfNotExists(ctx, "follow_merchant", "Follow a merchant")
		if createErr != nil {
			logger.Error("Failed to create missing audit action", "error", createErr)
			// Allow main operation to succeed
		} else {
			action = &data.Action{ID: actionID}
		}
	}

	// Resolve Audit Entity Type
	entityType, err := app.Models.EntityType.GetByName(ctx, "merchant_follow")
	if err != nil || entityType == nil {
		logger.Warn("Audit entity type 'merchant_follow' not found, attempting to create...", "error", err)
		entityTypeID, createErr := app.Models.EntityType.CreateIfNotExists(ctx, "merchant_follow", "Follow relationship between user and merchant")
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
			EntityID:     merchantID.String(),
		}
		if err := app.Models.AuditLog.Insert(ctx, &audit); err != nil {
			logger.Warn("Audit logging failed", "ctxMerchantID", merchantID, "error", err)
			app.respondWithJSON(w, http.StatusPartialContent, map[string]any{
				"ctxMerchantID": merchantID,
				"error":     "Follow operation succeeded, but audit logging failed",
			})
			return
		}
	}

	// Respond success
	logger.Info("Merchant followed successfully", "ctxUserID", userID, "ctxMerchantID", merchantID)
	app.respondWithJSON(w, http.StatusOK, map[string]any{
		"ctxMerchantID": merchantID,
		"status":    "followed",
	})
}


// UnfollowMerchantHandler handles the operation of unfollowing a merchant.
// It enforces permission checks, extracts trusted identifiers from context,
// performs the unfollow operation, resolves audit metadata, and logs the event.
func (app *Application) UnfollowMerchantHandler(w http.ResponseWriter, r *http.Request) {
	logger := app.Logger.GetLoggerWithContext(r).WithFunctionName("UnfollowMerchantHandler")
	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	// --- Extract user ID from context (injected by JWT middleware) ---
	userID := getUserIDFromContext(ctx)
	if userID == uuid.Nil {
		app.respondWithError(w, errors.New("unauthorized: login required"), http.StatusUnauthorized)
		return
	}

	// --- Extract merchant ID from context (injected by middleware) ---
	merchantID := app.getMerchantIDFromContext(ctx)
	if merchantID == nil {
		app.respondWithError(w, errors.New("bad request: missing merchant ID in context"), http.StatusBadRequest)
		return
	}

	// --- Perform unfollow operation ---
	err := app.Models.MerchantFollow.UnfollowMerchant(ctx, *userID, *merchantID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			logger.Warn("No follow record found", "user_id", userID, "merchant_id", merchantID)
			app.respondWithError(w, errors.New("not found: no follow relationship exists"), http.StatusNotFound)
			return
		}
		logger.Error("Failed to unfollow merchant", "error", err)
		app.respondWithError(w, fmt.Errorf("failed to unfollow merchant: %w", err), http.StatusInternalServerError)
		return
	}

	// --- Resolve audit action ---
	action, err := app.Models.Action.GetByName(ctx, "unfollow_merchant")
	if err != nil || action == nil {
		logger.Warn("Audit action 'unfollow_merchant' not found, creating...", "error", err)
		actionID, createErr := app.Models.Action.CreateIfNotExists(ctx, "unfollow_merchant", "Unfollow a merchant")
		if createErr == nil {
			action = &data.Action{ID: actionID}
		} else {
			logger.Error("Failed to create audit action", "error", createErr)
		}
	}

	// --- Resolve audit entity type ---
	entityType, err := app.Models.EntityType.GetByName(ctx, "merchant_follow")
	if err != nil || entityType == nil {
		logger.Warn("Entity type 'merchant_follow' not found, creating...", "error", err)
		entityTypeID, createErr := app.Models.EntityType.CreateIfNotExists(ctx, "merchant_follow", "Follow relationship between user and merchant")
		if createErr == nil {
			entityType = &data.EntityType{ID: entityTypeID}
		} else {
			logger.Error("Failed to create entity type", "error", createErr)
		}
	}

	// --- Insert audit log (fail gracefully if needed) ---
	if userID != nil && action != nil && entityType != nil {
		audit := data.AuditLog{
			ID:           uuid.New(),
			UserID:       userID,
			ActionID:     action.ID,
			EntityTypeID: entityType.ID,
			EntityID:     merchantID.String(),
		}
		if err := app.Models.AuditLog.Insert(ctx, &audit); err != nil {
			logger.Warn("Audit logging failed", "ctxMerchantID", merchantID, "error", err)
			app.respondWithJSON(w, http.StatusPartialContent, map[string]any{
				"ctxMerchantID": merchantID,
				"error":         "Unfollow succeeded, but audit logging failed",
			})
			return
		}
	}

	// --- Respond success ---
	logger.Info("Merchant unfollowed successfully", "ctxUserID", userID, "ctxMerchantID", merchantID)
	app.respondWithJSON(w, http.StatusOK, map[string]any{
		"ctxMerchantID": merchantID,
		"status":        "unfollowed",
	})
}


// GetFollowedMerchantsOffersHandler handles retrieving offers from merchants that the user follows.
// It enforces permission checks, extracts user ID from trusted context, queries the database for offers,
// performs structured logging, and logs an audit record for traceability.
func (app *Application) GetFollowedMerchantsOffersHandler(w http.ResponseWriter, r *http.Request) {
	logger := app.Logger.GetLoggerWithContext(r).WithFunctionName("GetFollowedMerchantsOffersHandler")
	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	// Permission enforcement
	if !app.HasPermission(ctx, "read_followed_merchants_offers") {
		app.respondWithError(w, errors.New("forbidden: insufficient permissions"), http.StatusForbidden)
		return
	}

	// Extract authenticated User ID
	userID := app.getUserIDFromContext(ctx)
	if userID == nil {
		app.respondWithError(w, errors.New("missing user ID in context"), http.StatusUnauthorized)
		return
	}

	// Retrieve offers from followed merchants
	offers, err := app.Models.Offer.GetOffersByFollowedMerchants(ctx, *userID)
	if err != nil {
		logger.Error("Failed to retrieve offers from followed merchants", "ctxUserID", userID, "error", err)
		app.respondWithError(w, fmt.Errorf("failed to retrieve offers: %w", err), http.StatusInternalServerError)
		return
	}

	// Resolve Audit Action
	action, err := app.Models.Action.GetByName(ctx, "read_followed_merchants_offers")
	if err != nil || action == nil {
		logger.Warn("Audit action 'read_followed_merchants_offers' not found, attempting to create...", "error", err)
		actionID, createErr := app.Models.Action.CreateIfNotExists(ctx, "read_followed_merchants_offers", "Retrieve offers from followed merchants")
		if createErr != nil {
			logger.Error("Failed to create missing audit action", "error", createErr)
			// Allow main operation to succeed
		} else {
			action = &data.Action{ID: actionID}
		}
	}

	// Resolve Audit Entity Type
	entityType, err := app.Models.EntityType.GetByName(ctx, "offer")
	if err != nil || entityType == nil {
		logger.Warn("Audit entity type 'offer' not found, attempting to create...", "error", err)
		entityTypeID, createErr := app.Models.EntityType.CreateIfNotExists(ctx, "offer", "Offers from followed merchants")
		if createErr != nil {
			logger.Error("Failed to create missing entity type", "error", createErr)
			// Allow main operation to succeed
		} else {
			entityType = &data.EntityType{ID: entityTypeID}
		}
	}

	// Insert Audit Log (fail gracefully if audit fails)
	if userID != nil && action != nil && entityType != nil {
		audit := data.AuditLog{
			ID:           uuid.New(),
			UserID:       userID,
			ActionID:     action.ID,
			EntityTypeID: entityType.ID,
			EntityID:     "followed_merchants_offers", // Placeholder for entity ID
		}
		if err := app.Models.AuditLog.Insert(ctx, &audit); err != nil {
			logger.Warn("Audit logging failed", "ctxUserID", userID, "error", err)
			// Proceed despite audit failure
		}
	}

	// Success response
	logger.Info("Successfully retrieved offers from followed merchants", "ctxUserID", userID, "offer_count", len(offers))
	app.respondWithJSON(w, http.StatusOK, map[string]any{
		"ctxUserID": userID,
		"offers":   offers,
	})
}
*/
