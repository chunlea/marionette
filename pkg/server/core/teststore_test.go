package core

import (
	"context"
	"sync"
	"time"

	"github.com/chunlea/marionette/pkg/store"
)

// testStore is an in-memory store implementation for testing.
type testStore struct {
	mu sync.RWMutex

	workspaces         map[string]*store.Workspace
	sessions           map[string]*store.Session
	tasks              map[string]*store.Task
	taskRuns           map[string]*store.TaskRun
	permissionRequests map[string]*store.PermissionRequest
	logs               []*store.Log
}

func newTestStore() *testStore {
	return &testStore{
		workspaces:         make(map[string]*store.Workspace),
		sessions:           make(map[string]*store.Session),
		tasks:              make(map[string]*store.Task),
		taskRuns:           make(map[string]*store.TaskRun),
		permissionRequests: make(map[string]*store.PermissionRequest),
		logs:               make([]*store.Log, 0),
	}
}

// Workspace methods
func (s *testStore) CreateWorkspace(_ context.Context, ws *store.Workspace) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.workspaces[ws.ID] = ws
	return nil
}

func (s *testStore) GetWorkspace(_ context.Context, id string) (*store.Workspace, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	ws, ok := s.workspaces[id]
	if !ok {
		return nil, store.ErrNotFound
	}
	return ws, nil
}

func (s *testStore) ListWorkspaces(_ context.Context, _ store.ListWorkspacesOptions) (*store.ListResult[store.Workspace], error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	items := make([]*store.Workspace, 0, len(s.workspaces))
	for _, ws := range s.workspaces {
		items = append(items, ws)
	}
	return &store.ListResult[store.Workspace]{Items: items}, nil
}

func (s *testStore) UpdateWorkspace(_ context.Context, _ string, _ store.WorkspaceUpdates) error {
	return nil
}

func (s *testStore) DeleteWorkspace(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.workspaces, id)
	return nil
}

// Session methods
func (s *testStore) CreateSession(_ context.Context, sess *store.Session) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessions[sess.ID] = sess
	return nil
}

func (s *testStore) GetSession(_ context.Context, id string) (*store.Session, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	sess, ok := s.sessions[id]
	if !ok {
		return nil, store.ErrNotFound
	}
	return sess, nil
}

func (s *testStore) ListSessions(_ context.Context, opts store.ListSessionsOptions) (*store.ListResult[store.Session], error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	items := make([]*store.Session, 0, len(s.sessions))
	for _, sess := range s.sessions {
		// Filter by runner ID if specified
		if opts.RunnerID != nil && (sess.RunnerID == nil || *sess.RunnerID != *opts.RunnerID) {
			continue
		}
		// Filter by status if specified
		if len(opts.Status) > 0 {
			found := false
			for _, status := range opts.Status {
				if sess.Status == status {
					found = true
					break
				}
			}
			if !found {
				continue
			}
		}
		items = append(items, sess)
	}
	return &store.ListResult[store.Session]{Items: items}, nil
}

func (s *testStore) UpdateSession(_ context.Context, id string, updates store.SessionUpdates) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	sess, ok := s.sessions[id]
	if !ok {
		return store.ErrNotFound
	}
	if updates.Status != nil {
		sess.Status = *updates.Status
	}
	if updates.RunnerID != nil {
		sess.RunnerID = updates.RunnerID
	}
	return nil
}

func (s *testStore) DeleteSession(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sessions, id)
	return nil
}

// Task methods
func (s *testStore) CreateTask(_ context.Context, task *store.Task) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tasks[task.ID] = task
	return nil
}

func (s *testStore) GetTask(_ context.Context, id string) (*store.Task, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	task, ok := s.tasks[id]
	if !ok {
		return nil, store.ErrNotFound
	}
	return task, nil
}

func (s *testStore) ListTasks(_ context.Context, opts store.ListTasksOptions) (*store.ListResult[store.Task], error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	items := make([]*store.Task, 0, len(s.tasks))
	for _, task := range s.tasks {
		// Filter by session ID if specified
		if opts.SessionID != nil && task.SessionID != *opts.SessionID {
			continue
		}
		// Filter by status if specified
		if len(opts.Status) > 0 {
			found := false
			for _, status := range opts.Status {
				if task.Status == status {
					found = true
					break
				}
			}
			if !found {
				continue
			}
		}
		items = append(items, task)
	}
	return &store.ListResult[store.Task]{Items: items}, nil
}

