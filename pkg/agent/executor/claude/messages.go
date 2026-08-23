// Package claude implements the executor for Claude Code CLI.
//
// The message model in this file is derived from real recorded CLI output
// (see testdata/golden/README.md, CLI 2.1.241). Two rules apply to every type
// declared here:
//
//  1. Unknown fields are ignored, never fatal. The CLI adds fields between
//     releases; a new field must not break the parse path.
//  2. Unknown message types are tolerated by the parser (logged and passed
//     through as system events), never turned into errors.
package claude

import (
	"bytes"
	"encoding/json"
	"strings"
)

// MessageType constants for stream messages.
const (
	MessageTypeSystem    = "system"
	MessageTypeAssistant = "assistant"
	MessageTypeUser      = "user"
	MessageTypeResult    = "result"
	// MessageTypeRateLimitEvent arrives mid-stream and carries no turn
	// semantics; it must not end the turn.
	MessageTypeRateLimitEvent = "rate_limit_event"
)

// SystemSubtype constants observed in the golden recordings. This list is not
// exhaustive and is not treated as such: any other subtype is passed through.
const (
	SystemSubtypeInit           = "init"
	SystemSubtypeHookStarted    = "hook_started"
	SystemSubtypeHookResponse   = "hook_response"
	SystemSubtypeThinkingTokens = "thinking_tokens"
)

// Result subtypes emitted by the CLI on the final `result` line.
const (
	ResultSubtypeSuccess              = "success"
	ResultSubtypeErrorMaxTurns        = "error_max_turns"
	ResultSubtypeErrorDuringExecution = "error_during_execution"
)

// ContentType constants for content blocks.
const (
	ContentTypeText       = "text"
	ContentTypeThinking   = "thinking"
	ContentTypeToolUse    = "tool_use"
	ContentTypeToolResult = "tool_result"
)

// StreamMessage is the envelope shared by every line of stream-json output.
// Only the fields needed to route a line to its typed decoder live here; the
// payload is decoded separately per type because the CLI puts `result` fields
// at the top level, not under a `result` key.
type StreamMessage struct {
	Type    string `json:"type"`
	Subtype string `json:"subtype,omitempty"`

	SessionID string `json:"session_id,omitempty"`
	UUID      string `json:"uuid,omitempty"`

	// Message holds the nested Anthropic message for "assistant" and "user".
	Message json.RawMessage `json:"message,omitempty"`
}

// InitMessage is the `system`/`init` line emitted once at session start.
// Its session_id is what `--resume` takes.
type InitMessage struct {
	Type      string `json:"type"`
	Subtype   string `json:"subtype"`
	SessionID string `json:"session_id"`

	CWD               string   `json:"cwd"`
	Model             string   `json:"model"`
	PermissionMode    string   `json:"permissionMode"`
	Tools             []string `json:"tools"`
	APIKeySource      string   `json:"apiKeySource"`
	ClaudeCodeVersion string   `json:"claude_code_version"`
}

// AssistantMessage is the nested `message` object of an "assistant" line.
// Note that stop_reason and stop_sequence are null mid-turn; decoding JSON
// null into a string is a no-op in Go, so plain strings are safe here.
type AssistantMessage struct {
	ID           string         `json:"id,omitempty"`
	Role         string         `json:"role"`
	Model        string         `json:"model,omitempty"`
	Content      []ContentBlock `json:"content"`
	Usage        *Usage         `json:"usage,omitempty"`
	StopReason   string         `json:"stop_reason,omitempty"`
	StopSequence string         `json:"stop_sequence,omitempty"`
}

// UserMessage is the nested `message` object of a "user" line. The CLI echoes
// tool results back through these, so they carry real content we must surface.
type UserMessage struct {
	Role    string         `json:"role"`
	Content []ContentBlock `json:"content"`
}

// ContentBlock is one block of an Anthropic message's content array.
// The Type field determines which of the remaining fields are populated.
type ContentBlock struct {
	Type string `json:"type"`

	// For type "text".
	Text string `json:"text,omitempty"`

	// For type "thinking".
	Thinking  string `json:"thinking,omitempty"`
	Signature string `json:"signature,omitempty"`

	// For type "tool_use".
	ID    string          `json:"id,omitempty"`
	Name  string          `json:"name,omitempty"`
	Input json.RawMessage `json:"input,omitempty"`

	// For type "tool_result". The wire type of Content is string OR an array
	// of blocks depending on the tool, hence FlexibleText.
	ToolUseID string       `json:"tool_use_id,omitempty"`
	Content   FlexibleText `json:"content,omitempty"`
	IsError   bool         `json:"is_error,omitempty"`
}

