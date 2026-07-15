// Package data provides models and database access methods for audit logs and related audit metadata.
//
// sdworkspace/sdbackend/internal/data/audit_logs.go
//
// GTM:
//   Layer: 2.1 Database / Governance Foundation
//   Release Class: SPINE
//   Reason:
//     Audit logs, entity types, and actions are release-critical governance
//     infrastructure. They support audit trail integrity, compliance review,
//     administrative accountability, and dynamic action/entity resolution across
//     the backend. This file must remain production-ready for v1.
//
// SPINE Rule:
//   Keep compiling.
//   Keep production-ready.
//   Preserve immutable audit-log semantics.
//   Preserve append -> archive -> retention purge lifecycle behavior.
//   Preserve dynamic audit action/entity metadata lookup.
//   Block deployment if this file breaks build, persistence, audit integrity,
//   metadata resolution, archival lifecycle, or governance observability.
package data

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/PiccoloMondoC/sdworkspace/sdbackend/internal/logging"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Archival retention constants.
//
// archivingThresholdYears: audit_logs rows older than this are moved to audit_logs_archive.
// archivalPurgeYears:      audit_logs_archive rows older than this are permanently deleted.
const (
	archivingThresholdYears = time.Duration(366*10) * 24 * time.Hour
	archivalPurgeYears      = time.Duration(366*25) * 24 * time.Hour
)

// ─── Models ──────────────────────────────────────────────────────────────────

// AuditLog represents one immutable audit event persisted in audit_logs or
// audit_logs_archive.
//
// Contract notes:
//   - UserID is nullable: system-generated actions carry no user principal.
//   - ActionID is non-nullable: the DB enforces NOT NULL on audit_logs.action_id.
//   - OccurredAt is DB-owned via DEFAULT NOW() with a UTC session guard.
//     Application code must never supply this value; it is read back with RETURNING.
//   - Audit logs do not support record-level soft delete.
//   - Audit logs do not support routine record-level hard delete from the live table.
//     Their lifecycle is append -> archive -> retention purge.
//   - This struct intentionally models only the columns shared by audit_logs and
//     audit_logs_archive. It does not include archived_at. If callers later need
//     archive metadata, introduce an archive-specific struct rather than
//     overloading this one.
type AuditLog struct {
	ID           uuid.UUID  `json:"id"                db:"id"`
	UserID       *uuid.UUID `json:"user_id,omitempty" db:"user_id"`
	ActionID     uuid.UUID  `json:"action_id"         db:"action_id"`
	EntityTypeID uuid.UUID  `json:"entity_type_id"    db:"entity_type_id"`
	EntityID     string     `json:"entity_id"         db:"entity_id"`
	OccurredAt   time.Time  `json:"occurred_at"       db:"occurred_at"`
}

// EntityType represents one row in entity_types.
type EntityType struct {
	ID          uuid.UUID `json:"id"          db:"id"`
	Name        string    `json:"name"        db:"name"`
	Description string    `json:"description" db:"description"`
	CreatedAt   time.Time `json:"created_at"  db:"created_at"`
}

// Action represents one row in actions.
type Action struct {
	ID          uuid.UUID `json:"id"          db:"id"`
	Name        string    `json:"name"        db:"name"`
	Description string    `json:"description" db:"description"`
	CreatedAt   time.Time `json:"created_at"  db:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"  db:"updated_at"`
}

// ─── Model holders ───────────────────────────────────────────────────────────

// AuditLogModel holds the database pool and logger for audit log operations.
type AuditLogModel struct {
	DB     *pgxpool.Pool
	Logger *logging.Logger
}

// EntityTypeModel holds the database pool and logger for entity type operations.
type EntityTypeModel struct {
	DB     *pgxpool.Pool
	Logger *logging.Logger
}

// ActionModel holds the database pool and logger for action operations.
type ActionModel struct {
	DB     *pgxpool.Pool
	Logger *logging.Logger
}

// ─── Internal helpers ─────────────────────────────────────────────────────────

// normalizeAuditMetadataName trims whitespace from name fields in this file
// before validation. Named with a file-specific prefix to avoid collisions with
// any similarly named helpers in other files within the data package.
func normalizeAuditMetadataName(name string) string {
	return strings.TrimSpace(name)
}

// normalizeAuditLogEntityID trims whitespace from entity_id fields in this file
// before validation. Named with a file-specific prefix to avoid collisions with
// any similarly named helpers in other files within the data package.
func normalizeAuditLogEntityID(entityID string) string {
	return strings.TrimSpace(entityID)
}

// ─── AuditLogModel ────────────────────────────────────────────────────────────

