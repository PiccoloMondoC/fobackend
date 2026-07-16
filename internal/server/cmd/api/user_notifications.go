// sdworkspace/sdbackend/internal/server/cmd/api/user_notifications.go
package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/PiccoloMondoC/sdworkspace/sdbackend/internal/data"

	"github.com/google/uuid"
)

// Principles:
// Always extract sensitive identifiers from a trusted context

/*
// GetUserNotificationByIDHandler retrieves a user notification by its ID, enforcing permissions and performing audit logging.
func (app *Application) GetUserNotificationByIDHandler(w http.ResponseWriter, r *http.Request) {
	logger := app.Logger.GetLoggerWithContext(r).WithFunctionName("GetUserNotificationByIDHandler")
	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	// Permission enforcement
	if !app.HasPermission(ctx, "read_user_notification") {
		app.respondWithError(w, errors.New("forbidden: insufficient permissions"), http.StatusForbidden)
		return
	}

	// Extract Notification ID from context
	notificationID := app.getNotificationIDFromContext(ctx)
	if notificationID == nil {
		app.respondWithError(w, errors.New("missing notification ID in context"), http.StatusBadRequest)
		return
	}

	// Retrieve UserNotification by ID
	notification, err := app.Models.UserNotification.GetByID(ctx, *notificationID)
	if err != nil {
		logger.Error("Failed to retrieve user notification", "error", err)
		app.respondWithError(w, errors.New("failed to retrieve user notification"), http.StatusInternalServerError)
		return
	}
	if notification == nil {
		app.respondWithError(w, errors.New("notification not found"), http.StatusNotFound)
		return
	}

	// Resolve Audit Action
	action, err := app.Models.Action.GetByName(ctx, "read_user_notification")
	if err != nil || action == nil {
		logger.Warn("Audit action 'read_user_notification' not found, attempting to create...", "error", err)
		actionID, createErr := app.Models.Action.CreateIfNotExists(ctx, "read_user_notification", "Read a user notification")
		if createErr != nil {
			logger.Error("Failed to create missing audit action", "error", createErr)
			// Allow main operation to succeed
		} else {
			action = &data.Action{ID: actionID}
		}
	}

	// Resolve Audit Entity Type
	entityType, err := app.Models.EntityType.GetByName(ctx, "user_notification")
	if err != nil || entityType == nil {
		logger.Warn("Audit entity type 'user_notification' not found, attempting to create...", "error", err)
		entityTypeID, createErr := app.Models.EntityType.CreateIfNotExists(ctx, "user_notification", "User notification")
		if createErr != nil {
			logger.Error("Failed to create missing entity type", "error", createErr)
			// Allow main operation to succeed
		} else {
			entityType = &data.EntityType{ID: entityTypeID}
		}
	}

	// Insert Audit Log
	if action != nil && entityType != nil {
		audit := data.AuditLog{
			ID:           uuid.New(),
			UserID:       notification.UserID,
			ActionID:     action.ID,
			EntityTypeID: entityType.ID,
			EntityID:     notification.ID.String(),
		}
		if err := app.Models.AuditLog.Insert(ctx, &audit); err != nil {
			logger.Warn("Audit logging failed", "notificationID", notification.ID, "error", err)
		}
	}

	// Respond with the notification
	logger.Info("User notification retrieved successfully", "notificationID", notification.ID)
	app.respondWithJSON(w, http.StatusOK, notification)
}
*/

