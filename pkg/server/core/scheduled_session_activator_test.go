package core

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/chunlea/marionette/pkg/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// mockSessionMgrForActivator implements SessionManagerInterface for testing.
type mockSessionMgrForActivator struct {
	mu          sync.Mutex
	resumeFunc  func(ctx context.Context, sessionID string) error
	resumeCalls []string
}

func (m *mockSessionMgrForActivator) Create(_ context.Context, _ CreateSessionOptions) (*store.Session, error) {
	return nil, nil
}

func (m *mockSessionMgrForActivator) Get(_ context.Context, _ string) (*store.Session, error) {
	return nil, nil
}

func (m *mockSessionMgrForActivator) List(_ context.Context, _ ListSessionsOptions) (*store.ListResult[store.Session], error) {
	return nil, nil
}

func (m *mockSessionMgrForActivator) Activate(_ context.Context, _, _ string) error {
	return nil
}

func (m *mockSessionMgrForActivator) Suspend(_ context.Context, _, _ string) error {
	return nil
}

func (m *mockSessionMgrForActivator) Resume(ctx context.Context, sessionID string) error {
	m.mu.Lock()
	m.resumeCalls = append(m.resumeCalls, sessionID)
	m.mu.Unlock()

	if m.resumeFunc != nil {
		return m.resumeFunc(ctx, sessionID)
	}
	return nil
}

func (m *mockSessionMgrForActivator) Terminate(_ context.Context, _ string) error {
	return nil
}

func (m *mockSessionMgrForActivator) AttachRunner(_ context.Context, _, _ string) error {
	return nil
}

func (m *mockSessionMgrForActivator) DetachRunner(_ context.Context, _ string) error {
	return nil
}

func (m *mockSessionMgrForActivator) UpdateContextSnapshot(_ context.Context, _ string, _ *ContextSnapshot) error {
	return nil
}

// activatorTestStore implements store.Store for the activator tests.
type activatorTestStore struct {
	mu                sync.Mutex
	getDueFunc        func(ctx context.Context, now time.Time, limit int) ([]*store.Session, error)
	updateSessionFunc func(ctx context.Context, id string, updates store.SessionUpdates) error
	getDueCalls       int
	updateCalls       []string
}

func (s *activatorTestStore) GetDueScheduledSessions(ctx context.Context, now time.Time, limit int) ([]*store.Session, error) {
	s.mu.Lock()
	s.getDueCalls++
	s.mu.Unlock()

	if s.getDueFunc != nil {
		return s.getDueFunc(ctx, now, limit)
	}
	return nil, nil
}

func (s *activatorTestStore) UpdateSession(ctx context.Context, id string, updates store.SessionUpdates) error {
	s.mu.Lock()
	s.updateCalls = append(s.updateCalls, id)
	s.mu.Unlock()

	if s.updateSessionFunc != nil {
		return s.updateSessionFunc(ctx, id, updates)
	}
	return nil
}

// Stub out all other store methods.
func (s *activatorTestStore) CreateSession(_ context.Context, _ *store.Session) error { return nil }
func (s *activatorTestStore) GetSession(_ context.Context, _ string) (*store.Session, error) {
	return nil, nil
}
func (s *activatorTestStore) ListSessions(_ context.Context, _ store.ListSessionsOptions) (*store.ListResult[store.Session], error) {
	return nil, nil
}
func (s *activatorTestStore) DeleteSession(_ context.Context, _ string) error { return nil }

func (s *activatorTestStore) CreateRunner(_ context.Context, _ *store.Runner) error { return nil }
func (s *activatorTestStore) GetRunner(_ context.Context, _ string) (*store.Runner, error) {
	return nil, nil
}
func (s *activatorTestStore) GetRunnerByName(_ context.Context, _ string) (*store.Runner, error) {
	return nil, nil
}
func (s *activatorTestStore) ListRunners(_ context.Context, _ store.ListRunnersOptions) (*store.ListResult[store.Runner], error) {
	return nil, nil
}
func (s *activatorTestStore) UpdateRunner(_ context.Context, _ string, _ store.RunnerUpdates) error {
	return nil
}
func (s *activatorTestStore) DeleteRunner(_ context.Context, _ string) error { return nil }

