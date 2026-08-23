package admin

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/chunlea/marionette/pkg/store"
)

// MockProviderConfigService is a mock implementation of ProviderConfigService for testing.
type MockProviderConfigService struct {
	mu              sync.RWMutex
	configs         map[string]*store.ProviderConfig
	nextID          int
	validationError *ValidationError
	internalError   error
}

// SetValidationError sets a validation error to be returned on next update.
func (m *MockProviderConfigService) SetValidationError(field, message string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.validationError = &ValidationError{Field: field, Message: message}
}

// ClearValidationError clears the validation error.
func (m *MockProviderConfigService) ClearValidationError() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.validationError = nil
}

// SetInternalError sets an internal error to be returned on next operation.
func (m *MockProviderConfigService) SetInternalError(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.internalError = err
}

// ClearInternalError clears the internal error.
func (m *MockProviderConfigService) ClearInternalError() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.internalError = nil
}

// NewMockProviderConfigService creates a new mock provider config service.
func NewMockProviderConfigService() *MockProviderConfigService {
	return &MockProviderConfigService{
		configs: make(map[string]*store.ProviderConfig),
	}
}

// Create creates a new provider configuration.
func (m *MockProviderConfigService) Create(_ context.Context, opts CreateProviderConfigOptions) (*store.ProviderConfig, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.internalError != nil {
		err := m.internalError
		m.internalError = nil
		return nil, err
	}

	if opts.Name == "" {
		return nil, &ValidationError{Field: "name", Message: "name is required"}
	}
	if opts.Provider == "" {
		return nil, &ValidationError{Field: "provider", Message: "provider is required"}
	}

	// Check for duplicate name
	for _, cfg := range m.configs {
		if cfg.Name == opts.Name {
			return nil, &ValidationError{Field: "name", Message: "name already exists"}
		}
	}

	m.nextID++
	id := "pcfg_mock" + string(rune('0'+m.nextID))

	labelsJSON := json.RawMessage("{}")
	annotationsJSON := json.RawMessage("{}")
	configJSON := json.RawMessage("{}")
	suspendConfigJSON := json.RawMessage(`{"strategy":"terminate"}`)

	if opts.Labels != nil {
		labelsJSON, _ = json.Marshal(opts.Labels)
	}
	if opts.Config != nil {
		configJSON, _ = json.Marshal(opts.Config)
	}
	if opts.SuspendConfig != nil {
		suspendConfigJSON, _ = json.Marshal(opts.SuspendConfig)
	}

	config := &store.ProviderConfig{
		ID:            id,
		Name:          opts.Name,
		Provider:      opts.Provider,
		Config:        configJSON,
		SuspendConfig: suspendConfigJSON,
		IsDefault:     opts.IsDefault,
		Labels:        labelsJSON,
		Annotations:   annotationsJSON,
		CreatedAt:     time.Now(),
		UpdatedAt:     time.Now(),
	}

	m.configs[id] = config

	return config, nil
}

// Get retrieves a provider configuration by ID.
func (m *MockProviderConfigService) Get(_ context.Context, id string) (*store.ProviderConfig, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.internalError != nil {
		err := m.internalError
		m.internalError = nil
		return nil, err
	}

	config, ok := m.configs[id]
	if !ok {
		return nil, store.ErrNotFound
	}

	return config, nil
}

// List returns provider configurations matching the given options.
func (m *MockProviderConfigService) List(_ context.Context, opts ListProviderConfigsOptions) (*ListResult[store.ProviderConfig], error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.internalError != nil {
		err := m.internalError
		m.internalError = nil
		return nil, err
	}

	items := make([]*store.ProviderConfig, 0, len(m.configs))
	for _, config := range m.configs {
		if opts.Provider != "" && config.Provider != opts.Provider {
			continue
		}
		if len(opts.Labels) > 0 {
			if !matchLabelsJSON(config.Labels, opts.Labels) {
				continue
			}
		}
		items = append(items, config)
	}

	limit := opts.Limit
	if limit <= 0 {
		limit = 50
	}
	if len(items) > limit {
		items = items[:limit]
	}

	return &ListResult[store.ProviderConfig]{
		Items:      items,
		TotalCount: int64(len(m.configs)),
	}, nil
}

// Update updates a provider configuration.
func (m *MockProviderConfigService) Update(_ context.Context, id string, opts UpdateProviderConfigOptions) (*store.ProviderConfig, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.internalError != nil {
		err := m.internalError
		m.internalError = nil
		return nil, err
	}

	if m.validationError != nil {
		err := m.validationError
		m.validationError = nil
		return nil, err
	}

	config, ok := m.configs[id]
	if !ok {
		return nil, store.ErrNotFound
	}

	if opts.Name != nil {
		config.Name = *opts.Name
	}
	if opts.Config != nil {
		configJSON, _ := json.Marshal(*opts.Config)
		config.Config = configJSON
	}
	if opts.SuspendConfig != nil {
		suspendConfigJSON, _ := json.Marshal(*opts.SuspendConfig)
		config.SuspendConfig = suspendConfigJSON
	}
	if opts.IsDefault != nil {
		config.IsDefault = *opts.IsDefault
	}
	if opts.Labels != nil {
		labelsJSON, _ := json.Marshal(*opts.Labels)
		config.Labels = labelsJSON
	}

	config.UpdatedAt = time.Now()

	return config, nil
}

// Delete deletes a provider configuration.
func (m *MockProviderConfigService) Delete(_ context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.internalError != nil {
		err := m.internalError
		m.internalError = nil
		return err
	}

	if _, ok := m.configs[id]; !ok {
		return store.ErrNotFound
	}

	delete(m.configs, id)
	return nil
}

// AddConfig adds a config to the mock for testing.
func (m *MockProviderConfigService) AddConfig(config *store.ProviderConfig) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.configs[config.ID] = config
}
