// Package main provides HTTP handlers for merchant program fee schedule
// governance.
//
// focodebase/fobackend/internal/server/cmd/api/merchant_program_fee_schedules.go
//
// GTM:
//
//	Layer: 2.4 Merchant / Future Offering Domain
//	Release Class: SPINE
//	Reason:
//	  merchant_program_fee_schedules is release-critical merchant
//	  monetization infrastructure. This handler surface governs privileged
//	  creation, inspection, effective resolution, lifecycle control, atomic
//	  replacement, and exceptional deletion of effective-dated merchant
//	  commercial policy.
//
//	  Fee schedules establish merchant setup fees, subscription pricing,
//	  Campaign Performance Fees, Future Offering and Launch Intelligence fees,
//	  adjustments, refunds, reversals, and global fallback pricing.
//
//	  Commercial identity and price terms are immutable after insertion.
//	  Pricing changes are represented by new effective-dated rows through the
//	  atomic replacement contract. Effective resolution remains owned by the
//	  data layer.
//
// SPINE Rule:
//
//	Keep compiling.
//	Keep production-ready.
//	Preserve fixed-precision decimal strings at the API boundary.
//	Preserve effective-dated commercial-policy integrity.
//	Preserve immutable commercial identity and price terms.
//	Preserve atomic replacement semantics.
//	Preserve privileged authentication and authorization at every boundary.
//	Preserve centralized governance auditing.
//	Preserve soft-delete, restore, retire, and hard-delete separation.
//	Do not expose raw persistence errors.
//	Do not implement fee calculations in this file.
//	Do not introduce a generic commercial-term update handler.
//	Block deployment if this file breaks merchant billing integrity,
//	Future Offering monetization readiness, permission enforcement,
//	audit accountability, monetary precision, or effective resolution.
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

const (
	merchantProgramFeeScheduleEntityType = "merchant_program_fee_schedule"

	merchantProgramFeeScheduleEntityTypeDescription = "Merchant program fee schedule effective-dated commercial policy entity"

	actionCreateMerchantProgramFeeSchedule = "create_merchant_program_fee_schedule"

	actionReadMerchantProgramFeeSchedule = "read_merchant_program_fee_schedule"

	actionListMerchantProgramFeeSchedules = "list_merchant_program_fee_schedules"

	actionResolveMerchantProgramFeeSchedule = "resolve_merchant_program_fee_schedule"

	actionActivateMerchantProgramFeeSchedule = "activate_merchant_program_fee_schedule"

	actionDeactivateMerchantProgramFeeSchedule = "deactivate_merchant_program_fee_schedule"

	actionRetireMerchantProgramFeeSchedule = "retire_merchant_program_fee_schedule"

	actionReplaceMerchantProgramFeeSchedule = "replace_merchant_program_fee_schedule"

	actionSoftDeleteMerchantProgramFeeSchedule = "soft_delete_merchant_program_fee_schedule"

	actionRestoreMerchantProgramFeeSchedule = "restore_merchant_program_fee_schedule"

	actionHardDeleteMerchantProgramFeeSchedule = "hard_delete_merchant_program_fee_schedule"

	merchantProgramFeeScheduleDefaultLimit = 20

	merchantProgramFeeScheduleMaxLimit = 100

	merchantProgramFeeScheduleDefaultCurrency = "USD"
)

// merchantProgramFeeScheduleWriteRequest is the bounded API input used when
// creating a schedule or constructing the incoming side of an atomic
// replacement.
//
// Clients cannot provide database-owned identifiers or lifecycle timestamps.
// Decimal values remain strings so PostgreSQL NUMERIC values never pass
// through binary floating-point representation.
type merchantProgramFeeScheduleWriteRequest struct {
	FeeType data.MerchantFeeType `json:"fee_type"`

	BillingInterval data.MerchantBillingInterval `json:"billing_interval"`

	CalculationMethod data.MerchantFeeCalculationMethod `json:"calculation_method"`

	FlatAmount *string `json:"flat_amount,omitempty"`

	PercentageRate *string `json:"percentage_rate,omitempty"`

	MinimumFee *string `json:"minimum_fee,omitempty"`

	MaximumFee *string `json:"maximum_fee,omitempty"`

	Currency string `json:"currency,omitempty"`

	IsActive *bool `json:"is_active,omitempty"`

	EffectiveFrom string `json:"effective_from"`

	EffectiveTo *string `json:"effective_to,omitempty"`
}

// merchantProgramFeeScheduleResolveRequest identifies the commercial policy
// that must be resolved at an optional reference time.
type merchantProgramFeeScheduleResolveRequest struct {
	FeeType data.MerchantFeeType `json:"fee_type"`

	BillingInterval data.MerchantBillingInterval `json:"billing_interval"`

	AsOf *string `json:"as_of,omitempty"`
}

// merchantProgramFeeScheduleRetireRequest optionally supplies the exact
// retirement boundary. When omitted, the data model owns the canonical
// application-side UTC-now behavior.
type merchantProgramFeeScheduleRetireRequest struct {
	EffectiveTo *string `json:"effective_to,omitempty"`
}

