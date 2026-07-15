// Package security provides shared authentication and credential-security helpers.
//
// sdworkspace/sdbackend/internal/security/security.go
//
// GTM:
//   Layer: 2.2 Identity / Auth Domain
//   Release Class: SPINE
//   Reason:
//     Token hashing is release-critical authentication infrastructure. It
//     supports protected storage of bearer-style token material, activation
//     tokens, password reset tokens, refresh-token style lookup patterns, and
//     other security boundaries required by the initial SagrentiDeals release
//     spine.
//
// SPINE Rule:
//   Keep compiling.
//   Keep production-ready.
//   Preserve SHA-256 token hashing behavior.
//   Preserve deterministic token-to-hash conversion.
//   Preserve no raw-token persistence expectation.
//   Preserve shared security helper ownership.
//   Block deployment if this file breaks build, token hashing,
//   protected token persistence, token lookup, or authentication integrity.
package security

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
)

const (
	// TokenHashAlgorithm documents the canonical protected-token hash algorithm.
	TokenHashAlgorithm = "SHA-256"

	// TokenHashHexLength is the canonical lowercase hex length of a SHA-256 hash.
	TokenHashHexLength = sha256.Size * 2
)

var (
	// ErrEmptyToken reports that a caller attempted to hash or verify an empty
	// token. Empty bearer material is invalid at security boundaries.
	ErrEmptyToken = errors.New("security: token must not be empty")

	// ErrInvalidTokenHash reports that a persisted or supplied token hash is not
	// the canonical lowercase hex-encoded SHA-256 representation.
	ErrInvalidTokenHash = errors.New("security: token hash is not canonical sha256 hex")

	// ErrTokenHashMismatch reports that a token does not match the expected hash.
	ErrTokenHashMismatch = errors.New("security: token hash mismatch")
)

// HashTokenStrict hashes a non-empty token using SHA-256.
//
// This helper is the canonical string-token hashing path for protected
// persistence. It rejects empty token material instead of allowing accidental
// persistence of the SHA-256 hash of an empty string.
func HashTokenStrict(token string) (string, error) {
	if token == "" {
		return "", ErrEmptyToken
	}

	return hashTokenBytesUnchecked([]byte(token)), nil
}

// HashTokenBytesStrict hashes non-empty raw token bytes using SHA-256.
//
// This helper is the canonical byte-token hashing path for protected
// persistence. The len(token) guard intentionally treats nil and empty byte
// slices the same because both represent zero bytes of bearer material and both
// would otherwise produce the same SHA-256 hash.
func HashTokenBytesStrict(token []byte) (string, error) {
	if len(token) == 0 {
		return "", ErrEmptyToken
	}

	return hashTokenBytesUnchecked(token), nil
}

// ValidateTokenHash verifies that hash is the canonical lowercase SHA-256 hex
// representation produced by HashTokenStrict or HashTokenBytesStrict.
//
// Canonical token hashes must be exactly TokenHashHexLength characters, decode
// as valid hexadecimal, and match Go's lowercase hex encoding. Uppercase hex is
// rejected even though it is technically decodable, because persisted token
// hashes must have one stable canonical representation.
func ValidateTokenHash(hash string) error {
	if len(hash) != TokenHashHexLength {
		return ErrInvalidTokenHash
	}

	decoded, err := hex.DecodeString(hash)
	if err != nil {
		return ErrInvalidTokenHash
	}

	if hex.EncodeToString(decoded) != hash {
		return ErrInvalidTokenHash
	}

	return nil
}

// IsCanonicalTokenHash reports whether hash is a canonical lowercase SHA-256
// token hash.
func IsCanonicalTokenHash(hash string) bool {
	return ValidateTokenHash(hash) == nil
}

// VerifyTokenHash verifies token against expectedHash using constant-time
// comparison after validating the expected hash shape.
//
// The constant-time comparison is performed on the canonical hex-encoded
// SHA-256 hash representations, not on raw bearer material. This is intentional:
// raw tokens remain controlled input-boundary values, while persisted comparison
// operates on protected derived token hashes.
func VerifyTokenHash(token, expectedHash string) error {
	actualHash, err := HashTokenStrict(token)
	if err != nil {
		return err
	}

	if err := ValidateTokenHash(expectedHash); err != nil {
		return err
	}

	if subtle.ConstantTimeCompare([]byte(actualHash), []byte(expectedHash)) != 1 {
		return ErrTokenHashMismatch
	}

	return nil
}

// hashTokenBytesUnchecked returns the canonical lowercase SHA-256 hex
// representation for already-validated non-empty token material.
//
// This helper is intentionally unexported so permissive empty-token hashing does
// not remain available as a public security API.
func hashTokenBytesUnchecked(token []byte) string {
	hash := sha256.Sum256(token)
	return hex.EncodeToString(hash[:])
}

// CheckPasswordHash verifies whether plainPassword matches passwordHash.
//
// This preserves the existing data-layer authentication call surface while
// keeping password verification owned by the security package.
func CheckPasswordHash(plainPassword, passwordHash string) bool {
	return VerifyPassword(plainPassword, passwordHash) == nil
}