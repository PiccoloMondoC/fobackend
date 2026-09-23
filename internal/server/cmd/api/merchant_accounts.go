// Package main provides HTTP handlers for merchant platform-account lifecycle
// governance.
//
// focodebase/fobackend/internal/server/cmd/api/merchant_accounts.go
//
// GTM:
//
//	Layer: 2.4 Merchant / Future Offering Domain
//	Release Class: SPINE
//	Reason:
//	  merchant_accounts handler infrastructure governs the canonical merchant
//	  platform-account lifecycle required by Merchant Center, merchant
//	  onboarding, Future Offering participation, merchant program
//	  subscriptions, entitlements, billing, and operational merchant access.
//
// SPINE Rule:
//
//	Keep compiling.
//	Keep production-ready.
//	Preserve exactly one canonical merchant account per merchant.
//	Preserve principal ownership immutability.
//	Preserve explicit account lifecycle operations.
//	Preserve database-owned lifecycle timestamps.
//	Preserve onboarded_at as the original onboarding milestone.
//	Preserve separation between closure, soft deletion, restoration, and
//	permanent deletion.
//	Preserve privileged authorization and audit coverage.
//	Do not expose merchant-account governance as a public API.
//	Do not treat restoration as account reopening.
//	Block deployment if this file breaks merchant-account lifecycle integrity,
//	authorization, auditability, Merchant Center readiness, or dependent
//	merchant monetization and Future Offering workflows.
package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/PiccoloMondoC/focodebase/fobackend/internal/data"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

const (
	merchantAccountEntityType = "merchant_account"

	actionReadMerchantAccount           = "read_merchant_account"
	actionReadDeletedMerchantAccount    = "read_deleted_merchant_account"
	actionReadMerchantAccountByMerchant = "read_merchant_account_by_merchant"
	actionReadDeletedAccountByMerchant  = "read_deleted_merchant_account_by_merchant"
	actionListMerchantAccounts          = "list_merchant_accounts"
	actionListDeletedMerchantAccounts   = "list_deleted_merchant_accounts"
	actionActivateMerchantAccount       = "activate_merchant_account"
	actionSuspendMerchantAccount        = "suspend_merchant_account"
	actionCloseMerchantAccount          = "close_merchant_account"
	actionSoftDeleteMerchantAccount     = "soft_delete_merchant_account"
	actionRestoreMerchantAccount        = "restore_merchant_account"
	actionHardDeleteMerchantAccount     = "hard_delete_merchant_account"
)

type merchantAccountByMerchantInput struct {
	MerchantID uuid.UUID `json:"merchant_id"`
}

type merchantAccountExistenceResponse struct {
	MerchantID uuid.UUID `json:"merchant_id"`
	Exists     bool      `json:"exists"`
}

func merchantAccountHTTPStatus(err error) int {
	if err == nil {
		return http.StatusOK
	}

	msg := strings.ToLower(err.Error())

	switch {
	case strings.Contains(msg, "already exists"):
		return http.StatusConflict

	case strings.Contains(msg, "does not exist"),
		strings.Contains(msg, "not found"):
		return http.StatusNotFound

	case strings.Contains(msg, "cannot be activated"),
		strings.Contains(msg, "cannot be suspended"),
		strings.Contains(msg, "cannot be closed"),
		strings.Contains(msg, "cannot be restored"),
		strings.Contains(msg, "must be closed"),
		strings.Contains(msg, "soft-deleted and cannot"),
		strings.Contains(msg, "restore operation could not be applied"),
		strings.Contains(msg, "cannot perform operation"):
		return http.StatusConflict

	case strings.Contains(msg, "required"),
		strings.Contains(msg, "invalid"),
		strings.Contains(msg, "cannot be nil"),
		strings.Contains(msg, "references a missing"),
		strings.Contains(msg, "must be between"),
		strings.Contains(msg, "must be non-negative"):
		return http.StatusBadRequest

	default:
		return http.StatusInternalServerError
	}
}

func (app *Application) parseMerchantAccountID(
	r *http.Request,
) (uuid.UUID, error) {
	raw := strings.TrimSpace(
		chi.URLParam(r, "merchantAccountID"),
	)
	if raw == "" {
		return uuid.Nil, errors.New(
			"merchant account ID is required",
		)
	}

	id, err := uuid.Parse(raw)
	if err != nil || id == uuid.Nil {
		return uuid.Nil, errors.New(
			"invalid merchant account ID",
		)
	}

	return id, nil
}