// GetUserNotificationByUserIDHandler retrieves all notifications for a given user ID.
// It enforces permission checks, extracts the target user ID from context, queries the database using the provided user ID,
// and returns the matched notifications or appropriate error with audit logging.
func (app *Application) GetUserNotificationByUserIDHandler(w http.ResponseWriter, r *http.Request) {
	logger := app.Logger.GetLoggerWithContext(r).WithFunctionName("GetUserNotificationByUserIDHandler")
	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	// Permission enforcement
	if !app.HasPermission(ctx, "read_user_notifications") {
		app.respondWithError(w, errors.New("forbidden: insufficient permissions"), http.StatusForbidden)
		return
	}

	// Extract User ID from context
	userID := app.getUserIDFromContext(ctx)
	if userID == nil {
		app.respondWithError(w, errors.New("missing user ID in context"), http.StatusUnauthorized)
		return
	}

	// Query the database for user notifications
	notifications, err := app.Models.UserNotification.GetByUserID(ctx, *userID)
	if err != nil {
		logger.Error("Failed to retrieve user notifications", "error", err)
		app.respondWithError(w, fmt.Errorf("failed to retrieve user notifications: %w", err), http.StatusInternalServerError)
		return
	}

	// If no notifications found, return 404
	if len(notifications) == 0 {
		app.respondWithError(w, errors.New("no notifications found"), http.StatusNotFound)
		return
	}

	// Resolve Audit Action
	action, err := app.Models.Action.GetByName(ctx, "read_user_notifications")
	if err != nil || action == nil {
		logger.Warn("Audit action 'read_user_notifications' not found, attempting to create...", "error", err)
		actionID, createErr := app.Models.Action.CreateIfNotExists(ctx, "read_user_notifications", "Read user notifications")
		if createErr != nil {
			logger.Error("Failed to create missing audit action", "error", createErr)
			// Allow main operation to succeed
		} else {
			action = &data.Action{ID: actionID}
		}
	}

	// Resolve Audit Entity Type
	entityType, err := app.Models.EntityType.GetByName(ctx, "user_notification")
	if err != nil || entityType == nil {
		logger.Warn("Audit entity type 'user_notification' not found, attempting to create...", "error", err)
		entityTypeID, createErr := app.Models.EntityType.CreateIfNotExists(ctx, "user_notification", "User notifications")
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
			EntityID:     userID.String(),
		}
		if err := app.Models.AuditLog.Insert(ctx, &audit); err != nil {
			// Audit logging failed (partial success)
			logger.Warn("Audit logging failed", "userID", userID, "error", err)
			app.respondWithJSON(w, http.StatusPartialContent, jsonResponse{
				Error:   false,
				Message: "Notifications retrieved, but audit logging failed",
				Data:    notifications,
			})
			return
		}
	}

	// Respond with the notifications
	logger.Info("User notifications retrieved successfully", "userID", userID)
	app.respondWithJSON(w, http.StatusOK, jsonResponse{
		Error:   false,
		Message: "User notifications retrieved successfully",
		Data:    notifications,
	})
}

