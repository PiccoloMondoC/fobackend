// Package data provides models and database access methods for administrative
// governance: the Root Super Admin, administrative appointments, the Root
// succession ledger, the administrative account directory, and governance
// audit records.
//
// focodebase/fobackend/internal/data/administrative_governance.go
//
// GTM:
//
//	Layer: 2.2 Identity / Auth Domain (Administrative Governance)
//	Release Class: SPINE
//	Reason:
//	  Administrative governance is release-critical security infrastructure.
//	  This file owns persistence for root_super_admin, root_succession_events,
//	  and administrative_appointments, plus the transaction-scoped governance
//	  lock (the users row of every participant, locked FOR UPDATE in ascending
//	  ID order) and the in-transaction governance audit writer.
//
//	  It does NOT own user_role_assignments or roles writes; those remain owned
//	  by RoleModel and are composed with this model by the governance service
//	  inside one caller-owned transaction. Reads here may join roles and
//	  user_role_assignments for presentation only.
//
// Lock order (every governance transaction):
//
//	1. pg_advisory_xact_lock(GovernanceAdvisoryLockKey)  all control-plane mutations
//	2. users rows of all participants, FOR UPDATE, ascending UUID byte order
//	3. administrative_appointments (written only under (2))
//	4. user_role_assignments (via RoleModel Tx primitives)
//	5. activation_tokens (account closure only; preserves users-first order)
//	6. root_super_admin / root_succession_events (bootstrap only)
//	7. audit_logs (append)
//
// SPINE Rule:
//
//	Keep compiling.
//	Keep production-ready.
//	Preserve exactly one protected Root Super Admin.
//	Preserve appointment state as the only home of administrative suspension.
//	Preserve deterministic lock ordering.
//	Preserve in-transaction auditing of successful governance mutations.
//	Do not store secrets, passwords, tokens, or search text in audit context.
//	Block deployment if this file breaks build, Root protection, governance
//	lock ordering, or governance audit integrity.
package data

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/PiccoloMondoC/focodebase/fobackend/internal/logging"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Canonical role names. RoleNameSuperAdmin and RoleNameAdmin are the single
// authoritative definition of internal administrative roles; they must match
// the literals used by the governance triggers in database.go.
const (
	RoleNameSuperAdmin = "super_admin"
	RoleNameAdmin      = "admin"
	RoleNameConsumer   = "consumer"
)

// GovernanceAdvisoryLockKey serializes control-plane mutations across API
// instances. User locks remain the common boundary with public auth workflows.
const GovernanceAdvisoryLockKey int64 = 7301195001

// Governance audit vocabulary. Every name here is seeded in dataseed.go; an
// unseeded name makes InsertGovernanceAuditTx fail rather than silently skip.
const (
	GovernanceEntityAdministrator  = "administrator"
	GovernanceEntityRootSuperAdmin = "root_super_admin"
	GovernanceEntityUsers          = "users"

	GovernanceOutcomeSucceeded = "succeeded"
	GovernanceOutcomeDenied    = "denied"
)

// AdministrativeLevel is an appointment level.
type AdministrativeLevel string

const (
	AdministrativeLevelNone       AdministrativeLevel = ""
	AdministrativeLevelAdmin      AdministrativeLevel = "admin"
	AdministrativeLevelSuperAdmin AdministrativeLevel = "super_admin"
)

// Valid reports whether l is an appointable level.
func (l AdministrativeLevel) Valid() bool {
	return l == AdministrativeLevelAdmin || l == AdministrativeLevelSuperAdmin
}

// RoleName returns the governance role that carries the level.
func (l AdministrativeLevel) RoleName() string {
	return string(l)
}

// AppointmentStatus is the lifecycle state of an appointment.
type AppointmentStatus string

const (
	AppointmentStatusNone      AppointmentStatus = ""
	AppointmentStatusActive    AppointmentStatus = "active"
	AppointmentStatusSuspended AppointmentStatus = "suspended"
	AppointmentStatusRevoked   AppointmentStatus = "revoked"
)

// IsAdministrativeRoleName is the single canonical classifier for internal
// administrative roles. The API layer's isInternalRole delegates here.
func IsAdministrativeRoleName(name string) bool {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case RoleNameSuperAdmin, RoleNameAdmin:
		return true
	default:
		return false
	}
}

