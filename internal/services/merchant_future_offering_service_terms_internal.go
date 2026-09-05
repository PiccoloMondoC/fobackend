// Package services contains trusted internal service composition,
// automation support, moderation helpers, and shared internal workflow logic.
//
// sdworkspace/sdbackend/internal/services/merchant_future_offering_service_terms_internal.go
//
// GTM:
//
//	Layer: 2.6 Internal Services / Future Offering Commercial Domain
//	Release Class: SPINE
//	Reason:
//	  Owns Service Term workflow semantics that cross persistence and governed
//	  Administration configuration boundaries.
//
//	  Persistence, lifecycle integrity, replacement atomicity, and the absolute
//	  Engineering duration envelope remain data-layer responsibilities.
//
//	  This service does not know about Service Period, Billing Period, Payment
//	  Period, invoicing, fee calculation, payment, or downstream event
//	  consumers.
//
// SPINE Rule:
//
//	Keep compiling.
//	Keep production-ready.
//	Preserve the Engineering duration envelope of >0 and <=1188 months.
//	Allow Administration to narrow, but never widen, that envelope.
//	Preserve atomic policy validation for establishment and replacement.
//	Preserve established historical commercial facts.
//	Do not manufacture calendar-duration semantics not established by FOCA.
//	Do not publish lifecycle events without canonical transactional outbox
//	infrastructure.
//	Do not couple Service Terms to downstream consumers.
//	Block deployment if administrative-range enforcement, lifecycle atomicity,
//	domain isolation, or build integrity is weakened.
package services

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/PiccoloMondoC/focodebase/fobackend/internal/data"

	"github.com/google/uuid"
)

const merchantFutureOfferingServiceTermDurationRangeSettingKey = "merchant_future_offering_service_term_duration_range"

// merchantFutureOfferingServiceTermOperatingRange is the effective currently
// permitted duration range.
//
// Engineering supplies the absolute capability envelope. Active Administration
// configuration may narrow either boundary but may never widen it.
type merchantFutureOfferingServiceTermOperatingRange struct {
	MinDurationMonths int
	MaxDurationMonths int
}

// merchantFutureOfferingServiceTermOperatingRangeSetting is the optional
// Administration configuration persisted in the canonical Platform Settings
// domain.
//
// Nil fields mean Administration has not narrowed that Engineering boundary.
type merchantFutureOfferingServiceTermOperatingRangeSetting struct {
	MinDurationMonths *int `json:"min_duration_months"`
	MaxDurationMonths *int `json:"max_duration_months"`
}

func (r merchantFutureOfferingServiceTermOperatingRange) validate(
	durationMonths int,
) error {
	if durationMonths < r.MinDurationMonths ||
		durationMonths > r.MaxDurationMonths {
		return fmt.Errorf(
			"%w: duration_months %d is outside the currently permitted range",
			ErrMerchantFutureOfferingServiceTermDurationOutsideOperatingRange,
			durationMonths,
		)
	}

	return nil
}

