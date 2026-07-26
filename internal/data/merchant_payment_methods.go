// Package data provides canonical merchant payment-method models and database access methods.
//
// sdworkspace/sdbackend/internal/data/merchant_payment_methods.go
//
// GTM:
//
//	Layer: 2.4 Merchant / Future Offering Domain
//	Release Class: SPINE
//	Reason:
//	  merchant_payment_methods is release-critical payment-collection
//	  infrastructure. It records the merchant-owned platform representation
//	  of payment methods used to fulfill commercial obligations created by
//	  Commerce Architecture. It does not determine fees, invoices, plans,
//	  subscriptions, promotions, adjustments, provider selection, or provider
//	  verification policy.
//
// Architecture Boundary:
//
//	This file defines what payment methods a merchant has. External-provider
//	identity, provider references, connectivity, verification, synchronization,
//	and disconnection belong to merchant_payment_method_provider_links.go.
//	Payment execution belongs to merchant_payments.go.
//
// Security Boundary:
//
//	This table stores canonical identity, lifecycle state, and safe display
//	metadata only. It is not a card vault, bank-account vault, provider-link
//	registry, or token store. It must never persist, expose, or log full card
//	numbers, bank account or routing numbers, CVVs, bearer-authority payment
//	tokens, provider references, raw provider payloads, or provider credentials.
//	last_four is display metadata only and is never proof of ownership,
//	verification, connectivity, or collectability.
//
// Operational Capability Doctrine:
//
//	Engineering provides the canonical payment-method capability and stable
//	extension boundaries. Administration configures supported method types,
//	provider availability, onboarding requirements, verification requirements,
//	and collection policy outside this canonical persistence contract.
//
// SPINE Rule:
//
//	Keep compiling.
//	Keep production-ready.
//	Preserve provider independence.
//	Preserve the single-active-default-per-merchant invariant under concurrency.
//	Preserve soft-delete lifecycle semantics.
//	Preserve revoked as terminal.
//	Permit expired only for card methods.
//	Never persist or expose protected payment credentials or provider references.
//	Block deployment if this file breaks merchant payment-method persistence,
//	ownership scoping, lifecycle integrity, default-method integrity, or the
//	Merchant Payments Architecture boundary.
package data

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/PiccoloMondoC/sdworkspace/sdbackend/internal/logging"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ---------------------------------------------------------------------------
// Controlled vocabularies
// ---------------------------------------------------------------------------

// MerchantPaymentMethodType is the controlled vocabulary for the canonical
// instrument or billing arrangement a merchant has registered with the platform.
type MerchantPaymentMethodType string

const (
	// PaymentMethodTypeCard is a card payment method. Provider connectivity is
	// represented separately by merchant_payment_method_provider_links.
	PaymentMethodTypeCard MerchantPaymentMethodType = "card"

	// PaymentMethodTypeBankAccount is a bank-account payment method. Provider
	// connectivity and verification are represented separately.
	PaymentMethodTypeBankAccount MerchantPaymentMethodType = "bank_account"

	// PaymentMethodTypeWire represents wire-transfer billing arrangements.
	// Provider linkage, when supported operationally, remains a separate concern.
	PaymentMethodTypeWire MerchantPaymentMethodType = "wire"

	// PaymentMethodTypeManualInvoice represents manually invoiced billing
	// arrangements. Provider linkage, if ever supported, remains separate.
	PaymentMethodTypeManualInvoice MerchantPaymentMethodType = "manual_invoice"
)

// MerchantPaymentMethodStatus is the controlled vocabulary for the
// operational lifecycle state of a merchant payment method.
type MerchantPaymentMethodStatus string

const (
	// PaymentMethodStatusActive indicates the payment method is currently
	// usable for billing collection and may be the merchant's operational
	// default.
	PaymentMethodStatusActive MerchantPaymentMethodStatus = "active"

	// PaymentMethodStatusInactive indicates the payment method is
	// temporarily not in use. Inactive methods may be reactivated.
	PaymentMethodStatusInactive MerchantPaymentMethodStatus = "inactive"

	// PaymentMethodStatusExpired indicates the payment method's underlying
	// instrument (e.g. a card) has expired.
	PaymentMethodStatusExpired MerchantPaymentMethodStatus = "expired"

	// PaymentMethodStatusRevoked indicates the payment method has been
	// permanently revoked. Revoked is a terminal state in this file; no
	// method here transitions a payment method out of revoked.
	PaymentMethodStatusRevoked MerchantPaymentMethodStatus = "revoked"
)

// ---------------------------------------------------------------------------
// Field constraints
// ---------------------------------------------------------------------------

const merchantPaymentMethodDisplayLabelMaxLen = 255

func validateMerchantPaymentMethodDisplayLabel(displayLabel *string) error {
	if displayLabel == nil {
		return nil
	}
	if len(*displayLabel) > merchantPaymentMethodDisplayLabelMaxLen {
		return fmt.Errorf(
			"display_label must be %d characters or fewer",
			merchantPaymentMethodDisplayLabelMaxLen,
		)
	}
	return nil
}

// merchantPaymentMethodSelectColumns centralizes the select-column list used
// by every read path in this file so SQL column order and Go scan order
// cannot drift independently.
const merchantPaymentMethodSelectColumns = `
	id,
	merchant_id,
	payment_method_type,
	display_label,
	last_four,
	status,
	is_default,
	created_at,
	updated_at,
	deleted_at
`

// ---------------------------------------------------------------------------
// Model
// ---------------------------------------------------------------------------

