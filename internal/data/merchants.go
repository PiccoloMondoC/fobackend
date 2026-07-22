// Package data provides models and database access methods for merchants and other entities.
//
// sdworkspace/sdbackend/internal/data/merchants.go
//
// GTM:
//   Layer: 2.4 Merchant / Affiliate Domain
//   Release Class: SPINE
//   Reason:
//     Merchants, merchant types, merchant-affiliate program relationships, and
//     platforms are release-critical merchant/catalog infrastructure. They
//     support merchant identity, merchant classification, affiliate-program
//     linkage, platform lookup, offer ownership, and monetization routing for
//     the initial Platform release spine.
//
// SPINE Rule:
//   Keep compiling.
//   Keep production-ready.
//   Preserve merchant identity and slug behavior.
//   Preserve merchant type classification.
//   Preserve merchant-affiliate program relationship integrity.
//   Preserve public-safe affiliate program summary reads.
//   Preserve platform lookup behavior.
//   Preserve soft-delete lifecycle semantics.
//   Block deployment if this file breaks build, merchant persistence,
//   offer ownership, affiliate-program linkage, platform lookup,
//   or merchant/catalog integrity.
package data

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/PiccoloMondoC/sdworkspace/sdbackend/internal/logging"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Merchant struct {
	ID             uuid.UUID  `json:"id" db:"id"`
	MerchantTypeID uuid.UUID  `json:"merchant_type_id" db:"merchant_type_id"`
	Name           string     `json:"name" db:"name"`
	DisplayName    string     `json:"display_name" db:"display_name"`
	Slug           string     `json:"slug" db:"slug"`
	LogoURL        *string    `json:"logo_url,omitempty" db:"logo_url"`
	Website        *string    `json:"website,omitempty" db:"website"`
	PlatformID     *uuid.UUID `json:"platform_id,omitempty" db:"platform_id"`
	DeletedAt      *time.Time `json:"-"                    db:"deleted_at"`
	CreatedAt      time.Time  `json:"created_at" db:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at" db:"updated_at"`
}

type MerchantType struct {
	ID          uuid.UUID  `json:"id" db:"id"`
	Name        string     `json:"name" db:"name"`
	Description string     `json:"description" db:"description"`
	DeletedAt   *time.Time `json:"deleted_at,omitempty" db:"deleted_at"`
	CreatedAt   time.Time  `json:"created_at" db:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at" db:"updated_at"`
}

type Platform struct {
	ID          uuid.UUID  `json:"id" db:"id"`
	Name        string     `json:"name" db:"name"`
	Description *string    `json:"description,omitempty" db:"description"`
	Website     *string    `json:"website,omitempty" db:"website"`
	DeletedAt   *time.Time `json:"deleted_at,omitempty" db:"deleted_at"`
	CreatedAt   time.Time  `json:"created_at" db:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at" db:"updated_at"`
}

type MerchantAffiliateProgram struct {
	MerchantID         uuid.UUID  `json:"merchant_id" db:"merchant_id"`
	AffiliateProgramID uuid.UUID  `json:"affiliate_program_id" db:"affiliate_program_id"`
	DeletedAt          *time.Time `json:"deleted_at,omitempty" db:"deleted_at"`
	CreatedAt          time.Time  `json:"created_at" db:"created_at"`
	UpdatedAt          time.Time  `json:"updated_at" db:"updated_at"`
}

// AffiliateProgramSummary is a safe standard-read projection for affiliate programs.
// It intentionally excludes encrypted secret material and secret-management metadata.
type AffiliateProgramSummary struct {
	ID            uuid.UUID  `json:"id" db:"id"`
	Name          string     `json:"name" db:"name"`
	Website       string     `json:"website" db:"website"`
	APIEndpoint   *string    `json:"api_endpoint,omitempty" db:"api_endpoint"`
	APIAuthMethod string     `json:"api_auth_method" db:"api_auth_method"`
	DeletedAt     *time.Time `json:"deleted_at,omitempty" db:"deleted_at"`
	CreatedAt     time.Time  `json:"created_at" db:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at" db:"updated_at"`
}

// MerchantModel is the structure which holds the DB instance.
type MerchantModel struct {
	DB     *pgxpool.Pool
	Logger *logging.Logger
}

// MerchantTypeModel is the structure which holds the DB instance.
type MerchantTypeModel struct {
	DB     *pgxpool.Pool
	Logger *logging.Logger
}

// MerchantAffiliateProgramModel is the structure which holds the DB instance.
type MerchantAffiliateProgramModel struct {
	DB     *pgxpool.Pool
	Logger *logging.Logger
}

// PlatformModel is the structure which holds the DB instance.
type PlatformModel struct {
	DB     *pgxpool.Pool
	Logger *logging.Logger
}


// MerchantModel CRUD Functions

const merchantSelectColumns = `
	id,
	merchant_type_id,
	name,
	display_name,
	slug,
	logo_url,
	website,
	platform_id,
	deleted_at,
	created_at,
	updated_at
`

func scanMerchant(row pgx.Row, merchant *Merchant) error {
	return row.Scan(
		&merchant.ID,
		&merchant.MerchantTypeID,
		&merchant.Name,
		&merchant.DisplayName,
		&merchant.Slug,
		&merchant.LogoURL,
		&merchant.Website,
		&merchant.PlatformID,
		&merchant.DeletedAt,
		&merchant.CreatedAt,
		&merchant.UpdatedAt,
	)
}

func scanMerchantFromRows(rows pgx.Rows, merchant *Merchant) error {
	return rows.Scan(
		&merchant.ID,
		&merchant.MerchantTypeID,
		&merchant.Name,
		&merchant.DisplayName,
		&merchant.Slug,
		&merchant.LogoURL,
		&merchant.Website,
		&merchant.PlatformID,
		&merchant.DeletedAt,
		&merchant.CreatedAt,
		&merchant.UpdatedAt,
	)
}

// Insert inserts a new merchant into the database.
func (m *MerchantModel) Insert(ctx context.Context, merchant *Merchant) error {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("InsertMerchant")

	if merchant == nil {
		err := errors.New("merchant is required")
		logger.Error("Validation failed", err)
		return err
	}

	merchant.Name = strings.TrimSpace(merchant.Name)
	merchant.DisplayName = strings.TrimSpace(merchant.DisplayName)
	merchant.Slug = strings.TrimSpace(merchant.Slug)

	if merchant.Name == "" {
		err := errors.New("merchant name is required")
		logger.Error("Validation failed", err)
		return err
	}
	if merchant.MerchantTypeID == uuid.Nil {
		err := errors.New("merchant_type_id is required")
		logger.Error("Validation failed", err)
		return err
	}
	if merchant.DisplayName == "" {
		err := errors.New("display_name is required")
		logger.Error("Validation failed", err)
		return err
	}
	if merchant.Slug == "" {
		err := errors.New("slug is required")
		logger.Error("Validation failed", err)
		return err
	}

	if merchant.ID == uuid.Nil {
		merchant.ID = uuid.New()
	}

	query := `
		INSERT INTO merchants
		(id, merchant_type_id, name, display_name, slug, logo_url, website, platform_id)
		VALUES
		($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING created_at, updated_at, deleted_at
	`

	err := m.DB.QueryRow(ctx, query,
		merchant.ID,
		merchant.MerchantTypeID,
		merchant.Name,
		merchant.DisplayName,
		merchant.Slug,
		merchant.LogoURL,
		merchant.Website,
		merchant.PlatformID,
	).Scan(
		&merchant.CreatedAt,
		&merchant.UpdatedAt,
		&merchant.DeletedAt,
	)
	if err != nil {
		logger.Error("Insert merchant failed", err)
		return err
	}

	logger.Info("Insert merchant successful", "merchant_id", merchant.ID)
	return nil
}


