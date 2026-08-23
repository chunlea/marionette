package claude

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// jsonUnmarshal is a thin alias so golden_test.go does not need to import
// encoding/json just to sniff a line's type.
func jsonUnmarshal(data []byte, v any) error { return json.Unmarshal(data, v) }

// TestResultMessage_FromGolden pins the wire types that the previous model got
// wrong: `result` is a string, `total_cost_usd` is a number, and the outcome
// lives in `subtype` + `is_error` rather than invented success/exit_code fields.
func TestResultMessage_FromGolden(t *testing.T) {
	tests := []struct {
		golden       string
		wantSession  string
		wantResult   string
		wantTurns    int
		wantCostMin  float64
		wantInput    int64
		wantOutput   int64
		wantCacheCr  int64
		wantCacheRd  int64
		wantStopWhy  string
		wantModelKey string
	}{
		{
			golden:       goldenBasic,
			wantSession:  goldenBasicSession,
			wantResult:   "hi",
			wantTurns:    1,
			wantCostMin:  0.07,
			wantInput:    2,
			wantOutput:   4,
			wantCacheCr:  16286,
			wantCacheRd:  30274,
			wantStopWhy:  "end_turn",
			wantModelKey: goldenModel,
		},
		{
			golden:       goldenToolUse,
			wantSession:  goldenToolUseSession,
			wantResult:   "marionette-golden",
			wantTurns:    2,
			wantCostMin:  0.12,
			wantInput:    4,
			wantOutput:   123,
			wantCacheCr:  27688,
			wantCacheRd:  71551,
			wantStopWhy:  "end_turn",
			wantModelKey: goldenModel,
		},
		{
			// Resume continues the session basic.jsonl created, so it reports
			// the same session id.
			golden:       goldenResume,
			wantSession:  goldenBasicSession,
			wantResult:   "hi again",
			wantTurns:    1,
			wantCostMin:  0.26,
			wantInput:    2,
			wantOutput:   5,
			wantCacheCr:  66393,
			wantCacheRd:  0,
			wantStopWhy:  "end_turn",
			wantModelKey: goldenModel,
		},
	}

	for _, tt := range tests {
		t.Run(tt.golden, func(t *testing.T) {
			var result ResultMessage
			require.NoError(t, json.Unmarshal(goldenLineOfType(t, tt.golden, MessageTypeResult), &result))

			assert.Equal(t, MessageTypeResult, result.Type)
			assert.Equal(t, ResultSubtypeSuccess, result.Subtype)
			assert.False(t, result.IsError)
			assert.True(t, result.Succeeded())
			assert.Empty(t, result.FailureReason())

			assert.Equal(t, tt.wantResult, result.Result)
			assert.Equal(t, tt.wantSession, result.SessionID)
			assert.Equal(t, tt.wantTurns, result.NumTurns)
			assert.Equal(t, tt.wantStopWhy, result.StopReason)
			assert.GreaterOrEqual(t, result.TotalCostUSD, tt.wantCostMin)
			assert.NotEmpty(t, result.UUID)
			assert.Positive(t, result.DurationMS)

			require.NotNil(t, result.Usage)
			assert.Equal(t, tt.wantInput, result.Usage.InputTokens)
			assert.Equal(t, tt.wantOutput, result.Usage.OutputTokens)
			assert.Equal(t, tt.wantCacheCr, result.Usage.CacheCreationInputTokens)
			assert.Equal(t, tt.wantCacheRd, result.Usage.CacheReadInputTokens)

			// permission_denials is present but empty in every recording; its
			// element shape is unverified, hence json.RawMessage.
			assert.Empty(t, result.PermissionDenials)

			if tt.wantModelKey != "" {
				usage, ok := result.ModelUsage[tt.wantModelKey]
				require.True(t, ok, "modelUsage must be keyed by model name")
				assert.Positive(t, usage.CostUSD)
				assert.Positive(t, usage.ContextWindow)
				assert.Equal(t, tt.wantModelKey, usage.CanonicalModel)
			}
		})
	}
}

// TestResultMessage_RejectsInventedShape guards against a regression to the
// old hand-written model: the CLI never emits success/exit_code/error, and a
// model that expects them silently stops parsing every real result line.
func TestResultMessage_RejectsInventedShape(t *testing.T) {
	line := goldenLineOfType(t, goldenBasic, MessageTypeResult)

	var probe struct {
		Success  *bool            `json:"success"`
		ExitCode *int             `json:"exit_code"`
		Error    *string          `json:"error"`
		Result   *json.RawMessage `json:"result"`
		Cost     *json.RawMessage `json:"total_cost_usd"`
	}
	require.NoError(t, json.Unmarshal(line, &probe))

	assert.Nil(t, probe.Success, "CLI does not emit a success field")
	assert.Nil(t, probe.ExitCode, "CLI does not emit an exit_code field")
	assert.Nil(t, probe.Error, "CLI does not emit a top-level error field")

	require.NotNil(t, probe.Result)
	assert.Equal(t, byte('"'), (*probe.Result)[0], "result is a string, not an object")

	require.NotNil(t, probe.Cost)
	assert.NotEqual(t, byte('"'), (*probe.Cost)[0], "total_cost_usd is a number, not a string")
}

