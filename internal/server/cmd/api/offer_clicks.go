// Package main provides HTTP handlers for the Sagrenti API.
//
// sdworkspace/sdbackend/internal/server/cmd/api/offer_clicks.go
//
// GTM:
//
//	Layer: 2.5 Catalog / Offer Domain
//	Release Class: DEFERRED
//	Reason:
//	  Offer-click HTTP tracking and administration are valid future commerce
//	  infrastructure, but they are not required for the initial Future
//	  Offering release spine. The data model may remain compile-safe and
//	  production-ready without exposing a routed v1 API family until this
//	  deferred commerce capability is intentionally activated.
//
// DEFERRED Rule:
//
//	Keep compiling.
//	Keep safe.
//	Keep production-ready.
//	Preserve authorization enforcement.
//	Preserve immutable click-event semantics.
//	Preserve DB-owned id and clicked_at lifecycle behavior.
//	Preserve nullable authenticated-user attribution.
//	Preserve canonical textual IP-address persistence.
//	Preserve bounded read paths.
//	Preserve LogOfferClick as the canonical lightweight tracking entry point.
//	Do not introduce update behavior for immutable click records.
//	Do not add aggregate analytics unsupported by the data-layer contract.
//	Do not route into v1 API or UI expansion.
//	Do not add new features.
//	Do not block deployment on this file unless it breaks the build.
package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"

	"github.com/PiccoloMondoC/sdworkspace/sdbackend/internal/data"

	"github.com/google/uuid"
)

const (
	trackOfferClickPermission = "track_offer_click"

	defaultOfferClickLimit = 20
	maxOfferClickLimit     = 100
)

type saveOfferClickInput struct {
	OfferID string `json:"offer_id"`
}

type getOfferClickByIDInput struct {
	ID string `json:"id"`
}

type getOfferClicksByOfferIDInput struct {
	OfferID string `json:"offer_id"`
	Limit   int    `json:"limit"`
	Offset  int    `json:"offset"`
}

// SaveOfferClickHandler records one immutable offer-click event and returns the
// canonical persisted record.
//
// The database owns the generated click ID and clicked_at value. The handler
// supplies only the offer identifier and available request-attribution data.
func (app *Application) SaveOfferClickHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	logger := app.Logger.
		GetLoggerWithContext(r).
		WithFunctionName("SaveOfferClickHandler")

	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	if !app.HasPermission(ctx, trackOfferClickPermission) {
		app.respondWithError(
			w,
			errors.New("forbidden: insufficient permissions"),
			http.StatusForbidden,
		)
		return
	}

	var input saveOfferClickInput
	if err := app.readJSON(w, r, &input); err != nil {
		app.respondWithError(
			w,
			fmt.Errorf("invalid JSON input: %w", err),
			http.StatusBadRequest,
		)
		return
	}

	offerID, err := parseRequiredOfferClickUUID(
		input.OfferID,
		"offer_id",
	)
	if err != nil {
		app.respondWithError(w, err, http.StatusBadRequest)
		return
	}

	offerClick := &data.OfferClick{
		OfferID:   offerID,
		UserID:    app.getUserIDFromContext(ctx),
		IPAddress: canonicalOfferClickRequestIPAddress(r),
		UserAgent: optionalOfferClickString(r.UserAgent()),
		Referrer:  optionalOfferClickString(r.Referer()),
	}

	if err := app.Models.OfferClick.Insert(ctx, offerClick); err != nil {
		logger.Error(
			"Save offer click failed",
			"offer_id", offerID,
			"error", err,
		)
		app.respondWithError(
			w,
			fmt.Errorf("failed to save offer click: %w", err),
			http.StatusInternalServerError,
		)
		return
	}

	logger.Info(
		"Offer click saved",
		"offer_click_id", offerClick.ID,
		"offer_id", offerClick.OfferID,
		"user_id", offerClick.UserID,
	)

	app.respondWithJSON(w, http.StatusCreated, jsonResponse{
		Error:   false,
		Message: "Offer click saved successfully",
		Data:    offerClick,
	})
}

