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
	"strings"
	"sync"
	"time"

	"github.com/chunlea/marionette/pkg/agent/executor"
)

// processWaitDelay bounds how long Wait blocks after the process is killed
// while a surviving grandchild still holds stdout or stderr open.
const processWaitDelay = 5 * time.Second

var (
	// ErrNotRunning is returned when trying to operate on a non-running executor.
	ErrNotRunning = errors.New("executor not running")

	// ErrAlreadyRunning is returned when trying to start an already running executor.
	ErrAlreadyRunning = errors.New("executor already running")

	// ErrNotStreamMode is returned when trying to send a message in non-stream mode.
	ErrNotStreamMode = errors.New("not in stream mode")
)

// Executor implements the executor.StreamExecutor interface for Claude Code.
type Executor struct {
	mu sync.Mutex

	// Command configuration
	binaryPath string

	// Running state
	cmd        *exec.Cmd
	stdin      io.WriteCloser
	running    bool
	streamMode bool

	// Parser for output
	parser *Parser

	// Permission gating
	hookArgv       []string
	permissionWait time.Duration
	gatingDisabled bool

	// Context for cancellation
	cancelFunc context.CancelFunc
}

// Option configures the Executor.
type Option func(*Executor)

// WithBinaryPath sets the path to the Claude Code binary.
func WithBinaryPath(path string) Option {
	return func(e *Executor) {
		e.binaryPath = path
	}
}

// WithPermissionHookCommand sets the command the CLI runs as its PreToolUse
// hook. It defaults to this binary re-invoked as `permission-hook`, which is
// what the agent wants; tests override it.
func WithPermissionHookCommand(argv ...string) Option {
	return func(e *Executor) {
		e.hookArgv = argv
	}
}

// WithPermissionWait sets how long a gated tool call may wait for an operator
// decision before it is denied.
func WithPermissionWait(d time.Duration) Option {
	return func(e *Executor) {
		e.permissionWait = d
	}
}

// WithoutPermissionGating disables pre-execution gating.
//
// Use it only where an operator has deliberately accepted that the agent runs
// unsupervised. It must never be a fallback for a gate that failed to start:
// running ungated while reporting success is the exact dishonesty this
// executor is being repaired to remove.
func WithoutPermissionGating() Option {
	return func(e *Executor) {
		e.gatingDisabled = true
	}
}

