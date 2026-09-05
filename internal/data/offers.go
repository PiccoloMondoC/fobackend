// Package data provides models and database access methods for offers and other entities.
//
// File: sdworkspace/sdbackend/internal/data/offers.go
//
// GTM:
//
//	Layer: 2.5 Catalog / Offer Domain
//	Release Class: SPINE
//	Reason:
//	  Offers are the canonical release-critical commerce object for the
//	  Platform. This file owns the Offer struct, OfferModel, canonical
//	  scan contract, core offer persistence, public visibility contract,
//	  internal/admin offer reads, soft-delete lifecycle, and hard-delete
//	  maintenance path used by the split offer-domain files.
//
// SPINE Rule:
//
//	Keep compiling.
//	Keep production-ready.
//	Preserve canonical Offer and OfferModel ownership.
//	Preserve offers table alignment.
//	Preserve money/decimal string policy.
//	Preserve canonical scan contract.
//	Preserve public visibility filtering.
//	Preserve DB-owned lifecycle timestamp behavior.
//	Block deployment if this file breaks build, offer persistence,
//	public offer reads, offer lifecycle behavior, or catalog integrity.
package data

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/PiccoloMondoC/focodebase/fobackend/internal/logging"
	"github.com/PiccoloMondoC/focodebase/fobackend/internal/utils/timeutil"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Sentinel errors returned by OfferModel methods.
var (
	// ErrOfferNotFound is returned when no active/non-deleted offer matches the
	// requested lookup or mutation target.
	ErrOfferNotFound = errors.New("offer not found")
)

// Offer represents the canonical offers table shape.
// This struct must remain aligned with database.go.
//
// Numeric policy:
// Money/decimal fields mirror NUMERIC columns and must not use float types.
type Offer struct {
	ID                  uuid.UUID  `json:"id" db:"id"`
	OfferKey            string     `json:"offer_key" db:"offer_key"`
	Type                string     `json:"type" db:"type"`
	Title               string     `json:"title" db:"title"`
	Description         *string    `json:"description,omitempty" db:"description"`
	ImageURL            *string    `json:"image_url,omitempty" db:"image_url"`
	AffiliateURL        string     `json:"affiliate_url" db:"affiliate_url"`
	Price               *string    `json:"price,omitempty" db:"price"`
	StartingPrice       *string    `json:"starting_price,omitempty" db:"starting_price"`
	ListPrice           *string    `json:"list_price,omitempty" db:"list_price"`
	Currency            string     `json:"currency" db:"currency"`
	DiscountPercent     *string    `json:"discount_percent,omitempty" db:"discount_percent"`
	CouponCode          *string    `json:"coupon_code,omitempty" db:"coupon_code"`
	ProductID           *uuid.UUID `json:"product_id,omitempty" db:"product_id"`
	MerchantID          uuid.UUID  `json:"merchant_id" db:"merchant_id"`
	CategoryID          uuid.UUID  `json:"category_id" db:"category_id"`
	AvgRating           string     `json:"avg_rating" db:"avg_rating"`
	IsEditorialApproved bool       `json:"is_editorial_approved" db:"is_editorial_approved"`
	StatusID            *uuid.UUID `json:"status_id,omitempty" db:"status_id"`
	IsActive            bool       `json:"is_active" db:"is_active"`
	DeletedAt           *time.Time `json:"-" db:"deleted_at"`
	ExpiresAt           *time.Time `json:"expires_at,omitempty" db:"expires_at"`
	PublishedAt         *time.Time `json:"published_at,omitempty" db:"published_at"`
	CreatedAt           time.Time  `json:"created_at" db:"created_at"`
	UpdatedAt           time.Time  `json:"updated_at" db:"updated_at"`
}

// OfferModel holds the DB instance for canonical offer persistence.
type OfferModel struct {
	DB     *pgxpool.Pool
	Logger *logging.Logger
}

// offerSelectColumns is the canonical offer scan list used across split files.
const offerSelectColumns = "id, offer_key, type, title, description, image_url, affiliate_url, price, starting_price, list_price, currency, discount_percent, coupon_code, product_id, merchant_id, category_id, avg_rating, is_editorial_approved, status_id, is_active, deleted_at, expires_at, published_at, created_at, updated_at"

