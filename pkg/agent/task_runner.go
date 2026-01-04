package agent

import (
	"context"
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

	// Current task state
	currentTask *pb.ExecuteTask
	currentCtx  context.Context
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

	// Default timeout if not specified
	timeout := 1 * time.Hour

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
	)

	return &pb.RunnerMessage{
		Payload: &pb.RunnerMessage_TaskCompleted{
			TaskCompleted: &pb.TaskCompleted{
				TaskId:       cmd.TaskId,
				RunId:        cmd.RunId,
				Attempt:      cmd.Attempt,
				Success:      result.Success,
				ExitCode:     int32(result.ExitCode),
				Error:        result.Error,
				TokensInput:  result.TokensInput,
				TokensOutput: result.TokensOutput,
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

// HandlePermissionRequest implements executor.OutputHandler.
// It sends permission requests to the server and waits for response.
func (r *TaskRunner) HandlePermissionRequest(ctx context.Context, req *executor.PermissionRequest) (bool, error) {
	r.mu.Lock()
	task := r.currentTask
	r.mu.Unlock()

	if task == nil {
		return false, ErrNoActiveTask
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

// HandlePermissionResponse handles permission approval/denial from the server.
// This is called by the CommandHandler's OnApprovePermission callback.
func (r *TaskRunner) HandlePermissionResponse(ctx context.Context, cmd *pb.ApprovePermission) error {
	r.mu.Lock()
	respChan, exists := r.permResponses[cmd.RequestId]
	r.mu.Unlock()

	if !exists {
		r.logger.Warn("permission response for unknown request",
			zap.String("request_id", cmd.RequestId),
		)
		return nil
	}

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
