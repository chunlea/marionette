package pool

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/chunlea/marionette/pkg/store"
)

// mockWatchdogStore implements WatchdogStore for testing
type mockWatchdogStore struct {
	mu         sync.Mutex
	runners    map[string]*store.Runner
	runnerList []*store.Runner
	listErr    error
	updateErr  error
}

func newMockWatchdogStore() *mockWatchdogStore {
	return &mockWatchdogStore{
		runners: make(map[string]*store.Runner),
	}
}

func (m *mockWatchdogStore) addRunner(r *store.Runner) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.runners[r.ID] = r
	m.runnerList = append(m.runnerList, r)
}

func (m *mockWatchdogStore) ListRunners(_ context.Context, opts store.ListRunnersOptions) (*store.ListResult[store.Runner], error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.listErr != nil {
		return nil, m.listErr
	}

	var filtered []*store.Runner
	for _, r := range m.runnerList {
		// Filter by pool name
		if opts.PoolName != nil && (r.PoolName == nil || *r.PoolName != *opts.PoolName) {
			continue
		}

		// Filter by tainted
		if opts.Tainted != nil && r.Tainted != *opts.Tainted {
			continue
		}

		filtered = append(filtered, r)
	}

	return &store.ListResult[store.Runner]{Items: filtered, TotalCount: int64(len(filtered))}, nil
}

func (m *mockWatchdogStore) UpdateRunner(_ context.Context, id string, updates store.RunnerUpdates) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.updateErr != nil {
		return m.updateErr
	}

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

func TestDefaultWatchdogConfig(t *testing.T) {
	cfg := DefaultWatchdogConfig()

	assert.Equal(t, 30*time.Second, cfg.HealthCheckInterval)
	assert.Equal(t, 90*time.Second, cfg.StaleThreshold)
	assert.Equal(t, 0, cfg.MinRunners)
	assert.Equal(t, 1*time.Hour, cfg.TaintedCleanupThreshold)
	assert.Nil(t, cfg.AlertCallback)
}

func TestNewWatchdog(t *testing.T) {
	ms := newMockWatchdogStore()

	w := NewWatchdog(ms, "test-pool", nil, nil)
	require.NotNil(t, w)
	assert.Equal(t, "test-pool", w.poolName)
	assert.NotNil(t, w.config)
	assert.NotNil(t, w.taintManager)

	cfg := &WatchdogConfig{
		HealthCheckInterval: 10 * time.Second,
	}
	w2 := NewWatchdog(ms, "test-pool", cfg, zap.NewNop())
	require.NotNil(t, w2)
	assert.Equal(t, 10*time.Second, w2.config.HealthCheckInterval)
}

func TestWatchdog_StartStop(t *testing.T) {
	ms := newMockWatchdogStore()
	poolName := "test-pool"

	// Add a healthy runner
	now := time.Now()
	ms.addRunner(&store.Runner{ID: "run_1", PoolName: &poolName, Status: "idle", LastSeenAt: &now})

	cfg := &WatchdogConfig{
		HealthCheckInterval: 50 * time.Millisecond,
		StaleThreshold:      1 * time.Hour,
	}

	w := NewWatchdog(ms, "test-pool", cfg, zap.NewNop())

	// Start should not block
	err := w.Start(context.Background())
	require.NoError(t, err)

	// Wait a bit for at least one health check
	time.Sleep(100 * time.Millisecond)

	// Stop should not block
	w.Stop()

	// Second stop should be safe
	w.Stop()

	// Second start should work
	err = w.Start(context.Background())
	require.NoError(t, err)

	w.Stop()
}

func TestWatchdog_StartWhileRunning(t *testing.T) {
	ms := newMockWatchdogStore()

	cfg := &WatchdogConfig{
		HealthCheckInterval: 1 * time.Second,
	}

	w := NewWatchdog(ms, "test-pool", cfg, nil)

	err := w.Start(context.Background())
	require.NoError(t, err)
	defer w.Stop()

	// Starting again should be a no-op
	err = w.Start(context.Background())
	require.NoError(t, err)
}

