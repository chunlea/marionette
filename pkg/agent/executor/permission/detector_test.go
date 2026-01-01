package permission

import (
	"context"
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
	mu                  sync.Mutex
	outputs             []OutputRecord
	permissionResponses map[string]bool
	permissionDelay     time.Duration
	permissionError     error
}

type OutputRecord struct {
	Stream string
	Data   []byte
}

func NewMockOutputHandler() *MockOutputHandler {
	return &MockOutputHandler{
		permissionResponses: make(map[string]bool),
	}
}

func (m *MockOutputHandler) HandleOutput(stream string, data []byte) {
	m.mu.Lock()
	defer m.mu.Unlock()
	dataCopy := make([]byte, len(data))
	copy(dataCopy, data)
	m.outputs = append(m.outputs, OutputRecord{Stream: stream, Data: dataCopy})
}

func (m *MockOutputHandler) HandlePermissionRequest(ctx context.Context, req *executor.PermissionRequest) (bool, error) {
	m.mu.Lock()
	delay := m.permissionDelay
	err := m.permissionError
	response, ok := m.permissionResponses[req.ID]
	if !ok {
		// Default to approve
		response = true
	}
	m.mu.Unlock()

	if delay > 0 {
		select {
		case <-time.After(delay):
		case <-ctx.Done():
			return false, ctx.Err()
		}
	}

	if err != nil {
		return false, err
	}

	return response, nil
}

func (m *MockOutputHandler) GetOutputs() []OutputRecord {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]OutputRecord, len(m.outputs))
	copy(result, m.outputs)
	return result
}

func (m *MockOutputHandler) SetPermissionResponse(reqID string, approved bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.permissionResponses[reqID] = approved
}

func (m *MockOutputHandler) SetPermissionDelay(d time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.permissionDelay = d
}

func (m *MockOutputHandler) SetPermissionError(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.permissionError = err
}

func TestDetector_HandleOutput_ForwardsToHandler(t *testing.T) {
	handler := NewMockOutputHandler()
	logger := zaptest.NewLogger(t)
	detector := NewDetector(handler, logger)

	// Send some output
	detector.HandleOutput("stdout", []byte("Hello, World!\n"))
	detector.HandleOutput("stderr", []byte("Error message\n"))

	outputs := handler.GetOutputs()
	require.Len(t, outputs, 2)

	assert.Equal(t, "stdout", outputs[0].Stream)
	assert.Equal(t, "Hello, World!\n", string(outputs[0].Data))

	assert.Equal(t, "stderr", outputs[1].Stream)
	assert.Equal(t, "Error message\n", string(outputs[1].Data))
}

func TestDetector_DetectsPermissionRequest(t *testing.T) {
	handler := NewMockOutputHandler()
	logger := zaptest.NewLogger(t)
	detector := NewDetector(handler, logger)

	// Initially no pending request
	assert.False(t, detector.HasPendingRequest())
	assert.Nil(t, detector.GetPendingRequest())

	// Send permission request output
	detector.HandleOutput("stdout", []byte("Claude wants to use the Bash tool to run:\n"))
	detector.HandleOutput("stdout", []byte("rm -rf /tmp/test\n"))
	detector.HandleOutput("stdout", []byte("Allow this action? (y)es / (n)o\n"))

	// Should detect the request
	assert.True(t, detector.HasPendingRequest())

	req := detector.GetPendingRequest()
	require.NotNil(t, req)
	assert.Equal(t, "bash", req.Tool)
	assert.Equal(t, "rm -rf /tmp/test", req.Action)
	assert.Equal(t, executor.RiskHigh, req.RiskLevel)
}

func TestDetector_IgnoresStderr(t *testing.T) {
	handler := NewMockOutputHandler()
	logger := zaptest.NewLogger(t)
	detector := NewDetector(handler, logger)

	// Send permission request via stderr (should be ignored for detection)
	detector.HandleOutput("stderr", []byte("Claude wants to use the Bash tool to run:\n"))
	detector.HandleOutput("stderr", []byte("rm -rf /tmp/test\n"))
	detector.HandleOutput("stderr", []byte("Allow this action? (y)es / (n)o\n"))

	// Should not detect request from stderr
	assert.False(t, detector.HasPendingRequest())
}