// AdministrativeAppointment is one administrative_appointments row.
type AdministrativeAppointment struct {
	UserID          uuid.UUID           `json:"user_id"`
	Level           AdministrativeLevel `json:"level"`
	Status          AppointmentStatus   `json:"status"`
	AppointedBy     *uuid.UUID          `json:"appointed_by,omitempty"`
	AppointedAt     time.Time           `json:"appointed_at"`
	StatusChangedBy *uuid.UUID          `json:"status_changed_by,omitempty"`
	StatusChangedAt time.Time           `json:"status_changed_at"`
	StatusReason    *string             `json:"status_reason,omitempty"`
	CreatedAt       time.Time           `json:"created_at"`
	UpdatedAt       time.Time           `json:"updated_at"`
}

const appointmentColumns = `
	user_id,
	level,
	status,
	appointed_by,
	appointed_at,
	status_changed_by,
	status_changed_at,
	status_reason,
	created_at,
	updated_at
`

func scanAppointment(row scannableRow) (*AdministrativeAppointment, error) {
	var a AdministrativeAppointment
	var level, status string
	if err := row.Scan(
		&a.UserID,
		&level,
		&status,
		&a.AppointedBy,
		&a.AppointedAt,
		&a.StatusChangedBy,
		&a.StatusChangedAt,
		&a.StatusReason,
		&a.CreatedAt,
		&a.UpdatedAt,
	); err != nil {
		return nil, err
	}
	a.Level = AdministrativeLevel(level)
	a.Status = AppointmentStatus(status)
	return &a, nil
}

// GovernanceSubject is the authoritative governance view of one account,
// read while that account's users row is locked (or as a snapshot for
// presentation when read outside a transaction).
type GovernanceSubject struct {
 PasswordHash string // never serialized; credential version checked after step-up

	UserID      uuid.UUID
	Email       string
	IsActive    bool
	IsDeleted   bool
	IsRoot      bool
	Appointment *AdministrativeAppointment
}

// AppointmentLevel returns the appointment level regardless of status.
func (s *GovernanceSubject) AppointmentLevel() AdministrativeLevel {
	if s == nil || s.Appointment == nil {
		return AdministrativeLevelNone
	}
	return s.Appointment.Level
}

// AppointmentStatus returns the appointment status, or none.
func (s *GovernanceSubject) AppointmentStatus() AppointmentStatus {
	if s == nil || s.Appointment == nil {
		return AppointmentStatusNone
	}
	return s.Appointment.Status
}

// ActiveLevel returns the level under which the account currently exercises
// administrative authority: an active appointment on a live account.
func (s *GovernanceSubject) ActiveLevel() AdministrativeLevel {
	if s == nil || s.IsDeleted || !s.IsActive || s.Appointment == nil {
		return AdministrativeLevelNone
	}
	if s.Appointment.Status != AppointmentStatusActive {
		return AdministrativeLevelNone
	}
	return s.Appointment.Level
}

// AdministratorRecord is the directory projection of an appointment.
type AdministratorRecord struct {
	UserID           uuid.UUID           `json:"user_id"`
	Email            string              `json:"email"`
	AccountActive    bool                `json:"account_active"`
	IsRoot           bool                `json:"is_root"`
	Level            AdministrativeLevel `json:"level"`
	Status           AppointmentStatus   `json:"status"`
	AppointedAt      time.Time           `json:"appointed_at"`
	AppointedByEmail *string             `json:"appointed_by_email"`
	StatusChangedAt  time.Time           `json:"status_changed_at"`
	StatusReason     *string             `json:"status_reason"`
}

// AccountRecord is the administrative account-directory projection. It is
// deliberately minimal: no profile data, no provider identifiers, no hashes.
type AccountRecord struct {
	UserID              uuid.UUID           `json:"user_id"`
	Email               string              `json:"email"`
	AccountActive       bool                `json:"account_active"`
	CreatedAt           time.Time           `json:"created_at"`
	PrimaryRole         string              `json:"primary_role"`
	IsRoot              bool                `json:"is_root"`
	AdministrativeLevel AdministrativeLevel `json:"administrative_level"`
	AppointmentStatus   AppointmentStatus   `json:"appointment_status"`
}

