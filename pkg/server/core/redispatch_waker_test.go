package core

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/chunlea/marionette/pkg/store"
)

// recordingWaker records the triggers a manager fires without doing any work.
type recordingWaker struct {
	mu       sync.Mutex
	triggers []string
}

func (w *recordingWaker) RunnerAvailable(_ context.Context, trigger string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.triggers = append(w.triggers, trigger)
}

func (w *recordingWaker) seen() []string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]string(nil), w.triggers...)
}

// countingStore counts the queries a pass makes, so "a wake that finds nothing
// must be free" is an assertion rather than a claim.
type countingStore struct {
	*testTaskStore
	mu           sync.Mutex
	listSessions int
	listTasks    int
}

func (s *countingStore) ListSessions(ctx context.Context, opts store.ListSessionsOptions) (*store.ListResult[store.Session], error) {
	s.mu.Lock()
	s.listSessions++
	s.mu.Unlock()
	return s.testTaskStore.ListSessions(ctx, opts)
}

func (s *countingStore) ListTasks(ctx context.Context, opts store.ListTasksOptions) (*store.ListResult[store.Task], error) {
	s.mu.Lock()
	s.listTasks++
	s.mu.Unlock()
	return s.testTaskStore.ListTasks(ctx, opts)
}

func (s *countingStore) counts() (sessions, tasks int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.listSessions, s.listTasks
}

// =============================================================================
// The pass
// =============================================================================

// TestDispatchWaker_WakesAParkedSessionOntoAFreedRunner is trigger 2 in one
// test: a session with a backlog and no runner is exactly the dead end
// ErrNoRunnerAttached used to leave behind, and the freed runner is the new
// information that resolves it.
func TestDispatchWaker_WakesAParkedSessionOntoAFreedRunner(t *testing.T) {
	f := newRedispatchFixture()
	f.sessions.store = f.store
	f.sessions.allocateRunnerID = "run_freed"

	f.store.sessions["sess_parked"] = &store.Session{
		ID: "sess_parked", Status: SessionStatusPending,
	}
	f.store.tasks["task_1"] = &store.Task{
		ID: "task_1", SessionID: "sess_parked", Status: TaskStatusPending,
		Prompt: "go", CreatedAt: time.Now(),
	}

	require.NoError(t, f.waker.WakeAndWait(context.Background(), WakeTriggerRunnerFreed))

	require.Len(t, f.cmdSender.sentCommands, 1, "the parked backlog must go out")
	assert.Equal(t, "task_1", f.cmdSender.sentCommands[0].GetExecuteTask().GetTaskId())

	session, err := f.store.GetSession(context.Background(), "sess_parked")
	require.NoError(t, err)
	require.NotNil(t, session.RunnerID)
	assert.Equal(t, "run_freed", *session.RunnerID)
}

// TestDispatchWaker_LeavesSuspendedSessionsAlone: waking a suspended session
// costs a runner acquisition and possibly a workspace restore. That stays an
// explicit user action - a proposal non-goal, restated as a query filter.
func TestDispatchWaker_LeavesSuspendedSessionsAlone(t *testing.T) {
	f := newRedispatchFixture()
	f.sessions.store = f.store
	f.sessions.allocateRunnerID = "run_freed"

	f.store.sessions["sess_susp"] = &store.Session{
		ID: "sess_susp", Status: SessionStatusSuspended,
	}
	f.store.tasks["task_1"] = &store.Task{
		ID: "task_1", SessionID: "sess_susp", Status: TaskStatusPending, Prompt: "go",
	}

	require.NoError(t, f.waker.WakeAndWait(context.Background(), WakeTriggerRunnerFreed))
	assert.Empty(t, f.cmdSender.sentCommands)
	assert.Zero(t, f.sessions.calls(), "a suspended session is not even asked about a runner")
}

