// Package main provides HTTP handlers for the privileged, read-only merchant
// program subscription billing-period surface.
//
// sdworkspace/sdbackend/internal/server/cmd/api/merchant_program_subscription_periods.go
//
// GTM:
//
//	Layer: 2.4 Merchant / Future Offering Domain
//	Release Class: SPINE
//	Reason:
//	  merchant_program_subscription_periods exposes privileged reads over the
//	  immutable, time-bounded plan and billing-cadence facts applicable to a
//	  merchant program subscription during specific commercial periods.
//
//	  Subscription periods are authoritative source facts for downstream
//	  monetization, including subscription_period billable events. They are not
//	  invoices, payments, fee calculations, billing-ledger entries, or
//	  subscription lifecycle events.
//
//	  Merchant program subscriptions are optional commercial packaging
//	  infrastructure beneath the Future Offering Platform and Monetization
//	  Layer. This handler capability remains compiled, complete, and
//	  production-ready regardless of whether Administration enables or disables
//	  subscriptions commercially.
//
//	  Canonical current subscription state remains owned by
//	  merchant_program_subscriptions. Canonical subscription lifecycle history
//	  remains owned by merchant_program_subscription_events. Subscription period
//	  records do not replace either source of truth.
//
//	  Period creation is intentionally not exposed through HTTP. Coordinating
//	  services own workflows that must combine period creation with subscription
//	  lifecycle mutation, lifecycle-event insertion, and downstream monetization
//	  work through transaction-compatible data-layer seams.
//
//	  Engineering owns authentication, authorization, bounded reads,
//	  deterministic retrieval, confidentiality, and integrity boundaries.
//	  Administration governs commercial and operational behavior within those
//	  boundaries.
//
// SPINE Rule:
//
//	Keep compiling.
//	Keep production-ready.
//	Preserve privileged authentication and authorization.
//	Preserve immutable period semantics.
//	Preserve canonical subscription-state ownership.
//	Preserve bounded deterministic timeline reads.
//	Preserve half-open period semantics owned by the data layer.
//	Preserve DB-owned created_at.
//	Preserve explicit response DTOs.
//	Preserve capability independently of commercial enablement.
//	Never expose period creation, update, deletion, restoration, or purge.
//	Do not infer merchant ownership from untrusted request values.
//	Do not synthesize subscription periods in the handler.
//	Do not default point-in-time resolution to an implicit current instant.
//	Do not hard-code subscription enablement, plan availability, renewal,
//	pricing, invoicing, or other commercial policy.
//	Block deployment if this file breaks build, authorization, period-history
//	integrity, point-in-time resolution, bounded reads, configuration separation,
//	or audit accountability.
package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/PiccoloMondoC/sdworkspace/sdbackend/internal/data"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

const (
	merchantProgramSubscriptionPeriodEntityType = "merchant_program_subscription_period"

	merchantProgramSubscriptionPeriodEntityTypeDescription = "Immutable merchant program subscription billing period entity"

	actionReadMerchantProgramSubscriptionPeriod = "read_merchant_program_subscription_period"

	actionListMerchantProgramSubscriptionPeriods = "list_merchant_program_subscription_periods"

	actionReadLatestMerchantProgramSubscriptionPeriod = "read_latest_merchant_program_subscription_period"

	actionReadMerchantProgramSubscriptionPeriodAtInstant = "read_merchant_program_subscription_period_at_instant"

	defaultMerchantProgramSubscriptionPeriodLimit = 50
	maxMerchantProgramSubscriptionPeriodLimit     = 100
)

// merchantProgramSubscriptionPeriodResponse is the stable HTTP presentation
// contract for one immutable merchant program subscription period.
type merchantProgramSubscriptionPeriodResponse struct {
	ID uuid.UUID `json:"id"`

	SubscriptionID uuid.UUID `json:"subscription_id"`

	PlanID uuid.UUID `json:"plan_id"`

	BillingPeriod data.MerchantProgramSubscriptionBillingPeriod `json:"billing_period"`

	PeriodStart time.Time `json:"period_start"`

	PeriodEnd time.Time `json:"period_end"`

	CreatedAt time.Time `json:"created_at"`
}

