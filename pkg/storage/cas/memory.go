package cas

import (
	"bytes"
	"context"
	"io"
	"sync"

	"github.com/chunlea/marionette/pkg/storage"
)

// MemoryProvider is an in-memory storage provider for testing.
type MemoryProvider struct {
	objects map[string][]byte
	mu      sync.RWMutex
}

// NewMemoryProvider creates a new in-memory storage provider.
func NewMemoryProvider() *MemoryProvider {
	return &MemoryProvider{
		objects: make(map[string][]byte),
	}
}

// Name returns the provider name.
func (p *MemoryProvider) Name() string {
	return "memory"
}

// Upload writes data to the given key.
func (p *MemoryProvider) Upload(_ context.Context, key string, r io.Reader, _ storage.UploadOptions) error {
	data, err := io.ReadAll(r)
	if err != nil {
		return err
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	// Make a defensive copy
	p.objects[key] = make([]byte, len(data))
	copy(p.objects[key], data)
	return nil
}

// Download returns a reader for the given key.
func (p *MemoryProvider) Download(_ context.Context, key string) (io.ReadCloser, int64, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	data, ok := p.objects[key]
	if !ok {
		return nil, 0, storage.ErrNotFound
	}

	// Make a defensive copy
	buf := make([]byte, len(data))
	copy(buf, data)
	return io.NopCloser(bytes.NewReader(buf)), int64(len(buf)), nil
}

// Delete removes the object at the given key.
func (p *MemoryProvider) Delete(_ context.Context, key string) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	delete(p.objects, key)
	return nil
}

// Exists checks if the object exists.
func (p *MemoryProvider) Exists(_ context.Context, key string) (bool, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	_, ok := p.objects[key]
	return ok, nil
}

// Keys returns all keys in the store (for testing).
func (p *MemoryProvider) Keys() []string {
	p.mu.RLock()
	defer p.mu.RUnlock()

	keys := make([]string, 0, len(p.objects))
	for k := range p.objects {
		keys = append(keys, k)
	}
	return keys
}

// Size returns the total size of all objects in bytes (for testing).
func (p *MemoryProvider) Size() int64 {
	p.mu.RLock()
	defer p.mu.RUnlock()

	var size int64
	for _, data := range p.objects {
		size += int64(len(data))
	}
	return size
}

// Clear removes all objects from the store (for testing).
func (p *MemoryProvider) Clear() {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.objects = make(map[string][]byte)
}

// Compile-time interface check.
var _ storage.StorageProvider = (*MemoryProvider)(nil)
