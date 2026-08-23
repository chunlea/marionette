package main

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/chunlea/marionette/pkg/client"
	"github.com/chunlea/marionette/test/perf/fakeexec"
	"go.uber.org/zap"
)

// harness owns the whole run: the fake runners, the sessions, the tasks, and
// the measurements taken off them.
type harness struct {
	opts      options
	api       *client.HTTPClient
	raw       *rawAPI
	logger    *zap.Logger
	workspace string

	runners  []*fakeRunner
	sessions []string
	tasks    []taskRecord
}

// taskRecord is one task and when the harness asked for it. CreatedAt comes
// from the server so the latency does not include the harness's own HTTP time.
type taskRecord struct {
	ID        string
	SessionID string
	CreatedAt time.Time
}

// startRunners brings up the fake pool. Runners connect concurrently: 50
// sequential registrations would take longer than the run they precede.
func (h *harness) startRunners(ctx context.Context) error {
	execCfg := fakeexec.Config{
		LogLines: h.opts.logLines,
		Duration: time.Duration(h.opts.taskMS) * time.Millisecond,
	}

	h.runners = make([]*fakeRunner, h.opts.runners)
	errs := make([]error, h.opts.runners)

	var wg sync.WaitGroup
	for i := 0; i < h.opts.runners; i++ {
		runner := newFakeRunner(
			fmt.Sprintf("loadtest-%03d", i),
			h.opts.grpcAddr,
			h.opts.runnerToken,
			h.opts.pool,
			runnerWorkspace(h.workspace, i),
			execCfg,
			h.logger,
		)
		h.runners[i] = runner

		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			errs[i] = runner.Start(ctx)
		}(i)
	}
	wg.Wait()

	started := 0
	for i, err := range errs {
		if err != nil {
			h.runners[i] = nil
			h.logger.Error("runner failed to start", zap.Error(err))
			continue
		}
		started++
	}
	if started == 0 {
		return fmt.Errorf("no runner could connect to %s: %w", h.opts.grpcAddr, errs[0])
	}
	if started < h.opts.runners {
		// Reported rather than tolerated silently: a short pool changes what
		// the dispatch latency means.
		fmt.Printf("WARNING: only %d/%d runners connected\n", started, h.opts.runners)
	}

	fmt.Printf("runners connected: %d\n", started)
	return nil
}

func (h *harness) stopRunners() {
	var wg sync.WaitGroup
	for _, runner := range h.runners {
		if runner == nil {
			continue
		}
		wg.Add(1)
		go func(r *fakeRunner) {
			defer wg.Done()
			r.Stop()
		}(runner)
	}
	wg.Wait()
}

// waitForRegistration blocks until the server can see the runners.
//
// Registration is an RPC that returns before the row is visible to the API, and
// creating a session against a pool the server does not know about yet parks it
// - which would then be measured as scheduler latency rather than as the setup
// race it is.
func (h *harness) waitForRegistration(ctx context.Context) error {
	want := 0
	for _, runner := range h.runners {
		if runner != nil && runner.RunnerID() != "" {
			want++
		}
	}
	if want == 0 {
		return errors.New("no runner reported an id after registering")
	}

	deadline := time.Now().Add(60 * time.Second)
	for {
		runners, err := h.api.ListRunners(ctx, client.ListRunnersOptions{
			Status: []string{"idle"},
		})
		if err == nil && len(runners.Items) >= want {
			fmt.Printf("runners registered and idle: %d\n\n", len(runners.Items))
			return nil
		}
		if time.Now().After(deadline) {
			have := 0
			if runners != nil {
				have = len(runners.Items)
			}
			return fmt.Errorf("only %d/%d runners became idle within 60s", have, want)
		}
		if err := sleep(ctx, 500*time.Millisecond); err != nil {
			return err
		}
	}
}

func (h *harness) createSessions(ctx context.Context) error {
	created := &mutexed[string]{}
	failures := &mutexed[error]{}

	// Concurrent on purpose: session creation allocates a runner, so doing it
	// in parallel is what puts the allocator under contention.
	var wg sync.WaitGroup
	sem := make(chan struct{}, 16)
	for i := 0; i < h.opts.sessions; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			session, err := h.api.CreateSession(ctx, client.CreateSessionOptions{
				Name:  fmt.Sprintf("loadtest-%03d", i),
				Agent: "claude",
			})
			if err != nil {
				failures.add(err)
				return
			}
			created.add(session.ID)
		}(i)
	}
	wg.Wait()

	h.sessions = created.all()
	if errs := failures.all(); len(errs) > 0 {
		return fmt.Errorf("%d/%d sessions failed to create: %w",
			len(errs), h.opts.sessions, errs[0])
	}

	fmt.Printf("sessions created: %d\n", len(h.sessions))
	return nil
}

// terminateSessions is the cleanup that matters: a terminated session releases
// its runner and drops its workspace, and 50 sessions left active would keep a
// development database and a temp directory populated after every run.
func (h *harness) terminateSessions() {
	if h.opts.keep {
		fmt.Printf("sessions kept: %d\n", len(h.sessions))
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	var wg sync.WaitGroup
	sem := make(chan struct{}, 16)
	failed := 0
	var mu sync.Mutex

	for _, sessionID := range h.sessions {
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			if err := h.api.TerminateSession(ctx, id); err != nil {
				mu.Lock()
				failed++
				mu.Unlock()
			}
		}(sessionID)
	}
	wg.Wait()

	if failed > 0 {
		fmt.Printf("WARNING: %d/%d sessions could not be terminated\n", failed, len(h.sessions))
	}
}

