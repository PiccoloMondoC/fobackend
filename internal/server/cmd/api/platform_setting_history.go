// Package main provides HTTP handlers for privileged historical review of
// platform setting value transitions.
//
// focodebase/fobackend/internal/server/cmd/api/platform_setting_history.go
//
// GTM:
//
//	Layer: 2.1 Database / Governance Foundation
//	Release Class: SPINE
//	Reason:
//	  platform_setting_history handler surface is release-critical
//	  configuration-governance infrastructure. It provides privileged,
//	  read-only administrative access to immutable value transitions for
//	  canonical platform settings.
//
//	  History rows are inserted internally and atomically with the associated
//	  platform-setting value mutation. This handler surface does not create,
//	  update, delete, restore, or purge history rows.
//
//	  This table records platform-setting value changes. It is not a complete
//	  lifecycle-event ledger for activation, deactivation, restoration, or
//	  deletion because those operation types are not represented by the
//	  current schema.
//
// SPINE Rule:
//
//	Keep compiling.
//	Keep production-ready.
//	Preserve privileged authorization at every boundary.
//	Preserve read-only platform-setting history semantics.
//	Preserve bounded, deterministic history reads.
//	Preserve explicit not-found behavior for single-record reads.
//	Preserve empty-array behavior for history-list reads.
//	Preserve handler and data-layer separation.
//	Preserve setting-value opacity in logs, traces, metrics, and errors.
//	Never log previous_value or new_value.
//	Do not expose public routes for this domain.
//	Do not introduce history mutation endpoints.
//	Block deployment if this file breaks build, privileged historical review,
//	pagination safety, or configuration-value confidentiality.
package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

const (
	permissionReadPlatformSettingHistory = "read_platform_setting_history"
	permissionListPlatformSettingHistory = "list_platform_setting_history"
)

// parsePlatformSettingHistoryID parses and validates the
// platformSettingHistoryID route parameter.
func (app *Application) parsePlatformSettingHistoryID(
	r *http.Request,
) (uuid.UUID, error) {
	raw := strings.TrimSpace(
		chi.URLParam(r, "platformSettingHistoryID"),
	)
	if raw == "" {
		return uuid.Nil, errors.New(
			"platform setting history ID is required",
		)
	}

	id, err := uuid.Parse(raw)
	if err != nil || id == uuid.Nil {
		return uuid.Nil, errors.New(
			"invalid platform setting history ID",
		)
	}

	return id, nil
}

// parsePlatformSettingHistoryPagination parses optional limit and offset query
// parameters.
//
// Omitted values remain zero so the data layer applies its canonical default.
// The data layer remains authoritative for the maximum permitted limit.
func parsePlatformSettingHistoryPagination(
	r *http.Request,
) (int, int, error) {
	limit, err := parsePlatformSettingHistoryQueryInteger(
		r,
		"limit",
	)
	if err != nil {
		return 0, 0, err
	}

	offset, err := parsePlatformSettingHistoryQueryInteger(
		r,
		"offset",
	)
	if err != nil {
		return 0, 0, err
	}

	return limit, offset, nil
}

func parsePlatformSettingHistoryQueryInteger(
	r *http.Request,
	name string,
) (int, error) {
	raw := strings.TrimSpace(r.URL.Query().Get(name))
	if raw == "" {
		return 0, nil
	}

	value, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf(
			"%s must be a valid integer",
			name,
		)
	}
	if value < 0 {
		return 0, fmt.Errorf(
			"%s must not be negative",
			name,
		)
	}

	return value, nil
}

