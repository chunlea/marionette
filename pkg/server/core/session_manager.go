package core

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	pb "github.com/chunlea/marionette/gen/proto/v1"
	"github.com/chunlea/marionette/pkg/audit"
	"github.com/chunlea/marionette/pkg/id"
	"github.com/chunlea/marionette/pkg/provider"
	"github.com/chunlea/marionette/pkg/store"
	"go.uber.org/zap"
)

// Session status constants.
const (
	SessionStatusPending    = "pending"
	SessionStatusActive     = "active"
	SessionStatusSuspended  = "suspended"
	SessionStatusResuming   = "resuming"
	SessionStatusTerminated = "terminated"
)

// Network policy constants.
const (
	NetworkPolicyNone      = "none"
	NetworkPolicyAllowList = "allow_list"
	NetworkPolicyProxy     = "proxy"
	NetworkPolicyAirGapped = "air_gapped"
)

// Lifecycle mode constants.
const (
	LifecycleModeOnDemand  = "on_demand"
	LifecycleModeAlwaysOn  = "always_on"
	LifecycleModeScheduled = "scheduled"
)

// Session-related errors.
var (
	ErrSessionNotFound          = errors.New("session not found")
	ErrInvalidSessionTransition = errors.New("invalid session status transition")
	ErrSessionAlreadyTerminated = errors.New("session is already terminated")
	ErrSessionNotActive         = errors.New("session is not active")
	ErrSessionNotSuspended      = errors.New("session is not suspended")
	ErrSessionNoRunner          = errors.New("session has no runner attached")
	ErrSessionAlreadyHasRunner  = errors.New("session already has a runner attached")
	ErrWorkspaceRequired        = errors.New("workspace_id is required")
	ErrAgentRequired            = errors.New("agent is required")
	ErrRunnerNotIdle            = errors.New("runner is not idle")
	ErrScheduleCronRequired     = errors.New("schedule_cron is required for scheduled lifecycle mode")
)

// SessionManagerInterface defines the interface for session management.
// This is used for dependency injection in other components.
type SessionManagerInterface interface {
	Create(ctx context.Context, opts CreateSessionOptions) (*store.Session, error)
	Get(ctx context.Context, sessionID string) (*store.Session, error)
	List(ctx context.Context, opts ListSessionsOptions) (*store.ListResult[store.Session], error)
	Activate(ctx context.Context, sessionID, runnerID string) error
	Suspend(ctx context.Context, sessionID, strategy string) error
	Resume(ctx context.Context, sessionID string) error
	Terminate(ctx context.Context, sessionID string) error
	AttachRunner(ctx context.Context, sessionID, runnerID string) error
	DetachRunner(ctx context.Context, sessionID string) error
	UpdateContextSnapshot(ctx context.Context, sessionID string, snapshot *ContextSnapshot) error
}

// ProviderRegistryInterface defines the interface for provider operations needed by SessionManager.
type ProviderRegistryInterface interface {
	// GetDefault returns the default provider.
	GetDefault(ctx context.Context) (provider.Provider, error)
	// Get returns a provider by name.
	Get(ctx context.Context, name string) (provider.Provider, error)
}

// SessionManager handles session lifecycle and state transitions.
type SessionManager struct {
	store            store.Store
	connManager      ConnectionManagerInterface
	cmdSender        CommandSender
	workspaceManager WorkspaceManagerInterface
	auditLog         audit.Logger
	providerRegistry ProviderRegistryInterface
	taskManager      TaskManagerInterface
	logger           *zap.Logger
}

// SessionManagerConfig holds configuration for SessionManager.
type SessionManagerConfig struct {
	Store            store.Store
	ConnManager      ConnectionManagerInterface
	CmdSender        CommandSender
	WorkspaceManager WorkspaceManagerInterface
	AuditLog         audit.Logger
	ProviderRegistry ProviderRegistryInterface
	Logger           *zap.Logger
}

// NewSessionManager creates a new SessionManager.
func NewSessionManager(store store.Store, connManager ConnectionManagerInterface, cmdSender CommandSender, logger *zap.Logger) *SessionManager {
	return &SessionManager{
		store:       store,
		connManager: connManager,
		cmdSender:   cmdSender,
		logger:      logger,
	}
}

// NewSessionManagerWithConfig creates a new SessionManager with full configuration.
func NewSessionManagerWithConfig(cfg SessionManagerConfig) *SessionManager {
	return &SessionManager{
		store:            cfg.Store,
		connManager:      cfg.ConnManager,
		cmdSender:        cfg.CmdSender,
		workspaceManager: cfg.WorkspaceManager,
		auditLog:         cfg.AuditLog,
		providerRegistry: cfg.ProviderRegistry,
		logger:           cfg.Logger,
	}
}

// SetWorkspaceManager sets the workspace manager. This allows optional injection.
func (m *SessionManager) SetWorkspaceManager(wm WorkspaceManagerInterface) {
	m.workspaceManager = wm
}

// SetProviderRegistry sets the provider registry. This allows optional injection.
func (m *SessionManager) SetProviderRegistry(pr ProviderRegistryInterface) {
	m.providerRegistry = pr
}

// SetTaskManager sets the task manager. This allows optional injection.
func (m *SessionManager) SetTaskManager(tm TaskManagerInterface) {
	m.taskManager = tm
}

