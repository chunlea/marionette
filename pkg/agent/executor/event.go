// Package executor provides interfaces and implementations for running AI agents.
package executor

import (
	"encoding/json"
	"time"
)

// EventType defines the type of agent event.
type EventType string

const (
	// EventText is assistant text output.
	EventText EventType = "text"

	// EventThinking is thinking/reasoning output (if exposed by agent).
	EventThinking EventType = "thinking"

	// EventToolUse is a tool invocation event.
	EventToolUse EventType = "tool_use"

	// EventToolResult is a tool execution result.
	EventToolResult EventType = "tool_result"

	// EventError is an error message.
	EventError EventType = "error"

	// EventSystem is a system notification.
	EventSystem EventType = "system"

	// EventUsage is token usage statistics.
	EventUsage EventType = "usage"
)

// AgentEvent is a unified event format for all agent types.
// Each agent executor parses its native output into this format.
type AgentEvent struct {
	// Type is the event type.
	Type EventType `json:"type"`

	// Content is text content (for text, thinking, error, system events).
	Content string `json:"content,omitempty"`

	// Tool contains tool invocation details (for tool_use events).
	Tool *ToolUseEvent `json:"tool,omitempty"`

	// ToolResult contains tool result details (for tool_result events).
	ToolResult *ToolResultEvent `json:"tool_result,omitempty"`

	// Usage contains token usage statistics (for usage events).
	Usage *UsageEvent `json:"usage,omitempty"`

	// Timestamp is when the event occurred.
	Timestamp time.Time `json:"ts"`

	// Sequence is the ordering within a run (correlates with raw_logs).
	Sequence int64 `json:"seq,omitempty"`

	// RawLogID references the source raw log entry (for traceability).
	RawLogID string `json:"raw_log_id,omitempty"`
}

// ToolUseEvent contains details about a tool invocation.
type ToolUseEvent struct {
	// ID is the unique identifier for this tool use (for correlation with result).
	ID string `json:"id"`

	// Name is the tool name (e.g., "bash", "edit", "read").
	Name string `json:"name"`

	// Input is the tool input parameters.
	Input json.RawMessage `json:"input,omitempty"`
}

// ToolResultEvent contains details about a tool execution result.
type ToolResultEvent struct {
	// ToolUseID correlates with the ToolUseEvent.ID.
	ToolUseID string `json:"tool_use_id"`

	// Output is the tool output.
	Output string `json:"output,omitempty"`

	// IsError indicates if the tool execution failed.
	IsError bool `json:"is_error,omitempty"`
}

// UsageEvent contains token usage statistics.
type UsageEvent struct {
	// InputTokens is the number of input tokens.
	InputTokens int64 `json:"input_tokens,omitempty"`

	// OutputTokens is the number of output tokens.
	OutputTokens int64 `json:"output_tokens,omitempty"`

	// CacheCreationTokens is tokens used for cache creation.
	CacheCreationTokens int64 `json:"cache_creation_tokens,omitempty"`

	// CacheReadTokens is tokens read from cache.
	CacheReadTokens int64 `json:"cache_read_tokens,omitempty"`

	// CostUSD is the estimated cost in USD.
	CostUSD float64 `json:"cost_usd,omitempty"`
}

// NewTextEvent creates a text event.
func NewTextEvent(content string) *AgentEvent {
	return &AgentEvent{
		Type:      EventText,
		Content:   content,
		Timestamp: time.Now(),
	}
}

// NewThinkingEvent creates a thinking event.
func NewThinkingEvent(content string) *AgentEvent {
	return &AgentEvent{
		Type:      EventThinking,
		Content:   content,
		Timestamp: time.Now(),
	}
}

// NewToolUseEvent creates a tool use event.
func NewToolUseEvent(id, name string, input json.RawMessage) *AgentEvent {
	return &AgentEvent{
		Type: EventToolUse,
		Tool: &ToolUseEvent{
			ID:    id,
			Name:  name,
			Input: input,
		},
		Timestamp: time.Now(),
	}
}

// NewToolResultEvent creates a tool result event.
func NewToolResultEvent(toolUseID, output string, isError bool) *AgentEvent {
	return &AgentEvent{
		Type: EventToolResult,
		ToolResult: &ToolResultEvent{
			ToolUseID: toolUseID,
			Output:    output,
			IsError:   isError,
		},
		Timestamp: time.Now(),
	}
}

// NewErrorEvent creates an error event.
func NewErrorEvent(message string) *AgentEvent {
	return &AgentEvent{
		Type:      EventError,
		Content:   message,
		Timestamp: time.Now(),
	}
}

// NewSystemEvent creates a system event.
func NewSystemEvent(message string) *AgentEvent {
	return &AgentEvent{
		Type:      EventSystem,
		Content:   message,
		Timestamp: time.Now(),
	}
}

// NewUsageEvent creates a usage event.
func NewUsageEvent(inputTokens, outputTokens int64) *AgentEvent {
	return &AgentEvent{
		Type: EventUsage,
		Usage: &UsageEvent{
			InputTokens:  inputTokens,
			OutputTokens: outputTokens,
		},
		Timestamp: time.Now(),
	}
}
