// Package data provides models and database access methods for affiliate programs and other entities.
//
// sdworkspace/sdbackend/internal/data/affiliate_programs.go
//
// GTM:
//   Layer: 2.4 Merchant / Affiliate Domain
//   Release Class: DEFERRED
//   Reason:
//     Affiliate programs are release-critical because the Platform needs a
//     canonical affiliate program catalog to support merchant affiliate
//     relationships, provider configuration, outbound integration credentials,
//     and offer monetization. This file must remain production-ready for v1.
//
// SPINE Rule:
//   Keep compiling.
//   Keep production-ready.
//   Keep secret material protected.
//   Preserve public-safe read paths.
//   Block deployment if this file breaks build, persistence, validation,
//   credential-safety, or affiliate program integrity.
package data

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/PiccoloMondoC/sdworkspace/sdbackend/internal/logging"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// pgUniqueViolation is the PostgreSQL SQLSTATE code for a unique-constraint
// violation. Used to translate raw DB errors into typed application sentinels
// at the model layer (Policy §2.6).
const pgUniqueViolation = "23505"

// AffiliateProgram represents an affiliate program in the system.
//
// Canonical persistence contract (mirrors affiliate_programs in database.go):
//
//   - APIKeyEncrypted, APIKeyKeyID, and APIKeyLastRotatedAt are secret storage
//     fields. They are never selected on standard read paths (GetByID, GetAll).
//     Use GetSecretMaterialByID for the narrow internal path that requires them
//     (Policy §27.8, §27.11).
//   - Encryption is the responsibility of the handler/service layer before
//     calling Insert or Update (Policy §27.4–27.5).
//   - All three secret fields carry json:"-" so they cannot be emitted via any
//     serialisation path on this model (Policy §27.7, §25.3).
//   - DeletedAt is an internal lifecycle sentinel; json:"-" (Policy §25.3, §18.1).
//   - UpdatedAt is DB-owned via the set_updated_at trigger. The application must
//     never write it directly; it is read back via RETURNING (Policy §5.3).
//   - Insert and Update normalise and validate on an internal clone. On success
//     the full persisted form (including normalised fields and DB-owned timestamps)
//     is written back via *program = *persisted so the caller observes exactly
//     what was stored (Policy §2.6).
type AffiliateProgram struct {
	ID                  uuid.UUID  `json:"id"                     db:"id"`
	Name                string     `json:"name"                   db:"name"`
	Website             string     `json:"website"                db:"website"`
	APIEndpoint         *string    `json:"api_endpoint,omitempty" db:"api_endpoint"`
	APIAuthMethod       string     `json:"api_auth_method"        db:"api_auth_method"`
	APIKeyEncrypted     []byte     `json:"-"                      db:"api_key_encrypted"`
	APIKeyKeyID         *string    `json:"-"                      db:"api_key_key_id"`
	APIKeyLastRotatedAt *time.Time `json:"-"                      db:"api_key_last_rotated_at"`
	DeletedAt           *time.Time `json:"-"                      db:"deleted_at"`
	CreatedAt           time.Time  `json:"created_at"             db:"created_at"`
	UpdatedAt           time.Time  `json:"updated_at"             db:"updated_at"`
}

// AffiliateProgramSecretMaterial carries internal-only secret storage fields for
// tightly scoped credential access paths (Policy §27.8, §27.11).
type AffiliateProgramSecretMaterial struct {
	// APIKeyEncrypted holds the ciphertext of the API key. Must never be logged
	// or returned in API responses (Policy §17.4, §27.7).
	APIKeyEncrypted []byte

	// APIKeyKeyID identifies the encryption key version used to produce
	// APIKeyEncrypted. Required when APIAuthMethod is APIKey (Policy §27.9).
	APIKeyKeyID *string

	// APIKeyLastRotatedAt is the timestamp of the most recent key rotation.
	// Nil when rotation has not yet occurred.
	APIKeyLastRotatedAt *time.Time
}

// AffiliateProgramModel holds the DB pool and logger for affiliate program persistence.
type AffiliateProgramModel struct {
	DB     *pgxpool.Pool
	Logger *logging.Logger
}

