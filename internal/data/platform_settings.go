// Package data provides models and database access methods for platform settings.
//
// sdworkspace/sdbackend/internal/data/platform_settings.go
//
// GTM:
//
//	Layer: 2.1 Database / Governance Foundation
//	Release Class: SPINE
//	Reason:
//	  platform_settings is release-critical system configuration infrastructure.
//	  It provides the canonical, auditable, admin-controlled configuration
//	  surface for Sagrenti's backend.
//
//	  This table is for platform-level operational configuration only. It is
//	  not a generic product-behavior escape hatch, not a user preferences table,
//	  not merchant configuration, and not consumer product configuration.
//
//	  Soft delete preserves historical configuration records while removing
//	  them from standard operational reads. Hard delete is administrative or
//	  pre-startup cleanup only.
//
// SPINE Rule:
//
//	Keep compiling.
//	Keep production-ready.
//	Preserve key-format integrity.
//	Preserve value_type and JSON-shape validation.
//	Preserve soft-delete-aware operational reads.
//	Preserve idempotent startup seed behavior.
//	Preserve configuration audit actor fields.
//	Block deployment if this file breaks build, platform configuration reads,
//	admin configuration mutations, startup seeding, or governance auditability.
package data

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"regexp"
	"time"

	"github.com/PiccoloMondoC/sdworkspace/sdbackend/internal/logging"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var platformSettingKeyPattern = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

// PlatformSettingValueType is the controlled vocabulary for platform setting
// JSON value types.
type PlatformSettingValueType string

const (
	// PlatformSettingValueTypeBoolean indicates setting_value is a JSON boolean.
	PlatformSettingValueTypeBoolean PlatformSettingValueType = "boolean"

	// PlatformSettingValueTypeInteger indicates setting_value is a whole JSON number.
	PlatformSettingValueTypeInteger PlatformSettingValueType = "integer"

	// PlatformSettingValueTypeDecimal indicates setting_value is a JSON number.
	PlatformSettingValueTypeDecimal PlatformSettingValueType = "decimal"

	// PlatformSettingValueTypeString indicates setting_value is a JSON string.
	PlatformSettingValueTypeString PlatformSettingValueType = "string"

	// PlatformSettingValueTypeJSON indicates setting_value is a JSON object or array.
	PlatformSettingValueTypeJSON PlatformSettingValueType = "json"
)

const platformSettingSelectColumns = `
	id,
	setting_key,
	setting_value,
	value_type,
	description,
	is_active,
	created_by,
	updated_by,
	created_at,
	updated_at,
	deleted_at
`

// PlatformSetting represents one row in platform_settings.
type PlatformSetting struct {
	ID           uuid.UUID                `json:"id" db:"id"`
	SettingKey   string                   `json:"setting_key" db:"setting_key"`
	SettingValue json.RawMessage          `json:"setting_value" db:"setting_value"`
	ValueType    PlatformSettingValueType `json:"value_type" db:"value_type"`
	Description  string                   `json:"description" db:"description"`
	IsActive     bool                     `json:"is_active" db:"is_active"`
	CreatedBy    *uuid.UUID               `json:"created_by" db:"created_by"`
	UpdatedBy    *uuid.UUID               `json:"updated_by" db:"updated_by"`
	CreatedAt    time.Time                `json:"created_at" db:"created_at"`
	UpdatedAt    time.Time                `json:"updated_at" db:"updated_at"`
	DeletedAt    *time.Time               `json:"deleted_at" db:"deleted_at"`
}

// PlatformSettingModel owns persistence for platform settings.
type PlatformSettingModel struct {
	DB     *pgxpool.Pool
	Logger *logging.Logger
}

func scanPlatformSetting(row scannableRow, setting *PlatformSetting) error {
	return row.Scan(
		&setting.ID,
		&setting.SettingKey,
		&setting.SettingValue,
		&setting.ValueType,
		&setting.Description,
		&setting.IsActive,
		&setting.CreatedBy,
		&setting.UpdatedBy,
		&setting.CreatedAt,
		&setting.UpdatedAt,
		&setting.DeletedAt,
	)
}

// NormalizePlatformSettingKey trims and canonicalizes a platform setting key.
func NormalizePlatformSettingKey(key string) string {
	return normalizeIdentifier(key)
}

