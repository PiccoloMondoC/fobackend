// Package main provides HTTP handlers for privileged merchant fee-calculation
// commercial-history and reconciliation reads.
//
// focodebase/fobackend/internal/server/cmd/api/merchant_fee_calculations.go
//
// GTM:
//
//	Layer: 2.4.b Commerce Architecture / Monetization Layer
//	Release Class: SPINE
//	Reason:
//	  merchant_fee_calculations is the canonical durable record of the monetary
//	  result produced for a merchant billable occurrence. It sits downstream of
//	  merchant_billable_events and merchant_program_fee_schedules and upstream of
//	  platform-credit applications, invoicing, and payment collection.
//
//	  This handler exposes bounded privileged reads for administration, support,
//	  audit, and reconciliation. It does not expose calculation creation or
//	  lifecycle mutation.
//
// Domain Boundary:
//
//	A merchant fee calculation records what was calculated. It is not a fee
//	schedule, invoice, invoice line, payment, payment attempt, billing account,
//	platform-credit application, commercial-policy store, or authorization
//	decision.
//
//	This handler does not calculate fees, select fee schedules, determine
//	commercial eligibility, apply credits, generate invoices, execute payments,
//	or decide collection policy.
//
// Mutation Boundary:
//
//	No merchant-fee-calculation mutation is exposed through HTTP.
//
//	Insert/InsertTx require service-owned merchant/source integrity validation.
//	Approve/Waive/Settle/Reverse and their transaction-aware forms remain
//	internal Commerce/service lifecycle capabilities.
//
//	The presence of a data-layer mutation method does not itself authorize an
//	HTTP mutation surface.
//
// Handler Boundary:
//
//	Operations in this file are business-logic-free privileged reads whose
//	persistence semantics belong to MerchantFeeCalculationModel. They therefore
//	call app.Models.MerchantFeeCalculation directly.
//
// Authorization:
//
//	This is privileged platform commercial history. No route in this file is
//	merchant self-service.
//
//	Merchant-role access must not be introduced unless a future route family has
//	canonical merchant ownership/delegation enforcement and an explicitly
//	approved merchant-facing requirement.
//
// Monetary Representation:
//
//	fee_rate, flat_fee_amount, and calculated_fee_amount remain decimal strings.
//	SQL NULL remains distinct from numeric zero.
//
// Lifecycle Representation:
//
//	Status and lifecycle timestamps exposed by this handler are Commerce-layer
//	calculation facts.
//
//	In particular, settled indicates the persisted Commerce lifecycle state of
//	the calculation. It is not a Merchant Payments provider-transaction status,
//	and this handler neither infers nor exposes payment-provider state. Service
//	orchestration owns the conditions and coordination required to enter that
//	lifecycle state.
//
// SPINE Rule:
//
//	Keep compiling.
//	Keep production-ready.
//	Preserve exact decimal-string monetary representation.
//	Preserve immutable calculation snapshots.
//	Preserve deterministic bounded reads.
//	Preserve calculated_at DESC, id DESC keyset ordering where supported.
//	Preserve strict privileged authorization.
//	Preserve audit accountability for successful privileged reads.
//	Never expose creation, generic update, deletion, restoration, or lifecycle
//	mutation without an independently approved service-backed boundary.
//	Never place monetary values or calculation_basis contents in audit metadata.
//	Never implement commercial or operational policy in this file.
//	Block deployment if this file breaks authorization, monetary precision,
//	audit accountability, calculation-history integrity, or read determinism.
package main

import (
	"context"
	"encoding/json"
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
	merchantFeeCalculationEntityType = "merchant_fee_calculation"

	merchantFeeCalculationEntityTypeDescription = "Canonical durable merchant fee calculation monetary-result history"

	actionReadMerchantFeeCalculation = "read_merchant_fee_calculation"

	actionListMerchantFeeCalculations = "list_merchant_fee_calculations"

	merchantFeeCalculationDefaultListLimit = 20
	merchantFeeCalculationMaximumListLimit = 100
)

// -----------------------------------------------------------------------------
// Error translation
// -----------------------------------------------------------------------------

func merchantFeeCalculationHTTPStatus(err error) int {
	if err == nil {
		return http.StatusOK
	}

	switch {
	case errors.Is(
		err,
		data.ErrMerchantFeeCalculationInvalidInput,
	):
		return http.StatusBadRequest

	default:
		return http.StatusInternalServerError
	}
}