// resolveMerchantFutureOfferingServiceTermOperatingRange resolves one
// operation's effective Service Term duration policy snapshot.
//
// Absence of Administration configuration means no narrowing has been applied
// and therefore leaves the absolute Engineering envelope in effect.
//
// The range is stored as one JSON setting so Administration cannot expose
// merchant operations to a torn min/max configuration assembled from two
// independently changing rows.
func (s *Service) resolveMerchantFutureOfferingServiceTermOperatingRange(
	ctx context.Context,
) (
	merchantFutureOfferingServiceTermOperatingRange,
	error,
) {
	operatingRange := merchantFutureOfferingServiceTermOperatingRange{
		MinDurationMonths: 1,
		MaxDurationMonths: data.MerchantFutureOfferingServiceTermMaxDurationMonths,
	}

	setting, err := s.Models.PlatformSetting.GetActiveByKey(
		ctx,
		merchantFutureOfferingServiceTermDurationRangeSettingKey,
	)
	if err != nil {
		return merchantFutureOfferingServiceTermOperatingRange{},
			fmt.Errorf(
				"%w: read %s: %w",
				ErrMerchantFutureOfferingServiceTermOperatingRangeInvalid,
				merchantFutureOfferingServiceTermDurationRangeSettingKey,
				err,
			)
	}

	if setting == nil {
		return operatingRange, nil
	}

	if setting.ValueType != data.PlatformSettingValueTypeJSON {
		return merchantFutureOfferingServiceTermOperatingRange{},
			fmt.Errorf(
				"%w: %s must use JSON value type",
				ErrMerchantFutureOfferingServiceTermOperatingRangeInvalid,
				merchantFutureOfferingServiceTermDurationRangeSettingKey,
			)
	}

	decoder := json.NewDecoder(
		bytes.NewReader(setting.SettingValue),
	)
	decoder.DisallowUnknownFields()

	var configured merchantFutureOfferingServiceTermOperatingRangeSetting

	if err := decoder.Decode(&configured); err != nil {
		return merchantFutureOfferingServiceTermOperatingRange{},
			fmt.Errorf(
				"%w: %s contains invalid range configuration",
				ErrMerchantFutureOfferingServiceTermOperatingRangeInvalid,
				merchantFutureOfferingServiceTermDurationRangeSettingKey,
			)
	}

	var trailing any

	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return merchantFutureOfferingServiceTermOperatingRange{},
			fmt.Errorf(
				"%w: %s contains invalid trailing JSON",
				ErrMerchantFutureOfferingServiceTermOperatingRangeInvalid,
				merchantFutureOfferingServiceTermDurationRangeSettingKey,
			)
	}

	if configured.MinDurationMonths != nil {
		operatingRange.MinDurationMonths =
			*configured.MinDurationMonths
	}

	if configured.MaxDurationMonths != nil {
		operatingRange.MaxDurationMonths =
			*configured.MaxDurationMonths
	}

	if operatingRange.MinDurationMonths <= 0 ||
		operatingRange.MaxDurationMonths <= 0 ||
		operatingRange.MaxDurationMonths >
			data.MerchantFutureOfferingServiceTermMaxDurationMonths ||
		operatingRange.MinDurationMonths >
			operatingRange.MaxDurationMonths {
		return merchantFutureOfferingServiceTermOperatingRange{},
			fmt.Errorf(
				"%w: configured range is outside the Engineering envelope",
				ErrMerchantFutureOfferingServiceTermOperatingRangeInvalid,
			)
	}

	return operatingRange, nil
}

// ProposeMerchantFutureOfferingServiceTermInternal creates a proposed Service
// Term after enforcing the currently effective operating range.
func (s *Service) ProposeMerchantFutureOfferingServiceTermInternal(
	ctx context.Context,
	futureOfferingID uuid.UUID,
	proposal data.MerchantFutureOfferingServiceTermProposal,
) (*data.MerchantFutureOfferingServiceTerm, error) {
	if err := s.validate(); err != nil {
		return nil, err
	}
	if ctx == nil {
		return nil, ErrNilContext
	}

	ctx, cancel := context.WithTimeout(
		ctx,
		s.Cfg.DBTimeout,
	)
	defer cancel()

	operatingRange, err :=
		s.resolveMerchantFutureOfferingServiceTermOperatingRange(ctx)
	if err != nil {
		return nil, err
	}

	if err := operatingRange.validate(
		proposal.DurationMonths,
	); err != nil {
		return nil, err
	}

	return s.Models.MerchantFutureOfferingServiceTerm.Propose(
		ctx,
		futureOfferingID,
		proposal,
	)
}

// UpdateProposedMerchantFutureOfferingServiceTermInternal replaces the mutable
// facts of a proposed Service Term after enforcing the currently effective
// operating range.
func (s *Service) UpdateProposedMerchantFutureOfferingServiceTermInternal(
	ctx context.Context,
	id uuid.UUID,
	futureOfferingID uuid.UUID,
	proposal data.MerchantFutureOfferingServiceTermProposal,
) (*data.MerchantFutureOfferingServiceTerm, error) {
	if err := s.validate(); err != nil {
		return nil, err
	}
	if ctx == nil {
		return nil, ErrNilContext
	}

	ctx, cancel := context.WithTimeout(
		ctx,
		s.Cfg.DBTimeout,
	)
	defer cancel()

	operatingRange, err :=
		s.resolveMerchantFutureOfferingServiceTermOperatingRange(ctx)
	if err != nil {
		return nil, err
	}

	if err := operatingRange.validate(
		proposal.DurationMonths,
	); err != nil {
		return nil, err
	}

	return s.Models.MerchantFutureOfferingServiceTerm.UpdateProposed(
		ctx,
		id,
		futureOfferingID,
		proposal,
	)
}

