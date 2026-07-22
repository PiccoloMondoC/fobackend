// Package main provides HTTP handlers for the Platform API.
//
// sdworkspace/sdbackend/internal/server/cmd/api/coupons.go
//
// GTM:
//
//	Layer: 2.5 Catalog / Offer Domain
//	Release Class: DEFERRED
//	Reason:
//	  Coupon administration, moderation, clipping, and performance reporting
//	  are valid future commerce capabilities, but they are not required for
//	  the initial Platform release spine. The v1 spine is offer-first and
//	  relies on canonical offers, affiliate links, publication status, click
//	  tracking, and price history before expanding into a dedicated coupon
//	  ecosystem.
//
// DEFERRED Rule:
//
//	Keep compiling.
//	Keep safe.
//	Keep production-ready.
//	Preserve offer-first coupon ownership.
//	Preserve coupon status lookup semantics.
//	Preserve exact decimal string handling.
//	Preserve DB-owned lifecycle timestamps.
//	Preserve soft-delete semantics for ordinary removal.
//	Preserve trusted-context identity extraction.
//	Preserve authorization enforcement.
//	Preserve typed data-layer error handling.
//	Preserve best-effort audit logging.
//	Do not add new features.
//	Do not route into v1 UI/API expansion.
//	Do not block deployment on this file unless it breaks the build or safety.
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

	"github.com/google/uuid"
)

const (
	couponEntityTypeName        = "coupon"
	couponEntityTypeDescription = "Coupon entity representing offer-based discounts"

	createCouponAction             = "create_coupon"
	readCouponAction               = "read_coupon"
	readActiveCouponsAction        = "read_active_coupons"
	readPopularCouponsAction       = "read_popular_coupons"
	readCouponsByCategoryAction    = "read_coupons_by_category"
	readFlaggedCouponsAction       = "read_flagged_coupons"
	analyzeCouponPerformanceAction = "analyze_coupon_performance"
	clipCouponAction               = "clip_coupon"
	readClippedCouponsAction       = "read_clipped_coupons"
	updateCouponAction             = "update_coupon"
	softDeleteCouponAction         = "soft_delete_coupon"
	approveCouponAction            = "approve_coupon"
	rejectCouponAction             = "reject_coupon"
	flagCouponAction               = "flag_coupon"

	defaultCouponLimit = 20
	maxCouponLimit     = 100
)

type createCouponInput struct {
	OfferID            *uuid.UUID `json:"offer_id"`
	CouponStatusID     *uuid.UUID `json:"coupon_status_id,omitempty"`
	Code               string     `json:"code"`
	DiscountType       string     `json:"discount_type"`
	DiscountValue      string     `json:"discount_value"`
	MinPurchaseAmount string     `json:"min_purchase_amount"`
	StartDate         time.Time  `json:"start_date"`
	EndDate           *time.Time `json:"end_date,omitempty"`
	AffiliateURL      *string    `json:"affiliate_url,omitempty"`
}

type updateCouponInput struct {
	OfferID            *uuid.UUID `json:"offer_id"`
	CouponStatusID     *uuid.UUID `json:"coupon_status_id,omitempty"`
	Code               string     `json:"code"`
	DiscountType       string     `json:"discount_type"`
	DiscountValue      string     `json:"discount_value"`
	MinPurchaseAmount string     `json:"min_purchase_amount"`
	StartDate         time.Time  `json:"start_date"`
	EndDate           *time.Time `json:"end_date,omitempty"`
	AffiliateURL      *string    `json:"affiliate_url,omitempty"`
	RejectionReason   *string    `json:"rejection_reason,omitempty"`
}

type couponReasonInput struct {
	Reason string `json:"reason"`
}

