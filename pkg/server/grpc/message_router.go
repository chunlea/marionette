package grpc

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	pb "github.com/chunlea/marionette/gen/proto/v1"
	"github.com/chunlea/marionette/pkg/server/core"
	"github.com/chunlea/marionette/pkg/store"
	"go.uber.org/zap"
)

// MessageRouter routes incoming runner messages to appropriate handlers.
type MessageRouter struct {
	logger            *zap.Logger
	runnerManager     RunnerManagerInterface
	taskManager       core.TaskManagerInterface
	permissionManager core.PermissionManagerInterface
	sessionManager    core.SessionManagerInterface
	streamManager     core.StreamManagerInterface
	store             store.Store
}

// MessageRouterOption is a functional option for MessageRouter.
type MessageRouterOption func(*MessageRouter)

// WithMRTaskManager sets the task manager for the message router.
func WithMRTaskManager(tm core.TaskManagerInterface) MessageRouterOption {
	return func(r *MessageRouter) {
		r.taskManager = tm
	}
}

// WithMRPermissionManager sets the permission manager for the message router.
func WithMRPermissionManager(pm core.PermissionManagerInterface) MessageRouterOption {
	return func(r *MessageRouter) {
		r.permissionManager = pm
	}
}

// WithMRStore sets the store for the message router.
func WithMRStore(s store.Store) MessageRouterOption {
	return func(r *MessageRouter) {
		r.store = s
	}
}

// WithMRSessionManager sets the session manager for the message router.
func WithMRSessionManager(sm core.SessionManagerInterface) MessageRouterOption {
	return func(r *MessageRouter) {
		r.sessionManager = sm
	}
}

// WithMRStreamManager sets the stream manager for the message router.
func WithMRStreamManager(sm core.StreamManagerInterface) MessageRouterOption {
	return func(r *MessageRouter) {
		r.streamManager = sm
	}
}

// NewMessageRouter creates a new MessageRouter.
func NewMessageRouter(logger *zap.Logger, runnerManager RunnerManagerInterface, opts ...MessageRouterOption) *MessageRouter {
	r := &MessageRouter{
		logger:        logger,
		runnerManager: runnerManager,
	}
	for _, opt := range opts {
		opt(r)
	}
	return r
}

// HandleMessage routes a message from a runner to the appropriate handler.
func (r *MessageRouter) HandleMessage(ctx context.Context, runnerID string, msg *pb.RunnerMessage) error {
	if msg == nil {
		return nil
	}

	switch payload := msg.Payload.(type) {
	case *pb.RunnerMessage_Heartbeat:
		return r.handleHeartbeat(ctx, runnerID, payload.Heartbeat)

	case *pb.RunnerMessage_TaskAccepted:
		return r.handleTaskAccepted(ctx, runnerID, payload.TaskAccepted)

	case *pb.RunnerMessage_TaskStarted:
		return r.handleTaskStarted(ctx, runnerID, payload.TaskStarted)

	case *pb.RunnerMessage_TaskProgress:
		return r.handleTaskProgress(ctx, runnerID, payload.TaskProgress)

	case *pb.RunnerMessage_TaskCompleted:
		return r.handleTaskCompleted(ctx, runnerID, payload.TaskCompleted)

	case *pb.RunnerMessage_PermissionRequest:
		return r.handlePermissionRequest(ctx, runnerID, payload.PermissionRequest)

	case *pb.RunnerMessage_SessionAttached:
		return r.handleSessionAttached(ctx, runnerID, payload.SessionAttached)

	case *pb.RunnerMessage_SessionSuspended:
		return r.handleSessionSuspended(ctx, runnerID, payload.SessionSuspended)

	case *pb.RunnerMessage_ContextUpdate:
		return r.handleContextUpdate(ctx, runnerID, payload.ContextUpdate)

	case *pb.RunnerMessage_DesktopStreamStarted:
		return r.handleDesktopStreamStarted(ctx, runnerID, payload.DesktopStreamStarted)

	case *pb.RunnerMessage_DesktopStreamStopped:
		return r.handleDesktopStreamStopped(ctx, runnerID, payload.DesktopStreamStopped)

	case *pb.RunnerMessage_DesktopStreamError:
		return r.handleDesktopStreamError(ctx, runnerID, payload.DesktopStreamError)

	default:
		r.logger.Warn("unknown message type",
			zap.String("runner_id", runnerID),
			zap.String("type", fmt.Sprintf("%T", msg.Payload)),
		)
		return nil
	}
}

