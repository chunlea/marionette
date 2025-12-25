package crypto

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGenerateToken(t *testing.T) {
	t.Run("generates valid token with prefix", func(t *testing.T) {
		token, displayPrefix, hash, version, err := GenerateToken("test_")
		require.NoError(t, err)

		assert.True(t, len(token) > len("test_"), "token should have content after prefix")
		assert.True(t, len(token) >= 45, "token should be at least 45 chars (prefix + base64)")
		assert.Equal(t, "test_", token[:5], "token should start with prefix")

		assert.True(t, len(displayPrefix) <= len("test_")+displayPrefixLen+1)
		assert.True(t, len(displayPrefix) >= len("test_"), "display prefix should have prefix")

		assert.Len(t, hash, 64, "SHA-256 hash should be 64 hex chars")
		assert.Equal(t, HashV1SHA256, version, "should use current hash version")
	})

	t.Run("generates unique tokens", func(t *testing.T) {
		seen := make(map[string]bool)
		for i := 0; i < 1000; i++ {
			token, _, _, _, err := GenerateToken("mk_")
			require.NoError(t, err)
			assert.False(t, seen[token], "token should be unique")
			seen[token] = true
		}
	})

	t.Run("generates unique hashes", func(t *testing.T) {
		seen := make(map[string]bool)
		for i := 0; i < 1000; i++ {
			_, _, hash, _, err := GenerateToken("mk_")
			require.NoError(t, err)
			assert.False(t, seen[hash], "hash should be unique")
			seen[hash] = true
		}
	})
}

func TestGenerateAPIKey(t *testing.T) {
	token, displayPrefix, hash, version, err := GenerateAPIKey()
	require.NoError(t, err)

	assert.True(t, ValidateTokenFormat(token, PrefixAPIKey))
	assert.Equal(t, PrefixAPIKey, ExtractPrefix(token))
	assert.Equal(t, PrefixAPIKey, displayPrefix[:len(PrefixAPIKey)])
	assert.Len(t, hash, 64)
	assert.Equal(t, HashV1SHA256, version)
}

func TestGenerateRunnerToken(t *testing.T) {
	token, displayPrefix, hash, version, err := GenerateRunnerToken()
	require.NoError(t, err)

	assert.True(t, ValidateTokenFormat(token, PrefixRunnerToken))
	assert.Equal(t, PrefixRunnerToken, ExtractPrefix(token))
	assert.Equal(t, PrefixRunnerToken, displayPrefix[:len(PrefixRunnerToken)])
	assert.Len(t, hash, 64)
	assert.Equal(t, HashV1SHA256, version)
}

func TestGenerateTunnelToken(t *testing.T) {
	token, displayPrefix, hash, version, err := GenerateTunnelToken()
	require.NoError(t, err)

	assert.True(t, ValidateTokenFormat(token, PrefixTunnelToken))
	assert.Equal(t, PrefixTunnelToken, ExtractPrefix(token))
	assert.Equal(t, PrefixTunnelToken, displayPrefix[:len(PrefixTunnelToken)])
	assert.Len(t, hash, 64)
	assert.Equal(t, HashV1SHA256, version)
}

func TestVerifyToken(t *testing.T) {
	t.Run("verifies correct token", func(t *testing.T) {
		token, _, hash, version, err := GenerateToken("mk_")
		require.NoError(t, err)

		assert.True(t, VerifyToken(token, hash, version, nil))
	})

	t.Run("rejects wrong token", func(t *testing.T) {
		_, _, hash, version, err := GenerateToken("mk_")
		require.NoError(t, err)

		wrongToken := "mk_wrongtokenwrongtoken"
		assert.False(t, VerifyToken(wrongToken, hash, version, nil))
	})

	t.Run("rejects modified token", func(t *testing.T) {
		token, _, hash, version, err := GenerateToken("mk_")
		require.NoError(t, err)

		// Modify one character
		modifiedToken := token[:len(token)-1] + "X"
		assert.False(t, VerifyToken(modifiedToken, hash, version, nil))
	})

	t.Run("rejects wrong hash", func(t *testing.T) {
		token, _, _, version, err := GenerateToken("mk_")
		require.NoError(t, err)

		wrongHash := "0000000000000000000000000000000000000000000000000000000000000000"
		assert.False(t, VerifyToken(token, wrongHash, version, nil))
	})

	t.Run("rejects unknown version", func(t *testing.T) {
		token, _, hash, _, err := GenerateToken("mk_")
		require.NoError(t, err)

		assert.False(t, VerifyToken(token, hash, 999, nil))
	})

	t.Run("HMAC version requires key", func(t *testing.T) {
		token, _, hash, _, err := GenerateToken("mk_")
		require.NoError(t, err)

		// HMAC version without key should fail
		assert.False(t, VerifyToken(token, hash, HashV2HMACSHA256, nil))
	})

	t.Run("HMAC version with key", func(t *testing.T) {
		token, _, _, _, err := GenerateToken("mk_")
		require.NoError(t, err)

		hmacKey := []byte("test-hmac-key-32-bytes-long!!!!!")

		// Compute HMAC hash manually
		mac := hmac.New(sha256.New, hmacKey)
		mac.Write([]byte(token))
		hmacHash := hex.EncodeToString(mac.Sum(nil))

		assert.True(t, VerifyToken(token, hmacHash, HashV2HMACSHA256, hmacKey))
		assert.False(t, VerifyToken(token, hmacHash, HashV2HMACSHA256, []byte("wrong-key")))
	})
}

