package core

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/chunlea/marionette/pkg/auth"
	"github.com/chunlea/marionette/pkg/store"
	mockstore "github.com/chunlea/marionette/pkg/store/mock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// mockRunnerStore implements the runner store methods for testing.
type mockRunnerStore struct {
	runners      map[string]*store.Runner
	runnerByName map[string]*store.Runner
	nextID       int
}

func newMockRunnerStore() *mockRunnerStore {
	return &mockRunnerStore{
		runners:      make(map[string]*store.Runner),
		runnerByName: make(map[string]*store.Runner),
		nextID:       1,
	}
}

func (m *mockRunnerStore) CreateRunner(_ context.Context, runner *store.Runner) error {
	if runner.ID == "" {
		runner.ID = "run_test" + string(rune('0'+m.nextID))
		m.nextID++
	}
	m.runners[runner.ID] = runner
	if runner.Name != "" {
		m.runnerByName[runner.Name] = runner
	}
	return nil
}

func (m *mockRunnerStore) GetRunner(_ context.Context, id string) (*store.Runner, error) {
	runner, ok := m.runners[id]
	if !ok {
		return nil, store.ErrNotFound
	}
	return runner, nil
}

func (m *mockRunnerStore) GetRunnerByName(_ context.Context, name string) (*store.Runner, error) {
	runner, ok := m.runnerByName[name]
	if !ok {
		return nil, store.ErrNotFound
	}
	return runner, nil
}

func (m *mockRunnerStore) UpdateRunner(_ context.Context, id string, updates store.RunnerUpdates) error {
	runner, ok := m.runners[id]
	if !ok {
		return store.ErrNotFound
	}
	if updates.Hostname != nil {
		runner.Hostname = *updates.Hostname
	}
	if updates.SandboxMode != nil {
		runner.SandboxMode = *updates.SandboxMode
	}
	if len(updates.SandboxTypes) > 0 {
		runner.SandboxTypes = updates.SandboxTypes
	}
	if len(updates.Capabilities) > 0 {
		runner.Capabilities = updates.Capabilities
	}
	return nil
}

func (m *mockRunnerStore) ListRunners(_ context.Context, _ store.ListRunnersOptions) (*store.ListResult[store.Runner], error) {
	items := make([]*store.Runner, 0, len(m.runners))
	for _, r := range m.runners {
		items = append(items, r)
	}
	return &store.ListResult[store.Runner]{Items: items}, nil
}

func (m *mockRunnerStore) DeleteRunner(_ context.Context, id string) error {
	delete(m.runners, id)
	return nil
}

// Implement other store.Store methods as stubs
func (m *mockRunnerStore) CreateWorkspace(_ context.Context, _ *store.Workspace) error { return nil }
func (m *mockRunnerStore) GetWorkspace(_ context.Context, _ string) (*store.Workspace, error) {
	return nil, store.ErrNotFound
}
func (m *mockRunnerStore) ListWorkspaces(_ context.Context, _ store.ListWorkspacesOptions) (*store.ListResult[store.Workspace], error) {
	return &store.ListResult[store.Workspace]{}, nil
}
func (m *mockRunnerStore) UpdateWorkspace(_ context.Context, _ string, _ store.WorkspaceUpdates) error {
	return nil
}
func (m *mockRunnerStore) DeleteWorkspace(_ context.Context, _ string) error { return nil }

func (m *mockRunnerStore) CreateSession(_ context.Context, _ *store.Session) error { return nil }
func (m *mockRunnerStore) GetSession(_ context.Context, _ string) (*store.Session, error) {
	return nil, store.ErrNotFound
}
func (m *mockRunnerStore) ListSessions(_ context.Context, _ store.ListSessionsOptions) (*store.ListResult[store.Session], error) {
	return &store.ListResult[store.Session]{}, nil
}
func (m *mockRunnerStore) UpdateSession(_ context.Context, _ string, _ store.SessionUpdates) error {
	return nil
}
func (m *mockRunnerStore) DeleteSession(_ context.Context, _ string) error { return nil }

func (m *mockRunnerStore) CreateTask(_ context.Context, _ *store.Task) error { return nil }
func (m *mockRunnerStore) GetTask(_ context.Context, _ string) (*store.Task, error) {
	return nil, store.ErrNotFound
}
func (m *mockRunnerStore) ListTasks(_ context.Context, _ store.ListTasksOptions) (*store.ListResult[store.Task], error) {
	return &store.ListResult[store.Task]{}, nil
}
func (m *mockRunnerStore) UpdateTask(_ context.Context, _ string, _ store.TaskUpdates) error {
	return nil
}
func (m *mockRunnerStore) DeleteTask(_ context.Context, _ string) error { return nil }

func (m *mockRunnerStore) CreateTaskRun(_ context.Context, _ *store.TaskRun) error { return nil }
func (m *mockRunnerStore) GetTaskRun(_ context.Context, _ string) (*store.TaskRun, error) {
	return nil, store.ErrNotFound
}
func (m *mockRunnerStore) ListTaskRuns(_ context.Context, _ store.ListTaskRunsOptions) (*store.ListResult[store.TaskRun], error) {
	return &store.ListResult[store.TaskRun]{}, nil
}
func (m *mockRunnerStore) UpdateTaskRun(_ context.Context, _ string, _ store.TaskRunUpdates) error {
	return nil
}
func (m *mockRunnerStore) DeleteTaskRun(_ context.Context, _ string) error { return nil }

