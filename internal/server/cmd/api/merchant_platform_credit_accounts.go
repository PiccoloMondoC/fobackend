// Package main provides HTTP handlers for merchant platform credit-account
// governance.
//
// focodebase/fobackend/internal/server/cmd/api/merchant_platform_credit_accounts.go
//
// GTM:
//
//	Layer: 2.4.b Commerce Architecture / Monetization Layer
//	Release Class: SPINE
//	Reason:
//	  merchant_platform_credit_accounts records platform-issued commercial
//	  credit granted to merchants. Merchant platform credit affects the
//	  amount commercially owed before payment settlement. It therefore
//	  belongs to Commerce Architecture, not Merchant Payments Architecture.
//
// Domain Boundary:
//
//	This handler governs privileged creation, reads, bounded listings,
//	descriptive correction, and cancellation of merchant platform credit
//	accounts. It does not determine credit eligibility or grant policy, does
//	not consume credit, does not perform invoice orchestration, does not run
//	expiration workers, does not treat credit as merchant-held funds, and
//	exposes no destructive deletion or restoration. Authorization to invoke
//	the administrative grant capability is distinct from commercial
//	eligibility policy, which remains governed by Administration.
//
// Monetary Boundary:
//
//	Amounts are PostgreSQL NUMERIC(19,4) values represented at the API
//	boundary as canonical decimal strings. This handler never parses,
//	serializes, or compares monetary amounts as floating point, and never
//	performs monetary arithmetic. remaining_amount can never be set by a
//	caller; it is always derived by the data layer.
//
// Handler Boundary:
//
//	This file implements only the handler layer. It does not implement
//	service orchestration, billing logic, eligibility logic, promotion
//	logic, payment logic, transaction coordination, or async-worker logic.
//	Consume, ConsumeTx, and ExpireBatch are intentionally not exposed here.
//
// SPINE Rule:
//
//	Keep compiling.
//	Keep production-ready.
//	Preserve strict authorization and audit coverage for every operation.
//	Preserve mass-assignment safety through explicit request DTOs.
//	Preserve monetary values as canonical decimal strings.
//	Preserve terminal lifecycle integrity; never resurrect a terminal
//	account through this handler.
//	Never expose a direct consumption or batch-expiration endpoint.
//	Never hard-code commercial eligibility, grant, or promotion policy.
//	Block deployment if this file breaks build, authorization, audit
//	accountability, monetary safety, or lifecycle integrity.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/PiccoloMondoC/focodebase/fobackend/internal/data"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// -----------------------------------------------------------------------------
// Permission / action / entity vocabulary
// -----------------------------------------------------------------------------

const (
	merchantPlatformCreditAccountEntityType = "merchant_platform_credit_account"

	merchantPlatformCreditAccountEntityTypeDescription = "Platform-issued merchant commercial credit account entity"

	actionCreateMerchantPlatformCreditAccount                  = "create_merchant_platform_credit_account"
	actionReadMerchantPlatformCreditAccount                    = "read_merchant_platform_credit_account"
	actionListMerchantPlatformCreditAccounts                   = "list_merchant_platform_credit_accounts"
	actionUpdateMerchantPlatformCreditAccountDescriptiveFields = "update_merchant_platform_credit_account_descriptive_fields"
	actionCancelMerchantPlatformCreditAccount                  = "cancel_merchant_platform_credit_account"

	merchantPlatformCreditAccountDefaultListLimit = 20
	merchantPlatformCreditAccountMaximumListLimit = 100
)

// -----------------------------------------------------------------------------
// Error translation
// -----------------------------------------------------------------------------

