//go:build integration

package claude

import (
	"context"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/chunlea/marionette/pkg/agent/executor"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// integrationOutputHandler captures output for integration testing.
type integrationOutputHandler struct {
	mu       sync.Mutex
	outputs  []integrationOutput
	events   []*executor.AgentEvent
	finished chan struct{}
}

type integrationOutput struct {
	stream string
	data   string
}

func newIntegrationOutputHandler() *integrationOutputHandler {
	return &integrationOutputHandler{
		finished: make(chan struct{}),
	}
}

func (h *integrationOutputHandler) HandleOutput(stream string, data []byte) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.outputs = append(h.outputs, integrationOutput{stream: stream, data: string(data)})
}

func (h *integrationOutputHandler) HandlePermissionRequest(_ context.Context, _ *executor.PermissionRequest) (bool, error) {
	// Auto-approve for integration tests
	return true, nil
}

func (h *integrationOutputHandler) getOutputs() []integrationOutput {
	h.mu.Lock()
	defer h.mu.Unlock()
	result := make([]integrationOutput, len(h.outputs))
	copy(result, h.outputs)
	return result
}

func (h *integrationOutputHandler) hasStream(stream string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, o := range h.outputs {
		if o.stream == stream {
			return true
		}
	}
	return false
}

// skipIfNoToken skips the test if CLAUDE_CODE_OAUTH_TOKEN is not set.
func skipIfNoToken(t *testing.T) {
	t.Helper()
	if os.Getenv("CLAUDE_CODE_OAUTH_TOKEN") == "" {
		t.Skip("CLAUDE_CODE_OAUTH_TOKEN not set, skipping integration test")
	}
}

// TestIntegration_Execute_SimplePrompt tests a simple prompt execution with real Claude CLI.
func TestIntegration_Execute_SimplePrompt(t *testing.T) {
	skipIfNoToken(t)

	e := New()
	handler := newIntegrationOutputHandler()

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	task := &executor.Task{
		ID:      "test-task-1",
		Prompt:  "What is 2+2? Reply with just the number.",
		Timeout: 90 * time.Second,
	}

	result, err := e.Execute(ctx, task, nil, handler)

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.True(t, result.Success, "Task should succeed, error: %s", result.Error)
	assert.Equal(t, 0, result.ExitCode)

	// Verify we got output
	outputs := handler.getOutputs()
	assert.NotEmpty(t, outputs, "Should have received output")

	// Should have stdout output
	assert.True(t, handler.hasStream("stdout"), "Should have stdout output")

	// Should have system start message
	assert.True(t, handler.hasStream("system"), "Should have system output")

	// Session ID should be captured
	assert.NotEmpty(t, result.AgentSession, "Should capture session ID")

	t.Logf("Result: success=%v, exitCode=%d, sessionID=%s", result.Success, result.ExitCode, result.AgentSession)
	t.Logf("Output count: %d", len(outputs))
}

// TestIntegration_Execute_WithModel tests execution with a specific model.
func TestIntegration_Execute_WithModel(t *testing.T) {
	skipIfNoToken(t)

	e := New()
	handler := newIntegrationOutputHandler()

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	task := &executor.Task{
		ID:      "test-task-2",
		Prompt:  "Say 'hello' and nothing else.",
		Timeout: 90 * time.Second,
	}

	config := &executor.AgentConfig{
		Model: "claude-sonnet-4-20250514",
	}

	result, err := e.Execute(ctx, task, config, handler)

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.True(t, result.Success, "Task should succeed, error: %s", result.Error)
}

// TestIntegration_Execute_Timeout tests that timeout is properly enforced.
func TestIntegration_Execute_Timeout(t *testing.T) {
	skipIfNoToken(t)

	e := New()
	handler := newIntegrationOutputHandler()

	ctx := context.Background()

	// Very short timeout that should trigger before completion
	task := &executor.Task{
		ID:      "test-task-timeout",
		Prompt:  "Write a detailed 5000 word essay about the history of computing.",
		Timeout: 2 * time.Second, // Very short timeout
	}

	result, err := e.Execute(ctx, task, nil, handler)

	require.NoError(t, err) // Execute itself shouldn't error
	require.NotNil(t, result)
	assert.False(t, result.Success, "Task should fail due to timeout")
	assert.Contains(t, result.Error, "timeout", "Error should mention timeout")
}

