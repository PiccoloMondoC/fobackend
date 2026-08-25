// Package main provides HTTP handlers for Merchant Future Offering Service
// Periods.
//
// sdworkspace/sdbackend/internal/server/cmd/api/merchant_future_offering_service_periods.go
//
// GTM:
//
//	Layer: 3.2 API / Future Offering Commercial Domain
//	Release Class: SPINE
//	Reason:
//	  Service Periods are the authoritative bounded performance windows within
//	  an established Future Offering Service Term. This handler exposes
//	  privileged observation and historical retrieval of those windows without
//	  allowing HTTP callers to manufacture Service Period cadence, create
//	  Service Periods, or perform authoritative lifecycle transitions.
//
//	  Service Period creation and Service Term boundary transitions are
//	  service/orchestration responsibilities. Service Period cadence is an
//	  Engineering invariant: each Service Period is one calendar-month
//	  performance window generated according to STCD from the authoritative
//	  Service Term anchor, with a shorter final period where necessary to end
//	  exactly at the authoritative Service Term boundary.
//
// SPINE Rule:
//
//	Keep compiling.
//	Keep production-ready.
//	Preserve permission-first authorization on every route.
//	Preserve Future Offering scope on every read.
//	Preserve date-based Service Period semantics.
//	Preserve bounded deterministic keyset pagination.
//	Keep Service Period creation and Service Term boundary transitions out of
//	the HTTP boundary.
//	Do not couple Service Periods to Billing Periods, Payment Periods,
//	invoices, payments, settlement, or downstream event consumers.
//	Block deployment if authorization, scoping, historical integrity,
//	pagination safety, or domain ownership is weakened.
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

	"github.com/PiccoloMondoC/sdworkspace/sdbackend/internal/data"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// -----------------------------------------------------------------------------
// Entity / action / permission identifiers
// -----------------------------------------------------------------------------

const (
	permissionReadMerchantFutureOfferingServicePeriod = "read_merchant_future_offering_service_period"

	permissionListMerchantFutureOfferingServicePeriods = "list_merchant_future_offering_service_periods"
)

const merchantFutureOfferingServicePeriodDateLayout = "2006-01-02"

const (
	merchantFutureOfferingServicePeriodTimelineDefaultLimit = 20
	merchantFutureOfferingServicePeriodTimelineMaxLimit     = 100

	// The encoded cursor contains only a YYYY-MM-DD domain date, separator,
	// and UUID. This ceiling is deliberately larger than a valid cursor while
	// preventing unbounded attacker-controlled decoding allocation.
	merchantFutureOfferingServicePeriodTimelineCursorMaxEncodedLength = 256
)

// -----------------------------------------------------------------------------
// Response DTOs
// -----------------------------------------------------------------------------

// merchantFutureOfferingServicePeriodDTO is the stable HTTP representation of
// one Future Offering Service Period.
//
// PostgreSQL DATE values are deliberately exposed as YYYY-MM-DD strings.
// Time-of-day, elapsed-hour arithmetic, and server-local timezone are not
// Service Period domain facts.
type merchantFutureOfferingServicePeriodDTO struct {
	ID               uuid.UUID `json:"id"`
	ServiceTermID    uuid.UUID `json:"service_term_id"`
	FutureOfferingID uuid.UUID `json:"future_offering_id"`
	PeriodNumber     int       `json:"period_number"`
	PeriodStartsOn   string    `json:"period_starts_on"`
	PeriodEndsOn     string    `json:"period_ends_on"`
	CreatedAt        time.Time `json:"created_at"`
}

func newMerchantFutureOfferingServicePeriodDTO(
	period *data.MerchantFutureOfferingServicePeriod,
) merchantFutureOfferingServicePeriodDTO {
	return merchantFutureOfferingServicePeriodDTO{
		ID:               period.ID,
		ServiceTermID:    period.ServiceTermID,
		FutureOfferingID: period.FutureOfferingID,
		PeriodNumber:     period.PeriodNumber,
		PeriodStartsOn:   formatMerchantFutureOfferingServicePeriodDate(period.PeriodStartsOn),
		PeriodEndsOn:     formatMerchantFutureOfferingServicePeriodDate(period.PeriodEndsOn),
		CreatedAt:        period.CreatedAt,
	}
}

func newMerchantFutureOfferingServicePeriodDTOs(
	periods []*data.MerchantFutureOfferingServicePeriod,
) []merchantFutureOfferingServicePeriodDTO {
	dtos := make(
		[]merchantFutureOfferingServicePeriodDTO,
		0,
		len(periods),
	)

	for _, period := range periods {
		dtos = append(
			dtos,
			newMerchantFutureOfferingServicePeriodDTO(period),
		)
	}

	return dtos
}

// merchantFutureOfferingServicePeriodTimelineResponse presents a bounded
// historical page and, where another page may exist, an opaque continuation
// cursor.
type merchantFutureOfferingServicePeriodTimelineResponse struct {
	Periods    []merchantFutureOfferingServicePeriodDTO `json:"periods"`
	NextCursor string                                   `json:"next_cursor,omitempty"`
}

