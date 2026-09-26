// Package data provides models and database access methods for merchant future
// offerings.
//
// focodebase/fobackend/internal/data/merchant_future_offerings.go
//
// GTM:
//
//	Layer: 2.4 Merchant / Future Offering Domain
//	Release Class: SPINE
//	Reason:
//	  merchant_future_offerings is the authoritative Future Offering aggregate.
//	  It is the domain around which PCDF-M01 (Future Offering Creation) and all
//	  later FO-scoped capabilities are built.
//
// Domain Boundary:
//
//	A Future Offering record is one merchant-owned project: its identity,
//	classification, core descriptive facts, schedule facts, and lifecycle
//	status. It is not an assets store, an engagement-options store, a goals
//	store, a service/commercial-terms store, a milestone store, or an events
//	store. Those remain owned by their own domain files and reference this
//	aggregate by future_offering_id. This model does not reference offers or
//	products; neither table is part of the Future Offering v1 architecture.
//
// Draft Completeness Boundary:
//
//	Only project_name is required at creation; it is FO identity, required so
//	an incomplete project can be saved and later resumed. title, category_id,
//	offering_type, release_strategy, access_policy, and launch_at may all be
//	NULL while status = 'draft'. The database intentionally does not enforce
//	submission completeness through NOT NULL constraints. Cross-domain M01
//	submission readiness is established by service orchestration.
//
// Lifecycle:
//
//	draft -> submitted
//
//	This file intentionally implements only the PCDF-M01 boundary. trust_
//	review, changes_requested, approved, published, paused, expired, rejected,
//	unpublished, and archived are canonical states in the persisted vocabulary
//	but their transition rules and timestamp-coupling semantics belong to
//	PCDF-M02/PCDF-A02 and are not yet specified. No generic Update method can
//	set status or any lifecycle timestamp; every lifecycle timestamp is
//	DB-owned and set only by the guarded transition that owns it.
//
// Deletion Boundary:
//
//	deleted_at represents draft discard only. A submitted-or-later FO can
//	never be soft-deleted through this model. Ordinary reads always exclude
//	soft-deleted rows.
//
// Transaction Boundary:
//
//	InsertDraftTx, UpdateDraftFactsTx, SubmitTx, DiscardDraftTx, and
//	GetByIDForUpdateTx accept caller-owned pgx.Tx values and never begin,
//	commit, or roll them back.
//
//	Submission is intentionally transaction-only. It is expected to compose,
//	in the same service-owned transaction, with a FutureOfferingSubmitted
//	outbox insert (via OutboxEventModel) and any other domain writes
//	submission requires. This file does not call OutboxEventModel directly.
//
// Concurrency:
//
//	Draft mutation, submission, and discard use optimistic concurrency: the
//	caller supplies the updated_at value it last observed, and each guarded
//	UPDATE succeeds only while that version still matches the current row.
//	Merchant-facing mutations are also guarded by merchant ownership,
//	lifecycle state, and deletion state. Cross-domain submission readiness is
//	not redefined by this persistence model.
//
// SPINE Rule:
//
//	Keep compiling.
//	Keep production-ready.
//	Preserve draft-only mutation and draft-only discard.
//	Preserve transaction-only, atomically guarded submission.
//	Preserve optimistic concurrency on draft mutation.
//	Preserve exclusion of offer_id/product_id from this model.
//	Preserve deterministic bounded reads ordered by updated_at.
//	Do not add generic Update/Delete methods that bypass lifecycle guards.
//	Do not redefine cross-domain M01 submission readiness from FO-table facts.
//	Block deployment if this file breaks persistence integrity, lifecycle
//	integrity, concurrency safety, or Future Offering aggregate ownership.
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

const merchantFutureOfferingSelectColumns = `
	id,
	merchant_id,
	project_name,
	title,
	summary,
	description,
	category_id,
	offering_type,
	release_strategy,
	access_policy,
	status,
	launch_at,
	submitted_at,
	approved_at,
	published_at,
	rejected_at,
	unpublished_at,
	archived_at,
	created_at,
	updated_at,
	deleted_at
`

const (
	merchantFutureOfferingMerchantFKConstraint      = "merchant_future_offerings_merchant_id_fkey"
	merchantFutureOfferingCategoryFKConstraint      = "merchant_future_offerings_category_id_fkey"
	merchantFutureOfferingProjectNameConstraint     = "merchant_future_offerings_project_name_check"
	merchantFutureOfferingTitleConstraint           = "merchant_future_offerings_title_check"
	merchantFutureOfferingOfferingTypeConstraint    = "merchant_future_offerings_offering_type_check"
	merchantFutureOfferingReleaseStrategyConstraint = "merchant_future_offerings_release_strategy_check"
	merchantFutureOfferingAccessPolicyConstraint    = "merchant_future_offerings_access_policy_check"
	merchantFutureOfferingStatusConstraint          = "merchant_future_offerings_status_check"
)

// MerchantFutureOfferingStatus is the persisted lifecycle state of a Future Offering.
type MerchantFutureOfferingStatus string

const (
	MerchantFutureOfferingStatusDraft            MerchantFutureOfferingStatus = "draft"
	MerchantFutureOfferingStatusSubmitted        MerchantFutureOfferingStatus = "submitted"
	MerchantFutureOfferingStatusTrustReview      MerchantFutureOfferingStatus = "trust_review"
	MerchantFutureOfferingStatusChangesRequested MerchantFutureOfferingStatus = "changes_requested"
	MerchantFutureOfferingStatusApproved         MerchantFutureOfferingStatus = "approved"
	MerchantFutureOfferingStatusPublished        MerchantFutureOfferingStatus = "published"
	MerchantFutureOfferingStatusPaused           MerchantFutureOfferingStatus = "paused"
	MerchantFutureOfferingStatusExpired          MerchantFutureOfferingStatus = "expired"
	MerchantFutureOfferingStatusRejected         MerchantFutureOfferingStatus = "rejected"
	MerchantFutureOfferingStatusUnpublished      MerchantFutureOfferingStatus = "unpublished"
	MerchantFutureOfferingStatusArchived         MerchantFutureOfferingStatus = "archived"
)

// NormalizeMerchantFutureOfferingStatus returns status in canonical identifier form.
func NormalizeMerchantFutureOfferingStatus(status MerchantFutureOfferingStatus) MerchantFutureOfferingStatus {
	return MerchantFutureOfferingStatus(normalizeIdentifier(string(status)))
}

