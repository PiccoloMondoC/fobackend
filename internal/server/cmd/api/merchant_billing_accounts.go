// Package main provides HTTP handlers for merchant billing accounts.
//
// focodebase/fobackend/internal/server/cmd/api/merchant_billing_accounts.go
//
// GTM:
//
//	Layer: 2.4.b Commerce Architecture / Monetization Layer
//	Release Class: SPINE
//	Reason:
//	  merchant_billing_accounts provides the canonical per-merchant billing
//	  relationship and billing-currency anchor used by monetization workflows.
//	  This handler exposes privileged creation, retrieval, bounded lifecycle
//	  review, and guarded administrative lifecycle transitions.
//
// Domain Boundary:
//
//	A merchant billing account is not:
//
//	  - a merchant-held balance;
//	  - a deposit, wallet, treasury account, escrow account, or stored value;
//	  - an invoice, fee calculation, or billing-ledger entry;
//	  - a platform credit account;
//	  - a payment method; or
//	  - a payment-provider account.
//
//	This handler records or exposes only:
//
//	  - the merchant owning the billing relationship;
//	  - the lifecycle state of that relationship; and
//	  - the canonical currency for new billing obligations.
//
//	Status does not erase or invalidate obligations already incurred.
//	Suspension or closure must not prevent settlement, reconciliation,
//	refunds, disputes, audit, or historical reads where those operations
//	remain legally or operationally required.
//
//	This handler does not determine:
//
//	  - whether a fee, invoice, or collection is enabled;
//	  - what a merchant owes;
//	  - why Administration suspends or closes an account;
//	  - whether another workflow may proceed in a given status;
//	  - which payment method or payment provider is used; or
//	  - commercial currency availability.
//
//	Those decisions belong to Administration-governed configuration,
//	service orchestration, authorization, Commerce Architecture, and Merchant
//	Payments Architecture.
//
// Consumer Identity Sovereignty:
//
//	This domain contains no consumer identity or Future Offering engagement
//	data and exposes no merchant-facing consumer disclosure capability.
//	Nothing in this handler weakens the consumer identity sovereignty
//	invariant established by BEG section 18.6C.
//
// Handler Boundary:
//
//	This file implements only the merchant billing account HTTP boundary. All
//	domain operations delegate through app.InternalServices. It does not call
//	app.Models.MerchantBillingAccount directly or implement service
//	orchestration, invoice generation, fee calculation, payment execution,
//	async processing, or commercial policy.
//
// SPINE Rule:
//
//	Keep compiling.
//	Keep production-ready.
//	Preserve exactly one billing account per merchant.
//	Preserve explicit creation currency.
//	Preserve guarded lifecycle transitions.
//	Preserve terminal closed-state history.
//	Preserve currency immutability.
//	Preserve strict route-level and handler-level authorization.
//	Preserve audit coverage for every operation.
//	Preserve deterministic keyset pagination.
//	Never expose generic status mutation, reopening, currency mutation,
//	or destructive deletion.
//	Never treat status as cancellation of existing obligations.
//	Never persist or imply merchant-held funds.
//	Never disclose consumer identity or engagement information.
//	Never implement commercial or operational policy in this file.
//	Block deployment if this file breaks build, authorization, audit
//	accountability, lifecycle integrity, currency integrity, or pagination
//	determinism.
//	Never bypass app.InternalServices to call the merchant billing account
//	data model directly.
package main

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/PiccoloMondoC/focodebase/fobackend/internal/data"

	"github.com/google/uuid"
)

// -----------------------------------------------------------------------------
// Permission, action, and entity vocabulary
// -----------------------------------------------------------------------------

const (
	merchantBillingAccountEntityType = "merchant_billing_account"

	merchantBillingAccountEntityTypeDescription = "Merchant billing relationship and canonical billing-currency entity"

	actionCreateMerchantBillingAccount = "create_merchant_billing_account"

	actionReadMerchantBillingAccount = "read_merchant_billing_account"

	actionListMerchantBillingAccounts = "list_merchant_billing_accounts"

	actionSuspendMerchantBillingAccount = "suspend_merchant_billing_account"

	actionReactivateMerchantBillingAccount = "reactivate_merchant_billing_account"

	actionCloseMerchantBillingAccount = "close_merchant_billing_account"

	merchantBillingAccountDefaultListLimit = 20
	merchantBillingAccountMaximumListLimit = 100
)

