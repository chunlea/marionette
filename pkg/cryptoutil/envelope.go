package cryptoutil

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"golang.org/x/sync/singleflight"
)

const (
	// keySize is the size of AES-256 keys in bytes.
	keySize = 32

	// nonceSize is the size of GCM nonce in bytes.
	nonceSize = 12

	// AlgorithmAES256GCM is the encryption algorithm identifier.
	AlgorithmAES256GCM = "AES-256-GCM"
)

// DataKey represents an encrypted DEK stored in the database.
type DataKey struct {
	ID           string     `json:"id"`            // dek_xxx
	ResourceType string     `json:"resource_type"` // "tenant", "agent_config", etc.
	ResourceID   string     `json:"resource_id"`   // The resource this DEK encrypts
	DEKEncrypted string     `json:"dek_encrypted"` // DEK encrypted with KEK (base64)
	Algorithm    string     `json:"algorithm"`     // "AES-256-GCM"
	KEKID        string     `json:"kek_id"`        // Optional: KEK identifier for rotation
	CreatedAt    time.Time  `json:"created_at"`
	RotatedAt    *time.Time `json:"rotated_at,omitempty"`
}

// DEKStore defines the interface for persisting encrypted DEKs.
type DEKStore interface {
	// GetDEK retrieves a DEK by resource type and ID.
	GetDEK(ctx context.Context, resourceType, resourceID string) (*DataKey, error)

	// CreateDEK stores a new encrypted DEK.
	CreateDEK(ctx context.Context, dk *DataKey) error

	// UpdateDEK updates an existing DEK (for rotation).
	UpdateDEK(ctx context.Context, dk *DataKey) error
}

// Service provides envelope encryption operations using KEK/DEK architecture.
//
// The Key Encryption Key (KEK) is used to encrypt Data Encryption Keys (DEKs).
// Each resource (e.g., tenant, agent config) has its own DEK, which is stored
// encrypted by the KEK in the database.
type Service struct {
	kek   []byte        // Key Encryption Key (from environment)
	kekID string        // Optional KEK identifier
	store DEKStore      // For persisting encrypted DEKs
	idGen func() string // ID generator for new DEKs

	// dekCache holds decrypted DEKs keyed by "resourceType:resourceID".
	// Bounded: one entry per tenant per resource type would otherwise grow
	// without limit, and these are plaintext key material held in memory.
	dekCache *dekCache

	// creating collapses concurrent first-use of the same resource into one
	// DEK creation. Without it every caller generates its own key and all but
	// one are discarded — along with anything already encrypted under them.
	creating singleflight.Group
}

// NewService creates a new encryption service.
//
// Parameters:
//   - kekHex: The Key Encryption Key as a hex-encoded string (64 hex chars = 32 bytes)
//   - store: The DEK store for persisting encrypted DEKs (can be nil for stateless mode)
//   - idGen: Function to generate DEK IDs (e.g., id.DataKey)
func NewService(kekHex string, store DEKStore, idGen func() string) (*Service, error) {
	kek, err := hex.DecodeString(kekHex)
	if err != nil {
		return nil, fmt.Errorf("%w: invalid hex encoding", ErrInvalidKEK)
	}

	if len(kek) != keySize {
		return nil, fmt.Errorf("%w: expected %d bytes, got %d", ErrInvalidKEK, keySize, len(kek))
	}

	return &Service{
		kek:      kek,
		store:    store,
		idGen:    idGen,
		dekCache: newDEKCache(defaultDEKCacheSize),
	}, nil
}

// NewServiceWithKEKID creates a new encryption service with a KEK identifier.
// The KEK ID is stored with DEKs to support KEK rotation.
func NewServiceWithKEKID(kekHex, kekID string, store DEKStore, idGen func() string) (*Service, error) {
	svc, err := NewService(kekHex, store, idGen)
	if err != nil {
		return nil, err
	}
	svc.kekID = kekID
	return svc, nil
}

// Encrypt encrypts plaintext using the DEK for the specified resource.
// If no DEK exists for the resource, a new one is created.
//
// The ciphertext format is: nonce (12 bytes) || ciphertext || tag (16 bytes)
func (s *Service) Encrypt(ctx context.Context, resourceType, resourceID string, plaintext []byte) ([]byte, error) {
	dek, err := s.getOrCreateDEK(ctx, resourceType, resourceID)
	if err != nil {
		return nil, err
	}

	return s.encryptWithKey(dek, plaintext)
}

// Decrypt decrypts ciphertext using the DEK for the specified resource.
func (s *Service) Decrypt(ctx context.Context, resourceType, resourceID string, ciphertext []byte) ([]byte, error) {
	dek, err := s.getDEK(ctx, resourceType, resourceID)
	if err != nil {
		return nil, err
	}

	return s.decryptWithKey(dek, ciphertext)
}

