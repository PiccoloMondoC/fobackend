// Package main contains HTTP handlers for the Platform backend API.
//
// sdworkspace/sdbackend/internal/server/cmd/api/audit_logs.go
//
// GTM:
//
//	Layer: 3.1 API / Governance Foundation
//	Release Class: SPINE
//	Reason:
//	  Audit log handlers expose the platform's governance,
//	  compliance, administrative accountability, and forensic
//	  audit capabilities. They coordinate the release-critical
//	  audit infrastructure implemented by the data layer while
//	  preserving authorization, observability, and immutable
//	  audit semantics required for production. Audit metadata
//	  resolution is centralized to eliminate duplicated
//	  get-or-create logic and prevent nil metadata dereferences.
//
// SPINE Rule:
//
//	Keep compiling.
//	Keep production-ready.
//	Preserve immutable audit-log semantics.
//	Preserve append -> archive -> restore -> retention purge lifecycle.
//	Preserve dynamic audit action/entity resolution.
//	Preserve authorization boundaries.
//	Preserve trusted-context identifier extraction.
//	Preserve governance observability.
//	Never construct a data.AuditLog with a caller-supplied OccurredAt;
//	that field is database-owned through AuditLogModel.Insert.
//	Block deployment if this file breaks build, API correctness,
//	audit integrity, authorization, archival lifecycle,
//	metadata resolution, or governance infrastructure.
package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/PiccoloMondoC/sdworkspace/sdbackend/internal/data"

	"github.com/google/uuid"
)

const (
	auditLogEntityTypeName        = "audit_log"
	auditLogEntityTypeDescription = "Audit log entity used to track system and user actions"

	actionEntityTypeName        = "action"
	actionEntityTypeDescription = "Action metadata used by audit records"

	entityTypeEntityTypeName        = "entity_type"
	entityTypeEntityTypeDescription = "Entity type metadata used by audit records"
)

type archivedAuditLogListResponse struct {
	Logs  []*data.AuditLog `json:"logs"`
	Count int              `json:"count"`
}

// resolveAuditMetadata resolves or creates the Action and EntityType metadata
// required for one audit record.
//
// A nil error guarantees that both returned UUIDs are non-zero. Callers must not
// build a data.AuditLog when this helper returns an error.
func (app *Application) resolveAuditMetadata(
	ctx context.Context,
	actionName string,
	actionDescription string,
	entityTypeName string,
	entityTypeDescription string,
) (uuid.UUID, uuid.UUID, error) {
	action, err := app.Models.Action.GetByName(ctx, actionName)
	if err != nil || action == nil {
		actionID, createErr := app.Models.Action.CreateIfNotExists(
			ctx,
			actionName,
			actionDescription,
		)
		if createErr != nil {
			return uuid.Nil, uuid.Nil, fmt.Errorf(
				"resolve audit action %q: %w",
				actionName,
				createErr,
			)
		}

		if actionID == uuid.Nil {
			return uuid.Nil, uuid.Nil, fmt.Errorf(
				"resolve audit action %q: empty ID returned",
				actionName,
			)
		}

		action = &data.Action{
			ID: actionID,
		}
	}

	if action.ID == uuid.Nil {
		return uuid.Nil, uuid.Nil, fmt.Errorf(
			"resolve audit action %q: empty ID",
			actionName,
		)
	}

	entityType, err := app.Models.EntityType.GetByName(ctx, entityTypeName)
	if err != nil || entityType == nil {
		entityTypeID, createErr := app.Models.EntityType.CreateIfNotExists(
			ctx,
			entityTypeName,
			entityTypeDescription,
		)
		if createErr != nil {
			return uuid.Nil, uuid.Nil, fmt.Errorf(
				"resolve audit entity type %q: %w",
				entityTypeName,
				createErr,
			)
		}

		if entityTypeID == uuid.Nil {
			return uuid.Nil, uuid.Nil, fmt.Errorf(
				"resolve audit entity type %q: empty ID returned",
				entityTypeName,
			)
		}

		entityType = &data.EntityType{
			ID: entityTypeID,
		}
	}

	if entityType.ID == uuid.Nil {
		return uuid.Nil, uuid.Nil, fmt.Errorf(
			"resolve audit entity type %q: empty ID",
			entityTypeName,
		)
	}

	return action.ID, entityType.ID, nil
}

