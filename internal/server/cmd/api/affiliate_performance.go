// Package main provides HTTP handlers for the SagrentiDeals API.
//
// sdworkspace/sdbackend/internal/server/cmd/api/affiliate_performance.go
//
// GTM:
//
//	Layer: 2.4 Merchant / Affiliate Domain
//	Release Class: DEFERRED
//	Reason:
//	  Affiliate performance HTTP administration is valid post-release
//	  analytics and reporting infrastructure, but it is not required for the
//	  initial SagrentiDeals release spine. The v1 spine only requires offer
//	  publication, affiliate click tracking, and safe public offer behavior.
//	  Do not expand these handlers until the release spine is functionally
//	  complete.
//
// DEFERRED Rule:
//
//	Keep compiling.
//	Keep safe.
//	Keep production-ready.
//	Preserve authorization enforcement.
//	Preserve trusted-context actor extraction.
//	Preserve fixed-point decimal string handling.
//	Preserve DB-owned lifecycle timestamps.
//	Preserve offer-based upsert behavior.
//	Preserve safe filtered pagination.
//	Preserve best-effort audit logging.
//	Do not add new features.
//	Do not route into v1 UI/API expansion.
//	Do not block deployment on this file unless it breaks the build.
package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/PiccoloMondoC/sdworkspace/sdbackend/internal/data"

	"github.com/google/uuid"
)

const (
	affiliatePerformanceEntityTypeName        = "affiliate_performance"
	affiliatePerformanceEntityTypeDescription = "Affiliate performance entity"

	createAffiliatePerformanceAction = "create_affiliate_performance"
	readAffiliatePerformanceAction   = "read_affiliate_performance"
	listAffiliatePerformanceAction   = "list_affiliate_performance"
	updateAffiliatePerformanceAction = "update_affiliate_performance"
	deleteAffiliatePerformanceAction = "delete_affiliate_performance"

	defaultAffiliatePerformanceLimit = 20
	maxAffiliatePerformanceLimit     = 100
)

type saveAffiliatePerformanceInput struct {
	OfferID           string `json:"offer_id"`
	TotalClicks       int    `json:"total_clicks"`
	EstimatedRevenue string `json:"estimated_revenue"`
	ConversionRate   string `json:"conversion_rate"`
	AvgOrderValue    string `json:"avg_order_value"`
}

type updateAffiliatePerformanceMetricsInput struct {
	TotalClicks       int    `json:"total_clicks"`
	EstimatedRevenue string `json:"estimated_revenue"`
	ConversionRate   string `json:"conversion_rate"`
	AvgOrderValue    string `json:"avg_order_value"`
}

// SaveAffiliatePerformanceHandler creates or replaces the aggregate affiliate
// performance metrics for an offer.
//
// The data layer owns offer-based upsert behavior. Decimal business values remain
// strings throughout the HTTP and persistence paths to preserve fixed-point
// precision.
func (app *Application) SaveAffiliatePerformanceHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	logger := app.Logger.
		GetLoggerWithContext(r).
		WithFunctionName("SaveAffiliatePerformanceHandler")

	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	if !app.HasPermission(ctx, createAffiliatePerformanceAction) {
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

	var input saveAffiliatePerformanceInput
	if err := app.readJSON(w, r, &input); err != nil {
		app.respondWithError(
			w,
			fmt.Errorf("invalid JSON input: %w", err),
			http.StatusBadRequest,
		)
		return
	}

	offerID, err := uuid.Parse(input.OfferID)
	if err != nil {
		app.respondWithError(
			w,
			fmt.Errorf("invalid offer_id: %w", err),
			http.StatusBadRequest,
		)
		return
	}

	performance := &data.AffiliatePerformance{
		OfferID:           offerID,
		TotalClicks:       input.TotalClicks,
		EstimatedRevenue: input.EstimatedRevenue,
		ConversionRate:   input.ConversionRate,
		AvgOrderValue:    input.AvgOrderValue,
	}

	if err := app.Models.AffiliatePerformance.Insert(ctx, performance); err != nil {
		logger.Error(
			"Save affiliate performance failed",
			"offer_id", offerID,
			"error", err,
		)
		app.respondWithError(
			w,
			fmt.Errorf("failed to save affiliate performance: %w", err),
			http.StatusInternalServerError,
		)
		return
	}

	// Insert is an offer-based upsert. Reload the canonical persisted row so an
	// existing row's primary key is returned when the operation resolves an
	// offer_id conflict.
	persisted, err := app.Models.AffiliatePerformance.GetByOfferID(ctx, offerID)
	if err != nil {
		logger.Error(
			"Reload saved affiliate performance failed",
			"offer_id", offerID,
			"error", err,
		)
		app.respondWithError(
			w,
			fmt.Errorf(
				"affiliate performance was saved but could not be reloaded: %w",
				err,
			),
			http.StatusInternalServerError,
		)
		return
	}

	if persisted == nil {
		logger.Error(
			"Saved affiliate performance could not be found",
			"offer_id", offerID,
		)
		app.respondWithError(
			w,
			errors.New("affiliate performance was saved but could not be found"),
			http.StatusInternalServerError,
		)
		return
	}

	app.insertAffiliatePerformanceAudit(
		ctx,
		userID,
		createAffiliatePerformanceAction,
		"Create or replace affiliate performance metrics",
		persisted.ID.String(),
	)

	logger.Info(
		"Affiliate performance saved",
		"performance_id", persisted.ID,
		"offer_id", persisted.OfferID,
		"user_id", userID,
	)

	app.respondWithJSON(w, http.StatusCreated, jsonResponse{
		Error:   false,
		Message: "Affiliate performance saved successfully",
		Data:    persisted,
	})
}

