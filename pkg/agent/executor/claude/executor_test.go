package claude

import (
	"context"
	"errors"
	"io"
	"os/exec"
	"strings"
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

	// Permission request tracking
	permissionRequests []*executor.PermissionRequest
	permissionApprove  bool
	permissionErr      error
}

type outputRecord struct {
	stream string
	data   []byte
}

func newTestOutputHandler() *testOutputHandler {
	return &testOutputHandler{
		permissionApprove: true, // Default to auto-approve
	}
}

func (h *testOutputHandler) HandleOutput(stream string, data []byte) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.outputs = append(h.outputs, outputRecord{stream: stream, data: append([]byte{}, data...)})
}

func (h *testOutputHandler) HandlePermissionRequest(_ context.Context, req *executor.PermissionRequest) (bool, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.permissionRequests = append(h.permissionRequests, req)
	return h.permissionApprove, h.permissionErr
}

func (h *testOutputHandler) GetPermissionRequests() []*executor.PermissionRequest {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.permissionRequests
}

func (h *testOutputHandler) GetOutputs() []outputRecord {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.outputs
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
	assert.Contains(t, args, "--permission-mode")
	assert.Contains(t, args, "acceptEdits")
	assert.Contains(t, args, "--print")
	assert.Contains(t, args, "Hello, Claude!")
}

func TestExecutor_buildArgs_WithWorkingDir(t *testing.T) {
	e := New()

	task := &executor.Task{
		Prompt:     "Test",
		WorkingDir: "/workspace/project",
	}

	args := e.buildArgs(task, nil)

	assert.Contains(t, args, "--add-dir")
	assert.Contains(t, args, "/workspace/project")
}

func TestExecutor_buildArgs_WithWorkingDirFromConfig(t *testing.T) {
	e := New()

	task := &executor.Task{
		Prompt: "Test",
		// No WorkingDir in task
	}
	config := &executor.AgentConfig{
		WorkingDir: "/config/workspace",
	}

	args := e.buildArgs(task, config)

	assert.Contains(t, args, "--add-dir")
	assert.Contains(t, args, "/config/workspace")
}

func TestExecutor_buildArgs_TaskWorkingDirOverridesConfig(t *testing.T) {
	e := New()

	task := &executor.Task{
		Prompt:     "Test",
		WorkingDir: "/task/workspace",
	}
	config := &executor.AgentConfig{
		WorkingDir: "/config/workspace",
	}

	args := e.buildArgs(task, config)

	// Task WorkingDir should take precedence
	assert.Contains(t, args, "--add-dir")
	assert.Contains(t, args, "/task/workspace")
	assert.NotContains(t, args, "/config/workspace")
}

