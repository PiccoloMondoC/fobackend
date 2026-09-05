// Package data provides shared data-layer models and database access methods.
//
// sdworkspace/sdbackend/internal/data/commons.go
//
// GTM:
//
//	Layer: 2.5 Catalog / Offer Domain
//	Release Class: SPINE
//	Reason:
//	  Global handles are release-critical because they reserve scarce public
//	  identity names across user, brand, and merchant surfaces. This file
//	  protects identity-handle integrity while keeping catalog slugs and
//	  offer keys governed by their own canonical tables.
//
// SPINE Rule:
//
//	Keep compiling.
//	Keep production-ready.
//	Preserve global handle uniqueness.
//	Preserve user/brand/merchant-only entity scope.
//	Preserve hard-delete reservation semantics.
//	Block deployment if this file breaks build, identity-handle reservation,
//	collision protection, or canonical public identity integrity.
package data

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/PiccoloMondoC/focodebase/fobackend/internal/logging"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	GlobalHandleEntityTypeUser     = "user"
	GlobalHandleEntityTypeBrand    = "brand"
	GlobalHandleEntityTypeMerchant = "merchant"
)

// GlobalHandle represents a reserved public identity handle.
//
// This table is not a general slug registry. It protects scarce public identity
// names across user, brand, and merchant surfaces. Departments, categories, and
// offers must keep their own slug/offer_key uniqueness in their own tables.
type GlobalHandle struct {
	Handle     string    `json:"handle" db:"handle"`
	EntityType string    `json:"entity_type" db:"entity_type"`
	EntityID   uuid.UUID `json:"entity_id" db:"entity_id"`
	CreatedAt  time.Time `json:"created_at" db:"created_at"`
}

// GlobalHandleModel manages globally reserved public identity handles.
type GlobalHandleModel struct {
	DB     *pgxpool.Pool
	Logger *logging.Logger
}

// Insert reserves a public identity handle.
//
// The database owns collision protection through PRIMARY KEY(handle). This
// method intentionally avoids SELECT-before-INSERT because that pattern is
// race-prone under concurrent registration.
func (m *GlobalHandleModel) Insert(ctx context.Context, handle, entityType string, entityID uuid.UUID) error {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("InsertGlobalHandle")

	handle = normalizeGlobalHandle(handle)
	entityType = normalizeGlobalHandleEntityType(entityType)

	if handle == "" {
		err := errors.New("global handle is required")
		logger.Error("global handle validation failed", "error", err)
		return err
	}

	if err := validateGlobalHandleEntityType(entityType); err != nil {
		logger.Error("global handle entity type validation failed", "error", err, "entity_type", entityType)
		return err
	}

	if entityID == uuid.Nil {
		err := errors.New("global handle entity ID is required")
		logger.Error("global handle entity ID validation failed", "error", err, "entity_type", entityType)
		return err
	}

	const query = `
		INSERT INTO global_handles (
			handle,
			entity_type,
			entity_id
		)
		VALUES ($1, $2, $3)
	`

	if _, err := m.DB.Exec(ctx, query, handle, entityType, entityID); err != nil {
		if IsUniqueViolation(err) {
			logger.Error(
				"global handle already reserved",
				"error", ErrGlobalHandleTaken,
				"handle", handle,
				"entity_type", entityType,
				"entity_id", entityID,
			)
			return ErrGlobalHandleTaken
		}

		logger.Error(
			"insert global handle failed",
			"error", err,
			"handle", handle,
			"entity_type", entityType,
			"entity_id", entityID,
		)
		return fmt.Errorf("insert global handle: %w", err)
	}

	logger.Info(
		"global handle reserved",
		"handle", handle,
		"entity_type", entityType,
		"entity_id", entityID,
	)

	return nil
}

// Exists reports whether a public identity handle is already reserved.
//
// Blank handles are invalid input and return ErrRecordNotFound rather than
// false, nil so callers can distinguish invalid lookup input from a valid
// negative lookup.
func (m *GlobalHandleModel) Exists(ctx context.Context, handle string) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("GlobalHandleExists")

	handle = normalizeGlobalHandle(handle)
	if handle == "" {
		logger.Warn("global handle existence check skipped because handle is blank")
		return false, ErrRecordNotFound
	}

	const query = `
		SELECT EXISTS (
			SELECT 1
			FROM global_handles
			WHERE handle = $1
		)
	`

	var exists bool
	if err := m.DB.QueryRow(ctx, query, handle).Scan(&exists); err != nil {
		logger.Error("check global handle existence failed", "error", err, "handle", handle)
		return false, fmt.Errorf("check global handle existence: %w", err)
	}

	return exists, nil
}