func parseMerchantAccountIncludeDeleted(
	r *http.Request,
) (bool, error) {
	raw := strings.TrimSpace(
		r.URL.Query().Get("include_deleted"),
	)

	switch strings.ToLower(raw) {
	case "":
		return false, nil
	case "true":
		return true, nil
	case "false":
		return false, nil
	default:
		return false, errors.New(
			"include_deleted must be true or false",
		)
	}
}

func (app *Application) auditMerchantAccount(
	ctx context.Context,
	userID *uuid.UUID,
	actionName string,
	actionDescription string,
	entityID string,
) error {
	if userID == nil {
		return errors.New(
			"user ID is required for audit logging",
		)
	}

	action, err := app.Models.Action.GetByName(
		ctx,
		actionName,
	)
	if err != nil || action == nil {
		actionID, createErr :=
			app.Models.Action.CreateIfNotExists(
				ctx,
				actionName,
				actionDescription,
			)
		if createErr != nil {
			return fmt.Errorf(
				"resolve audit action %s: %w",
				actionName,
				createErr,
			)
		}

		action = &data.Action{
			ID: actionID,
		}
	}

	entityType, err :=
		app.Models.EntityType.GetByName(
			ctx,
			merchantAccountEntityType,
		)
	if err != nil || entityType == nil {
		entityTypeID, createErr :=
			app.Models.EntityType.CreateIfNotExists(
				ctx,
				merchantAccountEntityType,
				"Merchant account entity",
			)
		if createErr != nil {
			return fmt.Errorf(
				"resolve audit entity type %s: %w",
				merchantAccountEntityType,
				createErr,
			)
		}

		entityType = &data.EntityType{
			ID: entityTypeID,
		}
	}

	auditLog := &data.AuditLog{
		ID:           uuid.New(),
		UserID:       userID,
		ActionID:     action.ID,
		EntityTypeID: entityType.ID,
		EntityID:     entityID,
	}

	if err := app.Models.AuditLog.Insert(
		ctx,
		auditLog,
	); err != nil {
		return fmt.Errorf(
			"insert merchant account audit log: %w",
			err,
		)
	}

	return nil
}

// GetMerchantAccountByIDHandler retrieves a non-deleted merchant account by
// canonical account ID.
func (app *Application) GetMerchantAccountByIDHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	logger := app.Logger.
		GetLoggerWithContext(r).
		WithFunctionName(
			"GetMerchantAccountByIDHandler",
		)

	ctx, cancel := context.WithTimeout(
		r.Context(),
		cfgTimeout,
	)
	defer cancel()

	if !app.HasPermission(
		ctx,
		actionReadMerchantAccount,
	) {
		app.respondWithError(
			w,
			errors.New(
				"forbidden: insufficient permissions",
			),
			http.StatusForbidden,
		)
		return
	}

	userID := app.getUserIDFromContext(ctx)
	if userID == nil {
		app.respondWithError(
			w,
			errors.New(
				"user ID not found in context",
			),
			http.StatusUnauthorized,
		)
		return
	}

	accountID, err :=
		app.parseMerchantAccountID(r)
	if err != nil {
		app.respondWithError(
			w,
			err,
			http.StatusBadRequest,
		)
		return
	}

	account, err :=
		app.Models.MerchantAccount.GetByID(
			ctx,
			accountID,
		)
	if err != nil {
		logger.Error(
			"Get merchant account by ID failed",
			"merchant_account_id",
			accountID,
			"error",
			err,
		)
		app.respondWithError(
			w,
			err,
			merchantAccountHTTPStatus(err),
		)
		return
	}

	if account == nil {
		app.respondWithError(
			w,
			errors.New(
				"merchant account not found",
			),
			http.StatusNotFound,
		)
		return
	}

	if err := app.auditMerchantAccount(
		ctx,
		userID,
		actionReadMerchantAccount,
		"Read a merchant account",
		account.ID.String(),
	); err != nil {
		logger.Warn(
			"Audit logging failed",
			"merchant_account_id",
			account.ID,
			"error",
			err,
		)
		app.respondWithJSON(
			w,
			http.StatusPartialContent,
			jsonResponse{
				Error: false,
				Message: "Merchant account retrieved, " +
					"but audit logging failed",
				Data: account,
			},
		)
		return
	}

	app.respondWithJSON(
		w,
		http.StatusOK,
		jsonResponse{
			Error: false,
			Message: "Merchant account retrieved " +
				"successfully",
			Data: account,
		},
	)
}

