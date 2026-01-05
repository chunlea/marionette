package agent

import (
	"context"
	"sync"
	"time"

	pb "github.com/chunlea/marionette/gen/proto/v1"
	"go.uber.org/zap"
)

// CommandHandler defines the interface for handling server commands.
type CommandHandler interface {
	// HandleExecuteTask handles a task execution command.
	HandleExecuteTask(ctx context.Context, cmd *pb.ExecuteTask) (*pb.RunnerMessage, error)

	// HandleApprovePermission handles a permission approval/denial.
	HandleApprovePermission(ctx context.Context, cmd *pb.ApprovePermission) (*pb.RunnerMessage, error)

	// HandleKillTask handles a task termination command.
	HandleKillTask(ctx context.Context, cmd *pb.KillTask) (*pb.RunnerMessage, error)

	// HandleCreateTunnel handles a tunnel creation command.
	HandleCreateTunnel(ctx context.Context, cmd *pb.CreateTunnel) (*pb.RunnerMessage, error)

	// HandleAttachSession handles a session attach command.
	HandleAttachSession(ctx context.Context, cmd *pb.AttachSession) (*pb.RunnerMessage, error)

	// HandleDetachSession handles a session detach command.
	HandleDetachSession(ctx context.Context, cmd *pb.DetachSession) (*pb.RunnerMessage, error)
}

// SessionState holds the state of an attached session.
type SessionState struct {
	SessionID       string
	WorkspacePath   string
	AgentConfig     *pb.AgentConfig
	ContextSnapshot []byte
	AttachedAt      time.Time
}

// DefaultCommandHandler provides a basic implementation of CommandHandler.
// It manages sessions and workspaces, and can be extended for task execution.
type DefaultCommandHandler struct {
	workspace *WorkspaceManager
	logger    *zap.Logger

	// Active sessions
	sessions   map[string]*SessionState
	sessionsMu sync.RWMutex

	// Callbacks for extensibility (set by executor in G3)
	OnExecuteTask       func(ctx context.Context, cmd *pb.ExecuteTask) (*pb.RunnerMessage, error)
	OnApprovePermission func(ctx context.Context, cmd *pb.ApprovePermission) error
	OnKillTask          func(ctx context.Context, cmd *pb.KillTask) error
	OnDetachSession     func(sessionID string) error
}

// NewDefaultCommandHandler creates a new default command handler.
func NewDefaultCommandHandler(workspace *WorkspaceManager, logger *zap.Logger) *DefaultCommandHandler {
	return &DefaultCommandHandler{
		workspace: workspace,
		logger:    logger.Named("handler"),
		sessions:  make(map[string]*SessionState),
	}
}

// HandleExecuteTask handles task execution.
// This is a stub that will be extended in G3 with actual executor logic.
func (h *DefaultCommandHandler) HandleExecuteTask(ctx context.Context, cmd *pb.ExecuteTask) (*pb.RunnerMessage, error) {
	h.logger.Info("received execute task command",
		zap.String("task_id", cmd.TaskId),
		zap.String("run_id", cmd.RunId),
		zap.String("session_id", cmd.SessionId),
		zap.Int32("attempt", cmd.Attempt),
	)

	// Check if session is attached
	h.sessionsMu.RLock()
	session, exists := h.sessions[cmd.SessionId]
	h.sessionsMu.RUnlock()

	if !exists {
		h.logger.Warn("task for unattached session", zap.String("session_id", cmd.SessionId))
		// Return task failed response
		return &pb.RunnerMessage{
			Payload: &pb.RunnerMessage_TaskCompleted{
				TaskCompleted: &pb.TaskCompleted{
					TaskId:  cmd.TaskId,
					RunId:   cmd.RunId,
					Attempt: cmd.Attempt,
					Success: false,
					Error:   "session not attached",
				},
			},
		}, nil
	}

	// If callback is set, delegate to it (for G3 executor integration)
	if h.OnExecuteTask != nil {
		return h.OnExecuteTask(ctx, cmd)
	}

	// Send task accepted
	acceptedMsg := &pb.RunnerMessage{
		Payload: &pb.RunnerMessage_TaskAccepted{
			TaskAccepted: &pb.TaskAccepted{
				TaskId:  cmd.TaskId,
				RunId:   cmd.RunId,
				Attempt: cmd.Attempt,
			},
		},
	}

	h.logger.Debug("task accepted",
		zap.String("task_id", cmd.TaskId),
		zap.String("workspace", session.WorkspacePath),
	)

	// For now, just return accepted (actual execution will be in G3)
	return acceptedMsg, nil
}