// Insert persists a new audit log entry into audit_logs.
//
// OccurredAt is intentionally not supplied by the caller: the database owns this
// timestamp via DEFAULT NOW() with a UTC session guard, which is strictly more
// reliable than an application-supplied value. The authoritative timestamp is
// read back with RETURNING and written into the log struct.
func (m *AuditLogModel) Insert(ctx context.Context, log *AuditLog) error {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("InsertAuditLog")

	if log == nil {
		err := errors.New("audit log is required")
		logger.Error("Validation failed", err)
		return err
	}

	if log.ActionID == uuid.Nil {
		err := errors.New("action_id is required")
		logger.Error("Validation failed", err)
		return err
	}

	if log.EntityTypeID == uuid.Nil {
		err := errors.New("entity_type_id is required")
		logger.Error("Validation failed", err)
		return err
	}

	log.EntityID = normalizeAuditLogEntityID(log.EntityID)
	if log.EntityID == "" {
		err := errors.New("entity_id is required")
		logger.Error("Validation failed", err)
		return err
	}

	if log.ID == uuid.Nil {
		log.ID = uuid.New()
	}

	const query = `
		INSERT INTO audit_logs (
			id,
			user_id,
			action_id,
			entity_type_id,
			entity_id
		)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING occurred_at
	`

	if err := m.DB.QueryRow(
		ctx,
		query,
		log.ID,
		log.UserID,
		log.ActionID,
		log.EntityTypeID,
		log.EntityID,
	).Scan(&log.OccurredAt); err != nil {
		logger.Error("Insert audit log failed", err)
		return err
	}

	logger.Info("Insert audit log successful",
		"id", log.ID,
		"user_id", log.UserID,
		"action_id", log.ActionID,
		"entity_type_id", log.EntityTypeID,
		"entity_id", log.EntityID,
		"occurred_at", log.OccurredAt,
	)

	return nil
}

// GetByID retrieves a single audit log entry by ID.
// Returns ErrAuditLogNotFound when the row does not exist.
func (m *AuditLogModel) GetByID(ctx context.Context, id uuid.UUID) (*AuditLog, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("GetAuditLogByID")

	if id == uuid.Nil {
		err := errors.New("id is required")
		logger.Error("Validation failed", err)
		return nil, err
	}

	var log AuditLog
	err := m.DB.QueryRow(ctx, `
		SELECT id, user_id, action_id, entity_type_id, entity_id, occurred_at
		FROM audit_logs
		WHERE id = $1
	`, id).Scan(
		&log.ID,
		&log.UserID,
		&log.ActionID,
		&log.EntityTypeID,
		&log.EntityID,
		&log.OccurredAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			logger.Warn("Audit log not found", "id", id)
			return nil, ErrAuditLogNotFound
		}
		logger.Error("Query audit log failed", err, "id", id)
		return nil, err
	}

	logger.Info("Retrieved audit log entry",
		"id", log.ID,
		"user_id", log.UserID,
		"action_id", log.ActionID,
		"entity_type_id", log.EntityTypeID,
		"entity_id", log.EntityID,
		"occurred_at", log.OccurredAt,
	)

	return &log, nil
}

// GetByUserID retrieves all audit logs for a given user, ordered newest first.
func (m *AuditLogModel) GetByUserID(ctx context.Context, userID uuid.UUID) ([]*AuditLog, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("GetAuditLogsByUserID")

	if userID == uuid.Nil {
		err := errors.New("user_id is required")
		logger.Error("Validation failed", err)
		return nil, err
	}

	rows, err := m.DB.Query(ctx, `
		SELECT id, user_id, action_id, entity_type_id, entity_id, occurred_at
		FROM audit_logs
		WHERE user_id = $1
		ORDER BY occurred_at DESC
	`, userID)
	if err != nil {
		logger.Error("Query audit logs by user_id failed", err, "user_id", userID)
		return nil, err
	}
	defer rows.Close()

	var logs []*AuditLog
	for rows.Next() {
		var log AuditLog
		if err := rows.Scan(
			&log.ID,
			&log.UserID,
			&log.ActionID,
			&log.EntityTypeID,
			&log.EntityID,
			&log.OccurredAt,
		); err != nil {
			logger.Error("Scanning audit log row failed", err, "user_id", userID)
			return nil, err
		}
		logs = append(logs, &log)
	}

	if err := rows.Err(); err != nil {
		logger.Error("Rows iteration error", err, "user_id", userID)
		return nil, err
	}

	logger.Info("Retrieved audit logs by user_id", "user_id", userID, "count", len(logs))
	return logs, nil
}