// GetMerchantAccountByIDIncludingDeletedHandler retrieves a merchant account
// regardless of soft-delete state.
func (app *Application) GetMerchantAccountByIDIncludingDeletedHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	logger := app.Logger.
		GetLoggerWithContext(r).
		WithFunctionName(
			"GetMerchantAccountByIDIncludingDeletedHandler",
		)

	ctx, cancel := context.WithTimeout(
		r.Context(),
		cfgTimeout,
	)
	defer cancel()

	if !app.HasPermission(
		ctx,
		actionReadDeletedMerchantAccount,
	) {
		app.respondWithError(
			w,
			errors.New(
				"forbidden: insufficient permissions",
			),
			http.StatusForbidden,
		)
		return
	}

	userID := app.getUserIDFromContext(ctx)
	if userID == nil {
		app.respondWithError(
			w,
			errors.New(
				"user ID not found in context",
			),
			http.StatusUnauthorized,
		)
		return
	}

	accountID, err :=
		app.parseMerchantAccountID(r)
	if err != nil {
		app.respondWithError(
			w,
			err,
			http.StatusBadRequest,
		)
		return
	}

	account, err :=
		app.Models.MerchantAccount.
			GetByIDIncludingDeleted(
				ctx,
				accountID,
			)
	if err != nil {
		logger.Error(
			"Get merchant account including deleted failed",
			"merchant_account_id",
			accountID,
			"error",
			err,
		)
		app.respondWithError(
			w,
			err,
			merchantAccountHTTPStatus(err),
		)
		return
	}

	if account == nil {
		app.respondWithError(
			w,
			errors.New(
				"merchant account not found",
			),
			http.StatusNotFound,
		)
		return
	}

	if err := app.auditMerchantAccount(
		ctx,
		userID,
		actionReadDeletedMerchantAccount,
		"Read a merchant account including deleted",
		account.ID.String(),
	); err != nil {
		logger.Warn(
			"Audit logging failed",
			"merchant_account_id",
			account.ID,
			"error",
			err,
		)
		app.respondWithJSON(
			w,
			http.StatusPartialContent,
			jsonResponse{
				Error: false,
				Message: "Merchant account retrieved, " +
					"but audit logging failed",
				Data: account,
			},
		)
		return
	}

	app.respondWithJSON(
		w,
		http.StatusOK,
		jsonResponse{
			Error: false,
			Message: "Merchant account retrieved " +
				"successfully",
			Data: account,
		},
	)
}

