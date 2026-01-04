package agent

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	pb "github.com/chunlea/marionette/gen/proto/v1"
	"github.com/chunlea/marionette/pkg/agent/executor"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
)

// mockMessageSender captures sent messages for testing.
type mockMessageSender struct {
	mu       sync.Mutex
	messages []*pb.RunnerMessage
}

func (m *mockMessageSender) Send(msg *pb.RunnerMessage) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.messages = append(m.messages, msg)
}

func (m *mockMessageSender) Messages() []*pb.RunnerMessage {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.messages
}

// mockStatusSetter tracks status changes.
type mockStatusSetter struct {
	mu       sync.Mutex
	statuses []string
}

func (m *mockStatusSetter) SetStatus(status string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.statuses = append(m.statuses, status)
}

func (m *mockStatusSetter) Statuses() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.statuses
}

// mockExecutor implements executor.Executor for testing.
type mockExecutor struct {
	executeFunc func(ctx context.Context, task *executor.Task, config *executor.AgentConfig, handler executor.OutputHandler) (*executor.Result, error)
}

func (m *mockExecutor) Name() string {
	return "mock"
}

func (m *mockExecutor) Execute(ctx context.Context, task *executor.Task, config *executor.AgentConfig, handler executor.OutputHandler) (*executor.Result, error) {
	if m.executeFunc != nil {
		return m.executeFunc(ctx, task, config, handler)
	}
	return &executor.Result{Success: true}, nil
}

func (m *mockExecutor) Kill() error {
	return nil
}

func TestNewTaskRunner(t *testing.T) {
	logger := zaptest.NewLogger(t)
	sender := &mockMessageSender{}
	exec := &mockExecutor{}
	wsMgr := NewWorkspaceManager("/tmp", logger)
	cmdHandler := NewDefaultCommandHandler(wsMgr, logger)
	statusSetter := &mockStatusSetter{}

	runner := NewTaskRunner(sender, exec, wsMgr, cmdHandler, statusSetter, nil, logger)

	assert.NotNil(t, runner)
	assert.Equal(t, sender, runner.sender)
	assert.Equal(t, exec, runner.executor)
	assert.Equal(t, wsMgr, runner.workspaceMgr)
	assert.Equal(t, cmdHandler, runner.cmdHandler)
	assert.Equal(t, statusSetter, runner.statusSetter)
	assert.NotNil(t, runner.permResponses)
}

func TestTaskRunner_Execute_SessionNotAttached(t *testing.T) {
	logger := zaptest.NewLogger(t)
	sender := &mockMessageSender{}
	exec := &mockExecutor{}
	wsMgr := NewWorkspaceManager("/tmp", logger)
	cmdHandler := NewDefaultCommandHandler(wsMgr, logger)
	statusSetter := &mockStatusSetter{}

	runner := NewTaskRunner(sender, exec, wsMgr, cmdHandler, statusSetter, nil, logger)

	cmd := &pb.ExecuteTask{
		TaskId:    "task_123",
		RunId:     "trun_456",
		SessionId: "sess_nonexistent",
		Attempt:   1,
		Prompt:    "test prompt",
	}

	result, err := runner.Execute(context.Background(), cmd)

	require.NoError(t, err)
	require.NotNil(t, result)

	completed := result.GetTaskCompleted()
	require.NotNil(t, completed)
	assert.Equal(t, "task_123", completed.TaskId)
	assert.Equal(t, "trun_456", completed.RunId)
	assert.False(t, completed.Success)
	assert.Equal(t, "session not attached", completed.Error)

	// Status should have been set to busy and back to idle
	statuses := statusSetter.Statuses()
	assert.Equal(t, []string{"busy", "idle"}, statuses)
}

