package claude

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chunlea/marionette/pkg/agent/executor"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRequiresPermission covers the policy inversion: the old five-entry
// allow-list let everything else through ungated, including every mcp__* tool
// and every built-in added since it was written.
func TestRequiresPermission(t *testing.T) {
	tests := []struct {
		tool  string
		gated bool
	}{
		// Read-only built-ins are not worth interrupting a human for.
		{"Read", false},
		{"Glob", false},
		{"Grep", false},
		{"LSP", false},
		{"ToolSearch", false},
		{"CronList", false},

		// Mutating or side-effecting built-ins.
		{"Bash", true},
		{"Write", true},
		{"Edit", true},
		{"NotebookEdit", true},

		// Built-ins that exist on 2.1.241 and were invisible to the old map.
		{"Task", true},
		{"Workflow", true},
		{"CronCreate", true},
		{"RemoteTrigger", true},
		{"SendMessage", true},
		{"PushNotification", true},
		{"WebFetch", true},
		{"EnterWorktree", true},

		// Third-party MCP tools: unknowable from here, so always gated.
		{"mcp__github__create_pull_request", true},
		{"mcp__anything__at_all", true},

		// Unknown names are gated so a CLI upgrade cannot open a hole.
		{"SomeToolFromTheFuture", true},
		{"", true},
	}

	for _, tt := range tests {
		name := tt.tool
		if name == "" {
			name = "empty"
		}
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, tt.gated, RequiresPermission(tt.tool))
		})
	}
}

func TestRiskLevelFor(t *testing.T) {
	assert.Equal(t, executor.RiskCritical, RiskLevelFor("Bash"))
	assert.Equal(t, executor.RiskHigh, RiskLevelFor("Write"))
	assert.Equal(t, executor.RiskMedium, RiskLevelFor("WebFetch"))
	assert.Equal(t, executor.RiskLow, RiskLevelFor("Read"))

	// Unknown and MCP tools get the default, and are still gated.
	assert.Equal(t, defaultToolRisk, RiskLevelFor("mcp__x__y"))
	assert.Equal(t, defaultToolRisk, RiskLevelFor("SomeToolFromTheFuture"))
}

// gateHandler is an OutputHandler that answers permission requests on demand.
type gateHandler struct {
	mu       sync.Mutex
	requests []*executor.PermissionRequest

	approve bool
	err     error
	delay   time.Duration
}

func (h *gateHandler) HandleOutput(string, []byte) {}

func (h *gateHandler) HandleContextUpdate(context.Context, string, string) {}

func (h *gateHandler) HandlePermissionRequest(ctx context.Context, req *executor.PermissionRequest) (bool, error) {
	h.mu.Lock()
	h.requests = append(h.requests, req)
	approve, err, delay := h.approve, h.err, h.delay
	h.mu.Unlock()

	if delay > 0 {
		select {
		case <-time.After(delay):
		case <-ctx.Done():
			return false, ctx.Err()
		}
	}
	return approve, err
}

func (h *gateHandler) seen() []*executor.PermissionRequest {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]*executor.PermissionRequest(nil), h.requests...)
}

// preToolUsePayload is the shape the CLI writes to the hook's stdin, as
// captured from a real 2.1.241 run.
func preToolUsePayload(tool string) []byte {
	return []byte(`{"session_id":"88ad66f2","transcript_path":"/tmp/t.jsonl","cwd":"/workspace",` +
		`"prompt_id":"e6f680d9","permission_mode":"acceptEdits","hook_event_name":"PreToolUse",` +
		`"tool_name":"` + tool + `","tool_input":{"command":"echo hi","description":"Echo hi"},` +
		`"tool_use_id":"toolu_014cBFj"}`)
}

// decodeHookOutput parses what the hook writes to the CLI's stdin.
func decodeHookOutput(t *testing.T, raw []byte) hookSpecificOutput {
	t.Helper()

	var out hookOutput
	require.NoError(t, json.Unmarshal(raw, &out))
	assert.Equal(t, hookEventPreToolUse, out.HookSpecificOutput.HookEventName)
	return out.HookSpecificOutput
}

