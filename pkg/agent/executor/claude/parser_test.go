package claude

import (
	"strings"
	"testing"

	"github.com/chunlea/marionette/pkg/agent/executor"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// parseGolden feeds an entire golden recording through a fresh parser and
// returns every event it produced. Parsing must never return an error.
func parseGolden(t *testing.T, name string) (*Parser, []*executor.AgentEvent) {
	t.Helper()

	p := NewParser().(*Parser)
	var events []*executor.AgentEvent
	for i, line := range goldenLines(t, name) {
		got, err := p.ParseLine(line)
		require.NoErrorf(t, err, "%s line %d must parse without error", name, i+1)
		events = append(events, got...)
	}
	return p, events
}

// countByType tallies events by type.
func countByType(events []*executor.AgentEvent) map[executor.EventType]int {
	counts := make(map[executor.EventType]int)
	for _, e := range events {
		counts[e.Type]++
	}
	return counts
}

// TestParser_Goldens walks every recording end to end. This is the test the
// old suite could not have passed: the production parse path now reaches the
// result branch instead of falling through to raw text.
func TestParser_Goldens(t *testing.T) {
	tests := []struct {
		golden      string
		wantSession string
		wantResult  string
		wantInput   int64
		wantOutput  int64
		wantToolUse int
		wantToolRes int
	}{
		{
			golden:      goldenBasic,
			wantSession: goldenBasicSession,
			wantResult:  "hi",
			wantInput:   2,
			wantOutput:  4,
		},
		{
			golden:      goldenToolUse,
			wantSession: goldenToolUseSession,
			wantResult:  "marionette-golden",
			wantInput:   4,
			wantOutput:  123,
			wantToolUse: 1,
			wantToolRes: 1,
		},
		{
			golden:      goldenResume,
			wantSession: goldenBasicSession,
			wantResult:  "hi again",
			wantInput:   2,
			wantOutput:  5,
		},
	}

	for _, tt := range tests {
		t.Run(tt.golden, func(t *testing.T) {
			p, events := parseGolden(t, tt.golden)

			assert.Equal(t, tt.wantSession, p.SessionID())

			result := p.Result()
			require.NotNil(t, result, "the result line must reach parseResultMessage")
			assert.True(t, result.Succeeded())
			assert.Equal(t, tt.wantResult, result.Result)
			assert.Equal(t, tt.wantInput, result.Usage.InputTokens)
			assert.Equal(t, tt.wantOutput, result.Usage.OutputTokens)

			counts := countByType(events)
			assert.Equal(t, tt.wantToolUse, counts[executor.EventToolUse])
			assert.Equal(t, tt.wantToolRes, counts[executor.EventToolResult])
			assert.Zero(t, counts[executor.EventError], "a successful run emits no error events")
			assert.Positive(t, counts[executor.EventUsage])
			assert.Positive(t, counts[executor.EventSystem])

			// Every event keeps the line it came from for storage/debugging.
			for _, e := range events {
				assert.NotEmpty(t, e.Raw, "event %s must carry its raw line", e.Type)
			}
		})
	}
}

// TestParser_GoldenBasicIsNotRawText is the direct regression guard: with the
// old model the result line failed to unmarshal and was emitted as raw text.
func TestParser_GoldenBasicIsNotRawText(t *testing.T) {
	_, events := parseGolden(t, goldenBasic)

	for _, e := range events {
		if e.Type != executor.EventText {
			continue
		}
		assert.NotContains(t, e.Text, `"total_cost_usd"`,
			"a result line leaked through as raw text: the result branch is unreachable")
	}
}

func TestParser_GoldenInitEvent(t *testing.T) {
	_, events := parseGolden(t, goldenBasic)

	var init string
	for _, e := range events {
		if e.Type == executor.EventSystem && strings.HasPrefix(e.Text, "init:") {
			init = e.Text
		}
	}

	require.NotEmpty(t, init, "the system/init line must produce an init event")
	assert.Contains(t, init, "session="+goldenBasicSession)
	assert.Contains(t, init, "version="+goldenCLIVersion)
	assert.Contains(t, init, "model="+goldenModel)
}

// TestParser_GoldenPassesThroughMidStreamNoise pins the trap from the brief:
// rate_limit_event and the extra system subtypes arrive mid-stream and must be
// passed through without ending the turn.
func TestParser_GoldenPassesThroughMidStreamNoise(t *testing.T) {
	p, events := parseGolden(t, goldenToolUse)

	var texts []string
	for _, e := range events {
		if e.Type == executor.EventSystem {
			texts = append(texts, e.Text)
		}
	}
	joined := strings.Join(texts, "\n")

	assert.Contains(t, joined, "rate_limit:")
	assert.Contains(t, joined, "hook=")
	assert.Contains(t, joined, "thinking_tokens")
	assert.NotContains(t, joined, "unknown message type")

	// The turn still completed despite the mid-stream noise.
	require.NotNil(t, p.Result())
	assert.True(t, p.Result().Succeeded())
}

func TestParser_GoldenToolUseAndResult(t *testing.T) {
	_, events := parseGolden(t, goldenToolUse)

	var toolUse *executor.ToolUseEvent
	var toolResult *executor.ToolResultEvent
	for _, e := range events {
		switch e.Type {
		case executor.EventToolUse:
			toolUse = e.ToolUse
		case executor.EventToolResult:
			toolResult = e.ToolResult
		}
	}

	require.NotNil(t, toolUse)
	assert.Equal(t, "Bash", toolUse.Name)
	assert.True(t, strings.HasPrefix(toolUse.ID, "toolu_"))
	assert.Contains(t, toolUse.Input, "echo marionette-golden")

	// Tool results arrive as `user` messages. The old parser dropped them.
	require.NotNil(t, toolResult)
	assert.Equal(t, toolUse.ID, toolResult.ToolUseID)
	assert.Equal(t, "marionette-golden", toolResult.Output)
	assert.False(t, toolResult.IsError)
}

func TestParser_GoldenThinkingEvents(t *testing.T) {
	_, events := parseGolden(t, goldenToolUse)

	counts := countByType(events)
	assert.Positive(t, counts[executor.EventThinking], "thinking blocks must surface as thinking events")
}

// TestParser_ResultFailure covers the outcomes the goldens cannot contain:
// a run that the CLI itself reports as failed.
func TestParser_ResultFailure(t *testing.T) {
	tests := []struct {
		name       string
		line       string
		wantReason string
	}{
		{
			name:       "max turns",
			line:       `{"type":"result","subtype":"error_max_turns","is_error":true,"result":"","session_id":"s1"}`,
			wantReason: "max turns reached",
		},
		{
			name:       "error during execution",
			line:       `{"type":"result","subtype":"error_during_execution","is_error":true,"result":"tool crashed","session_id":"s1"}`,
			wantReason: "tool crashed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := NewParser().(*Parser)
			events, err := p.ParseLine([]byte(tt.line))
			require.NoError(t, err)

			require.NotNil(t, p.Result())
			assert.False(t, p.Result().Succeeded())

			counts := countByType(events)
			require.Equal(t, 1, counts[executor.EventError])

			var errText string
			for _, e := range events {
				if e.Type == executor.EventError {
					errText = e.Text
				}
			}
			assert.Contains(t, errText, tt.wantReason)
		})
	}
}

