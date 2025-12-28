// Package claude implements the Claude Code executor.
package claude

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"

	"github.com/chunlea/marionette/pkg/agent/executor"
	"go.uber.org/zap"
)

// Common errors for Claude executor.
var (
	ErrAlreadyRunning  = errors.New("executor is already running a task")
	ErrNotRunning      = errors.New("executor is not running a task")
	ErrNotStreamMode   = errors.New("executor is not in stream input mode")
	ErrKilled          = errors.New("task was killed")
	ErrStdinNotReady   = errors.New("stdin writer not ready")
)

// Executor implements the executor.Executor interface for Claude Code.
// It also implements executor.StreamExecutor for bidirectional communication.
type Executor struct {
	logger *zap.Logger

	// Command path override for testing
	commandPath string

	// Process state
	mu      sync.Mutex
	cmd     *exec.Cmd
	running bool
	killed  bool

	// Stream input mode
	streamMode  bool
	stdinWriter io.WriteCloser

	// Session tracking
	sessionID string

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
		e.streamMode = false
		if e.stdinWriter != nil {
			_ = e.stdinWriter.Close()
			e.stdinWriter = nil
		}
		close(e.done)
		e.mu.Unlock()
	}()

	result := &Result{
		StartedAt: time.Now(),
	}

	// Build the command (returns whether to use stream input mode)
	cmd, useStreamInput, err := e.buildCommand(task, config)
	if err != nil {
		return e.failResult(result, fmt.Errorf("building command: %w", err)), nil
	}

	// Get stdout and stderr pipes
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return e.failResult(result, fmt.Errorf("getting stdout pipe: %w", err)), nil
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		return e.failResult(result, fmt.Errorf("getting stderr pipe: %w", err)), nil
	}

	// Get stdin pipe for stream input mode
	var stdin io.WriteCloser
	if useStreamInput {
		stdin, err = cmd.StdinPipe()
		if err != nil {
			return e.failResult(result, fmt.Errorf("getting stdin pipe: %w", err)), nil
		}
	}

	e.mu.Lock()
	e.cmd = cmd
	e.streamMode = useStreamInput
	e.stdinWriter = stdin
	e.mu.Unlock()

	// Start the process
	if err := cmd.Start(); err != nil {
		return e.failResult(result, fmt.Errorf("starting process: %w", err)), nil
	}

	defer func() {
		e.mu.Lock()
		e.cmd = nil
		e.mu.Unlock()
	}()

	// Determine working directory for logging
	workingDir := task.WorkingDir
	if workingDir == "" {
		workingDir = config.WorkingDir
	}

	e.logger.Info("started claude code",
		zap.String("task_id", task.ID),
		zap.String("run_id", task.RunID),
		zap.String("working_dir", workingDir),
		zap.Bool("stream_mode", useStreamInput),
	)

	// In stream input mode, send the initial prompt via stdin
	if useStreamInput && task.Prompt != "" {
		msg := NewTextMessage(task.Prompt)
		msgBytes, err := json.Marshal(msg)
		if err != nil {
			_ = e.Kill()
			return e.failResult(result, fmt.Errorf("marshaling initial message: %w", err)), nil
		}
		// Send message with newline (NDJSON format)
		if _, err := stdin.Write(append(msgBytes, '\n')); err != nil {
			_ = e.Kill()
			return e.failResult(result, fmt.Errorf("sending initial message: %w", err)), nil
		}
		e.logger.Debug("sent initial message via stdin",
			zap.Int("bytes", len(msgBytes)),
		)
	}

	// Create context with timeout if specified
	execCtx := ctx
	var cancel context.CancelFunc
	if task.Timeout > 0 {
		execCtx, cancel = context.WithTimeout(ctx, task.Timeout)
		defer cancel()
	}

	// Create output parser to collect results
	parser := &outputParser{
		handler: handler,
		logger:  e.logger,
		onSessionID: func(sessionID string) {
			e.mu.Lock()
			e.sessionID = sessionID
			e.mu.Unlock()
		},
	}

	// Read stdout (stream-json) in a goroutine
	stdoutDone := make(chan error, 1)
	go func() {
		stdoutDone <- e.readStreamJSON(stdout, parser)
	}()

	// Read stderr in a goroutine (for error messages)
	stderrDone := make(chan error, 1)
	go func() {
		stderrDone <- e.readStderr(stderr, handler)
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

	// Wait for output readers to finish
	<-stdoutDone
	<-stderrDone

	// Build result from parsed data
	result.CompletedAt = time.Now()
	result.TokensInput = parser.totalInputTokens
	result.TokensOutput = parser.totalOutputTokens

	e.mu.Lock()
	killed := e.killed
	capturedSessionID := e.sessionID
	e.mu.Unlock()

	// Set agent session ID for future resume
	result.AgentSession = capturedSessionID

	// Build context snapshot for future resume
	if capturedSessionID != "" {
		snapshot := &ContextSnapshot{
			SessionID:  capturedSessionID,
			WorkingDir: workingDir,
			CreatedAt:  result.CompletedAt.Format(time.RFC3339),
		}
		if contextBytes, err := json.Marshal(snapshot); err == nil {
			result.Context = contextBytes
		}
	}

	switch {
	case killed:
		result.ExitCode = -1
		result.Error = ErrKilled.Error()
		result.Success = false
	case parser.hasError:
		result.ExitCode = 1
		result.Error = parser.errorMessage
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
		zap.Int64("tokens_input", result.TokensInput),
		zap.Int64("tokens_output", result.TokensOutput),
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
func (e *Executor) buildCommand(task *executor.Task, config *executor.AgentConfig) (cmd *exec.Cmd, useStreamInput bool, err error) {
	// Use custom command path if set (for testing), otherwise find claude binary
	claudePath := e.commandPath
	if claudePath == "" {
		claudePath, err = exec.LookPath("claude")
		if err != nil {
			return nil, false, fmt.Errorf("claude not found in PATH: %w", err)
		}
	}

	// Parse context snapshot if provided (for resume)
	var contextSnapshot *ContextSnapshot
	if len(task.Context) > 0 {
		contextSnapshot = &ContextSnapshot{}
		if err := json.Unmarshal(task.Context, contextSnapshot); err != nil {
			e.logger.Warn("failed to parse context snapshot, starting fresh",
				zap.Error(err),
			)
			contextSnapshot = nil
		}
	}

	// Determine if we should use stream input mode
	// Stream input is used when resuming a session (needed for --resume)
	useStreamInput = contextSnapshot != nil && contextSnapshot.SessionID != ""

	// Build arguments
	args := []string{
		"--print",                        // Print mode (non-interactive)
		"--verbose",                      // Required for stream-json with --print
		"--output-format", "stream-json", // Stream JSON output for structured data
	}

	// Stream input mode (for resume and multi-turn)
	if useStreamInput {
		args = append(args, "--input-format", "stream-json")
	}

	// Session resume (extracted from context)
	if contextSnapshot != nil && contextSnapshot.SessionID != "" {
		args = append(args, "--resume", contextSnapshot.SessionID)
	}

	// Add permission mode based on sandbox_mode
	// See docs/agents.md for sandbox mode design
	sandboxMode := config.Extra["sandbox_mode"]
	switch sandboxMode {
	case "runner-is-sandbox", "runner-creates-sandbox":
		// Sandboxed environment - skip all permission prompts
		args = append(args, "--dangerously-skip-permissions")
	case "none", "":
		// No sandbox - use permission mode to handle prompts
		// Default to acceptEdits for non-sandboxed environments
		permMode := config.Extra["permission_mode"]
		if permMode == "" {
			permMode = "acceptEdits"
		}
		args = append(args, "--permission-mode", permMode)
	}

	// Add model if specified
	if config.Model != "" {
		args = append(args, "--model", config.Model)
	}

	// Add the prompt as the last argument (only for non-stream mode)
	// In stream mode, prompt is sent via stdin
	if !useStreamInput && task.Prompt != "" {
		args = append(args, "--", task.Prompt)
	}

	cmd = exec.Command(claudePath, args...) //nolint:gosec // claudePath is from LookPath, args are controlled

	// Set working directory (task.WorkingDir takes precedence)
	workingDir := task.WorkingDir
	if workingDir == "" {
		workingDir = config.WorkingDir
	}
	if workingDir != "" {
		cmd.Dir = workingDir
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

	return cmd, useStreamInput, nil
}

// outputParser parses stream-json output and forwards to handler.
type outputParser struct {
	handler executor.OutputHandler
	logger  *zap.Logger

	// Callback for session ID (from init message)
	onSessionID func(sessionID string)

	// Accumulated stats
	totalInputTokens  int64
	totalOutputTokens int64
	hasError          bool
	errorMessage      string
}

// readStreamJSON reads and parses the stream-json output.
func (e *Executor) readStreamJSON(r io.Reader, parser *outputParser) error {
	scanner := bufio.NewScanner(r)
	// Increase buffer size for long lines
	buf := make([]byte, 64*1024)
	scanner.Buffer(buf, 1024*1024)

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		// Always record raw JSON line for audit/replay
		parser.handler.HandleOutput("json", append(append([]byte{}, line...), '\n'))

		var msg StreamMessage
		if err := json.Unmarshal(line, &msg); err != nil {
			// Not valid JSON, also forward as text
			parser.handler.HandleOutput("stdout", append(line, '\n'))
			continue
		}

		parser.handleMessage(&msg)
	}

	return scanner.Err()
}

// handleMessage processes a single stream message.
func (p *outputParser) handleMessage(msg *StreamMessage) {
	switch msg.Type {
	case "init":
		p.logger.Debug("stream init",
			zap.String("session_id", msg.SessionID),
			zap.String("model", msg.Model),
		)
		// Notify executor of the session ID
		if p.onSessionID != nil && msg.SessionID != "" {
			p.onSessionID(msg.SessionID)
		}

	case "assistant":
		// Full assistant message - extract content
		if msg.Message != nil {
			for _, block := range msg.Message.Content {
				if block.Type == "text" && block.Text != "" {
					p.handler.HandleOutput("stdout", []byte(block.Text))
				}
			}
			// Accumulate usage
			if msg.Message.Usage != nil {
				p.totalInputTokens += msg.Message.Usage.InputTokens
				p.totalOutputTokens += msg.Message.Usage.OutputTokens
			}
		}

	case "content_block_delta":
		// Streaming text delta
		if msg.Delta != nil && msg.Delta.Text != "" {
			p.handler.HandleOutput("stdout", []byte(msg.Delta.Text))
		}

	case "tool_use":
		// Tool invocation - raw JSON already recorded
		if msg.ToolUse != nil {
			p.logger.Debug("tool use",
				zap.String("tool", msg.ToolUse.Name),
				zap.String("id", msg.ToolUse.ID),
			)
		}

	case "tool_result":
		// Tool result - raw JSON already recorded
		if msg.ToolResult != nil {
			p.logger.Debug("tool result",
				zap.String("tool_use_id", msg.ToolResult.ToolUseID),
				zap.Bool("is_error", msg.ToolResult.IsError),
			)
		}

	case "result":
		// Final result message with stats
		if msg.Result != nil {
			p.logger.Debug("stream result",
				zap.Int("num_turns", msg.Result.NumTurns),
				zap.Float64("cost_usd", msg.Result.CostUSD),
				zap.Int64("duration_ms", msg.Result.DurationMS),
			)
			// Get final usage if available
			if msg.Result.Usage != nil {
				// Use the final usage as it may be cumulative
				p.totalInputTokens = msg.Result.Usage.InputTokens
				p.totalOutputTokens = msg.Result.Usage.OutputTokens
			}
			if msg.Result.IsError {
				p.hasError = true
			}
		}

	case "error":
		// Error message
		if msg.Error != nil {
			p.hasError = true
			p.errorMessage = msg.Error.Message
			p.logger.Error("stream error",
				zap.String("code", msg.Error.Code),
				zap.String("message", msg.Error.Message),
			)
			p.handler.HandleOutput("stderr", []byte(msg.Error.Message+"\n"))
		}

	case "system":
		// System messages (e.g., API cost info)
		if msg.Data != "" {
			p.handler.HandleOutput("system", []byte(msg.Data+"\n"))
		}

	default:
		// Unknown message type, log for debugging
		p.logger.Debug("unknown stream message type", zap.String("type", msg.Type))
	}
}

// readStderr reads stderr and forwards to handler.
func (e *Executor) readStderr(r io.Reader, handler executor.OutputHandler) error {
	scanner := bufio.NewScanner(r)
	buf := make([]byte, 64*1024)
	scanner.Buffer(buf, 1024*1024)

	for scanner.Scan() {
		line := scanner.Bytes()
		handler.HandleOutput("stderr", append(line, '\n'))
	}

	return scanner.Err()
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
		AgentSession: r.AgentSession,
		Context:      r.Context,
		CompletedAt:  r.CompletedAt,
	}
}

// SendMessage sends a message to the running agent via stdin.
// Only valid when the executor is in stream input mode (Task.StreamInput = true).
func (e *Executor) SendMessage(msg []byte) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if !e.running {
		return ErrNotRunning
	}
	if !e.streamMode {
		return ErrNotStreamMode
	}
	if e.stdinWriter == nil {
		return ErrStdinNotReady
	}

	// Ensure message ends with newline for NDJSON format
	if len(msg) == 0 || msg[len(msg)-1] != '\n' {
		msg = append(msg, '\n')
	}

	_, err := e.stdinWriter.Write(msg)
	if err != nil {
		return fmt.Errorf("writing to stdin: %w", err)
	}

	e.logger.Debug("sent message via stdin",
		zap.Int("bytes", len(msg)),
	)

	return nil
}

// SendTextMessage sends a text message to the running agent.
// This is a convenience method that wraps the text in a StreamInputMessage.
func (e *Executor) SendTextMessage(text string) error {
	msg := NewTextMessage(text)
	msgBytes, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshaling message: %w", err)
	}
	return e.SendMessage(msgBytes)
}

// IsStreamMode returns true if the executor is currently in stream input mode.
func (e *Executor) IsStreamMode() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.streamMode
}

// SessionID returns the session ID captured from the init message.
// Returns empty string if no session has been started.
func (e *Executor) SessionID() string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.sessionID
}

// Ensure Executor implements the interfaces.
var _ executor.Executor = (*Executor)(nil)
var _ executor.StreamExecutor = (*Executor)(nil)
