// Package main provides HTTP handlers for merchant program plan governance.
//
// sdworkspace/sdbackend/internal/server/cmd/api/merchant_program_plans.go
//
// GTM:
//
//	Layer: 2.4 Merchant / Future Offering Domain
//	Release Class: SPINE
//	Reason:
//	  merchant_program_plans handler surface is release-critical merchant
//	  monetization infrastructure. It governs the create, read, update,
//	  activate/deactivate, soft-delete, and restore lifecycle of the three
//	  canonical program plan codes (standard, premium, enterprise) that
//	  control Future Offering access, Launch Campaign access, merchant
//	  subscriptions, entitlement assignment, fee schedules, billing accounts,
//	  and Merchant Center plan selection.
//
// SPINE Rule:
//
//	Keep compiling.
//	Keep production-ready.
//	Preserve plan-code integrity at the handler boundary.
//	Preserve authorization and audit coverage for privileged plan operations.
//	Preserve soft-delete lifecycle handler semantics.
//	Block deployment if this file breaks build, plan governance, Future Offering
//	entitlement handler readiness, or merchant billing plan integrity.
package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/PiccoloMondoC/sdworkspace/sdbackend/internal/data"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

const (
	merchantProgramPlanEntityType = "merchant_program_plan"

	actionCreateMerchantProgramPlan     = "create_merchant_program_plan"
	actionReadMerchantProgramPlan       = "read_merchant_program_plan"
	actionReadMerchantProgramPlanByCode = "read_merchant_program_plan_by_code"
	actionListMerchantProgramPlans      = "list_merchant_program_plans"
	actionUpdateMerchantProgramPlan     = "update_merchant_program_plan"
	actionActivateMerchantProgramPlan   = "activate_merchant_program_plan"
	actionDeactivateMerchantProgramPlan = "deactivate_merchant_program_plan"
	actionSoftDeleteMerchantProgramPlan = "soft_delete_merchant_program_plan"
	actionRestoreMerchantProgramPlan    = "restore_merchant_program_plan"
)

func merchantProgramPlanHTTPStatus(err error) int {
	if err == nil {
		return http.StatusOK
	}

	msg := strings.ToLower(err.Error())

	switch {
	case strings.Contains(msg, "already exists"):
		return http.StatusConflict
	case strings.Contains(msg, "not found"),
		strings.Contains(msg, "no active merchant program plan"),
		strings.Contains(msg, "no non-deleted merchant program plan"),
		strings.Contains(msg, "no deleted merchant program plan"),
		strings.Contains(msg, "no soft-deleted merchant program plan"):
		return http.StatusNotFound
	case strings.Contains(msg, "invalid"),
		strings.Contains(msg, "required"):
		return http.StatusBadRequest
	default:
		return http.StatusInternalServerError
	}
}

func (app *Application) parseMerchantProgramPlanID(r *http.Request) (uuid.UUID, error) {
	raw := strings.TrimSpace(chi.URLParam(r, "merchantProgramPlanID"))
	if raw == "" {
		return uuid.Nil, errors.New("merchant program plan ID is required")
	}

	id, err := uuid.Parse(raw)
	if err != nil || id == uuid.Nil {
		return uuid.Nil, errors.New("invalid merchant program plan ID")
	}

	return id, nil
}