func (s *activatorTestStore) CreateTask(_ context.Context, _ *store.Task) error { return nil }
func (s *activatorTestStore) GetTask(_ context.Context, _ string) (*store.Task, error) {
	return nil, nil
}
func (s *activatorTestStore) ListTasks(_ context.Context, _ store.ListTasksOptions) (*store.ListResult[store.Task], error) {
	return nil, nil
}
func (s *activatorTestStore) UpdateTask(_ context.Context, _ string, _ store.TaskUpdates) error {
	return nil
}
func (s *activatorTestStore) DeleteTask(_ context.Context, _ string) error { return nil }

func (s *activatorTestStore) CreateTaskRun(_ context.Context, _ *store.TaskRun) error { return nil }
func (s *activatorTestStore) GetTaskRun(_ context.Context, _ string) (*store.TaskRun, error) {
	return nil, nil
}
func (s *activatorTestStore) GetTaskRunByTaskAndAttempt(_ context.Context, _ string, _ int) (*store.TaskRun, error) {
	return nil, nil
}
func (s *activatorTestStore) ListTaskRuns(_ context.Context, _ store.ListTaskRunsOptions) (*store.ListResult[store.TaskRun], error) {
	return nil, nil
}
func (s *activatorTestStore) UpdateTaskRun(_ context.Context, _ string, _ store.TaskRunUpdates) error {
	return nil
}

func (s *activatorTestStore) CreateWorkspace(_ context.Context, _ *store.Workspace) error { return nil }
func (s *activatorTestStore) GetWorkspace(_ context.Context, _ string) (*store.Workspace, error) {
	return nil, nil
}
func (s *activatorTestStore) ListWorkspaces(_ context.Context, _ store.ListWorkspacesOptions) (*store.ListResult[store.Workspace], error) {
	return nil, nil
}
func (s *activatorTestStore) UpdateWorkspace(_ context.Context, _ string, _ store.WorkspaceUpdates) error {
	return nil
}
func (s *activatorTestStore) DeleteWorkspace(_ context.Context, _ string) error { return nil }

func (s *activatorTestStore) CreatePermissionRequest(_ context.Context, _ *store.PermissionRequest) error {
	return nil
}
func (s *activatorTestStore) GetPermissionRequest(_ context.Context, _ string) (*store.PermissionRequest, error) {
	return nil, nil
}
func (s *activatorTestStore) ListPermissionRequests(_ context.Context, _ store.ListPermissionRequestsOptions) (*store.ListResult[store.PermissionRequest], error) {
	return nil, nil
}
func (s *activatorTestStore) UpdatePermissionRequest(_ context.Context, _ string, _ store.PermissionRequestUpdates) error {
	return nil
}
func (s *activatorTestStore) DeletePermissionRequest(_ context.Context, _ string) error { return nil }
func (s *activatorTestStore) GetPendingPermissionByOriginalID(_ context.Context, _, _ string) (*store.PermissionRequest, error) {
	return nil, nil
}

func (s *activatorTestStore) CreateAPIKey(_ context.Context, _ *store.APIKey) error { return nil }
func (s *activatorTestStore) GetAPIKey(_ context.Context, _ string) (*store.APIKey, error) {
	return nil, nil
}
func (s *activatorTestStore) GetAPIKeyByHash(_ context.Context, _ string) (*store.APIKey, error) {
	return nil, nil
}
func (s *activatorTestStore) ListAPIKeys(_ context.Context, _ store.ListAPIKeysOptions) (*store.ListResult[store.APIKey], error) {
	return nil, nil
}
func (s *activatorTestStore) UpdateAPIKey(_ context.Context, _ string, _ store.APIKeyUpdates) error {
	return nil
}
func (s *activatorTestStore) DeleteAPIKey(_ context.Context, _ string) error { return nil }

func (s *activatorTestStore) CreateRunnerToken(_ context.Context, _ *store.RunnerToken) error {
	return nil
}
func (s *activatorTestStore) GetRunnerToken(_ context.Context, _ string) (*store.RunnerToken, error) {
	return nil, nil
}
func (s *activatorTestStore) GetRunnerTokenByHash(_ context.Context, _ string) (*store.RunnerToken, error) {
	return nil, nil
}
func (s *activatorTestStore) ListRunnerTokens(_ context.Context, _ store.ListRunnerTokensOptions) (*store.ListResult[store.RunnerToken], error) {
	return nil, nil
}
func (s *activatorTestStore) UpdateRunnerToken(_ context.Context, _ string, _ store.RunnerTokenUpdates) error {
	return nil
}
func (s *activatorTestStore) DeleteRunnerToken(_ context.Context, _ string) error { return nil }