// HandleApprovePermission handles permission approval/denial.
func (h *DefaultCommandHandler) HandleApprovePermission(ctx context.Context, cmd *pb.ApprovePermission) (*pb.RunnerMessage, error) {
	h.logger.Info("received permission response",
		zap.String("request_id", cmd.RequestId),
		zap.Bool("approved", cmd.Approved),
		zap.String("reason", cmd.Reason),
		zap.Bool("from_cache", cmd.FromCache),
	)

	// Delegate to callback if set (for G4 permission handling)
	if h.OnApprovePermission != nil {
		if err := h.OnApprovePermission(ctx, cmd); err != nil {
			return nil, err
		}
	}

	// No response message needed for permission approval
	return nil, nil
}

// HandleKillTask handles task termination.
func (h *DefaultCommandHandler) HandleKillTask(ctx context.Context, cmd *pb.KillTask) (*pb.RunnerMessage, error) {
	h.logger.Info("received kill task command",
		zap.String("task_id", cmd.TaskId),
		zap.String("run_id", cmd.RunId),
		zap.String("reason", cmd.Reason),
	)

	// Delegate to callback if set
	if h.OnKillTask != nil {
		if err := h.OnKillTask(ctx, cmd); err != nil {
			return nil, err
		}
	}

	// No response message needed
	return nil, nil
}

// HandleCreateTunnel handles tunnel creation.
// This is a stub for later phase implementation.
func (h *DefaultCommandHandler) HandleCreateTunnel(_ context.Context, cmd *pb.CreateTunnel) (*pb.RunnerMessage, error) {
	h.logger.Info("received create tunnel command",
		zap.String("tunnel_id", cmd.TunnelId),
		zap.String("type", cmd.Type),
		zap.Int32("local_port", cmd.LocalPort),
		zap.String("direction", cmd.Direction),
	)

	// Tunnel creation will be implemented in a later phase
	return nil, nil
}

// HandleAttachSession handles session attachment.
func (h *DefaultCommandHandler) HandleAttachSession(ctx context.Context, cmd *pb.AttachSession) (*pb.RunnerMessage, error) {
	h.logger.Info("received attach session command",
		zap.String("session_id", cmd.SessionId),
		zap.String("workspace_path", cmd.WorkspacePath),
		zap.Bool("has_context", len(cmd.ContextSnapshot) > 0),
		zap.Int("pending_permissions", len(cmd.PendingPermissions)),
	)

	// Ensure workspace exists
	if err := h.workspace.EnsureExists(cmd.WorkspacePath); err != nil {
		h.logger.Error("failed to ensure workspace exists",
			zap.String("path", cmd.WorkspacePath),
			zap.Error(err),
		)
		return nil, err
	}

	// Create session state
	session := &SessionState{
		SessionID:       cmd.SessionId,
		WorkspacePath:   cmd.WorkspacePath,
		AgentConfig:     cmd.AgentConfig,
		ContextSnapshot: cmd.ContextSnapshot,
		AttachedAt:      time.Now(),
	}

	// Store session
	h.sessionsMu.Lock()
	h.sessions[cmd.SessionId] = session
	h.sessionsMu.Unlock()

	h.logger.Info("session attached",
		zap.String("session_id", cmd.SessionId),
		zap.String("workspace", cmd.WorkspacePath),
	)

	// Process pending permissions if any
	for _, perm := range cmd.PendingPermissions {
		h.logger.Info("processing pending permission",
			zap.String("request_id", perm.RequestId),
			zap.Bool("approved", perm.Approved),
		)
		// Will be handled by OnApprovePermission callback in G4
		if h.OnApprovePermission != nil {
			_ = h.OnApprovePermission(ctx, &pb.ApprovePermission{
				RequestId:         perm.RequestId,
				Approved:          perm.Approved,
				Reason:            perm.Reason,
				FromCache:         true,
				RespondedBy:       perm.RespondedBy,
				RespondedAtUnixMs: perm.RespondedAtUnixMs,
				Tool:              perm.Tool,
				Action:            perm.Action,
				TaskId:            perm.TaskId,
			})
		}
	}

	// Send session attached response
	return &pb.RunnerMessage{
		Payload: &pb.RunnerMessage_SessionAttached{
			SessionAttached: &pb.SessionAttached{
				SessionId: cmd.SessionId,
				Restored:  len(cmd.ContextSnapshot) > 0,
			},
		},
	}, nil
}