// merchantPlatformCreditAccountHTTPStatus translates merchant platform credit
// account domain errors into stable HTTP status codes.
//
// Classification is based exclusively on exported data-layer sentinel errors.
// Error-message wording is diagnostic context and is never part of the HTTP
// contract.
func merchantPlatformCreditAccountHTTPStatus(err error) int {
	if err == nil {
		return http.StatusOK
	}

	switch {
	case errors.Is(
		err,
		data.ErrMerchantPlatformCreditAccountInvalidInput,
	):
		return http.StatusBadRequest

	case errors.Is(err, data.ErrMerchantNotFound),
		errors.Is(
			err,
			data.ErrMerchantPlatformCreditAccountNotFound,
		):
		return http.StatusNotFound

	case errors.Is(
		err,
		data.ErrMerchantPlatformCreditAccountInvalidTransition,
	),
		errors.Is(
			err,
			data.ErrMerchantPlatformCreditAccountInvalidState,
		),
		errors.Is(
			err,
			data.ErrMerchantPlatformCreditAccountCurrencyMismatch,
		),
		errors.Is(
			err,
			data.ErrMerchantPlatformCreditAccountNotUsable,
		),
		errors.Is(
			err,
			data.ErrMerchantPlatformCreditAccountInsufficientBalance,
		),
		errors.Is(
			err,
			data.ErrMerchantPlatformCreditAccountMutationConflict,
		):
		return http.StatusConflict

	default:
		return http.StatusInternalServerError
	}
}

// -----------------------------------------------------------------------------
// Path parameter parsing
// -----------------------------------------------------------------------------

// parseMerchantPlatformCreditAccountID extracts and validates the canonical
// merchant platform credit account ID from the URL path.
func (app *Application) parseMerchantPlatformCreditAccountID(r *http.Request) (uuid.UUID, error) {
	raw := strings.TrimSpace(chi.URLParam(r, "merchantPlatformCreditAccountID"))
	if raw == "" {
		return uuid.Nil, errors.New("merchant platform credit account ID is required")
	}

	id, err := uuid.Parse(raw)
	if err != nil || id == uuid.Nil {
		return uuid.Nil, errors.New("invalid merchant platform credit account ID")
	}

	return id, nil
}

// parseMerchantIDPathParam extracts and validates a merchant ID supplied as a
// path parameter under the trusted key "merchantID".
//
// This is distinct from context-injected merchant identity: routes in this
// file accept merchant ID as an explicit path parameter, not from a header or
// application context value.
func (app *Application) parseMerchantIDPathParam(r *http.Request) (uuid.UUID, error) {
	raw := strings.TrimSpace(chi.URLParam(r, "merchantID"))
	if raw == "" {
		return uuid.Nil, errors.New("merchant ID is required")
	}

	id, err := uuid.Parse(raw)
	if err != nil || id == uuid.Nil {
		return uuid.Nil, errors.New("invalid merchant ID")
	}

	return id, nil
}

// -----------------------------------------------------------------------------
// Timestamp parsing
// -----------------------------------------------------------------------------

// parseOptionalMerchantPlatformCreditTimestamp parses an optional RFC3339
// timestamp string and canonicalizes it to UTC.
//
// A nil input remains semantically distinguishable from an explicitly
// supplied timestamp: this function returns (nil, nil) for a nil or blank
// input, allowing the data layer to apply its own database-time default
// (e.g. NOW() for starts_at) rather than substituting application time.
func parseOptionalMerchantPlatformCreditTimestamp(raw *string) (*time.Time, error) {
	if raw == nil {
		return nil, nil
	}

	trimmed := strings.TrimSpace(*raw)
	if trimmed == "" {
		return nil, errors.New("timestamp must not be blank")
	}

	parsed, err := time.Parse(time.RFC3339, trimmed)
	if err != nil {
		return nil, errors.New("timestamp must be a valid RFC3339 value")
	}

	utc := parsed.UTC()
	return &utc, nil
}

// parseMerchantPlatformCreditAccountLimit parses a bounded optional limit.
//
// An omitted limit uses the handler default. A present malformed, zero,
// negative, or over-maximum value is rejected rather than silently replaced.
func parseMerchantPlatformCreditAccountLimit(r *http.Request) (int, error) {
	raw := strings.TrimSpace(r.URL.Query().Get("limit"))
	if raw == "" {
		return merchantPlatformCreditAccountDefaultListLimit, nil
	}

	limit, err := strconv.Atoi(raw)
	if err != nil {
		return 0, errors.New("limit must be an integer")
	}
	if limit < 1 || limit > merchantPlatformCreditAccountMaximumListLimit {
		return 0, errors.New("limit must be between 1 and 100")
	}

	return limit, nil
}

// -----------------------------------------------------------------------------
// Request DTOs
// -----------------------------------------------------------------------------