// GetMerchantAccountByMerchantIDHandler retrieves the non-deleted canonical
// account belonging to a merchant.
func (app *Application) GetMerchantAccountByMerchantIDHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	logger := app.Logger.
		GetLoggerWithContext(r).
		WithFunctionName(
			"GetMerchantAccountByMerchantIDHandler",
		)

	ctx, cancel := context.WithTimeout(
		r.Context(),
		cfgTimeout,
	)
	defer cancel()

	if !app.HasPermission(
		ctx,
		actionReadMerchantAccountByMerchant,
	) {
		app.respondWithError(
			w,
			errors.New(
				"forbidden: insufficient permissions",
			),
			http.StatusForbidden,
		)
		return
	}

	userID := app.getUserIDFromContext(ctx)
	if userID == nil {
		app.respondWithError(
			w,
			errors.New(
				"user ID not found in context",
			),
			http.StatusUnauthorized,
		)
		return
	}

	var input merchantAccountByMerchantInput

	if err := app.readJSON(
		w,
		r,
		&input,
	); err != nil {
		app.respondWithError(
			w,
			fmt.Errorf(
				"invalid JSON input: %v",
				err,
			),
			http.StatusBadRequest,
		)
		return
	}

	if input.MerchantID == uuid.Nil {
		app.respondWithError(
			w,
			errors.New("merchant ID is required"),
			http.StatusBadRequest,
		)
		return
	}

	account, err :=
		app.Models.MerchantAccount.
			GetByMerchantID(
				ctx,
				input.MerchantID,
			)
	if err != nil {
		logger.Error(
			"Get merchant account by merchant ID failed",
			"merchant_id",
			input.MerchantID,
			"error",
			err,
		)
		app.respondWithError(
			w,
			err,
			merchantAccountHTTPStatus(err),
		)
		return
	}

	if account == nil {
		app.respondWithError(
			w,
			errors.New(
				"merchant account not found",
			),
			http.StatusNotFound,
		)
		return
	}

	if err := app.auditMerchantAccount(
		ctx,
		userID,
		actionReadMerchantAccountByMerchant,
		"Read a merchant account by merchant ID",
		account.ID.String(),
	); err != nil {
		logger.Warn(
			"Audit logging failed",
			"merchant_account_id",
			account.ID,
			"error",
			err,
		)
		app.respondWithJSON(
			w,
			http.StatusPartialContent,
			jsonResponse{
				Error: false,
				Message: "Merchant account retrieved, " +
					"but audit logging failed",
				Data: account,
			},
		)
		return
	}

	app.respondWithJSON(
		w,
		http.StatusOK,
		jsonResponse{
			Error: false,
			Message: "Merchant account retrieved " +
				"successfully",
			Data: account,
		},
	)
}

// GetMerchantAccountByMerchantIDIncludingDeletedHandler retrieves a merchant
// account by merchant ID regardless of soft-delete state.
func (app *Application) GetMerchantAccountByMerchantIDIncludingDeletedHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	logger := app.Logger.
		GetLoggerWithContext(r).
		WithFunctionName(
			"GetMerchantAccountByMerchantIDIncludingDeletedHandler",
		)

	ctx, cancel := context.WithTimeout(
		r.Context(),
		cfgTimeout,
	)
	defer cancel()

	if !app.HasPermission(
		ctx,
		actionReadDeletedAccountByMerchant,
	) {
		app.respondWithError(
			w,
			errors.New(
				"forbidden: insufficient permissions",
			),
			http.StatusForbidden,
		)
		return
	}

	userID := app.getUserIDFromContext(ctx)
	if userID == nil {
		app.respondWithError(
			w,
			errors.New(
				"user ID not found in context",
			),
			http.StatusUnauthorized,
		)
		return
	}

	var input merchantAccountByMerchantInput

	if err := app.readJSON(
		w,
		r,
		&input,
	); err != nil {
		app.respondWithError(
			w,
			fmt.Errorf(
				"invalid JSON input: %v",
				err,
			),
			http.StatusBadRequest,
		)
		return
	}

	if input.MerchantID == uuid.Nil {
		app.respondWithError(
			w,
			errors.New("merchant ID is required"),
			http.StatusBadRequest,
		)
		return
	}

	account, err :=
		app.Models.MerchantAccount.
			GetByMerchantIDIncludingDeleted(
				ctx,
				input.MerchantID,
			)
	if err != nil {
		logger.Error(
			"Get merchant account by merchant ID including deleted failed",
			"merchant_id",
			input.MerchantID,
			"error",
			err,
		)
		app.respondWithError(
			w,
			err,
			merchantAccountHTTPStatus(err),
		)
		return
	}

	if account == nil {
		app.respondWithError(
			w,
			errors.New(
				"merchant account not found",
			),
			http.StatusNotFound,
		)
		return
	}

	if err := app.auditMerchantAccount(
		ctx,
		userID,
		actionReadDeletedAccountByMerchant,
		"Read a merchant account by merchant ID including deleted",
		account.ID.String(),
	); err != nil {
		logger.Warn(
			"Audit logging failed",
			"merchant_account_id",
			account.ID,
			"error",
			err,
		)
		app.respondWithJSON(
			w,
			http.StatusPartialContent,
			jsonResponse{
				Error: false,
				Message: "Merchant account retrieved, " +
					"but audit logging failed",
				Data: account,
			},
		)
		return
	}

	app.respondWithJSON(
		w,
		http.StatusOK,
		jsonResponse{
			Error: false,
			Message: "Merchant account retrieved " +
				"successfully",
			Data: account,
		},
	)
}