// handleHeartbeat processes a heartbeat message from a runner.
func (r *MessageRouter) handleHeartbeat(ctx context.Context, runnerID string, hb *pb.Heartbeat) error {
	r.logger.Debug("received heartbeat",
		zap.String("runner_id", runnerID),
		zap.String("status", hb.GetStatus()),
	)

	if r.runnerManager != nil {
		return r.runnerManager.OnHeartbeat(ctx, runnerID, hb)
	}
	return nil
}

// handleTaskAccepted processes a task accepted message.
// Updates task run status to "assigned".
func (r *MessageRouter) handleTaskAccepted(ctx context.Context, runnerID string, msg *pb.TaskAccepted) error {
	r.logger.Debug("task accepted",
		zap.String("runner_id", runnerID),
		zap.String("task_id", msg.GetTaskId()),
		zap.String("run_id", msg.GetRunId()),
	)

	if r.taskManager == nil {
		r.logger.Warn("no task manager configured, skipping task accepted handling")
		return nil
	}

	return r.taskManager.OnTaskAccepted(ctx, msg.GetRunId())
}

// handleTaskStarted processes a task started message.
// Updates task run status to "running" and runner to "busy".
func (r *MessageRouter) handleTaskStarted(ctx context.Context, runnerID string, msg *pb.TaskStarted) error {
	r.logger.Debug("task started",
		zap.String("runner_id", runnerID),
		zap.String("task_id", msg.GetTaskId()),
		zap.String("run_id", msg.GetRunId()),
	)

	if r.taskManager == nil {
		r.logger.Warn("no task manager configured, skipping task started handling")
		return nil
	}

	// Update task run status
	if err := r.taskManager.OnTaskStarted(ctx, msg.GetRunId()); err != nil {
		return err
	}

	// Set runner to busy
	if r.runnerManager != nil {
		return r.runnerManager.SetStatus(ctx, runnerID, "busy")
	}
	return nil
}

// handleTaskProgress processes a task progress message.
// Forwards progress to TaskManager for real-time updates.
func (r *MessageRouter) handleTaskProgress(ctx context.Context, runnerID string, msg *pb.TaskProgress) error {
	r.logger.Debug("task progress",
		zap.String("runner_id", runnerID),
		zap.String("task_id", msg.GetTaskId()),
		zap.String("run_id", msg.GetRunId()),
		zap.Int32("progress_pct", msg.GetProgressPercent()),
	)

	if r.taskManager == nil {
		return nil
	}

	return r.taskManager.OnTaskProgress(ctx, msg.GetRunId(), int(msg.GetProgressPercent()))
}

// handleTaskCompleted processes a task completed message.
// Updates task/run status and sets runner back to idle.
func (r *MessageRouter) handleTaskCompleted(ctx context.Context, runnerID string, msg *pb.TaskCompleted) error {
	r.logger.Debug("task completed",
		zap.String("runner_id", runnerID),
		zap.String("task_id", msg.GetTaskId()),
		zap.String("run_id", msg.GetRunId()),
		zap.Bool("success", msg.GetSuccess()),
	)

	if r.taskManager == nil {
		r.logger.Warn("no task manager configured, skipping task completed handling")
		return nil
	}

	// Build result
	result := &core.TaskCompletedResult{
		RunID:        msg.GetRunId(),
		Success:      msg.GetSuccess(),
		Error:        msg.GetError(),
		TokensInput:  int(msg.GetTokensInput()),
		TokensOutput: int(msg.GetTokensOutput()),
	}

	if msg.GetExitCode() != 0 {
		exitCode := int(msg.GetExitCode())
		result.ExitCode = &exitCode
	}

	// Update task/run status
	if err := r.taskManager.OnTaskCompleted(ctx, result); err != nil {
		return err
	}

	// Set runner back to idle
	if r.runnerManager != nil {
		return r.runnerManager.SetStatus(ctx, runnerID, "idle")
	}
	return nil
}