// merchantProgramFeeScheduleFeeTypeRequest identifies a fee type for a
// compound administrative listing.
type merchantProgramFeeScheduleFeeTypeRequest struct {
	FeeType data.MerchantFeeType `json:"fee_type"`
}

// merchantProgramFeeScheduleHTTPStatus maps stable domain error language to
// HTTP semantics. Original persistence errors remain available to structured
// logs but are never returned directly to clients.
func merchantProgramFeeScheduleHTTPStatus(
	err error,
) int {
	if err == nil {
		return http.StatusOK
	}

	message := strings.ToLower(err.Error())

	switch {
	case strings.Contains(
		message,
		"conflicts with an overlapping active schedule",
	),
		strings.Contains(
			message,
			"resolution is ambiguous",
		),
		strings.Contains(
			message,
			"cannot be retired",
		),
		strings.Contains(
			message,
			"replacement must preserve",
		),
		strings.Contains(
			message,
			"incoming effective_from must be after",
		):
		return http.StatusConflict

	case strings.Contains(message, "not found"),
		strings.Contains(
			message,
			"no mutable merchant program fee schedule",
		),
		strings.Contains(
			message,
			"no non-deleted merchant program fee schedule",
		),
		strings.Contains(
			message,
			"no soft-deleted merchant program fee schedule",
		):
		return http.StatusNotFound

	case strings.Contains(message, "invalid"),
		strings.Contains(message, "required"),
		strings.Contains(message, "must "),
		strings.Contains(message, "incompatible"),
		strings.Contains(
			message,
			"violates a database constraint",
		):
		return http.StatusBadRequest

	default:
		return http.StatusInternalServerError
	}
}

// merchantProgramFeeScheduleClientError returns bounded client-facing error
// text without exposing wrapped database-driver diagnostics.
func merchantProgramFeeScheduleClientError(
	status int,
) error {
	switch status {
	case http.StatusBadRequest:
		return errors.New(
			"invalid merchant program fee schedule request",
		)

	case http.StatusNotFound:
		return errors.New(
			"merchant program fee schedule not found",
		)

	case http.StatusConflict:
		return errors.New(
			"merchant program fee schedule conflicts with existing commercial policy",
		)

	default:
		return errors.New(
			"merchant program fee schedule operation failed",
		)
	}
}

// respondWithMerchantProgramFeeScheduleModelError logs must occur before this
// helper is called. The helper translates and sanitizes the model failure.
func (app *Application) respondWithMerchantProgramFeeScheduleModelError(
	w http.ResponseWriter,
	err error,
) {
	status := merchantProgramFeeScheduleHTTPStatus(err)

	app.respondWithError(
		w,
		merchantProgramFeeScheduleClientError(status),
		status,
	)
}

// parseMerchantProgramFeeScheduleID parses the canonical fee-schedule path
// parameter.
func (app *Application) parseMerchantProgramFeeScheduleID(
	r *http.Request,
) (uuid.UUID, error) {
	raw := strings.TrimSpace(
		chi.URLParam(
			r,
			"merchantProgramFeeScheduleID",
		),
	)
	if raw == "" {
		return uuid.Nil, errors.New(
			"merchant program fee schedule ID is required",
		)
	}

	id, err := uuid.Parse(raw)
	if err != nil || id == uuid.Nil {
		return uuid.Nil, errors.New(
			"invalid merchant program fee schedule ID",
		)
	}

	return id, nil
}

// parseRequiredMerchantProgramFeeScheduleTime parses one required RFC3339
// timestamp and normalizes it to UTC.
func parseRequiredMerchantProgramFeeScheduleTime(
	raw string,
	fieldName string,
) (time.Time, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, fmt.Errorf(
			"%s is required",
			fieldName,
		)
	}

	value, err := time.Parse(
		time.RFC3339,
		raw,
	)
	if err != nil {
		return time.Time{}, fmt.Errorf(
			"%s must be a valid RFC3339 timestamp",
			fieldName,
		)
	}

	return value.UTC(), nil
}

// parseOptionalMerchantProgramFeeScheduleTime parses one optional RFC3339
// timestamp. Nil means omitted. A supplied blank value is invalid.
func parseOptionalMerchantProgramFeeScheduleTime(
	raw *string,
	fieldName string,
) (*time.Time, error) {
	if raw == nil {
		return nil, nil
	}

	value, err :=
		parseRequiredMerchantProgramFeeScheduleTime(
			*raw,
			fieldName,
		)
	if err != nil {
		return nil, err
	}

	return &value, nil
}