// insertGovernanceAudit performs best-effort insertion of an audit record.
//
// The primary administrative operation has already completed when this helper
// is called. Metadata or audit insertion failure therefore does not panic and
// does not invalidate the completed read or restore operation.
func (app *Application) insertGovernanceAudit(
	ctx context.Context,
	userID *uuid.UUID,
	actionName string,
	actionDescription string,
	entityTypeName string,
	entityTypeDescription string,
	entityID string,
) error {
	entityID = strings.TrimSpace(entityID)
	if entityID == "" {
		return errors.New("audit entity ID is required")
	}

	actionID, entityTypeID, err := app.resolveAuditMetadata(
		ctx,
		actionName,
		actionDescription,
		entityTypeName,
		entityTypeDescription,
	)
	if err != nil {
		return err
	}

	auditLog := &data.AuditLog{
		ID:           uuid.New(),
		UserID:       userID,
		ActionID:     actionID,
		EntityTypeID: entityTypeID,
		EntityID:     entityID,
	}

	if err := app.Models.AuditLog.Insert(ctx, auditLog); err != nil {
		return fmt.Errorf("insert governance audit log: %w", err)
	}

	return nil
}

// GetAuditLogByIDHandler retrieves one live audit log using the trusted
// audit-log ID installed in request context by route middleware.
func (app *Application) GetAuditLogByIDHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	logger := app.Logger.
		GetLoggerWithContext(r).
		WithFunctionName("GetAuditLogByIDHandler")

	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	if !app.HasPermission(ctx, "read_audit_log") &&
		!app.HasAnyRole(ctx, "admin", "internal_moderator") {
		app.respondWithError(
			w,
			errors.New("forbidden: insufficient permissions"),
			http.StatusForbidden,
		)
		return
	}

	auditLogID := app.getAuditLogIDFromContext(ctx)
	if auditLogID == nil || *auditLogID == uuid.Nil {
		app.respondWithError(
			w,
			errors.New("audit log ID not found in context"),
			http.StatusBadRequest,
		)
		return
	}

	auditLog, err := app.Models.AuditLog.GetByID(ctx, *auditLogID)
	if err != nil {
		if errors.Is(err, data.ErrAuditLogNotFound) {
			app.respondWithError(
				w,
				errors.New("audit log not found"),
				http.StatusNotFound,
			)
			return
		}

		logger.Error(
			"Retrieve audit log failed",
			"audit_log_id", *auditLogID,
			"error", err,
		)
		app.respondWithError(
			w,
			fmt.Errorf("failed to retrieve audit log: %w", err),
			http.StatusInternalServerError,
		)
		return
	}

	if auditLog == nil {
		app.respondWithError(
			w,
			errors.New("audit log not found"),
			http.StatusNotFound,
		)
		return
	}

	userID := app.getUserIDFromContext(ctx)

	if err := app.insertGovernanceAudit(
		ctx,
		userID,
		"read_audit_log",
		"Retrieve a specific audit log entry",
		auditLogEntityTypeName,
		auditLogEntityTypeDescription,
		auditLog.ID.String(),
	); err != nil {
		logger.Warn(
			"Audit log access succeeded but recording access failed",
			"audit_log_id", auditLog.ID,
			"error", err,
		)
	}

	logger.Info(
		"Audit log retrieved",
		"audit_log_id", auditLog.ID,
		"user_id", userID,
	)

	app.respondWithJSON(w, http.StatusOK, jsonResponse{
		Error:   false,
		Message: "Audit log retrieved successfully",
		Data:    auditLog,
	})
}

