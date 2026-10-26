// Package data provides models and database access methods for user profiles and related entities.
//
// focodebase/fobackend/internal/data/user_profiles.go
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
 FullLegalName *string `json:"-"`
 HasPhoto bool `json:"-"`
	UserID           uuid.UUID  `json:"user_id" db:"user_id"`
	FirstName        *string    `json:"-" db:"first_name"`
	LastName         *string    `json:"-" db:"last_name"`
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

func (m *UserProfileModel) loadProfileSocialLinks(ctx context.Context, q profileQueryer, userID uuid.UUID) ([]UserProfileSocialLink, error) {
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

	rows, err := q.Query(ctx, query, userID)
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

func (m *UserProfileModel) loadProfileNotificationPreferences(ctx context.Context, q profileQueryer, userID uuid.UUID) ([]UserProfileNotificationPreference, error) {
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

	rows, err := q.Query(ctx, query, userID)
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


type profileQueryer interface { Query(context.Context,string,...any)(pgx.Rows,error) }

func(m *UserProfileModel)loadSocialLinks(ctx context.Context,id uuid.UUID)([]UserProfileSocialLink,error){return m.loadProfileSocialLinks(ctx,m.DB,id)}
func(m *UserProfileModel)loadNotificationPreferences(ctx context.Context,id uuid.UUID)([]UserProfileNotificationPreference,error){return m.loadProfileNotificationPreferences(ctx,m.DB,id)}