// HandleDetachSession handles session detachment.
func (h *DefaultCommandHandler) HandleDetachSession(_ context.Context, cmd *pb.DetachSession) (*pb.RunnerMessage, error) {
	h.logger.Info("received detach session command",
		zap.String("session_id", cmd.SessionId),
		zap.Bool("save_context", cmd.SaveContext),
	)

	// Cancel any running task for this session first
	if h.OnDetachSession != nil {
		if err := h.OnDetachSession(cmd.SessionId); err != nil {
			h.logger.Warn("failed to cancel task on session detach",
				zap.String("session_id", cmd.SessionId),
				zap.Error(err),
			)
		}
	}

	h.sessionsMu.Lock()
	session, exists := h.sessions[cmd.SessionId]
	if exists {
		delete(h.sessions, cmd.SessionId)
	}
	h.sessionsMu.Unlock()

	if !exists {
		h.logger.Warn("detaching non-existent session", zap.String("session_id", cmd.SessionId))
		return nil, nil
	}

	// Handle suspend configuration
	var strategy string
	var workspaceSynced bool

	if cmd.Suspend != nil {
		strategy = cmd.Suspend.Strategy
		h.logger.Info("session suspend requested",
			zap.String("strategy", strategy),
			zap.Bool("sync_workspace", cmd.Suspend.SyncWorkspace),
			zap.String("reason", cmd.Suspend.Reason),
		)

		// Workspace sync would be done here (implemented in Phase 5)
		if cmd.Suspend.SyncWorkspace {
			// TODO: Sync workspace to CAS
			workspaceSynced = false // Will be true when CAS sync is implemented
		}
	}

	h.logger.Info("session detached",
		zap.String("session_id", cmd.SessionId),
		zap.String("workspace", session.WorkspacePath),
	)

	// Send session suspended response
	return &pb.RunnerMessage{
		Payload: &pb.RunnerMessage_SessionSuspended{
			SessionSuspended: &pb.SessionSuspended{
				SessionId:         cmd.SessionId,
				Strategy:          strategy,
				ContextSaved:      cmd.SaveContext, // Simplified: assume context is saved
				WorkspaceSynced:   workspaceSynced,
				Success:           true,
				SuspendedAtUnixMs: time.Now().UnixMilli(),
			},
		},
	}, nil
}

// GetSession returns the session state for a given session ID.
func (h *DefaultCommandHandler) GetSession(sessionID string) (*SessionState, bool) {
	h.sessionsMu.RLock()
	defer h.sessionsMu.RUnlock()
	session, exists := h.sessions[sessionID]
	return session, exists
}

// ActiveSessionCount returns the number of active sessions.
func (h *DefaultCommandHandler) ActiveSessionCount() int {
	h.sessionsMu.RLock()
	defer h.sessionsMu.RUnlock()
	return len(h.sessions)
}
