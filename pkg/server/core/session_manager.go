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
	ErrNoRunnerAvailable        = errors.New("no runner available for session")
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
	EnsureRunner(ctx context.Context, sessionID string) (*store.Session, error)
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
	waker            RunnerAvailableNotifier
	provisioner      *RunnerProvisioner
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

// setProvisioner injects the runner provisioner. Like setTaskManager, this is
// a Wire-time injection rather than a constructor argument because the
// provisioner and the session manager are built from the same dependencies.
func (m *SessionManager) setProvisioner(p *RunnerProvisioner) {
	m.provisioner = p
}

// claimRunner takes the database-arbitrated claim on a runner for a session.
//
// It reports false for a runner somebody else holds, which is a lost race and
// not an error: the caller moves on to the next candidate.
func (m *SessionManager) claimRunner(ctx context.Context, runnerID, sessionID string) bool {
	won, err := m.store.ClaimRunner(ctx, runnerID, sessionID, store.DefaultRunnerClaimLease)
	if err != nil {
		m.logger.Warn("could not claim runner; skipping it",
			zap.String("runner_id", runnerID),
			zap.String("session_id", sessionID),
			zap.Error(err),
		)
		return false
	}
	return won
}

// releaseRunnerClaim drops a claim held by sessionID. It is safe to call for a
// runner that was never claimed, or whose claim has already been taken over.
//
// The release deliberately does not inherit the caller's cancellation: it runs
// after the work it was protecting, and dropping it because a request returned
// would leave the runner claimed until the lease expired.
func (m *SessionManager) releaseRunnerClaim(ctx context.Context, runnerID, sessionID string) {
	if runnerID == "" {
		return
	}
	if err := m.store.ReleaseRunnerClaim(context.WithoutCancel(ctx), runnerID, sessionID); err != nil {
		m.logger.Warn("failed to release runner claim; it will expire with its lease",
			zap.String("runner_id", runnerID),
			zap.String("session_id", sessionID),
			zap.Error(err),
		)
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

// setWaker injects the redispatch waker after construction.
//
// The waker needs both managers, so it cannot exist when either is built. Like
// setTaskManager this is package-private: production wiring happens once, in
// Wire.
func (m *SessionManager) setWaker(w RunnerAvailableNotifier) {
	m.waker = w
}

// wake asks the redispatch waker for a pass after this session gave up a
// runner. Safe with no waker wired.
func (m *SessionManager) wake(ctx context.Context) {
	if m.waker == nil {
		return
	}
	m.waker.RunnerAvailable(ctx, WakeTriggerRunnerFreed)
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
	workspace, err := m.store.GetWorkspace(ctx, opts.WorkspaceID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, errors.New("workspace not found")
		}
		return nil, err
	}

	tenantID, err := tenantFor(ctx, opts.TenantID)
	if err != nil {
		return nil, err
	}

	// A session and its workspace must belong to the same tenant. Row level
	// security stops a query from reading another tenant's workspace, but a
	// caller that already knows the id would otherwise be able to point a
	// session at it.
	if err := requireSameTenant("workspace", workspace.ID, tenantID, workspace.TenantID); err != nil {
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
		TenantID:           tenantID,
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

	// A session must not run on another tenant's runner: the runner mounts the
	// session's workspace and holds its credentials.
	if err := requireSameTenant("runner", runner.ID, session.TenantID, runner.TenantID); err != nil {
		return err
	}

	// Take the claim before touching anything. Detaching comes next, and
	// detaching without the claim is how a losing activation used to steal a
	// runner from the session that had just won it: two activations both passed
	// the checks above, the second detached the first and then took the runner,
	// and the session that believed it was running found itself suspended with
	// a task in flight.
	//
	// EnsureRunner already holds the claim for this session, and re-claiming as
	// the same session is allowed, so the allocation path passes straight
	// through. A direct activation - the admin route - contends here.
	if !m.claimRunner(ctx, runnerID, sessionID) {
		m.logger.Warn("runner is claimed by another session; refusing to activate",
			zap.String("session_id", sessionID),
			zap.String("runner_id", runnerID),
		)
		return ErrRunnerNotIdle
	}
	// Released on every path: once the session row names the runner,
	// runnerClaimed answers for it and the claim has done its job.
	defer m.releaseRunnerClaim(ctx, runnerID, sessionID)

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
			// A session that comes back with nothing running should pick up
			// its backlog rather than wait for another API call.
			m.dispatchPendingTask(bgCtx, sessionID)
		})
		return nil
	}

	// A session that just became active for the first time takes its backlog
	// too: the task may well have been created before any runner existed.
	m.background.Go("session-dispatch", func(bgCtx context.Context) {
		m.dispatchPendingTask(bgCtx, sessionID)
	})

	return nil
}