// GetByEntity retrieves audit logs for a given entity type and entity ID, ordered newest first.
func (m *AuditLogModel) GetByEntity(ctx context.Context, entityTypeID uuid.UUID, entityID string) ([]*AuditLog, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("GetAuditLogsByEntity")

	if entityTypeID == uuid.Nil {
		err := errors.New("entity_type_id is required")
		logger.Error("Validation failed", err)
		return nil, err
	}

	entityID = normalizeAuditLogEntityID(entityID)
	if entityID == "" {
		err := errors.New("entity_id is required")
		logger.Error("Validation failed", err)
		return nil, err
	}

	rows, err := m.DB.Query(ctx, `
		SELECT id, user_id, action_id, entity_type_id, entity_id, occurred_at
		FROM audit_logs
		WHERE entity_type_id = $1
		  AND entity_id = $2
		ORDER BY occurred_at DESC
	`, entityTypeID, entityID)
	if err != nil {
		logger.Error("Query execution failed", err, "entity_type_id", entityTypeID, "entity_id", entityID)
		return nil, err
	}
	defer rows.Close()

	var logs []*AuditLog
	for rows.Next() {
		var log AuditLog
		if err := rows.Scan(
			&log.ID,
			&log.UserID,
			&log.ActionID,
			&log.EntityTypeID,
			&log.EntityID,
			&log.OccurredAt,
		); err != nil {
			logger.Error("Row scan failed", err)
			return nil, err
		}
		logs = append(logs, &log)
	}

	if err := rows.Err(); err != nil {
		logger.Error("Rows iteration error", err)
		return nil, err
	}

	logger.Info("GetByEntity successful",
		"entity_type_id", entityTypeID,
		"entity_id", entityID,
		"records_found", len(logs),
	)

	return logs, nil
}

// GetByTimeRange retrieves audit logs whose occurred_at falls within [start, end],
// ordered oldest first. Both bounds are required and end must not precede start.
func (m *AuditLogModel) GetByTimeRange(ctx context.Context, start, end time.Time) ([]*AuditLog, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("GetAuditLogsByTimeRange")

	if start.IsZero() || end.IsZero() {
		err := errors.New("start and end times are required")
		logger.Error("Validation failed", err)
		return nil, err
	}

	if end.Before(start) {
		err := errors.New("end time must be after start time")
		logger.Error("Validation failed", err)
		return nil, err
	}

	rows, err := m.DB.Query(ctx, `
		SELECT id, user_id, action_id, entity_type_id, entity_id, occurred_at
		FROM audit_logs
		WHERE occurred_at BETWEEN $1 AND $2
		ORDER BY occurred_at ASC
	`, start, end)
	if err != nil {
		logger.Error("Query audit logs failed", err)
		return nil, err
	}
	defer rows.Close()

	var logs []*AuditLog
	for rows.Next() {
		var log AuditLog
		if err := rows.Scan(
			&log.ID,
			&log.UserID,
			&log.ActionID,
			&log.EntityTypeID,
			&log.EntityID,
			&log.OccurredAt,
		); err != nil {
			logger.Error("Failed to scan audit log row", err)
			return nil, err
		}
		logs = append(logs, &log)
	}

	if err := rows.Err(); err != nil {
		logger.Error("Error iterating audit logs", err)
		return nil, err
	}

	logger.Info("Retrieved audit logs", "count", len(logs), "start", start, "end", end)
	return logs, nil
}

// ─── Archival lifecycle ───────────────────────────────────────────────────────

// ArchiveOldLogs moves audit_logs rows older than archivingThresholdYears into
// audit_logs_archive in a single atomic CTE. The occurred_at timestamp is
// preserved exactly; the archive row additionally records archived_at = NOW().
//
// This method intentionally does not wrap the incoming context with dbTimeout.
// Archival can be a long-running maintenance operation across large tables or
// partitions, so the scheduler or caller must own the deadline and cancellation
// policy. Pass a context with an appropriate timeout from the job runner.
//
// Design notes:
//   - The CTE is atomic: a row is either moved or not; there is no window where
//     it exists in neither table.
//   - The audit_logs table is range-partitioned by occurred_at, so the DELETE
//     will target only the relevant partition(s).
//   - This archival workflow is the canonical lifecycle transition for aged
//     audit records. Audit logs intentionally do not expose SoftDelete or
//     ordinary Delete methods.
func (m *AuditLogModel) ArchiveOldLogs(ctx context.Context) error {
	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("ArchiveOldLogs")

	thresholdTime := time.Now().UTC().Add(-archivingThresholdYears)
	logger.Info("Starting audit log archival", "threshold_time", thresholdTime)

	const query = `
		WITH moved_rows AS (
			DELETE FROM audit_logs
			WHERE occurred_at < $1
			RETURNING id, user_id, action_id, entity_type_id, entity_id, occurred_at
		)
		INSERT INTO audit_logs_archive (
			id,
			user_id,
			action_id,
			entity_type_id,
			entity_id,
			occurred_at,
			archived_at
		)
		SELECT
			id,
			user_id,
			action_id,
			entity_type_id,
			entity_id,
			occurred_at,
			NOW()
		FROM moved_rows
	`

	result, err := m.DB.Exec(ctx, query, thresholdTime)
	if err != nil {
		logger.Error("Audit log archival failed", err)
		return err
	}

	logger.Info("Audit log archival completed successfully",
		"threshold_time", thresholdTime,
		"rows_archived", result.RowsAffected(),
	)

	return nil
}

