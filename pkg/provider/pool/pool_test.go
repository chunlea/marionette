package pool

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/chunlea/marionette/pkg/provider"
	"github.com/chunlea/marionette/pkg/store"
)

// mockRunnerStore implements RunnerStore for testing
type mockRunnerStore struct {
	runners     map[string]*store.Runner
	runnerList  []*store.Runner
	listErr     error
	getErr      error
	updateErr   error
	lastUpdates store.RunnerUpdates
}

func newMockRunnerStore() *mockRunnerStore {
	return &mockRunnerStore{
		runners: make(map[string]*store.Runner),
	}
}

func (m *mockRunnerStore) addRunner(r *store.Runner) {
	m.runners[r.ID] = r
	m.runnerList = append(m.runnerList, r)
}

func (m *mockRunnerStore) GetRunner(_ context.Context, id string) (*store.Runner, error) {
	if m.getErr != nil {
		return nil, m.getErr
	}
	r, ok := m.runners[id]
	if !ok {
		return nil, errors.New("runner not found")
	}
	return r, nil
}

func (m *mockRunnerStore) ListRunners(_ context.Context, opts store.ListRunnersOptions) (*store.ListResult[store.Runner], error) {
	if m.listErr != nil {
		return nil, m.listErr
	}

	var filtered []*store.Runner
	for _, r := range m.runnerList {
		// Filter by pool name
		if opts.PoolName != nil && (r.PoolName == nil || *r.PoolName != *opts.PoolName) {
			continue
		}

		// Filter by status
		if len(opts.Status) > 0 {
			found := false
			for _, s := range opts.Status {
				if r.Status == s {
					found = true
					break
				}
			}
			if !found {
				continue
			}
		}

		// Filter by tainted
		if opts.Tainted != nil && r.Tainted != *opts.Tainted {
			continue
		}

		filtered = append(filtered, r)
	}

	return &store.ListResult[store.Runner]{Items: filtered, TotalCount: int64(len(filtered))}, nil
}

func (m *mockRunnerStore) UpdateRunner(_ context.Context, id string, updates store.RunnerUpdates) error {
	if m.updateErr != nil {
		return m.updateErr
	}
	m.lastUpdates = updates

	r, ok := m.runners[id]
	if !ok {
		return errors.New("runner not found")
	}

	if updates.Status != nil {
		r.Status = *updates.Status
	}
	if updates.Tainted != nil {
		r.Tainted = *updates.Tainted
	}
	if updates.TaintReason != nil {
		r.TaintReason = updates.TaintReason
	}

	return nil
}

func TestNew(t *testing.T) {
	ms := newMockRunnerStore()

	cfg := &store.ProviderConfig{
		Name:     "test-pool-provider",
		Provider: "pool",
		Config:   json.RawMessage(`{"pool_name": "test-pool"}`),
	}

	p, err := New(cfg, ms, nil)
	require.NoError(t, err)
	require.NotNil(t, p)

	assert.Equal(t, "test-pool-provider", p.Name())
	assert.Equal(t, provider.ProviderTypePool, p.Type())
}

func TestNew_ValidationError(t *testing.T) {
	ms := newMockRunnerStore()

	cfg := &store.ProviderConfig{
		Name:     "test",
		Provider: "pool",
		Config:   json.RawMessage(`{}`), // missing pool_name
	}

	_, err := New(cfg, ms, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "pool_name")
}

func TestNew_InvalidConfig(t *testing.T) {
	ms := newMockRunnerStore()

	cfg := &store.ProviderConfig{
		Name:     "test",
		Provider: "pool",
		Config:   json.RawMessage(`{invalid`),
	}

	_, err := New(cfg, ms, nil)
	require.Error(t, err)
}

func TestNewWithConfig(t *testing.T) {
	ms := newMockRunnerStore()

	cfg := &Config{
		PoolName:   "test-pool",
		MinRunners: 2,
		MaxRunners: 10,
	}

	p, err := NewWithConfig("test-provider", cfg, nil, ms, nil)
	require.NoError(t, err)
	require.NotNil(t, p)

	assert.Equal(t, "test-provider", p.Name())
	assert.Equal(t, "test-pool", p.Config().PoolName)
}

