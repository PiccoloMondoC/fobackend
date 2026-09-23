// Package main provides HTTP handlers for canonical merchant payment methods.
//
// focodebase/fobackend/internal/server/cmd/api/merchant_payment_methods.go
//
// GTM:
//
//	Layer: 2.4 Merchant / Merchant Payments Domain
//	Release Class: SPINE
//	Reason:
//	  Merchant payment methods are release-critical payment-collection
//	  infrastructure. This handler surface governs the merchant-owned
//	  canonical identity, safe display metadata, lifecycle state, default
//	  assignment, soft deletion, and restoration of payment methods and
//	  billing arrangements recognized by the platform.
//
// Architecture Boundary:
//
//	This file defines the merchant-facing HTTP boundary for canonical payment
//	methods. It answers:
//
//	  "What payment methods or billing arrangements does this merchant have?"
//
//	External-provider identity, provider references, connectivity,
//	verification, synchronization, and disconnection do not belong to this
//	file. They belong to merchant_payment_method_provider_links.go.
//
//	Payment authorization, execution, settlement outcome, and transaction
//	recording do not belong to this file. They belong to
//	merchant_payments.go.
//
//	Commercial obligations, fees, invoices,
//	promotions, and adjustments do not belong to this file. They belong to
//	the Commerce Architecture and its canonical billing domains.
//
// Security Boundary:
//
//	This file is not a card vault, bank-account vault, provider-link
//	registry, token store, or payment-execution surface.
//
//	It must never accept, return, log, trace, metric-label, or audit:
//
//	  - full card numbers;
//	  - bank account or routing numbers;
//	  - CVVs or PINs;
//	  - provider payment-method references;
//	  - provider access or bearer tokens;
//	  - provider account or item identifiers;
//	  - raw provider payloads;
//	  - provider credentials.
//
//	last_four is display metadata only. It is not proof of payment-method
//	ownership, verification, connectivity, collectability, or validity.
//
// Ownership Boundary:
//
//	Every handler in this file is merchant-owned and merchant-scoped.
//
//	Merchant identity is resolved exclusively from trusted authenticated
//	request context. No handler accepts merchant_id from a request body,
//	query parameter, or route parameter.
//
//	A payment method belonging to another merchant must be indistinguishable
//	from one that does not exist.
//
// Operational Capability Doctrine:
//
//	Engineering provides the canonical payment-method lifecycle, stable
//	provider-link extension boundaries, collection-readiness seams, and
//	configuration integration points.
//
//	Administration governs operational policy outside this handler,
//	including:
//
//	  - whether merchant payment methods are available;
//	  - which supported payment-method types are currently enabled;
//	  - which installed providers are available;
//	  - onboarding payment-method requirements;
//	  - verification requirements;
//	  - default-method requirements;
//	  - collection eligibility policy.
//
//	This handler must not hard-code provider selection, provider availability,
//	enabled-method policy, onboarding policy, verification policy, pricing,
//	or collection policy.
//
// SPINE Rule:
//
//	Keep compiling.
//	Keep production-ready.
//	Preserve merchant ownership scoping on every read and mutation.
//	Preserve the canonical payment-method/provider-link/payment-execution
//	separation.
//	Preserve the single-active-default-per-merchant invariant under
//	concurrency.
//	Preserve soft-delete lifecycle semantics.
//	Preserve revoked as terminal.
//	Permit expired only for card methods.
//	Preserve explicit lifecycle transitions.
//	Never expose or accept provider-owned references.
//	Never treat last_four as evidence of ownership or verification.
//	Do not calculate commercial obligations or execute payments.
//	Block deployment if this file breaks build, ownership isolation,
//	lifecycle integrity, default-method integrity, confidentiality, or the
//	Merchant Payments Architecture boundary.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/PiccoloMondoC/focodebase/fobackend/internal/data"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// ---------------------------------------------------------------------------
// Constants: entity type, actions, pagination bounds
// ---------------------------------------------------------------------------

const (
	merchantPaymentMethodEntityType            = "merchant_payment_method"
	merchantPaymentMethodEntityTypeDescription = "Merchant billing payment-method reference entity"

	actionCreateMerchantPaymentMethod       = "create_merchant_payment_method"
	actionReadMerchantPaymentMethod         = "read_merchant_payment_method"
	actionReadDefaultMerchantPaymentMethod  = "read_default_merchant_payment_method"
	actionListMerchantPaymentMethods        = "list_merchant_payment_methods"
	actionUpdateMerchantPaymentMethod       = "update_merchant_payment_method"
	actionSetDefaultMerchantPaymentMethod   = "set_default_merchant_payment_method"
	actionClearDefaultMerchantPaymentMethod = "clear_default_merchant_payment_method"
	actionUpdateMerchantPaymentMethodStatus = "update_merchant_payment_method_status"
	actionSoftDeleteMerchantPaymentMethod   = "soft_delete_merchant_payment_method"
	actionRestoreMerchantPaymentMethod      = "restore_merchant_payment_method"

	// merchantPaymentMethodDefaultListLimit is used when the caller omits
	// an explicit limit query parameter.
	merchantPaymentMethodDefaultListLimit = 20

	// merchantPaymentMethodMaxListLimit mirrors the data layer's enforced
	// maximum in ListByMerchant. Keeping this constant here allows the
	// handler to reject an out-of-range limit with a clear 400 before ever
	// reaching the data layer.
	merchantPaymentMethodMaxListLimit = 100
)

