// Package data provides models and database access methods for offers and other entities.
//
// File: sdworkspace/sdbackend/internal/data/offer_discovery.go
//
// GTM:
//
//	Layer: 2.5 Catalog / Offer Domain
//	Release Class: SPINE
//	Reason:
//	  Offer discovery is release-critical public catalog infrastructure. It
//	  powers recent, top-rated, category, popular, and trending offer reads
//	  while enforcing the shared public-offer visibility contract over the
//	  canonical offers table.
//
// SPINE Rule:
//
//	Keep compiling.
//	Keep production-ready.
//	Preserve OfferModel ownership.
//	Preserve canonical offers table dependency.
//	Preserve public visibility filtering.
//	Preserve bounded pagination behavior.
//	Preserve click-based popularity/trending ranking.
//	Block deployment if this file breaks build, public offer discovery,
//	publication visibility, category browsing, or catalog integrity.
package data

import (
	"context"
	"errors"
	"fmt"

	"github.com/PiccoloMondoC/sdworkspace/sdbackend/internal/utils/timeutil"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

const (
	// popularOffersLookbackDays defines the ranking window for site-wide popular
	// public offers. This is a product/discovery policy value, not an incidental
	// SQL literal.
	popularOffersLookbackDays = 30

	// trendingOffersLookbackDays defines the shorter ranking window for
	// category-specific trending public offers.
	trendingOffersLookbackDays = 7
)

// collectOffers consumes a pgx rows result into []*Offer using the package's
// canonical scan helper.
//
// This keeps slice-returning query methods aligned with the package collection
// convention while still using the canonical offer scan contract.
func collectOffers(rows pgx.Rows) ([]*Offer, error) {
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (*Offer, error) {
		var d Offer
		if err := scanOffer(row, &d); err != nil {
			return nil, err
		}
		return &d, nil
	})
}

// GetRecentOffers returns recently published public offers.
//
// Public-read eligibility is enforced via the shared publication contract:
// active, editorially approved, published, non-deleted, and non-expired.
func (m *OfferModel) GetRecentOffers(ctx context.Context, limit, offset int) ([]*Offer, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("GetRecentOffers")

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
		err = fmt.Errorf("get recent offers: %w", err)
		logger.Error("Query execution failed", err, "limit", limit, "offset", offset)
		return nil, err
	}

	offers, err := collectOffers(rows)
	if err != nil {
		err = fmt.Errorf("collect recent offers: %w", err)
		logger.Error("Row collection failed", err, "limit", limit, "offset", offset)
		return nil, err
	}

	logger.Info("Retrieved recent public offers", "count", len(offers), "limit", limit, "offset", offset)
	return offers, nil
}

// GetTopRatedOffers returns top-rated public offers.
//
// Public-read eligibility is enforced via the shared publication contract.
func (m *OfferModel) GetTopRatedOffers(ctx context.Context, limit, offset int) ([]*Offer, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("GetTopRatedOffers")

	limit, offset = defaultPage(limit, offset)

	now := timeutil.Now()
	clause, nextArg := buildPublicOfferVisibilityClause(1)

	q := fmt.Sprintf(`
		SELECT %s
		FROM offers
		WHERE %s
		ORDER BY avg_rating DESC, published_at DESC, created_at DESC
		LIMIT $%d OFFSET $%d
	`, offerSelectColumns, clause, nextArg, nextArg+1)

	rows, err := m.DB.Query(ctx, q, now, limit, offset)
	if err != nil {
		err = fmt.Errorf("get top rated offers: %w", err)
		logger.Error("Query execution failed", err, "limit", limit, "offset", offset)
		return nil, err
	}

	offers, err := collectOffers(rows)
	if err != nil {
		err = fmt.Errorf("collect top rated offers: %w", err)
		logger.Error("Row collection failed", err, "limit", limit, "offset", offset)
		return nil, err
	}

	logger.Info("Retrieved top rated public offers", "count", len(offers), "limit", limit, "offset", offset)
	return offers, nil
}

// GetOffersByCategory returns public offers for a specific category.
//
// Public-read eligibility is enforced via the shared publication contract.
func (m *OfferModel) GetOffersByCategory(ctx context.Context, categoryID uuid.UUID, limit, offset int) ([]*Offer, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("GetOffersByCategory")

	if categoryID == uuid.Nil {
		err := errors.New("category ID is required")
		logger.Error("Validation failed", err)
		return nil, err
	}

	limit, offset = defaultPage(limit, offset)

	now := timeutil.Now()
	clause, nextArg := buildPublicOfferVisibilityClause(2)

	q := fmt.Sprintf(`
		SELECT %s
		FROM offers
		WHERE category_id = $1
		  AND %s
		ORDER BY published_at DESC, created_at DESC
		LIMIT $%d OFFSET $%d
	`, offerSelectColumns, clause, nextArg, nextArg+1)

	rows, err := m.DB.Query(ctx, q, categoryID, now, limit, offset)
	if err != nil {
		err = fmt.Errorf("get offers by category: %w", err)
		logger.Error("Query execution failed", err, "category_id", categoryID, "limit", limit, "offset", offset)
		return nil, err
	}

	offers, err := collectOffers(rows)
	if err != nil {
		err = fmt.Errorf("collect offers by category: %w", err)
		logger.Error("Row collection failed", err, "category_id", categoryID, "limit", limit, "offset", offset)
		return nil, err
	}

	logger.Info("Retrieved public offers by category", "category_id", categoryID, "count", len(offers), "limit", limit, "offset", offset)
	return offers, nil
}

