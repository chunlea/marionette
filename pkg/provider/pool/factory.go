package pool

import (
	"go.uber.org/zap"

	"github.com/chunlea/marionette/pkg/provider"
	"github.com/chunlea/marionette/pkg/store"
)

// Factory creates pool providers from store configurations.
type Factory struct {
	store  store.Store
	logger *zap.Logger
}

// NewFactory creates a new pool provider factory.
func NewFactory(st store.Store, logger *zap.Logger) *Factory {
	return &Factory{
		store:  st,
		logger: logger,
	}
}

// Create creates a pool provider from a ProviderConfig.
func (f *Factory) Create(cfg *store.ProviderConfig) (provider.Provider, error) {
	return New(cfg, f.store, f.logger)
}

// NewProviderFactory returns a ProviderFactory function for use with the registry.
// This function captures the store and logger dependencies.
func NewProviderFactory(st store.Store, logger *zap.Logger) provider.ProviderFactory {
	return func(cfg *store.ProviderConfig) (provider.Provider, error) {
		return New(cfg, st, logger)
	}
}
