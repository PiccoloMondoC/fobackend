// Package main provides HTTP handlers for merchant platform credit
// fee-type eligibility governance.
//
// sdworkspace/sdbackend/internal/server/cmd/api/merchant_platform_credit_eligible_fee_types.go
//
// GTM:
//
//	Layer: 2.4.b Commerce Architecture / Monetization Layer
//	Release Class: SPINE
//	Reason:
//	  merchant_platform_credit_eligible_fee_types records which canonical
//	  merchant fee types may consume a particular platform-issued merchant
//	  credit account. This capability affects the amount commercially owed
//	  before payment settlement and therefore belongs to Commerce
//	  Architecture, not Merchant Payments Architecture.
//
// Domain Boundary:
//
//	This handler governs privileged administrative configuration of
//	fee-type eligibility only. It does not decide which merchants receive
//	credit, which fee types are normally eligible, whether Plans,
//	Subscriptions, Launch Campaigns, or Anticipation Intelligence fees are
//	enabled, or whether an eligibility set is commercially advisable. It
//	does not apply credit to any invoice or billable event.
//
// Handler Boundary:
//
//	This file implements only the handler layer. It contains no SQL, opens
//	no database transactions, duplicates no model canonicalization, and
//	calls only the pool-based data-layer methods. Transaction-aware model
//	methods remain composition surfaces for a future service layer.
//
// SPINE Rule:
//
//	Keep compiling.
//	Keep production-ready.
//	Preserve strict authorization and audit coverage for every operation.
//	Preserve mass-assignment safety through explicit request DTOs.
//	Preserve the canonical shared fee vocabulary; never redeclare it here.
//	Preserve composite-identity handling; never invent a synthetic ID.
//	Preserve atomic complete-set replacement through the data layer.
//	Never hard-code eligibility, grant, promotion, or enablement policy.
//	Never expose a redundant delete-all endpoint.
//	Block deployment if this file breaks build, authorization, audit
//	accountability, or composite-identity integrity.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/PiccoloMondoC/sdworkspace/sdbackend/internal/data"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

const (
	merchantPlatformCreditEligibleFeeTypeEntityType = "merchant_platform_credit_eligible_fee_type"

	merchantPlatformCreditEligibleFeeTypeEntityTypeDescription = "Merchant platform credit fee-type eligibility association"

	actionCreateMerchantPlatformCreditEligibleFeeType     = "create_merchant_platform_credit_eligible_fee_type"
	actionReadMerchantPlatformCreditEligibleFeeType       = "read_merchant_platform_credit_eligible_fee_type"
	actionListMerchantPlatformCreditEligibleFeeTypes      = "list_merchant_platform_credit_eligible_fee_types"
	actionCheckMerchantPlatformCreditEligibleFeeType      = "check_merchant_platform_credit_eligible_fee_type"
	actionDeleteMerchantPlatformCreditEligibleFeeType     = "delete_merchant_platform_credit_eligible_fee_type"
	actionReplaceMerchantPlatformCreditEligibleFeeTypeSet = "replace_merchant_platform_credit_eligible_fee_type_set"
)

func merchantPlatformCreditEligibleFeeTypeHTTPStatus(err error) int {
	if err == nil {
		return http.StatusOK
	}

	switch {
	case errors.Is(err, data.ErrMerchantPlatformCreditEligibleFeeTypeInvalidInput):
		return http.StatusBadRequest
	case errors.Is(err, data.ErrMerchantPlatformCreditEligibleFeeTypeNotFound),
		errors.Is(err, data.ErrMerchantPlatformCreditAccountNotFound):
		return http.StatusNotFound
	case errors.Is(err, data.ErrMerchantPlatformCreditEligibleFeeTypeAlreadyExists):
		return http.StatusConflict
	default:
		return http.StatusInternalServerError
	}
}

// normalizeMerchantPlatformCreditEligibleFeeType canonicalizes and validates
// one fee type against the structural vocabulary owned by the data layer.
//
// This validation mirrors the database CHECK constraint. It does not decide
// whether the fee type should be assigned to a particular credit account.
func normalizeMerchantPlatformCreditEligibleFeeType(
	feeType data.MerchantFeeType,
) (data.MerchantFeeType, error) {
	feeType = data.NormalizeMerchantFeeType(feeType)
	if !data.IsMerchantPlatformCreditEligibleFeeTypeVocabulary(feeType) {
		return "", errors.New("invalid fee type")
	}

	return feeType, nil
}

