// Package main provides HTTP handlers for Merchant Future Offering Service
// Terms.
//
// sdworkspace/sdbackend/internal/server/cmd/api/merchant_future_offering_service_terms.go
//
// GTM:
//
//	Layer: 3.2 API / Future Offering Commercial Domain
//	Release Class: SPINE
//	Reason:
//	  Service Terms preserve the authoritative overall duration of Sagrenti
//	  service for individual Future Offerings. This handler exposes the
//	  Service Term lifecycle as an HTTP boundary without acquiring knowledge
//	  of Service Period, Billing Period, Payment Period, invoicing, fee
//	  calculation, payment, or downstream event consumers.
//
// SPINE Rule:
//
//	Keep compiling.
//	Keep production-ready.
//	Preserve permission-first authorization on every route.
//	Preserve Future-Offering scoping on every read and mutation.
//	Preserve server ownership of lifecycle fields.
//	Preserve atomic replacement through the data-layer lifecycle operation.
//	Preserve bounded deterministic keyset pagination.
//	Do not couple this handler to downstream commercial domains.
//	Do not publish domain events from the HTTP boundary.
//	Block deployment if authorization, lifecycle protection, scoping,
//	pagination safety, or build integrity is weakened.
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
	merchantFutureOfferingServiceTermEntityType = "merchant_future_offering_service_term"

	merchantFutureOfferingServiceTermEntityTypeDescription = "Future Offering Service Term overall-duration commercial lifecycle entity"

	actionProposeMerchantFutureOfferingServiceTerm = "propose_merchant_future_offering_service_term"

	actionReadMerchantFutureOfferingServiceTerm = "read_merchant_future_offering_service_term"

	actionListMerchantFutureOfferingServiceTerms = "list_merchant_future_offering_service_terms"

	actionUpdateMerchantFutureOfferingServiceTermProposal = "update_merchant_future_offering_service_term_proposal"

	actionEstablishMerchantFutureOfferingServiceTerm = "establish_merchant_future_offering_service_term"

	actionReplaceMerchantFutureOfferingServiceTerm = "replace_merchant_future_offering_service_term"

	permissionProposeMerchantFutureOfferingServiceTerm = "propose_merchant_future_offering_service_term"

	permissionReadMerchantFutureOfferingServiceTerm = "read_merchant_future_offering_service_term"

	permissionListMerchantFutureOfferingServiceTerms = "list_merchant_future_offering_service_terms"

	permissionUpdateMerchantFutureOfferingServiceTermProposal = "update_merchant_future_offering_service_term_proposal"

	permissionEstablishMerchantFutureOfferingServiceTerm = "establish_merchant_future_offering_service_term"

	permissionReplaceMerchantFutureOfferingServiceTerm = "replace_merchant_future_offering_service_term"
)

const merchantFutureOfferingServiceTermDateLayout = "2006-01-02"

const (
	merchantFutureOfferingServiceTermTimelineDefaultLimit = 20
	merchantFutureOfferingServiceTermTimelineMaxLimit     = 100

	// The encoded cursor contains only an RFC3339Nano timestamp, separator,
	// and UUID. The ceiling is deliberately much larger than a valid cursor
	// while preventing unbounded attacker-controlled decoding allocation.
	merchantFutureOfferingServiceTermTimelineCursorMaxEncodedLength = 256
)

// -----------------------------------------------------------------------------
// Response DTOs
// -----------------------------------------------------------------------------

// merchantFutureOfferingServiceTermDTO is the stable HTTP representation of a
// Service Term. PostgreSQL DATE values are deliberately represented as
// YYYY-MM-DD strings because time-of-day and timezone are not domain facts.
type merchantFutureOfferingServiceTermDTO struct {
	ID                      uuid.UUID  `json:"id"`
	FutureOfferingID        uuid.UUID  `json:"future_offering_id"`
	DurationMonths          int        `json:"duration_months"`
	TermStartsOn            *string    `json:"term_starts_on"`
	TermEndsOn              *string    `json:"term_ends_on"`
	TermStatus              string     `json:"term_status"`
	SupersedesServiceTermID *uuid.UUID `json:"supersedes_service_term_id,omitempty"`
	EstablishedAt           *time.Time `json:"established_at,omitempty"`
	SupersededAt            *time.Time `json:"superseded_at,omitempty"`
	CreatedAt               time.Time  `json:"created_at"`
	UpdatedAt               time.Time  `json:"updated_at"`
}

