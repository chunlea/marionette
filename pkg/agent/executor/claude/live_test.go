package claude

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/chunlea/marionette/pkg/agent/executor"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Live tests run the real Claude Code binary. They are the only tests that can
// tell us the wire contracts in messages.go and executor.go are actually
// right; everything else only proves we are self-consistent.
//
// They cost tokens and need network, so they are opt-in:
//
//	MARIONETTE_LIVE_CLAUDE=1 go test ./pkg/agent/executor/claude/ -run RealCLI

// requireLiveCLI skips unless live testing is enabled, and returns the binary.
func requireLiveCLI(t *testing.T) string {
	t.Helper()

	if os.Getenv("MARIONETTE_LIVE_CLAUDE") != "1" {
		t.Skip("set MARIONETTE_LIVE_CLAUDE=1 to run against the real Claude CLI")
	}

	binary, err := exec.LookPath("claude")
	require.NoError(t, err, "claude must be on PATH")
	return binary
}

// TestExecutor_Execute_RealCLIHappyPath is the main execution path against the
// real binary: prompt in, parsed result out, with tokens and a resume handle.
func TestExecutor_Execute_RealCLIHappyPath(t *testing.T) {
	binary := requireLiveCLI(t)

	handler := &gateHandler{approve: true}
	e := New(
		WithBinaryPath(binary),
		// Gating is exercised by TestExecutor_Execute_RealCLIGate; this run is
		// about the parse and result path.
		WithoutPermissionGating(),
	)

	result, err := e.Execute(context.Background(), &executor.Task{
		Prompt:     "Reply with exactly: MARIONETTE-LIVE-OK",
		WorkingDir: t.TempDir(),
		Timeout:    3 * time.Minute,
	}, nil, handler)

	require.NoError(t, err)
	require.NotNil(t, result)
	require.True(t, result.Success, "run failed: %s", result.Error)
	assert.Empty(t, result.Error)
	assert.Equal(t, 0, result.ExitCode)

	// The result line must have been parsed, not swallowed as raw text.
	agentResult := e.parser.Result()
	require.NotNil(t, agentResult, "the result line must reach parseResultMessage")
	assert.Equal(t, ResultSubtypeSuccess, agentResult.Subtype)
	assert.False(t, agentResult.IsError)
	assert.Contains(t, agentResult.Result, "MARIONETTE-LIVE-OK")
	assert.Positive(t, agentResult.TotalCostUSD)

	// Tokens and the resume handle must reach the server-facing Result.
	assert.Positive(t, result.TokensInput, "token counts must come off the result line")
	assert.Positive(t, result.TokensOutput)
	assert.NotEmpty(t, result.AgentSession)

	var snapshot contextSnapshot
	require.NoError(t, json.Unmarshal(result.ContextSnapshot, &snapshot))
	assert.Equal(t, result.AgentSession, snapshot.ConversationID)
}

// TestExecutor_Execute_RealCLIResume proves the resume path works end to end:
// the stream-json stdin contract, --resume, and the turn ending on its own
// instead of hanging until the timeout.
//
// It deliberately resumes from a DIFFERENT working directory than the session
// was created in: on 2.1.241 resume is keyed on session id alone, and nothing
// here may assume otherwise.
func TestExecutor_Execute_RealCLIResume(t *testing.T) {
	binary := requireLiveCLI(t)

	handler := &gateHandler{approve: true}

	// First turn: establish a session with a fact to recall.
	first := New(WithBinaryPath(binary), WithoutPermissionGating())
	firstResult, err := first.Execute(context.Background(), &executor.Task{
		Prompt:     "Remember the token MARIONETTE-RESUME-7391. Reply with exactly: STORED",
		WorkingDir: t.TempDir(),
		Timeout:    3 * time.Minute,
	}, nil, handler)

	require.NoError(t, err)
	require.True(t, firstResult.Success, "first turn failed: %s", firstResult.Error)
	require.NotEmpty(t, firstResult.ContextSnapshot)

	// Second turn: resume that session from a different directory.
	second := New(WithBinaryPath(binary), WithoutPermissionGating())

	started := time.Now()
	secondResult, err := second.Execute(context.Background(), &executor.Task{
		Prompt:          "What token did I ask you to remember? Reply with just the token.",
		WorkingDir:      t.TempDir(),
		ContextSnapshot: firstResult.ContextSnapshot,
		Timeout:         3 * time.Minute,
	}, nil, handler)
	elapsed := time.Since(started)

	require.NoError(t, err)
	require.NotNil(t, secondResult)
	require.True(t, secondResult.Success, "resume failed: %s", secondResult.Error)

	// Stream mode must have been used, and the turn must have ended by itself.
	// Before stdin was closed on the result line, a resume run could only end
	// by timing out.
	assert.Less(t, elapsed, 3*time.Minute, "resume must not hang until its timeout")

	agentResult := second.parser.Result()
	require.NotNil(t, agentResult)
	assert.Contains(t, agentResult.Result, "MARIONETTE-RESUME-7391",
		"the resumed session must still have the conversation")

	// Resume keeps the same CLI session id, so the snapshot round-trips.
	assert.Equal(t, firstResult.AgentSession, secondResult.AgentSession)
}