// PurgeArchivedLogs permanently deletes rows from audit_logs_archive that were
// archived more than archivalPurgeYears ago. This is a hard delete; ensure
// applicable retention and compliance requirements are met before scheduling.
//
// This method intentionally does not wrap the incoming context with dbTimeout.
// Purge can be a long-running maintenance operation, so the scheduler or caller
// must own the deadline and cancellation policy. Pass a context with an
// appropriate timeout from the job runner.
func (m *AuditLogModel) PurgeArchivedLogs(ctx context.Context) error {
	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("PurgeArchivedLogs")

	thresholdTime := time.Now().UTC().Add(-archivalPurgeYears)
	logger.Info("Starting audit log purge", "threshold_time", thresholdTime)

	result, err := m.DB.Exec(ctx, `
		DELETE FROM audit_logs_archive
		WHERE archived_at < $1
	`, thresholdTime)
	if err != nil {
		logger.Error("Audit log purge failed", err)
		return err
	}

	logger.Info("Audit log purge completed successfully",
		"threshold_time", thresholdTime,
		"rows_deleted", result.RowsAffected(),
	)

	return nil
}

// RestoreArchivedLogs moves the identified rows from audit_logs_archive back
// into audit_logs, preserving the original occurred_at timestamp. This is
// intended for investigation or compliance recall workflows, not routine use.
//
// Important:
//   - occurred_at is restored explicitly to preserve the original historical
//     event time. Relying on DEFAULT NOW() here would corrupt the audit record.
//   - The CTE is atomic: each row is either restored or left in the archive table.
//   - Operators must verify partition coverage before large restores of old rows.
//     If restored rows fall outside the defined partition ranges (audit_logs_y2025,
//     audit_logs_y2026, etc.), PostgreSQL will route them to audit_logs_default.
//     This is correct behaviour but should be anticipated for bulk restores of
//     historical data.
func (m *AuditLogModel) RestoreArchivedLogs(ctx context.Context, ids []uuid.UUID) error {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("RestoreArchivedLogs")

	if len(ids) == 0 {
		err := errors.New("at least one archived audit log id is required")
		logger.Error("Validation failed", err)
		return err
	}

	logger.Info("Starting audit log restoration", "count", len(ids))
	logger.Warn("Verify audit_logs partition coverage before bulk restore; out-of-range occurred_at values may route to audit_logs_default")

	const query = `
		WITH restored_rows AS (
			DELETE FROM audit_logs_archive
			WHERE id = ANY($1)
			RETURNING id, user_id, action_id, entity_type_id, entity_id, occurred_at
		)
		INSERT INTO audit_logs (
			id,
			user_id,
			action_id,
			entity_type_id,
			entity_id,
			occurred_at
		)
		SELECT
			id,
			user_id,
			action_id,
			entity_type_id,
			entity_id,
			occurred_at
		FROM restored_rows
	`

	result, err := m.DB.Exec(ctx, query, ids)
	if err != nil {
		logger.Error("Audit log restoration failed", err, "count", len(ids))
		return err
	}

	if result.RowsAffected() == 0 {
		logger.Warn("No archived audit logs matched the requested ids", "count", len(ids))
		return ErrArchivedAuditLogNotFound
	}

	logger.Info("Audit log restoration completed successfully",
		"count_requested", len(ids),
		"count_restored", result.RowsAffected(),
	)

	return nil
}

// ─── Archive read paths ───────────────────────────────────────────────────────

// GetArchivedByEntity retrieves rows from audit_logs_archive for a given entity,
// ordered newest first.
//
// TODO: add pagination or result limits before exposing this through an API or
// using it against large archive datasets. Archive tables can grow very large;
// unbounded reads here will become a performance problem over time.
func (m *AuditLogModel) GetArchivedByEntity(ctx context.Context, entityTypeID uuid.UUID, entityID string) ([]*AuditLog, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("GetArchivedAuditLogsByEntity")

	if entityTypeID == uuid.Nil {
		err := errors.New("entity_type_id is required")
		logger.Error("Validation failed", err)
		return nil, err
	}

	entityID = normalizeAuditLogEntityID(entityID)
	if entityID == "" {
		err := errors.New("entity_id is required")
		logger.Error("Validation failed", err)
		return nil, err
	}

	rows, err := m.DB.Query(ctx, `
		SELECT id, user_id, action_id, entity_type_id, entity_id, occurred_at
		FROM audit_logs_archive
		WHERE entity_type_id = $1
		  AND entity_id = $2
		ORDER BY occurred_at DESC
	`, entityTypeID, entityID)
	if err != nil {
		logger.Error("Query archived audit logs failed", err)
		return nil, err
	}
	defer rows.Close()

	var logs []*AuditLog
	for rows.Next() {
		var log AuditLog
		if err := rows.Scan(
			&log.ID,
			&log.UserID,
			&log.ActionID,
			&log.EntityTypeID,
			&log.EntityID,
			&log.OccurredAt,
		); err != nil {
			logger.Error("Row scan failed", err)
			return nil, err
		}
		logs = append(logs, &log)
	}

	if err := rows.Err(); err != nil {
		logger.Error("Row iteration failed", err)
		return nil, err
	}

	logger.Info("Retrieved archived audit logs successfully",
		"entity_type_id", entityTypeID,
		"entity_id", entityID,
		"total_logs", len(logs),
	)

	return logs, nil
}

