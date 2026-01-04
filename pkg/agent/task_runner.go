package agent

import (
	"context"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	pb "github.com/chunlea/marionette/gen/proto/v1"
	"github.com/chunlea/marionette/pkg/agent/executor"
	"github.com/chunlea/marionette/pkg/agent/executor/permission"
	"go.uber.org/zap"
)

// MessageSender is an interface for sending messages to the server.
type MessageSender interface {
	Send(msg *pb.RunnerMessage)
}

// TaskRunner executes tasks using an Executor and handles permission requests.
// It implements executor.OutputHandler to bridge the executor with the gRPC stream.
type TaskRunner struct {
	executor     executor.Executor
	sender       MessageSender
	workspaceMgr *WorkspaceManager
	logger       *zap.Logger

	// Permission response handling
	mu            sync.Mutex
	permResponses map[string]chan *pb.ApprovePermission

	// Current task context
	currentTask  *pb.ExecuteTask
	currentRunID string
	logSequence  atomic.Int64
}

// NewTaskRunner creates a new TaskRunner.
func NewTaskRunner(
	exec executor.Executor,
	sender MessageSender,
	workspaceMgr *WorkspaceManager,
	logger *zap.Logger,
) *TaskRunner {
	return &TaskRunner{
		executor:      exec,
		sender:        sender,
		workspaceMgr:  workspaceMgr,
		logger:        logger.Named("task-runner"),
		permResponses: make(map[string]chan *pb.ApprovePermission),
	}
}

// Execute runs a task and returns the appropriate RunnerMessage.
// This is called by CommandHandler.OnExecuteTask.
func (r *TaskRunner) Execute(ctx context.Context, cmd *pb.ExecuteTask) (*pb.RunnerMessage, error) {
	r.logger.Info("executing task",
		zap.String("task_id", cmd.TaskId),
		zap.String("run_id", cmd.RunId),
		zap.String("session_id", cmd.SessionId),
		zap.Int32("attempt", cmd.Attempt),
	)

	// Store current task context
	r.mu.Lock()
	r.currentTask = cmd
	r.currentRunID = cmd.RunId
	r.logSequence.Store(0)
	r.mu.Unlock()

	defer func() {
		r.mu.Lock()
		r.currentTask = nil
		r.currentRunID = ""
		r.mu.Unlock()
	}()

	// Send TaskAccepted
	r.sender.Send(&pb.RunnerMessage{
		Payload: &pb.RunnerMessage_TaskAccepted{
			TaskAccepted: &pb.TaskAccepted{
				TaskId:  cmd.TaskId,
				RunId:   cmd.RunId,
				Attempt: cmd.Attempt,
			},
		},
	})

	// Determine working directory
	workDir := cmd.WorkDir
	if workDir == "" {
		workDir = r.workspaceMgr.BaseDir()
	} else {
		workDir = r.workspaceMgr.ResolvePath(workDir)
	}

	// Ensure workspace directory exists
	if err := r.workspaceMgr.EnsureExists(workDir); err != nil {
		r.logger.Error("failed to ensure workspace exists", zap.Error(err))
		return r.taskCompleted(cmd, false, "workspace error: "+err.Error(), -1), nil
	}

	// Resolve symlinks to get the real path (e.g., /tmp -> /private/tmp on macOS)
	// This ensures --add-dir matches the path Claude Code sees
	if realPath, err := filepath.EvalSymlinks(workDir); err == nil {
		workDir = realPath
	}

	// Send TaskStarted
	r.sender.Send(&pb.RunnerMessage{
		Payload: &pb.RunnerMessage_TaskStarted{
			TaskStarted: &pb.TaskStarted{
				TaskId:  cmd.TaskId,
				RunId:   cmd.RunId,
				Attempt: cmd.Attempt,
			},
		},
	})

	// Determine timeout from sandbox config
	var timeout time.Duration
	if cmd.Sandbox != nil && cmd.Sandbox.TimeoutSeconds > 0 {
		timeout = time.Duration(cmd.Sandbox.TimeoutSeconds) * time.Second
	}

	// Build executor task
	task := &executor.Task{
		ID:         cmd.TaskId,
		RunID:      cmd.RunId,
		SessionID:  cmd.SessionId,
		Attempt:    cmd.Attempt,
		Prompt:     cmd.Prompt,
		Timeout:    timeout,
		WorkingDir: workDir,
	}

	// Build agent config from cmd.AgentConfig
	var agentConfig *executor.AgentConfig
	if cmd.AgentConfig != nil {
		agentConfig = &executor.AgentConfig{
			Agent:      cmd.AgentConfig.Agent,
			Model:      cmd.AgentConfig.Model,
			APIKey:     cmd.AgentConfig.ApiKey,
			BaseURL:    cmd.AgentConfig.BaseUrl,
			WorkingDir: workDir,
		}
		// Copy extra fields
		if len(cmd.AgentConfig.Extra) > 0 {
			agentConfig.Extra = make(map[string]string)
			for k, v := range cmd.AgentConfig.Extra {
				agentConfig.Extra[k] = v
			}
		}
	}

	// Create permission detector wrapper
	detector := permission.NewDetector(r, r.logger)

	// Execute the task
	result, err := r.executor.Execute(ctx, task, agentConfig, detector)
	if err != nil {
		r.logger.Error("executor error", zap.Error(err))
		return r.taskCompleted(cmd, false, err.Error(), -1), nil
	}

	// Build context snapshot for session state
	var contextSnapshot []byte
	if result.ContextSnapshot != nil {
		contextSnapshot = result.ContextSnapshot
	}

	// Send TaskCompleted
	completed := &pb.TaskCompleted{
		TaskId:          cmd.TaskId,
		RunId:           cmd.RunId,
		Attempt:         cmd.Attempt,
		Success:         result.Success,
		Error:           result.Error,
		ExitCode:        int32(result.ExitCode),
		TokensInput:     result.TokensInput,
		TokensOutput:    result.TokensOutput,
		ContextSnapshot: contextSnapshot,
	}

	return &pb.RunnerMessage{
		Payload: &pb.RunnerMessage_TaskCompleted{
			TaskCompleted: completed,
		},
	}, nil
}