func (s *testStore) UpdateTask(_ context.Context, id string, updates store.TaskUpdates) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	task, ok := s.tasks[id]
	if !ok {
		return store.ErrNotFound
	}
	if updates.Status != nil {
		task.Status = *updates.Status
	}
	return nil
}

func (s *testStore) DeleteTask(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.tasks, id)
	return nil
}

// TaskRun methods
func (s *testStore) CreateTaskRun(_ context.Context, run *store.TaskRun) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.taskRuns[run.ID] = run
	return nil
}

func (s *testStore) GetTaskRun(_ context.Context, id string) (*store.TaskRun, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	run, ok := s.taskRuns[id]
	if !ok {
		return nil, store.ErrNotFound
	}
	return run, nil
}

func (s *testStore) ListTaskRuns(_ context.Context, opts store.ListTaskRunsOptions) (*store.ListResult[store.TaskRun], error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	items := make([]*store.TaskRun, 0, len(s.taskRuns))
	for _, run := range s.taskRuns {
		// Filter by task ID if specified
		if opts.TaskID != nil && run.TaskID != *opts.TaskID {
			continue
		}
		// Filter by status if specified
		if len(opts.Status) > 0 {
			found := false
			for _, status := range opts.Status {
				if run.Status == status {
					found = true
					break
				}
			}
			if !found {
				continue
			}
		}
		items = append(items, run)
	}
	return &store.ListResult[store.TaskRun]{Items: items}, nil
}

func (s *testStore) UpdateTaskRun(_ context.Context, id string, updates store.TaskRunUpdates) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	run, ok := s.taskRuns[id]
	if !ok {
		return store.ErrNotFound
	}
	if updates.Status != nil {
		run.Status = *updates.Status
	}
	return nil
}

func (s *testStore) DeleteTaskRun(_ context.Context, _ string) error { return nil }

func (s *testStore) GetTaskRunByTaskAndAttempt(_ context.Context, _ string, _ int) (*store.TaskRun, error) {
	return nil, store.ErrNotFound
}

// PermissionRequest methods
func (s *testStore) CreatePermissionRequest(_ context.Context, perm *store.PermissionRequest) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.permissionRequests[perm.ID] = perm
	return nil
}

func (s *testStore) GetPermissionRequest(_ context.Context, id string) (*store.PermissionRequest, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	perm, ok := s.permissionRequests[id]
	if !ok {
		return nil, store.ErrNotFound
	}
	return perm, nil
}

func (s *testStore) ListPermissionRequests(_ context.Context, opts store.ListPermissionRequestsOptions) (*store.ListResult[store.PermissionRequest], error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	items := make([]*store.PermissionRequest, 0, len(s.permissionRequests))
	for _, perm := range s.permissionRequests {
		// Filter by session ID if specified
		if opts.SessionID != nil && perm.SessionID != *opts.SessionID {
			continue
		}
		// Filter by status if specified
		if len(opts.Status) > 0 {
			found := false
			for _, status := range opts.Status {
				if perm.Status == status {
					found = true
					break
				}
			}
			if !found {
				continue
			}
		}
		items = append(items, perm)
	}
	return &store.ListResult[store.PermissionRequest]{Items: items}, nil
}

func (s *testStore) UpdatePermissionRequest(_ context.Context, id string, updates store.PermissionRequestUpdates) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	perm, ok := s.permissionRequests[id]
	if !ok {
		return store.ErrNotFound
	}
	if updates.Status != nil {
		perm.Status = *updates.Status
	}
	if updates.RespondedBy != nil {
		perm.RespondedBy = updates.RespondedBy
	}
	if updates.ResponseReason != nil {
		perm.ResponseReason = updates.ResponseReason
	}
	if updates.RespondedAt != nil {
		perm.RespondedAt = updates.RespondedAt
	}
	return nil
}