// NormalizePlatformSettingValueType trims and canonicalizes a platform setting value type.
func NormalizePlatformSettingValueType(valueType PlatformSettingValueType) PlatformSettingValueType {
	return PlatformSettingValueType(normalizeIdentifier(string(valueType)))
}

// IsValidPlatformSettingValueType reports whether valueType is allowed.
func IsValidPlatformSettingValueType(valueType PlatformSettingValueType) bool {
	switch NormalizePlatformSettingValueType(valueType) {
	case PlatformSettingValueTypeBoolean,
		PlatformSettingValueTypeInteger,
		PlatformSettingValueTypeDecimal,
		PlatformSettingValueTypeString,
		PlatformSettingValueTypeJSON:
		return true
	default:
		return false
	}
}

func validatePlatformSettingKey(key string) (string, error) {
	key = NormalizePlatformSettingKey(key)
	if key == "" {
		return "", errors.New("platform setting key is required")
	}
	if !platformSettingKeyPattern.MatchString(key) {
		return "", fmt.Errorf("platform setting key %q must match ^[a-z][a-z0-9_]*$", key)
	}
	return key, nil
}

func validatePlatformSettingValueType(valueType PlatformSettingValueType) (PlatformSettingValueType, error) {
	valueType = NormalizePlatformSettingValueType(valueType)
	if !IsValidPlatformSettingValueType(valueType) {
		return "", fmt.Errorf("invalid platform setting value type: %q", valueType)
	}
	return valueType, nil
}

func decodePlatformSettingJSON(value json.RawMessage) (interface{}, error) {
	if len(bytes.TrimSpace(value)) == 0 {
		return nil, errors.New("platform setting value is required")
	}

	decoder := json.NewDecoder(bytes.NewReader(value))
	decoder.UseNumber()

	var decoded interface{}
	if err := decoder.Decode(&decoded); err != nil {
		return nil, fmt.Errorf(
			"platform setting value is not valid JSON: %w",
			err,
		)
	}

	var trailing interface{}
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return nil, errors.New(
				"platform setting value must contain exactly one JSON value",
			)
		}

		return nil, fmt.Errorf(
			"platform setting value contains invalid trailing content: %w",
			err,
		)
	}

	return decoded, nil
}

func validatePlatformSettingValue(value json.RawMessage, valueType PlatformSettingValueType) error {
	decoded, err := decodePlatformSettingJSON(value)
	if err != nil {
		return err
	}

	switch valueType {
	case PlatformSettingValueTypeBoolean:
		if _, ok := decoded.(bool); !ok {
			return fmt.Errorf("platform setting value must be a JSON boolean for value_type %q", valueType)
		}

	case PlatformSettingValueTypeInteger:
		number, ok := decoded.(json.Number)
		if !ok {
			return fmt.Errorf("platform setting value must be a JSON number for value_type %q", valueType)
		}
		if _, ok := new(big.Int).SetString(number.String(), 10); !ok {
			return fmt.Errorf("platform setting value must be a whole JSON number for value_type %q", valueType)
		}

	case PlatformSettingValueTypeDecimal:
		number, ok := decoded.(json.Number)
		if !ok {
			return fmt.Errorf("platform setting value must be a JSON number for value_type %q", valueType)
		}
		if _, ok := new(big.Rat).SetString(number.String()); !ok {
			return fmt.Errorf("platform setting value must be a valid decimal JSON number for value_type %q", valueType)
		}

	case PlatformSettingValueTypeString:
		if _, ok := decoded.(string); !ok {
			return fmt.Errorf("platform setting value must be a JSON string for value_type %q", valueType)
		}

	case PlatformSettingValueTypeJSON:
		switch decoded.(type) {
		case map[string]interface{}, []interface{}:
			return nil
		default:
			return fmt.Errorf("platform setting value must be a JSON object or array for value_type %q", valueType)
		}
	}

	return nil
}

func validateOptionalActorUUID(id *uuid.UUID, fieldName string) error {
	if id != nil && *id == uuid.Nil {
		return fmt.Errorf("%s must not be zero UUID when provided", fieldName)
	}
	return nil
}

func validateRequiredActorUUID(id uuid.UUID, fieldName string) error {
	if id == uuid.Nil {
		return fmt.Errorf("%s is required", fieldName)
	}
	return nil
}

