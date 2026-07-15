// Package timeutil provides centralized helpers for working with application
// time values.
//
// sdworkspace/sdbackend/internal/utils/timeutil/timeutil.go
//
// GTM:
//
//	Layer: 2.1 Database / Governance Foundation
//	Release Class: SPINE
//	Reason:
//	  Centralized UTC time generation is release-critical shared infrastructure.
//	  It supports authentication, token expiration, authorization deadlines,
//	  password recovery, account activation, and other application-owned time
//	  calculations required by the initial SagrentiDeals release spine.
//
// SPINE Rule:
//
//	Keep compiling.
//	Keep production-ready.
//	Preserve UTC time generation.
//	Preserve application-owned expiry and deadline calculations.
//	Do not use this package to application-write database-owned created_at,
//	updated_at, occurred_at, deleted_at, revoked_at, or equivalent persisted
//	lifecycle timestamps.
//	Do not add formatting or conversion helpers without an active caller.
//	Block deployment if this file breaks build, authentication timing,
//	token expiration, authorization deadlines, or application time integrity.
package timeutil

import "time"

// Now returns the current application time normalized to UTC.
//
// Use Now for application-owned calculations such as token expiration,
// authorization-code expiration, deadlines, and bounded validity windows.
//
// Persisted lifecycle timestamps governed by database defaults, triggers, or
// RETURNING clauses must remain database-owned and must not be populated with
// this helper.
func Now() time.Time {
	return time.Now().UTC()
}