// TestDispatchWaker_SkipsSessionsWithATaskInFlight keeps the one-task-per-session
// invariant: the next task goes out when this one finishes, not when a runner
// somewhere else frees up.
func TestDispatchWaker_SkipsSessionsWithATaskInFlight(t *testing.T) {
	f := newRedispatchFixture()
	runnerID := "run_1"
	f.store.sessions["sess_1"] = &store.Session{
		ID: "sess_1", Status: SessionStatusActive, RunnerID: &runnerID,
	}
	f.store.tasks["running"] = &store.Task{
		ID: "running", SessionID: "sess_1", Status: TaskStatusRunning,
		CreatedAt: time.Now().Add(-time.Hour),
	}
	f.store.tasks["waiting"] = &store.Task{
		ID: "waiting", SessionID: "sess_1", Status: TaskStatusPending,
		Prompt: "next", CreatedAt: time.Now(),
	}

	require.NoError(t, f.waker.WakeAndWait(context.Background(), WakeTriggerRunnerFreed))
	assert.Empty(t, f.cmdSender.sentCommands)
}

// TestDispatchWaker_JudgesTheTaskADispatchWouldPick. DispatchNext takes the
// oldest pending task, so eligibility has to be judged on that one. Asking "is
// any task due" instead wakes this session on every single trigger and the
// dispatch then correctly does nothing - the trigger noise the counters exist
// to expose.
func TestDispatchWaker_JudgesTheTaskADispatchWouldPick(t *testing.T) {
	f := newRedispatchFixture()
	runnerID := "run_1"
	f.store.sessions["sess_1"] = &store.Session{
		ID: "sess_1", Status: SessionStatusActive, RunnerID: &runnerID,
	}
	parked := "gave up after 6 dispatch attempts"
	f.store.tasks["oldest_parked"] = &store.Task{
		ID: "oldest_parked", SessionID: "sess_1", Status: TaskStatusPending,
		DispatchParkedReason: &parked, CreatedAt: time.Now().Add(-time.Hour),
	}
	f.store.tasks["newer_ready"] = &store.Task{
		ID: "newer_ready", SessionID: "sess_1", Status: TaskStatusPending,
		Prompt: "go", CreatedAt: time.Now(),
	}

	require.NoError(t, f.waker.WakeAndWait(context.Background(), WakeTriggerRunnerFreed))
	assert.Empty(t, f.cmdSender.sentCommands,
		"the head of the queue is parked, so the session is not woken")
}

// TestDispatchWaker_SweepRespectsBackoffAndEdgesDoNot separates the two
// callers. The timer waits out a failure's backoff; a runner genuinely
// becoming available is new information and earns one immediate attempt.
func TestDispatchWaker_SweepRespectsBackoffAndEdgesDoNot(t *testing.T) {
	f := newRedispatchFixture()
	runnerID := "run_1"
	f.store.sessions["sess_1"] = &store.Session{
		ID: "sess_1", Status: SessionStatusActive, RunnerID: &runnerID,
	}
	future := time.Now().Add(time.Hour)
	f.store.tasks["task_1"] = &store.Task{
		ID: "task_1", SessionID: "sess_1", Status: TaskStatusPending, Prompt: "go",
		DispatchAttempts: 1, NextDispatchAfter: &future, CreatedAt: time.Now(),
	}

	require.NoError(t, f.waker.WakeAndWait(context.Background(), WakeTriggerSweep))
	assert.Empty(t, f.cmdSender.sentCommands, "the sweep waits out the backoff")

	require.NoError(t, f.waker.WakeAndWait(context.Background(), WakeTriggerRunnerJoined))
	assert.Len(t, f.cmdSender.sentCommands, 1, "a new runner earns an immediate attempt")
}

