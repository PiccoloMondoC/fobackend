// Package main provides HTTP handlers for the Platform API.
//
// sdworkspace/sdbackend/internal/server/cmd/api/user_activation.go
//
// GTM:
//
//	Layer: 2.2 Identity / Auth Domain
//	Release Class: SPINE
//	Reason:
//	  User activation is release-critical identity infrastructure. These
//	  handlers create and deliver activation credentials, activate eligible
//	  user accounts, expose authenticated activation-status reads, and preserve
//	  the audit trail required by the initial Platform release spine.
//
// SPINE Rule:
//
//	Keep compiling.
//	Keep production-ready.
//	Preserve plaintext activation tokens only at controlled HTTP and
//	notification boundaries.
//	Preserve token hashing and token-hash lookup ownership in the data layer.
//	Preserve authenticated activation-token creation and delivery.
//	Preserve context-aware notification delivery through the root
//	notification_services EmailSender and SMSSender interfaces only.
//	Preserve contact-data ownership in the canonical user model rather than
//	user settings.
//	Preserve pre-seeded and preloaded audit action and entity-type governance.
//	Do not invoke one HTTP handler from another HTTP handler.
//	Do not dynamically create audit actions or entity types during requests.
//	Do not log, audit, trace, or return activation URLs or activation tokens
//	except where the endpoint contract explicitly returns the newly generated
//	plaintext token.
//	Block deployment if this file breaks build, activation-token issuance,
//	activation-link delivery, account activation, activation-status reads,
//	notification boundaries, or activation audit integrity.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/PiccoloMondoC/sdworkspace/sdbackend/internal/data"
	notificationservices "github.com/PiccoloMondoC/sdworkspace/sdbackend/internal/notification_services"

	"github.com/google/uuid"
)

const (
	activationPath = "/activate"

	activationChannelEmail = "email"
	activationChannelSMS   = "sms"
)

// CreateActivationTokenHandler creates a new activation token for the
// authenticated user.
//
// The plaintext token is returned once through the controlled response boundary.
// Persistence of the protected token hash remains owned by the data layer.
func (app *Application) CreateActivationTokenHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	logger := app.Logger.
		GetLoggerWithContext(r).
		WithFunctionName("CreateActivationTokenHandler")

	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	userID := app.getUserIDFromContext(ctx)
	if userID == nil || *userID == uuid.Nil {
		app.respondWithError(
			w,
			errors.New("unauthorized"),
			http.StatusUnauthorized,
		)
		return
	}

	token, err := retryActivationTokenWithBackoff(
		ctx,
		func() (string, error) {
			return app.Models.ActivationToken.CreateActivationToken(ctx, *userID)
		},
	)
	if err != nil {
		logger.Warn(
			"activation token creation failed",
			"user_id", *userID,
			"error", err,
		)

		switch {
		case errors.Is(err, data.ErrUserNotFound):
			app.respondWithError(
				w,
				errors.New("user not found"),
				http.StatusNotFound,
			)

		case errors.Is(err, context.Canceled),
			errors.Is(err, context.DeadlineExceeded):
			app.respondWithError(
				w,
				errors.New("activation token request timed out"),
				http.StatusGatewayTimeout,
			)

		default:
			app.respondWithError(
				w,
				errors.New("failed to create activation token"),
				http.StatusInternalServerError,
			)
		}
		return
	}

	if err := app.insertUserActivationAudit(
		ctx,
		userID,
		"create_activation_token",
		userID.String(),
	); err != nil {
		logger.Warn(
			"activation token created but audit insertion failed",
			"user_id", *userID,
			"error", err,
		)

		app.respondWithJSON(
			w,
			http.StatusPartialContent,
			jsonResponse{
				Error:   false,
				Message: "Activation token created, but audit logging failed",
				Data: struct {
					ActivationToken string `json:"activation_token"`
				}{
					ActivationToken: token,
				},
			},
		)
		return
	}

	logger.Info(
		"activation token created",
		"user_id", *userID,
	)

	app.respondWithJSON(
		w,
		http.StatusCreated,
		jsonResponse{
			Error:   false,
			Message: "Activation token created successfully",
			Data: struct {
				ActivationToken string `json:"activation_token"`
			}{
				ActivationToken: token,
			},
		},
	)
}