// createMerchantPlatformCreditAccountRequest is the explicit creation
// request contract.
//
// Only caller-owned creation fields are accepted. id, status,
// remaining_amount, created_at, and updated_at are intentionally absent:
// the data layer establishes status = active and remaining_amount =
// original_amount, and owns id/created_at/updated_at.
type createMerchantPlatformCreditAccountRequest struct {
	MerchantID     uuid.UUID `json:"merchant_id"`
	OriginalAmount string    `json:"original_amount"`
	Currency       string    `json:"currency"`
	StartsAt       *string   `json:"starts_at,omitempty"`
	ExpiresAt      *string   `json:"expires_at,omitempty"`
	SourceCode     *string   `json:"source_code,omitempty"`
	Note           *string   `json:"note,omitempty"`
}

// updateMerchantPlatformCreditAccountDescriptiveFieldsRequest is the explicit
// descriptive-metadata correction request contract.
//
// Only source_code and note may be corrected through this endpoint.
// merchant_id, status, original_amount, remaining_amount, currency,
// starts_at, expires_at, created_at, and updated_at are immutable through
// this handler.
type nullableMerchantPlatformCreditString struct {
	Set   bool
	Value *string
}

func (field *nullableMerchantPlatformCreditString) UnmarshalJSON(raw []byte) error {
	field.Set = true

	if string(raw) == "null" {
		field.Value = nil
		return nil
	}

	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return errors.New("must be a string or null")
	}

	field.Value = &value
	return nil
}

type updateMerchantPlatformCreditAccountDescriptiveFieldsRequest struct {
	SourceCode nullableMerchantPlatformCreditString `json:"source_code"`
	Note       nullableMerchantPlatformCreditString `json:"note"`
}

type merchantPlatformCreditAccountMutationResponse struct {
	ID uuid.UUID `json:"id"`
}

// -----------------------------------------------------------------------------
// Create
// -----------------------------------------------------------------------------

// CreateMerchantPlatformCreditAccountHandler creates one new platform-issued
// merchant credit account.
//
// This handler authorizes the administrative capability to grant credit. It
// does not determine whether the merchant commercially qualifies for credit;
// that is a distinct, Administration-governed eligibility concern.
func (app *Application) CreateMerchantPlatformCreditAccountHandler(w http.ResponseWriter, r *http.Request) {
	logger := app.Logger.GetLoggerWithContext(r).WithFunctionName("CreateMerchantPlatformCreditAccountHandler")

	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	if !app.HasPermission(ctx, actionCreateMerchantPlatformCreditAccount) {
		app.respondWithError(w, errors.New("forbidden: insufficient permissions"), http.StatusForbidden)
		return
	}

	userID := app.getUserIDFromContext(ctx)
	if userID == nil {
		app.respondWithError(w, errors.New("user ID not found in context"), http.StatusUnauthorized)
		return
	}

	var input createMerchantPlatformCreditAccountRequest
	if err := app.readJSON(w, r, &input); err != nil {
		app.respondWithError(w, errors.New("invalid JSON input: "+err.Error()), http.StatusBadRequest)
		return
	}

	if input.MerchantID == uuid.Nil {
		app.respondWithError(w, errors.New("merchant_id is required"), http.StatusBadRequest)
		return
	}

	startsAt, err := parseOptionalMerchantPlatformCreditTimestamp(input.StartsAt)
	if err != nil {
		app.respondWithError(w, errors.New("starts_at: "+err.Error()), http.StatusBadRequest)
		return
	}

	expiresAt, err := parseOptionalMerchantPlatformCreditTimestamp(input.ExpiresAt)
	if err != nil {
		app.respondWithError(w, errors.New("expires_at: "+err.Error()), http.StatusBadRequest)
		return
	}

	account := &data.MerchantPlatformCreditAccount{
		MerchantID:     input.MerchantID,
		OriginalAmount: input.OriginalAmount,
		Currency:       input.Currency,
		ExpiresAt:      expiresAt,
		SourceCode:     input.SourceCode,
		Note:           input.Note,
	}
	if startsAt != nil {
		account.StartsAt = *startsAt
	}

	if err := app.InternalServices.CreateMerchantPlatformCreditAccountInternal(ctx, account); err != nil {
		logger.Error("Create merchant platform credit account failed", "merchant_id", input.MerchantID, "error", err)
		app.respondWithError(w, err, merchantPlatformCreditAccountHTTPStatus(err))
		return
	}

	if err := app.insertGovernanceAudit(
		ctx,
		userID,
		actionCreateMerchantPlatformCreditAccount,
		"Create a merchant platform credit account",
		merchantPlatformCreditAccountEntityType,
		merchantPlatformCreditAccountEntityTypeDescription,
		account.ID.String(),
	); err != nil {
		logger.Warn("Audit logging failed", "merchant_platform_credit_account_id", account.ID, "error", err)
		app.respondWithJSON(w, http.StatusCreated, jsonResponse{
			Error:   false,
			Message: "Merchant platform credit account created, but audit logging failed",
			Data:    account,
		})
		return
	}

	logger.Info(
		"Merchant platform credit account created",
		"merchant_platform_credit_account_id", account.ID,
		"merchant_id", account.MerchantID,
		"currency", account.Currency,
	)
	app.respondWithJSON(w, http.StatusCreated, jsonResponse{
		Error:   false,
		Message: "Merchant platform credit account created successfully",
		Data:    account,
	})
}

