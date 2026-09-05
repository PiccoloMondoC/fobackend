// Package data provides models and database access methods for offer price history and related entities.
//
// File: sdworkspace/sdbackend/internal/data/offer_price_history.go
//
// GTM:
//
//	Layer: 2.5 Catalog / Offer Domain
//	Release Class: DEFERRED
//	Reason:
//	  Offer price history and price-drop subscriptions are release-critical
//	  catalog/value infrastructure. They preserve immutable offer price history,
//	  support price-drop discovery, enable user price-drop intent, and provide
//	  the pricing evidence needed for trust-first public offer behavior.
//
// SPINE Rule:
//
//	Keep compiling.
//	Keep production-ready.
//	Preserve immutable price-history semantics.
//	Preserve NUMERIC-safe decimal string behavior.
//	Preserve DB-owned id and recorded_at lifecycle behavior.
//	Preserve price-drop subscription persistence.
//	Preserve anomaly routing through offer_flags.
//	Block deployment if this file breaks build, price history persistence,
//	price-drop support, subscription behavior, or catalog pricing integrity.
package data

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/PiccoloMondoC/focodebase/fobackend/internal/logging"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	offerPriceHistorySelectColumns     = "id, offer_id, price::text, recorded_at"
	priceDropSubscriptionSelectColumns = "id, user_id, offer_id, threshold::text, created_at"
)

// OfferPriceHistory represents an immutable historical price record for an offer.
//
// Price is modeled as a canonical decimal string to avoid floating-point loss while still
// mapping cleanly to PostgreSQL NUMERIC(19,4).
type OfferPriceHistory struct {
	ID         uuid.UUID `json:"id"          db:"id"`
	OfferID    uuid.UUID `json:"offer_id"    db:"offer_id"`
	Price      string    `json:"price"       db:"price"`
	RecordedAt time.Time `json:"recorded_at" db:"recorded_at"`
}

// PriceDropSubscription represents a user's subscription to a price threshold for an offer.
//
// Threshold is modeled as a canonical decimal string to preserve NUMERIC(19,4) semantics.
type PriceDropSubscription struct {
	ID        uuid.UUID `json:"id"         db:"id"`
	UserID    uuid.UUID `json:"user_id"    db:"user_id"`
	OfferID   uuid.UUID `json:"offer_id"   db:"offer_id"`
	Threshold string    `json:"threshold"  db:"threshold"`
	CreatedAt time.Time `json:"created_at" db:"created_at"`
}

// OfferPriceHistoryModel holds the connection pool and logger for price history operations.
type OfferPriceHistoryModel struct {
	DB     *pgxpool.Pool
	Logger *logging.Logger
}

// Insert inserts a new offer price history record into the database.
//
// The database is authoritative for id and recorded_at via DEFAULT/RETURNING.
// Price must be a valid non-negative decimal string.
func (m *OfferPriceHistoryModel) Insert(ctx context.Context, history *OfferPriceHistory) error {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("InsertOfferPriceHistory")

	if history == nil {
		err := errors.New("history is required")
		logger.Error("Validation failed", err)
		return err
	}

	if history.OfferID == uuid.Nil {
		err := errors.New("offer_id is required")
		logger.Error("Validation failed", err)
		return err
	}

	if err := validateNonNegativeDecimalString(history.Price, "price"); err != nil {
		logger.Error("Validation failed", err)
		return err
	}

	query := `
		INSERT INTO offer_price_history (offer_id, price)
		VALUES ($1, $2)
		RETURNING id, offer_id, price::text, recorded_at
	`

	if err := m.DB.QueryRow(ctx, query, history.OfferID, strings.TrimSpace(history.Price)).Scan(
		&history.ID,
		&history.OfferID,
		&history.Price,
		&history.RecordedAt,
	); err != nil {
		logger.Error("Insert offer price history failed", err)
		return err
	}

	logger.Info("Insert offer price history successful",
		"id", history.ID,
		"offer_id", history.OfferID,
		"price", history.Price,
		"recorded_at", history.RecordedAt,
	)

	return nil
}

