package agent

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"

	pb "github.com/chunlea/marionette/gen/proto/v1"
	"github.com/chunlea/marionette/pkg/agent/executor"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
)

// ledgerHarness is a TaskRunner with one attached session and an executor the
// test drives, which is what the redelivery cases need: the first run has to
// still be inside the executor when the duplicate arrives.
type ledgerHarness struct {
	runner *TaskRunner
	sender *mockMessageSender

	// calls counts how many times the executor was entered - the number this
	// whole ledger exists to hold at one per run id.
	calls atomic.Int32

	// entered is signalled once per executor entry.
	entered chan struct{}

	// release blocks the executor until the test closes or feeds it.
	release chan struct{}
}

func newLedgerHarness(t *testing.T) *ledgerHarness {
	t.Helper()

	logger := zaptest.NewLogger(t)
	h := &ledgerHarness{
		sender:  &mockMessageSender{},
		entered: make(chan struct{}, 8),
		release: make(chan struct{}),
	}

	exec := &mockExecutor{
		executeFunc: func(ctx context.Context, _ *executor.Task, _ *executor.AgentConfig, _ executor.OutputHandler) (*executor.Result, error) {
			h.calls.Add(1)
			h.entered <- struct{}{}
			select {
			case <-h.release:
			case <-ctx.Done():
				return nil, ctx.Err()
			}
			return &executor.Result{Success: true, ExitCode: 0}, nil
		},
	}

	wsMgr := NewWorkspaceManager(t.TempDir(), logger)
	cmdHandler := NewDefaultCommandHandler(wsMgr, logger)
	h.runner = NewTaskRunner(h.sender, exec, wsMgr, cmdHandler, &mockStatusSetter{}, nil, logger)

	_, err := cmdHandler.HandleAttachSession(context.Background(), &pb.AttachSession{
		SessionId:     "sess_ledger",
		WorkspacePath: "ws_ledger",
	})
	require.NoError(t, err)

	return h
}

func ledgerTask(runID string) *pb.ExecuteTask {
	return &pb.ExecuteTask{
		TaskId:    "task_ledger",
		RunId:     runID,
		SessionId: "sess_ledger",
		Attempt:   1,
		Prompt:    "test prompt",
	}
}

// countAccepted reports how many TaskAccepted messages reached the server.
// A redelivery that is correctly ignored produces none.
func countAccepted(msgs []*pb.RunnerMessage) int {
	n := 0
	for _, msg := range msgs {
		if msg.GetTaskAccepted() != nil {
			n++
		}
	}
	return n
}

// A command re-delivered while its run is still executing must be dropped, not
// run a second time against the same workspace.
func TestTaskRunner_Execute_DuplicateInFlightIsIgnored(t *testing.T) {
	h := newLedgerHarness(t)

	firstResult := make(chan *pb.RunnerMessage, 1)
	go func() {
		result, err := h.runner.Execute(context.Background(), ledgerTask("trun_dup"))
		assert.NoError(t, err)
		firstResult <- result
	}()

	// Wait until the run is genuinely inside the executor.
	<-h.entered

	result, err := h.runner.Execute(context.Background(), ledgerTask("trun_dup"))
	require.NoError(t, err)
	assert.Nil(t, result, "a re-delivered in-flight run must produce no message")
	assert.Equal(t, int32(1), h.calls.Load(), "the prompt must not run twice")

	close(h.release)

	first := <-firstResult
	require.NotNil(t, first)
	require.NotNil(t, first.GetTaskCompleted())
	assert.True(t, first.GetTaskCompleted().Success)

	// One accepted, from the first delivery: the duplicate said nothing.
	assert.Equal(t, 1, countAccepted(h.sender.Messages()))
}

// A command re-delivered after its run finished means the server missed the
// completion. Re-send the result it produced; do not run the prompt again.
func TestTaskRunner_Execute_DuplicateCompletedResendsResult(t *testing.T) {
	h := newLedgerHarness(t)
	close(h.release)

	first, err := h.runner.Execute(context.Background(), ledgerTask("trun_done"))
	require.NoError(t, err)
	require.NotNil(t, first)
	<-h.entered

	second, err := h.runner.Execute(context.Background(), ledgerTask("trun_done"))
	require.NoError(t, err)
	require.NotNil(t, second, "a completed run must be answered, not silently dropped")

	assert.Same(t, first, second, "the recorded terminal result must be re-sent verbatim")
	assert.Equal(t, int32(1), h.calls.Load(), "the prompt must not run twice")
	assert.Equal(t, 1, countAccepted(h.sender.Messages()),
		"the re-delivery must not re-announce the task as accepted")
}

// A different run id is a real second dispatch. The agent passes it through;
// whether it should have happened is the server's CAS to decide.
func TestTaskRunner_Execute_DistinctRunIDsBothExecute(t *testing.T) {
	h := newLedgerHarness(t)
	close(h.release)

	first, err := h.runner.Execute(context.Background(), ledgerTask("trun_one"))
	require.NoError(t, err)
	require.NotNil(t, first.GetTaskCompleted())
	<-h.entered

	second, err := h.runner.Execute(context.Background(), ledgerTask("trun_two"))
	require.NoError(t, err)
	require.NotNil(t, second.GetTaskCompleted())
	<-h.entered

	assert.Equal(t, int32(2), h.calls.Load())
	assert.Equal(t, "trun_one", first.GetTaskCompleted().RunId)
	assert.Equal(t, "trun_two", second.GetTaskCompleted().RunId)
	assert.Equal(t, 2, countAccepted(h.sender.Messages()))
}

