// Package data provides models and database access methods for merchant promotions and other entities.
//
// sdworkspace/sdbackend/internal/data/merchant_promotions.go
//
// GTM:
//   Layer: 2.5 Catalog / Offer Domain
//   Release Class: DEFERRED
//   Reason:
//     Merchant promotions are valid future merchandising and campaign
//     infrastructure, but they are not required for the initial SagrentiDeals
//     release spine. The v1 spine requires canonical offers, publication status,
//     affiliate links, click tracking, price history, and merchant/catalog
//     foundations before expanding into promotion workflow management.
//
// DEFERRED Rule:
//   Keep compiling.
//   Keep safe.
//   Preserve merchant-owned promotion semantics.
//   Preserve storewide vs targeted-offer distinction.
//   Preserve soft-delete lifecycle behavior.
//   Preserve normalized merchant_promotion_offers join behavior.
//   Do not add new features.
//   Do not route into v1 UI/API expansion.
//   Do not block deployment on this file unless it breaks the build.
package data

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/PiccoloMondoC/sdworkspace/sdbackend/internal/logging"
	"github.com/PiccoloMondoC/sdworkspace/sdbackend/internal/utils/timeutil"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const merchantPromotionSelectColumns = `
	mp.id,
	mp.merchant_id,
	mp.promotion_id,
	mp.storewide,
	COALESCE(
		ARRAY_AGG(mpo.offer_id ORDER BY mpo.offer_id) FILTER (WHERE mpo.offer_id IS NOT NULL),
		'{}'::uuid[]
	) AS target_offer_ids,
	mp.start_date,
	mp.end_date,
	mp.deleted_at,
	mp.created_at,
	mp.updated_at
`

const merchantPromotionSelectBase = `
	FROM merchant_promotions mp
	LEFT JOIN merchant_promotion_offers mpo
		ON mpo.merchant_promotion_id = mp.id
`

const merchantPromotionGroupBy = `
	GROUP BY
		mp.id,
		mp.merchant_id,
		mp.promotion_id,
		mp.storewide,
		mp.start_date,
		mp.end_date,
		mp.deleted_at,
		mp.created_at,
		mp.updated_at
`

// MerchantPromotion is the canonical persisted/read model for merchant promotions.
//
// Targeted offers are not stored inline on merchant_promotions. They are derived from the
// normalized merchant_promotion_offers join table and exposed here as TargetOfferIDs.
//
// StartDate and EndDate are always stored and returned in UTC per §5.3 of Backend Engineer
// Guidance V1.4. Callers must normalize to UTC before passing values to Insert or Update.
//
// DeletedAt is the soft-delete timestamp. All standard read and mutation paths filter on
// deleted_at IS NULL. Use dedicated admin queries to access soft-deleted records.
// DeletedAt is intentionally excluded from JSON responses via json:"-".
type MerchantPromotion struct {
	ID             uuid.UUID  `json:"id" db:"id"`
	MerchantID     uuid.UUID  `json:"merchant_id" db:"merchant_id"`
	PromotionID    uuid.UUID  `json:"promotion_id" db:"promotion_id"`
	StoreWide      bool       `json:"storewide" db:"storewide"`
	TargetOfferIDs []uuid.UUID `json:"target_offer_ids,omitempty" db:"target_offer_ids"`
	StartDate      time.Time  `json:"start_date" db:"start_date"`
	EndDate        time.Time  `json:"end_date" db:"end_date"`
	DeletedAt      *time.Time `json:"-" db:"deleted_at"`
	CreatedAt      time.Time  `json:"created_at" db:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at" db:"updated_at"`
}

// MerchantPromotionModel is the structure which holds the DB instance.
type MerchantPromotionModel struct {
	DB     *pgxpool.Pool
	Logger *logging.Logger
}