// LogOfferClickHandler records one immutable offer-click event through the
// data layer's canonical lightweight tracking entry point.
//
// LogOfferClick intentionally does not return the generated database record.
// Callers requiring the generated ID and clicked_at value must use
// SaveOfferClickHandler.
func (app *Application) LogOfferClickHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	logger := app.Logger.
		GetLoggerWithContext(r).
		WithFunctionName("LogOfferClickHandler")

	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	if !app.HasPermission(ctx, trackOfferClickPermission) {
		app.respondWithError(
			w,
			errors.New("forbidden: insufficient permissions"),
			http.StatusForbidden,
		)
		return
	}

	var input saveOfferClickInput
	if err := app.readJSON(w, r, &input); err != nil {
		app.respondWithError(
			w,
			fmt.Errorf("invalid JSON input: %w", err),
			http.StatusBadRequest,
		)
		return
	}

	offerID, err := parseRequiredOfferClickUUID(
		input.OfferID,
		"offer_id",
	)
	if err != nil {
		app.respondWithError(w, err, http.StatusBadRequest)
		return
	}

	userID := app.getUserIDFromContext(ctx)
	ipAddress := canonicalOfferClickRequestIPAddress(r)
	userAgent := optionalOfferClickString(r.UserAgent())
	referrer := optionalOfferClickString(r.Referer())

	if err := app.Models.OfferClick.LogOfferClick(
		ctx,
		offerID,
		userID,
		ipAddress,
		userAgent,
		referrer,
	); err != nil {
		logger.Error(
			"Log offer click failed",
			"offer_id", offerID,
			"error", err,
		)
		app.respondWithError(
			w,
			fmt.Errorf("failed to log offer click: %w", err),
			http.StatusInternalServerError,
		)
		return
	}

	logger.Info(
		"Offer click logged",
		"offer_id", offerID,
		"user_id", userID,
	)

	app.respondWithJSON(w, http.StatusCreated, jsonResponse{
		Error:   false,
		Message: "Offer click logged successfully",
		Data: map[string]uuid.UUID{
			"offer_id": offerID,
		},
	})
}

// GetOfferClickByIDHandler retrieves one immutable offer-click event by its
// canonical identifier.
//
// The identifier is supplied explicitly through JSON rather than through an
// obsolete deal-specific context helper.
func (app *Application) GetOfferClickByIDHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	logger := app.Logger.
		GetLoggerWithContext(r).
		WithFunctionName("GetOfferClickByIDHandler")

	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	if !app.HasPermission(ctx, trackOfferClickPermission) {
		app.respondWithError(
			w,
			errors.New("forbidden: insufficient permissions"),
			http.StatusForbidden,
		)
		return
	}

	var input getOfferClickByIDInput
	if err := app.readJSON(w, r, &input); err != nil {
		app.respondWithError(
			w,
			fmt.Errorf("invalid JSON input: %w", err),
			http.StatusBadRequest,
		)
		return
	}

	offerClickID, err := parseRequiredOfferClickUUID(
		input.ID,
		"id",
	)
	if err != nil {
		app.respondWithError(w, err, http.StatusBadRequest)
		return
	}

	offerClick, err := app.Models.OfferClick.GetByID(
		ctx,
		offerClickID,
	)
	if err != nil {
		if errors.Is(err, data.ErrOfferClickNotFound) {
			app.respondWithError(
				w,
				errors.New("offer click not found"),
				http.StatusNotFound,
			)
			return
		}

		logger.Error(
			"Retrieve offer click failed",
			"offer_click_id", offerClickID,
			"error", err,
		)
		app.respondWithError(
			w,
			fmt.Errorf("failed to retrieve offer click: %w", err),
			http.StatusInternalServerError,
		)
		return
	}

	if offerClick == nil {
		app.respondWithError(
			w,
			errors.New("offer click not found"),
			http.StatusNotFound,
		)
		return
	}

	logger.Info(
		"Offer click retrieved",
		"offer_click_id", offerClick.ID,
		"offer_id", offerClick.OfferID,
	)

	app.respondWithJSON(w, http.StatusOK, jsonResponse{
		Error:   false,
		Message: "Offer click retrieved successfully",
		Data:    offerClick,
	})
}