// handlePermissionRequest processes a permission request from runner.
// Creates a PermissionRequest in DB and blocks runner until approval.
func (r *MessageRouter) handlePermissionRequest(ctx context.Context, runnerID string, msg *pb.PermissionRequest) error {
	r.logger.Info("permission request received",
		zap.String("runner_id", runnerID),
		zap.String("request_id", msg.GetRequestId()),
		zap.String("task_id", msg.GetTaskId()),
		zap.String("run_id", msg.GetRunId()),
		zap.String("tool", msg.GetTool()),
		zap.String("action", msg.GetAction()),
		zap.String("risk_level", msg.GetRiskLevel()),
	)

	if r.permissionManager == nil {
		r.logger.Warn("no permission manager configured, skipping permission request handling")
		return nil
	}

	if r.store == nil {
		r.logger.Warn("no store configured, skipping permission request handling")
		return nil
	}

	// Get session for this runner to verify the request is valid
	session, err := r.getSessionForRunner(ctx, runnerID)
	if err != nil {
		r.logger.Error("failed to get session for runner",
			zap.String("runner_id", runnerID),
			zap.Error(err),
		)
		return err
	}

	// Use task_id and run_id from the message directly
	// The agent already has this information from the ExecuteTask command
	taskID := msg.GetTaskId()
	runID := msg.GetRunId()

	if taskID == "" || runID == "" {
		r.logger.Error("permission request missing task_id or run_id",
			zap.String("task_id", taskID),
			zap.String("run_id", runID),
		)
		return fmt.Errorf("permission request missing task_id or run_id")
	}

	// Verify the task belongs to this session
	task, err := r.store.GetTask(ctx, taskID)
	if err != nil {
		r.logger.Error("failed to get task for permission request",
			zap.String("task_id", taskID),
			zap.Error(err),
		)
		return err
	}

	if task.SessionID != session.ID {
		r.logger.Error("task does not belong to session",
			zap.String("task_id", taskID),
			zap.String("task_session_id", task.SessionID),
			zap.String("runner_session_id", session.ID),
		)
		return fmt.Errorf("task %s does not belong to session %s", taskID, session.ID)
	}

	// Create permission request
	suspendAfter := int(msg.GetSuspendAfterSeconds())
	if suspendAfter == 0 {
		suspendAfter = core.DefaultSuspendAfterSeconds
	}

	_, err = r.permissionManager.Create(ctx, &core.CreatePermissionRequestInput{
		OriginalRequestID:   msg.GetRequestId(),
		SessionID:           session.ID,
		TaskID:              taskID,
		RunID:               runID,
		Tool:                msg.GetTool(),
		Action:              msg.GetAction(),
		Context:             msg.GetContext(),
		RiskLevel:           msg.GetRiskLevel(),
		SuspendAfterSeconds: suspendAfter,
		TenantID:            session.TenantID,
	})
	if err != nil {
		r.logger.Error("failed to create permission request",
			zap.String("runner_id", runnerID),
			zap.String("session_id", session.ID),
			zap.Error(err),
		)
		return err
	}

	return nil
}

// getSessionForRunner finds the active session attached to a runner.
func (r *MessageRouter) getSessionForRunner(ctx context.Context, runnerID string) (*store.Session, error) {
	sessions, err := r.store.ListSessions(ctx, store.ListSessionsOptions{
		RunnerID: &runnerID,
		Status:   []string{"active"},
	})
	if err != nil {
		return nil, err
	}

	if len(sessions.Items) == 0 {
		return nil, fmt.Errorf("no active session found for runner %s", runnerID)
	}

	// Return the first active session (there should only be one)
	return sessions.Items[0], nil
}

// handleSessionAttached processes a session attached confirmation.
// G3: Will update session status and notify subscribers.
func (r *MessageRouter) handleSessionAttached(_ context.Context, runnerID string, msg *pb.SessionAttached) error {
	r.logger.Debug("session attached (stub)",
		zap.String("runner_id", runnerID),
		zap.String("session_id", msg.GetSessionId()),
	)
	// G3: Implement session attached handling
	return nil
}

// handleSessionSuspended processes a session suspended confirmation.
// G3: Will update session status and trigger suspend strategy.
func (r *MessageRouter) handleSessionSuspended(_ context.Context, runnerID string, msg *pb.SessionSuspended) error {
	r.logger.Debug("session suspended (stub)",
		zap.String("runner_id", runnerID),
		zap.String("session_id", msg.GetSessionId()),
	)
	// G3: Implement session suspended handling
	return nil
}

// handleContextUpdate processes a context update from the runner.
// This is used to save context (like Claude Code's conversation_id)
// for session resume.
func (r *MessageRouter) handleContextUpdate(ctx context.Context, runnerID string, msg *pb.ContextUpdate) error {
	r.logger.Info("received context update",
		zap.String("runner_id", runnerID),
		zap.String("session_id", msg.GetSessionId()),
		zap.String("task_id", msg.GetTaskId()),
		zap.Int("context_size", len(msg.GetContextSnapshot())),
	)

	if r.sessionManager == nil {
		r.logger.Warn("session manager not set, cannot save context update")
		return nil
	}

	// Parse context snapshot to create ContextSnapshot object
	var snapshotData map[string]interface{}
	if err := json.Unmarshal(msg.GetContextSnapshot(), &snapshotData); err != nil {
		r.logger.Warn("failed to parse context snapshot",
			zap.Error(err),
		)
		return nil
	}

	// Create context snapshot with conversation_id
	snapshot := core.NewContextSnapshot()
	if convID, ok := snapshotData["conversation_id"].(string); ok {
		snapshot.ConversationID = convID
	}

	// Save to session
	if err := r.sessionManager.UpdateContextSnapshot(ctx, msg.GetSessionId(), snapshot); err != nil {
		r.logger.Warn("failed to update session context snapshot",
			zap.String("session_id", msg.GetSessionId()),
			zap.Error(err),
		)
		return nil
	}

	r.logger.Info("context snapshot saved",
		zap.String("session_id", msg.GetSessionId()),
		zap.String("conversation_id", snapshot.ConversationID),
	)

	return nil
}