// MerchantPaymentMethod represents a canonical row in merchant_payment_methods.
//
// It records the merchant-owned platform identity and lifecycle of a payment
// method together with safe display metadata. External-provider references,
// connectivity, verification, and synchronization state must not be added to
// this type; they belong to MerchantPaymentMethodProviderLink.
type MerchantPaymentMethod struct {
	ID                uuid.UUID                   `json:"id" db:"id"`
	MerchantID        uuid.UUID                   `json:"merchant_id" db:"merchant_id"`
	PaymentMethodType MerchantPaymentMethodType   `json:"payment_method_type" db:"payment_method_type"`
	DisplayLabel      *string                     `json:"display_label,omitempty" db:"display_label"`
	LastFour          *string                     `json:"last_four,omitempty" db:"last_four"`
	Status            MerchantPaymentMethodStatus `json:"status" db:"status"`
	IsDefault         bool                        `json:"is_default" db:"is_default"`
	CreatedAt         time.Time                   `json:"created_at" db:"created_at"`
	UpdatedAt         time.Time                   `json:"updated_at" db:"updated_at"`
	DeletedAt         *time.Time                  `json:"-" db:"deleted_at"`
}

// MerchantPaymentMethodModel owns persistence for merchant payment methods.
type MerchantPaymentMethodModel struct {
	DB     *pgxpool.Pool
	Logger *logging.Logger
}

// paymentMethodQuerier is the minimal shared query contract satisfied by both
// *pgxpool.Pool and pgx.Tx. It exists so insert and default-assignment logic
// can be shared between pool-based and transaction-based call paths without
// granting this model unscoped transaction ownership. Do not expand this
// interface with unrelated behavior.
type paymentMethodQuerier interface {
	QueryRow(ctx context.Context, sql string, args ...interface{}) pgx.Row
	Exec(ctx context.Context, sql string, args ...interface{}) (pgconn.CommandTag, error)
}

func scanMerchantPaymentMethod(row scannableRow, method *MerchantPaymentMethod) error {
	return row.Scan(
		&method.ID,
		&method.MerchantID,
		&method.PaymentMethodType,
		&method.DisplayLabel,
		&method.LastFour,
		&method.Status,
		&method.IsDefault,
		&method.CreatedAt,
		&method.UpdatedAt,
		&method.DeletedAt,
	)
}

// ---------------------------------------------------------------------------
// Normalization and validation
// ---------------------------------------------------------------------------

// NormalizeMerchantPaymentMethodType trims and canonicalizes a payment
// method type. Normalization is idempotent: normalizing an already-canonical
// value returns the same value.
func NormalizeMerchantPaymentMethodType(t MerchantPaymentMethodType) MerchantPaymentMethodType {
	return MerchantPaymentMethodType(normalizeIdentifier(string(t)))
}

// IsValidMerchantPaymentMethodType reports whether t is one of the allowed
// payment method types after normalization.
func IsValidMerchantPaymentMethodType(t MerchantPaymentMethodType) bool {
	switch NormalizeMerchantPaymentMethodType(t) {
	case PaymentMethodTypeCard, PaymentMethodTypeBankAccount, PaymentMethodTypeWire,
		PaymentMethodTypeManualInvoice:
		return true
	default:
		return false
	}
}

// NormalizeMerchantPaymentMethodStatus trims and canonicalizes a payment
// method status. Normalization is idempotent.
func NormalizeMerchantPaymentMethodStatus(s MerchantPaymentMethodStatus) MerchantPaymentMethodStatus {
	return MerchantPaymentMethodStatus(normalizeIdentifier(string(s)))
}

// IsValidMerchantPaymentMethodStatus reports whether s is one of the allowed
// statuses after normalization.
func IsValidMerchantPaymentMethodStatus(s MerchantPaymentMethodStatus) bool {
	switch NormalizeMerchantPaymentMethodStatus(s) {
	case PaymentMethodStatusActive, PaymentMethodStatusInactive,
		PaymentMethodStatusExpired, PaymentMethodStatusRevoked:
		return true
	default:
		return false
	}
}

// validateLastFour validates that, when present, last_four is exactly four
// ASCII digits. The input is expected to already be trimmed.
func validateLastFour(lastFour *string) error {
	if lastFour == nil {
		return nil
	}
	v := *lastFour
	if len(v) != 4 {
		return errors.New("last_four must be exactly four digits")
	}
	for _, r := range v {
		if r < '0' || r > '9' {
			return errors.New("last_four must contain only ASCII digits")
		}
	}
	return nil
}

// normalizeMerchantPaymentMethod applies canonical normalization to a
// MerchantPaymentMethod. The canonical validated value is what downstream
// code and persistence must use, per BEG 3.14/22.3.
func normalizeMerchantPaymentMethod(method *MerchantPaymentMethod) {
	method.PaymentMethodType = NormalizeMerchantPaymentMethodType(method.PaymentMethodType)
	method.Status = NormalizeMerchantPaymentMethodStatus(method.Status)
	method.DisplayLabel = normalizeOptionalString(method.DisplayLabel)
	method.LastFour = normalizeOptionalString(method.LastFour)
}