// GetAuditLogByUserIDHandler retrieves live audit logs for the trusted target
// user ID installed in request context.
func (app *Application) GetAuditLogByUserIDHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	logger := app.Logger.
		GetLoggerWithContext(r).
		WithFunctionName("GetAuditLogByUserIDHandler")

	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	if !app.HasPermission(ctx, "read_audit_log") &&
		!app.HasAnyRole(ctx, "admin", "internal_moderator") {
		app.respondWithError(
			w,
			errors.New("forbidden: insufficient permissions"),
			http.StatusForbidden,
		)
		return
	}

	targetUserID := app.getTargetUserIDFromContext(ctx)
	if targetUserID == nil || *targetUserID == uuid.Nil {
		app.respondWithError(
			w,
			errors.New("target user ID not found in context"),
			http.StatusBadRequest,
		)
		return
	}

	auditLogs, err := app.Models.AuditLog.GetByUserID(ctx, *targetUserID)
	if err != nil {
		logger.Error(
			"Retrieve audit logs by user failed",
			"target_user_id", *targetUserID,
			"error", err,
		)
		app.respondWithError(
			w,
			fmt.Errorf("failed to retrieve audit logs: %w", err),
			http.StatusInternalServerError,
		)
		return
	}

	actorUserID := app.getUserIDFromContext(ctx)

	if err := app.insertGovernanceAudit(
		ctx,
		actorUserID,
		"read_audit_log_by_user",
		"Retrieve audit logs for a specified user",
		auditLogEntityTypeName,
		auditLogEntityTypeDescription,
		targetUserID.String(),
	); err != nil {
		logger.Warn(
			"Audit logs by user were retrieved but recording access failed",
			"target_user_id", *targetUserID,
			"error", err,
		)
	}

	logger.Info(
		"Audit logs retrieved by user",
		"actor_user_id", actorUserID,
		"target_user_id", *targetUserID,
		"count", len(auditLogs),
	)

	app.respondWithJSON(w, http.StatusOK, jsonResponse{
		Error:   false,
		Message: "Audit logs retrieved successfully",
		Data:    auditLogs,
	})
}

// GetAuditLogByEntityIDHandler retrieves live audit logs for a trusted entity
// type ID and entity ID installed in request context.
func (app *Application) GetAuditLogByEntityIDHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	logger := app.Logger.
		GetLoggerWithContext(r).
		WithFunctionName("GetAuditLogByEntityIDHandler")

	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	if !app.HasPermission(ctx, "read_audit_log") &&
		!app.HasAnyRole(ctx, "admin", "internal_moderator") {
		app.respondWithError(
			w,
			errors.New("forbidden: insufficient permissions"),
			http.StatusForbidden,
		)
		return
	}

	entityTypeIDRaw := strings.TrimSpace(
		app.getContextValueAsString(ctx, ctxEntityTypeID),
	)
	entityID := strings.TrimSpace(
		app.getContextValueAsString(ctx, ctxEntityID),
	)

	if entityTypeIDRaw == "" || entityID == "" {
		app.respondWithError(
			w,
			errors.New("entity_type_id or entity_id missing in context"),
			http.StatusBadRequest,
		)
		return
	}

	targetEntityTypeID, err := uuid.Parse(entityTypeIDRaw)
	if err != nil {
		app.respondWithError(
			w,
			fmt.Errorf("invalid entity_type_id: %w", err),
			http.StatusBadRequest,
		)
		return
	}

	auditLogs, err := app.Models.AuditLog.GetByEntity(
		ctx,
		targetEntityTypeID,
		entityID,
	)
	if err != nil {
		logger.Error(
			"Retrieve audit logs by entity failed",
			"entity_type_id", targetEntityTypeID,
			"entity_id", entityID,
			"error", err,
		)
		app.respondWithError(
			w,
			fmt.Errorf("failed to retrieve audit logs: %w", err),
			http.StatusInternalServerError,
		)
		return
	}

	userID := app.getUserIDFromContext(ctx)

	auditEntityID := fmt.Sprintf(
		"%s:%s",
		targetEntityTypeID.String(),
		entityID,
	)

	if err := app.insertGovernanceAudit(
		ctx,
		userID,
		"read_audit_log_by_entity",
		"Retrieve audit logs for a specified entity",
		auditLogEntityTypeName,
		auditLogEntityTypeDescription,
		auditEntityID,
	); err != nil {
		logger.Warn(
			"Audit logs by entity were retrieved but recording access failed",
			"entity_type_id", targetEntityTypeID,
			"entity_id", entityID,
			"error", err,
		)
	}

	logger.Info(
		"Audit logs retrieved by entity",
		"entity_type_id", targetEntityTypeID,
		"entity_id", entityID,
		"user_id", userID,
		"count", len(auditLogs),
	)

	app.respondWithJSON(w, http.StatusOK, jsonResponse{
		Error:   false,
		Message: "Audit logs retrieved successfully",
		Data:    auditLogs,
	})
}

