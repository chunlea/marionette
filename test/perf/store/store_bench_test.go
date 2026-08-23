// Package store_test benchmarks the store queries that sit on a hot path.
//
// They run against real PostgreSQL in a container, because that is the only
// thing that measures what actually costs time: index choice, row level
// security wrapping every statement in a transaction, and the round trips a
// query shape implies. An in-memory fake would report the speed of a Go map.
//
// Three queries are covered, chosen because every other path leans on them:
//
//	session get        - every dispatch, every permission, every log write
//	pending-task scan  - every dispatch decision, and every redispatch trigger
//	                     pays it once per session in a pass
//	log insert         - the highest-volume write in the system by orders of
//	                     magnitude; one agent produces thousands per task
//
// Recorded numbers and the machine that produced them are in
// test/perf/BASELINE.md. There is no regression gate: the point is to know the
// shape of the curve before optimising anything.
package store_test

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
	"go.uber.org/zap"

	"github.com/chunlea/marionette/pkg/store"
	pgstore "github.com/chunlea/marionette/pkg/store/postgres"
)

var benchStore *pgstore.Store

const noDockerBanner = `
################################################################################
# test/perf/store SKIPPED - no Docker daemon reachable.
#
# These benchmarks measure real SQL against real PostgreSQL. There is no
# meaningful fallback: an in-memory store would report the speed of a Go map.
#
# To run them:  make bench-store
################################################################################
`

// dockerHealth reports whether a Docker daemon is reachable.
//
// testcontainers panics rather than returning an error when there is no Docker
// host at all, so the probe has to recover: that panic is exactly the case this
// function exists to detect.
func dockerHealth(ctx context.Context) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("no docker host: %v", r)
		}
	}()

	provider, err := testcontainers.NewDockerProvider()
	if err != nil {
		return err
	}
	defer func() { _ = provider.Close() }()

	return provider.Health(ctx)
}

// TestMain skips rather than fails without Docker, which is the opposite of
// pkg/store/postgres. That package is the only coverage real SQL has, so a
// silent skip there hides missing tests; these are a measurement, and a
// measurement that cannot run is not a broken build.
func TestMain(m *testing.M) {
	ctx := context.Background()

	// Starting a PostgreSQL container for a plain `go test ./...` would make
	// the whole suite pay for benchmarks nobody asked to run.
	if bench := flag.Lookup("test.bench"); bench == nil || bench.Value.String() == "" {
		os.Exit(m.Run())
	}

	if err := dockerHealth(ctx); err != nil {
		fmt.Fprintf(os.Stderr, noDockerBanner)
		fmt.Fprintf(os.Stderr, "Reason: %v\n", err)
		os.Exit(0)
	}

	container, err := postgres.Run(ctx,
		"postgres:15-alpine",
		postgres.WithDatabase("marionette_bench"),
		postgres.WithUsername("bench"),
		postgres.WithPassword("bench"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(60*time.Second),
		),
	)
	if err != nil {
		panic("failed to start postgres container: " + err.Error())
	}

	connStr, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		panic("failed to get connection string: " + err.Error())
	}

	benchStore, err = pgstore.New(ctx, pgstore.Config{URL: connStr}, zap.NewNop())
	if err != nil {
		panic("failed to create store: " + err.Error())
	}
	if err := runMigrations(ctx, benchStore); err != nil {
		panic("failed to run migrations: " + err.Error())
	}

	code := m.Run()

	_ = benchStore.Close()
	_ = container.Terminate(ctx)
	os.Exit(code)
}

func runMigrations(ctx context.Context, s *pgstore.Store) error {
	files, err := filepath.Glob("../../../migrations/*.up.sql")
	if err != nil {
		return err
	}
	if len(files) == 0 {
		return errors.New("no migrations found in ../../../migrations")
	}
	// Numeric prefixes are zero-padded, so lexical order is migration order.
	sort.Strings(files)

	for _, file := range files {
		migration, err := os.ReadFile(file) //nolint:gosec // fixed repo path
		if err != nil {
			return err
		}
		if err := s.ExecRaw(ctx, string(migration)); err != nil {
			return err
		}
	}
	return nil
}

