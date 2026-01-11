package core

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	pb "github.com/chunlea/marionette/gen/proto/v1"
	"github.com/chunlea/marionette/pkg/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// testRunnerStore implements store.Store for testing.
// Uses mutex + copy-on-read to avoid race conditions in concurrent tests.
type testRunnerStore struct {
	mu      sync.RWMutex
	runners map[string]*store.Runner
}

func newTestRunnerStore() *testRunnerStore {
	return &testRunnerStore{
		runners: make(map[string]*store.Runner),
	}
}

// SetRunner is a helper for tests to safely add/update runners.
func (m *testRunnerStore) SetRunner(runner *store.Runner) {
	m.mu.Lock()
	defer m.mu.Unlock()
	// Store a copy
	copy := *runner
	m.runners[runner.ID] = &copy
}

func (m *testRunnerStore) GetRunner(_ context.Context, id string) (*store.Runner, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	runner, ok := m.runners[id]
	if !ok {
		return nil, store.ErrNotFound
	}
	// Return a copy to avoid race conditions
	copy := *runner
	return &copy, nil
}

func (m *testRunnerStore) UpdateRunner(_ context.Context, id string, updates store.RunnerUpdates) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	runner, ok := m.runners[id]
	if !ok {
		return store.ErrNotFound
	}
	if updates.Status != nil {
		runner.Status = *updates.Status
	}
	if updates.LastSeenAt != nil {
		runner.LastSeenAt = updates.LastSeenAt
	}
	return nil
}

func (m *testRunnerStore) ListRunners(_ context.Context, opts store.ListRunnersOptions) (*store.ListResult[store.Runner], error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	items := make([]*store.Runner, 0, len(m.runners))
	for _, r := range m.runners {
		if len(opts.Status) > 0 {
			matched := false
			for _, s := range opts.Status {
				if r.Status == s {
					matched = true
					break
				}
			}
			if !matched {
				continue
			}
		}
		// Return copies to avoid race conditions
		copy := *r
		items = append(items, &copy)
	}
	return &store.ListResult[store.Runner]{Items: items}, nil
}

// testConnManager implements ConnectionManagerInterface for testing.
type testConnManager struct {
	connected   map[string]bool
	lastSeenErr error
}

func newTestConnManager() *testConnManager {
	return &testConnManager{
		connected: make(map[string]bool),
	}
}

func (m *testConnManager) IsConnected(runnerID string) bool {
	return m.connected[runnerID]
}

func (m *testConnManager) UpdateLastSeen(_ string) error {
	return m.lastSeenErr
}

// testStoreWrapper wraps testRunnerStore to implement store.Store.
type testStoreWrapper struct {
	testStore *testRunnerStore
}

func (w *testStoreWrapper) GetRunner(ctx context.Context, id string) (*store.Runner, error) {
	return w.testStore.GetRunner(ctx, id)
}

func (w *testStoreWrapper) UpdateRunner(ctx context.Context, id string, updates store.RunnerUpdates) error {
	return w.testStore.UpdateRunner(ctx, id, updates)
}

func (w *testStoreWrapper) ListRunners(ctx context.Context, opts store.ListRunnersOptions) (*store.ListResult[store.Runner], error) {
	return w.testStore.ListRunners(ctx, opts)
}

// Implement remaining store.Store methods as stubs
func (w *testStoreWrapper) CreateRunner(_ context.Context, _ *store.Runner) error { return nil }
func (w *testStoreWrapper) GetRunnerByName(_ context.Context, _ string) (*store.Runner, error) {
	return nil, store.ErrNotFound
}
func (w *testStoreWrapper) DeleteRunner(_ context.Context, _ string) error { return nil }

func (w *testStoreWrapper) CreateWorkspace(_ context.Context, _ *store.Workspace) error { return nil }
func (w *testStoreWrapper) GetWorkspace(_ context.Context, _ string) (*store.Workspace, error) {
	return nil, store.ErrNotFound
}
func (w *testStoreWrapper) ListWorkspaces(_ context.Context, _ store.ListWorkspacesOptions) (*store.ListResult[store.Workspace], error) {
	return &store.ListResult[store.Workspace]{}, nil
}
func (w *testStoreWrapper) UpdateWorkspace(_ context.Context, _ string, _ store.WorkspaceUpdates) error {
	return nil
}
func (w *testStoreWrapper) DeleteWorkspace(_ context.Context, _ string) error { return nil }

func (w *testStoreWrapper) CreateSession(_ context.Context, _ *store.Session) error { return nil }
func (w *testStoreWrapper) GetSession(_ context.Context, _ string) (*store.Session, error) {
	return nil, store.ErrNotFound
}
func (w *testStoreWrapper) ListSessions(_ context.Context, _ store.ListSessionsOptions) (*store.ListResult[store.Session], error) {
	return &store.ListResult[store.Session]{}, nil
}
func (w *testStoreWrapper) UpdateSession(_ context.Context, _ string, _ store.SessionUpdates) error {
	return nil
}
func (w *testStoreWrapper) DeleteSession(_ context.Context, _ string) error { return nil }

func (w *testStoreWrapper) CreateTask(_ context.Context, _ *store.Task) error { return nil }
func (w *testStoreWrapper) GetTask(_ context.Context, _ string) (*store.Task, error) {
	return nil, store.ErrNotFound
}
func (w *testStoreWrapper) ListTasks(_ context.Context, _ store.ListTasksOptions) (*store.ListResult[store.Task], error) {
	return &store.ListResult[store.Task]{}, nil
}
func (w *testStoreWrapper) UpdateTask(_ context.Context, _ string, _ store.TaskUpdates) error {
	return nil
}
func (w *testStoreWrapper) DeleteTask(_ context.Context, _ string) error { return nil }

func (w *testStoreWrapper) CreateTaskRun(_ context.Context, _ *store.TaskRun) error { return nil }
func (w *testStoreWrapper) GetTaskRun(_ context.Context, _ string) (*store.TaskRun, error) {
	return nil, store.ErrNotFound
}
func (w *testStoreWrapper) ListTaskRuns(_ context.Context, _ store.ListTaskRunsOptions) (*store.ListResult[store.TaskRun], error) {
	return &store.ListResult[store.TaskRun]{}, nil
}
func (w *testStoreWrapper) UpdateTaskRun(_ context.Context, _ string, _ store.TaskRunUpdates) error {
	return nil
}
func (w *testStoreWrapper) DeleteTaskRun(_ context.Context, _ string) error { return nil }

func (w *testStoreWrapper) CreateAPIKey(_ context.Context, _ *store.APIKey) error { return nil }
func (w *testStoreWrapper) GetAPIKey(_ context.Context, _ string) (*store.APIKey, error) {
	return nil, store.ErrNotFound
}
func (w *testStoreWrapper) GetAPIKeyByHash(_ context.Context, _ string) (*store.APIKey, error) {
	return nil, store.ErrNotFound
}
func (w *testStoreWrapper) ListAPIKeys(_ context.Context, _ store.ListAPIKeysOptions) (*store.ListResult[store.APIKey], error) {
	return &store.ListResult[store.APIKey]{}, nil
}
func (w *testStoreWrapper) UpdateAPIKey(_ context.Context, _ string, _ store.APIKeyUpdates) error {
	return nil
}
func (w *testStoreWrapper) DeleteAPIKey(_ context.Context, _ string) error { return nil }

