package claude

import (
	"context"
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