func newMerchantFutureOfferingServiceTermDTO(
	term *data.MerchantFutureOfferingServiceTerm,
) merchantFutureOfferingServiceTermDTO {
	return merchantFutureOfferingServiceTermDTO{
		ID:                      term.ID,
		FutureOfferingID:        term.FutureOfferingID,
		DurationMonths:          term.DurationMonths,
		TermStartsOn:            formatMerchantFutureOfferingServiceTermDate(term.TermStartsOn),
		TermEndsOn:              formatMerchantFutureOfferingServiceTermDate(term.TermEndsOn),
		TermStatus:              string(term.TermStatus),
		SupersedesServiceTermID: term.SupersedesServiceTermID,
		EstablishedAt:           term.EstablishedAt,
		SupersededAt:            term.SupersededAt,
		CreatedAt:               term.CreatedAt,
		UpdatedAt:               term.UpdatedAt,
	}
}

// -----------------------------------------------------------------------------
// Request DTOs
// -----------------------------------------------------------------------------

// proposeMerchantFutureOfferingServiceTermRequest contains only facts that a
// caller may supply while proposing a Service Term.
//
// Lifecycle identity and transition fields are intentionally absent.
type proposeMerchantFutureOfferingServiceTermRequest struct {
	DurationMonths int     `json:"duration_months"`
	TermStartsOn   *string `json:"term_starts_on"`
	TermEndsOn     *string `json:"term_ends_on"`
}

// updateProposedMerchantFutureOfferingServiceTermRequest represents the
// complete mutable material state of a proposal.
//
// This DTO therefore has PUT semantics, not PATCH semantics: omitted values
// are not interpreted as "leave unchanged".
type updateProposedMerchantFutureOfferingServiceTermRequest struct {
	DurationMonths int     `json:"duration_months"`
	TermStartsOn   *string `json:"term_starts_on"`
	TermEndsOn     *string `json:"term_ends_on"`
}

// establishMerchantFutureOfferingServiceTermRequest contains the authoritative
// service window used when establishing a proposal.
//
// Establishment dates are explicit. They are not inferred from draft proposal
// dates by the HTTP layer.
type establishMerchantFutureOfferingServiceTermRequest struct {
	TermStartsOn string `json:"term_starts_on"`
	TermEndsOn   string `json:"term_ends_on"`
}

// replaceMerchantFutureOfferingServiceTermRequest identifies the already
// proposed replacement and supplies its authoritative establishment window.
//
// The established predecessor is identified by the serviceTermID path
// parameter.
type replaceMerchantFutureOfferingServiceTermRequest struct {
	ReplacementServiceTermID uuid.UUID `json:"replacement_service_term_id"`
	TermStartsOn             string    `json:"term_starts_on"`
	TermEndsOn               string    `json:"term_ends_on"`
}

type merchantFutureOfferingServiceTermTimelineResponse struct {
	ServiceTerms []merchantFutureOfferingServiceTermDTO `json:"service_terms"`
	NextCursor   *string                                `json:"next_cursor,omitempty"`
}

type merchantFutureOfferingServiceTermReplacementResponse struct {
	Predecessor merchantFutureOfferingServiceTermDTO `json:"predecessor"`
	Replacement merchantFutureOfferingServiceTermDTO `json:"replacement"`
}

// -----------------------------------------------------------------------------
// Date boundary helpers
// -----------------------------------------------------------------------------

func formatMerchantFutureOfferingServiceTermDate(value *time.Time) *string {
	if value == nil {
		return nil
	}

	formatted := value.Format(merchantFutureOfferingServiceTermDateLayout)
	return &formatted
}

func parseOptionalMerchantFutureOfferingServiceTermDate(
	raw *string,
) (*time.Time, error) {
	if raw == nil {
		return nil, nil
	}

	trimmed := strings.TrimSpace(*raw)
	if trimmed == "" {
		return nil, errors.New("date must not be empty when provided")
	}

	parsed, err := time.Parse(
		merchantFutureOfferingServiceTermDateLayout,
		trimmed,
	)
	if err != nil {
		return nil, errors.New("date must be in YYYY-MM-DD format")
	}

	normalized := time.Date(
		parsed.Year(),
		parsed.Month(),
		parsed.Day(),
		0,
		0,
		0,
		0,
		time.UTC,
	)

	return &normalized, nil
}