// parseMerchantPlatformCreditEligibleFeeType extracts, canonicalizes, and
// validates the fee-type component of the composite URL identity.
func (app *Application) parseMerchantPlatformCreditEligibleFeeType(
	r *http.Request,
) (data.MerchantFeeType, error) {
	raw := strings.TrimSpace(chi.URLParam(r, "feeType"))
	if raw == "" {
		return "", errors.New("fee type is required")
	}

	return normalizeMerchantPlatformCreditEligibleFeeType(
		data.MerchantFeeType(raw),
	)
}

func (app *Application) parseMerchantPlatformCreditEligibleFeeTypeKey(r *http.Request) (uuid.UUID, data.MerchantFeeType, error) {
	accountID, err := app.parseMerchantPlatformCreditAccountID(r)
	if err != nil {
		return uuid.Nil, "", err
	}

	feeType, err := app.parseMerchantPlatformCreditEligibleFeeType(r)
	if err != nil {
		return uuid.Nil, "", err
	}

	return accountID, feeType, nil
}

func merchantPlatformCreditEligibleFeeTypeEntityID(accountID uuid.UUID, feeType data.MerchantFeeType) string {
	return accountID.String() + ":" + string(feeType)
}

type createMerchantPlatformCreditEligibleFeeTypeRequest struct {
	FeeType data.MerchantFeeType `json:"fee_type"`
}

type checkMerchantPlatformCreditEligibleFeeTypeResponse struct {
	CreditAccountID uuid.UUID            `json:"credit_account_id"`
	FeeType         data.MerchantFeeType `json:"fee_type"`
	Eligible        bool                 `json:"eligible"`
}

type merchantPlatformCreditEligibleFeeTypeMutationResponse struct {
	CreditAccountID uuid.UUID            `json:"credit_account_id"`
	FeeType         data.MerchantFeeType `json:"fee_type"`
}

type replaceMerchantPlatformCreditEligibleFeeTypeSetFeeTypes struct {
	Set   bool
	Value []data.MerchantFeeType
}

func (field *replaceMerchantPlatformCreditEligibleFeeTypeSetFeeTypes) UnmarshalJSON(raw []byte) error {
	field.Set = true

	if strings.TrimSpace(string(raw)) == "null" {
		return errors.New("fee_types must be an array; use an empty array to clear the set")
	}

	var values []data.MerchantFeeType
	if err := json.Unmarshal(raw, &values); err != nil {
		return errors.New("fee_types must be an array of fee type strings")
	}

	field.Value = values
	return nil
}

type replaceMerchantPlatformCreditEligibleFeeTypeSetRequest struct {
	FeeTypes replaceMerchantPlatformCreditEligibleFeeTypeSetFeeTypes `json:"fee_types"`
}

// CreateMerchantPlatformCreditEligibleFeeTypeHandler creates one eligibility
// association between a merchant platform credit account and a canonical fee
// type.
//
// Authorization permits the administrative actor to configure eligibility.
// It does not determine whether the association is commercially advisable.
func (app *Application) CreateMerchantPlatformCreditEligibleFeeTypeHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	logger := app.Logger.
		GetLoggerWithContext(r).
		WithFunctionName(
			"CreateMerchantPlatformCreditEligibleFeeTypeHandler",
		)

	ctx, cancel := context.WithTimeout(
		r.Context(),
		cfgTimeout,
	)
	defer cancel()

	if !app.HasPermission(
		ctx,
		actionCreateMerchantPlatformCreditEligibleFeeType,
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

	accountID, err := app.parseMerchantPlatformCreditAccountID(r)
	if err != nil {
		app.respondWithError(
			w,
			err,
			http.StatusBadRequest,
		)
		return
	}

	var input createMerchantPlatformCreditEligibleFeeTypeRequest
	if err := app.readJSON(w, r, &input); err != nil {
		app.respondWithError(
			w,
			errors.New("invalid JSON input: "+err.Error()),
			http.StatusBadRequest,
		)
		return
	}

	feeType, err :=
		normalizeMerchantPlatformCreditEligibleFeeType(
			input.FeeType,
		)
	if err != nil {
		app.respondWithError(
			w,
			err,
			http.StatusBadRequest,
		)
		return
	}

	association, err :=
		app.Models.
			MerchantPlatformCreditEligibleFeeType.
			Insert(
				ctx,
				accountID,
				feeType,
			)
	if err != nil {
		logger.Error(
			"Create merchant platform credit eligible fee type failed",
			"credit_account_id",
			accountID,
			"fee_type",
			feeType,
			"error",
			err,
		)

		app.respondWithError(
			w,
			err,
			merchantPlatformCreditEligibleFeeTypeHTTPStatus(err),
		)
		return
	}

	entityID :=
		merchantPlatformCreditEligibleFeeTypeEntityID(
			association.CreditAccountID,
			association.FeeType,
		)

	if err := app.insertGovernanceAudit(
		ctx,
		userID,
		actionCreateMerchantPlatformCreditEligibleFeeType,
		"Create a merchant platform credit fee-type eligibility association",
		merchantPlatformCreditEligibleFeeTypeEntityType,
		merchantPlatformCreditEligibleFeeTypeEntityTypeDescription,
		entityID,
	); err != nil {
		logger.Warn(
			"Audit logging failed",
			"entity_id",
			entityID,
			"error",
			err,
		)

		app.respondWithJSON(
			w,
			http.StatusCreated,
			jsonResponse{
				Error: false,
				Message: "Merchant platform credit eligible fee type " +
					"created, but audit logging failed",
				Data: association,
			},
		)
		return
	}

	logger.Info(
		"Merchant platform credit eligible fee type created",
		"credit_account_id",
		association.CreditAccountID,
		"fee_type",
		association.FeeType,
	)

	app.respondWithJSON(
		w,
		http.StatusCreated,
		jsonResponse{
			Error: false,
			Message: "Merchant platform credit eligible fee type " +
				"created successfully",
			Data: association,
		},
	)
}

