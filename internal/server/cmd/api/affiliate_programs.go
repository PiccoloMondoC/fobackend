// Package main provides HTTP handlers for the Platform API.
//
// sdworkspace/sdbackend/internal/server/cmd/api/affiliate_programs.go
//
// GTM:
//
//	Layer: 2.4 Merchant / Affiliate Domain
//	Release Class: DEFERRED
//	Reason:
//	  Affiliate program HTTP administration is valid post-release merchant
//	  integration infrastructure, but it is not required for the initial
//	  Future Commerce v1 release spine.
//
//	  The canonical affiliate program data model remains production-ready,
//	  including protected encrypted credential storage and public-safe read
//	  paths. This HTTP administration surface remains deferred until affiliate
//	  provider onboarding, credential encryption, rotation, and internal
//	  administration workflows are promoted into an active release.
//
// DEFERRED Rule:
//
//	Keep compiling.
//	Keep safe.
//	Keep production-ready.
//	Preserve authorization enforcement.
//	Preserve trusted-context actor and entity extraction.
//	Preserve compatibility with the canonical data layer.
//	Preserve DB-owned lifecycle timestamps.
//	Preserve typed data-layer sentinel handling.
//	Preserve public-safe reads.
//	Preserve best-effort audit logging.
//	Never accept plaintext API credentials.
//	Never expose encrypted credential material.
//	Support api_auth_method "None" only while DEFERRED.
//	Do not add new features.
//	Do not register these handlers in routes.go while DEFERRED.
//	Do not block deployment on this file unless it breaks the build or safety.
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
	affiliateProgramEntityTypeName        = "affiliate_program"
	affiliateProgramEntityTypeDescription = "Affiliate program entity"

	createAffiliateProgramAction     = "create_affiliate_program"
	readAffiliateProgramAction       = "read_affiliate_program"
	listAffiliateProgramsAction      = "list_affiliate_programs"
	updateAffiliateProgramAction     = "update_affiliate_program"
	softDeleteAffiliateProgramAction = "soft_delete_affiliate_program"

	affiliateProgramAuthMethodNone   = "None"
	affiliateProgramAuthMethodAPIKey = "APIKey"

	defaultAffiliateProgramLimit = 20
	maxAffiliateProgramLimit     = 100
)

type saveAffiliateProgramInput struct {
	Name          string  `json:"name"`
	Website       string  `json:"website"`
	APIEndpoint   *string `json:"api_endpoint,omitempty"`
	APIAuthMethod string  `json:"api_auth_method"`
}

type updateAffiliateProgramInput struct {
	Name          string  `json:"name"`
	Website       string  `json:"website"`
	APIEndpoint   *string `json:"api_endpoint,omitempty"`
	APIAuthMethod string  `json:"api_auth_method"`
}

// SaveAffiliateProgramHandler creates a new affiliate program.
//
// This DEFERRED HTTP domain accepts public, non-secret configuration only.
// api_auth_method "APIKey" is rejected until an approved credential-encryption
// boundary is wired into the handler or service layer.
func (app *Application) SaveAffiliateProgramHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	logger := app.Logger.
		GetLoggerWithContext(r).
		WithFunctionName("SaveAffiliateProgramHandler")

	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	if !app.HasPermission(ctx, createAffiliateProgramAction) {
		app.respondWithError(
			w,
			errors.New("forbidden: insufficient permissions"),
			http.StatusForbidden,
		)
		return
	}

	userID := app.getUserIDFromContext(ctx)
	if userID == nil || *userID == uuid.Nil {
		app.respondWithError(
			w,
			errors.New("user ID not found in context"),
			http.StatusUnauthorized,
		)
		return
	}

	var input saveAffiliateProgramInput
	if err := app.readJSON(w, r, &input); err != nil {
		app.respondWithError(
			w,
			fmt.Errorf("invalid JSON input: %w", err),
			http.StatusBadRequest,
		)
		return
	}

	authMethod, err := validateDeferredAffiliateProgramAuthMethod(
		input.APIAuthMethod,
	)
	if err != nil {
		app.respondWithError(w, err, http.StatusBadRequest)
		return
	}

	program := &data.AffiliateProgram{
		ID:            uuid.New(),
		Name:          input.Name,
		Website:       input.Website,
		APIEndpoint:   input.APIEndpoint,
		APIAuthMethod: authMethod,
	}

	if err := app.Models.AffiliateProgram.Insert(ctx, program); err != nil {
		if errors.Is(err, data.ErrAffiliateProgramAlreadyExists) {
			app.respondWithError(
				w,
				errors.New("affiliate program already exists"),
				http.StatusConflict,
			)
			return
		}

		logger.Error(
			"Create affiliate program failed",
			"program_id", program.ID,
			"error", err,
		)

		app.respondWithError(
			w,
			fmt.Errorf("failed to create affiliate program: %w", err),
			http.StatusInternalServerError,
		)
		return
	}

	app.insertAffiliateProgramAudit(
		ctx,
		userID,
		createAffiliateProgramAction,
		"Create an affiliate program",
		program.ID.String(),
	)

	logger.Info(
		"Affiliate program created",
		"program_id", program.ID,
		"user_id", userID,
	)

	app.respondWithJSON(w, http.StatusCreated, jsonResponse{
		Error:   false,
		Message: "Affiliate program created successfully",
		Data:    program,
	})
}

