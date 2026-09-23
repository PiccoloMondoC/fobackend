// Package main provides HTTP handlers for privileged merchant-invoice-item
// commercial-history and reconciliation reads.
//
// focodebase/fobackend/internal/server/cmd/api/merchant_invoice_items.go
//
// GTM:
//
//	Layer: 2.4.b Commerce Architecture / Monetization Layer
//	Release Class: SPINE
//	Reason:
//	  merchant_invoice_items is the normalized line-level bridge between
//	  merchant_fee_calculations and merchant_invoices. It preserves the
//	  auditable identity of the Sagrenti product/service charge represented by
//	  an invoice line while leaving fee calculation, monetary effects, invoice
//	  composition, invoice lifecycle, payment, and settlement in their owning
//	  domains.
//
// Domain Boundary:
//
//	A merchant invoice item identifies one Sagrenti product/service charge
//	represented on an invoice as:
//
//		quantity x unit_amount = line_amount
//
//	It is not:
//
//	  - a fee calculator or fee schedule;
//	  - a Platform Credit application or Platform Credit ledger;
//	  - a rebate, discount, tax, surcharge, or other monetary-effect record;
//	  - a payment, payment attempt, or settlement adjustment;
//	  - an invoice header or invoice-lifecycle substitute; or
//	  - a container for separately billing consumer engagement actions.
//
//	This handler exposes bounded privileged historical/reconciliation reads.
//	It does not calculate fees, determine billability, apply credits, compose
//	invoice totals, determine invoice eligibility, choose commercial policy,
//	execute payment, or determine settlement.
//
// Provenance:
//
//	fee_calculation_id is the sole direct upstream provenance reference exposed
//	by this domain. billable_event_id is deliberately not duplicated here.
//
//	The database UNIQUE(fee_calculation_id) constraint remains the authoritative
//	concurrency-safe one-invoice-item-per-fee-calculation boundary. This handler
//	must never attempt to replace that invariant with SELECT-before-INSERT logic.
//
// Monetary Representation:
//
//	description, quantity, unit_amount, and line_amount preserve the persisted
//	gross product/service charge snapshot.
//
//	quantity, unit_amount, and line_amount remain exact decimal strings. This
//	handler performs no floating-point conversion and no monetary arithmetic.
//
//	Platform Credits, rebates, discounts, taxes, surcharges, payments, and
//	settlement adjustments are not netted into these values.
//
// Currency:
//
//	merchant_invoice_items owns no currency field. Currency belongs to the
//	parent merchant invoice and must not be duplicated or inferred here.
//
// Mutation Boundary:
//
//	No merchant-invoice-item mutation is exposed through HTTP at this
//	handler-layer stage.
//
//	InsertDraftLineTx and RemoveDraftLineTx are transaction-only Commerce
//	composition capabilities. They row-lock the parent invoice and require
//	affected invoice-composition reconciliation to occur in the same transaction
//	before commit.
//
//	SumGrossLineAmountByInvoiceIDTx is transaction-only composition
//	infrastructure. It is not MISA items_subtotal, pre-tax total, or invoice
//	total and is not an administrative browsing endpoint.
//
// Handler Boundary:
//
//	The reads exposed here are business-logic-free persistence reads whose
//	complete persistence semantics belong to MerchantInvoiceItemModel. They call
//	the model directly, consistent with merchant_invoices.go,
//	merchant_fee_calculations.go, merchant_billable_events.go, and
//	merchant_platform_credit_applications.go.
//
// Authorization:
//
//	This is privileged platform commercial history.
//
//	No route in this file is merchant self-service. Merchant-role access must
//	not be introduced without a canonical merchant ownership/delegation boundary
//	and an explicitly approved merchant-facing API surface.
//
// Pagination:
//
//	Invoice-scoped listing preserves the data model's forward keyset contract:
//
//		created_at ASC, id ASC
//
//	after_created_at and after_id must be supplied together or omitted together.
//	The handler does not reorder results and does not substitute offset
//	pagination.
//
// Audit:
//
//	Successful privileged reads are governance-audited using identifiers and
//	bounded operational metadata only. quantity, unit_amount, line_amount, and
//	other monetary values must never be placed in audit metadata.
//
// SPINE Rule:
//
//	Keep compiling.
//	Keep production-ready.
//	Preserve exact decimal-string monetary representation.
//	Preserve fee_calculation_id as the sole direct provenance reference.
//	Preserve deterministic forward keyset pagination.
//	Preserve strict privileged authorization on every route.
//	Preserve audit accountability for every successful privileged read.
//	Never expose invoice-item mutation or composition helpers through this
//	handler without an approved service-orchestration boundary.
//	Never place monetary values in audit metadata.
//	Never implement commercial or operational policy in this file.
//	Block deployment if this file breaks authorization, monetary integrity,
//	provenance, audit accountability, historical integrity, or pagination
//	determinism.
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
	merchantInvoiceItemEntityType = "merchant_invoice_item"

	merchantInvoiceItemEntityTypeDescription = "Canonical durable merchant invoice-line history"

	actionReadMerchantInvoiceItem = "read_merchant_invoice_item"

	actionListMerchantInvoiceItems = "list_merchant_invoice_items"

	merchantInvoiceItemDefaultListLimit = 20
	merchantInvoiceItemMaximumListLimit = 100
)

