package core

import (
	"context"
	"fmt"
	"testing"
	"time"

	"go.uber.org/zap"

	"github.com/chunlea/marionette/pkg/store"
)

// Benchmarks for the scheduler's hot paths.
//
// They run against the in-memory test store, so what they measure is the
// manager logic: the transaction shape, the number of round trips, the locking,
// the allocations. They deliberately do NOT measure the database - the store
// benchmarks in test/perf/store do that against real PostgreSQL, and mixing the
// two would hide a doubling in query count behind network noise.
//
// Read them as a regression tripwire on the shape of the code, not as a
// throughput figure. See test/perf/BASELINE.md for the recorded numbers and the
// machine they came from.

// benchStore builds a store with one active session on a runner, ready to
// dispatch into.
func benchStore(sessionID, runnerID string) *testTaskStore {
	s := newTestTaskStore()
	s.sessions[sessionID] = &store.Session{
		ID:       sessionID,
		Status:   SessionStatusActive,
		RunnerID: &runnerID,
	}
	return s
}

// BenchmarkTaskDispatch_CreateToAssigned is the main path: a task created on an
// active session reaches a runner without a second API call. It covers Create,
// the runner check, the dispatch transaction, the run insert and the send.
func BenchmarkTaskDispatch_CreateToAssigned(b *testing.B) {
	s := benchStore("sess_bench", "run_bench")
	manager := NewTaskManager(s, &mockCommandSender{}, &mockSessionMgrForTask{}, nil, zap.NewNop())
	ctx := context.Background()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		task, err := manager.Create(ctx, CreateTaskOptions{
			SessionID: "sess_bench",
			Prompt:    "benchmark",
		})
		if err != nil {
			b.Fatal(err)
		}

		// Complete it so the next iteration is not blocked by the
		// one-task-per-session invariant. This is part of the cost being
		// measured: a dispatch only happens when the previous task finished.
		b.StopTimer()
		if err := s.UpdateTask(ctx, task.ID, store.TaskUpdates{
			Status: stringPtr(TaskStatusCompleted),
		}); err != nil {
			b.Fatal(err)
		}
		b.StartTimer()
	}
}

// BenchmarkTaskDispatch_DispatchNext isolates the dispatch decision from task
// creation: this is what every redispatch trigger calls, so its cost is
// multiplied by the number of parked sessions in a pass.
func BenchmarkTaskDispatch_DispatchNext(b *testing.B) {
	s := benchStore("sess_bench", "run_bench")
	manager := NewTaskManager(s, &mockCommandSender{}, &mockSessionMgrForTask{}, nil, zap.NewNop())
	ctx := context.Background()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		taskID := fmt.Sprintf("task_%d", i)
		s.tasks[taskID] = &store.Task{
			ID: taskID, SessionID: "sess_bench", Status: TaskStatusPending,
			Prompt: "benchmark", CreatedAt: time.Now(),
		}
		b.StartTimer()

		if err := manager.DispatchNext(ctx, "sess_bench"); err != nil {
			b.Fatal(err)
		}

		b.StopTimer()
		delete(s.tasks, taskID)
		b.StartTimer()
	}
}

// BenchmarkTaskDispatch_NoWorkToDo is the case that dominates in production:
// a trigger fires, the session is idle, and nothing happens. It has to be cheap
// or the redispatch triggers are not free the way their design claims.
func BenchmarkTaskDispatch_NoWorkToDo(b *testing.B) {
	s := benchStore("sess_bench", "run_bench")
	manager := NewTaskManager(s, &mockCommandSender{}, &mockSessionMgrForTask{}, nil, zap.NewNop())
	ctx := context.Background()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := manager.DispatchNext(ctx, "sess_bench"); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkRedispatchPass_Empty measures one full wake over N idle sessions:
// the query shape a freed runner pays for when there is nothing parked. This is
// the number that decides whether firing a trigger from every suspend, detach,
// terminate and busy->idle transition is affordable.
func BenchmarkRedispatchPass_Empty(b *testing.B) {
	for _, sessions := range []int{1, 10, 50} {
		b.Run(fmt.Sprintf("sessions=%d", sessions), func(b *testing.B) {
			s := newTestTaskStore()
			runnerID := "run_bench"
			for i := 0; i < sessions; i++ {
				id := fmt.Sprintf("sess_%d", i)
				s.sessions[id] = &store.Session{
					ID: id, Status: SessionStatusActive, RunnerID: &runnerID,
				}
			}
			sessionMgr := &mockSessionMgrForTask{}
			tasks := NewTaskManager(s, &mockCommandSender{}, sessionMgr, nil, zap.NewNop())
			waker := NewDispatchWaker(s, sessionMgr, tasks, nil, nil, zap.NewNop())
			ctx := context.Background()

			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if err := waker.WakeAndWait(ctx, WakeTriggerRunnerFreed); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// BenchmarkPermission_RoundTrip is create-then-respond in process: the agent
// asks, a user approves, the answer goes back to the runner. It is on the
// interactive path, so latency here is latency a human feels.
func BenchmarkPermission_RoundTrip(b *testing.B) {
	s := newTestStore()
	ctx := context.Background()

	runnerID := "run_bench"
	if err := s.CreateSession(ctx, &store.Session{
		ID: "sess_bench", Status: SessionStatusActive, RunnerID: &runnerID,
	}); err != nil {
		b.Fatal(err)
	}
	if err := s.CreateTask(ctx, &store.Task{
		ID: "task_bench", SessionID: "sess_bench", Status: TaskStatusRunning,
	}); err != nil {
		b.Fatal(err)
	}
	if err := s.CreateTaskRun(ctx, &store.TaskRun{
		ID: "trun_bench", TaskID: "task_bench", Attempt: 1, Status: TaskRunStatusRunning,
	}); err != nil {
		b.Fatal(err)
	}

	manager := NewPermissionManager(
		s, &mockCommandSenderForPerm{}, &mockSessionMgrForPerm{}, nil, zap.NewNop())

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		req, err := manager.Create(ctx, &CreatePermissionRequestInput{
			OriginalRequestID: fmt.Sprintf("tool_use_%d", i),
			SessionID:         "sess_bench",
			TaskID:            "task_bench",
			RunID:             "trun_bench",
			Tool:              "bash",
			Action:            "ls -la",
			RiskLevel:         "low",
		})
		if err != nil {
			b.Fatal(err)
		}
		if err := manager.Respond(ctx, req.ID, true, "", "bench"); err != nil {
			b.Fatal(err)
		}
	}
}
