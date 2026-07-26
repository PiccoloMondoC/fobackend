// Package main provides HTTP handlers for merchant program entitlement governance.
//
// sdworkspace/sdbackend/internal/server/cmd/api/merchant_program_entitlements.go
//
// GTM:
//
//	Layer: 2.4 Merchant / Future Offering Domain
//	Release Class: SPINE
//	Reason:
//	  merchant_program_entitlements handler surface is release-critical
//	  merchant capability-gating infrastructure for Future Offering v1. It
//	  governs privileged administrative creation, idempotent ensure, read,
//	  list, check, and hard-delete operations for entitlement rows that connect
//	  merchant program plans to Launch Campaign and Future Offering capability.
//
//	  Future Offering is the Platform's core merchant-side future-commerce object.
//	  A merchant program plan must not be treated as capable of activating
//	  Future Offering workflows unless the plan carries the appropriate
//	  entitlement row.
//
//	  The database owns the compound invariant enforced by
//	  ensure_future_offering_includes_campaign_trigger: inserting
//	  future_offering_access automatically ensures launch_campaign_access for
//	  the same plan. Handler code must document this invariant, but must not
//	  duplicate it with application-side companion inserts.
//
// SPINE Rule:
//
//	Keep compiling.
//	Keep production-ready.
//	Preserve entitlement-code validation at the handler boundary.
//	Preserve privileged authorization for entitlement governance operations.
//	Preserve audit coverage for privileged HTTP entitlement operations.
//	Preserve hard-delete semantics because this table has no deleted_at column.
//	Preserve database ownership of the future_offering_access implies
//	  launch_campaign_access invariant.
//	Block deployment if this file breaks build, entitlement governance,
//	Future Offering capability-gate integrity, route registration, or audit
//	metadata coverage.
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
	merchantProgramEntitlementEntityType = "merchant_program_entitlement"

	actionCreateMerchantProgramEntitlement              = "create_merchant_program_entitlement"
	actionEnsureMerchantProgramEntitlement              = "ensure_merchant_program_entitlement"
	actionReadMerchantProgramEntitlement                = "read_merchant_program_entitlement"
	actionReadMerchantProgramEntitlementByPlanAndCode   = "read_merchant_program_entitlement_by_plan_and_code"
	actionListMerchantProgramEntitlementsByPlan         = "list_merchant_program_entitlements_by_plan"
	actionCheckMerchantProgramEntitlement               = "check_merchant_program_entitlement"
	actionDeleteMerchantProgramEntitlement              = "delete_merchant_program_entitlement"
	actionDeleteMerchantProgramEntitlementByPlanAndCode = "delete_merchant_program_entitlement_by_plan_and_code"

	permissionCreateMerchantProgramEntitlement = "create_merchant_program_entitlement"
	permissionEnsureMerchantProgramEntitlement = "ensure_merchant_program_entitlement"
	permissionReadMerchantProgramEntitlement   = "read_merchant_program_entitlement"
	permissionListMerchantProgramEntitlements  = "list_merchant_program_entitlements"
	permissionDeleteMerchantProgramEntitlement = "delete_merchant_program_entitlement"
)

type merchantProgramEntitlementPlanCodeInput struct {
	PlanID          string                              `json:"plan_id"`
	EntitlementCode data.MerchantProgramEntitlementCode `json:"entitlement_code"`
}

type merchantProgramEntitlementCheckResponse struct {
	PlanID          uuid.UUID                           `json:"plan_id"`
	EntitlementCode data.MerchantProgramEntitlementCode `json:"entitlement_code"`
	HasEntitlement  bool                                `json:"has_entitlement"`
}

func merchantProgramEntitlementHTTPStatus(err error) int {
	if err == nil {
		return http.StatusOK
	}

	msg := strings.ToLower(err.Error())

	switch {
	case strings.Contains(msg, "already exists"):
		return http.StatusConflict
	case strings.Contains(msg, "not found"),
		strings.Contains(msg, "no merchant program entitlement found"):
		return http.StatusNotFound
	case strings.Contains(msg, "references missing merchant program plan"):
		return http.StatusUnprocessableEntity
	case strings.Contains(msg, "invalid"),
		strings.Contains(msg, "required"),
		strings.Contains(msg, "not a recognized value"):
		return http.StatusBadRequest
	default:
		return http.StatusInternalServerError
	}
}