// cloneAffiliateProgram returns a fully independent copy of src. All pointer
// fields are deep-copied so normalisation and validation on the clone can never
// alias back to the caller's original struct (Policy §2.6).
//
// APIKeyEncrypted is explicitly deep-copied because it is a []byte whose backing
// array would otherwise be shared between src and the clone, risking silent
// corruption of caller-owned ciphertext if anything modifies the slice downstream.
func cloneAffiliateProgram(src *AffiliateProgram) *AffiliateProgram {
	if src == nil {
		return nil
	}

	clone := *src // copies all value-type fields and all pointer addresses

	if src.APIEndpoint != nil {
		v := *src.APIEndpoint
		clone.APIEndpoint = &v
	}
	if src.APIKeyKeyID != nil {
		v := *src.APIKeyKeyID
		clone.APIKeyKeyID = &v
	}
	if src.APIKeyLastRotatedAt != nil {
		v := *src.APIKeyLastRotatedAt
		clone.APIKeyLastRotatedAt = &v
	}
	if src.DeletedAt != nil {
		v := *src.DeletedAt
		clone.DeletedAt = &v
	}
	if src.APIKeyEncrypted != nil {
		clone.APIKeyEncrypted = append([]byte(nil), src.APIKeyEncrypted...)
	}

	return &clone
}

// normalizeAffiliateProgram canonicalises text fields in place so the DB never
// accumulates padded or semantically empty values (Policy §2.1, §2.6).
//
// Rules:
//   - Required string fields (Name, Website, APIAuthMethod) are trimmed.
//   - Optional pointer fields (APIEndpoint, APIKeyKeyID) are trimmed; if the
//     trimmed value is empty the pointer is set to nil so the column is written
//     as NULL rather than a blank string.
//
// Always call on the clone produced by cloneAffiliateProgram, never on the
// caller's original struct directly.
func normalizeAffiliateProgram(program *AffiliateProgram) {
	if program == nil {
		return
	}

	program.Name = strings.TrimSpace(program.Name)
	program.Website = strings.TrimSpace(program.Website)
	program.APIAuthMethod = strings.TrimSpace(program.APIAuthMethod)

	if program.APIEndpoint != nil {
		trimmed := strings.TrimSpace(*program.APIEndpoint)
		if trimmed == "" {
			program.APIEndpoint = nil
		} else {
			program.APIEndpoint = &trimmed
		}
	}

	if program.APIKeyKeyID != nil {
		trimmed := strings.TrimSpace(*program.APIKeyKeyID)
		if trimmed == "" {
			program.APIKeyKeyID = nil
		} else {
			program.APIKeyKeyID = &trimmed
		}
	}
}

// validateAffiliateProgram enforces field-level invariants that mirror the
// chk_affiliate_programs_api_key_storage check constraint in database.go.
// Both layers must agree so violations are caught before a DB round-trip
// (Policy §19.1–19.2).
//
// Must only be called after normalizeAffiliateProgram so that comparisons
// against "" and nil operate on already-trimmed canonical values.
func validateAffiliateProgram(program *AffiliateProgram) error {
	if program == nil {
		return errors.New("affiliate program is required")
	}

	if program.Name == "" {
		return errors.New("name is required")
	}

	if program.Website == "" {
		return errors.New("website is required")
	}
	validatedWebsite, err := validateHTTPURL(program.Website)
	if err != nil {
		return fmt.Errorf("website: %w", err)
	}
	program.Website = validatedWebsite

	// APIEndpoint is nil after normalization if it was blank; only validate when
	// a non-empty value was actually supplied.
	if program.APIEndpoint != nil {
		validatedAPIEndpoint, err := validateHTTPURL(*program.APIEndpoint)
		if err != nil {
			return fmt.Errorf("api_endpoint: %w", err)
		}
		program.APIEndpoint = &validatedAPIEndpoint
	}

	switch program.APIAuthMethod {
	case "APIKey":
		// Encrypted key material must already be present. Plaintext must have been
		// encrypted by the handler/service layer before this model is called
		// (Policy §27.5).
		if len(program.APIKeyEncrypted) == 0 {
			return errors.New("api_key_encrypted is required when api_auth_method is APIKey")
		}
		// APIKeyKeyID is nil after normalization if it was blank.
		if program.APIKeyKeyID == nil {
			return errors.New("api_key_key_id is required when api_auth_method is APIKey")
		}

	case "None":
		if len(program.APIKeyEncrypted) > 0 {
			return errors.New("api_key_encrypted must be empty when api_auth_method is None")
		}
		// APIKeyKeyID is nil after normalization if it was blank; a non-nil pointer
		// here means a genuinely non-empty value was supplied, which is invalid.
		if program.APIKeyKeyID != nil {
			return errors.New("api_key_key_id must be nil when api_auth_method is None")
		}
		if program.APIKeyLastRotatedAt != nil {
			return errors.New("api_key_last_rotated_at must be nil when api_auth_method is None")
		}

	default:
		return fmt.Errorf("invalid api_auth_method %q: must be one of APIKey or None", program.APIAuthMethod)
	}

	return nil
}

