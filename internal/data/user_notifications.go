// Package data provides models and database access methods for user notifications and related lookup entities.
//
// File: sdworkspace/sdbackend/internal/data/user_notifications.go
//
// GTM:
//
//	Layer: 2.3 Consumer Domain
//	Release Class: SPINE
//	Reason:
//	  User notifications, notification types, notification channels, and
//	  notification-channel links are release-critical consumer communication
//	  infrastructure. They support retained notification history, price-drop
//	  and offer-related alerts, active notification lookup, dismissal behavior,
//	  and dynamic notification type/channel resolution required by the initial
//	  Platform release spine.
//
// SPINE Rule:
//
//	Keep compiling.
//	Keep production-ready.
//	Preserve immutable notification delivery/history semantics.
//	Preserve active notification type/channel lookup behavior.
//	Preserve dynamic notification type/channel resolution.
//	Preserve channel-link persistence.
//	Preserve DB-owned sent_at and lifecycle timestamp behavior.
//	Preserve soft-delete dismissal behavior.
//	Block deployment if this file breaks build, notification persistence,
//	notification lookup, type/channel resolution, channel linking,
//	or consumer communication integrity.
package data

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/PiccoloMondoC/sdworkspace/sdbackend/internal/logging"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// UserNotification represents a retained notification delivery/history record.
//
// Lifecycle contract:
//   - sent_at is DB-owned persisted lifecycle time.
//   - the record is immutable after creation.
//   - normal user-facing dismissal uses SoftDelete.
//   - hard Delete is reserved for explicit cleanup.
type UserNotification struct {
	ID                 uuid.UUID         `json:"id" db:"id"`
	UserID             uuid.UUID         `json:"user_id" db:"user_id"`
	OfferID            uuid.UUID         `json:"offer_id" db:"offer_id"`
	NotificationTypeID uuid.UUID         `json:"notification_type_id" db:"notification_type_id"`
	NotificationType   *NotificationType `json:"notification_type,omitempty"`
	SentAt             time.Time         `json:"sent_at" db:"sent_at"`
}

// NotificationType represents the type of a user notification such as "price_drop".
type NotificationType struct {
	ID          uuid.UUID `json:"id" db:"id"`
	Type        string    `json:"type" db:"type"`
	Description string    `json:"description,omitempty" db:"description"`
	IsActive    bool      `json:"is_active" db:"is_active"`
}

// NotificationChannel represents a notification delivery channel such as "email", "sms", or "in_app".
type NotificationChannel struct {
	ID          uuid.UUID `json:"id" db:"id"`
	Channel     string    `json:"channel" db:"channel"`
	Description string    `json:"description,omitempty" db:"description"`
	IsActive    bool      `json:"is_active" db:"is_active"`
}

// UserNotificationChannel links a persisted notification to a delivery channel.
type UserNotificationChannel struct {
	ID                 uuid.UUID `json:"id" db:"id"`
	UserNotificationID uuid.UUID `json:"user_notification_id" db:"user_notification_id"`
	ChannelID          uuid.UUID `json:"channel_id" db:"channel_id"`
}

// UserNotificationModel is the structure which holds the DB instance.
type UserNotificationModel struct {
	DB     *pgxpool.Pool
	Logger *logging.Logger
}

// NotificationTypeModel is the structure which holds the DB instance.
type NotificationTypeModel struct {
	DB     *pgxpool.Pool
	Logger *logging.Logger
}

// NotificationChannelModel is the structure which holds the DB instance.
type NotificationChannelModel struct {
	DB     *pgxpool.Pool
	Logger *logging.Logger
}

// scanUserNotificationFields scans any row-like value with Scan into a UserNotification.
func scanUserNotificationFields(row interface{ Scan(dest ...any) error }) (*UserNotification, error) {
	var notification UserNotification
	var notificationType NotificationType

	err := row.Scan(
		&notification.ID,
		&notification.UserID,
		&notification.OfferID,
		&notification.NotificationTypeID,
		&notificationType.ID,
		&notificationType.Type,
		&notificationType.Description,
		&notificationType.IsActive,
		&notification.SentAt,
	)
	if err != nil {
		return nil, err
	}

	notification.NotificationType = &notificationType
	return &notification, nil
}

// scanUserNotificationRow is for pgx.CollectRows.
func scanUserNotificationRow(row pgx.CollectableRow) (*UserNotification, error) {
	return scanUserNotificationFields(row)
}

// normalizeNotificationTypeName canonicalizes a notification type name for read/write lookups.
func normalizeNotificationTypeName(typeName string) string {
	return strings.ToLower(strings.TrimSpace(typeName))
}

