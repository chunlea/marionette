package executor

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestTask(t *testing.T) {
	task := &Task{
		ID:              "task_123",
		RunID:           "trun_123",
		SessionID:       "sess_123",
		Attempt:         1,
		Prompt:          "Test prompt",
		Timeout:         time.Hour,
		ContextSnapshot: []byte(`{"session_id":"abc123"}`),
	}

	assert.Equal(t, "task_123", task.ID)
	assert.Equal(t, "trun_123", task.RunID)
	assert.Equal(t, "sess_123", task.SessionID)
	assert.Equal(t, int32(1), task.Attempt)
	assert.Equal(t, "Test prompt", task.Prompt)
	assert.Equal(t, time.Hour, task.Timeout)
	assert.Equal(t, []byte(`{"session_id":"abc123"}`), task.ContextSnapshot)
}

func TestAgentConfig(t *testing.T) {
	config := &AgentConfig{
		Agent:      "claude",
		Model:      "claude-sonnet-4-20250514",
		APIKey:     "test-key",
		BaseURL:    "https://api.anthropic.com",
		WorkingDir: "/workspace",
		Extra: map[string]string{
			"key": "value",
		},
	}

	assert.Equal(t, "claude", config.Agent)
	assert.Equal(t, "claude-sonnet-4-20250514", config.Model)
	assert.Equal(t, "test-key", config.APIKey)
	assert.Equal(t, "https://api.anthropic.com", config.BaseURL)
	assert.Equal(t, "/workspace", config.WorkingDir)
	assert.Equal(t, "value", config.Extra["key"])
}

func TestResult(t *testing.T) {
	result := &Result{
		Success:         true,
		ExitCode:        0,
		Error:           "",
		TokensInput:     100,
		TokensOutput:    200,
		ContextSnapshot: []byte(`{"state":"test"}`),
		CompletedAt:     time.Now(),
	}

	assert.True(t, result.Success)
	assert.Equal(t, 0, result.ExitCode)
	assert.Empty(t, result.Error)
	assert.Equal(t, int64(100), result.TokensInput)
	assert.Equal(t, int64(200), result.TokensOutput)
	assert.Equal(t, []byte(`{"state":"test"}`), result.ContextSnapshot)
}

func TestPermissionRequest(t *testing.T) {
	req := &PermissionRequest{
		ID:        "perm_123",
		Tool:      "bash",
		Action:    "rm -rf /tmp/test",
		Context:   "Deleting temporary files",
		RiskLevel: RiskMedium,
	}

	assert.Equal(t, "perm_123", req.ID)
	assert.Equal(t, "bash", req.Tool)
	assert.Equal(t, "rm -rf /tmp/test", req.Action)
	assert.Equal(t, "Deleting temporary files", req.Context)
	assert.Equal(t, RiskMedium, req.RiskLevel)
}

func TestRiskLevel(t *testing.T) {
	t.Run("constants", func(t *testing.T) {
		assert.Equal(t, RiskLevel("low"), RiskLow)
		assert.Equal(t, RiskLevel("medium"), RiskMedium)
		assert.Equal(t, RiskLevel("high"), RiskHigh)
		assert.Equal(t, RiskLevel("critical"), RiskCritical)
	})

	t.Run("string", func(t *testing.T) {
		assert.Equal(t, "low", RiskLow.String())
		assert.Equal(t, "medium", RiskMedium.String())
		assert.Equal(t, "high", RiskHigh.String())
		assert.Equal(t, "critical", RiskCritical.String())
	})
}

// MockOutputHandler is a test helper for capturing output.
type MockOutputHandler struct {
	Outputs             []OutputRecord
	PermissionResponses map[string]bool
}

type OutputRecord struct {
	Stream string
	Data   []byte
}

func NewMockOutputHandler() *MockOutputHandler {
	return &MockOutputHandler{
		PermissionResponses: make(map[string]bool),
	}
}

func (m *MockOutputHandler) HandleOutput(stream string, data []byte) {
	m.Outputs = append(m.Outputs, OutputRecord{Stream: stream, Data: data})
}

func (m *MockOutputHandler) HandlePermissionRequest(_ context.Context, req *PermissionRequest) (bool, error) {
	if resp, ok := m.PermissionResponses[req.ID]; ok {
		return resp, nil
	}
	return true, nil
}

func TestOutputWriter(t *testing.T) {
	handler := NewMockOutputHandler()
	writer := &OutputWriter{
		Handler: handler,
		Stream:  "stdout",
	}

	data := []byte("Hello, World!\n")
	n, err := writer.Write(data)

	assert.NoError(t, err)
	assert.Equal(t, len(data), n)
	assert.Len(t, handler.Outputs, 1)
	assert.Equal(t, "stdout", handler.Outputs[0].Stream)
	assert.Equal(t, data, handler.Outputs[0].Data)
}

func TestOutputWriter_MultipleWrites(t *testing.T) {
	handler := NewMockOutputHandler()
	writer := &OutputWriter{
		Handler: handler,
		Stream:  "stderr",
	}

	writes := []string{"Line 1\n", "Line 2\n", "Line 3\n"}
	for _, s := range writes {
		n, err := writer.Write([]byte(s))
		assert.NoError(t, err)
		assert.Equal(t, len(s), n)
	}

	assert.Len(t, handler.Outputs, 3)
	for i, record := range handler.Outputs {
		assert.Equal(t, "stderr", record.Stream)
		assert.Equal(t, writes[i], string(record.Data))
	}
}