// ResultMessage is the final `result` line. Its fields sit at the TOP level of
// the line, and `result` itself is the assistant's final text, not an object.
// Outcome is carried by Subtype plus IsError; there are no success/exit_code
// fields on the wire.
type ResultMessage struct {
	Type      string `json:"type"`
	Subtype   string `json:"subtype"`
	IsError   bool   `json:"is_error"`
	Result    string `json:"result"`
	SessionID string `json:"session_id"`
	UUID      string `json:"uuid"`

	NumTurns       int     `json:"num_turns"`
	DurationMS     int64   `json:"duration_ms"`
	DurationAPIMS  int64   `json:"duration_api_ms"`
	TotalCostUSD   float64 `json:"total_cost_usd"`
	StopReason     string  `json:"stop_reason"`
	TerminalReason string  `json:"terminal_reason"`

	// APIErrorStatus is null on success; its populated shape is unverified, so
	// it is kept raw and only rendered for diagnostics.
	APIErrorStatus json.RawMessage `json:"api_error_status"`

	Usage      *Usage                `json:"usage"`
	ModelUsage map[string]ModelUsage `json:"modelUsage"`

	// PermissionDenials is always an empty list in the golden recordings; the
	// element shape is unverified, so it stays raw rather than being invented.
	PermissionDenials []json.RawMessage `json:"permission_denials"`
}

// Succeeded reports whether the run finished cleanly.
func (r *ResultMessage) Succeeded() bool {
	return !r.IsError && r.Subtype == ResultSubtypeSuccess
}

// FailureReason returns a human-readable reason for a failed run, or "" when
// the run succeeded.
func (r *ResultMessage) FailureReason() string {
	if r.Succeeded() {
		return ""
	}

	var b strings.Builder
	switch r.Subtype {
	case ResultSubtypeErrorMaxTurns:
		b.WriteString("agent stopped: max turns reached")
	case ResultSubtypeErrorDuringExecution:
		b.WriteString("agent stopped: error during execution")
	case "", ResultSubtypeSuccess:
		b.WriteString("agent reported an error")
	default:
		b.WriteString("agent stopped: " + r.Subtype)
	}

	if status := r.apiErrorStatus(); status != "" {
		b.WriteString(" (api_error_status: " + status + ")")
	}
	// The final text usually carries the CLI's own error description.
	if r.Result != "" {
		b.WriteString(": " + r.Result)
	}
	return b.String()
}

// apiErrorStatus returns the raw api_error_status payload when it is present
// and not JSON null.
func (r *ResultMessage) apiErrorStatus() string {
	raw := bytes.TrimSpace(r.APIErrorStatus)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return ""
	}
	return string(raw)
}

// Usage is the snake_case token usage block carried by assistant messages and
// by the final result line. The result line's copy is authoritative for a run.
type Usage struct {
	InputTokens              int64  `json:"input_tokens"`
	OutputTokens             int64  `json:"output_tokens"`
	CacheCreationInputTokens int64  `json:"cache_creation_input_tokens,omitempty"`
	CacheReadInputTokens     int64  `json:"cache_read_input_tokens,omitempty"`
	ServiceTier              string `json:"service_tier,omitempty"`
}

// ModelUsage is the per-model breakdown carried under the result line's
// `modelUsage` key. Unlike Usage, its keys are camelCase on the wire.
type ModelUsage struct {
	InputTokens              int64   `json:"inputTokens"`
	OutputTokens             int64   `json:"outputTokens"`
	CacheReadInputTokens     int64   `json:"cacheReadInputTokens"`
	CacheCreationInputTokens int64   `json:"cacheCreationInputTokens"`
	CostUSD                  float64 `json:"costUSD"`
	ContextWindow            int64   `json:"contextWindow"`
	CanonicalModel           string  `json:"canonicalModel"`
}

// RateLimitMessage is the `rate_limit_event` line. It is informational and
// must be passed through without ending the turn.
type RateLimitMessage struct {
	Type      string        `json:"type"`
	SessionID string        `json:"session_id"`
	RateLimit RateLimitInfo `json:"rate_limit_info"`
}

// RateLimitInfo describes the current rate limit window.
type RateLimitInfo struct {
	Status         string `json:"status"`
	ResetsAt       int64  `json:"resetsAt"`
	RateLimitType  string `json:"rateLimitType"`
	IsUsingOverage bool   `json:"isUsingOverage"`
}

// FlexibleText decodes a JSON value that the Anthropic wire format allows to be
// either a plain string or an array of content blocks (tool results use both).
// Anything else is preserved as its raw JSON so no information is lost.
type FlexibleText string

// String returns the decoded text.
func (f FlexibleText) String() string { return string(f) }

// UnmarshalJSON implements json.Unmarshaler.
func (f *FlexibleText) UnmarshalJSON(data []byte) error {
	data = bytes.TrimSpace(data)
	if len(data) == 0 || bytes.Equal(data, []byte("null")) {
		*f = ""
		return nil
	}

	switch data[0] {
	case '"':
		var s string
		if err := json.Unmarshal(data, &s); err != nil {
			return err
		}
		*f = FlexibleText(s)
		return nil

	case '[':
		var blocks []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		}
		if err := json.Unmarshal(data, &blocks); err != nil {
			// Not an array of blocks we recognise; keep the raw payload rather
			// than failing the whole message.
			*f = FlexibleText(data)
			return nil //nolint:nilerr // intentional: unknown shapes are preserved raw
		}
		var parts []string
		for _, b := range blocks {
			if b.Text != "" {
				parts = append(parts, b.Text)
			}
		}
		*f = FlexibleText(strings.Join(parts, "\n"))
		return nil

	default:
		// Numbers, objects, booleans: keep raw.
		*f = FlexibleText(data)
		return nil
	}
}