// merchantProgramSubscriptionPeriodPagination describes the bounded timeline
// window applied to a subscription-period list response.
type merchantProgramSubscriptionPeriodPagination struct {
	Limit int `json:"limit"`

	Offset int `json:"offset"`

	Count int `json:"count"`
}

// merchantProgramSubscriptionPeriodListResponse is the stable HTTP collection
// contract for one subscription's immutable period timeline.
type merchantProgramSubscriptionPeriodListResponse struct {
	Periods []merchantProgramSubscriptionPeriodResponse `json:"periods"`

	Pagination merchantProgramSubscriptionPeriodPagination `json:"pagination"`
}

// newMerchantProgramSubscriptionPeriodResponse converts one non-nil canonical
// persistence model into its authorized HTTP representation.
func newMerchantProgramSubscriptionPeriodResponse(
	period *data.MerchantProgramSubscriptionPeriod,
) merchantProgramSubscriptionPeriodResponse {
	return merchantProgramSubscriptionPeriodResponse{
		ID:             period.ID,
		SubscriptionID: period.SubscriptionID,
		PlanID:         period.PlanID,
		BillingPeriod:  period.BillingPeriod,
		PeriodStart:    period.PeriodStart,
		PeriodEnd:      period.PeriodEnd,
		CreatedAt:      period.CreatedAt,
	}
}

// parseMerchantProgramSubscriptionPeriodID extracts and validates the canonical
// subscription-period UUID from the route.
func parseMerchantProgramSubscriptionPeriodID(
	r *http.Request,
) (uuid.UUID, error) {
	rawID := strings.TrimSpace(
		chi.URLParam(r, "merchantProgramSubscriptionPeriodID"),
	)
	if rawID == "" {
		return uuid.Nil, errors.New(
			"merchant program subscription period ID is required",
		)
	}

	periodID, err := uuid.Parse(rawID)
	if err != nil || periodID == uuid.Nil {
		return uuid.Nil, errors.New(
			"invalid merchant program subscription period ID",
		)
	}

	return periodID, nil
}

// parseMerchantProgramSubscriptionPeriodPagination parses a bounded
// offset-pagination window for subscription-period timelines.
func parseMerchantProgramSubscriptionPeriodPagination(
	r *http.Request,
) (int, int, error) {
	limit := defaultMerchantProgramSubscriptionPeriodLimit
	offset := 0

	rawLimit := strings.TrimSpace(r.URL.Query().Get("limit"))
	if rawLimit != "" {
		parsedLimit, err := strconv.Atoi(rawLimit)
		if err != nil {
			return 0, 0, errors.New(
				"limit must be an integer",
			)
		}

		limit = parsedLimit
	}

	rawOffset := strings.TrimSpace(r.URL.Query().Get("offset"))
	if rawOffset != "" {
		parsedOffset, err := strconv.Atoi(rawOffset)
		if err != nil {
			return 0, 0, errors.New(
				"offset must be an integer",
			)
		}

		offset = parsedOffset
	}

	if limit <= 0 ||
		limit > maxMerchantProgramSubscriptionPeriodLimit {
		return 0, 0, fmt.Errorf(
			"limit must be between 1 and %d",
			maxMerchantProgramSubscriptionPeriodLimit,
		)
	}

	if offset < 0 {
		return 0, 0, errors.New(
			"offset must be non-negative",
		)
	}

	return limit, offset, nil
}