// ---------------------------------------------------------------------------
// Request DTOs
//
// None of these decode directly into data.MerchantPaymentMethod. Each
// carries only the fields that legitimately belong at the corresponding API
// boundary, per BEG 17.6 (persistence shape and transport shape must not be
// conflated).
// ---------------------------------------------------------------------------

// createMerchantPaymentMethodRequest is the API-boundary shape accepted by
// CreateMerchantPaymentMethodHandler.
//
// It contains only canonical payment-method fields. Canonical ID, merchant
// ownership, timestamps, deletion state, and lifecycle defaults are
// server-owned. Provider identity, provider references, connectivity, and
// verification belong to the provider-link domain and are deliberately absent.
//
// LastFour is unverified display metadata only. Its presence never establishes
// ownership, verification, connectivity, collectability, or validity.
type createMerchantPaymentMethodRequest struct {
	PaymentMethodType data.MerchantPaymentMethodType `json:"payment_method_type"`
	DisplayLabel      *string                        `json:"display_label,omitempty"`
	LastFour          *string                        `json:"last_four,omitempty"`
	IsDefault         bool                           `json:"is_default,omitempty"`
}

// merchantPaymentMethodNullableString distinguishes a missing JSON field
// from an explicitly supplied null value.
//
// The wrapper is local because it represents the presence semantics of this
// handler's complete mutable-display replacement contract.
type merchantPaymentMethodNullableString struct {
	Set   bool
	Value *string
}

// UnmarshalJSON accepts either a string or null and records field presence.
func (value *merchantPaymentMethodNullableString) UnmarshalJSON(raw []byte) error {
	value.Set = true
	if string(raw) == "null" {
		value.Value = nil
		return nil
	}

	var decoded string
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return errors.New("must be a string or null")
	}
	value.Value = &decoded
	return nil
}

// updateMerchantPaymentMethodRequest replaces the complete mutable display
// subset. Both fields must be present; either may explicitly be null.
type updateMerchantPaymentMethodRequest struct {
	DisplayLabel merchantPaymentMethodNullableString `json:"display_label"`
	LastFour     merchantPaymentMethodNullableString `json:"last_four"`
}

// updateMerchantPaymentMethodStatusRequest is the API-boundary shape
// accepted by UpdateMerchantPaymentMethodStatusHandler.
type updateMerchantPaymentMethodStatusRequest struct {
	Status data.MerchantPaymentMethodStatus `json:"status"`
}

// ---------------------------------------------------------------------------
// Response DTOs
//
// merchantPaymentMethodResponse is the merchant-safe presentation of a
// canonical payment method.
//
// It intentionally omits merchant_id because every route is scoped to the
// authenticated merchant. It also omits deleted_at and every provider-link,
// verification, connectivity, credential, and payment-execution concern.
// ---------------------------------------------------------------------------

type merchantPaymentMethodResponse struct {
	ID                uuid.UUID                        `json:"id"`
	PaymentMethodType data.MerchantPaymentMethodType   `json:"payment_method_type"`
	DisplayLabel      *string                          `json:"display_label,omitempty"`
	LastFour          *string                          `json:"last_four,omitempty"`
	Status            data.MerchantPaymentMethodStatus `json:"status"`
	IsDefault         bool                             `json:"is_default"`
	CreatedAt         time.Time                        `json:"created_at"`
	UpdatedAt         time.Time                        `json:"updated_at"`
}

// merchantPaymentMethodStatusResponse is the bounded response body for
// status-transition operations.
type merchantPaymentMethodStatusResponse struct {
	ID     uuid.UUID                        `json:"id"`
	Status data.MerchantPaymentMethodStatus `json:"status"`
}

// toMerchantPaymentMethodResponse converts a persistence-layer model into
// the merchant-safe transport representation.
//
// It returns nil for nil input. GetDefaultMerchantPaymentMethodHandler relies
// on this behavior because the absence of a default payment method is a valid
// merchant state rather than a not-found error.
func toMerchantPaymentMethodResponse(
	method *data.MerchantPaymentMethod,
) *merchantPaymentMethodResponse {
	if method == nil {
		return nil
	}

	return &merchantPaymentMethodResponse{
		ID:                method.ID,
		PaymentMethodType: method.PaymentMethodType,
		DisplayLabel:      method.DisplayLabel,
		LastFour:          method.LastFour,
		Status:            method.Status,
		IsDefault:         method.IsDefault,
		CreatedAt:         method.CreatedAt,
		UpdatedAt:         method.UpdatedAt,
	}
}

// toMerchantPaymentMethodResponseList converts a slice of persistence-layer
// models into public-safe response DTOs, always returning a non-nil slice.
func toMerchantPaymentMethodResponseList(methods []*data.MerchantPaymentMethod) []*merchantPaymentMethodResponse {
	out := make([]*merchantPaymentMethodResponse, 0, len(methods))
	for _, m := range methods {
		out = append(out, toMerchantPaymentMethodResponse(m))
	}
	return out
}

// ---------------------------------------------------------------------------
// Error mapping
// ---------------------------------------------------------------------------

