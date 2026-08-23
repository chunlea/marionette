package claude

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/chunlea/marionette/pkg/agent/executor"
)

// Parser turns Claude Code stream-json lines into executor.AgentEvents.
//
// Tolerance is the contract: no input ends parsing with an error. Non-JSON
// lines become text events, unknown message types and unknown subtypes become
// system events, and malformed nested payloads fall back to the raw line. A
// CLI upgrade that adds a message type must not break a running task.
type Parser struct {
	// sessionID is the CLI's own session id, needed for --resume.
	sessionID string

	// result holds the final result line once seen. The executor reads it to
	// derive the run outcome and token counts.
	result *ResultMessage
}

// NewParser creates a new Claude parser.
func NewParser() executor.AgentEventParser {
	return &Parser{}
}

// ParseLine parses a single line of Claude Code stream-json output.
func (p *Parser) ParseLine(line []byte) ([]*executor.AgentEvent, error) {
	line = bytes.TrimSpace(line)
	if len(line) == 0 {
		return nil, nil
	}

	var msg StreamMessage
	// Non-JSON output is normal: the CLI writes human-readable notices too.
	// Surface those as text rather than treating them as a failure.
	if err := json.Unmarshal(line, &msg); err != nil {
		return []*executor.AgentEvent{withRaw(executor.NewTextEvent(string(line)), line)}, nil //nolint:nilerr // intentional: non-JSON lines are text
	}

	if msg.SessionID != "" {
		p.sessionID = msg.SessionID
	}

	switch msg.Type {
	case MessageTypeSystem:
		return p.parseSystemMessage(line), nil
	case MessageTypeAssistant:
		return p.parseAssistantMessage(&msg, line), nil
	case MessageTypeUser:
		return p.parseUserMessage(&msg, line), nil
	case MessageTypeResult:
		return p.parseResultMessage(line), nil
	case MessageTypeRateLimitEvent:
		return p.parseRateLimitMessage(line), nil
	default:
		// Unknown type: log and skip. Never an error - the CLI adds message
		// types between releases and a running task must survive that.
		text := "unknown message type"
		if msg.Type != "" {
			text = "unknown message type: " + msg.Type
		}
		return []*executor.AgentEvent{withRaw(executor.NewSystemEvent(text), line)}, nil
	}
}

// systemLine carries the handful of system-message fields worth surfacing in
// logs. Every other field stays available through the event's Raw payload.
type systemLine struct {
	Subtype         string `json:"subtype"`
	HookName        string `json:"hook_name"`
	Outcome         string `json:"outcome"`
	EstimatedTokens int64  `json:"estimated_tokens"`
}

// parseSystemMessage handles system messages of every subtype. None of them
// end the turn.
func (p *Parser) parseSystemMessage(raw []byte) []*executor.AgentEvent {
	var sys systemLine
	_ = json.Unmarshal(raw, &sys)

	if sys.Subtype == SystemSubtypeInit {
		var init InitMessage
		if err := json.Unmarshal(raw, &init); err == nil {
			if init.SessionID != "" {
				p.sessionID = init.SessionID
			}
			return []*executor.AgentEvent{withRaw(executor.NewSystemEvent(fmt.Sprintf(
				"init: session=%s model=%s version=%s permission_mode=%s tools=%d cwd=%s",
				init.SessionID, init.Model, init.ClaudeCodeVersion,
				init.PermissionMode, len(init.Tools), init.CWD,
			)), raw)}
		}
	}

	var b strings.Builder
	b.WriteString("system")
	if sys.Subtype != "" {
		b.WriteString(": " + sys.Subtype)
	}
	if sys.HookName != "" {
		b.WriteString(" hook=" + sys.HookName)
	}
	if sys.Outcome != "" {
		b.WriteString(" outcome=" + sys.Outcome)
	}
	if sys.EstimatedTokens > 0 {
		fmt.Fprintf(&b, " estimated_tokens=%d", sys.EstimatedTokens)
	}

	return []*executor.AgentEvent{withRaw(executor.NewSystemEvent(b.String()), raw)}
}

// parseAssistantMessage turns an assistant line's content blocks into events.
func (p *Parser) parseAssistantMessage(msg *StreamMessage, raw []byte) []*executor.AgentEvent {
	if len(msg.Message) == 0 {
		return nil
	}

	var assistant AssistantMessage
	if err := json.Unmarshal(msg.Message, &assistant); err != nil {
		// Malformed nested payload: keep the line instead of dropping it.
		return []*executor.AgentEvent{withRaw(executor.NewSystemEvent(string(raw)), raw)}
	}

	events := p.contentEvents(assistant.Content, raw)

	// Per-request usage. The authoritative per-run totals come from the final
	// result line; this is emitted for live progress only.
	if assistant.Usage != nil {
		events = append(events, withRaw(usageEvent(assistant.Usage), raw))
	}

	return events
}