// parseMerchantProgramFeeSchedulePagination reads middleware-provided
// pagination values and enforces the same bounds as the data model.
func (app *Application) parseMerchantProgramFeeSchedulePagination(
	ctx context.Context,
) (int, int, error) {
	limit := merchantProgramFeeScheduleDefaultLimit
	offset := 0

	rawLimit := strings.TrimSpace(
		app.getContextValueAsString(
			ctx,
			ctxPaginationLimit,
		),
	)
	if rawLimit != "" {
		value, err := strconv.Atoi(rawLimit)
		if err != nil {
			return 0, 0, errors.New(
				"limit must be a valid integer",
			)
		}
		limit = value
	}

	rawOffset := strings.TrimSpace(
		app.getContextValueAsString(
			ctx,
			ctxPaginationOffset,
		),
	)
	if rawOffset != "" {
		value, err := strconv.Atoi(rawOffset)
		if err != nil {
			return 0, 0, errors.New(
				"offset must be a valid integer",
			)
		}
		offset = value
	}

	if limit < 1 ||
		limit > merchantProgramFeeScheduleMaxLimit {
		return 0, 0, fmt.Errorf(
			"limit must be between 1 and %d",
			merchantProgramFeeScheduleMaxLimit,
		)
	}

	if offset < 0 {
		return 0, 0, errors.New(
			"offset must be non-negative",
		)
	}

	return limit, offset, nil
}

// parseOptionalMerchantProgramFeeScheduleBool parses an optional boolean
// query parameter. Omission evaluates to false.
func parseOptionalMerchantProgramFeeScheduleBool(
	raw string,
	fieldName string,
) (bool, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return false, nil
	}

	value, err := strconv.ParseBool(raw)
	if err != nil {
		return false, fmt.Errorf(
			"%s must be true or false",
			fieldName,
		)
	}

	return value, nil
}

// buildMerchantProgramFeeSchedule constructs a data-layer fee schedule from
// bounded API input.
//
// Controlled values and timestamp shape are checked at the handler boundary.
// Detailed monetary compatibility and commercial-policy validation remain
// owned by the data model.
func buildMerchantProgramFeeSchedule(
	input *merchantProgramFeeScheduleWriteRequest,
) (*data.MerchantProgramFeeSchedule, error) {
	if input == nil {
		return nil, errors.New(
			"merchant program fee schedule input is required",
		)
	}

	feeType :=
		data.NormalizeMerchantFeeType(
			input.FeeType,
		)
	if !data.IsValidMerchantFeeType(feeType) {
		return nil, fmt.Errorf(
			"invalid fee_type: %s",
			input.FeeType,
		)
	}

	billingInterval :=
		data.NormalizeMerchantBillingInterval(
			input.BillingInterval,
		)
	if !data.IsValidMerchantBillingInterval(
		billingInterval,
	) {
		return nil, fmt.Errorf(
			"invalid billing_interval: %s",
			input.BillingInterval,
		)
	}

	if !data.IsMerchantFeeTypeIntervalCompatible(
		feeType,
		billingInterval,
	) {
		return nil, fmt.Errorf(
			"fee_type %s is incompatible with billing_interval %s",
			feeType,
			billingInterval,
		)
	}

	calculationMethod :=
		data.NormalizeMerchantFeeCalculationMethod(
			input.CalculationMethod,
		)
	if !data.IsValidMerchantFeeCalculationMethod(
		calculationMethod,
	) {
		return nil, fmt.Errorf(
			"invalid calculation_method: %s",
			input.CalculationMethod,
		)
	}

	effectiveFrom, err :=
		parseRequiredMerchantProgramFeeScheduleTime(
			input.EffectiveFrom,
			"effective_from",
		)
	if err != nil {
		return nil, err
	}

	effectiveTo, err :=
		parseOptionalMerchantProgramFeeScheduleTime(
			input.EffectiveTo,
			"effective_to",
		)
	if err != nil {
		return nil, err
	}

	isActive := true
	if input.IsActive != nil {
		isActive = *input.IsActive
	}

	currency := strings.ToUpper(
		strings.TrimSpace(input.Currency),
	)
	if currency == "" {
		currency =
			merchantProgramFeeScheduleDefaultCurrency
	}

	return &data.MerchantProgramFeeSchedule{

		FeeType: feeType,

		BillingInterval: billingInterval,

		CalculationMethod: calculationMethod,

		FlatAmount: input.FlatAmount,

		PercentageRate: input.PercentageRate,

		MinimumFee: input.MinimumFee,

		MaximumFee: input.MaximumFee,

		Currency: currency,

		IsActive: isActive,

		EffectiveFrom: effectiveFrom,

		EffectiveTo: effectiveTo,
	}, nil
}

// auditMerchantProgramFeeSchedule delegates merchant fee-schedule governance
// auditing to the centralized governance helper.
func (app *Application) auditMerchantProgramFeeSchedule(
	ctx context.Context,
	userID *uuid.UUID,
	actionName string,
	actionDescription string,
	entityID string,
) error {
	return app.insertGovernanceAudit(
		ctx,
		userID,
		actionName,
		actionDescription,
		merchantProgramFeeScheduleEntityType,
		merchantProgramFeeScheduleEntityTypeDescription,
		entityID,
	)
}

