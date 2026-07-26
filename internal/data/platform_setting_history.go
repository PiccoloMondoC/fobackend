// Package data provides models and database access methods for platform
// setting value history.
//
// sdworkspace/sdbackend/internal/data/platform_setting_history.go
//
// GTM:
//
//	Layer: 2.1 Database / Governance Foundation
//	Release Class: SPINE
//	Reason:
//	  platform_setting_history is release-critical configuration-governance
//	  infrastructure. It preserves immutable value transitions for canonical
//	  platform settings and supports privileged historical review.
//
//	  This table records platform-setting value changes. It is not a complete
//	  lifecycle-event ledger for activation, deactivation, restoration, or
//	  deletion because those operation types are not represented by the
//	  current schema.
//
// SPINE Rule:
//
//	Keep compiling.
//	Keep production-ready.
//	Preserve append-only value-history semantics.
//	Preserve atomic insertion with the corresponding platform-setting mutation.
//	Preserve database-derived setting-key integrity.
//	Preserve valid JSON values.
//	Preserve database-owned change timestamps.
//	Preserve nullable actor attribution.
//	Preserve bounded, deterministic history reads.
//	Never log previous_value or new_value.
//	Block deployment if this file breaks build, configuration value-history
//	integrity, transactional auditability, or privileged history reads.
package data

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/PiccoloMondoC/sdworkspace/sdbackend/internal/logging"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const platformSettingHistorySelectColumns = `
	id,
	platform_setting_id,
	setting_key,
	previous_value,
	new_value,
	change_reason,
	changed_by,
	changed_at