func (app *Application) parseMerchantProgramEntitlementID(r *http.Request) (uuid.UUID, error) {
	raw := strings.TrimSpace(chi.URLParam(r, "merchantProgramEntitlementID"))
	if raw == "" {
		return uuid.Nil, errors.New("merchant program entitlement ID is required")
	}

	id, err := uuid.Parse(raw)
	if err != nil || id == uuid.Nil {
		return uuid.Nil, errors.New("invalid merchant program entitlement ID")
	}

	return id, nil
}

func (app *Application) parseMerchantProgramEntitlementPlanID(r *http.Request) (uuid.UUID, error) {
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

func parseMerchantProgramEntitlementPlanAndCode(planIDRaw string, codeRaw data.MerchantProgramEntitlementCode) (uuid.UUID, data.MerchantProgramEntitlementCode, error) {
	planID, err := uuid.Parse(strings.TrimSpace(planIDRaw))
	if err != nil || planID == uuid.Nil {
		return uuid.Nil, "", errors.New("invalid plan_id")
	}

	code := data.NormalizeMerchantProgramEntitlementCode(codeRaw)
	if !data.IsValidMerchantProgramEntitlementCode(code) {
		return uuid.Nil, "", errors.New("entitlement_code is not a recognized value")
	}

	return planID, code, nil
}

func parseMerchantProgramEntitlementPlanAndCodeInput(input merchantProgramEntitlementPlanCodeInput) (uuid.UUID, data.MerchantProgramEntitlementCode, error) {
	return parseMerchantProgramEntitlementPlanAndCode(input.PlanID, input.EntitlementCode)
}

func parseMerchantProgramEntitlementPlanAndCodeQuery(r *http.Request) (uuid.UUID, data.MerchantProgramEntitlementCode, error) {
	return parseMerchantProgramEntitlementPlanAndCode(
		r.URL.Query().Get("plan_id"),
		data.MerchantProgramEntitlementCode(r.URL.Query().Get("entitlement_code")),
	)
}

func (app *Application) auditMerchantProgramEntitlement(
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

	entityType, err := app.Models.EntityType.GetByName(ctx, merchantProgramEntitlementEntityType)
	if err != nil || entityType == nil {
		entityTypeID, createErr := app.Models.EntityType.CreateIfNotExists(
			ctx,
			merchantProgramEntitlementEntityType,
			"Merchant program entitlement entity",
		)
		if createErr != nil {
			return fmt.Errorf("resolve audit entity type %s: %w", merchantProgramEntitlementEntityType, createErr)
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
		return fmt.Errorf("insert merchant program entitlement audit log: %w", err)
	}

	return nil
}

// CreateMerchantProgramEntitlementHandler creates a merchant program entitlement.
//
// This is a privileged platform-governance operation. Inserting
// future_offering_access may cause the database trigger to also insert
// launch_campaign_access for the same plan.
func (app *Application) CreateMerchantProgramEntitlementHandler(w http.ResponseWriter, r *http.Request) {
	logger := app.Logger.GetLoggerWithContext(r).WithFunctionName("CreateMerchantProgramEntitlementHandler")

	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	if !app.HasPermission(ctx, permissionCreateMerchantProgramEntitlement) {
		app.respondWithError(w, errors.New("forbidden: insufficient permissions"), http.StatusForbidden)
		return
	}

	userID := app.getUserIDFromContext(ctx)
	if userID == nil {
		app.respondWithError(w, errors.New("user ID not found in context"), http.StatusUnauthorized)
		return
	}

	var input merchantProgramEntitlementPlanCodeInput
	if err := app.readJSON(w, r, &input); err != nil {
		app.respondWithError(w, fmt.Errorf("invalid JSON input: %v", err), http.StatusBadRequest)
		return
	}

	planID, code, err := parseMerchantProgramEntitlementPlanAndCodeInput(input)
	if err != nil {
		app.respondWithError(w, err, http.StatusBadRequest)
		return
	}

	entitlement := &data.MerchantProgramEntitlement{
		PlanID:          planID,
		EntitlementCode: code,
	}

	if err := app.Models.MerchantProgramEntitlement.Insert(ctx, entitlement); err != nil {
		logger.Error("Create merchant program entitlement failed",
			"plan_id", planID,
			"entitlement_code", code,
			"error", err,
		)
		app.respondWithError(w, err, merchantProgramEntitlementHTTPStatus(err))
		return
	}

	if err := app.auditMerchantProgramEntitlement(ctx, userID, actionCreateMerchantProgramEntitlement, "Create a merchant program entitlement", entitlement.ID.String()); err != nil {
		logger.Warn("Audit logging failed", "entitlement_id", entitlement.ID, "error", err)
		app.respondWithJSON(w, http.StatusPartialContent, jsonResponse{
			Error:   false,
			Message: "Merchant program entitlement created, but audit logging failed",
			Data:    entitlement,
		})
		return
	}

	logger.Info("Merchant program entitlement created",
		"entitlement_id", entitlement.ID,
		"plan_id", entitlement.PlanID,
		"entitlement_code", entitlement.EntitlementCode,
	)

	app.respondWithJSON(w, http.StatusCreated, jsonResponse{
		Error:   false,
		Message: "Merchant program entitlement created successfully",
		Data:    entitlement,
	})
}

// EnsureMerchantProgramEntitlementHandler idempotently creates or retrieves a
// merchant program entitlement.
//
// This is intended for privileged admin/startup-style governance paths. It is
// not a merchant self-service operation.
func (app *Application) EnsureMerchantProgramEntitlementHandler(w http.ResponseWriter, r *http.Request) {
	logger := app.Logger.GetLoggerWithContext(r).WithFunctionName("EnsureMerchantProgramEntitlementHandler")

	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	if !app.HasPermission(ctx, permissionEnsureMerchantProgramEntitlement) {
		app.respondWithError(w, errors.New("forbidden: insufficient permissions"), http.StatusForbidden)
		return
	}

	userID := app.getUserIDFromContext(ctx)
	if userID == nil {
		app.respondWithError(w, errors.New("user ID not found in context"), http.StatusUnauthorized)
		return
	}

	var input merchantProgramEntitlementPlanCodeInput
	if err := app.readJSON(w, r, &input); err != nil {
		app.respondWithError(w, fmt.Errorf("invalid JSON input: %v", err), http.StatusBadRequest)
		return
	}

	planID, code, err := parseMerchantProgramEntitlementPlanAndCodeInput(input)
	if err != nil {
		app.respondWithError(w, err, http.StatusBadRequest)
		return
	}

	entitlement, err := app.Models.MerchantProgramEntitlement.Ensure(ctx, planID, code)
	if err != nil {
		logger.Error("Ensure merchant program entitlement failed",
			"plan_id", planID,
			"entitlement_code", code,
			"error", err,
		)
		app.respondWithError(w, err, merchantProgramEntitlementHTTPStatus(err))
		return
	}

	if err := app.auditMerchantProgramEntitlement(ctx, userID, actionEnsureMerchantProgramEntitlement, "Ensure a merchant program entitlement", entitlement.ID.String()); err != nil {
		logger.Warn("Audit logging failed", "entitlement_id", entitlement.ID, "error", err)
		app.respondWithJSON(w, http.StatusPartialContent, jsonResponse{
			Error:   false,
			Message: "Merchant program entitlement ensured, but audit logging failed",
			Data:    entitlement,
		})
		return
	}

	logger.Info("Merchant program entitlement ensured",
		"entitlement_id", entitlement.ID,
		"plan_id", entitlement.PlanID,
		"entitlement_code", entitlement.EntitlementCode,
	)

	app.respondWithJSON(w, http.StatusOK, jsonResponse{
		Error:   false,
		Message: "Merchant program entitlement ensured successfully",
		Data:    entitlement,
	})
}

// GetMerchantProgramEntitlementByIDHandler retrieves a merchant program
// entitlement by ID.
func (app *Application) GetMerchantProgramEntitlementByIDHandler(w http.ResponseWriter, r *http.Request) {
	logger := app.Logger.GetLoggerWithContext(r).WithFunctionName("GetMerchantProgramEntitlementByIDHandler")

	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	if !app.HasPermission(ctx, permissionReadMerchantProgramEntitlement) {
		app.respondWithError(w, errors.New("forbidden: insufficient permissions"), http.StatusForbidden)
		return
	}

	userID := app.getUserIDFromContext(ctx)
	if userID == nil {
		app.respondWithError(w, errors.New("user ID not found in context"), http.StatusUnauthorized)
		return
	}

	entitlementID, err := app.parseMerchantProgramEntitlementID(r)
	if err != nil {
		app.respondWithError(w, err, http.StatusBadRequest)
		return
	}

	entitlement, err := app.Models.MerchantProgramEntitlement.GetByID(ctx, entitlementID)
	if err != nil {
		logger.Error("Get merchant program entitlement by ID failed",
			"entitlement_id", entitlementID,
			"error", err,
		)
		app.respondWithError(w, err, merchantProgramEntitlementHTTPStatus(err))
		return
	}
	if entitlement == nil {
		app.respondWithError(w, errors.New("merchant program entitlement not found"), http.StatusNotFound)
		return
	}

	if err := app.auditMerchantProgramEntitlement(ctx, userID, actionReadMerchantProgramEntitlement, "Read a merchant program entitlement", entitlement.ID.String()); err != nil {
		logger.Warn("Audit logging failed", "entitlement_id", entitlement.ID, "error", err)
		app.respondWithJSON(w, http.StatusPartialContent, jsonResponse{
			Error:   false,
			Message: "Merchant program entitlement retrieved, but audit logging failed",
			Data:    entitlement,
		})
		return
	}

	app.respondWithJSON(w, http.StatusOK, jsonResponse{
		Error:   false,
		Message: "Merchant program entitlement retrieved successfully",
		Data:    entitlement,
	})
}

// GetMerchantProgramEntitlementByPlanAndCodeHandler retrieves a merchant
// program entitlement by plan ID and entitlement code.
func (app *Application) GetMerchantProgramEntitlementByPlanAndCodeHandler(w http.ResponseWriter, r *http.Request) {
	logger := app.Logger.GetLoggerWithContext(r).WithFunctionName("GetMerchantProgramEntitlementByPlanAndCodeHandler")

	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	if !app.HasPermission(ctx, permissionReadMerchantProgramEntitlement) {
		app.respondWithError(w, errors.New("forbidden: insufficient permissions"), http.StatusForbidden)
		return
	}

	userID := app.getUserIDFromContext(ctx)
	if userID == nil {
		app.respondWithError(w, errors.New("user ID not found in context"), http.StatusUnauthorized)
		return
	}

	var input merchantProgramEntitlementPlanCodeInput
	if err := app.readJSON(w, r, &input); err != nil {
		app.respondWithError(w, fmt.Errorf("invalid JSON input: %v", err), http.StatusBadRequest)
		return
	}

	planID, code, err := parseMerchantProgramEntitlementPlanAndCodeInput(input)
	if err != nil {
		app.respondWithError(w, err, http.StatusBadRequest)
		return
	}

	entitlement, err := app.Models.MerchantProgramEntitlement.GetByPlanAndCode(ctx, planID, code)
	if err != nil {
		logger.Error("Get merchant program entitlement by plan and code failed",
			"plan_id", planID,
			"entitlement_code", code,
			"error", err,
		)
		app.respondWithError(w, err, merchantProgramEntitlementHTTPStatus(err))
		return
	}
	if entitlement == nil {
		app.respondWithError(w, errors.New("merchant program entitlement not found"), http.StatusNotFound)
		return
	}

	if err := app.auditMerchantProgramEntitlement(ctx, userID, actionReadMerchantProgramEntitlementByPlanAndCode, "Read a merchant program entitlement by plan and code", entitlement.ID.String()); err != nil {
		logger.Warn("Audit logging failed", "entitlement_id", entitlement.ID, "error", err)
		app.respondWithJSON(w, http.StatusPartialContent, jsonResponse{
			Error:   false,
			Message: "Merchant program entitlement retrieved, but audit logging failed",
			Data:    entitlement,
		})
		return
	}

	app.respondWithJSON(w, http.StatusOK, jsonResponse{
		Error:   false,
		Message: "Merchant program entitlement retrieved successfully",
		Data:    entitlement,
	})
}

// ListMerchantProgramEntitlementsByPlanIDHandler lists all merchant program
// entitlements for a merchant program plan.
func (app *Application) ListMerchantProgramEntitlementsByPlanIDHandler(w http.ResponseWriter, r *http.Request) {
	logger := app.Logger.GetLoggerWithContext(r).WithFunctionName("ListMerchantProgramEntitlementsByPlanIDHandler")

	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	if !app.HasPermission(ctx, permissionListMerchantProgramEntitlements) {
		app.respondWithError(w, errors.New("forbidden: insufficient permissions"), http.StatusForbidden)
		return
	}

	userID := app.getUserIDFromContext(ctx)
	if userID == nil {
		app.respondWithError(w, errors.New("user ID not found in context"), http.StatusUnauthorized)
		return
	}

	planID, err := app.parseMerchantProgramEntitlementPlanID(r)
	if err != nil {
		app.respondWithError(w, err, http.StatusBadRequest)
		return
	}

	entitlements, err := app.Models.MerchantProgramEntitlement.ListByPlanID(ctx, planID)
	if err != nil {
		logger.Error("List merchant program entitlements by plan ID failed", "plan_id", planID, "error", err)
		app.respondWithError(w, err, merchantProgramEntitlementHTTPStatus(err))
		return
	}
	if entitlements == nil {
		entitlements = []*data.MerchantProgramEntitlement{}
	}

	if err := app.auditMerchantProgramEntitlement(ctx, userID, actionListMerchantProgramEntitlementsByPlan, "List merchant program entitlements by plan", planID.String()); err != nil {
		logger.Warn("Audit logging failed", "plan_id", planID, "error", err)
		app.respondWithJSON(w, http.StatusPartialContent, jsonResponse{
			Error:   false,
			Message: "Merchant program entitlements retrieved, but audit logging failed",
			Data:    entitlements,
		})
		return
	}

	app.respondWithJSON(w, http.StatusOK, jsonResponse{
		Error:   false,
		Message: "Merchant program entitlements retrieved successfully",
		Data:    entitlements,
	})
}

// PlanHasMerchantProgramEntitlementHandler checks whether a plan has a
// specific merchant program entitlement.
//
// This HTTP handler is a privileged governance/read surface and is audited.
// Hot-path capability checks should be performed inside service logic rather
// than by repeatedly calling this HTTP endpoint.
func (app *Application) PlanHasMerchantProgramEntitlementHandler(w http.ResponseWriter, r *http.Request) {
	logger := app.Logger.GetLoggerWithContext(r).WithFunctionName("PlanHasMerchantProgramEntitlementHandler")

	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	if !app.HasPermission(ctx, permissionReadMerchantProgramEntitlement) {
		app.respondWithError(w, errors.New("forbidden: insufficient permissions"), http.StatusForbidden)
		return
	}

	userID := app.getUserIDFromContext(ctx)
	if userID == nil {
		app.respondWithError(w, errors.New("user ID not found in context"), http.StatusUnauthorized)
		return
	}

	var input merchantProgramEntitlementPlanCodeInput
	if err := app.readJSON(w, r, &input); err != nil {
		app.respondWithError(w, fmt.Errorf("invalid JSON input: %v", err), http.StatusBadRequest)
		return
	}

	planID, code, err := parseMerchantProgramEntitlementPlanAndCodeInput(input)
	if err != nil {
		app.respondWithError(w, err, http.StatusBadRequest)
		return
	}

	hasEntitlement, err := app.Models.MerchantProgramEntitlement.PlanHasEntitlement(ctx, planID, code)
	if err != nil {
		logger.Error("Check merchant program entitlement failed",
			"plan_id", planID,
			"entitlement_code", code,
			"error", err,
		)
		app.respondWithError(w, err, merchantProgramEntitlementHTTPStatus(err))
		return
	}

	result := merchantProgramEntitlementCheckResponse{
		PlanID:          planID,
		EntitlementCode: code,
		HasEntitlement:  hasEntitlement,
	}

	entityID := fmt.Sprintf("%s/%s", planID.String(), code)
	if err := app.auditMerchantProgramEntitlement(ctx, userID, actionCheckMerchantProgramEntitlement, "Check whether a merchant program plan has an entitlement", entityID); err != nil {
		logger.Warn("Audit logging failed", "plan_id", planID, "entitlement_code", code, "error", err)
		app.respondWithJSON(w, http.StatusPartialContent, jsonResponse{
			Error:   false,
			Message: "Merchant program entitlement checked, but audit logging failed",
			Data:    result,
		})
		return
	}

	app.respondWithJSON(w, http.StatusOK, jsonResponse{
		Error:   false,
		Message: "Merchant program entitlement checked successfully",
		Data:    result,
	})
}

// DeleteMerchantProgramEntitlementHandler hard-deletes a merchant program
// entitlement by ID.
//
// The table has no deleted_at column. This is a true hard delete.
func (app *Application) DeleteMerchantProgramEntitlementHandler(w http.ResponseWriter, r *http.Request) {
	logger := app.Logger.GetLoggerWithContext(r).WithFunctionName("DeleteMerchantProgramEntitlementHandler")

	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	if !app.HasPermission(ctx, permissionDeleteMerchantProgramEntitlement) {
		app.respondWithError(w, errors.New("forbidden: insufficient permissions"), http.StatusForbidden)
		return
	}

	userID := app.getUserIDFromContext(ctx)
	if userID == nil {
		app.respondWithError(w, errors.New("user ID not found in context"), http.StatusUnauthorized)
		return
	}

	entitlementID, err := app.parseMerchantProgramEntitlementID(r)
	if err != nil {
		app.respondWithError(w, err, http.StatusBadRequest)
		return
	}

	if err := app.Models.MerchantProgramEntitlement.Delete(ctx, entitlementID); err != nil {
		logger.Error("Delete merchant program entitlement failed",
			"entitlement_id", entitlementID,
			"error", err,
		)
		app.respondWithError(w, err, merchantProgramEntitlementHTTPStatus(err))
		return
	}

	if err := app.auditMerchantProgramEntitlement(ctx, userID, actionDeleteMerchantProgramEntitlement, "Delete a merchant program entitlement", entitlementID.String()); err != nil {
		logger.Warn("Audit logging failed", "entitlement_id", entitlementID, "error", err)
		app.respondWithJSON(w, http.StatusPartialContent, jsonResponse{
			Error:   false,
			Message: "Merchant program entitlement deleted, but audit logging failed",
			Data:    entitlementID,
		})
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// DeleteMerchantProgramEntitlementByPlanAndCodeHandler hard-deletes a merchant
// program entitlement by plan ID and entitlement code.
//
// The table has no deleted_at column. This is a true hard delete. Removing
// future_offering_access does not automatically remove launch_campaign_access;
// revocation policy belongs in service orchestration.
func (app *Application) DeleteMerchantProgramEntitlementByPlanAndCodeHandler(w http.ResponseWriter, r *http.Request) {
	logger := app.Logger.GetLoggerWithContext(r).WithFunctionName("DeleteMerchantProgramEntitlementByPlanAndCodeHandler")

	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	if !app.HasPermission(ctx, permissionDeleteMerchantProgramEntitlement) {
		app.respondWithError(w, errors.New("forbidden: insufficient permissions"), http.StatusForbidden)
		return
	}

	userID := app.getUserIDFromContext(ctx)
	if userID == nil {
		app.respondWithError(w, errors.New("user ID not found in context"), http.StatusUnauthorized)
		return
	}

	var input merchantProgramEntitlementPlanCodeInput
	if err := app.readJSON(w, r, &input); err != nil {
		app.respondWithError(w, fmt.Errorf("invalid JSON input: %v", err), http.StatusBadRequest)
		return
	}

	planID, code, err := parseMerchantProgramEntitlementPlanAndCodeInput(input)
	if err != nil {
		app.respondWithError(w, err, http.StatusBadRequest)
		return
	}

	if err := app.Models.MerchantProgramEntitlement.DeleteByPlanAndCode(ctx, planID, code); err != nil {
		logger.Error("Delete merchant program entitlement by plan and code failed",
			"plan_id", planID,
			"entitlement_code", code,
			"error", err,
		)
		app.respondWithError(w, err, merchantProgramEntitlementHTTPStatus(err))
		return
	}

	entityID := fmt.Sprintf("%s/%s", planID.String(), code)
	if err := app.auditMerchantProgramEntitlement(ctx, userID, actionDeleteMerchantProgramEntitlementByPlanAndCode, "Delete a merchant program entitlement by plan and code", entityID); err != nil {
		logger.Warn("Audit logging failed", "plan_id", planID, "entitlement_code", code, "error", err)
		app.respondWithJSON(w, http.StatusPartialContent, jsonResponse{
			Error:   false,
			Message: "Merchant program entitlement deleted, but audit logging failed",
			Data:    entityID,
		})
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
