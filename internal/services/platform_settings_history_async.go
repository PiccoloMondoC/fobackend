// Package services provides asynchronous completion-channel wrappers for
// internal platform-setting value-history reads.
//
// sdworkspace/sdbackend/internal/services/platform_settings_history_async.go
//
// GTM:
//
//	Layer: 2.1 Database / Governance Foundation
//	Release Class: SPINE
//	Reason:
//	  platform_setting_history async support provides controlled,
//	  cancellation-aware completion channels around internal immutable-history
//	  read methods. It supports non-readiness-critical orchestration without
//	  bypassing service validation or introducing background mutation behavior.
//
//	  Platform-setting history has no canonical startup defaults. History rows
//	  arise only from genuine platform-setting value transitions performed by
//	  the canonical mutation transaction.
//
// SPINE Rule:
//
//	Keep compiling.
//	Keep production-ready.
//	Preserve cancellation-aware async result reporting.
//	Preserve one-result buffered completion channels.
//	Preserve exactly-once channel closure.
//	Preserve invocation of canonical internal service methods.
//	Preserve immutable, read-only history behavior.
//	Preserve setting-value opacity.
//	Do not bypass service validation.
//	Do not introduce recurring polling or background mutation loops.
//	Do not introduce startup history seeding.
//	Block deployment if this file breaks async history reads, cancellation,
//	channel completion, or service-layer orchestration.
package services

import (
	"context"

	"github.com/PiccoloMondoC/focodebase/fobackend/internal/data"

	"github.com/google/uuid"
)

// PlatformSettingHistoryAsyncResult carries the result of an asynchronous
// single-record platform-setting history read.
//
// History is nil when the record was not found or when Err is non-nil.
// Callers must inspect Err before interpreting a nil History value.
type PlatformSettingHistoryAsyncResult struct {
	History *data.PlatformSettingHistory
	Err     error
}

// PlatformSettingHistoryListAsyncResult carries the result of an asynchronous
// platform-setting history list read.
//
// History is a non-nil, possibly empty slice when Err is nil.
type PlatformSettingHistoryListAsyncResult struct {
	History []*data.PlatformSettingHistory
	Err     error
}

// GetPlatformSettingHistoryByIDAsync retrieves one immutable platform-setting
// history record asynchronously.
//
// The returned buffered channel publishes exactly one result and is then
// closed.
func GetPlatformSettingHistoryByIDAsync(
	ctx context.Context,
	s *Service,
	historyID uuid.UUID,
) <-chan PlatformSettingHistoryAsyncResult {
	ch := make(chan PlatformSettingHistoryAsyncResult, 1)

	go func() {
		defer close(ch)

		history, err := s.GetPlatformSettingHistoryByIDInternal(
			ctx,
			historyID,
		)

		ch <- PlatformSettingHistoryAsyncResult{
			History: history,
			Err:     err,
		}
	}()

	return ch
}

// ListPlatformSettingHistoryByPlatformSettingIDAsync retrieves a bounded,
// newest-first page of immutable history records for one platform setting
// asynchronously.
//
// The returned buffered channel publishes exactly one result and is then
// closed.
func ListPlatformSettingHistoryByPlatformSettingIDAsync(
	ctx context.Context,
	s *Service,
	platformSettingID uuid.UUID,
	limit int,
	offset int,
) <-chan PlatformSettingHistoryListAsyncResult {
	ch := make(chan PlatformSettingHistoryListAsyncResult, 1)

	go func() {
		defer close(ch)

		history, err :=
			s.ListPlatformSettingHistoryByPlatformSettingIDInternal(
				ctx,
				platformSettingID,
				limit,
				offset,
			)

		ch <- PlatformSettingHistoryListAsyncResult{
			History: history,
			Err:     err,
		}
	}()

	return ch
}

// ListPlatformSettingHistoryBySettingKeyAsync retrieves a bounded,
// newest-first page of immutable history records for one canonical
// platform-setting key asynchronously.
//
// The returned buffered channel publishes exactly one result and is then
// closed.
func ListPlatformSettingHistoryBySettingKeyAsync(
	ctx context.Context,
	s *Service,
	settingKey string,
	limit int,
	offset int,
) <-chan PlatformSettingHistoryListAsyncResult {
	ch := make(chan PlatformSettingHistoryListAsyncResult, 1)

	go func() {
		defer close(ch)

		history, err :=
			s.ListPlatformSettingHistoryBySettingKeyInternal(
				ctx,
				settingKey,
				limit,
				offset,
			)

		ch <- PlatformSettingHistoryListAsyncResult{
			History: history,
			Err:     err,
		}
	}()

	return ch
}