// CreateCouponHandler creates a new offer-owned coupon.
func (app *Application) CreateCouponHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	logger := app.Logger.
		GetLoggerWithContext(r).
		WithFunctionName("CreateCouponHandler")

	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	if !app.HasPermission(ctx, createCouponAction) {
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

	var input createCouponInput
	if err := app.readJSON(w, r, &input); err != nil {
		app.respondWithError(
			w,
			fmt.Errorf("invalid JSON input: %w", err),
			http.StatusBadRequest,
		)
		return
	}

	coupon := &data.Coupon{
		ID:                uuid.New(),
		OfferID:           input.OfferID,
		CouponStatusID:    input.CouponStatusID,
		Code:              input.Code,
		DiscountType:      input.DiscountType,
		DiscountValue:     input.DiscountValue,
		MinPurchaseAmount: input.MinPurchaseAmount,
		StartDate:         input.StartDate,
		EndDate:           input.EndDate,
		AffiliateURL:      input.AffiliateURL,
	}

	if err := app.Models.Coupon.Insert(ctx, coupon); err != nil {
		logger.Error(
			"Create coupon failed",
			"offer_id", input.OfferID,
			"code", input.Code,
			"error", err,
		)
		app.respondWithCouponError(
			w,
			err,
			"failed to create coupon",
		)
		return
	}

	app.insertCouponAudit(
		ctx,
		userID,
		createCouponAction,
		"Create an offer-owned coupon",
		coupon.ID.String(),
	)

	logger.Info(
		"Coupon created",
		"coupon_id", coupon.ID,
		"offer_id", coupon.OfferID,
		"user_id", userID,
	)

	app.respondWithJSON(w, http.StatusCreated, jsonResponse{
		Error:   false,
		Message: "Coupon created successfully",
		Data:    coupon,
	})
}

// GetCouponByIDHandler retrieves one coupon using the trusted coupon ID
// installed in request context by route middleware.
func (app *Application) GetCouponByIDHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	logger := app.Logger.
		GetLoggerWithContext(r).
		WithFunctionName("GetCouponByIDHandler")

	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	couponID := app.getContextValueAsUUID(ctx, ctxEntityID)
	if couponID == nil || *couponID == uuid.Nil {
		app.respondWithError(
			w,
			errors.New("coupon ID not found in context"),
			http.StatusBadRequest,
		)
		return
	}

	coupon, err := app.Models.Coupon.GetByID(ctx, *couponID)
	if err != nil {
		logger.Error(
			"Retrieve coupon failed",
			"coupon_id", *couponID,
			"error", err,
		)
		app.respondWithCouponError(
			w,
			err,
			"failed to retrieve coupon",
		)
		return
	}

	// Canonical coupons are offer-owned and may be exposed through the public
	// offer surface. A legacy row without an offer remains permission-gated.
	var userID *uuid.UUID
	if coupon.OfferID == nil || *coupon.OfferID == uuid.Nil {
		if !app.HasPermission(ctx, readCouponAction) {
			app.respondWithError(
				w,
				errors.New("forbidden: insufficient permissions"),
				http.StatusForbidden,
			)
			return
		}

		userID = app.getUserIDFromContext(ctx)
		if userID == nil {
			app.respondWithError(
				w,
				errors.New("user ID not found in context"),
				http.StatusUnauthorized,
			)
			return
		}
	} else {
		userID = app.getUserIDFromContext(ctx)
	}

	app.insertCouponAudit(
		ctx,
		userID,
		readCouponAction,
		"Read a coupon by ID",
		coupon.ID.String(),
	)

	logger.Info(
		"Coupon retrieved",
		"coupon_id", coupon.ID,
		"user_id", userID,
	)

	app.respondWithJSON(w, http.StatusOK, jsonResponse{
		Error:   false,
		Message: "Coupon retrieved successfully",
		Data:    coupon,
	})
}