// EncryptString is a convenience wrapper that encrypts a string and returns base64-encoded ciphertext.
func (s *Service) EncryptString(ctx context.Context, resourceType, resourceID, plaintext string) (string, error) {
	ciphertext, err := s.Encrypt(ctx, resourceType, resourceID, []byte(plaintext))
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(ciphertext), nil
}

// DecryptString is a convenience wrapper that decrypts base64-encoded ciphertext and returns a string.
func (s *Service) DecryptString(ctx context.Context, resourceType, resourceID, ciphertext string) (string, error) {
	data, err := base64.StdEncoding.DecodeString(ciphertext)
	if err != nil {
		return "", fmt.Errorf("%w: invalid base64", ErrDecryptionFailed)
	}

	plaintext, err := s.Decrypt(ctx, resourceType, resourceID, data)
	if err != nil {
		return "", err
	}
	return string(plaintext), nil
}

// RotateDEK generates a new DEK for the specified resource.
// Note: This does NOT re-encrypt existing data. The caller must re-encrypt any data
// that was encrypted with the old DEK.
func (s *Service) RotateDEK(ctx context.Context, resourceType, resourceID string) error {
	if s.store == nil {
		return fmt.Errorf("DEK rotation requires a store")
	}

	// Generate new DEK
	newDEK := make([]byte, keySize)
	if _, err := rand.Read(newDEK); err != nil {
		return fmt.Errorf("failed to generate new DEK: %w", err)
	}

	// Encrypt new DEK with KEK
	encryptedDEK, err := s.encryptWithKey(s.kek, newDEK)
	if err != nil {
		return fmt.Errorf("failed to encrypt new DEK: %w", err)
	}

	// Get existing DEK record
	existing, err := s.store.GetDEK(ctx, resourceType, resourceID)
	if err != nil {
		return fmt.Errorf("failed to get existing DEK: %w", err)
	}

	now := time.Now()
	existing.DEKEncrypted = base64.StdEncoding.EncodeToString(encryptedDEK)
	existing.RotatedAt = &now
	existing.KEKID = s.kekID

	// Update in store
	if err := s.store.UpdateDEK(ctx, existing); err != nil {
		return fmt.Errorf("failed to update DEK: %w", err)
	}

	// The rotated key replaces the cached one immediately: leaving the old one
	// cached is how a rotation silently fails to take effect.
	s.dekCache.put(resourceType+":"+resourceID, newDEK)

	return nil
}

// EncryptDirect encrypts data directly with the KEK (no DEK).
// Use this for encrypting DEKs themselves or other key material.
func (s *Service) EncryptDirect(plaintext []byte) ([]byte, error) {
	return s.encryptWithKey(s.kek, plaintext)
}

// DecryptDirect decrypts data directly with the KEK (no DEK).
// Use this for decrypting DEKs themselves or other key material.
func (s *Service) DecryptDirect(ciphertext []byte) ([]byte, error) {
	return s.decryptWithKey(s.kek, ciphertext)
}

// getOrCreateDEK retrieves or creates a DEK for the specified resource.
func (s *Service) getOrCreateDEK(ctx context.Context, resourceType, resourceID string) ([]byte, error) {
	cacheKey := resourceType + ":" + resourceID

	if cached, ok := s.dekCache.get(cacheKey); ok {
		return cached, nil
	}

	// One creation per resource per process; the rest wait and share the result.
	dek, err, _ := s.creating.Do(cacheKey, func() (any, error) {
		return s.loadOrCreateDEK(ctx, resourceType, resourceID)
	})
	if err != nil {
		return nil, err
	}

	return dek.([]byte), nil
}

