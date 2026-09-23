// Package main provides HTTP handlers for privileged observation and
// historical retrieval of Merchant Future Offering Billing Periods.
//
// focodebase/fobackend/internal/server/cmd/api/merchant_future_offering_billing_periods.go
//
// GTM:
//
//	Layer: 3.2 API / Future Offering Commercial Domain
//	Release Class: SPINE
//	Reason:
//	  Billing Periods are immutable, authoritative monthly accounting and
//	  consumption windows within an established Future Offering Service Term.
//	  This handler exposes privileged observation and historical reconstruction
//	  without allowing HTTP callers to manufacture Billing Period chronology,
//	  cadence, calendar semantics, or lifecycle mutations.
//
//	  Billing Period creation is transaction-only and remains a
//	  service/orchestration responsibility. A future producer workflow may
//	  therefore compose Billing Period persistence atomically with its
//	  producer-owned outbox fact without coupling this HTTP boundary to event
//	  consumers or transport.
//
// SPINE Rule:
//
//	Keep compiling.
//	Keep production-ready.
//	Preserve permission-first authorization on every route and handler.
//	Preserve Future Offering scope on every read.
//	Preserve immutable, date-based Billing Period semantics.
//	Preserve bounded deterministic keyset pagination.
//	Keep Billing Period creation and transaction orchestration out of HTTP.
//	Do not expose update, delete, supersession, replacement, retirement,
//	soft-delete, restore, or generic mutation behavior.
//	Do not derive Billing Period identity or lifecycle from Service Period rows.
//	Do not couple this handler to QAE qualification, AHO measurement,
//	fee calculation, invoices, FO financial accounts, funding, payments,
//	settlement, event consumers, or transport.
//	Block deployment if authorization, scoping, accounting chronology,
//	historical reconstruction, pagination safety, auditability, or domain
//	ownership is weakened.
package main

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/PiccoloMondoC/focodebase/fobackend/internal/data"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// -----------------------------------------------------------------------------
// Entity / action / permission identifiers
// -----------------------------------------------------------------------------

const (
	merchantFutureOfferingBillingPeriodEntityType = "merchant_future_offering_billing_period"

	merchantFutureOfferingBillingPeriodEntityTypeDescription = "Immutable Future Offering Billing Period accounting-window history entity"

	actionReadMerchantFutureOfferingBillingPeriod = "read_merchant_future_offering_billing_period"

	actionListMerchantFutureOfferingBillingPeriods = "list_merchant_future_offering_billing_periods"

	permissionReadMerchantFutureOfferingBillingPeriod = "read_merchant_future_offering_billing_period"

	permissionListMerchantFutureOfferingBillingPeriods = "list_merchant_future_offering_billing_periods"
)

const merchantFutureOfferingBillingPeriodDateLayout = "2006-01-02"

const (
	merchantFutureOfferingBillingPeriodTimelineDefaultLimit = 20
	merchantFutureOfferingBillingPeriodTimelineMaxLimit     = 100

	// The encoded cursor contains only a YYYY-MM-DD domain date, separator,
	// and UUID. This ceiling is intentionally larger than a valid cursor while
	// bounding attacker-controlled decoding input.
	merchantFutureOfferingBillingPeriodTimelineCursorMaxEncodedLength = 256
)

// -----------------------------------------------------------------------------
// Response DTOs
// -----------------------------------------------------------------------------

// merchantFutureOfferingBillingPeriodDTO is the stable HTTP representation of
// one immutable Future Offering Billing Period.
//
// PostgreSQL DATE values are deliberately exposed as YYYY-MM-DD strings.
// Billing Period boundaries are domain dates, not elapsed-time instants.
type merchantFutureOfferingBillingPeriodDTO struct {
	ID               uuid.UUID `json:"id"`
	ServiceTermID    uuid.UUID `json:"service_term_id"`
	FutureOfferingID uuid.UUID `json:"future_offering_id"`
	PeriodNumber     int       `json:"period_number"`
	PeriodStartsOn   string    `json:"period_starts_on"`
	PeriodEndsOn     string    `json:"period_ends_on"`
	CreatedAt        time.Time `json:"created_at"`
}