// GetCouponByOfferIDHandler retrieves all coupons belonging to the trusted
// offer ID installed in request context.
func (app *Application) GetCouponByOfferIDHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	logger := app.Logger.
		GetLoggerWithContext(r).
		WithFunctionName("GetCouponByOfferIDHandler")

	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	offerID := app.getOfferIDFromContext(ctx)
	if offerID == nil || *offerID == uuid.Nil {
		app.respondWithError(
			w,
			errors.New("offer ID not found in context"),
			http.StatusBadRequest,
		)
		return
	}

	coupons, err := app.Models.Coupon.GetByOfferID(ctx, *offerID)
	if err != nil {
		logger.Error(
			"Retrieve coupons by offer failed",
			"offer_id", *offerID,
			"error", err,
		)
		app.respondWithCouponError(
			w,
			err,
			"failed to retrieve coupons",
		)
		return
	}

	app.insertCouponAudit(
		ctx,
		app.getUserIDFromContext(ctx),
		readCouponAction,
		"Read coupons belonging to an offer",
		offerID.String(),
	)

	logger.Info(
		"Coupons retrieved by offer",
		"offer_id", *offerID,
		"count", len(coupons),
	)

	app.respondWithJSON(w, http.StatusOK, jsonResponse{
		Error:   false,
		Message: "Coupons retrieved successfully",
		Data:    coupons,
	})
}

// UpdateCouponHandler replaces the mutable business fields of an existing
// coupon. The data layer owns normalization, validation, and DB timestamps.
func (app *Application) UpdateCouponHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	logger := app.Logger.
		GetLoggerWithContext(r).
		WithFunctionName("UpdateCouponHandler")

	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	if !app.HasPermission(ctx, updateCouponAction) {
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

	couponID := app.getContextValueAsUUID(ctx, ctxEntityID)
	if couponID == nil || *couponID == uuid.Nil {
		app.respondWithError(
			w,
			errors.New("coupon ID not found in context"),
			http.StatusBadRequest,
		)
		return
	}

	var input updateCouponInput
	if err := app.readJSON(w, r, &input); err != nil {
		app.respondWithError(
			w,
			fmt.Errorf("invalid JSON input: %w", err),
			http.StatusBadRequest,
		)
		return
	}

	coupon := &data.Coupon{
		ID:                *couponID,
		OfferID:           input.OfferID,
		CouponStatusID:    input.CouponStatusID,
		Code:              input.Code,
		DiscountType:      input.DiscountType,
		DiscountValue:     input.DiscountValue,
		MinPurchaseAmount: input.MinPurchaseAmount,
		StartDate:         input.StartDate,
		EndDate:           input.EndDate,
		AffiliateURL:      input.AffiliateURL,
		RejectionReason:   input.RejectionReason,
	}

	if err := app.Models.Coupon.Update(ctx, coupon); err != nil {
		logger.Error(
			"Update coupon failed",
			"coupon_id", *couponID,
			"error", err,
		)
		app.respondWithCouponError(
			w,
			err,
			"failed to update coupon",
		)
		return
	}

	app.insertCouponAudit(
		ctx,
		userID,
		updateCouponAction,
		"Update an offer-owned coupon",
		coupon.ID.String(),
	)

	logger.Info(
		"Coupon updated",
		"coupon_id", coupon.ID,
		"user_id", userID,
	)

	app.respondWithJSON(w, http.StatusOK, jsonResponse{
		Error:   false,
		Message: "Coupon updated successfully",
		Data:    coupon,
	})
}

// DeleteCouponHandler performs the ordinary soft-delete path.
//
// CouponModel.Delete is the destructive purge path and is intentionally not
// used by this handler.
func (app *Application) DeleteCouponHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	logger := app.Logger.
		GetLoggerWithContext(r).
		WithFunctionName("DeleteCouponHandler")

	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	if !app.HasPermission(ctx, softDeleteCouponAction) {
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

	couponID := app.getContextValueAsUUID(ctx, ctxEntityID)
	if couponID == nil || *couponID == uuid.Nil {
		app.respondWithError(
			w,
			errors.New("coupon ID not found in context"),
			http.StatusBadRequest,
		)
		return
	}

	if err := app.Models.Coupon.SoftDelete(ctx, *couponID); err != nil {
		logger.Error(
			"Soft-delete coupon failed",
			"coupon_id", *couponID,
			"error", err,
		)
		app.respondWithCouponError(
			w,
			err,
			"failed to delete coupon",
		)
		return
	}

	app.insertCouponAudit(
		ctx,
		userID,
		softDeleteCouponAction,
		"Soft-delete a coupon",
		couponID.String(),
	)

	logger.Info(
		"Coupon soft-deleted",
		"coupon_id", *couponID,
		"user_id", userID,
	)

	app.respondWithJSON(w, http.StatusOK, jsonResponse{
		Error:   false,
		Message: "Coupon deleted successfully",
		Data:    couponID,
	})
}