// SendActivationLinkHandler creates an activation token and delivers an
// activation link through the user's canonical preferred contact channel.
func (app *Application) SendActivationLinkHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	logger := app.Logger.
		GetLoggerWithContext(r).
		WithFunctionName("SendActivationLinkHandler")

	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	userID := app.getUserIDFromContext(ctx)
	if userID == nil || *userID == uuid.Nil {
		app.respondWithError(
			w,
			errors.New("unauthorized"),
			http.StatusUnauthorized,
		)
		return
	}

	contact, err := app.Models.User.GetUserContactInfo(ctx, *userID)
	if err != nil {
		logger.Warn(
			"user contact lookup failed",
			"user_id", *userID,
			"error", err,
		)

		if errors.Is(err, data.ErrUserNotFound) {
			app.respondWithError(
				w,
				errors.New("user not found"),
				http.StatusNotFound,
			)
			return
		}

		app.respondWithError(
			w,
			errors.New("failed to load user contact information"),
			http.StatusInternalServerError,
		)
		return
	}

	if contact == nil {
		app.respondWithError(
			w,
			errors.New("user contact information not found"),
			http.StatusNotFound,
		)
		return
	}

	token, err := retryActivationTokenWithBackoff(
		ctx,
		func() (string, error) {
			return app.Models.ActivationToken.CreateActivationToken(ctx, *userID)
		},
	)
	if err != nil {
		logger.Warn(
			"activation token creation failed",
			"user_id", *userID,
			"error", err,
		)

		switch {
		case errors.Is(err, data.ErrUserNotFound):
			app.respondWithError(
				w,
				errors.New("user not found"),
				http.StatusNotFound,
			)

		case errors.Is(err, context.Canceled),
			errors.Is(err, context.DeadlineExceeded):
			app.respondWithError(
				w,
				errors.New("activation-link request timed out"),
				http.StatusGatewayTimeout,
			)

		default:
			app.respondWithError(
				w,
				errors.New("failed to create activation link"),
				http.StatusInternalServerError,
			)
		}
		return
	}

	activationURL, err := buildActivationURL(
		app.Config.Bootstrap.BaseURL,
		token,
	)
	if err != nil {
		logger.Error(
			"activation URL construction failed",
			"user_id", *userID,
			"error", err,
		)

		app.respondWithError(
			w,
			errors.New("activation service is not configured correctly"),
			http.StatusInternalServerError,
		)
		return
	}

	channel, err := app.sendActivationNotification(
		ctx,
		*userID,
		contact,
		activationURL,
	)
	if err != nil {
		logger.Warn(
			"activation notification delivery failed",
			"user_id", *userID,
			"channel", channel,
			"error", err,
		)

		app.respondWithError(
			w,
			errors.New("failed to send activation link"),
			http.StatusBadGateway,
		)
		return
	}

	if err := app.insertUserActivationAudit(
		ctx,
		userID,
		"send_activation_link",
		userID.String(),
	); err != nil {
		logger.Warn(
			"activation link sent but audit insertion failed",
			"user_id", *userID,
			"channel", channel,
			"error", err,
		)

		app.respondWithJSON(
			w,
			http.StatusPartialContent,
			jsonResponse{
				Error:   false,
				Message: "Activation link sent, but audit logging failed",
				Data: struct {
					Channel string `json:"channel"`
				}{
					Channel: channel,
				},
			},
		)
		return
	}

	logger.Info(
		"activation link sent",
		"user_id", *userID,
		"channel", channel,
	)

	app.respondWithJSON(
		w,
		http.StatusOK,
		jsonResponse{
			Error:   false,
			Message: "Activation link sent successfully",
			Data: struct {
				Channel string `json:"channel"`
			}{
				Channel: channel,
			},
		},
	)
}