// GetByID retrieves an merchant by ID from the database.
func (m *MerchantModel) GetByID(ctx context.Context, id uuid.UUID) (*Merchant, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("GetMerchantByID")

	if id == uuid.Nil {
		err := errors.New("invalid merchant ID")
		logger.Error("Validation failed", err)
		return nil, err
	}

	query := `
		SELECT ` + merchantSelectColumns + `
		FROM merchants
		WHERE id = $1 AND deleted_at IS NULL
	`
	var merchant Merchant
	err := scanMerchant(m.DB.QueryRow(ctx, query, id), &merchant)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			logger.Warn("Merchant not found", "merchant_id", id)
			return nil, nil
		}
		logger.Error("Query failed", err)
		return nil, err
	}

	logger.Info("Retrieved merchant", "merchant_id", id)
	return &merchant, nil
}


// GetByName retrieves an merchant by name from the database.
func (m *MerchantModel) GetByName(ctx context.Context, name string) (*Merchant, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("GetByName")

	name = strings.TrimSpace(name)
	if name == "" {
		err := errors.New("merchant name is required")
		logger.Error("Validation failed", err)
		return nil, err
	}

	query := `
		SELECT ` + merchantSelectColumns + `
		FROM merchants
		WHERE name = $1 AND deleted_at IS NULL
		LIMIT 1
	`

	var merchant Merchant
	err := scanMerchant(m.DB.QueryRow(ctx, query, name), &merchant)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			logger.Warn("Merchant not found", "name", name)
			return nil, nil
		}
		logger.Error("Query failed", err)
		return nil, err
	}

	logger.Info("Merchant retrieved successfully", "merchant_id", merchant.ID)
	return &merchant, nil
}

// GetBySlug retrieves an active merchant by slug.
func (m *MerchantModel) GetBySlug(ctx context.Context, slug string) (*Merchant, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("GetBySlug")

	slug = strings.ToLower(strings.TrimSpace(slug))
	if slug == "" {
		err := errors.New("merchant slug is required")
		logger.Error("Validation failed", err)
		return nil, err
	}

	query := `
		SELECT ` + merchantSelectColumns + `
		FROM merchants
		WHERE slug = $1 AND deleted_at IS NULL
		LIMIT 1
	`

	var merchant Merchant
	err := scanMerchant(m.DB.QueryRow(ctx, query, slug), &merchant)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			logger.Warn("Merchant not found", "slug", slug)
			return nil, nil
		}
		logger.Error("Query failed", err)
		return nil, err
	}

	logger.Info("Merchant retrieved successfully", "merchant_id", merchant.ID, "slug", slug)
	return &merchant, nil
}

// GetByOfferID retrieves the merchant associated with a specific offer ID.
func (m *MerchantModel) GetByOfferID(ctx context.Context, offerID uuid.UUID) (*Merchant, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("GetByOfferID")

	if offerID == uuid.Nil {
		err := errors.New("invalid offer ID")
		logger.Error("Validation failed", err)
		return nil, err
	}

	query := `
		SELECT ` + merchantSelectColumns + `
		FROM merchants
		INNER JOIN offers ON offers.merchant_id = merchants.id
		WHERE offers.id = $1
		  AND merchants.deleted_at IS NULL
		  AND offers.deleted_at IS NULL
	`

	var merchant Merchant
	err := scanMerchant(m.DB.QueryRow(ctx, query, offerID), &merchant)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			logger.Warn("Merchant not found for offer", "offer_id", offerID)
			return nil, nil
		}
		logger.Error("Query failed", err)
		return nil, err
	}

	logger.Info("Retrieved merchant by offer ID", "offer_id", offerID, "merchant_id", merchant.ID)
	return &merchant, nil
}


// GetByProductID retrieves all merchants associated with a given product ID.
func (m *MerchantModel) GetByProductID(ctx context.Context, productID uuid.UUID) ([]*Merchant, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("GetByProductID")

	if productID == uuid.Nil {
		err := errors.New("invalid product ID")
		logger.Error("Validation failed", err)
		return nil, err
	}

	query := `
		SELECT DISTINCT ` + merchantSelectColumns + `
		FROM merchants
		INNER JOIN merchant_products ON merchants.id = merchant_products.merchant_id
		WHERE merchant_products.product_id = $1
		  AND merchants.deleted_at IS NULL
	`

	rows, err := m.DB.Query(ctx, query, productID)
	if err != nil {
		logger.Error("Query failed", err)
		return nil, err
	}
	defer rows.Close()

	var merchants []*Merchant

	for rows.Next() {
		var merchant Merchant
		if err := scanMerchantFromRows(rows, &merchant); err != nil {
			logger.Error("Row scan failed", err)
			return nil, err
		}
		merchants = append(merchants, &merchant)
	}

	if rows.Err() != nil {
		logger.Error("Row iteration failed", rows.Err())
		return nil, rows.Err()
	}

	logger.Info("Retrieved merchants by product", "product_id", productID, "count", len(merchants))
	return merchants, nil
}


// GetByBrandID returns all merchants associated with products under the given brand ID.
func (m *MerchantModel) GetByBrandID(ctx context.Context, brandID uuid.UUID) ([]*Merchant, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("GetByBrandID")

	if brandID == uuid.Nil {
		err := errors.New("invalid brand ID")
		logger.Error("Validation failed", err)
		return nil, err
	}

	query := `
		SELECT DISTINCT ` + merchantSelectColumns + `
		FROM merchants
		JOIN merchant_products ON merchant_products.merchant_id = merchants.id
		JOIN products ON products.id = merchant_products.product_id
		WHERE products.brand_id = $1
		  AND merchants.deleted_at IS NULL
		  AND products.deleted_at IS NULL
	`

	rows, err := m.DB.Query(ctx, query, brandID)
	if err != nil {
		logger.Error("Query failed", err)
		return nil, err
	}
	defer rows.Close()

	var merchants []*Merchant

	for rows.Next() {
		var merchant Merchant
		err := scanMerchantFromRows(rows, &merchant)
		if err != nil {
			logger.Error("Row scan failed", err)
			return nil, err
		}
		merchants = append(merchants, &merchant)
	}

	if err := rows.Err(); err != nil {
		logger.Error("Rows iteration failed", err)
		return nil, err
	}

	logger.Info("Retrieved merchants by brand ID", "brand_id", brandID, "count", len(merchants))
	return merchants, nil
}


