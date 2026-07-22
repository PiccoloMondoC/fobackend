// Package main provides HTTP handlers for the Platform API.
//
// sdworkspace/sdbackend/internal/server/cmd/api/merchant_applications.go
//
// GTM:
//
//	Layer: 2.4 Merchant / Affiliate Domain
//	Release Class: DEFERRED
//	Reason:
//	  Merchant applications and merchant application statuses are valid
//	  self-service merchant onboarding infrastructure, but they are not required
//	  for the initial Platform release spine. The v1 spine requires
//	  merchant identity, merchant type classification, affiliate program catalog,
//	  and merchant-affiliate relationships before expanding into application
//	  workflow management.
//
// DEFERRED Rule:
//
//	Keep compiling.
//	Keep safe.
//	Keep production-ready.
//	Preserve authorization enforcement.
//	Preserve trusted-context identifier extraction.
//	Preserve DB-owned lifecycle timestamps.
//	Preserve status workflow semantics.
//	Preserve application soft-delete semantics.
//	Preserve status deactivation semantics.
//	Preserve bounded filtered pagination.
//	Preserve best-effort audit logging.
//	Do not add new features.
//	Do not route into v1 UI/API expansion.
//	Do not block deployment on this file unless it breaks the build.
package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/PiccoloMondoC/sdworkspace/sdbackend/internal/data"

	"github.com/google/uuid"
)

const (
	merchantApplicationEntityTypeName        = "merchant_application"
	merchantApplicationEntityTypeDescription = "Merchant application entity"

	merchantApplicationStatusEntityTypeName        = "merchant_application_status"
	merchantApplicationStatusEntityTypeDescription = "Merchant application status entity"

	createMerchantApplicationAction                   = "create_merchant_application"
	readMerchantApplicationAction                     = "read_merchant_application"
	readMerchantApplicationByMerchantAction           = "read_merchant_application_by_merchant"
	listMerchantApplicationsAction                    = "list_merchant_applications"
	adminListMerchantApplicationsAction               = "admin_list_merchant_applications"
	listMerchantApplicationsByStatusAction            = "list_merchant_applications_by_status"
	listMerchantApplicationsByAffiliateProgramAction  = "list_merchant_applications_by_affiliate_program"
	updateMerchantApplicationAction                   = "update_merchant_application"
	softDeleteMerchantApplicationAction               = "soft_delete_merchant_application"
	createMerchantApplicationStatusAction             = "create_merchant_application_status"
	readMerchantApplicationStatusAction               = "read_merchant_application_status"
	listMerchantApplicationStatusesAction             = "list_merchant_application_statuses"
	updateMerchantApplicationStatusAction             = "update_merchant_application_status"
	softDeleteMerchantApplicationStatusAction         = "soft_delete_merchant_application_status"
	defaultMerchantApplicationLimit                   = 20
	maxMerchantApplicationLimit                       = 100
)

type createMerchantApplicationInput struct {
	StatusID *uuid.UUID `json:"status_id,omitempty"`
}

type updateMerchantApplicationInput struct {
	ApplicationID uuid.UUID `json:"application_id"`
	StatusID      uuid.UUID `json:"status_id"`
}

type createMerchantApplicationStatusInput struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	IsActive    *bool  `json:"is_active,omitempty"`
}

type getMerchantApplicationStatusByNameInput struct {
	Name string `json:"name"`
}

type updateMerchantApplicationStatusInput struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	IsActive    bool   `json:"is_active"`
}

// CreateMerchantApplicationHandler creates a merchant application using the
// merchant and affiliate-program identifiers installed in trusted context.
//
// When status_id is omitted, uuid.Nil is passed to the data layer so the INSERT
// can omit status_id and allow the database pending-status default to apply.
func (app *Application) CreateMerchantApplicationHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	logger := app.Logger.
		GetLoggerWithContext(r).
		WithFunctionName("CreateMerchantApplicationHandler")

	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	if !app.HasPermission(ctx, createMerchantApplicationAction) {
		app.respondWithError(
			w,
			errors.New("forbidden: insufficient permissions"),
			http.StatusForbidden,
		)
		return
	}

	userID := app.getUserIDFromContext(ctx)
	if userID == nil {
		app.respondWithError(
			w,
			errors.New("user ID not found in context"),
			http.StatusUnauthorized,
		)
		return
	}

	merchantID := app.getMerchantIDFromContext(ctx)
	if merchantID == nil || *merchantID == uuid.Nil {
		app.respondWithError(
			w,
			errors.New("merchant ID not found in context"),
			http.StatusBadRequest,
		)
		return
	}

	affiliateProgramID := app.getAffiliateProgramIDFromContext(ctx)
	if affiliateProgramID == nil || *affiliateProgramID == uuid.Nil {
		app.respondWithError(
			w,
			errors.New("affiliate program ID not found in context"),
			http.StatusBadRequest,
		)
		return
	}

	var input createMerchantApplicationInput
	if err := app.readJSON(w, r, &input); err != nil {
		app.respondWithError(
			w,
			fmt.Errorf("invalid JSON input: %w", err),
			http.StatusBadRequest,
		)
		return
	}

	statusID := uuid.Nil
	if input.StatusID != nil {
		statusID = *input.StatusID
	}

	application := &data.MerchantApplication{
		ID:                 uuid.New(),
		MerchantID:         *merchantID,
		AffiliateProgramID: *affiliateProgramID,
		StatusID:           statusID,
	}

	if err := app.Models.MerchantApplication.Insert(ctx, application); err != nil {
		logger.Error(
			"Create merchant application failed",
			"merchant_id", *merchantID,
			"affiliate_program_id", *affiliateProgramID,
			"error", err,
		)
		app.respondWithError(
			w,
			fmt.Errorf("failed to create merchant application: %w", err),
			http.StatusInternalServerError,
		)
		return
	}

	app.insertMerchantApplicationAudit(
		ctx,
		userID,
		createMerchantApplicationAction,
		"Create a merchant application",
		merchantApplicationEntityTypeName,
		merchantApplicationEntityTypeDescription,
		application.ID.String(),
	)

	logger.Info(
		"Merchant application created",
		"application_id", application.ID,
		"merchant_id", application.MerchantID,
		"affiliate_program_id", application.AffiliateProgramID,
		"user_id", userID,
	)

	app.respondWithJSON(w, http.StatusCreated, jsonResponse{
		Error:   false,
		Message: "Merchant application created successfully",
		Data:    application,
	})
}

