package core

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	pb "github.com/chunlea/marionette/gen/proto/v1"
	"github.com/chunlea/marionette/pkg/id"
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
}

// SessionManager handles session lifecycle and state transitions.
type SessionManager struct {
	store       store.Store
	connManager ConnectionManagerInterface
	cmdSender   CommandSender
	logger      *zap.Logger
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
	if runner.Status != StatusIdle {
		return ErrRunnerNotIdle
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

	return nil
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

	// Prepare context snapshot
	var snapshotJSON json.RawMessage
	if opts.ContextSnapshot != nil {
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

	return result, nil
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
	if runner.Status != StatusIdle {
		return ErrRunnerNotIdle
	}

	// Activate the session (which also attaches the runner)
	return m.Activate(ctx, sessionID, runnerID)
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

	// Get workspace info
	workspace, err := m.store.GetWorkspace(ctx, session.WorkspaceID)
	if err != nil {
		return err
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
				WorkspacePath:      workspace.Name, // Use workspace name as path
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