func (s *Service) loadOrCreateDEK(ctx context.Context, resourceType, resourceID string) ([]byte, error) {
	cacheKey := resourceType + ":" + resourceID

	// Another caller may have finished while this one waited.
	if cached, ok := s.dekCache.get(cacheKey); ok {
		return cached, nil
	}

	if s.store != nil {
		dek, err := s.loadDEK(ctx, resourceType, resourceID)
		switch {
		case err == nil:
			s.dekCache.put(cacheKey, dek)
			return dek, nil
		case !errors.Is(err, ErrDEKNotFound):
			// Anything other than "no key yet" is a real failure. This used to
			// be a != comparison, which never matched a wrapped error and so
			// treated a broken store as an empty one — quietly generating a
			// second DEK for a resource that already had one.
			return nil, fmt.Errorf("failed to get DEK: %w", err)
		}
	}

	dek := make([]byte, keySize)
	if _, err := rand.Read(dek); err != nil {
		return nil, fmt.Errorf("failed to generate DEK: %w", err)
	}

	if s.store == nil {
		s.dekCache.put(cacheKey, dek)
		return dek, nil
	}

	encryptedDEK, err := s.encryptWithKey(s.kek, dek)
	if err != nil {
		return nil, fmt.Errorf("failed to encrypt DEK: %w", err)
	}

	id := ""
	if s.idGen != nil {
		id = s.idGen()
	}

	dk := &DataKey{
		ID:           id,
		ResourceType: resourceType,
		ResourceID:   resourceID,
		DEKEncrypted: base64.StdEncoding.EncodeToString(encryptedDEK),
		Algorithm:    AlgorithmAES256GCM,
		KEKID:        s.kekID,
		CreatedAt:    time.Now(),
	}

	if err := s.store.CreateDEK(ctx, dk); err != nil {
		if !errors.Is(err, ErrDEKExists) {
			return nil, fmt.Errorf("failed to store DEK: %w", err)
		}

		// Another process won. Its key is the one everything else will use, so
		// discard ours and adopt theirs; keeping ours would make anything we
		// encrypt unreadable to every other process.
		existing, err := s.loadDEK(ctx, resourceType, resourceID)
		if err != nil {
			return nil, fmt.Errorf("failed to load concurrently created DEK: %w", err)
		}
		s.dekCache.put(cacheKey, existing)
		return existing, nil
	}

	s.dekCache.put(cacheKey, dek)
	return dek, nil
}

// loadDEK reads and decrypts the stored DEK for a resource. It does not cache.
func (s *Service) loadDEK(ctx context.Context, resourceType, resourceID string) ([]byte, error) {
	dk, err := s.store.GetDEK(ctx, resourceType, resourceID)
	if err != nil {
		return nil, err
	}
	if dk == nil {
		return nil, ErrDEKNotFound
	}

	encryptedDEK, err := base64.StdEncoding.DecodeString(dk.DEKEncrypted)
	if err != nil {
		return nil, fmt.Errorf("failed to decode DEK: %w", err)
	}

	dek, err := s.decryptWithKey(s.kek, encryptedDEK)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt DEK: %w", err)
	}

	return dek, nil
}

// getDEK retrieves a DEK for the specified resource (does not create).
func (s *Service) getDEK(ctx context.Context, resourceType, resourceID string) ([]byte, error) {
	cacheKey := resourceType + ":" + resourceID

	if cached, ok := s.dekCache.get(cacheKey); ok {
		return cached, nil
	}

	if s.store == nil {
		return nil, ErrDEKNotFound
	}

	dek, err := s.loadDEK(ctx, resourceType, resourceID)
	if err != nil {
		return nil, err
	}

	s.dekCache.put(cacheKey, dek)
	return dek, nil
}

// InvalidateDEK drops the cached key for a resource, so the next use re-reads
// it. Call this after rotating a DEK: the cache is the whole reason a rotation
// would otherwise not take effect until the process restarts.
func (s *Service) InvalidateDEK(resourceType, resourceID string) {
	s.dekCache.delete(resourceType + ":" + resourceID)
}

// InvalidateAll drops every cached key. Call this after a KEK rotation.
func (s *Service) InvalidateAll() {
	s.dekCache.reset()
}

// encryptWithKey encrypts plaintext using AES-256-GCM with the given key.
// Returns: nonce (12 bytes) || ciphertext || tag (16 bytes)
func (s *Service) encryptWithKey(key, plaintext []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("failed to create cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("failed to create GCM: %w", err)
	}

	nonce := make([]byte, nonceSize)
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("failed to generate nonce: %w", err)
	}

	// Seal appends the ciphertext+tag to nonce
	ciphertext := gcm.Seal(nonce, nonce, plaintext, nil)
	return ciphertext, nil
}

// decryptWithKey decrypts ciphertext using AES-256-GCM with the given key.
// Expects: nonce (12 bytes) || ciphertext || tag (16 bytes)
func (s *Service) decryptWithKey(key, ciphertext []byte) ([]byte, error) {
	if len(ciphertext) < nonceSize {
		return nil, ErrDecryptionFailed
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("failed to create cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("failed to create GCM: %w", err)
	}

	nonce := ciphertext[:nonceSize]
	ciphertextAndTag := ciphertext[nonceSize:]

	plaintext, err := gcm.Open(nil, nonce, ciphertextAndTag, nil)
	if err != nil {
		return nil, ErrDecryptionFailed
	}

	return plaintext, nil
}

// ClearCache clears the DEK cache. Useful for testing or after KEK rotation.
//
// Deprecated: use InvalidateAll, which says what it does at the call site.
func (s *Service) ClearCache() {
	s.InvalidateAll()
}

// GenerateKEK generates a new random KEK and returns it as a hex string.
// This is a utility function for initial setup.
func GenerateKEK() (string, error) {
	key := make([]byte, keySize)
	if _, err := rand.Read(key); err != nil {
		return "", err
	}
	return hex.EncodeToString(key), nil
}
