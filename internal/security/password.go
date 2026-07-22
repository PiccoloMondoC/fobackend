// Package security provides shared authentication and credential-security helpers.
//
// sdworkspace/sdbackend/internal/security/password.go
//
// GTM:
//
//	Layer: 2.2 Identity / Auth Domain
//	Release Class: SPINE
//	Reason:
//	  Password hashing and password verification are release-critical
//	  authentication infrastructure. They protect plaintext credential handling,
//	  bcrypt password persistence, login verification, password updates, and
//	  password-reset flows required by the initial Platform release spine.
//
// SPINE Rule:
//
//	Keep compiling.
//	Keep production-ready.
//	Preserve bcrypt-backed password hashing.
//	Preserve plaintext-password boundary handling only at controlled inputs.
//	Preserve password hash verification behavior.
//	Preserve no raw password persistence or logging.
//	Block deployment if this file breaks build, password hashing,
//	credential verification, password updates, or authentication integrity.
package security

import (
	"errors"
	"fmt"
	"unicode/utf8"

	"golang.org/x/crypto/bcrypt"
)

const (
	minPasswordRunes = 8
	maxPasswordBytes = 72

	passwordHashCost = bcrypt.DefaultCost
)

var (
	// ErrEmptyPassword reports that plaintext password input is empty at a
	// controlled credential boundary.
	ErrEmptyPassword = errors.New("security: password is empty")

	// ErrPasswordLength reports that plaintext password input violates the
	// supported bcrypt password length bounds.
	ErrPasswordLength = errors.New("security: password length must be between 8 and 72 characters")

	// ErrIncorrectPassword reports that plaintext password verification failed
	// because the supplied password does not match the persisted bcrypt hash.
	ErrIncorrectPassword = errors.New("security: incorrect password")

	// ErrPasswordHashRequired reports that password verification was attempted
	// without a persisted bcrypt hash.
	ErrPasswordHashRequired = errors.New("security: password hash is required")

	// ErrPasswordHashInvalid reports that the persisted bcrypt hash is malformed,
	// corrupt, unsupported, or otherwise unusable for password verification.
	ErrPasswordHashInvalid = errors.New("security: password hash is invalid")
)

// ValidatePassword validates plaintext password input at the controlled
// credential boundary.
//
// This function intentionally does not trim, lowercase, normalize, log, or
// otherwise transform the password. Passwords are credential material, and
// changing them would corrupt the user's intended secret.
func ValidatePassword(password string) error {
	if password == "" {
		return ErrEmptyPassword
	}

	if utf8.RuneCountInString(password) < minPasswordRunes || len(password) > maxPasswordBytes {
		return ErrPasswordLength
	}

	return nil
}

// HashPassword validates and hashes a plaintext password using bcrypt.
//
// The returned value is the only form that may be persisted. Callers must never
// store, log, audit, trace, or return the plaintext password.
func HashPassword(password string) (string, error) {
	if err := ValidatePassword(password); err != nil {
		return "", err
	}

	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(password), passwordHashCost)
	if err != nil {
		return "", fmt.Errorf("security: hash password: %w", err)
	}

	return string(hashedPassword), nil
}

// VerifyPassword compares plaintext password input against a persisted bcrypt
// password hash.
//
// Public authentication responses should still normalize any returned error to
// a generic invalid-credentials response. This lower-level helper preserves
// diagnostic distinction for logs and traces without exposing credential values.
func VerifyPassword(password, hash string) error {
	if password == "" {
		return ErrEmptyPassword
	}

	if hash == "" {
		return ErrPasswordHashRequired
	}

	if err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)); err != nil {
		if errors.Is(err, bcrypt.ErrMismatchedHashAndPassword) {
			return ErrIncorrectPassword
		}

		return fmt.Errorf("%w: %v", ErrPasswordHashInvalid, err)
	}

	return nil
}