// HandleOutput implements executor.OutputHandler.
// It sends log output to the server via the control channel.
func (r *TaskRunner) HandleOutput(stream string, data []byte) {
	r.mu.Lock()
	task := r.currentTask
	currentRunID := r.currentRunID
	r.mu.Unlock()

	if task == nil {
		r.logger.Debug("output received but no current task",
			zap.String("stream", stream),
			zap.Int("bytes", len(data)),
		)
		return
	}

	// Log locally for debugging
	r.logger.Debug("task output",
		zap.String("task_id", task.TaskId),
		zap.String("run_id", currentRunID),
		zap.String("stream", stream),
		zap.String("content", string(data)),
	)

	// Send TaskProgress for now (logs will go through StreamLogs in a future phase)
	// For MVP, we'll use TaskProgress with current_step as the log content
	seq := r.logSequence.Add(1)
	_ = seq // Will be used when we implement StreamLogs

	// Note: In a full implementation, logs would be sent via StreamLogs RPC
	// For now, we just log them locally
}

// HandlePermissionRequest implements executor.OutputHandler.
// It sends a permission request to the server and blocks until a response is received.
func (r *TaskRunner) HandlePermissionRequest(ctx context.Context, req *executor.PermissionRequest) (bool, error) {
	r.mu.Lock()
	task := r.currentTask
	currentRunID := r.currentRunID
	r.mu.Unlock()

	if task == nil {
		return false, nil
	}

	r.logger.Info("permission request",
		zap.String("request_id", req.ID),
		zap.String("task_id", task.TaskId),
		zap.String("run_id", currentRunID),
		zap.String("tool", req.Tool),
		zap.String("action", req.Action),
		zap.String("risk_level", req.RiskLevel.String()),
	)

	// Create response channel
	respChan := make(chan *pb.ApprovePermission, 1)
	r.mu.Lock()
	r.permResponses[req.ID] = respChan
	r.mu.Unlock()

	defer func() {
		r.mu.Lock()
		delete(r.permResponses, req.ID)
		r.mu.Unlock()
	}()

	// Send permission request to server
	r.sender.Send(&pb.RunnerMessage{
		Payload: &pb.RunnerMessage_PermissionRequest{
			PermissionRequest: &pb.PermissionRequest{
				RequestId: req.ID,
				TaskId:    task.TaskId,
				RunId:     currentRunID,
				Tool:      req.Tool,
				Action:    req.Action,
				RiskLevel: req.RiskLevel.String(),
			},
		},
	})

	// Wait for response
	select {
	case <-ctx.Done():
		r.logger.Info("permission request cancelled",
			zap.String("request_id", req.ID),
		)
		return false, ctx.Err()
	case resp := <-respChan:
		r.logger.Info("permission response received",
			zap.String("request_id", req.ID),
			zap.Bool("approved", resp.Approved),
			zap.String("reason", resp.Reason),
		)
		return resp.Approved, nil
	}
}

// HandlePermissionResponse is called when a permission response is received from the server.
// This is wired to CommandHandler.OnApprovePermission.
func (r *TaskRunner) HandlePermissionResponse(ctx context.Context, cmd *pb.ApprovePermission) error {
	r.logger.Info("received permission response",
		zap.String("request_id", cmd.RequestId),
		zap.Bool("approved", cmd.Approved),
		zap.String("reason", cmd.Reason),
		zap.Bool("from_cache", cmd.FromCache),
	)

	r.mu.Lock()
	respChan, ok := r.permResponses[cmd.RequestId]
	r.mu.Unlock()

	if !ok {
		r.logger.Warn("no pending permission request for response",
			zap.String("request_id", cmd.RequestId),
		)
		return nil
	}

	// Send response to waiting goroutine
	select {
	case respChan <- cmd:
	default:
		r.logger.Warn("permission response channel full",
			zap.String("request_id", cmd.RequestId),
		)
	}

	return nil
}

// taskCompleted is a helper to create a TaskCompleted RunnerMessage.
func (r *TaskRunner) taskCompleted(cmd *pb.ExecuteTask, success bool, errMsg string, exitCode int) *pb.RunnerMessage {
	return &pb.RunnerMessage{
		Payload: &pb.RunnerMessage_TaskCompleted{
			TaskCompleted: &pb.TaskCompleted{
				TaskId:   cmd.TaskId,
				RunId:    cmd.RunId,
				Attempt:  cmd.Attempt,
				Success:  success,
				Error:    errMsg,
				ExitCode: int32(exitCode),
			},
		},
	}
}

// Ensure TaskRunner implements executor.OutputHandler.
var _ executor.OutputHandler = (*TaskRunner)(nil)