// validateMerchantPaymentMethodForInsert validates and canonicalizes a
// MerchantPaymentMethod prior to insertion.
//
// If method.Status is blank after normalization, it defaults to
// PaymentMethodStatusActive, matching the database column default. This
// default is applied here (rather than left implicit) so callers can rely on
// method.Status reflecting the value that will actually be persisted before
// Insert executes.
func validateMerchantPaymentMethodForInsert(method *MerchantPaymentMethod) error {
	if method == nil {
		return errors.New("merchant payment method is required")
	}

	normalizeMerchantPaymentMethod(method)

	if method.MerchantID == uuid.Nil {
		return errors.New("merchant payment method merchant_id is required")
	}

	if !IsValidMerchantPaymentMethodType(method.PaymentMethodType) {
		return fmt.Errorf("invalid merchant payment method type: %s", method.PaymentMethodType)
	}

	if method.Status == "" {
		method.Status = PaymentMethodStatusActive
	}
	if !IsValidMerchantPaymentMethodStatus(method.Status) {
		return fmt.Errorf("invalid merchant payment method status: %s", method.Status)
	}
	if method.Status == PaymentMethodStatusExpired && method.PaymentMethodType != PaymentMethodTypeCard {
		return errors.New("expired status is permitted only for card payment methods")
	}

	if method.IsDefault && method.Status != PaymentMethodStatusActive {
		return errors.New("merchant payment method cannot be inserted as default unless status is active")
	}

	if err := validateLastFour(method.LastFour); err != nil {
		return err
	}
	if err := validateMerchantPaymentMethodDisplayLabel(method.DisplayLabel); err != nil {
		return err
	}

	return nil
}

// validateMerchantPaymentMethodForUpdate validates the mutable-field subset
// permitted through Update. Immutable identity, lifecycle state, and default
// status are intentionally not touched by this path; see
// SetDefault, ClearDefault, UpdateStatus, SoftDelete, and Restore.
func validateMerchantPaymentMethodForUpdate(method *MerchantPaymentMethod) error {
	if method == nil {
		return errors.New("merchant payment method is required")
	}
	if method.ID == uuid.Nil {
		return errors.New("merchant payment method id is required")
	}
	if method.MerchantID == uuid.Nil {
		return errors.New("merchant payment method merchant_id is required")
	}

	method.DisplayLabel = normalizeOptionalString(method.DisplayLabel)
	method.LastFour = normalizeOptionalString(method.LastFour)

	if err := validateLastFour(method.LastFour); err != nil {
		return err
	}
	if err := validateMerchantPaymentMethodDisplayLabel(method.DisplayLabel); err != nil {
		return err
	}
	return nil
}

// ---------------------------------------------------------------------------
// Database error classification
// ---------------------------------------------------------------------------

// classifyMerchantPaymentMethodWriteError translates PostgreSQL write errors
// into centralized data-layer sentinels. Constraint-specific classification
// prevents unrelated unique violations from being mislabeled as default-method
// conflicts and preserves stable model-facing error contracts.
func classifyMerchantPaymentMethodWriteError(err error) error {
	switch {
	case IsForeignKeyViolation(err):
		return ErrMerchantNotFound
	case IsPgConstraint(err, "ux_merchant_payment_methods_default"):
		return ErrMerchantPaymentMethodDefaultConflict
	case IsUniqueViolation(err):
		return ErrDuplicate
	case IsCheckViolation(err), IsNotNullViolation(err):
		return ErrMerchantPaymentMethodInvalidState
	default:
		return err
	}
}

// ---------------------------------------------------------------------------
// Insert
// ---------------------------------------------------------------------------

// insertRow performs the raw INSERT for a validated, canonicalized
// MerchantPaymentMethod using the supplied querier (pool or transaction).
func (m *MerchantPaymentMethodModel) insertRow(ctx context.Context, q paymentMethodQuerier, method *MerchantPaymentMethod) error {
	const query = `
		INSERT INTO merchant_payment_methods (
			id,
			merchant_id,
			payment_method_type,
			display_label,
			last_four,
			status,
			is_default
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING created_at, updated_at, deleted_at
	`

	return q.QueryRow(ctx, query,
		method.ID,
		method.MerchantID,
		method.PaymentMethodType,
		method.DisplayLabel,
		method.LastFour,
		method.Status,
		method.IsDefault,
	).Scan(&method.CreatedAt, &method.UpdatedAt, &method.DeletedAt)
}

// lockMerchantForPaymentMethodDefault takes a row-level lock on the
// merchant row to serialize concurrent default-assignment operations for the
// same merchant. This is the concurrency-serialization mechanism used by
// Insert (when IsDefault is requested) and SetDefault; the partial unique
// index on (merchant_id) WHERE is_default remains the final integrity
// backstop, not the primary concurrency strategy.
func (m *MerchantPaymentMethodModel) lockMerchantForPaymentMethodDefault(ctx context.Context, q paymentMethodQuerier, merchantID uuid.UUID) error {
	var locked uuid.UUID
	err := q.QueryRow(ctx, `SELECT id FROM merchants WHERE id = $1 FOR UPDATE`, merchantID).Scan(&locked)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("%w: %s", ErrMerchantNotFound, merchantID)
		}
		return err
	}
	return nil
}

func (m *MerchantPaymentMethodModel) clearMerchantDefault(ctx context.Context, q paymentMethodQuerier, merchantID uuid.UUID) error {
	_, err := q.Exec(ctx, `
		UPDATE merchant_payment_methods
		SET is_default = FALSE
		WHERE merchant_id = $1
		  AND is_default = TRUE
		  AND deleted_at IS NULL
	`, merchantID)
	return err
}

// insertWithinTx contains the shared insert + default-assignment logic used
// by both Insert (which owns its own transaction) and InsertTx (which
// participates in a caller-supplied transaction).
func (m *MerchantPaymentMethodModel) insertWithinTx(ctx context.Context, q paymentMethodQuerier, method *MerchantPaymentMethod) error {
	if method.IsDefault {
		if err := m.lockMerchantForPaymentMethodDefault(ctx, q, method.MerchantID); err != nil {
			return err
		}
		if err := m.clearMerchantDefault(ctx, q, method.MerchantID); err != nil {
			return classifyMerchantPaymentMethodWriteError(err)
		}
	}

	if err := m.insertRow(ctx, q, method); err != nil {
		return classifyMerchantPaymentMethodWriteError(err)
	}

	return nil
}