// dispatchPendingTask hands the session's backlog to the task manager.
// DispatchNext is a no-op when a task is already in flight, so this is safe to
// call after re-executing running tasks.
func (m *SessionManager) dispatchPendingTask(ctx context.Context, sessionID string) {
	if m.taskManager == nil {
		return
	}
	// A session that just gained a runner is an edge trigger: the runner is new
	// information, so the backlog gets one attempt now rather than waiting out
	// a timer set when no runner existed.
	if err := m.taskManager.DispatchNextNow(ctx, sessionID); err != nil {
		m.logger.Warn("failed to dispatch pending task after activation",
			zap.String("session_id", sessionID),
			zap.Error(err),
		)
	}
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

	// KeepRunner suspends the session without releasing its runner to the
	// provider.
	//
	// Set this when the runner is being handed to another session rather than
	// given up: releasing it there would pause, release or destroy the very
	// instance the next session is about to attach to.
	KeepRunner bool
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
	if opts.KeepRunner {
		m.logger.Debug("keeping runner: it is being handed to another session",
			zap.String("session_id", sessionID),
			zap.Stringp("runner_id", previousRunnerID),
		)
	} else {
		m.releaseRunner(ctx, sessionID, previousRunnerID, opts)

		// Redispatch trigger 2. The runner this session was holding is gone -
		// back to its pool, paused, or destroyed - and which of those it was is
		// deliberately not checked here. A wake is two queries when it finds
		// nothing, and guessing wrong in the other direction strands a session.
		if previousRunnerID != nil && *previousRunnerID != "" {
			m.wake(ctx)
		}
	}

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

	prov, runner, err := m.providerForRunner(ctx, *runnerID)
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
	instanceID := runnerInstanceID(runner)
	actual, err := m.applySuspendStrategy(releaseCtx, prov, sessionID, *runnerID, instanceID, strategy, opts)
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
	sessionID, runnerID, instanceID string,
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
			Strategy:           strategy,
			ProviderInstanceID: instanceID,
			SaveSnapshot:       opts.SnapshotID != "" || strategy == provider.SuspendStrategySnapshot,
			SyncWorkspace:      opts.WorkspaceSynced,
			Timeout:            providerSuspendTimeout,
		})
	}

	// PausableProvider carries no options, so this branch cannot pass the
	// instance id on. It is reachable only for a provider that can pause but
	// not suspend; every provider in tree implements SuspendableProvider and
	// is served above.
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

	if err := prov.Destroy(ctx, runnerID, provider.DestroyOptions{
		ProviderInstanceID: instanceID,
	}); err != nil {
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
// recordProviderInstance persists the provider-side instance id a provider
// just handed back.
//
// Resume can return either the instance it just woke or a brand new one, and
// in both cases this is the moment the server learns the id. Failing to record
// it does not fail the resume - the runner is up - but it does mean the next
// suspend has nothing to address, so it is logged loudly.
func (m *SessionManager) recordProviderInstance(ctx context.Context, instance *provider.RunnerInstance) {
	if instance == nil || instance.ID == "" || instance.ProviderID == "" {
		return
	}
	if err := m.store.UpdateRunner(ctx, instance.ID, store.RunnerUpdates{
		ProviderInstanceID: &instance.ProviderID,
	}); err != nil {
		m.logger.Error("failed to record provider instance id for runner",
			zap.String("runner_id", instance.ID),
			zap.String("provider_instance_id", instance.ProviderID),
			zap.Error(err),
		)
	}
}

// runnerInstanceID returns the provider instance id recorded for a runner, or
// "" when there is none. An empty id makes every provider fall back to its own
// lookup, which is what happened everywhere before this was persisted.
func runnerInstanceID(runner *store.Runner) string {
	if runner == nil || runner.ProviderInstanceID == nil {
		return ""
	}
	return *runner.ProviderInstanceID
}

// The runner row is returned alongside the provider because callers need
// runners.provider_instance_id to address the instance: it is the only handle
// that survives a server restart.
func (m *SessionManager) providerForRunner(ctx context.Context, runnerID string) (provider.Provider, *store.Runner, error) {
	runner, err := m.store.GetRunner(ctx, runnerID)
	if err != nil {
		return nil, nil, err
	}
	if runner.ProviderConfigID == nil || *runner.ProviderConfigID == "" {
		return nil, runner, nil
	}

	provConfig, err := m.store.GetProviderConfig(ctx, *runner.ProviderConfigID)
	if err != nil {
		return nil, runner, err
	}
	prov, err := m.providerRegistry.Get(ctx, provConfig.Name)
	return prov, runner, err
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
	// Compare-and-set on the status the caller validated above. The scheduled
	// session activator polls with a plain SELECT, so two replicas can find
	// the same session due at the same moment; without this both would resume
	// it and both would advance next_scheduled_at, silently skipping a run.
	updates := store.SessionUpdates{
		Status:         stringPtr(SessionStatusResuming),
		ExpectedStatus: &session.Status,
		ResumedAt:      &now,
	}

	if err := m.store.UpdateSession(ctx, sessionID, updates); err != nil {
		if errors.Is(err, store.ErrConflict) {
			m.logger.Debug("session was resumed by another server first",
				zap.String("session_id", sessionID),
				zap.String("from_status", session.Status),
			)
			return nil, ErrInvalidSessionTransition
		}
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

	// Kept beyond the block below: its provider_instance_id is what lets the
	// provider find an instance it cannot enumerate.
	var prevRunner *store.Runner

	if session.PreviousRunnerID != nil && *session.PreviousRunnerID != "" {
		var err error
		prevRunner, err = m.store.GetRunner(ctx, *session.PreviousRunnerID)
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

	// Spawn options come from the provisioner because a spawned runner needs a
	// row and a token to exist before it boots. Without one there is nothing to
	// hand the provider but a runner id nothing knows about and an empty token,
	// which produces an instance that can never connect and only bills.
	if m.provisioner == nil {
		m.logger.Error("cannot request a managed runner: no provisioner configured",
			zap.String("session_id", session.ID),
			zap.String("provider", prov.Name()),
		)
		return
	}

	provisionOpts := ProvisionOptions{
		Name:           "runner-" + session.ID,
		WorkspaceMount: workspacePath,
	}
	if profile != nil {
		provisionOpts.ProfileID = profile.ID
		if profile.ProviderConfigID != nil {
			provisionOpts.ProviderConfigID = *profile.ProviderConfigID
		}
	}
	if provisionOpts.ProviderConfigID == "" {
		provisionOpts.ProviderName = prov.Name()
	}

	spawnOpts, err := m.provisioner.PrepareSpawn(ctx, provisionOpts)
	if err != nil {
		m.logger.Error("failed to prepare spawn options for resume",
			zap.String("session_id", session.ID),
			zap.String("provider", prov.Name()),
			zap.Error(err),
		)
		return
	}
	spawnOpts.NetworkPolicy = session.NetworkPolicy
	spawnOpts.AllowedHosts = session.AllowedHosts

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
		resumeOpts.ProviderInstanceID = runnerInstanceID(prevRunner)
	}

	m.logger.Info("requesting runner from provider for resume",
		zap.String("session_id", session.ID),
		zap.String("provider", prov.Name()),
	)

	// Call provider to spawn/resume runner
	instance, err := suspendProv.Resume(ctx, session.ID, resumeOpts)
	if err != nil {
		m.provisioner.DiscardPrepared(ctx, spawnOpts.RunnerID)
		m.logger.Error("failed to resume runner from provider",
			zap.String("session_id", session.ID),
			zap.Error(err),
		)
		return
	}

	// The provider reused the existing instance, so the runner prepared for a
	// spawn that did not happen has to go back.
	if instance.ID != spawnOpts.RunnerID {
		m.provisioner.DiscardPrepared(ctx, spawnOpts.RunnerID)
	}

	m.recordProviderInstance(ctx, instance)

	m.logger.Info("runner requested for resume",
		zap.String("session_id", session.ID),
		zap.String("runner_id", instance.ID),
		zap.String("provider_instance_id", instance.ProviderID),
		zap.String("status", string(instance.Status)),
	)

	// The runner will connect via gRPC and be assigned to the session
	// via the normal AttachRunner flow
}

// acquireFromPool asks a pool provider for a runner that satisfies the
// session's profile, and returns its ID without attaching it.
//
// Both the resume path and the allocate-for-a-pending-session path go through
// here, so a pool sees one consistent set of requirements either way.
func (m *SessionManager) acquireFromPool(
	ctx context.Context,
	session *store.Session,
	poolProv provider.PoolAcquirer,
	profile *store.Profile,
) (string, error) {
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

		if selector := m.profileSelector(profile); selector != nil {
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

	runnerInfo, err := poolProv.AcquireFromPool(ctx, opts)
	if err != nil {
		m.logger.Error("failed to acquire runner from pool",
			zap.String("session_id", session.ID),
			zap.String("provider", poolProv.Name()),
			zap.Error(err),
		)
		return "", err
	}

	m.logger.Info("runner acquired from pool",
		zap.String("session_id", session.ID),
		zap.String("runner_id", runnerInfo.ID),
		zap.String("runner_name", runnerInfo.Name),
	)
	return runnerInfo.ID, nil
}

// requestRunnerFromPool acquires a runner from a pool provider for session resume.
func (m *SessionManager) requestRunnerFromPool(ctx context.Context, session *store.Session, poolProv provider.PoolAcquirer, profile *store.Profile) {
	m.logger.Info("requesting runner from pool for resume",
		zap.String("session_id", session.ID),
		zap.String("provider", poolProv.Name()),
	)

	runnerID, err := m.acquireFromPool(ctx, session, poolProv, profile)
	if err != nil {
		return
	}

	// Attach runner to session
	if err := m.AttachRunner(ctx, session.ID, runnerID); err != nil {
		m.logger.Error("failed to attach pool runner to session",
			zap.String("session_id", session.ID),
			zap.String("runner_id", runnerID),
			zap.Error(err),
		)
		// Release runner back to pool on failure
		if releaseErr := poolProv.ReleaseToPool(ctx, runnerID, false, ""); releaseErr != nil {
			m.logger.Error("failed to release runner back to pool",
				zap.String("runner_id", runnerID),
				zap.Error(releaseErr),
			)
		}
		return
	}
}

// runnerSelectionBatch bounds one pass over candidate runners.
const runnerSelectionBatch = 100

// EnsureRunner makes sure a session has a runner it can execute on, and returns
// the session as it stands afterwards.
//
// A pending session is activated here by allocating a runner, which is what
// makes "create a task" enough on its own: before this, a pending session sat
// there until an operator called the admin activate endpoint with a runner_id
// they had looked up by hand.
//
// It deliberately does not resume a suspended session - that is an explicit
// user action with its own cost - and it does not race an in-flight resume.
func (m *SessionManager) EnsureRunner(ctx context.Context, sessionID string) (*store.Session, error) {
	session, err := m.store.GetSession(ctx, sessionID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, ErrSessionNotFound
		}
		return nil, err
	}

	switch session.Status {
	case SessionStatusTerminated:
		return nil, ErrSessionAlreadyTerminated
	case SessionStatusSuspended:
		// Resuming costs a runner acquisition and possibly a workspace
		// restore; that stays an explicit action.
		return nil, ErrSessionNotActive
	case SessionStatusResuming:
		// A resume is already acquiring a runner. Attaching a second one here
		// would leave two runners believing they own the session.
		return nil, ErrNoRunnerAvailable
	case SessionStatusActive:
		if session.RunnerID != nil && *session.RunnerID != "" {
			return session, nil
		}
		// An active session with no runner is inconsistent; allocate one.
	}

	runnerID, err := m.allocateRunner(ctx, session)
	if err != nil {
		return nil, err
	}
	// Held until the session row names the runner, which is the point at which
	// runnerClaimed starts answering correctly for it.
	defer m.releaseRunnerClaim(ctx, runnerID, sessionID)

	if err := m.Activate(ctx, sessionID, runnerID); err != nil {
		m.logger.Error("failed to activate session on allocated runner",
			zap.String("session_id", sessionID),
			zap.String("runner_id", runnerID),
			zap.Error(err),
		)
		return nil, err
	}

	m.logger.Info("session activated on an allocated runner",
		zap.String("session_id", sessionID),
		zap.String("runner_id", runnerID),
	)

	return m.store.GetSession(ctx, sessionID)
}

// allocateRunner picks a runner for a session that has none.
//
// A session whose profile names a pool provider goes through that provider, so
// the pool stays the authority on what it hands out. Everything else is served
// from the runners that registered themselves with a runner token: those have
// no provider config, so there is nobody to ask but the database.
func (m *SessionManager) allocateRunner(ctx context.Context, session *store.Session) (string, error) {
	profile := m.loadProfile(ctx, session)

	if poolProv := m.poolProviderForProfile(ctx, profile); poolProv != nil {
		runnerID, err := m.acquireFromPool(ctx, session, poolProv, profile)
		if err != nil {
			return "", err
		}
		return runnerID, nil
	}

	return m.selectIdleRunner(ctx, session, profile)
}

// loadProfile returns the session's profile, or nil when it has none or it
// cannot be read. A missing profile means "no constraints", not "fail".
func (m *SessionManager) loadProfile(ctx context.Context, session *store.Session) *store.Profile {
	if session.ProfileID == nil || *session.ProfileID == "" {
		return nil
	}
	profile, err := m.store.GetProfile(ctx, *session.ProfileID)
	if err != nil {
		m.logger.Warn("failed to load session profile, allocating without its constraints",
			zap.String("session_id", session.ID),
			zap.Stringp("profile_id", session.ProfileID),
			zap.Error(err),
		)
		return nil
	}
	return profile
}

// poolProviderForProfile resolves the profile's provider when it is a pool.
// Returns nil when there is no profile, no provider config, or the provider is
// not a pool - all of which mean "select from self-registered runners instead".
func (m *SessionManager) poolProviderForProfile(ctx context.Context, profile *store.Profile) provider.PoolAcquirer {
	if profile == nil || profile.ProviderConfigID == nil || *profile.ProviderConfigID == "" {
		return nil
	}
	if m.providerRegistry == nil {
		return nil
	}

	provConfig, err := m.store.GetProviderConfig(ctx, *profile.ProviderConfigID)
	if err != nil {
		m.logger.Warn("failed to load provider config for profile",
			zap.String("profile_id", profile.ID),
			zap.Error(err),
		)
		return nil
	}

	prov, err := m.providerRegistry.Get(ctx, provConfig.Name)
	if err != nil {
		m.logger.Warn("failed to resolve provider for profile",
			zap.String("profile_id", profile.ID),
			zap.String("provider_name", provConfig.Name),
			zap.Error(err),
		)
		return nil
	}

	poolProv, ok := prov.(provider.PoolAcquirer)
	if !ok {
		return nil
	}
	return poolProv
}

// selectIdleRunner picks a connected, idle, unclaimed runner that satisfies the
// profile's selector.
func (m *SessionManager) selectIdleRunner(ctx context.Context, session *store.Session, profile *store.Profile) (string, error) {
	selector := m.profileSelector(profile)

	opts := store.ListRunnersOptions{
		BaseListOptions: store.BaseListOptions{Limit: runnerSelectionBatch},
		Status:          []string{StatusIdle},
	}
	if selector != nil {
		labels := map[string]string{}
		if selector.OS != "" {
			labels["os"] = selector.OS
		}
		if selector.Arch != "" {
			labels["arch"] = selector.Arch
		}
		if len(labels) > 0 {
			opts.Labels = labels
		}
	}

	runners, err := m.store.ListRunners(ctx, opts)
	if err != nil {
		return "", err
	}

	for _, runner := range runners.Items {
		if runner.Tainted {
			continue
		}
		// Runner selection runs under whatever context asked for it, and the
		// background triggers run with system access so they can serve every
		// tenant. That makes row level security no help here: without this
		// check a redispatch pass could hand tenant A's runner to tenant B's
		// session, and only Activate's tenant assertion would catch it - after
		// the candidate had already been chosen and the alternatives skipped.
		if !sameTenant(session.TenantID, runner.TenantID) {
			continue
		}
		if selector != nil && !hasAllCapabilities(runner.Capabilities, selector.Capabilities) {
			continue
		}
		if m.connManager != nil && !m.connManager.IsConnected(runner.ID) {
			continue
		}
		// The claim is taken BEFORE the database is asked whether a session
		// already owns the runner, and that order is the whole point. The other
		// way round, a caller that reads "unowned" can be overtaken by a caller
		// that claims, activates and releases before it gets to claim; it then
		// takes a runner on the strength of an answer that is no longer true.
		// Claiming first means any ownership committed before the check is
		// seen, and any ownership committed after it can only come from the one
		// holder of the claim.
		//
		// The claim is a conditional UPDATE on the runner row, so this holds
		// across processes and not merely across goroutines.
		if !m.claimRunner(ctx, runner.ID, session.ID) {
			continue
		}

		// "idle" is the runner's connection state, not an assignment: a runner
		// can be idle while a session still owns it.
		owned, err := m.runnerClaimed(ctx, runner.ID)
		if err != nil {
			m.logger.Warn("could not determine whether runner is claimed; skipping it",
				zap.String("runner_id", runner.ID),
				zap.Error(err),
			)
			m.releaseRunnerClaim(ctx, runner.ID, session.ID)
			continue
		}
		if owned {
			m.releaseRunnerClaim(ctx, runner.ID, session.ID)
			continue
		}
		return runner.ID, nil
	}

	m.logger.Warn("no runner available for session",
		zap.String("session_id", session.ID),
		zap.Int("candidates", len(runners.Items)),
	)
	return "", ErrNoRunnerAvailable
}

// profileSelector parses the profile's selector, treating an unparseable one as
// no constraints rather than as a reason to strand the session.
func (m *SessionManager) profileSelector(profile *store.Profile) *ProfileSelector {
	if profile == nil {
		return nil
	}
	selector, err := parseProfileSelector(profile.Selector)
	if err != nil {
		m.logger.Warn("failed to parse profile selector, allocating without it",
			zap.String("profile_id", profile.ID),
			zap.Error(err),
		)
		return nil
	}
	return selector
}

// runnerClaimed reports whether a live session already owns the runner.
func (m *SessionManager) runnerClaimed(ctx context.Context, runnerID string) (bool, error) {
	sessions, err := m.store.ListSessions(ctx, store.ListSessionsOptions{
		BaseListOptions: store.BaseListOptions{Limit: 1},
		RunnerID:        &runnerID,
		Status:          liveSessionStatuses,
	})
	if err != nil {
		return false, err
	}
	return len(sessions.Items) > 0, nil
}

// hasAllCapabilities reports whether have covers every entry in want.
func hasAllCapabilities(have, want []string) bool {
	if len(want) == 0 {
		return true
	}
	set := make(map[string]struct{}, len(have))
	for _, c := range have {
		set[c] = struct{}{}
	}
	for _, c := range want {
		if _, ok := set[c]; !ok {
			return false
		}
	}
	return true
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

	// Redispatch trigger 2: a terminated session releases whatever it held.
	if previousRunnerID != nil && *previousRunnerID != "" {
		m.wake(ctx)
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

	// Redispatch trigger 2: an explicit detach hands the runner back with no
	// suspend to carry the wake.
	m.wake(ctx)

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

	// Determine where the workspace is mounted for this runner. This is a
	// location, not an identity: a container sees /workspace regardless of
	// which workspace it holds. The identity travels separately, in
	// workspace_id and tenant_id below.
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
				SessionId:     session.ID,
				WorkspacePath: workspacePath,
				// Identity, not location. workspace_path is "/workspace" for
				// every container-mode session, so a runner keying content
				// addressed storage on it would collide every session's chunks
				// into one namespace.
				WorkspaceId: workspace.ID,
				TenantId:    stringValue(workspace.TenantID),
				// The snapshot a previous runner left behind. The runner that
				// made it is long gone, so the server is the only thing that
				// can hand its id back.
				WorkspaceManifestId: stringValue(session.WorkspaceManifestID),
				ContextSnapshot:     session.ContextSnapshot,
				AgentConfig:         agentConfig,
				PendingPermissions:  pendingPerms,
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

		// KeepRunner: the runner is being handed to the session being
		// activated, not given up. Releasing it to the provider here would
		// pause, release or destroy the instance we are about to attach.
		if err := m.SuspendWithOptions(ctx, session.ID, SuspendOptions{
			Strategy:   "release_to_pool",
			KeepRunner: true,
		}); err != nil {
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