// CreateSessionOptions contains options for creating a new session.
type CreateSessionOptions struct {
	Name          *string           // Optional session name
	WorkspaceID   string            // Required
	Agent         string            // Required (e.g., "claude")
	IsBYOK        bool              // Whether using BYOK mode
	AgentConfigID *string           // Optional, for managed credentials
	LifecycleMode string            // on_demand, always_on, scheduled
	IdleTimeout   *int              // Seconds (for on_demand mode)
	NetworkPolicy string            // none, allow_list, proxy, air_gapped
	AllowedHosts  []string          // For allow_list network policy
	ScheduleCron  *string           // Required for scheduled mode
	ScheduleTZ    *string           // Timezone for scheduled mode
	TenantID      *string           // For multi-tenant deployments
	Labels        map[string]string // Optional metadata labels
	Annotations   map[string]string // Optional metadata annotations
}

// ListSessionsOptions wraps store.ListSessionsOptions for convenience.
type ListSessionsOptions = store.ListSessionsOptions

// Create creates a new session.
func (m *SessionManager) Create(ctx context.Context, opts CreateSessionOptions) (*store.Session, error) {
	// Validate required fields
	if opts.WorkspaceID == "" {
		return nil, ErrWorkspaceRequired
	}
	if opts.Agent == "" {
		return nil, ErrAgentRequired
	}

	// Validate workspace exists
	_, err := m.store.GetWorkspace(ctx, opts.WorkspaceID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, errors.New("workspace not found")
		}
		return nil, err
	}

	// Set defaults
	lifecycleMode := opts.LifecycleMode
	if lifecycleMode == "" {
		lifecycleMode = LifecycleModeOnDemand
	}

	networkPolicy := opts.NetworkPolicy
	if networkPolicy == "" {
		networkPolicy = NetworkPolicyAllowList
	}

	// Validate scheduled mode requires cron
	if lifecycleMode == LifecycleModeScheduled && (opts.ScheduleCron == nil || *opts.ScheduleCron == "") {
		return nil, ErrScheduleCronRequired
	}

	// Marshal labels and annotations
	labels, err := json.Marshal(opts.Labels)
	if err != nil {
		labels = []byte("{}")
	}
	annotations, err := json.Marshal(opts.Annotations)
	if err != nil {
		annotations = []byte("{}")
	}

	// Ensure AllowedHosts is never nil (database has NOT NULL constraint)
	allowedHosts := opts.AllowedHosts
	if allowedHosts == nil {
		allowedHosts = []string{}
	}

	// Create session
	session := &store.Session{
		ID:                 id.Session(),
		Name:               opts.Name,
		Status:             SessionStatusPending,
		WorkspaceID:        opts.WorkspaceID,
		Agent:              opts.Agent,
		IsBYOK:             opts.IsBYOK,
		AgentConfigID:      opts.AgentConfigID,
		NetworkPolicy:      networkPolicy,
		AllowedHosts:       allowedHosts,
		LifecycleMode:      lifecycleMode,
		IdleTimeoutSeconds: opts.IdleTimeout,
		ScheduleCron:       opts.ScheduleCron,
		ScheduleTimezone:   opts.ScheduleTZ,
		TenantID:           opts.TenantID,
		Labels:             labels,
		Annotations:        annotations,
		CreatedAt:          time.Now(),
		UpdatedAt:          time.Now(),
	}

	if err := m.store.CreateSession(ctx, session); err != nil {
		return nil, err
	}

	m.logger.Info("session created",
		zap.String("session_id", session.ID),
		zap.String("workspace_id", session.WorkspaceID),
		zap.String("agent", session.Agent),
		zap.String("lifecycle_mode", session.LifecycleMode),
	)

	// Log audit event
	if m.auditLog != nil {
		_ = audit.NewEvent(audit.ActionSessionCreated).
			WithSystemActor().
			WithResource(audit.ResourceTypeSession, session.ID).
			WithSession(session.ID).
			WithDetails(map[string]any{
				"workspace_id":   session.WorkspaceID,
				"agent":          session.Agent,
				"lifecycle_mode": session.LifecycleMode,
				"is_byok":        session.IsBYOK,
			}).
			WithSuccess(true).
			Log(ctx, m.auditLog)
	}

	return session, nil
}

// Get retrieves a session by ID.
func (m *SessionManager) Get(ctx context.Context, sessionID string) (*store.Session, error) {
	session, err := m.store.GetSession(ctx, sessionID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, ErrSessionNotFound
		}
		return nil, err
	}
	return session, nil
}

// List retrieves sessions matching the given options.
func (m *SessionManager) List(ctx context.Context, opts ListSessionsOptions) (*store.ListResult[store.Session], error) {
	return m.store.ListSessions(ctx, opts)
}