// ExistsMerchantAccountByMerchantIDHandler reports whether the canonical
// merchant-account row exists for a merchant, including a soft-deleted row.
func (app *Application) ExistsMerchantAccountByMerchantIDHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	logger := app.Logger.
		GetLoggerWithContext(r).
		WithFunctionName(
			"ExistsMerchantAccountByMerchantIDHandler",
		)

	ctx, cancel := context.WithTimeout(
		r.Context(),
		cfgTimeout,
	)
	defer cancel()

	if !app.HasPermission(
		ctx,
		actionReadMerchantAccountByMerchant,
	) {
		app.respondWithError(
			w,
			errors.New(
				"forbidden: insufficient permissions",
			),
			http.StatusForbidden,
		)
		return
	}

	userID := app.getUserIDFromContext(ctx)
	if userID == nil {
		app.respondWithError(
			w,
			errors.New(
				"user ID not found in context",
			),
			http.StatusUnauthorized,
		)
		return
	}

	var input merchantAccountByMerchantInput

	if err := app.readJSON(
		w,
		r,
		&input,
	); err != nil {
		app.respondWithError(
			w,
			fmt.Errorf(
				"invalid JSON input: %v",
				err,
			),
			http.StatusBadRequest,
		)
		return
	}

	if input.MerchantID == uuid.Nil {
		app.respondWithError(
			w,
			errors.New("merchant ID is required"),
			http.StatusBadRequest,
		)
		return
	}

	exists, err :=
		app.Models.MerchantAccount.ExistsByMerchantID(
			ctx,
			input.MerchantID,
		)
	if err != nil {
		logger.Error(
			"Merchant account existence check failed",
			"merchant_id",
			input.MerchantID,
			"error",
			err,
		)
		app.respondWithError(
			w,
			err,
			merchantAccountHTTPStatus(err),
		)
		return
	}

	if err := app.auditMerchantAccount(
		ctx,
		userID,
		actionReadMerchantAccountByMerchant,
		"Check merchant account existence by merchant ID",
		input.MerchantID.String(),
	); err != nil {
		logger.Warn(
			"Audit logging failed",
			"merchant_id",
			input.MerchantID,
			"error",
			err,
		)
		app.respondWithJSON(
			w,
			http.StatusPartialContent,
			jsonResponse{
				Error: false,
				Message: "Merchant account existence checked, " +
					"but audit logging failed",
				Data: merchantAccountExistenceResponse{
					MerchantID: input.MerchantID,
					Exists:     exists,
				},
			},
		)
		return
	}

	app.respondWithJSON(
		w,
		http.StatusOK,
		jsonResponse{
			Error: false,
			Message: "Merchant account existence checked " +
				"successfully",
			Data: merchantAccountExistenceResponse{
				MerchantID: input.MerchantID,
				Exists:     exists,
			},
		},
	)
}