// runHook drives one hook process in-process against a live broker.
func runHook(t *testing.T, socketPath string, payload []byte) hookSpecificOutput {
	t.Helper()

	var out bytes.Buffer
	require.NoError(t, RunPermissionHook(context.Background(), socketPath, bytes.NewReader(payload), &out))
	return decodeHookOutput(t, out.Bytes())
}

// TestPermissionBroker_RoundTrip walks the whole gate: hook payload in,
// operator decision out, in the exact wire shapes both ends require.
func TestPermissionBroker_RoundTrip(t *testing.T) {
	tests := []struct {
		name         string
		tool         string
		newHandler   func() *gateHandler
		wantDecision string
		wantAsked    bool
		wantReason   string
	}{
		{
			name:         "approved",
			tool:         "Bash",
			newHandler:   func() *gateHandler { return &gateHandler{approve: true} },
			wantDecision: decisionAllow,
			wantAsked:    true,
			wantReason:   "approved by operator",
		},
		{
			name:         "denied",
			tool:         "Bash",
			newHandler:   func() *gateHandler { return &gateHandler{approve: false} },
			wantDecision: decisionDeny,
			wantAsked:    true,
			wantReason:   "denied by operator",
		},
		{
			name:         "read-only tool is allowed without asking",
			tool:         "Read",
			newHandler:   func() *gateHandler { return &gateHandler{approve: false} },
			wantDecision: decisionAllow,
			wantAsked:    false,
		},
		{
			name:         "mcp tool is asked about",
			tool:         "mcp__github__merge_pr",
			newHandler:   func() *gateHandler { return &gateHandler{approve: true} },
			wantDecision: decisionAllow,
			wantAsked:    true,
		},
		{
			name:         "handler error denies",
			tool:         "Bash",
			newHandler:   func() *gateHandler { return &gateHandler{err: errors.New("session suspended")} },
			wantDecision: decisionDeny,
			wantAsked:    true,
			wantReason:   "session suspended",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := tt.newHandler()
			broker, err := NewPermissionBroker(handler, time.Second)
			require.NoError(t, err)
			defer func() { _ = broker.Close() }()
			broker.Serve(context.Background())

			got := runHook(t, broker.SocketPath(), preToolUsePayload(tt.tool))

			assert.Equal(t, tt.wantDecision, got.PermissionDecision)
			if tt.wantReason != "" {
				assert.Contains(t, got.PermissionDecisionReason, tt.wantReason)
			}

			asked := handler.seen()
			if !tt.wantAsked {
				assert.Empty(t, asked)
				return
			}

			require.Len(t, asked, 1)
			assert.Equal(t, tt.tool, asked[0].Tool)
			assert.Equal(t, "toolu_014cBFj", asked[0].ID)
			assert.Equal(t, RiskLevelFor(tt.tool), asked[0].RiskLevel)
			assert.Contains(t, asked[0].Action, "echo hi")
		})
	}
}

