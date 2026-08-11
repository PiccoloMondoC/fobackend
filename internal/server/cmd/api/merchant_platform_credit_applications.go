// Package main provides HTTP handlers for merchant platform credit
// application privileged historical review.
//
// sdworkspace/sdbackend/internal/server/cmd/api/merchant_platform_credit_applications.go
//
// GTM:
//
//	Layer: 2.4.b Commerce Architecture / Monetization Layer
//	Release Class: SPINE
//	Reason:
//	  merchant_platform_credit_applications records the durable historical
//	  fact that a specific amount of platform-issued commercial credit (see
//	  merchant_platform_credit_accounts.go) was applied against a specific
//	  fee calculation. This changes the amount commercially owed before
//	  payment collection. It therefore belongs to Commerce Architecture, not
//	  Merchant Payments Architecture.
//
// Domain Boundary:
//
//	This handler exposes only privileged, read-only access to append-only
//	credit-application history: retrieval by canonical ID, retrieval by the
//	composite (credit_account_id, fee_calculation_id) identity, and bounded
//	offset-paginated listing by credit account or by fee calculation. It does
//	not decide credit eligibility, does not select which credit account
//	should be consumed, does not determine account-consumption ordering, and
//	does not implement promotion or grant policy. Those responsibilities
//	belong to governed eligibility data and service orchestration.
//
// Monetary Boundary:
//
//	applied_amount is a PostgreSQL NUMERIC(19,4) value represented at the API
//	boundary as a canonical decimal string. This handler never parses,
//	serializes, or compares applied_amount as floating point, and never
//	performs monetary arithmetic. Currency is an uppercase three-letter
//	structural identifier only; this handler does not embed a commercially
//	enabled currency list.
//
// Handler Boundary:
//
//	This file implements only the handler layer for reads. These reads call the
//	canonical data model directly because they are business-logic-free,
//	transaction-free historical retrievals whose complete semantics already
//	belong to the data contract. No thin InternalServices pass-through is
//	introduced merely to add indirection. Any future credit-application
//	creation or other business-logic-bearing workflow must be owned by the
//	service layer.
//
//	This file does not implement service orchestration, transaction
//	coordination, credit-account consumption, eligibility logic,
//	account-selection rules, fee-type policy, or billing/invoice logic.
//
// Read-Only and Transaction-Boundary Explanation:
//
//	merchant_platform_credit_applications is append-only monetary history:
//	created once, retained permanently, with no update, patch, cancellation,
//	deletion, soft deletion, or restoration. The data model intentionally
//	exposes only a transaction-aware InsertTx, which must be composed with
//	MerchantPlatformCreditAccountModel.ConsumeTx (and any required
//	fee-calculation or billing mutation) inside one service-owned
//	transaction. This handler file therefore does not call InsertTx, does
//	not begin or coordinate that transaction, and exposes no POST, PUT,
//	PATCH, or DELETE route for this domain. A credit-application row
//	persisted independently of its corresponding account decrement would
//	break the atomicity this domain depends on; application creation
//	belongs to a later service-orchestration boundary, not to this handler.
//
// SPINE Rule:
//
//	Keep compiling.
//	Keep production-ready.
//	Preserve append-only monetary history: never expose update, delete, or
//	restore for this domain through this handler.
//	Never expose a creation endpoint; InsertTx composition with
//	MerchantPlatformCreditAccountModel.ConsumeTx remains a service-owned
//	transaction boundary.
//	Preserve strict authorization and audit coverage for every operation.
//	Preserve monetary values as canonical decimal strings.
//	Preserve deterministic ordering (applied_at DESC, id DESC) as returned
//	by the data layer; never reorder results in this handler.
//	Never hard-code commercial eligibility, grant, or account-selection
//	policy.
//	Block deployment if this file breaks build, authorization, audit
//	accountability, monetary safety, or the append-only lifecycle boundary.
package main

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/PiccoloMondoC/sdworkspace/sdbackend/internal/data"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// -----------------------------------------------------------------------------
// Permission / action / entity vocabulary
// -----------------------------------------------------------------------------

