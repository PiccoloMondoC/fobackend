// Package services contains trusted internal service composition,
// automation support, moderation helpers, and shared internal workflow logic.
//
// focodebase/fobackend/internal/services/merchant_future_offering_readiness.go
//
// GTM:
//
//	Layer: 2.6 Internal Services / Merchant Future Offering Domain
//	Release Class: SPINE
//	Reason:
//	  Defines the canonical readiness engine for the M01 domains currently implemented.
//	  Readiness shown to the merchant and readiness enforced at submission are
//	  produced by the SAME canonical evaluator. Merchant inspection uses a
//	  coherent read transaction; submission re-evaluates after its write lock, so
//	  Angular presents authoritative results rather than re-implementing
//	  submission policy.
//
// Readiness Contract:
//
//	Readiness is a list of issues. Zero issues means ready. Each issue carries
//	a stable machine code, the contributing section, an optional field, and a
//	default English message. Codes and sections are the API contract;
//	messages are presentation defaults that a future runtime terminology
//	capability (FOCA §2) may replace.
//
//	An issue is a merchant-correctable deficiency. A returned error is a
//	system failure. Contributors must never convert system failures into
//	issues or issues into errors.
//
// Composition:
//
//	canonicalMerchantFutureOfferingReadiness owns the complete contributor
//	list. It is constructed internally; no caller can select or omit
//	contributors. Contributors currently composed:
//
//	  details     core Future Offering facts (merchant_future_offerings)
//	  engagement  groups/options (merchant_future_offering_engagement_*)
//
//	PENDING M01 contributors: assets, goals, service term, milestones. Until they land, this result describes implemented workspace readiness and MUST NOT authorize submission.
//	Each lands as an additional contributor appended here; the evaluation
//	and submission algorithms do not change.
//
// Concurrency:
//
//	Evaluate runs inside a caller-owned coherent transaction. Readiness GET does
//	not take a write lock; submission holds the merchant-scoped Future Offering
//	row lock before invoking the same evaluator. Every M01 draft mutation — core facts and
//	engagement replacement — requires that same lock and draft status, so no
//	readiness fact owned by a composed contributor can change before the
//	enclosing transaction ends. Catalog activity is protected by FOR SHARE
//	locks (see engagement_actions.go).
//
// SPINE Rule:
//
//	Keep compiling.
//	Keep production-ready.
//	Preserve one canonical composite; never accept caller-supplied checks.
//	Preserve issue/error separation.
//	Preserve stable issue codes and sections as API contract.
//	Do not query another domain's tables directly; use its model.
//	Block deployment if submission can commit with a non-empty issue list.
package services

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/PiccoloMondoC/focodebase/fobackend/internal/data"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// MerchantFutureOfferingReadinessSection identifies the M01 workspace
// section that owns an issue. Stable API contract.
type MerchantFutureOfferingReadinessSection string

// Readiness sections.
const (
	MerchantFutureOfferingReadinessSectionDetails    MerchantFutureOfferingReadinessSection = "details"
	MerchantFutureOfferingReadinessSectionEngagement MerchantFutureOfferingReadinessSection = "engagement"
)

// Stable readiness issue codes. Additive only once published.
const (
	ReadinessCodeTitleRequired            = "title_required"
	ReadinessCodeSummaryRequired          = "summary_required"
	ReadinessCodeCategoryRequired         = "category_required"
	ReadinessCodeOfferingTypeRequired     = "offering_type_required"
	ReadinessCodeReleaseStrategyRequired  = "release_strategy_required"
	ReadinessCodeAccessPolicyRequired     = "access_policy_required"
	ReadinessCodeLaunchAtRequired         = "launch_at_required"
	ReadinessCodeLaunchAtNotFuture        = "launch_at_not_in_future"
	ReadinessCodeEngagementGroupEmpty     = "engagement_group_empty"
	ReadinessCodeEngagementCeilingTooHigh = "engagement_group_max_selections_exceeds_options"
	ReadinessCodeEngagementActionInactive = "engagement_action_unavailable"
	ReadinessCodeEngagementOptionOrphaned = "engagement_option_group_missing"
)

// MerchantFutureOfferingReadinessIssue is one merchant-correctable deficiency.
type MerchantFutureOfferingReadinessIssue struct {
	Section MerchantFutureOfferingReadinessSection `json:"section"`
	Code    string                                 `json:"code"`
	Field   string                                 `json:"field,omitempty"`
	// SubjectID identifies the offending group/option where applicable.
	SubjectID *uuid.UUID `json:"subject_id,omitempty"`
	Message   string     `json:"message"`
}