// seed creates one workspace, one session, one runner and one task run, and
// returns the session id everything hangs off.
func seed(b *testing.B, suffix string) (sessionID, taskID, runID, runnerID string) {
	b.Helper()
	ctx := context.Background()
	now := time.Now()

	workspaceID := "ws_bench_" + suffix
	sessionID = "sess_bench_" + suffix
	taskID = "task_bench_" + suffix
	runID = "trun_bench_" + suffix
	runnerID = "run_bench_" + suffix

	if err := benchStore.CreateWorkspace(ctx, &store.Workspace{
		ID: workspaceID, Name: "bench-" + suffix, CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		b.Fatal(err)
	}
	if err := benchStore.CreateRunner(ctx, &store.Runner{
		ID: runnerID, Name: "bench-" + suffix, Hostname: "bench-host",
		SandboxMode: "runner-is-sandbox", Status: "idle",
		CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		b.Fatal(err)
	}
	if err := benchStore.CreateSession(ctx, &store.Session{
		ID: sessionID, WorkspaceID: workspaceID, Agent: "claude", Status: "active",
		RunnerID: &runnerID, CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		b.Fatal(err)
	}
	if err := benchStore.CreateTask(ctx, &store.Task{
		ID: taskID, SessionID: sessionID, Prompt: "bench", Status: "running",
		CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		b.Fatal(err)
	}
	if err := benchStore.CreateTaskRun(ctx, &store.TaskRun{
		ID: runID, TaskID: taskID, Attempt: 1, Status: "running",
		RunnerID: &runnerID, QueuedAt: now, UpdatedAt: now,
	}); err != nil {
		b.Fatal(err)
	}
	return sessionID, taskID, runID, runnerID
}

// BenchmarkStore_GetSession is the single most-called read in the system: every
// dispatch, every permission decision and every attach starts with it.
func BenchmarkStore_GetSession(b *testing.B) {
	sessionID, _, _, _ := seed(b, "get")
	ctx := context.Background()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := benchStore.GetSession(ctx, sessionID); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkStore_PendingTaskScan is what a dispatch decision costs, and a
// redispatch pass pays it once per session it looks at. The backlog sizes are
// the range a real session sees: sequential execution keeps it small, and the
// interesting question is whether the query degrades when it is not.
func BenchmarkStore_PendingTaskScan(b *testing.B) {
	for _, backlog := range []int{1, 10, 100} {
		b.Run(fmt.Sprintf("backlog=%d", backlog), func(b *testing.B) {
			sessionID, _, _, _ := seed(b, fmt.Sprintf("scan%d", backlog))
			ctx := context.Background()
			now := time.Now()

			for i := 0; i < backlog; i++ {
				if err := benchStore.CreateTask(ctx, &store.Task{
					ID:        fmt.Sprintf("task_scan%d_%d", backlog, i),
					SessionID: sessionID, Prompt: "bench", Status: "pending",
					CreatedAt: now, UpdatedAt: now,
				}); err != nil {
					b.Fatal(err)
				}
			}

			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_, err := benchStore.ListTasks(ctx, store.ListTasksOptions{
					BaseListOptions: store.BaseListOptions{Limit: 200},
					SessionID:       &sessionID,
					Status:          []string{"pending", "running"},
				})
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// BenchmarkStore_CreateLog is the highest-volume write in the system: one agent
// task produces thousands of these, and they land in a partitioned table.
func BenchmarkStore_CreateLog(b *testing.B) {
	sessionID, taskID, runID, runnerID := seed(b, "log1")
	ctx := context.Background()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := benchStore.CreateLog(ctx, &store.Log{
			ID:        fmt.Sprintf("log_bench1_%d", i),
			SessionID: sessionID, TaskID: taskID, RunID: runID, RunnerID: runnerID,
			Stream: "stdout", Level: "info", Content: "benchmark log line",
			Sequence: int64(i), CreatedAt: time.Now(),
		}); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkStore_CreateLogs is the batched form the log streamer actually uses.
// The per-line cost is what matters: if batching does not beat CreateLog by a
// wide margin, the streamer's buffering is not earning its complexity.
func BenchmarkStore_CreateLogs(b *testing.B) {
	for _, batch := range []int{10, 100} {
		b.Run(fmt.Sprintf("batch=%d", batch), func(b *testing.B) {
			sessionID, taskID, runID, runnerID := seed(b, fmt.Sprintf("logn%d", batch))
			ctx := context.Background()

			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				logs := make([]*store.Log, batch)
				for j := range logs {
					logs[j] = &store.Log{
						ID:        fmt.Sprintf("log_benchn%d_%d_%d", batch, i, j),
						SessionID: sessionID, TaskID: taskID, RunID: runID, RunnerID: runnerID,
						Stream: "stdout", Level: "info", Content: "benchmark log line",
						Sequence: int64(i*batch + j), CreatedAt: time.Now(),
					}
				}
				if err := benchStore.CreateLogs(ctx, logs); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
