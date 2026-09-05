// Package data provides models and database access methods for products and related entities.
//
// File: sdworkspace/sdbackend/internal/data/products.go
//
// GTM:
//
//	Layer: 2.5 Catalog / Offer Domain
//	Release Class: DEFERRED
//	Reason:
//	  Products and brands are release-critical catalog support infrastructure.
//	  They provide product identity, brand identity, category association, and
//	  offer support for the public catalog. Merchant-product associations remain
//	  valid expanded catalog infrastructure but are not the v1 spine driver.
//
// SPINE Rule:
//
//	Keep compiling.
//	Keep production-ready.
//	Preserve product and brand canonical row alignment.
//	Preserve soft-delete lifecycle behavior for products and brands.
//	Preserve DB-owned lifecycle timestamp behavior.
//	Preserve brand-handle lookup behavior.
//	Preserve merchant_products association behavior without making it the v1 driver.
//	Block deployment if this file breaks build, product persistence,
//	brand persistence, offer support, catalog lookup, or catalog integrity.
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

// Product represents the canonical product record persisted in the products table.
// Soft-delete lifecycle is represented by DeletedAt, not by an is_deleted flag.
type Product struct {
	ID              uuid.UUID  `json:"id" db:"id"`
	Name            string     `json:"name" db:"name"`
	BrandID         *uuid.UUID `json:"brand_id,omitempty" db:"brand_id"`
	MarketSegmentID *uuid.UUID `json:"market_segment_id,omitempty" db:"market_segment_id"`
	CategoryID      *uuid.UUID `json:"category_id,omitempty" db:"category_id"`
	UPC             *string    `json:"upc,omitempty" db:"upc"`
	SKU             *string    `json:"sku,omitempty" db:"sku"`
	Description     *string    `json:"description,omitempty" db:"description"`
	ProductLine     *string    `json:"product_line,omitempty" db:"product_line"`
	IsComparable    bool       `json:"is_comparable" db:"is_comparable"`
	DeletedAt       *time.Time `json:"-" db:"deleted_at"`
	CreatedAt       time.Time  `json:"created_at" db:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at" db:"updated_at"`
}

// Brand represents the canonical brand record persisted in the brands table.
type Brand struct {
	ID          uuid.UUID  `json:"id" db:"id"`
	Name        string     `json:"name" db:"name"`
	BrandHandle *string    `json:"brand_handle,omitempty" db:"brand_handle"`
	DeletedAt   *time.Time `json:"-" db:"deleted_at"`
	CreatedAt   time.Time  `json:"created_at" db:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at" db:"updated_at"`
}

// MerchantProduct represents the canonical merchant-specific product association
// persisted in the merchant_products table.
type MerchantProduct struct {
	ID                 uuid.UUID `json:"id" db:"id"`
	ProductID          uuid.UUID `json:"product_id" db:"product_id"`
	MerchantID         uuid.UUID `json:"merchant_id" db:"merchant_id"`
	MerchantSKU        *string   `json:"merchant_sku,omitempty" db:"merchant_sku"`
	MerchantProductURL *string   `json:"merchant_product_url,omitempty" db:"merchant_product_url"`
	MerchantTitle      *string   `json:"merchant_title,omitempty" db:"merchant_title"`
	IsActive           bool      `json:"is_active" db:"is_active"`
	CreatedAt          time.Time `json:"created_at" db:"created_at"`
	UpdatedAt          time.Time `json:"updated_at" db:"updated_at"`
}

// ProductModel holds the DB pool and logger for product operations.
type ProductModel struct {
	DB     *pgxpool.Pool
	Logger *logging.Logger
}

// BrandModel holds the DB pool and logger for brand operations.
type BrandModel struct {
	DB     *pgxpool.Pool
	Logger *logging.Logger
}

// ProductMerchantModel is retained for continuity with models.go, but it now operates
// against the canonical merchant_products table.
type ProductMerchantModel struct {
	DB     *pgxpool.Pool
	Logger *logging.Logger
}

const productSelectColumns = "id, name, brand_id, market_segment_id, category_id, upc, sku, description, product_line, is_comparable, deleted_at, created_at, updated_at"
const productSelectColumnsAliased = "p.id, p.name, p.brand_id, p.market_segment_id, p.category_id, p.upc, p.sku, p.description, p.product_line, p.is_comparable, p.deleted_at, p.created_at, p.updated_at"
const brandSelectColumns = "id, name, brand_handle, deleted_at, created_at, updated_at"
const merchantProductSelectColumns = "id, product_id, merchant_id, merchant_sku, merchant_product_url, merchant_title, is_active, created_at, updated_at"

func scanProduct(scanner interface {
	Scan(dest ...any) error
}, product *Product) error {
	return scanner.Scan(
		&product.ID,
		&product.Name,
		&product.BrandID,
		&product.MarketSegmentID,
		&product.CategoryID,
		&product.UPC,
		&product.SKU,
		&product.Description,
		&product.ProductLine,
		&product.IsComparable,
		&product.DeletedAt,
		&product.CreatedAt,
		&product.UpdatedAt,
	)
}

