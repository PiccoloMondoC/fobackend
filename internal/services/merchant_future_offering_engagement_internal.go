// Package services contains trusted internal service composition,
// automation support, moderation helpers, and shared internal workflow logic.
//
// focodebase/fobackend/internal/services/merchant_future_offering_engagement_internal.go
//
// GTM:
//
//	Layer: 2.6 Internal Services / Merchant Future Offering Domain
//	Release Class: SPINE
//	Reason:
//	  Merchant engagement configuration spans two authoritative tables
//	  (groups and options), the Platform catalog, and the Future Offering
//	  aggregate version. This file owns that transaction.
//
// Aggregate Version:
//
//	merchant_future_offerings.updated_at is the single optimistic-concurrency
//	version of the whole M01 draft. Replacing engagement configuration
//	advances it (MerchantFutureOfferingModel.AdvanceDraftVersionTx). This
//	gives the workspace one version to carry, makes submission's
//	expected_updated_at cover every composed readiness domain, and guarantees
//	a merchant cannot submit configuration they have not reviewed.
//
// Replacement Semantics:
//
//	PUT replaces the complete draft configuration. Array order is display
//	order. Groups may be saved empty (valid incomplete draft); readiness
//	reports them. Structural violations (duplicate actions, invalid quantity,
//	unknown/inactive catalog action) are rejected at save.
//
// Lock Order: see merchant_future_offerings_internal.go.
//
// SPINE Rule:
//
//	Keep compiling.
//	Keep production-ready.
//	Preserve draft-only, merchant-scoped, version-guarded replacement.
//	Preserve atomic replacement of groups and options.
//	Preserve aggregate-version advancement on every engagement mutation.
//	Block deployment if engagement can change without the FO row lock.
package services