// GetByHandle retrieves a reserved public identity handle.
func (m *GlobalHandleModel) GetByHandle(ctx context.Context, handle string) (*GlobalHandle, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("GetGlobalHandleByHandle")

	handle = normalizeGlobalHandle(handle)
	if handle == "" {
		logger.Warn("global handle lookup skipped because handle is blank")
		return nil, ErrRecordNotFound
	}

	const query = `
		SELECT
			handle,
			entity_type,
			entity_id,
			created_at
		FROM global_handles
		WHERE handle = $1
	`

	var globalHandle GlobalHandle
	if err := m.DB.QueryRow(ctx, query, handle).Scan(
		&globalHandle.Handle,
		&globalHandle.EntityType,
		&globalHandle.EntityID,
		&globalHandle.CreatedAt,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrGlobalHandleNotFound
		}

		logger.Error("get global handle failed", "error", err, "handle", handle)
		return nil, fmt.Errorf("get global handle: %w", err)
	}

	return &globalHandle, nil
}

// Delete releases a reserved public identity handle.
//
// This is a hard delete because global_handles is a reservation table, not a
// lifecycle-bearing business record. If historical handle ownership becomes a
// product requirement later, that should become a separate handle history table.
func (m *GlobalHandleModel) Delete(ctx context.Context, handle string) error {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("DeleteGlobalHandle")

	handle = normalizeGlobalHandle(handle)
	if handle == "" {
		logger.Warn("global handle delete skipped because handle is blank")
		return ErrRecordNotFound
	}

	const query = `
		DELETE FROM global_handles
		WHERE handle = $1
		RETURNING handle
	`

	var deletedHandle string
	if err := m.DB.QueryRow(ctx, query, handle).Scan(&deletedHandle); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrGlobalHandleNotFound
		}

		logger.Error("delete global handle failed", "error", err, "handle", handle)
		return fmt.Errorf("delete global handle: %w", err)
	}

	logger.Info("global handle released", "handle", deletedHandle)
	return nil
}

// validateNonNegativeDecimalString validates a decimal string that may be zero.
//
// The value is trimmed before validation. The decimal must be syntactically
// valid and greater than or equal to zero.
func validateNonNegativeDecimalString(value, fieldName string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return fmt.Errorf("%s is required", fieldName)
	}
	r, ok := new(big.Rat).SetString(value)
	if !ok {
		return fmt.Errorf("%s must be a valid decimal value", fieldName)
	}
	if r.Sign() < 0 {
		return fmt.Errorf("%s must be greater than or equal to zero", fieldName)
	}
	return nil
}

// validatePositiveDecimalString validates a decimal string that must be
// strictly greater than zero.
//
// The value is trimmed before validation. The decimal must be syntactically
// valid and positive.
func validatePositiveDecimalString(value, fieldName string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return fmt.Errorf("%s is required", fieldName)
	}
	r, ok := new(big.Rat).SetString(value)
	if !ok {
		return fmt.Errorf("%s must be a valid decimal value", fieldName)
	}
	if r.Sign() <= 0 {
		return fmt.Errorf("%s must be greater than zero", fieldName)
	}
	return nil
}

// normalizeOptionalString trims whitespace from an optional string pointer.
// Returns nil if the input is nil or trims to empty.
func normalizeOptionalString(v *string) *string {
	if v == nil {
		return nil
	}
	s := strings.TrimSpace(*v)
	if s == "" {
		return nil
	}
	return &s
}

// normalizeIdentifier trims surrounding whitespace and converts identifiers
// to canonical lowercase form.
func normalizeIdentifier(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

func normalizeGlobalHandle(handle string) string {
	return normalizeIdentifier(handle)
}

func normalizeGlobalHandleEntityType(entityType string) string {
	return normalizeIdentifier(entityType)
}

func validateGlobalHandleEntityType(entityType string) error {
	switch entityType {
	case GlobalHandleEntityTypeUser, GlobalHandleEntityTypeBrand, GlobalHandleEntityTypeMerchant:
		return nil
	default:
		return fmt.Errorf("invalid global handle entity type: %s", entityType)
	}
}