func TestResultMessage_Outcome(t *testing.T) {
	tests := []struct {
		name        string
		result      ResultMessage
		wantOK      bool
		wantReason  string
		containsAll []string
	}{
		{
			name:   "success",
			result: ResultMessage{Subtype: ResultSubtypeSuccess, Result: "done"},
			wantOK: true,
		},
		{
			name:       "max turns",
			result:     ResultMessage{Subtype: ResultSubtypeErrorMaxTurns, IsError: true},
			wantReason: "agent stopped: max turns reached",
		},
		{
			name:        "error during execution carries the CLI text",
			result:      ResultMessage{Subtype: ResultSubtypeErrorDuringExecution, IsError: true, Result: "boom"},
			containsAll: []string{"error during execution", "boom"},
		},
		{
			name:       "is_error with success subtype still fails",
			result:     ResultMessage{Subtype: ResultSubtypeSuccess, IsError: true},
			wantReason: "agent reported an error",
		},
		{
			name:        "unknown subtype is reported verbatim",
			result:      ResultMessage{Subtype: "error_something_new", IsError: true},
			containsAll: []string{"error_something_new"},
		},
		{
			name: "api error status is surfaced",
			result: ResultMessage{
				Subtype:        ResultSubtypeErrorDuringExecution,
				IsError:        true,
				APIErrorStatus: json.RawMessage(`529`),
			},
			containsAll: []string{"api_error_status: 529"},
		},
		{
			name: "null api error status is omitted",
			result: ResultMessage{
				Subtype:        ResultSubtypeErrorMaxTurns,
				IsError:        true,
				APIErrorStatus: json.RawMessage(`null`),
			},
			wantReason: "agent stopped: max turns reached",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.wantOK, tt.result.Succeeded())

			reason := tt.result.FailureReason()
			if tt.wantOK {
				assert.Empty(t, reason)
				return
			}
			assert.NotEmpty(t, reason)
			if tt.wantReason != "" {
				assert.Equal(t, tt.wantReason, reason)
			}
			for _, want := range tt.containsAll {
				assert.Contains(t, reason, want)
			}
		})
	}
}

func TestInitMessage_FromGolden(t *testing.T) {
	var init InitMessage
	require.NoError(t, json.Unmarshal(goldenLineOfType(t, goldenBasic, MessageTypeSystem, SystemSubtypeInit), &init))

	assert.Equal(t, MessageTypeSystem, init.Type)
	assert.Equal(t, SystemSubtypeInit, init.Subtype)
	assert.Equal(t, goldenBasicSession, init.SessionID)
	assert.Equal(t, goldenCLIVersion, init.ClaudeCodeVersion)
	assert.Equal(t, goldenModel, init.Model)
	assert.Equal(t, goldenPermissionMode, init.PermissionMode)
	assert.NotEmpty(t, init.Tools)
	assert.NotEmpty(t, init.CWD)
}

// TestInitMessage_RecordingsAreConsistent keeps the fixture set honest: all
// three recordings must come from the same CLI version, model and permission
// mode. The CLI echoes back what it actually ran with, so a re-record that
// drifts on any of these shows up here instead of silently changing what the
// other tests are pinning.
func TestInitMessage_RecordingsAreConsistent(t *testing.T) {
	for _, name := range []string{goldenBasic, goldenToolUse, goldenResume} {
		t.Run(name, func(t *testing.T) {
			var init InitMessage
			require.NoError(t, json.Unmarshal(
				goldenLineOfType(t, name, MessageTypeSystem, SystemSubtypeInit), &init))

			assert.Equal(t, goldenCLIVersion, init.ClaudeCodeVersion)
			assert.Equal(t, goldenModel, init.Model)
			assert.Equal(t, goldenPermissionMode, init.PermissionMode)
		})
	}
}

// TestInitMessage_ResumeIsNotCwdBound documents a trap: resume.jsonl was
// recorded resuming basic.jsonl's session from a different directory, and it
// worked. No code may assume the resumed session shares a cwd.
func TestInitMessage_ResumeIsNotCwdBound(t *testing.T) {
	var origin, resumed InitMessage
	require.NoError(t, json.Unmarshal(goldenLineOfType(t, goldenBasic, MessageTypeSystem, SystemSubtypeInit), &origin))
	require.NoError(t, json.Unmarshal(goldenLineOfType(t, goldenResume, MessageTypeSystem, SystemSubtypeInit), &resumed))

	assert.Equal(t, origin.SessionID, resumed.SessionID, "resume reuses the session id")
	assert.NotEqual(t, origin.CWD, resumed.CWD, "resume happened from a different cwd")
}