// normalizeNotificationChannelName canonicalizes a notification channel name for read/write lookups.
func normalizeNotificationChannelName(channel string) string {
	return strings.ToLower(strings.TrimSpace(channel))
}

// resolveNotificationTypeID ensures an active notification type exists and returns its ID.
//
// This helper intentionally uses the incoming context as-is. The caller owns timeout policy.
func (m *UserNotificationModel) resolveNotificationTypeID(ctx context.Context, typeName string) (uuid.UUID, error) {
	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("resolveNotificationTypeID")

	typeName = normalizeNotificationTypeName(typeName)
	if typeName == "" {
		err := errors.New("notification type name is required")
		logger.Error("Validation failed", err)
		return uuid.Nil, err
	}

	var id uuid.UUID

	insertQuery := `
		INSERT INTO notification_types (type)
		VALUES ($1)
		ON CONFLICT (type) DO NOTHING
		RETURNING id
	`

	err := m.DB.QueryRow(ctx, insertQuery, typeName).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) || id == uuid.Nil {
		selectQuery := `
			SELECT id
			FROM notification_types
			WHERE type = $1
			  AND is_active = TRUE
		`
		err = m.DB.QueryRow(ctx, selectQuery, typeName).Scan(&id)
	}

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			logger.Warn("Notification type not found or inactive", "type_name", typeName)
			return uuid.Nil, ErrNotificationTypeNotFound
		}
		logger.Error("Failed to resolve notification type ID", err, "type_name", typeName)
		return uuid.Nil, err
	}

	logger.Debug("Resolved notification type ID", "type_name", typeName, "notification_type_id", id)
	return id, nil
}

// Insert inserts a new immutable notification delivery record.
//
// The database owns id and sent_at. Both are returned via RETURNING.
func (m *UserNotificationModel) Insert(ctx context.Context, notification *UserNotification) error {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("InsertUserNotification")

	if notification == nil {
		err := errors.New("notification is required")
		logger.Error("Validation failed", err)
		return err
	}

	if notification.UserID == uuid.Nil {
		err := errors.New("user_id is required")
		logger.Error("Validation failed", err)
		return err
	}

	if notification.OfferID == uuid.Nil {
		err := errors.New("offer_id is required")
		logger.Error("Validation failed", err)
		return err
	}

	notificationTypeID := notification.NotificationTypeID
	if notificationTypeID == uuid.Nil {
		if notification.NotificationType == nil {
			err := errors.New("notification type is required")
			logger.Error("Validation failed", err)
			return err
		}

		resolvedID, err := m.resolveNotificationTypeID(ctx, notification.NotificationType.Type)
		if err != nil {
			logger.Error("Could not resolve notification type ID", err)
			return err
		}
		notificationTypeID = resolvedID
	}

	query := `
		INSERT INTO user_notifications (
			user_id,
			offer_id,
			notification_type_id
		)
		VALUES ($1, $2, $3)
		RETURNING id, sent_at
	`

	err := m.DB.QueryRow(ctx, query, notification.UserID, notification.OfferID, notificationTypeID).Scan(
		&notification.ID,
		&notification.SentAt,
	)
	if err != nil {
		logger.Error(
			"Insert user notification failed",
			err,
			"user_id", notification.UserID,
			"offer_id", notification.OfferID,
			"notification_type_id", notificationTypeID,
		)
		return err
	}

	notification.NotificationTypeID = notificationTypeID

	logger.Info("Insert user notification successful", "notification_id", notification.ID)
	return nil
}

// GetByID retrieves one active notification by ID.
func (m *UserNotificationModel) GetByID(ctx context.Context, id uuid.UUID) (*UserNotification, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("GetUserNotificationByID")

	if id == uuid.Nil {
		err := errors.New("notification ID is required")
		logger.Error("Validation failed", err)
		return nil, err
	}

	query := `
		SELECT
			un.id,
			un.user_id,
			un.offer_id,
			un.notification_type_id,
			nt.id,
			nt.type,
			nt.description,
			nt.is_active,
			un.sent_at
		FROM user_notifications un
		INNER JOIN notification_types nt ON nt.id = un.notification_type_id
		WHERE un.id = $1
		  AND un.deleted_at IS NULL
		  AND nt.is_active = TRUE
	`

	notification, err := scanUserNotificationFields(m.DB.QueryRow(ctx, query, id))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			logger.Warn("User notification not found", "notification_id", id)
			return nil, ErrUserNotificationNotFound
		}
		logger.Error("Get user notification failed", err, "notification_id", id)
		return nil, err
	}

	logger.Info("Get user notification successful", "notification_id", notification.ID)
	return notification, nil
}