// Insert inserts a new merchant payment method.
//
// If method.ID is uuid.Nil, a new UUID is generated. A caller-provided ID is
// accepted only to support the established controlled-seed convention;
// ordinary application callers should leave method.ID as uuid.Nil.
//
// If method.IsDefault is true, Insert opens a short transaction, locks the
// merchant row, clears any existing active default for that merchant, and
// inserts the new row as the default — preserving the single-active-default
// invariant under concurrent requests.
func (m *MerchantPaymentMethodModel) Insert(ctx context.Context, method *MerchantPaymentMethod) error {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("InsertMerchantPaymentMethod")

	if err := validateMerchantPaymentMethodForInsert(method); err != nil {
		logger.Error("validation failed", err)
		return err
	}

	if method.ID == uuid.Nil {
		method.ID = uuid.New()
	}

	tx, err := m.DB.Begin(ctx)
	if err != nil {
		logger.Error("begin transaction failed", err, "merchant_id", method.MerchantID)
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck // no-op once committed

	if err := m.insertWithinTx(ctx, tx, method); err != nil {
		logger.Error(
			"insert merchant payment method failed", err,
			"merchant_id", method.MerchantID,
			"payment_method_type", method.PaymentMethodType,
		)
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		logger.Error("commit merchant payment method insert failed", err, "merchant_id", method.MerchantID)
		return err
	}

	logger.Info(
		"insert merchant payment method successful",
		"payment_method_id", method.ID,
		"merchant_id", method.MerchantID,
		"payment_method_type", method.PaymentMethodType,
		"status", method.Status,
		"is_default", method.IsDefault,
	)
	return nil
}

// InsertTx performs the same validated insert as Insert, but participates in
// a caller-supplied transaction instead of opening its own. This is the
// transaction seam for orchestration workflows (e.g. merchant onboarding or
// billing-account activation) that must atomically insert a payment method
// alongside other writes. The caller owns tx.Commit/tx.Rollback.
func (m *MerchantPaymentMethodModel) InsertTx(ctx context.Context, tx pgx.Tx, method *MerchantPaymentMethod) error {
	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("InsertMerchantPaymentMethodTx")

	if tx == nil {
		err := errors.New("merchant payment method transaction is required")
		logger.Error("validation failed", err)
		return err
	}

	if err := validateMerchantPaymentMethodForInsert(method); err != nil {
		logger.Error("validation failed", err)
		return err
	}
	if method.ID == uuid.Nil {
		method.ID = uuid.New()
	}

	if err := m.insertWithinTx(ctx, tx, method); err != nil {
		logger.Error(
			"insert merchant payment method (tx) failed", err,
			"merchant_id", method.MerchantID,
			"payment_method_type", method.PaymentMethodType,
		)
		return err
	}

	logger.Info(
		"insert merchant payment method (tx) successful",
		"payment_method_id", method.ID,
		"merchant_id", method.MerchantID,
		"payment_method_type", method.PaymentMethodType,
		"status", method.Status,
		"is_default", method.IsDefault,
	)
	return nil
}

// ---------------------------------------------------------------------------
// Reads
// ---------------------------------------------------------------------------

// GetByID retrieves a non-deleted merchant payment method by its global id.
//
// This is a global, admin-facing lookup. It does not perform merchant
// ownership scoping. Merchant-facing or authorization-sensitive callers must
// use GetByIDForMerchant instead.
func (m *MerchantPaymentMethodModel) GetByID(ctx context.Context, id uuid.UUID) (*MerchantPaymentMethod, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("GetMerchantPaymentMethodByID")

	if id == uuid.Nil {
		err := errors.New("merchant payment method id is required")
		logger.Error("validation failed", err)
		return nil, err
	}

	query := `
		SELECT ` + merchantPaymentMethodSelectColumns + `
		FROM merchant_payment_methods
		WHERE id = $1
		  AND deleted_at IS NULL
	`

	var method MerchantPaymentMethod
	if err := scanMerchantPaymentMethod(m.DB.QueryRow(ctx, query, id), &method); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			logger.Warn("merchant payment method not found", "payment_method_id", id)
			return nil, ErrMerchantPaymentMethodNotFound
		}
		logger.Error("get merchant payment method by id failed", err, "payment_method_id", id)
		return nil, err
	}

	logger.Info("get merchant payment method by id successful", "payment_method_id", method.ID, "merchant_id", method.MerchantID)
	return &method, nil
}

// GetByIDForMerchant retrieves a non-deleted merchant payment method by id,
// scoped to the owning merchant. This is the correct method for
// service-layer authorization checks: a row belonging to a different
// merchant is reported as not found, not as a distinct forbidden error, to
// avoid leaking cross-merchant existence.
func (m *MerchantPaymentMethodModel) GetByIDForMerchant(ctx context.Context, merchantID, paymentMethodID uuid.UUID) (*MerchantPaymentMethod, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("GetMerchantPaymentMethodByIDForMerchant")

	if merchantID == uuid.Nil || paymentMethodID == uuid.Nil {
		err := errors.New("merchant id and payment method id are required")
		logger.Error("validation failed", err)
		return nil, err
	}

	query := `
		SELECT ` + merchantPaymentMethodSelectColumns + `
		FROM merchant_payment_methods
		WHERE id = $1
		  AND merchant_id = $2
		  AND deleted_at IS NULL
	`

	var method MerchantPaymentMethod
	if err := scanMerchantPaymentMethod(m.DB.QueryRow(ctx, query, paymentMethodID, merchantID), &method); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			logger.Warn("merchant payment method not found for merchant", "payment_method_id", paymentMethodID, "merchant_id", merchantID)
			return nil, ErrMerchantPaymentMethodNotFound
		}
		logger.Error("get merchant payment method for merchant failed", err, "payment_method_id", paymentMethodID, "merchant_id", merchantID)
		return nil, err
	}

	logger.Info("get merchant payment method for merchant successful", "payment_method_id", method.ID, "merchant_id", merchantID)
	return &method, nil
}

