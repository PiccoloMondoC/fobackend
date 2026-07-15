// Package main provides HTTP handlers for platform setting governance.
//
// sdworkspace/sdbackend/internal/server/cmd/api/platform_settings.go
//
// GTM:
//
//	Layer: 2.1 Database / Governance Foundation
//	Release Class: SPINE
//	Reason:
//	  platform_settings handler surface is release-critical platform
//	  configuration governance infrastructure. It governs privileged
//	  administrative creation, idempotent ensure, read, list, value update,
//	  active-state management, soft delete, hard delete, and existence-check
//	  operations for platform_settings rows.
//
//	  Platform settings are platform-level operational configuration only.
//	  This handler surface is not user preferences, merchant configuration,
//	  consumer product configuration, or a general product-behavior escape
//	  hatch.
//
//	  Soft delete preserves historical configuration records while removing
//	  them from standard operational reads. Hard delete is administrative or
//	  pre-startup cleanup only.
//
// SPINE Rule:
//
//	Keep compiling.
//	Keep production-ready.
//	Preserve privileged authorization at every boundary.
//	Preserve audit coverage for all platform setting mutations.
//	Preserve soft-delete-aware operational handler routing.
//	Preserve JSON value passthrough without coercion.
//	Preserve setting_value opacity in all log output.
//	Block deployment if this file breaks build, platform configuration
//	governance, admin mutation coverage, audit metadata coverage, or route
//	registration.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/PiccoloMondoC/sdworkspace/sdbackend/internal/data"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

const (
	platformSettingEntityType = "platform_setting"
	platformSettingsHardDeleteEnabledKey = "platform_settings_hard_delete_enabled"

	actionCreatePlatformSetting      = "create_platform_setting"
	actionEnsurePlatformSetting      = "ensure_platform_setting"
	actionUpdatePlatformSettingValue = "update_platform_setting_value"
	actionSetPlatformSettingActive   = "set_platform_setting_active"
	actionSoftDeletePlatformSetting  = "soft_delete_platform_setting"
	actionHardDeletePlatformSetting  = "hard_delete_platform_setting"

	permissionCreatePlatformSetting     = "create_platform_setting"
	permissionEnsurePlatformSetting     = "ensure_platform_setting"
	permissionReadPlatformSetting       = "read_platform_setting"
	permissionListPlatformSettings      = "list_platform_settings"
	permissionUpdatePlatformSetting     = "update_platform_setting"
	permissionActivatePlatformSetting   = "activate_platform_setting"
	permissionDeactivatePlatformSetting = "deactivate_platform_setting"
	permissionSoftDeletePlatformSetting = "soft_delete_platform_setting"
	permissionHardDeletePlatformSetting = "hard_delete_platform_setting"
)

type createPlatformSettingInput struct {
	SettingKey   string                        `json:"setting_key"`
	SettingValue json.RawMessage               `json:"setting_value"`
	ValueType    data.PlatformSettingValueType `json:"value_type"`
	Description  string                        `json:"description"`
	IsActive     bool                          `json:"is_active"`
}

type ensurePlatformSettingInput struct {
	SettingKey   string                        `json:"setting_key"`
	SettingValue json.RawMessage               `json:"setting_value"`
	ValueType    data.PlatformSettingValueType `json:"value_type"`
	Description  string                        `json:"description"`
	IsActive     bool                          `json:"is_active"`
}

type platformSettingKeyInput struct {
	SettingKey string `json:"setting_key"`
}

type updatePlatformSettingValueInput struct {
	SettingValue json.RawMessage               `json:"setting_value"`
	ValueType    data.PlatformSettingValueType `json:"value_type"`
	Description  string                        `json:"description"`
}

type setPlatformSettingActiveInput struct {
	IsActive bool `json:"is_active"`
}

type platformSettingExistsResponse struct {
	SettingKey string `json:"setting_key"`
	Exists     bool   `json:"exists"`
}