// Insert inserts a new merchant promotion into the database and, for non-storewide promotions,
// inserts the targeted offer links into merchant_promotion_offers atomically.
func (m *MerchantPromotionModel) Insert(ctx context.Context, merchantPromotion *MerchantPromotion) error {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("InsertMerchantPromotion")

	if merchantPromotion == nil {
		err := errors.New("merchant promotion is required")
		logger.Error("Validation failed", "error", err)
		return err
	}

	merchantPromotion.StartDate = merchantPromotion.StartDate.UTC()
	merchantPromotion.EndDate = merchantPromotion.EndDate.UTC()
	merchantPromotion.TargetOfferIDs = uniqueUUIDs(merchantPromotion.TargetOfferIDs)
	// Ensure callers cannot inject a pre-set deleted_at on insert.
	merchantPromotion.DeletedAt = nil

	if err := validateMerchantPromotion(merchantPromotion); err != nil {
		logger.Error("Validation failed", "error", err)
		return err
	}

	if merchantPromotion.ID == uuid.Nil {
		merchantPromotion.ID = uuid.New()
	}

	tx, err := m.DB.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		logger.Error("Begin transaction failed", "error", err)
		return fmt.Errorf("begin merchant promotion insert transaction: %w", err)
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	if !merchantPromotion.StoreWide {
		if err := m.validateOffersBelongToMerchant(ctx, tx, merchantPromotion.MerchantID, merchantPromotion.TargetOfferIDs); err != nil {
			logger.Error("Target-offer validation failed", "error", err)
			return err
		}
	}

	insertQuery := `
		INSERT INTO merchant_promotions (
			id,
			merchant_id,
			promotion_id,
			storewide,
			start_date,
			end_date
		)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING created_at, updated_at
	`

	if err := tx.QueryRow(
		ctx,
		insertQuery,
		merchantPromotion.ID,
		merchantPromotion.MerchantID,
		merchantPromotion.PromotionID,
		merchantPromotion.StoreWide,
		merchantPromotion.StartDate,
		merchantPromotion.EndDate,
	).Scan(&merchantPromotion.CreatedAt, &merchantPromotion.UpdatedAt); err != nil {
		logger.Error("Insert merchant promotion failed", "error", err)
		return fmt.Errorf("insert merchant promotion: %w", err)
	}

	if !merchantPromotion.StoreWide {
		if err := m.insertMerchantPromotionOffers(ctx, tx, merchantPromotion.ID, merchantPromotion.TargetOfferIDs); err != nil {
			logger.Error("Insert merchant promotion offers failed", "error", err)
			return err
		}
	}

	if err := tx.Commit(ctx); err != nil {
		logger.Error("Commit transaction failed", "error", err)
		return fmt.Errorf("commit merchant promotion insert transaction: %w", err)
	}

	logger.Info("Insert merchant promotion successful", "merchant_promotion_id", merchantPromotion.ID)
	return nil
}

// AddOffersToMerchantPromotion attaches targeted offers to an existing non-storewide merchant promotion.
func (m *MerchantPromotionModel) AddOffersToMerchantPromotion(ctx context.Context, merchantPromotionID uuid.UUID, offerIDs []uuid.UUID) error {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("AddOffersToMerchantPromotion")

	if merchantPromotionID == uuid.Nil {
		err := errors.New("merchant_promotion_id is required")
		logger.Error("Validation failed", "error", err)
		return err
	}

	offerIDs = uniqueUUIDs(offerIDs)
	if len(offerIDs) == 0 {
		err := errors.New("at least one offer_id is required")
		logger.Error("Validation failed", "error", err)
		return err
	}

	tx, err := m.DB.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		logger.Error("Begin transaction failed", "error", err)
		return fmt.Errorf("begin merchant promotion offer-link transaction: %w", err)
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	var merchantID uuid.UUID
	var storewide bool

	parentQuery := `
		SELECT merchant_id, storewide
		FROM merchant_promotions
		WHERE id = $1
		  AND deleted_at IS NULL
	`

	err = tx.QueryRow(ctx, parentQuery, merchantPromotionID).Scan(&merchantID, &storewide)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			logger.Warn("Merchant promotion not found", "merchant_promotion_id", merchantPromotionID)
			return ErrMerchantPromotionNotFound
		}
		logger.Error("Load merchant promotion failed", "error", err, "merchant_promotion_id", merchantPromotionID)
		return fmt.Errorf("load merchant promotion: %w", err)
	}

	if storewide {
		err := errors.New("cannot attach targeted offers to a storewide promotion")
		logger.Error("Validation failed", "error", err, "merchant_promotion_id", merchantPromotionID)
		return err
	}

	if err := m.validateOffersBelongToMerchant(ctx, tx, merchantID, offerIDs); err != nil {
		logger.Error("Target-offer validation failed", "error", err, "merchant_id", merchantID)
		return err
	}

	if err := m.insertMerchantPromotionOffers(ctx, tx, merchantPromotionID, offerIDs); err != nil {
		logger.Error("Insert merchant promotion offers failed", "error", err, "merchant_promotion_id", merchantPromotionID)
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		logger.Error("Commit transaction failed", "error", err)
		return fmt.Errorf("commit merchant promotion offer-link transaction: %w", err)
	}

	logger.Info("Add offers to merchant promotion successful", "merchant_promotion_id", merchantPromotionID, "offer_count", len(offerIDs))
	return nil
}