// -----------------------------------------------------------------------------
// Error translation
// -----------------------------------------------------------------------------

// merchantBillingAccountHTTPStatus translates merchant billing account
// domain errors into stable HTTP status codes.
//
// Classification relies exclusively on exported data-layer sentinel errors.
// Diagnostic error wording is not part of the HTTP contract.
func merchantBillingAccountHTTPStatus(err error) int {
	if err == nil {
		return http.StatusOK
	}

	switch {
	case errors.Is(
		err,
		data.ErrMerchantBillingAccountInvalidInput,
	):
		return http.StatusBadRequest

	case errors.Is(err, data.ErrMerchantNotFound),
		errors.Is(
			err,
			data.ErrMerchantBillingAccountNotFound,
		):
		return http.StatusNotFound

	case errors.Is(
		err,
		data.ErrMerchantBillingAccountAlreadyExists,
	),
		errors.Is(
			err,
			data.ErrMerchantBillingAccountInvalidTransition,
		),
		errors.Is(
			err,
			data.ErrMerchantBillingAccountInvalidState,
		),
		errors.Is(
			err,
			data.ErrMerchantBillingAccountMutationConflict,
		):
		return http.StatusConflict

	default:
		return http.StatusInternalServerError
	}
}

// respondWithMerchantBillingAccountError translates a model error into the
// merchant billing account HTTP contract without exposing unclassified
// persistence, driver, schema, or infrastructure details.
//
// Caller-safe domain errors retain their canonical diagnostic context.
// Unclassified errors are logged by the calling handler and returned to the
// client only as a stable internal-error response.
func (app *Application) respondWithMerchantBillingAccountError(
	w http.ResponseWriter,
	err error,
) {
	status := merchantBillingAccountHTTPStatus(err)

	if status == http.StatusInternalServerError {
		app.respondWithError(
			w,
			errors.New(
				"failed to process merchant billing account request",
			),
			status,
		)
		return
	}

	app.respondWithError(
		w,
		err,
		status,
	)
}

// -----------------------------------------------------------------------------
// Query parsing
// -----------------------------------------------------------------------------

// parseMerchantBillingAccountStatusParam extracts and validates the required
// status query parameter using the canonical data-layer vocabulary.
func parseMerchantBillingAccountStatusParam(
	r *http.Request,
) (data.MerchantBillingAccountStatus, error) {
	raw := strings.TrimSpace(
		r.URL.Query().Get("status"),
	)
	if raw == "" {
		return "", errors.New(
			"status is required",
		)
	}

	status :=
		data.NormalizeMerchantBillingAccountStatus(
			data.MerchantBillingAccountStatus(raw),
		)

	if !data.IsValidMerchantBillingAccountStatus(status) {
		return "", errors.New(
			"status must be one of: active, suspended, closed",
		)
	}

	return status, nil
}

// parseMerchantBillingAccountLimit parses the optional bounded list limit.
//
// Missing input uses the handler default. Present malformed or out-of-range
// input is rejected rather than silently replaced.
func parseMerchantBillingAccountLimit(
	r *http.Request,
) (int, error) {
	raw := strings.TrimSpace(
		r.URL.Query().Get("limit"),
	)
	if raw == "" {
		return merchantBillingAccountDefaultListLimit, nil
	}

	limit, err := strconv.Atoi(raw)
	if err != nil {
		return 0, errors.New(
			"limit must be an integer",
		)
	}

	if limit < 1 ||
		limit > merchantBillingAccountMaximumListLimit {
		return 0, errors.New(
			"limit must be between 1 and 100",
		)
	}

	return limit, nil
}

// parseMerchantBillingAccountListCursor parses the optional keyset cursor.
//
// before_created_at and before_merchant_id must either both be absent or both
// be present. Malformed or incomplete cursors are rejected.
func parseMerchantBillingAccountListCursor(
	r *http.Request,
) (*time.Time, *uuid.UUID, error) {
	rawCreatedAt := strings.TrimSpace(
		r.URL.Query().Get("before_created_at"),
	)
	rawMerchantID := strings.TrimSpace(
		r.URL.Query().Get("before_merchant_id"),
	)

	if rawCreatedAt == "" && rawMerchantID == "" {
		return nil, nil, nil
	}

	if rawCreatedAt == "" || rawMerchantID == "" {
		return nil, nil, errors.New(
			"before_created_at and before_merchant_id " +
				"must be supplied together",
		)
	}

	createdAt, err :=
		time.Parse(time.RFC3339, rawCreatedAt)
	if err != nil {
		return nil, nil, errors.New(
			"before_created_at must be a valid RFC3339 timestamp",
		)
	}

	merchantID, err := uuid.Parse(rawMerchantID)
	if err != nil || merchantID == uuid.Nil {
		return nil, nil, errors.New(
			"before_merchant_id must be a valid non-nil UUID",
		)
	}

	createdAtUTC := createdAt.UTC()

	return &createdAtUTC, &merchantID, nil
}

