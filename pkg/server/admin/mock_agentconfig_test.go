package admin

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/chunlea/marionette/pkg/store"
)

// MockAgentConfigService is a mock implementation of AgentConfigService for testing.
type MockAgentConfigService struct {
	mu              sync.RWMutex
	configs         map[string]*store.AgentConfig
	nextID          int
	validationError *ValidationError
	internalError   error
}

// NewMockAgentConfigService creates a new mock agent config service.
func NewMockAgentConfigService() *MockAgentConfigService {
	return &MockAgentConfigService{
		configs: make(map[string]*store.AgentConfig),
	}
}

// SetValidationError sets a validation error to be returned on next update.
func (m *MockAgentConfigService) SetValidationError(field, message string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.validationError = &ValidationError{Field: field, Message: message}
}

// ClearValidationError clears the validation error.
func (m *MockAgentConfigService) ClearValidationError() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.validationError = nil
}

// SetInternalError sets an internal error to be returned on next operation.
func (m *MockAgentConfigService) SetInternalError(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.internalError = err
}

// ClearInternalError clears the internal error.
func (m *MockAgentConfigService) ClearInternalError() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.internalError = nil
}

// Create creates a new agent configuration.
func (m *MockAgentConfigService) Create(_ context.Context, opts CreateAgentConfigOptions) (*store.AgentConfig, error) {
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
	if opts.Agent == "" {
		return nil, &ValidationError{Field: "agent", Message: "agent is required"}
	}
	if opts.APIKey == "" {
		return nil, &ValidationError{Field: "api_key", Message: "api_key is required"}
	}

	// Check for duplicate name
	for _, cfg := range m.configs {
		if cfg.Name == opts.Name {
			return nil, &ValidationError{Field: "name", Message: "name already exists"}
		}
	}

	m.nextID++
	id := "acfg_mock" + string(rune('0'+m.nextID))

	labelsJSON := json.RawMessage("{}")
	annotationsJSON := json.RawMessage("{}")
	extraJSON := json.RawMessage("{}")
	if opts.Labels != nil {
		labelsJSON, _ = json.Marshal(opts.Labels)
	}
	if opts.Extra != nil {
		extraJSON, _ = json.Marshal(opts.Extra)
	}

	config := &store.AgentConfig{
		ID:              id,
		Name:            opts.Name,
		Agent:           opts.Agent,
		APIKeyEncrypted: "encrypted:" + opts.APIKey, // Mock encryption
		Model:           strPtr(opts.Model),
		BaseURL:         strPtr(opts.BaseURL),
		Extra:           extraJSON,
		IsDefault:       opts.IsDefault,
		Labels:          labelsJSON,
		Annotations:     annotationsJSON,
		CreatedAt:       time.Now(),
		UpdatedAt:       time.Now(),
	}

	m.configs[id] = config

	return config, nil
}

// Get retrieves an agent configuration by ID.
func (m *MockAgentConfigService) Get(_ context.Context, id string) (*store.AgentConfig, error) {
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

// List returns agent configurations matching the given options.
func (m *MockAgentConfigService) List(_ context.Context, opts ListAgentConfigsOptions) (*ListResult[store.AgentConfig], error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.internalError != nil {
		err := m.internalError
		m.internalError = nil
		return nil, err
	}

	items := make([]*store.AgentConfig, 0, len(m.configs))
	for _, config := range m.configs {
		if opts.Agent != "" && config.Agent != opts.Agent {
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

	return &ListResult[store.AgentConfig]{
		Items:      items,
		TotalCount: int64(len(m.configs)),
	}, nil
}

// Update updates an agent configuration.
func (m *MockAgentConfigService) Update(_ context.Context, id string, opts UpdateAgentConfigOptions) (*store.AgentConfig, error) {
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
	if opts.APIKey != nil {
		config.APIKeyEncrypted = "encrypted:" + *opts.APIKey
	}
	if opts.Model != nil {
		config.Model = opts.Model
	}
	if opts.BaseURL != nil {
		config.BaseURL = opts.BaseURL
	}
	if opts.Extra != nil {
		extraJSON, _ := json.Marshal(*opts.Extra)
		config.Extra = extraJSON
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

// Delete deletes an agent configuration.
func (m *MockAgentConfigService) Delete(_ context.Context, id string) error {
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
func (m *MockAgentConfigService) AddConfig(config *store.AgentConfig) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.configs[config.ID] = config
}

func strPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