// -----------------------------------------------------------------------------
// Read by ID
// -----------------------------------------------------------------------------

// GetMerchantFutureOfferingServicePeriodHandler retrieves one Service Period
// strictly within the Future Offering identified by the route.
//
// GetByID is intentionally followed by scope verification because the data
// capability's canonical identity lookup is global to Service Period identity.
// A mismatched Future Offering is presented as not found rather than exposing
// cross-scope existence.
func (app *Application) GetMerchantFutureOfferingServicePeriodHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	const fn = "GetMerchantFutureOfferingServicePeriodHandler"

	ctx, cancel := context.WithTimeout(
		r.Context(),
		cfgTimeout,
	)
	defer cancel()

	if !app.HasPermission(
		ctx,
		permissionReadMerchantFutureOfferingServicePeriod,
	) {
		app.respondWithError(
			w,
			errors.New("forbidden: insufficient permissions"),
			http.StatusForbidden,
		)
		return
	}

	futureOfferingID, err :=
		parseMerchantFutureOfferingServicePeriodPathUUID(
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

	servicePeriodID, err :=
		parseMerchantFutureOfferingServicePeriodPathUUID(
			r,
			"servicePeriodID",
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
		app.Models.MerchantFutureOfferingServicePeriod.GetByID(
			ctx,
			servicePeriodID,
		)
	if err != nil {
		app.respondMerchantFutureOfferingServicePeriodError(
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
			errors.New("service period not found"),
			http.StatusNotFound,
		)
		return
	}

	app.respondWithJSON(
		w,
		http.StatusOK,
		jsonResponse{
			Error:   false,
			Message: "Service period retrieved successfully",
			Data:    newMerchantFutureOfferingServicePeriodDTO(period),
		},
	)
}

// -----------------------------------------------------------------------------
// Current at domain date
// -----------------------------------------------------------------------------

// GetCurrentMerchantFutureOfferingServicePeriodAtDateHandler retrieves the
// authoritative current Service Period covering the supplied domain date.
//
// The date is interpreted solely as YYYY-MM-DD. The handler performs no
// calendar arithmetic and does not reinterpret the date through server-local
// timezone.
func (app *Application) GetCurrentMerchantFutureOfferingServicePeriodAtDateHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	const fn = "GetCurrentMerchantFutureOfferingServicePeriodAtDateHandler"

	ctx, cancel := context.WithTimeout(
		r.Context(),
		cfgTimeout,
	)
	defer cancel()

	if !app.HasPermission(
		ctx,
		permissionReadMerchantFutureOfferingServicePeriod,
	) {
		app.respondWithError(
			w,
			errors.New("forbidden: insufficient permissions"),
			http.StatusForbidden,
		)
		return
	}

	futureOfferingID, err :=
		parseMerchantFutureOfferingServicePeriodPathUUID(
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
		parseMerchantFutureOfferingServicePeriodDate(
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
		app.Models.MerchantFutureOfferingServicePeriod.GetCurrentAt(
			ctx,
			futureOfferingID,
			on,
		)
	if err != nil {
		app.respondMerchantFutureOfferingServicePeriodError(
			w,
			r,
			fn,
			err,
		)
		return
	}

	app.respondWithJSON(
		w,
		http.StatusOK,
		jsonResponse{
			Error:   false,
			Message: "Current service period retrieved successfully",
			Data:    newMerchantFutureOfferingServicePeriodDTO(period),
		},
	)
}

// -----------------------------------------------------------------------------
// Timeline
// -----------------------------------------------------------------------------

// ListMerchantFutureOfferingServicePeriodTimelineHandler returns bounded
// immutable Service Period history across Service Term revisions using the
// data layer's (period_starts_on, id) keyset.
//
// The data capability orders this timeline oldest-first.
func (app *Application) ListMerchantFutureOfferingServicePeriodTimelineHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	const fn = "ListMerchantFutureOfferingServicePeriodTimelineHandler"

	ctx, cancel := context.WithTimeout(
		r.Context(),
		cfgTimeout,
	)
	defer cancel()

	if !app.HasPermission(
		ctx,
		permissionListMerchantFutureOfferingServicePeriods,
	) {
		app.respondWithError(
			w,
			errors.New("forbidden: insufficient permissions"),
			http.StatusForbidden,
		)
		return
	}

	futureOfferingID, err :=
		parseMerchantFutureOfferingServicePeriodPathUUID(
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

	limit := merchantFutureOfferingServicePeriodTimelineDefaultLimit

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

		if parsed > merchantFutureOfferingServicePeriodTimelineMaxLimit {
			parsed = merchantFutureOfferingServicePeriodTimelineMaxLimit
		}

		limit = parsed
	}

	var cursor *data.MerchantFutureOfferingServicePeriodTimelineCursor

	if raw := strings.TrimSpace(
		r.URL.Query().Get("cursor"),
	); raw != "" {
		cursor, err =
			decodeMerchantFutureOfferingServicePeriodTimelineCursor(
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
		app.Models.MerchantFutureOfferingServicePeriod.
			ListTimelineForFutureOffering(
				ctx,
				futureOfferingID,
				limit,
				cursor,
			)
	if err != nil {
		app.respondMerchantFutureOfferingServicePeriodError(
			w,
			r,
			fn,
			err,
		)
		return
	}

	response := merchantFutureOfferingServicePeriodTimelineResponse{
		Periods: newMerchantFutureOfferingServicePeriodDTOs(periods),
	}

	// The data contract returns at most limit rows rather than limit+1.
	// Returning a continuation cursor whenever the page is full safely permits
	// another request; the eventual terminal request may legitimately return an
	// empty page when the preceding page contained exactly the final limit rows.
	if len(periods) == limit && len(periods) > 0 {
		last := periods[len(periods)-1]

		response.NextCursor =
			encodeMerchantFutureOfferingServicePeriodTimelineCursor(
				data.MerchantFutureOfferingServicePeriodTimelineCursor{
					PeriodStartsOn: last.PeriodStartsOn,
					ID:             last.ID,
				},
			)
	}

	app.respondWithJSON(
		w,
		http.StatusOK,
		jsonResponse{
			Error:   false,
			Message: "Service period timeline retrieved successfully",
			Data:    response,
		},
	)
}

// -----------------------------------------------------------------------------
// Domain-date helpers
// -----------------------------------------------------------------------------

func formatMerchantFutureOfferingServicePeriodDate(
	value time.Time,
) string {
	return value.Format(
		merchantFutureOfferingServicePeriodDateLayout,
	)
}

// parseMerchantFutureOfferingServicePeriodDate parses one required Service
// Period domain date.
//
// It deliberately constructs UTC midnight from the supplied calendar fields
// rather than treating the value as an elapsed-time instant.
func parseMerchantFutureOfferingServicePeriodDate(
	raw string,
) (time.Time, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return time.Time{}, errors.New("date is required")
	}

	parsed, err := time.Parse(
		merchantFutureOfferingServicePeriodDateLayout,
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

// encodeMerchantFutureOfferingServicePeriodTimelineCursor converts the
// internal (period_starts_on, id) keyset position into an opaque HTTP cursor.
func encodeMerchantFutureOfferingServicePeriodTimelineCursor(
	cursor data.MerchantFutureOfferingServicePeriodTimelineCursor,
) string {
	raw := fmt.Sprintf(
		"%s|%s",
		cursor.PeriodStartsOn.Format(
			merchantFutureOfferingServicePeriodDateLayout,
		),
		cursor.ID.String(),
	)

	return base64.RawURLEncoding.EncodeToString(
		[]byte(raw),
	)
}

func decodeMerchantFutureOfferingServicePeriodTimelineCursor(
	encoded string,
) (*data.MerchantFutureOfferingServicePeriodTimelineCursor, error) {
	encoded = strings.TrimSpace(encoded)

	if encoded == "" ||
		len(encoded) >
			merchantFutureOfferingServicePeriodTimelineCursorMaxEncodedLength {
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
		parseMerchantFutureOfferingServicePeriodDate(
			parts[0],
		)
	if err != nil {
		return nil, errors.New("malformed cursor")
	}

	id, err := uuid.Parse(parts[1])
	if err != nil || id == uuid.Nil {
		return nil, errors.New("malformed cursor")
	}

	return &data.MerchantFutureOfferingServicePeriodTimelineCursor{
		PeriodStartsOn: periodStartsOn,
		ID:             id,
	}, nil
}

// -----------------------------------------------------------------------------
// Path helpers
// -----------------------------------------------------------------------------

func parseMerchantFutureOfferingServicePeriodPathUUID(
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

// respondMerchantFutureOfferingServicePeriodError translates Service Period
// data-layer failures into safe HTTP responses.
//
// This handler is intentionally read-only. Mutation-only lifecycle/write
// sentinels reaching this boundary therefore represent unexpected internal
// behavior and are not translated into public mutation semantics.
func (app *Application) respondMerchantFutureOfferingServicePeriodError(
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
		data.ErrMerchantFutureOfferingServicePeriodInvalidInput,
	):
		app.respondWithError(
			w,
			errors.New("invalid service period request"),
			http.StatusBadRequest,
		)

	case errors.Is(
		err,
		data.ErrMerchantFutureOfferingServicePeriodNotFound,
	),
		errors.Is(
			err,
			data.ErrMerchantFutureOfferingServicePeriodServiceTermNotFound,
		):
		app.respondWithError(
			w,
			errors.New("service period not found"),
			http.StatusNotFound,
		)

	default:
		logger.Error(
			"service period request failed",
			"error",
			err,
		)

		app.respondWithError(
			w,
			errors.New("failed to process service period request"),
			http.StatusInternalServerError,
		)
	}
}