// ActivateUserHandler validates a plaintext activation token and activates the
// associated user account.
//
// Token hashing, token lookup, expiry validation, account mutation, and token
// consumption remain atomic data-layer responsibilities.
func (app *Application) ActivateUserHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	logger := app.Logger.
		GetLoggerWithContext(r).
		WithFunctionName("ActivateUserHandler")

	var input struct {
		Token string `json:"token"`
	}

	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()

	if err := decoder.Decode(&input); err != nil {
		app.respondWithError(
			w,
			errors.New("invalid request body"),
			http.StatusBadRequest,
		)
		return
	}

	input.Token = strings.TrimSpace(input.Token)
	if input.Token == "" {
		app.respondWithError(
			w,
			errors.New("activation token is required"),
			http.StatusBadRequest,
		)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	// ActivateUser accepts the plaintext token and owns canonical hashing before
	// persistence lookup. The API layer must not pre-hash or double-hash it.
	userID, err := app.Models.ActivationToken.ActivateUser(ctx, input.Token)
	if err != nil {
		counterKey := activationFailureCounterKey(r)
		failedAttempts := trackFailedAttempt(counterKey)

		logger.Warn(
			"user activation failed",
			"failed_attempts", failedAttempts,
			"error", err,
		)

		if auditErr := app.insertUserActivationAudit(
			ctx,
			nil,
			"failed_activate_user",
			"activation_attempt",
		); auditErr != nil {
			logger.Warn(
				"failed activation audit insertion failed",
				"error", auditErr,
			)
		}

		switch {
		case errors.Is(err, data.ErrActivationTokenRequired):
			app.respondWithError(
				w,
				errors.New("activation token is required"),
				http.StatusBadRequest,
			)

		case errors.Is(err, data.ErrActivationTokenInvalid),
			errors.Is(err, data.ErrActivationTokenExpired),
			errors.Is(err, data.ErrActivationTokenNotFound):
			app.respondWithError(
				w,
				errors.New("activation token is invalid or expired"),
				http.StatusUnauthorized,
			)

		case errors.Is(err, data.ErrUserNotFound):
			app.respondWithError(
				w,
				errors.New("activation failed"),
				http.StatusUnauthorized,
			)

		case errors.Is(err, context.Canceled),
			errors.Is(err, context.DeadlineExceeded):
			app.respondWithError(
				w,
				errors.New("activation request timed out"),
				http.StatusGatewayTimeout,
			)

		default:
			app.respondWithError(
				w,
				errors.New("activation failed"),
				http.StatusInternalServerError,
			)
		}
		return
	}

	resetFailedAttempts(activationFailureCounterKey(r))

	if err := app.insertUserActivationAudit(
		ctx,
		&userID,
		"activate_user",
		userID.String(),
	); err != nil {
		logger.Warn(
			"user activated but audit insertion failed",
			"user_id", userID,
			"error", err,
		)

		app.respondWithJSON(
			w,
			http.StatusPartialContent,
			jsonResponse{
				Error:   false,
				Message: "User activated, but audit logging failed",
				Data: struct {
					UserID uuid.UUID `json:"user_id"`
				}{
					UserID: userID,
				},
			},
		)
		return
	}

	logger.Info(
		"user activated",
		"user_id", userID,
	)

	app.respondWithJSON(
		w,
		http.StatusOK,
		jsonResponse{
			Error:   false,
			Message: "User activated successfully",
			Data: struct {
				UserID uuid.UUID `json:"user_id"`
			}{
				UserID: userID,
			},
		},
	)
}

// GetUserActivationStatusHandler returns the authenticated user's current
// activation status.
func (app *Application) GetUserActivationStatusHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	logger := app.Logger.
		GetLoggerWithContext(r).
		WithFunctionName("GetUserActivationStatusHandler")

	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	userID := app.getUserIDFromContext(ctx)
	if userID == nil || *userID == uuid.Nil {
		app.respondWithError(
			w,
			errors.New("unauthorized"),
			http.StatusUnauthorized,
		)
		return
	}

	isActive, err := app.Models.ActivationToken.GetUserActivationStatus(
		ctx,
		*userID,
	)
	if err != nil {
		logger.Warn(
			"activation status lookup failed",
			"user_id", *userID,
			"error", err,
		)

		switch {
		case errors.Is(err, data.ErrUserNotFound):
			app.respondWithError(
				w,
				errors.New("user not found"),
				http.StatusNotFound,
			)

		case errors.Is(err, context.Canceled),
			errors.Is(err, context.DeadlineExceeded):
			app.respondWithError(
				w,
				errors.New("activation-status request timed out"),
				http.StatusGatewayTimeout,
			)

		default:
			app.respondWithError(
				w,
				errors.New("failed to retrieve activation status"),
				http.StatusInternalServerError,
			)
		}
		return
	}

	responseData := struct {
		IsActive bool `json:"is_active"`
	}{
		IsActive: isActive,
	}

	if err := app.insertUserActivationAudit(
		ctx,
		userID,
		"get_user_activation_status",
		userID.String(),
	); err != nil {
		logger.Warn(
			"activation status retrieved but audit insertion failed",
			"user_id", *userID,
			"error", err,
		)

		app.respondWithJSON(
			w,
			http.StatusPartialContent,
			jsonResponse{
				Error:   false,
				Message: "Activation status retrieved, but audit logging failed",
				Data:    responseData,
			},
		)
		return
	}

	logger.Info(
		"activation status retrieved",
		"user_id", *userID,
		"is_active", isActive,
	)

	app.respondWithJSON(
		w,
		http.StatusOK,
		jsonResponse{
			Error:   false,
			Message: "Activation status retrieved successfully",
			Data:    responseData,
		},
	)
}