func (app *Application) auditMerchantProgramPlan(
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

	entityType, err := app.Models.EntityType.GetByName(ctx, merchantProgramPlanEntityType)
	if err != nil || entityType == nil {
		entityTypeID, createErr := app.Models.EntityType.CreateIfNotExists(
			ctx,
			merchantProgramPlanEntityType,
			"Merchant program plan entity",
		)
		if createErr != nil {
			return fmt.Errorf("resolve audit entity type %s: %w", merchantProgramPlanEntityType, createErr)
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
		return fmt.Errorf("insert merchant program plan audit log: %w", err)
	}

	return nil
}

// CreateMerchantProgramPlanHandler creates a merchant program plan.
func (app *Application) CreateMerchantProgramPlanHandler(w http.ResponseWriter, r *http.Request) {
	logger := app.Logger.GetLoggerWithContext(r).WithFunctionName("CreateMerchantProgramPlanHandler")

	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	if !app.HasPermission(ctx, actionCreateMerchantProgramPlan) {
		app.respondWithError(w, errors.New("forbidden: insufficient permissions"), http.StatusForbidden)
		return
	}

	userID := app.getUserIDFromContext(ctx)
	if userID == nil {
		app.respondWithError(w, errors.New("user ID not found in context"), http.StatusUnauthorized)
		return
	}

	var input struct {
		Code        data.MerchantPlanCode `json:"code"`
		Name        string                `json:"name"`
		Description string                `json:"description"`
	}

	if err := app.readJSON(w, r, &input); err != nil {
		app.respondWithError(w, fmt.Errorf("invalid JSON input: %v", err), http.StatusBadRequest)
		return
	}

	input.Code = data.NormalizeMerchantPlanCode(input.Code)
	if !data.IsValidMerchantPlanCode(input.Code) {
		app.respondWithError(w, fmt.Errorf("invalid merchant program plan code: %s", input.Code), http.StatusBadRequest)
		return
	}

	plan := &data.MerchantProgramPlan{
		Code:        input.Code,
		Name:        input.Name,
		Description: input.Description,
	}

	if err := app.Models.MerchantProgramPlan.Insert(ctx, plan); err != nil {
		logger.Error("Create merchant program plan failed", "error", err)
		app.respondWithError(w, err, merchantProgramPlanHTTPStatus(err))
		return
	}

	if err := app.auditMerchantProgramPlan(ctx, userID, actionCreateMerchantProgramPlan, "Create a merchant program plan", plan.ID.String()); err != nil {
		logger.Warn("Audit logging failed", "plan_id", plan.ID, "error", err)
		app.respondWithJSON(w, http.StatusPartialContent, jsonResponse{
			Error:   false,
			Message: "Merchant program plan created, but audit logging failed",
			Data:    plan,
		})
		return
	}

	logger.Info("Merchant program plan created", "plan_id", plan.ID, "code", plan.Code)
	app.respondWithJSON(w, http.StatusCreated, jsonResponse{
		Error:   false,
		Message: "Merchant program plan created successfully",
		Data:    plan,
	})
}

// GetMerchantProgramPlanByIDHandler retrieves a merchant program plan by ID.
func (app *Application) GetMerchantProgramPlanByIDHandler(w http.ResponseWriter, r *http.Request) {
	logger := app.Logger.GetLoggerWithContext(r).WithFunctionName("GetMerchantProgramPlanByIDHandler")

	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	if !app.HasPermission(ctx, actionReadMerchantProgramPlan) {
		app.respondWithError(w, errors.New("forbidden: insufficient permissions"), http.StatusForbidden)
		return
	}

	userID := app.getUserIDFromContext(ctx)
	if userID == nil {
		app.respondWithError(w, errors.New("user ID not found in context"), http.StatusUnauthorized)
		return
	}

	planID, err := app.parseMerchantProgramPlanID(r)
	if err != nil {
		app.respondWithError(w, err, http.StatusBadRequest)
		return
	}

	plan, err := app.Models.MerchantProgramPlan.GetByID(ctx, planID)
	if err != nil {
		logger.Error("Get merchant program plan by ID failed", "plan_id", planID, "error", err)
		app.respondWithError(w, err, merchantProgramPlanHTTPStatus(err))
		return
	}
	if plan == nil {
		app.respondWithError(w, errors.New("merchant program plan not found"), http.StatusNotFound)
		return
	}

	if err := app.auditMerchantProgramPlan(ctx, userID, actionReadMerchantProgramPlan, "Read a merchant program plan", plan.ID.String()); err != nil {
		logger.Warn("Audit logging failed", "plan_id", plan.ID, "error", err)
		app.respondWithJSON(w, http.StatusPartialContent, jsonResponse{
			Error:   false,
			Message: "Merchant program plan retrieved, but audit logging failed",
			Data:    plan,
		})
		return
	}

	app.respondWithJSON(w, http.StatusOK, jsonResponse{
		Error:   false,
		Message: "Merchant program plan retrieved successfully",
		Data:    plan,
	})
}

// GetMerchantProgramPlanByCodeHandler retrieves a merchant program plan by code.
func (app *Application) GetMerchantProgramPlanByCodeHandler(w http.ResponseWriter, r *http.Request) {
	logger := app.Logger.GetLoggerWithContext(r).WithFunctionName("GetMerchantProgramPlanByCodeHandler")

	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	if !app.HasPermission(ctx, actionReadMerchantProgramPlan) {
		app.respondWithError(w, errors.New("forbidden: insufficient permissions"), http.StatusForbidden)
		return
	}

	userID := app.getUserIDFromContext(ctx)
	if userID == nil {
		app.respondWithError(w, errors.New("user ID not found in context"), http.StatusUnauthorized)
		return
	}

	var input struct {
		Code data.MerchantPlanCode `json:"code"`
	}

	if err := app.readJSON(w, r, &input); err != nil {
		app.respondWithError(w, fmt.Errorf("invalid JSON input: %v", err), http.StatusBadRequest)
		return
	}

	input.Code = data.NormalizeMerchantPlanCode(input.Code)
	if !data.IsValidMerchantPlanCode(input.Code) {
		app.respondWithError(w, fmt.Errorf("invalid merchant program plan code: %s", input.Code), http.StatusBadRequest)
		return
	}

	plan, err := app.Models.MerchantProgramPlan.GetByCode(ctx, input.Code)
	if err != nil {
		logger.Error("Get merchant program plan by code failed", "code", input.Code, "error", err)
		app.respondWithError(w, err, merchantProgramPlanHTTPStatus(err))
		return
	}
	if plan == nil {
		app.respondWithError(w, errors.New("merchant program plan not found"), http.StatusNotFound)
		return
	}

	if err := app.auditMerchantProgramPlan(ctx, userID, actionReadMerchantProgramPlanByCode, "Read a merchant program plan by code", plan.ID.String()); err != nil {
		logger.Warn("Audit logging failed", "plan_id", plan.ID, "error", err)
		app.respondWithJSON(w, http.StatusPartialContent, jsonResponse{
			Error:   false,
			Message: "Merchant program plan retrieved, but audit logging failed",
			Data:    plan,
		})
		return
	}

	app.respondWithJSON(w, http.StatusOK, jsonResponse{
		Error:   false,
		Message: "Merchant program plan retrieved successfully",
		Data:    plan,
	})
}

// GetActiveMerchantProgramPlanByCodeHandler retrieves an active merchant program plan by code.
func (app *Application) GetActiveMerchantProgramPlanByCodeHandler(w http.ResponseWriter, r *http.Request) {
	logger := app.Logger.GetLoggerWithContext(r).WithFunctionName("GetActiveMerchantProgramPlanByCodeHandler")

	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	if !app.HasPermission(ctx, actionReadMerchantProgramPlan) {
		app.respondWithError(w, errors.New("forbidden: insufficient permissions"), http.StatusForbidden)
		return
	}

	var input struct {
		Code data.MerchantPlanCode `json:"code"`
	}

	if err := app.readJSON(w, r, &input); err != nil {
		app.respondWithError(w, fmt.Errorf("invalid JSON input: %v", err), http.StatusBadRequest)
		return
	}

	input.Code = data.NormalizeMerchantPlanCode(input.Code)
	if !data.IsValidMerchantPlanCode(input.Code) {
		app.respondWithError(w, fmt.Errorf("invalid merchant program plan code: %s", input.Code), http.StatusBadRequest)
		return
	}

	plan, err := app.Models.MerchantProgramPlan.GetActiveByCode(ctx, input.Code)
	if err != nil {
		logger.Error("Get active merchant program plan by code failed", "code", input.Code, "error", err)
		app.respondWithError(w, err, merchantProgramPlanHTTPStatus(err))
		return
	}
	if plan == nil {
		app.respondWithError(w, errors.New("active merchant program plan not found"), http.StatusNotFound)
		return
	}

	// Active-plan reads are intentionally unaudited: this endpoint supports
	// high-frequency entitlement/readiness resolution and remains permission-controlled.
	app.respondWithJSON(w, http.StatusOK, jsonResponse{
		Error:   false,
		Message: "Active merchant program plan retrieved successfully",
		Data:    plan,
	})

	app.respondWithJSON(w, http.StatusOK, jsonResponse{
		Error:   false,
		Message: "Active merchant program plan retrieved successfully",
		Data:    plan,
	})
}

// GetAllMerchantProgramPlansHandler lists merchant program plans.
func (app *Application) GetAllMerchantProgramPlansHandler(w http.ResponseWriter, r *http.Request) {
	logger := app.Logger.GetLoggerWithContext(r).WithFunctionName("GetAllMerchantProgramPlansHandler")

	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	if !app.HasPermission(ctx, actionListMerchantProgramPlans) {
		app.respondWithError(w, errors.New("forbidden: insufficient permissions"), http.StatusForbidden)
		return
	}

	userID := app.getUserIDFromContext(ctx)
	if userID == nil {
		app.respondWithError(w, errors.New("user ID not found in context"), http.StatusUnauthorized)
		return
	}

	limit := parseIntOrDefault(app.getContextValueAsString(ctx, ctxPaginationLimit), 20)
	offset := parseIntOrDefault(app.getContextValueAsString(ctx, ctxPaginationOffset), 0)
	if limit > 100 {
		limit = 100
	}

	includeDeleted := strings.EqualFold(r.URL.Query().Get("include_deleted"), "true")

	plans, err := app.Models.MerchantProgramPlan.GetAll(ctx, includeDeleted, limit, offset)
	if err != nil {
		logger.Error("Get all merchant program plans failed", "error", err)
		app.respondWithError(w, err, merchantProgramPlanHTTPStatus(err))
		return
	}

	if err := app.auditMerchantProgramPlan(ctx, userID, actionListMerchantProgramPlans, "List merchant program plans", "*"); err != nil {
		logger.Warn("Audit logging failed", "error", err)
		app.respondWithJSON(w, http.StatusPartialContent, jsonResponse{
			Error:   false,
			Message: "Merchant program plans retrieved, but audit logging failed",
			Data:    plans,
		})
		return
	}

	app.respondWithJSON(w, http.StatusOK, jsonResponse{
		Error:   false,
		Message: "Merchant program plans retrieved successfully",
		Data:    plans,
	})
}

// ListActiveMerchantProgramPlansHandler lists active merchant program plans.
func (app *Application) ListActiveMerchantProgramPlansHandler(w http.ResponseWriter, r *http.Request) {
	logger := app.Logger.GetLoggerWithContext(r).WithFunctionName("ListActiveMerchantProgramPlansHandler")

	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	if !app.HasPermission(ctx, actionListMerchantProgramPlans) {
		app.respondWithError(w, errors.New("forbidden: insufficient permissions"), http.StatusForbidden)
		return
	}

	plans, err := app.Models.MerchantProgramPlan.ListActive(ctx)
	if err != nil {
		logger.Error("List active merchant program plans failed", "error", err)
		app.respondWithError(w, err, merchantProgramPlanHTTPStatus(err))
		return
	}

	// Active-plan list reads are intentionally unaudited: this endpoint supports
	// high-frequency entitlement/readiness resolution and remains permission-controlled.
	app.respondWithJSON(w, http.StatusOK, jsonResponse{
		Error:   false,
		Message: "Active merchant program plans retrieved successfully",
		Data:    plans,
	})

	app.respondWithJSON(w, http.StatusOK, jsonResponse{
		Error:   false,
		Message: "Active merchant program plans retrieved successfully",
		Data:    plans,
	})
}

// UpdateMerchantProgramPlanHandler updates merchant program plan display fields.
func (app *Application) UpdateMerchantProgramPlanHandler(w http.ResponseWriter, r *http.Request) {
	logger := app.Logger.GetLoggerWithContext(r).WithFunctionName("UpdateMerchantProgramPlanHandler")

	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	if !app.HasPermission(ctx, actionUpdateMerchantProgramPlan) {
		app.respondWithError(w, errors.New("forbidden: insufficient permissions"), http.StatusForbidden)
		return
	}

	userID := app.getUserIDFromContext(ctx)
	if userID == nil {
		app.respondWithError(w, errors.New("user ID not found in context"), http.StatusUnauthorized)
		return
	}

	planID, err := app.parseMerchantProgramPlanID(r)
	if err != nil {
		app.respondWithError(w, err, http.StatusBadRequest)
		return
	}

	var input struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}

	if err := app.readJSON(w, r, &input); err != nil {
		app.respondWithError(w, fmt.Errorf("invalid JSON input: %v", err), http.StatusBadRequest)
		return
	}

	plan := &data.MerchantProgramPlan{
		ID:          planID,
		Name:        input.Name,
		Description: input.Description,
	}

	if err := app.Models.MerchantProgramPlan.Update(ctx, plan); err != nil {
		logger.Error("Update merchant program plan failed", "plan_id", planID, "error", err)
		app.respondWithError(w, err, merchantProgramPlanHTTPStatus(err))
		return
	}

	if err := app.auditMerchantProgramPlan(ctx, userID, actionUpdateMerchantProgramPlan, "Update a merchant program plan", planID.String()); err != nil {
		logger.Warn("Audit logging failed", "plan_id", planID, "error", err)
		app.respondWithJSON(w, http.StatusPartialContent, jsonResponse{
			Error:   false,
			Message: "Merchant program plan updated, but audit logging failed",
			Data:    plan,
		})
		return
	}

	app.respondWithJSON(w, http.StatusOK, jsonResponse{
		Error:   false,
		Message: "Merchant program plan updated successfully",
		Data:    plan,
	})
}

