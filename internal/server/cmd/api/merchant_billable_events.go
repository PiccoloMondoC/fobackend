// Package main provides HTTP handlers for privileged merchant billable-event
// commercial-history and reconciliation reads.
//
// sdworkspace/sdbackend/internal/server/cmd/api/merchant_billable_events.go
//
// GTM:
//
//	Layer: 2.4.b Commerce Architecture / Monetization Layer
//	Release Class: SPINE
//	Reason:
//	  merchant_billable_events is the canonical billable-occurrence record
//	  downstream of authoritative Commerce source facts and upstream of fee
//	  calculation, platform-credit application, invoicing, and payment
//	  collection. This handler exposes bounded privileged read access for
//	  administration, support, audit, and reconciliation.
//
// Domain Boundary:
//
//	A merchant billable event is not:
//	  - an invoice;
//	  - a fee calculation;
//	  - an invoice line;
//	  - a payment or payment attempt;
//	  - a payment-provider transaction;
//	  - a billing account;
//	  - a platform-credit application;
//	  - a merchant-held balance;
//	  - a billing-policy decision;
//	  - authorization to charge a merchant; or
//	  - proof that a merchant owes a particular amount.
//
//	This handler does not calculate fees, determine billability policy, select
//	fee schedules, apply credits or promotions, generate invoices, execute
//	payments, or decide collection policy.
//
// Consumer Identity Sovereignty:
//
//	engagement_event_id is exposed only as an opaque authoritative source
//	identifier to privileged platform administration. This handler never fetches
//	or discloses engagement contents or consumer identity and provides no
//	merchant self-service access.
//
// Mutation Boundary:
//
//	No merchant billable-event mutation is exposed through HTTP in this handler
//	layer.
//
//	Insert/InsertTx require service-owned source-semantic and merchant-ownership
//	validation before an occurrence may be recorded. Confirm/Reject/Reverse and
//	their transaction-aware forms remain data/service lifecycle capabilities for
//	internal Commerce orchestration and reconciliation. The existence of those
//	data methods does not itself justify an administrative HTTP mutation surface.
//
//	Any future HTTP mutation capability requires an independently approved
//	operational use case and service-layer orchestration. It must not be added as
//	a generic CRUD consequence of the persistence model.
//
// Handler Boundary:
//
//	All operations in this file are business-logic-free historical reads whose
//	complete persistence semantics already belong to MerchantBillableEventModel.
//	They therefore call app.Models.MerchantBillableEvent directly, consistent
//	with the established append-only historical-read boundary used by
//	merchant_platform_credit_applications.
//
// Authorization:
//
//	This is privileged platform commercial history. No route in this file is
//	merchant self-service. Merchant-role access is deliberately withheld unless
//	a future route family has canonical ownership/delegation enforcement and an
//	explicitly approved merchant-facing requirement.
//
// SPINE Rule:
//
//	Keep compiling.
//	Keep production-ready.
//	Preserve exact-one-source semantics in every response.
//	Preserve immutable source, occurrence, and monetary history.
//	Preserve deterministic bounded keyset pagination
//	(occurred_at DESC, id DESC).
//	Preserve exact decimal-string monetary representation.
//	Preserve strict privileged authorization on every route.
//	Preserve audit coverage for every successful read.
//	Never expose creation, generic update, deletion, restoration, or lifecycle
//	mutation through this handler without an independently approved boundary.
//	Never disclose consumer identity or engagement contents.
//	Never place monetary values or engagement contents in audit metadata.
//	Never implement commercial or operational policy in this file.
//	Block deployment if this file breaks build, authorization, audit
//	accountability, source integrity, privacy, or pagination determinism.
package main

