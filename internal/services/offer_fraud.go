// Package services contains trusted internal service composition,
// automation support, moderation helpers, and shared internal workflow logic.
//
// sdworkspace/sdbackend/internal/services/internal-services/offer-fraud.go
//
// GTM:
//
//	Layer: 2.6 Internal Services / Automation Foundation
//	Release Class: SPINE
//	Reason:
//	  Provides internal offer-moderation heuristics and structured reason
//	  collection for suspicious present-commerce offers.
//
//	  The established boolean service contract remains available while the
//	  implementation records complete reason information internally.
//
// SPINE Rule:
//
//	Keep compiling.
//	Keep production-ready.
//	Preserve exact decimal handling for discount percentages.
//	Evaluate Unicode title length by rune count, not byte count.
//	Require valid HTTPS merchant commerce URLs where this present-commerce
//	heuristic applies.
//	Never treat malformed input as trustworthy.
//	Never short-circuit in a way that loses additional moderation reasons.
//	Do not apply present-commerce affiliate requirements to Future Offerings
//	without an explicit domain-specific policy boundary.
package services

import (
	"context"
	"math/big"
	"net/url"
	"strings"
	"unicode/utf8"

	"github.com/PiccoloMondoC/sdworkspace/sdbackend/internal/data"
	"github.com/google/uuid"
)

const minOfferTitleRunes = 5

var suspiciousDiscountThresholdPercent = big.NewRat(85, 1)

type offerFraudReason string

const (
	offerFraudInvalidDiscount  offerFraudReason = "invalid_discount"
	offerFraudHighDiscount     offerFraudReason = "high_discount"
	offerFraudInvalidTitle     offerFraudReason = "invalid_title"
	offerFraudMissingMerchant  offerFraudReason = "missing_merchant"
	offerFraudInvalidAffiliate offerFraudReason = "invalid_affiliate_url"
)

type offerFraudAssessment struct {
	Suspicious bool
	Reasons    []offerFraudReason
}

func assessOfferFraud(offer data.Offer) offerFraudAssessment {
	assessment := offerFraudAssessment{
		Reasons: make([]offerFraudReason, 0, 4),
	}

	discount, err := parseOfferDiscountPercent(offer.DiscountPercent)
	switch {
	case err != nil:
		assessment.Reasons = append(
			assessment.Reasons,
			offerFraudInvalidDiscount,
		)
	case discount != nil &&
		discount.Cmp(suspiciousDiscountThresholdPercent) > 0:
		assessment.Reasons = append(
			assessment.Reasons,
			offerFraudHighDiscount,
		)
	}

	title := strings.TrimSpace(offer.Title)
	if title == "" || utf8.RuneCountInString(title) < minOfferTitleRunes {
		assessment.Reasons = append(
			assessment.Reasons,
			offerFraudInvalidTitle,
		)
	}

	if offer.MerchantID == uuid.Nil {
		assessment.Reasons = append(
			assessment.Reasons,
			offerFraudMissingMerchant,
		)
	}

	if !isValidHTTPSURL(offer.AffiliateURL) {
		assessment.Reasons = append(
			assessment.Reasons,
			offerFraudInvalidAffiliate,
		)
	}

	assessment.Suspicious = len(assessment.Reasons) > 0
	return assessment
}

func isValidHTTPSURL(raw string) bool {
	value := strings.TrimSpace(raw)
	if value == "" {
		return false
	}

	parsed, err := url.ParseRequestURI(value)
	if err != nil {
		return false
	}

	return parsed.Scheme == "https" && parsed.Host != ""
}

// isSuspiciousOffer preserves the established caller contract while delegating
// to a structured, complete assessment.
func (s *Service) isSuspiciousOffer(
	ctx context.Context,
	offer data.Offer,
) bool {
	assessment := assessOfferFraud(offer)

	if ctx != nil && s != nil && s.Logger != nil && assessment.Suspicious {
		logger := s.Logger.
			GetLoggerWithContextFromContext(ctx).
			WithFunctionName("isSuspiciousOffer")

		logger.Warn(
			"Offer requires moderation review",
			"offer_id", offer.ID,
			"reasons", assessment.Reasons,
		)
	}

	return assessment.Suspicious
}