func collectProduct(row pgx.CollectableRow) (*Product, error) {
	var product Product
	if err := scanProduct(row, &product); err != nil {
		return nil, err
	}
	return &product, nil
}

func scanBrand(scanner interface {
	Scan(dest ...any) error
}, brand *Brand) error {
	return scanner.Scan(
		&brand.ID,
		&brand.Name,
		&brand.BrandHandle,
		&brand.DeletedAt,
		&brand.CreatedAt,
		&brand.UpdatedAt,
	)
}

func collectBrand(row pgx.CollectableRow) (*Brand, error) {
	var brand Brand
	if err := scanBrand(row, &brand); err != nil {
		return nil, err
	}
	return &brand, nil
}

func scanMerchantProduct(scanner interface {
	Scan(dest ...any) error
}, mp *MerchantProduct) error {
	return scanner.Scan(
		&mp.ID,
		&mp.ProductID,
		&mp.MerchantID,
		&mp.MerchantSKU,
		&mp.MerchantProductURL,
		&mp.MerchantTitle,
		&mp.IsActive,
		&mp.CreatedAt,
		&mp.UpdatedAt,
	)
}

func collectMerchantProduct(row pgx.CollectableRow) (*MerchantProduct, error) {
	var mp MerchantProduct
	if err := scanMerchantProduct(row, &mp); err != nil {
		return nil, err
	}
	return &mp, nil
}

func normalizeProduct(product *Product) {
	if product == nil {
		return
	}

	product.Name = strings.TrimSpace(product.Name)

	if product.UPC != nil {
		trimmed := strings.TrimSpace(*product.UPC)
		if trimmed == "" {
			product.UPC = nil
		} else {
			product.UPC = &trimmed
		}
	}

	if product.SKU != nil {
		trimmed := strings.TrimSpace(*product.SKU)
		if trimmed == "" {
			product.SKU = nil
		} else {
			product.SKU = &trimmed
		}
	}

	if product.Description != nil {
		trimmed := strings.TrimSpace(*product.Description)
		if trimmed == "" {
			product.Description = nil
		} else {
			product.Description = &trimmed
		}
	}

	if product.ProductLine != nil {
		trimmed := strings.TrimSpace(*product.ProductLine)
		if trimmed == "" {
			product.ProductLine = nil
		} else {
			product.ProductLine = &trimmed
		}
	}
}

func normalizeBrand(brand *Brand) {
	if brand == nil {
		return
	}

	brand.Name = strings.TrimSpace(brand.Name)

	if brand.BrandHandle != nil {
		trimmed := strings.TrimSpace(*brand.BrandHandle)
		if trimmed == "" {
			brand.BrandHandle = nil
		} else {
			brand.BrandHandle = &trimmed
		}
	}
}

func normalizeMerchantProduct(mp *MerchantProduct) {
	if mp == nil {
		return
	}

	if mp.MerchantSKU != nil {
		trimmed := strings.TrimSpace(*mp.MerchantSKU)
		if trimmed == "" {
			mp.MerchantSKU = nil
		} else {
			mp.MerchantSKU = &trimmed
		}
	}

	if mp.MerchantProductURL != nil {
		trimmed := strings.TrimSpace(*mp.MerchantProductURL)
		if trimmed == "" {
			mp.MerchantProductURL = nil
		} else {
			mp.MerchantProductURL = &trimmed
		}
	}

	if mp.MerchantTitle != nil {
		trimmed := strings.TrimSpace(*mp.MerchantTitle)
		if trimmed == "" {
			mp.MerchantTitle = nil
		} else {
			mp.MerchantTitle = &trimmed
		}
	}
}

// Insert creates a new product.
// DB-owned id and timestamps are returned from PostgreSQL rather than written by application time.
func (m *ProductModel) Insert(ctx context.Context, product *Product) error {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("InsertProduct")

	if product == nil {
		err := errors.New("product is required")
		logger.Error("Validation failed", err)
		return err
	}

	normalizeProduct(product)

	if product.Name == "" {
		err := errors.New("product name is required")
		logger.Error("Validation failed", err)
		return err
	}

	query := `
		INSERT INTO products (
			name,
			brand_id,
			market_segment_id,
			category_id,
			upc,
			sku,
			description,
			product_line,
			is_comparable
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		RETURNING ` + productSelectColumns

	if err := scanProduct(
		m.DB.QueryRow(
			ctx,
			query,
			product.Name,
			product.BrandID,
			product.MarketSegmentID,
			product.CategoryID,
			product.UPC,
			product.SKU,
			product.Description,
			product.ProductLine,
			product.IsComparable,
		),
		product,
	); err != nil {
		logger.Error("Insert product failed", err)
		return err
	}

	logger.Info("Insert product successful", "product_id", product.ID)
	return nil
}

