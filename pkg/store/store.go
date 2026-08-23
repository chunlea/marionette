package store

import (
	"context"
	"time"
)

// Store and Tx are large enough that hand-written fakes rot: every method added
// here used to mean editing eight of them. Regenerate with `make generate`.
//
// The behavioural in-memory fakes in pkg/store/mock are deliberately NOT
// generated — they implement real behaviour, which a call recorder cannot.
//
//go:generate go tool mockgen -source=store.go -destination=storemock/storemock.go -package=storemock

// Store defines the persistence interface for Marionette.
// All methods accept context.Context for cancellation and timeout support.
type Store interface {
	// Runners
	CreateRunner(ctx context.Context, runner *Runner) error
	GetRunner(ctx context.Context, id string) (*Runner, error)
	GetRunnerByName(ctx context.Context, name string) (*Runner, error)
	ListRunners(ctx context.Context, opts ListRunnersOptions) (*ListResult[Runner], error)
	UpdateRunner(ctx context.Context, id string, updates RunnerUpdates) error
	DeleteRunner(ctx context.Context, id string) error
	// ClaimRunner takes an exclusive, leased claim on a runner for a session.
	// It reports whether the claim was taken; false means another session
	// holds a live claim and the caller must pick a different runner.
	//
	// This is the cross-process arbiter for runner allocation: selecting a
	// runner and recording the choice are two statements, and without a claim
	// two servers both pass the check and take the same runner.
	ClaimRunner(ctx context.Context, runnerID, sessionID string, lease time.Duration) (bool, error)
	// ReleaseRunnerClaim drops a claim held by sessionID. Releasing a claim
	// held by somebody else, or one that has already expired and been taken
	// over, is a no-op rather than an error.
	ReleaseRunnerClaim(ctx context.Context, runnerID, sessionID string) error

	// Workspaces
	CreateWorkspace(ctx context.Context, workspace *Workspace) error
	GetWorkspace(ctx context.Context, id string) (*Workspace, error)
	ListWorkspaces(ctx context.Context, opts ListWorkspacesOptions) (*ListResult[Workspace], error)
	UpdateWorkspace(ctx context.Context, id string, updates WorkspaceUpdates) error
	DeleteWorkspace(ctx context.Context, id string) error

	// Sessions
	CreateSession(ctx context.Context, session *Session) error
	GetSession(ctx context.Context, id string) (*Session, error)
	ListSessions(ctx context.Context, opts ListSessionsOptions) (*ListResult[Session], error)
	UpdateSession(ctx context.Context, id string, updates SessionUpdates) error
	DeleteSession(ctx context.Context, id string) error
	GetDueScheduledSessions(ctx context.Context, now time.Time, limit int) ([]*Session, error)

	// Tasks
	CreateTask(ctx context.Context, task *Task) error
	GetTask(ctx context.Context, id string) (*Task, error)
	ListTasks(ctx context.Context, opts ListTasksOptions) (*ListResult[Task], error)
	UpdateTask(ctx context.Context, id string, updates TaskUpdates) error
	DeleteTask(ctx context.Context, id string) error

	// TaskRuns
	CreateTaskRun(ctx context.Context, run *TaskRun) error
	GetTaskRun(ctx context.Context, id string) (*TaskRun, error)
	GetTaskRunByTaskAndAttempt(ctx context.Context, taskID string, attempt int) (*TaskRun, error)
	ListTaskRuns(ctx context.Context, opts ListTaskRunsOptions) (*ListResult[TaskRun], error)
	UpdateTaskRun(ctx context.Context, id string, updates TaskRunUpdates) error

	// ScheduledTasks
	CreateScheduledTask(ctx context.Context, task *ScheduledTask) error
	GetScheduledTask(ctx context.Context, id string) (*ScheduledTask, error)
	ListScheduledTasks(ctx context.Context, opts ListScheduledTasksOptions) (*ListResult[ScheduledTask], error)
	UpdateScheduledTask(ctx context.Context, id string, updates ScheduledTaskUpdates) error
	DeleteScheduledTask(ctx context.Context, id string) error
	GetDueScheduledTasks(ctx context.Context, now time.Time, limit int) ([]*ScheduledTask, error)

	// PermissionRequests
	CreatePermissionRequest(ctx context.Context, req *PermissionRequest) error
	GetPermissionRequest(ctx context.Context, id string) (*PermissionRequest, error)
	ListPermissionRequests(ctx context.Context, opts ListPermissionRequestsOptions) (*ListResult[PermissionRequest], error)
	UpdatePermissionRequest(ctx context.Context, id string, updates PermissionRequestUpdates) error

	// APIKeys
	CreateAPIKey(ctx context.Context, key *APIKey) error
	GetAPIKey(ctx context.Context, id string) (*APIKey, error)
	GetAPIKeyByHash(ctx context.Context, hash string) (*APIKey, error)
	ListAPIKeys(ctx context.Context, opts ListAPIKeysOptions) (*ListResult[APIKey], error)
	UpdateAPIKey(ctx context.Context, id string, updates APIKeyUpdates) error
	DeleteAPIKey(ctx context.Context, id string) error

	// RunnerTokens
	CreateRunnerToken(ctx context.Context, token *RunnerToken) error
	GetRunnerToken(ctx context.Context, id string) (*RunnerToken, error)
	GetRunnerTokenByHash(ctx context.Context, hash string) (*RunnerToken, error)
	ListRunnerTokens(ctx context.Context, opts ListRunnerTokensOptions) (*ListResult[RunnerToken], error)
	UpdateRunnerToken(ctx context.Context, id string, updates RunnerTokenUpdates) error
	DeleteRunnerToken(ctx context.Context, id string) error

	// AgentConfigs
	CreateAgentConfig(ctx context.Context, config *AgentConfig) error
	GetAgentConfig(ctx context.Context, id string) (*AgentConfig, error)
	GetAgentConfigByName(ctx context.Context, name string) (*AgentConfig, error)
	GetDefaultAgentConfig(ctx context.Context, agent string) (*AgentConfig, error)
	ListAgentConfigs(ctx context.Context, opts ListAgentConfigsOptions) (*ListResult[AgentConfig], error)
	UpdateAgentConfig(ctx context.Context, id string, updates AgentConfigUpdates) error
	DeleteAgentConfig(ctx context.Context, id string) error

	// ProviderConfigs
	CreateProviderConfig(ctx context.Context, config *ProviderConfig) error
	GetProviderConfig(ctx context.Context, id string) (*ProviderConfig, error)
	GetProviderConfigByName(ctx context.Context, name string) (*ProviderConfig, error)
	GetDefaultProviderConfig(ctx context.Context, provider string) (*ProviderConfig, error)
	ListProviderConfigs(ctx context.Context, opts ListProviderConfigsOptions) (*ListResult[ProviderConfig], error)
	UpdateProviderConfig(ctx context.Context, id string, updates ProviderConfigUpdates) error
	DeleteProviderConfig(ctx context.Context, id string) error

	// Profiles
	CreateProfile(ctx context.Context, profile *Profile) error
	GetProfile(ctx context.Context, id string) (*Profile, error)
	GetProfileByName(ctx context.Context, name string) (*Profile, error)
	ListProfiles(ctx context.Context, opts ListProfilesOptions) (*ListResult[Profile], error)
	UpdateProfile(ctx context.Context, id string, updates ProfileUpdates) error
	DeleteProfile(ctx context.Context, id string) error

	// Snapshots
	CreateSnapshot(ctx context.Context, snapshot *Snapshot) error
	GetSnapshot(ctx context.Context, id string) (*Snapshot, error)
	GetSnapshotByRunnerAndName(ctx context.Context, runnerID, name string) (*Snapshot, error)
	ListSnapshots(ctx context.Context, opts ListSnapshotsOptions) (*ListResult[Snapshot], error)
	UpdateSnapshot(ctx context.Context, id string, updates SnapshotUpdates) error
	DeleteSnapshot(ctx context.Context, id string) error

	// Tunnels
	CreateTunnel(ctx context.Context, tunnel *Tunnel) error
	GetTunnel(ctx context.Context, id string) (*Tunnel, error)
	GetTunnelByTokenHash(ctx context.Context, hash string) (*Tunnel, error)
	ListTunnels(ctx context.Context, opts ListTunnelsOptions) (*ListResult[Tunnel], error)
	UpdateTunnel(ctx context.Context, id string, updates TunnelUpdates) error
	DeleteTunnel(ctx context.Context, id string) error

	// ActionLogs (audit logs - no update/delete for immutability)
	CreateActionLog(ctx context.Context, log *ActionLog) error
	GetActionLog(ctx context.Context, id string) (*ActionLog, error)
	ListActionLogs(ctx context.Context, opts ListActionLogsOptions) (*ListResult[ActionLog], error)

	// Logs
	CreateLog(ctx context.Context, log *Log) error
	CreateLogs(ctx context.Context, logs []*Log) error
	ListLogs(ctx context.Context, opts ListLogsOptions) (*ListResult[Log], error)

	// LogArchives
	CreateLogArchive(ctx context.Context, archive *LogArchive) error
	GetLogArchive(ctx context.Context, id string) (*LogArchive, error)
	GetLogArchiveBySession(ctx context.Context, sessionID string) (*LogArchive, error)
	ListLogArchives(ctx context.Context, opts ListLogArchivesOptions) (*ListResult[LogArchive], error)
	UpdateLogArchive(ctx context.Context, id string, updates LogArchiveUpdates) error

	// DataKeys (encryption)
	CreateDataKey(ctx context.Context, key *DataKey) error
	GetDataKey(ctx context.Context, id string) (*DataKey, error)
	GetDataKeyByResource(ctx context.Context, resourceType, resourceID string) (*DataKey, error)
	UpdateDataKey(ctx context.Context, id string, updates DataKeyUpdates) error
	DeleteDataKey(ctx context.Context, id string) error

	// Chunks (CAS storage)
	CreateChunk(ctx context.Context, chunk *Chunk) error
	GetChunk(ctx context.Context, tenantID, hash string) (*Chunk, error)
	UpdateChunk(ctx context.Context, tenantID, hash string, updates ChunkUpdates) error
	DeleteChunk(ctx context.Context, tenantID, hash string) error
	IncrementChunkRefCount(ctx context.Context, tenantID, hash string) error
	DecrementChunkRefCount(ctx context.Context, tenantID, hash string) error
	ListUnreferencedChunks(ctx context.Context, tenantID string, limit int) ([]*Chunk, error)
	ListSoftDeletedChunks(ctx context.Context, tenantID string, olderThan time.Time, limit int) ([]*Chunk, error)
	MarkChunkDeleted(ctx context.Context, tenantID, hash string) error
	ClearChunkDeleted(ctx context.Context, tenantID, hash string) error

	// Manifests
	CreateManifest(ctx context.Context, manifest *Manifest) error
	GetManifest(ctx context.Context, id string) (*Manifest, error)
	GetLatestManifest(ctx context.Context, workspaceID string) (*Manifest, error)
	DeleteManifest(ctx context.Context, id string) error

	// Streams
	CreateStream(ctx context.Context, stream *Stream) error
	GetStream(ctx context.Context, id string) (*Stream, error)
	GetStreamBySessionAndType(ctx context.Context, sessionID, streamType string, activeOnly bool) (*Stream, error)
	ListStreams(ctx context.Context, opts ListStreamsOptions) (*ListResult[Stream], error)
	UpdateStream(ctx context.Context, id string, updates StreamUpdates) error
	DeleteStream(ctx context.Context, id string) error
	CleanupExpiredStreams(ctx context.Context) (int, error)

	// Webhooks
	CreateWebhook(ctx context.Context, webhook *Webhook) error
	GetWebhook(ctx context.Context, id string) (*Webhook, error)
	GetWebhookByName(ctx context.Context, name string, tenantID *string) (*Webhook, error)
	ListWebhooks(ctx context.Context, opts ListWebhooksOptions) (*ListResult[Webhook], error)
	UpdateWebhook(ctx context.Context, id string, updates WebhookUpdates) error
	DeleteWebhook(ctx context.Context, id string) error
	GetActiveWebhooksForEvent(ctx context.Context, eventType string, tenantID *string) ([]*Webhook, error)

	// WebhookEvents
	CreateWebhookEvent(ctx context.Context, event *WebhookEvent) error
	GetWebhookEvent(ctx context.Context, id string) (*WebhookEvent, error)
	ListWebhookEvents(ctx context.Context, opts ListWebhookEventsOptions) (*ListResult[WebhookEvent], error)
	UpdateWebhookEvent(ctx context.Context, id string, updates WebhookEventUpdates) error
	GetPendingWebhookEvents(ctx context.Context, limit int) ([]*WebhookEvent, error)
	CancelWebhookEventsByWebhook(ctx context.Context, webhookID string) error

	// Transactions
	BeginTx(ctx context.Context) (Tx, error)

	// Health check
	Ping(ctx context.Context) error

	// Cleanup
	Close() error
}