// GetAuditLogsByTimeRangeHandler retrieves live audit logs within an inclusive
// RFC3339 time range supplied through start and end query parameters.
func (app *Application) GetAuditLogsByTimeRangeHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	logger := app.Logger.
		GetLoggerWithContext(r).
		WithFunctionName("GetAuditLogsByTimeRangeHandler")

	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	if !app.HasPermission(ctx, "read_audit_logs_by_time_range") &&
		!app.HasAnyRole(ctx, "admin", "internal_moderator") {
		app.respondWithError(
			w,
			errors.New("forbidden: insufficient permissions"),
			http.StatusForbidden,
		)
		return
	}

	startRaw := strings.TrimSpace(r.URL.Query().Get("start"))
	endRaw := strings.TrimSpace(r.URL.Query().Get("end"))

	if startRaw == "" {
		app.respondWithError(
			w,
			errors.New("start query parameter is required"),
			http.StatusBadRequest,
		)
		return
	}

	if endRaw == "" {
		app.respondWithError(
			w,
			errors.New("end query parameter is required"),
			http.StatusBadRequest,
		)
		return
	}

	start, err := time.Parse(time.RFC3339, startRaw)
	if err != nil {
		app.respondWithError(
			w,
			fmt.Errorf("invalid start time: RFC3339 format required: %w", err),
			http.StatusBadRequest,
		)
		return
	}

	end, err := time.Parse(time.RFC3339, endRaw)
	if err != nil {
		app.respondWithError(
			w,
			fmt.Errorf("invalid end time: RFC3339 format required: %w", err),
			http.StatusBadRequest,
		)
		return
	}

	if end.Before(start) {
		app.respondWithError(
			w,
			errors.New("end time cannot precede start time"),
			http.StatusBadRequest,
		)
		return
	}

	auditLogs, err := app.Models.AuditLog.GetByTimeRange(ctx, start, end)
	if err != nil {
		logger.Error(
			"Retrieve audit logs by time range failed",
			"start", start,
			"end", end,
			"error", err,
		)
		app.respondWithError(
			w,
			fmt.Errorf("failed to retrieve audit logs: %w", err),
			http.StatusInternalServerError,
		)
		return
	}

	userID := app.getUserIDFromContext(ctx)

	auditEntityID := fmt.Sprintf(
		"%s->%s",
		start.Format(time.RFC3339),
		end.Format(time.RFC3339),
	)

	if err := app.insertGovernanceAudit(
		ctx,
		userID,
		"read_audit_logs_by_time_range",
		"Retrieve audit logs within a time range",
		auditLogEntityTypeName,
		auditLogEntityTypeDescription,
		auditEntityID,
	); err != nil {
		logger.Warn(
			"Audit logs by time range were retrieved but recording access failed",
			"start", start,
			"end", end,
			"error", err,
		)
	}

	logger.Info(
		"Audit logs retrieved by time range",
		"start", start,
		"end", end,
		"user_id", userID,
		"count", len(auditLogs),
	)

	app.respondWithJSON(w, http.StatusOK, jsonResponse{
		Error:   false,
		Message: "Audit logs retrieved successfully",
		Data:    auditLogs,
	})
}