func TestTaskRunner_Execute_Success(t *testing.T) {
	logger := zaptest.NewLogger(t)
	sender := &mockMessageSender{}
	exec := &mockExecutor{
		executeFunc: func(ctx context.Context, task *executor.Task, config *executor.AgentConfig, handler executor.OutputHandler) (*executor.Result, error) {
			// Simulate some output
			handler.HandleOutput("stdout", []byte("test output"))
			return &executor.Result{
				Success:      true,
				ExitCode:     0,
				TokensInput:  100,
				TokensOutput: 50,
			}, nil
		},
	}
	wsMgr := NewWorkspaceManager("/tmp", logger)
	cmdHandler := NewDefaultCommandHandler(wsMgr, logger)
	statusSetter := &mockStatusSetter{}

	runner := NewTaskRunner(sender, exec, wsMgr, cmdHandler, statusSetter, nil, logger)

	// Attach session first
	attachCmd := &pb.AttachSession{
		SessionId:     "sess_123",
		WorkspacePath: "ws_test",
		AgentConfig: &pb.AgentConfig{
			Agent:  "claude",
			Model:  "claude-3-5-sonnet",
			ApiKey: "test-key",
		},
	}
	_, err := cmdHandler.HandleAttachSession(context.Background(), attachCmd)
	require.NoError(t, err)

	cmd := &pb.ExecuteTask{
		TaskId:    "task_123",
		RunId:     "trun_456",
		SessionId: "sess_123",
		Attempt:   1,
		Prompt:    "test prompt",
	}

	result, err := runner.Execute(context.Background(), cmd)

	require.NoError(t, err)
	require.NotNil(t, result)

	// Check TaskAccepted was sent
	messages := sender.Messages()
	require.GreaterOrEqual(t, len(messages), 1)
	accepted := messages[0].GetTaskAccepted()
	require.NotNil(t, accepted)
	assert.Equal(t, "task_123", accepted.TaskId)

	// Check TaskCompleted
	completed := result.GetTaskCompleted()
	require.NotNil(t, completed)
	assert.Equal(t, "task_123", completed.TaskId)
	assert.True(t, completed.Success)
	assert.Equal(t, int32(0), completed.ExitCode)
	assert.Equal(t, int64(100), completed.TokensInput)
	assert.Equal(t, int64(50), completed.TokensOutput)

	// Status should have been set to busy and back to idle
	statuses := statusSetter.Statuses()
	assert.Equal(t, []string{"busy", "idle"}, statuses)
}

func TestTaskRunner_Execute_ExecutorError(t *testing.T) {
	logger := zaptest.NewLogger(t)
	sender := &mockMessageSender{}
	exec := &mockExecutor{
		executeFunc: func(ctx context.Context, task *executor.Task, config *executor.AgentConfig, handler executor.OutputHandler) (*executor.Result, error) {
			return nil, errors.New("executor failed")
		},
	}
	wsMgr := NewWorkspaceManager("/tmp", logger)
	cmdHandler := NewDefaultCommandHandler(wsMgr, logger)
	statusSetter := &mockStatusSetter{}

	runner := NewTaskRunner(sender, exec, wsMgr, cmdHandler, statusSetter, nil, logger)

	// Attach session first
	attachCmd := &pb.AttachSession{
		SessionId:     "sess_123",
		WorkspacePath: "ws_test",
	}
	_, err := cmdHandler.HandleAttachSession(context.Background(), attachCmd)
	require.NoError(t, err)

	cmd := &pb.ExecuteTask{
		TaskId:    "task_123",
		RunId:     "trun_456",
		SessionId: "sess_123",
		Attempt:   1,
		Prompt:    "test prompt",
	}

	result, err := runner.Execute(context.Background(), cmd)

	require.NoError(t, err) // Execute itself doesn't return error
	require.NotNil(t, result)

	completed := result.GetTaskCompleted()
	require.NotNil(t, completed)
	assert.False(t, completed.Success)
	assert.Equal(t, "executor failed", completed.Error)
}

func TestTaskRunner_Execute_NilStatusSetter(t *testing.T) {
	logger := zaptest.NewLogger(t)
	sender := &mockMessageSender{}
	exec := &mockExecutor{}
	wsMgr := NewWorkspaceManager("/tmp", logger)
	cmdHandler := NewDefaultCommandHandler(wsMgr, logger)

	// Create runner without status setter
	runner := NewTaskRunner(sender, exec, wsMgr, cmdHandler, nil, nil, logger)

	// Attach session first
	attachCmd := &pb.AttachSession{
		SessionId:     "sess_123",
		WorkspacePath: "ws_test",
	}
	_, err := cmdHandler.HandleAttachSession(context.Background(), attachCmd)
	require.NoError(t, err)

	cmd := &pb.ExecuteTask{
		TaskId:    "task_123",
		RunId:     "trun_456",
		SessionId: "sess_123",
		Attempt:   1,
		Prompt:    "test prompt",
	}

	// Should not panic with nil status setter
	result, err := runner.Execute(context.Background(), cmd)

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.True(t, result.GetTaskCompleted().Success)
}