// -----------------------------------------------------------------------------
// Request and response DTOs
// -----------------------------------------------------------------------------

// createMerchantBillingAccountRequest contains only caller-owned creation
// input.
//
// Merchant identity belongs to the route. Status and timestamps belong to the
// data layer. Currency cannot subsequently be mutated through this domain.
type createMerchantBillingAccountRequest struct {
	Currency string `json:"currency"`
}

// merchantBillingAccountLifecycleResponse reports the deterministic result of
// a successfully completed lifecycle operation.
//
// The handler may safely report the target status because successful model
// completion guarantees either that the guarded transition occurred or that
// the account was already in that target state.
type merchantBillingAccountLifecycleResponse struct {
	MerchantID uuid.UUID                         `json:"merchant_id"`
	Status     data.MerchantBillingAccountStatus `json:"status"`
}

// -----------------------------------------------------------------------------
// Create
// -----------------------------------------------------------------------------

// CreateMerchantBillingAccountHandler creates the merchant's one canonical
// billing account using an explicitly supplied billing currency.
func (app *Application) CreateMerchantBillingAccountHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	logger := app.Logger.
		GetLoggerWithContext(r).
		WithFunctionName(
			"CreateMerchantBillingAccountHandler",
		)

	ctx, cancel := context.WithTimeout(
		r.Context(),
		cfgTimeout,
	)
	defer cancel()

	if !app.HasPermission(
		ctx,
		actionCreateMerchantBillingAccount,
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

	merchantID, err :=
		app.parseMerchantIDPathParam(r)
	if err != nil {
		app.respondWithError(
			w,
			err,
			http.StatusBadRequest,
		)
		return
	}

	var input createMerchantBillingAccountRequest

	if err := app.readJSON(w, r, &input); err != nil {
		app.respondWithError(
			w,
			errors.New(
				"invalid JSON input: "+err.Error(),
			),
			http.StatusBadRequest,
		)
		return
	}

	account, err :=
		app.InternalServices.
			CreateMerchantBillingAccountInternal(
				ctx,
				merchantID,
				input.Currency,
			)
	if err != nil {
		logger.Error(
			"Create merchant billing account failed",
			"merchant_id",
			merchantID,
			"error",
			err,
		)

		app.respondWithMerchantBillingAccountError(
			w,
			err,
		)
		return
	}

	auditErr := app.insertGovernanceAudit(
		ctx,
		userID,
		actionCreateMerchantBillingAccount,
		"Create a merchant billing account",
		merchantBillingAccountEntityType,
		merchantBillingAccountEntityTypeDescription,
		merchantID.String(),
	)

	message :=
		"Merchant billing account created successfully"

	if auditErr != nil {
		logger.Warn(
			"Merchant billing account created but audit recording failed",
			"merchant_id",
			merchantID,
			"error",
			auditErr,
		)

		message =
			"Merchant billing account created, but audit logging failed"
	}

	logger.Info(
		"Merchant billing account created",
		"merchant_id",
		account.MerchantID,
		"status",
		account.Status,
		"currency",
		account.Currency,
	)

	app.respondWithJSON(
		w,
		http.StatusCreated,
		jsonResponse{
			Error:   false,
			Message: message,
			Data:    account,
		},
	)
}

// -----------------------------------------------------------------------------
// Read
// -----------------------------------------------------------------------------