// isUniqueViolation returns true when err is a PostgreSQL unique-constraint
// violation (SQLSTATE 23505).
func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == pgUniqueViolation
}

// Insert persists a new affiliate program and writes the full canonical persisted
// form back to the caller's struct on success.
//
// Normalisation and validation are performed on an internal clone so the caller's
// original struct is never modified on failure (Policy §2.6). On success,
// *program is replaced with the clone, which carries normalised fields and
// DB-returned lifecycle timestamps — exactly what was stored.
//
// The caller is responsible for encrypting any plaintext API key and populating
// APIKeyEncrypted and APIKeyKeyID before calling this method (Policy §27.4–27.5).
//
// Returns ErrAffiliateProgramAlreadyExists on a unique-constraint violation
// (Policy §2.6).
func (m *AffiliateProgramModel) Insert(ctx context.Context, program *AffiliateProgram) error {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("InsertAffiliateProgram")

	if program == nil {
		err := errors.New("affiliate program is required")
		logger.Error("Validation failed", err)
		return err
	}

	// Work on a clone so failed inserts do not mutate caller-owned state.
	persisted := cloneAffiliateProgram(program)
	normalizeAffiliateProgram(persisted)
	if err := validateAffiliateProgram(persisted); err != nil {
		logger.Error("Validation failed", err)
		return err
	}

	if persisted.ID == uuid.Nil {
		persisted.ID = uuid.New()
	}

	// The DB owns created_at, updated_at, and deleted_at via column defaults and
	// the set_updated_at trigger. RETURNING hydrates the clone; on success the
	// full persisted form is written back to the caller.
	err := m.DB.QueryRow(ctx,
		`INSERT INTO affiliate_programs
			(id, name, website, api_endpoint, api_auth_method,
			 api_key_encrypted, api_key_key_id, api_key_last_rotated_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		 RETURNING created_at, updated_at, deleted_at`,
		persisted.ID,
		persisted.Name,
		persisted.Website,
		persisted.APIEndpoint,
		persisted.APIAuthMethod,
		persisted.APIKeyEncrypted,
		persisted.APIKeyKeyID,
		persisted.APIKeyLastRotatedAt,
	).Scan(&persisted.CreatedAt, &persisted.UpdatedAt, &persisted.DeletedAt)

	if err != nil {
		if isUniqueViolation(err) {
			logger.Warn("Insert affiliate program conflicted with existing row",
				"id", persisted.ID,
				"name", persisted.Name,
				"website", persisted.Website,
			)
			return ErrAffiliateProgramAlreadyExists
		}
		logger.Error("Insert affiliate program failed", err)
		return err
	}

	// Replace the caller's struct with the full canonical persisted form only
	// on success. This includes normalised semantic fields and DB-owned timestamps.
	*program = *persisted

	// Secret fields are intentionally excluded from the log (Policy §17.4, §27.4).
	logger.Info("Insert affiliate program successful",
		"id", program.ID,
		"name", program.Name,
		"website", program.Website,
		"api_auth_method", program.APIAuthMethod,
		"created_at", program.CreatedAt,
		"updated_at", program.UpdatedAt,
	)

	return nil
}

