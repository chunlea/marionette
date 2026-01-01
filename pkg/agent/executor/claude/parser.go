package claude

import (
	"encoding/json"

	"github.com/chunlea/marionette/pkg/agent/executor"
)

// Parser parses Claude Code stream-json output into AgentEvents.
type Parser struct {
	// Last seen session ID for resume support
	sessionID string
}

// NewParser creates a new Claude parser.
func NewParser() executor.AgentEventParser {
	return &Parser{}
}

// ParseLine parses a single line of Claude Code stream-json output.
func (p *Parser) ParseLine(line []byte) ([]*executor.AgentEvent, error) {
	if len(line) == 0 {
		return nil, nil
	}

	var msg StreamMessage
	// Attempt to parse as JSON. If it fails, we treat the line as raw text.
	// This is not an error condition - Claude may output non-JSON debug info.
	parseErr := json.Unmarshal(line, &msg)
	if parseErr != nil {
		event := executor.NewTextEvent(string(line))
		event.Raw = line
		// Intentionally returning nil error: we handle parse failures gracefully
		// by converting unparseable lines to text events.
		return []*executor.AgentEvent{event}, nil //nolint:nilerr // intentional: parse errors are handled as text events
	}

	// Track session ID for resume
	if msg.SessionID != "" {
		p.sessionID = msg.SessionID
	}

	switch msg.Type {
	case MessageTypeSystem:
		return p.parseSystemMessage(&msg, line)
	case MessageTypeAssistant:
		return p.parseAssistantMessage(&msg, line)
	case MessageTypeResult:
		return p.parseResultMessage(&msg, line)
	case MessageTypeUser:
		// User messages are echoed back, we can ignore or treat as system
		return nil, nil
	default:
		// Unknown type, emit as system event
		event := executor.NewSystemEvent(string(line))
		event.Raw = line
		return []*executor.AgentEvent{event}, nil
	}
}

// parseSystemMessage handles system messages.
func (p *Parser) parseSystemMessage(msg *StreamMessage, raw []byte) ([]*executor.AgentEvent, error) {
	var text string
	if msg.Data != "" {
		text = msg.Data
	} else {
		text = msg.Subtype
	}

	event := executor.NewSystemEvent(text)
	event.Raw = raw
	return []*executor.AgentEvent{event}, nil
}

// parseAssistantMessage handles assistant messages with content blocks.
func (p *Parser) parseAssistantMessage(msg *StreamMessage, raw []byte) ([]*executor.AgentEvent, error) {
	if msg.Message == nil {
		return nil, nil
	}

	var assistant AssistantMessage
	// Attempt to parse the nested message. If it fails, emit as raw system event.
	// This gracefully handles malformed messages without failing the entire parse.
	parseErr := json.Unmarshal(msg.Message, &assistant)
	if parseErr != nil {
		event := executor.NewSystemEvent(string(raw))
		event.Raw = raw
		// Intentionally returning nil error: malformed messages are emitted as system events.
		return []*executor.AgentEvent{event}, nil //nolint:nilerr // intentional: malformed messages handled gracefully
	}

	var events []*executor.AgentEvent

	for _, block := range assistant.Content {
		event := p.parseContentBlock(&block)
		if event != nil {
			event.Raw = raw
			events = append(events, event)
		}
	}

	// Emit usage if present
	if assistant.Usage != nil {
		usageEvent := executor.NewUsageEvent(
			assistant.Usage.InputTokens,
			assistant.Usage.OutputTokens,
			assistant.Usage.CacheReadInputTokens,
			assistant.Usage.CacheCreationInputTokens,
		)
		usageEvent.Raw = raw
		events = append(events, usageEvent)
	}

	return events, nil
}

// parseContentBlock converts a content block to an AgentEvent.
func (p *Parser) parseContentBlock(block *ContentBlock) *executor.AgentEvent {
	switch block.Type {
	case ContentTypeText:
		return executor.NewTextEvent(block.Text)

	case ContentTypeThinking:
		return executor.NewThinkingEvent(block.Thinking)

	case ContentTypeToolUse:
		input := ""
		if block.Input != nil {
			input = string(block.Input)
		}
		return executor.NewToolUseEvent(block.ID, block.Name, input)

	case ContentTypeToolResult:
		return executor.NewToolResultEvent(block.ToolUseID, block.Content, block.IsError)

	default:
		return nil
	}
}

// parseResultMessage handles the final result message.
func (p *Parser) parseResultMessage(msg *StreamMessage, raw []byte) ([]*executor.AgentEvent, error) {
	if msg.Result == nil {
		return nil, nil
	}

	var events []*executor.AgentEvent

	// Track session ID
	if msg.Result.SessionID != "" {
		p.sessionID = msg.Result.SessionID
	}

	// Emit usage event if present
	if msg.Result.Usage != nil {
		usageEvent := executor.NewUsageEvent(
			msg.Result.Usage.InputTokens,
			msg.Result.Usage.OutputTokens,
			msg.Result.Usage.CacheReadInputTokens,
			msg.Result.Usage.CacheCreationInputTokens,
		)
		usageEvent.Raw = raw
		events = append(events, usageEvent)
	}

	// Emit error event if failed
	if !msg.Result.Success && msg.Result.Error != "" {
		errorEvent := executor.NewErrorEvent(msg.Result.Error)
		errorEvent.Raw = raw
		events = append(events, errorEvent)
	}

	// Emit system event with result summary
	resultText := "Task completed"
	if msg.Result.Interrupted {
		resultText = "Task interrupted"
	} else if !msg.Result.Success {
		resultText = "Task failed"
	}
	systemEvent := executor.NewSystemEvent(resultText)
	systemEvent.Raw = raw
	events = append(events, systemEvent)

	return events, nil
}

// Flush returns any buffered events.
func (p *Parser) Flush() ([]*executor.AgentEvent, error) {
	// No buffering needed for Claude parser
	return nil, nil
}

// Reset clears the parser state.
func (p *Parser) Reset() {
	p.sessionID = ""
}

// SessionID returns the last seen session ID.
func (p *Parser) SessionID() string {
	return p.sessionID
}

// init registers the Claude parser with the default registry.
func init() {
	executor.RegisterParser("claude", NewParser)
}
