package main

import (
	"fmt"
	"sync"
	"time"
)

// stuckTask is a task that never reached a terminal state.
type stuckTask struct {
	ID        string
	SessionID string
	Status    string
}

// report is what a run produces. It is printed even when the run fails,
// because a failed run's numbers are the diagnosis.
type report struct {
	start   time.Time
	elapsed time.Duration

	total     int
	completed int
	failed    int
	canceled  int
	executed  int

	latencies        []time.Duration
	tasksWithoutRuns int
	stuck            []stuckTask

	mu           sync.Mutex
	totalRuns    int
	multiRunTask int
}

func (r *report) addAttempts(n int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.totalRuns += n
	if n > 1 {
		r.multiRunTask++
	}
}

func (r *report) tally(terminal map[string]string) {
	for _, status := range terminal {
		switch status {
		case "completed":
			r.completed++
		case "failed":
			r.failed++
		case "canceled":
			r.canceled++
		}
	}
}

func (r *report) print() {
	fmt.Printf(`
================================================================================
RESULTS
================================================================================
  wall clock        %s
  tasks             %d
    completed       %d
    failed          %d
    canceled        %d
    never terminal  %d
  runner executions %d
  task runs         %d  (%d task(s) needed more than one)
`,
		r.elapsed.Round(time.Millisecond),
		r.total, r.completed, r.failed, r.canceled,
		r.total-r.completed-r.failed-r.canceled,
		r.executed, r.totalRuns, r.multiRunTask)

	if r.elapsed > 0 && r.total > 0 {
		fmt.Printf("  throughput        %.1f tasks/s\n",
			float64(r.completed+r.failed)/r.elapsed.Seconds())
	}

	if len(r.latencies) > 0 {
		fmt.Printf(`
  dispatch latency (task created -> run queued on a runner)
    samples         %d
    min             %s
    p50             %s
    p95             %s
    p99             %s
    max             %s
`,
			len(r.latencies),
			r.latencies[0].Round(time.Millisecond),
			percentile(r.latencies, 50).Round(time.Millisecond),
			percentile(r.latencies, 95).Round(time.Millisecond),
			percentile(r.latencies, 99).Round(time.Millisecond),
			r.latencies[len(r.latencies)-1].Round(time.Millisecond),
		)
	}
	if r.tasksWithoutRuns > 0 {
		fmt.Printf("    %d task(s) had no run at all - they were never dispatched\n",
			r.tasksWithoutRuns)
	}

	if len(r.stuck) > 0 {
		fmt.Printf("\n  STUCK TASKS (first %d):\n", len(r.stuck))
		for _, task := range r.stuck {
			status := task.Status
			if status == "" {
				status = "<not found>"
			}
			fmt.Printf("    %s  session=%s  status=%s\n", task.ID, task.SessionID, status)
		}
	}
	fmt.Println("================================================================================")
}

// verdict decides whether the run met its targets.
//
// The bar is deliberately about correctness, not speed: latency is recorded as
// a baseline, but a lost task, a task dispatched twice, or a task that never got
// a run at all are the failures this exists to catch.
func (r *report) verdict(opts options) error {
	if unfinished := r.total - r.completed - r.failed - r.canceled; unfinished > 0 {
		return fmt.Errorf("%d task(s) never reached a terminal state", unfinished)
	}
	if r.total != opts.tasks {
		return fmt.Errorf("expected %d tasks, tracked %d", opts.tasks, r.total)
	}
	if r.failed > 0 || r.canceled > 0 {
		return fmt.Errorf("%d failed and %d canceled task(s); the fake executor always succeeds, so this is the stack",
			r.failed, r.canceled)
	}
	if r.tasksWithoutRuns > 0 {
		return fmt.Errorf("%d task(s) completed without a run row", r.tasksWithoutRuns)
	}
	// One execution per task, exactly. More means a task reached two runners,
	// which is the same prompt running twice against one workspace.
	if r.executed != r.completed {
		return fmt.Errorf(
			"runners executed %d task(s) but %d completed: a task was dispatched more than once",
			r.executed, r.completed)
	}
	if r.totalRuns != r.total {
		return fmt.Errorf("expected exactly one run per task, got %d runs for %d tasks",
			r.totalRuns, r.total)
	}
	return nil
}
