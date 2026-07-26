// Package services contains business logic for internal operations such as moderation,
// automation, audit logging, and data synchronization. It is used by async routines
// and internal system workflows, not exposed via public API routes.
//
// sdworkspace/sdbackend/internal/services/user_profiles_internal.go
//
// GTM:
//
//	Layer: 2.2 Identity / Auth Domain
//	Release Class: SPINE
//	Reason:
//	  User profile internal services are release-critical identity, trust,
//	  safety, and account lifecycle infrastructure. They preserve profile
//	  moderation, handle validation and normalization, profile ownership
//	  resolution, and audit visibility required by the v1 Future Offering
//	  release spine.
//
// SPINE Rule:
//
//	Keep compiling.
//	Keep production-ready.
//	Preserve profile auto-flagging behavior.
//	Preserve moderation audit trail behavior.
//	Preserve handle validation and normalization parity with the data layer.
//	Preserve profile ownership resolution correctness.
//	Preserve normalized social-link moderation checks.
//	Block deployment if this file breaks build, moderation,
//	handle validation, ownership checks, or audit integrity.
package services

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/PiccoloMondoC/sdworkspace/sdbackend/internal/data"
	"github.com/PiccoloMondoC/sdworkspace/sdbackend/internal/utils"

	"github.com/google/uuid"
)

const (
	userProfileAuditActionAutoFlagProfile = "auto_flag_user_profile"
	userProfileAuditActionValidateHandle  = "validate_user_handle"
	userProfileAuditActionResolveOwner    = "resolve_profile_owner"
	userProfileAuditEntityType            = "user_profile"

	userProfileAutoFlagReason = "Auto-flagged by system: policy violation"

	userProfileModerationBatchLimit  = 100
	userProfileModerationBatchOffset = 0
)

func (s *Service) getSystemUserID() uuid.UUID {
	return uuid.MustParse("00000000-0000-0000-0000-000000000000")
}

func (s *Service) CheckAndAutoFlagProfileInternal(ctx context.Context, userID uuid.UUID) {
	logger := s.Logger.GetLoggerWithContextFromContext(ctx).
		WithFunctionName("CheckAndAutoFlagProfileInternal")

	if userID == uuid.Nil {
		logger.Warn("missing user_id for profile auto-flag check")
		return
	}

	profile, err := s.Models.UserProfile.GetByUserID(ctx, userID)
	if err != nil {
		logger.Error("load profile failed", "user_id", userID, "error", err)
		return
	}
	if profile == nil {
		logger.Info("profile not found for auto-flag check", "user_id", userID)
		return
	}

	if !shouldFlagProfile(profile) {
		logger.Info("profile passed moderation check", "user_id", userID)
		return
	}

	if err := s.Models.UserProfile.FlagUserProfile(ctx, userID, true, userProfileAutoFlagReason); err != nil {
		logger.Error("failed to flag profile", "user_id", userID, "error", err)
		return
	}

	if err := utils.InsertAutoModerationAudit(ctx, s.Models, s.getSystemUserID(), userID); err != nil {
		logger.Warn("audit log failed after flagging", "user_id", userID, "error", err)
	}

	logger.Warn("profile auto-flagged for violation", "user_id", userID)
}

func (s *Service) AutoFlagUserProfilesInternal(ctx context.Context) error {
	logger := s.Logger.GetLoggerWithContextFromContext(ctx).
		WithFunctionName("AutoFlagUserProfilesInternal")

	ctx, cancel := context.WithTimeout(ctx, s.Cfg.DBTimeout)
	defer cancel()

	profiles, err := s.Models.UserProfile.GetAll(
		ctx,
		userProfileModerationBatchLimit,
		userProfileModerationBatchOffset,
	)
	if err != nil {
		logger.Error("failed to fetch user profiles", "error", err)
		return fmt.Errorf("user profile query failed: %w", err)
	}

	if len(profiles) == 0 {
		logger.Info("no profiles found for moderation review")
		return nil
	}

	action, entityType := s.resolveUserProfileAuditMetadata(
		ctx,
		userProfileAuditActionAutoFlagProfile,
		"Automatically flag a user profile for moderation review",
	)

	requesterID := s.getUserIDFromContext(ctx)
	if requesterID == nil {
		systemUserID := s.getSystemUserID()
		requesterID = &systemUserID
	}

	var flaggedCount int

	for _, profile := range profiles {
		if profile == nil || profile.UserID == uuid.Nil {
			continue
		}

		if !shouldFlagProfile(profile) {
			continue
		}

		if err := s.Models.UserProfile.FlagUserProfile(ctx, profile.UserID, true, userProfileAutoFlagReason); err != nil {
			logger.Warn("failed to flag profile", "user_id", profile.UserID, "error", err)
			continue
		}

		s.insertUserProfileAuditLog(ctx, requesterID, action, entityType, profile.UserID.String())
		flaggedCount++
	}

	logger.Info("auto moderation completed", "total_flagged", flaggedCount)
	return nil
}

func shouldFlagProfile(p *data.UserProfile) bool {
	if p == nil {
		return false
	}

	banned := []string{"scam", "nazi", "xxx", "admin", "root", "support"}

	checkText := func(text string) bool {
		lower := strings.ToLower(strings.TrimSpace(text))
		if lower == "" {
			return false
		}

		for _, word := range banned {
			if strings.Contains(lower, word) {
				return true
			}
		}

		return false
	}

	if p.UserHandle != nil && checkText(*p.UserHandle) {
		return true
	}
	if p.Bio != nil && checkText(*p.Bio) {
		return true
	}
	if p.Phone != nil && strings.Contains(strings.TrimSpace(*p.Phone), "123456") {
		return true
	}

	for _, link := range p.SocialLinks {
		if checkText(link.URL) {
			return true
		}
		if link.Handle != nil && checkText(*link.Handle) {
			return true
		}
		if checkText(link.PlatformName) {
			return true
		}
	}

	if !p.CreatedAt.IsZero() && time.Since(p.CreatedAt) < 10*time.Second {
		return true
	}

	return false
}