// GetMerchantApplicationByIDHandler retrieves one active merchant application
// using the trusted application ID installed in request context.
func (app *Application) GetMerchantApplicationByIDHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	logger := app.Logger.
		GetLoggerWithContext(r).
		WithFunctionName("GetMerchantApplicationByIDHandler")

	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	if !app.HasPermission(ctx, readMerchantApplicationAction) {
		app.respondWithError(
			w,
			errors.New("forbidden: insufficient permissions"),
			http.StatusForbidden,
		)
		return
	}

	userID := app.getUserIDFromContext(ctx)
	if userID == nil {
		app.respondWithError(
			w,
			errors.New("user ID not found in context"),
			http.StatusUnauthorized,
		)
		return
	}

	applicationID := app.getMerchantApplicationIDFromContext(ctx)
	if applicationID == nil || *applicationID == uuid.Nil {
		app.respondWithError(
			w,
			errors.New("merchant application ID not found in context"),
			http.StatusBadRequest,
		)
		return
	}

	application, err := app.Models.MerchantApplication.GetByID(
		ctx,
		*applicationID,
	)
	if err != nil {
		if errors.Is(err, data.ErrMerchantApplicationNotFound) {
			app.respondWithError(
				w,
				errors.New("merchant application not found"),
				http.StatusNotFound,
			)
			return
		}

		logger.Error(
			"Retrieve merchant application failed",
			"application_id", *applicationID,
			"error", err,
		)
		app.respondWithError(
			w,
			fmt.Errorf("failed to retrieve merchant application: %w", err),
			http.StatusInternalServerError,
		)
		return
	}

	app.insertMerchantApplicationAudit(
		ctx,
		userID,
		readMerchantApplicationAction,
		"Read merchant application by ID",
		merchantApplicationEntityTypeName,
		merchantApplicationEntityTypeDescription,
		application.ID.String(),
	)

	logger.Info(
		"Merchant application retrieved",
		"application_id", application.ID,
		"user_id", userID,
	)

	app.respondWithJSON(w, http.StatusOK, jsonResponse{
		Error:   false,
		Message: "Merchant application retrieved successfully",
		Data:    application,
	})
}

// GetMerchantApplicationByMerchantIDHandler retrieves all active applications
// belonging to the trusted merchant context.
//
// The current data-layer method is intentionally unpaginated.
func (app *Application) GetMerchantApplicationByMerchantIDHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	logger := app.Logger.
		GetLoggerWithContext(r).
		WithFunctionName("GetMerchantApplicationByMerchantIDHandler")

	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	if !app.HasPermission(ctx, readMerchantApplicationAction) {
		app.respondWithError(
			w,
			errors.New("forbidden: insufficient permissions"),
			http.StatusForbidden,
		)
		return
	}

	userID := app.getUserIDFromContext(ctx)
	if userID == nil {
		app.respondWithError(
			w,
			errors.New("user ID not found in context"),
			http.StatusUnauthorized,
		)
		return
	}

	merchantID := app.getMerchantIDFromContext(ctx)
	if merchantID == nil || *merchantID == uuid.Nil {
		app.respondWithError(
			w,
			errors.New("merchant ID not found in context"),
			http.StatusBadRequest,
		)
		return
	}

	applications, err := app.Models.MerchantApplication.GetByMerchantID(
		ctx,
		*merchantID,
	)
	if err != nil {
		logger.Error(
			"Retrieve merchant applications by merchant failed",
			"merchant_id", *merchantID,
			"error", err,
		)
		app.respondWithError(
			w,
			fmt.Errorf("failed to retrieve merchant applications: %w", err),
			http.StatusInternalServerError,
		)
		return
	}

	app.insertMerchantApplicationAudit(
		ctx,
		userID,
		readMerchantApplicationByMerchantAction,
		"Read merchant applications by merchant",
		merchantApplicationEntityTypeName,
		merchantApplicationEntityTypeDescription,
		merchantID.String(),
	)

	logger.Info(
		"Merchant applications retrieved by merchant",
		"merchant_id", *merchantID,
		"count", len(applications),
		"user_id", userID,
	)

	app.respondWithJSON(w, http.StatusOK, jsonResponse{
		Error:   false,
		Message: "Merchant applications retrieved successfully",
		Data:    applications,
	})
}

