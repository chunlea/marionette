package cryptoutil

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockDEKStore implements DEKStore for testing.
type mockDEKStore struct {
	keys map[string]*DataKey // keyed by "resourceType:resourceID"
	mu   sync.RWMutex
}

func newMockDEKStore() *mockDEKStore {
	return &mockDEKStore{
		keys: make(map[string]*DataKey),
	}
}

func (m *mockDEKStore) GetDEK(ctx context.Context, resourceType, resourceID string) (*DataKey, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	key := resourceType + ":" + resourceID
	if dk, ok := m.keys[key]; ok {
		return dk, nil
	}
	return nil, ErrDEKNotFound
}

func (m *mockDEKStore) CreateDEK(ctx context.Context, dk *DataKey) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	key := dk.ResourceType + ":" + dk.ResourceID
	m.keys[key] = dk
	return nil
}

func (m *mockDEKStore) UpdateDEK(ctx context.Context, dk *DataKey) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	key := dk.ResourceType + ":" + dk.ResourceID
	m.keys[key] = dk
	return nil
}

func TestNewService(t *testing.T) {
	t.Run("valid KEK", func(t *testing.T) {
		kek, err := GenerateKEK()
		require.NoError(t, err)

		svc, err := NewService(kek, nil, nil)
		require.NoError(t, err)
		assert.NotNil(t, svc)
	})

	t.Run("invalid hex", func(t *testing.T) {
		_, err := NewService("not-hex", nil, nil)
		assert.ErrorIs(t, err, ErrInvalidKEK)
	})

	t.Run("wrong length", func(t *testing.T) {
		_, err := NewService("0123456789abcdef", nil, nil) // Only 8 bytes
		assert.ErrorIs(t, err, ErrInvalidKEK)
	})

	t.Run("empty KEK", func(t *testing.T) {
		_, err := NewService("", nil, nil)
		assert.ErrorIs(t, err, ErrInvalidKEK)
	})
}

func TestEncryptDecrypt(t *testing.T) {
	kek, err := GenerateKEK()
	require.NoError(t, err)

	store := newMockDEKStore()
	idGen := func() string { return "dek_test123" }
	svc, err := NewService(kek, store, idGen)
	require.NoError(t, err)

	ctx := context.Background()

	t.Run("encrypt and decrypt roundtrip", func(t *testing.T) {
		plaintext := []byte("hello, world!")

		ciphertext, err := svc.Encrypt(ctx, "tenant", "t1", plaintext)
		require.NoError(t, err)
		assert.NotEqual(t, plaintext, ciphertext)
		assert.Greater(t, len(ciphertext), len(plaintext)) // nonce + tag overhead

		decrypted, err := svc.Decrypt(ctx, "tenant", "t1", ciphertext)
		require.NoError(t, err)
		assert.Equal(t, plaintext, decrypted)
	})

	t.Run("same plaintext produces different ciphertext", func(t *testing.T) {
		plaintext := []byte("test data")

		ct1, err := svc.Encrypt(ctx, "tenant", "t1", plaintext)
		require.NoError(t, err)

		ct2, err := svc.Encrypt(ctx, "tenant", "t1", plaintext)
		require.NoError(t, err)

		assert.NotEqual(t, ct1, ct2, "same plaintext should produce different ciphertext due to random nonce")
	})

	t.Run("different resources get different DEKs", func(t *testing.T) {
		plaintext := []byte("secret")

		// Encrypt for resource 1
		ct1, err := svc.Encrypt(ctx, "tenant", "r1", plaintext)
		require.NoError(t, err)

		// Encrypt for resource 2
		ct2, err := svc.Encrypt(ctx, "tenant", "r2", plaintext)
		require.NoError(t, err)

		// Verify they have different DEKs in store
		dk1, _ := store.GetDEK(ctx, "tenant", "r1")
		dk2, _ := store.GetDEK(ctx, "tenant", "r2")
		assert.NotEqual(t, dk1.DEKEncrypted, dk2.DEKEncrypted)

		// But both decrypt correctly
		dec1, err := svc.Decrypt(ctx, "tenant", "r1", ct1)
		require.NoError(t, err)
		assert.Equal(t, plaintext, dec1)

		dec2, err := svc.Decrypt(ctx, "tenant", "r2", ct2)
		require.NoError(t, err)
		assert.Equal(t, plaintext, dec2)
	})

	t.Run("tampering detection", func(t *testing.T) {
		plaintext := []byte("important data")

		ciphertext, err := svc.Encrypt(ctx, "tenant", "tamper", plaintext)
		require.NoError(t, err)

		// Tamper with ciphertext
		tampered := make([]byte, len(ciphertext))
		copy(tampered, ciphertext)
		tampered[len(tampered)-5] ^= 0xFF // Flip some bits

		_, err = svc.Decrypt(ctx, "tenant", "tamper", tampered)
		assert.ErrorIs(t, err, ErrDecryptionFailed)
	})

	t.Run("wrong resource fails decrypt", func(t *testing.T) {
		plaintext := []byte("secret")

		ciphertext, err := svc.Encrypt(ctx, "tenant", "correct", plaintext)
		require.NoError(t, err)

		// Try to decrypt with wrong resource (different DEK)
		_, err = svc.Decrypt(ctx, "tenant", "wrong", ciphertext)
		assert.Error(t, err) // Should fail because DEK doesn't exist
	})

	t.Run("empty plaintext", func(t *testing.T) {
		plaintext := []byte{}

		ciphertext, err := svc.Encrypt(ctx, "tenant", "empty", plaintext)
		require.NoError(t, err)

		decrypted, err := svc.Decrypt(ctx, "tenant", "empty", ciphertext)
		require.NoError(t, err)
		assert.Len(t, decrypted, 0, "decrypted should be empty")
	})

	t.Run("large plaintext", func(t *testing.T) {
		plaintext := make([]byte, 1024*1024) // 1 MB
		for i := range plaintext {
			plaintext[i] = byte(i % 256)
		}

		ciphertext, err := svc.Encrypt(ctx, "tenant", "large", plaintext)
		require.NoError(t, err)

		decrypted, err := svc.Decrypt(ctx, "tenant", "large", ciphertext)
		require.NoError(t, err)
		assert.Equal(t, plaintext, decrypted)
	})
}

