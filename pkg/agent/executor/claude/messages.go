// Package claude implements the executor for Claude Code CLI.
package claude

import "encoding/json"

// StreamMessage represents a message in Claude Code's stream-json output.
// Each line of output is a JSON object with a "type" field.
type StreamMessage struct {
	Type    string          `json:"type"`
	Message json.RawMessage `json:"message,omitempty"`

	// For type "system"
	Subtype string `json:"subtype,omitempty"`
	Data    string `json:"data,omitempty"`

	// For type "result"
	Result *ResultMessage `json:"result,omitempty"`

	// Session info (for resume)
	SessionID string `json:"session_id,omitempty"`
}

// AssistantMessage represents an assistant message with content blocks.
type AssistantMessage struct {
	ID      string         `json:"id,omitempty"`
	Role    string         `json:"role"`
	Content []ContentBlock `json:"content"`
	Model   string         `json:"model,omitempty"`
	Usage   *Usage         `json:"usage,omitempty"`

	// Stop reason
	StopReason   string `json:"stop_reason,omitempty"`
	StopSequence string `json:"stop_sequence,omitempty"`
}

// ContentBlock represents a content block in an assistant message.
// The Type field determines which other fields are populated.
type ContentBlock struct {
	Type string `json:"type"`

	// For type "text"
	Text string `json:"text,omitempty"`

	// For type "thinking"
	Thinking string `json:"thinking,omitempty"`

	// For type "tool_use"
	ID    string          `json:"id,omitempty"`
	Name  string          `json:"name,omitempty"`
	Input json.RawMessage `json:"input,omitempty"`

	// For type "tool_result"
	ToolUseID string `json:"tool_use_id,omitempty"`
	Content   string `json:"content,omitempty"`
	IsError   bool   `json:"is_error,omitempty"`
}

// ResultMessage contains the final result of a Claude Code execution.
type ResultMessage struct {
	Success     bool   `json:"success"`
	ExitCode    int    `json:"exit_code"`
	Error       string `json:"error,omitempty"`
	Duration    int64  `json:"duration_ms,omitempty"`
	NumTurns    int    `json:"num_turns,omitempty"`
	SessionID   string `json:"session_id,omitempty"`
	TotalCost   string `json:"total_cost_usd,omitempty"`
	Usage       *Usage `json:"usage,omitempty"`
	Interrupted bool   `json:"interrupted,omitempty"`
}

// Usage contains token usage information.
type Usage struct {
	InputTokens              int64 `json:"input_tokens"`
	OutputTokens             int64 `json:"output_tokens"`
	CacheCreationInputTokens int64 `json:"cache_creation_input_tokens,omitempty"`
	CacheReadInputTokens     int64 `json:"cache_read_input_tokens,omitempty"`
}

// SystemSubtype constants for system messages.
const (
	SystemSubtypeInit   = "init"
	SystemSubtypeStatus = "status"
	SystemSubtypeError  = "error"
)

// MessageType constants for stream messages.
const (
	MessageTypeSystem    = "system"
	MessageTypeAssistant = "assistant"
	MessageTypeUser      = "user"
	MessageTypeResult    = "result"
)

// ContentType constants for content blocks.
const (
	ContentTypeText       = "text"
	ContentTypeThinking   = "thinking"
	ContentTypeToolUse    = "tool_use"
	ContentTypeToolResult = "tool_result"
)

// PermissionToolNames are tools that typically require permission.
var PermissionToolNames = map[string]bool{
	"Bash":         true,
	"Write":        true,
	"Edit":         true,
	"NotebookEdit": true,
	"computer":     true,
}

// IsPermissionRequired checks if a tool typically requires permission.
func IsPermissionRequired(toolName string) bool {
	return PermissionToolNames[toolName]
}
