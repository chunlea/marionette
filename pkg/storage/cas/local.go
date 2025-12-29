package cas

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/chunlea/marionette/pkg/storage"
)

// LocalProvider stores blobs on the local filesystem.
type LocalProvider struct {
	basePath string
}

// NewLocalProvider creates a new local storage provider.
// The basePath directory will be created if it doesn't exist.
func NewLocalProvider(basePath string) (*LocalProvider, error) {
	// Ensure base directory exists
	if err := os.MkdirAll(basePath, 0755); err != nil {
		return nil, fmt.Errorf("failed to create storage directory: %w", err)
	}
	return &LocalProvider{basePath: basePath}, nil
}

// Name returns the provider name.
func (p *LocalProvider) Name() string {
	return "local"
}

// Upload writes data to the given key.
// Uses atomic write (temp file + rename) to prevent partial writes.
func (p *LocalProvider) Upload(_ context.Context, key string, r io.Reader, _ storage.UploadOptions) error {
	fullPath := filepath.Join(p.basePath, key)

	// Validate path doesn't escape base directory
	if !p.isValidPath(fullPath) {
		return storage.ErrInvalidKey
	}

	// Create parent directories
	if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
		return fmt.Errorf("failed to create parent directories: %w", err)
	}

	// Write to temp file then rename (atomic)
	tmpPath := fullPath + ".tmp"
	f, err := os.Create(tmpPath)
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}

	_, err = io.Copy(f, r)
	closeErr := f.Close()

	if err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("failed to write data: %w", err)
	}
	if closeErr != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("failed to close temp file: %w", closeErr)
	}

	// Atomic rename
	if err := os.Rename(tmpPath, fullPath); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("failed to rename temp file: %w", err)
	}

	return nil
}

// Download returns a reader for the given key.
// Caller must close the returned reader.
func (p *LocalProvider) Download(_ context.Context, key string) (io.ReadCloser, int64, error) {
	fullPath := filepath.Join(p.basePath, key)

	// Validate path doesn't escape base directory
	if !p.isValidPath(fullPath) {
		return nil, 0, storage.ErrInvalidKey
	}

	info, err := os.Stat(fullPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, 0, storage.ErrNotFound
		}
		return nil, 0, fmt.Errorf("failed to stat file: %w", err)
	}

	f, err := os.Open(fullPath)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to open file: %w", err)
	}

	return f, info.Size(), nil
}

// Delete removes the object at the given key.
// Returns nil if the object doesn't exist (idempotent).
func (p *LocalProvider) Delete(_ context.Context, key string) error {
	fullPath := filepath.Join(p.basePath, key)

	// Validate path doesn't escape base directory
	if !p.isValidPath(fullPath) {
		return storage.ErrInvalidKey
	}

	err := os.Remove(fullPath)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to delete file: %w", err)
	}
	return nil
}

// Exists checks if the object exists.
func (p *LocalProvider) Exists(_ context.Context, key string) (bool, error) {
	fullPath := filepath.Join(p.basePath, key)

	// Validate path doesn't escape base directory
	if !p.isValidPath(fullPath) {
		return false, storage.ErrInvalidKey
	}

	_, err := os.Stat(fullPath)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("failed to stat file: %w", err)
	}
	return true, nil
}

// isValidPath checks that the path doesn't escape the base directory.
func (p *LocalProvider) isValidPath(fullPath string) bool {
	// Clean the path and check it starts with the base path
	cleanPath := filepath.Clean(fullPath)
	cleanBase := filepath.Clean(p.basePath)
	return len(cleanPath) >= len(cleanBase) && cleanPath[:len(cleanBase)] == cleanBase
}

// Compile-time interface check.
var _ storage.StorageProvider = (*LocalProvider)(nil)