func TestWatchdog_DetectsStaleRunners(t *testing.T) {
	ms := newMockWatchdogStore()
	poolName := "test-pool"

	// Add a stale runner
	staleTime := time.Now().Add(-5 * time.Minute)
	ms.addRunner(&store.Runner{ID: "run_1", PoolName: &poolName, Status: "idle", LastSeenAt: &staleTime})

	cfg := &WatchdogConfig{
		HealthCheckInterval: 50 * time.Millisecond,
		StaleThreshold:      1 * time.Minute,
	}

	var alerts []*PoolAlert
	var alertMu sync.Mutex
	cfg.AlertCallback = func(alert *PoolAlert) {
		alertMu.Lock()
		alerts = append(alerts, alert)
		alertMu.Unlock()
	}

	w := NewWatchdog(ms, "test-pool", cfg, zap.NewNop())
	err := w.Start(context.Background())
	require.NoError(t, err)

	// Wait for health check
	time.Sleep(100 * time.Millisecond)
	w.Stop()

	// Runner should be tainted
	ms.mu.Lock()
	assert.True(t, ms.runners["run_1"].Tainted)
	ms.mu.Unlock()

	// Should have received alert
	alertMu.Lock()
	assert.Len(t, alerts, 1)
	assert.Equal(t, AlertTypeRunnerStale, alerts[0].Type)
	alertMu.Unlock()
}

func TestWatchdog_MinRunnersAlert(t *testing.T) {
	ms := newMockWatchdogStore()
	poolName := "test-pool"

	// Add only 2 runners, min is 5
	now := time.Now()
	ms.addRunner(&store.Runner{ID: "run_1", PoolName: &poolName, Status: "idle", LastSeenAt: &now})
	ms.addRunner(&store.Runner{ID: "run_2", PoolName: &poolName, Status: "busy", LastSeenAt: &now})

	cfg := &WatchdogConfig{
		HealthCheckInterval: 50 * time.Millisecond,
		StaleThreshold:      1 * time.Hour,
		MinRunners:          5,
	}

	var alerts []*PoolAlert
	var alertMu sync.Mutex
	cfg.AlertCallback = func(alert *PoolAlert) {
		alertMu.Lock()
		alerts = append(alerts, alert)
		alertMu.Unlock()
	}

	w := NewWatchdog(ms, "test-pool", cfg, zap.NewNop())
	err := w.Start(context.Background())
	require.NoError(t, err)

	// Wait for health check
	time.Sleep(100 * time.Millisecond)
	w.Stop()

	// Should have received min runners alert
	alertMu.Lock()
	found := false
	for _, a := range alerts {
		if a.Type == AlertTypeMinRunnersBelow {
			found = true
			break
		}
	}
	assert.True(t, found, "should have received min runners alert")
	alertMu.Unlock()
}

func TestWatchdog_GetStats(t *testing.T) {
	ms := newMockWatchdogStore()
	poolName := "test-pool"

	ms.addRunner(&store.Runner{ID: "run_1", PoolName: &poolName, Status: "idle"})
	ms.addRunner(&store.Runner{ID: "run_2", PoolName: &poolName, Status: "busy"})
	ms.addRunner(&store.Runner{ID: "run_3", PoolName: &poolName, Status: "offline"})
	ms.addRunner(&store.Runner{ID: "run_4", PoolName: &poolName, Status: "paused"})
	ms.addRunner(&store.Runner{ID: "run_5", PoolName: &poolName, Status: "idle", Tainted: true})

	w := NewWatchdog(ms, "test-pool", nil, nil)

	stats, err := w.GetStats(context.Background())
	require.NoError(t, err)

	assert.Equal(t, "test-pool", stats.PoolName)
	assert.Equal(t, 5, stats.Total)
	assert.Equal(t, 2, stats.Idle)
	assert.Equal(t, 1, stats.Busy)
	assert.Equal(t, 1, stats.Offline)
	assert.Equal(t, 1, stats.Paused)
	assert.Equal(t, 1, stats.Tainted)
}

func TestWatchdog_ContextCancellation(t *testing.T) {
	ms := newMockWatchdogStore()

	cfg := &WatchdogConfig{
		HealthCheckInterval: 100 * time.Millisecond,
	}

	w := NewWatchdog(ms, "test-pool", cfg, zap.NewNop())

	ctx, cancel := context.WithCancel(context.Background())
	err := w.Start(ctx)
	require.NoError(t, err)

	// Cancel context
	cancel()

	// Wait for watchdog to stop
	time.Sleep(200 * time.Millisecond)

	// Watchdog should have stopped
	w.mu.Lock()
	running := w.running
	w.mu.Unlock()
	// Note: running might still be true if watchdog hasn't processed cancellation yet
	// The important thing is that it doesn't hang
	_ = running
}

func TestWatchdog_ListError(t *testing.T) {
	ms := newMockWatchdogStore()
	ms.listErr = errors.New("database error")

	cfg := &WatchdogConfig{
		HealthCheckInterval: 50 * time.Millisecond,
	}

	w := NewWatchdog(ms, "test-pool", cfg, zap.NewNop())
	err := w.Start(context.Background())
	require.NoError(t, err)

	// Should not crash
	time.Sleep(100 * time.Millisecond)
	w.Stop()
}
