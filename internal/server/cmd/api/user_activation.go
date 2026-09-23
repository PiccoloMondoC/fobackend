// Package main provides HTTP handlers and API-boundary helpers for user
// activation.
//
// focodebase/fobackend/internal/server/cmd/api/user_activation.go
//
// GTM:
//
//	Layer: 2.2 Identity / Auth Domain
//	Release Class: SPINE
//	Reason:
//	  User activation is release-critical identity infrastructure. This file
//	  owns the HTTP boundary for activation-credential redemption and
//	  authenticated activation-status reads, together with controlled
//	  activation-link construction, delivery, and audit helpers used by the
//	  surrounding identity workflow.
//
//	  Cross-model activation orchestration does not belong here. The canonical
//	  internal service layer owns the transaction that composes
//	  ActivationTokenModel and UserModel.
//
// SPINE Rule:
//
//	Keep compiling.
//	Keep production-ready.
//	Preserve public activation-token redemption without requiring an already
//	active authenticated session.
//	Preserve activation-token plaintext only at controlled input and
//	notification boundaries.
//	Never log, audit, trace, or persist plaintext activation tokens or
//	activation URLs.
//	Preserve token hashing, lookup, expiry validation, and consumption behind
//	the canonical internal-service/data-layer boundary.
//	Preserve canonical user activation state ownership in UserModel.
//	Do not call ActivationTokenModel transaction-scoped primitives from the
//	handler layer.
//	Do not begin, commit, or roll back cross-model transactions here.
//	Preserve activation-status reads through UserModel, never
//	ActivationTokenModel.
//	Preserve context-aware activation delivery through the canonical
//	notification_services EmailSender and SMSSender boundaries.
//	Preserve pre-seeded/preloaded audit governance through the single shared
//	audit-recording helper already used across the Identity/Auth handler surface.
//	Do not fabricate authenticated actor identity for public bearer-token flows.
//	Do not invoke one HTTP handler from another HTTP handler.
//	Do not dynamically create audit actions or entity types during requests.
//	Do not expose a general HTTP endpoint that returns newly generated
//	activation bearer credentials.
//	Block deployment if this file breaks activation redemption,
//	activation-status reads, protected token handling, notification boundaries,
//	or activation audit integrity.
package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/PiccoloMondoC/focodebase/fobackend/internal/data"
	notificationservices "github.com/PiccoloMondoC/focodebase/fobackend/internal/notification_services"

	"github.com/google/uuid"
)

const (
	activationPath = "/activate"

	activationChannelEmail = "email"
	activationChannelSMS   = "sms"

	actionActivateUser            = "activate_user"
	actionGetUserActivationStatus = "get_user_activation_status"
)