// GetByID retrieves an active merchant promotion by its ID, including normalized target offer links.
// Soft-deleted promotions are not returned; callers receive ErrMerchantPromotionNotFound instead.
func (m *MerchantPromotionModel) GetByID(ctx context.Context, id uuid.UUID) (*MerchantPromotion, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("GetMerchantPromotionByID")

	if id == uuid.Nil {
		err := errors.New("invalid merchant promotion ID")
		logger.Error("Validation failed", "error", err)
		return nil, err
	}

	query := `
		SELECT ` + merchantPromotionSelectColumns + `
		` + merchantPromotionSelectBase + `
		WHERE mp.id = $1
		  AND mp.deleted_at IS NULL
		` + merchantPromotionGroupBy

	var merchantPromotion MerchantPromotion

	err := scanMerchantPromotion(
		m.DB.QueryRow(ctx, query, id).Scan,
		&merchantPromotion,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			logger.Warn("Merchant promotion not found", "merchant_promotion_id", id)
			return nil, ErrMerchantPromotionNotFound
		}
		logger.Error("Get merchant promotion by ID failed", "error", err, "merchant_promotion_id", id)
		return nil, fmt.Errorf("get merchant promotion by id: %w", err)
	}

	logger.Info("Get merchant promotion by ID successful", "merchant_promotion_id", merchantPromotion.ID)
	return &merchantPromotion, nil
}

// Update updates an existing active merchant promotion and atomically replaces its target offer
// links when the promotion is non-storewide. Soft-deleted promotions cannot be updated.
func (m *MerchantPromotionModel) Update(ctx context.Context, merchantPromotion *MerchantPromotion) error {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("UpdateMerchantPromotion")

	if merchantPromotion == nil {
		err := errors.New("merchant promotion is required")
		logger.Error("Validation failed", "error", err)
		return err
	}

	if merchantPromotion.ID == uuid.Nil {
		err := errors.New("merchant promotion ID is required for update")
		logger.Error("Validation failed", "error", err)
		return err
	}

	merchantPromotion.StartDate = merchantPromotion.StartDate.UTC()
	merchantPromotion.EndDate = merchantPromotion.EndDate.UTC()
	merchantPromotion.TargetOfferIDs = uniqueUUIDs(merchantPromotion.TargetOfferIDs)

	if err := validateMerchantPromotion(merchantPromotion); err != nil {
		logger.Error("Validation failed", "error", err)
		return err
	}

	tx, err := m.DB.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		logger.Error("Begin transaction failed", "error", err)
		return fmt.Errorf("begin merchant promotion update transaction: %w", err)
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	updateQuery := `
		UPDATE merchant_promotions
		SET
			merchant_id  = $1,
			promotion_id = $2,
			storewide    = $3,
			start_date   = $4,
			end_date     = $5
		WHERE id = $6
		  AND deleted_at IS NULL
		RETURNING created_at, updated_at
	`

	err = tx.QueryRow(
		ctx,
		updateQuery,
		merchantPromotion.MerchantID,
		merchantPromotion.PromotionID,
		merchantPromotion.StoreWide,
		merchantPromotion.StartDate,
		merchantPromotion.EndDate,
		merchantPromotion.ID,
	).Scan(&merchantPromotion.CreatedAt, &merchantPromotion.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			logger.Warn("Merchant promotion not found", "merchant_promotion_id", merchantPromotion.ID)
			return ErrMerchantPromotionNotFound
		}
		logger.Error("Update merchant promotion failed", "error", err, "merchant_promotion_id", merchantPromotion.ID)
		return fmt.Errorf("update merchant promotion: %w", err)
	}

	if err := m.replaceMerchantPromotionOffers(ctx, tx, merchantPromotion); err != nil {
		logger.Error("Replace merchant promotion offers failed", "error", err, "merchant_promotion_id", merchantPromotion.ID)
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		logger.Error("Commit transaction failed", "error", err)
		return fmt.Errorf("commit merchant promotion update transaction: %w", err)
	}

	logger.Info("Update merchant promotion successful", "merchant_promotion_id", merchantPromotion.ID)
	return nil
}

// ListByMerchantID retrieves all active merchant promotions for a given merchant.
func (m *MerchantPromotionModel) ListByMerchantID(ctx context.Context, merchantID uuid.UUID) ([]*MerchantPromotion, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("ListMerchantPromotionsByMerchantID")

	if merchantID == uuid.Nil {
		err := errors.New("invalid merchant ID")
		logger.Error("Validation failed", "error", err)
		return nil, err
	}

	query := `
		SELECT ` + merchantPromotionSelectColumns + `
		` + merchantPromotionSelectBase + `
		WHERE mp.merchant_id = $1
		  AND mp.deleted_at IS NULL
		` + merchantPromotionGroupBy + `
		ORDER BY mp.created_at DESC
	`

	promotions, err := m.listMerchantPromotions(ctx, query, merchantID)
	if err != nil {
		logger.Error("List merchant promotions by merchant ID failed", "error", err, "merchant_id", merchantID)
		return nil, err
	}

	logger.Info("List merchant promotions by merchant ID successful", "merchant_id", merchantID, "count", len(promotions))
	return promotions, nil
}

