// Package main provides authenticated merchant registration, draft persistence, completion, and business management.
// focodebase/fobackend/internal/server/cmd/api/merchant_onboarding.go
//
// registration (Foundation Level 4 — Become a Merchant).
//
// GTM:
//
//	Layer: 2.4 Merchant / Future Offering Domain
//	Release Class: SPINE
//	Reason:
//	  A signed-in account holder saves, resumes, and completes a merchant
//	  registration and then manages their business information. Completion
//	  establishes the canonical Merchant and principal-owned Merchant Account.
//
// Contract (all routes require AuthMiddleware; the principal is always the
// authenticated caller):
//
//	GET  /api/v1/merchant-onboarding/registration
//	     Own registration state. Never another principal's record.
//	PUT  /api/v1/merchant-onboarding/registration
//	     Full-replacement save. Draft: no fact required. Completed: the full
//	     completion rule set applies (business-information management).
//	POST /api/v1/merchant-onboarding/registration/complete
//	     Save the submitted facts, then complete. Idempotent.
//
//	The earlier one-shot POST /api/v1/merchant-onboarding/ (name and optional
//	website) is retired: it created an active Merchant without the eight
//	required registration facts. Its only consumer, the Angular onboarding
//	page, is replaced in the same delivery.
//
// SPINE Rule:
//
//	Keep compiling.
//	Keep production-ready.
//	Derive the principal exclusively from trusted authentication context.
//	Accept no principal, merchant, or account identifiers from clients.
//	Respond with Cache-Control: no-store; registration facts are private.
//	Never echo submitted values in errors and never log registration facts.
//	Return stable machine-readable error codes; clients never parse messages.
package main

import (
	"context"
 "bytes"
 "encoding/json"
 "io"
	"errors"
	"net/http"
	"time"

	"github.com/PiccoloMondoC/focodebase/fobackend/internal/data"
	"github.com/PiccoloMondoC/focodebase/fobackend/internal/services"

	"github.com/google/uuid"
)

// merchantRegistrationMaxBodyBytes bounds registration request bodies. The
// largest valid body is well under 8 KiB.
const merchantRegistrationMaxBodyBytes = 32 << 10

// merchantRegistrationRequest is the strict full-replacement registration
// body. Unknown keys are rejected by readJSON. JSON null and absent keys both
// clear a fact; blank strings normalize to null.
type merchantRegistrationRequest struct {
	ExpectedRevision  *int64  `json:"expected_revision"`
	LegalName         *string `json:"legal_name"`
	DBAName           *string `json:"dba_name"`
	TaxID             *string `json:"tax_id"`
	AddressLine1      *string `json:"address_line1"`
	AddressLine2      *string `json:"address_line2"`
	AddressCity       *string `json:"address_city"`
	AddressRegion     *string `json:"address_region"`
	AddressPostalCode *string `json:"address_postal_code"`
	AddressCountry    *string `json:"address_country"`
	CorporateEmail    *string `json:"corporate_email"`
	ContactName       *string `json:"contact_name"`
	Phone             *string `json:"phone"`
	Website           *string `json:"website"`
}

func (r merchantRegistrationRequest) input() services.MerchantRegistrationInput {
	var expected int64
	if r.ExpectedRevision != nil {
		expected = *r.ExpectedRevision
	}
	return services.MerchantRegistrationInput{
		ExpectedRevision: expected,
		Fields: data.MerchantRegistrationFields{
			LegalName:         r.LegalName,
			DBAName:           r.DBAName,
			TaxID:             r.TaxID,
			AddressLine1:      r.AddressLine1,
			AddressLine2:      r.AddressLine2,
			AddressCity:       r.AddressCity,
			AddressRegion:     r.AddressRegion,
			AddressPostalCode: r.AddressPostalCode,
			AddressCountry:    r.AddressCountry,
			CorporateEmail:    r.CorporateEmail,
			ContactName:       r.ContactName,
			Phone:             r.Phone,
			Website:           r.Website,
		},
	}
}

