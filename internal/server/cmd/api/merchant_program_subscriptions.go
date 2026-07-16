// Package main provides HTTP handlers for merchant program subscription lifecycle.
//
// sdworkspace/sdbackend/internal/server/cmd/api/merchant_program_subscriptions.go
//
// GTM:
//
//	Layer: 2.4 Merchant / Future Offering Domain
//	Release Class: SPINE
//	Reason:
//	  merchant_program_subscriptions handler surface is release-critical merchant
//	  monetization infrastructure. It governs merchant-plan subscription
//	  lifecycle records used for Future Offering readiness, Merchant Center
//	  plan state, entitlement eligibility, and merchant program access.
//
//	  This file does not implement checkout, invoice handling, payment
//	  processing, settlement, fee calculation, merchant-of-record behavior, or
//	  consumer Future Commerce engagement. Those concerns belong elsewhere.
//
// SPINE Rule:
//
//	Keep compiling.
//	Keep production-ready.
//	Preserve subscription-status integrity.
//	Preserve billing-period integrity.
//	Preserve one-current-subscription-per-merchant invariant.
//	Preserve authorization and audit coverage for privileged operations.
//	Preserve soft-delete lifecycle semantics.
//	Block deployment if this file breaks build, merchant subscription governance,
//	Future Offering access readiness, entitlement readiness, or Merchant Center
//	plan-state integrity.
package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/PiccoloMondoC/sdworkspace/sdbackend/internal/data"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

const (
	merchantProgramSubscriptionEntityType = "merchant_program_subscription"

	actionCreateMerchantProgramSubscription          = "create_merchant_program_subscription"
	actionReadMerchantProgramSubscription            = "read_merchant_program_subscription"
	actionReadCurrentMerchantProgramSubscription     = "read_current_merchant_program_subscription"
	actionReadActiveMerchantProgramSubscription      = "read_active_merchant_program_subscription"
	actionReadMerchantProgramSubscriptionsByMerchant = "read_merchant_program_subscriptions_by_merchant"
	actionReadMerchantProgramSubscriptionsByPlan     = "read_merchant_program_subscriptions_by_plan"
	actionUpdateMerchantProgramSubscriptionPlan      = "update_merchant_program_subscription_plan"
	actionUpdateMerchantProgramSubscriptionStatus    = "update_merchant_program_subscription_status"
	actionCancelMerchantProgramSubscription          = "cancel_merchant_program_subscription"
	actionSoftDeleteMerchantProgramSubscription      = "soft_delete_merchant_program_subscription"
	actionRestoreMerchantProgramSubscription         = "restore_merchant_program_subscription"
)


// merchantProgramSubscriptionHTTPStatus maps data-layer validation and lifecycle
// errors to HTTP status codes. Keep data-layer not-found wording aligned with
// this mapper unless/until merchant program subscription sentinels are added.
func merchantProgramSubscriptionHTTPStatus(err error) int {
	if err == nil {
		return http.StatusOK
	}

	msg := strings.ToLower(err.Error())

	switch {
	case strings.Contains(msg, "already has a current merchant program subscription"),
		strings.Contains(msg, "already exists"):
		return http.StatusConflict
	case strings.Contains(msg, "not found"),
		strings.Contains(msg, "no active merchant program subscription"),
		strings.Contains(msg, "no current merchant program subscription"),
		strings.Contains(msg, "no soft-deleted merchant program subscription"):
		return http.StatusNotFound
	case strings.Contains(msg, "references missing"),
		strings.Contains(msg, "references a missing"):
		return http.StatusUnprocessableEntity
	case strings.Contains(msg, "invalid"),
		strings.Contains(msg, "required"),
		strings.Contains(msg, "must be"),
		strings.Contains(msg, "limit"),
		strings.Contains(msg, "offset"):
		return http.StatusBadRequest
	default:
		return http.StatusInternalServerError
	}
}

func (app *Application) parseMerchantProgramSubscriptionID(r *http.Request) (uuid.UUID, error) {
	raw := strings.TrimSpace(chi.URLParam(r, "merchantProgramSubscriptionID"))
	if raw == "" {
		return uuid.Nil, errors.New("merchant program subscription ID is required")
	}

	id, err := uuid.Parse(raw)
	if err != nil || id == uuid.Nil {
		return uuid.Nil, errors.New("invalid merchant program subscription ID")
	}

	return id, nil
}