func parseRequiredMerchantFutureOfferingServiceTermDate(
	raw string,
) (time.Time, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return time.Time{}, errors.New("date is required")
	}

	parsed, err := time.Parse(
		merchantFutureOfferingServiceTermDateLayout,
		trimmed,
	)
	if err != nil {
		return time.Time{}, errors.New("date must be in YYYY-MM-DD format")
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

// encodeMerchantFutureOfferingServiceTermTimelineCursor converts the internal
// (created_at, id) keyset position into an opaque HTTP cursor.
func encodeMerchantFutureOfferingServiceTermTimelineCursor(
	cursor data.MerchantFutureOfferingServiceTermTimelineCursor,
) string {
	raw := fmt.Sprintf(
		"%s|%s",
		cursor.CreatedAt.UTC().Format(time.RFC3339Nano),
		cursor.ID.String(),
	)

	return base64.RawURLEncoding.EncodeToString([]byte(raw))
}

func decodeMerchantFutureOfferingServiceTermTimelineCursor(
	encoded string,
) (*data.MerchantFutureOfferingServiceTermTimelineCursor, error) {
	encoded = strings.TrimSpace(encoded)

	if encoded == "" ||
		len(encoded) > merchantFutureOfferingServiceTermTimelineCursorMaxEncodedLength {
		return nil, errors.New("malformed cursor")
	}

	rawBytes, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return nil, errors.New("malformed cursor")
	}

	parts := strings.SplitN(string(rawBytes), "|", 2)
	if len(parts) != 2 {
		return nil, errors.New("malformed cursor")
	}

	createdAt, err := time.Parse(time.RFC3339Nano, parts[0])
	if err != nil {
		return nil, errors.New("malformed cursor")
	}

	id, err := uuid.Parse(parts[1])
	if err != nil || id == uuid.Nil {
		return nil, errors.New("malformed cursor")
	}

	return &data.MerchantFutureOfferingServiceTermTimelineCursor{
		CreatedAt: createdAt.UTC(),
		ID:        id,
	}, nil
}

// -----------------------------------------------------------------------------
// Path helpers
// -----------------------------------------------------------------------------

func parseMerchantFutureOfferingServiceTermPathUUID(
	r *http.Request,
	paramName string,
) (uuid.UUID, error) {
	raw := strings.TrimSpace(chi.URLParam(r, paramName))

	id, err := uuid.Parse(raw)
	if err != nil || id == uuid.Nil {
		return uuid.Nil, fmt.Errorf("invalid %s", paramName)
	}

	return id, nil
}

// -----------------------------------------------------------------------------
// Error translation
// -----------------------------------------------------------------------------

// respondMerchantFutureOfferingServiceTermError is the sole translation
// boundary between Service Term persistence errors and HTTP responses.
//
// Expected domain/persistence sentinels receive deterministic statuses.
// Unexpected implementation details are logged server-side and never exposed.
func (app *Application) respondMerchantFutureOfferingServiceTermError(
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
		data.ErrMerchantFutureOfferingServiceTermInvalidInput,
	):
		app.respondWithError(
			w,
			errors.New("invalid service term input"),
			http.StatusBadRequest,
		)

	case errors.Is(
		err,
		data.ErrMerchantFutureOfferingServiceTermFutureOfferingNotFound,
	):
		app.respondWithError(
			w,
			errors.New("future offering not found"),
			http.StatusNotFound,
		)

	case errors.Is(
		err,
		data.ErrMerchantFutureOfferingServiceTermNotFound,
	):
		app.respondWithError(
			w,
			errors.New("service term not found"),
			http.StatusNotFound,
		)

	case errors.Is(
		err,
		data.ErrMerchantFutureOfferingServiceTermProposedAlreadyExists,
	):
		app.respondWithError(
			w,
			errors.New(
				"a proposed service term already exists for this future offering",
			),
			http.StatusConflict,
		)

	case errors.Is(
		err,
		data.ErrMerchantFutureOfferingServiceTermEstablishedAlreadyExists,
	):
		app.respondWithError(
			w,
			errors.New(
				"an established service term already exists for this future offering",
			),
			http.StatusConflict,
		)

	case errors.Is(
		err,
		data.ErrMerchantFutureOfferingServiceTermInvalidTransition,
	):
		app.respondWithError(
			w,
			errors.New(
				"the requested service term transition is not permitted from its current state",
			),
			http.StatusConflict,
		)

	case errors.Is(
		err,
		data.ErrMerchantFutureOfferingServiceTermReplacementConflict,
	):
		app.respondWithError(
			w,
			errors.New(
				"the replacement service term conflicts with an existing replacement relationship",
			),
			http.StatusConflict,
		)

	case errors.Is(
		err,
		data.ErrMerchantFutureOfferingServiceTermInvalidState,
	):
		app.respondWithError(
			w,
			errors.New(
				"the request conflicts with the service term's current state",
			),
			http.StatusConflict,
		)

	default:
		logger.Error(
			"unexpected service term persistence failure",
			"error",
			err,
		)

		app.respondWithError(
			w,
			errors.New("failed to process service term request"),
			http.StatusInternalServerError,
		)
	}
}

