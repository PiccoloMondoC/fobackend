// Package main provides HTTP handlers and retained internal integration points
// for the Platform API.
//
// sdworkspace/sdbackend/internal/server/cmd/api/user_wallets.go
//
// GTM:
//
//	Layer: 2.3 Consumer Domain
//	Release Class: DEFERRED
//	Reason:
//	  User wallets and wallet-ledger workflows are valid future rewards,
//	  stored-value, platform-credit, and balance infrastructure, but they are
//	  not required for the initial Platform release spine.
//
//	  The data layer preserves the canonical wallet and immutable ledger
//	  contracts so the domain can be activated later without reconstructing
//	  its persistence foundation.
//
// DEFERRED Rule:
//
//	Keep compiling.
//	Keep safe.
//	Preserve canonical wallet-type and unit-code semantics.
//	Preserve NUMERIC-safe decimal string handling.
//	Preserve idempotent reward-wallet creation through EnsureRewardWallet.
//	Preserve immutable wallet-ledger-entry semantics.
//	Preserve pending-to-confirmed ledger lifecycle behavior.
//	Preserve wallet deactivation, soft-delete, and hard-delete distinctions.
//	Preserve database-owned lifecycle timestamps.
//	Do not construct wallet persistence rows in the handler layer.
//	Do not recreate removed Currency, IsDeleted, or generic Insert contracts.
//	Do not add wallet credit, redemption, transfer, or ledger HTTP workflows.
//	Do not route wallet handlers into the active v1 API surface.
//	Do not add new features without explicit release reclassification.
//	Do not block deployment on this file unless it breaks the build.
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

// Principles:
// Always extract sensitive identifiers from a trusted context

// createUserWalletAfterRegistration ensures that the canonical rewards wallet
// exists for a newly registered user.
//
// This is an internal integration hook and is never exposed directly through
// HTTP. The data layer owns wallet construction, canonical defaults,
// idempotency, persistence, and database-owned lifecycle timestamps.
//
// Audit logging is best-effort. Failure to resolve audit metadata or persist
// the audit row must not invalidate successful wallet creation.
func (app *Application) createUserWalletAfterRegistration(
	ctx context.Context,
	userID uuid.UUID,
) error {
	logger := app.Logger.
		GetLoggerWithContextFromContext(ctx).
		WithFunctionName("createUserWalletAfterRegistration")

	if userID == uuid.Nil {
		err := errors.New("user ID is required")
		logger.Error(
			"ensure reward wallet validation failed",
			"error",
			err,
		)
		return err
	}

	wallet, err := app.Models.UserWallet.EnsureRewardWallet(ctx, userID)
	if err != nil {
		logger.Error(
			"ensure reward wallet failed",
			"user_id",
			userID,
			"error",
			err,
		)
		return err
	}

	var action *data.Action

	action, err = app.Models.Action.GetByName(ctx, "create_user_wallet")
	if err != nil || action == nil {
		actionID, createErr := app.Models.Action.CreateIfNotExists(
			ctx,
			"create_user_wallet",
			"Ensure a user reward wallet",
		)
		if createErr != nil {
			logger.Warn(
				"resolve create-user-wallet audit action failed",
				"user_id",
				userID,
				"error",
				createErr,
			)
		} else {
			action = &data.Action{
				ID: actionID,
			}
		}
	}

	var entityType *data.EntityType

	entityType, err = app.Models.EntityType.GetByName(ctx, "user_wallet")
	if err != nil || entityType == nil {
		entityTypeID, createErr := app.Models.EntityType.CreateIfNotExists(
			ctx,
			"user_wallet",
			"User wallet entity",
		)
		if createErr != nil {
			logger.Warn(
				"resolve user-wallet audit entity type failed",
				"user_id",
				userID,
				"wallet_id",
				wallet.ID,
				"error",
				createErr,
			)
		} else {
			entityType = &data.EntityType{
				ID: entityTypeID,
			}
		}
	}

	if action != nil &&
		action.ID != uuid.Nil &&
		entityType != nil &&
		entityType.ID != uuid.Nil {

		auditLog := &data.AuditLog{
			ID:           uuid.New(),
			UserID:       &userID,
			ActionID:     action.ID,
			EntityTypeID: entityType.ID,
			EntityID:     wallet.ID.String(),
		}

		if err := app.Models.AuditLog.Insert(ctx, auditLog); err != nil {
			logger.Warn(
				"create-user-wallet audit insertion failed",
				"user_id",
				userID,
				"wallet_id",
				wallet.ID,
				"error",
				err,
			)
		}
	} else {
		logger.Warn(
			"create-user-wallet audit skipped because metadata is unresolved",
			"user_id",
			userID,
			"wallet_id",
			wallet.ID,
			"has_action",
			action != nil,
			"has_entity_type",
			entityType != nil,
		)
	}

	logger.Info(
		"user reward wallet ensured",
		"user_id",
		userID,
		"wallet_id",
		wallet.ID,
	)

	return nil
}