// GetByID returns a single active product by ID.
func (m *ProductModel) GetByID(ctx context.Context, id uuid.UUID) (*Product, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("GetProductByID")

	if id == uuid.Nil {
		err := errors.New("product ID is required")
		logger.Error("Validation failed", err)
		return nil, err
	}

	query := `
		SELECT ` + productSelectColumns + `
		FROM products
		WHERE id = $1
		  AND deleted_at IS NULL
	`

	var product Product
	err := scanProduct(m.DB.QueryRow(ctx, query, id), &product)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			logger.Warn("Product not found", "product_id", id)
			return nil, ErrProductNotFound
		}
		logger.Error("Get product failed", err)
		return nil, err
	}

	logger.Info("Get product successful", "product_id", product.ID)
	return &product, nil
}

// GetAll returns active products with pagination.
func (m *ProductModel) GetAll(ctx context.Context, limit, offset int) ([]*Product, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("GetAllProducts")

	if limit <= 0 {
		err := errors.New("limit must be greater than zero")
		logger.Error("Validation failed", err)
		return nil, err
	}
	if offset < 0 {
		err := errors.New("offset cannot be negative")
		logger.Error("Validation failed", err)
		return nil, err
	}
	if limit > 100 {
		limit = 100
	}

	query := `
		SELECT ` + productSelectColumns + `
		FROM products
		WHERE deleted_at IS NULL
		ORDER BY created_at DESC
		LIMIT $1 OFFSET $2
	`

	rows, err := m.DB.Query(ctx, query, limit, offset)
	if err != nil {
		logger.Error("Query execution failed", err)
		return nil, err
	}
	defer rows.Close()

	products, err := pgx.CollectRows(rows, collectProduct)
	if err != nil {
		logger.Error("Collect rows failed", err)
		return nil, err
	}

	logger.Info("GetAll products successful", "count", len(products))
	return products, nil
}

// GetByBrandID returns active products for a given active brand.
func (m *ProductModel) GetByBrandID(ctx context.Context, brandID uuid.UUID, limit, offset int) ([]*Product, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("GetByBrandID")

	if brandID == uuid.Nil {
		err := errors.New("brand ID is required")
		logger.Error("Validation failed", err)
		return nil, err
	}
	if limit <= 0 {
		err := errors.New("limit must be greater than zero")
		logger.Error("Validation failed", err)
		return nil, err
	}
	if offset < 0 {
		err := errors.New("offset cannot be negative")
		logger.Error("Validation failed", err)
		return nil, err
	}
	if limit > 100 {
		limit = 100
	}

	query := `
		SELECT ` + productSelectColumns + `
		FROM products
		WHERE brand_id = $1
		  AND deleted_at IS NULL
		ORDER BY created_at DESC
		LIMIT $2 OFFSET $3
	`

	rows, err := m.DB.Query(ctx, query, brandID, limit, offset)
	if err != nil {
		logger.Error("Query execution failed", err)
		return nil, err
	}
	defer rows.Close()

	products, err := pgx.CollectRows(rows, collectProduct)
	if err != nil {
		logger.Error("Collect rows failed", err)
		return nil, err
	}

	logger.Info("GetByBrandID successful", "brand_id", brandID, "count", len(products))
	return products, nil
}

// GetByMarketSegmentID returns active products for a given market segment.
func (m *ProductModel) GetByMarketSegmentID(ctx context.Context, marketSegmentID uuid.UUID, limit, offset int) ([]*Product, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("GetByMarketSegmentID")

	if marketSegmentID == uuid.Nil {
		err := errors.New("market segment ID is required")
		logger.Error("Validation failed", err)
		return nil, err
	}
	if limit <= 0 {
		err := errors.New("limit must be greater than zero")
		logger.Error("Validation failed", err)
		return nil, err
	}
	if offset < 0 {
		err := errors.New("offset cannot be negative")
		logger.Error("Validation failed", err)
		return nil, err
	}
	if limit > 100 {
		limit = 100
	}

	query := `
		SELECT ` + productSelectColumns + `
		FROM products
		WHERE market_segment_id = $1
		  AND deleted_at IS NULL
		ORDER BY created_at DESC
		LIMIT $2 OFFSET $3
	`

	rows, err := m.DB.Query(ctx, query, marketSegmentID, limit, offset)
	if err != nil {
		logger.Error("Query execution failed", err)
		return nil, err
	}
	defer rows.Close()

	products, err := pgx.CollectRows(rows, collectProduct)
	if err != nil {
		logger.Error("Collect rows failed", err)
		return nil, err
	}

	logger.Info("GetByMarketSegmentID successful", "market_segment_id", marketSegmentID, "count", len(products))
	return products, nil
}

