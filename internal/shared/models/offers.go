// Package models defines lightweight data structures used for service communication,
// event transport, and background job processing. These types intentionally exclude
// database logic and should be safe for use across package boundaries to avoid import cycles.
//
// focodebase/fobackend/internal/shared/models/offer.go
package models

import "github.com/google/uuid"

// OfferLite is a lightweight version safe for event transport.
// Do NOT put DB logic here.
type OfferLite struct {
	ID                  uuid.UUID
	Title               string
	MerchantID          uuid.UUID
	IsEditorialApproved bool
	StatusID            uuid.UUID
	CreatedBy           uuid.UUID
}


// OfferPayload represents a complete, transport-safe version of a curated offer,
// intended for use in event messages, background processing, and inter-service
// communication. It includes all relevant offer fields needed for insertion,
// validation, and enrichment but excludes any database logic or methods.
//
// This struct should be used in place of data.Offer when crossing package boundaries
// (e.g., events → services) to avoid import cycles and enforce separation of concerns.
type OfferPayload struct {
	ID                  uuid.UUID
	Title               string
	Description         *string
	ImageURL            *string
	AffiliateURL        string
	Price               float64
	Currency            string
	DiscountPercent     *float64
	ProductID           *uuid.UUID
	MerchantID          uuid.UUID
	CategoryID          *uuid.UUID
	AvgRating           float64
	IsEditorialApproved bool
	StatusID            uuid.UUID
	CreatedBy           uuid.UUID
}