// RestoreArchivedLogsHandler restores selected archived audit logs to the live
// audit_logs table while preserving their original occurred_at timestamps.
func (app *Application) RestoreArchivedLogsHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	logger := app.Logger.
		GetLoggerWithContext(r).
		WithFunctionName("RestoreArchivedLogsHandler")

	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	if !app.HasPermission(ctx, "restore_archived_audit_logs") &&
		!app.HasAnyRole(ctx, "admin", "internal_moderator") {
		app.respondWithError(
			w,
			errors.New("forbidden: insufficient permissions"),
			http.StatusForbidden,
		)
		return
	}

	var input struct {
		IDs []string `json:"ids"`
	}

	if err := app.readJSON(w, r, &input); err != nil {
		app.respondWithError(
			w,
			fmt.Errorf("invalid JSON input: %w", err),
			http.StatusBadRequest,
		)
		return
	}

	if len(input.IDs) == 0 {
		app.respondWithError(
			w,
			errors.New("at least one archived audit log ID is required"),
			http.StatusBadRequest,
		)
		return
	}

	ids := make([]uuid.UUID, 0, len(input.IDs))
	seen := make(map[uuid.UUID]struct{}, len(input.IDs))

	for _, rawID := range input.IDs {
		rawID = strings.TrimSpace(rawID)
		if rawID == "" {
			app.respondWithError(
				w,
				errors.New("archived audit log IDs cannot be empty"),
				http.StatusBadRequest,
			)
			return
		}

		id, err := uuid.Parse(rawID)
		if err != nil {
			app.respondWithError(
				w,
				fmt.Errorf("invalid archived audit log ID %q: %w", rawID, err),
				http.StatusBadRequest,
			)
			return
		}

		if _, exists := seen[id]; exists {
			continue
		}

		seen[id] = struct{}{}
		ids = append(ids, id)
	}

	if len(ids) == 0 {
		app.respondWithError(
			w,
			errors.New("at least one unique archived audit log ID is required"),
			http.StatusBadRequest,
		)
		return
	}

	if err := app.Models.AuditLog.RestoreArchivedLogs(ctx, ids); err != nil {
		if errors.Is(err, data.ErrArchivedAuditLogNotFound) {
			app.respondWithError(
				w,
				errors.New("no matching archived audit logs were found"),
				http.StatusNotFound,
			)
			return
		}

		logger.Error(
			"Restore archived audit logs failed",
			"count", len(ids),
			"error", err,
		)
		app.respondWithError(
			w,
			fmt.Errorf("failed to restore archived audit logs: %w", err),
			http.StatusInternalServerError,
		)
		return
	}

	userID := app.getUserIDFromContext(ctx)

	if err := app.insertGovernanceAudit(
		ctx,
		userID,
		"restore_archived_audit_logs",
		"Restore audit logs from archive",
		auditLogEntityTypeName,
		auditLogEntityTypeDescription,
		"bulk_restore:"+uuid.NewString(),
	); err != nil {
		logger.Warn(
			"Archived audit logs restored but recording restore failed",
			"count", len(ids),
			"error", err,
		)
	}

	logger.Info(
		"Archived audit logs restored",
		"count", len(ids),
		"user_id", userID,
	)

	app.respondWithJSON(w, http.StatusOK, jsonResponse{
		Error:   false,
		Message: "Archived audit logs restored successfully",
		Data:    len(ids),
	})
}