func (s *activatorTestStore) CreateTunnel(_ context.Context, _ *store.Tunnel) error { return nil }
func (s *activatorTestStore) GetTunnel(_ context.Context, _ string) (*store.Tunnel, error) {
	return nil, nil
}
func (s *activatorTestStore) ListTunnels(_ context.Context, _ store.ListTunnelsOptions) (*store.ListResult[store.Tunnel], error) {
	return nil, nil
}
func (s *activatorTestStore) UpdateTunnel(_ context.Context, _ string, _ store.TunnelUpdates) error {
	return nil
}
func (s *activatorTestStore) DeleteTunnel(_ context.Context, _ string) error { return nil }

func (s *activatorTestStore) CreateAgentConfig(_ context.Context, _ *store.AgentConfig) error {
	return nil
}
func (s *activatorTestStore) GetAgentConfig(_ context.Context, _ string) (*store.AgentConfig, error) {
	return nil, nil
}
func (s *activatorTestStore) GetAgentConfigByName(_ context.Context, _ string) (*store.AgentConfig, error) {
	return nil, nil
}
func (s *activatorTestStore) GetDefaultAgentConfig(_ context.Context, _ string) (*store.AgentConfig, error) {
	return nil, nil
}
func (s *activatorTestStore) ListAgentConfigs(_ context.Context, _ store.ListAgentConfigsOptions) (*store.ListResult[store.AgentConfig], error) {
	return nil, nil
}
func (s *activatorTestStore) UpdateAgentConfig(_ context.Context, _ string, _ store.AgentConfigUpdates) error {
	return nil
}
func (s *activatorTestStore) DeleteAgentConfig(_ context.Context, _ string) error { return nil }

func (s *activatorTestStore) CreateProviderConfig(_ context.Context, _ *store.ProviderConfig) error {
	return nil
}
func (s *activatorTestStore) GetProviderConfig(_ context.Context, _ string) (*store.ProviderConfig, error) {
	return nil, nil
}
func (s *activatorTestStore) GetProviderConfigByName(_ context.Context, _ string) (*store.ProviderConfig, error) {
	return nil, nil
}
func (s *activatorTestStore) GetDefaultProviderConfig(_ context.Context, _ string) (*store.ProviderConfig, error) {
	return nil, nil
}
func (s *activatorTestStore) ListProviderConfigs(_ context.Context, _ store.ListProviderConfigsOptions) (*store.ListResult[store.ProviderConfig], error) {
	return nil, nil
}
func (s *activatorTestStore) UpdateProviderConfig(_ context.Context, _ string, _ store.ProviderConfigUpdates) error {
	return nil
}
func (s *activatorTestStore) DeleteProviderConfig(_ context.Context, _ string) error { return nil }

func (s *activatorTestStore) CreateProfile(_ context.Context, _ *store.Profile) error { return nil }
func (s *activatorTestStore) GetProfile(_ context.Context, _ string) (*store.Profile, error) {
	return nil, nil
}
func (s *activatorTestStore) GetProfileByName(_ context.Context, _ string) (*store.Profile, error) {
	return nil, nil
}
func (s *activatorTestStore) ListProfiles(_ context.Context, _ store.ListProfilesOptions) (*store.ListResult[store.Profile], error) {
	return nil, nil
}
func (s *activatorTestStore) UpdateProfile(_ context.Context, _ string, _ store.ProfileUpdates) error {
	return nil
}
func (s *activatorTestStore) DeleteProfile(_ context.Context, _ string) error { return nil }