// IsValidMerchantFutureOfferingStatus reports whether status belongs to the persisted vocabulary.
func IsValidMerchantFutureOfferingStatus(status MerchantFutureOfferingStatus) bool {
	switch NormalizeMerchantFutureOfferingStatus(status) {
	case MerchantFutureOfferingStatusDraft,
		MerchantFutureOfferingStatusSubmitted,
		MerchantFutureOfferingStatusTrustReview,
		MerchantFutureOfferingStatusChangesRequested,
		MerchantFutureOfferingStatusApproved,
		MerchantFutureOfferingStatusPublished,
		MerchantFutureOfferingStatusPaused,
		MerchantFutureOfferingStatusExpired,
		MerchantFutureOfferingStatusRejected,
		MerchantFutureOfferingStatusUnpublished,
		MerchantFutureOfferingStatusArchived:
		return true
	default:
		return false
	}
}

// MerchantFutureOfferingType is the stable structural classification of a
// Future Offering. It determines which supporting FO facts are structurally
// meaningful and is engineering-owned, not administrative policy.
type MerchantFutureOfferingType string

const (
	MerchantFutureOfferingTypeProduct     MerchantFutureOfferingType = "product"
	MerchantFutureOfferingTypeService     MerchantFutureOfferingType = "service"
	MerchantFutureOfferingTypeEvent       MerchantFutureOfferingType = "event"
	MerchantFutureOfferingTypeVenue       MerchantFutureOfferingType = "venue"
	MerchantFutureOfferingTypeDevelopment MerchantFutureOfferingType = "development"
	MerchantFutureOfferingTypeExperience  MerchantFutureOfferingType = "experience"
)

// NormalizeMerchantFutureOfferingType returns offeringType in canonical identifier form.
func NormalizeMerchantFutureOfferingType(offeringType MerchantFutureOfferingType) MerchantFutureOfferingType {
	return MerchantFutureOfferingType(normalizeIdentifier(string(offeringType)))
}

// IsValidMerchantFutureOfferingType reports whether offeringType belongs to the persisted vocabulary.
func IsValidMerchantFutureOfferingType(offeringType MerchantFutureOfferingType) bool {
	switch NormalizeMerchantFutureOfferingType(offeringType) {
	case MerchantFutureOfferingTypeProduct,
		MerchantFutureOfferingTypeService,
		MerchantFutureOfferingTypeEvent,
		MerchantFutureOfferingTypeVenue,
		MerchantFutureOfferingTypeDevelopment,
		MerchantFutureOfferingTypeExperience:
		return true
	default:
		return false
	}
}

// MerchantFutureOfferingReleaseStrategy describes how a Future Offering is
// intended to become available. It is deliberately distinct from consumer
// engagement options (waitlist, preorder intent) and from access policy (who
// may participate). Like OfferingType, this is a small, stable,
// engineering-owned structural classification.
type MerchantFutureOfferingReleaseStrategy string

const (
	MerchantFutureOfferingReleaseStrategyDrop            MerchantFutureOfferingReleaseStrategy = "drop"
	MerchantFutureOfferingReleaseStrategyScheduled       MerchantFutureOfferingReleaseStrategy = "scheduled"
	MerchantFutureOfferingReleaseStrategyRolling         MerchantFutureOfferingReleaseStrategy = "rolling"
	MerchantFutureOfferingReleaseStrategyLimitedQuantity MerchantFutureOfferingReleaseStrategy = "limited_quantity"
)

// NormalizeMerchantFutureOfferingReleaseStrategy returns v in canonical identifier form.
func NormalizeMerchantFutureOfferingReleaseStrategy(v MerchantFutureOfferingReleaseStrategy) MerchantFutureOfferingReleaseStrategy {
	return MerchantFutureOfferingReleaseStrategy(normalizeIdentifier(string(v)))
}

// IsValidMerchantFutureOfferingReleaseStrategy reports whether v belongs to the persisted vocabulary.
func IsValidMerchantFutureOfferingReleaseStrategy(v MerchantFutureOfferingReleaseStrategy) bool {
	switch NormalizeMerchantFutureOfferingReleaseStrategy(v) {
	case MerchantFutureOfferingReleaseStrategyDrop,
		MerchantFutureOfferingReleaseStrategyScheduled,
		MerchantFutureOfferingReleaseStrategyRolling,
		MerchantFutureOfferingReleaseStrategyLimitedQuantity:
		return true
	default:
		return false
	}
}

// MerchantFutureOfferingAccessPolicy describes who may participate in a
// Future Offering. It is distinct from release strategy and from consumer
// engagement options.
type MerchantFutureOfferingAccessPolicy string

const (
	MerchantFutureOfferingAccessPolicyPublic           MerchantFutureOfferingAccessPolicy = "public"
	MerchantFutureOfferingAccessPolicyInviteOnly       MerchantFutureOfferingAccessPolicy = "invite_only"
	MerchantFutureOfferingAccessPolicyApprovalRequired MerchantFutureOfferingAccessPolicy = "approval_required"
)

// NormalizeMerchantFutureOfferingAccessPolicy returns v in canonical identifier form.
func NormalizeMerchantFutureOfferingAccessPolicy(v MerchantFutureOfferingAccessPolicy) MerchantFutureOfferingAccessPolicy {
	return MerchantFutureOfferingAccessPolicy(normalizeIdentifier(string(v)))
}

// IsValidMerchantFutureOfferingAccessPolicy reports whether v belongs to the persisted vocabulary.
func IsValidMerchantFutureOfferingAccessPolicy(v MerchantFutureOfferingAccessPolicy) bool {
	switch NormalizeMerchantFutureOfferingAccessPolicy(v) {
	case MerchantFutureOfferingAccessPolicyPublic,
		MerchantFutureOfferingAccessPolicyInviteOnly,
		MerchantFutureOfferingAccessPolicyApprovalRequired:
		return true
	default:
		return false
	}
}

