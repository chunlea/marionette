package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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

// ProfileResources defines the resource configuration from a profile.
type ProfileResources struct {
	CPU    int    `json:"cpu"`
	Memory string `json:"memory"` // e.g., "8GB"
	Disk   string `json:"disk"`   // e.g., "50GB"
}

// ProfileNetwork defines the network configuration from a profile.
type ProfileNetwork struct {
	Level        string   `json:"level"`         // "none", "allow_list", "proxy", "air_gapped"
	AllowedHosts []string `json:"allowed_hosts"` // For allow_list mode
}

// ProfileSelector defines the runner selector constraints from a profile.
type ProfileSelector struct {
	OS           string   `json:"os,omitempty"`           // e.g., "darwin", "linux"
	Arch         string   `json:"arch,omitempty"`         // e.g., "arm64", "amd64"
	Capabilities []string `json:"capabilities,omitempty"` // e.g., ["gpu", "xcode"]
}

// parseProfileResources parses a profile's resources JSON.
func parseProfileResources(data json.RawMessage) (*ProfileResources, error) {
	if len(data) == 0 || string(data) == "{}" {
		return nil, nil
	}
	var resources ProfileResources
	if err := json.Unmarshal(data, &resources); err != nil {
		return nil, err
	}
	return &resources, nil
}

// parseProfileNetwork parses a profile's network JSON.
func parseProfileNetwork(data json.RawMessage) (*ProfileNetwork, error) {
	if len(data) == 0 || string(data) == "{}" {
		return nil, nil
	}
	var network ProfileNetwork
	if err := json.Unmarshal(data, &network); err != nil {
		return nil, err
	}
	return &network, nil
}

// parseProfileSelector parses a profile's selector JSON.
func parseProfileSelector(data json.RawMessage) (*ProfileSelector, error) {
	if len(data) == 0 || string(data) == "{}" {
		return nil, nil
	}
	var selector ProfileSelector
	if err := json.Unmarshal(data, &selector); err != nil {
		return nil, err
	}
	return &selector, nil
}

// parseMemorySize parses a memory size string like "8GB" to megabytes.
func parseMemorySize(s string) int {
	if s == "" {
		return 0
	}
	// Try to parse the numeric part
	var num int
	var unit string
	_, err := parseSize(s, &num, &unit)
	if err != nil {
		return 0
	}
	switch unit {
	case "GB", "G", "gb", "g":
		return num * 1024
	case "MB", "M", "mb", "m":
		return num
	case "TB", "T", "tb", "t":
		return num * 1024 * 1024
	default:
		return num
	}
}

// parseSize parses a size string into number and unit.
func parseSize(s string, num *int, unit *string) (int, error) {
	// Find where the unit starts
	i := 0
	for i < len(s) && (s[i] >= '0' && s[i] <= '9') {
		i++
	}
	if i == 0 {
		return 0, errors.New("no numeric part")
	}
	n := 0
	for j := 0; j < i; j++ {
		n = n*10 + int(s[j]-'0')
	}
	*num = n
	*unit = s[i:]
	return n, nil
}

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
	webhooks         *WebhookIntegration
	background       *backgroundTasks
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
	Webhooks         *WebhookIntegration
	// Background runs work that must outlive the request that started it.
	// Wire supplies the shared pool; when nil the manager makes its own.
	Background *backgroundTasks
	Logger     *zap.Logger
}

// NewSessionManager creates a new SessionManager.
func NewSessionManager(store store.Store, connManager ConnectionManagerInterface, cmdSender CommandSender, logger *zap.Logger) *SessionManager {
	return &SessionManager{
		store:       store,
		connManager: connManager,
		cmdSender:   cmdSender,
		background:  newBackgroundTasks(context.Background(), 0, logger),
		logger:      logger,
	}
}

// NewSessionManagerWithConfig creates a new SessionManager with full configuration.
func NewSessionManagerWithConfig(cfg SessionManagerConfig) *SessionManager {
	background := cfg.Background
	if background == nil {
		background = newBackgroundTasks(context.Background(), 0, cfg.Logger)
	}
	return &SessionManager{
		store:            cfg.Store,
		connManager:      cfg.ConnManager,
		cmdSender:        cfg.CmdSender,
		workspaceManager: cfg.WorkspaceManager,
		auditLog:         cfg.AuditLog,
		providerRegistry: cfg.ProviderRegistry,
		webhooks:         cfg.Webhooks,
		background:       background,
		logger:           cfg.Logger,
	}
}