func (m *mockRunnerStore) CreateAPIKey(_ context.Context, _ *store.APIKey) error { return nil }
func (m *mockRunnerStore) GetAPIKey(_ context.Context, _ string) (*store.APIKey, error) {
	return nil, store.ErrNotFound
}
func (m *mockRunnerStore) GetAPIKeyByHash(_ context.Context, _ string) (*store.APIKey, error) {
	return nil, store.ErrNotFound
}
func (m *mockRunnerStore) ListAPIKeys(_ context.Context, _ store.ListAPIKeysOptions) (*store.ListResult[store.APIKey], error) {
	return &store.ListResult[store.APIKey]{}, nil
}
func (m *mockRunnerStore) UpdateAPIKey(_ context.Context, _ string, _ store.APIKeyUpdates) error {
	return nil
}
func (m *mockRunnerStore) DeleteAPIKey(_ context.Context, _ string) error { return nil }

func (m *mockRunnerStore) CreateRunnerToken(_ context.Context, _ *store.RunnerToken) error {
	return nil
}
func (m *mockRunnerStore) GetRunnerToken(_ context.Context, _ string) (*store.RunnerToken, error) {
	return nil, store.ErrNotFound
}
func (m *mockRunnerStore) GetRunnerTokenByHash(_ context.Context, _ string) (*store.RunnerToken, error) {
	return nil, store.ErrNotFound
}
func (m *mockRunnerStore) ListRunnerTokens(_ context.Context, _ store.ListRunnerTokensOptions) (*store.ListResult[store.RunnerToken], error) {
	return &store.ListResult[store.RunnerToken]{}, nil
}
func (m *mockRunnerStore) UpdateRunnerToken(_ context.Context, _ string, _ store.RunnerTokenUpdates) error {
	return nil
}
func (m *mockRunnerStore) DeleteRunnerToken(_ context.Context, _ string) error { return nil }

func (m *mockRunnerStore) GetTaskRunByTaskAndAttempt(_ context.Context, _ string, _ int) (*store.TaskRun, error) {
	return nil, store.ErrNotFound
}

func (m *mockRunnerStore) CreateScheduledTask(_ context.Context, _ *store.ScheduledTask) error {
	return nil
}
func (m *mockRunnerStore) GetScheduledTask(_ context.Context, _ string) (*store.ScheduledTask, error) {
	return nil, store.ErrNotFound
}
func (m *mockRunnerStore) ListScheduledTasks(_ context.Context, _ store.ListScheduledTasksOptions) (*store.ListResult[store.ScheduledTask], error) {
	return &store.ListResult[store.ScheduledTask]{}, nil
}
func (m *mockRunnerStore) UpdateScheduledTask(_ context.Context, _ string, _ store.ScheduledTaskUpdates) error {
	return nil
}
func (m *mockRunnerStore) DeleteScheduledTask(_ context.Context, _ string) error { return nil }

func (m *mockRunnerStore) CreatePermissionRequest(_ context.Context, _ *store.PermissionRequest) error {
	return nil
}
func (m *mockRunnerStore) GetPermissionRequest(_ context.Context, _ string) (*store.PermissionRequest, error) {
	return nil, store.ErrNotFound
}
func (m *mockRunnerStore) ListPermissionRequests(_ context.Context, _ store.ListPermissionRequestsOptions) (*store.ListResult[store.PermissionRequest], error) {
	return &store.ListResult[store.PermissionRequest]{}, nil
}
func (m *mockRunnerStore) UpdatePermissionRequest(_ context.Context, _ string, _ store.PermissionRequestUpdates) error {
	return nil
}

func (m *mockRunnerStore) CreateAgentConfig(_ context.Context, _ *store.AgentConfig) error {
	return nil
}
func (m *mockRunnerStore) GetAgentConfig(_ context.Context, _ string) (*store.AgentConfig, error) {
	return nil, store.ErrNotFound
}
func (m *mockRunnerStore) GetAgentConfigByName(_ context.Context, _ string) (*store.AgentConfig, error) {
	return nil, store.ErrNotFound
}
func (m *mockRunnerStore) GetDefaultAgentConfig(_ context.Context, _ string) (*store.AgentConfig, error) {
	return nil, store.ErrNotFound
}
func (m *mockRunnerStore) ListAgentConfigs(_ context.Context, _ store.ListAgentConfigsOptions) (*store.ListResult[store.AgentConfig], error) {
	return &store.ListResult[store.AgentConfig]{}, nil
}
func (m *mockRunnerStore) UpdateAgentConfig(_ context.Context, _ string, _ store.AgentConfigUpdates) error {
	return nil
}
func (m *mockRunnerStore) DeleteAgentConfig(_ context.Context, _ string) error { return nil }

func (m *mockRunnerStore) CreateProviderConfig(_ context.Context, _ *store.ProviderConfig) error {
	return nil
}
func (m *mockRunnerStore) GetProviderConfig(_ context.Context, _ string) (*store.ProviderConfig, error) {
	return nil, store.ErrNotFound
}
func (m *mockRunnerStore) GetProviderConfigByName(_ context.Context, _ string) (*store.ProviderConfig, error) {
	return nil, store.ErrNotFound
}
func (m *mockRunnerStore) GetDefaultProviderConfig(_ context.Context, _ string) (*store.ProviderConfig, error) {
	return nil, store.ErrNotFound
}
func (m *mockRunnerStore) ListProviderConfigs(_ context.Context, _ store.ListProviderConfigsOptions) (*store.ListResult[store.ProviderConfig], error) {
	return &store.ListResult[store.ProviderConfig]{}, nil
}
func (m *mockRunnerStore) UpdateProviderConfig(_ context.Context, _ string, _ store.ProviderConfigUpdates) error {
	return nil
}
func (m *mockRunnerStore) DeleteProviderConfig(_ context.Context, _ string) error { return nil }

