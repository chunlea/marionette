package claude

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chunlea/marionette/pkg/agent/executor"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
)

// MockOutputHandler for testing.
type MockOutputHandler struct {
	mu      sync.Mutex
	Outputs []OutputRecord
}

type OutputRecord struct {
	Stream string
	Data   []byte
}

func NewMockOutputHandler() *MockOutputHandler {
	return &MockOutputHandler{}
}

func (m *MockOutputHandler) HandleOutput(stream string, data []byte) {
	m.mu.Lock()
	defer m.mu.Unlock()
	// Make a copy of data since it might be reused
	dataCopy := make([]byte, len(data))
	copy(dataCopy, data)
	m.Outputs = append(m.Outputs, OutputRecord{Stream: stream, Data: dataCopy})
}

func (m *MockOutputHandler) HandlePermissionRequest(_ context.Context, _ *executor.PermissionRequest) (bool, error) {
	return true, nil
}

func (m *MockOutputHandler) GetOutputs() []OutputRecord {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]OutputRecord, len(m.Outputs))
	copy(result, m.Outputs)
	return result
}

func (m *MockOutputHandler) GetCombinedOutput() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	var sb strings.Builder
	for _, o := range m.Outputs {
		sb.Write(o.Data)
	}
	return sb.String()
}

// createMockScript creates a temporary script that simulates claude behavior.
func createMockScript(t *testing.T, script string) string {
	t.Helper()
	tmpDir := t.TempDir()
	scriptPath := filepath.Join(tmpDir, "mock-claude")
	err := os.WriteFile(scriptPath, []byte(script), 0755) //nolint:gosec // test script needs exec
	require.NoError(t, err)
	return scriptPath
}

func TestExecutor_Name(t *testing.T) {
	logger := zaptest.NewLogger(t)
	e := New(logger)
	assert.Equal(t, "claude", e.Name())
}

func TestExecutor_Kill_NotRunning(t *testing.T) {
	logger := zaptest.NewLogger(t)
	e := New(logger)

	// Kill when not running should not error
	err := e.Kill()
	assert.NoError(t, err)
}

func TestExecutor_WithCommandPath(t *testing.T) {
	logger := zaptest.NewLogger(t)
	e := New(logger).WithCommandPath("/custom/path")
	assert.Equal(t, "/custom/path", e.commandPath)
}

func TestExecutor_Execute_Success(t *testing.T) {
	// Create a mock script that outputs some text and exits successfully
	script := `#!/bin/bash
echo "Starting task"
echo "Processing prompt: $*"
echo "Task completed"
exit 0
`
	scriptPath := createMockScript(t, script)

	logger := zaptest.NewLogger(t)
	e := New(logger).WithCommandPath(scriptPath)
	handler := NewMockOutputHandler()

	ctx := context.Background()
	task := &executor.Task{
		ID:      "task_1",
		RunID:   "run_1",
		Prompt:  "Test prompt",
		Timeout: 10 * time.Second,
	}
	config := &executor.AgentConfig{
		WorkingDir: t.TempDir(),
	}

	result, err := e.Execute(ctx, task, config, handler)
	require.NoError(t, err)
	require.NotNil(t, result)

	assert.True(t, result.Success)
	assert.Equal(t, 0, result.ExitCode)
	assert.Empty(t, result.Error)

	// Verify output was captured
	output := handler.GetCombinedOutput()
	assert.Contains(t, output, "Starting task")
	assert.Contains(t, output, "Task completed")
}

func TestExecutor_Execute_Failure(t *testing.T) {
	// Create a mock script that fails
	script := `#!/bin/bash
echo "Error occurred"
exit 1
`
	scriptPath := createMockScript(t, script)

	logger := zaptest.NewLogger(t)
	e := New(logger).WithCommandPath(scriptPath)
	handler := NewMockOutputHandler()

	ctx := context.Background()
	task := &executor.Task{
		ID:      "task_1",
		RunID:   "run_1",
		Prompt:  "Test prompt",
		Timeout: 10 * time.Second,
	}
	config := &executor.AgentConfig{
		WorkingDir: t.TempDir(),
	}

	result, err := e.Execute(ctx, task, config, handler)
	require.NoError(t, err)
	require.NotNil(t, result)

	assert.False(t, result.Success)
	assert.Equal(t, 1, result.ExitCode)
	assert.Contains(t, result.Error, "exited with code 1")
}

func TestExecutor_Execute_Timeout(t *testing.T) {
	// Create a mock script that runs forever
	script := `#!/bin/bash
echo "Starting long task"
sleep 60
echo "Should not reach here"
`
	scriptPath := createMockScript(t, script)

	logger := zaptest.NewLogger(t)
	e := New(logger).WithCommandPath(scriptPath)
	handler := NewMockOutputHandler()

	ctx := context.Background()
	task := &executor.Task{
		ID:      "task_1",
		RunID:   "run_1",
		Prompt:  "Test prompt",
		Timeout: 500 * time.Millisecond, // Short timeout
	}
	config := &executor.AgentConfig{
		WorkingDir: t.TempDir(),
	}

	result, err := e.Execute(ctx, task, config, handler)
	require.NoError(t, err)
	require.NotNil(t, result)

	assert.False(t, result.Success)
	assert.NotEqual(t, 0, result.ExitCode)
}

func TestExecutor_Execute_ContextCancel(t *testing.T) {
	// Create a mock script that runs for a while
	script := `#!/bin/bash
echo "Starting task"
sleep 60
`
	scriptPath := createMockScript(t, script)

	logger := zaptest.NewLogger(t)
	e := New(logger).WithCommandPath(scriptPath)
	handler := NewMockOutputHandler()

	ctx, cancel := context.WithCancel(context.Background())
	task := &executor.Task{
		ID:      "task_1",
		RunID:   "run_1",
		Prompt:  "Test prompt",
		Timeout: 0, // No timeout
	}
	config := &executor.AgentConfig{
		WorkingDir: t.TempDir(),
	}

	// Cancel context after a short delay
	go func() {
		time.Sleep(200 * time.Millisecond)
		cancel()
	}()

	result, err := e.Execute(ctx, task, config, handler)
	require.NoError(t, err)
	require.NotNil(t, result)

	assert.False(t, result.Success)
}