// GetDefaultForMerchant retrieves the merchant's current active, non-deleted
// operational default payment method, if one exists.
//
// Unlike other Get methods, a missing default is a normal, expected state
// (not every merchant has designated a default), so this method returns
// (nil, nil) rather than ErrMerchantPaymentMethodNotFound when none exists.
func (m *MerchantPaymentMethodModel) GetDefaultForMerchant(ctx context.Context, merchantID uuid.UUID) (*MerchantPaymentMethod, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("GetDefaultMerchantPaymentMethodForMerchant")

	if merchantID == uuid.Nil {
		err := errors.New("merchant id is required")
		logger.Error("validation failed", err)
		return nil, err
	}

	query := `
		SELECT ` + merchantPaymentMethodSelectColumns + `
		FROM merchant_payment_methods
		WHERE merchant_id = $1
		  AND is_default = TRUE
		  AND status = 'active'
		  AND deleted_at IS NULL
		LIMIT 1
	`

	var method MerchantPaymentMethod
	if err := scanMerchantPaymentMethod(m.DB.QueryRow(ctx, query, merchantID), &method); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			logger.Info("no default merchant payment method set", "merchant_id", merchantID)
			return nil, nil
		}
		logger.Error("get default merchant payment method failed", err, "merchant_id", merchantID)
		return nil, err
	}

	logger.Info("get default merchant payment method successful", "payment_method_id", method.ID, "merchant_id", merchantID)
	return &method, nil
}

// ListByMerchant retrieves a merchant's non-deleted payment methods with
// explicit bounded pagination, optionally filtered by status.
//
// Ordering is default-first, then most-recently-created first, which matches
// real merchant billing use: the operational default should surface first,
// followed by the most likely-to-be-relevant recent methods.
func (m *MerchantPaymentMethodModel) ListByMerchant(
	ctx context.Context,
	merchantID uuid.UUID,
	status *MerchantPaymentMethodStatus,
	limit, offset int,
) ([]*MerchantPaymentMethod, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("ListMerchantPaymentMethodsByMerchant")

	if merchantID == uuid.Nil {
		err := errors.New("merchant id is required")
		logger.Error("validation failed", err)
		return nil, err
	}
	if limit <= 0 || limit > 100 {
		err := errors.New("limit must be between 1 and 100")
		logger.Error("validation failed", err)
		return nil, err
	}
	if offset < 0 {
		err := errors.New("offset must be non-negative")
		logger.Error("validation failed", err)
		return nil, err
	}

	var normalizedStatus *MerchantPaymentMethodStatus
	if status != nil {
		s := NormalizeMerchantPaymentMethodStatus(*status)
		if !IsValidMerchantPaymentMethodStatus(s) {
			err := fmt.Errorf("invalid merchant payment method status filter: %s", s)
			logger.Error("validation failed", err)
			return nil, err
		}
		normalizedStatus = &s
	}

	query := `
		SELECT ` + merchantPaymentMethodSelectColumns + `
		FROM merchant_payment_methods
		WHERE merchant_id = $1
		  AND deleted_at IS NULL
	`
	args := []interface{}{merchantID}
	if normalizedStatus != nil {
		query += ` AND status = $2`
		args = append(args, *normalizedStatus)
	}
	query += `
		ORDER BY is_default DESC, created_at DESC
		LIMIT ` + fmt.Sprintf("$%d", len(args)+1) + ` OFFSET ` + fmt.Sprintf("$%d", len(args)+2)
	args = append(args, limit, offset)

	rows, err := m.DB.Query(ctx, query, args...)
	if err != nil {
		logger.Error("list merchant payment methods query failed", err, "merchant_id", merchantID)
		return nil, err
	}
	defer rows.Close()

	var methods []*MerchantPaymentMethod
	for rows.Next() {
		var method MerchantPaymentMethod
		if err := scanMerchantPaymentMethod(rows, &method); err != nil {
			logger.Error("merchant payment method row scan failed", err, "merchant_id", merchantID)
			return nil, err
		}
		methods = append(methods, &method)
	}
	if err := rows.Err(); err != nil {
		logger.Error("merchant payment method row iteration failed", err, "merchant_id", merchantID)
		return nil, err
	}

	logger.Info("list merchant payment methods successful", "merchant_id", merchantID, "count", len(methods))
	return methods, nil
}

// Exists reports whether a non-deleted merchant payment method exists by id.
func (m *MerchantPaymentMethodModel) Exists(ctx context.Context, id uuid.UUID) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("MerchantPaymentMethodExists")

	if id == uuid.Nil {
		err := errors.New("merchant payment method id is required")
		logger.Error("validation failed", err)
		return false, err
	}

	const query = `
		SELECT EXISTS (
			SELECT 1 FROM merchant_payment_methods
			WHERE id = $1 AND deleted_at IS NULL
		)
	`

	var exists bool
	if err := m.DB.QueryRow(ctx, query, id).Scan(&exists); err != nil {
		logger.Error("merchant payment method exists check failed", err, "payment_method_id", id)
		return false, err
	}

	return exists, nil
}