// parseMerchantProgramSubscriptionPeriodInstant parses the required point in
// time used for subscription-period resolution.
//
// The handler does not invent an implicit "current" period. Callers requesting
// point-in-time resolution must identify the instant explicitly.
func parseMerchantProgramSubscriptionPeriodInstant(
	r *http.Request,
) (time.Time, error) {
	rawInstant := strings.TrimSpace(
		r.URL.Query().Get("instant"),
	)
	if rawInstant == "" {
		return time.Time{}, errors.New(
			"instant query parameter is required",
		)
	}

	instant, err := time.Parse(time.RFC3339, rawInstant)
	if err != nil {
		return time.Time{}, errors.New(
			"instant must be a valid RFC3339 timestamp",
		)
	}

	return instant.UTC(), nil
}

// auditMerchantProgramSubscriptionPeriod records one governance audit entry
// without making audit persistence the owner of an otherwise successful read.
func (app *Application) auditMerchantProgramSubscriptionPeriod(
	ctx context.Context,
	userID *uuid.UUID,
	actionName string,
	actionDescription string,
	entityID string,
) error {
	return app.insertGovernanceAudit(
		ctx,
		userID,
		actionName,
		actionDescription,
		merchantProgramSubscriptionPeriodEntityType,
		merchantProgramSubscriptionPeriodEntityTypeDescription,
		entityID,
	)
}

// GetMerchantProgramSubscriptionPeriodByIDHandler retrieves one immutable
// merchant program subscription period by canonical period ID.
func (app *Application) GetMerchantProgramSubscriptionPeriodByIDHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	logger := app.Logger.
		GetLoggerWithContext(r).
		WithFunctionName(
			"GetMerchantProgramSubscriptionPeriodByIDHandler",
		)

	ctx, cancel := context.WithTimeout(
		r.Context(),
		cfgTimeout,
	)
	defer cancel()

	if !app.HasPermission(
		ctx,
		actionReadMerchantProgramSubscriptionPeriod,
	) {
		app.respondWithError(
			w,
			errors.New(
				"forbidden: insufficient permissions",
			),
			http.StatusForbidden,
		)
		return
	}

	userID := app.getUserIDFromContext(ctx)
	if userID == nil {
		app.respondWithError(
			w,
			errors.New(
				"user ID not found in context",
			),
			http.StatusUnauthorized,
		)
		return
	}

	periodID, err :=
		parseMerchantProgramSubscriptionPeriodID(r)
	if err != nil {
		app.respondWithError(
			w,
			err,
			http.StatusBadRequest,
		)
		return
	}

	period, err :=
		app.InternalServices.
			ResolveMerchantProgramSubscriptionPeriodByIDInternal(
				ctx,
				periodID,
			)
	if err != nil {
		logger.Error(
			"Get merchant program subscription period failed",
			"period_id",
			periodID,
			"error",
			err,
		)

		app.respondWithError(
			w,
			errors.New(
				"failed to retrieve merchant program subscription period",
			),
			http.StatusInternalServerError,
		)
		return
	}

	if period == nil {
		app.respondWithError(
			w,
			errors.New(
				"merchant program subscription period not found",
			),
			http.StatusNotFound,
		)
		return
	}

	if err := app.auditMerchantProgramSubscriptionPeriod(
		ctx,
		userID,
		actionReadMerchantProgramSubscriptionPeriod,
		"Read a merchant program subscription billing period",
		period.ID.String(),
	); err != nil {
		logger.Warn(
			"Merchant program subscription period read completed but audit recording failed",
			"period_id",
			period.ID,
			"subscription_id",
			period.SubscriptionID,
			"user_id",
			userID,
			"error",
			err,
		)
	}

	logger.Info(
		"Merchant program subscription period retrieved",
		"period_id",
		period.ID,
		"subscription_id",
		period.SubscriptionID,
		"plan_id",
		period.PlanID,
		"user_id",
		userID,
	)

	app.respondWithJSON(
		w,
		http.StatusOK,
		jsonResponse{
			Error: false,
			Message: "Merchant program subscription period " +
				"retrieved successfully",
			Data: newMerchantProgramSubscriptionPeriodResponse(
				period,
			),
		},
	)
}