// GetByCategoryID returns active products for a given category.
func (m *ProductModel) GetByCategoryID(ctx context.Context, categoryID uuid.UUID, limit, offset int) ([]*Product, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("GetByCategoryID")

	if categoryID == uuid.Nil {
		err := errors.New("category ID is required")
		logger.Error("Validation failed", err)
		return nil, err
	}
	if limit <= 0 {
		err := errors.New("limit must be greater than zero")
		logger.Error("Validation failed", err)
		return nil, err
	}
	if offset < 0 {
		err := errors.New("offset cannot be negative")
		logger.Error("Validation failed", err)
		return nil, err
	}
	if limit > 100 {
		limit = 100
	}

	query := `
		SELECT ` + productSelectColumns + `
		FROM products
		WHERE category_id = $1
		  AND deleted_at IS NULL
		ORDER BY created_at DESC
		LIMIT $2 OFFSET $3
	`

	rows, err := m.DB.Query(ctx, query, categoryID, limit, offset)
	if err != nil {
		logger.Error("Query execution failed", err)
		return nil, err
	}
	defer rows.Close()

	products, err := pgx.CollectRows(rows, collectProduct)
	if err != nil {
		logger.Error("Collect rows failed", err)
		return nil, err
	}

	logger.Info("GetByCategoryID successful", "category_id", categoryID, "count", len(products))
	return products, nil
}

// GetByUPC returns an active product by UPC.
func (m *ProductModel) GetByUPC(ctx context.Context, upc string) (*Product, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("GetProductByUPC")

	upc = strings.TrimSpace(upc)
	if upc == "" {
		err := errors.New("UPC is required")
		logger.Error("Validation failed", err)
		return nil, err
	}

	query := `
		SELECT ` + productSelectColumns + `
		FROM products
		WHERE upc = $1
		  AND deleted_at IS NULL
	`

	var product Product
	err := scanProduct(m.DB.QueryRow(ctx, query, upc), &product)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			logger.Warn("Product not found by UPC", "upc", upc)
			return nil, ErrProductNotFound
		}
		logger.Error("Get product by UPC failed", err)
		return nil, err
	}

	logger.Info("Get product by UPC successful", "product_id", product.ID)
	return &product, nil
}

// GetBySKU returns active products by SKU.
func (m *ProductModel) GetBySKU(ctx context.Context, sku string, limit, offset int) ([]*Product, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("GetProductsBySKU")

	sku = strings.TrimSpace(sku)
	if sku == "" {
		err := errors.New("SKU is required")
		logger.Error("Validation failed", err)
		return nil, err
	}
	if limit <= 0 {
		err := errors.New("limit must be greater than zero")
		logger.Error("Validation failed", err)
		return nil, err
	}
	if offset < 0 {
		err := errors.New("offset cannot be negative")
		logger.Error("Validation failed", err)
		return nil, err
	}
	if limit > 100 {
		limit = 100
	}

	query := `
		SELECT ` + productSelectColumns + `
		FROM products
		WHERE sku = $1
		  AND deleted_at IS NULL
		ORDER BY created_at DESC
		LIMIT $2 OFFSET $3
	`

	rows, err := m.DB.Query(ctx, query, sku, limit, offset)
	if err != nil {
		logger.Error("Get products by SKU failed", err)
		return nil, err
	}
	defer rows.Close()

	products, err := pgx.CollectRows(rows, collectProduct)
	if err != nil {
		logger.Error("Collect rows failed", err)
		return nil, err
	}

	logger.Info("Get products by SKU successful", "sku", sku, "count", len(products))
	return products, nil
}

// GetByProductLine returns active products by partial product line match.
func (m *ProductModel) GetByProductLine(ctx context.Context, productLine string, limit, offset int) ([]*Product, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("GetByProductLine")

	productLine = strings.TrimSpace(productLine)
	if productLine == "" {
		err := errors.New("product line is required")
		logger.Error("Validation failed", err)
		return nil, err
	}
	if limit <= 0 {
		err := errors.New("limit must be greater than zero")
		logger.Error("Validation failed", err)
		return nil, err
	}
	if offset < 0 {
		err := errors.New("offset cannot be negative")
		logger.Error("Validation failed", err)
		return nil, err
	}
	if limit > 100 {
		limit = 100
	}

	query := `
		SELECT ` + productSelectColumns + `
		FROM products
		WHERE product_line ILIKE $1
		  AND deleted_at IS NULL
		ORDER BY created_at DESC
		LIMIT $2 OFFSET $3
	`

	rows, err := m.DB.Query(ctx, query, "%"+productLine+"%", limit, offset)
	if err != nil {
		logger.Error("Query execution failed", err)
		return nil, err
	}
	defer rows.Close()

	products, err := pgx.CollectRows(rows, collectProduct)
	if err != nil {
		logger.Error("Collect rows failed", err)
		return nil, err
	}

	logger.Info("GetByProductLine successful", "product_line", productLine, "count", len(products))
	return products, nil
}