// TestDispatchWaker_EmptyPassIsCheap. Every trigger fires optimistically and
// most find nothing, so the empty pass is the case that decides whether "wakes
// are cheap and idempotent" is a design or a wish.
func TestDispatchWaker_EmptyPassIsCheap(t *testing.T) {
	counting := &countingStore{testTaskStore: newTestTaskStore()}
	sessions := &mockSessionMgrForTask{}
	tasks := NewTaskManager(counting, &mockCommandSender{}, sessions, nil, zap.NewNop())
	waker := NewDispatchWaker(counting, sessions, tasks, nil, nil, zap.NewNop())

	// One active session with no backlog: the shape of a healthy deployment.
	runnerID := "run_1"
	counting.sessions["sess_1"] = &store.Session{
		ID: "sess_1", Status: SessionStatusActive, RunnerID: &runnerID,
	}

	require.NoError(t, waker.WakeAndWait(context.Background(), WakeTriggerRunnerFreed))

	sessionQueries, taskQueries := counting.counts()
	assert.Equal(t, 1, sessionQueries, "one page of sessions")
	assert.Equal(t, 1, taskQueries,
		"pending and running are read together, so an idle session costs one query")
	assert.Zero(t, sessions.calls(), "a session that already has a runner is not reallocated")
}

// TestDispatchWaker_StopsWhenThereIsNoCapacity. The overwhelmingly common
// reason a parked session cannot be woken is that nothing is spare, and in that
// case every remaining session fails identically. Paying for all of them turns
// one freed runner into a full scan.
func TestDispatchWaker_StopsWhenThereIsNoCapacity(t *testing.T) {
	f := newRedispatchFixture()
	f.sessions.ensureRunnerErr = ErrNoRunnerAvailable

	for _, id := range []string{"sess_1", "sess_2", "sess_3", "sess_4", "sess_5"} {
		f.store.sessions[id] = &store.Session{ID: id, Status: SessionStatusPending}
		f.store.tasks["task_"+id] = &store.Task{
			ID: "task_" + id, SessionID: id, Status: TaskStatusPending,
			Prompt: "go", CreatedAt: time.Now(),
		}
	}

	require.NoError(t, f.waker.WakeAndWait(context.Background(), WakeTriggerRunnerFreed))
	assert.Equal(t, redispatchNoRunnerStreak, f.sessions.calls(),
		"the pass gives up once the answer is clearly the same for everyone")
	assert.Empty(t, f.cmdSender.sentCommands)
}

// =============================================================================
// Coalescing
// =============================================================================

// TestDispatchWaker_ConcurrentWakesDispatchExactlyOnce is the regression that
// matters. A suspend, a disconnect, a runner going idle and a sweep tick can
// all fire around one event, and the cost of getting this wrong is not a wasted
// cycle: it is the same prompt executed twice, concurrently, against one
// workspace, by an agent with shell access.
//
// Nothing here relies on the coalescing gate for correctness - the database CAS
// on the pending->running transition is the anchor. The gate is what keeps the
// storm cheap; the CAS is what keeps it safe. This test would still pass with
// coalescing removed, and that is the point.
func TestDispatchWaker_ConcurrentWakesDispatchExactlyOnce(t *testing.T) {
	triggers := []string{
		WakeTriggerRunnerFreed,
		WakeTriggerRunnerJoined,
		WakeTriggerSweep,
		WakeTriggerRunnerFreed,
	}

	tests := []struct {
		name string
		// seed sets the session up and reports whether a bare DispatchNext is
		// a legitimate racer. It is not for a session with no runner: that
		// call correctly reports ErrNoRunnerAttached, which is a different
		// question from the one this test asks.
		seed         func(*redispatchFixture)
		alsoDispatch bool
	}{
		{
			name: "the session already holds a runner",
			seed: func(f *redispatchFixture) {
				runnerID := "run_1"
				f.store.sessions["sess_1"] = &store.Session{
					ID: "sess_1", Status: SessionStatusActive, RunnerID: &runnerID,
				}
			},
			alsoDispatch: true,
		},
		{
			name: "the wake has to allocate the runner too",
			seed: func(f *redispatchFixture) {
				f.sessions.store = f.store
				f.sessions.allocateRunnerID = "run_1"
				f.store.sessions["sess_1"] = &store.Session{
					ID: "sess_1", Status: SessionStatusPending,
				}
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			for round := 0; round < 20; round++ {
				f := newRedispatchFixture()
				tc.seed(f)
				f.store.tasks["task_1"] = &store.Task{
					ID: "task_1", SessionID: "sess_1", Status: TaskStatusPending,
					Prompt: "only once", CreatedAt: time.Now(),
				}

				var wg sync.WaitGroup
				start := make(chan struct{})
				for _, trigger := range triggers {
					wg.Add(1)
					go func(trigger string) {
						defer wg.Done()
						<-start
						assert.NoError(t, f.waker.WakeAndWait(context.Background(), trigger))
					}(trigger)

					if !tc.alsoDispatch {
						continue
					}
					// The direct path races the wakes: creating a task
					// dispatches it while a freed runner wakes the same
					// session.
					wg.Add(1)
					go func() {
						defer wg.Done()
						<-start
						assert.NoError(t, f.tasks.DispatchNext(context.Background(), "sess_1"))
					}()
				}
				close(start)
				wg.Wait()

				require.Len(t, f.cmdSender.sentCommands, 1,
					"a pending task must reach a runner exactly once, round %d", round)

				runs, err := f.store.ListTaskRuns(context.Background(), store.ListTaskRunsOptions{})
				require.NoError(t, err)
				assert.Len(t, runs.Items, 1, "a lost race must not leave an orphan run behind")
			}
		})
	}
}