// ApproveCouponHandler transitions a coupon to the active status.
func (app *Application) ApproveCouponHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	logger := app.Logger.
		GetLoggerWithContext(r).
		WithFunctionName("ApproveCouponHandler")

	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	if !app.HasPermission(ctx, updateCouponAction) {
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

	couponID := app.getContextValueAsUUID(ctx, ctxEntityID)
	if couponID == nil || *couponID == uuid.Nil {
		app.respondWithError(
			w,
			errors.New("coupon ID not found in context"),
			http.StatusBadRequest,
		)
		return
	}

	if err := app.Models.Coupon.ApproveCoupon(ctx, *couponID); err != nil {
		logger.Error(
			"Approve coupon failed",
			"coupon_id", *couponID,
			"error", err,
		)
		app.respondWithCouponError(
			w,
			err,
			"failed to approve coupon",
		)
		return
	}

	app.insertCouponAudit(
		ctx,
		userID,
		approveCouponAction,
		"Approve a coupon",
		couponID.String(),
	)

	logger.Info(
		"Coupon approved",
		"coupon_id", *couponID,
		"user_id", userID,
	)

	app.respondWithJSON(w, http.StatusOK, jsonResponse{
		Error:   false,
		Message: "Coupon approved successfully",
		Data:    couponID,
	})
}

// RejectCouponHandler transitions a coupon to rejected status with a required
// rejection reason.
func (app *Application) RejectCouponHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	logger := app.Logger.
		GetLoggerWithContext(r).
		WithFunctionName("RejectCouponHandler")

	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	if !app.HasPermission(ctx, updateCouponAction) {
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

	couponID := app.getContextValueAsUUID(ctx, ctxEntityID)
	if couponID == nil || *couponID == uuid.Nil {
		app.respondWithError(
			w,
			errors.New("coupon ID not found in context"),
			http.StatusBadRequest,
		)
		return
	}

	var input couponReasonInput
	if err := app.readJSON(w, r, &input); err != nil {
		app.respondWithError(
			w,
			fmt.Errorf("invalid JSON input: %w", err),
			http.StatusBadRequest,
		)
		return
	}

	input.Reason = strings.TrimSpace(input.Reason)
	if input.Reason == "" {
		app.respondWithError(
			w,
			errors.New("reason is required"),
			http.StatusBadRequest,
		)
		return
	}

	if err := app.Models.Coupon.RejectCoupon(
		ctx,
		*couponID,
		input.Reason,
	); err != nil {
		logger.Error(
			"Reject coupon failed",
			"coupon_id", *couponID,
			"error", err,
		)
		app.respondWithCouponError(
			w,
			err,
			"failed to reject coupon",
		)
		return
	}

	app.insertCouponAudit(
		ctx,
		userID,
		rejectCouponAction,
		"Reject a coupon",
		couponID.String(),
	)

	logger.Info(
		"Coupon rejected",
		"coupon_id", *couponID,
		"user_id", userID,
	)

	app.respondWithJSON(w, http.StatusOK, jsonResponse{
		Error:   false,
		Message: "Coupon rejected successfully",
		Data:    couponID,
	})
}

