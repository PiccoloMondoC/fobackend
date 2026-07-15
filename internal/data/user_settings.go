// Package data provides models and database access methods for user settings.
//
// sdworkspace/sdbackend/internal/data/user_settings.go
//
// GTM:
//   Layer: 2.3 Consumer Domain
//   Release Class: SPINE
//   Reason:
//     User settings are release-critical account preference infrastructure.
//     They preserve notification preferences, privacy data-sharing controls,
//     preferred notification channel selection, structured preference storage,
//     and soft-delete/restore lifecycle behavior required by the initial
//     SagrentiDeals release spine.
//
// SPINE Rule:
//   Keep compiling.
//   Keep production-ready.
//   Preserve user-owned settings semantics.
//   Preserve separation between notification decisions and contact data.
//   Preserve preferred notification channel linkage.
//   Preserve JSONB preferences marshal/unmarshal behavior.
//   Preserve soft-delete, restore, and hard-delete distinction.
//   Block deployment if this file breaks build, settings persistence,
//   notification preference behavior, privacy controls, or account preference integrity.
package data

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/PiccoloMondoC/sdworkspace/sdbackend/internal/logging"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// UserSettings represents account-level notification and privacy settings
// owned by a single authenticated user.
//
// Contact fields such as email and phone belong to users/user_profiles, not
// this table. User settings decide how/whether to notify; account/profile data
// decide where to notify.
type UserSettings struct {
	UserID                 uuid.UUID      `json:"user_id" db:"user_id"`
	PreferredChannelID     uuid.UUID      `json:"preferred_channel_id" db:"preferred_channel_id"`
	MarketingNotifications bool           `json:"marketing_notifications" db:"marketing_notifications"`
	SecurityNotifications  bool           `json:"security_notifications" db:"security_notifications"`
	PrivacyDataSharing     bool           `json:"privacy_data_sharing" db:"privacy_data_sharing"`
	Preferences            map[string]any `json:"preferences" db:"preferences"`
	CreatedAt              time.Time      `json:"created_at" db:"created_at"`
	UpdatedAt              time.Time      `json:"updated_at" db:"updated_at"`
	DeletedAt              *time.Time     `json:"-" db:"deleted_at"`
}

// UserSettingsModel provides database access for user-owned settings.
type UserSettingsModel struct {
	DB     *pgxpool.Pool
	Logger *logging.Logger
}

// Insert creates or restores a user's settings row.
func (m *UserSettingsModel) Insert(ctx context.Context, settings *UserSettings) error {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("InsertUserSettings")

	if err := validateUserSettings(settings); err != nil {
		logger.Error("validation failed", err, "user_id", safeUserSettingsUserID(settings))
		return err
	}

	preferences, err := marshalUserSettingsPreferences(settings.Preferences)
	if err != nil {
		logger.Error("marshal preferences failed", err, "user_id", settings.UserID)
		return err
	}

	query := `
		INSERT INTO user_settings (
			user_id,
			preferred_channel_id,
			marketing_notifications,
			security_notifications,
			privacy_data_sharing,
			preferences
		)
		VALUES ($1, $2, $3, $4, $5, $6::jsonb)
		ON CONFLICT (user_id) DO UPDATE
		SET preferred_channel_id = EXCLUDED.preferred_channel_id,
		    marketing_notifications = EXCLUDED.marketing_notifications,
		    security_notifications = EXCLUDED.security_notifications,
		    privacy_data_sharing = EXCLUDED.privacy_data_sharing,
		    preferences = EXCLUDED.preferences,
		    deleted_at = NULL
		RETURNING user_id, preferred_channel_id, marketing_notifications,
		          security_notifications, privacy_data_sharing, preferences,
		          created_at, updated_at, deleted_at
	`

	inserted, err := scanUserSettings(m.DB.QueryRow(ctx, query,
		settings.UserID,
		settings.PreferredChannelID,
		settings.MarketingNotifications,
		settings.SecurityNotifications,
		settings.PrivacyDataSharing,
		preferences,
	))
	if err != nil {
		logger.Error("insert user settings failed", err, "user_id", settings.UserID)
		return fmt.Errorf("insert user settings: %w", err)
	}

	*settings = *inserted

	logger.Info("user settings inserted", "user_id", settings.UserID)
	return nil
}