// GetByID retrieves an active (non-deleted) affiliate program by its primary key.
//
// Returns ErrAffiliateProgramNotFound when no active row matches the given ID,
// consistent with the typed not-found contract used by Update (Policy §2.2, §2.6).
//
// Secret storage fields (api_key_encrypted, api_key_key_id, api_key_last_rotated_at)
// are intentionally not selected on this standard read path. Use
// GetSecretMaterialByID for the narrow internal path that requires credential
// material (Policy §27.8, §27.11).
func (m *AffiliateProgramModel) GetByID(ctx context.Context, id uuid.UUID) (*AffiliateProgram, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("GetByIDAffiliateProgram")

	if id == uuid.Nil {
		err := errors.New("invalid UUID: id is required")
		logger.Error("Validation failed", err)
		return nil, err
	}

	query := `
		SELECT id, name, website, api_endpoint, api_auth_method,
		       deleted_at, created_at, updated_at
		FROM affiliate_programs
		WHERE id = $1
		  AND deleted_at IS NULL`

	var program AffiliateProgram

	err := m.DB.QueryRow(ctx, query, id).Scan(
		&program.ID,
		&program.Name,
		&program.Website,
		&program.APIEndpoint,
		&program.APIAuthMethod,
		&program.DeletedAt,
		&program.CreatedAt,
		&program.UpdatedAt,
	)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			logger.Warn("Affiliate program not found", "id", id)
			return nil, ErrAffiliateProgramNotFound
		}
		logger.Error("Failed to retrieve affiliate program", err, "id", id)
		return nil, err
	}

	// Secret fields are intentionally excluded from the log (Policy §17.4).
	logger.Info("Affiliate program retrieved successfully",
		"id", program.ID,
		"name", program.Name,
		"website", program.Website,
		"api_auth_method", program.APIAuthMethod,
		"created_at", program.CreatedAt,
		"updated_at", program.UpdatedAt,
	)

	return &program, nil
}

// GetSecretMaterialByID retrieves encrypted secret storage fields for a single
// active affiliate program. Standard read paths must not use this method.
//
// This method is intentionally narrow and must only be called from internal
// service paths that genuinely require credential material for outbound
// integration work (Policy §27.8, §27.11).
//
// Returns ErrAffiliateProgramSecretNotFound when no active row matches the
// supplied ID.
func (m *AffiliateProgramModel) GetSecretMaterialByID(ctx context.Context, id uuid.UUID) (*AffiliateProgramSecretMaterial, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("GetAffiliateProgramSecretMaterialByID")

	if id == uuid.Nil {
		err := errors.New("invalid UUID: id is required")
		logger.Error("Validation failed", err)
		return nil, err
	}

	var secret AffiliateProgramSecretMaterial

	err := m.DB.QueryRow(ctx, `
		SELECT api_key_encrypted, api_key_key_id, api_key_last_rotated_at
		FROM affiliate_programs
		WHERE id = $1
		  AND deleted_at IS NULL`,
		id,
	).Scan(&secret.APIKeyEncrypted, &secret.APIKeyKeyID, &secret.APIKeyLastRotatedAt)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			logger.Warn("Affiliate program secret material not found", "id", id)
			return nil, ErrAffiliateProgramSecretNotFound
		}
		logger.Error("Failed to retrieve affiliate program secret material", err, "id", id)
		return nil, err
	}

	// Secret values themselves are intentionally excluded from the log.
	// Presence metadata is safe to emit for operational observability (Policy §17.4).
	logger.Info("Affiliate program secret material retrieved",
		"id", id,
		"has_api_key_encrypted", len(secret.APIKeyEncrypted) > 0,
		"has_api_key_key_id", secret.APIKeyKeyID != nil,
	)

	return &secret, nil
}