// auditMerchantProgramFeeScheduleBestEffort records governance accountability
// without changing the HTTP result of an already-successful domain operation.
func (app *Application) auditMerchantProgramFeeScheduleBestEffort(
	ctx context.Context,
	logger interface {
		Warn(string, ...any)
	},
	userID *uuid.UUID,
	actionName string,
	actionDescription string,
	entityID string,
) {
	if err := app.auditMerchantProgramFeeSchedule(
		ctx,
		userID,
		actionName,
		actionDescription,
		entityID,
	); err != nil {
		logger.Warn(
			"Merchant program fee schedule operation succeeded "+
				"but audit recording failed",
			"action",
			actionName,
			"entity_id",
			entityID,
			"error",
			err,
		)
	}
}

// CreateMerchantProgramFeeScheduleHandler creates an immutable,
// effective-dated merchant fee schedule.
func (app *Application) CreateMerchantProgramFeeScheduleHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	logger := app.Logger.
		GetLoggerWithContext(r).
		WithFunctionName(
			"CreateMerchantProgramFeeScheduleHandler",
		)

	ctx, cancel := context.WithTimeout(
		r.Context(),
		cfgTimeout,
	)
	defer cancel()

	if !app.HasPermission(
		ctx,
		actionCreateMerchantProgramFeeSchedule,
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

	userID := app.getUserIDFromContext(ctx)
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

	var input merchantProgramFeeScheduleWriteRequest
	if err := app.readJSON(
		w,
		r,
		&input,
	); err != nil {
		app.respondWithError(
			w,
			fmt.Errorf(
				"invalid JSON input: %v",
				err,
			),
			http.StatusBadRequest,
		)
		return
	}

	schedule, err :=
		buildMerchantProgramFeeSchedule(&input)
	if err != nil {
		app.respondWithError(
			w,
			err,
			http.StatusBadRequest,
		)
		return
	}

	if err := app.Models.
		MerchantProgramFeeSchedule.
		Insert(
			ctx,
			schedule,
		); err != nil {
		logger.Error(
			"Create merchant program fee schedule failed",
			"error",
			err,
		)

		app.respondWithMerchantProgramFeeScheduleModelError(
			w,
			err,
		)
		return
	}

	app.auditMerchantProgramFeeScheduleBestEffort(
		ctx,
		logger,
		userID,
		actionCreateMerchantProgramFeeSchedule,
		"Create a merchant program fee schedule",
		schedule.ID.String(),
	)

	logger.Info(
		"Merchant program fee schedule created",
		"fee_schedule_id",
		schedule.ID,
	)

	app.respondWithJSON(
		w,
		http.StatusCreated,
		jsonResponse{
			Error: false,

			Message: "Merchant program fee schedule " +
				"created successfully",

			Data: schedule,
		},
	)
}

// GetMerchantProgramFeeScheduleByIDHandler retrieves one non-deleted fee
// schedule by ID.
func (app *Application) GetMerchantProgramFeeScheduleByIDHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	logger := app.Logger.
		GetLoggerWithContext(r).
		WithFunctionName(
			"GetMerchantProgramFeeScheduleByIDHandler",
		)

	ctx, cancel := context.WithTimeout(
		r.Context(),
		cfgTimeout,
	)
	defer cancel()

	if !app.HasPermission(
		ctx,
		actionReadMerchantProgramFeeSchedule,
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

	userID := app.getUserIDFromContext(ctx)
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

	scheduleID, err :=
		app.parseMerchantProgramFeeScheduleID(r)
	if err != nil {
		app.respondWithError(
			w,
			err,
			http.StatusBadRequest,
		)
		return
	}

	schedule, err := app.Models.
		MerchantProgramFeeSchedule.
		GetByID(
			ctx,
			scheduleID,
		)
	if err != nil {
		logger.Error(
			"Get merchant program fee schedule failed",
			"fee_schedule_id",
			scheduleID,
			"error",
			err,
		)

		app.respondWithMerchantProgramFeeScheduleModelError(
			w,
			err,
		)
		return
	}

	if schedule == nil {
		app.respondWithError(
			w,
			errors.New(
				"merchant program fee schedule not found",
			),
			http.StatusNotFound,
		)
		return
	}

	app.auditMerchantProgramFeeScheduleBestEffort(
		ctx,
		logger,
		userID,
		actionReadMerchantProgramFeeSchedule,
		"Read a merchant program fee schedule",
		schedule.ID.String(),
	)

	app.respondWithJSON(
		w,
		http.StatusOK,
		jsonResponse{
			Error: false,

			Message: "Merchant program fee schedule " +
				"retrieved successfully",

			Data: schedule,
		},
	)
}