// EstablishMerchantFutureOfferingServiceTermInternal establishes an existing
// proposal as the initial authoritative Service Term.
//
// The proposal row is locked before its duration and current Administration
// policy are evaluated. This prevents proposal mutation while the establishment
// decision is made.
//
// The effective operating range resolved after locking is the policy snapshot
// governing this lifecycle decision. Administration changes committed after
// that snapshot govern subsequent decisions and do not retroactively invalidate
// this establishment.
//
// term_starts_on and term_ends_on remain authoritative DATE-semantic facts.
// This service deliberately does not manufacture calendar-duration arithmetic
// that FOCA has not yet defined.
func (s *Service) EstablishMerchantFutureOfferingServiceTermInternal(
	ctx context.Context,
	id uuid.UUID,
	futureOfferingID uuid.UUID,
	termStartsOn time.Time,
	termEndsOn time.Time,
) (*data.MerchantFutureOfferingServiceTerm, error) {
	if err := s.validate(); err != nil {
		return nil, err
	}
	if ctx == nil {
		return nil, ErrNilContext
	}

	ctx, cancel := context.WithTimeout(
		ctx,
		s.Cfg.DBTimeout,
	)
	defer cancel()

	tx, err :=
		s.Models.MerchantFutureOfferingServiceTerm.BeginTx(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	proposed, err :=
		s.Models.MerchantFutureOfferingServiceTerm.
			GetProposedForUpdateTx(
				ctx,
				tx,
				id,
				futureOfferingID,
			)
	if err != nil {
		return nil, err
	}

	operatingRange, err :=
		s.resolveMerchantFutureOfferingServiceTermOperatingRange(ctx)
	if err != nil {
		return nil, err
	}

	if err := operatingRange.validate(
		proposed.DurationMonths,
	); err != nil {
		return nil, err
	}

	established, err :=
		s.Models.MerchantFutureOfferingServiceTerm.EstablishTx(
			ctx,
			tx,
			id,
			futureOfferingID,
			termStartsOn,
			termEndsOn,
		)
	if err != nil {
		return nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf(
			"commit merchant future offering service term establishment: %w",
			err,
		)
	}

	// Domain-event seam:
	//
	// Establishment is a producer-owned domain occurrence. Reliable publication
	// requires canonical transactional outbox infrastructure so the event record
	// commits atomically with this mutation. Until that infrastructure exists,
	// deliberately publish nothing here.

	return established, nil
}

// ReplaceEstablishedMerchantFutureOfferingServiceTermInternal atomically
// supersedes an established predecessor and establishes its proposed
// replacement.
//
// The predecessor and replacement are locked before the replacement duration
// and current Administration policy are evaluated. This prevents concurrent
// proposal mutation from bypassing the policy governing replacement.
//
// The effective operating range resolved after locking is the policy snapshot
// governing this lifecycle decision. Administration changes committed after
// that snapshot govern subsequent decisions and do not retroactively invalidate
// this replacement.
func (s *Service) ReplaceEstablishedMerchantFutureOfferingServiceTermInternal(
	ctx context.Context,
	predecessorID uuid.UUID,
	replacementID uuid.UUID,
	futureOfferingID uuid.UUID,
	termStartsOn time.Time,
	termEndsOn time.Time,
) (
	*data.MerchantFutureOfferingServiceTerm,
	*data.MerchantFutureOfferingServiceTerm,
	error,
) {
	if err := s.validate(); err != nil {
		return nil, nil, err
	}
	if ctx == nil {
		return nil, nil, ErrNilContext
	}

	ctx, cancel := context.WithTimeout(
		ctx,
		s.Cfg.DBTimeout,
	)
	defer cancel()

	tx, err :=
		s.Models.MerchantFutureOfferingServiceTerm.BeginTx(ctx)
	if err != nil {
		return nil, nil, err
	}
	defer tx.Rollback(ctx)

	replacement, err :=
		s.Models.MerchantFutureOfferingServiceTerm.
			GetReplacementCandidateForUpdateTx(
				ctx,
				tx,
				predecessorID,
				replacementID,
				futureOfferingID,
			)
	if err != nil {
		return nil, nil, err
	}

	operatingRange, err :=
		s.resolveMerchantFutureOfferingServiceTermOperatingRange(ctx)
	if err != nil {
		return nil, nil, err
	}

	if err := operatingRange.validate(
		replacement.DurationMonths,
	); err != nil {
		return nil, nil, err
	}

	predecessor, established, err :=
		s.Models.MerchantFutureOfferingServiceTerm.
			ReplaceEstablishedTx(
				ctx,
				tx,
				predecessorID,
				replacementID,
				futureOfferingID,
				termStartsOn,
				termEndsOn,
			)
	if err != nil {
		return nil, nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, nil, fmt.Errorf(
			"commit merchant future offering service term replacement: %w",
			err,
		)
	}

	// Domain-event seam:
	//
	// Replacement is a producer-owned Service Term occurrence. Do not invoke
	// Service Period, Billing Period, Payment Period, invoice, or payment
	// consumers here. Future publication belongs to the canonical outbox seam.

	return predecessor, established, nil
}
