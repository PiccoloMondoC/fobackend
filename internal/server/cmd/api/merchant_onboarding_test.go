// Package main provides regression specifications for authenticated merchant registration, draft persistence, completion, and business management.
// focodebase/fobackend/internal/server/cmd/api/merchant_onboarding_test.go
//
// GTM:
//     Layer: 3.2 Merchant Registration / HTTP and Authorization
//     Release Class: SPINE
//     Reason: Supports the complete Foundation Level 4 merchant-registration and account-navigation vertical.
//
// SPINE Rule:
//     Keep compiling and preserve established security, lifecycle, and privacy contracts.
//     Preserve canonical principal ownership and backend authorization.
//     Keep private registration values out of public projections, logs, and errors.
//
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/PiccoloMondoC/focodebase/fobackend/internal/data"
	"github.com/PiccoloMondoC/focodebase/fobackend/internal/services"

	"github.com/google/uuid"
)

func onboardingStr(s string) *string { return &s }

func TestBuildMerchantRegistrationResponse_NoneIsCompleteContract(t *testing.T) {
	body, err := json.Marshal(buildMerchantRegistrationResponse(&services.MerchantRegistrationView{Eligible: true}))
	if err != nil {
		t.Fatal(err)
	}

	var decoded map[string]any
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded["state"] != "none" || decoded["eligible"] != true || decoded["merchant"] != nil {
		t.Fatalf("unexpected none contract: %s", body)
	}
	fields, ok := decoded["fields"].(map[string]any)
	if !ok || len(fields) != 13 {
		t.Fatalf("every field key must be present (as null): %s", body)
	}
	progress := decoded["progress"].(map[string]any)
	if progress["total_facts"].(float64) != 8 || progress["completed_facts"].(float64) != 0 {
		t.Fatalf("unexpected progress: %v", progress)
	}
	if missing, ok := progress["missing_facts"].([]any); !ok || len(missing) != 8 {
		t.Fatalf("missing_facts must be an array of eight: %v", progress["missing_facts"])
	}
}

func TestBuildMerchantRegistrationResponse_MapsDraftAndCompleted(t *testing.T) {
	now := time.Now().UTC()
	merchantID := uuid.New()
	view := &services.MerchantRegistrationView{
		Eligible: true,
		Registration: &data.MerchantRegistration{
			Status:   data.MerchantRegistrationStatusDraft,
			Revision: 3,
			Fields: data.MerchantRegistrationFields{
				DBAName: onboardingStr("Acme"),
				TaxID:   onboardingStr("12-3456789"),
			},
			CreatedAt: now,
			UpdatedAt: now,
		},
	}

	draft := buildMerchantRegistrationResponse(view)
	if draft.State != "draft" || draft.Revision != 3 || draft.Merchant != nil ||
		draft.Progress.CompletedFacts != 2 || *draft.Fields.TaxID != "12-3456789" {
		t.Fatalf("unexpected draft mapping: %+v", draft)
	}

	view.Registration.Status = data.MerchantRegistrationStatusCompleted
	view.Registration.CompletedAt = &now
	view.Registration.MerchantID = &merchantID
	view.Account = &data.MerchantAccount{
		ID:            uuid.New(),
		MerchantID:    merchantID,
		AccountStatus: data.MerchantAccountStatusActive,
		OnboardedAt:   &now,
	}
	view.Merchant = &data.Merchant{ID: merchantID, Name: "Acme"}

	completed := buildMerchantRegistrationResponse(view)
	if completed.State != "completed" || completed.Merchant == nil ||
		completed.Merchant.PublicName != "Acme" || completed.Merchant.AccountStatus != "active" ||
		completed.Merchant.MerchantID != merchantID {
		t.Fatalf("unexpected completed mapping: %+v", completed.Merchant)
	}
}