// GetByBrandHandle returns active products for an active brand handle.
func (m *ProductModel) GetByBrandHandle(ctx context.Context, brandHandle string, brandName *string, limit, offset int) ([]*Product, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("GetByBrandHandle")

	brandHandle = strings.TrimSpace(brandHandle)
	if brandHandle == "" {
		err := errors.New("brand handle is required")
		logger.Error("Validation failed", err)
		return nil, err
	}
	if limit <= 0 {
		err := errors.New("limit must be greater than zero")
		logger.Error("Validation failed", err)
		return nil, err
	}
	if offset < 0 {
		err := errors.New("offset cannot be negative")
		logger.Error("Validation failed", err)
		return nil, err
	}
	if limit > 100 {
		limit = 100
	}

	query := `
		SELECT ` + productSelectColumnsAliased + `
		FROM products p
		INNER JOIN brands b ON p.brand_id = b.id
		WHERE b.brand_handle = $1
		  AND b.deleted_at IS NULL
		  AND p.deleted_at IS NULL
	`
	args := []any{brandHandle}
	argPos := 2

	if brandName != nil {
		trimmedBrandName := strings.TrimSpace(*brandName)
		if trimmedBrandName != "" {
			query += fmt.Sprintf(" AND b.name ILIKE $%d", argPos)
			args = append(args, "%"+trimmedBrandName+"%")
			argPos++
		}
	}

	query += fmt.Sprintf(" ORDER BY p.created_at DESC LIMIT $%d OFFSET $%d", argPos, argPos+1)
	args = append(args, limit, offset)

	rows, err := m.DB.Query(ctx, query, args...)
	if err != nil {
		logger.Error("Query execution failed", err)
		return nil, err
	}
	defer rows.Close()

	products, err := pgx.CollectRows(rows, collectProduct)
	if err != nil {
		logger.Error("Collect rows failed", err)
		return nil, err
	}

	logger.Info("GetByBrandHandle successful", "brand_handle", brandHandle, "count", len(products))
	return products, nil
}

// Update updates an active product.
// DB-owned updated_at remains database-owned; the query sets NOW() and returns the canonical row.
func (m *ProductModel) Update(ctx context.Context, product *Product) error {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("UpdateProduct")

	if product == nil {
		err := errors.New("product is required")
		logger.Error("Validation failed", err)
		return err
	}
	if product.ID == uuid.Nil {
		err := errors.New("product ID is required")
		logger.Error("Validation failed", err)
		return err
	}

	normalizeProduct(product)

	if product.Name == "" {
		err := errors.New("product name is required")
		logger.Error("Validation failed", err)
		return err
	}

	query := `
		UPDATE products
		SET
			name = $1,
			brand_id = $2,
			market_segment_id = $3,
			category_id = $4,
			upc = $5,
			sku = $6,
			description = $7,
			product_line = $8,
			is_comparable = $9,
			updated_at = NOW()
		WHERE id = $10
		  AND deleted_at IS NULL
		RETURNING ` + productSelectColumns

	var updated Product
	err := scanProduct(
		m.DB.QueryRow(
			ctx,
			query,
			product.Name,
			product.BrandID,
			product.MarketSegmentID,
			product.CategoryID,
			product.UPC,
			product.SKU,
			product.Description,
			product.ProductLine,
			product.IsComparable,
			product.ID,
		),
		&updated,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			logger.Warn("Update product affected no rows", "product_id", product.ID)
			return ErrProductNotFound
		}
		logger.Error("Update product failed", err, "product_id", product.ID)
		return err
	}

	*product = updated

	logger.Info("Update product successful", "product_id", product.ID)
	return nil
}

// SoftDelete logically removes a product by setting deleted_at.
// Canonical persisted lifecycle time is DB-owned.
func (m *ProductModel) SoftDelete(ctx context.Context, id uuid.UUID) error {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("SoftDeleteProduct")

	if id == uuid.Nil {
		err := errors.New("product ID is required")
		logger.Error("Validation failed", err)
		return err
	}

	var deletedID uuid.UUID
	query := `
		UPDATE products
		SET
			deleted_at = NOW(),
			updated_at = NOW()
		WHERE id = $1
		  AND deleted_at IS NULL
		RETURNING id
	`

	err := m.DB.QueryRow(ctx, query, id).Scan(&deletedID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			logger.Warn("Soft delete affected no rows", "product_id", id)
			return ErrProductNotFound
		}
		logger.Error("Soft delete product failed", err)
		return err
	}

	logger.Info("Soft delete product successful", "product_id", deletedID)
	return nil
}

// Delete permanently removes a product row.
func (m *ProductModel) Delete(ctx context.Context, id uuid.UUID) error {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("DeleteProduct")

	if id == uuid.Nil {
		err := errors.New("product ID is required")
		logger.Error("Validation failed", err)
		return err
	}

	var deletedID uuid.UUID
	query := `
		DELETE FROM products
		WHERE id = $1
		RETURNING id
	`

	err := m.DB.QueryRow(ctx, query, id).Scan(&deletedID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			logger.Warn("Delete affected no rows", "product_id", id)
			return ErrProductNotFound
		}
		logger.Error("Delete product failed", err)
		return err
	}

	logger.Info("Delete product successful", "product_id", deletedID)
	return nil
}