// ListByPromotionID retrieves all active merchant promotions associated with a given promotion ID.
func (m *MerchantPromotionModel) ListByPromotionID(ctx context.Context, promotionID uuid.UUID) ([]*MerchantPromotion, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("ListMerchantPromotionsByPromotionID")

	if promotionID == uuid.Nil {
		err := errors.New("invalid promotion ID")
		logger.Error("Validation failed", "error", err)
		return nil, err
	}

	query := `
		SELECT ` + merchantPromotionSelectColumns + `
		` + merchantPromotionSelectBase + `
		WHERE mp.promotion_id = $1
		  AND mp.deleted_at IS NULL
		` + merchantPromotionGroupBy + `
		ORDER BY mp.created_at DESC
	`

	promotions, err := m.listMerchantPromotions(ctx, query, promotionID)
	if err != nil {
		logger.Error("List merchant promotions by promotion ID failed", "error", err, "promotion_id", promotionID)
		return nil, err
	}

	logger.Info("List merchant promotions by promotion ID successful", "promotion_id", promotionID, "count", len(promotions))
	return promotions, nil
}

// ListActivePromotions retrieves all currently active, non-deleted merchant promotions.
func (m *MerchantPromotionModel) ListActivePromotions(ctx context.Context) ([]*MerchantPromotion, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("ListActiveMerchantPromotions")

	now := timeutil.Now()

	query := `
		SELECT ` + merchantPromotionSelectColumns + `
		` + merchantPromotionSelectBase + `
		WHERE mp.deleted_at IS NULL
		  AND mp.start_date <= $1
		  AND mp.end_date   >= $1
		` + merchantPromotionGroupBy + `
		ORDER BY mp.start_date ASC, mp.created_at DESC
	`

	promotions, err := m.listMerchantPromotions(ctx, query, now)
	if err != nil {
		logger.Error("List active merchant promotions failed", "error", err)
		return nil, err
	}

	logger.Info("List active merchant promotions successful", "count", len(promotions))
	return promotions, nil
}

// ListAllPromotions retrieves all non-deleted merchant promotions.
func (m *MerchantPromotionModel) ListAllPromotions(ctx context.Context) ([]*MerchantPromotion, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("ListAllMerchantPromotions")

	query := `
		SELECT ` + merchantPromotionSelectColumns + `
		` + merchantPromotionSelectBase + `
		WHERE mp.deleted_at IS NULL
		` + merchantPromotionGroupBy + `
		ORDER BY mp.created_at DESC
	`

	promotions, err := m.listMerchantPromotions(ctx, query)
	if err != nil {
		logger.Error("List all merchant promotions failed", "error", err)
		return nil, err
	}

	logger.Info("List all merchant promotions successful", "count", len(promotions))
	return promotions, nil
}

// GetPromotionsByMerchantID retrieves active promotions belonging to the supplied merchant.
// This is intentionally aligned to merchant_id ownership semantics.
func (m *MerchantPromotionModel) GetPromotionsByMerchantID(ctx context.Context, merchantID uuid.UUID) ([]*MerchantPromotion, error) {
	return m.ListByMerchantID(ctx, merchantID)
}

// ListUpcomingPromotions retrieves all non-deleted merchant promotions scheduled to start in the future.
func (m *MerchantPromotionModel) ListUpcomingPromotions(ctx context.Context) ([]*MerchantPromotion, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("ListUpcomingMerchantPromotions")

	now := timeutil.Now()

	query := `
		SELECT ` + merchantPromotionSelectColumns + `
		` + merchantPromotionSelectBase + `
		WHERE mp.deleted_at IS NULL
		  AND mp.start_date > $1
		` + merchantPromotionGroupBy + `
		ORDER BY mp.start_date ASC
	`

	promotions, err := m.listMerchantPromotions(ctx, query, now)
	if err != nil {
		logger.Error("List upcoming merchant promotions failed", "error", err)
		return nil, err
	}

	logger.Info("List upcoming merchant promotions successful", "count", len(promotions))
	return promotions, nil
}

// ListExpiredPromotions retrieves all non-deleted merchant promotions that have already ended.
func (m *MerchantPromotionModel) ListExpiredPromotions(ctx context.Context) ([]*MerchantPromotion, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("ListExpiredMerchantPromotions")

	now := timeutil.Now()

	query := `
		SELECT ` + merchantPromotionSelectColumns + `
		` + merchantPromotionSelectBase + `
		WHERE mp.deleted_at IS NULL
		  AND mp.end_date < $1
		` + merchantPromotionGroupBy + `
		ORDER BY mp.end_date DESC
	`

	promotions, err := m.listMerchantPromotions(ctx, query, now)
	if err != nil {
		logger.Error("List expired merchant promotions failed", "error", err)
		return nil, err
	}

	logger.Info("List expired merchant promotions successful", "count", len(promotions))
	return promotions, nil
}