`

const (
	defaultPlatformSettingHistoryLimit = 50
	maxPlatformSettingHistoryLimit     = 200
)

// PlatformSettingHistory represents one immutable platform-setting value
// transition.
//
// SettingKey is database-derived from the referenced platform setting during
// insertion. PreviousValue is nil only when no prior value existed.
// ChangedAt is database-owned.
type PlatformSettingHistory struct {
	ID                uuid.UUID       `json:"id" db:"id"`
	PlatformSettingID uuid.UUID       `json:"platform_setting_id" db:"platform_setting_id"`
	SettingKey        string          `json:"setting_key" db:"setting_key"`
	PreviousValue     json.RawMessage `json:"previous_value" db:"previous_value"`
	NewValue          json.RawMessage `json:"new_value" db:"new_value"`
	ChangeReason      *string         `json:"change_reason" db:"change_reason"`
	ChangedBy         *uuid.UUID      `json:"changed_by" db:"changed_by"`
	ChangedAt         time.Time       `json:"changed_at" db:"changed_at"`
}

// PlatformSettingHistoryModel owns persistence and privileged reads for
// platform-setting value history.
type PlatformSettingHistoryModel struct {
	DB     *pgxpool.Pool
	Logger *logging.Logger
}

func scanPlatformSettingHistory(
	row scannableRow,
	history *PlatformSettingHistory,
) error {
	return row.Scan(
		&history.ID,
		&history.PlatformSettingID,
		&history.SettingKey,
		&history.PreviousValue,
		&history.NewValue,
		&history.ChangeReason,
		&history.ChangedBy,
		&history.ChangedAt,
	)
}

func validatePlatformSettingHistoryPagination(
	limit int,
	offset int,
) (int, int, error) {
	if limit == 0 {
		limit = defaultPlatformSettingHistoryLimit
	}
	if limit < 0 {
		return 0, 0, errors.New("limit must not be negative")
	}
	if limit > maxPlatformSettingHistoryLimit {
		return 0, 0, fmt.Errorf(
			"limit must not exceed %d",
			maxPlatformSettingHistoryLimit,
		)
	}
	if offset < 0 {
		return 0, 0, errors.New("offset must not be negative")
	}

	return limit, offset, nil
}

func normalizePlatformSettingHistoryReason(reason *string) *string {
	if reason == nil {
		return nil
	}

	normalized := strings.TrimSpace(*reason)
	if normalized == "" {
		return nil
	}

	return &normalized
}

func validatePlatformSettingHistoryForInsert(
	history *PlatformSettingHistory,
) error {
	if history == nil {
		return errors.New("platform setting history is required")
	}
	if history.PlatformSettingID == uuid.Nil {
		return errors.New("platform setting ID is required")
	}
	if len(history.PreviousValue) > 0 {
		if _, err := decodePlatformSettingJSON(history.PreviousValue); err != nil {
			return fmt.Errorf("invalid previous platform setting value: %w", err)
		}
	}
	if _, err := decodePlatformSettingJSON(history.NewValue); err != nil {
		return fmt.Errorf("invalid new platform setting value: %w", err)
	}
	if err := validateOptionalActorUUID(history.ChangedBy, "changed_by"); err != nil {
		return err
	}

	history.ChangeReason = normalizePlatformSettingHistoryReason(
		history.ChangeReason,
	)

	return nil
}

// InsertTx inserts an immutable platform-setting value-history row inside an
// existing transaction.
//
// The caller must use the same transaction for the canonical platform-setting
// mutation and this history insert. InsertTx derives setting_key from
// platform_settings rather than trusting caller-supplied duplicated data.
//
// The method does not commit or roll back tx.
func (m *PlatformSettingHistoryModel) InsertTx(
	ctx context.Context,
	tx pgx.Tx,
	history *PlatformSettingHistory,
) error {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.
		GetLoggerWithContextFromContext(ctx).
		WithFunctionName("InsertPlatformSettingHistoryTx")

	if tx == nil {
		err := errors.New("platform setting history transaction is required")
		logger.Error("Validation failed", err)
		return err
	}
	if err := validatePlatformSettingHistoryForInsert(history); err != nil {
		logger.Error("Validation failed", err)
		return err
	}

	if history.ID == uuid.Nil {
		history.ID = uuid.New()
	}

	query := `
		INSERT INTO platform_setting_history (
			id,
			platform_setting_id,
			setting_key,
			previous_value,
			new_value,
			change_reason,
			changed_by
		)
		SELECT
			$1,
			ps.id,
			ps.setting_key,
			$3,
			$4,
			$5,
			$6
		FROM platform_settings AS ps
		WHERE ps.id = $2
		RETURNING
			setting_key,
			changed_at
	`

	err := tx.QueryRow(
		ctx,
		query,
		history.ID,
		history.PlatformSettingID,
		history.PreviousValue,
		history.NewValue,
		history.ChangeReason,
		history.ChangedBy,
	).Scan(
		&history.SettingKey,
		&history.ChangedAt,
	)
	if err != nil {
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			err = fmt.Errorf(
				"%w: id %s",
				ErrPlatformSettingNotFound,
				history.PlatformSettingID,
			)

		case IsForeignKeyViolation(err):
			err = fmt.Errorf(
				"%w: changed_by user missing",
				ErrPlatformSettingActorNotFound,
			)
		}

		logger.Error(
			"Insert platform setting history failed",
			err,
			"history_id", history.ID,
			"platform_setting_id", history.PlatformSettingID,
		)
		return err
	}

	logger.Info(
		"Insert platform setting history successful",
		"history_id", history.ID,
		"platform_setting_id", history.PlatformSettingID,
		"setting_key", history.SettingKey,
	)

	return nil
}

// GetByID retrieves one platform-setting history row by ID.
//
// GetByID returns nil, nil when no matching row exists.
func (m *PlatformSettingHistoryModel) GetByID(
	ctx context.Context,
	id uuid.UUID,
) (*PlatformSettingHistory, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.
		GetLoggerWithContextFromContext(ctx).
		WithFunctionName("GetPlatformSettingHistoryByID")

	if id == uuid.Nil {
		err := errors.New("platform setting history ID is required")
		logger.Error("Validation failed", err)
		return nil, err
	}

	query := `
		SELECT ` + platformSettingHistorySelectColumns + `
		FROM platform_setting_history
		WHERE id = $1
	`

	var history PlatformSettingHistory
	err := scanPlatformSettingHistory(
		m.DB.QueryRow(ctx, query, id),
		&history,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			logger.Warn(
				"Platform setting history not found by ID",
				"history_id", id,
			)
			return nil, nil
		}

		logger.Error(
			"Get platform setting history by ID failed",
			err,
			"history_id", id,
		)
		return nil, err
	}

	return &history, nil
}

// ListByPlatformSettingID retrieves a bounded page of value-history rows for
// one platform setting, newest first.
func (m *PlatformSettingHistoryModel) ListByPlatformSettingID(
	ctx context.Context,
	platformSettingID uuid.UUID,
	limit int,
	offset int,
) ([]*PlatformSettingHistory, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.
		GetLoggerWithContextFromContext(ctx).
		WithFunctionName("ListPlatformSettingHistoryByPlatformSettingID")

	if platformSettingID == uuid.Nil {
		err := errors.New("platform setting ID is required")
		logger.Error("Validation failed", err)
		return nil, err
	}

	limit, offset, err := validatePlatformSettingHistoryPagination(
		limit,
		offset,
	)
	if err != nil {
		logger.Error("Validation failed", err)
		return nil, err
	}

	query := `
		SELECT ` + platformSettingHistorySelectColumns + `
		FROM platform_setting_history
		WHERE platform_setting_id = $1
		ORDER BY changed_at DESC, id DESC
		LIMIT $2
		OFFSET $3
	`

	rows, err := m.DB.Query(
		ctx,
		query,
		platformSettingID,
		limit,
		offset,
	)
	if err != nil {
		logger.Error(
			"List platform setting history by setting ID failed",
			err,
			"platform_setting_id", platformSettingID,
			"limit", limit,
			"offset", offset,
		)
		return nil, err
	}
	defer rows.Close()

	history := make([]*PlatformSettingHistory, 0)

	for rows.Next() {
		var entry PlatformSettingHistory
		if err := scanPlatformSettingHistory(rows, &entry); err != nil {
			logger.Error(
				"Scan platform setting history failed",
				err,
				"platform_setting_id", platformSettingID,
			)
			return nil, err
		}

		history = append(history, &entry)
	}

	if err := rows.Err(); err != nil {
		logger.Error(
			"Iterate platform setting history failed",
			err,
			"platform_setting_id", platformSettingID,
		)
		return nil, err
	}

	logger.Info(
		"List platform setting history by setting ID successful",
		"platform_setting_id", platformSettingID,
		"result_count", len(history),
		"limit", limit,
		"offset", offset,
	)

	return history, nil
}

// ListBySettingKey retrieves a bounded page of value-history rows for one
// canonical platform-setting key, newest first.
func (m *PlatformSettingHistoryModel) ListBySettingKey(
	ctx context.Context,
	key string,
	limit int,
	offset int,
) ([]*PlatformSettingHistory, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.
		GetLoggerWithContextFromContext(ctx).
		WithFunctionName("ListPlatformSettingHistoryBySettingKey")

	key, err := validatePlatformSettingKey(key)
	if err != nil {
		logger.Error("Validation failed", err)
		return nil, err
	}

	limit, offset, err = validatePlatformSettingHistoryPagination(
		limit,
		offset,
	)
	if err != nil {
		logger.Error("Validation failed", err)
		return nil, err
	}

	query := `
		SELECT ` + platformSettingHistorySelectColumns + `
		FROM platform_setting_history
		WHERE setting_key = $1
		ORDER BY changed_at DESC, id DESC
		LIMIT $2
		OFFSET $3
	`

	rows, err := m.DB.Query(ctx, query, key, limit, offset)
	if err != nil {
		logger.Error(
			"List platform setting history by key failed",
			err,
			"setting_key", key,
			"limit", limit,
			"offset", offset,
		)
		return nil, err
	}
	defer rows.Close()

	history := make([]*PlatformSettingHistory, 0)

	for rows.Next() {
		var entry PlatformSettingHistory
		if err := scanPlatformSettingHistory(rows, &entry); err != nil {
			logger.Error(
				"Scan platform setting history failed",
				err,
				"setting_key", key,
			)
			return nil, err
		}

		history = append(history, &entry)
	}

	if err := rows.Err(); err != nil {
		logger.Error(
			"Iterate platform setting history failed",
			err,
			"setting_key", key,
		)
		return nil, err
	}

	logger.Info(
		"List platform setting history by key successful",
		"setting_key", key,
		"result_count", len(history),
		"limit", limit,
		"offset", offset,
	)

	return history, nil
}