// ActivateMerchantProgramPlanHandler activates a merchant program plan.
func (app *Application) ActivateMerchantProgramPlanHandler(w http.ResponseWriter, r *http.Request) {
	app.setMerchantProgramPlanActiveState(w, r, true)
}

// DeactivateMerchantProgramPlanHandler deactivates a merchant program plan.
func (app *Application) DeactivateMerchantProgramPlanHandler(w http.ResponseWriter, r *http.Request) {
	app.setMerchantProgramPlanActiveState(w, r, false)
}

func (app *Application) setMerchantProgramPlanActiveState(w http.ResponseWriter, r *http.Request, active bool) {
	functionName := "DeactivateMerchantProgramPlanHandler"
	actionName := actionDeactivateMerchantProgramPlan
	actionDescription := "Deactivate a merchant program plan"
	successMessage := "Merchant program plan deactivated successfully"

	if active {
		functionName = "ActivateMerchantProgramPlanHandler"
		actionName = actionActivateMerchantProgramPlan
		actionDescription = "Activate a merchant program plan"
		successMessage = "Merchant program plan activated successfully"
	}

	logger := app.Logger.GetLoggerWithContext(r).WithFunctionName(functionName)

	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	if !app.HasPermission(ctx, actionName) {
		app.respondWithError(w, errors.New("forbidden: insufficient permissions"), http.StatusForbidden)
		return
	}

	userID := app.getUserIDFromContext(ctx)
	if userID == nil {
		app.respondWithError(w, errors.New("user ID not found in context"), http.StatusUnauthorized)
		return
	}

	planID, err := app.parseMerchantProgramPlanID(r)
	if err != nil {
		app.respondWithError(w, err, http.StatusBadRequest)
		return
	}

	if active {
		err = app.Models.MerchantProgramPlan.Activate(ctx, planID)
	} else {
		err = app.Models.MerchantProgramPlan.Deactivate(ctx, planID)
	}
	if err != nil {
		logger.Error("Set merchant program plan active state failed", "plan_id", planID, "active", active, "error", err)
		app.respondWithError(w, err, merchantProgramPlanHTTPStatus(err))
		return
	}

	if err := app.auditMerchantProgramPlan(ctx, userID, actionName, actionDescription, planID.String()); err != nil {
		logger.Warn("Audit logging failed", "plan_id", planID, "error", err)
		app.respondWithJSON(w, http.StatusPartialContent, jsonResponse{
			Error:   false,
			Message: successMessage + ", but audit logging failed",
			Data:    planID,
		})
		return
	}

	app.respondWithJSON(w, http.StatusOK, jsonResponse{
		Error:   false,
		Message: successMessage,
		Data:    planID,
	})
}