// GovernanceHistoryEntry is one governance audit record about an account.
type GovernanceHistoryEntry struct {
	OccurredAt     time.Time `json:"occurred_at"`
	Action         string    `json:"action"`
	Outcome        string    `json:"outcome"`
	ActorEmail     *string   `json:"actor_email"`
	Reason         *string   `json:"reason"`
	PreviousLevel  *string   `json:"previous_level"`
	NewLevel       *string   `json:"new_level"`
	PreviousStatus *string   `json:"previous_status"`
	NewStatus      *string   `json:"new_status"`
	DenialCode     *string   `json:"denial_code"`
}

// GovernanceAuditContext is the only shape ever written to audit_logs.context
// by governance code. Fixed fields keep secrets and free-form data out.
type GovernanceAuditContext struct {
	Operation           string `json:"operation,omitempty"`
	Reason              string `json:"reason,omitempty"`
	PreviousLevel       string `json:"previous_level,omitempty"`
	NewLevel            string `json:"new_level,omitempty"`
	PreviousStatus      string `json:"previous_status,omitempty"`
	NewStatus           string `json:"new_status,omitempty"`
	DenialCode          string `json:"denial_code,omitempty"`
	Mechanism           string `json:"mechanism,omitempty"`
	SuccessionGeneration int   `json:"succession_generation,omitempty"`
}

// GovernanceAuditEntry describes one audit record.
type GovernanceAuditEntry struct {
	ActorID    *uuid.UUID
	Action     string
	EntityType string
	EntityID   string
	Outcome    string
	Context    GovernanceAuditContext
}

// AppointmentChange is the input to PutAppointmentTx.
type AppointmentChange struct {
	UserID  uuid.UUID
	Level   AdministrativeLevel
	Status  AppointmentStatus
	ActorID *uuid.UUID
	Reason  *string
	// NewTerm marks an appointment act (appoint, promote): appointed_by and
	// appointed_at are reset to this actor and now. Status changes and
	// demotion keep the original appointment provenance.
	NewTerm bool
}

// governanceExecer is satisfied by both pgx.Tx and *pgxpool.Pool.
type governanceExecer interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
}

// AdministrativeGovernanceModel owns governance persistence.
type AdministrativeGovernanceModel struct {
	DB     *pgxpool.Pool
	Logger *logging.Logger
}

// NewAdministrativeGovernanceModel constructs the model from the shared pool.
// It is constructed by the service layer from data.Models.DB so that this
// slice does not depend on changes to the Models registry.
func NewAdministrativeGovernanceModel(db *pgxpool.Pool, logger *logging.Logger) *AdministrativeGovernanceModel {
	return &AdministrativeGovernanceModel{DB: db, Logger: logger}
}

// sortedUniqueIDs returns ids de-duplicated and ordered by byte value, the
// canonical governance lock order.
func sortedUniqueIDs(ids []uuid.UUID) []uuid.UUID {
	seen := make(map[uuid.UUID]struct{}, len(ids))
	out := make([]uuid.UUID, 0, len(ids))
	for _, id := range ids {
		if id == uuid.Nil {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	sort.Slice(out, func(i, j int) bool {
		return bytes.Compare(out[i][:], out[j][:]) < 0
	})
	return out
}

// SortedGovernanceLockOrder exposes the lock order for tests.
func SortedGovernanceLockOrder(ids ...uuid.UUID) []uuid.UUID {
	return sortedUniqueIDs(ids)
}

// AcquireGovernanceAdvisoryLockTx takes the transaction-scoped advisory lock
// used by bootstrap and reconciliation.
func (m *AdministrativeGovernanceModel) AcquireGovernanceAdvisoryLockTx(ctx context.Context, tx pgx.Tx) error {
	if tx == nil {
		return errors.New("transaction is required")
	}
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, GovernanceAdvisoryLockKey); err != nil {
		return fmt.Errorf("acquire governance advisory lock: %w", err)
	}
	return nil
}

// LockGovernanceSubjectsTx locks the users row of every participant FOR
// UPDATE in canonical order and returns their governance state. Missing
// accounts are absent from the returned map. Deleted accounts are returned
// with IsDeleted set so callers can classify rather than guess.
func (m *AdministrativeGovernanceModel) LockGovernanceSubjectsTx(
	ctx context.Context,
	tx pgx.Tx,
	ids ...uuid.UUID,
) (map[uuid.UUID]*GovernanceSubject, error) {
	if tx == nil {
		return nil, errors.New("transaction is required")
	}

	subjects := make(map[uuid.UUID]*GovernanceSubject, len(ids))

	for _, id := range sortedUniqueIDs(ids) {
		var subject GovernanceSubject
		err := tx.QueryRow(ctx, `
			SELECT id, email::text, is_active, deleted_at IS NOT NULL, COALESCE(password_hash,'')
			FROM users
			WHERE id = $1
			FOR UPDATE
		`, id).Scan(&subject.UserID, &subject.Email, &subject.IsActive, &subject.IsDeleted, &subject.PasswordHash)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				continue
			}
			return nil, fmt.Errorf("lock governance subject: %w", TranslateGovernanceError(err))
		}
		subjects[id] = &subject
	}

	if err := m.hydrateSubjects(ctx, tx, subjects); err != nil {
		return nil, err
	}
	return subjects, nil
}