// Exists checks whether an active product exists.
func (m *ProductModel) Exists(ctx context.Context, id uuid.UUID) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("ExistsProduct")

	if id == uuid.Nil {
		err := errors.New("product ID is required")
		logger.Error("Validation failed", err)
		return false, err
	}

	query := `SELECT EXISTS (SELECT 1 FROM products WHERE id = $1 AND deleted_at IS NULL)`

	var exists bool
	err := m.DB.QueryRow(ctx, query, id).Scan(&exists)
	if err != nil {
		logger.Error("Query failed", err)
		return false, err
	}

	logger.Info("Product existence check complete", "product_id", id, "exists", exists)
	return exists, nil
}

// Insert creates a new brand.
// DB owns id and lifecycle timestamps.
func (m *BrandModel) Insert(ctx context.Context, brand *Brand) error {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("InsertBrand")

	if brand == nil {
		err := errors.New("brand is required")
		logger.Error("Validation failed", err)
		return err
	}

	normalizeBrand(brand)

	if brand.Name == "" {
		err := errors.New("brand name is required")
		logger.Error("Validation failed", err)
		return err
	}

	query := `
		INSERT INTO brands (name, brand_handle)
		VALUES ($1, $2)
		RETURNING ` + brandSelectColumns

	if err := scanBrand(m.DB.QueryRow(ctx, query, brand.Name, brand.BrandHandle), brand); err != nil {
		logger.Error("Insert brand failed", err)
		return err
	}

	logger.Info("Insert brand successful", "brand_id", brand.ID)
	return nil
}

// GetByID returns one active brand by ID.
func (m *BrandModel) GetByID(ctx context.Context, id uuid.UUID) (*Brand, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("GetBrandByID")

	if id == uuid.Nil {
		err := errors.New("brand ID is required")
		logger.Error("Validation failed", err)
		return nil, err
	}

	query := `
		SELECT ` + brandSelectColumns + `
		FROM brands
		WHERE id = $1
		  AND deleted_at IS NULL
	`

	var brand Brand
	err := scanBrand(m.DB.QueryRow(ctx, query, id), &brand)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			logger.Warn("Brand not found", "brand_id", id)
			return nil, ErrBrandNotFound
		}
		logger.Error("Get brand failed", err)
		return nil, err
	}

	logger.Info("Get brand successful", "brand_id", brand.ID)
	return &brand, nil
}

// GetByName returns one active brand by name.
func (m *BrandModel) GetByName(ctx context.Context, name string) (*Brand, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("GetBrandByName")

	name = strings.TrimSpace(name)
	if name == "" {
		err := errors.New("brand name is required")
		logger.Error("Validation failed", err)
		return nil, err
	}

	query := `
		SELECT ` + brandSelectColumns + `
		FROM brands
		WHERE name = $1
		  AND deleted_at IS NULL
	`

	var brand Brand
	err := scanBrand(m.DB.QueryRow(ctx, query, name), &brand)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			logger.Warn("Brand not found by name", "name", name)
			return nil, ErrBrandNotFound
		}
		logger.Error("Get brand by name failed", err)
		return nil, err
	}

	logger.Info("Get brand by name successful", "brand_id", brand.ID)
	return &brand, nil
}

// GetAll returns active brands with pagination.
func (m *BrandModel) GetAll(ctx context.Context, limit, offset int) ([]*Brand, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("GetAllBrands")

	if limit <= 0 {
		err := errors.New("limit must be greater than zero")
		logger.Error("Validation failed", err)
		return nil, err
	}
	if offset < 0 {
		err := errors.New("offset cannot be negative")
		logger.Error("Validation failed", err)
		return nil, err
	}
	if limit > 100 {
		limit = 100
	}

	query := `
		SELECT ` + brandSelectColumns + `
		FROM brands
		WHERE deleted_at IS NULL
		ORDER BY created_at DESC
		LIMIT $1 OFFSET $2
	`

	rows, err := m.DB.Query(ctx, query, limit, offset)
	if err != nil {
		logger.Error("Query failed", err)
		return nil, err
	}
	defer rows.Close()

	brands, err := pgx.CollectRows(rows, collectBrand)
	if err != nil {
		logger.Error("Collect rows failed", err)
		return nil, err
	}

	logger.Info("GetAll brands successful", "count", len(brands))
	return brands, nil
}