// merchantPaymentMethodHTTPStatus maps a canonical data-layer error into a
// bounded HTTP status code.
//
// Stable sentinel errors are classified first. Validation errors that have
// not yet been assigned domain sentinels are classified using a deliberately
// bounded fallback. Internal database errors remain internal-server errors.
func merchantPaymentMethodHTTPStatus(err error) int {
	if err == nil {
		return http.StatusOK
	}

	switch {
	case errors.Is(err, data.ErrMerchantPaymentMethodNotFound),
		errors.Is(err, data.ErrMerchantNotFound):
		return http.StatusNotFound

	case errors.Is(err, data.ErrMerchantPaymentMethodDefaultConflict),
		errors.Is(err, data.ErrDuplicate),
		errors.Is(err, data.ErrMerchantPaymentMethodInvalidState):
		return http.StatusConflict
	}

	message := strings.ToLower(err.Error())

	switch {
	case strings.Contains(message, "required"),
		strings.Contains(message, "invalid"),
		strings.Contains(message, "must be"),
		strings.Contains(message, "not permitted"),
		strings.Contains(message, "characters or fewer"):
		return http.StatusBadRequest

	default:
		return http.StatusInternalServerError
	}
}

// ---------------------------------------------------------------------------
// Identity, ownership, and pagination parsing
// ---------------------------------------------------------------------------

// parseMerchantPaymentMethodID extracts and validates the canonical
// merchantPaymentMethodID route parameter. It rejects a missing, malformed,
// or nil UUID.
func (app *Application) parseMerchantPaymentMethodID(r *http.Request) (uuid.UUID, error) {
	raw := strings.TrimSpace(chi.URLParam(r, "merchantPaymentMethodID"))
	if raw == "" {
		return uuid.Nil, errors.New("merchant payment method ID is required")
	}

	id, err := uuid.Parse(raw)
	if err != nil || id == uuid.Nil {
		return uuid.Nil, errors.New("invalid merchant payment method ID")
	}

	return id, nil
}

// requireMerchantActorID resolves the authenticated merchant actor's
// canonical merchant ID from trusted application context. The centralized
// getMerchantIDFromContext helper already exists.
func (app *Application) requireMerchantActorID(ctx context.Context) (uuid.UUID, error) {
	merchantID := app.getMerchantIDFromContext(ctx)
	if merchantID == nil || *merchantID == uuid.Nil {
		return uuid.Nil, errors.New("merchant identity not found in context")
	}
	return *merchantID, nil
}

// parseMerchantPaymentMethodListPagination parses and strictly validates
// the limit and offset query parameters for the list endpoint.
//
// Unlike some existing list handlers in this codebase that silently clamp
// an out-of-range limit, this function explicitly rejects malformed or
// out-of-range values, per the stricter bounded-read requirement for
// billing infrastructure. The default limit is
// merchantPaymentMethodDefaultListLimit; the enforced maximum mirrors the
// data layer's own ListByMerchant bound.
func parseMerchantPaymentMethodListPagination(r *http.Request) (limit, offset int, err error) {
	limit = merchantPaymentMethodDefaultListLimit
	offset = 0

	q := r.URL.Query()

	if raw := strings.TrimSpace(q.Get("limit")); raw != "" {
		parsed, convErr := strconv.Atoi(raw)
		if convErr != nil || parsed <= 0 {
			return 0, 0, errors.New("invalid limit: must be a positive integer")
		}
		if parsed > merchantPaymentMethodMaxListLimit {
			return 0, 0, fmt.Errorf("invalid limit: must not exceed %d", merchantPaymentMethodMaxListLimit)
		}
		limit = parsed
	}

	if raw := strings.TrimSpace(q.Get("offset")); raw != "" {
		parsed, convErr := strconv.Atoi(raw)
		if convErr != nil || parsed < 0 {
			return 0, 0, errors.New("invalid offset: must be a non-negative integer")
		}
		offset = parsed
	}

	return limit, offset, nil
}

// ---------------------------------------------------------------------------
// Creation
// ---------------------------------------------------------------------------