// FlagCouponHandler creates an unresolved moderation flag for a coupon.
func (app *Application) FlagCouponHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	logger := app.Logger.
		GetLoggerWithContext(r).
		WithFunctionName("FlagCouponHandler")

	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	userID := app.getUserIDFromContext(ctx)
	if userID == nil {
		app.respondWithError(
			w,
			errors.New("unauthorized: login required"),
			http.StatusUnauthorized,
		)
		return
	}

	couponID := app.getContextValueAsUUID(ctx, ctxEntityID)
	if couponID == nil || *couponID == uuid.Nil {
		app.respondWithError(
			w,
			errors.New("coupon ID not found in context"),
			http.StatusBadRequest,
		)
		return
	}

	var input couponReasonInput
	if err := app.readJSON(w, r, &input); err != nil {
		app.respondWithError(
			w,
			fmt.Errorf("invalid JSON input: %w", err),
			http.StatusBadRequest,
		)
		return
	}

	input.Reason = strings.TrimSpace(input.Reason)
	if input.Reason == "" {
		app.respondWithError(
			w,
			errors.New("reason is required"),
			http.StatusBadRequest,
		)
		return
	}

	if err := app.Models.Coupon.FlagCoupon(
		ctx,
		*couponID,
		input.Reason,
		userID,
	); err != nil {
		logger.Error(
			"Flag coupon failed",
			"coupon_id", *couponID,
			"error", err,
		)
		app.respondWithCouponError(
			w,
			err,
			"failed to flag coupon",
		)
		return
	}

	app.insertCouponAudit(
		ctx,
		userID,
		flagCouponAction,
		"Flag a coupon for moderation review",
		couponID.String(),
	)

	logger.Info(
		"Coupon flagged",
		"coupon_id", *couponID,
		"user_id", userID,
	)

	app.respondWithJSON(w, http.StatusOK, jsonResponse{
		Error:   false,
		Message: "Coupon flagged successfully",
		Data:    couponID,
	})
}

// GetActiveCouponsHandler retrieves coupons active at the current database
// time.
func (app *Application) GetActiveCouponsHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	logger := app.Logger.
		GetLoggerWithContext(r).
		WithFunctionName("GetActiveCouponsHandler")

	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	if !app.HasPermission(ctx, readActiveCouponsAction) {
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

	coupons, err := app.Models.Coupon.GetActiveCoupons(ctx)
	if err != nil {
		logger.Error(
			"Retrieve active coupons failed",
			"error", err,
		)
		app.respondWithCouponError(
			w,
			err,
			"failed to retrieve active coupons",
		)
		return
	}

	app.insertCouponAudit(
		ctx,
		userID,
		readActiveCouponsAction,
		"Read currently active coupons",
		"bulk-active-coupons",
	)

	logger.Info(
		"Active coupons retrieved",
		"count", len(coupons),
		"user_id", userID,
	)

	app.respondWithJSON(w, http.StatusOK, jsonResponse{
		Error:   false,
		Message: "Active coupons retrieved successfully",
		Data:    coupons,
	})
}

// GetPopularCouponsHandler retrieves coupons ranked by coupon usage volume.
func (app *Application) GetPopularCouponsHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	logger := app.Logger.
		GetLoggerWithContext(r).
		WithFunctionName("GetPopularCouponsHandler")

	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	if !app.HasPermission(ctx, readPopularCouponsAction) {
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

	limit, err := parseCouponLimit(r.URL.Query().Get("limit"))
	if err != nil {
		app.respondWithError(w, err, http.StatusBadRequest)
		return
	}

	coupons, err := app.Models.Coupon.GetPopularCoupons(ctx, limit)
	if err != nil {
		logger.Error(
			"Retrieve popular coupons failed",
			"limit", limit,
			"error", err,
		)
		app.respondWithCouponError(
			w,
			err,
			"failed to retrieve popular coupons",
		)
		return
	}

	app.insertCouponAudit(
		ctx,
		userID,
		readPopularCouponsAction,
		"Read coupons ranked by usage volume",
		"bulk-popular-coupons",
	)

	logger.Info(
		"Popular coupons retrieved",
		"count", len(coupons),
		"limit", limit,
		"user_id", userID,
	)

	app.respondWithJSON(w, http.StatusOK, jsonResponse{
		Error:   false,
		Message: "Popular coupons retrieved successfully",
		Data:    coupons,
	})
}

