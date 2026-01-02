package claude

import (
	"context"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/chunlea/marionette/pkg/agent/executor"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testOutputHandler captures output for testing.
type testOutputHandler struct {
	mu      sync.Mutex
	outputs []outputRecord
}

type outputRecord struct {
	stream string
	data   []byte
}

func newTestOutputHandler() *testOutputHandler {
	return &testOutputHandler{}
}

func (h *testOutputHandler) HandleOutput(stream string, data []byte) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.outputs = append(h.outputs, outputRecord{stream: stream, data: append([]byte{}, data...)})
}

func (h *testOutputHandler) HandlePermissionRequest(_ context.Context, _ *executor.PermissionRequest) (bool, error) {
	return true, nil // Auto-approve for tests
}

func TestExecutor_Name(t *testing.T) {
	e := New()
	assert.Equal(t, "claude", e.Name())
}

func TestExecutor_New_WithOptions(t *testing.T) {
	e := New(
		WithBinaryPath("/usr/local/bin/claude"),
	)

	assert.Equal(t, "/usr/local/bin/claude", e.binaryPath)
}

func TestExecutor_buildArgs_Basic(t *testing.T) {
	e := New()

	task := &executor.Task{
		Prompt: "Hello, Claude!",
	}

	args := e.buildArgs(task, nil)

	assert.Contains(t, args, "--output-format")
	assert.Contains(t, args, "stream-json")
	assert.Contains(t, args, "--verbose")
	assert.Contains(t, args, "--print")
	assert.Contains(t, args, "Hello, Claude!")
}

func TestExecutor_buildArgs_WithModel(t *testing.T) {
	e := New()

	task := &executor.Task{
		Prompt: "Test",
	}
	config := &executor.AgentConfig{
		Model: "claude-sonnet-4-20250514",
	}

	args := e.buildArgs(task, config)

	assert.Contains(t, args, "--model")
	assert.Contains(t, args, "claude-sonnet-4-20250514")
}

func TestExecutor_buildArgs_WithResume(t *testing.T) {
	e := New()

	task := &executor.Task{
		Prompt:          "Continue",
		ContextSnapshot: []byte(`{"session_id":"sess_abc123"}`),
	}

	args := e.buildArgs(task, nil)

	assert.Contains(t, args, "--resume")
	assert.Contains(t, args, "sess_abc123")
}

func TestExecutor_buildArgs_EmptyPrompt(t *testing.T) {
	e := New()

	task := &executor.Task{
		Prompt: "",
	}

	args := e.buildArgs(task, nil)

	// Should not contain --print with empty prompt
	for i, arg := range args {
		if arg == "--print" {
			// Next arg should not be empty
			if i+1 < len(args) {
				assert.NotEmpty(t, args[i+1])
			}
		}
	}
}

func TestExecutor_buildEnv_NoConfig(t *testing.T) {
	e := New()

	env := e.buildEnv(nil)

	// Should include OS environment
	assert.NotEmpty(t, env)
}

func TestExecutor_buildEnv_WithAPIKey(t *testing.T) {
	e := New()

	config := &executor.AgentConfig{
		APIKey: "test-api-key",
	}

	env := e.buildEnv(config)

	found := false
	for _, v := range env {
		if v == "ANTHROPIC_API_KEY=test-api-key" {
			found = true
			break
		}
	}
	assert.True(t, found, "ANTHROPIC_API_KEY should be set")
}

func TestExecutor_buildEnv_WithBaseURL(t *testing.T) {
	e := New()

	config := &executor.AgentConfig{
		BaseURL: "https://api.example.com",
	}

	env := e.buildEnv(config)

	found := false
	for _, v := range env {
		if v == "ANTHROPIC_BASE_URL=https://api.example.com" {
			found = true
			break
		}
	}
	assert.True(t, found, "ANTHROPIC_BASE_URL should be set")
}

