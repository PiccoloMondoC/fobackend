// Package data provides canonical merchant identity and coordination with private business registration.
// focodebase/fobackend/internal/data/merchants.go
//
// GTM:
//
//	Layer: 2.4 Merchant / Future Offering Domain
//	Release Class: SPINE
//	Reason:
//	  Merchant is the durable commercial identity behind Future Offerings.
//	  The Future Offering platform requires a canonical Merchant identity so
//	  that Merchant Accounts and Future Offerings can refer to the entity
//	  responsible for an offering without conflating Merchant identity with
//	  user identity, account lifecycle, FO commercial state, or consumer
//	  disclosure state.
//
// SPINE Rule:
//
//	Keep compiling.
//	Keep production-ready.
//	Preserve Merchant as a pure durable identity record.
//	Preserve the boundary between Merchant, User, Merchant Account, and
//	Future Offering.
//	Preserve canonical validated URL values for logo_url and website.
//	Preserve the distinction between a Merchant's general website and any
//	FO-, Engagement-Action-, or Engagement-Action-Group-specific consumer
//	handoff destination.
//	Preserve database-owned lifecycle timestamps.
//	Preserve typed translation of caller-correctable validation failures
//	(ErrMerchantInvalid) and canonical-name conflicts
//	(ErrMerchantIdentityConflict).
//	Do not introduce account lifecycle, Merchant classification, public
//	routing, catalog, affiliate, platform, engagement, billing, or
//	FO-specific state into Merchant persistence.
//	Block deployment if this file breaks Merchant identity persistence or
//	Merchant / Merchant Account / Future Offering referential integrity.
package data

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/PiccoloMondoC/focodebase/fobackend/internal/logging"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// merchantSelectColumns is the canonical persisted Merchant projection.
//
// Its order is significant and must remain aligned with scanMerchant.
const merchantSelectColumns = `
	id,
	name,
	logo_url,
	website,
	created_at,
	updated_at
`

// merchantNameUniqueConstraint is the canonical uniqueness constraint over
// Merchant name. The column is CITEXT, so uniqueness is case-insensitive.
const merchantNameUniqueConstraint = "ux_merchants_name"

var ErrMerchantRegistrationManaged = errors.New("use merchant registration to manage this identity")

// Merchant is the canonical durable commercial identity behind one or more
// Future Offerings.
//
// Merchant is distinct from User and Merchant Account. User owns Platform
// authentication identity. Merchant Account owns the principal-user
// relationship and account lifecycle. Future Offering owns FO-specific
// commercial and lifecycle state.
//
// Merchant persists identity facts only. Persistence of those facts does not
// authorize their disclosure to consumers. Consumer-facing disclosure is
// governed above this persistence layer.
type Merchant struct {
	ID uuid.UUID `json:"id" db:"id"`

	// Name is the Merchant's canonical identifying name.
	Name string `json:"name" db:"name"`

	// LogoURL is an optional validated and canonical HTTP(S) URL identifying
	// the Merchant's brand mark. Nil means no logo has been supplied.
	LogoURL *string `json:"logo_url,omitempty" db:"logo_url"`

	// Website is the Merchant's optional validated and canonical general
	// website.
	//
	// Website is Merchant identity information. It is not an actionable
	// Future Offering, Engagement Action, or Engagement Action Group consumer
	// handoff destination. Such destinations belong to their corresponding
	// FO-specific domains.
	Website *string `json:"website,omitempty" db:"website"`

	CreatedAt time.Time `json:"created_at" db:"created_at"`
	UpdatedAt time.Time `json:"updated_at" db:"updated_at"`
}

// MerchantModel owns canonical Merchant identity persistence.
type MerchantModel struct {
	DB     *pgxpool.Pool
	Logger *logging.Logger
}

// scanMerchant scans the canonical merchantSelectColumns projection.
func scanMerchant(row scannableRow, merchant *Merchant) error {
	return row.Scan(
		&merchant.ID,
		&merchant.Name,
		&merchant.LogoURL,
		&merchant.Website,
		&merchant.CreatedAt,
		&merchant.UpdatedAt,
	)
}

// isMerchantNameConflict reports whether err is a unique violation of the
// canonical Merchant-name constraint. SQLSTATE and constraint inspection are
// delegated to the central PostgreSQL helpers in errors.go.
func isMerchantNameConflict(err error) bool {
	return IsUniqueViolation(err) &&
		IsPgConstraint(err, merchantNameUniqueConstraint)
}

