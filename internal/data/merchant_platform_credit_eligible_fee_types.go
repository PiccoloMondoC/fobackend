// Package data provides models and database access methods for merchant
// platform credit eligibility by fee type.
//
// sdworkspace/sdbackend/internal/data/merchant_platform_credit_eligible_fee_types.go
//
// GTM:
//
//	Layer: 2.4.b Commerce Architecture / Monetization Layer
//	Release Class: SPINE
//	Reason:
//	  merchant_platform_credit_eligible_fee_types records the canonical fee
//	  types against which a platform-issued merchant commercial credit may be
//	  applied. Billing and credit-application orchestration depend on this
//	  association being structurally valid, transaction-safe, and free of
//	  embedded commercial policy.
//
// Domain Boundary:
//
//	This file records fee-type eligibility only. It does not decide:
//
//	  - which merchants receive credit;
//	  - how much credit they receive;
//	  - which eligibility set Administration should assign;
//	  - whether Plans, Subscriptions, Launch Campaigns, or fees are enabled;
//	  - which credit account is consumed first;
//	  - whether an actor is authorized to configure eligibility; or
//	  - how credit is applied to an invoice or billable event.
//
//	Those decisions belong to Administration-governed configuration,
//	authorization, and higher-layer commercial orchestration.
//
// SPINE Rule:
//
//	Keep compiling.
//	Keep production-ready.
//	Preserve the composite primary key.
//	Preserve parent-account foreign-key integrity.
//	Preserve canonical shared fee vocabulary.
//	Preserve atomic, serialized eligibility-set replacement.
//	Never hard-code current commercial eligibility policy.
//	Block deployment if this file breaks build, relational integrity,
//	concurrency safety, or merchant billing readiness.
package data

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/PiccoloMondoC/sdworkspace/sdbackend/internal/logging"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

const merchantPlatformCreditEligibleFeeTypeSelectColumns = `
	credit_account_id,
	fee_type,
	created_at
`

// MerchantPlatformCreditEligibleFeeType represents one canonical fee-type
// eligibility association for a merchant platform credit account.
//
// The entity identity is the composite key (CreditAccountID, FeeType). It has
// no synthetic identifier or independent soft-delete lifecycle.
type MerchantPlatformCreditEligibleFeeType struct {
	CreditAccountID uuid.UUID       `json:"credit_account_id" db:"credit_account_id"`
	FeeType         MerchantFeeType `json:"fee_type" db:"fee_type"`
	CreatedAt       time.Time       `json:"created_at" db:"created_at"`
}

// MerchantPlatformCreditEligibleFeeTypeModel owns persistence for merchant
// platform credit fee-type eligibility associations.
type MerchantPlatformCreditEligibleFeeTypeModel struct {
	DB     *pgxpool.Pool
	Logger *logging.Logger
}

// merchantPlatformCreditEligibleFeeTypeQuerier is the minimum query surface
// shared by *pgxpool.Pool and pgx.Tx for this model.
type merchantPlatformCreditEligibleFeeTypeQuerier interface {
	QueryRow(ctx context.Context, sql string, args ...interface{}) pgx.Row
	Exec(ctx context.Context, sql string, args ...interface{}) (pgconn.CommandTag, error)
}

func merchantPlatformCreditEligibleFeeTypeInvalidInput(
	format string,
	args ...interface{},
) error {
	return fmt.Errorf(
		"%w: %s",
		ErrMerchantPlatformCreditEligibleFeeTypeInvalidInput,
		fmt.Sprintf(format, args...),
	)
}

func scanMerchantPlatformCreditEligibleFeeType(
	row scannableRow,
	association *MerchantPlatformCreditEligibleFeeType,
) error {
	return row.Scan(
		&association.CreditAccountID,
		&association.FeeType,
		&association.CreatedAt,
	)
}

// IsMerchantPlatformCreditEligibleFeeTypeVocabulary reports whether feeType
// belongs to the structural fee vocabulary accepted by
// merchant_platform_credit_eligible_fee_types.
//
// This is not a commercial-policy decision. It only mirrors the table CHECK
// constraint. Administration remains free to assign any subset of these
// canonical fee types to any credit account.
func IsMerchantPlatformCreditEligibleFeeTypeVocabulary(
	feeType MerchantFeeType,
) bool {
	switch NormalizeMerchantFeeType(feeType) {
	case MerchantFeeTypeAnticipationIntelligenceActivation,
		MerchantFeeTypeAnticipationIntelligence,
		MerchantFeeTypeCampaignPerformance,
		MerchantFeeTypeSubscription:
		return true
	default:
		return false
	}
}

