// Package storage provides interfaces and implementations for blob storage.
package storage

import (
	"context"
	"io"
)

// StorageProvider defines the interface for blob storage backends.
type StorageProvider interface {
	// Name returns the provider name (e.g., "local", "s3", "memory").
	Name() string

	// Upload writes data from reader to the given key.
	Upload(ctx context.Context, key string, r io.Reader, opts UploadOptions) error

	// Download returns a reader for the given key.
	// Returns the reader, size in bytes, and any error.
	// Caller must close the reader.
	Download(ctx context.Context, key string) (io.ReadCloser, int64, error)

	// Delete removes the object at the given key.
	// Returns nil if the object does not exist (idempotent).
	Delete(ctx context.Context, key string) error

	// Exists checks if the object exists.
	Exists(ctx context.Context, key string) (bool, error)
}

// UploadOptions contains options for upload operations.
type UploadOptions struct {
	// ContentType is the MIME type of the content.
	ContentType string

	// Metadata contains custom metadata to store with the object.
	Metadata map[string]string
}