import (
	"context"
	"fmt"
	"time"

	"github.com/PiccoloMondoC/focodebase/fobackend/internal/data"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// MerchantFutureOfferingEngagementOptionInput is one requested option.
type MerchantFutureOfferingEngagementOptionInput struct {
	EngagementActionID uuid.UUID
	QuantityEnabled    bool
	MinQuantity        *int
	MaxQuantity        *int
}

// MerchantFutureOfferingEngagementGroupInput is one requested group.
type MerchantFutureOfferingEngagementGroupInput struct {
	Name          *string
	MaxSelections *int
	Options       []MerchantFutureOfferingEngagementOptionInput
}

// MerchantFutureOfferingEngagementGroupView is a group with its options.
type MerchantFutureOfferingEngagementGroupView struct {
	Group   *data.MerchantFutureOfferingEngagementActionGroup
	Options []*data.MerchantFutureOfferingEngagementOption
}

// MerchantFutureOfferingEngagementConfiguration is the authoritative
// engagement configuration of one Future Offering at Version.
type MerchantFutureOfferingEngagementConfiguration struct {
	FutureOffering *data.MerchantFutureOffering
	Groups         []MerchantFutureOfferingEngagementGroupView
}

func assembleEngagementConfiguration(
	fo *data.MerchantFutureOffering,
	groups []*data.MerchantFutureOfferingEngagementActionGroup,
	options []*data.MerchantFutureOfferingEngagementOption,
) MerchantFutureOfferingEngagementConfiguration {
	byGroup := make(map[uuid.UUID][]*data.MerchantFutureOfferingEngagementOption, len(groups))
	for _, o := range options {
		byGroup[o.EngagementActionGroupID] = append(byGroup[o.EngagementActionGroupID], o)
	}
	views := make([]MerchantFutureOfferingEngagementGroupView, 0, len(groups))
	for _, g := range groups {
		opts := byGroup[g.ID]
		if opts == nil {
			opts = []*data.MerchantFutureOfferingEngagementOption{}
		}
		views = append(views, MerchantFutureOfferingEngagementGroupView{Group: g, Options: opts})
	}
	return MerchantFutureOfferingEngagementConfiguration{FutureOffering: fo, Groups: views}
}

// GetMerchantFutureOfferingEngagementConfiguration returns one coherent
// transaction snapshot of the Future Offering aggregate version and its
// engagement configuration. The read transaction takes no write lock.
func (s *Service) GetMerchantFutureOfferingEngagementConfiguration(ctx context.Context, merchantID, futureOfferingID uuid.UUID) (MerchantFutureOfferingEngagementConfiguration, error) {
	var result MerchantFutureOfferingEngagementConfiguration
	if err := validateMerchantFutureOfferingScope(merchantID, futureOfferingID); err != nil {
		return result, err
	}
	err := s.runMerchantFutureOfferingTx(ctx, "GetMerchantFutureOfferingEngagementConfiguration", false, func(ctx context.Context, tx pgx.Tx) error {
		fo, err := s.Models.MerchantFutureOffering.GetByIDForMerchantTx(ctx, tx, merchantID, futureOfferingID)
		if err != nil {
			return err
		}
		groups, err := s.Models.MerchantFutureOfferingEngagementActionGroup.ListActiveTx(ctx, tx, futureOfferingID)
		if err != nil {
			return err
		}
		options, err := s.Models.MerchantFutureOfferingEngagementOption.ListActiveTx(ctx, tx, futureOfferingID)
		if err != nil {
			return err
		}
		result = assembleEngagementConfiguration(fo, groups, options)
		return nil
	})
	return result, err
}

type canonicalEngagementGroup struct {
	group   data.NewMerchantFutureOfferingEngagementActionGroup
	options []MerchantFutureOfferingEngagementOptionInput
}

// canonicalizeEngagementInput performs structural validation outside the
// transaction and returns the canonical values that will be persisted.
func canonicalizeEngagementInput(
	groups []MerchantFutureOfferingEngagementGroupInput,
) ([]canonicalEngagementGroup, []uuid.UUID, error) {
	if len(groups) > data.MaxMerchantFutureOfferingEngagementGroups {
		return nil, nil, fmt.Errorf("%w: at most %d groups are supported",
			data.ErrMerchantFutureOfferingEngagementInvalidInput, data.MaxMerchantFutureOfferingEngagementGroups)
	}
	seen := make(map[uuid.UUID]struct{})
	actionIDs := make([]uuid.UUID, 0)
	out := make([]canonicalEngagementGroup, 0, len(groups))
	for gi, g := range groups {
		canonical, err := data.NormalizeMerchantFutureOfferingEngagementGroupInput(
			data.NewMerchantFutureOfferingEngagementActionGroup{
				Name:          g.Name,
				DisplayOrder:  gi,
				MaxSelections: g.MaxSelections,
			})
		if err != nil {
			return nil, nil, err
		}
		for _, o := range g.Options {
			if o.EngagementActionID == uuid.Nil {
				return nil, nil, fmt.Errorf("%w: engagement_action_id is required", data.ErrMerchantFutureOfferingEngagementInvalidInput)
			}
			if _, dup := seen[o.EngagementActionID]; dup {
				return nil, nil, fmt.Errorf("%w: an engagement action may be offered only once", data.ErrMerchantFutureOfferingEngagementInvalidInput)
			}
			seen[o.EngagementActionID] = struct{}{}
			if err := data.ValidateMerchantFutureOfferingEngagementQuantity(o.QuantityEnabled, o.MinQuantity, o.MaxQuantity); err != nil {
				return nil, nil, err
			}
			actionIDs = append(actionIDs, o.EngagementActionID)
		}
		out = append(out, canonicalEngagementGroup{group: canonical, options: g.Options})
	}
	return out, actionIDs, nil
}

// ReplaceMerchantFutureOfferingEngagementConfiguration atomically replaces a
// draft's engagement configuration and advances the aggregate version.
func (s *Service) ReplaceMerchantFutureOfferingEngagementConfiguration(
	ctx context.Context,
	merchantID, futureOfferingID uuid.UUID,
	expectedUpdatedAt time.Time,
	groups []MerchantFutureOfferingEngagementGroupInput,
) (MerchantFutureOfferingEngagementConfiguration, error) {
	var result MerchantFutureOfferingEngagementConfiguration
	if err := validateMerchantFutureOfferingScope(merchantID, futureOfferingID); err != nil {
		return result, err
	}
	if expectedUpdatedAt.IsZero() {
		return result, fmt.Errorf("%w: expected_updated_at is required", data.ErrMerchantFutureOfferingInvalidInput)
	}
	canonical, actionIDs, err := canonicalizeEngagementInput(groups)
	if err != nil {
		return result, err
	}

	err = s.runMerchantFutureOfferingTx(ctx, "ReplaceMerchantFutureOfferingEngagementConfiguration", true,
		func(ctx context.Context, tx pgx.Tx) error {
			if _, err := s.lockDraftAtVersion(ctx, tx, merchantID, futureOfferingID, expectedUpdatedAt); err != nil {
				return err
			}

			active, err := s.Models.EngagementAction.ListActiveByIDsForShareTx(ctx, tx, actionIDs)
			if err != nil {
				return err
			}
			for _, id := range actionIDs {
				if _, ok := active[id]; !ok {
					return data.ErrEngagementActionNotFound
				}
			}

			if err := s.Models.MerchantFutureOfferingEngagementOption.DeleteAllForDraftReplacementTx(ctx, tx, futureOfferingID); err != nil {
				return err
			}
			if err := s.Models.MerchantFutureOfferingEngagementActionGroup.DeleteAllForDraftReplacementTx(ctx, tx, futureOfferingID); err != nil {
				return err
			}

			persistedGroups := make([]*data.MerchantFutureOfferingEngagementActionGroup, 0, len(canonical))
			persistedOptions := make([]*data.MerchantFutureOfferingEngagementOption, 0, len(actionIDs))
			for _, cg := range canonical {
				group, err := s.Models.MerchantFutureOfferingEngagementActionGroup.InsertTx(ctx, tx, futureOfferingID, cg.group)
				if err != nil {
					return err
				}
				persistedGroups = append(persistedGroups, group)
				for oi, o := range cg.options {
					option, err := s.Models.MerchantFutureOfferingEngagementOption.InsertTx(ctx, tx, futureOfferingID,
						data.NewMerchantFutureOfferingEngagementOption{
							EngagementActionGroupID: group.ID,
							EngagementActionID:      o.EngagementActionID,
							QuantityEnabled:         o.QuantityEnabled,
							MinQuantity:             o.MinQuantity,
							MaxQuantity:             o.MaxQuantity,
							DisplayOrder:            oi,
						})
					if err != nil {
						return err
					}
					persistedOptions = append(persistedOptions, option)
				}
			}

			advanced, err := s.Models.MerchantFutureOffering.AdvanceDraftVersionTx(ctx, tx, merchantID, futureOfferingID, expectedUpdatedAt)
			if err != nil {
				return err
			}
			result = assembleEngagementConfiguration(advanced, persistedGroups, persistedOptions)
			return nil
		})
	if err != nil {
		return MerchantFutureOfferingEngagementConfiguration{}, err
	}
	return result, nil
}