// GetAllMerchantAccountsHandler lists merchant accounts using deterministic
// offset pagination.
func (app *Application) GetAllMerchantAccountsHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	logger := app.Logger.
		GetLoggerWithContext(r).
		WithFunctionName(
			"GetAllMerchantAccountsHandler",
		)

	ctx, cancel := context.WithTimeout(
		r.Context(),
		cfgTimeout,
	)
	defer cancel()

	includeDeleted, err :=
		parseMerchantAccountIncludeDeleted(r)
	if err != nil {
		app.respondWithError(
			w,
			err,
			http.StatusBadRequest,
		)
		return
	}

	requiredPermission :=
		actionListMerchantAccounts
	auditAction :=
		actionListMerchantAccounts
	auditDescription :=
		"List merchant accounts"

	if includeDeleted {
		requiredPermission =
			actionListDeletedMerchantAccounts
		auditAction =
			actionListDeletedMerchantAccounts
		auditDescription =
			"List merchant accounts including deleted"
	}

	if !app.HasPermission(
		ctx,
		requiredPermission,
	) {
		app.respondWithError(
			w,
			errors.New(
				"forbidden: insufficient permissions",
			),
			http.StatusForbidden,
		)
		return
	}

	userID := app.getUserIDFromContext(ctx)
	if userID == nil {
		app.respondWithError(
			w,
			errors.New(
				"user ID not found in context",
			),
			http.StatusUnauthorized,
		)
		return
	}

	limit := parseIntOrDefault(
		app.getContextValueAsString(
			ctx,
			ctxPaginationLimit,
		),
		20,
	)
	offset := parseIntOrDefault(
		app.getContextValueAsString(
			ctx,
			ctxPaginationOffset,
		),
		0,
	)

	if limit < 1 || limit > 100 {
		app.respondWithError(
			w,
			errors.New(
				"limit must be between 1 and 100",
			),
			http.StatusBadRequest,
		)
		return
	}

	if offset < 0 {
		app.respondWithError(
			w,
			errors.New(
				"offset must be non-negative",
			),
			http.StatusBadRequest,
		)
		return
	}

	accounts, err :=
		app.Models.MerchantAccount.GetAll(
			ctx,
			includeDeleted,
			limit,
			offset,
		)
	if err != nil {
		logger.Error(
			"Get all merchant accounts failed",
			"include_deleted",
			includeDeleted,
			"limit",
			limit,
			"offset",
			offset,
			"error",
			err,
		)
		app.respondWithError(
			w,
			err,
			merchantAccountHTTPStatus(err),
		)
		return
	}

	if err := app.auditMerchantAccount(
		ctx,
		userID,
		auditAction,
		auditDescription,
		"*",
	); err != nil {
		logger.Warn(
			"Audit logging failed",
			"include_deleted",
			includeDeleted,
			"error",
			err,
		)
		app.respondWithJSON(
			w,
			http.StatusPartialContent,
			jsonResponse{
				Error: false,
				Message: "Merchant accounts retrieved, " +
					"but audit logging failed",
				Data: accounts,
			},
		)
		return
	}

	app.respondWithJSON(
		w,
		http.StatusOK,
		jsonResponse{
			Error: false,
			Message: "Merchant accounts retrieved " +
				"successfully",
			Data: accounts,
		},
	)
}

type merchantAccountTransition func(
	context.Context,
	uuid.UUID,
) (*data.MerchantAccount, error)

func (app *Application) transitionMerchantAccount(
	w http.ResponseWriter,
	r *http.Request,
	functionName string,
	actionName string,
	actionDescription string,
	successMessage string,
	transition merchantAccountTransition,
) {
	logger := app.Logger.
		GetLoggerWithContext(r).
		WithFunctionName(functionName)

	ctx, cancel := context.WithTimeout(
		r.Context(),
		cfgTimeout,
	)
	defer cancel()

	if !app.HasPermission(
		ctx,
		actionName,
	) {
		app.respondWithError(
			w,
			errors.New(
				"forbidden: insufficient permissions",
			),
			http.StatusForbidden,
		)
		return
	}

	userID := app.getUserIDFromContext(ctx)
	if userID == nil {
		app.respondWithError(
			w,
			errors.New(
				"user ID not found in context",
			),
			http.StatusUnauthorized,
		)
		return
	}

	accountID, err :=
		app.parseMerchantAccountID(r)
	if err != nil {
		app.respondWithError(
			w,
			err,
			http.StatusBadRequest,
		)
		return
	}

	account, err :=
		transition(ctx, accountID)
	if err != nil {
		logger.Error(
			"Merchant account lifecycle transition failed",
			"merchant_account_id",
			accountID,
			"operation",
			actionName,
			"error",
			err,
		)
		app.respondWithError(
			w,
			err,
			merchantAccountHTTPStatus(err),
		)
		return
	}

	if err := app.auditMerchantAccount(
		ctx,
		userID,
		actionName,
		actionDescription,
		account.ID.String(),
	); err != nil {
		logger.Warn(
			"Audit logging failed",
			"merchant_account_id",
			account.ID,
			"operation",
			actionName,
			"error",
			err,
		)
		app.respondWithJSON(
			w,
			http.StatusPartialContent,
			jsonResponse{
				Error: false,
				Message: successMessage +
					", but audit logging failed",
				Data: account,
			},
		)
		return
	}

	logger.Info(
		"Merchant account lifecycle transition successful",
		"merchant_account_id",
		account.ID,
		"merchant_id",
		account.MerchantID,
		"account_status",
		account.AccountStatus,
		"operation",
		actionName,
	)

	app.respondWithJSON(
		w,
		http.StatusOK,
		jsonResponse{
			Error:   false,
			Message: successMessage,
			Data:    account,
		},
	)
}