// Activate transitions a session from pending/resuming to active.
// This is called when a runner is assigned to the session.
func (m *SessionManager) Activate(ctx context.Context, sessionID, runnerID string) error {
	session, err := m.store.GetSession(ctx, sessionID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return ErrSessionNotFound
		}
		return err
	}

	// Validate transition
	if !isValidSessionTransition(session.Status, SessionStatusActive) {
		m.logger.Warn("invalid session transition",
			zap.String("session_id", sessionID),
			zap.String("from", session.Status),
			zap.String("to", SessionStatusActive),
		)
		return ErrInvalidSessionTransition
	}

	// Validate runner is available
	runner, err := m.store.GetRunner(ctx, runnerID)
	if err != nil {
		return err
	}

	// Skip idle check for resume scenarios where we're re-attaching to the previous runner.
	// This is necessary because the runner might still be "busy" processing the detach command
	// when we try to resume. The runner will become idle shortly after.
	isResumeToSameRunner := session.Status == SessionStatusResuming &&
		session.PreviousRunnerID != nil &&
		*session.PreviousRunnerID == runnerID

	if runner.Status != StatusIdle && !isResumeToSameRunner {
		return ErrRunnerNotIdle
	}

	// Detach any existing sessions from this runner
	// This ensures only one session is attached to a runner at a time
	if err := m.detachSessionsFromRunner(ctx, runnerID, sessionID); err != nil {
		m.logger.Error("failed to detach existing sessions from runner",
			zap.String("runner_id", runnerID),
			zap.Error(err),
		)
		// Continue with activation - don't fail because of cleanup issues
	}

	// Update session
	now := time.Now()
	updates := store.SessionUpdates{
		Status:         stringPtr(SessionStatusActive),
		RunnerID:       &runnerID,
		LastActivityAt: &now,
	}

	// If resuming, also set resumed_at
	if session.Status == SessionStatusResuming {
		updates.ResumedAt = &now
	}

	if err := m.store.UpdateSession(ctx, sessionID, updates); err != nil {
		return err
	}

	m.logger.Info("session activated",
		zap.String("session_id", sessionID),
		zap.String("runner_id", runnerID),
		zap.String("from_status", session.Status),
	)

	// Send AttachSession command to the runner
	if err := m.sendAttachSession(ctx, session, runnerID); err != nil {
		m.logger.Error("failed to send AttachSession command",
			zap.String("session_id", sessionID),
			zap.String("runner_id", runnerID),
			zap.Error(err),
		)
		// Don't fail the activation - the session is already active
		// The agent can request session info on next heartbeat
	}

	// If this was a resume operation, re-execute any running tasks
	// This ensures tasks continue after session suspend/resume
	m.logger.Info("checking if we need to re-execute tasks",
		zap.String("session_id", sessionID),
		zap.String("session_status", session.Status),
		zap.Bool("was_resuming", session.Status == SessionStatusResuming),
	)
	if session.Status == SessionStatusResuming {
		go m.reExecuteRunningTasks(ctx, sessionID, runnerID)
	}

	return nil
}

// reExecuteRunningTasks finds and re-executes any running tasks for a resumed session.
// This is called asynchronously after session activation to not block the activation.
func (m *SessionManager) reExecuteRunningTasks(ctx context.Context, sessionID, runnerID string) {
	m.logger.Info("reExecuteRunningTasks called",
		zap.String("session_id", sessionID),
		zap.String("runner_id", runnerID),
		zap.Bool("has_task_manager", m.taskManager != nil),
	)

	if m.taskManager == nil {
		m.logger.Debug("task manager not set, skipping task re-execution",
			zap.String("session_id", sessionID),
		)
		return
	}

	// Find running tasks for this session
	tasks, err := m.store.ListTasks(ctx, store.ListTasksOptions{
		SessionID: &sessionID,
		Status:    []string{TaskStatusRunning},
	})
	if err != nil {
		m.logger.Error("failed to list running tasks for session",
			zap.String("session_id", sessionID),
			zap.Error(err),
		)
		return
	}

	if len(tasks.Items) == 0 {
		m.logger.Debug("no running tasks to re-execute after resume",
			zap.String("session_id", sessionID),
		)
		return
	}

	m.logger.Info("re-executing running tasks after session resume",
		zap.String("session_id", sessionID),
		zap.Int("task_count", len(tasks.Items)),
	)

	// Re-execute each running task
	// Use a fresh context since the original request context may be canceled
	execCtx := context.Background()
	for _, task := range tasks.Items {
		m.logger.Info("re-executing task after resume",
			zap.String("session_id", sessionID),
			zap.String("task_id", task.ID),
		)

		// Use ReExecute to reuse the existing task_run instead of creating a new one
		if err := m.taskManager.ReExecute(execCtx, task.ID); err != nil {
			m.logger.Error("failed to re-execute task after resume",
				zap.String("session_id", sessionID),
				zap.String("task_id", task.ID),
				zap.Error(err),
			)
		}
	}
}

// SuspendOptions contains options for suspending a session.
type SuspendOptions struct {
	// Strategy specifies how to handle the suspend (pause, snapshot, etc.).
	Strategy string

	// ContextSnapshot is the context to save with the session.
	// If nil, a basic snapshot is created automatically.
	ContextSnapshot *ContextSnapshot

	// WorkspaceSynced indicates if workspace was synced to object storage.
	WorkspaceSynced bool

	// SnapshotID is the ID of any snapshot created during suspend.
	SnapshotID string
}

// Suspend transitions a session from active to suspended.
// The strategy parameter specifies how to handle the suspend (pause, snapshot, etc.).
func (m *SessionManager) Suspend(ctx context.Context, sessionID, strategy string) error {
	return m.SuspendWithOptions(ctx, sessionID, SuspendOptions{Strategy: strategy})
}