// normalizeMerchantURLField normalizes an optional Merchant URL and, when
// present, validates and canonicalizes it using the package's shared HTTP URL
// validator.
//
// The returned value is the canonical value that must be used downstream.
// Validation failures wrap ErrMerchantInvalid.
func normalizeMerchantURLField(
	raw *string,
	fieldName string,
) (*string, error) {
	cleaned := normalizeOptionalString(raw)
	if cleaned == nil {
		return nil, nil
	}

	canonical, err := validateHTTPURL(*cleaned)
	if err != nil {
		return nil, fmt.Errorf("%w: %s: %w", ErrMerchantInvalid, fieldName, err)
	}

	return &canonical, nil
}

// validateAndNormalizeMerchant validates and canonicalizes mutable Merchant
// identity facts in place.
//
// Callers must persist the resulting canonical values rather than the original
// unvalidated input. Caller-correctable identity failures wrap
// ErrMerchantInvalid so boundary layers can classify them as input errors. A
// nil merchant is a programming error and is not classified as input.
func validateAndNormalizeMerchant(merchant *Merchant) error {
	if merchant == nil {
		return errors.New("merchant is required")
	}

	name := strings.TrimSpace(merchant.Name)
	if name == "" {
		return fmt.Errorf("%w: merchant name is required", ErrMerchantInvalid)
	}
	merchant.Name = name

	logoURL, err := normalizeMerchantURLField(
		merchant.LogoURL,
		"merchant logo URL",
	)
	if err != nil {
		return err
	}
	merchant.LogoURL = logoURL

	website, err := normalizeMerchantURLField(
		merchant.Website,
		"merchant website",
	)
	if err != nil {
		return err
	}
	merchant.Website = website

	return nil
}

// InsertTx creates a Merchant inside a caller-owned transaction.
//
// Merchant creation participates in the atomic merchant-onboarding workflow
// that also creates the principal-owned Merchant Account. Transaction
// orchestration belongs to the service layer; this method owns only the
// Merchant persistence operation.
//
// If merchant.ID is uuid.Nil, InsertTx generates a new identifier.
// created_at and updated_at are database-owned and returned to the caller.
//
// Returns an error wrapping ErrMerchantInvalid when identity facts fail
// validation, and ErrMerchantIdentityConflict when the canonical name is
// already held by another Merchant.
func (m *MerchantModel) InsertTx(
	ctx context.Context,
	tx pgx.Tx,
	merchant *Merchant,
) error {
	if tx == nil {
		return errors.New("merchant transaction is required")
	}
	if merchant == nil {
		return errors.New("merchant is required")
	}

	if err := validateAndNormalizeMerchant(merchant); err != nil {
		return err
	}

	if merchant.ID == uuid.Nil {
		merchant.ID = uuid.New()
	}

	const query = `
		INSERT INTO merchants (
			id,
			name,
			logo_url,
			website
		)
		VALUES ($1, $2, $3, $4)
		RETURNING ` + merchantSelectColumns + `
	`

	if err := scanMerchant(
		tx.QueryRow(
			ctx,
			query,
			merchant.ID,
			merchant.Name,
			merchant.LogoURL,
			merchant.Website,
		),
		merchant,
	); err != nil {
		if isMerchantNameConflict(err) {
			return ErrMerchantIdentityConflict
		}
		return SafeMerchantRegistrationError("insert merchant", err)
	}

	return nil
}

// GetByID retrieves a Merchant by its canonical ID.
//
// Returns ErrMerchantNotFound when no Merchant exists with id.
func (m *MerchantModel) GetByID(
	ctx context.Context,
	id uuid.UUID,
) (*Merchant, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.
		GetLoggerWithContextFromContext(ctx).
		WithFunctionName("GetMerchantByID")

	if id == uuid.Nil {
		err := errors.New("merchant ID is required")
		logger.Error("Merchant validation failed", err)
		return nil, err
	}

	const query = `
		SELECT ` + merchantSelectColumns + `
		FROM merchants
		WHERE id = $1
	`

	var merchant Merchant

	if err := scanMerchant(
		m.DB.QueryRow(ctx, query, id),
		&merchant,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrMerchantNotFound
		}

		logger.Error(
			"Get merchant by ID failed",
			err,
			"merchant_id", id,
		)

		return nil, fmt.Errorf("get merchant by id: %w", err)
	}

	return &merchant, nil
}

