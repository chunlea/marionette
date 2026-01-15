package admin

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/chunlea/marionette/pkg/store"
)

// MockProfileService is a mock implementation of ProfileService for testing.
type MockProfileService struct {
	mu              sync.RWMutex
	profiles        map[string]*store.Profile
	nextID          int
	validationError *ValidationError
	internalError   error
}

// SetValidationError sets a validation error to be returned on next update.
func (m *MockProfileService) SetValidationError(field, message string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.validationError = &ValidationError{Field: field, Message: message}
}

// ClearValidationError clears the validation error.
func (m *MockProfileService) ClearValidationError() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.validationError = nil
}

// SetInternalError sets an internal error to be returned on next operation.
func (m *MockProfileService) SetInternalError(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.internalError = err
}

// ClearInternalError clears the internal error.
func (m *MockProfileService) ClearInternalError() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.internalError = nil
}

// NewMockProfileService creates a new mock profile service.
func NewMockProfileService() *MockProfileService {
	return &MockProfileService{
		profiles: make(map[string]*store.Profile),
	}
}

// Create creates a new profile.
func (m *MockProfileService) Create(_ context.Context, opts CreateProfileOptions) (*store.Profile, error) {
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

	// Check for duplicate name
	for _, p := range m.profiles {
		if p.Name == opts.Name {
			return nil, &ValidationError{Field: "name", Message: "name already exists"}
		}
	}

	m.nextID++
	id := "prof_mock" + string(rune('0'+m.nextID))

	labelsJSON := json.RawMessage("{}")
	annotationsJSON := json.RawMessage("{}")
	resourcesJSON := json.RawMessage("{}")
	networkJSON := json.RawMessage("{}")
	tunnelsJSON := json.RawMessage("[]")
	selectorJSON := json.RawMessage("{}")

	if opts.Labels != nil {
		labelsJSON, _ = json.Marshal(opts.Labels)
	}
	if opts.Annotations != nil {
		annotationsJSON, _ = json.Marshal(opts.Annotations)
	}
	if opts.Resources != nil {
		resourcesJSON, _ = json.Marshal(opts.Resources)
	}
	if opts.Network != nil {
		networkJSON, _ = json.Marshal(opts.Network)
	}
	if opts.Tunnels != nil {
		tunnelsJSON, _ = json.Marshal(opts.Tunnels)
	}
	if opts.Selector != nil {
		selectorJSON, _ = json.Marshal(opts.Selector)
	}

	var description *string
	if opts.Description != "" {
		description = &opts.Description
	}
	var providerConfigID *string
	if opts.ProviderConfigID != "" {
		providerConfigID = &opts.ProviderConfigID
	}
	var initScript *string
	if opts.InitScript != "" {
		initScript = &opts.InitScript
	}
	var cleanupScript *string
	if opts.CleanupScript != "" {
		cleanupScript = &opts.CleanupScript
	}

	profile := &store.Profile{
		ID:               id,
		Name:             opts.Name,
		Description:      description,
		ProviderConfigID: providerConfigID,
		Resources:        resourcesJSON,
		Network:          networkJSON,
		InitScript:       initScript,
		CleanupScript:    cleanupScript,
		Tunnels:          tunnelsJSON,
		Selector:         selectorJSON,
		Labels:           labelsJSON,
		Annotations:      annotationsJSON,
		IsBuiltin:        false,
		CreatedAt:        time.Now(),
		UpdatedAt:        time.Now(),
	}

	m.profiles[id] = profile

	return profile, nil
}

// Get retrieves a profile by ID.
func (m *MockProfileService) Get(_ context.Context, id string) (*store.Profile, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.internalError != nil {
		err := m.internalError
		m.internalError = nil
		return nil, err
	}

	profile, ok := m.profiles[id]
	if !ok {
		return nil, store.ErrNotFound
	}

	return profile, nil
}

// List returns profiles matching the given options.
func (m *MockProfileService) List(_ context.Context, opts ListProfilesOptions) (*ListResult[store.Profile], error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.internalError != nil {
		err := m.internalError
		m.internalError = nil
		return nil, err
	}

	items := make([]*store.Profile, 0, len(m.profiles))
	for _, profile := range m.profiles {
		if opts.ProviderConfigID != "" && (profile.ProviderConfigID == nil || *profile.ProviderConfigID != opts.ProviderConfigID) {
			continue
		}
		if !opts.IncludeBuiltin && profile.IsBuiltin {
			continue
		}
		if len(opts.Labels) > 0 {
			if !matchLabelsJSON(profile.Labels, opts.Labels) {
				continue
			}
		}
		items = append(items, profile)
	}

	limit := opts.Limit
	if limit <= 0 {
		limit = 50
	}
	if len(items) > limit {
		items = items[:limit]
	}

	return &ListResult[store.Profile]{
		Items:      items,
		TotalCount: int64(len(m.profiles)),
	}, nil
}

// Update updates a profile.
func (m *MockProfileService) Update(_ context.Context, id string, opts UpdateProfileOptions) (*store.Profile, error) {
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

	profile, ok := m.profiles[id]
	if !ok {
		return nil, store.ErrNotFound
	}

	if opts.Name != nil {
		profile.Name = *opts.Name
	}
	if opts.Description != nil {
		profile.Description = opts.Description
	}
	if opts.ProviderConfigID != nil {
		profile.ProviderConfigID = opts.ProviderConfigID
	}
	if opts.Resources != nil {
		resourcesJSON, _ := json.Marshal(*opts.Resources)
		profile.Resources = resourcesJSON
	}
	if opts.Network != nil {
		networkJSON, _ := json.Marshal(*opts.Network)
		profile.Network = networkJSON
	}
	if opts.InitScript != nil {
		profile.InitScript = opts.InitScript
	}
	if opts.CleanupScript != nil {
		profile.CleanupScript = opts.CleanupScript
	}
	if opts.Tunnels != nil {
		tunnelsJSON, _ := json.Marshal(*opts.Tunnels)
		profile.Tunnels = tunnelsJSON
	}
	if opts.Selector != nil {
		selectorJSON, _ := json.Marshal(*opts.Selector)
		profile.Selector = selectorJSON
	}
	if opts.Labels != nil {
		labelsJSON, _ := json.Marshal(*opts.Labels)
		profile.Labels = labelsJSON
	}
	if opts.Annotations != nil {
		annotationsJSON, _ := json.Marshal(*opts.Annotations)
		profile.Annotations = annotationsJSON
	}

	profile.UpdatedAt = time.Now()

	return profile, nil
}

// Delete deletes a profile.
func (m *MockProfileService) Delete(_ context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.internalError != nil {
		err := m.internalError
		m.internalError = nil
		return err
	}

	if _, ok := m.profiles[id]; !ok {
		return store.ErrNotFound
	}

	delete(m.profiles, id)
	return nil
}

// AddProfile adds a profile to the mock for testing.
func (m *MockProfileService) AddProfile(profile *store.Profile) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.profiles[profile.ID] = profile
}