// GetUserWalletByIDHandler handles retrieving a user wallet by its ID.
// It enforces permission checks, extracts user_id from context, retrieves the user wallet from the database,
// performs audit logging, and responds with the user wallet details.
func (app *Application) GetUserWalletByIDHandler(w http.ResponseWriter, r *http.Request) {
	logger := app.Logger.GetLoggerWithContext(r).WithFunctionName("GetUserWalletByIDHandler")
	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	// Permission enforcement
	if !app.HasPermission(ctx, "read_user_wallet") {
		app.respondWithError(w, errors.New("forbidden: insufficient permissions"), http.StatusForbidden)
		return
	}

	// Extract User ID from context
	userID := app.getUserIDFromContext(ctx)
	if userID == nil {
		app.respondWithError(w, errors.New("missing user ID in context"), http.StatusBadRequest)
		return
	}

	// Retrieve the user wallet
	userWallet, err := app.Models.UserWallet.GetByID(ctx, *userID)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			app.respondWithError(w, fmt.Errorf("user wallet not found"), http.StatusNotFound)
		} else {
			logger.Error("Failed to retrieve user wallet", "error", err)
			app.respondWithError(w, fmt.Errorf("failed to retrieve user wallet: %w", err), http.StatusInternalServerError)
		}
		return
	}

	// Resolve Audit Action
	action, err := app.Models.Action.GetByName(ctx, "read_user_wallet")
	if err != nil || action == nil {
		logger.Warn("Audit action 'read_user_wallet' not found, attempting to create...", "error", err)
		actionID, createErr := app.Models.Action.CreateIfNotExists(ctx, "read_user_wallet", "Retrieve a user wallet by ID")
		if createErr != nil {
			logger.Error("Failed to create missing audit action", "error", createErr)
			// Allow main operation to proceed
		} else {
			action = &data.Action{ID: actionID}
		}
	}

	// Resolve Audit Entity Type
	entityType, err := app.Models.EntityType.GetByName(ctx, "user_wallet")
	if err != nil || entityType == nil {
		logger.Warn("Audit entity type 'user_wallet' not found, attempting to create...", "error", err)
		entityTypeID, createErr := app.Models.EntityType.CreateIfNotExists(ctx, "user_wallet", "User wallet details")
		if createErr != nil {
			logger.Error("Failed to create missing entity type", "error", createErr)
			// Allow main operation to proceed
		} else {
			entityType = &data.EntityType{ID: entityTypeID}
		}
	}

	// Insert Audit Log (fail gracefully if fails)
	if action != nil && entityType != nil {
		audit := data.AuditLog{
			ID:           uuid.New(),
			UserID:       userID,
			ActionID:     action.ID,
			EntityTypeID: entityType.ID,
			EntityID:     userWallet.ID.String(),
		}
		if err := app.Models.AuditLog.Insert(ctx, &audit); err != nil {
			// Audit logging failed (partial success)
			logger.Warn("Audit logging failed", "user_wallet_id", userWallet.ID, "error", err)
			app.respondWithJSON(w, http.StatusPartialContent, jsonResponse{
				Error:   false,
				Message: "User wallet retrieved, but audit logging failed",
				Data:    userWallet,
			})
			return
		}
	}

	// Respond with the user wallet
	logger.Info("Successfully retrieved user wallet", "user_wallet_id", userWallet.ID)
	app.respondWithJSON(w, http.StatusOK, jsonResponse{
		Error:   false,
		Message: "User wallet retrieved successfully",
		Data:    userWallet,
	})
}