func TestDetector_WaitForPermission_Approved(t *testing.T) {
	handler := NewMockOutputHandler()
	logger := zaptest.NewLogger(t)
	detector := NewDetector(handler, logger)

	// Send permission request output
	detector.HandleOutput("stdout", []byte("Claude wants to use the Bash tool to run:\n"))
	detector.HandleOutput("stdout", []byte("echo hello\n"))
	detector.HandleOutput("stdout", []byte("Allow this action? (y)es / (n)o\n"))

	require.True(t, detector.HasPendingRequest())

	// Wait for permission (default response is approve)
	ctx := context.Background()
	approved, err := detector.WaitForPermission(ctx)

	require.NoError(t, err)
	assert.True(t, approved)

	// Pending request should be cleared
	assert.False(t, detector.HasPendingRequest())
}

func TestDetector_WaitForPermission_Denied(t *testing.T) {
	handler := NewMockOutputHandler()
	logger := zaptest.NewLogger(t)
	detector := NewDetector(handler, logger)

	// Send permission request output
	detector.HandleOutput("stdout", []byte("Claude wants to use the Bash tool to run:\n"))
	detector.HandleOutput("stdout", []byte("rm -rf /\n"))
	detector.HandleOutput("stdout", []byte("Allow this action? (y)es / (n)o\n"))

	req := detector.GetPendingRequest()
	require.NotNil(t, req)

	// Set response to deny
	handler.SetPermissionResponse(req.ID, false)

	ctx := context.Background()
	approved, err := detector.WaitForPermission(ctx)

	require.NoError(t, err)
	assert.False(t, approved)
}

func TestDetector_WaitForPermission_ContextCancelled(t *testing.T) {
	handler := NewMockOutputHandler()
	handler.SetPermissionDelay(5 * time.Second) // Long delay

	logger := zaptest.NewLogger(t)
	detector := NewDetector(handler, logger)

	// Send permission request output
	detector.HandleOutput("stdout", []byte("Claude wants to use the Bash tool to run:\n"))
	detector.HandleOutput("stdout", []byte("echo hello\n"))
	detector.HandleOutput("stdout", []byte("Allow this action? (y)es / (n)o\n"))

	require.True(t, detector.HasPendingRequest())

	// Cancel context quickly
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	approved, err := detector.WaitForPermission(ctx)

	assert.Error(t, err)
	assert.False(t, approved)
}

func TestDetector_WaitForPermission_NoPendingRequest(t *testing.T) {
	handler := NewMockOutputHandler()
	logger := zaptest.NewLogger(t)
	detector := NewDetector(handler, logger)

	// No pending request
	assert.False(t, detector.HasPendingRequest())

	ctx := context.Background()
	approved, err := detector.WaitForPermission(ctx)

	// Should return false with no error
	require.NoError(t, err)
	assert.False(t, approved)
}

func TestDetector_Reset(t *testing.T) {
	handler := NewMockOutputHandler()
	logger := zaptest.NewLogger(t)
	detector := NewDetector(handler, logger)

	// Send permission request output
	detector.HandleOutput("stdout", []byte("Claude wants to use the Bash tool to run:\n"))
	detector.HandleOutput("stdout", []byte("echo hello\n"))
	detector.HandleOutput("stdout", []byte("Allow this action? (y)es / (n)o\n"))

	require.True(t, detector.HasPendingRequest())

	// Reset
	detector.Reset()

	assert.False(t, detector.HasPendingRequest())
	assert.Nil(t, detector.GetPendingRequest())
}

func TestDetector_ClearPendingRequest(t *testing.T) {
	handler := NewMockOutputHandler()
	logger := zaptest.NewLogger(t)
	detector := NewDetector(handler, logger)

	// Send permission request output
	detector.HandleOutput("stdout", []byte("Claude wants to use the Bash tool to run:\n"))
	detector.HandleOutput("stdout", []byte("echo hello\n"))
	detector.HandleOutput("stdout", []byte("Allow this action? (y)es / (n)o\n"))

	require.True(t, detector.HasPendingRequest())

	// Clear
	detector.ClearPendingRequest()

	assert.False(t, detector.HasPendingRequest())
}

func TestDetector_MultipleOutputLines(t *testing.T) {
	handler := NewMockOutputHandler()
	logger := zaptest.NewLogger(t)
	detector := NewDetector(handler, logger)

	// Send all lines at once
	output := `Claude wants to use the Bash tool to run:
ls -la
Allow this action? (y)es / (n)o
`
	detector.HandleOutput("stdout", []byte(output))

	assert.True(t, detector.HasPendingRequest())

	req := detector.GetPendingRequest()
	require.NotNil(t, req)
	assert.Equal(t, "bash", req.Tool)
	assert.Equal(t, "ls -la", req.Action)
}