// TestDispatchWaker_CoalescesWakesArrivingDuringAPass: a storm costs two passes,
// not one per trigger.
func TestDispatchWaker_CoalescesWakesArrivingDuringAPass(t *testing.T) {
	f := newRedispatchFixture()

	// Hold the gate as if a pass were running, then fire a burst at it.
	f.waker.mu.Lock()
	f.waker.inFlight = true
	f.waker.mu.Unlock()

	for i := 0; i < 10; i++ {
		require.NoError(t, f.waker.WakeAndWait(context.Background(), WakeTriggerRunnerFreed))
	}

	f.waker.mu.Lock()
	pending := f.waker.pending
	f.waker.mu.Unlock()
	assert.True(t, pending, "ten wakes during a pass collapse into exactly one follow-up")
}

// TestDispatchWaker_RunnerAvailableSurvivesASaturatedPool. A rejected background
// spawn must reopen the gate: leaving it closed would swallow every wake for
// the life of the process, which is a far worse failure than a dropped pass.
func TestDispatchWaker_RunnerAvailableSurvivesASaturatedPool(t *testing.T) {
	f := newRedispatchFixture()

	// A pool that is shutting down rejects everything.
	require.NoError(t, f.background.Wait(context.Background()))

	f.waker.RunnerAvailable(context.Background(), WakeTriggerRunnerFreed)

	f.waker.mu.Lock()
	inFlight := f.waker.inFlight
	f.waker.mu.Unlock()
	assert.False(t, inFlight, "a rejected pass must not wedge the gate shut")
}

// =============================================================================
// Trigger wiring
// =============================================================================

// TestRunnerManager_OnConnect_WakesRedispatch is trigger 3: a pool that was
// empty when the task was created is never revisited without it.
func TestRunnerManager_OnConnect_WakesRedispatch(t *testing.T) {
	s := newTestRunnerStore()
	s.SetRunner(&store.Runner{ID: "run_1", Status: StatusOffline})
	waker := &recordingWaker{}
	background := newBackgroundTasks(context.Background(), 0, zap.NewNop())
	manager := NewRunnerManager(&testStoreWrapper{testStore: s}, newTestConnManager(), zap.NewNop(),
		WithRunnerWaker(waker), WithRunnerBackground(background))

	require.NoError(t, manager.OnConnect(context.Background(), "run_1"))
	require.NoError(t, background.Wait(context.Background()))

	assert.Equal(t, []string{WakeTriggerRunnerJoined}, waker.seen())
}