// LockGovernanceSubjectByEmailTx locks one account by email (bootstrap).
// Returns ErrUserNotFound when no account has the email.
func (m *AdministrativeGovernanceModel) LockGovernanceSubjectByEmailTx(
	ctx context.Context,
	tx pgx.Tx,
	email string,
) (*GovernanceSubject, error) {
	if tx == nil {
		return nil, errors.New("transaction is required")
	}
	email = normalizeUserEmail(email)
	if email == "" {
		return nil, ErrUserNotFound
	}

	var id uuid.UUID
	if err := tx.QueryRow(ctx, `SELECT id FROM users WHERE email = $1`, email).Scan(&id); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrUserNotFound
		}
		return nil, fmt.Errorf("resolve account by email: %w", err)
	}

	subjects, err := m.LockGovernanceSubjectsTx(ctx, tx, id)
	if err != nil {
		return nil, err
	}
	subject, ok := subjects[id]
	if !ok || !strings.EqualFold(subject.Email,email) {
		return nil, ErrUserNotFound
	}
	return subject, nil
}

// GetGovernanceSubject reads one account's governance state without locking.
// It is for presentation and capability display only; every mutation
// re-reads state under LockGovernanceSubjectsTx.
func (m *AdministrativeGovernanceModel) GetGovernanceSubject(ctx context.Context, id uuid.UUID) (*GovernanceSubject, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	var subject GovernanceSubject
	err := m.DB.QueryRow(ctx, `
		SELECT id, email::text, is_active, deleted_at IS NOT NULL, COALESCE(password_hash,'')
		FROM users
		WHERE id = $1
	`, id).Scan(&subject.UserID, &subject.Email, &subject.IsActive, &subject.IsDeleted, &subject.PasswordHash)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrUserNotFound
		}
		return nil, fmt.Errorf("read governance subject: %w", err)
	}

	subjects := map[uuid.UUID]*GovernanceSubject{id: &subject}
	if err := m.hydrateSubjects(ctx, m.DB, subjects); err != nil {
		return nil, err
	}
	return &subject, nil
}

type governanceQueryer interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

func (m *AdministrativeGovernanceModel) hydrateSubjects(
	ctx context.Context,
	q governanceQueryer,
	subjects map[uuid.UUID]*GovernanceSubject,
) error {
	if len(subjects) == 0 {
		return nil
	}

	rootID, err := rootUserID(ctx, q)
	if err != nil {
		return err
	}

	for id, subject := range subjects {
		subject.IsRoot = rootID != nil && *rootID == id

		appointment, err := scanAppointment(q.QueryRow(ctx,
			`SELECT `+appointmentColumns+` FROM administrative_appointments WHERE user_id = $1`,
			id,
		))
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				continue
			}
			return fmt.Errorf("read administrative appointment: %w", err)
		}
		subject.Appointment = appointment
	}
	return nil
}

func rootUserID(ctx context.Context, q governanceQueryer) (*uuid.UUID, error) {
	var id uuid.UUID
	if err := q.QueryRow(ctx, `SELECT user_id FROM root_super_admin WHERE singleton = TRUE`).Scan(&id); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("read root super admin: %w", err)
	}
	return &id, nil
}

