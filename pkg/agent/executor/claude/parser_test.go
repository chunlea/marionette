package claude

import (
	"testing"

	"github.com/chunlea/marionette/pkg/agent/executor"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParser_ParseLine_Empty(t *testing.T) {
	parser := NewParser()

	events, err := parser.ParseLine([]byte{})
	assert.NoError(t, err)
	assert.Nil(t, events)
}

func TestParser_ParseLine_InvalidJSON(t *testing.T) {
	parser := NewParser()

	events, err := parser.ParseLine([]byte("not json"))
	require.NoError(t, err)
	require.Len(t, events, 1)
	assert.Equal(t, executor.EventText, events[0].Type)
	assert.Equal(t, "not json", events[0].Text)
}

func TestParser_ParseLine_SystemMessage(t *testing.T) {
	parser := NewParser()

	line := []byte(`{"type":"system","subtype":"init","data":"Starting Claude Code"}`)
	events, err := parser.ParseLine(line)

	require.NoError(t, err)
	require.Len(t, events, 1)
	assert.Equal(t, executor.EventSystem, events[0].Type)
	assert.Equal(t, "Starting Claude Code", events[0].Text)
}

func TestParser_ParseLine_AssistantMessage_Text(t *testing.T) {
	parser := NewParser()

	line := []byte(`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"Hello, I can help you with that."}]}}`)
	events, err := parser.ParseLine(line)

	require.NoError(t, err)
	require.Len(t, events, 1)
	assert.Equal(t, executor.EventText, events[0].Type)
	assert.Equal(t, "Hello, I can help you with that.", events[0].Text)
}

func TestParser_ParseLine_AssistantMessage_Thinking(t *testing.T) {
	parser := NewParser()

	line := []byte(`{"type":"assistant","message":{"role":"assistant","content":[{"type":"thinking","thinking":"Let me analyze this..."}]}}`)
	events, err := parser.ParseLine(line)

	require.NoError(t, err)
	require.Len(t, events, 1)
	assert.Equal(t, executor.EventThinking, events[0].Type)
	assert.Equal(t, "Let me analyze this...", events[0].Text)
}

func TestParser_ParseLine_AssistantMessage_ToolUse(t *testing.T) {
	parser := NewParser()

	line := []byte(`{"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","id":"tool_123","name":"Bash","input":{"command":"ls -la"}}]}}`)
	events, err := parser.ParseLine(line)

	require.NoError(t, err)
	require.Len(t, events, 1)
	assert.Equal(t, executor.EventToolUse, events[0].Type)
	require.NotNil(t, events[0].ToolUse)
	assert.Equal(t, "tool_123", events[0].ToolUse.ID)
	assert.Equal(t, "Bash", events[0].ToolUse.Name)
	assert.Equal(t, `{"command":"ls -la"}`, events[0].ToolUse.Input)
}

func TestParser_ParseLine_AssistantMessage_ToolResult(t *testing.T) {
	parser := NewParser()

	line := []byte(`{"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_result","tool_use_id":"tool_123","content":"file1.txt\nfile2.txt","is_error":false}]}}`)
	events, err := parser.ParseLine(line)

	require.NoError(t, err)
	require.Len(t, events, 1)
	assert.Equal(t, executor.EventToolResult, events[0].Type)
	require.NotNil(t, events[0].ToolResult)
	assert.Equal(t, "tool_123", events[0].ToolResult.ToolUseID)
	assert.Equal(t, "file1.txt\nfile2.txt", events[0].ToolResult.Output)
	assert.False(t, events[0].ToolResult.IsError)
}

func TestParser_ParseLine_AssistantMessage_WithUsage(t *testing.T) {
	parser := NewParser()

	line := []byte(`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"Done!"}],"usage":{"input_tokens":100,"output_tokens":50}}}`)
	events, err := parser.ParseLine(line)

	require.NoError(t, err)
	require.Len(t, events, 2)

	// Text event
	assert.Equal(t, executor.EventText, events[0].Type)
	assert.Equal(t, "Done!", events[0].Text)

	// Usage event
	assert.Equal(t, executor.EventUsage, events[1].Type)
	require.NotNil(t, events[1].Usage)
	assert.Equal(t, int64(100), events[1].Usage.InputTokens)
	assert.Equal(t, int64(50), events[1].Usage.OutputTokens)
}

func TestParser_ParseLine_AssistantMessage_MultipleBlocks(t *testing.T) {
	parser := NewParser()

	line := []byte(`{"type":"assistant","message":{"role":"assistant","content":[{"type":"thinking","thinking":"Planning..."},{"type":"text","text":"Here's my plan:"},{"type":"tool_use","id":"tool_1","name":"Read","input":{"path":"README.md"}}]}}`)
	events, err := parser.ParseLine(line)

	require.NoError(t, err)
	require.Len(t, events, 3)

	assert.Equal(t, executor.EventThinking, events[0].Type)
	assert.Equal(t, executor.EventText, events[1].Type)
	assert.Equal(t, executor.EventToolUse, events[2].Type)
}

func TestParser_ParseLine_ResultMessage_Success(t *testing.T) {
	parser := NewParser()

	line := []byte(`{"type":"result","result":{"success":true,"exit_code":0,"session_id":"sess_abc123","usage":{"input_tokens":500,"output_tokens":200}}}`)
	events, err := parser.ParseLine(line)

	require.NoError(t, err)
	require.Len(t, events, 2)

	// Usage event
	assert.Equal(t, executor.EventUsage, events[0].Type)

	// System event for completion
	assert.Equal(t, executor.EventSystem, events[1].Type)
	assert.Equal(t, "Task completed", events[1].Text)

	// Session ID should be tracked
	p := parser.(*Parser)
	assert.Equal(t, "sess_abc123", p.SessionID())
}

func TestParser_ParseLine_ResultMessage_Failed(t *testing.T) {
	parser := NewParser()

	line := []byte(`{"type":"result","result":{"success":false,"exit_code":1,"error":"API error"}}`)
	events, err := parser.ParseLine(line)

	require.NoError(t, err)
	require.Len(t, events, 2)

	// Error event
	assert.Equal(t, executor.EventError, events[0].Type)
	assert.Equal(t, "API error", events[0].Text)

	// System event for failure
	assert.Equal(t, executor.EventSystem, events[1].Type)
	assert.Equal(t, "Task failed", events[1].Text)
}

func TestParser_ParseLine_ResultMessage_Interrupted(t *testing.T) {
	parser := NewParser()

	line := []byte(`{"type":"result","result":{"success":false,"exit_code":130,"interrupted":true}}`)
	events, err := parser.ParseLine(line)

	require.NoError(t, err)
	require.Len(t, events, 1)

	assert.Equal(t, executor.EventSystem, events[0].Type)
	assert.Equal(t, "Task interrupted", events[0].Text)
}

func TestParser_ParseLine_UserMessage(t *testing.T) {
	parser := NewParser()

	line := []byte(`{"type":"user","message":"What is the time?"}`)
	events, err := parser.ParseLine(line)

	assert.NoError(t, err)
	assert.Nil(t, events) // User messages are ignored
}

func TestParser_ParseLine_UnknownType(t *testing.T) {
	parser := NewParser()

	line := []byte(`{"type":"unknown","data":"something"}`)
	events, err := parser.ParseLine(line)

	require.NoError(t, err)
	require.Len(t, events, 1)
	assert.Equal(t, executor.EventSystem, events[0].Type)
}

func TestParser_SessionID_Tracking(t *testing.T) {
	parser := NewParser().(*Parser)

	// Initially empty
	assert.Empty(t, parser.SessionID())

	// Session ID from message
	_, _ = parser.ParseLine([]byte(`{"type":"system","session_id":"sess_123"}`))
	assert.Equal(t, "sess_123", parser.SessionID())

	// Session ID from result
	_, _ = parser.ParseLine([]byte(`{"type":"result","result":{"success":true,"session_id":"sess_456"}}`))
	assert.Equal(t, "sess_456", parser.SessionID())
}

func TestParser_Flush(t *testing.T) {
	parser := NewParser()

	events, err := parser.Flush()
	assert.NoError(t, err)
	assert.Nil(t, events)
}

func TestParser_Reset(t *testing.T) {
	parser := NewParser().(*Parser)

	// Set some state
	_, _ = parser.ParseLine([]byte(`{"type":"system","session_id":"sess_123"}`))
	assert.Equal(t, "sess_123", parser.SessionID())

	// Reset
	parser.Reset()
	assert.Empty(t, parser.SessionID())
}

func TestParser_Registration(t *testing.T) {
	// The parser should be registered with the default registry
	parser, err := executor.GetParser("claude")
	require.NoError(t, err)
	assert.NotNil(t, parser)
}