func newMerchantFutureOfferingBillingPeriodDTO(
	period *data.MerchantFutureOfferingBillingPeriod,
) merchantFutureOfferingBillingPeriodDTO {
	return merchantFutureOfferingBillingPeriodDTO{
		ID:               period.ID,
		ServiceTermID:    period.ServiceTermID,
		FutureOfferingID: period.FutureOfferingID,
		PeriodNumber:     period.PeriodNumber,
		PeriodStartsOn: formatMerchantFutureOfferingBillingPeriodDate(
			period.PeriodStartsOn,
		),
		PeriodEndsOn: formatMerchantFutureOfferingBillingPeriodDate(
			period.PeriodEndsOn,
		),
		CreatedAt: period.CreatedAt,
	}
}

func newMerchantFutureOfferingBillingPeriodDTOs(
	periods []*data.MerchantFutureOfferingBillingPeriod,
) []merchantFutureOfferingBillingPeriodDTO {
	dtos := make(
		[]merchantFutureOfferingBillingPeriodDTO,
		0,
		len(periods),
	)

	for _, period := range periods {
		dtos = append(
			dtos,
			newMerchantFutureOfferingBillingPeriodDTO(period),
		)
	}

	return dtos
}

// merchantFutureOfferingBillingPeriodTimelineResponse presents one bounded
// historical page together with an opaque continuation cursor where another
// page may exist.
type merchantFutureOfferingBillingPeriodTimelineResponse struct {
	Periods []merchantFutureOfferingBillingPeriodDTO `json:"periods"`

	NextCursor string `json:"next_cursor,omitempty"`
}

// -----------------------------------------------------------------------------
// Read by ID
// -----------------------------------------------------------------------------

// GetMerchantFutureOfferingBillingPeriodHandler retrieves one immutable Billing
// Period strictly within the Future Offering identified by the route.
//
// GetByID is globally scoped to Billing Period identity. The handler therefore
// verifies the returned FutureOfferingID and presents cross-scope mismatch as
// not found.
func (app *Application) GetMerchantFutureOfferingBillingPeriodHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	const fn = "GetMerchantFutureOfferingBillingPeriodHandler"

	ctx, cancel := context.WithTimeout(
		r.Context(),
		cfgTimeout,
	)
	defer cancel()

	if !app.HasPermission(
		ctx,
		permissionReadMerchantFutureOfferingBillingPeriod,
	) {
		app.respondWithError(
			w,
			errors.New("forbidden: insufficient permissions"),
			http.StatusForbidden,
		)
		return
	}

	userID := app.getUserIDFromContext(ctx)
	if userID == nil {
		app.respondWithError(
			w,
			errors.New("user ID not found in context"),
			http.StatusUnauthorized,
		)
		return
	}

	futureOfferingID, err :=
		parseMerchantFutureOfferingBillingPeriodPathUUID(
			r,
			"futureOfferingID",
		)
	if err != nil {
		app.respondWithError(
			w,
			err,
			http.StatusBadRequest,
		)
		return
	}

	billingPeriodID, err :=
		parseMerchantFutureOfferingBillingPeriodPathUUID(
			r,
			"billingPeriodID",
		)
	if err != nil {
		app.respondWithError(
			w,
			err,
			http.StatusBadRequest,
		)
		return
	}

	period, err :=
		app.Models.MerchantFutureOfferingBillingPeriod.GetByID(
			ctx,
			billingPeriodID,
		)
	if err != nil {
		app.respondMerchantFutureOfferingBillingPeriodError(
			w,
			r,
			fn,
			err,
		)
		return
	}

	if period.FutureOfferingID != futureOfferingID {
		app.respondWithError(
			w,
			errors.New("billing period not found"),
			http.StatusNotFound,
		)
		return
	}

	app.auditMerchantFutureOfferingBillingPeriodRead(
		ctx,
		r,
		userID,
		actionReadMerchantFutureOfferingBillingPeriod,
		"Read a Merchant Future Offering Billing Period",
		period.ID.String(),
	)

	app.respondWithJSON(
		w,
		http.StatusOK,
		jsonResponse{
			Error:   false,
			Message: "Billing period retrieved successfully",
			Data:    newMerchantFutureOfferingBillingPeriodDTO(period),
		},
	)
}

// -----------------------------------------------------------------------------
// Current at domain date
// -----------------------------------------------------------------------------

