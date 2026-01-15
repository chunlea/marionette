package webhook

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
	"time"
)

const (
	// SignatureHeader is the HTTP header containing the webhook signature.
	SignatureHeader = "X-Webhook-Signature"

	// TimestampHeader is the HTTP header containing the request timestamp.
	TimestampHeader = "X-Webhook-Timestamp"

	// IDHeader is the HTTP header containing the webhook event ID.
	IDHeader = "X-Webhook-ID"

	// secretLength is the length of generated secrets in bytes.
	secretLength = 32

	// signatureTimestampTolerance is the maximum age of a valid signature (5 minutes).
	signatureTimestampTolerance = 5 * time.Minute
)

// GenerateSecret generates a new random secret for webhook signing.
// Returns the secret (for display to user) and its SHA-256 hash (for storage).
func GenerateSecret() (secret, hash, prefix string, err error) {
	// Generate random bytes
	bytes := make([]byte, secretLength)
	if _, err := rand.Read(bytes); err != nil {
		return "", "", "", fmt.Errorf("failed to generate secret: %w", err)
	}

	// Encode as hex for the secret
	secret = hex.EncodeToString(bytes)

	// Hash the secret for storage
	hashBytes := sha256.Sum256([]byte(secret))
	hash = hex.EncodeToString(hashBytes[:])

	// First 8 chars as prefix for identification
	prefix = secret[:8]

	return secret, hash, prefix, nil
}

// HashSecret computes the SHA-256 hash of a secret.
func HashSecret(secret string) string {
	hashBytes := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(hashBytes[:])
}

// Sign creates an HMAC-SHA256 signature for the given payload.
// The signature includes the timestamp to prevent replay attacks.
func Sign(payload []byte, secret string, timestamp time.Time) string {
	// Create the signed payload: timestamp.payload
	ts := strconv.FormatInt(timestamp.Unix(), 10)
	signedPayload := ts + "." + string(payload)

	// Compute HMAC-SHA256
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(signedPayload))
	signature := mac.Sum(nil)

	// Return as sha256=<hex>
	return "sha256=" + hex.EncodeToString(signature)
}

// Verify checks if the signature is valid for the given payload and secret.
// It also validates that the timestamp is within the tolerance window.
func Verify(payload []byte, signature, secret string, timestamp time.Time) bool {
	// Check timestamp tolerance
	now := time.Now()
	if timestamp.Before(now.Add(-signatureTimestampTolerance)) ||
		timestamp.After(now.Add(signatureTimestampTolerance)) {
		return false
	}

	// Compute expected signature
	expected := Sign(payload, secret, timestamp)

	// Constant-time comparison to prevent timing attacks
	return hmac.Equal([]byte(signature), []byte(expected))
}

// ParseTimestamp parses a Unix timestamp string from the webhook headers.
func ParseTimestamp(ts string) (time.Time, error) {
	unix, err := strconv.ParseInt(ts, 10, 64)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid timestamp: %w", err)
	}
	return time.Unix(unix, 0), nil
}