func TestExecutor_Execute_Kill(t *testing.T) {
	// Create a mock script that runs for a while
	script := `#!/bin/bash
trap 'echo "Received signal"; exit 0' SIGTERM
echo "Starting task"
sleep 60
`
	scriptPath := createMockScript(t, script)

	logger := zaptest.NewLogger(t)
	e := New(logger).WithCommandPath(scriptPath)
	handler := NewMockOutputHandler()

	ctx := context.Background()
	task := &executor.Task{
		ID:      "task_1",
		RunID:   "run_1",
		Prompt:  "Test prompt",
		Timeout: 0, // No timeout
	}
	config := &executor.AgentConfig{
		WorkingDir: t.TempDir(),
	}

	// Run execute in goroutine
	done := make(chan *executor.Result)
	go func() {
		result, _ := e.Execute(ctx, task, config, handler)
		done <- result
	}()

	// Wait for task to start, then kill
	time.Sleep(200 * time.Millisecond)
	err := e.Kill()
	require.NoError(t, err)

	result := <-done
	require.NotNil(t, result)
	assert.False(t, result.Success)
	assert.Contains(t, result.Error, "killed")
}

func TestExecutor_Execute_AlreadyRunning(t *testing.T) {
	// Create a mock script that runs for a while
	script := `#!/bin/bash
echo "Task running"
sleep 60
`
	scriptPath := createMockScript(t, script)

	logger := zaptest.NewLogger(t)
	e := New(logger).WithCommandPath(scriptPath)
	handler := NewMockOutputHandler()

	ctx := context.Background()
	task := &executor.Task{
		ID:      "task_1",
		RunID:   "run_1",
		Prompt:  "Test prompt",
		Timeout: 0,
	}
	config := &executor.AgentConfig{
		WorkingDir: t.TempDir(),
	}

	// Start first task
	go func() {
		_, _ = e.Execute(ctx, task, config, handler)
	}()

	// Wait for task to start
	time.Sleep(100 * time.Millisecond)

	// Try to start second task - should fail
	_, err := e.Execute(ctx, task, config, handler)
	assert.ErrorIs(t, err, ErrAlreadyRunning)

	// Clean up
	_ = e.Kill()
}

func TestExecutor_Execute_EnvironmentVariables(t *testing.T) {
	// Create a mock script that prints environment variables
	script := `#!/bin/bash
echo "API_KEY=${ANTHROPIC_API_KEY}"
echo "BASE_URL=${ANTHROPIC_BASE_URL}"
echo "EXTRA_VAR=${MY_EXTRA_VAR}"
`
	scriptPath := createMockScript(t, script)

	logger := zaptest.NewLogger(t)
	e := New(logger).WithCommandPath(scriptPath)
	handler := NewMockOutputHandler()

	ctx := context.Background()
	task := &executor.Task{
		ID:      "task_1",
		RunID:   "run_1",
		Prompt:  "Test prompt",
		Timeout: 10 * time.Second,
	}
	config := &executor.AgentConfig{
		APIKey:     "test-api-key",
		BaseURL:    "https://test.api.com",
		WorkingDir: t.TempDir(),
		Extra: map[string]string{
			"MY_EXTRA_VAR": "extra-value",
		},
	}

	result, err := e.Execute(ctx, task, config, handler)
	require.NoError(t, err)
	require.NotNil(t, result)

	assert.True(t, result.Success)

	output := handler.GetCombinedOutput()
	assert.Contains(t, output, "API_KEY=test-api-key")
	assert.Contains(t, output, "BASE_URL=https://test.api.com")
	assert.Contains(t, output, "EXTRA_VAR=extra-value")
}

func TestExecutor_Execute_WorkingDirectory(t *testing.T) {
	// Create a mock script that prints working directory
	script := `#!/bin/bash
echo "CWD=$(pwd)"
`
	scriptPath := createMockScript(t, script)

	logger := zaptest.NewLogger(t)
	e := New(logger).WithCommandPath(scriptPath)
	handler := NewMockOutputHandler()

	workDir := t.TempDir()
	// Resolve symlinks for comparison (macOS /var -> /private/var)
	workDirResolved, err := filepath.EvalSymlinks(workDir)
	require.NoError(t, err)

	ctx := context.Background()
	task := &executor.Task{
		ID:      "task_1",
		RunID:   "run_1",
		Prompt:  "Test prompt",
		Timeout: 10 * time.Second,
	}
	config := &executor.AgentConfig{
		WorkingDir: workDir,
	}

	result, err := e.Execute(ctx, task, config, handler)
	require.NoError(t, err)
	require.NotNil(t, result)

	assert.True(t, result.Success)

	output := handler.GetCombinedOutput()
	assert.Contains(t, output, "CWD="+workDirResolved)
}

func TestExecutor_Execute_MultilineOutput(t *testing.T) {
	// Create a mock script with multiple lines of output
	script := `#!/bin/bash
echo "Line 1"
echo "Line 2"
echo "Line 3"
echo ""
echo "Line after empty"
`
	scriptPath := createMockScript(t, script)

	logger := zaptest.NewLogger(t)
	e := New(logger).WithCommandPath(scriptPath)
	handler := NewMockOutputHandler()

	ctx := context.Background()
	task := &executor.Task{
		ID:      "task_1",
		RunID:   "run_1",
		Prompt:  "Test prompt",
		Timeout: 10 * time.Second,
	}
	config := &executor.AgentConfig{
		WorkingDir: t.TempDir(),
	}

	result, err := e.Execute(ctx, task, config, handler)
	require.NoError(t, err)
	require.NotNil(t, result)

	assert.True(t, result.Success)

	outputs := handler.GetOutputs()
	assert.GreaterOrEqual(t, len(outputs), 4) // At least 4 non-empty lines

	// Outputs should be either "stdout" (non-JSON) or "json" (raw JSON for audit)
	for _, o := range outputs {
		assert.True(t, o.Stream == "stdout" || o.Stream == "json",
			"unexpected stream type: %s", o.Stream)
	}
}