import (
	"context"
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
// Permission, action, and entity vocabulary
// -----------------------------------------------------------------------------

const (
	merchantBillableEventEntityType = "merchant_billable_event"

	merchantBillableEventEntityTypeDescription = "Canonical source-linked merchant billable-occurrence history"

	actionReadMerchantBillableEvent = "read_merchant_billable_event"

	actionListMerchantBillableEvents = "list_merchant_billable_events"

	merchantBillableEventDefaultListLimit = 20
	merchantBillableEventMaximumListLimit = 100
)

// -----------------------------------------------------------------------------
// Error translation
// -----------------------------------------------------------------------------

// merchantBillableEventHTTPStatus translates exported domain errors into a
// stable HTTP status. Diagnostic error wording is not part of the API contract.
func merchantBillableEventHTTPStatus(err error) int {
	if err == nil {
		return http.StatusOK
	}

	switch {
	case errors.Is(err, data.ErrMerchantBillableEventInvalidInput):
		return http.StatusBadRequest

	default:
		return http.StatusInternalServerError
	}
}

// respondWithMerchantBillableEventError prevents internal persistence,
// constraint, driver, and wrapped diagnostic details from becoming public API
// error text.
func (app *Application) respondWithMerchantBillableEventError(
	w http.ResponseWriter,
	err error,
) {
	status := merchantBillableEventHTTPStatus(err)

	switch status {
	case http.StatusBadRequest:
		app.respondWithError(
			w,
			errors.New("invalid merchant billable event request"),
			status,
		)
	default:
		app.respondWithError(
			w,
			errors.New("failed to process merchant billable event request"),
			http.StatusInternalServerError,
		)
	}
}

// -----------------------------------------------------------------------------
// Path parameter parsing
// -----------------------------------------------------------------------------

func parseMerchantBillableEventID(
	r *http.Request,
) (uuid.UUID, error) {
	raw := strings.TrimSpace(
		chi.URLParam(r, "merchantBillableEventID"),
	)
	if raw == "" {
		return uuid.Nil, errors.New(
			"merchant billable event ID is required",
		)
	}

	id, err := uuid.Parse(raw)
	if err != nil || id == uuid.Nil {
		return uuid.Nil, errors.New(
			"invalid merchant billable event ID",
		)
	}

	return id, nil
}

func parseMerchantBillableEventSourceIDPathParam(
	r *http.Request,
	paramName string,
	fieldLabel string,
) (uuid.UUID, error) {
	raw := strings.TrimSpace(chi.URLParam(r, paramName))
	if raw == "" {
		return uuid.Nil, fmt.Errorf(
			"%s is required",
			fieldLabel,
		)
	}

	id, err := uuid.Parse(raw)
	if err != nil || id == uuid.Nil {
		return uuid.Nil, fmt.Errorf(
			"invalid %s",
			fieldLabel,
		)
	}

	return id, nil
}

// -----------------------------------------------------------------------------
// Query parsing
// -----------------------------------------------------------------------------

func parseMerchantBillableEventStatusParam(
	r *http.Request,
) (data.MerchantBillableEventStatus, error) {
	raw := strings.TrimSpace(r.URL.Query().Get("status"))
	if raw == "" {
		return "", errors.New("status is required")
	}

	status := data.NormalizeMerchantBillableEventStatus(
		data.MerchantBillableEventStatus(raw),
	)
	if !data.IsValidMerchantBillableEventStatus(status) {
		return "", errors.New(
			"status must be one of: pending, confirmed, rejected, reversed",
		)
	}

	return status, nil
}

func parseMerchantBillableEventLimit(
	r *http.Request,
) (int, error) {
	raw := strings.TrimSpace(r.URL.Query().Get("limit"))
	if raw == "" {
		return merchantBillableEventDefaultListLimit, nil
	}

	limit, err := strconv.Atoi(raw)
	if err != nil {
		return 0, errors.New("limit must be an integer")
	}

	if limit < 1 || limit > merchantBillableEventMaximumListLimit {
		return 0, fmt.Errorf(
			"limit must be between 1 and %d",
			merchantBillableEventMaximumListLimit,
		)
	}

	return limit, nil
}

func parseMerchantBillableEventListCursor(
	r *http.Request,
) (*time.Time, *uuid.UUID, error) {
	rawOccurredAt := strings.TrimSpace(
		r.URL.Query().Get("before_occurred_at"),
	)
	rawID := strings.TrimSpace(
		r.URL.Query().Get("before_id"),
	)

	if rawOccurredAt == "" && rawID == "" {
		return nil, nil, nil
	}

	if rawOccurredAt == "" || rawID == "" {
		return nil, nil, errors.New(
			"before_occurred_at and before_id must be supplied together",
		)
	}

	occurredAt, err := time.Parse(time.RFC3339, rawOccurredAt)
	if err != nil {
		return nil, nil, errors.New(
			"before_occurred_at must be a valid RFC3339 timestamp",
		)
	}

	id, err := uuid.Parse(rawID)
	if err != nil || id == uuid.Nil {
		return nil, nil, errors.New(
			"before_id must be a valid non-nil UUID",
		)
	}

	occurredAtUTC := occurredAt.UTC()

	return &occurredAtUTC, &id, nil
}

// -----------------------------------------------------------------------------
// Response DTOs
// -----------------------------------------------------------------------------

// merchantBillableEventResponse is the stable privileged HTTP presentation of
// one billable-occurrence record.
//
// Exactly one source ID is non-nil. EngagementEventID remains an opaque source
// reference; this handler never resolves it to engagement or consumer data.
type merchantBillableEventResponse struct {
	ID uuid.UUID `json:"id"`

	MerchantID uuid.UUID `json:"merchant_id"`

	FutureOfferingEventID *uuid.UUID `json:"future_offering_event_id,omitempty"`

	BillingPeriodID *uuid.UUID `json:"billing_period_id,omitempty"`

	EngagementEventID *uuid.UUID `json:"engagement_event_id,omitempty"`

	BillableEventType data.MerchantBillableEventType `json:"billable_event_type"`

	GrossEventValue *string `json:"gross_event_value,omitempty"`

	Currency *string `json:"currency,omitempty"`

	OccurredAt time.Time `json:"occurred_at"`

	ConfirmedAt *time.Time `json:"confirmed_at,omitempty"`

	RejectedAt *time.Time `json:"rejected_at,omitempty"`

	ReversedAt *time.Time `json:"reversed_at,omitempty"`

	Status data.MerchantBillableEventStatus `json:"status"`

	CreatedAt time.Time `json:"created_at"`

	UpdatedAt time.Time `json:"updated_at"`
}

func newMerchantBillableEventResponse(
	event *data.MerchantBillableEvent,
) merchantBillableEventResponse {
	return merchantBillableEventResponse{
		ID:                    event.ID,
		MerchantID:            event.MerchantID,
		FutureOfferingEventID: event.FutureOfferingEventID,
		BillingPeriodID:       event.BillingPeriodID,
		EngagementEventID:     event.EngagementEventID,
		BillableEventType:     event.BillableEventType,
		GrossEventValue:       event.GrossEventValue,
		Currency:              event.Currency,
		OccurredAt:            event.OccurredAt,
		ConfirmedAt:           event.ConfirmedAt,
		RejectedAt:            event.RejectedAt,
		ReversedAt:            event.ReversedAt,
		Status:                event.Status,
		CreatedAt:             event.CreatedAt,
		UpdatedAt:             event.UpdatedAt,
	}
}

type merchantBillableEventListPagination struct {
	Limit int `json:"limit"`

	Count int `json:"count"`

	NextBeforeOccurredAt *time.Time `json:"next_before_occurred_at,omitempty"`

	NextBeforeID *uuid.UUID `json:"next_before_id,omitempty"`
}

type merchantBillableEventListResponse struct {
	Events []merchantBillableEventResponse `json:"events"`

	Pagination merchantBillableEventListPagination `json:"pagination"`
}

func newMerchantBillableEventListResponse(
	events []*data.MerchantBillableEvent,
	limit int,
) merchantBillableEventListResponse {
	responseEvents := make(
		[]merchantBillableEventResponse,
		0,
		len(events),
	)

	for _, event := range events {
		if event == nil {
			continue
		}

		responseEvents = append(
			responseEvents,
			newMerchantBillableEventResponse(event),
		)
	}

	pagination := merchantBillableEventListPagination{
		Limit: limit,
		Count: len(responseEvents),
	}

	// The underlying model returns at most limit rows. A cursor on every
	// non-empty page is intentionally safe: clients may issue one final bounded
	// request and receive an empty page when history is exhausted.
	if len(responseEvents) > 0 {
		last := responseEvents[len(responseEvents)-1]
		occurredAt := last.OccurredAt.UTC()
		id := last.ID

		pagination.NextBeforeOccurredAt = &occurredAt
		pagination.NextBeforeID = &id
	}

	return merchantBillableEventListResponse{
		Events:     responseEvents,
		Pagination: pagination,
	}
}

// -----------------------------------------------------------------------------
// Audit
// -----------------------------------------------------------------------------

func (app *Application) auditMerchantBillableEventRead(
	ctx context.Context,
	userID *uuid.UUID,
	action string,
	description string,
	entityID string,
) error {
	return app.insertGovernanceAudit(
		ctx,
		userID,
		action,
		description,
		merchantBillableEventEntityType,
		merchantBillableEventEntityTypeDescription,
		entityID,
	)
}

// -----------------------------------------------------------------------------
// Canonical ID read
// -----------------------------------------------------------------------------

func (app *Application) GetMerchantBillableEventByIDHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	logger := app.Logger.
		GetLoggerWithContext(r).
		WithFunctionName(
			"GetMerchantBillableEventByIDHandler",
		)

	ctx, cancel := context.WithTimeout(
		r.Context(),
		cfgTimeout,
	)
	defer cancel()

	if !app.HasPermission(
		ctx,
		actionReadMerchantBillableEvent,
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

	id, err := parseMerchantBillableEventID(r)
	if err != nil {
		app.respondWithError(
			w,
			err,
			http.StatusBadRequest,
		)
		return
	}

	event, err :=
		app.Models.MerchantBillableEvent.GetByID(
			ctx,
			id,
		)
	if err != nil {
		logger.Error(
			"Get merchant billable event by ID failed",
			"merchant_billable_event_id",
			id,
			"error",
			err,
		)

		app.respondWithMerchantBillableEventError(
			w,
			err,
		)
		return
	}

	if event == nil {
		app.respondWithError(
			w,
			errors.New(
				"merchant billable event not found",
			),
			http.StatusNotFound,
		)
		return
	}

	message :=
		"Merchant billable event retrieved successfully"

	if auditErr := app.auditMerchantBillableEventRead(
		ctx,
		userID,
		actionReadMerchantBillableEvent,
		"Read a merchant billable event",
		event.ID.String(),
	); auditErr != nil {
		logger.Warn(
			"Merchant billable event retrieved but audit recording failed",
			"merchant_billable_event_id",
			event.ID,
			"error",
			auditErr,
		)

		message =
			"Merchant billable event retrieved, but audit logging failed"
	}

	app.respondWithJSON(
		w,
		http.StatusOK,
		jsonResponse{
			Error:   false,
			Message: message,
			Data:    newMerchantBillableEventResponse(event),
		},
	)
}

// -----------------------------------------------------------------------------
// Source-ID reads
// -----------------------------------------------------------------------------

func (app *Application) getMerchantBillableEventBySource(
	w http.ResponseWriter,
	r *http.Request,
	functionName string,
	sourceParamName string,
	fieldLabel string,
	lookup func(
		context.Context,
		uuid.UUID,
	) (*data.MerchantBillableEvent, error),
) {
	logger := app.Logger.
		GetLoggerWithContext(r).
		WithFunctionName(functionName)

	ctx, cancel := context.WithTimeout(
		r.Context(),
		cfgTimeout,
	)
	defer cancel()

	if !app.HasPermission(
		ctx,
		actionReadMerchantBillableEvent,
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

	sourceID, err :=
		parseMerchantBillableEventSourceIDPathParam(
			r,
			sourceParamName,
			fieldLabel,
		)
	if err != nil {
		app.respondWithError(
			w,
			err,
			http.StatusBadRequest,
		)
		return
	}

	event, err := lookup(ctx, sourceID)
	if err != nil {
		logger.Error(
			"Get merchant billable event by source ID failed",
			"source_field",
			fieldLabel,
			"source_id",
			sourceID,
			"error",
			err,
		)

		app.respondWithMerchantBillableEventError(
			w,
			err,
		)
		return
	}

	if event == nil {
		app.respondWithError(
			w,
			errors.New(
				"merchant billable event not found for supplied source",
			),
			http.StatusNotFound,
		)
		return
	}

	message :=
		"Merchant billable event retrieved successfully"

	if auditErr := app.auditMerchantBillableEventRead(
		ctx,
		userID,
		actionReadMerchantBillableEvent,
		"Read a merchant billable event by source ID",
		event.ID.String(),
	); auditErr != nil {
		logger.Warn(
			"Merchant billable event retrieved but audit recording failed",
			"merchant_billable_event_id",
			event.ID,
			"source_field",
			fieldLabel,
			"source_id",
			sourceID,
			"error",
			auditErr,
		)

		message =
			"Merchant billable event retrieved, but audit logging failed"
	}

	app.respondWithJSON(
		w,
		http.StatusOK,
		jsonResponse{
			Error:   false,
			Message: message,
			Data:    newMerchantBillableEventResponse(event),
		},
	)
}

func (app *Application) GetMerchantBillableEventByFutureOfferingEventIDHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	app.getMerchantBillableEventBySource(
		w,
		r,
		"GetMerchantBillableEventByFutureOfferingEventIDHandler",
		"futureOfferingEventID",
		"future offering event ID",
		app.Models.MerchantBillableEvent.
			GetByFutureOfferingEventID,
	)
}

func (app *Application) GetMerchantBillableEventByBillingPeriodIDHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	app.getMerchantBillableEventBySource(
		w,
		r,
		"GetMerchantBillableEventByBillingPeriodIDHandler",
		"billingPeriodID",
		"billing period ID",
		app.Models.MerchantBillableEvent.
			GetByBillingPeriodID,
	)
}