func platformSettingHTTPStatus(err error) int {
	if err == nil {
		return http.StatusOK
	}

	switch {
	case errors.Is(err, data.ErrPlatformSettingAlreadyExists):
		return http.StatusConflict
	case errors.Is(err, data.ErrPlatformSettingNotFound):
		return http.StatusNotFound
	case errors.Is(err, data.ErrPlatformSettingActorNotFound):
		return http.StatusUnprocessableEntity
	case errors.Is(err, data.ErrPlatformSettingTypeMismatch):
		return http.StatusConflict
	}

	msg := strings.ToLower(err.Error())

	switch {
	case strings.Contains(msg, "already exists"):
		return http.StatusConflict
	case strings.Contains(msg, "not found"):
		return http.StatusNotFound
	case strings.Contains(msg, "references missing"):
		return http.StatusUnprocessableEntity
	case strings.Contains(msg, "invalid"),
		strings.Contains(msg, "required"),
		strings.Contains(msg, "must match"),
		strings.Contains(msg, "must be"),
		strings.Contains(msg, "must contain"),
		strings.Contains(msg, "not valid json"):
		return http.StatusBadRequest
	default:
		return http.StatusInternalServerError
	}
}

func (app *Application) parsePlatformSettingID(r *http.Request) (uuid.UUID, error) {
	raw := strings.TrimSpace(chi.URLParam(r, "platformSettingID"))
	if raw == "" {
		return uuid.Nil, errors.New("platform setting ID is required")
	}

	id, err := uuid.Parse(raw)
	if err != nil || id == uuid.Nil {
		return uuid.Nil, errors.New("invalid platform setting ID")
	}

	return id, nil
}

func (app *Application) platformSettingsHardDeleteEnabled(ctx context.Context) (bool, error) {
	setting, err := app.Models.PlatformSetting.GetActiveByKey(ctx, platformSettingsHardDeleteEnabledKey)
	if err != nil {
		return false, err
	}
	if setting == nil {
		return false, nil
	}

	var enabled bool
	decoder := json.NewDecoder(bytes.NewReader(setting.SettingValue))
	decoder.DisallowUnknownFields()

	if err := decoder.Decode(&enabled); err != nil {
		return false, fmt.Errorf("decode %s: %w", platformSettingsHardDeleteEnabledKey, err)
	}

	return enabled, nil
}