func TestResult_ToExecutorResult(t *testing.T) {
	now := time.Now()
	r := &Result{
		Result: executor.Result{
			Success:      true,
			ExitCode:     0,
			Error:        "",
			TokensInput:  100,
			TokensOutput: 200,
			Context:      []byte(`{}`),
			CompletedAt:  now,
		},
		StartedAt: now.Add(-time.Second),
		TimedOut:  false,
	}

	result := r.ToExecutorResult()

	assert.True(t, result.Success)
	assert.Equal(t, 0, result.ExitCode)
	assert.Empty(t, result.Error)
	assert.Equal(t, int64(100), result.TokensInput)
	assert.Equal(t, int64(200), result.TokensOutput)
	assert.Equal(t, now, result.CompletedAt)
}

func TestErrors(t *testing.T) {
	assert.Equal(t, "executor is already running a task", ErrAlreadyRunning.Error())
	assert.Equal(t, "executor is not running a task", ErrNotRunning.Error())
	assert.Equal(t, "task was killed", ErrKilled.Error())
}

// TestExecutor_Interface ensures Executor implements the interface.
func TestExecutor_Interface(t *testing.T) {
	logger := zaptest.NewLogger(t)
	var _ executor.Executor = New(logger)
}

func TestExecutor_Execute_CommandNotFound(t *testing.T) {
	// Test that executor fails gracefully when command is not found
	logger := zaptest.NewLogger(t)
	// Use a non-existent command path
	e := New(logger).WithCommandPath("/nonexistent/path/to/command")
	handler := NewMockOutputHandler()

	ctx := context.Background()
	task := &executor.Task{
		ID:      "task_1",
		RunID:   "run_1",
		Prompt:  "Test prompt",
		Timeout: 10 * time.Second,
	}
	config := &executor.AgentConfig{
		WorkingDir: t.TempDir(),
	}

	result, err := e.Execute(ctx, task, config, handler)
	// Should not return error, but result should indicate failure
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.False(t, result.Success)
	assert.Contains(t, result.Error, "starting process")
}

// =============================================================================
// handleMessage Tests
// =============================================================================

func TestOutputParser_HandleMessage_Init(t *testing.T) {
	logger := zaptest.NewLogger(t)
	handler := NewMockOutputHandler()

	var capturedSessionID string
	parser := &outputParser{
		handler: handler,
		logger:  logger,
		onSessionID: func(sessionID string) {
			capturedSessionID = sessionID
		},
	}

	msg := &StreamMessage{
		Type:      "init",
		SessionID: "sess_123",
		Model:     "claude-3-sonnet",
		CWD:       "/workspace",
	}

	parser.handleMessage(msg)

	assert.Equal(t, "sess_123", capturedSessionID)
}

func TestOutputParser_HandleMessage_Assistant(t *testing.T) {
	logger := zaptest.NewLogger(t)
	handler := NewMockOutputHandler()

	parser := &outputParser{
		handler: handler,
		logger:  logger,
	}

	msg := &StreamMessage{
		Type: "assistant",
		Message: &AssistantMessage{
			Content: []ContentBlock{
				{Type: "text", Text: "Hello, world!"},
				{Type: "text", Text: " More text."},
			},
			Usage: &Usage{
				InputTokens:  100,
				OutputTokens: 50,
			},
		},
	}

	parser.handleMessage(msg)

	assert.Equal(t, int64(100), parser.totalInputTokens)
	assert.Equal(t, int64(50), parser.totalOutputTokens)

	outputs := handler.GetOutputs()
	assert.Len(t, outputs, 2)
	assert.Equal(t, "stdout", outputs[0].Stream)
	assert.Equal(t, "Hello, world!", string(outputs[0].Data))
}

func TestOutputParser_HandleMessage_ContentBlockDelta(t *testing.T) {
	logger := zaptest.NewLogger(t)
	handler := NewMockOutputHandler()

	parser := &outputParser{
		handler: handler,
		logger:  logger,
	}

	msg := &StreamMessage{
		Type:  "content_block_delta",
		Index: 0,
		Delta: &Delta{
			Type: "text_delta",
			Text: "streaming text",
		},
	}

	parser.handleMessage(msg)

	outputs := handler.GetOutputs()
	assert.Len(t, outputs, 1)
	assert.Equal(t, "stdout", outputs[0].Stream)
	assert.Equal(t, "streaming text", string(outputs[0].Data))
}

func TestOutputParser_HandleMessage_ToolUse(t *testing.T) {
	logger := zaptest.NewLogger(t)
	handler := NewMockOutputHandler()

	parser := &outputParser{
		handler: handler,
		logger:  logger,
	}

	msg := &StreamMessage{
		Type: "tool_use",
		ToolUse: &ToolUse{
			ID:    "tool_1",
			Name:  "bash",
			Input: json.RawMessage(`{"command": "ls"}`),
		},
	}

	parser.handleMessage(msg)
	// Just verifies no panic
}

func TestOutputParser_HandleMessage_ToolResult(t *testing.T) {
	logger := zaptest.NewLogger(t)
	handler := NewMockOutputHandler()

	parser := &outputParser{
		handler: handler,
		logger:  logger,
	}

	msg := &StreamMessage{
		Type: "tool_result",
		ToolResult: &ToolResult{
			ToolUseID: "tool_1",
			Content:   "file1.go\nfile2.go",
			IsError:   false,
		},
	}

	parser.handleMessage(msg)
	// Just verifies no panic
}

func TestOutputParser_HandleMessage_Result(t *testing.T) {
	logger := zaptest.NewLogger(t)
	handler := NewMockOutputHandler()

	parser := &outputParser{
		handler: handler,
		logger:  logger,
	}

	msg := &StreamMessage{
		Type: "result",
		Result: &ResultMessage{
			NumTurns:   5,
			CostUSD:    0.05,
			DurationMS: 3000,
			Usage: &Usage{
				InputTokens:  500,
				OutputTokens: 200,
			},
			IsError: false,
		},
	}

	parser.handleMessage(msg)

	assert.Equal(t, int64(500), parser.totalInputTokens)
	assert.Equal(t, int64(200), parser.totalOutputTokens)
	assert.False(t, parser.hasError)
}

