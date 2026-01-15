package e2b

import (
	"github.com/chunlea/marionette/pkg/provider"
	"github.com/chunlea/marionette/pkg/store"
)

// NewProviderFactory returns a ProviderFactory function for use with the registry.
func NewProviderFactory() provider.ProviderFactory {
	return func(cfg *store.ProviderConfig) (provider.Provider, error) {
		return New(cfg)
	}
}
