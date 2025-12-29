package cas

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/chunlea/marionette/pkg/crypto"
	"github.com/chunlea/marionette/pkg/id"
)

func TestNoOpEncryptor_EncryptDecrypt(t *testing.T) {
	ctx := context.Background()
	encryptor := NewNoOpEncryptor()

	original := []byte("hello world, this is a test message for compression")

	// Encrypt (compress)
	encrypted, err := encryptor.Encrypt(ctx, "tenant-1", original)
	require.NoError(t, err)
	assert.NotEmpty(t, encrypted)

	// For compressible data, encrypted should be smaller
	// (but may be larger for small or random data due to zstd overhead)

	// Decrypt (decompress)
	decrypted, err := encryptor.Decrypt(ctx, "tenant-1", encrypted)
	require.NoError(t, err)
	assert.Equal(t, original, decrypted)
}

func TestNoOpEncryptor_CompressesLargeData(t *testing.T) {
	ctx := context.Background()
	encryptor := NewNoOpEncryptor()

	// Create highly compressible data
	original := make([]byte, 100*1024) // 100 KB
	for i := range original {
		original[i] = byte(i % 256)
	}

	// Encrypt (compress)
	encrypted, err := encryptor.Encrypt(ctx, "tenant-1", original)
	require.NoError(t, err)

	// Compressed size should be smaller for repetitive data
	t.Logf("Original size: %d, Compressed size: %d", len(original), len(encrypted))
	assert.Less(t, len(encrypted), len(original))

	// Decrypt (decompress)
	decrypted, err := encryptor.Decrypt(ctx, "tenant-1", encrypted)
	require.NoError(t, err)
	assert.Equal(t, original, decrypted)
}

func TestNoOpEncryptor_RandomData(t *testing.T) {
	ctx := context.Background()
	encryptor := NewNoOpEncryptor()

	// Random data is less compressible
	original := make([]byte, 10*1024) // 10 KB
	_, err := rand.Read(original)
	require.NoError(t, err)

	// Encrypt
	encrypted, err := encryptor.Encrypt(ctx, "tenant-1", original)
	require.NoError(t, err)

	// Decrypt
	decrypted, err := encryptor.Decrypt(ctx, "tenant-1", encrypted)
	require.NoError(t, err)
	assert.Equal(t, original, decrypted)
}

func TestNoOpEncryptor_EmptyData(t *testing.T) {
	ctx := context.Background()
	encryptor := NewNoOpEncryptor()

	// Empty data
	original := []byte{}

	encrypted, err := encryptor.Encrypt(ctx, "tenant-1", original)
	require.NoError(t, err)

	decrypted, err := encryptor.Decrypt(ctx, "tenant-1", encrypted)
	require.NoError(t, err)
	// Both nil and empty slice are acceptable for empty data
	assert.Empty(t, decrypted)
}

func TestNoOpEncryptor_TenantIndependent(t *testing.T) {
	ctx := context.Background()
	encryptor := NewNoOpEncryptor()

	original := []byte("test data")

	// Encrypt with tenant-1
	encrypted1, err := encryptor.Encrypt(ctx, "tenant-1", original)
	require.NoError(t, err)

	// Encrypt with tenant-2
	encrypted2, err := encryptor.Encrypt(ctx, "tenant-2", original)
	require.NoError(t, err)

	// NoOpEncryptor should produce same output (no encryption, just compression)
	assert.Equal(t, encrypted1, encrypted2)

	// Can decrypt with any tenant ID
	decrypted, err := encryptor.Decrypt(ctx, "tenant-3", encrypted1)
	require.NoError(t, err)
	assert.Equal(t, original, decrypted)
}

func TestNoOpEncryptor_InvalidInput(t *testing.T) {
	ctx := context.Background()
	encryptor := NewNoOpEncryptor()

	// Try to decrypt invalid data
	_, err := encryptor.Decrypt(ctx, "tenant-1", []byte("not valid zstd data"))
	assert.Error(t, err)
}

// mockDEKStore implements crypto.DEKStore for testing.
type mockDEKStore struct {
	deks map[string]*crypto.DataKey
}

func newMockDEKStore() *mockDEKStore {
	return &mockDEKStore{deks: make(map[string]*crypto.DataKey)}
}

func (m *mockDEKStore) GetDEK(_ context.Context, resourceType, resourceID string) (*crypto.DataKey, error) {
	key := resourceType + ":" + resourceID
	dk, ok := m.deks[key]
	if !ok {
		return nil, crypto.ErrDEKNotFound
	}
	return dk, nil
}

func (m *mockDEKStore) CreateDEK(_ context.Context, dk *crypto.DataKey) error {
	key := dk.ResourceType + ":" + dk.ResourceID
	m.deks[key] = dk
	return nil
}

func (m *mockDEKStore) UpdateDEK(_ context.Context, dk *crypto.DataKey) error {
	key := dk.ResourceType + ":" + dk.ResourceID
	m.deks[key] = dk
	return nil
}