func TestOutputParser_HandleMessage_ResultWithError(t *testing.T) {
	logger := zaptest.NewLogger(t)
	handler := NewMockOutputHandler()

	parser := &outputParser{
		handler: handler,
		logger:  logger,
	}

	msg := &StreamMessage{
		Type: "result",
		Result: &ResultMessage{
			IsError: true,
		},
	}

	parser.handleMessage(msg)

	assert.True(t, parser.hasError)
}

func TestOutputParser_HandleMessage_Error(t *testing.T) {
	logger := zaptest.NewLogger(t)
	handler := NewMockOutputHandler()

	parser := &outputParser{
		handler: handler,
		logger:  logger,
	}

	msg := &StreamMessage{
		Type: "error",
		Error: &ErrorMessage{
			Code:    "rate_limit",
			Message: "Rate limit exceeded",
		},
	}

	parser.handleMessage(msg)

	assert.True(t, parser.hasError)
	assert.Equal(t, "Rate limit exceeded", parser.errorMessage)

	outputs := handler.GetOutputs()
	assert.Len(t, outputs, 1)
	assert.Equal(t, "stderr", outputs[0].Stream)
}

func TestOutputParser_HandleMessage_System(t *testing.T) {
	logger := zaptest.NewLogger(t)
	handler := NewMockOutputHandler()

	parser := &outputParser{
		handler: handler,
		logger:  logger,
	}

	msg := &StreamMessage{
		Type: "system",
		Data: "API cost: $0.05",
	}

	parser.handleMessage(msg)

	outputs := handler.GetOutputs()
	assert.Len(t, outputs, 1)
	assert.Equal(t, "system", outputs[0].Stream)
	assert.Contains(t, string(outputs[0].Data), "API cost")
}

func TestOutputParser_HandleMessage_Unknown(t *testing.T) {
	logger := zaptest.NewLogger(t)
	handler := NewMockOutputHandler()

	parser := &outputParser{
		handler: handler,
		logger:  logger,
	}

	msg := &StreamMessage{
		Type: "unknown_type",
	}

	parser.handleMessage(msg)
	// Just verifies no panic and logs unknown type
}

// =============================================================================
// SendMessage/SendTextMessage Tests
// =============================================================================

func TestExecutor_SendMessage_NotRunning(t *testing.T) {
	logger := zaptest.NewLogger(t)
	e := New(logger)

	err := e.SendMessage([]byte(`{"type":"user"}`))
	assert.ErrorIs(t, err, ErrNotRunning)
}

func TestExecutor_SendMessage_NotStreamMode(t *testing.T) {
	// Create a mock script that runs briefly
	script := `#!/bin/bash
sleep 0.5
`
	scriptPath := createMockScript(t, script)

	logger := zaptest.NewLogger(t)
	e := New(logger).WithCommandPath(scriptPath)
	handler := NewMockOutputHandler()

	ctx := context.Background()
	task := &executor.Task{
		ID:     "task_1",
		RunID:  "run_1",
		Prompt: "Test",
		// No Context = no stream mode
	}
	config := &executor.AgentConfig{
		WorkingDir: t.TempDir(),
	}

	done := make(chan struct{})
	go func() {
		_, _ = e.Execute(ctx, task, config, handler)
		close(done)
	}()

	// Wait for task to start
	time.Sleep(100 * time.Millisecond)

	// Try to send message - should fail because not in stream mode
	err := e.SendMessage([]byte(`{"type":"user"}`))
	assert.ErrorIs(t, err, ErrNotStreamMode)

	<-done
}

func TestExecutor_SendTextMessage_NotRunning(t *testing.T) {
	logger := zaptest.NewLogger(t)
	e := New(logger)

	err := e.SendTextMessage("hello")
	assert.ErrorIs(t, err, ErrNotRunning)
}

// =============================================================================
// IsStreamMode/SessionID Tests
// =============================================================================

func TestExecutor_IsStreamMode_Default(t *testing.T) {
	logger := zaptest.NewLogger(t)
	e := New(logger)

	assert.False(t, e.IsStreamMode())
}

func TestExecutor_SessionID_Default(t *testing.T) {
	logger := zaptest.NewLogger(t)
	e := New(logger)

	assert.Empty(t, e.SessionID())
}

// =============================================================================
// NewTextMessage/NewContentBlockMessage Tests
// =============================================================================

func TestNewTextMessage(t *testing.T) {
	msg := NewTextMessage("Hello, Claude!")

	assert.Equal(t, "user", msg.Type)
	assert.NotNil(t, msg.Message)
	assert.Equal(t, "user", msg.Message.Role)

	// Content should be marshaled string
	var content string
	err := json.Unmarshal(msg.Message.Content, &content)
	require.NoError(t, err)
	assert.Equal(t, "Hello, Claude!", content)
}

func TestNewContentBlockMessage(t *testing.T) {
	blocks := []InputContentBlock{
		{Type: "text", Text: "First block"},
		{Type: "text", Text: "Second block"},
	}

	msg := NewContentBlockMessage(blocks)

	assert.Equal(t, "user", msg.Type)
	assert.NotNil(t, msg.Message)
	assert.Equal(t, "user", msg.Message.Role)

	// Content should be marshaled blocks array
	var parsedBlocks []InputContentBlock
	err := json.Unmarshal(msg.Message.Content, &parsedBlocks)
	require.NoError(t, err)
	assert.Len(t, parsedBlocks, 2)
	assert.Equal(t, "First block", parsedBlocks[0].Text)
	assert.Equal(t, "Second block", parsedBlocks[1].Text)
}

// =============================================================================
// buildCommand Tests
// =============================================================================