func (app *Application) respondWithMerchantFeeCalculationError(
	w http.ResponseWriter,
	err error,
) {
	status := merchantFeeCalculationHTTPStatus(err)

	switch status {
	case http.StatusBadRequest:
		app.respondWithError(
			w,
			errors.New(
				"invalid merchant fee calculation request",
			),
			status,
		)

	default:
		app.respondWithError(
			w,
			errors.New(
				"failed to process merchant fee calculation request",
			),
			http.StatusInternalServerError,
		)
	}
}

// -----------------------------------------------------------------------------
// Path parameter parsing
// -----------------------------------------------------------------------------

func parseMerchantFeeCalculationID(
	r *http.Request,
) (uuid.UUID, error) {
	raw := strings.TrimSpace(
		chi.URLParam(
			r,
			"merchantFeeCalculationID",
		),
	)

	if raw == "" {
		return uuid.Nil, errors.New(
			"merchant fee calculation ID is required",
		)
	}

	id, err := uuid.Parse(raw)
	if err != nil || id == uuid.Nil {
		return uuid.Nil, errors.New(
			"invalid merchant fee calculation ID",
		)
	}

	return id, nil
}

func parseMerchantFeeCalculationBillableEventIDParam(
	r *http.Request,
) (uuid.UUID, error) {
	raw := strings.TrimSpace(
		chi.URLParam(
			r,
			"billableEventID",
		),
	)

	if raw == "" {
		return uuid.Nil, errors.New(
			"billable event ID is required",
		)
	}

	id, err := uuid.Parse(raw)
	if err != nil || id == uuid.Nil {
		return uuid.Nil, errors.New(
			"invalid billable event ID",
		)
	}

	return id, nil
}

func parseMerchantFeeCalculationFeeTypeIDParam(
	r *http.Request,
) (uuid.UUID, error) {
	raw := strings.TrimSpace(
		chi.URLParam(
			r,
			"feeTypeID",
		),
	)

	if raw == "" {
		return uuid.Nil, errors.New(
			"fee type ID is required",
		)
	}

	id, err := uuid.Parse(raw)
	if err != nil || id == uuid.Nil {
		return uuid.Nil, errors.New(
			"invalid fee type ID",
		)
	}

	return id, nil
}

// -----------------------------------------------------------------------------
// Query parsing
// -----------------------------------------------------------------------------

func parseMerchantFeeCalculationStatusParam(
	r *http.Request,
) (data.MerchantFeeCalculationStatus, error) {
	raw := strings.TrimSpace(
		r.URL.Query().Get("status"),
	)

	if raw == "" {
		return "", errors.New(
			"status is required",
		)
	}

	status :=
		data.NormalizeMerchantFeeCalculationStatus(
			data.MerchantFeeCalculationStatus(raw),
		)

	if !data.IsValidMerchantFeeCalculationStatus(
		status,
	) {
		return "", errors.New(
			"status must be one of: pending, approved, settled, waived, reversed",
		)
	}

	return status, nil
}

func parseMerchantFeeCalculationLimit(
	r *http.Request,
) (int, error) {
	raw := strings.TrimSpace(
		r.URL.Query().Get("limit"),
	)

	if raw == "" {
		return merchantFeeCalculationDefaultListLimit, nil
	}

	limit, err := strconv.Atoi(raw)
	if err != nil {
		return 0, errors.New(
			"limit must be an integer",
		)
	}

	if limit < 1 ||
		limit > merchantFeeCalculationMaximumListLimit {
		return 0, fmt.Errorf(
			"limit must be between 1 and %d",
			merchantFeeCalculationMaximumListLimit,
		)
	}

	return limit, nil
}

func parseMerchantFeeCalculationListCursor(
	r *http.Request,
) (*time.Time, *uuid.UUID, error) {
	rawCalculatedAt := strings.TrimSpace(
		r.URL.Query().Get(
			"before_calculated_at",
		),
	)

	rawID := strings.TrimSpace(
		r.URL.Query().Get(
			"before_id",
		),
	)

	if rawCalculatedAt == "" && rawID == "" {
		return nil, nil, nil
	}

	if rawCalculatedAt == "" || rawID == "" {
		return nil, nil, errors.New(
			"before_calculated_at and before_id must be supplied together",
		)
	}

	calculatedAt, err :=
		time.Parse(
			time.RFC3339,
			rawCalculatedAt,
		)
	if err != nil {
		return nil, nil, errors.New(
			"before_calculated_at must be a valid RFC3339 timestamp",
		)
	}

	id, err := uuid.Parse(rawID)
	if err != nil || id == uuid.Nil {
		return nil, nil, errors.New(
			"before_id must be a valid non-nil UUID",
		)
	}

	calculatedAtUTC := calculatedAt.UTC()

	return &calculatedAtUTC, &id, nil
}