func TestTaskRunner_HandleOutput_NoActiveTask(t *testing.T) {
	logger := zaptest.NewLogger(t)
	sender := &mockMessageSender{}
	exec := &mockExecutor{}
	wsMgr := NewWorkspaceManager("/tmp", logger)
	cmdHandler := NewDefaultCommandHandler(wsMgr, logger)

	runner := NewTaskRunner(sender, exec, wsMgr, cmdHandler, nil, nil, logger)

	// Should not panic when no active task
	runner.HandleOutput("stdout", []byte("test data"))
}

func TestTaskRunner_HandleOutput_WithActiveTask(t *testing.T) {
	logger := zaptest.NewLogger(t)
	sender := &mockMessageSender{}
	outputReceived := make(chan struct{})
	exec := &mockExecutor{
		executeFunc: func(ctx context.Context, task *executor.Task, config *executor.AgentConfig, handler executor.OutputHandler) (*executor.Result, error) {
			handler.HandleOutput("stdout", []byte("test output"))
			close(outputReceived)
			return &executor.Result{Success: true}, nil
		},
	}
	wsMgr := NewWorkspaceManager("/tmp", logger)
	cmdHandler := NewDefaultCommandHandler(wsMgr, logger)

	runner := NewTaskRunner(sender, exec, wsMgr, cmdHandler, nil, nil, logger)

	// Attach session
	attachCmd := &pb.AttachSession{
		SessionId:     "sess_123",
		WorkspacePath: "ws_test",
	}
	_, err := cmdHandler.HandleAttachSession(context.Background(), attachCmd)
	require.NoError(t, err)

	cmd := &pb.ExecuteTask{
		TaskId:    "task_123",
		RunId:     "trun_456",
		SessionId: "sess_123",
		Attempt:   1,
		Prompt:    "test prompt",
	}

	_, err = runner.Execute(context.Background(), cmd)
	require.NoError(t, err)

	// Wait for output to be handled
	select {
	case <-outputReceived:
		// Success
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for output")
	}
}

func TestTaskRunner_HandlePermissionRequest_NoActiveTask(t *testing.T) {
	logger := zaptest.NewLogger(t)
	sender := &mockMessageSender{}
	exec := &mockExecutor{}
	wsMgr := NewWorkspaceManager("/tmp", logger)
	cmdHandler := NewDefaultCommandHandler(wsMgr, logger)

	runner := NewTaskRunner(sender, exec, wsMgr, cmdHandler, nil, nil, logger)

	req := &executor.PermissionRequest{
		ID:        "perm_123",
		Tool:      "bash",
		Action:    "rm -rf /",
		RiskLevel: executor.RiskHigh,
	}

	approved, err := runner.HandlePermissionRequest(context.Background(), req)

	assert.False(t, approved)
	assert.ErrorIs(t, err, ErrNoActiveTask)
}