// setTaskManager injects the task manager after construction.
//
// SessionManager and TaskManager reference each other: TaskManager needs the
// session manager to look up a session's runner, SessionManager needs the task
// manager to re-execute running tasks after a resume. That value-level cycle
// cannot be resolved by constructor arguments alone, so Wire builds the session
// manager first and closes the loop here. This is deliberately package-private:
// production wiring happens exactly once, in Wire.
func (m *SessionManager) setTaskManager(tm TaskManagerInterface) {
	m.taskManager = tm
}

// CreateSessionOptions contains options for creating a new session.
type CreateSessionOptions struct {
	Name          *string           // Optional session name
	WorkspaceID   string            // Required
	Agent         string            // Required (e.g., "claude")
	IsBYOK        bool              // Whether using BYOK mode
	AgentConfigID *string           // Optional, for managed credentials
	ProfileID     *string           // Optional, for runner configuration
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
		ProfileID:          opts.ProfileID,
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

	// Dispatch webhook event
	if m.webhooks != nil {
		m.webhooks.DispatchSessionEvent(ctx, "session.created", session)
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

	// Detach any session still holding this runner. A failure here used to be
	// logged and ignored, which let two sessions own one runner: both would
	// then dispatch tasks to it and interleave in the same workspace.
	// A partial detach must therefore abort the activation.
	if err := m.detachSessionsFromRunner(ctx, runnerID, sessionID); err != nil {
		m.logger.Error("failed to detach existing sessions from runner",
			zap.String("session_id", sessionID),
			zap.String("runner_id", runnerID),
			zap.Error(err),
		)
		return err
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

	// The partial unique index on sessions(runner_id) WHERE status = 'active'
	// is the real guard: two concurrent activations both pass the checks above,
	// and the database rejects the loser here.
	if err := store.WithTx(ctx, m.store, func(tx store.Tx) error {
		return tx.UpdateSession(ctx, sessionID, updates)
	}); err != nil {
		if errors.Is(err, store.ErrAlreadyExists) || errors.Is(err, store.ErrConflict) {
			m.logger.Warn("runner was claimed by another session during activation",
				zap.String("session_id", sessionID),
				zap.String("runner_id", runnerID),
				zap.Error(err),
			)
			return ErrRunnerNotIdle
		}
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
		// Dispatch session.resumed webhook event
		if m.webhooks != nil {
			if updatedSession, err := m.store.GetSession(ctx, sessionID); err == nil {
				m.webhooks.DispatchSessionEvent(ctx, "session.resumed", updatedSession)
			}
		}
		// Deliberately not the request context: re-execution outlives the
		// activation call, and cancelling it when the handler returns is how
		// resumed tasks used to silently never restart.
		m.background.Go("session-reexecute", func(bgCtx context.Context) {
			m.reExecuteRunningTasks(bgCtx, sessionID, runnerID)
		})
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

	// ctx is the application background context supplied by the caller, so it
	// is already independent of any request.
	for _, task := range tasks.Items {
		m.logger.Info("re-executing task after resume",
			zap.String("session_id", sessionID),
			zap.String("task_id", task.ID),
		)

		// Use ReExecute to reuse the existing task_run instead of creating a new one
		if err := m.taskManager.ReExecute(ctx, task.ID); err != nil {
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

	// The database write comes first. Sending DetachSession before it meant a
	// failed or partial write left the runner detached from a session the
	// database still believed was active, with no way to reconcile the two.
	if err := store.WithTx(ctx, m.store, func(tx store.Tx) error {
		return tx.UpdateSession(ctx, sessionID, updates)
	}); err != nil {
		return err
	}

	m.logger.Info("session suspended",
		zap.String("session_id", sessionID),
		zap.String("strategy", opts.Strategy),
		zap.Stringp("previous_runner_id", previousRunnerID),
		zap.Bool("workspace_synced", opts.WorkspaceSynced),
	)

	// Now tell the runner to detach. The session is already suspended, so a
	// failed send is not fatal: the runner has no session to report against and
	// will be reaped when its heartbeat goes stale. Recovery is therefore
	// "do nothing" - the database is authoritative and already correct.
	if err := m.sendDetachSession(ctx, session, opts); err != nil {
		m.logger.Warn("session suspended but the runner was not notified",
			zap.String("session_id", sessionID),
			zap.Stringp("runner_id", previousRunnerID),
			zap.Error(err),
		)
	}

	// Release the underlying infrastructure. Until this landed, "suspended"
	// only ever meant a row in the database: no provider was resolved, no
	// Suspend was called, and every container, pod and E2B sandbox kept running
	// (and billing) for the whole life of the "suspended" session.
	m.releaseRunner(ctx, sessionID, previousRunnerID, opts)

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

	// Dispatch webhook event
	if m.webhooks != nil {
		// Fetch updated session for webhook
		if updatedSession, err := m.store.GetSession(ctx, sessionID); err == nil {
			m.webhooks.DispatchSessionEvent(ctx, "session.suspended", updatedSession)
		}
	}

	return nil
}

// providerSuspendTimeout bounds a single provider suspend call. Suspend runs on
// the caller's path, so a wedged provider must not hold the request open.
const providerSuspendTimeout = 60 * time.Second

// releaseRunner releases the infrastructure behind a suspended session.
//
// The strategy recorded on the session drives the call:
//
//	pool provider            -> ReleaseToPool
//	SuspendableProvider      -> Suspend (provider picks the actual strategy)
//	PausableProvider + pause  -> Pause
//	any other managed provider -> Destroy
//
// The last line is the documented fallback: a provider that cannot suspend has
// only two honest options, stop paying or keep paying, and "suspended" must
// mean the former. External providers are left alone - we do not own them.
//
// Failures are logged and not propagated: the session is already suspended in
// the database, and an orphaned runner is picked up by the Reaper.
func (m *SessionManager) releaseRunner(ctx context.Context, sessionID string, runnerID *string, opts SuspendOptions) {
	if runnerID == nil || *runnerID == "" {
		return
	}
	if m.providerRegistry == nil {
		m.logger.Debug("no provider registry configured, leaving runner running",
			zap.String("session_id", sessionID),
			zap.String("runner_id", *runnerID),
		)
		return
	}

	prov, err := m.providerForRunner(ctx, *runnerID)
	if err != nil {
		m.logger.Warn("could not resolve provider to release runner",
			zap.String("session_id", sessionID),
			zap.String("runner_id", *runnerID),
			zap.Error(err),
		)
		return
	}
	if prov == nil {
		// External or manually registered runner: not ours to release.
		return
	}

	releaseCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), providerSuspendTimeout)
	defer cancel()

	strategy := provider.SuspendStrategy(opts.Strategy)
	actual, err := m.applySuspendStrategy(releaseCtx, prov, sessionID, *runnerID, strategy, opts)
	if err != nil {
		m.logger.Error("failed to release runner on suspend",
			zap.String("session_id", sessionID),
			zap.String("runner_id", *runnerID),
			zap.String("provider", prov.Name()),
			zap.String("strategy", string(strategy)),
			zap.Error(err),
		)
		return
	}

	m.logger.Info("runner released on suspend",
		zap.String("session_id", sessionID),
		zap.String("runner_id", *runnerID),
		zap.String("provider", prov.Name()),
		zap.String("requested_strategy", string(strategy)),
		zap.String("actual_strategy", string(actual.Strategy)),
	)

	m.recordSuspendResult(releaseCtx, sessionID, *runnerID, actual)
}

// applySuspendStrategy performs the provider-side release and reports the
// strategy that was actually used.
func (m *SessionManager) applySuspendStrategy(
	ctx context.Context,
	prov provider.Provider,
	sessionID, runnerID string,
	strategy provider.SuspendStrategy,
	opts SuspendOptions,
) (*provider.SuspendResult, error) {
	now := time.Now()

	if prov.Type() == provider.ProviderTypeExternal {
		return &provider.SuspendResult{Strategy: strategy, SuspendedAt: now}, nil
	}

	if pool, ok := prov.(provider.PoolAcquirer); ok {
		if err := pool.ReleaseToPool(ctx, runnerID, false, ""); err != nil {
			return nil, err
		}
		return &provider.SuspendResult{
			Strategy:    provider.SuspendStrategyReleaseToPool,
			SuspendedAt: now,
		}, nil
	}

	if susp, ok := prov.(provider.SuspendableProvider); ok {
		return susp.Suspend(ctx, runnerID, provider.SuspendOptions{
			Strategy:      strategy,
			SaveSnapshot:  opts.SnapshotID != "" || strategy == provider.SuspendStrategySnapshot,
			SyncWorkspace: opts.WorkspaceSynced,
			Timeout:       providerSuspendTimeout,
		})
	}

	if pausable, ok := prov.(provider.PausableProvider); ok && strategy == provider.SuspendStrategyPause {
		if err := pausable.Pause(ctx, runnerID); err != nil {
			return nil, err
		}
		return &provider.SuspendResult{
			Strategy:    provider.SuspendStrategyPause,
			SuspendedAt: now,
		}, nil
	}

	// Documented fallback: the provider cannot suspend, so terminate rather
	// than leave the instance running and billing against a session nobody is
	// using.
	//
	// Sessions outlive runners by design, so this is only safe while the
	// workspace lives outside the runner. If it does not, keep paying rather
	// than destroy the user's only copy of their work.
	if survives, reason := m.workspaceSurvivesRunner(ctx, sessionID); !survives {
		m.logger.Error("refusing to destroy runner on suspend: workspace would not survive it",
			zap.String("session_id", sessionID),
			zap.String("runner_id", runnerID),
			zap.String("provider", prov.Name()),
			zap.String("reason", reason),
		)
		return nil, fmt.Errorf("cannot destroy runner %s: %s", runnerID, reason)
	}

	m.logger.Warn("destroying runner on suspend: provider cannot suspend",
		zap.String("session_id", sessionID),
		zap.String("runner_id", runnerID),
		zap.String("provider", prov.Name()),
		zap.String("requested_strategy", string(strategy)),
		zap.String("reason", "provider implements neither SuspendableProvider nor a usable Pause"),
	)

	if err := prov.Destroy(ctx, runnerID); err != nil {
		return nil, err
	}
	return &provider.SuspendResult{
		Strategy:    provider.SuspendStrategyTerminate,
		SuspendedAt: now,
	}, nil
}

// workspaceSurvivesRunner reports whether a session's workspace lives outside
// its runner, and why not when it does not.
//
// Sessions outlive runners, so destroying a runner is only acceptable while the
// files are somewhere else: a host mount, a volume, shared storage or object
// sync. When the answer cannot be established, the answer is no - leaking a
// container costs money, losing a workspace loses work.
func (m *SessionManager) workspaceSurvivesRunner(ctx context.Context, sessionID string) (bool, string) {
	if m.workspaceManager == nil {
		return false, "no workspace manager configured, cannot establish where the workspace lives"
	}

	session, err := m.store.GetSession(ctx, sessionID)
	if err != nil {
		return false, "could not load session: " + err.Error()
	}

	ws, err := m.workspaceManager.Get(ctx, session.WorkspaceID)
	if err != nil {
		return false, "could not load workspace: " + err.Error()
	}
	if ws == nil {
		return false, "workspace not found"
	}

	// An explicitly ephemeral workspace has nothing to preserve.
	if !ws.Persist {
		return true, ""
	}

	// Shared and object-synced workspaces are external by definition.
	if ws.Mobility == WorkspaceMobilityShared || ws.Mobility == WorkspaceMobilityObjectSync {
		return true, ""
	}

	// Otherwise the workspace must be mounted from the host.
	hostPath, err := m.workspaceManager.GetHostPath(ctx, session.WorkspaceID)
	if err != nil {
		return false, "could not resolve workspace host path: " + err.Error()
	}
	if hostPath == "" {
		return false, "workspace is runner-local (persisted, no host mount, mobility=" + ws.Mobility + ")"
	}

	return true, ""
}

// recordSuspendResult writes back what the provider actually did, so a resume
// knows whether it is unpausing, restoring a snapshot or spawning fresh.
func (m *SessionManager) recordSuspendResult(
	ctx context.Context,
	sessionID, runnerID string,
	result *provider.SuspendResult,
) {
	strategy := string(result.Strategy)
	updates := store.SessionUpdates{SuspendStrategy: &strategy}
	if result.SnapshotID != "" {
		updates.SuspendSnapshotID = &result.SnapshotID
	}
	if result.WorkspaceSynced {
		synced := true
		updates.SuspendWorkspaceSynced = &synced
	}

	if err := m.store.UpdateSession(ctx, sessionID, updates); err != nil {
		m.logger.Warn("failed to record the suspend strategy the provider used",
			zap.String("session_id", sessionID),
			zap.String("strategy", strategy),
			zap.Error(err),
		)
	}

	// Keep the runner row honest about what happened to the instance.
	runnerStatus := StatusOffline
	if result.Strategy == provider.SuspendStrategyPause {
		runnerStatus = StatusPaused
	}
	if err := m.store.UpdateRunner(ctx, runnerID, store.RunnerUpdates{
		Status: stringPtr(runnerStatus),
	}); err != nil {
		m.logger.Warn("failed to update runner status after release",
			zap.String("runner_id", runnerID),
			zap.String("status", runnerStatus),
			zap.Error(err),
		)
	}
}

// providerForRunner resolves the provider that owns a runner.
// Returns (nil, nil) for runners with no provider config: those are external
// or manually registered and are not ours to manage.
func (m *SessionManager) providerForRunner(ctx context.Context, runnerID string) (provider.Provider, error) {
	runner, err := m.store.GetRunner(ctx, runnerID)
	if err != nil {
		return nil, err
	}
	if runner.ProviderConfigID == nil || *runner.ProviderConfigID == "" {
		return nil, nil
	}

	provConfig, err := m.store.GetProviderConfig(ctx, *runner.ProviderConfigID)
	if err != nil {
		return nil, err
	}
	return m.providerRegistry.Get(ctx, provConfig.Name)
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

	// Load profile for this session (used by both pool and managed providers)
	var profile *store.Profile
	if session.ProfileID != nil && *session.ProfileID != "" {
		var err error
		profile, err = m.store.GetProfile(ctx, *session.ProfileID)
		if err != nil {
			m.logger.Warn("failed to get profile for session resume, using defaults",
				zap.String("session_id", session.ID),
				zap.Stringp("profile_id", session.ProfileID),
				zap.Error(err),
			)
		}
	}

	// Check if provider is a pool acquirer (pool providers)
	if poolProv, ok := prov.(provider.PoolAcquirer); ok {
		m.requestRunnerFromPool(ctx, session, poolProv, profile)
		return
	}

	// Check if provider supports suspend/resume (managed providers)
	suspendProv, ok := prov.(provider.SuspendableProvider)
	if !ok {
		m.logger.Debug("provider does not support suspend/resume or pool acquisition, skipping runner request",
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

	// Build spawn options with defaults
	spawnOpts := &provider.SpawnOptions{
		RunnerID:       id.Runner(),
		Name:           "runner-" + session.ID,
		WorkspaceMount: workspacePath,
		SandboxMode:    "runner-is-sandbox",
		NetworkPolicy:  session.NetworkPolicy,
		AllowedHosts:   session.AllowedHosts,
	}

	// Apply profile configuration for managed providers
	if profile != nil {
		// Apply profile resources
		resources, err := parseProfileResources(profile.Resources)
		if err != nil {
			m.logger.Warn("failed to parse profile resources",
				zap.String("profile_id", profile.ID),
				zap.Error(err),
			)
		} else if resources != nil {
			spawnOpts.CPUs = float64(resources.CPU)
			spawnOpts.MemoryMB = parseMemorySize(resources.Memory)
			spawnOpts.DiskMB = parseMemorySize(resources.Disk)
		}

		// Apply profile network settings
		network, err := parseProfileNetwork(profile.Network)
		if err != nil {
			m.logger.Warn("failed to parse profile network",
				zap.String("profile_id", profile.ID),
				zap.Error(err),
			)
		} else if network != nil {
			// Profile network settings override session defaults
			if network.Level != "" {
				spawnOpts.NetworkPolicy = network.Level
			}
			if len(network.AllowedHosts) > 0 {
				spawnOpts.AllowedHosts = network.AllowedHosts
			}
		}

		m.logger.Info("applied profile configuration to runner",
			zap.String("session_id", session.ID),
			zap.String("profile_id", profile.ID),
			zap.Float64("cpus", spawnOpts.CPUs),
			zap.Int("memory_mb", spawnOpts.MemoryMB),
			zap.Int("disk_mb", spawnOpts.DiskMB),
			zap.String("network_policy", spawnOpts.NetworkPolicy),
		)
	}

	// Build resume options
	resumeOpts := provider.ResumeOptions{
		SpawnOpts: spawnOpts,
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

// requestRunnerFromPool acquires a runner from a pool provider for session resume.
func (m *SessionManager) requestRunnerFromPool(ctx context.Context, session *store.Session, poolProv provider.PoolAcquirer, profile *store.Profile) {
	// Build pool acquire options
	opts := provider.PoolAcquireOptions{
		SessionID: session.ID,
	}

	// Prefer previous runner if available
	if session.PreviousRunnerID != nil && *session.PreviousRunnerID != "" {
		opts.PreferRunnerID = *session.PreviousRunnerID
	}

	// Apply profile selector and capabilities if profile is specified
	if profile != nil {
		opts.ProfileID = profile.ID

		// Parse profile selector for required labels
		selector, err := parseProfileSelector(profile.Selector)
		if err != nil {
			m.logger.Warn("failed to parse profile selector",
				zap.String("profile_id", profile.ID),
				zap.Error(err),
			)
		} else if selector != nil {
			opts.RequiredLabels = make(map[string]string)
			if selector.OS != "" {
				opts.RequiredLabels["os"] = selector.OS
			}
			if selector.Arch != "" {
				opts.RequiredLabels["arch"] = selector.Arch
			}
			opts.RequiredCapabilities = selector.Capabilities
		}

		m.logger.Info("acquiring pool runner with profile requirements",
			zap.String("session_id", session.ID),
			zap.String("profile_id", profile.ID),
			zap.Any("required_labels", opts.RequiredLabels),
			zap.Strings("required_capabilities", opts.RequiredCapabilities),
		)
	}

	m.logger.Info("requesting runner from pool for resume",
		zap.String("session_id", session.ID),
		zap.String("provider", poolProv.Name()),
	)

	// Acquire runner from pool
	runnerInfo, err := poolProv.AcquireFromPool(ctx, opts)
	if err != nil {
		m.logger.Error("failed to acquire runner from pool",
			zap.String("session_id", session.ID),
			zap.Error(err),
		)
		return
	}

	m.logger.Info("runner acquired from pool",
		zap.String("session_id", session.ID),
		zap.String("runner_id", runnerInfo.ID),
		zap.String("runner_name", runnerInfo.Name),
	)

	// Attach runner to session
	if err := m.AttachRunner(ctx, session.ID, runnerInfo.ID); err != nil {
		m.logger.Error("failed to attach pool runner to session",
			zap.String("session_id", session.ID),
			zap.String("runner_id", runnerInfo.ID),
			zap.Error(err),
		)
		// Release runner back to pool on failure
		if releaseErr := poolProv.ReleaseToPool(ctx, runnerInfo.ID, false, ""); releaseErr != nil {
			m.logger.Error("failed to release runner back to pool",
				zap.String("runner_id", runnerInfo.ID),
				zap.Error(releaseErr),
			)
		}
		return
	}
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

	// Dispatch webhook event
	if m.webhooks != nil {
		// Fetch updated session for webhook
		if updatedSession, err := m.store.GetSession(ctx, sessionID); err == nil {
			m.webhooks.DispatchSessionEvent(ctx, "session.terminated", updatedSession)
		}
	}

	// Optionally cleanup workspace host directory.
	// Note: the workspace database record is NOT deleted here - only the host
	// directory, and only when no other session still uses the workspace.
	// CleanupHostDirectory is an unconditional os.RemoveAll, so terminating one
	// of N sessions sharing a workspace used to delete the other N-1's files.
	if m.workspaceManager != nil {
		m.cleanupWorkspaceIfUnused(ctx, sessionID, session.WorkspaceID)
	}

	return nil
}

// cleanupWorkspaceIfUnused removes a session's workspace directory from the
// host, but only once nothing else is using it. A failed IsInUse check is
// treated as "in use": losing a workspace is unrecoverable, leaking a directory
// is not.
func (m *SessionManager) cleanupWorkspaceIfUnused(ctx context.Context, sessionID, workspaceID string) {
	inUse, err := m.workspaceManager.IsInUse(ctx, workspaceID)
	if err != nil {
		m.logger.Warn("could not determine whether workspace is still in use; keeping it",
			zap.String("session_id", sessionID),
			zap.String("workspace_id", workspaceID),
			zap.Error(err),
		)
		return
	}
	if inUse {
		m.logger.Info("workspace still in use by another session, skipping cleanup",
			zap.String("session_id", sessionID),
			zap.String("workspace_id", workspaceID),
		)
		return
	}

	if err := m.workspaceManager.CleanupHostDirectory(ctx, workspaceID); err != nil {
		m.logger.Warn("failed to cleanup workspace host directory on termination",
			zap.String("session_id", sessionID),
			zap.String("workspace_id", workspaceID),
			zap.Error(err),
		)
	}
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