// GetCouponsByCategoryHandler retrieves coupons through the category of their
// parent offer.
func (app *Application) GetCouponsByCategoryHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	logger := app.Logger.
		GetLoggerWithContext(r).
		WithFunctionName("GetCouponsByCategoryHandler")

	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	if !app.HasPermission(ctx, readCouponsByCategoryAction) {
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

	category := strings.TrimSpace(
		app.getContextValueAsString(ctx, ctxCategoryID),
	)
	if category == "" {
		app.respondWithError(
			w,
			errors.New("category name not found in context"),
			http.StatusBadRequest,
		)
		return
	}

	limitRaw := app.getContextValueAsString(ctx, ctxPaginationLimit)
	limit, err := parseCouponLimit(limitRaw)
	if err != nil {
		app.respondWithError(w, err, http.StatusBadRequest)
		return
	}

	coupons, err := app.Models.Coupon.GetCouponsByCategory(
		ctx,
		category,
		limit,
	)
	if err != nil {
		logger.Error(
			"Retrieve coupons by category failed",
			"category", category,
			"limit", limit,
			"error", err,
		)
		app.respondWithCouponError(
			w,
			err,
			"failed to retrieve coupons by category",
		)
		return
	}

	app.insertCouponAudit(
		ctx,
		userID,
		readCouponsByCategoryAction,
		"Read coupons through their offer category",
		category,
	)

	logger.Info(
		"Coupons retrieved by category",
		"category", category,
		"count", len(coupons),
		"user_id", userID,
	)

	app.respondWithJSON(w, http.StatusOK, jsonResponse{
		Error:   false,
		Message: "Coupons retrieved successfully",
		Data:    coupons,
	})
}

// GetFlaggedCouponsHandler retrieves coupons with unresolved moderation flags.
//
// The response uses data.CouponWithFlag so the moderation metadata returned by
// the data layer is not discarded.
func (app *Application) GetFlaggedCouponsHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	logger := app.Logger.
		GetLoggerWithContext(r).
		WithFunctionName("GetFlaggedCouponsHandler")

	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	if !app.HasPermission(ctx, readFlaggedCouponsAction) {
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

	limit, err := parseCouponLimit(query.Get("limit"))
	if err != nil {
		app.respondWithError(w, err, http.StatusBadRequest)
		return
	}

	offset, err := parseCouponOffset(query.Get("offset"))
	if err != nil {
		app.respondWithError(w, err, http.StatusBadRequest)
		return
	}

	coupons, err := app.Models.Coupon.GetFlaggedCoupons(
		ctx,
		limit,
		offset,
	)
	if err != nil {
		logger.Error(
			"Retrieve flagged coupons failed",
			"limit", limit,
			"offset", offset,
			"error", err,
		)
		app.respondWithCouponError(
			w,
			err,
			"failed to retrieve flagged coupons",
		)
		return
	}

	app.insertCouponAudit(
		ctx,
		userID,
		readFlaggedCouponsAction,
		"Read coupons with unresolved moderation flags",
		"bulk-flagged-coupons",
	)

	logger.Info(
		"Flagged coupons retrieved",
		"count", len(coupons),
		"limit", limit,
		"offset", offset,
		"user_id", userID,
	)

	app.respondWithJSON(w, http.StatusOK, jsonResponse{
		Error:   false,
		Message: "Flagged coupons retrieved successfully",
		Data:    coupons,
	})
}