func (w *testStoreWrapper) CreateRunnerToken(_ context.Context, _ *store.RunnerToken) error {
	return nil
}
func (w *testStoreWrapper) GetRunnerToken(_ context.Context, _ string) (*store.RunnerToken, error) {
	return nil, store.ErrNotFound
}
func (w *testStoreWrapper) GetRunnerTokenByHash(_ context.Context, _ string) (*store.RunnerToken, error) {
	return nil, store.ErrNotFound
}
func (w *testStoreWrapper) ListRunnerTokens(_ context.Context, _ store.ListRunnerTokensOptions) (*store.ListResult[store.RunnerToken], error) {
	return &store.ListResult[store.RunnerToken]{}, nil
}
func (w *testStoreWrapper) UpdateRunnerToken(_ context.Context, _ string, _ store.RunnerTokenUpdates) error {
	return nil
}
func (w *testStoreWrapper) DeleteRunnerToken(_ context.Context, _ string) error { return nil }

func (w *testStoreWrapper) GetTaskRunByTaskAndAttempt(_ context.Context, _ string, _ int) (*store.TaskRun, error) {
	return nil, store.ErrNotFound
}

func (w *testStoreWrapper) CreateScheduledTask(_ context.Context, _ *store.ScheduledTask) error {
	return nil
}
func (w *testStoreWrapper) GetScheduledTask(_ context.Context, _ string) (*store.ScheduledTask, error) {
	return nil, store.ErrNotFound
}
func (w *testStoreWrapper) ListScheduledTasks(_ context.Context, _ store.ListScheduledTasksOptions) (*store.ListResult[store.ScheduledTask], error) {
	return &store.ListResult[store.ScheduledTask]{}, nil
}
func (w *testStoreWrapper) UpdateScheduledTask(_ context.Context, _ string, _ store.ScheduledTaskUpdates) error {
	return nil
}
func (w *testStoreWrapper) DeleteScheduledTask(_ context.Context, _ string) error { return nil }

func (w *testStoreWrapper) CreatePermissionRequest(_ context.Context, _ *store.PermissionRequest) error {
	return nil
}
func (w *testStoreWrapper) GetPermissionRequest(_ context.Context, _ string) (*store.PermissionRequest, error) {
	return nil, store.ErrNotFound
}
func (w *testStoreWrapper) ListPermissionRequests(_ context.Context, _ store.ListPermissionRequestsOptions) (*store.ListResult[store.PermissionRequest], error) {
	return &store.ListResult[store.PermissionRequest]{}, nil
}
func (w *testStoreWrapper) UpdatePermissionRequest(_ context.Context, _ string, _ store.PermissionRequestUpdates) error {
	return nil
}

func (w *testStoreWrapper) CreateAgentConfig(_ context.Context, _ *store.AgentConfig) error {
	return nil
}
func (w *testStoreWrapper) GetAgentConfig(_ context.Context, _ string) (*store.AgentConfig, error) {
	return nil, store.ErrNotFound
}
func (w *testStoreWrapper) GetAgentConfigByName(_ context.Context, _ string) (*store.AgentConfig, error) {
	return nil, store.ErrNotFound
}
func (w *testStoreWrapper) GetDefaultAgentConfig(_ context.Context, _ string) (*store.AgentConfig, error) {
	return nil, store.ErrNotFound
}
func (w *testStoreWrapper) ListAgentConfigs(_ context.Context, _ store.ListAgentConfigsOptions) (*store.ListResult[store.AgentConfig], error) {
	return &store.ListResult[store.AgentConfig]{}, nil
}
func (w *testStoreWrapper) UpdateAgentConfig(_ context.Context, _ string, _ store.AgentConfigUpdates) error {
	return nil
}
func (w *testStoreWrapper) DeleteAgentConfig(_ context.Context, _ string) error { return nil }

func (w *testStoreWrapper) CreateProviderConfig(_ context.Context, _ *store.ProviderConfig) error {
	return nil
}
func (w *testStoreWrapper) GetProviderConfig(_ context.Context, _ string) (*store.ProviderConfig, error) {
	return nil, store.ErrNotFound
}
func (w *testStoreWrapper) GetProviderConfigByName(_ context.Context, _ string) (*store.ProviderConfig, error) {
	return nil, store.ErrNotFound
}
func (w *testStoreWrapper) GetDefaultProviderConfig(_ context.Context, _ string) (*store.ProviderConfig, error) {
	return nil, store.ErrNotFound
}
func (w *testStoreWrapper) ListProviderConfigs(_ context.Context, _ store.ListProviderConfigsOptions) (*store.ListResult[store.ProviderConfig], error) {
	return &store.ListResult[store.ProviderConfig]{}, nil
}
func (w *testStoreWrapper) UpdateProviderConfig(_ context.Context, _ string, _ store.ProviderConfigUpdates) error {
	return nil
}
func (w *testStoreWrapper) DeleteProviderConfig(_ context.Context, _ string) error { return nil }

func (w *testStoreWrapper) CreateProfile(_ context.Context, _ *store.Profile) error { return nil }
func (w *testStoreWrapper) GetProfile(_ context.Context, _ string) (*store.Profile, error) {
	return nil, store.ErrNotFound
}
func (w *testStoreWrapper) GetProfileByName(_ context.Context, _ string) (*store.Profile, error) {
	return nil, store.ErrNotFound
}
func (w *testStoreWrapper) ListProfiles(_ context.Context, _ store.ListProfilesOptions) (*store.ListResult[store.Profile], error) {
	return &store.ListResult[store.Profile]{}, nil
}
func (w *testStoreWrapper) UpdateProfile(_ context.Context, _ string, _ store.ProfileUpdates) error {
	return nil
}
func (w *testStoreWrapper) DeleteProfile(_ context.Context, _ string) error { return nil }

func (w *testStoreWrapper) CreateSnapshot(_ context.Context, _ *store.Snapshot) error { return nil }
func (w *testStoreWrapper) GetSnapshot(_ context.Context, _ string) (*store.Snapshot, error) {
	return nil, store.ErrNotFound
}
func (w *testStoreWrapper) GetSnapshotByRunnerAndName(_ context.Context, _, _ string) (*store.Snapshot, error) {
	return nil, store.ErrNotFound
}
func (w *testStoreWrapper) ListSnapshots(_ context.Context, _ store.ListSnapshotsOptions) (*store.ListResult[store.Snapshot], error) {
	return &store.ListResult[store.Snapshot]{}, nil
}
func (w *testStoreWrapper) UpdateSnapshot(_ context.Context, _ string, _ store.SnapshotUpdates) error {
	return nil
}
func (w *testStoreWrapper) DeleteSnapshot(_ context.Context, _ string) error { return nil }

func (w *testStoreWrapper) CreateTunnel(_ context.Context, _ *store.Tunnel) error { return nil }
func (w *testStoreWrapper) GetTunnel(_ context.Context, _ string) (*store.Tunnel, error) {
	return nil, store.ErrNotFound
}
func (w *testStoreWrapper) GetTunnelByTokenHash(_ context.Context, _ string) (*store.Tunnel, error) {
	return nil, store.ErrNotFound
}
func (w *testStoreWrapper) ListTunnels(_ context.Context, _ store.ListTunnelsOptions) (*store.ListResult[store.Tunnel], error) {
	return &store.ListResult[store.Tunnel]{}, nil
}
func (w *testStoreWrapper) UpdateTunnel(_ context.Context, _ string, _ store.TunnelUpdates) error {
	return nil
}
func (w *testStoreWrapper) DeleteTunnel(_ context.Context, _ string) error { return nil }

func (w *testStoreWrapper) CreateActionLog(_ context.Context, _ *store.ActionLog) error { return nil }
func (w *testStoreWrapper) GetActionLog(_ context.Context, _ string) (*store.ActionLog, error) {
	return nil, store.ErrNotFound
}
func (w *testStoreWrapper) ListActionLogs(_ context.Context, _ store.ListActionLogsOptions) (*store.ListResult[store.ActionLog], error) {
	return &store.ListResult[store.ActionLog]{}, nil
}