// -----------------------------------------------------------------------------
// Stable response DTOs
// -----------------------------------------------------------------------------

type merchantFeeCalculationResponse struct {
	ID uuid.UUID `json:"id"`

	MerchantID uuid.UUID `json:"merchant_id"`

	BillableEventID uuid.UUID `json:"billable_event_id"`

	FeeScheduleID *uuid.UUID `json:"fee_schedule_id,omitempty"`

	FeeTypeID uuid.UUID `json:"fee_type_id"`

	CalculationMethod data.MerchantFeeCalculationMethod `json:"calculation_method"`

	FeeRate *string `json:"fee_rate,omitempty"`

	FlatFeeAmount *string `json:"flat_fee_amount,omitempty"`

	CalculatedFeeAmount string `json:"calculated_fee_amount"`

	Currency string `json:"currency"`

	CalculationBasis json.RawMessage `json:"calculation_basis"`

	CalculatedAt time.Time `json:"calculated_at"`

	// Status is the persisted Commerce-layer calculation lifecycle state.
	// A settled calculation does not itself represent payment-provider state.
	Status data.MerchantFeeCalculationStatus `json:"status"`

	ApprovedAt *time.Time `json:"approved_at,omitempty"`

	SettledAt *time.Time `json:"settled_at,omitempty"`

	WaivedAt *time.Time `json:"waived_at,omitempty"`

	ReversedAt *time.Time `json:"reversed_at,omitempty"`

	CreatedAt time.Time `json:"created_at"`

	UpdatedAt time.Time `json:"updated_at"`
}

func newMerchantFeeCalculationResponse(
	calc *data.MerchantFeeCalculation,
) merchantFeeCalculationResponse {
	return merchantFeeCalculationResponse{
		ID:                  calc.ID,
		MerchantID:          calc.MerchantID,
		BillableEventID:     calc.BillableEventID,
		FeeScheduleID:       calc.FeeScheduleID,
		FeeTypeID:           calc.FeeTypeID,
		CalculationMethod:   calc.CalculationMethod,
		FeeRate:             calc.FeeRate,
		FlatFeeAmount:       calc.FlatFeeAmount,
		CalculatedFeeAmount: calc.CalculatedFeeAmount,
		Currency:            calc.Currency,
		CalculationBasis:    calc.CalculationBasis,
		CalculatedAt:        calc.CalculatedAt,
		Status:              calc.Status,
		ApprovedAt:          calc.ApprovedAt,
		SettledAt:           calc.SettledAt,
		WaivedAt:            calc.WaivedAt,
		ReversedAt:          calc.ReversedAt,
		CreatedAt:           calc.CreatedAt,
		UpdatedAt:           calc.UpdatedAt,
	}
}

func newMerchantFeeCalculationResponseList(
	calculations []*data.MerchantFeeCalculation,
) []merchantFeeCalculationResponse {
	out := make(
		[]merchantFeeCalculationResponse,
		0,
		len(calculations),
	)

	for _, calc := range calculations {
		if calc == nil {
			continue
		}

		out = append(
			out,
			newMerchantFeeCalculationResponse(calc),
		)
	}

	return out
}

// merchantFeeCalculationHistoryResponse represents the bounded newest-first
// window exposed by ListByBillableEventID.
//
// The underlying data contract does not provide keyset continuation for this
// particular lookup, so this response deliberately does not manufacture a
// continuation cursor.
type merchantFeeCalculationHistoryResponse struct {
	Calculations []merchantFeeCalculationResponse `json:"calculations"`

	Limit int `json:"limit"`

	Count int `json:"count"`
}

type merchantFeeCalculationListPagination struct {
	Limit int `json:"limit"`

	Count int `json:"count"`

	NextBeforeCalculatedAt *time.Time `json:"next_before_calculated_at,omitempty"`

	NextBeforeID *uuid.UUID `json:"next_before_id,omitempty"`
}

type merchantFeeCalculationListResponse struct {
	Calculations []merchantFeeCalculationResponse `json:"calculations"`

	Pagination merchantFeeCalculationListPagination `json:"pagination"`
}