// MerchantFutureOfferingReadiness is the authoritative readiness result for
// one Future Offering version.
type MerchantFutureOfferingReadiness struct {
	FutureOfferingID uuid.UUID `json:"future_offering_id"`
	// EvaluatedVersion is the Future Offering updated_at the result applies
	// to. Submission requires the same version.
	EvaluatedVersion time.Time                              `json:"evaluated_version"`
	Ready            bool                                   `json:"ready"`
	Issues           []MerchantFutureOfferingReadinessIssue `json:"issues"`
}

// ErrMerchantFutureOfferingNotReady classifies a submission rejected because
// canonical readiness produced issues.
var ErrMerchantFutureOfferingNotReady = errors.New("merchant future offering is not ready for submission")

// MerchantFutureOfferingNotReadyError carries the authoritative readiness
// result of a rejected submission.
type MerchantFutureOfferingNotReadyError struct {
	Readiness MerchantFutureOfferingReadiness
}

func (e *MerchantFutureOfferingNotReadyError) Error() string {
	return fmt.Sprintf("%s: %d issue(s)", ErrMerchantFutureOfferingNotReady, len(e.Readiness.Issues))
}

// Is lets errors.Is(err, ErrMerchantFutureOfferingNotReady) match.
func (e *MerchantFutureOfferingNotReadyError) Is(target error) bool {
	return target == ErrMerchantFutureOfferingNotReady
}

// merchantFutureOfferingReadinessContributor is one domain's contribution.
// Implementations are package-internal and composed only below.
type merchantFutureOfferingReadinessContributor interface {
	evaluate(
		ctx context.Context,
		tx pgx.Tx,
		fo *data.MerchantFutureOffering,
		dbNow time.Time,
	) ([]MerchantFutureOfferingReadinessIssue, error)
}

// canonicalMerchantFutureOfferingReadiness is the one M01 composite.
type canonicalMerchantFutureOfferingReadiness struct {
	contributors []merchantFutureOfferingReadinessContributor
}

func (s *Service) merchantFutureOfferingReadiness() *canonicalMerchantFutureOfferingReadiness {
	return &canonicalMerchantFutureOfferingReadiness{
		contributors: []merchantFutureOfferingReadinessContributor{
			&coreFactsReadiness{},
			&engagementReadiness{models: s.Models},
		},
	}
}

// Evaluate runs every contributor against the locked Future Offering.
func (c *canonicalMerchantFutureOfferingReadiness) Evaluate(
	ctx context.Context,
	tx pgx.Tx,
	fo *data.MerchantFutureOffering,
) (MerchantFutureOfferingReadiness, error) {
	result := MerchantFutureOfferingReadiness{Issues: []MerchantFutureOfferingReadinessIssue{}}
	if fo == nil || tx == nil {
		return result, data.ErrMerchantFutureOfferingInvalidState
	}
	result.FutureOfferingID = fo.ID
	result.EvaluatedVersion = fo.UpdatedAt.UTC()

	// DB-owned reference time keeps "future" consistent with lifecycle
	// timestamps written in the same transaction (BEG 6.7/6.9).
	var dbNow time.Time
	if err := tx.QueryRow(ctx, `SELECT NOW()`).Scan(&dbNow); err != nil {
		return result, fmt.Errorf("read database reference time: %w", err)
	}

	for _, contributor := range c.contributors {
		issues, err := contributor.evaluate(ctx, tx, fo, dbNow.UTC())
		if err != nil {
			return result, err
		}
		result.Issues = append(result.Issues, issues...)
	}
	result.Ready = len(result.Issues) == 0
	return result, nil
}

// -----------------------------------------------------------------------------
// Core facts contributor
// -----------------------------------------------------------------------------

// coreFactsReadiness enforces the facts the canonical DDL deliberately leaves
// nullable only while status = 'draft' (see merchant_future_offerings DDL and
// data-layer Draft Completeness Boundary).
type coreFactsReadiness struct{}

func detailsIssue(code, field, message string) MerchantFutureOfferingReadinessIssue {
	return MerchantFutureOfferingReadinessIssue{
		Section: MerchantFutureOfferingReadinessSectionDetails,
		Code:    code,
		Field:   field,
		Message: message,
	}
}

