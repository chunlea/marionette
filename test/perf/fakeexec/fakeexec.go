// Package fakeexec provides an agent executor that scripts its output instead
// of running a real coding agent.
//
// It exists so the load test can drive the real stack - real gRPC, real control
// channel, real TaskRunner, real log streaming, real store - without spending
// money on model tokens or depending on a Claude CLI being installed and logged
// in on the load generator.
//
// # Why this lives under test/ and not in pkg/agent
//
// The brief allowed a build-tagged executor inside pkg/agent. A build tag is a
// weaker guarantee than it looks: `go build -tags fakeexec ./cmd/agent` is one
// typo in a Makefile or a CI matrix away, and the resulting binary looks and
// starts exactly like the real one while silently completing every task without
// doing anything. It would take a production incident to notice.
//
// Living under test/ makes the mistake unrepresentable instead of unlikely.
// Nothing in cmd/ can import this package without that import being visible in
// review, and TestProductionBinariesDoNotLinkTheFakeExecutor in
// test/perf/loadtest asserts it against the real dependency graph. The load
// test binary constructs its own runner from the same exported pkg/agent
// pieces cmd/agent uses, so what is exercised is the same pipeline.
package fakeexec

import (
	"context"
	"fmt"
	"math/rand/v2"
	"strings"
	"sync"
	"time"

	"github.com/chunlea/marionette/pkg/agent/executor"
)

// Config tunes what one fake task execution looks like on the wire.
//
// The defaults describe a short, chatty task: the shape that stresses the log
// pipeline and the dispatch loop rather than the model.
type Config struct {
	// LogLines is how many output lines a task emits. Bounded on purpose:
	// 50 sessions x 200 tasks x an unbounded line count is how a load test
	// fills a disk with rows nobody will ever read.
	LogLines int

	// LineBytes is the length of each emitted line.
	LineBytes int

	// Duration is how long a task pretends to think, before jitter.
	Duration time.Duration

	// Jitter spreads Duration by +/- this fraction (0.2 = +/-20%), so 50
	// runners do not complete in lockstep and hand the server an artificially
	// synchronised burst.
	Jitter float64

	// TokensInput and TokensOutput are reported on completion, so the token
	// accounting path is exercised rather than skipped.
	TokensInput  int64
	TokensOutput int64

	// PermissionEvery makes every Nth task ask for permission before
	// finishing. Zero disables it. The load test leaves it off by default:
	// a pending permission blocks the task until something answers, which
	// measures the responder rather than the scheduler.
	PermissionEvery int
}

// DefaultConfig is a short, chatty task.
func DefaultConfig() Config {
	return Config{
		LogLines:     20,
		LineBytes:    120,
		Duration:     200 * time.Millisecond,
		Jitter:       0.2,
		TokensInput:  1200,
		TokensOutput: 340,
	}
}

// Executor is a scripted executor.Executor.
type Executor struct {
	cfg Config

	mu       sync.Mutex
	executed int
	killed   bool
	cancel   context.CancelFunc
}

// New creates a fake executor. Zero-valued Config fields take the defaults, so
// a caller can override one knob without restating the rest.
func New(cfg Config) *Executor {
	def := DefaultConfig()
	if cfg.LogLines <= 0 {
		cfg.LogLines = def.LogLines
	}
	if cfg.LineBytes <= 0 {
		cfg.LineBytes = def.LineBytes
	}
	if cfg.Duration <= 0 {
		cfg.Duration = def.Duration
	}
	if cfg.Jitter < 0 {
		cfg.Jitter = 0
	}
	return &Executor{cfg: cfg}
}

// Name implements executor.Executor.
func (e *Executor) Name() string { return "fake" }

// Executed reports how many tasks this executor has run.
func (e *Executor) Executed() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.executed
}

// Kill implements executor.Executor. It cancels the in-flight fake task, which
// is what a real Kill does to a subprocess.
func (e *Executor) Kill() error {
	e.mu.Lock()
	e.killed = true
	cancel := e.cancel
	e.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	return nil
}