// TestIntegration_Execute_ContextCancel tests that context cancellation works.
func TestIntegration_Execute_ContextCancel(t *testing.T) {
	skipIfNoToken(t)

	e := New()
	handler := newIntegrationOutputHandler()

	ctx, cancel := context.WithCancel(context.Background())

	task := &executor.Task{
		ID:      "test-task-cancel",
		Prompt:  "Write a detailed 5000 word essay about the history of computing.",
		Timeout: 60 * time.Second,
	}

	// Cancel after a short delay
	go func() {
		time.Sleep(2 * time.Second)
		cancel()
	}()

	result, err := e.Execute(ctx, task, nil, handler)

	require.NoError(t, err) // Execute itself shouldn't error
	require.NotNil(t, result)
	assert.False(t, result.Success, "Task should fail due to cancellation")
	assert.Contains(t, result.Error, "canceled", "Error should mention canceled")
}

// TestIntegration_Kill_RunningProcess tests killing a running process.
func TestIntegration_Kill_RunningProcess(t *testing.T) {
	skipIfNoToken(t)

	e := New()
	handler := newIntegrationOutputHandler()

	ctx := context.Background()

	task := &executor.Task{
		ID:      "test-task-kill",
		Prompt:  "Write a detailed 5000 word essay about the history of computing.",
		Timeout: 60 * time.Second,
	}

	// Start execution in goroutine
	resultCh := make(chan *executor.Result, 1)
	errCh := make(chan error, 1)
	started := make(chan struct{})

	go func() {
		// Signal that we're about to start
		close(started)
		result, err := e.Execute(ctx, task, nil, handler)
		resultCh <- result
		errCh <- err
	}()

	// Wait for goroutine to start
	<-started

	// Wait a bit for process to actually start and produce some output
	time.Sleep(3 * time.Second)

	// Kill the process
	err := e.Kill()
	require.NoError(t, err)

	// Wait for execution to complete
	select {
	case result := <-resultCh:
		assert.NotNil(t, result)
		// Process was killed via context cancel, so error should be "canceled"
		assert.False(t, result.Success, "Task should not succeed after kill")
		assert.Equal(t, "canceled", result.Error, "Error should be 'canceled' after kill")
		t.Logf("Result after kill: success=%v, exitCode=%d, error=%s", result.Success, result.ExitCode, result.Error)
	case <-time.After(10 * time.Second):
		t.Fatal("Execution did not complete after kill")
	}
}

// TestIntegration_Execute_WorkingDirectory tests setting working directory.
func TestIntegration_Execute_WorkingDirectory(t *testing.T) {
	skipIfNoToken(t)

	e := New()
	handler := newIntegrationOutputHandler()

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	// Use temp directory as working directory
	tmpDir := t.TempDir()

	task := &executor.Task{
		ID:         "test-task-workdir",
		Prompt:     "What is your current working directory? Reply with just the path.",
		Timeout:    90 * time.Second,
		WorkingDir: tmpDir,
	}

	result, err := e.Execute(ctx, task, nil, handler)

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.True(t, result.Success, "Task should succeed, error: %s", result.Error)

	t.Logf("Working directory test completed, tmpDir=%s", tmpDir)
}

// TestIntegration_Parser_SessionIDTracking tests that session ID is tracked across messages.
func TestIntegration_Parser_SessionIDTracking(t *testing.T) {
	skipIfNoToken(t)

	e := New()
	handler := newIntegrationOutputHandler()

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	task := &executor.Task{
		ID:      "test-task-session",
		Prompt:  "Say 'test' and nothing else.",
		Timeout: 90 * time.Second,
	}

	result, err := e.Execute(ctx, task, nil, handler)

	require.NoError(t, err)
	require.NotNil(t, result)

	// The parser should have captured the session ID
	assert.NotEmpty(t, result.AgentSession, "Session ID should be captured from result message")
	t.Logf("Captured session ID: %s", result.AgentSession)
}