// merchantRegistrationFieldsResponse is the owner-only projection of the
// registration facts. Every key is always present; absent facts are null.
type merchantRegistrationFieldsResponse struct {
	LegalName         *string `json:"legal_name"`
	DBAName           *string `json:"dba_name"`
	TaxID             *string `json:"tax_id"`
	AddressLine1      *string `json:"address_line1"`
	AddressLine2      *string `json:"address_line2"`
	AddressCity       *string `json:"address_city"`
	AddressRegion     *string `json:"address_region"`
	AddressPostalCode *string `json:"address_postal_code"`
	AddressCountry    *string `json:"address_country"`
	CorporateEmail    *string `json:"corporate_email"`
	ContactName       *string `json:"contact_name"`
	Phone             *string `json:"phone"`
	Website           *string `json:"website"`
}

// merchantCenterMerchantResponse identifies the principal's canonical
// Merchant and Merchant Account. PublicName is the public DBA / brand name.
type merchantCenterMerchantResponse struct {
	MerchantID        uuid.UUID  `json:"merchant_id"`
	MerchantAccountID uuid.UUID  `json:"merchant_account_id"`
	AccountStatus     string     `json:"account_status"`
	PublicName        string     `json:"public_name"`
	OnboardedAt       *time.Time `json:"onboarded_at"`
}

// merchantRegistrationResponse is the stable owner-only Merchant Center
// registration contract.
type merchantRegistrationResponse struct {
	Eligible    bool                               `json:"eligible"`
	State       string                             `json:"state"`
	Revision    int64                              `json:"revision"`
	Fields      merchantRegistrationFieldsResponse `json:"fields"`
	Progress    data.MerchantRegistrationProgress  `json:"progress"`
	CreatedAt   *time.Time                         `json:"created_at"`
	UpdatedAt   *time.Time                         `json:"updated_at"`
	CompletedAt *time.Time                         `json:"completed_at"`
	Merchant    *merchantCenterMerchantResponse    `json:"merchant"`
}

const (
	merchantRegistrationStateNone      = "none"
	merchantRegistrationStateDraft     = "draft"
	merchantRegistrationStateCompleted = "completed"
)

// buildMerchantRegistrationResponse maps the service view into the owner-only
// contract. It creates no authority.
func buildMerchantRegistrationResponse(
	view *services.MerchantRegistrationView,
) merchantRegistrationResponse {
	response := merchantRegistrationResponse{
		State:    merchantRegistrationStateNone,
		Progress: data.AssessMerchantRegistrationProgress(data.MerchantRegistrationFields{}),
	}
	if view == nil {
		return response
	}

	response.Eligible = view.Eligible

	if registration := view.Registration; registration != nil {
		fields := registration.Fields
		response.State = merchantRegistrationStateDraft
		if registration.Status == data.MerchantRegistrationStatusCompleted {
			response.State = merchantRegistrationStateCompleted
		}
		response.Revision = registration.Revision
		response.Fields = merchantRegistrationFieldsResponse{
			LegalName:         fields.LegalName,
			DBAName:           fields.DBAName,
			TaxID:             fields.TaxID,
			AddressLine1:      fields.AddressLine1,
			AddressLine2:      fields.AddressLine2,
			AddressCity:       fields.AddressCity,
			AddressRegion:     fields.AddressRegion,
			AddressPostalCode: fields.AddressPostalCode,
			AddressCountry:    fields.AddressCountry,
			CorporateEmail:    fields.CorporateEmail,
			ContactName:       fields.ContactName,
			Phone:             fields.Phone,
			Website:           fields.Website,
		}
		response.Progress = data.AssessMerchantRegistrationProgress(fields)
		createdAt := registration.CreatedAt
		updatedAt := registration.UpdatedAt
		response.CreatedAt = &createdAt
		response.UpdatedAt = &updatedAt
		response.CompletedAt = registration.CompletedAt
	}

	if view.Account != nil {
		merchant := &merchantCenterMerchantResponse{
			MerchantID:        view.Account.MerchantID,
			MerchantAccountID: view.Account.ID,
			AccountStatus:     string(view.Account.AccountStatus),
			OnboardedAt:       view.Account.OnboardedAt,
		}
		if view.Merchant != nil {
			merchant.PublicName = view.Merchant.Name
		}
		response.Merchant = merchant
	}

	return response
}

// merchantRegistrationValidationResponse is the field-level error payload.
type merchantRegistrationValidationResponse struct {
	Code   string                               `json:"code"`
	Fields data.MerchantRegistrationFieldErrors `json:"fields"`
}