// Update updates an active brand.
func (m *BrandModel) Update(ctx context.Context, brand *Brand) error {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("UpdateBrand")

	if brand == nil {
		err := errors.New("brand is required")
		logger.Error("Validation failed", err)
		return err
	}
	if brand.ID == uuid.Nil {
		err := errors.New("brand ID is required")
		logger.Error("Validation failed", err)
		return err
	}

	normalizeBrand(brand)

	if brand.Name == "" {
		err := errors.New("brand name is required")
		logger.Error("Validation failed", err)
		return err
	}

	query := `
		UPDATE brands
		SET
			name = $1,
			brand_handle = $2,
			updated_at = NOW()
		WHERE id = $3
		  AND deleted_at IS NULL
		RETURNING ` + brandSelectColumns

	var updated Brand
	err := scanBrand(m.DB.QueryRow(ctx, query, brand.Name, brand.BrandHandle, brand.ID), &updated)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			logger.Warn("Update brand affected no rows", "brand_id", brand.ID)
			return ErrBrandNotFound
		}
		logger.Error("Update brand failed", err)
		return err
	}

	*brand = updated

	logger.Info("Update brand successful", "brand_id", brand.ID)
	return nil
}

// SoftDelete logically removes a brand by setting deleted_at.
func (m *BrandModel) SoftDelete(ctx context.Context, id uuid.UUID) error {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("SoftDeleteBrand")

	if id == uuid.Nil {
		err := errors.New("brand ID is required")
		logger.Error("Validation failed", err)
		return err
	}

	var deletedID uuid.UUID
	query := `
		UPDATE brands
		SET
			deleted_at = NOW(),
			updated_at = NOW()
		WHERE id = $1
		  AND deleted_at IS NULL
		RETURNING id
	`

	err := m.DB.QueryRow(ctx, query, id).Scan(&deletedID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			logger.Warn("Soft delete brand affected no rows", "brand_id", id)
			return ErrBrandNotFound
		}
		logger.Error("Soft delete brand failed", err)
		return err
	}

	logger.Info("Soft delete brand successful", "brand_id", deletedID)
	return nil
}

// Delete permanently removes a brand row.
func (m *BrandModel) Delete(ctx context.Context, id uuid.UUID) error {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("DeleteBrand")

	if id == uuid.Nil {
		err := errors.New("brand ID is required")
		logger.Error("Validation failed", err)
		return err
	}

	var deletedID uuid.UUID
	query := `
		DELETE FROM brands
		WHERE id = $1
		RETURNING id
	`

	err := m.DB.QueryRow(ctx, query, id).Scan(&deletedID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			logger.Warn("Delete brand affected no rows", "brand_id", id)
			return ErrBrandNotFound
		}
		logger.Error("Delete brand failed", err)
		return err
	}

	logger.Info("Delete brand successful", "brand_id", deletedID)
	return nil
}

// Insert creates a merchant_products record.
func (m *ProductMerchantModel) Insert(ctx context.Context, mp *MerchantProduct) error {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("InsertMerchantProduct")

	if mp == nil {
		err := errors.New("merchant product is required")
		logger.Error("Validation failed", err)
		return err
	}
	if mp.ProductID == uuid.Nil {
		err := errors.New("product ID is required")
		logger.Error("Validation failed", err)
		return err
	}
	if mp.MerchantID == uuid.Nil {
		err := errors.New("merchant ID is required")
		logger.Error("Validation failed", err)
		return err
	}

	normalizeMerchantProduct(mp)

	query := `
		INSERT INTO merchant_products (
			product_id,
			merchant_id,
			merchant_sku,
			merchant_product_url,
			merchant_title,
			is_active
		)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING ` + merchantProductSelectColumns

	if err := scanMerchantProduct(
		m.DB.QueryRow(
			ctx,
			query,
			mp.ProductID,
			mp.MerchantID,
			mp.MerchantSKU,
			mp.MerchantProductURL,
			mp.MerchantTitle,
			mp.IsActive,
		),
		mp,
	); err != nil {
		logger.Error("Insert merchant product failed", err)
		return err
	}

	logger.Info("Insert merchant product successful", "merchant_product_id", mp.ID)
	return nil
}

// Exists checks whether a merchant_products association exists.
func (m *ProductMerchantModel) Exists(ctx context.Context, productID, merchantID uuid.UUID) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("ExistsMerchantProduct")

	if productID == uuid.Nil || merchantID == uuid.Nil {
		err := errors.New("product ID and merchant ID are required")
		logger.Error("Validation failed", err)
		return false, err
	}

	query := `
		SELECT EXISTS (
			SELECT 1
			FROM merchant_products
			WHERE product_id = $1
			  AND merchant_id = $2
		)
	`

	var exists bool
	err := m.DB.QueryRow(ctx, query, productID, merchantID).Scan(&exists)
	if err != nil {
		logger.Error("Query execution failed", err)
		return false, err
	}

	logger.Info("Merchant product existence check complete", "product_id", productID, "merchant_id", merchantID, "exists", exists)
	return exists, nil
}