func validatePlatformSettingForInsert(setting *PlatformSetting) error {
	if setting == nil {
		return errors.New("platform setting is required")
	}

	key, err := validatePlatformSettingKey(setting.SettingKey)
	if err != nil {
		return err
	}
	setting.SettingKey = key

	valueType, err := validatePlatformSettingValueType(setting.ValueType)
	if err != nil {
		return err
	}
	setting.ValueType = valueType

	if err := validatePlatformSettingValue(setting.SettingValue, setting.ValueType); err != nil {
		return err
	}
	if err := validateOptionalActorUUID(setting.CreatedBy, "created_by"); err != nil {
		return err
	}
	if err := validateOptionalActorUUID(setting.UpdatedBy, "updated_by"); err != nil {
		return err
	}

	return nil
}


// Insert inserts a new platform setting.
//
// Duplicate setting_key values return an error. Startup seed paths should use
// Ensure instead.
func (m *PlatformSettingModel) Insert(ctx context.Context, setting *PlatformSetting) error {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("InsertPlatformSetting")

	if err := validatePlatformSettingForInsert(setting); err != nil {
		logger.Error("Validation failed", err)
		return err
	}

	if setting.ID == uuid.Nil {
		setting.ID = uuid.New()
	}

	query := `
		INSERT INTO platform_settings (
			id,
			setting_key,
			setting_value,
			value_type,
			description,
			is_active,
			created_by,
			updated_by
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING created_at, updated_at
	`

	err := m.DB.QueryRow(ctx, query,
		setting.ID,
		setting.SettingKey,
		setting.SettingValue,
		setting.ValueType,
		setting.Description,
		setting.IsActive,
		setting.CreatedBy,
		setting.UpdatedBy,
	).Scan(&setting.CreatedAt, &setting.UpdatedAt)
	if err != nil {
		switch {
		case IsUniqueViolation(err):
			err = fmt.Errorf("%w: key %q", ErrPlatformSettingAlreadyExists, setting.SettingKey)
		case IsForeignKeyViolation(err):
			err = fmt.Errorf("%w: created_by or updated_by user missing", ErrPlatformSettingActorNotFound)
		}

		logger.Error("Insert platform setting failed", err,
			"setting_id", setting.ID,
			"setting_key", setting.SettingKey,
			"value_type", setting.ValueType,
		)
		return err
	}

	logger.Info("Insert platform setting successful",
		"setting_id", setting.ID,
		"setting_key", setting.SettingKey,
		"value_type", setting.ValueType,
		"is_active", setting.IsActive,
	)
	return nil
}