// GetByUserID retrieves active notifications for a user, newest first.
func (m *UserNotificationModel) GetByUserID(ctx context.Context, userID uuid.UUID) ([]*UserNotification, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("GetByUserID")

	if userID == uuid.Nil {
		err := errors.New("user ID is required")
		logger.Error("Validation failed", err)
		return nil, err
	}

	query := `
		SELECT
			un.id,
			un.user_id,
			un.offer_id,
			un.notification_type_id,
			nt.id,
			nt.type,
			nt.description,
			nt.is_active,
			un.sent_at
		FROM user_notifications un
		INNER JOIN notification_types nt ON nt.id = un.notification_type_id
		WHERE un.user_id = $1
		  AND un.deleted_at IS NULL
		  AND nt.is_active = TRUE
		ORDER BY un.sent_at DESC
	`

	rows, err := m.DB.Query(ctx, query, userID)
	if err != nil {
		logger.Error("Query failed", err, "user_id", userID)
		return nil, err
	}
	defer rows.Close()

	notifications, err := pgx.CollectRows(rows, scanUserNotificationRow)
	if err != nil {
		logger.Error("Collect rows failed", err, "user_id", userID)
		return nil, err
	}

	logger.Info("Retrieved user notifications successfully", "user_id", userID, "count", len(notifications))
	return notifications, nil
}

// GetByOfferID retrieves active notifications for an offer, newest first.
func (m *UserNotificationModel) GetByOfferID(ctx context.Context, offerID uuid.UUID) ([]*UserNotification, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("GetByOfferID")

	if offerID == uuid.Nil {
		err := errors.New("offer ID is required")
		logger.Error("Validation failed", err)
		return nil, err
	}

	query := `
		SELECT
			un.id,
			un.user_id,
			un.offer_id,
			un.notification_type_id,
			nt.id,
			nt.type,
			nt.description,
			nt.is_active,
			un.sent_at
		FROM user_notifications un
		INNER JOIN notification_types nt ON nt.id = un.notification_type_id
		WHERE un.offer_id = $1
		  AND un.deleted_at IS NULL
		  AND nt.is_active = TRUE
		ORDER BY un.sent_at DESC
	`

	rows, err := m.DB.Query(ctx, query, offerID)
	if err != nil {
		logger.Error("Query failed", err, "offer_id", offerID)
		return nil, err
	}
	defer rows.Close()

	notifications, err := pgx.CollectRows(rows, scanUserNotificationRow)
	if err != nil {
		logger.Error("Collect rows failed", err, "offer_id", offerID)
		return nil, err
	}

	logger.Info("Retrieved user notifications by offer ID", "offer_id", offerID, "count", len(notifications))
	return notifications, nil
}

// GetByType retrieves active notifications for a type, newest first.
func (m *UserNotificationModel) GetByType(ctx context.Context, notificationType string) ([]*UserNotification, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("GetByType")

	notificationType = normalizeNotificationTypeName(notificationType)
	if notificationType == "" {
		err := errors.New("notification type is required")
		logger.Error("Validation failed", err)
		return nil, err
	}

	query := `
		SELECT
			un.id,
			un.user_id,
			un.offer_id,
			un.notification_type_id,
			nt.id,
			nt.type,
			nt.description,
			nt.is_active,
			un.sent_at
		FROM user_notifications un
		INNER JOIN notification_types nt ON nt.id = un.notification_type_id
		WHERE nt.type = $1
		  AND un.deleted_at IS NULL
		  AND nt.is_active = TRUE
		ORDER BY un.sent_at DESC
	`

	rows, err := m.DB.Query(ctx, query, notificationType)
	if err != nil {
		logger.Error("Query failed", err, "notification_type", notificationType)
		return nil, err
	}
	defer rows.Close()

	notifications, err := pgx.CollectRows(rows, scanUserNotificationRow)
	if err != nil {
		logger.Error("Collect rows failed", err, "notification_type", notificationType)
		return nil, err
	}

	logger.Info("Retrieved user notifications by type", "notification_type", notificationType, "count", len(notifications))
	return notifications, nil
}

// Update is intentionally rejected because user notifications are immutable delivery/history rows.
func (m *UserNotificationModel) Update(ctx context.Context, notification *UserNotification) error {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("UpdateUserNotification")

	_ = notification

	logger.Error("Update user notification rejected", ErrUserNotificationImmutable)
	return ErrUserNotificationImmutable
}

