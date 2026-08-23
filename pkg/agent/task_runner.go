package agent

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"sync"
	"time"

	pb "github.com/chunlea/marionette/gen/proto/v1"
	"github.com/chunlea/marionette/pkg/agent/executor"
	"github.com/chunlea/marionette/pkg/agent/executor/claude"
	"github.com/chunlea/marionette/pkg/id"
	"go.uber.org/zap"
)

// defaultTaskTimeout applies when the server sends no timeout for a task.
const defaultTaskTimeout = 1 * time.Hour

// MessageSender is an interface for sending messages to the server.
type MessageSender interface {
	Send(msg *pb.RunnerMessage)
}

// StatusSetter is an interface for setting runner status.
type StatusSetter interface {
	SetStatus(status string)
}

// TaskRunner executes tasks using the Claude executor and handles
// output/permission communication with the server.
type TaskRunner struct {
	sender       MessageSender
	executor     executor.Executor
	workspaceMgr *WorkspaceManager
	cmdHandler   *DefaultCommandHandler
	statusSetter StatusSetter
	logStreamer  LogStreamer
	logger       *zap.Logger

	// Permission response channels (per request)
	mu            sync.Mutex
	permResponses map[string]chan *pb.ApprovePermission

	// Permission cache for resume scenarios.
	// When a session resumes with pending permission responses, we cache them here.
	// When a task requests a permission, we check this cache first.
	// Key format: "tool:action" (e.g., "bash:rm -rf /tmp/foo")
	permCache map[string]*pb.ApprovePermission

	// Current task state
	currentTask *pb.ExecuteTask
	currentCtx  context.Context

	// Flag to indicate the task was canceled due to session detach.
	// When this is true, we don't send TaskCompleted message because
	// the task will be re-executed after session resume.
	canceledForDetach bool
}

// NewTaskRunner creates a new TaskRunner.
func NewTaskRunner(
	sender MessageSender,
	exec executor.Executor,
	wsMgr *WorkspaceManager,
	cmdHandler *DefaultCommandHandler,
	statusSetter StatusSetter,
	logStreamer LogStreamer,
	logger *zap.Logger,
) *TaskRunner {
	return &TaskRunner{
		sender:        sender,
		executor:      exec,
		workspaceMgr:  wsMgr,
		cmdHandler:    cmdHandler,
		statusSetter:  statusSetter,
		logStreamer:   logStreamer,
		logger:        logger.Named("task-runner"),
		permResponses: make(map[string]chan *pb.ApprovePermission),
		permCache:     make(map[string]*pb.ApprovePermission),
	}
}