// -----------------------------------------------------------------------------
// Error translation
// -----------------------------------------------------------------------------

// merchantInvoiceItemHTTPStatus translates only domain errors reachable from
// the read surface exposed by this handler.
//
// GetByID and GetByFeeCalculationID represent absence as nil, nil, so 404 is
// handled explicitly by those handlers rather than through an invented
// not-found sentinel contract.
//
// Mutation-only classifications such as duplicate fee calculation,
// invoice-not-draft, and missing write-path parent references deliberately do
// not appear here because this handler exposes no mutation capability.
func merchantInvoiceItemHTTPStatus(err error) int {
	if err == nil {
		return http.StatusOK
	}

	switch {
	case errors.Is(
		err,
		data.ErrMerchantInvoiceItemInvalidInput,
	):
		return http.StatusBadRequest

	case errors.Is(
		err,
		data.ErrMerchantInvoiceItemInvalidState,
	):
		// Invalid persisted state is a server/data-integrity failure,
		// never a client-input classification.
		return http.StatusInternalServerError

	default:
		return http.StatusInternalServerError
	}
}

// respondWithMerchantInvoiceItemError prevents persistence, constraint, driver,
// and wrapped diagnostic details from becoming part of the public API contract.
func (app *Application) respondWithMerchantInvoiceItemError(
	w http.ResponseWriter,
	err error,
) {
	status := merchantInvoiceItemHTTPStatus(err)

	switch status {
	case http.StatusBadRequest:
		app.respondWithError(
			w,
			errors.New(
				"invalid merchant invoice item request",
			),
			status,
		)

	default:
		app.respondWithError(
			w,
			errors.New(
				"failed to process merchant invoice item request",
			),
			http.StatusInternalServerError,
		)
	}
}

// -----------------------------------------------------------------------------
// Path and query parsing
// -----------------------------------------------------------------------------

func parseMerchantInvoiceItemID(
	r *http.Request,
) (uuid.UUID, error) {
	raw := strings.TrimSpace(
		chi.URLParam(
			r,
			"merchantInvoiceItemID",
		),
	)

	if raw == "" {
		return uuid.Nil, errors.New(
			"merchant invoice item ID is required",
		)
	}

	id, err := uuid.Parse(raw)
	if err != nil || id == uuid.Nil {
		return uuid.Nil, errors.New(
			"invalid merchant invoice item ID",
		)
	}

	return id, nil
}

func parseMerchantInvoiceItemFeeCalculationID(
	r *http.Request,
) (uuid.UUID, error) {
	raw := strings.TrimSpace(
		chi.URLParam(
			r,
			"feeCalculationID",
		),
	)

	if raw == "" {
		return uuid.Nil, errors.New(
			"fee calculation ID is required",
		)
	}

	id, err := uuid.Parse(raw)
	if err != nil || id == uuid.Nil {
		return uuid.Nil, errors.New(
			"invalid fee calculation ID",
		)
	}

	return id, nil
}