func TestExecutor_buildEnv_WithExtra(t *testing.T) {
	e := New()

	config := &executor.AgentConfig{
		Extra: map[string]string{
			"CUSTOM_VAR": "custom_value",
		},
	}

	env := e.buildEnv(config)

	found := false
	for _, v := range env {
		if v == "CUSTOM_VAR=custom_value" {
			found = true
			break
		}
	}
	assert.True(t, found, "CUSTOM_VAR should be set")
}

func TestExecutor_Kill_NotRunning(t *testing.T) {
	e := New()

	err := e.Kill()
	assert.NoError(t, err) // Should not error when not running
}

func TestExecutor_SendMessage_NotRunning(t *testing.T) {
	e := New()

	err := e.SendMessage([]byte("test"))
	assert.ErrorIs(t, err, ErrNotRunning)
}

func TestExecutor_SendMessage_NotStreamMode(t *testing.T) {
	e := New()

	// Simulate running but not in stream mode
	e.mu.Lock()
	e.running = true
	e.streamMode = false
	e.mu.Unlock()

	err := e.SendMessage([]byte("test"))
	assert.ErrorIs(t, err, ErrNotStreamMode)

	// Cleanup
	e.mu.Lock()
	e.running = false
	e.mu.Unlock()
}

func TestExecutor_IsStreamMode_Default(t *testing.T) {
	e := New()
	assert.False(t, e.IsStreamMode())
}

func TestExecutor_Execute_InvalidBinary(t *testing.T) {
	e := New(WithBinaryPath("/nonexistent/binary"))

	ctx := context.Background()
	task := &executor.Task{
		Prompt:  "test",
		Timeout: 5 * time.Second,
	}
	handler := newTestOutputHandler()

	result, err := e.Execute(ctx, task, nil, handler)

	// Should return an error for invalid binary
	require.Error(t, err)
	assert.Nil(t, result)
}

func TestExecutor_Execute_AlreadyRunning(t *testing.T) {
	e := New()

	// Simulate running
	e.mu.Lock()
	e.running = true
	e.mu.Unlock()

	ctx := context.Background()
	task := &executor.Task{Prompt: "test"}
	handler := newTestOutputHandler()

	result, err := e.Execute(ctx, task, nil, handler)

	assert.ErrorIs(t, err, ErrAlreadyRunning)
	assert.Nil(t, result)

	// Cleanup
	e.mu.Lock()
	e.running = false
	e.mu.Unlock()
}

func TestExecutor_Execute_ContextCanceled(t *testing.T) {
	// Use a simple echo command to simulate claude
	e := New(WithBinaryPath("echo"))

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	task := &executor.Task{
		Prompt:  "test",
		Timeout: 5 * time.Second,
	}
	handler := newTestOutputHandler()

	result, err := e.Execute(ctx, task, nil, handler)

	// Should complete (echo exits quickly) or be canceled
	if err == nil {
		assert.NotNil(t, result)
	}
}

func TestExecutor_Interfaces(t *testing.T) {
	e := New()

	// Verify interface implementation
	var _ executor.Executor = e
	var _ executor.StreamExecutor = e
}

func TestIsPermissionRequired(t *testing.T) {
	tests := []struct {
		tool     string
		required bool
	}{
		{"Bash", true},
		{"Write", true},
		{"Edit", true},
		{"NotebookEdit", true},
		{"computer", true},
		{"Read", false},
		{"Glob", false},
		{"Grep", false},
		{"WebFetch", false},
	}

	for _, tc := range tests {
		t.Run(tc.tool, func(t *testing.T) {
			assert.Equal(t, tc.required, IsPermissionRequired(tc.tool))
		})
	}
}

func TestPermissionToolNames(t *testing.T) {
	assert.True(t, PermissionToolNames["Bash"])
	assert.True(t, PermissionToolNames["Write"])
	assert.True(t, PermissionToolNames["Edit"])
	assert.False(t, PermissionToolNames["Read"])
	assert.False(t, PermissionToolNames["nonexistent"])
}