// New creates a new Claude executor.
func New(opts ...Option) *Executor {
	e := &Executor{
		binaryPath: "claude", // Default to finding in PATH
	}

	for _, opt := range opts {
		opt(e)
	}

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
	e.parser = NewParser().(*Parser)
	e.mu.Unlock()

	defer func() {
		e.mu.Lock()
		e.running = false
		e.cmd = nil
		e.stdin = nil
		e.streamMode = false
		e.cancelFunc = nil
		e.mu.Unlock()
	}()

	// Build command arguments
	args, hasResume := e.buildArgs(task, config)

	// Real permission gating runs as a PreToolUse hook in a subprocess that
	// calls back into this broker. Starting it is not optional: if the gate
	// cannot run, the task fails rather than running unsupervised.
	if !e.gatingDisabled {
		broker, err := NewPermissionBroker(handler, e.permissionWait)
		if err != nil {
			return nil, fmt.Errorf("failed to start permission gate: %w", err)
		}
		defer func() { _ = broker.Close() }()

		hookArgv, err := e.permissionHookArgv(broker.SocketPath())
		if err != nil {
			return nil, fmt.Errorf("failed to resolve permission hook command: %w", err)
		}

		settings, err := hookSettings(hookArgv, broker.Wait())
		if err != nil {
			return nil, fmt.Errorf("failed to build permission hook settings: %w", err)
		}

		args = append(args, "--settings", settings)
		brokerCtx, brokerCancel := context.WithCancel(ctx)
		defer brokerCancel()
		broker.Serve(brokerCtx)
	}

	// Create cancellable context
	ctx, cancel := context.WithCancel(ctx)
	e.mu.Lock()
	e.cancelFunc = cancel
	e.mu.Unlock()
	defer cancel()

	// Apply timeout if set
	if task.Timeout > 0 {
		var timeoutCancel context.CancelFunc
		ctx, timeoutCancel = context.WithTimeout(ctx, task.Timeout)
		defer timeoutCancel()
	}

	// Create command
	cmd := exec.CommandContext(ctx, e.binaryPath, args...)

	// Kill the whole process tree on cancel or timeout, not just the CLI: its
	// tool subprocesses would otherwise keep running and keep our pipes open,
	// so Wait would block long past the deadline we just enforced.
	configureProcessGroup(cmd)
	cmd.Cancel = func() error { return killProcessTree(cmd.Process) }
	// Backstop for a grandchild that survives the kill still holding a pipe.
	cmd.WaitDelay = processWaitDelay

	// Set working directory
	workDir := task.WorkingDir
	if workDir == "" && config != nil {
		workDir = config.WorkingDir
	}
	if workDir != "" {
		cmd.Dir = workDir
	}

	// Set environment
	cmd.Env = e.buildEnv(config)

	// Set up pipes
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to create stdout pipe: %w", err)
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to create stderr pipe: %w", err)
	}

	// Set up stdin for stream mode (resume)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to create stdin pipe: %w", err)
	}

	e.mu.Lock()
	e.cmd = cmd
	e.stdin = stdin
	// Enable stream mode only if we're actually resuming (--resume flag was added)
	// This is important: context snapshot may exist but not have a conversation_id,
	// in which case we should NOT be in stream mode (stdin must be closed).
	e.streamMode = hasResume
	e.mu.Unlock()

	// Start command
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("failed to start claude: %w", err)
	}

	// Emit system event for start with command info for debugging
	handler.HandleOutput("system", []byte("Claude Code started"))
	handler.HandleOutput("system", []byte(fmt.Sprintf("command: %s %s", e.binaryPath, strings.Join(args, " "))))

	// Process output in goroutines
	var wg sync.WaitGroup
	wg.Add(2)

	// Stdout processing (stream-json)
	go func() {
		defer wg.Done()
		e.processOutput(ctx, stdout, "stdout", handler, task)
	}()

	// Stderr processing (raw text)
	go func() {
		defer wg.Done()
		e.processStderr(ctx, stderr, handler)
	}()

	if e.streamMode {
		// With --input-format stream-json the CLI reads the prompt from stdin
		// and ignores the positional prompt argument entirely (verified on
		// 2.1.241), so the prompt must be delivered as the first user message.
		if err := e.SendMessage([]byte(task.Prompt)); err != nil {
			e.killProcess()
			wg.Wait()
			_ = cmd.Wait()
			return nil, fmt.Errorf("failed to send prompt to claude: %w", err)
		}
	} else {
		// --print mode: the CLI waits for stdin EOF before producing output.
		e.closeStdin()
	}

	// Wait for output processing to complete
	wg.Wait()

	// Wait for command to exit
	err = cmd.Wait()

	return e.buildResult(ctx, err), nil
}

// buildResult derives the run outcome from the CLI's own result line and the
// process exit status. The result line is authoritative for what the agent
// did; the exit status only tells us whether the process itself survived.
func (e *Executor) buildResult(ctx context.Context, waitErr error) *executor.Result {
	result := &executor.Result{
		CompletedAt:  time.Now(),
		AgentSession: e.parser.SessionID(),
	}

	// Carry the CLI session id forward so the next task can --resume it.
	if result.AgentSession != "" {
		if snapshot, err := json.Marshal(contextSnapshot{ConversationID: result.AgentSession}); err == nil {
			result.ContextSnapshot = snapshot
		}
	}

	// Token counts come from the final result line's usage block, which is the
	// authoritative per-run total. Input counts everything fed to the model,
	// including the cached prefix, because the raw input_tokens field excludes
	// cache hits and is close to meaningless on its own.
	agentResult := e.parser.Result()
	if agentResult != nil && agentResult.Usage != nil {
		u := agentResult.Usage
		result.TokensInput = u.InputTokens + u.CacheCreationInputTokens + u.CacheReadInputTokens
		result.TokensOutput = u.OutputTokens
	}

	// Process-level failures win: if the process died, nothing the agent said
	// before dying makes the run a success.
	if waitErr != nil {
		result.Success = false

		var exitErr *exec.ExitError
		switch {
		case ctx.Err() != nil:
			result.Error = ctx.Err().Error()
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				result.Error = "timeout"
			} else if errors.Is(ctx.Err(), context.Canceled) {
				result.Error = "canceled"
			}
			if errors.As(waitErr, &exitErr) {
				result.ExitCode = exitErr.ExitCode()
			}
		case errors.As(waitErr, &exitErr):
			result.ExitCode = exitErr.ExitCode()
			result.Error = fmt.Sprintf("exit code %d", result.ExitCode)
			// Prefer the agent's own explanation when it managed to emit one.
			if agentResult != nil && !agentResult.Succeeded() {
				result.Error = agentResult.FailureReason()
			}
		default:
			result.Error = waitErr.Error()
		}

		return result
	}

	result.ExitCode = 0

	// The process exited cleanly. Now the result line decides the outcome.
	// A clean exit with no result line means the CLI never finished its turn:
	// reporting that as success is exactly the silent-failure mode this path
	// used to have, so it is reported as a failure instead.
	switch {
	case agentResult == nil:
		result.Success = false
		result.Error = "claude exited without emitting a result message"
	case !agentResult.Succeeded():
		result.Success = false
		result.Error = agentResult.FailureReason()
	default:
		result.Success = true
	}

	return result
}