// GetByProductLine returns all merchants associated with products in the given product line.
func (m *MerchantModel) GetByProductLine(ctx context.Context, productLine string) ([]*Merchant, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("GetByProductLine")

	if strings.TrimSpace(productLine) == "" {
		err := errors.New("product line cannot be empty")
		logger.Error("Validation failed", err)
		return nil, err
	}

	query := `
		SELECT DISTINCT ` + merchantSelectColumns + `
		FROM merchants
		JOIN merchant_products ON merchant_products.merchant_id = merchants.id
		JOIN products ON products.id = merchant_products.product_id
		WHERE products.product_line = $1
		  AND merchants.deleted_at IS NULL
		  AND products.deleted_at IS NULL
	`

	rows, err := m.DB.Query(ctx, query, productLine)
	if err != nil {
		logger.Error("Query failed", err)
		return nil, err
	}
	defer rows.Close()

	var merchants []*Merchant

	for rows.Next() {
		var merchant Merchant
		err := scanMerchantFromRows(rows, &merchant)
		if err != nil {
			logger.Error("Row scan failed", err)
			return nil, err
		}
		merchants = append(merchants, &merchant)
	}

	if rows.Err() != nil {
		logger.Error("Rows iteration error", rows.Err())
		return nil, rows.Err()
	}

	logger.Info("Retrieved merchants by product line", "product_line", productLine, "count", len(merchants))
	return merchants, nil
}


// GetByWebsite retrieves an merchant by their website URL.
func (m *MerchantModel) GetByWebsite(ctx context.Context, website string) (*Merchant, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("GetByWebsite")

	website = strings.TrimSpace(website)
	if website == "" {
		err := errors.New("website is required")
		logger.Error("Validation failed", err)
		return nil, err
	}

	query := `
		SELECT ` + merchantSelectColumns + `
		FROM merchants
		WHERE website = $1 AND deleted_at IS NULL
		LIMIT 1
	`

	var merchant Merchant
	err := scanMerchant(m.DB.QueryRow(ctx, query, website), &merchant)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			logger.Info("Merchant not found", "website", website)
			return nil, nil
		}
		logger.Error("Get merchant by website failed", err)
		return nil, err
	}

	logger.Info("Get merchant by website successful", "merchant_id", merchant.ID)
	return &merchant, nil
}


// GetByPlatform retrieves all merchants associated with a specific platform ID.
func (m *MerchantModel) GetByPlatform(ctx context.Context, platformID uuid.UUID) ([]*Merchant, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("GetByPlatform")

	if platformID == uuid.Nil {
		err := errors.New("platform_id is required")
		logger.Error("Validation failed", err)
		return nil, err
	}

	query := `
		SELECT ` + merchantSelectColumns + `
		FROM merchants
		WHERE platform_id = $1 AND deleted_at IS NULL
	`

	rows, err := m.DB.Query(ctx, query, platformID)
	if err != nil {
		logger.Error("Query failed", err)
		return nil, err
	}
	defer rows.Close()

	var merchants []*Merchant

	for rows.Next() {
		var merchant Merchant
		err := scanMerchantFromRows(rows, &merchant)
		if err != nil {
			logger.Error("Row scan failed", err)
			return nil, err
		}
		merchants = append(merchants, &merchant)
	}

	if rows.Err() != nil {
		logger.Error("Rows iteration error", rows.Err())
		return nil, rows.Err()
	}

	logger.Info("Retrieved merchants by platform", "platform_id", platformID, "count", len(merchants))
	return merchants, nil
}


// GetByMerchantType retrieves all merchants matching a specific merchant_type_id.
func (m *MerchantModel) GetByMerchantType(ctx context.Context, merchantTypeID uuid.UUID) ([]*Merchant, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("GetByMerchantType")

	if merchantTypeID == uuid.Nil {
		err := errors.New("merchant_type_id is required")
		logger.Error("Validation failed", err)
		return nil, err
	}

	query := `
		SELECT ` + merchantSelectColumns + `
		FROM merchants
		WHERE merchant_type_id = $1 AND deleted_at IS NULL
	`

	rows, err := m.DB.Query(ctx, query, merchantTypeID)
	if err != nil {
		logger.Error("Query failed", err)
		return nil, err
	}
	defer rows.Close()

	var merchants []*Merchant

	for rows.Next() {
		var merchant Merchant
		err := scanMerchantFromRows(rows, &merchant)
		if err != nil {
			logger.Error("Row scan failed", err)
			return nil, err
		}
		merchants = append(merchants, &merchant)
	}

	if rows.Err() != nil {
		logger.Error("Row iteration failed", rows.Err())
		return nil, rows.Err()
	}

	logger.Info("GetByMerchantType successful", "merchant_type_id", merchantTypeID, "count", len(merchants))
	return merchants, nil
}


// Count returns the total number of non-deleted merchants in the database.
// If merchantTypeID is not uuid.Nil, it filters by that type.
func (m *MerchantModel) Count(ctx context.Context, merchantTypeID uuid.UUID) (int, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("CountMerchants")

	var (
		query string
		args  []any
	)

	if merchantTypeID != uuid.Nil {
		query = `SELECT COUNT(*) FROM merchants WHERE deleted_at IS NULL AND merchant_type_id = $1`
		args = append(args, merchantTypeID)
	} else {
		query = `SELECT COUNT(*) FROM merchants WHERE deleted_at IS NULL`
	}

	var count int
	err := m.DB.QueryRow(ctx, query, args...).Scan(&count)
	if err != nil {
		logger.Error("Count merchants failed", err)
		return 0, err
	}

	logger.Info("Count merchants successful", "count", count)
	return count, nil
}


// Exists checks whether an merchant with the given ID exists and is not deleted.
func (m *MerchantModel) Exists(ctx context.Context, merchantID uuid.UUID) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("ExistsMerchant")

	if merchantID == uuid.Nil {
		err := errors.New("merchant_id is required")
		logger.Error("Validation failed", err)
		return false, err
	}

	query := `
		SELECT EXISTS (
			SELECT 1 FROM merchants
			WHERE id = $1 AND deleted_at IS NULL
		)
	`

	var exists bool
	err := m.DB.QueryRow(ctx, query, merchantID).Scan(&exists)
	if err != nil {
		logger.Error("Exists query failed", err)
		return false, err
	}

	logger.Info("Exists check successful", "merchant_id", merchantID, "exists", exists)
	return exists, nil
}


