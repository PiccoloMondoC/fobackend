// Package main provides merchant-facing Future Offering engagement configuration HTTP boundaries.
// focodebase/fobackend/internal/server/cmd/api/merchant_future_offering_engagement.go
package main

import (
	"context"
	"errors"
	"github.com/PiccoloMondoC/focodebase/fobackend/internal/services"
	"github.com/google/uuid"
	"net/http"
	"time"
)

const actionUpdateMerchantFutureOfferingEngagement = "update_merchant_future_offering_engagement"

type engagementActionResponse struct {
	ID          uuid.UUID `json:"id"`
	Code        string    `json:"code"`
	Name        string    `json:"name"`
	Description *string   `json:"description,omitempty"`
}
type engagementActionListResponse struct {
	EngagementActions []engagementActionResponse `json:"engagement_actions"`
}
type engagementOptionRequest struct {
	EngagementActionID uuid.UUID `json:"engagement_action_id"`
	QuantityEnabled    bool      `json:"quantity_enabled"`
	MinQuantity        *int      `json:"min_quantity"`
	MaxQuantity        *int      `json:"max_quantity"`
}
type engagementGroupRequest struct {
	Name          *string                   `json:"name"`
	MaxSelections *int                      `json:"max_selections"`
	Options       []engagementOptionRequest `json:"options"`
}
type replaceEngagementConfigurationRequest struct {
	ExpectedUpdatedAt time.Time                `json:"expected_updated_at"`
	Groups            []engagementGroupRequest `json:"groups"`
}
type engagementOptionResponse struct {
	ID                 uuid.UUID `json:"id"`
	EngagementActionID uuid.UUID `json:"engagement_action_id"`
	QuantityEnabled    bool      `json:"quantity_enabled"`
	MinQuantity        *int      `json:"min_quantity,omitempty"`
	MaxQuantity        *int      `json:"max_quantity,omitempty"`
}
type engagementGroupResponse struct {
	ID            uuid.UUID                  `json:"id"`
	Name          *string                    `json:"name,omitempty"`
	MaxSelections *int                       `json:"max_selections,omitempty"`
	Options       []engagementOptionResponse `json:"options"`
}
type engagementConfigurationResponse struct {
	FutureOfferingID uuid.UUID                 `json:"future_offering_id"`
	UpdatedAt        time.Time                 `json:"updated_at"`
	Groups           []engagementGroupResponse `json:"groups"`
}

func newEngagementConfigurationResponse(c services.MerchantFutureOfferingEngagementConfiguration) engagementConfigurationResponse {
	out := engagementConfigurationResponse{FutureOfferingID: c.FutureOffering.ID, UpdatedAt: c.FutureOffering.UpdatedAt, Groups: make([]engagementGroupResponse, 0, len(c.Groups))}
	for _, g := range c.Groups {
		gr := engagementGroupResponse{ID: g.Group.ID, Name: g.Group.Name, MaxSelections: g.Group.MaxSelections, Options: make([]engagementOptionResponse, 0, len(g.Options))}
		for _, o := range g.Options {
			gr.Options = append(gr.Options, engagementOptionResponse{ID: o.ID, EngagementActionID: o.EngagementActionID, QuantityEnabled: o.QuantityEnabled, MinQuantity: o.MinQuantity, MaxQuantity: o.MaxQuantity})
		}
		out.Groups = append(out.Groups, gr)
	}
	return out
}
func (app *Application) ListEngagementActionsHandler(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()
	actions, err := app.Models.EngagementAction.ListActive(ctx)
	if err != nil {
		app.serverErrorResponse(app.Logger.GetLoggerWithContext(r), w, r, err)
		return
	}
	resp := engagementActionListResponse{EngagementActions: make([]engagementActionResponse, 0, len(actions))}
	for _, a := range actions {
		resp.EngagementActions = append(resp.EngagementActions, engagementActionResponse{ID: a.ID, Code: a.Code, Name: a.Name, Description: a.Description})
	}
	app.respondWithJSON(w, http.StatusOK, jsonResponse{Error: false, Message: "Engagement actions retrieved", Data: resp})
}
func (app *Application) GetMerchantFutureOfferingEngagementHandler(w http.ResponseWriter, r *http.Request) {
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
	c, err := app.InternalServices.GetMerchantFutureOfferingEngagementConfiguration(ctx, merchantID, foID)
	if err != nil {
		app.respondMerchantFutureOfferingServiceError(w, r, err)
		return
	}
	app.respondWithJSON(w, http.StatusOK, jsonResponse{Error: false, Message: "Engagement configuration retrieved", Data: newEngagementConfigurationResponse(c)})
}
func (app *Application) ReplaceMerchantFutureOfferingEngagementHandler(w http.ResponseWriter, r *http.Request) {
	logger := app.Logger.GetLoggerWithContext(r).WithFunctionName("ReplaceMerchantFutureOfferingEngagementHandler")
	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()
	merchantID, err := app.requireAuthorizedMerchantFutureOfferingMerchant(ctx)
	if err != nil {
		app.respondMerchantFutureOfferingAuthorizationError(w, r, err)
		return
	}
	userID := app.getUserIDFromContext(ctx)
	foID, ok := app.parseFutureOfferingIDParam(w, r)
	if !ok {
		return
	}
	var input replaceEngagementConfigurationRequest
	if err := app.readJSON(w, r, &input); err != nil {
		app.respondWithError(w, errors.New("malformed request body"), http.StatusBadRequest)
		return
	}
	if input.ExpectedUpdatedAt.IsZero() {
		app.respondWithError(w, errors.New("expected_updated_at is required"), http.StatusBadRequest)
		return
	}
	groups := make([]services.MerchantFutureOfferingEngagementGroupInput, 0, len(input.Groups))
	for _, g := range input.Groups {
		opts := make([]services.MerchantFutureOfferingEngagementOptionInput, 0, len(g.Options))
		for _, o := range g.Options {
			opts = append(opts, services.MerchantFutureOfferingEngagementOptionInput{EngagementActionID: o.EngagementActionID, QuantityEnabled: o.QuantityEnabled, MinQuantity: o.MinQuantity, MaxQuantity: o.MaxQuantity})
		}
		groups = append(groups, services.MerchantFutureOfferingEngagementGroupInput{Name: g.Name, MaxSelections: g.MaxSelections, Options: opts})
	}
	c, err := app.InternalServices.ReplaceMerchantFutureOfferingEngagementConfiguration(ctx, merchantID, foID, input.ExpectedUpdatedAt, groups)
	if err != nil {
		app.respondMerchantFutureOfferingServiceError(w, r, err)
		return
	}
	if err := app.insertGovernanceAudit(ctx, userID, actionUpdateMerchantFutureOfferingEngagement, "Replaced engagement configuration of a merchant Future Offering draft", merchantFutureOfferingEntityType, merchantFutureOfferingEntityTypeDescription, foID.String()); err != nil {
		logger.Warn("Engagement configuration saved but audit recording failed", "future_offering_id", foID, "error", err)
	}
	app.respondWithJSON(w, http.StatusOK, jsonResponse{Error: false, Message: "Engagement configuration saved", Data: newEngagementConfigurationResponse(c)})
}