func (w *testStoreWrapper) CreateLog(_ context.Context, _ *store.Log) error    { return nil }
func (w *testStoreWrapper) CreateLogs(_ context.Context, _ []*store.Log) error { return nil }
func (w *testStoreWrapper) ListLogs(_ context.Context, _ store.ListLogsOptions) (*store.ListResult[store.Log], error) {
	return &store.ListResult[store.Log]{}, nil
}

func (w *testStoreWrapper) CreateLogArchive(_ context.Context, _ *store.LogArchive) error {
	return nil
}
func (w *testStoreWrapper) GetLogArchive(_ context.Context, _ string) (*store.LogArchive, error) {
	return nil, store.ErrNotFound
}
func (w *testStoreWrapper) GetLogArchiveBySession(_ context.Context, _ string) (*store.LogArchive, error) {
	return nil, store.ErrNotFound
}
func (w *testStoreWrapper) ListLogArchives(_ context.Context, _ store.ListLogArchivesOptions) (*store.ListResult[store.LogArchive], error) {
	return &store.ListResult[store.LogArchive]{}, nil
}
func (w *testStoreWrapper) UpdateLogArchive(_ context.Context, _ string, _ store.LogArchiveUpdates) error {
	return nil
}

func (w *testStoreWrapper) CreateDataKey(_ context.Context, _ *store.DataKey) error { return nil }
func (w *testStoreWrapper) GetDataKey(_ context.Context, _ string) (*store.DataKey, error) {
	return nil, store.ErrNotFound
}
func (w *testStoreWrapper) GetDataKeyByResource(_ context.Context, _, _ string) (*store.DataKey, error) {
	return nil, store.ErrNotFound
}
func (w *testStoreWrapper) UpdateDataKey(_ context.Context, _ string, _ store.DataKeyUpdates) error {
	return nil
}
func (w *testStoreWrapper) DeleteDataKey(_ context.Context, _ string) error { return nil }

func (w *testStoreWrapper) CreateChunk(_ context.Context, _ *store.Chunk) error { return nil }
func (w *testStoreWrapper) GetChunk(_ context.Context, _, _ string) (*store.Chunk, error) {
	return nil, store.ErrNotFound
}
func (w *testStoreWrapper) UpdateChunk(_ context.Context, _, _ string, _ store.ChunkUpdates) error {
	return nil
}
func (w *testStoreWrapper) DeleteChunk(_ context.Context, _, _ string) error { return nil }
func (w *testStoreWrapper) IncrementChunkRefCount(_ context.Context, _, _ string) error {
	return nil
}
func (w *testStoreWrapper) DecrementChunkRefCount(_ context.Context, _, _ string) error {
	return nil
}
func (w *testStoreWrapper) ListUnreferencedChunks(_ context.Context, _ string, _ int) ([]*store.Chunk, error) {
	return nil, nil
}
func (w *testStoreWrapper) ListSoftDeletedChunks(_ context.Context, _ string, _ time.Time, _ int) ([]*store.Chunk, error) {
	return nil, nil
}
func (w *testStoreWrapper) MarkChunkDeleted(_ context.Context, _, _ string) error  { return nil }
func (w *testStoreWrapper) ClearChunkDeleted(_ context.Context, _, _ string) error { return nil }

func (w *testStoreWrapper) CreateManifest(_ context.Context, _ *store.Manifest) error { return nil }
func (w *testStoreWrapper) GetManifest(_ context.Context, _ string) (*store.Manifest, error) {
	return nil, store.ErrNotFound
}
func (w *testStoreWrapper) GetLatestManifest(_ context.Context, _ string) (*store.Manifest, error) {
	return nil, store.ErrNotFound
}
func (w *testStoreWrapper) DeleteManifest(_ context.Context, _ string) error { return nil }

func (w *testStoreWrapper) BeginTx(_ context.Context) (store.Tx, error) { return nil, nil }
func (w *testStoreWrapper) Ping(_ context.Context) error                { return nil }
func (w *testStoreWrapper) Close() error                                { return nil }

// Stream methods (stub)
func (w *testStoreWrapper) CreateStream(_ context.Context, _ *store.Stream) error { return nil }
func (w *testStoreWrapper) GetStream(_ context.Context, _ string) (*store.Stream, error) {
	return nil, store.ErrNotFound
}
func (w *testStoreWrapper) GetStreamBySessionAndType(_ context.Context, _, _ string, _ bool) (*store.Stream, error) {
	return nil, store.ErrNotFound
}
func (w *testStoreWrapper) ListStreams(_ context.Context, _ store.ListStreamsOptions) (*store.ListResult[store.Stream], error) {
	return &store.ListResult[store.Stream]{}, nil
}
func (w *testStoreWrapper) UpdateStream(_ context.Context, _ string, _ store.StreamUpdates) error {
	return nil
}
func (w *testStoreWrapper) DeleteStream(_ context.Context, _ string) error { return nil }
func (w *testStoreWrapper) CleanupExpiredStreams(_ context.Context) (int, error) {
	return 0, nil
}

// Helper to create test setup with real RunnerManager
func setupRunnerManagerTest() (*RunnerManager, *testRunnerStore) {
	s := newTestRunnerStore()
	connMgr := newTestConnManager()
	logger := zap.NewNop()
	wrapperStore := &testStoreWrapper{testStore: s}
	manager := NewRunnerManager(wrapperStore, connMgr, logger)
	return manager, s
}

// =============================================================================
// RunnerManager Tests
// =============================================================================

func TestRunnerManager_OnConnect(t *testing.T) {
	manager, s := setupRunnerManagerTest()
	s.runners["run_123"] = &store.Runner{
		ID:     "run_123",
		Name:   "test-runner",
		Status: StatusOffline,
	}

	err := manager.OnConnect(context.Background(), "run_123")
	require.NoError(t, err)

	runner := s.runners["run_123"]
	assert.Equal(t, StatusIdle, runner.Status)
	assert.NotNil(t, runner.LastSeenAt)
}

func TestRunnerManager_OnConnect_RunnerNotFound(t *testing.T) {
	manager, _ := setupRunnerManagerTest()

	err := manager.OnConnect(context.Background(), "run_nonexistent")
	assert.ErrorIs(t, err, store.ErrNotFound)
}

func TestRunnerManager_OnConnect_InvalidTransition(t *testing.T) {
	manager, s := setupRunnerManagerTest()
	// Runner already busy - invalid transition but allowed for reconnection
	s.runners["run_123"] = &store.Runner{
		ID:     "run_123",
		Name:   "test-runner",
		Status: StatusBusy,
	}

	// Should succeed anyway (reconnection scenario)
	err := manager.OnConnect(context.Background(), "run_123")
	require.NoError(t, err)
	assert.Equal(t, StatusIdle, s.runners["run_123"].Status)
}

func TestRunnerManager_OnDisconnect(t *testing.T) {
	manager, s := setupRunnerManagerTest()
	s.runners["run_123"] = &store.Runner{
		ID:     "run_123",
		Name:   "test-runner",
		Status: StatusIdle,
	}

	err := manager.OnDisconnect(context.Background(), "run_123")
	require.NoError(t, err)

	runner := s.runners["run_123"]
	assert.Equal(t, StatusOffline, runner.Status)
}

func TestRunnerManager_OnDisconnect_RunnerNotFound(t *testing.T) {
	manager, _ := setupRunnerManagerTest()

	err := manager.OnDisconnect(context.Background(), "run_nonexistent")
	assert.ErrorIs(t, err, store.ErrNotFound)
}

func TestRunnerManager_OnHeartbeat(t *testing.T) {
	manager, s := setupRunnerManagerTest()
	s.runners["run_123"] = &store.Runner{
		ID:     "run_123",
		Name:   "test-runner",
		Status: StatusIdle,
	}

	hb := &pb.Heartbeat{
		Status: StatusIdle,
	}

	err := manager.OnHeartbeat(context.Background(), "run_123", hb)
	require.NoError(t, err)

	runner := s.runners["run_123"]
	assert.NotNil(t, runner.LastSeenAt)
}