// offerScanDestinations returns the canonical positional scan destination list
// for offerSelectColumns.
//
// This helper exists so every query that selects offerSelectColumns can share
// one authoritative scan contract. That prevents maintenance drift between
// scanOffer(...) and any query that appends additional joined columns after the
// canonical offer selection.
func offerScanDestinations(d *Offer) []any {
	return []any{
		&d.ID,
		&d.OfferKey,
		&d.Type,
		&d.Title,
		&d.Description,
		&d.ImageURL,
		&d.AffiliateURL,
		&d.Price,
		&d.StartingPrice,
		&d.ListPrice,
		&d.Currency,
		&d.DiscountPercent,
		&d.CouponCode,
		&d.ProductID,
		&d.MerchantID,
		&d.CategoryID,
		&d.AvgRating,
		&d.IsEditorialApproved,
		&d.StatusID,
		&d.IsActive,
		&d.DeletedAt,
		&d.ExpiresAt,
		&d.PublishedAt,
		&d.CreatedAt,
		&d.UpdatedAt,
	}
}

// scanOffer scans a single offer row using the canonical destination contract.
func scanOffer(row interface {
	Scan(dest ...any) error
}, d *Offer) error {
	return row.Scan(offerScanDestinations(d)...)
}

func mustHTTPURL(raw string) error {
	u, err := url.ParseRequestURI(strings.TrimSpace(raw))
	if err != nil {
		return err
	}

	if u.Scheme != "http" && u.Scheme != "https" {
		return errors.New("URL must use http or https")
	}

	if u.Host == "" {
		return errors.New("URL host is required")
	}

	return nil
}

func validateOfferType(v string) error {
	switch v {
	case "deal", "trend":
		return nil
	default:
		return errors.New("offer type must be either 'deal' or 'trend'")
	}
}

func defaultPage(limit, offset int) (int, int) {
	if limit <= 0 {
		limit = 10
	}
	if limit > 200 {
		limit = 200
	}
	if offset < 0 {
		offset = 0
	}
	return limit, offset
}

func buildPublicOfferVisibilityClause(argStart int) (string, int) {
	return fmt.Sprintf(`
		is_active = TRUE
		AND is_editorial_approved = TRUE
		AND deleted_at IS NULL
		AND published_at IS NOT NULL
		AND (expires_at IS NULL OR expires_at > $%d)
	`, argStart), argStart + 1
}

