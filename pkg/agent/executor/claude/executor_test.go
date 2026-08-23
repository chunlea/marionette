package claude

import (
	"context"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chunlea/marionette/pkg/agent/executor"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMain ensures the /tmp/claude directory exists for mock script tests.
func TestMain(m *testing.M) {
	// Create /tmp/claude directory for tests that need mock scripts
	_ = os.MkdirAll("/tmp/claude", 0755)
	os.Exit(m.Run())
}

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

func (h *testOutputHandler) HandleContextUpdate(_ context.Context, _ string, _ string) {
	// No-op for tests
}

// testTask creates a simple task for processOutput tests.
func testTask() *executor.Task {
	return &executor.Task{
		ID:        "task_test",
		SessionID: "sess_test",
	}
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

	args, _ := e.buildArgs(task, nil)

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

	args, _ := e.buildArgs(task, nil)

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

	args, _ := e.buildArgs(task, config)

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

	args, _ := e.buildArgs(task, config)

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

	args, _ := e.buildArgs(task, nil)

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

	args, _ := e.buildArgs(task, config)

	assert.Contains(t, args, "--model")
	assert.Contains(t, args, "claude-sonnet-4-20250514")
}

func TestExecutor_buildArgs_WithResume(t *testing.T) {
	e := New()

	// Test with session_id (backwards compatibility)
	task := &executor.Task{
		Prompt:          "Continue",
		ContextSnapshot: []byte(`{"session_id":"sess_abc123"}`),
	}

	args, hasResume := e.buildArgs(task, nil)

	assert.Contains(t, args, "--resume")
	assert.Contains(t, args, "sess_abc123")
	assert.True(t, hasResume)

	// Resume runs read the prompt from stdin as stream-json. Without this the
	// CLI never reads stdin and SendMessage writes into a pipe nobody reads.
	assertArgPair(t, args, "--input-format", "stream-json")
	assert.Contains(t, args, "--print")
	assert.NotContains(t, args, "Continue", "the prompt is delivered over stdin, not argv")
}

// assertArgPair asserts that flag is present and immediately followed by value.
func assertArgPair(t *testing.T, args []string, flag, value string) {
	t.Helper()

	for i, arg := range args {
		if arg == flag {
			require.Less(t, i+1, len(args), "%s must have a value", flag)
			assert.Equal(t, value, args[i+1])
			return
		}
	}
	t.Fatalf("args %v missing flag %s", args, flag)
}

// TestExecutor_buildArgs_NonResumeKeepsArgvPrompt pins that only stream mode
// moves the prompt to stdin.
func TestExecutor_buildArgs_NonResumeKeepsArgvPrompt(t *testing.T) {
	e := New()

	args, hasResume := e.buildArgs(&executor.Task{Prompt: "Do the thing"}, nil)

	assert.False(t, hasResume)
	assert.NotContains(t, args, "--input-format")
	assertArgPair(t, args, "--print", "Do the thing")
}

func TestExecutor_buildArgs_WithResumeConversationID(t *testing.T) {
	e := New()

	// Test with conversation_id (preferred field)
	task := &executor.Task{
		Prompt:          "Continue",
		ContextSnapshot: []byte(`{"conversation_id":"conv_xyz789"}`),
	}

	args, hasResume := e.buildArgs(task, nil)

	assert.Contains(t, args, "--resume")
	assert.Contains(t, args, "conv_xyz789")
	assert.True(t, hasResume)
}

func TestExecutor_buildArgs_WithResumeBothFields(t *testing.T) {
	e := New()

	// Test with both fields - conversation_id should be preferred
	task := &executor.Task{
		Prompt:          "Continue",
		ContextSnapshot: []byte(`{"conversation_id":"conv_preferred","session_id":"sess_fallback"}`),
	}

	args, hasResume := e.buildArgs(task, nil)

	assert.Contains(t, args, "--resume")
	assert.Contains(t, args, "conv_preferred")
	assert.NotContains(t, args, "sess_fallback")
	assert.True(t, hasResume)
}

func TestExecutor_buildArgs_EmptyPrompt(t *testing.T) {
	e := New()

	task := &executor.Task{
		Prompt: "",
	}

	args, _ := e.buildArgs(task, nil)

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

func TestExecutor_buildArgs_ContextSnapshotWithoutResumeID(t *testing.T) {
	e := New()

	// Test with context snapshot that has no conversation_id or session_id
	// This happens when session is suspended with a running task
	task := &executor.Task{
		Prompt:          "Test",
		ContextSnapshot: []byte(`{"version":1,"last_activity":"2024-01-01T00:00:00Z"}`),
	}

	args, hasResume := e.buildArgs(task, nil)

	// Should NOT contain --resume since there's no conversation_id or session_id
	assert.NotContains(t, args, "--resume")
	assert.False(t, hasResume, "hasResume should be false when no conversation_id/session_id")
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
	e := New(WithBinaryPath("/nonexistent/binary"), WithoutPermissionGating())

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
	e := New(WithBinaryPath("echo"), WithoutPermissionGating())

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
		e.processOutput(ctx, reader, "stdout", handler, testTask())
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

	// Read from the pipe. SendMessage takes plain text and writes the
	// stream-json envelope the CLI expects, one message per line.
	buf := make([]byte, 1024)
	n, err := reader.Read(buf)
	assert.NoError(t, err)
	written := string(buf[:n])
	assert.True(t, strings.HasSuffix(written, "\n"), "each message is one line")
	assert.JSONEq(t,
		`{"type":"user","message":{"role":"user","content":[{"type":"text","text":"test message"}]}}`,
		strings.TrimSpace(written))

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

	e.processOutput(ctx, reader, "stdout", handler, testTask())

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

	e.processOutput(ctx, reader, "stdout", handler, testTask())

	// Parser handles invalid JSON gracefully (returns text event)
	// The raw output should still be sent to handler
	outputs := handler.GetOutputs()
	assert.GreaterOrEqual(t, len(outputs), 1)
}

func TestExecutor_Execute_WithMockScript(t *testing.T) {
	// Create a temp script that simulates Claude output
	scriptContent := `#!/bin/bash
echo '{"type":"system","subtype":"init","session_id":"sess_test123","model":"mock","claude_code_version":"2.1.241"}'
echo '{"type":"assistant","message":{"content":[{"type":"text","text":"Hello!"}]}}'
echo '{"type":"result","subtype":"success","is_error":false,"result":"Hello!","session_id":"sess_test123","num_turns":1,"duration_ms":5,"total_cost_usd":0.01,"usage":{"input_tokens":10,"output_tokens":20,"cache_creation_input_tokens":3,"cache_read_input_tokens":5}}'
`
	scriptPath := "/tmp/claude/mock_claude.sh"
	err := os.WriteFile(scriptPath, []byte(scriptContent), 0755)
	require.NoError(t, err)
	defer func() { _ = os.Remove(scriptPath) }()

	e := New(WithBinaryPath(scriptPath), WithoutPermissionGating())

	ctx := context.Background()
	task := &executor.Task{
		Prompt:  "test prompt",
		Timeout: 10 * time.Second,
	}
	handler := newTestOutputHandler()

	result, err := e.Execute(ctx, task, nil, handler)

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.True(t, result.Success)
	assert.Equal(t, 0, result.ExitCode)
	assert.Equal(t, "sess_test123", result.AgentSession)

	// Token counts come from the result line's usage block:
	// input_tokens + cache_creation + cache_read, and output_tokens.
	assert.Equal(t, int64(18), result.TokensInput)
	assert.Equal(t, int64(20), result.TokensOutput)

	// The context snapshot must carry the CLI session id so the next task
	// can --resume it.
	assert.JSONEq(t, `{"conversation_id":"sess_test123"}`, string(result.ContextSnapshot))

	// Verify outputs were captured
	outputs := handler.GetOutputs()
	assert.Greater(t, len(outputs), 0)
}

func TestExecutor_Execute_WithWorkingDir(t *testing.T) {
	// Create a script that prints working directory
	scriptContent := `#!/bin/bash
echo '{"type":"system","subtype":"init","session_id":"sess_wd","cwd":"'"$PWD"'"}'
echo '{"type":"result","subtype":"success","is_error":false,"result":"Hello!","session_id":"sess_wd","num_turns":1,"duration_ms":5,"total_cost_usd":0.01,"usage":{"input_tokens":10,"output_tokens":20,"cache_creation_input_tokens":3,"cache_read_input_tokens":5}}'
`
	scriptPath := "/tmp/claude/mock_claude_wd.sh"
	err := os.WriteFile(scriptPath, []byte(scriptContent), 0755)
	require.NoError(t, err)
	defer func() { _ = os.Remove(scriptPath) }()

	e := New(WithBinaryPath(scriptPath), WithoutPermissionGating())

	ctx := context.Background()
	task := &executor.Task{
		Prompt:     "test",
		WorkingDir: "/tmp",
		Timeout:    10 * time.Second,
	}
	handler := newTestOutputHandler()

	result, err := e.Execute(ctx, task, nil, handler)

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.True(t, result.Success)
}

func TestExecutor_Execute_WithTimeout(t *testing.T) {
	// Create a script that sleeps forever
	scriptContent := `#!/bin/bash
sleep 100
`
	scriptPath := "/tmp/claude/mock_claude_slow.sh"
	err := os.WriteFile(scriptPath, []byte(scriptContent), 0755)
	require.NoError(t, err)
	defer func() { _ = os.Remove(scriptPath) }()

	e := New(WithBinaryPath(scriptPath), WithoutPermissionGating())

	ctx := context.Background()
	task := &executor.Task{
		Prompt:  "test",
		Timeout: 100 * time.Millisecond,
	}
	handler := newTestOutputHandler()

	result, err := e.Execute(ctx, task, nil, handler)

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.False(t, result.Success)
	// When timeout kills the process, it may exit with -1 (signal)
	// or the error might be "timeout" depending on timing
	assert.NotEmpty(t, result.Error)
}

func TestExecutor_Execute_NonZeroExit(t *testing.T) {
	// Create a script that exits with error
	scriptContent := `#!/bin/bash
echo '{"type":"system","subtype":"init","session_id":"sess_fail"}'
exit 42
`
	scriptPath := "/tmp/claude/mock_claude_fail.sh"
	err := os.WriteFile(scriptPath, []byte(scriptContent), 0755)
	require.NoError(t, err)
	defer func() { _ = os.Remove(scriptPath) }()

	e := New(WithBinaryPath(scriptPath), WithoutPermissionGating())

	ctx := context.Background()
	task := &executor.Task{
		Prompt:  "test",
		Timeout: 10 * time.Second,
	}
	handler := newTestOutputHandler()

	result, err := e.Execute(ctx, task, nil, handler)

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.False(t, result.Success)
	assert.Equal(t, 42, result.ExitCode)
}

// TestExecutor_Execute_NoResultLine pins the honesty rule: a CLI that exits 0
// without ever emitting a result line has not completed the turn, and must not
// be reported as a successful run.
func TestExecutor_Execute_NoResultLine(t *testing.T) {
	scriptContent := `#!/bin/bash
echo '{"type":"system","subtype":"init","session_id":"sess_silent"}'
exit 0
`
	scriptPath := "/tmp/claude/mock_claude_silent.sh"
	err := os.WriteFile(scriptPath, []byte(scriptContent), 0755)
	require.NoError(t, err)
	defer func() { _ = os.Remove(scriptPath) }()

	e := New(WithBinaryPath(scriptPath), WithoutPermissionGating())

	result, err := e.Execute(context.Background(), &executor.Task{
		Prompt:  "test",
		Timeout: 10 * time.Second,
	}, nil, newTestOutputHandler())

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.False(t, result.Success)
	assert.Equal(t, 0, result.ExitCode)
	assert.Contains(t, result.Error, "without emitting a result message")
}

// TestExecutor_Execute_AgentReportedFailure pins the other half: a clean exit
// whose result line says the agent failed is a failed run, with the CLI's own
// reason carried through.
func TestExecutor_Execute_AgentReportedFailure(t *testing.T) {
	scriptContent := `#!/bin/bash
echo '{"type":"result","subtype":"error_max_turns","is_error":true,"result":"ran out of turns","session_id":"sess_maxturns","usage":{"input_tokens":1,"output_tokens":2}}'
exit 0
`
	scriptPath := "/tmp/claude/mock_claude_maxturns.sh"
	err := os.WriteFile(scriptPath, []byte(scriptContent), 0755)
	require.NoError(t, err)
	defer func() { _ = os.Remove(scriptPath) }()

	e := New(WithBinaryPath(scriptPath), WithoutPermissionGating())

	result, err := e.Execute(context.Background(), &executor.Task{
		Prompt:  "test",
		Timeout: 10 * time.Second,
	}, nil, newTestOutputHandler())

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.False(t, result.Success)
	assert.Contains(t, result.Error, "max turns reached")
	assert.Contains(t, result.Error, "ran out of turns")
	assert.Equal(t, int64(1), result.TokensInput)
	assert.Equal(t, int64(2), result.TokensOutput)
}

func TestExecutor_Execute_WithStderr(t *testing.T) {
	// Create a script that outputs to stderr
	scriptContent := `#!/bin/bash
echo '{"type":"system","subtype":"init","session_id":"sess_stderr"}' >&1
echo "Warning: something" >&2
echo '{"type":"result","subtype":"success","is_error":false,"result":"Hello!","session_id":"sess_stderr","num_turns":1,"duration_ms":5,"total_cost_usd":0.01,"usage":{"input_tokens":10,"output_tokens":20,"cache_creation_input_tokens":3,"cache_read_input_tokens":5}}' >&1
`
	scriptPath := "/tmp/claude/mock_claude_stderr.sh"
	err := os.WriteFile(scriptPath, []byte(scriptContent), 0755)
	require.NoError(t, err)
	defer func() { _ = os.Remove(scriptPath) }()

	e := New(WithBinaryPath(scriptPath), WithoutPermissionGating())

	ctx := context.Background()
	task := &executor.Task{
		Prompt:  "test",
		Timeout: 10 * time.Second,
	}
	handler := newTestOutputHandler()

	result, err := e.Execute(ctx, task, nil, handler)

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.True(t, result.Success)

	// Verify stderr was captured
	outputs := handler.GetOutputs()
	hasStderr := false
	for _, o := range outputs {
		if o.stream == "stderr" && strings.Contains(string(o.data), "Warning") {
			hasStderr = true
			break
		}
	}
	assert.True(t, hasStderr, "Should capture stderr output")
}

func TestExecutor_Execute_WithConfig(t *testing.T) {
	// Create a script that prints environment variables
	scriptContent := `#!/bin/bash
echo '{"type":"system","subtype":"init","session_id":"sess_env","apiKeySource":"'"${ANTHROPIC_API_KEY}"'"}'
echo '{"type":"result","subtype":"success","is_error":false,"result":"Hello!","session_id":"sess_env","num_turns":1,"duration_ms":5,"total_cost_usd":0.01,"usage":{"input_tokens":10,"output_tokens":20,"cache_creation_input_tokens":3,"cache_read_input_tokens":5}}'
`
	scriptPath := "/tmp/claude/mock_claude_env.sh"
	err := os.WriteFile(scriptPath, []byte(scriptContent), 0755)
	require.NoError(t, err)
	defer func() { _ = os.Remove(scriptPath) }()

	e := New(WithBinaryPath(scriptPath), WithoutPermissionGating())

	ctx := context.Background()
	task := &executor.Task{
		Prompt:  "test",
		Timeout: 10 * time.Second,
	}
	config := &executor.AgentConfig{
		APIKey:  "test-key-123",
		BaseURL: "https://api.example.com",
	}
	handler := newTestOutputHandler()

	result, err := e.Execute(ctx, task, config, handler)

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.True(t, result.Success)
}

// TestExecutor_Execute_StreamMode proves the resume path end to end: the
// prompt is delivered as a stream-json user message on stdin, the turn's
// result closes stdin, and the process exits instead of hanging.
func TestExecutor_Execute_StreamMode(t *testing.T) {
	scriptPath := "/tmp/claude/mock_claude_stream.sh"
	capturePath := scriptPath + ".stdin"
	_ = os.Remove(capturePath)

	// Reads the prompt from stdin, then answers. A script that ignored stdin
	// would pass trivially, so it echoes what it read for the assertions.
	scriptContent := `#!/bin/bash
IFS= read -r prompt
printf '%s\n' "$prompt" >> "$0.stdin"
echo '{"type":"system","subtype":"init","session_id":"sess_stream"}'
echo '{"type":"result","subtype":"success","is_error":false,"result":"Hello!","session_id":"sess_stream","num_turns":1,"duration_ms":5,"total_cost_usd":0.01,"usage":{"input_tokens":10,"output_tokens":20,"cache_creation_input_tokens":3,"cache_read_input_tokens":5}}'
# Block until stdin is closed; the executor must close it after the result.
cat > /dev/null
`
	err := os.WriteFile(scriptPath, []byte(scriptContent), 0755)
	require.NoError(t, err)
	defer func() {
		_ = os.Remove(scriptPath)
		_ = os.Remove(capturePath)
	}()

	e := New(WithBinaryPath(scriptPath), WithoutPermissionGating())

	task := &executor.Task{
		Prompt:          "continue please",
		ContextSnapshot: []byte(`{"session_id":"sess_stream"}`),
		Timeout:         10 * time.Second,
	}

	result, err := e.Execute(context.Background(), task, nil, newTestOutputHandler())

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.True(t, result.Success)
	assert.Equal(t, "sess_stream", result.AgentSession)

	captured, err := os.ReadFile(capturePath)
	require.NoError(t, err, "the mock must have received the prompt on stdin")
	assert.JSONEq(t,
		`{"type":"user","message":{"role":"user","content":[{"type":"text","text":"continue please"}]}}`,
		strings.TrimSpace(string(captured)))
}

// TestExecutor_SendMessage_RoundTrip queues a second user message during a
// stream-mode run and proves it reaches the CLI's stdin in the envelope the
// CLI actually accepts.
func TestExecutor_SendMessage_RoundTrip(t *testing.T) {
	scriptPath := "/tmp/claude/mock_claude_roundtrip.sh"
	capturePath := scriptPath + ".stdin"
	_ = os.Remove(capturePath)

	// Reads two messages before finishing the turn, so the test can queue one
	// while the run is still in flight.
	scriptContent := `#!/bin/bash
IFS= read -r first
printf '%s\n' "$first" >> "$0.stdin"
echo '{"type":"assistant","message":{"content":[{"type":"text","text":"ack"}]}}'
IFS= read -r second
printf '%s\n' "$second" >> "$0.stdin"
echo '{"type":"result","subtype":"success","is_error":false,"result":"done","session_id":"sess_rt","usage":{"input_tokens":1,"output_tokens":1}}'
cat > /dev/null
`
	err := os.WriteFile(scriptPath, []byte(scriptContent), 0755)
	require.NoError(t, err)
	defer func() {
		_ = os.Remove(scriptPath)
		_ = os.Remove(capturePath)
	}()

	e := New(WithBinaryPath(scriptPath), WithoutPermissionGating())
	handler := newTestOutputHandler()

	done := make(chan *executor.Result, 1)
	go func() {
		result, execErr := e.Execute(context.Background(), &executor.Task{
			Prompt:          "first message",
			ContextSnapshot: []byte(`{"conversation_id":"sess_rt"}`),
			Timeout:         15 * time.Second,
		}, nil, handler)
		assert.NoError(t, execErr)
		done <- result
	}()

	// Wait for the mock to acknowledge the first message, then queue another.
	require.Eventually(t, func() bool {
		data, readErr := os.ReadFile(capturePath)
		return readErr == nil && strings.Contains(string(data), "first message")
	}, 10*time.Second, 20*time.Millisecond, "mock never received the initial prompt")

	require.NoError(t, e.SendMessage([]byte("second message")))

	result := <-done
	require.NotNil(t, result)
	assert.True(t, result.Success)

	captured, err := os.ReadFile(capturePath)
	require.NoError(t, err)
	lines := strings.Split(strings.TrimSpace(string(captured)), "\n")
	require.Len(t, lines, 2, "both messages must reach the CLI")
	assert.JSONEq(t,
		`{"type":"user","message":{"role":"user","content":[{"type":"text","text":"first message"}]}}`,
		lines[0])
	assert.JSONEq(t,
		`{"type":"user","message":{"role":"user","content":[{"type":"text","text":"second message"}]}}`,
		lines[1])

	// Once the turn ends the executor closes stdin, so further sends fail
	// rather than writing into a closed pipe.
	assert.Error(t, e.SendMessage([]byte("too late")))
}

// TestExecutor_userMessageEnvelope pins the exact stdin contract verified
// against CLI 2.1.241.
func TestExecutor_userMessageEnvelope(t *testing.T) {
	envelope, err := userMessageEnvelope("hello \"world\"")
	require.NoError(t, err)

	assert.JSONEq(t,
		`{"type":"user","message":{"role":"user","content":[{"type":"text","text":"hello \"world\""}]}}`,
		string(envelope))
	assert.NotContains(t, string(envelope), "\n", "the envelope must be a single line")
}