// GetArchivedByTimeRange retrieves rows from audit_logs_archive whose occurred_at
// falls within [start, end], ordered oldest first. Both bounds are required and
// end must not precede start.
//
// TODO: add pagination or result limits before exposing this through an API or
// using it against large archive datasets. Archive tables can grow very large;
// unbounded reads here will become a performance problem over time.
func (m *AuditLogModel) GetArchivedByTimeRange(ctx context.Context, start, end time.Time) ([]*AuditLog, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("GetArchivedAuditLogsByTimeRange")

	if start.IsZero() || end.IsZero() {
		err := errors.New("start and end times are required")
		logger.Error("Validation failed", err)
		return nil, err
	}

	if start.After(end) {
		err := errors.New("start time cannot be after end time")
		logger.Error("Validation failed", err, "start", start, "end", end)
		return nil, err
	}

	rows, err := m.DB.Query(ctx, `
		SELECT id, user_id, action_id, entity_type_id, entity_id, occurred_at
		FROM audit_logs_archive
		WHERE occurred_at >= $1
		  AND occurred_at <= $2
		ORDER BY occurred_at ASC
	`, start, end)
	if err != nil {
		logger.Error("Query execution failed", err, "start", start, "end", end)
		return nil, err
	}
	defer rows.Close()

	var logs []*AuditLog
	for rows.Next() {
		var log AuditLog
		if err := rows.Scan(
			&log.ID,
			&log.UserID,
			&log.ActionID,
			&log.EntityTypeID,
			&log.EntityID,
			&log.OccurredAt,
		); err != nil {
			logger.Error("Row scan failed", err)
			return nil, err
		}
		logs = append(logs, &log)
	}

	if err := rows.Err(); err != nil {
		logger.Error("Row iteration error", err)
		return nil, err
	}

	logger.Info("Retrieved archived audit logs successfully",
		"start", start,
		"end", end,
		"count", len(logs),
	)

	return logs, nil
}

// ─── EntityTypeModel ──────────────────────────────────────────────────────────

// Insert persists a new entity type into entity_types.
// created_at is DB-owned and returned via RETURNING.
func (m *EntityTypeModel) Insert(ctx context.Context, entityType *EntityType) error {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("InsertEntityType")

	if entityType == nil {
		err := errors.New("entity type is required")
		logger.Error("Validation failed", err)
		return err
	}

	entityType.Name = normalizeAuditMetadataName(entityType.Name)
	entityType.Description = strings.TrimSpace(entityType.Description)

	if entityType.Name == "" {
		err := errors.New("name is required")
		logger.Error("Validation failed", err)
		return err
	}

	if entityType.Description == "" {
		err := errors.New("description is required")
		logger.Error("Validation failed", err)
		return err
	}

	if entityType.ID == uuid.Nil {
		entityType.ID = uuid.New()
	}

	if err := m.DB.QueryRow(ctx, `
		INSERT INTO entity_types (id, name, description)
		VALUES ($1, $2, $3)
		RETURNING created_at
	`, entityType.ID, entityType.Name, entityType.Description).Scan(&entityType.CreatedAt); err != nil {
		logger.Error("Insert entity type failed", err)
		return err
	}

	logger.Info("Insert entity type successful",
		"id", entityType.ID,
		"name", entityType.Name,
		"description", entityType.Description,
		"created_at", entityType.CreatedAt,
	)

	return nil
}

// CreateIfNotExists inserts an entity type if no row with that name exists.
// Returns the ID of the existing or newly created row.
//
// The upsert uses a no-op UPDATE (SET name = entity_types.name) to force the
// RETURNING clause without mutating any data on conflict.
func (m *EntityTypeModel) CreateIfNotExists(ctx context.Context, name, description string) (uuid.UUID, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("CreateEntityTypeIfNotExists")

	name = normalizeAuditMetadataName(name)
	description = strings.TrimSpace(description)

	if name == "" {
		err := errors.New("entity type name is required")
		logger.Error("Validation failed", err)
		return uuid.Nil, err
	}

	if description == "" {
		err := errors.New("entity type description is required")
		logger.Error("Validation failed", err)
		return uuid.Nil, err
	}

	var id uuid.UUID
	err := m.DB.QueryRow(ctx, `
		INSERT INTO entity_types (name, description)
		VALUES ($1, $2)
		ON CONFLICT (name) DO UPDATE
		SET name = entity_types.name
		RETURNING id
	`, name, description).Scan(&id)
	if err != nil {
		logger.Error("CreateIfNotExists query failed", err)
		return uuid.Nil, err
	}

	logger.Info("Created or fetched entity type", "name", name, "id", id)
	return id, nil
}