// Runner methods (stub)
func (s *testStore) CreateRunner(_ context.Context, _ *store.Runner) error { return nil }
func (s *testStore) GetRunner(_ context.Context, _ string) (*store.Runner, error) {
	return nil, store.ErrNotFound
}
func (s *testStore) GetRunnerByName(_ context.Context, _ string) (*store.Runner, error) {
	return nil, store.ErrNotFound
}
func (s *testStore) ListRunners(_ context.Context, _ store.ListRunnersOptions) (*store.ListResult[store.Runner], error) {
	return &store.ListResult[store.Runner]{}, nil
}
func (s *testStore) UpdateRunner(_ context.Context, _ string, _ store.RunnerUpdates) error {
	return nil
}
func (s *testStore) DeleteRunner(_ context.Context, _ string) error { return nil }

// Other stub methods to satisfy store.Store interface
func (s *testStore) CreateAPIKey(_ context.Context, _ *store.APIKey) error { return nil }
func (s *testStore) GetAPIKey(_ context.Context, _ string) (*store.APIKey, error) {
	return nil, store.ErrNotFound
}
func (s *testStore) GetAPIKeyByHash(_ context.Context, _ string) (*store.APIKey, error) {
	return nil, store.ErrNotFound
}
func (s *testStore) ListAPIKeys(_ context.Context, _ store.ListAPIKeysOptions) (*store.ListResult[store.APIKey], error) {
	return &store.ListResult[store.APIKey]{}, nil
}
func (s *testStore) UpdateAPIKey(_ context.Context, _ string, _ store.APIKeyUpdates) error {
	return nil
}
func (s *testStore) DeleteAPIKey(_ context.Context, _ string) error { return nil }

func (s *testStore) CreateRunnerToken(_ context.Context, _ *store.RunnerToken) error { return nil }
func (s *testStore) GetRunnerToken(_ context.Context, _ string) (*store.RunnerToken, error) {
	return nil, store.ErrNotFound
}
func (s *testStore) GetRunnerTokenByHash(_ context.Context, _ string) (*store.RunnerToken, error) {
	return nil, store.ErrNotFound
}
func (s *testStore) ListRunnerTokens(_ context.Context, _ store.ListRunnerTokensOptions) (*store.ListResult[store.RunnerToken], error) {
	return &store.ListResult[store.RunnerToken]{}, nil
}
func (s *testStore) UpdateRunnerToken(_ context.Context, _ string, _ store.RunnerTokenUpdates) error {
	return nil
}
func (s *testStore) DeleteRunnerToken(_ context.Context, _ string) error { return nil }

func (s *testStore) CreateScheduledTask(_ context.Context, _ *store.ScheduledTask) error { return nil }
func (s *testStore) GetScheduledTask(_ context.Context, _ string) (*store.ScheduledTask, error) {
	return nil, store.ErrNotFound
}
func (s *testStore) ListScheduledTasks(_ context.Context, _ store.ListScheduledTasksOptions) (*store.ListResult[store.ScheduledTask], error) {
	return &store.ListResult[store.ScheduledTask]{}, nil
}
func (s *testStore) UpdateScheduledTask(_ context.Context, _ string, _ store.ScheduledTaskUpdates) error {
	return nil
}
func (s *testStore) DeleteScheduledTask(_ context.Context, _ string) error { return nil }

func (s *testStore) CreateAgentConfig(_ context.Context, _ *store.AgentConfig) error { return nil }
func (s *testStore) GetAgentConfig(_ context.Context, _ string) (*store.AgentConfig, error) {
	return nil, store.ErrNotFound
}
func (s *testStore) GetAgentConfigByName(_ context.Context, _ string) (*store.AgentConfig, error) {
	return nil, store.ErrNotFound
}
func (s *testStore) GetDefaultAgentConfig(_ context.Context, _ string) (*store.AgentConfig, error) {
	return nil, store.ErrNotFound
}
func (s *testStore) ListAgentConfigs(_ context.Context, _ store.ListAgentConfigsOptions) (*store.ListResult[store.AgentConfig], error) {
	return &store.ListResult[store.AgentConfig]{}, nil
}
func (s *testStore) UpdateAgentConfig(_ context.Context, _ string, _ store.AgentConfigUpdates) error {
	return nil
}
func (s *testStore) DeleteAgentConfig(_ context.Context, _ string) error { return nil }

