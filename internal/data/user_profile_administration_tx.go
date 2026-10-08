// Package data provides models and database access methods for user profiles and related entities.
//
// focodebase/fobackend/internal/data/user_profile_administration_tx.go
//
// CE transaction composition retains the canonical profile validators,
// handle maintenance and moderation ledger; the service owns commit/audit.
package data
import (
 "context"
 "errors"
 "fmt"
 "strings"
 "time"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
)
func (m *UserProfileModel) UpdateTx(ctx context.Context,tx pgx.Tx, profile *UserProfile) error {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("UpdateUserProfile")

	if profile == nil {
		err := errors.New("profile is required")
		logger.Error("validation failed", err)
		return ErrUserProfileInvalidInput
	}

	if profile.UserID == uuid.Nil {
		err := errors.New("user ID is required")
		logger.Error("validation failed", err)
		return ErrUserProfileInvalidInput
	}

	firstName, err := normalizeRequiredProfileString(profile.FirstName, "first_name")
	if err != nil {
		logger.Error("validation failed", err, "user_id", profile.UserID)
		return ErrUserProfileInvalidInput
	}

	lastName, err := normalizeRequiredProfileString(profile.LastName, "last_name")
	if err != nil {
		logger.Error("validation failed", err, "user_id", profile.UserID)
		return ErrUserProfileInvalidInput
	}

	handle, err := normalizeOptionalUserHandle(profile.UserHandle)
	if err != nil {
		logger.Error("validation failed", err, "user_id", profile.UserID)
		return ErrUserProfileInvalidInput
	}

	phone := normalizeOptionalString(profile.Phone)
	avatarURL := normalizeOptionalString(profile.AvatarURL)
	bio := normalizeOptionalString(profile.Bio)
	location := normalizeOptionalString(profile.Location)
	website := normalizeOptionalString(profile.Website)
	company := normalizeOptionalString(profile.Company)
	moderationNotes := normalizeOptionalString(profile.ModerationNotes)

	preferredContact := strings.ToLower(strings.TrimSpace(profile.PreferredContact))
	if preferredContact == "" {
		preferredContact = "email"
	}
	if err := validatePreferredContact(preferredContact); err != nil {
		logger.Error("validation failed", err, "user_id", profile.UserID)
		return ErrUserProfileInvalidInput
	}


	var existingHandle *string
	lockQuery := `
		SELECT user_handle
		FROM user_profiles
		WHERE user_id = $1
		  AND deleted_at IS NULL
		FOR UPDATE
	`
	if err := tx.QueryRow(ctx, lockQuery, profile.UserID).Scan(&existingHandle); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			logger.Warn("user profile not found", "user_id", profile.UserID)
			return ErrUserProfileNotFound
		}
		logger.Error("load existing handle failed", err, "user_id", profile.UserID)
		return fmt.Errorf("load existing user profile handle: %w", err)
	}

	updateQuery := `
		UPDATE user_profiles
		SET
			first_name = $1,
			last_name = $2,
			user_handle = $3,
			phone = $4,
			avatar_url = $5,
			bio = $6,
			location = $7,
			website = $8,
			company = $9,
			preferred_contact = $10,
			moderation_notes = $11,
			updated_at = NOW()
		WHERE user_id = $12
		  AND deleted_at IS NULL
		RETURNING updated_at, deleted_at
	`

	var deletedAt *time.Time
	if err := tx.QueryRow(
		ctx,
		updateQuery,
		firstName,
		lastName,
		handle,
		phone,
		avatarURL,
		bio,
		location,
		website,
		company,
		preferredContact,
		moderationNotes,
		profile.UserID,
	).Scan(&profile.UpdatedAt, &deletedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			logger.Warn("user profile not found during update", "user_id", profile.UserID)
			return ErrUserProfileNotFound
		}
		logger.Error("update user profile failed", err, "user_id", profile.UserID)
		return fmt.Errorf("update user profile: %w", err)
	}

	oldHandle := ""
	if existingHandle != nil {
		oldHandle = *existingHandle
	}
	newHandle := ""
	if handle != nil {
		newHandle = *handle
	}

	if oldHandle != newHandle {
		if oldHandle != "" {
			if err := m.deleteGlobalHandleTx(ctx, tx, oldHandle, "user"); err != nil {
				logger.Error("delete old global handle failed", err, "user_id", profile.UserID, "old_handle", oldHandle)
				return fmt.Errorf("delete old global handle: %w", err)
			}
		}

		if newHandle != "" {
			if err := m.insertGlobalHandleTx(ctx, tx, newHandle, "user", profile.UserID); err != nil {
				logger.Error("insert new global handle failed", err, "user_id", profile.UserID, "new_handle", newHandle)
				return fmt.Errorf("insert new global handle: %w", err)
			}
		}
	}


	profile.FirstName = &firstName
	profile.LastName = &lastName
	profile.UserHandle = handle
	profile.Phone = phone
	profile.AvatarURL = avatarURL
	profile.Bio = bio
	profile.Location = location
	profile.Website = website
	profile.Company = company
	profile.PreferredContact = preferredContact
	profile.ModerationNotes = moderationNotes
	profile.DeletedAt = deletedAt

	return nil
}

func (m *UserProfileModel) FlagUserProfileTx(ctx context.Context,tx pgx.Tx, userID uuid.UUID, isFlagged bool, notes string) error {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("FlagUserProfile")

	if userID == uuid.Nil {
		err := errors.New("user ID is required for flagging")
		logger.Error("validation failed", err)
		return err
	}


	notes = strings.TrimSpace(notes)

	var updatedAt time.Time
	err := tx.QueryRow(
		ctx,
		`
			UPDATE user_profiles
			SET
				is_flagged = $1,
				moderation_notes = NULLIF($2, ''),
				updated_at = NOW()
			WHERE user_id = $3
			  AND deleted_at IS NULL
			RETURNING updated_at
		`,
		isFlagged,
		notes,
		userID,
	).Scan(&updatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			logger.Warn("user profile not found", "user_id", userID)
			return ErrUserProfileNotFound
		}
		logger.Error("flag user profile failed", err, "user_id", userID)
		return fmt.Errorf("flag user profile: %w", err)
	}

	action := "unflagged"
	if isFlagged {
		action = "flagged"
	}

	if _, err := tx.Exec(
		ctx,
		`
			INSERT INTO user_profile_moderation_logs (user_id, action, note)
			VALUES ($1, $2, NULLIF($3, ''))
		`,
		userID,
		action,
		notes,
	); err != nil {
		logger.Error("insert moderation log failed", err, "user_id", userID, "action", action)
		return fmt.Errorf("insert user profile moderation log: %w", err)
	}


	return nil
}