// ActivateMerchantAccountHandler activates a pending or suspended merchant
// account.
func (app *Application) ActivateMerchantAccountHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	app.transitionMerchantAccount(
		w,
		r,
		"ActivateMerchantAccountHandler",
		actionActivateMerchantAccount,
		"Activate a merchant account",
		"Merchant account activated successfully",
		func(
			ctx context.Context,
			id uuid.UUID,
		) (*data.MerchantAccount, error) {
			return app.Models.MerchantAccount.
				Activate(ctx, id)
		},
	)
}

// SuspendMerchantAccountHandler suspends an active merchant account.
func (app *Application) SuspendMerchantAccountHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	app.transitionMerchantAccount(
		w,
		r,
		"SuspendMerchantAccountHandler",
		actionSuspendMerchantAccount,
		"Suspend a merchant account",
		"Merchant account suspended successfully",
		func(
			ctx context.Context,
			id uuid.UUID,
		) (*data.MerchantAccount, error) {
			return app.Models.MerchantAccount.
				Suspend(ctx, id)
		},
	)
}

// CloseMerchantAccountHandler closes a pending, active, or suspended merchant
// account.
func (app *Application) CloseMerchantAccountHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	app.transitionMerchantAccount(
		w,
		r,
		"CloseMerchantAccountHandler",
		actionCloseMerchantAccount,
		"Close a merchant account",
		"Merchant account closed successfully",
		func(
			ctx context.Context,
			id uuid.UUID,
		) (*data.MerchantAccount, error) {
			return app.Models.MerchantAccount.
				Close(ctx, id)
		},
	)
}

// SoftDeleteMerchantAccountHandler soft-deletes a closed merchant account.
func (app *Application) SoftDeleteMerchantAccountHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	logger := app.Logger.
		GetLoggerWithContext(r).
		WithFunctionName(
			"SoftDeleteMerchantAccountHandler",
		)

	ctx, cancel := context.WithTimeout(
		r.Context(),
		cfgTimeout,
	)
	defer cancel()

	if !app.HasPermission(
		ctx,
		actionSoftDeleteMerchantAccount,
	) {
		app.respondWithError(
			w,
			errors.New(
				"forbidden: insufficient permissions",
			),
			http.StatusForbidden,
		)
		return
	}

	userID := app.getUserIDFromContext(ctx)
	if userID == nil {
		app.respondWithError(
			w,
			errors.New(
				"user ID not found in context",
			),
			http.StatusUnauthorized,
		)
		return
	}

	accountID, err :=
		app.parseMerchantAccountID(r)
	if err != nil {
		app.respondWithError(
			w,
			err,
			http.StatusBadRequest,
		)
		return
	}

	if err := app.Models.MerchantAccount.
		SoftDelete(ctx, accountID); err != nil {
		logger.Error(
			"Soft delete merchant account failed",
			"merchant_account_id",
			accountID,
			"error",
			err,
		)
		app.respondWithError(
			w,
			err,
			merchantAccountHTTPStatus(err),
		)
		return
	}

	if err := app.auditMerchantAccount(
		ctx,
		userID,
		actionSoftDeleteMerchantAccount,
		"Soft-delete a merchant account",
		accountID.String(),
	); err != nil {
		logger.Warn(
			"Audit logging failed",
			"merchant_account_id",
			accountID,
			"error",
			err,
		)
		app.respondWithJSON(
			w,
			http.StatusPartialContent,
			jsonResponse{
				Error: false,
				Message: "Merchant account soft-deleted, " +
					"but audit logging failed",
				Data: accountID,
			},
		)
		return
	}

	app.respondWithJSON(
		w,
		http.StatusOK,
		jsonResponse{
			Error: false,
			Message: "Merchant account soft-deleted " +
				"successfully",
			Data: accountID,
		},
	)
}