// SearchPromotions searches active merchant promotions by optional merchant ID, promotion ID,
// and date-window filters. All date filter values are normalized to UTC before use.
func (m *MerchantPromotionModel) SearchPromotions(ctx context.Context, merchantID, promotionID uuid.UUID, startDate, endDate time.Time) ([]*MerchantPromotion, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("SearchMerchantPromotions")

	var conditions []string
	var args []any

	if merchantID != uuid.Nil {
		args = append(args, merchantID)
		conditions = append(conditions, fmt.Sprintf("mp.merchant_id = $%d", len(args)))
	}

	if promotionID != uuid.Nil {
		args = append(args, promotionID)
		conditions = append(conditions, fmt.Sprintf("mp.promotion_id = $%d", len(args)))
	}

	if !startDate.IsZero() {
		args = append(args, startDate.UTC())
		conditions = append(conditions, fmt.Sprintf("mp.start_date >= $%d", len(args)))
	}

	if !endDate.IsZero() {
		args = append(args, endDate.UTC())
		conditions = append(conditions, fmt.Sprintf("mp.end_date <= $%d", len(args)))
	}

	// deleted_at IS NULL is always the first condition. Optional filters are appended with AND.
	whereClause := "WHERE mp.deleted_at IS NULL"
	if len(conditions) > 0 {
		whereClause += "\n\t\t  AND " + strings.Join(conditions, "\n\t\t  AND ")
	}

	query := `
		SELECT ` + merchantPromotionSelectColumns + `
		` + merchantPromotionSelectBase + `
		` + whereClause + `
		` + merchantPromotionGroupBy + `
		ORDER BY mp.created_at DESC
	`

	promotions, err := m.listMerchantPromotions(ctx, query, args...)
	if err != nil {
		logger.Error("Search merchant promotions failed", "error", err)
		return nil, err
	}

	logger.Info("Search merchant promotions successful", "result_count", len(promotions))
	return promotions, nil
}

// ExtendPromotionDates atomically updates the start and/or end dates of an active merchant
// promotion. Effective dates are resolved entirely in SQL via COALESCE on nullable pointer
// parameters, eliminating the read-modify-write race inherent in a load-then-update pattern.
//
// Pass a nil pointer for either date to preserve the existing value. The database-level
// chk_merchant_promotions_dates check constraint enforces end_date > start_date on write,
// so an invalid resulting date combination will produce a clear constraint violation error.
func (m *MerchantPromotionModel) ExtendPromotionDates(ctx context.Context, id uuid.UUID, newStartDate, newEndDate *time.Time) error {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("ExtendMerchantPromotionDates")

	if id == uuid.Nil {
		err := errors.New("invalid merchant promotion ID")
		logger.Error("Validation failed", "error", err)
		return err
	}

	if newStartDate == nil && newEndDate == nil {
		err := errors.New("at least one of newStartDate or newEndDate must be provided")
		logger.Error("Validation failed", "error", err)
		return err
	}

	// Normalize non-nil values to UTC before passing to SQL.
	var startArg, endArg *time.Time
	if newStartDate != nil {
		v := newStartDate.UTC()
		startArg = &v
	}
	if newEndDate != nil {
		v := newEndDate.UTC()
		endArg = &v
	}

	// COALESCE keeps the existing column value when the caller passes NULL.
	// The chk_merchant_promotions_dates constraint enforces end_date > start_date atomically.
	updateQuery := `
		UPDATE merchant_promotions
		SET
			start_date = COALESCE($1, start_date),
			end_date   = COALESCE($2, end_date)
		WHERE id = $3
		  AND deleted_at IS NULL
	`

	commandTag, err := m.DB.Exec(ctx, updateQuery, startArg, endArg, id)
	if err != nil {
		logger.Error("Extend promotion dates failed", "error", err, "merchant_promotion_id", id)
		return fmt.Errorf("extend merchant promotion dates: %w", err)
	}

	if commandTag.RowsAffected() == 0 {
		logger.Warn("Merchant promotion not found", "merchant_promotion_id", id)
		return ErrMerchantPromotionNotFound
	}

	logger.Info("Extend promotion dates successful", "merchant_promotion_id", id)
	return nil
}

// SoftDelete marks an active merchant promotion as deleted by setting deleted_at.
// Subsequent standard read and mutation paths will treat the record as non-existent.
// Promotions that are already soft-deleted return ErrMerchantPromotionNotFound.
func (m *MerchantPromotionModel) SoftDelete(ctx context.Context, id uuid.UUID) error {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("SoftDeleteMerchantPromotion")

	if id == uuid.Nil {
		err := errors.New("invalid merchant promotion ID")
		logger.Error("Validation failed", "error", err)
		return err
	}

	now := timeutil.Now()

	commandTag, err := m.DB.Exec(
		ctx,
		`UPDATE merchant_promotions
		 SET deleted_at = $1
		 WHERE id = $2
		   AND deleted_at IS NULL`,
		now,
		id,
	)
	if err != nil {
		logger.Error("Soft delete merchant promotion failed", "error", err, "merchant_promotion_id", id)
		return fmt.Errorf("soft delete merchant promotion: %w", err)
	}

	if commandTag.RowsAffected() == 0 {
		logger.Warn("Merchant promotion not found", "merchant_promotion_id", id)
		return ErrMerchantPromotionNotFound
	}

	logger.Info("Soft delete merchant promotion successful", "merchant_promotion_id", id)
	return nil
}

