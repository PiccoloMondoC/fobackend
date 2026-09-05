// Package services contains trusted internal service composition,
// automation support, moderation helpers, and shared internal workflow logic.
//
// sdworkspace/sdbackend/internal/services/offer_description.go
//
// GTM:
//
//	Layer: 2.6 Internal Services / Automation Foundation
//	Release Class: SPINE
//	Reason:
//	  Generates deterministic internal offer-description text from validated
//	  offer metadata without weakening canonical decimal handling or allowing
//	  merchant-controlled Markdown structure.
//
//	  This file supports present-commerce offer description workflows only. It
//	  does not define Future Offering messaging or lifecycle policy.
//
// SPINE Rule:
//
//	Keep compiling.
//	Keep production-ready.
//	Require complete validated offer metadata before generating text.
//	Preserve exact decimal parsing and never convert monetary values to float64.
//	Escape merchant-controlled Markdown characters before interpolation.
//	Never embed platform branding, unsupported claims, affiliate persuasion,
//	or Future Offering purchase language in generated copy.
//	Never log the full generated description.
//	Do not overwrite authoritative merchant or editorial content implicitly.
package services

import (
	"context"
	"fmt"
	"strings"

	"github.com/PiccoloMondoC/focodebase/fobackend/internal/data"
)

var markdownEscaper = strings.NewReplacer(
	`\`, `\\`,
	`*`, `\*`,
	`_`, `\_`,
	"`", "\\`",
	`[`, `\[`,
	`]`, `\]`,
	`(`, `\(`,
	`)`, `\)`,
	`#`, `\#`,
	`>`, `\>`,
)

// generateTextualDescriptionForOffer creates a deterministic, escaped
// description for a present-commerce offer.
func (s *Service) generateTextualDescriptionForOffer(
	ctx context.Context,
	offer *data.Offer,
) (string, error) {
	if ctx == nil {
		return "", ErrNilContext
	}
	if err := s.validate(); err != nil {
		return "", err
	}

	logger := s.Logger.
		GetLoggerWithContextFromContext(ctx).
		WithFunctionName("generateTextualDescriptionForOffer")

	if offer == nil {
		logger.Error("Nil offer received")
		return "", ErrNilOffer
	}

	title := strings.TrimSpace(offer.Title)
	currency := strings.ToUpper(strings.TrimSpace(offer.Currency))

	price, err := parseOfferPrice(offer.Price)
	if err != nil {
		logger.Error("Invalid offer price", "offer_id", offer.ID, "error", err)
		return "", fmt.Errorf("offer price: %w", err)
	}
	if title == "" || currency == "" {
		logger.Error("Missing critical offer metadata", "offer_id", offer.ID)
		return "", ErrOfferMetadataIncomplete
	}

	discount, err := parseOfferDiscountPercent(offer.DiscountPercent)
	if err != nil {
		logger.Error(
			"Invalid offer discount percent",
			"offer_id", offer.ID,
			"error", err,
		)
		return "", fmt.Errorf("offer discount percent: %w", err)
	}

	var builder strings.Builder
	fmt.Fprintf(
		&builder,
		"Offer: **%s** — **%s %s**",
		markdownEscaper.Replace(title),
		price.FloatString(2),
		markdownEscaper.Replace(currency),
	)

	if discount != nil && discount.Sign() > 0 {
		fmt.Fprintf(
			&builder,
			" (**%s%% discount**).",
			discount.FloatString(2),
		)
	} else {
		builder.WriteString(".")
	}

	if offer.IsEditorialApproved {
		builder.WriteString(" This offer has completed editorial review.")
	}

	if strings.TrimSpace(offer.AffiliateURL) != "" {
		builder.WriteString(" A merchant commerce link is available.")
	}

	logger.Debug("Generated offer description", "offer_id", offer.ID)
	return builder.String(), nil
}