// AnalyzeCouponPerformanceHandler retrieves usage-based performance statistics
// for one coupon.
func (app *Application) AnalyzeCouponPerformanceHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	logger := app.Logger.
		GetLoggerWithContext(r).
		WithFunctionName("AnalyzeCouponPerformanceHandler")

	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	if !app.HasPermission(ctx, analyzeCouponPerformanceAction) {
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

	couponID := app.getContextValueAsUUID(ctx, ctxEntityID)
	if couponID == nil || *couponID == uuid.Nil {
		app.respondWithError(
			w,
			errors.New("coupon ID not found in context"),
			http.StatusBadRequest,
		)
		return
	}

	stats, err := app.Models.Coupon.AnalyzeCouponPerformance(
		ctx,
		*couponID,
	)
	if err != nil {
		logger.Error(
			"Analyze coupon performance failed",
			"coupon_id", *couponID,
			"error", err,
		)
		app.respondWithCouponError(
			w,
			err,
			"failed to analyze coupon performance",
		)
		return
	}

	app.insertCouponAudit(
		ctx,
		userID,
		analyzeCouponPerformanceAction,
		"Analyze coupon performance",
		couponID.String(),
	)

	logger.Info(
		"Coupon performance analyzed",
		"coupon_id", *couponID,
		"user_id", userID,
	)

	app.respondWithJSON(w, http.StatusOK, jsonResponse{
		Error:   false,
		Message: "Coupon performance analyzed successfully",
		Data:    stats,
	})
}

// ClipCouponHandler stores the coupon's parent offer in the authenticated
// user's saved-offer persistence.
func (app *Application) ClipCouponHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	logger := app.Logger.
		GetLoggerWithContext(r).
		WithFunctionName("ClipCouponHandler")

	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	userID := app.getUserIDFromContext(ctx)
	if userID == nil {
		app.respondWithError(
			w,
			errors.New("unauthorized: login required"),
			http.StatusUnauthorized,
		)
		return
	}

	couponID := app.getContextValueAsUUID(ctx, ctxEntityID)
	if couponID == nil || *couponID == uuid.Nil {
		app.respondWithError(
			w,
			errors.New("coupon ID not found in context"),
			http.StatusBadRequest,
		)
		return
	}

	if err := app.Models.Coupon.ClipCoupon(
		ctx,
		*userID,
		*couponID,
	); err != nil {
		logger.Error(
			"Clip coupon failed",
			"coupon_id", *couponID,
			"user_id", *userID,
			"error", err,
		)
		app.respondWithCouponError(
			w,
			err,
			"failed to clip coupon",
		)
		return
	}

	app.insertCouponAudit(
		ctx,
		userID,
		clipCouponAction,
		"Save the coupon parent offer for a user",
		couponID.String(),
	)

	logger.Info(
		"Coupon clipped",
		"coupon_id", *couponID,
		"user_id", *userID,
	)

	app.respondWithJSON(w, http.StatusOK, jsonResponse{
		Error:   false,
		Message: "Coupon clipped successfully",
		Data:    couponID,
	})
}

// GetClippedCouponsHandler retrieves coupons whose parent offers have been
// saved by the authenticated user.
func (app *Application) GetClippedCouponsHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	logger := app.Logger.
		GetLoggerWithContext(r).
		WithFunctionName("GetClippedCouponsHandler")

	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	if !app.HasPermission(ctx, readClippedCouponsAction) {
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

	coupons, err := app.Models.Coupon.GetClippedCoupons(ctx, *userID)
	if err != nil {
		logger.Error(
			"Retrieve clipped coupons failed",
			"user_id", *userID,
			"error", err,
		)
		app.respondWithCouponError(
			w,
			err,
			"failed to retrieve clipped coupons",
		)
		return
	}

	app.insertCouponAudit(
		ctx,
		userID,
		readClippedCouponsAction,
		"Read coupons whose parent offers were saved by a user",
		userID.String(),
	)

	logger.Info(
		"Clipped coupons retrieved",
		"count", len(coupons),
		"user_id", *userID,
	)

	app.respondWithJSON(w, http.StatusOK, jsonResponse{
		Error:   false,
		Message: "Clipped coupons retrieved successfully",
		Data:    coupons,
	})
}