// GetAll retrieves a paginated list of active merchants from the database,
// optionally filtered by merchant_type_id.
func (m *MerchantModel) GetAll(ctx context.Context, merchantTypeID *uuid.UUID, limit, offset int) ([]*Merchant, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("GetAllMerchants")

	if limit <= 0 || limit > 100 {
		err := fmt.Errorf("invalid limit: must be between 1 and 100")
		logger.Error("Validation failed", err)
		return nil, err
	}
	if offset < 0 {
		err := fmt.Errorf("invalid offset: must be non-negative")
		logger.Error("Validation failed", err)
		return nil, err
	}

	query := `SELECT ` + merchantSelectColumns + ` FROM merchants WHERE deleted_at IS NULL`
	args := []any{}
	argID := 1

	if merchantTypeID != nil {
		query += fmt.Sprintf(" AND merchant_type_id = $%d", argID)
		args = append(args, *merchantTypeID)
		argID++
	}

	query += fmt.Sprintf(" ORDER BY created_at DESC LIMIT $%d OFFSET $%d", argID, argID+1)
	args = append(args, limit, offset)

	rows, err := m.DB.Query(ctx, query, args...)
	if err != nil {
		logger.Error("Query failed", err)
		return nil, err
	}
	defer rows.Close()

	var merchants []*Merchant
	for rows.Next() {
		var merchant Merchant
		err := scanMerchantFromRows(rows, &merchant)
		if err != nil {
			logger.Error("Row scan failed", err)
			return nil, err
		}
		merchants = append(merchants, &merchant)
	}

	if rows.Err() != nil {
		logger.Error("Row iteration failed", rows.Err())
		return nil, rows.Err()
	}

	logger.Info("GetAll merchants successful", "count", len(merchants))
	return merchants, nil
}


// Update partially updates via PATCH an existing merchant in the database.
func (m *MerchantModel) Update(ctx context.Context, merchant *Merchant) error {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("UpdateMerchant")

	if merchant == nil {
		err := errors.New("merchant is required")
		logger.Error("Validation failed", err)
		return err
	}

	if merchant.ID == uuid.Nil {
		err := errors.New("merchant ID is required")
		logger.Error("Validation failed", err)
		return err
	}
	if merchant.MerchantTypeID == uuid.Nil {
		err := errors.New("merchant_type_id is required")
		logger.Error("Validation failed", err)
		return err
	}

	merchant.Name = strings.TrimSpace(merchant.Name)
	merchant.DisplayName = strings.TrimSpace(merchant.DisplayName)
	merchant.Slug = strings.TrimSpace(merchant.Slug)

	if merchant.Name == "" {
		err := errors.New("merchant name is required")
		logger.Error("Validation failed", err)
		return err
	}
	if merchant.DisplayName == "" {
		err := errors.New("display_name is required")
		logger.Error("Validation failed", err)
		return err
	}
	if merchant.Slug == "" {
		err := errors.New("slug is required")
		logger.Error("Validation failed", err)
		return err
	}

	query := `
		UPDATE merchants
		SET merchant_type_id = $1,
			name = $2,
			display_name = $3,
			slug = $4,
			logo_url = $5,
			website = $6,
			platform_id = $7
		WHERE id = $8
		  AND deleted_at IS NULL
		RETURNING created_at, updated_at, deleted_at
	`

	err := m.DB.QueryRow(ctx, query,
		merchant.MerchantTypeID,
		merchant.Name,
		merchant.DisplayName,
		merchant.Slug,
		merchant.LogoURL,
		merchant.Website,
		merchant.PlatformID,
		merchant.ID,
	).Scan(
		&merchant.CreatedAt,
		&merchant.UpdatedAt,
		&merchant.DeletedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			err = fmt.Errorf("no active merchant found with ID %s", merchant.ID)
		}
		logger.Error("Update merchant failed", err)
		return err
	}

	logger.Info("Update merchant successful", "merchant_id", merchant.ID)
	return nil
}


// SoftDelete marks an merchant as deleted by setting deleted_at = NOW().
func (m *MerchantModel) SoftDelete(ctx context.Context, merchantID uuid.UUID) error {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("SoftDeleteMerchant")

	if merchantID == uuid.Nil {
		err := errors.New("merchant_id is required")
		logger.Error("Validation failed", err)
		return err
	}

	var deletedAt time.Time
	err := m.DB.QueryRow(ctx, `
		UPDATE merchants
		SET deleted_at = NOW()
		WHERE id = $1
		AND deleted_at IS NULL
		RETURNING deleted_at
	`, merchantID).Scan(&deletedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			logger.Warn("Merchant not found for soft delete", "merchant_id", merchantID)
			return ErrMerchantNotFound
		}
		logger.Error("Soft delete merchant failed", err, "merchant_id", merchantID)
		return err
	}

	logger.Info("Soft delete merchant successful",
		"merchant_id", merchantID,
		"deleted_at", deletedAt,
	)
	return nil
}


// Restore restores a soft deleted merchant by setting deleted_at = NULL.
func (m *MerchantModel) Restore(ctx context.Context, merchantID uuid.UUID) error {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("RestoreMerchant")

	if merchantID == uuid.Nil {
		err := errors.New("merchant ID is required")
		logger.Error("Validation failed", err)
		return err
	}

	query := `
		UPDATE merchants
		SET deleted_at = NULL
		WHERE id = $1 AND deleted_at IS NOT NULL
	`

	cmdTag, err := m.DB.Exec(ctx, query, merchantID)
	if err != nil {
		logger.Error("Restore merchant failed", err)
		return err
	}

	if cmdTag.RowsAffected() == 0 {
		err := fmt.Errorf("no deleted merchant found with ID %s", merchantID)
		logger.Error("Restore merchant failed", err)
		return err
	}

	logger.Info("Restore merchant successful", "merchant_id", merchantID)
	return nil
}


// Delete permanently removes a merchant from the database.
//
// Policy note:
// SoftDelete is the default business-path delete for merchants. This hard delete
// method is retained only for tightly controlled internal/admin use cases such as
// exceptional maintenance, irreversible cleanup, or test-data teardown where
// physical deletion is explicitly intended.
func (m *MerchantModel) Delete(ctx context.Context, merchantID uuid.UUID) error {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("DeleteMerchant")

	if merchantID == uuid.Nil {
		err := errors.New("merchant ID is required")
		logger.Error("Validation failed", err)
		return err
	}

	var deletedID uuid.UUID
	err := m.DB.QueryRow(ctx, `
		DELETE FROM merchants
		WHERE id = $1
		RETURNING id
	`, merchantID).Scan(&deletedID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			logger.Warn("Merchant not found for hard delete", "merchant_id", merchantID)
			return ErrMerchantNotFound
		}
		logger.Error("Permanent delete merchant failed", err)
		return err
	}

	logger.Info("Permanent delete merchant successful", "merchant_id", deletedID)
	return nil
}