// GetAll retrieves affiliate programs with optional soft-delete inclusion and
// offset-based pagination.
//
// limit must be >= 1. offset must be >= 0. Both are validated before the query
// is issued (Policy §2.5, §19.1).
//
// Secret storage fields are intentionally not selected on this standard read
// path (Policy §27.8, §27.11).
//
// Always returns a non-nil slice; callers and JSON serialisers will receive []
// rather than null on zero results (Policy §2.2).
func (m *AffiliateProgramModel) GetAll(ctx context.Context, includeDeleted bool, limit int, offset int) ([]*AffiliateProgram, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("GetAllAffiliatePrograms")

	if limit <= 0 {
		err := errors.New("limit must be greater than zero")
		logger.Error("Validation failed", err, "limit", limit)
		return nil, err
	}
	if offset < 0 {
		err := errors.New("offset must be zero or greater")
		logger.Error("Validation failed", err, "offset", offset)
		return nil, err
	}

	// The query has exactly two bound parameters in all paths ($1 = limit,
	// $2 = offset). The WHERE clause uses IS NULL which needs no placeholder.
	// Placeholders are hardcoded to make the binding unambiguous (Policy §2.2).
	query := `
		SELECT id, name, website, api_endpoint, api_auth_method,
		       deleted_at, created_at, updated_at
		FROM affiliate_programs`

	if !includeDeleted {
		query += " WHERE deleted_at IS NULL"
	}

	query += " ORDER BY created_at DESC LIMIT $1 OFFSET $2"

	rows, err := m.DB.Query(ctx, query, limit, offset)
	if err != nil {
		logger.Error("Query execution failed", err)
		return nil, err
	}
	defer rows.Close()

	// Initialise to a non-nil empty slice so callers and JSON serialisers always
	// receive [] rather than null on zero results (Policy §2.2).
	programs := make([]*AffiliateProgram, 0)

	for rows.Next() {
		var program AffiliateProgram

		err := rows.Scan(
			&program.ID,
			&program.Name,
			&program.Website,
			&program.APIEndpoint,
			&program.APIAuthMethod,
			&program.DeletedAt,
			&program.CreatedAt,
			&program.UpdatedAt,
		)
		if err != nil {
			logger.Error("Row scan failed", err)
			return nil, err
		}

		programs = append(programs, &program)
	}

	if err = rows.Err(); err != nil {
		logger.Error("Row iteration error", err)
		return nil, err
	}

	logger.Info("Retrieved affiliate programs",
		"count", len(programs),
		"include_deleted", includeDeleted,
		"limit", limit,
		"offset", offset,
	)

	return programs, nil
}

// Update replaces the mutable fields of an active affiliate program and writes
// the full canonical persisted form back to the caller's struct on success.
//
// Normalisation and validation are performed on an internal clone so the caller's
// original struct is never modified on failure (Policy §2.6). On success,
// *program is replaced with the clone, which carries normalised fields and
// DB-returned lifecycle timestamps — exactly what was stored.
//
// created_at is immutable after insert and is never written by this method.
// updated_at is owned by the set_updated_at trigger and is read back via
// RETURNING only (Policy §5.3).
//
// Returns ErrAffiliateProgramNotFound when no active row matches the supplied ID
// (row does not exist, or exists but is soft-deleted). Callers must translate
// this to an appropriate HTTP 404 at the handler layer (Policy §2.6).
//
// The caller is responsible for encrypting any plaintext API key before calling
// this method (Policy §27.4–27.5).
func (m *AffiliateProgramModel) Update(ctx context.Context, program *AffiliateProgram) error {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("UpdateAffiliateProgram")

	if program == nil {
		err := errors.New("affiliate program is required")
		logger.Error("Validation failed", err)
		return err
	}
	if program.ID == uuid.Nil {
		err := errors.New("id is required")
		logger.Error("Validation failed", err)
		return err
	}

	// Work on a clone so a not-found or DB error does not mutate caller-owned state.
	persisted := cloneAffiliateProgram(program)
	normalizeAffiliateProgram(persisted)
	if err := validateAffiliateProgram(persisted); err != nil {
		logger.Error("Validation failed", err)
		return err
	}

	// Only mutable business fields are written. created_at is intentionally
	// omitted. updated_at is managed by the set_updated_at trigger and returned
	// via RETURNING so the clone reflects the authoritative DB state on success.
	err := m.DB.QueryRow(ctx,
		`UPDATE affiliate_programs
		 SET name                    = $1,
		     website                 = $2,
		     api_endpoint            = $3,
		     api_auth_method         = $4,
		     api_key_encrypted       = $5,
		     api_key_key_id          = $6,
		     api_key_last_rotated_at = $7
		 WHERE id = $8
		   AND deleted_at IS NULL
		 RETURNING created_at, updated_at, deleted_at`,
		persisted.Name,
		persisted.Website,
		persisted.APIEndpoint,
		persisted.APIAuthMethod,
		persisted.APIKeyEncrypted,
		persisted.APIKeyKeyID,
		persisted.APIKeyLastRotatedAt,
		persisted.ID,
	).Scan(&persisted.CreatedAt, &persisted.UpdatedAt, &persisted.DeletedAt)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			logger.Warn("Affiliate program not found for update", "id", persisted.ID)
			return ErrAffiliateProgramNotFound
		}
		logger.Error("Update affiliate program failed", err)
		return err
	}

	// Replace the caller's struct with the full canonical persisted form only
	// on success. This includes normalised semantic fields and DB-owned timestamps.
	*program = *persisted

	// Secret fields are intentionally excluded from the log (Policy §17.4).
	logger.Info("Update affiliate program successful",
		"id", program.ID,
		"name", program.Name,
		"website", program.Website,
		"api_auth_method", program.APIAuthMethod,
		"updated_at", program.UpdatedAt,
	)

	return nil
}