// sendActivationNotification sends an activation link through the canonical
// contact channel selected by the user.
//
// SMS is used only when it is explicitly preferred and a phone number exists.
// All other cases use email. Missing preferred-channel contact data fails
// rather than silently sending through an unintended channel.
func (app *Application) sendActivationNotification(
	ctx context.Context,
	userID uuid.UUID,
	contact *data.UserContactInfo,
	activationURL string,
) (string, error) {
	if contact == nil {
		return "", errors.New("activation contact information is required")
	}

	preferredMethod := strings.ToLower(
		strings.TrimSpace(contact.PreferredMethod),
	)

	switch preferredMethod {
	case activationChannelSMS:
		phone := strings.TrimSpace(contact.Phone)
		if phone == "" {
			return activationChannelSMS, errors.New(
				"preferred SMS contact is unavailable",
			)
		}

		err := app.SMSService.SendActivationSMSContextWithOptions(
			ctx,
			phone,
			activationURL,
			notificationservices.SendOptions{
				CorrelationID: userID.String(),
				Flow:          notificationservices.FlowAccountActivation,
			},
		)
		if err != nil {
			return activationChannelSMS, err
		}

		return activationChannelSMS, nil

	case "", activationChannelEmail:
		email := strings.TrimSpace(contact.Email)
		if email == "" {
			return activationChannelEmail, errors.New(
				"activation email contact is unavailable",
			)
		}

		err := app.EmailService.SendActivationEmailContext(
			ctx,
			email,
			activationURL,
		)
		if err != nil {
			return activationChannelEmail, err
		}

		return activationChannelEmail, nil

	default:
		return preferredMethod, fmt.Errorf(
			"unsupported activation contact method %q",
			preferredMethod,
		)
	}
}

// insertUserActivationAudit inserts a user-activation audit event using
// readiness-preloaded governance identifiers.
//
// Actions and entity types are canonical seeded data. Request handlers must not
// create or repair governance rows dynamically.
func (app *Application) insertUserActivationAudit(
	ctx context.Context,
	userID *uuid.UUID,
	actionName string,
	entityID string,
) error {
	if app.Preloaded == nil {
		return errors.New("activation audit preload is unavailable")
	}

	actionID, ok := app.Preloaded.ActionIDs[actionName]
	if !ok || actionID == uuid.Nil {
		return fmt.Errorf(
			"activation audit action %q is not preloaded",
			actionName,
		)
	}

	entityTypeID, ok := app.Preloaded.EntityTypeIDs["users"]
	if !ok || entityTypeID == uuid.Nil {
		return errors.New(
			"activation audit entity type \"users\" is not preloaded",
		)
	}

	entityID = strings.TrimSpace(entityID)
	if entityID == "" {
		return errors.New("activation audit entity ID is required")
	}

	auditLog := data.AuditLog{
		ID:           uuid.New(),
		UserID:       userID,
		ActionID:     actionID,
		EntityTypeID: entityTypeID,
		EntityID:     entityID,
	}

	if err := app.Models.AuditLog.Insert(ctx, &auditLog); err != nil {
		return fmt.Errorf("insert activation audit log: %w", err)
	}

	return nil
}

// buildActivationURL constructs and validates the account-activation URL
// without manually concatenating unescaped bearer material.
func buildActivationURL(baseURL string, token string) (string, error) {
	baseURL = strings.TrimSpace(baseURL)
	token = strings.TrimSpace(token)

	if baseURL == "" {
		return "", errors.New("activation base URL is required")
	}
	if token == "" {
		return "", errors.New("activation token is required")
	}

	parsed, err := url.Parse(baseURL)
	if err != nil {
		return "", fmt.Errorf("parse activation base URL: %w", err)
	}
	if !parsed.IsAbs() || parsed.Hostname() == "" {
		return "", errors.New("activation base URL must be absolute")
	}

	parsed.Path = strings.TrimRight(parsed.Path, "/") + activationPath
	parsed.RawPath = ""
	parsed.Fragment = ""

	query := parsed.Query()
	query.Set("token", token)
	parsed.RawQuery = query.Encode()

	validated, err := notificationservices.ValidateActivationURL(
		parsed.String(),
	)
	if err != nil {
		return "", fmt.Errorf("validate activation URL: %w", err)
	}

	return validated, nil
}

// activationFailureCounterKey returns the bounded-attempt counter key used by
// the existing activation throttling helpers.
//
// RemoteAddr is used only as ephemeral in-memory rate-control input. It is not
// written to audit storage or application logs by this file.
func activationFailureCounterKey(r *http.Request) string {
	if r == nil {
		return "activation:unknown"
	}

	value := strings.TrimSpace(r.RemoteAddr)
	if value == "" {
		return "activation:unknown"
	}

	return "activation:" + value
}