// MerchantFutureOffering represents one durable Future Offering project.
//
// title, category_id, offering_type, release_strategy, access_policy, and
// launch_at are nullable: they may be unset while status = 'draft'. Once
// status leaves draft, submitted_at records the completed FO-owned transition;
// cross-domain submission readiness remains a service-layer concern.
type MerchantFutureOffering struct {
	ID              uuid.UUID                              `json:"id" db:"id"`
	MerchantID      uuid.UUID                              `json:"merchant_id" db:"merchant_id"`
	ProjectName     string                                 `json:"project_name" db:"project_name"`
	Title           *string                                `json:"title,omitempty" db:"title"`
	Summary         string                                 `json:"summary" db:"summary"`
	Description     *string                                `json:"description,omitempty" db:"description"`
	CategoryID      *uuid.UUID                             `json:"category_id,omitempty" db:"category_id"`
	OfferingType    *MerchantFutureOfferingType            `json:"offering_type,omitempty" db:"offering_type"`
	ReleaseStrategy *MerchantFutureOfferingReleaseStrategy `json:"release_strategy,omitempty" db:"release_strategy"`
	AccessPolicy    *MerchantFutureOfferingAccessPolicy    `json:"access_policy,omitempty" db:"access_policy"`
	Status          MerchantFutureOfferingStatus           `json:"status" db:"status"`
	LaunchAt        *time.Time                             `json:"launch_at,omitempty" db:"launch_at"`
	SubmittedAt     *time.Time                             `json:"submitted_at,omitempty" db:"submitted_at"`
	ApprovedAt      *time.Time                             `json:"approved_at,omitempty" db:"approved_at"`
	PublishedAt     *time.Time                             `json:"published_at,omitempty" db:"published_at"`
	RejectedAt      *time.Time                             `json:"rejected_at,omitempty" db:"rejected_at"`
	UnpublishedAt   *time.Time                             `json:"unpublished_at,omitempty" db:"unpublished_at"`
	ArchivedAt      *time.Time                             `json:"archived_at,omitempty" db:"archived_at"`
	CreatedAt       time.Time                              `json:"created_at" db:"created_at"`
	UpdatedAt       time.Time                              `json:"updated_at" db:"updated_at"`
	DeletedAt       *time.Time                             `json:"deleted_at,omitempty" db:"deleted_at"`
}

// MerchantFutureOfferingDraftFacts contains the mutable core facts owned by
// the Future Offering aggregate while it remains in draft. UpdateDraftFacts
// replaces this set atomically. Supporting-domain facts remain owned by their
// respective domains and are not represented here.
type MerchantFutureOfferingDraftFacts struct {
	ProjectName     string
	Title           *string
	Summary         string
	Description     *string
	CategoryID      *uuid.UUID
	OfferingType    *MerchantFutureOfferingType
	ReleaseStrategy *MerchantFutureOfferingReleaseStrategy
	AccessPolicy    *MerchantFutureOfferingAccessPolicy
	LaunchAt        *time.Time
}

// MerchantFutureOfferingModel owns Future Offering aggregate persistence.
type MerchantFutureOfferingModel struct {
	DB     *pgxpool.Pool
	Logger *logging.Logger
}

type merchantFutureOfferingQueryRower interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

func (m *MerchantFutureOfferingModel) validateBase() error {
	if m == nil {
		return errors.New("merchant future offering model is required")
	}
	if m.Logger == nil {
		return errors.New("merchant future offering model logger is required")
	}
	return nil
}

func (m *MerchantFutureOfferingModel) validatePool() error {
	if err := m.validateBase(); err != nil {
		return err
	}
	if m.DB == nil {
		return errors.New("merchant future offering model database pool is required")
	}
	return nil
}

func scanMerchantFutureOffering(row scannableRow, fo *MerchantFutureOffering) error {
	return row.Scan(
		&fo.ID,
		&fo.MerchantID,
		&fo.ProjectName,
		&fo.Title,
		&fo.Summary,
		&fo.Description,
		&fo.CategoryID,
		&fo.OfferingType,
		&fo.ReleaseStrategy,
		&fo.AccessPolicy,
		&fo.Status,
		&fo.LaunchAt,
		&fo.SubmittedAt,
		&fo.ApprovedAt,
		&fo.PublishedAt,
		&fo.RejectedAt,
		&fo.UnpublishedAt,
		&fo.ArchivedAt,
		&fo.CreatedAt,
		&fo.UpdatedAt,
		&fo.DeletedAt,
	)
}

func merchantFutureOfferingInvalidInput(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrMerchantFutureOfferingInvalidInput, fmt.Sprintf(format, args...))
}

func classifyMerchantFutureOfferingWriteError(err error) error {
	switch {
	case IsPgConstraint(err, merchantFutureOfferingMerchantFKConstraint):
		return ErrMerchantFutureOfferingMerchantNotFound
	case IsPgConstraint(err, merchantFutureOfferingCategoryFKConstraint):
		return ErrMerchantFutureOfferingCategoryNotFound
	case IsPgConstraint(err, merchantFutureOfferingProjectNameConstraint),
		IsPgConstraint(err, merchantFutureOfferingTitleConstraint),
		IsPgConstraint(err, merchantFutureOfferingOfferingTypeConstraint),
		IsPgConstraint(err, merchantFutureOfferingReleaseStrategyConstraint),
		IsPgConstraint(err, merchantFutureOfferingAccessPolicyConstraint):
		return ErrMerchantFutureOfferingInvalidInput
	case IsPgConstraint(err, merchantFutureOfferingStatusConstraint):
		return ErrMerchantFutureOfferingInvalidState
	case IsForeignKeyViolation(err):
		return ErrMerchantFutureOfferingInvalidState
	case IsCheckViolation(err), IsNotNullViolation(err):
		return ErrMerchantFutureOfferingInvalidState
	default:
		return err
	}
}

func normalizeMerchantFutureOfferingProjectName(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", merchantFutureOfferingInvalidInput("project_name is required")
	}
	return value, nil
}

func normalizeMerchantFutureOfferingSummary(value string) string {
	return strings.TrimSpace(value)
}

func normalizeOptionalMerchantFutureOfferingType(offeringType *MerchantFutureOfferingType) (*MerchantFutureOfferingType, error) {
	if offeringType == nil {
		return nil, nil
	}
	normalized := NormalizeMerchantFutureOfferingType(*offeringType)
	if normalized == "" {
		return nil, nil
	}
	if !IsValidMerchantFutureOfferingType(normalized) {
		return nil, merchantFutureOfferingInvalidInput("invalid offering_type: %q", *offeringType)
	}
	return &normalized, nil
}