// GetAllMerchantProgramFeeSchedulesHandler retrieves a bounded page of fee
// schedules with explicit optional inclusion of soft-deleted rows.
func (app *Application) GetAllMerchantProgramFeeSchedulesHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	logger := app.Logger.
		GetLoggerWithContext(r).
		WithFunctionName(
			"GetAllMerchantProgramFeeSchedulesHandler",
		)

	ctx, cancel := context.WithTimeout(
		r.Context(),
		cfgTimeout,
	)
	defer cancel()

	if !app.HasPermission(
		ctx,
		actionListMerchantProgramFeeSchedules,
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

	userID := app.getUserIDFromContext(ctx)
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

	limit, offset, err :=
		app.parseMerchantProgramFeeSchedulePagination(
			ctx,
		)
	if err != nil {
		app.respondWithError(
			w,
			err,
			http.StatusBadRequest,
		)
		return
	}

	includeDeleted, err :=
		parseOptionalMerchantProgramFeeScheduleBool(
			r.URL.Query().Get(
				"include_deleted",
			),
			"include_deleted",
		)
	if err != nil {
		app.respondWithError(
			w,
			err,
			http.StatusBadRequest,
		)
		return
	}

	schedules, err := app.Models.
		MerchantProgramFeeSchedule.
		GetAll(
			ctx,
			includeDeleted,
			limit,
			offset,
		)
	if err != nil {
		logger.Error(
			"List merchant program fee schedules failed",
			"include_deleted",
			includeDeleted,
			"limit",
			limit,
			"offset",
			offset,
			"error",
			err,
		)

		app.respondWithMerchantProgramFeeScheduleModelError(
			w,
			err,
		)
		return
	}

	app.auditMerchantProgramFeeScheduleBestEffort(
		ctx,
		logger,
		userID,
		actionListMerchantProgramFeeSchedules,
		"List merchant program fee schedules",
		"*",
	)

	app.respondWithJSON(
		w,
		http.StatusOK,
		jsonResponse{
			Error: false,

			Message: "Merchant program fee schedules " +
				"retrieved successfully",

			Data: schedules,
		},
	)
}


// ListMerchantProgramFeeSchedulesByFeeTypeHandler retrieves a bounded page
// of non-deleted schedules for one canonical fee type.
func (app *Application) ListMerchantProgramFeeSchedulesByFeeTypeHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	logger := app.Logger.
		GetLoggerWithContext(r).
		WithFunctionName(
			"ListMerchantProgramFeeSchedulesByFeeTypeHandler",
		)

	ctx, cancel := context.WithTimeout(
		r.Context(),
		cfgTimeout,
	)
	defer cancel()

	if !app.HasPermission(
		ctx,
		actionListMerchantProgramFeeSchedules,
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

	userID := app.getUserIDFromContext(ctx)
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

	var input merchantProgramFeeScheduleFeeTypeRequest
	if err := app.readJSON(
		w,
		r,
		&input,
	); err != nil {
		app.respondWithError(
			w,
			fmt.Errorf(
				"invalid JSON input: %v",
				err,
			),
			http.StatusBadRequest,
		)
		return
	}

	feeType :=
		data.NormalizeMerchantFeeType(
			input.FeeType,
		)
	if !data.IsValidMerchantFeeType(feeType) {
		app.respondWithError(
			w,
			fmt.Errorf(
				"invalid fee_type: %s",
				input.FeeType,
			),
			http.StatusBadRequest,
		)
		return
	}

	limit, offset, err :=
		app.parseMerchantProgramFeeSchedulePagination(
			ctx,
		)
	if err != nil {
		app.respondWithError(
			w,
			err,
			http.StatusBadRequest,
		)
		return
	}

	schedules, err := app.Models.
		MerchantProgramFeeSchedule.
		ListByFeeType(
			ctx,
			feeType,
			limit,
			offset,
		)
	if err != nil {
		logger.Error(
			"List merchant program fee schedules by fee type failed",
			"fee_type",
			feeType,
			"limit",
			limit,
			"offset",
			offset,
			"error",
			err,
		)

		app.respondWithMerchantProgramFeeScheduleModelError(
			w,
			err,
		)
		return
	}

	app.auditMerchantProgramFeeScheduleBestEffort(
		ctx,
		logger,
		userID,
		actionListMerchantProgramFeeSchedules,
		"List merchant program fee schedules by fee type",
		string(feeType),
	)

	app.respondWithJSON(
		w,
		http.StatusOK,
		jsonResponse{
			Error: false,

			Message: "Merchant program fee schedules " +
				"retrieved successfully",

			Data: schedules,
		},
	)
}