func TestRunnerManager_OnHeartbeat_WithStatusChange(t *testing.T) {
	manager, s := setupRunnerManagerTest()
	s.runners["run_123"] = &store.Runner{
		ID:     "run_123",
		Name:   "test-runner",
		Status: StatusIdle,
	}

	hb := &pb.Heartbeat{
		Status: StatusBusy,
	}

	err := manager.OnHeartbeat(context.Background(), "run_123", hb)
	require.NoError(t, err)

	runner := s.runners["run_123"]
	assert.Equal(t, StatusBusy, runner.Status)
}

func TestRunnerManager_OnHeartbeat_InvalidStatusChange(t *testing.T) {
	manager, s := setupRunnerManagerTest()
	s.runners["run_123"] = &store.Runner{
		ID:     "run_123",
		Name:   "test-runner",
		Status: StatusOffline,
	}

	hb := &pb.Heartbeat{
		Status: StatusBusy, // Invalid: offline -> busy
	}

	err := manager.OnHeartbeat(context.Background(), "run_123", hb)
	require.NoError(t, err)

	// Status should not change
	runner := s.runners["run_123"]
	assert.Equal(t, StatusOffline, runner.Status)
}

func TestRunnerManager_OnHeartbeat_EmptyStatus(t *testing.T) {
	manager, s := setupRunnerManagerTest()
	s.runners["run_123"] = &store.Runner{
		ID:     "run_123",
		Name:   "test-runner",
		Status: StatusIdle,
	}

	hb := &pb.Heartbeat{
		Status: "", // Empty status
	}

	err := manager.OnHeartbeat(context.Background(), "run_123", hb)
	require.NoError(t, err)

	// Status should not change, but LastSeenAt should be updated
	runner := s.runners["run_123"]
	assert.Equal(t, StatusIdle, runner.Status)
	assert.NotNil(t, runner.LastSeenAt)
}

func TestRunnerManager_OnHeartbeat_RunnerNotFound(t *testing.T) {
	manager, _ := setupRunnerManagerTest()

	hb := &pb.Heartbeat{Status: StatusIdle}
	err := manager.OnHeartbeat(context.Background(), "run_nonexistent", hb)
	assert.ErrorIs(t, err, store.ErrNotFound)
}

func TestRunnerManager_SetStatus(t *testing.T) {
	manager, s := setupRunnerManagerTest()
	s.runners["run_123"] = &store.Runner{
		ID:     "run_123",
		Name:   "test-runner",
		Status: StatusIdle,
	}

	err := manager.SetStatus(context.Background(), "run_123", StatusBusy)
	require.NoError(t, err)

	runner := s.runners["run_123"]
	assert.Equal(t, StatusBusy, runner.Status)
}

func TestRunnerManager_SetStatus_InvalidTransition(t *testing.T) {
	manager, s := setupRunnerManagerTest()
	s.runners["run_123"] = &store.Runner{
		ID:     "run_123",
		Name:   "test-runner",
		Status: StatusOffline,
	}

	err := manager.SetStatus(context.Background(), "run_123", StatusBusy)
	assert.ErrorIs(t, err, ErrInvalidStatusTransition)
}

func TestRunnerManager_SetStatus_RunnerNotFound(t *testing.T) {
	manager, _ := setupRunnerManagerTest()

	err := manager.SetStatus(context.Background(), "run_nonexistent", StatusIdle)
	assert.ErrorIs(t, err, store.ErrNotFound)
}

// =============================================================================
// IsValidTransition Tests
// =============================================================================