// merchantRegistrationErrorStatus classifies a service error into an HTTP
// status, a stable code, a client-safe message, and optional field errors.
// ok is false for errors that must be treated as server failures.
func merchantRegistrationErrorStatus(err error) (
	status int,
	code string,
	message string,
	fields data.MerchantRegistrationFieldErrors,
	ok bool,
) {
	var validation *data.MerchantRegistrationValidationError
	switch {
	case errors.As(err, &validation):
		status = http.StatusBadRequest
		for _, fieldCode := range validation.Fields {
			if fieldCode == data.MerchantRegistrationErrTaken {
				status = http.StatusConflict
			}
		}
		return status, "validation_failed", "Please check the highlighted details.", validation.Fields, true
	case errors.Is(err, data.ErrMerchantRegistrationInvalid):
		return http.StatusBadRequest, "invalid_request", "Please check the registration details.", nil, true
	case errors.Is(err, services.ErrMerchantOnboardingActorIneligible):
		return http.StatusForbidden, "merchant_registration_unavailable",
			"Merchant registration isn't available for this account.", nil, true
	case errors.Is(err, services.ErrMerchantRegistrationEditConflict):
		return http.StatusConflict, "registration_edit_conflict",
			"Your registration changed elsewhere. Load the latest version before saving.", nil, true
	case errors.Is(err, services.ErrMerchantRegistrationPrincipalUnavailable):
		return http.StatusConflict, "merchant_account_unavailable",
			"Your merchant account can't be completed online. Please contact support.", nil, true
	case errors.Is(err, context.DeadlineExceeded):
		return http.StatusGatewayTimeout, "request_timeout",
			"The result could not be confirmed. Load your saved registration before retrying.", nil, true
	default:
		return http.StatusInternalServerError, "merchant_registration_failed",
			"The result could not be confirmed. Load your saved registration before retrying.", nil, false
	}
}

func (app *Application) respondMerchantRegistrationError(
	w http.ResponseWriter,
	r *http.Request,
	functionName string,
	err error,
) {
	if errors.Is(err, context.Canceled) {
		return
	}

	status, code, message, fields, ok := merchantRegistrationErrorStatus(err)
	if !ok {
		// Service and data errors never carry registration values; the
		// driver's constraint detail is translated away in the data layer.
		app.Logger.
			GetLoggerWithContext(r).
			WithFunctionName(functionName).
			Error("merchant registration operation failed", "error", err)
	}

	if fields != nil {
		app.respondWithJSON(w, status, jsonResponse{
			Error:   true,
			Message: message,
			Data: merchantRegistrationValidationResponse{
				Code:   code,
				Fields: fields,
			},
		})
		return
	}

	app.respondWithErrorCode(w, status, code, message)
}

// merchantRegistrationActor establishes the trusted principal and the
// response privacy headers.
func (app *Application) merchantRegistrationActor(
	w http.ResponseWriter,
	r *http.Request,
) (uuid.UUID, bool) {
	w.Header().Set("Cache-Control", "no-store")
	userID := app.getUserIDFromContext(r.Context())
	if userID == nil || *userID == uuid.Nil {
		app.respondWithErrorCode(w, http.StatusUnauthorized, "authentication_required", "Please sign in.")
		return uuid.Nil, false
	}
	return *userID, true
}

func (app *Application) decodeMerchantRegistrationRequest(w http.ResponseWriter,r *http.Request) (merchantRegistrationRequest,bool) {
 var request merchantRegistrationRequest
 bad:=func() (merchantRegistrationRequest,bool) {
  app.respondWithErrorCode(w,http.StatusBadRequest,"invalid_request","Supply one registration object with a nonnegative expected_revision.")
  return request,false
 }
 body,err:=io.ReadAll(http.MaxBytesReader(w,r.Body,merchantRegistrationMaxBodyBytes))
 if err!=nil {
  var limit *http.MaxBytesError
  if errors.As(err,&limit){
   app.respondWithErrorCode(w,http.StatusRequestEntityTooLarge,"request_too_large","The registration request is too large.")
   return request,false
  }
  return bad()
 }
 // This is a flat scalar contract. Reject duplicate keys instead of relying
 // on encoding/json's last-value-wins behavior.
 dec:=json.NewDecoder(bytes.NewReader(body))
 first,err:=dec.Token();if err!=nil||first!=json.Delim('{'){return bad()}
 seen:=map[string]bool{}
 for dec.More(){
  token,err:=dec.Token();if err!=nil{return bad()}
  key,ok:=token.(string);if !ok||seen[key]{return bad()};seen[key]=true
  var raw json.RawMessage;if dec.Decode(&raw)!=nil{return bad()}
 }
 if _,err=dec.Token();err!=nil{return bad()}
 var extra any;if dec.Decode(&extra)!=io.EOF{return bad()}
 strict:=json.NewDecoder(bytes.NewReader(body));strict.DisallowUnknownFields()
 if strict.Decode(&request)!=nil||request.ExpectedRevision==nil||*request.ExpectedRevision<0{return bad()}
 return request,true
}