// ActivateUserHandler redeems a plaintext activation credential and activates
// the associated user account.
//
// This endpoint is intentionally public because the activation credential is
// itself the bearer proof required to complete account activation. A user who
// is not yet active cannot be required to authenticate before redeeming it.
//
// The canonical internal service owns all cross-model orchestration, including:
//
//	activation-token lock and validation
//	→ inactive-to-active UserModel transition
//	→ exact activation-token consumption
//
// The handler never hashes the credential, accesses activation-token
// persistence directly, or manages the transaction.
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

	if err := app.readJSON(w, r, &input); err != nil {
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

	ctx, cancel := context.WithTimeout(
		r.Context(),
		cfgTimeout,
	)
	defer cancel()

	if app.InternalServices == nil {
		logger.Error("internal activation service is unavailable")
		app.respondWithError(
			w,
			errors.New("activation service is unavailable"),
			http.StatusServiceUnavailable,
		)
		return
	}

	userID, err :=
		app.InternalServices.RedeemUserActivationTokenInternal(
			ctx,
			input.Token,
		)
	if err != nil {
		// Never log the plaintext token. Error classification and request
		// metadata are sufficient for operational diagnosis.
		logger.Warn(
			"user activation failed",
			"error", err,
		)

		switch {
		case errors.Is(
			err,
			data.ErrActivationTokenRequired,
		):
			app.respondWithError(
				w,
				errors.New("activation token is required"),
				http.StatusBadRequest,
			)

		case errors.Is(
			err,
			data.ErrActivationTokenInvalid,
		),
			errors.Is(
				err,
				data.ErrActivationTokenExpired,
			),
			errors.Is(
				err,
				data.ErrActivationTokenNotFound,
			),
			errors.Is(
				err,
				data.ErrUserNotFound,
			),
			errors.Is(
				err,
				data.ErrUserAlreadyActive,
			):
			// Deliberately collapse bearer-token and target-user lifecycle
			// distinctions at the public boundary.
			app.respondWithError(
				w,
				errors.New(
					"activation token is invalid or expired",
				),
				http.StatusUnauthorized,
			)

		case errors.Is(
			err,
			context.Canceled,
		),
			errors.Is(
				err,
				context.DeadlineExceeded,
			):
			app.respondWithError(
				w,
				errors.New(
					"activation request timed out",
				),
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

	// The activation transaction has already committed successfully. Audit
	// failure is therefore an observable soft failure, not grounds for lying
	// to the client that activation itself failed or returning HTTP 206.
	//
	// Activation is a public bearer-token flow, not an authenticated request.
	// Record the activated account as the entity target without fabricating the
	// target user as an authenticated audit actor.
	if auditErr := app.recordUserAudit(
		ctx,
		nil,
		actionActivateUser,
		"Activate a user account based on a valid activation token",
		userID,
	); auditErr != nil {
		logger.Warn(
			"user activated but activation audit insertion failed",
			"user_id", userID,
			"error", auditErr,
		)
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

type resendActivationInput struct {
	Email string `json:"email"`
}

// ResendActivationLinkHandler requests a replacement activation message for an
// inactive email-addressed account.
//
// This endpoint is intentionally public because an inactive account cannot
// obtain an ordinary authenticated session. The response is deliberately
// non-enumerating: account absence, active state, and activation ineligibility
// are not disclosed to the caller.
//
// The handler owns only the public HTTP/privacy boundary and controlled
// notification delivery. Activation-token issuance remains owned by the
// canonical internal activation service.
//
// Known residual risk (tracked for the services/infrastructure layer, not
// fixable here): this handler does materially more work — a database write
// plus a synchronous outbound notification-provider call — when the account
// exists and is inactive than when it does not, which creates a timing
// side-channel even though the response body is always identical. Moving
// delivery onto the durable async/outbox foundation would close this gap and
// should be revisited there. Per-caller rate limiting is also IP-scoped only
// (see RateLimitMiddleware); it does not throttle repeated resend requests
// against the same target email from different source IPs. A per-email
// issuance cooldown belongs in IssueUserActivationTokenInternal.
func (app *Application) ResendActivationLinkHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	logger := app.Logger.
		GetLoggerWithContext(r).
		WithFunctionName("ResendActivationLinkHandler")

	var input resendActivationInput
	if err := app.readJSON(w, r, &input); err != nil {
		app.respondWithError(
			w,
			errors.New("invalid request body"),
			http.StatusBadRequest,
		)
		return
	}

	email := strings.TrimSpace(input.Email)
	if email == "" {
		app.respondWithError(
			w,
			errors.New("email is required"),
			http.StatusBadRequest,
		)
		return
	}

	ctx, cancel := context.WithTimeout(
		r.Context(),
		cfgTimeout,
	)
	defer cancel()

	if app.InternalServices == nil {
		logger.Error("internal activation service is unavailable")
		app.respondWithError(
			w,
			errors.New("activation service is unavailable"),
			http.StatusServiceUnavailable,
		)
		return
	}

	// Public privacy invariant: do not disclose whether this email identifies
	// an account, whether that account is already active, or whether activation
	// token issuance is currently eligible.
	user, err := app.Models.User.GetByEmail(ctx, email)
	if err == nil &&
		user != nil &&
		user.ID != uuid.Nil &&
		!user.IsActive {

		token, issueErr :=
			app.InternalServices.IssueUserActivationTokenInternal(
				ctx,
				user.ID,
			)
		if issueErr != nil {
			logger.Warn(
				"activation resend token issuance failed",
				"user_id", user.ID,
				"error", issueErr,
			)
		} else {
			activationURL, urlErr := buildActivationURL(
				app.Config.Bootstrap.BaseURL,
				token,
				app.activationURLPolicy(),
			)
			if urlErr != nil {
				logger.Error(
					"activation resend URL construction failed",
					"user_id", user.ID,
					"error", urlErr,
				)
			} else {
				// The account was resolved by its canonical email address.
				// Resend is therefore deliberately email-addressed and does not
				// reinterpret another preferred channel from unauthenticated input.
				contact := &data.UserContactInfo{
					Email: user.Email,
				}

				if _, sendErr := app.sendActivationNotification(
					ctx,
					user.ID,
					contact,
					activationURL,
				); sendErr != nil {
					logger.Warn(
						"activation resend delivery failed",
						"user_id", user.ID,
						"error", sendErr,
					)
				}
			}
		}
	} else if err != nil &&
		!errors.Is(err, data.ErrUserNotFound) &&
		!errors.Is(err, context.Canceled) &&
		!errors.Is(err, context.DeadlineExceeded) {
		// Preserve non-enumerating response behavior while retaining operational
		// visibility for genuine datastore failures.
		logger.Warn(
			"activation resend account lookup failed",
			"error", err,
		)
	}

	if errors.Is(ctx.Err(), context.Canceled) {
		return
	}

	app.respondWithJSON(
		w,
		http.StatusAccepted,
		jsonResponse{
			Error:   false,
			Message: "If the account is eligible for activation, a new activation message will be sent",
		},
	)
}

// GetUserActivationStatusHandler returns the authenticated user's current
// canonical account activation state.
//
// Activation state is users.is_active and therefore belongs to UserModel.
// ActivationTokenModel owns activation_tokens exclusively and must never be
// used as a user-state read path.
func (app *Application) GetUserActivationStatusHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	logger := app.Logger.
		GetLoggerWithContext(r).
		WithFunctionName(
			"GetUserActivationStatusHandler",
		)

	ctx, cancel := context.WithTimeout(
		r.Context(),
		cfgTimeout,
	)
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

	user, err := app.Models.User.GetByID(
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
		case errors.Is(
			err,
			data.ErrUserNotFound,
		):
			app.respondWithError(
				w,
				errors.New("user not found"),
				http.StatusNotFound,
			)

		case errors.Is(
			err,
			context.Canceled,
		),
			errors.Is(
				err,
				context.DeadlineExceeded,
			):
			app.respondWithError(
				w,
				errors.New(
					"activation-status request timed out",
				),
				http.StatusGatewayTimeout,
			)

		default:
			app.respondWithError(
				w,
				errors.New(
					"failed to retrieve activation status",
				),
				http.StatusInternalServerError,
			)
		}

		return
	}

	if user == nil {
		logger.Warn(
			"activation status lookup returned nil user",
			"user_id", *userID,
		)
		app.respondWithError(
			w,
			errors.New("user not found"),
			http.StatusNotFound,
		)
		return
	}

	responseData := struct {
		IsActive bool `json:"is_active"`
	}{
		IsActive: user.IsActive,
	}

	// Activation status is an ordinary authenticated self-read. Keep it in
	// structured request logs rather than generating a durable audit row for
	// every poll.
	logger.Info(
		"activation status retrieved",
		"user_id", *userID,
		"is_active", user.IsActive,
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

// sendActivationNotification delivers an already-constructed activation URL
// through the user's canonical preferred contact channel.
//
// This is an internal API-layer helper, not an HTTP handler. Registration and
// any future explicitly designed activation-link resend workflow may call this
// helper without invoking one HTTP handler from another.
//
// SMS is used only when it is explicitly preferred and a phone number exists.
// Email is used when email is explicitly preferred or no preference has yet
// been stored. Missing contact data fails rather than silently selecting an
// unintended channel.
func (app *Application) sendActivationNotification(
	ctx context.Context,
	userID uuid.UUID,
	contact *data.UserContactInfo,
	activationURL string,
) (string, error) {
	if contact == nil {
		return "", errors.New(
			"activation contact information is required",
		)
	}

	preferredMethod := strings.ToLower(
		strings.TrimSpace(
			contact.PreferredMethod,
		),
	)

	switch preferredMethod {
	case activationChannelSMS:
		phone := strings.TrimSpace(
			contact.Phone,
		)
		if phone == "" {
			return activationChannelSMS, errors.New(
				"preferred SMS contact is unavailable",
			)
		}

		if app.SMSService == nil {
			return activationChannelSMS, errors.New(
				"activation SMS service is unavailable",
			)
		}

		err := app.SMSService.
			SendActivationSMSContextWithOptions(
				ctx,
				phone,
				activationURL,
				notificationservices.SendOptions{
					CorrelationID: userID.String(),
					Flow: notificationservices.
						FlowAccountActivation,
				},
			)
		if err != nil {
			return activationChannelSMS, err
		}

		return activationChannelSMS, nil

	case "", activationChannelEmail:
		email := strings.TrimSpace(
			contact.Email,
		)
		if email == "" {
			return activationChannelEmail, errors.New(
				"activation email contact is unavailable",
			)
		}

		if app.EmailService == nil {
			return activationChannelEmail, errors.New(
				"activation email service is unavailable",
			)
		}

		err := app.EmailService.
			SendActivationEmailContext(
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

// activationURLPolicy returns the explicit activation-link policy derived from
// the already-validated canonical runtime environment. The zero value remains
// production-safe; only dev/test may opt into HTTP on loopback hosts.
func (app *Application) activationURLPolicy() notificationservices.ActivationURLPolicy {
	if app == nil {
		return notificationservices.ActivationURLPolicy{}
	}

	switch strings.ToLower(strings.TrimSpace(app.Config.Bootstrap.Env)) {
	case "dev", "test":
		return notificationservices.ActivationURLPolicy{
			AllowHTTPOnLoopback: true,
		}
	default:
		return notificationservices.ActivationURLPolicy{}
	}
}

// buildActivationURL constructs and validates an account-activation URL.
//
// The token is placed in the encoded query string only for controlled delivery
// to the intended user. Callers must never log, audit, trace, or persist the
// resulting URL or the plaintext token.
func buildActivationURL(
	baseURL string,
	token string,
	policy notificationservices.ActivationURLPolicy,
) (string, error) {
	baseURL = strings.TrimSpace(baseURL)
	token = strings.TrimSpace(token)

	if baseURL == "" {
		return "", errors.New(
			"activation base URL is required",
		)
	}

	if token == "" {
		return "", errors.New(
			"activation token is required",
		)
	}

	parsed, err := url.Parse(baseURL)
	if err != nil {
		return "", fmt.Errorf(
			"parse activation base URL: %w",
			err,
		)
	}

	if !parsed.IsAbs() ||
		parsed.Hostname() == "" {
		return "", errors.New(
			"activation base URL must be absolute",
		)
	}

	parsed.Path =
		strings.TrimRight(
			parsed.Path,
			"/",
		) + activationPath
	parsed.RawPath = ""
	parsed.Fragment = ""

	query := parsed.Query()
	query.Set(
		"token",
		token,
	)
	parsed.RawQuery = query.Encode()

	validated, err :=
		notificationservices.ValidateActivationURLWithPolicy(
			parsed.String(),
			policy,
		)
	if err != nil {
		return "", fmt.Errorf(
			"validate activation URL: %w",
			err,
		)
	}

	return validated, nil
}
