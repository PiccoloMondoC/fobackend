// Package notificationservices owns shared notification-service contracts,
// the application-facing dependency container, and validation helpers used
// by channel-specific notification packages.
//
// focodebase/fobackend/internal/notification_services/password_reset.go
//
// GTM:
//
//	Layer: 2.3 Consumer Domain
//	Release Class: SPINE
//	Reason:
//	  Password-reset message content is release-critical credential-recovery
//	  communication. This file owns the provider-neutral subject and body of
//	  the password-reset email and validates the reset link under the same
//	  explicit credential-link policy used for account activation.
//
//	  Delivery deliberately goes through the existing EmailSender
//	  SendEmailContext contract, so every EmailSender implementation
//	  (SMTP-backed and test) delivers password-reset mail without a contract
//	  change.
//
// SPINE Rule:
//
//	Keep compiling.
//	Keep production-ready.
//	Preserve strict HTTPS reset-link validation by default, with explicit
//	policy opt-in for dev/test loopback HTTP only.
//	Preserve user-info, fragment, and decoded traversal rejection by reusing
//	ValidateActivationURLWithPolicy, the canonical credential-link validator.
//	Never log the reset URL or the composed body; both carry bearer material.
//	Keep message copy non-enumerating: it must not reveal anything the public
//	reset-request response withholds.
//	Block deployment if this file breaks reset-link validation or
//	password-reset message composition.
package notificationservices

import (
	"errors"
	"fmt"
)

const (
	// FlowPasswordReset identifies password-reset delivery in notification
	// observability metadata.
	FlowPasswordReset = "password_reset"

	passwordResetEmailSubject = "Reset your Sagrenti password"
)

// ErrInvalidPasswordResetURL reports that a password-reset link failed
// credential-link validation. It wraps nothing that could disclose the URL.
var ErrInvalidPasswordResetURL = errors.New("notification: password reset URL is invalid")

// ComposePasswordResetEmail validates resetURL under policy and returns the
// subject and plain-text body of the password-reset email.
//
// The returned body contains plaintext bearer material. Callers must pass it
// directly to EmailSender.SendEmailContext and never log, trace, audit, or
// persist it.
func ComposePasswordResetEmail(
	resetURL string,
	policy ActivationURLPolicy,
) (subject string, body string, err error) {
	validatedURL, err := ValidateActivationURLWithPolicy(resetURL, policy)
	if err != nil {
		return "", "", ErrInvalidPasswordResetURL
	}

	body = fmt.Sprintf(
		"Use the link below to choose a new password for your Sagrenti account:\n\n"+
			"%s\n\n"+
			"The link works once and expires soon. Choosing a new password signs "+
			"your account out everywhere.\n\n"+
			"If you did not ask to reset your password, you can ignore this email; "+
			"your password has not changed.",
		validatedURL,
	)

	return passwordResetEmailSubject, body, nil
}