// GetOwnMerchantRegistrationHandler returns the caller's own registration.
func (app *Application) GetOwnMerchantRegistrationHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	userID, ok := app.merchantRegistrationActor(w, r)
	if !ok {
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	view, err := app.InternalServices.GetOwnMerchantRegistrationInternal(ctx, userID)
	if err != nil {
		app.respondMerchantRegistrationError(w, r, "GetOwnMerchantRegistrationHandler", err)
		return
	}

	app.respondWithJSON(w, http.StatusOK, jsonResponse{
		Error:   false,
		Message: "Merchant registration retrieved successfully",
		Data:    buildMerchantRegistrationResponse(view),
	})
}

// SaveOwnMerchantRegistrationHandler saves the caller's registration draft or
// completed business information.
func (app *Application) SaveOwnMerchantRegistrationHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	userID, ok := app.merchantRegistrationActor(w, r)
	if !ok {
		return
	}

	request, ok := app.decodeMerchantRegistrationRequest(w, r)
	if !ok {
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	view, err := app.InternalServices.SaveOwnMerchantRegistrationInternal(ctx, userID, request.input())
	if err != nil {
		app.respondMerchantRegistrationError(w, r, "SaveOwnMerchantRegistrationHandler", err)
		return
	}

	app.respondWithJSON(w, http.StatusOK, jsonResponse{
		Error:   false,
		Message: "Merchant registration saved",
		Data:    buildMerchantRegistrationResponse(view),
	})
}

// CompleteOwnMerchantRegistrationHandler saves and completes the caller's
// registration, establishing the caller as the principal of the canonical
// Merchant and active Merchant Account.
func (app *Application) CompleteOwnMerchantRegistrationHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	userID, ok := app.merchantRegistrationActor(w, r)
	if !ok {
		return
	}

	request, ok := app.decodeMerchantRegistrationRequest(w, r)
	if !ok {
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	result, err := app.InternalServices.CompleteOwnMerchantRegistrationInternal(ctx, userID, request.input())
	if err != nil {
		app.respondMerchantRegistrationError(w, r, "CompleteOwnMerchantRegistrationHandler", err)
		return
	}

	if result.NewlyCompleted && result.View != nil && result.View.Account != nil {
		if auditErr := app.insertGovernanceAudit(
			ctx,
			&userID,
			"complete_merchant_registration",
			"Complete merchant registration and establish the merchant principal",
			"merchant",
			"Merchant entity",
			result.View.Account.MerchantID.String(),
		); auditErr != nil {
			app.Logger.
				GetLoggerWithContext(r).
				WithFunctionName("CompleteOwnMerchantRegistrationHandler").
				Warn(
					"merchant registration completed but audit recording failed",
					"merchant_id", result.View.Account.MerchantID,
					"error", auditErr,
				)
		}
	}

	app.respondWithJSON(w, http.StatusOK, jsonResponse{
		Error:   false,
		Message: "Merchant registration completed",
		Data:    buildMerchantRegistrationResponse(result.View),
	})
}

// OnboardMerchantHandler deliberately retains the established endpoint, with
// an explicit retirement response. It cannot bypass eight-field completion.
func (app *Application) OnboardMerchantHandler(w http.ResponseWriter,r *http.Request) {
 w.Header().Set("Cache-Control","no-store")
 app.respondWithErrorCode(w,http.StatusGone,"merchant_registration_required","Use Merchant Center to save and complete your business registration.")
}