// GetAffiliatePerformanceByIDHandler retrieves one affiliate performance record
// using the trusted record ID installed in request context by route middleware.
func (app *Application) GetAffiliatePerformanceByIDHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	logger := app.Logger.
		GetLoggerWithContext(r).
		WithFunctionName("GetAffiliatePerformanceByIDHandler")

	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	if !app.HasPermission(ctx, readAffiliatePerformanceAction) {
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

	performanceID := app.getAffiliatePerformanceIDFromContext(ctx)
	if performanceID == nil || *performanceID == uuid.Nil {
		app.respondWithError(
			w,
			errors.New("affiliate performance ID not found in context"),
			http.StatusBadRequest,
		)
		return
	}

	performance, err := app.Models.AffiliatePerformance.GetByID(
		ctx,
		*performanceID,
	)
	if err != nil {
		logger.Error(
			"Retrieve affiliate performance failed",
			"performance_id", *performanceID,
			"error", err,
		)
		app.respondWithError(
			w,
			fmt.Errorf("failed to retrieve affiliate performance: %w", err),
			http.StatusInternalServerError,
		)
		return
	}

	if performance == nil {
		app.respondWithError(
			w,
			errors.New("affiliate performance record not found"),
			http.StatusNotFound,
		)
		return
	}

	app.insertAffiliatePerformanceAudit(
		ctx,
		userID,
		readAffiliatePerformanceAction,
		"Read affiliate performance by ID",
		performance.ID.String(),
	)

	logger.Info(
		"Affiliate performance retrieved",
		"performance_id", performance.ID,
		"user_id", userID,
	)

	app.respondWithJSON(w, http.StatusOK, jsonResponse{
		Error:   false,
		Message: "Affiliate performance retrieved successfully",
		Data:    performance,
	})
}

