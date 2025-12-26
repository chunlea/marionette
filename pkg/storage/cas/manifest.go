package cas

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
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
}

// NewBlobManifestStore creates a new manifest store.
func NewBlobManifestStore(storageProvider storage.StorageProvider, encryptor Encryptor) *BlobManifestStore {
	return &BlobManifestStore{
		storage:   storageProvider,
		encryptor: encryptor,
	}
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
func (s *BlobManifestStore) LoadManifest(ctx context.Context, tenantID, workspaceID, manifestID string) (*Manifest, error) {
	key := manifestKey(tenantID, workspaceID, manifestID)

	// Download
	reader, _, err := s.storage.Download(ctx, key)
	if err != nil {
		if err == storage.ErrNotFound {
			return nil, ErrManifestNotFound
		}
		return nil, fmt.Errorf("failed to download manifest: %w", err)
	}
	defer func() { _ = reader.Close() }()

	// Read encrypted data
	encrypted, err := io.ReadAll(reader)
	if err != nil {
		return nil, fmt.Errorf("failed to read manifest: %w", err)
	}

	// Decrypt
	compressed, err := s.encryptor.Decrypt(ctx, tenantID, encrypted)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt manifest: %w", err)
	}

	// Decompress
	decoder, err := zstd.NewReader(nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create zstd decoder: %w", err)
	}
	defer decoder.Close()

	data, err := decoder.DecodeAll(compressed, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to decompress manifest: %w", err)
	}

	// Parse JSONL
	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(make([]byte, 64*1024), 1024*1024) // 1MB max line

	// Read header (first line)
	if !scanner.Scan() {
		return nil, ErrInvalidManifest
	}

	var header ManifestHeader
	if err := json.Unmarshal(scanner.Bytes(), &header); err != nil {
		return nil, fmt.Errorf("failed to parse manifest header: %w", err)
	}

	manifest := FromHeader(header)

	// Read file entries
	for scanner.Scan() {
		var file ManifestFile
		if err := json.Unmarshal(scanner.Bytes(), &file); err != nil {
			return nil, fmt.Errorf("failed to parse manifest file entry: %w", err)
		}
		manifest.Files = append(manifest.Files, file)
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("failed to scan manifest: %w", err)
	}

	return manifest, nil
}

// StreamManifestFiles returns a channel for streaming manifest files.
// Use for very large manifests (100k+ files).
// The header is returned synchronously, files are streamed via channel.
func (s *BlobManifestStore) StreamManifestFiles(ctx context.Context, tenantID, workspaceID, manifestID string) (<-chan ManifestFile, *ManifestHeader, error) {
	key := manifestKey(tenantID, workspaceID, manifestID)

	// Download
	reader, _, err := s.storage.Download(ctx, key)
	if err != nil {
		if err == storage.ErrNotFound {
			return nil, nil, ErrManifestNotFound
		}
		return nil, nil, fmt.Errorf("failed to download manifest: %w", err)
	}

	// Read encrypted data (we need all of it for AES-GCM decryption)
	encrypted, err := io.ReadAll(reader)
	_ = reader.Close()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to read manifest: %w", err)
	}

	// Decrypt
	compressed, err := s.encryptor.Decrypt(ctx, tenantID, encrypted)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to decrypt manifest: %w", err)
	}

	// Decompress
	decoder, err := zstd.NewReader(nil)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create zstd decoder: %w", err)
	}

	data, err := decoder.DecodeAll(compressed, nil)
	decoder.Close()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to decompress manifest: %w", err)
	}

	// Parse header synchronously
	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)

	if !scanner.Scan() {
		return nil, nil, ErrInvalidManifest
	}

	var header ManifestHeader
	if err := json.Unmarshal(scanner.Bytes(), &header); err != nil {
		return nil, nil, fmt.Errorf("failed to parse manifest header: %w", err)
	}

	// Stream file entries via channel
	ch := make(chan ManifestFile, 100)

	go func() {
		defer close(ch)

		for scanner.Scan() {
			var file ManifestFile
			if err := json.Unmarshal(scanner.Bytes(), &file); err != nil {
				continue // Skip invalid entries
			}

			select {
			case ch <- file:
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
