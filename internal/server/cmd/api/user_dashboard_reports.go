// Package main provides HTTP handlers for the Platform API.
//
// sdworkspace/sdbackend/internal/server/cmd/api/user_dashboard_reports.go
//
// GTM:
//
//	Layer: 2.3 Consumer Domain / Internal Analytics
//	Release Class: DEFERRED
//	Reason:
//	  Dashboard report generation and retrieval are internal analytics
//	  workflows. They are not required for the initial authenticated dashboard
//	  experience or the Future Offering Platform release spine.
//
// DEFERRED Rule:
//
//	Keep compiling.
//	Keep safe.
//	Do not expose dashboard-report routes in the v1 API.
//	Do not preserve handlers whose persistence contracts have been retired.
//	Do not treat aggregate reports as dashboard-scoped resources.
//	Do not add placeholder business behavior or synthetic responses.
//	Do not block deployment unless this file breaks the build.
package main