// GetArchivedAuditLogByEntityHandler retrieves archived audit logs for a trusted
// entity type ID and entity ID installed in request context.
func (app *Application) GetArchivedAuditLogByEntityHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	logger := app.Logger.
		GetLoggerWithContext(r).
		WithFunctionName("GetArchivedAuditLogByEntityHandler")

	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	if !app.HasPermission(ctx, "read_archived_audit_log") &&
		!app.HasAnyRole(ctx, "admin", "internal_moderator") {
		app.respondWithError(
			w,
			errors.New("forbidden: insufficient permissions"),
			http.StatusForbidden,
		)
		return
	}

	targetEntityTypeID := app.getEntityTypeIDFromContext(ctx)
	entityID := strings.TrimSpace(
		app.getContextValueAsString(ctx, ctxEntityID),
	)

	if targetEntityTypeID == nil ||
		*targetEntityTypeID == uuid.Nil ||
		entityID == "" {
		app.respondWithError(
			w,
			errors.New("entity_type_id or entity_id missing in context"),
			http.StatusBadRequest,
		)
		return
	}

	auditLogs, err := app.Models.AuditLog.GetArchivedByEntity(
		ctx,
		*targetEntityTypeID,
		entityID,
	)
	if err != nil {
		logger.Error(
			"Retrieve archived audit logs by entity failed",
			"entity_type_id", *targetEntityTypeID,
			"entity_id", entityID,
			"error", err,
		)
		app.respondWithError(
			w,
			fmt.Errorf("failed to retrieve archived audit logs: %w", err),
			http.StatusInternalServerError,
		)
		return
	}

	userID := app.getUserIDFromContext(ctx)

	auditEntityID := fmt.Sprintf(
		"%s:%s",
		targetEntityTypeID.String(),
		entityID,
	)

	if err := app.insertGovernanceAudit(
		ctx,
		userID,
		"read_archived_audit_log_by_entity",
		"Retrieve archived audit logs for a specified entity",
		auditLogEntityTypeName,
		auditLogEntityTypeDescription,
		auditEntityID,
	); err != nil {
		logger.Warn(
			"Archived audit logs were retrieved but recording access failed",
			"entity_type_id", *targetEntityTypeID,
			"entity_id", entityID,
			"error", err,
		)
	}

	logger.Info(
		"Archived audit logs retrieved by entity",
		"entity_type_id", *targetEntityTypeID,
		"entity_id", entityID,
		"user_id", userID,
		"count", len(auditLogs),
	)

	app.respondWithJSON(w, http.StatusOK, jsonResponse{
		Error:   false,
		Message: "Archived audit logs retrieved successfully",
		Data:    auditLogs,
	})
}

// GetArchivedAuditLogsByTimeRangeHandler retrieves archived audit logs within
// an inclusive RFC3339 time range supplied in the JSON request body.
func (app *Application) GetArchivedAuditLogsByTimeRangeHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	logger := app.Logger.
		GetLoggerWithContext(r).
		WithFunctionName("GetArchivedAuditLogsByTimeRangeHandler")

	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	if !app.HasPermission(ctx, "read_archived_audit_logs") &&
		!app.HasAnyRole(ctx, "admin", "internal_moderator") {
		app.respondWithError(
			w,
			errors.New("forbidden: insufficient permissions"),
			http.StatusForbidden,
		)
		return
	}

	var input struct {
		Start string `json:"start"`
		End   string `json:"end"`
	}

	if err := app.readJSON(w, r, &input); err != nil {
		app.respondWithError(
			w,
			fmt.Errorf("invalid JSON input: %w", err),
			http.StatusBadRequest,
		)
		return
	}

	startRaw := strings.TrimSpace(input.Start)
	endRaw := strings.TrimSpace(input.End)

	if startRaw == "" || endRaw == "" {
		app.respondWithError(
			w,
			errors.New("start and end are required"),
			http.StatusBadRequest,
		)
		return
	}

	start, err := time.Parse(time.RFC3339, startRaw)
	if err != nil {
		app.respondWithError(
			w,
			fmt.Errorf("invalid start time: RFC3339 format required: %w", err),
			http.StatusBadRequest,
		)
		return
	}

	end, err := time.Parse(time.RFC3339, endRaw)
	if err != nil {
		app.respondWithError(
			w,
			fmt.Errorf("invalid end time: RFC3339 format required: %w", err),
			http.StatusBadRequest,
		)
		return
	}

	if end.Before(start) {
		app.respondWithError(
			w,
			errors.New("end time cannot precede start time"),
			http.StatusBadRequest,
		)
		return
	}

	auditLogs, err := app.Models.AuditLog.GetArchivedByTimeRange(
		ctx,
		start,
		end,
	)
	if err != nil {
		logger.Error(
			"Retrieve archived audit logs by time range failed",
			"start", start,
			"end", end,
			"error", err,
		)
		app.respondWithError(
			w,
			fmt.Errorf("failed to retrieve archived audit logs: %w", err),
			http.StatusInternalServerError,
		)
		return
	}

	response := archivedAuditLogListResponse{
		Logs:  auditLogs,
		Count: len(auditLogs),
	}

	userID := app.getUserIDFromContext(ctx)

	auditEntityID := fmt.Sprintf(
		"%s->%s",
		start.Format(time.RFC3339),
		end.Format(time.RFC3339),
	)

	if err := app.insertGovernanceAudit(
		ctx,
		userID,
		"read_archived_audit_logs",
		"Retrieve archived audit logs within a time range",
		auditLogEntityTypeName,
		auditLogEntityTypeDescription,
		auditEntityID,
	); err != nil {
		logger.Warn(
			"Archived audit logs were retrieved but recording access failed",
			"start", start,
			"end", end,
			"error", err,
		)
	}

	logger.Info(
		"Archived audit logs retrieved by time range",
		"start", start,
		"end", end,
		"user_id", userID,
		"count", len(auditLogs),
	)

	app.respondWithJSON(w, http.StatusOK, jsonResponse{
		Error:   false,
		Message: "Archived audit logs retrieved successfully",
		Data:    response,
	})
}