// GetByID retrieves an entity type by primary key.
// Returns ErrEntityTypeNotFound when the row does not exist.
func (m *EntityTypeModel) GetByID(ctx context.Context, id uuid.UUID) (*EntityType, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("GetEntityTypeByID")

	if id == uuid.Nil {
		err := errors.New("id is required")
		logger.Error("Validation failed", err)
		return nil, err
	}

	entityType := &EntityType{}
	err := m.DB.QueryRow(ctx, `
		SELECT id, name, description, created_at
		FROM entity_types
		WHERE id = $1
	`, id).Scan(
		&entityType.ID,
		&entityType.Name,
		&entityType.Description,
		&entityType.CreatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			logger.Warn("Entity type not found", "id", id)
			return nil, ErrEntityTypeNotFound
		}
		logger.Error("Query failed", err, "id", id)
		return nil, err
	}

	logger.Info("Entity type retrieved successfully",
		"id", entityType.ID,
		"name", entityType.Name,
		"description", entityType.Description,
		"created_at", entityType.CreatedAt,
	)

	return entityType, nil
}

// GetByName retrieves an entity type by name (case-insensitive).
// Returns ErrEntityTypeNotFound when the row does not exist.
func (m *EntityTypeModel) GetByName(ctx context.Context, name string) (*EntityType, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("GetEntityTypeByName")

	name = normalizeAuditMetadataName(name)
	if name == "" {
		err := errors.New("entity type name is required")
		logger.Error("Validation failed", err)
		return nil, err
	}

	entityType := &EntityType{}
	err := m.DB.QueryRow(ctx, `
		SELECT id, name, description, created_at
		FROM entity_types
		WHERE LOWER(name) = LOWER($1)
	`, name).Scan(
		&entityType.ID,
		&entityType.Name,
		&entityType.Description,
		&entityType.CreatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			logger.Warn("Entity type not found", "name", name)
			return nil, ErrEntityTypeNotFound
		}
		logger.Error("Query failed", err, "name", name)
		return nil, err
	}

	logger.Info("Entity type retrieved successfully",
		"id", entityType.ID,
		"name", entityType.Name,
		"description", entityType.Description,
		"created_at", entityType.CreatedAt,
	)

	return entityType, nil
}

// GetAll retrieves all entity types ordered by name.
//
// TODO: add limit/offset or keyset pagination before exposing this through an
// API. Unbounded reads are acceptable for current seed/admin usage only.
func (m *EntityTypeModel) GetAll(ctx context.Context) ([]*EntityType, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("GetAllEntityTypes")

	rows, err := m.DB.Query(ctx, `
		SELECT id, name, description, created_at
		FROM entity_types
		ORDER BY name ASC
	`)
	if err != nil {
		logger.Error("Failed to retrieve entity types", err)
		return nil, err
	}
	defer rows.Close()

	var entityTypes []*EntityType
	for rows.Next() {
		var entityType EntityType
		if err := rows.Scan(
			&entityType.ID,
			&entityType.Name,
			&entityType.Description,
			&entityType.CreatedAt,
		); err != nil {
			logger.Error("Failed to scan entity type row", err)
			return nil, err
		}
		entityTypes = append(entityTypes, &entityType)
	}

	if err := rows.Err(); err != nil {
		logger.Error("Error iterating over entity types", err)
		return nil, err
	}

	logger.Info("Successfully retrieved entity types", "count", len(entityTypes))
	return entityTypes, nil
}

// Update replaces the name and description of an existing entity type.
// created_at is returned via RETURNING so the struct reflects the persisted value.
// Returns ErrEntityTypeNotFound when no row matches the given ID.
func (m *EntityTypeModel) Update(ctx context.Context, entityType *EntityType) error {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("UpdateEntityType")

	if entityType == nil {
		err := errors.New("entity type is required")
		logger.Error("Validation failed", err)
		return err
	}

	if entityType.ID == uuid.Nil {
		err := errors.New("id is required for update")
		logger.Error("Validation failed", err)
		return err
	}

	entityType.Name = normalizeAuditMetadataName(entityType.Name)
	entityType.Description = strings.TrimSpace(entityType.Description)

	if entityType.Name == "" {
		err := errors.New("name is required")
		logger.Error("Validation failed", err)
		return err
	}

	if entityType.Description == "" {
		err := errors.New("description is required")
		logger.Error("Validation failed", err)
		return err
	}

	err := m.DB.QueryRow(ctx, `
		UPDATE entity_types
		SET name        = $1,
		    description = $2
		WHERE id = $3
		RETURNING created_at
	`, entityType.Name, entityType.Description, entityType.ID).Scan(&entityType.CreatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			logger.Warn("Entity type not found for update", "id", entityType.ID)
			return ErrEntityTypeNotFound
		}
		logger.Error("Update entity type failed", err)
		return err
	}

	logger.Info("Update entity type successful",
		"id", entityType.ID,
		"name", entityType.Name,
		"description", entityType.Description,
		"created_at", entityType.CreatedAt,
	)

	return nil
}

