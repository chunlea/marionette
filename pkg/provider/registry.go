package provider

import (
	"context"
	"sync"

	"github.com/chunlea/marionette/pkg/store"
)

// ProviderFactory creates a provider from a ProviderConfig.
type ProviderFactory func(cfg *store.ProviderConfig) (Provider, error)

// ProviderConfigStore is the subset of store.Store needed by the Registry.
type ProviderConfigStore interface {
	GetProviderConfigByName(ctx context.Context, name string) (*store.ProviderConfig, error)
}

// Registry manages provider instances and factories.
type Registry struct {
	mu        sync.RWMutex
	providers map[string]Provider        // name -> Provider
	factories map[string]ProviderFactory // provider type -> factory
	store     ProviderConfigStore
	default_  string
}

// NewRegistry creates a provider registry.
// The store is used to load provider configurations from the database.
func NewRegistry(s store.Store) *Registry {
	return &Registry{
		providers: make(map[string]Provider),
		factories: make(map[string]ProviderFactory),
		store:     s,
	}
}

// NewRegistryWithStore creates a registry with a custom store implementation.
// This is useful for testing or when only a subset of store operations is needed.
func NewRegistryWithStore(s ProviderConfigStore) *Registry {
	return &Registry{
		providers: make(map[string]Provider),
		factories: make(map[string]ProviderFactory),
		store:     s,
	}
}

// RegisterFactory registers a factory function for a provider type.
// The provider type is the value stored in provider_configs.provider (e.g., "docker", "kubernetes").
func (r *Registry) RegisterFactory(providerType string, factory ProviderFactory) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.factories[providerType] = factory
}

// Register adds a pre-created provider to the registry.
// This is useful for providers loaded from configuration files.
func (r *Registry) Register(name string, p Provider) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.providers[name] = p
	return nil
}

// Unregister removes a provider from the registry.
func (r *Registry) Unregister(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.providers, name)
}

// Get retrieves a provider by name.
// If the provider is not already loaded, it attempts to load it from the database.
func (r *Registry) Get(ctx context.Context, name string) (Provider, error) {
	r.mu.RLock()
	p, ok := r.providers[name]
	r.mu.RUnlock()

	if ok {
		return p, nil
	}

	// Try to load from database
	return r.loadFromDB(ctx, name)
}

// GetDefault retrieves the default provider.
func (r *Registry) GetDefault(ctx context.Context) (Provider, error) {
	r.mu.RLock()
	defaultName := r.default_
	r.mu.RUnlock()

	if defaultName == "" {
		return nil, &ErrProviderNotFound{Name: "default"}
	}

	return r.Get(ctx, defaultName)
}

// SetDefault sets the default provider name.
func (r *Registry) SetDefault(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.default_ = name
}

// DefaultName returns the current default provider name.
func (r *Registry) DefaultName() string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.default_
}

// List returns all registered provider names.
func (r *Registry) List() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	names := make([]string, 0, len(r.providers))
	for name := range r.providers {
		names = append(names, name)
	}
	return names
}

// Has checks if a provider is registered.
func (r *Registry) Has(name string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, ok := r.providers[name]
	return ok
}

// Close closes all registered providers that implement io.Closer.
func (r *Registry) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	var lastErr error
	for name, p := range r.providers {
		if closer, ok := p.(interface{ Close() error }); ok {
			if err := closer.Close(); err != nil {
				lastErr = err
			}
		}
		delete(r.providers, name)
	}

	return lastErr
}

// loadFromDB loads a provider from the database by name.
func (r *Registry) loadFromDB(ctx context.Context, name string) (Provider, error) {
	if r.store == nil {
		return nil, &ErrProviderNotFound{Name: name}
	}

	// Load config from database
	cfg, err := r.store.GetProviderConfigByName(ctx, name)
	if err != nil {
		return nil, &ErrProviderNotFound{Name: name}
	}

	// Get factory for this provider type
	r.mu.RLock()
	factory, ok := r.factories[cfg.Provider]
	r.mu.RUnlock()

	if !ok {
		return nil, &ErrProviderNotFound{Name: name}
	}

	// Create provider
	p, err := factory(cfg)
	if err != nil {
		return nil, err
	}

	// Cache for future use
	r.mu.Lock()
	r.providers[name] = p
	r.mu.Unlock()

	return p, nil
}

// CreateFromConfig creates a provider from a ProviderConfig without registering it.
// This is useful for one-off operations or testing.
func (r *Registry) CreateFromConfig(cfg *store.ProviderConfig) (Provider, error) {
	r.mu.RLock()
	factory, ok := r.factories[cfg.Provider]
	r.mu.RUnlock()

	if !ok {
		return nil, &ErrProviderNotFound{Name: cfg.Provider}
	}

	return factory(cfg)
}

// GetFactoryTypes returns all registered factory types.
func (r *Registry) GetFactoryTypes() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	types := make([]string, 0, len(r.factories))
	for t := range r.factories {
		types = append(types, t)
	}
	return types
}