// GetEntityTypeByIDHandler retrieves one entity type using the trusted ID
// installed in request context.
func (app *Application) GetEntityTypeByIDHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	logger := app.Logger.
		GetLoggerWithContext(r).
		WithFunctionName("GetEntityTypeByIDHandler")

	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	if !app.HasPermission(ctx, "read_entity_type") {
		app.respondWithError(
			w,
			errors.New("forbidden: insufficient permissions"),
			http.StatusForbidden,
		)
		return
	}

	entityTypeID := app.getEntityTypeIDFromContext(ctx)
	if entityTypeID == nil || *entityTypeID == uuid.Nil {
		app.respondWithError(
			w,
			errors.New("entity type ID not found in context"),
			http.StatusBadRequest,
		)
		return
	}

	entityType, err := app.Models.EntityType.GetByID(ctx, *entityTypeID)
	if err != nil {
		logger.Error(
			"Retrieve entity type failed",
			"entity_type_id", *entityTypeID,
			"error", err,
		)
		app.respondWithError(
			w,
			fmt.Errorf("failed to retrieve entity type: %w", err),
			http.StatusInternalServerError,
		)
		return
	}

	if entityType == nil {
		app.respondWithError(
			w,
			errors.New("entity type not found"),
			http.StatusNotFound,
		)
		return
	}

	userID := app.getUserIDFromContext(ctx)

	if err := app.insertGovernanceAudit(
		ctx,
		userID,
		"read_entity_type",
		"Retrieve an entity type by ID",
		entityTypeEntityTypeName,
		entityTypeEntityTypeDescription,
		entityType.ID.String(),
	); err != nil {
		logger.Warn(
			"Entity type was retrieved but recording access failed",
			"entity_type_id", entityType.ID,
			"error", err,
		)
	}

	logger.Info(
		"Entity type retrieved",
		"entity_type_id", entityType.ID,
		"user_id", userID,
	)

	app.respondWithJSON(w, http.StatusOK, jsonResponse{
		Error:   false,
		Message: "Entity type retrieved successfully",
		Data:    entityType,
	})
}