// TestRunnerManager_SetStatus_WakesOnlyOnTheEdgeIntoIdle is trigger 2's
// busy->idle half. A runner that just finished a task is the likeliest moment
// a different parked session can proceed, and this is the only place the server
// hears about it.
func TestRunnerManager_SetStatus_WakesOnlyOnTheEdgeIntoIdle(t *testing.T) {
	s := newTestRunnerStore()
	s.SetRunner(&store.Runner{ID: "run_1", Status: StatusIdle})
	waker := &recordingWaker{}
	manager := NewRunnerManager(&testStoreWrapper{testStore: s}, newTestConnManager(), zap.NewNop(),
		WithRunnerWaker(waker))

	require.NoError(t, manager.SetStatus(context.Background(), "run_1", StatusBusy))
	assert.Empty(t, waker.seen(), "taking work is not freeing a runner")

	require.NoError(t, manager.SetStatus(context.Background(), "run_1", StatusIdle))
	assert.Equal(t, []string{WakeTriggerRunnerFreed}, waker.seen())

	require.NoError(t, manager.SetStatus(context.Background(), "run_1", StatusIdle))
	assert.Len(t, waker.seen(), 1, "idle -> idle is not an edge and must not wake")
}

// TestSessionManager_WakesWhenItGivesUpARunner covers the three ways a session
// hands its runner back.
func TestSessionManager_WakesWhenItGivesUpARunner(t *testing.T) {
	tests := []struct {
		name string
		act  func(*SessionManager) error
		want int
	}{
		{
			name: "suspend",
			act: func(m *SessionManager) error {
				return m.Suspend(context.Background(), "sess_1", "release_to_pool")
			},
			want: 1,
		},
		{
			name: "detach",
			act: func(m *SessionManager) error {
				return m.DetachRunner(context.Background(), "sess_1")
			},
			want: 1,
		},
		{
			name: "terminate",
			act: func(m *SessionManager) error {
				return m.Terminate(context.Background(), "sess_1")
			},
			want: 1,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			manager, s := setupSessionManagerTest()
			waker := &recordingWaker{}
			manager.setWaker(waker)

			runnerID := "run_1"
			s.SetSession(&store.Session{
				ID: "sess_1", Status: SessionStatusActive, RunnerID: &runnerID,
			})

			require.NoError(t, tc.act(manager))
			assert.Len(t, waker.seen(), tc.want)
			for _, trigger := range waker.seen() {
				assert.Equal(t, WakeTriggerRunnerFreed, trigger)
			}
		})
	}
}

// TestSessionManager_DoesNotWakeWhenItHeldNoRunner: there is nothing new for
// anyone to use, so the wake would be pure noise.
func TestSessionManager_DoesNotWakeWhenItHeldNoRunner(t *testing.T) {
	manager, s := setupSessionManagerTest()
	waker := &recordingWaker{}
	manager.setWaker(waker)

	s.SetSession(&store.Session{ID: "sess_1", Status: SessionStatusActive})

	require.NoError(t, manager.Terminate(context.Background(), "sess_1"))
	assert.Empty(t, waker.seen())
}

// =============================================================================
// Regressions the load test found
// =============================================================================

// TestTaskManager_OnTaskCompleted_DispatchesTheNextTask.
//
// A task finishing is what frees the session's one-task-in-flight slot, so it is
// the moment the next task can go out. Nothing fired here, and the runner's
// busy -> idle report cannot stand in for it: the agent sets its status back to
// idle as Execute returns, which is before the completion message is even sent,
// so that wake arrives while the task is still running and correctly does
// nothing.
//
// The backlog therefore waited for the 60s sweeper. The load test found this as
// "8 tasks took two minutes"; four sessions with a queue each ran one task per
// sweep interval no matter how fast the tasks were.
func TestTaskManager_OnTaskCompleted_DispatchesTheNextTask(t *testing.T) {
	s := newTestTaskStore()
	cmdSender := &mockCommandSender{}
	background := newBackgroundTasks(context.Background(), 0, zap.NewNop())
	manager := NewTaskManager(s, cmdSender, &mockSessionMgrForTask{}, nil, zap.NewNop(),
		WithTaskBackground(background))

	runnerID := "run_1"
	s.sessions["sess_1"] = &store.Session{
		ID: "sess_1", Status: SessionStatusActive, RunnerID: &runnerID,
	}
	s.tasks["first"] = &store.Task{
		ID: "first", SessionID: "sess_1", Status: TaskStatusRunning,
		CreatedAt: time.Now().Add(-time.Hour),
	}
	s.tasks["second"] = &store.Task{
		ID: "second", SessionID: "sess_1", Status: TaskStatusPending,
		Prompt: "next please", CreatedAt: time.Now(),
	}
	s.setTaskRun("trun_1", &store.TaskRun{
		ID: "trun_1", TaskID: "first", Attempt: 1, Status: TaskRunStatusRunning,
		RunnerID: &runnerID,
	})

	require.NoError(t, manager.OnTaskCompleted(context.Background(), &TaskCompletedResult{
		RunID: "trun_1", Success: true,
	}))
	require.NoError(t, background.Wait(context.Background()))

	require.Len(t, cmdSender.sentCommands, 1,
		"finishing a task must pull the next one in the session")
	assert.Equal(t, "second", cmdSender.sentCommands[0].GetExecuteTask().GetTaskId())
}