func newMerchantFeeCalculationListResponse(
	calculations []*data.MerchantFeeCalculation,
	limit int,
) merchantFeeCalculationListResponse {
	responseCalculations :=
		newMerchantFeeCalculationResponseList(
			calculations,
		)

	pagination :=
		merchantFeeCalculationListPagination{
			Limit: limit,
			Count: len(
				responseCalculations,
			),
		}

	// The data model returns at most limit rows. Returning the final row as the
	// continuation position is deterministic and permits the client to discover
	// exhaustion through one final bounded empty-page request.
	if len(responseCalculations) > 0 {
		last := responseCalculations[len(responseCalculations)-1]

		calculatedAt := last.CalculatedAt.UTC()
		id := last.ID

		pagination.NextBeforeCalculatedAt =
			&calculatedAt

		pagination.NextBeforeID =
			&id
	}

	return merchantFeeCalculationListResponse{
		Calculations: responseCalculations,
		Pagination:   pagination,
	}
}

// -----------------------------------------------------------------------------
// Audit
// -----------------------------------------------------------------------------

func (app *Application) auditMerchantFeeCalculationRead(
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
		merchantFeeCalculationEntityType,
		merchantFeeCalculationEntityTypeDescription,
		entityID,
	)
}

// -----------------------------------------------------------------------------
// Canonical ID read
// -----------------------------------------------------------------------------

func (app *Application) GetMerchantFeeCalculationByIDHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	logger := app.Logger.
		GetLoggerWithContext(r).
		WithFunctionName(
			"GetMerchantFeeCalculationByIDHandler",
		)

	ctx, cancel :=
		context.WithTimeout(
			r.Context(),
			cfgTimeout,
		)
	defer cancel()

	if !app.HasPermission(
		ctx,
		actionReadMerchantFeeCalculation,
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

	userID :=
		app.getUserIDFromContext(ctx)

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

	id, err :=
		parseMerchantFeeCalculationID(r)
	if err != nil {
		app.respondWithError(
			w,
			err,
			http.StatusBadRequest,
		)
		return
	}

	calc, err :=
		app.Models.
			MerchantFeeCalculation.
			GetByID(
				ctx,
				id,
			)
	if err != nil {
		logger.Error(
			"Get merchant fee calculation by ID failed",
			"merchant_fee_calculation_id",
			id,
			"error",
			err,
		)

		app.respondWithMerchantFeeCalculationError(
			w,
			err,
		)
		return
	}

	if calc == nil {
		app.respondWithError(
			w,
			errors.New(
				"merchant fee calculation not found",
			),
			http.StatusNotFound,
		)
		return
	}

	message :=
		"Merchant fee calculation retrieved successfully"

	if auditErr :=
		app.auditMerchantFeeCalculationRead(
			ctx,
			userID,
			actionReadMerchantFeeCalculation,
			"Read a merchant fee calculation",
			calc.ID.String(),
		); auditErr != nil {
		logger.Warn(
			"Merchant fee calculation retrieved but audit recording failed",
			"merchant_fee_calculation_id",
			calc.ID,
			"error",
			auditErr,
		)

		message =
			"Merchant fee calculation retrieved, but audit logging failed"
	}

	app.respondWithJSON(
		w,
		http.StatusOK,
		jsonResponse{
			Error:   false,
			Message: message,
			Data: newMerchantFeeCalculationResponse(
				calc,
			),
		},
	)
}

// -----------------------------------------------------------------------------
// Active source-identity read
// -----------------------------------------------------------------------------