// ListMerchantProgramSubscriptionPeriodsHandler retrieves the bounded,
// deterministic period timeline for one canonical subscription.
//
// An existing subscription with no periods returns an empty collection.
// A nonexistent or soft-deleted subscription returns not found under the
// canonical subscription model contract.
func (app *Application) ListMerchantProgramSubscriptionPeriodsHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	logger := app.Logger.
		GetLoggerWithContext(r).
		WithFunctionName(
			"ListMerchantProgramSubscriptionPeriodsHandler",
		)

	ctx, cancel := context.WithTimeout(
		r.Context(),
		cfgTimeout,
	)
	defer cancel()

	if !app.HasPermission(
		ctx,
		actionListMerchantProgramSubscriptionPeriods,
	) {
		app.respondWithError(
			w,
			errors.New(
				"forbidden: insufficient permissions",
			),
			http.StatusForbidden,
		)
		return
	}

	userID := app.getUserIDFromContext(ctx)
	if userID == nil {
		app.respondWithError(
			w,
			errors.New(
				"user ID not found in context",
			),
			http.StatusUnauthorized,
		)
		return
	}

	subscriptionID, err :=
		app.parseMerchantProgramSubscriptionID(r)
	if err != nil {
		app.respondWithError(
			w,
			err,
			http.StatusBadRequest,
		)
		return
	}

	limit, offset, err :=
		parseMerchantProgramSubscriptionPeriodPagination(r)
	if err != nil {
		app.respondWithError(
			w,
			err,
			http.StatusBadRequest,
		)
		return
	}

	subscription, err :=
		app.getMerchantProgramSubscriptionForRelatedRead(
			ctx,
			subscriptionID,
		)
	if err != nil {
		logger.Error(
			"Get subscription for period timeline failed",
			"subscription_id",
			subscriptionID,
			"error",
			err,
		)

		app.respondWithError(
			w,
			errors.New(
				"failed to retrieve merchant program subscription periods",
			),
			http.StatusInternalServerError,
		)
		return
	}

	if subscription == nil {
		app.respondWithError(
			w,
			errors.New(
				"merchant program subscription not found",
			),
			http.StatusNotFound,
		)
		return
	}

	periods, err :=
		app.InternalServices.
			ListMerchantProgramSubscriptionPeriodsBySubscriptionInternal(
				ctx,
				subscriptionID,
				limit,
				offset,
			)
	if err != nil {
		logger.Error(
			"List merchant program subscription periods failed",
			"subscription_id",
			subscriptionID,
			"limit",
			limit,
			"offset",
			offset,
			"error",
			err,
		)

		app.respondWithError(
			w,
			errors.New(
				"failed to retrieve merchant program subscription periods",
			),
			http.StatusInternalServerError,
		)
		return
	}

	responsePeriods :=
		make(
			[]merchantProgramSubscriptionPeriodResponse,
			0,
			len(periods),
		)

	for _, period := range periods {
		if period == nil {
			continue
		}

		responsePeriods = append(
			responsePeriods,
			newMerchantProgramSubscriptionPeriodResponse(
				period,
			),
		)
	}

	response := merchantProgramSubscriptionPeriodListResponse{
		Periods: responsePeriods,
		Pagination: merchantProgramSubscriptionPeriodPagination{
			Limit:  limit,
			Offset: offset,
			Count:  len(responsePeriods),
		},
	}

	if err := app.auditMerchantProgramSubscriptionPeriod(
		ctx,
		userID,
		actionListMerchantProgramSubscriptionPeriods,
		"List merchant program subscription billing periods",
		subscriptionID.String(),
	); err != nil {
		logger.Warn(
			"Merchant program subscription period read completed but audit recording failed",
			"subscription_id",
			subscriptionID,
			"user_id",
			userID,
			"error",
			err,
		)
	}

	logger.Info(
		"Merchant program subscription periods retrieved",
		"subscription_id",
		subscriptionID,
		"limit",
		limit,
		"offset",
		offset,
		"result_count",
		len(responsePeriods),
		"user_id",
		userID,
	)

	app.respondWithJSON(
		w,
		http.StatusOK,
		jsonResponse{
			Error: false,
			Message: "Merchant program subscription periods " +
				"retrieved successfully",
			Data: response,
		},
	)
}