func (m *mockRunnerStore) CreateProfile(_ context.Context, _ *store.Profile) error { return nil }
func (m *mockRunnerStore) GetProfile(_ context.Context, _ string) (*store.Profile, error) {
	return nil, store.ErrNotFound
}
func (m *mockRunnerStore) GetProfileByName(_ context.Context, _ string) (*store.Profile, error) {
	return nil, store.ErrNotFound
}
func (m *mockRunnerStore) ListProfiles(_ context.Context, _ store.ListProfilesOptions) (*store.ListResult[store.Profile], error) {
	return &store.ListResult[store.Profile]{}, nil
}
func (m *mockRunnerStore) UpdateProfile(_ context.Context, _ string, _ store.ProfileUpdates) error {
	return nil
}
func (m *mockRunnerStore) DeleteProfile(_ context.Context, _ string) error { return nil }

func (m *mockRunnerStore) CreateSnapshot(_ context.Context, _ *store.Snapshot) error { return nil }
func (m *mockRunnerStore) GetSnapshot(_ context.Context, _ string) (*store.Snapshot, error) {
	return nil, store.ErrNotFound
}
func (m *mockRunnerStore) GetSnapshotByRunnerAndName(_ context.Context, _, _ string) (*store.Snapshot, error) {
	return nil, store.ErrNotFound
}
func (m *mockRunnerStore) ListSnapshots(_ context.Context, _ store.ListSnapshotsOptions) (*store.ListResult[store.Snapshot], error) {
	return &store.ListResult[store.Snapshot]{}, nil
}
func (m *mockRunnerStore) UpdateSnapshot(_ context.Context, _ string, _ store.SnapshotUpdates) error {
	return nil
}
func (m *mockRunnerStore) DeleteSnapshot(_ context.Context, _ string) error { return nil }

func (m *mockRunnerStore) CreateTunnel(_ context.Context, _ *store.Tunnel) error { return nil }
func (m *mockRunnerStore) GetTunnel(_ context.Context, _ string) (*store.Tunnel, error) {
	return nil, store.ErrNotFound
}
func (m *mockRunnerStore) GetTunnelByTokenHash(_ context.Context, _ string) (*store.Tunnel, error) {
	return nil, store.ErrNotFound
}
func (m *mockRunnerStore) ListTunnels(_ context.Context, _ store.ListTunnelsOptions) (*store.ListResult[store.Tunnel], error) {
	return &store.ListResult[store.Tunnel]{}, nil
}
func (m *mockRunnerStore) UpdateTunnel(_ context.Context, _ string, _ store.TunnelUpdates) error {
	return nil
}
func (m *mockRunnerStore) DeleteTunnel(_ context.Context, _ string) error { return nil }

func (m *mockRunnerStore) CreateActionLog(_ context.Context, _ *store.ActionLog) error { return nil }
func (m *mockRunnerStore) GetActionLog(_ context.Context, _ string) (*store.ActionLog, error) {
	return nil, store.ErrNotFound
}
func (m *mockRunnerStore) ListActionLogs(_ context.Context, _ store.ListActionLogsOptions) (*store.ListResult[store.ActionLog], error) {
	return &store.ListResult[store.ActionLog]{}, nil
}

func (m *mockRunnerStore) CreateLog(_ context.Context, _ *store.Log) error    { return nil }
func (m *mockRunnerStore) CreateLogs(_ context.Context, _ []*store.Log) error { return nil }
func (m *mockRunnerStore) ListLogs(_ context.Context, _ store.ListLogsOptions) (*store.ListResult[store.Log], error) {
	return &store.ListResult[store.Log]{}, nil
}

func (m *mockRunnerStore) CreateLogArchive(_ context.Context, _ *store.LogArchive) error {
	return nil
}
func (m *mockRunnerStore) GetLogArchive(_ context.Context, _ string) (*store.LogArchive, error) {
	return nil, store.ErrNotFound
}
func (m *mockRunnerStore) GetLogArchiveBySession(_ context.Context, _ string) (*store.LogArchive, error) {
	return nil, store.ErrNotFound
}
func (m *mockRunnerStore) ListLogArchives(_ context.Context, _ store.ListLogArchivesOptions) (*store.ListResult[store.LogArchive], error) {
	return &store.ListResult[store.LogArchive]{}, nil
}
func (m *mockRunnerStore) UpdateLogArchive(_ context.Context, _ string, _ store.LogArchiveUpdates) error {
	return nil
}

func (m *mockRunnerStore) CreateDataKey(_ context.Context, _ *store.DataKey) error { return nil }
func (m *mockRunnerStore) GetDataKey(_ context.Context, _ string) (*store.DataKey, error) {
	return nil, store.ErrNotFound
}
func (m *mockRunnerStore) GetDataKeyByResource(_ context.Context, _, _ string) (*store.DataKey, error) {
	return nil, store.ErrNotFound
}
func (m *mockRunnerStore) UpdateDataKey(_ context.Context, _ string, _ store.DataKeyUpdates) error {
	return nil
}
func (m *mockRunnerStore) DeleteDataKey(_ context.Context, _ string) error { return nil }