func (s *activatorTestStore) CreateSnapshot(_ context.Context, _ *store.Snapshot) error { return nil }
func (s *activatorTestStore) GetSnapshot(_ context.Context, _ string) (*store.Snapshot, error) {
	return nil, nil
}
func (s *activatorTestStore) GetSnapshotByRunnerAndName(_ context.Context, _, _ string) (*store.Snapshot, error) {
	return nil, nil
}
func (s *activatorTestStore) ListSnapshots(_ context.Context, _ store.ListSnapshotsOptions) (*store.ListResult[store.Snapshot], error) {
	return nil, nil
}
func (s *activatorTestStore) UpdateSnapshot(_ context.Context, _ string, _ store.SnapshotUpdates) error {
	return nil
}
func (s *activatorTestStore) DeleteSnapshot(_ context.Context, _ string) error { return nil }

func (s *activatorTestStore) CreateLog(_ context.Context, _ *store.Log) error { return nil }
func (s *activatorTestStore) ListLogs(_ context.Context, _ store.ListLogsOptions) (*store.ListResult[store.Log], error) {
	return nil, nil
}

func (s *activatorTestStore) CreateActionLog(_ context.Context, _ *store.ActionLog) error { return nil }
func (s *activatorTestStore) ListActionLogs(_ context.Context, _ store.ListActionLogsOptions) (*store.ListResult[store.ActionLog], error) {
	return nil, nil
}

func (s *activatorTestStore) CreateScheduledTask(_ context.Context, _ *store.ScheduledTask) error {
	return nil
}
func (s *activatorTestStore) GetScheduledTask(_ context.Context, _ string) (*store.ScheduledTask, error) {
	return nil, nil
}
func (s *activatorTestStore) ListScheduledTasks(_ context.Context, _ store.ListScheduledTasksOptions) (*store.ListResult[store.ScheduledTask], error) {
	return nil, nil
}
func (s *activatorTestStore) UpdateScheduledTask(_ context.Context, _ string, _ store.ScheduledTaskUpdates) error {
	return nil
}
func (s *activatorTestStore) DeleteScheduledTask(_ context.Context, _ string) error { return nil }
func (s *activatorTestStore) GetDueScheduledTasks(_ context.Context, _ time.Time, _ int) ([]*store.ScheduledTask, error) {
	return nil, nil
}

func (s *activatorTestStore) Ping(_ context.Context) error { return nil }
func (s *activatorTestStore) Close() error                 { return nil }
func (s *activatorTestStore) BeginTx(_ context.Context) (store.Tx, error) {
	return nil, nil
}

// Tunnel additional methods
func (s *activatorTestStore) GetTunnelByTokenHash(_ context.Context, _ string) (*store.Tunnel, error) {
	return nil, nil
}

// ActionLog additional methods
func (s *activatorTestStore) GetActionLog(_ context.Context, _ string) (*store.ActionLog, error) {
	return nil, nil
}

// Log methods
func (s *activatorTestStore) CreateLogs(_ context.Context, _ []*store.Log) error { return nil }

// LogArchive methods
func (s *activatorTestStore) CreateLogArchive(_ context.Context, _ *store.LogArchive) error {
	return nil
}
func (s *activatorTestStore) GetLogArchive(_ context.Context, _ string) (*store.LogArchive, error) {
	return nil, nil
}
func (s *activatorTestStore) GetLogArchiveBySession(_ context.Context, _ string) (*store.LogArchive, error) {
	return nil, nil
}
func (s *activatorTestStore) ListLogArchives(_ context.Context, _ store.ListLogArchivesOptions) (*store.ListResult[store.LogArchive], error) {
	return nil, nil
}
func (s *activatorTestStore) UpdateLogArchive(_ context.Context, _ string, _ store.LogArchiveUpdates) error {
	return nil
}

// DataKey methods
func (s *activatorTestStore) CreateDataKey(_ context.Context, _ *store.DataKey) error { return nil }
func (s *activatorTestStore) GetDataKey(_ context.Context, _ string) (*store.DataKey, error) {
	return nil, nil
}
func (s *activatorTestStore) GetDataKeyByResource(_ context.Context, _, _ string) (*store.DataKey, error) {
	return nil, nil
}
func (s *activatorTestStore) UpdateDataKey(_ context.Context, _ string, _ store.DataKeyUpdates) error {
	return nil
}
func (s *activatorTestStore) DeleteDataKey(_ context.Context, _ string) error { return nil }