// Execute runs a task and returns the result message.
// This implements the OnExecuteTask callback signature.
func (r *TaskRunner) Execute(ctx context.Context, cmd *pb.ExecuteTask) (*pb.RunnerMessage, error) {
	r.logger.Info("executing task",
		zap.String("task_id", cmd.TaskId),
		zap.String("run_id", cmd.RunId),
		zap.String("session_id", cmd.SessionId),
	)

	// Track whether we actually started executing (for proper status cleanup)
	executorStarted := false

	r.mu.Lock()
	r.currentTask = cmd
	r.currentCtx = ctx
	r.mu.Unlock()

	defer func() {
		// Only set status back to idle if we actually started executing
		// If the executor was already running (from another task), we shouldn't
		// change the status - the other task will reset it when done
		if executorStarted && r.statusSetter != nil {
			r.statusSetter.SetStatus("idle")
		}
		r.mu.Lock()
		r.currentTask = nil
		r.currentCtx = nil
		r.mu.Unlock()
	}()

	// Get session state
	session, exists := r.cmdHandler.GetSession(cmd.SessionId)
	if !exists {
		r.logger.Error("session not attached", zap.String("session_id", cmd.SessionId))
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

	// Send task accepted first
	acceptedMsg := &pb.RunnerMessage{
		Payload: &pb.RunnerMessage_TaskAccepted{
			TaskAccepted: &pb.TaskAccepted{
				TaskId:  cmd.TaskId,
				RunId:   cmd.RunId,
				Attempt: cmd.Attempt,
			},
		},
	}
	r.sender.Send(acceptedMsg)

	// Start log streaming if available
	if r.logStreamer != nil {
		init := &pb.StreamLogsInit{
			SessionId: cmd.SessionId,
			TaskId:    cmd.TaskId,
			RunId:     cmd.RunId,
		}
		if err := r.logStreamer.Start(ctx, init); err != nil {
			r.logger.Warn("failed to start log stream", zap.Error(err))
			// Continue without log streaming - not a fatal error
		} else {
			defer func() {
				if resp, err := r.logStreamer.Close(); err != nil {
					r.logger.Warn("failed to close log stream", zap.Error(err))
				} else {
					r.logger.Debug("log stream closed",
						zap.Int64("logs_received", resp.LogsReceived),
						zap.Int64("logs_stored", resp.LogsStored),
					)
				}
			}()
		}
	}

	// Get workspace path - resolve relative path to absolute
	workspacePath := filepath.Join(r.workspaceMgr.BaseDir(), session.WorkspacePath)

	// Honor the server-supplied timeout. It arrives on the sandbox config;
	// hardcoding an hour here meant a task the server expected to cap at, say,
	// five minutes ran twelve times longer than it was allowed to.
	timeout := defaultTaskTimeout
	if cmd.Sandbox != nil && cmd.Sandbox.TimeoutSeconds > 0 {
		timeout = time.Duration(cmd.Sandbox.TimeoutSeconds) * time.Second
	} else {
		r.logger.Debug("no server-supplied timeout, using default",
			zap.String("task_id", cmd.TaskId),
			zap.Duration("timeout", timeout),
		)
	}

	// Build executor task
	task := &executor.Task{
		ID:              cmd.TaskId,
		RunID:           cmd.RunId,
		SessionID:       cmd.SessionId,
		Attempt:         cmd.Attempt,
		Prompt:          cmd.Prompt,
		Timeout:         timeout,
		WorkingDir:      workspacePath,
		ContextSnapshot: session.ContextSnapshot,
	}

	r.logger.Debug("executor task built",
		zap.String("task_id", task.ID),
		zap.String("prompt", task.Prompt),
		zap.Duration("timeout", task.Timeout),
		zap.Int("context_snapshot_len", len(session.ContextSnapshot)),
		zap.ByteString("context_snapshot", session.ContextSnapshot),
	)

	// Build agent config from session
	var agentConfig *executor.AgentConfig
	if session.AgentConfig != nil {
		agentConfig = &executor.AgentConfig{
			Agent:      session.AgentConfig.Agent,
			Model:      session.AgentConfig.Model,
			APIKey:     session.AgentConfig.ApiKey,
			BaseURL:    session.AgentConfig.BaseUrl,
			WorkingDir: workspacePath,
		}
	}

	// Set status to busy before executing
	// We'll track if execution actually started to properly handle cleanup
	if r.statusSetter != nil {
		r.statusSetter.SetStatus("busy")
	}
	executorStarted = true

	// Execute the task
	result, err := r.executor.Execute(ctx, task, agentConfig, r)

	// Check if task was canceled for session detach.
	// If so, don't send TaskCompleted - the task will be re-executed after resume.
	r.mu.Lock()
	canceledForDetach := r.canceledForDetach
	r.canceledForDetach = false // Reset for next task
	r.mu.Unlock()

	if canceledForDetach {
		r.logger.Info("task execution interrupted for session detach, not sending TaskCompleted",
			zap.String("task_id", cmd.TaskId),
		)
		// Return nil to indicate no message should be sent
		return nil, nil
	}

	if err != nil {
		r.logger.Error("executor error", zap.Error(err))
		// If executor was already running, don't reset status - the other task will do it
		if errors.Is(err, claude.ErrAlreadyRunning) {
			executorStarted = false
		}
		return &pb.RunnerMessage{
			Payload: &pb.RunnerMessage_TaskCompleted{
				TaskCompleted: &pb.TaskCompleted{
					TaskId:  cmd.TaskId,
					RunId:   cmd.RunId,
					Attempt: cmd.Attempt,
					Success: false,
					Error:   err.Error(),
				},
			},
		}, nil
	}

	r.logger.Info("task completed",
		zap.String("task_id", cmd.TaskId),
		zap.Bool("success", result.Success),
		zap.Int("exit_code", result.ExitCode),
		zap.String("error", result.Error),
		zap.Int64("tokens_input", result.TokensInput),
		zap.Int64("tokens_output", result.TokensOutput),
	)

	return &pb.RunnerMessage{
		Payload: &pb.RunnerMessage_TaskCompleted{
			TaskCompleted: &pb.TaskCompleted{
				TaskId:   cmd.TaskId,
				RunId:    cmd.RunId,
				Attempt:  cmd.Attempt,
				Success:  result.Success,
				ExitCode: int32(result.ExitCode),
				// Carries the agent's own reason when it reported a failure
				// (error_max_turns, error_during_execution, is_error), not
				// just an exit code.
				Error:        result.Error,
				TokensInput:  result.TokensInput,
				TokensOutput: result.TokensOutput,
				// The executor derives this from the CLI session id, so the
				// next task in the session can --resume. Previously only the
				// mid-run ContextUpdate carried it, which is lost if the run
				// ends before one is sent.
				ContextSnapshot: result.ContextSnapshot,
			},
		},
	}, nil
}

// HandleOutput implements executor.OutputHandler.
// Sends logs to the server via StreamLogs RPC if available.
func (r *TaskRunner) HandleOutput(stream string, data []byte) {
	r.mu.Lock()
	task := r.currentTask
	r.mu.Unlock()

	if task == nil {
		return
	}

	// Log at debug level locally
	r.logger.Debug("executor output",
		zap.String("task_id", task.TaskId),
		zap.String("stream", stream),
		zap.ByteString("data", data),
	)

	// Send to server if log streaming is active
	if r.logStreamer != nil && r.logStreamer.IsActive() {
		entry := &pb.LogEntry{
			TaskId:    task.TaskId,
			RunId:     task.RunId,
			SessionId: task.SessionId,
			Stream:    stream,
			Level:     streamToLevel(stream),
			Content:   string(data),
		}
		if err := r.logStreamer.Send(entry); err != nil {
			r.logger.Warn("failed to send log entry",
				zap.Error(err),
				zap.String("stream", stream),
			)
		}
	}
}

// streamToLevel converts a stream name to a log level.
func streamToLevel(stream string) string {
	switch stream {
	case "stderr":
		return "error"
	case "system":
		return "info"
	default:
		return "info"
	}
}

// HandleContextUpdate implements executor.OutputHandler.
// It sends context updates (like Claude Code's session_id) to the server
// so they can be saved for session resume.
func (r *TaskRunner) HandleContextUpdate(ctx context.Context, sessionID string, conversationID string) {
	r.mu.Lock()
	task := r.currentTask
	r.mu.Unlock()

	if task == nil {
		r.logger.Warn("context update with no active task",
			zap.String("session_id", sessionID),
			zap.String("conversation_id", conversationID),
		)
		return
	}

	r.logger.Info("sending context update to server",
		zap.String("session_id", sessionID),
		zap.String("task_id", task.TaskId),
		zap.String("conversation_id", conversationID),
	)

	// Build context snapshot JSON with conversation_id
	contextSnapshot := map[string]string{
		"conversation_id": conversationID,
	}
	snapshotJSON, err := json.Marshal(contextSnapshot)
	if err != nil {
		r.logger.Warn("failed to marshal context snapshot",
			zap.Error(err),
		)
		return
	}

	// Send ContextUpdate to server
	msg := &pb.RunnerMessage{
		Payload: &pb.RunnerMessage_ContextUpdate{
			ContextUpdate: &pb.ContextUpdate{
				SessionId:       sessionID,
				TaskId:          task.TaskId,
				ContextSnapshot: snapshotJSON,
			},
		},
	}
	r.sender.Send(msg)
}

// HandlePermissionRequest implements executor.OutputHandler.
// It sends permission requests to the server and waits for response.
func (r *TaskRunner) HandlePermissionRequest(ctx context.Context, req *executor.PermissionRequest) (bool, error) {
	r.mu.Lock()
	task := r.currentTask
	r.mu.Unlock()

	if task == nil {
		return false, ErrNoActiveTask
	}

	// Check permission cache first (for resume scenarios)
	// Try primary key (tool:action) first, then fallback to secondary key (task_id:tool)
	primaryKey := req.Tool + ":" + req.Action
	secondaryKey := task.TaskId + ":" + req.Tool

	r.mu.Lock()
	cachedResp, hasCached := r.permCache[primaryKey]
	usedKey := primaryKey
	if hasCached {
		// Remove from cache after use (one-time use)
		delete(r.permCache, primaryKey)
		// Also remove secondary key if it exists
		delete(r.permCache, secondaryKey)
	} else {
		// Try secondary key (task_id:tool) - more lenient matching
		// This handles the case where Claude re-generates a slightly different action string
		cachedResp, hasCached = r.permCache[secondaryKey]
		usedKey = secondaryKey
		if hasCached {
			delete(r.permCache, secondaryKey)
		}
	}
	r.mu.Unlock()

	if hasCached {
		r.logger.Info("using cached permission response",
			zap.String("tool", req.Tool),
			zap.String("cache_key", usedKey),
			zap.Bool("approved", cachedResp.Approved),
		)
		return cachedResp.Approved, nil
	}

	// Generate request ID if not set
	requestID := req.ID
	if requestID == "" {
		requestID = id.PermissionRequest()
	}

	// Create response channel
	respChan := make(chan *pb.ApprovePermission, 1)
	r.mu.Lock()
	r.permResponses[requestID] = respChan
	r.mu.Unlock()

	defer func() {
		r.mu.Lock()
		delete(r.permResponses, requestID)
		r.mu.Unlock()
	}()

	// Map risk level to string
	riskLevel := req.RiskLevel.String()

	// Send permission request to server
	permMsg := &pb.RunnerMessage{
		Payload: &pb.RunnerMessage_PermissionRequest{
			PermissionRequest: &pb.PermissionRequest{
				RequestId: requestID,
				TaskId:    task.TaskId,
				RunId:     task.RunId,
				Tool:      req.Tool,
				Action:    req.Action,
				Context:   req.Context,
				RiskLevel: riskLevel,
			},
		},
	}
	r.sender.Send(permMsg)

	r.logger.Info("permission request sent, waiting for response",
		zap.String("request_id", requestID),
		zap.String("tool", req.Tool),
	)

	// Wait for response
	select {
	case resp := <-respChan:
		r.logger.Info("permission response received",
			zap.String("request_id", requestID),
			zap.Bool("approved", resp.Approved),
		)
		return resp.Approved, nil
	case <-ctx.Done():
		r.logger.Warn("permission request cancelled", zap.String("request_id", requestID))
		return false, ctx.Err()
	}
}

// CancelTask cancels the current running task for the given session.
// This is called when a session is detached to stop any running task.
func (r *TaskRunner) CancelTask(sessionID string) error {
	r.mu.Lock()
	task := r.currentTask
	r.mu.Unlock()

	if task == nil {
		r.logger.Debug("no task to cancel", zap.String("session_id", sessionID))
		return nil
	}

	// Only cancel if the task belongs to the detached session
	if task.SessionId != sessionID {
		r.logger.Debug("task belongs to different session, not canceling",
			zap.String("task_session_id", task.SessionId),
			zap.String("detached_session_id", sessionID),
		)
		return nil
	}

	r.logger.Info("canceling task due to session detach",
		zap.String("task_id", task.TaskId),
		zap.String("session_id", sessionID),
	)

	// Set flag to prevent sending TaskCompleted message.
	// The task will be re-executed after session resume.
	r.mu.Lock()
	r.canceledForDetach = true
	r.mu.Unlock()

	// Kill the executor to stop the task
	if killer, ok := r.executor.(interface{ Kill() error }); ok {
		if err := killer.Kill(); err != nil {
			r.logger.Warn("failed to kill executor", zap.Error(err))
		}
	}

	return nil
}

// HandlePermissionResponse handles permission approval/denial from the server.
// This is called by the CommandHandler's OnApprovePermission callback.
func (r *TaskRunner) HandlePermissionResponse(ctx context.Context, cmd *pb.ApprovePermission) error {
	r.mu.Lock()
	respChan, exists := r.permResponses[cmd.RequestId]

	if !exists {
		// No pending request - this happens after suspend/resume.
		// Cache the response so it can be used when the task is re-run.
		// Use both primary (tool:action) and secondary (task_id:tool) cache keys.
		// Secondary key is needed because when task re-executes from scratch,
		// Claude may generate a slightly different action string.
		if cmd.Tool != "" {
			// Secondary cache key: task_id:tool (more lenient matching)
			if cmd.TaskId != "" {
				secondaryKey := cmd.TaskId + ":" + cmd.Tool
				r.permCache[secondaryKey] = cmd
				r.logger.Info("cached permission response with secondary key",
					zap.String("request_id", cmd.RequestId),
					zap.String("secondary_key", secondaryKey),
					zap.Bool("approved", cmd.Approved),
				)
			}

			// Primary cache key: tool:action (exact matching)
			if cmd.Action != "" {
				primaryKey := cmd.Tool + ":" + cmd.Action
				r.permCache[primaryKey] = cmd
				r.logger.Info("cached permission response with primary key",
					zap.String("request_id", cmd.RequestId),
					zap.String("primary_key", primaryKey),
					zap.Bool("approved", cmd.Approved),
				)
			}

			r.mu.Unlock()
			return nil
		}
		r.mu.Unlock()
		r.logger.Warn("permission response for unknown request (no tool to cache)",
			zap.String("request_id", cmd.RequestId),
		)
		return nil
	}
	r.mu.Unlock()

	select {
	case respChan <- cmd:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	default:
		// Channel full or already handled
		return nil
	}
}

// Verify TaskRunner implements executor.OutputHandler.
var _ executor.OutputHandler = (*TaskRunner)(nil)