func TestIsValidTransition(t *testing.T) {
	tests := []struct {
		name     string
		from     string
		to       string
		expected bool
	}{
		// Offline transitions
		{"offline to idle", StatusOffline, StatusIdle, true},
		{"offline to busy", StatusOffline, StatusBusy, false},
		{"offline to offline", StatusOffline, StatusOffline, true},
		{"offline to paused", StatusOffline, StatusPaused, true},

		// Idle transitions
		{"idle to busy", StatusIdle, StatusBusy, true},
		{"idle to idle", StatusIdle, StatusIdle, true},
		{"idle to offline", StatusIdle, StatusOffline, true},
		{"idle to paused", StatusIdle, StatusPaused, true},

		// Busy transitions
		{"busy to idle", StatusBusy, StatusIdle, true},
		{"busy to busy", StatusBusy, StatusBusy, true},
		{"busy to offline", StatusBusy, StatusOffline, true},
		{"busy to paused", StatusBusy, StatusPaused, true},

		// Paused transitions
		{"paused to idle", StatusPaused, StatusIdle, true},
		{"paused to busy", StatusPaused, StatusBusy, false},
		{"paused to offline", StatusPaused, StatusOffline, true},
		{"paused to paused", StatusPaused, StatusPaused, true},

		// Unknown status (should allow any transition)
		{"unknown to idle", "unknown", StatusIdle, true},
		{"unknown to busy", "unknown", StatusBusy, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsValidTransition(tt.from, tt.to)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// =============================================================================
// StaleDetector Tests
// =============================================================================

func TestStaleDetector_CheckStaleRunners(t *testing.T) {
	s := newTestRunnerStore()
	staleTime := time.Now().Add(-2 * time.Minute)
	s.runners["run_stale"] = &store.Runner{
		ID:         "run_stale",
		Name:       "stale-runner",
		Status:     StatusIdle,
		LastSeenAt: &staleTime,
	}

	freshTime := time.Now()
	s.runners["run_fresh"] = &store.Runner{
		ID:         "run_fresh",
		Name:       "fresh-runner",
		Status:     StatusIdle,
		LastSeenAt: &freshTime,
	}

	connMgr := newTestConnManager()
	logger := zap.NewNop()

	wrapperStore := &testStoreWrapper{testStore: s}
	manager := NewRunnerManager(wrapperStore, connMgr, logger)

	detector := NewStaleDetector(
		wrapperStore,
		connMgr,
		manager,
		logger,
		WithStaleThreshold(30*time.Second),
	)

	err := detector.checkStaleRunners(context.Background())
	require.NoError(t, err)

	// Stale runner should be offline
	assert.Equal(t, StatusOffline, s.runners["run_stale"].Status)

	// Fresh runner should still be idle
	assert.Equal(t, StatusIdle, s.runners["run_fresh"].Status)
}

func TestStaleDetector_CheckStaleRunners_NoLastSeen(t *testing.T) {
	s := newTestRunnerStore()
	// Runner with no LastSeenAt and not connected
	s.runners["run_no_lastseen"] = &store.Runner{
		ID:         "run_no_lastseen",
		Name:       "no-lastseen-runner",
		Status:     StatusIdle,
		LastSeenAt: nil,
	}

	connMgr := newTestConnManager()
	connMgr.connected["run_no_lastseen"] = false // Not connected

	logger := zap.NewNop()
	wrapperStore := &testStoreWrapper{testStore: s}
	manager := NewRunnerManager(wrapperStore, connMgr, logger)

	detector := NewStaleDetector(
		wrapperStore,
		connMgr,
		manager,
		logger,
		WithStaleThreshold(30*time.Second),
	)

	err := detector.checkStaleRunners(context.Background())
	require.NoError(t, err)

	// Should be marked offline because no LastSeenAt and not connected
	assert.Equal(t, StatusOffline, s.runners["run_no_lastseen"].Status)
}

func TestStaleDetector_CheckStaleRunners_NoLastSeenButConnected(t *testing.T) {
	s := newTestRunnerStore()
	s.runners["run_connected"] = &store.Runner{
		ID:         "run_connected",
		Name:       "connected-runner",
		Status:     StatusIdle,
		LastSeenAt: nil,
	}

	connMgr := newTestConnManager()
	connMgr.connected["run_connected"] = true // Connected

	logger := zap.NewNop()
	wrapperStore := &testStoreWrapper{testStore: s}
	manager := NewRunnerManager(wrapperStore, connMgr, logger)

	detector := NewStaleDetector(
		wrapperStore,
		connMgr,
		manager,
		logger,
		WithStaleThreshold(30*time.Second),
	)

	err := detector.checkStaleRunners(context.Background())
	require.NoError(t, err)

	// Should NOT be marked offline because it's connected
	assert.Equal(t, StatusIdle, s.runners["run_connected"].Status)
}

func TestStaleDetector_WithCheckInterval(t *testing.T) {
	s := newTestRunnerStore()
	connMgr := newTestConnManager()
	logger := zap.NewNop()
	wrapperStore := &testStoreWrapper{testStore: s}
	manager := NewRunnerManager(wrapperStore, connMgr, logger)

	detector := NewStaleDetector(
		wrapperStore,
		connMgr,
		manager,
		logger,
		WithCheckInterval(5*time.Second),
	)

	assert.Equal(t, 5*time.Second, detector.checkInterval)
}

func TestStaleDetector_StartStop(t *testing.T) {
	s := newTestRunnerStore()
	connMgr := newTestConnManager()
	logger := zap.NewNop()
	wrapperStore := &testStoreWrapper{testStore: s}
	manager := NewRunnerManager(wrapperStore, connMgr, logger)

	detector := NewStaleDetector(
		wrapperStore,
		connMgr,
		manager,
		logger,
		WithCheckInterval(100*time.Millisecond),
	)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	detector.Start(ctx)

	// Let it run briefly
	time.Sleep(50 * time.Millisecond)

	// Stop should complete without hanging
	done := make(chan struct{})
	go func() {
		detector.Stop()
		close(done)
	}()

	select {
	case <-done:
		// Success
	case <-time.After(time.Second):
		t.Fatal("Stop() did not complete in time")
	}
}

func TestStaleDetector_ContextCancellation(t *testing.T) {
	s := newTestRunnerStore()
	connMgr := newTestConnManager()
	logger := zap.NewNop()
	wrapperStore := &testStoreWrapper{testStore: s}
	manager := NewRunnerManager(wrapperStore, connMgr, logger)

	detector := NewStaleDetector(
		wrapperStore,
		connMgr,
		manager,
		logger,
		WithCheckInterval(100*time.Millisecond),
	)

	ctx, cancel := context.WithCancel(context.Background())
	detector.Start(ctx)

	// Cancel context
	cancel()

	// doneCh should be closed
	select {
	case <-detector.doneCh:
		// Success
	case <-time.After(time.Second):
		t.Fatal("detector did not stop after context cancellation")
	}
}

func TestStaleDetector_MarkStale_WithoutRunnerManager(t *testing.T) {
	s := newTestRunnerStore()
	s.runners["run_123"] = &store.Runner{
		ID:     "run_123",
		Name:   "test-runner",
		Status: StatusIdle,
	}

	connMgr := newTestConnManager()
	logger := zap.NewNop()
	wrapperStore := &testStoreWrapper{testStore: s}

	// Create detector WITHOUT runner manager
	detector := NewStaleDetector(
		wrapperStore,
		connMgr,
		nil, // No runner manager
		logger,
	)

	err := detector.markStale(context.Background(), "run_123", "test reason")
	require.NoError(t, err)

	// Should still be marked offline via fallback
	assert.Equal(t, StatusOffline, s.runners["run_123"].Status)
}

func TestStaleDetector_MarkStale_RunnerNotFound(t *testing.T) {
	s := newTestRunnerStore()
	connMgr := newTestConnManager()
	logger := zap.NewNop()
	wrapperStore := &testStoreWrapper{testStore: s}

	detector := NewStaleDetector(
		wrapperStore,
		connMgr,
		nil,
		logger,
	)

	err := detector.markStale(context.Background(), "run_nonexistent", "test reason")
	assert.ErrorIs(t, err, store.ErrNotFound)
}

// =============================================================================
// stringPtr Helper Test
// =============================================================================

func TestStringPtr(t *testing.T) {
	ptr := stringPtr("test")
	require.NotNil(t, ptr)
	assert.Equal(t, "test", *ptr)
}

// =============================================================================
// Error Path Tests
// =============================================================================

// errorTestStore wraps testRunnerStore to inject errors
type errorTestStore struct {
	*testStoreWrapper
	updateErr error
	listErr   error
}

func (e *errorTestStore) UpdateRunner(_ context.Context, id string, updates store.RunnerUpdates) error {
	if e.updateErr != nil {
		return e.updateErr
	}
	return e.testStoreWrapper.UpdateRunner(context.Background(), id, updates)
}

func (e *errorTestStore) ListRunners(_ context.Context, opts store.ListRunnersOptions) (*store.ListResult[store.Runner], error) {
	if e.listErr != nil {
		return nil, e.listErr
	}
	return e.testStoreWrapper.ListRunners(context.Background(), opts)
}

func TestRunnerManager_OnConnect_UpdateError(t *testing.T) {
	s := newTestRunnerStore()
	s.runners["run_123"] = &store.Runner{
		ID:     "run_123",
		Name:   "test-runner",
		Status: StatusOffline,
	}

	connMgr := newTestConnManager()
	logger := zap.NewNop()
	wrapperStore := &testStoreWrapper{testStore: s}
	errorStore := &errorTestStore{
		testStoreWrapper: wrapperStore,
		updateErr:        errors.New("update failed"),
	}
	manager := NewRunnerManager(errorStore, connMgr, logger)

	err := manager.OnConnect(context.Background(), "run_123")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "update failed")
}

func TestRunnerManager_OnHeartbeat_UpdateError(t *testing.T) {
	s := newTestRunnerStore()
	s.runners["run_123"] = &store.Runner{
		ID:     "run_123",
		Name:   "test-runner",
		Status: StatusIdle,
	}

	connMgr := newTestConnManager()
	logger := zap.NewNop()
	wrapperStore := &testStoreWrapper{testStore: s}
	errorStore := &errorTestStore{
		testStoreWrapper: wrapperStore,
		updateErr:        errors.New("update failed"),
	}
	manager := NewRunnerManager(errorStore, connMgr, logger)

	hb := &pb.Heartbeat{Status: ""}

	err := manager.OnHeartbeat(context.Background(), "run_123", hb)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "update failed")
}

func TestRunnerManager_SetStatus_UpdateError(t *testing.T) {
	s := newTestRunnerStore()
	s.runners["run_123"] = &store.Runner{
		ID:     "run_123",
		Name:   "test-runner",
		Status: StatusIdle,
	}

	connMgr := newTestConnManager()
	logger := zap.NewNop()
	wrapperStore := &testStoreWrapper{testStore: s}
	errorStore := &errorTestStore{
		testStoreWrapper: wrapperStore,
		updateErr:        errors.New("update failed"),
	}
	manager := NewRunnerManager(errorStore, connMgr, logger)

	err := manager.SetStatus(context.Background(), "run_123", StatusBusy)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "update failed")
}

func TestStaleDetector_CheckStaleRunners_ListError(t *testing.T) {
	s := newTestRunnerStore()
	connMgr := newTestConnManager()
	logger := zap.NewNop()
	wrapperStore := &testStoreWrapper{testStore: s}
	errorStore := &errorTestStore{
		testStoreWrapper: wrapperStore,
		listErr:          errors.New("list failed"),
	}
	manager := NewRunnerManager(errorStore, connMgr, logger)

	detector := NewStaleDetector(
		errorStore,
		connMgr,
		manager,
		logger,
		WithStaleThreshold(30*time.Second),
	)

	err := detector.checkStaleRunners(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "list failed")
}

