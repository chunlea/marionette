package core

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/chunlea/marionette/pkg/store"
)

func TestRedispatchBackoff(t *testing.T) {
	// Growing, then flat: a poisoned task must not hot-loop a pool, and must
	// not back off past the point of being effectively abandoned either.
	assert.Equal(t, 5*time.Second, redispatchBackoff(1))
	assert.Equal(t, 15*time.Second, redispatchBackoff(2))
	assert.Equal(t, 5*time.Minute, redispatchBackoff(5))
	assert.Equal(t, 5*time.Minute, redispatchBackoff(99), "the ceiling holds")
	assert.Equal(t, 5*time.Second, redispatchBackoff(0), "a nonsense attempt is treated as the first")
}

func TestEligibleForRedispatch(t *testing.T) {
	now := time.Now()
	future := now.Add(time.Minute)
	past := now.Add(-time.Minute)
	parked := "gave up"

	assert.True(t, eligibleForRedispatch(&store.Task{Status: TaskStatusPending}, false, now),
		"a task that has never failed is eligible now")
	assert.False(t, eligibleForRedispatch(&store.Task{Status: TaskStatusRunning}, false, now),
		"only pending tasks are dispatched")
	assert.False(t, eligibleForRedispatch(
		&store.Task{Status: TaskStatusPending, NextDispatchAfter: &future}, false, now),
		"a backing-off task waits for its timer")
	assert.True(t, eligibleForRedispatch(
		&store.Task{Status: TaskStatusPending, NextDispatchAfter: &past}, false, now),
		"an expired timer releases the task")

	// The edge-trigger exemption, and its limit.
	assert.True(t, eligibleForRedispatch(
		&store.Task{Status: TaskStatusPending, NextDispatchAfter: &future}, true, now),
		"a runner becoming available earns one immediate attempt")
	assert.False(t, eligibleForRedispatch(
		&store.Task{Status: TaskStatusPending, DispatchParkedReason: &parked}, true, now),
		"parking is terminal: not even an edge trigger overrides it")
}

// TestRecordDispatchFailure_BacksOffThenParks is the budget in action. Parking
// leaves the task exactly where tasks sat before any of this existed, so the
// fallback is the status quo.
func TestRecordDispatchFailure_BacksOffThenParks(t *testing.T) {
	s := newTestTaskStore()
	manager := NewTaskManager(s, &mockCommandSender{}, &mockSessionMgrForTask{}, nil, zap.NewNop(),
		WithRedispatchMaxAttempts(3))

	s.tasks["task_1"] = &store.Task{ID: "task_1", SessionID: "sess_1", Status: TaskStatusPending}
	ctx := context.Background()
	cause := errors.New("runner unreachable")

	manager.recordDispatchFailure(ctx, "task_1", cause)
	got, err := s.GetTask(ctx, "task_1")
	require.NoError(t, err)
	assert.Equal(t, 1, got.DispatchAttempts)
	require.NotNil(t, got.NextDispatchAfter, "a backoff timer must be recorded")
	assert.Nil(t, got.DispatchParkedReason)

	manager.recordDispatchFailure(ctx, "task_1", cause)
	got, err = s.GetTask(ctx, "task_1")
	require.NoError(t, err)
	assert.Equal(t, 2, got.DispatchAttempts)
	assert.Nil(t, got.DispatchParkedReason)

	manager.recordDispatchFailure(ctx, "task_1", cause)
	got, err = s.GetTask(ctx, "task_1")
	require.NoError(t, err)
	assert.Equal(t, 3, got.DispatchAttempts)
	require.NotNil(t, got.DispatchParkedReason, "the budget is spent, so the task parks")
	assert.Contains(t, *got.DispatchParkedReason, "runner unreachable")
	assert.Equal(t, TaskStatusPending, got.Status,
		"a parked task stays pending, waiting for a human")
}

// TestDispatch_ClearsBackoffOnSuccess: backoff describes a failing runner, and
// once one accepts the work it no longer describes anything.
func TestDispatch_ClearsBackoffOnSuccess(t *testing.T) {
	manager, s, _, _ := setupAutoDispatchTest()
	earlier := time.Now().Add(-time.Minute)
	s.tasks["task_1"] = &store.Task{
		ID: "task_1", SessionID: "sess_123", Status: TaskStatusPending, Prompt: "go",
		DispatchAttempts: 3, NextDispatchAfter: &earlier,
	}

	require.NoError(t, manager.Execute(context.Background(), "task_1"))

	got, err := s.GetTask(context.Background(), "task_1")
	require.NoError(t, err)
	assert.Zero(t, got.DispatchAttempts)
	assert.Nil(t, got.NextDispatchAfter)
	assert.Nil(t, got.DispatchParkedReason)
}

// TestDispatchNext_RespectsBackoffButEdgeTriggersDoNot separates the two
// callers: the sweeper waits, a newly attached runner does not.
func TestDispatchNext_RespectsBackoffButEdgeTriggersDoNot(t *testing.T) {
	manager, s, cmdSender, _ := setupAutoDispatchTest()
	future := time.Now().Add(time.Hour)
	s.tasks["task_1"] = &store.Task{
		ID: "task_1", SessionID: "sess_123", Status: TaskStatusPending, Prompt: "go",
		DispatchAttempts: 1, NextDispatchAfter: &future,
	}

	require.NoError(t, manager.DispatchNext(context.Background(), "sess_123"))
	assert.Empty(t, cmdSender.sentCommands, "the sweeper waits out the backoff")

	require.NoError(t, manager.DispatchNextNow(context.Background(), "sess_123"))
	assert.Len(t, cmdSender.sentCommands, 1, "a new runner earns an immediate attempt")
}

