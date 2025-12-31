package cas

import (
	"context"
	"fmt"

	"github.com/klauspost/compress/zstd"

	"github.com/chunlea/marionette/pkg/cryptoutil"
)

// TenantEncryptor provides tenant-scoped encryption for CAS data.
// It uses zstd compression followed by AES-256-GCM encryption.
type TenantEncryptor struct {
	crypto        *cryptoutil.Service
	compressLevel zstd.EncoderLevel
}

// NewTenantEncryptor creates a new encryptor with default compression level.
func NewTenantEncryptor(cryptoSvc *cryptoutil.Service) *TenantEncryptor {
	return &TenantEncryptor{
		crypto:        cryptoSvc,
		compressLevel: zstd.SpeedDefault,
	}
}

// NewTenantEncryptorWithLevel creates a new encryptor with specified compression level.
func NewTenantEncryptorWithLevel(cryptoSvc *cryptoutil.Service, level int) *TenantEncryptor {
	// Map int level to zstd.EncoderLevel
	var zstdLevel zstd.EncoderLevel
	switch {
	case level <= 1:
		zstdLevel = zstd.SpeedFastest
	case level <= 3:
		zstdLevel = zstd.SpeedDefault
	case level <= 6:
		zstdLevel = zstd.SpeedBetterCompression
	default:
		zstdLevel = zstd.SpeedBestCompression
	}

	return &TenantEncryptor{
		crypto:        cryptoSvc,
		compressLevel: zstdLevel,
	}
}

// Encrypt compresses and encrypts data for a tenant.
// Pipeline: data -> zstd compress -> AES-256-GCM encrypt
// Returns: compressed + encrypted bytes
func (e *TenantEncryptor) Encrypt(ctx context.Context, tenantID string, data []byte) ([]byte, error) {
	// 1. Compress with zstd
	encoder, err := zstd.NewWriter(nil, zstd.WithEncoderLevel(e.compressLevel))
	if err != nil {
		return nil, fmt.Errorf("failed to create zstd encoder: %w", err)
	}
	compressed := encoder.EncodeAll(data, nil)
	if err := encoder.Close(); err != nil {
		return nil, fmt.Errorf("failed to close zstd encoder: %w", err)
	}

	// 2. Encrypt with tenant DEK
	// Uses resourceType="tenant" and resourceID=tenantID
	ciphertext, err := e.cryptoutil.Encrypt(ctx, "tenant", tenantID, compressed)
	if err != nil {
		return nil, fmt.Errorf("failed to encrypt data: %w", err)
	}

	return ciphertext, nil
}

// Decrypt decrypts and decompresses data for a tenant.
// Pipeline: ciphertext -> AES-256-GCM decrypt -> zstd decompress
func (e *TenantEncryptor) Decrypt(ctx context.Context, tenantID string, ciphertext []byte) ([]byte, error) {
	// 1. Decrypt with tenant DEK
	compressed, err := e.cryptoutil.Decrypt(ctx, "tenant", tenantID, ciphertext)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt data: %w", err)
	}

	// 2. Decompress with zstd
	decoder, err := zstd.NewReader(nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create zstd decoder: %w", err)
	}
	defer decoder.Close()

	data, err := decoder.DecodeAll(compressed, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to decompress data: %w", err)
	}

	return data, nil
}

// Compile-time interface check.
var _ Encryptor = (*TenantEncryptor)(nil)

// NoOpEncryptor is an encryptor that only compresses data without encryption.
// Use for testing only.
type NoOpEncryptor struct {
	compressLevel zstd.EncoderLevel
}

// NewNoOpEncryptor creates a new no-op encryptor for testing.
func NewNoOpEncryptor() *NoOpEncryptor {
	return &NoOpEncryptor{
		compressLevel: zstd.SpeedDefault,
	}
}

// Encrypt compresses data without encryption.
func (e *NoOpEncryptor) Encrypt(_ context.Context, _ string, data []byte) ([]byte, error) {
	encoder, err := zstd.NewWriter(nil, zstd.WithEncoderLevel(e.compressLevel))
	if err != nil {
		return nil, fmt.Errorf("failed to create zstd encoder: %w", err)
	}
	compressed := encoder.EncodeAll(data, nil)
	if err := encoder.Close(); err != nil {
		return nil, fmt.Errorf("failed to close zstd encoder: %w", err)
	}
	return compressed, nil
}

// Decrypt decompresses data without decryption.
func (e *NoOpEncryptor) Decrypt(_ context.Context, _ string, data []byte) ([]byte, error) {
	decoder, err := zstd.NewReader(nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create zstd decoder: %w", err)
	}
	defer decoder.Close()

	decompressed, err := decoder.DecodeAll(data, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to decompress data: %w", err)
	}
	return decompressed, nil
}

// Compile-time interface check.
var _ Encryptor = (*NoOpEncryptor)(nil)