// GetMerchantBillingAccountByMerchantIDHandler retrieves the merchant's
// canonical billing account.
func (app *Application) GetMerchantBillingAccountByMerchantIDHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	logger := app.Logger.
		GetLoggerWithContext(r).
		WithFunctionName(
			"GetMerchantBillingAccountByMerchantIDHandler",
		)

	ctx, cancel := context.WithTimeout(
		r.Context(),
		cfgTimeout,
	)
	defer cancel()

	if !app.HasPermission(
		ctx,
		actionReadMerchantBillingAccount,
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

	merchantID, err :=
		app.parseMerchantIDPathParam(r)
	if err != nil {
		app.respondWithError(
			w,
			err,
			http.StatusBadRequest,
		)
		return
	}

	account, err :=
		app.InternalServices.
			GetMerchantBillingAccountByMerchantIDInternal(
				ctx,
				merchantID,
			)
	if err != nil {
		logger.Error(
			"Get merchant billing account failed",
			"merchant_id",
			merchantID,
			"error",
			err,
		)

		app.respondWithMerchantBillingAccountError(
			w,
			err,
		)
		return
	}

	auditErr := app.insertGovernanceAudit(
		ctx,
		userID,
		actionReadMerchantBillingAccount,
		"Read a merchant billing account",
		merchantBillingAccountEntityType,
		merchantBillingAccountEntityTypeDescription,
		merchantID.String(),
	)

	message :=
		"Merchant billing account retrieved successfully"

	if auditErr != nil {
		logger.Warn(
			"Merchant billing account retrieved but audit recording failed",
			"merchant_id",
			merchantID,
			"error",
			auditErr,
		)

		message =
			"Merchant billing account retrieved, but audit logging failed"
	}

	logger.Info(
		"Merchant billing account retrieved",
		"merchant_id",
		merchantID,
	)

	app.respondWithJSON(
		w,
		http.StatusOK,
		jsonResponse{
			Error:   false,
			Message: message,
			Data:    account,
		},
	)
}

// ListMerchantBillingAccountsByStatusHandler returns a bounded administrative
// page of billing accounts for one required lifecycle status.
//
// Pagination follows the data layer's deterministic descending keyset order:
// created_at, then merchant_id.
func (app *Application) ListMerchantBillingAccountsByStatusHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	logger := app.Logger.
		GetLoggerWithContext(r).
		WithFunctionName(
			"ListMerchantBillingAccountsByStatusHandler",
		)

	ctx, cancel := context.WithTimeout(
		r.Context(),
		cfgTimeout,
	)
	defer cancel()

	if !app.HasPermission(
		ctx,
		actionListMerchantBillingAccounts,
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

	status, err :=
		parseMerchantBillingAccountStatusParam(r)
	if err != nil {
		app.respondWithError(
			w,
			err,
			http.StatusBadRequest,
		)
		return
	}

	limit, err :=
		parseMerchantBillingAccountLimit(r)
	if err != nil {
		app.respondWithError(
			w,
			err,
			http.StatusBadRequest,
		)
		return
	}

	beforeCreatedAt, beforeMerchantID, err :=
		parseMerchantBillingAccountListCursor(r)
	if err != nil {
		app.respondWithError(
			w,
			err,
			http.StatusBadRequest,
		)
		return
	}

	accounts, err :=
		app.InternalServices.
			ListMerchantBillingAccountsByStatusInternal(
				ctx,
				status,
				limit,
				beforeCreatedAt,
				beforeMerchantID,
			)
	if err != nil {
		logger.Error(
			"List merchant billing accounts by status failed",
			"status",
			status,
			"error",
			err,
		)

		app.respondWithMerchantBillingAccountError(
			w,
			err,
		)
		return
	}

	auditEntityID :=
		"status:" + string(status)

	auditErr := app.insertGovernanceAudit(
		ctx,
		userID,
		actionListMerchantBillingAccounts,
		"List merchant billing accounts by status",
		merchantBillingAccountEntityType,
		merchantBillingAccountEntityTypeDescription,
		auditEntityID,
	)

	message :=
		"Merchant billing accounts retrieved successfully"

	if auditErr != nil {
		logger.Warn(
			"Merchant billing accounts retrieved but audit recording failed",
			"status",
			status,
			"error",
			auditErr,
		)

		message =
			"Merchant billing accounts retrieved, but audit logging failed"
	}

	logger.Info(
		"Merchant billing accounts retrieved",
		"status",
		status,
		"result_count",
		len(accounts),
	)

	app.respondWithJSON(
		w,
		http.StatusOK,
		jsonResponse{
			Error:   false,
			Message: message,
			Data:    accounts,
		},
	)
}

// -----------------------------------------------------------------------------
// Lifecycle
// -----------------------------------------------------------------------------