func TestTaskRunner_HandlePermissionRequest_Approved(t *testing.T) {
	logger := zaptest.NewLogger(t)
	sender := &mockMessageSender{}
	permRequestReceived := make(chan struct{})
	exec := &mockExecutor{
		executeFunc: func(ctx context.Context, task *executor.Task, config *executor.AgentConfig, handler executor.OutputHandler) (*executor.Result, error) {
			// Request permission
			go func() {
				<-permRequestReceived
			}()
			approved, permErr := handler.HandlePermissionRequest(ctx, &executor.PermissionRequest{
				ID:        "perm_123",
				Tool:      "bash",
				Action:    "echo hello",
				RiskLevel: executor.RiskLow,
			})
			if permErr != nil {
				return &executor.Result{Success: false, Error: permErr.Error()}, nil //nolint:nilerr // intentional: return result with error details
			}
			if !approved {
				return &executor.Result{Success: false, Error: "permission denied"}, nil
			}
			return &executor.Result{Success: true}, nil
		},
	}
	wsMgr := NewWorkspaceManager("/tmp", logger)
	cmdHandler := NewDefaultCommandHandler(wsMgr, logger)

	runner := NewTaskRunner(sender, exec, wsMgr, cmdHandler, nil, nil, logger)

	// Attach session
	attachCmd := &pb.AttachSession{
		SessionId:     "sess_123",
		WorkspacePath: "ws_test",
	}
	_, err := cmdHandler.HandleAttachSession(context.Background(), attachCmd)
	require.NoError(t, err)

	cmd := &pb.ExecuteTask{
		TaskId:    "task_123",
		RunId:     "trun_456",
		SessionId: "sess_123",
		Attempt:   1,
		Prompt:    "test prompt",
	}

	// Start execution in goroutine
	resultChan := make(chan *pb.RunnerMessage)
	go func() {
		result, _ := runner.Execute(context.Background(), cmd)
		resultChan <- result
	}()

	// Wait for permission request to be sent
	time.Sleep(100 * time.Millisecond)

	// Approve the permission
	err = runner.HandlePermissionResponse(context.Background(), &pb.ApprovePermission{
		RequestId: "perm_123",
		Approved:  true,
	})
	require.NoError(t, err)
	close(permRequestReceived)

	// Get result
	result := <-resultChan
	completed := result.GetTaskCompleted()
	require.NotNil(t, completed)
	assert.True(t, completed.Success)
}

func TestTaskRunner_HandlePermissionRequest_Denied(t *testing.T) {
	logger := zaptest.NewLogger(t)
	sender := &mockMessageSender{}
	exec := &mockExecutor{
		executeFunc: func(ctx context.Context, task *executor.Task, config *executor.AgentConfig, handler executor.OutputHandler) (*executor.Result, error) {
			approved, permErr := handler.HandlePermissionRequest(ctx, &executor.PermissionRequest{
				ID:        "perm_456",
				Tool:      "bash",
				Action:    "rm -rf /",
				RiskLevel: executor.RiskHigh,
			})
			if permErr != nil {
				return &executor.Result{Success: false, Error: permErr.Error()}, nil //nolint:nilerr // intentional: return result with error details
			}
			if !approved {
				return &executor.Result{Success: false, Error: "permission denied"}, nil
			}
			return &executor.Result{Success: true}, nil
		},
	}
	wsMgr := NewWorkspaceManager("/tmp", logger)
	cmdHandler := NewDefaultCommandHandler(wsMgr, logger)

	runner := NewTaskRunner(sender, exec, wsMgr, cmdHandler, nil, nil, logger)

	// Attach session
	attachCmd := &pb.AttachSession{
		SessionId:     "sess_123",
		WorkspacePath: "ws_test",
	}
	_, err := cmdHandler.HandleAttachSession(context.Background(), attachCmd)
	require.NoError(t, err)

	cmd := &pb.ExecuteTask{
		TaskId:    "task_123",
		RunId:     "trun_456",
		SessionId: "sess_123",
		Attempt:   1,
		Prompt:    "test prompt",
	}

	// Start execution in goroutine
	resultChan := make(chan *pb.RunnerMessage)
	go func() {
		result, _ := runner.Execute(context.Background(), cmd)
		resultChan <- result
	}()

	// Wait for permission request to be sent
	time.Sleep(100 * time.Millisecond)

	// Deny the permission
	err = runner.HandlePermissionResponse(context.Background(), &pb.ApprovePermission{
		RequestId: "perm_456",
		Approved:  false,
	})
	require.NoError(t, err)

	// Get result
	result := <-resultChan
	completed := result.GetTaskCompleted()
	require.NotNil(t, completed)
	assert.False(t, completed.Success)
	assert.Equal(t, "permission denied", completed.Error)
}