func (app *Application) auditPlatformSetting(
	ctx context.Context,
	userID *uuid.UUID,
	actionName string,
	actionDescription string,
	entityID string,
) error {
	if userID == nil {
		return errors.New("user ID is required for platform setting audit logging")
	}

	action, err := app.Models.Action.GetByName(ctx, actionName)
	if err != nil || action == nil {
		actionID, createErr := app.Models.Action.CreateIfNotExists(ctx, actionName, actionDescription)
		if createErr != nil {
			return fmt.Errorf("resolve audit action %s: %w", actionName, createErr)
		}
		action = &data.Action{ID: actionID}
	}

	entityType, err := app.Models.EntityType.GetByName(ctx, platformSettingEntityType)
	if err != nil || entityType == nil {
		entityTypeID, createErr := app.Models.EntityType.CreateIfNotExists(
			ctx,
			platformSettingEntityType,
			"Platform setting configuration entity",
		)
		if createErr != nil {
			return fmt.Errorf("resolve audit entity type %s: %w", platformSettingEntityType, createErr)
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
		return fmt.Errorf("insert platform setting audit log: %w", err)
	}

	return nil
}

// CreatePlatformSettingHandler creates a new platform setting.
func (app *Application) CreatePlatformSettingHandler(w http.ResponseWriter, r *http.Request) {
	logger := app.Logger.GetLoggerWithContext(r).WithFunctionName("CreatePlatformSettingHandler")

	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	if !app.HasPermission(ctx, permissionCreatePlatformSetting) {
		app.respondWithError(w, errors.New("forbidden: insufficient permissions"), http.StatusForbidden)
		return
	}

	userID := app.getUserIDFromContext(ctx)
	if userID == nil {
		app.respondWithError(w, errors.New("user ID not found in context"), http.StatusUnauthorized)
		return
	}

	var input createPlatformSettingInput
	if err := app.readJSON(w, r, &input); err != nil {
		app.respondWithError(w, fmt.Errorf("invalid JSON input: %v", err), http.StatusBadRequest)
		return
	}

	setting := &data.PlatformSetting{
		SettingKey:   input.SettingKey,
		SettingValue: input.SettingValue,
		ValueType:    input.ValueType,
		Description:  input.Description,
		IsActive:     input.IsActive,
		CreatedBy:    userID,
		UpdatedBy:    userID,
	}

	if err := app.Models.PlatformSetting.Insert(ctx, setting); err != nil {
		logger.Error("Create platform setting failed",
			"setting_key", input.SettingKey,
			"value_type", input.ValueType,
			"error", err,
		)
		app.respondWithError(w, err, platformSettingHTTPStatus(err))
		return
	}

	if err := app.auditPlatformSetting(ctx, userID, actionCreatePlatformSetting, "Create a platform setting", setting.ID.String()); err != nil {
		logger.Warn("Audit logging failed", "setting_id", setting.ID, "error", err)
		app.respondWithJSON(w, http.StatusPartialContent, jsonResponse{
			Error:   false,
			Message: "Platform setting created, but audit logging failed",
			Data:    setting,
		})
		return
	}

	app.respondWithJSON(w, http.StatusCreated, jsonResponse{
		Error:   false,
		Message: "Platform setting created successfully",
		Data:    setting,
	})
}

// EnsurePlatformSettingHandler idempotently creates or refreshes a platform setting.
func (app *Application) EnsurePlatformSettingHandler(w http.ResponseWriter, r *http.Request) {
	logger := app.Logger.GetLoggerWithContext(r).WithFunctionName("EnsurePlatformSettingHandler")

	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	if !app.HasPermission(ctx, permissionEnsurePlatformSetting) {
		app.respondWithError(w, errors.New("forbidden: insufficient permissions"), http.StatusForbidden)
		return
	}

	userID := app.getUserIDFromContext(ctx)
	if userID == nil {
		app.respondWithError(w, errors.New("user ID not found in context"), http.StatusUnauthorized)
		return
	}

	var input ensurePlatformSettingInput
	if err := app.readJSON(w, r, &input); err != nil {
		app.respondWithError(w, fmt.Errorf("invalid JSON input: %v", err), http.StatusBadRequest)
		return
	}

	setting, err := app.Models.PlatformSetting.Ensure(
		ctx,
		input.SettingKey,
		input.SettingValue,
		input.ValueType,
		input.Description,
		input.IsActive,
		userID,
	)
	if err != nil {
		logger.Error("Ensure platform setting failed",
			"setting_key", input.SettingKey,
			"value_type", input.ValueType,
			"error", err,
		)
		app.respondWithError(w, err, platformSettingHTTPStatus(err))
		return
	}

	if err := app.auditPlatformSetting(ctx, userID, actionEnsurePlatformSetting, "Ensure a platform setting", setting.ID.String()); err != nil {
		logger.Warn("Audit logging failed", "setting_id", setting.ID, "error", err)
		app.respondWithJSON(w, http.StatusPartialContent, jsonResponse{
			Error:   false,
			Message: "Platform setting ensured, but audit logging failed",
			Data:    setting,
		})
		return
	}

	app.respondWithJSON(w, http.StatusOK, jsonResponse{
		Error:   false,
		Message: "Platform setting ensured successfully",
		Data:    setting,
	})
}

// GetPlatformSettingByIDHandler retrieves a non-deleted platform setting by ID.
func (app *Application) GetPlatformSettingByIDHandler(w http.ResponseWriter, r *http.Request) {
	logger := app.Logger.GetLoggerWithContext(r).WithFunctionName("GetPlatformSettingByIDHandler")

	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	if !app.HasPermission(ctx, permissionReadPlatformSetting) {
		app.respondWithError(w, errors.New("forbidden: insufficient permissions"), http.StatusForbidden)
		return
	}

	settingID, err := app.parsePlatformSettingID(r)
	if err != nil {
		app.respondWithError(w, err, http.StatusBadRequest)
		return
	}

	setting, err := app.Models.PlatformSetting.GetByID(ctx, settingID)
	if err != nil {
		logger.Error("Get platform setting by ID failed", "setting_id", settingID, "error", err)
		app.respondWithError(w, err, platformSettingHTTPStatus(err))
		return
	}
	if setting == nil {
		app.respondWithError(w, errors.New("platform setting not found"), http.StatusNotFound)
		return
	}

	app.respondWithJSON(w, http.StatusOK, jsonResponse{
		Error:   false,
		Message: "Platform setting retrieved successfully",
		Data:    setting,
	})
}

// GetPlatformSettingByIDIncludingDeletedHandler retrieves a platform setting by ID including soft-deleted rows.
func (app *Application) GetPlatformSettingByIDIncludingDeletedHandler(w http.ResponseWriter, r *http.Request) {
	logger := app.Logger.GetLoggerWithContext(r).WithFunctionName("GetPlatformSettingByIDIncludingDeletedHandler")

	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	if !app.HasPermission(ctx, permissionReadPlatformSetting) {
		app.respondWithError(w, errors.New("forbidden: insufficient permissions"), http.StatusForbidden)
		return
	}

	settingID, err := app.parsePlatformSettingID(r)
	if err != nil {
		app.respondWithError(w, err, http.StatusBadRequest)
		return
	}

	setting, err := app.Models.PlatformSetting.GetByIDIncludingDeleted(ctx, settingID)
	if err != nil {
		logger.Error("Get platform setting by ID including deleted failed", "setting_id", settingID, "error", err)
		app.respondWithError(w, err, platformSettingHTTPStatus(err))
		return
	}
	if setting == nil {
		app.respondWithError(w, errors.New("platform setting not found"), http.StatusNotFound)
		return
	}

	app.respondWithJSON(w, http.StatusOK, jsonResponse{
		Error:   false,
		Message: "Platform setting retrieved successfully",
		Data:    setting,
	})
}

// GetPlatformSettingByKeyHandler retrieves a non-deleted platform setting by key.
func (app *Application) GetPlatformSettingByKeyHandler(w http.ResponseWriter, r *http.Request) {
	logger := app.Logger.GetLoggerWithContext(r).WithFunctionName("GetPlatformSettingByKeyHandler")

	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	if !app.HasPermission(ctx, permissionReadPlatformSetting) {
		app.respondWithError(w, errors.New("forbidden: insufficient permissions"), http.StatusForbidden)
		return
	}

	var input platformSettingKeyInput
	if err := app.readJSON(w, r, &input); err != nil {
		app.respondWithError(w, fmt.Errorf("invalid JSON input: %v", err), http.StatusBadRequest)
		return
	}

	setting, err := app.Models.PlatformSetting.GetByKey(ctx, input.SettingKey)
	if err != nil {
		logger.Error("Get platform setting by key failed", "setting_key", input.SettingKey, "error", err)
		app.respondWithError(w, err, platformSettingHTTPStatus(err))
		return
	}
	if setting == nil {
		app.respondWithError(w, errors.New("platform setting not found"), http.StatusNotFound)
		return
	}

	app.respondWithJSON(w, http.StatusOK, jsonResponse{
		Error:   false,
		Message: "Platform setting retrieved successfully",
		Data:    setting,
	})
}

// GetPlatformSettingByKeyIncludingDeletedHandler retrieves a platform setting by key including soft-deleted rows.
func (app *Application) GetPlatformSettingByKeyIncludingDeletedHandler(w http.ResponseWriter, r *http.Request) {
	logger := app.Logger.GetLoggerWithContext(r).WithFunctionName("GetPlatformSettingByKeyIncludingDeletedHandler")

	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	if !app.HasPermission(ctx, permissionReadPlatformSetting) {
		app.respondWithError(w, errors.New("forbidden: insufficient permissions"), http.StatusForbidden)
		return
	}

	var input platformSettingKeyInput
	if err := app.readJSON(w, r, &input); err != nil {
		app.respondWithError(w, fmt.Errorf("invalid JSON input: %v", err), http.StatusBadRequest)
		return
	}

	setting, err := app.Models.PlatformSetting.GetByKeyIncludingDeleted(ctx, input.SettingKey)
	if err != nil {
		logger.Error("Get platform setting by key including deleted failed", "setting_key", input.SettingKey, "error", err)
		app.respondWithError(w, err, platformSettingHTTPStatus(err))
		return
	}
	if setting == nil {
		app.respondWithError(w, errors.New("platform setting not found"), http.StatusNotFound)
		return
	}

	app.respondWithJSON(w, http.StatusOK, jsonResponse{
		Error:   false,
		Message: "Platform setting retrieved successfully",
		Data:    setting,
	})
}

// GetActivePlatformSettingByKeyHandler retrieves an active, non-deleted platform setting by key.
func (app *Application) GetActivePlatformSettingByKeyHandler(w http.ResponseWriter, r *http.Request) {
	logger := app.Logger.GetLoggerWithContext(r).WithFunctionName("GetActivePlatformSettingByKeyHandler")

	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	if !app.HasPermission(ctx, permissionReadPlatformSetting) {
		app.respondWithError(w, errors.New("forbidden: insufficient permissions"), http.StatusForbidden)
		return
	}

	var input platformSettingKeyInput
	if err := app.readJSON(w, r, &input); err != nil {
		app.respondWithError(w, fmt.Errorf("invalid JSON input: %v", err), http.StatusBadRequest)
		return
	}

	setting, err := app.Models.PlatformSetting.GetActiveByKey(ctx, input.SettingKey)
	if err != nil {
		logger.Error("Get active platform setting by key failed", "setting_key", input.SettingKey, "error", err)
		app.respondWithError(w, err, platformSettingHTTPStatus(err))
		return
	}
	if setting == nil {
		app.respondWithError(w, errors.New("active platform setting not found"), http.StatusNotFound)
		return
	}

	app.respondWithJSON(w, http.StatusOK, jsonResponse{
		Error:   false,
		Message: "Active platform setting retrieved successfully",
		Data:    setting,
	})
}

// ListActivePlatformSettingsHandler lists active, non-deleted platform settings.
func (app *Application) ListActivePlatformSettingsHandler(w http.ResponseWriter, r *http.Request) {
	logger := app.Logger.GetLoggerWithContext(r).WithFunctionName("ListActivePlatformSettingsHandler")

	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	if !app.HasPermission(ctx, permissionListPlatformSettings) {
		app.respondWithError(w, errors.New("forbidden: insufficient permissions"), http.StatusForbidden)
		return
	}

	settings, err := app.Models.PlatformSetting.ListActive(ctx)
	if err != nil {
		logger.Error("List active platform settings failed", "error", err)
		app.respondWithError(w, err, platformSettingHTTPStatus(err))
		return
	}

	app.respondWithJSON(w, http.StatusOK, jsonResponse{
		Error:   false,
		Message: "Active platform settings retrieved successfully",
		Data:    settings,
	})
}

// ListPlatformSettingsHandler lists non-deleted platform settings.
func (app *Application) ListPlatformSettingsHandler(w http.ResponseWriter, r *http.Request) {
	logger := app.Logger.GetLoggerWithContext(r).WithFunctionName("ListPlatformSettingsHandler")

	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	if !app.HasPermission(ctx, permissionListPlatformSettings) {
		app.respondWithError(w, errors.New("forbidden: insufficient permissions"), http.StatusForbidden)
		return
	}

	settings, err := app.Models.PlatformSetting.List(ctx)
	if err != nil {
		logger.Error("List platform settings failed", "error", err)
		app.respondWithError(w, err, platformSettingHTTPStatus(err))
		return
	}

	app.respondWithJSON(w, http.StatusOK, jsonResponse{
		Error:   false,
		Message: "Platform settings retrieved successfully",
		Data:    settings,
	})
}

// ListPlatformSettingsIncludingDeletedHandler lists platform settings including soft-deleted rows.
func (app *Application) ListPlatformSettingsIncludingDeletedHandler(w http.ResponseWriter, r *http.Request) {
	logger := app.Logger.GetLoggerWithContext(r).WithFunctionName("ListPlatformSettingsIncludingDeletedHandler")

	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	if !app.HasPermission(ctx, permissionListPlatformSettings) {
		app.respondWithError(w, errors.New("forbidden: insufficient permissions"), http.StatusForbidden)
		return
	}

	settings, err := app.Models.PlatformSetting.ListIncludingDeleted(ctx)
	if err != nil {
		logger.Error("List platform settings including deleted failed", "error", err)
		app.respondWithError(w, err, platformSettingHTTPStatus(err))
		return
	}

	app.respondWithJSON(w, http.StatusOK, jsonResponse{
		Error:   false,
		Message: "Platform settings retrieved successfully",
		Data:    settings,
	})
}

// UpdatePlatformSettingValueHandler updates setting_value, value_type, and description.
func (app *Application) UpdatePlatformSettingValueHandler(w http.ResponseWriter, r *http.Request) {
	logger := app.Logger.GetLoggerWithContext(r).WithFunctionName("UpdatePlatformSettingValueHandler")

	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	if !app.HasPermission(ctx, permissionUpdatePlatformSetting) {
		app.respondWithError(w, errors.New("forbidden: insufficient permissions"), http.StatusForbidden)
		return
	}

	userID := app.getUserIDFromContext(ctx)
	if userID == nil {
		app.respondWithError(w, errors.New("user ID not found in context"), http.StatusUnauthorized)
		return
	}

	settingID, err := app.parsePlatformSettingID(r)
	if err != nil {
		app.respondWithError(w, err, http.StatusBadRequest)
		return
	}

	var input updatePlatformSettingValueInput
	if err := app.readJSON(w, r, &input); err != nil {
		app.respondWithError(w, fmt.Errorf("invalid JSON input: %v", err), http.StatusBadRequest)
		return
	}

	setting, err := app.Models.PlatformSetting.UpdateValue(
		ctx,
		settingID,
		input.SettingValue,
		input.ValueType,
		input.Description,
		*userID,
	)
	if err != nil {
		logger.Error("Update platform setting value failed",
			"setting_id", settingID,
			"value_type", input.ValueType,
			"error", err,
		)
		app.respondWithError(w, err, platformSettingHTTPStatus(err))
		return
	}

	if err := app.auditPlatformSetting(ctx, userID, actionUpdatePlatformSettingValue, "Update platform setting value", setting.ID.String()); err != nil {
		logger.Warn("Audit logging failed", "setting_id", setting.ID, "error", err)
		app.respondWithJSON(w, http.StatusPartialContent, jsonResponse{
			Error:   false,
			Message: "Platform setting value updated, but audit logging failed",
			Data:    setting,
		})
		return
	}

	app.respondWithJSON(w, http.StatusOK, jsonResponse{
		Error:   false,
		Message: "Platform setting value updated successfully",
		Data:    setting,
	})
}

// SetPlatformSettingActiveHandler updates the active state for a platform setting.
func (app *Application) SetPlatformSettingActiveHandler(w http.ResponseWriter, r *http.Request) {
	logger := app.Logger.GetLoggerWithContext(r).WithFunctionName("SetPlatformSettingActiveHandler")

	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	userID := app.getUserIDFromContext(ctx)
	if userID == nil {
		app.respondWithError(w, errors.New("user ID not found in context"), http.StatusUnauthorized)
		return
	}

	settingID, err := app.parsePlatformSettingID(r)
	if err != nil {
		app.respondWithError(w, err, http.StatusBadRequest)
		return
	}

	var input setPlatformSettingActiveInput
	if err := app.readJSON(w, r, &input); err != nil {
		app.respondWithError(w, fmt.Errorf("invalid JSON input: %v", err), http.StatusBadRequest)
		return
	}

	requiredPermission := permissionDeactivatePlatformSetting
	if input.IsActive {
		requiredPermission = permissionActivatePlatformSetting
	}

	if !app.HasPermission(ctx, requiredPermission) {
		app.respondWithError(w, errors.New("forbidden: insufficient permissions"), http.StatusForbidden)
		return
	}

	setting, err := app.Models.PlatformSetting.SetActive(ctx, settingID, input.IsActive, *userID)
	if err != nil {
		logger.Error("Set platform setting active failed",
			"setting_id", settingID,
			"is_active", input.IsActive,
			"error", err,
		)
		app.respondWithError(w, err, platformSettingHTTPStatus(err))
		return
	}

	if err := app.auditPlatformSetting(ctx, userID, actionSetPlatformSettingActive, "Set platform setting active state", setting.ID.String()); err != nil {
		logger.Warn("Audit logging failed", "setting_id", setting.ID, "error", err)
		app.respondWithJSON(w, http.StatusPartialContent, jsonResponse{
			Error:   false,
			Message: "Platform setting active state updated, but audit logging failed",
			Data:    setting,
		})
		return
	}

	app.respondWithJSON(w, http.StatusOK, jsonResponse{
		Error:   false,
		Message: "Platform setting active state updated successfully",
		Data:    setting,
	})
}

// SoftDeletePlatformSettingHandler soft-deletes a platform setting.
func (app *Application) SoftDeletePlatformSettingHandler(w http.ResponseWriter, r *http.Request) {
	logger := app.Logger.GetLoggerWithContext(r).WithFunctionName("SoftDeletePlatformSettingHandler")

	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	if !app.HasPermission(ctx, permissionSoftDeletePlatformSetting) {
		app.respondWithError(w, errors.New("forbidden: insufficient permissions"), http.StatusForbidden)
		return
	}

	userID := app.getUserIDFromContext(ctx)
	if userID == nil {
		app.respondWithError(w, errors.New("user ID not found in context"), http.StatusUnauthorized)
		return
	}

	settingID, err := app.parsePlatformSettingID(r)
	if err != nil {
		app.respondWithError(w, err, http.StatusBadRequest)
		return
	}

	if err := app.Models.PlatformSetting.SoftDelete(ctx, settingID, *userID); err != nil {
		logger.Error("Soft delete platform setting failed", "setting_id", settingID, "error", err)
		app.respondWithError(w, err, platformSettingHTTPStatus(err))
		return
	}

	if err := app.auditPlatformSetting(ctx, userID, actionSoftDeletePlatformSetting, "Soft delete a platform setting", settingID.String()); err != nil {
		logger.Warn("Audit logging failed", "setting_id", settingID, "error", err)
		app.respondWithJSON(w, http.StatusPartialContent, jsonResponse{
			Error:   false,
			Message: "Platform setting soft-deleted, but audit logging failed",
			Data:    settingID,
		})
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// HardDeletePlatformSettingHandler permanently deletes a platform setting.
//
// This is administrative/pre-startup cleanup only. Ordinary removal must use
// SoftDeletePlatformSettingHandler.
func (app *Application) HardDeletePlatformSettingHandler(w http.ResponseWriter, r *http.Request) {
	logger := app.Logger.GetLoggerWithContext(r).WithFunctionName("HardDeletePlatformSettingHandler")

	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	if !app.HasPermission(ctx, permissionHardDeletePlatformSetting) {
		app.respondWithError(w, errors.New("forbidden: insufficient permissions"), http.StatusForbidden)
		return
	}

	userID := app.getUserIDFromContext(ctx)
	if userID == nil {
		app.respondWithError(w, errors.New("user ID not found in context"), http.StatusUnauthorized)
		return
	}

	settingID, err := app.parsePlatformSettingID(r)
	if err != nil {
		app.respondWithError(w, err, http.StatusBadRequest)
		return
	}

	hardDeleteEnabled, err := app.platformSettingsHardDeleteEnabled(ctx)
	if err != nil {
		logger.Error("Check platform settings hard-delete policy failed", "error", err)
		app.respondWithError(w, err, platformSettingHTTPStatus(err))
		return
	}
	if !hardDeleteEnabled {
		app.respondWithError(w, errors.New("platform settings hard delete is disabled"), http.StatusForbidden)
		return
	}

	if err := app.Models.PlatformSetting.HardDelete(ctx, settingID); err != nil {
		logger.Error("Hard delete platform setting failed", "setting_id", settingID, "error", err)
		app.respondWithError(w, err, platformSettingHTTPStatus(err))
		return
	}

	if err := app.auditPlatformSetting(ctx, userID, actionHardDeletePlatformSetting, "Hard delete a platform setting", settingID.String()); err != nil {
		logger.Warn("Audit logging failed", "setting_id", settingID, "error", err)
		app.respondWithJSON(w, http.StatusPartialContent, jsonResponse{
			Error:   false,
			Message: "Platform setting hard-deleted, but audit logging failed",
			Data:    settingID,
		})
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// ExistsActivePlatformSettingByKeyHandler checks whether an active setting exists.
func (app *Application) ExistsActivePlatformSettingByKeyHandler(w http.ResponseWriter, r *http.Request) {
	logger := app.Logger.GetLoggerWithContext(r).WithFunctionName("ExistsActivePlatformSettingByKeyHandler")

	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	if !app.HasPermission(ctx, permissionReadPlatformSetting) {
		app.respondWithError(w, errors.New("forbidden: insufficient permissions"), http.StatusForbidden)
		return
	}

	var input platformSettingKeyInput
	if err := app.readJSON(w, r, &input); err != nil {
		app.respondWithError(w, fmt.Errorf("invalid JSON input: %v", err), http.StatusBadRequest)
		return
	}

	exists, err := app.Models.PlatformSetting.ExistsActiveByKey(ctx, input.SettingKey)
	if err != nil {
		logger.Error("Check active platform setting existence failed", "setting_key", input.SettingKey, "error", err)
		app.respondWithError(w, err, platformSettingHTTPStatus(err))
		return
	}

	app.respondWithJSON(w, http.StatusOK, jsonResponse{
		Error:   false,
		Message: "Active platform setting existence checked successfully",
		Data: platformSettingExistsResponse{
			SettingKey: data.NormalizePlatformSettingKey(input.SettingKey),
			Exists:     exists,
		},
	})
}