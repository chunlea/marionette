package executor

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
	event := NewTextEvent("Hello, world!")

	assert.Equal(t, EventText, event.Type)
	assert.Equal(t, "Hello, world!", event.Content)
	assert.WithinDuration(t, time.Now(), event.Timestamp, time.Second)
}

func TestNewThinkingEvent(t *testing.T) {
	event := NewThinkingEvent("Let me think about this...")

	assert.Equal(t, EventThinking, event.Type)
	assert.Equal(t, "Let me think about this...", event.Content)
}

func TestNewToolUseEvent(t *testing.T) {
	input := json.RawMessage(`{"command": "ls"}`)
	event := NewToolUseEvent("tool_123", "bash", input)

	assert.Equal(t, EventToolUse, event.Type)
	require.NotNil(t, event.Tool)
	assert.Equal(t, "tool_123", event.Tool.ID)
	assert.Equal(t, "bash", event.Tool.Name)
	assert.Equal(t, input, event.Tool.Input)
}

func TestNewToolResultEvent(t *testing.T) {
	event := NewToolResultEvent("tool_123", "file1.go\nfile2.go", false)

	assert.Equal(t, EventToolResult, event.Type)
	require.NotNil(t, event.ToolResult)
	assert.Equal(t, "tool_123", event.ToolResult.ToolUseID)
	assert.Equal(t, "file1.go\nfile2.go", event.ToolResult.Output)
	assert.False(t, event.ToolResult.IsError)
}

func TestNewToolResultEvent_WithError(t *testing.T) {
	event := NewToolResultEvent("tool_456", "command not found", true)

	assert.Equal(t, EventToolResult, event.Type)
	require.NotNil(t, event.ToolResult)
	assert.True(t, event.ToolResult.IsError)
}

func TestNewErrorEvent(t *testing.T) {
	event := NewErrorEvent("Something went wrong")

	assert.Equal(t, EventError, event.Type)
	assert.Equal(t, "Something went wrong", event.Content)
}

func TestNewSystemEvent(t *testing.T) {
	event := NewSystemEvent("System notification")

	assert.Equal(t, EventSystem, event.Type)
	assert.Equal(t, "System notification", event.Content)
}

func TestNewUsageEvent(t *testing.T) {
	event := NewUsageEvent(100, 50)

	assert.Equal(t, EventUsage, event.Type)
	require.NotNil(t, event.Usage)
	assert.Equal(t, int64(100), event.Usage.InputTokens)
	assert.Equal(t, int64(50), event.Usage.OutputTokens)
}

func TestAgentEvent_JSON(t *testing.T) {
	event := &AgentEvent{
		Type:      EventToolUse,
		Timestamp: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		Tool: &ToolUseEvent{
			ID:    "tool_1",
			Name:  "bash",
			Input: json.RawMessage(`{"command":"pwd"}`),
		},
		Sequence: 42,
	}

	data, err := json.Marshal(event)
	require.NoError(t, err)

	var parsed AgentEvent
	err = json.Unmarshal(data, &parsed)
	require.NoError(t, err)

	assert.Equal(t, EventToolUse, parsed.Type)
	assert.Equal(t, int64(42), parsed.Sequence)
	require.NotNil(t, parsed.Tool)
	assert.Equal(t, "tool_1", parsed.Tool.ID)
	assert.Equal(t, "bash", parsed.Tool.Name)
}