// MerchantTypeModel CRUD Functions

// Insert inserts a new merchant type into the database.
func (m *MerchantTypeModel) Insert(ctx context.Context, merchantType *MerchantType) error {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("InsertMerchantType")

	if merchantType == nil {
		err := errors.New("merchant type is required")
		logger.Error("Validation failed", err)
		return err
	}

	if merchantType.Name == "" {
		err := errors.New("merchant type name is required")
		logger.Error("Validation failed", err)
		return err
	}

	if merchantType.Description == "" {
		err := errors.New("merchant type description is required")
		logger.Error("Validation failed", err)
		return err
	}

	// Generate UUID
	merchantType.ID = uuid.New()

	query := `
		INSERT INTO merchant_types
		(id, name, description)
		VALUES ($1, $2, $3)
		RETURNING created_at, updated_at, deleted_at
	`

	err := m.DB.QueryRow(ctx, query, merchantType.ID, merchantType.Name, merchantType.Description).
		Scan(&merchantType.CreatedAt, &merchantType.UpdatedAt, &merchantType.DeletedAt)
	if err != nil {
		logger.Error("Insert merchant type failed", err)
		return err
	}

	logger.Info("Insert merchant type successful", "merchant_type_id", merchantType.ID)
	return nil
}


// GetByID retrieves an merchant type by its ID.
func (m *MerchantTypeModel) GetByID(ctx context.Context, id uuid.UUID) (*MerchantType, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("GetMerchantTypeByID")

	if id == uuid.Nil {
		err := errors.New("invalid merchant type ID")
		logger.Error("Validation failed", err)
		return nil, err
	}

	query := `
		SELECT id, name, description, deleted_at, created_at, updated_at
		FROM merchant_types
		WHERE id = $1 AND deleted_at IS NULL
	`

	var merchantType MerchantType

	err := m.DB.QueryRow(ctx, query, id).Scan(
		&merchantType.ID,
		&merchantType.Name,
		&merchantType.Description,
		&merchantType.DeletedAt,
		&merchantType.CreatedAt,
		&merchantType.UpdatedAt,
	)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			logger.Warn("Merchant type not found", "merchant_type_id", id)
			return nil, nil
		}
		logger.Error("Get merchant type failed", err)
		return nil, err
	}

	logger.Info("Get merchant type successful", "merchant_type_id", id)
	return &merchantType, nil
}


func (m *MerchantTypeModel) GetByName(ctx context.Context, name string) (*MerchantType, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("GetMerchantTypeByName")

	if name == "" {
		err := errors.New("merchant type name is required")
		logger.Error("Validation failed", err)
		return nil, err
	}

	query := `
		SELECT id, name, description, deleted_at, created_at, updated_at
		FROM merchant_types
		WHERE name = $1 AND deleted_at IS NULL
	`

	var at MerchantType

	err := m.DB.QueryRow(ctx, query, name).Scan(
		&at.ID,
		&at.Name,
		&at.Description,
		&at.DeletedAt,
		&at.CreatedAt,
		&at.UpdatedAt,
	)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			logger.Warn("No merchant type found", "name", name)
			return nil, nil
		}
		logger.Error("Query failed", err)
		return nil, err
	}

	logger.Info("Merchant type retrieved", "merchant_type_id", at.ID)
	return &at, nil
}


// GetAll retrieves merchant types from the database using explicit pagination
// and an optional affiliate-program filter.
func (m *MerchantTypeModel) GetAll(ctx context.Context, limit, offset int, affiliateProgramID *uuid.UUID) ([]*MerchantType, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("GetAllMerchantTypes")

	if limit <= 0 || limit > 100 {
		err := errors.New("limit must be between 1 and 100")
		logger.Error("Validation failed", err)
		return nil, err
	}
	if offset < 0 {
		err := errors.New("offset must be non-negative")
		logger.Error("Validation failed", err)
		return nil, err
	}

	var rows pgx.Rows
	var err error

	switch {
	case affiliateProgramID != nil:
		query := `
			SELECT DISTINCT at.id, at.name, at.description, at.deleted_at, at.created_at, at.updated_at
			FROM merchant_types at
			JOIN merchants a ON at.id = a.merchant_type_id
			JOIN merchant_affiliate_programs ap ON a.id = ap.merchant_id
			WHERE ap.affiliate_program_id = $1
			  AND at.deleted_at IS NULL
			  AND a.deleted_at IS NULL
			  AND ap.deleted_at IS NULL
			ORDER BY at.name ASC
			LIMIT $2 OFFSET $3
		`
		rows, err = m.DB.Query(ctx, query, *affiliateProgramID, limit, offset)

	default:
		query := `
			SELECT id, name, description, deleted_at, created_at, updated_at
			FROM merchant_types
			WHERE deleted_at IS NULL
			ORDER BY name ASC
			LIMIT $1 OFFSET $2
		`
		rows, err = m.DB.Query(ctx, query, limit, offset)
	}

	if err != nil {
		logger.Error("Query failed", err)
		return nil, err
	}
	defer rows.Close()

	var merchantTypes []*MerchantType

	for rows.Next() {
		var at MerchantType
		if err := rows.Scan(
			&at.ID,
			&at.Name,
			&at.Description,
			&at.DeletedAt,
			&at.CreatedAt,
			&at.UpdatedAt,
		); err != nil {
			logger.Error("Row scan failed", err)
			return nil, err
		}
		merchantTypes = append(merchantTypes, &at)
	}

	if err := rows.Err(); err != nil {
		logger.Error("Row iteration error", err)
		return nil, err
	}

	logger.Info("Retrieved merchant types", "count", len(merchantTypes))
	return merchantTypes, nil
}


// Update partially updates via PATCH an existing merchant type in the database.
func (m *MerchantTypeModel) Update(ctx context.Context, merchantType *MerchantType) error {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("UpdateMerchantType")

	if merchantType.ID == uuid.Nil {
		err := errors.New("merchant type ID is required")
		logger.Error("Validation failed", err)
		return err
	}
	if merchantType.Name == "" {
		err := errors.New("merchant type name is required")
		logger.Error("Validation failed", err)
		return err
	}
	if merchantType.Description == "" {
		err := errors.New("merchant type description is required")
		logger.Error("Validation failed", err)
		return err
	}

	query := `
		UPDATE merchant_types
		SET name = $1, description = $2
		WHERE id = $3 AND deleted_at IS NULL
		RETURNING created_at, updated_at, deleted_at
	`

	err := m.DB.QueryRow(ctx, query, merchantType.Name, merchantType.Description, merchantType.ID).
		Scan(&merchantType.CreatedAt, &merchantType.UpdatedAt, &merchantType.DeletedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			err = fmt.Errorf("no active merchant type found with id %s", merchantType.ID)
		}
		logger.Error("Update merchant type failed", err)
		return err
	}

	logger.Info("Update merchant type successful", "merchant_type_id", merchantType.ID)
	return nil
}


