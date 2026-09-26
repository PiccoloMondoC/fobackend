// Package main provides merchant-facing readiness and lifecycle-history HTTP boundaries.
//
// focodebase/fobackend/internal/server/cmd/api/merchant_future_offering_submission.go
//
// Submission is intentionally not exposed until the complete M01 readiness composite exists.
package main

import (
	"context"
	"errors"
	"github.com/PiccoloMondoC/focodebase/fobackend/internal/data"
	"github.com/PiccoloMondoC/focodebase/fobackend/internal/services"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"net/http"
	"time"
)

type merchantFutureOfferingHistoryItemResponse struct {
	ID         uuid.UUID `json:"id"`
	EventType  string    `json:"event_type"`
	FromStatus *string   `json:"from_status,omitempty"`
	ToStatus   *string   `json:"to_status,omitempty"`
	Note       *string   `json:"note,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
}
type merchantFutureOfferingHistoryResponse struct {
	Events []merchantFutureOfferingHistoryItemResponse `json:"events"`
}

func (app *Application) parseFutureOfferingIDParam(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	id, err := uuid.Parse(chi.URLParam(r, "futureOfferingID"))
	if err != nil || id == uuid.Nil {
		app.respondWithError(w, errors.New("invalid future offering id"), http.StatusBadRequest)
		return uuid.Nil, false
	}
	return id, true
}
func (app *Application) respondMerchantFutureOfferingServiceError(w http.ResponseWriter, r *http.Request, err error) {
	var nr *services.MerchantFutureOfferingNotReadyError
	switch {
	case errors.As(err, &nr):
		app.respondWithJSON(w, http.StatusUnprocessableEntity, jsonResponse{Error: true, Message: "Future Offering is not ready for submission", Data: nr.Readiness})
	case errors.Is(err, data.ErrMerchantFutureOfferingEngagementInvalidInput):
		app.respondWithError(w, err, http.StatusBadRequest)
	case errors.Is(err, data.ErrEngagementActionNotFound):
		app.respondWithError(w, errors.New("an engagement action is unavailable"), http.StatusBadRequest)
	default:
		app.respondMerchantFutureOfferingError(w, r, err)
	}
}
func (app *Application) GetMerchantFutureOfferingReadinessHandler(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()
	merchantID, err := app.requireAuthorizedMerchantFutureOfferingMerchant(ctx)
	if err != nil {
		app.respondMerchantFutureOfferingAuthorizationError(w, r, err)
		return
	}
	foID, ok := app.parseFutureOfferingIDParam(w, r)
	if !ok {
		return
	}
	readiness, err := app.InternalServices.EvaluateMerchantFutureOfferingReadiness(ctx, merchantID, foID)
	if err != nil {
		app.respondMerchantFutureOfferingServiceError(w, r, err)
		return
	}
	app.respondWithJSON(w, http.StatusOK, jsonResponse{Error: false, Message: "Future Offering readiness evaluated", Data: readiness})
}
func (app *Application) ListMerchantFutureOfferingHistoryHandler(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()
	merchantID, err := app.requireAuthorizedMerchantFutureOfferingMerchant(ctx)
	if err != nil {
		app.respondMerchantFutureOfferingAuthorizationError(w, r, err)
		return
	}
	foID, ok := app.parseFutureOfferingIDParam(w, r)
	if !ok {
		return
	}
	if _, err := app.Models.MerchantFutureOffering.GetByIDForMerchant(ctx, merchantID, foID); err != nil {
		app.respondMerchantFutureOfferingError(w, r, err)
		return
	}
	events, err := app.Models.MerchantFutureOfferingEvent.ListForMerchant(ctx, merchantID, foID)
	if err != nil {
		app.respondMerchantFutureOfferingError(w, r, err)
		return
	}
	resp := merchantFutureOfferingHistoryResponse{Events: make([]merchantFutureOfferingHistoryItemResponse, 0, len(events))}
	for _, e := range events {
		resp.Events = append(resp.Events, merchantFutureOfferingHistoryItemResponse{ID: e.ID, EventType: string(e.EventType), FromStatus: e.FromStatus, ToStatus: e.ToStatus, Note: e.Note, CreatedAt: e.CreatedAt})
	}
	app.respondWithJSON(w, http.StatusOK, jsonResponse{Error: false, Message: "Future Offering history retrieved", Data: resp})
}
