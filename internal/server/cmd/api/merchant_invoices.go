// Package main provides HTTP handlers for privileged merchant-invoice
// commercial-obligation history reads.
//
// sdworkspace/sdbackend/internal/server/cmd/api/merchant_invoices.go
//
// GTM:
//
//	Layer: 2.4.b Commerce Architecture / Monetization Layer
//	Release Class: SPINE
//	Reason:
//	  merchant_invoices is the canonical durable statement of a merchant's
//	  commercial obligation to Sagrenti. It sits downstream of fee calculation
//	  and platform-credit application and upstream of payment collection.
//
// Domain Boundary:
//
//	An invoice records an obligation snapshot and its settlement lifecycle.
//	This handler exposes bounded privileged reads for administration, support,
//	audit, and reconciliation.
//
//	It does not calculate fees, allocate Platform Credits, determine invoice
//	generation policy, choose currency, determine payment terms, execute payment,
//	or decide collection policy.
//
// Mutation Boundary:
//
//	No merchant-invoice mutation is exposed through HTTP at this handler-layer
//	stage.
//
//	InsertDraft, UpdateDraftFinancials, Issue, IssueDueNow, SettleZeroBalance,
//	MarkOverdue, and Void are Commerce lifecycle capabilities whose HTTP use
//	requires an approved service-backed orchestration boundary.
//
//	ApplyPaymentTx is transaction-only and must be composed atomically with the
//	authoritative merchant_payments write. It must never be exposed as a direct
//	invoice mutation endpoint.
//
// Scheduler Boundary:
//
//	ListIssuedDueForOverdueProcessing is an internal work-processing capability.
//	It is not an Admin Console browsing endpoint. Administrative visibility into
//	overdue invoices is provided by merchant-scoped status listing.
//
// Handler Boundary:
//
//	The operations exposed here are business-logic-free privileged reads whose
//	persistence semantics belong to MerchantInvoiceModel. They call the model
//	directly, consistent with the established Commerce historical-read boundary.
//
// Authorization:
//
//	This is privileged platform commercial history. No route in this file is
//	merchant self-service. Merchant-role access must not be added without a
//	canonical merchant ownership/delegation boundary and an explicitly approved
//	merchant-facing API surface.
//
// Monetary Representation:
//
//	subtotal_amount, adjustment_amount, total_amount, and amount_paid remain
//	exact decimal strings. This handler performs no floating-point conversion or
//	monetary arithmetic.
//
// SPINE Rule:
//
//	Keep compiling.
//	Keep production-ready.
//	Preserve exact decimal-string monetary representation.
//	Preserve deterministic bounded reads ordered by created_at DESC, id DESC.
//	Preserve strict privileged authorization on every route.
//	Preserve audit accountability for every successful privileged read.
//	Never expose generic update, delete, restore, payment application, or
//	lifecycle mutation through this handler without an approved service boundary.
//	Never place invoice monetary values in audit metadata.
//	Never implement commercial or operational policy in this file.
//	Block deployment if this file breaks authorization, monetary integrity,
//	audit accountability, historical integrity, or pagination determinism.
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

// -----------------------------------------------------------------------------
// Permission, action, and entity vocabulary
// -----------------------------------------------------------------------------

const (
	merchantInvoiceEntityType = "merchant_invoice"

	merchantInvoiceEntityTypeDescription = "Canonical durable merchant commercial-obligation and settlement-lifecycle history"

	actionReadMerchantInvoice = "read_merchant_invoice"

	actionListMerchantInvoices = "list_merchant_invoices"

	merchantInvoiceDefaultListLimit = 20
	merchantInvoiceMaximumListLimit = 100
)

// -----------------------------------------------------------------------------
// Error translation
// -----------------------------------------------------------------------------

// merchantInvoiceHTTPStatus translates exported merchant-invoice domain errors
// into stable HTTP status codes. Internal persistence diagnostics are not part
// of the public API contract.
func merchantInvoiceHTTPStatus(err error) int {
	if err == nil {
		return http.StatusOK
	}

	switch {
	case errors.Is(err, data.ErrMerchantInvoiceInvalidInput):
		return http.StatusBadRequest

	default:
		return http.StatusInternalServerError
	}
}

