package webhook

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGenerateSecret(t *testing.T) {
	secret, hash, prefix, err := GenerateSecret()
	require.NoError(t, err)

	// Check secret is hex-encoded and correct length
	assert.Len(t, secret, 64) // 32 bytes * 2 (hex encoding)

	// Check hash is SHA-256 hex
	assert.Len(t, hash, 64) // SHA-256 produces 32 bytes

	// Check prefix is first 8 chars of secret
	assert.Len(t, prefix, 8)
	assert.Equal(t, secret[:8], prefix)

	// Check that hash matches the secret
	assert.Equal(t, hash, HashSecret(secret))

	// Check uniqueness
	secret2, hash2, prefix2, err := GenerateSecret()
	require.NoError(t, err)
	assert.NotEqual(t, secret, secret2)
	assert.NotEqual(t, hash, hash2)
	assert.NotEqual(t, prefix, prefix2)
}

func TestHashSecret(t *testing.T) {
	// Known test case
	secret := "test-secret"
	hash := HashSecret(secret)

	// Hash should be consistent
	assert.Equal(t, hash, HashSecret(secret))

	// Different secrets should produce different hashes
	hash2 := HashSecret("different-secret")
	assert.NotEqual(t, hash, hash2)

	// Hash should be 64 hex chars (SHA-256)
	assert.Len(t, hash, 64)
}

func TestSign(t *testing.T) {
	secret := "test-secret-12345678"
	payload := []byte(`{"event":"task.created","data":{"id":"task_123"}}`)
	timestamp := time.Unix(1704067200, 0) // 2024-01-01 00:00:00 UTC

	signature := Sign(payload, secret, timestamp)

	// Signature should start with sha256=
	assert.True(t, len(signature) > 7)
	assert.Equal(t, "sha256=", signature[:7])

	// Signature should be deterministic
	signature2 := Sign(payload, secret, timestamp)
	assert.Equal(t, signature, signature2)

	// Different payload should produce different signature
	signature3 := Sign([]byte(`{"different":"payload"}`), secret, timestamp)
	assert.NotEqual(t, signature, signature3)

	// Different secret should produce different signature
	signature4 := Sign(payload, "different-secret", timestamp)
	assert.NotEqual(t, signature, signature4)

	// Different timestamp should produce different signature
	signature5 := Sign(payload, secret, timestamp.Add(time.Second))
	assert.NotEqual(t, signature, signature5)
}

func TestVerify(t *testing.T) {
	secret := "test-secret-12345678"
	payload := []byte(`{"event":"task.created","data":{"id":"task_123"}}`)

	t.Run("valid signature within tolerance", func(t *testing.T) {
		timestamp := time.Now()
		signature := Sign(payload, secret, timestamp)

		valid := Verify(payload, signature, secret, timestamp)
		assert.True(t, valid)
	})

	t.Run("invalid signature", func(t *testing.T) {
		timestamp := time.Now()

		valid := Verify(payload, "sha256=invalid", secret, timestamp)
		assert.False(t, valid)
	})

	t.Run("wrong secret", func(t *testing.T) {
		timestamp := time.Now()
		signature := Sign(payload, secret, timestamp)

		valid := Verify(payload, signature, "wrong-secret", timestamp)
		assert.False(t, valid)
	})

	t.Run("modified payload", func(t *testing.T) {
		timestamp := time.Now()
		signature := Sign(payload, secret, timestamp)

		valid := Verify([]byte(`{"modified":"payload"}`), signature, secret, timestamp)
		assert.False(t, valid)
	})

	t.Run("timestamp too old", func(t *testing.T) {
		oldTimestamp := time.Now().Add(-10 * time.Minute)
		signature := Sign(payload, secret, oldTimestamp)

		valid := Verify(payload, signature, secret, oldTimestamp)
		assert.False(t, valid)
	})

	t.Run("timestamp in future", func(t *testing.T) {
		futureTimestamp := time.Now().Add(10 * time.Minute)
		signature := Sign(payload, secret, futureTimestamp)

		valid := Verify(payload, signature, secret, futureTimestamp)
		assert.False(t, valid)
	})

	t.Run("timestamp at edge of tolerance", func(t *testing.T) {
		// Just within tolerance
		timestamp := time.Now().Add(-4 * time.Minute)
		signature := Sign(payload, secret, timestamp)

		valid := Verify(payload, signature, secret, timestamp)
		assert.True(t, valid)
	})
}

func TestParseTimestamp(t *testing.T) {
	t.Run("valid timestamp", func(t *testing.T) {
		ts, err := ParseTimestamp("1704067200")
		require.NoError(t, err)
		assert.Equal(t, int64(1704067200), ts.Unix())
	})

	t.Run("invalid timestamp", func(t *testing.T) {
		_, err := ParseTimestamp("not-a-number")
		assert.Error(t, err)
	})

	t.Run("empty timestamp", func(t *testing.T) {
		_, err := ParseTimestamp("")
		assert.Error(t, err)
	})
}