func TestExecutor_BuildCommand_SandboxModes(t *testing.T) {
	tests := []struct {
		name        string
		sandboxMode string
		wantArg     string
		notWantArg  string
	}{
		{
			name:        "runner-is-sandbox",
			sandboxMode: "runner-is-sandbox",
			wantArg:     "--dangerously-skip-permissions",
		},
		{
			name:        "runner-creates-sandbox",
			sandboxMode: "runner-creates-sandbox",
			wantArg:     "--dangerously-skip-permissions",
		},
		{
			name:        "none uses permission mode",
			sandboxMode: "none",
			wantArg:     "--permission-mode",
			notWantArg:  "--dangerously-skip-permissions",
		},
		{
			name:        "empty uses permission mode",
			sandboxMode: "",
			wantArg:     "--permission-mode",
			notWantArg:  "--dangerously-skip-permissions",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger := zaptest.NewLogger(t)
			e := New(logger).WithCommandPath("/usr/bin/echo") // Use echo as dummy

			task := &executor.Task{
				ID:     "task_1",
				Prompt: "test",
			}
			config := &executor.AgentConfig{
				Extra: map[string]string{
					"sandbox_mode": tt.sandboxMode,
				},
			}

			cmd, _, err := e.buildCommand(task, config)
			require.NoError(t, err)

			args := strings.Join(cmd.Args, " ")
			if tt.wantArg != "" {
				assert.Contains(t, args, tt.wantArg)
			}
			if tt.notWantArg != "" {
				assert.NotContains(t, args, tt.notWantArg)
			}
		})
	}
}

func TestExecutor_BuildCommand_CustomPermissionMode(t *testing.T) {
	logger := zaptest.NewLogger(t)
	e := New(logger).WithCommandPath("/usr/bin/echo")

	task := &executor.Task{
		ID:     "task_1",
		Prompt: "test",
	}
	config := &executor.AgentConfig{
		Extra: map[string]string{
			"sandbox_mode":    "none",
			"permission_mode": "bypassPermissions",
		},
	}

	cmd, _, err := e.buildCommand(task, config)
	require.NoError(t, err)

	args := strings.Join(cmd.Args, " ")
	assert.Contains(t, args, "--permission-mode bypassPermissions")
}

func TestExecutor_BuildCommand_WithModel(t *testing.T) {
	logger := zaptest.NewLogger(t)
	e := New(logger).WithCommandPath("/usr/bin/echo")

	task := &executor.Task{
		ID:     "task_1",
		Prompt: "test",
	}
	config := &executor.AgentConfig{
		Model: "claude-3-opus",
	}

	cmd, _, err := e.buildCommand(task, config)
	require.NoError(t, err)

	args := strings.Join(cmd.Args, " ")
	assert.Contains(t, args, "--model claude-3-opus")
}

func TestExecutor_BuildCommand_WithResume(t *testing.T) {
	logger := zaptest.NewLogger(t)
	e := New(logger).WithCommandPath("/usr/bin/echo")

	contextSnapshot := &ContextSnapshot{
		SessionID:  "sess_abc123",
		WorkingDir: "/workspace",
	}
	contextBytes, _ := json.Marshal(contextSnapshot)

	task := &executor.Task{
		ID:      "task_1",
		Prompt:  "continue work",
		Context: contextBytes,
	}
	config := &executor.AgentConfig{}

	cmd, useStreamInput, err := e.buildCommand(task, config)
	require.NoError(t, err)

	assert.True(t, useStreamInput)

	args := strings.Join(cmd.Args, " ")
	assert.Contains(t, args, "--resume sess_abc123")
	assert.Contains(t, args, "--input-format stream-json")
	// Prompt should NOT be in args for stream mode
	assert.NotContains(t, args, "continue work")
}

func TestExecutor_BuildCommand_InvalidContext(t *testing.T) {
	logger := zaptest.NewLogger(t)
	e := New(logger).WithCommandPath("/usr/bin/echo")

	task := &executor.Task{
		ID:      "task_1",
		Prompt:  "test",
		Context: []byte(`{invalid json`),
	}
	config := &executor.AgentConfig{}

	cmd, useStreamInput, err := e.buildCommand(task, config)
	require.NoError(t, err)

	// Should recover and start fresh
	assert.False(t, useStreamInput)
	args := strings.Join(cmd.Args, " ")
	assert.NotContains(t, args, "--resume")
}

func TestExecutor_BuildCommand_TaskWorkingDir(t *testing.T) {
	logger := zaptest.NewLogger(t)
	e := New(logger).WithCommandPath("/usr/bin/echo")

	taskDir := "/task/specific/dir"
	configDir := "/config/dir"

	task := &executor.Task{
		ID:         "task_1",
		Prompt:     "test",
		WorkingDir: taskDir, // Task dir takes precedence
	}
	config := &executor.AgentConfig{
		WorkingDir: configDir,
	}

	cmd, _, err := e.buildCommand(task, config)
	require.NoError(t, err)

	assert.Equal(t, taskDir, cmd.Dir)
}

func TestExecutor_BuildCommand_ConfigWorkingDir(t *testing.T) {
	logger := zaptest.NewLogger(t)
	e := New(logger).WithCommandPath("/usr/bin/echo")

	configDir := "/config/dir"

	task := &executor.Task{
		ID:     "task_1",
		Prompt: "test",
		// No WorkingDir
	}
	config := &executor.AgentConfig{
		WorkingDir: configDir,
	}

	cmd, _, err := e.buildCommand(task, config)
	require.NoError(t, err)

	assert.Equal(t, configDir, cmd.Dir)
}

func TestExecutor_BuildCommand_NoCommandPath(t *testing.T) {
	logger := zaptest.NewLogger(t)
	e := New(logger)
	// No command path set

	// Save original PATH and set to empty to ensure claude is not found
	origPath := os.Getenv("PATH")
	t.Setenv("PATH", "/nonexistent")
	defer func() { _ = os.Setenv("PATH", origPath) }()

	task := &executor.Task{
		ID:     "task_1",
		Prompt: "test",
	}
	config := &executor.AgentConfig{}

	_, _, err := e.buildCommand(task, config)
	// Should fail to find claude in PATH
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "claude not found")
}