// ListAffiliatePerformanceHandler retrieves a bounded, filtered list of
// affiliate performance records.
func (app *Application) ListAffiliatePerformanceHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	logger := app.Logger.
		GetLoggerWithContext(r).
		WithFunctionName("ListAffiliatePerformanceHandler")

	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	if !app.HasPermission(ctx, listAffiliatePerformanceAction) {
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

	query := r.URL.Query()

	limit := parseIntOrDefault(
		query.Get("limit"),
		defaultAffiliatePerformanceLimit,
	)
	offset := parseIntOrDefault(query.Get("offset"), 0)

	if limit < 1 {
		app.respondWithError(
			w,
			errors.New("limit must be greater than 0"),
			http.StatusBadRequest,
		)
		return
	}

	if limit > maxAffiliatePerformanceLimit {
		limit = maxAffiliatePerformanceLimit
	}

	if offset < 0 {
		app.respondWithError(
			w,
			errors.New("offset cannot be negative"),
			http.StatusBadRequest,
		)
		return
	}

	offerID, err := parseOptionalAffiliatePerformanceUUID(
		query.Get("offer_id"),
		"offer_id",
	)
	if err != nil {
		app.respondWithError(w, err, http.StatusBadRequest)
		return
	}

	merchantID, err := parseOptionalAffiliatePerformanceUUID(
		query.Get("merchant_id"),
		"merchant_id",
	)
	if err != nil {
		app.respondWithError(w, err, http.StatusBadRequest)
		return
	}

	startDate, err := parseOptionalAffiliatePerformanceTime(
		query.Get("start_date"),
		"start_date",
	)
	if err != nil {
		app.respondWithError(w, err, http.StatusBadRequest)
		return
	}

	endDate, err := parseOptionalAffiliatePerformanceTime(
		query.Get("end_date"),
		"end_date",
	)
	if err != nil {
		app.respondWithError(w, err, http.StatusBadRequest)
		return
	}

	if startDate != nil && endDate != nil && startDate.After(*endDate) {
		app.respondWithError(
			w,
			errors.New("start_date cannot be after end_date"),
			http.StatusBadRequest,
		)
		return
	}

	performances, err := app.Models.AffiliatePerformance.FilteredList(
		ctx,
		limit,
		offset,
		offerID,
		merchantID,
		startDate,
		endDate,
	)
	if err != nil {
		logger.Error(
			"List affiliate performance failed",
			"limit", limit,
			"offset", offset,
			"error", err,
		)
		app.respondWithError(
			w,
			fmt.Errorf(
				"failed to list affiliate performance records: %w",
				err,
			),
			http.StatusInternalServerError,
		)
		return
	}

	app.insertAffiliatePerformanceAudit(
		ctx,
		userID,
		listAffiliatePerformanceAction,
		"List affiliate performance records",
		"",
	)

	logger.Info(
		"Affiliate performance list retrieved",
		"count", len(performances),
		"limit", limit,
		"offset", offset,
		"user_id", userID,
	)

	app.respondWithJSON(w, http.StatusOK, jsonResponse{
		Error:   false,
		Message: "Affiliate performance records retrieved successfully",
		Data:    performances,
	})
}

// UpdateAffiliatePerformanceMetricsHandler replaces the aggregate metrics on an
// existing affiliate performance record.
//
// This endpoint does not insert missing records. Creation and offer-based upsert
// belong exclusively to SaveAffiliatePerformanceHandler and the data Insert
// contract.
func (app *Application) UpdateAffiliatePerformanceMetricsHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	logger := app.Logger.
		GetLoggerWithContext(r).
		WithFunctionName("UpdateAffiliatePerformanceMetricsHandler")

	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	if !app.HasPermission(ctx, updateAffiliatePerformanceAction) {
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

	performanceID := app.getAffiliatePerformanceIDFromContext(ctx)
	if performanceID == nil || *performanceID == uuid.Nil {
		app.respondWithError(
			w,
			errors.New("affiliate performance ID not found in context"),
			http.StatusBadRequest,
		)
		return
	}

	var input updateAffiliatePerformanceMetricsInput
	if err := app.readJSON(w, r, &input); err != nil {
		app.respondWithError(
			w,
			fmt.Errorf("invalid JSON input: %w", err),
			http.StatusBadRequest,
		)
		return
	}

	err := app.Models.AffiliatePerformance.UpdateMetrics(
		ctx,
		*performanceID,
		input.TotalClicks,
		input.EstimatedRevenue,
		input.ConversionRate,
		input.AvgOrderValue,
	)
	if err != nil {
		if errors.Is(err, data.ErrAffiliatePerformanceNotFound) {
			app.respondWithError(
				w,
				errors.New("affiliate performance record not found"),
				http.StatusNotFound,
			)
			return
		}

		logger.Error(
			"Update affiliate performance metrics failed",
			"performance_id", *performanceID,
			"error", err,
		)
		app.respondWithError(
			w,
			fmt.Errorf(
				"failed to update affiliate performance metrics: %w",
				err,
			),
			http.StatusInternalServerError,
		)
		return
	}

	performance, err := app.Models.AffiliatePerformance.GetByID(
		ctx,
		*performanceID,
	)
	if err != nil {
		logger.Error(
			"Reload updated affiliate performance failed",
			"performance_id", *performanceID,
			"error", err,
		)
		app.respondWithError(
			w,
			fmt.Errorf(
				"affiliate performance was updated but could not be reloaded: %w",
				err,
			),
			http.StatusInternalServerError,
		)
		return
	}

	if performance == nil {
		app.respondWithError(
			w,
			errors.New(
				"affiliate performance was updated but could not be found",
			),
			http.StatusInternalServerError,
		)
		return
	}

	app.insertAffiliatePerformanceAudit(
		ctx,
		userID,
		updateAffiliatePerformanceAction,
		"Update affiliate performance metrics",
		performance.ID.String(),
	)

	logger.Info(
		"Affiliate performance metrics updated",
		"performance_id", performance.ID,
		"user_id", userID,
	)

	app.respondWithJSON(w, http.StatusOK, jsonResponse{
		Error:   false,
		Message: "Affiliate performance metrics updated successfully",
		Data:    performance,
	})
}