// Insert inserts a new offer using the canonical offers schema.
//
// Time-source rule:
// created_at, updated_at, published_at, and insert-time is_active are DB-owned.
func (m *OfferModel) Insert(ctx context.Context, d *Offer) error {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	log := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("InsertOffer")

	if d == nil {
		err := errors.New("offer payload is required")
		log.Error("Validation failed", "error", err)
		return err
	}

	d.OfferKey = strings.ToLower(strings.TrimSpace(d.OfferKey))
	d.Type = strings.ToLower(strings.TrimSpace(d.Type))
	d.Title = strings.TrimSpace(d.Title)
	d.AffiliateURL = strings.TrimSpace(d.AffiliateURL)
	d.Currency = strings.ToUpper(strings.TrimSpace(d.Currency))
	d.Description = normalizeOptionalString(d.Description)
	d.ImageURL = normalizeOptionalString(d.ImageURL)
	d.CouponCode = normalizeOptionalString(d.CouponCode)
	d.Price = normalizeOptionalString(d.Price)
	d.StartingPrice = normalizeOptionalString(d.StartingPrice)
	d.ListPrice = normalizeOptionalString(d.ListPrice)
	d.DiscountPercent = normalizeOptionalString(d.DiscountPercent)

	if d.OfferKey == "" {
		err := errors.New("offer_key is required")
		log.Error("Validation failed", "error", err)
		return err
	}
	if err := validateOfferType(d.Type); err != nil {
		log.Error("Validation failed", "error", err)
		return err
	}
	if d.Title == "" {
		err := errors.New("title is required")
		log.Error("Validation failed", "error", err)
		return err
	}
	if d.AffiliateURL == "" {
		err := errors.New("affiliate_url is required")
		log.Error("Validation failed", "error", err)
		return err
	}
	if err := mustHTTPURL(d.AffiliateURL); err != nil {
		err = fmt.Errorf("invalid affiliate_url: %w", err)
		log.Error("Validation failed", "error", err)
		return err
	}
	if d.ImageURL != nil {
		if err := mustHTTPURL(*d.ImageURL); err != nil {
			err = fmt.Errorf("invalid image_url: %w", err)
			log.Error("Validation failed", "error", err)
			return err
		}
	}
	if d.MerchantID == uuid.Nil {
		err := errors.New("merchant_id is required")
		log.Error("Validation failed", "error", err)
		return err
	}
	if d.CategoryID == uuid.Nil {
		err := errors.New("category_id is required")
		log.Error("Validation failed", "error", err)
		return err
	}
	if len(d.Currency) != 3 {
		err := errors.New("currency must be a valid 3-letter ISO code")
		log.Error("Validation failed", "error", err)
		return err
	}
	if d.AvgRating == "" {
		err := errors.New("avg_rating is required")
		log.Error("Validation failed", "error", err)
		return err
	}
	if d.DeletedAt != nil {
		err := errors.New("deleted_at must not be supplied on insert")
		log.Error("Validation failed", "error", err)
		return err
	}
	if !d.IsEditorialApproved && d.PublishedAt != nil {
		err := errors.New("published_at cannot be supplied when is_editorial_approved is false")
		log.Error("Validation failed", "error", err)
		return err
	}

	if d.ID == uuid.Nil {
		d.ID = uuid.New()
	}

	if d.StatusID == nil {
		defaultStatusName := "pending_review"
		if d.IsEditorialApproved {
			defaultStatusName = "approved"
		}

		statusID, err := getOfferStatusIDByName(ctx, m.DB, defaultStatusName)
		if err != nil {
			log.Error("Resolve default offer status failed", "error", err, "status_name", defaultStatusName)
			return fmt.Errorf("resolve default offer status: %w", err)
		}
		d.StatusID = &statusID
	}

	const q = `
		INSERT INTO offers (
			id,
			offer_key,
			type,
			title,
			description,
			image_url,
			affiliate_url,
			price,
			starting_price,
			list_price,
			currency,
			discount_percent,
			coupon_code,
			product_id,
			merchant_id,
			category_id,
			avg_rating,
			is_editorial_approved,
			status_id,
			is_active,
			deleted_at,
			expires_at,
			published_at
		) VALUES (
			$1,$2,$3,$4,$5,$6,$7,$8,$9,$10,
			$11,$12,$13,$14,$15,$16,$17,$18,$19,
			CASE
				WHEN $18 = TRUE
				 AND $21 IS NULL
				 AND ($22 IS NULL OR $22 > NOW())
				THEN TRUE
				ELSE FALSE
			END,
			$20,
			$21,
			CASE
				WHEN $18 = TRUE
				 AND $23 IS NULL
				 AND ($22 IS NULL OR $22 > NOW())
				THEN NOW()
				ELSE $23
			END
		)
		RETURNING created_at, updated_at, published_at, is_active
	`

	err := m.DB.QueryRow(
		ctx,
		q,
		d.ID,
		d.OfferKey,
		d.Type,
		d.Title,
		d.Description,
		d.ImageURL,
		d.AffiliateURL,
		d.Price,
		d.StartingPrice,
		d.ListPrice,
		d.Currency,
		d.DiscountPercent,
		d.CouponCode,
		d.ProductID,
		d.MerchantID,
		d.CategoryID,
		d.AvgRating,
		d.IsEditorialApproved,
		d.StatusID,
		d.DeletedAt,
		d.ExpiresAt,
		d.PublishedAt,
		d.PublishedAt,
	).Scan(&d.CreatedAt, &d.UpdatedAt, &d.PublishedAt, &d.IsActive)
	if err != nil {
		log.Error("Insert offer failed", "error", err, "offer_id", d.ID, "offer_key", d.OfferKey)
		return fmt.Errorf("insert offer: %w", err)
	}

	log.Info("Offer inserted", "offer_id", d.ID, "offer_key", d.OfferKey)
	return nil
}