func parseMerchantProgramSubscriptionUUIDQuery(r *http.Request, key string) (uuid.UUID, error) {
	raw := strings.TrimSpace(r.URL.Query().Get(key))
	if raw == "" {
		return uuid.Nil, fmt.Errorf("%s query parameter is required", key)
	}

	id, err := uuid.Parse(raw)
	if err != nil || id == uuid.Nil {
		return uuid.Nil, fmt.Errorf("invalid %s query parameter", key)
	}

	return id, nil
}

func parseMerchantProgramSubscriptionPagination(r *http.Request) (int, int, error) {
	limit := 20
	offset := 0

	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil {
			return 0, 0, errors.New("limit must be an integer")
		}
		limit = value
	}

	if raw := strings.TrimSpace(r.URL.Query().Get("offset")); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil {
			return 0, 0, errors.New("offset must be an integer")
		}
		offset = value
	}

	if limit <= 0 || limit > 100 {
		return 0, 0, errors.New("limit must be between 1 and 100")
	}
	if offset < 0 {
		return 0, 0, errors.New("offset must be non-negative")
	}

	return limit, offset, nil
}

func parseOptionalRFC3339Time(value string, fieldName string) (*time.Time, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, nil
	}

	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return nil, fmt.Errorf("%s must be RFC3339 timestamp", fieldName)
	}

	utc := parsed.UTC()
	return &utc, nil
}

func (app *Application) auditMerchantProgramSubscription(
	ctx context.Context,
	userID *uuid.UUID,
	actionName string,
	actionDescription string,
	entityID string,
) error {
	if userID == nil {
		return errors.New("user ID is required for audit logging")
	}

	action, err := app.Models.Action.GetByName(ctx, actionName)
	if err != nil || action == nil {
		actionID, createErr := app.Models.Action.CreateIfNotExists(ctx, actionName, actionDescription)
		if createErr != nil {
			return fmt.Errorf("resolve audit action %s: %w", actionName, createErr)
		}
		action = &data.Action{ID: actionID}
	}

	entityType, err := app.Models.EntityType.GetByName(ctx, merchantProgramSubscriptionEntityType)
	if err != nil || entityType == nil {
		entityTypeID, createErr := app.Models.EntityType.CreateIfNotExists(
			ctx,
			merchantProgramSubscriptionEntityType,
			"Merchant program subscription lifecycle entity",
		)
		if createErr != nil {
			return fmt.Errorf("resolve audit entity type %s: %w", merchantProgramSubscriptionEntityType, createErr)
		}
		entityType = &data.EntityType{ID: entityTypeID}
	}

	auditLog := data.AuditLog{
		ID:           uuid.New(),
		UserID:       userID,
		ActionID:     action.ID,
		EntityTypeID: entityType.ID,
		EntityID:     entityID,
	}

	if err := app.Models.AuditLog.Insert(ctx, &auditLog); err != nil {
		return fmt.Errorf("insert merchant program subscription audit log: %w", err)
	}

	return nil
}

