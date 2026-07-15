// Package utils provides reusable helpers for audit logging across services.
//
// sdworkspace/sdbackend/internal/utils/auditutil.go
package utils

import (
	"context"

	"github.com/PiccoloMondoC/sdworkspace/sdbackend/internal/data"

	"github.com/google/uuid"
)

// InsertAutoModerationAudit inserts a structured audit log entry when a profile is auto-flagged.
// - Uses synthetic system actor ID
// - Dynamically resolves or creates audit action and entity type definitions
// - Returns error only on critical failure (not recoverable silently)
func InsertAutoModerationAudit(
	ctx context.Context,
	models *data.Models,
	systemUserID uuid.UUID,
	targetUserID uuid.UUID,
) error {
	const actionName = "auto_flag_user_profile"
	const entityTypeName = "user_profile"

	// Resolve or create action
	action, err := models.Action.GetByName(ctx, actionName)
	if err != nil || action == nil {
		id, createErr := models.Action.CreateIfNotExists(ctx, actionName, "Automated profile moderation")
		if createErr != nil {
			return createErr
		}
		action = &data.Action{ID: id}
	}

	// Resolve or create entity type
	entityType, err := models.EntityType.GetByName(ctx, entityTypeName)
	if err != nil || entityType == nil {
		id, createErr := models.EntityType.CreateIfNotExists(ctx, entityTypeName, "User profile entity")
		if createErr != nil {
			return createErr
		}
		entityType = &data.EntityType{ID: id}
	}

	audit := &data.AuditLog{
		ID:           uuid.New(),
		UserID:       nil, // system action
		ActionID:     action.ID,
		EntityTypeID: entityType.ID,
		EntityID:     targetUserID.String(),
	}

	return models.AuditLog.Insert(ctx, audit)
}