// respondWithMerchantInvoiceError prevents persistence, constraint, driver, and
// wrapped diagnostic details from becoming public API error text.
func (app *Application) respondWithMerchantInvoiceError(
	w http.ResponseWriter,
	err error,
) {
	status := merchantInvoiceHTTPStatus(err)

	switch status {
	case http.StatusBadRequest:
		app.respondWithError(
			w,
			errors.New("invalid merchant invoice request"),
			status,
		)

	default:
		app.respondWithError(
			w,
			errors.New("failed to process merchant invoice request"),
			http.StatusInternalServerError,
		)
	}
}

// -----------------------------------------------------------------------------
// Path and query parsing
// -----------------------------------------------------------------------------

func parseMerchantInvoiceID(
	r *http.Request,
) (uuid.UUID, error) {
	raw := strings.TrimSpace(
		chi.URLParam(
			r,
			"merchantInvoiceID",
		),
	)

	if raw == "" {
		return uuid.Nil, errors.New(
			"merchant invoice ID is required",
		)
	}

	id, err := uuid.Parse(raw)
	if err != nil || id == uuid.Nil {
		return uuid.Nil, errors.New(
			"invalid merchant invoice ID",
		)
	}

	return id, nil
}

// parseMerchantInvoiceNumberQueryParam obtains the lookup identity without
// duplicating the data layer's canonical invoice-number validation.
//
// Invoice numbers are not constrained by the domain contract to URI-segment-safe
// characters, so canonical lookup uses a query parameter rather than embedding
// the value in the route path.
func parseMerchantInvoiceNumberQueryParam(
	r *http.Request,
) (string, error) {
	raw := strings.TrimSpace(
		r.URL.Query().Get("invoice_number"),
	)

	if raw == "" {
		return "", errors.New(
			"invoice_number is required",
		)
	}

	return raw, nil
}

func parseMerchantInvoiceStatusParam(
	r *http.Request,
) (data.MerchantInvoiceStatus, error) {
	raw := strings.TrimSpace(
		r.URL.Query().Get("status"),
	)

	if raw == "" {
		return "", errors.New(
			"status is required",
		)
	}

	status :=
		data.NormalizeMerchantInvoiceStatus(
			data.MerchantInvoiceStatus(raw),
		)

	if !data.IsValidMerchantInvoiceStatus(status) {
		return "", errors.New(
			"status must be one of: draft, issued, partially_paid, paid, overdue, void",
		)
	}

	return status, nil
}

func parseMerchantInvoiceLimit(
	r *http.Request,
) (int, error) {
	raw := strings.TrimSpace(
		r.URL.Query().Get("limit"),
	)

	if raw == "" {
		return merchantInvoiceDefaultListLimit, nil
	}

	limit, err := strconv.Atoi(raw)
	if err != nil {
		return 0, errors.New(
			"limit must be an integer",
		)
	}

	if limit < 1 ||
		limit > merchantInvoiceMaximumListLimit {
		return 0, fmt.Errorf(
			"limit must be between 1 and %d",
			merchantInvoiceMaximumListLimit,
		)
	}

	return limit, nil
}

func parseMerchantInvoiceListCursor(
	r *http.Request,
) (*time.Time, *uuid.UUID, error) {
	rawCreatedAt := strings.TrimSpace(
		r.URL.Query().Get("before_created_at"),
	)

	rawID := strings.TrimSpace(
		r.URL.Query().Get("before_id"),
	)

	if rawCreatedAt == "" && rawID == "" {
		return nil, nil, nil
	}

	if rawCreatedAt == "" || rawID == "" {
		return nil, nil, errors.New(
			"before_created_at and before_id must be supplied together",
		)
	}

	createdAt, err :=
		time.Parse(
			time.RFC3339,
			rawCreatedAt,
		)
	if err != nil {
		return nil, nil, errors.New(
			"before_created_at must be a valid RFC3339 timestamp",
		)
	}

	id, err := uuid.Parse(rawID)
	if err != nil || id == uuid.Nil {
		return nil, nil, errors.New(
			"before_id must be a valid non-nil UUID",
		)
	}

	createdAtUTC := createdAt.UTC()

	return &createdAtUTC, &id, nil
}