func normalizeOptionalMerchantFutureOfferingReleaseStrategy(
	v *MerchantFutureOfferingReleaseStrategy,
) (*MerchantFutureOfferingReleaseStrategy, error) {
	if v == nil {
		return nil, nil
	}
	normalized := NormalizeMerchantFutureOfferingReleaseStrategy(*v)
	if normalized == "" {
		return nil, nil
	}
	if !IsValidMerchantFutureOfferingReleaseStrategy(normalized) {
		return nil, merchantFutureOfferingInvalidInput("invalid release_strategy: %q", *v)
	}
	return &normalized, nil
}

func normalizeOptionalMerchantFutureOfferingAccessPolicy(
	v *MerchantFutureOfferingAccessPolicy,
) (*MerchantFutureOfferingAccessPolicy, error) {
	if v == nil {
		return nil, nil
	}
	normalized := NormalizeMerchantFutureOfferingAccessPolicy(*v)
	if normalized == "" {
		return nil, nil
	}
	if !IsValidMerchantFutureOfferingAccessPolicy(normalized) {
		return nil, merchantFutureOfferingInvalidInput("invalid access_policy: %q", *v)
	}
	return &normalized, nil
}

func normalizeOptionalMerchantFutureOfferingCategoryID(categoryID *uuid.UUID) (*uuid.UUID, error) {
	if categoryID == nil {
		return nil, nil
	}
	if *categoryID == uuid.Nil {
		return nil, merchantFutureOfferingInvalidInput("category_id must not be the zero UUID")
	}
	id := *categoryID
	return &id, nil
}

func normalizeMerchantFutureOfferingOptionalTime(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	v := value.UTC().Truncate(time.Microsecond)
	return &v
}

func validateMerchantFutureOfferingID(id uuid.UUID) error {
	if id == uuid.Nil {
		return merchantFutureOfferingInvalidInput("id is required")
	}
	return nil
}

func validateMerchantFutureOfferingMerchantID(id uuid.UUID) error {
	if id == uuid.Nil {
		return merchantFutureOfferingInvalidInput("merchant_id is required")
	}
	return nil
}

func validateMerchantFutureOfferingLimit(limit int) error {
	if limit <= 0 {
		return merchantFutureOfferingInvalidInput("limit must be greater than zero")
	}
	return nil
}

func validateMerchantFutureOfferingUpdatedCursor(beforeUpdatedAt *time.Time, beforeID *uuid.UUID) error {
	if (beforeUpdatedAt == nil) != (beforeID == nil) {
		return merchantFutureOfferingInvalidInput("before_updated_at and before_id must be supplied together")
	}
	if beforeUpdatedAt == nil {
		return nil
	}
	if beforeUpdatedAt.IsZero() || *beforeID == uuid.Nil {
		return merchantFutureOfferingInvalidInput("invalid merchant future offering updated cursor")
	}
	return nil
}

// validateMerchantFutureOfferingPersistedState performs defense-in-depth
// structural validation on rows read from the database.
//
// This validator only encodes invariants this file actually establishes: the
// draft <-> submitted timestamp coupling and discard-implies-draft. It does
// not attempt to encode the full
// downstream lifecycle timestamp matrix for trust_review, approved,
// published, paused, expired, rejected, unpublished, or archived, because
// those transitions are not yet specified. Extend this validator when
// PCDF-M02/A02 lifecycle transitions land.
func validateMerchantFutureOfferingPersistedState(fo *MerchantFutureOffering) error {
	if fo == nil ||
		fo.ID == uuid.Nil ||
		fo.MerchantID == uuid.Nil ||
		strings.TrimSpace(fo.ProjectName) == "" ||
		fo.CreatedAt.IsZero() ||
		fo.UpdatedAt.IsZero() {
		return ErrMerchantFutureOfferingInvalidState
	}
	if !IsValidMerchantFutureOfferingStatus(fo.Status) {
		return ErrMerchantFutureOfferingInvalidState
	}
	if fo.Title != nil && strings.TrimSpace(*fo.Title) == "" {
		return ErrMerchantFutureOfferingInvalidState
	}
	if fo.OfferingType != nil && !IsValidMerchantFutureOfferingType(*fo.OfferingType) {
		return ErrMerchantFutureOfferingInvalidState
	}
	if fo.ReleaseStrategy != nil && !IsValidMerchantFutureOfferingReleaseStrategy(*fo.ReleaseStrategy) {
		return ErrMerchantFutureOfferingInvalidState
	}
	if fo.AccessPolicy != nil && !IsValidMerchantFutureOfferingAccessPolicy(*fo.AccessPolicy) {
		return ErrMerchantFutureOfferingInvalidState
	}
	if fo.CategoryID != nil && *fo.CategoryID == uuid.Nil {
		return ErrMerchantFutureOfferingInvalidState
	}

	status := NormalizeMerchantFutureOfferingStatus(fo.Status)
	if status == MerchantFutureOfferingStatusDraft {
		if fo.SubmittedAt != nil {
			return ErrMerchantFutureOfferingInvalidState
		}
	} else if fo.SubmittedAt == nil {
		return ErrMerchantFutureOfferingInvalidState
	}

	if fo.DeletedAt != nil && status != MerchantFutureOfferingStatusDraft {
		return ErrMerchantFutureOfferingInvalidState
	}

	return nil
}

func (m *MerchantFutureOfferingModel) getByIDViaQuerier(
	ctx context.Context,
	querier merchantFutureOfferingQueryRower,
	id uuid.UUID,
	includeDeleted bool,
) (*MerchantFutureOffering, error) {
	query := `
		SELECT ` + merchantFutureOfferingSelectColumns + `
		FROM merchant_future_offerings
		WHERE id = $1
	`
	if !includeDeleted {
		query += ` AND deleted_at IS NULL`
	}

	var fo MerchantFutureOffering
	if err := scanMerchantFutureOffering(querier.QueryRow(ctx, query, id), &fo); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	if err := validateMerchantFutureOfferingPersistedState(&fo); err != nil {
		return nil, err
	}
	return &fo, nil
}