func (s *testStore) CreateProviderConfig(_ context.Context, _ *store.ProviderConfig) error {
	return nil
}
func (s *testStore) GetProviderConfig(_ context.Context, _ string) (*store.ProviderConfig, error) {
	return nil, store.ErrNotFound
}
func (s *testStore) GetProviderConfigByName(_ context.Context, _ string) (*store.ProviderConfig, error) {
	return nil, store.ErrNotFound
}
func (s *testStore) GetDefaultProviderConfig(_ context.Context, _ string) (*store.ProviderConfig, error) {
	return nil, store.ErrNotFound
}
func (s *testStore) ListProviderConfigs(_ context.Context, _ store.ListProviderConfigsOptions) (*store.ListResult[store.ProviderConfig], error) {
	return &store.ListResult[store.ProviderConfig]{}, nil
}
func (s *testStore) UpdateProviderConfig(_ context.Context, _ string, _ store.ProviderConfigUpdates) error {
	return nil
}
func (s *testStore) DeleteProviderConfig(_ context.Context, _ string) error { return nil }

func (s *testStore) CreateProfile(_ context.Context, _ *store.Profile) error { return nil }
func (s *testStore) GetProfile(_ context.Context, _ string) (*store.Profile, error) {
	return nil, store.ErrNotFound
}
func (s *testStore) GetProfileByName(_ context.Context, _ string) (*store.Profile, error) {
	return nil, store.ErrNotFound
}
func (s *testStore) ListProfiles(_ context.Context, _ store.ListProfilesOptions) (*store.ListResult[store.Profile], error) {
	return &store.ListResult[store.Profile]{}, nil
}
func (s *testStore) UpdateProfile(_ context.Context, _ string, _ store.ProfileUpdates) error {
	return nil
}
func (s *testStore) DeleteProfile(_ context.Context, _ string) error { return nil }

func (s *testStore) CreateSnapshot(_ context.Context, _ *store.Snapshot) error { return nil }
func (s *testStore) GetSnapshot(_ context.Context, _ string) (*store.Snapshot, error) {
	return nil, store.ErrNotFound
}
func (s *testStore) GetSnapshotByRunnerAndName(_ context.Context, _, _ string) (*store.Snapshot, error) {
	return nil, store.ErrNotFound
}
func (s *testStore) ListSnapshots(_ context.Context, _ store.ListSnapshotsOptions) (*store.ListResult[store.Snapshot], error) {
	return &store.ListResult[store.Snapshot]{}, nil
}
func (s *testStore) UpdateSnapshot(_ context.Context, _ string, _ store.SnapshotUpdates) error {
	return nil
}
func (s *testStore) DeleteSnapshot(_ context.Context, _ string) error { return nil }

func (s *testStore) CreateTunnel(_ context.Context, _ *store.Tunnel) error { return nil }
func (s *testStore) GetTunnel(_ context.Context, _ string) (*store.Tunnel, error) {
	return nil, store.ErrNotFound
}
func (s *testStore) GetTunnelByTokenHash(_ context.Context, _ string) (*store.Tunnel, error) {
	return nil, store.ErrNotFound
}
func (s *testStore) ListTunnels(_ context.Context, _ store.ListTunnelsOptions) (*store.ListResult[store.Tunnel], error) {
	return &store.ListResult[store.Tunnel]{}, nil
}
func (s *testStore) UpdateTunnel(_ context.Context, _ string, _ store.TunnelUpdates) error {
	return nil
}
func (s *testStore) DeleteTunnel(_ context.Context, _ string) error { return nil }

func (s *testStore) CreateActionLog(_ context.Context, _ *store.ActionLog) error { return nil }
func (s *testStore) GetActionLog(_ context.Context, _ string) (*store.ActionLog, error) {
	return nil, store.ErrNotFound
}
func (s *testStore) ListActionLogs(_ context.Context, _ store.ListActionLogsOptions) (*store.ListResult[store.ActionLog], error) {
	return &store.ListResult[store.ActionLog]{}, nil
}

func (s *testStore) CreateLog(_ context.Context, log *store.Log) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.logs = append(s.logs, log)
	return nil
}

func (s *testStore) CreateLogs(_ context.Context, logs []*store.Log) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.logs = append(s.logs, logs...)
	return nil
}

func (s *testStore) ListLogs(_ context.Context, _ store.ListLogsOptions) (*store.ListResult[store.Log], error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return &store.ListResult[store.Log]{Items: s.logs}, nil
}