func TestTenantEncryptor_EncryptDecrypt(t *testing.T) {
	ctx := context.Background()

	// Create a valid KEK (32 bytes = 64 hex chars)
	kekBytes := make([]byte, 32)
	_, err := rand.Read(kekBytes)
	require.NoError(t, err)
	kekHex := hex.EncodeToString(kekBytes)

	store := newMockDEKStore()
	cryptoSvc, err := crypto.NewService(kekHex, store, id.DataKey)
	require.NoError(t, err)

	encryptor := NewTenantEncryptor(cryptoSvc)

	original := []byte("hello world, this is a test message for tenant encryption")

	// Encrypt
	encrypted, err := encryptor.Encrypt(ctx, "tenant-1", original)
	require.NoError(t, err)
	assert.NotEmpty(t, encrypted)
	assert.NotEqual(t, original, encrypted)

	// Decrypt
	decrypted, err := encryptor.Decrypt(ctx, "tenant-1", encrypted)
	require.NoError(t, err)
	assert.Equal(t, original, decrypted)
}

func TestTenantEncryptor_TenantIsolation(t *testing.T) {
	ctx := context.Background()

	kekBytes := make([]byte, 32)
	_, _ = rand.Read(kekBytes)
	kekHex := hex.EncodeToString(kekBytes)

	store := newMockDEKStore()
	cryptoSvc, _ := crypto.NewService(kekHex, store, id.DataKey)

	encryptor := NewTenantEncryptor(cryptoSvc)

	original := []byte("secret data")

	// Encrypt with tenant-1
	encrypted1, err := encryptor.Encrypt(ctx, "tenant-1", original)
	require.NoError(t, err)

	// Encrypt with tenant-2
	encrypted2, err := encryptor.Encrypt(ctx, "tenant-2", original)
	require.NoError(t, err)

	// Different tenants should produce different ciphertexts (different DEKs)
	assert.NotEqual(t, encrypted1, encrypted2)

	// Each tenant can only decrypt their own data
	decrypted1, err := encryptor.Decrypt(ctx, "tenant-1", encrypted1)
	require.NoError(t, err)
	assert.Equal(t, original, decrypted1)

	// Tenant-2 cannot decrypt tenant-1's data
	_, err = encryptor.Decrypt(ctx, "tenant-2", encrypted1)
	assert.Error(t, err)
}

func TestTenantEncryptorWithLevel(t *testing.T) {
	ctx := context.Background()

	kekBytes := make([]byte, 32)
	_, _ = rand.Read(kekBytes)
	kekHex := hex.EncodeToString(kekBytes)

	store := newMockDEKStore()
	cryptoSvc, _ := crypto.NewService(kekHex, store, id.DataKey)

	// Test different compression levels
	testCases := []struct {
		level int
	}{
		{0},  // Fastest
		{1},  // Fastest
		{3},  // Default
		{6},  // Better
		{10}, // Best
	}

	original := []byte("test data for different compression levels")

	for _, tc := range testCases {
		encryptor := NewTenantEncryptorWithLevel(cryptoSvc, tc.level)

		encrypted, err := encryptor.Encrypt(ctx, "tenant-1", original)
		require.NoError(t, err)

		decrypted, err := encryptor.Decrypt(ctx, "tenant-1", encrypted)
		require.NoError(t, err)
		assert.Equal(t, original, decrypted)
	}
}

func TestTenantEncryptor_LargeData(t *testing.T) {
	ctx := context.Background()

	kekBytes := make([]byte, 32)
	_, _ = rand.Read(kekBytes)
	kekHex := hex.EncodeToString(kekBytes)

	store := newMockDEKStore()
	cryptoSvc, _ := crypto.NewService(kekHex, store, id.DataKey)

	encryptor := NewTenantEncryptor(cryptoSvc)

	// Create large compressible data
	original := make([]byte, 1024*1024) // 1 MB
	for i := range original {
		original[i] = byte(i % 256)
	}

	encrypted, err := encryptor.Encrypt(ctx, "tenant-1", original)
	require.NoError(t, err)

	// Compressed + encrypted should be smaller than original for compressible data
	t.Logf("Original: %d bytes, Encrypted: %d bytes", len(original), len(encrypted))

	decrypted, err := encryptor.Decrypt(ctx, "tenant-1", encrypted)
	require.NoError(t, err)
	assert.Equal(t, original, decrypted)
}

func TestTenantEncryptor_DecryptInvalidData(t *testing.T) {
	ctx := context.Background()

	kekBytes := make([]byte, 32)
	_, _ = rand.Read(kekBytes)
	kekHex := hex.EncodeToString(kekBytes)

	store := newMockDEKStore()
	cryptoSvc, _ := crypto.NewService(kekHex, store, id.DataKey)

	encryptor := NewTenantEncryptor(cryptoSvc)

	// First encrypt something to create a DEK
	_, err := encryptor.Encrypt(ctx, "tenant-1", []byte("test"))
	require.NoError(t, err)

	// Try to decrypt garbage data
	_, err = encryptor.Decrypt(ctx, "tenant-1", []byte("garbage data that is definitely not valid"))
	assert.Error(t, err)
}

func TestNoOpEncryptor_DecryptCorruptedZstd(t *testing.T) {
	ctx := context.Background()
	encryptor := NewNoOpEncryptor()

	// Encrypt valid data
	encrypted, err := encryptor.Encrypt(ctx, "tenant-1", []byte("test data"))
	require.NoError(t, err)

	// Corrupt the encrypted data
	if len(encrypted) > 5 {
		encrypted[5] ^= 0xFF
	}

	// Try to decrypt corrupted data
	_, err = encryptor.Decrypt(ctx, "tenant-1", encrypted)
	assert.Error(t, err)
}