func validateMerchantPlatformCreditEligibleFeeTypeKey(
	creditAccountID uuid.UUID,
	feeType MerchantFeeType,
) (MerchantFeeType, error) {
	if creditAccountID == uuid.Nil {
		return "", merchantPlatformCreditEligibleFeeTypeInvalidInput(
			"credit_account_id is required",
		)
	}

	feeType = NormalizeMerchantFeeType(feeType)
	if !IsMerchantPlatformCreditEligibleFeeTypeVocabulary(feeType) {
		return "", merchantPlatformCreditEligibleFeeTypeInvalidInput(
			"invalid fee_type: %q",
			feeType,
		)
	}

	return feeType, nil
}

func validateMerchantPlatformCreditEligibleFeeTypeAccountID(
	creditAccountID uuid.UUID,
) error {
	if creditAccountID == uuid.Nil {
		return merchantPlatformCreditEligibleFeeTypeInvalidInput(
			"credit_account_id is required",
		)
	}
	return nil
}

func translateMerchantPlatformCreditEligibleFeeTypeWriteError(err error) error {
	switch {
	case IsUniqueViolation(err):
		return fmt.Errorf(
			"%w: %v",
			ErrMerchantPlatformCreditEligibleFeeTypeAlreadyExists,
			err,
		)
	case IsForeignKeyViolation(err):
		return fmt.Errorf(
			"%w: %v",
			ErrMerchantPlatformCreditAccountNotFound,
			err,
		)
	case IsCheckViolation(err), IsNotNullViolation(err):
		return fmt.Errorf(
			"%w: database constraint rejected the association: %v",
			ErrMerchantPlatformCreditEligibleFeeTypeInvalidInput,
			err,
		)
	default:
		return err
	}
}

func (m *MerchantPlatformCreditEligibleFeeTypeModel) insertWithQuerier(
	ctx context.Context,
	q merchantPlatformCreditEligibleFeeTypeQuerier,
	creditAccountID uuid.UUID,
	feeType MerchantFeeType,
) (*MerchantPlatformCreditEligibleFeeType, error) {
	const query = `
		INSERT INTO merchant_platform_credit_eligible_fee_types (
			credit_account_id,
			fee_type
		)
		VALUES ($1, $2)
		RETURNING ` + merchantPlatformCreditEligibleFeeTypeSelectColumns

	var association MerchantPlatformCreditEligibleFeeType
	err := scanMerchantPlatformCreditEligibleFeeType(
		q.QueryRow(ctx, query, creditAccountID, feeType),
		&association,
	)
	if err != nil {
		return nil, translateMerchantPlatformCreditEligibleFeeTypeWriteError(err)
	}

	return &association, nil
}

// Insert creates one fee-type eligibility association.
//
// A duplicate association is an explicit error rather than idempotent success.
// Use InsertTx when this mutation must be composed with another transaction.
func (m *MerchantPlatformCreditEligibleFeeTypeModel) Insert(
	ctx context.Context,
	creditAccountID uuid.UUID,
	feeType MerchantFeeType,
) (*MerchantPlatformCreditEligibleFeeType, error) {
	return m.InsertTx(ctx, m.DB, creditAccountID, feeType)
}

