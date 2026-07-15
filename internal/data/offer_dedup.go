// Package data provides models and database access methods for offers and other entities.
//
// File: sdworkspace/sdbackend/internal/data/offer_dedup.go
//
// GTM:
//   Layer: 2.5 Catalog / Offer Domain
//   Release Class: SPINE
//   Reason:
//     Offer deduplication is release-critical catalog integrity infrastructure.
//     It protects the canonical offers table from duplicate merchant offer
//     ingestion, preserves offer quality, and supports clean public offer
//     discovery before broader automation and provider ingestion scale up.
//
// SPINE Rule:
//   Keep compiling.
//   Keep production-ready.
//   Preserve OfferModel ownership.
//   Preserve canonical offers table dependency.
//   Preserve strict affiliate URL normalization.
//   Preserve bounded duplicate lookback behavior.
//   Block deployment if this file breaks build, offer persistence,
//   duplicate detection, affiliate URL integrity, or catalog quality.
package data

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
)

// duplicateOfferLookbackDays defines the bounded recency window used by the
// interim application-level deduplication strategy.
//
// Design note:
// This remains an application-side comparison for now. The long-term scalable
// design is to persist a canonical normalized affiliate URL and index it.
// That schema decision is deferred. For the current pre-launch phase, bounding
// the candidate set by recent creation time is an acceptable interim policy.
const duplicateOfferLookbackDays = 30

// normalizeOfferURL standardizes affiliate URLs for deduplication checks.
//
// This helper is intentionally strict. It does not silently degrade on parse
// failure because deduplication must fail clearly rather than comparing
// malformed values as if they were canonical.
func normalizeOfferURL(rawURL string) (string, error) {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return "", errors.New("affiliate_url is required")
	}

	parsed, err := url.Parse(rawURL)
	if err != nil {
		return "", fmt.Errorf("parse affiliate_url: %w", err)
	}

	// Require a minimally valid HTTP(S) URL for canonical comparison.
	if parsed.Scheme == "" || parsed.Host == "" {
		return "", errors.New("affiliate_url must include scheme and host")
	}

	parsed.Scheme = strings.ToLower(parsed.Scheme)
	parsed.Host = strings.ToLower(parsed.Host)
	parsed.Path = strings.TrimRight(parsed.Path, "/")

	q := parsed.Query()
	for _, key := range []string{
		"utm_source",
		"utm_medium",
		"utm_campaign",
		"utm_term",
		"utm_content",
		"ref",
		"fbclid",
		"gclid",
		"mc_cid",
		"mc_eid",
		"aff_id",
		"aff_sub",
	} {
		q.Del(key)
	}
	parsed.RawQuery = q.Encode()

	normalized := strings.TrimSpace(parsed.String())
	if normalized == "" {
		return "", errors.New("normalized affiliate_url is empty")
	}

	return normalized, nil
}

// IsDuplicate returns true when a similar non-deleted offer already exists for
// the same merchant within the bounded lookback window.
//
// Update-flow safety:
// The current offer ID is excluded from the candidate set. On create, passing
// the zero UUID is a no-op because no persisted row uses the zero UUID.
func (m *OfferModel) IsDuplicate(ctx context.Context, offer *Offer) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	log := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("IsDuplicateOffer")

	if offer == nil {
		err := errors.New("offer payload is required")
		log.Error("validation failed", err)
		return false, err
	}

	if offer.MerchantID == uuid.Nil {
		err := errors.New("merchant_id is required")
		log.Error("validation failed", err)
		return false, err
	}

	if strings.TrimSpace(offer.AffiliateURL) == "" {
		err := errors.New("affiliate_url is required")
		log.Error("validation failed", err)
		return false, err
	}

	normalizedURL, err := normalizeOfferURL(offer.AffiliateURL)
	if err != nil {
		err = fmt.Errorf("normalize candidate affiliate_url: %w", err)
		log.Error("validation failed", err, "merchant_id", offer.MerchantID, "offer_id", offer.ID)
		return false, err
	}

	if normalizedURL == "" {
		err := errors.New("normalized affiliate_url is empty")
		log.Error("validation failed", err, "merchant_id", offer.MerchantID, "offer_id", offer.ID)
		return false, err
	}

	cutoff := time.Now().UTC().AddDate(0, 0, -duplicateOfferLookbackDays)

	const q = `
		SELECT affiliate_url
		FROM offers
		WHERE merchant_id = $1
		  AND deleted_at IS NULL
		  AND created_at >= $2
		  AND id != $3
	`

	rows, err := m.DB.Query(ctx, q, offer.MerchantID, cutoff, offer.ID)
	if err != nil {
		err = fmt.Errorf("query duplicate offers: %w", err)
		log.Error("query failed", err, "merchant_id", offer.MerchantID, "offer_id", offer.ID)
		return false, err
	}
	defer rows.Close()

	for rows.Next() {
		var existingURL string
		if err := rows.Scan(&existingURL); err != nil {
			err = fmt.Errorf("scan duplicate offer candidate: %w", err)
			log.Error("row scan failed", err, "merchant_id", offer.MerchantID, "offer_id", offer.ID)
			return false, err
		}

		normalizedExistingURL, err := normalizeOfferURL(existingURL)
		if err != nil {
			err = fmt.Errorf("normalize stored affiliate_url during duplicate check: %w", err)
			log.Error("stored data invalid", err, "merchant_id", offer.MerchantID, "offer_id", offer.ID)
			return false, err
		}

		if normalizedExistingURL == "" {
			err := errors.New("normalized stored affiliate_url is empty")
			log.Error("stored data invalid", err, "merchant_id", offer.MerchantID, "offer_id", offer.ID)
			return false, err
		}

		if normalizedExistingURL == normalizedURL {
			return true, nil
		}
	}

	if err := rows.Err(); err != nil {
		err = fmt.Errorf("iterate duplicate offer candidates: %w", err)
		log.Error("row iteration failed", err, "merchant_id", offer.MerchantID, "offer_id", offer.ID)
		return false, err
	}

	return false, nil
}