// BulkSoftDelete marks multiple active merchant promotions as deleted.
// Returns ErrMerchantPromotionNotFound if none of the provided IDs matched an active record.
func (m *MerchantPromotionModel) BulkSoftDelete(ctx context.Context, ids []uuid.UUID) error {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("BulkSoftDeleteMerchantPromotions")

	ids = uniqueUUIDs(ids)
	if len(ids) == 0 {
		err := errors.New("no merchant promotion IDs provided for bulk soft delete")
		logger.Error("Validation failed", "error", err)
		return err
	}

	now := timeutil.Now()

	commandTag, err := m.DB.Exec(
		ctx,
		`UPDATE merchant_promotions
		 SET deleted_at = $1
		 WHERE id = ANY($2::uuid[])
		   AND deleted_at IS NULL`,
		now,
		ids,
	)
	if err != nil {
		logger.Error("Bulk soft delete merchant promotions failed", "error", err)
		return fmt.Errorf("bulk soft delete merchant promotions: %w", err)
	}

	if commandTag.RowsAffected() == 0 {
		logger.Warn("Bulk soft delete matched no active merchant promotions", "requested_count", len(ids))
		return ErrMerchantPromotionNotFound
	}

	logger.Info("Bulk soft delete merchant promotions successful", "deleted_count", commandTag.RowsAffected())
	return nil
}

// Delete permanently removes a merchant promotion row.
//
// This is the physical purge path. It must remain a true hard delete and must
// not become an alias for SoftDelete.
//
// Because merchant_promotion_offers is a normalized child table of
// merchant_promotions, the schema is expected to own referential cleanup for
// dependent rows. The foreign key on merchant_promotion_offers is configured
// with ON DELETE CASCADE, so join rows are removed automatically by PostgreSQL
// as part of this delete.
func (m *MerchantPromotionModel) Delete(ctx context.Context, id uuid.UUID) error {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("DeleteMerchantPromotion")

	if id == uuid.Nil {
		err := errors.New("invalid merchant promotion ID")
		logger.Error("Validation failed", "error", err)
		return err
	}

	var deletedID uuid.UUID
	err := m.DB.QueryRow(
		ctx,
		`DELETE FROM merchant_promotions
		  WHERE id = $1
		  RETURNING id`,
		id,
	).Scan(&deletedID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			logger.Warn("Merchant promotion not found", "merchant_promotion_id", id)
			return ErrMerchantPromotionNotFound
		}
		logger.Error("Delete merchant promotion failed", "error", err, "merchant_promotion_id", id)
		return fmt.Errorf("delete merchant promotion: %w", err)
	}

	logger.Info("Delete merchant promotion successful", "merchant_promotion_id", deletedID)
	return nil
}

// BulkDelete permanently removes multiple merchant promotions.
//
// This is the bulk physical purge path. It must remain a true hard delete and
// must not delegate to BulkSoftDelete.
//
// The foreign key on merchant_promotion_offers is configured with ON DELETE CASCADE,
// so join rows are removed automatically by PostgreSQL as part of this delete.
// Returns ErrMerchantPromotionNotFound if none of the provided IDs matched any row.
func (m *MerchantPromotionModel) BulkDelete(ctx context.Context, ids []uuid.UUID) error {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("BulkDeleteMerchantPromotions")

	ids = uniqueUUIDs(ids)
	if len(ids) == 0 {
		err := errors.New("no merchant promotion IDs provided for bulk delete")
		logger.Error("Validation failed", "error", err)
		return err
	}

	commandTag, err := m.DB.Exec(
		ctx,
		`DELETE FROM merchant_promotions
		  WHERE id = ANY($1::uuid[])`,
		ids,
	)
	if err != nil {
		logger.Error("Bulk delete merchant promotions failed", "error", err)
		return fmt.Errorf("bulk delete merchant promotions: %w", err)
	}

	if commandTag.RowsAffected() == 0 {
		logger.Warn("Bulk delete matched no merchant promotions", "requested_count", len(ids))
		return ErrMerchantPromotionNotFound
	}

	logger.Info("Bulk delete merchant promotions successful", "deleted_count", commandTag.RowsAffected())
	return nil
}