func (s *Service) ValidateAndNormalizeHandleInternal(ctx context.Context, handle string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	logger := s.Logger.GetLoggerWithContextFromContext(ctx).
		WithFunctionName("ValidateAndNormalizeHandleInternal")

	normalized := strings.ToLower(strings.TrimSpace(handle))
	if normalized == "" {
		return "", errors.New("user handle is required")
	}

	if err := s.Models.UserProfile.ValidateUserHandle(ctx, normalized); err != nil {
		logger.Warn("handle validation failed", "user_handle", normalized, "error", err)
		return "", err
	}

	action, entityType := s.resolveUserProfileAuditMetadata(
		ctx,
		userProfileAuditActionValidateHandle,
		"Validate availability and format of a user handle",
	)

	requesterID := s.getUserIDFromContext(ctx)
	if requesterID != nil {
		s.insertUserProfileAuditLog(ctx, requesterID, action, entityType, normalized)
	}

	logger.Info("handle validation passed", "normalized_handle", normalized)
	return normalized, nil
}

func (s *Service) ResolveProfileOwnerInternal(
	ctx context.Context,
	inputHandle string,
	inputUserID uuid.UUID,
) (bool, error) {
	logger := s.Logger.GetLoggerWithContextFromContext(ctx).
		WithFunctionName("ResolveProfileOwnerInternal")

	requesterID := s.getUserIDFromContext(ctx)
	if requesterID == nil {
		logger.Warn("missing requester ID in context")
		return false, errors.New("unauthorized: requester ID missing")
	}

	var resolvedUserID uuid.UUID

	switch {
	case strings.TrimSpace(inputHandle) != "":
		normalized := strings.ToLower(strings.TrimSpace(inputHandle))

		userID, err := s.Models.UserProfile.GetUserIDByHandle(ctx, normalized)
		if err != nil {
			if errors.Is(err, data.ErrUserHandleNotFound) {
				logger.Info("handle not found", "user_handle", normalized)
				return false, nil
			}

			logger.Error("failed to resolve user ID from handle", "user_handle", normalized, "error", err)
			return false, err
		}

		resolvedUserID = userID

	case inputUserID != uuid.Nil:
		exists, err := s.Models.UserProfile.ExistsByUserID(ctx, inputUserID)
		if err != nil {
			logger.Error("failed checking profile existence", "user_id", inputUserID, "error", err)
			return false, err
		}
		if !exists {
			logger.Info("profile user ID not found", "user_id", inputUserID)
			return false, nil
		}

		resolvedUserID = inputUserID

	default:
		logger.Warn("missing both handle and user ID")
		return false, errors.New("must provide handle or user ID")
	}

	isOwner := resolvedUserID == *requesterID

	action, entityType := s.resolveUserProfileAuditMetadata(
		ctx,
		userProfileAuditActionResolveOwner,
		"Resolve ownership of a user handle or ID",
	)

	s.insertUserProfileAuditLog(ctx, requesterID, action, entityType, resolvedUserID.String())

	return isOwner, nil
}

func (s *Service) getUserIDFromContext(ctx context.Context) *uuid.UUID {
	val, ok := ctx.Value("user_id").(uuid.UUID)
	if !ok || val == uuid.Nil {
		return nil
	}

	return &val
}

func (s *Service) resolveUserProfileAuditMetadata(
	ctx context.Context,
	actionName string,
	actionDescription string,
) (*data.Action, *data.EntityType) {
	logger := s.Logger.GetLoggerWithContextFromContext(ctx).
		WithFunctionName("resolveUserProfileAuditMetadata")

	action, err := s.Models.Action.GetByName(ctx, actionName)
	if err != nil || action == nil {
		id, createErr := s.Models.Action.CreateIfNotExists(ctx, actionName, actionDescription)
		if createErr != nil {
			logger.Warn("failed to resolve audit action", "action", actionName, "error", createErr)
		} else {
			action = &data.Action{ID: id}
		}
	}

	entityType, err := s.Models.EntityType.GetByName(ctx, userProfileAuditEntityType)
	if err != nil || entityType == nil {
		id, createErr := s.Models.EntityType.CreateIfNotExists(
			ctx,
			userProfileAuditEntityType,
			"User profile entity",
		)
		if createErr != nil {
			logger.Warn("failed to resolve audit entity type", "entity_type", userProfileAuditEntityType, "error", createErr)
		} else {
			entityType = &data.EntityType{ID: id}
		}
	}

	return action, entityType
}

func (s *Service) insertUserProfileAuditLog(
	ctx context.Context,
	requesterID *uuid.UUID,
	action *data.Action,
	entityType *data.EntityType,
	entityID string,
) {
	logger := s.Logger.GetLoggerWithContextFromContext(ctx).
		WithFunctionName("insertUserProfileAuditLog")

	if requesterID == nil || *requesterID == uuid.Nil || action == nil || entityType == nil || strings.TrimSpace(entityID) == "" {
		return
	}

	audit := data.AuditLog{
		ID:           uuid.New(),
		UserID:       requesterID,
		ActionID:     action.ID,
		EntityTypeID: entityType.ID,
		EntityID:     entityID,
	}

	if err := s.Models.AuditLog.Insert(ctx, &audit); err != nil {
		logger.Error("audit log insert failed", "entity_id", entityID, "error", err)
	}
}