func TestEncryptDecryptString(t *testing.T) {
	kek, err := GenerateKEK()
	require.NoError(t, err)

	store := newMockDEKStore()
	svc, err := NewService(kek, store, nil)
	require.NoError(t, err)

	ctx := context.Background()

	t.Run("string roundtrip", func(t *testing.T) {
		original := "hello, world! 你好世界"

		encrypted, err := svc.EncryptString(ctx, "tenant", "str1", original)
		require.NoError(t, err)
		assert.NotEqual(t, original, encrypted)

		decrypted, err := svc.DecryptString(ctx, "tenant", "str1", encrypted)
		require.NoError(t, err)
		assert.Equal(t, original, decrypted)
	})

	t.Run("invalid base64 fails", func(t *testing.T) {
		_, err := svc.DecryptString(ctx, "tenant", "str1", "not-base64!!!")
		assert.ErrorIs(t, err, ErrDecryptionFailed)
	})
}

func TestDirectEncryption(t *testing.T) {
	kek, err := GenerateKEK()
	require.NoError(t, err)

	svc, err := NewService(kek, nil, nil)
	require.NoError(t, err)

	t.Run("direct encryption roundtrip", func(t *testing.T) {
		plaintext := []byte("direct encryption test")

		ciphertext, err := svc.EncryptDirect(plaintext)
		require.NoError(t, err)

		decrypted, err := svc.DecryptDirect(ciphertext)
		require.NoError(t, err)
		assert.Equal(t, plaintext, decrypted)
	})
}

func TestDEKCaching(t *testing.T) {
	kek, err := GenerateKEK()
	require.NoError(t, err)

	store := newMockDEKStore()
	svc, err := NewService(kek, store, nil)
	require.NoError(t, err)

	ctx := context.Background()

	t.Run("DEK is cached", func(t *testing.T) {
		// First encryption creates and caches DEK
		_, err := svc.Encrypt(ctx, "tenant", "cache1", []byte("test1"))
		require.NoError(t, err)

		// Verify DEK is in store
		dk, err := store.GetDEK(ctx, "tenant", "cache1")
		require.NoError(t, err)
		originalDEK := dk.DEKEncrypted

		// Second encryption should use cached DEK
		_, err = svc.Encrypt(ctx, "tenant", "cache1", []byte("test2"))
		require.NoError(t, err)

		// DEK in store should be unchanged
		dk, err = store.GetDEK(ctx, "tenant", "cache1")
		require.NoError(t, err)
		assert.Equal(t, originalDEK, dk.DEKEncrypted)
	})

	t.Run("ClearCache forces reload", func(t *testing.T) {
		_, err := svc.Encrypt(ctx, "tenant", "cache2", []byte("test"))
		require.NoError(t, err)

		svc.ClearCache()

		// Should still work (reloads from store)
		_, err = svc.Encrypt(ctx, "tenant", "cache2", []byte("test2"))
		require.NoError(t, err)
	})
}