// contextSnapshot is the shape the agent stores for session resume. buildArgs
// reads it back to decide whether to pass --resume.
type contextSnapshot struct {
	ConversationID string `json:"conversation_id"`
}

// buildArgs constructs command line arguments for Claude Code.
// Returns the args and whether --resume was added (for stream mode).
func (e *Executor) buildArgs(task *executor.Task, config *executor.AgentConfig) ([]string, bool) {
	args := []string{
		"--output-format", "stream-json",
		"--verbose",
		"--permission-mode", "acceptEdits",
	}

	// Add working directory to allowed directories for write access
	// This is required for Claude's sandbox to allow file writes
	workDir := task.WorkingDir
	if workDir == "" && config != nil {
		workDir = config.WorkingDir
	}
	if workDir != "" {
		args = append(args, "--add-dir", workDir)
	}

	// Add model if specified
	if config != nil && config.Model != "" {
		args = append(args, "--model", config.Model)
	}

	// Check for resume mode
	hasResume := false
	if task.ContextSnapshot != nil && len(task.ContextSnapshot) > 0 {
		// Try to extract conversation_id from context (Claude's session ID for --resume)
		var ctxData struct {
			ConversationID string `json:"conversation_id"`
			// Also support session_id for backwards compatibility
			SessionID string `json:"session_id"`
		}
		if err := json.Unmarshal(task.ContextSnapshot, &ctxData); err == nil {
			// Prefer conversation_id, fall back to session_id
			resumeID := ctxData.ConversationID
			if resumeID == "" {
				resumeID = ctxData.SessionID
			}
			if resumeID != "" {
				args = append(args, "--resume", resumeID)
				hasResume = true
			}
		}
	}

	if hasResume {
		// Stream input keeps stdin open so further user messages can be sent
		// during the turn. It requires --print, and it makes the CLI ignore a
		// positional prompt, so the prompt goes over stdin instead.
		args = append(args, "--input-format", "stream-json", "--print")
		return args, hasResume
	}

	// Add prompt
	if task.Prompt != "" {
		args = append(args, "--print", task.Prompt)
	}

	return args, hasResume
}

// permissionHookArgv returns the command the CLI runs for each tool call.
func (e *Executor) permissionHookArgv(socketPath string) ([]string, error) {
	if len(e.hookArgv) > 0 {
		return append(append([]string{}, e.hookArgv...), socketPath), nil
	}

	self, err := os.Executable()
	if err != nil {
		return nil, err
	}
	return []string{self, PermissionHookCommand, socketPath}, nil
}

// buildEnv constructs environment variables for Claude Code.
func (e *Executor) buildEnv(config *executor.AgentConfig) []string {
	env := os.Environ()

	if config == nil {
		return env
	}

	// Set API key
	if config.APIKey != "" {
		env = append(env, "ANTHROPIC_API_KEY="+config.APIKey)
	}

	// Set base URL if specified
	if config.BaseURL != "" {
		env = append(env, "ANTHROPIC_BASE_URL="+config.BaseURL)
	}

	// Add any extra environment variables
	for k, v := range config.Extra {
		env = append(env, k+"="+v)
	}

	return env
}