// GetAffiliateProgramByIDHandler retrieves one active affiliate program using
// the trusted affiliate program ID installed in request context.
//
// The standard data-layer read path intentionally excludes encrypted credential
// material.
func (app *Application) GetAffiliateProgramByIDHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	logger := app.Logger.
		GetLoggerWithContext(r).
		WithFunctionName("GetAffiliateProgramByIDHandler")

	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	if !app.HasPermission(ctx, readAffiliateProgramAction) {
		app.respondWithError(
			w,
			errors.New("forbidden: insufficient permissions"),
			http.StatusForbidden,
		)
		return
	}

	userID := app.getUserIDFromContext(ctx)
	if userID == nil || *userID == uuid.Nil {
		app.respondWithError(
			w,
			errors.New("user ID not found in context"),
			http.StatusUnauthorized,
		)
		return
	}

	programID := app.getAffiliateProgramIDFromContext(ctx)
	if programID == nil || *programID == uuid.Nil {
		app.respondWithError(
			w,
			errors.New("affiliate program ID not found in context"),
			http.StatusBadRequest,
		)
		return
	}

	program, err := app.Models.AffiliateProgram.GetByID(ctx, *programID)
	if err != nil {
		if errors.Is(err, data.ErrAffiliateProgramNotFound) {
			app.respondWithError(
				w,
				errors.New("affiliate program not found"),
				http.StatusNotFound,
			)
			return
		}

		logger.Error(
			"Retrieve affiliate program failed",
			"program_id", *programID,
			"error", err,
		)

		app.respondWithError(
			w,
			fmt.Errorf("failed to retrieve affiliate program: %w", err),
			http.StatusInternalServerError,
		)
		return
	}

	if program == nil {
		app.respondWithError(
			w,
			errors.New("affiliate program not found"),
			http.StatusNotFound,
		)
		return
	}

	app.insertAffiliateProgramAudit(
		ctx,
		userID,
		readAffiliateProgramAction,
		"Read an affiliate program by ID",
		program.ID.String(),
	)

	logger.Info(
		"Affiliate program retrieved",
		"program_id", program.ID,
		"user_id", userID,
	)

	app.respondWithJSON(w, http.StatusOK, jsonResponse{
		Error:   false,
		Message: "Affiliate program retrieved successfully",
		Data:    program,
	})
}

// GetAllAffiliateProgramsHandler retrieves a bounded list of affiliate programs.
//
// Pagination and the optional include-deleted filter are read from trusted
// middleware context. Invalid or missing values fall back safely rather than
// panicking on context type assertions.
func (app *Application) GetAllAffiliateProgramsHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	logger := app.Logger.
		GetLoggerWithContext(r).
		WithFunctionName("GetAllAffiliateProgramsHandler")

	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	if !app.HasPermission(ctx, listAffiliateProgramsAction) {
		app.respondWithError(
			w,
			errors.New("forbidden: insufficient permissions"),
			http.StatusForbidden,
		)
		return
	}

	userID := app.getUserIDFromContext(ctx)
	if userID == nil || *userID == uuid.Nil {
		app.respondWithError(
			w,
			errors.New("user ID not found in context"),
			http.StatusUnauthorized,
		)
		return
	}

	limit := affiliateProgramContextInt(
		ctx.Value(ctxPaginationLimit),
		defaultAffiliateProgramLimit,
	)
	offset := affiliateProgramContextInt(
		ctx.Value(ctxPaginationOffset),
		0,
	)
	includeDeleted := affiliateProgramContextBool(
		ctx.Value(ctxIncludeDeleted),
		false,
	)

	if limit < 1 {
		limit = defaultAffiliateProgramLimit
	}

	if limit > maxAffiliateProgramLimit {
		limit = maxAffiliateProgramLimit
	}

	if offset < 0 {
		offset = 0
	}

	programs, err := app.Models.AffiliateProgram.GetAll(
		ctx,
		includeDeleted,
		limit,
		offset,
	)
	if err != nil {
		logger.Error(
			"List affiliate programs failed",
			"include_deleted", includeDeleted,
			"limit", limit,
			"offset", offset,
			"error", err,
		)

		app.respondWithError(
			w,
			fmt.Errorf("failed to retrieve affiliate programs: %w", err),
			http.StatusInternalServerError,
		)
		return
	}

	// List operations use one collection-level audit event rather than one
	// audit row per returned item. This avoids multiplying audit writes by the
	// page size while still recording access to the protected collection.
	app.insertAffiliateProgramAudit(
		ctx,
		userID,
		listAffiliateProgramsAction,
		"List affiliate programs",
		"",
	)

	logger.Info(
		"Affiliate programs retrieved",
		"count", len(programs),
		"include_deleted", includeDeleted,
		"limit", limit,
		"offset", offset,
		"user_id", userID,
	)

	app.respondWithJSON(w, http.StatusOK, jsonResponse{
		Error:   false,
		Message: "Affiliate programs retrieved successfully",
		Data:    programs,
	})
}