// Tx represents a database transaction with the same operations as Store.
// Call Commit() to commit or Rollback() to abort.
type Tx interface {
	// Runners
	CreateRunner(ctx context.Context, runner *Runner) error
	GetRunner(ctx context.Context, id string) (*Runner, error)
	GetRunnerByName(ctx context.Context, name string) (*Runner, error)
	ListRunners(ctx context.Context, opts ListRunnersOptions) (*ListResult[Runner], error)
	UpdateRunner(ctx context.Context, id string, updates RunnerUpdates) error
	DeleteRunner(ctx context.Context, id string) error
	// ClaimRunner takes an exclusive, leased claim on a runner for a session.
	// It reports whether the claim was taken; false means another session
	// holds a live claim and the caller must pick a different runner.
	//
	// This is the cross-process arbiter for runner allocation: selecting a
	// runner and recording the choice are two statements, and without a claim
	// two servers both pass the check and take the same runner.
	ClaimRunner(ctx context.Context, runnerID, sessionID string, lease time.Duration) (bool, error)
	// ReleaseRunnerClaim drops a claim held by sessionID. Releasing a claim
	// held by somebody else, or one that has already expired and been taken
	// over, is a no-op rather than an error.
	ReleaseRunnerClaim(ctx context.Context, runnerID, sessionID string) error

	// Workspaces
	CreateWorkspace(ctx context.Context, workspace *Workspace) error
	GetWorkspace(ctx context.Context, id string) (*Workspace, error)
	ListWorkspaces(ctx context.Context, opts ListWorkspacesOptions) (*ListResult[Workspace], error)
	UpdateWorkspace(ctx context.Context, id string, updates WorkspaceUpdates) error
	DeleteWorkspace(ctx context.Context, id string) error

	// Sessions
	CreateSession(ctx context.Context, session *Session) error
	GetSession(ctx context.Context, id string) (*Session, error)
	ListSessions(ctx context.Context, opts ListSessionsOptions) (*ListResult[Session], error)
	UpdateSession(ctx context.Context, id string, updates SessionUpdates) error
	DeleteSession(ctx context.Context, id string) error
	GetDueScheduledSessions(ctx context.Context, now time.Time, limit int) ([]*Session, error)

	// Tasks
	CreateTask(ctx context.Context, task *Task) error
	GetTask(ctx context.Context, id string) (*Task, error)
	ListTasks(ctx context.Context, opts ListTasksOptions) (*ListResult[Task], error)
	UpdateTask(ctx context.Context, id string, updates TaskUpdates) error
	DeleteTask(ctx context.Context, id string) error

	// TaskRuns
	CreateTaskRun(ctx context.Context, run *TaskRun) error
	GetTaskRun(ctx context.Context, id string) (*TaskRun, error)
	GetTaskRunByTaskAndAttempt(ctx context.Context, taskID string, attempt int) (*TaskRun, error)
	ListTaskRuns(ctx context.Context, opts ListTaskRunsOptions) (*ListResult[TaskRun], error)
	UpdateTaskRun(ctx context.Context, id string, updates TaskRunUpdates) error

	// ScheduledTasks
	CreateScheduledTask(ctx context.Context, task *ScheduledTask) error
	GetScheduledTask(ctx context.Context, id string) (*ScheduledTask, error)
	ListScheduledTasks(ctx context.Context, opts ListScheduledTasksOptions) (*ListResult[ScheduledTask], error)
	UpdateScheduledTask(ctx context.Context, id string, updates ScheduledTaskUpdates) error
	DeleteScheduledTask(ctx context.Context, id string) error
	GetDueScheduledTasks(ctx context.Context, now time.Time, limit int) ([]*ScheduledTask, error)

	// PermissionRequests
	CreatePermissionRequest(ctx context.Context, req *PermissionRequest) error
	GetPermissionRequest(ctx context.Context, id string) (*PermissionRequest, error)
	ListPermissionRequests(ctx context.Context, opts ListPermissionRequestsOptions) (*ListResult[PermissionRequest], error)
	UpdatePermissionRequest(ctx context.Context, id string, updates PermissionRequestUpdates) error

	// APIKeys
	CreateAPIKey(ctx context.Context, key *APIKey) error
	GetAPIKey(ctx context.Context, id string) (*APIKey, error)
	GetAPIKeyByHash(ctx context.Context, hash string) (*APIKey, error)
	ListAPIKeys(ctx context.Context, opts ListAPIKeysOptions) (*ListResult[APIKey], error)
	UpdateAPIKey(ctx context.Context, id string, updates APIKeyUpdates) error
	DeleteAPIKey(ctx context.Context, id string) error

	// RunnerTokens
	CreateRunnerToken(ctx context.Context, token *RunnerToken) error
	GetRunnerToken(ctx context.Context, id string) (*RunnerToken, error)
	GetRunnerTokenByHash(ctx context.Context, hash string) (*RunnerToken, error)
	ListRunnerTokens(ctx context.Context, opts ListRunnerTokensOptions) (*ListResult[RunnerToken], error)
	UpdateRunnerToken(ctx context.Context, id string, updates RunnerTokenUpdates) error
	DeleteRunnerToken(ctx context.Context, id string) error

	// AgentConfigs
	CreateAgentConfig(ctx context.Context, config *AgentConfig) error
	GetAgentConfig(ctx context.Context, id string) (*AgentConfig, error)
	GetAgentConfigByName(ctx context.Context, name string) (*AgentConfig, error)
	GetDefaultAgentConfig(ctx context.Context, agent string) (*AgentConfig, error)
	ListAgentConfigs(ctx context.Context, opts ListAgentConfigsOptions) (*ListResult[AgentConfig], error)
	UpdateAgentConfig(ctx context.Context, id string, updates AgentConfigUpdates) error
	DeleteAgentConfig(ctx context.Context, id string) error

	// ProviderConfigs
	CreateProviderConfig(ctx context.Context, config *ProviderConfig) error
	GetProviderConfig(ctx context.Context, id string) (*ProviderConfig, error)
	GetProviderConfigByName(ctx context.Context, name string) (*ProviderConfig, error)
	GetDefaultProviderConfig(ctx context.Context, provider string) (*ProviderConfig, error)
	ListProviderConfigs(ctx context.Context, opts ListProviderConfigsOptions) (*ListResult[ProviderConfig], error)
	UpdateProviderConfig(ctx context.Context, id string, updates ProviderConfigUpdates) error
	DeleteProviderConfig(ctx context.Context, id string) error

	// Profiles
	CreateProfile(ctx context.Context, profile *Profile) error
	GetProfile(ctx context.Context, id string) (*Profile, error)
	GetProfileByName(ctx context.Context, name string) (*Profile, error)
	ListProfiles(ctx context.Context, opts ListProfilesOptions) (*ListResult[Profile], error)
	UpdateProfile(ctx context.Context, id string, updates ProfileUpdates) error
	DeleteProfile(ctx context.Context, id string) error

	// Snapshots
	CreateSnapshot(ctx context.Context, snapshot *Snapshot) error
	GetSnapshot(ctx context.Context, id string) (*Snapshot, error)
	GetSnapshotByRunnerAndName(ctx context.Context, runnerID, name string) (*Snapshot, error)
	ListSnapshots(ctx context.Context, opts ListSnapshotsOptions) (*ListResult[Snapshot], error)
	UpdateSnapshot(ctx context.Context, id string, updates SnapshotUpdates) error
	DeleteSnapshot(ctx context.Context, id string) error

	// Tunnels
	CreateTunnel(ctx context.Context, tunnel *Tunnel) error
	GetTunnel(ctx context.Context, id string) (*Tunnel, error)
	GetTunnelByTokenHash(ctx context.Context, hash string) (*Tunnel, error)
	ListTunnels(ctx context.Context, opts ListTunnelsOptions) (*ListResult[Tunnel], error)
	UpdateTunnel(ctx context.Context, id string, updates TunnelUpdates) error
	DeleteTunnel(ctx context.Context, id string) error

	// ActionLogs
	CreateActionLog(ctx context.Context, log *ActionLog) error
	GetActionLog(ctx context.Context, id string) (*ActionLog, error)
	ListActionLogs(ctx context.Context, opts ListActionLogsOptions) (*ListResult[ActionLog], error)

	// Logs
	CreateLog(ctx context.Context, log *Log) error
	CreateLogs(ctx context.Context, logs []*Log) error
	ListLogs(ctx context.Context, opts ListLogsOptions) (*ListResult[Log], error)

	// LogArchives
	CreateLogArchive(ctx context.Context, archive *LogArchive) error
	GetLogArchive(ctx context.Context, id string) (*LogArchive, error)
	GetLogArchiveBySession(ctx context.Context, sessionID string) (*LogArchive, error)
	ListLogArchives(ctx context.Context, opts ListLogArchivesOptions) (*ListResult[LogArchive], error)
	UpdateLogArchive(ctx context.Context, id string, updates LogArchiveUpdates) error

	// DataKeys
	CreateDataKey(ctx context.Context, key *DataKey) error
	GetDataKey(ctx context.Context, id string) (*DataKey, error)
	GetDataKeyByResource(ctx context.Context, resourceType, resourceID string) (*DataKey, error)
	UpdateDataKey(ctx context.Context, id string, updates DataKeyUpdates) error
	DeleteDataKey(ctx context.Context, id string) error

	// Chunks
	CreateChunk(ctx context.Context, chunk *Chunk) error
	GetChunk(ctx context.Context, tenantID, hash string) (*Chunk, error)
	UpdateChunk(ctx context.Context, tenantID, hash string, updates ChunkUpdates) error
	DeleteChunk(ctx context.Context, tenantID, hash string) error
	IncrementChunkRefCount(ctx context.Context, tenantID, hash string) error
	DecrementChunkRefCount(ctx context.Context, tenantID, hash string) error
	ListUnreferencedChunks(ctx context.Context, tenantID string, limit int) ([]*Chunk, error)
	ListSoftDeletedChunks(ctx context.Context, tenantID string, olderThan time.Time, limit int) ([]*Chunk, error)
	MarkChunkDeleted(ctx context.Context, tenantID, hash string) error
	ClearChunkDeleted(ctx context.Context, tenantID, hash string) error

	// Manifests
	CreateManifest(ctx context.Context, manifest *Manifest) error
	GetManifest(ctx context.Context, id string) (*Manifest, error)
	GetLatestManifest(ctx context.Context, workspaceID string) (*Manifest, error)
	DeleteManifest(ctx context.Context, id string) error

	// Streams
	CreateStream(ctx context.Context, stream *Stream) error
	GetStream(ctx context.Context, id string) (*Stream, error)
	GetStreamBySessionAndType(ctx context.Context, sessionID, streamType string, activeOnly bool) (*Stream, error)
	ListStreams(ctx context.Context, opts ListStreamsOptions) (*ListResult[Stream], error)
	UpdateStream(ctx context.Context, id string, updates StreamUpdates) error
	DeleteStream(ctx context.Context, id string) error
	CleanupExpiredStreams(ctx context.Context) (int, error)

	// Webhooks
	CreateWebhook(ctx context.Context, webhook *Webhook) error
	GetWebhook(ctx context.Context, id string) (*Webhook, error)
	GetWebhookByName(ctx context.Context, name string, tenantID *string) (*Webhook, error)
	ListWebhooks(ctx context.Context, opts ListWebhooksOptions) (*ListResult[Webhook], error)
	UpdateWebhook(ctx context.Context, id string, updates WebhookUpdates) error
	DeleteWebhook(ctx context.Context, id string) error
	GetActiveWebhooksForEvent(ctx context.Context, eventType string, tenantID *string) ([]*Webhook, error)

	// WebhookEvents
	CreateWebhookEvent(ctx context.Context, event *WebhookEvent) error
	GetWebhookEvent(ctx context.Context, id string) (*WebhookEvent, error)
	ListWebhookEvents(ctx context.Context, opts ListWebhookEventsOptions) (*ListResult[WebhookEvent], error)
	UpdateWebhookEvent(ctx context.Context, id string, updates WebhookEventUpdates) error
	GetPendingWebhookEvents(ctx context.Context, limit int) ([]*WebhookEvent, error)
	CancelWebhookEventsByWebhook(ctx context.Context, webhookID string) error

	// Transaction control
	Commit(ctx context.Context) error
	Rollback(ctx context.Context) error
}