// GetByUserID retrieves active settings for one user.
func (m *UserSettingsModel) GetByUserID(ctx context.Context, userID uuid.UUID) (*UserSettings, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("GetUserSettingsByUserID")

	if userID == uuid.Nil {
		err := errors.New("user ID is required")
		logger.Error("validation failed", err)
		return nil, err
	}

	query := `
		SELECT user_id, preferred_channel_id, marketing_notifications,
		       security_notifications, privacy_data_sharing, preferences,
		       created_at, updated_at, deleted_at
		FROM user_settings
		WHERE user_id = $1
		  AND deleted_at IS NULL
	`

	settings, err := scanUserSettings(m.DB.QueryRow(ctx, query, userID))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			logger.Warn("user settings not found", "user_id", userID)
			return nil, ErrUserSettingsNotFound
		}
		logger.Error("get user settings failed", err, "user_id", userID)
		return nil, fmt.Errorf("get user settings: %w", err)
	}

	return settings, nil
}

// GetAll retrieves all active user settings rows.
func (m *UserSettingsModel) GetAll(ctx context.Context) ([]UserSettings, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("GetAllUserSettings")

	query := `
		SELECT user_id, preferred_channel_id, marketing_notifications,
		       security_notifications, privacy_data_sharing, preferences,
		       created_at, updated_at, deleted_at
		FROM user_settings
		WHERE deleted_at IS NULL
		ORDER BY updated_at DESC
	`

	rows, err := m.DB.Query(ctx, query)
	if err != nil {
		logger.Error("query user settings failed", err)
		return nil, fmt.Errorf("query user settings: %w", err)
	}
	defer rows.Close()

	settings, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (UserSettings, error) {
		setting, err := scanUserSettings(row)
		if err != nil {
			return UserSettings{}, err
		}
		return *setting, nil
	})
	if err != nil {
		logger.Error("collect user settings failed", err)
		return nil, fmt.Errorf("collect user settings: %w", err)
	}

	return settings, nil
}

// Update updates an active user-owned settings row.
func (m *UserSettingsModel) Update(ctx context.Context, settings *UserSettings) error {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("UpdateUserSettings")

	if err := validateUserSettings(settings); err != nil {
		logger.Error("validation failed", err, "user_id", safeUserSettingsUserID(settings))
		return err
	}

	preferences, err := marshalUserSettingsPreferences(settings.Preferences)
	if err != nil {
		logger.Error("marshal preferences failed", err, "user_id", settings.UserID)
		return err
	}

	query := `
		UPDATE user_settings
		SET preferred_channel_id = $1,
		    marketing_notifications = $2,
		    security_notifications = $3,
		    privacy_data_sharing = $4,
		    preferences = $5::jsonb
		WHERE user_id = $6
		  AND deleted_at IS NULL
		RETURNING user_id, preferred_channel_id, marketing_notifications,
		          security_notifications, privacy_data_sharing, preferences,
		          created_at, updated_at, deleted_at
	`

	updated, err := scanUserSettings(m.DB.QueryRow(ctx, query,
		settings.PreferredChannelID,
		settings.MarketingNotifications,
		settings.SecurityNotifications,
		settings.PrivacyDataSharing,
		preferences,
		settings.UserID,
	))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			logger.Warn("active user settings not found", "user_id", settings.UserID)
			return ErrUserSettingsNotFound
		}
		logger.Error("update user settings failed", err, "user_id", settings.UserID)
		return fmt.Errorf("update user settings: %w", err)
	}

	*settings = *updated

	logger.Info("user settings updated", "user_id", settings.UserID)
	return nil
}

// SoftDelete logically removes a user's settings row.
func (m *UserSettingsModel) SoftDelete(ctx context.Context, userID uuid.UUID) error {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("SoftDeleteUserSettings")

	if userID == uuid.Nil {
		err := errors.New("user ID is required")
		logger.Error("validation failed", err)
		return err
	}

	var returnedUserID uuid.UUID
	err := m.DB.QueryRow(ctx, `
		UPDATE user_settings
		SET deleted_at = NOW()
		WHERE user_id = $1
		  AND deleted_at IS NULL
		RETURNING user_id
	`, userID).Scan(&returnedUserID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			logger.Warn("active user settings not found for soft delete", "user_id", userID)
			return ErrUserSettingsNotFound
		}
		logger.Error("soft delete user settings failed", err, "user_id", userID)
		return fmt.Errorf("soft delete user settings: %w", err)
	}

	logger.Info("user settings soft deleted", "user_id", returnedUserID)
	return nil
}