// createTasks spreads the task count over the sessions round-robin.
//
// A session executes its tasks sequentially, so several tasks per session is
// not just load: it is the dispatch chain - a task completing has to pull the
// next one - which is the path a lost task hides in.
func (h *harness) createTasks(ctx context.Context) error {
	if len(h.sessions) == 0 {
		return errors.New("no sessions to create tasks in")
	}

	created := &mutexed[taskRecord]{}
	failures := &mutexed[error]{}

	var wg sync.WaitGroup
	sem := make(chan struct{}, 32)
	for i := 0; i < h.opts.tasks; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			sessionID := h.sessions[i%len(h.sessions)]
			task, err := h.api.CreateTask(ctx, client.CreateTaskOptions{
				SessionID:      sessionID,
				Prompt:         fmt.Sprintf("load test task %d", i),
				TimeoutSeconds: 300,
			})
			if err != nil {
				failures.add(err)
				return
			}
			created.add(taskRecord{
				ID: task.ID, SessionID: sessionID, CreatedAt: task.CreatedAt,
			})
		}(i)
	}
	wg.Wait()

	h.tasks = created.all()
	if errs := failures.all(); len(errs) > 0 {
		return fmt.Errorf("%d/%d tasks failed to create: %w", len(errs), h.opts.tasks, errs[0])
	}

	fmt.Printf("tasks created: %d\n\n", len(h.tasks))
	return nil
}

// awaitCompletion polls until every task is terminal or the deadline passes.
//
// It polls per session rather than per task: one ListTasks answers for a whole
// session's backlog, so the harness adds 50 requests per round instead of 200
// and stops being a meaningful share of the load it is trying to measure.
func (h *harness) awaitCompletion(ctx context.Context) (*report, error) {
	rep := &report{start: time.Now(), total: len(h.tasks)}
	terminal := map[string]string{}
	deadline := time.Now().Add(h.opts.deadline)

	for {
		statuses, err := h.pollStatuses(ctx)
		if err != nil {
			return rep, err
		}
		for id, status := range statuses {
			switch status {
			case "completed", "failed", "canceled":
				terminal[id] = status
			}
		}

		if len(terminal) >= len(h.tasks) {
			break
		}
		if time.Now().After(deadline) {
			rep.elapsed = time.Since(rep.start)
			rep.stuck = h.stuckTasks(statuses)
			rep.tally(terminal)
			return rep, fmt.Errorf(
				"%d/%d tasks never reached a terminal state within %s",
				len(h.tasks)-len(terminal), len(h.tasks), h.opts.deadline)
		}

		fmt.Printf("\r  terminal: %d/%d", len(terminal), len(h.tasks))
		if err := sleep(ctx, 500*time.Millisecond); err != nil {
			return rep, err
		}
	}

	fmt.Printf("\r  terminal: %d/%d\n\n", len(terminal), len(h.tasks))
	rep.elapsed = time.Since(rep.start)
	rep.tally(terminal)

	for _, runner := range h.runners {
		if runner != nil {
			rep.executed += runner.Executed()
		}
	}
	return rep, nil
}

// pollStatuses reads the current status of every task, one query per session.
func (h *harness) pollStatuses(ctx context.Context) (map[string]string, error) {
	statuses := make(map[string]string, len(h.tasks))
	var mu sync.Mutex
	var firstErr error

	var wg sync.WaitGroup
	sem := make(chan struct{}, 16)
	for _, sessionID := range h.sessions {
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			result, err := h.api.ListTasks(ctx, client.ListTasksOptions{
				SessionID: id,
				Limit:     200,
			})
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				if firstErr == nil {
					firstErr = err
				}
				return
			}
			for _, task := range result.Items {
				statuses[task.ID] = task.Status
			}
		}(sessionID)
	}
	wg.Wait()

	return statuses, firstErr
}

// stuckTasks lists what never moved, with the status it was left in. A lost
// task is the failure this whole subsystem exists to prevent, so the report
// names them rather than counting them.
func (h *harness) stuckTasks(statuses map[string]string) []stuckTask {
	var stuck []stuckTask
	for _, task := range h.tasks {
		status := statuses[task.ID]
		switch status {
		case "completed", "failed", "canceled":
			continue
		}
		stuck = append(stuck, stuckTask{
			ID: task.ID, SessionID: task.SessionID, Status: status,
		})
		if len(stuck) >= 20 {
			break
		}
	}
	return stuck
}

// measureDispatch reads each task's runs and records create -> assigned.
//
// The earliest run is the one that counts: a task that was parked and later
// redispatched has more than one, and the last one would report the redispatch
// delay rather than the scheduler's.
func (h *harness) measureDispatch(ctx context.Context, rep *report) error {
	latencies := &mutexed[time.Duration]{}
	noRun := &mutexed[string]{}

	var wg sync.WaitGroup
	sem := make(chan struct{}, 16)
	for _, task := range h.tasks {
		wg.Add(1)
		go func(task taskRecord) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			runs, err := h.raw.listRuns(ctx, task.ID)
			if err != nil || len(runs) == 0 {
				noRun.add(task.ID)
				return
			}
			earliest := runs[0]
			for _, run := range runs[1:] {
				if run.QueuedAt.Before(earliest.QueuedAt) {
					earliest = run
				}
			}
			rep.addAttempts(len(runs))
			latencies.add(earliest.QueuedAt.Sub(task.CreatedAt))
		}(task)
	}
	wg.Wait()

	rep.latencies = sortDurations(latencies.all())
	rep.tasksWithoutRuns = len(noRun.all())
	return nil
}

func sleep(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