// Update updates mutable canonical offer fields.
// Workflow transitions such as approval/rejection live in offer-review.go.
//
// Time-source rule:
// updated_at is DB-owned and therefore written with NOW() in SQL.
func (m *OfferModel) Update(ctx context.Context, d *Offer) error {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	log := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("UpdateOffer")

	if d == nil {
		err := errors.New("offer payload is required")
		log.Error("Validation failed", "error", err)
		return err
	}
	if d.ID == uuid.Nil {
		err := errors.New("offer ID is required")
		log.Error("Validation failed", "error", err)
		return err
	}

	d.OfferKey = strings.ToLower(strings.TrimSpace(d.OfferKey))
	d.Type = strings.ToLower(strings.TrimSpace(d.Type))
	d.Title = strings.TrimSpace(d.Title)
	d.AffiliateURL = strings.TrimSpace(d.AffiliateURL)
	d.Currency = strings.ToUpper(strings.TrimSpace(d.Currency))
	d.Description = normalizeOptionalString(d.Description)
	d.ImageURL = normalizeOptionalString(d.ImageURL)
	d.CouponCode = normalizeOptionalString(d.CouponCode)
	d.Price = normalizeOptionalString(d.Price)
	d.StartingPrice = normalizeOptionalString(d.StartingPrice)
	d.ListPrice = normalizeOptionalString(d.ListPrice)
	d.DiscountPercent = normalizeOptionalString(d.DiscountPercent)

	if d.OfferKey == "" {
		err := errors.New("offer_key is required")
		log.Error("Validation failed", "error", err)
		return err
	}
	if err := validateOfferType(d.Type); err != nil {
		log.Error("Validation failed", "error", err)
		return err
	}
	if d.Title == "" {
		err := errors.New("title is required")
		log.Error("Validation failed", "error", err)
		return err
	}
	if d.AffiliateURL == "" {
		err := errors.New("affiliate_url is required")
		log.Error("Validation failed", "error", err)
		return err
	}
	if err := mustHTTPURL(d.AffiliateURL); err != nil {
		err = fmt.Errorf("invalid affiliate_url: %w", err)
		log.Error("Validation failed", "error", err)
		return err
	}
	if d.ImageURL != nil {
		if err := mustHTTPURL(*d.ImageURL); err != nil {
			err = fmt.Errorf("invalid image_url: %w", err)
			log.Error("Validation failed", "error", err)
			return err
		}
	}
	if d.MerchantID == uuid.Nil {
		err := errors.New("merchant_id is required")
		log.Error("Validation failed", "error", err)
		return err
	}
	if d.CategoryID == uuid.Nil {
		err := errors.New("category_id is required")
		log.Error("Validation failed", "error", err)
		return err
	}
	if len(d.Currency) != 3 {
		err := errors.New("currency must be a valid 3-letter ISO code")
		log.Error("Validation failed", "error", err)
		return err
	}
	if d.AvgRating == "" {
		err := errors.New("avg_rating is required")
		log.Error("Validation failed", "error", err)
		return err
	}

	const q = `
		UPDATE offers
		SET offer_key = $1,
			type = $2,
			title = $3,
			description = $4,
			image_url = $5,
			affiliate_url = $6,
			price = $7,
			starting_price = $8,
			list_price = $9,
			currency = $10,
			discount_percent = $11,
			coupon_code = $12,
			product_id = $13,
			merchant_id = $14,
			category_id = $15,
			expires_at = $16,
			updated_at = NOW()
		WHERE id = $17
		  AND deleted_at IS NULL
		RETURNING updated_at
	`

	err := m.DB.QueryRow(
		ctx,
		q,
		d.OfferKey,
		d.Type,
		d.Title,
		d.Description,
		d.ImageURL,
		d.AffiliateURL,
		d.Price,
		d.StartingPrice,
		d.ListPrice,
		d.Currency,
		d.DiscountPercent,
		d.CouponCode,
		d.ProductID,
		d.MerchantID,
		d.CategoryID,
		d.ExpiresAt,
		d.ID,
	).Scan(&d.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			log.Warn("Offer not found for update", "offer_id", d.ID)
			return ErrOfferNotFound
		}
		log.Error("Update offer failed", "error", err, "offer_id", d.ID)
		return fmt.Errorf("update offer: %w", err)
	}

	log.Info("Offer updated", "offer_id", d.ID, "offer_key", d.OfferKey)
	return nil
}