// CreateMerchantPaymentMethodHandler creates a merchant payment method
// owned by the authenticated merchant actor.
func (app *Application) CreateMerchantPaymentMethodHandler(w http.ResponseWriter, r *http.Request) {
	logger := app.Logger.GetLoggerWithContext(r).WithFunctionName("CreateMerchantPaymentMethodHandler")

	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	if !app.HasPermission(ctx, actionCreateMerchantPaymentMethod) {
		app.respondWithError(w, errors.New("forbidden: insufficient permissions"), http.StatusForbidden)
		return
	}

	userID := app.getUserIDFromContext(ctx)
	if userID == nil {
		app.respondWithError(w, errors.New("user ID not found in context"), http.StatusUnauthorized)
		return
	}

	merchantID, err := app.requireMerchantActorID(ctx)
	if err != nil {
		app.respondWithError(w, err, http.StatusBadRequest)
		return
	}

	var input createMerchantPaymentMethodRequest
	if err := app.readJSON(w, r, &input); err != nil {
		app.respondWithError(w, fmt.Errorf("invalid JSON input: %v", err), http.StatusBadRequest)
		return
	}

	normalizedType := data.NormalizeMerchantPaymentMethodType(input.PaymentMethodType)
	if !data.IsValidMerchantPaymentMethodType(normalizedType) {
		app.respondWithError(w, fmt.Errorf("invalid payment_method_type: %s", normalizedType), http.StatusBadRequest)
		return
	}

	method := &data.MerchantPaymentMethod{
		MerchantID:        merchantID,
		PaymentMethodType: normalizedType,
		DisplayLabel:      input.DisplayLabel,
		LastFour:          input.LastFour,
		IsDefault:         input.IsDefault,
	}

	if err := app.Models.MerchantPaymentMethod.Insert(ctx, method); err != nil {
		logger.Error("create merchant payment method failed",
			"merchant_id", merchantID,
			"payment_method_type", normalizedType,
			"error", err,
		)
		app.respondWithError(w, err, merchantPaymentMethodHTTPStatus(err))
		return
	}

	if err := app.insertGovernanceAudit(
		ctx,
		userID,
		actionCreateMerchantPaymentMethod,
		"Create a merchant payment method",
		merchantPaymentMethodEntityType,
		merchantPaymentMethodEntityTypeDescription,
		method.ID.String(),
	); err != nil {
		logger.Warn(
			"merchant payment method created but audit recording failed",
			"payment_method_id",
			method.ID,
			"merchant_id",
			merchantID,
			"error",
			err,
		)

		app.respondWithJSON(w, http.StatusCreated, jsonResponse{
			Error: false,
			Message: "Merchant payment method created successfully; " +
				"audit recording requires operational attention",
			Data: toMerchantPaymentMethodResponse(method),
		})
		return
	}

	logger.Info("merchant payment method created",
		"payment_method_id", method.ID,
		"merchant_id", merchantID,
		"payment_method_type", normalizedType,
		"status", method.Status,
		"is_default", method.IsDefault,
	)
	app.respondWithJSON(w, http.StatusCreated, jsonResponse{
		Error:   false,
		Message: "Merchant payment method created successfully",
		Data:    toMerchantPaymentMethodResponse(method),
	})
}

// ---------------------------------------------------------------------------
// Reads
// ---------------------------------------------------------------------------

// GetMerchantPaymentMethodByIDHandler retrieves a single merchant payment
// method owned by the authenticated merchant actor.
//
// A payment method that exists but belongs to a different merchant is
// reported identically to a payment method that does not exist at all,
// preventing cross-merchant existence enumeration.
func (app *Application) GetMerchantPaymentMethodByIDHandler(w http.ResponseWriter, r *http.Request) {
	logger := app.Logger.GetLoggerWithContext(r).WithFunctionName("GetMerchantPaymentMethodByIDHandler")

	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	if !app.HasPermission(ctx, actionReadMerchantPaymentMethod) {
		app.respondWithError(w, errors.New("forbidden: insufficient permissions"), http.StatusForbidden)
		return
	}

	userID := app.getUserIDFromContext(ctx)
	if userID == nil {
		app.respondWithError(w, errors.New("user ID not found in context"), http.StatusUnauthorized)
		return
	}

	merchantID, err := app.requireMerchantActorID(ctx)
	if err != nil {
		app.respondWithError(w, err, http.StatusBadRequest)
		return
	}

	paymentMethodID, err := app.parseMerchantPaymentMethodID(r)
	if err != nil {
		app.respondWithError(w, err, http.StatusBadRequest)
		return
	}

	method, err := app.Models.MerchantPaymentMethod.GetByIDForMerchant(ctx, merchantID, paymentMethodID)
	if err != nil {
		logger.Error("get merchant payment method failed",
			"merchant_id", merchantID,
			"payment_method_id", paymentMethodID,
			"error", err,
		)
		app.respondWithError(w, err, merchantPaymentMethodHTTPStatus(err))
		return
	}

	if err := app.insertGovernanceAudit(
		ctx, userID, actionReadMerchantPaymentMethod,
		"Read a merchant payment method",
		merchantPaymentMethodEntityType, merchantPaymentMethodEntityTypeDescription,
		paymentMethodID.String(),
	); err != nil {
		logger.Warn("audit logging failed", "payment_method_id", paymentMethodID, "error", err)
		app.respondWithJSON(w, http.StatusOK, jsonResponse{
			Error: false,
			Message: "Merchant payment method retrieved successfully; " +
				"audit recording requires operational attention",
			Data: toMerchantPaymentMethodResponse(method),
		})
		return
	}

	app.respondWithJSON(w, http.StatusOK, jsonResponse{
		Error:   false,
		Message: "Merchant payment method retrieved successfully",
		Data:    toMerchantPaymentMethodResponse(method),
	})
}