// InsertSubscription creates a new price drop subscription or updates only its threshold if one already exists.
//
// On conflict, only threshold is updated. created_at is preserved as the original creation timestamp.
func (m *OfferPriceHistoryModel) InsertSubscription(ctx context.Context, userID uuid.UUID, offerID uuid.UUID, threshold string) error {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("InsertPriceDropSubscription")

	if userID == uuid.Nil {
		err := errors.New("user_id is required")
		logger.Error("Validation failed", err)
		return err
	}

	if offerID == uuid.Nil {
		err := errors.New("offer_id is required")
		logger.Error("Validation failed", err)
		return err
	}

	if err := validatePositiveDecimalString(threshold, "threshold"); err != nil {
		logger.Error("Validation failed", err)
		return err
	}

	query := `
		INSERT INTO price_drop_subscriptions (user_id, offer_id, threshold)
		VALUES ($1, $2, $3)
		ON CONFLICT (user_id, offer_id)
		DO UPDATE SET threshold = EXCLUDED.threshold
	`

	if _, err := m.DB.Exec(ctx, query, userID, offerID, strings.TrimSpace(threshold)); err != nil {
		logger.Error("Insert or update subscription failed", err)
		return err
	}

	logger.Info("Price drop subscription saved",
		"user_id", userID,
		"offer_id", offerID,
		"threshold", strings.TrimSpace(threshold),
	)

	return nil
}

// GetSubscription retrieves a user's subscription for a specific offer.
//
// Returns ErrPriceDropSubscriptionNotFound when no matching subscription exists.
func (m *OfferPriceHistoryModel) GetSubscription(ctx context.Context, userID uuid.UUID, offerID uuid.UUID) (*PriceDropSubscription, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("GetPriceDropSubscription")

	if userID == uuid.Nil {
		err := errors.New("user_id is required")
		logger.Error("Validation failed", err)
		return nil, err
	}

	if offerID == uuid.Nil {
		err := errors.New("offer_id is required")
		logger.Error("Validation failed", err)
		return nil, err
	}

	query := fmt.Sprintf(`
		SELECT %s
		FROM price_drop_subscriptions
		WHERE user_id = $1 AND offer_id = $2
	`, priceDropSubscriptionSelectColumns)

	var subscription PriceDropSubscription
	err := m.DB.QueryRow(ctx, query, userID, offerID).Scan(
		&subscription.ID,
		&subscription.UserID,
		&subscription.OfferID,
		&subscription.Threshold,
		&subscription.CreatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			logger.Warn("Price drop subscription not found", "user_id", userID, "offer_id", offerID)
			return nil, ErrPriceDropSubscriptionNotFound
		}
		logger.Error("Query price drop subscription failed", err)
		return nil, err
	}

	logger.Info("Retrieved price drop subscription successfully",
		"id", subscription.ID,
		"user_id", subscription.UserID,
		"offer_id", subscription.OfferID,
	)

	return &subscription, nil
}

// GetByID retrieves an offer price history record by its primary key.
//
// Returns ErrOfferPriceHistoryNotFound when no record exists for the given id.
func (m *OfferPriceHistoryModel) GetByID(ctx context.Context, id uuid.UUID) (*OfferPriceHistory, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("GetOfferPriceHistoryByID")

	if id == uuid.Nil {
		err := errors.New("id is required")
		logger.Error("Validation failed", err)
		return nil, err
	}

	query := fmt.Sprintf(`
		SELECT %s
		FROM offer_price_history
		WHERE id = $1
	`, offerPriceHistorySelectColumns)

	var history OfferPriceHistory
	err := m.DB.QueryRow(ctx, query, id).Scan(
		&history.ID,
		&history.OfferID,
		&history.Price,
		&history.RecordedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			logger.Warn("Offer price history not found", "id", id)
			return nil, ErrOfferPriceHistoryNotFound
		}
		logger.Error("Query offer price history failed", err)
		return nil, err
	}

	logger.Info("Retrieved offer price history successfully",
		"id", history.ID,
		"offer_id", history.OfferID,
		"price", history.Price,
		"recorded_at", history.RecordedAt,
	)

	return &history, nil
}

// GetByOfferID retrieves all price history records for a specific offer, ordered newest to oldest.
//
// Returns an empty slice when no records exist. Does not return ErrOfferPriceHistoryNotFound.
func (m *OfferPriceHistoryModel) GetByOfferID(ctx context.Context, offerID uuid.UUID) ([]OfferPriceHistory, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("GetOfferPriceHistoryByOfferID")

	if offerID == uuid.Nil {
		err := errors.New("offer_id is required")
		logger.Error("Validation failed", err)
		return nil, err
	}

	query := fmt.Sprintf(`
		SELECT %s
		FROM offer_price_history
		WHERE offer_id = $1
		ORDER BY recorded_at DESC, id DESC
	`, offerPriceHistorySelectColumns)

	rows, err := m.DB.Query(ctx, query, offerID)
	if err != nil {
		logger.Error("Query offer price history by offer failed", err)
		return nil, err
	}

	historyRecords, err := pgx.CollectRows(rows, pgx.RowToStructByPos[OfferPriceHistory])
	if err != nil {
		logger.Error("Collect offer price history rows failed", err)
		return nil, err
	}

	logger.Info("Retrieved offer price history successfully",
		"offer_id", offerID,
		"record_count", len(historyRecords),
	)

	return historyRecords, nil
}

