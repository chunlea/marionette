package executor

import (
	"time"
)

// EventType represents the type of agent event.
type EventType string

const (
	// EventText is a text output event (assistant message content).
	EventText EventType = "text"

	// EventThinking is a thinking/reasoning event (extended thinking).
	EventThinking EventType = "thinking"

	// EventToolUse is a tool invocation event.
	EventToolUse EventType = "tool_use"

	// EventToolResult is the result of a tool invocation.
	EventToolResult EventType = "tool_result"

	// EventError is an error event.
	EventError EventType = "error"

	// EventSystem is a system message event.
	EventSystem EventType = "system"

	// EventUsage is a token usage summary event.
	EventUsage EventType = "usage"
)

// String returns the string representation of the event type.
func (e EventType) String() string {
	return string(e)
}

// AgentEvent represents a structured event from an agent.
// Events are parsed from raw agent output and provide a unified format
// for all agent types.
type AgentEvent struct {
	Type      EventType `json:"type"`
	Timestamp time.Time `json:"timestamp"`

	// Text content (for text, thinking, error, system events)
	Text string `json:"text,omitempty"`

	// Tool events
	ToolUse    *ToolUseEvent    `json:"tool_use,omitempty"`
	ToolResult *ToolResultEvent `json:"tool_result,omitempty"`

	// Usage summary
	Usage *UsageEvent `json:"usage,omitempty"`

	// Raw data for debugging/storage
	Raw []byte `json:"raw,omitempty"`
}

// ToolUseEvent represents a tool invocation.
type ToolUseEvent struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Input string `json:"input"` // JSON string of tool input
}

// ToolResultEvent represents the result of a tool invocation.
type ToolResultEvent struct {
	ToolUseID string `json:"tool_use_id"`
	Output    string `json:"output"`
	IsError   bool   `json:"is_error,omitempty"`
}

// UsageEvent represents token usage information.
type UsageEvent struct {
	InputTokens  int64 `json:"input_tokens"`
	OutputTokens int64 `json:"output_tokens"`
	CacheRead    int64 `json:"cache_read,omitempty"`
	CacheWrite   int64 `json:"cache_write,omitempty"`
}

// NewTextEvent creates a new text event.
func NewTextEvent(text string) *AgentEvent {
	return &AgentEvent{
		Type:      EventText,
		Timestamp: time.Now(),
		Text:      text,
	}
}

// NewThinkingEvent creates a new thinking event.
func NewThinkingEvent(text string) *AgentEvent {
	return &AgentEvent{
		Type:      EventThinking,
		Timestamp: time.Now(),
		Text:      text,
	}
}

// NewToolUseEvent creates a new tool use event.
func NewToolUseEvent(id, name, input string) *AgentEvent {
	return &AgentEvent{
		Type:      EventToolUse,
		Timestamp: time.Now(),
		ToolUse: &ToolUseEvent{
			ID:    id,
			Name:  name,
			Input: input,
		},
	}
}

// NewToolResultEvent creates a new tool result event.
func NewToolResultEvent(toolUseID, output string, isError bool) *AgentEvent {
	return &AgentEvent{
		Type:      EventToolResult,
		Timestamp: time.Now(),
		ToolResult: &ToolResultEvent{
			ToolUseID: toolUseID,
			Output:    output,
			IsError:   isError,
		},
	}
}

// NewErrorEvent creates a new error event.
func NewErrorEvent(text string) *AgentEvent {
	return &AgentEvent{
		Type:      EventError,
		Timestamp: time.Now(),
		Text:      text,
	}
}

// NewSystemEvent creates a new system event.
func NewSystemEvent(text string) *AgentEvent {
	return &AgentEvent{
		Type:      EventSystem,
		Timestamp: time.Now(),
		Text:      text,
	}
}

// NewUsageEvent creates a new usage event.
func NewUsageEvent(input, output, cacheRead, cacheWrite int64) *AgentEvent {
	return &AgentEvent{
		Type:      EventUsage,
		Timestamp: time.Now(),
		Usage: &UsageEvent{
			InputTokens:  input,
			OutputTokens: output,
			CacheRead:    cacheRead,
			CacheWrite:   cacheWrite,
		},
	}
}