// SoftDelete performs a soft delete on a merchant type.
func (m *MerchantTypeModel) SoftDelete(ctx context.Context, id uuid.UUID) error {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("SoftDeleteMerchantType")

	if id == uuid.Nil {
		err := errors.New("invalid merchant type ID")
		logger.Error("Validation failed", err)
		return err
	}

	query := `
		UPDATE merchant_types
		SET deleted_at = NOW()
		WHERE id = $1 AND deleted_at IS NULL
	`

	cmdTag, err := m.DB.Exec(ctx, query, id)
	if err != nil {
		logger.Error("Soft delete merchant type failed", err)
		return err
	}

	if cmdTag.RowsAffected() == 0 {
		err := fmt.Errorf("no active merchant type found with id: %s", id)
		logger.Warn("No rows affected", "merchant_type_id", id)
		return err
	}

	logger.Info("Soft delete merchant type successful", "merchant_type_id", id)
	return nil
}


// MerchantAffiliateProgramModel CRUD Functions

// Insert inserts a new merchant-affiliate program association into the database.
func (m *MerchantAffiliateProgramModel) Insert(ctx context.Context, assoc *MerchantAffiliateProgram) error {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("InsertMerchantAffiliateProgram")

	if assoc == nil {
		err := errors.New("association is required")
		logger.Error("Validation failed", err)
		return err
	}

	if assoc.MerchantID == uuid.Nil {
		err := errors.New("merchant_id is required")
		logger.Error("Validation failed", err)
		return err
	}

	if assoc.AffiliateProgramID == uuid.Nil {
		err := errors.New("affiliate_program_id is required")
		logger.Error("Validation failed", err)
		return err
	}

	query := `
		INSERT INTO merchant_affiliate_programs (merchant_id, affiliate_program_id)
		VALUES ($1, $2)
		RETURNING deleted_at, created_at, updated_at
	`

	err := m.DB.QueryRow(ctx, query, assoc.MerchantID, assoc.AffiliateProgramID).Scan(
		&assoc.DeletedAt,
		&assoc.CreatedAt,
		&assoc.UpdatedAt,
	)
	if err != nil {
		logger.Error("Insert merchant-affiliate program association failed", err)
		return err
	}

	logger.Info("Insert merchant-affiliate program association successful",
		"merchant_id", assoc.MerchantID, "affiliate_program_id", assoc.AffiliateProgramID)
	return nil
}

func (m *MerchantAffiliateProgramModel) GetByIDs(ctx context.Context, merchantID, affiliateProgramID uuid.UUID) (*MerchantAffiliateProgram, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("GetByIDs")

	if merchantID == uuid.Nil {
		err := errors.New("merchant_id is required")
		logger.Error("Validation failed", err)
		return nil, err
	}
	if affiliateProgramID == uuid.Nil {
		err := errors.New("affiliate_program_id is required")
		logger.Error("Validation failed", err)
		return nil, err
	}

	query := `
		SELECT merchant_id, affiliate_program_id, deleted_at, created_at, updated_at
		FROM merchant_affiliate_programs
		WHERE merchant_id = $1
		  AND affiliate_program_id = $2
		  AND deleted_at IS NULL
	`

	var assoc MerchantAffiliateProgram
	err := m.DB.QueryRow(ctx, query, merchantID, affiliateProgramID).Scan(
		&assoc.MerchantID,
		&assoc.AffiliateProgramID,
		&assoc.DeletedAt,
		&assoc.CreatedAt,
		&assoc.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			logger.Info("No merchant-affiliate program association found",
				"merchant_id", merchantID, "affiliate_program_id", affiliateProgramID)
			return nil, nil
		}
		logger.Error("Query failed", err)
		return nil, err
	}

	logger.Info("Retrieved merchant-affiliate program association",
		"merchant_id", assoc.MerchantID, "affiliate_program_id", assoc.AffiliateProgramID)
	return &assoc, nil
}

func (m *MerchantAffiliateProgramModel) GetByMerchantID(ctx context.Context, merchantID uuid.UUID) ([]MerchantAffiliateProgram, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("GetByMerchantID")

	if merchantID == uuid.Nil {
		err := errors.New("merchant_id is required")
		logger.Error("Validation failed", err)
		return nil, err
	}

	query := `
		SELECT merchant_id, affiliate_program_id, deleted_at, created_at, updated_at
		FROM merchant_affiliate_programs
		WHERE merchant_id = $1 AND deleted_at IS NULL
	`

	rows, err := m.DB.Query(ctx, query, merchantID)
	if err != nil {
		logger.Error("Query failed", err)
		return nil, err
	}
	defer rows.Close()

	var results []MerchantAffiliateProgram
	for rows.Next() {
		var assoc MerchantAffiliateProgram
		if err := rows.Scan(
			&assoc.MerchantID,
			&assoc.AffiliateProgramID,
			&assoc.DeletedAt,
			&assoc.CreatedAt,
			&assoc.UpdatedAt,
		); err != nil {
			logger.Error("Row scan failed", err)
			return nil, err
		}
		results = append(results, assoc)
	}

	if err := rows.Err(); err != nil {
		logger.Error("Rows iteration error", err)
		return nil, err
	}

	logger.Info("Retrieved merchant-affiliate program associations", "merchant_id", merchantID, "count", len(results))
	return results, nil
}

func (m *MerchantAffiliateProgramModel) GetFullAffiliateProgramsByMerchantID(ctx context.Context, merchantID uuid.UUID, limit, offset int) ([]AffiliateProgramSummary, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("GetFullAffiliateProgramsByMerchantID")

	if merchantID == uuid.Nil {
		err := errors.New("merchant_id is required")
		logger.Error("Validation failed", err)
		return nil, err
	}
	if limit <= 0 || limit > 100 {
		err := errors.New("limit must be between 1 and 100")
		logger.Error("Validation failed", err)
		return nil, err
	}
	if offset < 0 {
		err := errors.New("offset must be non-negative")
		logger.Error("Validation failed", err)
		return nil, err
	}

	query := `
		SELECT
			ap.id,
			ap.name,
			ap.website,
			ap.api_endpoint,
			ap.api_auth_method,
			ap.deleted_at,
			ap.created_at,
			ap.updated_at
		FROM merchant_affiliate_programs map
		JOIN affiliate_programs ap
		  ON ap.id = map.affiliate_program_id
		JOIN merchants m
		  ON m.id = map.merchant_id
		WHERE map.merchant_id = $1
		  AND map.deleted_at IS NULL
		  AND ap.deleted_at IS NULL
		  AND m.deleted_at IS NULL
		ORDER BY ap.name ASC
		LIMIT $2 OFFSET $3
	`

	rows, err := m.DB.Query(ctx, query, merchantID, limit, offset)
	if err != nil {
		logger.Error("Query failed", err)
		return nil, err
	}
	defer rows.Close()

	var programs []AffiliateProgramSummary
	for rows.Next() {
		var program AffiliateProgramSummary
		if err := rows.Scan(
			&program.ID,
			&program.Name,
			&program.Website,
			&program.APIEndpoint,
			&program.APIAuthMethod,
			&program.DeletedAt,
			&program.CreatedAt,
			&program.UpdatedAt,
		); err != nil {
			logger.Error("Row scan failed", err)
			return nil, err
		}
		programs = append(programs, program)
	}
	if err := rows.Err(); err != nil {
		logger.Error("Rows iteration error", err)
		return nil, err
	}

	logger.Info("Retrieved affiliate programs for merchant", "merchant_id", merchantID, "count", len(programs))
	return programs, nil
}