// RootUserIDTx returns the Root's user ID, or nil before bootstrap.
func (m *AdministrativeGovernanceModel) RootUserIDTx(ctx context.Context, tx pgx.Tx) (*uuid.UUID, error) {
	return rootUserID(ctx, tx)
}

// RootUserID returns the Root's user ID, or nil before bootstrap.
func (m *AdministrativeGovernanceModel) RootUserID(ctx context.Context) (*uuid.UUID, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()
	return rootUserID(ctx, m.DB)
}

// PutAppointmentTx inserts or updates the subject's appointment. The caller
// must hold the subject's users row lock.
func (m *AdministrativeGovernanceModel) PutAppointmentTx(
	ctx context.Context,
	tx pgx.Tx,
	change AppointmentChange,
) (*AdministrativeAppointment, error) {
	if tx == nil {
		return nil, errors.New("transaction is required")
	}
	if change.UserID == uuid.Nil || !change.Level.Valid() {
		return nil, fmt.Errorf("put appointment: invalid subject or level")
	}
	switch change.Status {
	case AppointmentStatusActive, AppointmentStatusSuspended, AppointmentStatusRevoked:
	default:
		return nil, fmt.Errorf("put appointment: invalid status")
	}

	appointment, err := scanAppointment(tx.QueryRow(ctx, `
		INSERT INTO administrative_appointments (
			user_id,
			level,
			status,
			appointed_by,
			appointed_at,
			status_changed_by,
			status_changed_at,
			status_reason
		)
		VALUES ($1, $2, $3, $4, NOW(), $4, NOW(), $5)
		ON CONFLICT (user_id) DO UPDATE SET
			level = EXCLUDED.level,
			status = EXCLUDED.status,
			appointed_by = CASE WHEN $6::boolean
				THEN EXCLUDED.appointed_by
				ELSE administrative_appointments.appointed_by END,
			appointed_at = CASE WHEN $6::boolean
				THEN NOW()
				ELSE administrative_appointments.appointed_at END,
			status_changed_by = EXCLUDED.status_changed_by,
			status_changed_at = NOW(),
			status_reason = EXCLUDED.status_reason
		RETURNING `+appointmentColumns,
		change.UserID,
		string(change.Level),
		string(change.Status),
		change.ActorID,
		change.Reason,
		change.NewTerm,
	))
	if err != nil {
		return nil, fmt.Errorf("put appointment: %w", TranslateGovernanceError(err))
	}
	return appointment, nil
}

// InsertRootSuperAdminTx records succession generation 1 and the singleton
// Root row for userID. The caller must already hold an active Super Admin
// appointment and super_admin primary role for userID in the same
// transaction (validated by trigger). A second Root is impossible: the
// singleton key turns any attempt into ErrRootSuperAdminAlreadyEstablished.
func (m *AdministrativeGovernanceModel) InsertRootSuperAdminTx(ctx context.Context, tx pgx.Tx, userID uuid.UUID) error {
	if tx == nil {
		return errors.New("transaction is required")
	}
	if userID == uuid.Nil {
		return errors.New("root user ID is required")
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO root_succession_events (
			succession_generation,
			predecessor_user_id,
			successor_user_id,
			mechanism
		)
		VALUES (1, NULL, $1, 'bootstrap')
	`, userID); err != nil {
		if IsUniqueViolation(err) {
			return ErrRootSuperAdminAlreadyEstablished
		}
		return fmt.Errorf("record root succession event: %w", TranslateGovernanceError(err))
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO root_super_admin (singleton, user_id, succession_generation)
		VALUES (TRUE, $1, 1)
	`, userID); err != nil {
		if IsUniqueViolation(err) {
			return ErrRootSuperAdminAlreadyEstablished
		}
		return fmt.Errorf("record root super admin: %w", TranslateGovernanceError(err))
	}
	return nil
}