// GetByID retrieves a single non-deleted offer for internal use.
func (m *OfferModel) GetByID(ctx context.Context, id uuid.UUID) (*Offer, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	log := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("GetOfferByID")

	if id == uuid.Nil {
		err := errors.New("offer ID is required")
		log.Error("Validation failed", "error", err)
		return nil, err
	}

	q := fmt.Sprintf(`
		SELECT %s
		FROM offers
		WHERE id = $1
		  AND deleted_at IS NULL
		LIMIT 1
	`, offerSelectColumns)

	var d Offer
	err := scanOffer(m.DB.QueryRow(ctx, q, id), &d)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			log.Warn("Offer not found by ID", "offer_id", id)
			return nil, ErrOfferNotFound
		}
		log.Error("Get offer by ID failed", "error", err, "offer_id", id)
		return nil, fmt.Errorf("get offer by id: %w", err)
	}

	log.Info("Offer retrieved by ID", "offer_id", d.ID, "offer_key", d.OfferKey)
	return &d, nil
}

// GetByOfferKey retrieves a single non-deleted offer by offer_key for internal use.
func (m *OfferModel) GetByOfferKey(ctx context.Context, offerKey string) (*Offer, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	log := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("GetOfferByKey")

	offerKey = strings.ToLower(strings.TrimSpace(offerKey))
	if offerKey == "" {
		err := errors.New("offer_key is required")
		log.Error("Validation failed", "error", err)
		return nil, err
	}

	q := fmt.Sprintf(`
		SELECT %s
		FROM offers
		WHERE offer_key = $1
		  AND deleted_at IS NULL
		LIMIT 1
	`, offerSelectColumns)

	var d Offer
	err := scanOffer(m.DB.QueryRow(ctx, q, offerKey), &d)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			log.Warn("Offer not found by key", "offer_key", offerKey)
			return nil, ErrOfferNotFound
		}
		log.Error("Get offer by key failed", "error", err, "offer_key", offerKey)
		return nil, fmt.Errorf("get offer by key: %w", err)
	}

	log.Info("Offer retrieved by key", "offer_id", d.ID, "offer_key", d.OfferKey)
	return &d, nil
}

// GetLiveOfferByID retrieves a single public-visible offer by ID.
func (m *OfferModel) GetLiveOfferByID(ctx context.Context, id uuid.UUID) (*Offer, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	log := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("GetLiveOfferByID")

	if id == uuid.Nil {
		err := errors.New("offer ID is required")
		log.Error("Validation failed", "error", err)
		return nil, err
	}

	now := timeutil.Now()
	clause, _ := buildPublicOfferVisibilityClause(2)

	q := fmt.Sprintf(`
		SELECT %s
		FROM offers
		WHERE id = $1
		  AND %s
		LIMIT 1
	`, offerSelectColumns, clause)

	var d Offer
	err := scanOffer(m.DB.QueryRow(ctx, q, id, now), &d)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			log.Warn("Live offer not found by ID", "offer_id", id)
			return nil, ErrOfferNotFound
		}
		log.Error("Get live offer by ID failed", "error", err, "offer_id", id)
		return nil, fmt.Errorf("get live offer by id: %w", err)
	}

	log.Info("Live offer retrieved by ID", "offer_id", d.ID, "offer_key", d.OfferKey)
	return &d, nil
}