// -----------------------------------------------------------------------------
// Reads
// -----------------------------------------------------------------------------

// GetMerchantPlatformCreditAccountByIDHandler retrieves one merchant platform
// credit account by canonical ID.
//
// This is a privileged administrative read. Terminal accounts remain
// readable.
func (app *Application) GetMerchantPlatformCreditAccountByIDHandler(w http.ResponseWriter, r *http.Request) {
	logger := app.Logger.GetLoggerWithContext(r).WithFunctionName("GetMerchantPlatformCreditAccountByIDHandler")

	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	if !app.HasPermission(ctx, actionReadMerchantPlatformCreditAccount) {
		app.respondWithError(w, errors.New("forbidden: insufficient permissions"), http.StatusForbidden)
		return
	}

	userID := app.getUserIDFromContext(ctx)
	if userID == nil {
		app.respondWithError(w, errors.New("user ID not found in context"), http.StatusUnauthorized)
		return
	}

	accountID, err := app.parseMerchantPlatformCreditAccountID(r)
	if err != nil {
		app.respondWithError(w, err, http.StatusBadRequest)
		return
	}

	account, err := app.InternalServices.GetMerchantPlatformCreditAccountByIDInternal(ctx, accountID)
	if err != nil {
		logger.Error("Get merchant platform credit account by ID failed", "merchant_platform_credit_account_id", accountID, "error", err)
		app.respondWithError(w, err, merchantPlatformCreditAccountHTTPStatus(err))
		return
	}
	if account == nil {
		app.respondWithError(w, errors.New("merchant platform credit account not found"), http.StatusNotFound)
		return
	}

	if err := app.insertGovernanceAudit(
		ctx,
		userID,
		actionReadMerchantPlatformCreditAccount,
		"Read a merchant platform credit account",
		merchantPlatformCreditAccountEntityType,
		merchantPlatformCreditAccountEntityTypeDescription,
		account.ID.String(),
	); err != nil {
		logger.Warn("Audit logging failed", "merchant_platform_credit_account_id", account.ID, "error", err)
		app.respondWithJSON(w, http.StatusOK, jsonResponse{
			Error:   false,
			Message: "Merchant platform credit account retrieved, but audit logging failed",
			Data:    account,
		})
		return
	}

	app.respondWithJSON(w, http.StatusOK, jsonResponse{
		Error:   false,
		Message: "Merchant platform credit account retrieved successfully",
		Data:    account,
	})
}