func TestTaskRunner_HandlePermissionRequest_ContextCancelled(t *testing.T) {
	logger := zaptest.NewLogger(t)
	sender := &mockMessageSender{}
	exec := &mockExecutor{
		executeFunc: func(ctx context.Context, task *executor.Task, config *executor.AgentConfig, handler executor.OutputHandler) (*executor.Result, error) {
			approved, permErr := handler.HandlePermissionRequest(ctx, &executor.PermissionRequest{
				ID:        "perm_789",
				Tool:      "bash",
				Action:    "sleep 100",
				RiskLevel: executor.RiskMedium,
			})
			if permErr != nil {
				return &executor.Result{Success: false, Error: permErr.Error()}, nil //nolint:nilerr // intentional: return result with error details
			}
			if !approved {
				return &executor.Result{Success: false, Error: "permission denied"}, nil
			}
			return &executor.Result{Success: true}, nil
		},
	}
	wsMgr := NewWorkspaceManager("/tmp", logger)
	cmdHandler := NewDefaultCommandHandler(wsMgr, logger)

	runner := NewTaskRunner(sender, exec, wsMgr, cmdHandler, nil, nil, logger)

	// Attach session
	attachCmd := &pb.AttachSession{
		SessionId:     "sess_123",
		WorkspacePath: "ws_test",
	}
	_, err := cmdHandler.HandleAttachSession(context.Background(), attachCmd)
	require.NoError(t, err)

	cmd := &pb.ExecuteTask{
		TaskId:    "task_123",
		RunId:     "trun_456",
		SessionId: "sess_123",
		Attempt:   1,
		Prompt:    "test prompt",
	}

	// Create cancellable context
	ctx, cancel := context.WithCancel(context.Background())

	// Start execution in goroutine
	resultChan := make(chan *pb.RunnerMessage)
	go func() {
		result, _ := runner.Execute(ctx, cmd)
		resultChan <- result
	}()

	// Wait for permission request to be sent
	time.Sleep(100 * time.Millisecond)

	// Cancel the context
	cancel()

	// Get result
	result := <-resultChan
	completed := result.GetTaskCompleted()
	require.NotNil(t, completed)
	assert.False(t, completed.Success)
	assert.Contains(t, completed.Error, "context canceled")
}

func TestTaskRunner_HandlePermissionResponse_UnknownRequest(t *testing.T) {
	logger := zaptest.NewLogger(t)
	sender := &mockMessageSender{}
	exec := &mockExecutor{}
	wsMgr := NewWorkspaceManager("/tmp", logger)
	cmdHandler := NewDefaultCommandHandler(wsMgr, logger)

	runner := NewTaskRunner(sender, exec, wsMgr, cmdHandler, nil, nil, logger)

	// Handle response for unknown request - should not error
	err := runner.HandlePermissionResponse(context.Background(), &pb.ApprovePermission{
		RequestId: "perm_unknown",
		Approved:  true,
	})

	assert.NoError(t, err)
}

func TestTaskRunner_HandlePermissionResponse_ChannelFull(t *testing.T) {
	logger := zaptest.NewLogger(t)
	sender := &mockMessageSender{}

	// Create a channel to signal when permission request is made
	permRequestMade := make(chan struct{})
	// Create a channel to signal when we should return from executor
	returnSignal := make(chan struct{})

	exec := &mockExecutor{
		executeFunc: func(ctx context.Context, task *executor.Task, config *executor.AgentConfig, handler executor.OutputHandler) (*executor.Result, error) {
			// Signal that permission request is about to be made
			close(permRequestMade)
			// This will block waiting for permission
			_, _ = handler.HandlePermissionRequest(ctx, &executor.PermissionRequest{
				ID:        "perm_block",
				Tool:      "bash",
				Action:    "test",
				RiskLevel: executor.RiskLow,
			})
			<-returnSignal
			return &executor.Result{Success: true}, nil
		},
	}
	wsMgr := NewWorkspaceManager("/tmp", logger)
	cmdHandler := NewDefaultCommandHandler(wsMgr, logger)

	runner := NewTaskRunner(sender, exec, wsMgr, cmdHandler, nil, nil, logger)

	// Attach session
	attachCmd := &pb.AttachSession{
		SessionId:     "sess_123",
		WorkspacePath: "ws_test",
	}
	_, err := cmdHandler.HandleAttachSession(context.Background(), attachCmd)
	require.NoError(t, err)

	cmd := &pb.ExecuteTask{
		TaskId:    "task_123",
		RunId:     "trun_456",
		SessionId: "sess_123",
		Attempt:   1,
		Prompt:    "test prompt",
	}

	// Start execution in goroutine
	go func() {
		_, _ = runner.Execute(context.Background(), cmd)
	}()

	// Wait for permission request to be registered
	<-permRequestMade
	time.Sleep(50 * time.Millisecond)

	// Send first response - this fills the channel
	err = runner.HandlePermissionResponse(context.Background(), &pb.ApprovePermission{
		RequestId: "perm_block",
		Approved:  true,
	})
	require.NoError(t, err)

	// Send second response - channel is full, should return nil (default case)
	err = runner.HandlePermissionResponse(context.Background(), &pb.ApprovePermission{
		RequestId: "perm_block",
		Approved:  false,
	})
	// This should not error - just silently drops due to default case
	assert.NoError(t, err)

	close(returnSignal)
}

