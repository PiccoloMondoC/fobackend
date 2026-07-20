// Package main provides HTTP handlers for the privileged, read-only merchant
// program subscription lifecycle-event history surface.
//
// sdworkspace/sdbackend/internal/server/cmd/api/merchant_program_subscription_events.go
//
// GTM:
//
//	Layer: 2.4 Merchant / Future Offering Domain
//	Release Class: SPINE
//	Reason:
//	  merchant_program_subscription_events preserves the durable,
//	  append-only history of commercially meaningful merchant program
//	  subscription lifecycle actions.
//
//	  This handler surface supports privileged subscription administration,
//	  merchant support, lifecycle investigation, commercial traceability,
//	  and later billing reconciliation.
//
//	  Canonical current subscription state remains owned by
//	  merchant_program_subscriptions. Event creation is not exposed through
//	  HTTP. Subscription lifecycle services must eventually record canonical
//	  subscription mutations and corresponding event rows atomically through
//	  the transaction-compatible data-layer insertion seam.
//
//	  This file is not subscription-transition policy, merchant ownership
//	  resolution, billing-ledger logic, payment processing, fee calculation,
//	  audit-log persistence, outbox publication, or general-purpose event
//	  administration.
//
// SPINE Rule:
//
//	Keep compiling.
//	Keep production-ready.
//	Preserve privileged authentication and authorization.
//	Preserve append-only event semantics.
//	Preserve canonical subscription-state ownership.
//	Preserve bounded event-history reads.
//	Preserve deterministic timeline ordering from the data layer.
//	Preserve DB-owned event timestamps.
//	Preserve explicit response DTOs.
//	Never expose event creation, update, deletion, restoration, or purge.
//	Never log or audit event-note contents.
//	Do not infer merchant ownership from untrusted request values.
//	Block deployment if this file breaks build, privileged access control,
//	event-history integrity, subscription traceability, or audit accountability.
package main

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/PiccoloMondoC/sdworkspace/sdbackend/internal/data"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

const (
	merchantProgramSubscriptionEventEntityType =
		"merchant_program_subscription_event"

	merchantProgramSubscriptionEventEntityTypeDescription =
		"Immutable merchant program subscription lifecycle event entity"

	actionReadMerchantProgramSubscriptionEvent =
		"read_merchant_program_subscription_event"

	actionListMerchantProgramSubscriptionEvents =
		"list_merchant_program_subscription_events"

	actionReadLatestMerchantProgramSubscriptionEvent =
		"read_latest_merchant_program_subscription_event"

	defaultMerchantProgramSubscriptionEventLimit = 50
	maxMerchantProgramSubscriptionEventLimit     = 100
)

// merchantProgramSubscriptionEventResponse is the stable HTTP presentation
// contract for one immutable subscription lifecycle event.
type merchantProgramSubscriptionEventResponse struct {
	ID uuid.UUID `json:"id"`

	SubscriptionID uuid.UUID `json:"subscription_id"`

	EventType data.MerchantProgramSubscriptionEventType `json:"event_type"`

	Note *string `json:"note,omitempty"`

	PerformedBy *uuid.UUID `json:"performed_by,omitempty"`

	CreatedAt time.Time `json:"created_at"`
}

// merchantProgramSubscriptionEventPagination reports the bounded timeline
// window applied to an event-list request.
type merchantProgramSubscriptionEventPagination struct {
	Limit int `json:"limit"`

	Offset int `json:"offset"`

	Count int `json:"count"`
}

// merchantProgramSubscriptionEventListResponse is the stable HTTP collection
// contract for one subscription's lifecycle timeline.
type merchantProgramSubscriptionEventListResponse struct {
	Events []merchantProgramSubscriptionEventResponse `json:"events"`

	Pagination merchantProgramSubscriptionEventPagination `json:"pagination"`
}

// newMerchantProgramSubscriptionEventResponse converts one canonical
// persistence model into its authorized HTTP representation.
func newMerchantProgramSubscriptionEventResponse(
	event *data.MerchantProgramSubscriptionEvent,
) merchantProgramSubscriptionEventResponse {
	return merchantProgramSubscriptionEventResponse{
		ID:             event.ID,
		SubscriptionID: event.SubscriptionID,
		EventType:      event.EventType,
		Note:           event.Note,
		PerformedBy:    event.PerformedBy,
		CreatedAt:      event.CreatedAt,
	}
}