func parseMerchantInvoiceItemInvoiceID(
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

func parseMerchantInvoiceItemLimit(
	r *http.Request,
) (int, error) {
	raw := strings.TrimSpace(
		r.URL.Query().Get("limit"),
	)

	if raw == "" {
		return merchantInvoiceItemDefaultListLimit, nil
	}

	limit, err := strconv.Atoi(raw)
	if err != nil {
		return 0, errors.New(
			"limit must be an integer",
		)
	}

	if limit < 1 ||
		limit > merchantInvoiceItemMaximumListLimit {
		return 0, fmt.Errorf(
			"limit must be between 1 and %d",
			merchantInvoiceItemMaximumListLimit,
		)
	}

	return limit, nil
}

// parseMerchantInvoiceItemCursor parses the forward keyset cursor used by
// MerchantInvoiceItemModel.ListByInvoiceID.
//
// Both components are one logical cursor and therefore must be supplied
// together or omitted together.
func parseMerchantInvoiceItemCursor(
	r *http.Request,
) (*time.Time, *uuid.UUID, error) {
	rawCreatedAt :=
		strings.TrimSpace(
			r.URL.Query().Get(
				"after_created_at",
			),
		)

	rawID :=
		strings.TrimSpace(
			r.URL.Query().Get(
				"after_id",
			),
		)

	if rawCreatedAt == "" && rawID == "" {
		return nil, nil, nil
	}

	if rawCreatedAt == "" || rawID == "" {
		return nil, nil, errors.New(
			"after_created_at and after_id must be supplied together",
		)
	}

	createdAt, err :=
		time.Parse(
			time.RFC3339,
			rawCreatedAt,
		)
	if err != nil {
		return nil, nil, errors.New(
			"after_created_at must be a valid RFC3339 timestamp",
		)
	}

	id, err := uuid.Parse(rawID)
	if err != nil || id == uuid.Nil {
		return nil, nil, errors.New(
			"after_id must be a valid non-nil UUID",
		)
	}

	createdAt = createdAt.UTC()

	return &createdAt, &id, nil
}

// -----------------------------------------------------------------------------
// Stable privileged response DTOs
// -----------------------------------------------------------------------------

// merchantInvoiceItemResponse is the stable privileged HTTP representation of
// one persisted merchant invoice item.
//
// Currency, billable_event_id, Platform Credit values, net line values, taxes,
// payments, and settlement state are deliberately absent because this domain
// does not own them.
type merchantInvoiceItemResponse struct {
	ID uuid.UUID `json:"id"`

	InvoiceID uuid.UUID `json:"invoice_id"`

	FeeCalculationID uuid.UUID `json:"fee_calculation_id"`

	Description string `json:"description"`

	Quantity string `json:"quantity"`

	UnitAmount string `json:"unit_amount"`

	LineAmount string `json:"line_amount"`

	CreatedAt time.Time `json:"created_at"`
}

func newMerchantInvoiceItemResponse(
	item *data.MerchantInvoiceItem,
) merchantInvoiceItemResponse {
	return merchantInvoiceItemResponse{
		ID: item.ID,

		InvoiceID: item.InvoiceID,

		FeeCalculationID: item.FeeCalculationID,

		Description: item.Description,

		Quantity: item.Quantity,

		UnitAmount: item.UnitAmount,

		LineAmount: item.LineAmount,

		CreatedAt: item.CreatedAt,
	}
}

func newMerchantInvoiceItemResponseList(
	items []*data.MerchantInvoiceItem,
) []merchantInvoiceItemResponse {
	out :=
		make(
			[]merchantInvoiceItemResponse,
			0,
			len(items),
		)

	for _, item := range items {
		if item == nil {
			// The model contract does not intentionally return nil members.
			// Avoid manufacturing a zero-valued DTO should malformed caller
			// input ever reach this presentation helper.
			continue
		}

		out =
			append(
				out,
				newMerchantInvoiceItemResponse(
					item,
				),
			)
	}

	return out
}

type merchantInvoiceItemListPagination struct {
	Limit int `json:"limit"`

	Count int `json:"count"`

	NextAfterCreatedAt *time.Time `json:"next_after_created_at,omitempty"`

	NextAfterID *uuid.UUID `json:"next_after_id,omitempty"`
}

type merchantInvoiceItemListResponse struct {
	Items []merchantInvoiceItemResponse `json:"items"`

	Pagination merchantInvoiceItemListPagination `json:"pagination"`
}

func newMerchantInvoiceItemListResponse(
	items []*data.MerchantInvoiceItem,
	limit int,
) merchantInvoiceItemListResponse {
	responseItems :=
		newMerchantInvoiceItemResponseList(
			items,
		)

	pagination :=
		merchantInvoiceItemListPagination{
			Limit: limit,
			Count: len(responseItems),
		}

	// The model returns at most limit rows. A cursor is deliberately returned
	// for every non-empty page; exhaustion is established by a subsequent
	// bounded request returning no rows. This mirrors the neighboring Commerce
	// keyset handler contract.
	if len(responseItems) > 0 {
		last := responseItems[len(responseItems)-1]

		createdAt := last.CreatedAt.UTC()
		id := last.ID

		pagination.NextAfterCreatedAt =
			&createdAt

		pagination.NextAfterID =
			&id
	}

	return merchantInvoiceItemListResponse{
		Items: responseItems,

		Pagination: pagination,
	}
}

// -----------------------------------------------------------------------------
// Audit
// -----------------------------------------------------------------------------

func (app *Application) auditMerchantInvoiceItemRead(
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
		merchantInvoiceItemEntityType,
		merchantInvoiceItemEntityTypeDescription,
		entityID,
	)
}