// GetLiveOfferByKey retrieves a single public-visible offer by offer_key.
func (m *OfferModel) GetLiveOfferByKey(ctx context.Context, offerKey string) (*Offer, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	log := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("GetLiveOfferByKey")

	offerKey = strings.ToLower(strings.TrimSpace(offerKey))
	if offerKey == "" {
		err := errors.New("offer_key is required")
		log.Error("Validation failed", "error", err)
		return nil, err
	}

	now := timeutil.Now()
	clause, _ := buildPublicOfferVisibilityClause(2)

	q := fmt.Sprintf(`
		SELECT %s
		FROM offers
		WHERE offer_key = $1
		  AND %s
		LIMIT 1
	`, offerSelectColumns, clause)

	var d Offer
	err := scanOffer(m.DB.QueryRow(ctx, q, offerKey, now), &d)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			log.Warn("Live offer not found by key", "offer_key", offerKey)
			return nil, ErrOfferNotFound
		}
		log.Error("Get live offer by key failed", "error", err, "offer_key", offerKey)
		return nil, fmt.Errorf("get live offer by key: %w", err)
	}

	log.Info("Live offer retrieved by key", "offer_id", d.ID, "offer_key", d.OfferKey)
	return &d, nil
}

// GetLiveOffers returns paginated public-visible offers.
func (m *OfferModel) GetLiveOffers(ctx context.Context, limit, offset int) ([]*Offer, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	log := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("GetLiveOffers")

	limit, offset = defaultPage(limit, offset)

	now := timeutil.Now()
	clause, nextArg := buildPublicOfferVisibilityClause(1)

	q := fmt.Sprintf(`
		SELECT %s
		FROM offers
		WHERE %s
		ORDER BY published_at DESC, created_at DESC
		LIMIT $%d OFFSET $%d
	`, offerSelectColumns, clause, nextArg, nextArg+1)

	rows, err := m.DB.Query(ctx, q, now, limit, offset)
	if err != nil {
		log.Error("Query execution failed", "error", err, "limit", limit, "offset", offset)
		return nil, fmt.Errorf("get live offers: %w", err)
	}

	offers, err := collectOffers(rows)
	if err != nil {
		log.Error("Row collection failed", "error", err, "limit", limit, "offset", offset)
		return nil, fmt.Errorf("collect live offers: %w", err)
	}

	log.Info("Retrieved live offers", "count", len(offers), "limit", limit, "offset", offset)
	return offers, nil
}

// GetByProductID retrieves non-deleted offers by product ID for internal use.
func (m *OfferModel) GetByProductID(ctx context.Context, productID uuid.UUID, limit, offset int) ([]*Offer, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	log := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("GetOffersByProductID")

	if productID == uuid.Nil {
		err := errors.New("product ID is required")
		log.Error("Validation failed", "error", err)
		return nil, err
	}

	limit, offset = defaultPage(limit, offset)

	q := fmt.Sprintf(`
		SELECT %s
		FROM offers
		WHERE product_id = $1
		  AND deleted_at IS NULL
		ORDER BY created_at DESC
		LIMIT $2 OFFSET $3
	`, offerSelectColumns)

	rows, err := m.DB.Query(ctx, q, productID, limit, offset)
	if err != nil {
		log.Error("Query execution failed", "error", err, "product_id", productID, "limit", limit, "offset", offset)
		return nil, fmt.Errorf("get offers by product id: %w", err)
	}

	offers, err := collectOffers(rows)
	if err != nil {
		log.Error("Row collection failed", "error", err, "product_id", productID, "limit", limit, "offset", offset)
		return nil, fmt.Errorf("collect offers by product id: %w", err)
	}

	log.Info("Retrieved offers by product ID", "product_id", productID, "count", len(offers), "limit", limit, "offset", offset)
	return offers, nil
}