// parseMerchantProgramSubscriptionEventID extracts and validates the canonical
// event UUID from the "eventID" route parameter.
func parseMerchantProgramSubscriptionEventID(
	r *http.Request,
) (uuid.UUID, error) {
	rawID := strings.TrimSpace(
		chi.URLParam(r, "eventID"),
	)
	if rawID == "" {
		return uuid.Nil, errors.New(
			"merchant program subscription event ID is required",
		)
	}

	eventID, err := uuid.Parse(rawID)
	if err != nil || eventID == uuid.Nil {
		return uuid.Nil, errors.New(
			"invalid merchant program subscription event ID",
		)
	}

	return eventID, nil
}

// parseMerchantProgramSubscriptionEventPagination parses and validates the
// bounded offset-pagination window for subscription event timelines.
func parseMerchantProgramSubscriptionEventPagination(
	r *http.Request,
) (int, int, error) {
	limit := defaultMerchantProgramSubscriptionEventLimit
	offset := 0

	rawLimit := strings.TrimSpace(
		r.URL.Query().Get("limit"),
	)
	if rawLimit != "" {
		parsedLimit, err := strconv.Atoi(rawLimit)
		if err != nil {
			return 0, 0, errors.New(
				"limit must be an integer",
			)
		}

		limit = parsedLimit
	}

	rawOffset := strings.TrimSpace(
		r.URL.Query().Get("offset"),
	)
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
		limit > maxMerchantProgramSubscriptionEventLimit {
		return 0, 0, errors.New(
			"limit must be between 1 and 100",
		)
	}

	if offset < 0 {
		return 0, 0, errors.New(
			"offset must be non-negative",
		)
	}

	return limit, offset, nil
}

// getMerchantProgramSubscriptionForEventHistory verifies that the canonical,
// non-deleted subscription exists before its event timeline is returned.
//
// Event presence must not be used as proof that the canonical subscription
// exists. This also preserves the distinction between an existing
// subscription with no events and a nonexistent subscription.
func (app *Application) getMerchantProgramSubscriptionForEventHistory(
	ctx context.Context,
	subscriptionID uuid.UUID,
) (*data.MerchantProgramSubscription, error) {
	return app.Models.
		MerchantProgramSubscription.
		GetByID(
			ctx,
			subscriptionID,
		)
}

// auditMerchantProgramSubscriptionEvent records one governance audit entry
// without allowing an audit failure to erase a successfully completed read.
//
// Event notes must never be included in action descriptions, entity
// identifiers, logs, or audit metadata.
func (app *Application) auditMerchantProgramSubscriptionEvent(
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
		merchantProgramSubscriptionEventEntityType,
		merchantProgramSubscriptionEventEntityTypeDescription,
		entityID,
	)
}

// GetMerchantProgramSubscriptionEventByIDHandler retrieves one immutable
// merchant program subscription lifecycle event.
//
// This endpoint is privileged and must not be exposed publicly or granted to
// merchant actors until a canonical merchant-account ownership resolver is
// available and applied consistently across the subscription domain.
func (app *Application) GetMerchantProgramSubscriptionEventByIDHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	logger := app.Logger.
		GetLoggerWithContext(r).
		WithFunctionName(
			"GetMerchantProgramSubscriptionEventByIDHandler",
		)

	ctx, cancel := context.WithTimeout(
		r.Context(),
		cfgTimeout,
	)
	defer cancel()

	if !app.HasPermission(
		ctx,
		actionReadMerchantProgramSubscriptionEvent,
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

	eventID, err :=
		parseMerchantProgramSubscriptionEventID(r)
	if err != nil {
		app.respondWithError(
			w,
			err,
			http.StatusBadRequest,
		)
		return
	}

	event, err :=
		app.Models.
			MerchantProgramSubscriptionEvent.
			GetByID(
				ctx,
				eventID,
			)
	if err != nil {
		logger.Error(
			"Get merchant program subscription event failed",
			"event_id",
			eventID,
			"error",
			err,
		)

		app.respondWithError(
			w,
			errors.New(
				"failed to retrieve merchant program subscription event",
			),
			http.StatusInternalServerError,
		)
		return
	}

	if event == nil {
		app.respondWithError(
			w,
			errors.New(
				"merchant program subscription event not found",
			),
			http.StatusNotFound,
		)
		return
	}

	if err := app.auditMerchantProgramSubscriptionEvent(
		ctx,
		userID,
		actionReadMerchantProgramSubscriptionEvent,
		"Read a merchant program subscription lifecycle event",
		event.ID.String(),
	); err != nil {
		logger.Warn(
			"Merchant program subscription event retrieved but audit recording failed",
			"event_id",
			event.ID,
			"subscription_id",
			event.SubscriptionID,
			"user_id",
			userID,
			"error",
			err,
		)

		app.respondWithJSON(
			w,
			http.StatusPartialContent,
			jsonResponse{
				Error: false,
				Message: "Merchant program subscription event " +
					"retrieved, but audit logging failed",
				Data: newMerchantProgramSubscriptionEventResponse(
					event,
				),
			},
		)
		return
	}

	logger.Info(
		"Merchant program subscription event retrieved",
		"event_id",
		event.ID,
		"subscription_id",
		event.SubscriptionID,
		"event_type",
		event.EventType,
		"user_id",
		userID,
	)

	app.respondWithJSON(
		w,
		http.StatusOK,
		jsonResponse{
			Error: false,
			Message: "Merchant program subscription event " +
				"retrieved successfully",
			Data: newMerchantProgramSubscriptionEventResponse(
				event,
			),
		},
	)
}