// Update is intentionally prohibited because offer price history is an
// immutable historical time-series record.
//
// Corrections must be represented as new authoritative price points via Insert
// or ManuallyAdjustPrice rather than rewriting prior history rows.
func (m *OfferPriceHistoryModel) Update(ctx context.Context, history *OfferPriceHistory) error {
	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("UpdateOfferPriceHistory")

	if history == nil {
		err := errors.New("history is required")
		logger.Error("Validation failed", err)
		return err
	}

	if history.OfferID == uuid.Nil {
		err := errors.New("offer_id is required")
		logger.Error("Validation failed", err)
		return err
	}

	logger.Warn("Offer price history update rejected because price history records are immutable",
		"id", history.ID,
		"offer_id", history.OfferID,
	)
	return ErrOfferPriceHistoryImmutable
}

// Delete permanently removes an offer price history record by its primary key.
//
// This is an explicit administrative cleanup operation, not a normal business
// mutation path. Historical price records do not support SoftDelete semantics.
//
// Returns ErrOfferPriceHistoryNotFound when no record exists for the given id.
func (m *OfferPriceHistoryModel) Delete(ctx context.Context, id uuid.UUID) error {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("DeleteOfferPriceHistory")

	if id == uuid.Nil {
		err := errors.New("id is required")
		logger.Error("Validation failed", err)
		return err
	}

	var deletedID uuid.UUID
	err := m.DB.QueryRow(ctx, `
		DELETE FROM offer_price_history
		WHERE id = $1
		RETURNING id
	`, id).Scan(&deletedID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			logger.Warn("Delete attempted on non-existent offer price history", "id", id)
			return ErrOfferPriceHistoryNotFound
		}
		logger.Error("Delete offer price history failed", err)
		return err
	}

	logger.Info("Delete offer price history successful", "id", deletedID)
	return nil
}

// DeleteSubscription removes a user's subscription to a specific offer's price drop alerts.
//
// Returns ErrPriceDropSubscriptionNotFound when no matching subscription exists.
func (m *OfferPriceHistoryModel) DeleteSubscription(ctx context.Context, userID uuid.UUID, offerID uuid.UUID) error {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("DeletePriceDropSubscription")

	if userID == uuid.Nil {
		err := errors.New("user_id is required")
		logger.Error("Validation failed", err)
		return err
	}

	if offerID == uuid.Nil {
		err := errors.New("offer_id is required")
		logger.Error("Validation failed", err)
		return err
	}

	result, err := m.DB.Exec(ctx,
		`DELETE FROM price_drop_subscriptions WHERE user_id = $1 AND offer_id = $2`,
		userID, offerID,
	)
	if err != nil {
		logger.Error("Delete subscription failed", err)
		return err
	}

	if result.RowsAffected() == 0 {
		logger.Warn("Price drop subscription not found for delete", "user_id", userID, "offer_id", offerID)
		return ErrPriceDropSubscriptionNotFound
	}

	logger.Info("Price drop subscription deleted",
		"user_id", userID,
		"offer_id", offerID,
	)

	return nil
}

// GetLatestPrice retrieves the most recently recorded price for an offer as a decimal string.
//
// Returns ErrOfferPriceHistoryNotFound when no price history exists for the offer.
func (m *OfferPriceHistoryModel) GetLatestPrice(ctx context.Context, offerID uuid.UUID) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("GetLatestOfferPrice")

	if offerID == uuid.Nil {
		err := errors.New("offer_id is required")
		logger.Error("Validation failed", err)
		return "", err
	}

	query := `
		SELECT price::text
		FROM offer_price_history
		WHERE offer_id = $1
		ORDER BY recorded_at DESC, id DESC
		LIMIT 1
	`

	var price string
	err := m.DB.QueryRow(ctx, query, offerID).Scan(&price)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			logger.Warn("No price history found for offer", "offer_id", offerID)
			return "", ErrOfferPriceHistoryNotFound
		}
		logger.Error("Query latest price failed", err)
		return "", err
	}

	logger.Info("Retrieved latest price successfully", "offer_id", offerID, "price", price)
	return price, nil
}