func (m *MerchantFutureOfferingModel) insertDraft(
	ctx context.Context,
	querier merchantFutureOfferingQueryRower,
	functionName string,
	fo *MerchantFutureOffering,
) (*MerchantFutureOffering, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName(functionName)
	if fo == nil {
		return nil, merchantFutureOfferingInvalidInput("future offering is required")
	}
	if err := validateMerchantFutureOfferingMerchantID(fo.MerchantID); err != nil {
		return nil, err
	}
	projectName, err := normalizeMerchantFutureOfferingProjectName(fo.ProjectName)
	if err != nil {
		return nil, err
	}
	title := normalizeOptionalString(fo.Title)
	description := normalizeOptionalString(fo.Description)
	summary := normalizeMerchantFutureOfferingSummary(fo.Summary)
	categoryID, err := normalizeOptionalMerchantFutureOfferingCategoryID(fo.CategoryID)
	if err != nil {
		return nil, err
	}
	offeringType, err := normalizeOptionalMerchantFutureOfferingType(fo.OfferingType)
	if err != nil {
		return nil, err
	}
	releaseStrategy, err := normalizeOptionalMerchantFutureOfferingReleaseStrategy(fo.ReleaseStrategy)
	if err != nil {
		return nil, err
	}
	accessPolicy, err := normalizeOptionalMerchantFutureOfferingAccessPolicy(fo.AccessPolicy)
	if err != nil {
		return nil, err
	}
	if fo.Status != "" && NormalizeMerchantFutureOfferingStatus(fo.Status) != MerchantFutureOfferingStatusDraft {
		return nil, merchantFutureOfferingInvalidInput("status must be draft at creation")
	}
	if fo.SubmittedAt != nil || fo.ApprovedAt != nil || fo.PublishedAt != nil ||
		fo.RejectedAt != nil || fo.UnpublishedAt != nil || fo.ArchivedAt != nil {
		return nil, merchantFutureOfferingInvalidInput("lifecycle timestamps must not be supplied at creation")
	}
	launchAt := normalizeMerchantFutureOfferingOptionalTime(fo.LaunchAt)

	id := fo.ID
	if id == uuid.Nil {
		id = uuid.New()
	}

	const query = `
		INSERT INTO merchant_future_offerings (
			id,
			merchant_id,
			project_name,
			title,
			summary,
			description,
			category_id,
			offering_type,
			release_strategy,
			access_policy,
			launch_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		RETURNING ` + merchantFutureOfferingSelectColumns

	var result MerchantFutureOffering
	if err := scanMerchantFutureOffering(
		querier.QueryRow(
			ctx, query,
			id, fo.MerchantID, projectName, title, summary, description,
			categoryID, offeringType, releaseStrategy, accessPolicy, launchAt,
		),
		&result,
	); err != nil {
		err = classifyMerchantFutureOfferingWriteError(err)
		logger.Error("Insert merchant future offering draft failed", err, "merchant_id", fo.MerchantID)
		return nil, err
	}
	if err := validateMerchantFutureOfferingPersistedState(&result); err != nil {
		return nil, err
	}
	return &result, nil
}

// InsertDraft creates a new draft Future Offering through the model pool.
// Only merchant_id and project_name are required; all other facts may be
// configured incrementally through UpdateDraftFacts.
func (m *MerchantFutureOfferingModel) InsertDraft(ctx context.Context, fo *MerchantFutureOffering) (*MerchantFutureOffering, error) {
	if err := m.validatePool(); err != nil {
		return nil, err
	}
	return m.insertDraft(ctx, m.DB, "InsertMerchantFutureOfferingDraft", fo)
}

// InsertDraftTx creates a new draft Future Offering through caller-owned tx.
func (m *MerchantFutureOfferingModel) InsertDraftTx(ctx context.Context, tx pgx.Tx, fo *MerchantFutureOffering) (*MerchantFutureOffering, error) {
	if err := m.validateBase(); err != nil {
		return nil, err
	}
	if tx == nil {
		return nil, merchantFutureOfferingInvalidInput("transaction is required")
	}
	return m.insertDraft(ctx, tx, "InsertMerchantFutureOfferingDraftTx", fo)
}

// GetByID retrieves a non-deleted Future Offering by ID without applying
// merchant ownership scope.
//
// This method is intended for internal, administrative, or other trusted
// service paths whose authorization semantics are established independently.
// Merchant-owned request paths must use GetByIDForMerchant unless ownership
// has already been authoritatively established by the calling service.
func (m *MerchantFutureOfferingModel) GetByID(ctx context.Context, id uuid.UUID) (*MerchantFutureOffering, error) {
	if err := m.validatePool(); err != nil {
		return nil, err
	}
	if err := validateMerchantFutureOfferingID(id); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	fo, err := m.getByIDViaQuerier(ctx, m.DB, id, false)
	if err != nil {
		return nil, fmt.Errorf("get merchant future offering by ID: %w", err)
	}
	if fo == nil {
		return nil, ErrMerchantFutureOfferingNotFound
	}
	return fo, nil
}

// GetByIDForMerchant retrieves a non-deleted Future Offering scoped to
// merchant ownership, giving the data layer a defense-in-depth authorization
// boundary independent of the handler/service layer.
func (m *MerchantFutureOfferingModel) GetByIDForMerchant(ctx context.Context, merchantID, id uuid.UUID) (*MerchantFutureOffering, error) {
	if err := m.validatePool(); err != nil {
		return nil, err
	}
	if err := validateMerchantFutureOfferingMerchantID(merchantID); err != nil {
		return nil, err
	}
	if err := validateMerchantFutureOfferingID(id); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	const query = `
		SELECT ` + merchantFutureOfferingSelectColumns + `
		FROM merchant_future_offerings
		WHERE id = $1
		  AND merchant_id = $2
		  AND deleted_at IS NULL
	`
	var fo MerchantFutureOffering
	if err := scanMerchantFutureOffering(m.DB.QueryRow(ctx, query, id, merchantID), &fo); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrMerchantFutureOfferingNotFound
		}
		return nil, fmt.Errorf("get merchant future offering by ID for merchant: %w", err)
	}
	if err := validateMerchantFutureOfferingPersistedState(&fo); err != nil {
		return nil, err
	}
	return &fo, nil
}

// GetByIDForUpdateTx retrieves and row-locks one non-deleted Future Offering
// through caller-owned tx, scoped to merchant ownership.
func (m *MerchantFutureOfferingModel) GetByIDForUpdateTx(ctx context.Context, tx pgx.Tx, merchantID, id uuid.UUID) (*MerchantFutureOffering, error) {
	if err := m.validateBase(); err != nil {
		return nil, err
	}
	if tx == nil {
		return nil, merchantFutureOfferingInvalidInput("transaction is required")
	}
	if err := validateMerchantFutureOfferingMerchantID(merchantID); err != nil {
		return nil, err
	}
	if err := validateMerchantFutureOfferingID(id); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()
	const query = `
		SELECT ` + merchantFutureOfferingSelectColumns + `
		FROM merchant_future_offerings
		WHERE id = $1
		  AND merchant_id = $2
		  AND deleted_at IS NULL
		FOR UPDATE
	`
	var fo MerchantFutureOffering
	if err := scanMerchantFutureOffering(tx.QueryRow(ctx, query, id, merchantID), &fo); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrMerchantFutureOfferingNotFound
		}
		return nil, fmt.Errorf("get merchant future offering by ID for update: %w", err)
	}
	if err := validateMerchantFutureOfferingPersistedState(&fo); err != nil {
		return nil, err
	}
	return &fo, nil
}