// Chunk methods
func (s *activatorTestStore) CreateChunk(_ context.Context, _ *store.Chunk) error { return nil }
func (s *activatorTestStore) GetChunk(_ context.Context, _, _ string) (*store.Chunk, error) {
	return nil, nil
}
func (s *activatorTestStore) UpdateChunk(_ context.Context, _, _ string, _ store.ChunkUpdates) error {
	return nil
}
func (s *activatorTestStore) IncrementChunkRefCount(_ context.Context, _, _ string) error { return nil }
func (s *activatorTestStore) DecrementChunkRefCount(_ context.Context, _, _ string) error { return nil }
func (s *activatorTestStore) DeleteChunk(_ context.Context, _, _ string) error            { return nil }
func (s *activatorTestStore) ListUnreferencedChunks(_ context.Context, _ string, _ int) ([]*store.Chunk, error) {
	return nil, nil
}
func (s *activatorTestStore) ListSoftDeletedChunks(_ context.Context, _ string, _ time.Time, _ int) ([]*store.Chunk, error) {
	return nil, nil
}
func (s *activatorTestStore) MarkChunkDeleted(_ context.Context, _, _ string) error  { return nil }
func (s *activatorTestStore) ClearChunkDeleted(_ context.Context, _, _ string) error { return nil }

// Manifest methods
func (s *activatorTestStore) CreateManifest(_ context.Context, _ *store.Manifest) error { return nil }
func (s *activatorTestStore) GetManifest(_ context.Context, _ string) (*store.Manifest, error) {
	return nil, nil
}
func (s *activatorTestStore) GetLatestManifest(_ context.Context, _ string) (*store.Manifest, error) {
	return nil, nil
}
func (s *activatorTestStore) DeleteManifest(_ context.Context, _ string) error { return nil }

// Stream methods
func (s *activatorTestStore) CreateStream(_ context.Context, _ *store.Stream) error { return nil }
func (s *activatorTestStore) GetStream(_ context.Context, _ string) (*store.Stream, error) {
	return nil, nil
}
func (s *activatorTestStore) GetStreamBySessionAndType(_ context.Context, _, _ string, _ bool) (*store.Stream, error) {
	return nil, nil
}
func (s *activatorTestStore) ListStreams(_ context.Context, _ store.ListStreamsOptions) (*store.ListResult[store.Stream], error) {
	return nil, nil
}
func (s *activatorTestStore) UpdateStream(_ context.Context, _ string, _ store.StreamUpdates) error {
	return nil
}
func (s *activatorTestStore) DeleteStream(_ context.Context, _ string) error { return nil }
func (s *activatorTestStore) CleanupExpiredStreams(_ context.Context) (int, error) {
	return 0, nil
}

func TestNewScheduledSessionActivator(t *testing.T) {
	logger := zap.NewNop()
	mockStore := &activatorTestStore{}
	mockSessionMgr := &mockSessionMgrForActivator{}

	t.Run("default values", func(t *testing.T) {
		activator := NewScheduledSessionActivator(ScheduledSessionActivatorConfig{
			Store:      mockStore,
			SessionMgr: mockSessionMgr,
			Logger:     logger,
		})
		require.NotNil(t, activator)
		assert.Equal(t, 30*time.Second, activator.checkInterval)
		assert.Equal(t, 50, activator.batchSize)
	})

	t.Run("custom values", func(t *testing.T) {
		activator := NewScheduledSessionActivator(ScheduledSessionActivatorConfig{
			Store:         mockStore,
			SessionMgr:    mockSessionMgr,
			CheckInterval: 10 * time.Second,
			BatchSize:     100,
			Logger:        logger,
		})
		require.NotNil(t, activator)
		assert.Equal(t, 10*time.Second, activator.checkInterval)
		assert.Equal(t, 100, activator.batchSize)
	})
}