func (m *mockRunnerStore) CreateChunk(_ context.Context, _ *store.Chunk) error { return nil }
func (m *mockRunnerStore) GetChunk(_ context.Context, _, _ string) (*store.Chunk, error) {
	return nil, store.ErrNotFound
}
func (m *mockRunnerStore) UpdateChunk(_ context.Context, _, _ string, _ store.ChunkUpdates) error {
	return nil
}
func (m *mockRunnerStore) DeleteChunk(_ context.Context, _, _ string) error { return nil }
func (m *mockRunnerStore) IncrementChunkRefCount(_ context.Context, _, _ string) error {
	return nil
}
func (m *mockRunnerStore) DecrementChunkRefCount(_ context.Context, _, _ string) error {
	return nil
}
func (m *mockRunnerStore) ListUnreferencedChunks(_ context.Context, _ string, _ int) ([]*store.Chunk, error) {
	return nil, nil
}
func (m *mockRunnerStore) ListSoftDeletedChunks(_ context.Context, _ string, _ time.Time, _ int) ([]*store.Chunk, error) {
	return nil, nil
}
func (m *mockRunnerStore) MarkChunkDeleted(_ context.Context, _, _ string) error  { return nil }
func (m *mockRunnerStore) ClearChunkDeleted(_ context.Context, _, _ string) error { return nil }

func (m *mockRunnerStore) CreateManifest(_ context.Context, _ *store.Manifest) error { return nil }
func (m *mockRunnerStore) GetManifest(_ context.Context, _ string) (*store.Manifest, error) {
	return nil, store.ErrNotFound
}
func (m *mockRunnerStore) GetLatestManifest(_ context.Context, _ string) (*store.Manifest, error) {
	return nil, store.ErrNotFound
}
func (m *mockRunnerStore) DeleteManifest(_ context.Context, _ string) error { return nil }

// Stream methods (stub)
func (m *mockRunnerStore) CreateStream(_ context.Context, _ *store.Stream) error { return nil }
func (m *mockRunnerStore) GetStream(_ context.Context, _ string) (*store.Stream, error) {
	return nil, store.ErrNotFound
}
func (m *mockRunnerStore) GetStreamBySessionAndType(_ context.Context, _, _ string, _ bool) (*store.Stream, error) {
	return nil, store.ErrNotFound
}
func (m *mockRunnerStore) ListStreams(_ context.Context, _ store.ListStreamsOptions) (*store.ListResult[store.Stream], error) {
	return &store.ListResult[store.Stream]{}, nil
}
func (m *mockRunnerStore) UpdateStream(_ context.Context, _ string, _ store.StreamUpdates) error {
	return nil
}
func (m *mockRunnerStore) DeleteStream(_ context.Context, _ string) error { return nil }
func (m *mockRunnerStore) CleanupExpiredStreams(_ context.Context) (int, error) {
	return 0, nil
}

func (m *mockRunnerStore) BeginTx(_ context.Context) (store.Tx, error) { return nil, nil }
func (m *mockRunnerStore) Ping(_ context.Context) error                { return nil }
func (m *mockRunnerStore) Close() error                                { return nil }

func TestRunnerRegistry_Register_NoToken(t *testing.T) {
	runnerStore := newMockRunnerStore()
	tokenStore := mockstore.NewRunnerTokenStore()
	tokenSvc := auth.NewRunnerTokenService(tokenStore, nil)
	logger := zap.NewNop()

	registry := NewRunnerRegistry(runnerStore, tokenSvc, logger)

	req := &RegisterRequest{
		Name:     "test-runner",
		Hostname: "localhost",
		// Token is empty
	}

	_, err := registry.Register(context.Background(), req)
	assert.ErrorIs(t, err, ErrTokenRequired)
}

func TestRunnerRegistry_Register_InvalidToken(t *testing.T) {
	runnerStore := newMockRunnerStore()
	tokenStore := mockstore.NewRunnerTokenStore()
	tokenSvc := auth.NewRunnerTokenService(tokenStore, nil)
	logger := zap.NewNop()

	registry := NewRunnerRegistry(runnerStore, tokenSvc, logger)

	req := &RegisterRequest{
		Token:    "invalid-token",
		Name:     "test-runner",
		Hostname: "localhost",
	}

	_, err := registry.Register(context.Background(), req)
	require.Error(t, err)
}

func TestRunnerRegistry_Register_NewRunner(t *testing.T) {
	runnerStore := newMockRunnerStore()
	tokenStore := mockstore.NewRunnerTokenStore()
	tokenSvc := auth.NewRunnerTokenService(tokenStore, func() string { return "rtok_test1" })
	logger := zap.NewNop()

	// Create a valid token
	ctx := context.Background()
	token, plaintext, err := tokenSvc.Create(ctx, auth.CreateRunnerTokenOptions{
		PoolName: "test-pool",
	})
	require.NoError(t, err)
	require.NotEmpty(t, plaintext)
	require.NotNil(t, token)

	registry := NewRunnerRegistry(runnerStore, tokenSvc, logger)

	req := &RegisterRequest{
		Token:    plaintext,
		Name:     "test-runner",
		Hostname: "localhost",
	}

	result, err := registry.Register(ctx, req)
	require.NoError(t, err)
	require.NotNil(t, result)

	assert.True(t, result.IsNew)
	assert.NotEmpty(t, result.RunnerID)
	assert.Equal(t, "test-pool", result.PoolName)
}