// handleDesktopStreamStarted processes a desktop stream started message.
func (r *MessageRouter) handleDesktopStreamStarted(ctx context.Context, runnerID string, msg *pb.DesktopStreamStarted) error {
	r.logger.Info("desktop stream started",
		zap.String("runner_id", runnerID),
		zap.String("stream_id", msg.GetStreamId()),
		zap.String("session_id", msg.GetSessionId()),
		zap.String("provider", msg.GetProvider()),
	)

	if r.streamManager == nil {
		r.logger.Warn("no stream manager configured, skipping desktop stream started handling")
		return nil
	}

	// Convert proto config to core config
	var config *core.StreamConfig
	if msg.GetActualConfig() != nil {
		cfg := msg.GetActualConfig()
		config = &core.StreamConfig{
			Width:        int(cfg.GetWidth()),
			Height:       int(cfg.GetHeight()),
			FrameRate:    int(cfg.GetFrameRate()),
			Bitrate:      int(cfg.GetBitrate()),
			VideoCodec:   cfg.GetVideoCodec(),
			AudioEnabled: cfg.GetAudioEnabled(),
			InputEnabled: cfg.GetInputEnabled(),
			Display:      cfg.GetDisplay(),
			HWAccel:      cfg.GetHwAccel(),
		}
	}

	startedAt := timeFromUnixMS(msg.GetStartedAtUnixMs())

	return r.streamManager.OnStreamStarted(ctx, &core.StreamStartedMessage{
		StreamID:        msg.GetStreamId(),
		SessionID:       msg.GetSessionId(),
		ActualConfig:    config,
		SignalingURL:    msg.GetSignalingUrl(),
		Provider:        msg.GetProvider(),
		ProviderVersion: msg.GetProviderVersion(),
		StartedAt:       startedAt,
	})
}

// handleDesktopStreamStopped processes a desktop stream stopped message.
func (r *MessageRouter) handleDesktopStreamStopped(ctx context.Context, runnerID string, msg *pb.DesktopStreamStopped) error {
	r.logger.Info("desktop stream stopped",
		zap.String("runner_id", runnerID),
		zap.String("stream_id", msg.GetStreamId()),
		zap.String("session_id", msg.GetSessionId()),
		zap.String("reason", msg.GetReason()),
	)

	if r.streamManager == nil {
		r.logger.Warn("no stream manager configured, skipping desktop stream stopped handling")
		return nil
	}

	stoppedAt := timeFromUnixMS(msg.GetStoppedAtUnixMs())

	return r.streamManager.OnStreamStopped(ctx, &core.StreamStoppedMessage{
		StreamID:  msg.GetStreamId(),
		SessionID: msg.GetSessionId(),
		Reason:    msg.GetReason(),
		StoppedAt: stoppedAt,
	})
}

// handleDesktopStreamError processes a desktop stream error message.
func (r *MessageRouter) handleDesktopStreamError(ctx context.Context, runnerID string, msg *pb.DesktopStreamError) error {
	r.logger.Error("desktop stream error",
		zap.String("runner_id", runnerID),
		zap.String("stream_id", msg.GetStreamId()),
		zap.String("session_id", msg.GetSessionId()),
		zap.String("error", msg.GetError()),
		zap.String("error_code", msg.GetErrorCode()),
		zap.Bool("recoverable", msg.GetRecoverable()),
	)

	if r.streamManager == nil {
		r.logger.Warn("no stream manager configured, skipping desktop stream error handling")
		return nil
	}

	timestamp := timeFromUnixMS(msg.GetTimestampUnixMs())

	return r.streamManager.OnStreamError(ctx, &core.StreamErrorMessage{
		StreamID:    msg.GetStreamId(),
		SessionID:   msg.GetSessionId(),
		Error:       msg.GetError(),
		ErrorCode:   msg.GetErrorCode(),
		Recoverable: msg.GetRecoverable(),
		Timestamp:   timestamp,
	})
}

// timeFromUnixMS converts Unix milliseconds to time.Time.
func timeFromUnixMS(ms int64) time.Time {
	if ms == 0 {
		return time.Now()
	}
	return time.UnixMilli(ms)
}