// InsertGovernanceAuditTx appends one governance audit record. Pass a
// transaction to make the record atomic with the audited change, or the pool
// to record a denial after the governance transaction has rolled back.
// A missing seeded action or entity type is an error: governance actions are
// never silently unaudited.
func (m *AdministrativeGovernanceModel) InsertGovernanceAuditTx(
	ctx context.Context,
	execer governanceExecer,
	entry GovernanceAuditEntry,
) error {
	if execer == nil {
		return errors.New("audit executor is required")
	}
	if entry.Action == "" || entry.EntityType == "" || entry.EntityID == "" {
		return errors.New("governance audit entry is incomplete")
	}
	if entry.Outcome != GovernanceOutcomeSucceeded && entry.Outcome != GovernanceOutcomeDenied {
		return errors.New("governance audit outcome is invalid")
	}
	if entry.ActorID != nil && *entry.ActorID == uuid.Nil {
		entry.ActorID = nil
	}

	contextJSON, err := json.Marshal(entry.Context)
	if err != nil {
		return fmt.Errorf("encode governance audit context: %w", err)
	}

	// UUID identity distinguishes repeated actions. Timestamp is ordering data,
	// never the identity of an audit event.
	tag, err := execer.Exec(ctx, `
		INSERT INTO audit_logs (
			id,
			user_id,
			action_id,
			entity_type_id,
			entity_id,
			occurred_at,
			outcome,
			context
		)
		SELECT
			gen_random_uuid(),
			$1,
			a.id,
			et.id,
			$4,
			clock_timestamp(),
			$5,
			$6::jsonb
		FROM actions a
		CROSS JOIN entity_types et
		WHERE a.name = $2
		  AND et.name = $3
	`, entry.ActorID, entry.Action, entry.EntityType, entry.EntityID, entry.Outcome, string(contextJSON))
	if err != nil {
		return fmt.Errorf("insert governance audit: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return fmt.Errorf("%w: action %q entity type %q", ErrGovernanceAuditMetadataMissing, entry.Action, entry.EntityType)
	}
	return nil
}

const administratorSelect = `
	SELECT
		aa.user_id,
		u.email::text,
		u.is_active AND u.deleted_at IS NULL,
		rsa.user_id IS NOT NULL,
		aa.level,
		aa.status,
		aa.appointed_at,
		ab.email::text,
		aa.status_changed_at,
		aa.status_reason
	FROM administrative_appointments aa
	JOIN users u ON u.id = aa.user_id
	LEFT JOIN users ab ON ab.id = aa.appointed_by
	LEFT JOIN root_super_admin rsa ON rsa.user_id = aa.user_id
`

func scanAdministrator(row scannableRow) (*AdministratorRecord, error) {
	var r AdministratorRecord
	var level, status string
	if err := row.Scan(
		&r.UserID,
		&r.Email,
		&r.AccountActive,
		&r.IsRoot,
		&level,
		&status,
		&r.AppointedAt,
		&r.AppointedByEmail,
		&r.StatusChangedAt,
		&r.StatusReason,
	); err != nil {
		return nil, err
	}
	r.Level = AdministrativeLevel(level)
	r.Status = AppointmentStatus(status)
	return &r, nil
}

// MaxAdministratorDirectorySize bounds the administrator list.
const MaxAdministratorDirectorySize = 500

// ListAdministrators returns appointments, Root first, then Super Admins,
// then Admins; active before suspended before revoked.
func (m *AdministrativeGovernanceModel) ListAdministrators(ctx context.Context, includeRevoked bool) ([]*AdministratorRecord, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	rows, err := m.DB.Query(ctx, administratorSelect+`
		WHERE ($1::boolean OR aa.status <> 'revoked')
		ORDER BY
			(rsa.user_id IS NOT NULL) DESC,
			CASE aa.level WHEN 'super_admin' THEN 0 ELSE 1 END,
			CASE aa.status WHEN 'active' THEN 0 WHEN 'suspended' THEN 1 ELSE 2 END,
			u.email ASC
		LIMIT $2
	`, includeRevoked, MaxAdministratorDirectorySize)
	if err != nil {
		return nil, fmt.Errorf("list administrators: %w", err)
	}
	defer rows.Close()

	records := make([]*AdministratorRecord, 0)
	for rows.Next() {
		record, err := scanAdministrator(rows)
		if err != nil {
			return nil, fmt.Errorf("scan administrator: %w", err)
		}
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate administrators: %w", err)
	}
	return records, nil
}

// GetAdministrator returns one appointment record or ErrAdministrativeAppointmentNotFound.
func (m *AdministrativeGovernanceModel) GetAdministrator(ctx context.Context, userID uuid.UUID) (*AdministratorRecord, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	record, err := scanAdministrator(m.DB.QueryRow(ctx, administratorSelect+` WHERE aa.user_id = $1`, userID))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrAdministrativeAppointmentNotFound
		}
		return nil, fmt.Errorf("read administrator: %w", err)
	}
	return record, nil
}