// IsMerchantUnderPromotion checks whether the supplied merchant currently has any active,
// non-deleted promotion.
func (m *MerchantPromotionModel) IsMerchantUnderPromotion(ctx context.Context, merchantID uuid.UUID) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("IsMerchantUnderPromotion")

	if merchantID == uuid.Nil {
		err := errors.New("invalid merchant ID")
		logger.Error("Validation failed", "error", err)
		return false, err
	}

	now := timeutil.Now()

	query := `
		SELECT EXISTS (
			SELECT 1
			FROM merchant_promotions
			WHERE merchant_id = $1
			  AND deleted_at IS NULL
			  AND start_date <= $2
			  AND end_date   >= $2
		)
	`

	var exists bool
	if err := m.DB.QueryRow(ctx, query, merchantID, now).Scan(&exists); err != nil {
		logger.Error("Check merchant under promotion failed", "error", err, "merchant_id", merchantID)
		return false, fmt.Errorf("check merchant under promotion: %w", err)
	}

	logger.Info("Check merchant under promotion successful", "merchant_id", merchantID, "is_under_promotion", exists)
	return exists, nil
}

// GetStorewidePromotion retrieves the active storewide promotion for a given merchant, if any.
//
// This method intentionally returns (nil, nil) when no active storewide promotion exists.
// This is not an error condition — the absence of a storewide promotion is a valid state
// and callers must handle the nil return explicitly. This differs from GetByID, which returns
// ErrMerchantPromotionNotFound for missing records, because the storewide query is a
// conditional lookup against a business state rather than a direct ID resolution.
func (m *MerchantPromotionModel) GetStorewidePromotion(ctx context.Context, merchantID uuid.UUID) (*MerchantPromotion, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("GetStorewidePromotion")

	if merchantID == uuid.Nil {
		err := errors.New("invalid merchant ID")
		logger.Error("Validation failed", "error", err)
		return nil, err
	}

	now := timeutil.Now()

	query := `
		SELECT ` + merchantPromotionSelectColumns + `
		` + merchantPromotionSelectBase + `
		WHERE mp.merchant_id = $1
		  AND mp.deleted_at IS NULL
		  AND mp.storewide   = TRUE
		  AND mp.start_date <= $2
		  AND mp.end_date   >= $2
		` + merchantPromotionGroupBy + `
		ORDER BY mp.start_date DESC, mp.created_at DESC
		LIMIT 1
	`

	var merchantPromotion MerchantPromotion

	err := scanMerchantPromotion(
		m.DB.QueryRow(ctx, query, merchantID, now).Scan,
		&merchantPromotion,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// No active storewide promotion is a valid business state, not an error.
			logger.Info("No active storewide promotion found", "merchant_id", merchantID)
			return nil, nil
		}
		logger.Error("Get storewide promotion failed", "error", err, "merchant_id", merchantID)
		return nil, fmt.Errorf("get storewide promotion: %w", err)
	}

	logger.Info("Get storewide promotion successful", "merchant_id", merchantID, "merchant_promotion_id", merchantPromotion.ID)
	return &merchantPromotion, nil
}

// validateOffersBelongToMerchant verifies that all supplied offers exist, are not soft-deleted,
// and belong to the supplied merchant.
func (m *MerchantPromotionModel) validateOffersBelongToMerchant(ctx context.Context, tx pgx.Tx, merchantID uuid.UUID, offerIDs []uuid.UUID) error {
	if len(offerIDs) == 0 {
		return errors.New("no offer IDs supplied for validation")
	}

	var matchedCount int

	query := `
		SELECT COUNT(DISTINCT o.id)
		FROM offers o
		WHERE o.merchant_id = $1
		  AND o.deleted_at  IS NULL
		  AND o.id          = ANY($2::uuid[])
	`

	if err := tx.QueryRow(ctx, query, merchantID, offerIDs).Scan(&matchedCount); err != nil {
		return fmt.Errorf("validate targeted offers: %w", err)
	}

	if matchedCount != len(offerIDs) {
		return errors.New("one or more targeted offers do not belong to the specified merchant or do not exist")
	}

	return nil
}

// insertMerchantPromotionOffers inserts normalized promotion-offer links.
// ON CONFLICT DO NOTHING keeps the operation idempotent under retry.
func (m *MerchantPromotionModel) insertMerchantPromotionOffers(ctx context.Context, tx pgx.Tx, merchantPromotionID uuid.UUID, offerIDs []uuid.UUID) error {
	if len(offerIDs) == 0 {
		return nil
	}

	query := `
		INSERT INTO merchant_promotion_offers (merchant_promotion_id, offer_id)
		SELECT $1, UNNEST($2::uuid[])
		ON CONFLICT (merchant_promotion_id, offer_id) DO NOTHING
	`

	if _, err := tx.Exec(ctx, query, merchantPromotionID, offerIDs); err != nil {
		return fmt.Errorf("insert merchant promotion offers: %w", err)
	}

	return nil
}

