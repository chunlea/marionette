package public

import (
	"context"
	"sync"

	"github.com/chunlea/marionette/pkg/store"
)

// MockRunnerService is a mock implementation of RunnerService for testing.
type MockRunnerService struct {
	mu      sync.RWMutex
	runners map[string]*store.Runner

	// Function stubs for custom behavior
	GetFunc  func(ctx context.Context, id string) (*store.Runner, error)
	ListFunc func(ctx context.Context, opts ListRunnersOptions) (*store.ListResult[store.Runner], error)
}

// NewMockRunnerService creates a new MockRunnerService with an empty runner store.
func NewMockRunnerService() *MockRunnerService {
	return &MockRunnerService{
		runners: make(map[string]*store.Runner),
	}
}

// Get retrieves a runner by ID.
func (m *MockRunnerService) Get(ctx context.Context, runnerID string) (*store.Runner, error) {
	if m.GetFunc != nil {
		return m.GetFunc(ctx, runnerID)
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	runner, ok := m.runners[runnerID]
	if !ok {
		return nil, store.ErrNotFound
	}
	return runner, nil
}

// List returns runners matching the filter options.
func (m *MockRunnerService) List(ctx context.Context, opts ListRunnersOptions) (*store.ListResult[store.Runner], error) {
	if m.ListFunc != nil {
		return m.ListFunc(ctx, opts)
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	items := make([]*store.Runner, 0, len(m.runners))
	for _, runner := range m.runners {
		// Apply filters
		if len(opts.Status) > 0 && !contains(opts.Status, runner.Status) {
			continue
		}
		if opts.PoolName != "" {
			if runner.PoolName == nil || *runner.PoolName != opts.PoolName {
				continue
			}
		}
		if opts.Labels != nil && !matchLabels(runner.Labels, opts.Labels) {
			continue
		}
		items = append(items, runner)
	}

	// Apply limit
	limit := opts.Limit
	if limit <= 0 {
		limit = 50
	}
	if len(items) > limit {
		items = items[:limit]
	}

	return &store.ListResult[store.Runner]{
		Items:      items,
		TotalCount: int64(len(items)),
		HasMore:    false,
	}, nil
}

// AddRunner adds a runner directly to the mock store (for testing).
func (m *MockRunnerService) AddRunner(runner *store.Runner) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.runners[runner.ID] = runner
}

// GetAllRunners returns all runners in the mock store (for testing).
func (m *MockRunnerService) GetAllRunners() []*store.Runner {
	m.mu.RLock()
	defer m.mu.RUnlock()

	runners := make([]*store.Runner, 0, len(m.runners))
	for _, r := range m.runners {
		runners = append(runners, r)
	}
	return runners
}

// Reset clears all runners from the mock store.
func (m *MockRunnerService) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.runners = make(map[string]*store.Runner)
}

// Verify MockRunnerService implements RunnerService.
var _ RunnerService = (*MockRunnerService)(nil)
