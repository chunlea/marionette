// Package claude implements the Claude Code executor.
package claude

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"

	"github.com/chunlea/marionette/pkg/agent/executor"
	"github.com/creack/pty"
	"go.uber.org/zap"
)

// Common errors for Claude executor.
var (
	ErrAlreadyRunning = errors.New("executor is already running a task")
	ErrNotRunning     = errors.New("executor is not running a task")
	ErrKilled         = errors.New("task was killed")
)

// Executor implements the executor.Executor interface for Claude Code.
type Executor struct {
	logger *zap.Logger

	// Command path override for testing
	commandPath string

	// Process state
	mu      sync.Mutex
	cmd     *exec.Cmd
	pty     *os.File
	running bool
	killed  bool

	// For graceful shutdown
	done chan struct{}
}

// New creates a new Claude Code executor.
func New(logger *zap.Logger) *Executor {
	return &Executor{
		logger: logger.Named("claude"),
	}
}

// WithCommandPath sets a custom command path (for testing).
func (e *Executor) WithCommandPath(path string) *Executor {
	e.commandPath = path
	return e
}

// Name returns the executor name.
func (e *Executor) Name() string {
	return "claude"
}

// Execute runs Claude Code with the given task.
func (e *Executor) Execute(ctx context.Context, task *executor.Task, config *executor.AgentConfig, handler executor.OutputHandler) (*executor.Result, error) {
	e.mu.Lock()
	if e.running {
		e.mu.Unlock()
		return nil, ErrAlreadyRunning
	}
	e.running = true
	e.killed = false
	e.done = make(chan struct{})
	e.mu.Unlock()

	defer func() {
		e.mu.Lock()
		e.running = false
		close(e.done)
		e.mu.Unlock()
	}()

	result := &Result{
		StartedAt: time.Now(),
	}

	// Build the command
	cmd, err := e.buildCommand(task, config)
	if err != nil {
		return e.failResult(result, fmt.Errorf("building command: %w", err)), nil
	}

	e.mu.Lock()
	e.cmd = cmd
	e.mu.Unlock()

	// Start with PTY
	ptmx, err := pty.Start(cmd)
	if err != nil {
		return e.failResult(result, fmt.Errorf("starting pty: %w", err)), nil
	}

	e.mu.Lock()
	e.pty = ptmx
	e.mu.Unlock()

	defer func() {
		_ = ptmx.Close()
		e.mu.Lock()
		e.pty = nil
		e.cmd = nil
		e.mu.Unlock()
	}()

	e.logger.Info("started claude code",
		zap.String("task_id", task.ID),
		zap.String("run_id", task.RunID),
		zap.String("working_dir", config.WorkingDir),
	)

	// Send prompt to stdin
	if task.Prompt != "" {
		if _, err := ptmx.WriteString(task.Prompt + "\n"); err != nil {
			e.logger.Warn("failed to write prompt", zap.Error(err))
		}
	}

	// Create context with timeout if specified
	execCtx := ctx
	var cancel context.CancelFunc
	if task.Timeout > 0 {
		execCtx, cancel = context.WithTimeout(ctx, task.Timeout)
		defer cancel()
	}

	// Read output in a goroutine
	outputDone := make(chan error, 1)
	go func() {
		outputDone <- e.readOutput(ptmx, handler)
	}()

	// Wait for process completion or context cancellation
	waitDone := make(chan error, 1)
	go func() {
		waitDone <- cmd.Wait()
	}()

	var waitErr error
	select {
	case <-execCtx.Done():
		// Context cancelled or timed out
		e.logger.Info("context done, killing process",
			zap.String("task_id", task.ID),
			zap.Error(execCtx.Err()),
		)
		_ = e.Kill()
		waitErr = <-waitDone
		if errors.Is(execCtx.Err(), context.DeadlineExceeded) {
			result.TimedOut = true
		}
	case waitErr = <-waitDone:
		// Process completed normally
	}

	// Wait for output reader to finish
	<-outputDone

	// Build result
	result.CompletedAt = time.Now()

	e.mu.Lock()
	killed := e.killed
	e.mu.Unlock()

	switch {
	case killed:
		result.ExitCode = -1
		result.Error = ErrKilled.Error()
		result.Success = false
	case waitErr != nil:
		var exitErr *exec.ExitError
		if errors.As(waitErr, &exitErr) {
			result.ExitCode = exitErr.ExitCode()
			result.Success = result.ExitCode == 0
			if !result.Success {
				result.Error = fmt.Sprintf("process exited with code %d", result.ExitCode)
			}
		} else {
			result.ExitCode = -1
			result.Error = waitErr.Error()
			result.Success = false
		}
	default:
		result.ExitCode = 0
		result.Success = true
	}

	e.logger.Info("claude code completed",
		zap.String("task_id", task.ID),
		zap.Bool("success", result.Success),
		zap.Int("exit_code", result.ExitCode),
		zap.Duration("duration", result.CompletedAt.Sub(result.StartedAt)),
	)

	return result.ToExecutorResult(), nil
}