// GetMerchantApplicationsByStatusIDHandler retrieves a bounded page of active
// merchant applications for the trusted status ID.
func (app *Application) GetMerchantApplicationsByStatusIDHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	logger := app.Logger.
		GetLoggerWithContext(r).
		WithFunctionName("GetMerchantApplicationsByStatusIDHandler")

	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	if !app.HasPermission(ctx, listMerchantApplicationsAction) {
		app.respondWithError(
			w,
			errors.New("forbidden: insufficient permissions"),
			http.StatusForbidden,
		)
		return
	}

	userID := app.getUserIDFromContext(ctx)
	if userID == nil {
		app.respondWithError(
			w,
			errors.New("user ID not found in context"),
			http.StatusUnauthorized,
		)
		return
	}

	statusID := app.getStatusIDFromContext(ctx)
	if statusID == nil || *statusID == uuid.Nil {
		app.respondWithError(
			w,
			errors.New("status ID not found in context"),
			http.StatusBadRequest,
		)
		return
	}

	limit, offset, err := merchantApplicationPagination(r)
	if err != nil {
		app.respondWithError(w, err, http.StatusBadRequest)
		return
	}

	applications, err := app.Models.MerchantApplication.GetByStatusID(
		ctx,
		*statusID,
		limit,
		offset,
	)
	if err != nil {
		logger.Error(
			"Retrieve merchant applications by status failed",
			"status_id", *statusID,
			"error", err,
		)
		app.respondWithError(
			w,
			fmt.Errorf("failed to retrieve merchant applications: %w", err),
			http.StatusInternalServerError,
		)
		return
	}

	app.insertMerchantApplicationAudit(
		ctx,
		userID,
		listMerchantApplicationsByStatusAction,
		"List merchant applications by status",
		merchantApplicationEntityTypeName,
		merchantApplicationEntityTypeDescription,
		statusID.String(),
	)

	logger.Info(
		"Merchant applications retrieved by status",
		"status_id", *statusID,
		"count", len(applications),
		"limit", limit,
		"offset", offset,
		"user_id", userID,
	)

	app.respondWithJSON(w, http.StatusOK, jsonResponse{
		Error:   false,
		Message: "Merchant applications retrieved successfully",
		Data:    applications,
	})
}

// GetMerchantApplicationsByAffiliateProgramIDHandler retrieves a bounded page
// of active applications for the trusted affiliate-program context.
//
// An optional status_id query value narrows the result. uuid.Nil means no status
// filter and matches the current data-layer contract.
func (app *Application) GetMerchantApplicationsByAffiliateProgramIDHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	logger := app.Logger.
		GetLoggerWithContext(r).
		WithFunctionName(
			"GetMerchantApplicationsByAffiliateProgramIDHandler",
		)

	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	if !app.HasPermission(ctx, listMerchantApplicationsAction) {
		app.respondWithError(
			w,
			errors.New("forbidden: insufficient permissions"),
			http.StatusForbidden,
		)
		return
	}

	userID := app.getUserIDFromContext(ctx)
	if userID == nil {
		app.respondWithError(
			w,
			errors.New("user ID not found in context"),
			http.StatusUnauthorized,
		)
		return
	}

	affiliateProgramID := app.getAffiliateProgramIDFromContext(ctx)
	if affiliateProgramID == nil || *affiliateProgramID == uuid.Nil {
		app.respondWithError(
			w,
			errors.New("affiliate program ID not found in context"),
			http.StatusBadRequest,
		)
		return
	}

	limit, offset, err := merchantApplicationPagination(r)
	if err != nil {
		app.respondWithError(w, err, http.StatusBadRequest)
		return
	}

	statusID, err := parseOptionalMerchantApplicationUUID(
		r.URL.Query().Get("status_id"),
		"status_id",
	)
	if err != nil {
		app.respondWithError(w, err, http.StatusBadRequest)
		return
	}

	applications, err := app.Models.MerchantApplication.
		GetByAffiliateProgramID(
			ctx,
			*affiliateProgramID,
			statusID,
			limit,
			offset,
		)
	if err != nil {
		logger.Error(
			"Retrieve merchant applications by affiliate program failed",
			"affiliate_program_id", *affiliateProgramID,
			"status_id", statusID,
			"error", err,
		)
		app.respondWithError(
			w,
			fmt.Errorf("failed to retrieve merchant applications: %w", err),
			http.StatusInternalServerError,
		)
		return
	}

	app.insertMerchantApplicationAudit(
		ctx,
		userID,
		listMerchantApplicationsByAffiliateProgramAction,
		"List merchant applications by affiliate program",
		merchantApplicationEntityTypeName,
		merchantApplicationEntityTypeDescription,
		affiliateProgramID.String(),
	)

	logger.Info(
		"Merchant applications retrieved by affiliate program",
		"affiliate_program_id", *affiliateProgramID,
		"status_id", statusID,
		"count", len(applications),
		"limit", limit,
		"offset", offset,
		"user_id", userID,
	)

	app.respondWithJSON(w, http.StatusOK, jsonResponse{
		Error:   false,
		Message: "Merchant applications retrieved successfully",
		Data:    applications,
	})
}