// -----------------------------------------------------------------------------
// Canonical ID read
// -----------------------------------------------------------------------------

func (app *Application) GetMerchantInvoiceItemByIDHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	logger :=
		app.Logger.
			GetLoggerWithContext(r).
			WithFunctionName(
				"GetMerchantInvoiceItemByIDHandler",
			)

	ctx, cancel :=
		context.WithTimeout(
			r.Context(),
			cfgTimeout,
		)
	defer cancel()

	if !app.HasPermission(
		ctx,
		actionReadMerchantInvoiceItem,
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
		parseMerchantInvoiceItemID(r)
	if err != nil {
		app.respondWithError(
			w,
			err,
			http.StatusBadRequest,
		)
		return
	}

	item, err :=
		app.Models.
			MerchantInvoiceItem.
			GetByID(
				ctx,
				id,
			)
	if err != nil {
		if merchantInvoiceItemHTTPStatus(err) ==
			http.StatusInternalServerError {
			logger.Error(
				"Get merchant invoice item by ID failed",
				"merchant_invoice_item_id",
				id,
				"error",
				err,
			)
		}

		app.respondWithMerchantInvoiceItemError(
			w,
			err,
		)
		return
	}

	if item == nil {
		app.respondWithError(
			w,
			errors.New(
				"merchant invoice item not found",
			),
			http.StatusNotFound,
		)
		return
	}

	if auditErr :=
		app.auditMerchantInvoiceItemRead(
			ctx,
			userID,
			actionReadMerchantInvoiceItem,
			"Read a merchant invoice item",
			item.ID.String(),
		); auditErr != nil {
		app.serverErrorResponse(
			logger,
			w,
			r,
			fmt.Errorf(
				"record merchant invoice item read audit: %w",
				auditErr,
			),
		)
		return
	}

	app.respondWithJSON(
		w,
		http.StatusOK,
		jsonResponse{
			Error: false,

			Message: "Merchant invoice item retrieved successfully",

			Data: newMerchantInvoiceItemResponse(
				item,
			),
		},
	)
}

// -----------------------------------------------------------------------------
// Fee-calculation provenance read
// -----------------------------------------------------------------------------