// GetByProductID returns merchant_products for a product.
func (m *ProductMerchantModel) GetByProductID(ctx context.Context, productID uuid.UUID) ([]*MerchantProduct, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("GetMerchantProductsByProductID")

	if productID == uuid.Nil {
		err := errors.New("product ID is required")
		logger.Error("Validation failed", err)
		return nil, err
	}

	query := `
		SELECT ` + merchantProductSelectColumns + `
		FROM merchant_products
		WHERE product_id = $1
		ORDER BY created_at DESC
	`

	rows, err := m.DB.Query(ctx, query, productID)
	if err != nil {
		logger.Error("Query failed", err)
		return nil, err
	}
	defer rows.Close()

	results, err := pgx.CollectRows(rows, collectMerchantProduct)
	if err != nil {
		logger.Error("Collect rows failed", err)
		return nil, err
	}

	logger.Info("Retrieved merchant products by product ID", "product_id", productID, "count", len(results))
	return results, nil
}

// GetByMerchantID returns merchant_products for a merchant.
func (m *ProductMerchantModel) GetByMerchantID(ctx context.Context, merchantID uuid.UUID) ([]*MerchantProduct, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("GetMerchantProductsByMerchantID")

	if merchantID == uuid.Nil {
		err := errors.New("merchant ID is required")
		logger.Error("Validation failed", err)
		return nil, err
	}

	query := `
		SELECT ` + merchantProductSelectColumns + `
		FROM merchant_products
		WHERE merchant_id = $1
		ORDER BY created_at DESC
	`

	rows, err := m.DB.Query(ctx, query, merchantID)
	if err != nil {
		logger.Error("Query failed", err)
		return nil, err
	}
	defer rows.Close()

	results, err := pgx.CollectRows(rows, collectMerchantProduct)
	if err != nil {
		logger.Error("Collect rows failed", err)
		return nil, err
	}

	logger.Info("Retrieved merchant products by merchant ID", "merchant_id", merchantID, "count", len(results))
	return results, nil
}

// GetAll returns active products joined through merchant_products with optional merchant and brand filters.
func (m *ProductMerchantModel) GetAll(ctx context.Context, merchantID, brandID *uuid.UUID, limit, offset int) ([]*Product, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("GetAllMerchantProducts")

	if limit <= 0 {
		err := errors.New("limit must be greater than zero")
		logger.Error("Validation failed", err)
		return nil, err
	}
	if offset < 0 {
		err := errors.New("offset cannot be negative")
		logger.Error("Validation failed", err)
		return nil, err
	}
	if limit > 100 {
		limit = 100
	}

	args := make([]any, 0, 4)
	query := `
		SELECT ` + productSelectColumnsAliased + `
		FROM products p
		INNER JOIN merchant_products mp ON p.id = mp.product_id
		WHERE p.deleted_at IS NULL
	`
	argPos := 1

	if merchantID != nil {
		query += fmt.Sprintf(" AND mp.merchant_id = $%d", argPos)
		args = append(args, *merchantID)
		argPos++
	}

	if brandID != nil {
		query += fmt.Sprintf(" AND p.brand_id = $%d", argPos)
		args = append(args, *brandID)
		argPos++
	}

	query += fmt.Sprintf(" ORDER BY p.created_at DESC LIMIT $%d OFFSET $%d", argPos, argPos+1)
	args = append(args, limit, offset)

	rows, err := m.DB.Query(ctx, query, args...)
	if err != nil {
		logger.Error("GetAll merchant products query failed", err)
		return nil, err
	}
	defer rows.Close()

	products, err := pgx.CollectRows(rows, collectProduct)
	if err != nil {
		logger.Error("Collect rows failed", err)
		return nil, err
	}

	logger.Info("GetAll merchant products successful", "count", len(products))
	return products, nil
}

// SoftDelete for merchant-linked products is not a lifecycle soft delete.
// The canonical merchant_products table uses hard delete or is_active workflow.
// This method remains for interface continuity and performs a true delete of the association.
func (m *ProductMerchantModel) SoftDelete(ctx context.Context, productID, merchantID uuid.UUID) error {
	return m.Delete(ctx, productID, merchantID)
}

// Delete permanently removes a merchant_products association row.
func (m *ProductMerchantModel) Delete(ctx context.Context, productID, merchantID uuid.UUID) error {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("DeleteMerchantProduct")

	if productID == uuid.Nil || merchantID == uuid.Nil {
		err := errors.New("product ID and merchant ID are required")
		logger.Error("Validation failed", err)
		return err
	}

	var deletedID uuid.UUID
	query := `
		DELETE FROM merchant_products
		WHERE product_id = $1
		  AND merchant_id = $2
		RETURNING id
	`

	err := m.DB.QueryRow(ctx, query, productID, merchantID).Scan(&deletedID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			logger.Warn("Delete merchant product affected no rows", "product_id", productID, "merchant_id", merchantID)
			return ErrMerchantProductNotFound
		}
		logger.Error("Delete merchant product failed", err)
		return err
	}

	logger.Info("Delete merchant product successful", "merchant_product_id", deletedID, "product_id", productID, "merchant_id", merchantID)
	return nil
}