func TestRateLimitMessage_FromGolden(t *testing.T) {
	var msg RateLimitMessage
	require.NoError(t, json.Unmarshal(goldenLineOfType(t, goldenBasic, MessageTypeRateLimitEvent), &msg))

	assert.Equal(t, MessageTypeRateLimitEvent, msg.Type)
	assert.Equal(t, "allowed", msg.RateLimit.Status)
	assert.Equal(t, "five_hour", msg.RateLimit.RateLimitType)
	assert.Positive(t, msg.RateLimit.ResetsAt)
}

func TestAssistantMessage_FromGolden(t *testing.T) {
	var envelope StreamMessage
	require.NoError(t, json.Unmarshal(goldenLineOfType(t, goldenToolUse, MessageTypeAssistant), &envelope))
	require.NotEmpty(t, envelope.Message)

	var assistant AssistantMessage
	require.NoError(t, json.Unmarshal(envelope.Message, &assistant))

	assert.Equal(t, "assistant", assistant.Role)
	assert.NotEmpty(t, assistant.Model)
	require.NotEmpty(t, assistant.Content)
	require.NotNil(t, assistant.Usage)

	// stop_reason is JSON null mid-turn; decoding null into a string is a no-op.
	assert.Empty(t, assistant.StopReason)
}

// TestAssistantMessage_ToolUseInputIsObject pins that tool_use.input is a JSON
// object on the wire, not a JSON-encoded string.
func TestAssistantMessage_ToolUseInputIsObject(t *testing.T) {
	var toolUse *ContentBlock

	for _, line := range goldenLines(t, goldenToolUse) {
		var envelope StreamMessage
		if err := json.Unmarshal(line, &envelope); err != nil || envelope.Type != MessageTypeAssistant {
			continue
		}
		var assistant AssistantMessage
		require.NoError(t, json.Unmarshal(envelope.Message, &assistant))
		for i := range assistant.Content {
			if assistant.Content[i].Type == ContentTypeToolUse {
				toolUse = &assistant.Content[i]
			}
		}
	}

	require.NotNil(t, toolUse, "tooluse golden must contain a tool_use block")
	assert.Equal(t, "Bash", toolUse.Name)
	assert.NotEmpty(t, toolUse.ID)
	require.NotEmpty(t, toolUse.Input)
	assert.Equal(t, byte('{'), toolUse.Input[0], "tool input is an object")

	var input struct {
		Command string `json:"command"`
	}
	require.NoError(t, json.Unmarshal(toolUse.Input, &input))
	assert.Equal(t, "echo marionette-golden", input.Command)
}

func TestUserMessage_ToolResultFromGolden(t *testing.T) {
	var envelope StreamMessage
	require.NoError(t, json.Unmarshal(goldenLineOfType(t, goldenToolUse, MessageTypeUser), &envelope))

	var user UserMessage
	require.NoError(t, json.Unmarshal(envelope.Message, &user))

	require.Len(t, user.Content, 1)
	block := user.Content[0]
	assert.Equal(t, ContentTypeToolResult, block.Type)
	assert.NotEmpty(t, block.ToolUseID)
	assert.False(t, block.IsError)
	assert.Equal(t, "marionette-golden", block.Content.String())
}

// TestFlexibleText_Unmarshal covers the shapes the Anthropic wire format allows
// for a tool_result's content. A model that only accepts a string would fail
// the whole message when a tool returns blocks.
func TestFlexibleText_Unmarshal(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "string", in: `"plain"`, want: "plain"},
		{name: "null", in: `null`, want: ""},
		{name: "block array", in: `[{"type":"text","text":"a"},{"type":"text","text":"b"}]`, want: "a\nb"},
		{name: "empty array", in: `[]`, want: ""},
		{name: "array of unknown shape stays raw", in: `[1,2]`, want: "[1,2]"},
		{name: "object stays raw", in: `{"k":1}`, want: `{"k":1}`},
		{name: "number stays raw", in: `42`, want: "42"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got FlexibleText
			require.NoError(t, json.Unmarshal([]byte(tt.in), &got))
			assert.Equal(t, tt.want, got.String())
		})
	}
}

// TestMessages_TolerateUnknownFields is the contract that keeps a CLI upgrade
// from breaking a running task.
func TestMessages_TolerateUnknownFields(t *testing.T) {
	var result ResultMessage
	require.NoError(t, json.Unmarshal([]byte(
		`{"type":"result","subtype":"success","result":"ok","brand_new_field":{"nested":true}}`), &result))
	assert.True(t, result.Succeeded())
	assert.Equal(t, "ok", result.Result)

	var envelope StreamMessage
	require.NoError(t, json.Unmarshal([]byte(
		`{"type":"some_future_type","session_id":"s1","future":[1,2,3]}`), &envelope))
	assert.Equal(t, "some_future_type", envelope.Type)
	assert.Equal(t, "s1", envelope.SessionID)
}