func (app *Application) GetMerchantPlatformCreditEligibleFeeTypeHandler(w http.ResponseWriter, r *http.Request) {
	logger := app.Logger.GetLoggerWithContext(r).WithFunctionName("GetMerchantPlatformCreditEligibleFeeTypeHandler")

	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	if !app.HasPermission(ctx, actionReadMerchantPlatformCreditEligibleFeeType) {
		app.respondWithError(w, errors.New("forbidden: insufficient permissions"), http.StatusForbidden)
		return
	}

	userID := app.getUserIDFromContext(ctx)
	if userID == nil {
		app.respondWithError(w, errors.New("user ID not found in context"), http.StatusUnauthorized)
		return
	}

	accountID, feeType, err := app.parseMerchantPlatformCreditEligibleFeeTypeKey(r)
	if err != nil {
		app.respondWithError(w, err, http.StatusBadRequest)
		return
	}

	association, err := app.Models.MerchantPlatformCreditEligibleFeeType.Get(ctx, accountID, feeType)
	if err != nil {
		logger.Error("Get merchant platform credit eligible fee type failed", "credit_account_id", accountID, "fee_type", feeType, "error", err)
		app.respondWithError(w, err, merchantPlatformCreditEligibleFeeTypeHTTPStatus(err))
		return
	}
	if association == nil {
		app.respondWithError(w, data.ErrMerchantPlatformCreditEligibleFeeTypeNotFound, http.StatusNotFound)
		return
	}

	entityID := merchantPlatformCreditEligibleFeeTypeEntityID(accountID, feeType)
	if err := app.insertGovernanceAudit(ctx, userID, actionReadMerchantPlatformCreditEligibleFeeType, "Read a merchant platform credit fee-type eligibility association", merchantPlatformCreditEligibleFeeTypeEntityType, merchantPlatformCreditEligibleFeeTypeEntityTypeDescription, entityID); err != nil {
		logger.Warn("Audit logging failed", "entity_id", entityID, "error", err)
		app.respondWithJSON(w, http.StatusOK, jsonResponse{Error: false, Message: "Merchant platform credit eligible fee type retrieved, but audit logging failed", Data: association})
		return
	}

	app.respondWithJSON(w, http.StatusOK, jsonResponse{Error: false, Message: "Merchant platform credit eligible fee type retrieved successfully", Data: association})
}