func TestTaskRunner_HandlePermissionRequest_GeneratesID(t *testing.T) {
	logger := zaptest.NewLogger(t)
	sender := &mockMessageSender{}
	exec := &mockExecutor{
		executeFunc: func(ctx context.Context, task *executor.Task, config *executor.AgentConfig, handler executor.OutputHandler) (*executor.Result, error) {
			// Request permission without ID - should generate one
			approved, permErr := handler.HandlePermissionRequest(ctx, &executor.PermissionRequest{
				// No ID set
				Tool:      "bash",
				Action:    "echo hello",
				RiskLevel: executor.RiskLow,
			})
			if permErr != nil {
				return &executor.Result{Success: false, Error: permErr.Error()}, nil //nolint:nilerr // intentional: return result with error details
			}
			if !approved {
				return &executor.Result{Success: false, Error: "permission denied"}, nil
			}
			return &executor.Result{Success: true}, nil
		},
	}
	wsMgr := NewWorkspaceManager("/tmp", logger)
	cmdHandler := NewDefaultCommandHandler(wsMgr, logger)

	runner := NewTaskRunner(sender, exec, wsMgr, cmdHandler, nil, nil, logger)

	// Attach session
	attachCmd := &pb.AttachSession{
		SessionId:     "sess_123",
		WorkspacePath: "ws_test",
	}
	_, err := cmdHandler.HandleAttachSession(context.Background(), attachCmd)
	require.NoError(t, err)

	cmd := &pb.ExecuteTask{
		TaskId:    "task_123",
		RunId:     "trun_456",
		SessionId: "sess_123",
		Attempt:   1,
		Prompt:    "test prompt",
	}

	// Start execution in goroutine
	resultChan := make(chan *pb.RunnerMessage)
	go func() {
		result, _ := runner.Execute(context.Background(), cmd)
		resultChan <- result
	}()

	// Wait for permission request to be sent
	time.Sleep(100 * time.Millisecond)

	// Check that a permission request was sent with generated ID
	messages := sender.Messages()
	var permReq *pb.PermissionRequest
	for _, msg := range messages {
		if pr := msg.GetPermissionRequest(); pr != nil {
			permReq = pr
			break
		}
	}
	require.NotNil(t, permReq)
	assert.NotEmpty(t, permReq.RequestId)
	assert.True(t, len(permReq.RequestId) > 0)

	// Approve using the generated ID
	err = runner.HandlePermissionResponse(context.Background(), &pb.ApprovePermission{
		RequestId: permReq.RequestId,
		Approved:  true,
	})
	require.NoError(t, err)

	// Get result
	result := <-resultChan
	completed := result.GetTaskCompleted()
	require.NotNil(t, completed)
	assert.True(t, completed.Success)
}