const (
	// merchantPlatformCreditApplicationEntityType is the canonical audit
	// entity type for this domain.
	merchantPlatformCreditApplicationEntityType = "merchant_platform_credit_application"

	// merchantPlatformCreditApplicationEntityTypeDescription is the
	// canonical audit entity type description for this domain.
	merchantPlatformCreditApplicationEntityTypeDescription = "Platform-issued merchant commercial credit application entity"

	// actionReadMerchantPlatformCreditApplication covers both single-record
	// retrieval by canonical ID and retrieval by the composite
	// (credit_account_id, fee_calculation_id) identity: both return at most
	// one record and represent the same "read one application" audited
	// operation.
	actionReadMerchantPlatformCreditApplication = "read_merchant_platform_credit_application"

	// actionListMerchantPlatformCreditApplications covers both
	// bounded-listing forms (by credit account, by fee calculation): both
	// return a bounded set of applications and represent the same "list
	// applications" audited operation.
	actionListMerchantPlatformCreditApplications = "list_merchant_platform_credit_applications"

	merchantPlatformCreditApplicationDefaultListLimit = 20
	merchantPlatformCreditApplicationMaximumListLimit = 100
)

// -----------------------------------------------------------------------------
// Error translation
// -----------------------------------------------------------------------------

// merchantPlatformCreditApplicationHTTPStatus translates merchant platform
// credit application domain errors into stable HTTP status codes.
//
// Classification is based exclusively on exported data-layer sentinel
// errors. Error-message wording is diagnostic context and is never part of
// the HTTP contract. This handler is read-only, so
// ErrMerchantPlatformCreditApplicationCreditAccountNotFound,
// ErrMerchantPlatformCreditApplicationFeeCalculationNotFound,
// ErrMerchantPlatformCreditApplicationDuplicate, and
// ErrMerchantPlatformCreditApplicationInvalidState are write-path
// classifications that should not ordinarily arise from these read methods.
// They remain mapped here so the translator stays semantically correct
// rather than silently defaulting to 500 if the data layer's error surface
// changes.
func merchantPlatformCreditApplicationHTTPStatus(err error) int {
	if err == nil {
		return http.StatusOK
	}

	switch {
	case errors.Is(
		err,
		data.ErrMerchantPlatformCreditApplicationInvalidInput,
	):
		return http.StatusBadRequest

	case errors.Is(
		err,
		data.ErrMerchantPlatformCreditApplicationCreditAccountNotFound,
	),
		errors.Is(
			err,
			data.ErrMerchantPlatformCreditApplicationFeeCalculationNotFound,
		):
		return http.StatusNotFound

	case errors.Is(
		err,
		data.ErrMerchantPlatformCreditApplicationDuplicate,
	),
		errors.Is(
			err,
			data.ErrMerchantPlatformCreditApplicationInvalidState,
		):
		return http.StatusConflict

	default:
		return http.StatusInternalServerError
	}
}

// respondWithMerchantPlatformCreditApplicationError writes a stable public
// error without exposing database, driver, SQL, or wrapped internal details.
func (app *Application) respondWithMerchantPlatformCreditApplicationError(
	w http.ResponseWriter,
	err error,
) {
	status := merchantPlatformCreditApplicationHTTPStatus(err)

	message := "failed to process merchant platform credit application request"
	switch status {
	case http.StatusBadRequest:
		message = "invalid merchant platform credit application request"
	case http.StatusNotFound:
		message = "merchant platform credit application not found"
	case http.StatusConflict:
		message = "merchant platform credit application request conflicts with persisted state"
	case http.StatusInternalServerError:
		message = "failed to process merchant platform credit application request"
	}

	app.respondWithError(w, errors.New(message), status)
}

// -----------------------------------------------------------------------------
// Path parameter parsing
// -----------------------------------------------------------------------------

// parseMerchantPlatformCreditApplicationID extracts and validates the
// canonical merchant platform credit application ID from the URL path.
func (app *Application) parseMerchantPlatformCreditApplicationID(r *http.Request) (uuid.UUID, error) {
	raw := strings.TrimSpace(chi.URLParam(r, "merchantPlatformCreditApplicationID"))
	if raw == "" {
		return uuid.Nil, errors.New("merchant platform credit application ID is required")
	}

	id, err := uuid.Parse(raw)
	if err != nil || id == uuid.Nil {
		return uuid.Nil, errors.New("invalid merchant platform credit application ID")
	}

	return id, nil
}