func TestStaleDetector_CheckStaleRunners_MarkStaleError(t *testing.T) {
	s := newTestRunnerStore()
	staleTime := time.Now().Add(-2 * time.Minute)
	s.runners["run_stale"] = &store.Runner{
		ID:         "run_stale",
		Name:       "stale-runner",
		Status:     StatusIdle,
		LastSeenAt: &staleTime,
	}

	connMgr := newTestConnManager()
	logger := zap.NewNop()
	wrapperStore := &testStoreWrapper{testStore: s}
	errorStore := &errorTestStore{
		testStoreWrapper: wrapperStore,
		updateErr:        errors.New("update failed"),
	}
	manager := NewRunnerManager(errorStore, connMgr, logger)

	detector := NewStaleDetector(
		errorStore,
		connMgr,
		manager,
		logger,
		WithStaleThreshold(30*time.Second),
	)

	// Should not return error but should log it
	err := detector.checkStaleRunners(context.Background())
	require.NoError(t, err)
}

func TestStaleDetector_CheckStaleRunners_NoLastSeenMarkStaleError(t *testing.T) {
	s := newTestRunnerStore()
	s.runners["run_no_lastseen"] = &store.Runner{
		ID:         "run_no_lastseen",
		Name:       "no-lastseen-runner",
		Status:     StatusIdle,
		LastSeenAt: nil,
	}

	connMgr := newTestConnManager()
	connMgr.connected["run_no_lastseen"] = false

	logger := zap.NewNop()
	wrapperStore := &testStoreWrapper{testStore: s}
	errorStore := &errorTestStore{
		testStoreWrapper: wrapperStore,
		updateErr:        errors.New("update failed"),
	}
	manager := NewRunnerManager(errorStore, connMgr, logger)

	detector := NewStaleDetector(
		errorStore,
		connMgr,
		manager,
		logger,
		WithStaleThreshold(30*time.Second),
	)

	// Should not return error but should log the markStale error
	err := detector.checkStaleRunners(context.Background())
	require.NoError(t, err)
}

func TestStaleDetector_CheckStaleRunners_NilConnManager(t *testing.T) {
	s := newTestRunnerStore()
	s.runners["run_no_lastseen"] = &store.Runner{
		ID:         "run_no_lastseen",
		Name:       "no-lastseen-runner",
		Status:     StatusIdle,
		LastSeenAt: nil,
	}

	logger := zap.NewNop()
	wrapperStore := &testStoreWrapper{testStore: s}
	manager := NewRunnerManager(wrapperStore, nil, logger)

	detector := NewStaleDetector(
		wrapperStore,
		nil, // nil connManager
		manager,
		logger,
		WithStaleThreshold(30*time.Second),
	)

	// With nil connManager, should skip the no-LastSeenAt check
	err := detector.checkStaleRunners(context.Background())
	require.NoError(t, err)

	// Runner should still be idle (not marked offline)
	assert.Equal(t, StatusIdle, s.runners["run_no_lastseen"].Status)
}

func TestRunnerManager_OnHeartbeat_NilConnManager(t *testing.T) {
	s := newTestRunnerStore()
	s.runners["run_123"] = &store.Runner{
		ID:     "run_123",
		Name:   "test-runner",
		Status: StatusIdle,
	}

	logger := zap.NewNop()
	wrapperStore := &testStoreWrapper{testStore: s}
	manager := NewRunnerManager(wrapperStore, nil, logger) // nil connManager

	hb := &pb.Heartbeat{Status: StatusIdle}

	err := manager.OnHeartbeat(context.Background(), "run_123", hb)
	require.NoError(t, err)
}

func TestStaleDetector_Run_TickerLoop(t *testing.T) {
	s := newTestRunnerStore()
	staleTime := time.Now().Add(-2 * time.Minute)
	s.SetRunner(&store.Runner{
		ID:         "run_stale",
		Name:       "stale-runner",
		Status:     StatusIdle,
		LastSeenAt: &staleTime,
	})

	connMgr := newTestConnManager()
	logger := zap.NewNop()
	wrapperStore := &testStoreWrapper{testStore: s}
	manager := NewRunnerManager(wrapperStore, connMgr, logger)

	detector := NewStaleDetector(
		wrapperStore,
		connMgr,
		manager,
		logger,
		WithCheckInterval(50*time.Millisecond),
		WithStaleThreshold(30*time.Second),
	)

	ctx, cancel := context.WithCancel(context.Background())
	detector.Start(ctx)

	// Wait for at least one tick
	time.Sleep(100 * time.Millisecond)

	// Stale runner should be marked offline by the ticker
	runner, err := s.GetRunner(context.Background(), "run_stale")
	require.NoError(t, err)
	assert.Equal(t, StatusOffline, runner.Status)

	cancel()

	// Wait for done
	select {
	case <-detector.doneCh:
		// Success
	case <-time.After(time.Second):
		t.Fatal("detector did not stop")
	}
}

// =============================================================================
// RunnerManager Options Tests
// =============================================================================

// mockTaskMgrForRunner implements TaskManagerInterface for testing runner manager.
type mockTaskMgrForRunner struct {
	failRunCalled     bool
	failRunID         string
	failRunReason     string
	failRunErr        error
	shouldRetryCalled bool
	shouldRetryTaskID string
	shouldRetryResult bool
	shouldRetryErr    error
	retryCalled       bool
	retryTaskID       string
	retryErr          error
}

func (m *mockTaskMgrForRunner) Create(_ context.Context, _ CreateTaskOptions) (*store.Task, error) {
	return nil, nil
}

func (m *mockTaskMgrForRunner) Get(_ context.Context, _ string) (*store.Task, error) {
	return nil, nil
}

func (m *mockTaskMgrForRunner) List(_ context.Context, _ ListTasksOptions) (*store.ListResult[store.Task], error) {
	return nil, nil
}

func (m *mockTaskMgrForRunner) Cancel(_ context.Context, _ string) error {
	return nil
}

func (m *mockTaskMgrForRunner) Execute(_ context.Context, _ string) error {
	return nil
}

func (m *mockTaskMgrForRunner) ReExecute(_ context.Context, _ string) error {
	return nil
}

func (m *mockTaskMgrForRunner) CreateRun(_ context.Context, _ string) (*store.TaskRun, error) {
	return nil, nil
}

func (m *mockTaskMgrForRunner) OnTaskAccepted(_ context.Context, _ string) error {
	return nil
}

func (m *mockTaskMgrForRunner) OnTaskStarted(_ context.Context, _ string) error {
	return nil
}

func (m *mockTaskMgrForRunner) OnTaskProgress(_ context.Context, _ string, _ int) error {
	return nil
}

func (m *mockTaskMgrForRunner) OnTaskCompleted(_ context.Context, _ *TaskCompletedResult) error {
	return nil
}

func (m *mockTaskMgrForRunner) FailRun(_ context.Context, runID, reason string) error {
	m.failRunCalled = true
	m.failRunID = runID
	m.failRunReason = reason
	return m.failRunErr
}

func (m *mockTaskMgrForRunner) ShouldRetry(_ context.Context, taskID string) (bool, error) {
	m.shouldRetryCalled = true
	m.shouldRetryTaskID = taskID
	return m.shouldRetryResult, m.shouldRetryErr
}

func (m *mockTaskMgrForRunner) Retry(_ context.Context, taskID string) (*store.TaskRun, error) {
	m.retryCalled = true
	m.retryTaskID = taskID
	return nil, m.retryErr
}

// mockSessionMgrForRunner implements SessionManagerInterface for testing runner manager.
type mockSessionMgrForRunner struct {
	attachRunnerCalled bool
	attachSessionID    string
	attachRunnerID     string
	attachErr          error
	detachRunnerCalled bool
	detachSessionID    string
	detachErr          error
	suspendCalled      bool
	suspendSessionID   string
	suspendStrategy    string
	suspendErr         error
}