// -----------------------------------------------------------------------------
// Stable response DTOs
// -----------------------------------------------------------------------------

// merchantInvoiceResponse is the stable privileged HTTP representation of one
// merchant-invoice header.
//
// Monetary fields remain exact canonical decimal strings. No derived monetary
// field is manufactured at this boundary.
type merchantInvoiceResponse struct {
	ID uuid.UUID `json:"id"`

	MerchantID uuid.UUID `json:"merchant_id"`

	InvoiceNumber string `json:"invoice_number"`

	InvoiceStatus data.MerchantInvoiceStatus `json:"invoice_status"`

	SubtotalAmount string `json:"subtotal_amount"`

	AdjustmentAmount string `json:"adjustment_amount"`

	TotalAmount string `json:"total_amount"`

	AmountPaid string `json:"amount_paid"`

	Currency string `json:"currency"`

	IssuedAt *time.Time `json:"issued_at,omitempty"`

	DueAt *time.Time `json:"due_at,omitempty"`

	PaidAt *time.Time `json:"paid_at,omitempty"`

	VoidedAt *time.Time `json:"voided_at,omitempty"`

	CreatedAt time.Time `json:"created_at"`

	UpdatedAt time.Time `json:"updated_at"`
}

func newMerchantInvoiceResponse(
	invoice *data.MerchantInvoice,
) merchantInvoiceResponse {
	return merchantInvoiceResponse{
		ID:               invoice.ID,
		MerchantID:       invoice.MerchantID,
		InvoiceNumber:    invoice.InvoiceNumber,
		InvoiceStatus:    invoice.InvoiceStatus,
		SubtotalAmount:   invoice.SubtotalAmount,
		AdjustmentAmount: invoice.AdjustmentAmount,
		TotalAmount:      invoice.TotalAmount,
		AmountPaid:       invoice.AmountPaid,
		Currency:         invoice.Currency,
		IssuedAt:         invoice.IssuedAt,
		DueAt:            invoice.DueAt,
		PaidAt:           invoice.PaidAt,
		VoidedAt:         invoice.VoidedAt,
		CreatedAt:        invoice.CreatedAt,
		UpdatedAt:        invoice.UpdatedAt,
	}
}

func newMerchantInvoiceResponseList(
	invoices []*data.MerchantInvoice,
) []merchantInvoiceResponse {
	out := make(
		[]merchantInvoiceResponse,
		0,
		len(invoices),
	)

	for _, invoice := range invoices {
		if invoice == nil {
			continue
		}

		out = append(
			out,
			newMerchantInvoiceResponse(invoice),
		)
	}

	return out
}

type merchantInvoiceListPagination struct {
	Limit int `json:"limit"`

	Count int `json:"count"`

	NextBeforeCreatedAt *time.Time `json:"next_before_created_at,omitempty"`

	NextBeforeID *uuid.UUID `json:"next_before_id,omitempty"`
}

type merchantInvoiceListResponse struct {
	Invoices []merchantInvoiceResponse `json:"invoices"`

	Pagination merchantInvoiceListPagination `json:"pagination"`
}

func newMerchantInvoiceListResponse(
	invoices []*data.MerchantInvoice,
	limit int,
) merchantInvoiceListResponse {
	responseInvoices :=
		newMerchantInvoiceResponseList(
			invoices,
		)

	pagination := merchantInvoiceListPagination{
		Limit: limit,
		Count: len(responseInvoices),
	}

	// The underlying model returns at most limit rows. Returning a cursor on
	// every non-empty page is deterministic; exhaustion is discovered by one
	// final bounded request returning an empty page.
	if len(responseInvoices) > 0 {
		last := responseInvoices[len(responseInvoices)-1]

		createdAt := last.CreatedAt.UTC()
		id := last.ID

		pagination.NextBeforeCreatedAt =
			&createdAt

		pagination.NextBeforeID =
			&id
	}

	return merchantInvoiceListResponse{
		Invoices:   responseInvoices,
		Pagination: pagination,
	}
}

// -----------------------------------------------------------------------------
// Audit
// -----------------------------------------------------------------------------

func (app *Application) auditMerchantInvoiceRead(
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
		merchantInvoiceEntityType,
		merchantInvoiceEntityTypeDescription,
		entityID,
	)
}