// SoftDeleteMerchantProgramPlanHandler soft-deletes a merchant program plan.
func (app *Application) SoftDeleteMerchantProgramPlanHandler(w http.ResponseWriter, r *http.Request) {
	logger := app.Logger.GetLoggerWithContext(r).WithFunctionName("SoftDeleteMerchantProgramPlanHandler")

	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	if !app.HasPermission(ctx, actionSoftDeleteMerchantProgramPlan) {
		app.respondWithError(w, errors.New("forbidden: insufficient permissions"), http.StatusForbidden)
		return
	}

	userID := app.getUserIDFromContext(ctx)
	if userID == nil {
		app.respondWithError(w, errors.New("user ID not found in context"), http.StatusUnauthorized)
		return
	}

	planID, err := app.parseMerchantProgramPlanID(r)
	if err != nil {
		app.respondWithError(w, err, http.StatusBadRequest)
		return
	}

	if err := app.Models.MerchantProgramPlan.SoftDelete(ctx, planID); err != nil {
		logger.Error("Soft delete merchant program plan failed", "plan_id", planID, "error", err)
		app.respondWithError(w, err, merchantProgramPlanHTTPStatus(err))
		return
	}

	if err := app.auditMerchantProgramPlan(ctx, userID, actionSoftDeleteMerchantProgramPlan, "Soft-delete a merchant program plan", planID.String()); err != nil {
		logger.Warn("Audit logging failed", "plan_id", planID, "error", err)
		app.respondWithJSON(w, http.StatusPartialContent, jsonResponse{
			Error:   false,
			Message: "Merchant program plan soft-deleted, but audit logging failed",
			Data:    planID,
		})
		return
	}

	app.respondWithJSON(w, http.StatusOK, jsonResponse{
		Error:   false,
		Message: "Merchant program plan soft-deleted successfully",
		Data:    planID,
	})
}