// GetAllEntityTypesHandler retrieves all configured entity types.
func (app *Application) GetAllEntityTypesHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	logger := app.Logger.
		GetLoggerWithContext(r).
		WithFunctionName("GetAllEntityTypesHandler")

	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	if !app.HasPermission(ctx, "read_entity_type") {
		app.respondWithError(
			w,
			errors.New("forbidden: insufficient permissions"),
			http.StatusForbidden,
		)
		return
	}

	entityTypes, err := app.Models.EntityType.GetAll(ctx)
	if err != nil {
		logger.Error(
			"Retrieve all entity types failed",
			"error", err,
		)
		app.respondWithError(
			w,
			fmt.Errorf("failed to retrieve entity types: %w", err),
			http.StatusInternalServerError,
		)
		return
	}

	userID := app.getUserIDFromContext(ctx)

	if err := app.insertGovernanceAudit(
		ctx,
		userID,
		"list_entity_types",
		"List all entity types",
		entityTypeEntityTypeName,
		entityTypeEntityTypeDescription,
		"all",
	); err != nil {
		logger.Warn(
			"Entity types were retrieved but recording access failed",
			"error", err,
		)
	}

	logger.Info(
		"Entity types retrieved",
		"count", len(entityTypes),
		"user_id", userID,
	)

	app.respondWithJSON(w, http.StatusOK, jsonResponse{
		Error:   false,
		Message: "Entity types retrieved successfully",
		Data:    entityTypes,
	})
}

// GetActionByIDHandler retrieves one audit action using the trusted action ID
// installed in request context.
func (app *Application) GetActionByIDHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	logger := app.Logger.
		GetLoggerWithContext(r).
		WithFunctionName("GetActionByIDHandler")

	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	if !app.HasPermission(ctx, "read_action") {
		app.respondWithError(
			w,
			errors.New("forbidden: insufficient permissions"),
			http.StatusForbidden,
		)
		return
	}

	actionID := app.getActionIDFromContext(ctx)
	if actionID == nil || *actionID == uuid.Nil {
		app.respondWithError(
			w,
			errors.New("action ID not found in context"),
			http.StatusBadRequest,
		)
		return
	}

	action, err := app.Models.Action.GetByID(ctx, *actionID)
	if err != nil {
		logger.Error(
			"Retrieve action failed",
			"action_id", *actionID,
			"error", err,
		)
		app.respondWithError(
			w,
			fmt.Errorf("failed to retrieve action: %w", err),
			http.StatusInternalServerError,
		)
		return
	}

	if action == nil {
		app.respondWithError(
			w,
			errors.New("action not found"),
			http.StatusNotFound,
		)
		return
	}

	userID := app.getUserIDFromContext(ctx)

	if err := app.insertGovernanceAudit(
		ctx,
		userID,
		"read_action",
		"Retrieve an audit action by ID",
		actionEntityTypeName,
		actionEntityTypeDescription,
		action.ID.String(),
	); err != nil {
		logger.Warn(
			"Action was retrieved but recording access failed",
			"action_id", action.ID,
			"error", err,
		)
	}

	logger.Info(
		"Action retrieved",
		"action_id", action.ID,
		"user_id", userID,
	)

	app.respondWithJSON(w, http.StatusOK, jsonResponse{
		Error:   false,
		Message: "Action retrieved successfully",
		Data:    action,
	})
}

// GetAllActionsHandler retrieves all configured audit actions.
func (app *Application) GetAllActionsHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	logger := app.Logger.
		GetLoggerWithContext(r).
		WithFunctionName("GetAllActionsHandler")

	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	if !app.HasPermission(ctx, "read_action") {
		app.respondWithError(
			w,
			errors.New("forbidden: insufficient permissions"),
			http.StatusForbidden,
		)
		return
	}

	actions, err := app.Models.Action.GetAll(ctx)
	if err != nil {
		logger.Error(
			"Retrieve all actions failed",
			"error", err,
		)
		app.respondWithError(
			w,
			fmt.Errorf("failed to retrieve actions: %w", err),
			http.StatusInternalServerError,
		)
		return
	}

	userID := app.getUserIDFromContext(ctx)

	if err := app.insertGovernanceAudit(
		ctx,
		userID,
		"list_actions",
		"List all audit actions",
		actionEntityTypeName,
		actionEntityTypeDescription,
		"all",
	); err != nil {
		logger.Warn(
			"Actions were retrieved but recording access failed",
			"error", err,
		)
	}

	logger.Info(
		"Actions retrieved",
		"count", len(actions),
		"user_id", userID,
	)

	app.respondWithJSON(w, http.StatusOK, jsonResponse{
		Error:   false,
		Message: "Actions retrieved successfully",
		Data:    actions,
	})
}