func TestDEKRotation(t *testing.T) {
	kek, err := GenerateKEK()
	require.NoError(t, err)

	store := newMockDEKStore()
	svc, err := NewService(kek, store, nil)
	require.NoError(t, err)

	ctx := context.Background()

	t.Run("rotation changes DEK", func(t *testing.T) {
		// Create initial DEK
		ct1, err := svc.Encrypt(ctx, "tenant", "rotate1", []byte("before rotation"))
		require.NoError(t, err)

		dk1, _ := store.GetDEK(ctx, "tenant", "rotate1")
		originalDEK := dk1.DEKEncrypted

		// Rotate DEK
		err = svc.RotateDEK(ctx, "tenant", "rotate1")
		require.NoError(t, err)

		dk2, _ := store.GetDEK(ctx, "tenant", "rotate1")
		assert.NotEqual(t, originalDEK, dk2.DEKEncrypted)
		assert.NotNil(t, dk2.RotatedAt)

		// Old ciphertext can no longer be decrypted (wrong DEK in cache)
		// Note: In a real system, you'd need to re-encrypt data after rotation
		ct2, err := svc.Encrypt(ctx, "tenant", "rotate1", []byte("after rotation"))
		require.NoError(t, err)
		assert.NotEqual(t, ct1, ct2)
	})
}

func TestStatelessMode(t *testing.T) {
	kek, err := GenerateKEK()
	require.NoError(t, err)

	// No store - stateless mode
	svc, err := NewService(kek, nil, nil)
	require.NoError(t, err)

	ctx := context.Background()

	t.Run("works without store", func(t *testing.T) {
		plaintext := []byte("stateless test")

		ciphertext, err := svc.Encrypt(ctx, "tenant", "stateless", plaintext)
		require.NoError(t, err)

		decrypted, err := svc.Decrypt(ctx, "tenant", "stateless", ciphertext)
		require.NoError(t, err)
		assert.Equal(t, plaintext, decrypted)
	})

	t.Run("DEK is only in cache", func(t *testing.T) {
		_, err := svc.Encrypt(ctx, "tenant", "nocache", []byte("test"))
		require.NoError(t, err)

		// Clear cache
		svc.ClearCache()

		// Now decrypt fails because DEK is gone
		_, err = svc.Decrypt(ctx, "tenant", "nocache", []byte("anything"))
		assert.ErrorIs(t, err, ErrDEKNotFound)
	})
}

func TestGenerateKEK(t *testing.T) {
	t.Run("generates valid KEK", func(t *testing.T) {
		kek, err := GenerateKEK()
		require.NoError(t, err)

		assert.Len(t, kek, 64, "KEK should be 64 hex chars (32 bytes)")

		// Should be valid for creating a service
		_, err = NewService(kek, nil, nil)
		require.NoError(t, err)
	})

	t.Run("generates unique KEKs", func(t *testing.T) {
		seen := make(map[string]bool)
		for i := 0; i < 100; i++ {
			kek, err := GenerateKEK()
			require.NoError(t, err)
			assert.False(t, seen[kek], "KEK should be unique")
			seen[kek] = true
		}
	})
}

func TestKEKID(t *testing.T) {
	kek, err := GenerateKEK()
	require.NoError(t, err)

	store := newMockDEKStore()
	svc, err := NewServiceWithKEKID(kek, "kek-v1", store, nil)
	require.NoError(t, err)

	ctx := context.Background()

	t.Run("KEKID is stored with DEK", func(t *testing.T) {
		_, err := svc.Encrypt(ctx, "tenant", "kekid", []byte("test"))
		require.NoError(t, err)

		dk, err := store.GetDEK(ctx, "tenant", "kekid")
		require.NoError(t, err)
		assert.Equal(t, "kek-v1", dk.KEKID)
	})
}

// BenchmarkEncrypt measures encryption performance.
func BenchmarkEncrypt(b *testing.B) {
	kek, _ := GenerateKEK()
	svc, _ := NewService(kek, nil, nil)
	ctx := context.Background()
	plaintext := make([]byte, 1024) // 1 KB

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = svc.Encrypt(ctx, "tenant", "bench", plaintext)
	}
}

// BenchmarkDecrypt measures decryption performance.
func BenchmarkDecrypt(b *testing.B) {
	kek, _ := GenerateKEK()
	svc, _ := NewService(kek, nil, nil)
	ctx := context.Background()
	plaintext := make([]byte, 1024)
	ciphertext, _ := svc.Encrypt(ctx, "tenant", "bench", plaintext)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = svc.Decrypt(ctx, "tenant", "bench", ciphertext)
	}
}