// -----------------------------------------------------------------------------
// Canonical ID read
// -----------------------------------------------------------------------------

func (app *Application) GetMerchantInvoiceByIDHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	logger := app.Logger.
		GetLoggerWithContext(r).
		WithFunctionName(
			"GetMerchantInvoiceByIDHandler",
		)

	ctx, cancel :=
		context.WithTimeout(
			r.Context(),
			cfgTimeout,
		)
	defer cancel()

	if !app.HasPermission(
		ctx,
		actionReadMerchantInvoice,
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
		parseMerchantInvoiceID(r)
	if err != nil {
		app.respondWithError(
			w,
			err,
			http.StatusBadRequest,
		)
		return
	}

	invoice, err :=
		app.Models.
			MerchantInvoice.
			GetByID(
				ctx,
				id,
			)
	if err != nil {
		logger.Error(
			"Get merchant invoice by ID failed",
			"merchant_invoice_id",
			id,
			"error",
			err,
		)

		app.respondWithMerchantInvoiceError(
			w,
			err,
		)
		return
	}

	if invoice == nil {
		app.respondWithError(
			w,
			errors.New(
				"merchant invoice not found",
			),
			http.StatusNotFound,
		)
		return
	}

	message :=
		"Merchant invoice retrieved successfully"

	if auditErr :=
		app.auditMerchantInvoiceRead(
			ctx,
			userID,
			actionReadMerchantInvoice,
			"Read a merchant invoice",
			invoice.ID.String(),
		); auditErr != nil {
		logger.Warn(
			"Merchant invoice retrieved but audit recording failed",
			"merchant_invoice_id",
			invoice.ID,
			"error",
			auditErr,
		)

		message =
			"Merchant invoice retrieved, but audit logging failed"
	}

	app.respondWithJSON(
		w,
		http.StatusOK,
		jsonResponse{
			Error:   false,
			Message: message,
			Data:    newMerchantInvoiceResponse(invoice),
		},
	)
}

// -----------------------------------------------------------------------------
// Canonical invoice-number read
// -----------------------------------------------------------------------------

func (app *Application) GetMerchantInvoiceByInvoiceNumberHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	logger := app.Logger.
		GetLoggerWithContext(r).
		WithFunctionName(
			"GetMerchantInvoiceByInvoiceNumberHandler",
		)

	ctx, cancel :=
		context.WithTimeout(
			r.Context(),
			cfgTimeout,
		)
	defer cancel()

	if !app.HasPermission(
		ctx,
		actionReadMerchantInvoice,
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

	invoiceNumber, err :=
		parseMerchantInvoiceNumberQueryParam(r)
	if err != nil {
		app.respondWithError(
			w,
			err,
			http.StatusBadRequest,
		)
		return
	}

	invoice, err :=
		app.Models.
			MerchantInvoice.
			GetByInvoiceNumber(
				ctx,
				invoiceNumber,
			)
	if err != nil {
		// Deliberately do not log the supplied invoice number.
		logger.Error(
			"Get merchant invoice by invoice number failed",
			"error",
			err,
		)

		app.respondWithMerchantInvoiceError(
			w,
			err,
		)
		return
	}

	if invoice == nil {
		app.respondWithError(
			w,
			errors.New(
				"merchant invoice not found",
			),
			http.StatusNotFound,
		)
		return
	}

	message :=
		"Merchant invoice retrieved successfully"

	if auditErr :=
		app.auditMerchantInvoiceRead(
			ctx,
			userID,
			actionReadMerchantInvoice,
			"Read a merchant invoice by invoice number",
			invoice.ID.String(),
		); auditErr != nil {
		logger.Warn(
			"Merchant invoice retrieved but audit recording failed",
			"merchant_invoice_id",
			invoice.ID,
			"error",
			auditErr,
		)

		message =
			"Merchant invoice retrieved, but audit logging failed"
	}

	app.respondWithJSON(
		w,
		http.StatusOK,
		jsonResponse{
			Error:   false,
			Message: message,
			Data:    newMerchantInvoiceResponse(invoice),
		},
	)
}

// -----------------------------------------------------------------------------
// Merchant-scoped history
// -----------------------------------------------------------------------------