// parseFeeCalculationIDPathParam extracts and validates a fee calculation ID
// supplied as a path parameter under the trusted key "feeCalculationID".
//
// This historical-read handler treats feeCalculationID as an opaque,
// structurally validated UUID. Cross-entity monetary and lifecycle validation
// belongs to merchant_fee_calculations persistence and trusted service
// orchestration, not to this read-only handler.
func (app *Application) parseFeeCalculationIDPathParam(r *http.Request) (uuid.UUID, error) {
	raw := strings.TrimSpace(chi.URLParam(r, "feeCalculationID"))
	if raw == "" {
		return uuid.Nil, errors.New("fee calculation ID is required")
	}

	id, err := uuid.Parse(raw)
	if err != nil || id == uuid.Nil {
		return uuid.Nil, errors.New("invalid fee calculation ID")
	}

	return id, nil
}

// -----------------------------------------------------------------------------
// Pagination parsing
// -----------------------------------------------------------------------------

// parseMerchantPlatformCreditApplicationLimit parses a bounded optional
// limit.
//
// An omitted limit uses the handler default. A present malformed, zero,
// negative, or over-maximum value is rejected rather than silently replaced,
// per BEG input-handling doctrine.
func parseMerchantPlatformCreditApplicationLimit(r *http.Request) (int, error) {
	raw := strings.TrimSpace(r.URL.Query().Get("limit"))
	if raw == "" {
		return merchantPlatformCreditApplicationDefaultListLimit, nil
	}

	limit, err := strconv.Atoi(raw)
	if err != nil {
		return 0, errors.New("limit must be an integer")
	}
	if limit < 1 || limit > merchantPlatformCreditApplicationMaximumListLimit {
		return 0, errors.New("limit must be between 1 and 100")
	}

	return limit, nil
}

// parseMerchantPlatformCreditApplicationOffset parses a bounded optional
// offset.
//
// An omitted offset defaults to zero. A present malformed or negative value
// is rejected rather than silently replaced, per BEG input-handling
// doctrine.
func parseMerchantPlatformCreditApplicationOffset(r *http.Request) (int, error) {
	raw := strings.TrimSpace(r.URL.Query().Get("offset"))
	if raw == "" {
		return 0, nil
	}

	offset, err := strconv.Atoi(raw)
	if err != nil {
		return 0, errors.New("offset must be an integer")
	}
	if offset < 0 {
		return 0, errors.New("offset must be non-negative")
	}

	return offset, nil
}

// -----------------------------------------------------------------------------
// Reads
// -----------------------------------------------------------------------------

// GetMerchantPlatformCreditApplicationByIDHandler retrieves one merchant
// platform credit application by canonical ID.
//
// This is a privileged administrative read over durable, append-only
// monetary history. A missing record is reported as 404; it is not
// classified through a data-layer sentinel error because GetByID returns
// (nil, nil) for absence, consistent with the credit-account read
// precedent.
func (app *Application) GetMerchantPlatformCreditApplicationByIDHandler(w http.ResponseWriter, r *http.Request) {
	logger := app.Logger.GetLoggerWithContext(r).WithFunctionName("GetMerchantPlatformCreditApplicationByIDHandler")

	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	if !app.HasPermission(ctx, actionReadMerchantPlatformCreditApplication) {
		app.respondWithError(w, errors.New("forbidden: insufficient permissions"), http.StatusForbidden)
		return
	}

	userID := app.getUserIDFromContext(ctx)
	if userID == nil {
		app.respondWithError(w, errors.New("user ID not found in context"), http.StatusUnauthorized)
		return
	}

	applicationID, err := app.parseMerchantPlatformCreditApplicationID(r)
	if err != nil {
		app.respondWithError(w, err, http.StatusBadRequest)
		return
	}

	application, err := app.Models.MerchantPlatformCreditApplication.GetByID(ctx, applicationID)
	if err != nil {
		logger.Error(
			"Get merchant platform credit application by ID failed",
			"merchant_platform_credit_application_id", applicationID,
			"error", err,
		)
		app.respondWithMerchantPlatformCreditApplicationError(w, err)
		return
	}
	if application == nil {
		app.respondWithError(w, errors.New("merchant platform credit application not found"), http.StatusNotFound)
		return
	}

	if err := app.insertGovernanceAudit(
		ctx,
		userID,
		actionReadMerchantPlatformCreditApplication,
		"Read a merchant platform credit application",
		merchantPlatformCreditApplicationEntityType,
		merchantPlatformCreditApplicationEntityTypeDescription,
		application.ID.String(),
	); err != nil {
		logger.Warn("Audit logging failed", "merchant_platform_credit_application_id", application.ID, "error", err)
	}

	app.respondWithJSON(w, http.StatusOK, jsonResponse{
		Error:   false,
		Message: "Merchant platform credit application retrieved successfully",
		Data:    application,
	})
}