// RestoreMerchantProgramPlanHandler restores a soft-deleted merchant program plan.
func (app *Application) RestoreMerchantProgramPlanHandler(w http.ResponseWriter, r *http.Request) {
	logger := app.Logger.GetLoggerWithContext(r).WithFunctionName("RestoreMerchantProgramPlanHandler")

	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	if !app.HasPermission(ctx, actionRestoreMerchantProgramPlan) {
		app.respondWithError(w, errors.New("forbidden: insufficient permissions"), http.StatusForbidden)
		return
	}

	userID := app.getUserIDFromContext(ctx)
	if userID == nil {
		app.respondWithError(w, errors.New("user ID not found in context"), http.StatusUnauthorized)
		return
	}

	planID, err := app.parseMerchantProgramPlanID(r)
	if err != nil {
		app.respondWithError(w, err, http.StatusBadRequest)
		return
	}

	if err := app.Models.MerchantProgramPlan.Restore(ctx, planID); err != nil {
		logger.Error("Restore merchant program plan failed", "plan_id", planID, "error", err)
		app.respondWithError(w, err, merchantProgramPlanHTTPStatus(err))
		return
	}

	if err := app.auditMerchantProgramPlan(ctx, userID, actionRestoreMerchantProgramPlan, "Restore a merchant program plan", planID.String()); err != nil {
		logger.Warn("Audit logging failed", "plan_id", planID, "error", err)
		app.respondWithJSON(w, http.StatusPartialContent, jsonResponse{
			Error:   false,
			Message: "Merchant program plan restored, but audit logging failed",
			Data:    planID,
		})
		return
	}

	app.respondWithJSON(w, http.StatusOK, jsonResponse{
		Error:   false,
		Message: "Merchant program plan restored successfully",
		Data:    planID,
	})
}