// GetUserWalletByUserIDHandler retrieves a user wallet by user ID.
// It enforces permission checks, extracts the user ID from the trusted context,
// queries the database for the user wallet, performs audit logging, and responds with the user wallet details.
func (app *Application) GetUserWalletByUserIDHandler(w http.ResponseWriter, r *http.Request) {
	logger := app.Logger.GetLoggerWithContext(r).WithFunctionName("GetUserWalletByUserIDHandler")
	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	// Permission enforcement
	if !app.HasPermission(ctx, "read_user_wallet") {
		app.respondWithError(w, errors.New("forbidden: insufficient permissions"), http.StatusForbidden)
		return
	}

	// Extract User ID from context
	userID := app.getUserIDFromContext(ctx)
	if userID == nil {
		app.respondWithError(w, errors.New("missing user ID in context"), http.StatusBadRequest)
		return
	}

	// Retrieve the user wallet from the database
	userWallet, err := app.Models.UserWallet.GetByID(ctx, *userID)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			app.respondWithError(w, fmt.Errorf("user wallet not found"), http.StatusNotFound)
		} else {
			logger.Error("Failed to retrieve user wallet", "error", err)
			app.respondWithError(w, fmt.Errorf("failed to retrieve user wallet: %w", err), http.StatusInternalServerError)
		}
		return
	}

	// Resolve Audit Action
	action, err := app.Models.Action.GetByName(ctx, "read_user_wallet")
	if err != nil || action == nil {
		logger.Warn("Audit action 'read_user_wallet' not found, attempting to create...", "error", err)
		actionID, createErr := app.Models.Action.CreateIfNotExists(ctx, "read_user_wallet", "Retrieve a user wallet by ID")
		if createErr != nil {
			logger.Error("Failed to create missing audit action", "error", createErr)
			// Allow main operation to proceed
		} else {
			action = &data.Action{ID: actionID}
		}
	}

	// Resolve Audit Entity Type
	entityType, err := app.Models.EntityType.GetByName(ctx, "user_wallet")
	if err != nil || entityType == nil {
		logger.Warn("Audit entity type 'user_wallet' not found, attempting to create...", "error", err)
		entityTypeID, createErr := app.Models.EntityType.CreateIfNotExists(ctx, "user_wallet", "User wallet details")
		if createErr != nil {
			logger.Error("Failed to create missing entity type", "error", createErr)
			// Allow main operation to proceed
		} else {
			entityType = &data.EntityType{ID: entityTypeID}
		}
	}

	// Insert Audit Log (fail gracefully if fails)
	if action != nil && entityType != nil {
		audit := data.AuditLog{
			ID:           uuid.New(),
			UserID:       userID,
			ActionID:     action.ID,
			EntityTypeID: entityType.ID,
			EntityID:     userWallet.ID.String(),
		}
		if err := app.Models.AuditLog.Insert(ctx, &audit); err != nil {
			// Audit logging failed (partial success)
			logger.Warn("Audit logging failed", "user_wallet_id", userWallet.ID, "error", err)
			app.respondWithJSON(w, http.StatusPartialContent, jsonResponse{
				Error:   false,
				Message: "User wallet retrieved, but audit logging failed",
				Data:    userWallet,
			})
			return
		}
	}

	// Success
	logger.Info("Successfully retrieved user wallet", "user_wallet_id", userWallet.ID)
	app.respondWithJSON(w, http.StatusOK, jsonResponse{
		Error:   false,
		Message: "User wallet retrieved successfully",
		Data:    userWallet,
	})
}

// ListUserWalletsHandler handles the retrieval of user wallets.
// It enforces permission checks, extracts identifiers from trusted context,
// queries the database for user wallets, performs audit logging, and responds with the user wallet details.
func (app *Application) ListUserWalletsHandler(w http.ResponseWriter, r *http.Request) {
	logger := app.Logger.GetLoggerWithContext(r).WithFunctionName("ListUserWalletsHandler")
	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	// Permission enforcement
	if !app.HasPermission(ctx, "read_user_wallets") {
		app.respondWithError(w, errors.New("forbidden: insufficient permissions"), http.StatusForbidden)
		return
	}

	// Extract pagination parameters from context
	limit := parseIntOrDefault(app.getContextValueAsString(ctx, ctxPaginationLimit), 20)
	offset := parseIntOrDefault(app.getContextValueAsString(ctx, ctxPaginationOffset), 0)

	// Retrieve user wallets from the database
	userWallets, err := app.Models.UserWallet.List(ctx, limit, offset)
	if err != nil {
		logger.Error("Failed to retrieve user wallets", "error", err)
		app.respondWithError(w, fmt.Errorf("failed to retrieve user wallets: %w", err), http.StatusInternalServerError)
		return
	}

	// Resolve Audit Action
	action, err := app.Models.Action.GetByName(ctx, "read_user_wallets")
	if err != nil || action == nil {
		logger.Warn("Audit action 'read_user_wallets' not found, attempting to create...", "error", err)
		actionID, createErr := app.Models.Action.CreateIfNotExists(ctx, "read_user_wallets", "Retrieve user wallets")
		if createErr != nil {
			logger.Error("Failed to create missing audit action", "error", createErr)
		} else {
			action = &data.Action{ID: actionID}
		}
	}

	// Resolve Audit Entity Type
	entityType, err := app.Models.EntityType.GetByName(ctx, "user_wallet")
	if err != nil || entityType == nil {
		logger.Warn("Audit entity type 'user_wallet' not found, attempting to create...", "error", err)
		entityTypeID, createErr := app.Models.EntityType.CreateIfNotExists(ctx, "user_wallet", "User wallet information")
		if createErr != nil {
			logger.Error("Failed to create missing entity type", "error", createErr)
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
			EntityID:     "user_wallets_listing",
		}
		if err := app.Models.AuditLog.Insert(ctx, &audit); err != nil {
			// Audit logging failed (partial success)
			logger.Warn("Audit logging failed", "error", err)
			app.respondWithJSON(w, http.StatusPartialContent, jsonResponse{
				Error:   false,
				Message: "User wallets retrieved, but audit logging failed",
				Data:    userWallets,
			})
			return
		}
	}

	// Respond success
	logger.Info("User wallets retrieved successfully", "count", len(userWallets))
	app.respondWithJSON(w, http.StatusOK, jsonResponse{
		Error:   false,
		Message: "User wallets retrieved successfully",
		Data:    userWallets,
	})
}
