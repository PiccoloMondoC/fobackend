// Package services provides internal service-layer orchestration for
// privileged reads of immutable platform-setting value history.
//
// sdworkspace/sdbackend/internal/services/platform_settings_history_internal.go
//
// GTM:
//
//	Layer: 2.1 Database / Governance Foundation
//	Release Class: SPINE
//	Reason:
//	  platform_setting_history internal service support is release-critical
//	  configuration-governance infrastructure. It provides bounded internal
//	  orchestration for privileged review of immutable platform-setting value
//	  transitions while preserving configuration-value confidentiality.
//
//	  This service surface is read-only. History insertion belongs exclusively
//	  to the canonical platform-setting mutation transaction. This file does
//	  not create, update, delete, restore, or purge history records.
//
//	  platform_setting_history records value transitions only. It is not a
//	  complete lifecycle-event ledger for activation, deactivation,
//	  restoration, deletion, or purge because those operation types are not
//	  represented by the current schema.
//
// SPINE Rule:
//
//	Keep compiling.
//	Keep production-ready.
//	Preserve privileged internal history-read semantics.
//	Preserve immutable, read-only history behavior.
//	Preserve service and data-layer separation.
//	Preserve bounded, deterministic history reads.
//	Preserve configured service timeout boundaries.
//	Preserve caller cancellation and deadline propagation.
//	Preserve nil, nil behavior for a missing single history record.
//	Preserve non-nil empty slices for successful list reads with no results.
//	Preserve setting-value opacity in logs, traces, metrics, and errors.
//	Never log previous_value or new_value.
//	Do not introduce detached or non-transactional history insertion.
//	Do not claim lifecycle coverage unsupported by the schema.
//	Block deployment if this file breaks build, bounded history review,
//	immutable-history semantics, or configuration-value confidentiality.
package services

import (
	"context"
	"fmt"

	"github.com/PiccoloMondoC/sdworkspace/sdbackend/internal/data"

	"github.com/google/uuid"
)

// GetPlatformSettingHistoryByIDInternal retrieves one immutable
// platform-setting history record by its canonical ID.
//
// A missing record returns nil, nil in accordance with the canonical
// data-layer contract.
func (s *Service) GetPlatformSettingHistoryByIDInternal(
	ctx context.Context,
	historyID uuid.UUID,
) (*data.PlatformSettingHistory, error) {
	if err := validatePlatformSettingService(s); err != nil {
		return nil, err
	}

	ctx = platformSettingContext(ctx)

	dbCtx, cancel := context.WithTimeout(ctx, s.Cfg.DBTimeout)
	defer cancel()

	history, err := s.Models.PlatformSettingHistory.GetByID(
		dbCtx,
		historyID,
	)
	if err != nil {
		return nil, fmt.Errorf(
			"get platform setting history by ID %s: %w",
			historyID,
			err,
		)
	}

	return history, nil
}

// ListPlatformSettingHistoryByPlatformSettingIDInternal retrieves a bounded,
// newest-first page of immutable value-history records for one platform
// setting.
//
// Pagination defaults and maximum limits remain owned by the data layer.
// A successful read with no matching records returns a non-nil empty slice.
func (s *Service) ListPlatformSettingHistoryByPlatformSettingIDInternal(
	ctx context.Context,
	platformSettingID uuid.UUID,
	limit int,
	offset int,
) ([]*data.PlatformSettingHistory, error) {
	if err := validatePlatformSettingService(s); err != nil {
		return nil, err
	}

	ctx = platformSettingContext(ctx)

	dbCtx, cancel := context.WithTimeout(ctx, s.Cfg.DBTimeout)
	defer cancel()

	history, err :=
		s.Models.PlatformSettingHistory.ListByPlatformSettingID(
			dbCtx,
			platformSettingID,
			limit,
			offset,
		)
	if err != nil {
		return nil, fmt.Errorf(
			"list platform setting history by platform setting ID %s: %w",
			platformSettingID,
			err,
		)
	}

	return history, nil
}

// ListPlatformSettingHistoryBySettingKeyInternal retrieves a bounded,
// newest-first page of immutable value-history records for one canonical
// platform-setting key.
//
// Setting-key validation, canonicalization, pagination defaults, and maximum
// limits remain owned by the data layer. A successful read with no matching
// records returns a non-nil empty slice.
func (s *Service) ListPlatformSettingHistoryBySettingKeyInternal(
	ctx context.Context,
	settingKey string,
	limit int,
	offset int,
) ([]*data.PlatformSettingHistory, error) {
	if err := validatePlatformSettingService(s); err != nil {
		return nil, err
	}

	ctx = platformSettingContext(ctx)

	dbCtx, cancel := context.WithTimeout(ctx, s.Cfg.DBTimeout)
	defer cancel()

	history, err := s.Models.PlatformSettingHistory.ListBySettingKey(
		dbCtx,
		settingKey,
		limit,
		offset,
	)
	if err != nil {
		return nil, fmt.Errorf(
			"list platform setting history by setting key %q: %w",
			settingKey,
			err,
		)
	}

	return history, nil
}