// GetAllMerchantApplicationsHandler retrieves every active merchant
// application without merchant, affiliate-program, or status scoping.
//
// This is an administrative operation and therefore uses a distinct permission
// instead of silently duplicating an affiliate-program-scoped handler.
func (app *Application) GetAllMerchantApplicationsHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	logger := app.Logger.
		GetLoggerWithContext(r).
		WithFunctionName("GetAllMerchantApplicationsHandler")

	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	if !app.HasPermission(ctx, adminListMerchantApplicationsAction) {
		app.respondWithError(
			w,
			errors.New("forbidden: insufficient permissions"),
			http.StatusForbidden,
		)
		return
	}

	userID := app.getUserIDFromContext(ctx)
	if userID == nil {
		app.respondWithError(
			w,
			errors.New("user ID not found in context"),
			http.StatusUnauthorized,
		)
		return
	}

	applications, err := app.Models.MerchantApplication.GetAll(ctx)
	if err != nil {
		logger.Error(
			"Retrieve all merchant applications failed",
			"error", err,
		)
		app.respondWithError(
			w,
			fmt.Errorf("failed to retrieve merchant applications: %w", err),
			http.StatusInternalServerError,
		)
		return
	}

	app.insertMerchantApplicationAudit(
		ctx,
		userID,
		adminListMerchantApplicationsAction,
		"Administratively list all merchant applications",
		merchantApplicationEntityTypeName,
		merchantApplicationEntityTypeDescription,
		"",
	)

	logger.Info(
		"All merchant applications retrieved",
		"count", len(applications),
		"user_id", userID,
	)

	app.respondWithJSON(w, http.StatusOK, jsonResponse{
		Error:   false,
		Message: "Merchant applications retrieved successfully",
		Data:    applications,
	})
}

// UpdateMerchantApplicationHandler fully replaces the mutable fields of one
// active merchant application.
//
// Merchant and affiliate-program IDs remain trusted-context values. status_id
// is required because the data-layer Update contract has no default fallback.
func (app *Application) UpdateMerchantApplicationHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	logger := app.Logger.
		GetLoggerWithContext(r).
		WithFunctionName("UpdateMerchantApplicationHandler")

	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	if !app.HasPermission(ctx, updateMerchantApplicationAction) {
		app.respondWithError(
			w,
			errors.New("forbidden: insufficient permissions"),
			http.StatusForbidden,
		)
		return
	}

	userID := app.getUserIDFromContext(ctx)
	if userID == nil {
		app.respondWithError(
			w,
			errors.New("user ID not found in context"),
			http.StatusUnauthorized,
		)
		return
	}

	merchantID := app.getMerchantIDFromContext(ctx)
	if merchantID == nil || *merchantID == uuid.Nil {
		app.respondWithError(
			w,
			errors.New("merchant ID not found in context"),
			http.StatusBadRequest,
		)
		return
	}

	affiliateProgramID := app.getAffiliateProgramIDFromContext(ctx)
	if affiliateProgramID == nil || *affiliateProgramID == uuid.Nil {
		app.respondWithError(
			w,
			errors.New("affiliate program ID not found in context"),
			http.StatusBadRequest,
		)
		return
	}

	var input updateMerchantApplicationInput
	if err := app.readJSON(w, r, &input); err != nil {
		app.respondWithError(
			w,
			fmt.Errorf("invalid JSON input: %w", err),
			http.StatusBadRequest,
		)
		return
	}

	if input.ApplicationID == uuid.Nil {
		app.respondWithError(
			w,
			errors.New("application_id is required"),
			http.StatusBadRequest,
		)
		return
	}

	if input.StatusID == uuid.Nil {
		app.respondWithError(
			w,
			errors.New("status_id is required"),
			http.StatusBadRequest,
		)
		return
	}

	application := &data.MerchantApplication{
		ID:                 input.ApplicationID,
		MerchantID:         *merchantID,
		AffiliateProgramID: *affiliateProgramID,
		StatusID:           input.StatusID,
	}

	if err := app.Models.MerchantApplication.Update(ctx, application); err != nil {
		if errors.Is(err, data.ErrMerchantApplicationNotFound) {
			app.respondWithError(
				w,
				errors.New("merchant application not found"),
				http.StatusNotFound,
			)
			return
		}

		logger.Error(
			"Update merchant application failed",
			"application_id", application.ID,
			"error", err,
		)
		app.respondWithError(
			w,
			fmt.Errorf("failed to update merchant application: %w", err),
			http.StatusInternalServerError,
		)
		return
	}

	app.insertMerchantApplicationAudit(
		ctx,
		userID,
		updateMerchantApplicationAction,
		"Update a merchant application",
		merchantApplicationEntityTypeName,
		merchantApplicationEntityTypeDescription,
		application.ID.String(),
	)

	logger.Info(
		"Merchant application updated",
		"application_id", application.ID,
		"user_id", userID,
	)

	app.respondWithJSON(w, http.StatusOK, jsonResponse{
		Error:   false,
		Message: "Merchant application updated successfully",
		Data:    application,
	})
}

