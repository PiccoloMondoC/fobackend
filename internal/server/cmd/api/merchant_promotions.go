// Package main provides HTTP handlers for the Platform API.
//
// sdworkspace/sdbackend/internal/server/cmd/api/merchant_promotions.go
//
// GTM:
//
//	Layer: 2.5 Catalog / Offer Domain
//	Release Class: DEFERRED
//	Reason:
//	  Merchant promotions are valid future merchandising and campaign
//	  infrastructure, but they are not required for the initial Platform
//	  release spine. The initial release prioritizes canonical offers,
//	  publication status, commerce routing, attribution, price history,
//	  merchant foundations, and the Future Offering Platform supported by
//	  its Monetization Layer.
//
// DEFERRED Rule:
//
//	Keep compiling.
//	Keep safe.
//	Preserve merchant-owned promotion semantics in the canonical data layer.
//	Preserve storewide versus targeted-offer distinction.
//	Preserve soft-delete lifecycle behavior.
//	Preserve normalized merchant_promotion_offers join behavior.
//	Do not recreate the removed standalone Promotion model.
//	Do not invent unsupported offer-to-promotion reverse queries.
//	Do not add new API features.
//	Do not route into v1 UI/API expansion.
//	Do not preserve legacy handlers or route compatibility.
//	Do not block deployment on this file unless it breaks the build.
package main