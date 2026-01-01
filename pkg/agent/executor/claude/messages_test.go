package claude

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStreamMessage_Unmarshal_System(t *testing.T) {
	data := `{"type":"system","subtype":"init","data":"Starting"}`

	var msg StreamMessage
	err := json.Unmarshal([]byte(data), &msg)

	require.NoError(t, err)
	assert.Equal(t, MessageTypeSystem, msg.Type)
	assert.Equal(t, "init", msg.Subtype)
	assert.Equal(t, "Starting", msg.Data)
}

func TestStreamMessage_Unmarshal_Result(t *testing.T) {
	data := `{"type":"result","result":{"success":true,"exit_code":0,"session_id":"sess_123"}}`

	var msg StreamMessage
	err := json.Unmarshal([]byte(data), &msg)

	require.NoError(t, err)
	assert.Equal(t, MessageTypeResult, msg.Type)
	require.NotNil(t, msg.Result)
	assert.True(t, msg.Result.Success)
	assert.Equal(t, 0, msg.Result.ExitCode)
	assert.Equal(t, "sess_123", msg.Result.SessionID)
}

func TestAssistantMessage_Unmarshal(t *testing.T) {
	data := `{"role":"assistant","content":[{"type":"text","text":"Hello"}],"model":"claude-3-5-sonnet"}`

	var msg AssistantMessage
	err := json.Unmarshal([]byte(data), &msg)

	require.NoError(t, err)
	assert.Equal(t, "assistant", msg.Role)
	assert.Equal(t, "claude-3-5-sonnet", msg.Model)
	require.Len(t, msg.Content, 1)
	assert.Equal(t, ContentTypeText, msg.Content[0].Type)
	assert.Equal(t, "Hello", msg.Content[0].Text)
}

func TestContentBlock_Unmarshal_Text(t *testing.T) {
	data := `{"type":"text","text":"Some text content"}`

	var block ContentBlock
	err := json.Unmarshal([]byte(data), &block)

	require.NoError(t, err)
	assert.Equal(t, ContentTypeText, block.Type)
	assert.Equal(t, "Some text content", block.Text)
}

func TestContentBlock_Unmarshal_Thinking(t *testing.T) {
	data := `{"type":"thinking","thinking":"Let me think..."}`

	var block ContentBlock
	err := json.Unmarshal([]byte(data), &block)

	require.NoError(t, err)
	assert.Equal(t, ContentTypeThinking, block.Type)
	assert.Equal(t, "Let me think...", block.Thinking)
}

func TestContentBlock_Unmarshal_ToolUse(t *testing.T) {
	data := `{"type":"tool_use","id":"toolu_123","name":"Bash","input":{"command":"ls"}}`

	var block ContentBlock
	err := json.Unmarshal([]byte(data), &block)

	require.NoError(t, err)
	assert.Equal(t, ContentTypeToolUse, block.Type)
	assert.Equal(t, "toolu_123", block.ID)
	assert.Equal(t, "Bash", block.Name)
	assert.Equal(t, `{"command":"ls"}`, string(block.Input))
}

func TestContentBlock_Unmarshal_ToolResult(t *testing.T) {
	data := `{"type":"tool_result","tool_use_id":"toolu_123","content":"output","is_error":false}`

	var block ContentBlock
	err := json.Unmarshal([]byte(data), &block)

	require.NoError(t, err)
	assert.Equal(t, ContentTypeToolResult, block.Type)
	assert.Equal(t, "toolu_123", block.ToolUseID)
	assert.Equal(t, "output", block.Content)
	assert.False(t, block.IsError)
}

func TestUsage_Unmarshal(t *testing.T) {
	data := `{"input_tokens":1000,"output_tokens":500,"cache_creation_input_tokens":100,"cache_read_input_tokens":200}`

	var usage Usage
	err := json.Unmarshal([]byte(data), &usage)

	require.NoError(t, err)
	assert.Equal(t, int64(1000), usage.InputTokens)
	assert.Equal(t, int64(500), usage.OutputTokens)
	assert.Equal(t, int64(100), usage.CacheCreationInputTokens)
	assert.Equal(t, int64(200), usage.CacheReadInputTokens)
}

func TestResultMessage_Unmarshal_Full(t *testing.T) {
	data := `{
		"success": true,
		"exit_code": 0,
		"duration_ms": 5000,
		"num_turns": 3,
		"session_id": "sess_abc",
		"total_cost_usd": "0.05",
		"usage": {"input_tokens": 100, "output_tokens": 50}
	}`

	var result ResultMessage
	err := json.Unmarshal([]byte(data), &result)

	require.NoError(t, err)
	assert.True(t, result.Success)
	assert.Equal(t, 0, result.ExitCode)
	assert.Equal(t, int64(5000), result.Duration)
	assert.Equal(t, 3, result.NumTurns)
	assert.Equal(t, "sess_abc", result.SessionID)
	assert.Equal(t, "0.05", result.TotalCost)
	require.NotNil(t, result.Usage)
	assert.Equal(t, int64(100), result.Usage.InputTokens)
}

func TestConstants(t *testing.T) {
	// Message types
	assert.Equal(t, "system", MessageTypeSystem)
	assert.Equal(t, "assistant", MessageTypeAssistant)
	assert.Equal(t, "user", MessageTypeUser)
	assert.Equal(t, "result", MessageTypeResult)

	// Content types
	assert.Equal(t, "text", ContentTypeText)
	assert.Equal(t, "thinking", ContentTypeThinking)
	assert.Equal(t, "tool_use", ContentTypeToolUse)
	assert.Equal(t, "tool_result", ContentTypeToolResult)

	// System subtypes
	assert.Equal(t, "init", SystemSubtypeInit)
	assert.Equal(t, "status", SystemSubtypeStatus)
	assert.Equal(t, "error", SystemSubtypeError)
}