/*
// GetUserNotificationByOfferIDHandler retrieves all user notifications for a specific offer ID.
// It enforces permission checks, extracts the target offer ID from context, queries the database using the provided offer ID,
// and returns the matched notifications or appropriate error with audit logging.
func (app *Application) GetUserNotificationByOfferIDHandler(w http.ResponseWriter, r *http.Request) {
	logger := app.Logger.GetLoggerWithContext(r).WithFunctionName("GetUserNotificationByOfferIDHandler")
	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	// Permission enforcement
	if !app.HasPermission(ctx, "read_user_notifications") {
		app.respondWithError(w, errors.New("forbidden: insufficient permissions"), http.StatusForbidden)
		return
	}

	// Extract Offer ID from context
	offerID := app.getOfferIDFromContext(ctx)
	if offerID == nil {
		app.respondWithError(w, errors.New("missing offer ID in context"), http.StatusBadRequest)
		return
	}

	// Query the database for user notifications by offer ID
	notifications, err := app.Models.UserNotification.GetByOfferID(ctx, *offerID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			app.respondWithError(w, errors.New("no notifications found"), http.StatusNotFound)
			return
		}
		logger.Error("Failed to retrieve notifications", "error", err)
		app.respondWithError(w, errors.New("internal server error"), http.StatusInternalServerError)
		return
	}

	// Resolve Audit Action
	action, err := app.Models.Action.GetByName(ctx, "read_user_notifications")
	if err != nil || action == nil {
		logger.Warn("Audit action 'read_user_notifications' not found, attempting to create...", "error", err)
		actionID, createErr := app.Models.Action.CreateIfNotExists(ctx, "read_user_notifications", "Read user notifications by offer ID")
		if createErr != nil {
			logger.Error("Failed to create missing audit action", "error", createErr)
			// Allow main operation to succeed
		} else {
			action = &data.Action{ID: actionID}
		}
	}

	// Resolve Audit Entity Type
	entityType, err := app.Models.EntityType.GetByName(ctx, "user_notification")
	if err != nil || entityType == nil {
		logger.Warn("Audit entity type 'user_notification' not found, attempting to create...", "error", err)
		entityTypeID, createErr := app.Models.EntityType.CreateIfNotExists(ctx, "user_notification", "User notification entity")
		if createErr != nil {
			logger.Error("Failed to create missing entity type", "error", createErr)
			// Allow main operation to succeed
		} else {
			entityType = &data.EntityType{ID: entityTypeID}
		}
	}

	// Insert Audit Log
	if action != nil && entityType != nil {
		audit := data.AuditLog{
			ID:           uuid.New(),
			UserID:       app.getUserIDFromContext(ctx),
			ActionID:     action.ID,
			EntityTypeID: entityType.ID,
			EntityID:     offerID.String(),
		}
		if err := app.Models.AuditLog.Insert(ctx, &audit); err != nil {
			logger.Warn("Audit logging failed", "offerID", offerID, "error", err)
		}
	}

	// Respond with notifications
	logger.Info("Retrieved user notifications successfully", "offerID", offerID)
	app.writeJSON(w, http.StatusOK, jsonResponse{Error: false, Data: notifications})
}


// GetUserNotificationByTypeHandler retrieves user notifications filtered by notification type.
// It enforces permission checks, extracts the notification type ID from context, queries the database,
// performs structured logging, and handles dynamic audit logging.
func (app *Application) GetUserNotificationByTypeHandler(w http.ResponseWriter, r *http.Request) {
	logger := app.Logger.GetLoggerWithContext(r).WithFunctionName("GetUserNotificationByTypeHandler")
	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	// Permission enforcement
	if !app.HasPermission(ctx, "read_user_notifications") {
		app.respondWithError(w, errors.New("forbidden: insufficient permissions"), http.StatusForbidden)
		return
	}

	// Extract User ID from context
	userID := app.getUserIDFromContext(ctx)
	if userID == nil {
		app.respondWithError(w, errors.New("missing user ID in context"), http.StatusUnauthorized)
		return
	}

	// Extract Notification Type ID from context
	notificationTypeID := app.getNotificationTypeIDFromContext(ctx)
	if notificationTypeID == nil {
		app.respondWithError(w, errors.New("missing notification type ID in context"), http.StatusBadRequest)
		return
	}

	// Query the database for user notifications by notification type
	notifications, err := app.Models.UserNotification.GetByType(ctx, *userID, *notificationTypeID)
	if err != nil {
		logger.Error("Failed to retrieve user notifications", "error", err)
		app.respondWithError(w, fmt.Errorf("failed to retrieve user notifications: %w", err), http.StatusInternalServerError)
		return
	}

	if len(notifications) == 0 {
		app.respondWithError(w, errors.New("no notifications found"), http.StatusNotFound)
		return
	}

	// Resolve Audit Action
	action, err := app.Models.Action.GetByName(ctx, "read_user_notifications")
	if err != nil || action == nil {
		logger.Warn("Audit action 'read_user_notifications' not found, attempting to create...", "error", err)
		actionID, createErr := app.Models.Action.CreateIfNotExists(ctx, "read_user_notifications", "Read user notifications by type")
		if createErr != nil {
			logger.Error("Failed to create missing audit action", "error", createErr)
			// Allow main operation to succeed
		} else {
			action = &data.Action{ID: actionID}
		}
	}

	// Resolve Audit Entity Type
	entityType, err := app.Models.EntityType.GetByName(ctx, "user_notification")
	if err != nil || entityType == nil {
		logger.Warn("Audit entity type 'user_notification' not found, attempting to create...", "error", err)
		entityTypeID, createErr := app.Models.EntityType.CreateIfNotExists(ctx, "user_notification", "User notification")
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
			EntityID:     notificationTypeID.String(),
		}
		if err := app.Models.AuditLog.Insert(ctx, &audit); err != nil {
			logger.Warn("Audit logging failed", "notificationTypeID", notificationTypeID, "error", err)
		}
	}

	// Respond with the retrieved notifications
	logger.Info("User notifications retrieved successfully", "notificationTypeID", notificationTypeID)
	app.respondWithJSON(w, http.StatusOK, map[string]any{
		"notifications": notifications,
	})
}


// UpdateUserNotificationHandler handles partial updates to an existing user notification.
// It enforces permission checks, extracts the user notification ID from context,
// parses and validates incoming JSON payload, updates the target user notification in the database,
// and logs the action with structured audit tracking.
func (app *Application) UpdateUserNotificationHandler(w http.ResponseWriter, r *http.Request) {
	logger := app.Logger.GetLoggerWithContext(r).WithFunctionName("UpdateUserNotificationHandler")
	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	// Permission enforcement
	if !app.HasPermission(ctx, "update_user_notification") {
		app.respondWithError(w, errors.New("forbidden: insufficient permissions"), http.StatusForbidden)
		return
	}

	// Extract User Notification ID from context
	notificationID := app.getUserNotificationIDFromContext(ctx)
	if notificationID == nil {
		app.respondWithError(w, errors.New("missing user notification ID in context"), http.StatusBadRequest)
		return
	}

	// Decode and validate JSON payload
	var input data.UserNotification
	if err := app.readJSON(w, r, &input); err != nil {
		app.respondWithError(w, err, http.StatusBadRequest)
		return
	}

	// Update User Notification in the database
	if err := app.Models.UserNotification.Update(ctx, *notificationID, &input); err != nil {
		if errors.Is(err, data.ErrRecordNotFound) {
			app.respondWithError(w, errors.New("user notification not found"), http.StatusNotFound)
		} else {
			logger.Error("Failed to update user notification", "error", err)
			app.respondWithError(w, errors.New("internal server error"), http.StatusInternalServerError)
		}
		return
	}

	// Resolve Audit Action
	action, err := app.Models.Action.GetByName(ctx, "update_user_notification")
	if err != nil || action == nil {
		logger.Warn("Audit action 'update_user_notification' not found, attempting to create...", "error", err)
		actionID, createErr := app.Models.Action.CreateIfNotExists(ctx, "update_user_notification", "Update user notification")
		if createErr != nil {
			logger.Error("Failed to create missing audit action", "error", createErr)
			// Allow main operation to succeed
		} else {
			action = &data.Action{ID: actionID}
		}
	}

	// Resolve Audit Entity Type
	entityType, err := app.Models.EntityType.GetByName(ctx, "user_notification")
	if err != nil || entityType == nil {
		logger.Warn("Audit entity type 'user_notification' not found, attempting to create...", "error", err)
		entityTypeID, createErr := app.Models.EntityType.CreateIfNotExists(ctx, "user_notification", "User notification")
		if createErr != nil {
			logger.Error("Failed to create missing entity type", "error", createErr)
			// Allow main operation to succeed
		} else {
			entityType = &data.EntityType{ID: entityTypeID}
		}
	}

	// Insert Audit Log
	if action != nil && entityType != nil {
		audit := data.AuditLog{
			ID:           uuid.New(),
			UserID:       app.getUserIDFromContext(ctx),
			ActionID:     action.ID,
			EntityTypeID: entityType.ID,
			EntityID:     notificationID.String(),
		}
		if err := app.Models.AuditLog.Insert(ctx, &audit); err != nil {
			logger.Warn("Audit logging failed", "notificationID", notificationID, "error", err)
		}
	}

	// Respond success
	logger.Info("User notification updated successfully", "notificationID", notificationID)
	app.respondWithJSON(w, http.StatusOK, map[string]any{
		"notificationID": notificationID,
	})
}
*/