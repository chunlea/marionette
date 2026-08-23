// Command loadtest drives a running marionette server with N fake pool runners
// and reports what the scheduler did under load.
//
// It is deliberately not a mock: the server, the database, the gRPC transport,
// the control channel, the log stream and the store are all real. Only the
// coding agent is scripted, by test/perf/fakeexec, so a run costs nothing in
// model tokens and does not need a Claude CLI on the load generator.
//
// What it reports:
//
//	dispatch latency  create -> the run row exists and is assigned to a runner.
//	                  This is the scheduler's own latency and the number the
//	                  performance baseline tracks.
//	completion        every task must reach a terminal state. A task that is
//	                  still pending at the deadline is a LOST task and fails
//	                  the run - that is the failure mode the whole redispatch
//	                  mechanism exists to prevent.
//	execution count   what the runners actually ran, checked against what the
//	                  server says completed. A mismatch means a task was
//	                  dispatched twice, which is the expensive bug.
//
// See scripts/loadtest.sh for the topology it expects, and test/perf/BASELINE.md
// for recorded numbers.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"sort"
	"sync"
	"syscall"
	"time"

	"github.com/chunlea/marionette/pkg/client"
	"go.uber.org/zap"
)

type options struct {
	apiURL      string
	grpcAddr    string
	apiKey      string
	runnerToken string
	pool        string

	runners  int
	sessions int
	tasks    int

	logLines  int
	taskMS    int
	deadline  time.Duration
	workspace string
	keep      bool
	verbose   bool
}

func main() {
	opts := parseFlags()

	logger := newLogger(opts.verbose)
	defer func() { _ = logger.Sync() }()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigC := make(chan os.Signal, 1)
	signal.Notify(sigC, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigC
		logger.Warn("interrupted; tearing down")
		cancel()
	}()

	if err := run(ctx, opts, logger); err != nil {
		fmt.Fprintf(os.Stderr, "\nLOAD TEST FAILED: %v\n", err)
		os.Exit(1)
	}
}

