package agent

import (
	"sync"

	pb "github.com/chunlea/marionette/gen/proto/v1"
)

// completedRunsWindow bounds how many finished run ids the ledger remembers.
// It only has to cover the gap between a completion the server missed and the
// re-delivery that follows it, which is a handful of runs at most.
const completedRunsWindow = 64

// runDecision is what the ledger says to do with an incoming ExecuteTask.
type runDecision int

const (
	// runFresh means this run id has not been seen: execute it.
	runFresh runDecision = iota
	// runInFlight means the same run id is already executing here: the
	// command was re-delivered, so ignore it.
	runInFlight
	// runCompleted means the same run id already finished here: re-send the
	// result it produced rather than running the prompt a second time.
	runCompleted
)

// runLedger remembers which run ids this process is executing and which it
// recently finished, so a re-delivered ExecuteTask does not run the same
// attempt twice.
//
// The transport is at-least-once for redelivery: the server's dispatch CAS
// stops two dispatch *decisions* for one run, but nothing stops one decision's
// command reaching the runner twice (a retried send, a reconnect that replays,
// a future outbox). Without this ledger the second copy overwrote
// TaskRunner.currentTask and ran the prompt again - concurrently, against the
// same workspace, with shell and file-write access.
//
// Scope, stated honestly: the ledger is per-process memory. An agent restart
// forgets every run it ever saw, so a redelivery that straddles a restart is
// still executed twice as far as the agent is concerned. Making it durable
// would mean persisting run state on the runner, which is the server's job:
// the dispatch CAS in the task manager remains the durable guard, and this is
// the cheap in-process defence in front of it.
type runLedger struct {
	mu sync.Mutex

	// inFlight holds the run ids currently executing.
	inFlight map[string]struct{}

	// completed maps a recently finished run id to the terminal message it
	// produced, so a redelivery can be answered from memory.
	completed map[string]*pb.RunnerMessage

	// order is the completed run ids oldest-first, used to evict past window.
	order []string

	// window is the maximum number of completed runs kept.
	window int
}

// newRunLedger creates a ledger keeping at most window completed runs.
func newRunLedger(window int) *runLedger {
	if window < 1 {
		window = 1
	}
	return &runLedger{
		inFlight:  make(map[string]struct{}),
		completed: make(map[string]*pb.RunnerMessage),
		window:    window,
	}
}

// begin claims a run id and reports what the caller should do with it.
//
// A run with no id is untracked and always reported fresh: there is nothing to
// deduplicate on, and refusing to run it would be worse than today's behaviour.
func (l *runLedger) begin(runID string) (runDecision, *pb.RunnerMessage) {
	if runID == "" {
		return runFresh, nil
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	if _, ok := l.inFlight[runID]; ok {
		return runInFlight, nil
	}
	if result, ok := l.completed[runID]; ok {
		return runCompleted, result
	}

	l.inFlight[runID] = struct{}{}

	return runFresh, nil
}

// finish releases a run id and, if the run produced a terminal message,
// records it for redelivery.
//
// A nil result means the run ended without one - interrupted for a session
// detach, say - so nothing is remembered: there is no result to re-send, and
// the run must be executable again when the server sends it back.
//
// Releasing and recording happen under one lock so a redelivery arriving at
// this exact moment sees either the in-flight entry or the recorded result,
// never a gap where the run looks fresh.
func (l *runLedger) finish(runID string, result *pb.RunnerMessage) {
	if runID == "" {
		return
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	delete(l.inFlight, runID)

	if result == nil {
		return
	}

	if _, ok := l.completed[runID]; !ok {
		l.order = append(l.order, runID)
	}
	l.completed[runID] = result

	for len(l.order) > l.window {
		delete(l.completed, l.order[0])
		l.order = l.order[1:]
	}
}