// replaceMerchantPromotionOffers replaces the target offer links for a promotion atomically.
// For storewide promotions, all existing links are cleared. For non-storewide promotions,
// links are replaced after validating the new set belongs to the correct merchant.
//
// This method is intentionally unconditional: it must also handle the non-storewide →
// storewide transition by clearing join-table rows inside the active transaction.
func (m *MerchantPromotionModel) replaceMerchantPromotionOffers(ctx context.Context, tx pgx.Tx, merchantPromotion *MerchantPromotion) error {
	if merchantPromotion.StoreWide {
		if _, err := tx.Exec(ctx, `DELETE FROM merchant_promotion_offers WHERE merchant_promotion_id = $1`, merchantPromotion.ID); err != nil {
			return fmt.Errorf("clear merchant promotion offers for storewide promotion: %w", err)
		}
		return nil
	}

	if err := m.validateOffersBelongToMerchant(ctx, tx, merchantPromotion.MerchantID, merchantPromotion.TargetOfferIDs); err != nil {
		return err
	}

	if _, err := tx.Exec(ctx, `DELETE FROM merchant_promotion_offers WHERE merchant_promotion_id = $1`, merchantPromotion.ID); err != nil {
		return fmt.Errorf("delete existing merchant promotion offers: %w", err)
	}

	return m.insertMerchantPromotionOffers(ctx, tx, merchantPromotion.ID, merchantPromotion.TargetOfferIDs)
}

// listMerchantPromotions executes a list query and scans normalized merchant-promotion rows.
func (m *MerchantPromotionModel) listMerchantPromotions(ctx context.Context, query string, args ...any) ([]*MerchantPromotion, error) {
	rows, err := m.DB.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query merchant promotions: %w", err)
	}
	defer rows.Close()

	promotions := make([]*MerchantPromotion, 0)

	for rows.Next() {
		var merchantPromotion MerchantPromotion

		if err := scanMerchantPromotion(rows.Scan, &merchantPromotion); err != nil {
			return nil, fmt.Errorf("scan merchant promotion row: %w", err)
		}

		promotions = append(promotions, &merchantPromotion)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate merchant promotion rows: %w", err)
	}

	return promotions, nil
}

// scanMerchantPromotion centralizes row-to-struct scanning so every SELECT path stays aligned
// to the single canonical column order defined in merchantPromotionSelectColumns.
func scanMerchantPromotion(scan func(dest ...any) error, merchantPromotion *MerchantPromotion) error {
	return scan(
		&merchantPromotion.ID,
		&merchantPromotion.MerchantID,
		&merchantPromotion.PromotionID,
		&merchantPromotion.StoreWide,
		&merchantPromotion.TargetOfferIDs,
		&merchantPromotion.StartDate,
		&merchantPromotion.EndDate,
		&merchantPromotion.DeletedAt,
		&merchantPromotion.CreatedAt,
		&merchantPromotion.UpdatedAt,
	)
}

// validateMerchantPromotion enforces business rules for merchant promotion writes.
// Callers are responsible for UTC normalization of date fields before invoking this function.
func validateMerchantPromotion(merchantPromotion *MerchantPromotion) error {
	if merchantPromotion.MerchantID == uuid.Nil {
		return errors.New("merchant ID is required")
	}

	if merchantPromotion.PromotionID == uuid.Nil {
		return errors.New("promotion ID is required")
	}

	if merchantPromotion.StartDate.IsZero() {
		return errors.New("start date is required")
	}

	if merchantPromotion.EndDate.IsZero() {
		return errors.New("end date is required")
	}

	if !merchantPromotion.EndDate.After(merchantPromotion.StartDate) {
		return errors.New("end date must be after start date")
	}

	if merchantPromotion.StoreWide && len(merchantPromotion.TargetOfferIDs) > 0 {
		return errors.New("storewide promotions must not include target offer IDs")
	}

	if !merchantPromotion.StoreWide && len(merchantPromotion.TargetOfferIDs) == 0 {
		return errors.New("non-storewide promotions must include at least one target offer ID")
	}

	return nil
}

// uniqueUUIDs removes duplicates while preserving stable order and dropping uuid.Nil values.
// NOTE: This helper must not be duplicated in other files within the data package.
// If other models require this function, extract it to a shared helpers.go.
func uniqueUUIDs(ids []uuid.UUID) []uuid.UUID {
	if len(ids) == 0 {
		return nil
	}

	seen := make(map[uuid.UUID]struct{}, len(ids))
	result := make([]uuid.UUID, 0, len(ids))

	for _, id := range ids {
		if id == uuid.Nil {
			continue
		}
		if _, exists := seen[id]; exists {
			continue
		}
		seen[id] = struct{}{}
		result = append(result, id)
	}

	return result
}