// -----------------------------------------------------------------------------
// Proposal
// -----------------------------------------------------------------------------

// ProposeMerchantFutureOfferingServiceTermHandler creates a proposed Service
// Term for a Future Offering.
//
// It does not establish the term and does not permit callers to supply
// lifecycle state.
func (app *Application) ProposeMerchantFutureOfferingServiceTermHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	const fn = "ProposeMerchantFutureOfferingServiceTermHandler"

	logger := app.Logger.
		GetLoggerWithContext(r).
		WithFunctionName(fn)

	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	if !app.HasPermission(
		ctx,
		permissionProposeMerchantFutureOfferingServiceTerm,
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
		parseMerchantFutureOfferingServiceTermPathUUID(
			r,
			"futureOfferingID",
		)
	if err != nil {
		app.respondWithError(w, err, http.StatusBadRequest)
		return
	}

	var req proposeMerchantFutureOfferingServiceTermRequest
	if err := app.readJSON(w, r, &req); err != nil {
		app.respondWithError(
			w,
			fmt.Errorf("invalid request body: %w", err),
			http.StatusBadRequest,
		)
		return
	}

	termStartsOn, err :=
		parseOptionalMerchantFutureOfferingServiceTermDate(
			req.TermStartsOn,
		)
	if err != nil {
		app.respondWithError(
			w,
			fmt.Errorf("term_starts_on: %w", err),
			http.StatusBadRequest,
		)
		return
	}

	termEndsOn, err :=
		parseOptionalMerchantFutureOfferingServiceTermDate(
			req.TermEndsOn,
		)
	if err != nil {
		app.respondWithError(
			w,
			fmt.Errorf("term_ends_on: %w", err),
			http.StatusBadRequest,
		)
		return
	}

	term, err :=
		app.Models.MerchantFutureOfferingServiceTerm.Propose(
			ctx,
			futureOfferingID,
			data.MerchantFutureOfferingServiceTermProposal{
				DurationMonths: req.DurationMonths,
				TermStartsOn:   termStartsOn,
				TermEndsOn:     termEndsOn,
			},
		)
	if err != nil {
		app.respondMerchantFutureOfferingServiceTermError(
			w,
			r,
			fn,
			err,
		)
		return
	}

	if err := app.insertGovernanceAudit(
		ctx,
		userID,
		actionProposeMerchantFutureOfferingServiceTerm,
		"Proposed a Future Offering Service Term",
		merchantFutureOfferingServiceTermEntityType,
		merchantFutureOfferingServiceTermEntityTypeDescription,
		term.ID.String(),
	); err != nil {
		logger.Warn(
			"service term proposed but audit recording failed",
			"service_term_id",
			term.ID,
			"future_offering_id",
			futureOfferingID,
			"error",
			err,
		)
	}

	logger.Info(
		"service term proposed",
		"service_term_id",
		term.ID,
		"future_offering_id",
		futureOfferingID,
	)

	app.respondWithJSON(
		w,
		http.StatusCreated,
		jsonResponse{
			Error:   false,
			Message: "Service term proposed successfully",
			Data:    newMerchantFutureOfferingServiceTermDTO(term),
		},
	)
}

// -----------------------------------------------------------------------------
// Read by ID
// -----------------------------------------------------------------------------