// Ensure inserts or refreshes a platform setting and returns the persisted row.
//
// Ensure is idempotent and race-safe for startup seed paths. If a matching key
// exists but was soft-deleted, Ensure restores it by clearing deleted_at.
func (m *PlatformSettingModel) Ensure(
	ctx context.Context,
	key string,
	value json.RawMessage,
	valueType PlatformSettingValueType,
	description string,
	isActive bool,
	actorID *uuid.UUID,
) (*PlatformSetting, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("EnsurePlatformSetting")

	key, err := validatePlatformSettingKey(key)
	if err != nil {
		logger.Error("Validation failed", err)
		return nil, err
	}

	valueType, err = validatePlatformSettingValueType(valueType)
	if err != nil {
		logger.Error("Validation failed", err)
		return nil, err
	}

	if err := validatePlatformSettingValue(value, valueType); err != nil {
		logger.Error("Validation failed", err)
		return nil, err
	}
	if err := validateOptionalActorUUID(actorID, "actor_id"); err != nil {
		logger.Error("Validation failed", err)
		return nil, err
	}

	query := `
		WITH existing AS (
			SELECT id, deleted_at IS NOT NULL AS was_deleted
			FROM platform_settings
			WHERE setting_key = $1
		),
		upsert AS (
			INSERT INTO platform_settings (
				setting_key,
				setting_value,
				value_type,
				description,
				is_active,
				created_by,
				updated_by
			)
			VALUES ($1, $2, $3, $4, $5, $6, $6)
			ON CONFLICT (setting_key) DO UPDATE
			SET
				setting_value = EXCLUDED.setting_value,
				value_type = EXCLUDED.value_type,
				description = EXCLUDED.description,
				is_active = EXCLUDED.is_active,
				updated_by = EXCLUDED.updated_by,
				updated_at = NOW(),
				deleted_at = NULL
			RETURNING ` + platformSettingSelectColumns + `
		)
		SELECT
			upsert.id,
			upsert.setting_key,
			upsert.setting_value,
			upsert.value_type,
			upsert.description,
			upsert.is_active,
			upsert.created_by,
			upsert.updated_by,
			upsert.created_at,
			upsert.updated_at,
			upsert.deleted_at,
			COALESCE((SELECT was_deleted FROM existing), FALSE) AS was_restored
		FROM upsert
	`

	var setting PlatformSetting
	var wasRestored bool

	err = m.DB.QueryRow(ctx, query,
		key,
		value,
		valueType,
		description,
		isActive,
		actorID,
	).Scan(
		&setting.ID,
		&setting.SettingKey,
		&setting.SettingValue,
		&setting.ValueType,
		&setting.Description,
		&setting.IsActive,
		&setting.CreatedBy,
		&setting.UpdatedBy,
		&setting.CreatedAt,
		&setting.UpdatedAt,
		&setting.DeletedAt,
		&wasRestored,
	)
	if err != nil {
		if IsForeignKeyViolation(err) {
			err = fmt.Errorf("%w: actor user missing", ErrPlatformSettingActorNotFound)
		}
		logger.Error("Ensure platform setting failed", err,
			"setting_key", key,
			"value_type", valueType,
		)
		return nil, err
	}

	if wasRestored {
		logger.Info("Soft-deleted platform setting restored by ensure",
			"setting_id", setting.ID,
			"setting_key", setting.SettingKey,
			"value_type", setting.ValueType,
			"is_active", setting.IsActive,
		)
	} else {
		logger.Info("Ensure platform setting successful",
			"setting_id", setting.ID,
			"setting_key", setting.SettingKey,
			"value_type", setting.ValueType,
			"is_active", setting.IsActive,
		)
	}

	return &setting, nil
}

// GetByID retrieves a non-deleted platform setting by ID.
func (m *PlatformSettingModel) GetByID(ctx context.Context, id uuid.UUID) (*PlatformSetting, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("GetPlatformSettingByID")

	if id == uuid.Nil {
		err := errors.New("platform setting ID is required")
		logger.Error("Validation failed", err)
		return nil, err
	}

	query := `
		SELECT ` + platformSettingSelectColumns + `
		FROM platform_settings
		WHERE id = $1
		  AND deleted_at IS NULL
	`

	var setting PlatformSetting
	err := scanPlatformSetting(m.DB.QueryRow(ctx, query, id), &setting)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			logger.Warn("Platform setting not found by ID", "setting_id", id)
			return nil, nil
		}
		logger.Error("Get platform setting by ID failed", err, "setting_id", id)
		return nil, err
	}

	return &setting, nil
}

// GetByIDIncludingDeleted retrieves a platform setting by ID including soft-deleted rows.
func (m *PlatformSettingModel) GetByIDIncludingDeleted(ctx context.Context, id uuid.UUID) (*PlatformSetting, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("GetPlatformSettingByIDIncludingDeleted")

	if id == uuid.Nil {
		err := errors.New("platform setting ID is required")
		logger.Error("Validation failed", err)
		return nil, err
	}

	query := `
		SELECT ` + platformSettingSelectColumns + `
		FROM platform_settings
		WHERE id = $1
	`

	var setting PlatformSetting
	err := scanPlatformSetting(m.DB.QueryRow(ctx, query, id), &setting)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			logger.Warn("Platform setting not found by ID including deleted", "setting_id", id)
			return nil, nil
		}
		logger.Error("Get platform setting by ID including deleted failed", err, "setting_id", id)
		return nil, err
	}

	return &setting, nil
}