// GetMerchantPlatformCreditAccountByIDForMerchantHandler retrieves one
// merchant platform credit account only when it belongs to the merchant
// identified in the path.
//
// This uses the ownership-scoped data method directly rather than a global
// retrieval followed by an ownership comparison.
func (app *Application) GetMerchantPlatformCreditAccountByIDForMerchantHandler(w http.ResponseWriter, r *http.Request) {
	logger := app.Logger.GetLoggerWithContext(r).WithFunctionName("GetMerchantPlatformCreditAccountByIDForMerchantHandler")

	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	if !app.HasPermission(ctx, actionReadMerchantPlatformCreditAccount) {
		app.respondWithError(w, errors.New("forbidden: insufficient permissions"), http.StatusForbidden)
		return
	}

	userID := app.getUserIDFromContext(ctx)
	if userID == nil {
		app.respondWithError(w, errors.New("user ID not found in context"), http.StatusUnauthorized)
		return
	}

	merchantID, err := app.parseMerchantIDPathParam(r)
	if err != nil {
		app.respondWithError(w, err, http.StatusBadRequest)
		return
	}

	accountID, err := app.parseMerchantPlatformCreditAccountID(r)
	if err != nil {
		app.respondWithError(w, err, http.StatusBadRequest)
		return
	}

	account, err := app.InternalServices.GetMerchantPlatformCreditAccountByIDForMerchantInternal(ctx, merchantID, accountID)
	if err != nil {
		logger.Error(
			"Get merchant platform credit account by ID for merchant failed",
			"merchant_platform_credit_account_id", accountID,
			"merchant_id", merchantID,
			"error", err,
		)
		app.respondWithError(w, err, merchantPlatformCreditAccountHTTPStatus(err))
		return
	}
	if account == nil {
		app.respondWithError(w, errors.New("merchant platform credit account not found for merchant"), http.StatusNotFound)
		return
	}

	if err := app.insertGovernanceAudit(
		ctx,
		userID,
		actionReadMerchantPlatformCreditAccount,
		"Read a merchant-scoped merchant platform credit account",
		merchantPlatformCreditAccountEntityType,
		merchantPlatformCreditAccountEntityTypeDescription,
		account.ID.String(),
	); err != nil {
		logger.Warn("Audit logging failed", "merchant_platform_credit_account_id", account.ID, "error", err)
		app.respondWithJSON(w, http.StatusOK, jsonResponse{
			Error:   false,
			Message: "Merchant platform credit account retrieved, but audit logging failed",
			Data:    account,
		})
		return
	}

	app.respondWithJSON(w, http.StatusOK, jsonResponse{
		Error:   false,
		Message: "Merchant platform credit account retrieved successfully",
		Data:    account,
	})
}

// ListMerchantPlatformCreditAccountsByMerchantHandler lists all lifecycle
// states of a merchant's platform credit accounts using bounded offset
// pagination.
//
// Ordering is deterministic and supplied by the data layer. This handler
// never reorders results.
func (app *Application) ListMerchantPlatformCreditAccountsByMerchantHandler(w http.ResponseWriter, r *http.Request) {
	logger := app.Logger.GetLoggerWithContext(r).WithFunctionName("ListMerchantPlatformCreditAccountsByMerchantHandler")

	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	if !app.HasPermission(ctx, actionListMerchantPlatformCreditAccounts) {
		app.respondWithError(w, errors.New("forbidden: insufficient permissions"), http.StatusForbidden)
		return
	}

	userID := app.getUserIDFromContext(ctx)
	if userID == nil {
		app.respondWithError(w, errors.New("user ID not found in context"), http.StatusUnauthorized)
		return
	}

	merchantID, err := app.parseMerchantIDPathParam(r)
	if err != nil {
		app.respondWithError(w, err, http.StatusBadRequest)
		return
	}

	limit, offset, err := app.parseLimitOffset(r)
	if err != nil {
		app.respondWithError(w, err, http.StatusBadRequest)
		return
	}

	accounts, err := app.InternalServices.ListMerchantPlatformCreditAccountsByMerchantInternal(ctx, merchantID, limit, offset)
	if err != nil {
		logger.Error("List merchant platform credit accounts by merchant failed", "merchant_id", merchantID, "error", err)
		app.respondWithError(w, err, merchantPlatformCreditAccountHTTPStatus(err))
		return
	}

	if err := app.insertGovernanceAudit(
		ctx,
		userID,
		actionListMerchantPlatformCreditAccounts,
		"List a merchant's platform credit account history",
		merchantPlatformCreditAccountEntityType,
		merchantPlatformCreditAccountEntityTypeDescription,
		merchantID.String(),
	); err != nil {
		logger.Warn("Audit logging failed", "merchant_id", merchantID, "error", err)
		app.respondWithJSON(w, http.StatusOK, jsonResponse{
			Error:   false,
			Message: "Merchant platform credit accounts retrieved, but audit logging failed",
			Data:    accounts,
		})
		return
	}

	app.respondWithJSON(w, http.StatusOK, jsonResponse{
		Error:   false,
		Message: "Merchant platform credit accounts retrieved successfully",
		Data:    accounts,
	})
}