func TestDispatchNext_SkipsParkedTasks(t *testing.T) {
	manager, s, cmdSender, _ := setupAutoDispatchTest()
	parked := "gave up after 6 attempts"
	s.tasks["task_1"] = &store.Task{
		ID: "task_1", SessionID: "sess_123", Status: TaskStatusPending, Prompt: "go",
		DispatchParkedReason: &parked,
	}

	require.NoError(t, manager.DispatchNext(context.Background(), "sess_123"))
	require.NoError(t, manager.DispatchNextNow(context.Background(), "sess_123"))
	assert.Empty(t, cmdSender.sentCommands, "a parked task waits for a human, not a timer")
}

// redispatchFixture wires the pieces a trigger test needs: a store, a task
// manager whose commands are observable, and the waker every trigger routes
// through.
type redispatchFixture struct {
	store      *testTaskStore
	tasks      *TaskManager
	sessions   *mockSessionMgrForTask
	cmdSender  *mockCommandSender
	waker      *DispatchWaker
	background *backgroundTasks
}

func newRedispatchFixture() *redispatchFixture {
	s := newTestTaskStore()
	cmdSender := &mockCommandSender{}
	sessions := &mockSessionMgrForTask{store: s}
	tasks := NewTaskManager(s, cmdSender, sessions, nil, zap.NewNop())
	background := newBackgroundTasks(context.Background(), 0, zap.NewNop())
	return &redispatchFixture{
		store:      s,
		tasks:      tasks,
		sessions:   sessions,
		cmdSender:  cmdSender,
		waker:      NewDispatchWaker(s, sessions, tasks, background, nil, zap.NewNop()),
		background: background,
	}
}

// TestRedispatchSweeper_DispatchesDueWork is the backstop trigger: this is the
// path that survives a restart, which every edge trigger does not.
func TestRedispatchSweeper_DispatchesDueWork(t *testing.T) {
	f := newRedispatchFixture()

	runnerID := "run_1"
	f.store.sessions["sess_1"] = &store.Session{
		ID: "sess_1", Status: SessionStatusActive, RunnerID: &runnerID,
	}
	past := time.Now().Add(-time.Minute)
	f.store.tasks["task_1"] = &store.Task{
		ID: "task_1", SessionID: "sess_1", Status: TaskStatusPending, Prompt: "go",
		DispatchAttempts: 1, NextDispatchAfter: &past,
	}

	sweeper := NewRedispatchSweeper(f.waker, zap.NewNop())
	require.NoError(t, sweeper.Sweep(context.Background()))

	assert.Len(t, f.cmdSender.sentCommands, 1, "an expired backoff must be picked up")
}

func TestRedispatchSweeper_LeavesUndueWorkAlone(t *testing.T) {
	f := newRedispatchFixture()

	runnerID := "run_1"
	f.store.sessions["sess_1"] = &store.Session{
		ID: "sess_1", Status: SessionStatusActive, RunnerID: &runnerID,
	}
	future := time.Now().Add(time.Hour)
	parked := "gave up"
	f.store.tasks["backing_off"] = &store.Task{
		ID: "backing_off", SessionID: "sess_1", Status: TaskStatusPending,
		NextDispatchAfter: &future, CreatedAt: time.Now(),
	}
	f.store.tasks["parked"] = &store.Task{
		ID: "parked", SessionID: "sess_1", Status: TaskStatusPending,
		DispatchParkedReason: &parked, CreatedAt: time.Now().Add(-time.Hour),
	}

	require.NoError(t, NewRedispatchSweeper(f.waker, zap.NewNop()).Sweep(context.Background()))
	assert.Empty(t, f.cmdSender.sentCommands)
}

// TestRedispatchSweeper_SkipsSessionsWithNoRunnerToBeHad: a pass asks the
// session manager for a runner, and when there is none the session stays parked
// rather than being dispatched into nowhere.
func TestRedispatchSweeper_SkipsSessionsWithNoRunnerToBeHad(t *testing.T) {
	f := newRedispatchFixture()
	f.sessions.ensureRunnerErr = ErrNoRunnerAvailable

	f.store.sessions["sess_1"] = &store.Session{ID: "sess_1", Status: SessionStatusActive}
	f.store.tasks["task_1"] = &store.Task{
		ID: "task_1", SessionID: "sess_1", Status: TaskStatusPending, Prompt: "go",
	}

	require.NoError(t, NewRedispatchSweeper(f.waker, zap.NewNop()).Sweep(context.Background()))
	assert.Empty(t, f.cmdSender.sentCommands)
}

func TestRedispatchSweeper_StartStop(t *testing.T) {
	f := newRedispatchFixture()
	sweeper := NewRedispatchSweeper(f.waker, zap.NewNop(), WithRedispatchInterval(time.Hour))

	sweeper.Start(context.Background())
	sweeper.Start(context.Background()) // second Start is a no-op
	sweeper.Stop()
	sweeper.Stop() // second Stop is a no-op
}
