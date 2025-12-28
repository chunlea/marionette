package claude

import (
	"encoding/json"
	"testing"

	"github.com/chunlea/marionette/pkg/agent/executor"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEventParser_AgentType(t *testing.T) {
	p := NewEventParser()
	assert.Equal(t, "claude", p.AgentType())
}

func TestEventParser_Parse_NonJSON(t *testing.T) {
	p := NewEventParser()

	tests := []struct {
		name       string
		stream     string
		content    string
		wantType   executor.EventType
		wantOutput string
	}{
		{
			name:       "stdout",
			stream:     "stdout",
			content:    "Hello world",
			wantType:   executor.EventText,
			wantOutput: "Hello world",
		},
		{
			name:       "stderr",
			stream:     "stderr",
			content:    "Error occurred",
			wantType:   executor.EventError,
			wantOutput: "Error occurred",
		},
		{
			name:       "system",
			stream:     "system",
			content:    "System message",
			wantType:   executor.EventSystem,
			wantOutput: "System message",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			events, err := p.Parse(tt.stream, []byte(tt.content))
			require.NoError(t, err)
			require.Len(t, events, 1)
			assert.Equal(t, tt.wantType, events[0].Type)
			assert.Equal(t, tt.wantOutput, events[0].Content)
		})
	}
}

func TestEventParser_Parse_EmptyContent(t *testing.T) {
	p := NewEventParser()

	events, err := p.Parse("stdout", []byte{})
	require.NoError(t, err)
	assert.Nil(t, events)
}

func TestEventParser_Parse_InitMessage(t *testing.T) {
	p := NewEventParser()

	msg := `{"type":"init","session_id":"sess_abc123","model":"claude-3"}`
	events, err := p.Parse("json", []byte(msg))
	require.NoError(t, err)
	require.Len(t, events, 1)

	assert.Equal(t, executor.EventSystem, events[0].Type)
	assert.Contains(t, events[0].Content, "sess_abc123")
	assert.Equal(t, "sess_abc123", p.SessionID())
}

func TestEventParser_Parse_AssistantMessage(t *testing.T) {
	p := NewEventParser()

	msg := `{
		"type": "assistant",
		"message": {
			"content": [
				{"type": "text", "text": "Hello!"},
				{"type": "text", "text": " How can I help?"}
			],
			"usage": {
				"input_tokens": 100,
				"output_tokens": 50
			}
		}
	}`

	events, err := p.Parse("json", []byte(msg))
	require.NoError(t, err)
	require.Len(t, events, 3) // 2 text + 1 usage

	assert.Equal(t, executor.EventText, events[0].Type)
	assert.Equal(t, "Hello!", events[0].Content)

	assert.Equal(t, executor.EventText, events[1].Type)
	assert.Equal(t, " How can I help?", events[1].Content)

	assert.Equal(t, executor.EventUsage, events[2].Type)
	assert.Equal(t, int64(100), events[2].Usage.InputTokens)
	assert.Equal(t, int64(50), events[2].Usage.OutputTokens)
}

func TestEventParser_Parse_ContentBlockDelta(t *testing.T) {
	p := NewEventParser()

	msg := `{"type":"content_block_delta","delta":{"type":"text_delta","text":"streaming "}}`
	events, err := p.Parse("json", []byte(msg))
	require.NoError(t, err)
	require.Len(t, events, 1)

	assert.Equal(t, executor.EventText, events[0].Type)
	assert.Equal(t, "streaming ", events[0].Content)
}

func TestEventParser_Parse_ToolUse(t *testing.T) {
	p := NewEventParser()

	msg := `{
		"type": "tool_use",
		"tool_use": {
			"id": "tool_123",
			"name": "bash",
			"input": {"command": "ls -la"}
		}
	}`

	events, err := p.Parse("json", []byte(msg))
	require.NoError(t, err)
	require.Len(t, events, 1)

	assert.Equal(t, executor.EventToolUse, events[0].Type)
	assert.Equal(t, "tool_123", events[0].Tool.ID)
	assert.Equal(t, "bash", events[0].Tool.Name)

	var input map[string]string
	err = json.Unmarshal(events[0].Tool.Input, &input)
	require.NoError(t, err)
	assert.Equal(t, "ls -la", input["command"])
}

func TestEventParser_Parse_ToolResult(t *testing.T) {
	p := NewEventParser()

	msg := `{
		"type": "tool_result",
		"tool_result": {
			"tool_use_id": "tool_123",
			"content": "file1.go\nfile2.go",
			"is_error": false
		}
	}`

	events, err := p.Parse("json", []byte(msg))
	require.NoError(t, err)
	require.Len(t, events, 1)

	assert.Equal(t, executor.EventToolResult, events[0].Type)
	assert.Equal(t, "tool_123", events[0].ToolResult.ToolUseID)
	assert.Equal(t, "file1.go\nfile2.go", events[0].ToolResult.Output)
	assert.False(t, events[0].ToolResult.IsError)
}