// GetPriceTrend retrieves price history for an offer within the provided lookback duration,
// ordered oldest to newest for time-series consumption.
//
// duration must be strictly positive. Returns an empty slice when no records fall within the window.
func (m *OfferPriceHistoryModel) GetPriceTrend(ctx context.Context, offerID uuid.UUID, duration time.Duration) ([]OfferPriceHistory, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("GetOfferPriceTrend")

	if offerID == uuid.Nil {
		err := errors.New("offer_id is required")
		logger.Error("Validation failed", err)
		return nil, err
	}

	if duration <= 0 {
		err := errors.New("duration must be greater than zero")
		logger.Error("Validation failed", err)
		return nil, err
	}

	startTime := time.Now().UTC().Add(-duration)

	query := fmt.Sprintf(`
		SELECT %s
		FROM offer_price_history
		WHERE offer_id = $1 AND recorded_at >= $2
		ORDER BY recorded_at ASC, id ASC
	`, offerPriceHistorySelectColumns)

	rows, err := m.DB.Query(ctx, query, offerID, startTime)
	if err != nil {
		logger.Error("Query offer price trend failed", err)
		return nil, err
	}

	priceHistory, err := pgx.CollectRows(rows, pgx.RowToStructByPos[OfferPriceHistory])
	if err != nil {
		logger.Error("Collect offer price trend rows failed", err)
		return nil, err
	}

	logger.Info("Retrieved offer price trend successfully",
		"offer_id", offerID,
		"duration", duration,
		"record_count", len(priceHistory),
	)

	return priceHistory, nil
}

// TriggerPriceDropNotificationForUser stores or updates a user's price-drop threshold subscription.
//
// Notification delivery itself belongs in the service layer. This method only persists the subscription.
func (m *OfferPriceHistoryModel) TriggerPriceDropNotificationForUser(ctx context.Context, userID uuid.UUID, offerID uuid.UUID, threshold string) error {
	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("TriggerPriceDropNotificationForUser")

	if err := m.InsertSubscription(ctx, userID, offerID, threshold); err != nil {
		logger.Error("Trigger price drop notification subscription failed", err)
		return err
	}

	logger.Info("Trigger price drop notification subscription saved",
		"user_id", userID,
		"offer_id", offerID,
		"threshold", strings.TrimSpace(threshold),
	)

	return nil
}

// ManuallyAdjustPrice records a new authoritative price point as an append-only history insert.
//
// It does not mutate prior history rows. newPrice must be a valid non-negative decimal string.
func (m *OfferPriceHistoryModel) ManuallyAdjustPrice(ctx context.Context, offerID uuid.UUID, newPrice string) error {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("ManuallyAdjustOfferPrice")

	if offerID == uuid.Nil {
		err := errors.New("offer_id is required")
		logger.Error("Validation failed", err)
		return err
	}

	if err := validateNonNegativeDecimalString(newPrice, "new price"); err != nil {
		logger.Error("Validation failed", err)
		return err
	}

	history := &OfferPriceHistory{
		OfferID: offerID,
		Price:   strings.TrimSpace(newPrice),
	}

	if err := m.Insert(ctx, history); err != nil {
		logger.Error("Manual price adjustment failed", err)
		return err
	}

	logger.Info("Manual price adjustment successful",
		"id", history.ID,
		"offer_id", history.OfferID,
		"new_price", history.Price,
		"recorded_at", history.RecordedAt,
	)

	return nil
}

// FlagPriceAnomaly records a price anomaly flag against the offer using the canonical offer_flags table.
//
// The offer_price_history table has no is_anomaly column. Anomaly signals are persisted
// as offer_flags rows with reason = 'price_anomaly'.
func (m *OfferPriceHistoryModel) FlagPriceAnomaly(ctx context.Context, offerID uuid.UUID) error {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("FlagOfferPriceAnomaly")

	if offerID == uuid.Nil {
		err := errors.New("offer_id is required")
		logger.Error("Validation failed", err)
		return err
	}

	query := `
		INSERT INTO offer_flags (offer_id, reason)
		VALUES ($1, $2)
	`

	if _, err := m.DB.Exec(ctx, query, offerID, "price_anomaly"); err != nil {
		logger.Error("Flag price anomaly failed", err)
		return err
	}

	logger.Info("Flagged offer price anomaly successfully", "offer_id", offerID)
	return nil
}