// ListCurrentlyUsableMerchantPlatformCreditAccountsHandler lists accounts
// that are currently usable for a merchant and currency.
//
// This is a point-in-time read. It does not select an account for
// consumption, does not claim the returned order is mandatory consumption
// policy, and does not promise that any account will remain usable after the
// response is sent. Concurrent mutation may change availability immediately
// afterward.
func (app *Application) ListCurrentlyUsableMerchantPlatformCreditAccountsHandler(w http.ResponseWriter, r *http.Request) {
	logger := app.Logger.GetLoggerWithContext(r).WithFunctionName("ListCurrentlyUsableMerchantPlatformCreditAccountsHandler")

	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	if !app.HasPermission(ctx, actionListMerchantPlatformCreditAccounts) {
		app.respondWithError(w, errors.New("forbidden: insufficient permissions"), http.StatusForbidden)
		return
	}

	userID := app.getUserIDFromContext(ctx)
	if userID == nil {
		app.respondWithError(w, errors.New("user ID not found in context"), http.StatusUnauthorized)
		return
	}

	merchantID, err := app.parseMerchantIDPathParam(r)
	if err != nil {
		app.respondWithError(w, err, http.StatusBadRequest)
		return
	}

	currency := strings.TrimSpace(r.URL.Query().Get("currency"))
	if currency == "" {
		app.respondWithError(w, errors.New("currency is required"), http.StatusBadRequest)
		return
	}

	limit, err := parseMerchantPlatformCreditAccountLimit(r)
	if err != nil {
		app.respondWithError(w, err, http.StatusBadRequest)
		return
	}

	accounts, err := app.InternalServices.ListCurrentlyUsableMerchantPlatformCreditAccountsInternal(ctx, merchantID, currency, limit)
	if err != nil {
		logger.Error(
			"List currently usable merchant platform credit accounts failed",
			"merchant_id", merchantID,
			"currency", currency,
			"error", err,
		)
		app.respondWithError(w, err, merchantPlatformCreditAccountHTTPStatus(err))
		return
	}

	if err := app.insertGovernanceAudit(
		ctx,
		userID,
		actionListMerchantPlatformCreditAccounts,
		"List a merchant's currently usable platform credit accounts",
		merchantPlatformCreditAccountEntityType,
		merchantPlatformCreditAccountEntityTypeDescription,
		merchantID.String(),
	); err != nil {
		logger.Warn("Audit logging failed", "merchant_id", merchantID, "error", err)
		app.respondWithJSON(w, http.StatusOK, jsonResponse{
			Error:   false,
			Message: "Currently usable merchant platform credit accounts retrieved, but audit logging failed",
			Data:    accounts,
		})
		return
	}

	app.respondWithJSON(w, http.StatusOK, jsonResponse{
		Error:   false,
		Message: "Currently usable merchant platform credit accounts retrieved successfully",
		Data:    accounts,
	})
}

// -----------------------------------------------------------------------------
// Descriptive metadata
// -----------------------------------------------------------------------------