// GetByKey retrieves a non-deleted platform setting by key, whether active or inactive.
func (m *PlatformSettingModel) GetByKey(ctx context.Context, key string) (*PlatformSetting, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("GetPlatformSettingByKey")

	key, err := validatePlatformSettingKey(key)
	if err != nil {
		logger.Error("Validation failed", err)
		return nil, err
	}

	query := `
		SELECT ` + platformSettingSelectColumns + `
		FROM platform_settings
		WHERE setting_key = $1
		  AND deleted_at IS NULL
	`

	var setting PlatformSetting
	err = scanPlatformSetting(m.DB.QueryRow(ctx, query, key), &setting)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			logger.Warn("Platform setting not found by key", "setting_key", key)
			return nil, nil
		}
		logger.Error("Get platform setting by key failed", err, "setting_key", key)
		return nil, err
	}

	return &setting, nil
}

// GetByKeyIncludingDeleted retrieves a platform setting by key including soft-deleted rows.
func (m *PlatformSettingModel) GetByKeyIncludingDeleted(ctx context.Context, key string) (*PlatformSetting, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("GetPlatformSettingByKeyIncludingDeleted")

	key, err := validatePlatformSettingKey(key)
	if err != nil {
		logger.Error("Validation failed", err)
		return nil, err
	}

	query := `
		SELECT ` + platformSettingSelectColumns + `
		FROM platform_settings
		WHERE setting_key = $1
	`

	var setting PlatformSetting
	err = scanPlatformSetting(m.DB.QueryRow(ctx, query, key), &setting)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			logger.Warn("Platform setting not found by key including deleted", "setting_key", key)
			return nil, nil
		}
		logger.Error("Get platform setting by key including deleted failed", err, "setting_key", key)
		return nil, err
	}

	return &setting, nil
}

// GetActiveByKey retrieves an active, non-deleted platform setting by key.
func (m *PlatformSettingModel) GetActiveByKey(ctx context.Context, key string) (*PlatformSetting, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("GetActivePlatformSettingByKey")

	key, err := validatePlatformSettingKey(key)
	if err != nil {
		logger.Error("Validation failed", err)
		return nil, err
	}

	query := `
		SELECT ` + platformSettingSelectColumns + `
		FROM platform_settings
		WHERE setting_key = $1
		  AND is_active = TRUE
		  AND deleted_at IS NULL
	`

	var setting PlatformSetting
	err = scanPlatformSetting(m.DB.QueryRow(ctx, query, key), &setting)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			logger.Warn("Active platform setting not found by key", "setting_key", key)
			return nil, nil
		}
		logger.Error("Get active platform setting by key failed", err, "setting_key", key)
		return nil, err
	}

	return &setting, nil
}

// ListActive retrieves all active, non-deleted platform settings.
func (m *PlatformSettingModel) ListActive(ctx context.Context) ([]*PlatformSetting, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("ListActivePlatformSettings")

	query := `
		SELECT ` + platformSettingSelectColumns + `
		FROM platform_settings
		WHERE is_active = TRUE
		  AND deleted_at IS NULL
		ORDER BY setting_key ASC
	`

	rows, err := m.DB.Query(ctx, query)
	if err != nil {
		logger.Error("List active platform settings failed", err)
		return nil, err
	}
	defer rows.Close()

	var settings []*PlatformSetting
	for rows.Next() {
		var setting PlatformSetting
		if err := scanPlatformSetting(rows, &setting); err != nil {
			logger.Error("Scan active platform setting failed", err)
			return nil, err
		}
		settings = append(settings, &setting)
	}
	if err := rows.Err(); err != nil {
		logger.Error("Iterate active platform settings failed", err)
		return nil, err
	}

	return settings, nil
}

// List retrieves all non-deleted platform settings, active and inactive.
func (m *PlatformSettingModel) List(ctx context.Context) ([]*PlatformSetting, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("ListPlatformSettings")

	query := `
		SELECT ` + platformSettingSelectColumns + `
		FROM platform_settings
		WHERE deleted_at IS NULL
		ORDER BY setting_key ASC
	`

	rows, err := m.DB.Query(ctx, query)
	if err != nil {
		logger.Error("List platform settings failed", err)
		return nil, err
	}
	defer rows.Close()

	var settings []*PlatformSetting
	for rows.Next() {
		var setting PlatformSetting
		if err := scanPlatformSetting(rows, &setting); err != nil {
			logger.Error("Scan platform setting failed", err)
			return nil, err
		}
		settings = append(settings, &setting)
	}
	if err := rows.Err(); err != nil {
		logger.Error("Iterate platform settings failed", err)
		return nil, err
	}

	return settings, nil
}

