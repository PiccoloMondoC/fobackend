// Package main provides the merchant-facing HTTP boundary for the
// authoritative Future Offering aggregate.
//
// focodebase/fobackend/internal/server/cmd/api/merchant_future_offerings.go
//
// GTM:
//
//	Layer: 3.2 API / Merchant Future Offering Domain
//	Release Class: SPINE
//	Reason:
//	  merchant_future_offerings is the authoritative merchant-owned Future
//	  Offering aggregate for PCDF-M01. This handler exposes draft creation,
//	  merchant-scoped retrieval and listing, draft-only fact replacement,
//	  and draft discard.
//
//	  Submission is intentionally not exposed by this file until the service
//	  layer can perform submission readiness, the draft -> submitted lifecycle
//	  transition, and FutureOfferingSubmitted outbox persistence atomically.
//
// Domain Boundary:
//
//	This handler owns HTTP representation for merchant_future_offerings only.
//
//	It does not own:
//	  - Future Offering assets;
//	  - engagement options;
//	  - goals;
//	  - service/commercial terms;
//	  - milestones;
//	  - Future Offering event/history;
//	  - submission-readiness orchestration.
//
// Merchant Authorization:
//
//	The authenticated user comes exclusively from AuthMiddleware.
//
//	X-Merchant-ID may identify which Merchant an authenticated principal wishes
//	to operate, but possession of that identifier is not authority. Every
//	request verifies the authenticated User is the principal of that Merchant's
//	active canonical Merchant Account before accessing Future Offering state.
//
//	The data layer independently scopes every merchant-facing Future Offering
//	read or mutation by merchant_id, preserving defense in depth.
//
// Concurrency:
//
//	Draft update and discard require the last observed updated_at value.
//	The data layer performs the authoritative optimistic-concurrency check.
//
// SPINE Rule:
//
//	Keep compiling.
//	Keep production-ready.
//	Preserve authenticated Merchant Account principal verification.
//	Preserve merchant-scoped data access.
//	Preserve draft-only mutation and discard.
//	Preserve optimistic concurrency.
//	Preserve bounded keyset pagination.
//	Do not expose generic lifecycle mutation.
//	Do not accept merchant_id, status, lifecycle timestamps, or server-owned
//	fields in request JSON.
//	Do not implement submission in the handler.
//	Do not couple this aggregate to assets, goals, engagement options,
//	commercial terms, milestones, or event/history persistence.
package main

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/PiccoloMondoC/focodebase/fobackend/internal/data"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

const (
	merchantFutureOfferingEntityType = "merchant_future_offering"

	merchantFutureOfferingEntityTypeDescription = "Canonical merchant-owned Future Offering aggregate"

	actionCreateMerchantFutureOfferingDraft = "create_merchant_future_offering"

	actionUpdateMerchantFutureOfferingDraft = "update_merchant_future_offering_draft"

	actionDiscardMerchantFutureOfferingDraft = "discard_merchant_future_offering_draft"

	permissionCreateMerchantFutureOffering = "create_merchant_future_offering"

	permissionReadMerchantFutureOffering = "read_merchant_future_offering"

	permissionListMerchantFutureOfferings = "list_merchant_future_offerings"

	permissionUpdateMerchantFutureOfferingDraft = "update_merchant_future_offering_draft"

	permissionDiscardMerchantFutureOfferingDraft = "discard_merchant_future_offering_draft"

	merchantFutureOfferingDefaultListLimit = 20
	merchantFutureOfferingMaxListLimit     = 100
)

var (
	errMerchantFutureOfferingAuthenticatedUserMissing = errors.New("authenticated user missing from trusted context")

	errMerchantFutureOfferingMerchantContextRequired = errors.New("merchant context is required")
)

// -----------------------------------------------------------------------------
// Request / response contracts
// -----------------------------------------------------------------------------

type createMerchantFutureOfferingDraftRequest struct {
	ProjectName     string     `json:"project_name"`
	Title           *string    `json:"title"`
	Summary         string     `json:"summary"`
	Description     *string    `json:"description"`
	CategoryID      *uuid.UUID `json:"category_id"`
	OfferingType    *string    `json:"offering_type"`
	ReleaseStrategy *string    `json:"release_strategy"`
	AccessPolicy    *string    `json:"access_policy"`
	LaunchAt        *time.Time `json:"launch_at"`
}