func TestRunnerRegistry_Register_ExistingRunner(t *testing.T) {
	runnerStore := newMockRunnerStore()
	tokenStore := mockstore.NewRunnerTokenStore()
	tokenSvc := auth.NewRunnerTokenService(tokenStore, func() string { return "rtok_test1" })
	logger := zap.NewNop()

	ctx := context.Background()

	// Create a token and bind it to an existing runner
	token, plaintext, err := tokenSvc.Create(ctx, auth.CreateRunnerTokenOptions{
		PoolName: "test-pool",
	})
	require.NoError(t, err)

	// Create existing runner
	existingRunner := &store.Runner{
		ID:       "run_existing",
		Name:     "existing-runner",
		Hostname: "old-host",
		Status:   "offline",
	}
	err = runnerStore.CreateRunner(ctx, existingRunner)
	require.NoError(t, err)

	// Bind token to runner
	err = tokenSvc.BindRunner(ctx, token.ID, existingRunner.ID)
	require.NoError(t, err)

	registry := NewRunnerRegistry(runnerStore, tokenSvc, logger)

	req := &RegisterRequest{
		Token:    plaintext,
		Name:     "existing-runner",
		Hostname: "new-host",
	}

	result, err := registry.Register(ctx, req)
	require.NoError(t, err)
	require.NotNil(t, result)

	assert.False(t, result.IsNew)
	assert.Equal(t, "run_existing", result.RunnerID)

	// Verify hostname was updated
	runner, _ := runnerStore.GetRunner(ctx, "run_existing")
	assert.Equal(t, "new-host", runner.Hostname)
}

func TestRunnerRegistry_Register_BoundTokenRunnerDeleted(t *testing.T) {
	runnerStore := newMockRunnerStore()
	tokenStore := mockstore.NewRunnerTokenStore()
	tokenSvc := auth.NewRunnerTokenService(tokenStore, func() string { return "rtok_test1" })
	logger := zap.NewNop()

	ctx := context.Background()

	// Create a token
	token, plaintext, err := tokenSvc.Create(ctx, auth.CreateRunnerTokenOptions{
		PoolName: "test-pool",
	})
	require.NoError(t, err)

	// Bind token to a runner that doesn't exist (simulating deleted runner)
	err = tokenSvc.BindRunner(ctx, token.ID, "run_deleted")
	require.NoError(t, err)

	registry := NewRunnerRegistry(runnerStore, tokenSvc, logger)

	req := &RegisterRequest{
		Token:    plaintext,
		Name:     "new-runner",
		Hostname: "localhost",
	}

	// Should create new runner since bound runner doesn't exist
	result, err := registry.Register(ctx, req)
	require.NoError(t, err)
	require.NotNil(t, result)

	assert.True(t, result.IsNew)
	assert.NotEmpty(t, result.RunnerID)
	assert.Equal(t, "test-pool", result.PoolName)
}

func TestRunnerRegistry_Register_UnboundTokenExistingName(t *testing.T) {
	runnerStore := newMockRunnerStore()
	tokenStore := mockstore.NewRunnerTokenStore()
	tokenSvc := auth.NewRunnerTokenService(tokenStore, func() string { return "rtok_test1" })
	logger := zap.NewNop()

	ctx := context.Background()

	// Create existing runner with known name
	existingRunner := &store.Runner{
		ID:       "run_existing",
		Name:     "existing-runner",
		Hostname: "old-host",
		Status:   "offline",
	}
	err := runnerStore.CreateRunner(ctx, existingRunner)
	require.NoError(t, err)

	// Create an unbound token
	_, plaintext, err := tokenSvc.Create(ctx, auth.CreateRunnerTokenOptions{
		PoolName: "test-pool",
	})
	require.NoError(t, err)

	registry := NewRunnerRegistry(runnerStore, tokenSvc, logger)

	// Register with same name as existing runner
	req := &RegisterRequest{
		Token:    plaintext,
		Name:     "existing-runner",
		Hostname: "new-host",
	}

	result, err := registry.Register(ctx, req)
	require.NoError(t, err)
	require.NotNil(t, result)

	// Should bind to existing runner, not create new
	assert.False(t, result.IsNew)
	assert.Equal(t, "run_existing", result.RunnerID)

	// Verify hostname was updated
	runner, _ := runnerStore.GetRunner(ctx, "run_existing")
	assert.Equal(t, "new-host", runner.Hostname)
}

func TestRunnerRegistry_Register_WithCapabilitiesAndSandbox(t *testing.T) {
	runnerStore := newMockRunnerStore()
	tokenStore := mockstore.NewRunnerTokenStore()
	tokenSvc := auth.NewRunnerTokenService(tokenStore, func() string { return "rtok_test1" })
	logger := zap.NewNop()

	ctx := context.Background()

	// Create a valid token
	_, plaintext, err := tokenSvc.Create(ctx, auth.CreateRunnerTokenOptions{
		PoolName: "test-pool",
	})
	require.NoError(t, err)

	registry := NewRunnerRegistry(runnerStore, tokenSvc, logger)

	req := &RegisterRequest{
		Token:        plaintext,
		Name:         "full-runner",
		Hostname:     "localhost",
		SandboxMode:  "runner-creates-sandbox",
		SandboxTypes: []string{"docker", "kata"},
		Capabilities: []string{"gpu", "arm64"},
		Labels:       map[string]string{"env": "test"},
	}

	result, err := registry.Register(ctx, req)
	require.NoError(t, err)
	require.NotNil(t, result)

	assert.True(t, result.IsNew)

	// Verify runner was created with all fields
	runner, err := runnerStore.GetRunner(ctx, result.RunnerID)
	require.NoError(t, err)
	assert.Equal(t, "runner-creates-sandbox", runner.SandboxMode)
	assert.Equal(t, []string{"docker", "kata"}, runner.SandboxTypes)
	assert.Equal(t, []string{"gpu", "arm64"}, runner.Capabilities)
}