// GetCurrentMerchantFutureOfferingBillingPeriodAtDateHandler retrieves the
// Billing Period containing the supplied domain date when its owning Service
// Term is presently established.
//
// Historical reconstruction against a specifically identified Service Term is
// provided separately.
func (app *Application) GetCurrentMerchantFutureOfferingBillingPeriodAtDateHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	const fn = "GetCurrentMerchantFutureOfferingBillingPeriodAtDateHandler"

	ctx, cancel := context.WithTimeout(
		r.Context(),
		cfgTimeout,
	)
	defer cancel()

	if !app.HasPermission(
		ctx,
		permissionReadMerchantFutureOfferingBillingPeriod,
	) {
		app.respondWithError(
			w,
			errors.New("forbidden: insufficient permissions"),
			http.StatusForbidden,
		)
		return
	}

	userID := app.getUserIDFromContext(ctx)
	if userID == nil {
		app.respondWithError(
			w,
			errors.New("user ID not found in context"),
			http.StatusUnauthorized,
		)
		return
	}

	futureOfferingID, err :=
		parseMerchantFutureOfferingBillingPeriodPathUUID(
			r,
			"futureOfferingID",
		)
	if err != nil {
		app.respondWithError(
			w,
			err,
			http.StatusBadRequest,
		)
		return
	}

	on, err :=
		parseMerchantFutureOfferingBillingPeriodDate(
			r.URL.Query().Get("on"),
		)
	if err != nil {
		app.respondWithError(
			w,
			fmt.Errorf("'on': %w", err),
			http.StatusBadRequest,
		)
		return
	}

	period, err :=
		app.Models.MerchantFutureOfferingBillingPeriod.GetCurrentAt(
			ctx,
			futureOfferingID,
			on,
		)
	if err != nil {
		app.respondMerchantFutureOfferingBillingPeriodError(
			w,
			r,
			fn,
			err,
		)
		return
	}

	app.auditMerchantFutureOfferingBillingPeriodRead(
		ctx,
		r,
		userID,
		actionReadMerchantFutureOfferingBillingPeriod,
		"Read the current Merchant Future Offering Billing Period at a date",
		period.ID.String(),
	)

	app.respondWithJSON(
		w,
		http.StatusOK,
		jsonResponse{
			Error:   false,
			Message: "Current billing period retrieved successfully",
			Data:    newMerchantFutureOfferingBillingPeriodDTO(period),
		},
	)
}

// -----------------------------------------------------------------------------
// Historical reconstruction for explicit Service Term
// -----------------------------------------------------------------------------

// GetMerchantFutureOfferingBillingPeriodForServiceTermAtDateHandler retrieves
// the Billing Period containing the supplied domain date for an explicitly
// identified Service Term.
//
// This is historical reconstruction. The Service Term is explicit rather than
// implicitly replaced by whichever Service Term is currently established.
func (app *Application) GetMerchantFutureOfferingBillingPeriodForServiceTermAtDateHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	const fn = "GetMerchantFutureOfferingBillingPeriodForServiceTermAtDateHandler"

	ctx, cancel := context.WithTimeout(
		r.Context(),
		cfgTimeout,
	)
	defer cancel()

	if !app.HasPermission(
		ctx,
		permissionReadMerchantFutureOfferingBillingPeriod,
	) {
		app.respondWithError(
			w,
			errors.New("forbidden: insufficient permissions"),
			http.StatusForbidden,
		)
		return
	}

	userID := app.getUserIDFromContext(ctx)
	if userID == nil {
		app.respondWithError(
			w,
			errors.New("user ID not found in context"),
			http.StatusUnauthorized,
		)
		return
	}

	futureOfferingID, err :=
		parseMerchantFutureOfferingBillingPeriodPathUUID(
			r,
			"futureOfferingID",
		)
	if err != nil {
		app.respondWithError(
			w,
			err,
			http.StatusBadRequest,
		)
		return
	}

	serviceTermID, err :=
		parseMerchantFutureOfferingBillingPeriodPathUUID(
			r,
			"serviceTermID",
		)
	if err != nil {
		app.respondWithError(
			w,
			err,
			http.StatusBadRequest,
		)
		return
	}

	on, err :=
		parseMerchantFutureOfferingBillingPeriodDate(
			r.URL.Query().Get("on"),
		)
	if err != nil {
		app.respondWithError(
			w,
			fmt.Errorf("'on': %w", err),
			http.StatusBadRequest,
		)
		return
	}

	period, err :=
		app.Models.MerchantFutureOfferingBillingPeriod.
			GetForServiceTermAt(
				ctx,
				serviceTermID,
				futureOfferingID,
				on,
			)
	if err != nil {
		app.respondMerchantFutureOfferingBillingPeriodError(
			w,
			r,
			fn,
			err,
		)
		return
	}

	app.auditMerchantFutureOfferingBillingPeriodRead(
		ctx,
		r,
		userID,
		actionReadMerchantFutureOfferingBillingPeriod,
		"Read a Merchant Future Offering Billing Period for a Service Term at a date",
		period.ID.String(),
	)

	app.respondWithJSON(
		w,
		http.StatusOK,
		jsonResponse{
			Error:   false,
			Message: "Billing period retrieved successfully",
			Data:    newMerchantFutureOfferingBillingPeriodDTO(period),
		},
	)
}