// SuspendWithOptions transitions a session from active to suspended with full options.
func (m *SessionManager) SuspendWithOptions(ctx context.Context, sessionID string, opts SuspendOptions) error {
	session, err := m.store.GetSession(ctx, sessionID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return ErrSessionNotFound
		}
		return err
	}

	// Validate transition
	if !isValidSessionTransition(session.Status, SessionStatusSuspended) {
		m.logger.Warn("invalid session transition for suspend",
			zap.String("session_id", sessionID),
			zap.String("from", session.Status),
			zap.String("to", SessionStatusSuspended),
		)
		return ErrInvalidSessionTransition
	}

	// Send DetachSession command to runner before updating database
	// This notifies the agent to clean up and save context
	if err := m.sendDetachSession(ctx, session, opts); err != nil {
		m.logger.Warn("failed to send DetachSession command, continuing with suspend",
			zap.String("session_id", sessionID),
			zap.Error(err),
		)
	}

	// Store previous runner ID before detaching
	previousRunnerID := session.RunnerID

	// Check if there are running tasks for this session.
	// If so, we should NOT preserve the context snapshot because:
	// 1. The task was killed mid-execution (waiting for tool result)
	// 2. Claude Code's conversation cannot be properly resumed from mid-tool state
	// 3. Trying to --resume with the old conversation_id will hang
	// The task will be re-executed from scratch with the original prompt after resume.
	hasRunningTask := false
	tasks, err := m.store.ListTasks(ctx, store.ListTasksOptions{
		SessionID: &sessionID,
		Status:    []string{TaskStatusRunning},
	})
	if err != nil {
		m.logger.Warn("failed to check for running tasks during suspend",
			zap.String("session_id", sessionID),
			zap.Error(err),
		)
	} else if len(tasks.Items) > 0 {
		hasRunningTask = true
		m.logger.Info("clearing context snapshot due to running task during suspend",
			zap.String("session_id", sessionID),
			zap.Int("running_task_count", len(tasks.Items)),
		)
	}

	// Prepare context snapshot
	var snapshotJSON json.RawMessage
	if hasRunningTask {
		// Don't save context snapshot - task will re-execute from scratch
		snapshot := NewContextSnapshot()
		snapshotJSON, _ = snapshot.ToJSON()
	} else if opts.ContextSnapshot != nil {
		snapshotJSON, err = opts.ContextSnapshot.ToJSON()
		if err != nil {
			m.logger.Warn("failed to serialize context snapshot",
				zap.String("session_id", sessionID),
				zap.Error(err),
			)
		}
	} else {
		// Create basic snapshot
		snapshot := NewContextSnapshot()
		snapshotJSON, _ = snapshot.ToJSON()
	}

	now := time.Now()
	updates := store.SessionUpdates{
		Status:                 stringPtr(SessionStatusSuspended),
		RunnerID:               nil, // Detach runner
		SuspendedAt:            &now,
		SuspendStrategy:        &opts.Strategy,
		PreviousRunnerID:       previousRunnerID,
		ContextSnapshot:        snapshotJSON,
		SuspendWorkspaceSynced: &opts.WorkspaceSynced,
	}

	if opts.SnapshotID != "" {
		updates.SuspendSnapshotID = &opts.SnapshotID
	}

	// Clear runner_id by setting empty string pointer (store layer will handle NULL)
	emptyStr := ""
	updates.RunnerID = &emptyStr

	if err := m.store.UpdateSession(ctx, sessionID, updates); err != nil {
		return err
	}

	m.logger.Info("session suspended",
		zap.String("session_id", sessionID),
		zap.String("strategy", opts.Strategy),
		zap.Stringp("previous_runner_id", previousRunnerID),
		zap.Bool("workspace_synced", opts.WorkspaceSynced),
	)

	// Log audit event
	if m.auditLog != nil {
		details := map[string]any{
			"strategy":         opts.Strategy,
			"workspace_synced": opts.WorkspaceSynced,
		}
		if previousRunnerID != nil {
			details["previous_runner_id"] = *previousRunnerID
		}
		_ = audit.NewEvent(audit.ActionSessionSuspended).
			WithSystemActor().
			WithResource(audit.ResourceTypeSession, sessionID).
			WithSession(sessionID).
			WithDetails(details).
			WithSuccess(true).
			Log(ctx, m.auditLog)
	}

	return nil
}

// ResumeResult contains information about a resumed session.
type ResumeResult struct {
	// Session is the resumed session.
	Session *store.Session

	// ContextSnapshot is the saved context to restore.
	ContextSnapshot *ContextSnapshot

	// SuspendStrategy is the strategy used when suspended.
	SuspendStrategy string

	// SnapshotID is the snapshot ID if one was created during suspend.
	SnapshotID string

	// WorkspaceSynced indicates if workspace was synced during suspend.
	WorkspaceSynced bool
}

// Resume transitions a session from suspended to resuming.
// The session will be fully activated once a runner is attached.
func (m *SessionManager) Resume(ctx context.Context, sessionID string) error {
	_, err := m.ResumeWithResult(ctx, sessionID)
	return err
}