func (m *mockSessionMgrForRunner) Create(_ context.Context, _ CreateSessionOptions) (*store.Session, error) {
	return nil, nil
}

func (m *mockSessionMgrForRunner) Get(_ context.Context, _ string) (*store.Session, error) {
	return nil, nil
}

func (m *mockSessionMgrForRunner) List(_ context.Context, _ ListSessionsOptions) (*store.ListResult[store.Session], error) {
	return nil, nil
}

func (m *mockSessionMgrForRunner) Activate(_ context.Context, _, _ string) error {
	return nil
}

func (m *mockSessionMgrForRunner) Resume(_ context.Context, _ string) error {
	return nil
}

func (m *mockSessionMgrForRunner) Terminate(_ context.Context, _ string) error {
	return nil
}

func (m *mockSessionMgrForRunner) AttachRunner(_ context.Context, sessionID, runnerID string) error {
	m.attachRunnerCalled = true
	m.attachSessionID = sessionID
	m.attachRunnerID = runnerID
	return m.attachErr
}

func (m *mockSessionMgrForRunner) DetachRunner(_ context.Context, sessionID string) error {
	m.detachRunnerCalled = true
	m.detachSessionID = sessionID
	return m.detachErr
}

func (m *mockSessionMgrForRunner) Suspend(_ context.Context, sessionID, strategy string) error {
	m.suspendCalled = true
	m.suspendSessionID = sessionID
	m.suspendStrategy = strategy
	return m.suspendErr
}

func (m *mockSessionMgrForRunner) UpdateContextSnapshot(_ context.Context, _ string, _ *ContextSnapshot) error {
	return nil
}

func TestWithTaskManager(t *testing.T) {
	s := newTestRunnerStore()
	connMgr := newTestConnManager()
	logger := zap.NewNop()
	wrapperStore := &testStoreWrapper{testStore: s}

	taskMgr := &mockTaskMgrForRunner{}
	manager := NewRunnerManager(wrapperStore, connMgr, logger, WithTaskManager(taskMgr))

	assert.NotNil(t, manager.taskMgr)
	assert.Equal(t, taskMgr, manager.taskMgr)
}

func TestWithSessionManager(t *testing.T) {
	s := newTestRunnerStore()
	connMgr := newTestConnManager()
	logger := zap.NewNop()
	wrapperStore := &testStoreWrapper{testStore: s}

	sessionMgr := &mockSessionMgrForRunner{}
	manager := NewRunnerManager(wrapperStore, connMgr, logger, WithSessionManager(sessionMgr))

	assert.NotNil(t, manager.sessionMgr)
	assert.Equal(t, sessionMgr, manager.sessionMgr)
}

func TestWithBothManagers(t *testing.T) {
	s := newTestRunnerStore()
	connMgr := newTestConnManager()
	logger := zap.NewNop()
	wrapperStore := &testStoreWrapper{testStore: s}

	taskMgr := &mockTaskMgrForRunner{}
	sessionMgr := &mockSessionMgrForRunner{}
	manager := NewRunnerManager(wrapperStore, connMgr, logger,
		WithTaskManager(taskMgr),
		WithSessionManager(sessionMgr),
	)

	assert.NotNil(t, manager.taskMgr)
	assert.NotNil(t, manager.sessionMgr)
}

// =============================================================================
// handleInFlightTasks Tests
// =============================================================================

// extendedTestStoreWrapper extends testStoreWrapper to support task runs and sessions for disconnect tests.
type extendedTestStoreWrapper struct {
	*testStoreWrapper
	taskRuns map[string]*store.TaskRun
	sessions map[string]*store.Session
}

func newExtendedTestStoreWrapper(s *testRunnerStore) *extendedTestStoreWrapper {
	return &extendedTestStoreWrapper{
		testStoreWrapper: &testStoreWrapper{testStore: s},
		taskRuns:         make(map[string]*store.TaskRun),
		sessions:         make(map[string]*store.Session),
	}
}

func (w *extendedTestStoreWrapper) ListTaskRuns(_ context.Context, opts store.ListTaskRunsOptions) (*store.ListResult[store.TaskRun], error) {
	items := make([]*store.TaskRun, 0)
	for _, run := range w.taskRuns {
		// Filter by runner ID
		if opts.RunnerID != nil && (run.RunnerID == nil || *run.RunnerID != *opts.RunnerID) {
			continue
		}
		// Filter by status
		if len(opts.Status) > 0 {
			matched := false
			for _, s := range opts.Status {
				if run.Status == s {
					matched = true
					break
				}
			}
			if !matched {
				continue
			}
		}
		items = append(items, run)
	}
	return &store.ListResult[store.TaskRun]{Items: items}, nil
}

func (w *extendedTestStoreWrapper) ListSessions(_ context.Context, opts store.ListSessionsOptions) (*store.ListResult[store.Session], error) {
	items := make([]*store.Session, 0)
	for _, sess := range w.sessions {
		// Filter by runner ID
		if opts.RunnerID != nil && (sess.RunnerID == nil || *sess.RunnerID != *opts.RunnerID) {
			continue
		}
		// Filter by status
		if len(opts.Status) > 0 {
			matched := false
			for _, s := range opts.Status {
				if sess.Status == s {
					matched = true
					break
				}
			}
			if !matched {
				continue
			}
		}
		items = append(items, sess)
	}
	return &store.ListResult[store.Session]{Items: items}, nil
}

func TestRunnerManager_HandleInFlightTasks_NoTaskManager(t *testing.T) {
	s := newTestRunnerStore()
	connMgr := newTestConnManager()
	logger := zap.NewNop()
	wrapperStore := newExtendedTestStoreWrapper(s)

	// Runner with no task manager
	manager := NewRunnerManager(wrapperStore, connMgr, logger)

	s.runners["run_123"] = &store.Runner{
		ID:     "run_123",
		Name:   "test-runner",
		Status: StatusIdle,
	}

	// Should succeed without task manager (just logs and returns)
	err := manager.OnDisconnect(context.Background(), "run_123")
	require.NoError(t, err)
}

func TestRunnerManager_HandleInFlightTasks_NoRunningTasks(t *testing.T) {
	s := newTestRunnerStore()
	connMgr := newTestConnManager()
	logger := zap.NewNop()
	wrapperStore := newExtendedTestStoreWrapper(s)

	taskMgr := &mockTaskMgrForRunner{}
	manager := NewRunnerManager(wrapperStore, connMgr, logger, WithTaskManager(taskMgr))

	s.runners["run_123"] = &store.Runner{
		ID:     "run_123",
		Name:   "test-runner",
		Status: StatusIdle,
	}

	// No task runs in store
	err := manager.OnDisconnect(context.Background(), "run_123")
	require.NoError(t, err)

	// TaskManager should not be called
	assert.False(t, taskMgr.failRunCalled)
}

func TestRunnerManager_HandleInFlightTasks_WithRunningTasks(t *testing.T) {
	s := newTestRunnerStore()
	connMgr := newTestConnManager()
	logger := zap.NewNop()
	wrapperStore := newExtendedTestStoreWrapper(s)

	runnerID := "run_123"
	taskMgr := &mockTaskMgrForRunner{shouldRetryResult: false}
	manager := NewRunnerManager(wrapperStore, connMgr, logger, WithTaskManager(taskMgr))

	s.runners[runnerID] = &store.Runner{
		ID:     runnerID,
		Name:   "test-runner",
		Status: StatusBusy,
	}

	// Add running task runs
	wrapperStore.taskRuns["trun_1"] = &store.TaskRun{
		ID:       "trun_1",
		TaskID:   "task_1",
		RunnerID: &runnerID,
		Status:   TaskRunStatusRunning,
	}
	wrapperStore.taskRuns["trun_2"] = &store.TaskRun{
		ID:       "trun_2",
		TaskID:   "task_2",
		RunnerID: &runnerID,
		Status:   TaskRunStatusAssigned,
	}

	err := manager.OnDisconnect(context.Background(), runnerID)
	require.NoError(t, err)

	// TaskManager should be called to fail runs
	assert.True(t, taskMgr.failRunCalled)
	assert.True(t, taskMgr.shouldRetryCalled)
}