// GetMerchantFutureOfferingServiceTermHandler retrieves one Service Term
// strictly within its Future Offering scope.
func (app *Application) GetMerchantFutureOfferingServiceTermHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	const fn = "GetMerchantFutureOfferingServiceTermHandler"

	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	if !app.HasPermission(
		ctx,
		permissionReadMerchantFutureOfferingServiceTerm,
	) {
		app.respondWithError(
			w,
			errors.New("forbidden: insufficient permissions"),
			http.StatusForbidden,
		)
		return
	}

	futureOfferingID, err :=
		parseMerchantFutureOfferingServiceTermPathUUID(
			r,
			"futureOfferingID",
		)
	if err != nil {
		app.respondWithError(w, err, http.StatusBadRequest)
		return
	}

	serviceTermID, err :=
		parseMerchantFutureOfferingServiceTermPathUUID(
			r,
			"serviceTermID",
		)
	if err != nil {
		app.respondWithError(w, err, http.StatusBadRequest)
		return
	}

	term, err :=
		app.Models.MerchantFutureOfferingServiceTerm.
			GetByIDForFutureOffering(
				ctx,
				serviceTermID,
				futureOfferingID,
			)
	if err != nil {
		app.respondMerchantFutureOfferingServiceTermError(
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
			Message: "Service term retrieved successfully",
			Data:    newMerchantFutureOfferingServiceTermDTO(term),
		},
	)
}

// -----------------------------------------------------------------------------
// Current proposed
// -----------------------------------------------------------------------------

func (app *Application) GetCurrentProposedMerchantFutureOfferingServiceTermHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	const fn = "GetCurrentProposedMerchantFutureOfferingServiceTermHandler"

	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	if !app.HasPermission(
		ctx,
		permissionReadMerchantFutureOfferingServiceTerm,
	) {
		app.respondWithError(
			w,
			errors.New("forbidden: insufficient permissions"),
			http.StatusForbidden,
		)
		return
	}

	futureOfferingID, err :=
		parseMerchantFutureOfferingServiceTermPathUUID(
			r,
			"futureOfferingID",
		)
	if err != nil {
		app.respondWithError(w, err, http.StatusBadRequest)
		return
	}

	term, err :=
		app.Models.MerchantFutureOfferingServiceTerm.
			GetCurrentProposed(
				ctx,
				futureOfferingID,
			)
	if err != nil {
		app.respondMerchantFutureOfferingServiceTermError(
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
			Message: "Current proposed service term retrieved successfully",
			Data:    newMerchantFutureOfferingServiceTermDTO(term),
		},
	)
}

// -----------------------------------------------------------------------------
// Current established
// -----------------------------------------------------------------------------

func (app *Application) GetCurrentEstablishedMerchantFutureOfferingServiceTermHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	const fn = "GetCurrentEstablishedMerchantFutureOfferingServiceTermHandler"

	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	if !app.HasPermission(
		ctx,
		permissionReadMerchantFutureOfferingServiceTerm,
	) {
		app.respondWithError(
			w,
			errors.New("forbidden: insufficient permissions"),
			http.StatusForbidden,
		)
		return
	}

	futureOfferingID, err :=
		parseMerchantFutureOfferingServiceTermPathUUID(
			r,
			"futureOfferingID",
		)
	if err != nil {
		app.respondWithError(w, err, http.StatusBadRequest)
		return
	}

	term, err :=
		app.Models.MerchantFutureOfferingServiceTerm.
			GetCurrentEstablished(
				ctx,
				futureOfferingID,
			)
	if err != nil {
		app.respondMerchantFutureOfferingServiceTermError(
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
			Message: "Current established service term retrieved successfully",
			Data:    newMerchantFutureOfferingServiceTermDTO(term),
		},
	)
}

// -----------------------------------------------------------------------------
// Timeline
// -----------------------------------------------------------------------------