func (app *Application) ListMerchantInvoicesByMerchantHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	logger := app.Logger.
		GetLoggerWithContext(r).
		WithFunctionName(
			"ListMerchantInvoicesByMerchantHandler",
		)

	ctx, cancel :=
		context.WithTimeout(
			r.Context(),
			cfgTimeout,
		)
	defer cancel()

	if !app.HasPermission(
		ctx,
		actionListMerchantInvoices,
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
		parseMerchantInvoiceLimit(r)
	if err != nil {
		app.respondWithError(
			w,
			err,
			http.StatusBadRequest,
		)
		return
	}

	beforeCreatedAt, beforeID, err :=
		parseMerchantInvoiceListCursor(r)
	if err != nil {
		app.respondWithError(
			w,
			err,
			http.StatusBadRequest,
		)
		return
	}

	invoices, err :=
		app.Models.
			MerchantInvoice.
			ListByMerchant(
				ctx,
				merchantID,
				limit,
				beforeCreatedAt,
				beforeID,
			)
	if err != nil {
		logger.Error(
			"List merchant invoices by merchant failed",
			"merchant_id",
			merchantID,
			"error",
			err,
		)

		app.respondWithMerchantInvoiceError(
			w,
			err,
		)
		return
	}

	response :=
		newMerchantInvoiceListResponse(
			invoices,
			limit,
		)

	message :=
		"Merchant invoices retrieved successfully"

	if auditErr :=
		app.auditMerchantInvoiceRead(
			ctx,
			userID,
			actionListMerchantInvoices,
			"List a merchant's invoice history",
			merchantID.String(),
		); auditErr != nil {
		logger.Warn(
			"Merchant invoices retrieved but audit recording failed",
			"merchant_id",
			merchantID,
			"error",
			auditErr,
		)

		message =
			"Merchant invoices retrieved, but audit logging failed"
	}

	logger.Info(
		"Merchant invoices retrieved",
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

func (app *Application) ListMerchantInvoicesByMerchantAndStatusHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	logger := app.Logger.
		GetLoggerWithContext(r).
		WithFunctionName(
			"ListMerchantInvoicesByMerchantAndStatusHandler",
		)

	ctx, cancel :=
		context.WithTimeout(
			r.Context(),
			cfgTimeout,
		)
	defer cancel()

	if !app.HasPermission(
		ctx,
		actionListMerchantInvoices,
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
		parseMerchantInvoiceStatusParam(r)
	if err != nil {
		app.respondWithError(
			w,
			err,
			http.StatusBadRequest,
		)
		return
	}

	limit, err :=
		parseMerchantInvoiceLimit(r)
	if err != nil {
		app.respondWithError(
			w,
			err,
			http.StatusBadRequest,
		)
		return
	}

	beforeCreatedAt, beforeID, err :=
		parseMerchantInvoiceListCursor(r)
	if err != nil {
		app.respondWithError(
			w,
			err,
			http.StatusBadRequest,
		)
		return
	}

	invoices, err :=
		app.Models.
			MerchantInvoice.
			ListByMerchantAndStatus(
				ctx,
				merchantID,
				status,
				limit,
				beforeCreatedAt,
				beforeID,
			)
	if err != nil {
		logger.Error(
			"List merchant invoices by merchant and status failed",
			"merchant_id",
			merchantID,
			"status",
			status,
			"error",
			err,
		)

		app.respondWithMerchantInvoiceError(
			w,
			err,
		)
		return
	}

	response :=
		newMerchantInvoiceListResponse(
			invoices,
			limit,
		)

	auditEntityID :=
		fmt.Sprintf(
			"merchant:%s:status:%s",
			merchantID,
			status,
		)

	message :=
		"Merchant invoices retrieved successfully"

	if auditErr :=
		app.auditMerchantInvoiceRead(
			ctx,
			userID,
			actionListMerchantInvoices,
			"List a merchant's invoice history by status",
			auditEntityID,
		); auditErr != nil {
		logger.Warn(
			"Merchant invoices retrieved but audit recording failed",
			"merchant_id",
			merchantID,
			"status",
			status,
			"error",
			auditErr,
		)

		message =
			"Merchant invoices retrieved, but audit logging failed"
	}

	logger.Info(
		"Merchant invoices retrieved by status",
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
