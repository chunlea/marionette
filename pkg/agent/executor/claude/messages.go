// Package claude implements the Claude Code executor.
package claude

import "encoding/json"

// StreamMessage represents a message in the stream-json output.
// Claude Code outputs NDJSON (newline-delimited JSON) with various message types.
type StreamMessage struct {
	Type string `json:"type"`

	// For "init" messages
	SessionID string `json:"session_id,omitempty"`
	Model     string `json:"model,omitempty"`
	CWD       string `json:"cwd,omitempty"`

	// For "assistant" messages
	Message *AssistantMessage `json:"message,omitempty"`

	// For "content_block_start", "content_block_delta", "content_block_stop"
	Index        int           `json:"index,omitempty"`
	ContentBlock *ContentBlock `json:"content_block,omitempty"`
	Delta        *Delta        `json:"delta,omitempty"`

	// For "tool_use" messages
	ToolUse *ToolUse `json:"tool_use,omitempty"`

	// For "tool_result" messages
	ToolResult *ToolResult `json:"tool_result,omitempty"`

	// For "result" messages (final)
	Result *ResultMessage `json:"result,omitempty"`

	// For "error" messages
	Error *ErrorMessage `json:"error,omitempty"`

	// For "system" messages
	Subtype string `json:"subtype,omitempty"`
	Data    string `json:"data,omitempty"`
}

// AssistantMessage represents an assistant response message.
type AssistantMessage struct {
	ID           string         `json:"id,omitempty"`
	Type         string         `json:"type,omitempty"`
	Role         string         `json:"role,omitempty"`
	Content      []ContentBlock `json:"content,omitempty"`
	Model        string         `json:"model,omitempty"`
	StopReason   string         `json:"stop_reason,omitempty"`
	StopSequence string         `json:"stop_sequence,omitempty"`
	Usage        *Usage         `json:"usage,omitempty"`
}

// ContentBlock represents a content block in a message.
type ContentBlock struct {
	Type string `json:"type"`

	// For "text" blocks
	Text string `json:"text,omitempty"`

	// For "tool_use" blocks
	ID    string          `json:"id,omitempty"`
	Name  string          `json:"name,omitempty"`
	Input json.RawMessage `json:"input,omitempty"`

	// For "tool_result" blocks
	ToolUseID string `json:"tool_use_id,omitempty"`
	Content   string `json:"content,omitempty"`
	IsError   bool   `json:"is_error,omitempty"`
}

// Delta represents a streaming delta update.
type Delta struct {
	Type string `json:"type,omitempty"`
	Text string `json:"text,omitempty"`
}

// ToolUse represents a tool use event.
type ToolUse struct {
	ID    string          `json:"id"`
	Name  string          `json:"name"`
	Input json.RawMessage `json:"input"`
}

// ToolInput represents common tool input fields.
type ToolInput struct {
	Command     string `json:"command,omitempty"`
	FilePath    string `json:"file_path,omitempty"`
	Content     string `json:"content,omitempty"`
	OldString   string `json:"old_string,omitempty"`
	NewString   string `json:"new_string,omitempty"`
	Pattern     string `json:"pattern,omitempty"`
	URL         string `json:"url,omitempty"`
	Description string `json:"description,omitempty"`
}

// ToolResult represents a tool result event.
type ToolResult struct {
	ToolUseID string `json:"tool_use_id"`
	Content   string `json:"content,omitempty"`
	IsError   bool   `json:"is_error,omitempty"`
}

// ResultMessage represents the final result message.
type ResultMessage struct {
	Type          string `json:"type,omitempty"`
	Subtype       string `json:"subtype,omitempty"`
	CostUSD       float64 `json:"cost_usd,omitempty"`
	DurationMS    int64   `json:"duration_ms,omitempty"`
	DurationAPIMS int64   `json:"duration_api_ms,omitempty"`
	IsError       bool    `json:"is_error,omitempty"`
	NumTurns      int     `json:"num_turns,omitempty"`
	SessionID     string  `json:"session_id,omitempty"`
	TotalCostUSD  float64 `json:"total_cost_usd,omitempty"`
	Usage         *Usage  `json:"usage,omitempty"`
}

// ErrorMessage represents an error in the stream.
type ErrorMessage struct {
	Code    string `json:"code,omitempty"`
	Message string `json:"message,omitempty"`
}

// Usage represents token usage statistics.
type Usage struct {
	InputTokens              int64 `json:"input_tokens,omitempty"`
	OutputTokens             int64 `json:"output_tokens,omitempty"`
	CacheCreationInputTokens int64 `json:"cache_creation_input_tokens,omitempty"`
	CacheReadInputTokens     int64 `json:"cache_read_input_tokens,omitempty"`
}