// GetDefaultMerchantPaymentMethodHandler retrieves the authenticated
// merchant's current active, non-deleted default payment method.
//
// A missing default is a normal merchant self-service state and is returned
// as 200 OK with a null payload rather than 404.
//
// This endpoint intentionally uses GetDefaultForMerchant directly. It reports
// the merchant's configured state; it does not resolve or certify a payment
// method for billing collection. Collection workflows must use the stricter
// internal readiness and default-resolution service contracts.
func (app *Application) GetDefaultMerchantPaymentMethodHandler(w http.ResponseWriter, r *http.Request) {
	logger := app.Logger.GetLoggerWithContext(r).WithFunctionName("GetDefaultMerchantPaymentMethodHandler")

	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	if !app.HasPermission(ctx, actionReadDefaultMerchantPaymentMethod) {
		app.respondWithError(w, errors.New("forbidden: insufficient permissions"), http.StatusForbidden)
		return
	}

	userID := app.getUserIDFromContext(ctx)
	if userID == nil {
		app.respondWithError(w, errors.New("user ID not found in context"), http.StatusUnauthorized)
		return
	}

	merchantID, err := app.requireMerchantActorID(ctx)
	if err != nil {
		app.respondWithError(w, err, http.StatusBadRequest)
		return
	}

	method, err := app.Models.MerchantPaymentMethod.GetDefaultForMerchant(ctx, merchantID)
	if err != nil {
		logger.Error("get default merchant payment method failed",
			"merchant_id", merchantID,
			"error", err,
		)
		app.respondWithError(w, err, merchantPaymentMethodHTTPStatus(err))
		return
	}

	if err := app.insertGovernanceAudit(
		ctx, userID, actionReadDefaultMerchantPaymentMethod,
		"Read the merchant's default payment method",
		merchantPaymentMethodEntityType, merchantPaymentMethodEntityTypeDescription,
		merchantID.String(),
	); err != nil {
		logger.Warn("audit logging failed", "merchant_id", merchantID, "error", err)

		auditFailureMessage := "No default merchant payment method set; " +
			"audit recording requires operational attention"

		if method != nil {
			auditFailureMessage =
				"Default merchant payment method retrieved successfully; " +
					"audit recording requires operational attention"
		}

		app.respondWithJSON(w, http.StatusOK, jsonResponse{
			Error:   false,
			Message: auditFailureMessage,
			Data:    toMerchantPaymentMethodResponse(method),
		})
		return
	}

	message := "No default merchant payment method set"
	if method != nil {
		message = "Default merchant payment method retrieved successfully"
	}

	app.respondWithJSON(w, http.StatusOK, jsonResponse{
		Error:   false,
		Message: message,
		Data:    toMerchantPaymentMethodResponse(method),
	})
}

// ListMerchantPaymentMethodsHandler lists the authenticated merchant's
// non-deleted payment methods with bounded pagination and an optional
// canonical status filter.
func (app *Application) ListMerchantPaymentMethodsHandler(w http.ResponseWriter, r *http.Request) {
	logger := app.Logger.GetLoggerWithContext(r).WithFunctionName("ListMerchantPaymentMethodsHandler")

	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	if !app.HasPermission(ctx, actionListMerchantPaymentMethods) {
		app.respondWithError(w, errors.New("forbidden: insufficient permissions"), http.StatusForbidden)
		return
	}

	userID := app.getUserIDFromContext(ctx)
	if userID == nil {
		app.respondWithError(w, errors.New("user ID not found in context"), http.StatusUnauthorized)
		return
	}

	merchantID, err := app.requireMerchantActorID(ctx)
	if err != nil {
		app.respondWithError(w, err, http.StatusBadRequest)
		return
	}

	limit, offset, err := parseMerchantPaymentMethodListPagination(r)
	if err != nil {
		app.respondWithError(w, err, http.StatusBadRequest)
		return
	}

	var statusFilter *data.MerchantPaymentMethodStatus
	if raw := strings.TrimSpace(r.URL.Query().Get("status")); raw != "" {
		normalized := data.NormalizeMerchantPaymentMethodStatus(data.MerchantPaymentMethodStatus(raw))
		if !data.IsValidMerchantPaymentMethodStatus(normalized) {
			app.respondWithError(w, fmt.Errorf("invalid status filter: %s", normalized), http.StatusBadRequest)
			return
		}
		statusFilter = &normalized
	}

	methods, err := app.Models.MerchantPaymentMethod.ListByMerchant(ctx, merchantID, statusFilter, limit, offset)
	if err != nil {
		logger.Error("list merchant payment methods failed",
			"merchant_id", merchantID,
			"error", err,
		)
		app.respondWithError(w, err, merchantPaymentMethodHTTPStatus(err))
		return
	}

	if err := app.insertGovernanceAudit(
		ctx, userID, actionListMerchantPaymentMethods,
		"List merchant payment methods",
		merchantPaymentMethodEntityType, merchantPaymentMethodEntityTypeDescription,
		merchantID.String(),
	); err != nil {
		logger.Warn("audit logging failed", "merchant_id", merchantID, "error", err)
		app.respondWithJSON(w, http.StatusOK, jsonResponse{
			Error: false,
			Message: "Merchant payment methods retrieved successfully; " +
				"audit recording requires operational attention",
			Data: toMerchantPaymentMethodResponseList(methods),
		})
		return
	}

	logger.Info("list merchant payment methods successful",
		"merchant_id", merchantID,
		"count", len(methods),
		"limit", limit,
		"offset", offset,
	)
	app.respondWithJSON(w, http.StatusOK, jsonResponse{
		Error:   false,
		Message: "Merchant payment methods retrieved successfully",
		Data:    toMerchantPaymentMethodResponseList(methods),
	})
}

// ---------------------------------------------------------------------------
// Safe display update
// ---------------------------------------------------------------------------