// GetAll retrieves internal/admin offers with optional whitelisted filters.
func (m *OfferModel) GetAll(ctx context.Context, filters map[string]any, limit, offset int) ([]*Offer, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	log := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("GetAllOffers")

	limit, offset = defaultPage(limit, offset)

	var (
		sb   strings.Builder
		args []any
		argN = 1
	)

	fmt.Fprintf(&sb, "SELECT %s FROM offers WHERE deleted_at IS NULL", offerSelectColumns)

	for k, v := range filters {
		switch k {
		case "merchant_id", "status_id", "product_id":
			fmt.Fprintf(&sb, " AND %s = $%d", k, argN)
			args = append(args, v)
			argN++
		case "category_id":
			fmt.Fprintf(&sb, " AND category_id = $%d", argN)
			args = append(args, v)
			argN++
		case "type":
			fmt.Fprintf(&sb, " AND type = $%d", argN)
			args = append(args, v)
			argN++
		case "is_active", "is_editorial_approved":
			fmt.Fprintf(&sb, " AND %s = $%d", k, argN)
			args = append(args, v)
			argN++
		case "title":
			title, ok := v.(string)
			if !ok {
				continue
			}
			title = strings.TrimSpace(title)
			if title == "" {
				continue
			}
			fmt.Fprintf(&sb, " AND title ILIKE $%d", argN)
			args = append(args, "%"+title+"%")
			argN++
		case "min_price":
			fmt.Fprintf(&sb, " AND price IS NOT NULL AND price >= $%d", argN)
			args = append(args, v)
			argN++
		case "max_price":
			fmt.Fprintf(&sb, " AND price IS NOT NULL AND price <= $%d", argN)
			args = append(args, v)
			argN++
		}
	}

	fmt.Fprintf(&sb, " ORDER BY created_at DESC LIMIT $%d OFFSET $%d", argN, argN+1)
	args = append(args, limit, offset)

	rows, err := m.DB.Query(ctx, sb.String(), args...)
	if err != nil {
		log.Error("Query execution failed", "error", err, "limit", limit, "offset", offset)
		return nil, fmt.Errorf("get all offers: %w", err)
	}

	offers, err := collectOffers(rows)
	if err != nil {
		log.Error("Row collection failed", "error", err, "limit", limit, "offset", offset)
		return nil, fmt.Errorf("collect all offers: %w", err)
	}

	log.Info("Retrieved offers", "count", len(offers), "limit", limit, "offset", offset)
	return offers, nil
}

// SoftDelete performs canonical logical removal.
//
// Time-source rule:
// deleted_at and updated_at are DB-owned persisted lifecycle timestamps.
func (m *OfferModel) SoftDelete(ctx context.Context, id uuid.UUID) error {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	log := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("SoftDeleteOffer")

	if id == uuid.Nil {
		err := errors.New("offer ID is required")
		log.Error("Validation failed", "error", err)
		return err
	}

	const q = `
		UPDATE offers
		SET deleted_at = NOW(),
			is_active = FALSE,
			updated_at = NOW()
		WHERE id = $1
		  AND deleted_at IS NULL
	`

	res, err := m.DB.Exec(ctx, q, id)
	if err != nil {
		log.Error("Soft delete offer failed", "error", err, "offer_id", id)
		return fmt.Errorf("soft delete offer: %w", err)
	}
	if res.RowsAffected() == 0 {
		log.Warn("Offer not found for soft delete", "offer_id", id)
		return ErrOfferNotFound
	}

	log.Info("Offer soft deleted", "offer_id", id)
	return nil
}

// Delete performs a true hard delete.
func (m *OfferModel) Delete(ctx context.Context, id uuid.UUID) error {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	log := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("DeleteOffer")

	if id == uuid.Nil {
		err := errors.New("offer ID is required")
		log.Error("Validation failed", "error", err)
		return err
	}

	res, err := m.DB.Exec(ctx, `DELETE FROM offers WHERE id = $1`, id)
	if err != nil {
		log.Error("Delete offer failed", "error", err, "offer_id", id)
		return fmt.Errorf("delete offer: %w", err)
	}
	if res.RowsAffected() == 0 {
		log.Warn("Offer not found for hard delete", "offer_id", id)
		return ErrOfferNotFound
	}

	log.Info("Offer hard deleted", "offer_id", id)
	return nil
}