// GetPopularOffers ranks public offers by click activity in the recent
// popularity window.
//
// Ranking uses immutable offer_clicks history; visibility is still governed
// entirely by the offers publication contract.
//
// Query-shape note:
// This uses a lateral aggregate subquery instead of GROUP BY mirroring every
// offer column. That keeps ranking logic maintainable if offerSelectColumns
// evolves later.
func (m *OfferModel) GetPopularOffers(ctx context.Context, limit, offset int) ([]*Offer, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("GetPopularOffers")

	limit, offset = defaultPage(limit, offset)

	now := timeutil.Now()
	clause, nextArg := buildPublicOfferVisibilityClause(1)

	q := fmt.Sprintf(`
		SELECT %s
		FROM offers o
		LEFT JOIN LATERAL (
			SELECT COUNT(*) AS click_count
			FROM offer_clicks oc
			WHERE oc.offer_id = o.id
			  AND oc.clicked_at >= $1 - ($2::int * INTERVAL '1 day')
		) oc_stats ON TRUE
		WHERE %s
		ORDER BY COALESCE(oc_stats.click_count, 0) DESC, o.published_at DESC, o.created_at DESC
		LIMIT $%d OFFSET $%d
	`, offerSelectColumns, clause, nextArg+1, nextArg+2)

	rows, err := m.DB.Query(ctx, q, now, popularOffersLookbackDays, limit, offset)
	if err != nil {
		err = fmt.Errorf("get popular offers: %w", err)
		logger.Error("Query execution failed", err, "lookback_days", popularOffersLookbackDays, "limit", limit, "offset", offset)
		return nil, err
	}

	offers, err := collectOffers(rows)
	if err != nil {
		err = fmt.Errorf("collect popular offers: %w", err)
		logger.Error("Row collection failed", err, "lookback_days", popularOffersLookbackDays, "limit", limit, "offset", offset)
		return nil, err
	}

	logger.Info("Retrieved popular public offers", "count", len(offers), "lookback_days", popularOffersLookbackDays, "limit", limit, "offset", offset)
	return offers, nil
}

// GetTrendingOffersByCategory ranks public offers within a category by recent
// click activity in the shorter trending window.
//
// Query-shape note:
// This also uses a lateral aggregate subquery to avoid coupling query validity
// to a manually mirrored GROUP BY list.
func (m *OfferModel) GetTrendingOffersByCategory(ctx context.Context, categoryID uuid.UUID, limit, offset int) ([]*Offer, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("GetTrendingOffersByCategory")

	if categoryID == uuid.Nil {
		err := errors.New("category ID is required")
		logger.Error("Validation failed", err)
		return nil, err
	}

	limit, offset = defaultPage(limit, offset)

	now := timeutil.Now()
	clause, nextArg := buildPublicOfferVisibilityClause(2)

	q := fmt.Sprintf(`
		SELECT %s
		FROM offers o
		LEFT JOIN LATERAL (
			SELECT COUNT(*) AS click_count
			FROM offer_clicks oc
			WHERE oc.offer_id = o.id
			  AND oc.clicked_at >= $2 - ($3::int * INTERVAL '1 day')
		) oc_stats ON TRUE
		WHERE o.category_id = $1
		  AND %s
		ORDER BY COALESCE(oc_stats.click_count, 0) DESC, o.published_at DESC, o.created_at DESC
		LIMIT $%d OFFSET $%d
	`, offerSelectColumns, clause, nextArg+1, nextArg+2)

	rows, err := m.DB.Query(ctx, q, categoryID, now, trendingOffersLookbackDays, limit, offset)
	if err != nil {
		err = fmt.Errorf("get trending offers by category: %w", err)
		logger.Error("Query execution failed", err, "category_id", categoryID, "lookback_days", trendingOffersLookbackDays, "limit", limit, "offset", offset)
		return nil, err
	}

	offers, err := collectOffers(rows)
	if err != nil {
		err = fmt.Errorf("collect trending offers by category: %w", err)
		logger.Error("Row collection failed", err, "category_id", categoryID, "lookback_days", trendingOffersLookbackDays, "limit", limit, "offset", offset)
		return nil, err
	}

	logger.Info("Retrieved trending public offers by category", "category_id", categoryID, "count", len(offers), "lookback_days", trendingOffersLookbackDays, "limit", limit, "offset", offset)
	return offers, nil
}