// ResolveEffectiveMerchantProgramFeeScheduleHandler resolves the active
// commercial policy applicable at an optional reference time.
//
// Active-window filtering, and
// ambiguity detection remain entirely data-owned. This operational lookup is
// intentionally not audit-logged because billing and fee-calculation flows
// may invoke it at high frequency.
func (app *Application) ResolveEffectiveMerchantProgramFeeScheduleHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	logger := app.Logger.
		GetLoggerWithContext(r).
		WithFunctionName(
			"ResolveEffectiveMerchantProgramFeeScheduleHandler",
		)

	ctx, cancel := context.WithTimeout(
		r.Context(),
		cfgTimeout,
	)
	defer cancel()

	if !app.HasPermission(
		ctx,
		actionResolveMerchantProgramFeeSchedule,
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

	if app.getUserIDFromContext(ctx) == nil {
		app.respondWithError(
			w,
			errors.New(
				"user ID not found in context",
			),
			http.StatusUnauthorized,
		)
		return
	}

	var input merchantProgramFeeScheduleResolveRequest
	if err := app.readJSON(
		w,
		r,
		&input,
	); err != nil {
		app.respondWithError(
			w,
			fmt.Errorf(
				"invalid JSON input: %v",
				err,
			),
			http.StatusBadRequest,
		)
		return
	}

	feeType :=
		data.NormalizeMerchantFeeType(
			input.FeeType,
		)
	if !data.IsValidMerchantFeeType(feeType) {
		app.respondWithError(
			w,
			fmt.Errorf(
				"invalid fee_type: %s",
				input.FeeType,
			),
			http.StatusBadRequest,
		)
		return
	}

	billingInterval :=
		data.NormalizeMerchantBillingInterval(
			input.BillingInterval,
		)
	if !data.IsValidMerchantBillingInterval(
		billingInterval,
	) {
		app.respondWithError(
			w,
			fmt.Errorf(
				"invalid billing_interval: %s",
				input.BillingInterval,
			),
			http.StatusBadRequest,
		)
		return
	}

	if !data.IsMerchantFeeTypeIntervalCompatible(
		feeType,
		billingInterval,
	) {
		app.respondWithError(
			w,
			fmt.Errorf(
				"fee_type %s is incompatible with billing_interval %s",
				feeType,
				billingInterval,
			),
			http.StatusBadRequest,
		)
		return
	}

	var asOf time.Time

	if input.AsOf != nil {
		parsed, err :=
			parseRequiredMerchantProgramFeeScheduleTime(
				*input.AsOf,
				"as_of",
			)
		if err != nil {
			app.respondWithError(
				w,
				err,
				http.StatusBadRequest,
			)
			return
		}

		asOf = parsed
	}

	schedule, err := app.Models.
		MerchantProgramFeeSchedule.
		ResolveEffective(
			ctx,
			feeType,
			billingInterval,
			asOf,
		)
	if err != nil {
		logger.Error(
			"Resolve effective merchant program fee schedule failed",
			"fee_type",
			feeType,
			"billing_interval",
			billingInterval,
			"error",
			err,
		)

		app.respondWithMerchantProgramFeeScheduleModelError(
			w,
			err,
		)
		return
	}

	if schedule == nil {
		app.respondWithError(
			w,
			errors.New(
				"no effective merchant program fee schedule found",
			),
			http.StatusNotFound,
		)
		return
	}

	app.respondWithJSON(
		w,
		http.StatusOK,
		jsonResponse{
			Error: false,

			Message: "Effective merchant program fee schedule " +
				"resolved successfully",

			Data: schedule,
		},
	)
}

// setMerchantProgramFeeScheduleActiveState performs the shared authenticated,
// authorized, audited handler flow for activation and deactivation.
func (app *Application) setMerchantProgramFeeScheduleActiveState(
	w http.ResponseWriter,
	r *http.Request,
	active bool,
) {
	functionName :=
		"DeactivateMerchantProgramFeeScheduleHandler"

	actionName :=
		actionDeactivateMerchantProgramFeeSchedule

	actionDescription :=
		"Deactivate a merchant program fee schedule"

	successMessage :=
		"Merchant program fee schedule deactivated successfully"

	if active {
		functionName =
			"ActivateMerchantProgramFeeScheduleHandler"

		actionName =
			actionActivateMerchantProgramFeeSchedule

		actionDescription =
			"Activate a merchant program fee schedule"

		successMessage =
			"Merchant program fee schedule activated successfully"
	}

	logger := app.Logger.
		GetLoggerWithContext(r).
		WithFunctionName(functionName)

	ctx, cancel := context.WithTimeout(
		r.Context(),
		cfgTimeout,
	)
	defer cancel()

	if !app.HasPermission(
		ctx,
		actionName,
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

	userID := app.getUserIDFromContext(ctx)
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

	scheduleID, err :=
		app.parseMerchantProgramFeeScheduleID(r)
	if err != nil {
		app.respondWithError(
			w,
			err,
			http.StatusBadRequest,
		)
		return
	}

	if active {
		err = app.Models.
			MerchantProgramFeeSchedule.
			Activate(
				ctx,
				scheduleID,
			)
	} else {
		err = app.Models.
			MerchantProgramFeeSchedule.
			Deactivate(
				ctx,
				scheduleID,
			)
	}

	if err != nil {
		logger.Error(
			"Set merchant program fee schedule active state failed",
			"fee_schedule_id",
			scheduleID,
			"is_active",
			active,
			"error",
			err,
		)

		app.respondWithMerchantProgramFeeScheduleModelError(
			w,
			err,
		)
		return
	}

	app.auditMerchantProgramFeeScheduleBestEffort(
		ctx,
		logger,
		userID,
		actionName,
		actionDescription,
		scheduleID.String(),
	)

	logger.Info(
		"Merchant program fee schedule active state changed",
		"fee_schedule_id",
		scheduleID,
		"is_active",
		active,
	)

	app.respondWithJSON(
		w,
		http.StatusOK,
		jsonResponse{
			Error: false,

			Message: successMessage,

			Data: scheduleID,
		},
	)
}

// ActivateMerchantProgramFeeScheduleHandler activates a non-deleted fee
// schedule.
func (app *Application) ActivateMerchantProgramFeeScheduleHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	app.setMerchantProgramFeeScheduleActiveState(
		w,
		r,
		true,
	)
}