// TestParser_Tolerance is the contract: nothing the CLI can emit turns into a
// parse error, because an error would abort a running task.
func TestParser_Tolerance(t *testing.T) {
	tests := []struct {
		name      string
		line      string
		wantCount int
		wantType  executor.EventType
		wantText  string
	}{
		{name: "empty line", line: "", wantCount: 0},
		{name: "whitespace only", line: "   ", wantCount: 0},
		{
			name: "non-JSON debug output", line: "Ignoring 30 permissions.allow entries",
			wantCount: 1, wantType: executor.EventText, wantText: "Ignoring 30 permissions.allow entries",
		},
		{
			name: "truncated JSON", line: `{"type":"result","subtype":`,
			wantCount: 1, wantType: executor.EventText,
		},
		{
			name: "unknown message type", line: `{"type":"holodeck_event","session_id":"s1"}`,
			wantCount: 1, wantType: executor.EventSystem, wantText: "unknown message type: holodeck_event",
		},
		{
			name: "system message with unknown subtype", line: `{"type":"system","subtype":"quantum_flux"}`,
			wantCount: 1, wantType: executor.EventSystem, wantText: "system: quantum_flux",
		},
		{
			name: "assistant with no message", line: `{"type":"assistant"}`,
			wantCount: 0,
		},
		{
			name: "assistant with malformed message", line: `{"type":"assistant","message":"not-an-object"}`,
			wantCount: 1, wantType: executor.EventSystem,
		},
		{
			name: "user with no message", line: `{"type":"user"}`,
			wantCount: 0,
		},
		{
			name: "unknown content block type is skipped", line: `{"type":"assistant","message":{"content":[{"type":"holo_block"}]}}`,
			wantCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := NewParser().(*Parser)
			events, err := p.ParseLine([]byte(tt.line))

			require.NoError(t, err, "ParseLine must never return an error")
			require.Len(t, events, tt.wantCount)

			if tt.wantCount == 0 {
				return
			}
			assert.Equal(t, tt.wantType, events[0].Type)
			if tt.wantText != "" {
				assert.Equal(t, tt.wantText, events[0].Text)
			}
		})
	}
}