// -----------------------------------------------------------------------------
// Service Term history
// -----------------------------------------------------------------------------

// ListMerchantFutureOfferingBillingPeriodsForServiceTermHandler returns all
// persisted Billing Periods for one Service Term in canonical period-number
// order.
//
// The data contract is structurally bounded to at most 1188 periods per Service
// Term, so this operation does not require an additional API pagination model.
func (app *Application) ListMerchantFutureOfferingBillingPeriodsForServiceTermHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	const fn = "ListMerchantFutureOfferingBillingPeriodsForServiceTermHandler"

	ctx, cancel := context.WithTimeout(
		r.Context(),
		cfgTimeout,
	)
	defer cancel()

	if !app.HasPermission(
		ctx,
		permissionListMerchantFutureOfferingBillingPeriods,
	) {
		app.respondWithError(
			w,
			errors.New("forbidden: insufficient permissions"),
			http.StatusForbidden,
		)
		return
	}

	userID := app.getUserIDFromContext(ctx)
	if userID == nil {
		app.respondWithError(
			w,
			errors.New("user ID not found in context"),
			http.StatusUnauthorized,
		)
		return
	}

	futureOfferingID, err :=
		parseMerchantFutureOfferingBillingPeriodPathUUID(
			r,
			"futureOfferingID",
		)
	if err != nil {
		app.respondWithError(
			w,
			err,
			http.StatusBadRequest,
		)
		return
	}

	serviceTermID, err :=
		parseMerchantFutureOfferingBillingPeriodPathUUID(
			r,
			"serviceTermID",
		)
	if err != nil {
		app.respondWithError(
			w,
			err,
			http.StatusBadRequest,
		)
		return
	}

	periods, err :=
		app.Models.MerchantFutureOfferingBillingPeriod.
			ListForServiceTerm(
				ctx,
				serviceTermID,
				futureOfferingID,
			)
	if err != nil {
		app.respondMerchantFutureOfferingBillingPeriodError(
			w,
			r,
			fn,
			err,
		)
		return
	}

	app.auditMerchantFutureOfferingBillingPeriodRead(
		ctx,
		r,
		userID,
		actionListMerchantFutureOfferingBillingPeriods,
		"List Merchant Future Offering Billing Periods for a Service Term",
		fmt.Sprintf("service-term:%s", serviceTermID),
	)

	app.respondWithJSON(
		w,
		http.StatusOK,
		jsonResponse{
			Error:   false,
			Message: "Billing periods retrieved successfully",
			Data:    newMerchantFutureOfferingBillingPeriodDTOs(periods),
		},
	)
}

// -----------------------------------------------------------------------------
// Future Offering timeline
// -----------------------------------------------------------------------------