// =============================================================================
// Stream Mode Execute Tests
// =============================================================================

func TestExecutor_Execute_StreamJSON_Parsing(t *testing.T) {
	// Create a mock script that outputs stream-json format
	script := `#!/bin/bash
echo '{"type":"init","session_id":"sess_test123","model":"claude-3"}'
echo '{"type":"assistant","message":{"content":[{"type":"text","text":"Hello!"}],"usage":{"input_tokens":10,"output_tokens":5}}}'
echo '{"type":"result","result":{"num_turns":1,"cost_usd":0.01,"usage":{"input_tokens":10,"output_tokens":5}}}'
`
	scriptPath := createMockScript(t, script)

	logger := zaptest.NewLogger(t)
	e := New(logger).WithCommandPath(scriptPath)
	handler := NewMockOutputHandler()

	ctx := context.Background()
	task := &executor.Task{
		ID:      "task_1",
		RunID:   "run_1",
		Prompt:  "Test prompt",
		Timeout: 10 * time.Second,
	}
	config := &executor.AgentConfig{
		WorkingDir: t.TempDir(),
	}

	result, err := e.Execute(ctx, task, config, handler)
	require.NoError(t, err)
	require.NotNil(t, result)

	assert.True(t, result.Success)
	assert.Equal(t, int64(10), result.TokensInput)
	assert.Equal(t, int64(5), result.TokensOutput)
	assert.Equal(t, "sess_test123", result.AgentSession)

	// Verify context snapshot was created
	assert.NotEmpty(t, result.Context)
	var snapshot ContextSnapshot
	err = json.Unmarshal(result.Context, &snapshot)
	require.NoError(t, err)
	assert.Equal(t, "sess_test123", snapshot.SessionID)
}

func TestExecutor_Execute_WithResumeContext(t *testing.T) {
	// Create a mock script that simulates stream mode with stdin
	script := `#!/bin/bash
# Read from stdin if available
if read -t 0.1 line; then
    echo "Received: $line" >&2
fi
echo '{"type":"init","session_id":"sess_resumed"}'
echo '{"type":"result","result":{"num_turns":1}}'
`
	scriptPath := createMockScript(t, script)

	logger := zaptest.NewLogger(t)
	e := New(logger).WithCommandPath(scriptPath)
	handler := NewMockOutputHandler()

	// Create context snapshot for resume
	contextSnapshot := &ContextSnapshot{
		SessionID:  "sess_previous",
		WorkingDir: "/workspace",
	}
	contextBytes, _ := json.Marshal(contextSnapshot)

	ctx := context.Background()
	task := &executor.Task{
		ID:      "task_1",
		RunID:   "run_1",
		Prompt:  "Continue work",
		Timeout: 10 * time.Second,
		Context: contextBytes,
	}
	config := &executor.AgentConfig{
		WorkingDir: t.TempDir(),
	}

	result, err := e.Execute(ctx, task, config, handler)
	require.NoError(t, err)
	require.NotNil(t, result)

	assert.True(t, result.Success)
	// New session ID should be captured
	assert.Equal(t, "sess_resumed", result.AgentSession)
}

func TestExecutor_Execute_ErrorMessage(t *testing.T) {
	// Create a mock script that outputs an error message
	script := `#!/bin/bash
echo '{"type":"init","session_id":"sess_test"}'
echo '{"type":"error","error":{"code":"api_error","message":"Something went wrong"}}'
exit 0
`
	scriptPath := createMockScript(t, script)

	logger := zaptest.NewLogger(t)
	e := New(logger).WithCommandPath(scriptPath)
	handler := NewMockOutputHandler()

	ctx := context.Background()
	task := &executor.Task{
		ID:      "task_1",
		RunID:   "run_1",
		Prompt:  "Test prompt",
		Timeout: 10 * time.Second,
	}
	config := &executor.AgentConfig{
		WorkingDir: t.TempDir(),
	}

	result, err := e.Execute(ctx, task, config, handler)
	require.NoError(t, err)
	require.NotNil(t, result)

	assert.False(t, result.Success)
	assert.Equal(t, "Something went wrong", result.Error)
}

func TestExecutor_Execute_StderrOutput(t *testing.T) {
	// Create a mock script that outputs to stderr
	script := `#!/bin/bash
echo "stdout message"
echo "stderr message" >&2
echo "another stderr" >&2
`
	scriptPath := createMockScript(t, script)

	logger := zaptest.NewLogger(t)
	e := New(logger).WithCommandPath(scriptPath)
	handler := NewMockOutputHandler()

	ctx := context.Background()
	task := &executor.Task{
		ID:      "task_1",
		RunID:   "run_1",
		Prompt:  "Test prompt",
		Timeout: 10 * time.Second,
	}
	config := &executor.AgentConfig{
		WorkingDir: t.TempDir(),
	}

	result, err := e.Execute(ctx, task, config, handler)
	require.NoError(t, err)
	require.NotNil(t, result)

	outputs := handler.GetOutputs()

	// Check we got stderr outputs
	var stderrCount int
	for _, o := range outputs {
		if o.Stream == "stderr" {
			stderrCount++
		}
	}
	assert.GreaterOrEqual(t, stderrCount, 2)
}

// =============================================================================
// ContextSnapshot Tests
// =============================================================================

func TestContextSnapshot_JSON(t *testing.T) {
	snapshot := &ContextSnapshot{
		SessionID:    "sess_abc123",
		AgentVersion: "1.0.45",
		WorkingDir:   "/workspace/project",
		CreatedAt:    "2024-01-01T00:00:00Z",
	}

	data, err := json.Marshal(snapshot)
	require.NoError(t, err)

	var parsed ContextSnapshot
	err = json.Unmarshal(data, &parsed)
	require.NoError(t, err)

	assert.Equal(t, snapshot.SessionID, parsed.SessionID)
	assert.Equal(t, snapshot.AgentVersion, parsed.AgentVersion)
	assert.Equal(t, snapshot.WorkingDir, parsed.WorkingDir)
	assert.Equal(t, snapshot.CreatedAt, parsed.CreatedAt)
}

