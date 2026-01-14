package pool

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestNewScriptExecutor(t *testing.T) {
	e := NewScriptExecutor(nil)
	require.NotNil(t, e)

	e2 := NewScriptExecutor(zap.NewNop())
	require.NotNil(t, e2)
}

func TestScriptExecutor_Execute_EmptyScript(t *testing.T) {
	e := NewScriptExecutor(nil)

	result, err := e.Execute(context.Background(), ScriptTypeInit, "", ScriptContext{}, 0)
	require.NoError(t, err)
	assert.Equal(t, 0, result.ExitCode)
}

func TestScriptExecutor_Execute_SimpleScript(t *testing.T) {
	e := NewScriptExecutor(zap.NewNop())

	ctx := ScriptContext{
		RunnerID:   "run_123",
		RunnerName: "test-runner",
		PoolName:   "test-pool",
	}

	result, err := e.Execute(context.Background(), ScriptTypeInit, "echo hello", ctx, 5*time.Second)
	require.NoError(t, err)
	assert.Equal(t, 0, result.ExitCode)
	assert.Contains(t, result.Stdout, "hello")
	assert.False(t, result.TimedOut)
}

func TestScriptExecutor_Execute_ExitCode(t *testing.T) {
	e := NewScriptExecutor(zap.NewNop())

	result, err := e.Execute(context.Background(), ScriptTypeInit, "exit 42", ScriptContext{}, 5*time.Second)
	require.Error(t, err)
	assert.Equal(t, 42, result.ExitCode)
	assert.False(t, result.TimedOut)
}

func TestScriptExecutor_Execute_Timeout(t *testing.T) {
	e := NewScriptExecutor(zap.NewNop())

	result, err := e.Execute(context.Background(), ScriptTypeInit, "sleep 10", ScriptContext{}, 100*time.Millisecond)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "timed out")
	assert.True(t, result.TimedOut)
	assert.Equal(t, -1, result.ExitCode)
}

func TestScriptExecutor_Execute_Stderr(t *testing.T) {
	e := NewScriptExecutor(zap.NewNop())

	result, err := e.Execute(context.Background(), ScriptTypeInit, "echo error >&2 && exit 1", ScriptContext{}, 5*time.Second)
	require.Error(t, err)
	assert.Contains(t, result.Stderr, "error")
}

func TestScriptExecutor_Execute_EnvVariables(t *testing.T) {
	e := NewScriptExecutor(zap.NewNop())

	ctx := ScriptContext{
		RunnerID:      "run_123",
		RunnerName:    "test-runner",
		SessionID:     "sess_456",
		TaskID:        "task_789",
		WorkspacePath: "/workspace",
		PoolName:      "test-pool",
		Labels:        map[string]string{"gpu": "nvidia", "env-name": "prod"},
		ExtraEnv:      map[string]string{"CUSTOM_VAR": "custom_value"},
	}

	// Script that prints environment variables
	script := `
echo "MARIONETTE_RUNNER_ID=$MARIONETTE_RUNNER_ID"
echo "MARIONETTE_RUNNER_NAME=$MARIONETTE_RUNNER_NAME"
echo "MARIONETTE_SESSION_ID=$MARIONETTE_SESSION_ID"
echo "MARIONETTE_TASK_ID=$MARIONETTE_TASK_ID"
echo "MARIONETTE_WORKSPACE=$MARIONETTE_WORKSPACE"
echo "MARIONETTE_POOL_NAME=$MARIONETTE_POOL_NAME"
echo "MARIONETTE_SCRIPT_TYPE=$MARIONETTE_SCRIPT_TYPE"
echo "MARIONETTE_LABEL_GPU=$MARIONETTE_LABEL_GPU"
echo "MARIONETTE_LABEL_ENV_NAME=$MARIONETTE_LABEL_ENV_NAME"
echo "CUSTOM_VAR=$CUSTOM_VAR"
`

	result, err := e.Execute(context.Background(), ScriptTypeInit, script, ctx, 5*time.Second)
	require.NoError(t, err)
	assert.Contains(t, result.Stdout, "MARIONETTE_RUNNER_ID=run_123")
	assert.Contains(t, result.Stdout, "MARIONETTE_RUNNER_NAME=test-runner")
	assert.Contains(t, result.Stdout, "MARIONETTE_SESSION_ID=sess_456")
	assert.Contains(t, result.Stdout, "MARIONETTE_TASK_ID=task_789")
	assert.Contains(t, result.Stdout, "MARIONETTE_WORKSPACE=/workspace")
	assert.Contains(t, result.Stdout, "MARIONETTE_POOL_NAME=test-pool")
	assert.Contains(t, result.Stdout, "MARIONETTE_SCRIPT_TYPE=init")
	assert.Contains(t, result.Stdout, "MARIONETTE_LABEL_GPU=nvidia")
	assert.Contains(t, result.Stdout, "MARIONETTE_LABEL_ENV_NAME=prod")
	assert.Contains(t, result.Stdout, "CUSTOM_VAR=custom_value")
}