// ListMerchantFutureOfferingServiceTermTimelineHandler returns bounded,
// newest-first Service Term history using the data layer's deterministic
// (created_at, id) keyset.
func (app *Application) ListMerchantFutureOfferingServiceTermTimelineHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	const fn = "ListMerchantFutureOfferingServiceTermTimelineHandler"

	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	if !app.HasPermission(
		ctx,
		permissionListMerchantFutureOfferingServiceTerms,
	) {
		app.respondWithError(
			w,
			errors.New("forbidden: insufficient permissions"),
			http.StatusForbidden,
		)
		return
	}

	futureOfferingID, err :=
		parseMerchantFutureOfferingServiceTermPathUUID(
			r,
			"futureOfferingID",
		)
	if err != nil {
		app.respondWithError(w, err, http.StatusBadRequest)
		return
	}

	limit := merchantFutureOfferingServiceTermTimelineDefaultLimit

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

		if parsed > merchantFutureOfferingServiceTermTimelineMaxLimit {
			parsed = merchantFutureOfferingServiceTermTimelineMaxLimit
		}

		limit = parsed
	}

	var cursor *data.MerchantFutureOfferingServiceTermTimelineCursor

	if raw := strings.TrimSpace(
		r.URL.Query().Get("cursor"),
	); raw != "" {
		cursor, err =
			decodeMerchantFutureOfferingServiceTermTimelineCursor(raw)
		if err != nil {
			app.respondWithError(
				w,
				err,
				http.StatusBadRequest,
			)
			return
		}
	}

	terms, err :=
		app.Models.MerchantFutureOfferingServiceTerm.ListTimeline(
			ctx,
			futureOfferingID,
			limit,
			cursor,
		)
	if err != nil {
		app.respondMerchantFutureOfferingServiceTermError(
			w,
			r,
			fn,
			err,
		)
		return
	}

	dtos := make(
		[]merchantFutureOfferingServiceTermDTO,
		0,
		len(terms),
	)

	for _, term := range terms {
		dtos = append(
			dtos,
			newMerchantFutureOfferingServiceTermDTO(term),
		)
	}

	response := merchantFutureOfferingServiceTermTimelineResponse{
		ServiceTerms: dtos,
	}

	// A full page means another keyset position may exist. No count query is
	// required; following a cursor that ultimately yields no rows is safe.
	if len(terms) == limit {
		last := terms[len(terms)-1]

		nextCursor :=
			encodeMerchantFutureOfferingServiceTermTimelineCursor(
				data.MerchantFutureOfferingServiceTermTimelineCursor{
					CreatedAt: last.CreatedAt,
					ID:        last.ID,
				},
			)

		response.NextCursor = &nextCursor
	}

	app.respondWithJSON(
		w,
		http.StatusOK,
		jsonResponse{
			Error:   false,
			Message: "Service term timeline retrieved successfully",
			Data:    response,
		},
	)
}

// -----------------------------------------------------------------------------
// Replace proposed facts
// -----------------------------------------------------------------------------

// UpdateProposedMerchantFutureOfferingServiceTermHandler replaces the complete
// mutable material state of a proposed Service Term.
//
// This operation intentionally has PUT semantics. It is not a partial patch.
// Established and superseded commercial history cannot be modified through
// this handler.
func (app *Application) UpdateProposedMerchantFutureOfferingServiceTermHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	const fn = "UpdateProposedMerchantFutureOfferingServiceTermHandler"

	logger := app.Logger.
		GetLoggerWithContext(r).
		WithFunctionName(fn)

	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	if !app.HasPermission(
		ctx,
		permissionUpdateMerchantFutureOfferingServiceTermProposal,
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
		parseMerchantFutureOfferingServiceTermPathUUID(
			r,
			"futureOfferingID",
		)
	if err != nil {
		app.respondWithError(w, err, http.StatusBadRequest)
		return
	}

	serviceTermID, err :=
		parseMerchantFutureOfferingServiceTermPathUUID(
			r,
			"serviceTermID",
		)
	if err != nil {
		app.respondWithError(w, err, http.StatusBadRequest)
		return
	}

	var req updateProposedMerchantFutureOfferingServiceTermRequest

	if err := app.readJSON(w, r, &req); err != nil {
		app.respondWithError(
			w,
			fmt.Errorf("invalid request body: %w", err),
			http.StatusBadRequest,
		)
		return
	}

	termStartsOn, err :=
		parseOptionalMerchantFutureOfferingServiceTermDate(
			req.TermStartsOn,
		)
	if err != nil {
		app.respondWithError(
			w,
			fmt.Errorf("term_starts_on: %w", err),
			http.StatusBadRequest,
		)
		return
	}

	termEndsOn, err :=
		parseOptionalMerchantFutureOfferingServiceTermDate(
			req.TermEndsOn,
		)
	if err != nil {
		app.respondWithError(
			w,
			fmt.Errorf("term_ends_on: %w", err),
			http.StatusBadRequest,
		)
		return
	}

	term, err :=
		app.Models.MerchantFutureOfferingServiceTerm.UpdateProposed(
			ctx,
			serviceTermID,
			futureOfferingID,
			data.MerchantFutureOfferingServiceTermProposal{
				DurationMonths: req.DurationMonths,
				TermStartsOn:   termStartsOn,
				TermEndsOn:     termEndsOn,
			},
		)
	if err != nil {
		app.respondMerchantFutureOfferingServiceTermError(
			w,
			r,
			fn,
			err,
		)
		return
	}

	if err := app.insertGovernanceAudit(
		ctx,
		userID,
		actionUpdateMerchantFutureOfferingServiceTermProposal,
		"Updated a proposed Future Offering Service Term",
		merchantFutureOfferingServiceTermEntityType,
		merchantFutureOfferingServiceTermEntityTypeDescription,
		term.ID.String(),
	); err != nil {
		logger.Warn(
			"proposed service term updated but audit recording failed",
			"service_term_id",
			term.ID,
			"future_offering_id",
			futureOfferingID,
			"error",
			err,
		)
	}

	logger.Info(
		"proposed service term updated",
		"service_term_id",
		term.ID,
		"future_offering_id",
		futureOfferingID,
	)

	app.respondWithJSON(
		w,
		http.StatusOK,
		jsonResponse{
			Error:   false,
			Message: "Proposed service term updated successfully",
			Data:    newMerchantFutureOfferingServiceTermDTO(term),
		},
	)
}