// GetPlatformSettingHistoryByIDHandler retrieves one immutable
// platform-setting history row by canonical ID.
func (app *Application) GetPlatformSettingHistoryByIDHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	logger := app.Logger.
		GetLoggerWithContext(r).
		WithFunctionName("GetPlatformSettingHistoryByIDHandler")

	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	if !app.HasPermission(
		ctx,
		permissionReadPlatformSettingHistory,
	) {
		app.respondWithError(
			w,
			errors.New("forbidden: insufficient permissions"),
			http.StatusForbidden,
		)
		return
	}

	historyID, err := app.parsePlatformSettingHistoryID(r)
	if err != nil {
		app.respondWithError(
			w,
			err,
			http.StatusBadRequest,
		)
		return
	}

	history, err := app.Models.PlatformSettingHistory.GetByID(
		ctx,
		historyID,
	)
	if err != nil {
		logger.Error(
			"Get platform setting history by ID failed",
			"history_id", historyID,
			"error", err,
		)
		app.respondWithError(
			w,
			err,
			platformSettingHTTPStatus(err),
		)
		return
	}
	if history == nil {
		app.respondWithError(
			w,
			errors.New("platform setting history not found"),
			http.StatusNotFound,
		)
		return
	}

	app.respondWithJSON(
		w,
		http.StatusOK,
		jsonResponse{
			Error:   false,
			Message: "Platform setting history retrieved successfully",
			Data:    history,
		},
	)
}

// ListPlatformSettingHistoryByPlatformSettingIDHandler retrieves a bounded,
// newest-first page of value-history rows for one platform setting.
func (app *Application) ListPlatformSettingHistoryByPlatformSettingIDHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	logger := app.Logger.
		GetLoggerWithContext(r).
		WithFunctionName(
			"ListPlatformSettingHistoryByPlatformSettingIDHandler",
		)

	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	if !app.HasPermission(
		ctx,
		permissionListPlatformSettingHistory,
	) {
		app.respondWithError(
			w,
			errors.New("forbidden: insufficient permissions"),
			http.StatusForbidden,
		)
		return
	}

	platformSettingID, err := app.parsePlatformSettingID(r)
	if err != nil {
		app.respondWithError(
			w,
			err,
			http.StatusBadRequest,
		)
		return
	}

	limit, offset, err :=
		parsePlatformSettingHistoryPagination(r)
	if err != nil {
		app.respondWithError(
			w,
			err,
			http.StatusBadRequest,
		)
		return
	}

	history, err :=
		app.Models.PlatformSettingHistory.
			ListByPlatformSettingID(
				ctx,
				platformSettingID,
				limit,
				offset,
			)
	if err != nil {
		logger.Error(
			"List platform setting history by platform setting ID failed",
			"platform_setting_id", platformSettingID,
			"limit", limit,
			"offset", offset,
			"error", err,
		)
		app.respondWithError(
			w,
			err,
			platformSettingHTTPStatus(err),
		)
		return
	}

	app.respondWithJSON(
		w,
		http.StatusOK,
		jsonResponse{
			Error:   false,
			Message: "Platform setting history retrieved successfully",
			Data:    history,
		},
	)
}

// ListPlatformSettingHistoryBySettingKeyHandler retrieves a bounded,
// newest-first page of value-history rows for one canonical platform-setting
// key.
func (app *Application) ListPlatformSettingHistoryBySettingKeyHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	logger := app.Logger.
		GetLoggerWithContext(r).
		WithFunctionName(
			"ListPlatformSettingHistoryBySettingKeyHandler",
		)

	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	if !app.HasPermission(
		ctx,
		permissionListPlatformSettingHistory,
	) {
		app.respondWithError(
			w,
			errors.New("forbidden: insufficient permissions"),
			http.StatusForbidden,
		)
		return
	}

	var input platformSettingKeyInput
	if err := app.readJSON(w, r, &input); err != nil {
		app.respondWithError(
			w,
			fmt.Errorf("invalid JSON input: %v", err),
			http.StatusBadRequest,
		)
		return
	}

	limit, offset, err :=
		parsePlatformSettingHistoryPagination(r)
	if err != nil {
		app.respondWithError(
			w,
			err,
			http.StatusBadRequest,
		)
		return
	}

	history, err :=
		app.Models.PlatformSettingHistory.
			ListBySettingKey(
				ctx,
				input.SettingKey,
				limit,
				offset,
			)
	if err != nil {
		logger.Error(
			"List platform setting history by setting key failed",
			"setting_key", input.SettingKey,
			"limit", limit,
			"offset", offset,
			"error", err,
		)
		app.respondWithError(
			w,
			err,
			platformSettingHTTPStatus(err),
		)
		return
	}

	app.respondWithJSON(
		w,
		http.StatusOK,
		jsonResponse{
			Error:   false,
			Message: "Platform setting history retrieved successfully",
			Data:    history,
		},
	)
}