// TestPermissionBroker_FailsClosed is the security property: the CLI runs a
// tool anyway when its hook exceeds the timeout, so every failure inside the
// gate must produce an explicit deny before that happens.
func TestPermissionBroker_FailsClosed(t *testing.T) {
	t.Run("no decision within the wait", func(t *testing.T) {
		handler := &gateHandler{approve: true, delay: time.Hour}
		broker, err := NewPermissionBroker(handler, 100*time.Millisecond)
		require.NoError(t, err)
		defer func() { _ = broker.Close() }()
		broker.Serve(context.Background())

		got := runHook(t, broker.SocketPath(), preToolUsePayload("Bash"))

		assert.Equal(t, decisionDeny, got.PermissionDecision)
		assert.Contains(t, got.PermissionDecisionReason, "no approval received")
	})

	t.Run("broker unreachable", func(t *testing.T) {
		got := runHook(t, filepath.Join(t.TempDir(), "nope.sock"), preToolUsePayload("Bash"))

		assert.Equal(t, decisionDeny, got.PermissionDecision)
		assert.Contains(t, got.PermissionDecisionReason, "unreachable")
	})

	t.Run("unreadable payload", func(t *testing.T) {
		handler := &gateHandler{approve: true}
		broker, err := NewPermissionBroker(handler, time.Second)
		require.NoError(t, err)
		defer func() { _ = broker.Close() }()
		broker.Serve(context.Background())

		got := runHook(t, broker.SocketPath(), []byte("this is not json"))

		assert.Equal(t, decisionDeny, got.PermissionDecision)
		assert.Empty(t, handler.seen())
	})

	t.Run("broker closed mid-flight", func(t *testing.T) {
		handler := &gateHandler{approve: true}
		broker, err := NewPermissionBroker(handler, time.Second)
		require.NoError(t, err)
		broker.Serve(context.Background())
		socketPath := broker.SocketPath()
		require.NoError(t, broker.Close())

		got := runHook(t, socketPath, preToolUsePayload("Bash"))

		assert.Equal(t, decisionDeny, got.PermissionDecision)
	})
}

func TestPermissionBroker_RequiresHandler(t *testing.T) {
	_, err := NewPermissionBroker(nil, time.Second)
	assert.Error(t, err)
}

// TestHookSettings pins the --settings payload against the shape the CLI
// accepts, and the rule that our own deadline must fire before the CLI's.
func TestHookSettings(t *testing.T) {
	raw, err := hookSettings([]string{"/opt/marionette agent", PermissionHookCommand, "/tmp/s"}, 2*time.Minute)
	require.NoError(t, err)

	var settings struct {
		Hooks map[string][]struct {
			Matcher string `json:"matcher"`
			Hooks   []struct {
				Type    string `json:"type"`
				Command string `json:"command"`
				Timeout int    `json:"timeout"`
			} `json:"hooks"`
		} `json:"hooks"`
	}
	require.NoError(t, json.Unmarshal([]byte(raw), &settings))

	matchers := settings.Hooks[hookEventPreToolUse]
	require.Len(t, matchers, 1)
	assert.Equal(t, "*", matchers[0].Matcher)
	require.Len(t, matchers[0].Hooks, 1)

	hook := matchers[0].Hooks[0]
	assert.Equal(t, "command", hook.Type)
	// A path with a space must survive the shell that runs the command.
	assert.Contains(t, hook.Command, `'/opt/marionette agent'`)
	assert.Contains(t, hook.Command, PermissionHookCommand)

	// The CLI fails open on hook timeout, so its budget must exceed ours.
	assert.Greater(t, time.Duration(hook.Timeout)*time.Second, 2*time.Minute)

	_, err = hookSettings(nil, time.Minute)
	assert.Error(t, err)
}

func TestShellQuoteArgv(t *testing.T) {
	assert.Equal(t, `'a' 'b c' 'it'\''s'`, shellQuoteArgv([]string{"a", "b c", "it's"}))
}