func TestMerchantRegistrationErrorStatus_StableCodes(t *testing.T) {
	cases := []struct {
		name   string
		err    error
		status int
		code   string
	}{
		{"field validation", data.NewMerchantRegistrationValidationError(data.MerchantRegistrationFieldErrors{"phone": "invalid"}), 400, "validation_failed"},
		{"taken brand", data.NewMerchantRegistrationValidationError(data.MerchantRegistrationFieldErrors{"dba_name": "taken"}), 409, "validation_failed"},
		{"generic invalid", fmt.Errorf("%w: x", data.ErrMerchantRegistrationInvalid), 400, "invalid_request"},
		{"ineligible", services.ErrMerchantOnboardingActorIneligible, 403, "merchant_registration_unavailable"},
		{"conflict", services.ErrMerchantRegistrationEditConflict, 409, "registration_edit_conflict"},
		{"principal unavailable", services.ErrMerchantRegistrationPrincipalUnavailable, 409, "merchant_account_unavailable"},
		{"timeout", fmt.Errorf("wrapped: %w", context.DeadlineExceeded), 504, "request_timeout"},
		{"unknown", errors.New("boom"), 500, "merchant_registration_failed"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			status, code, message, _, _ := merchantRegistrationErrorStatus(tc.err)
			if status != tc.status || code != tc.code {
				t.Fatalf("got %d/%s, want %d/%s", status, code, tc.status, tc.code)
			}
			if strings.Contains(message, "boom") {
				t.Fatal("internal error text must not reach clients")
			}
		})
	}
}

func TestDecodeMerchantRegistrationRequest_IsStrict(t *testing.T) {
	app := &Application{}

	decode := func(body string) (*httptest.ResponseRecorder, merchantRegistrationRequest, bool) {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPut, "/api/v1/merchant-onboarding/registration", strings.NewReader(body))
		decoded, ok := app.decodeMerchantRegistrationRequest(recorder, request)
		return recorder, decoded, ok
	}

	_, decoded, ok := decode(`{"expected_revision":2,"dba_name":"Acme","website":null}`)
	if !ok || decoded.input().ExpectedRevision != 2 || *decoded.input().Fields.DBAName != "Acme" ||
		decoded.input().Fields.Website != nil {
		t.Fatalf("valid body rejected or mis-mapped: %+v", decoded)
	}

	for name, body := range map[string]string{
		"client principal": `{"principal_user_id":"` + uuid.NewString() + `"}`,
		"client merchant":  `{"merchant_id":"` + uuid.NewString() + `"}`,
		"trailing json":    `{} {}`,
		"wrong type":       `{"dba_name":42}`,
	} {
		recorder, _, ok := decode(body)
		if ok || recorder.Code != http.StatusBadRequest {
			t.Errorf("%s: expected 400, got ok=%v status=%d", name, ok, recorder.Code)
		}
	}

	oversized := `{"legal_name":"` + strings.Repeat("a", merchantRegistrationMaxBodyBytes) + `"}`
	recorder, _, ok := decode(oversized)
	if ok || recorder.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversized body: expected 413, got %d", recorder.Code)
	}
}

func TestRespondMerchantRegistrationError_FieldPayloadHasNoValues(t *testing.T) {
	app := &Application{}
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPut, "/", bytes.NewReader(nil))

	app.respondMerchantRegistrationError(recorder, request, "test",
		data.NewMerchantRegistrationValidationError(data.MerchantRegistrationFieldErrors{"tax_id": "invalid"}))

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status %d", recorder.Code)
	}
	var envelope struct {
		Error bool `json:"error"`
		Data  struct {
			Code   string            `json:"code"`
			Fields map[string]string `json:"fields"`
		} `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if !envelope.Error || envelope.Data.Code != "validation_failed" || envelope.Data.Fields["tax_id"] != "invalid" {
		t.Fatalf("unexpected payload: %s", recorder.Body.String())
	}
}

func TestMerchantRegistrationActor_RequiresTrustedIdentityAndNoStore(t *testing.T) {
	app := &Application{}
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/merchant-onboarding/registration", nil)
	request.Header.Set("X-Target-User-ID", uuid.NewString())

	if _, ok := app.merchantRegistrationActor(recorder, request); ok {
		t.Fatal("a request without AuthMiddleware identity must be refused")
	}
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", recorder.Code)
	}
	if recorder.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("registration responses must not be cached")
	}

	userID := uuid.New()
	recorder = httptest.NewRecorder()
	request = request.WithContext(context.WithValue(request.Context(), ctxUserID, userID))
	got, ok := app.merchantRegistrationActor(recorder, request)
	if !ok || got != userID {
		t.Fatal("the authenticated identity must be the principal")
	}
}
