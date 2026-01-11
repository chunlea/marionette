package streaming

import (
	"context"
	"slices"
)

// StreamProvider defines the interface for stream providers.
// Each provider implements support for one or more stream types
// (desktop, browser, iOS, Android).
type StreamProvider interface {
	// Name returns the unique name of this provider.
	Name() string

	// SupportedTypes returns the stream types this provider supports.
	SupportedTypes() []StreamType

	// Start initiates a stream with the given options.
	// It returns stream info including the signaling URL for WebRTC.
	Start(ctx context.Context, opts StreamOptions) (*StreamInfo, error)

	// Stop stops the stream with the given provider stream ID.
	Stop(ctx context.Context, providerStreamID string) error

	// GetInfo returns the current info for a stream.
	GetInfo(ctx context.Context, providerStreamID string) (*StreamInfo, error)
}

// ProviderRegistry manages stream providers.
type ProviderRegistry struct {
	providers map[string]StreamProvider
}

// NewProviderRegistry creates a new provider registry.
func NewProviderRegistry() *ProviderRegistry {
	return &ProviderRegistry{
		providers: make(map[string]StreamProvider),
	}
}

// Register adds a provider to the registry.
func (r *ProviderRegistry) Register(provider StreamProvider) error {
	if provider == nil {
		return ErrProviderNotFound
	}
	name := provider.Name()
	if _, exists := r.providers[name]; exists {
		return ErrProviderExists
	}
	r.providers[name] = provider
	return nil
}

// Get returns a provider by name.
func (r *ProviderRegistry) Get(name string) (StreamProvider, bool) {
	provider, ok := r.providers[name]
	return provider, ok
}

// GetForType returns a provider that supports the given stream type.
// If multiple providers support the type, the first one registered is returned.
func (r *ProviderRegistry) GetForType(streamType StreamType) (StreamProvider, error) {
	for _, provider := range r.providers {
		if slices.Contains(provider.SupportedTypes(), streamType) {
			return provider, nil
		}
	}
	return nil, ErrNoProviderForType
}

// List returns all registered providers.
func (r *ProviderRegistry) List() []StreamProvider {
	providers := make([]StreamProvider, 0, len(r.providers))
	for _, p := range r.providers {
		providers = append(providers, p)
	}
	return providers
}

// Unregister removes a provider from the registry.
func (r *ProviderRegistry) Unregister(name string) bool {
	if _, exists := r.providers[name]; !exists {
		return false
	}
	delete(r.providers, name)
	return true
}