// Delete permanently removes an entity type by primary key.
// Returns ErrEntityTypeNotFound when no row matches the given ID.
func (m *EntityTypeModel) Delete(ctx context.Context, id uuid.UUID) error {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("DeleteEntityType")

	if id == uuid.Nil {
		err := errors.New("entity type id is required")
		logger.Error("Validation failed", err)
		return err
	}

	var deletedID uuid.UUID
	err := m.DB.QueryRow(ctx, `
		DELETE FROM entity_types
		WHERE id = $1
		RETURNING id
	`, id).Scan(&deletedID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			logger.Warn("Entity type not found for deletion", "id", id)
			return ErrEntityTypeNotFound
		}
		logger.Error("Delete entity type failed", err, "id", id)
		return err
	}

	logger.Info("Delete entity type successful", "id", deletedID)
	return nil
}

// ─── ActionModel ──────────────────────────────────────────────────────────────

// Insert persists a new action into actions.
// created_at and updated_at are DB-owned and returned via RETURNING.
func (m *ActionModel) Insert(ctx context.Context, action *Action) error {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("InsertAction")

	if action == nil {
		err := errors.New("action is required")
		logger.Error("Validation failed", err)
		return err
	}

	action.Name = normalizeAuditMetadataName(action.Name)
	action.Description = strings.TrimSpace(action.Description)

	if action.Name == "" {
		err := errors.New("name is required")
		logger.Error("Validation failed", err)
		return err
	}

	if action.Description == "" {
		err := errors.New("description is required")
		logger.Error("Validation failed", err)
		return err
	}

	if action.ID == uuid.Nil {
		action.ID = uuid.New()
	}

	err := m.DB.QueryRow(ctx, `
		INSERT INTO actions (id, name, description)
		VALUES ($1, $2, $3)
		RETURNING created_at, updated_at
	`, action.ID, action.Name, action.Description).Scan(&action.CreatedAt, &action.UpdatedAt)
	if err != nil {
		logger.Error("Insert action failed", err)
		return err
	}

	logger.Info("Insert action successful",
		"id", action.ID,
		"name", action.Name,
		"description", action.Description,
		"created_at", action.CreatedAt,
		"updated_at", action.UpdatedAt,
	)

	return nil
}

// CreateIfNotExists inserts an action if no row with that name exists.
// Returns the ID of the existing or newly created row.
//
// The upsert uses a no-op UPDATE (SET name = actions.name) to force the
// RETURNING clause without mutating any data on conflict.
func (m *ActionModel) CreateIfNotExists(ctx context.Context, name, description string) (uuid.UUID, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("CreateActionIfNotExists")

	name = normalizeAuditMetadataName(name)
	description = strings.TrimSpace(description)

	if name == "" {
		err := errors.New("action name is required")
		logger.Error("Validation failed", err)
		return uuid.Nil, err
	}

	if description == "" {
		err := errors.New("action description is required")
		logger.Error("Validation failed", err)
		return uuid.Nil, err
	}

	var id uuid.UUID
	err := m.DB.QueryRow(ctx, `
		INSERT INTO actions (name, description)
		VALUES ($1, $2)
		ON CONFLICT (name) DO UPDATE
		SET name = actions.name
		RETURNING id
	`, name, description).Scan(&id)
	if err != nil {
		logger.Error("CreateIfNotExists query failed", err)
		return uuid.Nil, err
	}

	logger.Info("Created or fetched action", "name", name, "id", id)
	return id, nil
}

// GetByID retrieves an action by primary key.
// Returns ErrActionNotFound when the row does not exist.
func (m *ActionModel) GetByID(ctx context.Context, id uuid.UUID) (*Action, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("GetActionByID")

	if id == uuid.Nil {
		err := errors.New("id is required")
		logger.Error("Validation failed", err)
		return nil, err
	}

	action := &Action{}
	err := m.DB.QueryRow(ctx, `
		SELECT id, name, description, created_at, updated_at
		FROM actions
		WHERE id = $1
	`, id).Scan(
		&action.ID,
		&action.Name,
		&action.Description,
		&action.CreatedAt,
		&action.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			logger.Warn("Action not found", "id", id)
			return nil, ErrActionNotFound
		}
		logger.Error("Query failed", err, "id", id)
		return nil, err
	}

	logger.Info("GetByID successful",
		"id", action.ID,
		"name", action.Name,
		"description", action.Description,
		"created_at", action.CreatedAt,
		"updated_at", action.UpdatedAt,
	)

	return action, nil
}