func TestProvider_Capabilities(t *testing.T) {
	ms := newMockRunnerStore()
	cfg := &Config{PoolName: "test"}
	p, err := NewWithConfig("test", cfg, nil, ms, nil)
	require.NoError(t, err)

	caps := p.Capabilities()

	assert.False(t, caps.Pause, "pool providers don't support pause")
	assert.False(t, caps.Snapshot, "pool providers don't support snapshots")
	assert.Contains(t, caps.Suspend.Strategies, provider.SuspendStrategyReleaseToPool)
	assert.Contains(t, caps.Suspend.Strategies, provider.SuspendStrategyTerminate)
	assert.Equal(t, provider.SuspendStrategyReleaseToPool, caps.Suspend.Default)
}

func TestProvider_Spawn(t *testing.T) {
	ms := newMockRunnerStore()
	cfg := &Config{PoolName: "test"}
	p, err := NewWithConfig("test", cfg, nil, ms, nil)
	require.NoError(t, err)

	_, err = p.Spawn(context.Background(), provider.SpawnOptions{})
	require.Error(t, err)

	var spawnErr *provider.ErrSpawnNotSupported
	assert.ErrorAs(t, err, &spawnErr)
}

func TestProvider_Destroy(t *testing.T) {
	ms := newMockRunnerStore()
	poolName := "test-pool"

	ms.addRunner(&store.Runner{
		ID:       "run_1",
		Status:   "idle",
		PoolName: &poolName,
	})

	cfg := &Config{PoolName: "test-pool"}
	p, err := NewWithConfig("test", cfg, nil, ms, zap.NewNop())
	require.NoError(t, err)

	err = p.Destroy(context.Background(), "run_1")
	require.NoError(t, err)

	// Runner should be marked offline
	assert.Equal(t, "offline", ms.runners["run_1"].Status)
}

func TestProvider_Destroy_WrongPool(t *testing.T) {
	ms := newMockRunnerStore()
	otherPool := "other-pool"

	ms.addRunner(&store.Runner{
		ID:       "run_1",
		PoolName: &otherPool,
	})

	cfg := &Config{PoolName: "test-pool"}
	p, err := NewWithConfig("test", cfg, nil, ms, nil)
	require.NoError(t, err)

	err = p.Destroy(context.Background(), "run_1")
	require.Error(t, err)

	var notFoundErr *provider.ErrRunnerNotFound
	assert.ErrorAs(t, err, &notFoundErr)
}

func TestProvider_Status(t *testing.T) {
	ms := newMockRunnerStore()
	poolName := "test-pool"
	now := time.Now()

	ms.addRunner(&store.Runner{
		ID:        "run_1",
		Status:    "idle",
		PoolName:  &poolName,
		UpdatedAt: now,
	})

	cfg := &Config{PoolName: "test-pool"}
	p, err := NewWithConfig("test", cfg, nil, ms, nil)
	require.NoError(t, err)

	status, err := p.Status(context.Background(), "run_1")
	require.NoError(t, err)
	assert.Equal(t, provider.InstanceStatusRunning, status.Status)
}

func TestProvider_Status_WrongPool(t *testing.T) {
	ms := newMockRunnerStore()
	otherPool := "other-pool"

	ms.addRunner(&store.Runner{
		ID:       "run_1",
		PoolName: &otherPool,
	})

	cfg := &Config{PoolName: "test-pool"}
	p, err := NewWithConfig("test", cfg, nil, ms, nil)
	require.NoError(t, err)

	_, err = p.Status(context.Background(), "run_1")
	require.Error(t, err)
}

func TestProvider_List(t *testing.T) {
	ms := newMockRunnerStore()
	poolName := "test-pool"
	otherPool := "other-pool"

	ms.addRunner(&store.Runner{ID: "run_1", PoolName: &poolName, Status: "idle"})
	ms.addRunner(&store.Runner{ID: "run_2", PoolName: &poolName, Status: "busy"})
	ms.addRunner(&store.Runner{ID: "run_3", PoolName: &otherPool, Status: "idle"})

	cfg := &Config{PoolName: "test-pool"}
	p, err := NewWithConfig("test", cfg, nil, ms, nil)
	require.NoError(t, err)

	instances, err := p.List(context.Background())
	require.NoError(t, err)
	assert.Len(t, instances, 2)
}