// DeleteAffiliatePerformanceHandler permanently deletes one derived affiliate
// performance aggregate by its trusted context ID.
func (app *Application) DeleteAffiliatePerformanceHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	logger := app.Logger.
		GetLoggerWithContext(r).
		WithFunctionName("DeleteAffiliatePerformanceHandler")

	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	if !app.HasPermission(ctx, deleteAffiliatePerformanceAction) {
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

	performanceID := app.getAffiliatePerformanceIDFromContext(ctx)
	if performanceID == nil || *performanceID == uuid.Nil {
		app.respondWithError(
			w,
			errors.New("affiliate performance ID not found in context"),
			http.StatusBadRequest,
		)
		return
	}

	err := app.Models.AffiliatePerformance.Delete(ctx, *performanceID)
	if err != nil {
		if errors.Is(err, data.ErrAffiliatePerformanceNotFound) {
			app.respondWithError(
				w,
				errors.New("affiliate performance record not found"),
				http.StatusNotFound,
			)
			return
		}

		logger.Error(
			"Delete affiliate performance failed",
			"performance_id", *performanceID,
			"error", err,
		)
		app.respondWithError(
			w,
			fmt.Errorf("failed to delete affiliate performance: %w", err),
			http.StatusInternalServerError,
		)
		return
	}

	app.insertAffiliatePerformanceAudit(
		ctx,
		userID,
		deleteAffiliatePerformanceAction,
		"Delete affiliate performance record",
		performanceID.String(),
	)

	logger.Info(
		"Affiliate performance deleted",
		"performance_id", *performanceID,
		"user_id", userID,
	)

	app.respondWithJSON(w, http.StatusOK, jsonResponse{
		Error:   false,
		Message: "Affiliate performance deleted successfully",
		Data:    performanceID,
	})
}

// insertAffiliatePerformanceAudit performs best-effort audit insertion.
//
// Affiliate performance is DEFERRED analytics infrastructure. A completed
// domain operation is not rolled back or converted into an HTTP partial-content
// response merely because audit metadata or audit persistence is unavailable.
func (app *Application) insertAffiliatePerformanceAudit(
	ctx context.Context,
	userID *uuid.UUID,
	actionName string,
	actionDescription string,
	entityID string,
) {
	if userID == nil || *userID == uuid.Nil {
		return
	}

	action, err := app.Models.Action.GetByName(ctx, actionName)
	if err != nil || action == nil {
		actionID, createErr := app.Models.Action.CreateIfNotExists(
			ctx,
			actionName,
			actionDescription,
		)
		if createErr != nil {
			return
		}

		action = &data.Action{
			ID: actionID,
		}
	}

	entityType, err := app.Models.EntityType.GetByName(
		ctx,
		affiliatePerformanceEntityTypeName,
	)
	if err != nil || entityType == nil {
		entityTypeID, createErr := app.Models.EntityType.CreateIfNotExists(
			ctx,
			affiliatePerformanceEntityTypeName,
			affiliatePerformanceEntityTypeDescription,
		)
		if createErr != nil {
			return
		}

		entityType = &data.EntityType{
			ID: entityTypeID,
		}
	}

	if action == nil || action.ID == uuid.Nil {
		return
	}

	if entityType == nil || entityType.ID == uuid.Nil {
		return
	}

	auditLog := &data.AuditLog{
		ID:           uuid.New(),
		UserID:       userID,
		ActionID:     action.ID,
		EntityTypeID: entityType.ID,
		EntityID:     entityID,
	}

	// Audit insertion is intentionally best-effort for this DEFERRED domain.
	_ = app.Models.AuditLog.Insert(ctx, auditLog)
}

func parseOptionalAffiliatePerformanceUUID(
	rawValue string,
	fieldName string,
) (*uuid.UUID, error) {
	if rawValue == "" {
		return nil, nil
	}

	id, err := uuid.Parse(rawValue)
	if err != nil {
		return nil, fmt.Errorf("invalid %s: %w", fieldName, err)
	}

	return &id, nil
}

func parseOptionalAffiliatePerformanceTime(
	rawValue string,
	fieldName string,
) (*time.Time, error) {
	if rawValue == "" {
		return nil, nil
	}

	value, err := time.Parse(time.RFC3339, rawValue)
	if err != nil {
		return nil, fmt.Errorf(
			"invalid %s: must use RFC3339 format: %w",
			fieldName,
			err,
		)
	}

	return &value, nil
}