// UpdateMerchantPaymentMethodHandler replaces the complete safely mutable
// display-field subset (display_label and last_four) of a merchant payment method
// owned by the authenticated merchant actor.
//
// Both fields must be present; JSON null explicitly clears a field.
// This handler is not, and must never become, a generic update endpoint:
// ownership, payment_method_type, processor identity, status, default
// state, and deletion state are all outside the request DTO and cannot be
// changed through this path.
func (app *Application) UpdateMerchantPaymentMethodHandler(w http.ResponseWriter, r *http.Request) {
	logger := app.Logger.GetLoggerWithContext(r).WithFunctionName("UpdateMerchantPaymentMethodHandler")

	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	if !app.HasPermission(ctx, actionUpdateMerchantPaymentMethod) {
		app.respondWithError(w, errors.New("forbidden: insufficient permissions"), http.StatusForbidden)
		return
	}

	userID := app.getUserIDFromContext(ctx)
	if userID == nil {
		app.respondWithError(w, errors.New("user ID not found in context"), http.StatusUnauthorized)
		return
	}

	merchantID, err := app.requireMerchantActorID(ctx)
	if err != nil {
		app.respondWithError(w, err, http.StatusBadRequest)
		return
	}

	paymentMethodID, err := app.parseMerchantPaymentMethodID(r)
	if err != nil {
		app.respondWithError(w, err, http.StatusBadRequest)
		return
	}

	var input updateMerchantPaymentMethodRequest
	if err := app.readJSON(w, r, &input); err != nil {
		app.respondWithError(w, fmt.Errorf("invalid JSON input: %v", err), http.StatusBadRequest)
		return
	}
	if !input.DisplayLabel.Set || !input.LastFour.Set {
		app.respondWithError(
			w,
			errors.New("display_label and last_four are required; use null to clear a field"),
			http.StatusBadRequest,
		)
		return
	}

	method := &data.MerchantPaymentMethod{
		ID:           paymentMethodID,
		MerchantID:   merchantID,
		DisplayLabel: input.DisplayLabel.Value,
		LastFour:     input.LastFour.Value,
	}

	if err := app.Models.MerchantPaymentMethod.Update(ctx, method); err != nil {
		logger.Error("update merchant payment method failed",
			"merchant_id", merchantID,
			"payment_method_id", paymentMethodID,
			"error", err,
		)
		app.respondWithError(w, err, merchantPaymentMethodHTTPStatus(err))
		return
	}

	if err := app.insertGovernanceAudit(
		ctx, userID, actionUpdateMerchantPaymentMethod,
		"Update merchant payment method display metadata",
		merchantPaymentMethodEntityType, merchantPaymentMethodEntityTypeDescription,
		paymentMethodID.String(),
	); err != nil {
		logger.Warn("audit logging failed", "payment_method_id", paymentMethodID, "error", err)
		app.respondWithJSON(w, http.StatusOK, jsonResponse{
			Error: false,
			Message: "Merchant payment method updated successfully; " +
				"audit recording requires operational attention",
			Data: toMerchantPaymentMethodResponse(method),
		})
		return
	}

	app.respondWithJSON(w, http.StatusOK, jsonResponse{
		Error:   false,
		Message: "Merchant payment method updated successfully",
		Data:    toMerchantPaymentMethodResponse(method),
	})
}

// ---------------------------------------------------------------------------
// Default management
// ---------------------------------------------------------------------------

// SetDefaultMerchantPaymentMethodHandler assigns the specified payment
// method as the authenticated merchant's operational default.
//
// This handler performs no read-then-write default selection of its own;
// it delegates entirely to the data layer's atomic, row-locked SetDefault,
// which also enforces that only an active, non-deleted method may become
// the default.
func (app *Application) SetDefaultMerchantPaymentMethodHandler(w http.ResponseWriter, r *http.Request) {
	logger := app.Logger.GetLoggerWithContext(r).WithFunctionName("SetDefaultMerchantPaymentMethodHandler")

	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	if !app.HasPermission(ctx, actionSetDefaultMerchantPaymentMethod) {
		app.respondWithError(w, errors.New("forbidden: insufficient permissions"), http.StatusForbidden)
		return
	}

	userID := app.getUserIDFromContext(ctx)
	if userID == nil {
		app.respondWithError(w, errors.New("user ID not found in context"), http.StatusUnauthorized)
		return
	}

	merchantID, err := app.requireMerchantActorID(ctx)
	if err != nil {
		app.respondWithError(w, err, http.StatusBadRequest)
		return
	}

	paymentMethodID, err := app.parseMerchantPaymentMethodID(r)
	if err != nil {
		app.respondWithError(w, err, http.StatusBadRequest)
		return
	}

	if err := app.Models.MerchantPaymentMethod.SetDefault(ctx, merchantID, paymentMethodID); err != nil {
		logger.Error("set default merchant payment method failed",
			"merchant_id", merchantID,
			"payment_method_id", paymentMethodID,
			"error", err,
		)
		app.respondWithError(w, err, merchantPaymentMethodHTTPStatus(err))
		return
	}

	if err := app.insertGovernanceAudit(
		ctx, userID, actionSetDefaultMerchantPaymentMethod,
		"Set the merchant's default payment method",
		merchantPaymentMethodEntityType, merchantPaymentMethodEntityTypeDescription,
		paymentMethodID.String(),
	); err != nil {
		logger.Warn("audit logging failed", "payment_method_id", paymentMethodID, "error", err)
		app.respondWithJSON(w, http.StatusOK, jsonResponse{
			Error: false,
			Message: "Default merchant payment method set successfully; " +
				"audit recording requires operational attention",
			Data: paymentMethodID,
		})
		return
	}

	app.respondWithJSON(w, http.StatusOK, jsonResponse{
		Error:   false,
		Message: "Default merchant payment method set successfully",
		Data:    paymentMethodID,
	})
}