func (app *Application) GetMerchantBillableEventByEngagementEventIDHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	app.getMerchantBillableEventBySource(
		w,
		r,
		"GetMerchantBillableEventByEngagementEventIDHandler",
		"engagementEventID",
		"engagement event ID",
		app.Models.MerchantBillableEvent.
			GetByEngagementEventID,
	)
}

// -----------------------------------------------------------------------------
// Merchant-scoped history
// -----------------------------------------------------------------------------

func (app *Application) ListMerchantBillableEventsByMerchantHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	logger := app.Logger.
		GetLoggerWithContext(r).
		WithFunctionName(
			"ListMerchantBillableEventsByMerchantHandler",
		)

	ctx, cancel := context.WithTimeout(
		r.Context(),
		cfgTimeout,
	)
	defer cancel()

	if !app.HasPermission(
		ctx,
		actionListMerchantBillableEvents,
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

	merchantID, err :=
		app.parseMerchantIDPathParam(r)
	if err != nil {
		app.respondWithError(
			w,
			err,
			http.StatusBadRequest,
		)
		return
	}

	limit, err :=
		parseMerchantBillableEventLimit(r)
	if err != nil {
		app.respondWithError(
			w,
			err,
			http.StatusBadRequest,
		)
		return
	}

	beforeOccurredAt, beforeID, err :=
		parseMerchantBillableEventListCursor(r)
	if err != nil {
		app.respondWithError(
			w,
			err,
			http.StatusBadRequest,
		)
		return
	}

	events, err :=
		app.Models.MerchantBillableEvent.
			ListByMerchant(
				ctx,
				merchantID,
				limit,
				beforeOccurredAt,
				beforeID,
			)
	if err != nil {
		logger.Error(
			"List merchant billable events by merchant failed",
			"merchant_id",
			merchantID,
			"error",
			err,
		)

		app.respondWithMerchantBillableEventError(
			w,
			err,
		)
		return
	}

	response :=
		newMerchantBillableEventListResponse(
			events,
			limit,
		)

	message :=
		"Merchant billable events retrieved successfully"

	if auditErr := app.auditMerchantBillableEventRead(
		ctx,
		userID,
		actionListMerchantBillableEvents,
		"List a merchant's billable event history",
		merchantID.String(),
	); auditErr != nil {
		logger.Warn(
			"Merchant billable events retrieved but audit recording failed",
			"merchant_id",
			merchantID,
			"error",
			auditErr,
		)

		message =
			"Merchant billable events retrieved, but audit logging failed"
	}

	logger.Info(
		"Merchant billable events retrieved",
		"merchant_id",
		merchantID,
		"result_count",
		len(response.Events),
	)

	app.respondWithJSON(
		w,
		http.StatusOK,
		jsonResponse{
			Error:   false,
			Message: message,
			Data:    response,
		},
	)
}