// Execute implements executor.Executor.
//
// It walks the same handler contract a real executor does - output, a context
// update, optionally a permission request, then a result with token counts - so
// the TaskRunner, the log streamer and the server-side completion path all see
// exactly what they see in production. Only the subprocess is missing.
func (e *Executor) Execute(
	ctx context.Context,
	task *executor.Task,
	_ *executor.AgentConfig,
	handler executor.OutputHandler,
) (*executor.Result, error) {
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	e.mu.Lock()
	e.executed++
	n := e.executed
	e.killed = false
	e.cancel = cancel
	e.mu.Unlock()

	defer func() {
		e.mu.Lock()
		e.cancel = nil
		e.mu.Unlock()
	}()

	// A real agent reports its own session id early so the server can resume
	// the conversation later. Skipping it would leave the resume path
	// untested under load, which is where it is most likely to break.
	agentSession := fmt.Sprintf("fake-%s-%d", task.SessionID, n)
	handler.HandleContextUpdate(runCtx, agentSession, task.ID)

	line := strings.Repeat("x", e.cfg.LineBytes)
	perLine := e.cfg.Duration / time.Duration(e.cfg.LogLines)

	for i := 0; i < e.cfg.LogLines; i++ {
		if err := sleepCtx(runCtx, jitter(perLine, e.cfg.Jitter)); err != nil {
			return e.interrupted(ctx, agentSession)
		}
		handler.HandleOutput("stdout",
			[]byte(fmt.Sprintf("[fake %s %03d] %s\n", task.ID, i, line)))
	}

	if e.cfg.PermissionEvery > 0 && n%e.cfg.PermissionEvery == 0 {
		approved, err := handler.HandlePermissionRequest(runCtx, &executor.PermissionRequest{
			ID:        fmt.Sprintf("%s-perm-%d", task.ID, n),
			Tool:      "bash",
			Action:    "echo load-test",
			Context:   "fake executor permission probe",
			RiskLevel: executor.RiskLow,
		})
		if err != nil {
			return e.interrupted(ctx, agentSession)
		}
		if !approved {
			return &executor.Result{
				Success:      false,
				ExitCode:     1,
				Error:        "permission denied",
				TokensInput:  e.cfg.TokensInput,
				TokensOutput: e.cfg.TokensOutput,
				AgentSession: agentSession,
				CompletedAt:  time.Now(),
			}, nil
		}
	}

	return &executor.Result{
		Success:         true,
		ExitCode:        0,
		TokensInput:     e.cfg.TokensInput,
		TokensOutput:    e.cfg.TokensOutput,
		AgentSession:    agentSession,
		ContextSnapshot: snapshot(agentSession),
		CompletedAt:     time.Now(),
	}, nil
}

// interrupted is the result for a task that was killed or whose context ended.
// A killed task reports failure rather than an error, the same way a real
// executor reports a non-zero exit: the run ended, and the server needs to
// record that rather than retry a transport failure.
func (e *Executor) interrupted(ctx context.Context, agentSession string) (*executor.Result, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	return &executor.Result{
		Success:      false,
		ExitCode:     130,
		Error:        "killed",
		AgentSession: agentSession,
		CompletedAt:  time.Now(),
	}, nil
}

// snapshot is the minimal context a resume needs, in the shape the Claude
// executor writes it.
func snapshot(agentSession string) []byte {
	return []byte(fmt.Sprintf(`{"agent_session_id":%q}`, agentSession))
}

// jitter spreads d by +/- fraction.
func jitter(d time.Duration, fraction float64) time.Duration {
	if fraction <= 0 || d <= 0 {
		return d
	}
	spread := float64(d) * fraction
	return time.Duration(float64(d) - spread + rand.Float64()*2*spread) //nolint:gosec // pacing, not security
}

// sleepCtx waits for d or until ctx ends.
func sleepCtx(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	timer := time.NewTimer(d)
	defer timer.Stop()

	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

var _ executor.Executor = (*Executor)(nil)