// InsertTx is the transaction-aware form of Insert.
//
// q may be *pgxpool.Pool or pgx.Tx. The caller owns transaction commit or
// rollback when q is a transaction.
func (m *MerchantPlatformCreditEligibleFeeTypeModel) InsertTx(
	ctx context.Context,
	q merchantPlatformCreditEligibleFeeTypeQuerier,
	creditAccountID uuid.UUID,
	feeType MerchantFeeType,
) (*MerchantPlatformCreditEligibleFeeType, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.
		GetLoggerWithContextFromContext(ctx).
		WithFunctionName("InsertMerchantPlatformCreditEligibleFeeType")

	if q == nil {
		err := merchantPlatformCreditEligibleFeeTypeInvalidInput(
			"querier is required",
		)
		logger.Error("validation failed", err)
		return nil, err
	}

	normalizedFeeType, err := validateMerchantPlatformCreditEligibleFeeTypeKey(
		creditAccountID,
		feeType,
	)
	if err != nil {
		logger.Error("validation failed", err)
		return nil, err
	}

	association, err := m.insertWithQuerier(
		ctx,
		q,
		creditAccountID,
		normalizedFeeType,
	)
	if err != nil {
		logger.Error(
			"insert merchant platform credit eligible fee type failed",
			err,
			"credit_account_id", creditAccountID,
			"fee_type", normalizedFeeType,
		)
		return nil, err
	}

	logger.Info(
		"insert merchant platform credit eligible fee type successful",
		"credit_account_id", association.CreditAccountID,
		"fee_type", association.FeeType,
	)

	return association, nil
}

// Get retrieves one association by composite key.
//
// Absence is a normal result and returns (nil, nil).
func (m *MerchantPlatformCreditEligibleFeeTypeModel) Get(
	ctx context.Context,
	creditAccountID uuid.UUID,
	feeType MerchantFeeType,
) (*MerchantPlatformCreditEligibleFeeType, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.
		GetLoggerWithContextFromContext(ctx).
		WithFunctionName("GetMerchantPlatformCreditEligibleFeeType")

	normalizedFeeType, err := validateMerchantPlatformCreditEligibleFeeTypeKey(
		creditAccountID,
		feeType,
	)
	if err != nil {
		logger.Error("validation failed", err)
		return nil, err
	}

	const query = `
		SELECT ` + merchantPlatformCreditEligibleFeeTypeSelectColumns + `
		FROM merchant_platform_credit_eligible_fee_types
		WHERE credit_account_id = $1
		  AND fee_type = $2
	`

	var association MerchantPlatformCreditEligibleFeeType
	err = scanMerchantPlatformCreditEligibleFeeType(
		m.DB.QueryRow(ctx, query, creditAccountID, normalizedFeeType),
		&association,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}

		logger.Error(
			"get merchant platform credit eligible fee type failed",
			err,
			"credit_account_id", creditAccountID,
			"fee_type", normalizedFeeType,
		)
		return nil, err
	}

	return &association, nil
}

// ListByCreditAccount returns all associations for creditAccountID ordered by
// canonical fee type.
//
// No rows returns a non-nil empty slice. Pagination is unnecessary because the
// table CHECK constraint bounds the result to four fee types.
func (m *MerchantPlatformCreditEligibleFeeTypeModel) ListByCreditAccount(
	ctx context.Context,
	creditAccountID uuid.UUID,
) ([]*MerchantPlatformCreditEligibleFeeType, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.
		GetLoggerWithContextFromContext(ctx).
		WithFunctionName("ListMerchantPlatformCreditEligibleFeeTypesByCreditAccount")

	if err := validateMerchantPlatformCreditEligibleFeeTypeAccountID(
		creditAccountID,
	); err != nil {
		logger.Error("validation failed", err)
		return nil, err
	}

	const query = `
		SELECT ` + merchantPlatformCreditEligibleFeeTypeSelectColumns + `
		FROM merchant_platform_credit_eligible_fee_types
		WHERE credit_account_id = $1
		ORDER BY fee_type ASC
	`

	rows, err := m.DB.Query(ctx, query, creditAccountID)
	if err != nil {
		logger.Error(
			"list merchant platform credit eligible fee types failed",
			err,
			"credit_account_id", creditAccountID,
		)
		return nil, err
	}
	defer rows.Close()

	associations := make([]*MerchantPlatformCreditEligibleFeeType, 0, 4)
	for rows.Next() {
		var association MerchantPlatformCreditEligibleFeeType
		if err := scanMerchantPlatformCreditEligibleFeeType(
			rows,
			&association,
		); err != nil {
			logger.Error(
				"scan merchant platform credit eligible fee type failed",
				err,
				"credit_account_id", creditAccountID,
			)
			return nil, err
		}
		associations = append(associations, &association)
	}

	if err := rows.Err(); err != nil {
		logger.Error(
			"iterate merchant platform credit eligible fee types failed",
			err,
			"credit_account_id", creditAccountID,
		)
		return nil, err
	}

	return associations, nil
}