// GetByIDForMerchantTx retrieves one non-deleted Future Offering through a
// caller-owned transaction, scoped to merchant ownership, without taking a
// write lock. It is used by coherent read-only aggregate snapshots.
func (m *MerchantFutureOfferingModel) GetByIDForMerchantTx(ctx context.Context, tx pgx.Tx, merchantID, id uuid.UUID) (*MerchantFutureOffering, error) {
	if err := m.validateBase(); err != nil {
		return nil, err
	}
	if tx == nil {
		return nil, merchantFutureOfferingInvalidInput("transaction is required")
	}
	if err := validateMerchantFutureOfferingMerchantID(merchantID); err != nil {
		return nil, err
	}
	if err := validateMerchantFutureOfferingID(id); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()
	const query = `SELECT ` + merchantFutureOfferingSelectColumns + ` FROM merchant_future_offerings WHERE id = $1 AND merchant_id = $2 AND deleted_at IS NULL`
	var fo MerchantFutureOffering
	if err := scanMerchantFutureOffering(tx.QueryRow(ctx, query, id, merchantID), &fo); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrMerchantFutureOfferingNotFound
		}
		return nil, fmt.Errorf("get merchant future offering by ID for merchant transaction: %w", err)
	}
	if err := validateMerchantFutureOfferingPersistedState(&fo); err != nil {
		return nil, err
	}
	return &fo, nil
}

// AdvanceDraftVersionTx advances the aggregate optimistic-concurrency version
// after a supporting M01 domain mutates inside the same transaction.
func (m *MerchantFutureOfferingModel) AdvanceDraftVersionTx(ctx context.Context, tx pgx.Tx, merchantID, id uuid.UUID, expectedUpdatedAt time.Time) (*MerchantFutureOffering, error) {
	if err := m.validateBase(); err != nil {
		return nil, err
	}
	if tx == nil {
		return nil, merchantFutureOfferingInvalidInput("transaction is required")
	}
	if err := validateMerchantFutureOfferingMerchantID(merchantID); err != nil {
		return nil, err
	}
	if err := validateMerchantFutureOfferingID(id); err != nil {
		return nil, err
	}
	if expectedUpdatedAt.IsZero() {
		return nil, merchantFutureOfferingInvalidInput("expected_updated_at is required")
	}
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()
	const query = `UPDATE merchant_future_offerings SET updated_at = ` + merchantFutureOfferingNextVersionSQL + ` WHERE id = $1 AND merchant_id = $2 AND status = 'draft' AND deleted_at IS NULL AND updated_at = $3 RETURNING ` + merchantFutureOfferingSelectColumns
	var fo MerchantFutureOffering
	if err := scanMerchantFutureOffering(tx.QueryRow(ctx, query, id, merchantID, expectedUpdatedAt.UTC()), &fo); err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			return nil, classifyMerchantFutureOfferingWriteError(err)
		}
		return nil, m.resolveDraftMutationNoMatch(ctx, tx, merchantID, id)
	}
	if err := validateMerchantFutureOfferingPersistedState(&fo); err != nil {
		return nil, err
	}
	return &fo, nil
}

