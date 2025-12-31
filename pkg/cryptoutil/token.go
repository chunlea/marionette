// Package cryptoutil provides cryptographic utilities for Marionette.
//
// This includes token generation with SHA-256 hashing for authentication,
// and envelope encryption (KEK/DEK) for protecting sensitive data at rest.
package cryptoutil

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"strings"
)

// Token prefixes identify the type of token.
const (
	PrefixAPIKey      = "mk_"
	PrefixRunnerToken = "rtok_"
	PrefixTunnelToken = "ttok_"
)

// Hash version constants for algorithm migration support.
const (
	HashV1SHA256     = 1 // Current: SHA-256
	HashV2HMACSHA256 = 2 // Reserved: HMAC-SHA256
)

// CurrentHashVersion is the version used for new tokens.
var CurrentHashVersion = HashV1SHA256

const (
	// tokenRandomBytes is the number of random bytes in a token (256 bits).
	tokenRandomBytes = 32

	// displayPrefixLen is the number of characters shown after the prefix for display.
	displayPrefixLen = 8
)

// GenerateToken creates a new cryptographically secure token with the given prefix.
//
// Returns:
//   - token: The full token string (e.g., "mk_dGhpcyBpcyBhIHRlc3Q...")
//   - displayPrefix: A safe prefix for logging (e.g., "mk_dGhpcyBp")
//   - hash: SHA-256 hash of the token as hex (64 chars)
//   - version: The hash algorithm version used
//   - err: Any error that occurred
func GenerateToken(prefix string) (token, displayPrefix, hash string, version int, err error) {
	// Generate 32 bytes of cryptographically secure random data
	raw := make([]byte, tokenRandomBytes)
	if _, err := rand.Read(raw); err != nil {
		return "", "", "", 0, err
	}

	// Encode as base64url (no padding) for URL-safe tokens
	encoded := base64.RawURLEncoding.EncodeToString(raw)

	// Build the full token
	token = prefix + encoded

	// Create display prefix for logging (shows first 8 chars after prefix)
	if len(encoded) >= displayPrefixLen {
		displayPrefix = prefix + encoded[:displayPrefixLen]
	} else {
		displayPrefix = token
	}

	// Hash the token with SHA-256
	hash = sha256Hex(token)

	return token, displayPrefix, hash, CurrentHashVersion, nil
}

// VerifyToken verifies a token against a stored hash using constant-time comparison.
//
// Parameters:
//   - token: The plaintext token to verify
//   - storedHash: The stored hash (hex-encoded)
//   - version: The hash algorithm version (1=SHA-256, 2=HMAC-SHA256)
//   - hmacKey: The HMAC key (only used for version 2, can be nil for version 1)
//
// Returns true if the token matches the stored hash.
func VerifyToken(token, storedHash string, version int, hmacKey []byte) bool {
	var computed string

	switch version {
	case HashV1SHA256:
		computed = sha256Hex(token)
	case HashV2HMACSHA256:
		if hmacKey == nil {
			return false
		}
		mac := hmac.New(sha256.New, hmacKey)
		mac.Write([]byte(token))
		computed = hex.EncodeToString(mac.Sum(nil))
	default:
		return false
	}

	// Use constant-time comparison to prevent timing attacks
	return subtle.ConstantTimeCompare([]byte(computed), []byte(storedHash)) == 1
}

// sha256Hex computes SHA-256 hash of a string and returns it as hex.
func sha256Hex(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

// Convenience functions for generating specific token types.

// GenerateAPIKey generates a new API key with the "mk_" prefix.
func GenerateAPIKey() (token, displayPrefix, hash string, version int, err error) {
	return GenerateToken(PrefixAPIKey)
}

// GenerateRunnerToken generates a new runner token with the "rtok_" prefix.
func GenerateRunnerToken() (token, displayPrefix, hash string, version int, err error) {
	return GenerateToken(PrefixRunnerToken)
}

// GenerateTunnelToken generates a new tunnel token with the "ttok_" prefix.
func GenerateTunnelToken() (token, displayPrefix, hash string, version int, err error) {
	return GenerateToken(PrefixTunnelToken)
}

// ExtractPrefix extracts the prefix from a token (everything before and including the first "_").
// Returns empty string if no underscore is found.
func ExtractPrefix(token string) string {
	idx := strings.Index(token, "_")
	if idx == -1 {
		return ""
	}
	return token[:idx+1]
}

// ValidateTokenFormat checks if a token has the expected prefix and minimum length.
func ValidateTokenFormat(token string, expectedPrefix string) bool {
	if !strings.HasPrefix(token, expectedPrefix) {
		return false
	}

	// Token should have prefix + at least some base64 content
	// base64url of 32 bytes = 43 chars
	minLen := len(expectedPrefix) + 40 // Allow some flexibility
	return len(token) >= minLen
}

// HashToken computes the SHA-256 hash of a token using the current hash version.
// This is useful for looking up tokens by hash.
func HashToken(token string) string {
	return sha256Hex(token)
}
