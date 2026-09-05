// Package data provides models and database access methods for user profiles and related entities.
//
// File: sdworkspace/sdbackend/internal/data/user_profiles.go
//
// GTM:
//
//	Layer: 2.2 Identity / Auth Domain
//	Release Class: SPINE
//	Reason:
//	  User profiles are release-critical identity and account lifecycle
//	  infrastructure. They preserve canonical user profile data, user-handle
//	  validation, global handle reservation, profile moderation state,
//	  normalized social-link reads, notification-preference reads, and
//	  soft-delete lifecycle behavior required by the initial Platform
//	  release spine.
//
// SPINE Rule:
//
//	Keep compiling.
//	Keep production-ready.
//	Preserve user profile canonical row alignment.
//	Preserve user-handle validation and reserved-handle behavior.
//	Preserve global handle reservation/release transaction semantics.
//	Preserve profile moderation state and moderation log behavior.
//	Preserve normalized relation loading for social links and notification preferences.
//	Preserve DB-owned lifecycle timestamp behavior.
//	Block deployment if this file breaks build, profile persistence,
//	handle integrity, global handle reservation, profile moderation,
//	or identity/account lifecycle integrity.
package data

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/PiccoloMondoC/focodebase/fobackend/internal/logging"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var reservedHandles = map[string]bool{
	"admin":   true,
	"support": true,
	"root":    true,
	"system":  true,
	"help":    true,
}