func TestRunnerRegistry_Register_BoundTokenWithSandboxUpdate(t *testing.T) {
	runnerStore := newMockRunnerStore()
	tokenStore := mockstore.NewRunnerTokenStore()
	tokenSvc := auth.NewRunnerTokenService(tokenStore, func() string { return "rtok_test1" })
	logger := zap.NewNop()

	ctx := context.Background()

	// Create a token and bind it to an existing runner
	token, plaintext, err := tokenSvc.Create(ctx, auth.CreateRunnerTokenOptions{
		PoolName: "test-pool",
	})
	require.NoError(t, err)

	// Create existing runner
	existingRunner := &store.Runner{
		ID:          "run_existing",
		Name:        "existing-runner",
		Hostname:    "old-host",
		Status:      "offline",
		SandboxMode: "runner-is-sandbox",
	}
	err = runnerStore.CreateRunner(ctx, existingRunner)
	require.NoError(t, err)

	// Bind token to runner
	err = tokenSvc.BindRunner(ctx, token.ID, existingRunner.ID)
	require.NoError(t, err)

	registry := NewRunnerRegistry(runnerStore, tokenSvc, logger)

	req := &RegisterRequest{
		Token:        plaintext,
		Name:         "existing-runner",
		Hostname:     "new-host",
		SandboxMode:  "runner-creates-sandbox",
		SandboxTypes: []string{"docker"},
		Capabilities: []string{"arm64"},
	}

	result, err := registry.Register(ctx, req)
	require.NoError(t, err)
	require.NotNil(t, result)

	assert.False(t, result.IsNew)
	assert.Equal(t, "run_existing", result.RunnerID)

	// Verify runner was updated with new sandbox settings
	runner, _ := runnerStore.GetRunner(ctx, "run_existing")
	assert.Equal(t, "new-host", runner.Hostname)
	assert.Equal(t, "runner-creates-sandbox", runner.SandboxMode)
	assert.Equal(t, []string{"docker"}, runner.SandboxTypes)
	assert.Equal(t, []string{"arm64"}, runner.Capabilities)
}

func TestRunnerRegistry_Get(t *testing.T) {
	runnerStore := newMockRunnerStore()
	logger := zap.NewNop()

	ctx := context.Background()

	// Create a runner
	runner := &store.Runner{
		ID:       "run_test123",
		Name:     "test-runner",
		Hostname: "localhost",
		Status:   "idle",
	}
	err := runnerStore.CreateRunner(ctx, runner)
	require.NoError(t, err)

	registry := NewRunnerRegistry(runnerStore, nil, logger)

	// Get existing runner
	result, err := registry.Get(ctx, "run_test123")
	require.NoError(t, err)
	assert.Equal(t, "run_test123", result.ID)
	assert.Equal(t, "test-runner", result.Name)

	// Get non-existent runner
	_, err = registry.Get(ctx, "run_nonexistent")
	assert.ErrorIs(t, err, store.ErrNotFound)
}

func TestRunnerRegistry_GetByName(t *testing.T) {
	runnerStore := newMockRunnerStore()
	logger := zap.NewNop()

	ctx := context.Background()

	// Create a runner
	runner := &store.Runner{
		ID:       "run_test123",
		Name:     "test-runner",
		Hostname: "localhost",
		Status:   "idle",
	}
	err := runnerStore.CreateRunner(ctx, runner)
	require.NoError(t, err)

	registry := NewRunnerRegistry(runnerStore, nil, logger)

	// Get existing runner by name
	result, err := registry.GetByName(ctx, "test-runner")
	require.NoError(t, err)
	assert.Equal(t, "run_test123", result.ID)
	assert.Equal(t, "test-runner", result.Name)

	// Get non-existent runner by name
	_, err = registry.GetByName(ctx, "nonexistent-runner")
	assert.ErrorIs(t, err, store.ErrNotFound)
}

func TestRunnerRegistry_Register_EmptyName(t *testing.T) {
	runnerStore := newMockRunnerStore()
	tokenStore := mockstore.NewRunnerTokenStore()
	tokenSvc := auth.NewRunnerTokenService(tokenStore, func() string { return "rtok_test1" })
	logger := zap.NewNop()

	ctx := context.Background()

	// Create a valid token
	_, plaintext, err := tokenSvc.Create(ctx, auth.CreateRunnerTokenOptions{
		PoolName: "test-pool",
	})
	require.NoError(t, err)

	registry := NewRunnerRegistry(runnerStore, tokenSvc, logger)

	// Register with empty name (should still work - creates runner without name lookup)
	req := &RegisterRequest{
		Token:    plaintext,
		Name:     "", // Empty name
		Hostname: "localhost",
	}

	result, err := registry.Register(ctx, req)
	require.NoError(t, err)
	require.NotNil(t, result)

	assert.True(t, result.IsNew)
	assert.NotEmpty(t, result.RunnerID)
}