func TestEventParser_Parse_ToolResultError(t *testing.T) {
	p := NewEventParser()

	msg := `{
		"type": "tool_result",
		"tool_result": {
			"tool_use_id": "tool_456",
			"content": "command not found",
			"is_error": true
		}
	}`

	events, err := p.Parse("json", []byte(msg))
	require.NoError(t, err)
	require.Len(t, events, 1)

	assert.Equal(t, executor.EventToolResult, events[0].Type)
	assert.True(t, events[0].ToolResult.IsError)
}

func TestEventParser_Parse_Result(t *testing.T) {
	p := NewEventParser()

	msg := `{
		"type": "result",
		"result": {
			"num_turns": 5,
			"cost_usd": 0.05,
			"usage": {
				"input_tokens": 500,
				"output_tokens": 200,
				"cache_creation_input_tokens": 100,
				"cache_read_input_tokens": 50
			}
		}
	}`

	events, err := p.Parse("json", []byte(msg))
	require.NoError(t, err)
	require.Len(t, events, 1)

	assert.Equal(t, executor.EventUsage, events[0].Type)
	assert.Equal(t, int64(500), events[0].Usage.InputTokens)
	assert.Equal(t, int64(200), events[0].Usage.OutputTokens)
	assert.Equal(t, int64(100), events[0].Usage.CacheCreationTokens)
	assert.Equal(t, int64(50), events[0].Usage.CacheReadTokens)
	assert.Equal(t, 0.05, events[0].Usage.CostUSD)
}

func TestEventParser_Parse_ResultWithError(t *testing.T) {
	p := NewEventParser()

	msg := `{"type":"result","result":{"is_error":true}}`
	events, err := p.Parse("json", []byte(msg))
	require.NoError(t, err)
	require.Len(t, events, 1)

	assert.Equal(t, executor.EventError, events[0].Type)
	assert.Contains(t, events[0].Content, "error")
}

func TestEventParser_Parse_Error(t *testing.T) {
	p := NewEventParser()

	msg := `{"type":"error","error":{"code":"rate_limit","message":"Rate limit exceeded"}}`
	events, err := p.Parse("json", []byte(msg))
	require.NoError(t, err)
	require.Len(t, events, 1)

	assert.Equal(t, executor.EventError, events[0].Type)
	assert.Equal(t, "Rate limit exceeded", events[0].Content)
}

func TestEventParser_Parse_System(t *testing.T) {
	p := NewEventParser()

	msg := `{"type":"system","data":"API cost: $0.05"}`
	events, err := p.Parse("json", []byte(msg))
	require.NoError(t, err)
	require.Len(t, events, 1)

	assert.Equal(t, executor.EventSystem, events[0].Type)
	assert.Equal(t, "API cost: $0.05", events[0].Content)
}

func TestEventParser_Parse_InvalidJSON(t *testing.T) {
	p := NewEventParser()

	events, err := p.Parse("json", []byte("not valid json"))
	require.NoError(t, err)
	require.Len(t, events, 1)

	// Should treat as text
	assert.Equal(t, executor.EventText, events[0].Type)
	assert.Equal(t, "not valid json", events[0].Content)
}

func TestEventParser_Parse_UnknownType(t *testing.T) {
	p := NewEventParser()

	msg := `{"type":"unknown_type","data":"something"}`
	events, err := p.Parse("json", []byte(msg))
	require.NoError(t, err)

	// Should return empty for unknown types
	assert.Empty(t, events)
}

func TestEventParser_Parse_AssistantWithToolUse(t *testing.T) {
	p := NewEventParser()

	msg := `{
		"type": "assistant",
		"message": {
			"content": [
				{"type": "text", "text": "Let me run that command."},
				{"type": "tool_use", "id": "tool_789", "name": "bash", "input": {"command": "pwd"}}
			]
		}
	}`

	events, err := p.Parse("json", []byte(msg))
	require.NoError(t, err)
	require.Len(t, events, 2)

	assert.Equal(t, executor.EventText, events[0].Type)
	assert.Equal(t, "Let me run that command.", events[0].Content)

	assert.Equal(t, executor.EventToolUse, events[1].Type)
	assert.Equal(t, "tool_789", events[1].Tool.ID)
	assert.Equal(t, "bash", events[1].Tool.Name)
}

func TestEventParser_Reset(t *testing.T) {
	p := NewEventParser()

	// Set some state
	_, _ = p.Parse("json", []byte(`{"type":"init","session_id":"sess_test"}`))
	assert.Equal(t, "sess_test", p.SessionID())

	// Reset
	p.Reset()
	assert.Empty(t, p.SessionID())
}

func TestEventParser_Flush(t *testing.T) {
	p := NewEventParser()

	events, err := p.Flush()
	require.NoError(t, err)
	assert.Nil(t, events)
}

func TestEventParser_Interface(t *testing.T) {
	var _ executor.AgentEventParser = NewEventParser()
}