// ResumeWithResult transitions a session from suspended to resuming and returns resume info.
func (m *SessionManager) ResumeWithResult(ctx context.Context, sessionID string) (*ResumeResult, error) {
	session, err := m.store.GetSession(ctx, sessionID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, ErrSessionNotFound
		}
		return nil, err
	}

	// Validate transition
	if !isValidSessionTransition(session.Status, SessionStatusResuming) {
		m.logger.Warn("invalid session transition for resume",
			zap.String("session_id", sessionID),
			zap.String("from", session.Status),
			zap.String("to", SessionStatusResuming),
		)
		return nil, ErrInvalidSessionTransition
	}

	// Parse context snapshot
	var contextSnapshot *ContextSnapshot
	if len(session.ContextSnapshot) > 0 {
		contextSnapshot, err = ParseContextSnapshot(session.ContextSnapshot)
		if err != nil {
			m.logger.Warn("failed to parse context snapshot",
				zap.String("session_id", sessionID),
				zap.Error(err),
			)
		}
	}

	now := time.Now()
	updates := store.SessionUpdates{
		Status:    stringPtr(SessionStatusResuming),
		ResumedAt: &now,
	}

	if err := m.store.UpdateSession(ctx, sessionID, updates); err != nil {
		return nil, err
	}

	m.logger.Info("session resuming",
		zap.String("session_id", sessionID),
		zap.Stringp("suspend_strategy", session.SuspendStrategy),
	)

	// Log audit event
	if m.auditLog != nil {
		details := map[string]any{}
		if session.SuspendStrategy != nil {
			details["suspend_strategy"] = *session.SuspendStrategy
		}
		_ = audit.NewEvent(audit.ActionSessionResumed).
			WithSystemActor().
			WithResource(audit.ResourceTypeSession, sessionID).
			WithSession(sessionID).
			WithDetails(details).
			WithSuccess(true).
			Log(ctx, m.auditLog)
	}

	result := &ResumeResult{
		Session:         session,
		ContextSnapshot: contextSnapshot,
	}

	if session.SuspendStrategy != nil {
		result.SuspendStrategy = *session.SuspendStrategy
	}
	if session.SuspendSnapshotID != nil {
		result.SnapshotID = *session.SuspendSnapshotID
	}
	if session.SuspendWorkspaceSynced != nil {
		result.WorkspaceSynced = *session.SuspendWorkspaceSynced
	}

	// Request a runner for the resumed session.
	// For provider-managed runners: spawn a new one
	// For external runners: re-attach if still connected
	// Use context.Background() since this runs asynchronously after the HTTP request completes.
	if session.PreviousRunnerID != nil && *session.PreviousRunnerID != "" {
		go m.requestRunnerForResume(context.Background(), session)
	}

	return result, nil
}

// requestRunnerForResume requests a new runner from the provider for a resuming session.
// This is called asynchronously to not block the resume operation.
// Only spawns a new runner if the previous runner was managed by a provider.
// For external/manually started runners, we wait for them to reconnect on their own.
func (m *SessionManager) requestRunnerForResume(ctx context.Context, session *store.Session) {
	// Check if previous runner was managed by a provider
	var prov provider.Provider

	if session.PreviousRunnerID != nil && *session.PreviousRunnerID != "" {
		prevRunner, err := m.store.GetRunner(ctx, *session.PreviousRunnerID)
		if err != nil {
			m.logger.Debug("previous runner not found, skipping provider spawn",
				zap.String("session_id", session.ID),
				zap.Stringp("previous_runner_id", session.PreviousRunnerID),
			)
			return
		}

		// If runner has no provider config, it's an external/manual runner
		// Check if it's still connected and re-attach if so
		if prevRunner.ProviderConfigID == nil {
			if m.connManager != nil && m.connManager.IsConnected(prevRunner.ID) {
				m.logger.Info("previous external runner still connected, re-attaching",
					zap.String("session_id", session.ID),
					zap.String("runner_id", prevRunner.ID),
				)
				if err := m.AttachRunner(ctx, session.ID, prevRunner.ID); err != nil {
					m.logger.Error("failed to re-attach external runner",
						zap.String("session_id", session.ID),
						zap.String("runner_id", prevRunner.ID),
						zap.Error(err),
					)
				}
			} else {
				m.logger.Debug("previous runner is external (no provider config), waiting for reconnect",
					zap.String("session_id", session.ID),
					zap.String("runner_id", prevRunner.ID),
				)
			}
			return
		}

		// Get provider from runner's config
		provConfig, err := m.store.GetProviderConfig(ctx, *prevRunner.ProviderConfigID)
		if err != nil {
			m.logger.Warn("failed to get provider config for runner",
				zap.String("session_id", session.ID),
				zap.Stringp("provider_config_id", prevRunner.ProviderConfigID),
				zap.Error(err),
			)
			return
		}

		prov, err = m.providerRegistry.Get(ctx, provConfig.Name)
		if err != nil {
			m.logger.Warn("failed to get provider for session resume",
				zap.String("session_id", session.ID),
				zap.String("provider_name", provConfig.Name),
				zap.Error(err),
			)
			return
		}
	} else {
		// No previous runner - this shouldn't happen for resume, but handle gracefully
		m.logger.Warn("no previous runner for session resume, skipping provider spawn",
			zap.String("session_id", session.ID),
		)
		return
	}

	// Check if provider supports suspend/resume
	suspendProv, ok := prov.(provider.SuspendableProvider)
	if !ok {
		m.logger.Debug("provider does not support suspend/resume, skipping runner request",
			zap.String("session_id", session.ID),
			zap.String("provider", prov.Name()),
		)
		return
	}

	// Get workspace path for spawning new runner
	var workspacePath string
	if m.workspaceManager != nil {
		workspacePath, _ = m.workspaceManager.GetHostPath(ctx, session.WorkspaceID)
	}

	// Build resume options
	resumeOpts := provider.ResumeOptions{
		SpawnOpts: &provider.SpawnOptions{
			RunnerID:       id.Runner(),
			Name:           "runner-" + session.ID,
			WorkspaceMount: workspacePath,
			SandboxMode:    "runner-is-sandbox",
			// TODO: Get server URL and token from config
		},
	}

	if session.PreviousRunnerID != nil {
		resumeOpts.RunnerID = *session.PreviousRunnerID
	}

	m.logger.Info("requesting runner from provider for resume",
		zap.String("session_id", session.ID),
		zap.String("provider", prov.Name()),
	)

	// Call provider to spawn/resume runner
	instance, err := suspendProv.Resume(ctx, session.ID, resumeOpts)
	if err != nil {
		m.logger.Error("failed to resume runner from provider",
			zap.String("session_id", session.ID),
			zap.Error(err),
		)
		return
	}

	m.logger.Info("runner requested for resume",
		zap.String("session_id", session.ID),
		zap.String("runner_id", instance.ID),
		zap.String("status", string(instance.Status)),
	)

	// The runner will connect via gRPC and be assigned to the session
	// via the normal AttachRunner flow
}