// ListIncludingDeleted retrieves all platform settings including soft-deleted rows.
func (m *PlatformSettingModel) ListIncludingDeleted(ctx context.Context) ([]*PlatformSetting, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("ListPlatformSettingsIncludingDeleted")

	query := `
		SELECT ` + platformSettingSelectColumns + `
		FROM platform_settings
		ORDER BY setting_key ASC
	`

	rows, err := m.DB.Query(ctx, query)
	if err != nil {
		logger.Error("List platform settings including deleted failed", err)
		return nil, err
	}
	defer rows.Close()

	var settings []*PlatformSetting
	for rows.Next() {
		var setting PlatformSetting
		if err := scanPlatformSetting(rows, &setting); err != nil {
			logger.Error("Scan platform setting including deleted failed", err)
			return nil, err
		}
		settings = append(settings, &setting)
	}
	if err := rows.Err(); err != nil {
		logger.Error("Iterate platform settings including deleted failed", err)
		return nil, err
	}

	return settings, nil
}

// UpdateValue updates setting_value and description for a non-deleted setting.
//
// value_type must match the setting's existing value_type. Retyping platform
// settings is intentionally rejected to prevent downstream typed-reader breakage.
func (m *PlatformSettingModel) UpdateValue(
	ctx context.Context,
	id uuid.UUID,
	value json.RawMessage,
	valueType PlatformSettingValueType,
	description string,
	updatedBy uuid.UUID,
) (*PlatformSetting, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("UpdatePlatformSettingValue")

	if id == uuid.Nil {
		err := errors.New("platform setting ID is required")
		logger.Error("Validation failed", err)
		return nil, err
	}
	if err := validateRequiredActorUUID(updatedBy, "updated_by"); err != nil {
		logger.Error("Validation failed", err)
		return nil, err
	}

	valueType, err := validatePlatformSettingValueType(valueType)
	if err != nil {
		logger.Error("Validation failed", err)
		return nil, err
	}
	if err := validatePlatformSettingValue(value, valueType); err != nil {
		logger.Error("Validation failed", err)
		return nil, err
	}

	query := `
		UPDATE platform_settings
		SET
			setting_value = $2,
			description = $3,
			updated_by = $4,
			updated_at = NOW()
		WHERE id = $1
		  AND deleted_at IS NULL
		  AND value_type = $5
		RETURNING ` + platformSettingSelectColumns + `
	`

	var setting PlatformSetting
	err = scanPlatformSetting(m.DB.QueryRow(ctx, query, id, value, description, updatedBy, valueType), &setting)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			var existingValueType PlatformSettingValueType
			checkErr := m.DB.QueryRow(ctx, `
				SELECT value_type
				FROM platform_settings
				WHERE id = $1
				  AND deleted_at IS NULL
			`, id).Scan(&existingValueType)

			switch {
			case errors.Is(checkErr, pgx.ErrNoRows):
				err = fmt.Errorf("%w: id %s", ErrPlatformSettingNotFound, id)
			case checkErr != nil:
				err = checkErr
			default:
				err = fmt.Errorf(
					"%w: setting %s has value_type %q, update requested %q",
					ErrPlatformSettingTypeMismatch,
					id,
					existingValueType,
					valueType,
				)
			}
		} else if IsForeignKeyViolation(err) {
			err = fmt.Errorf("%w: updated_by user %s", ErrPlatformSettingActorNotFound, updatedBy)
		}

		logger.Error("Update platform setting value failed", err, "setting_id", id)
		return nil, err
	}

	logger.Info("Update platform setting value successful",
		"setting_id", setting.ID,
		"setting_key", setting.SettingKey,
		"value_type", setting.ValueType,
		"updated_by", updatedBy,
	)

	return &setting, nil
}