// TestParser_UnknownTypeDoesNotEndTurn makes sure a future message type
// injected mid-stream leaves the run intact.
func TestParser_UnknownTypeDoesNotEndTurn(t *testing.T) {
	p := NewParser().(*Parser)

	lines := goldenLines(t, goldenBasic)
	injected := make([][]byte, 0, len(lines)+1)
	for i, line := range lines {
		if i == len(lines)-1 {
			injected = append(injected, []byte(`{"type":"future_type_2027","session_id":"s1","payload":{"a":1}}`))
		}
		injected = append(injected, line)
	}

	for _, line := range injected {
		_, err := p.ParseLine(line)
		require.NoError(t, err)
	}

	require.NotNil(t, p.Result())
	assert.True(t, p.Result().Succeeded())
	assert.Equal(t, goldenBasicSession, p.SessionID())
}

func TestParser_SessionIDTracking(t *testing.T) {
	p := NewParser().(*Parser)
	assert.Empty(t, p.SessionID())

	_, err := p.ParseLine([]byte(`{"type":"system","subtype":"init","session_id":"sess-1"}`))
	require.NoError(t, err)
	assert.Equal(t, "sess-1", p.SessionID())

	// A line without a session id must not clear the tracked one.
	_, err = p.ParseLine([]byte(`{"type":"assistant","message":{"content":[{"type":"text","text":"x"}]}}`))
	require.NoError(t, err)
	assert.Equal(t, "sess-1", p.SessionID())
}

func TestParser_Reset(t *testing.T) {
	p, _ := parseGolden(t, goldenBasic)
	require.NotEmpty(t, p.SessionID())
	require.NotNil(t, p.Result())

	p.Reset()

	assert.Empty(t, p.SessionID())
	assert.Nil(t, p.Result())
}

func TestParser_Flush(t *testing.T) {
	p := NewParser().(*Parser)
	events, err := p.Flush()
	require.NoError(t, err)
	assert.Empty(t, events)
}

func TestParser_Registration(t *testing.T) {
	p, err := executor.GetParser("claude")
	require.NoError(t, err)
	assert.IsType(t, &Parser{}, p)
}
