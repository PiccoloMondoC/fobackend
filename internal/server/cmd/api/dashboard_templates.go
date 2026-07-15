// Package main provides HTTP handlers for the SagrentiDeals API.
//
// sdworkspace/sdbackend/internal/server/cmd/api/dashboard_templates.go
//
// GTM:
//
//	Layer: 2.3 Consumer Domain / Dashboard Personalization
//	Release Class: DEFERRED
//	Reason:
//	  Dashboard-template administration and assignment are future
//	  personalization workflows. They are not required for the initial
//	  authenticated dashboard experience.
//
// DEFERRED Rule:
//
//	Keep compiling.
//	Keep safe.
//	Do not expose dashboard-template routes in the v1 API.
//	Do not preserve handlers whose data-layer contracts have been retired.
//	Do not add placeholder business behavior or synthetic responses.
//	Do not block deployment unless this file breaks the build.
package main