// ListMerchantFutureOfferingBillingPeriodTimelineHandler returns bounded
// Billing Period history across Service Term revisions using the data layer's
// deterministic (period_starts_on, id) keyset.
//
// The internal keyset is represented externally as an opaque cursor so the
// HTTP contract does not expose persistence-pagination structure as two
// independently meaningful API parameters.
func (app *Application) ListMerchantFutureOfferingBillingPeriodTimelineHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	const fn = "ListMerchantFutureOfferingBillingPeriodTimelineHandler"

	ctx, cancel := context.WithTimeout(
		r.Context(),
		cfgTimeout,
	)
	defer cancel()

	if !app.HasPermission(
		ctx,
		permissionListMerchantFutureOfferingBillingPeriods,
	) {
		app.respondWithError(
			w,
			errors.New("forbidden: insufficient permissions"),
			http.StatusForbidden,
		)
		return
	}

	userID := app.getUserIDFromContext(ctx)
	if userID == nil {
		app.respondWithError(
			w,
			errors.New("user ID not found in context"),
			http.StatusUnauthorized,
		)
		return
	}

	futureOfferingID, err :=
		parseMerchantFutureOfferingBillingPeriodPathUUID(
			r,
			"futureOfferingID",
		)
	if err != nil {
		app.respondWithError(
			w,
			err,
			http.StatusBadRequest,
		)
		return
	}

	limit := merchantFutureOfferingBillingPeriodTimelineDefaultLimit

	if raw := strings.TrimSpace(
		r.URL.Query().Get("limit"),
	); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed <= 0 {
			app.respondWithError(
				w,
				errors.New("'limit' must be a positive integer"),
				http.StatusBadRequest,
			)
			return
		}

		if parsed > merchantFutureOfferingBillingPeriodTimelineMaxLimit {
			parsed = merchantFutureOfferingBillingPeriodTimelineMaxLimit
		}

		limit = parsed
	}

	var cursor *data.MerchantFutureOfferingBillingPeriodTimelineCursor

	if raw := strings.TrimSpace(
		r.URL.Query().Get("cursor"),
	); raw != "" {
		cursor, err =
			decodeMerchantFutureOfferingBillingPeriodTimelineCursor(
				raw,
			)
		if err != nil {
			app.respondWithError(
				w,
				err,
				http.StatusBadRequest,
			)
			return
		}
	}

	periods, err :=
		app.Models.MerchantFutureOfferingBillingPeriod.
			ListTimelineForFutureOffering(
				ctx,
				futureOfferingID,
				limit,
				cursor,
			)
	if err != nil {
		app.respondMerchantFutureOfferingBillingPeriodError(
			w,
			r,
			fn,
			err,
		)
		return
	}

	response := merchantFutureOfferingBillingPeriodTimelineResponse{
		Periods: newMerchantFutureOfferingBillingPeriodDTOs(periods),
	}

	// The data contract returns at most limit rows rather than limit+1.
	// A full page may have another page, so provide a continuation cursor.
	// If the full page happened to be terminal, following the cursor safely
	// produces an empty final page.
	if len(periods) == limit && len(periods) > 0 {
		last := periods[len(periods)-1]

		response.NextCursor =
			encodeMerchantFutureOfferingBillingPeriodTimelineCursor(
				data.MerchantFutureOfferingBillingPeriodTimelineCursor{
					PeriodStartsOn: last.PeriodStartsOn,
					ID:             last.ID,
				},
			)
	}

	app.auditMerchantFutureOfferingBillingPeriodRead(
		ctx,
		r,
		userID,
		actionListMerchantFutureOfferingBillingPeriods,
		"List Merchant Future Offering Billing Period timeline",
		fmt.Sprintf("future-offering:%s", futureOfferingID),
	)

	app.respondWithJSON(
		w,
		http.StatusOK,
		jsonResponse{
			Error:   false,
			Message: "Billing period timeline retrieved successfully",
			Data:    response,
		},
	)
}

// -----------------------------------------------------------------------------
// Audit helper
// -----------------------------------------------------------------------------

func (app *Application) auditMerchantFutureOfferingBillingPeriodRead(
	ctx context.Context,
	r *http.Request,
	userID *uuid.UUID,
	action string,
	description string,
	entityID string,
) {
	if userID == nil {
		return
	}

	if err := app.insertGovernanceAudit(
		ctx,
		userID,
		action,
		description,
		merchantFutureOfferingBillingPeriodEntityType,
		merchantFutureOfferingBillingPeriodEntityTypeDescription,
		entityID,
	); err != nil {
		app.Logger.
			GetLoggerWithContext(r).
			WithFunctionName(
				"auditMerchantFutureOfferingBillingPeriodRead",
			).
			Warn(
				"merchant future offering billing period read succeeded but audit recording failed",
				"action",
				action,
				"entity_id",
				entityID,
				"error",
				err,
			)
	}
}

// -----------------------------------------------------------------------------
// Domain-date helpers
// -----------------------------------------------------------------------------

func formatMerchantFutureOfferingBillingPeriodDate(
	value time.Time,
) string {
	return value.Format(
		merchantFutureOfferingBillingPeriodDateLayout,
	)
}

