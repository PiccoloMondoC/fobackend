// Package main provides HTTP handlers for platform setting governance.
//
// focodebase/fobackend/internal/server/cmd/api/platform_settings.go
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

	"github.com/PiccoloMondoC/focodebase/fobackend/internal/data"
 "github.com/PiccoloMondoC/focodebase/fobackend/internal/services"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

const (
	platformSettingEntityType            = "platform_setting"
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
	IsActive *bool `json:"is_active"`
}

type platformSettingExistsResponse struct {
	SettingKey string `json:"setting_key"`
	Exists     bool   `json:"exists"`
}

func platformSettingHTTPStatus(err error) int {
 switch {
 case err==nil:return http.StatusOK
 case errors.Is(err,data.ErrPlatformSettingAlreadyExists):return http.StatusConflict
 case errors.Is(err,data.ErrPlatformSettingNotFound):return http.StatusNotFound
 case errors.Is(err,data.ErrPlatformSettingActorNotFound):return http.StatusUnprocessableEntity
 case errors.Is(err,data.ErrPlatformSettingTypeMismatch):return http.StatusConflict
 case errors.Is(err,data.ErrPlatformSettingInvalidInput):return http.StatusBadRequest
 default:return http.StatusInternalServerError
 }
}
func safePlatformSettingError(err error) error {
 switch {
 case errors.Is(err,data.ErrPlatformSettingAlreadyExists):return errors.New("platform setting already exists")
 case errors.Is(err,data.ErrPlatformSettingNotFound):return errors.New("platform setting not found")
 case errors.Is(err,data.ErrPlatformSettingTypeMismatch):return errors.New("platform setting value type cannot be changed")
 case errors.Is(err,data.ErrPlatformSettingInvalidInput):return errors.New("invalid platform setting input")
 default:return errors.New("internal server error")
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


// ---------------------------------------------------------------------------
// Platform Settings administration governance
// ---------------------------------------------------------------------------

type platformSettingMutationKind string

const (
	platformSettingMutationCreate      platformSettingMutationKind = "create"
	platformSettingMutationEnsure      platformSettingMutationKind = "ensure"
	platformSettingMutationUpdateValue platformSettingMutationKind = "update_value"
	platformSettingMutationSetActive   platformSettingMutationKind = "set_active"
	platformSettingMutationSoftDelete  platformSettingMutationKind = "soft_delete"
	platformSettingMutationHardDelete  platformSettingMutationKind = "hard_delete"
)

// protectedPlatformSettingKeys are the controls that govern Platform
// Settings administration itself. They can never be deleted (deleting one
// would make recovery impossible through the API) and only a Super Admin may
// change them.
var protectedPlatformSettingKeys = map[string]struct{}{
	adminConsolePlatformSettingsAdminEnabledKey: {},
	platformSettingsHardDeleteEnabledKey:        {},
}

type platformSettingMutation struct {
	Kind     platformSettingMutationKind
	Key      string
	Activate bool
	NewValue json.RawMessage
}

func isJSONTrue(raw json.RawMessage) bool {
	return bytes.Equal(bytes.TrimSpace(raw), []byte("true"))
}

// decidePlatformSettingMutation is the pure Platform Settings governance
// decision. It returns "" when the mutation may proceed, or a stable code.
//
//   - Protected governance keys cannot be soft- or hard-deleted, and only a
//     Super Admin may otherwise change them.
//   - With administration enabled, every other permitted mutation proceeds.
//   - With administration disabled (switch false, inactive, or missing),
//     the only mutation allowed is the recovery path: a Super Admin
//     re-activating the switch or setting it to true. Disabling therefore
//     never creates a permanent dead end, and an Admin cannot undo a Super
//     Admin's decision to freeze settings.
func decidePlatformSettingMutation(enabled,super bool,m platformSettingMutation) string {
 return services.DecideSettingMutation(enabled,super,services.SettingOperation(m.Kind),m.Key,m.Activate,m.NewValue)
}

// authorizePlatformSettingMutation applies decidePlatformSettingMutation with
// the current switch state and the caller's standing, both read fresh.
// It writes the refusal and returns false when the mutation must not run.
func (app *Application) respondWithSettingGovernanceError(w http.ResponseWriter,r *http.Request,err error) {
 switch {
 case errors.Is(err,services.ErrSettingsAdministrationDisabled):
  app.respondWithErrorCode(w,http.StatusForbidden,"platform_settings_administration_disabled","platform settings administration is disabled")
 case errors.Is(err,services.ErrSettingsHardDeleteDisabled):
  app.respondWithErrorCode(w,http.StatusForbidden,"platform_settings_hard_delete_disabled","platform settings hard delete is disabled")
 case errors.Is(err,services.ErrProtectedPlatformSetting):
  app.respondWithErrorCode(w,http.StatusConflict,"protected_platform_setting","this platform control cannot be deleted")
 case errors.Is(err,data.ErrPlatformSettingInvalidInput):
  app.respondWithErrorCode(w,http.StatusBadRequest,"invalid_request","invalid platform setting input")
 case errors.Is(err,data.ErrPlatformSettingAlreadyExists):
  app.respondWithErrorCode(w,http.StatusConflict,"setting_exists","platform setting already exists")
 case errors.Is(err,data.ErrPlatformSettingNotFound):
  app.respondWithErrorCode(w,http.StatusNotFound,"setting_not_found","platform setting not found")
 case errors.Is(err,data.ErrPlatformSettingTypeMismatch):
  app.respondWithErrorCode(w,http.StatusConflict,"setting_type_mismatch","platform setting value type cannot be changed")
 default:
  app.respondWithGovernanceError(w,r,"platform_setting_mutation",err)
 }
}

// CreatePlatformSettingHandler creates a new platform setting.
func (app *Application) CreatePlatformSettingHandler(w http.ResponseWriter, r *http.Request) {
 ctx,cancel:=context.WithTimeout(r.Context(),cfgTimeout);defer cancel()
 actor,ok:=app.governanceActor(w,ctx);if !ok{return}
 req:=services.SettingGovernanceRequest{ActorID:actor,Operation:services.SettingCreate}
 var input createPlatformSettingInput
 if err:=app.readJSON(w,r,&input);err!=nil {app.respondWithErrorCode(w,http.StatusBadRequest,"invalid_request","invalid request body");return}
 req.Key=input.SettingKey;req.Value=input.SettingValue;req.ValueType=input.ValueType;req.Description=input.Description;req.Active=input.IsActive
 setting,err:=app.InternalServices.MutatePlatformSettingInternal(ctx,req)
 if err!=nil {app.respondWithSettingGovernanceError(w,r,err);return}
 app.respondWithJSON(w,http.StatusCreated,jsonResponse{Error:false,Message:"Platform setting created successfully",Data:setting})
}

// EnsurePlatformSettingHandler idempotently creates or refreshes a platform setting.
func (app *Application) EnsurePlatformSettingHandler(w http.ResponseWriter, r *http.Request) {
 ctx,cancel:=context.WithTimeout(r.Context(),cfgTimeout);defer cancel()
 actor,ok:=app.governanceActor(w,ctx);if !ok{return}
 req:=services.SettingGovernanceRequest{ActorID:actor,Operation:services.SettingEnsure}
 var input ensurePlatformSettingInput
 if err:=app.readJSON(w,r,&input);err!=nil {app.respondWithErrorCode(w,http.StatusBadRequest,"invalid_request","invalid request body");return}
 req.Key=input.SettingKey;req.Value=input.SettingValue;req.ValueType=input.ValueType;req.Description=input.Description;req.Active=input.IsActive
 setting,err:=app.InternalServices.MutatePlatformSettingInternal(ctx,req)
 if err!=nil {app.respondWithSettingGovernanceError(w,r,err);return}
 app.respondWithJSON(w,http.StatusOK,jsonResponse{Error:false,Message:"Platform setting ensured successfully",Data:setting})
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
		app.respondWithError(w, safePlatformSettingError(err), platformSettingHTTPStatus(err))
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
		app.respondWithError(w, safePlatformSettingError(err), platformSettingHTTPStatus(err))
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
		app.respondWithErrorCode(w, http.StatusBadRequest, "invalid_request", "invalid JSON input")
		return
	}

	setting, err := app.Models.PlatformSetting.GetByKey(ctx, input.SettingKey)
	if err != nil {
		logger.Error("Get platform setting by key failed", "setting_key", input.SettingKey, "error", err)
		app.respondWithError(w, safePlatformSettingError(err), platformSettingHTTPStatus(err))
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
		app.respondWithErrorCode(w, http.StatusBadRequest, "invalid_request", "invalid JSON input")
		return
	}

	setting, err := app.Models.PlatformSetting.GetByKeyIncludingDeleted(ctx, input.SettingKey)
	if err != nil {
		logger.Error("Get platform setting by key including deleted failed", "setting_key", input.SettingKey, "error", err)
		app.respondWithError(w, safePlatformSettingError(err), platformSettingHTTPStatus(err))
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
		app.respondWithErrorCode(w, http.StatusBadRequest, "invalid_request", "invalid JSON input")
		return
	}

	setting, err := app.Models.PlatformSetting.GetActiveByKey(ctx, input.SettingKey)
	if err != nil {
		logger.Error("Get active platform setting by key failed", "setting_key", input.SettingKey, "error", err)
		app.respondWithError(w, safePlatformSettingError(err), platformSettingHTTPStatus(err))
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
		app.respondWithError(w, safePlatformSettingError(err), platformSettingHTTPStatus(err))
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
		app.respondWithError(w, safePlatformSettingError(err), platformSettingHTTPStatus(err))
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
		app.respondWithError(w, safePlatformSettingError(err), platformSettingHTTPStatus(err))
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
 ctx,cancel:=context.WithTimeout(r.Context(),cfgTimeout);defer cancel()
 actor,ok:=app.governanceActor(w,ctx);if !ok{return}
 req:=services.SettingGovernanceRequest{ActorID:actor,Operation:services.SettingUpdate}
 id,err:=app.parsePlatformSettingID(r);if err!=nil {app.respondWithErrorCode(w,http.StatusBadRequest,"invalid_request","invalid platform setting ID");return};req.ID=id
 var input updatePlatformSettingValueInput
 if err:=app.readJSON(w,r,&input);err!=nil {app.respondWithErrorCode(w,http.StatusBadRequest,"invalid_request","invalid request body");return}
 req.Value=input.SettingValue;req.ValueType=input.ValueType;req.Description=input.Description
 setting,err:=app.InternalServices.MutatePlatformSettingInternal(ctx,req)
 if err!=nil {app.respondWithSettingGovernanceError(w,r,err);return}
 app.respondWithJSON(w,http.StatusOK,jsonResponse{Error:false,Message:"Platform setting updated successfully",Data:setting})
}

// SetPlatformSettingActiveHandler updates the active state for a platform setting.
func (app *Application) SetPlatformSettingActiveHandler(w http.ResponseWriter, r *http.Request) {
 ctx,cancel:=context.WithTimeout(r.Context(),cfgTimeout);defer cancel()
 actor,ok:=app.governanceActor(w,ctx);if !ok{return}
 req:=services.SettingGovernanceRequest{ActorID:actor,Operation:services.SettingActivate}
 id,err:=app.parsePlatformSettingID(r);if err!=nil {app.respondWithErrorCode(w,http.StatusBadRequest,"invalid_request","invalid platform setting ID");return};req.ID=id
 var input setPlatformSettingActiveInput
 if err:=app.readJSON(w,r,&input);err!=nil {app.respondWithErrorCode(w,http.StatusBadRequest,"invalid_request","invalid request body");return}
 if input.IsActive==nil{app.respondWithErrorCode(w,http.StatusBadRequest,"invalid_request","is_active is required");return}
 req.Active=*input.IsActive
 setting,err:=app.InternalServices.MutatePlatformSettingInternal(ctx,req)
 if err!=nil {app.respondWithSettingGovernanceError(w,r,err);return}
 app.respondWithJSON(w,http.StatusOK,jsonResponse{Error:false,Message:"Platform setting updated successfully",Data:setting})
}

// SoftDeletePlatformSettingHandler soft-deletes a platform setting.
func (app *Application) SoftDeletePlatformSettingHandler(w http.ResponseWriter, r *http.Request) {
 ctx,cancel:=context.WithTimeout(r.Context(),cfgTimeout);defer cancel()
 actor,ok:=app.governanceActor(w,ctx);if !ok{return}
 req:=services.SettingGovernanceRequest{ActorID:actor,Operation:services.SettingSoftDelete}
 id,err:=app.parsePlatformSettingID(r);if err!=nil {app.respondWithErrorCode(w,http.StatusBadRequest,"invalid_request","invalid platform setting ID");return};req.ID=id
 setting,err:=app.InternalServices.MutatePlatformSettingInternal(ctx,req)
 if err!=nil {app.respondWithSettingGovernanceError(w,r,err);return}
 _=setting
 w.WriteHeader(http.StatusNoContent)
}

// HardDeletePlatformSettingHandler permanently deletes a platform setting.
//
// This is administrative/pre-startup cleanup only. Ordinary removal must use
// SoftDeletePlatformSettingHandler.
func (app *Application) HardDeletePlatformSettingHandler(w http.ResponseWriter, r *http.Request) {
 ctx,cancel:=context.WithTimeout(r.Context(),cfgTimeout);defer cancel()
 actor,ok:=app.governanceActor(w,ctx);if !ok{return}
 req:=services.SettingGovernanceRequest{ActorID:actor,Operation:services.SettingHardDelete}
 id,err:=app.parsePlatformSettingID(r);if err!=nil {app.respondWithErrorCode(w,http.StatusBadRequest,"invalid_request","invalid platform setting ID");return};req.ID=id
 setting,err:=app.InternalServices.MutatePlatformSettingInternal(ctx,req)
 if err!=nil {app.respondWithSettingGovernanceError(w,r,err);return}
 _=setting
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
		app.respondWithErrorCode(w, http.StatusBadRequest, "invalid_request", "invalid JSON input")
		return
	}

	exists, err := app.Models.PlatformSetting.ExistsActiveByKey(ctx, input.SettingKey)
	if err != nil {
		logger.Error("Check active platform setting existence failed", "setting_key", input.SettingKey, "error", err)
		app.respondWithError(w, safePlatformSettingError(err), platformSettingHTTPStatus(err))
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