// GetOffersWithSignificantPriceDrops returns the latest history row for offers whose latest price
// has dropped by at least the provided threshold compared to the immediately preceding recorded price.
//
// This uses a latest-vs-previous comparison via ROW_NUMBER(), not a comparison against historical max.
// threshold must be a valid strictly-positive decimal string.
func (m *OfferPriceHistoryModel) GetOffersWithSignificantPriceDrops(ctx context.Context, threshold string) ([]OfferPriceHistory, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("GetOffersWithSignificantPriceDrops")

	if err := validatePositiveDecimalString(threshold, "threshold"); err != nil {
		logger.Error("Validation failed", err)
		return nil, err
	}

	query := `
		WITH ranked AS (
			SELECT
				id,
				offer_id,
				price,
				recorded_at,
				ROW_NUMBER() OVER (PARTITION BY offer_id ORDER BY recorded_at DESC, id DESC) AS rn
			FROM offer_price_history
		),
		latest AS (
			SELECT offer_id, id, price, recorded_at
			FROM ranked
			WHERE rn = 1
		),
		previous AS (
			SELECT offer_id, price
			FROM ranked
			WHERE rn = 2
		)
		SELECT
			l.id,
			l.offer_id,
			l.price::text,
			l.recorded_at
		FROM latest l
		JOIN previous p ON p.offer_id = l.offer_id
		WHERE (p.price - l.price) >= $1::numeric
		ORDER BY l.recorded_at DESC, l.id DESC
	`

	rows, err := m.DB.Query(ctx, query, strings.TrimSpace(threshold))
	if err != nil {
		logger.Error("Query significant price drops failed", err)
		return nil, err
	}

	histories, err := pgx.CollectRows(rows, pgx.RowToStructByPos[OfferPriceHistory])
	if err != nil {
		logger.Error("Collect significant price drop rows failed", err)
		return nil, err
	}

	logger.Info("Retrieved offers with significant price drops successfully", "count", len(histories))
	return histories, nil
}

// MonitorPriceChanges does not belong in the data layer. External polling is a service concern.
//
// This method returns an explicit error to make the misuse visible rather than silently no-oping.
func (m *OfferPriceHistoryModel) MonitorPriceChanges(_ context.Context) error {
	return errors.New("MonitorPriceChanges belongs in the service layer, not the data layer")
}

// GetSubscribersForPriceDrop returns subscriptions for an offer whose configured
// threshold is met by the current price.
//
// A subscription threshold is met when currentPrice is less than or equal to the
// user's threshold.
func (m *OfferPriceHistoryModel) GetSubscribersForPriceDrop(ctx context.Context, offerID uuid.UUID, currentPrice string) ([]PriceDropSubscription, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("GetSubscribersForPriceDrop")

	if offerID == uuid.Nil {
		err := errors.New("offer_id is required")
		logger.Error("Validation failed", err)
		return nil, err
	}

	if err := validatePositiveDecimalString(currentPrice, "current price"); err != nil {
		logger.Error("Validation failed", err)
		return nil, err
	}

	query := fmt.Sprintf(`
		SELECT %s
		FROM price_drop_subscriptions
		WHERE offer_id = $1
		  AND $2::numeric <= threshold
		ORDER BY created_at ASC, id ASC
	`, priceDropSubscriptionSelectColumns)

	rows, err := m.DB.Query(ctx, query, offerID, strings.TrimSpace(currentPrice))
	if err != nil {
		logger.Error("Query price-drop subscribers failed", err)
		return nil, err
	}

	subscriptions, err := pgx.CollectRows(rows, pgx.RowToStructByPos[PriceDropSubscription])
	if err != nil {
		logger.Error("Collect price-drop subscribers failed", err)
		return nil, err
	}

	logger.Info("Retrieved price-drop subscribers",
		"offer_id", offerID,
		"current_price", strings.TrimSpace(currentPrice),
		"subscriber_count", len(subscriptions),
	)

	return subscriptions, nil
}

// DeleteExpiredSubscriptions deletes price-drop subscriptions created before cutoff.
//
// This is a scheduled cleanup helper for deferred internal automation.
func (m *OfferPriceHistoryModel) DeleteExpiredSubscriptions(ctx context.Context, cutoff time.Time) (int, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("DeleteExpiredSubscriptions")

	rows, err := m.DB.Query(ctx, `
		DELETE FROM price_drop_subscriptions
		WHERE created_at < $1
		RETURNING id
	`, cutoff)
	if err != nil {
		logger.Error("Delete expired price-drop subscriptions failed", err)
		return 0, err
	}

	deletedIDs, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
	if err != nil {
		logger.Error("Collect deleted price-drop subscription ids failed", err)
		return 0, err
	}

	logger.Info("Deleted expired price-drop subscriptions",
		"deleted_count", len(deletedIDs),
		"cutoff", cutoff,
	)

	return len(deletedIDs), nil
}
