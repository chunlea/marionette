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

	// Close stdin to signal no input is coming (for --print mode)
	// Claude Code waits for stdin EOF before producing output
	if !e.streamMode {
		_ = stdin.Close()
	}

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

	// Wait for output processing to complete
	wg.Wait()

	// Wait for command to exit
	err = cmd.Wait()

	// Build result
	result := &executor.Result{
		CompletedAt:  time.Now(),
		AgentSession: e.parser.SessionID(),
	}

	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			result.ExitCode = exitErr.ExitCode()
			result.Success = false
			result.Error = fmt.Sprintf("exit code %d", result.ExitCode)
		} else if ctx.Err() != nil {
			result.Success = false
			result.Error = ctx.Err().Error()
			if ctx.Err() == context.DeadlineExceeded {
				result.Error = "timeout"
			} else if ctx.Err() == context.Canceled {
				result.Error = "canceled"
			}
		} else {
			result.Success = false
			result.Error = err.Error()
		}
	} else {
		result.Success = true
		result.ExitCode = 0
	}

	return result, nil
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

	// Add prompt
	if task.Prompt != "" {
		args = append(args, "--print", task.Prompt)
	}

	return args, hasResume
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

		// Process events (could emit to a separate event handler in the future)
		for _, event := range events {
			// Check for tool use that might require permission
			if event.Type == executor.EventToolUse && event.ToolUse != nil {
				if IsPermissionRequired(event.ToolUse.Name) {
					// Create permission request
					req := &executor.PermissionRequest{
						ID:        event.ToolUse.ID,
						Tool:      event.ToolUse.Name,
						Action:    event.ToolUse.Input,
						RiskLevel: executor.RiskMedium,
					}

					// Send permission request and block until approved/denied
					approved, err := handler.HandlePermissionRequest(ctx, req)
					if err != nil {
						handler.HandleOutput("system", []byte(fmt.Sprintf("permission_request_error: %s %v", req.ID, err)))
						return // Context cancelled or other error - stop processing
					}
					if !approved {
						handler.HandleOutput("system", []byte(fmt.Sprintf("permission_denied: %s %s", req.Tool, req.ID)))
						// Note: With --permission-mode acceptEdits, Claude has already executed the tool.
						// Denial here is for audit/tracking purposes. Future: use a permission mode
						// that actually pauses Claude until approval.
					} else {
						handler.HandleOutput("system", []byte(fmt.Sprintf("permission_approved: %s %s", req.Tool, req.ID)))
					}
				}
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

	// Then kill process
	if e.cmd.Process != nil {
		return e.cmd.Process.Kill()
	}

	return nil
}

// SendMessage sends a message to the running Claude process.
// Only valid in stream mode (resume).
func (e *Executor) SendMessage(msg []byte) error {
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

	_, err := e.stdin.Write(append(msg, '\n'))
	return err
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