func TestScheduledSessionActivator_StartStop(t *testing.T) {
	logger := zap.NewNop()
	mockStore := &activatorTestStore{}
	mockSessionMgr := &mockSessionMgrForActivator{}

	t.Run("start and stop", func(t *testing.T) {
		activator := NewScheduledSessionActivator(ScheduledSessionActivatorConfig{
			Store:         mockStore,
			SessionMgr:    mockSessionMgr,
			CheckInterval: 100 * time.Millisecond,
			Logger:        logger,
		})

		err := activator.Start()
		require.NoError(t, err)

		// Wait a bit for it to process
		time.Sleep(150 * time.Millisecond)

		activator.Stop()
	})

	t.Run("double start is no-op", func(t *testing.T) {
		activator := NewScheduledSessionActivator(ScheduledSessionActivatorConfig{
			Store:         mockStore,
			SessionMgr:    mockSessionMgr,
			CheckInterval: 100 * time.Millisecond,
			Logger:        logger,
		})

		err := activator.Start()
		require.NoError(t, err)

		// Second start should be no-op
		err = activator.Start()
		require.NoError(t, err)

		activator.Stop()
	})

	t.Run("double stop is no-op", func(t *testing.T) {
		activator := NewScheduledSessionActivator(ScheduledSessionActivatorConfig{
			Store:         mockStore,
			SessionMgr:    mockSessionMgr,
			CheckInterval: 100 * time.Millisecond,
			Logger:        logger,
		})

		// Stop without starting - should be no-op
		activator.Stop()

		err := activator.Start()
		require.NoError(t, err)

		activator.Stop()
		activator.Stop() // Second stop should be no-op
	})
}

func TestScheduledSessionActivator_CheckAndActivate(t *testing.T) {
	logger := zap.NewNop()

	t.Run("no due sessions", func(t *testing.T) {
		mockStore := &activatorTestStore{
			getDueFunc: func(_ context.Context, _ time.Time, _ int) ([]*store.Session, error) {
				return nil, nil
			},
		}
		mockSessionMgr := &mockSessionMgrForActivator{}

		activator := NewScheduledSessionActivator(ScheduledSessionActivatorConfig{
			Store:         mockStore,
			SessionMgr:    mockSessionMgr,
			CheckInterval: 100 * time.Millisecond,
			Logger:        logger,
		})

		err := activator.Start()
		require.NoError(t, err)

		time.Sleep(150 * time.Millisecond)
		activator.Stop()

		assert.GreaterOrEqual(t, mockStore.getDueCalls, 1)
		assert.Len(t, mockSessionMgr.resumeCalls, 0)
	})

	t.Run("due sessions are activated", func(t *testing.T) {
		scheduleCron := "0 9 * * *"
		scheduleTimezone := "UTC"

		mockStore := &activatorTestStore{
			getDueFunc: func(_ context.Context, _ time.Time, _ int) ([]*store.Session, error) {
				return []*store.Session{
					{
						ID:               "sess_1",
						ScheduleCron:     &scheduleCron,
						ScheduleTimezone: &scheduleTimezone,
						LifecycleMode:    "scheduled",
					},
					{
						ID:               "sess_2",
						ScheduleCron:     &scheduleCron,
						ScheduleTimezone: &scheduleTimezone,
						LifecycleMode:    "scheduled",
					},
				}, nil
			},
		}
		mockSessionMgr := &mockSessionMgrForActivator{}

		activator := NewScheduledSessionActivator(ScheduledSessionActivatorConfig{
			Store:         mockStore,
			SessionMgr:    mockSessionMgr,
			CheckInterval: 100 * time.Millisecond,
			Logger:        logger,
		})

		err := activator.Start()
		require.NoError(t, err)

		time.Sleep(150 * time.Millisecond)
		activator.Stop()

		// Both sessions should be resumed
		assert.GreaterOrEqual(t, len(mockSessionMgr.resumeCalls), 2)
	})

	t.Run("GetDueScheduledSessions error", func(t *testing.T) {
		mockStore := &activatorTestStore{
			getDueFunc: func(_ context.Context, _ time.Time, _ int) ([]*store.Session, error) {
				return nil, errors.New("database error")
			},
		}
		mockSessionMgr := &mockSessionMgrForActivator{}

		activator := NewScheduledSessionActivator(ScheduledSessionActivatorConfig{
			Store:         mockStore,
			SessionMgr:    mockSessionMgr,
			CheckInterval: 100 * time.Millisecond,
			Logger:        logger,
		})

		err := activator.Start()
		require.NoError(t, err)

		time.Sleep(150 * time.Millisecond)
		activator.Stop()

		// No sessions should be resumed on error
		assert.Len(t, mockSessionMgr.resumeCalls, 0)
	})
}