func TestScriptExecutor_ExecuteInit(t *testing.T) {
	e := NewScriptExecutor(zap.NewNop())

	result, err := e.ExecuteInit(context.Background(), "echo init", ScriptContext{}, 5*time.Second)
	require.NoError(t, err)
	assert.Equal(t, 0, result.ExitCode)
	assert.Contains(t, result.Stdout, "init")
}

func TestScriptExecutor_ExecuteCleanup(t *testing.T) {
	e := NewScriptExecutor(zap.NewNop())

	result, err := e.ExecuteCleanup(context.Background(), "echo cleanup", ScriptContext{}, 5*time.Second)
	require.NoError(t, err)
	assert.Equal(t, 0, result.ExitCode)
	assert.Contains(t, result.Stdout, "cleanup")
}

func TestTruncate(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		maxLen int
		want   string
	}{
		{
			name:   "short string",
			input:  "hello",
			maxLen: 10,
			want:   "hello",
		},
		{
			name:   "exact length",
			input:  "hello",
			maxLen: 5,
			want:   "hello",
		},
		{
			name:   "truncated",
			input:  "hello world",
			maxLen: 5,
			want:   "hello...",
		},
		{
			name:   "empty string",
			input:  "",
			maxLen: 5,
			want:   "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := truncate(tt.input, tt.maxLen)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestScriptExecutor_BuildEnv(t *testing.T) {
	e := NewScriptExecutor(nil)

	ctx := ScriptContext{
		RunnerID:      "run_123",
		RunnerName:    "test-runner",
		SessionID:     "sess_456",
		TaskID:        "task_789",
		WorkspacePath: "/workspace",
		PoolName:      "test-pool",
		Labels:        map[string]string{"key": "value"},
		ExtraEnv:      map[string]string{"EXTRA": "extra_value"},
	}

	env := e.buildEnv(ScriptTypeInit, ctx)

	envMap := make(map[string]string)
	for _, e := range env {
		parts := strings.SplitN(e, "=", 2)
		if len(parts) == 2 {
			envMap[parts[0]] = parts[1]
		}
	}

	assert.Equal(t, "init", envMap["MARIONETTE_SCRIPT_TYPE"])
	assert.Equal(t, "run_123", envMap["MARIONETTE_RUNNER_ID"])
	assert.Equal(t, "test-runner", envMap["MARIONETTE_RUNNER_NAME"])
	assert.Equal(t, "sess_456", envMap["MARIONETTE_SESSION_ID"])
	assert.Equal(t, "task_789", envMap["MARIONETTE_TASK_ID"])
	assert.Equal(t, "/workspace", envMap["MARIONETTE_WORKSPACE"])
	assert.Equal(t, "test-pool", envMap["MARIONETTE_POOL_NAME"])
	assert.Equal(t, "value", envMap["MARIONETTE_LABEL_KEY"])
	assert.Equal(t, "extra_value", envMap["EXTRA"])
}