// ClearDefaultMerchantPaymentMethodHandler clears the specified payment
// method as the authenticated merchant's default, leaving the merchant
// with no operational default.
func (app *Application) ClearDefaultMerchantPaymentMethodHandler(w http.ResponseWriter, r *http.Request) {
	logger := app.Logger.GetLoggerWithContext(r).WithFunctionName("ClearDefaultMerchantPaymentMethodHandler")

	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	if !app.HasPermission(ctx, actionClearDefaultMerchantPaymentMethod) {
		app.respondWithError(w, errors.New("forbidden: insufficient permissions"), http.StatusForbidden)
		return
	}

	userID := app.getUserIDFromContext(ctx)
	if userID == nil {
		app.respondWithError(w, errors.New("user ID not found in context"), http.StatusUnauthorized)
		return
	}

	merchantID, err := app.requireMerchantActorID(ctx)
	if err != nil {
		app.respondWithError(w, err, http.StatusBadRequest)
		return
	}

	paymentMethodID, err := app.parseMerchantPaymentMethodID(r)
	if err != nil {
		app.respondWithError(w, err, http.StatusBadRequest)
		return
	}

	if err := app.Models.MerchantPaymentMethod.ClearDefault(ctx, merchantID, paymentMethodID); err != nil {
		logger.Error("clear default merchant payment method failed",
			"merchant_id", merchantID,
			"payment_method_id", paymentMethodID,
			"error", err,
		)
		app.respondWithError(w, err, merchantPaymentMethodHTTPStatus(err))
		return
	}

	if err := app.insertGovernanceAudit(
		ctx, userID, actionClearDefaultMerchantPaymentMethod,
		"Clear the merchant's default payment method",
		merchantPaymentMethodEntityType, merchantPaymentMethodEntityTypeDescription,
		paymentMethodID.String(),
	); err != nil {
		logger.Warn("audit logging failed", "payment_method_id", paymentMethodID, "error", err)
		app.respondWithJSON(w, http.StatusOK, jsonResponse{
			Error: false,
			Message: "Default merchant payment method cleared successfully; " +
				"audit recording requires operational attention",
			Data: paymentMethodID,
		})
		return
	}

	app.respondWithJSON(w, http.StatusOK, jsonResponse{
		Error:   false,
		Message: "Default merchant payment method cleared successfully",
		Data:    paymentMethodID,
	})
}

// ---------------------------------------------------------------------------
// Status management
// ---------------------------------------------------------------------------

// UpdateMerchantPaymentMethodStatusHandler transitions the status of a
// merchant payment method owned by the authenticated merchant actor.
//
// This handler does not implement its own lifecycle transition table. It
// normalizes and validates the requested status enum, then delegates to
// the data layer's UpdateStatus, whose predecessor-status WHERE clause is
// the sole authority for which transitions are legal — including the
// terminal nature of revoked, which this handler cannot bypass.
func (app *Application) UpdateMerchantPaymentMethodStatusHandler(w http.ResponseWriter, r *http.Request) {
	logger := app.Logger.GetLoggerWithContext(r).WithFunctionName("UpdateMerchantPaymentMethodStatusHandler")

	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	if !app.HasPermission(ctx, actionUpdateMerchantPaymentMethodStatus) {
		app.respondWithError(w, errors.New("forbidden: insufficient permissions"), http.StatusForbidden)
		return
	}

	userID := app.getUserIDFromContext(ctx)
	if userID == nil {
		app.respondWithError(w, errors.New("user ID not found in context"), http.StatusUnauthorized)
		return
	}

	merchantID, err := app.requireMerchantActorID(ctx)
	if err != nil {
		app.respondWithError(w, err, http.StatusBadRequest)
		return
	}

	paymentMethodID, err := app.parseMerchantPaymentMethodID(r)
	if err != nil {
		app.respondWithError(w, err, http.StatusBadRequest)
		return
	}

	var input updateMerchantPaymentMethodStatusRequest
	if err := app.readJSON(w, r, &input); err != nil {
		app.respondWithError(w, fmt.Errorf("invalid JSON input: %v", err), http.StatusBadRequest)
		return
	}

	normalizedStatus := data.NormalizeMerchantPaymentMethodStatus(input.Status)
	if !data.IsValidMerchantPaymentMethodStatus(normalizedStatus) {
		app.respondWithError(w, fmt.Errorf("invalid status: %s", normalizedStatus), http.StatusBadRequest)
		return
	}

	if err := app.Models.MerchantPaymentMethod.UpdateStatus(ctx, merchantID, paymentMethodID, normalizedStatus); err != nil {
		logger.Error("update merchant payment method status failed",
			"merchant_id", merchantID,
			"payment_method_id", paymentMethodID,
			"status", normalizedStatus,
			"error", err,
		)
		app.respondWithError(w, err, merchantPaymentMethodHTTPStatus(err))
		return
	}

	result := merchantPaymentMethodStatusResponse{ID: paymentMethodID, Status: normalizedStatus}

	if err := app.insertGovernanceAudit(
		ctx, userID, actionUpdateMerchantPaymentMethodStatus,
		"Update merchant payment method status",
		merchantPaymentMethodEntityType, merchantPaymentMethodEntityTypeDescription,
		paymentMethodID.String(),
	); err != nil {
		logger.Warn("audit logging failed", "payment_method_id", paymentMethodID, "error", err)
		app.respondWithJSON(w, http.StatusOK, jsonResponse{
			Error: false,
			Message: "Merchant payment method status updated successfully; " +
				"audit recording requires operational attention",
			Data: result,
		})
		return
	}

	app.respondWithJSON(w, http.StatusOK, jsonResponse{
		Error:   false,
		Message: "Merchant payment method status updated successfully",
		Data:    result,
	})
}