// listByMerchant is ordered by updated_at DESC, id DESC to align with
// idx_merchant_future_offerings_merchant_status(merchant_id, status,
// updated_at DESC), which favors surfacing the most recently touched draft
// first for the resume-work-in-progress workflow.
func (m *MerchantFutureOfferingModel) listByMerchant(
	ctx context.Context,
	merchantID uuid.UUID,
	status *MerchantFutureOfferingStatus,
	limit int,
	beforeUpdatedAt *time.Time,
	beforeID *uuid.UUID,
) ([]*MerchantFutureOffering, error) {
	if err := m.validatePool(); err != nil {
		return nil, err
	}
	if err := validateMerchantFutureOfferingMerchantID(merchantID); err != nil {
		return nil, err
	}
	if err := validateMerchantFutureOfferingLimit(limit); err != nil {
		return nil, err
	}
	if err := validateMerchantFutureOfferingUpdatedCursor(beforeUpdatedAt, beforeID); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	var rows pgx.Rows
	var err error
	switch {
	case status == nil && beforeUpdatedAt == nil:
		const query = `
			SELECT ` + merchantFutureOfferingSelectColumns + `
			FROM merchant_future_offerings
			WHERE merchant_id = $1
			  AND deleted_at IS NULL
			ORDER BY updated_at DESC, id DESC
			LIMIT $2
		`
		rows, err = m.DB.Query(ctx, query, merchantID, limit)
	case status == nil:
		const query = `
			SELECT ` + merchantFutureOfferingSelectColumns + `
			FROM merchant_future_offerings
			WHERE merchant_id = $1
			  AND deleted_at IS NULL
			  AND (updated_at, id) < ($2, $3)
			ORDER BY updated_at DESC, id DESC
			LIMIT $4
		`
		rows, err = m.DB.Query(ctx, query, merchantID, beforeUpdatedAt.UTC(), *beforeID, limit)
	case beforeUpdatedAt == nil:
		const query = `
			SELECT ` + merchantFutureOfferingSelectColumns + `
			FROM merchant_future_offerings
			WHERE merchant_id = $1
			  AND status = $2
			  AND deleted_at IS NULL
			ORDER BY updated_at DESC, id DESC
			LIMIT $3
		`
		rows, err = m.DB.Query(ctx, query, merchantID, *status, limit)
	default:
		const query = `
			SELECT ` + merchantFutureOfferingSelectColumns + `
			FROM merchant_future_offerings
			WHERE merchant_id = $1
			  AND status = $2
			  AND deleted_at IS NULL
			  AND (updated_at, id) < ($3, $4)
			ORDER BY updated_at DESC, id DESC
			LIMIT $5
		`
		rows, err = m.DB.Query(ctx, query, merchantID, *status, beforeUpdatedAt.UTC(), *beforeID, limit)
	}
	if err != nil {
		return nil, fmt.Errorf("list merchant future offerings by merchant: %w", err)
	}
	defer rows.Close()

	offerings := make([]*MerchantFutureOffering, 0, limit)
	for rows.Next() {
		var fo MerchantFutureOffering
		if err := scanMerchantFutureOffering(rows, &fo); err != nil {
			return nil, err
		}
		if err := validateMerchantFutureOfferingPersistedState(&fo); err != nil {
			return nil, err
		}
		offerings = append(offerings, &fo)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return offerings, nil
}

// ListByMerchant returns bounded keyset-paginated Future Offerings owned by
// merchantID, most-recently-updated first, excluding soft-deleted drafts.
func (m *MerchantFutureOfferingModel) ListByMerchant(
	ctx context.Context,
	merchantID uuid.UUID,
	limit int,
	beforeUpdatedAt *time.Time,
	beforeID *uuid.UUID,
) ([]*MerchantFutureOffering, error) {
	return m.listByMerchant(ctx, merchantID, nil, limit, beforeUpdatedAt, beforeID)
}

// ListByMerchantAndStatus returns bounded keyset-paginated Future Offerings
// filtered by status, most-recently-updated first. Used for draft resumption
// via status = MerchantFutureOfferingStatusDraft.
func (m *MerchantFutureOfferingModel) ListByMerchantAndStatus(
	ctx context.Context,
	merchantID uuid.UUID,
	status MerchantFutureOfferingStatus,
	limit int,
	beforeUpdatedAt *time.Time,
	beforeID *uuid.UUID,
) ([]*MerchantFutureOffering, error) {
	status = NormalizeMerchantFutureOfferingStatus(status)
	if !IsValidMerchantFutureOfferingStatus(status) {
		return nil, merchantFutureOfferingInvalidInput("invalid status: %q", status)
	}
	return m.listByMerchant(ctx, merchantID, &status, limit, beforeUpdatedAt, beforeID)
}

func (m *MerchantFutureOfferingModel) resolveDraftMutationNoMatch(
	ctx context.Context,
	querier merchantFutureOfferingQueryRower,
	merchantID uuid.UUID,
	id uuid.UUID,
) error {
	const query = `
		SELECT status
		FROM merchant_future_offerings
		WHERE id = $1
		  AND merchant_id = $2
		  AND deleted_at IS NULL
	`

	var status MerchantFutureOfferingStatus
	if err := querier.QueryRow(ctx, query, id, merchantID).Scan(&status); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrMerchantFutureOfferingNotFound
		}
		return err
	}
	if NormalizeMerchantFutureOfferingStatus(status) != MerchantFutureOfferingStatusDraft {
		return ErrMerchantFutureOfferingInvalidTransition
	}
	return ErrMerchantFutureOfferingEditConflict
}

const merchantFutureOfferingNextVersionSQL = `GREATEST(clock_timestamp(), updated_at + INTERVAL '1 microsecond')`

func (m *MerchantFutureOfferingModel) updateDraftFacts(
	ctx context.Context,
	querier merchantFutureOfferingQueryRower,
	functionName string,
	merchantID uuid.UUID,
	id uuid.UUID,
	facts MerchantFutureOfferingDraftFacts,
	expectedUpdatedAt time.Time,
) (*MerchantFutureOffering, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()
	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName(functionName)

	if err := validateMerchantFutureOfferingMerchantID(merchantID); err != nil {
		return nil, err
	}
	if err := validateMerchantFutureOfferingID(id); err != nil {
		return nil, err
	}
	projectName, err := normalizeMerchantFutureOfferingProjectName(facts.ProjectName)
	if err != nil {
		return nil, err
	}
	title := normalizeOptionalString(facts.Title)
	summary := normalizeMerchantFutureOfferingSummary(facts.Summary)
	description := normalizeOptionalString(facts.Description)
	categoryID, err := normalizeOptionalMerchantFutureOfferingCategoryID(facts.CategoryID)
	if err != nil {
		return nil, err
	}
	offeringType, err := normalizeOptionalMerchantFutureOfferingType(facts.OfferingType)
	if err != nil {
		return nil, err
	}
	releaseStrategy, err := normalizeOptionalMerchantFutureOfferingReleaseStrategy(facts.ReleaseStrategy)
	if err != nil {
		return nil, err
	}
	accessPolicy, err := normalizeOptionalMerchantFutureOfferingAccessPolicy(facts.AccessPolicy)
	if err != nil {
		return nil, err
	}
	launchAt := normalizeMerchantFutureOfferingOptionalTime(facts.LaunchAt)
	if expectedUpdatedAt.IsZero() {
		return nil, merchantFutureOfferingInvalidInput("expected_updated_at is required")
	}

	const query = `
		UPDATE merchant_future_offerings
		SET
			project_name = $3,
			title = $4,
			summary = $5,
			description = $6,
			category_id = $7,
			offering_type = $8,
			release_strategy = $9,
			access_policy = $10,
			launch_at = $11,
			updated_at = ` + merchantFutureOfferingNextVersionSQL + `
		WHERE id = $1
		  AND merchant_id = $2
		  AND status = 'draft'
		  AND deleted_at IS NULL
		  AND updated_at = $12
		RETURNING ` + merchantFutureOfferingSelectColumns

	var fo MerchantFutureOffering
	if err := scanMerchantFutureOffering(querier.QueryRow(ctx, query,
		id, merchantID, projectName, title, summary, description, categoryID,
		offeringType, releaseStrategy, accessPolicy, launchAt, expectedUpdatedAt.UTC(),
	), &fo); err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			err = classifyMerchantFutureOfferingWriteError(err)
			logger.Error("Update merchant future offering draft facts failed", err, "future_offering_id", id, "merchant_id", merchantID)
			return nil, err
		}
		return nil, m.resolveDraftMutationNoMatch(ctx, querier, merchantID, id)
	}
	if err := validateMerchantFutureOfferingPersistedState(&fo); err != nil {
		return nil, err
	}
	return &fo, nil
}

// UpdateDraftFacts atomically replaces mutable core facts on a merchant-owned draft.
func (m *MerchantFutureOfferingModel) UpdateDraftFacts(ctx context.Context, merchantID, id uuid.UUID, facts MerchantFutureOfferingDraftFacts, expectedUpdatedAt time.Time) (*MerchantFutureOffering, error) {
	if err := m.validatePool(); err != nil {
		return nil, err
	}
	return m.updateDraftFacts(ctx, m.DB, "UpdateMerchantFutureOfferingDraftFacts", merchantID, id, facts, expectedUpdatedAt)
}