// GetMerchantPlatformCreditApplicationByCreditAccountAndFeeCalculationHandler
// retrieves the application row, if any, representing a given credit
// account's contribution to a given fee calculation.
//
// This is the canonical read for checking the (credit_account_id,
// fee_calculation_id) idempotency boundary from a privileged administrative
// or service-diagnostic context. It must not be treated as a concurrency
// guarantee itself; the persisted unique constraint enforced at insertion
// time is the concurrency guarantee.
func (app *Application) GetMerchantPlatformCreditApplicationByCreditAccountAndFeeCalculationHandler(w http.ResponseWriter, r *http.Request) {
	logger := app.Logger.GetLoggerWithContext(r).WithFunctionName("GetMerchantPlatformCreditApplicationByCreditAccountAndFeeCalculationHandler")

	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	if !app.HasPermission(ctx, actionReadMerchantPlatformCreditApplication) {
		app.respondWithError(w, errors.New("forbidden: insufficient permissions"), http.StatusForbidden)
		return
	}

	userID := app.getUserIDFromContext(ctx)
	if userID == nil {
		app.respondWithError(w, errors.New("user ID not found in context"), http.StatusUnauthorized)
		return
	}

	creditAccountID, err := app.parseMerchantPlatformCreditAccountID(r)
	if err != nil {
		app.respondWithError(w, err, http.StatusBadRequest)
		return
	}

	feeCalculationID, err := app.parseFeeCalculationIDPathParam(r)
	if err != nil {
		app.respondWithError(w, err, http.StatusBadRequest)
		return
	}

	application, err := app.Models.MerchantPlatformCreditApplication.GetByCreditAccountAndFeeCalculation(
		ctx,
		creditAccountID,
		feeCalculationID,
	)
	if err != nil {
		logger.Error(
			"Get merchant platform credit application by credit account and fee calculation failed",
			"credit_account_id", creditAccountID,
			"fee_calculation_id", feeCalculationID,
			"error", err,
		)
		app.respondWithMerchantPlatformCreditApplicationError(w, err)
		return
	}
	if application == nil {
		app.respondWithError(w, errors.New("merchant platform credit application not found for credit account and fee calculation"), http.StatusNotFound)
		return
	}

	// Audit the canonical application identity returned by the lookup. The
	// account and fee-calculation IDs remain structured diagnostic fields in
	// the request log, while governance audit identity stays aligned with the
	// canonical entity ID used by the domain's other single-record read.
	if err := app.insertGovernanceAudit(
		ctx,
		userID,
		actionReadMerchantPlatformCreditApplication,
		"Read a merchant platform credit application by credit account and fee calculation",
		merchantPlatformCreditApplicationEntityType,
		merchantPlatformCreditApplicationEntityTypeDescription,
		application.ID.String(),
	); err != nil {
		logger.Warn(
			"Audit logging failed",
			"merchant_platform_credit_application_id",
			application.ID,
			"credit_account_id",
			creditAccountID,
			"fee_calculation_id",
			feeCalculationID,
			"error",
			err,
		)
	}

	app.respondWithJSON(w, http.StatusOK, jsonResponse{
		Error:   false,
		Message: "Merchant platform credit application retrieved successfully",
		Data:    application,
	})
}