// UpdateMerchantPlatformCreditAccountDescriptiveFieldsHandler replaces
// source_code and note as one complete descriptive-metadata representation.
//
// Both fields must be present. Either may be null to clear it. This full
// replacement contract prevents an omitted PATCH field from being
// unintentionally interpreted as a request to erase persisted data.
//
// Descriptive correction is permitted even for terminal accounts because it
// does not change monetary or lifecycle meaning.
func (app *Application) UpdateMerchantPlatformCreditAccountDescriptiveFieldsHandler(w http.ResponseWriter, r *http.Request) {
	logger := app.Logger.GetLoggerWithContext(r).WithFunctionName("UpdateMerchantPlatformCreditAccountDescriptiveFieldsHandler")

	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	if !app.HasPermission(ctx, actionUpdateMerchantPlatformCreditAccountDescriptiveFields) {
		app.respondWithError(w, errors.New("forbidden: insufficient permissions"), http.StatusForbidden)
		return
	}

	userID := app.getUserIDFromContext(ctx)
	if userID == nil {
		app.respondWithError(w, errors.New("user ID not found in context"), http.StatusUnauthorized)
		return
	}

	accountID, err := app.parseMerchantPlatformCreditAccountID(r)
	if err != nil {
		app.respondWithError(w, err, http.StatusBadRequest)
		return
	}

	var input updateMerchantPlatformCreditAccountDescriptiveFieldsRequest
	if err := app.readJSON(w, r, &input); err != nil {
		app.respondWithError(w, errors.New("invalid JSON input: "+err.Error()), http.StatusBadRequest)
		return
	}

	if !input.SourceCode.Set || !input.Note.Set {
		app.respondWithError(
			w,
			errors.New("source_code and note are both required; use null to clear either field"),
			http.StatusBadRequest,
		)
		return
	}

	if err := app.InternalServices.UpdateMerchantPlatformCreditAccountDescriptiveFieldsInternal(
		ctx,
		accountID,
		input.SourceCode.Value,
		input.Note.Value,
	); err != nil {
		logger.Error(
			"Update merchant platform credit account descriptive fields failed",
			"merchant_platform_credit_account_id", accountID,
			"error", err,
		)
		app.respondWithError(w, err, merchantPlatformCreditAccountHTTPStatus(err))
		return
	}

	responseData := merchantPlatformCreditAccountMutationResponse{ID: accountID}

	if err := app.insertGovernanceAudit(
		ctx,
		userID,
		actionUpdateMerchantPlatformCreditAccountDescriptiveFields,
		"Update merchant platform credit account descriptive metadata",
		merchantPlatformCreditAccountEntityType,
		merchantPlatformCreditAccountEntityTypeDescription,
		accountID.String(),
	); err != nil {
		logger.Warn("Audit logging failed", "merchant_platform_credit_account_id", accountID, "error", err)
		app.respondWithJSON(w, http.StatusOK, jsonResponse{
			Error:   false,
			Message: "Merchant platform credit account descriptive metadata updated, but audit logging failed",
			Data:    responseData,
		})
		return
	}

	app.respondWithJSON(w, http.StatusOK, jsonResponse{
		Error:   false,
		Message: "Merchant platform credit account descriptive metadata updated successfully",
		Data:    responseData,
	})
}

// -----------------------------------------------------------------------------
// Lifecycle
// -----------------------------------------------------------------------------

// CancelMerchantPlatformCreditAccountHandler transitions an active merchant
// platform credit account to cancelled.
//
// Cancellation preserves remaining credit as historical truth, is idempotent
// for an already-cancelled account per the data-layer contract, and rejects
// invalid terminal-state transitions. It never implies a refund and never
// reactivates or restores an account.
func (app *Application) CancelMerchantPlatformCreditAccountHandler(w http.ResponseWriter, r *http.Request) {
	logger := app.Logger.GetLoggerWithContext(r).WithFunctionName("CancelMerchantPlatformCreditAccountHandler")

	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	if !app.HasPermission(ctx, actionCancelMerchantPlatformCreditAccount) {
		app.respondWithError(w, errors.New("forbidden: insufficient permissions"), http.StatusForbidden)
		return
	}

	userID := app.getUserIDFromContext(ctx)
	if userID == nil {
		app.respondWithError(w, errors.New("user ID not found in context"), http.StatusUnauthorized)
		return
	}

	accountID, err := app.parseMerchantPlatformCreditAccountID(r)
	if err != nil {
		app.respondWithError(w, err, http.StatusBadRequest)
		return
	}

	if err := app.InternalServices.CancelMerchantPlatformCreditAccountInternal(ctx, accountID); err != nil {
		logger.Error("Cancel merchant platform credit account failed", "merchant_platform_credit_account_id", accountID, "error", err)
		app.respondWithError(w, err, merchantPlatformCreditAccountHTTPStatus(err))
		return
	}

	responseData := merchantPlatformCreditAccountMutationResponse{ID: accountID}

	if err := app.insertGovernanceAudit(
		ctx,
		userID,
		actionCancelMerchantPlatformCreditAccount,
		"Cancel a merchant platform credit account",
		merchantPlatformCreditAccountEntityType,
		merchantPlatformCreditAccountEntityTypeDescription,
		accountID.String(),
	); err != nil {
		logger.Warn("Audit logging failed", "merchant_platform_credit_account_id", accountID, "error", err)
		app.respondWithJSON(w, http.StatusOK, jsonResponse{
			Error:   false,
			Message: "Merchant platform credit account cancelled, but audit logging failed",
			Data:    responseData,
		})
		return
	}

	app.respondWithJSON(w, http.StatusOK, jsonResponse{
		Error:   false,
		Message: "Merchant platform credit account cancelled successfully",
		Data:    responseData,
	})
}