// SoftDeleteMerchantApplicationHandler logically removes one active merchant
// application by setting deleted_at through the data layer.
//
// Application deletion does not alter status_id. Workflow state and logical
// deletion remain separate concerns.
func (app *Application) SoftDeleteMerchantApplicationHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	logger := app.Logger.
		GetLoggerWithContext(r).
		WithFunctionName("SoftDeleteMerchantApplicationHandler")

	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	if !app.HasPermission(ctx, softDeleteMerchantApplicationAction) {
		app.respondWithError(
			w,
			errors.New("forbidden: insufficient permissions"),
			http.StatusForbidden,
		)
		return
	}

	userID := app.getUserIDFromContext(ctx)
	if userID == nil {
		app.respondWithError(
			w,
			errors.New("user ID not found in context"),
			http.StatusUnauthorized,
		)
		return
	}

	applicationID := app.getMerchantApplicationIDFromContext(ctx)
	if applicationID == nil || *applicationID == uuid.Nil {
		app.respondWithError(
			w,
			errors.New("merchant application ID not found in context"),
			http.StatusBadRequest,
		)
		return
	}

	if err := app.Models.MerchantApplication.SoftDelete(
		ctx,
		*applicationID,
	); err != nil {
		if errors.Is(err, data.ErrMerchantApplicationNotFound) {
			app.respondWithError(
				w,
				errors.New("merchant application not found"),
				http.StatusNotFound,
			)
			return
		}

		logger.Error(
			"Soft delete merchant application failed",
			"application_id", *applicationID,
			"error", err,
		)
		app.respondWithError(
			w,
			fmt.Errorf("failed to soft-delete merchant application: %w", err),
			http.StatusInternalServerError,
		)
		return
	}

	app.insertMerchantApplicationAudit(
		ctx,
		userID,
		softDeleteMerchantApplicationAction,
		"Soft-delete a merchant application",
		merchantApplicationEntityTypeName,
		merchantApplicationEntityTypeDescription,
		applicationID.String(),
	)

	logger.Info(
		"Merchant application soft-deleted",
		"application_id", *applicationID,
		"user_id", userID,
	)

	app.respondWithJSON(w, http.StatusOK, jsonResponse{
		Error:   false,
		Message: "Merchant application soft-deleted successfully",
		Data:    *applicationID,
	})
}

// CreateApplicationStatusHandler creates a merchant application status.
//
// New statuses default to active unless is_active is explicitly supplied.
func (app *Application) CreateApplicationStatusHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	logger := app.Logger.
		GetLoggerWithContext(r).
		WithFunctionName("CreateApplicationStatusHandler")

	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	if !app.HasPermission(ctx, createMerchantApplicationStatusAction) {
		app.respondWithError(
			w,
			errors.New("forbidden: insufficient permissions"),
			http.StatusForbidden,
		)
		return
	}

	userID := app.getUserIDFromContext(ctx)
	if userID == nil {
		app.respondWithError(
			w,
			errors.New("user ID not found in context"),
			http.StatusUnauthorized,
		)
		return
	}

	var input createMerchantApplicationStatusInput
	if err := app.readJSON(w, r, &input); err != nil {
		app.respondWithError(
			w,
			fmt.Errorf("invalid JSON input: %w", err),
			http.StatusBadRequest,
		)
		return
	}

	isActive := true
	if input.IsActive != nil {
		isActive = *input.IsActive
	}

	status := &data.MerchantApplicationStatus{
		ID:          uuid.New(),
		Name:        input.Name,
		Description: input.Description,
		IsActive:    isActive,
	}

	if err := app.Models.MerchantApplicationStatus.Insert(
		ctx,
		status,
	); err != nil {
		logger.Error(
			"Create merchant application status failed",
			"name", input.Name,
			"error", err,
		)
		app.respondWithError(
			w,
			fmt.Errorf("failed to create merchant application status: %w", err),
			http.StatusInternalServerError,
		)
		return
	}

	app.insertMerchantApplicationAudit(
		ctx,
		userID,
		createMerchantApplicationStatusAction,
		"Create a merchant application status",
		merchantApplicationStatusEntityTypeName,
		merchantApplicationStatusEntityTypeDescription,
		status.ID.String(),
	)

	logger.Info(
		"Merchant application status created",
		"status_id", status.ID,
		"user_id", userID,
	)

	app.respondWithJSON(w, http.StatusCreated, jsonResponse{
		Error:   false,
		Message: "Merchant application status created successfully",
		Data:    status,
	})
}