// ExistsForMerchant reports whether a non-deleted merchant payment method
// exists by id, scoped to the owning merchant.
func (m *MerchantPaymentMethodModel) ExistsForMerchant(ctx context.Context, merchantID, id uuid.UUID) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("MerchantPaymentMethodExistsForMerchant")

	if merchantID == uuid.Nil || id == uuid.Nil {
		err := errors.New("merchant id and payment method id are required")
		logger.Error("validation failed", err)
		return false, err
	}

	const query = `
		SELECT EXISTS (
			SELECT 1 FROM merchant_payment_methods
			WHERE id = $1 AND merchant_id = $2 AND deleted_at IS NULL
		)
	`

	var exists bool
	if err := m.DB.QueryRow(ctx, query, id, merchantID).Scan(&exists); err != nil {
		logger.Error("merchant payment method exists-for-merchant check failed", err, "payment_method_id", id, "merchant_id", merchantID)
		return false, err
	}

	return exists, nil
}

// ---------------------------------------------------------------------------
// Update (mutable display fields only)
// ---------------------------------------------------------------------------

// Update updates the safely mutable display fields of a non-deleted merchant
// payment method: display_label and last_four.
//
// Update deliberately does not permit changes to merchant ownership,
// payment_method_type, status, or is_default. Those require SetDefault, ClearDefault, UpdateStatus,
// SoftDelete, or Restore, which protect their own invariants.
func (m *MerchantPaymentMethodModel) Update(ctx context.Context, method *MerchantPaymentMethod) error {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("UpdateMerchantPaymentMethod")

	if err := validateMerchantPaymentMethodForUpdate(method); err != nil {
		logger.Error("validation failed", err)
		return err
	}

	query := `
		UPDATE merchant_payment_methods
		SET
			display_label = $1,
			last_four = $2
		WHERE id = $3
		  AND merchant_id = $4
		  AND deleted_at IS NULL
		RETURNING payment_method_type, status, is_default,
			created_at, updated_at, deleted_at
	`

	err := m.DB.QueryRow(ctx, query,
		method.DisplayLabel,
		method.LastFour,
		method.ID,
		method.MerchantID,
	).Scan(
		&method.PaymentMethodType,
		&method.Status,
		&method.IsDefault,
		&method.CreatedAt,
		&method.UpdatedAt,
		&method.DeletedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			logger.Warn("merchant payment method not found for update", "payment_method_id", method.ID, "merchant_id", method.MerchantID)
			return ErrMerchantPaymentMethodNotFound
		}
		logger.Error("update merchant payment method failed", err, "payment_method_id", method.ID, "merchant_id", method.MerchantID)
		return err
	}

	logger.Info("update merchant payment method successful", "payment_method_id", method.ID, "merchant_id", method.MerchantID)
	return nil
}

// ---------------------------------------------------------------------------
// SetDefault / ClearDefault
// ---------------------------------------------------------------------------

// setDefaultWithinTx contains the shared default-reassignment logic used by
// both SetDefault (owns its own transaction) and SetDefaultTx (participates
// in a caller-supplied transaction).
func (m *MerchantPaymentMethodModel) setDefaultWithinTx(ctx context.Context, q paymentMethodQuerier, merchantID, paymentMethodID uuid.UUID) error {
	if err := m.lockMerchantForPaymentMethodDefault(ctx, q, merchantID); err != nil {
		return err
	}

	// Verify the target belongs to this merchant, is non-deleted, and is
	// active, taking a row lock so a concurrent status/delete transition
	// cannot race this check.
	var currentStatus MerchantPaymentMethodStatus
	var deletedAt *time.Time
	err := q.QueryRow(ctx, `
		SELECT status, deleted_at
		FROM merchant_payment_methods
		WHERE id = $1 AND merchant_id = $2
		FOR UPDATE
	`, paymentMethodID, merchantID).Scan(&currentStatus, &deletedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrMerchantPaymentMethodNotFound
		}
		return err
	}
	if deletedAt != nil {
		return fmt.Errorf("%w: payment method is soft-deleted", ErrMerchantPaymentMethodInvalidState)
	}
	if currentStatus != PaymentMethodStatusActive {
		return fmt.Errorf("%w: payment method status is %s, must be active", ErrMerchantPaymentMethodInvalidState, currentStatus)
	}

	if err := m.clearMerchantDefault(ctx, q, merchantID); err != nil {
		return classifyMerchantPaymentMethodWriteError(err)
	}

	_, err = q.Exec(ctx, `
		UPDATE merchant_payment_methods
		SET is_default = TRUE
		WHERE id = $1 AND merchant_id = $2
	`, paymentMethodID, merchantID)
	if err != nil {
		return classifyMerchantPaymentMethodWriteError(err)
	}

	return nil
}