func TestProvider_AcquireRunner(t *testing.T) {
	ms := newMockRunnerStore()
	poolName := "test-pool"

	ms.addRunner(&store.Runner{
		ID:       "run_1",
		Name:     "runner-1",
		Status:   "idle",
		PoolName: &poolName,
	})

	cfg := &Config{PoolName: "test-pool"}
	p, err := NewWithConfig("test", cfg, nil, ms, zap.NewNop())
	require.NoError(t, err)

	runner, err := p.AcquireRunner(context.Background(), AcquireOptions{})
	require.NoError(t, err)
	require.NotNil(t, runner)
	assert.Equal(t, "run_1", runner.ID)
	assert.Equal(t, "busy", ms.runners["run_1"].Status)
}

func TestProvider_AcquireRunner_NoIdle(t *testing.T) {
	ms := newMockRunnerStore()
	poolName := "test-pool"

	ms.addRunner(&store.Runner{
		ID:       "run_1",
		Status:   "busy",
		PoolName: &poolName,
	})

	cfg := &Config{PoolName: "test-pool"}
	p, err := NewWithConfig("test", cfg, nil, ms, nil)
	require.NoError(t, err)

	_, err = p.AcquireRunner(context.Background(), AcquireOptions{})
	require.Error(t, err)

	var noIdleErr *ErrNoIdleRunner
	assert.ErrorAs(t, err, &noIdleErr)
}

func TestProvider_AcquireRunner_WithLabels(t *testing.T) {
	ms := newMockRunnerStore()
	poolName := "test-pool"
	labels, _ := json.Marshal(map[string]string{"gpu": "nvidia"})

	ms.addRunner(&store.Runner{
		ID:       "run_1",
		Status:   "idle",
		PoolName: &poolName,
		Labels:   labels,
	})
	ms.addRunner(&store.Runner{
		ID:       "run_2",
		Status:   "idle",
		PoolName: &poolName,
	})

	cfg := &Config{PoolName: "test-pool", RequiredLabels: map[string]string{"gpu": "nvidia"}}
	p, err := NewWithConfig("test", cfg, nil, ms, zap.NewNop())
	require.NoError(t, err)

	runner, err := p.AcquireRunner(context.Background(), AcquireOptions{})
	require.NoError(t, err)
	assert.Equal(t, "run_1", runner.ID)
}

func TestProvider_ReleaseRunner(t *testing.T) {
	ms := newMockRunnerStore()
	poolName := "test-pool"

	ms.addRunner(&store.Runner{
		ID:       "run_1",
		Status:   "busy",
		PoolName: &poolName,
	})

	cfg := &Config{PoolName: "test-pool"}
	p, err := NewWithConfig("test", cfg, nil, ms, zap.NewNop())
	require.NoError(t, err)

	err = p.ReleaseRunner(context.Background(), "run_1", false, "")
	require.NoError(t, err)
	assert.Equal(t, "idle", ms.runners["run_1"].Status)
}

func TestProvider_ReleaseRunner_Tainted(t *testing.T) {
	ms := newMockRunnerStore()
	poolName := "test-pool"

	ms.addRunner(&store.Runner{
		ID:       "run_1",
		Status:   "busy",
		PoolName: &poolName,
	})

	cfg := &Config{PoolName: "test-pool"}
	p, err := NewWithConfig("test", cfg, nil, ms, zap.NewNop())
	require.NoError(t, err)

	err = p.ReleaseRunner(context.Background(), "run_1", true, "test reason")
	require.NoError(t, err)
	assert.Equal(t, "offline", ms.runners["run_1"].Status)
	assert.True(t, ms.runners["run_1"].Tainted)
}