func (app *Application) ListMerchantBillableEventsByMerchantAndStatusHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	logger := app.Logger.
		GetLoggerWithContext(r).
		WithFunctionName(
			"ListMerchantBillableEventsByMerchantAndStatusHandler",
		)

	ctx, cancel := context.WithTimeout(
		r.Context(),
		cfgTimeout,
	)
	defer cancel()

	if !app.HasPermission(
		ctx,
		actionListMerchantBillableEvents,
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

	merchantID, err :=
		app.parseMerchantIDPathParam(r)
	if err != nil {
		app.respondWithError(
			w,
			err,
			http.StatusBadRequest,
		)
		return
	}

	status, err :=
		parseMerchantBillableEventStatusParam(r)
	if err != nil {
		app.respondWithError(
			w,
			err,
			http.StatusBadRequest,
		)
		return
	}

	limit, err :=
		parseMerchantBillableEventLimit(r)
	if err != nil {
		app.respondWithError(
			w,
			err,
			http.StatusBadRequest,
		)
		return
	}

	beforeOccurredAt, beforeID, err :=
		parseMerchantBillableEventListCursor(r)
	if err != nil {
		app.respondWithError(
			w,
			err,
			http.StatusBadRequest,
		)
		return
	}

	events, err :=
		app.Models.MerchantBillableEvent.
			ListByMerchantAndStatus(
				ctx,
				merchantID,
				status,
				limit,
				beforeOccurredAt,
				beforeID,
			)
	if err != nil {
		logger.Error(
			"List merchant billable events by merchant and status failed",
			"merchant_id",
			merchantID,
			"status",
			status,
			"error",
			err,
		)

		app.respondWithMerchantBillableEventError(
			w,
			err,
		)
		return
	}

	response :=
		newMerchantBillableEventListResponse(
			events,
			limit,
		)

	auditEntityID := fmt.Sprintf(
		"merchant:%s:status:%s",
		merchantID,
		status,
	)

	message :=
		"Merchant billable events retrieved successfully"

	if auditErr := app.auditMerchantBillableEventRead(
		ctx,
		userID,
		actionListMerchantBillableEvents,
		"List a merchant's billable event history by status",
		auditEntityID,
	); auditErr != nil {
		logger.Warn(
			"Merchant billable events retrieved but audit recording failed",
			"merchant_id",
			merchantID,
			"status",
			status,
			"error",
			auditErr,
		)

		message =
			"Merchant billable events retrieved, but audit logging failed"
	}

	logger.Info(
		"Merchant billable events retrieved by status",
		"merchant_id",
		merchantID,
		"status",
		status,
		"result_count",
		len(response.Events),
	)

	app.respondWithJSON(
		w,
		http.StatusOK,
		jsonResponse{
			Error:   false,
			Message: message,
			Data:    response,
		},
	)
}
