package pool

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/chunlea/marionette/pkg/store"
)

// mockTaintStore implements TaintStore for testing
type mockTaintStore struct {
	runners    map[string]*store.Runner
	runnerList []*store.Runner
	listErr    error
	updateErr  error
}

func newMockTaintStore() *mockTaintStore {
	return &mockTaintStore{
		runners: make(map[string]*store.Runner),
	}
}

func (m *mockTaintStore) addRunner(r *store.Runner) {
	m.runners[r.ID] = r
	m.runnerList = append(m.runnerList, r)
}

func (m *mockTaintStore) ListRunners(_ context.Context, opts store.ListRunnersOptions) (*store.ListResult[store.Runner], error) {
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

func (m *mockTaintStore) UpdateRunner(_ context.Context, id string, updates store.RunnerUpdates) error {
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

func TestNewTaintManager(t *testing.T) {
	ms := newMockTaintStore()

	m := NewTaintManager(ms, nil)
	require.NotNil(t, m)

	m2 := NewTaintManager(ms, zap.NewNop())
	require.NotNil(t, m2)
}

func TestTaintManager_TaintRunner(t *testing.T) {
	ms := newMockTaintStore()
	ms.addRunner(&store.Runner{ID: "run_1", Status: "idle"})

	m := NewTaintManager(ms, zap.NewNop())

	err := m.TaintRunner(context.Background(), "run_1", TaintReasonScriptFailed, "init script exited with code 1")
	require.NoError(t, err)

	assert.True(t, ms.runners["run_1"].Tainted)
	assert.Equal(t, "offline", ms.runners["run_1"].Status)
	assert.Contains(t, *ms.runners["run_1"].TaintReason, "script_failed")
	assert.Contains(t, *ms.runners["run_1"].TaintReason, "init script exited")
}

func TestTaintManager_TaintRunner_NoDetails(t *testing.T) {
	ms := newMockTaintStore()
	ms.addRunner(&store.Runner{ID: "run_1", Status: "idle"})

	m := NewTaintManager(ms, zap.NewNop())

	err := m.TaintRunner(context.Background(), "run_1", TaintReasonManualTaint, "")
	require.NoError(t, err)

	assert.Equal(t, "manual", *ms.runners["run_1"].TaintReason)
}

func TestTaintManager_TaintRunner_UpdateError(t *testing.T) {
	ms := newMockTaintStore()
	ms.addRunner(&store.Runner{ID: "run_1", Status: "idle"})
	ms.updateErr = errors.New("db error")

	m := NewTaintManager(ms, zap.NewNop())

	err := m.TaintRunner(context.Background(), "run_1", TaintReasonManualTaint, "")
	require.Error(t, err)
}

func TestTaintManager_UntaintRunner(t *testing.T) {
	ms := newMockTaintStore()
	taintReason := "previous taint"
	ms.addRunner(&store.Runner{ID: "run_1", Tainted: true, TaintReason: &taintReason})

	m := NewTaintManager(ms, zap.NewNop())

	err := m.UntaintRunner(context.Background(), "run_1")
	require.NoError(t, err)

	assert.False(t, ms.runners["run_1"].Tainted)
}

func TestTaintManager_ListTaintedRunners(t *testing.T) {
	ms := newMockTaintStore()
	poolName := "test-pool"

	ms.addRunner(&store.Runner{ID: "run_1", PoolName: &poolName, Tainted: true})
	ms.addRunner(&store.Runner{ID: "run_2", PoolName: &poolName, Tainted: false})
	ms.addRunner(&store.Runner{ID: "run_3", PoolName: &poolName, Tainted: true})

	m := NewTaintManager(ms, nil)

	runners, err := m.ListTaintedRunners(context.Background(), "test-pool")
	require.NoError(t, err)
	assert.Len(t, runners, 2)
}

func TestTaintManager_CleanupTaintedRunners(t *testing.T) {
	ms := newMockTaintStore()
	poolName := "test-pool"
	oldTime := time.Now().Add(-2 * time.Hour)
	recentTime := time.Now().Add(-10 * time.Minute)

	ms.addRunner(&store.Runner{ID: "run_1", PoolName: &poolName, Tainted: true, LastSeenAt: &oldTime})
	ms.addRunner(&store.Runner{ID: "run_2", PoolName: &poolName, Tainted: true, LastSeenAt: &recentTime})
	ms.addRunner(&store.Runner{ID: "run_3", PoolName: &poolName, Tainted: true, LastSeenAt: nil})

	m := NewTaintManager(ms, zap.NewNop())

	// Cleanup runners offline for more than 1 hour
	cleaned, err := m.CleanupTaintedRunners(context.Background(), "test-pool", 1*time.Hour)
	require.NoError(t, err)
	assert.Equal(t, 2, cleaned) // run_1 (old) and run_3 (nil lastSeen)
}

func TestTaintManager_DetectScriptFailure(t *testing.T) {
	ms := newMockTaintStore()
	ms.addRunner(&store.Runner{ID: "run_1", Status: "idle"})

	m := NewTaintManager(ms, zap.NewNop())

	err := m.DetectScriptFailure(context.Background(), "run_1", ScriptTypeInit, 1, "error message")
	require.NoError(t, err)

	assert.True(t, ms.runners["run_1"].Tainted)
	assert.Contains(t, *ms.runners["run_1"].TaintReason, "init")
	assert.Contains(t, *ms.runners["run_1"].TaintReason, "exit code 1")
	assert.Contains(t, *ms.runners["run_1"].TaintReason, "error message")
}

func TestTaintManager_DetectScriptFailure_LongStderr(t *testing.T) {
	ms := newMockTaintStore()
	ms.addRunner(&store.Runner{ID: "run_1", Status: "idle"})

	m := NewTaintManager(ms, zap.NewNop())

	longStderr := make([]byte, 300)
	for i := range longStderr {
		longStderr[i] = 'a'
	}

	err := m.DetectScriptFailure(context.Background(), "run_1", ScriptTypeCleanup, 1, string(longStderr))
	require.NoError(t, err)

	assert.True(t, ms.runners["run_1"].Tainted)
	// Stderr should be truncated
	assert.LessOrEqual(t, len(*ms.runners["run_1"].TaintReason), 300)
}

func TestTaintManager_DetectMaxTasksReached(t *testing.T) {
	ms := newMockTaintStore()
	ms.addRunner(&store.Runner{ID: "run_1", Status: "idle"})

	m := NewTaintManager(ms, zap.NewNop())

	err := m.DetectMaxTasksReached(context.Background(), "run_1", 100, 100)
	require.NoError(t, err)

	assert.True(t, ms.runners["run_1"].Tainted)
	assert.Contains(t, *ms.runners["run_1"].TaintReason, "max_tasks_reached")
	assert.Contains(t, *ms.runners["run_1"].TaintReason, "100/100")
}

func TestTaintManager_DetectSessionCrash(t *testing.T) {
	ms := newMockTaintStore()
	ms.addRunner(&store.Runner{ID: "run_1", Status: "idle"})

	m := NewTaintManager(ms, zap.NewNop())

	err := m.DetectSessionCrash(context.Background(), "run_1", "sess_123", "unexpected termination")
	require.NoError(t, err)

	assert.True(t, ms.runners["run_1"].Tainted)
	assert.Contains(t, *ms.runners["run_1"].TaintReason, "session_crashed")
	assert.Contains(t, *ms.runners["run_1"].TaintReason, "sess_123")
}

func TestTaintManager_DetectHealthCheckFailure(t *testing.T) {
	ms := newMockTaintStore()
	ms.addRunner(&store.Runner{ID: "run_1", Status: "idle"})

	m := NewTaintManager(ms, zap.NewNop())

	err := m.DetectHealthCheckFailure(context.Background(), "run_1", "disk full")
	require.NoError(t, err)

	assert.True(t, ms.runners["run_1"].Tainted)
	assert.Contains(t, *ms.runners["run_1"].TaintReason, "health_check_failed")
	assert.Contains(t, *ms.runners["run_1"].TaintReason, "disk full")
}

func TestTaintManager_DetectStaleRunner(t *testing.T) {
	ms := newMockTaintStore()
	ms.addRunner(&store.Runner{ID: "run_1", Status: "idle"})

	m := NewTaintManager(ms, zap.NewNop())

	lastSeen := time.Now().Add(-1 * time.Hour)
	err := m.DetectStaleRunner(context.Background(), "run_1", lastSeen)
	require.NoError(t, err)

	assert.True(t, ms.runners["run_1"].Tainted)
	assert.Contains(t, *ms.runners["run_1"].TaintReason, "stale")
	assert.Contains(t, *ms.runners["run_1"].TaintReason, "last seen at")
}

func TestPointerToString(t *testing.T) {
	s := "test"
	assert.Equal(t, "test", pointerToString(&s))
	assert.Equal(t, "", pointerToString(nil))
}