// GetContextSnapshot retrieves the context snapshot for a session.
func (m *SessionManager) GetContextSnapshot(ctx context.Context, sessionID string) (*ContextSnapshot, error) {
	session, err := m.store.GetSession(ctx, sessionID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, ErrSessionNotFound
		}
		return nil, err
	}

	if len(session.ContextSnapshot) == 0 {
		return nil, nil
	}

	return ParseContextSnapshot(session.ContextSnapshot)
}

// UpdateContextSnapshot updates the context snapshot for a session.
func (m *SessionManager) UpdateContextSnapshot(ctx context.Context, sessionID string, snapshot *ContextSnapshot) error {
	if snapshot == nil {
		return nil
	}

	snapshotJSON, err := snapshot.ToJSON()
	if err != nil {
		return err
	}

	updates := store.SessionUpdates{
		ContextSnapshot: snapshotJSON,
	}

	return m.store.UpdateSession(ctx, sessionID, updates)
}

// Terminate transitions a session to terminated status.
// This can be called from any non-terminated state.
func (m *SessionManager) Terminate(ctx context.Context, sessionID string) error {
	session, err := m.store.GetSession(ctx, sessionID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return ErrSessionNotFound
		}
		return err
	}

	// Check if already terminated
	if session.Status == SessionStatusTerminated {
		return ErrSessionAlreadyTerminated
	}

	// Store previous runner ID if attached
	previousRunnerID := session.RunnerID

	updates := store.SessionUpdates{
		Status: stringPtr(SessionStatusTerminated),
	}

	// Clear runner_id
	emptyStr := ""
	updates.RunnerID = &emptyStr
	updates.PreviousRunnerID = previousRunnerID

	if err := m.store.UpdateSession(ctx, sessionID, updates); err != nil {
		return err
	}

	m.logger.Info("session terminated",
		zap.String("session_id", sessionID),
		zap.String("from_status", session.Status),
		zap.Stringp("previous_runner_id", previousRunnerID),
	)

	// Log audit event
	if m.auditLog != nil {
		details := map[string]any{
			"from_status": session.Status,
		}
		if previousRunnerID != nil {
			details["previous_runner_id"] = *previousRunnerID
		}
		_ = audit.NewEvent(audit.ActionSessionTerminated).
			WithSystemActor().
			WithResource(audit.ResourceTypeSession, sessionID).
			WithSession(sessionID).
			WithDetails(details).
			WithSuccess(true).
			Log(ctx, m.auditLog)
	}

	// Optionally cleanup workspace host directory
	// Note: The workspace database record is NOT deleted here - only the host directory
	// This is controlled by the WorkspaceManager's CleanupOnTerminate configuration
	if m.workspaceManager != nil {
		// Cleanup is handled by WorkspaceManager based on its configuration
		// We just log if cleanup fails, don't fail the termination
		if err := m.workspaceManager.CleanupHostDirectory(ctx, session.WorkspaceID); err != nil {
			m.logger.Warn("failed to cleanup workspace host directory on termination",
				zap.String("session_id", sessionID),
				zap.String("workspace_id", session.WorkspaceID),
				zap.Error(err),
			)
		}
	}

	return nil
}

// AttachRunner attaches a runner to a session.
// The session must be in pending or resuming state.
func (m *SessionManager) AttachRunner(ctx context.Context, sessionID, runnerID string) error {
	session, err := m.store.GetSession(ctx, sessionID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return ErrSessionNotFound
		}
		return err
	}

	// Check if session already has a runner
	if session.RunnerID != nil && *session.RunnerID != "" {
		return ErrSessionAlreadyHasRunner
	}

	// Only allow attaching to pending or resuming sessions
	if session.Status != SessionStatusPending && session.Status != SessionStatusResuming {
		return ErrInvalidSessionTransition
	}

	// Validate runner is available
	runner, err := m.store.GetRunner(ctx, runnerID)
	if err != nil {
		return err
	}

	// Skip idle check for resume scenarios where we're re-attaching to the previous runner.
	// This is necessary because the runner might still be "busy" processing the detach command
	// when we try to resume. The runner will become idle shortly after.
	isResumeToSameRunner := session.Status == SessionStatusResuming &&
		session.PreviousRunnerID != nil &&
		*session.PreviousRunnerID == runnerID

	if runner.Status != StatusIdle && !isResumeToSameRunner {
		return ErrRunnerNotIdle
	}

	// Ensure workspace host directory exists
	if m.workspaceManager != nil {
		if _, err := m.workspaceManager.EnsureHostDirectory(ctx, session.WorkspaceID); err != nil {
			m.logger.Error("failed to ensure workspace host directory",
				zap.String("session_id", sessionID),
				zap.String("workspace_id", session.WorkspaceID),
				zap.Error(err),
			)
			return err
		}
	}

	// Activate the session (which also attaches the runner)
	return m.Activate(ctx, sessionID, runnerID)
}