func TestProvider_ReleaseRunner_MaxTasks(t *testing.T) {
	ms := newMockRunnerStore()
	poolName := "test-pool"

	ms.addRunner(&store.Runner{
		ID:       "run_1",
		Status:   "busy",
		PoolName: &poolName,
	})

	cfg := &Config{PoolName: "test-pool", MaxTasksPerRunner: 2}
	p, err := NewWithConfig("test", cfg, nil, ms, zap.NewNop())
	require.NoError(t, err)

	// First release
	err = p.ReleaseRunner(context.Background(), "run_1", false, "")
	require.NoError(t, err)
	assert.Equal(t, "idle", ms.runners["run_1"].Status)
	assert.False(t, ms.runners["run_1"].Tainted)

	// Reset status for second release
	ms.runners["run_1"].Status = "busy"

	// Second release - should hit max tasks and taint
	err = p.ReleaseRunner(context.Background(), "run_1", false, "")
	require.NoError(t, err)
	assert.Equal(t, "offline", ms.runners["run_1"].Status)
	assert.True(t, ms.runners["run_1"].Tainted)
}

func TestProvider_PoolStats(t *testing.T) {
	ms := newMockRunnerStore()
	poolName := "test-pool"

	ms.addRunner(&store.Runner{ID: "run_1", Status: "idle", PoolName: &poolName})
	ms.addRunner(&store.Runner{ID: "run_2", Status: "busy", PoolName: &poolName})
	ms.addRunner(&store.Runner{ID: "run_3", Status: "offline", PoolName: &poolName})
	ms.addRunner(&store.Runner{ID: "run_4", Status: "idle", PoolName: &poolName, Tainted: true})

	cfg := &Config{PoolName: "test-pool"}
	p, err := NewWithConfig("test", cfg, nil, ms, nil)
	require.NoError(t, err)

	stats, err := p.PoolStats(context.Background())
	require.NoError(t, err)

	assert.Equal(t, "test-pool", stats.PoolName)
	assert.Equal(t, 4, stats.Total)
	assert.Equal(t, 2, stats.Idle)
	assert.Equal(t, 1, stats.Busy)
	assert.Equal(t, 1, stats.Offline)
	assert.Equal(t, 1, stats.Tainted)
}

func TestMapRunnerStatus(t *testing.T) {
	tests := []struct {
		status string
		want   provider.InstanceStatus
	}{
		{"idle", provider.InstanceStatusRunning},
		{"busy", provider.InstanceStatusRunning},
		{"paused", provider.InstanceStatusPaused},
		{"offline", provider.InstanceStatusStopped},
		{"unknown", provider.InstanceStatusFailed},
	}

	for _, tt := range tests {
		t.Run(tt.status, func(t *testing.T) {
			got := mapRunnerStatus(tt.status)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestRunnerToInstance(t *testing.T) {
	poolName := "test-pool"
	labels, _ := json.Marshal(map[string]string{"env": "prod"})
	annotations, _ := json.Marshal(map[string]string{"note": "test"})
	taintReason := "test reason"
	now := time.Now()

	runner := &store.Runner{
		ID:          "run_123",
		Name:        "test-runner",
		Status:      "idle",
		SandboxMode: "runner-is-sandbox",
		PoolName:    &poolName,
		Labels:      labels,
		Annotations: annotations,
		Tainted:     true,
		TaintReason: &taintReason,
		CreatedAt:   now,
	}

	instance := runnerToInstance(runner)

	assert.Equal(t, "run_123", instance.ID)
	assert.Equal(t, "test-runner", instance.Name)
	assert.Equal(t, provider.InstanceStatusRunning, instance.Status)
	assert.Equal(t, "runner-is-sandbox", instance.SandboxMode)
	assert.Equal(t, "prod", instance.Labels["env"])
	assert.Equal(t, "test", instance.Annotations["note"])
	assert.Equal(t, "test-pool", instance.Metadata["pool_name"])
	assert.Equal(t, "true", instance.Metadata["tainted"])
	assert.Equal(t, "test reason", instance.Metadata["taint_reason"])
}

func TestErrNoIdleRunner(t *testing.T) {
	err := &ErrNoIdleRunner{PoolName: "test-pool"}
	assert.Contains(t, err.Error(), "test-pool")
	assert.Contains(t, err.Error(), "no idle runner")
}
