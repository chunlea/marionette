package cas

import (
	"context"
)

// ChunkStore defines operations for storing and retrieving encrypted chunks.
type ChunkStore interface {
	// StoreChunk compresses, encrypts, and stores a chunk.
	// Returns the storage size (compressed + encrypted) and any error.
	StoreChunk(ctx context.Context, tenantID, hash string, data []byte) (int64, error)

	// GetChunk retrieves, decrypts, and decompresses a chunk.
	GetChunk(ctx context.Context, tenantID, hash string) ([]byte, error)

	// DeleteChunk removes a chunk from storage.
	DeleteChunk(ctx context.Context, tenantID, hash string) error

	// ChunkExists checks if a chunk exists in storage.
	ChunkExists(ctx context.Context, tenantID, hash string) (bool, error)
}

// ManifestStore defines operations for workspace manifests.
type ManifestStore interface {
	// SaveManifest stores a manifest using JSONL streaming format.
	SaveManifest(ctx context.Context, manifest *Manifest) error

	// LoadManifest loads a complete manifest.
	LoadManifest(ctx context.Context, tenantID, workspaceID, manifestID string) (*Manifest, error)

	// StreamManifestFiles returns a channel for streaming manifest files.
	// Use for very large manifests (100k+ files).
	// The header is returned synchronously, files are streamed via channel.
	// The channel is closed when all files have been sent or an error occurs.
	StreamManifestFiles(ctx context.Context, tenantID, workspaceID, manifestID string) (<-chan ManifestFile, *ManifestHeader, error)

	// DeleteManifest removes a manifest from storage.
	DeleteManifest(ctx context.Context, tenantID, workspaceID, manifestID string) error
}

// Syncer defines the workspace sync/restore interface.
type Syncer interface {
	// Sync performs incremental workspace sync.
	// Returns the created manifest ID.
	Sync(ctx context.Context, workspaceID, tenantID, srcDir string) (string, error)

	// Restore reconstructs a workspace from the latest manifest.
	Restore(ctx context.Context, workspaceID, tenantID, dstDir string) error

	// RestoreFromManifest restores from a specific manifest.
	RestoreFromManifest(ctx context.Context, manifestID, tenantID, dstDir string) error

	// ValidateManifest checks that all chunks referenced by a manifest exist.
	ValidateManifest(ctx context.Context, manifest *Manifest) error
}

// Encryptor provides encryption and decryption for CAS data.
type Encryptor interface {
	// Encrypt compresses and encrypts data for a tenant.
	Encrypt(ctx context.Context, tenantID string, data []byte) ([]byte, error)

	// Decrypt decrypts and decompresses data for a tenant.
	Decrypt(ctx context.Context, tenantID string, ciphertext []byte) ([]byte, error)
}