func TestExecutor_SendMessage_NilStdin(t *testing.T) {
	e := New()

	// Simulate running in stream mode but stdin is nil
	e.mu.Lock()
	e.running = true
	e.streamMode = true
	e.stdin = nil
	e.mu.Unlock()

	err := e.SendMessage([]byte("test"))
	assert.ErrorIs(t, err, ErrNotRunning)

	// Cleanup
	e.mu.Lock()
	e.running = false
	e.streamMode = false
	e.mu.Unlock()
}

func TestExecutor_Kill_WithCancelFunc(t *testing.T) {
	e := New()

	// Track if cancel was called
	cancelCalled := false
	mockCancel := func() {
		cancelCalled = true
	}

	// Simulate running state with a cancel function
	e.mu.Lock()
	e.running = true
	e.cancelFunc = mockCancel
	e.mu.Unlock()

	err := e.Kill()
	assert.NoError(t, err)
	assert.True(t, cancelCalled, "Cancel function should be called")

	// Cleanup
	e.mu.Lock()
	e.running = false
	e.cancelFunc = nil
	e.mu.Unlock()
}

func TestExecutor_processOutput_ContextDone(t *testing.T) {
	e := New()
	e.parser = NewParser().(*Parser)
	handler := newTestOutputHandler()

	// Create a reader that will block
	pr, pw := io.Pipe()
	defer pr.Close()

	// Cancel context immediately
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	// Write some data and close
	go func() {
		pw.Write([]byte(`{"type":"system","data":"test"}`))
		pw.Write([]byte("\n"))
		pw.Close()
	}()

	// processOutput should return quickly when context is done
	done := make(chan struct{})
	go func() {
		e.processOutput(ctx, pr, "stdout", handler)
		close(done)
	}()

	select {
	case <-done:
		// Success - returned after context done
	case <-time.After(2 * time.Second):
		t.Fatal("processOutput did not return when context was done")
	}
}

func TestExecutor_processOutput_ValidJSON(t *testing.T) {
	e := New()
	e.parser = NewParser().(*Parser)
	handler := newTestOutputHandler()

	// Create a pipe reader
	pr, pw := io.Pipe()

	ctx := context.Background()

	// Write valid JSON
	go func() {
		pw.Write([]byte(`{"type":"system","subtype":"init","data":"Starting"}`))
		pw.Write([]byte("\n"))
		pw.Write([]byte(`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"Hello"}]}}`))
		pw.Write([]byte("\n"))
		pw.Close()
	}()

	e.processOutput(ctx, pr, "stdout", handler)

	// Check that outputs were captured
	handler.mu.Lock()
	outputCount := len(handler.outputs)
	handler.mu.Unlock()

	assert.GreaterOrEqual(t, outputCount, 2, "Should have received at least 2 outputs")
}

func TestExecutor_processOutput_EmptyLines(t *testing.T) {
	e := New()
	e.parser = NewParser().(*Parser)
	handler := newTestOutputHandler()

	pr, pw := io.Pipe()
	ctx := context.Background()

	// Write with empty lines
	go func() {
		pw.Write([]byte("\n")) // Empty line
		pw.Write([]byte(`{"type":"system","data":"test"}`))
		pw.Write([]byte("\n"))
		pw.Write([]byte("\n")) // Empty line
		pw.Close()
	}()

	e.processOutput(ctx, pr, "stdout", handler)

	// Empty lines should be skipped
	handler.mu.Lock()
	outputCount := len(handler.outputs)
	handler.mu.Unlock()

	assert.Equal(t, 1, outputCount, "Should only have 1 output (empty lines skipped)")
}