func (app *Application) ListMerchantPlatformCreditEligibleFeeTypesHandler(w http.ResponseWriter, r *http.Request) {
	logger := app.Logger.GetLoggerWithContext(r).WithFunctionName("ListMerchantPlatformCreditEligibleFeeTypesHandler")

	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	if !app.HasPermission(ctx, actionListMerchantPlatformCreditEligibleFeeTypes) {
		app.respondWithError(w, errors.New("forbidden: insufficient permissions"), http.StatusForbidden)
		return
	}

	userID := app.getUserIDFromContext(ctx)
	if userID == nil {
		app.respondWithError(w, errors.New("user ID not found in context"), http.StatusUnauthorized)
		return
	}

	accountID, err := app.parseMerchantPlatformCreditAccountID(r)
	if err != nil {
		app.respondWithError(w, err, http.StatusBadRequest)
		return
	}

	associations, err := app.Models.MerchantPlatformCreditEligibleFeeType.ListByCreditAccount(ctx, accountID)
	if err != nil {
		logger.Error("List merchant platform credit eligible fee types failed", "credit_account_id", accountID, "error", err)
		app.respondWithError(w, err, merchantPlatformCreditEligibleFeeTypeHTTPStatus(err))
		return
	}
	if associations == nil {
		associations = make([]*data.MerchantPlatformCreditEligibleFeeType, 0)
	}

	if err := app.insertGovernanceAudit(ctx, userID, actionListMerchantPlatformCreditEligibleFeeTypes, "List a merchant platform credit account's fee-type eligibility set", merchantPlatformCreditEligibleFeeTypeEntityType, merchantPlatformCreditEligibleFeeTypeEntityTypeDescription, accountID.String()); err != nil {
		logger.Warn("Audit logging failed", "credit_account_id", accountID, "error", err)
		app.respondWithJSON(w, http.StatusOK, jsonResponse{Error: false, Message: "Merchant platform credit eligible fee types retrieved, but audit logging failed", Data: associations})
		return
	}

	app.respondWithJSON(w, http.StatusOK, jsonResponse{Error: false, Message: "Merchant platform credit eligible fee types retrieved successfully", Data: associations})
}

func (app *Application) CheckMerchantPlatformCreditEligibleFeeTypeHandler(w http.ResponseWriter, r *http.Request) {
	logger := app.Logger.GetLoggerWithContext(r).WithFunctionName("CheckMerchantPlatformCreditEligibleFeeTypeHandler")

	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	if !app.HasPermission(ctx, actionCheckMerchantPlatformCreditEligibleFeeType) {
		app.respondWithError(w, errors.New("forbidden: insufficient permissions"), http.StatusForbidden)
		return
	}

	userID := app.getUserIDFromContext(ctx)
	if userID == nil {
		app.respondWithError(w, errors.New("user ID not found in context"), http.StatusUnauthorized)
		return
	}

	accountID, feeType, err := app.parseMerchantPlatformCreditEligibleFeeTypeKey(r)
	if err != nil {
		app.respondWithError(w, err, http.StatusBadRequest)
		return
	}

	eligible, err := app.Models.MerchantPlatformCreditEligibleFeeType.IsEligible(ctx, accountID, feeType)
	if err != nil {
		logger.Error("Check merchant platform credit fee-type eligibility failed", "credit_account_id", accountID, "fee_type", feeType, "error", err)
		app.respondWithError(w, err, merchantPlatformCreditEligibleFeeTypeHTTPStatus(err))
		return
	}

	responseData := checkMerchantPlatformCreditEligibleFeeTypeResponse{CreditAccountID: accountID, FeeType: feeType, Eligible: eligible}
	entityID := merchantPlatformCreditEligibleFeeTypeEntityID(accountID, feeType)
	if err := app.insertGovernanceAudit(ctx, userID, actionCheckMerchantPlatformCreditEligibleFeeType, "Check a merchant platform credit fee-type eligibility association", merchantPlatformCreditEligibleFeeTypeEntityType, merchantPlatformCreditEligibleFeeTypeEntityTypeDescription, entityID); err != nil {
		logger.Warn("Audit logging failed", "entity_id", entityID, "error", err)
		app.respondWithJSON(w, http.StatusOK, jsonResponse{Error: false, Message: "Merchant platform credit fee-type eligibility checked, but audit logging failed", Data: responseData})
		return
	}

	app.respondWithJSON(w, http.StatusOK, jsonResponse{Error: false, Message: "Merchant platform credit fee-type eligibility checked successfully", Data: responseData})
}