type updateMerchantFutureOfferingDraftRequest struct {
	ProjectName       string     `json:"project_name"`
	Title             *string    `json:"title"`
	Summary           string     `json:"summary"`
	Description       *string    `json:"description"`
	CategoryID        *uuid.UUID `json:"category_id"`
	OfferingType      *string    `json:"offering_type"`
	ReleaseStrategy   *string    `json:"release_strategy"`
	AccessPolicy      *string    `json:"access_policy"`
	LaunchAt          *time.Time `json:"launch_at"`
	ExpectedUpdatedAt time.Time  `json:"expected_updated_at"`
}

type merchantFutureOfferingResponse struct {
	ID              uuid.UUID  `json:"id"`
	ProjectName     string     `json:"project_name"`
	Title           *string    `json:"title,omitempty"`
	Summary         string     `json:"summary"`
	Description     *string    `json:"description,omitempty"`
	CategoryID      *uuid.UUID `json:"category_id,omitempty"`
	OfferingType    *string    `json:"offering_type,omitempty"`
	ReleaseStrategy *string    `json:"release_strategy,omitempty"`
	AccessPolicy    *string    `json:"access_policy,omitempty"`
	Status          string     `json:"status"`
	LaunchAt        *time.Time `json:"launch_at,omitempty"`
	SubmittedAt     *time.Time `json:"submitted_at,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
}

type merchantFutureOfferingListResponse struct {
	FutureOfferings     []merchantFutureOfferingResponse `json:"future_offerings"`
	NextBeforeUpdatedAt *time.Time                       `json:"next_before_updated_at,omitempty"`
	NextBeforeID        *uuid.UUID                       `json:"next_before_id,omitempty"`
}

// -----------------------------------------------------------------------------
// Conversion
// -----------------------------------------------------------------------------

func merchantFutureOfferingDraftFacts(
	projectName string,
	title *string,
	summary string,
	description *string,
	categoryID *uuid.UUID,
	offeringType *string,
	releaseStrategy *string,
	accessPolicy *string,
	launchAt *time.Time,
) data.MerchantFutureOfferingDraftFacts {
	facts := data.MerchantFutureOfferingDraftFacts{
		ProjectName: projectName,
		Title:       title,
		Summary:     summary,
		Description: description,
		CategoryID:  categoryID,
		LaunchAt:    launchAt,
	}

	if offeringType != nil {
		value := data.MerchantFutureOfferingType(*offeringType)
		facts.OfferingType = &value
	}

	if releaseStrategy != nil {
		value := data.MerchantFutureOfferingReleaseStrategy(
			*releaseStrategy,
		)
		facts.ReleaseStrategy = &value
	}

	if accessPolicy != nil {
		value := data.MerchantFutureOfferingAccessPolicy(*accessPolicy)
		facts.AccessPolicy = &value
	}

	return facts
}

func newMerchantFutureOfferingResponse(
	fo *data.MerchantFutureOffering,
) merchantFutureOfferingResponse {
	response := merchantFutureOfferingResponse{
		ID:          fo.ID,
		ProjectName: fo.ProjectName,
		Title:       fo.Title,
		Summary:     fo.Summary,
		Description: fo.Description,
		CategoryID:  fo.CategoryID,
		Status:      string(fo.Status),
		LaunchAt:    fo.LaunchAt,
		SubmittedAt: fo.SubmittedAt,
		CreatedAt:   fo.CreatedAt,
		UpdatedAt:   fo.UpdatedAt,
	}

	if fo.OfferingType != nil {
		value := string(*fo.OfferingType)
		response.OfferingType = &value
	}

	if fo.ReleaseStrategy != nil {
		value := string(*fo.ReleaseStrategy)
		response.ReleaseStrategy = &value
	}

	if fo.AccessPolicy != nil {
		value := string(*fo.AccessPolicy)
		response.AccessPolicy = &value
	}

	return response
}

// -----------------------------------------------------------------------------
// Merchant authorization
// -----------------------------------------------------------------------------

// requireAuthorizedMerchantFutureOfferingMerchant resolves the selected
// Merchant and proves that the authenticated user is the canonical principal
// of that Merchant's active Merchant Account.
//
// X-Merchant-ID is a selector only.
func (app *Application) requireAuthorizedMerchantFutureOfferingMerchant(
	ctx context.Context,
) (uuid.UUID, error) {
	userID := app.getUserIDFromContext(ctx)
	if userID == nil || *userID == uuid.Nil {
		return uuid.Nil, errMerchantFutureOfferingAuthenticatedUserMissing
	}

	merchantID := app.getMerchantIDFromContext(ctx)
	if merchantID == nil || *merchantID == uuid.Nil {
		return uuid.Nil, errMerchantFutureOfferingMerchantContextRequired
	}

	authorized, err := app.Models.MerchantAccount.IsPrincipalForMerchant(
		ctx,
		*userID,
		*merchantID,
	)
	if err != nil {
		return uuid.Nil, err
	}
	if !authorized {
		return uuid.Nil, data.ErrMerchantAccountAccessDenied
	}

	return *merchantID, nil
}

func (app *Application) respondMerchantFutureOfferingAuthorizationError(
	w http.ResponseWriter,
	r *http.Request,
	err error,
) {
	switch {
	case errors.Is(
		err,
		errMerchantFutureOfferingMerchantContextRequired,
	):
		app.respondWithError(
			w,
			errors.New("merchant context is required"),
			http.StatusBadRequest,
		)

	case errors.Is(
		err,
		data.ErrMerchantAccountAccessDenied,
	):
		// Deliberately avoid revealing whether the merchant exists.
		app.respondWithError(
			w,
			errors.New("forbidden"),
			http.StatusForbidden,
		)

	case errors.Is(
		err,
		errMerchantFutureOfferingAuthenticatedUserMissing,
	):
		// AuthMiddleware is required upstream for every Future Offering route.
		// Reaching the handler without authenticated-user context therefore
		// indicates broken trusted-context wiring rather than an ordinary
		// unauthenticated client request.
		app.serverErrorResponse(
			app.Logger.GetLoggerWithContext(r),
			w,
			r,
			err,
		)

	default:
		app.serverErrorResponse(
			app.Logger.GetLoggerWithContext(r),
			w,
			r,
			err,
		)
	}
}

// -----------------------------------------------------------------------------
// Error mapping
// -----------------------------------------------------------------------------

func (app *Application) respondMerchantFutureOfferingError(
	w http.ResponseWriter,
	r *http.Request,
	err error,
) {
	switch {
	case errors.Is(
		err,
		data.ErrMerchantFutureOfferingInvalidInput,
	):
		app.respondWithError(
			w,
			errors.New("invalid future offering request"),
			http.StatusBadRequest,
		)

	case errors.Is(
		err,
		data.ErrMerchantFutureOfferingCategoryNotFound,
	):
		app.respondWithError(
			w,
			errors.New("category not found"),
			http.StatusBadRequest,
		)

	case errors.Is(
		err,
		data.ErrMerchantFutureOfferingMerchantNotFound,
	):
		app.respondWithError(
			w,
			errors.New("merchant not found"),
			http.StatusBadRequest,
		)

	case errors.Is(
		err,
		data.ErrMerchantFutureOfferingNotFound,
	):
		app.respondWithError(
			w,
			errors.New("future offering not found"),
			http.StatusNotFound,
		)

	case errors.Is(
		err,
		data.ErrMerchantFutureOfferingInvalidTransition,
	),
		errors.Is(
			err,
			data.ErrMerchantFutureOfferingEditConflict,
		):
		app.respondWithError(
			w,
			errors.New("future offering state conflict"),
			http.StatusConflict,
		)

	case errors.Is(
		err,
		data.ErrMerchantFutureOfferingInvalidState,
	):
		// InvalidState represents a persisted/domain-integrity contradiction,
		// not ordinary client-correctable validation input.
		app.serverErrorResponse(
			app.Logger.GetLoggerWithContext(r),
			w,
			r,
			err,
		)

	default:
		app.serverErrorResponse(
			app.Logger.GetLoggerWithContext(r),
			w,
			r,
			err,
		)
	}
}

// -----------------------------------------------------------------------------
// Pagination
// -----------------------------------------------------------------------------

func parseMerchantFutureOfferingListCursor(
	r *http.Request,
) (*time.Time, *uuid.UUID, error) {
	query := r.URL.Query()

	rawUpdatedAt :=
		strings.TrimSpace(query.Get("before_updated_at"))

	rawID :=
		strings.TrimSpace(query.Get("before_id"))

	if rawUpdatedAt == "" && rawID == "" {
		return nil, nil, nil
	}

	if rawUpdatedAt == "" || rawID == "" {
		return nil, nil, errors.New(
			"before_updated_at and before_id must be supplied together",
		)
	}

	updatedAt, err :=
		time.Parse(time.RFC3339Nano, rawUpdatedAt)
	if err != nil {
		return nil, nil, errors.New(
			"before_updated_at must be an RFC3339 timestamp",
		)
	}

	id, err := uuid.Parse(rawID)
	if err != nil || id == uuid.Nil {
		return nil, nil, errors.New(
			"before_id must be a valid UUID",
		)
	}

	return &updatedAt, &id, nil
}

func merchantFutureOfferingListLimit(
	r *http.Request,
) (int, error) {
	raw :=
		strings.TrimSpace(r.URL.Query().Get("limit"))

	if raw == "" {
		return merchantFutureOfferingDefaultListLimit, nil
	}

	limit, err := strconv.Atoi(raw)
	if err != nil || limit <= 0 {
		return 0, errors.New(
			"limit must be a positive integer",
		)
	}

	if limit > merchantFutureOfferingMaxListLimit {
		limit = merchantFutureOfferingMaxListLimit
	}

	return limit, nil
}

// -----------------------------------------------------------------------------
// Create
// -----------------------------------------------------------------------------

func (app *Application) CreateMerchantFutureOfferingDraftHandler(
	w http.ResponseWriter,
	r *http.Request,
) {

	logger :=
		app.Logger.
			GetLoggerWithContext(r).
			WithFunctionName(
				"CreateMerchantFutureOfferingDraftHandler",
			)

	ctx, cancel :=
		context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	merchantID, err :=
		app.requireAuthorizedMerchantFutureOfferingMerchant(ctx)
	if err != nil {
		app.respondMerchantFutureOfferingAuthorizationError(
			w,
			r,
			err,
		)
		return
	}

	userID := app.getUserIDFromContext(ctx)

	var input createMerchantFutureOfferingDraftRequest

	if err := app.readJSON(w, r, &input); err != nil {
		app.respondWithError(
			w,
			errors.New("malformed request body"),
			http.StatusBadRequest,
		)
		return
	}

	facts := merchantFutureOfferingDraftFacts(
		input.ProjectName,
		input.Title,
		input.Summary,
		input.Description,
		input.CategoryID,
		input.OfferingType,
		input.ReleaseStrategy,
		input.AccessPolicy,
		input.LaunchAt,
	)

	fo := &data.MerchantFutureOffering{
		MerchantID:      merchantID,
		ProjectName:     facts.ProjectName,
		Title:           facts.Title,
		Summary:         facts.Summary,
		Description:     facts.Description,
		CategoryID:      facts.CategoryID,
		OfferingType:    facts.OfferingType,
		ReleaseStrategy: facts.ReleaseStrategy,
		AccessPolicy:    facts.AccessPolicy,
		LaunchAt:        facts.LaunchAt,
	}

	created, err :=
		app.InternalServices.CreateMerchantFutureOfferingDraft(
			ctx,
			*userID,
			fo,
		)
	if err != nil {
		app.respondMerchantFutureOfferingError(
			w,
			r,
			err,
		)
		return
	}

	if err := app.insertGovernanceAudit(
		ctx,
		userID,
		actionCreateMerchantFutureOfferingDraft,
		"Created a merchant Future Offering draft",
		merchantFutureOfferingEntityType,
		merchantFutureOfferingEntityTypeDescription,
		created.ID.String(),
	); err != nil {
		logger.Warn(
			"Future Offering draft created but audit recording failed",
			"future_offering_id",
			created.ID,
			"error",
			err,
		)
	}

	app.respondWithJSON(
		w,
		http.StatusCreated,
		jsonResponse{
			Error:   false,
			Message: "Future Offering draft created successfully",
			Data:    newMerchantFutureOfferingResponse(created),
		},
	)
}

// -----------------------------------------------------------------------------
// Read one
// -----------------------------------------------------------------------------

func (app *Application) GetMerchantFutureOfferingHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	ctx, cancel :=
		context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	merchantID, err :=
		app.requireAuthorizedMerchantFutureOfferingMerchant(ctx)
	if err != nil {
		app.respondMerchantFutureOfferingAuthorizationError(
			w,
			r,
			err,
		)
		return
	}

	futureOfferingID, err :=
		uuid.Parse(
			chi.URLParam(r, "futureOfferingID"),
		)
	if err != nil || futureOfferingID == uuid.Nil {
		app.respondWithError(
			w,
			errors.New("invalid future offering id"),
			http.StatusBadRequest,
		)
		return
	}

	fo, err :=
		app.Models.MerchantFutureOffering.
			GetByIDForMerchant(
				ctx,
				merchantID,
				futureOfferingID,
			)
	if err != nil {
		app.respondMerchantFutureOfferingError(
			w,
			r,
			err,
		)
		return
	}

	app.respondWithJSON(
		w,
		http.StatusOK,
		jsonResponse{
			Error:   false,
			Message: "Future Offering retrieved successfully",
			Data:    newMerchantFutureOfferingResponse(fo),
		},
	)
}

// -----------------------------------------------------------------------------
// List
// -----------------------------------------------------------------------------

func (app *Application) ListMerchantFutureOfferingsHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	ctx, cancel :=
		context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	merchantID, err :=
		app.requireAuthorizedMerchantFutureOfferingMerchant(ctx)
	if err != nil {
		app.respondMerchantFutureOfferingAuthorizationError(
			w,
			r,
			err,
		)
		return
	}

	limit, err := merchantFutureOfferingListLimit(r)
	if err != nil {
		app.respondWithError(
			w,
			err,
			http.StatusBadRequest,
		)
		return
	}

	beforeUpdatedAt, beforeID, err :=
		parseMerchantFutureOfferingListCursor(r)
	if err != nil {
		app.respondWithError(
			w,
			err,
			http.StatusBadRequest,
		)
		return
	}

	// Read one additional row so next-page availability is factual rather
	// than inferred merely because the requested page happened to be full.
	queryLimit := limit + 1

	var offerings []*data.MerchantFutureOffering

	rawStatus :=
		strings.TrimSpace(r.URL.Query().Get("status"))

	if rawStatus == "" {
		offerings, err =
			app.Models.MerchantFutureOffering.ListByMerchant(
				ctx,
				merchantID,
				queryLimit,
				beforeUpdatedAt,
				beforeID,
			)
	} else {
		offerings, err =
			app.Models.MerchantFutureOffering.
				ListByMerchantAndStatus(
					ctx,
					merchantID,
					data.MerchantFutureOfferingStatus(
						rawStatus,
					),
					queryLimit,
					beforeUpdatedAt,
					beforeID,
				)
	}

	if err != nil {
		app.respondMerchantFutureOfferingError(
			w,
			r,
			err,
		)
		return
	}

	hasMore := len(offerings) > limit
	if hasMore {
		offerings = offerings[:limit]
	}

	response := merchantFutureOfferingListResponse{
		FutureOfferings: make(
			[]merchantFutureOfferingResponse,
			0,
			len(offerings),
		),
	}

	for _, fo := range offerings {
		response.FutureOfferings =
			append(
				response.FutureOfferings,
				newMerchantFutureOfferingResponse(fo),
			)
	}

	if hasMore && len(offerings) > 0 {
		last := offerings[len(offerings)-1]

		nextUpdatedAt := last.UpdatedAt
		nextID := last.ID

		response.NextBeforeUpdatedAt = &nextUpdatedAt
		response.NextBeforeID = &nextID
	}

	app.respondWithJSON(
		w,
		http.StatusOK,
		jsonResponse{
			Error:   false,
			Message: "Future Offerings retrieved successfully",
			Data:    response,
		},
	)
}

// -----------------------------------------------------------------------------
// Update draft
// -----------------------------------------------------------------------------

func (app *Application) UpdateMerchantFutureOfferingDraftHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	logger :=
		app.Logger.
			GetLoggerWithContext(r).
			WithFunctionName(
				"UpdateMerchantFutureOfferingDraftHandler",
			)

	ctx, cancel :=
		context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	merchantID, err :=
		app.requireAuthorizedMerchantFutureOfferingMerchant(ctx)
	if err != nil {
		app.respondMerchantFutureOfferingAuthorizationError(
			w,
			r,
			err,
		)
		return
	}

	userID := app.getUserIDFromContext(ctx)

	futureOfferingID, err :=
		uuid.Parse(
			chi.URLParam(r, "futureOfferingID"),
		)
	if err != nil || futureOfferingID == uuid.Nil {
		app.respondWithError(
			w,
			errors.New("invalid future offering id"),
			http.StatusBadRequest,
		)
		return
	}

	var input updateMerchantFutureOfferingDraftRequest

	if err := app.readJSON(w, r, &input); err != nil {
		app.respondWithError(
			w,
			errors.New("malformed request body"),
			http.StatusBadRequest,
		)
		return
	}

	if input.ExpectedUpdatedAt.IsZero() {
		app.respondWithError(
			w,
			errors.New("expected_updated_at is required"),
			http.StatusBadRequest,
		)
		return
	}

	facts := merchantFutureOfferingDraftFacts(
		input.ProjectName,
		input.Title,
		input.Summary,
		input.Description,
		input.CategoryID,
		input.OfferingType,
		input.ReleaseStrategy,
		input.AccessPolicy,
		input.LaunchAt,
	)

	updated, err :=
		app.Models.MerchantFutureOffering.UpdateDraftFacts(
			ctx,
			merchantID,
			futureOfferingID,
			facts,
			input.ExpectedUpdatedAt,
		)
	if err != nil {
		app.respondMerchantFutureOfferingError(
			w,
			r,
			err,
		)
		return
	}

	if err := app.insertGovernanceAudit(
		ctx,
		userID,
		actionUpdateMerchantFutureOfferingDraft,
		"Updated mutable facts of a merchant Future Offering draft",
		merchantFutureOfferingEntityType,
		merchantFutureOfferingEntityTypeDescription,
		updated.ID.String(),
	); err != nil {
		logger.Warn(
			"Future Offering draft updated but audit recording failed",
			"future_offering_id",
			updated.ID,
			"error",
			err,
		)
	}

	app.respondWithJSON(
		w,
		http.StatusOK,
		jsonResponse{
			Error:   false,
			Message: "Future Offering draft updated successfully",
			Data:    newMerchantFutureOfferingResponse(updated),
		},
	)
}

// -----------------------------------------------------------------------------
// Discard draft
// -----------------------------------------------------------------------------

func (app *Application) DiscardMerchantFutureOfferingDraftHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	logger :=
		app.Logger.
			GetLoggerWithContext(r).
			WithFunctionName(
				"DiscardMerchantFutureOfferingDraftHandler",
			)

	ctx, cancel :=
		context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	merchantID, err :=
		app.requireAuthorizedMerchantFutureOfferingMerchant(ctx)
	if err != nil {
		app.respondMerchantFutureOfferingAuthorizationError(
			w,
			r,
			err,
		)
		return
	}

	userID := app.getUserIDFromContext(ctx)

	futureOfferingID, err :=
		uuid.Parse(
			chi.URLParam(r, "futureOfferingID"),
		)
	if err != nil || futureOfferingID == uuid.Nil {
		app.respondWithError(
			w,
			errors.New("invalid future offering id"),
			http.StatusBadRequest,
		)
		return
	}

	rawExpectedUpdatedAt :=
		strings.TrimSpace(
			r.URL.Query().Get("expected_updated_at"),
		)

	if rawExpectedUpdatedAt == "" {
		app.respondWithError(
			w,
			errors.New(
				"expected_updated_at query parameter is required",
			),
			http.StatusBadRequest,
		)
		return
	}

	expectedUpdatedAt, err :=
		time.Parse(
			time.RFC3339Nano,
			rawExpectedUpdatedAt,
		)
	if err != nil {
		app.respondWithError(
			w,
			errors.New(
				"expected_updated_at must be an RFC3339 timestamp",
			),
			http.StatusBadRequest,
		)
		return
	}

	discarded, err :=
		app.Models.MerchantFutureOffering.DiscardDraft(
			ctx,
			merchantID,
			futureOfferingID,
			expectedUpdatedAt,
		)
	if err != nil {
		app.respondMerchantFutureOfferingError(
			w,
			r,
			err,
		)
		return
	}

	if err := app.insertGovernanceAudit(
		ctx,
		userID,
		actionDiscardMerchantFutureOfferingDraft,
		"Discarded a merchant Future Offering draft",
		merchantFutureOfferingEntityType,
		merchantFutureOfferingEntityTypeDescription,
		discarded.ID.String(),
	); err != nil {
		logger.Warn(
			"Future Offering draft discarded but audit recording failed",
			"future_offering_id",
			discarded.ID,
			"error",
			err,
		)
	}

	w.WriteHeader(http.StatusNoContent)
}