func TestExecutor_buildArgs_NoWorkingDir(t *testing.T) {
	e := New()

	task := &executor.Task{
		Prompt: "Test",
		// No WorkingDir
	}

	args := e.buildArgs(task, nil)

	// Should not contain --add-dir if no working dir
	assert.NotContains(t, args, "--add-dir")
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

func TestExecutor_processOutput_PermissionRequest(t *testing.T) {
	e := New()
	e.parser = NewParser().(*Parser)

	ctx := context.Background()
	handler := newTestOutputHandler()

	// Simulate Claude output with a tool_use event for Bash (permission required)
	toolUseJSON := `{"type":"assistant","message":{"content":[{"type":"tool_use","id":"toolu_123","name":"Bash","input":"{\"command\":\"ls -la\"}"}]}}`

	reader := strings.NewReader(toolUseJSON + "\n")
	e.processOutput(ctx, reader, "stdout", handler)

	// Verify permission request was made
	requests := handler.GetPermissionRequests()
	require.Len(t, requests, 1)
	assert.Equal(t, "toolu_123", requests[0].ID)
	assert.Equal(t, "Bash", requests[0].Tool)
	assert.Contains(t, requests[0].Action, "ls -la")
	assert.Equal(t, executor.RiskMedium, requests[0].RiskLevel)
}

func TestExecutor_processOutput_PermissionApproved(t *testing.T) {
	e := New()
	e.parser = NewParser().(*Parser)

	ctx := context.Background()
	handler := newTestOutputHandler()
	handler.permissionApprove = true

	// Simulate Claude output with a tool_use event
	toolUseJSON := `{"type":"assistant","message":{"content":[{"type":"tool_use","id":"toolu_456","name":"Write","input":"{\"file_path\":\"/tmp/test.txt\"}"}]}}`

	reader := strings.NewReader(toolUseJSON + "\n")
	e.processOutput(ctx, reader, "stdout", handler)

	// Verify permission was approved
	outputs := handler.GetOutputs()
	found := false
	for _, o := range outputs {
		if strings.Contains(string(o.data), "permission_approved: Write toolu_456") {
			found = true
			break
		}
	}
	assert.True(t, found, "Should emit permission_approved system message")
}

func TestExecutor_processOutput_PermissionDenied(t *testing.T) {
	e := New()
	e.parser = NewParser().(*Parser)

	ctx := context.Background()
	handler := newTestOutputHandler()
	handler.permissionApprove = false

	// Simulate Claude output with a tool_use event
	toolUseJSON := `{"type":"assistant","message":{"content":[{"type":"tool_use","id":"toolu_789","name":"Edit","input":"{\"file_path\":\"/etc/passwd\"}"}]}}`

	reader := strings.NewReader(toolUseJSON + "\n")
	e.processOutput(ctx, reader, "stdout", handler)

	// Verify permission was denied
	outputs := handler.GetOutputs()
	found := false
	for _, o := range outputs {
		if strings.Contains(string(o.data), "permission_denied: Edit toolu_789") {
			found = true
			break
		}
	}
	assert.True(t, found, "Should emit permission_denied system message")
}

func TestExecutor_processOutput_PermissionError(t *testing.T) {
	e := New()
	e.parser = NewParser().(*Parser)

	ctx := context.Background()
	handler := newTestOutputHandler()
	handler.permissionErr = errors.New("context canceled")

	// Simulate Claude output with a tool_use event
	toolUseJSON := `{"type":"assistant","message":{"content":[{"type":"tool_use","id":"toolu_err","name":"Bash","input":"{}"}]}}`

	reader := strings.NewReader(toolUseJSON + "\n")
	e.processOutput(ctx, reader, "stdout", handler)

	// Verify error message was emitted
	outputs := handler.GetOutputs()
	found := false
	for _, o := range outputs {
		if strings.Contains(string(o.data), "permission_request_error") {
			found = true
			break
		}
	}
	assert.True(t, found, "Should emit permission_request_error system message")
}

func TestExecutor_processOutput_NoPermissionForReadTools(t *testing.T) {
	e := New()
	e.parser = NewParser().(*Parser)

	ctx := context.Background()
	handler := newTestOutputHandler()

	// Simulate Claude output with a tool_use event for Read (no permission required)
	toolUseJSON := `{"type":"assistant","message":{"content":[{"type":"tool_use","id":"toolu_read","name":"Read","input":"{\"file_path\":\"/tmp/test.txt\"}"}]}}`

	reader := strings.NewReader(toolUseJSON + "\n")
	e.processOutput(ctx, reader, "stdout", handler)

	// Verify no permission request was made
	requests := handler.GetPermissionRequests()
	assert.Len(t, requests, 0, "Read tool should not require permission")
}

func TestExecutor_processOutput_MultipleToolUses(t *testing.T) {
	e := New()
	e.parser = NewParser().(*Parser)

	ctx := context.Background()
	handler := newTestOutputHandler()
	handler.permissionApprove = true

	// Simulate multiple tool uses - one requiring permission, one not
	bashJSON := `{"type":"assistant","message":{"content":[{"type":"tool_use","id":"toolu_1","name":"Bash","input":"{}"}]}}`
	readJSON := `{"type":"assistant","message":{"content":[{"type":"tool_use","id":"toolu_2","name":"Read","input":"{}"}]}}`
	writeJSON := `{"type":"assistant","message":{"content":[{"type":"tool_use","id":"toolu_3","name":"Write","input":"{}"}]}}`

	reader := strings.NewReader(bashJSON + "\n" + readJSON + "\n" + writeJSON + "\n")
	e.processOutput(ctx, reader, "stdout", handler)

	// Verify only Bash and Write triggered permission requests
	requests := handler.GetPermissionRequests()
	require.Len(t, requests, 2)
	assert.Equal(t, "Bash", requests[0].Tool)
	assert.Equal(t, "Write", requests[1].Tool)
}

func TestExecutor_processOutput_ContextCanceled(t *testing.T) {
	e := New()
	e.parser = NewParser().(*Parser)

	ctx, cancel := context.WithCancel(context.Background())
	handler := newTestOutputHandler()

	// Create a pipe - we'll close writer to trigger EOF
	reader, writer := io.Pipe()

	// This should return when we cancel context and close the writer
	done := make(chan struct{})
	go func() {
		e.processOutput(ctx, reader, "stdout", handler)
		close(done)
	}()

	// Give the goroutine time to start
	time.Sleep(10 * time.Millisecond)

	// Cancel context and close writer to unblock the scanner
	cancel()
	_ = writer.Close()

	select {
	case <-done:
		// Good - processOutput returned
		_ = reader.Close()
	case <-time.After(1 * time.Second):
		_ = reader.Close()
		_ = writer.Close()
		t.Fatal("processOutput did not return after context cancellation and pipe close")
	}
}

func TestExecutor_processOutput_PermissionRequestStopsOnError(t *testing.T) {
	e := New()
	e.parser = NewParser().(*Parser)

	// Use a cancelable context
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	handler := newTestOutputHandler()
	handler.permissionErr = context.Canceled

	// Simulate two tool uses that require permission
	// After the first error, processing should stop
	bashJSON1 := `{"type":"assistant","message":{"content":[{"type":"tool_use","id":"toolu_1","name":"Bash","input":"{}"}]}}`
	bashJSON2 := `{"type":"assistant","message":{"content":[{"type":"tool_use","id":"toolu_2","name":"Bash","input":"{}"}]}}`

	reader := strings.NewReader(bashJSON1 + "\n" + bashJSON2 + "\n")
	e.processOutput(ctx, reader, "stdout", handler)

	// Should only have one permission request since processing stops on error
	requests := handler.GetPermissionRequests()
	assert.Len(t, requests, 1, "Processing should stop after permission error")
}

func TestExecutor_processStderr(t *testing.T) {
	e := New()

	ctx := context.Background()
	handler := newTestOutputHandler()

	// Simulate stderr output
	stderrContent := "Error: something went wrong\nWarning: deprecated feature\n"
	reader := strings.NewReader(stderrContent)

	e.processStderr(ctx, reader, handler)

	// Verify stderr outputs were captured
	outputs := handler.GetOutputs()
	require.Len(t, outputs, 2)
	assert.Equal(t, "stderr", outputs[0].stream)
	assert.Contains(t, string(outputs[0].data), "Error: something went wrong")
	assert.Equal(t, "stderr", outputs[1].stream)
	assert.Contains(t, string(outputs[1].data), "Warning: deprecated feature")
}

func TestExecutor_processStderr_EmptyLines(t *testing.T) {
	e := New()

	ctx := context.Background()
	handler := newTestOutputHandler()

	// Simulate stderr with empty lines
	stderrContent := "Error message\n\n\nAnother error\n"
	reader := strings.NewReader(stderrContent)

	e.processStderr(ctx, reader, handler)

	// Only non-empty lines should be captured
	outputs := handler.GetOutputs()
	require.Len(t, outputs, 2)
	assert.Contains(t, string(outputs[0].data), "Error message")
	assert.Contains(t, string(outputs[1].data), "Another error")
}

func TestExecutor_processStderr_ContextCanceled(t *testing.T) {
	e := New()

	ctx, cancel := context.WithCancel(context.Background())
	handler := newTestOutputHandler()

	// Create a pipe
	reader, writer := io.Pipe()

	done := make(chan struct{})
	go func() {
		e.processStderr(ctx, reader, handler)
		close(done)
	}()

	// Give the goroutine time to start
	time.Sleep(10 * time.Millisecond)

	// Cancel context and close writer
	cancel()
	_ = writer.Close()

	select {
	case <-done:
		_ = reader.Close()
	case <-time.After(1 * time.Second):
		_ = reader.Close()
		_ = writer.Close()
		t.Fatal("processStderr did not return after context cancellation")
	}
}

func TestExecutor_Kill_WhileRunning(t *testing.T) {
	e := New()

	// Simulate a running state with a real cancelFunc and a dummy cmd
	// Note: Kill returns early if cmd is nil (see executor.go:387)
	ctx, cancel := context.WithCancel(context.Background())
	dummyCmd := exec.Command("echo", "test")

	e.mu.Lock()
	e.running = true
	e.cancelFunc = cancel
	e.cmd = dummyCmd
	e.mu.Unlock()

	// Kill should cancel the context
	err := e.Kill()
	// Kill on a non-started process returns an error, which is expected
	// The important thing is that the context was canceled
	_ = err

	// Verify context was canceled
	select {
	case <-ctx.Done():
		// Good - context was canceled
	default:
		t.Fatal("Kill did not cancel context")
	}

	// Cleanup
	e.mu.Lock()
	e.running = false
	e.cancelFunc = nil
	e.cmd = nil
	e.mu.Unlock()
}

func TestExecutor_SendMessage_Success(t *testing.T) {
	e := New()

	// Create a pipe for stdin simulation
	reader, writer := io.Pipe()

	// Simulate running stream mode
	e.mu.Lock()
	e.running = true
	e.streamMode = true
	e.stdin = writer
	e.mu.Unlock()

	// Send message in goroutine
	done := make(chan error)
	go func() {
		done <- e.SendMessage([]byte("test message"))
	}()

	// Read from the pipe
	buf := make([]byte, 1024)
	n, err := reader.Read(buf)
	assert.NoError(t, err)
	assert.Equal(t, "test message\n", string(buf[:n]))

	// Check SendMessage returned without error
	err = <-done
	assert.NoError(t, err)

	// Cleanup
	_ = writer.Close()
	_ = reader.Close()
	e.mu.Lock()
	e.running = false
	e.streamMode = false
	e.stdin = nil
	e.mu.Unlock()
}

func TestExecutor_SendMessage_NilStdin(t *testing.T) {
	e := New()

	// Simulate running stream mode but nil stdin
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

func TestExecutor_processOutput_EmptyLines(t *testing.T) {
	e := New()
	e.parser = NewParser().(*Parser)

	ctx := context.Background()
	handler := newTestOutputHandler()

	// Simulate output with empty lines
	output := "\n\n{\"type\":\"system\",\"data\":\"Hello\"}\n\n"
	reader := strings.NewReader(output)

	e.processOutput(ctx, reader, "stdout", handler)

	// Should skip empty lines
	outputs := handler.GetOutputs()
	assert.Len(t, outputs, 1)
}

func TestExecutor_processOutput_ParseError(t *testing.T) {
	e := New()
	e.parser = NewParser().(*Parser)

	ctx := context.Background()
	handler := newTestOutputHandler()

	// Simulate output that's not valid JSON (but not empty)
	output := "Invalid JSON here\n"
	reader := strings.NewReader(output)

	e.processOutput(ctx, reader, "stdout", handler)

	// Parser handles invalid JSON gracefully (returns text event)
	// The raw output should still be sent to handler
	outputs := handler.GetOutputs()
	assert.GreaterOrEqual(t, len(outputs), 1)
}