// parseMerchantFutureOfferingBillingPeriodDate parses one required Billing
// Period domain date.
//
// The returned value is UTC midnight constructed from the supplied calendar
// fields. The handler performs no Billing Period boundary arithmetic.
func parseMerchantFutureOfferingBillingPeriodDate(
	raw string,
) (time.Time, error) {
	trimmed := strings.TrimSpace(raw)

	if trimmed == "" {
		return time.Time{}, errors.New("date is required")
	}

	parsed, err := time.Parse(
		merchantFutureOfferingBillingPeriodDateLayout,
		trimmed,
	)
	if err != nil {
		return time.Time{}, errors.New(
			"date must be in YYYY-MM-DD format",
		)
	}

	return time.Date(
		parsed.Year(),
		parsed.Month(),
		parsed.Day(),
		0,
		0,
		0,
		0,
		time.UTC,
	), nil
}

// -----------------------------------------------------------------------------
// Timeline cursor codec
// -----------------------------------------------------------------------------

func encodeMerchantFutureOfferingBillingPeriodTimelineCursor(
	cursor data.MerchantFutureOfferingBillingPeriodTimelineCursor,
) string {
	raw := fmt.Sprintf(
		"%s|%s",
		cursor.PeriodStartsOn.Format(
			merchantFutureOfferingBillingPeriodDateLayout,
		),
		cursor.ID.String(),
	)

	return base64.RawURLEncoding.EncodeToString(
		[]byte(raw),
	)
}

func decodeMerchantFutureOfferingBillingPeriodTimelineCursor(
	encoded string,
) (*data.MerchantFutureOfferingBillingPeriodTimelineCursor, error) {
	encoded = strings.TrimSpace(encoded)

	if encoded == "" ||
		len(encoded) >
			merchantFutureOfferingBillingPeriodTimelineCursorMaxEncodedLength {
		return nil, errors.New("malformed cursor")
	}

	rawBytes, err :=
		base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return nil, errors.New("malformed cursor")
	}

	parts := strings.SplitN(
		string(rawBytes),
		"|",
		2,
	)
	if len(parts) != 2 {
		return nil, errors.New("malformed cursor")
	}

	periodStartsOn, err :=
		parseMerchantFutureOfferingBillingPeriodDate(
			parts[0],
		)
	if err != nil {
		return nil, errors.New("malformed cursor")
	}

	id, err := uuid.Parse(parts[1])
	if err != nil || id == uuid.Nil {
		return nil, errors.New("malformed cursor")
	}

	return &data.MerchantFutureOfferingBillingPeriodTimelineCursor{
		PeriodStartsOn: periodStartsOn,
		ID:             id,
	}, nil
}

// -----------------------------------------------------------------------------
// Path helpers
// -----------------------------------------------------------------------------

func parseMerchantFutureOfferingBillingPeriodPathUUID(
	r *http.Request,
	paramName string,
) (uuid.UUID, error) {
	raw := strings.TrimSpace(
		chi.URLParam(
			r,
			paramName,
		),
	)

	if raw == "" {
		return uuid.Nil, fmt.Errorf(
			"%s is required",
			paramName,
		)
	}

	id, err := uuid.Parse(raw)
	if err != nil || id == uuid.Nil {
		return uuid.Nil, fmt.Errorf(
			"invalid %s",
			paramName,
		)
	}

	return id, nil
}

// -----------------------------------------------------------------------------
// Error translation
// -----------------------------------------------------------------------------

// respondMerchantFutureOfferingBillingPeriodError translates Billing Period
// read-path data errors into stable, safe HTTP responses.
//
// Mutation-only Billing Period sentinels are deliberately not exposed here.
// Their appearance at this read-only HTTP boundary is an internal failure.
func (app *Application) respondMerchantFutureOfferingBillingPeriodError(
	w http.ResponseWriter,
	r *http.Request,
	functionName string,
	err error,
) {
	logger := app.Logger.
		GetLoggerWithContext(r).
		WithFunctionName(functionName)

	switch {
	case errors.Is(
		err,
		data.ErrMerchantFutureOfferingBillingPeriodInvalidInput,
	):
		app.respondWithError(
			w,
			errors.New("invalid billing period request"),
			http.StatusBadRequest,
		)

	case errors.Is(
		err,
		data.ErrMerchantFutureOfferingBillingPeriodNotFound,
	),
		errors.Is(
			err,
			data.ErrMerchantFutureOfferingBillingPeriodServiceTermNotFound,
		):
		app.respondWithError(
			w,
			errors.New("billing period not found"),
			http.StatusNotFound,
		)

	default:
		logger.Error(
			"billing period request failed",
			"error",
			err,
		)

		app.respondWithError(
			w,
			errors.New("failed to process billing period request"),
			http.StatusInternalServerError,
		)
	}
}