// -----------------------------------------------------------------------------
// Establish
// -----------------------------------------------------------------------------

// EstablishMerchantFutureOfferingServiceTermHandler establishes an existing
// proposal as the authoritative Service Term.
//
// The handler knows nothing about downstream Service Period generation or
// other consumers of establishment. Those concerns must remain outside the
// HTTP boundary.
func (app *Application) EstablishMerchantFutureOfferingServiceTermHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	const fn = "EstablishMerchantFutureOfferingServiceTermHandler"

	logger := app.Logger.
		GetLoggerWithContext(r).
		WithFunctionName(fn)

	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	if !app.HasPermission(
		ctx,
		permissionEstablishMerchantFutureOfferingServiceTerm,
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
		parseMerchantFutureOfferingServiceTermPathUUID(
			r,
			"futureOfferingID",
		)
	if err != nil {
		app.respondWithError(w, err, http.StatusBadRequest)
		return
	}

	serviceTermID, err :=
		parseMerchantFutureOfferingServiceTermPathUUID(
			r,
			"serviceTermID",
		)
	if err != nil {
		app.respondWithError(w, err, http.StatusBadRequest)
		return
	}

	var req establishMerchantFutureOfferingServiceTermRequest

	if err := app.readJSON(w, r, &req); err != nil {
		app.respondWithError(
			w,
			fmt.Errorf("invalid request body: %w", err),
			http.StatusBadRequest,
		)
		return
	}

	termStartsOn, err :=
		parseRequiredMerchantFutureOfferingServiceTermDate(
			req.TermStartsOn,
		)
	if err != nil {
		app.respondWithError(
			w,
			fmt.Errorf("term_starts_on: %w", err),
			http.StatusBadRequest,
		)
		return
	}

	termEndsOn, err :=
		parseRequiredMerchantFutureOfferingServiceTermDate(
			req.TermEndsOn,
		)
	if err != nil {
		app.respondWithError(
			w,
			fmt.Errorf("term_ends_on: %w", err),
			http.StatusBadRequest,
		)
		return
	}

	term, err :=
		app.Models.MerchantFutureOfferingServiceTerm.Establish(
			ctx,
			serviceTermID,
			futureOfferingID,
			termStartsOn,
			termEndsOn,
		)
	if err != nil {
		app.respondMerchantFutureOfferingServiceTermError(
			w,
			r,
			fn,
			err,
		)
		return
	}

	if err := app.insertGovernanceAudit(
		ctx,
		userID,
		actionEstablishMerchantFutureOfferingServiceTerm,
		"Established a Future Offering Service Term",
		merchantFutureOfferingServiceTermEntityType,
		merchantFutureOfferingServiceTermEntityTypeDescription,
		term.ID.String(),
	); err != nil {
		logger.Warn(
			"service term established but audit recording failed",
			"service_term_id",
			term.ID,
			"future_offering_id",
			futureOfferingID,
			"error",
			err,
		)
	}

	logger.Info(
		"service term established",
		"service_term_id",
		term.ID,
		"future_offering_id",
		futureOfferingID,
	)

	app.respondWithJSON(
		w,
		http.StatusOK,
		jsonResponse{
			Error:   false,
			Message: "Service term established successfully",
			Data:    newMerchantFutureOfferingServiceTermDTO(term),
		},
	)
}