// ListMerchantPlatformCreditApplicationsByCreditAccountHandler lists
// applications for a credit account using bounded offset pagination.
//
// Ordering (applied_at DESC, id DESC) is deterministic and supplied by the
// data layer. This handler never reorders results. An empty result set is
// returned as an empty array, never null.
func (app *Application) ListMerchantPlatformCreditApplicationsByCreditAccountHandler(w http.ResponseWriter, r *http.Request) {
	logger := app.Logger.GetLoggerWithContext(r).WithFunctionName("ListMerchantPlatformCreditApplicationsByCreditAccountHandler")

	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	if !app.HasPermission(ctx, actionListMerchantPlatformCreditApplications) {
		app.respondWithError(w, errors.New("forbidden: insufficient permissions"), http.StatusForbidden)
		return
	}

	userID := app.getUserIDFromContext(ctx)
	if userID == nil {
		app.respondWithError(w, errors.New("user ID not found in context"), http.StatusUnauthorized)
		return
	}

	creditAccountID, err := app.parseMerchantPlatformCreditAccountID(r)
	if err != nil {
		app.respondWithError(w, err, http.StatusBadRequest)
		return
	}

	limit, err := parseMerchantPlatformCreditApplicationLimit(r)
	if err != nil {
		app.respondWithError(w, err, http.StatusBadRequest)
		return
	}

	offset, err := parseMerchantPlatformCreditApplicationOffset(r)
	if err != nil {
		app.respondWithError(w, err, http.StatusBadRequest)
		return
	}

	applications, err := app.Models.MerchantPlatformCreditApplication.ListByCreditAccount(
		ctx,
		creditAccountID,
		limit,
		offset,
	)
	if err != nil {
		logger.Error(
			"List merchant platform credit applications by credit account failed",
			"credit_account_id", creditAccountID,
			"error", err,
		)
		app.respondWithMerchantPlatformCreditApplicationError(w, err)
		return
	}

	// Bounded audit identifier: the credit account being listed, not the
	// unbounded result set, per audit doctrine for list operations.
	if err := app.insertGovernanceAudit(
		ctx,
		userID,
		actionListMerchantPlatformCreditApplications,
		"List a credit account's merchant platform credit application history",
		merchantPlatformCreditApplicationEntityType,
		merchantPlatformCreditApplicationEntityTypeDescription,
		creditAccountID.String(),
	); err != nil {
		logger.Warn("Audit logging failed", "credit_account_id", creditAccountID, "error", err)
	}

	app.respondWithJSON(w, http.StatusOK, jsonResponse{
		Error:   false,
		Message: "Merchant platform credit applications retrieved successfully",
		Data:    applications,
	})
}

// ListMerchantPlatformCreditApplicationsByFeeCalculationHandler lists
// applications for a fee calculation using bounded offset pagination.
//
// Ordering (applied_at DESC, id DESC) is deterministic and supplied by the
// data layer. This handler never reorders results. An empty result set is
// returned as an empty array, never null. Multiple different credit
// accounts may each apply credit to the same fee calculation (partial
// coverage across accounts), so this list may legitimately contain more
// than one row for a single fee calculation.
func (app *Application) ListMerchantPlatformCreditApplicationsByFeeCalculationHandler(w http.ResponseWriter, r *http.Request) {
	logger := app.Logger.GetLoggerWithContext(r).WithFunctionName("ListMerchantPlatformCreditApplicationsByFeeCalculationHandler")

	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	if !app.HasPermission(ctx, actionListMerchantPlatformCreditApplications) {
		app.respondWithError(w, errors.New("forbidden: insufficient permissions"), http.StatusForbidden)
		return
	}

	userID := app.getUserIDFromContext(ctx)
	if userID == nil {
		app.respondWithError(w, errors.New("user ID not found in context"), http.StatusUnauthorized)
		return
	}

	feeCalculationID, err := app.parseFeeCalculationIDPathParam(r)
	if err != nil {
		app.respondWithError(w, err, http.StatusBadRequest)
		return
	}

	limit, err := parseMerchantPlatformCreditApplicationLimit(r)
	if err != nil {
		app.respondWithError(w, err, http.StatusBadRequest)
		return
	}

	offset, err := parseMerchantPlatformCreditApplicationOffset(r)
	if err != nil {
		app.respondWithError(w, err, http.StatusBadRequest)
		return
	}

	applications, err := app.Models.MerchantPlatformCreditApplication.ListByFeeCalculation(
		ctx,
		feeCalculationID,
		limit,
		offset,
	)
	if err != nil {
		logger.Error(
			"List merchant platform credit applications by fee calculation failed",
			"fee_calculation_id", feeCalculationID,
			"error", err,
		)
		app.respondWithMerchantPlatformCreditApplicationError(w, err)
		return
	}

	// Bounded audit identifier: the fee calculation being listed, not the
	// unbounded result set, per audit doctrine for list operations.
	if err := app.insertGovernanceAudit(
		ctx,
		userID,
		actionListMerchantPlatformCreditApplications,
		"List a fee calculation's merchant platform credit application history",
		merchantPlatformCreditApplicationEntityType,
		merchantPlatformCreditApplicationEntityTypeDescription,
		feeCalculationID.String(),
	); err != nil {
		logger.Warn("Audit logging failed", "fee_calculation_id", feeCalculationID, "error", err)
	}

	app.respondWithJSON(w, http.StatusOK, jsonResponse{
		Error:   false,
		Message: "Merchant platform credit applications retrieved successfully",
		Data:    applications,
	})
}