// =============================================================================
// Edge Cases
// =============================================================================

func TestOutputParser_HandleMessage_NilCases(t *testing.T) {
	logger := zaptest.NewLogger(t)
	handler := NewMockOutputHandler()

	parser := &outputParser{
		handler: handler,
		logger:  logger,
	}

	// Nil message content
	parser.handleMessage(&StreamMessage{Type: "assistant", Message: nil})
	parser.handleMessage(&StreamMessage{Type: "content_block_delta", Delta: nil})
	parser.handleMessage(&StreamMessage{Type: "tool_use", ToolUse: nil})
	parser.handleMessage(&StreamMessage{Type: "tool_result", ToolResult: nil})
	parser.handleMessage(&StreamMessage{Type: "result", Result: nil})
	parser.handleMessage(&StreamMessage{Type: "error", Error: nil})
	parser.handleMessage(&StreamMessage{Type: "system", Data: ""})

	// Should not panic, and no output for empty data
	outputs := handler.GetOutputs()
	assert.Empty(t, outputs)
}

func TestOutputParser_HandleMessage_AssistantNoUsage(t *testing.T) {
	logger := zaptest.NewLogger(t)
	handler := NewMockOutputHandler()

	parser := &outputParser{
		handler: handler,
		logger:  logger,
	}

	msg := &StreamMessage{
		Type: "assistant",
		Message: &AssistantMessage{
			Content: []ContentBlock{
				{Type: "text", Text: "Hello"},
			},
			Usage: nil, // No usage
		},
	}

	parser.handleMessage(msg)

	assert.Equal(t, int64(0), parser.totalInputTokens)
	assert.Equal(t, int64(0), parser.totalOutputTokens)
}

func TestOutputParser_HandleMessage_ResultNoUsage(t *testing.T) {
	logger := zaptest.NewLogger(t)
	handler := NewMockOutputHandler()

	parser := &outputParser{
		handler: handler,
		logger:  logger,
	}

	msg := &StreamMessage{
		Type: "result",
		Result: &ResultMessage{
			NumTurns: 1,
			Usage:    nil, // No usage
		},
	}

	parser.handleMessage(msg)

	assert.Equal(t, int64(0), parser.totalInputTokens)
	assert.Equal(t, int64(0), parser.totalOutputTokens)
}

func TestOutputParser_HandleMessage_InitNoSessionID(t *testing.T) {
	logger := zaptest.NewLogger(t)
	handler := NewMockOutputHandler()

	var called bool
	parser := &outputParser{
		handler: handler,
		logger:  logger,
		onSessionID: func(sessionID string) {
			called = true
		},
	}

	msg := &StreamMessage{
		Type:      "init",
		SessionID: "", // Empty session ID
	}

	parser.handleMessage(msg)

	assert.False(t, called) // Should not call onSessionID for empty ID
}

func TestOutputParser_HandleMessage_ContentBlockDeltaEmpty(t *testing.T) {
	logger := zaptest.NewLogger(t)
	handler := NewMockOutputHandler()

	parser := &outputParser{
		handler: handler,
		logger:  logger,
	}

	msg := &StreamMessage{
		Type: "content_block_delta",
		Delta: &Delta{
			Type: "text_delta",
			Text: "", // Empty text
		},
	}

	parser.handleMessage(msg)

	outputs := handler.GetOutputs()
	assert.Empty(t, outputs) // Should not output empty text
}

func TestExecutor_Execute_EmptyPrompt(t *testing.T) {
	script := `#!/bin/bash
echo '{"type":"result","result":{"num_turns":0}}'
`
	scriptPath := createMockScript(t, script)

	logger := zaptest.NewLogger(t)
	e := New(logger).WithCommandPath(scriptPath)
	handler := NewMockOutputHandler()

	ctx := context.Background()
	task := &executor.Task{
		ID:      "task_1",
		RunID:   "run_1",
		Prompt:  "", // Empty prompt
		Timeout: 10 * time.Second,
	}
	config := &executor.AgentConfig{
		WorkingDir: t.TempDir(),
	}

	result, err := e.Execute(ctx, task, config, handler)
	require.NoError(t, err)
	require.NotNil(t, result)

	assert.True(t, result.Success)
}

// =============================================================================
// Stream Mode SendMessage Tests
// =============================================================================

func TestExecutor_SendMessage_StreamMode(t *testing.T) {
	// Create a mock script that reads from stdin and outputs to stdout
	script := `#!/bin/bash
# Read all messages from stdin until EOF
while IFS= read -r line; do
    echo "Received: $line" >&2
done
echo '{"type":"result","result":{"num_turns":1}}'
`
	scriptPath := createMockScript(t, script)

	logger := zaptest.NewLogger(t)
	e := New(logger).WithCommandPath(scriptPath)
	handler := NewMockOutputHandler()

	// Create context snapshot to enable stream mode
	contextSnapshot := &ContextSnapshot{
		SessionID:  "sess_stream_test",
		WorkingDir: "/workspace",
	}
	contextBytes, _ := json.Marshal(contextSnapshot)

	ctx := context.Background()
	task := &executor.Task{
		ID:      "task_1",
		RunID:   "run_1",
		Prompt:  "Initial prompt",
		Timeout: 5 * time.Second,
		Context: contextBytes,
	}
	config := &executor.AgentConfig{
		WorkingDir: t.TempDir(),
	}

	done := make(chan *executor.Result)
	go func() {
		result, _ := e.Execute(ctx, task, config, handler)
		done <- result
	}()

	// Wait for task to start and enter stream mode
	time.Sleep(200 * time.Millisecond)

	// Verify stream mode is active
	assert.True(t, e.IsStreamMode())

	// Send a message - should succeed
	err := e.SendMessage([]byte(`{"type":"user","message":{"role":"user","content":"hello"}}`))
	assert.NoError(t, err)

	// Send another message without trailing newline
	err = e.SendMessage([]byte(`{"type":"user"}`))
	assert.NoError(t, err)

	// Clean up
	_ = e.Kill()
	<-done
}

