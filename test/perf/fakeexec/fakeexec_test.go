package fakeexec_test

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/chunlea/marionette/pkg/agent/executor"
	"github.com/chunlea/marionette/test/perf/fakeexec"
)

// captureHandler records everything the executor sends it.
type captureHandler struct {
	mu           sync.Mutex
	output       []string
	contexts     []string
	permissions  []*executor.PermissionRequest
	approve      bool
	permissionCh chan struct{}
}

func (h *captureHandler) HandleOutput(_ string, data []byte) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.output = append(h.output, string(data))
}

func (h *captureHandler) HandlePermissionRequest(_ context.Context, req *executor.PermissionRequest) (bool, error) {
	h.mu.Lock()
	h.permissions = append(h.permissions, req)
	approve := h.approve
	h.mu.Unlock()
	if h.permissionCh != nil {
		close(h.permissionCh)
	}
	return approve, nil
}

func (h *captureHandler) HandleContextUpdate(_ context.Context, sessionID, _ string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.contexts = append(h.contexts, sessionID)
}

func (h *captureHandler) lines() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]string(nil), h.output...)
}

func task() *executor.Task {
	return &executor.Task{
		ID: "task_1", RunID: "trun_1", SessionID: "sess_1", Attempt: 1,
		Prompt: "do a thing", Timeout: time.Minute,
	}
}

// TestExecute_WalksTheWholeHandlerContract. The point of the fake is that the
// pipeline downstream of it cannot tell the difference, so every callback a
// real executor makes has to be made here too.
func TestExecute_WalksTheWholeHandlerContract(t *testing.T) {
	exec := fakeexec.New(fakeexec.Config{
		LogLines: 5, LineBytes: 10, Duration: 5 * time.Millisecond,
		TokensInput: 100, TokensOutput: 20,
	})
	handler := &captureHandler{}

	result, err := exec.Execute(context.Background(), task(), nil, handler)
	require.NoError(t, err)
	require.NotNil(t, result)

	assert.True(t, result.Success)
	assert.Zero(t, result.ExitCode)
	assert.Equal(t, int64(100), result.TokensInput)
	assert.Equal(t, int64(20), result.TokensOutput)
	assert.NotEmpty(t, result.AgentSession, "the resume path needs an agent session id")
	assert.Contains(t, string(result.ContextSnapshot), result.AgentSession)

	assert.Len(t, handler.lines(), 5, "the line count is a bound, not a suggestion")
	assert.Len(t, handler.contexts, 1, "a real agent reports its session id early")
	assert.Equal(t, 1, exec.Executed())

	for _, line := range handler.lines() {
		assert.True(t, strings.HasSuffix(line, "\n"), "log lines are newline-terminated")
	}
}

// TestExecute_BoundsItsOwnOutput: 50 sessions x 200 tasks x unbounded output is
// how a load test fills a disk.
func TestExecute_BoundsItsOwnOutput(t *testing.T) {
	exec := fakeexec.New(fakeexec.Config{
		LogLines: 3, LineBytes: 8, Duration: time.Millisecond,
	})
	handler := &captureHandler{}

	for i := 0; i < 4; i++ {
		_, err := exec.Execute(context.Background(), task(), nil, handler)
		require.NoError(t, err)
	}

	assert.Len(t, handler.lines(), 12)
	total := 0
	for _, line := range handler.lines() {
		total += len(line)
	}
	assert.Less(t, total, 4*3*100, "line length stays near what was asked for")
}

func TestExecute_AsksForPermissionOnSchedule(t *testing.T) {
	exec := fakeexec.New(fakeexec.Config{
		LogLines: 1, LineBytes: 4, Duration: time.Millisecond, PermissionEvery: 2,
	})
	handler := &captureHandler{approve: true}

	_, err := exec.Execute(context.Background(), task(), nil, handler)
	require.NoError(t, err)
	assert.Empty(t, handler.permissions, "the first task does not ask")

	_, err = exec.Execute(context.Background(), task(), nil, handler)
	require.NoError(t, err)
	require.Len(t, handler.permissions, 1, "every second task asks")
	assert.Equal(t, "bash", handler.permissions[0].Tool)
}

func TestExecute_DeniedPermissionFailsTheTask(t *testing.T) {
	exec := fakeexec.New(fakeexec.Config{
		LogLines: 1, LineBytes: 4, Duration: time.Millisecond, PermissionEvery: 1,
	})
	handler := &captureHandler{approve: false}

	result, err := exec.Execute(context.Background(), task(), nil, handler)
	require.NoError(t, err)
	assert.False(t, result.Success)
	assert.Equal(t, "permission denied", result.Error)
}

// TestKill_EndsTheRun. Kill on a real executor terminates a subprocess; the
// fake has to end its run the same way or a cancelled task under load would
// hang the harness instead of the server.
func TestKill_EndsTheRun(t *testing.T) {
	exec := fakeexec.New(fakeexec.Config{
		LogLines: 100, LineBytes: 8, Duration: 5 * time.Second,
	})
	handler := &captureHandler{}

	done := make(chan *executor.Result, 1)
	go func() {
		result, err := exec.Execute(context.Background(), task(), nil, handler)
		assert.NoError(t, err)
		done <- result
	}()

	// Wait until it is actually running before killing it.
	require.Eventually(t, func() bool { return exec.Executed() == 1 },
		2*time.Second, 5*time.Millisecond)
	require.NoError(t, exec.Kill())

	select {
	case result := <-done:
		require.NotNil(t, result)
		assert.False(t, result.Success)
		assert.Equal(t, "killed", result.Error)
	case <-time.After(2 * time.Second):
		t.Fatal("Kill did not end the run")
	}
}

// TestExecute_CancelledContextIsATransportFailure, not a completed run: the
// caller's context ending means the task never finished, and reporting success
// or a clean failure there would tell the server something false.
func TestExecute_CancelledContextIsATransportFailure(t *testing.T) {
	exec := fakeexec.New(fakeexec.Config{
		LogLines: 100, LineBytes: 8, Duration: 5 * time.Second,
	})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := exec.Execute(ctx, task(), nil, &captureHandler{})
	require.ErrorIs(t, err, context.Canceled)
}

func TestNew_FillsInDefaults(t *testing.T) {
	exec := fakeexec.New(fakeexec.Config{})
	handler := &captureHandler{}

	_, err := exec.Execute(context.Background(), task(), nil, handler)
	require.NoError(t, err)
	assert.Len(t, handler.lines(), fakeexec.DefaultConfig().LogLines)
}
