package executor

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestEventTypes(t *testing.T) {
	assert.Equal(t, EventType("text"), EventText)
	assert.Equal(t, EventType("thinking"), EventThinking)
	assert.Equal(t, EventType("tool_use"), EventToolUse)
	assert.Equal(t, EventType("tool_result"), EventToolResult)
	assert.Equal(t, EventType("error"), EventError)
	assert.Equal(t, EventType("system"), EventSystem)
	assert.Equal(t, EventType("usage"), EventUsage)
}

func TestNewTextEvent(t *testing.T) {
	before := time.Now()
	event := NewTextEvent("Hello, world!")
	after := time.Now()

	assert.Equal(t, EventText, event.Type)
	assert.Equal(t, "Hello, world!", event.Text)
	assert.True(t, event.Timestamp.After(before) || event.Timestamp.Equal(before))
	assert.True(t, event.Timestamp.Before(after) || event.Timestamp.Equal(after))
	assert.Nil(t, event.ToolUse)
	assert.Nil(t, event.ToolResult)
	assert.Nil(t, event.Usage)
}

func TestNewThinkingEvent(t *testing.T) {
	event := NewThinkingEvent("Let me think about this...")

	assert.Equal(t, EventThinking, event.Type)
	assert.Equal(t, "Let me think about this...", event.Text)
	assert.Nil(t, event.ToolUse)
}

func TestNewToolUseEvent(t *testing.T) {
	event := NewToolUseEvent("tool_123", "bash", `{"command": "ls -la"}`)

	assert.Equal(t, EventToolUse, event.Type)
	assert.NotNil(t, event.ToolUse)
	assert.Equal(t, "tool_123", event.ToolUse.ID)
	assert.Equal(t, "bash", event.ToolUse.Name)
	assert.Equal(t, `{"command": "ls -la"}`, event.ToolUse.Input)
	assert.Empty(t, event.Text)
}

func TestNewToolResultEvent(t *testing.T) {
	t.Run("success result", func(t *testing.T) {
		event := NewToolResultEvent("tool_123", "file1.txt\nfile2.txt", false)

		assert.Equal(t, EventToolResult, event.Type)
		assert.NotNil(t, event.ToolResult)
		assert.Equal(t, "tool_123", event.ToolResult.ToolUseID)
		assert.Equal(t, "file1.txt\nfile2.txt", event.ToolResult.Output)
		assert.False(t, event.ToolResult.IsError)
	})

	t.Run("error result", func(t *testing.T) {
		event := NewToolResultEvent("tool_456", "command not found", true)

		assert.Equal(t, EventToolResult, event.Type)
		assert.NotNil(t, event.ToolResult)
		assert.Equal(t, "tool_456", event.ToolResult.ToolUseID)
		assert.Equal(t, "command not found", event.ToolResult.Output)
		assert.True(t, event.ToolResult.IsError)
	})
}

func TestNewErrorEvent(t *testing.T) {
	event := NewErrorEvent("something went wrong")

	assert.Equal(t, EventError, event.Type)
	assert.Equal(t, "something went wrong", event.Text)
}

func TestNewSystemEvent(t *testing.T) {
	event := NewSystemEvent("Agent started")

	assert.Equal(t, EventSystem, event.Type)
	assert.Equal(t, "Agent started", event.Text)
}

func TestNewUsageEvent(t *testing.T) {
	event := NewUsageEvent(1000, 500, 100, 50)

	assert.Equal(t, EventUsage, event.Type)
	assert.NotNil(t, event.Usage)
	assert.Equal(t, int64(1000), event.Usage.InputTokens)
	assert.Equal(t, int64(500), event.Usage.OutputTokens)
	assert.Equal(t, int64(100), event.Usage.CacheRead)
	assert.Equal(t, int64(50), event.Usage.CacheWrite)
	assert.Empty(t, event.Text)
}

func TestToolUseEvent(t *testing.T) {
	toolUse := &ToolUseEvent{
		ID:    "tu_abc123",
		Name:  "read_file",
		Input: `{"path": "/tmp/test.txt"}`,
	}

	assert.Equal(t, "tu_abc123", toolUse.ID)
	assert.Equal(t, "read_file", toolUse.Name)
	assert.Equal(t, `{"path": "/tmp/test.txt"}`, toolUse.Input)
}

func TestToolResultEvent(t *testing.T) {
	result := &ToolResultEvent{
		ToolUseID: "tu_abc123",
		Output:    "file contents here",
		IsError:   false,
	}

	assert.Equal(t, "tu_abc123", result.ToolUseID)
	assert.Equal(t, "file contents here", result.Output)
	assert.False(t, result.IsError)
}

func TestUsageEvent(t *testing.T) {
	usage := &UsageEvent{
		InputTokens:  2000,
		OutputTokens: 800,
		CacheRead:    500,
		CacheWrite:   200,
	}

	assert.Equal(t, int64(2000), usage.InputTokens)
	assert.Equal(t, int64(800), usage.OutputTokens)
	assert.Equal(t, int64(500), usage.CacheRead)
	assert.Equal(t, int64(200), usage.CacheWrite)
}

func TestAgentEvent_RawField(t *testing.T) {
	event := &AgentEvent{
		Type:      EventText,
		Timestamp: time.Now(),
		Text:      "test",
		Raw:       []byte(`{"type":"text","content":"test"}`),
	}

	assert.Equal(t, EventText, event.Type)
	assert.Equal(t, []byte(`{"type":"text","content":"test"}`), event.Raw)
}