func (app *Application) GetActiveMerchantFeeCalculationByBillableEventAndFeeTypeHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	logger := app.Logger.
		GetLoggerWithContext(r).
		WithFunctionName(
			"GetActiveMerchantFeeCalculationByBillableEventAndFeeTypeHandler",
		)

	ctx, cancel :=
		context.WithTimeout(
			r.Context(),
			cfgTimeout,
		)
	defer cancel()

	if !app.HasPermission(
		ctx,
		actionReadMerchantFeeCalculation,
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

	userID :=
		app.getUserIDFromContext(ctx)

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

	billableEventID, err :=
		parseMerchantFeeCalculationBillableEventIDParam(
			r,
		)
	if err != nil {
		app.respondWithError(
			w,
			err,
			http.StatusBadRequest,
		)
		return
	}

	feeTypeID, err :=
		parseMerchantFeeCalculationFeeTypeIDParam(
			r,
		)
	if err != nil {
		app.respondWithError(
			w,
			err,
			http.StatusBadRequest,
		)
		return
	}

	calc, err :=
		app.Models.
			MerchantFeeCalculation.
			GetActiveByBillableEventAndFeeType(
				ctx,
				billableEventID,
				feeTypeID,
			)
	if err != nil {
		logger.Error(
			"Get active merchant fee calculation failed",
			"billable_event_id",
			billableEventID,
			"fee_type_id",
			feeTypeID,
			"error",
			err,
		)

		app.respondWithMerchantFeeCalculationError(
			w,
			err,
		)
		return
	}

	if calc == nil {
		app.respondWithError(
			w,
			errors.New(
				"active merchant fee calculation not found",
			),
			http.StatusNotFound,
		)
		return
	}

	message :=
		"Merchant fee calculation retrieved successfully"

	if auditErr :=
		app.auditMerchantFeeCalculationRead(
			ctx,
			userID,
			actionReadMerchantFeeCalculation,
			"Read the active merchant fee calculation for a billable event and fee type",
			calc.ID.String(),
		); auditErr != nil {
		logger.Warn(
			"Merchant fee calculation retrieved but audit recording failed",
			"merchant_fee_calculation_id",
			calc.ID,
			"billable_event_id",
			billableEventID,
			"fee_type_id",
			feeTypeID,
			"error",
			auditErr,
		)

		message =
			"Merchant fee calculation retrieved, but audit logging failed"
	}

	app.respondWithJSON(
		w,
		http.StatusOK,
		jsonResponse{
			Error:   false,
			Message: message,
			Data: newMerchantFeeCalculationResponse(
				calc,
			),
		},
	)
}

// -----------------------------------------------------------------------------
// Billable-event-scoped bounded history
// -----------------------------------------------------------------------------