// parseUserMessage surfaces tool results, which the CLI echoes back as user
// messages. The previous implementation dropped these entirely.
func (p *Parser) parseUserMessage(msg *StreamMessage, raw []byte) []*executor.AgentEvent {
	if len(msg.Message) == 0 {
		return nil
	}

	var user UserMessage
	if err := json.Unmarshal(msg.Message, &user); err != nil {
		return []*executor.AgentEvent{withRaw(executor.NewSystemEvent(string(raw)), raw)}
	}

	return p.contentEvents(user.Content, raw)
}

// contentEvents converts content blocks into events, skipping block types we
// have no event for.
func (p *Parser) contentEvents(blocks []ContentBlock, raw []byte) []*executor.AgentEvent {
	var events []*executor.AgentEvent
	for i := range blocks {
		if event := p.parseContentBlock(&blocks[i]); event != nil {
			events = append(events, withRaw(event, raw))
		}
	}
	return events
}

// parseContentBlock converts a content block to an AgentEvent, or nil for
// block types that carry no event.
func (p *Parser) parseContentBlock(block *ContentBlock) *executor.AgentEvent {
	switch block.Type {
	case ContentTypeText:
		return executor.NewTextEvent(block.Text)

	case ContentTypeThinking:
		return executor.NewThinkingEvent(block.Thinking)

	case ContentTypeToolUse:
		input := ""
		if len(block.Input) > 0 {
			input = string(block.Input)
		}
		return executor.NewToolUseEvent(block.ID, block.Name, input)

	case ContentTypeToolResult:
		return executor.NewToolResultEvent(block.ToolUseID, block.Content.String(), block.IsError)

	default:
		return nil
	}
}

// parseResultMessage handles the final result line. Its fields are top-level:
// `result` is the assistant's final text, and the outcome is carried by
// `subtype` plus `is_error`.
func (p *Parser) parseResultMessage(raw []byte) []*executor.AgentEvent {
	var result ResultMessage
	if err := json.Unmarshal(raw, &result); err != nil {
		return []*executor.AgentEvent{withRaw(executor.NewSystemEvent(string(raw)), raw)}
	}

	p.result = &result
	if result.SessionID != "" {
		p.sessionID = result.SessionID
	}

	var events []*executor.AgentEvent

	if result.Usage != nil {
		events = append(events, withRaw(usageEvent(result.Usage), raw))
	}

	if !result.Succeeded() {
		events = append(events, withRaw(executor.NewErrorEvent(result.FailureReason()), raw))
	}

	summary := fmt.Sprintf("result: subtype=%s is_error=%t turns=%d duration_ms=%d cost_usd=%.6f",
		result.Subtype, result.IsError, result.NumTurns, result.DurationMS, result.TotalCostUSD)
	events = append(events, withRaw(executor.NewSystemEvent(summary), raw))

	return events
}

// parseRateLimitMessage passes a rate limit notice through as a system event.
// It carries no turn semantics and must not end the turn.
func (p *Parser) parseRateLimitMessage(raw []byte) []*executor.AgentEvent {
	var msg RateLimitMessage
	if err := json.Unmarshal(raw, &msg); err != nil {
		return []*executor.AgentEvent{withRaw(executor.NewSystemEvent(string(raw)), raw)}
	}

	text := fmt.Sprintf("rate_limit: status=%s type=%s resets_at=%d",
		msg.RateLimit.Status, msg.RateLimit.RateLimitType, msg.RateLimit.ResetsAt)
	return []*executor.AgentEvent{withRaw(executor.NewSystemEvent(text), raw)}
}

// usageEvent builds a usage event from a wire Usage block.
func usageEvent(u *Usage) *executor.AgentEvent {
	return executor.NewUsageEvent(
		u.InputTokens,
		u.OutputTokens,
		u.CacheReadInputTokens,
		u.CacheCreationInputTokens,
	)
}

// withRaw attaches the originating line to an event for debugging and storage.
func withRaw(event *executor.AgentEvent, raw []byte) *executor.AgentEvent {
	event.Raw = raw
	return event
}

// Flush returns any buffered events. The Claude parser is line-synchronous and
// buffers nothing.
func (p *Parser) Flush() ([]*executor.AgentEvent, error) {
	return nil, nil
}

// Reset clears the parser state so the instance can drive a new run.
func (p *Parser) Reset() {
	p.sessionID = ""
	p.result = nil
}

// SessionID returns the last seen CLI session ID, which is what --resume takes.
func (p *Parser) SessionID() string {
	return p.sessionID
}

// Result returns the final result line, or nil if the run produced none (the
// CLI was killed, timed out, or crashed before finishing the turn).
func (p *Parser) Result() *ResultMessage {
	return p.result
}

// init registers the Claude parser with the default registry.
func init() {
	executor.RegisterParser("claude", NewParser)
}
