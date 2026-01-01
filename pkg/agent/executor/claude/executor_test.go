package claude

import (
	"context"
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
		Prompt:  "Continue",
		Context: []byte(`{"session_id":"sess_abc123"}`),
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