// UpdateAffiliateProgramHandler replaces the mutable public fields of one
// active affiliate program.
//
// The canonical data-layer Update method performs a complete replacement of
// mutable fields. This handler therefore requires the complete public mutable
// representation even though the historical route uses PATCH.
//
// Credential material is intentionally not accepted. While this HTTP domain is
// DEFERRED, only api_auth_method "None" is supported.
func (app *Application) UpdateAffiliateProgramHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	logger := app.Logger.
		GetLoggerWithContext(r).
		WithFunctionName("UpdateAffiliateProgramHandler")

	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	if !app.HasPermission(ctx, updateAffiliateProgramAction) {
		app.respondWithError(
			w,
			errors.New("forbidden: insufficient permissions"),
			http.StatusForbidden,
		)
		return
	}

	userID := app.getUserIDFromContext(ctx)
	if userID == nil || *userID == uuid.Nil {
		app.respondWithError(
			w,
			errors.New("user ID not found in context"),
			http.StatusUnauthorized,
		)
		return
	}

	programID := app.getAffiliateProgramIDFromContext(ctx)
	if programID == nil || *programID == uuid.Nil {
		app.respondWithError(
			w,
			errors.New("affiliate program ID not found in context"),
			http.StatusBadRequest,
		)
		return
	}

	var input updateAffiliateProgramInput
	if err := app.readJSON(w, r, &input); err != nil {
		app.respondWithError(
			w,
			fmt.Errorf("invalid JSON input: %w", err),
			http.StatusBadRequest,
		)
		return
	}

	authMethod, err := validateDeferredAffiliateProgramAuthMethod(
		input.APIAuthMethod,
	)
	if err != nil {
		app.respondWithError(w, err, http.StatusBadRequest)
		return
	}

	program := &data.AffiliateProgram{
		ID:            *programID,
		Name:          input.Name,
		Website:       input.Website,
		APIEndpoint:   input.APIEndpoint,
		APIAuthMethod: authMethod,
	}

	if err := app.Models.AffiliateProgram.Update(ctx, program); err != nil {
		if errors.Is(err, data.ErrAffiliateProgramNotFound) {
			app.respondWithError(
				w,
				errors.New("affiliate program not found"),
				http.StatusNotFound,
			)
			return
		}

		if errors.Is(err, data.ErrAffiliateProgramAlreadyExists) {
			app.respondWithError(
				w,
				errors.New("affiliate program already exists"),
				http.StatusConflict,
			)
			return
		}

		logger.Error(
			"Update affiliate program failed",
			"program_id", *programID,
			"error", err,
		)

		app.respondWithError(
			w,
			fmt.Errorf("failed to update affiliate program: %w", err),
			http.StatusInternalServerError,
		)
		return
	}

	app.insertAffiliateProgramAudit(
		ctx,
		userID,
		updateAffiliateProgramAction,
		"Update an affiliate program",
		program.ID.String(),
	)

	logger.Info(
		"Affiliate program updated",
		"program_id", program.ID,
		"user_id", userID,
	)

	app.respondWithJSON(w, http.StatusOK, jsonResponse{
		Error:   false,
		Message: "Affiliate program updated successfully",
		Data:    program,
	})
}