func (app *Application) DeleteMerchantPlatformCreditEligibleFeeTypeHandler(w http.ResponseWriter, r *http.Request) {
	logger := app.Logger.GetLoggerWithContext(r).WithFunctionName("DeleteMerchantPlatformCreditEligibleFeeTypeHandler")

	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	if !app.HasPermission(ctx, actionDeleteMerchantPlatformCreditEligibleFeeType) {
		app.respondWithError(w, errors.New("forbidden: insufficient permissions"), http.StatusForbidden)
		return
	}

	userID := app.getUserIDFromContext(ctx)
	if userID == nil {
		app.respondWithError(w, errors.New("user ID not found in context"), http.StatusUnauthorized)
		return
	}

	accountID, feeType, err := app.parseMerchantPlatformCreditEligibleFeeTypeKey(r)
	if err != nil {
		app.respondWithError(w, err, http.StatusBadRequest)
		return
	}

	if err := app.Models.MerchantPlatformCreditEligibleFeeType.Delete(ctx, accountID, feeType); err != nil {
		logger.Error("Delete merchant platform credit eligible fee type failed", "credit_account_id", accountID, "fee_type", feeType, "error", err)
		app.respondWithError(w, err, merchantPlatformCreditEligibleFeeTypeHTTPStatus(err))
		return
	}

	responseData := merchantPlatformCreditEligibleFeeTypeMutationResponse{CreditAccountID: accountID, FeeType: feeType}
	entityID := merchantPlatformCreditEligibleFeeTypeEntityID(accountID, feeType)
	if err := app.insertGovernanceAudit(ctx, userID, actionDeleteMerchantPlatformCreditEligibleFeeType, "Delete a merchant platform credit fee-type eligibility association", merchantPlatformCreditEligibleFeeTypeEntityType, merchantPlatformCreditEligibleFeeTypeEntityTypeDescription, entityID); err != nil {
		logger.Warn("Audit logging failed", "entity_id", entityID, "error", err)
		app.respondWithJSON(w, http.StatusOK, jsonResponse{Error: false, Message: "Merchant platform credit eligible fee type deleted, but audit logging failed", Data: responseData})
		return
	}

	logger.Info("Merchant platform credit eligible fee type deleted", "credit_account_id", accountID, "fee_type", feeType)
	app.respondWithJSON(w, http.StatusOK, jsonResponse{Error: false, Message: "Merchant platform credit eligible fee type deleted successfully", Data: responseData})
}

func (app *Application) ReplaceMerchantPlatformCreditEligibleFeeTypeSetHandler(w http.ResponseWriter, r *http.Request) {
	logger := app.Logger.GetLoggerWithContext(r).WithFunctionName("ReplaceMerchantPlatformCreditEligibleFeeTypeSetHandler")

	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	if !app.HasPermission(ctx, actionReplaceMerchantPlatformCreditEligibleFeeTypeSet) {
		app.respondWithError(w, errors.New("forbidden: insufficient permissions"), http.StatusForbidden)
		return
	}

	userID := app.getUserIDFromContext(ctx)
	if userID == nil {
		app.respondWithError(w, errors.New("user ID not found in context"), http.StatusUnauthorized)
		return
	}

	accountID, err := app.parseMerchantPlatformCreditAccountID(r)
	if err != nil {
		app.respondWithError(w, err, http.StatusBadRequest)
		return
	}

	var input replaceMerchantPlatformCreditEligibleFeeTypeSetRequest
	if err := app.readJSON(w, r, &input); err != nil {
		app.respondWithError(w, errors.New("invalid JSON input: "+err.Error()), http.StatusBadRequest)
		return
	}
	if !input.FeeTypes.Set {
		app.respondWithError(w, errors.New("fee_types is required; use an empty array to clear the set"), http.StatusBadRequest)
		return
	}

	replaced, err := app.Models.MerchantPlatformCreditEligibleFeeType.ReplaceSet(ctx, accountID, input.FeeTypes.Value)
	if err != nil {
		logger.Error("Replace merchant platform credit eligible fee type set failed", "credit_account_id", accountID, "error", err)
		app.respondWithError(w, err, merchantPlatformCreditEligibleFeeTypeHTTPStatus(err))
		return
	}
	if replaced == nil {
		replaced = make([]*data.MerchantPlatformCreditEligibleFeeType, 0)
	}

	if err := app.insertGovernanceAudit(ctx, userID, actionReplaceMerchantPlatformCreditEligibleFeeTypeSet, "Replace a merchant platform credit account's fee-type eligibility set", merchantPlatformCreditEligibleFeeTypeEntityType, merchantPlatformCreditEligibleFeeTypeEntityTypeDescription, accountID.String()); err != nil {
		logger.Warn("Audit logging failed", "credit_account_id", accountID, "error", err)
		app.respondWithJSON(w, http.StatusOK, jsonResponse{Error: false, Message: "Merchant platform credit eligible fee type set replaced, but audit logging failed", Data: replaced})
		return
	}

	logger.Info("Merchant platform credit eligible fee type set replaced", "credit_account_id", accountID, "row_count", len(replaced))
	app.respondWithJSON(w, http.StatusOK, jsonResponse{Error: false, Message: "Merchant platform credit eligible fee type set replaced successfully", Data: replaced})
}