func TestRunnerManager_HandleInFlightTasks_WithRetry(t *testing.T) {
	s := newTestRunnerStore()
	connMgr := newTestConnManager()
	logger := zap.NewNop()
	wrapperStore := newExtendedTestStoreWrapper(s)

	runnerID := "run_123"
	taskMgr := &mockTaskMgrForRunner{shouldRetryResult: true}
	manager := NewRunnerManager(wrapperStore, connMgr, logger, WithTaskManager(taskMgr))

	s.runners[runnerID] = &store.Runner{
		ID:     runnerID,
		Name:   "test-runner",
		Status: StatusBusy,
	}

	// Add running task run
	wrapperStore.taskRuns["trun_1"] = &store.TaskRun{
		ID:       "trun_1",
		TaskID:   "task_1",
		RunnerID: &runnerID,
		Status:   TaskRunStatusRunning,
	}

	err := manager.OnDisconnect(context.Background(), runnerID)
	require.NoError(t, err)

	// Should have checked for retry
	assert.True(t, taskMgr.shouldRetryCalled)
	assert.Equal(t, "task_1", taskMgr.shouldRetryTaskID)
}

func TestRunnerManager_HandleInFlightTasks_FailRunError(t *testing.T) {
	s := newTestRunnerStore()
	connMgr := newTestConnManager()
	logger := zap.NewNop()
	wrapperStore := newExtendedTestStoreWrapper(s)

	runnerID := "run_123"
	taskMgr := &mockTaskMgrForRunner{failRunErr: errors.New("fail run error")}
	manager := NewRunnerManager(wrapperStore, connMgr, logger, WithTaskManager(taskMgr))

	s.runners[runnerID] = &store.Runner{
		ID:     runnerID,
		Name:   "test-runner",
		Status: StatusBusy,
	}

	wrapperStore.taskRuns["trun_1"] = &store.TaskRun{
		ID:       "trun_1",
		TaskID:   "task_1",
		RunnerID: &runnerID,
		Status:   TaskRunStatusRunning,
	}

	// Should not return error (just logs)
	err := manager.OnDisconnect(context.Background(), runnerID)
	require.NoError(t, err)

	assert.True(t, taskMgr.failRunCalled)
	// ShouldRetry should not be called because FailRun failed
	assert.False(t, taskMgr.shouldRetryCalled)
}

// =============================================================================
// detachSessions Tests
// =============================================================================

func TestRunnerManager_DetachSessions_NoSessionManager(t *testing.T) {
	s := newTestRunnerStore()
	connMgr := newTestConnManager()
	logger := zap.NewNop()
	wrapperStore := newExtendedTestStoreWrapper(s)

	// Runner with no session manager
	manager := NewRunnerManager(wrapperStore, connMgr, logger)

	s.runners["run_123"] = &store.Runner{
		ID:     "run_123",
		Name:   "test-runner",
		Status: StatusIdle,
	}

	// Should succeed without session manager
	err := manager.OnDisconnect(context.Background(), "run_123")
	require.NoError(t, err)
}

func TestRunnerManager_DetachSessions_NoActiveSessions(t *testing.T) {
	s := newTestRunnerStore()
	connMgr := newTestConnManager()
	logger := zap.NewNop()
	wrapperStore := newExtendedTestStoreWrapper(s)

	sessionMgr := &mockSessionMgrForRunner{}
	manager := NewRunnerManager(wrapperStore, connMgr, logger, WithSessionManager(sessionMgr))

	s.runners["run_123"] = &store.Runner{
		ID:     "run_123",
		Name:   "test-runner",
		Status: StatusIdle,
	}

	// No sessions in store
	err := manager.OnDisconnect(context.Background(), "run_123")
	require.NoError(t, err)

	// SessionManager should not be called
	assert.False(t, sessionMgr.suspendCalled)
}

func TestRunnerManager_DetachSessions_WithActiveSessions(t *testing.T) {
	s := newTestRunnerStore()
	connMgr := newTestConnManager()
	logger := zap.NewNop()
	wrapperStore := newExtendedTestStoreWrapper(s)

	runnerID := "run_123"
	sessionMgr := &mockSessionMgrForRunner{}
	manager := NewRunnerManager(wrapperStore, connMgr, logger, WithSessionManager(sessionMgr))

	s.runners[runnerID] = &store.Runner{
		ID:     runnerID,
		Name:   "test-runner",
		Status: StatusIdle,
	}

	// Add active session
	wrapperStore.sessions["sess_1"] = &store.Session{
		ID:       "sess_1",
		RunnerID: &runnerID,
		Status:   SessionStatusActive,
	}

	err := manager.OnDisconnect(context.Background(), runnerID)
	require.NoError(t, err)

	// SessionManager should be called to suspend
	assert.True(t, sessionMgr.suspendCalled)
	assert.Equal(t, "sess_1", sessionMgr.suspendSessionID)
	assert.Equal(t, "terminate", sessionMgr.suspendStrategy)
}

func TestRunnerManager_DetachSessions_MultipleSessions(t *testing.T) {
	s := newTestRunnerStore()
	connMgr := newTestConnManager()
	logger := zap.NewNop()
	wrapperStore := newExtendedTestStoreWrapper(s)

	runnerID := "run_123"
	suspendCount := 0
	sessionMgr := &mockSessionMgrForRunner{}
	// Override suspend to count calls
	originalSuspend := sessionMgr.Suspend
	_ = originalSuspend
	manager := NewRunnerManager(wrapperStore, connMgr, logger, WithSessionManager(sessionMgr))

	s.runners[runnerID] = &store.Runner{
		ID:     runnerID,
		Name:   "test-runner",
		Status: StatusIdle,
	}

	// Add multiple active sessions
	wrapperStore.sessions["sess_1"] = &store.Session{
		ID:       "sess_1",
		RunnerID: &runnerID,
		Status:   SessionStatusActive,
	}
	wrapperStore.sessions["sess_2"] = &store.Session{
		ID:       "sess_2",
		RunnerID: &runnerID,
		Status:   SessionStatusActive,
	}

	err := manager.OnDisconnect(context.Background(), runnerID)
	require.NoError(t, err)

	// Count suspended sessions manually
	for _, sess := range wrapperStore.sessions {
		if sess.RunnerID != nil && *sess.RunnerID == runnerID && sess.Status == SessionStatusActive {
			suspendCount++
		}
	}

	// SessionManager should be called
	assert.True(t, sessionMgr.suspendCalled)
}

func TestRunnerManager_DetachSessions_SuspendError(t *testing.T) {
	s := newTestRunnerStore()
	connMgr := newTestConnManager()
	logger := zap.NewNop()
	wrapperStore := newExtendedTestStoreWrapper(s)

	runnerID := "run_123"
	sessionMgr := &mockSessionMgrForRunner{suspendErr: errors.New("suspend error")}
	manager := NewRunnerManager(wrapperStore, connMgr, logger, WithSessionManager(sessionMgr))

	s.runners[runnerID] = &store.Runner{
		ID:     runnerID,
		Name:   "test-runner",
		Status: StatusIdle,
	}

	wrapperStore.sessions["sess_1"] = &store.Session{
		ID:       "sess_1",
		RunnerID: &runnerID,
		Status:   SessionStatusActive,
	}

	// Should not return error (just logs)
	err := manager.OnDisconnect(context.Background(), runnerID)
	require.NoError(t, err)

	assert.True(t, sessionMgr.suspendCalled)
}
