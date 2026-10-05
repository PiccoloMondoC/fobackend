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
//	  User activation is release-critical identity infrastructure. Users meet
//	  it as "email confirmation"; the domain keeps the activation name. This
//	  file owns the HTTP boundary for link-token redemption, manual
//	  confirmation-code redemption, non-enumerating resend, and authenticated
//	  activation-status reads, together with controlled confirmation-link
//	  construction (from the authoritative FrontendURL), delivery, and audit
//	  helpers used by the surrounding identity workflow.
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
//	Build every emailed browser link from Config.Bootstrap.FrontendURL, never
//	from the API BaseURL.
//	Collapse every manual-code failure (unknown account, already verified,
//	wrong, expired, attempts exhausted) into one public outcome.
//	Absorb resend cooldown silently; resend responses never vary.
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
	"github.com/PiccoloMondoC/focodebase/fobackend/internal/services"

	"github.com/google/uuid"
)

const (
	// confirmEmailPath is the Angular route that redeems an emailed
	// confirmation link (?token=) and accepts the manual code. The legacy
	// /activate route redirects there in the browser, so links sent before
	// this change keep working.
	confirmEmailPath = "/confirm-email"

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
			Message: "Email verified",
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
// Timing: email delivery runs detached from the request (see
// notification_dispatch.go), so the outbound provider call no longer makes
// existing inactive accounts measurably slower. A small residual difference
// remains from the credential-issuance database write, which must commit
// before the response so replacement invalidation is never lost. A durable
// outbox would remove even that and remains the long-term home. Per-caller rate limiting is also IP-scoped only
// (see RateLimitMiddleware); it does not throttle repeated resend requests
// against the same target email from different source IPs. A per-account
// issuance cooldown (ACTIVATION_RESEND_COOLDOWN) is enforced by
// IssueUserEmailConfirmationInternal and absorbed silently here, so repeated
// resends neither flood the inbox nor reset code-attempt budgets.
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

		credentials, issueErr :=
			app.InternalServices.IssueUserEmailConfirmationInternal(
				ctx,
				user.ID,
				services.IssueEmailConfirmationOptions{
					EnforceResendCooldown: true,
				},
			)
		switch {
		case errors.Is(issueErr, services.ErrEmailConfirmationResendCooldown):
			// Silently absorbed: the previous message is still the newest.
		case issueErr != nil:
			logger.Warn(
				"confirmation resend credential issuance failed",
				"user_id", user.ID,
				"error", issueErr,
			)
		default:
			// The account was resolved by its canonical email address.
			// Resend is therefore deliberately email-addressed and does not
			// reinterpret another preferred channel from unauthenticated input.
			//
			// The replacement credential is already committed (and the
			// previous one invalidated). Delivery runs off the request path
			// so this branch costs about the same time as the no-account
			// branch; see notification_dispatch.go.
			contact := &data.UserContactInfo{
				Email: user.Email,
			}
			userID := user.ID

			app.dispatchCredentialNotification(
				ctx,
				logger,
				notificationservices.FlowAccountActivation,
				func(deliveryCtx context.Context) error {
					_, sendErr := app.sendActivationNotification(
						deliveryCtx,
						userID,
						contact,
						credentials,
					)
					return sendErr
				},
			)
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
			Message: "If the account is waiting for email confirmation, a new confirmation email will be sent",
		},
	)
}

type confirmEmailCodeInput struct {
	Email string `json:"email"`
	Code  string `json:"code"`
}

// confirmationCodeRejectedMessage is the single public outcome for every
// manual-code failure. It must not vary by account existence or state.
const confirmationCodeRejectedMessage = "confirmation code is invalid or expired"