// ListMerchantProgramSubscriptionEventsHandler retrieves the bounded,
// deterministic lifecycle timeline for one canonical subscription.
//
// An existing subscription with no recorded events returns an empty collection.
// A nonexistent or soft-deleted subscription returns not found under the
// current canonical subscription model contract.
func (app *Application) ListMerchantProgramSubscriptionEventsHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	logger := app.Logger.
		GetLoggerWithContext(r).
		WithFunctionName(
			"ListMerchantProgramSubscriptionEventsHandler",
		)

	ctx, cancel := context.WithTimeout(
		r.Context(),
		cfgTimeout,
	)
	defer cancel()

	if !app.HasPermission(
		ctx,
		actionListMerchantProgramSubscriptionEvents,
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
		parseMerchantProgramSubscriptionEventPagination(r)
	if err != nil {
		app.respondWithError(
			w,
			err,
			http.StatusBadRequest,
		)
		return
	}

	subscription, err :=
		app.getMerchantProgramSubscriptionForEventHistory(
			ctx,
			subscriptionID,
		)
	if err != nil {
		logger.Error(
			"Get subscription for lifecycle-event timeline failed",
			"subscription_id",
			subscriptionID,
			"error",
			err,
		)

		app.respondWithError(
			w,
			errors.New(
				"failed to retrieve merchant program subscription events",
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

	rawEventType := strings.TrimSpace(
		r.URL.Query().Get("event_type"),
	)

	var events []*data.MerchantProgramSubscriptionEvent

	if rawEventType == "" {
		events, err =
			app.Models.
				MerchantProgramSubscriptionEvent.
				ListBySubscriptionID(
					ctx,
					subscriptionID,
					limit,
					offset,
				)
	} else {
		eventType :=
			data.NormalizeMerchantProgramSubscriptionEventType(
				data.MerchantProgramSubscriptionEventType(
					rawEventType,
				),
			)

		if !data.IsValidMerchantProgramSubscriptionEventType(
			eventType,
		) {
			app.respondWithError(
				w,
				errors.New(
					"invalid event_type query parameter",
				),
				http.StatusBadRequest,
			)
			return
		}

		events, err =
			app.Models.
				MerchantProgramSubscriptionEvent.
				ListBySubscriptionIDAndType(
					ctx,
					subscriptionID,
					eventType,
					limit,
					offset,
				)
	}

	if err != nil {
		logger.Error(
			"List merchant program subscription events failed",
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
				"failed to retrieve merchant program subscription events",
			),
			http.StatusInternalServerError,
		)
		return
	}

	responseEvents :=
		make(
			[]merchantProgramSubscriptionEventResponse,
			0,
			len(events),
		)

	for _, event := range events {
		if event == nil {
			continue
		}

		responseEvents = append(
			responseEvents,
			newMerchantProgramSubscriptionEventResponse(
				event,
			),
		)
	}

	response := merchantProgramSubscriptionEventListResponse{
		Events: responseEvents,
		Pagination: merchantProgramSubscriptionEventPagination{
			Limit:  limit,
			Offset: offset,
			Count:  len(responseEvents),
		},
	}

	if err := app.auditMerchantProgramSubscriptionEvent(
		ctx,
		userID,
		actionListMerchantProgramSubscriptionEvents,
		"List merchant program subscription lifecycle events",
		subscriptionID.String(),
	); err != nil {
		logger.Warn(
			"Merchant program subscription events retrieved but audit recording failed",
			"subscription_id",
			subscriptionID,
			"user_id",
			userID,
			"error",
			err,
		)

		app.respondWithJSON(
			w,
			http.StatusPartialContent,
			jsonResponse{
				Error: false,
				Message: "Merchant program subscription events " +
					"retrieved, but audit logging failed",
				Data: response,
			},
		)
		return
	}

	logger.Info(
		"Merchant program subscription events retrieved",
		"subscription_id",
		subscriptionID,
		"limit",
		limit,
		"offset",
		offset,
		"result_count",
		len(responseEvents),
		"user_id",
		userID,
	)

	app.respondWithJSON(
		w,
		http.StatusOK,
		jsonResponse{
			Error: false,
			Message: "Merchant program subscription events " +
				"retrieved successfully",
			Data: response,
		},
	)
}

// GetLatestMerchantProgramSubscriptionEventHandler retrieves the latest
// persisted lifecycle event for one canonical subscription.
//
// The handler never synthesizes history from the subscription's current
// status. A subscription with no persisted events returns not found.
func (app *Application) GetLatestMerchantProgramSubscriptionEventHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	logger := app.Logger.
		GetLoggerWithContext(r).
		WithFunctionName(
			"GetLatestMerchantProgramSubscriptionEventHandler",
		)

	ctx, cancel := context.WithTimeout(
		r.Context(),
		cfgTimeout,
	)
	defer cancel()

	if !app.HasPermission(
		ctx,
		actionListMerchantProgramSubscriptionEvents,
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
		app.getMerchantProgramSubscriptionForEventHistory(
			ctx,
			subscriptionID,
		)
	if err != nil {
		logger.Error(
			"Get subscription for latest lifecycle event failed",
			"subscription_id",
			subscriptionID,
			"error",
			err,
		)

		app.respondWithError(
			w,
			errors.New(
				"failed to retrieve latest merchant program subscription event",
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

	event, err :=
		app.Models.
			MerchantProgramSubscriptionEvent.
			GetLatestBySubscriptionID(
				ctx,
				subscriptionID,
			)
	if err != nil {
		logger.Error(
			"Get latest merchant program subscription event failed",
			"subscription_id",
			subscriptionID,
			"error",
			err,
		)

		app.respondWithError(
			w,
			errors.New(
				"failed to retrieve latest merchant program subscription event",
			),
			http.StatusInternalServerError,
		)
		return
	}

	if event == nil {
		app.respondWithError(
			w,
			errors.New(
				"no merchant program subscription events found",
			),
			http.StatusNotFound,
		)
		return
	}

	response :=
		newMerchantProgramSubscriptionEventResponse(
			event,
		)

	if err := app.auditMerchantProgramSubscriptionEvent(
		ctx,
		userID,
		actionReadLatestMerchantProgramSubscriptionEvent,
		"Read the latest merchant program subscription lifecycle event",
		event.ID.String(),
	); err != nil {
		logger.Warn(
			"Latest merchant program subscription event retrieved but audit recording failed",
			"event_id",
			event.ID,
			"subscription_id",
			subscriptionID,
			"user_id",
			userID,
			"error",
			err,
		)

		app.respondWithJSON(
			w,
			http.StatusPartialContent,
			jsonResponse{
				Error: false,
				Message: "Latest merchant program subscription event " +
					"retrieved, but audit logging failed",
				Data: response,
			},
		)
		return
	}

	logger.Info(
		"Latest merchant program subscription event retrieved",
		"event_id",
		event.ID,
		"subscription_id",
		subscriptionID,
		"event_type",
		event.EventType,
		"user_id",
		userID,
	)

	app.respondWithJSON(
		w,
		http.StatusOK,
		jsonResponse{
			Error: false,
			Message: "Latest merchant program subscription event " +
				"retrieved successfully",
			Data: response,
		},
	)
}