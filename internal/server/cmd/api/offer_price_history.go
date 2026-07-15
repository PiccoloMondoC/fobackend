// Package main provides the Sagrenti HTTP API application.
//
// sdworkspace/sdbackend/internal/server/cmd/api/offer_price_history.go
//
// GTM:
//
//	Layer: 3.1 HTTP API / Offer Domain
//	Release Class: DEFERRED
//	Reason:
//	  Offer price-history HTTP endpoints are not required for the initial
//	  Future Offering Platform release spine. The production-grade data model
//	  remains available for future offer-price evidence, price-drop discovery,
//	  subscription behavior, anomaly reporting, and pricing intelligence, but
//	  no public or privileged HTTP contract is activated in this release.
//
// DEFERRED Rule:
//
//	Keep compiling.
//	Do not register offer price-history routes.
//	Do not expose deferred price-history, price-drop subscription, anomaly,
//	trend, manual-adjustment, or significant-price-drop handlers.
//	Do not recreate removed DealPriceHistory types, models, permissions,
//	context getters, route contracts, or audit actions.
//	Do not mutate immutable historical price records through an update handler.
//	Preserve NUMERIC-safe decimal-string behavior when this capability is activated.
//	Preserve database-owned IDs and lifecycle timestamps.
//	Preserve anomaly routing through offer_flags.
//	Preserve price-drop notification delivery as a service-layer concern.
//	Activate only through a complete vertical slice covering data, services,
//	HTTP handlers, trusted identifier resolution, permissions, audit metadata,
//	routes, tests, and release review.
package main