// DeactivateMerchantProgramFeeScheduleHandler deactivates a non-deleted fee
// schedule.
func (app *Application) DeactivateMerchantProgramFeeScheduleHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	app.setMerchantProgramFeeScheduleActiveState(
		w,
		r,
		false,
	)
}

// RetireMerchantProgramFeeScheduleHandler closes a schedule's effective
// window and deactivates it without rewriting historical commercial terms.
func (app *Application) RetireMerchantProgramFeeScheduleHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	logger := app.Logger.
		GetLoggerWithContext(r).
		WithFunctionName(
			"RetireMerchantProgramFeeScheduleHandler",
		)

	ctx, cancel := context.WithTimeout(
		r.Context(),
		cfgTimeout,
	)
	defer cancel()

	if !app.HasPermission(
		ctx,
		actionRetireMerchantProgramFeeSchedule,
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

	userID := app.getUserIDFromContext(ctx)
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

	scheduleID, err :=
		app.parseMerchantProgramFeeScheduleID(r)
	if err != nil {
		app.respondWithError(
			w,
			err,
			http.StatusBadRequest,
		)
		return
	}

	var input merchantProgramFeeScheduleRetireRequest
	if err := app.readJSON(
		w,
		r,
		&input,
	); err != nil {
		app.respondWithError(
			w,
			fmt.Errorf(
				"invalid JSON input: %v",
				err,
			),
			http.StatusBadRequest,
		)
		return
	}

	var effectiveTo time.Time

	if input.EffectiveTo != nil {
		parsed, err :=
			parseRequiredMerchantProgramFeeScheduleTime(
				*input.EffectiveTo,
				"effective_to",
			)
		if err != nil {
			app.respondWithError(
				w,
				err,
				http.StatusBadRequest,
			)
			return
		}

		effectiveTo = parsed
	}

	if err := app.Models.
		MerchantProgramFeeSchedule.
		Retire(
			ctx,
			scheduleID,
			effectiveTo,
		); err != nil {
		logger.Error(
			"Retire merchant program fee schedule failed",
			"fee_schedule_id",
			scheduleID,
			"error",
			err,
		)

		app.respondWithMerchantProgramFeeScheduleModelError(
			w,
			err,
		)
		return
	}

	app.auditMerchantProgramFeeScheduleBestEffort(
		ctx,
		logger,
		userID,
		actionRetireMerchantProgramFeeSchedule,
		"Retire a merchant program fee schedule",
		scheduleID.String(),
	)

	logger.Info(
		"Merchant program fee schedule retired",
		"fee_schedule_id",
		scheduleID,
	)

	app.respondWithJSON(
		w,
		http.StatusOK,
		jsonResponse{
			Error: false,

			Message: "Merchant program fee schedule " +
				"retired successfully",

			Data: scheduleID,
		},
	)
}

// ReplaceMerchantProgramFeeScheduleHandler atomically retires the outgoing
// schedule and inserts its effective-dated successor.
func (app *Application) ReplaceMerchantProgramFeeScheduleHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	logger := app.Logger.
		GetLoggerWithContext(r).
		WithFunctionName(
			"ReplaceMerchantProgramFeeScheduleHandler",
		)

	ctx, cancel := context.WithTimeout(
		r.Context(),
		cfgTimeout,
	)
	defer cancel()

	if !app.HasPermission(
		ctx,
		actionReplaceMerchantProgramFeeSchedule,
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

	userID := app.getUserIDFromContext(ctx)
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

	outgoingID, err :=
		app.parseMerchantProgramFeeScheduleID(r)
	if err != nil {
		app.respondWithError(
			w,
			err,
			http.StatusBadRequest,
		)
		return
	}

	var input merchantProgramFeeScheduleWriteRequest
	if err := app.readJSON(
		w,
		r,
		&input,
	); err != nil {
		app.respondWithError(
			w,
			fmt.Errorf(
				"invalid JSON input: %v",
				err,
			),
			http.StatusBadRequest,
		)
		return
	}

	incoming, err :=
		buildMerchantProgramFeeSchedule(&input)
	if err != nil {
		app.respondWithError(
			w,
			err,
			http.StatusBadRequest,
		)
		return
	}

	if err := app.Models.
		MerchantProgramFeeSchedule.
		Replace(
			ctx,
			outgoingID,
			incoming,
		); err != nil {
		logger.Error(
			"Replace merchant program fee schedule failed",
			"outgoing_fee_schedule_id",
			outgoingID,
			"error",
			err,
		)

		app.respondWithMerchantProgramFeeScheduleModelError(
			w,
			err,
		)
		return
	}

	entityID := fmt.Sprintf(
		"%s->%s",
		outgoingID,
		incoming.ID,
	)

	app.auditMerchantProgramFeeScheduleBestEffort(
		ctx,
		logger,
		userID,
		actionReplaceMerchantProgramFeeSchedule,
		"Replace a merchant program fee schedule",
		entityID,
	)

	logger.Info(
		"Merchant program fee schedule replaced",
		"outgoing_fee_schedule_id",
		outgoingID,
		"incoming_fee_schedule_id",
		incoming.ID,
	)

	app.respondWithJSON(
		w,
		http.StatusCreated,
		jsonResponse{
			Error: false,

			Message: "Merchant program fee schedule " +
				"replaced successfully",

			Data: incoming,
		},
	)
}

