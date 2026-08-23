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

	// HandleTunnelData handles tunnel data from the server.
	HandleTunnelData(ctx context.Context, cmd *pb.TunnelData) (*pb.RunnerMessage, error)

	// HandleAttachSession handles a session attach command.
	HandleAttachSession(ctx context.Context, cmd *pb.AttachSession) (*pb.RunnerMessage, error)

	// HandleDetachSession handles a session detach command.
	HandleDetachSession(ctx context.Context, cmd *pb.DetachSession) (*pb.RunnerMessage, error)

	// HandleStartDesktopStream handles a desktop stream start command.
	HandleStartDesktopStream(ctx context.Context, cmd *pb.StartDesktopStream) (*pb.RunnerMessage, error)

	// HandleStopDesktopStream handles a desktop stream stop command.
	HandleStopDesktopStream(ctx context.Context, cmd *pb.StopDesktopStream) (*pb.RunnerMessage, error)
}

// SessionState holds the state of an attached session.
type SessionState struct {
	SessionID       string
	WorkspacePath   string
	AgentConfig     *pb.AgentConfig
	ContextSnapshot []byte
	AttachedAt      time.Time

	// Workspace is the CAS key for this session's workspace. It is only set
	// once the server tells the runner which workspace it holds.
	Workspace WorkspaceIdentity

	// WorkspaceManifestID is the snapshot to restore from on attach, and is
	// updated to the newest snapshot after each successful sync.
	WorkspaceManifestID string
}

// DefaultCommandHandler provides a basic implementation of CommandHandler.
// It manages sessions and workspaces, and can be extended for task execution.
type DefaultCommandHandler struct {
	workspace *WorkspaceManager
	syncer    *WorkspaceSyncer
	logger    *zap.Logger

	// Active sessions
	sessions   map[string]*SessionState
	sessionsMu sync.RWMutex

	// Callbacks for extensibility (set by executor in G3)
	OnExecuteTask       func(ctx context.Context, cmd *pb.ExecuteTask) (*pb.RunnerMessage, error)
	OnApprovePermission func(ctx context.Context, cmd *pb.ApprovePermission) error
	OnKillTask          func(ctx context.Context, cmd *pb.KillTask) error
	OnCreateTunnel      func(tunnelID, tunnelType string, localPort int) error
	OnTunnelData        func(ctx context.Context, cmd *pb.TunnelData) error
	OnDetachSession     func(sessionID string) error

	// Desktop streaming callbacks
	OnStartDesktopStream func(ctx context.Context, cmd *pb.StartDesktopStream) (*pb.RunnerMessage, error)
	OnStopDesktopStream  func(ctx context.Context, cmd *pb.StopDesktopStream) (*pb.RunnerMessage, error)
}

// NewDefaultCommandHandler creates a new default command handler.
func NewDefaultCommandHandler(workspace *WorkspaceManager, logger *zap.Logger) *DefaultCommandHandler {
	return &DefaultCommandHandler{
		workspace: workspace,
		logger:    logger.Named("handler"),
		sessions:  make(map[string]*SessionState),
	}
}

// SetWorkspaceSyncer attaches the CAS syncer used for suspend and resume.
// Without one, suspends report the workspace as not synced.
func (h *DefaultCommandHandler) SetWorkspaceSyncer(syncer *WorkspaceSyncer) {
	h.syncer = syncer
}