func (m *MerchantAffiliateProgramModel) GetByAffiliateProgramID(ctx context.Context, affiliateProgramID uuid.UUID) ([]*MerchantAffiliateProgram, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("GetByAffiliateProgramID")

	if affiliateProgramID == uuid.Nil {
		err := errors.New("affiliate_program_id is required")
		logger.Error("Validation failed", err)
		return nil, err
	}

	query := `
		SELECT merchant_id, affiliate_program_id, deleted_at, created_at, updated_at
		FROM merchant_affiliate_programs
		WHERE affiliate_program_id = $1 AND deleted_at IS NULL
	`

	rows, err := m.DB.Query(ctx, query, affiliateProgramID)
	if err != nil {
		logger.Error("Query failed", err)
		return nil, err
	}
	defer rows.Close()

	var associations []*MerchantAffiliateProgram
	for rows.Next() {
		var assoc MerchantAffiliateProgram
		err := rows.Scan(
			&assoc.MerchantID,
			&assoc.AffiliateProgramID,
			&assoc.DeletedAt,
			&assoc.CreatedAt,
			&assoc.UpdatedAt,
		)
		if err != nil {
			logger.Error("Row scan failed", err)
			return nil, err
		}
		associations = append(associations, &assoc)
	}

	if err = rows.Err(); err != nil {
		logger.Error("Rows iteration error", err)
		return nil, err
	}

	logger.Info("Retrieved merchant-affiliate program associations",
		"affiliate_program_id", affiliateProgramID, "count", len(associations))
	return associations, nil
}

func (m *MerchantAffiliateProgramModel) GetAll(ctx context.Context) ([]*MerchantAffiliateProgram, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("GetAllMerchantAffiliatePrograms")

	query := `
		SELECT merchant_id, affiliate_program_id, deleted_at, created_at, updated_at
		FROM merchant_affiliate_programs
		WHERE deleted_at IS NULL
	`

	rows, err := m.DB.Query(ctx, query)
	if err != nil {
		logger.Error("Query failed", err)
		return nil, err
	}
	defer rows.Close()

	var associations []*MerchantAffiliateProgram
	for rows.Next() {
		var assoc MerchantAffiliateProgram
		err := rows.Scan(
			&assoc.MerchantID,
			&assoc.AffiliateProgramID,
			&assoc.DeletedAt,
			&assoc.CreatedAt,
			&assoc.UpdatedAt,
		)
		if err != nil {
			logger.Error("Row scan failed", err)
			return nil, err
		}
		associations = append(associations, &assoc)
	}

	if err := rows.Err(); err != nil {
		logger.Error("Rows iteration error", err)
		return nil, err
	}

	logger.Info("Retrieved merchant-affiliate program associations", "count", len(associations))
	return associations, nil
}

func (m *MerchantAffiliateProgramModel) Exists(ctx context.Context, merchantID, affiliateProgramID uuid.UUID) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("ExistsMerchantAffiliateProgram")

	if merchantID == uuid.Nil {
		err := errors.New("merchant_id is required")
		logger.Error("Validation failed", err)
		return false, err
	}
	if affiliateProgramID == uuid.Nil {
		err := errors.New("affiliate_program_id is required")
		logger.Error("Validation failed", err)
		return false, err
	}

	query := `
		SELECT EXISTS (
			SELECT 1
			FROM merchant_affiliate_programs
			WHERE merchant_id = $1
			  AND affiliate_program_id = $2
			  AND deleted_at IS NULL
		)
	`

	var exists bool
	err := m.DB.QueryRow(ctx, query, merchantID, affiliateProgramID).Scan(&exists)
	if err != nil {
		logger.Error("Exists check failed", err)
		return false, err
	}

	logger.Info("Exists check successful", "merchant_id", merchantID, "affiliate_program_id", affiliateProgramID, "exists", exists)
	return exists, nil
}

func (m *MerchantAffiliateProgramModel) SoftDelete(ctx context.Context, merchantID, affiliateProgramID uuid.UUID) error {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("SoftDeleteMerchantAffiliateProgram")

	if merchantID == uuid.Nil {
		err := errors.New("merchant_id is required")
		logger.Error("Validation failed", err)
		return err
	}
	if affiliateProgramID == uuid.Nil {
		err := errors.New("affiliate_program_id is required")
		logger.Error("Validation failed", err)
		return err
	}

	query := `
		UPDATE merchant_affiliate_programs
		SET deleted_at = NOW()
		WHERE merchant_id = $1
		  AND affiliate_program_id = $2
		  AND deleted_at IS NULL
	`

	cmdTag, err := m.DB.Exec(ctx, query, merchantID, affiliateProgramID)
	if err != nil {
		logger.Error("Soft delete failed", err)
		return err
	}

	if cmdTag.RowsAffected() == 0 {
		err := fmt.Errorf("no active record found for merchant_id=%s and affiliate_program_id=%s", merchantID, affiliateProgramID)
		logger.Warn("Soft delete skipped", "merchant_id", merchantID, "affiliate_program_id", affiliateProgramID)
		return err
	}

	logger.Info("Soft delete successful", "merchant_id", merchantID, "affiliate_program_id", affiliateProgramID)
	return nil
}

func (m *MerchantAffiliateProgramModel) SoftDeleteByMerchantID(ctx context.Context, merchantID uuid.UUID) error {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("SoftDeleteByMerchantID")

	if merchantID == uuid.Nil {
		err := errors.New("merchant_id is required")
		logger.Error("Validation failed", err)
		return err
	}

	query := `
		UPDATE merchant_affiliate_programs
		SET deleted_at = NOW()
		WHERE merchant_id = $1 AND deleted_at IS NULL
	`

	_, err := m.DB.Exec(ctx, query, merchantID)
	if err != nil {
		logger.Error("Soft delete by merchant_id failed", err)
		return err
	}

	logger.Info("Soft deleted merchant-affiliate program associations for merchant", "merchant_id", merchantID)
	return nil
}