// TestExecutor_Execute_GateIsInstalled proves the executor actually hands the
// CLI a PreToolUse hook. A gate that is built but never passed on the command
// line is exactly the failure mode this lane exists to fix.
func TestExecutor_Execute_GateIsInstalled(t *testing.T) {
	scriptPath := "/tmp/claude/mock_claude_gate.sh"
	capturePath := scriptPath + ".args"
	_ = os.Remove(capturePath)

	scriptContent := `#!/bin/bash
printf '%s\n' "$@" > "$0.args"
echo '{"type":"result","subtype":"success","is_error":false,"result":"ok","session_id":"sess_gate","usage":{"input_tokens":1,"output_tokens":1}}'
`
	require.NoError(t, os.WriteFile(scriptPath, []byte(scriptContent), 0755))
	defer func() {
		_ = os.Remove(scriptPath)
		_ = os.Remove(capturePath)
	}()

	e := New(
		WithBinaryPath(scriptPath),
		WithPermissionHookCommand("/usr/bin/true"),
		WithPermissionWait(time.Minute),
	)

	result, err := e.Execute(context.Background(), &executor.Task{
		Prompt:  "test",
		Timeout: 10 * time.Second,
	}, nil, &gateHandler{approve: true})

	require.NoError(t, err)
	require.True(t, result.Success)

	args, err := os.ReadFile(capturePath)
	require.NoError(t, err)
	assert.Contains(t, string(args), "--settings")
	assert.Contains(t, string(args), hookEventPreToolUse)
	assert.Contains(t, string(args), "/usr/bin/true")
}

// TestExecutor_Execute_RealCLIGate runs the gate against the actual Claude
// Code binary. It is the only test that proves the hook contract is right; the
// rest only prove we are internally consistent.
//
// Skipped unless MARIONETTE_LIVE_CLAUDE=1, since it costs tokens.
func TestExecutor_Execute_RealCLIGate(t *testing.T) {
	binary := requireLiveCLI(t)
	hookBinary := buildHookHelper(t)

	handler := &gateHandler{approve: false}
	e := New(
		WithBinaryPath(binary),
		WithPermissionHookCommand(hookBinary, PermissionHookCommand),
		WithPermissionWait(time.Minute),
	)

	result, err := e.Execute(context.Background(), &executor.Task{
		Prompt:     "run the bash command: echo MARIONETTE-GATE-ESCAPED",
		WorkingDir: t.TempDir(),
		Timeout:    3 * time.Minute,
	}, nil, handler)

	require.NoError(t, err)
	require.NotNil(t, result)

	asked := handler.seen()
	require.NotEmpty(t, asked, "the CLI must have consulted the gate before running Bash")
	assert.Equal(t, "Bash", asked[0].Tool)

	// The denial must have actually stopped the tool. The CLI records blocked
	// calls under permission_denials; this also pins the element shape, which
	// the golden recordings could not (they only ever contain an empty list).
	agentResult := e.parser.Result()
	require.NotNil(t, agentResult)
	require.NotEmpty(t, agentResult.PermissionDenials,
		"a denied tool call must be recorded in permission_denials")

	var denial struct {
		ToolName  string          `json:"tool_name"`
		ToolUseID string          `json:"tool_use_id"`
		ToolInput json.RawMessage `json:"tool_input"`
	}
	require.NoError(t, json.Unmarshal(agentResult.PermissionDenials[0], &denial))
	assert.Equal(t, "Bash", denial.ToolName)
	assert.NotEmpty(t, denial.ToolUseID)
	assert.Contains(t, string(denial.ToolInput), "MARIONETTE-GATE-ESCAPED")

	// Note: the model's final text may quote the command it was blocked from
	// running, so the answer text is not evidence either way. The denial
	// record is.
}

// buildHookHelper compiles the agent binary so the real CLI has something to
// invoke as its hook.
func buildHookHelper(t *testing.T) string {
	t.Helper()

	out := filepath.Join(t.TempDir(), "marionette-agent")
	cmd := exec.Command("go", "build", "-o", out, "github.com/chunlea/marionette/cmd/agent")
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("failed to build agent binary: %v\n%s", err, output)
	}
	return out
}

// TestRunPermissionHook_AlwaysWritesADecision guards the invariant that keeps
// the CLI's fail-open timeout from ever being reached.
func TestRunPermissionHook_AlwaysWritesADecision(t *testing.T) {
	var out bytes.Buffer
	err := RunPermissionHook(context.Background(), "/nonexistent/socket",
		strings.NewReader(""), &out)

	require.NoError(t, err)
	got := decodeHookOutput(t, out.Bytes())
	assert.Equal(t, decisionDeny, got.PermissionDecision)
}