func TestTaskRunner_Execute_WithAgentConfig(t *testing.T) {
	logger := zaptest.NewLogger(t)
	sender := &mockMessageSender{}

	var capturedConfig *executor.AgentConfig
	exec := &mockExecutor{
		executeFunc: func(ctx context.Context, task *executor.Task, config *executor.AgentConfig, handler executor.OutputHandler) (*executor.Result, error) {
			capturedConfig = config
			return &executor.Result{Success: true}, nil
		},
	}
	wsMgr := NewWorkspaceManager("/tmp", logger)
	cmdHandler := NewDefaultCommandHandler(wsMgr, logger)

	runner := NewTaskRunner(sender, exec, wsMgr, cmdHandler, nil, nil, logger)

	// Attach session with agent config
	attachCmd := &pb.AttachSession{
		SessionId:     "sess_123",
		WorkspacePath: "ws_test",
		AgentConfig: &pb.AgentConfig{
			Agent:   "claude",
			Model:   "claude-3-5-sonnet",
			ApiKey:  "test-api-key",
			BaseUrl: "https://api.anthropic.com",
		},
	}
	_, err := cmdHandler.HandleAttachSession(context.Background(), attachCmd)
	require.NoError(t, err)

	cmd := &pb.ExecuteTask{
		TaskId:    "task_123",
		RunId:     "trun_456",
		SessionId: "sess_123",
		Attempt:   1,
		Prompt:    "test prompt",
	}

	_, err = runner.Execute(context.Background(), cmd)
	require.NoError(t, err)

	// Verify agent config was passed
	require.NotNil(t, capturedConfig)
	assert.Equal(t, "claude", capturedConfig.Agent)
	assert.Equal(t, "claude-3-5-sonnet", capturedConfig.Model)
	assert.Equal(t, "test-api-key", capturedConfig.APIKey)
	assert.Equal(t, "https://api.anthropic.com", capturedConfig.BaseURL)
	assert.Equal(t, "/tmp/ws_test", capturedConfig.WorkingDir)
}