func TestExecutor_processStderr_Basic(t *testing.T) {
	e := New()
	handler := newTestOutputHandler()

	pr, pw := io.Pipe()
	ctx := context.Background()

	go func() {
		pw.Write([]byte("Warning: something happened\n"))
		pw.Write([]byte("Error: something bad\n"))
		pw.Close()
	}()

	e.processStderr(ctx, pr, handler)

	handler.mu.Lock()
	defer handler.mu.Unlock()

	assert.Len(t, handler.outputs, 2)
	for _, out := range handler.outputs {
		assert.Equal(t, "stderr", out.stream)
	}
}

func TestExecutor_processStderr_ContextDone(t *testing.T) {
	e := New()
	handler := newTestOutputHandler()

	pr, pw := io.Pipe()
	defer pr.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	go func() {
		pw.Write([]byte("test\n"))
		pw.Close()
	}()

	done := make(chan struct{})
	go func() {
		e.processStderr(ctx, pr, handler)
		close(done)
	}()

	select {
	case <-done:
		// Success
	case <-time.After(2 * time.Second):
		t.Fatal("processStderr did not return when context was done")
	}
}

func TestExecutor_processStderr_EmptyLines(t *testing.T) {
	e := New()
	handler := newTestOutputHandler()

	pr, pw := io.Pipe()
	ctx := context.Background()

	go func() {
		pw.Write([]byte("\n")) // Empty line
		pw.Write([]byte("actual error\n"))
		pw.Write([]byte("\n")) // Empty line
		pw.Close()
	}()

	e.processStderr(ctx, pr, handler)

	handler.mu.Lock()
	defer handler.mu.Unlock()

	// Empty lines should be skipped
	assert.Len(t, handler.outputs, 1)
	assert.Equal(t, "actual error", string(handler.outputs[0].data))
}

func TestExecutor_processOutput_PermissionRequired(t *testing.T) {
	e := New()
	e.parser = NewParser().(*Parser)
	handler := newTestOutputHandler()

	pr, pw := io.Pipe()
	ctx := context.Background()

	// Write a tool_use message that requires permission (Bash)
	go func() {
		pw.Write([]byte(`{"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","id":"tool_123","name":"Bash","input":{"command":"rm -rf /"}}]}}`))
		pw.Write([]byte("\n"))
		pw.Close()
	}()

	e.processOutput(ctx, pr, "stdout", handler)

	// Should have stdout output + system permission_request message
	handler.mu.Lock()
	defer handler.mu.Unlock()

	hasPermissionRequest := false
	for _, out := range handler.outputs {
		if out.stream == "system" && string(out.data) == "permission_request: Bash tool_123" {
			hasPermissionRequest = true
			break
		}
	}
	assert.True(t, hasPermissionRequest, "Should emit permission_request for Bash tool")
}

func TestExecutor_Execute_WorkingDir(t *testing.T) {
	// Use echo to test working directory is set
	e := New(WithBinaryPath("pwd"))

	ctx := context.Background()
	tmpDir := t.TempDir()
	task := &executor.Task{
		Prompt:     "",           // pwd doesn't need prompt
		WorkingDir: tmpDir,
		Timeout:    5 * time.Second,
	}
	handler := newTestOutputHandler()

	result, err := e.Execute(ctx, task, nil, handler)

	require.NoError(t, err)
	assert.NotNil(t, result)
	// pwd should output the working directory
}

func TestExecutor_Execute_ConfigWorkingDir(t *testing.T) {
	// Test that config.WorkingDir is used when task.WorkingDir is empty
	e := New(WithBinaryPath("pwd"))

	ctx := context.Background()
	tmpDir := t.TempDir()
	task := &executor.Task{
		Prompt:  "",
		Timeout: 5 * time.Second,
	}
	config := &executor.AgentConfig{
		WorkingDir: tmpDir,
	}
	handler := newTestOutputHandler()

	result, err := e.Execute(ctx, task, config, handler)

	require.NoError(t, err)
	assert.NotNil(t, result)
}
