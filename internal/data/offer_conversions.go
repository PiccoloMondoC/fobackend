// Package data provides models and database access methods for offer conversions and other entities.
//
// sdworkspace/sdbackend/internal/data/offer_conversions.go
//
// GTM:
//   Layer: 2.5 Catalog / Offer Domain
//   Release Class: DEFERRED
//   Reason:
//     Offer conversions are valid affiliate analytics infrastructure, but they
//     are not required for the initial SagrentiDeals release spine because
//     conversion reporting is expected to come from affiliate networks first.
//     The v1 spine requires immutable offer-click tracking before expanding
//     into internal conversion attribution.
//
// DEFERRED Rule:
//   Keep compiling.
//   Keep safe.
//   Preserve immutable conversion-event semantics.
//   Preserve DB-owned id and converted_at lifecycle behavior.
//   Preserve bounded read paths.
//   Preserve LogOfferConversion as the canonical write entry point.
//   Do not add new features.
//   Do not route into v1 UI/API expansion.
//   Do not block deployment on this file unless it breaks the build.
package data

import (
	"context"
	"errors"
	"time"

	"github.com/PiccoloMondoC/sdworkspace/sdbackend/internal/logging"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const offerConversionSelectColumns = "id, offer_id, ip_address, user_agent, referrer, converted_at"

// OfferConversion represents a successful conversion (e.g., purchase or signup) from an offer click.
type OfferConversion struct {
	ID          uuid.UUID `json:"id"                   db:"id"`
	OfferID     uuid.UUID `json:"offer_id"             db:"offer_id"`
	IPAddress   *string   `json:"ip_address,omitempty" db:"ip_address"` // Nullable canonical text IP address.
	UserAgent   *string   `json:"user_agent,omitempty" db:"user_agent"` // Nullable user agent.
	Referrer    *string   `json:"referrer,omitempty"   db:"referrer"`   // Nullable referrer URL.
	ConvertedAt time.Time `json:"converted_at"         db:"converted_at"`
}

// OfferConversionCount represents an aggregate conversion count grouped by offer.
type OfferConversionCount struct {
	OfferID         uuid.UUID `json:"offer_id"`
	ConversionCount int64     `json:"conversion_count"`
}

// OfferConversionModel is the structure which holds the DB instance.
type OfferConversionModel struct {
	DB     *pgxpool.Pool
	Logger *logging.Logger
}

// offerConversionScan scans the current row into an OfferConversion.
func offerConversionScan(scanner interface {
	Scan(dest ...any) error
}) (*OfferConversion, error) {
	var conv OfferConversion

	err := scanner.Scan(
		&conv.ID,
		&conv.OfferID,
		&conv.IPAddress,
		&conv.UserAgent,
		&conv.Referrer,
		&conv.ConvertedAt,
	)
	if err != nil {
		return nil, err
	}

	return &conv, nil
}

// Insert inserts a new offer conversion entry into the database.
// The database owns id and converted_at; both fields are populated on the
// provided struct via RETURNING after a successful insert.
func (m *OfferConversionModel) Insert(ctx context.Context, conv *OfferConversion) error {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("InsertOfferConversion")

	if conv == nil {
		err := errors.New("offer conversion is required")
		logger.Error("Validation failed", err)
		return err
	}

	if conv.OfferID == uuid.Nil {
		err := errors.New("offer_id is required")
		logger.Error("Validation failed", err)
		return err
	}

	query := `
		INSERT INTO offer_conversions
		(
			offer_id,
			ip_address,
			user_agent,
			referrer
		)
		VALUES ($1, $2, $3, $4)
		RETURNING id, converted_at
	`

	err := m.DB.QueryRow(
		ctx,
		query,
		conv.OfferID,
		conv.IPAddress,
		conv.UserAgent,
		conv.Referrer,
	).Scan(
		&conv.ID,
		&conv.ConvertedAt,
	)
	if err != nil {
		logger.Error("Insert offer conversion failed", err)
		return err
	}

	logger.Info("Insert offer conversion successful", "offer_conversion_id", conv.ID)

	return nil
}

// Update is intentionally prohibited because offer-conversion records are
// immutable historical events. Any caller attempting to mutate a conversion
// record will receive ErrOfferConversionImmutable. This method exists
// explicitly at the model surface to make the immutability contract
// unambiguous rather than relying on the absence of a method.
func (m *OfferConversionModel) Update(ctx context.Context, conv *OfferConversion) error {
	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("UpdateOfferConversion")

	if conv == nil {
		err := errors.New("offer conversion is required")
		logger.Error("Validation failed", err)
		return err
	}

	if conv.ID == uuid.Nil {
		err := errors.New("id is required for update")
		logger.Error("Validation failed", err)
		return err
	}

	logger.Warn("Offer conversion update rejected because conversion records are immutable", "offer_conversion_id", conv.ID)
	return ErrOfferConversionImmutable
}

// GetByID retrieves an offer conversion by its ID.
// Returns ErrOfferConversionNotFound when no row matches.
func (m *OfferConversionModel) GetByID(ctx context.Context, id uuid.UUID) (*OfferConversion, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("GetOfferConversionByID")

	if id == uuid.Nil {
		err := errors.New("id is required")
		logger.Error("Validation failed", err)
		return nil, err
	}

	query := `
		SELECT ` + offerConversionSelectColumns + `
		FROM offer_conversions
		WHERE id = $1
	`

	conv, err := offerConversionScan(m.DB.QueryRow(ctx, query, id))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			logger.Warn("Offer conversion not found", "offer_conversion_id", id)
			return nil, ErrOfferConversionNotFound
		}

		logger.Error("Query execution failed", err)
		return nil, err
	}

	logger.Info("Get offer conversion successful", "offer_conversion_id", id)

	return conv, nil
}