// Kill terminates the running process.
func (e *Executor) Kill() error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if !e.running || e.cmd == nil || e.cmd.Process == nil {
		return nil
	}

	e.killed = true

	// Try graceful shutdown first with SIGTERM
	e.logger.Debug("sending SIGTERM to process")
	if err := e.cmd.Process.Signal(syscall.SIGTERM); err != nil {
		e.logger.Warn("failed to send SIGTERM", zap.Error(err))
	}

	// Give process time to exit gracefully
	go func() {
		select {
		case <-time.After(5 * time.Second):
			e.mu.Lock()
			defer e.mu.Unlock()
			if e.cmd != nil && e.cmd.Process != nil {
				e.logger.Debug("sending SIGKILL to process")
				_ = e.cmd.Process.Kill()
			}
		case <-e.done:
			// Process exited
		}
	}()

	return nil
}

// buildCommand creates the exec.Cmd for running Claude Code.
func (e *Executor) buildCommand(task *executor.Task, config *executor.AgentConfig) (*exec.Cmd, error) {
	// Use custom command path if set (for testing), otherwise find claude binary
	claudePath := e.commandPath
	if claudePath == "" {
		var err error
		claudePath, err = exec.LookPath("claude")
		if err != nil {
			return nil, fmt.Errorf("claude not found in PATH: %w", err)
		}
	}

	// Build arguments
	args := []string{}

	// Add model if specified
	if config.Model != "" {
		args = append(args, "--model", config.Model)
	}

	// Add print mode for non-interactive execution
	args = append(args, "--print")

	// Add the prompt as the last argument
	if task.Prompt != "" {
		args = append(args, task.Prompt)
	}

	cmd := exec.Command(claudePath, args...) //nolint:gosec // claudePath is from LookPath, args are controlled

	// Set working directory
	if config.WorkingDir != "" {
		cmd.Dir = config.WorkingDir
	}

	// Set environment
	cmd.Env = os.Environ()

	// Set API key
	if config.APIKey != "" {
		cmd.Env = append(cmd.Env, "ANTHROPIC_API_KEY="+config.APIKey)
	}

	// Set base URL if specified
	if config.BaseURL != "" {
		cmd.Env = append(cmd.Env, "ANTHROPIC_BASE_URL="+config.BaseURL)
	}

	// Add extra environment variables
	for k, v := range config.Extra {
		cmd.Env = append(cmd.Env, k+"="+v)
	}

	return cmd, nil
}

// readOutput reads from the PTY and sends to the handler.
func (e *Executor) readOutput(r io.Reader, handler executor.OutputHandler) error {
	scanner := bufio.NewScanner(r)
	// Increase buffer size for long lines
	buf := make([]byte, 64*1024)
	scanner.Buffer(buf, 1024*1024)

	for scanner.Scan() {
		line := scanner.Bytes()
		// PTY combines stdout and stderr, so we use "stdout"
		handler.HandleOutput("stdout", append(line, '\n'))
	}

	if err := scanner.Err(); err != nil {
		// EOF is expected when process exits
		if !errors.Is(err, io.EOF) && !errors.Is(err, os.ErrClosed) {
			return err
		}
	}

	return nil
}

// failResult creates a failed result.
func (e *Executor) failResult(r *Result, err error) *executor.Result {
	r.CompletedAt = time.Now()
	r.ExitCode = -1
	r.Error = err.Error()
	r.Success = false
	return r.ToExecutorResult()
}

// Result extends executor.Result with internal timing info.
type Result struct {
	executor.Result
	StartedAt time.Time
	TimedOut  bool
}

// ToExecutorResult converts to executor.Result.
func (r *Result) ToExecutorResult() *executor.Result {
	return &executor.Result{
		Success:      r.Success,
		ExitCode:     r.ExitCode,
		Error:        r.Error,
		TokensInput:  r.TokensInput,
		TokensOutput: r.TokensOutput,
		Context:      r.Context,
		CompletedAt:  r.CompletedAt,
	}
}

// Ensure Executor implements the interface.
var _ executor.Executor = (*Executor)(nil)
