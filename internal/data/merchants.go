// Package data provides canonical persistence for Merchant identity.
//
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
		return fmt.Errorf("insert merchant: %w", err)
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
func (m *MerchantModel) Update(
	ctx context.Context,
	merchant *Merchant,
) error {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.
		GetLoggerWithContextFromContext(ctx).
		WithFunctionName("UpdateMerchant")

	if merchant == nil {
		err := errors.New("merchant is required")
		logger.Error("Merchant validation failed", err)
		return err
	}

	if merchant.ID == uuid.Nil {
		err := errors.New("merchant ID is required")
		logger.Error("Merchant validation failed", err)
		return err
	}

	if err := validateAndNormalizeMerchant(merchant); err != nil {
		logger.Error(
			"Merchant validation failed",
			err,
			"merchant_id", merchant.ID,
		)
		return err
	}

	const query = `
		UPDATE merchants
		SET
			name = $1,
			logo_url = $2,
			website = $3,
			updated_at = NOW()
		WHERE id = $4
		RETURNING ` + merchantSelectColumns + `
	`

	if err := scanMerchant(
		m.DB.QueryRow(
			ctx,
			query,
			merchant.Name,
			merchant.LogoURL,
			merchant.Website,
			merchant.ID,
		),
		merchant,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrMerchantNotFound
		}
		if isMerchantNameConflict(err) {
			return ErrMerchantIdentityConflict
		}

		logger.Error(
			"Update merchant failed",
			err,
			"merchant_id", merchant.ID,
		)

		return fmt.Errorf("update merchant: %w", err)
	}

	logger.Info(
		"Merchant updated",
		"merchant_id", merchant.ID,
	)

	return nil
}