func TestStreamToLevel(t *testing.T) {
	tests := []struct {
		stream   string
		expected string
	}{
		{"stderr", "error"},
		{"system", "info"},
		{"stdout", "info"},
		{"unknown", "info"},
		{"", "info"},
	}

	for _, tt := range tests {
		t.Run(tt.stream, func(t *testing.T) {
			result := streamToLevel(tt.stream)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// mockLogStreamer implements LogStreamer for testing.
type mockLogStreamer struct {
	mu       sync.Mutex
	started  bool
	closed   bool
	entries  []*pb.LogEntry
	startErr error
	sendErr  error
	closeErr error
	response *pb.StreamLogsResponse
}

func (m *mockLogStreamer) Start(ctx context.Context, init *pb.StreamLogsInit) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.startErr != nil {
		return m.startErr
	}
	m.started = true
	return nil
}

func (m *mockLogStreamer) Send(entry *pb.LogEntry) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.sendErr != nil {
		return m.sendErr
	}
	m.entries = append(m.entries, entry)
	return nil
}

func (m *mockLogStreamer) Close() (*pb.StreamLogsResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closed = true
	if m.closeErr != nil {
		return nil, m.closeErr
	}
	if m.response == nil {
		m.response = &pb.StreamLogsResponse{}
	}
	return m.response, nil
}

func (m *mockLogStreamer) IsActive() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.started && !m.closed
}

func (m *mockLogStreamer) Entries() []*pb.LogEntry {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.entries
}

func TestTaskRunner_HandleOutput_WithLogStreamer(t *testing.T) {
	logger := zaptest.NewLogger(t)
	sender := &mockMessageSender{}
	logStreamer := &mockLogStreamer{
		response: &pb.StreamLogsResponse{
			LogsReceived: 3,
			LogsStored:   3,
		},
	}

	var outputHandled sync.WaitGroup
	outputHandled.Add(3)

	exec := &mockExecutor{
		executeFunc: func(ctx context.Context, task *executor.Task, config *executor.AgentConfig, handler executor.OutputHandler) (*executor.Result, error) {
			// Simulate multiple outputs
			handler.HandleOutput("stdout", []byte("stdout message"))
			outputHandled.Done()
			handler.HandleOutput("stderr", []byte("stderr message"))
			outputHandled.Done()
			handler.HandleOutput("system", []byte("system message"))
			outputHandled.Done()
			return &executor.Result{Success: true}, nil
		},
	}
	wsMgr := NewWorkspaceManager("/tmp", logger)
	cmdHandler := NewDefaultCommandHandler(wsMgr, logger)

	runner := NewTaskRunner(sender, exec, wsMgr, cmdHandler, nil, logStreamer, logger)

	// Attach session
	attachCmd := &pb.AttachSession{
		SessionId:     "sess_123",
		WorkspacePath: "ws_test",
	}
	_, err := cmdHandler.HandleAttachSession(context.Background(), attachCmd)
	require.NoError(t, err)

	cmd := &pb.ExecuteTask{
		TaskId:    "task_123",
		RunId:     "trun_456",
		SessionId: "sess_123",
		Attempt:   1,
		Prompt:    "test prompt",
	}

	result, err := runner.Execute(context.Background(), cmd)
	require.NoError(t, err)
	assert.True(t, result.GetTaskCompleted().Success)

	// Wait for outputs
	outputHandled.Wait()

	// Verify log streamer received entries
	entries := logStreamer.Entries()
	require.Len(t, entries, 3)

	// Verify entry contents
	assert.Equal(t, "stdout message", entries[0].Content)
	assert.Equal(t, "stdout", entries[0].Stream)
	assert.Equal(t, "info", entries[0].Level)

	assert.Equal(t, "stderr message", entries[1].Content)
	assert.Equal(t, "stderr", entries[1].Stream)
	assert.Equal(t, "error", entries[1].Level)

	assert.Equal(t, "system message", entries[2].Content)
	assert.Equal(t, "system", entries[2].Stream)
	assert.Equal(t, "info", entries[2].Level)

	// Verify stream was started and closed
	assert.True(t, logStreamer.closed)
}

func TestTaskRunner_HandleOutput_LogStreamerSendError(t *testing.T) {
	logger := zaptest.NewLogger(t)
	sender := &mockMessageSender{}
	logStreamer := &mockLogStreamer{
		sendErr: errors.New("send failed"),
	}

	exec := &mockExecutor{
		executeFunc: func(ctx context.Context, task *executor.Task, config *executor.AgentConfig, handler executor.OutputHandler) (*executor.Result, error) {
			// This should not panic even if send fails
			handler.HandleOutput("stdout", []byte("test message"))
			return &executor.Result{Success: true}, nil
		},
	}
	wsMgr := NewWorkspaceManager("/tmp", logger)
	cmdHandler := NewDefaultCommandHandler(wsMgr, logger)

	runner := NewTaskRunner(sender, exec, wsMgr, cmdHandler, nil, logStreamer, logger)

	// Attach session
	attachCmd := &pb.AttachSession{
		SessionId:     "sess_123",
		WorkspacePath: "ws_test",
	}
	_, err := cmdHandler.HandleAttachSession(context.Background(), attachCmd)
	require.NoError(t, err)

	cmd := &pb.ExecuteTask{
		TaskId:    "task_123",
		RunId:     "trun_456",
		SessionId: "sess_123",
		Attempt:   1,
		Prompt:    "test prompt",
	}

	// Should complete without error even if log streaming fails
	result, err := runner.Execute(context.Background(), cmd)
	require.NoError(t, err)
	assert.True(t, result.GetTaskCompleted().Success)
}

func TestTaskRunner_Execute_LogStreamerStartError(t *testing.T) {
	logger := zaptest.NewLogger(t)
	sender := &mockMessageSender{}
	logStreamer := &mockLogStreamer{
		startErr: errors.New("start failed"),
	}

	exec := &mockExecutor{
		executeFunc: func(ctx context.Context, task *executor.Task, config *executor.AgentConfig, handler executor.OutputHandler) (*executor.Result, error) {
			return &executor.Result{Success: true}, nil
		},
	}
	wsMgr := NewWorkspaceManager("/tmp", logger)
	cmdHandler := NewDefaultCommandHandler(wsMgr, logger)

	runner := NewTaskRunner(sender, exec, wsMgr, cmdHandler, nil, logStreamer, logger)

	// Attach session
	attachCmd := &pb.AttachSession{
		SessionId:     "sess_123",
		WorkspacePath: "ws_test",
	}
	_, err := cmdHandler.HandleAttachSession(context.Background(), attachCmd)
	require.NoError(t, err)

	cmd := &pb.ExecuteTask{
		TaskId:    "task_123",
		RunId:     "trun_456",
		SessionId: "sess_123",
		Attempt:   1,
		Prompt:    "test prompt",
	}

	// Should complete successfully even if log streaming fails to start
	result, err := runner.Execute(context.Background(), cmd)
	require.NoError(t, err)
	assert.True(t, result.GetTaskCompleted().Success)
}