func (coreFactsReadiness) evaluate(
	_ context.Context,
	_ pgx.Tx,
	fo *data.MerchantFutureOffering,
	dbNow time.Time,
) ([]MerchantFutureOfferingReadinessIssue, error) {
	var issues []MerchantFutureOfferingReadinessIssue
	if fo.Title == nil || strings.TrimSpace(*fo.Title) == "" {
		issues = append(issues, detailsIssue(ReadinessCodeTitleRequired, "title", "Add a title for this Future Offering."))
	}
	if strings.TrimSpace(fo.Summary) == "" {
		issues = append(issues, detailsIssue(ReadinessCodeSummaryRequired, "summary", "Add a short summary of what is coming."))
	}
	if fo.CategoryID == nil {
		issues = append(issues, detailsIssue(ReadinessCodeCategoryRequired, "category_id", "Choose a category."))
	}
	if fo.OfferingType == nil {
		issues = append(issues, detailsIssue(ReadinessCodeOfferingTypeRequired, "offering_type", "Choose what kind of offering this is."))
	}
	if fo.ReleaseStrategy == nil {
		issues = append(issues, detailsIssue(ReadinessCodeReleaseStrategyRequired, "release_strategy", "Choose how it will be released."))
	}
	if fo.AccessPolicy == nil {
		issues = append(issues, detailsIssue(ReadinessCodeAccessPolicyRequired, "access_policy", "Choose who can take part."))
	}
	switch {
	case fo.LaunchAt == nil:
		issues = append(issues, detailsIssue(ReadinessCodeLaunchAtRequired, "launch_at", "Set the planned launch date."))
	case !fo.LaunchAt.After(dbNow):
		// Engineering invariant: a Future Offering launches in the future
		// (FOCA §15). Minimum anticipation runway is Administration policy
		// and is not yet configured (SE report §10).
		issues = append(issues, detailsIssue(ReadinessCodeLaunchAtNotFuture, "launch_at", "The planned launch date must be in the future."))
	}
	return issues, nil
}

// -----------------------------------------------------------------------------
// Engagement contributor
// -----------------------------------------------------------------------------

// engagementReadiness validates engagement configuration structure. Offering
// zero merchant Engagement Actions is valid: Watch is Platform-owned and
// always available (FOCA §4).
type engagementReadiness struct {
	models *data.Models
}

func engagementIssue(code string, subject *uuid.UUID, message string) MerchantFutureOfferingReadinessIssue {
	return MerchantFutureOfferingReadinessIssue{
		Section:   MerchantFutureOfferingReadinessSectionEngagement,
		Code:      code,
		SubjectID: subject,
		Message:   message,
	}
}

func (r *engagementReadiness) evaluate(
	ctx context.Context,
	tx pgx.Tx,
	fo *data.MerchantFutureOffering,
	_ time.Time,
) ([]MerchantFutureOfferingReadinessIssue, error) {
	groups, err := r.models.MerchantFutureOfferingEngagementActionGroup.ListActiveTx(ctx, tx, fo.ID)
	if err != nil {
		return nil, err
	}
	options, err := r.models.MerchantFutureOfferingEngagementOption.ListActiveTx(ctx, tx, fo.ID)
	if err != nil {
		return nil, err
	}

	actionIDs := make([]uuid.UUID, 0, len(options))
	perGroup := make(map[uuid.UUID]int, len(groups))
	for _, o := range options {
		actionIDs = append(actionIDs, o.EngagementActionID)
		perGroup[o.EngagementActionGroupID]++
	}
	active, err := r.models.EngagementAction.ListActiveByIDsForShareTx(ctx, tx, actionIDs)
	if err != nil {
		return nil, err
	}

	var issues []MerchantFutureOfferingReadinessIssue
	known := make(map[uuid.UUID]struct{}, len(groups))
	for _, g := range groups {
		id := g.ID
		known[id] = struct{}{}
		count := perGroup[id]
		if count == 0 {
			issues = append(issues, engagementIssue(ReadinessCodeEngagementGroupEmpty, &id,
				"This group has no actions. Add an action or remove the group."))
			continue
		}
		if g.MaxSelections != nil && *g.MaxSelections > count {
			issues = append(issues, engagementIssue(ReadinessCodeEngagementCeilingTooHigh, &id,
				fmt.Sprintf("This group allows %d choices but offers only %d.", *g.MaxSelections, count)))
		}
	}
	for _, o := range options {
		id := o.ID
		if _, ok := known[o.EngagementActionGroupID]; !ok {
			issues = append(issues, engagementIssue(ReadinessCodeEngagementOptionOrphaned, &id,
				"An action belongs to a group that is no longer active."))
		}
		if _, ok := active[o.EngagementActionID]; !ok {
			issues = append(issues, engagementIssue(ReadinessCodeEngagementActionInactive, &id,
				"An action you selected is no longer available. Remove it to continue."))
		}
	}
	return issues, nil
}