// IsEligible reports whether the composite-key association exists using the
// model's connection pool.
//
// false with a nil error is a valid negative result. Use IsEligibleTx when the
// eligibility decision must participate in a caller-owned transaction.
func (m *MerchantPlatformCreditEligibleFeeTypeModel) IsEligible(
	ctx context.Context,
	creditAccountID uuid.UUID,
	feeType MerchantFeeType,
) (bool, error) {
	return m.IsEligibleTx(
		ctx,
		m.DB,
		creditAccountID,
		feeType,
	)
}

// IsEligibleTx is the transaction-aware form of IsEligible.
//
// q may be *pgxpool.Pool or pgx.Tx. When q is a transaction, the eligibility
// check observes that transaction's snapshot and any eligibility mutations
// already performed through the same transaction.
//
// false with a nil error is a valid negative result. The caller owns commit or
// rollback when q is a transaction.
func (m *MerchantPlatformCreditEligibleFeeTypeModel) IsEligibleTx(
	ctx context.Context,
	q merchantPlatformCreditEligibleFeeTypeQuerier,
	creditAccountID uuid.UUID,
	feeType MerchantFeeType,
) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.
		GetLoggerWithContextFromContext(ctx).
		WithFunctionName(
			"IsMerchantPlatformCreditEligibleFeeType",
		)

	if q == nil {
		err := merchantPlatformCreditEligibleFeeTypeInvalidInput(
			"querier is required",
		)
		logger.Error("validation failed", err)
		return false, err
	}

	normalizedFeeType, err :=
		validateMerchantPlatformCreditEligibleFeeTypeKey(
			creditAccountID,
			feeType,
		)
	if err != nil {
		logger.Error("validation failed", err)
		return false, err
	}

	const query = `
		SELECT EXISTS (
			SELECT 1
			FROM merchant_platform_credit_eligible_fee_types
			WHERE credit_account_id = $1
			  AND fee_type = $2
		)
	`

	var eligible bool
	if err := q.QueryRow(
		ctx,
		query,
		creditAccountID,
		normalizedFeeType,
	).Scan(&eligible); err != nil {
		logger.Error(
			"check merchant platform credit fee-type eligibility failed",
			err,
			"credit_account_id", creditAccountID,
			"fee_type", normalizedFeeType,
		)
		return false, err
	}

	return eligible, nil
}

// Delete removes one association.
//
// Delete is not idempotent. Absence returns
// ErrMerchantPlatformCreditEligibleFeeTypeNotFound.
func (m *MerchantPlatformCreditEligibleFeeTypeModel) Delete(
	ctx context.Context,
	creditAccountID uuid.UUID,
	feeType MerchantFeeType,
) error {
	return m.DeleteTx(ctx, m.DB, creditAccountID, feeType)
}

// DeleteTx is the transaction-aware form of Delete.
func (m *MerchantPlatformCreditEligibleFeeTypeModel) DeleteTx(
	ctx context.Context,
	q merchantPlatformCreditEligibleFeeTypeQuerier,
	creditAccountID uuid.UUID,
	feeType MerchantFeeType,
) error {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.
		GetLoggerWithContextFromContext(ctx).
		WithFunctionName("DeleteMerchantPlatformCreditEligibleFeeType")

	if q == nil {
		err := merchantPlatformCreditEligibleFeeTypeInvalidInput(
			"querier is required",
		)
		logger.Error("validation failed", err)
		return err
	}

	normalizedFeeType, err := validateMerchantPlatformCreditEligibleFeeTypeKey(
		creditAccountID,
		feeType,
	)
	if err != nil {
		logger.Error("validation failed", err)
		return err
	}

	const query = `
		DELETE FROM merchant_platform_credit_eligible_fee_types
		WHERE credit_account_id = $1
		  AND fee_type = $2
		RETURNING fee_type
	`

	var deleted MerchantFeeType
	err = q.QueryRow(
		ctx,
		query,
		creditAccountID,
		normalizedFeeType,
	).Scan(&deleted)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrMerchantPlatformCreditEligibleFeeTypeNotFound
		}

		logger.Error(
			"delete merchant platform credit eligible fee type failed",
			err,
			"credit_account_id", creditAccountID,
			"fee_type", normalizedFeeType,
		)
		return err
	}

	logger.Info(
		"delete merchant platform credit eligible fee type successful",
		"credit_account_id", creditAccountID,
		"fee_type", normalizedFeeType,
	)

	return nil
}