// SoftDeleteAffiliateProgramHandler logically deletes one active affiliate
// program through the canonical deleted_at lifecycle path.
func (app *Application) SoftDeleteAffiliateProgramHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	logger := app.Logger.
		GetLoggerWithContext(r).
		WithFunctionName("SoftDeleteAffiliateProgramHandler")

	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	if !app.HasPermission(ctx, "delete_affiliate_program") {
		app.respondWithError(
			w,
			errors.New("forbidden: insufficient permissions"),
			http.StatusForbidden,
		)
		return
	}

	userID := app.getUserIDFromContext(ctx)
	if userID == nil || *userID == uuid.Nil {
		app.respondWithError(
			w,
			errors.New("user ID not found in context"),
			http.StatusUnauthorized,
		)
		return
	}

	programID := app.getAffiliateProgramIDFromContext(ctx)
	if programID == nil || *programID == uuid.Nil {
		app.respondWithError(
			w,
			errors.New("affiliate program ID not found in context"),
			http.StatusBadRequest,
		)
		return
	}

	if err := app.Models.AffiliateProgram.SoftDelete(
		ctx,
		*programID,
	); err != nil {
		if errors.Is(err, data.ErrAffiliateProgramNotFound) {
			app.respondWithError(
				w,
				errors.New("affiliate program not found"),
				http.StatusNotFound,
			)
			return
		}

		logger.Error(
			"Soft delete affiliate program failed",
			"program_id", *programID,
			"error", err,
		)

		app.respondWithError(
			w,
			fmt.Errorf("failed to soft delete affiliate program: %w", err),
			http.StatusInternalServerError,
		)
		return
	}

	app.insertAffiliateProgramAudit(
		ctx,
		userID,
		softDeleteAffiliateProgramAction,
		"Soft delete an affiliate program",
		programID.String(),
	)

	logger.Info(
		"Affiliate program soft deleted",
		"program_id", *programID,
		"user_id", userID,
	)

	app.respondWithJSON(w, http.StatusOK, jsonResponse{
		Error:   false,
		Message: "Affiliate program soft deleted successfully",
		Data:    programID,
	})
}

// insertAffiliateProgramAudit performs best-effort audit insertion.
//
// Affiliate program HTTP administration is DEFERRED. A completed domain
// operation is not rolled back or converted into a partial-content response
// merely because audit metadata resolution or audit persistence is unavailable.
func (app *Application) insertAffiliateProgramAudit(
	ctx context.Context,
	userID *uuid.UUID,
	actionName string,
	actionDescription string,
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
		affiliateProgramEntityTypeName,
	)
	if err != nil || entityType == nil {
		entityTypeID, createErr := app.Models.EntityType.CreateIfNotExists(
			ctx,
			affiliateProgramEntityTypeName,
			affiliateProgramEntityTypeDescription,
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

	// Audit insertion is intentionally best-effort for this DEFERRED domain.
	_ = app.Models.AuditLog.Insert(ctx, auditLog)
}

// validateDeferredAffiliateProgramAuthMethod enforces the DEFERRED HTTP policy.
//
// Empty input is normalized to "None". "APIKey" and every unknown value are
// rejected because this HTTP layer does not yet own an approved encryption
// pipeline and must never accept plaintext credentials.
func validateDeferredAffiliateProgramAuthMethod(
	rawValue string,
) (string, error) {
	value := strings.TrimSpace(rawValue)

	if value == "" || value == affiliateProgramAuthMethodNone {
		return affiliateProgramAuthMethodNone, nil
	}

	if value == affiliateProgramAuthMethodAPIKey {
		return "", errors.New(
			"api_auth_method \"APIKey\" is unavailable while affiliate program administration is deferred",
		)
	}

	return "", fmt.Errorf(
		"invalid api_auth_method %q: only %q is supported while deferred",
		value,
		affiliateProgramAuthMethodNone,
	)
}

// affiliateProgramContextInt safely converts middleware context values to int.
//
// Pagination middleware may store values as strings or integer types. Invalid
// values return the supplied default instead of causing a type-assertion panic.
func affiliateProgramContextInt(
	rawValue any,
	defaultValue int,
) int {
	switch value := rawValue.(type) {
	case int:
		return value

	case int8:
		return int(value)

	case int16:
		return int(value)

	case int32:
		return int(value)

	case int64:
		return int(value)

	case uint:
		return int(value)

	case uint8:
		return int(value)

	case uint16:
		return int(value)

	case uint32:
		return int(value)

	case uint64:
		if value > uint64(^uint(0)>>1) {
			return defaultValue
		}
		return int(value)

	case string:
		parsed, err := strconv.Atoi(strings.TrimSpace(value))
		if err != nil {
			return defaultValue
		}
		return parsed

	case fmt.Stringer:
		parsed, err := strconv.Atoi(strings.TrimSpace(value.String()))
		if err != nil {
			return defaultValue
		}
		return parsed

	default:
		return defaultValue
	}
}

// affiliateProgramContextBool safely converts middleware context values to bool.
func affiliateProgramContextBool(
	rawValue any,
	defaultValue bool,
) bool {
	switch value := rawValue.(type) {
	case bool:
		return value

	case string:
		parsed, err := strconv.ParseBool(strings.TrimSpace(value))
		if err != nil {
			return defaultValue
		}
		return parsed

	case fmt.Stringer:
		parsed, err := strconv.ParseBool(
			strings.TrimSpace(value.String()),
		)
		if err != nil {
			return defaultValue
		}
		return parsed

	default:
		return defaultValue
	}
}
