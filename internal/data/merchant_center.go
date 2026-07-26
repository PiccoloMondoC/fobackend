// Package data provides models and database access methods for Merchant Center and related entities.
//
// sdworkspace/sdbackend/internal/data/merchant_center.go
//
// GTM:
//
//	Layer: 2.4 Merchant / Affiliate Domain
//	Release Class: DEFERRED
//	Reason:
//	  Merchant Center is valid future merchant self-service infrastructure, but
//	  it is not required for the initial Platform release spine. The v1
//	  spine requires merchant identity, merchant type classification,
//	  affiliate-program relationships, canonical offers, publication governance,
//	  click tracking, price history, favorites/stash, and merchant follows
//	  before expanding into merchant self-service dashboards, onboarding
//	  workspaces, campaign management, storefront controls, analytics, and
//	  operational console workflows.
//
// DEFERRED Rule:
//
//	Keep compiling.
//	Keep safe.
//	Preserve merchant-scoped Merchant Center ownership semantics.
//	Preserve separation from internal/admin governance surfaces.
//	Preserve merchant self-service boundary definitions.
//	Preserve DB-owned lifecycle timestamp behavior where persisted.
//	Do not add new features.
//	Do not route into v1 UI/API expansion.
//	Do not block deployment on this file unless it breaks the build.
package data
