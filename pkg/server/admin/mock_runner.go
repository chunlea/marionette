package admin

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/chunlea/marionette/pkg/store"
)

// MockRunnerAdminService is a mock implementation of RunnerAdminService for testing.
type MockRunnerAdminService struct {
	mu              sync.RWMutex
	runners         map[string]*store.Runner
	nextID          int
	validationError *ValidationError
	internalError   error
}

// NewMockRunnerAdminService creates a new mock runner admin service.
func NewMockRunnerAdminService() *MockRunnerAdminService {
	return &MockRunnerAdminService{
		runners: make(map[string]*store.Runner),
	}
}

// SetValidationError sets a validation error to be returned on next operation.
func (m *MockRunnerAdminService) SetValidationError(field, message string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.validationError = &ValidationError{Field: field, Message: message}
}

// ClearValidationError clears the validation error.
func (m *MockRunnerAdminService) ClearValidationError() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.validationError = nil
}

// SetInternalError sets an internal error to be returned on next operation.
func (m *MockRunnerAdminService) SetInternalError(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.internalError = err
}

// ClearInternalError clears the internal error.
func (m *MockRunnerAdminService) ClearInternalError() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.internalError = nil
}

// Spawn creates a new runner using the specified provider.
func (m *MockRunnerAdminService) Spawn(_ context.Context, opts SpawnRunnerOptions) (*store.Runner, error) {
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

	m.nextID++
	id := "run_mock" + string(rune('0'+m.nextID))

	name := opts.Name
	if name == "" {
		name = "runner-" + id
	}

	labelsJSON := json.RawMessage("{}")
	annotationsJSON := json.RawMessage("{}")
	if opts.Labels != nil {
		labelsJSON, _ = json.Marshal(opts.Labels)
	}

	runner := &store.Runner{
		ID:               id,
		Name:             name,
		Hostname:         "mock-host-" + id,
		Status:           "idle",
		SandboxMode:      "runner-is-sandbox",
		ProviderConfigID: strPtr(opts.ProviderConfigID),
		ProfileID:        strPtr(opts.ProfileID),
		Labels:           labelsJSON,
		Annotations:      annotationsJSON,
		CreatedAt:        time.Now(),
		UpdatedAt:        time.Now(),
	}

	m.runners[id] = runner

	return runner, nil
}

// Destroy terminates and removes a runner.
func (m *MockRunnerAdminService) Destroy(_ context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.internalError != nil {
		err := m.internalError
		m.internalError = nil
		return err
	}

	runner, ok := m.runners[id]
	if !ok {
		return store.ErrNotFound
	}

	// Check if runner can be destroyed (not busy)
	if runner.Status == "busy" {
		return &InvalidStateError{
			Resource: "runner",
			ID:       id,
			Current:  "busy",
			Expected: "idle or offline",
		}
	}

	delete(m.runners, id)
	return nil
}

// Get retrieves a runner by ID.
func (m *MockRunnerAdminService) Get(_ context.Context, id string) (*store.Runner, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.internalError != nil {
		err := m.internalError
		m.internalError = nil
		return nil, err
	}

	runner, ok := m.runners[id]
	if !ok {
		return nil, store.ErrNotFound
	}

	return runner, nil
}

// List returns runners matching the given options.
func (m *MockRunnerAdminService) List(_ context.Context, opts ListRunnersOptions) (*ListResult[store.Runner], error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.internalError != nil {
		err := m.internalError
		m.internalError = nil
		return nil, err
	}

	items := make([]*store.Runner, 0, len(m.runners))
	for _, runner := range m.runners {
		if len(opts.Status) > 0 {
			found := false
			for _, s := range opts.Status {
				if runner.Status == s {
					found = true
					break
				}
			}
			if !found {
				continue
			}
		}
		if opts.PoolName != "" && (runner.PoolName == nil || *runner.PoolName != opts.PoolName) {
			continue
		}
		if len(opts.Labels) > 0 {
			if !matchLabelsJSON(runner.Labels, opts.Labels) {
				continue
			}
		}
		items = append(items, runner)
	}

	limit := opts.Limit
	if limit <= 0 {
		limit = 50
	}
	if len(items) > limit {
		items = items[:limit]
	}

	return &ListResult[store.Runner]{
		Items:      items,
		TotalCount: int64(len(m.runners)),
	}, nil
}

// AddRunner adds a runner to the mock for testing.
func (m *MockRunnerAdminService) AddRunner(runner *store.Runner) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.runners[runner.ID] = runner
}

// InvalidStateError represents an invalid state transition error.
type InvalidStateError struct {
	Resource string
	ID       string
	Current  string
	Expected string
}

func (e *InvalidStateError) Error() string {
	return e.Resource + " " + e.ID + " is in state " + e.Current + ", expected " + e.Expected
}

// IsInvalidState returns true if the error is an InvalidStateError.
func IsInvalidState(err error) bool {
	_, ok := err.(*InvalidStateError)
	return ok
}