// Update replaces the mutable identity facts of an existing Merchant.
//
// Name, LogoURL, and Website are validated and canonicalized before
// persistence. The canonical values returned by validation are the values
// written to the database.
//
// Returns an error wrapping ErrMerchantInvalid when identity facts fail
// validation, ErrMerchantIdentityConflict when the canonical name is already
// held by another Merchant, and ErrMerchantNotFound when no Merchant exists
// with merchant.ID.
func (m *MerchantModel) Update(ctx context.Context, merchant *Merchant) error {
    if merchant == nil || merchant.ID == uuid.Nil { return ErrMerchantInvalid }
    if err := validateAndNormalizeMerchant(merchant); err != nil { return err }
    ctx,cancel:=context.WithTimeout(ctx,dbTimeout);defer cancel()
    tx,err:=m.DB.BeginTx(ctx,pgx.TxOptions{});if err!=nil{return SafeMerchantRegistrationError("begin identity update",err)}
    defer func(){ cleanup,done:=context.WithTimeout(context.Background(),3*time.Second);defer done();_ = tx.Rollback(cleanup) }()
    // The old tuple writer participates in the same user-first lock order as
    // registration completion. A pre-check outside a transaction would race
    // completion and permit canonical DBA/private-registration divergence.
    var principal uuid.UUID
    err=tx.QueryRow(ctx,`SELECT principal_user_id FROM merchant_accounts WHERE merchant_id=$1`,merchant.ID).Scan(&principal)
    if err!=nil&&!errors.Is(err,pgx.ErrNoRows){return SafeMerchantRegistrationError("resolve identity owner",err)}
    if err==nil {
        var locked uuid.UUID
        if err=tx.QueryRow(ctx,`SELECT id FROM users WHERE id=$1 FOR UPDATE`,principal).Scan(&locked);err!=nil{
            return SafeMerchantRegistrationError("lock identity owner",err)
        }
        var registered bool
        if err=tx.QueryRow(ctx,`SELECT EXISTS(SELECT 1 FROM merchant_registrations
            WHERE principal_user_id=$1 AND registration_status='completed')`,principal).Scan(&registered);err!=nil{
            return SafeMerchantRegistrationError("resolve identity management",err)
        }
        if registered{return ErrMerchantRegistrationManaged}
    }
    err=scanMerchant(tx.QueryRow(ctx,`UPDATE merchants SET name=$1,logo_url=$2,website=$3,updated_at=NOW()
        WHERE id=$4 RETURNING `+merchantSelectColumns,merchant.Name,merchant.LogoURL,merchant.Website,merchant.ID),merchant)
    if errors.Is(err,pgx.ErrNoRows){return ErrMerchantNotFound}
    if isMerchantNameConflict(err){return ErrMerchantIdentityConflict}
    if err!=nil{return SafeMerchantRegistrationError("update identity",err)}
    return SafeMerchantRegistrationError("commit identity update",tx.Commit(ctx))
}

// UpdateNameTx replaces the canonical Merchant name inside a caller-owned
// transaction. Merchant registration uses it to keep the Merchant name equal
// to the registered public DBA / brand name. LogoURL is preserved. A historical website is cleared from the general
// identity once the owner submits the private registration website.
//
// Returns an error wrapping ErrMerchantInvalid when the name fails validation,
// ErrMerchantIdentityConflict when another Merchant holds the canonical name,
// and ErrMerchantNotFound when no Merchant exists with id.
func (m *MerchantModel) UpdateNameTx(
	ctx context.Context,
	tx pgx.Tx,
	id uuid.UUID,
	name string,
) (*Merchant, error) {
	if tx == nil {
		return nil, errors.New("merchant transaction is required")
	}
	if id == uuid.Nil {
		return nil, errors.New("merchant ID is required")
	}

	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return nil, fmt.Errorf("%w: merchant name is required", ErrMerchantInvalid)
	}

	const query = `
		UPDATE merchants
		SET
			name = $1,
            website = NULL,
			updated_at = NOW()
		WHERE id = $2 AND deleted_at IS NULL
		RETURNING ` + merchantSelectColumns + `
	`

	var merchant Merchant
	if err := scanMerchant(tx.QueryRow(ctx, query, trimmed, id), &merchant); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrMerchantNotFound
		}
		if isMerchantNameConflict(err) {
			return nil, ErrMerchantIdentityConflict
		}
		return nil, SafeMerchantRegistrationError("update merchant name", err)
	}

	return &merchant, nil
}