// workspaceIdentityFromAttach extracts the CAS key for the attached workspace.
//
// It returns an unknown identity today because AttachSession carries no
// workspace id: the server sends session_id and workspace_path, and
// workspace_path is the literal string "/workspace" for every container-mode
// session, so it cannot key anything. This is the single input the sync path
// is missing; see the NEEDS in the lane report for the proto fields.
func workspaceIdentityFromAttach(_ *pb.AttachSession) (WorkspaceIdentity, string) {
	return WorkspaceIdentity{}, ""
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
func (h *DefaultCommandHandler) HandleCreateTunnel(_ context.Context, cmd *pb.CreateTunnel) (*pb.RunnerMessage, error) {
	h.logger.Info("received create tunnel command",
		zap.String("tunnel_id", cmd.TunnelId),
		zap.String("type", cmd.Type),
		zap.Int32("local_port", cmd.LocalPort),
		zap.String("direction", cmd.Direction),
	)

	// Delegate to callback if set (for tunnel manager integration)
	if h.OnCreateTunnel != nil {
		if err := h.OnCreateTunnel(cmd.TunnelId, cmd.Type, int(cmd.LocalPort)); err != nil {
			h.logger.Error("failed to create tunnel relay",
				zap.String("tunnel_id", cmd.TunnelId),
				zap.Error(err),
			)
			return nil, err
		}
	}

	return nil, nil
}

// HandleTunnelData handles tunnel data from the server.
func (h *DefaultCommandHandler) HandleTunnelData(ctx context.Context, cmd *pb.TunnelData) (*pb.RunnerMessage, error) {
	h.logger.Debug("received tunnel data",
		zap.String("tunnel_id", cmd.TunnelId),
		zap.String("connection_id", cmd.ConnectionId),
		zap.Int("data_len", len(cmd.Data)),
		zap.Bool("eof", cmd.Eof),
	)

	// Delegate to callback if set (for tunnel manager integration)
	if h.OnTunnelData != nil {
		if err := h.OnTunnelData(ctx, cmd); err != nil {
			return nil, err
		}
	}

	// No response message needed - tunnel data responses are sent separately
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
	session.Workspace, session.WorkspaceManifestID = workspaceIdentityFromAttach(cmd)

	// Store session
	h.sessionsMu.Lock()
	h.sessions[cmd.SessionId] = session
	h.sessionsMu.Unlock()

	h.logger.Info("session attached",
		zap.String("session_id", cmd.SessionId),
		zap.String("workspace", cmd.WorkspacePath),
	)

	h.restoreWorkspace(ctx, session)

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
func (h *DefaultCommandHandler) HandleDetachSession(ctx context.Context, cmd *pb.DetachSession) (*pb.RunnerMessage, error) {
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

		if cmd.Suspend.SyncWorkspace {
			var reason string
			workspaceSynced, reason = h.syncWorkspace(ctx, session)
			if !workspaceSynced {
				// A workspace that could not be saved must not read as saved,
				// but it also must not fail the suspend: the runner is going
				// away either way, and refusing to suspend strands it.
				h.logger.Warn("workspace not synced on suspend",
					zap.String("session_id", cmd.SessionId),
					zap.String("reason", reason),
				)
			}
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

// HandleStartDesktopStream handles a desktop stream start command.
func (h *DefaultCommandHandler) HandleStartDesktopStream(ctx context.Context, cmd *pb.StartDesktopStream) (*pb.RunnerMessage, error) {
	h.logger.Info("received start desktop stream command",
		zap.String("stream_id", cmd.StreamId),
		zap.String("session_id", cmd.SessionId),
	)

	// Check if session is attached
	h.sessionsMu.RLock()
	_, exists := h.sessions[cmd.SessionId]
	h.sessionsMu.RUnlock()

	if !exists {
		h.logger.Warn("desktop stream for unattached session", zap.String("session_id", cmd.SessionId))
		return &pb.RunnerMessage{
			Payload: &pb.RunnerMessage_DesktopStreamError{
				DesktopStreamError: &pb.DesktopStreamError{
					StreamId:         cmd.StreamId,
					SessionId:        cmd.SessionId,
					Error:            "session not attached",
					ErrorCode:        "session_not_attached",
					Recoverable:      false,
					OccurredAtUnixMs: time.Now().UnixMilli(),
				},
			},
		}, nil
	}

	// Delegate to callback if set
	if h.OnStartDesktopStream != nil {
		return h.OnStartDesktopStream(ctx, cmd)
	}

	// Default: return error indicating no provider
	return &pb.RunnerMessage{
		Payload: &pb.RunnerMessage_DesktopStreamError{
			DesktopStreamError: &pb.DesktopStreamError{
				StreamId:         cmd.StreamId,
				SessionId:        cmd.SessionId,
				Error:            "no desktop streaming provider configured",
				ErrorCode:        "provider_unavailable",
				Recoverable:      false,
				OccurredAtUnixMs: time.Now().UnixMilli(),
			},
		},
	}, nil
}

// HandleStopDesktopStream handles a desktop stream stop command.
func (h *DefaultCommandHandler) HandleStopDesktopStream(ctx context.Context, cmd *pb.StopDesktopStream) (*pb.RunnerMessage, error) {
	h.logger.Info("received stop desktop stream command",
		zap.String("stream_id", cmd.StreamId),
		zap.String("reason", cmd.Reason),
	)

	// Delegate to callback if set
	if h.OnStopDesktopStream != nil {
		return h.OnStopDesktopStream(ctx, cmd)
	}

	// Default: return stopped confirmation
	return &pb.RunnerMessage{
		Payload: &pb.RunnerMessage_DesktopStreamStopped{
			DesktopStreamStopped: &pb.DesktopStreamStopped{
				StreamId:        cmd.StreamId,
				Reason:          cmd.Reason,
				StoppedAtUnixMs: time.Now().UnixMilli(),
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

// syncWorkspace stores the session's workspace in CAS and reports whether it
// actually landed, plus why not when it did not.
//
// It never returns an error: the suspend path must proceed regardless. The
// caller's job is to report the truth, not to abort.
func (h *DefaultCommandHandler) syncWorkspace(ctx context.Context, session *SessionState) (synced bool, reason string) {
	if !h.syncer.Available() {
		return false, ErrSyncUnavailable.Error()
	}

	result, err := h.syncer.Sync(ctx, session.Workspace, h.workspace.Resolve(session.WorkspacePath))
	if err != nil {
		h.logger.Warn("workspace sync failed",
			zap.String("session_id", session.SessionID),
			zap.String("workspace_id", session.Workspace.WorkspaceID),
			zap.Error(err),
		)
	}
	if result.Synced {
		session.WorkspaceManifestID = result.ManifestID
	}

	return result.Synced, result.Reason
}

// restoreWorkspace materializes a previously synced workspace when the runner
// has nothing local for this session.
//
// A populated workspace is left alone: the runner may be re-attaching to work
// it still holds, and overwriting that with an older snapshot would destroy it.
func (h *DefaultCommandHandler) restoreWorkspace(ctx context.Context, session *SessionState) {
	if !h.syncer.Available() || !session.Workspace.Known() {
		return
	}
	if session.WorkspaceManifestID == "" {
		// Nothing was ever synced for this workspace, or the server did not
		// tell us which snapshot to use. Either way there is nothing to
		// restore, and guessing would be worse than starting empty.
		return
	}

	dir := h.workspace.Resolve(session.WorkspacePath)

	empty, err := IsEmptyDir(dir)
	if err != nil {
		h.logger.Warn("cannot inspect workspace before restore",
			zap.String("session_id", session.SessionID),
			zap.String("dir", dir),
			zap.Error(err),
		)
		return
	}
	if !empty {
		h.logger.Debug("workspace already populated, skipping restore",
			zap.String("session_id", session.SessionID),
			zap.String("dir", dir),
		)
		return
	}

	if err := h.syncer.Restore(ctx, session.Workspace, session.WorkspaceManifestID, dir); err != nil {
		// A failed restore leaves an empty workspace rather than a wrong one.
		// The task will run against nothing and say so, which beats running
		// against a half-materialized tree.
		h.logger.Error("workspace restore failed",
			zap.String("session_id", session.SessionID),
			zap.String("workspace_id", session.Workspace.WorkspaceID),
			zap.Error(err),
		)
		return
	}

	h.logger.Info("workspace restored from CAS",
		zap.String("session_id", session.SessionID),
		zap.String("workspace_id", session.Workspace.WorkspaceID),
	)
}