func TestExtractPrefix(t *testing.T) {
	tests := []struct {
		token    string
		expected string
	}{
		{"mk_abc123", "mk_"},
		{"rtok_xyz789", "rtok_"},
		{"ttok_test", "ttok_"},
		{"no_prefix_here_", "no_"},
		{"nounderscoreatall", ""},
		{"_leadingunderscore", "_"},
		{"", ""},
	}

	for _, tt := range tests {
		t.Run(tt.token, func(t *testing.T) {
			assert.Equal(t, tt.expected, ExtractPrefix(tt.token))
		})
	}
}

func TestValidateTokenFormat(t *testing.T) {
	tests := []struct {
		name     string
		token    string
		prefix   string
		expected bool
	}{
		{
			name:     "valid API key",
			token:    "mk_" + "abcdefghijklmnopqrstuvwxyz0123456789ABCDEF",
			prefix:   PrefixAPIKey,
			expected: true,
		},
		{
			name:     "valid runner token",
			token:    "rtok_" + "abcdefghijklmnopqrstuvwxyz0123456789ABCDE",
			prefix:   PrefixRunnerToken,
			expected: true,
		},
		{
			name:     "wrong prefix",
			token:    "rtok_abcdefghijklmnopqrstuvwxyz0123456789ABC",
			prefix:   PrefixAPIKey,
			expected: false,
		},
		{
			name:     "too short",
			token:    "mk_abc",
			prefix:   PrefixAPIKey,
			expected: false,
		},
		{
			name:     "empty token",
			token:    "",
			prefix:   PrefixAPIKey,
			expected: false,
		},
		{
			name:     "prefix only",
			token:    "mk_",
			prefix:   PrefixAPIKey,
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, ValidateTokenFormat(tt.token, tt.prefix))
		})
	}
}

func TestHashToken(t *testing.T) {
	t.Run("produces consistent hash", func(t *testing.T) {
		token := "mk_testtoken123"
		hash1 := HashToken(token)
		hash2 := HashToken(token)

		assert.Equal(t, hash1, hash2, "same token should produce same hash")
		assert.Len(t, hash1, 64, "hash should be 64 hex chars")
	})

	t.Run("different tokens produce different hashes", func(t *testing.T) {
		hash1 := HashToken("mk_token1")
		hash2 := HashToken("mk_token2")

		assert.NotEqual(t, hash1, hash2)
	})

	t.Run("matches GenerateToken hash", func(t *testing.T) {
		token, _, expectedHash, _, err := GenerateToken("mk_")
		require.NoError(t, err)

		actualHash := HashToken(token)
		assert.Equal(t, expectedHash, actualHash)
	})
}

// BenchmarkGenerateToken measures token generation performance.
func BenchmarkGenerateToken(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_, _, _, _, _ = GenerateToken("mk_")
	}
}

// BenchmarkVerifyToken measures token verification performance.
func BenchmarkVerifyToken(b *testing.B) {
	token, _, hash, version, _ := GenerateToken("mk_")
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		VerifyToken(token, hash, version, nil)
	}
}

// BenchmarkHashToken measures hashing performance.
func BenchmarkHashToken(b *testing.B) {
	token := "mk_dGhpcyBpcyBhIHRlc3QgdG9rZW4gZm9yIGRlbW8"
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		HashToken(token)
	}
}