// GetOfferClickByOfferIDHandler retrieves a bounded page of immutable click
// events for one offer, ordered by clicked_at descending.
func (app *Application) GetOfferClickByOfferIDHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	logger := app.Logger.
		GetLoggerWithContext(r).
		WithFunctionName("GetOfferClickByOfferIDHandler")

	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	if !app.HasPermission(ctx, trackOfferClickPermission) {
		app.respondWithError(
			w,
			errors.New("forbidden: insufficient permissions"),
			http.StatusForbidden,
		)
		return
	}

	var input getOfferClicksByOfferIDInput
	if err := app.readJSON(w, r, &input); err != nil {
		app.respondWithError(
			w,
			fmt.Errorf("invalid JSON input: %w", err),
			http.StatusBadRequest,
		)
		return
	}

	offerID, err := parseRequiredOfferClickUUID(
		input.OfferID,
		"offer_id",
	)
	if err != nil {
		app.respondWithError(w, err, http.StatusBadRequest)
		return
	}

	limit := input.Limit
	if limit == 0 {
		limit = defaultOfferClickLimit
	}

	if limit < 1 || limit > maxOfferClickLimit {
		app.respondWithError(
			w,
			fmt.Errorf(
				"limit must be between 1 and %d",
				maxOfferClickLimit,
			),
			http.StatusBadRequest,
		)
		return
	}

	if input.Offset < 0 {
		app.respondWithError(
			w,
			errors.New("offset must be zero or greater"),
			http.StatusBadRequest,
		)
		return
	}

	offerClicks, err := app.Models.OfferClick.GetByOfferID(
		ctx,
		offerID,
		limit,
		input.Offset,
	)
	if err != nil {
		logger.Error(
			"Retrieve offer clicks by offer ID failed",
			"offer_id", offerID,
			"limit", limit,
			"offset", input.Offset,
			"error", err,
		)
		app.respondWithError(
			w,
			fmt.Errorf("failed to retrieve offer clicks: %w", err),
			http.StatusInternalServerError,
		)
		return
	}

	if offerClicks == nil {
		offerClicks = make([]*data.OfferClick, 0)
	}

	logger.Info(
		"Offer clicks retrieved",
		"offer_id", offerID,
		"count", len(offerClicks),
		"limit", limit,
		"offset", input.Offset,
	)

	app.respondWithJSON(w, http.StatusOK, jsonResponse{
		Error:   false,
		Message: "Offer clicks retrieved successfully",
		Data: map[string]any{
			"offer_id": offerID,
			"clicks":   offerClicks,
			"limit":    limit,
			"offset":   input.Offset,
		},
	})
}

// parseRequiredOfferClickUUID parses and validates a required UUID transport
// value for the deferred offer-click handler surface.
func parseRequiredOfferClickUUID(
	rawValue string,
	fieldName string,
) (uuid.UUID, error) {
	value := strings.TrimSpace(rawValue)
	if value == "" {
		return uuid.Nil, fmt.Errorf("%s is required", fieldName)
	}

	id, err := uuid.Parse(value)
	if err != nil || id == uuid.Nil {
		return uuid.Nil, fmt.Errorf(
			"%s must be a valid UUID",
			fieldName,
		)
	}

	return id, nil
}

// canonicalOfferClickRequestIPAddress returns the request's canonical textual
// client IP address.
//
// RealIP middleware may leave RemoteAddr as either a bare IP address or an
// IP-and-port pair. Empty or invalid values are omitted rather than persisted.
func canonicalOfferClickRequestIPAddress(
	r *http.Request,
) *string {
	if r == nil {
		return nil
	}

	remoteAddress := strings.TrimSpace(r.RemoteAddr)
	if remoteAddress == "" {
		return nil
	}

	host := remoteAddress
	if parsedHost, _, err := net.SplitHostPort(remoteAddress); err == nil {
		host = parsedHost
	}

	host = strings.Trim(
		strings.TrimSpace(host),
		"[]",
	)

	ipAddress := net.ParseIP(host)
	if ipAddress == nil {
		return nil
	}

	canonical := ipAddress.String()
	return &canonical
}

// optionalOfferClickString converts blank request metadata to nil and returns
// nonblank metadata in trimmed form.
func optionalOfferClickString(
	value string,
) *string {
	canonical := strings.TrimSpace(value)
	if canonical == "" {
		return nil
	}

	return &canonical
}