// SoftDelete logically removes an affiliate program by setting deleted_at to the
// current DB time.
//
// affiliate_programs is a lifecycle-sensitive entity. The schema already encodes
// soft-delete semantics via deleted_at TIMESTAMPTZ and an active-row partial
// index on name WHERE deleted_at IS NULL. Standard reads exclude soft-deleted
// rows by default (V1.5 §15.6, §15.8).
//
// Returns ErrAffiliateProgramNotFound when no active row matches the supplied
// ID. That covers both "row never existed" and "row already soft-deleted".
// Silent no-op on missing rows is not acceptable for this model — the contract
// must be explicit (V1.5 §2.2, §2.6, §28.6).
//
// deleted_at is the canonical soft-delete sentinel in this schema (V1.5 §15.3).
func (m *AffiliateProgramModel) SoftDelete(ctx context.Context, id uuid.UUID) error {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("SoftDeleteAffiliateProgram")

	if id == uuid.Nil {
		err := errors.New("invalid UUID: id is required")
		logger.Error("Validation failed", err)
		return err
	}

	var deletedAt time.Time

	err := m.DB.QueryRow(ctx, `
		UPDATE affiliate_programs
		SET deleted_at = NOW()
		WHERE id = $1
		  AND deleted_at IS NULL
		RETURNING deleted_at
	`, id).Scan(&deletedAt)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			logger.Warn("Affiliate program not found for soft delete", "id", id)
			return ErrAffiliateProgramNotFound
		}
		logger.Error("Soft delete affiliate program failed", err, "id", id)
		return err
	}

	// Secret fields are intentionally excluded from the log (V1.5 §17.4).
	logger.Info("Affiliate program soft delete successful",
		"id", id,
		"deleted_at", deletedAt,
	)

	return nil
}

// Delete permanently removes an affiliate program from the database.
//
// This is the physical purge path and must remain distinct from SoftDelete.
// It must never become an alias for logical deletion (V1.5 §15.7).
// Restricted to administrative use cases where physical removal is explicitly
// required (e.g. GDPR erasure, data correction, test teardown).
//
// Returns ErrAffiliateProgramNotFound when no row exists for the supplied ID,
// whether active or already soft-deleted (V1.5 §2.2, §2.6).
//
// Secret fields are intentionally excluded from the log (V1.5 §17.4).
func (m *AffiliateProgramModel) Delete(ctx context.Context, id uuid.UUID) error {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("DeleteAffiliateProgram")

	if id == uuid.Nil {
		err := errors.New("invalid UUID: id is required")
		logger.Error("Validation failed", err)
		return err
	}

	var deletedID uuid.UUID

	err := m.DB.QueryRow(ctx, `
		DELETE FROM affiliate_programs
		WHERE id = $1
		RETURNING id
	`, id).Scan(&deletedID)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			logger.Warn("Affiliate program not found for hard delete", "id", id)
			return ErrAffiliateProgramNotFound
		}
		logger.Error("Delete affiliate program failed", err, "id", id)
		return err
	}

	// Secret fields are intentionally excluded from the log (V1.5 §17.4).
	logger.Info("Delete affiliate program successful", "id", deletedID)
	return nil
}