// A run interrupted for a session detach produces no terminal message, so
// there is nothing to re-send: when the server sends it back after the resume
// it has to execute, not be swallowed as a duplicate.
func TestTaskRunner_Execute_DetachedRunIsRunnableAgain(t *testing.T) {
	h := newLedgerHarness(t)

	detached := make(chan *pb.RunnerMessage, 1)
	go func() {
		result, err := h.runner.Execute(context.Background(), ledgerTask("trun_detach"))
		assert.NoError(t, err)
		detached <- result
	}()

	<-h.entered
	require.NoError(t, h.runner.CancelTask("sess_ledger"))
	close(h.release)

	assert.Nil(t, <-detached, "a detached run sends no TaskCompleted")

	result, err := h.runner.Execute(context.Background(), ledgerTask("trun_detach"))
	require.NoError(t, err)
	require.NotNil(t, result, "the run must be executable again after the resume")
	assert.Equal(t, int32(2), h.calls.Load())
	<-h.entered
}

// Without a run id there is nothing to deduplicate on, so both deliveries run
// - the same as before the ledger existed.
func TestTaskRunner_Execute_EmptyRunIDIsUntracked(t *testing.T) {
	h := newLedgerHarness(t)
	close(h.release)

	for range 2 {
		result, err := h.runner.Execute(context.Background(), ledgerTask(""))
		require.NoError(t, err)
		require.NotNil(t, result)
		<-h.entered
	}

	assert.Equal(t, int32(2), h.calls.Load())
}

func TestRunLedger_Begin(t *testing.T) {
	l := newRunLedger(completedRunsWindow)

	decision, recorded := l.begin("trun_a")
	assert.Equal(t, runFresh, decision)
	assert.Nil(t, recorded)

	decision, _ = l.begin("trun_a")
	assert.Equal(t, runInFlight, decision, "a claimed run id is in flight until it finishes")

	decision, _ = l.begin("trun_b")
	assert.Equal(t, runFresh, decision, "a different run id is unaffected")

	result := &pb.RunnerMessage{}
	l.finish("trun_a", result)

	decision, recorded = l.begin("trun_a")
	assert.Equal(t, runCompleted, decision)
	assert.Same(t, result, recorded)
}

func TestRunLedger_FinishWithoutResultForgetsTheRun(t *testing.T) {
	l := newRunLedger(completedRunsWindow)

	_, _ = l.begin("trun_a")
	l.finish("trun_a", nil)

	decision, recorded := l.begin("trun_a")
	assert.Equal(t, runFresh, decision, "a run with no recorded result must be runnable again")
	assert.Nil(t, recorded)
}

func TestRunLedger_CompletedWindowIsBounded(t *testing.T) {
	const window = 4
	l := newRunLedger(window)

	ids := []string{"trun_0", "trun_1", "trun_2", "trun_3", "trun_4", "trun_5"}
	for _, id := range ids {
		_, _ = l.begin(id)
		l.finish(id, &pb.RunnerMessage{})
	}

	// The oldest two fell out of the window; the newest four are remembered.
	for _, id := range ids[:2] {
		decision, _ := l.begin(id)
		assert.Equal(t, runFresh, decision, "%s should have been evicted", id)
		l.finish(id, nil)
	}
	for _, id := range ids[2:] {
		decision, _ := l.begin(id)
		assert.Equal(t, runCompleted, decision, "%s should still be remembered", id)
	}

	l.mu.Lock()
	defer l.mu.Unlock()
	assert.LessOrEqual(t, len(l.completed), window)
	assert.LessOrEqual(t, len(l.order), window)
}

// Re-finishing a run id must not grow the eviction queue with a second entry
// for it, which would evict a younger run early.
func TestRunLedger_FinishTwiceKeepsOneEntry(t *testing.T) {
	l := newRunLedger(completedRunsWindow)

	_, _ = l.begin("trun_a")
	l.finish("trun_a", &pb.RunnerMessage{})
	l.finish("trun_a", &pb.RunnerMessage{})

	l.mu.Lock()
	defer l.mu.Unlock()
	assert.Equal(t, []string{"trun_a"}, l.order)
}

// Concurrent claims of one run id must produce exactly one winner: the ledger
// is read by the dispatcher's per-task goroutines, so this runs under -race.
func TestRunLedger_ConcurrentBeginClaimsOnce(t *testing.T) {
	l := newRunLedger(completedRunsWindow)

	const goroutines = 32
	var fresh atomic.Int32
	var wg sync.WaitGroup
	start := make(chan struct{})

	for range goroutines {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			if decision, _ := l.begin("trun_race"); decision == runFresh {
				fresh.Add(1)
			}
		}()
	}

	close(start)
	wg.Wait()

	assert.Equal(t, int32(1), fresh.Load(), "exactly one caller may claim a run id")
}