func TestScheduledSessionActivator_ActivateSession(t *testing.T) {
	logger := zap.NewNop()

	t.Run("resume error", func(t *testing.T) {
		scheduleCron := "0 9 * * *"
		scheduleTimezone := "UTC"

		mockStore := &activatorTestStore{
			getDueFunc: func(_ context.Context, _ time.Time, _ int) ([]*store.Session, error) {
				return []*store.Session{
					{
						ID:               "sess_1",
						ScheduleCron:     &scheduleCron,
						ScheduleTimezone: &scheduleTimezone,
						LifecycleMode:    "scheduled",
					},
				}, nil
			},
		}
		mockSessionMgr := &mockSessionMgrForActivator{
			resumeFunc: func(_ context.Context, _ string) error {
				return errors.New("resume failed")
			},
		}

		activator := NewScheduledSessionActivator(ScheduledSessionActivatorConfig{
			Store:         mockStore,
			SessionMgr:    mockSessionMgr,
			CheckInterval: 100 * time.Millisecond,
			Logger:        logger,
		})

		err := activator.Start()
		require.NoError(t, err)

		time.Sleep(150 * time.Millisecond)
		activator.Stop()

		// Resume was called but failed
		assert.GreaterOrEqual(t, len(mockSessionMgr.resumeCalls), 1)
		// Session should not be updated if resume failed
		assert.Len(t, mockStore.updateCalls, 0)
	})

	t.Run("UpdateSession error", func(t *testing.T) {
		scheduleCron := "0 9 * * *"
		scheduleTimezone := "UTC"

		mockStore := &activatorTestStore{
			getDueFunc: func(_ context.Context, _ time.Time, _ int) ([]*store.Session, error) {
				return []*store.Session{
					{
						ID:               "sess_1",
						ScheduleCron:     &scheduleCron,
						ScheduleTimezone: &scheduleTimezone,
						LifecycleMode:    "scheduled",
					},
				}, nil
			},
			updateSessionFunc: func(_ context.Context, _ string, _ store.SessionUpdates) error {
				return errors.New("update failed")
			},
		}
		mockSessionMgr := &mockSessionMgrForActivator{}

		activator := NewScheduledSessionActivator(ScheduledSessionActivatorConfig{
			Store:         mockStore,
			SessionMgr:    mockSessionMgr,
			CheckInterval: 100 * time.Millisecond,
			Logger:        logger,
		})

		err := activator.Start()
		require.NoError(t, err)

		time.Sleep(150 * time.Millisecond)
		activator.Stop()

		// Both resume and update were called
		assert.GreaterOrEqual(t, len(mockSessionMgr.resumeCalls), 1)
		assert.GreaterOrEqual(t, len(mockStore.updateCalls), 1)
	})

	t.Run("invalid cron expression", func(t *testing.T) {
		invalidCron := "invalid"
		scheduleTimezone := "UTC"

		mockStore := &activatorTestStore{
			getDueFunc: func(_ context.Context, _ time.Time, _ int) ([]*store.Session, error) {
				return []*store.Session{
					{
						ID:               "sess_1",
						ScheduleCron:     &invalidCron,
						ScheduleTimezone: &scheduleTimezone,
						LifecycleMode:    "scheduled",
					},
				}, nil
			},
		}
		mockSessionMgr := &mockSessionMgrForActivator{}

		activator := NewScheduledSessionActivator(ScheduledSessionActivatorConfig{
			Store:         mockStore,
			SessionMgr:    mockSessionMgr,
			CheckInterval: 100 * time.Millisecond,
			Logger:        logger,
		})

		err := activator.Start()
		require.NoError(t, err)

		time.Sleep(150 * time.Millisecond)
		activator.Stop()

		// Resume was called
		assert.GreaterOrEqual(t, len(mockSessionMgr.resumeCalls), 1)
		// But update should not be called for invalid cron
		assert.Len(t, mockStore.updateCalls, 0)
	})

	t.Run("nil schedule cron", func(t *testing.T) {
		mockStore := &activatorTestStore{
			getDueFunc: func(_ context.Context, _ time.Time, _ int) ([]*store.Session, error) {
				return []*store.Session{
					{
						ID:            "sess_1",
						ScheduleCron:  nil,
						LifecycleMode: "scheduled",
					},
				}, nil
			},
		}
		mockSessionMgr := &mockSessionMgrForActivator{}

		activator := NewScheduledSessionActivator(ScheduledSessionActivatorConfig{
			Store:         mockStore,
			SessionMgr:    mockSessionMgr,
			CheckInterval: 100 * time.Millisecond,
			Logger:        logger,
		})

		err := activator.Start()
		require.NoError(t, err)

		time.Sleep(150 * time.Millisecond)
		activator.Stop()

		// Resume was called
		assert.GreaterOrEqual(t, len(mockSessionMgr.resumeCalls), 1)
		// But update should not be called for nil cron
		assert.Len(t, mockStore.updateCalls, 0)
	})

	t.Run("empty schedule cron", func(t *testing.T) {
		emptyCron := ""
		mockStore := &activatorTestStore{
			getDueFunc: func(_ context.Context, _ time.Time, _ int) ([]*store.Session, error) {
				return []*store.Session{
					{
						ID:            "sess_1",
						ScheduleCron:  &emptyCron,
						LifecycleMode: "scheduled",
					},
				}, nil
			},
		}
		mockSessionMgr := &mockSessionMgrForActivator{}

		activator := NewScheduledSessionActivator(ScheduledSessionActivatorConfig{
			Store:         mockStore,
			SessionMgr:    mockSessionMgr,
			CheckInterval: 100 * time.Millisecond,
			Logger:        logger,
		})

		err := activator.Start()
		require.NoError(t, err)

		time.Sleep(150 * time.Millisecond)
		activator.Stop()

		// Resume was called
		assert.GreaterOrEqual(t, len(mockSessionMgr.resumeCalls), 1)
		// But update should not be called for empty cron
		assert.Len(t, mockStore.updateCalls, 0)
	})

	t.Run("invalid timezone uses UTC", func(t *testing.T) {
		scheduleCron := "0 9 * * *"
		invalidTimezone := "Invalid/Timezone"

		mockStore := &activatorTestStore{
			getDueFunc: func(_ context.Context, _ time.Time, _ int) ([]*store.Session, error) {
				return []*store.Session{
					{
						ID:               "sess_1",
						ScheduleCron:     &scheduleCron,
						ScheduleTimezone: &invalidTimezone,
						LifecycleMode:    "scheduled",
					},
				}, nil
			},
		}
		mockSessionMgr := &mockSessionMgrForActivator{}

		activator := NewScheduledSessionActivator(ScheduledSessionActivatorConfig{
			Store:         mockStore,
			SessionMgr:    mockSessionMgr,
			CheckInterval: 100 * time.Millisecond,
			Logger:        logger,
		})

		err := activator.Start()
		require.NoError(t, err)

		time.Sleep(150 * time.Millisecond)
		activator.Stop()

		// Resume and update should both be called (uses UTC as fallback)
		assert.GreaterOrEqual(t, len(mockSessionMgr.resumeCalls), 1)
		assert.GreaterOrEqual(t, len(mockStore.updateCalls), 1)
	})
}

func TestScheduledSessionActivator_PeriodicPolling(t *testing.T) {
	logger := zap.NewNop()
	var callCount int
	var mu sync.Mutex

	mockStore := &activatorTestStore{
		getDueFunc: func(_ context.Context, _ time.Time, _ int) ([]*store.Session, error) {
			mu.Lock()
			callCount++
			mu.Unlock()
			return nil, nil
		},
	}
	mockSessionMgr := &mockSessionMgrForActivator{}

	activator := NewScheduledSessionActivator(ScheduledSessionActivatorConfig{
		Store:         mockStore,
		SessionMgr:    mockSessionMgr,
		CheckInterval: 50 * time.Millisecond,
		Logger:        logger,
	})

	err := activator.Start()
	require.NoError(t, err)

	// Wait for multiple polls
	time.Sleep(180 * time.Millisecond)

	activator.Stop()

	mu.Lock()
	finalCount := callCount
	mu.Unlock()

	// Should have been called at least 2-3 times (initial + 2-3 ticks)
	require.GreaterOrEqual(t, finalCount, 2, "Expected at least 2 poll cycles")
}