// GetByOfferID retrieves paginated offer conversion records for a given offer ID,
// ordered by converted_at DESC. Both limit and offset are validated; callers must
// supply valid values rather than relying on silent clamping.
func (m *OfferConversionModel) GetByOfferID(ctx context.Context, offerID uuid.UUID, limit, offset int) ([]*OfferConversion, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("GetByOfferID")

	if offerID == uuid.Nil {
		err := errors.New("offer_id is required")
		logger.Error("Validation failed", err)
		return nil, err
	}

	if limit <= 0 {
		err := errors.New("limit must be greater than zero")
		logger.Error("Validation failed", err, "limit", limit)
		return nil, err
	}

	if limit > 100 {
		err := errors.New("limit must be less than or equal to 100")
		logger.Error("Validation failed", err, "limit", limit)
		return nil, err
	}

	if offset < 0 {
		err := errors.New("offset must be greater than or equal to zero")
		logger.Error("Validation failed", err, "offset", offset)
		return nil, err
	}

	query := `
		SELECT ` + offerConversionSelectColumns + `
		FROM offer_conversions
		WHERE offer_id = $1
		ORDER BY converted_at DESC
		LIMIT $2 OFFSET $3
	`

	rows, err := m.DB.Query(ctx, query, offerID, limit, offset)
	if err != nil {
		logger.Error("Query failed", err, "offer_id", offerID, "limit", limit, "offset", offset)
		return nil, err
	}

	conversions, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (*OfferConversion, error) {
		return offerConversionScan(row)
	})
	if err != nil {
		logger.Error("Row collection failed", err, "offer_id", offerID)
		return nil, err
	}

	logger.Info("Offer conversions retrieved", "offer_id", offerID, "count", len(conversions), "limit", limit, "offset", offset)

	return conversions, nil
}

// Delete permanently removes an offer conversion record by ID.
//
// This is an explicit administrative cleanup operation, not a normal business
// mutation path. Offer-conversion records are immutable historical events and
// do not support SoftDelete semantics.
//
// Returns ErrOfferConversionNotFound when no row matches.
func (m *OfferConversionModel) Delete(ctx context.Context, id uuid.UUID) error {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("DeleteOfferConversion")

	if id == uuid.Nil {
		err := errors.New("id is required")
		logger.Error("Validation failed", err)
		return err
	}

	var deletedID uuid.UUID
	err := m.DB.QueryRow(ctx, `
		DELETE FROM offer_conversions
		WHERE id = $1
		RETURNING id
	`, id).Scan(&deletedID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			logger.Warn("Offer conversion not found for delete", "offer_conversion_id", id)
			return ErrOfferConversionNotFound
		}
		logger.Error("Delete offer conversion failed", err, "offer_conversion_id", id)
		return err
	}

	logger.Info("Delete offer conversion successful", "offer_conversion_id", deletedID)

	return nil
}