// DeleteAllForCreditAccount removes every eligibility association for a
// retained parent credit account.
//
// This operation is idempotent and returns the number of deleted rows.
func (m *MerchantPlatformCreditEligibleFeeTypeModel) DeleteAllForCreditAccount(
	ctx context.Context,
	creditAccountID uuid.UUID,
) (int64, error) {
	return m.DeleteAllForCreditAccountTx(ctx, m.DB, creditAccountID)
}

// DeleteAllForCreditAccountTx is the transaction-aware form of
// DeleteAllForCreditAccount.
func (m *MerchantPlatformCreditEligibleFeeTypeModel) DeleteAllForCreditAccountTx(
	ctx context.Context,
	q merchantPlatformCreditEligibleFeeTypeQuerier,
	creditAccountID uuid.UUID,
) (int64, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.
		GetLoggerWithContextFromContext(ctx).
		WithFunctionName("DeleteAllMerchantPlatformCreditEligibleFeeTypesForCreditAccount")

	if q == nil {
		err := merchantPlatformCreditEligibleFeeTypeInvalidInput(
			"querier is required",
		)
		logger.Error("validation failed", err)
		return 0, err
	}

	if err := validateMerchantPlatformCreditEligibleFeeTypeAccountID(
		creditAccountID,
	); err != nil {
		logger.Error("validation failed", err)
		return 0, err
	}

	const query = `
		DELETE FROM merchant_platform_credit_eligible_fee_types
		WHERE credit_account_id = $1
	`

	tag, err := q.Exec(ctx, query, creditAccountID)
	if err != nil {
		logger.Error(
			"delete all merchant platform credit eligible fee types failed",
			err,
			"credit_account_id", creditAccountID,
		)
		return 0, err
	}

	logger.Info(
		"delete all merchant platform credit eligible fee types successful",
		"credit_account_id", creditAccountID,
		"row_count", tag.RowsAffected(),
	)

	return tag.RowsAffected(), nil
}

func canonicalizeMerchantPlatformCreditEligibleFeeTypeSet(
	feeTypes []MerchantFeeType,
) ([]MerchantFeeType, error) {
	set := make(map[MerchantFeeType]struct{}, len(feeTypes))

	for _, feeType := range feeTypes {
		feeType = NormalizeMerchantFeeType(feeType)
		if !IsMerchantPlatformCreditEligibleFeeTypeVocabulary(feeType) {
			return nil, merchantPlatformCreditEligibleFeeTypeInvalidInput(
				"invalid fee_type: %q",
				feeType,
			)
		}
		set[feeType] = struct{}{}
	}

	canonical := make([]MerchantFeeType, 0, len(set))
	for feeType := range set {
		canonical = append(canonical, feeType)
	}

	sort.Slice(canonical, func(i, j int) bool {
		return canonical[i] < canonical[j]
	})

	return canonical, nil
}

func lockMerchantPlatformCreditAccountForEligibilityReplacement(
	ctx context.Context,
	tx pgx.Tx,
	creditAccountID uuid.UUID,
) error {
	const query = `
		SELECT id
		FROM merchant_platform_credit_accounts
		WHERE id = $1
		FOR UPDATE
	`

	var id uuid.UUID
	err := tx.QueryRow(ctx, query, creditAccountID).Scan(&id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrMerchantPlatformCreditAccountNotFound
		}
		return err
	}

	return nil
}