// RestoreMerchantAccountHandler removes soft deletion from a closed merchant
// account without reopening or activating it.
func (app *Application) RestoreMerchantAccountHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	logger := app.Logger.
		GetLoggerWithContext(r).
		WithFunctionName(
			"RestoreMerchantAccountHandler",
		)

	ctx, cancel := context.WithTimeout(
		r.Context(),
		cfgTimeout,
	)
	defer cancel()

	if !app.HasPermission(
		ctx,
		actionRestoreMerchantAccount,
	) {
		app.respondWithError(
			w,
			errors.New(
				"forbidden: insufficient permissions",
			),
			http.StatusForbidden,
		)
		return
	}

	userID := app.getUserIDFromContext(ctx)
	if userID == nil {
		app.respondWithError(
			w,
			errors.New(
				"user ID not found in context",
			),
			http.StatusUnauthorized,
		)
		return
	}

	accountID, err :=
		app.parseMerchantAccountID(r)
	if err != nil {
		app.respondWithError(
			w,
			err,
			http.StatusBadRequest,
		)
		return
	}

	if err := app.Models.MerchantAccount.
		Restore(ctx, accountID); err != nil {
		logger.Error(
			"Restore merchant account failed",
			"merchant_account_id",
			accountID,
			"error",
			err,
		)
		app.respondWithError(
			w,
			err,
			merchantAccountHTTPStatus(err),
		)
		return
	}

	if err := app.auditMerchantAccount(
		ctx,
		userID,
		actionRestoreMerchantAccount,
		"Restore a closed merchant account",
		accountID.String(),
	); err != nil {
		logger.Warn(
			"Audit logging failed",
			"merchant_account_id",
			accountID,
			"error",
			err,
		)
		app.respondWithJSON(
			w,
			http.StatusPartialContent,
			jsonResponse{
				Error: false,
				Message: "Merchant account restored, " +
					"but audit logging failed",
				Data: accountID,
			},
		)
		return
	}

	app.respondWithJSON(
		w,
		http.StatusOK,
		jsonResponse{
			Error: false,
			Message: "Merchant account restored successfully; " +
				"account remains closed",
			Data: accountID,
		},
	)
}

// HardDeleteMerchantAccountHandler permanently deletes a closed,
// soft-deleted merchant account.
func (app *Application) HardDeleteMerchantAccountHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	logger := app.Logger.
		GetLoggerWithContext(r).
		WithFunctionName(
			"HardDeleteMerchantAccountHandler",
		)

	ctx, cancel := context.WithTimeout(
		r.Context(),
		cfgTimeout,
	)
	defer cancel()

	if !app.HasPermission(
		ctx,
		actionHardDeleteMerchantAccount,
	) {
		app.respondWithError(
			w,
			errors.New(
				"forbidden: insufficient permissions",
			),
			http.StatusForbidden,
		)
		return
	}

	userID := app.getUserIDFromContext(ctx)
	if userID == nil {
		app.respondWithError(
			w,
			errors.New(
				"user ID not found in context",
			),
			http.StatusUnauthorized,
		)
		return
	}

	accountID, err :=
		app.parseMerchantAccountID(r)
	if err != nil {
		app.respondWithError(
			w,
			err,
			http.StatusBadRequest,
		)
		return
	}

	if err := app.Models.MerchantAccount.
		HardDelete(ctx, accountID); err != nil {
		logger.Error(
			"Hard delete merchant account failed",
			"merchant_account_id",
			accountID,
			"error",
			err,
		)
		app.respondWithError(
			w,
			err,
			merchantAccountHTTPStatus(err),
		)
		return
	}

	if err := app.auditMerchantAccount(
		ctx,
		userID,
		actionHardDeleteMerchantAccount,
		"Permanently delete a merchant account",
		accountID.String(),
	); err != nil {
		logger.Warn(
			"Audit logging failed",
			"merchant_account_id",
			accountID,
			"error",
			err,
		)
		app.respondWithJSON(
			w,
			http.StatusPartialContent,
			jsonResponse{
				Error: false,
				Message: "Merchant account permanently deleted, " +
					"but audit logging failed",
				Data: accountID,
			},
		)
		return
	}

	logger.Info(
		"Merchant account permanently deleted",
		"merchant_account_id",
		accountID,
	)

	app.respondWithJSON(
		w,
		http.StatusOK,
		jsonResponse{
			Error: false,
			Message: "Merchant account permanently deleted " +
				"successfully",
			Data: accountID,
		},
	)
}
