// Package security provides shared authentication and credential-security helpers.
//
// sdworkspace/sdbackend/internal/security/random_tokens.go
//
// GTM:
//   Layer: 2.2 Identity / Auth Domain
//   Release Class: SPINE
//   Reason:
//     Secure random token generation is release-critical authentication
//     infrastructure. It supports activation tokens, password reset tokens,
//     verification flows, one-time credential material, refresh tokens, and
//     other security boundaries required by the initial SagrentiDeals release
//     spine.
//
// SPINE Rule:
//   Keep compiling.
//   Keep production-ready.
//   Preserve crypto/rand-backed token generation.
//   Preserve URL-safe token encoding.
//   Preserve caller-controlled token length behavior with a security-owned
//   minimum entropy floor.
//   Preserve canonical token-size constants for known auth workflows.
//   Preserve error surfacing from entropy generation.
//   Block deployment if this file breaks build, random token generation,
//   activation-token creation, password-reset token creation,
//   refresh-token creation, or authentication integrity.
package security

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
)

const (
	// refreshTokenBytes is the canonical raw entropy size for refresh-token
	// bearer material. Thirty-two random bytes provide 256 bits of entropy
	// before URL-safe base64 encoding.
	refreshTokenBytes = 32

	// minRandomTokenBytes is the minimum raw entropy required for any generated
	// token in this file. Sixteen bytes provide a 128-bit floor for bearer
	// material used at authentication boundaries.
	minRandomTokenBytes = 16

	// ActivationTokenSize is the canonical encoded character length for account
	// activation tokens. Passing this value to GenerateRandomToken produces
	// 33 raw entropy bytes, encodes them to 44 URL-safe base64 characters, and
	// returns the first 43 characters.
	ActivationTokenSize = 43

	// PasswordResetTokenSize is the canonical encoded character length for
	// password-reset tokens. Passing this value to GenerateRandomToken produces
	// 33 raw entropy bytes, encodes them to 44 URL-safe base64 characters, and
	// returns the first 43 characters.
	PasswordResetTokenSize = 43
)

var (
	// ErrInvalidRandomTokenSize reports that a caller requested a token with a
	// non-positive output length. Empty bearer material is invalid at security
	// boundaries and must fail clearly rather than generating an unusable token.
	ErrInvalidRandomTokenSize = errors.New("security: random token size must be greater than zero")

	// ErrInsufficientTokenEntropy reports that a requested or configured token
	// size would produce less than the security-owned minimum raw entropy floor.
	ErrInsufficientTokenEntropy = errors.New("security: random token size provides insufficient entropy")

	// ErrRandomTokenGenerationFailed reports failure to obtain cryptographic
	// entropy from crypto/rand. Callers may wrap or map this error at service or
	// API boundaries, but the low-level security package owns the sentinel.
	ErrRandomTokenGenerationFailed = errors.New("security: random token generation failed")
)

// GenerateRandomToken generates URL-safe cryptographic bearer material with the
// exact caller-requested encoded character length.
//
// Callers should use the exported canonical size constants for known workflows.
// The returned token is plaintext bearer material. Callers must not log it and
// must persist only a protected derived form, such as a hash.
func GenerateRandomToken(size int) (string, error) {
	if size <= 0 {
		return "", fmt.Errorf("%w: got %d", ErrInvalidRandomTokenSize, size)
	}

	rawSize := rawBytesForBase64Length(size)
	if rawSize < minRandomTokenBytes {
		return "", fmt.Errorf(
			"%w: requested size produces %d entropy bytes; minimum is %d",
			ErrInsufficientTokenEntropy,
			rawSize,
			minRandomTokenBytes,
		)
	}

	raw := make([]byte, rawSize)
	if _, err := io.ReadFull(rand.Reader, raw); err != nil {
		return "", fmt.Errorf("%w: %v", ErrRandomTokenGenerationFailed, err)
	}

	token := base64.RawURLEncoding.EncodeToString(raw)
	if len(token) < size {
		return "", fmt.Errorf("%w: encoded token shorter than requested size", ErrRandomTokenGenerationFailed)
	}

	return token[:size], nil
}

// GenerateRefreshToken returns high-entropy bearer material suitable for refresh
// token issuance.
//
// The returned token is plaintext bearer material. Callers must hash it before
// persistence and must never log it.
func GenerateRefreshToken() (string, error) {
	if refreshTokenBytes < minRandomTokenBytes {
		return "", fmt.Errorf(
			"%w: refresh token uses %d entropy bytes; minimum is %d",
			ErrInsufficientTokenEntropy,
			refreshTokenBytes,
			minRandomTokenBytes,
		)
	}

	raw := make([]byte, refreshTokenBytes)
	if _, err := io.ReadFull(rand.Reader, raw); err != nil {
		return "", fmt.Errorf("%w: generate refresh token: %v", ErrRandomTokenGenerationFailed, err)
	}

	return base64.RawURLEncoding.EncodeToString(raw), nil
}

// rawBytesForBase64Length returns the minimum number of raw random bytes needed
// to produce at least encodedLength base64 characters. Each base64 character
// carries six bits, so the formula rounds encodedLength*6 bits up to whole
// bytes.
func rawBytesForBase64Length(encodedLength int) int {
	return (encodedLength*6 + 7) / 8
}