// processOutput processes stdout from Claude Code (stream-json format).
func (e *Executor) processOutput(ctx context.Context, r io.Reader, stream string, handler executor.OutputHandler, task *executor.Task) {
	scanner := bufio.NewScanner(r)
	// Increase buffer size for large outputs
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 1024*1024)

	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return
		default:
		}

		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		// Send raw output
		handler.HandleOutput(stream, line)

		// Try to parse the raw message to check for init
		var rawMsg StreamMessage
		if err := json.Unmarshal(line, &rawMsg); err == nil {
			// Check for init message with session_id
			if rawMsg.Type == MessageTypeSystem && rawMsg.Subtype == SystemSubtypeInit && rawMsg.SessionID != "" {
				// Notify handler about context update with Claude's session_id
				handler.HandleContextUpdate(ctx, task.SessionID, rawMsg.SessionID)
			}
		}

		// Parse into events
		events, err := e.parser.ParseLine(line)
		if err != nil {
			handler.HandleOutput("system", []byte(fmt.Sprintf("parse error: %v", err)))
			continue
		}

		// In stream mode the CLI keeps reading stdin and would serve turn after
		// turn until EOF. A task is one turn, so close stdin as soon as the
		// turn's result arrives; without this the process never exits and the
		// run hangs until its timeout.
		if e.streamMode && e.parser.Result() != nil {
			e.closeStdin()
		}

		// Permission gating is NOT done here. By the time a tool_use event
		// appears in the output stream the tool has already run, so asking
		// here could only ever produce a decision after the fact. The real
		// gate is the PreToolUse hook wired up in Execute; see
		// permission_broker.go.
		for _, event := range events {
			if event.Type == executor.EventToolUse && event.ToolUse != nil {
				handler.HandleOutput("system", []byte(fmt.Sprintf(
					"tool_use: %s %s", event.ToolUse.Name, event.ToolUse.ID)))
			}
		}
	}

	if err := scanner.Err(); err != nil && !errors.Is(err, io.EOF) {
		handler.HandleOutput("system", []byte(fmt.Sprintf("stdout scan error: %v", err)))
	}
}

// processStderr processes stderr from Claude Code.
func (e *Executor) processStderr(ctx context.Context, r io.Reader, handler executor.OutputHandler) {
	scanner := bufio.NewScanner(r)
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 1024*1024)

	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return
		default:
		}

		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		handler.HandleOutput("stderr", line)
	}

	if err := scanner.Err(); err != nil && !errors.Is(err, io.EOF) {
		handler.HandleOutput("system", []byte(fmt.Sprintf("stderr scan error: %v", err)))
	}
}

// Kill terminates the running Claude process.
func (e *Executor) Kill() error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if !e.running || e.cmd == nil {
		return nil // Not an error to kill non-running executor
	}

	// Cancel context first
	if e.cancelFunc != nil {
		e.cancelFunc()
	}

	// Then kill the process tree
	if e.cmd.Process != nil {
		return killProcessTree(e.cmd.Process)
	}

	return nil
}

// streamInputMessage is the stream-json envelope the CLI accepts on stdin when
// started with --input-format stream-json. Verified against CLI 2.1.241: a
// single line of this shape drives one turn, and the same process serves
// further turns until stdin reaches EOF.
type streamInputMessage struct {
	Type    string          `json:"type"`
	Message streamInputBody `json:"message"`
}

type streamInputBody struct {
	Role    string               `json:"role"`
	Content []streamInputContent `json:"content"`
}

type streamInputContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// userMessageEnvelope wraps plain text in the CLI's stream-json user message.
func userMessageEnvelope(text string) ([]byte, error) {
	return json.Marshal(streamInputMessage{
		Type: "user",
		Message: streamInputBody{
			Role:    "user",
			Content: []streamInputContent{{Type: "text", Text: text}},
		},
	})
}

// SendMessage sends a user message to the running Claude process.
//
// msg is the message TEXT; it is wrapped in the stream-json envelope the CLI
// expects. Only valid in stream mode (resume), and only until the turn's
// result arrives, after which stdin is closed so the process can exit.
func (e *Executor) SendMessage(msg []byte) error {
	envelope, err := userMessageEnvelope(string(msg))
	if err != nil {
		return fmt.Errorf("failed to encode user message: %w", err)
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	if !e.running {
		return ErrNotRunning
	}

	if !e.streamMode {
		return ErrNotStreamMode
	}

	if e.stdin == nil {
		return ErrNotRunning
	}

	_, err = e.stdin.Write(append(envelope, '\n'))
	return err
}

// closeStdin closes the CLI's stdin exactly once. Further SendMessage calls
// then fail with ErrNotRunning rather than writing to a closed pipe.
func (e *Executor) closeStdin() {
	e.mu.Lock()
	stdin := e.stdin
	e.stdin = nil
	e.mu.Unlock()

	if stdin != nil {
		_ = stdin.Close()
	}
}

// killProcess terminates the running process without touching Kill's
// not-running bookkeeping.
func (e *Executor) killProcess() {
	e.mu.Lock()
	cmd := e.cmd
	cancel := e.cancelFunc
	e.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	if cmd != nil && cmd.Process != nil {
		_ = killProcessTree(cmd.Process)
	}
}

// IsStreamMode returns true if the executor is in stream mode.
func (e *Executor) IsStreamMode() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.streamMode
}

// Ensure Executor implements the interfaces.
var (
	_ executor.Executor       = (*Executor)(nil)
	_ executor.StreamExecutor = (*Executor)(nil)
)