func (m *MerchantAffiliateProgramModel) SoftDeleteByAffiliateProgramID(ctx context.Context, affiliateProgramID uuid.UUID) error {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("SoftDeleteByAffiliateProgramID")

	if affiliateProgramID == uuid.Nil {
		err := errors.New("affiliate_program_id is required")
		logger.Error("Validation failed", err)
		return err
	}

	query := `
		UPDATE merchant_affiliate_programs
		SET deleted_at = NOW()
		WHERE affiliate_program_id = $1 AND deleted_at IS NULL
	`

	cmdTag, err := m.DB.Exec(ctx, query, affiliateProgramID)
	if err != nil {
		logger.Error("Soft delete merchant-affiliate program associations failed", err)
		return err
	}

	logger.Info("Soft delete merchant-affiliate program associations successful",
		"affiliate_program_id", affiliateProgramID, "rows_affected", cmdTag.RowsAffected())
	return nil
}


// PlatformModel CRUD Functions
func (m *PlatformModel) Insert(ctx context.Context, platform *Platform) error {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("InsertPlatform")

	if platform == nil {
		err := errors.New("platform is required")
		logger.Error("Validation failed", err)
		return err
	}
	if platform.Name == "" {
		err := errors.New("name is required")
		logger.Error("Validation failed", err)
		return err
	}
	if platform.ID == uuid.Nil {
		platform.ID = uuid.New()
	}

	query := `
		INSERT INTO platforms (id, name, description, website)
		VALUES ($1, $2, $3, $4)
		RETURNING created_at, updated_at, deleted_at
	`

	err := m.DB.QueryRow(ctx, query, platform.ID, platform.Name, platform.Description, platform.Website).
		Scan(&platform.CreatedAt, &platform.UpdatedAt, &platform.DeletedAt)
	if err != nil {
		logger.Error("Insert platform failed", err)
		return err
	}

	logger.Info("Insert platform successful", "platform_id", platform.ID)
	return nil
}

func (m *PlatformModel) GetByID(ctx context.Context, id uuid.UUID) (*Platform, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("GetByID")

	if id == uuid.Nil {
		err := errors.New("invalid platform ID")
		logger.Error("Validation failed", err)
		return nil, err
	}

	query := `
		SELECT id, name, description, website, deleted_at, created_at, updated_at
		FROM platforms
		WHERE id = $1 AND deleted_at IS NULL
	`

	var platform Platform
	err := m.DB.QueryRow(ctx, query, id).Scan(
		&platform.ID,
		&platform.Name,
		&platform.Description,
		&platform.Website,
		&platform.DeletedAt,
		&platform.CreatedAt,
		&platform.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			logger.Warn("Platform not found", "platform_id", id)
			return nil, nil
		}
		logger.Error("Query failed", err, "platform_id", id)
		return nil, err
	}

	logger.Info("Retrieved platform", "platform_id", platform.ID)
	return &platform, nil
}

func (m *PlatformModel) GetByName(ctx context.Context, name string) (*Platform, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("GetByName")

	name = strings.TrimSpace(name)
	if name == "" {
		err := errors.New("name is required")
		logger.Error("Validation failed", err)
		return nil, err
	}

	query := `
		SELECT id, name, description, website, deleted_at, created_at, updated_at
		FROM platforms
		WHERE name = $1 AND deleted_at IS NULL
	`

	var platform Platform
	err := m.DB.QueryRow(ctx, query, name).Scan(
		&platform.ID,
		&platform.Name,
		&platform.Description,
		&platform.Website,
		&platform.DeletedAt,
		&platform.CreatedAt,
		&platform.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			logger.Info("Platform not found", "name", name)
			return nil, nil
		}
		logger.Error("Query failed", err)
		return nil, err
	}

	logger.Info("Platform retrieved successfully", "platform_id", platform.ID)
	return &platform, nil
}

func (m *PlatformModel) GetAll(ctx context.Context) ([]*Platform, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("GetAllPlatforms")

	query := `
		SELECT id, name, description, website, deleted_at, created_at, updated_at
		FROM platforms
		WHERE deleted_at IS NULL
		ORDER BY name ASC
	`

	rows, err := m.DB.Query(ctx, query)
	if err != nil {
		logger.Error("Query failed", err)
		return nil, err
	}
	defer rows.Close()

	var platforms []*Platform
	for rows.Next() {
		var p Platform
		if err := rows.Scan(
			&p.ID,
			&p.Name,
			&p.Description,
			&p.Website,
			&p.DeletedAt,
			&p.CreatedAt,
			&p.UpdatedAt,
		); err != nil {
			logger.Error("Row scan failed", err)
			return nil, err
		}
		platforms = append(platforms, &p)
	}

	if rows.Err() != nil {
		logger.Error("Row iteration error", rows.Err())
		return nil, rows.Err()
	}

	logger.Info("Retrieved platforms", "count", len(platforms))
	return platforms, nil
}

func (m *PlatformModel) Update(ctx context.Context, platform *Platform) error {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("UpdatePlatform")

	if platform == nil {
		err := errors.New("platform is required")
		logger.Error("Validation failed", err)
		return err
	}
	if platform.ID == uuid.Nil {
		err := errors.New("platform ID is required for update")
		logger.Error("Validation failed", err)
		return err
	}
	if platform.Name == "" {
		err := errors.New("name is required")
		logger.Error("Validation failed", err)
		return err
	}

	query := `
		UPDATE platforms
		SET name = $1, description = $2, website = $3
		WHERE id = $4 AND deleted_at IS NULL
		RETURNING created_at, updated_at, deleted_at
	`

	err := m.DB.QueryRow(ctx, query, platform.Name, platform.Description, platform.Website, platform.ID).
		Scan(&platform.CreatedAt, &platform.UpdatedAt, &platform.DeletedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			err = fmt.Errorf("no active platform found with ID %s", platform.ID)
		}
		logger.Error("Update platform failed", err)
		return err
	}

	logger.Info("Update platform successful", "platform_id", platform.ID)
	return nil
}

func (m *PlatformModel) SoftDelete(ctx context.Context, platformID uuid.UUID) error {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("SoftDeletePlatform")

	if platformID == uuid.Nil {
		err := errors.New("platform ID is required")
		logger.Error("Validation failed", err)
		return err
	}

	query := `
		UPDATE platforms
		SET deleted_at = NOW()
		WHERE id = $1 AND deleted_at IS NULL
	`

	result, err := m.DB.Exec(ctx, query, platformID)
	if err != nil {
		logger.Error("Soft delete platform failed", err)
		return err
	}

	if result.RowsAffected() == 0 {
		err := fmt.Errorf("no active platform found to delete with id %s", platformID)
		logger.Warn("Soft delete platform: no rows affected", "platform_id", platformID)
		return err
	}

	logger.Info("Soft delete platform successful", "platform_id", platformID)
	return nil
}