// ConfirmEmailCodeHandler confirms a pending account's email address with the
// manual six-digit code from the confirmation email.
//
// The endpoint is public because an unconfirmed account cannot sign in. It is
// rate-limited by RateLimitMiddleware and, authoritatively, by the per-code
// and per-record failure limits enforced in the service/data layers.
//
// Public privacy invariant: unknown email, already-verified account, wrong
// code, expired code, and exhausted attempts all produce the same 401 body.
// Only request-shape problems (no email, a code that is not six digits) are
// reported as 400, because they reveal nothing about any account.
func (app *Application) ConfirmEmailCodeHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	logger := app.Logger.
		GetLoggerWithContext(r).
		WithFunctionName("ConfirmEmailCodeHandler")

	var input confirmEmailCodeInput
	if err := app.readJSON(w, r, &input); err != nil {
		app.respondWithError(
			w,
			errors.New("invalid request body"),
			http.StatusBadRequest,
		)
		return
	}

	email := strings.TrimSpace(input.Email)
	code, codeOK := services.NormalizeActivationCode(input.Code)

	if email == "" || !codeOK {
		app.respondWithError(
			w,
			errors.New("email and a 6-digit confirmation code are required"),
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
			errors.New("confirmation service is unavailable"),
			http.StatusServiceUnavailable,
		)
		return
	}

	userID, err :=
		app.InternalServices.RedeemUserEmailConfirmationCodeInternal(
			ctx,
			email,
			code,
		)
	if err != nil {
		// Never log the code or the email address.
		switch {
		case errors.Is(err, data.ErrUserNotFound),
			errors.Is(err, data.ErrUserAlreadyActive),
			errors.Is(err, data.ErrActivationCodeRequired),
			errors.Is(err, data.ErrActivationCodeInvalid),
			errors.Is(err, data.ErrActivationCodeExpired),
			errors.Is(err, data.ErrActivationCodeAttemptsExceeded),
			errors.Is(err, data.ErrActivationTokenNotFound):
			logger.Warn(
				"email confirmation by code rejected",
				"error", err,
			)
			app.respondWithError(
				w,
				errors.New(confirmationCodeRejectedMessage),
				http.StatusUnauthorized,
			)

		case errors.Is(err, context.Canceled),
			errors.Is(err, context.DeadlineExceeded):
			app.respondWithError(
				w,
				errors.New("confirmation request timed out"),
				http.StatusGatewayTimeout,
			)

		default:
			logger.Error(
				"email confirmation by code failed",
				"error", err,
			)
			app.respondWithError(
				w,
				errors.New("confirmation failed"),
				http.StatusInternalServerError,
			)
		}

		return
	}

	if auditErr := app.recordUserAudit(
		ctx,
		nil,
		actionActivateUser,
		"Confirm a user email address with a confirmation code",
		userID,
	); auditErr != nil {
		logger.Warn(
			"email verified but audit insertion failed",
			"user_id", userID,
			"error", auditErr,
		)
	}

	logger.Info(
		"email verified by confirmation code",
		"user_id", userID,
	)

	app.respondWithJSON(
		w,
		http.StatusOK,
		jsonResponse{
			Error:   false,
			Message: "Email verified",
			Data: struct {
				UserID uuid.UUID `json:"user_id"`
			}{
				UserID: userID,
			},
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

// sendActivationNotification builds the browser confirmation links for an
// issued credential pair and delivers them through the user's canonical
// preferred contact channel.
//
// Email receives the full confirmation message (button link plus the manual
// six-digit code). SMS behavior is unchanged and out of scope for this
// capability: it receives the link only.
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
	credentials data.ActivationCredentials,
) (string, error) {
	if contact == nil {
		return "", errors.New(
			"activation contact information is required",
		)
	}

	content, err := app.emailConfirmationContent(credentials)
	if err != nil {
		return "", fmt.Errorf(
			"build confirmation links: %w",
			err,
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
				content.ConfirmURL,
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
			SendEmailConfirmationContext(
				ctx,
				email,
				content,
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

// frontendBaseURL returns the authoritative browser-facing origin used for
// every emailed link. It is configuration (FRONTEND_URL), never the API
// BaseURL and never a hard-coded development origin.
func (app *Application) frontendBaseURL() string {
	if app == nil {
		return ""
	}
	return app.Config.Bootstrap.FrontendURL
}

// emailConfirmationContent builds the browser confirmation link (carrying the
// link token, for the HTML button href only) and the bearer-free
// confirmation page URL for an issued credential pair, plus the distinct
// link and code expiry inputs. The activation-link TTL is passed through
// unchanged from ActivationTokenTTL.
//
// The result carries plaintext credentials; never log, audit, or persist it.
func (app *Application) emailConfirmationContent(
	credentials data.ActivationCredentials,
) (notificationservices.EmailConfirmationContent, error) {
	var none notificationservices.EmailConfirmationContent

	confirmURL, err := buildActivationURL(
		app.frontendBaseURL(),
		credentials.LinkToken,
		app.credentialLinkURLPolicy(),
	)
	if err != nil {
		return none, err
	}

	content := notificationservices.EmailConfirmationContent{
		ConfirmURL: confirmURL,
	}

	if app.InternalServices != nil && app.InternalServices.Cfg != nil {
		content.LinkValidFor = app.InternalServices.Cfg.ActivationTokenTTL
		content.CodeValidFor = app.InternalServices.Cfg.EffectiveActivationCodeTTL()
	}

	// The six-digit code is mandatory: the plain-text alternative is
	// link-free, so the code is the text reader's way to confirm.
	if credentials.Code == "" {
		return none, errors.New("confirmation code is required")
	}

	codeEntryURL, err := buildFrontendPageURL(
		app.frontendBaseURL(),
		confirmEmailPath,
		app.credentialLinkURLPolicy(),
	)
	if err != nil {
		return none, err
	}

	content.CodeEntryURL = codeEntryURL
	content.Code = credentials.Code

	return content, nil
}

// buildFrontendPageURL constructs and validates a bearer-free browser page
// URL: baseURL + path with no query string.
func buildFrontendPageURL(
	baseURL string,
	path string,
	policy notificationservices.ActivationURLPolicy,
) (string, error) {
	baseURL = strings.TrimSpace(baseURL)
	if baseURL == "" {
		return "", errors.New("frontend base URL is required")
	}

	if !strings.HasPrefix(path, "/") {
		return "", errors.New("frontend page path must be absolute")
	}

	parsed, err := url.Parse(baseURL)
	if err != nil {
		return "", fmt.Errorf("parse frontend base URL: %w", err)
	}

	if !parsed.IsAbs() || parsed.Hostname() == "" {
		return "", errors.New("frontend base URL must be absolute")
	}

	parsed.Path = strings.TrimRight(parsed.Path, "/") + path
	parsed.RawPath = ""
	parsed.RawQuery = ""
	parsed.Fragment = ""

	return notificationservices.ValidateActivationURLWithPolicy(
		parsed.String(),
		policy,
	)
}

// credentialLinkURLPolicy returns the explicit link policy for emailed bearer
// credential URLs (account activation and password reset), derived from the
// already-validated canonical runtime environment.
//
// The zero value remains production-safe; only dev/test may opt into HTTP on
// loopback hosts. This mirrors the policy derivation in
// notification_services/runtime so link construction here and link
// validation inside the email sender always agree.
func (app *Application) credentialLinkURLPolicy() notificationservices.ActivationURLPolicy {
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

// buildActivationURL constructs and validates the browser email-confirmation
// link (FrontendURL + /confirm-email?token=...).
//
// The token is placed in the encoded query string only for controlled delivery
// to the intended user. Callers must never log, audit, trace, or persist the
// resulting URL or the plaintext token.
func buildActivationURL(
	baseURL string,
	token string,
	policy notificationservices.ActivationURLPolicy,
) (string, error) {
	return buildCredentialLinkURL(
		baseURL,
		confirmEmailPath,
		token,
		policy,
	)
}

// buildCredentialLinkURL constructs and validates an emailed bearer-credential
// link: baseURL + path with the credential in the "token" query parameter.
//
// It is shared by account activation and password reset so both links obey
// one construction and validation contract. The result carries plaintext
// bearer material; callers must never log, audit, trace, or persist it.
//
// Validation reuses notificationservices.ValidateActivationURLWithPolicy,
// the canonical credential-link validator. Its name predates password reset;
// the rules it enforces (absolute URL, HTTPS outside loopback dev/test, no
// user-info) apply identically to both link types.
func buildCredentialLinkURL(
	baseURL string,
	path string,
	token string,
	policy notificationservices.ActivationURLPolicy,
) (string, error) {
	baseURL = strings.TrimSpace(baseURL)
	token = strings.TrimSpace(token)

	if baseURL == "" {
		return "", errors.New(
			"credential link base URL is required",
		)
	}

	if !strings.HasPrefix(path, "/") {
		return "", errors.New(
			"credential link path must be absolute",
		)
	}

	if token == "" {
		return "", errors.New(
			"credential link token is required",
		)
	}

	parsed, err := url.Parse(baseURL)
	if err != nil {
		return "", fmt.Errorf(
			"parse credential link base URL: %w",
			err,
		)
	}

	if !parsed.IsAbs() ||
		parsed.Hostname() == "" {
		return "", errors.New(
			"credential link base URL must be absolute",
		)
	}

	parsed.Path =
		strings.TrimRight(
			parsed.Path,
			"/",
		) + path
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
			"validate credential link URL: %w",
			err,
		)
	}

	return validated, nil
}