// accountSelect projects live (non-deleted) accounts with their primary role
// and administrative standing. Read-only joins only.
const accountSelect = `
	SELECT
		u.id,
		u.email::text,
		u.is_active,
		u.created_at,
		COALESCE(r.name::text, ''),
		rsa.user_id IS NOT NULL,
		COALESCE(aa.level, ''),
		COALESCE(aa.status, '')
	FROM users u
	LEFT JOIN user_role_assignments ura
		ON ura.user_id = u.id
		AND ura.is_primary = TRUE
		AND ura.deleted_at IS NULL
	LEFT JOIN roles r
		ON r.id = ura.role_id
		AND r.deleted_at IS NULL
		AND r.is_active = TRUE
	LEFT JOIN administrative_appointments aa ON aa.user_id = u.id
	LEFT JOIN root_super_admin rsa ON rsa.user_id = u.id
	WHERE u.deleted_at IS NULL
`

func scanAccount(row scannableRow) (*AccountRecord, error) {
	var r AccountRecord
	var level, status string
	if err := row.Scan(
		&r.UserID,
		&r.Email,
		&r.AccountActive,
		&r.CreatedAt,
		&r.PrimaryRole,
		&r.IsRoot,
		&level,
		&status,
	); err != nil {
		return nil, err
	}
	r.AdministrativeLevel = AdministrativeLevel(level)
	r.AppointmentStatus = AppointmentStatus(status)
	return &r, nil
}

// MaxAccountDirectoryPageSize bounds one directory page.
const MaxAccountDirectoryPageSize = 100

// escapeLikePattern escapes LIKE metacharacters so user text matches literally.
func escapeLikePattern(s string) string {
	replacer := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return replacer.Replace(s)
}

// EscapeLikePatternForTest exposes escapeLikePattern to tests.
func EscapeLikePatternForTest(s string) string { return escapeLikePattern(s) }

// SearchAccounts lists live accounts. An empty query lists newest first; a
// UUID matches that account exactly; other text matches the email as a
// literal substring (case-insensitive).
func (m *AdministrativeGovernanceModel) SearchAccounts(ctx context.Context, query string, limit, offset int) ([]*AccountRecord, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	if limit <= 0 || limit > MaxAccountDirectoryPageSize {
		limit = MaxAccountDirectoryPageSize
	}
	if offset < 0 {
		offset = 0
	}
	query = strings.TrimSpace(query)

	var rows pgx.Rows
	var err error
	switch {
	case query == "":
		rows, err = m.DB.Query(ctx, accountSelect+`
			ORDER BY u.created_at DESC, u.id
			LIMIT $1 OFFSET $2
		`, limit, offset)
	default:
		if id, parseErr := uuid.Parse(query); parseErr == nil {
			rows, err = m.DB.Query(ctx, accountSelect+`
				AND u.id = $1
				LIMIT $2 OFFSET $3
			`, id, limit, offset)
		} else {
			rows, err = m.DB.Query(ctx, accountSelect+`
				AND u.email::text ILIKE '%' || $1 || '%' ESCAPE '\'
				ORDER BY u.email ASC, u.id
				LIMIT $2 OFFSET $3
			`, escapeLikePattern(query), limit, offset)
		}
	}
	if err != nil {
		return nil, fmt.Errorf("search accounts: %w", err)
	}
	defer rows.Close()

	records := make([]*AccountRecord, 0)
	for rows.Next() {
		record, err := scanAccount(rows)
		if err != nil {
			return nil, fmt.Errorf("scan account: %w", err)
		}
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate accounts: %w", err)
	}
	return records, nil
}

// GetAccount returns one live account, or ErrUserNotFound.
func (m *AdministrativeGovernanceModel) GetAccount(ctx context.Context, userID uuid.UUID) (*AccountRecord, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	record, err := scanAccount(m.DB.QueryRow(ctx, accountSelect+` AND u.id = $1`, userID))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrUserNotFound
		}
		return nil, fmt.Errorf("read account: %w", err)
	}
	return record, nil
}