// SoftDelete dismisses a notification without destroying its retained history row.
func (m *UserNotificationModel) SoftDelete(ctx context.Context, id uuid.UUID) error {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("SoftDeleteUserNotification")

	if id == uuid.Nil {
		err := errors.New("notification ID is required")
		logger.Error("Validation failed", err)
		return err
	}

	query := `
		UPDATE user_notifications
		SET deleted_at = NOW(),
		    updated_at = NOW()
		WHERE id = $1
		  AND deleted_at IS NULL
		RETURNING id
	`

	var deletedID uuid.UUID
	err := m.DB.QueryRow(ctx, query, id).Scan(&deletedID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			logger.Warn("User notification not found for soft delete", "notification_id", id)
			return ErrUserNotificationNotFound
		}
		logger.Error("Soft delete user notification failed", err, "notification_id", id)
		return err
	}

	logger.Info("Soft delete user notification successful", "notification_id", deletedID)
	return nil
}

// Delete permanently removes a notification row.
// This remains an exceptional cleanup path.
func (m *UserNotificationModel) Delete(ctx context.Context, id uuid.UUID) error {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("DeleteUserNotification")

	if id == uuid.Nil {
		err := errors.New("notification ID is required")
		logger.Error("Validation failed", err)
		return err
	}

	query := `
		DELETE FROM user_notifications
		WHERE id = $1
		RETURNING id
	`

	var deletedID uuid.UUID
	err := m.DB.QueryRow(ctx, query, id).Scan(&deletedID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			logger.Warn("User notification not found for delete", "notification_id", id)
			return ErrUserNotificationNotFound
		}
		logger.Error("Delete user notification failed", err, "notification_id", id)
		return err
	}

	logger.Info("Delete user notification successful", "notification_id", deletedID)
	return nil
}

// ResolveIDByName resolves an active notification type ID by canonical name,
// creating it if it does not already exist.
func (m *NotificationTypeModel) ResolveIDByName(ctx context.Context, typeName string) (uuid.UUID, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	helper := UserNotificationModel{
		DB:     m.DB,
		Logger: m.Logger,
	}

	return helper.resolveNotificationTypeID(ctx, typeName)
}

// ResolveIDByName resolves an active notification channel ID by canonical name,
// creating it if it does not already exist.
func (m *NotificationChannelModel) ResolveIDByName(ctx context.Context, channel string) (uuid.UUID, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("ResolveNotificationChannelIDByName")

	channel = normalizeNotificationChannelName(channel)
	if channel == "" {
		err := errors.New("channel name is required")
		logger.Error("Validation failed", err)
		return uuid.Nil, err
	}

	var id uuid.UUID

	insertQuery := `
		INSERT INTO notification_channels (channel)
		VALUES ($1)
		ON CONFLICT (channel) DO NOTHING
		RETURNING id
	`

	err := m.DB.QueryRow(ctx, insertQuery, channel).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) || id == uuid.Nil {
		selectQuery := `
			SELECT id
			FROM notification_channels
			WHERE channel = $1
			  AND is_active = TRUE
		`
		err = m.DB.QueryRow(ctx, selectQuery, channel).Scan(&id)
	}

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			logger.Warn("Notification channel not found or inactive", "channel", channel)
			return uuid.Nil, ErrNotificationChannelNotFound
		}
		logger.Error("Failed to resolve notification channel ID", err, "channel", channel)
		return uuid.Nil, err
	}

	logger.Debug("Resolved notification channel ID", "channel", channel, "channel_id", id)
	return id, nil
}

// InsertChannelLink is the single canonical path for writing user_notification_channels.
func (m *UserNotificationModel) InsertChannelLink(ctx context.Context, link *UserNotificationChannel) error {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("InsertChannelLink")

	if link == nil {
		err := errors.New("link is required")
		logger.Error("Validation failed", err)
		return err
	}

	if link.UserNotificationID == uuid.Nil || link.ChannelID == uuid.Nil {
		err := errors.New("user_notification_id and channel_id are required")
		logger.Error("Validation failed", err)
		return err
	}

	query := `
		INSERT INTO user_notification_channels (
			user_notification_id,
			channel_id
		)
		VALUES ($1, $2)
		RETURNING id
	`

	err := m.DB.QueryRow(ctx, query, link.UserNotificationID, link.ChannelID).Scan(&link.ID)
	if err != nil {
		logger.Error(
			"Insert channel link failed",
			err,
			"user_notification_id", link.UserNotificationID,
			"channel_id", link.ChannelID,
		)
		return err
	}

	logger.Info(
		"Insert channel link successful",
		"link_id", link.ID,
		"user_notification_id", link.UserNotificationID,
		"channel_id", link.ChannelID,
	)
	return nil
}