// ---------------------------------------------------------------------------
// Soft delete and restore
// ---------------------------------------------------------------------------

// SoftDeleteMerchantPaymentMethodHandler soft-deletes a merchant payment
// method owned by the authenticated merchant actor. The data layer's
// SoftDelete atomically clears is_default in the same statement, so a
// deleted method is never left as the merchant's operational default.
func (app *Application) SoftDeleteMerchantPaymentMethodHandler(w http.ResponseWriter, r *http.Request) {
	logger := app.Logger.GetLoggerWithContext(r).WithFunctionName("SoftDeleteMerchantPaymentMethodHandler")

	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	if !app.HasPermission(ctx, actionSoftDeleteMerchantPaymentMethod) {
		app.respondWithError(w, errors.New("forbidden: insufficient permissions"), http.StatusForbidden)
		return
	}

	userID := app.getUserIDFromContext(ctx)
	if userID == nil {
		app.respondWithError(w, errors.New("user ID not found in context"), http.StatusUnauthorized)
		return
	}

	merchantID, err := app.requireMerchantActorID(ctx)
	if err != nil {
		app.respondWithError(w, err, http.StatusBadRequest)
		return
	}

	paymentMethodID, err := app.parseMerchantPaymentMethodID(r)
	if err != nil {
		app.respondWithError(w, err, http.StatusBadRequest)
		return
	}

	if err := app.Models.MerchantPaymentMethod.SoftDelete(ctx, merchantID, paymentMethodID); err != nil {
		logger.Error("soft delete merchant payment method failed",
			"merchant_id", merchantID,
			"payment_method_id", paymentMethodID,
			"error", err,
		)
		app.respondWithError(w, err, merchantPaymentMethodHTTPStatus(err))
		return
	}

	if err := app.insertGovernanceAudit(
		ctx, userID, actionSoftDeleteMerchantPaymentMethod,
		"Soft-delete a merchant payment method",
		merchantPaymentMethodEntityType, merchantPaymentMethodEntityTypeDescription,
		paymentMethodID.String(),
	); err != nil {
		logger.Warn("audit logging failed", "payment_method_id", paymentMethodID, "error", err)
		app.respondWithJSON(w, http.StatusOK, jsonResponse{
			Error: false,
			Message: "Merchant payment method soft-deleted successfully; " +
				"audit recording requires operational attention",
			Data: paymentMethodID,
		})
		return
	}

	app.respondWithJSON(w, http.StatusOK, jsonResponse{
		Error:   false,
		Message: "Merchant payment method soft-deleted successfully",
		Data:    paymentMethodID,
	})
}

// RestoreMerchantPaymentMethodHandler restores a soft-deleted merchant
// payment method owned by the authenticated merchant actor.
//
// Restore never reactivates status and never restores default state; the
// data layer's Restore leaves both exactly as SoftDelete left them
// (status untouched, is_default false). A caller that wants the restored
// method to become active and/or default again must invoke
// UpdateMerchantPaymentMethodStatusHandler and/or
// SetDefaultMerchantPaymentMethodHandler as explicit subsequent steps.
func (app *Application) RestoreMerchantPaymentMethodHandler(w http.ResponseWriter, r *http.Request) {
	logger := app.Logger.GetLoggerWithContext(r).WithFunctionName("RestoreMerchantPaymentMethodHandler")

	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	if !app.HasPermission(ctx, actionRestoreMerchantPaymentMethod) {
		app.respondWithError(w, errors.New("forbidden: insufficient permissions"), http.StatusForbidden)
		return
	}

	userID := app.getUserIDFromContext(ctx)
	if userID == nil {
		app.respondWithError(w, errors.New("user ID not found in context"), http.StatusUnauthorized)
		return
	}

	merchantID, err := app.requireMerchantActorID(ctx)
	if err != nil {
		app.respondWithError(w, err, http.StatusBadRequest)
		return
	}

	paymentMethodID, err := app.parseMerchantPaymentMethodID(r)
	if err != nil {
		app.respondWithError(w, err, http.StatusBadRequest)
		return
	}

	if err := app.Models.MerchantPaymentMethod.Restore(ctx, merchantID, paymentMethodID); err != nil {
		logger.Error("restore merchant payment method failed",
			"merchant_id", merchantID,
			"payment_method_id", paymentMethodID,
			"error", err,
		)
		app.respondWithError(w, err, merchantPaymentMethodHTTPStatus(err))
		return
	}

	if err := app.insertGovernanceAudit(
		ctx, userID, actionRestoreMerchantPaymentMethod,
		"Restore a merchant payment method",
		merchantPaymentMethodEntityType, merchantPaymentMethodEntityTypeDescription,
		paymentMethodID.String(),
	); err != nil {
		logger.Warn("audit logging failed", "payment_method_id", paymentMethodID, "error", err)
		app.respondWithJSON(w, http.StatusOK, jsonResponse{
			Error: false,
			Message: "Merchant payment method restored successfully; " +
				"audit recording requires operational attention",
			Data: paymentMethodID,
		})
		return
	}

	app.respondWithJSON(w, http.StatusOK, jsonResponse{
		Error:   false,
		Message: "Merchant payment method restored successfully",
		Data:    paymentMethodID,
	})
}
