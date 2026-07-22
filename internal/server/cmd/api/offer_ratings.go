// Package main provides HTTP handlers for the Platform API.
//
// sdworkspace/sdbackend/internal/server/cmd/api/offer_ratings.go
//
// GTM:
//
//	Layer: 2.5 Catalog / Offer Domain
//	Release Class: DEFERRED
//	Reason:
//	  Offer ratings and written reviews are valid future engagement and
//	  trust-signal infrastructure, but they are not required for the initial
//	  Platform release spine. The initial release prioritizes canonical
//	  offers, publication governance, commerce routing, attribution,
//	  price history, merchant foundations, and the Future Offering Platform
//	  supported by its Monetization Layer.
//
// DEFERRED Rule:
//
//	Keep compiling.
//	Keep safe.
//	Preserve the canonical data-layer model and its one-active-rating-per-user-
//	per-offer invariant.
//	Preserve soft-delete lifecycle and moderation semantics in the data layer.
//	Preserve database-owned lifecycle timestamps.
//	Do not recreate handler-local persistence contracts.
//	Do not expose offer-rating creation, update, moderation, deletion,
//	aggregation, or analytics workflows in the v1 API.
//	Do not retain unused handler stubs or legacy route compatibility.
//	Do not add new API features.
//	Do not block deployment on this file unless it breaks the build.
package main