// GetByName retrieves an action by name (case-insensitive).
// Returns ErrActionNotFound when the row does not exist.
func (m *ActionModel) GetByName(ctx context.Context, name string) (*Action, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("GetActionByName")

	name = normalizeAuditMetadataName(name)
	if name == "" {
		err := errors.New("action name is required")
		logger.Error("Validation failed", err)
		return nil, err
	}

	action := &Action{}
	err := m.DB.QueryRow(ctx, `
		SELECT id, name, description, created_at, updated_at
		FROM actions
		WHERE LOWER(name) = LOWER($1)
	`, name).Scan(
		&action.ID,
		&action.Name,
		&action.Description,
		&action.CreatedAt,
		&action.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			logger.Warn("Action not found", "name", name)
			return nil, ErrActionNotFound
		}
		logger.Error("Query failed", err, "name", name)
		return nil, err
	}

	logger.Info("GetByName successful",
		"id", action.ID,
		"name", action.Name,
		"description", action.Description,
		"created_at", action.CreatedAt,
		"updated_at", action.UpdatedAt,
	)

	return action, nil
}

// GetAll retrieves all actions ordered by name.
//
// TODO: add limit/offset or keyset pagination before exposing this through an
// API. Unbounded reads are acceptable for current seed/admin usage only.
func (m *ActionModel) GetAll(ctx context.Context) ([]*Action, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("GetAllActions")

	rows, err := m.DB.Query(ctx, `
		SELECT id, name, description, created_at, updated_at
		FROM actions
		ORDER BY name ASC
	`)
	if err != nil {
		logger.Error("Failed to retrieve actions", err)
		return nil, err
	}
	defer rows.Close()

	var actions []*Action
	for rows.Next() {
		var action Action
		if err := rows.Scan(
			&action.ID,
			&action.Name,
			&action.Description,
			&action.CreatedAt,
			&action.UpdatedAt,
		); err != nil {
			logger.Error("Failed to scan action row", err)
			return nil, err
		}
		actions = append(actions, &action)
	}

	if err := rows.Err(); err != nil {
		logger.Error("Error iterating over action rows", err)
		return nil, err
	}

	logger.Info("Retrieved all actions successfully", "count", len(actions))
	return actions, nil
}

// Update replaces the name and description of an existing action.
// The DB trigger keeps updated_at current; both created_at and updated_at are
// returned via RETURNING so the struct reflects persisted values.
// Returns ErrActionNotFound when no row matches the given ID.
func (m *ActionModel) Update(ctx context.Context, action *Action) error {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("UpdateAction")

	if action == nil {
		err := errors.New("action is required")
		logger.Error("Validation failed", err)
		return err
	}

	if action.ID == uuid.Nil {
		err := errors.New("action id is required")
		logger.Error("Validation failed", err)
		return err
	}

	action.Name = normalizeAuditMetadataName(action.Name)
	action.Description = strings.TrimSpace(action.Description)

	if action.Name == "" {
		err := errors.New("name is required")
		logger.Error("Validation failed", err)
		return err
	}

	if action.Description == "" {
		err := errors.New("description is required")
		logger.Error("Validation failed", err)
		return err
	}

	err := m.DB.QueryRow(ctx, `
		UPDATE actions
		SET name        = $1,
		    description = $2
		WHERE id = $3
		RETURNING created_at, updated_at
	`, action.Name, action.Description, action.ID).Scan(&action.CreatedAt, &action.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			logger.Warn("Update action failed - not found", "id", action.ID)
			return ErrActionNotFound
		}
		logger.Error("Update action failed", err)
		return err
	}

	logger.Info("Update action successful",
		"id", action.ID,
		"name", action.Name,
		"description", action.Description,
		"updated_at", action.UpdatedAt,
	)

	return nil
}

// Delete permanently removes an action by primary key.
// Returns ErrActionNotFound when no row matches the given ID.
func (m *ActionModel) Delete(ctx context.Context, id uuid.UUID) error {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("DeleteAction")

	if id == uuid.Nil {
		err := errors.New("action id is required")
		logger.Error("Validation failed", err)
		return err
	}

	var deletedID uuid.UUID
	err := m.DB.QueryRow(ctx, `
		DELETE FROM actions
		WHERE id = $1
		RETURNING id
	`, id).Scan(&deletedID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			logger.Warn("Action not found for deletion", "id", id)
			return ErrActionNotFound
		}
		logger.Error("Delete action failed", err, "id", id)
		return err
	}

	logger.Info("Delete action successful", "id", deletedID)
	return nil
}