func TestExecutor_SendTextMessage_StreamMode(t *testing.T) {
	// Create a mock script that reads from stdin
	script := `#!/bin/bash
while IFS= read -r line; do
    echo "Got: $line" >&2
done
echo '{"type":"result","result":{"num_turns":1}}'
`
	scriptPath := createMockScript(t, script)

	logger := zaptest.NewLogger(t)
	e := New(logger).WithCommandPath(scriptPath)
	handler := NewMockOutputHandler()

	// Create context snapshot to enable stream mode
	contextSnapshot := &ContextSnapshot{
		SessionID: "sess_text_test",
	}
	contextBytes, _ := json.Marshal(contextSnapshot)

	ctx := context.Background()
	task := &executor.Task{
		ID:      "task_1",
		RunID:   "run_1",
		Prompt:  "Initial",
		Timeout: 5 * time.Second,
		Context: contextBytes,
	}
	config := &executor.AgentConfig{
		WorkingDir: t.TempDir(),
	}

	done := make(chan *executor.Result)
	go func() {
		result, _ := e.Execute(ctx, task, config, handler)
		done <- result
	}()

	// Wait for task to start
	time.Sleep(200 * time.Millisecond)

	// Send text message
	err := e.SendTextMessage("Follow-up question")
	assert.NoError(t, err)

	_ = e.Kill()
	<-done
}

func TestExecutor_SessionID_AfterExecution(t *testing.T) {
	// Create a mock script that outputs stream-json with session ID
	script := `#!/bin/bash
echo '{"type":"init","session_id":"sess_captured_123"}'
echo '{"type":"result","result":{"num_turns":1}}'
`
	scriptPath := createMockScript(t, script)

	logger := zaptest.NewLogger(t)
	e := New(logger).WithCommandPath(scriptPath)
	handler := NewMockOutputHandler()

	ctx := context.Background()
	task := &executor.Task{
		ID:      "task_1",
		RunID:   "run_1",
		Prompt:  "Test",
		Timeout: 10 * time.Second,
	}
	config := &executor.AgentConfig{
		WorkingDir: t.TempDir(),
	}

	result, err := e.Execute(ctx, task, config, handler)
	require.NoError(t, err)
	require.NotNil(t, result)

	// SessionID should be captured
	assert.Equal(t, "sess_captured_123", e.SessionID())
}

func TestExecutor_Execute_NonExitError(t *testing.T) {
	// Test when cmd.Wait returns a non-exit error
	logger := zaptest.NewLogger(t)
	e := New(logger).WithCommandPath("/bin/sh")
	handler := NewMockOutputHandler()

	ctx, cancel := context.WithCancel(context.Background())
	task := &executor.Task{
		ID:      "task_1",
		RunID:   "run_1",
		Prompt:  "-c 'sleep 60'",
		Timeout: 0,
	}
	config := &executor.AgentConfig{
		WorkingDir: t.TempDir(),
	}

	done := make(chan *executor.Result)
	go func() {
		result, _ := e.Execute(ctx, task, config, handler)
		done <- result
	}()

	// Cancel immediately to trigger context error
	time.Sleep(50 * time.Millisecond)
	cancel()

	result := <-done
	require.NotNil(t, result)
	assert.False(t, result.Success)
}

// Test Kill with SIGKILL after SIGTERM timeout
func TestExecutor_Kill_ForcefulAfterTimeout(t *testing.T) {
	// Create a script that ignores SIGTERM
	script := `#!/bin/bash
trap '' SIGTERM  # Ignore SIGTERM
echo "Started"
sleep 60
`
	scriptPath := createMockScript(t, script)

	logger := zaptest.NewLogger(t)
	e := New(logger).WithCommandPath(scriptPath)
	handler := NewMockOutputHandler()

	ctx := context.Background()
	task := &executor.Task{
		ID:      "task_1",
		RunID:   "run_1",
		Prompt:  "Test",
		Timeout: 0,
	}
	config := &executor.AgentConfig{
		WorkingDir: t.TempDir(),
	}

	done := make(chan *executor.Result)
	go func() {
		result, _ := e.Execute(ctx, task, config, handler)
		done <- result
	}()

	// Wait for task to start
	time.Sleep(100 * time.Millisecond)

	// Kill - this should eventually use SIGKILL since SIGTERM is ignored
	err := e.Kill()
	require.NoError(t, err)

	// Wait for completion (should happen within ~6 seconds due to SIGKILL fallback)
	select {
	case result := <-done:
		assert.False(t, result.Success)
	case <-time.After(10 * time.Second):
		t.Fatal("Kill did not terminate the process in time")
	}
}

func TestExecutor_IsStreamMode_DuringExecution(t *testing.T) {
	script := `#!/bin/bash
sleep 0.5
echo '{"type":"result","result":{"num_turns":0}}'
`
	scriptPath := createMockScript(t, script)

	logger := zaptest.NewLogger(t)
	e := New(logger).WithCommandPath(scriptPath)
	handler := NewMockOutputHandler()

	// With context = stream mode
	contextSnapshot := &ContextSnapshot{SessionID: "sess_test"}
	contextBytes, _ := json.Marshal(contextSnapshot)

	ctx := context.Background()
	task := &executor.Task{
		ID:      "task_1",
		RunID:   "run_1",
		Prompt:  "Test",
		Timeout: 5 * time.Second,
		Context: contextBytes,
	}
	config := &executor.AgentConfig{
		WorkingDir: t.TempDir(),
	}

	done := make(chan struct{})
	go func() {
		_, _ = e.Execute(ctx, task, config, handler)
		close(done)
	}()

	// Wait a bit and check stream mode
	time.Sleep(100 * time.Millisecond)
	assert.True(t, e.IsStreamMode())

	<-done
	// After completion, stream mode should be false
	assert.False(t, e.IsStreamMode())
}