// CreateMerchantProgramSubscriptionHandler creates a merchant program subscription.
func (app *Application) CreateMerchantProgramSubscriptionHandler(w http.ResponseWriter, r *http.Request) {
	logger := app.Logger.GetLoggerWithContext(r).WithFunctionName("CreateMerchantProgramSubscriptionHandler")

	if !app.HasPermission(r.Context(), actionCreateMerchantProgramSubscription) {
		app.respondWithError(w, errors.New("forbidden: insufficient permissions"), http.StatusForbidden)
		return
	}

	userID := app.getUserIDFromContext(r.Context())
	if userID == nil {
		app.respondWithError(w, errors.New("user ID not found in context"), http.StatusUnauthorized)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	var input struct {
		MerchantID    uuid.UUID                                      `json:"merchant_id"`
		PlanID        uuid.UUID                                      `json:"plan_id"`
		Status        data.MerchantProgramSubscriptionStatus         `json:"status,omitempty"`
		BillingPeriod data.MerchantProgramSubscriptionBillingPeriod  `json:"billing_period,omitempty"`
		StartedAt     string                                         `json:"started_at,omitempty"`
		ExpiresAt     string                                         `json:"expires_at,omitempty"`
		CancelledAt   string                                         `json:"cancelled_at,omitempty"`
	}

	if err := app.readJSON(w, r, &input); err != nil {
		app.respondWithError(w, fmt.Errorf("invalid JSON input: %v", err), http.StatusBadRequest)
		return
	}

	startedAt, err := parseOptionalRFC3339Time(input.StartedAt, "started_at")
	if err != nil {
		app.respondWithError(w, err, http.StatusBadRequest)
		return
	}

	expiresAt, err := parseOptionalRFC3339Time(input.ExpiresAt, "expires_at")
	if err != nil {
		app.respondWithError(w, err, http.StatusBadRequest)
		return
	}

	cancelledAt, err := parseOptionalRFC3339Time(input.CancelledAt, "cancelled_at")
	if err != nil {
		app.respondWithError(w, err, http.StatusBadRequest)
		return
	}

	subscription := &data.MerchantProgramSubscription{
		MerchantID:    input.MerchantID,
		PlanID:        input.PlanID,
		Status:        input.Status,
		BillingPeriod: input.BillingPeriod,
		StartedAt:     startedAt,
		ExpiresAt:     expiresAt,
		CancelledAt:   cancelledAt,
	}

	if err := app.Models.MerchantProgramSubscription.Insert(ctx, subscription); err != nil {
		logger.Error("Create merchant program subscription failed", "error", err)
		app.respondWithError(w, err, merchantProgramSubscriptionHTTPStatus(err))
		return
	}

	if err := app.auditMerchantProgramSubscription(ctx, userID, actionCreateMerchantProgramSubscription, "Create a merchant program subscription", subscription.ID.String()); err != nil {
		logger.Warn("Audit logging failed", "subscription_id", subscription.ID, "error", err)
		app.respondWithJSON(w, http.StatusPartialContent, jsonResponse{
			Error:   false,
			Message: "Merchant program subscription created, but audit logging failed",
			Data:    subscription,
		})
		return
	}

	app.respondWithJSON(w, http.StatusCreated, jsonResponse{
		Error:   false,
		Message: "Merchant program subscription created successfully",
		Data:    subscription,
	})
}

// GetMerchantProgramSubscriptionByIDHandler retrieves a merchant program subscription by ID.
func (app *Application) GetMerchantProgramSubscriptionByIDHandler(w http.ResponseWriter, r *http.Request) {
	logger := app.Logger.GetLoggerWithContext(r).WithFunctionName("GetMerchantProgramSubscriptionByIDHandler")

	if !app.HasPermission(r.Context(), actionReadMerchantProgramSubscription) {
		app.respondWithError(w, errors.New("forbidden: insufficient permissions"), http.StatusForbidden)
		return
	}

	userID := app.getUserIDFromContext(r.Context())
	if userID == nil {
		app.respondWithError(w, errors.New("user ID not found in context"), http.StatusUnauthorized)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	subscriptionID, err := app.parseMerchantProgramSubscriptionID(r)
	if err != nil {
		app.respondWithError(w, err, http.StatusBadRequest)
		return
	}

	subscription, err := app.Models.MerchantProgramSubscription.GetByID(ctx, subscriptionID)
	if err != nil {
		logger.Error("Get merchant program subscription by ID failed", "subscription_id", subscriptionID, "error", err)
		app.respondWithError(w, err, merchantProgramSubscriptionHTTPStatus(err))
		return
	}
	if subscription == nil {
		app.respondWithError(w, errors.New("merchant program subscription not found"), http.StatusNotFound)
		return
	}

	if err := app.auditMerchantProgramSubscription(ctx, userID, actionReadMerchantProgramSubscription, "Read a merchant program subscription", subscription.ID.String()); err != nil {
		logger.Warn("Audit logging failed", "subscription_id", subscription.ID, "error", err)
		app.respondWithJSON(w, http.StatusPartialContent, jsonResponse{
			Error:   false,
			Message: "Merchant program subscription retrieved, but audit logging failed",
			Data:    subscription,
		})
		return
	}

	app.respondWithJSON(w, http.StatusOK, jsonResponse{
		Error:   false,
		Message: "Merchant program subscription retrieved successfully",
		Data:    subscription,
	})
}


// GetCurrentMerchantProgramSubscriptionByMerchantIDHandler retrieves the current subscription for a merchant.
func (app *Application) GetCurrentMerchantProgramSubscriptionByMerchantIDHandler(w http.ResponseWriter, r *http.Request) {
	logger := app.Logger.GetLoggerWithContext(r).WithFunctionName("GetCurrentMerchantProgramSubscriptionByMerchantIDHandler")

	if !app.HasPermission(r.Context(), actionReadCurrentMerchantProgramSubscription) {
		app.respondWithError(w, errors.New("forbidden: insufficient permissions"), http.StatusForbidden)
		return
	}

	userID := app.getUserIDFromContext(r.Context())
	if userID == nil {
		app.respondWithError(w, errors.New("user ID not found in context"), http.StatusUnauthorized)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	merchantID, err := parseMerchantProgramSubscriptionUUIDQuery(r, "merchant_id")
	if err != nil {
		app.respondWithError(w, err, http.StatusBadRequest)
		return
	}

	subscription, err := app.Models.MerchantProgramSubscription.GetCurrentByMerchantID(ctx, merchantID)
	if err != nil {
		logger.Error("Get current merchant program subscription failed", "merchant_id", merchantID, "error", err)
		app.respondWithError(w, err, merchantProgramSubscriptionHTTPStatus(err))
		return
	}
	if subscription == nil {
		app.respondWithError(w, errors.New("no current merchant program subscription found"), http.StatusNotFound)
		return
	}

	if err := app.auditMerchantProgramSubscription(
		ctx,
		userID,
		actionReadCurrentMerchantProgramSubscription,
		"Read current merchant program subscription",
		subscription.ID.String(),
	); err != nil {
		logger.Warn("Audit logging failed", "subscription_id", subscription.ID, "merchant_id", merchantID, "error", err)
		app.respondWithJSON(w, http.StatusPartialContent, jsonResponse{
			Error:   false,
			Message: "Current merchant program subscription retrieved, but audit logging failed",
			Data:    subscription,
		})
		return
	}

	app.respondWithJSON(w, http.StatusOK, jsonResponse{
		Error:   false,
		Message: "Current merchant program subscription retrieved successfully",
		Data:    subscription,
	})
}


// GetActiveMerchantProgramSubscriptionByMerchantIDHandler retrieves the active subscription for a merchant.
func (app *Application) GetActiveMerchantProgramSubscriptionByMerchantIDHandler(w http.ResponseWriter, r *http.Request) {
	logger := app.Logger.GetLoggerWithContext(r).WithFunctionName("GetActiveMerchantProgramSubscriptionByMerchantIDHandler")

	if !app.HasPermission(r.Context(), actionReadActiveMerchantProgramSubscription) {
		app.respondWithError(w, errors.New("forbidden: insufficient permissions"), http.StatusForbidden)
		return
	}

	userID := app.getUserIDFromContext(r.Context())
	if userID == nil {
		app.respondWithError(w, errors.New("user ID not found in context"), http.StatusUnauthorized)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	merchantID, err := parseMerchantProgramSubscriptionUUIDQuery(r, "merchant_id")
	if err != nil {
		app.respondWithError(w, err, http.StatusBadRequest)
		return
	}

	subscription, err := app.Models.MerchantProgramSubscription.GetActiveByMerchantID(ctx, merchantID)
	if err != nil {
		logger.Error("Get active merchant program subscription failed", "merchant_id", merchantID, "error", err)
		app.respondWithError(w, err, merchantProgramSubscriptionHTTPStatus(err))
		return
	}
	if subscription == nil {
		app.respondWithError(w, errors.New("no active merchant program subscription found"), http.StatusNotFound)
		return
	}

	if err := app.auditMerchantProgramSubscription(
		ctx,
		userID,
		actionReadActiveMerchantProgramSubscription,
		"Read active merchant program subscription",
		subscription.ID.String(),
	); err != nil {
		logger.Warn("Audit logging failed", "subscription_id", subscription.ID, "merchant_id", merchantID, "error", err)
		app.respondWithJSON(w, http.StatusPartialContent, jsonResponse{
			Error:   false,
			Message: "Active merchant program subscription retrieved, but audit logging failed",
			Data:    subscription,
		})
		return
	}

	app.respondWithJSON(w, http.StatusOK, jsonResponse{
		Error:   false,
		Message: "Active merchant program subscription retrieved successfully",
		Data:    subscription,
	})
}


// ListMerchantProgramSubscriptionsByMerchantIDHandler lists subscriptions for one merchant.
func (app *Application) ListMerchantProgramSubscriptionsByMerchantIDHandler(w http.ResponseWriter, r *http.Request) {
	logger := app.Logger.GetLoggerWithContext(r).WithFunctionName("ListMerchantProgramSubscriptionsByMerchantIDHandler")

	if !app.HasPermission(r.Context(), actionReadMerchantProgramSubscriptionsByMerchant) {
		app.respondWithError(w, errors.New("forbidden: insufficient permissions"), http.StatusForbidden)
		return
	}

	userID := app.getUserIDFromContext(r.Context())
	if userID == nil {
		app.respondWithError(w, errors.New("user ID not found in context"), http.StatusUnauthorized)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	merchantID, err := parseMerchantProgramSubscriptionUUIDQuery(r, "merchant_id")
	if err != nil {
		app.respondWithError(w, err, http.StatusBadRequest)
		return
	}

	limit, offset, err := parseMerchantProgramSubscriptionPagination(r)
	if err != nil {
		app.respondWithError(w, err, http.StatusBadRequest)
		return
	}

	includeDeleted := strings.EqualFold(strings.TrimSpace(r.URL.Query().Get("include_deleted")), "true")

	subscriptions, err := app.Models.MerchantProgramSubscription.ListByMerchantID(ctx, merchantID, includeDeleted, limit, offset)
	if err != nil {
		logger.Error("List merchant program subscriptions by merchant failed", "merchant_id", merchantID, "error", err)
		app.respondWithError(w, err, merchantProgramSubscriptionHTTPStatus(err))
		return
	}

	if err := app.auditMerchantProgramSubscription(ctx, userID, actionReadMerchantProgramSubscriptionsByMerchant, "List merchant program subscriptions by merchant", merchantID.String()); err != nil {
		logger.Warn("Audit logging failed", "merchant_id", merchantID, "error", err)
		app.respondWithJSON(w, http.StatusPartialContent, jsonResponse{
			Error:   false,
			Message: "Merchant program subscriptions retrieved, but audit logging failed",
			Data:    subscriptions,
		})
		return
	}

	app.respondWithJSON(w, http.StatusOK, jsonResponse{
		Error:   false,
		Message: "Merchant program subscriptions retrieved successfully",
		Data:    subscriptions,
	})
}

// ListMerchantProgramSubscriptionsByPlanAndStatusHandler lists subscriptions by plan and status.
func (app *Application) ListMerchantProgramSubscriptionsByPlanAndStatusHandler(w http.ResponseWriter, r *http.Request) {
	logger := app.Logger.GetLoggerWithContext(r).WithFunctionName("ListMerchantProgramSubscriptionsByPlanAndStatusHandler")

	if !app.HasPermission(r.Context(), actionReadMerchantProgramSubscriptionsByPlan) {
		app.respondWithError(w, errors.New("forbidden: insufficient permissions"), http.StatusForbidden)
		return
	}

	userID := app.getUserIDFromContext(r.Context())
	if userID == nil {
		app.respondWithError(w, errors.New("user ID not found in context"), http.StatusUnauthorized)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	planID, err := parseMerchantProgramSubscriptionUUIDQuery(r, "plan_id")
	if err != nil {
		app.respondWithError(w, err, http.StatusBadRequest)
		return
	}

	rawStatus := strings.TrimSpace(r.URL.Query().Get("status"))
	if rawStatus == "" {
		app.respondWithError(w, errors.New("status query parameter is required"), http.StatusBadRequest)
		return
	}

	status := data.NormalizeMerchantProgramSubscriptionStatus(data.MerchantProgramSubscriptionStatus(rawStatus))
	if !data.IsValidMerchantProgramSubscriptionStatus(status) {
		app.respondWithError(w, fmt.Errorf("invalid merchant program subscription status: %s", rawStatus), http.StatusBadRequest)
		return
	}

	limit, offset, err := parseMerchantProgramSubscriptionPagination(r)
	if err != nil {
		app.respondWithError(w, err, http.StatusBadRequest)
		return
	}

	subscriptions, err := app.Models.MerchantProgramSubscription.ListByPlanAndStatus(ctx, planID, status, limit, offset)
	if err != nil {
		logger.Error("List merchant program subscriptions by plan and status failed", "plan_id", planID, "status", status, "error", err)
		app.respondWithError(w, err, merchantProgramSubscriptionHTTPStatus(err))
		return
	}

	if err := app.auditMerchantProgramSubscription(ctx, userID, actionReadMerchantProgramSubscriptionsByPlan, "List merchant program subscriptions by plan and status", planID.String()); err != nil {
		logger.Warn("Audit logging failed", "plan_id", planID, "status", status, "error", err)
		app.respondWithJSON(w, http.StatusPartialContent, jsonResponse{
			Error:   false,
			Message: "Merchant program subscriptions retrieved, but audit logging failed",
			Data:    subscriptions,
		})
		return
	}

	app.respondWithJSON(w, http.StatusOK, jsonResponse{
		Error:   false,
		Message: "Merchant program subscriptions retrieved successfully",
		Data:    subscriptions,
	})
}


// UpdateMerchantProgramSubscriptionPlanHandler updates a subscription's plan.
func (app *Application) UpdateMerchantProgramSubscriptionPlanHandler(w http.ResponseWriter, r *http.Request) {
	logger := app.Logger.GetLoggerWithContext(r).WithFunctionName("UpdateMerchantProgramSubscriptionPlanHandler")

	if !app.HasPermission(r.Context(), actionUpdateMerchantProgramSubscriptionPlan) {
		app.respondWithError(w, errors.New("forbidden: insufficient permissions"), http.StatusForbidden)
		return
	}

	userID := app.getUserIDFromContext(r.Context())
	if userID == nil {
		app.respondWithError(w, errors.New("user ID not found in context"), http.StatusUnauthorized)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	subscriptionID, err := app.parseMerchantProgramSubscriptionID(r)
	if err != nil {
		app.respondWithError(w, err, http.StatusBadRequest)
		return
	}

	var input struct {
		PlanID uuid.UUID `json:"plan_id"`
	}

	if err := app.readJSON(w, r, &input); err != nil {
		app.respondWithError(w, fmt.Errorf("invalid JSON input: %v", err), http.StatusBadRequest)
		return
	}

	if input.PlanID == uuid.Nil {
		app.respondWithError(w, errors.New("plan_id is required"), http.StatusBadRequest)
		return
	}

	if err := app.Models.MerchantProgramSubscription.UpdatePlan(ctx, subscriptionID, input.PlanID); err != nil {
		logger.Error("Update merchant program subscription plan failed", "subscription_id", subscriptionID, "plan_id", input.PlanID, "error", err)
		app.respondWithError(w, err, merchantProgramSubscriptionHTTPStatus(err))
		return
	}

	if err := app.auditMerchantProgramSubscription(ctx, userID, actionUpdateMerchantProgramSubscriptionPlan, "Update merchant program subscription plan", subscriptionID.String()); err != nil {
		logger.Warn("Audit logging failed", "subscription_id", subscriptionID, "error", err)
		app.respondWithJSON(w, http.StatusPartialContent, jsonResponse{
			Error:   false,
			Message: "Merchant program subscription plan updated, but audit logging failed",
			Data:    subscriptionID,
		})
		return
	}

	app.respondWithJSON(w, http.StatusOK, jsonResponse{
		Error:   false,
		Message: "Merchant program subscription plan updated successfully",
		Data:    subscriptionID,
	})
}

func (app *Application) transitionMerchantProgramSubscriptionStatus(
	w http.ResponseWriter,
	r *http.Request,
	functionName string,
	actionName string,
	actionDescription string,
	successMessage string,
	modelFn func(context.Context, uuid.UUID) error,
) {
	logger := app.Logger.GetLoggerWithContext(r).WithFunctionName(functionName)

	if !app.HasPermission(r.Context(), actionName) {
		app.respondWithError(w, errors.New("forbidden: insufficient permissions"), http.StatusForbidden)
		return
	}

	userID := app.getUserIDFromContext(r.Context())
	if userID == nil {
		app.respondWithError(w, errors.New("user ID not found in context"), http.StatusUnauthorized)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	subscriptionID, err := app.parseMerchantProgramSubscriptionID(r)
	if err != nil {
		app.respondWithError(w, err, http.StatusBadRequest)
		return
	}

	if err := modelFn(ctx, subscriptionID); err != nil {
		logger.Error("Merchant program subscription status transition failed", "subscription_id", subscriptionID, "action", actionName, "error", err)
		app.respondWithError(w, err, merchantProgramSubscriptionHTTPStatus(err))
		return
	}

	if err := app.auditMerchantProgramSubscription(ctx, userID, actionName, actionDescription, subscriptionID.String()); err != nil {
		logger.Warn("Audit logging failed", "subscription_id", subscriptionID, "error", err)
		app.respondWithJSON(w, http.StatusPartialContent, jsonResponse{
			Error:   false,
			Message: successMessage + ", but audit logging failed",
			Data:    subscriptionID,
		})
		return
	}

	app.respondWithJSON(w, http.StatusOK, jsonResponse{
		Error:   false,
		Message: successMessage,
		Data:    subscriptionID,
	})
}

// ActivateMerchantProgramSubscriptionHandler activates a subscription.
func (app *Application) ActivateMerchantProgramSubscriptionHandler(w http.ResponseWriter, r *http.Request) {
	app.transitionMerchantProgramSubscriptionStatus(
		w,
		r,
		"ActivateMerchantProgramSubscriptionHandler",
		actionUpdateMerchantProgramSubscriptionStatus,
		"Activate a merchant program subscription",
		"Merchant program subscription activated successfully",
		app.Models.MerchantProgramSubscription.Activate,
	)
}

// PauseMerchantProgramSubscriptionHandler pauses a subscription.
func (app *Application) PauseMerchantProgramSubscriptionHandler(w http.ResponseWriter, r *http.Request) {
	app.transitionMerchantProgramSubscriptionStatus(
		w,
		r,
		"PauseMerchantProgramSubscriptionHandler",
		actionUpdateMerchantProgramSubscriptionStatus,
		"Pause a merchant program subscription",
		"Merchant program subscription paused successfully",
		app.Models.MerchantProgramSubscription.Pause,
	)
}

// SuspendMerchantProgramSubscriptionHandler suspends a subscription.
func (app *Application) SuspendMerchantProgramSubscriptionHandler(w http.ResponseWriter, r *http.Request) {
	app.transitionMerchantProgramSubscriptionStatus(
		w,
		r,
		"SuspendMerchantProgramSubscriptionHandler",
		actionUpdateMerchantProgramSubscriptionStatus,
		"Suspend a merchant program subscription",
		"Merchant program subscription suspended successfully",
		app.Models.MerchantProgramSubscription.Suspend,
	)
}

// ExpireMerchantProgramSubscriptionHandler expires a subscription.
func (app *Application) ExpireMerchantProgramSubscriptionHandler(w http.ResponseWriter, r *http.Request) {
	app.transitionMerchantProgramSubscriptionStatus(
		w,
		r,
		"ExpireMerchantProgramSubscriptionHandler",
		actionUpdateMerchantProgramSubscriptionStatus,
		"Expire a merchant program subscription",
		"Merchant program subscription expired successfully",
		app.Models.MerchantProgramSubscription.Expire,
	)
}

// CancelMerchantProgramSubscriptionHandler cancels a subscription.
func (app *Application) CancelMerchantProgramSubscriptionHandler(w http.ResponseWriter, r *http.Request) {
	app.transitionMerchantProgramSubscriptionStatus(
		w,
		r,
		"CancelMerchantProgramSubscriptionHandler",
		actionCancelMerchantProgramSubscription,
		"Cancel a merchant program subscription",
		"Merchant program subscription cancelled successfully",
		app.Models.MerchantProgramSubscription.Cancel,
	)
}

// SoftDeleteMerchantProgramSubscriptionHandler soft-deletes a subscription.
func (app *Application) SoftDeleteMerchantProgramSubscriptionHandler(w http.ResponseWriter, r *http.Request) {
	logger := app.Logger.GetLoggerWithContext(r).WithFunctionName("SoftDeleteMerchantProgramSubscriptionHandler")

	if !app.HasPermission(r.Context(), actionSoftDeleteMerchantProgramSubscription) {
		app.respondWithError(w, errors.New("forbidden: insufficient permissions"), http.StatusForbidden)
		return
	}

	userID := app.getUserIDFromContext(r.Context())
	if userID == nil {
		app.respondWithError(w, errors.New("user ID not found in context"), http.StatusUnauthorized)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	subscriptionID, err := app.parseMerchantProgramSubscriptionID(r)
	if err != nil {
		app.respondWithError(w, err, http.StatusBadRequest)
		return
	}

	if err := app.Models.MerchantProgramSubscription.SoftDelete(ctx, subscriptionID); err != nil {
		logger.Error("Soft delete merchant program subscription failed", "subscription_id", subscriptionID, "error", err)
		app.respondWithError(w, err, merchantProgramSubscriptionHTTPStatus(err))
		return
	}

	if err := app.auditMerchantProgramSubscription(ctx, userID, actionSoftDeleteMerchantProgramSubscription, "Soft-delete a merchant program subscription", subscriptionID.String()); err != nil {
		logger.Warn("Audit logging failed", "subscription_id", subscriptionID, "error", err)
		app.respondWithJSON(w, http.StatusPartialContent, jsonResponse{
			Error:   false,
			Message: "Merchant program subscription soft-deleted, but audit logging failed",
			Data:    subscriptionID,
		})
		return
	}

	app.respondWithJSON(w, http.StatusOK, jsonResponse{
		Error:   false,
		Message: "Merchant program subscription soft-deleted successfully",
		Data:    subscriptionID,
	})
}

// RestoreMerchantProgramSubscriptionHandler restores a soft-deleted subscription.
func (app *Application) RestoreMerchantProgramSubscriptionHandler(w http.ResponseWriter, r *http.Request) {
	logger := app.Logger.GetLoggerWithContext(r).WithFunctionName("RestoreMerchantProgramSubscriptionHandler")

	if !app.HasPermission(r.Context(), actionRestoreMerchantProgramSubscription) {
		app.respondWithError(w, errors.New("forbidden: insufficient permissions"), http.StatusForbidden)
		return
	}

	userID := app.getUserIDFromContext(r.Context())
	if userID == nil {
		app.respondWithError(w, errors.New("user ID not found in context"), http.StatusUnauthorized)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	subscriptionID, err := app.parseMerchantProgramSubscriptionID(r)
	if err != nil {
		app.respondWithError(w, err, http.StatusBadRequest)
		return
	}

	if err := app.Models.MerchantProgramSubscription.Restore(ctx, subscriptionID); err != nil {
		logger.Error("Restore merchant program subscription failed", "subscription_id", subscriptionID, "error", err)
		app.respondWithError(w, err, merchantProgramSubscriptionHTTPStatus(err))
		return
	}

	if err := app.auditMerchantProgramSubscription(ctx, userID, actionRestoreMerchantProgramSubscription, "Restore a merchant program subscription", subscriptionID.String()); err != nil {
		logger.Warn("Audit logging failed", "subscription_id", subscriptionID, "error", err)
		app.respondWithJSON(w, http.StatusPartialContent, jsonResponse{
			Error:   false,
			Message: "Merchant program subscription restored, but audit logging failed",
			Data:    subscriptionID,
		})
		return
	}

	app.respondWithJSON(w, http.StatusOK, jsonResponse{
		Error:   false,
		Message: "Merchant program subscription restored successfully",
		Data:    subscriptionID,
	})
}