// UpdateDraftFactsTx is the transaction-aware form of UpdateDraftFacts.
func (m *MerchantFutureOfferingModel) UpdateDraftFactsTx(ctx context.Context, tx pgx.Tx, merchantID, id uuid.UUID, facts MerchantFutureOfferingDraftFacts, expectedUpdatedAt time.Time) (*MerchantFutureOffering, error) {
	if err := m.validateBase(); err != nil {
		return nil, err
	}
	if tx == nil {
		return nil, merchantFutureOfferingInvalidInput("transaction is required")
	}
	return m.updateDraftFacts(ctx, tx, "UpdateMerchantFutureOfferingDraftFactsTx", merchantID, id, facts, expectedUpdatedAt)
}

func (m *MerchantFutureOfferingModel) resolveSubmitNoMatch(ctx context.Context, querier merchantFutureOfferingQueryRower, merchantID, id uuid.UUID) error {
	const query = `
		SELECT status
		FROM merchant_future_offerings
		WHERE id = $1
		  AND merchant_id = $2
		  AND deleted_at IS NULL
	`
	var status MerchantFutureOfferingStatus
	if err := querier.QueryRow(ctx, query, id, merchantID).Scan(&status); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrMerchantFutureOfferingNotFound
		}
		return err
	}
	if NormalizeMerchantFutureOfferingStatus(status) != MerchantFutureOfferingStatusDraft {
		return ErrMerchantFutureOfferingInvalidTransition
	}
	return ErrMerchantFutureOfferingEditConflict
}

// SubmitTx performs the FO-owned draft -> submitted transition. Cross-domain
// submission readiness is established by M01 orchestration before this call.
func (m *MerchantFutureOfferingModel) SubmitTx(ctx context.Context, tx pgx.Tx, merchantID, id uuid.UUID, expectedUpdatedAt time.Time) (*MerchantFutureOffering, error) {
	if err := m.validateBase(); err != nil {
		return nil, err
	}
	if tx == nil {
		return nil, merchantFutureOfferingInvalidInput("transaction is required")
	}
	if err := validateMerchantFutureOfferingMerchantID(merchantID); err != nil {
		return nil, err
	}
	if err := validateMerchantFutureOfferingID(id); err != nil {
		return nil, err
	}
	if expectedUpdatedAt.IsZero() {
		return nil, merchantFutureOfferingInvalidInput("expected_updated_at is required")
	}
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()
	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName("SubmitMerchantFutureOfferingTx")
	const query = `
		UPDATE merchant_future_offerings
		SET status = 'submitted', submitted_at = NOW(), updated_at = ` + merchantFutureOfferingNextVersionSQL + `
		WHERE id = $1
		  AND merchant_id = $2
		  AND status = 'draft'
		  AND deleted_at IS NULL
		  AND updated_at = $3
		RETURNING ` + merchantFutureOfferingSelectColumns
	var fo MerchantFutureOffering
	if err := scanMerchantFutureOffering(tx.QueryRow(ctx, query, id, merchantID, expectedUpdatedAt.UTC()), &fo); err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			err = classifyMerchantFutureOfferingWriteError(err)
			logger.Error("Submit merchant future offering failed", err, "future_offering_id", id, "merchant_id", merchantID)
			return nil, err
		}
		return nil, m.resolveSubmitNoMatch(ctx, tx, merchantID, id)
	}
	if err := validateMerchantFutureOfferingPersistedState(&fo); err != nil {
		return nil, err
	}
	return &fo, nil
}

func (m *MerchantFutureOfferingModel) discardDraft(ctx context.Context, querier merchantFutureOfferingQueryRower, functionName string, merchantID, id uuid.UUID, expectedUpdatedAt time.Time) (*MerchantFutureOffering, error) {
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()
	logger := m.Logger.GetLoggerWithContextFromContext(ctx).WithFunctionName(functionName)
	if err := validateMerchantFutureOfferingMerchantID(merchantID); err != nil {
		return nil, err
	}
	if err := validateMerchantFutureOfferingID(id); err != nil {
		return nil, err
	}
	if expectedUpdatedAt.IsZero() {
		return nil, merchantFutureOfferingInvalidInput("expected_updated_at is required")
	}
	const query = `
		UPDATE merchant_future_offerings
		SET deleted_at = NOW(), updated_at = ` + merchantFutureOfferingNextVersionSQL + `
		WHERE id = $1
		  AND merchant_id = $2
		  AND status = 'draft'
		  AND deleted_at IS NULL
		  AND updated_at = $3
		RETURNING ` + merchantFutureOfferingSelectColumns
	var fo MerchantFutureOffering
	if err := scanMerchantFutureOffering(querier.QueryRow(ctx, query, id, merchantID, expectedUpdatedAt.UTC()), &fo); err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			err = classifyMerchantFutureOfferingWriteError(err)
			logger.Error("Discard merchant future offering draft failed", err, "future_offering_id", id, "merchant_id", merchantID)
			return nil, err
		}
		return nil, m.resolveDraftMutationNoMatch(ctx, querier, merchantID, id)
	}
	return &fo, nil
}

// DiscardDraft soft-deletes a merchant-owned draft guarded by optimistic concurrency.
func (m *MerchantFutureOfferingModel) DiscardDraft(ctx context.Context, merchantID, id uuid.UUID, expectedUpdatedAt time.Time) (*MerchantFutureOffering, error) {
	if err := m.validatePool(); err != nil {
		return nil, err
	}
	return m.discardDraft(ctx, m.DB, "DiscardMerchantFutureOfferingDraft", merchantID, id, expectedUpdatedAt)
}

// DiscardDraftTx is the transaction-aware form of DiscardDraft.
func (m *MerchantFutureOfferingModel) DiscardDraftTx(ctx context.Context, tx pgx.Tx, merchantID, id uuid.UUID, expectedUpdatedAt time.Time) (*MerchantFutureOffering, error) {
	if err := m.validateBase(); err != nil {
		return nil, err
	}
	if tx == nil {
		return nil, merchantFutureOfferingInvalidInput("transaction is required")
	}
	return m.discardDraft(ctx, tx, "DiscardMerchantFutureOfferingDraftTx", merchantID, id, expectedUpdatedAt)
}