// DeleteByOfferID permanently removes all offer conversion entries for a given offer ID.
//
// This is an explicit administrative cleanup operation, not a normal business
// mutation path. It is intended for offer retirement or data-retention
// enforcement workflows only.
func (m *OfferConversionModel) DeleteByOfferID(ctx context.Context, offerID uuid.UUID) error {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("DeleteByOfferID")

	if offerID == uuid.Nil {
		err := errors.New("offer_id is required")
		logger.Error("Validation failed", err)
		return err
	}

	result, err := m.DB.Exec(ctx, `
		DELETE FROM offer_conversions
		WHERE offer_id = $1
	`, offerID)
	if err != nil {
		logger.Error("Delete offer conversions failed", err, "offer_id", offerID)
		return err
	}

	logger.Info(
		"Deleted offer conversions by offer ID",
		"offer_id", offerID,
		"rows_affected", result.RowsAffected(),
	)

	return nil
}

// CountByOfferID returns the total number of conversions for a given offer ID.
func (m *OfferConversionModel) CountByOfferID(ctx context.Context, offerID uuid.UUID) (int64, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("CountByOfferID")

	if offerID == uuid.Nil {
		err := errors.New("offer_id is required")
		logger.Error("Validation failed", err)
		return 0, err
	}

	var count int64
	err := m.DB.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM offer_conversions
		WHERE offer_id = $1
	`, offerID).Scan(&count)
	if err != nil {
		logger.Error("Count query failed", err)
		return 0, err
	}

	logger.Info("Count query successful", "offer_id", offerID, "count", count)

	return count, nil
}

// GetRecentConversions retrieves the most recent offer conversions up to the specified limit.
func (m *OfferConversionModel) GetRecentConversions(ctx context.Context, limit int) ([]*OfferConversion, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("GetRecentConversions")

	if limit <= 0 {
		err := errors.New("limit must be greater than zero")
		logger.Error("Validation failed", err)
		return nil, err
	}

	if limit > 100 {
		err := errors.New("limit must be less than or equal to 100")
		logger.Error("Validation failed", err, "limit", limit)
		return nil, err
	}

	rows, err := m.DB.Query(ctx, `
		SELECT `+offerConversionSelectColumns+`
		FROM offer_conversions
		ORDER BY converted_at DESC
		LIMIT $1
	`, limit)
	if err != nil {
		logger.Error("Query failed", err)
		return nil, err
	}

	conversions, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (*OfferConversion, error) {
		return offerConversionScan(row)
	})
	if err != nil {
		logger.Error("Row collection failed", err)
		return nil, err
	}

	logger.Info("Retrieved recent offer conversions", "count", len(conversions))

	return conversions, nil
}

// LogOfferConversion logs a new offer conversion using the provided OfferConversion struct.
// It delegates to Insert so the package has one canonical conversion write path.
func (m *OfferConversionModel) LogOfferConversion(ctx context.Context, conv *OfferConversion) error {
	return m.Insert(ctx, conv)
}

// GetMostConvertedOffers retrieves the top N most converted offers within a recent timeframe.
// It returns a slice of OfferConversionCount ordered by conversion count descending.
func (m *OfferConversionModel) GetMostConvertedOffers(ctx context.Context, limit int, since time.Time) ([]OfferConversionCount, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("GetMostConvertedOffers")

	if limit <= 0 {
		err := errors.New("limit must be greater than zero")
		logger.Error("Validation failed", err)
		return nil, err
	}

	if limit > 100 {
		err := errors.New("limit must be less than or equal to 100")
		logger.Error("Validation failed", err, "limit", limit)
		return nil, err
	}

	if since.IsZero() {
		err := errors.New("since is required")
		logger.Error("Validation failed", err)
		return nil, err
	}

	rows, err := m.DB.Query(ctx, `
		SELECT offer_id, COUNT(*) AS conversion_count
		FROM offer_conversions
		WHERE converted_at >= $1
		GROUP BY offer_id
		ORDER BY conversion_count DESC
		LIMIT $2
	`, since.UTC(), limit)
	if err != nil {
		logger.Error("Query failed", err)
		return nil, err
	}

	results, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (OfferConversionCount, error) {
		var r OfferConversionCount
		err := row.Scan(&r.OfferID, &r.ConversionCount)
		return r, err
	})
	if err != nil {
		logger.Error("Row collection failed", err)
		return nil, err
	}

	logger.Info("Retrieved most converted offers", "count", len(results))

	return results, nil
}