// GetWorkspaceHostPath returns the host filesystem path for a session's workspace.
// This path is used for mounting the workspace into containers.
func (m *SessionManager) GetWorkspaceHostPath(ctx context.Context, sessionID string) (string, error) {
	session, err := m.store.GetSession(ctx, sessionID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return "", ErrSessionNotFound
		}
		return "", err
	}

	if m.workspaceManager == nil {
		return "", nil
	}

	return m.workspaceManager.GetHostPath(ctx, session.WorkspaceID)
}

// DetachRunner detaches the runner from a session without changing status.
// This is called internally when a runner disconnects.
func (m *SessionManager) DetachRunner(ctx context.Context, sessionID string) error {
	session, err := m.store.GetSession(ctx, sessionID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return ErrSessionNotFound
		}
		return err
	}

	if session.RunnerID == nil || *session.RunnerID == "" {
		return ErrSessionNoRunner
	}

	previousRunnerID := session.RunnerID
	emptyStr := ""
	updates := store.SessionUpdates{
		RunnerID:         &emptyStr,
		PreviousRunnerID: previousRunnerID,
	}

	if err := m.store.UpdateSession(ctx, sessionID, updates); err != nil {
		return err
	}

	m.logger.Info("runner detached from session",
		zap.String("session_id", sessionID),
		zap.Stringp("runner_id", previousRunnerID),
	)

	return nil
}

// isValidSessionTransition checks if a session status transition is valid.
//
// Valid transitions (from plan):
//   - pending → active (runner assigned)
//   - active → suspended (idle timeout, explicit suspend)
//   - active → terminated (user terminates)
//   - suspended → resuming (user resumes)
//   - resuming → active (new runner attached)
//   - resuming → suspended (runner unavailable)
//   - * → terminated (always allowed, except from terminated)
func isValidSessionTransition(from, to string) bool {
	// Terminate is always allowed (except from terminated)
	if to == SessionStatusTerminated && from != SessionStatusTerminated {
		return true
	}

	switch from {
	case SessionStatusPending:
		// Can only go to active (when runner assigned)
		return to == SessionStatusActive

	case SessionStatusActive:
		// Can go to suspended or terminated
		return to == SessionStatusSuspended || to == SessionStatusTerminated

	case SessionStatusSuspended:
		// Can go to resuming or terminated
		return to == SessionStatusResuming || to == SessionStatusTerminated

	case SessionStatusResuming:
		// Can go to active (success) or suspended (failure)
		return to == SessionStatusActive || to == SessionStatusSuspended

	case SessionStatusTerminated:
		// Cannot transition from terminated
		return false

	default:
		// Unknown status, disallow
		return false
	}
}

// IsValidSessionTransition is exported for testing.
func IsValidSessionTransition(from, to string) bool {
	return isValidSessionTransition(from, to)
}