// GetLatestMerchantProgramSubscriptionPeriodHandler retrieves the immutable
// period having the latest period_start for one canonical subscription.
//
// "Latest" means newest recorded period by the data layer's deterministic
// period_start DESC, id DESC ordering. It does not mean "currently active."
func (app *Application) GetLatestMerchantProgramSubscriptionPeriodHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	logger := app.Logger.
		GetLoggerWithContext(r).
		WithFunctionName(
			"GetLatestMerchantProgramSubscriptionPeriodHandler",
		)

	ctx, cancel := context.WithTimeout(
		r.Context(),
		cfgTimeout,
	)
	defer cancel()

	if !app.HasPermission(
		ctx,
		actionListMerchantProgramSubscriptionPeriods,
	) {
		app.respondWithError(
			w,
			errors.New(
				"forbidden: insufficient permissions",
			),
			http.StatusForbidden,
		)
		return
	}

	userID := app.getUserIDFromContext(ctx)
	if userID == nil {
		app.respondWithError(
			w,
			errors.New(
				"user ID not found in context",
			),
			http.StatusUnauthorized,
		)
		return
	}

	subscriptionID, err :=
		app.parseMerchantProgramSubscriptionID(r)
	if err != nil {
		app.respondWithError(
			w,
			err,
			http.StatusBadRequest,
		)
		return
	}

	subscription, err :=
		app.getMerchantProgramSubscriptionForRelatedRead(
			ctx,
			subscriptionID,
		)
	if err != nil {
		logger.Error(
			"Get subscription for latest period failed",
			"subscription_id",
			subscriptionID,
			"error",
			err,
		)

		app.respondWithError(
			w,
			errors.New(
				"failed to retrieve latest merchant program subscription period",
			),
			http.StatusInternalServerError,
		)
		return
	}

	if subscription == nil {
		app.respondWithError(
			w,
			errors.New(
				"merchant program subscription not found",
			),
			http.StatusNotFound,
		)
		return
	}

	period, err :=
		app.InternalServices.
			ResolveLatestMerchantProgramSubscriptionPeriodInternal(
				ctx,
				subscriptionID,
			)
	if err != nil {
		logger.Error(
			"Get latest merchant program subscription period failed",
			"subscription_id",
			subscriptionID,
			"error",
			err,
		)

		app.respondWithError(
			w,
			errors.New(
				"failed to retrieve latest merchant program subscription period",
			),
			http.StatusInternalServerError,
		)
		return
	}

	if period == nil {
		app.respondWithError(
			w,
			errors.New(
				"no merchant program subscription periods found",
			),
			http.StatusNotFound,
		)
		return
	}

	if err := app.auditMerchantProgramSubscriptionPeriod(
		ctx,
		userID,
		actionReadLatestMerchantProgramSubscriptionPeriod,
		"Read the latest merchant program subscription billing period",
		period.ID.String(),
	); err != nil {
		logger.Warn(
			"Merchant program subscription period read completed but audit recording failed",
			"period_id",
			period.ID,
			"subscription_id",
			subscriptionID,
			"user_id",
			userID,
			"error",
			err,
		)
	}

	logger.Info(
		"Latest merchant program subscription period retrieved",
		"period_id",
		period.ID,
		"subscription_id",
		subscriptionID,
		"user_id",
		userID,
	)

	app.respondWithJSON(
		w,
		http.StatusOK,
		jsonResponse{
			Error: false,
			Message: "Latest merchant program subscription period " +
				"retrieved successfully",
			Data: newMerchantProgramSubscriptionPeriodResponse(
				period,
			),
		},
	)
}

