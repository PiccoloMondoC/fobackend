// Package data provides canonical persistence models and database access for
// SagrentiDeals.
//
// sdworkspace/sdbackend/internal/data/dashboard_templates.go
//
// GTM:
//
//	Layer: 2.3 Consumer Domain / Dashboard Personalization
//	Release Class: DEFERRED
//	Reason:
//	  Dashboard templates and user-template assignments are valid future
//	  personalization infrastructure, but they are not required for the
//	  canonical one-dashboard-per-user workflow in the initial release.
//
// DEFERRED Rule:
//
//	Keep compiling.
//	Keep safe.
//	Preserve the dashboard_templates and user_dashboard_templates schemas for
//	future implementation.
//	Preserve the distinction between a reusable template and a user's canonical
//	dashboard.
//	Do not register template models or activate assignment workflows.
//	Do not add template creation, mutation, deletion, listing, or assignment
//	behavior.
//	Do not expose dashboard-template workflows in the v1 API.
//	Do not block deployment unless this file breaks the build.
package data