// ReplaceSet atomically makes feeTypes the complete eligibility set for one
// credit account.
//
// The method validates the entire input before mutation, canonicalizes and
// deduplicates fee types, serializes concurrent replacements by locking the
// parent credit-account row, and performs delete-and-reinsert in one
// transaction. An empty slice is valid and produces an empty eligibility set
// for an existing credit account.
func (m *MerchantPlatformCreditEligibleFeeTypeModel) ReplaceSet(
	ctx context.Context,
	creditAccountID uuid.UUID,
	feeTypes []MerchantFeeType,
) ([]*MerchantPlatformCreditEligibleFeeType, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.
		GetLoggerWithContextFromContext(ctx).
		WithFunctionName("ReplaceMerchantPlatformCreditEligibleFeeTypeSet")

	if err := validateMerchantPlatformCreditEligibleFeeTypeAccountID(
		creditAccountID,
	); err != nil {
		logger.Error("validation failed", err)
		return nil, err
	}

	canonicalFeeTypes, err :=
		canonicalizeMerchantPlatformCreditEligibleFeeTypeSet(feeTypes)
	if err != nil {
		logger.Error("validation failed", err)
		return nil, err
	}

	tx, err := m.DB.Begin(ctx)
	if err != nil {
		logger.Error(
			"begin merchant platform credit eligibility replacement failed",
			err,
			"credit_account_id", creditAccountID,
		)
		return nil, err
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	replaced, err := m.replaceSetTx(
		ctx,
		tx,
		creditAccountID,
		canonicalFeeTypes,
	)
	if err != nil {
		logger.Error(
			"replace merchant platform credit eligibility set failed",
			err,
			"credit_account_id", creditAccountID,
		)
		return nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		logger.Error(
			"commit merchant platform credit eligibility replacement failed",
			err,
			"credit_account_id", creditAccountID,
		)
		return nil, err
	}

	logger.Info(
		"replace merchant platform credit eligibility set successful",
		"credit_account_id", creditAccountID,
		"row_count", len(replaced),
	)

	return replaced, nil
}

// ReplaceSetTx replaces the complete eligibility set inside a caller-owned
// transaction.
//
// The method locks the parent credit-account row and therefore serializes
// concurrent replacements for the same account. The caller owns commit or
// rollback.
func (m *MerchantPlatformCreditEligibleFeeTypeModel) ReplaceSetTx(
	ctx context.Context,
	tx pgx.Tx,
	creditAccountID uuid.UUID,
	feeTypes []MerchantFeeType,
) ([]*MerchantPlatformCreditEligibleFeeType, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.
		GetLoggerWithContextFromContext(ctx).
		WithFunctionName("ReplaceMerchantPlatformCreditEligibleFeeTypeSetTx")

	if tx == nil {
		err := merchantPlatformCreditEligibleFeeTypeInvalidInput(
			"transaction is required",
		)
		logger.Error("validation failed", err)
		return nil, err
	}

	if err := validateMerchantPlatformCreditEligibleFeeTypeAccountID(
		creditAccountID,
	); err != nil {
		logger.Error("validation failed", err)
		return nil, err
	}

	canonicalFeeTypes, err :=
		canonicalizeMerchantPlatformCreditEligibleFeeTypeSet(feeTypes)
	if err != nil {
		logger.Error("validation failed", err)
		return nil, err
	}

	replaced, err := m.replaceSetTx(
		ctx,
		tx,
		creditAccountID,
		canonicalFeeTypes,
	)
	if err != nil {
		logger.Error(
			"replace merchant platform credit eligibility set in transaction failed",
			err,
			"credit_account_id", creditAccountID,
		)
		return nil, err
	}

	return replaced, nil
}

func (m *MerchantPlatformCreditEligibleFeeTypeModel) replaceSetTx(
	ctx context.Context,
	tx pgx.Tx,
	creditAccountID uuid.UUID,
	canonicalFeeTypes []MerchantFeeType,
) ([]*MerchantPlatformCreditEligibleFeeType, error) {
	if err := lockMerchantPlatformCreditAccountForEligibilityReplacement(
		ctx,
		tx,
		creditAccountID,
	); err != nil {
		return nil, err
	}

	const deleteQuery = `
		DELETE FROM merchant_platform_credit_eligible_fee_types
		WHERE credit_account_id = $1
	`

	if _, err := tx.Exec(ctx, deleteQuery, creditAccountID); err != nil {
		return nil, err
	}

	replaced := make(
		[]*MerchantPlatformCreditEligibleFeeType,
		0,
		len(canonicalFeeTypes),
	)

	for _, feeType := range canonicalFeeTypes {
		association, err := m.insertWithQuerier(
			ctx,
			tx,
			creditAccountID,
			feeType,
		)
		if err != nil {
			return nil, err
		}
		replaced = append(replaced, association)
	}

	return replaced, nil
}