// GetMerchantProgramSubscriptionPeriodAtInstantHandler retrieves the immutable
// period containing an explicitly supplied instant for one canonical
// subscription.
//
// Half-open [period_start, period_end) containment remains owned by the data
// layer. This handler only parses and supplies the requested instant.
func (app *Application) GetMerchantProgramSubscriptionPeriodAtInstantHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	logger := app.Logger.
		GetLoggerWithContext(r).
		WithFunctionName(
			"GetMerchantProgramSubscriptionPeriodAtInstantHandler",
		)

	ctx, cancel := context.WithTimeout(
		r.Context(),
		cfgTimeout,
	)
	defer cancel()

	if !app.HasPermission(
		ctx,
		actionReadMerchantProgramSubscriptionPeriod,
	) {
		app.respondWithError(
			w,
			errors.New(
				"forbidden: insufficient permissions",
			),
			http.StatusForbidden,
		)
		return
	}

	userID := app.getUserIDFromContext(ctx)
	if userID == nil {
		app.respondWithError(
			w,
			errors.New(
				"user ID not found in context",
			),
			http.StatusUnauthorized,
		)
		return
	}

	subscriptionID, err :=
		app.parseMerchantProgramSubscriptionID(r)
	if err != nil {
		app.respondWithError(
			w,
			err,
			http.StatusBadRequest,
		)
		return
	}

	instant, err :=
		parseMerchantProgramSubscriptionPeriodInstant(r)
	if err != nil {
		app.respondWithError(
			w,
			err,
			http.StatusBadRequest,
		)
		return
	}

	subscription, err :=
		app.getMerchantProgramSubscriptionForRelatedRead(
			ctx,
			subscriptionID,
		)
	if err != nil {
		logger.Error(
			"Get subscription for period-at-instant lookup failed",
			"subscription_id",
			subscriptionID,
			"error",
			err,
		)

		app.respondWithError(
			w,
			errors.New(
				"failed to retrieve merchant program subscription period",
			),
			http.StatusInternalServerError,
		)
		return
	}

	if subscription == nil {
		app.respondWithError(
			w,
			errors.New(
				"merchant program subscription not found",
			),
			http.StatusNotFound,
		)
		return
	}

	period, err :=
		app.InternalServices.
			ResolveMerchantProgramSubscriptionPeriodAtInstantInternal(
				ctx,
				subscriptionID,
				instant,
			)
	if err != nil {
		logger.Error(
			"Get merchant program subscription period at instant failed",
			"subscription_id",
			subscriptionID,
			"instant",
			instant,
			"error",
			err,
		)

		app.respondWithError(
			w,
			errors.New(
				"failed to retrieve merchant program subscription period",
			),
			http.StatusInternalServerError,
		)
		return
	}

	if period == nil {
		app.respondWithError(
			w,
			errors.New(
				"no merchant program subscription period found for the given instant",
			),
			http.StatusNotFound,
		)
		return
	}

	if err := app.auditMerchantProgramSubscriptionPeriod(
		ctx,
		userID,
		actionReadMerchantProgramSubscriptionPeriodAtInstant,
		"Read the merchant program subscription billing period containing an instant",
		period.ID.String(),
	); err != nil {
		logger.Warn(
			"Merchant program subscription period read completed but audit recording failed",
			"period_id",
			period.ID,
			"subscription_id",
			subscriptionID,
			"user_id",
			userID,
			"error",
			err,
		)
	}

	logger.Info(
		"Merchant program subscription period at instant retrieved",
		"period_id",
		period.ID,
		"subscription_id",
		subscriptionID,
		"instant",
		instant,
		"user_id",
		userID,
	)

	app.respondWithJSON(
		w,
		http.StatusOK,
		jsonResponse{
			Error: false,
			Message: "Merchant program subscription period " +
				"retrieved successfully",
			Data: newMerchantProgramSubscriptionPeriodResponse(
				period,
			),
		},
	)
}