// SetActive updates is_active for a non-deleted platform setting.
func (m *PlatformSettingModel) SetActive(
	ctx context.Context,
	id uuid.UUID,
	isActive bool,
	updatedBy uuid.UUID,
) (*PlatformSetting, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("SetPlatformSettingActive")

	if id == uuid.Nil {
		err := errors.New("platform setting ID is required")
		logger.Error("Validation failed", err)
		return nil, err
	}
	if err := validateRequiredActorUUID(updatedBy, "updated_by"); err != nil {
		logger.Error("Validation failed", err)
		return nil, err
	}

	query := `
		UPDATE platform_settings
		SET
			is_active = $2,
			updated_by = $3,
			updated_at = NOW()
		WHERE id = $1
		  AND deleted_at IS NULL
		RETURNING ` + platformSettingSelectColumns + `
	`

	var setting PlatformSetting
	err := scanPlatformSetting(m.DB.QueryRow(ctx, query, id, isActive, updatedBy), &setting)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			err = fmt.Errorf("%w: id %s", ErrPlatformSettingNotFound, id)
		} else if IsForeignKeyViolation(err) {
			err = fmt.Errorf("%w: updated_by user %s", ErrPlatformSettingActorNotFound, updatedBy)
		}
		logger.Error("Set platform setting active failed", err, "setting_id", id)
		return nil, err
	}

	logger.Info("Set platform setting active successful",
		"setting_id", setting.ID,
		"setting_key", setting.SettingKey,
		"is_active", setting.IsActive,
		"updated_by", updatedBy,
	)

	return &setting, nil
}

// SoftDelete marks a platform setting deleted.
func (m *PlatformSettingModel) SoftDelete(ctx context.Context, id uuid.UUID, updatedBy uuid.UUID) error {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("SoftDeletePlatformSetting")

	if id == uuid.Nil {
		err := errors.New("platform setting ID is required")
		logger.Error("Validation failed", err)
		return err
	}
	if err := validateRequiredActorUUID(updatedBy, "updated_by"); err != nil {
		logger.Error("Validation failed", err)
		return err
	}

	query := `
		UPDATE platform_settings
		SET
			deleted_at = NOW(),
			updated_by = $2,
			updated_at = NOW()
		WHERE id = $1
		  AND deleted_at IS NULL
		RETURNING setting_key
	`

	var key string
	err := m.DB.QueryRow(ctx, query, id, updatedBy).Scan(&key)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			err = fmt.Errorf("%w: id %s", ErrPlatformSettingNotFound, id)
		} else if IsForeignKeyViolation(err) {
			err = fmt.Errorf("%w: updated_by user %s", ErrPlatformSettingActorNotFound, updatedBy)
		}
		logger.Error("Soft delete platform setting failed", err, "setting_id", id)
		return err
	}

	logger.Info("Soft delete platform setting successful", "setting_id", id, "setting_key", key)
	return nil
}

// HardDelete permanently removes a platform setting row.
//
// This is administrative/pre-startup cleanup only. Ordinary operational removal
// must use SoftDelete.
func (m *PlatformSettingModel) HardDelete(ctx context.Context, id uuid.UUID) error {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("HardDeletePlatformSetting")

	if id == uuid.Nil {
		err := errors.New("platform setting ID is required")
		logger.Error("Validation failed", err)
		return err
	}

	query := `
		DELETE FROM platform_settings
		WHERE id = $1
		AND deleted_at IS NOT NULL
		RETURNING setting_key
	`

	var key string
	err := m.DB.QueryRow(ctx, query, id).Scan(&key)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			err = fmt.Errorf("%w: soft-deleted platform setting with ID %s", ErrPlatformSettingNotFound, id)
		}
		logger.Error("Hard delete platform setting failed", err, "setting_id", id)
		return err
	}

	logger.Info("Hard delete platform setting successful", "setting_id", id, "setting_key", key)
	return nil
}

// ExistsActiveByKey reports whether an active, non-deleted setting exists.
func (m *PlatformSettingModel) ExistsActiveByKey(ctx context.Context, key string) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("ExistsActivePlatformSettingByKey")

	key, err := validatePlatformSettingKey(key)
	if err != nil {
		logger.Error("Validation failed", err)
		return false, err
	}

	query := `
		SELECT EXISTS (
			SELECT 1
			FROM platform_settings
			WHERE setting_key = $1
			  AND is_active = TRUE
			  AND deleted_at IS NULL
		)
	`

	var exists bool
	if err := m.DB.QueryRow(ctx, query, key).Scan(&exists); err != nil {
		logger.Error("Check active platform setting existence failed", err, "setting_key", key)
		return false, err
	}

	return exists, nil
}