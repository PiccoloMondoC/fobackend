// Package data provides canonical persistence models and database access for
// SagrentiDeals.
//
// sdworkspace/sdbackend/internal/data/user_dashboard_reports.go
//
// GTM:
//
//	Layer: 2.3 Consumer Domain / Internal Analytics
//	Release Class: DEFERRED
//	Reason:
//	  Dashboard reports are internal aggregate reporting infrastructure. They
//	  summarize dashboard adoption and widget usage but are not required for
//	  authenticated users to create, retrieve, or maintain their canonical
//	  dashboards in the initial release.
//
// DEFERRED Rule:
//
//	Keep compiling.
//	Keep safe.
//	Preserve the user_dashboard_reports schema for future implementation.
//	Do not model reports as children of individual user dashboards.
//	Do not add dashboard_id without an approved reporting requirement and
//	schema migration.
//	Do not register report models or expose report workflows in the v1 API.
//	Do not add report generation, listing, filtering, or export behavior.
//	Do not block deployment unless this file breaks the build.
package data