// GetApplicationStatusByIDHandler retrieves one merchant application status by
// its trusted context ID.
func (app *Application) GetApplicationStatusByIDHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	logger := app.Logger.
		GetLoggerWithContext(r).
		WithFunctionName("GetApplicationStatusByIDHandler")

	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	if !app.HasPermission(ctx, readMerchantApplicationStatusAction) {
		app.respondWithError(
			w,
			errors.New("forbidden: insufficient permissions"),
			http.StatusForbidden,
		)
		return
	}

	userID := app.getUserIDFromContext(ctx)
	if userID == nil {
		app.respondWithError(
			w,
			errors.New("user ID not found in context"),
			http.StatusUnauthorized,
		)
		return
	}

	statusID := app.getStatusIDFromContext(ctx)
	if statusID == nil || *statusID == uuid.Nil {
		app.respondWithError(
			w,
			errors.New("status ID not found in context"),
			http.StatusBadRequest,
		)
		return
	}

	status, err := app.Models.MerchantApplicationStatus.GetByID(
		ctx,
		*statusID,
	)
	if err != nil {
		if errors.Is(err, data.ErrMerchantApplicationStatusNotFound) {
			app.respondWithError(
				w,
				errors.New("merchant application status not found"),
				http.StatusNotFound,
			)
			return
		}

		logger.Error(
			"Retrieve merchant application status failed",
			"status_id", *statusID,
			"error", err,
		)
		app.respondWithError(
			w,
			fmt.Errorf(
				"failed to retrieve merchant application status: %w",
				err,
			),
			http.StatusInternalServerError,
		)
		return
	}

	app.insertMerchantApplicationAudit(
		ctx,
		userID,
		readMerchantApplicationStatusAction,
		"Read merchant application status by ID",
		merchantApplicationStatusEntityTypeName,
		merchantApplicationStatusEntityTypeDescription,
		status.ID.String(),
	)

	logger.Info(
		"Merchant application status retrieved",
		"status_id", status.ID,
		"user_id", userID,
	)

	app.respondWithJSON(w, http.StatusOK, jsonResponse{
		Error:   false,
		Message: "Merchant application status retrieved successfully",
		Data:    status,
	})
}

// GetApplicationStatusByNameHandler retrieves one merchant application status
// by its supplied name.
func (app *Application) GetApplicationStatusByNameHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	logger := app.Logger.
		GetLoggerWithContext(r).
		WithFunctionName("GetApplicationStatusByNameHandler")

	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	if !app.HasPermission(ctx, readMerchantApplicationStatusAction) {
		app.respondWithError(
			w,
			errors.New("forbidden: insufficient permissions"),
			http.StatusForbidden,
		)
		return
	}

	userID := app.getUserIDFromContext(ctx)
	if userID == nil {
		app.respondWithError(
			w,
			errors.New("user ID not found in context"),
			http.StatusUnauthorized,
		)
		return
	}

	var input getMerchantApplicationStatusByNameInput
	if err := app.readJSON(w, r, &input); err != nil {
		app.respondWithError(
			w,
			fmt.Errorf("invalid JSON input: %w", err),
			http.StatusBadRequest,
		)
		return
	}

	if strings.TrimSpace(input.Name) == "" {
		app.respondWithError(
			w,
			errors.New("name is required"),
			http.StatusBadRequest,
		)
		return
	}

	status, err := app.Models.MerchantApplicationStatus.GetByName(
		ctx,
		input.Name,
	)
	if err != nil {
		if errors.Is(err, data.ErrMerchantApplicationStatusNotFound) {
			app.respondWithError(
				w,
				errors.New("merchant application status not found"),
				http.StatusNotFound,
			)
			return
		}

		logger.Error(
			"Retrieve merchant application status by name failed",
			"name", input.Name,
			"error", err,
		)
		app.respondWithError(
			w,
			fmt.Errorf(
				"failed to retrieve merchant application status: %w",
				err,
			),
			http.StatusInternalServerError,
		)
		return
	}

	app.insertMerchantApplicationAudit(
		ctx,
		userID,
		readMerchantApplicationStatusAction,
		"Read merchant application status by name",
		merchantApplicationStatusEntityTypeName,
		merchantApplicationStatusEntityTypeDescription,
		status.ID.String(),
	)

	logger.Info(
		"Merchant application status retrieved by name",
		"status_id", status.ID,
		"name", status.Name,
		"user_id", userID,
	)

	app.respondWithJSON(w, http.StatusOK, jsonResponse{
		Error:   false,
		Message: "Merchant application status retrieved successfully",
		Data:    status,
	})
}