// UserProfileSocialLink represents one normalized social-link row associated with a user profile.
type UserProfileSocialLink struct {
	ID           uuid.UUID `json:"id"`
	UserID       uuid.UUID `json:"user_id"`
	PlatformID   uuid.UUID `json:"platform_id"`
	PlatformName string    `json:"platform_name"`
	BaseURL      *string   `json:"base_url,omitempty"`
	URL          string    `json:"url"`
	Handle       *string   `json:"handle,omitempty"`
	IsPrimary    bool      `json:"is_primary"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// UserProfileNotificationPreference represents one normalized notification-preference row.
type UserProfileNotificationPreference struct {
	ID                 uuid.UUID `json:"id"`
	UserID             uuid.UUID `json:"user_id"`
	NotificationTypeID uuid.UUID `json:"notification_type_id"`
	NotificationType   string    `json:"notification_type"`
	ChannelID          uuid.UUID `json:"channel_id"`
	Channel            string    `json:"channel"`
	IsEnabled          bool      `json:"is_enabled"`
	Frequency          string    `json:"frequency"`
	CreatedAt          time.Time `json:"created_at"`
	UpdatedAt          time.Time `json:"updated_at"`
}

// UserProfile represents the canonical persisted user profile row plus normalized related data.
type UserProfile struct {
	UserID           uuid.UUID  `json:"user_id" db:"user_id"`
	FirstName        *string    `json:"first_name,omitempty" db:"first_name"`
	LastName         *string    `json:"last_name,omitempty" db:"last_name"`
	UserHandle       *string    `json:"user_handle,omitempty" db:"user_handle"`
	Phone            *string    `json:"phone,omitempty" db:"phone"`
	AvatarURL        *string    `json:"avatar_url,omitempty" db:"avatar_url"`
	Bio              *string    `json:"bio,omitempty" db:"bio"`
	Location         *string    `json:"location,omitempty" db:"location"`
	Website          *string    `json:"website,omitempty" db:"website"`
	Company          *string    `json:"company,omitempty" db:"company"`
	PreferredContact string     `json:"preferred_contact" db:"preferred_contact"`
	IsFlagged        bool       `json:"is_flagged" db:"is_flagged"`
	ModerationNotes  *string    `json:"moderation_notes,omitempty" db:"moderation_notes"`
	CreatedAt        time.Time  `json:"created_at" db:"created_at"`
	UpdatedAt        time.Time  `json:"updated_at" db:"updated_at"`
	DeletedAt        *time.Time `json:"-" db:"deleted_at"`

	SocialLinks             []UserProfileSocialLink             `json:"social_links,omitempty"`
	NotificationPreferences []UserProfileNotificationPreference `json:"notification_preferences,omitempty"`
}

// UserProfileModel is the structure which holds the DB instance.
type UserProfileModel struct {
	DB     *pgxpool.Pool
	Logger *logging.Logger
}

func normalizeRequiredProfileString(value *string, fieldName string) (string, error) {
	if value == nil {
		return "", fmt.Errorf("%s is required", fieldName)
	}

	normalized := strings.TrimSpace(*value)
	if normalized == "" {
		return "", fmt.Errorf("%s is required", fieldName)
	}

	return normalized, nil
}

func normalizeOptionalUserHandle(value *string) (*string, error) {
	if value == nil {
		return nil, nil
	}

	handle := strings.ToLower(strings.TrimSpace(*value))
	if handle == "" {
		return nil, nil
	}

	if err := validateUserHandle(handle); err != nil {
		return nil, err
	}

	return &handle, nil
}

func validatePreferredContact(value string) error {
	switch strings.TrimSpace(strings.ToLower(value)) {
	case "email", "phone":
		return nil
	default:
		return errors.New("preferred_contact must be either 'email' or 'phone'")
	}
}

func validateUserHandle(handle string) error {
	if len(handle) < 3 || len(handle) > 30 {
		return fmt.Errorf("user handle must be between 3 and 30 characters")
	}

	for _, c := range handle {
		if !(c == '_' || c == '-' || (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9')) {
			return fmt.Errorf("user handle can only contain lowercase letters, digits, hyphens, or underscores")
		}
	}

	if reservedHandles[handle] {
		return fmt.Errorf("user handle '%s' is reserved", handle)
	}

	return nil
}

func (m *UserProfileModel) scanUserProfile(scanner interface {
	Scan(dest ...any) error
}) (*UserProfile, error) {
	var profile UserProfile

	err := scanner.Scan(
		&profile.UserID,
		&profile.FirstName,
		&profile.LastName,
		&profile.UserHandle,
		&profile.Phone,
		&profile.AvatarURL,
		&profile.Bio,
		&profile.Location,
		&profile.Website,
		&profile.Company,
		&profile.PreferredContact,
		&profile.IsFlagged,
		&profile.ModerationNotes,
		&profile.CreatedAt,
		&profile.UpdatedAt,
		&profile.DeletedAt,
	)
	if err != nil {
		return nil, err
	}

	return &profile, nil
}

func (m *UserProfileModel) loadSocialLinks(ctx context.Context, userID uuid.UUID) ([]UserProfileSocialLink, error) {
	query := `
		SELECT
			upsl.id,
			upsl.user_id,
			upsl.platform_id,
			sp.name,
			sp.base_url,
			upsl.url,
			upsl.handle,
			upsl.is_primary,
			upsl.created_at,
			upsl.updated_at
		FROM user_profile_social_links upsl
		INNER JOIN social_platforms sp ON sp.id = upsl.platform_id
		WHERE upsl.user_id = $1
		ORDER BY upsl.is_primary DESC, sp.name ASC, upsl.created_at ASC
	`

	rows, err := m.DB.Query(ctx, query, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	links := make([]UserProfileSocialLink, 0)
	for rows.Next() {
		var item UserProfileSocialLink
		if err := rows.Scan(
			&item.ID,
			&item.UserID,
			&item.PlatformID,
			&item.PlatformName,
			&item.BaseURL,
			&item.URL,
			&item.Handle,
			&item.IsPrimary,
			&item.CreatedAt,
			&item.UpdatedAt,
		); err != nil {
			return nil, err
		}

		links = append(links, item)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return links, nil
}

func (m *UserProfileModel) loadNotificationPreferences(ctx context.Context, userID uuid.UUID) ([]UserProfileNotificationPreference, error) {
	query := `
		SELECT
			unp.id,
			unp.user_id,
			unp.notification_type_id,
			nt.type,
			unp.channel_id,
			nc.channel,
			unp.is_enabled,
			unp.frequency,
			unp.created_at,
			unp.updated_at
		FROM user_notification_preferences unp
		INNER JOIN notification_types nt ON nt.id = unp.notification_type_id
		INNER JOIN notification_channels nc ON nc.id = unp.channel_id
		WHERE unp.user_id = $1
		ORDER BY nt.type ASC, nc.channel ASC
	`

	rows, err := m.DB.Query(ctx, query, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]UserProfileNotificationPreference, 0)
	for rows.Next() {
		var item UserProfileNotificationPreference
		if err := rows.Scan(
			&item.ID,
			&item.UserID,
			&item.NotificationTypeID,
			&item.NotificationType,
			&item.ChannelID,
			&item.Channel,
			&item.IsEnabled,
			&item.Frequency,
			&item.CreatedAt,
			&item.UpdatedAt,
		); err != nil {
			return nil, err
		}

		items = append(items, item)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return items, nil
}

func (m *UserProfileModel) populateRelations(ctx context.Context, profile *UserProfile) error {
	socialLinks, err := m.loadSocialLinks(ctx, profile.UserID)
	if err != nil {
		return err
	}

	notificationPreferences, err := m.loadNotificationPreferences(ctx, profile.UserID)
	if err != nil {
		return err
	}

	profile.SocialLinks = socialLinks
	profile.NotificationPreferences = notificationPreferences
	return nil
}

func (m *UserProfileModel) insertGlobalHandleTx(ctx context.Context, tx pgx.Tx, handle string, entityType string, entityID uuid.UUID) error {
	_, err := tx.Exec(
		ctx,
		`
			INSERT INTO global_handles (handle, type, entity_id)
			VALUES ($1, $2, $3)
		`,
		handle,
		entityType,
		entityID,
	)
	return err
}

func (m *UserProfileModel) deleteGlobalHandleTx(ctx context.Context, tx pgx.Tx, handle string, entityType string) error {
	_, err := tx.Exec(
		ctx,
		`
			DELETE FROM global_handles
			WHERE handle = $1
			  AND type = $2
		`,
		handle,
		entityType,
	)
	return err
}

// Insert inserts a new user profile row.
// Related social links and notification preferences are read from their normalized tables; they are not persisted here.
func (m *UserProfileModel) Insert(ctx context.Context, profile *UserProfile) error {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("InsertUserProfile")

	if profile == nil {
		err := errors.New("profile is required")
		logger.Error("validation failed", err)
		return err
	}

	if profile.UserID == uuid.Nil {
		err := errors.New("user ID is required")
		logger.Error("validation failed", err)
		return err
	}

	firstName, err := normalizeRequiredProfileString(profile.FirstName, "first_name")
	if err != nil {
		logger.Error("validation failed", err, "user_id", profile.UserID)
		return err
	}

	lastName, err := normalizeRequiredProfileString(profile.LastName, "last_name")
	if err != nil {
		logger.Error("validation failed", err, "user_id", profile.UserID)
		return err
	}

	handle, err := normalizeOptionalUserHandle(profile.UserHandle)
	if err != nil {
		logger.Error("validation failed", err, "user_id", profile.UserID)
		return err
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
		return err
	}

	tx, err := m.DB.Begin(ctx)
	if err != nil {
		logger.Error("begin transaction failed", err, "user_id", profile.UserID)
		return err
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	query := `
		INSERT INTO user_profiles (
			user_id,
			first_name,
			last_name,
			user_handle,
			phone,
			avatar_url,
			bio,
			location,
			website,
			company,
			preferred_contact,
			is_flagged,
			moderation_notes
		) VALUES (
			$1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13
		)
		RETURNING created_at, updated_at, deleted_at
	`

	var deletedAt *time.Time
	err = tx.QueryRow(
		ctx,
		query,
		profile.UserID,
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
		profile.IsFlagged,
		moderationNotes,
	).Scan(&profile.CreatedAt, &profile.UpdatedAt, &deletedAt)
	if err != nil {
		logger.Error("insert user profile failed", err, "user_id", profile.UserID)
		return err
	}

	if handle != nil {
		if err := m.insertGlobalHandleTx(ctx, tx, *handle, "user", profile.UserID); err != nil {
			logger.Error("insert global handle failed", err, "user_id", profile.UserID, "user_handle", *handle)
			return err
		}
	}

	if err := tx.Commit(ctx); err != nil {
		logger.Error("commit failed", err, "user_id", profile.UserID)
		return err
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

	logger.Info("user profile inserted successfully", "user_id", profile.UserID)
	return nil
}

// GetByUserID retrieves an active user profile by user ID.
func (m *UserProfileModel) GetByUserID(ctx context.Context, userID uuid.UUID) (*UserProfile, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("GetByUserID")

	if userID == uuid.Nil {
		err := errors.New("user ID is required")
		logger.Error("validation failed", err)
		return nil, err
	}

	query := `
		SELECT
			user_id,
			first_name,
			last_name,
			user_handle,
			phone,
			avatar_url,
			bio,
			location,
			website,
			company,
			preferred_contact,
			is_flagged,
			moderation_notes,
			created_at,
			updated_at,
			deleted_at
		FROM user_profiles
		WHERE user_id = $1
		  AND deleted_at IS NULL
	`

	profile, err := m.scanUserProfile(m.DB.QueryRow(ctx, query, userID))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			logger.Warn("user profile not found", "user_id", userID)
			return nil, nil
		}
		logger.Error("get by user id failed", err, "user_id", userID)
		return nil, err
	}

	if err := m.populateRelations(ctx, profile); err != nil {
		logger.Error("load related profile data failed", err, "user_id", userID)
		return nil, err
	}

	logger.Info("user profile loaded", "user_id", userID)
	return profile, nil
}

// GetByUserHandle retrieves an active user profile by handle.
func (m *UserProfileModel) GetByUserHandle(ctx context.Context, userHandle string) (*UserProfile, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("GetByUserHandle")

	handle := strings.ToLower(strings.TrimSpace(userHandle))
	if handle == "" {
		err := errors.New("user handle is required")
		logger.Error("validation failed", err)
		return nil, err
	}

	if err := validateUserHandle(handle); err != nil {
		logger.Error("validation failed", err, "user_handle", handle)
		return nil, err
	}

	query := `
		SELECT
			user_id,
			first_name,
			last_name,
			user_handle,
			phone,
			avatar_url,
			bio,
			location,
			website,
			company,
			preferred_contact,
			is_flagged,
			moderation_notes,
			created_at,
			updated_at,
			deleted_at
		FROM user_profiles
		WHERE user_handle = $1
		  AND deleted_at IS NULL
	`

	profile, err := m.scanUserProfile(m.DB.QueryRow(ctx, query, handle))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			logger.Warn("user profile not found", "user_handle", handle)
			return nil, nil
		}
		logger.Error("get by user handle failed", err, "user_handle", handle)
		return nil, err
	}

	if err := m.populateRelations(ctx, profile); err != nil {
		logger.Error("load related profile data failed", err, "user_handle", handle)
		return nil, err
	}

	logger.Info("user profile loaded", "user_handle", handle)
	return profile, nil
}

// GetAll retrieves active user profiles with pagination.
// Manual row iteration is retained here as a bounded exception because each row requires additional relation loading.
func (m *UserProfileModel) GetAll(ctx context.Context, limit, offset int) ([]*UserProfile, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("GetAllUserProfiles")

	const maxPageSize = 100
	if limit <= 0 || limit > maxPageSize {
		limit = 20
	}
	if offset < 0 {
		offset = 0
	}

	query := `
		SELECT
			user_id,
			first_name,
			last_name,
			user_handle,
			phone,
			avatar_url,
			bio,
			location,
			website,
			company,
			preferred_contact,
			is_flagged,
			moderation_notes,
			created_at,
			updated_at,
			deleted_at
		FROM user_profiles
		WHERE deleted_at IS NULL
		ORDER BY updated_at DESC, created_at DESC
		LIMIT $1 OFFSET $2
	`

	rows, err := m.DB.Query(ctx, query, limit, offset)
	if err != nil {
		logger.Error("get all user profiles failed", err)
		return nil, err
	}
	defer rows.Close()

	profiles := make([]*UserProfile, 0)
	for rows.Next() {
		profile, err := m.scanUserProfile(rows)
		if err != nil {
			logger.Error("scan user profile failed", err)
			return nil, err
		}

		if err := m.populateRelations(ctx, profile); err != nil {
			logger.Error("load related profile data failed", err, "user_id", profile.UserID)
			return nil, err
		}

		profiles = append(profiles, profile)
	}

	if err := rows.Err(); err != nil {
		logger.Error("row iteration failed", err)
		return nil, err
	}

	logger.Info("user profiles loaded", "count", len(profiles), "limit", limit, "offset", offset)
	return profiles, nil
}

// SearchUserProfiles performs an internal/consumer account profile search over active profile rows.
// Manual row iteration is retained here as a bounded exception because each row requires additional relation loading.
func (m *UserProfileModel) SearchUserProfiles(ctx context.Context, queryText string, limit, offset int) ([]*UserProfile, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("SearchUserProfiles")

	searchTerm := strings.TrimSpace(queryText)
	if searchTerm == "" {
		err := errors.New("search query cannot be empty")
		logger.Error("validation failed", err)
		return nil, err
	}

	const maxPageSize = 100
	if limit <= 0 || limit > maxPageSize {
		limit = 20
	}
	if offset < 0 {
		offset = 0
	}

	query := `
		SELECT
			user_id,
			first_name,
			last_name,
			user_handle,
			phone,
			avatar_url,
			bio,
			location,
			website,
			company,
			preferred_contact,
			is_flagged,
			moderation_notes,
			created_at,
			updated_at,
			deleted_at
		FROM user_profiles
		WHERE deleted_at IS NULL
		  AND (
			to_tsvector(
				'simple',
				coalesce(first_name, '') || ' ' ||
				coalesce(last_name, '') || ' ' ||
				coalesce(user_handle::text, '') || ' ' ||
				coalesce(company, '') || ' ' ||
				coalesce(location, '') || ' ' ||
				coalesce(bio, '')
			) @@ plainto_tsquery('simple', $1)
			OR first_name ILIKE '%' || $1 || '%'
			OR last_name ILIKE '%' || $1 || '%'
			OR user_handle::text ILIKE '%' || $1 || '%'
			OR company ILIKE '%' || $1 || '%'
			OR location ILIKE '%' || $1 || '%'
			OR bio ILIKE '%' || $1 || '%'
		  )
		ORDER BY updated_at DESC, created_at DESC
		LIMIT $2 OFFSET $3
	`

	rows, err := m.DB.Query(ctx, query, searchTerm, limit, offset)
	if err != nil {
		logger.Error("search user profiles failed", err, "query", searchTerm)
		return nil, err
	}
	defer rows.Close()

	results := make([]*UserProfile, 0)
	for rows.Next() {
		profile, err := m.scanUserProfile(rows)
		if err != nil {
			logger.Error("scan user profile failed", err)
			return nil, err
		}

		if err := m.populateRelations(ctx, profile); err != nil {
			logger.Error("load related profile data failed", err, "user_id", profile.UserID)
			return nil, err
		}

		results = append(results, profile)
	}

	if err := rows.Err(); err != nil {
		logger.Error("row iteration failed", err)
		return nil, err
	}

	logger.Info("user profile search completed", "query", searchTerm, "count", len(results), "limit", limit, "offset", offset)
	return results, nil
}

// Update updates an active user profile.
// Related social links and notification preferences remain in their own normalized tables.
func (m *UserProfileModel) Update(ctx context.Context, profile *UserProfile) error {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("UpdateUserProfile")

	if profile == nil {
		err := errors.New("profile is required")
		logger.Error("validation failed", err)
		return err
	}

	if profile.UserID == uuid.Nil {
		err := errors.New("user ID is required")
		logger.Error("validation failed", err)
		return err
	}

	firstName, err := normalizeRequiredProfileString(profile.FirstName, "first_name")
	if err != nil {
		logger.Error("validation failed", err, "user_id", profile.UserID)
		return err
	}

	lastName, err := normalizeRequiredProfileString(profile.LastName, "last_name")
	if err != nil {
		logger.Error("validation failed", err, "user_id", profile.UserID)
		return err
	}

	handle, err := normalizeOptionalUserHandle(profile.UserHandle)
	if err != nil {
		logger.Error("validation failed", err, "user_id", profile.UserID)
		return err
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
		return err
	}

	tx, err := m.DB.Begin(ctx)
	if err != nil {
		logger.Error("begin transaction failed", err, "user_id", profile.UserID)
		return err
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()

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

	if err := tx.Commit(ctx); err != nil {
		logger.Error("commit failed", err, "user_id", profile.UserID)
		return fmt.Errorf("commit user profile update: %w", err)
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

	logger.Info("user profile updated successfully", "user_id", profile.UserID)
	return nil
}

// FlagUserProfile updates moderation state for an active user profile.
// It does not suspend the user account itself.
func (m *UserProfileModel) FlagUserProfile(ctx context.Context, userID uuid.UUID, isFlagged bool, notes string) error {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("FlagUserProfile")

	if userID == uuid.Nil {
		err := errors.New("user ID is required for flagging")
		logger.Error("validation failed", err)
		return err
	}

	tx, err := m.DB.Begin(ctx)
	if err != nil {
		logger.Error("begin transaction failed", err, "user_id", userID)
		return err
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	notes = strings.TrimSpace(notes)

	var updatedAt time.Time
	err = tx.QueryRow(
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

	if err := tx.Commit(ctx); err != nil {
		logger.Error("commit failed", err, "user_id", userID)
		return fmt.Errorf("commit user profile flag update: %w", err)
	}

	logger.Info("user profile moderation updated", "user_id", userID, "is_flagged", isFlagged, "updated_at", updatedAt)
	return nil
}

// SoftDelete logically removes a user profile while retaining lifecycle history.
// The associated global user handle is released as part of the same transaction.
func (m *UserProfileModel) SoftDelete(ctx context.Context, userID uuid.UUID) error {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("SoftDeleteUserProfile")

	if userID == uuid.Nil {
		err := errors.New("user ID is required")
		logger.Error("validation failed", err)
		return err
	}

	tx, err := m.DB.Begin(ctx)
	if err != nil {
		logger.Error("begin transaction failed", err, "user_id", userID)
		return err
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	var existingHandle *string
	err = tx.QueryRow(
		ctx,
		`
			SELECT user_handle
			FROM user_profiles
			WHERE user_id = $1
			  AND deleted_at IS NULL
			FOR UPDATE
		`,
		userID,
	).Scan(&existingHandle)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			logger.Warn("user profile not found", "user_id", userID)
			return ErrUserProfileNotFound
		}
		logger.Error("load existing handle failed", err, "user_id", userID)
		return fmt.Errorf("load existing user profile for soft delete: %w", err)
	}

	var deletedAt time.Time
	err = tx.QueryRow(
		ctx,
		`
			UPDATE user_profiles
			SET
				deleted_at = NOW(),
				updated_at = NOW()
			WHERE user_id = $1
			  AND deleted_at IS NULL
			RETURNING deleted_at
		`,
		userID,
	).Scan(&deletedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			logger.Warn("user profile not found during soft delete", "user_id", userID)
			return ErrUserProfileNotFound
		}
		logger.Error("soft delete failed", err, "user_id", userID)
		return fmt.Errorf("soft delete user profile: %w", err)
	}

	if existingHandle != nil && *existingHandle != "" {
		if err := m.deleteGlobalHandleTx(ctx, tx, *existingHandle, "user"); err != nil {
			logger.Error("delete global handle failed", err, "user_id", userID, "user_handle", *existingHandle)
			return fmt.Errorf("delete global handle during soft delete: %w", err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		logger.Error("commit failed", err, "user_id", userID)
		return fmt.Errorf("commit user profile soft delete: %w", err)
	}

	logger.Info("user profile soft deleted", "user_id", userID, "deleted_at", deletedAt)
	return nil
}

// Delete permanently removes a user profile row.
// The associated global user handle is also removed in the same transaction.
func (m *UserProfileModel) Delete(ctx context.Context, userID uuid.UUID) error {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("DeleteUserProfile")

	if userID == uuid.Nil {
		err := errors.New("user ID is required")
		logger.Error("validation failed", err)
		return err
	}

	tx, err := m.DB.Begin(ctx)
	if err != nil {
		logger.Error("begin transaction failed", err, "user_id", userID)
		return err
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	var existingHandle *string
	err = tx.QueryRow(
		ctx,
		`
			SELECT user_handle
			FROM user_profiles
			WHERE user_id = $1
			FOR UPDATE
		`,
		userID,
	).Scan(&existingHandle)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			logger.Warn("user profile not found", "user_id", userID)
			return ErrUserProfileNotFound
		}
		logger.Error("load existing handle failed", err, "user_id", userID)
		return fmt.Errorf("load existing user profile for delete: %w", err)
	}

	var deletedUserID uuid.UUID
	err = tx.QueryRow(
		ctx,
		`
			DELETE FROM user_profiles
			WHERE user_id = $1
			RETURNING user_id
		`,
		userID,
	).Scan(&deletedUserID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			logger.Warn("user profile not found during delete", "user_id", userID)
			return ErrUserProfileNotFound
		}
		logger.Error("hard delete failed", err, "user_id", userID)
		return fmt.Errorf("delete user profile: %w", err)
	}

	if existingHandle != nil && *existingHandle != "" {
		if err := m.deleteGlobalHandleTx(ctx, tx, *existingHandle, "user"); err != nil {
			logger.Error("delete global handle failed", err, "user_id", userID, "user_handle", *existingHandle)
			return fmt.Errorf("delete global handle during hard delete: %w", err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		logger.Error("commit failed", err, "user_id", userID)
		return fmt.Errorf("commit user profile delete: %w", err)
	}

	logger.Info("user profile hard deleted", "user_id", deletedUserID)
	return nil
}

// GetReservedHandles returns hard-reserved handles plus already-claimed global user handles.
func (m *UserProfileModel) GetReservedHandles(ctx context.Context) ([]string, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("GetReservedHandles")

	reserved := make(map[string]bool, len(reservedHandles))
	for handle := range reservedHandles {
		reserved[handle] = true
	}

	rows, err := m.DB.Query(ctx, `SELECT handle FROM global_handles WHERE type = 'user'`)
	if err != nil {
		logger.Error("query global handles failed", err)
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var handle string
		if err := rows.Scan(&handle); err != nil {
			logger.Error("scan handle failed", err)
			return nil, err
		}
		reserved[handle] = true
	}

	if err := rows.Err(); err != nil {
		logger.Error("row iteration failed", err)
		return nil, err
	}

	result := make([]string, 0, len(reserved))
	for handle := range reserved {
		result = append(result, handle)
	}
	sort.Strings(result)

	logger.Info("reserved handles loaded", "count", len(result))
	return result, nil
}

// ValidateUserHandle validates a proposed handle and checks global availability.
func (m *UserProfileModel) ValidateUserHandle(ctx context.Context, handle string) error {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("ValidateUserHandle")

	handle = strings.ToLower(strings.TrimSpace(handle))
	if err := validateUserHandle(handle); err != nil {
		logger.Error("validation failed", err, "user_handle", handle)
		return err
	}

	var exists bool
	err := m.DB.QueryRow(ctx, `
		SELECT EXISTS(
			SELECT 1 
			FROM global_handles 
			WHERE handle = $1
				AND type = 'user'
		)
	`,
		handle).Scan(&exists)

	if err != nil {
		logger.Error("check handle availability failed", err, "user_handle", handle)
		return fmt.Errorf("check user handle availability: %w", err)
	}
	if exists {
		logger.Warn("user handle already taken", "user_handle", handle)
		return ErrUserHandleTaken
	}

	logger.Info("user handle is available", "user_handle", handle)
	return nil
}

// GetUserIDByHandle resolves an active user ID from a valid user handle.
func (m *UserProfileModel) GetUserIDByHandle(ctx context.Context, handle string) (uuid.UUID, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("GetUserIDByHandle")

	handle = strings.ToLower(strings.TrimSpace(handle))
	if err := validateUserHandle(handle); err != nil {
		logger.Error("validation failed", err, "user_handle", handle)
		return uuid.Nil, err
	}

	var userID uuid.UUID
	err := m.DB.QueryRow(
		ctx,
		`
			SELECT user_id
			FROM user_profiles
			WHERE user_handle = $1
			  AND deleted_at IS NULL
		`,
		handle,
	).Scan(&userID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			logger.Warn("user handle not found", "user_handle", handle)
			return uuid.Nil, ErrUserHandleNotFound
		}
		logger.Error("get user id by handle failed", err, "user_handle", handle)
		return uuid.Nil, fmt.Errorf("get user id by handle: %w", err)
	}

	logger.Info("resolved user id by handle", "user_handle", handle, "user_id", userID)
	return userID, nil
}

// ExistsByUserID checks whether an active user profile exists for the given user ID.
func (m *UserProfileModel) ExistsByUserID(ctx context.Context, userID uuid.UUID) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("ExistsByUserID")

	if userID == uuid.Nil {
		err := errors.New("user ID is required")
		logger.Error("validation failed", err)
		return false, err
	}

	var exists bool
	err := m.DB.QueryRow(
		ctx,
		`
			SELECT EXISTS(
				SELECT 1
				FROM user_profiles
				WHERE user_id = $1
				  AND deleted_at IS NULL
			)
		`,
		userID,
	).Scan(&exists)
	if err != nil {
		logger.Error("exists by user id failed", err, "user_id", userID)
		return false, fmt.Errorf("check user profile existence: %w", err)
	}

	logger.Info("user profile existence checked", "user_id", userID, "exists", exists)
	return exists, nil
}