// insertCouponAudit performs best-effort audit logging.
//
// The primary HTTP operation remains successful when audit metadata resolution
// or insertion fails. Every failure is logged, and no nil action or entity type
// is dereferenced.
func (app *Application) insertCouponAudit(
	ctx context.Context,
	userID *uuid.UUID,
	actionName string,
	actionDescription string,
	entityID string,
) {
	logger := app.Logger.
		GetLoggerWithContextFromContext(ctx).
		WithFunctionName("insertCouponAudit")

	action, err := app.Models.Action.GetByName(ctx, actionName)
	if err != nil || action == nil {
		actionID, createErr := app.Models.Action.CreateIfNotExists(
			ctx,
			actionName,
			actionDescription,
		)
		if createErr != nil {
			logger.Warn(
				"Coupon audit action resolution failed",
				"action", actionName,
				"error", createErr,
			)
			return
		}

		action = &data.Action{ID: actionID}
	}

	if action.ID == uuid.Nil {
		logger.Warn(
			"Coupon audit action has empty ID",
			"action", actionName,
		)
		return
	}

	entityType, err := app.Models.EntityType.GetByName(
		ctx,
		couponEntityTypeName,
	)
	if err != nil || entityType == nil {
		entityTypeID, createErr := app.Models.EntityType.CreateIfNotExists(
			ctx,
			couponEntityTypeName,
			couponEntityTypeDescription,
		)
		if createErr != nil {
			logger.Warn(
				"Coupon audit entity type resolution failed",
				"entity_type", couponEntityTypeName,
				"error", createErr,
			)
			return
		}

		entityType = &data.EntityType{ID: entityTypeID}
	}

	if entityType.ID == uuid.Nil {
		logger.Warn(
			"Coupon audit entity type has empty ID",
			"entity_type", couponEntityTypeName,
		)
		return
	}

	audit := &data.AuditLog{
		ID:           uuid.New(),
		UserID:       userID,
		ActionID:     action.ID,
		EntityTypeID: entityType.ID,
		EntityID:     entityID,
	}

	if err := app.Models.AuditLog.Insert(ctx, audit); err != nil {
		logger.Warn(
			"Coupon audit insertion failed",
			"action", actionName,
			"entity_id", entityID,
			"error", err,
		)
	}
}

// respondWithCouponError translates canonical coupon data errors into stable
// HTTP responses.
func (app *Application) respondWithCouponError(
	w http.ResponseWriter,
	err error,
	operation string,
) {
	switch {
	case errors.Is(err, data.ErrCouponNotFound):
		app.respondWithError(
			w,
			errors.New("coupon not found"),
			http.StatusNotFound,
		)

	case errors.Is(err, data.ErrCouponAlreadyExists):
		app.respondWithError(
			w,
			errors.New("a coupon with this offer and code already exists"),
			http.StatusConflict,
		)

	case errors.Is(err, data.ErrCouponOfferRequired):
		app.respondWithError(
			w,
			errors.New("offer_id is required"),
			http.StatusBadRequest,
		)

	case errors.Is(err, data.ErrCouponCodeRequired):
		app.respondWithError(
			w,
			errors.New("code is required"),
			http.StatusBadRequest,
		)

	case errors.Is(err, data.ErrCouponStatusNotFound):
		app.respondWithError(
			w,
			errors.New("coupon status not found"),
			http.StatusUnprocessableEntity,
		)

	case errors.Is(err, data.ErrCouponClipNotSupported):
		app.respondWithError(
			w,
			errors.New(
				"coupon cannot be clipped because it has no linked offer",
			),
			http.StatusUnprocessableEntity,
		)

	default:
		app.respondWithError(
			w,
			fmt.Errorf("%s: %w", operation, err),
			http.StatusInternalServerError,
		)
	}
}

// parseCouponLimit validates a bounded list limit.
func parseCouponLimit(raw string) (int, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return defaultCouponLimit, nil
	}

	limit, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("invalid limit: %w", err)
	}

	if limit < 1 {
		return 0, errors.New("limit must be greater than 0")
	}

	if limit > maxCouponLimit {
		limit = maxCouponLimit
	}

	return limit, nil
}

// parseCouponOffset validates a non-negative list offset.
func parseCouponOffset(raw string) (int, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, nil
	}

	offset, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("invalid offset: %w", err)
	}

	if offset < 0 {
		return 0, errors.New("offset cannot be negative")
	}

	return offset, nil
}