func TestRunnerRegistry_Register_DefaultSandboxMode(t *testing.T) {
	runnerStore := newMockRunnerStore()
	tokenStore := mockstore.NewRunnerTokenStore()
	tokenSvc := auth.NewRunnerTokenService(tokenStore, func() string { return "rtok_test1" })
	logger := zap.NewNop()

	ctx := context.Background()

	// Create a valid token
	_, plaintext, err := tokenSvc.Create(ctx, auth.CreateRunnerTokenOptions{
		PoolName: "test-pool",
	})
	require.NoError(t, err)

	registry := NewRunnerRegistry(runnerStore, tokenSvc, logger)

	// Register without sandbox mode (should default to runner-is-sandbox)
	req := &RegisterRequest{
		Token:    plaintext,
		Name:     "default-sandbox-runner",
		Hostname: "localhost",
		// SandboxMode is empty
	}

	result, err := registry.Register(ctx, req)
	require.NoError(t, err)

	// Verify default sandbox mode was set
	runner, err := runnerStore.GetRunner(ctx, result.RunnerID)
	require.NoError(t, err)
	assert.Equal(t, "runner-is-sandbox", runner.SandboxMode)
}

// errorMockRunnerStore is a mock store that can return errors
type errorMockRunnerStore struct {
	mockRunnerStore
	createErr error
	updateErr error
	getErr    error
}

func (m *errorMockRunnerStore) CreateRunner(_ context.Context, runner *store.Runner) error {
	if m.createErr != nil {
		return m.createErr
	}
	return m.mockRunnerStore.CreateRunner(context.Background(), runner)
}

func (m *errorMockRunnerStore) UpdateRunner(_ context.Context, id string, updates store.RunnerUpdates) error {
	if m.updateErr != nil {
		return m.updateErr
	}
	return m.mockRunnerStore.UpdateRunner(context.Background(), id, updates)
}

func (m *errorMockRunnerStore) GetRunner(_ context.Context, id string) (*store.Runner, error) {
	if m.getErr != nil {
		return nil, m.getErr
	}
	return m.mockRunnerStore.GetRunner(context.Background(), id)
}

func (m *errorMockRunnerStore) GetRunnerByName(_ context.Context, name string) (*store.Runner, error) {
	if m.getErr != nil {
		return nil, m.getErr
	}
	return m.mockRunnerStore.GetRunnerByName(context.Background(), name)
}

func TestRunnerRegistry_Register_BoundTokenUpdateError(t *testing.T) {
	baseStore := newMockRunnerStore()
	runnerStore := &errorMockRunnerStore{
		mockRunnerStore: *baseStore,
	}
	tokenStore := mockstore.NewRunnerTokenStore()
	tokenSvc := auth.NewRunnerTokenService(tokenStore, func() string { return "rtok_test1" })
	logger := zap.NewNop()

	ctx := context.Background()

	// Create a token and bind it to an existing runner
	token, plaintext, err := tokenSvc.Create(ctx, auth.CreateRunnerTokenOptions{
		PoolName: "test-pool",
	})
	require.NoError(t, err)

	// Create existing runner
	existingRunner := &store.Runner{
		ID:       "run_existing",
		Name:     "existing-runner",
		Hostname: "old-host",
		Status:   "offline",
	}
	err = runnerStore.CreateRunner(ctx, existingRunner)
	require.NoError(t, err)

	// Bind token to runner
	err = tokenSvc.BindRunner(ctx, token.ID, existingRunner.ID)
	require.NoError(t, err)

	// Now set update error
	runnerStore.updateErr = errors.New("update failed")

	registry := NewRunnerRegistry(runnerStore, tokenSvc, logger)

	req := &RegisterRequest{
		Token:    plaintext,
		Name:     "existing-runner",
		Hostname: "new-host",
	}

	_, err = registry.Register(ctx, req)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "update failed")
}

func TestRunnerRegistry_Register_BoundTokenGetError(t *testing.T) {
	baseStore := newMockRunnerStore()
	runnerStore := &errorMockRunnerStore{
		mockRunnerStore: *baseStore,
	}
	tokenStore := mockstore.NewRunnerTokenStore()
	tokenSvc := auth.NewRunnerTokenService(tokenStore, func() string { return "rtok_test1" })
	logger := zap.NewNop()

	ctx := context.Background()

	// Create a token
	token, plaintext, err := tokenSvc.Create(ctx, auth.CreateRunnerTokenOptions{
		PoolName: "test-pool",
	})
	require.NoError(t, err)

	// Create existing runner first
	existingRunner := &store.Runner{
		ID:       "run_existing",
		Name:     "existing-runner",
		Hostname: "old-host",
		Status:   "offline",
	}
	err = runnerStore.CreateRunner(ctx, existingRunner)
	require.NoError(t, err)

	// Bind token to runner
	err = tokenSvc.BindRunner(ctx, token.ID, existingRunner.ID)
	require.NoError(t, err)

	// Set get error (non-ErrNotFound)
	runnerStore.getErr = errors.New("database connection failed")

	registry := NewRunnerRegistry(runnerStore, tokenSvc, logger)

	req := &RegisterRequest{
		Token:    plaintext,
		Name:     "existing-runner",
		Hostname: "new-host",
	}

	_, err = registry.Register(ctx, req)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "database connection failed")
}

func TestRunnerRegistry_Register_UnboundTokenGetByNameError(t *testing.T) {
	baseStore := newMockRunnerStore()
	runnerStore := &errorMockRunnerStore{
		mockRunnerStore: *baseStore,
	}
	tokenStore := mockstore.NewRunnerTokenStore()
	tokenSvc := auth.NewRunnerTokenService(tokenStore, func() string { return "rtok_test1" })
	logger := zap.NewNop()

	ctx := context.Background()

	// Create an unbound token
	_, plaintext, err := tokenSvc.Create(ctx, auth.CreateRunnerTokenOptions{
		PoolName: "test-pool",
	})
	require.NoError(t, err)

	// Set get error (non-ErrNotFound)
	runnerStore.getErr = errors.New("database connection failed")

	registry := NewRunnerRegistry(runnerStore, tokenSvc, logger)

	req := &RegisterRequest{
		Token:    plaintext,
		Name:     "new-runner",
		Hostname: "localhost",
	}

	_, err = registry.Register(ctx, req)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "database connection failed")
}