// GetMerchantInvoiceItemByFeeCalculationIDHandler retrieves the invoice item,
// if any, already associated with one fee calculation.
//
// This is a reconciliation/provenance read. It does not participate in
// uniqueness enforcement; UNIQUE(fee_calculation_id) remains the concurrency
// boundary.
func (app *Application) GetMerchantInvoiceItemByFeeCalculationIDHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	logger :=
		app.Logger.
			GetLoggerWithContext(r).
			WithFunctionName(
				"GetMerchantInvoiceItemByFeeCalculationIDHandler",
			)

	ctx, cancel :=
		context.WithTimeout(
			r.Context(),
			cfgTimeout,
		)
	defer cancel()

	if !app.HasPermission(
		ctx,
		actionReadMerchantInvoiceItem,
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

	feeCalculationID, err :=
		parseMerchantInvoiceItemFeeCalculationID(
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

	item, err :=
		app.Models.
			MerchantInvoiceItem.
			GetByFeeCalculationID(
				ctx,
				feeCalculationID,
			)
	if err != nil {
		if merchantInvoiceItemHTTPStatus(err) ==
			http.StatusInternalServerError {
			logger.Error(
				"Get merchant invoice item by fee calculation failed",
				"fee_calculation_id",
				feeCalculationID,
				"error",
				err,
			)
		}

		app.respondWithMerchantInvoiceItemError(
			w,
			err,
		)
		return
	}

	if item == nil {
		app.respondWithError(
			w,
			errors.New(
				"merchant invoice item not found",
			),
			http.StatusNotFound,
		)
		return
	}

	if auditErr :=
		app.auditMerchantInvoiceItemRead(
			ctx,
			userID,
			actionReadMerchantInvoiceItem,
			"Read a merchant invoice item by fee calculation",
			item.ID.String(),
		); auditErr != nil {
		app.serverErrorResponse(
			logger,
			w,
			r,
			fmt.Errorf(
				"record merchant invoice item fee-calculation read audit: %w",
				auditErr,
			),
		)
		return
	}

	app.respondWithJSON(
		w,
		http.StatusOK,
		jsonResponse{
			Error: false,

			Message: "Merchant invoice item retrieved successfully",

			Data: newMerchantInvoiceItemResponse(
				item,
			),
		},
	)
}

// -----------------------------------------------------------------------------
// Invoice-scoped history
// -----------------------------------------------------------------------------

// ListMerchantInvoiceItemsByInvoiceIDHandler returns a bounded forward-keyset
// page of invoice items for one invoice.
//
// The model deliberately represents a structurally valid invoice ID with no
// matching item rows as an empty collection. This handler does not issue a
// second parent-invoice existence query merely to transform that persistence
// contract into 404.
func (app *Application) ListMerchantInvoiceItemsByInvoiceIDHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	logger :=
		app.Logger.
			GetLoggerWithContext(r).
			WithFunctionName(
				"ListMerchantInvoiceItemsByInvoiceIDHandler",
			)

	ctx, cancel :=
		context.WithTimeout(
			r.Context(),
			cfgTimeout,
		)
	defer cancel()

	if !app.HasPermission(
		ctx,
		actionListMerchantInvoiceItems,
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

	invoiceID, err :=
		parseMerchantInvoiceItemInvoiceID(
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
		parseMerchantInvoiceItemLimit(
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

	afterCreatedAt, afterID, err :=
		parseMerchantInvoiceItemCursor(
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

	items, err :=
		app.Models.
			MerchantInvoiceItem.
			ListByInvoiceID(
				ctx,
				invoiceID,
				limit,
				afterCreatedAt,
				afterID,
			)
	if err != nil {
		if merchantInvoiceItemHTTPStatus(err) ==
			http.StatusInternalServerError {
			logger.Error(
				"List merchant invoice items by invoice failed",
				"invoice_id",
				invoiceID,
				"error",
				err,
			)
		}

		app.respondWithMerchantInvoiceItemError(
			w,
			err,
		)
		return
	}

	response :=
		newMerchantInvoiceItemListResponse(
			items,
			limit,
		)

	if auditErr :=
		app.auditMerchantInvoiceItemRead(
			ctx,
			userID,
			actionListMerchantInvoiceItems,
			"List an invoice's merchant invoice item history",
			invoiceID.String(),
		); auditErr != nil {
		app.serverErrorResponse(
			logger,
			w,
			r,
			fmt.Errorf(
				"record merchant invoice item list audit: %w",
				auditErr,
			),
		)
		return
	}

	logger.Info(
		"Merchant invoice items retrieved",
		"invoice_id",
		invoiceID,
		"result_count",
		response.Pagination.Count,
	)

	app.respondWithJSON(
		w,
		http.StatusOK,
		jsonResponse{
			Error: false,

			Message: "Merchant invoice items retrieved successfully",

			Data: response,
		},
	)
}