func parseFlags() options {
	var opts options
	flag.StringVar(&opts.apiURL, "api", envOr("MARIONETTE_API_URL", "http://localhost:8080"),
		"public API base URL")
	flag.StringVar(&opts.grpcAddr, "grpc", envOr("MARIONETTE_SERVER", "localhost:9090"),
		"gRPC address runners connect to")
	flag.StringVar(&opts.apiKey, "api-key", os.Getenv("MARIONETTE_API_KEY"),
		"API key for the public API")
	flag.StringVar(&opts.runnerToken, "runner-token", os.Getenv("MARIONETTE_RUNNER_TOKEN"),
		"runner registration token")
	flag.StringVar(&opts.pool, "pool", "default", "pool the fake runners join")

	flag.IntVar(&opts.runners, "runners", 50, "number of fake runners")
	flag.IntVar(&opts.sessions, "sessions", 50, "number of concurrent sessions")
	flag.IntVar(&opts.tasks, "tasks", 200, "total tasks, spread over the sessions")

	flag.IntVar(&opts.logLines, "log-lines", 20, "log lines each fake task emits")
	flag.IntVar(&opts.taskMS, "task-ms", 200, "how long a fake task takes, in milliseconds")
	flag.DurationVar(&opts.deadline, "deadline", 10*time.Minute,
		"how long to wait for every task to reach a terminal state")
	flag.StringVar(&opts.workspace, "workspace", "", "workspace root (default: a temp dir)")
	flag.BoolVar(&opts.keep, "keep", false, "keep workspaces and sessions after the run")
	flag.BoolVar(&opts.verbose, "v", false, "verbose runner logging")
	flag.Parse()

	return opts
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func newLogger(verbose bool) *zap.Logger {
	cfg := zap.NewProductionConfig()
	cfg.Encoding = "console"
	if !verbose {
		// 50 runners at info level bury the report they exist to produce.
		cfg.Level = zap.NewAtomicLevelAt(zap.ErrorLevel)
	}
	logger, err := cfg.Build()
	if err != nil {
		return zap.NewNop()
	}
	return logger
}

func run(ctx context.Context, opts options, logger *zap.Logger) error {
	if opts.apiKey == "" {
		return errors.New("no API key: pass -api-key or set MARIONETTE_API_KEY")
	}
	if opts.runnerToken == "" {
		return errors.New("no runner token: pass -runner-token or set MARIONETTE_RUNNER_TOKEN")
	}
	if opts.sessions <= 0 || opts.tasks <= 0 || opts.runners <= 0 {
		return errors.New("runners, sessions and tasks must all be positive")
	}

	workspaceRoot, cleanupWorkspaces, err := workspaceRoot(opts)
	if err != nil {
		return err
	}
	defer cleanupWorkspaces()

	api := client.NewHTTPClient(opts.apiURL, opts.apiKey)
	h := &harness{
		opts:      opts,
		api:       api,
		raw:       &rawAPI{baseURL: opts.apiURL, apiKey: opts.apiKey, client: &http.Client{Timeout: 30 * time.Second}},
		logger:    logger,
		workspace: workspaceRoot,
	}

	banner(opts, workspaceRoot)

	if err := h.startRunners(ctx); err != nil {
		return err
	}
	defer h.stopRunners()

	if err := h.waitForRegistration(ctx); err != nil {
		return err
	}

	if err := h.createSessions(ctx); err != nil {
		return err
	}
	defer h.terminateSessions()

	if err := h.createTasks(ctx); err != nil {
		return err
	}

	report, err := h.awaitCompletion(ctx)
	if err != nil {
		report.print()
		return err
	}

	if err := h.measureDispatch(ctx, report); err != nil {
		return err
	}
	report.print()

	return report.verdict(opts)
}

func workspaceRoot(opts options) (string, func(), error) {
	if opts.workspace != "" {
		if err := os.MkdirAll(opts.workspace, 0o750); err != nil {
			return "", nil, err
		}
		return opts.workspace, func() {}, nil
	}

	dir, err := os.MkdirTemp("", "marionette-loadtest-")
	if err != nil {
		return "", nil, err
	}
	cleanup := func() {
		if opts.keep {
			fmt.Printf("\nworkspaces kept at %s\n", dir)
			return
		}
		// 50 runners x one workspace per session is the whole reason this is
		// not left to the operating system's temp reaper.
		if err := os.RemoveAll(dir); err != nil {
			fmt.Fprintf(os.Stderr, "warning: could not remove %s: %v\n", dir, err)
		}
	}
	return dir, cleanup, nil
}

func banner(opts options, workspace string) {
	fmt.Printf(`
================================================================================
marionette load test - FAKE EXECUTOR, no model tokens are spent
================================================================================
  api          %s
  grpc         %s
  runners      %d  (pool %q)
  sessions     %d
  tasks        %d  (%.1f per session)
  task shape   %d log lines, ~%dms each
  workspaces   %s
================================================================================

`,
		opts.apiURL, opts.grpcAddr, opts.runners, opts.pool,
		opts.sessions, opts.tasks, float64(opts.tasks)/float64(opts.sessions),
		opts.logLines, opts.taskMS, workspace)
}

// percentile returns the p'th percentile of a sorted slice, nearest-rank.
func percentile(sorted []time.Duration, p float64) time.Duration {
	if len(sorted) == 0 {
		return 0
	}
	idx := int(float64(len(sorted))*p/100.0+0.5) - 1
	if idx < 0 {
		idx = 0
	}
	if idx >= len(sorted) {
		idx = len(sorted) - 1
	}
	return sorted[idx]
}

// rawAPI reaches the endpoints the Go SDK does not wrap.
type rawAPI struct {
	baseURL string
	apiKey  string
	client  *http.Client
}

type taskRun struct {
	ID         string     `json:"id"`
	Status     string     `json:"status"`
	RunnerID   *string    `json:"runner_id,omitempty"`
	QueuedAt   time.Time  `json:"queued_at"`
	AssignedAt *time.Time `json:"assigned_at,omitempty"`
	StartedAt  *time.Time `json:"started_at,omitempty"`
	EndedAt    *time.Time `json:"ended_at,omitempty"`
}

// listRuns fetches a task's runs. The SDK has no wrapper for this route, and
// the run row is the only place the dispatch timestamp lives.
func (r *rawAPI) listRuns(ctx context.Context, taskID string) ([]taskRun, error) {
	url := fmt.Sprintf("%s/api/v1/tasks/%s/runs", r.baseURL, taskID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+r.apiKey)

	resp, err := r.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("listing runs for %s: HTTP %d", taskID, resp.StatusCode)
	}

	var body struct {
		Items []taskRun `json:"items"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, err
	}
	return body.Items, nil
}

// sortDurations sorts in place and returns the slice, so callers can inline it.
func sortDurations(d []time.Duration) []time.Duration {
	sort.Slice(d, func(i, j int) bool { return d[i] < d[j] })
	return d
}

// mutexed is a tiny helper for the concurrent collection phases.
type mutexed[T any] struct {
	mu    sync.Mutex
	items []T
}

func (m *mutexed[T]) add(item T) {
	m.mu.Lock()
	m.items = append(m.items, item)
	m.mu.Unlock()
}

func (m *mutexed[T]) all() []T {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]T(nil), m.items...)
}
