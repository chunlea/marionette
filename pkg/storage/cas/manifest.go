package cas

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/klauspost/compress/zstd"

	"github.com/chunlea/marionette/pkg/storage"
)

// manifestKey returns the storage key for a manifest.
func manifestKey(tenantID, workspaceID, manifestID string) string {
	return fmt.Sprintf("manifests/%s/%s/%s.jsonl.zst.enc", tenantID, workspaceID, manifestID)
}

// BlobManifestStore implements ManifestStore using a blob storage provider.
type BlobManifestStore struct {
	storage   storage.StorageProvider
	encryptor Encryptor

	// tempDir is where a streaming write spools its sealed frames. Empty uses
	// the system temporary directory.
	tempDir string
}

// NewBlobManifestStore creates a new manifest store.
func NewBlobManifestStore(storageProvider storage.StorageProvider, encryptor Encryptor) *BlobManifestStore {
	return &BlobManifestStore{
		storage:   storageProvider,
		encryptor: encryptor,
	}
}

// NewBlobManifestStoreWithTempDir creates a manifest store that spools
// streaming writes into tempDir.
//
// A manifest is spooled rather than buffered because its header cannot be
// written until the walk that produces it has finished, and the workspaces
// this matters for have manifests too large to hold. The spool is unlinked as
// soon as it is created, so nothing survives a crash.
func NewBlobManifestStoreWithTempDir(storageProvider storage.StorageProvider, encryptor Encryptor, tempDir string) *BlobManifestStore {
	return &BlobManifestStore{
		storage:   storageProvider,
		encryptor: encryptor,
		tempDir:   tempDir,
	}
}

// newZstdDecoder creates a zstd decoder for manifest decompression.
func newZstdDecoder() (*zstd.Decoder, error) {
	decoder, err := zstd.NewReader(nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create zstd decoder: %w", err)
	}
	return decoder, nil
}

// SaveManifest stores a manifest using JSONL streaming format.
// Format:
//   - Line 1: ManifestHeader (metadata)
//   - Lines 2+: One ManifestFile per line
func (s *BlobManifestStore) SaveManifest(ctx context.Context, manifest *Manifest) error {
	key := manifestKey(manifest.TenantID, manifest.WorkspaceID, manifest.ID)

	// Build JSONL content in memory
	// For very large manifests, consider streaming directly to storage
	var buf bytes.Buffer

	// Write header line
	header := manifest.ToHeader()
	headerBytes, err := json.Marshal(header)
	if err != nil {
		return fmt.Errorf("failed to marshal manifest header: %w", err)
	}
	buf.Write(headerBytes)
	buf.WriteByte('\n')

	// Write file entries
	for _, file := range manifest.Files {
		fileBytes, err := json.Marshal(file)
		if err != nil {
			return fmt.Errorf("failed to marshal manifest file: %w", err)
		}
		buf.Write(fileBytes)
		buf.WriteByte('\n')
	}

	// Compress with zstd
	encoder, err := zstd.NewWriter(nil, zstd.WithEncoderLevel(zstd.SpeedDefault))
	if err != nil {
		return fmt.Errorf("failed to create zstd encoder: %w", err)
	}
	compressed := encoder.EncodeAll(buf.Bytes(), nil)
	if err := encoder.Close(); err != nil {
		return fmt.Errorf("failed to close zstd encoder: %w", err)
	}

	// Encrypt
	encrypted, err := s.encryptor.Encrypt(ctx, manifest.TenantID, compressed)
	if err != nil {
		return fmt.Errorf("failed to encrypt manifest: %w", err)
	}

	// Upload
	if err := s.storage.Upload(ctx, key, bytes.NewReader(encrypted), storage.UploadOptions{
		ContentType: "application/x-ndjson+zstd",
	}); err != nil {
		return fmt.Errorf("failed to upload manifest: %w", err)
	}

	return nil
}

// LoadManifest loads a complete manifest.
//
// Every entry ends up in memory, so this is for manifests a caller has a
// reason to hold whole. Sync and restore use OpenManifest instead.
func (s *BlobManifestStore) LoadManifest(ctx context.Context, tenantID, workspaceID, manifestID string) (*Manifest, error) {
	entries, err := s.OpenManifest(ctx, tenantID, workspaceID, manifestID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = entries.Close() }()

	manifest := entries.Manifest()
	for {
		entry, err := entries.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		manifest.Files = append(manifest.Files, entry)
	}

	return manifest, nil
}

// StreamManifestFiles returns a channel for streaming manifest files.
// Use for very large manifests (100k+ files).
// The header is returned synchronously, files are streamed via channel.
//
// The channel is closed when the manifest is exhausted or ctx is cancelled. A
// caller that stops reading early must cancel ctx, or the object reader behind
// the channel is never released.
func (s *BlobManifestStore) StreamManifestFiles(ctx context.Context, tenantID, workspaceID, manifestID string) (<-chan ManifestFile, *ManifestHeader, error) {
	entries, err := s.OpenManifest(ctx, tenantID, workspaceID, manifestID)
	if err != nil {
		return nil, nil, err
	}

	header := entries.Header()
	ch := make(chan ManifestFile, 100)

	go func() {
		defer close(ch)
		defer func() { _ = entries.Close() }()

		for {
			entry, err := entries.Next()
			if err != nil {
				return
			}

			select {
			case ch <- entry:
			case <-ctx.Done():
				return
			}
		}
	}()

	return ch, &header, nil
}

// DeleteManifest removes a manifest from storage.
func (s *BlobManifestStore) DeleteManifest(ctx context.Context, tenantID, workspaceID, manifestID string) error {
	key := manifestKey(tenantID, workspaceID, manifestID)
	return s.storage.Delete(ctx, key)
}

// Compile-time interface check.
var _ ManifestStore = (*BlobManifestStore)(nil)