// TestTaskManager_OnTaskCompleted_LeavesAnEmptyBacklogAlone: the dispatch after
// a completion is a trigger like any other, so finding nothing has to be free
// and silent rather than an error.
func TestTaskManager_OnTaskCompleted_LeavesAnEmptyBacklogAlone(t *testing.T) {
	s := newTestTaskStore()
	cmdSender := &mockCommandSender{}
	background := newBackgroundTasks(context.Background(), 0, zap.NewNop())
	manager := NewTaskManager(s, cmdSender, &mockSessionMgrForTask{}, nil, zap.NewNop(),
		WithTaskBackground(background))

	runnerID := "run_1"
	s.sessions["sess_1"] = &store.Session{
		ID: "sess_1", Status: SessionStatusActive, RunnerID: &runnerID,
	}
	s.tasks["only"] = &store.Task{
		ID: "only", SessionID: "sess_1", Status: TaskStatusRunning, CreatedAt: time.Now(),
	}
	s.setTaskRun("trun_1", &store.TaskRun{
		ID: "trun_1", TaskID: "only", Attempt: 1, Status: TaskRunStatusRunning,
		RunnerID: &runnerID,
	})

	require.NoError(t, manager.OnTaskCompleted(context.Background(), &TaskCompletedResult{
		RunID: "trun_1", Success: true,
	}))
	require.NoError(t, background.Wait(context.Background()))
	assert.Empty(t, cmdSender.sentCommands)
}

// TestSessionManager_ConcurrentEnsureRunner_GivesEachSessionItsOwnRunner.
//
// runnerClaimed answers from the database, so between selecting an idle runner
// and recording the choice there is a window in which the runner still looks
// free. Two sessions activating at the same time both take it, and Activate
// then detaches the loser - so N concurrent creates against N idle runners left
// most of the sessions with nothing.
//
// The load test found this as "4 sessions, 4 idle runners, 2 sessions stranded".
func TestSessionManager_ConcurrentEnsureRunner_GivesEachSessionItsOwnRunner(t *testing.T) {
	const n = 8

	for round := 0; round < 10; round++ {
		manager, s := setupSessionManagerTestWithCmdSender(&mockCommandSenderForSession{})

		for i := 0; i < n; i++ {
			s.SetRunner(&store.Runner{
				ID: fmt.Sprintf("run_%d", i), Status: StatusIdle,
			})
			s.SetSession(&store.Session{
				ID: fmt.Sprintf("sess_%d", i), Status: SessionStatusPending,
			})
		}

		var wg sync.WaitGroup
		start := make(chan struct{})
		results := make([]*store.Session, n)
		for i := 0; i < n; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				<-start
				session, err := manager.EnsureRunner(context.Background(), fmt.Sprintf("sess_%d", i))
				assert.NoError(t, err)
				results[i] = session
			}(i)
		}
		close(start)
		wg.Wait()

		seen := map[string]string{}
		for i, session := range results {
			require.NotNil(t, session, "session %d got no runner in round %d", i, round)
			require.NotNil(t, session.RunnerID)
			runnerID := *session.RunnerID
			if other, taken := seen[runnerID]; taken {
				t.Fatalf("round %d: runner %s handed to both %s and %s",
					round, runnerID, other, session.ID)
			}
			seen[runnerID] = session.ID
		}
	}
}