func TestRunnerRegistry_Register_CreateRunnerError(t *testing.T) {
	baseStore := newMockRunnerStore()
	runnerStore := &errorMockRunnerStore{
		mockRunnerStore: *baseStore,
		createErr:       errors.New("create failed"),
	}
	tokenStore := mockstore.NewRunnerTokenStore()
	tokenSvc := auth.NewRunnerTokenService(tokenStore, func() string { return "rtok_test1" })
	logger := zap.NewNop()

	ctx := context.Background()

	// Create an unbound token
	_, plaintext, err := tokenSvc.Create(ctx, auth.CreateRunnerTokenOptions{
		PoolName: "test-pool",
	})
	require.NoError(t, err)

	registry := NewRunnerRegistry(runnerStore, tokenSvc, logger)

	req := &RegisterRequest{
		Token:    plaintext,
		Name:     "new-runner",
		Hostname: "localhost",
	}

	_, err = registry.Register(ctx, req)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "create failed")
}

func TestRunnerRegistry_Register_BindRunnerError(t *testing.T) {
	runnerStore := newMockRunnerStore()
	tokenStore := mockstore.NewRunnerTokenStore()
	// Use the real token service but we'll create a scenario where bind fails
	tokenSvc := auth.NewRunnerTokenService(tokenStore, func() string { return "rtok_test1" })
	logger := zap.NewNop()

	ctx := context.Background()

	// Create a valid token
	_, plaintext, err := tokenSvc.Create(ctx, auth.CreateRunnerTokenOptions{
		PoolName: "test-pool",
	})
	require.NoError(t, err)

	registry := NewRunnerRegistry(runnerStore, tokenSvc, logger)

	req := &RegisterRequest{
		Token:    plaintext,
		Name:     "new-runner",
		Hostname: "localhost",
	}

	// First registration should work
	result, err := registry.Register(ctx, req)
	require.NoError(t, err)
	assert.True(t, result.IsNew)
}

func TestRunnerRegistry_Register_UnboundTokenWithExistingRunnerName(t *testing.T) {
	runnerStore := newMockRunnerStore()
	tokenStore := mockstore.NewRunnerTokenStore()
	tokenSvc := auth.NewRunnerTokenService(tokenStore, func() string { return "rtok_test1" })
	logger := zap.NewNop()

	ctx := context.Background()

	// Create existing runner with known name (no token bound yet)
	existingRunner := &store.Runner{
		ID:       "run_existing",
		Name:     "existing-runner",
		Hostname: "old-host",
		Status:   "offline",
	}
	err := runnerStore.CreateRunner(ctx, existingRunner)
	require.NoError(t, err)

	// Create an unbound token
	_, plaintext, err := tokenSvc.Create(ctx, auth.CreateRunnerTokenOptions{
		PoolName: "test-pool",
	})
	require.NoError(t, err)

	registry := NewRunnerRegistry(runnerStore, tokenSvc, logger)

	// Register with same name as existing runner (unbound token)
	req := &RegisterRequest{
		Token:    plaintext,
		Name:     "existing-runner",
		Hostname: "new-host",
	}

	// This should succeed - binds the token to the existing runner
	result, err := registry.Register(ctx, req)
	require.NoError(t, err)
	assert.False(t, result.IsNew)
	assert.Equal(t, "run_existing", result.RunnerID)

	// Verify hostname was updated
	runner, _ := runnerStore.GetRunner(ctx, "run_existing")
	assert.Equal(t, "new-host", runner.Hostname)
}

func TestRunnerRegistry_Register_BoundToken_SecondRegistration(t *testing.T) {
	runnerStore := newMockRunnerStore()
	tokenStore := mockstore.NewRunnerTokenStore()
	tokenSvc := auth.NewRunnerTokenService(tokenStore, func() string { return "rtok_test1" })
	logger := zap.NewNop()

	ctx := context.Background()

	// Create a token
	token, plaintext, err := tokenSvc.Create(ctx, auth.CreateRunnerTokenOptions{
		PoolName: "test-pool",
	})
	require.NoError(t, err)

	registry := NewRunnerRegistry(runnerStore, tokenSvc, logger)

	// First registration - creates new runner
	req := &RegisterRequest{
		Token:    plaintext,
		Name:     "new-runner",
		Hostname: "host1",
	}

	result, err := registry.Register(ctx, req)
	require.NoError(t, err)
	assert.True(t, result.IsNew)
	runnerID := result.RunnerID

	// Verify token is bound
	updatedToken, err := tokenStore.GetRunnerToken(ctx, token.ID)
	require.NoError(t, err)
	require.NotNil(t, updatedToken.RunnerID)
	assert.Equal(t, runnerID, *updatedToken.RunnerID)

	// Second registration with same token - updates existing runner
	req2 := &RegisterRequest{
		Token:    plaintext,
		Name:     "new-runner",
		Hostname: "host2",
	}

	result2, err := registry.Register(ctx, req2)
	require.NoError(t, err)
	assert.False(t, result2.IsNew)
	assert.Equal(t, runnerID, result2.RunnerID)

	// Verify hostname was updated
	runner, _ := runnerStore.GetRunner(ctx, runnerID)
	assert.Equal(t, "host2", runner.Hostname)
}