// Restore reactivates a previously soft-deleted settings row.
func (m *UserSettingsModel) Restore(ctx context.Context, userID uuid.UUID) error {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("RestoreUserSettings")

	if userID == uuid.Nil {
		err := errors.New("user ID is required")
		logger.Error("validation failed", err)
		return err
	}

	var returnedUserID uuid.UUID
	err := m.DB.QueryRow(ctx, `
		UPDATE user_settings
		SET deleted_at = NULL
		WHERE user_id = $1
		  AND deleted_at IS NOT NULL
		RETURNING user_id
	`, userID).Scan(&returnedUserID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			logger.Warn("deleted user settings not found for restore", "user_id", userID)
			return ErrUserSettingsNotFound
		}
		logger.Error("restore user settings failed", err, "user_id", userID)
		return fmt.Errorf("restore user settings: %w", err)
	}

	logger.Info("user settings restored", "user_id", returnedUserID)
	return nil
}

// Delete permanently removes a user's settings row.
//
// This is a true hard delete and must remain separate from SoftDelete.
func (m *UserSettingsModel) Delete(ctx context.Context, userID uuid.UUID) error {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("DeleteUserSettings")

	if userID == uuid.Nil {
		err := errors.New("user ID is required")
		logger.Error("validation failed", err)
		return err
	}

	var returnedUserID uuid.UUID
	err := m.DB.QueryRow(ctx, `
		DELETE FROM user_settings
		WHERE user_id = $1
		RETURNING user_id
	`, userID).Scan(&returnedUserID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			logger.Warn("user settings not found for hard delete", "user_id", userID)
			return ErrUserSettingsNotFound
		}
		logger.Error("delete user settings failed", err, "user_id", userID)
		return fmt.Errorf("delete user settings: %w", err)
	}

	logger.Info("user settings hard deleted", "user_id", returnedUserID)
	return nil
}


type userSettingsRowScanner interface {
	Scan(dest ...any) error
}

func scanUserSettings(row userSettingsRowScanner) (*UserSettings, error) {
	var preferences []byte
	settings := &UserSettings{}

	err := row.Scan(
		&settings.UserID,
		&settings.PreferredChannelID,
		&settings.MarketingNotifications,
		&settings.SecurityNotifications,
		&settings.PrivacyDataSharing,
		&preferences,
		&settings.CreatedAt,
		&settings.UpdatedAt,
		&settings.DeletedAt,
	)
	if err != nil {
		return nil, err
	}

	settings.Preferences, err = unmarshalUserSettingsPreferences(preferences)
	if err != nil {
		return nil, err
	}

	return settings, nil
}

func validateUserSettings(settings *UserSettings) error {
	if settings == nil {
		return errors.New("user settings is required")
	}
	if settings.UserID == uuid.Nil {
		return errors.New("user ID is required")
	}
	if settings.PreferredChannelID == uuid.Nil {
		return errors.New("preferred notification channel ID is required")
	}
	return nil
}

func marshalUserSettingsPreferences(preferences map[string]any) ([]byte, error) {
	if preferences == nil {
		preferences = map[string]any{}
	}

	data, err := json.Marshal(preferences)
	if err != nil {
		return nil, fmt.Errorf("marshal preferences: %w", err)
	}

	return data, nil
}

func unmarshalUserSettingsPreferences(data []byte) (map[string]any, error) {
	if len(data) == 0 {
		return map[string]any{}, nil
	}

	var preferences map[string]any
	if err := json.Unmarshal(data, &preferences); err != nil {
		return nil, fmt.Errorf("unmarshal preferences: %w", err)
	}

	if preferences == nil {
		return map[string]any{}, nil
	}

	return preferences, nil
}

func safeUserSettingsUserID(settings *UserSettings) uuid.UUID {
	if settings == nil {
		return uuid.Nil
	}
	return settings.UserID
}