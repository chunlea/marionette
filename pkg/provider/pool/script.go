package pool

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"go.uber.org/zap"
)

// ScriptExecutor handles execution of init and cleanup scripts for pool runners.
type ScriptExecutor struct {
	logger *zap.Logger
}

// NewScriptExecutor creates a new script executor.
func NewScriptExecutor(logger *zap.Logger) *ScriptExecutor {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &ScriptExecutor{logger: logger}
}

// ScriptType represents the type of script being executed.
type ScriptType string

const (
	ScriptTypeInit    ScriptType = "init"
	ScriptTypeCleanup ScriptType = "cleanup"
)

// ScriptContext contains context for script execution.
type ScriptContext struct {
	// RunnerID is the ID of the runner.
	RunnerID string

	// RunnerName is the name of the runner.
	RunnerName string

	// SessionID is the session ID (for init scripts).
	SessionID string

	// TaskID is the task ID (for cleanup scripts).
	TaskID string

	// WorkspacePath is the path to the workspace.
	WorkspacePath string

	// PoolName is the name of the pool.
	PoolName string

	// Labels are runner labels.
	Labels map[string]string

	// ExtraEnv contains additional environment variables.
	ExtraEnv map[string]string
}

// ScriptResult contains the result of script execution.
type ScriptResult struct {
	// ExitCode is the exit code of the script.
	ExitCode int

	// Stdout is the standard output.
	Stdout string

	// Stderr is the standard error output.
	Stderr string

	// Duration is how long the script ran.
	Duration time.Duration

	// TimedOut indicates if the script timed out.
	TimedOut bool
}

// Execute runs a script with the given context and timeout.
func (e *ScriptExecutor) Execute(
	ctx context.Context,
	scriptType ScriptType,
	script string,
	scriptCtx ScriptContext,
	timeout time.Duration,
) (*ScriptResult, error) {
	if script == "" {
		return &ScriptResult{ExitCode: 0}, nil
	}

	// Create context with timeout
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}

	// Build environment variables
	env := e.buildEnv(scriptType, scriptCtx)

	// Create command
	cmd := exec.CommandContext(ctx, "/bin/sh", "-c", script)
	cmd.Env = env

	// Capture output
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	// Execute
	start := time.Now()
	err := cmd.Run()
	duration := time.Since(start)

	result := &ScriptResult{
		Stdout:   stdout.String(),
		Stderr:   stderr.String(),
		Duration: duration,
	}

	// Check for timeout
	if ctx.Err() == context.DeadlineExceeded {
		result.TimedOut = true
		result.ExitCode = -1
		e.logger.Warn("script timed out",
			zap.String("script_type", string(scriptType)),
			zap.String("runner_id", scriptCtx.RunnerID),
			zap.Duration("timeout", timeout),
		)
		return result, fmt.Errorf("script timed out after %v", timeout)
	}

	// Get exit code
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			result.ExitCode = exitErr.ExitCode()
		} else {
			result.ExitCode = -1
		}
		e.logger.Warn("script failed",
			zap.String("script_type", string(scriptType)),
			zap.String("runner_id", scriptCtx.RunnerID),
			zap.Int("exit_code", result.ExitCode),
			zap.String("stderr", truncate(result.Stderr, 500)),
		)
		return result, fmt.Errorf("script failed with exit code %d: %s", result.ExitCode, truncate(result.Stderr, 200))
	}

	result.ExitCode = 0
	e.logger.Info("script completed",
		zap.String("script_type", string(scriptType)),
		zap.String("runner_id", scriptCtx.RunnerID),
		zap.Duration("duration", duration),
	)

	return result, nil
}

// ExecuteInit runs the init script for a runner.
func (e *ScriptExecutor) ExecuteInit(
	ctx context.Context,
	script string,
	scriptCtx ScriptContext,
	timeout time.Duration,
) (*ScriptResult, error) {
	return e.Execute(ctx, ScriptTypeInit, script, scriptCtx, timeout)
}

// ExecuteCleanup runs the cleanup script for a runner.
func (e *ScriptExecutor) ExecuteCleanup(
	ctx context.Context,
	script string,
	scriptCtx ScriptContext,
	timeout time.Duration,
) (*ScriptResult, error) {
	return e.Execute(ctx, ScriptTypeCleanup, script, scriptCtx, timeout)
}

// buildEnv creates environment variables for script execution.
func (e *ScriptExecutor) buildEnv(scriptType ScriptType, ctx ScriptContext) []string {
	env := []string{
		fmt.Sprintf("MARIONETTE_SCRIPT_TYPE=%s", scriptType),
		fmt.Sprintf("MARIONETTE_RUNNER_ID=%s", ctx.RunnerID),
		fmt.Sprintf("MARIONETTE_RUNNER_NAME=%s", ctx.RunnerName),
		fmt.Sprintf("MARIONETTE_POOL_NAME=%s", ctx.PoolName),
	}

	if ctx.SessionID != "" {
		env = append(env, fmt.Sprintf("MARIONETTE_SESSION_ID=%s", ctx.SessionID))
	}

	if ctx.TaskID != "" {
		env = append(env, fmt.Sprintf("MARIONETTE_TASK_ID=%s", ctx.TaskID))
	}

	if ctx.WorkspacePath != "" {
		env = append(env, fmt.Sprintf("MARIONETTE_WORKSPACE=%s", ctx.WorkspacePath))
	}

	// Add labels as environment variables
	for k, v := range ctx.Labels {
		// Convert label key to valid env var name
		envKey := "MARIONETTE_LABEL_" + strings.ToUpper(strings.ReplaceAll(k, "-", "_"))
		env = append(env, fmt.Sprintf("%s=%s", envKey, v))
	}

	// Add extra environment variables
	for k, v := range ctx.ExtraEnv {
		env = append(env, fmt.Sprintf("%s=%s", k, v))
	}

	return env
}

// truncate truncates a string to the given length.
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