// sendAttachSession sends an AttachSession command to the runner.
// This is called after a session is activated to notify the agent.
func (m *SessionManager) sendAttachSession(ctx context.Context, session *store.Session, runnerID string) error {
	if m.cmdSender == nil {
		m.logger.Debug("cmdSender not configured, skipping AttachSession command")
		return nil
	}

	// Get runner info to determine sandbox mode
	runner, err := m.store.GetRunner(ctx, runnerID)
	if err != nil {
		m.logger.Warn("failed to get runner info, using default workspace path",
			zap.String("runner_id", runnerID),
			zap.Error(err),
		)
	}

	// Get workspace info
	workspace, err := m.store.GetWorkspace(ctx, session.WorkspaceID)
	if err != nil {
		return err
	}

	// Determine workspace path to send to agent
	// If workspaceManager is configured, use host path; otherwise fall back to workspace name
	workspacePath := workspace.Name
	if m.workspaceManager != nil {
		if hostPath, err := m.workspaceManager.GetHostPath(ctx, session.WorkspaceID); err == nil && hostPath != "" {
			// Check runner's sandbox mode to determine path
			// For Docker containers: workspace is mounted at /workspace
			// For local/native runners: use the actual host path
			if runner != nil && runner.SandboxMode == "runner-is-sandbox" {
				// Docker/container mode - workspace is mounted at /workspace
				workspacePath = "/workspace"
			} else {
				// Local/native mode - use actual host path
				workspacePath = hostPath
			}
		}
	}

	// Build agent config (for non-BYOK sessions)
	var agentConfig *pb.AgentConfig
	if !session.IsBYOK && session.AgentConfigID != nil {
		cfg, err := m.store.GetAgentConfig(ctx, *session.AgentConfigID)
		if err != nil {
			m.logger.Warn("failed to get agent config",
				zap.String("session_id", session.ID),
				zap.Stringp("agent_config_id", session.AgentConfigID),
				zap.Error(err),
			)
			// Continue without agent config - agent may use BYOK
		} else {
			agentConfig = &pb.AgentConfig{
				Agent:   cfg.Agent,
				Model:   stringValue(cfg.Model),
				BaseUrl: stringValue(cfg.BaseURL),
				// Note: API key is decrypted and passed separately for security
				// The actual decryption should happen in a crypto layer
			}
		}
	}

	// For BYOK or when no agent config, create minimal config
	if agentConfig == nil {
		agentConfig = &pb.AgentConfig{
			Agent: session.Agent,
		}
	}

	// Get pending permission responses (for resumed sessions)
	// Check if session was previously suspended (SuspendedAt is set)
	var pendingPerms []*pb.PendingPermissionResponse
	if session.SuspendedAt != nil {
		perms, err := m.store.ListPermissionRequests(ctx, store.ListPermissionRequestsOptions{
			SessionID: &session.ID,
			Status:    []string{"approved", "denied"},
		})
		if err != nil {
			m.logger.Warn("failed to get pending permissions",
				zap.String("session_id", session.ID),
				zap.Error(err),
			)
		} else {
			for _, p := range perms.Items {
				// Only include permissions that were responded to while suspended
				if p.RespondedAt != nil && p.RespondedAt.After(*session.SuspendedAt) {
					pendingPerms = append(pendingPerms, &pb.PendingPermissionResponse{
						RequestId:         p.ID,
						Approved:          p.Status == "approved",
						Reason:            stringValue(p.ResponseReason),
						RespondedBy:       stringValue(p.RespondedBy),
						RespondedAtUnixMs: p.RespondedAt.UnixMilli(),
						Tool:              p.Tool,
						Action:            p.Action,
						TaskId:            p.TaskID,
					})
				}
			}
		}
	}

	// Build and send the command
	cmd := &pb.ServerCommand{
		Payload: &pb.ServerCommand_AttachSession{
			AttachSession: &pb.AttachSession{
				SessionId:          session.ID,
				WorkspacePath:      workspacePath,
				ContextSnapshot:    session.ContextSnapshot,
				AgentConfig:        agentConfig,
				PendingPermissions: pendingPerms,
			},
		},
	}

	if err := m.cmdSender.SendCommand(runnerID, cmd); err != nil {
		return err
	}

	m.logger.Debug("AttachSession command sent",
		zap.String("session_id", session.ID),
		zap.String("runner_id", runnerID),
		zap.Int("pending_permissions", len(pendingPerms)),
	)

	return nil
}

// stringValue returns the string value or empty string if nil.
func stringValue(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// sendDetachSession sends a DetachSession command to the runner.
// This is called before a session is suspended to notify the agent.
func (m *SessionManager) sendDetachSession(ctx context.Context, session *store.Session, opts SuspendOptions) error {
	if m.cmdSender == nil {
		m.logger.Debug("cmdSender not configured, skipping DetachSession command")
		return nil
	}

	if session.RunnerID == nil || *session.RunnerID == "" {
		m.logger.Debug("no runner attached, skipping DetachSession command",
			zap.String("session_id", session.ID),
		)
		return nil
	}

	runnerID := *session.RunnerID

	// Build and send the command
	cmd := &pb.ServerCommand{
		Payload: &pb.ServerCommand_DetachSession{
			DetachSession: &pb.DetachSession{
				SessionId:   session.ID,
				SaveContext: true, // Always save context for potential resume
				Suspend: &pb.SuspendConfig{
					Strategy:      opts.Strategy,
					SyncWorkspace: opts.WorkspaceSynced,
					SaveSnapshot:  opts.SnapshotID != "",
				},
			},
		},
	}

	if err := m.cmdSender.SendCommand(runnerID, cmd); err != nil {
		m.logger.Warn("failed to send DetachSession command",
			zap.String("session_id", session.ID),
			zap.String("runner_id", runnerID),
			zap.Error(err),
		)
		// Don't fail the suspend if we can't notify the runner
		// The suspend will proceed and the runner will eventually time out
		return nil
	}

	m.logger.Debug("DetachSession command sent",
		zap.String("session_id", session.ID),
		zap.String("runner_id", runnerID),
		zap.String("strategy", opts.Strategy),
	)

	return nil
}

// detachSessionsFromRunner detaches any active sessions from the specified runner.
// This is called before attaching a new session to ensure only one session per runner.
// The exceptSessionID is excluded from detachment (this is the session being activated).
func (m *SessionManager) detachSessionsFromRunner(ctx context.Context, runnerID, exceptSessionID string) error {
	// Find all active sessions attached to this runner
	sessions, err := m.store.ListSessions(ctx, store.ListSessionsOptions{
		RunnerID: &runnerID,
		Status:   []string{SessionStatusActive},
	})
	if err != nil {
		return err
	}

	var detachErrors []error
	for _, session := range sessions.Items {
		// Skip the session being activated
		if session.ID == exceptSessionID {
			continue
		}

		m.logger.Info("detaching session from runner due to new activation",
			zap.String("session_id", session.ID),
			zap.String("runner_id", runnerID),
			zap.String("new_session_id", exceptSessionID),
		)

		// Suspend the old session (this will clear runner_id and set status to suspended)
		if err := m.Suspend(ctx, session.ID, "release_to_pool"); err != nil {
			m.logger.Warn("failed to suspend old session during runner reattachment",
				zap.String("session_id", session.ID),
				zap.String("runner_id", runnerID),
				zap.Error(err),
			)
			detachErrors = append(detachErrors, err)
		}
	}

	if len(detachErrors) > 0 {
		return detachErrors[0]
	}
	return nil
}