// -----------------------------------------------------------------------------
// Replace established
// -----------------------------------------------------------------------------

// ReplaceEstablishedMerchantFutureOfferingServiceTermHandler atomically
// supersedes the established predecessor and establishes an already proposed
// replacement.
//
// Replacement must remain one domain operation. The HTTP layer must never
// decompose it into independent supersede and establish mutations.
func (app *Application) ReplaceEstablishedMerchantFutureOfferingServiceTermHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	const fn = "ReplaceEstablishedMerchantFutureOfferingServiceTermHandler"

	logger := app.Logger.
		GetLoggerWithContext(r).
		WithFunctionName(fn)

	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	if !app.HasPermission(
		ctx,
		permissionReplaceMerchantFutureOfferingServiceTerm,
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
		parseMerchantFutureOfferingServiceTermPathUUID(
			r,
			"futureOfferingID",
		)
	if err != nil {
		app.respondWithError(w, err, http.StatusBadRequest)
		return
	}

	predecessorID, err :=
		parseMerchantFutureOfferingServiceTermPathUUID(
			r,
			"serviceTermID",
		)
	if err != nil {
		app.respondWithError(w, err, http.StatusBadRequest)
		return
	}

	var req replaceMerchantFutureOfferingServiceTermRequest

	if err := app.readJSON(w, r, &req); err != nil {
		app.respondWithError(
			w,
			fmt.Errorf("invalid request body: %w", err),
			http.StatusBadRequest,
		)
		return
	}

	if req.ReplacementServiceTermID == uuid.Nil {
		app.respondWithError(
			w,
			errors.New("replacement_service_term_id is required"),
			http.StatusBadRequest,
		)
		return
	}

	if req.ReplacementServiceTermID == predecessorID {
		app.respondWithError(
			w,
			errors.New(
				"replacement_service_term_id must differ from the established service term",
			),
			http.StatusBadRequest,
		)
		return
	}

	termStartsOn, err :=
		parseRequiredMerchantFutureOfferingServiceTermDate(
			req.TermStartsOn,
		)
	if err != nil {
		app.respondWithError(
			w,
			fmt.Errorf("term_starts_on: %w", err),
			http.StatusBadRequest,
		)
		return
	}

	termEndsOn, err :=
		parseRequiredMerchantFutureOfferingServiceTermDate(
			req.TermEndsOn,
		)
	if err != nil {
		app.respondWithError(
			w,
			fmt.Errorf("term_ends_on: %w", err),
			http.StatusBadRequest,
		)
		return
	}

	predecessor, replacement, err :=
		app.Models.MerchantFutureOfferingServiceTerm.
			ReplaceEstablished(
				ctx,
				predecessorID,
				req.ReplacementServiceTermID,
				futureOfferingID,
				termStartsOn,
				termEndsOn,
			)
	if err != nil {
		app.respondMerchantFutureOfferingServiceTermError(
			w,
			r,
			fn,
			err,
		)
		return
	}

	if err := app.insertGovernanceAudit(
		ctx,
		userID,
		actionReplaceMerchantFutureOfferingServiceTerm,
		"Replaced an established Future Offering Service Term",
		merchantFutureOfferingServiceTermEntityType,
		merchantFutureOfferingServiceTermEntityTypeDescription,
		replacement.ID.String(),
	); err != nil {
		logger.Warn(
			"service term replaced but audit recording failed",
			"predecessor_service_term_id",
			predecessor.ID,
			"replacement_service_term_id",
			replacement.ID,
			"future_offering_id",
			futureOfferingID,
			"error",
			err,
		)
	}

	logger.Info(
		"established service term replaced",
		"predecessor_service_term_id",
		predecessor.ID,
		"replacement_service_term_id",
		replacement.ID,
		"future_offering_id",
		futureOfferingID,
	)

	app.respondWithJSON(
		w,
		http.StatusOK,
		jsonResponse{
			Error:   false,
			Message: "Established service term replaced successfully",
			Data: merchantFutureOfferingServiceTermReplacementResponse{
				Predecessor: newMerchantFutureOfferingServiceTermDTO(
					predecessor,
				),
				Replacement: newMerchantFutureOfferingServiceTermDTO(
					replacement,
				),
			},
		},
	)
}