// SoftDeleteMerchantProgramFeeScheduleHandler performs the normal
// history-preserving removal operation.
func (app *Application) SoftDeleteMerchantProgramFeeScheduleHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	app.mutateMerchantProgramFeeScheduleByID(
		w,
		r,
		"SoftDeleteMerchantProgramFeeScheduleHandler",
		actionSoftDeleteMerchantProgramFeeSchedule,
		"Soft-delete a merchant program fee schedule",
		"Merchant program fee schedule soft-deleted successfully",
		func(
			ctx context.Context,
			id uuid.UUID,
		) error {
			return app.Models.
				MerchantProgramFeeSchedule.
				SoftDelete(
					ctx,
					id,
				)
		},
	)
}

// RestoreMerchantProgramFeeScheduleHandler restores a soft-deleted schedule.
// The data model restores it in an inactive state.
func (app *Application) RestoreMerchantProgramFeeScheduleHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	app.mutateMerchantProgramFeeScheduleByID(
		w,
		r,
		"RestoreMerchantProgramFeeScheduleHandler",
		actionRestoreMerchantProgramFeeSchedule,
		"Restore a merchant program fee schedule",
		"Merchant program fee schedule restored successfully",
		func(
			ctx context.Context,
			id uuid.UUID,
		) error {
			return app.Models.
				MerchantProgramFeeSchedule.
				Restore(
					ctx,
					id,
				)
		},
	)
}

// HardDeleteMerchantProgramFeeScheduleHandler permanently deletes a fee
// schedule. This exceptional administrative operation is never an alias for
// soft deletion.
func (app *Application) HardDeleteMerchantProgramFeeScheduleHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	app.mutateMerchantProgramFeeScheduleByID(
		w,
		r,
		"HardDeleteMerchantProgramFeeScheduleHandler",
		actionHardDeleteMerchantProgramFeeSchedule,
		"Permanently delete a merchant program fee schedule",
		"Merchant program fee schedule permanently deleted successfully",
		func(
			ctx context.Context,
			id uuid.UUID,
		) error {
			return app.Models.
				MerchantProgramFeeSchedule.
				HardDelete(
					ctx,
					id,
				)
		},
	)
}

// mutateMerchantProgramFeeScheduleByID provides the shared privileged,
// authenticated, audited execution boundary for ID-only lifecycle mutations.
func (app *Application) mutateMerchantProgramFeeScheduleByID(
	w http.ResponseWriter,
	r *http.Request,
	functionName string,
	actionName string,
	actionDescription string,
	successMessage string,
	mutation func(
		context.Context,
		uuid.UUID,
	) error,
) {
	logger := app.Logger.
		GetLoggerWithContext(r).
		WithFunctionName(functionName)

	ctx, cancel := context.WithTimeout(
		r.Context(),
		cfgTimeout,
	)
	defer cancel()

	if !app.HasPermission(
		ctx,
		actionName,
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

	userID := app.getUserIDFromContext(ctx)
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

	scheduleID, err :=
		app.parseMerchantProgramFeeScheduleID(r)
	if err != nil {
		app.respondWithError(
			w,
			err,
			http.StatusBadRequest,
		)
		return
	}

	if err := mutation(
		ctx,
		scheduleID,
	); err != nil {
		logger.Error(
			"Merchant program fee schedule mutation failed",
			"fee_schedule_id",
			scheduleID,
			"action",
			actionName,
			"error",
			err,
		)

		app.respondWithMerchantProgramFeeScheduleModelError(
			w,
			err,
		)
		return
	}

	app.auditMerchantProgramFeeScheduleBestEffort(
		ctx,
		logger,
		userID,
		actionName,
		actionDescription,
		scheduleID.String(),
	)

	logger.Info(
		"Merchant program fee schedule mutation succeeded",
		"fee_schedule_id",
		scheduleID,
		"action",
		actionName,
	)

	app.respondWithJSON(
		w,
		http.StatusOK,
		jsonResponse{
			Error: false,

			Message: successMessage,

			Data: scheduleID,
		},
	)
}