func (app *Application) ListMerchantFeeCalculationsByBillableEventIDHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	logger := app.Logger.
		GetLoggerWithContext(r).
		WithFunctionName(
			"ListMerchantFeeCalculationsByBillableEventIDHandler",
		)

	ctx, cancel :=
		context.WithTimeout(
			r.Context(),
			cfgTimeout,
		)
	defer cancel()

	if !app.HasPermission(
		ctx,
		actionListMerchantFeeCalculations,
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

	userID :=
		app.getUserIDFromContext(ctx)

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

	billableEventID, err :=
		parseMerchantFeeCalculationBillableEventIDParam(
			r,
		)
	if err != nil {
		app.respondWithError(
			w,
			err,
			http.StatusBadRequest,
		)
		return
	}

	limit, err :=
		parseMerchantFeeCalculationLimit(r)
	if err != nil {
		app.respondWithError(
			w,
			err,
			http.StatusBadRequest,
		)
		return
	}

	calculations, err :=
		app.Models.
			MerchantFeeCalculation.
			ListByBillableEventID(
				ctx,
				billableEventID,
				limit,
			)
	if err != nil {
		logger.Error(
			"List merchant fee calculations by billable event failed",
			"billable_event_id",
			billableEventID,
			"error",
			err,
		)

		app.respondWithMerchantFeeCalculationError(
			w,
			err,
		)
		return
	}

	responseCalculations :=
		newMerchantFeeCalculationResponseList(
			calculations,
		)

	response :=
		merchantFeeCalculationHistoryResponse{
			Calculations: responseCalculations,
			Limit:        limit,
			Count: len(
				responseCalculations,
			),
		}

	message :=
		"Merchant fee calculation history retrieved successfully"

	if auditErr :=
		app.auditMerchantFeeCalculationRead(
			ctx,
			userID,
			actionListMerchantFeeCalculations,
			"List bounded fee calculation history for a billable event",
			billableEventID.String(),
		); auditErr != nil {
		logger.Warn(
			"Merchant fee calculation history retrieved but audit recording failed",
			"billable_event_id",
			billableEventID,
			"error",
			auditErr,
		)

		message =
			"Merchant fee calculation history retrieved, but audit logging failed"
	}

	logger.Info(
		"Merchant fee calculation history retrieved",
		"billable_event_id",
		billableEventID,
		"result_count",
		response.Count,
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

// -----------------------------------------------------------------------------
// Merchant-scoped keyset timelines
// -----------------------------------------------------------------------------

func (app *Application) ListMerchantFeeCalculationsByMerchantHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	logger := app.Logger.
		GetLoggerWithContext(r).
		WithFunctionName(
			"ListMerchantFeeCalculationsByMerchantHandler",
		)

	ctx, cancel :=
		context.WithTimeout(
			r.Context(),
			cfgTimeout,
		)
	defer cancel()

	if !app.HasPermission(
		ctx,
		actionListMerchantFeeCalculations,
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

	userID :=
		app.getUserIDFromContext(ctx)

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
		parseMerchantFeeCalculationLimit(r)
	if err != nil {
		app.respondWithError(
			w,
			err,
			http.StatusBadRequest,
		)
		return
	}

	beforeCalculatedAt, beforeID, err :=
		parseMerchantFeeCalculationListCursor(r)
	if err != nil {
		app.respondWithError(
			w,
			err,
			http.StatusBadRequest,
		)
		return
	}

	calculations, err :=
		app.Models.
			MerchantFeeCalculation.
			ListByMerchant(
				ctx,
				merchantID,
				limit,
				beforeCalculatedAt,
				beforeID,
			)
	if err != nil {
		logger.Error(
			"List merchant fee calculations by merchant failed",
			"merchant_id",
			merchantID,
			"error",
			err,
		)

		app.respondWithMerchantFeeCalculationError(
			w,
			err,
		)
		return
	}

	response :=
		newMerchantFeeCalculationListResponse(
			calculations,
			limit,
		)

	message :=
		"Merchant fee calculations retrieved successfully"

	if auditErr :=
		app.auditMerchantFeeCalculationRead(
			ctx,
			userID,
			actionListMerchantFeeCalculations,
			"List a merchant's fee calculation timeline",
			merchantID.String(),
		); auditErr != nil {
		logger.Warn(
			"Merchant fee calculations retrieved but audit recording failed",
			"merchant_id",
			merchantID,
			"error",
			auditErr,
		)

		message =
			"Merchant fee calculations retrieved, but audit logging failed"
	}

	logger.Info(
		"Merchant fee calculations retrieved",
		"merchant_id",
		merchantID,
		"result_count",
		response.Pagination.Count,
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

func (app *Application) ListMerchantFeeCalculationsByMerchantAndStatusHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	logger := app.Logger.
		GetLoggerWithContext(r).
		WithFunctionName(
			"ListMerchantFeeCalculationsByMerchantAndStatusHandler",
		)

	ctx, cancel :=
		context.WithTimeout(
			r.Context(),
			cfgTimeout,
		)
	defer cancel()

	if !app.HasPermission(
		ctx,
		actionListMerchantFeeCalculations,
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

	userID :=
		app.getUserIDFromContext(ctx)

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
		parseMerchantFeeCalculationStatusParam(
			r,
		)
	if err != nil {
		app.respondWithError(
			w,
			err,
			http.StatusBadRequest,
		)
		return
	}

	limit, err :=
		parseMerchantFeeCalculationLimit(r)
	if err != nil {
		app.respondWithError(
			w,
			err,
			http.StatusBadRequest,
		)
		return
	}

	beforeCalculatedAt, beforeID, err :=
		parseMerchantFeeCalculationListCursor(
			r,
		)
	if err != nil {
		app.respondWithError(
			w,
			err,
			http.StatusBadRequest,
		)
		return
	}

	calculations, err :=
		app.Models.
			MerchantFeeCalculation.
			ListByMerchantAndStatus(
				ctx,
				merchantID,
				status,
				limit,
				beforeCalculatedAt,
				beforeID,
			)
	if err != nil {
		logger.Error(
			"List merchant fee calculations by merchant and status failed",
			"merchant_id",
			merchantID,
			"status",
			status,
			"error",
			err,
		)

		app.respondWithMerchantFeeCalculationError(
			w,
			err,
		)
		return
	}

	response :=
		newMerchantFeeCalculationListResponse(
			calculations,
			limit,
		)

	auditEntityID :=
		fmt.Sprintf(
			"merchant:%s:status:%s",
			merchantID,
			status,
		)

	message :=
		"Merchant fee calculations retrieved successfully"

	if auditErr :=
		app.auditMerchantFeeCalculationRead(
			ctx,
			userID,
			actionListMerchantFeeCalculations,
			"List a merchant's fee calculation timeline by status",
			auditEntityID,
		); auditErr != nil {
		logger.Warn(
			"Merchant fee calculations retrieved but audit recording failed",
			"merchant_id",
			merchantID,
			"status",
			status,
			"error",
			auditErr,
		)

		message =
			"Merchant fee calculations retrieved, but audit logging failed"
	}

	logger.Info(
		"Merchant fee calculations retrieved by status",
		"merchant_id",
		merchantID,
		"status",
		status,
		"result_count",
		response.Pagination.Count,
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