// SetDefault atomically assigns paymentMethodID as the merchant's
// operational default payment method.
//
// It verifies the target belongs to merchantID, is non-deleted, and is
// active; clears any existing default for the merchant; and sets the target
// as default, all within a single short transaction that locks the merchant
// row to serialize concurrent default-assignment requests. It never sets a
// method belonging to another merchant as default.
func (m *MerchantPaymentMethodModel) SetDefault(ctx context.Context, merchantID, paymentMethodID uuid.UUID) error {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("SetDefaultMerchantPaymentMethod")

	if merchantID == uuid.Nil || paymentMethodID == uuid.Nil {
		err := errors.New("merchant id and payment method id are required")
		logger.Error("validation failed", err)
		return err
	}

	tx, err := m.DB.Begin(ctx)
	if err != nil {
		logger.Error("begin transaction failed", err, "merchant_id", merchantID)
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck // no-op once committed

	if err := m.setDefaultWithinTx(ctx, tx, merchantID, paymentMethodID); err != nil {
		logger.Error("set default merchant payment method failed", err, "merchant_id", merchantID, "payment_method_id", paymentMethodID)
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		logger.Error("commit set-default merchant payment method failed", err, "merchant_id", merchantID, "payment_method_id", paymentMethodID)
		return err
	}

	logger.Info("set default merchant payment method successful", "merchant_id", merchantID, "payment_method_id", paymentMethodID, "is_default", true)
	return nil
}

// SetDefaultTx performs the same default-reassignment as SetDefault, but
// participates in a caller-supplied transaction. This is the transaction seam
// for orchestration workflows (e.g. subscription activation requiring a
// verified default payment method) that must combine this operation
// atomically with other writes. The caller owns tx.Commit/tx.Rollback.
func (m *MerchantPaymentMethodModel) SetDefaultTx(ctx context.Context, tx pgx.Tx, merchantID, paymentMethodID uuid.UUID) error {
	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("SetDefaultMerchantPaymentMethodTx")

	if tx == nil {
		err := errors.New("merchant payment method transaction is required")
		logger.Error("validation failed", err)
		return err
	}

	if merchantID == uuid.Nil || paymentMethodID == uuid.Nil {
		err := errors.New("merchant id and payment method id are required")
		logger.Error("validation failed", err)
		return err
	}

	if err := m.setDefaultWithinTx(ctx, tx, merchantID, paymentMethodID); err != nil {
		logger.Error("set default merchant payment method (tx) failed", err, "merchant_id", merchantID, "payment_method_id", paymentMethodID)
		return err
	}

	logger.Info("set default merchant payment method (tx) successful", "merchant_id", merchantID, "payment_method_id", paymentMethodID, "is_default", true)
	return nil
}

// ClearDefault clears is_default for a specific, currently-default merchant
// payment method, leaving the merchant with no operational default.
//
// This is a single atomic statement: the WHERE clause requires the row to
// currently be the merchant's default and non-deleted, so no explicit
// transaction is needed.
func (m *MerchantPaymentMethodModel) ClearDefault(ctx context.Context, merchantID, paymentMethodID uuid.UUID) error {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("ClearDefaultMerchantPaymentMethod")

	if merchantID == uuid.Nil || paymentMethodID == uuid.Nil {
		err := errors.New("merchant id and payment method id are required")
		logger.Error("validation failed", err)
		return err
	}

	const query = `
		UPDATE merchant_payment_methods
		SET is_default = FALSE
		WHERE id = $1
		  AND merchant_id = $2
		  AND is_default = TRUE
		  AND deleted_at IS NULL
		RETURNING updated_at
	`

	var updatedAt time.Time
	err := m.DB.QueryRow(ctx, query, paymentMethodID, merchantID).Scan(&updatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			logger.Warn("clear default skipped: not found, not owned, already cleared, or deleted",
				"payment_method_id", paymentMethodID, "merchant_id", merchantID)
			return ErrMerchantPaymentMethodNotFound
		}
		logger.Error("clear default merchant payment method failed", err, "payment_method_id", paymentMethodID, "merchant_id", merchantID)
		return err
	}

	logger.Info("clear default merchant payment method successful", "payment_method_id", paymentMethodID, "merchant_id", merchantID, "is_default", false)
	return nil
}

// ---------------------------------------------------------------------------
// Status transitions
// ---------------------------------------------------------------------------

// allowedPredecessorStatusesFor returns the legal predecessor statuses for a
// target lifecycle state. Revoked is terminal. Expired is a card-only state;
// type-specific enforcement occurs in UpdateStatus against the persisted row.
func allowedPredecessorStatusesFor(target MerchantPaymentMethodStatus) ([]string, error) {
	switch target {
	case PaymentMethodStatusActive:
		return []string{
			string(PaymentMethodStatusInactive),
			string(PaymentMethodStatusExpired),
		}, nil
	case PaymentMethodStatusInactive:
		return []string{string(PaymentMethodStatusActive)}, nil
	case PaymentMethodStatusExpired:
		return []string{
			string(PaymentMethodStatusActive),
			string(PaymentMethodStatusInactive),
		}, nil
	case PaymentMethodStatusRevoked:
		return []string{
			string(PaymentMethodStatusActive),
			string(PaymentMethodStatusInactive),
			string(PaymentMethodStatusExpired),
		}, nil
	default:
		return nil, fmt.Errorf("invalid merchant payment method target status: %s", target)
	}
}

// UpdateStatus atomically transitions a non-deleted merchant payment
// method's status, clearing is_default whenever the new status is not
// active. Expired is valid only for card methods; revoked is terminal.
//
// This is implemented as a single atomic UPDATE whose WHERE clause encodes
// both ownership and the legal-predecessor-status check, so a concurrent
// conflicting transition simply yields zero rows updated rather than
// requiring a separate transaction. Soft-deleted rows are never matched.
func (m *MerchantPaymentMethodModel) UpdateStatus(ctx context.Context, merchantID, paymentMethodID uuid.UUID, newStatus MerchantPaymentMethodStatus) error {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("UpdateMerchantPaymentMethodStatus")

	if merchantID == uuid.Nil || paymentMethodID == uuid.Nil {
		err := errors.New("merchant id and payment method id are required")
		logger.Error("validation failed", err)
		return err
	}

	newStatus = NormalizeMerchantPaymentMethodStatus(newStatus)
	if !IsValidMerchantPaymentMethodStatus(newStatus) {
		err := fmt.Errorf("invalid merchant payment method status: %s", newStatus)
		logger.Error("validation failed", err)
		return err
	}

	predecessors, err := allowedPredecessorStatusesFor(newStatus)
	if err != nil {
		logger.Error("validation failed", err)
		return err
	}

	const query = `
		UPDATE merchant_payment_methods
		SET
			status = $1,
			is_default = CASE WHEN $1 = 'active' THEN is_default ELSE FALSE END
		WHERE id = $2
		  AND merchant_id = $3
		  AND deleted_at IS NULL
		  AND status = ANY($4)
		  AND ($1 <> 'expired' OR payment_method_type = 'card')
		RETURNING is_default, updated_at
	`

	var isDefault bool
	var updatedAt time.Time
	err = m.DB.QueryRow(ctx, query, newStatus, paymentMethodID, merchantID, predecessors).Scan(&isDefault, &updatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			logger.Warn("status transition rejected: not found, wrong merchant, deleted, or invalid predecessor status",
				"payment_method_id", paymentMethodID, "merchant_id", merchantID, "status", newStatus)
			return ErrMerchantPaymentMethodInvalidState
		}
		logger.Error("update merchant payment method status failed", err, "payment_method_id", paymentMethodID, "merchant_id", merchantID, "status", newStatus)
		return err
	}

	logger.Info("update merchant payment method status successful",
		"payment_method_id", paymentMethodID, "merchant_id", merchantID, "status", newStatus, "is_default", isDefault)
	return nil
}

