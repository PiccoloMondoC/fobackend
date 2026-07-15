// Package data provides the shared scan interface for data-layer row hydration.
//
// sdworkspace/sdbackend/internal/data/scan.go
//
// GTM:
//   Layer: 2.1 Database / Governance Foundation
//   Release Class: SPINE
//   Reason:
//     The shared scan contract is release-critical data-layer infrastructure. It
//     centralizes row hydration behavior used across model files and helps
//     prevent scan drift between SQL column lists and Go destination ordering.
//
// SPINE Rule:
//   Keep compiling.
//   Keep production-ready.
//   Preserve the shared scanner interface.
//   Preserve canonical row-hydration behavior.
//   Preserve compatibility with pgx.Row, pgx.Rows, and pgx.CollectableRow
//   scan paths, including collector functions used with pgx.CollectRows.
//   Block deployment if this file breaks build, model scanning,
//   row hydration, or data-layer persistence integrity.
package data

// scannableRow is the shared package-level scanner contract used by row
// hydration helpers across the data package.
//
// This interface is intentionally narrow. It captures only the Scan behavior
// shared by pgx.Row, pgx.Rows, and pgx.CollectableRow. That lets the same
// hydration helper support single-row lookups and collector-function paths used
// with pgx.CollectRows without duplicating scan destination ordering.
//
// Keep this interface centralized. Do not redefine local scanner interfaces
// inside individual data files, and do not expand this contract with logging,
// context, database access, lifecycle behavior, model-specific behavior,
// error translation, or pgx.ErrNoRows interception.
type scannableRow interface {
	Scan(dest ...any) error
}