// MaxGovernanceHistory bounds a history read.
const MaxGovernanceHistory = 100

// ListGovernanceHistory returns governance audit records about subjectID,
// newest first.
func (m *AdministrativeGovernanceModel) ListGovernanceHistory(ctx context.Context, subjectID uuid.UUID, limit int) ([]*GovernanceHistoryEntry, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	if limit <= 0 || limit > MaxGovernanceHistory {
		limit = MaxGovernanceHistory
	}

	rows, err := m.DB.Query(ctx, `
		SELECT
			al.occurred_at,
			a.name,
			COALESCE(al.outcome, 'succeeded'),
			actor.email::text,
			al.context
		FROM audit_logs al
		JOIN actions a ON a.id = al.action_id
		JOIN entity_types et ON et.id = al.entity_type_id
		LEFT JOIN users actor ON actor.id = al.user_id
		WHERE et.name IN ('administrator', 'root_super_admin')
		  AND al.entity_id = $1
		ORDER BY al.occurred_at DESC
		LIMIT $2
	`, subjectID.String(), limit)
	if err != nil {
		return nil, fmt.Errorf("list governance history: %w", err)
	}
	defer rows.Close()

	entries := make([]*GovernanceHistoryEntry, 0)
	for rows.Next() {
		var entry GovernanceHistoryEntry
		var raw []byte
		if err := rows.Scan(&entry.OccurredAt, &entry.Action, &entry.Outcome, &entry.ActorEmail, &raw); err != nil {
			return nil, fmt.Errorf("scan governance history: %w", err)
		}
		if len(raw) > 0 {
			var c GovernanceAuditContext
			if json.Unmarshal(raw, &c) == nil {
				entry.Reason = nonEmpty(c.Reason)
				entry.PreviousLevel = nonEmpty(c.PreviousLevel)
				entry.NewLevel = nonEmpty(c.NewLevel)
				entry.PreviousStatus = nonEmpty(c.PreviousStatus)
				entry.NewStatus = nonEmpty(c.NewStatus)
				entry.DenialCode = nonEmpty(c.DenialCode)
			}
		}
		entries = append(entries, &entry)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate governance history: %w", err)
	}
	return entries, nil
}

func nonEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// RequirePermissionTx evaluates live primary-role permissions under the same
// transaction as the change. User locks stabilize role assignment; the shared
// role lock stabilizes lifecycle while the governance mutex serializes catalog
// edits. Cached HTTP permissions are never enough to authorize a mutation.
func (m *AdministrativeGovernanceModel) RequirePermissionTx(ctx context.Context,tx pgx.Tx,userID uuid.UUID,permission string) error {
 var established bool
 if err:=tx.QueryRow(ctx,`SELECT EXISTS(SELECT 1 FROM root_super_admin WHERE singleton=TRUE)`).Scan(&established);err!=nil{return err}
 if !established{return ErrAdministrativePermissionDenied}
 var roleID uuid.UUID
 err:=tx.QueryRow(ctx,`SELECT r.id FROM user_role_assignments ura JOIN roles r ON r.id=ura.role_id
 JOIN users u ON u.id=ura.user_id WHERE ura.user_id=$1 AND ura.is_primary AND ura.deleted_at IS NULL
 AND r.deleted_at IS NULL AND r.is_active AND u.deleted_at IS NULL AND u.is_active FOR SHARE OF r`,userID).Scan(&roleID)
 if errors.Is(err,pgx.ErrNoRows){return ErrAdministrativePermissionDenied}
 if err!=nil{return err}
 var allowed bool
 if err:=tx.QueryRow(ctx,`SELECT EXISTS(SELECT 1 FROM role_permissions rp JOIN permissions p ON p.id=rp.permission_id
 WHERE rp.role_id=$1 AND p.name=$2)`,roleID,permission).Scan(&allowed);err!=nil{return err}
 if !allowed{return ErrAdministrativePermissionDenied};return nil
}

// GetAdministratorTx reads the response inside the successful mutation.
func (m *AdministrativeGovernanceModel) GetAdministratorTx(ctx context.Context,tx pgx.Tx,userID uuid.UUID) (*AdministratorRecord,error) {
 return scanAdministrator(tx.QueryRow(ctx,administratorSelect+` WHERE aa.user_id=$1`,userID))
}