// mutateMerchantBillingAccountLifecycle executes one guarded lifecycle
// operation and returns a stable response.
//
// Mutation success is not followed by a second persistence read. This avoids
// reporting a committed lifecycle mutation as failed merely because an
// independent follow-up read encounters a transient failure.
func (app *Application) mutateMerchantBillingAccountLifecycle(
	w http.ResponseWriter,
	r *http.Request,
	functionName string,
	requiredPermission string,
	action string,
	auditDescription string,
	successMessage string,
	auditFailureMessage string,
	targetStatus data.MerchantBillingAccountStatus,
	mutate func(context.Context, uuid.UUID) error,
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

	merchantID, err :=
		app.parseMerchantIDPathParam(r)
	if err != nil {
		app.respondWithError(
			w,
			err,
			http.StatusBadRequest,
		)
		return
	}

	if err := mutate(ctx, merchantID); err != nil {
		logger.Error(
			"Merchant billing account lifecycle mutation failed",
			"merchant_id",
			merchantID,
			"target_status",
			targetStatus,
			"error",
			err,
		)

		app.respondWithMerchantBillingAccountError(
			w,
			err,
		)
		return
	}

	auditErr := app.insertGovernanceAudit(
		ctx,
		userID,
		action,
		auditDescription,
		merchantBillingAccountEntityType,
		merchantBillingAccountEntityTypeDescription,
		merchantID.String(),
	)

	message := successMessage

	if auditErr != nil {
		logger.Warn(
			"Merchant billing account lifecycle mutation completed but audit recording failed",
			"merchant_id",
			merchantID,
			"target_status",
			targetStatus,
			"error",
			auditErr,
		)

		message = auditFailureMessage
	}

	logger.Info(
		"Merchant billing account lifecycle mutation completed",
		"merchant_id",
		merchantID,
		"target_status",
		targetStatus,
	)

	app.respondWithJSON(
		w,
		http.StatusOK,
		jsonResponse{
			Error:   false,
			Message: message,
			Data: merchantBillingAccountLifecycleResponse{
				MerchantID: merchantID,
				Status:     targetStatus,
			},
		},
	)
}

// SuspendMerchantBillingAccountHandler transitions an active account to
// suspended.
//
// The data layer also treats an account already in suspended status as an
// idempotent success.
func (app *Application) SuspendMerchantBillingAccountHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	app.mutateMerchantBillingAccountLifecycle(
		w,
		r,
		"SuspendMerchantBillingAccountHandler",
		actionSuspendMerchantBillingAccount,
		actionSuspendMerchantBillingAccount,
		"Suspend a merchant billing account",
		"Merchant billing account suspended successfully",
		"Merchant billing account suspended, but audit logging failed",
		data.MerchantBillingAccountStatusSuspended,
		app.InternalServices.SuspendMerchantBillingAccountInternal,
	)
}

// ReactivateMerchantBillingAccountHandler transitions a suspended account to
// active.
//
// The data layer also treats an account already in active status as an
// idempotent success.
func (app *Application) ReactivateMerchantBillingAccountHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	app.mutateMerchantBillingAccountLifecycle(
		w,
		r,
		"ReactivateMerchantBillingAccountHandler",
		actionReactivateMerchantBillingAccount,
		actionReactivateMerchantBillingAccount,
		"Reactivate a merchant billing account",
		"Merchant billing account reactivated successfully",
		"Merchant billing account reactivated, but audit logging failed",
		data.MerchantBillingAccountStatusActive,
		app.InternalServices.ReactivateMerchantBillingAccountInternal,
	)
}

// CloseMerchantBillingAccountHandler transitions an active or suspended
// account to closed.
//
// Closed is terminal for new ordinary billing activity. Closure does not erase
// obligations, invoices, payment history, credits, reconciliation records,
// or audit history. The data layer treats an already closed account as an
// idempotent success.
func (app *Application) CloseMerchantBillingAccountHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	app.mutateMerchantBillingAccountLifecycle(
		w,
		r,
		"CloseMerchantBillingAccountHandler",
		actionCloseMerchantBillingAccount,
		actionCloseMerchantBillingAccount,
		"Close a merchant billing account",
		"Merchant billing account closed successfully",
		"Merchant billing account closed, but audit logging failed",
		data.MerchantBillingAccountStatusClosed,
		app.InternalServices.CloseMerchantBillingAccountInternal,
	)
}