func (s *testStore) CreateLogArchive(_ context.Context, _ *store.LogArchive) error { return nil }
func (s *testStore) GetLogArchive(_ context.Context, _ string) (*store.LogArchive, error) {
	return nil, store.ErrNotFound
}
func (s *testStore) GetLogArchiveBySession(_ context.Context, _ string) (*store.LogArchive, error) {
	return nil, store.ErrNotFound
}
func (s *testStore) ListLogArchives(_ context.Context, _ store.ListLogArchivesOptions) (*store.ListResult[store.LogArchive], error) {
	return &store.ListResult[store.LogArchive]{}, nil
}
func (s *testStore) UpdateLogArchive(_ context.Context, _ string, _ store.LogArchiveUpdates) error {
	return nil
}

func (s *testStore) CreateDataKey(_ context.Context, _ *store.DataKey) error { return nil }
func (s *testStore) GetDataKey(_ context.Context, _ string) (*store.DataKey, error) {
	return nil, store.ErrNotFound
}
func (s *testStore) GetDataKeyByResource(_ context.Context, _, _ string) (*store.DataKey, error) {
	return nil, store.ErrNotFound
}
func (s *testStore) UpdateDataKey(_ context.Context, _ string, _ store.DataKeyUpdates) error {
	return nil
}

func (s *testStore) CreateChunk(_ context.Context, _ *store.Chunk) error { return nil }
func (s *testStore) GetChunk(_ context.Context, _, _ string) (*store.Chunk, error) {
	return nil, store.ErrNotFound
}
func (s *testStore) UpdateChunk(_ context.Context, _, _ string, _ store.ChunkUpdates) error {
	return nil
}
func (s *testStore) IncrementChunkRefCount(_ context.Context, _, _ string) error { return nil }
func (s *testStore) DecrementChunkRefCount(_ context.Context, _, _ string) error { return nil }
func (s *testStore) DeleteChunk(_ context.Context, _, _ string) error            { return nil }
func (s *testStore) ListUnreferencedChunks(_ context.Context, _ string, _ int) ([]*store.Chunk, error) {
	return nil, nil
}
func (s *testStore) ListSoftDeletedChunks(_ context.Context, _ string, _ time.Time, _ int) ([]*store.Chunk, error) {
	return nil, nil
}
func (s *testStore) MarkChunkDeleted(_ context.Context, _, _ string) error  { return nil }
func (s *testStore) ClearChunkDeleted(_ context.Context, _, _ string) error { return nil }

func (s *testStore) CreateManifest(_ context.Context, _ *store.Manifest) error { return nil }
func (s *testStore) GetManifest(_ context.Context, _ string) (*store.Manifest, error) {
	return nil, store.ErrNotFound
}
func (s *testStore) GetLatestManifest(_ context.Context, _ string) (*store.Manifest, error) {
	return nil, store.ErrNotFound
}
func (s *testStore) DeleteManifest(_ context.Context, _ string) error { return nil }

// BeginTx starts a mock transaction (not implemented for simple tests).
func (s *testStore) BeginTx(_ context.Context) (store.Tx, error) {
	return nil, nil
}

// Ping implements store.Store.
func (s *testStore) Ping(_ context.Context) error {
	return nil
}

// Close implements store.Store.
func (s *testStore) Close() error {
	return nil
}

// DeleteDataKey implements store.Store.
func (s *testStore) DeleteDataKey(_ context.Context, _ string) error {
	return nil
}

// Stream methods (stub)
func (s *testStore) CreateStream(_ context.Context, _ *store.Stream) error { return nil }
func (s *testStore) GetStream(_ context.Context, _ string) (*store.Stream, error) {
	return nil, store.ErrNotFound
}
func (s *testStore) GetStreamBySessionAndType(_ context.Context, _, _ string, _ bool) (*store.Stream, error) {
	return nil, store.ErrNotFound
}
func (s *testStore) ListStreams(_ context.Context, _ store.ListStreamsOptions) (*store.ListResult[store.Stream], error) {
	return &store.ListResult[store.Stream]{}, nil
}
func (s *testStore) UpdateStream(_ context.Context, _ string, _ store.StreamUpdates) error {
	return nil
}
func (s *testStore) DeleteStream(_ context.Context, _ string) error { return nil }
func (s *testStore) CleanupExpiredStreams(_ context.Context) (int, error) {
	return 0, nil
}