// GetAllApplicationStatusHandler retrieves a bounded page of merchant
// application statuses.
//
// The optional name query parameter performs a partial case-insensitive match.
// active_only=true restricts results to active statuses.
func (app *Application) GetAllApplicationStatusHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	logger := app.Logger.
		GetLoggerWithContext(r).
		WithFunctionName("GetAllApplicationStatusHandler")

	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	if !app.HasPermission(ctx, listMerchantApplicationStatusesAction) {
		app.respondWithError(
			w,
			errors.New("forbidden: insufficient permissions"),
			http.StatusForbidden,
		)
		return
	}

	userID := app.getUserIDFromContext(ctx)
	if userID == nil {
		app.respondWithError(
			w,
			errors.New("user ID not found in context"),
			http.StatusUnauthorized,
		)
		return
	}

	limit, offset, err := merchantApplicationPagination(r)
	if err != nil {
		app.respondWithError(w, err, http.StatusBadRequest)
		return
	}

	nameFilter := strings.TrimSpace(r.URL.Query().Get("name"))

	activeOnly, err := parseOptionalMerchantApplicationBool(
		r.URL.Query().Get("active_only"),
		"active_only",
	)
	if err != nil {
		app.respondWithError(w, err, http.StatusBadRequest)
		return
	}

	statuses, err := app.Models.MerchantApplicationStatus.GetAllWithFilter(
		ctx,
		nameFilter,
		activeOnly,
		limit,
		offset,
	)
	if err != nil {
		logger.Error(
			"List merchant application statuses failed",
			"name", nameFilter,
			"active_only", activeOnly,
			"limit", limit,
			"offset", offset,
			"error", err,
		)
		app.respondWithError(
			w,
			fmt.Errorf(
				"failed to retrieve merchant application statuses: %w",
				err,
			),
			http.StatusInternalServerError,
		)
		return
	}

	app.insertMerchantApplicationAudit(
		ctx,
		userID,
		listMerchantApplicationStatusesAction,
		"List merchant application statuses",
		merchantApplicationStatusEntityTypeName,
		merchantApplicationStatusEntityTypeDescription,
		"",
	)

	logger.Info(
		"Merchant application statuses retrieved",
		"count", len(statuses),
		"name", nameFilter,
		"active_only", activeOnly,
		"limit", limit,
		"offset", offset,
		"user_id", userID,
	)

	app.respondWithJSON(w, http.StatusOK, jsonResponse{
		Error:   false,
		Message: "Merchant application statuses retrieved successfully",
		Data:    statuses,
	})
}

// UpdateApplicationStatusHandler fully replaces the mutable fields of one
// merchant application status.
func (app *Application) UpdateApplicationStatusHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	logger := app.Logger.
		GetLoggerWithContext(r).
		WithFunctionName("UpdateApplicationStatusHandler")

	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	if !app.HasPermission(ctx, updateMerchantApplicationStatusAction) {
		app.respondWithError(
			w,
			errors.New("forbidden: insufficient permissions"),
			http.StatusForbidden,
		)
		return
	}

	userID := app.getUserIDFromContext(ctx)
	if userID == nil {
		app.respondWithError(
			w,
			errors.New("user ID not found in context"),
			http.StatusUnauthorized,
		)
		return
	}

	statusID := app.getStatusIDFromContext(ctx)
	if statusID == nil || *statusID == uuid.Nil {
		app.respondWithError(
			w,
			errors.New("status ID not found in context"),
			http.StatusBadRequest,
		)
		return
	}

	var input updateMerchantApplicationStatusInput
	if err := app.readJSON(w, r, &input); err != nil {
		app.respondWithError(
			w,
			fmt.Errorf("invalid JSON input: %w", err),
			http.StatusBadRequest,
		)
		return
	}

	status := &data.MerchantApplicationStatus{
		ID:          *statusID,
		Name:        input.Name,
		Description: input.Description,
		IsActive:    input.IsActive,
	}

	if err := app.Models.MerchantApplicationStatus.Update(
		ctx,
		status,
	); err != nil {
		if errors.Is(err, data.ErrMerchantApplicationStatusNotFound) {
			app.respondWithError(
				w,
				errors.New("merchant application status not found"),
				http.StatusNotFound,
			)
			return
		}

		logger.Error(
			"Update merchant application status failed",
			"status_id", status.ID,
			"error", err,
		)
		app.respondWithError(
			w,
			fmt.Errorf(
				"failed to update merchant application status: %w",
				err,
			),
			http.StatusInternalServerError,
		)
		return
	}

	app.insertMerchantApplicationAudit(
		ctx,
		userID,
		updateMerchantApplicationStatusAction,
		"Update a merchant application status",
		merchantApplicationStatusEntityTypeName,
		merchantApplicationStatusEntityTypeDescription,
		status.ID.String(),
	)

	logger.Info(
		"Merchant application status updated",
		"status_id", status.ID,
		"user_id", userID,
	)

	app.respondWithJSON(w, http.StatusOK, jsonResponse{
		Error:   false,
		Message: "Merchant application status updated successfully",
		Data:    status,
	})
}

