// Package claude implements the Claude Code executor.
package claude

import (
	"encoding/json"
	"time"

	"github.com/chunlea/marionette/pkg/agent/executor"
)

// EventParser parses Claude Code stream-json output into AgentEvents.
type EventParser struct {
	// Accumulated text for streaming deltas
	textBuffer string

	// Session ID captured from init message
	sessionID string
}

// NewEventParser creates a new Claude event parser.
func NewEventParser() *EventParser {
	return &EventParser{}
}

// AgentType returns "claude".
func (p *EventParser) AgentType() string {
	return "claude"
}

// Parse parses a raw log entry and returns events.
func (p *EventParser) Parse(stream string, content []byte) ([]*executor.AgentEvent, error) {
	// Only parse JSON stream (Claude's stream-json format)
	if stream != "json" {
		// For stdout/stderr, create simple text events
		if len(content) > 0 {
			eventType := executor.EventText
			if stream == "stderr" {
				eventType = executor.EventError
			} else if stream == "system" {
				eventType = executor.EventSystem
			}
			return []*executor.AgentEvent{{
				Type:      eventType,
				Content:   string(content),
				Timestamp: time.Now(),
			}}, nil
		}
		return nil, nil
	}

	// Parse the JSON message
	var msg StreamMessage
	if err := json.Unmarshal(content, &msg); err != nil {
		// Not valid JSON, treat as text
		return []*executor.AgentEvent{{
			Type:      executor.EventText,
			Content:   string(content),
			Timestamp: time.Now(),
		}}, nil
	}

	return p.parseMessage(&msg), nil
}

// parseMessage converts a StreamMessage to AgentEvents.
func (p *EventParser) parseMessage(msg *StreamMessage) []*executor.AgentEvent {
	var events []*executor.AgentEvent
	now := time.Now()

	switch msg.Type {
	case "init":
		// Capture session ID, emit system event
		p.sessionID = msg.SessionID
		if msg.SessionID != "" {
			events = append(events, &executor.AgentEvent{
				Type:      executor.EventSystem,
				Content:   "Session started: " + msg.SessionID,
				Timestamp: now,
			})
		}

	case "assistant":
		// Full assistant message - extract content
		if msg.Message != nil {
			for _, block := range msg.Message.Content {
				switch block.Type {
				case "text":
					if block.Text != "" {
						events = append(events, &executor.AgentEvent{
							Type:      executor.EventText,
							Content:   block.Text,
							Timestamp: now,
						})
					}
				case "tool_use":
					events = append(events, &executor.AgentEvent{
						Type: executor.EventToolUse,
						Tool: &executor.ToolUseEvent{
							ID:    block.ID,
							Name:  block.Name,
							Input: block.Input,
						},
						Timestamp: now,
					})
				}
			}
			// Emit usage event if available
			if msg.Message.Usage != nil {
				events = append(events, &executor.AgentEvent{
					Type: executor.EventUsage,
					Usage: &executor.UsageEvent{
						InputTokens:        msg.Message.Usage.InputTokens,
						OutputTokens:       msg.Message.Usage.OutputTokens,
						CacheCreationTokens: msg.Message.Usage.CacheCreationInputTokens,
						CacheReadTokens:    msg.Message.Usage.CacheReadInputTokens,
					},
					Timestamp: now,
				})
			}
		}

	case "content_block_delta":
		// Streaming text delta - accumulate
		if msg.Delta != nil && msg.Delta.Text != "" {
			p.textBuffer += msg.Delta.Text
			// Emit partial text event for real-time streaming
			events = append(events, &executor.AgentEvent{
				Type:      executor.EventText,
				Content:   msg.Delta.Text,
				Timestamp: now,
			})
		}

	case "tool_use":
		// Tool invocation event
		if msg.ToolUse != nil {
			events = append(events, &executor.AgentEvent{
				Type: executor.EventToolUse,
				Tool: &executor.ToolUseEvent{
					ID:    msg.ToolUse.ID,
					Name:  msg.ToolUse.Name,
					Input: msg.ToolUse.Input,
				},
				Timestamp: now,
			})
		}

	case "tool_result":
		// Tool result event
		if msg.ToolResult != nil {
			events = append(events, &executor.AgentEvent{
				Type: executor.EventToolResult,
				ToolResult: &executor.ToolResultEvent{
					ToolUseID: msg.ToolResult.ToolUseID,
					Output:    msg.ToolResult.Content,
					IsError:   msg.ToolResult.IsError,
				},
				Timestamp: now,
			})
		}

	case "result":
		// Final result with usage stats
		if msg.Result != nil {
			if msg.Result.Usage != nil {
				events = append(events, &executor.AgentEvent{
					Type: executor.EventUsage,
					Usage: &executor.UsageEvent{
						InputTokens:        msg.Result.Usage.InputTokens,
						OutputTokens:       msg.Result.Usage.OutputTokens,
						CacheCreationTokens: msg.Result.Usage.CacheCreationInputTokens,
						CacheReadTokens:    msg.Result.Usage.CacheReadInputTokens,
						CostUSD:            msg.Result.CostUSD,
					},
					Timestamp: now,
				})
			}
			if msg.Result.IsError {
				events = append(events, &executor.AgentEvent{
					Type:      executor.EventError,
					Content:   "Task completed with error",
					Timestamp: now,
				})
			}
		}

	case "error":
		// Error message
		if msg.Error != nil {
			events = append(events, &executor.AgentEvent{
				Type:      executor.EventError,
				Content:   msg.Error.Message,
				Timestamp: now,
			})
		}

	case "system":
		// System message
		if msg.Data != "" {
			events = append(events, &executor.AgentEvent{
				Type:      executor.EventSystem,
				Content:   msg.Data,
				Timestamp: now,
			})
		}
	}

	return events
}

// Flush returns any pending events (e.g., buffered text).
func (p *EventParser) Flush() ([]*executor.AgentEvent, error) {
	// Currently we emit text events immediately, so nothing to flush
	// In the future, we might want to batch small deltas
	return nil, nil
}

// Reset clears internal state for a new task.
func (p *EventParser) Reset() {
	p.textBuffer = ""
	p.sessionID = ""
}

// SessionID returns the captured session ID.
func (p *EventParser) SessionID() string {
	return p.sessionID
}

// Ensure EventParser implements the interface.
var _ executor.AgentEventParser = (*EventParser)(nil)