// ---------------------------------------------------------------------------
// SoftDelete / Restore / HardDelete
// ---------------------------------------------------------------------------

// SoftDelete marks a non-deleted merchant payment method as deleted using
// database-owned NOW(), atomically clearing is_default in the same
// statement so a deleted method is never left as the merchant's operational
// default. Provider-link history is owned and preserved by its own domain.
func (m *MerchantPaymentMethodModel) SoftDelete(ctx context.Context, merchantID, paymentMethodID uuid.UUID) error {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("SoftDeleteMerchantPaymentMethod")

	if merchantID == uuid.Nil || paymentMethodID == uuid.Nil {
		err := errors.New("merchant id and payment method id are required")
		logger.Error("validation failed", err)
		return err
	}

	const query = `
		UPDATE merchant_payment_methods
		SET deleted_at = NOW(),
		    is_default = FALSE
		WHERE id = $1
		  AND merchant_id = $2
		  AND deleted_at IS NULL
		RETURNING deleted_at
	`

	var deletedAt time.Time
	err := m.DB.QueryRow(ctx, query, paymentMethodID, merchantID).Scan(&deletedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			logger.Warn("soft delete skipped: not found, not owned, or already deleted", "payment_method_id", paymentMethodID, "merchant_id", merchantID)
			return ErrMerchantPaymentMethodNotFound
		}
		logger.Error("soft delete merchant payment method failed", err, "payment_method_id", paymentMethodID, "merchant_id", merchantID)
		return err
	}

	logger.Info("soft delete merchant payment method successful", "payment_method_id", paymentMethodID, "merchant_id", merchantID, "deleted_at", deletedAt)
	return nil
}

// Restore clears deleted_at for a soft-deleted merchant payment method.
//
// Restore intentionally does not touch status or is_default: status is
// preserved exactly as it was before deletion (SoftDelete never mutates
// status), and is_default remains false (SoftDelete always clears it and
// Restore does not set it back). This means Restore can never turn a
// revoked or expired method back into an active one, and never silently
// restores default status. If the restored method should become active
// and/or default again, callers must invoke UpdateStatus and SetDefault as
// explicit subsequent steps.
func (m *MerchantPaymentMethodModel) Restore(ctx context.Context, merchantID, paymentMethodID uuid.UUID) error {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("RestoreMerchantPaymentMethod")

	if merchantID == uuid.Nil || paymentMethodID == uuid.Nil {
		err := errors.New("merchant id and payment method id are required")
		logger.Error("validation failed", err)
		return err
	}

	const query = `
		UPDATE merchant_payment_methods
		SET deleted_at = NULL
		WHERE id = $1
		  AND merchant_id = $2
		  AND deleted_at IS NOT NULL
		RETURNING updated_at
	`

	var updatedAt time.Time
	err := m.DB.QueryRow(ctx, query, paymentMethodID, merchantID).Scan(&updatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			logger.Warn("restore skipped: not found, not owned, or not soft-deleted", "payment_method_id", paymentMethodID, "merchant_id", merchantID)
			return ErrMerchantPaymentMethodNotFound
		}
		logger.Error("restore merchant payment method failed", err, "payment_method_id", paymentMethodID, "merchant_id", merchantID)
		return err
	}

	logger.Info("restore merchant payment method successful", "payment_method_id", paymentMethodID, "merchant_id", merchantID)
	return nil
}

// HardDelete permanently, physically deletes a merchant payment method row.
//
// HardDelete is distinct from SoftDelete and is never an alias for it.
// Ordinary merchant flows must use SoftDelete or UpdateStatus (revoked).
// HardDelete is intended only for tightly controlled administrative,
// retention-policy, test-cleanup, or legally approved erasure paths, and is
// therefore id-scoped (global) rather than merchant-scoped, matching the
// administrative nature of this operation.
func (m *MerchantPaymentMethodModel) HardDelete(ctx context.Context, id uuid.UUID) error {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("HardDeleteMerchantPaymentMethod")

	if id == uuid.Nil {
		err := errors.New("merchant payment method id is required")
		logger.Error("validation failed", err)
		return err
	}

	const query = `
		DELETE FROM merchant_payment_methods
		WHERE id = $1
		RETURNING id
	`

	var deletedID uuid.UUID
	err := m.DB.QueryRow(ctx, query, id).Scan(&deletedID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			logger.Warn("hard delete skipped: not found", "payment_method_id", id)
			return ErrMerchantPaymentMethodNotFound
		}
		logger.Error("hard delete merchant payment method failed", err, "payment_method_id", id)
		return err
	}

	logger.Info("hard delete merchant payment method successful", "payment_method_id", deletedID)
	return nil
}