// SoftDeleteApplicationStatusHandler deactivates one merchant application
// status by setting is_active=false through the data layer.
//
// The historical soft-delete method and permission names are retained for
// compatibility, but the domain operation is deactivation.
func (app *Application) SoftDeleteApplicationStatusHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	logger := app.Logger.
		GetLoggerWithContext(r).
		WithFunctionName("SoftDeleteApplicationStatusHandler")

	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	if !app.HasPermission(ctx, softDeleteMerchantApplicationStatusAction) {
		app.respondWithError(
			w,
			errors.New("forbidden: insufficient permissions"),
			http.StatusForbidden,
		)
		return
	}

	userID := app.getUserIDFromContext(ctx)
	if userID == nil {
		app.respondWithError(
			w,
			errors.New("user ID not found in context"),
			http.StatusUnauthorized,
		)
		return
	}

	statusID := app.getStatusIDFromContext(ctx)
	if statusID == nil || *statusID == uuid.Nil {
		app.respondWithError(
			w,
			errors.New("status ID not found in context"),
			http.StatusBadRequest,
		)
		return
	}

	if err := app.Models.MerchantApplicationStatus.SoftDelete(
		ctx,
		*statusID,
	); err != nil {
		switch {
		case errors.Is(err, data.ErrMerchantApplicationStatusNotFound):
			app.respondWithError(
				w,
				errors.New("merchant application status not found"),
				http.StatusNotFound,
			)
			return

		case errors.Is(
			err,
			data.ErrMerchantApplicationStatusAlreadyInactive,
		):
			app.respondWithError(
				w,
				errors.New("merchant application status is already inactive"),
				http.StatusConflict,
			)
			return

		default:
			logger.Error(
				"Deactivate merchant application status failed",
				"status_id", *statusID,
				"error", err,
			)
			app.respondWithError(
				w,
				fmt.Errorf(
					"failed to deactivate merchant application status: %w",
					err,
				),
				http.StatusInternalServerError,
			)
			return
		}
	}

	app.insertMerchantApplicationAudit(
		ctx,
		userID,
		softDeleteMerchantApplicationStatusAction,
		"Deactivate a merchant application status",
		merchantApplicationStatusEntityTypeName,
		merchantApplicationStatusEntityTypeDescription,
		statusID.String(),
	)

	logger.Info(
		"Merchant application status deactivated",
		"status_id", *statusID,
		"user_id", userID,
	)

	app.respondWithJSON(w, http.StatusOK, jsonResponse{
		Error:   false,
		Message: "Merchant application status deactivated successfully",
		Data:    *statusID,
	})
}

// insertMerchantApplicationAudit performs best-effort audit insertion.
//
// A successfully completed domain operation is not converted into partial
// content or failure merely because audit metadata or persistence is unavailable.
func (app *Application) insertMerchantApplicationAudit(
	ctx context.Context,
	userID *uuid.UUID,
	actionName string,
	actionDescription string,
	entityTypeName string,
	entityTypeDescription string,
	entityID string,
) {
	if userID == nil || *userID == uuid.Nil {
		return
	}

	action, err := app.Models.Action.GetByName(ctx, actionName)
	if err != nil || action == nil {
		actionID, createErr := app.Models.Action.CreateIfNotExists(
			ctx,
			actionName,
			actionDescription,
		)
		if createErr != nil {
			return
		}

		action = &data.Action{
			ID: actionID,
		}
	}

	entityType, err := app.Models.EntityType.GetByName(
		ctx,
		entityTypeName,
	)
	if err != nil || entityType == nil {
		entityTypeID, createErr := app.Models.EntityType.CreateIfNotExists(
			ctx,
			entityTypeName,
			entityTypeDescription,
		)
		if createErr != nil {
			return
		}

		entityType = &data.EntityType{
			ID: entityTypeID,
		}
	}

	if action == nil || action.ID == uuid.Nil {
		return
	}

	if entityType == nil || entityType.ID == uuid.Nil {
		return
	}

	auditLog := &data.AuditLog{
		ID:           uuid.New(),
		UserID:       userID,
		ActionID:     action.ID,
		EntityTypeID: entityType.ID,
		EntityID:     entityID,
	}

	_ = app.Models.AuditLog.Insert(ctx, auditLog)
}

func merchantApplicationPagination(
	r *http.Request,
) (int, int, error) {
	query := r.URL.Query()

	limit, err := parseMerchantApplicationInteger(
		query.Get("limit"),
		defaultMerchantApplicationLimit,
		"limit",
	)
	if err != nil {
		return 0, 0, err
	}

	offset, err := parseMerchantApplicationInteger(
		query.Get("offset"),
		0,
		"offset",
	)
	if err != nil {
		return 0, 0, err
	}

	if limit < 1 {
		return 0, 0, errors.New("limit must be greater than 0")
	}

	if limit > maxMerchantApplicationLimit {
		limit = maxMerchantApplicationLimit
	}

	if offset < 0 {
		return 0, 0, errors.New("offset cannot be negative")
	}

	return limit, offset, nil
}

func parseMerchantApplicationInteger(
	rawValue string,
	defaultValue int,
	fieldName string,
) (int, error) {
	rawValue = strings.TrimSpace(rawValue)
	if rawValue == "" {
		return defaultValue, nil
	}

	value, err := strconv.Atoi(rawValue)
	if err != nil {
		return 0, fmt.Errorf("%s must be a valid integer", fieldName)
	}

	return value, nil
}

func parseOptionalMerchantApplicationUUID(
	rawValue string,
	fieldName string,
) (uuid.UUID, error) {
	rawValue = strings.TrimSpace(rawValue)
	if rawValue == "" {
		return uuid.Nil, nil
	}

	value, err := uuid.Parse(rawValue)
	if err != nil {
		return uuid.Nil, fmt.Errorf("%s must be a valid UUID", fieldName)
	}

	return value, nil
}

func parseOptionalMerchantApplicationBool(
	rawValue string,
	fieldName string,
) (bool, error) {
	rawValue = strings.TrimSpace(rawValue)
	if rawValue == "" {
		return false, nil
	}

	value, err := strconv.ParseBool(rawValue)
	if err != nil {
		return false, fmt.Errorf("%s must be true or false", fieldName)
	}

	return value, nil
}