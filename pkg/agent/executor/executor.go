// Package executor provides interfaces and implementations for running AI agents.
package executor

import (
	"context"
	"io"
	"time"
)

// Task represents a task to be executed by an agent.
type Task struct {
	ID             string
	RunID          string
	SessionID      string
	ConversationID string // Conversation this task belongs to
	Attempt        int32
	Prompt         string
	Timeout        time.Duration
	WorkingDir     string // Working directory (may be worktree path)

	// Context for resume - contains previous conversation state
	// The executor interprets this based on agent type:
	// - Claude: extracts session_id and uses --resume
	// - Other agents: may parse as conversation history
	Context []byte
}

// AgentConfig holds the configuration for running an agent.
type AgentConfig struct {
	Agent      string            // Agent type: "claude", "codex", etc.
	Model      string            // Model to use
	APIKey     string            // API key for the agent
	BaseURL    string            // Optional base URL override
	Extra      map[string]string // Additional configuration
	WorkingDir string            // Working directory for the agent
}

// Result contains the outcome of task execution.
type Result struct {
	Success      bool
	ExitCode     int
	Error        string
	TokensInput  int64
	TokensOutput int64
	AgentSession string // Agent's internal session ID (for resume)
	Context      []byte // Context snapshot for session restore
	CompletedAt  time.Time
}

// OutputHandler receives output from the executor.
type OutputHandler interface {
	// HandleOutput is called when output is received from the agent.
	// stream is "stdout", "stderr", or "system"
	HandleOutput(stream string, data []byte)

	// HandlePermissionRequest is called when the agent requests permission.
	// Returns true if approved, false if denied.
	// This blocks until a response is received or context is cancelled.
	HandlePermissionRequest(ctx context.Context, req *PermissionRequest) (approved bool, err error)
}

// PermissionRequest represents a permission request from the agent.
type PermissionRequest struct {
	ID        string
	Tool      string // "bash", "edit", "browser", etc.
	Action    string // Command or description
	Context   string // Additional context
	RiskLevel string // "low", "medium", "high", "critical"
}

// Executor defines the interface for running AI agents.
type Executor interface {
	// Execute runs the agent with the given task and configuration.
	// It blocks until the task completes, is killed, or context is cancelled.
	// Output and permission requests are sent to the OutputHandler.
	Execute(ctx context.Context, task *Task, config *AgentConfig, handler OutputHandler) (*Result, error)

	// Kill terminates the running task.
	// This is safe to call even if no task is running.
	Kill() error

	// Name returns the name of this executor (e.g., "claude", "codex").
	Name() string
}

// StreamExecutor extends Executor with stream input capabilities.
// Agents that support bidirectional communication implement this interface.
// The executor decides internally when to use stream mode (e.g., for resume).
type StreamExecutor interface {
	Executor

	// SendMessage sends a message to the running agent.
	// Only valid when the executor is in stream mode.
	// Returns ErrNotRunning if no task is running.
	// Returns ErrNotStreamMode if not in stream mode.
	SendMessage(msg []byte) error

	// IsStreamMode returns true if the executor is currently in stream input mode.
	IsStreamMode() bool
}

// OutputWriter wraps an OutputHandler to implement io.Writer.
type OutputWriter struct {
	Handler OutputHandler
	Stream  string // "stdout" or "stderr"
}

// Write implements io.Writer.
func (w *OutputWriter) Write(p []byte) (n int, err error) {
	w.Handler.HandleOutput(w.Stream, p)
	return len(p), nil
}

// Ensure OutputWriter implements io.Writer.
var _ io.Writer = (*OutputWriter)(nil)
